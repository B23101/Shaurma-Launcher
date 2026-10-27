package minecraft

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"shaurma-launcher-backend/internal/prismmeta"
	"shaurma-launcher-backend/internal/versions"
)

// ── Встановлення лоадерів (Fabric / Quilt / Forge / NeoForge) ────────────
//
// Портуємо MinecraftInstaller.java старого лаунчера (архітектура PolyMC):
//   - Fabric/Quilt: мета-ендпоінт віддає готовий loader-профіль (version.json
//     з InheritsFrom) — завантажуємо і кладемо у versions/<id>.json, далі
//     EnsureVersion качає батьківську ванилла-версію та всі libraries.
//   - Forge/NeoForge: качаємо installer.jar, витягуємо version.json (може
//     лежати в install_profile.json → json/versionInfo або в корені архіву),
//     розкладаємо bundled maven-артефакти у libraries/ та запускаємо
//     processors (client-side) через Java.
//
// Всі файли живуть у СПІЛЬНІЙ базі <dataDir>/minecraft (Installer.baseDir),
// версії-лоадери мають InheritsFrom → ванилла, класшлях/асети рахуються
// через ланцюг (BuildClassPath / ResolveClientJar).

// Fallback URLs для кожного лоадера (пробуємо по черзі при помилках).
// Захист від зміни посилань, тимчасових збоїв, блокувань.
var (
	fabricMetaURLs = []string{
		"https://meta.fabricmc.net/v2/versions/loader/",
		"https://meta.fabricmc.net/v1/versions/loader/", // Старий API (fallback)
	}
	quiltMetaURLs = []string{
		"https://meta.quiltmc.org/v3/versions/loader/",
		"https://meta.quiltmc.org/v2/versions/loader/", // Старий API (fallback)
	}
	forgeMavenURLs = []string{
		"https://maven.minecraftforge.net/net/minecraftforge/forge/",
		"https://files.minecraftforge.net/maven/net/minecraftforge/forge/", // Старий домен
	}
)

const (
	// Основні URLs (для зворотної сумісності з існуючим кодом)
	forgeMaven = "https://maven.minecraftforge.net/net/minecraftforge/forge/"
	neoMaven   = "https://maven.neoforged.net/releases/net/neoforged/neoforge/"
	// neoMavenForge — артефакт "forge" в репо neoforged: сюди публікуються
	// версії NeoForge для Minecraft 1.20.1 (47.x — тоді NeoForge був форком
	// Forge і мав mc-префіксовані версії 1.20.1-47.1.106).
	neoMavenForge = "https://maven.neoforged.net/releases/net/neoforged/forge/"
	fabricMeta    = "https://meta.fabricmc.net/v2/versions/loader/"
	quiltMeta     = "https://meta.quiltmc.org/v3/versions/loader/"

	// *MetaVersions — той самий базовий шлях без кінцевого сегмента
	// профілю: GET <base><mcVersion> повертає впорядкований (найновіша
	// перша) список версій лоадера з полем "stable" — звідси
	// resolveRecommendedLoaderVersion бере рекомендовану версію, так само
	// як Prism (BaseVersionList::RecommendedRole).
	fabricMetaVersions = fabricMeta
	quiltMetaVersions  = quiltMeta
)

// LoaderVersionID повертає ID версії-профілю лоадера у спільній базі
// (versions/<id>.json). Для vanilla — це сама версія Minecraft.
// Для Fabric/Quilt формат: fabric-loader-<loaderVer>-<mc> (як у мети);
// для Forge/NeoForge — ID береться з version.json інсталера:
//
//	Forge           -> "<mc>-forge-<loaderVersion>"   (1.20.1-forge-47.4.10)
//	NeoForge (1.20.2+)-> "neoforge-<loaderVersion>"   (neoforge-21.1.233)
//	NeoForge (1.20.1) -> "<mc>-forge-<loaderVersion>" (1.20.1-forge-47.1.106 —
//	                    той самий Forge-формат, бо артефакт теж "forge").
func LoaderVersionID(loader, mcVersion, loaderVersion string) string {
	switch strings.ToLower(loader) {
	case "fabric":
		if loaderVersion == "" {
			return "fabric-loader-" + mcVersion
		}
		return fmt.Sprintf("fabric-loader-%s-%s", loaderVersion, mcVersion)
	case "quilt":
		if loaderVersion == "" {
			return "quilt-loader-" + mcVersion
		}
		return fmt.Sprintf("quilt-loader-%s-%s", loaderVersion, mcVersion)
	case "neoforge":
		if neoforgeUsesForgeArtifact(mcVersion, loaderVersion) {
			// NeoForge 1.20.1: профіль 1.20.1-forge-47.1.106 (артефакт forge)
			return mcVersion + "-forge-" + loaderVersion
		}
		return "neoforge-" + loaderVersion
	case "forge":
		return mcVersion + "-forge-" + loaderVersion
	default:
		return mcVersion
	}
}

// neoforgeUsesForgeArtifact визначає, чи версія NeoForge живе на Maven під
// артефактом "forge" (Minecraft 1.20.1, версії 47.x — NeoForge тоді був
// форком Forge і публікувався як net.neoforged:forge:<mc>-<ver>, напр.
// 1.20.1-47.1.106), а не сучасним артефактом "neoforge" (1.20.2+, версії
// 20.2.x/21.x — net.neoforged:neoforge:<ver> БЕЗ mc-префікса).
//
// Рішення приймається за версією лоадера (47.x існує ЛИШЕ для MC 1.20.1)
// з підстраховкою за mcVersion — так само розділяють ці лінійки й Prism meta.
func neoforgeUsesForgeArtifact(mcVersion, loaderVersion string) bool {
	if strings.HasPrefix(strings.TrimSpace(loaderVersion), "47.") {
		return true
	}
	return mcVersion == "1.20.1"
}

// EnsureLoader встановлює лоадер для збірки і повертає ID версії-профілю
// (той, що передається у LaunchConfig.Instance.MCVersion для запуску).
// Для vanilla повертає mcVersion без змін.
//
// ОНОВЛЕННЯ 2026-08-04: Додано валідацію існування версії перед встановленням
// (як у Prism Launcher: ComponentUpdateTask::loadComponent). Якщо версія не
// існує в мета-API — повертається зрозуміла помилка з переліком доступних
// версій, замість криптичного HTTP 404 при спробі завантажити installer.jar.
func (inst *Installer) EnsureLoader(loader, mcVersion, loaderVersion string) (string, error) {
	// Серіалізація з EnsureVersion/фоновими передзавантаженнями (див. runMu
	// у installer.go): спільний інстанс може одночасно обслуговувати запуск
	// і фонове передзавантаження «після створення збірки».
	inst.runMu.Lock()
	defer inst.runMu.Unlock()
	// Валідація існування версії ПЕРЕД встановленням (Prism-стиль) — АЛЕ
	// лише коли лоадер реально встановлюється. Для вже встановленого
	// (профіль на диску) мережева перевірка мета-API — зайвий HTTP-запит з
	// ретраями/фолбеками на ГАРЯЧОМУ шляху кожного запуску гри (кнопка
	// «Грати» «висіла» секунди, бо валідація ганяла meta.fabricmc.net /
	// Prism-meta / Maven навіть коли лоадер давно стоїть). Встановлений
	// лоадер не міг би існувати, якби його версії не було в меті —
	// перевірка потрібна лише на шляху НОВОГО встановлення.
	if !inst.loaderProfileInstalled(loader, mcVersion, loaderVersion) {
		if err := inst.validator.ValidateLoaderVersion(loader, mcVersion, loaderVersion); err != nil {
			return "", fmt.Errorf("валідація версії лоадера: %w", err)
		}
	}

	switch strings.ToLower(loader) {
	case "vanilla", "":
		return mcVersion, nil
	case "fabric":
		loaderVersion = inst.resolveRecommendedLoaderVersion(fabricMetaVersions, "fabric", mcVersion, loaderVersion)
		return inst.installMetaLoader(fabricMeta, "fabric", mcVersion, loaderVersion)
	case "quilt":
		loaderVersion = inst.resolveRecommendedLoaderVersion(quiltMetaVersions, "quilt", mcVersion, loaderVersion)
		return inst.installMetaLoader(quiltMeta, "quilt", mcVersion, loaderVersion)
	case "forge":
		return inst.installForge(mcVersion, loaderVersion, false)
	case "neoforge":
		return inst.installForge(mcVersion, loaderVersion, true)
	default:
		return "", fmt.Errorf("непідтримуваний лоадер: %s", loader)
	}
}

// loaderProfileInstalled повертає true, якщо профіль лоадера вже
// встановлений у спільній базі (versions/<id>/<id>.json існує на диску).
// Використовується, щоб НЕ ходити в мережу за валідацією версії, коли
// встановлювати нічого: прогрітий запуск гри не повинен залежати від
// доступності мета-API (див. EnsureLoader).
func (inst *Installer) loaderProfileInstalled(loader, mcVersion, loaderVersion string) bool {
	switch strings.ToLower(loader) {
	case "vanilla", "":
		// Ванілла не має окремого профілю лоадера — валідація для неї і
		// так миттєва (ValidateLoaderVersion повертає nil одразу).
		return false
	case "fabric", "quilt":
		// ID профілю на диску детермінований: fabric-loader-<ver>-<mc> / quilt-loader-<ver>-<mc>.
		id := LoaderVersionID(loader, mcVersion, loaderVersion)
		if id == "" {
			return false
		}
		_, err := os.Stat(filepath.Join(inst.baseDir, "versions", id, id+".json"))
		return err == nil
	case "forge", "neoforge":
		_, ok := inst.checkForgeInstalled(mcVersion, loaderVersion, loader == "neoforge")
		return ok
	}
	return false
}

