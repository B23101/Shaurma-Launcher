package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"shaurma-launcher-wails/internal/ai"
	"shaurma-launcher-wails/internal/api"
	"shaurma-launcher-wails/internal/auth"
	"shaurma-launcher-wails/internal/avatar"
	"shaurma-launcher-wails/internal/builds"
	"shaurma-launcher-wails/internal/config"
	"shaurma-launcher-wails/internal/console"
	"shaurma-launcher-wails/internal/custompack"
	"shaurma-launcher-wails/internal/download"
	"shaurma-launcher-wails/internal/java"
	"shaurma-launcher-wails/internal/minecraft"
	"shaurma-launcher-wails/internal/model"
	"shaurma-launcher-wails/internal/mods"
	"shaurma-launcher-wails/internal/sessions"
	syncengine "shaurma-launcher-wails/internal/sync"
	"shaurma-launcher-wails/internal/sysinfo"
	"shaurma-launcher-wails/internal/update"
	"shaurma-launcher-wails/internal/wardrobe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type PackProvider interface {
	GetPackIndex() ([]api.PackIndexEntry, error)
	DownloadPack(packID string, a *App)
	// CancelDownload — ПОВНЕ скасування: часткові файли видаляються,
	// кнопка в UI повертається на "Встановити" (див. syncengine.Runner.Cancel).
	CancelDownload(packID string)
	// PauseDownload — м'яка зупинка: прогрес зберігається на диску,
	// кнопка в UI стає "Продовжити" (див. syncengine.Runner.Pause). Той самий
	// шлях, яким PauseAll зупиняє все при закритті лаунчера/краху.
	PauseDownload(packID string)
	GetPackByID(id string) (*api.PackIndexEntry, error)
	// AssetDataURL повертає data URL асета збірки (іконка/фон) з CDN Шаурми.
	// Прямі <img src> не працюють: Worker віддає файли лише з токеном у
	// заголовках, яких браузер додати не може (див. ShaurmaClient.AssetDataURL).
	AssetDataURL(assetURL string) (string, error)
}

type App struct {
	ctx      context.Context
	window   *application.WebviewWindow
	cfg      *config.Manager
	dl       *download.Engine
	modMgr   *mods.Manager
	msAuth   *auth.Authenticator
	pirate   *auth.PirateService
	modrinth *api.ModrinthClient
	updater  *update.Checker
	inst     *minecraft.Installer
	javaInst *java.Installer
	avatars  *avatar.Service
	wardrobe *wardrobe.Service
	packs    PackProvider

	// customPacks — реєстр локальних кастомних збірок (створених вручну
	// або імпортованих з .mrpack/.zip), окремий від Shaurma-каталогу
	// (packs). versionsProv — легкий кеш-клієнт списків версій
	// Minecraft/лоадерів для форми "Нова збірка → Вручну".
	customPacks  *custompack.Store
	versionsProv *custompack.VersionsProvider
	// customImportMu/customImportCancel — керування активним імпортом
	// модпаку (Скасувати на прев'ю-екрані вкладки "Імпортувати").
	customImportMu     sync.Mutex
	customImportCancel map[string]context.CancelFunc

	// Архітектура збірок: реєстр встановлених збірок (builds.json) та
	// менеджер незалежних ігрових сесій по акаунтах. Завантаження/стан
	// файлів спільний для всіх акаунтів, а процеси гри — окремі на
	// (акаунт × збірка).
	buildsReg *builds.Registry
	sessionsM *sessions.Manager

	// syncMgr — менеджер нової двочергової системи синхронізації збірок
	// Шаурма V2 (DOWNLOAD_SYNC_DESIGN_V2.md): тримає активні Runner-сесії
	// по packID, дозволяє одночасну качку кількох збірок, дає Pause/Cancel
	// і прапорець HasActive для попередження при закритті лаунчера.
	syncMgr *syncengine.Manager

	// consoleBufs — буфери консолі ПО ПАРАХ (акаунт × збірка): кожен акаунт
	// має власний лог для тієї самої збірки (незалежні запуски). consoleBuf
	// вказує на буфер, який КОНСОЛЬ ПОКАЗУЄ ЗАРАЗ (може відрізнятись від
	// активного акаунта після перемикання у перемикачі консолі);
	// consoleCurKey фіксує, чий це буфер.
	consoleBufs   map[consoleKey]*console.Buffer
	consoleBufMu  sync.Mutex
	consoleCurKey consoleKey
	dlTransient   map[string]*builds.Progress // packID -> активний прогрес
	dlTransientMu sync.Mutex

	// launchPacks — packID, для яких зараз іде LaunchInstance (звірка
	// файлів з worker + ensure-Java/Loader). Поки збірка тут — прогрес
	// синхронізації (sync:progress) форвардиться ще й у launch:progress
	// (стадія worker_update), щоб сегментний бар на сторінці збірки показував
	// реальний % оновлення файлів, а не статичний підпис.
	launchPacks   map[string]bool
	launchPacksMu sync.Mutex

	// launchState — ОСТАННЯ стадія запуску по кожній збірці (та сама, що
	// йде у launch:progress). Консоль читає її через GetConsoleContext,
	// щоб показувати реальний стан збірки навіть якщо відкрилась посеред
	// запуску (події до цього моменту вже пройшли повз неї).
	launchState   map[string]LaunchProgress
	launchStateMu sync.Mutex

	// consoleDiag — останній AI-діагноз крашу по збірці (і код виходу).
	// Це СПІЛЬНИЙ стан для ВСІХ копій консолі (вкладка на сторінці збірки
	// + окреме вікно): вони читають його через GetConsoleContext і
	// оновлюються через подію console:ai — тож показують ОДНАКОВУ відповідь
	// ШІ, і відкривши вікно після краху користувач бачить той самий віджет.
	consoleDiag   map[string]model.AIDiagnosis // buildID -> останній діагноз
	consoleDiagAt map[string]time.Time         // buildID -> час останнього аналізу (TTL-дедуп)
	consoleExit   map[string]int               // buildID -> останній код виходу (0 = норм, !=0 = краш)
	consoleDiagMu sync.Mutex

	// syncFailures — packID, для яких останній DownloadPack завершився ДО
	// реєстрації runner-а (health-check/маніфест: немає мережі або сервер
	// недоступний). waitForSyncStartThenFinish перевіряє цей прапорець у
	// своєму циклі й бейлить ОДРАЗУ, як тільки він з'явився — щоб запуск
	// гри не зависав на повні 20с очікування runner-а, який ніколи не
	// з'явиться (користувач: «немає бути зависань»).
	syncFailures   map[string]bool
	syncFailuresMu sync.Mutex

	// Розумна консоль: кільцевий буфер логу гри (записується ЗАВЖДИ, поки
	// гра запущена — незалежно від того, відкрите вікно консолі чи ні) та
	// клієнт Gemini для AI-аналізу крашів (ліниво створюється на перший
	// виклик).
	consoleBuf *console.Buffer
	ai         *ai.Client

	// lastInstanceID — остання збірка, яку запускали. Консоль прив'язана до
	// КОНКРЕТНОЇ збірки (див. ТЗ): навіть коли гра зупинена, консоль знає,
	// яку збірку показувати у панелі запуску.
	lastInstanceID string

	// activeInstanceID — збірка, яка ЗАРАЗ вибрана/відкрита у головному вікні
	// (сторінка деталей). Окреме вікно консолі прив'язується до НЕЇ, а не
	// до lastInstanceID (остання запущена КОЛИСЬ): інакше консоль, відкрита
	// без запущеної гри, показувала б застарілу збірку (напр. BlockFront з
	// минулого запуску) з живою кнопкою «Запустити» — клік по ній запускав
	// би НЕ ТУ збірку. Оновлюється через SelectActivePack (openPackPage у
	// фронтенді) та при кожному LaunchInstance.
	activeInstanceID string

	// stoppedByUser — гра зупинена КОРИСТУВАЧЕМ (StopGame), а не впала.
	// Кілл процесу дає ненульовий код виходу, тому без цього прапорця
	// ручна зупинка виглядала б як краш (і тригерила б авто-показ консолі
	// за showConsoleOnCrash).
	stoppedByUser atomic.Bool

	// modUpdateCache — кеш результатів останньої МЕРЕЖЕВОЇ перевірки
	// оновлень модів, окремо на кожну (packID, mcVersion, loader) — див.
	// ScanModsWithCachedUpdates/RefreshModUpdates/invalidateModUpdateCache
	// у mods_api.go. Ключ: modUpdateCacheKey(...). Мета — не бити мережу
	// (Modrinth/CurseForge) при кожному відкритті сторінки модів, а лише
	// коли користувач явно натиснув «Перевірити оновлення» чи вперше
	// відкрив сторінку для цієї пари версія/лоадер.
	modUpdateCache   map[string][]model.ModEntry
	modUpdateCacheMu sync.Mutex
}

func NewApp() *App {
	cfg := config.NewManager()
	a := &App{
		cfg:      cfg,
		dl:       download.NewEngine(),
		modMgr:   mods.NewManager(),
		msAuth:   auth.NewAuthenticator(),
		pirate:   auth.NewPirateService(),
		modrinth: api.NewModrinthClient(),
		updater:  update.NewChecker(appVersion),
		inst:     minecraft.NewInstaller(filepath.Join(cfg.Dir(), "minecraft")),
		javaInst: java.NewInstaller(cfg.JavaDir()),
		avatars:  avatar.NewService(filepath.Join(cfg.Dir(), "avatars")),
		// Реєстр встановлених збірок (builds.json) та менеджер сесій.
		buildsReg:     builds.NewRegistry(filepath.Join(cfg.Dir(), "builds.json")),
		sessionsM:     sessions.NewManager(),
		consoleBufs:   map[consoleKey]*console.Buffer{},
		dlTransient:   map[string]*builds.Progress{},
		consoleDiag:   map[string]model.AIDiagnosis{},
		consoleDiagAt: map[string]time.Time{},
		consoleExit:   map[string]int{},
		launchPacks:   map[string]bool{},
		launchState:   map[string]LaunchProgress{},
		syncFailures:  map[string]bool{},
		// Буфер консолі: дефолтний ліміт 3000 символів, реальне значення
		// (consoleMaxLines) застосовується у ServiceStartup після Load().
		consoleBuf:         console.NewBuffer(3000),
		customPacks:        custompack.NewStore(cfg.ConfigDir()),
		versionsProv:       custompack.NewVersionsProvider(),
		customImportCancel: map[string]context.CancelFunc{},
		modUpdateCache:     map[string][]model.ModEntry{},
	}
	a.packs = newPackProvider(a)
	a.wardrobe = wardrobe.NewService(filepath.Join(cfg.Dir(), "skins"), a.refreshAccountForWardrobe)
	return a
}

// ServiceStartup викликається Wails v3 на старті застосунку (App
// реалізує application.ServiceStartup). Тут — те, що в v2 було в OnStartup.
func (a *App) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	a.ctx = ctx
	a.cfg.Load()
	a.buildsReg.Load()

	// Ліміт рядків буфера консолі береться з налаштувань (consoleMaxLines),
	// щоб консоль витримувала великі сплески логів без зайвої пам'яті.
	if a.consoleBuf != nil {
		a.consoleBuf.SetMaxLines(a.cfg.GetSettings().ConsoleMaxLines)
	}

	// Відновлюємо зв'язок з грою, запущеною до перезапуску лаунчера
	// (tray-PID): якщо PID з running-game.json живий — гра досі йде, шлемо
	// game:started, щоб UI/трей не «забули» її (ТЗ: «вбити трей і
	// перезапустити лаунчер — він не має думати, що гра не запущена»).
	a.restoreRunningGame()

	// Launcher майже завжди стартує з shrm-updater.exe, який виходить
	// одразу після запуску — Windows не віддає фокус нашому вікну, і воно
	// відкривається позаду інших вікон ("блимнуло і нічого"). Піднімаємо
	// себе у фокус примусово (деталі в focus_windows.go).
	go bringWindowToFront()

	a.rewireComponents()
	// Переносимо кастомні збірки зі старого лаунчера (custom-packs.json у
	// %APPDATA%\.shaurm), якщо такі є — щоб у "Моїх збірках" одразу
	// з'явилися всі збірки користувача з минулої версії. Ідемпотентно.
	a.ImportLegacyCustomPacks()
	// Ліміти завантажень застосовуємо ПІСЛЯ rewireComponents: той
	// перестворює download-engine з дефолтними лімітами, інакше наші
	// значення з settings.json були б втрачені.
	a.applyDownloadLimits()
	// Прапорець м'якого завершення від апдейтера (аудит, п. 5): замість
	// taskkill /F апдейтер створює shrm-quit.flag, і ми самі виконуємо
	// graceful shutdown (PauseAll синхронізацій) перед виходом.
	a.startQuitFlagWatcher()
	return nil
}

