// Package java — власна вбудована Java лаунчера.
//
// Лаунчер має свою теку з Java (~/.shaurma/java) і САМ встановлює туди
// JRE, якщо її ще немає. Завдяки цьому параметр javaPath за замовчуванням
// завжди вказує на реальний шлях, а користувачу не потрібно ставити
// Java вручну.
//
// ДЖЕРЕЛО ЗАВАНТАЖЕННЯ: офіційний Mojang java-runtime маніфест
// (piston-meta.mojang.com / launchermeta.mojang.com), той самий, яким
// користується vanilla Minecraft launcher і Prism Launcher. Раніше
// лаунчер качав Java напряму з Adoptium API (api.adoptium.net) — це
// давало ІНШУ збірку/вендора, ніж те, що реально ставить і тестує
// Mojang для кожної версії гри (напр. для нових snapshot-ів Mojang
// використовує Microsoft Build of OpenJDK для java-runtime-epsilon
// (Java 25), тоді як Adoptium міг віддавати щось інше під тим самим
// номером мажора). Використання того самого джерела, що й Prism,
// усуває розбіжності версій і поламані запуски на нових snapshot-ах.
package java

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// bundledFeatureVersion — «базова» Java, яку ставить майстер налаштувань
// за замовчуванням (java-runtime-delta від Mojang = Java 21).
// Показується лише як текст у статусі.
const bundledFeatureVersion = "21"

// javaRuntimeManifestURL — головний манифест Mojang: список компонентів
// (java-runtime-alpha/beta/gamma/delta/epsilon, jre-legacy) × ОС ×
// архітектура з посиланнями на пакетні манифести. Той самий URL, який
// використовує vanilla launcher і Prism.
const javaRuntimeManifestURL = "https://launchermeta.mojang.com/v1/products/java-runtime/2ec0cc96c44e5a76b9c8b7c39df7210883d12871/all.json"

// componentForMajor мапує major-версію Java на компонент Mojang-манифесту.
// Це FALLBACK для випадків, коли version.json не містить поля javaVersion.component.
// Порядок відповідає тому, що реально постачає Mojang: jre-legacy = 8,
// java-runtime-gamma = 17, java-runtime-delta = 21, java-runtime-epsilon = 25.
func componentForMajor(major int) (string, error) {
	switch major {
	case 8:
		return "jre-legacy", nil
	case 16:
		return "java-runtime-alpha", nil
	case 17:
		return "java-runtime-gamma", nil
	case 21:
		return "java-runtime-delta", nil
	case 25:
		return "java-runtime-epsilon", nil
	default:
		// Для невідомих версій (26+) спробуємо вгадати назву компонента.
		// Mojang іноді використовує грецький алфавіт для нових runtime:
		// alpha(16), gamma(17), delta(21), epsilon(25) → наступні: zeta, eta, theta
		// Якщо Mojang додасть новий компонент у manifest, ця евристика спрацює.
		guessed := guessComponentName(major)
		return guessed, fmt.Errorf("Java %d не має вбудованого маппінгу (підтримуються: 8, 16, 17, 21, 25). Спроба використати вгаданий компонент %q. Якщо це не спрацює, переконайтесь що version.json містить поле javaVersion.component", major, guessed)
	}
}

// guessComponentName намагається вгадати назву компонента для невідомих Java версій.
// Це страховка на випадок, якщо Mojang додасть новий runtime у manifest,
// але version.json чомусь не містить component (малоймовірно, але можливо).
func guessComponentName(major int) string {
	// Грецький алфавіт (після epsilon):
	// zeta (ζ), eta (η), theta (θ), iota (ι), kappa (κ), lambda (λ), mu (μ)
	greekNames := map[int]string{
		26: "java-runtime-zeta",
		27: "java-runtime-eta",
		28: "java-runtime-theta",
		29: "java-runtime-iota",
		30: "java-runtime-kappa",
		31: "java-runtime-lambda",
		32: "java-runtime-mu",
		33: "java-runtime-nu",
		34: "java-runtime-xi",
		35: "java-runtime-omicron",
	}
	
	if name, ok := greekNames[major]; ok {
		return name
	}
	
	// Якщо major > 35, використовуємо універсальний формат
	// (можливо Mojang перейде на просту нумерацію)
	return fmt.Sprintf("java-runtime-major-%d", major)
}

