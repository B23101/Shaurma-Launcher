package minecraft

import (
	"archive/zip"
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
	neoMavenURLs = []string{
		"https://maven.neoforged.net/releases/net/neoforged/neoforge/",
	}
)

const (
	// Основні URLs (для зворотної сумісності з існуючим кодом)
	forgeMaven = "https://maven.minecraftforge.net/net/minecraftforge/forge/"
	neoMaven   = "https://maven.neoforged.net/releases/net/neoforged/neoforge/"
	fabricMeta = "https://meta.fabricmc.net/v2/versions/loader/"
	quiltMeta  = "https://meta.quiltmc.org/v3/versions/loader/"

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
// для Forge/NeoForge — ID береться з version.json інсталера.
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
	case "forge", "neoforge":
		return mcVersion + "-" + loaderVersion
	default:
		return mcVersion
	}
}

// EnsureLoader встановлює лоадер для збірки і повертає ID версії-профілю
// (той, що передається у LaunchConfig.Instance.MCVersion для запуску).
// Для vanilla повертає mcVersion без змін.
func (inst *Installer) EnsureLoader(loader, mcVersion, loaderVersion string) (string, error) {
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

// resolveRecommendedLoaderVersion повертає версію лоадера, яку СПРАВДІ
// слід встановити.
//
// РАНІШЕ це рахувалось лише тоді, коли loaderVersion був порожній або
// маркером "recommended"/"latest" — а явно задане (нехай і застаріле)
// значення з даних збірки (сервер Shaurma чи кастомний пак) бралось як є.
// Це і було причиною багу: сервер міг тримати захардкоджену стару версію
// лоадера (напр. quilt 0.20.0-beta.9 для MC 26.1.2), яка РЕАЛЬНО існує в
// меті (не 404) і встановлюється, але несумісна з новішим байткодом гри й
// падає з криптичним "Unsupported class file major version". Prism у
// такому разі ЗАВЖДИ бере поточну рекомендовану версію з мети — він
// взагалі не має механізму "запам'ятати стару версію лоадера назавжди".
//
// ТЕПЕР: завжди питаємо мета-API за реальною рекомендованою версією для
// цієї MC-версії і, якщо вона відрізняється від заданої — використовуємо
// рекомендовану, а задану ігноруємо (з логом, щоб було видно чому). Це
// узгоджує поведінку з Prism: користувач завжди отримує актуальний,
// протестований лоадер, а не те, що випадково лежить у даних збірки.
//
// ОНОВЛЕННЯ 2026-08-03: Додано fallback URLs та retry логіку для захисту
// від зміни посилань та тимчасових мережевих помилок.
func (inst *Installer) resolveRecommendedLoaderVersion(metaVersionsBase, loaderName, mcVersion, loaderVersion string) string {
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
		inst.log(fmt.Sprintf("Не вдалось отримати рекомендовану версію %s для %s (%v) — використовую задану: %q", loaderName, mcVersion, err, loaderVersion))
		return loaderVersion
	}

	var entries []struct {
		Loader struct {
			Version string `json:"version"`
			Stable  bool   `json:"stable"`
		} `json:"loader"`
	}
	if err := json.Unmarshal(body, &entries); err != nil || len(entries) == 0 {
		inst.log(fmt.Sprintf("Порожній/невалідний список версій %s для %s — використовую задану: %q", loaderName, mcVersion, loaderVersion))
		return loaderVersion
	}

	// Перша стабільна — це і є "рекомендована". Якщо стабільних немає
	// (рідкісний випадок на щойно вийшлому MC snapshot) — беремо просто
	// найновішу (перший запис), це краще за застарілу захардкоджену.
	recommended := ""
	for _, e := range entries {
		if e.Loader.Stable && e.Loader.Version != "" {
			recommended = e.Loader.Version
			break
		}
	}
	if recommended == "" && entries[0].Loader.Version != "" {
		recommended = entries[0].Loader.Version
	}
	if recommended == "" {
		return loaderVersion
	}

	if strings.TrimSpace(loaderVersion) == "" {
		inst.log(fmt.Sprintf("Рекомендована версія %s для %s: %s", loaderName, mcVersion, recommended))
		return recommended
	}
	if recommended != loaderVersion {
		inst.log(fmt.Sprintf("Задана версія %s (%s) відрізняється від рекомендованої (%s) — беремо рекомендовану, як Prism", loaderName, loaderVersion, recommended))
		return recommended
	}
	return loaderVersion
}