// resolveRecommendedLoaderVersion повертає версію лоадера, яку СПРАВДІ
// слід встановити.
//
// ПОВАЖАЄ КОНФІГ НАПОВНО: якщо в даних збірки (Shaurma/кастомний пак)
// задана конкретна версія — повертаємо її як є, БЕЗ будь-якого перезапису
// на "рекомендовану". Юзер сам вирішує, який лоадер: зміни версії гри,
// лоадера або самої версії лоадера у формі збірки мають застосовуватись
// на наступному запуску напряму, а не бути скасовані лаунчером.
//
// РАНІШЕ тут була "розумна" логіка: завжди питати мета-API за реальною
// рекомендованою версією і перекреслювати задану. Це було причиною багу —
// юзер ставив у збірці нову версію лоадера (напр. quilt 0.30.1-beta.2),
// а запуск завжди брав стару 0.20.0-beta.9: у відповіді Quilt-мети немає
// поля "stable", тому код не знаходив нічого "рекомендованого" і падав на
// entries[0] — перший-ліпший запис списку мети, який і був тією старою
// версією. Це ламало зміну версії лоадера/гри у збірках повністю.
//
// "Рекомендована" вважається ЛИШЕ як фолбек, коли версію не задано взагалі
// або задано маркерами "recommended"/"latest": обирається найновіша
// стабільна за FlexVer (fallback URLs та retry — від 2026-08-03).
func (inst *Installer) resolveRecommendedLoaderVersion(metaVersionsBase, loaderName, mcVersion, loaderVersion string) string {
	// ЗАВЖДИ поважаємо те, що реально задане у конфігурації збірки. Якщо
	// версія задана конкретно — повертаємо її і НЕ ходимо в мету взагалі
	// (це усуває мережеві залежності на критичному шляху запуску).
	concrete := strings.TrimSpace(loaderVersion)
	if concrete != "" && concrete != "recommended" && concrete != "latest" {
		return concrete
	}

	// Визначаємо список fallback URLs
	var urls []string
	switch loaderName {
	case "fabric":
		urls = fabricMetaURLs
	case "quilt":
		urls = quiltMetaURLs
	default:
		urls = []string{metaVersionsBase}
	}

	// Завантажуємо з fallback та retry
	body, err := inst.fetchURLWithFallback(urls, mcVersion, 3)
	if err != nil {
		inst.log(fmt.Sprintf("Не вдалось отримати версії %s для %s (%v) — використовую задану: %q", loaderName, mcVersion, err, concrete))
		return concrete
	}

	var entries []struct {
		Loader struct {
			Version string `json:"version"`
		} `json:"loader"`
	}
	if err := json.Unmarshal(body, &entries); err != nil || len(entries) == 0 {
		inst.log(fmt.Sprintf("Порожній/невалідний список версій %s для %s — використовую задану: %q", loaderName, mcVersion, concrete))
		return concrete
	}

	// Серед доступних обираємо найновішу СТАБІЛЬНУ за FlexVer; якщо
	// стабільних немає — просто найновішу. Не покладаємося на порядок
	// записів у відповіді мети (metа може віддавати невпорядковано).
	best, bestStable := "", false
	for _, e := range entries {
		v := e.Loader.Version
		if v == "" {
			continue
		}
		stable := prismmeta.IsStableVersion(v)
		if best == "" {
			best, bestStable = v, stable
			continue
		}
		if stable == bestStable {
			if versions.Compare(v, best) > 0 {
				best = v
			}
		} else if stable {
			best, bestStable = v, true
		}
	}
	if best == "" {
		return concrete
	}
	inst.log(fmt.Sprintf("Версію %s для %s не задано — обрано: %s", loaderName, mcVersion, best))
	return best
}

// installMetaLoader — спільний шлях для Fabric/Quilt: мета-ендпоінт віддає
// готовий loader-профіль (JSON з id, inheritsFrom, libraries, mainClass).
// Версія лоадера на цей момент вже пройшла через
// resolveRecommendedLoaderVersion (EnsureLoader) — сюди приходить те, що
// реально треба встановити.
//
// ОНОВЛЕННЯ 2026-08-03: Додано fallback URLs та retry логіку.
// ОНОВЛЕННЯ 2026-08-17: Ранній вихід, якщо профіль уже встановлений —
// мета-профіль ідемпотентний (versions/<id>/<id>.json + ensureVersion), тож
// повторне якання того самого JSON з мети при КОЖНОМУ запуску гри — зайва
// мережева залежність на критичному шляху (аналог checkForgeInstalled для
// Forge нижче).
func (inst *Installer) installMetaLoader(metaBase, loader, mcVersion, loaderVersion string) (string, error) {
	// ID профілю детермінований (LoaderVersionID) — якщо json уже на диску,
	// лоадер встановлений: пропускаємо і мережевий запит профілю, і його
	// перезапис, лишаючись лише на докачці відсутніх компонентів (EnsureVersion).
	profileID := LoaderVersionID(loader, mcVersion, loaderVersion)
	profilePath := filepath.Join(inst.baseDir, "versions", profileID, profileID+".json")
	if _, err := os.Stat(profilePath); err == nil {
		inst.log(fmt.Sprintf("%s %s вже встановлено (профіль %s), пропускаю завантаження профілю.", loader, loaderVersion, profileID))
		// ensureVersionStart (БЕЗ runMu): EnsureLoader уже тримає runMu на цій
		// горутині, а sync.Mutex не реентрантний — публічний EnsureVersion тут
		// завис би намертво. Докачує батька (vanilla) і бібліотеки, якщо чогось
		// бракує; на прогрітому запуску — майже миттєво (stat-перевірки).
		if _, err := inst.ensureVersionStart(profileID); err != nil {
			return "", fmt.Errorf("ensure %s %s: %w", loader, profileID, err)
		}
		return profileID, nil
	}

	// Визначаємо список fallback URLs
	var urls []string
	switch loader {
	case "fabric":
		urls = fabricMetaURLs
	case "quilt":
		urls = quiltMetaURLs
	default:
		urls = []string{metaBase}
	}

	suffix := mcVersion + "/" + loaderVersion + "/profile/json"
	inst.log(fmt.Sprintf("Завантаження %s-профілю %s для Minecraft %s...", loader, loaderVersion, mcVersion))
	inst.reportInstallProgress("loader", 3, fmt.Sprintf("Завантаження %s-профілю %s...", loader, loaderVersion))

	body, err := inst.fetchURLWithFallback(urls, suffix, 3)
	if err != nil {
		return "", fmt.Errorf("%s profile: %w", loader, err)
	}
	inst.reportInstallProgress("loader", 5, fmt.Sprintf("%s-профіль отримано, встановлення Minecraft...", loader))

	var profile VersionJSON
	if err := json.Unmarshal(body, &profile); err != nil {
		return "", fmt.Errorf("парсинг %s профілю: %w", loader, err)
	}
	if profile.ID == "" {
		return "", fmt.Errorf("%s профіль без id", loader)
	}

	// Зберігаємо профіль у спільну базу, далі EnsureVersion докачає батька
	// і libraries.
	vDir := filepath.Join(inst.baseDir, "versions", profile.ID)
	if err := os.MkdirAll(vDir, 0755); err != nil {
		return "", err
	}
	data, _ := json.MarshalIndent(profile, "", "  ")
	if err := os.WriteFile(filepath.Join(vDir, profile.ID+".json"), data, 0644); err != nil {
		return "", err
	}

	// ensureVersionStart (БЕЗ runMu): EnsureLoader уже тримає runMu на цій
	// горутині, а sync.Mutex не реентрантний — публічний EnsureVersion тут
	// завис би намертво.
	if _, err := inst.ensureVersionStart(profile.ID); err != nil {
		return "", fmt.Errorf("ensure %s %s: %w", loader, profile.ID, err)
	}
	inst.log(fmt.Sprintf("%s %s встановлено.", loader, loaderVersion))
	return profile.ID, nil
}

// installForge встановлює Forge або NeoForge (neoforge=true) через офіційний
// installer.jar: version.json, bundled maven jars, processors.
//
// Портовано напряму з логіки старого (Java) лаунчера
// (MinecraftInstaller.installForge/installNeoForge + extractForgeMetadata):
// той самий порядок кроків (installer.jar → version.json/install_profile.json
// → bundled maven jars → processors), та сама обробка помилок процесорів з
// повторним завантаженням бібліотек при невдачі. Файлова структура (спільна
// база versions/libraries замість per-instance тек) лишається Go-архітектурою
// нового лаунчера — портується лише алгоритм.
func (inst *Installer) installForge(mcVersion, loaderVersion string, neoforge bool) (string, error) {
	name := "forge"
	if neoforge {
		name = "neoforge"
	}

	// Ранній вихід, якщо лоадер УЖЕ встановлений — портовано з
	// MinecraftInstaller.ensureInstalled старого лаунчера
	// (mcInstalled && loaderInstalled → install-крок повністю
	// пропускається). Без цієї перевірки installForge виконувала б ВЕСЬ
	// install-шлях (розпаковка installer.jar, парсинг
	// install_profile.json, прохід по всіх processors) при КОЖНОМУ
	// запуску гри, бо EnsureLoader викликається з LaunchInstance на
	// кожен старт, а не лише один раз при першому встановленні. Кожен
	// processor усередині runForgeProcessors і так пропускає себе, якщо
	// outputs вже валідні, — але сам прохід (відкрити installer.jar,
	// прочитати install_profile.json, застатити купу файлів на диску,
	// перевірити sha1) відбувався б щоразу, що і є зайвою роботою при
	// звичайному запуску вже встановленої збірки.
	if profileID, ok := inst.checkForgeInstalled(mcVersion, loaderVersion, neoforge); ok {
		inst.log(fmt.Sprintf("%s %s вже встановлено (профіль %s), пропускаю install.", name, loaderVersion, profileID))
		return profileID, nil
	}

	// NeoForge-версії вже містять mc-версію у loaderVersion (напр. 21.1.95),
	// тому повний ID складаємо так само: mcVersion + "-" + loaderVersion.
	// Але перевіряємо фактичний ID з version.json інсталера — він авторитетний.
	mavenBase := forgeMaven
	if neoforge {
		mavenBase = neoMaven
	}
	fullVer := mcVersion + "-" + loaderVersion

	// Список КАНДИДАТІВ installer URL, пробуємо по черзі (перший успішний).
	// БАГ (виправлено): Forge для старих версій Minecraft (1.8.9, 1.7.10)
	// публікує maven-артефакти під «подвійним» ім'ям версії з суфіксом MC:
	//   net/minecraftforge/forge/1.8.9-11.15.1.2318-1.8.9/
	//       forge-1.8.9-11.15.1.2318-1.8.9-installer.jar  (існує — 200)
	// а НЕ під звичайним
	//   net/minecraftforge/forge/1.8.9-11.15.1.2318/
	//       forge-1.8.9-11.15.1.2318-installer.jar        (404!)
	// Тому пробуємо обидва формати — так само, як Prism Launcher будує URL
	// з повного maven-імені версії зі своєї мети (meta.prismlauncher.org
	// для 1.8.9 віддає саме 1.8.9-11.15.1.2318-1.8.9).
	var installerURLs []string
	if neoforge {
		if neoforgeUsesForgeArtifact(mcVersion, loaderVersion) {
			// NeoForge 1.20.1 (47.x) публікується під артефактом "forge" з
			// mc-префіксом версії, тому шлях ідентичний Forge, але в репо
			// neoforged:
			//   net/neoforged/forge/1.20.1-47.1.106/forge-1.20.1-47.1.106-installer.jar
			// БАГ (виправлено): раніше завжди будувався шлях сучасного
			// артефакту neoforge (net/neoforged/neoforge/47.1.106/
			// neoforge-47.1.106-installer.jar), якого для 1.20.1 не існує —
			// звідси і був "download neoforge installer: HTTP 404".
			installerURLs = append(installerURLs,
				neoMavenForge+fullVer+"/forge-"+fullVer+"-installer.jar")
		} else {
			// NeoForge 1.20.2+: артефакт "neoforge" без mc-префікса:
			//   net/neoforged/neoforge/<ver>/neoforge-<ver>-installer.jar
			installerURLs = append(installerURLs,
				mavenBase+loaderVersion+"/neoforge-"+loaderVersion+"-installer.jar")
		}
	} else {
		installerURLs = append(installerURLs,
			mavenBase+fullVer+"/forge-"+fullVer+"-installer.jar",
			mavenBase+fullVer+"-"+mcVersion+"/forge-"+fullVer+"-"+mcVersion+"-installer.jar")
	}

	inst.log(fmt.Sprintf("Завантаження %s installer...", name))
	inst.reportInstallProgress("loader", 5, fmt.Sprintf("Завантаження %s installer...", name))
	installerPath := filepath.Join(inst.baseDir, "installers", name+"-"+fullVer+".jar")
	if err := inst.downloadIfMissingAny(installerURLs, installerPath); err != nil {
		return "", fmt.Errorf("download %s installer: %w", name, err)
	}
	inst.reportInstallProgress("loader", 15, "Інсталер завантажено — розпаковка...")

	inst.log("Витягування метаданих " + name + "...")
	profileID, err := inst.extractForgeMetadata(installerPath, mcVersion, loaderVersion, neoforge)
	if err != nil {
		return "", err
	}
	inst.reportInstallProgress("loader", 30, "Запуск процессорів Forge...")
	inst.log(fmt.Sprintf("%s %s встановлено (профіль %s).", name, loaderVersion, profileID))
	return profileID, nil
}