// supportedMajors — мажори, які лаунчер має вбудовані (для фолбеку).
// Для нових версій Java (26+) лаунчер використовує component з version.json.
var supportedMajors = []int{8, 16, 17, 21, 25}

// mojangOSKey — ключ ОС/архітектури в манифесті Mojang (windows-x64,
// windows-arm64, windows-x86, linux, linux-i386, mac-os, mac-os-arm64).
func mojangOSKey() string {
	switch runtime.GOOS {
	case "windows":
		switch runtime.GOARCH {
		case "arm64":
			return "windows-arm64"
		case "386":
			return "windows-x86"
		default:
			return "windows-x64"
		}
	case "darwin":
		if runtime.GOARCH == "arm64" {
			return "mac-os-arm64"
		}
		return "mac-os"
	case "linux":
		if runtime.GOARCH == "386" {
			return "linux-i386"
		}
		return "linux"
	default:
		return "linux"
	}
}

// Progress — прогрес встановлення Java з відсотками. Замість текстового
// «Завантаження Java...» фронтенд отримує реальний % (подія "java:progress"):
// manifest 0→5, download 5→100. Той самий прогрес живить launch:progress
// (стадія java) під час запуску збірки.
type Progress struct {
	Phase   string `json:"phase"` // "manifest" | "download" | "extract"
	Percent int    `json:"percent"`
	Message string `json:"message"`
}

// ProgressFunc — колбек прогресу встановлення Java.
type ProgressFunc func(p Progress)

// Installer керує вбудованою Java: перевіряє наявність, завантажує JRE
// з офіційного манифесту Mojang і розкладає у теку лаунчера.
type Installer struct {
	mu         sync.Mutex // захист від паралельного встановлення
	dir        string
	client     *http.Client
	onStatus   func(msg string)
	onProgress ProgressFunc
}

func NewInstaller(javaDir string) *Installer {
	return &Installer{
		dir: javaDir,
		client: &http.Client{
			Timeout: 30 * time.Minute,
		},
	}
}

// SetStatusHandler реєструє колбек для прогресу встановлення
// (рядки українською, як у решти лаунчера). Фронтенд отримує їх
// подіями "java:status".
func (j *Installer) SetStatusHandler(f func(string)) { j.onStatus = f }

func (j *Installer) status(msg string) {
	if j.onStatus != nil {
		j.onStatus(msg)
	}
}

// SetProgressHandler реєструє колбек реального прогресу з відсотками
// (подія "java:progress"). Відрізняється від SetStatusHandler тим, що
// несе Percent — фронтенд малює реальний прогрес-бар, а не текст.
func (j *Installer) SetProgressHandler(f ProgressFunc) { j.onProgress = f }

// ProgressHandler повертає поточний колбек прогресу (для тимчасового
// перевизначення під час запуску збірки: app.go підвішує свій, що
// форвардить у launch:progress, а потім відновлює загальний).
func (j *Installer) ProgressHandler() ProgressFunc { return j.onProgress }

func (j *Installer) reportProgress(phase string, pct int, msg string) {
	if j.onProgress != nil {
		j.onProgress(Progress{Phase: phase, Percent: pct, Message: msg})
	}
}

// exeName — ім'я виконуваного файлу JVM залежно від ОС (javaw.exe на
// Windows; на Linux/macOS манифест Mojang кладе лише "java", без "javaw").
func exeName() string {
	if runtime.GOOS == "windows" {
		return "javaw.exe"
	}
	return "java"
}

// ExePath — очікуваний шлях до виконуваного файлу вбудованої Java.
// Саме на нього дивляться дефолти (config.normalize, GetWizardDefaults,
// DetectJava), тому після встановлення розкладка Java лежить так,
// що bin/<exe> знаходиться напряму в j.dir.
func (j *Installer) ExePath() string {
	return filepath.Join(j.dir, "bin", exeName())
}

