package autorepair

import (
	"fmt"
	"regexp"
	"strings"

	"shaurma-launcher-backend/internal/modgraph"
)

// BackupDirName — підтека, у яку Executor.BackupConfigs переносить конфіги
// (всередині службової теки лаунчера, а не теки гри: експорт збірки не має
// підхопити резервні копії).
const BackupDirName = "repair-backups"

var depIDRe = regexp.MustCompile(`^[\w.\-]{2,64}$`)

func disableSteps(files []string) []string {
	shown := strings.Join(head(files, 6), ", ")
	if len(files) > 6 {
		shown += fmt.Sprintf(" та ще %d", len(files)-6)
	}
	return []string{
		"Перейменувати на *.jar.disabled: " + shown,
		"Файли не видаляються — увімкнути їх назад можна на вкладці «Моди».",
	}
}

func disableFix(files []string, label string) *Fix {
	files = head(unique(files), maxFilesPerFix)
	if len(files) == 0 {
		return nil
	}
	return &Fix{Action: "disable_mods", Label: label, Steps: disableSteps(files), Params: map[string]any{"files": files}}
}

func installSteps(ids []string) []string {
	return []string{
		"Знайти в каталозі Modrinth та встановити: " + strings.Join(head(ids, 8), ", "),
		"Береться версія під Minecraft і завантажувач цієї збірки; незнайдені залежності буде перелічено.",
	}
}

func installFix(ids []string) *Fix {
	var ok []string
	for _, id := range unique(ids) {
		if depIDRe.MatchString(id) {
			ok = append(ok, id)
		}
	}
	ok = head(ok, maxFilesPerFix)
	if len(ok) == 0 {
		return nil
	}
	return &Fix{Action: "install_dependencies", Label: "Завантажити відсутні залежності", Steps: installSteps(ok), Params: map[string]any{"ids": ok}}
}

func memoryFix(currentMB, targetMB int, label string) *Fix {
	if targetMB <= 0 || targetMB == currentMB {
		return nil
	}
	return &Fix{
		Action: "set_memory",
		Label:  fmt.Sprintf("%s до %s", label, humanMemory(targetMB)),
		Steps: []string{
			fmt.Sprintf("Пам'ять збірки: %s → %s", humanMemory(currentMB), humanMemory(targetMB)),
			"Зберігається в налаштуваннях цієї збірки; глобальні налаштування не змінюються.",
		},
		Params: map[string]any{"memoryMb": targetMB},
	}
}

func javaFixes(required int, javaPath string) []*Fix {
	var fixes []*Fix
	steps := "Лаунчер сам завантажить офіційне середовище Mojang для версії гри цієї збірки."
	label := "Завантажити підходящу Java автоматично"
	if required >= 8 {
		label = fmt.Sprintf("Завантажити Java %d автоматично", required)
		steps = fmt.Sprintf("Лаунчер завантажить Java %d (офіційне середовище Mojang) у власну теку.", required)
	}
	fixes = append(fixes, &Fix{
		Action: "install_java", Label: label,
		Steps:  []string{steps, "Системна Java не змінюється."},
		Params: map[string]any{"major": required},
	})
	if javaPath != "" {
		fixes = append(fixes, autoJavaFix(javaPath))
	}
	return fixes
}

func autoJavaFix(javaPath string) *Fix {
	return &Fix{
		Action: "set_java", Label: "Повернути автоматичний вибір Java",
		Steps: []string{
			"Прибрати ручний шлях до Java: " + javaPath,
			"Лаунчер сам підбере Java потрібної версії або завантажить офіційне середовище Mojang.",
		},
		Params: map[string]any{"javaPath": ""},
	}
}

func jvmResetFix(in Input) *Fix {
	steps := []string{}
	if in.JVMArgs != "" {
		steps = append(steps, "Прибрати власні аргументи JVM збірки: "+clipStr(in.JVMArgs, 160))
	} else {
		steps = append(steps, "Прибрати власні аргументи JVM збірки")
	}
	steps = append(steps, "Старі значення можна повернути в налаштуваннях збірки («Продуктивність»).")
	return &Fix{Action: "reset_jvm_arguments", Label: "Скинути аргументи JVM", Steps: steps, Params: map[string]any{}}
}

func repairFix(loader string) *Fix {
	target := "версію Minecraft"
	if loader != "" && loader != "vanilla" {
		target += " і " + loaderTitles[loader]
	}
	return &Fix{
		Action: "repair_version", Label: "Відновити файли версії та завантажувача",
		Steps: []string{
			"Заново завантажити й перевірити " + target,
			"Моди, світи, конфіги та скріншоти не чіпаються.",
		},
		Params: map[string]any{},
	}
}