// installMetaLoader — спільний шлях для Fabric/Quilt: мета-ендпоінт віддає
// готовий loader-профіль (JSON з id, inheritsFrom, libraries, mainClass).
// Версія лоадера на цей момент вже пройшла через
// resolveRecommendedLoaderVersion (EnsureLoader) — сюди приходить те, що
// реально треба встановити.
//
// ОНОВЛЕННЯ 2026-08-03: Додано fallback URLs та retry логіку.
func (inst *Installer) installMetaLoader(metaBase, loader, mcVersion, loaderVersion string) (string, error) {
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

	if _, err := inst.EnsureVersion(profile.ID); err != nil {
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
	installerURL := mavenBase + fullVer + "/forge-" + fullVer + "-installer.jar"
	if neoforge {
		// NeoForge maven: .../neoforge/<ver>/neoforge-<ver>-installer.jar
		installerURL = mavenBase + loaderVersion + "/neoforge-" + loaderVersion + "-installer.jar"
	}

	inst.log(fmt.Sprintf("Завантаження %s installer...", name))
	inst.reportInstallProgress("loader", 5, fmt.Sprintf("Завантаження %s installer...", name))
	installerPath := filepath.Join(inst.baseDir, "installers", name+"-"+fullVer+".jar")
	if err := inst.downloadIfMissing(installerURL, installerPath); err != nil {
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
	//   Forge    -> "<mc>-forge-<loaderVersion>"  (напр. 1.20.1-forge-47.4.10)
	//   NeoForge -> "neoforge-<loaderVersion>"     (напр. neoforge-21.1.233)
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
			// ID профілю NeoForge ЗАВЖДИ рівно "neoforge-<loaderVersion>"
			// (без mc-префікса) — точний збіг, щоб не чіпати інші версії.
			if name == "neoforge-"+loaderVersion {
				profileID = name
				break
			}
		} else if strings.HasSuffix(name, "-forge-"+loaderVersion) {
			// Forge: "<mc>-forge-<loaderVersion>" — префікс-частину (mc)
			// скануємо за суфіксом, щоб не залежати від точності mcVersion
			// у даних збірки.
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

	// Реальна MC-версія, від якої успадковує лоадер — авторитетне джерело
	// (як inheritsFrom у forge-version.json старого лаунчера), а не голий
	// параметр mcVersion, який міг бути неточним у даних збірки.
	realMcVersion := vj.InheritsFrom
	if realMcVersion == "" {
		realMcVersion = mcVersion
	}

	// Шлях client.jar у спільній libraries/ залежить від лоадера:
	//   Forge    -> net/minecraftforge/forge/<mc>-<lv>/forge-<mc>-<lv>-client.jar
	//   NeoForge -> net/neoforged/neoforge/<lv>/neoforge-<lv>-client.jar
	// (у NeoForge maven-тека — це сам loaderVersion БЕЗ mc-префікса; БАГ:
	// раніше для neoforge будувався шлях з mc-префіксом, тож навіть
	// знайдений профіль не проходив перевірку client.jar.)
	var clientJar string
	if neoforge {
		clientJar = filepath.Join(inst.baseDir, "libraries", "net", "neoforged", "neoforge", loaderVersion, "neoforge-"+loaderVersion+"-client.jar")
	} else {
		fullVer := realMcVersion + "-" + loaderVersion
		clientJar = filepath.Join(inst.baseDir, "libraries", "net", "minecraftforge", "forge", fullVer, "forge-"+fullVer+"-client.jar")
	}
	if fi, err := os.Stat(clientJar); err != nil || fi.Size() == 0 {
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

	// 3) Докачуємо/перевіряємо libraries лоадера через EnsureVersion (це
	//    завантажить і батьківську ванилла-версію теж).
	if _, err := inst.EnsureVersion(vj.ID); err != nil {
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

// installProfile — мінімальна модель install_profile.json інсталера.
type installProfile struct {
	JSON         string           `json:"json"`
	Minecraft    string           `json:"minecraft"`
	VersionInfo  *json.RawMessage `json:"versionInfo"`
	Libraries    []forgeLibrary   `json:"libraries"`
	Data         map[string]forgeDatum `json:"data"`
	Processors   []forgeProcessor `json:"processors"`
	Install      *struct {
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
//   а) сучасна (1.13+): downloads.artifact.{path,url,sha1} задані явно;
//   б) старіша/спрощена: лише "name" (+ опційний "url" репозиторію,
//      БЕЗ секції "downloads" узагалі) -- шлях і кінцевий URL треба
//      побудувати самим через gradlePath(name), так само, як
//      resolveLibraryArtifact() уже робить для fabric/quilt libraries
//      (installer.go). Без цього fallback такі бібліотеки просто мовчки
//      пропускались (URL == "" -> continue), а processor, якому вони
//      потрібні в classpath, падав з "класспас-бібліотека відсутня".
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
	Jar       string   `json:"jar"`
	Classpath []string `json:"classpath"`
	Args      []string `json:"args"`
	Sides     []string `json:"sides"`
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
	if _, err := inst.EnsureVersion(mcVersion); err != nil {
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
				tmp := filepath.Join(os.TempDir(), "shaurma-forge", sanitizeTempKey(cacheKey), filepath.FromSlash(pathInZip))
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
func runJavaProcessor(inst *Installer, procJar, mainClass string, cp, args []string, timeoutSec int) error {
	javaBin := "java"
	// Передаємо виконуваний файл Java, який використовує лаунчер для гри.
	if inst.procJava != "" {
		if _, err := os.Stat(inst.procJava); err == nil {
			javaBin = inst.procJava
		}
	}
	cmd := exec.Command(javaBin, append([]string{"-cp", strings.Join(cp, string(os.PathListSeparator)), mainClass}, args...)...)
	cmd.Dir = inst.baseDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
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
	if fi, err := os.Stat(dest); err == nil && fi.Size() > 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	if err := inst.downloadFile(url, dest); err != nil {
		os.Remove(dest)
		return err
	}
	// Перевірка: архів має бути валідним zip.
	if strings.HasSuffix(strings.ToLower(dest), ".jar") {
		if zr, err := zip.OpenReader(dest); err != nil {
			os.Remove(dest)
			return fmt.Errorf("пошкоджений архів %s: %w", dest, err)
		} else {
			zr.Close()
		}
	}
	return nil
}

var _ = runtime.GOOS // зберегти import runtime при подальших правках