// IsInstalled — чи Java вже розкладена на диску.
func (j *Installer) IsInstalled() bool {
	_, err := os.Stat(j.ExePath())
	return err == nil
}

// Version — версія вбудованої Java, що ставиться лаунчером.
func (j *Installer) Version() string {
	return bundledFeatureVersion + " (Mojang java-runtime-delta)"
}

// MajorExePath — шлях до виконуваного файлу конкретного мажора Java у
// теці лаунчера. КОЖЕН мажор живе у своїй підтеці java-<major>/ і ніколи
// не видаляється при запуску іншої збірки (як у старому лаунчері/Prism):
// 21 у java-21/, 17 у java-17/ тощо. Корінь javaDir (ExePath) — лише
// «базова» Java 21 для майстра/статусу; запуск гри через EnsureMajor
// користується підтеками.
func (j *Installer) MajorExePath(major int) string {
	return filepath.Join(j.dir, fmt.Sprintf("java-%d", major), "bin", exeName())
}

// MajorInstalled — чи розкладена Java конкретного мажора на диску.
func (j *Installer) MajorInstalled(major int) bool {
	_, err := os.Stat(j.MajorExePath(major))
	return err == nil
}

// EnsureMajor встановлює (при потребі) Java конкретного мажора і повертає
// шлях до виконуваного файлу. Качає з офіційного манифесту Mojang (той
// самий, що й Prism/vanilla launcher).
//
// ВАЖЛИВО (фікс «качає і видаляє»): кожен мажор живе у СВОЇЙ теці
// java-<major>/ і ставиться лише один раз. Якщо потрібна Java вже
// встановлена (у своїй теці або, для старого стану, в корені javaDir) —
// повертаємо її як є і НЕ чіпаємо інші мажори.
func (j *Installer) EnsureMajor(major int) (string, error) {
	return j.EnsureMajorWithComponent(major, "")
}

