package autorepair

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
)

// Executor — дії, які рушій просить виконати над збіркою. Реалізує App.
type Executor interface {
	// DisableMods вимикає jar за іменами файлів; повертає вимкнені та пропущені.
	DisableMods(files []string) (done, skipped []string)
	// InstallDependencies ставить залежності з каталогу.
	InstallDependencies(ids []string) (installed, failed []string)
	SetMemory(mb int) error
	// SetJava: "" — автовибір.
	SetJava(path string) error
	// InstallJava завантажує Java потрібного мажору (0 — за версією гри збірки).
	InstallJava(major int) error
	ResetJVMArguments() error
	RepairVersion() error
	// UpdateLoader змінює версію завантажувача (не нижче minimum) і повертає її.
	UpdateLoader(minimum string) (string, error)
	// BackupConfigs переносить конфіги (відносні шляхи "config/…") у резервну
	// теку; повертає перенесені.
	BackupConfigs(files []string) ([]string, error)
	SetDedicatedGPU(enabled bool) error
	// SetAccount робить акаунт активним і повертає його назву.
	SetAccount(id string) (string, error)
	// TotalRAMMB — фізична пам'ять (0 — невідомо) для перевірки set_memory.
	TotalRAMMB() int
}

var fixActions = map[string]bool{
	"disable_mods": true, "install_dependencies": true, "set_memory": true, "set_java": true,
	"install_java": true, "reset_jvm_arguments": true, "repair_version": true, "backup_configs": true,
	"set_dedicated_gpu": true, "update_loader": true, "set_account": true, "login_microsoft": true,
}

var (
	loaderVersionRe = regexp.MustCompile(`^[\w.+\-]{1,64}$`)
	accountIDRe     = regexp.MustCompile(`^[\w.\-]{1,200}$`)
)

// ErrNoFixes — порожній список лагоджень.
var ErrNoFixes = errors.New("не вибрано жодного лагодження")

// Apply застосовує вибрані лагодження. Параметри перевіряються заново —
// інтерфейсу не довіряємо. Довгі (repair_version, update_loader) виконуються
// останніми: від решти вони не залежать.
func Apply(ex Executor, fixes []Fix) ([]FixResult, error) {
	return ApplyProgress(ex, fixes, nil)
}

// ApplyProgress — те саме, але перед кожним кроком викликає onStep(done,
// total, fix). Лаунчер шле з нього подію repair:progress, щоб модалка
// «Авто-починка» показувала, ЯКИЙ крок зараз триває: repair_version,
// install_java чи update_loader качають сотні мегабайт і без цього вікно
// виглядає завислим на «Застосовую…».
func ApplyProgress(ex Executor, fixes []Fix, onStep func(done, total int, f Fix)) ([]FixResult, error) {
	if len(fixes) == 0 {
		return nil, ErrNoFixes
	}
	if len(fixes) > 20 {
		return nil, errors.New("забагато лагоджень за раз")
	}
	ordered := make([]Fix, 0, len(fixes))
	var late []Fix
	for _, f := range fixes {
		if f.Action == "repair_version" || f.Action == "update_loader" {
			late = append(late, f)
		} else {
			ordered = append(ordered, f)
		}
	}
	ordered = append(ordered, late...)

	results := make([]FixResult, 0, len(ordered))
	for i, f := range ordered {
		if onStep != nil {
			onStep(i, len(ordered), f)
		}
		id := f.ID
		if id == "" {
			id = f.Action
		}
		id = clipStr(id, 120)
		if !fixActions[f.Action] {
			results = append(results, FixResult{ID: id, Message: "Невідоме лагодження: " + f.Action})
			continue
		}
		res := applyOne(ex, f.Action, f.Params)
		res.ID = id
		results = append(results, res)
	}
	return results, nil
}