// checkForgeInstalled перевіряє, чи лоадер (Forge/NeoForge) для цієї
// mc+loaderVersion уже повністю встановлений — щоб installForge міг вийти
// рано і не проходити install_profile.json/processors при кожному запуску
// гри. Портовано з MinecraftInstaller.checkForgeInstalled старого
// лаунчера, адаптовано під архітектуру спільної бази versions/libraries
// (замість per-instance forge-version.json перевіряємо versions/<id>.json,
// а замість librariesDir().resolve(mcVersion) — спільну libraries/).
//
// Повертає (profileID, true), якщо версійний профіль на диску вже є і
// (де застосовно) клієнтський jar лоадера в libraries теж на місці —
// тобто runForgeProcessors точно відпрацював раніше і install можна
// пропустити цілком.
func (inst *Installer) checkForgeInstalled(mcVersion, loaderVersion string, neoforge bool) (string, bool) {
	// Профіль на диску зберігається під ID інсталера (versions/<id>/, де id —
	// з version.json інсталера), а НЕ у форматі "mcVersion-loaderVersion":
	//   Forge           -> "<mc>-forge-<loaderVersion>" (напр. 1.20.1-forge-47.4.10)
	//   NeoForge 1.20.2+ -> "neoforge-<loaderVersion>"  (напр. neoforge-21.1.233)
	//   NeoForge 1.20.1  -> "<mc>-forge-<loaderVersion>" (напр. 1.20.1-forge-47.1.106)
	// БАГ (виправлено): раніше тут підставлявся LoaderVersionID(...) =
	// "mc-loaderVersion" (1.20.1-47.4.10 / 1.21.1-21.1.233), якого на диску
	// ніколи не було — перевірка завжди провалювалась, і Forge/NeoForge
	// перезапускали ВЕСЬ install-шлях (розпаковка installer.jar, перевірка
	// всіх бібліотек/асетів і важкі processors) при КОЖНОМУ запуску гри.
	// Шукаємо реальний профіль серед versions/, щоб не покладатись на
	// точний mcVersion з даних збірки.
	entries, err := os.ReadDir(filepath.Join(inst.baseDir, "versions"))
	if err != nil {
		return "", false
	}
	var profileID string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if neoforge {
			if neoforgeUsesForgeArtifact(mcVersion, loaderVersion) {
				// NeoForge 1.20.1: профіль має Forge-формат
				// "<mc>-forge-<loaderVersion>" (1.20.1-forge-47.1.106), бо
				// артефакт теж "forge" — скануємо за суфіксом, як і Forge.
				if strings.HasSuffix(name, "-forge-"+loaderVersion) {
					profileID = name
					break
				}
			} else {
				// ID профілю NeoForge 1.20.2+ ЗАВЖДИ рівно
				// "neoforge-<loaderVersion>" (без mc-префікса) — точний збіг,
				// щоб не чіпати інші версії.
				if name == "neoforge-"+loaderVersion {
					profileID = name
					break
				}
			}
		} else if strings.HasSuffix(name, "-forge-"+loaderVersion) {
			// Forge: "<mc>-forge-<loaderVersion>" — префікс-частину (mc)
			// скануємо за суфіксом, щоб не залежати від точності mcVersion
			// у даних збірки.
			profileID = name
			break
		} else if isOldForgeProfileID(name, mcVersion, loaderVersion) {
			// СТАРІ Forge (1.8.9/1.7.10): ID іншого формату — mc і версія
			// вклеєні БЕЗ дефіса після "forge":
			//   "1.8.9-forge1.8.9-11.15.1.2318-1.8.9" (з mc-суфіксом maven)
			//   "1.8.9-forge1.8.9-11.15.0.1669"        (без суфікса)
			// Раніше тут шукали лише "-forge-<ver>", тож старий профіль
			// ніколи не знаходився і install перезапускався при КОЖНОМУ
			// запуску гри.
			profileID = name
			break
		}
	}
	if profileID == "" {
		return "", false
	}

	versionJSONPath := filepath.Join(inst.baseDir, "versions", profileID, profileID+".json")
	data, err := os.ReadFile(versionJSONPath)
	if err != nil {
		return "", false
	}

	var vj VersionJSON
	if err := json.Unmarshal(data, &vj); err != nil {
		return "", false
	}
	if !vj.IsLoader() {
		// Профіль на диску є, але це не лоадер-версія (немає inheritsFrom) —
		// щось не так із записом, безпечніше перевстановити.
		return "", false
	}

	// Forge ↔ NeoForge 1.20.1: обидві лінійки зберігають профілі під
	// однаковим форматом імен "<mc>-forge-<version>" (версії 47.x у них
	// перетинаються — напр. 47.1.106 існує і в Forge, і в NeoForge), тож
	// сканування за суфіксом може знайти профіль ЧУЖОЇ лінійки і хибно
	// вирішити, що лоадер вже встановлений (далі запустилась би гра з
	// неправильним лоадером). Розрізняємо за вмістом version.json: профіль
	// NeoForge несе бібліотеки net.neoforged.fancymodloader:* (fmlcore/
	// fmlloader), яких у звичайного Forge немає.
	needsDisambiguation := !neoforge || neoforgeUsesForgeArtifact(mcVersion, loaderVersion)
	if needsDisambiguation {
		isNeo := vjHasNeoForgeLibs(&vj)
		if neoforge && !isNeo {
			// Знайдено звичайний Forge-профіль із тим самим ім'ям — це не
			// наш NeoForge-лоадер, перевстановлюємо.
			return "", false
		}
		if !neoforge && isNeo {
			// Знайдено NeoForge 1.20.1-профіль, а запитують Forge.
			return "", false
		}
	}

	// Реальна MC-версія, від якої успадковує лоадер — авторитетне джерело
	// (як inheritsFrom у forge-version.json старого лаунчера), а не голий
	// параметр mcVersion, який міг бути неточним у даних збірки.
	realMcVersion := vj.InheritsFrom
	if realMcVersion == "" {
		realMcVersion = mcVersion
	}

	// Шлях client.jar у спільній libraries/ залежить від лоадера:
	//   Forge           -> net/minecraftforge/forge/<mc>-<lv>/forge-<mc>-<lv>-client.jar
	//   NeoForge 1.20.2+ -> net/neoforged/neoforge/<lv>/neoforge-<lv>-client.jar
	//   NeoForge 1.20.1  -> net/neoforged/forge/<mc>-<lv>/forge-<mc>-<lv>-client.jar
	// (у NeoForge 1.20.2+ maven-тека — це сам loaderVersion БЕЗ mc-префікса;
	// у 1.20.1 — mc-префіксована тека артефакту "forge", куди processors
	// інсталера пишуть PATCHED client jar. БАГ: раніше для neoforge завжди
	// будувався шлях сучасного артефакту, тож для 1.20.1 навіть знайдений
	// профіль не проходив перевірку client.jar.)
	var clientJar string
	if neoforge {
		if neoforgeUsesForgeArtifact(realMcVersion, loaderVersion) {
			fullVer := realMcVersion + "-" + loaderVersion
			clientJar = filepath.Join(inst.baseDir, "libraries", "net", "neoforged", "forge", fullVer, "forge-"+fullVer+"-client.jar")
		} else {
			clientJar = filepath.Join(inst.baseDir, "libraries", "net", "neoforged", "neoforge", loaderVersion, "neoforge-"+loaderVersion+"-client.jar")
		}
	} else {
		fullVer := realMcVersion + "-" + loaderVersion
		clientJar = filepath.Join(inst.baseDir, "libraries", "net", "minecraftforge", "forge", fullVer, "forge-"+fullVer+"-client.jar")
	}
	if fi, err := os.Stat(clientJar); err != nil || fi.Size() == 0 {
		// Не всі Forge-версії генерують forge-*-client.jar:
		//   · 1.8.9/1.7.10 — клієнтом є universal jar (version.json має
		//     "jar": mcVersion — сам client.jar лишається ванилла);
		//   · 1.12.2 — jar бандлиться БЕЗ classifier (forge-1.12.2-...jar)
		//     і версія.json вказує на нього через downloads.artifact.path.
		// У всіх таких випадках патчений jar лоадера лежить у libraries/ за
		// шляхом, який задає САМ version.json (resolveLibraryArtifact записав
		// його в downloads.artifact.path при встановленні). Шукаємо цей шлях
		// серед бібліотек профілю — так install вважається завершеним тоді,
		// коли jar лоадера реально на місці (як Prism перевіряє наявність
		// патченого jar), незалежно від формату (client/universal/без
		// classifier). Раніше fallback покривав лише старий формат через
		// хевристику oldForgeUniversalJar і для 1.12.2 перевстановлював
		// лоадер при КОЖНОМУ запуску гри.
		for i := range vj.Libraries {
			lib := &vj.Libraries[i]
			if !isForgeArtifactName(lib.Name) {
				continue
			}
			p := lib.Downloads.Artifact.Path
			if p == "" {
				continue
			}
			jarPath := filepath.Join(inst.baseDir, "libraries", filepath.FromSlash(p))
			if fi, err := os.Stat(jarPath); err == nil && fi.Size() > 0 {
				return profileID, true
			}
		}
		return "", false
	}
	// Санітарна перевірка вмісту, а не лише розміру: битий/обірваний
	// jar (напр. процес завершився під час запису processor'ом) може
	// мати ненульовий розмір, але бути невалідним zip-архівом. Без
	// цієї перевірки checkForgeInstalled сприймав такий файл як "усе
	// встановлено", запуск гри падав на завантаженні класів, а install
	// ніколи не перезапускався, щоб полагодити output. zip.OpenReader
	// тут дешевий (лише читає центральний каталог, не розпаковує).
	if zr, err := zip.OpenReader(clientJar); err != nil {
		return "", false
	} else {
		zr.Close()
	}
	return profileID, true
}