// EnsureMajorWithComponent встановлює Java, використовуючи component name
// з version.json (якщо є) або fallback маппінг за major версією.
// Це робить лаунчер автономним - нові версії Java (26+) підтримуються
// автоматично, якщо Mojang додає їх у manifest.
func (j *Installer) EnsureMajorWithComponent(major int, component string) (string, error) {
	// Якщо component не вказаний, спробуємо отримати з вбудованого маппінгу
	var fallbackErr error
	if component == "" {
		component, fallbackErr = componentForMajor(major)
		// Якщо є помилка (невідома версія), component все одно містить вгаданну назву
		// Спробуємо її використати, але запишемо попередження
		if fallbackErr != nil {
			// Логуємо попередження, але продовжуємо спробу
			j.status(fmt.Sprintf("Попередження: %v", fallbackErr))
		}
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	// 1) Своя тека java-<major>/ — нова розкладка, пріоритет.
	exe := j.MajorExePath(major)
	if cur := DetectMajor(exe); cur == major {
		return exe, nil
	}
	// 2) Корінь javaDir — старий стан (майстер ставить туди базову Java 21;
	//    старі інсталятори могли лишити там інший мажор). Якщо в корені
	//    вже є ПОТРІБНИЙ мажор — використовуємо його, не перекачуючи.
	if cur := DetectMajor(j.ExePath()); cur == major {
		return j.ExePath(), nil
	}
	// 3) Немає — ставимо у СВОЮ теку java-<major>/. Корінь НЕ чіпаємо:
	//    там може лежати інший мажор (напр. 21 від майстра) — перезапис
	//    зламав би його для інших збірок.
	if err := j.installComponent(component, major, filepath.Join(j.dir, fmt.Sprintf("java-%d", major))); err != nil {
		// Якщо встановлення не вдалося і був fallback error, додаємо контекст
		if fallbackErr != nil {
			return "", fmt.Errorf("не вдалося встановити Java %d з компонентом %q (вгаданий fallback): %w. Оригінальна помилка: %v", major, component, err, fallbackErr)
		}
		return "", err
	}
	return j.MajorExePath(major), nil
}

// EnsureInstalled перевіряє Java і, якщо її немає, завантажує та
// розкладає. Повертає шлях до виконуваного файлу.
//
// Мютекс: EnsureInstalled може викликатись одночасно з двох місць
// (кнопка «Встановити Java» у майстрі + автовстановлення при запуску
// гри). Другий виклик чекає, поки перший завершиться, і просто повертає
// вже розкладений шлях — жодного подвійного завантаження чи гонитви.
func (j *Installer) EnsureInstalled() (string, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.IsInstalled() {
		return j.ExePath(), nil
	}
	if err := j.install(); err != nil {
		return "", err
	}
	return j.ExePath(), nil
}

func (j *Installer) install() error {
	// Майстер ставить базову Java 21 у КОРІНЬ javaDir (ExePath) — так
	// на неї дивляться дефолти, статус і старий код (EnsureInstalled).
	return j.installComponent("java-runtime-delta", 21, j.dir)
}

// ─── Формат манифесту Mojang ────────────────────────────────────────────

type mojangAllManifest map[string]map[string][]struct {
	Manifest struct {
		SHA1 string `json:"sha1"`
		URL  string `json:"url"`
	} `json:"manifest"`
	Version struct {
		Name string `json:"name"`
	} `json:"version"`
}

type mojangPackageFile struct {
	Type       string `json:"type"`
	Executable bool   `json:"executable"`
	Target     string `json:"target"` // для symlink (тільки *nix)
	Downloads  struct {
		Raw struct {
			SHA1 string `json:"sha1"`
			Size int64  `json:"size"`
			URL  string `json:"url"`
		} `json:"raw"`
	} `json:"downloads"`
}

type mojangPackageManifest struct {
	Files map[string]mojangPackageFile `json:"files"`
}

// installComponent встановлює Java з Mojang manifest, використовуючи
// вказаний component name (напр. "java-runtime-epsilon", "java-runtime-zeta").
// Це дозволяє підтримувати нові версії Java автоматично, без оновлення лаунчера.
func (j *Installer) installComponent(component string, major int, targetDir string) error {
	os.MkdirAll(targetDir, 0755)

	j.status(fmt.Sprintf("Отримання маніфесту Java %d (%s)...", major, component))
	j.reportProgress("manifest", 0, fmt.Sprintf("Отримання маніфесту Java %d...", major))

	pkgManifestURL, err := j.resolvePackageManifestURL(component)
	if err != nil {
		return fmt.Errorf("java manifest для %s: %w", component, err)
	}

	pkg, err := j.fetchPackageManifest(pkgManifestURL)
	if err != nil {
		return fmt.Errorf("java package manifest для %s: %w", component, err)
	}
	j.reportProgress("manifest", 5, "Маніфест отримано — завантажуємо файли...")

	j.status(fmt.Sprintf("Завантаження Java %d (%s)...", major, component))
	if err := j.downloadPackageFiles(pkg, targetDir, major); err != nil {
		return fmt.Errorf("download java %s: %w", component, err)
	}
	j.reportProgress("extract", 100, "Java встановлено")

	if _, err := os.Stat(filepath.Join(targetDir, "bin", exeName())); err != nil {
		return fmt.Errorf("вбудовану Java %d (%s) не вдалося встановити: %s не знайдено після завантаження", major, component, exeName())
	}
	j.status(fmt.Sprintf("Java %d (%s) встановлено: %s", major, component, filepath.Join(targetDir, "bin", exeName())))
	return nil
}

// installMajorTo — застаріла функція, викликає installComponent з fallback маппінгом
func (j *Installer) installMajorTo(major int, targetDir string) error {
	component, err := componentForMajor(major)
	if err != nil {
		return err
	}
	return j.installComponent(component, major, targetDir)
}

// resolvePackageManifestURL тягне головний манифест Mojang і повертає URL
// пакетного манифесту для потрібного компонента на поточній ОС/архітектурі.
func (j *Installer) resolvePackageManifestURL(component string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, javaRuntimeManifestURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "ShaurmaLauncher/2.0")

	resp, err := j.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d при отриманні маніфесту java-runtime", resp.StatusCode)
	}

	var all mojangAllManifest
	if err := json.NewDecoder(resp.Body).Decode(&all); err != nil {
		return "", fmt.Errorf("парсинг маніфесту java-runtime: %w", err)
	}

	osKey := mojangOSKey()
	osEntries, ok := all[osKey]
	if !ok {
		return "", fmt.Errorf("немає записів java-runtime для ОС %q у маніфесті Mojang", osKey)
	}
	entries, ok := osEntries[component]
	if !ok || len(entries) == 0 {
		return "", fmt.Errorf("немає доступного компонента %q для ОС %q у маніфесті Mojang", component, osKey)
	}
	return entries[0].Manifest.URL, nil
}

