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
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const manifestURL = "https://launchermeta.mojang.com/mc/game/version_manifest_v2.json"

type VersionManifest struct {
	Latest struct {
		Release  string `json:"release"`
		Snapshot string `json:"snapshot"`
	} `json:"latest"`
	Versions []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		URL  string `json:"url"`
	} `json:"versions"`
}

type Artifact struct {
	Path string `json:"path"`
	Sha1 string `json:"sha1"`
	Size int    `json:"size"`
	URL  string `json:"url"`
}

type LibraryJSON struct {
	Name string `json:"name"`
	// URL — репозиторій на рівні бібліотеки (Fabric/Quilt meta віддають
	// libraries БЕЗ downloads, лише name + url maven-репо). Звідси
	// резолвимо артефакт через gradlePath(name) — інакше loader jar
	// (fabric-loader.jar) НІКОЛИ не качається і гра падає з
	// "ClassNotFoundException: net.fabricmc.loader.impl.launch.knot.KnotClient".
	URL       string `json:"url"`
	Downloads struct {
		Artifact    Artifact            `json:"artifact"`
		Classifiers map[string]Artifact `json:"classifiers"`
	} `json:"downloads"`
	Rules []struct {
		Action string `json:"action"`
		OS     struct {
			Name string `json:"name"`
		} `json:"os"`
	} `json:"rules"`
}

// resolveLibraryArtifact заповнює Downloads.Artifact бібліотеки, у якої
// downloads.artifact відсутній, але є maven name + url репозиторію (типовий
// формат meta.fabricmc.net / meta.quiltmc.org). Повертає true, якщо
// артефакт тепер резолвиться (і Path, і URL заповнені).
func resolveLibraryArtifact(lib *LibraryJSON) bool {
	if lib == nil || lib.Name == "" {
		return false
	}
	if lib.Downloads.Artifact.URL != "" && lib.Downloads.Artifact.Path != "" {
		return true
	}
	// gradlePath повертає ПОВНИЙ шлях, включно з розширенням файлу
	// (типово .jar, або те, що вказано через "@ext" у maven-координаті,
	// напр. "...@zip" -> .zip) -- group/artifact/ver/artifact-ver(-classifier).ext
	rel := gradlePath(lib.Name)
	if rel == lib.Name || rel == "" {
		return false
	}
	path := rel
	base := lib.URL
	if base == "" {
		// Резерв: спільний репозиторій для maven-координат Mojang/Forge.
		base = "https://repo1.maven.org/maven2"
	}
	lib.Downloads.Artifact.Path = path
	lib.Downloads.Artifact.URL = strings.TrimRight(base, "/") + "/" + path
	return true
}

type VersionJSON struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	MainClass    string `json:"mainClass"`
	InheritsFrom string `json:"inheritsFrom,omitempty"`
	Jar          string `json:"jar,omitempty"`
	Assets       string `json:"assets"`
	AssetIndex   struct {
		ID        string `json:"id"`
		Sha1      string `json:"sha1"`
		Size      int    `json:"size"`
		TotalSize int    `json:"totalSize"`
		URL       string `json:"url"`
	} `json:"assetIndex"`
	Downloads struct {
		Client Artifact `json:"client"`
		ClientMappings struct {
			URL string `json:"url"`
		} `json:"client_mappings"`
		Server struct {
			URL string `json:"url"`
		} `json:"server"`
	} `json:"downloads"`
	Libraries []LibraryJSON `json:"libraries"`
	Logging   interface{}   `json:"logging"`
	Arguments struct {
		Game []interface{} `json:"game"`
		JVM  []interface{} `json:"jvm"`
	} `json:"arguments,omitempty"`
	MinecraftArguments string `json:"minecraftArguments,omitempty"`
	// JavaVersion — офіційне поле version.json з мінімальною/рекомендованою
	// major-версією Java для ЦІЄЇ КОНКРЕТНОЇ збірки (Mojang, а для
	// snapshot-ів з новими вимогами — і Fabric/Quilt у своєму profile
	// json теж часто дублюють це поле). Це набагато надійніше джерело,
	// ніж вгадування Java за номером версії MC: буває, що snapshot вже
	// вимагає новішу Java (напр. прапорець JVM-аргументів
	// --sun-misc-unsafe-memory-access=allow existує лише з Java 22+), а
	// наша власна евристика цього не знає. Якщо поле відсутнє в json
	// (MajorVersion == 0) — резолвер лишається на старій евристиці за
	// номером версії (RecommendedMajor).
	JavaVersion struct {
		Component    string `json:"component,omitempty"`
		MajorVersion int    `json:"majorVersion,omitempty"`
	} `json:"javaVersion,omitempty"`
}

// InstallProgress — структурований прогрес встановлення Minecraft
// (клієнтський jar, бібліотеки, асети, нативки). Замість текстових
// installer:log-рядків фронтенд отримує реальний % (подія
// "installer:progress") — ним живиться launch:progress (стадія loader).
type InstallProgress struct {
	Stage   string `json:"stage"` // client | libraries | assets | natives | loader
	Percent int    `json:"percent"`
	Message string `json:"message"`
}

// InstallProgressFunc — колбек прогресу встановлення.
type InstallProgressFunc func(p InstallProgress)

type Installer struct {
	client   *http.Client
	baseDir  string
	progress func(msg string)
	// installProgress — реальний прогрес з відсотками (див. InstallProgress).
	installProgress InstallProgressFunc
	// procJava — Java для запуску Forge processors (виконуваний файл).
	// Задається лаунчером перед EnsureLoader через SetProcessorJava.
	procJava string

	// lastPct/lastPctMu — монотонний %: прогрес ніколи не йде ВНИЗ (рекурсія
	// InheritsFrom: батько 0→100, потім нащадок починає з 5 — без затиску
	// бар стрибав би назад і виглядав «завислим/ламаним»).
	lastPct   int
	lastPctMu sync.Mutex
}

