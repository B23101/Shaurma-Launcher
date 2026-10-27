// install.go — завантаження та встановлення ядра сервера (Vanilla/Paper/
// Purpur/Fabric/Quilt/Forge/NeoForge/Velocity). Раніше ця логіка існувала
// ЛИШЕ у integration_test.go (тест сам качав jar в обхід продакшн-коду) —
// CreateServer писав тільки server.json, а InstallCore ніхто не викликав.
// Тепер install відбувається ЛІНИВО: так само, як збірка створюється без
// завантаження Minecraft-компонентів (це відбувається лише при першому
// запуску), CreateServer лише зберігає конфіг, а InstallCore викликається
// з Manager.Start() перед стартом java, якщо ядра ще немає на диску.
//
// Провайдер-абстракція (coreInstaller/coreInstallers нижче) і
// LaunchConfig-персистентність перенесені з GDLauncher Carbon
// (crates/carbon_app/src/managers/server/provider.rs,
// modloader_install.rs) — детальні коментарі-причини на місці кожного
// перенесеного рішення.
package servers

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ServerInstallProgress — структурований прогрес встановлення ядра сервера.
// Дзеркалює java.Progress/minecraft.InstallProgress для клієнтського запуску.
// Стрімиться окремим каналом "server:install-progress" (а НЕ "server:line"),
// тому консоль сервера лишається чистою до першого реального виводу java.
type ServerInstallProgress struct {
	Stage   string `json:"stage"`   // "java" | "core"
	Percent int    `json:"percent"` // 0-100, -1 = невідомо (без шкали)
	Message string `json:"message"`
}

// ServerInstallProgressFunc — колбек прогресу для InstallCore та
// пов'язаних функцій. Замінює старий InstallProgress func(text string).
type ServerInstallProgressFunc func(p ServerInstallProgress)

// InstallProgress — застарілий тип, зберігається для сумісності з
// installLogAdapter (installPurpur використовує інтерфейс Logf).
type InstallProgress func(text string)

// coreInstaller — єдиний внутрішній інтерфейс встановлення ядра. Кожне
// ядро реєструє свою реалізацію в coreInstallers нижче; CoreInstalled і
// InstallCore лише диспетчерять через мапу замість дублювання switch на
// 8 кейсів у двох місцях (як було раніше).
//
// Перенесено концептуально з provider.rs::ServerProvider (Rust trait
// object) — Go-варіант через map[string]coreInstaller, оскільки в Go
// немає прямого відповідника trait-об'єктів для цього випадку (звичайний
// switch на 8 галузей і так вимагав би того самого dispatch-у; мапа з
// одним інтерфейсом прибирає дублювання, не додаючи абстракції, якої тут
// не було б і в ідіоматичному Go).
//
// install повертає (loaderVer, launchConfig, err): loaderVer — фактично
// встановлена версія лоадера (порожньо, якщо ядро її не має, напр.
// Vanilla/Paper/Purpur), launchConfig — розділ 1.1/1.2: ЩО саме запускати,
// вирахуване один раз тут, а не при кожному Start().
type coreInstaller interface {
	// installed повідомляє, чи ядро вже встановлене на диску (без мережі).
	installed(srv *Server) bool
	// install якає та встановлює ядро. ctx дозволяє скасувати мережевий
	// запит чи дочірній процес інсталятора (розділ 1.4).
	install(ctx context.Context, srv *Server, javaPath string, onProgress ServerInstallProgressFunc) (loaderVer string, launchConfig LaunchConfig, err error)
}

// coreInstallers — реєстр coreInstaller за ядром. Заповнюється в init()
// нижче, по одному запису на кожен switch-кейс, що раніше дублювався в
// CoreInstalled і InstallCore.
var coreInstallers map[string]coreInstaller

func init() {
	coreInstallers = map[string]coreInstaller{
		CoreVanilla:  vanillaInstaller{},
		CorePaper:    paperInstaller{},
		CorePurpur:   purpurInstaller{},
		CoreFabric:   fabricInstaller{},
		CoreQuilt:    quiltInstaller{},
		CoreForge:    forgeInstaller{},
		CoreNeoForge: neoForgeInstaller{},
		CoreVelocity: velocityInstaller{},
	}
}

// CoreInstalled перевіряє, чи ядро сервера вже встановлене на диску
// (тобто чи можна одразу запускати java, не якаючи нічого заново).
func CoreInstalled(srv *Server) bool {
	inst, ok := coreInstallers[srv.Core]
	if !ok {
		return fileExists(filepath.Join(srv.Dir, "server.jar")) // невідоме ядро — фолбек, як vanilla
	}
	return inst.installed(srv)
}

// InstallCore качає та встановлює ядро вказаного сервера. Ідемпотентно:
// якщо ядро вже стоїть (CoreInstalled), одразу повертає nil без мережі.
// javaPath — шлях до виконуваного файлу Java, отриманий з java.Installer
// (той самий, що використовує клієнтський запуск — БЕЗ системного пошуку).
//
// Публічна сигнатура — жорсткий контракт з api.go (не приймає
// context.Context: викликається синхронно з єдиного місця, скасування
// зсередини використовує context.Background() — розділ 1.4 нижче описує,
// де саме контекст РЕАЛЬНО поширюється).
func InstallCore(srv *Server, javaPath string, onProgress ServerInstallProgressFunc) error {
	if onProgress == nil {
		onProgress = func(ServerInstallProgress) {}
	}
	if CoreInstalled(srv) {
		return nil
	}
	if err := os.MkdirAll(srv.Dir, 0755); err != nil {
		return fmt.Errorf("створення папки сервера: %w", err)
	}

	inst, ok := coreInstallers[srv.Core]
	if !ok {
		return fmt.Errorf("невідоме ядро: %s", srv.Core)
	}

	loaderVer, launchConfig, err := inst.install(context.Background(), srv, javaPath, onProgress)
	if err != nil {
		return err
	}
	if loaderVer != "" {
		srv.LoaderVer = loaderVer
	}

	// Розділ 1.1: зберігаємо знайдений launch-конфіг ОДИН РАЗ тут, а не
	// перевираховуємо його в process.go при кожному Start(). Порожній
	// launchConfig (Vanilla/Paper/Purpur/Velocity — ядра, де install не
	// повертає нічого специфічного) зберігається так само: наступний
	// resolveLaunchConfig побачить конфіг, що "нічого не називає", і для
	// цих ядер це й правильно — вони не проходять через LaunchConfig
	// узагалі, buildCommand визначає їхній jar напряму (розділ 2.2).
	if launchConfig.namesSomethingToLaunch() {
		if err := saveLaunchConfig(srv.Dir, launchConfig); err != nil {
			return fmt.Errorf("збереження launch_config.json: %w", err)
		}
	}

	return nil
}