// emit шле подію на фронтенд через глобальний EventManager v3.
func (a *App) emit(name string, data ...any) {
	if app := application.Get(); app != nil {
		app.Event.Emit(name, data...)
	}
}

// applyDownloadLimits переносить ліміт одночасних завантажень (вкладка
// «Завдання») у download-engine: стелю СПІЛЬНОГО пулу. Той самий Engine
// (і його пул) використовує і syncengine.Runner через download.Engine.Submit,
// тому окремо syncMgr нічого підстроювати не треба — досить оновити a.dl.
// Ретраї/HTTP-таймаут читаються напряму з налаштувань у sync.Runner та
// api.ShaurmaClient. Викликається при старті та після збереження налаштувань.
func (a *App) applyDownloadLimits() {
	s := a.cfg.GetSettings()
	a.dl.SetLimits(s.MaxConcurrentDownloads)
}

func (a *App) GetPackIndex() ([]api.PackIndexEntry, error) {
	return a.packs.GetPackIndex()
}

// PauseDownload — м'яка зупинка якання конкретної збірки: прогрес
// зберігається, фронт після цього показує кнопку "Продовжити". Прив'язана
// до кнопки "Зупинити" на сторінці/картці збірки.
func (a *App) PauseDownload(packID string) {
	a.packs.PauseDownload(packID)
}

// GetActiveSyncPackIDs повертає ID усіх збірок, що зараз синхронізуються
// (кілька одночасно) — потрібно фронту для sidebar/діалогу підтвердження
// закриття лаунчера.
func (a *App) GetActiveSyncPackIDs() []string {
	if a.syncMgr == nil {
		return nil
	}
	return a.syncMgr.ActivePackIDs()
}

// syncStateStore повертає (створюючи за потреби) сховище InstallState для
// нової V2-системи синхронізації — живе в тій самій теці даних лаунчера,
// що й download-state/builds.json, і так само перестворюється в
// rewireComponents при зміні AppDir.
func (a *App) syncStateStore() *syncengine.StateStore {
	return syncengine.NewStateStore(a.cfg.Dir())
}

func (a *App) DownloadPack(packID string) {
	a.packs.DownloadPack(packID, a)
}

// CancelDownload — ПОВНЕ скасування якання конкретної збірки: часткові
// файли видаляються, кнопка повертається на "Встановити" (див.
// syncengine.Runner.Cancel).
func (a *App) CancelDownload(packID string) {
	a.packs.CancelDownload(packID)
}

// GetPackByID повертає одну збірку Шаурма за ID — для сторінки деталей
// збірки (назва, іконка, версія гри/лоадер — лише перегляд, вони фіксовані
// маніфестом і на фронтенді не редагуються).
func (a *App) GetPackByID(id string) (*api.PackIndexEntry, error) {
	return a.packs.GetPackByID(id)
}

// GetPackAsset повертає data URL асета збірки (іконка/фон) через бекенд —
// браузер не може додати токен-заголовки до <img>/background-image, тому
// картинки Шаурма-збірок качаються тут і повертаються як data: URL.
// Для кастомних збірок (build.iconUrl/backgroundUrl = "local-file://<шлях>")
// файл читається напряму з диска — той самий data: URL контракт, просто
// без мережевого запиту.
func (a *App) GetPackAsset(assetURL string) (string, error) {
	if strings.HasPrefix(assetURL, "local-file://") {
		return localFileDataURL(strings.TrimPrefix(assetURL, "local-file://"))
	}
	if a.packs == nil {
		return "", fmt.Errorf("pack provider unavailable")
	}
	return a.packs.AssetDataURL(assetURL)
}

// localFileDataURL читає локальний файл іконки/фону кастомної збірки і
// повертає його як data: URL (той самий формат, що AssetDataURL для CDN).
func localFileDataURL(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("не вдалося прочитати локальний асет: %w", err)
	}
	ct := "image/png"
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		ct = "image/jpeg"
	case ".webp":
		ct = "image/webp"
	case ".gif":
		ct = "image/gif"
	}
	return "data:" + ct + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

// GetInstanceConfig повертає редаговані параметри збірки (RAM, JVM-аргументи,
// вікно гри, команди, сервери). Незбережені поля успадковують глобальні
// налаштування — фронтенд показує саме той стан, з яким збірка реально
// запуститься.
func (a *App) GetInstanceConfig(id string) model.InstanceConfig {
	return a.cfg.GetInstanceConfig(id)
}

// SaveInstanceConfig зберігає редаговані параметри однієї збірки.
func (a *App) SaveInstanceConfig(cfg model.InstanceConfig) error {
	return a.cfg.SaveInstanceConfig(cfg)
}

// autoJoinType повертає AutoJoinType для запуску, лише якщо автоприєднання
// увімкнене і для обраного типу задано ціль (світ/сервер). Порожній
// результат означає "автоприєднання вимкнене" — Launcher нічого не додає.
func autoJoinType(icfg model.InstanceConfig) string {
	if !icfg.AutoJoinEnabled {
		return ""
	}
	switch icfg.AutoJoinType {
	case "world":
		if strings.TrimSpace(icfg.AutoJoinWorld) != "" {
			return "world"
		}
	case "server":
		if strings.TrimSpace(icfg.AutoJoinServer) != "" {
			return "server"
		}
	}
	return ""
}

// LaunchStage — стадія процесу запуску збірки, транслюється на фронтенд
// подією "launch:progress" для сегментного прогрес-бару на сторінці збірки.
// Кольори закріплені за стадіями (узгоджено з фронтендом):
//
//	checking      — жовтий  (перевірка стану/акаунта/токена)
//	worker_update — синій   (звірка версії файлів збірки з Шаурма-worker)
//	java          — червоний (перевірка/встановлення Java)
//	loader        — зелений (встановлення лоадера Fabric/Quilt/Forge/NeoForge)
type LaunchStage string

const (
	LaunchStageChecking     LaunchStage = "checking"
	LaunchStageWorkerUpdate LaunchStage = "worker_update"
	LaunchStageJava         LaunchStage = "java"
	LaunchStageLoader       LaunchStage = "loader"
	LaunchStageDone         LaunchStage = "done"
)

// LaunchProgress — подія прогресу одного етапу запуску (не якання файлів
// збірки — це sync:progress; тут суто стадії ensure-Java/ensure-Loader/
// worker-перевірка перед стартом процесу гри).
// Percent — реальний % виконання поточного етапу (0-100), коли він відомий
// (качка клієнта/бібліотек/асетів, розпаковка Java тощо); інакше -1, і
// фронтенд показує лише підпис стадії без шкали.
type LaunchProgress struct {
	InstanceID string      `json:"instanceId"`
	Stage      LaunchStage `json:"stage"`
	Message    string      `json:"message"`
	Percent    int         `json:"percent"`
	Done       bool        `json:"done"`
	Error      string      `json:"error,omitempty"`
}

func (a *App) emitLaunchProgress(instanceID string, stage LaunchStage, msg string) {
	a.storeLaunch(instanceID, LaunchProgress{InstanceID: instanceID, Stage: stage, Message: msg, Percent: -1})
	a.emit("launch:progress", LaunchProgress{InstanceID: instanceID, Stage: stage, Message: msg, Percent: -1})
}

func (a *App) emitLaunchProgressPct(instanceID string, stage LaunchStage, msg string, pct int) {
	a.storeLaunch(instanceID, LaunchProgress{InstanceID: instanceID, Stage: stage, Message: msg, Percent: pct})
	a.emit("launch:progress", LaunchProgress{InstanceID: instanceID, Stage: stage, Message: msg, Percent: pct})
}

func (a *App) emitLaunchDone(instanceID string) {
	a.storeLaunch(instanceID, LaunchProgress{InstanceID: instanceID, Stage: LaunchStageDone, Done: true})
	a.emit("launch:progress", LaunchProgress{InstanceID: instanceID, Stage: LaunchStageDone, Done: true})
}

func (a *App) emitLaunchError(instanceID string, err error) {
	a.storeLaunch(instanceID, LaunchProgress{InstanceID: instanceID, Error: err.Error(), Done: true})
	a.emit("launch:progress", LaunchProgress{InstanceID: instanceID, Error: err.Error(), Done: true})
}

// storeLaunch зберігає останню стадію запуску збірки (для консолі:
// GetConsoleContext повертає її, навіть якщо консоль відкрилась після
// того, як подія launch:progress уже пройшла).
func (a *App) storeLaunch(instanceID string, p LaunchProgress) {
	a.launchStateMu.Lock()
	a.launchState[instanceID] = p
	a.launchStateMu.Unlock()
}

// setLaunchTracked позначає збірку як таку, що зараз проходить
// LaunchInstance: поки збірка тут, recordSyncProgress дублює її прогрес
// у launch:progress (стадія worker_update) з реальним %.
func (a *App) setLaunchTracked(packID string) {
	a.launchPacksMu.Lock()
	a.launchPacks[packID] = true
	a.launchPacksMu.Unlock()
}

func (a *App) unsetLaunchTracked(packID string) {
	a.launchPacksMu.Lock()
	delete(a.launchPacks, packID)
	a.launchPacksMu.Unlock()
}

func (a *App) isLaunchTracked(packID string) bool {
	a.launchPacksMu.Lock()
	defer a.launchPacksMu.Unlock()
	return a.launchPacks[packID]
}

// waitForSyncStartThenFinish блокується, доки DownloadPack (запущений у
// власній горутині: health-check → маніфест з мережі → реєстрація runner-а
// в syncMgr.active) реально не почне синхронізацію ЦІЄЇ збірки, а потім —
// доки вона не завершиться. syncMgr.WaitFor сам по собі тут ненадійний:
// якщо викликати його одразу після DownloadPack, runner ще може бути не
// зареєстрований (мережевий маніфест ще вантажиться), і WaitFor поверне
// управління миттєво, не дочекавшись жодного байта.
// markSyncFailure фіксує, що DownloadPack для packID завершився ДО реєстрації
// runner-а (немає мережі / сервер недоступний) — waitForSyncStartThenFinish
// підхопить це і не буде чекати мертвого runner-а до дедлайну.
func (a *App) markSyncFailure(packID string) {
	a.syncFailuresMu.Lock()
	a.syncFailures[packID] = true
	a.syncFailuresMu.Unlock()
}

// clearSyncFailure знімає прапорець невдалого старту синхронізації (коли
// runner-а все ж зареєстровано або LaunchInstance завершив своє очікування).
func (a *App) clearSyncFailure(packID string) {
	a.syncFailuresMu.Lock()
	delete(a.syncFailures, packID)
	a.syncFailuresMu.Unlock()
}

// isSyncFailed — чи DownloadPack останнього разу впав ДО старту runner-а.
func (a *App) isSyncFailed(packID string) bool {
	a.syncFailuresMu.Lock()
	defer a.syncFailuresMu.Unlock()
	return a.syncFailures[packID]
}