// isOldForgeProfileID — чи ID профілю належить СТАРОМУ формату Forge
// (1.8.9/1.7.10). Сучасний формат має "forge" одразу за яким іде дефіс:
// "<mc>-forge-<ver>" (1.20.1-forge-47.4.10). Старий формат вклеює версію
// БЕЗ дефіса: "1.8.9-forge1.8.9-11.15.1.2318-1.8.9" або
// "1.7.10-Forge10.13.4.1614-1.7.10" (регістр "Forge" різний).
// Відрізняємо за символом ПІСЛЯ "forge": цифра = старий, дефіс = новий.
func isOldForgeProfileID(name, mcVersion, loaderVersion string) bool {
	if name == "" || loaderVersion == "" {
		return false
	}
	lower := strings.ToLower(name)
	idx := strings.Index(lower, "forge")
	if idx < 0 {
		return false
	}
	rest := lower[idx+len("forge"):]
	if rest == "" || rest[0] == '-' {
		// "forge-" — сучасний формат (1.20.1-forge-47.4.10) або
		// підрядок "neoforge-21.1.233" — не старий Forge.
		return false
	}
	// Старий формат: після "forge" одразу йде версія лоадера (або mc+версія).
	return strings.Contains(rest, loaderVersion)
}

// vjHasNeoForgeLibs визначає, чи version.json належить NeoForge, а не
// звичайному Forge: NeoForge 1.17+ несе бібліотеки
// net.neoforged.fancymodloader:* (fmlcore/fmlloader/language-*), яких у
// Forge-профілях немає. Використовується, щоб розрізнити профілі Forge і
// NeoForge 1.20.1, які мають однаковий формат імен ("<mc>-forge-<ver>").
func vjHasNeoForgeLibs(vj *VersionJSON) bool {
	if vj == nil {
		return false
	}
	for _, lib := range vj.Libraries {
		if strings.Contains(lib.Name, "net.neoforged.fancymodloader") {
			return true
		}
	}
	return false
}

// fmlForgeVersion витягує значення аргумента --fml.forgeVersion з
// game-аргументів профілю (напр. "37.0.0" для 1.17.1-forge-37.0.0).
func fmlForgeVersion(vjson *VersionJSON) string {
	if vjson == nil {
		return ""
	}
	for i := 0; i+1 < len(vjson.Arguments.Game); i++ {
		k, ok := vjson.Arguments.Game[i].(string)
		if !ok || k != "--fml.forgeVersion" {
			continue
		}
		if v, ok := vjson.Arguments.Game[i+1].(string); ok {
			return v
		}
	}
	return ""
}

// Примітка про FORGE_SPEC: ForgeVersion.<clinit> (ForgeVersion.java:47-48)
// читає spec так:
//
//	spec = JarVersionLookupHandler.getSpecificationVersion(ForgeVersion.class)
//	         .orElse(System.getenv("FORGE_SPEC"));
//	if (spec == null) throw new RuntimeException("Missing forge spec, cannot continue");
//
// getSpecificationVersion = Class.getPackage().getSpecificationVersion(), що
// бере значення з MANIFEST.MF-секції пакета net/minecraftforge/versions/forge/.
// Це працює для бібліотек зі звичайного classpath (-cp), АЛЕ в bootstrap-
// форматах Forge 1.17+ (bootstraplauncher) universal jar та fmlcore/
// javafmllanguage/mclanguage потрапляють у legacy classpath
// (-DlegacyClassPath.file, ignoreList), і їх класи завантажуються через
// securejarhandler БЕЗ прокидання manifest-секцій у Package — тому
// getSpecificationVersion повертає null, попри те, що секція в jar-і є.
// Forge передбачив fallback на FORGE_SPEC; ми його і задаємо при запуску
// (див. forgeSpecEnv у launcher.go). Universal jar 37.0.0 патчити НЕ треба —
// секція там на місці (sha1 збігається з офіційним maven артефактом).

// extractForgeMetadata читає installer.jar: знаходить version.json (через
// install_profile.json → json/versionInfo або в корені архіву), зберігає
// профіль у versions/<id>.json, розкладає bundled maven jars у libraries/,
// запускає processors. Повертає ID профілю (versions/<id>.json).
func (inst *Installer) extractForgeMetadata(installerPath, mcVersion, loaderVersion string, neoforge bool) (string, error) {
	zr, err := zip.OpenReader(installerPath)
	if err != nil {
		return "", fmt.Errorf("open installer: %w", err)
	}
	defer zr.Close()

	// 1) Знаходимо version.json.
	var versionJSON []byte
	var profile *installProfile
	var profileJSON []byte
	for _, f := range zr.File {
		switch f.Name {
		case "install_profile.json":
			rc, err := f.Open()
			if err != nil {
				continue
			}
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				continue
			}
			profileJSON = data
			var ip installProfile
			if json.Unmarshal(data, &ip) == nil {
				profile = &ip
			}
		}
	}

	if profile != nil {
		// Формат Forge 1.21+ (Bootstrap): install_profile.json має поле
		// "json" — шлях до version.json усередині архіву.
		if profile.JSON != "" {
			for _, f := range zr.File {
				if f.Name == profile.JSON {
					rc, _ := f.Open()
					data, err := io.ReadAll(rc)
					rc.Close()
					if err == nil {
						versionJSON = data
					}
					break
				}
			}
		}
		// Формат 1.13–1.20: versionInfo всередині install_profile.json.
		if versionJSON == nil && profile.VersionInfo != nil {
			versionJSON, _ = json.Marshal(profile.VersionInfo)
		}
	}

	// Альтернатива: version.json у корені архіву.
	if versionJSON == nil {
		for _, f := range zr.File {
			if f.Name == "version.json" {
				rc, _ := f.Open()
				data, err := io.ReadAll(rc)
				rc.Close()
				if err == nil {
					versionJSON = data
				}
				break
			}
		}
	}

	if versionJSON == nil {
		return "", fmt.Errorf("не знайдено version.json у %s installer", map[bool]string{true: "NeoForge", false: "Forge"}[neoforge])
	}

	var vj VersionJSON
	if err := json.Unmarshal(versionJSON, &vj); err != nil {
		return "", fmt.Errorf("парсинг version.json: %w", err)
	}
	if vj.ID == "" {
		vj.ID = LoaderVersionID(map[bool]string{true: "neoforge", false: "forge"}[neoforge], mcVersion, loaderVersion)
	}

	// Старі Forge-профілі (1.8.9/1.7.10) у versionInfo задають forge-
	// бібліотеку БЕЗ classifier: "net.minecraftforge:forge:1.8.9-..." —
	// але на maven такий артефакт існує ЛИШЕ як -universal.jar (без
	// classifier jar немає: 404). Prism-meta для 1.8.9 віддає ту саму
	// бібліотеку з явним ":universal" — робимо те саме, щоб і
	// завантаження (resolveLibraryArtifact/gradlePath), і classpath
	// (BuildClassPath) вказували на файл, який реально існує.
	versionJSON = normalizeOldForgeLibraries(versionJSON)

	// Зберігаємо профіль у спільну базу.
	vDir := filepath.Join(inst.baseDir, "versions", vj.ID)
	if err := os.MkdirAll(vDir, 0755); err != nil {
		return "", err
	}
	os.WriteFile(filepath.Join(vDir, vj.ID+".json"), versionJSON, 0644)

	// 2) Розкладаємо bundled maven jars (maven/...) у libraries/.
	if err := inst.extractBundledJars(zr, vj.ID); err != nil {
		return "", err
	}

	// 3) Докачуємо/перевіряємо libraries лоадера через ensureVersionStart
	//    (це завантажить і батьківську ванилла-версію теж). Саме
	//    ensureVersionStart, бо runMu уже тримає EnsureLoader — публічний
	//    EnsureVersion зайняв би лок повторно і намертво завис.
	if _, err := inst.ensureVersionStart(vj.ID); err != nil {
		return "", fmt.Errorf("ensure %s: %w", vj.ID, err)
	}

	// 4) Запускаємо processors (client-side), якщо є install_profile.json.
	if profile != nil {
		if err := inst.runForgeProcessors(zr, installerPath, profile, profileJSON, vj.ID, mcVersion); err != nil {
			return "", err
		}
	}

	return vj.ID, nil
}

// normalizeOldForgeLibraries виправляє бібліотеки СТАРИХ Forge-профілів
// (1.8.9/1.7.10 та інших до 1.13): їх versionInfo задає forge-бібліотеку
// БЕЗ classifier — "net.minecraftforge:forge:1.8.9-11.15.1.2318-1.8.9" —
// але на maven такий артефакт не існує: файл називається
// forge-1.8.9-11.15.1.2318-1.8.9-universal.jar (класифікатор "universal").
//
// Prism-meta для 1.8.9 віддає цю ж бібліотеку з явним ":universal" —
// тут робимо те саме, щоб і завантаження (resolveLibraryArtifact →
// gradlePath), і classpath (BuildClassPath) вказували на файл, який
// реально існує, а не падали 404 на безкласифікаторному .jar.
//
// Пропускаємо бібліотеки з явним downloads.artifact.path — сучасні
// профілі (1.12.2+; NeoForge 1.20.1) бандлять свій forge-jar у maven/...
// під ТОЧНИМ шляхом з version.json, і чіпати його не можна.
func normalizeOldForgeLibraries(versionJSON []byte) []byte {
	var doc map[string]interface{}
	if err := json.Unmarshal(versionJSON, &doc); err != nil {
		return versionJSON
	}
	rawLibs, ok := doc["libraries"].([]interface{})
	if !ok {
		return versionJSON
	}
	changed := false
	for _, rl := range rawLibs {
		lib, ok := rl.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := lib["name"].(string)

		// СТАРІ Forge-профілі (1.8.9/1.7.10) у versionInfo задають частину
		// бібліотек БЕЗ url (напр. net.minecraft:launchwrapper:1.12,
		// lzma:lzma:0.0.1) — resolveLibraryArtifact тоді фолбечив би на
		// repo1.maven.org, де цих артефактів немає (404). Всі вони є на
		// libraries.minecraft.net — офіційному дзеркалі бібліотек Minecraft,
		// тому url задаємо явно (той самий дефолт, що в офіційного
		// лаунчера і Prism для бібліотек без репозиторію).
		if u, _ := lib["url"].(string); u == "" {
			if dl, ok := lib["downloads"].(map[string]interface{}); ok {
				if art, ok := dl["artifact"].(map[string]interface{}); ok {
					if p, _ := art["path"].(string); p == "" {
						lib["url"] = "https://libraries.minecraft.net"
						changed = true
					}
				} else {
					lib["url"] = "https://libraries.minecraft.net"
					changed = true
				}
			} else {
				lib["url"] = "https://libraries.minecraft.net"
				changed = true
			}
		}

		// Лише forge-артефакти: group=net.minecraftforge|net.neoforged,
		// artifact=forge|neoforge. Решту (launchwrapper, scala тощо) не
		// чіпаємо щодо classifier.
		if !isForgeArtifactName(name) {
			continue
		}
		// Вже має classifier (universal/installer/...) — не чіпаємо.
		if gradleClassifier(name) != "" {
			continue
		}
		// Явний downloads.artifact.path (сучасний профіль) — не чіпаємо.
		if dl, ok := lib["downloads"].(map[string]interface{}); ok {
			if art, ok := dl["artifact"].(map[string]interface{}); ok {
				if p, _ := art["path"].(string); p != "" {
					continue
				}
			}
		}
		lib["name"] = name + ":universal"
		changed = true
	}
	if !changed {
		return versionJSON
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return versionJSON
	}
	return out
}