func applyOne(ex Executor, action string, params map[string]any) (res FixResult) {
	fail := func(format string, a ...any) FixResult { return FixResult{Message: fmt.Sprintf(format, a...)} }
	ok := func(msg string) FixResult { return FixResult{OK: true, Message: msg} }
	defer func() {
		if r := recover(); r != nil {
			res = fail("Внутрішня помилка: %v", r)
		}
	}()
	if params == nil {
		params = map[string]any{}
	}
	switch action {
	case "disable_mods":
		var files []string
		for _, name := range head(stringList(params["files"]), maxFilesPerFix) {
			// лише ім'я файлу прямо в mods, без підтек
			if name == "" || strings.ContainsAny(name, `/\`) || name == ".." {
				continue
			}
			files = append(files, name)
		}
		done, skipped := ex.DisableMods(files)
		if len(done) == 0 {
			msg := "Не вдалося вимкнути жодного мода"
			if len(skipped) > 0 {
				msg += ": " + strings.Join(head(skipped, 4), ", ")
			}
			return fail("%s", msg)
		}
		msg := fmt.Sprintf("Вимкнено модів: %d", len(done))
		if len(skipped) > 0 {
			msg += "; пропущено: " + strings.Join(head(skipped, 4), ", ")
		}
		return ok(msg)

	case "install_dependencies":
		var ids []string
		for _, id := range head(stringList(params["ids"]), maxFilesPerFix) {
			if depIDRe.MatchString(id) {
				ids = append(ids, id)
			}
		}
		installed, failed := ex.InstallDependencies(ids)
		if len(installed) == 0 {
			return fail("Не вдалося знайти залежності в каталозі: %s. Встановіть їх вручну на вкладці «Моди».", strings.Join(head(failed, 6), ", "))
		}
		msg := "Встановлено: " + strings.Join(installed, ", ")
		if len(failed) > 0 {
			msg += "; не знайдено: " + strings.Join(failed, ", ")
		}
		return ok(msg)

	case "set_memory":
		mb := intParam(params["memoryMb"])
		total := ex.TotalRAMMB()
		if mb < 512 || (total > 0 && mb > total) {
			return fail("Неприпустимий обсяг пам'яті")
		}
		if err := ex.SetMemory(mb); err != nil {
			return fail("%v", err)
		}
		return ok("Пам'ять збірки: " + humanMemory(mb))

	case "set_java":
		path, _ := params["javaPath"].(string)
		path = strings.TrimSpace(path)
		if path != "" {
			return fail("Ручний шлях до Java через лагодження не задається — лише повернення автовибору")
		}
		if err := ex.SetJava(""); err != nil {
			return fail("%v", err)
		}
		return ok("Java вибирається автоматично")

	case "install_java":
		major := intParam(params["major"])
		if major < 0 || major > 99 || (major != 0 && major < 8) {
			return fail("Некоректна версія Java")
		}
		if err := ex.InstallJava(major); err != nil {
			return fail("%v", err)
		}
		if major == 0 {
			return ok("Java для цієї збірки встановлено")
		}
		return ok(fmt.Sprintf("Java %d встановлено", major))

	case "reset_jvm_arguments":
		if err := ex.ResetJVMArguments(); err != nil {
			return fail("%v", err)
		}
		return ok("Власні аргументи JVM збірки скинуто")

	case "set_dedicated_gpu":
		enabled, isBool := params["enabled"].(bool)
		if !isBool {
			return fail("Параметри лагодження мають неочікуваний формат")
		}
		if err := ex.SetDedicatedGPU(enabled); err != nil {
			return fail("%v", err)
		}
		if enabled {
			return ok("Гра запускатиметься на дискретній відеокарті")
		}
		return ok("Примусовий вибір дискретної відеокарти вимкнено")

	case "backup_configs":
		var files []string
		for _, f := range head(stringList(params["files"]), maxFilesPerFix) {
			if strings.HasPrefix(strings.ReplaceAll(f, "\\", "/"), "config/") {
				files = append(files, f)
			}
		}
		moved, err := ex.BackupConfigs(files)
		if err != nil {
			return fail("Помилка файлової системи: %v", err)
		}
		if len(moved) == 0 {
			return fail("Конфіги не знайдено — можливо, їх уже перестворено")
		}
		return ok("Перенесено в резервну теку: " + strings.Join(head(moved, 6), ", "))

	case "repair_version":
		if err := ex.RepairVersion(); err != nil {
			return fail("%v", err)
		}
		return ok("Версію та завантажувач відновлено")

	case "update_loader":
		minimum, _ := params["minimum"].(string)
		minimum = strings.TrimSpace(minimum)
		if minimum != "" && !loaderVersionRe.MatchString(minimum) {
			return fail("Некоректна версія завантажувача")
		}
		installed, err := ex.UpdateLoader(minimum)
		if err != nil {
			return fail("%v", err)
		}
		if installed == "" {
			return ok("Завантажувач перевстановлено")
		}
		return ok("Завантажувач встановлено: " + installed)

	case "set_account":
		id, _ := params["accountId"].(string)
		if !accountIDRe.MatchString(id) {
			return fail("Некоректний акаунт")
		}
		name, err := ex.SetAccount(id)
		if err != nil {
			return fail("%v", err)
		}
		if name == "" {
			name = id
		}
		return ok("Активний акаунт: " + name)

	case "login_microsoft":
		// Вхід Microsoft інтерактивний: його відкриває інтерфейс після застосування.
		return FixResult{OK: true, NeedsLogin: true, Message: "Відкриється вхід Microsoft — підтвердіть його у вікні"}
	}
	return fail("Невідоме лагодження: %s", action)
}

func intParam(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return -1
		}
		return int(x)
	}
	return -1
}