func (a *App) waitForSyncStartThenFinish(packID string) {
	if a.syncMgr == nil {
		return
	}
	// Чекаємо появи runner-а в active (до ~20с на завантаження маніфесту —
	// health-check + GetPackManifest усередині DownloadPack). Якщо мережі
	// немає, DownloadPack сам пошле download:error/sync:no-internet і ніколи
	// не зареєструє runner — тоді просто виходимо, не блокуючи запуск гри
	// назавжди (нехай гра стартує з тим, що вже є локально).
	deadline := time.Now().Add(20 * time.Second)
	for !a.syncMgr.IsActive(packID) {
		// Провал ДО старту (немає мережі тощо) — виходимо одразу, не чекаючи
		// 20с на runner, який не з'явиться. Користувач бачить «Немає
		// інтернету» від DownloadPack і гру, що стартує з локальними файлами.
		if a.isSyncFailed(packID) {
			return
		}
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Runner є — чекаємо його завершення (успіх/пауза/скасування/помилка),
	// а потім чистимо прапорець невдачі (якщо був залишений від минулого).
	a.clearSyncFailure(packID)
	a.syncMgr.WaitFor(packID)
}

// SelectActivePack позначає збірку, відкриту зараз у головному вікні
// (сторінка деталей), як АКТИВНУ для консолі. Окреме вікно консолі
// (GetConsoleContext з порожнім instanceID) прив'язується до неї, а не до
// lastInstanceID: після відкриття сторінки збірки консоль показує САМЕ її
// стан і кнопку дії, а не застарілу збірку з минулого запуску (баг
// «запустилась інша збірка»: консоль була прив'язана до BlockFront зі
// старої сесії і її кнопка «Запустити» запускала саме його).
// Подія console:focus змушує відкрите вікно консолі перечитати знімок і
// контекст одразу.
func (a *App) SelectActivePack(packID string) {
	a.activeInstanceID = packID
	a.emit("console:focus", packID)
}

func (a *App) LaunchInstance(instanceID string) error {
	a.emitLaunchProgress(instanceID, LaunchStageChecking, "Перевірка збірки...")
	build, err := a.resolveLaunchBuild(instanceID)
	if err != nil {
		a.emitLaunchError(instanceID, err)
		return err
	}
	accounts := a.cfg.GetAccounts()
	if len(accounts) == 0 {
		err := fmt.Errorf("no account")
		a.emitLaunchError(instanceID, err)
		return err
	}
	account := a.cfg.ActiveAccount()
	// Прив'язка збірки до конкретного акаунта ("вхід лише через такий
	// акаунт") — override сильніший за активний акаунт лаунчера. Якщо
	// прив'язаний акаунт видалено — тихо повертаємось до активного, щоб
	// збірка не переставала запускатись через видалений акаунт.
	if preCfg := a.cfg.GetInstanceConfig(instanceID); preCfg.AccountIDOverride != "" {
		for _, acc := range accounts {
			if acc.ID == preCfg.AccountIDOverride {
				account = acc
				break
			}
		}
	}
	if account.ID == "" {
		err := fmt.Errorf("no account")
		a.emitLaunchError(instanceID, err)
		return err
	}
	// На цьому акаунті збірка вже запущена — дублікат не створюємо.
	// (Та сама збірка на ІНШОМУ акаунті — окрема сесія, вона дозволена.)
	if a.sessionsM.IsRunning(account.ID, instanceID) {
		err := fmt.Errorf("build already running")
		a.emitLaunchError(instanceID, err)
		return err
	}

	// Microsoft-акаунт: якщо токен прострочений (або без нього) —
	// оновлюємо через refresh token і зберігаємо назад. Токен у
	// accounts.json тримаємо для цього (як і старий лаунчер).
	if account.Type == "microsoft" {
		needRefresh := account.AccessToken == "" ||
			(account.ExpiresAt > 0 && time.Now().UnixMilli() > account.ExpiresAt-5*60*1000)
		if needRefresh && account.RefreshToken != "" {
			refreshed, err := a.msAuth.RefreshTokens(account.RefreshToken)
			if err != nil {
				err = fmt.Errorf("token refresh: %w", err)
				a.emitLaunchError(instanceID, err)
				return err
			}
			account = *refreshed
			_ = a.cfg.UpdateAccount(account)
		}
	}

	// Шаурма-збірки: перед запуском звіряємо локальну встановлену версію
	// файлів із версією на worker (той самий remoteVersion, що живить
	// статус needs-update у списку збірок). Якщо є зміни — якаємо їх
	// зараз, а не сподіваємось, що юзер натисне "Оновити" сам: інакше гра
	// стартує зі старими/пошкодженими файлами збірки і може не запуститись
	// або впасти "взагалі нічого".
	if build.IsShaurma {
		a.emitLaunchProgress(instanceID, LaunchStageWorkerUpdate, "Перевірка оновлень збірки...")
		if entry, err := a.packs.GetPackByID(instanceID); err == nil && entry != nil {
			installedEntry, installed := a.buildsReg.Get(instanceID)
			remoteVersion := firstNonEmpty(entry.Version, entry.UpdatedAt)
			if !installed || (remoteVersion != "" && installedEntry.InstalledVersion != remoteVersion) {
				a.emitLaunchProgress(instanceID, LaunchStageWorkerUpdate, "Оновлення файлів збірки...")
				// Позначаємо збірку як таку, що запускається: sync:progress цієї
				// збірки дублюватиметься у launch:progress з реальним %.
				a.setLaunchTracked(instanceID)
				a.packs.DownloadPack(instanceID, a)
				a.waitForSyncStartThenFinish(instanceID)
				a.unsetLaunchTracked(instanceID)
				// Реєстр міг змінитись — перечитуємо build (на випадок, якщо
				// worker відкоригував версію лоадера/гри у маніфесті).
				if nb, err := a.resolveLaunchBuild(instanceID); err == nil {
					build = nb
				}
			}
		}
	}

	settings := a.cfg.GetSettings()
	// Тека гри збірки — InstanceDir/<id>/.minecraft (як у старого лаунчера:
	// overrides розпаковуються в installations/<id>/.minecraft/, звідти ж
	// запускається гра). Versions/libraries/assets живуть окремо в спільній
	// теці <dataDir>/minecraft (див. LaunchConfig.MinecraftDir).
	gameDir := filepath.Join(settings.InstanceDir, build.ID, ".minecraft")
	// Override-параметри збірки (сторінка редагування): порожні/нульові
	// поля означають "успадкувати з глобальних Settings", тому кожне
	// значення нижче обране як icfg, якщо воно задане, інакше — глобальне.
	icfg := a.cfg.GetInstanceConfig(build.ID)

	// Рекомендована Java для версії гри: MC <1.17 → 8, 1.17–1.20.4 → 17,
	// 1.20.5+/1.21+ → 21, нові snapshot → 25.
	recommended := java.RecommendedMajor(build.MCVersion)

	// Якщо для збірки увімкнена окрема Java — використовуємо її шлях.
	// Кастомна збірка тримає свій шлях у самому Pack (UseSeparateJava),
	// Shaurma-збірка — у InstanceConfig (JavaPathOverride); інакше поведінка
	// як раніше: глобальний settings.JavaPath.
	rawJavaPath := settings.JavaPath
	if build.UseSeparateJava && build.JavaPath != "" {
		rawJavaPath = build.JavaPath
	} else if icfg.UseSeparateJava && icfg.JavaPathOverride != "" {
		rawJavaPath = icfg.JavaPathOverride
	}

	// Якщо javaPath вказує на вбудовану Java лаунчера (ПАПКА m.JavaDir()) —
	// переконуємось, що вона встановлена у ПОТРІБНОМУ мажорі для версії гри
	// (лаунчер САМ ставить її, коли відсутня). Інші шляхи (вказані вручну)
	// лишаємо як є і лише знаходимо всередині них javaw.exe, а потім
	// перевіряємо, чи версія підходить для збірки.
	javaPath := rawJavaPath
	javaWarn := ""
	a.emitLaunchProgress(instanceID, LaunchStageJava, "Перевірка Java...")
	// Порожній javaPath (налаштування не збережені / дефолт) = вбудована Java
	// лаунчера: EnsureMajorWithComponent сам ставить ПОТРІБНИЙ мажор (Java 21 для
	// neoforge 1.21.1, Java 17 для forge 1.20.1 тощо) використовуючи component
	// з version.json (якщо є) для повної автономності. Раніше порожній шлях
	// минав цю гілку і запускав на Java, що лишилась у теці з минулого
	// разу (напр. Java 17 замість 21) — звідси "Unsupported major.minor
	// version 65.0" при читанні bootstraplauncher-2.0.2.jar.
	if javaPath == "" || javaPath == a.cfg.JavaDir() {
		// Реальний прогрес встановлення Java (java.Progress з відсотками)
		// форвардиться у launch:progress — бар на сторінці збірки показує
		// "Завантаження Java 21... 45%" замість статичного підпису.
		prevJava := a.javaInst.ProgressHandler()
		a.javaInst.SetProgressHandler(func(p java.Progress) {
			a.emit("java:progress", p)
			a.emitLaunchProgressPct(instanceID, LaunchStageJava, p.Message, p.Percent)
		})
		// Спочатку спробуємо отримати component з version.json (якщо вже завантажений)
		// Це дозволить встановити правильну Java навіть для майбутніх версій (30+)
		versionID := minecraft.LoaderVersionID(build.Loader, build.MCVersion, build.LoaderVersion)
		vjsonPath := filepath.Join(a.cfg.Dir(), "minecraft", "versions", versionID, versionID+".json")
		earlyComponent := ""
		if vjsonData, err := os.ReadFile(vjsonPath); err == nil {
			var earlyVjson minecraft.VersionJSON
			if json.Unmarshal(vjsonData, &earlyVjson) == nil && earlyVjson.JavaVersion.Component != "" {
				earlyComponent = earlyVjson.JavaVersion.Component
			}
		}
		installed, err := a.javaInst.EnsureMajorWithComponent(recommended, earlyComponent)
		a.javaInst.SetProgressHandler(prevJava)
		if err != nil {
			err = fmt.Errorf("вбудована Java %d: %w", recommended, err)
			a.emitLaunchError(instanceID, err)
			return err
		}
		javaPath = installed
	} else {
		javaPath = a.cfg.ResolveJavaExe(javaPath)
		if major := java.DetectMajor(javaPath); major > 0 && major != recommended {
			if major < recommended {
				// Forge/NeoForge (FML/bootstrap) ЖОРСТКО вимагає Java 17+/21+ —
				// на старішій JVM падає ще ДО старту гри з незрозумілим
				// "Unsupported major.minor version" / FindException. Замість
				// криптичного крашу одразу даємо зрозумілу помилку: де її
				// виправити (налаштування Java або вбудована Java лаунчера).
				// Для ванилла/fabric/quilt — попередження (вони терплячіші).
				if l := strings.ToLower(build.Loader); l == "forge" || l == "neoforge" {
					err := fmt.Errorf("Java %d замало для %s %s: потрібна Java %d+ (Forge/NeoForge). Оберіть відповідну Java у Налаштуваннях → Теки та шляхи, або використайте вбудовану Java лаунчера", major, build.Loader, build.MCVersion, recommended)
					a.emitLaunchError(instanceID, err)
					return err
				}
				javaWarn = fmt.Sprintf("Попередження: Java %d НЕ підходить для Minecraft %s (потрібна Java %d+) — гра може не запуститись.", major, build.MCVersion, recommended)
			} else {
				javaWarn = fmt.Sprintf("Попередження: Java %d новіша за рекомендовану (%d) для Minecraft %s — на старих версіях можливі збої.", major, recommended, build.MCVersion)
			}
		}
	}

	// Встановлюємо/перевіряємо лоадер збірки. Для vanilla повертає саму
	// mcVersion; для Fabric/Quilt/Forge/NeoForge — ID loader-профілю
	// (versions/<id>.json з InheritsFrom), яким і запускається гра.
	// Processor Java = та, що для гри (Forge processors потребують Java).
	a.inst.SetProcessorJava(javaPath)
	a.emitLaunchProgress(instanceID, LaunchStageLoader, "Встановлення лоадера...")
	// Реальний прогрес встановлення Minecraft (клієнтський jar, бібліотеки,
	// асети, нативки — minecraft.InstallProgress з відсотками) форвардиться
	// у launch:progress: "Бібліотеки: 12/45", "Асети: 30/100" тощо.
	prevInst := a.inst.InstallProgressHandler()
	a.inst.SetInstallProgressHandler(func(p minecraft.InstallProgress) {
		a.emit("installer:progress", p)
		a.emitLaunchProgressPct(instanceID, LaunchStageLoader, p.Message, p.Percent)
	})
	launchVersion, err := a.inst.EnsureLoader(build.Loader, build.MCVersion, build.LoaderVersion)
	if err != nil {
		a.inst.SetInstallProgressHandler(prevInst)
		err = fmt.Errorf("ensure loader: %w", err)
		a.emitLaunchError(instanceID, err)
		return err
	}
	vjson, err := a.inst.EnsureVersion(launchVersion)
	a.inst.SetInstallProgressHandler(prevInst)
	if err != nil {
		err = fmt.Errorf("ensure version: %w", err)
		a.emitLaunchError(instanceID, err)
		return err
	}

	// Джерело істини для потрібної Java — поле javaVersion у самому
	// version.json (доступне лише ПІСЛЯ EnsureVersion, тому перевірка
	// саме тут, а не разом з початковим RecommendedMajor вище). Якщо
	// json явно вказує інший мажор, ніж наша евристика за номером версії —
	// довіряємо json (так само чинить Prism і офіційний Mojang launcher:
	// напр. Quilt 26.2 → java-runtime-epsilon → Java 25).
	//
	// ВАЖЛИВО про Forge 1.17.x (37.0.0): vanilla-батько вказує Java 16
	// (Mojang збирав 1.17.1 на Java 16), і НЕ МОЖНА піднімати її до 17:
	// новіші збірки JDK 17 (8u321+/11u/17u, жовтень 2021+) змінили
	// внутрішній конструктор sun.security.util.ManifestEntryVerifier,
	// а старий bootstraplauncher 0.1.x/modlauncher 9.x Forge 37.0.0
	// викликає його через reflection — це падає NoSuchMethodError.
	// Java 16 цих змін не має, тож для 1.17.x беремо рівно те, що
	// декларує version.json, без підняття до 17.
	effective := minecraft.EffectiveJavaMajor(vjson, filepath.Join(a.cfg.Dir(), "minecraft"))
	component := minecraft.EffectiveJavaComponent(vjson, filepath.Join(a.cfg.Dir(), "minecraft"))
	if effective > 0 && effective != recommended {
		recommended = effective
		if rawJavaPath == "" || rawJavaPath == a.cfg.JavaDir() {
			// Вбудована Java лаунчера — перевстановлюємо/перевибираємо на
			// правильний мажор перед фактичним запуском. Використовуємо
			// component з version.json (якщо є) для повної автономності.
			prevJava := a.javaInst.ProgressHandler()
			a.javaInst.SetProgressHandler(func(p java.Progress) {
				a.emit("java:progress", p)
				a.emitLaunchProgressPct(instanceID, LaunchStageJava, p.Message, p.Percent)
			})
			installed, err := a.javaInst.EnsureMajorWithComponent(recommended, component)
			a.javaInst.SetProgressHandler(prevJava)
			if err != nil {
				err = fmt.Errorf("вбудована Java %d (за version.json): %w", recommended, err)
				a.emitLaunchError(instanceID, err)
				return err
			}
			javaPath = installed
		} else {
			// Ручний шлях до Java — не чіпаємо, лише попереджаємо, якщо
			// версія явно не збігається.
			if major := java.DetectMajor(javaPath); major > 0 && major != recommended {
				javaWarn = fmt.Sprintf("Попередження: version.json цієї збірки вказує Java %d, а обрана Java — %d. Гра може не запуститись.", recommended, major)
			}
		}
	}

	inst := model.Instance{
		ID:            build.ID,
		Name:          build.Name,
		MCVersion:     launchVersion,
		Loader:        build.Loader,
		LoaderVersion: build.LoaderVersion,
		IsShaurma:     build.IsShaurma,
	}

	// Override-параметри збірки зливаються з глобальними Settings через
	// merge-хелпери (strOverride/intOverride/boolPtrOverride в overrides.go):
	// порожнє/нульове значення override означає «успадкувати з Settings».
	// Кастомна збірка має власні RAM-налаштування (UseCustomRAM) — вони
	// сильніші за per-instance override, бо задані явно у формі збірки.
	maxRAM := settings.MaxRAM
	if build.UseCustomRAM && build.MaxRAMMB > 0 {
		maxRAM = build.MaxRAMMB
	} else {
		maxRAM = intOverride(settings.MaxRAM, icfg.MaxRAMOverride)
	}
	// Мінімальна RAM (-Xms) окремим полем: якщо для збірки не задано,
	// LaunchConfig.MinRAM лишається 0 і Launcher сам порахує ram/2 —
	// та сама поведінка, що й раніше, коли цього поля не було.
	minRAM := 0
	if build.UseCustomRAM && build.MinRAMMB > 0 {
		minRAM = build.MinRAMMB
	} else {
		minRAM = intOverride(0, icfg.MinRAMOverride)
	}
	javaArgs := strOverride(settings.JavaArgs, icfg.JavaArgsOverride)
	fullscreen := boolPtrOverride(settings.Fullscreen, icfg.FullscreenOverride)
	windowWidth := intOverride(settings.WindowWidth, icfg.WindowWidthOverride)
	windowHeight := intOverride(settings.WindowHeight, icfg.WindowHeightOverride)
	preLaunch := strOverride(settings.PreLaunchCommand, icfg.PreLaunchCommandOverride)
	wrapper := strOverride(settings.WrapperCommand, icfg.WrapperCommandOverride)
	postExit := strOverride(settings.PostExitCommand, icfg.PostExitCommandOverride)
	envVars := strOverride(settings.EnvVars, icfg.EnvVarsOverride)

	// Службовий рядок у консоль: яка збірка стартує. Буфер консолі
	// прив'язується до цієї (акаунт × збірка) пари — кожен акаунт має
	// власний лог тієї самої збірки.
	buf := a.consoleBufFor(account.ID, build.ID)
	buf.AppendLevel(console.LevelSystem, fmt.Sprintf("Запуск збірки \"%s\" (%s %s)...", build.Name, build.Loader, build.MCVersion))
	if javaWarn != "" {
		buf.AppendLevel(console.LevelWarn, javaWarn)
	}

	// Кожна (акаунт × збірка) — окрема сесія з власним Launcher: та сама
	// збірка може йти на кількох акаунтах незалежно, ізоляція повна.
	launcher := minecraft.NewLauncher()
	a.wireSessionLauncher(launcher, account, build, buf)
	if err := a.sessionsM.Start(&sessions.Session{
		AccountID:   account.ID,
		BuildID:     build.ID,
		AccountName: account.Username,
		BuildName:   build.Name,
		Launcher:    launcher,
	}); err != nil {
		a.emitLaunchError(instanceID, err)
		return err
	}

	err = launcher.Launch(minecraft.LaunchConfig{
		Instance: inst,
		JavaPath: javaPath,
		MaxRAM:   maxRAM,
		MinRAM:   minRAM,
		JavaArgs: javaArgs,
		Account:  account,
		GameDir:  gameDir,
		// Спільна база Minecraft (versions/libraries/assets) — інсталер качає
		// її в <dataDir>/minecraft; класшлях і --assetsDir рахуються звідси,
		// а не з теки конкретної збірки.
		MinecraftDir: filepath.Join(a.cfg.Dir(), "minecraft"),
		VersionJSON:  vjson,
		SaveLogs:     settings.SaveLogs,

		// Вікно гри
		Fullscreen:   fullscreen,
		WindowWidth:  windowWidth,
		WindowHeight: windowHeight,

		// Команди
		PreLaunchCommand: preLaunch,
		WrapperCommand:   wrapper,
		PostExitCommand:  postExit,
		EnvVars:          envVars,

		// Автоприєднання
		AutoJoinType:   autoJoinType(icfg),
		AutoJoinWorld:  icfg.AutoJoinWorld,
		AutoJoinServer: icfg.AutoJoinServer,
	})
	if err != nil {
		a.sessionsM.Remove(account.ID, build.ID)
		a.emitLaunchError(instanceID, err)
		return err
	}
	a.emitLaunchDone(instanceID)
	// Зберігаємо PID гри на диск: якщо лаунчер уб'ють/закриють, а гра лишиться
	// жити, наступний запуск лаунчера відновить зв'язок (restoreRunningGame).
	a.saveRunningGame(build.ID, account.ID, launcher.PID())
	// Запам'ятовуємо останню запущену збірку: консоль прив'язується до неї,
	// навіть коли гра вже зупинена (панель «Запустити знову» у консолі).
	// activeInstanceID теж оновлюємо — запущена збірка автоматично стає
	// «активною» для окремого вікна консолі.
	a.lastInstanceID = build.ID
	a.activeInstanceID = build.ID
	// Новий запуск — скидаємо стан попереднього крашу для цієї збірки,
	// щоб консоль не показувала застарілий AI-діагноз/«код виходу» під
	// час нової сесії.
	a.consoleDiagMu.Lock()
	delete(a.consoleExit, build.ID)
	delete(a.consoleDiag, build.ID)
	delete(a.consoleDiagAt, build.ID)
	a.consoleDiagMu.Unlock()
	a.emit("game:started", build.ID)
	a.emit("build:started", model.BuildStartEvent{
		BuildID:     build.ID,
		AccountID:   account.ID,
		AccountName: account.Username,
	})
	// showConsoleOnLaunch → авто-показ окремого вікна консолі САМЕ ЦІЄЇ
	// збірки (саме "вікна", не віджета). buildID передаємо явно: у режимі
	// кількох вікон консолі по збірках вікно має відкритись для тієї
	// збірки, що запускається.
	a.maybeAutoShowConsole("launch", build.ID)
	return nil
}

// wireSessionLauncher підвішує на окремий Launcher сесії хендлери виводу,
// виходу та ігрового часу. Лог записується у буфер ЗАВЖДИ (навіть якщо
// вікно консолі закрите) і одночасно йде подіями на фронтенд: звичайна
// console:line (для сумісності) + buildConsole:line з контекстом збірки/
// акаунта (для майбутньої «однієї консолі на збірку з перемикачем
// акаунтів»).
func (a *App) wireSessionLauncher(l *minecraft.Launcher, account model.Account, build *launchBuild, buf *console.Buffer) {
	l.SetOutputHandler(func(line string) {
		if buf != nil {
			buf.Append(line)
			// «console:line» (живий потік для вікна/віджета) шлемо ЛИШЕ для
			// поточного буфера консолі — перемикання акаунтів не повинно
			// домішувати чужі логи у відображувану консоль. Скоупований
			// buildConsole:line йде завжди (для перемикача/архіву).
			if buf == a.activeConsoleBuf() {
				a.emit("console:line", line)
			}
		}
		a.emit("buildConsole:line", model.BuildConsoleLine{
			BuildID:   build.ID,
			AccountID: account.ID,
			Line:      line,
		})
	})
	l.SetExitHandler(func(code int) {
		if buf != nil {
			lvl := console.LevelSystem
			if code != 0 {
				lvl = console.LevelError
			}
			buf.AppendLevel(lvl, fmt.Sprintf("Процес гри завершено з кодом виходу %d", code))
		}
		// Останній код виходу по збірці — спільний стан для всіх копій
		// консолі (вікно + вкладка): GetConsoleContext повертає crashed /
		// lastExitCode, щоб після повторного відкриття консоль знала, що
		// гра впала, і показувала AI-панель (навіть якщо подія game:exit
		// пройшла, поки консоль була закрита).
		a.consoleDiagMu.Lock()
		a.consoleExit[build.ID] = code
		a.consoleDiagMu.Unlock()
		a.emit("game:exit", code)
		a.emit("build:exit", model.BuildExitEvent{
			BuildID:   build.ID,
			AccountID: account.ID,
			Code:      code,
		})
		a.sessionsM.Remove(account.ID, build.ID)
		// Гра завершилась (штатно/крашем/ручно) — файл стану запущеної гри
		// більше не потрібен (наступний запуск лаунчера не знайде мертвий PID).
		a.clearRunningGame()
		// Гра закрилась (крашем чи штатно) — головне вікно лаунчера має
		// піднятись ПЕРШИМ, і лише ПОТІМ (за потреби) вікно консолі.
		//
		// Раніше порядок був: emit("game:exit") → maybeAutoShowConsole.
		// game:exit — асинхронна подія, яку фронтенд ловить і вже САМ
		// викликає RestoreWindowAfterGame() через JS event loop. Але
		// maybeAutoShowConsole нижче виконувався одразу й СИНХРОННО у Go
		// і встигав реально показати native-вікно консолі раніше, ніж
		// фронтенд встигав дійти до свого обробника — тому користувач
		// бачив спочатку вікно консолі (яке виглядає як "консоль"), а
		// вже потім головне вікно лаунчера. Тепер піднімаємо головне
		// вікно тут напряму з Go (той самий код, що й RestoreWindowAfterGame),
		// синхронно й ДО показу консолі — порядок гарантований.
		if a.window != nil {
			a.window.Show()
			a.window.UnMinimise()
		}
		// Краш (ненульовий код) + showConsoleOnCrash → показати вікно
		// консолі. Штатний вихід
		// (код 0) + showConsoleOnClose → показати останні рядки логу.
		// Ручна зупинка (stoppedByUser) НЕ вважається крашем. Прапорець
		// споживається РІВНО ОДИН раз на будь-якому виході (Swap).
		wasUserStop := a.stoppedByUser.Swap(false)
		if code != 0 && !wasUserStop {
			a.maybeAutoShowConsole("crash", build.ID)
		} else if code == 0 {
			a.maybeAutoShowConsole("close", build.ID)
		}
	})
	// Ігровий час: записуємо тривалість сесії у playtime.json, якщо
	// увімкнено налаштування RecordGameTime.
	l.SetPlaytimeHandler(func(instanceID string, seconds int64) {
		if a.cfg.GetSettings().RecordGameTime {
			_ = a.cfg.AddPlaytime(instanceID, seconds)
		}
	})
}

// ── Збереження PID гри (переживає перезапуск лаунчера) ──────────────────
// ТЗ: «якщо вбити трей і перезапустити лаунчер, він не має втратити
// зв'язок з грою і думати, що вона не запущена». Тому при успішному
// старті гри пишемо у <dataDir>/running-game.json: buildID, accountID,
// PID процесу. При старті лаунчера restoreRunningGame читає файл і, якщо
// процес із збереженим PID досі живий, — емітує game:started (фронт і
// трей одразу бачать запущену гру, час гри не пропадає). Файл чиститься
// на game:exit (штатний вихід або краш).

// runningGamePath — шлях до файлу стану запущеної гри.
func (a *App) runningGamePath() string {
	return filepath.Join(a.cfg.Dir(), "running-game.json")
}

// saveRunningGame записує поточну запущену гру (buildID, accountID, PID).
func (a *App) saveRunningGame(buildID, accountID string, pid int) {
	data, _ := json.Marshal(map[string]any{
		"buildId":   buildID,
		"accountId": accountID,
		"pid":       pid,
		"savedAt":   time.Now().Unix(),
	})
	os.WriteFile(a.runningGamePath(), data, 0644)
}

// clearRunningGame видаляє файл стану запущеної гри (гра завершилась).
func (a *App) clearRunningGame() {
	os.Remove(a.runningGamePath())
}

// restoreRunningGame намагається відновити стан «гра запущена» після
// перезапуску лаунчера (див. коментар пакунка вище). Якщо файлу немає
// або PID мертвий — файл прибирається і нічого не відбувається.
func (a *App) restoreRunningGame() {
	data, err := os.ReadFile(a.runningGamePath())
	if err != nil {
		return
	}
	var st struct {
		BuildID   string `json:"buildId"`
		AccountID string `json:"accountId"`
		PID       int    `json:"pid"`
	}
	if err := json.Unmarshal(data, &st); err != nil {
		a.clearRunningGame()
		return
	}
	// PID мертвий (гра закінчилась, поки лаунчер був вимкнений) — просто
	// прибираємо файл: стан «запущена» брехав би.
	if !isProcessAlive(st.PID) {
		a.clearRunningGame()
		return
	}
	// Гра жива: реєструємо «відновлену» сесію — Launcher, що прийняв PID
	// (AdoptProcess). Завдяки цьому IsGameRunning()/GetBuilds() бачать гру
	// запущеною, sidebar показує «Запущена», а StopGame реально вбиває
	// процес по PID (без цього відновлення було б декоративним: подія
	// game:started на старті губиться — фронт ще не підписаний, і сесії
	// в менеджері не було б).
	if _, exists := a.cfg.GetAccountByID(st.AccountID); st.AccountID == "" || !exists {
		// Акаунт видалено або невідомий — все одно показуємо гру як
		// запущену (вона ж реально йде), але без прив'язки до акаунта.
		a.lastInstanceID = st.BuildID
		a.activeInstanceID = st.BuildID
		a.emit("game:started", st.BuildID)
		return
	}
	launcher := minecraft.NewLauncher()
	if !launcher.AdoptProcess(st.PID) {
		a.clearRunningGame()
		return
	}
	// Буфер консолі для цієї (акаунт × збірка) пари — консоль після
	// перезапуску показує ту саму збірку як запущену.
	buf := a.consoleBufFor(st.AccountID, st.BuildID)
	buf.AppendLevel(console.LevelSystem, "Зв'язок з грою відновлено після перезапуску лаунчера.")
	if err := a.sessionsM.Start(&sessions.Session{
		AccountID: st.AccountID,
		BuildID:   st.BuildID,
		Launcher:  launcher,
	}); err == nil {
		a.lastInstanceID = st.BuildID
		a.activeInstanceID = st.BuildID
		// Активний акаунт = той, під яким йде гра (щоб «Зупинити» влучив у
		// правильну сесію). Аккаунт точно існує — перевірили вище.
		_ = a.cfg.SetActiveAccountID(st.AccountID)
	}
	// Моніторинг завершення відновленого процесу: AdoptProcess не веде
	// власного waitExit (це чужий процес, запущений поза нашим exec.Cmd), а
	// exit-хендлер wireSessionLauncher для нього не підключався. Без цього
	// після виходу гри сесія лишалась би в менеджері назавжди: IsGameRunning
	// → true, sidebar показував би «Запущена» вічно, LaunchInstance падав би
	// з "build already running", а running-game.json ніколи б не чистився
	// (баг, знайдений рев'ю). Поллимо isProcessAlive і по завершенню гри
	// чистимо сесію/файл стану і шлемо game:exit — той самий шлях, що й
	// звичайний exit-хендлер.
	go func() {
		for isProcessAlive(st.PID) {
			time.Sleep(3 * time.Second)
		}
		// Споживаємо stoppedByUser: якщо гра була зупинена вручну (StopGame),
		// прапорець уже стоїть — не даємо йому «протекти» у майбутні краші
		// (інакше наступний реальний краш помилково вважався б ручною
		// зупинкою і не показав би консоль).
		a.stoppedByUser.Swap(false)
		a.sessionsM.Remove(st.AccountID, st.BuildID)
		a.clearRunningGame()
		a.emit("game:exit", 0)
		a.emit("build:exit", model.BuildExitEvent{
			BuildID:   st.BuildID,
			AccountID: st.AccountID,
			Code:      0,
		})
	}()
	a.emit("game:started", st.BuildID)
}

func (a *App) StopGame() error {
	// Ручна зупинка: позначаємо, щоб ненульовий код виходу від кілла не
	// трактувався як краш (інакше консоль авто-показувалась би на ручній
	// зупинці за showConsoleOnCrash). Якщо зупинка
	// НЕ вдалась (гра не запущена, кілл не спрацював) — знімаємо прапорець,
	// щоб наступний справжній краш не був помилково "з'їдений".
	a.stoppedByUser.Store(true)
	acc := a.cfg.ActiveAccount()
	if acc.ID == "" {
		return nil
	}
	// Спершу — сесія останньої запущеної збірки на цьому акаунті, інакше —
	// будь-яка активна сесія акаунта (перемикання акаунтів → незалежні
	// запуски, зупиняємо саме свою).
	if a.lastInstanceID != "" && a.sessionsM.IsRunning(acc.ID, a.lastInstanceID) {
		if err := a.sessionsM.Stop(acc.ID, a.lastInstanceID); err != nil {
			a.stoppedByUser.Store(false)
			return err
		}
		return nil
	}
	for _, s := range a.sessionsM.Snapshot() {
		if s.AccountID == acc.ID {
			if err := a.sessionsM.Stop(s.AccountID, s.BuildID); err != nil {
				a.stoppedByUser.Store(false)
				return err
			}
			return nil
		}
	}
	return nil
}

// IsGameRunning — чи запущена будь-яка гра (на будь-якому акаунті).
func (a *App) IsGameRunning() bool { return a.sessionsM.AnyRunning() }

// HideToTray ховає вікно у системний трей. Використовується налаштуванням
// "Закривати лаунчер під час гри" (closeOnLaunch): після успішного
// запуску гри вікно ховається, а повертається автоматично на подію
// game:exit (RestoreWindowAfterGame).
func (a *App) HideToTray() {
	if a.window == nil {
		return
	}
	a.window.Hide()
	a.ensureTray()
}

// RestoreWindowAfterGame викликається фронтендом на подію game:exit.
// Якщо вікно було сховане у трей (гра йшла, лаунчер закрили), показує
// його назад — трей більше не потрібен, бо гра не запущена. Якщо
// вікно й так було видиме — просто no-op.
func (a *App) RestoreWindowAfterGame() {
	if a.window == nil {
		return
	}
	a.window.Show()
	a.window.UnMinimise()
}

func (a *App) SearchContent(query string) ([]model.BrowserEntry, error) {
	return a.modrinth.Search(query, 20)
}

func (a *App) GetMods(instanceID string) ([]model.ModEntry, error) {
	settings := a.cfg.GetSettings()
	modsDir := filepath.Join(settings.InstanceDir, instanceID, ".minecraft", "mods")
	return a.modMgr.ListMods(modsDir)
}

func (a *App) ToggleMod(instanceID, fileName string, enabled bool) error {
	settings := a.cfg.GetSettings()
	modsDir := filepath.Join(settings.InstanceDir, instanceID, ".minecraft", "mods")
	return a.modMgr.ToggleMod(modsDir, fileName, enabled)
}

func (a *App) GetSettings() model.Settings { return a.cfg.GetSettings() }

// SaveSettings зберігає налаштування і одразу застосовує ті, що впливають
// на поточні компоненти: ліміти завантажень (download-engine). Решта
// (акцент/тема/шрифт) застосовується фронтендом миттєво.
func (a *App) SaveSettings(s model.Settings) error {
	if err := a.cfg.UpdateSettings(s); err != nil {
		return err
	}
	a.applyDownloadLimits()
	// Після зміни налаштувань — оновлюємо ліміт буфера консолі.
	if a.consoleBuf != nil {
		a.consoleBuf.SetMaxLines(s.ConsoleMaxLines)
	}
	return nil
}
func (a *App) GetAppDir() string { return a.cfg.Dir() }

// SaveFolderPaths зберігає ОСТАТОЧНО обрані користувачем шляхи тек
// (installations для збірок, Java) у launcher-location.json — стабільна
// тека Local, НЕ всередині папки даних. Тому перенесення/видалення папки
// даних не губить налаштування тек: лаунчер завжди знає, де його збірки
// і Java. javaPath може бути як ПАПКОЮ Java (новий формат — саме його
// показує UI), так і шляхом до javaw.exe (старий формат): з exe-шляху
// виводиться тека Java (два рівні вгору: <javaDir>/bin/javaw.exe).
func (a *App) SaveFolderPaths(installations, javaPath string) error {
	javaDir := ""
	if javaPath != "" {
		if strings.HasSuffix(strings.ToLower(javaPath), "javaw.exe") {
			javaDir = filepath.Dir(filepath.Dir(javaPath)) // <javaDir>/bin/javaw.exe
		} else {
			javaDir = javaPath // вже папка Java — як показує UI
		}
	}
	return a.cfg.SetFolderPaths(installations, javaDir)
}

// GetFolderPaths повертає поточні шляхи всіх тек лаунчера для вкладки
// «Теки та шляхи»: папка даних, installations, java, cache, logs, config
// + кастомні значення підтек ("" = дефолт відносно папки даних).
func (a *App) GetFolderPaths() model.FolderPaths {
	return a.cfg.FolderPaths()
}

// SetFolderPath змінює одну теку лаунчера за іменем (kind): baseDir,
// installations, java, cache, logs. Для baseDir використовується SetAppDir
// (з перебудовою залежних компонентів); для решти — override у
// launcher-location.json. installations/java впливають на запуск гри,
// тому після їх зміни синхронізуємо settings.InstanceDir/JavaPath.
func (a *App) SetFolderPath(kind, path string) error {
	if kind == "baseDir" {
		if err := a.SetAppDir(path); err != nil {
			return err
		}
		// Синхронізація settings.InstanceDir/JavaPath робиться ТУТ (у
		// налаштуваннях), а не в SetAppDir — бо SetAppDir викликається ще
		// й майстром першого запуску, де писати settings.json зарано
		// (це створило б файл і зламало б IsFirstRun/відновлення майстра).
		return a.syncFolderSettings()
	}
	if err := a.cfg.SetFolderPath(kind, path); err != nil {
		return err
	}
	if kind == "installations" || kind == "java" {
		return a.syncFolderSettings()
	}
	return nil
}

// syncFolderSettings підтягує актуальні шляхи збірок/Java у settings.json,
// щоб запуск гри використовував нові теки. Кастомні override у
// launcher-location.json лишаються у перевазі (InstancesDir/JavaDir
// їх поважають).
func (a *App) syncFolderSettings() error {
	s := a.cfg.GetSettings()
	s.InstanceDir = a.cfg.InstancesDir()
	s.JavaPath = a.cfg.JavaDir()
	return a.cfg.UpdateSettings(s)
}

// SetAppDir змінює папку даних лаунчера (вибір у майстрі «Теки та Java»).
// Крім запису в launcher-location.json перебудовує залежні від папки даних
// компоненти (download-engine, minecraft-installer, java-installer), щоб
// вони качали й клали файли вже в нові теки. Прогрес-хендлери підвішуються
// заново через rewireComponents().
func (a *App) SetAppDir(dir string) error {
	if err := a.cfg.SetAppDir(dir); err != nil {
		return err
	}
	a.rewireComponents()
	// Після перестворення двигуна — заново застосовуємо ліміти.
	a.applyDownloadLimits()
	return nil
}

// rewireComponents перестворює компоненти, чиї теки залежать від папки
// даних лаунчера, і перепідвішує прогрес-хендлери (вони єдині реєструються
// в startup, тому після SetAppDir це треба повторити).
func (a *App) rewireComponents() {
	a.dl = download.NewEngine()
	a.inst = minecraft.NewInstaller(filepath.Join(a.cfg.Dir(), "minecraft"))
	a.javaInst = java.NewInstaller(a.cfg.JavaDir())
	a.avatars = avatar.NewService(filepath.Join(a.cfg.Dir(), "avatars"))
	// Реєстр збірок пересоздаємо: builds.json живе у папці даних, яка
	// могла змінитись через SetAppDir.
	a.buildsReg = builds.NewRegistry(filepath.Join(a.cfg.Dir(), "builds.json"))
	a.buildsReg.Load()
	// custom-packs.json живе в теці конфігурації лаунчера — так само
	// перестворюється при зміні AppDir, як і решта реєстрів вище.
	a.customPacks = custompack.NewStore(a.cfg.ConfigDir())

	// Хендлери виводу/виходу гри підвішуються на КОЖЕН екземпляр Launcher
	// окремої сесії (акаунт × збірка) у wireSessionLauncher — тут лише
	// прогрес-хендлери встановлення Java/Minecraft (реальні завантаження
	// збірок звітують через sync:progress від sync.Runner).
	a.inst.SetProgressHandler(func(msg string) {
		a.emit("installer:log", msg)
	})
	a.javaInst.SetStatusHandler(func(msg string) {
		a.emit("java:status", msg)
	})

	// syncMgr — новий двочерговий рушій V2. onEvent летить на КОЖЕН
	// прогрес-тік (~300мс) обох черг разом і живить одразу sidebar,
	// картку в "Моїх збірках" і сторінку збірки (усі три читають той
	// самий a.dlTransient[packID] через recordSyncProgress).
	a.syncMgr = syncengine.NewManager(func(p syncengine.RunnerProgress) {
		a.recordSyncProgress(p)
		a.emit("sync:progress", p)
	})
}

// consoleKey — унікальний ідентифікатор буфера консолі: (акаунт × збірка).
type consoleKey struct {
	accountID string
	buildID   string
}

// consoleBufFor повертає (створюючи за потреби) буфер консолі для пари
// (акаунт, збірка) і робить його ПОТОЧНИМ — консоль показує саме його.
// Перемикання акаунтів дає незалежні запуски однієї збірки, тому кожен
// має власний лог; у перемикачі консолі користувач може переглянути
// будь-який.
func (a *App) consoleBufFor(accountID, buildID string) *console.Buffer {
	a.consoleBufMu.Lock()
	defer a.consoleBufMu.Unlock()
	key := consoleKey{accountID: accountID, buildID: buildID}
	if b, ok := a.consoleBufs[key]; ok {
		a.consoleBuf = b
		a.consoleCurKey = key
		return b
	}
	max := 3000
	if s := a.cfg.GetSettings(); s.ConsoleMaxLines > 0 {
		max = s.ConsoleMaxLines
	}
	b := console.NewBuffer(max)
	a.consoleBufs[key] = b
	a.consoleBuf = b
	a.consoleCurKey = key
	return b
}

// activeConsoleBuf повертає ПОТОЧНИЙ буфер консолі під м'ютексом.
func (a *App) activeConsoleBuf() *console.Buffer {
	a.consoleBufMu.Lock()
	defer a.consoleBufMu.Unlock()
	return a.consoleBuf
}

// recordSyncProgress наповнює dlTransient-канал прогресу (житвить sidebar,
// картку збірки в "Моїх збірках" і сторінку збірки одночасно) — єдиний
// шлях, яким тепер надходить прогрес завантажень збірок (від sync.Runner).
// (Колись був ще recordDownloadProgress для мертвої сесійної системи
// download.Engine — видалено разом з нею.)
// (syncengine.Manager): той самий dlTransient-канал живить sidebar, картку
// збірки в "Моїх збірках" і сторінку збірки одночасно, тож усі три місця
// UI завжди показують один консистентний прогрес/стан. Mode/Stage/
// NoInternet прокидаються прямо в builds.Progress, щоб UI показував
// "Зупинено", "Скасовано", "Немає інтернету" без окремих подій-тостів.
func (a *App) recordSyncProgress(sp syncengine.RunnerProgress) {
	p := &builds.Progress{
		Percent:     sp.OverallPercent,
		BytesDone:   sp.BytesDownloaded,
		BytesTotal:  sp.BytesTotal,
		FilesDone:   sp.FilesCompleted,
		FilesTotal:  sp.FilesTotal,
		CurrentFile: sp.CurrentFile,
		SpeedBPS:    int64(sp.SpeedBPS),
		Mode:        string(sp.Mode),
		Stage:       string(sp.Stage),
		NoInternet:  sp.NoInternet,
	}
	a.dlTransientMu.Lock()
	defer a.dlTransientMu.Unlock()

	// ВАЖЛИВО про порядок м'ютексів: тут ми тримаємо dlTransientMu і всередині
	// викликаємо isLaunchTracked (бере launchPacksMu). Зворотного напрямку
	// НЕ існує (setLaunchTracked/unsetLaunchTracked тримають лише launchPacksMu
	// і ніколи не беруть dlTransientMu), тому порядок завжди
	// dlTransientMu → launchPacksMu — без deadlock. Не порушувати при змінах.

	// Під час LaunchInstance цієї збірки (setLaunchTracked) синхронізація
	// файлів показується ще й у launch:progress (стадія worker_update) —
	// сегментний бар на сторінці збірки живе реальним % качки.
	if a.isLaunchTracked(sp.PackID) && (sp.Mode == syncengine.ModeRunning || sp.Mode == syncengine.ModePaused) {
		a.emitLaunchProgressPct(sp.PackID, LaunchStageWorkerUpdate, sp.CurrentFile, sp.OverallPercent)
	}

	switch sp.Mode {
	case syncengine.ModeRunning:
		a.dlTransient[sp.PackID] = p
	case syncengine.ModePaused:
		// Пауза лишає прогрес видимим (не зникає з картки) — кнопка в UI
		// стає "Продовжити", тому transient-запис навмисно НЕ видаляється.
		a.dlTransient[sp.PackID] = p
	case syncengine.ModeCancelled:
		delete(a.dlTransient, sp.PackID)
	case syncengine.ModeComplete:
		delete(a.dlTransient, sp.PackID)
		version := ""
		if e, err := a.packs.GetPackByID(sp.PackID); err == nil && e != nil {
			version = firstNonEmpty(e.Version, e.UpdatedAt)
		}
		a.buildsReg.SetInstalled(sp.PackID, builds.KindShaurma, version)
		a.emit("builds:changed", sp.PackID)
	case syncengine.ModeError:
		// Помилка: transient прибираємо, щоб збірка повернулась до свого
		// реального файлового статусу, а UI показав помилку через подію
		// download:error (тост), а не «завислі 100%» на картці.
		delete(a.dlTransient, sp.PackID)
	default:
		a.dlTransient[sp.PackID] = p
	}
}

// GetJavaStatus повертає стан вбудованої Java лаунчера: чи вона вже
// встановлена у теку лаунчера та шлях до неї. Використовується кроком
// «Теки та Java» майстра та сторінкою налаштувань.
func (a *App) GetJavaStatus() model.JavaStatus {
	return model.JavaStatus{
		Installed: a.javaInst.IsInstalled(),
		Dir:       a.cfg.JavaDir(),
		ExePath:   a.javaInst.ExePath(),
		Version:   a.javaInst.Version(),
	}
}

// EnsureJava встановлює вбудовану Java лаунчера, якщо її ще немає
// (лаунчер сам ставить Java у свою теку). Прогрес надсилається
// подіями "java:status" для відображення в інтерфейсі.
func (a *App) EnsureJava() error {
	_, err := a.javaInst.EnsureInstalled()
	return err
}

// GetSettingsSchema повертає декларативну схему налаштувань (див.
// internal/config/schema.go). Фронтенд рендерить сторінку «Налаштування»
// та майстер із цих даних — додавання нового параметра = один рядок у
// схемі, без дублювання у UI.
func (a *App) GetSettingsSchema() []model.SettingDef {
	return config.SettingsSchema()
}

// GetSystemInfo повертає інформацію про систему (CPU/RAM/диск) для
// кроку «Продуктивність» майстра та сторінки налаштувань.
func (a *App) GetSystemInfo() model.SystemInfo {
	return sysinfo.Collect(a.cfg.Dir())
}
func (a *App) GetAccounts() []model.Account { return a.cfg.GetAccounts() }

// GetVersion повертає поточну версію лаунчера для вкладки «Про лаунчер».
func (a *App) GetVersion() string {
	if a.updater != nil {
		return a.updater.CurrentVersion()
	}
	return appVersion
}

// GetPlaytime повертає записаний ігровий час (секунди на збірку) для
// вкладки «Ігровий час». Зберігається у playtime.json.
func (a *App) GetPlaytime() map[string]int64 {
	return a.cfg.GetPlaytime()
}

// GetStorageUsage підраховує зайняте місце по теках лаунчера для вкладки
// «Пам'ять і кеш»: installations, java, minecraft, cache, logs, avatars
// та решта в папці даних. Повертає також вільне місце на диску.
func (a *App) GetStorageUsage() model.StorageInfo {
	dirs := []struct {
		name string
		path string
	}{
		{"installations", a.cfg.InstancesDir()},
		{"java", a.cfg.JavaDir()},
		{"minecraft", filepath.Join(a.cfg.Dir(), "minecraft")},
		{"cache", a.cfg.CacheDir()},
		{"logs", a.cfg.LogsDir()},
		{"avatars", filepath.Join(a.cfg.Dir(), "avatars")},
	}
	info := model.StorageInfo{Entries: []model.StorageEntry{}}
	for i, d := range dirs {
		// Живий прогрес сканування тек (вкладка «Пам'ять і кеш»): бекенд
		// рахує розміри багатогігабайтних тек послідовно, а UI показує
		// скелетон з прогрес-баром замість «пустоти, яка потім заповнюється
		// інформацією» (раніше розділ був порожнім, поки все не дорахується).
		a.emit("storage:usage-scan", map[string]any{"done": i, "total": len(dirs), "label": d.name})
		size := dirSize(d.path)
		info.Entries = append(info.Entries, model.StorageEntry{Name: d.name, Path: d.path, Bytes: size})
		info.TotalBytes += size
	}
	si := sysinfo.Collect(a.cfg.Dir())
	info.FreeDiskMB = si.FreeDiskMB
	return info
}

// GetStoragePacks повертає розмір теки КОЖНОЇ встановленої збірки
// (вкладка «Пам'ять і кеш» → швидке видалення зайвого місця). Рахуємо
// лише ВСТАНОВЛЕНІ збірки (not-installed не мають теки установки, або
// вона порожня) — список береться з GetBuilds, тож кастомні теж входять.
func (a *App) GetStoragePacks() []model.StoragePackEntry {
	settings := a.cfg.GetSettings()
	// Спершу збираємо ВСТАНОВЛЕНІ збірки (для коректного done/total у
	// прогресі — раніше total рахував і невстановлені, і бар «доїжджав»
	// до 100% раніше, ніж вимірювались усі теки).
	type inst struct {
		id      string
		name    string
		iconURL string
		color   string
	}
	var list []inst
	for _, v := range a.GetBuilds() {
		if v.Installed {
			list = append(list, inst{id: v.Build.ID, name: v.Build.Name, iconURL: v.Build.IconURL, color: v.Build.Color})
		}
	}
	out := []model.StoragePackEntry{}
	for i, s := range list {
		dir := filepath.Join(settings.InstanceDir, s.id)
		// Прогрес обходу теки кожної збірки (та сама ідея, що в
		// GetStorageUsage): розміри великих збірок рахуються довго.
		a.emit("storage:packs-scan", map[string]any{"done": i, "total": len(list), "label": s.name})
		out = append(out, model.StoragePackEntry{
			ID:      s.id,
			Name:    s.name,
			Bytes:   dirSize(dir),
			IconURL: s.iconURL,
			Color:   s.color,
		})
	}
	return out
}

// ClearPackLogs очищає логи лаунчера (logsDir) та лог-файли і краш-репорти
// ВСІХ встановлених збірок (лог гри + crash-reports/). Світи, налаштування
// та моди НЕ чіпаються — видаляються лише лог-артефакти, що ростуть
// з кожним запуском. Сама тека лишається (щоб гра могла писати далі).
func (a *App) ClearPackLogs() error {
	settings := a.cfg.GetSettings()
	var firstErr error
	clearDir := func(dir string) {
		if dir == "" || firstErr != nil {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			if !os.IsNotExist(err) {
				firstErr = err
			}
			return
		}
		for _, e := range entries {
			if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	// Логи самого лаунчера (game-console-*.txt тощо).
	clearDir(a.cfg.LogsDir())
	// Логи/краш-репорти кожної встановленої збірки: <inst>/<id>/.minecraft/
	// logs + crash-reports (або <inst>/<id>/logs, якщо .minecraft немає).
	for _, v := range a.GetBuilds() {
		if !v.Installed {
			continue
		}
		root := filepath.Join(settings.InstanceDir, v.Build.ID)
		clearDir(filepath.Join(root, ".minecraft", "logs"))
		clearDir(filepath.Join(root, ".minecraft", "crash-reports"))
		// Старі версії/крайові випадки: логи прямо в теці збірки.
		clearDir(filepath.Join(root, "logs"))
	}
	if firstErr != nil {
		return firstErr
	}
	a.emit("storage:cleared-logs", nil)
	return nil
}

// ClearCache видаляє вміст теки кешу лаунчера (images, archives,
// mod-storage тощо). Саму теку залишає, щоб лаунчер міг працювати далі.
func (a *App) ClearCache() error {
	cacheDir := a.cfg.CacheDir()
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(cacheDir, e.Name())); err != nil {
			return err
		}
	}
	return nil
} // ── Очищення невикористовуваних Java та версій Minecraft ───────────────
// ТЗ: «видалити Java, яка не використовується — якщо жодна встановлена
// збірка не потребує Java 8, видаляємо її». Аналіз йде по ВСТАНОВЛЕНИХ
// збірках (їх MC-версія → потрібний Java-мажор); теки java-<major>/ у
// теці Java, які не потрібні жодній збірці, пропонуються до видалення.

// JavaMajorInfo — один встановлений мажор Java у теці лаунчера: номер,
// розмір на диску і чи потрібен він хоч одній встановленій збірці.
// Модалка аналізу показує список таких рядків (Java N · розмір · статус).
type JavaMajorInfo struct {
	Major int   `json:"major"`
	Bytes int64 `json:"bytes"`
	Used  bool  `json:"used"`
}

// UnusedJavaInfo — результат аналізу: які мажори Java встановлені в теці
// лаунчера (з розмірами), які з них НЕ потрібні жодній встановленій збірці,
// і загальний обсяг, який можна звільнити.
type UnusedJavaInfo struct {
	Installed  []JavaMajorInfo `json:"installed"`
	Unused     []int           `json:"unused"`
	TotalBytes int64           `json:"totalBytes"`
}

// AnalyzeUnusedJava сканує теки java-<major>/ у теці Java лаунчера і
// порівнює з потрібними мажорами всіх ВСТАНОВЛЕНИХ збірок. Кожна збірка
// вимагає java.RecommendedMajor(її MC-версії) — як при реальному запуску.
// Повертає список встановлених мажорів і підмножину невикористовуваних
// (кандидатів на видалення). Користувач підтверджує — CleanupUnusedJava
// видаляє саме їх.
func (a *App) AnalyzeUnusedJava() UnusedJavaInfo {
	info := UnusedJavaInfo{}
	// Які мажори фізично розкладені (підтеки java-<major>) — з розмірами.
	// Спершу збираємо список, потім рахуємо розміри ПОСЛІДОВНО й шлемо
	// подію прогресу (storage:java-scan) — UI показує, який мажор зараз
	// сканується і скільки лишилось, замість статичного спінера.
	type jdir struct {
		name  string
		major int
	}
	var dirs []jdir
	entries, err := os.ReadDir(a.cfg.JavaDir())
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			var major int
			if _, err := fmt.Sscanf(e.Name(), "java-%d", &major); err == nil {
				dirs = append(dirs, jdir{name: e.Name(), major: major})
			}
		}
	}
	for i, d := range dirs {
		a.emit("storage:java-scan", map[string]any{"done": i, "total": len(dirs), "major": d.major})
		info.Installed = append(info.Installed, JavaMajorInfo{
			Major: d.major,
			Bytes: dirSize(filepath.Join(a.cfg.JavaDir(), d.name)),
		})
	}
	// Потрібні мажори = RecommendedMajor MC-версії кожної ВСТАНОВЛЕНОЇ
	// збірки (та сама евристика, що в LaunchInstance).
	needed := map[int]bool{}
	for _, v := range a.GetBuilds() {
		if !v.Installed {
			continue
		}
		m := java.RecommendedMajor(v.Build.MCVersion)
		if m > 0 {
			needed[m] = true
		}
	}
	for i := range info.Installed {
		if needed[info.Installed[i].Major] {
			info.Installed[i].Used = true
		} else {
			info.Unused = append(info.Unused, info.Installed[i].Major)
			info.TotalBytes += info.Installed[i].Bytes
		}
	}
	return info
}