// isForgeArtifactName — чи maven-координата є артефактом Forge/NeoForge
// (group net.minecraftforge або net.neoforged, artifact forge або neoforge).
func isForgeArtifactName(name string) bool {
	return strings.HasPrefix(name, "net.minecraftforge:forge:") ||
		strings.HasPrefix(name, "net.neoforged:forge:") ||
		strings.HasPrefix(name, "net.neoforged:neoforge:")
}

// gradleClassifier повертає classifier з maven-координати
// group:artifact:version[:classifier] (порожньо, якщо його немає).
func gradleClassifier(name string) string {
	m := gradlePathRe.FindStringSubmatch(name)
	if m == nil {
		return ""
	}
	return m[4]
}

// installProfile — мінімальна модель install_profile.json інсталера.
type installProfile struct {
	JSON        string                `json:"json"`
	Minecraft   string                `json:"minecraft"`
	VersionInfo *json.RawMessage      `json:"versionInfo"`
	Libraries   []forgeLibrary        `json:"libraries"`
	Data        map[string]forgeDatum `json:"data"`
	Processors  []forgeProcessor      `json:"processors"`
	Install     *struct {
		Path     string `json:"path"`
		FilePath string `json:"filePath"`
	} `json:"install"`
}

type forgeLibrary struct {
	Name string `json:"name"`
	// URL — репозиторій на рівні бібліотеки. Старіший/спрощений формат
	// install_profile.json (напр. деякі Forge-профілі) задає лише
	// "name" + "url" БЕЗ секції "downloads.artifact" узагалі (на відміну
	// від сучасного 1.13+ формату, де downloads.artifact.path/url задані
	// явно). Якщо покладатись тільки на Downloads.Artifact, такі
	// бібліотеки МОВЧКИ пропускаються в циклі завантаження -- і
	// processor згодом падає "classpath-бібліотека відсутня", хоча
	// насправді її просто ніколи не намагались качати.
	URL       string `json:"url"`
	Downloads struct {
		Artifact struct {
			Path string `json:"path"`
			URL  string `json:"url"`
			Sha1 string `json:"sha1"`
		} `json:"artifact"`
	} `json:"downloads"`
}

// forgeDatum -- один запис секції "data" install_profile.json. Кожен
// ключ (напр. "BINPATCH", "MC_UNPACKED", "MOJMAPS", "MERGED_MAPPINGS")
// має клієнтське й серверне значення у форматі processor-літералу:
// 'literal', [maven:coords] (файл з libraries/), або /path-у-архіві.
// Саме ці токени підставляються в args processors (${BINPATCH} тощо) --
// без цього args лишаються буквальним рядком "{BINPATCH}", processor
// отримує сміттєвий шлях і мовчки нічого корисного не робить, а
// потрібні client-*-srg.jar/-extra.jar так і не генеруються.
type forgeDatum struct {
	Client string `json:"client"`
	Server string `json:"server"`
}

// resolveForgeLibraryPathURL повертає (path, url, ok) для завантаження
// однієї бібліотеки з install_profile.json.libraries. Винесено в окрему
// функцію (без залежності від *Installer/мережі), щоб її можна було
// покрити unit-тестами незалежно від runForgeProcessors.
//
// Дві форми запису бібліотеки в install_profile.json:
//
//	а) сучасна (1.13+): downloads.artifact.{path,url,sha1} задані явно;
//	б) старіша/спрощена: лише "name" (+ опційний "url" репозиторію,
//	   БЕЗ секції "downloads" узагалі) -- шлях і кінцевий URL треба
//	   побудувати самим через gradlePath(name), так само, як
//	   resolveLibraryArtifact() уже робить для fabric/quilt libraries
//	   (installer.go). Без цього fallback такі бібліотеки просто мовчки
//	   пропускались (URL == "" -> continue), а processor, якому вони
//	   потрібні в classpath, падав з "класспас-бібліотека відсутня".
func resolveForgeLibraryPathURL(lib forgeLibrary) (path, url string, ok bool) {
	if lib.Name == "" {
		return "", "", false
	}
	path = lib.Downloads.Artifact.Path
	url = lib.Downloads.Artifact.URL
	if path != "" && url != "" {
		return path, url, true
	}
	rel := gradlePath(lib.Name)
	if rel == "" || rel == lib.Name {
		return "", "", false // "name" не є валідними maven-координатами
	}
	base := lib.URL
	if base == "" {
		base = "https://maven.minecraftforge.net/"
	}
	if path == "" {
		path = rel
	}
	if url == "" {
		url = strings.TrimRight(base, "/") + "/" + rel
	}
	return path, url, true
}

type forgeProcessor struct {
	Jar       string            `json:"jar"`
	Classpath []string          `json:"classpath"`
	Args      []string          `json:"args"`
	Sides     []string          `json:"sides"`
	Outputs   map[string]string `json:"outputs"`
}

// extractBundledJars розкладає усі бандлені артефакти з "maven/..." інсталера
// у спільну базу libraries/ (за шляхом усередині архіву).
//
// НЕ фільтруємо за розширенням ".jar": офіційний Forge/NeoForge installer
// іноді кладе у "maven/" артефакти з іншим розширенням (напр. деякі старіші
// NeoForge/Forge fat-installer збірки бандлять .zip чи .txt поруч із .jar,
// коли відповідний maven-координат заданий з "@ext", як
// "net.neoforged:neoform:...@zip" -- див. gradlePath()). Раніше фільтр
// "!strings.HasSuffix(name, ".jar")" тихо пропускав будь-який такий файл:
// install.jar МІГ містити потрібний артефакт всередині, а ми його просто не
// копіювали на диск, і processor згодом падав з "Input does not exist",
// хоча насправді дані для нього фізично лежали в installer.jar поруч з
// іншими maven/-артефактами.
func (inst *Installer) extractBundledJars(zr *zip.ReadCloser, versionID string) error {
	for _, f := range zr.File {
		name := f.Name
		if !strings.HasPrefix(name, "maven/") || f.FileInfo().IsDir() {
			continue
		}
		rel := strings.TrimPrefix(name, "maven/")
		dest := filepath.Join(inst.baseDir, "libraries", filepath.FromSlash(rel))
		if _, err := os.Stat(dest); err == nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(dest)
		if err != nil {
			rc.Close()
			return err
		}
		_, cerr := io.Copy(out, rc)
		out.Close()
		rc.Close()
		if cerr != nil {
			return cerr
		}
	}
	return nil
}