// installLogAdapter підлаштовує InstallProgress під сигнатуру
// interface{ Logf(string, ...any) }, яку очікує installPurpur (написана під
// тестовий t.Logf ще з часів, коли install викликався лише з тестів).
type installLogAdapter struct{ fn InstallProgress }

func (a installLogAdapter) Logf(format string, args ...any) {
	if a.fn != nil {
		a.fn(fmt.Sprintf(format, args...))
	}
}

// ── Vanilla ────────────────────────────────────────────────────────────

type vanillaInstaller struct{}

func (vanillaInstaller) installed(srv *Server) bool {
	return fileExists(filepath.Join(srv.Dir, "server.jar"))
}

func (vanillaInstaller) install(ctx context.Context, srv *Server, javaPath string, onProgress ServerInstallProgressFunc) (string, LaunchConfig, error) {
	err := installVanillaCore(ctx, srv.Dir, srv.MCVersion, onProgress)
	return "", LaunchConfig{}, err
}

func installVanillaCore(ctx context.Context, dir, mcVersion string, onProgress ServerInstallProgressFunc) error {
	onProgress(ServerInstallProgress{Stage: "core", Percent: -1, Message: "Завантаження списку версій Minecraft..."})
	manifestURL := "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"
	var manifest struct {
		Versions []struct {
			ID  string `json:"id"`
			URL string `json:"url"`
		} `json:"versions"`
	}
	if err := fetchJSONRealCtx(ctx, manifestURL, &manifest); err != nil {
		return fmt.Errorf("manifest: %w", err)
	}

	var versionURL string
	for _, v := range manifest.Versions {
		if v.ID == mcVersion {
			versionURL = v.URL
			break
		}
	}
	if versionURL == "" {
		return fmt.Errorf("версія %s не знайдена в маніфесті Mojang", mcVersion)
	}

	var versionMeta struct {
		Downloads struct {
			Server struct {
				URL string `json:"url"`
			} `json:"server"`
		} `json:"downloads"`
	}
	if err := fetchJSONRealCtx(ctx, versionURL, &versionMeta); err != nil {
		return fmt.Errorf("version.json: %w", err)
	}
	if versionMeta.Downloads.Server.URL == "" {
		return fmt.Errorf("версія %s не має server.jar (можливо, лише клієнтська)", mcVersion)
	}

	onProgress(ServerInstallProgress{Stage: "core", Percent: -1, Message: fmt.Sprintf("Завантаження server.jar (Vanilla %s)...", mcVersion)})
	if err := downloadFileWithProgressCtx(ctx, versionMeta.Downloads.Server.URL, filepath.Join(dir, "server.jar"), func(pct int) {
		onProgress(ServerInstallProgress{Stage: "core", Percent: pct, Message: fmt.Sprintf("Завантаження server.jar (Vanilla %s)... %d%%", mcVersion, pct)})
	}); err != nil {
		return fmt.Errorf("завантаження server.jar: %w", err)
	}
	onProgress(ServerInstallProgress{Stage: "core", Percent: 100, Message: "Vanilla server.jar встановлено"})
	return nil
}

// ── Paper ──────────────────────────────────────────────────────────────

type paperInstaller struct{}

func (paperInstaller) installed(srv *Server) bool {
	jar := findServerJar(srv.Dir)
	return jar != "server.jar" || fileExists(filepath.Join(srv.Dir, "server.jar"))
}

func (paperInstaller) install(ctx context.Context, srv *Server, javaPath string, onProgress ServerInstallProgressFunc) (string, LaunchConfig, error) {
	err := installPaperCore(ctx, srv.Dir, srv.MCVersion, onProgress)
	return "", LaunchConfig{}, err
}