func NewInstaller(baseDir string) *Installer {
	return &Installer{
		client:  &http.Client{Timeout: 30 * time.Second},
		baseDir: baseDir,
	}
}

func (inst *Installer) SetProgressHandler(f func(string)) { inst.progress = f }

func (inst *Installer) log(msg string) {
	if inst.progress != nil {
		inst.progress(msg)
	}
}

// SetInstallProgressHandler реєструє колбек реального прогресу з відсотками
// (подія "installer:progress"). app.go під час запуску збірки тимчасово
// підвішує свій, що форвардить у launch:progress (стадія loader).
func (inst *Installer) SetInstallProgressHandler(f InstallProgressFunc) { inst.installProgress = f }

// InstallProgressHandler повертає поточний колбек прогресу (для тимчасового
// перевизначення з відновленням у app.go).
func (inst *Installer) InstallProgressHandler() InstallProgressFunc { return inst.installProgress }

func (inst *Installer) reportInstallProgress(stage string, pct int, msg string) {
	// Монотонність: новий % не може бути меншим за попередній (див. lastPct).
	inst.lastPctMu.Lock()
	if pct < inst.lastPct {
		pct = inst.lastPct
	} else {
		inst.lastPct = pct
	}
	inst.lastPctMu.Unlock()
	if inst.installProgress != nil {
		inst.installProgress(InstallProgress{Stage: stage, Percent: pct, Message: msg})
	}
}

func (inst *Installer) EnsureVersion(versionID string) (*VersionJSON, error) {
	// Новий цикл встановлення (EnsureVersion може викликатись багаторазово
	// на спільному Installer) — скидаємо монотонний % на старті, інакше
	// попередній 100% затиснув би прогрес цього запуску. Рекурсивний виклик
	// для InheritsFrom йде через ensureVersionInner, щоб СЕРЕДИНІ циклу
	// прогрес не скидався (бар лишається монотонним через увесь ланцюг).
	inst.lastPctMu.Lock()
	inst.lastPct = 0
	inst.lastPctMu.Unlock()
	return inst.ensureVersionInner(versionID)
}

func (inst *Installer) ensureVersionInner(versionID string) (*VersionJSON, error) {
	vDir := filepath.Join(inst.baseDir, "versions", versionID)
	os.MkdirAll(vDir, 0755)

	inst.reportInstallProgress("loader", 0, fmt.Sprintf("Встановлення Minecraft %s...", versionID))
	vjson, err := inst.loadOrFetchVersion(versionID)
	if err != nil {
		return nil, err
	}

	// Захист від "діри" в перевірці sha1 нижче: якщо це справжня vanilla-
	// версія (немає InheritsFrom — отже вона МАЄ мати власний
	// downloads.client за форматом Mojang version.json), а кешований на
	// диску versions/<id>/<id>.json чомусь не містить URL/sha1 клієнта
	// (лишився запис зі старішої версії лаунчера, чи файл був частково
	// записаний) — перезапитуємо офіційний маніфест з мережі. Без цього
	// needClientDownload нижче ніколи не спрацьовує (Sha1 == "" ⇒
	// перевірка мовчки пропускається), битий client.jar лишається
	// назавжди, і forge/neoforge processors постійно падають з checksum
	// mismatch на першому ж патчі.
	if vjson.InheritsFrom == "" && vjson.Downloads.Client.URL == "" {
		if fresh, ferr := inst.fetchVersionFromManifest(versionID); ferr == nil {
			vjson = fresh
			metaPath := filepath.Join(vDir, versionID+".json")
			if data, merr := json.MarshalIndent(vjson, "", "  "); merr == nil {
				os.WriteFile(metaPath, data, 0644)
			}
		}
	}

	if vjson.InheritsFrom != "" {
		inst.reportInstallProgress("loader", 2, "Перевірка базової версії...")
		parent, err := inst.ensureVersionInner(vjson.InheritsFrom)
		if err != nil {
			return nil, err
		}
		if vjson.MainClass == "" {
			vjson.MainClass = parent.MainClass
		}
		if vjson.Assets == "" {
			vjson.Assets = parent.Assets
		}
		if vjson.AssetIndex.URL == "" {
			vjson.AssetIndex = parent.AssetIndex
		}
	}

	clientPath := filepath.Join(vDir, versionID+".jar")
	// needClientDownload: якщо файл відсутній — качаємо однозначно. Якщо
	// присутній, але задана очікувана sha1 (Mojang завжди її дає) — звіряємо
	// проти диска: раніше тут була лише перевірка "файл існує", і битий
	// (не докачаний/пошкоджений) client jar з попереднього невдалого
	// запуску назавжди лишався на диску, ніколи не перекачувався і ламав
	// усі наступні кроки, що від нього залежать (forge/neoforge processors
	// падали з checksum mismatch на самому першому патчі, бо clean jar не
	// той, якого очікує binpatch).
	needClientDownload := false
	if _, err := os.Stat(clientPath); os.IsNotExist(err) {
		needClientDownload = true
	} else if vjson.Downloads.Client.Sha1 != "" {
		if actual, err := sha1OfFile(clientPath); err != nil || !strings.EqualFold(actual, vjson.Downloads.Client.Sha1) {
			inst.log(fmt.Sprintf("client.jar для %s пошкоджений або неповний — перекачую...", versionID))
			needClientDownload = true
		}
	}

	if needClientDownload {
		// Версія-нащадок може не мати власного клієнтського jar (лоадер) —
		// тоді jar ванилла-батька вже завантажено у теці батька, і для
		// запуску використовується ResolveClientJar. Качаємо лише якщо є
		// реальне посилання на клієнта.
		if vjson.Downloads.Client.URL != "" {
			inst.log(fmt.Sprintf("Завантаження Minecraft %s...", versionID))
			inst.reportInstallProgress("client", 5, fmt.Sprintf("Завантаження Minecraft %s...", versionID))
			// Клієнтський jar — 5→40% загального прогресу (по байтах).
			if err := inst.downloadFile(vjson.Downloads.Client.URL, clientPath, func(done, total int64) {
				pct := 5
				if total > 0 {
					pct = 5 + int(float64(done)*35/float64(total))
				}
				inst.reportInstallProgress("client", pct, fmt.Sprintf("Завантаження Minecraft %s... %d%%", versionID, pct))
			}, vjson.Downloads.Client.Sha1); err != nil {
				return nil, fmt.Errorf("download client: %w", err)
			}
		}
	}

	if err := inst.ensureLibraries(vjson); err != nil {
		return nil, err
	}

	if err := inst.ensureAssets(vjson); err != nil {
		inst.log(fmt.Sprintf("Попередження: assets: %v", err))
	}

	// Нативки: розпаковуємо natives-classifier jar у natives/<versionID>.
	inst.reportInstallProgress("natives", 92, "Розпаковка нативок...")
	if err := inst.ensureNatives(vjson, versionID); err != nil {
		inst.log(fmt.Sprintf("Попередження: natives: %v", err))
	}
	inst.reportInstallProgress("natives", 100, "Minecraft встановлено")

	metaPath := filepath.Join(vDir, versionID+".json")
	metaData, _ := json.MarshalIndent(vjson, "", "  ")
	os.WriteFile(metaPath, metaData, 0644)

	return vjson, nil
}