// runForgeProcessors виконує client-side processors інсталера (патчі
// Forge/NeoForge 1.13-1.20, до переходу на bootstrap-формат). Це той
// самий алгоритм, що офіційний Forge/NeoForge installer і Prism Launcher
// виконують під час встановлення:
//
//  1. Качаються processor-libraries з install_profile.json.libraries.
//  2. Будується таблиця змінних: захардкоджені (SIDE, ROOT, INSTALLER,
//     LIBRARY_DIR, MINECRAFT_JAR, MINECRAFT_VERSION) + УСІ ключі секції
//     install_profile.json.data (BINPATCH, MC_UNPACKED, MOJMAPS,
//     MERGED_MAPPINGS, PATCHED, MC_SLIM, MC_EXTRA, MC_SRG тощо -- набір
//     конкретних ключів залежить від версії Forge/NeoForge, тому ми не
//     хардкодимо їх імена, а копіюємо секцію data як є).
//  3. Кожен processor запускається по черзі (у порядку install_profile.json
//     -- порядок важливий: пізніші processors залежать від outputs
//     попередніх), з підстановкою цих змінних в args.
//  4. Після запуску (не тільки до!) перевіряється, що ВСІ outputs
//     processor'а існують і, якщо для output задано sha1 -- що хеш
//     збігається. Якщо ні -- install падає з чіткою помилкою, а не
//     тихо продовжується без потрібних jar-ів (типова причина краху
//     "Invalid paths argument, contained no existing paths: ...-srg.jar,
//     ...-extra.jar, ...-client.jar" -- три файли, які якраз і мали
//     згенерувати ці processors).
//
// КРИТИЧНО: MINECRAFT_VERSION/MINECRAFT_JAR мають вказувати на РЕАЛЬНУ
// версію ванілли, від якої вважає Forge (напр. "1.20.1-20230612.114412"
// -- версія з MCP-таймстемпом, profile.Minecraft), а НЕ на голий
// mcVersion ("1.20.1"). Раніше тут підставлявся mcVersion — тому шлях у
// {MINECRAFT_JAR} і всі похідні від нього токени вказували на
// versions/1.20.1/1.20.1.jar, якого processor ніколи не чіпав, а
// реальний патчений jar лишався ненайденим (саме ця розбіжність і
// спричиняла крах ModLauncher/FML: srg/extra/client-jar просто не
// існували за очікуваним шляхом).
func (inst *Installer) runForgeProcessors(zr *zip.ReadCloser, installerPath string, profile *installProfile, profileJSON []byte, versionID, mcVersion string) error {
	if len(profile.Processors) == 0 {
		return nil
	}

	inst.log("Запуск Forge processors...")
	libsDir := filepath.Join(inst.baseDir, "libraries")

	// 1) Качаємо processor libraries з install_profile.json.
	//
	// Дві форми запису бібліотеки в install_profile.json:
	//   а) сучасна (1.13+): downloads.artifact.{path,url,sha1} задані явно;
	//   б) старіша/спрощена: лише "name" (+ опційний "url" репозиторію,
	//      БЕЗ секції "downloads" узагалі) -- шлях і кінцевий URL треба
	//      побудувати самим через gradlePath(name), так само, як
	//      resolveLibraryArtifact() уже робить для fabric/quilt libraries
	//      (installer.go). Без цього fallback такі бібліотеки просто
	//      мовчки пропускались (URL == "" -> continue), а processor, якому
	//      вони потрібні в classpath, падав з "класспас-бібліотека відсутня".
	for _, lib := range profile.Libraries {
		path, url, ok := resolveForgeLibraryPathURL(lib)
		if !ok {
			continue
		}
		dest := filepath.Join(libsDir, filepath.FromSlash(path))
		if fi, err := os.Stat(dest); err == nil && fi.Size() > 0 {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return err
		}
		if err := inst.downloadFile(url, dest); err != nil {
			return fmt.Errorf("processor library %s: %w", lib.Name, err)
		}
	}

	// profile.Minecraft -- MCP-позначення версії (напр.
	// "1.20.1-20230612.114412"), яке Forge/NeoForge встановлює у ШЛЯХИ
	// ВИВОДУ processors (libraries/net/minecraft/client/<MCP-версія>/
	// client-<MCP-версія>-{slim,srg,extra,data}.jar). Це НЕ версія, яку
	// можна "встановити" через Mojang API -- Mojang про MCP-таймстемпи
	// нічого не знає, там існує лише гола mcVersion ("1.20.1"). Реальний
	// vanilla client jar якраз і качається під mcVersion; MCP-jar-и
	// (srg/extra/client) processors ГЕНЕРУЮТЬ ЛОКАЛЬНО з нього.
	//
	// Попередня версія цього коду плутала їх: якщо тут підставити
	// profile.Minecraft напряму в EnsureVersion/шлях клієнтського jar-а,
	// або (як робилось РАНІШЕ) навпаки — підставити голий mcVersion туди,
	// де processors очікують МСP-рядок токена MINECRAFT_VERSION — обидва
	// варіанти ламають шлях, за яким processor шукає вхідний client jar
	// або куди пише вихідний srg/extra/client jar. Симптом точно такий,
	// як у зафіксованому крашлозі: "Invalid paths argument, contained no
	// existing paths: ...-srg.jar, ...-extra.jar, ...-client.jar" — три
	// файли, шлях до яких побудований з неправильного значення токена.
	mcpVersion := strings.TrimSpace(profile.Minecraft)
	if mcpVersion == "" {
		mcpVersion = mcVersion
	}

	// Прибираємо ВСЮ проміжну MCP-теку (libraries/net/minecraft/client/
	// <mcpVersion>/, де processors тримають client-<mcp>-{slim,srg,extra,
	// data}.jar) перед запуском. runForgeProcessors викликається лише
	// коли checkForgeInstalled вирішив, що install ще не завершено — тож
	// видаляти тут безпечно, готового встановлення це не зачепить.
	//
	// Без цього битий/неповний проміжний файл із попередньої невдалої
	// спроби (обрив мережі, вбитий процес, сумісний з "просто збігом" по
	// os.Stat) лишався на диску: жоден наступний processor не декларує
	// цей конкретний файл як свій "Output" із sha1 у install_profile.json
	// (він Output ОДНОГО кроку і Input/Clean наступного), тому раніша
	// перевірка через processorOutputsValid його ніколи не перевіряла —
	// саме це й давало стабільний "Patch expected checksum X but was Y"
	// на РІВНО ОДНОМУ й тому ж кроці щоразу, скільки б install не
	// повторювався.
	mcpLibDir := filepath.Join(libsDir, "net", "minecraft", "client", mcpVersion)
	if err := os.RemoveAll(mcpLibDir); err != nil {
		inst.log(fmt.Sprintf("Попередження: не вдалося очистити %s: %v", mcpLibDir, err))
	}

	// 2) Гарантуємо, що СПРАВЖНЯ vanilla-версія (mcVersion, та, що є в
	// Mojang version_manifest) встановлена — саме її client jar є
	// вхідними даними для processors. EnsureVersion сам звіряє sha1
	// наявного client.jar проти version.json і перекачує при
	// невідповідності (див. installer.go) — тому просто завжди
	// проходимо через нього, а не дублюємо тут "голу" перевірку
	// os.Stat+Size()>0. Саме така "гола" перевірка й пропускала биті/
	// неповні client jar-и як нібито готові, через що binarypatcher
	// падав з checksum mismatch на першому ж патчі.
	if _, err := inst.ensureVersionStart(mcVersion); err != nil {
		return fmt.Errorf("vanilla %s для processors: %w", mcVersion, err)
	}
	mcJar := filepath.Join(inst.baseDir, "versions", mcVersion, mcVersion+".jar")

	// 3) База змінних: захардкоджені + УСЯ секція data (клієнтські
	// значення — ми ставимо тільки client-side processors, SIDE=client).
	// MINECRAFT_VERSION навмисно = mcpVersion (MCP-рядок), бо саме цей
	// токен install_profile.json підставляє у ШЛЯХИ ВИВОДУ processors
	// (напр. data.MC_SLIM = "[net.minecraft:client:{MINECRAFT_VERSION}:slim]"),
	// а MINECRAFT_JAR = справжній vanilla client jar (mcVersion) — вхід
	// для тих самих processors.
	vars := map[string]string{
		"SIDE":              "client",
		"ROOT":              inst.baseDir,
		"INSTALLER":         installerPath,
		"LIBRARY_DIR":       libsDir,
		"MINECRAFT_VERSION": mcpVersion,
		"MINECRAFT_JAR":     mcJar,
	}
	for key, datum := range profile.Data {
		v := datum.Client
		if v == "" {
			continue
		}
		vars[key] = inst.resolveProcValue(v, vars, zr)
	}

	// Кількість client-side процесорів для прогрес-бару (як у старому
	// лаунчері — рахуємо лише те, що реально буде виконано).
	totalProc := 0
	for _, proc := range profile.Processors {
		if len(proc.Sides) > 0 {
			clientSide := false
			for _, s := range proc.Sides {
				if s == "client" {
					clientSide = true
					break
				}
			}
			if !clientSide {
				continue
			}
		}
		totalProc++
	}
	procDone := 0

	// 4) Виконуємо processors У ПОРЯДКУ install_profile.json — кожен
	// наступний зазвичай споживає output попереднього.
	//
	// Ретрай-логіка портована з MinecraftInstaller.runForgeProcessors
	// (старий лаунчер): якщо processor впав, перед повторною спробою
	// перезавантажуємо ВСІ install-бібліотеки (частина з них могла бути
	// пошкоджена/неповна після невдалого мережевого запиту) і перебудовуємо
	// classpath. Без цього одинична мережева похибка під час завантаження
	// бібліотек назавжди валила встановлення Forge/NeoForge, хоча повторна
	// спроба з чистими файлами зазвичай минається без проблем.
	const maxRetries = 1
	for _, proc := range profile.Processors {
		if len(proc.Sides) > 0 {
			clientSide := false
			for _, s := range proc.Sides {
				if s == "client" {
					clientSide = true
					break
				}
			}
			if !clientSide {
				continue
			}
		}
		procJar := filepath.Join(libsDir, filepath.FromSlash(gradlePath(proc.Jar)))
		if fi, err := os.Stat(procJar); err != nil || fi.Size() == 0 {
			return fmt.Errorf("processor jar відсутній: %s (шлях %s) — встановлення Forge неможливо продовжити коректно", proc.Jar, procJar)
		}

		// Skip, якщо всі outputs ВЖЕ валідні (повторний запуск install —
		// не переробляємо готове). Processor БЕЗ outputs пропускати не
		// можна: немає чим перевірити, чи він уже виконувався (напр. перші
		// installertools-кроки: EXTRACT_FILES/BUNDLER_EXTRACT/MCP_DATA/
		// DOWNLOAD_MOJMAPS/MERGE_MAPPING у install_profile.json просто не
		// декларують outputs) -- такі запускаємо завжди.
		if len(proc.Outputs) > 0 {
			if allValid, _ := inst.processorOutputsValid(proc.Outputs, vars); allValid {
				continue
			}
		}

		mainClass := mainClassFromJar(procJar)
		if mainClass == "" {
			return fmt.Errorf("не вдалося визначити Main-Class processor'а %s", proc.Jar)
		}

		args := []string{}
		for _, a := range proc.Args {
			args = append(args, inst.resolveProcValue(a, vars, zr))
		}

		// Інтерсепт DOWNLOAD_MOJMAPS — портовано з MinecraftInstaller
		// (interceptDownloadMojmaps): офіційний processor DOWNLOAD_MOJMAPS
		// качає НАЙНОВІШИЙ mojmap з Mojang, а бінарний патч Forge/NeoForge
		// створений під конкретну ЗАФІКСОВАНУ версію. Замість покладатись
		// на офіційний крок, качаємо mappings із version_manifest конкретно
		// для потрібної версії — так само, як робить HMCL. Якщо з якоїсь
		// причини інтерсепт не спрацював (версія не знайдена в маніфесті
		// тощо) — переходимо до звичайного запуску processor'а як fallback.
		if intercepted, err := inst.interceptDownloadMojmaps(args); err != nil {
			return fmt.Errorf("DOWNLOAD_MOJMAPS: %w", err)
		} else if intercepted {
			procDone++
			inst.reportInstallProgress("loader", 30+procDone*40/maxInt(totalProc, 1), fmt.Sprintf("Processor %d/%d: %s", procDone, totalProc, proc.Jar))
			continue
		}

		procDone++
		inst.log(fmt.Sprintf("Processor %d/%d: %s", procDone, totalProc, proc.Jar))
		inst.reportInstallProgress("loader", 30+procDone*40/maxInt(totalProc, 1), fmt.Sprintf("Processor %d/%d: %s", procDone, totalProc, proc.Jar))

		var lastErr error
		for retry := 0; retry <= maxRetries; retry++ {
			if retry > 0 {
				inst.log(fmt.Sprintf("Повтор %d для processor'а %s (перезавантажую бібліотеки)...", retry, proc.Jar))
				// Перезавантажуємо всі install-бібліотеки: видаляємо і
				// качаємо заново (як старий лаунчер — Files.delete +
				// downloadForgeArtifact у циклі re-download).
				for _, lib := range profile.Libraries {
					path, _, ok := resolveForgeLibraryPathURL(lib)
					if !ok {
						continue
					}
					os.Remove(filepath.Join(libsDir, filepath.FromSlash(path)))
				}
				for _, lib := range profile.Libraries {
					path, url, ok := resolveForgeLibraryPathURL(lib)
					if !ok {
						continue
					}
					dest := filepath.Join(libsDir, filepath.FromSlash(path))
					if err := os.MkdirAll(filepath.Dir(dest), 0755); err == nil {
						inst.downloadFile(url, dest)
					}
				}
			}

			cp := []string{}
			cpOK := true
			for _, spec := range proc.Classpath {
				p := filepath.Join(libsDir, filepath.FromSlash(gradlePath(spec)))
				if fi, err := os.Stat(p); err == nil && fi.Size() > 0 {
					cp = append(cp, p)
				} else {
					cpOK = false
					lastErr = fmt.Errorf("classpath-бібліотека processor'а %s відсутня: %s", proc.Jar, p)
					break
				}
			}
			if !cpOK {
				if retry < maxRetries {
					continue
				}
				return lastErr
			}
			cp = append(cp, procJar)

			// Кнопка «Зупинити»: перевіряємо й між процесорами (retry-цикл
			// теж), щоб скасування спрацьовувало навіть у паузі між Java-
			// процесами, а не лише всередині одного runJavaProcessor.
			if inst.cancelRequested.Load() {
				return ErrInstallCancelled
			}
			if err := runJavaProcessor(inst, procJar, mainClass, cp, args, 5*60); err != nil {
				lastErr = fmt.Errorf("processor %s завершився з помилкою: %w", proc.Jar, err)
				if retry < maxRetries {
					continue
				}
				return lastErr
			}

			// ПІСЛЯ запуску обов'язково перевіряємо, що outputs і справді
			// з'явились (і за наявності sha1 — що хеш збігається). Без
			// цього тихо "успішний" процесор, який з якоїсь причини нічого
			// не створив (напр. неправильний шлях у $MINECRAFT_JAR), лишає
			// відсутніми client-*-srg.jar/-extra.jar/forge-*-client.jar —
			// саме ці три файли й вимагає ModLauncher на старті гри.
			if ok, missing := inst.processorOutputsValid(proc.Outputs, vars); !ok {
				lastErr = fmt.Errorf("processor %s відпрацював, але очікуваний файл не з'явився: %s", proc.Jar, missing)
				if retry < maxRetries {
					continue
				}
				return lastErr
			}

			lastErr = nil
			break // success
		}
		if lastErr != nil {
			return lastErr
		}
	}
	return nil
}

