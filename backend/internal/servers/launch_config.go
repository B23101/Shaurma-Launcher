// launch_config.go — те, ЩО саме запускати для модованого сервера
// (аргумент-файл Forge/NeoForge, launch-jar Fabric/Quilt), вирахуване
// ОДИН РАЗ під час встановлення й збережене на диску, а не перевираховане
// при кожному Start().
//
// Перенесено з GDLauncher Carbon
// (crates/carbon_app/src/managers/server/modloader_launch.rs) —
// LaunchConfig/get_launch_config/save_launch_config/resolve_launch_config,
// наскільки дозволяють відмінності мов (Rust tokio::fs — Go os,
// async fn — синхронні функції; логіка й порядок перевірок ті самі).
package servers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"shaurma-launcher-backend/internal/atomicfile"
)

// LaunchConfig — те, ЩО саме запускати для сервера з модовим лоадером.
// Дзеркалить LaunchConfig з domain/server/mod.rs (Rust): усі поля
// nullable/порожні для vanilla та ядер без окремого launch-кроку
// (Paper/Purpur/Vanilla/Velocity далі й так шукають свій jar напряму в
// buildCommand — LaunchConfig стосується лише Forge/NeoForge/Fabric/Quilt,
// де встановлення дійсно вирішує щось нетривіальне).
type LaunchConfig struct {
	// ArgsFile — файл аргументів Java для розгортання при запуску (напр.
	// "libraries/net/neoforged/neoforge/21.1.77/unix_args.txt"), відносно
	// кореня сервера. Передається JVM як "@<path>" — саме так Forge й
	// NeoForge запускають сервер у власних run.sh/run.bat. JVM сама
	// токенізує файл, тож classpath, main class і game-аргументи беруться
	// з нього — інші поля не потрібні. Має пріоритет над MainClass і JarPath.
	ArgsFile string `json:"argsFile,omitempty"`
	// JarPath — заміна server.jar (напр. "fabric-server-launch.jar" або
	// Forge-шим/universal jar), відносно кореня сервера.
	JarPath string `json:"jarPath,omitempty"`
	// MainClass — явний main class (наразі не використовується жодним
	// встановленим ядром напряму, але лишається як прямий відповідник
	// LaunchConfig::main_class з оригіналу — на випадок майбутнього ядра
	// без ані args-файлу, ані самодостатнього jar).
	MainClass string `json:"mainClass,omitempty"`
}

// vanillaLaunchConfig — LaunchConfig::vanilla() з оригіналу: усі поля
// порожні. Це "нічого особливого запускати не треба" — buildCommand сам
// збере команду за ядром (server.jar/velocity.jar/тощо).
func vanillaLaunchConfig() LaunchConfig { return LaunchConfig{} }

// namesSomethingToLaunch — LaunchConfig, у якого ArgsFile, JarPath чи
// MainClass непорожні: "я знаю, що саме запускати". Порожній
// LaunchConfig (усі три поля "") — це і є "нічого не знайдено", той самий
// стан, що LaunchConfig::vanilla() у оригіналі.
func (c LaunchConfig) namesSomethingToLaunch() bool {
	return c.ArgsFile != "" || c.JarPath != "" || c.MainClass != ""
}

// launchConfigPath повертає шлях до launch_config.json у папці сервера.
// Аналог ServerPath::get_modloader_config_path() з оригіналу.
func launchConfigPath(serverDir string) string {
	return filepath.Join(serverDir, ".shaurma", "launch_config.json")
}

// getLaunchConfig читає launch_config.json. Якщо файлу нема — повертає
// vanillaLaunchConfig() (усі поля порожні), як LaunchConfig::vanilla() у
// get_launch_config оригіналу.
func getLaunchConfig(serverDir string) (LaunchConfig, error) {
	path := launchConfigPath(serverDir)
	if !fileExists(path) {
		return vanillaLaunchConfig(), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return LaunchConfig{}, fmt.Errorf("читання launch_config.json: %w", err)
	}
	var cfg LaunchConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return LaunchConfig{}, fmt.Errorf("парсинг launch_config.json: %w", err)
	}
	return cfg, nil
}

// saveLaunchConfig зберігає LaunchConfig у launch_config.json. Атомарний
// запис через internal/atomicfile — той самий хелпер, що вже
// використовується для server.json/settings.json/тощо в решті лаунчера
// (rationale: launch_config.json — так само персистентний конфіг, втрата
// якого посеред запису так само небажана, як для будь-якого іншого
// *.json файлу лаунчера).
func saveLaunchConfig(serverDir string, cfg LaunchConfig) error {
	return atomicfile.WriteJSONAtomic(launchConfigPath(serverDir), cfg)
}

// resolveLaunchConfig читає збережений launch_config.json і, якщо він
// існує але НЕ називає нічого для запуску (порожній — усі поля ""),
// пересканує диск на встановлений лоадер і, якщо щось знайшлось,
// перезаписує файл цим значенням.
//
// Перенесено з modloader_launch.rs::resolve_launch_config (рядки 32-63).
// ПРИЧИНА (дослівно з коментаря оригіналу, рядки 27-31 і 49-50):
// сервери, встановлені до виправлення пошуку args-файлу, мають конфіг з
// усіма порожніми полями launch — цей конфіг парситься чисто, тому шлях
// запуску інакше й далі стартував би модований сервер як vanilla, а
// сервер, створений не зі збірки, не має шляху перевстановлення для
// самостійного відновлення. Пересканований конфіг записується назад, щоб
// пошук на диску відбувся рівно один раз, а не при кожному старті.
//
// Якщо після пересканування на диску справді нічого немає (лоадер не
// встановлений) — ЗБЕРЕЖЕНИЙ (порожній) конфіг лишається як є: "genuinely
// broken install still surfaces as one instead of being papered over"
// (оригінал, рядки 49-50) — вигадувати щось замість чесної помилки не
// можна.
func resolveLaunchConfig(serverDir, core, loaderVer string) (LaunchConfig, error) {
	stored, err := getLaunchConfig(serverDir)
	if err != nil {
		return LaunchConfig{}, err
	}
	if stored.namesSomethingToLaunch() {
		return stored, nil
	}
	if core == "" {
		return stored, nil
	}

	derived, ok := existingInstallLaunchConfig(serverDir, core, loaderVer)
	if !ok {
		// Нічого встановленого на диску, звідки можна вивести конфіг —
		// лишаємо збережений (порожній) конфіг, нічого не вигадуємо.
		return stored, nil
	}

	if err := saveLaunchConfig(serverDir, derived); err != nil {
		return LaunchConfig{}, fmt.Errorf("збереження пересканованого launch_config.json: %w", err)
	}
	return derived, nil
}