// CleanupUnusedJava видаляє підтеки java-<major> для мажорів зі списку
// unused (підтвердженого користувачем). Кожна Java живе у своїй теці,
// тому видалення однієї не чіпає інші; потрібні мажори перевстановляться
// автоматично при наступному запуску збірки (EnsureMajor).
func (a *App) CleanupUnusedJava(majors []int) error {
	for _, major := range majors {
		dir := filepath.Join(a.cfg.JavaDir(), fmt.Sprintf("java-%d", major))
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("видалення Java %d: %w", major, err)
		}
	}
	return nil
}

// dirSize підраховує сумарний розмір теки рекурсивно.
func dirSize(dir string) int64 {
	var total int64
	filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

// GetAccountHead повертає data URL 2D-голови акаунта (64x64 PNG), яка
// рендериться зі скіна з серверів Mojang і кешується у теку даних
// лаунчера. Для офлайн-акаунтів нік пробивається на Mojang (як у
// PrismLauncher): якщо гравець існує — показується його реальний скін
// (Алекс тощо), якщо ні — дефолтна голова Стіва/Алекса.
func (a *App) GetAccountHead(uuid, username string, licensed bool) string {
	if a.avatars == nil {
		return ""
	}
	return a.avatars.Head(uuid, username, licensed)
}

// StartMSLogin запускає локальний callback-сервер (як у старому лаунчері,
// порти 29111-29115) і повертає URL для відкриття у системному браузері.
// Код авторизації захопиться автоматично — копіювати його вручну не треба.
func (a *App) StartMSLogin() (string, error) {
	return a.msAuth.StartLogin()
}

// AwaitMSLogin блокується, поки браузер не перенаправить код на локальний
// сервер, потім обмінює його на токени, завершує авторизацію і зберігає
// акаунт. Викликається фронтендом після StartMSLogin.
func (a *App) AwaitMSLogin() error {
	account, err := a.msAuth.AwaitLogin()
	if err != nil {
		return err
	}
	return a.cfg.AddAccount(*account)
}

// CancelMSLogin перериває очікування AwaitMSLogin (кнопка «Скасувати»).
func (a *App) CancelMSLogin() {
	a.msAuth.CancelLogin()
}

// OpenExternal відкриває URL у системному браузері користувача
// (а не у вбудованому вікні WebView2).
func (a *App) OpenExternal(url string) {
	if app := application.Get(); app != nil {
		_ = app.Browser.OpenURL(url)
	}
}

// OpenPackFolder відкриває теку встановленої збірки у системному файловому
// менеджері (Провідник Windows / файловий менеджер Linux). Якщо теки ще
// немає (збірку ще жодного разу не качали) — повертає зрозумілу помилку,
// фронт покаже її тостом.
func (a *App) OpenPackFolder(packID string) error {
	if packID == "" {
		return nil
	}
	settings := a.cfg.GetSettings()
	// Теку збірки відкриваємо в корені installations/<packID>; якщо гра
	// вже встановлена — відкриваємо .minecraft (там реальні файли: моди,
	// конфіги, saves), як у старому лаунчері.
	packRoot := filepath.Join(settings.InstanceDir, packID)
	dir := filepath.Join(packRoot, ".minecraft")
	if _, err := os.Stat(dir); err != nil {
		dir = packRoot
	}
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("теку збірки ще не створено — спершу завантажте збірку")
	}
	if runtime.GOOS == "windows" {
		return exec.Command("explorer", dir).Start()
	}
	return exec.Command("xdg-open", dir).Start()
}