// ResolveClientJar повертає шлях до клієнтського jar версії, включно з
// ланцюгом InheritsFrom: для лоадер-версії (що не має власного jar) —
// jar ванилла-батька. Це потрібно для запуску через loader-профілі.
func (inst *Installer) ResolveClientJar(vjson *VersionJSON) string {
	cur := vjson
	for cur != nil {
		vDir := filepath.Join(inst.baseDir, "versions", cur.ID)
		jar := filepath.Join(vDir, cur.ID+".jar")
		if _, err := os.Stat(jar); err == nil {
			return jar
		}
		if cur.InheritsFrom == "" {
			break
		}
		parent, err := inst.loadVersionFromDisk(cur.InheritsFrom)
		if err != nil {
			break
		}
		cur = parent
	}
	return ""
}

// ensureLoaderClientJar гарантує існування jar версії-лоадера —
// versions/<loader-id>/<loader-id>.jar. Стандартна розкладка Mojang/Prism
// кладе в цей файл КЛІЄНТ ВАНИЛЛА (Forge 1.20.1 — саме такий лоадер: його
// version.json не має власного downloads.client, тому install jar не качає).
//
// Це критично для Forge 1.17+ (cpw.mods.bootstraplauncher): його
// -DignoreList закінчується ${version_name}.jar, і сам лоадер-джар на
// класшляху ігнорується (не стає окремим модулем). Якщо ж ігровий jar на
// класшляху — ванилла versions/<mc>/<mc>.jar (не збігається з ignoreList),
// bootstraplauncher модулює його як _1._20._1, і старт падає з
// "Modules minecraft and _1._20._1 export package net.minecraft.data to
// module ..." — split-package між автоматичним модулем ванилла-jar і
// модулем гри minecraft, який FML будує з forge-*-client.jar.
func ensureLoaderClientJar(baseDir string, vjson *VersionJSON) {
	if vjson == nil || vjson.ID == "" || vjson.InheritsFrom == "" {
		return // ванилла-версія або не лоадер
	}
	loaderJar := filepath.Join(baseDir, "versions", vjson.ID, vjson.ID+".jar")
	if _, err := os.Stat(loaderJar); err == nil {
		return // вже є — не чіпаємо
	}
	// Джерело — jar найглибшої (ванилла) версії ланцюга: ідемо лоадер →
	// батько → ... і лишаємо ОСТАННІЙ існуючий jar.
	src := ""
	for cur := vjson; cur != nil; cur = cur.inheritsFrom(baseDir) {
		c := filepath.Join(baseDir, "versions", cur.ID, cur.ID+".jar")
		if _, err := os.Stat(c); err == nil {
			src = c
		}
	}
	if src == "" || src == loaderJar {
		return
	}
	os.MkdirAll(filepath.Dir(loaderJar), 0755)
	in, err := os.Open(src)
	if err != nil {
		return
	}
	defer in.Close()
	out, err := os.Create(loaderJar)
	if err != nil {
		return
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return
	}
}

// loadVersionFromDisk читає збережений version.json версії зі спільної бази
// (без мережі). Використовується для обходу ланцюга InheritsFrom.
func (inst *Installer) loadVersionFromDisk(versionID string) (*VersionJSON, error) {
	metaPath := filepath.Join(inst.baseDir, "versions", versionID, versionID+".json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, err
	}
	var v VersionJSON
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func (inst *Installer) loadOrFetchVersion(versionID string) (*VersionJSON, error) {
	vDir := filepath.Join(inst.baseDir, "versions", versionID)
	metaPath := filepath.Join(vDir, versionID+".json")
	if data, err := os.ReadFile(metaPath); err == nil {
		var v VersionJSON
		if err := json.Unmarshal(data, &v); err == nil {
			return &v, nil
		}
	}

	vjson, err := inst.fetchVersionFromManifest(versionID)
	if err != nil {
		return nil, err
	}

	os.MkdirAll(vDir, 0755)
	data, _ := json.MarshalIndent(vjson, "", "  ")
	os.WriteFile(metaPath, data, 0644)

	return vjson, nil
}

// fetchVersionFromManifest якщо version.json з офіційного Mojang
// version_manifest_v2 — завжди мережевий запит, без звернення до
// кешу на диску. Використовується як (а) fallback у loadOrFetchVersion,
// коли кешу ще немає, і (б) примусовий рефреш у ensureVersionInner,
// коли кешований на диску .json виявився неповним (без downloads.client)
// і локальній копії довіряти не можна.
func (inst *Installer) fetchVersionFromManifest(versionID string) (*VersionJSON, error) {
	resp, err := inst.client.Get(manifestURL)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest: %w", err)
	}
	defer resp.Body.Close()

	var manifest VersionManifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, err
	}

	var versionURL string
	for _, v := range manifest.Versions {
		if v.ID == versionID {
			versionURL = v.URL
			break
		}
	}
	if versionURL == "" {
		return nil, fmt.Errorf("version %s not found", versionID)
	}

	resp2, err := inst.client.Get(versionURL)
	if err != nil {
		return nil, err
	}
	defer resp2.Body.Close()

	var vjson VersionJSON
	if err := json.NewDecoder(resp2.Body).Decode(&vjson); err != nil {
		return nil, err
	}

	return &vjson, nil
}