// existingInstallLaunchConfig будує LaunchConfig за тим, що ФАКТИЧНО
// встановлено на диску, без завантаження чи запуску інсталятора. Друге
// повернене значення — false, якщо для вказаного ядра на диску нічого
// не знайдено.
//
// Перенесено з modloader_install.rs::existing_install_launch_config
// (рядки 599-632) — той самий порядок перевірок для кожного ядра.
func existingInstallLaunchConfig(serverDir, core, loaderVer string) (LaunchConfig, bool) {
	switch core {
	case CoreNeoForge:
		if argsFile, ok := findLoaderArgsFile(serverDir, filepath.Join("net", "neoforged", "neoforge"), loaderVer); ok {
			return LaunchConfig{ArgsFile: argsFile}, true
		}
		return LaunchConfig{}, false

	case CoreForge:
		// Спершу шукаємо launcher-jar (шим/universal) на корені — якщо є,
		// він самодостатній і не потребує args-файлу взагалі.
		if jar, ok := findForgeLauncherJar(serverDir); ok {
			return LaunchConfig{JarPath: jar}, true
		}
		if argsFile, ok := findLoaderArgsFile(serverDir, filepath.Join("net", "minecraftforge", "forge"), loaderVer); ok {
			return LaunchConfig{ArgsFile: argsFile}, true
		}
		return LaunchConfig{}, false

	case CoreFabric, CoreQuilt:
		jar := core + "-server-launch.jar"
		if fileExists(filepath.Join(serverDir, jar)) {
			return LaunchConfig{JarPath: jar}, true
		}
		return LaunchConfig{}, false

	default:
		return LaunchConfig{}, false
	}
}

// findForgeLauncherJar шукає самодостатній launcher-jar Forge (шим на
// сучасних версіях, universal jar на pre-1.17) на корені папки сервера.
// Виключає installer-jar — це інший файл, який треба ЗАПУСТИТИ, а не
// запускати НИМ.
//
// Перенесено з modloader_install.rs::find_forge_launcher_jar (рядки 581-590).
func findForgeLauncherJar(serverDir string) (string, bool) {
	entries, err := os.ReadDir(serverDir)
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "forge-") && strings.HasSuffix(name, ".jar") && !strings.Contains(name, "installer") {
			return name, true
		}
	}
	return "", false
}

// argsFileName повертає ім'я аргумент-файлу для поточної платформи.
// Перенесено з modloader_install.rs::platform_args_file_name (рядки
// 634-643); той самий вибір, що вже використовує buildForgeArgs/
// buildNeoForgeArgs у process.go (win_args.txt/unix_args.txt).
func argsFileName() string {
	if runtime.GOOS == "windows" {
		return "win_args.txt"
	}
	return "unix_args.txt"
}

// findLoaderArgsFile шукає аргумент-файл Forge/NeoForge, повертаючи його
// шлях відносно кореня сервера з прямими слешами — портативно між
// платформами і безпечно передавати JVM як "@<path>".
//
// Сучасні інсталятори (Forge 1.17+, кожен NeoForge) пишуть цей файл під
// libraries/<vendorPath>/<версія лоадера>/, а не в корені. preferredVersion
// передається, коли версія лоадера відома, щоб корінь з кількома
// встановленими версіями резолвився однозначно.
//
// Перенесено з modloader_install.rs::find_loader_args_file (рядки 653-696).
func findLoaderArgsFile(serverDir, vendorPath, preferredVersion string) (string, bool) {
	fileName := argsFileName()
	vendorDir := filepath.Join(serverDir, "libraries", vendorPath)

	var versions []string
	if entries, err := os.ReadDir(vendorDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if fileExists(filepath.Join(vendorDir, name, fileName)) {
				versions = append(versions, name)
			}
		}
	}

	var chosen string
	preferredFound := false
	if preferredVersion != "" {
		for _, v := range versions {
			if v == preferredVersion {
				preferredFound = true
				break
			}
		}
	}
	if preferredFound {
		chosen = preferredVersion
	} else if len(versions) > 0 {
		sort.Strings(versions)
		chosen = versions[len(versions)-1]
	}

	if chosen != "" {
		// filepath.Join використовує роздільник ОС; @-аргумент-файл іде
		// в JVM, якому потрібні прямі слеші на будь-якій платформі (так
		// само робить оригінал: "libraries/{vendor_path}/{version}/{file_name}").
		rel := filepath.Join("libraries", vendorPath, chosen, fileName)
		return filepath.ToSlash(rel), true
	}

	// Застарілий шар: деякі старі інсталятори клали файл у корінь.
	if fileExists(filepath.Join(serverDir, fileName)) {
		return fileName, true
	}

	return "", false
}