func installPaperCore(ctx context.Context, dir, mcVersion string, onProgress ServerInstallProgressFunc) error {
	onProgress(ServerInstallProgress{Stage: "core", Percent: -1, Message: fmt.Sprintf("Пошук останнього стабільного білда Paper %s...", mcVersion)})

	type paperBuild struct {
		ID        int    `json:"id"`
		Channel   string `json:"channel"`
		Downloads map[string]struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"downloads"`
	}

	url := fmt.Sprintf("https://fill.papermc.io/v3/projects/paper/versions/%s/builds", mcVersion)
	var builds []paperBuild
	if err := fetchJSONWithUARealCtx(ctx, url, &builds); err != nil {
		return fmt.Errorf("paper API: %w", err)
	}

	var downloadURL string
	var buildID int
	for _, b := range builds {
		if b.Channel == "STABLE" {
			if dl, ok := b.Downloads["server:default"]; ok {
				downloadURL = dl.URL
				buildID = b.ID
			}
		}
	}
	if downloadURL == "" {
		return fmt.Errorf("немає стабільних білдів Paper для MC %s", mcVersion)
	}

	onProgress(ServerInstallProgress{Stage: "core", Percent: -1, Message: fmt.Sprintf("Завантаження Paper build %d...", buildID)})
	if err := downloadFileWithProgressCtx(ctx, downloadURL, filepath.Join(dir, "server.jar"), func(pct int) {
		onProgress(ServerInstallProgress{Stage: "core", Percent: pct, Message: fmt.Sprintf("Завантаження Paper build %d... %d%%", buildID, pct)})
	}); err != nil {
		return fmt.Errorf("завантаження Paper: %w", err)
	}
	onProgress(ServerInstallProgress{Stage: "core", Percent: 100, Message: "Paper встановлено"})
	return nil
}

// ── Purpur ─────────────────────────────────────────────────────────────

type purpurInstaller struct{}

func (purpurInstaller) installed(srv *Server) bool {
	jar := findServerJar(srv.Dir)
	return jar != "server.jar" || fileExists(filepath.Join(srv.Dir, "server.jar"))
}

func (purpurInstaller) install(ctx context.Context, srv *Server, javaPath string, onProgress ServerInstallProgressFunc) (string, LaunchConfig, error) {
	textProgress := func(msg string) {
		onProgress(ServerInstallProgress{Stage: "core", Percent: -1, Message: msg})
	}
	err := installPurpur(installLogAdapter{fn: textProgress}, srv.Dir, srv.MCVersion)
	return "", LaunchConfig{}, err
}

// ── Fabric ─────────────────────────────────────────────────────────────

type fabricInstaller struct{}

func (fabricInstaller) installed(srv *Server) bool {
	return fileExists(filepath.Join(srv.Dir, "fabric-server-launch.jar"))
}

func (fabricInstaller) install(ctx context.Context, srv *Server, javaPath string, onProgress ServerInstallProgressFunc) (string, LaunchConfig, error) {
	ver, err := installFabricCore(ctx, srv.Dir, srv.MCVersion, srv.LoaderVer, javaPath, onProgress)
	if err != nil {
		return "", LaunchConfig{}, err
	}
	return ver, LaunchConfig{JarPath: "fabric-server-launch.jar"}, nil
}

func installFabricCore(ctx context.Context, dir, mcVersion, loaderVer string, javaPath string, onProgress ServerInstallProgressFunc) (string, error) {
	if javaPath == "" {
		return "", fmt.Errorf("Java не знайдена для запуску fabric-installer")
	}

	onProgress(ServerInstallProgress{Stage: "core", Percent: -1, Message: "Завантаження Fabric installer..."})
	var installerMeta []struct {
		Version string `json:"version"`
		URL     string `json:"url"`
	}
	if err := fetchJSONWithUARealCtx(ctx, "https://meta.fabricmc.net/v2/versions/installer", &installerMeta); err != nil {
		return "", fmt.Errorf("fabric installer meta: %w", err)
	}
	if len(installerMeta) == 0 {
		return "", fmt.Errorf("немає доступних версій fabric-installer")
	}

	installerPath := filepath.Join(dir, "fabric-installer.jar")
	if err := downloadFileWithProgressCtx(ctx, installerMeta[0].URL, installerPath, func(pct int) {
		onProgress(ServerInstallProgress{Stage: "core", Percent: pct, Message: fmt.Sprintf("Завантаження Fabric installer... %d%%", pct)})
	}); err != nil {
		return "", fmt.Errorf("завантаження fabric-installer: %w", err)
	}

	if loaderVer == "" {
		var loaderEntries []struct {
			Loader struct {
				Version string `json:"version"`
			} `json:"loader"`
		}
		loaderURL := fmt.Sprintf("https://meta.fabricmc.net/v2/versions/loader/%s", mcVersion)
		if err := fetchJSONWithUARealCtx(ctx, loaderURL, &loaderEntries); err != nil {
			return "", fmt.Errorf("fabric loader meta: %w", err)
		}
		if len(loaderEntries) == 0 {
			return "", fmt.Errorf("немає fabric loader для MC %s", mcVersion)
		}
		loaderVer = loaderEntries[0].Loader.Version
	}

	onProgress(ServerInstallProgress{Stage: "core", Percent: -1, Message: fmt.Sprintf("Встановлення Fabric %s (loader %s)...", mcVersion, loaderVer)})
	cmd := exec.CommandContext(ctx, javaPath, "-jar", installerPath, "server",
		"-dir", dir, "-mcversion", mcVersion, "-loader", loaderVer, "-downloadMinecraft")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("fabric-installer: %v\n%s", err, out)
	}

	launchJar := filepath.Join(dir, "fabric-server-launch.jar")
	if !fileExists(launchJar) {
		return "", fmt.Errorf("fabric-server-launch.jar не створено після встановлення")
	}
	onProgress(ServerInstallProgress{Stage: "core", Percent: 100, Message: "Fabric встановлено"})
	return loaderVer, nil
}

// ── Quilt (той самий installer-протокол, що і Fabric) ─────────────────

type quiltInstaller struct{}

func (quiltInstaller) installed(srv *Server) bool {
	return fileExists(filepath.Join(srv.Dir, "quilt-server-launch.jar"))
}

func (quiltInstaller) install(ctx context.Context, srv *Server, javaPath string, onProgress ServerInstallProgressFunc) (string, LaunchConfig, error) {
	ver, err := installQuiltCore(ctx, srv.Dir, srv.MCVersion, srv.LoaderVer, javaPath, onProgress)
	if err != nil {
		return "", LaunchConfig{}, err
	}
	return ver, LaunchConfig{JarPath: "quilt-server-launch.jar"}, nil
}

func installQuiltCore(ctx context.Context, dir, mcVersion, loaderVer string, javaPath string, onProgress ServerInstallProgressFunc) (string, error) {
	if javaPath == "" {
		return "", fmt.Errorf("Java не знайдена для запуску quilt-installer")
	}

	onProgress(ServerInstallProgress{Stage: "core", Percent: -1, Message: "Завантаження Quilt installer..."})
	var installerMeta []struct {
		Version string `json:"version"`
	}
	if err := fetchJSONWithUARealCtx(ctx, "https://meta.quiltmc.org/v3/versions/installer", &installerMeta); err != nil {
		return "", fmt.Errorf("quilt installer meta: %w", err)
	}
	if len(installerMeta) == 0 {
		return "", fmt.Errorf("немає доступних версій quilt-installer")
	}
	installerVer := installerMeta[0].Version
	installerURL := fmt.Sprintf("https://maven.quiltmc.org/repository/release/org/quiltmc/quilt-installer/%s/quilt-installer-%s.jar", installerVer, installerVer)

	installerPath := filepath.Join(dir, "quilt-installer.jar")
	if err := downloadFileWithProgressCtx(ctx, installerURL, installerPath, func(pct int) {
		onProgress(ServerInstallProgress{Stage: "core", Percent: pct, Message: fmt.Sprintf("Завантаження Quilt installer... %d%%", pct)})
	}); err != nil {
		return "", fmt.Errorf("завантаження quilt-installer: %w", err)
	}

	if loaderVer == "" {
		// meta.quiltmc.org v3 кладе версію loader у loader.version (як
		// meta.fabricmc.net v2) — читати з верхнього рівня означало б
		// порожній loaderVer, і quilt-installer падав асинхронно
		// («loader version X was not found» → exit 0 без jar).
		var loaderEntries []struct {
			Loader struct {
				Version string `json:"version"`
			} `json:"loader"`
		}
		loaderURL := fmt.Sprintf("https://meta.quiltmc.org/v3/versions/loader/%s", mcVersion)
		if err := fetchJSONWithUARealCtx(ctx, loaderURL, &loaderEntries); err != nil {
			return "", fmt.Errorf("quilt loader meta: %w", err)
		}
		if len(loaderEntries) == 0 || loaderEntries[0].Loader.Version == "" {
			return "", fmt.Errorf("немає quilt loader для MC %s", mcVersion)
		}
		loaderVer = loaderEntries[0].Loader.Version
	}

	onProgress(ServerInstallProgress{Stage: "core", Percent: -1, Message: fmt.Sprintf("Встановлення Quilt %s (loader %s)...", mcVersion, loaderVer)})
	// Синтаксис quilt-installer (перевірено на v0.15.1):
	//   install server <mc> [<loader>] --install-dir=<dir> --download-server
	// Однотире "-dir=" не розпізнається (мовчки ігнорується — інсталятор
	// ставив сервер у <CWD>/server, а не в теку сервера), тож jar не
	// з'являвся там, де його шукав лаунчер.
	cmd := exec.CommandContext(ctx, javaPath, "-jar", installerPath, "install", "server",
		mcVersion, loaderVer, "--install-dir="+dir, "--download-server")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("quilt-installer: %v\n%s", err, out)
	}

	launchJar := filepath.Join(dir, "quilt-server-launch.jar")
	if !fileExists(launchJar) {
		return "", fmt.Errorf("quilt-server-launch.jar не створено після встановлення")
	}
	onProgress(ServerInstallProgress{Stage: "core", Percent: 100, Message: "Quilt встановлено"})
	return loaderVer, nil
}

// ── Forge ──────────────────────────────────────────────────────────────

type forgeInstaller struct{}

func (forgeInstaller) installed(srv *Server) bool {
	return forgeInstalledOnDisk(srv)
}

func forgeInstalledOnDisk(srv *Server) bool {
	if fileExists(filepath.Join(srv.Dir, "libraries", "net", "minecraftforge", "forge", srv.LoaderVer, "win_args.txt")) {
		return true
	}
	if fileExists(filepath.Join(srv.Dir, "libraries", "net", "minecraftforge", "forge", srv.LoaderVer, "unix_args.txt")) {
		return true
	}
	if srv.LoaderVer != "" && fileExists(filepath.Join(srv.Dir, fmt.Sprintf("forge-%s-universal.jar", srv.LoaderVer))) {
		return true
	}
	return fileExists(filepath.Join(srv.Dir, "server.jar"))
}

func (forgeInstaller) install(ctx context.Context, srv *Server, javaPath string, onProgress ServerInstallProgressFunc) (string, LaunchConfig, error) {
	ver, launchConfig, err := installForgeCore(ctx, srv.Dir, srv.MCVersion, srv.LoaderVer, javaPath, onProgress)
	return ver, launchConfig, err
}

// forgePromotions — promotions_slim.json: офіційне джерело "яка версія
// Forge зараз recommended/latest для конкретної MC", яке Forge підтримує
// синхронізованим із реально опублікованими на Maven файлами. Це надійніше
// за "взяти останній рядок з maven-metadata.xml" — metadata іноді містить
// версії, чиї файли ще не залито на CDN або вже прибрано, через що пряме
// завантаження installer.jar для "останньої за списком" версії може дати
// 404 (реальний кейс: forge 1.20.1-47.4.10 у метаданих, але без файлів).
func forgePromotions(ctx context.Context) (map[string]string, error) {
	var data struct {
		Promos map[string]string `json:"promos"`
	}
	// files.minecraftforge.net інколи віддає 403 без "схожого на браузер"
	// User-Agent для automated-клієнтів — той самий UA, що й для решти
	// запитів install.go, вже це враховує.
	if err := fetchJSONWithUARealCtx(ctx, "https://files.minecraftforge.net/net/minecraftforge/forge/promotions_slim.json", &data); err != nil {
		// Дзеркало на maven.minecraftforge.net як запасний варіант.
		if err2 := fetchJSONWithUARealCtx(ctx, "https://maven.minecraftforge.net/net/minecraftforge/forge/promotions_slim.json", &data); err2 != nil {
			return nil, fmt.Errorf("%w (дзеркало: %v)", err, err2)
		}
	}
	return data.Promos, nil
}

// forgeCandidateVersions повертає версії Forge для mcVersion, від
// найімовірніше-робочої до менш імовірної: спершу promotions_slim.json
// (recommended, потім latest — це версії, які Forge сам вважає готовими),
// далі решта версій з maven-metadata.xml від найновішої до найстарішої, як
// фолбек-ланцюжок для installForgeCore.
func forgeCandidateVersions(ctx context.Context, mcVersion string) ([]string, error) {
	ordered := make([]string, 0, 8)
	seen := map[string]bool{}
	add := func(v string) {
		if v != "" && !seen[v] {
			seen[v] = true
			ordered = append(ordered, v)
		}
	}

	if promos, err := forgePromotions(ctx); err == nil {
		// promotions_slim.json повертає ЛИШЕ номер Forge-збірки (напр. "47.4.10"),
		// а Maven-шлях потребує формат MC_VERSION-FORGE_VERSION
		// ("1.20.1-47.4.10"). Додаємо префікс MC-версії, якщо його ще немає.
		for _, key := range []string{mcVersion + "-recommended", mcVersion + "-latest"} {
			if v := promos[key]; v != "" {
				if !strings.Contains(v, "-") {
					v = mcVersion + "-" + v
				}
				add(v)
			}
		}
	}

	metadataURL := "https://maven.minecraftforge.net/net/minecraftforge/forge/maven-metadata.xml"
	var xmlData struct {
		Versioning struct {
			Versions struct {
				Version []string `xml:"version"`
			} `xml:"versions"`
		} `xml:"versioning"`
	}
	if err := fetchXMLRealCtx(ctx, metadataURL, &xmlData); err != nil {
		if len(ordered) == 0 {
			return nil, fmt.Errorf("forge metadata: %w", err)
		}
		return ordered, nil // promotions спрацювали — metadata як бонус не критична
	}
	prefix := mcVersion + "-"
	// maven-metadata.xml перелічує версії від старих до нових — йдемо в
	// зворотному порядку, щоб фолбек пробував спершу найновіші.
	for i := len(xmlData.Versioning.Versions.Version) - 1; i >= 0; i-- {
		v := xmlData.Versioning.Versions.Version[i]
		if strings.HasPrefix(v, prefix) && !strings.Contains(v, "-beta") && !strings.Contains(v, "-rc") {
			add(v)
		}
	}
	if len(ordered) == 0 {
		return nil, fmt.Errorf("Forge не знайдено для MC %s", mcVersion)
	}
	return ordered, nil
}

// installerRunResult — результат однієї спроби встановлення інсталятором
// (Forge/NeoForge) для однієї версії-кандидата. Використовується
// tryInstallerCandidateChain (розділ 1.5) — спільний хелпер, що пробує
// список версій по черзі й повертає першу вдалу.
type installerRunResult struct {
	loaderVer    string
	launchConfig LaunchConfig
}

// tryInstallerCandidateChain пробує candidates по черзі (той самий
// ланцюжок, що forgeCandidateVersions/neoForgeCandidateVersions вже
// будували правильно за логікою — recommended → latest → maven metadata
// зверху вниз), зупиняючись на першій вдалій спробі. attempt виконує
// одну спробу для однієї версії; його друге повернене значення (bool) —
// чи варто пробувати наступного кандидата при помилці (завжди true тут,
// параметр лишений для симетрії з можливим майбутнім "фатальна помилка,
// не пробувати далі").
//
// Перенесено з двох майже ідентичних циклів (installForgeCore/
// installNeoForgeCore до рефакторингу) — розділ 1.5 запиту прямо просить
// звести їх до одного хелпера замість дублювання.
func tryInstallerCandidateChain(
	candidates []string,
	onProgress ServerInstallProgressFunc,
	label string,
	attempt func(ver string) (installerRunResult, error),
) (installerRunResult, error) {
	var lastErr error
	for i, ver := range candidates {
		if i == 0 {
			onProgress(ServerInstallProgress{Stage: "core", Percent: -1, Message: fmt.Sprintf("Завантаження %s %s...", label, ver)})
		} else {
			onProgress(ServerInstallProgress{Stage: "core", Percent: -1, Message: fmt.Sprintf("%s %s недоступний, пробую %s...", label, candidates[i-1], ver)})
		}
		result, err := attempt(ver)
		if err != nil {
			lastErr = err
			continue
		}
		return result, nil
	}
	return installerRunResult{}, lastErr
}

func installForgeCore(ctx context.Context, dir, mcVersion, loaderVer string, javaPath string, onProgress ServerInstallProgressFunc) (string, LaunchConfig, error) {
	if javaPath == "" {
		return "", LaunchConfig{}, fmt.Errorf("Java не знайдена для запуску forge-installer")
	}

	// Кандидати для спроби — якщо loaderVer заданий явно (користувач обрав
	// конкретну версію в UI), пробуємо лише її, без фолбеку на інші версії
	// (підміняти вибір користувача мовчки не можна). Якщо не заданий —
	// пробуємо ланцюжок recommended → latest → решта, поки щось не спрацює.
	var candidates []string
	if loaderVer != "" {
		// Якщо користувач вказав «голу» версію Forge ("47.4.10" замість
		// "1.20.1-47.4.10"), додаємо MC-префікс для коректного Maven-шляху.
		if !strings.Contains(loaderVer, "-") {
			loaderVer = mcVersion + "-" + loaderVer
		}
		candidates = []string{loaderVer}
	} else {
		onProgress(ServerInstallProgress{Stage: "core", Percent: -1, Message: fmt.Sprintf("Пошук версії Forge для %s...", mcVersion)})
		var err error
		candidates, err = forgeCandidateVersions(ctx, mcVersion)
		if err != nil {
			return "", LaunchConfig{}, err
		}
	}

	result, lastErr := tryInstallerCandidateChain(candidates, onProgress, "Forge", func(ver string) (installerRunResult, error) {
		installerURL := fmt.Sprintf("https://maven.minecraftforge.net/net/minecraftforge/forge/%s/forge-%s-installer.jar", ver, ver)
		installerPath := filepath.Join(dir, "forge-installer.jar")
		if err := downloadFileWithProgressCtx(ctx, installerURL, installerPath, func(pct int) {
			onProgress(ServerInstallProgress{Stage: "core", Percent: pct, Message: fmt.Sprintf("Завантаження Forge %s... %d%%", ver, pct)})
		}); err != nil {
			return installerRunResult{}, fmt.Errorf("завантаження forge-installer %s: %w", ver, err)
		}

		onProgress(ServerInstallProgress{Stage: "core", Percent: -1, Message: "Встановлення Forge (--installServer)..."})
		cmd := exec.CommandContext(ctx, javaPath, "-jar", installerPath, "--installServer")
		cmd.Dir = dir
		out, runErr := cmd.CombinedOutput()
		if runErr != nil {
			return installerRunResult{}, fmt.Errorf("forge-installer %s: %v\n%s", ver, runErr, out)
		}

		// Розділ 1.1/1.2: одразу після успішного встановлення визначаємо
		// launch-конфіг тим самим порядком перевірок, що
		// existingInstallLaunchConfig використовує для самозцілення —
		// launcher-jar (шим/universal) на корені має пріоритет, бо він
		// самодостатній і не потребує args-файлу.
		if jar, ok := findForgeLauncherJar(dir); ok {
			return installerRunResult{loaderVer: ver, launchConfig: LaunchConfig{JarPath: jar}}, nil
		}
		if argsFile, ok := findLoaderArgsFile(dir, filepath.Join("net", "minecraftforge", "forge"), ver); ok {
			return installerRunResult{loaderVer: ver, launchConfig: LaunchConfig{ArgsFile: argsFile}}, nil
		}
		// Інсталятор відпрацював без помилки, але не лишив ані launcher-jar,
		// ані args-файлу — вигадувати vanilla-фолбек тут не можна: сервер
		// без Forge буде недоступний для форжевих клієнтів. Явна помилка.
		return installerRunResult{}, fmt.Errorf("forge-installer %s завершився без помилки, але не створив ані launcher-jar, ані args-файл", ver)
	})
	if result.loaderVer == "" {
		if loaderVer != "" {
			return "", LaunchConfig{}, fmt.Errorf("версія Forge %s недоступна для завантаження (файли відсутні на сервері Forge) — оберіть іншу версію: %w", loaderVer, lastErr)
		}
		return "", LaunchConfig{}, fmt.Errorf("не вдалося встановити жодну версію Forge для MC %s: %w", mcVersion, lastErr)
	}
	onProgress(ServerInstallProgress{Stage: "core", Percent: 100, Message: "Forge встановлено"})
	return result.loaderVer, result.launchConfig, nil
}

// ── NeoForge ───────────────────────────────────────────────────────────

type neoForgeInstaller struct{}

func (neoForgeInstaller) installed(srv *Server) bool {
	return neoForgeInstalledOnDisk(srv)
}

func neoForgeInstalledOnDisk(srv *Server) bool {
	if fileExists(filepath.Join(srv.Dir, "libraries", "net", "neoforged", "neoforge", srv.LoaderVer, "win_args.txt")) {
		return true
	}
	if fileExists(filepath.Join(srv.Dir, "libraries", "net", "neoforged", "neoforge", srv.LoaderVer, "unix_args.txt")) {
		return true
	}
	return fileExists(filepath.Join(srv.Dir, "server.jar"))
}

func (neoForgeInstaller) install(ctx context.Context, srv *Server, javaPath string, onProgress ServerInstallProgressFunc) (string, LaunchConfig, error) {
	return installNeoForgeCore(ctx, srv.Dir, srv.MCVersion, srv.LoaderVer, javaPath, onProgress)
}

// neoForgeCandidateVersions повертає версії NeoForge для mcVersion від
// найновішої до найстарішої (той самий принцип фолбеку, що forgeCandidate-
// Versions — деякі версії у maven-metadata.xml можуть не мати опублікованих
// файлів на CDN).
func neoForgeCandidateVersions(ctx context.Context, mcVersion string) ([]string, error) {
	metadataURL := "https://maven.neoforged.net/releases/net/neoforged/neoforge/maven-metadata.xml"
	var xmlData struct {
		Versioning struct {
			Versions struct {
				Version []string `xml:"version"`
			} `xml:"versions"`
		} `xml:"versioning"`
	}
	if err := fetchXMLRealCtx(ctx, metadataURL, &xmlData); err != nil {
		return nil, fmt.Errorf("neoforge metadata: %w", err)
	}
	// NeoForge версії мають формат <mc-minor>.<mc-patch>.<build>, без
	// префіксу повної MC-версії (напр. "21.1.100" під MC 1.21.1).
	mcParts := strings.SplitN(strings.TrimPrefix(mcVersion, "1."), ".", 2)
	prefix := mcParts[0] + "."
	if len(mcParts) > 1 {
		prefix = mcParts[0] + "." + mcParts[1] + "."
	}
	out := make([]string, 0, 8)
	// Список іде від старих до нових — розвертаємо, щоб пробувати спершу
	// найновіші (найімовірніше стабільно опубліковані).
	for i := len(xmlData.Versioning.Versions.Version) - 1; i >= 0; i-- {
		v := xmlData.Versioning.Versions.Version[i]
		if strings.HasPrefix(v, prefix) && !strings.Contains(v, "-beta") {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("NeoForge не знайдено для MC %s", mcVersion)
	}
	return out, nil
}

func installNeoForgeCore(ctx context.Context, dir, mcVersion, loaderVer string, javaPath string, onProgress ServerInstallProgressFunc) (string, LaunchConfig, error) {
	if javaPath == "" {
		return "", LaunchConfig{}, fmt.Errorf("Java не знайдена для запуску neoforge-installer")
	}

	var candidates []string
	if loaderVer != "" {
		candidates = []string{loaderVer}
	} else {
		onProgress(ServerInstallProgress{Stage: "core", Percent: -1, Message: "Пошук версії NeoForge..."})
		var err error
		candidates, err = neoForgeCandidateVersions(ctx, mcVersion)
		if err != nil {
			return "", LaunchConfig{}, err
		}
	}

	result, lastErr := tryInstallerCandidateChain(candidates, onProgress, "NeoForge", func(ver string) (installerRunResult, error) {
		installerURL := fmt.Sprintf("https://maven.neoforged.net/releases/net/neoforged/neoforge/%s/neoforge-%s-installer.jar", ver, ver)
		installerPath := filepath.Join(dir, "neoforge-installer.jar")
		if err := downloadFileWithProgressCtx(ctx, installerURL, installerPath, func(pct int) {
			onProgress(ServerInstallProgress{Stage: "core", Percent: pct, Message: fmt.Sprintf("Завантаження NeoForge %s... %d%%", ver, pct)})
		}); err != nil {
			return installerRunResult{}, fmt.Errorf("завантаження neoforge-installer %s: %w", ver, err)
		}

		onProgress(ServerInstallProgress{Stage: "core", Percent: -1, Message: "Встановлення NeoForge (--installServer)..."})
		cmd := exec.CommandContext(ctx, javaPath, "-jar", installerPath, "--installServer")
		cmd.Dir = dir
		out, runErr := cmd.CombinedOutput()
		if runErr != nil {
			return installerRunResult{}, fmt.Errorf("neoforge-installer %s: %v\n%s", ver, runErr, out)
		}

		// NeoForge завжди запускається через args-файл під libraries/ —
		// немає self-contained launcher-jar, як у Forge (розділ 1.2).
		if argsFile, ok := findLoaderArgsFile(dir, filepath.Join("net", "neoforged", "neoforge"), ver); ok {
			return installerRunResult{loaderVer: ver, launchConfig: LaunchConfig{ArgsFile: argsFile}}, nil
		}
		return installerRunResult{}, fmt.Errorf("neoforge-installer %s завершився без помилки, але не створив args-файл", ver)
	})
	if result.loaderVer == "" {
		if loaderVer != "" {
			return "", LaunchConfig{}, fmt.Errorf("версія NeoForge %s недоступна для завантаження — оберіть іншу версію: %w", loaderVer, lastErr)
		}
		return "", LaunchConfig{}, fmt.Errorf("не вдалося встановити жодну версію NeoForge для MC %s: %w", mcVersion, lastErr)
	}
	onProgress(ServerInstallProgress{Stage: "core", Percent: 100, Message: "NeoForge встановлено"})
	return result.loaderVer, result.launchConfig, nil
}

// ── Velocity ───────────────────────────────────────────────────────────

type velocityInstaller struct{}

func (velocityInstaller) installed(srv *Server) bool {
	return fileExists(filepath.Join(srv.Dir, "velocity.jar"))
}

func (velocityInstaller) install(ctx context.Context, srv *Server, javaPath string, onProgress ServerInstallProgressFunc) (string, LaunchConfig, error) {
	err := installVelocityCore(ctx, srv.Dir, srv.LoaderVer, onProgress)
	return "", LaunchConfig{}, err
}

// installVelocityCore качає velocity.jar з fill.papermc.io v3.
// Раніше використовувався api.papermc.io/v2, який PaperMC закрив
// (відповідає 410 Gone) — звідси і був баг «Velocity не встановлюється».
func installVelocityCore(ctx context.Context, dir, loaderVer string, onProgress ServerInstallProgressFunc) error {
	// 1. Версія проксі: явна (з UI) або рекомендована стабільна.
	ver := loaderVer
	if ver == "" {
		onProgress(ServerInstallProgress{Stage: "core", Percent: -1, Message: "Пошук останньої версії Velocity..."})
		cands, err := velocityCandidates()
		if err != nil {
			return fmt.Errorf("velocity versions: %w", err)
		}
		rec, ok := velocityRecommended(cands)
		if !ok {
			return fmt.Errorf("немає доступних версій Velocity")
		}
		ver = rec.Version
	}

	// 2. Білди версії (v3: масив білдів з downloads.server:default прямо в
	// тілі відповіді). Обираємо останній STABLE (або просто найновіший).
	onProgress(ServerInstallProgress{Stage: "core", Percent: -1, Message: fmt.Sprintf("Пошук білдів Velocity %s...", ver)})
	type fillBuild struct {
		ID        int    `json:"id"`
		Channel   string `json:"channel"`
		Downloads map[string]struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"downloads"`
	}
	var builds []fillBuild
	buildsURL := fmt.Sprintf("https://fill.papermc.io/v3/projects/velocity/versions/%s/builds", ver)
	if err := fetchJSONWithUARealCtx(ctx, buildsURL, &builds); err != nil {
		return fmt.Errorf("velocity builds: %w", err)
	}
	if len(builds) == 0 {
		return fmt.Errorf("немає білдів Velocity %s", ver)
	}

	var chosen *fillBuild
	for i := range builds {
		b := builds[i]
		if b.Channel == "STABLE" && (chosen == nil || b.ID > chosen.ID) {
			chosen = &builds[i]
		}
	}
	if chosen == nil {
		chosen = &builds[0]
		for i := range builds {
			if builds[i].ID > chosen.ID {
				chosen = &builds[i]
			}
		}
	}
	dl, ok := chosen.Downloads["server:default"]
	if !ok || dl.URL == "" {
		return fmt.Errorf("Velocity %s (build %d): немає server:default download", ver, chosen.ID)
	}

	// 3. Качаємо як velocity.jar.
	onProgress(ServerInstallProgress{Stage: "core", Percent: -1, Message: fmt.Sprintf("Завантаження Velocity %s (build %d)...", ver, chosen.ID)})
	if err := downloadFileWithProgressCtx(ctx, dl.URL, filepath.Join(dir, "velocity.jar"), func(pct int) {
		onProgress(ServerInstallProgress{Stage: "core", Percent: pct, Message: fmt.Sprintf("Завантаження Velocity %s (build %d)... %d%%", ver, chosen.ID, pct)})
	}); err != nil {
		return fmt.Errorf("завантаження Velocity: %w", err)
	}
	onProgress(ServerInstallProgress{Stage: "core", Percent: 100, Message: "Velocity встановлено"})
	return nil
}