// ListInstanceWorlds повертає назви тек одиночних світів (saves/<world>) для
// збірки — використовується у списку вибору світу для автоприєднання
// («Гра» → «Автоприєднання» → «Одиночна гра»). Порожній список — не
// помилка: збірку могли ще не запускати або в ній ще немає жодного світу.
func (a *App) ListInstanceWorlds(packID string) []string {
	if packID == "" {
		return []string{}
	}
	settings := a.cfg.GetSettings()
	savesDir := filepath.Join(settings.InstanceDir, packID, ".minecraft", "saves")
	entries, err := os.ReadDir(savesDir)
	if err != nil {
		return []string{}
	}
	out := []string{}
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out
}

// ListPackServers читає servers.dat збірки (список серверів, збережених
// грою/користувачем) для вибору цілі автоприєднання по IP. Порожній
// результат — не помилка: servers.dat міг ще не створитись.
func (a *App) ListPackServers(packID string) []model.InstanceServer {
	if packID == "" {
		return nil
	}
	settings := a.cfg.GetSettings()
	path := filepath.Join(settings.InstanceDir, packID, ".minecraft", "servers.dat")
	entries, err := minecraft.ReadServersDat(path)
	if err != nil {
		return nil
	}
	out := make([]model.InstanceServer, 0, len(entries))
	for _, e := range entries {
		out = append(out, model.InstanceServer{
			ID: "srv-" + e.IP, Name: e.Name, Address: e.IP, Official: true,
		})
	}
	return out
}