func (inst *Installer) ensureLibraries(vjson *VersionJSON) error {
	// Спершу порахуємо, скільки бібліотек реально треба качати — щоб %
	// був точним (бібліотеки займають 40→90% загального прогресу).
	type libTask struct {
		idx     int
		libPath string
	}
	tasks := []libTask{}
	for i := range vjson.Libraries {
		lib := &vjson.Libraries[i]
		if !inst.shouldInclude(*lib) {
			continue
		}
		// natives-only бібліотека (напр. net.java.jinput:jinput-platform:2.0.5):
		// downloads.artifact у профілі порожній (path/url "") — окремого
		// jar-артефакта не існує, є лише natives-класифікатори на
		// libraries.minecraft.net. Резолвити таку з maven-координат НЕ можна:
		// jar'а на maven-repo нема, download впаде 404 (саме це падало до
		// фікса). Такі бібліотеки обробляються лише в natives-проході нижче.
		if lib.Downloads.Artifact.Path == "" && lib.Downloads.Classifiers != nil {
			continue
		}
		// Fabric/Quilt meta: downloads.artifact порожній — резолвимо з
		// maven name + url репозиторію (див. resolveLibraryArtifact).
		// Записуємо резолв ПРЯМО в масив vjson, щоб BuildClassPath (який
		// читає meta-файл з диска) теж бачив Path/URL loader jar.
		if !resolveLibraryArtifact(lib) || lib.Downloads.Artifact.Path == "" {
			continue
		}
		libPath := filepath.Join(inst.baseDir, "libraries", lib.Downloads.Artifact.Path)
		if _, err := os.Stat(libPath); err == nil {
			continue
		}
		tasks = append(tasks, libTask{idx: i, libPath: libPath})
	}

	total := len(tasks)
	done := 0
	report := func(name string) {
		done++
		pct := 40
		if total > 0 {
			pct = 40 + done*50/total
		}
		inst.reportInstallProgress("libraries", pct, fmt.Sprintf("Бібліотеки: %d/%d", done, total))
	}

	for _, t := range tasks {
		lib := &vjson.Libraries[t.idx]
		os.MkdirAll(filepath.Dir(t.libPath), 0755)
		if err := inst.downloadFile(lib.Downloads.Artifact.URL, t.libPath); err != nil {
			return fmt.Errorf("library %s: %w", lib.Name, err)
		}
		report(lib.Name)
	}

	// natives-прохід окремо (а не в тому ж циклі, як було): по-перше, він
	// має охопити й natives-only бібліотеки (jinput-platform), яких нема в
	// tasks, а по-друге — не втрачати natives для бібліотек, чий артефакт
	// уже був на диску (раніше вони йшли далі через continue і natives не
	// докачувались). Скачуємо лише відсутні.
	for i := range vjson.Libraries {
		lib := &vjson.Libraries[i]
		if !inst.shouldInclude(*lib) || lib.Downloads.Classifiers == nil {
			continue
		}
		nativesKey := inst.getNativesKey()
		if nativesKey == "" {
			continue
		}
		classifier, ok := lib.Downloads.Classifiers[nativesKey]
		if !ok {
			continue
		}
		natPath := filepath.Join(inst.baseDir, "libraries", classifier.Path)
		if _, err := os.Stat(natPath); err == nil {
			continue
		}
		os.MkdirAll(filepath.Dir(natPath), 0755)
		if err := inst.downloadFile(classifier.URL, natPath); err != nil {
			return fmt.Errorf("natives %s: %w", lib.Name, err)
		}
		report(lib.Name)
	}
	return nil
}

func (inst *Installer) ensureAssets(vjson *VersionJSON) error {
	assetsDir := filepath.Join(inst.baseDir, "assets")
	indexesDir := filepath.Join(assetsDir, "indexes")
	objectsDir := filepath.Join(assetsDir, "objects")
	os.MkdirAll(indexesDir, 0755)
	os.MkdirAll(objectsDir, 0755)

	indexID := vjson.Assets
	if indexID == "" {
		indexID = "legacy"
	}

	indexPath := filepath.Join(indexesDir, indexID+".json")
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		if vjson.AssetIndex.URL == "" {
			return nil
		}
		if err := inst.downloadFile(vjson.AssetIndex.URL, indexPath); err != nil {
			return err
		}
	}

	return inst.ensureAssetObjects(indexPath, objectsDir)
}