func updateLoaderFix(in Input, loader, minimum string, reinstall bool) *Fix {
	if !in.CanUpdateLoader || loaderDependencyIDs[loader] == nil {
		return nil
	}
	title := loaderTitles[loader]
	target := "останню стабільну версію"
	if minimum != "" {
		target = minimum + " або новішу"
	}
	label := fmt.Sprintf("Оновити %s до %s", title, target)
	if reinstall {
		label = fmt.Sprintf("Перевстановити %s останньої стабільної версії", title)
	}
	step := fmt.Sprintf("Завантажити %s %s", title, target)
	if in.MinecraftVersion != "" {
		step += " для Minecraft " + in.MinecraftVersion
	}
	if in.LoaderVersion != "" {
		step += fmt.Sprintf(" (зараз %s)", in.LoaderVersion)
	}
	return &Fix{
		Action: "update_loader", Label: label,
		Steps:  []string{step, "Версія завантажувача зберігається в налаштуваннях збірки.", "Моди, світи, конфіги та скріншоти не чіпаються."},
		Params: map[string]any{"minimum": minimum},
	}
}

func configFix(files []string) *Fix {
	files = head(unique(files), maxFilesPerFix)
	if len(files) == 0 {
		return nil
	}
	return &Fix{
		Action: "backup_configs", Label: "Скинути пошкоджені конфіги",
		Steps: []string{
			"Перенести в резервну теку лаунчера: " + strings.Join(head(files, 6), ", "),
			"Копії лежать у службовій теці лаунчера (repair-backups) — їх можна повернути вручну.",
			"Під час наступного запуску моди створять конфіги за замовчуванням.",
		},
		Params: map[string]any{"files": files},
	}
}

func graphicsFixes(in Input, mods []modgraph.ModFile) []*Fix {
	var fixes []*Fix
	if in.DedicatedGPU {
		fixes = append(fixes, &Fix{
			Action: "set_dedicated_gpu", Label: "Не примушувати дискретну відеокарту",
			Steps:  []string{"Глобальне налаштування «дискретна відеокарта для гри»: увімкнено → вимкнено", "Зміна стосується всіх збірок; повернути можна в налаштуваннях лаунчера."},
			Params: map[string]any{"enabled": false},
		})
	} else {
		fixes = append(fixes, &Fix{
			Action: "set_dedicated_gpu", Label: "Запускати гру на дискретній відеокарті",
			Steps:  []string{"Глобальне налаштування «дискретна відеокарта для гри»: вимкнено → увімкнено", "Зміна стосується всіх збірок; повернути можна в налаштуваннях лаунчера."},
			Params: map[string]any{"enabled": true},
		})
	}
	var shaders []string
	for _, m := range mods {
		lower := strings.ToLower(m.File)
		if shaderModIDs[m.ModID] || strings.Contains(lower, "iris") || strings.Contains(lower, "oculus") || strings.Contains(lower, "optifine") || strings.Contains(lower, "optifabric") {
			shaders = append(shaders, m.File)
		}
	}
	if f := disableFix(shaders, "Вимкнути моди шейдерів"); f != nil {
		fixes = append(fixes, f)
	}
	return fixes
}

func recentFix(mods []modgraph.ModFile, in Input) *Fix {
	if in.LastSuccess.IsZero() {
		return nil
	}
	cut := in.LastSuccess.Unix()
	var recent []string
	for _, m := range mods {
		if m.MTime > cut {
			recent = append(recent, m.File)
		}
	}
	if len(recent) == 0 || len(recent) > maxFilesPerFix {
		return nil
	}
	return disableFix(recent, "Вимкнути моди, додані після останнього вдалого запуску")
}

func setAccountFix(a Account, reason string) *Fix {
	steps := []string{"Активний акаунт → " + accountLabel(a)}
	if reason != "" {
		steps = append(steps, reason)
	}
	steps = append(steps, "Повернути попередній можна в списку акаунтів.")
	return &Fix{Action: "set_account", Label: "Запускати від акаунта " + accountLabel(a), Steps: steps, Params: map[string]any{"accountId": a.ID}}
}

func loginFix(label string) *Fix {
	if label == "" {
		label = "Увійти в Microsoft заново"
	}
	return &Fix{
		Action: "login_microsoft", Label: label,
		Steps: []string{
			"Відкриється вікно входу Microsoft — пароль вводиться лише на сайті Microsoft.",
			"Нові токени збережуться в системному сховищі й замінять застарілі.",
		},
		Params: map[string]any{},
	}
}