// ── HTTP-хелпери (продакшн-версії; у тестах є власні дублікати з іменами
// без суфіксу Real, тому конфлікту символів немає — різні файли/build tags) ──
//
// Розділ 1.4: усі приймають context.Context і передають його в
// http.NewRequestWithContext/exec.CommandContext (де застосовно), щоб
// скасування InstallCore (у майбутньому — коли якийсь виклик передасть
// не context.Background(), а реальний скасовуваний контекст) переривало
// мережевий запит чи дочірній процес, а не чекало його природного
// завершення.

func downloadFileReal(url, path string) error {
	return downloadFileWithProgress(url, path, nil)
}

// downloadFileWithProgress завантажує файл з прогресом у відсотках.
// Якщо onPct == nil або сервер не віддає Content-Length — працює як
// звичайний downloadFileReal (без %). Throttling: onPct викликається
// не частіше ніж раз на 100мс (як лічильник файлів у minecraft.Installer).
//
// Сигнатура БЕЗ context.Context — навмисно: purpur.go (поза списком
// файлів, які можна змінювати) викликає цю функцію в наявній 3-аргументній
// формі, і чіпати purpur.go заборонено умовами завдання. Розділ 1.4
// (скасування) реалізовано через downloadFileWithProgressCtx нижче —
// новий контекст-обізнаний варіант, яким користується весь новий код
// цього файлу; ця функція лишається тонкою обгорткою над ним з
// context.Background() для сумісності зі старим викликачем.
func downloadFileWithProgress(url, path string, onPct func(pct int)) error {
	return downloadFileWithProgressCtx(context.Background(), url, path, onPct)
}