// PingServer перевіряє доступність Minecraft-сервера (status ping).
func (a *App) PingServer(address string) minecraft.ServerPing {
	addr := strings.TrimSpace(address)
	if addr == "" {
		return minecraft.ServerPing{Online: false, Error: "порожня адреса"}
	}
	return minecraft.PingMinecraftServer(addr, 3*time.Second)
}

func (a *App) LoginPirate(username string) error {
	account, err := a.pirate.Login(username)
	if err != nil {
		return err
	}
	return a.cfg.AddAccount(*account)
}

func (a *App) RemoveAccount(accountID string) error { return a.cfg.RemoveAccount(accountID) }

// SetActiveAccount перемикає акаунт, який використовується для запуску
// гри (клік по акаунту у дропдауні сайдбара).
func (a *App) SetActiveAccount(accountID string) error  { return a.cfg.SetActiveAccountID(accountID) }
func (a *App) CheckUpdate() (*update.UpdateInfo, error) { return a.updater.Check() }
func (a *App) GetJavaVersions() []string                { return []string{"21", "17", "11", "8"} }
func (a *App) GetRAMOptions() []int                     { return []int{1024, 2048, 3072, 4096, 6144, 8192, 12288, 16384} }

func (a *App) IsFirstRun() bool { return a.cfg.IsFirstRun() }