// fetchPackageManifest завантажує і парсить пакетний манифест (список
// окремих файлів з їхніми SHA1 та URL) для конкретного рантайму.
func (j *Installer) fetchPackageManifest(url string) (*mojangPackageManifest, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ShaurmaLauncher/2.0")

	resp, err := j.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d при отриманні пакетного маніфесту", resp.StatusCode)
	}

	var pkg mojangPackageManifest
	if err := json.NewDecoder(resp.Body).Decode(&pkg); err != nil {
		return nil, fmt.Errorf("парсинг пакетного маніфесту: %w", err)
	}
	return &pkg, nil
}

// downloadPackageFiles розкладає директорії/symlink-и й завантажує кожен
// файл із пакетного манифесту в targetDir. Прогрес рахується від
// кількості файлів (5→100% загального прогресу встановлення).
func (j *Installer) downloadPackageFiles(pkg *mojangPackageManifest, targetDir string, major int) error {
	// Спершу директорії (щоб MkdirAll файлів не гнався за порядком мапи).
	for relPath, f := range pkg.Files {
		if f.Type == "directory" {
			if err := os.MkdirAll(filepath.Join(targetDir, filepath.FromSlash(relPath)), 0755); err != nil {
				return err
			}
		}
	}

	type job struct {
		relPath string
		f       mojangPackageFile
	}
	var jobs []job
	for relPath, f := range pkg.Files {
		if f.Type == "file" {
			jobs = append(jobs, job{relPath, f})
		}
	}

	total := len(jobs)
	done := 0
	lastReport := time.Now()
	for _, jb := range jobs {
		dest := filepath.Join(targetDir, filepath.FromSlash(jb.relPath))
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return err
		}
		if err := j.downloadOneFile(jb.f.Downloads.Raw.URL, dest); err != nil {
			return fmt.Errorf("файл %s: %w", jb.relPath, err)
		}
		if jb.f.Executable {
			os.Chmod(dest, 0755)
		}
		done++
		if time.Since(lastReport) >= 150*time.Millisecond || done == total {
			pct := 5
			if total > 0 {
				pct = 5 + done*95/total
			}
			j.reportProgress("download", pct, fmt.Sprintf("Завантаження Java %d... %d/%d файлів", major, done, total))
			lastReport = time.Now()
		}
	}

	// symlink-и — лише на *nix (Windows-манифест Mojang symlink не містить).
	for relPath, f := range pkg.Files {
		if f.Type == "link" && f.Target != "" {
			dest := filepath.Join(targetDir, filepath.FromSlash(relPath))
			os.MkdirAll(filepath.Dir(dest), 0755)
			os.Remove(dest)
			os.Symlink(f.Target, dest)
		}
	}

	return nil
}

// downloadOneFile качає один файл з Mojang CDN (piston-data.mojang.com) у
// dest. Файли зазвичай невеликі (найбільший — jvm.dll, кілька МБ), тому
// окремий прогрес по кожному файлу не потрібен — прогрес рахується по
// кількості завершених файлів (downloadPackageFiles).
func (j *Installer) downloadOneFile(url, dest string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "ShaurmaLauncher/2.0")

	resp, err := j.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d (%s)", resp.StatusCode, url)
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}