// ensureAssetObjects завантажує всі об'єкти асетів (objects/<2>/<hash>) з
// індексу, яких ще немає на диску. Concurrency — маленький пул (I/O-bound).
func (inst *Installer) ensureAssetObjects(indexPath, objectsDir string) error {
	data, err := os.ReadFile(indexPath)
	if err != nil {
		return err
	}
	var index struct {
		Objects map[string]struct {
			Hash string `json:"hash"`
			Size int64  `json:"size"`
		} `json:"objects"`
	}
	if err := json.Unmarshal(data, &index); err != nil {
		return fmt.Errorf("парсинг asset index: %w", err)
	}
	if len(index.Objects) == 0 {
		return nil
	}

	// Швидкий прохід: які об'єкти вже є (stat дешевий), решта — качаємо.
	type assetEntry struct {
		dest string
		url  string
	}
	var toDownload []assetEntry
	for _, obj := range index.Objects {
		if obj.Hash == "" {
			continue
		}
		dest := filepath.Join(objectsDir, obj.Hash[:2], obj.Hash)
		// Файл вважається встановленим лише якщо існує І має очікуваний
		// розмір: порожній/обрізаний файл (урвана качка) не має рахуватись
		// як встановлений і лишатись назавжди без перекачки.
		if fi, err := os.Stat(dest); err == nil && fi.Size() == obj.Size {
			continue
		}
		toDownload = append(toDownload, assetEntry{
			dest: dest,
			url:  "https://resources.download.minecraft.net/" + obj.Hash[:2] + "/" + obj.Hash,
		})
	}
	if len(toDownload) == 0 {
		return nil
	}

	inst.log(fmt.Sprintf("Завантаження асетів: %d нових...", len(toDownload)))
	inst.reportInstallProgress("assets", 90, fmt.Sprintf("Асети: 0/%d", len(toDownload)))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var doneAssets int
	var firstErr error
	assetTotal := len(toDownload)
	for _, ae := range toDownload {
		wg.Add(1)
		go func(ae assetEntry) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := inst.downloadFile(ae.url, ae.dest); err != nil {
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("asset %s: %w", ae.url, err)
				}
				mu.Unlock()
			}
			mu.Lock()
			doneAssets++
			d := doneAssets
			mu.Unlock()
			pct := 90
			if assetTotal > 0 {
				pct = 90 + d*9/assetTotal
			}
			inst.reportInstallProgress("assets", pct, fmt.Sprintf("Асети: %d/%d", d, assetTotal))
		}(ae)
	}
	wg.Wait()

	if firstErr != nil {
		// Частина асетів могла не скачатись (мережа) — гра все одно може
		// працювати без них, тому це попередження, не фатальна помилка.
		inst.log(fmt.Sprintf("Попередження: деякі асети не завантажились: %v", firstErr))
	}
	return nil
}

// ensureNatives розпаковує natives-classifier jar (усіх версій ланцюга
// InheritsFrom) у теку natives/<versionID>. Шлях передається у JVM через
// -Djava.library.path (див. Launcher.Launch).
func (inst *Installer) ensureNatives(vjson *VersionJSON, versionID string) error {
	nativesDir := filepath.Join(inst.baseDir, "versions", versionID, "natives")
	if err := os.MkdirAll(nativesDir, 0755); err != nil {
		return err
	}

	nativesKey := inst.getNativesKey()
	if nativesKey == "" {
		return nil
	}

	seen := map[string]bool{}
	for cur := vjson; cur != nil; cur = cur.inheritsFrom(inst.baseDir) {
		if seen[cur.ID] {
			break
		}
		seen[cur.ID] = true
		for _, lib := range cur.Libraries {
			// ОЛД-формат version.json: natives-класифікатор закодований у
			// назві ("org.lwjgl:lwjgl:3.3.1:natives-windows") окремим
			// записом. Розпаковуємо jar лише потрібної платформи.
			if cl := libraryClassifier(lib.Name); cl != "" {
				if !nativesClassifierMatches(cl) {
					continue
				}
				jarPath := filepath.Join(inst.baseDir, "libraries", lib.Downloads.Artifact.Path)
				if _, err := os.Stat(jarPath); err != nil {
					continue
				}
				if err := extractNativeJar(jarPath, nativesDir); err != nil {
					inst.log(fmt.Sprintf("Попередження: natives %s: %v", jarPath, err))
				}
				continue
			}
			if lib.Downloads.Classifiers == nil {
				continue
			}
			classifier, ok := lib.Downloads.Classifiers[nativesKey]
			if !ok || classifier.Path == "" {
				continue
			}
			jarPath := filepath.Join(inst.baseDir, "libraries", classifier.Path)
			if _, err := os.Stat(jarPath); err != nil {
				continue
			}
			if err := extractNativeJar(jarPath, nativesDir); err != nil {
				inst.log(fmt.Sprintf("Попередження: natives %s: %v", jarPath, err))
			}
		}
	}
	return nil
}