func (a *App) CompleteSetup(s model.Settings) error { return a.cfg.SetupComplete(s) }

// GetWizardDefaults повертає дефолтні значення майстра першого запуску:
// тека збірок = папка даних/installations (завжди вказана за замовчуванням),
// Java = власна вбудована тека лаунчера папка даних/java (лаунчер сам
// поставить її, якщо відсутня), RAM — половина системної пам'яті
// (в межах 2–8 ГБ), акцент — помаранчевий. Фронтенд використовує це для
// першого кроку майстра, якщо збереженого прогресу ще немає.
func (a *App) GetWizardDefaults() model.SetupProgress {
	si := sysinfo.Collect(a.cfg.Dir())
	ram := 4096
	if si.TotalRAMMB > 0 {
		half := int(si.TotalRAMMB / 2 / 1024) // половина в ГБ
		switch {
		case half < 2:
			half = 2
		case half > 8:
			half = 8
		}
		ram = half * 1024
	}
	return model.SetupProgress{
		Step:        1,
		InstanceDir: a.cfg.InstancesDir(),
		JavaPath:    a.cfg.JavaDir(),
		Language:    "uk",
		Accent:      "orange",
		MaxRAM:      ram,
		JavaArgs:    "",
	}
} // GetSetupProgress повертає збережений крок майстра першого запуску
// (див. model.SetupProgress). Фронтенд викликає це одразу при
// відкритті вікна, ДО показу кроку 1 — якщо повернувся Step > 0,
// майстер відновлюється з цього кроку і вже введеними даними, замість
// того щоб почати спочатку.
func (a *App) GetSetupProgress() model.SetupProgress {
	p, _ := a.cfg.LoadSetupProgress()
	return p
}