// maxInt повертає більше з двох цілих чисел (явна реалізація, щоб не
// покладатись на версію toolchain з вбудованим max/min).
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// interceptDownloadMojmaps перехоплює DOWNLOAD_MOJMAPS-processor: замість
// довіряти офіційному кроку (який іноді качає НАЙНОВІШИЙ mojmap з Mojang)
// качає mappings конкретної версії з version_manifest_v2 (URL, прив'язаний
// до конкретного контенту). Портовано з
// MinecraftInstaller.interceptDownloadMojmaps у старому лаунчері. Повертає
// (true, nil), якщо перехоплено й оброблено; (false, nil) — якщо це не
// DOWNLOAD_MOJMAPS або дані не знайдені (виклик має продовжити звичайний
// запуск processor'а як fallback).
func (inst *Installer) interceptDownloadMojmaps(args []string) (bool, error) {
	task := extractArgValue(args, "--task")
	if task != "DOWNLOAD_MOJMAPS" {
		return false, nil
	}
	side := extractArgValue(args, "--side")
	if side == "server" {
		return false, nil
	}
	version := extractArgValue(args, "--version")
	output := extractArgValue(args, "--output")
	if version == "" || output == "" {
		return false, nil
	}

	if fi, err := os.Stat(output); err == nil && fi.Size() > 0 {
		inst.log(fmt.Sprintf("Mojmap для %s вже існує, пропускаємо DOWNLOAD_MOJMAPS", version))
		return true, nil
	}

	body, err := inst.fetchURL(manifestURL)
	if err != nil {
		inst.log(fmt.Sprintf("DOWNLOAD_MOJMAPS: не вдалось завантажити version manifest (%v) — fallback на звичайний processor", err))
		return false, nil
	}
	var manifest VersionManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return false, nil
	}
	var versionURL string
	for _, v := range manifest.Versions {
		if v.ID == version {
			versionURL = v.URL
			break
		}
	}
	if versionURL == "" {
		inst.log(fmt.Sprintf("DOWNLOAD_MOJMAPS: версія %s не знайдена в manifest — fallback", version))
		return false, nil
	}

	vBody, err := inst.fetchURL(versionURL)
	if err != nil {
		return false, nil
	}
	var vObj VersionJSON
	if err := json.Unmarshal(vBody, &vObj); err != nil {
		return false, nil
	}

	mappingsURL := vObj.Downloads.ClientMappings.URL
	if mappingsURL == "" {
		inst.log("DOWNLOAD_MOJMAPS: немає client_mappings у version.json — fallback")
		return false, nil
	}

	inst.log(fmt.Sprintf("Завантаження client mappings для %s (перехоплення DOWNLOAD_MOJMAPS)...", version))
	if err := os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return false, err
	}
	if err := inst.downloadFile(mappingsURL, output); err != nil {
		return false, fmt.Errorf("download mojmap: %w", err)
	}
	return true, nil
}

// extractArgValue шукає значення прапорця у плоскому списку args
// (["--task", "DOWNLOAD_MOJMAPS", ...]) — та сама семантика, що
// extractArg у старому лаунчері.
func extractArgValue(args []string, flag string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag {
			return args[i+1]
		}
	}
	return ""
}

// processorOutputsValid перевіряє, чи всі outputs processor'а вже існують
// на диску і (якщо задано очікуваний sha1) чи збігається хеш. Повертає
// також шлях першого невалідного output — для зрозумілого повідомлення
// про помилку.
func (inst *Installer) processorOutputsValid(outputs map[string]string, vars map[string]string) (bool, string) {
	if len(outputs) == 0 {
		// Немає оголошених outputs -- перевіряти нічого, тож "всі outputs
		// валідні" тривіально. Раніше тут поверталось (false, "") -- і
		// кожен processor без outputs (напр. перші кроки installertools:
		// EXTRACT_FILES/BUNDLER_EXTRACT/MCP_DATA/DOWNLOAD_MOJMAPS/
		// MERGE_MAPPING) після успішного запуску падав із хибною помилкою
		// "очікуваний файл не з'явився:" (порожній шлях), блокуючи все
		// встановлення Forge/NeoForge.
		return true, ""
	}
	for out, expectedSha1 := range outputs {
		p := inst.resolveProcValue(out, vars, nil)
		fi, err := os.Stat(p)
		if err != nil || fi.Size() == 0 {
			return false, p
		}
		sha1Literal := inst.resolveProcValue(expectedSha1, vars, nil)
		// sha1Literal сам може бути ще одним {TOKEN}, що резолвиться в
		// hex-рядок хеша (так задають install_profile.json для деяких
		// output'ів) — перевіряємо, лише якщо це схоже на sha1 (40 hex).
		if len(sha1Literal) == 40 && isHex(sha1Literal) {
			actual, err := sha1OfFile(p)
			if err != nil || !strings.EqualFold(actual, sha1Literal) {
				return false, p
			}
		}
	}
	return true, ""
}

// sha1OfFile рахує SHA-1 файлу — використовується для перевірки outputs
// processors, коли install_profile.json задає очікуваний хеш (замість
// довіряти самому факту існування файлу, який міг лишитись від
// попереднього невдалого/часткового встановлення).
func sha1OfFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// isHex перевіряє, що рядок складається лише з hex-символів.
func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// resolveProcValue підставляє змінні processors: {KEY} → vars, 'literal' →
// без лапок, [artifact] → шлях у libraries/, /path чи data/ → файл з
// інсталера у тимчасову теки.
func (inst *Installer) resolveProcValue(literal string, vars map[string]string, zr *zip.ReadCloser) string {
	if literal == "" {
		return literal
	}
	// {KEY}
	if strings.HasPrefix(literal, "{") && strings.HasSuffix(literal, "}") {
		key := strings.TrimSuffix(strings.TrimPrefix(literal, "{"), "}")
		if v, ok := vars[key]; ok {
			return v
		}
		return literal
	}
	// 'literal'
	if strings.HasPrefix(literal, "'") && strings.HasSuffix(literal, "'") && len(literal) >= 2 {
		return literal[1 : len(literal)-1]
	}
	// [artifact] -- maven-координати можуть містити {TOKEN} усередині
	// (напр. "[net.minecraft:client:{MINECRAFT_VERSION}:slim]" -- так
	// install_profile.json задає шлях виводу processor'а, прив'язаний до
	// конкретної MCP-версії). Токени ТРЕБА підставити ДО виклику
	// gradlePath, інакше фігурні дужки потраплять буквально у groupId/
	// artifactId/version і зламають увесь шлях (processor або нічого не
	// створить за таким шляхом, або запис піде не туди, куди його потім
	// шукатиме гра).
	if strings.HasPrefix(literal, "[") && strings.HasSuffix(literal, "]") && len(literal) >= 2 {
		inner := replaceTokens(vars, literal[1:len(literal)-1])
		return filepath.Join(inst.baseDir, "libraries", filepath.FromSlash(gradlePath(inner)))
	}
	// /path або data/... усередині інсталера
	if zr != nil && (strings.HasPrefix(literal, "/") || strings.HasPrefix(literal, "data/")) {
		pathInZip := strings.TrimPrefix(literal, "/")
		for _, f := range zr.File {
			if f.Name == pathInZip {
				// ВАЖЛИВО: temp-шлях МАЄ бути унікальним для КОЖНОЇ
				// версії Forge/NeoForge, яку встановлюємо, а не спільним
				// "shaurma-forge/data/client.lzma" для всіх запусків
				// підряд. Усередині install_profile.json будь-якого
				// Forge/NeoForge installer.jar внутрішній шлях до binpatch
				// файлу (data/client.lzma) ЗАВЖДИ однаковий незалежно від
				// версії — це просто конвенція формату installer.jar. Якщо
				// раніше в цій самій temp-теці вже лежав client.lzma ВІД
				// ІНШОЇ версії Forge (напр. установка 1.16.5 після
				// 1.21.1-neoforge) — os.Stat бачив "файл є" і повертав
				// ЧУЖИЙ, застарілий набір binpatch-ів, тому clean vanilla
				// jar правильної (поточної) версії порівнювався проти
				// checksum-ів, зашитих під зовсім ІНШУ версію Minecraft.
				// Це і давало стабільний, однаковий на вигляд checksum
				// mismatch (той самий клас, той самий очікуваний checksum)
				// на будь-якій версії, встановленій НЕ першою в цій
				// temp-теці. Ключуємо підтеку за MCP/loader-версією
				// (vars["MINECRAFT_VERSION"] — унікальна для кожного
				// install_profile.json), щоб різні версії ніколи не
				// ділили один і той самий кеш.
				cacheKey := vars["MINECRAFT_VERSION"]
				if cacheKey == "" {
					cacheKey = "unknown"
				}
				tmp := filepath.Join(inst.forgeCacheDir, sanitizeTempKey(cacheKey), filepath.FromSlash(pathInZip))
				if _, err := os.Stat(tmp); err == nil {
					return tmp
				}
				rc, err := f.Open()
				if err != nil {
					break
				}
				os.MkdirAll(filepath.Dir(tmp), 0755)
				out, err := os.Create(tmp)
				if err != nil {
					rc.Close()
					break
				}
				_, cerr := io.Copy(out, rc)
				out.Close()
				rc.Close()
				if cerr == nil {
					return tmp
				}
				break
			}
		}
	}
	// Заміна токенів у рядку.
	return replaceTokens(vars, literal)
}