// downloadFileWithProgressCtx — те саме, що downloadFileWithProgress, але
// приймає context.Context (розділ 1.4): скасування контексту перериває
// мережевий запит через http.NewRequestWithContext, а не чекає його
// природного завершення.
func downloadFileWithProgressCtx(ctx context.Context, url, path string, onPct func(pct int)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, url)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if onPct == nil || resp.ContentLength <= 0 {
		_, err = io.Copy(f, resp.Body)
		return err
	}
	// Звичайний варіант з %.
	total := resp.ContentLength
	var written int64
	var lastPct int
	var lastTime time.Time
	buf := make([]byte, 64*1024) // 64KB буфер
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			n2, writeErr := f.Write(buf[:n])
			written += int64(n2)
			if writeErr != nil {
				return writeErr
			}
			// Throttled % callback.
			if onPct != nil {
				pct := int(math.Min(100, float64(written)*100/float64(total)))
				now := time.Now()
				if pct != lastPct && (now.Sub(lastTime) >= 100*time.Millisecond || pct == 100) {
					onPct(pct)
					lastPct = pct
					lastTime = now
				}
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if onPct != nil {
		onPct(100)
	}
	return nil
}

// fetchJSONReal/fetchXMLReal/fetchJSONWithUAReal лишаються БЕЗ
// context.Context у публічній (пакетній) сигнатурі — versions.go (поза
// списком файлів, які можна змінювати) викликає fetchJSONWithUAReal у
// наявній 2-аргументній формі. Кожна — тонка обгортка над *Ctx-варіантом
// із context.Background(); увесь новий код цього файлу викликає *Ctx
// напряму й реально поширює скасування (розділ 1.4).

func fetchJSONReal(url string, v interface{}) error {
	return fetchJSONRealCtx(context.Background(), url, v)
}

func fetchJSONRealCtx(ctx context.Context, url string, v interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, url)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

func fetchXMLReal(url string, v interface{}) error {
	return fetchXMLRealCtx(context.Background(), url, v)
}

func fetchXMLRealCtx(ctx context.Context, url string, v interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, url)
	}
	return xml.NewDecoder(resp.Body).Decode(v)
}

func fetchJSONWithUAReal(url string, v interface{}) error {
	return fetchJSONWithUARealCtx(context.Background(), url, v)
}

func fetchJSONWithUARealCtx(ctx context.Context, url string, v interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "shaurma-launcher/1.0.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, url)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}