// SaveSetupProgress фронтенд викликає при КОЖНІЙ зміні кроку (Далі/
// Назад) та при зміні полів wiz.instanceDir/javaPath/language — щоб
// закриття лаунчера всередині майстра не скидало прогрес на крок 1.
func (a *App) SaveSetupProgress(p model.SetupProgress) error {
	return a.cfg.SaveSetupProgress(p)
}

// DetectJava шукає придатну Java і повертає ПАПКУ Java (не шлях до файлу:
// користувач бачить теку, де зберігаються версії). Пріоритет:
// (1) власна вбудована Java лаунчера (~/.shaurm/java) — саме вона
// використовується за замовчуванням; (2) системні шляхи; (3) інакше
// повертає теку вбудованої Java — лаунчер сам її встановить.
func (a *App) DetectJava() string {
	// 1. Вбудована Java лаунчера (головний пріоритет).
	bundled := a.cfg.JavaDir()
	if _, err := os.Stat(filepath.Join(bundled, "bin", "javaw.exe")); err == nil {
		return bundled
	}
	// 2. Системні шляхи.
	candidates := []string{
		`C:\Program Files\Java\jdk-21\bin\javaw.exe`,
		`C:\Program Files\Java\jdk-17\bin\javaw.exe`,
		`C:\Program Files\Java\jdk-8\bin\javaw.exe`,
		`C:\Program Files\Eclipse Adoptium\jdk-21\bin\javaw.exe`,
		`C:\Program Files\Eclipse Adoptium\jdk-17\bin\javaw.exe`,
		`C:\Program Files\Microsoft\jdk-21\bin\javaw.exe`,
		`C:\Program Files\Microsoft\jdk-17\bin\javaw.exe`,
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return filepath.Dir(filepath.Dir(p))
		}
	}
	// 3. Дефолт — вбудована Java (лаунчер встановить її сам).
	return bundled
}

func (a *App) BrowseFolder() (string, error) {
	if app := application.Get(); app != nil {
		return app.Dialog.OpenFile().
			CanChooseDirectories(true).
			SetTitle("Виберіть теку для збірок").
			PromptForSingleSelection()
	}
	return "", fmt.Errorf("no context")
}

// BrowseFontFile відкриває діалог вибору кастомного шрифту (.ttf/.otf).
// Повертає повний шлях до файлу — зберігається в Settings.FontPath.
func (a *App) BrowseFontFile() (string, error) {
	if app := application.Get(); app != nil {
		return app.Dialog.OpenFile().
			SetTitle("Виберіть файл шрифту").
			AddFilter("Шрифти (*.ttf;*.otf)", "*.ttf;*.otf").
			PromptForSingleSelection()
	}
	return "", fmt.Errorf("no context")
}

// LoadFontFile повертає файл шрифту як data: URL (base64) для @font-face.
// WebView не має доступу до довільних шляхів ФС, тому TTF передається
// через бекенд. Повертає порожній рядок, якщо файл нечитабельний.
func (a *App) LoadFontFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	// MIME за розширенням: .otf і .ttf. TTF — application/font-sfnt,
	// OTF — application/vnd.ms-opentype. Браузери приймають обидва.
	mime := "application/font-sfnt"
	switch strings.ToLower(filepath.Ext(path)) {
	case ".otf":
		mime = "application/vnd.ms-opentype"
	case ".woff", ".woff2":
		mime = "font/woff2"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// ── Гардероб ──────────────────────────────────────────────────────────
//
// Перенесено з WardrobeScreen.java: локальні пресети (скін + опційний
// плащ) + застосування на ліцензійний акаунт через Mojang API, плюс
// нова фіча "скопіювати скін гравця за ніком".

// refreshAccountForWardrobe — TokenRefresher для internal/wardrobe: той
// самий патерн освіження токена, що й перед запуском гри в LaunchInstance,
// винесений в окремий метод, щоб не дублювати логіку і не тягнути пакет
// wardrobe на internal/auth напряму (уникнення циклічного імпорту).
func (a *App) refreshAccountForWardrobe(accountID string) (model.Account, error) {
	accounts := a.cfg.GetAccounts()
	var account model.Account
	found := false
	for _, acc := range accounts {
		if acc.ID == accountID {
			account = acc
			found = true
			break
		}
	}
	if !found {
		return model.Account{}, fmt.Errorf("wardrobe: account not found")
	}

	if account.Type == "microsoft" {
		needRefresh := account.AccessToken == "" ||
			(account.ExpiresAt > 0 && time.Now().UnixMilli() > account.ExpiresAt-5*60*1000)
		if needRefresh && account.RefreshToken != "" {
			refreshed, err := a.msAuth.RefreshTokens(account.RefreshToken)
			if err != nil {
				return model.Account{}, fmt.Errorf("wardrobe: token refresh: %w", err)
			}
			account = *refreshed
			_ = a.cfg.UpdateAccount(account)
		}
	}
	return account, nil
}

// GetWardrobePresets повертає пресети гардеробу конкретного акаунта.
// Пресети зберігаються окремо для кожного ліцензійного акаунта — при
// зміні акаунта гардероб показує саме пресети цього акаунта.
func (a *App) GetWardrobePresets(accountID string) []model.SkinPreset {
	return a.wardrobe.ListPresets(accountID)
}

// DeleteWardrobePreset видаляє пресет за ID в межах акаунта.
func (a *App) DeleteWardrobePreset(accountID, presetID string) error {
	return a.wardrobe.DeletePreset(accountID, presetID)
}

// UpdateWardrobePreset оновлює існуючий пресет: назва, тип рук і плащ
// (кнопка «Зберегти» у редакторі пресету). Плащ за URL завантажується в
// локальний кеш і прив'язується до пресету, щоб картка одразу оновилась.
func (a *App) UpdateWardrobePreset(accountID, presetID, name string, slim bool, capeURL string) (model.SkinPreset, error) {
	return a.wardrobe.UpdatePreset(accountID, presetID, name, slim, capeURL)
}

// SaveWardrobeLocalSkin створює пресет із власноруч завантаженого PNG
// (кнопка "Додати скін" / drag&drop у діалозі редактора пресету).
// skinPNGBase64 — вміст файлу, закодований у base64 (без префіксу data:).
func (a *App) SaveWardrobeLocalSkin(accountID, name, skinPNGBase64 string, slim bool) (model.SkinPreset, error) {
	data, err := base64.StdEncoding.DecodeString(skinPNGBase64)
	if err != nil {
		return model.SkinPreset{}, fmt.Errorf("wardrobe: invalid skin data: %w", err)
	}
	return a.wardrobe.SaveLocalSkin(accountID, name, data, slim)
}

// LookupWardrobePlayer шукає гравця за ніком на Mojang для превʼю в
// модалці "Скопіювати скін гравця" (викликається з debounce на фронтенді,
// нічого ще не зберігає).
func (a *App) LookupWardrobePlayer(nickname string) (*model.PlayerProfile, error) {
	return a.wardrobe.LookupPlayer(nickname)
}

// CopyWardrobePlayerSkin завершує фічу "Скопіювати скін гравця": зберігає
// знайдений профіль як новий пресет акаунта (з плащем, якщо includeCape=true).
func (a *App) CopyWardrobePlayerSkin(accountID string, profile model.PlayerProfile, includeCape bool) (model.SkinPreset, error) {
	return a.wardrobe.CopyPlayerSkin(accountID, profile, includeCape)
}

// ApplyWardrobePreset вивантажує скін пресету на вказаний ліцензійний
// акаунт (Mojang API), і намагається активувати плащ пресету, якщо він
// вже виданий акаунту. Після успіху скидає кеш аватарки акаунта і шле
// подію avatar:changed — картка профілю гравця одразу показує новий скін
// (без цього голова лишалась би старою до закінчення TTL кешу).
func (a *App) ApplyWardrobePreset(accountID, presetID string) error {
	if err := a.wardrobe.ApplyPreset(accountID, presetID); err != nil {
		return err
	}
	// Скін на Mojang змінився — знаходимо UUID акаунта, чия аватарка
	// кешується, і скидаємо її, щоб наступний GetAccountHead перерендерив
	// голову з нового скіна.
	uuid := ""
	for _, acc := range a.cfg.GetAccounts() {
		if acc.ID == accountID {
			uuid = acc.UUID
			break
		}
	}
	if a.avatars != nil && uuid != "" {
		a.avatars.Invalidate(uuid)
	}
	a.emit("avatar:changed", accountID)
	return nil
}

// GetWardrobeOwnedCapes повертає плащі, доступні вказаному акаунту.
func (a *App) GetWardrobeOwnedCapes(accountID string) ([]model.CapeInfo, error) {
	return a.wardrobe.FetchOwnedCapes(accountID)
}

// SetWardrobeActiveCape вмикає плащ за ID на вказаному акаунті (порожній
// ID знімає плащ).
func (a *App) SetWardrobeActiveCape(accountID, capeID string) error {
	return a.wardrobe.SetActiveCape(accountID, capeID)
}

// GetWardrobeActivePresetID шукає серед збережених пресетів акаунта той,
// чий вигляд (скін + плащ, побайтово) вже фактично застосований на Mojang,
// і повертає його ID (порожній рядок — збігів нема). Фронтенд викликає це
// при відкритті гардеробу, щоб автоматично виділити пресет як вибраний
// замість першого в списку.
func (a *App) GetWardrobeActivePresetID(accountID string) (string, error) {
	return a.wardrobe.ActiveMatchingPresetID(accountID)
}

// GetWardrobeActiveLook повертає поточний вигляд акаунта на Mojang
// (активний скін + плащ) як data URL. Використовується гардеробом, щоб
// показати в 3D-переглядачі скін, який стоїть на акаунті, коли жоден
// пресет не збігається (як це робив старий лаунчер).
func (a *App) GetWardrobeActiveLook(accountID string) (*model.ActiveLook, error) {
	return a.wardrobe.ActiveLook(accountID)
}

// SaveWardrobeActiveLookPreset створює пресет з поточного вигляду акаунта —
// кнопка "Редагувати скін", коли поточний скін/плащ не мають збереженого
// пресету: пропонує створити такий пресет, а не редагувати чужий.
func (a *App) SaveWardrobeActiveLookPreset(accountID, name string) (model.SkinPreset, error) {
	return a.wardrobe.SaveActiveLookPreset(accountID, name)
}

// GetWardrobeTextureDataURL читає локально збережену текстуру пресету
// (скін або плащ) і повертає data URL для рендеру у фронтенді.
func (a *App) GetWardrobeTextureDataURL(path string) (string, error) {
	return a.wardrobe.TextureDataURL(path)
}

// GetWardrobeCapeDataURL завантажує плащ за Mojang-CDN URL і повертає
// data URL для превʼю плаща на 3D-моделі в редакторі пресету.
func (a *App) GetWardrobeCapeDataURL(capeURL string) (string, error) {
	return a.wardrobe.CapeDataURL(capeURL)
}

// GetWardrobeRemoteSkinDataURL завантажує довільну текстуру скіна за
// Mojang-CDN URL (skinUrl іншого гравця, знайденого через LookupWardrobePlayer)
// і повертає data URL для 3D-переглядача в модалці "Скопіювати скін гравця".
func (a *App) GetWardrobeRemoteSkinDataURL(skinURL string) (string, error) {
	return a.wardrobe.RemoteSkinDataURL(skinURL)
}