// sanitizeTempKey прибирає символи, недопустимі у назвах тек Windows
// (напр. ":" у MCP-версіях на кшталт "1.16.5-20210115.111550" безпечний,
// але про всяк випадок фільтруємо весь набір заборонених символів NTFS).
func sanitizeTempKey(key string) string {
	var sb strings.Builder
	for _, r := range key {
		switch r {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			sb.WriteByte('_')
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func replaceTokens(vars map[string]string, value string) string {
	var sb strings.Builder
	for x := 0; x < len(value); x++ {
		c := value[x]
		// '\\' -- escape ТІЛЬКИ перед спецсимволами токена ('{', '}', '\\'),
		// інакше це звичайний символ (критично для Windows-шляхів у outputs/
		// args processors -- напр. "C:\\Users\\..." не має втрачати бекслеші).
		if c == '\\' && x+1 < len(value) && (value[x+1] == '{' || value[x+1] == '}' || value[x+1] == '\\') {
			sb.WriteByte(value[x+1])
			x++
		} else if c == '{' {
			var key strings.Builder
			matched := false
			for y := x + 1; y < len(value); y++ {
				if value[y] == '}' {
					if v, ok := vars[key.String()]; ok {
						sb.WriteString(v)
					} else {
						sb.WriteString("{" + key.String() + "}")
					}
					x = y
					matched = true
					break
				}
				key.WriteByte(value[y])
			}
			if !matched {
				sb.WriteByte(c)
			}
		} else {
			sb.WriteByte(c)
		}
	}
	return sb.String()
}

var gradlePathRe = regexp.MustCompile(`^([^:@]+):([^:@]+):([^:@]+)(?::([^:@]+))?(?:@([^:@]+))?$`)

// gradlePath перетворює maven-координати group:artifact:version[:classifier][@ext]
// на повний шлях у libraries/ РАЗОМ з розширенням файлу (типово "jar",
// якщо "@ext" не вказано -- так само, як Prism GradleSpecifier::toPath()).
// "@extension" -- окремий, необов'язковий компонент maven-нотації; regex не
// дозволяє символам ':' або '@' потрапити всередину жодної з груп, тож
// "@zip" НІКОЛИ не потрапляє у version/classifier, навіть якщо він
// написаний впритул до версії (як у "25w14craftmine-20250401.222524@zip").
// Приклади:
//
//	net.minecraftforge:forge:1.20.1-47.2.0
//	  -> net/minecraftforge/forge/1.20.1-47.2.0/forge-1.20.1-47.2.0.jar
//	net.neoforged:neoform:25w14craftmine-20250401.222524@zip
//	  -> net/neoforged/neoform/25w14craftmine-20250401.222524/neoform-25w14craftmine-20250401.222524.zip
func gradlePath(spec string) string {
	spec = strings.TrimSpace(spec)
	if strings.Contains(spec, "\\") || strings.Contains(spec, "/") {
		// Вже шлях або URL.
		if strings.HasSuffix(spec, ".jar") {
			return strings.TrimPrefix(spec, "libraries/")
		}
	}
	m := gradlePathRe.FindStringSubmatch(spec)
	if m == nil {
		return spec
	}
	group, artifact, version, classifier, ext := m[1], m[2], m[3], m[4], m[5]
	if ext == "" {
		ext = "jar"
	}
	groupPath := strings.ReplaceAll(group, ".", "/")
	filename := fmt.Sprintf("%s-%s", artifact, version)
	if classifier != "" {
		filename += "-" + classifier
	}
	filename += "." + ext
	return fmt.Sprintf("%s/%s/%s/%s", groupPath, artifact, version, filename)
}

// mainClassFromJar читає Main-Class з MANIFEST.MF jar-файлу.
func mainClassFromJar(jarPath string) string {
	zr, err := zip.OpenReader(jarPath)
	if err != nil {
		return ""
	}
	defer zr.Close()
	for _, f := range zr.File {
		if strings.EqualFold(f.Name, "META-INF/MANIFEST.MF") {
			rc, _ := f.Open()
			data, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return ""
			}
			for _, line := range strings.Split(string(data), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "Main-Class:") {
					return strings.TrimSpace(strings.TrimPrefix(line, "Main-Class:"))
				}
			}
		}
	}
	return ""
}

// runJavaProcessor запускає Java-процес (processor) з classpath та args.
//
// Forge/NeoForge processors — довготривалий зовнішній Java-patcher. Раніше тут
// був блокуючий cmd.CombinedOutput(), який НЕ перевіряв прапорець скасування і
// ніколи не вбивав процес — кнопка «Зупинити» під час experiments процесорів
// не діяла (inst.cancelRequested просто ставився, але Java-NGCдобігав сам).
// Тепер: перевіряємо прапорець ДО запуску і паралельно слідкуємо за ним під
// час роботи (і за таймаутом) — при скасуванні вбиваємо дочірній Java.
func runJavaProcessor(inst *Installer, procJar, mainClass string, cp, args []string, timeoutSec int) error {
	javaBin := "java"
	// Передаємо виконуваний файл Java, який використовує лаунчер для гри.
	if inst.procJava != "" {
		if _, err := os.Stat(inst.procJava); err == nil {
			javaBin = inst.procJava
		}
	}
	if inst.cancelRequested.Load() {
		return ErrInstallCancelled
	}
	cmd := exec.Command(javaBin, append([]string{"-cp", strings.Join(cp, string(os.PathListSeparator)), mainClass}, args...)...)
	cmd.Dir = inst.baseDir
	hideConsoleWindow(cmd)
	// Збираємо вивід для діагностики (як CombinedOutput), але запускаємо
	// асинхронно, щоб мати змогу вбити процес раніше.
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s (output: %s)", err, strings.TrimSpace(out.String()))
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()

	var timeoutCh <-chan time.Time
	if timeoutSec > 0 {
		timeoutCh = time.After(time.Duration(timeoutSec) * time.Second)
	}

	// Періодично перевіряємо скасування, поки процес працює.
	tick := time.NewTicker(150 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case err := <-waited:
			if err != nil {
				return fmt.Errorf("%s (output: %s)", err, strings.TrimSpace(out.String()))
			}
			return nil
		case <-timeoutCh:
			_ = cmd.Process.Kill()
			<-waited
			return fmt.Errorf("processor timeout after %d s (output: %s)", timeoutSec, strings.TrimSpace(out.String()))
		case <-tick.C:
			if inst.cancelRequested.Load() {
				_ = cmd.Process.Kill()
				<-waited
				return ErrInstallCancelled
			}
		}
	}
}

// SetProcessorJava задає Java для запуску Forge processors (зазвичай та,
// яку лаунчер обрав для самої гри). Викликається перед EnsureLoader.
func (inst *Installer) SetProcessorJava(path string) { inst.procJava = path }

// fetchURL качає URL і повертає тіло.
func (inst *Installer) fetchURL(url string) ([]byte, error) {
	resp, err := inst.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d (%s)", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}

// fetchURLWithRetry качає URL з retry логікою (3 спроби, експоненційна затримка).
// Захист від тимчасових мережевих помилок.
func (inst *Installer) fetchURLWithRetry(url string, maxRetries int) ([]byte, error) {
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			inst.log(fmt.Sprintf("Повторна спроба %d/%d: %s", attempt, maxRetries, url))
		}

		resp, err := inst.client.Get(url)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return io.ReadAll(resp.Body)
			}
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			// 404 = файл не існує, повторювати немає сенсу
			if resp.StatusCode == http.StatusNotFound {
				return nil, fmt.Errorf("файл не знайдено (404): %s", url)
			}
		} else {
			lastErr = err
		}

		// Затримка перед наступною спробою (2s, 4s)
		if attempt < maxRetries {
			delay := time.Duration(attempt) * 2 * time.Second
			inst.log(fmt.Sprintf("Затримка %v перед наступною спробою...", delay))
			time.Sleep(delay)
		}
	}

	return nil, fmt.Errorf("не вдалося завантажити після %d спроб: %w", maxRetries, lastErr)
}

// fetchURLWithFallback пробує завантажити з кількох fallback URLs.
// Захист від зміни посилань та блокувань.
func (inst *Installer) fetchURLWithFallback(baseURLs []string, suffix string, maxRetries int) ([]byte, error) {
	var lastErr error

	for i, baseURL := range baseURLs {
		fullURL := baseURL + suffix
		if i > 0 {
			inst.log(fmt.Sprintf("Fallback %d/%d: %s", i+1, len(baseURLs), fullURL))
		}

		body, err := inst.fetchURLWithRetry(fullURL, maxRetries)
		if err == nil {
			if i > 0 {
				inst.log(fmt.Sprintf("Успішно завантажено з fallback URL: %s", baseURL))
			}
			return body, nil
		}

		inst.log(fmt.Sprintf("Помилка (%s): %v", baseURL, err))
		lastErr = err
	}

	return nil, fmt.Errorf("не вдалося завантажити з жодного з %d URLs: %w", len(baseURLs), lastErr)
}

// downloadIfMissing качає файл, якщо його ще немає (перевірка наявності —
// Prism-стиль: пошкоджені/пусті перекачуються).
func (inst *Installer) downloadIfMissing(url, dest string) error {
	return inst.downloadIfMissingAny([]string{url}, dest)
}

// downloadIfMissingAny качає файл з ПЕРШОГО успішного кандидата. Для
// installer.jar Forge/NeoForge один і той самий артефакт іноді живе під
// РІЗНИМИ maven-іменами (напр. старий Forge 1.8.9: і
// forge-1.8.9-11.15.1.2318-installer.jar, і
// forge-1.8.9-11.15.1.2318-1.8.9-installer.jar у відповідній теці) —
// пробуємо всі кандидати по черзі, поки один не скачається. Якщо файл
// уже є на диску — повертаємо nil одразу (жодного запиту).
func (inst *Installer) downloadIfMissingAny(urls []string, dest string) error {
	if fi, err := os.Stat(dest); err == nil && fi.Size() > 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}

	var lastErr error
	for _, url := range urls {
		inst.log(fmt.Sprintf("Спроба завантажити: %s", url))
		if err := inst.downloadFile(url, dest); err != nil {
			lastErr = err
			inst.log(fmt.Sprintf("  не вдалось (%v), пробую наступний кандидат", err))
			os.Remove(dest)
			continue
		}
		// Перевірка: архів має бути валідним zip.
		if strings.HasSuffix(strings.ToLower(dest), ".jar") {
			if zr, err := zip.OpenReader(dest); err != nil {
				os.Remove(dest)
				lastErr = fmt.Errorf("пошкоджений архів %s: %w", dest, err)
				continue
			} else {
				zr.Close()
			}
		}
		return nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("немає жодного кандидата URL")
	}
	return lastErr
}

var _ = runtime.GOOS // зберегти import runtime при подальших правках