// extractNativeJar розпаковує з natives-jar лише файли нативок (.dll/.so/
// .dylib/.jnilib), пропускаючи META-INF — за архітектурою MinecraftUpdate.
func extractNativeJar(jarPath, nativesDir string) error {
	zr, err := zip.OpenReader(jarPath)
	if err != nil {
		return err
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := filepath.ToSlash(f.Name)
		if strings.HasPrefix(name, "META-INF/") {
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".dll" && ext != ".so" && ext != ".dylib" && ext != ".jnilib" {
			continue
		}
		// macOS: .jnilib → .dylib
		filename := f.Name
		if runtime.GOOS == "darwin" && strings.HasSuffix(strings.ToLower(filename), ".jnilib") {
			filename = strings.TrimSuffix(filename, ".jnilib") + ".dylib"
		}
		dest := filepath.Join(nativesDir, filepath.Base(filename))
		if _, err := os.Stat(dest); err == nil {
			continue
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

func (inst *Installer) shouldInclude(lib LibraryJSON) bool {
	return libraryAllowed(lib)
}

// libraryAllowed визначає, чи бібліотека дозволена на поточній платформі
// згідно з rules (allow/disallow + os). Без rules — дозволена. Правила
// обробляються в порядку списку: останній збіг перемагає (Mojang-семантика).
// Використовується і при завантаженні, і при зборі classpath — інакше
// macOS-only бібліотеки (напр. ca.weblite:java-objc-bridge) потрапляють
// у Windows-classpath, і Forge 1.17.x bootstraplauncher (який валідує кожен
// запис classpath.txt через Files.readAllLines + Paths.get) падає
// NoSuchFileException.
func libraryAllowed(lib LibraryJSON) bool {
	if len(lib.Rules) == 0 {
		return true
	}
	allowed := false
	for _, rule := range lib.Rules {
		if rule.OS.Name == "" {
			allowed = rule.Action == "allow"
		} else if rule.OS.Name == runtime.GOOS {
			allowed = rule.Action == "allow"
		}
	}
	return allowed
}

func (inst *Installer) getNativesKey() string {
	return nativesBaseKey()
}

// downloadFile качає файл у dest. Якщо onBytes != nil — звітує прогрес
// по байтах (для реального % при качці великих файлів: клієнтський jar,
// asset index тощо).
// downloadFile качає url у dest. Пише спершу у тимчасовий файл поруч
// (dest + ".part") і лише при повному успіху (і, якщо переданий
// expectedSha1, при збігу контрольної суми) перейменовує його в dest —
// os.Rename атомарний у межах однієї файлової системи. Це усуває клас
// багів "битий файл на диску сприймається як готовий": раніше при
// обриві мережі/завершенні процесу посеред запису на диску лишався
// НЕПОВНИЙ файл із фінальним іменем, і подальші перевірки на кшталт
// os.Stat(dest) бачили "файл існує" й більше ніколи не перекачували
// його — саме так у клієнта виникав checksum mismatch у forge
// binarypatcher (net.mojang.blaze3d...binpatch Checksum: X, Exists:
// true, Reading patch — але фактичний вміст clean-jar не той) і
// безкінечна перерозпаковка forge-processors при кожному запуску.
func (inst *Installer) downloadFile(url, dest string, opts ...interface{}) error {
	var report func(done, total int64)
	var expectedSha1 string
	for _, o := range opts {
		switch v := o.(type) {
		case func(done, total int64):
			report = v
		case string:
			expectedSha1 = strings.ToLower(strings.TrimSpace(v))
		}
	}

	resp, err := inst.client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// ВАЖЛИВО: os.Create НЕ створює батьківських тек. Асети качаються в
	// objects/<xx>/<hash> — без MkdirAll кожен файл падав з "no such file
	// or directory", тіка лишалась порожньою, і асети намагались качатись
	// ЗАНОВО при кожному запуску гри.
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}

	tmpPath := dest + ".part"
	out, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	// tmpPath прибираємо в усіх випадках, окрім успішного rename в кінці
	// (де tmpPath уже не існує) — defer виконується після Close, тому
	// повторний Remove неіснуючого файлу просто не спрацює і не завадить.
	defer os.Remove(tmpPath)

	h := sha1.New()
	var w io.Writer = out
	if expectedSha1 != "" {
		w = io.MultiWriter(out, h)
	}

	total := resp.ContentLength
	var done int64
	buf := make([]byte, 64*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				out.Close()
				return werr
			}
			done += int64(n)
			if report != nil {
				report(done, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			out.Close()
			return rerr
		}
	}
	if err := out.Close(); err != nil {
		return err
	}

	if expectedSha1 != "" {
		actual := hex.EncodeToString(h.Sum(nil))
		if actual != expectedSha1 {
			return fmt.Errorf("sha1 mismatch: очікував %s, отримав %s", expectedSha1, actual)
		}
	}

	return os.Rename(tmpPath, dest)
}

// libraryGA повертає ключ "groupId:artifactId" (БЕЗ версії і класифікатора)
// з maven-координат бібліотеки (lib.Name, напр.
// "com.google.code.gson:gson:2.10.1" -> "com.google.code.gson:gson"). Це
// той самий ключ, за яким Prism (LaunchProfile::applyLibrary ->
// findLibraryByName/GradleSpecifier::matchName) звіряє бібліотеки різних
// компонентів між собою, ігноруючи версію.
// compareVersions порівнює дві версії у стилі semver/gradle
// (крапко-розділені числові сегменти, з можливим нечисловим суфіксом типу
// "-beta", "+build"). Повертає 1, якщо a > b; -1, якщо a < b; 0 якщо
// рівні або непорівнянні (тоді BuildClassPath трактує >= 0 як "новіша чи
// невідомо" і надає перевагу пізнішому в ланцюгу компоненту — так само
// чинить Prism при рівних версіях).
func compareVersions(a, b string) int {
	if a == b {
		return 0
	}
	an := splitVersionNumeric(a)
	bn := splitVersionNumeric(b)
	for i := 0; i < len(an) || i < len(bn); i++ {
		var av, bv int
		if i < len(an) {
			av = an[i]
		}
		if i < len(bn) {
			bv = bn[i]
		}
		if av != bv {
			if av > bv {
				return 1
			}
			return -1
		}
	}
	return 0
}

// splitVersionNumeric розбирає рядок версії на числові сегменти
// (роздільники: крапка, дефіс, плюс, підкреслення), зупиняючись на
// першому нечисловому сегменті.
func splitVersionNumeric(v string) []int {
	if v == "" {
		return nil
	}
	var out []int
	for _, seg := range strings.FieldsFunc(v, func(r rune) bool {
		return r == '.' || r == '-' || r == '+' || r == '_'
	}) {
		n := 0
		ok := true
		for _, ch := range seg {
			if ch < '0' || ch > '9' {
				ok = false
				break
			}
			n = n*10 + int(ch-'0')
		}
		if !ok {
			break
		}
		out = append(out, n)
	}
	return out
}

func libraryGA(name string) string {
	parts := strings.Split(name, ":")
	if len(parts) < 2 {
		return name
	}
	return parts[0] + ":" + parts[1]
}

// libraryClassifier витягує classifier з maven-координат
// "group:artifact:version[:classifier][@ext]" (частина після версії).
// Для основного jar (без classifier) повертає "". Для ОЛД-формату
// version.json (напр. "org.lwjgl:lwjgl:3.3.1:natives-windows") — повертає
// "natives-windows".
func libraryClassifier(name string) string {
	parts := strings.Split(name, ":")
	if len(parts) < 4 {
		return ""
	}
	c := parts[3]
	if i := strings.IndexByte(c, '@'); i >= 0 {
		c = c[:i]
	}
	return c
}

// libraryKey — ключ дедупу бібліотек: "groupId:artifactId" з урахуванням
// classifier. Для ОЛД-формату version.json, де кожен natives-classifier —
// ОКРЕМИЙ запис ("org.lwjgl:lwjgl:3.3.1:natives-windows"), classifier
// ІГНОРУВАТИ не можна: без нього основний jar і всі natives-варіанти
// зливаються в один ключ, і за дедупом (при рівних версіях) класшлях
// отримує ЛИШЕ останній natives (напр. ...-natives-windows-x86.jar), а
// основний "org.lwjgl:lwjgl:3.3.1.jar" (module org.lwjgl) зникає — модульний
// резолвер падає "Module org.lwjgl not found, required by org.lwjgl.natives".
func libraryKey(name string) string {
	key := libraryGA(name)
	if cl := libraryClassifier(name); cl != "" {
		key += ":" + cl
	}
	return key
}

// nativesBaseKey — базова назва natives-classifier для поточної ОС
// (без суфікса архітектури): "natives-windows", "natives-linux",
// "natives-osx". Для непідтримуваної ОС — "".
func nativesBaseKey() string {
	switch runtime.GOOS {
	case "windows":
		return "natives-windows"
	case "linux":
		return "natives-linux"
	case "darwin":
		return "natives-osx"
	}
	return ""
}

// nativesClassifierMatches — чи natives-classifier відповідає поточній
// платформі (ОС + архітектура). "natives-windows" — це amd64; "-x86" для
// 32-біт; "-arm64"/"-arm" для ARM. Фільтруємо за архітектурою, бо всі
// платформенні natives-джарі декларують module org.lwjgl.natives: якщо
// покласти на класшлях кілька одразу (natives-windows, -x86, -arm64,
// natives-linux, ...), модульний резолвер падає "duplicate module", і гра
// не стартує.
func nativesClassifierMatches(classifier string) bool {
	base := nativesBaseKey()
	if base == "" {
		return false
	}
	if classifier == base {
		return true
	}
	switch runtime.GOARCH {
	case "386":
		return classifier == base+"-x86"
	case "arm64":
		return classifier == base+"-arm64"
	case "arm":
		return classifier == base+"-arm"
	}
	return false
}

// libraryVersion повертає версійний сегмент maven-координат
// ("group:artifact:version") для порівняння, яка з двох бібліотек з
// однаковим GA новіша.
func libraryVersion(name string) string {
	parts := strings.Split(name, ":")
	if len(parts) < 3 {
		return ""
	}
	return parts[2]
}

// BuildClassPath збирає classpath з усього ланцюга InheritsFrom (loader +
// vanilla-батько), дедуплікуючи бібліотеки за maven-координатами
// "groupId:artifactId" — так само, як це робить Prism Launcher
// (LaunchProfile::applyLibrary): якщо одна й та сама бібліотека (напр.
// com.google.code.gson:gson) описана і в батьківському vanilla json, і в
// дочірньому forge/neoforge json, у підсумковий classpath потрапляє РІВНО
// ОДИН jar — новіший за версією, а при рівних версіях перевагу має
// дочірній (loader) компонент.
//
// Без цієї дедуплікації один і той самий шлях jar міг потрапити в -cp
// двічі, і NeoForge/Forge (cpw.mods.securejarhandler, який будує
// UnionFileSystem з classpath) падає на старті з
// "IllegalStateException: Duplicate key <шлях до jar>" — саме ця помилка
// й спостерігалась у логах запуску.
func BuildClassPath(baseDir string, vjson *VersionJSON, versionID string) string {
	var parts []string

	// Для лоадер-версій (Fabric/Forge/…) libraries описані у СВОЄМУ
	// version.json, а libraries ванилла-батька — у батьківському. Обходимо
	// весь ланцюг InheritsFrom, щоб класшлях містив і loader, і його базу.
	var chain []*VersionJSON
	seen := map[string]bool{}
	for cur := vjson; cur != nil; cur = cur.inheritsFrom(baseDir) {
		if seen[cur.ID] {
			break // захист від циклів
		}
		seen[cur.ID] = true
		chain = append(chain, cur)
	}

	// Плоский список бібліотек з усього ланцюга, дедуплікований за GA
	// (groupId:artifactId) + classifier, у порядку батьківський->дочірній
	// (щоб дочірні/loader-бібліотеки могли перевизначити батьківські).
	// classifier обов'язково входить у ключ: у ОЛД-форматі version.json
	// natives-варіанти ("org.lwjgl:lwjgl:3.3.1:natives-windows") — це ОКРЕМІ
	// записи, і дедуп за самим GA стирав основний jar на користь останнього
	// natives (див. libraryKey).
	byGA := map[string]LibraryJSON{}
	var order []string // порядок першої появи ключа — стабільний порядок classpath

	for i := len(chain) - 1; i >= 0; i-- {
		for _, lib := range chain[i].Libraries {
			if lib.Downloads.Artifact.Path == "" {
				continue
			}
			// OS-rules (allow/disallow): напр. java-objc-bridge доступна
			// лише на macOS — на Windows її не включаємо в classpath.
			if !libraryAllowed(lib) {
				continue
			}
			key := libraryKey(lib.Name)
			existing, ok := byGA[key]
			if !ok {
				byGA[key] = lib
				order = append(order, key)
				continue
			}
			// Вже бачили цю бібліотеку (і у vanilla-батька, і у
			// forge-нащадка, наприклад). Лишаємо новішу версію; при
			// рівності (чи якщо версії не порівнюються як числа)
			// перевагу віддаємо дочірньому компоненту — цикл iде від
			// найглибшого батька до нащадка, тож поточний lib і є
			// "пізнішим" за existing.
			if compareVersions(libraryVersion(lib.Name), libraryVersion(existing.Name)) >= 0 {
				byGA[key] = lib
			}
		}
	}

	for _, key := range order {
		lib := byGA[key]
		// ОЛД-формат: natives-варіант у назві (classifier). Кладемо на
		// класшлях ЛИШЕ natives поточної платформи. ВАЖЛИВО: фільтруємо
		// виключно classifier'и з префіксом "natives-" (natives-windows,
		// natives-linux, ...) — решта класифікаторів це ЗВИЧАЙНІ
		// бібліотеки, які обов'язково мають лишитись на класшляху. Напр.
		// "net.minecraftforge:mergetool:1.1.5:api" (classifier "api")
		// несе net.minecraftforge.api.distmarker.OnlyIn — без нього
		// ModLauncher падає "ClassNotFoundException: ...OnlyIn" →
		// RuntimeDistCleaner не вантажиться → "Dist Cleaner is missing".
		if cl := libraryClassifier(lib.Name); cl != "" && strings.HasPrefix(cl, "natives-") && !nativesClassifierMatches(cl) {
			continue
		}
		libPath := filepath.Join(baseDir, "libraries", lib.Downloads.Artifact.Path)
		parts = append(parts, libPath)

		// Новий формат: natives лежать у downloads.classifiers — беремо
		// той, що відповідає поточній платформі (ОС + архітектура).
		if lib.Downloads.Classifiers != nil {
			for cl, c := range lib.Downloads.Classifiers {
				if nativesClassifierMatches(cl) {
					parts = append(parts, filepath.Join(baseDir, "libraries", c.Path))
				}
			}
		}
	}

	// Клієнтський jar: у loader-версії власного jar немає — беремо jar
	// найглибшої (ванилла) версії ланцюга.
	clientJar := filepath.Join(baseDir, "versions", versionID, versionID+".jar")
	if _, err := os.Stat(clientJar); os.IsNotExist(err) {
		for _, v := range chain {
			candidate := filepath.Join(baseDir, "versions", v.ID, v.ID+".jar")
			if _, err := os.Stat(candidate); err == nil {
				clientJar = candidate
				break
			}
		}
	}
	parts = append(parts, clientJar)

	return strings.Join(parts, string(os.PathListSeparator))
}

// inheritsFrom читає батьківський version.json зі спільної бази (без
// мережі) або повертає nil, якщо батька немає.
func (v *VersionJSON) inheritsFrom(baseDir string) *VersionJSON {
	if v == nil || v.InheritsFrom == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(baseDir, "versions", v.InheritsFrom, v.InheritsFrom+".json"))
	if err != nil {
		return nil
	}
	var parent VersionJSON
	if err := json.Unmarshal(data, &parent); err != nil {
		return nil
	}
	return &parent
}

// IsLoader повертає true, якщо версія є лоадер-профілем (має батьківську
// ванилла-версію) — тоді assets/класшлях рахуються через ланцюг.
func (v *VersionJSON) IsLoader() bool {
	return v != nil && v.InheritsFrom != ""
}

// EffectiveJavaMajor повертає major-версію Java, яку ЯВНО вказує сам
// version.json (поле javaVersion.majorVersion), проходячи весь ланцюг
// InheritsFrom (loader-профіль зазвичай цього поля не має — воно є лише
// у батьківському ванилла-json, тому дивимось спочатку на себе, потім
// на батьків). Повертає 0, якщо жоден json у ланцюгу цього не вказує —
// у такому разі виклик має впасти на fallback-евристику RecommendedMajor.
//
// Це те саме джерело істини, яким користується Prism і офіційний Mojang
// launcher (напр. для Quilt 26.2 Mojang вказує компоненту
// java-runtime-epsilon → Java 25 — і саме її Prism реально запускає).
// Вгадування Java лише за номером версії MC (RecommendedMajor) — це
// fallback на випадок відсутності цього поля, а не основне джерело.
func EffectiveJavaMajor(vjson *VersionJSON, baseDir string) int {
	for cur := vjson; cur != nil; cur = cur.inheritsFrom(baseDir) {
		if cur.JavaVersion.MajorVersion > 0 {
			return cur.JavaVersion.MajorVersion
		}
	}
	return 0
}

// EffectiveJavaComponent повертає назву Java component з version.json
// (поле javaVersion.component, напр. "java-runtime-epsilon", "java-runtime-zeta"),
// проходячи весь ланцюг InheritsFrom. Повертає порожній рядок, якщо жоден
// json у ланцюгу не вказує component — у такому разі використовується
// fallback-маппінг за major версією (componentForMajor у java/installer.go).
//
// Це робить лаунчер повністю автономним: коли Mojang додасть Java 30 як
// "java-runtime-zeta", version.json нових версій MC міститиме це поле,
// і лаунчер автоматично завантажить правильну Java БЕЗ оновлення коду.
func EffectiveJavaComponent(vjson *VersionJSON, baseDir string) string {
	for cur := vjson; cur != nil; cur = cur.inheritsFrom(baseDir) {
		if cur.JavaVersion.Component != "" {
			return cur.JavaVersion.Component
		}
	}
	return ""
}

func GetAssetIndexPath(baseDir, versionID string) string {
	return filepath.Join(baseDir, "assets", "indexes", versionID+".json")
}