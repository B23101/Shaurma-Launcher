package main

import (
	"shaurma-launcher-backend/internal/jvmargs"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"shaurma-launcher-backend/internal/crashguard"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"shaurma-launcher-backend/internal/ai"
	"shaurma-launcher-backend/internal/api"
	"shaurma-launcher-backend/internal/auth"
	"shaurma-launcher-backend/internal/avatar"
	"shaurma-launcher-backend/internal/builds"
	"shaurma-launcher-backend/internal/config"
	"shaurma-launcher-backend/internal/console"
	"shaurma-launcher-backend/internal/custompack"
	"shaurma-launcher-backend/internal/discordrpc"
	"shaurma-launcher-backend/internal/download"
	"shaurma-launcher-backend/internal/gallery"
	"shaurma-launcher-backend/internal/java"
	"shaurma-launcher-backend/internal/launchlog"
	"shaurma-launcher-backend/internal/minecraft"
	"shaurma-launcher-backend/internal/model"
	"shaurma-launcher-backend/internal/mods"
	"shaurma-launcher-backend/internal/neterr"
	"shaurma-launcher-backend/internal/p2p"
	"shaurma-launcher-backend/internal/servers"
	"shaurma-launcher-backend/internal/sessions"
	"shaurma-launcher-backend/internal/store"
	syncengine "shaurma-launcher-backend/internal/sync"
	"shaurma-launcher-backend/internal/sysinfo"
	"shaurma-launcher-backend/internal/update"
	"shaurma-launcher-backend/internal/wardrobe"
	"shaurma-launcher-backend/internal/xbox"
)

// discordAppID — Application ID застосунку Shaurma Launcher у Discord
// Developer Portal (https://discord.com/developers/applications). Rich
// Presence показується у профілях лише для зареєстрованого застосунку —
// підстав свій Client ID. Handshake з Discord працює з будь-яким числовим
// ID; публікація активності — лише з валідним ID твого застосунку.
const discordAppID = "1514026811733053500"

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
	// GetPackWhitelist повертає ПОВНІ записи білого списку (глобальний +
	// per-pack, дедупльовані за ModID) для показу картки мода в UI
	// (іконка/назва/посилання на сторінку Modrinth/CurseForge) — на
	// відміну від internal/api.GetEffectiveWhitelist, який на гарячому
	// шляху перевірки цілісності файлів свідомо повертає лише голий
	// map[string]bool без метаданих.
	GetPackWhitelist(packID string) ([]syncengine.WhitelistEntry, error)
	// GetPackWiki повертає вікі збірки (packs/<id>/wiki.json) з CDN Шаурми.
	// nil, nil — вікі ще не створено (404): фронтенд просто не показує
	// вкладку «Вікі» для такої збірки.
	GetPackWiki(packID string) (*syncengine.PackWikiDoc, error)
	// InvalidatePackCache скидає TTL-кеш packs-index.json негайно. Викликає
	// AccountsChanged (див. нижче) після входу/виходу/перемикання акаунта —
	// щоб приватна збірка, доступ до якої щойно з'явився чи зник, оновилась
	// у списку без перезапуску лаунчера, а не чекала packsIndexTTL.
	InvalidatePackCache()
}

type App struct {
	ctx      context.Context
	cfg      *config.Manager
	dl       *download.Engine
	modMgr   *mods.Manager
	msAuth   *auth.Authenticator
	pirate   *auth.PirateService
	modrinth *api.ModrinthClient
	// xbox — клієнт соціальних Xbox Live API (People Hub / presence /
	// privacy). Розділ «Друзі» працює лише через акаунт Shaurma; xbox лишився для зворотної сумісності старого
	// коду GetXboxFriends/AddXboxFriend/GetXboxPrivacy.
	xbox     *xbox.Client
	updater  *update.Checker
	inst     *minecraft.Installer
	javaInst *java.Installer
	srvAPI   *servers.API
	// Віддалене керування серверами друзів (remote_control_*.go).
	remoteInit sync.Once
	remoteRT   *remoteRT
	p2pAPI   *p2p.API
	// activeInstallID — instanceID збірки, чиє встановлення компонентів
	// (EnsureLoader/EnsureVersion/Java) зараз реально виконує a.inst/
	// a.javaInst. Обидва інсталери ГЛОБАЛЬНІ на весь App (не по-збіркам),
	// тому без цього поля CancelGameInstall(), викликаний з картки
	// збірки A, фактично скасовував би встановлення збірки B, якщо саме
	// вона реально йде в цей момент (LaunchInstance дозволяє паралельний
	// запуск різних збірок на одному акаунті — див. коментар там).
	// sync/atomic.Value замість mutex+string: CancelGameInstall читає це
	// поле з іншої горутини (клік по UI), не варто ганяти мьютекс заради
	// одного порівняння рядків.
	activeInstallID atomic.Value // string
	rpc             *discordrpc.Manager
	avatars         *avatar.Service
	wardrobe        *wardrobe.Service
	// skinRestoreTracker — живий стан повернення скіна збірки (restoring/
	// failed/done) для панелі статусу в розділі «Гра» (див. build_skin.go).
	skinRestoreTracker *skinRestoreTracker
	packs              PackProvider

	// customPacks — реєстр локальних кастомних збірок (створених вручну
	// або імпортованих з .mrpack/.zip), окремий від Shaurma-каталогу
	// (packs). versionsProv — легкий кеш-клієнт списків версій
	// Minecraft/лоадерів для форми "Нова збірка → Вручну".
	customPacks *custompack.Store

	// gallery — метадані розділу «Галерея» (підписи/описи скріншотів),
	// screenshots.go. Самі файли скріншотів читаються напряму з диска.
	gallery      *gallery.Store
	versionsProv *custompack.VersionsProvider
	// customImportMu/customImportCancel — керування активним імпортом
	// модпаку (Скасувати на прев'ю-екрані вкладки "Імпортувати").
	customImportMu     sync.Mutex
	customImportCancel map[string]context.CancelFunc
	// customImportPaused — packID, чий активний імпорт був призупинений
	// (PauseModpackImport), а НЕ скасований. Result-горутина імпорту
	// перевіряє його як «останнє слово»: пізній тік прогресу (що встиг
	// пройти ctx-перевірку до паузи) не має затерти стан «Пауза» назад
	// у running — фінальний emitCustomPaused перемогає.
	customImportPaused map[string]bool

	// blockedWaiters — очікування рішення користувача по заблокованих
	// модах (імпорт CurseForge zip, діалог як у Prism). Ключ — packID;
	// імпорт-горутина спить на каналі, поки фронтенд не відповість
	// RespondBlockedMods, або поки не скасується контекст імпорту.
	blockedWaiters map[string]chan custompack.BlockedModsDecision

	// blockedCheckedMods — назви файлів модів, позначених галочками
	// «скачано вручну» в діалозі заблокованих модів. Ключ — packID;
	// значення — список назв файлів. Використовується при створенні
	// збірки: позначені моди отримують Downloaded=true.
	blockedCheckedMods map[string][]string

	// customImportStateAt — останній час запису персистентного стану імпорту
	// (Pack.ImportState) по packID. Троттлінг запису у custom-packs.json:
	// прогрес іде тіками кожні ~150мс, писати JSON на кожен тік марно —
	// пишемо не частіше ніж раз на customImportStateSaveInterval.
	customImportStateAt map[string]time.Time
	customImportStateMu sync.Mutex

	// Авто-збірки «Vanilla — актуальна версія» / «Snapshot — нова версія»
	// (autopacks.go): стан у .autopacks.json. autoRefreshBusy — packID, для
	// якого зараз іде фонове встановлення компонентів нової версії
	// (RefreshAutoPackFiles) — поки запис тут, запуск збірки заблокований.
	autoPacksMu     sync.Mutex
	autoPacksData   *autoPacksFile
	autoRefreshMu   sync.Mutex
	autoRefreshBusy map[string]bool
	autoCheckMu     sync.Mutex
	// autoCheckAt — час останнього УСПІШНОГО повного циклу перевірки
	// авто-збірок (від нього рахується autoCheckTTL). autoCheckAttemptAt —
	// час останньої спроби ВЗАГАЛІ, успішної чи ні: потрібен лише як
	// коротка пауза між спробами, щоб офлайн-лаунчер не довбав
	// launchermeta.mojang.com на кожен GetAutoPacks.
	autoCheckAt        time.Time
	autoCheckAttemptAt time.Time

	// migrateAnalyzeMu/migrateAnalyzeCancel — скасування АНАЛІЗУ міграції
	// модів по ключу targetPackID ('' для міні-картки створення збірки).
	// Кнопка «Скасувати» в діалозі міграції зупиняє розбір сотень модів
	// без закриття діалогу: AnalyzeModMigration повертає частковий звіт.
	migrateAnalyzeMu     sync.Mutex
	migrateAnalyzeCancel map[string]context.CancelFunc

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
	repair        repairRuntime // метрики ресурсів гри (auto_repair_api.go)
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

	// runningGamesMu — захищає читання/запис running-games.json. Кілька
	// паралельних сесій (різні збірки і/або акаунти) можуть завершуватись і
	// стартувати одночасно, кожна пише у той самий файл — без м'ютексу це
	// був би classic read-modify-write data race і втрата записів.
	runningGamesMu sync.Mutex

	// syncFailures — packID, для яких останній DownloadPack завершився ДО
	// реєстрації runner-а (health-check/маніфест: немає мережі або сервер
	// недоступний). waitForSyncStartThenFinish перевіряє цей прапорець у
	// своєму циклі й бейлить ОДРАЗУ, як тільки він з'явився — щоб запуск
	// гри не зависав на повні 20с очікування runner-а, який ніколи не
	// з'явиться (користувач: «немає бути зависань»).
	syncFailures   map[string]bool
	syncFailuresMu sync.Mutex

	// launchServerOverride — тимчасова адреса сервера для "Грати" з рядка
	// конкретного сервера в розділі "Сервери" (App.LaunchInstanceOnServer):
	// перекриває icfg.AutoJoinServer РІВНО на один запуск, нічого не
	// зберігаючи на диск — звичайне автоприєднання зі сторінки "Гра" після
	// цього лишається незмінним.
	launchServerOverride   map[string]string
	launchServerOverrideMu sync.Mutex

	// launchAccountOverride — тимчасовий акаунт для запуску з ЯРЛИКА збірки
	// (персональна опція ярлика, як у Prism: --account <id> у CLI-аргументах
	// .lnk): перекриває активний акаунт/прив'язку збірки РІВНО на один
	// запуск, нічого не зберігаючи на диск. Решта налаштувань запуску — як
	// при звичайному запуску.
	launchAccountOverride   map[string]string
	launchAccountOverrideMu sync.Mutex

	// launchClickAccount — акаунт, який фронтенд бачив активним у МОМЕНТ
	// КЛІКУ «Грати» (App.svelte doLaunch передає activeAccount().id у
	// LaunchInstance). Виправляє race condition: LaunchInstance раніше
	// читав a.cfg.ActiveAccount() у момент ВИКОНАННЯ на бекенді, а не в
	// момент кліку — якщо гравець паралельно перемикав акаунт (напр., поки
	// на екрані йшла тривала перевірка/встановлення лоадерів попереднього
	// запуску іншої збірки), гра стартувала на акаунті, обраному ПІЗНІШЕ,
	// а не на тому, що був активний, коли гравець натиснув кнопку. Нижчий
	// пріоритет, ніж launchAccountOverride (ярлик) і AccountIDOverride
	// (прив'язка збірки) — лише підміняє МОМЕНТ читання активного акаунта,
	// не сам факт "хто має пріоритет", якщо для збірки явно налаштовано
	// інше.
	launchClickAccount   map[string]string
	launchClickAccountMu sync.Mutex

	// Розумна консоль: кільцевий буфер логу гри (записується ЗАВЖДИ, поки
	// гра запущена — незалежно від того, відкрите вікно консолі чи ні) та
	// клієнт Gemini для AI-аналізу крашів (ліниво створюється на перший
	// виклик).
	consoleBuf *console.Buffer
	ai         *ai.Client
	// aiKey — Gemini ключ, з яким створено ai (щоб пересоздати клієнта
	// при зміні ключа в Налаштуваннях → Консоль).
	aiKey string
	// consoleBatch — батчинг console:line/buildConsole:line подій (див.
	// console_batch.go): захист від зависання/росту пам'яті при великому
	// потоці логів гри.
	consoleBatch consoleBatchState

	// modsBatch — батчинг mods:update-progress/mods:update-one подій
	// (див. mods_batch.go): той самий захист, що consoleBatch вище, але
	// для сплеску подій при перевірці оновлень модів (сотні модів у
	// синхронному циклі CheckModUpdates).
	modsBatch modsBatchState

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

	// lastExitWasUserStop — той самий факт, що й wasUserStop у
	// model.BuildExitEvent, але НЕ споживається (на відміну від
	// stoppedByUser.Swap, який consumed рівно раз при виході): це
	// "останнє відоме" значення для GetLastExitWasUserStop(), яке читає
	// окреме плаваюче вікно консолі (packID=="", слідує за останньою
	// грою) в обробнику безадресної події game:exit — вона, на відміну
	// від build:exit, не несе buildId/wasUserStop у собі (див. коментар
	// біля a.emit("game:exit", ...) — розширювати формат цієї події
	// ризиковано, її вже читають кілька місць як просте число).
	lastExitWasUserStop atomic.Bool

	// modUpdateCache — кеш результатів останньої МЕРЕЖЕВОЇ перевірки
	// оновлень модів, окремо на кожну (packID, mcVersion, loader) — див.
	// ScanModsWithCachedUpdates/RefreshModUpdates/invalidateModUpdateCache
	// у mods_api.go. Ключ: modUpdateCacheKey(...). Мета — не бити мережу
	// (Modrinth/CurseForge) при кожному відкритті сторінки модів, а лише
	// коли користувач явно натиснув «Перевірити оновлення» чи вперше
	// відкрив сторінку для цієї пари версія/лоадер.
	// modUpdateCacheEntry тепер містить час запису (at) — pruneModsCaches
	// видаляє лише ЗАСТАРІЛІ записи (а не весь кеш разом).
	modUpdateCache   map[string]modUpdateCacheEntry
	modUpdateCacheMu sync.Mutex

	// modResolveCache — ГЛОБАЛЬНИЙ кеш результату мережевого резолву мода
	// за sha1 файлу, СПІЛЬНИЙ для всіх збірок (на відміну від
	// modUpdateCache вище, який прив'язаний до конкретної packID). Раніше
	// той самий мод (напр. Create тієї самої версії), встановлений у 5
	// різних збірках, перевірявся в мережі 5 разів окремо — Modrinth/
	// CurseForge били запитами даремно. Тепер: перша збірка, що перевіряє
	// мод з певним sha1, кладе результат сюди; всі наступні збірки (і
	// повторні перевірки цієї ж збірки) з тим самим sha1 читають з кешу
	// без походу в мережу. Ключ — sha1 файлу (порожній sha1 не кешується,
	// бо не унікальний). TTL — щоб з часом підхопити нові версії модів
	// на Modrinth/CurseForge, а не кешувати "немає оновлення" назавжди.
	modResolveCache   map[string]modResolveEntry
	modResolveCacheMu sync.Mutex

	// onlineLoaderCache — ГЛОБАЛЬНИЙ кеш (за sha1 файлу, спільний для всіх
	// збірок) результату онлайн-перевірки лоадерів мода через Modrinth/
	// CurseForge (verifyIncompatibleModsOnline / onlineLoadersForJar).
	// Питається лише для модів, яких локальний парсер metadata вже
	// позначив «несумісний» — таких зазвичай одиниці, тож окремий кеш
	// (замість modResolveCache, який тримає зовсім інші дані — оновлення)
	// не потребує великого TTL-менеджменту: pruneModsCaches чистить його
	// тим самим проходом, що й решту кешів модів.
	onlineLoaderCache   map[string]onlineLoaderCacheEntry
	onlineLoaderCacheMu sync.Mutex

	// suggestResolveCache — кеш резолву depID → SuggestedMod для блоку
	// «Рекомендовані моди» (вкладка Контент). Ключ — depID (fabric mod
	// ID, НЕ sha1), значення nil = мод не знайшовся на Modrinth (теж
	// кешується, щоб не бити мережу повторно кожне відкриття вкладки).
	suggestResolveCache   map[string]*suggestResolveEntry
	suggestResolveCacheMu sync.Mutex
}

// suggestResolveEntry — закешований результат резолву однієї
// рекомендації (nil-значення саме по собі, а не відсутність ключа,
// відрізняє "перевіряли, нічого не знайшли" від "ще не перевіряли").
type suggestResolveEntry struct {
	mod *model.SuggestedMod
	at  time.Time
}

const suggestResolveCacheTTL = 30 * time.Minute

func (a *App) getSuggestResolveCache(depID string) (*model.SuggestedMod, bool) {
	a.suggestResolveCacheMu.Lock()
	defer a.suggestResolveCacheMu.Unlock()
	e, ok := a.suggestResolveCache[depID]
	if !ok || time.Since(e.at) > suggestResolveCacheTTL {
		return nil, false
	}
	return e.mod, true
}

func (a *App) setSuggestResolveCache(depID string, mod *model.SuggestedMod) {
	a.suggestResolveCacheMu.Lock()
	defer a.suggestResolveCacheMu.Unlock()
	if a.suggestResolveCache == nil {
		a.suggestResolveCache = map[string]*suggestResolveEntry{}
	}
	a.suggestResolveCache[depID] = &suggestResolveEntry{mod: mod, at: time.Now()}
}

// modUpdateCacheEntry — один запис кешу перевірки оновлень для конкретної
// (packID, mcVersion, loader) пари. Зберігає час запису (at) для точкового
// TTL-pruning (а не повного скидання) і карту модів за FileName для
// часткового оновлення (smart invalidation).
type modUpdateCacheEntry struct {
	mods   []model.ModEntry
	byFile map[string]int // FileName → індекс у mods (для O(1) патчінгу)
	at     time.Time
}

// modResolveEntry — закешований результат мережевого резолву одного
// мода (за sha1), незалежний від того, в якій збірці його перевіряли.
type modResolveEntry struct {
	source        string
	projectID     string
	projectSlug   string
	iconURL       string
	hasUpdate     bool
	latestVersion string
	latestURL     string
	at            time.Time
}

const modResolveCacheTTL = 30 * time.Minute

// newUpdateChecker створює update.Checker і одразу виставляє канал
// (stable/beta) з наявного settings.json — щоб і фонова checkAutoUpdate(),
// і перша ручна перевірка в налаштуваннях одразу дивились у канал, який
// користувач обрав раніше, а не в дефолтний "stable" до першого SaveSettings.
//
// ВИПРАВЛЕННЯ (Linux показував версію 2.0.0 у «Про лаунчер», хоча
// апдейтер бачив актуальну): тут раніше стояло readLauncherVersion(cfg.Dir()).
// cfg.Dir() — це тека ДАНИХ лаунчера (%APPDATA%\.shaurm / ~/.shaurm), а
// launcher-version.json пише updater у теку ВСТАНОВЛЕННЯ (installDir —
// та сама, що shell передає в backend_init, і ту саму, з коментаря
// readLauncherVersion). На Windows ці дві теки в частині сетапів
// випадково збігались (installDir поруч з APPDATA-профілем однакового
// користувача), тому баг там не проявлявся; на Linux (AppImage —
// installDir у XDG data dir, а cfg.Dir() лишається ~/.shaurm) шлях був
// просто неправильний — файл там ніколи не лежав, і readLauncherVersion
// мовчки відкочувалась на жорстко вшите "2.0.0". cfg.InstallDir() віддає
// саме installDir.
func newUpdateChecker(cfg *config.Manager) *update.Checker {
	c := update.NewChecker(readLauncherVersion(cfg.InstallDir()))
	c.SetChannel(cfg.GetSettings().UpdateChannel)
	return c
}

func NewApp() *App {
	// installDir — РЕАЛЬНА тека встановлення лаунчера (куди користувач
	// поставив exe; shell передає її в backend_init). Конфіг і база
	// (launcher-location.json, shaurma.db) живуть біля exe, а не в
	// захардкоженому %LOCALAPPDATA%\ShaurmLauncher — програма розуміє свою
	// справжню теку, хоч би куди її встановили.
	cfg := config.NewManager(backendInitConfig.InstallDir)
	modMgr := mods.NewManager()
	// Персистентний кеш SHA1 (internal/mods/sha1cache_persist.go) мусить
	// лежати в СЛУЖБОВІЙ теці лаунчера (CacheDir), а не в mods/ чи
	// .minecraft самої збірки — та тека належить гравцю/маніфесту.
	modMgr.SetCacheDir(cfg.CacheDir())
	a := &App{
		cfg:      cfg,
		dl:       download.NewEngine(),
		modMgr:   modMgr,
		msAuth:   auth.NewAuthenticator(),
		pirate:   auth.NewPirateService(),
		xbox:     xbox.NewClient(),
		modrinth: api.NewModrinthClient(),
		updater:  newUpdateChecker(cfg),
		inst:     minecraft.NewInstaller(filepath.Join(cfg.Dir(), "minecraft")),
		javaInst: java.NewInstaller(cfg.JavaDir()),
		avatars:  avatar.NewService(filepath.Join(cfg.CacheDir(), "avatars")),
		// Реєстр встановлених збірок (builds_registry у SQLite) та менеджер
		// сесій. Store може бути ще не відкритий на цьому етапі — реальний
		// бекенд перепризначиться в rewireComponents після cfg.Load().
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
		consoleBuf:          console.NewBuffer(3000),
		customPacks:         custompack.NewStore(cfg.InstancesDir()),
		gallery:             gallery.NewStore(custompack.NewStore(cfg.InstancesDir())),
		versionsProv:        custompack.NewVersionsProvider(),
		customImportCancel:  map[string]context.CancelFunc{},
		customImportPaused:  map[string]bool{},
		blockedWaiters:      map[string]chan custompack.BlockedModsDecision{},
		customImportStateAt: map[string]time.Time{},
		modUpdateCache:      map[string]modUpdateCacheEntry{},
		modResolveCache:     map[string]modResolveEntry{},
		suggestResolveCache: map[string]*suggestResolveEntry{},
		autoRefreshBusy:     map[string]bool{},
	}
	a.packs = newPackProvider(a)
	// Гардероб: пресети в SQLite (wardrobe_presets), PNG текстури скінів —
	// у .shaurm/skins (постійна тека даних, а НЕ cache: скіни пресетів —
	// дані користувача, які не можна чистити як кеш, див. maintenance).
	a.wardrobe = wardrobe.NewService(filepath.Join(cfg.Dir(), "skins"), a.refreshAccountForWardrobe)
	a.skinRestoreTracker = newSkinRestoreTracker()
	// Іконки Шаурма-акаунтів (аватарка в сайдбарі): у Shaurma-редакції
	// беремо скін із власного сервера скінів замість Mojang (no-op у
	// base — див. offline_account_base.go). Виклик тут, а не одразу в
	// avatar.NewService вище, бо резолвер — замикання на методи App
	// (a.skinRequest), яких до завершення &App{...} ще нема.
	a.wireShaurmaAvatarResolver()

	// Оптимізація запуску (план A): коли гра переходить у стан «запущена»
	// або «зупинена», реагуємо зміною пріоритету процесу лаунчера. Гри це
	// не торкається (JVM — окремий процес на NORMAL), лише сам лаунчер і
	// його WebView2-фронтенд. Callback синхронний і безпечний — не блокує
	// реєстр сесій (див. sessions.notifyRunning).
	a.sessionsM.OnRunningStateChange(func(running bool) {
		if running {
			guardGamePriority()
			// Гра щойно стартувала: лаунчер уже зробив усю важку роботу
			// (JAR-и, JSON, іконки) і далі йому немає сенсу тримати пік RSS
			// у собі — саме зараз ОЗП потрібна ГРІ. Повертаємо вільні
			// сторінки ОС одразу, не чекаючи планового прибирання
			// (memtrim.go).
			releaseMemoryNow()
		} else {
			restoreGamePriority()
		}
	})

	// Fallback на Modrinth для заблокованих CurseForge-модів (аналог Prism
	// FallbackMRBlockedMods): реєструємо з урахуванням поточного налаштування.
	a.applyModrinthBlockedFallback(a.cfg.GetSettings().UseModrinthForBlocked)
	// Власний API-ключ CurseForge користувача: відновлюємо зі збережених
	// налаштувань при старті (інакше після перезапуску ефективний ключ
	// мовчки повернувся б до вбудованого, поки користувач не збереже
	// налаштування знову).
	custompack.SetUserCurseForgeAPIKey(a.cfg.GetSettings().CurseForgeAPIKey)
	// Прапорці UI для shell (hardwareAcceleration тощо): shell читає цей файл
	// ПРИ СТАРТІ, до створення WebView2-вікон, тож оновлюємо його одразу
	// після завантаження налаштувань.
	a.writeUIFlagsFile(a.cfg.GetSettings())
	return a
}

// writeUIFlagsFile зберігає кілька прапорців UI у ConfigDir/ui.json — файл,
// який читає shell (не має доступу до SQLite) на старті, до application.New:
//
//	{"hardwareAcceleration": false, "lowMemoryMode": true}
//	  → WebView2 отримує --disable-gpu; на Linux ще й WebKit-тюнінг
//	    (без композитингу/dma-buf) та WebviewGpuPolicyNever для вікон.
//
// Це місток «налаштування (SQLite) → shell (до першого вікна)». Саме тому
// обидва прапорці мусять бути ТУТ, а не доїжджати через bridge-виклики:
// shell читає цей файл до створення першого вікна, коли ще немає ні
// backend.so, ні каналу в нього.
func (a *App) writeUIFlagsFile(s model.Settings) {
	data, err := json.Marshal(map[string]any{
		"hardwareAcceleration": s.HardwareAcceleration,
		"lowMemoryMode":        s.LowMemoryMode,
	})
	if err != nil {
		return
	}
	path := filepath.Join(a.cfg.ConfigDir(), "ui.json")
	if tmp := path + ".tmp"; os.WriteFile(tmp, data, 0o644) == nil {
		_ = os.Rename(tmp, path)
	}
}

// applyModrinthBlockedFallback реєструє/вимикає fallback-резолвер sha1 →
// версія Modrinth для CF-модів, заблокованих автором (3rd party download
// opt-out). enabled=false реєструє nil — custompack.resolveCFFiles тоді не
// пробує Modrinth і позначає такі моди помилкою (стара поведінка).
func (a *App) applyModrinthBlockedFallback(enabled bool) {
	if !enabled {
		custompack.SetModrinthBlockedResolver(nil)
		return
	}
	custompack.SetModrinthBlockedResolver(func(ctx context.Context, sha1hex string) (*custompack.ModrinthMirror, error) {
		v, err := a.modrinth.GetVersionByHash(sha1hex)
		if err != nil || v == nil {
			return nil, err
		}
		fi := v.PrimaryFileIndex()
		if fi >= len(v.Files) || v.Files[fi].URL == "" {
			return nil, nil
		}
		return &custompack.ModrinthMirror{URL: v.Files[fi].URL, Filename: v.Files[fi].Filename}, nil
	})
}

// ServiceStartup виконує ініціалізацію App. Викликається з backend_init
// (C ABI, bridge.h) замість Wails-сервісу — це те, що в v2 було в OnStartup.
func (a *App) ServiceStartup() error {
	a.ctx = context.Background()

	// Crash recovery: якщо попередня міграція теки даних не завершилась
	if state := a.CheckPendingMigration(); state != nil {
		log.Printf("Відновлення міграції %s → %s (зупинка на %s)", state.OldDir, state.NewDir, state.CurrentSub)
		if err := a.ResumeMigration(); err != nil {
			log.Printf("Помилка відновлення міграції: %v", err)
		}
	}

	// ── Структурований лог лаунчера ───────────────────────────────────────
	// Ініціалізуємо одразу після cfg.Load() (нижче), але відкриваємо тут,
	// щоб перші рядки вже писались у файл. Помилка відкриття — не фатальна.
	a.cfg.Load()
	if err := a.applyInstallerSkinPreference(); err != nil {
		log.Printf("installer skin preference: %v", err)
	}
	_ = launchlog.Init(a.cfg.LogsDir(), launchlog.LevelDebug)
	// Перехоплення крашів: crash.log (фатальні помилки рантайму + паніки
	// горутин) і маркер аварійного завершення попередньої сесії. Одразу
	// після launchlog.Init, бо читає хвіст щойно ротованого launcher.log.1.
	crashguard.OnPanic = appendError
	crashguard.Init(a.cfg.LogsDir())

	appLog := launchlog.Component("App")
	appLog.Info("Starting (pid %d)", os.Getpid())
	// InstallDir (не Dir!) — та сама причина, що й у newUpdateChecker
	// вище: launcher-version.json лежить у теці встановлення, а не в
	// теці даних.
	appLog.Info("App version: %s", readLauncherVersion(a.cfg.InstallDir()))

	// Системна інформація (ОС / CPU / RAM / hostname)
	appLog.Info("Platform: %s %s (%s)", runtime.GOOS, osVersion(), runtime.GOARCH)
	appLog.Info("CPUs: %d logical", runtime.NumCPU())
	if mem := totalMemoryMiB(); mem > 0 {
		appLog.Info("Memory: %.1f GiB", float64(mem)/1024)
	}
	if h, err := os.Hostname(); err == nil {
		appLog.Info("Hostname: %s", h)
	}
	appLog.Info("Data directory: %s", a.cfg.Dir())
	appLog.Info("Logs directory: %s", a.cfg.LogsDir())

	// ── Стан бази даних (резервна копія / навмисне скидання) ────────────
	// Якщо база була пошкоджена і відновлена з резервної копії, або стоїть
	// прапорець «стерто навмисно», або базу взагалі не вдалось відкрити —
	// фронтенд показує модальне вікно з поясненням (НЕ мовчимо про втрату
	// даних користувача, ТЗ). Подія db:health іде одразу після старту.
	a.emitDBHealth()
	// ОДНОРАЗОВА міграція файлових реєстрів (builds.json, custom-packs.json,
	// gallery-meta.json, shortcuts.json, browser-ai/, install-state/,
	// presets.json) у SQLite + прибирання легасі-сміття (download-state/,
	// .locks/, logs/, avatars/→cache, skins/→cache). Працює рівно один раз
	// (прапорець files_migrated у app_meta); після неї код працює лише з БД.
	// Виконується ДО першого використання реєстрів — інакше файли вже
	// були б «загублені» (реєстр стартував би порожнім).
	a.runFilesMigration()
	// Реєстр встановлених збірок перестворюємо з БД (міграція могла
	// перенести дані з builds.json у builds_registry).
	a.buildsReg = a.newBuildsRegistry()
	a.buildsReg.Load()
	// Переносимо Shaurma-збірки в окрему теку shaurma_packs/
	a.migrateShaurmaPacksToSubdir()
	// Реєструємо orphaned збірки в customPacks при старті.
	//
	// ПРИСКОРЕННЯ СТАРТУ: винесено у фонову горутину. Функція лише
	// СКАНУЄ теку встановлених збірок (os.Stat/os.ReadFile на кожен ID
	// з buildsReg) і дописує "осиротілі" збірки в customPacks — головний
	// екран не потребує цього результату для першого кадру, а список
	// "Моїх збірок" оновиться на фронтенді, коли скан завершиться (реєстр
	// customPacks — той самий store, який далі читає GetCustomPacks).
	// Безпечно робити асинхронно: на відміну від reconcileLegacyFolders,
	// жоден наступний крок ServiceStartup (rewireComponents і далі) не
	// читає результат цієї функції.
	go func() {
		defer crashguard.Recover("a.syncOrphanedToCustomPacks")
		a.syncOrphanedToCustomPacks()
	}()
	// Автозапуск (Windows Run-ключ / Linux ~/.config/autostart):
	// синхронізуємо запис з режимом startupMode при кожному старті. Покриває
	// й випадок, коли користувач вручну прибрав запис (msconfig/Диспетчер
	// задач), і зміну аргументів (--background) після оновлення лаунчера.
	// Помилка не фатальна для старту — лише лог.
	a.syncAutostart()
	// Канал оновлень застосовуємо ПІСЛЯ завантаження налаштувань: раніше
	// newUpdateChecker читав cfg.GetSettings() у NewApp ДО cfg.Load() (який
	// викликається тут, у ServiceStartup), тож чекер при старті завжди бачив
	// дефолтний "stable" — канал з settings.json (напр. "beta") застосовувався
	// лише після першого SaveSettings. Через це «Перевірити оновлення» могло
	// казати «актуальна версія», поки апдейтер (що читає settings.json напряму)
	// знаходив beta-оновлення. Тепер канал з налаштувань прикладається одразу
	// після Load.
	a.updater.SetChannel(a.cfg.GetSettings().UpdateChannel)

	// Discord Rich Presence: підтримуємо з'єднання у фоні і публікуємо
	// присутність за запущеними збірками (одна — назва збірки, кілька —
	// «N збірок» + список). Увімкнено за замовчуванням, тексти шаблонів
	// редагуються на вкладці «Discord» у Налаштуваннях. discordAppID —
	// Application ID з Discord Developer Portal (див. коментар біля const).
	a.rpc = discordrpc.New(discordAppID, uint32(os.Getpid()), false, "", "")
	a.rpc.SetConfig(
		a.cfg.GetSettings().DiscordRPCEnabled,
		a.cfg.GetSettings().DiscordRPCDetails,
		a.cfg.GetSettings().DiscordRPCState,
	)
	// Безпечний режим (safe_mode.go): не піднімаємо фоновий IPC-клієнт Discord
	// (overlapped named pipe) — у режимі відновлення після повторюваних збоїв
	// у процесі має лишитись якнайменше нативних шляхів.
	if !safeModeActive() {
		a.rpc.Start()
	}

	// Пам'ять: періодична очистка кешів модів (modUpdateCache/
	// modResolveCache/jarMetaCache). Без цього довга сесія з багатьма
	// збірками і сотнями модів накопичувала в пам'яті всі результати
	// мережевих перевірок і метадані jar (див. startModsCachePruner).
	a.startModsCachePruner()

	// Ігровий час: (1) старі сесії згортаємо в місячні підсумки, щоб instance.json не ріс
	// безкінечно (аналітика від цього не змінюється — див. store.CompactPlaytimeSessions);
	// (2) shaurma-редакція заодно дошле загальний час у хмару. Фон, із затримкою — не заважає старту.
	go func() {
		defer crashguard.Recover("a.compactPlaytime")
		time.Sleep(20 * time.Second)
		if n, err := a.cfg.CompactPlaytime(); err != nil {
			log.Printf("playtime: compact: %v", err)
		} else if n > 0 {
			log.Printf("playtime: згорнуто %d старих сесій у місячні підсумки", n)
		}
		a.scheduleCloudPlaytimeSync(10 * time.Second)
	}()

	// Друзі Shaurma: якщо є скін-сесія — піднімаємо presence у фоні (shaurma-
	// редакція; у base-збірці no-op). Горутина, щоб не затримувати старт
	// читанням файлу сесії.
	go func() {
		defer crashguard.Recover("a.startSocialIfLoggedIn")
		a.startSocialIfLoggedIn()
	}()

	// Пам'ять: профіль з налаштувань (GOGC для «економного режиму») і
	// фоновий прибиральник, який повертає вільні сторінки ОС навіть коли
	// лаунчер просто стоїть у треї або поруч запущена гра (memtrim.go).
	a.applyMemoryProfile(a.cfg.GetSettings().LowMemoryMode)
	a.startMemoryTrimmer()

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
	go func() {
		defer crashguard.Recover("bringWindowToFront")
		bringWindowToFront()
	}()

	// Консолідація легасі-тек: усе, що лежало у власних кастомних теках
	// (старі settings.InstanceDir/JavaPath, override з launcher-location.json),
	// переноситься ВСЕРЕДИНІ папки даних .shaurm. Виконуємо ЛИШЕ коли
	// жодна збірка не запущена (після restoreRunningGame знаємо точно) —
	// переносити теку збірок під живою грою не можна. Після консолідації
	// rewireComponents нижче створює компоненти вже з фінальними шляхами.
	if !a.sessionsM.AnyRunning() {
		a.reconcileLegacyFolders()
	}
	a.rewireComponents()
	// Гостьовий P2P-слухач ("LAN через інтернет") — має вже працювати на
	// 127.0.0.1:45678 ДО того, як гравець спробує підключитись до чийогось
	// LAN-світу через Direct Connect: він не тисне жодної кнопки "приєднатись",
	// просто вставляє адресу в гру. Помилка не фатальна для старту лаунчера
	// (напр. порт 45678 зайнятий іншим процесом) — лише пишемо в лог.
	//
	// ВАЖЛИВО: кличемо ОБГОРТКУ App, а не p2pAPI напряму — саме вона
	// перевіряє вимикач disableP2P з налаштувань. Прямий виклик (як було
	// раніше) піднімав слухач навіть при вимкненій фічі.
	if err := a.EnsureP2PGuestService(); err != nil {
		log.Printf("p2p guest service: %v", err)
	}
	// Тимчасові адреси запрошень не зберігаються: усе, що лишилось у книзі з минулого запуску, — сміття.
	go a.purgeTempP2PAddresses(8 * time.Second)
	// Синхронізація settings.InstanceDir/JavaPath з ефективними теками
	// (cfg.InstancesDir()/cfg.JavaDir()). ФІКС «Shaurma-збірка не качає
	// Java/лоадер, поки не поставиш кастомну»: усі робочі місця
	// (LaunchInstance, DownloadPack, моди, міграція) будують шляхи від
	// settings.InstanceDir — якщо в БД лежить старе/бите значення (без
	// .shaurm, легасі-шлях зі старої версії), Shaurma-збірка «не знає, де
	// сканувати» і тихо працює з порожньою текою. Кастомна збірка
	// створювала теки за битим шляхом — і наступний запуск «прогрівався».
	// Тепер шляхи синхронізуються при КОЖНОМУ старті, а не лише при зміні
	// папки даних.
	//
	// НЕ на першому запуску: IsFirstRun() = «у сховищі ще немає рядка налаштувань».
	// syncFolderSettings() його створює, тож без цієї перевірки майстер першого
	// запуску не показувався ніколи (до першого запиту фронтенду рядок уже був).
	// Теки першого запуску записує сам майстер (CompleteSetup → SetupComplete).
	if !a.cfg.IsFirstRun() {
		if err := a.syncFolderSettings(); err != nil {
			log.Printf("ServiceStartup: syncFolderSettings: %v", err)
		}
	}
	// Переносимо кастомні збірки зі старого лаунчера (custom-packs.json у
	// %APPDATA%\.shaurm), якщо такі є — щоб у "Моїх збірках" одразу
	// з'явилися всі збірки користувача з минулої версії. Ідемпотентно.
	a.ImportLegacyCustomPacks()
	// Ліміти завантажень застосовуємо ПІСЛЯ rewireComponents: той
	// перестворює download-engine з дефолтними лімітами, інакше наші
	// значення з settings.json були б втрачені.
	a.applyDownloadLimits()
	// Скін збірки: повертаємо вигляд акаунтів для снапшотів, чиї ігри вже
	// завершились, поки лаунчер був мертвий (крах/перезапуск/вимкнення ПК).
	// Живі сесії не чіпаємо — їх поверне exit-хендлер після зупинки гри.
	//
	// ПРИСКОРЕННЯ СТАРТУ: раніше це виконувалось СИНХРОННО в ServiceStartup,
	// а відновлення скіна ходить у мережу з ретраями (до ~93 с на збірку) —
	// якщо після краху лишився снапшот, backend:ready приходив із запізненням
	// у десятки секунд. Тепер у фоні, ПІСЛЯ rewireComponents (використовує
	// вже фінальний a.wardrobe).
	go func() {
		defer crashguard.Recover("a.restoreStaleBuildSkins")
		a.restoreStaleBuildSkins()
	}()
	// Прапорець м'якого завершення від апдейтера (аудит, п. 5): замість
	// taskkill /F апдейтер створює shrm-quit.flag, і ми самі виконуємо
	// graceful shutdown (PauseAll синхронізацій) перед виходом.
	a.startQuitFlagWatcher()
	// ЛАУНЧЕР НЕ ОНОВЛЮЄ НІЧОГО: оновленням займається ТІЛЬКИ апдейтер
	// (updater.dll), який shell завантажує при старті ДО backend.dll.
	// Лаунчер максимум може показати в налаштуваннях, яка версія нова
	// (App.CheckUpdate — лише читання маніфесту), і при наступному старті
	// застосувати .new-файли, які апдейтер уже скачав
	// (shell applyPendingUpdates: видалити старий updater.dll, перейменувати
	// updater.dll.new). Жодного качання компонентів із боку backend.dll.
	// Стеження за новими скріншотами запущених збірок — для плаваючого
	// віджету «новий скріншот» (App.svelte, gallery:new-screenshot).
	a.startScreenshotWatcher()
	// Бот-прибиральник: фонова очистка застарілого ШІ-кешу, стану
	// встановлення, іконок ярликів без .lnk, старих текстур голів/скінів
	// та метаданих галереї-сиріт (перший прохід — одразу, далі щогодини).
	a.startMaintenance()
	// Авто-збірки (auto-vanilla/auto-snapshot): перевірка найновішої версії
	// Mojang РІВНО раз при кожному старті лаунчера, у фоні. Без цього
	// виклику єдиним тригером перевірки лишався би розділ налаштувань
	// "Авто-збірки" (GetAutoPacks) — якщо користувач туди не заходив,
	// конфіг збірки назавжди застигав на версії з моменту створення, навіть
	// коли Mojang давно випустив новішу (checkAutoVersionPacks просто
	// ніколи не викликався). LaunchInstance додатково підстраховує це
	// синхронною перевіркою прямо перед запуском (ensureAutoVersionSync).
	a.ensureAutoVersionsAtStartup()
	// Динамічний банер головного екрана: один фонова перевірка картинки
	// останнього офіційного оновлення Minecraft при кожному старті
	// (news_banner.go). Результат приїжджає у фронтенд подією news:banner —
	// банер з'являється сам, без дій користувача, і підхоплює нову версію
	// гри одразу після виходу офіційного релізу.
	a.ensureNewsBannerAtStartup()
	// Статус мережі: фоновий пінгер шле network:status (online/offline),
	// фронтенд показує позначку «офлайн» на головній сторінці під назвою
	// лаунчера і ховає/дизейблить мережеві дії, коли інтернету немає.
	a.startNetworkStatusLoop()
	// Пам'ять: старт створює пік (парсинг сховища, скан збірок, міграції) —
	// після того як усе осіло, одразу віддаємо зайве ОС, а не чекаємо
	// першого тіку memtrim (раніше — 5 хв).
	go func() {
		defer crashguard.Recover("a.postStartupTrim")
		time.Sleep(12 * time.Second)
		releaseMemoryNow()
	}()
	return nil
}

// emit шле подію на фронтенд через bridge (shell.exe реалізує emit-колбек
// як Wails Event.Emit). Дані серіалізуються в JSON.
func (a *App) emit(name string, data ...any) {
	var payload any
	if len(data) == 1 {
		payload = data[0]
	} else if len(data) > 1 {
		payload = data
	}
	// Довгі операції (передачі, встановлення, міграції…) — у спільний реєстр
	// для індикатора активності й панелі завдань (activity_api.go).
	a.observeActivity(name, payload)
	b, err := json.Marshal(payload)
	if err != nil {
		bridgeEmit(name, "null")
		return
	}
	bridgeEmit(name, string(b))
}

// ── RAM-попередження перед запуском (ТЗ) ───────────────────────────────
// GetLaunchWarnings аналізує збірку ПЕРЕД запуском і вирішує, чи показувати
// попередження:
//  1. «Нижче рекомендованої RAM» — RecommendedRAMMB збірки (пише Pack
//     Manager у packs-index.json) більша за ефективну RAM;
//  2. «Мало вільної оперативки» — виділена RAM перевищує ВІЛЬНУ ЗАРАЗ
//     пам'ять системи (можливий краш/своп).
//
// Обидва попередження можна вимкнути глобально (Settings.ShowRamWarnings)
// або «не показувати знову» для конкретної збірки (InstanceConfig).
func (a *App) GetLaunchWarnings(packID string) model.LaunchWarnings {
	w := model.LaunchWarnings{PackID: packID}
	settings := a.cfg.GetSettings()
	w.WarningsEnabled = settings.ShowRamWarnings
	w.EffectiveRAMMB = a.effectiveRAM(packID)

	if build, err := a.resolveLaunchBuild(packID); err == nil {
		w.RecommendedRAMMB = build.RecommendedRAMMB
	}
	icfg := a.cfg.GetInstanceConfig(packID)
	w.RecommendedDismissed = icfg.RecommendedRamDismissed
	w.LowRamDismissed = icfg.LowRamDismissed

	si := sysinfo.Collect(a.cfg.Dir())
	// ВАЖЛИВО: раніше тут стояло si.TotalRAMMB (загальний обсяг ВСТАНОВЛЕНОЇ
	// пам'яті) — перевірка №2 фактично звіряла виділену RAM з тим, скільки
	// пам'яті ФІЗИЧНО СТОЇТЬ у системі, а не скільки її ВІЛЬНО ЗАРАЗ. Через
	// це попередження не спрацьовувало навіть коли у користувача реально
	// лишалось критично мало вільної RAM (браузер/інші програми зайняли
	// решту) — воно порівнювало виділені 4-8 ГБ з усіма 16+ ГБ пам'яті,
	// які "є в комп'ютері", а не з тим, що можна реально виділити зараз.
	// FreeRAMMB — вільна пам'ять НА ЦЮ МИТЬ (Windows: AvailPhys,
	// Linux: MemAvailable) — саме те, з чим і повинна звірятись RAM гри.
	w.AvailableRAMMB = si.FreeRAMMB
	if w.AvailableRAMMB <= 0 {
		// Платформа без підтримки FreeRAMMB (напр. macOS-стаб) — фолбек на
		// загальний обсяг, щоб попередження хоч якось працювало замість
		// повного вимкнення (стара поведінка), а не мовчати зовсім.
		w.AvailableRAMMB = si.TotalRAMMB
	}

	// 1. Рекомендована RAM: ефективна менша за рекомендовану і не скинута.
	if w.WarningsEnabled && w.RecommendedRAMMB > 0 && w.EffectiveRAMMB > 0 &&
		w.EffectiveRAMMB < w.RecommendedRAMMB && !w.RecommendedDismissed {
		w.ShowRecommended = true
	}
	// 2. Вільна оперативка: виділено більше, ніж реально вільно → краш/своп.
	if w.WarningsEnabled && w.AvailableRAMMB > 0 &&
		int64(w.EffectiveRAMMB) > w.AvailableRAMMB && !w.LowRamDismissed {
		w.ShowLowRam = true
	}
	return w
}

// DismissPackWarning зберігає «не показувати знову» для попередження
// (kind: "recommended" | "lowram") — ЛИШЕ для цієї збірки, як того вимагає
// ТЗ («галочка лише для цієї збірки», глобальний вимикач — окремо в
// налаштуваннях).
func (a *App) DismissPackWarning(packID, kind string) error {
	icfg := a.cfg.GetInstanceConfig(packID)
	icfg.ID = packID
	switch kind {
	case "recommended":
		icfg.RecommendedRamDismissed = true
	case "lowram":
		icfg.LowRamDismissed = true
	default:
		return fmt.Errorf("невідомий тип попередження: %s", kind)
	}
	return a.cfg.SaveInstanceConfig(icfg)
}

// SetPackRAM встановлює RAM саме для ЦІЄЇ збірки (MaxRAMOverride). Це
// реалізація ТЗ «якщо RAM була з загальних налаштувань і не вистачало — ми
// не редагуємо загальні, а додаємо, що збірка використовує окрему RAM, і
// редагуємо повзунок»: наявність override автоматично означає окрему RAM
// для збірки.
func (a *App) SetPackRAM(packID string, ramMB int) error {
	if ramMB < 1024 {
		return fmt.Errorf("RAM замала: %d МБ", ramMB)
	}
	icfg := a.cfg.GetInstanceConfig(packID)
	icfg.ID = packID
	icfg.MaxRAMOverride = ramMB
	return a.cfg.SaveInstanceConfig(icfg)
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
// Для кастомних збірок (браузерний модпак/імпорт) — м'який cancel
// активного імпорту: часткові файли (.part) лишаються на диску, і
// повторний DownloadBrowserModpack/ImportModpackArchive продовжить з місця
// (Range-резюме), тобто пауза НЕ починає завантаження з нуля.
func (a *App) PauseDownload(packID string) {
	if a.isCustomPack(packID) {
		// М'яка зупинка активного імпорту: прогрес лишається в dlTransient
		// з Mode="paused", кнопка в UI стає "Продовжити". Раніше тут
		// викликався CancelModpackImport (без події paused), через що
		// картка «зависала» у стані downloading і «Продовжити» не з'являвся.
		a.PauseModpackImport(packID)
		return
	}
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

// syncStateStore повертає сховище InstallState для V2-системи синхронізації.
// Джерело правди — SQLite (таблиця install_state); файловий фолбек — лише
// для тестів. Перестворюється в rewireComponents при зміні AppDir.
func (a *App) syncStateStore() *syncengine.StateStore {
	return syncengine.NewStateStore(a.cfg.Dir())
}

func (a *App) DownloadPack(packID string) {
	// Кастомна збірка — «Завантажити»/«Продовжити»: браузерні модпаки
	// (modrinth/curseforge) качають архів з Range-резюме та імпортують у
	// той самий pack ID; модпак з локального файлу (import) відновлюється
	// зі збереженого шляху архіву. Без цього «Продовжити» після паузи для
	// custom-збірок падало в глухий кут (DownloadPack не робив нічого).
	if a.isCustomPack(packID) {
		if _, err := a.ResumeModpackImport(packID); err != nil {
			a.emit("download:error", map[string]string{"packId": packID, "message": err.Error()})
		}
		return
	}
	a.packs.DownloadPack(packID, a)
}

// ResumeModpackImport продовжує завантаження кастомної збірки після паузи
// (кнопка «Продовжити»):
//   - браузерний модпак (SourceArchiveURL) — DownloadBrowserModpack:
//     архів докачується з .download через Range-резюме, імпорт іде в той
//     самий pack ID;
//   - модпак з локального файлу (Pack.ArchivePath) — імпорт повторно
//     запускається зі збереженого шляху архіву; вже завантажені моди
//     (і недокачані .part) докачаються з місця через Range.
//
// АСИНХРОННИЙ: повертає pack ID одразу, прогрес іде подіями sync:progress
// (той самий конвеєр, що ImportModpackArchive/DownloadBrowserModpack).
func (a *App) ResumeModpackImport(packID string) (custompack.Pack, error) {
	pack, ok := a.customPacks.Get(packID)
	if !ok {
		return custompack.Pack{}, fmt.Errorf("збірку не знайдено: %s", packID)
	}
	// Новий запуск конвеєра — знімаємо paused-маркер минулої паузи, щоб
	// пізніше скасування коректно розпізнавалось як скасування.
	a.customImportMu.Lock()
	delete(a.customImportPaused, packID)
	a.customImportMu.Unlock()
	if pack.ArchivePath == "" {
		return a.DownloadBrowserModpack(packID)
	}
	// .shaurmpack — власний конвеєр (internal/shaurmpack), не custompack.
	if strings.EqualFold(filepath.Ext(pack.ArchivePath), shaurmpackExt) {
		return a.resumeShaurmpackImport(pack)
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.customImportMu.Lock()
	a.customImportCancel[packID] = cancel
	a.customImportMu.Unlock()
	packName := pack.Name
	archivePath := pack.ArchivePath

	go func() {
		defer crashguard.Recover("app.go:1040")
		defer func() {
			cancel()
			a.customImportMu.Lock()
			delete(a.customImportCancel, packID)
			a.customImportMu.Unlock()
		}()
		detected, err := custompack.DetectArchive(archivePath)
		if err != nil {
			a.emit("download:error", map[string]string{"packId": packID, "message": err.Error()})
			return
		}
		res, err := a.importBrowserArchiveInto(ctx, archivePath, detected, pack, pack.SourceVersionID)
		if err != nil {
			if ctx.Err() == nil {
				a.emit("download:error", map[string]string{"packId": packID, "message": err.Error()})
			}
			return
		}
		a.applyMinecraftDefaultsNow(res.ID)
		a.emit("builds:changed", res.ID)
	}()

	return custompack.Pack{ID: packID, Name: packName}, nil
}

// CancelDownload — ПОВНЕ скасування якання конкретної збірки: часткові
// файли видаляються, кнопка повертається на "Встановити" (див.
// syncengine.Runner.Cancel). Для кастомних збірок — скасовує активний
// імпорт і прибирає .part-прогрес (повний cancel, на відміну від паузи).
func (a *App) CancelDownload(packID string) {
	if a.isCustomPack(packID) {
		a.CancelModpackImport(packID)
		// Повне скасування: прибираємо .part-архів, щоб наступний запуск
		// почав чисто, а не продовжував наполовину скачаний файл.
		if dir := filepath.Join(a.packDir(packID, ""), ".download"); dir != "" {
			_ = os.RemoveAll(dir)
		}
		return
	}
	a.packs.CancelDownload(packID)
}

// isCustomPack — чи збірка з цим ID належить кастомному сховищу
// (браузерні модпаки, локальний імпорт, ручне створення).
// Shaurma-збірки НІКОЛИ не є кастомними, навіть якщо випадково потрапили
// в customPacks.
func (a *App) isCustomPack(packID string) bool {
	if a.buildsReg != nil {
		if entry, ok := a.buildsReg.Get(packID); ok && entry.Kind == builds.KindShaurma {
			return false
		}
	}
	_, ok := a.customPacks.Get(packID)
	return ok
}

// packDir повертає теку збірки залежно від типу.
// Kind визначається buildsRegistry — єдиному джерелу правди.
// Кастомні: installations/<id>/
// Shaurma:   installations/shaurma_packs/<id>/
func (a *App) packDir(packID, kind string) string {
	if t, ok := a.serverForModTarget(packID); ok {
		return t.srv.Dir // таргет менеджера модів — сервер (mod_target.go)
	}
	if kind == "" {
		if a.buildsReg != nil {
			if entry, ok := a.buildsReg.Get(packID); ok {
				kind = string(entry.Kind)
			}
		}
		if kind == "" {
			kind = string(builds.KindCustom)
		}
	}
	if kind == string(builds.KindShaurma) {
		newDir := filepath.Join(a.cfg.ShaurmaPacksDir(), packID)
		if _, err := os.Stat(newDir); err == nil {
			return newDir
		}
		// Fallback: міграція не завершена — шукаємо в старій теці
		oldDir := filepath.Join(a.cfg.InstancesDir(), packID)
		if _, err := os.Stat(oldDir); err == nil {
			return oldDir
		}
		return newDir
	}
	return filepath.Join(a.cfg.InstancesDir(), packID)
}

// gameDir повертає .minecraft теку збірки (авто-детект kind).
func (a *App) gameDir(packID string) string {
	if t, ok := a.serverForModTarget(packID); ok {
		return t.srv.Dir // у сервера mods/ лежить прямо в його теці
	}
	return filepath.Join(a.packDir(packID, ""), ".minecraft")
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

// GetPackWhitelist повертає повні записи білого списку моду (глобальний +
// per-pack, дедупльовані за ModID) для показу картки в UI сторінки
// збірки — іконка/назва/посилання на сторінку Modrinth/CurseForge, коли
// автор збірки додав запис за посиланням на проєкт (Platform/ProjectID/
// IconURL/ProjectURL заповнені); для записів "jar"/"manual" UI показує
// звичайну картку без іконки. Порожній список — не помилка (білий список
// збірки може бути просто порожнім); мережева помилка повертається
// окремо, щоб UI міг показати "не вдалось завантажити" замість тихо
// показати порожній список.
func (a *App) GetPackWiki(packID string) (*syncengine.PackWikiDoc, error) {
	return a.packs.GetPackWiki(packID)
}

func (a *App) GetPackWhitelist(packID string) ([]syncengine.WhitelistEntry, error) {
	if a.packs == nil {
		return nil, fmt.Errorf("pack provider unavailable")
	}
	return a.packs.GetPackWhitelist(packID)
}

// localFileDataURL читає локальний файл іконки/фону кастомної збірки і
// повертає його як data: URL (той самий формат, що AssetDataURL для CDN).
func localFileDataURL(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		// Збірка ще НЕ встановлена (іконка/фон з `installations/<id>/`)
		// або файл видалено — це не помилка, а «асета поки немає».
		// Фронтенд (packAssets) кешує порожній результат і показує
		// fallback-іконку; раніше помилка через автолог call() шуміла
		// в журналі на кожній невстановленій збірці.
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("не вдалося прочитати локальний асет: %w", err)
	}
	// MIME визначаємо за ВМІСТОМ (магічні байти), а не за розширенням:
	// SaveEditedScreenshot завжди пише PNG-байти незалежно від розширення
	// оригіналу, тож після редагування .jpg-скріншота у файлі лежать PNG-
	// дані. Якби MIME брався з розширення (.jpeg), браузер отримував би
	// data:image/jpeg з PNG-вмістом — <img> не міг його декодувати, і
	// галерея показувала «Не вдалось завантажити скріншот» (скарга:
	// «картинка втрачена»). image.Decode і так снифтить вміст, тож це
	// вирівнює data-URL з рештою пайплайну.
	ct := contentTypeForBytes(data)
	if ct == "" {
		// Нерозпізнаний вміст — фолбек на розширення (для файлів без
		// магічних байтів, напр. SVG-текст).
		ct = "image/png"
		switch strings.ToLower(filepath.Ext(path)) {
		case ".jpg", ".jpeg":
			ct = "image/jpeg"
		case ".webp":
			ct = "image/webp"
		case ".gif":
			ct = "image/gif"
		case ".svg":
			ct = "image/svg+xml"
		}
	}
	return "data:" + ct + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

// contentTypeForBytes визначає MIME-тип зображення за магічними байтами
// (PNG/JPEG/GIF/WebP); порожній рядок = вміст не розпізнано.
func contentTypeForBytes(data []byte) string {
	switch {
	case len(data) >= 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n":
		return "image/png"
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return "image/jpeg"
	case len(data) >= 6 && (string(data[:6]) == "GIF87a" || string(data[:6]) == "GIF89a"):
		return "image/gif"
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp"
	}
	return ""
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
//	mod_sources   — фіолетовий (синхронізація модів із «єдиних джерел»)
//	worker_update — синій   (звірка версії файлів збірки з Шаурма-worker)
//	java          — червоний (перевірка/встановлення Java)
//	loader        — зелений (встановлення лоадера Fabric/Quilt/Forge/NeoForge)
type LaunchStage string

const (
	LaunchStageChecking     LaunchStage = "checking"
	LaunchStageModSources   LaunchStage = "mod_sources"
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
	// Warning — непорушне попередження, яке НЕ зупиняє запуск (напр.
	// "немає інтернету — B2310 запуститься офлайн"). На відміну від
	// Error, після Warning прогрес продовжується далі як звичайно.
	// Фронтенд показує його як модалку і йде далі, не чекаючи Done.
	Warning string `json:"warning,omitempty"`
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
	// Авто-починка: помилка запуску (Java, лоадер, файли) → пропозиція лагоджень.
	a.repairOnLaunchError(instanceID, err)
}

// ErrLaunchCancelled — запуск збірки скасовано користувачем під час
// встановлення компонентів (кнопка «Зупинити»/«Скасувати»). Окремий
// сентинел, щоб фронтенд відрізняв СКАСУВАННЯ від реальної помилки
// запуску і не показував червону помилку там, де користувач сам зупинив
// качку (Java/лоадер/Minecraft).
var ErrLaunchCancelled = errors.New("launch cancelled by user")

// ErrBuildAlreadyRunning — ця збірка вже запущена на цьому акаунті. Текст
// лишається незмінним: фронтенд розпізнає помилку за підрядком
// "already running" (див. isAlreadyRunning у ShaurmaChat / P2PJoinModal).
var ErrBuildAlreadyRunning = errors.New("this build is already running on this account")

// isLaunchCancelled — чи помилка є наслідком скасування користувачем
// (кнопка «Зупинити» під час встановлення компонентів гри).
func isLaunchCancelled(err error) bool {
	return errors.Is(err, minecraft.ErrInstallCancelled) ||
		errors.Is(err, java.ErrInstallCancelled) ||
		errors.Is(err, ErrLaunchCancelled)
}

// launchInstallErr — спільна обробка помилки встановлення компонентів під
// час LaunchInstance (Java/лоадер/версія). Скасування користувачем
// перетворюється на ErrLaunchCancelled + подію launch:cancelled (фронтенд
// показує нейтральне «Запуск скасовано» замість червоної помилки); решта
// помилок обгортається контекстом і емітить launch:error як раніше.
func (a *App) launchInstallErr(instanceID string, err error, wrap string) error {
	if isLaunchCancelled(err) {
		a.emitLaunchCancelled(instanceID)
		return ErrLaunchCancelled
	}
	wrapped := fmt.Errorf("%s: %w", wrap, err)
	a.emitLaunchError(instanceID, wrapped)
	return wrapped
}

// emitLaunchCancelled повідомляє фронтенд, що запуск збірки скасовано
// користувачем (не помилка!): стан прогресу скидається у нейтральний
// done без Error, а окрема подія launch:cancelled дозволяє UI показати
// «Запуск скасовано» і не тримати червоний стан на тайлі.
func (a *App) emitLaunchCancelled(instanceID string) {
	a.storeLaunch(instanceID, LaunchProgress{InstanceID: instanceID, Stage: LaunchStageDone, Done: true})
	a.emit("launch:cancelled", instanceID)
}

// emitLaunchWarning шле попередження, яке НЕ зупиняє запуск (Done:
// false) — фронтенд показує модалку і продовжує слухати launch:progress
// далі, аж до Done. НЕ зберігається в launchState (storeLaunch), бо це
// не "останній відомий стан" — наступні Progress/Error/Done одразу
// перепишуть його для GetConsoleContext.
func (a *App) emitLaunchWarning(instanceID string, warning string) {
	a.emit("launch:progress", LaunchProgress{InstanceID: instanceID, Warning: warning})
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

// isLauncherManagedJava — чи тека Java, яку вказує користувач/налаштування
// (settings.JavaPath або override збірки), керується САМИМ лаунчером, тобто
// лаунчер має САМ поставити туди Java при запуску гри:
//   - порожній шлях — дефолт, вбудована тека лаунчера (cfg.JavaDir());
//   - рівно cfg.JavaDir() — вбудована тека;
//   - будь-яка інша тека, де javaw.exe ще не існує, — користувач обрав
//     СВОЮ теку для Java, і лаунчер ставить Java саме туди (шлях
//     користувача підпорядковується), а не падає «java not found».
//
// Останній випадок критичний: settings.JavaPath може розійтись з поточною
// папкою даних (застаріле значення після зміни папки даних, незавершене
// перенесення в майстрі, легасі-імпорт зі старого лаунчера тощо). Раніше
// жорстка рівність javaPath == cfg.JavaDir() відправляла такий шлях у гілку
// «ручної Java» — лаунчер лише перевіряв наявність javaw.exe і помирав
// «java not found at <теку>\bin\javaw.exe», навіть не спробувавши качати.
//
// Явний шлях до .exe (ручна Java) — НЕ керується лаунчером: якщо файл
// відсутній, це помилка користувача, і її треба показати, а не затирати.
func (a *App) isLauncherManagedJava(javaPath string) bool {
	if javaPath == "" || javaPath == a.cfg.JavaDir() {
		return true
	}
	if strings.HasSuffix(strings.ToLower(javaPath), ".exe") {
		return false
	}
	_, err := os.Stat(a.cfg.ResolveJavaExe(javaPath))
	return os.IsNotExist(err)
}

// ensureJavaForLaunch ставить вбудовану Java потрібного мажора у ТЕКУ, яку
// користувач обрав для Java (settings.JavaPath, override збірки або
// cfg.JavaDir() — залежить від переданого javaPath). Порожній javaPath =
// вбудована тека лаунчера. Для кастомної теки створюється окремий
// java.Installer, націлений саме на неї — Java лягає у теку користувача,
// а не у вбудовану. Реальний прогрес (java.Progress з відсотками)
// форвардиться у launch:progress — бар на сторінці збірки показує
// "Завантаження Java 21... 45%" замість статичного підпису.
func (a *App) ensureJavaForLaunch(instanceID, javaPath string, major int, component string) (string, error) {
	inst := a.javaInst
	if javaPath != "" && javaPath != a.cfg.JavaDir() {
		// Користувач обрав СВОЮ теку Java — ставимо Java саме туди.
		inst = java.NewInstaller(javaPath)
		// Тимчасовий інстанс слухає ТОЙ САМИЙ прапорець скасування, що й
		// основний: «Зупинити» під час качки Java у кастомній теці також
		// скасовує встановлення (інакше кнопка «Зупинити» не працювала б
		// для Java користувача).
		inst.ShareCancelFlagWith(a.javaInst)
	}
	prevJava := inst.ProgressHandler()
	inst.SetProgressHandler(func(p java.Progress) {
		a.emit("java:progress", p)
		a.emitLaunchProgressPct(instanceID, LaunchStageJava, p.Message, p.Percent)
	})
	installed, err := inst.EnsureMajorWithComponent(major, component)
	inst.SetProgressHandler(prevJava)
	if err != nil {
		return "", err
	}
	return installed, nil
}

func (a *App) LaunchInstance(instanceID string) error {
	if err := a.shareLockErr("build", instanceID); err != nil {
		a.emitLaunchError(instanceID, err)
		return err
	}
	// Новий запуск = нове встановлення: скидаємо прапорець скасування
	// (і minecraft-інсталера, і Java), щоб «Зупинити» з попереднього
	// запуску не вбив цей одразу. activeInstallID фіксує, ЯКА САМЕ
	// збірка зараз власник глобального a.inst/a.javaInst — див.
	// CancelGameInstall(instanceID) нижче: без цього клік «Стоп» на
	// картці збірки A міг би скасувати встановлення збірки B, якщо вони
	// збіглись у часі (LaunchInstance свідомо дозволяє паралельний запуск
	// різних збірок на одному акаунті).
	a.activeInstallID.Store(instanceID)
	// Авто-збірка оновлює компоненти нової версії (кнопка «Завантажити»
	// після авто-перемикання конфіга на нову версію Mojang): запуск під
	// час качки заблокований — інакше гра стартувала б «напівпорожньою»
	// або заважала б інсталеру писати в спільну базу Minecraft.
	a.autoRefreshMu.Lock()
	refreshing := a.autoRefreshBusy[instanceID]
	a.autoRefreshMu.Unlock()
	if refreshing {
		err := fmt.Errorf("оновлення збірки вже йде — дочекайтесь завершення завантаження нової версії")
		a.emitLaunchError(instanceID, err)
		return err
	}
	a.inst.ResetCancelInstall()
	a.javaInst.ResetCancelInstall()
	// Jar-и з "%" в імені (%2B, %20 …) Minecraft не завантажує — чистимо імена.
	a.fixModFileNames(instanceID)
	a.emitLaunchProgress(instanceID, LaunchStageChecking, "Перевірка збірки...")
	// Авто-збірка (auto-vanilla/auto-snapshot): підхоплюємо найновішу версію
	// Mojang прямо ПЕРЕД тим, як читати конфіг збірки нижче — синхронно, а
	// не покладаючись лише на фонову перевірку (стартову чи періодичну),
	// яка могла ще не встигнути відпрацювати. no-op для звичайних кастомних
	// і Shaurma-збірок (ensureAutoVersionSync одразу виходить, якщо
	// instanceID не належить жодній авто-збірці) і безпечний офлайн (мовчки
	// лишає поточну встановлену версію при мережевій помилці).
	a.ensureAutoVersionSync(instanceID)
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
	// launchClickAccount (див. поле в App): акаунт, зафіксований фронтендом
	// РІВНО в момент кліку «Грати» — має пріоритет над пізнім читанням
	// ActiveAccount(), щоб паралельна зміна акаунта під час тривалої
	// перевірки/встановлення файлів НЕ підмінила, на кому саме стартує гра.
	a.launchClickAccountMu.Lock()
	clickAccID, hasClickAcc := a.launchClickAccount[instanceID]
	if hasClickAcc {
		delete(a.launchClickAccount, instanceID)
	}
	a.launchClickAccountMu.Unlock()
	if hasClickAcc && clickAccID != "" {
		for _, acc := range accounts {
			if acc.ID == clickAccID {
				account = acc
				break
			}
		}
	}
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
	// Персональний акаунт ЯРЛИКА (--account <id> у CLI-аргументах .lnk,
	// App.LaunchInstanceShortcut): найсильніший override — перекриває і
	// активний акаунт, і прив'язку збірки, рівно для цього запуску.
	a.launchAccountOverrideMu.Lock()
	overrideAccID, hasAccOverride := a.launchAccountOverride[instanceID]
	if hasAccOverride {
		delete(a.launchAccountOverride, instanceID)
	}
	a.launchAccountOverrideMu.Unlock()
	if hasAccOverride && overrideAccID != "" {
		for _, acc := range accounts {
			if acc.ID == overrideAccID {
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
	// На цьому акаунті вже запущена САМЕ ЦЯ збірка — дублікат не створюємо.
	// РІЗНІ збірки на одному акаунті — дозволено, кожна своя незалежна сесія
	// (той самий акаунт може паралельно грати в кілька збірок одночасно,
	// кожна зі своїм таймером/консоллю/моніторингом виходу).
	if a.sessionsM.IsRunning(account.ID, build.ID) {
		// Запуск «на сервер» (чат / P2P-картка): збірка вже грає — це НЕ збій
		// запуску. Не пишемо помилку в стан запуску й не кличемо авто-починку
		// (інакше запущена збірка отримує червоний «Error» і пропозицію
		// лагодження). Фронтенд за ErrBuildAlreadyRunning сам копіює адресу
		// сервера, щоб гравець вставив її в «Пряме підключення» у вже
		// запущеній грі.
		a.launchServerOverrideMu.Lock()
		_, viaServer := a.launchServerOverride[instanceID]
		a.launchServerOverrideMu.Unlock()
		if viaServer {
			return ErrBuildAlreadyRunning
		}
		a.emitLaunchError(instanceID, ErrBuildAlreadyRunning)
		return ErrBuildAlreadyRunning
	}

	// Microsoft-акаунт: якщо токен прострочений (або без нього) —
	// оновлюємо через refresh token і зберігаємо назад. Токен у
	// accounts.json тримаємо для цього (як і старий лаунчер).
	//
	// НЕМАЄ ІНТЕРНЕТУ (як у Prism — "Play Offline"): якщо саме мережа
	// винна (DNS/тайм-аут/з'єднання), а не акаунт (забанений, відкликаний
	// refresh token тощо) — не блокуємо гравця повністю. Гра сама по собі
	// НЕ перевіряє токен по мережі при старті одиночної гри — токен
	// потрібен лише для функцій, які й так вимагають інтернет
	// (мультиплеєр на офіційних серверах, скіни). Тож якщо в акаунті вже
	// є Username/UUID з попереднього успішного входу — запускаємо з ними
	// й попереджаємо гравця модалкою, що це офлайн-сесія, а не мовчки
	// підміняємо і не блокуємо запуск сирою помилкою "token refresh:
	// context deadline exceeded".
	if account.Type == "microsoft" {
		needRefresh := account.AccessToken == "" ||
			(account.ExpiresAt > 0 && time.Now().UnixMilli() > account.ExpiresAt-5*60*1000)
		if needRefresh && account.RefreshToken != "" {
			refreshed, err := a.msAuth.RefreshTokens(account.RefreshToken)
			if err != nil {
				if neterr.IsNetwork(err) && account.Username != "" && account.UUID != "" {
					a.emitLaunchWarning(instanceID, fmt.Sprintf(
						"Немає інтернету — не вдалося перевірити акаунт Microsoft. %s запуститься в офлайн-режимі: без мультиплеєра на офіційних серверах, поки з'єднання не відновиться.",
						build.Name,
					))
					// Продовжуємо запуск зі СТАРИМИ полями account
					// (Username/UUID лишились з попереднього успішного
					// входу) — AccessToken може бути протухлим/порожнім,
					// гра це не перевіряє по мережі при старті.
				} else {
					// Акаунт справді невалідний (відкликаний токен,
					// заблокований і т.д.) АБО немає навіть кешованого
					// Username/UUID, щоб було з чим запускати офлайн —
					// тут дійсно нема іншого виходу, крім помилки.
					err = wrapRefreshErr("token refresh", err)
					a.emitLaunchError(instanceID, err)
					return err
				}
			} else {
				account = *refreshed
				_ = a.cfg.UpdateAccount(account)
			}
		}
	}

	// «Єдине джерело» модів: перед запуском звіряємо хеші прикріплених
	// джерел (окремих jar або теки модів іншої збірки) з mods/ і копіюємо
	// змінене — з візуальним прогресом (стадія mod_sources). Для збірок
	// без джерел і для Shaurma-збірок — миттєвий no-op.
	if !build.IsShaurma {
		a.syncSourcesForLaunch(instanceID)
	}

	// Шаурма-збірки: перед запуском ЗАВЖДИ звіряємо локальний стан файлів
	// проти маніфесту worker'а через BuildPlan — а не лише тоді, коли
	// InstalledVersion відрізняється від версії на сервері. Порівняння
	// версій — це один рядок на всю збірку: якщо версія НЕ змінилась, а
	// гравець сам видалив/зіпсував мод, шейдерпак чи конфіг, версії
	// однакові, і раніше цей блок пропускав перевірку ЦІЛКОМ — BuildPlan
	// (який якраз вміє звіряти по кожному файлу окремо: хеші, on_missing,
	// orphan-видалення) узагалі не викликався. Тепер синк запускається
	// щоразу; для вже актуальної збірки BuildPlan має швидкий шлях (0
	// мережевих завантажень, лише existence-перевірки на диску) і одразу
	// повертає ModeComplete — тож зайвої затримки для звичайного запуску
	// це не додає, а биту/неповну установку саме тут і виправляє.
	if build.IsShaurma {
		a.emitLaunchProgress(instanceID, LaunchStageWorkerUpdate, "Перевірка файлів збірки...")
		if entry, err := a.packs.GetPackByID(instanceID); err == nil {
			a.setLaunchTracked(instanceID)
			a.packs.DownloadPack(instanceID, a)
			a.waitForSyncStartThenFinish(instanceID)
			a.unsetLaunchTracked(instanceID)
			// Реєстр міг змінитись — перечитуємо build (на випадок, якщо
			// worker відкоригував версію лоадера/гри у маніфесті).
			if nb, err := a.resolveLaunchBuild(instanceID); err == nil {
				build = nb
			}
			// Офіційний сервер збірки (serverIp з packs-index.json): якщо
			// servers.dat відсутній або порожній — додаємо офіційний сервер,
			// щоб він був у списку Multiplayer. Список, який гравець уже
			// наповнив, не чіпаємо.
			// Якщо в Pack Manager увімкнено «Офіційний сервер завжди в
			// servers.dat» (officialServerSync) — додатково ставимо/оновлюємо
			// запис з АКТУАЛЬНИМ IP із CDN при кожному запуску.
			a.ensureOfficialServer(instanceID, build.Name, entry.ServerIP, entry.OfficialServerSync)
		}
	}

	settings := a.cfg.GetSettings()
	// Тека гри збірки — InstanceDir/<id>/.minecraft (як у старого лаунчера:
	// overrides розпаковуються в installations/<id>/.minecraft/, звідти ж
	// запускається гра). Versions/libraries/assets живуть окремо в спільній
	// теці <dataDir>/minecraft (див. LaunchConfig.MinecraftDir).
	gameDir := a.gameDir(build.ID)
	// Override-параметри збірки (сторінка редагування): порожні/нульові
	// поля означають "успадкувати з глобальних Settings", тому кожне
	// значення нижче обране як icfg, якщо воно задане, інакше — глобальне.
	icfg := a.cfg.GetInstanceConfig(build.ID)

	// Разовий override сервера (App.LaunchInstanceOnServer — "Грати" з
	// конкретного рядка в розділі "Сервери"): підмінюємо ЛИШЕ параметри
	// автоприєднання для ЦЬОГО запуску, не чіпаючи збережений icfg.
	a.launchServerOverrideMu.Lock()
	overrideAddr, hasOverride := a.launchServerOverride[instanceID]
	a.launchServerOverrideMu.Unlock()
	if hasOverride && overrideAddr != "" {
		icfg.AutoJoinEnabled = true
		icfg.AutoJoinType = "server"
		icfg.AutoJoinServer = overrideAddr
	}
	// Сервери «разом зі збіркою» стартують ДО підготовки гри — поки гра
	// вантажиться, сервер встигає піднятись (pack_server_link.go).
	a.startLinkedServers(build.ID, &icfg, hasOverride && overrideAddr != "")

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
	// managedJava — теку Java керує САМ лаунчер: вбудована тека лаунчера
	// (cfg.JavaDir()), порожній шлях (дефолт = вбудована) або БУДЬ-ЯКА
	// тека, яку користувач обрав для Java, але де javaw.exe ще не існує.
	// У такій теці лаунчер САМ ставить потрібний мажор замість того, щоб
	// падати «java not found» — шлях користувача підпорядковується, а не
	// ігнорується (див. isLauncherManagedJava). Раніше тут була жорстка
	// рівність javaPath == cfg.JavaDir(): якщо settings.JavaPath розходився
	// з поточною папкою даних (застаріле значення після зміни папки,
	// незавершене перенесення в майстрі тощо), лаунчер вважав Java
	// «ручною» і падав «java not found at <теку>/bin/javaw.exe», навіть
	// не спробувавши завантажити Java у теку користувача.
	managedJava := a.isLauncherManagedJava(javaPath)
	// Тека, в яку лаунчер ставить Java (порожня = вбудована cfg.JavaDir()).
	// Захоплюємо ДО першого перепризначення javaPath на шлях до exe.
	managedJavaDir := javaPath
	a.emitLaunchProgress(instanceID, LaunchStageJava, "Перевірка Java...")
	// Порожній javaPath (налаштування не збережені / дефолт) = вбудована Java
	// лаунчера: EnsureMajorWithComponent сам ставить ПОТРІБНИЙ мажор (Java 21 для
	// neoforge 1.21.1, Java 17 для forge 1.20.1 тощо) використовуючи component
	// з version.json (якщо є) для повної автономності. Раніше порожній шлях
	// минав цю гілку і запускав на Java, що лишилась у теці з минулого
	// разу (напр. Java 17 замість 21) — звідси "Unsupported major.minor
	// version 65.0" при читанні bootstraplauncher-2.0.2.jar.
	if managedJava {
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
		// ensureJavaForLaunch ставить Java потрібного мажора у ТЕКУ, яку
		// обрав користувач (вбудовану чи власну), і форвардить реальний
		// прогрес (java.Progress з відсотками) у launch:progress — бар на
		// сторінці збірки показує "Завантаження Java 21... 45%" замість
		// статичного підпису.
		installed, err := a.ensureJavaForLaunch(instanceID, managedJavaDir, recommended, earlyComponent)
		if err != nil {
			return a.launchInstallErr(instanceID, err, fmt.Sprintf("вбудована Java %d", recommended))
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
		return a.launchInstallErr(instanceID, err, "ensure loader")
	}
	vjson, err := a.inst.EnsureVersion(launchVersion)
	a.inst.SetInstallProgressHandler(prevInst)
	if err != nil {
		return a.launchInstallErr(instanceID, err, "ensure version")
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
		if managedJava {
			// Java лаунчера (вбудована або обрана користувачем тека) —
			// перевстановлюємо/перевибираємо на правильний мажор перед
			// фактичним запуском. Використовуємо component з version.json
			// (якщо є) для повної автономності.
			installed, err := a.ensureJavaForLaunch(instanceID, managedJavaDir, recommended, component)
			if err != nil {
				return a.launchInstallErr(instanceID, err, fmt.Sprintf("вбудована Java %d (за version.json)", recommended))
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
		// Per-instance override (слайдер RAM у налаштуваннях збірки)
		// завжди сильніший за заводське значення збірки.
		maxRAM = intOverride(build.MaxRAMMB, icfg.MaxRAMOverride)
	} else {
		maxRAM = intOverride(settings.MaxRAM, icfg.MaxRAMOverride)
	}
	// Мінімальна RAM (-Xms) окремим полем: якщо для збірки не задано,
	// LaunchConfig.MinRAM лишається 0 і Launcher сам порахує ram/2 —
	// та сама поведінка, що й раніше, коли цього поля не було.
	minRAM := 0
	if build.UseCustomRAM && build.MinRAMMB > 0 {
		minRAM = intOverride(build.MinRAMMB, icfg.MinRAMOverride)
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
	// Очищення буфера ПЕРЕД кожним запуском: лог минулої сесії не має
	// змішуватись з новою (вимога «при запуску буфер консолі чиститься»).
	// Окремі вікна/вкладки консолі цієї збірки теж прибирають рядки й
	// AI-панель (подія console:cleared з buildID).
	buf.Clear()
	a.emit("console:cleared", build.ID)
	buf.AppendLevel(console.LevelSystem, fmt.Sprintf("Запуск збірки \"%s\" (%s %s)...", build.Name, build.Loader, build.MCVersion))
	if javaWarn != "" {
		buf.AppendLevel(console.LevelWarn, javaWarn)
	}

	// Санітизація JVM-аргументів (internal/jvmargs). JavaArgs/JavaArgsOverride
	// можуть прийти з чужих даних (міграція Prism), а build.LaunchArgs — з
	// маніфесту збірки, тож -javaagent, -XX:OnError, -Xbootclasspath тощо до
	// exec не доходять. Відхилене НЕ зникає мовчки — кожен аргумент іде в
	// консоль збірки попередженням; запуск продовжується. ExtraJVMArgs
	// (authlib-injector) і аргументи version.json цим кроком не фільтруються.
	keptJava, rejJava := jvmargs.SanitizeUser(javaArgs)
	javaArgs = strings.Join(keptJava, " ")
	packArgs, rejPack := jvmargs.FilterPack(build.LaunchArgs)
	for _, r := range rejJava {
		buf.AppendLevel(console.LevelWarn, fmt.Sprintf("JVM-аргумент відхилено: %s — %s", r.Arg, r.Reason))
	}
	for _, r := range rejPack {
		buf.AppendLevel(console.LevelWarn, fmt.Sprintf("Аргумент збірки відхилено: %s — %s", r.Arg, r.Reason))
	}

	// Кожна (акаунт × збірка) — окрема сесія з власним Launcher: та сама
	// збірка може йти на кількох акаунтах незалежно, ізоляція повна.
	launcher := minecraft.NewLauncher()
	// Дефолтні налаштування гри: перед запуском застосовуємо їх до
	// options.txt збірки (одноразово, «файл гравця» не чіпаємо).
	a.applyMinecraftDefaultsBeforeLaunch(build.ID)
	// Скін збірки: застосовуємо пресет з Гардероба на акаунт ПЕРЕД стартом
	// гри (знімаємо снапшот для повернення). Помилка не блокує запуск —
	// лише попередження в консоль (пресет, що не застосувався, при
	// зупинці все одно коректно «поверне» поточний вигляд).
	if err := a.applyBuildSkinBeforeLaunch(build.ID, account, icfg); err != nil {
		buf.AppendLevel(console.LevelWarn, err.Error())
	}
	// Скіни приходять у гру через java-агент authlib-injector, який вказує
	// клієнту на Yggdrasil-API нашого worker'а. Хук викликається для акаунта
	// БУДЬ-ЯКОГО типу (офлайн / Microsoft / Шаурма) — єдиний вимикач це
	// налаштування DisableSkins. Хук повертає додаткові JVM-аргументи
	// (порожньо в base-збірці — див. offline_account_base.go; у shaurma —
	// wardrobe_inject.go + authlib_injector.go). Помилки не блокують
	// запуск — лише попередження в консоль збірки.
	extraJVMArgs := a.prepareOfflineAccount(account, build.ID, gameDir)
	a.wireSessionLauncher(launcher, account, build, buf, nil)
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
		// Аргументи редакції (authlib-injector для скінів офлайн-акаунтів)
		// окремим зрізом: шлях до jar'а чи base64-metadata не мають шансу
		// бути порізаними strings.Fields.
		ExtraJVMArgs: extraJVMArgs,
		// Аргументи запуску збірки (пише Pack Manager у packs-index.json):
		// JVM-подібні додаються до java-команди, решта — у гру.
		PackArgs: packArgs,
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

		// Дискретна графіка для гри (ноутбуки з двома GPU).
		DedicatedGPU: settings.UseDedicatedGPU,

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
		// Скін збірки вже встиг застосуватись (applyBuildSkinBeforeLaunch
		// виконується ДО launch) — а гра не стартувала (помилка spawn/
		// pre-launch). Повертаємо вигляд одразу, не чекаючи наступного
		// запуску лаунчера (снапшот би інакше висів до рестарту).
		// Асинхронно, щоб ретраї повернення не блокували emitLaunchError.
		{
			_f, _a0, _a1 := a.restoreBuildSkinAfterExit, build.ID, account.ID
			go func() {
				defer crashguard.Recover("a.restoreBuildSkinAfterExit")
				_f(_a0, _a1)
			}()
		}
		a.emitLaunchError(instanceID, err)
		return err
	}
	a.emitLaunchDone(instanceID)
	// Гра впевнено запустилась (Launch повернув nil і процес живий). Recheck
	// перераховує агрегований стан сесій: тепер IsRunning == true, тож
	// спрацьовує callback OnRunningStateChange(false→true) і пріоритет
	// процесу лаунчера опускається нижче гри (оптимізація запуску, план A).
	a.sessionsM.Recheck()
	// Зберігаємо PID гри на диск: якщо лаунчер уб'ють/закриють, а гра лишиться
	// жити, наступний запуск лаунчера відновить зв'язок (restoreRunningGame).
	a.saveRunningGame(build.ID, account.ID, launcher.PID())
	a.startGameMetrics(build.ID, launcher.PID())
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
		// StartedAt — момент старту сесії: фронтенд живе за ним таймер
		// «у сесії» на кнопці «Зупинити».
		StartedAt: time.Now().UnixMilli(),
	})
	// Discord Rich Presence: збірка запущена — показуємо її в присутності.
	if a.rpc != nil {
		a.rpc.AddBuild(discordrpc.BuildInfo{
			ID:        build.ID,
			Name:      build.Name,
			Loader:    build.Loader,
			MCVersion: build.MCVersion,
			StartTS:   time.Now().Unix(),
		})
	}
	// Друзі Shaurma: показуємо друзям, у якій збірці гравець (лише Shaurma-
	// редакція; у base-збірці це no-op). Робимо поруч із Discord RPC — те
	// саме місце, що вже знає про запуск гри, без окремого детектора.
	a.socialGameStarted(build.ID, build.Name, a.cfg.GetInstanceConfig(build.ID).Color)
	// showConsoleOnLaunch → авто-показ окремого вікна консолі САМЕ ЦІЄЇ
	// збірки (саме "вікна", не віджета). buildID передаємо явно: у режимі
	// кількох вікон консолі по збірках вікно має відкритись для тієї
	// збірки, що запускається.
	a.maybeAutoShowConsole("launch", build.ID)
	// Вікно лаунчера під час гри (gameLaunchAction): згорнути/сховати —
	// за потреби з затримкою. Єдине місце для будь-якого способу запуску
	// (кнопка «Грати», ярлик, сервер, трей).
	a.applyGameLaunchWindow()
	return nil
}

// wireSessionLauncher підвішує на окремий Launcher сесії хендлери виводу,
// виходу та ігрового часу. Лог записується у буфер ЗАВЖДИ (навіть якщо
// вікно консолі закрите) і одночасно йде подіями на фронтенд: звичайна
// console:line (для сумісності) + buildConsole:line з контекстом збірки/
// акаунта (для майбутньої «однієї консолі на збірку з перемикачем
// акаунтів»).
func (a *App) wireSessionLauncher(l *minecraft.Launcher, account model.Account, build *launchBuild, buf *console.Buffer, restored *runningGameEntry) {
	repairSessionStart := time.Now()
	if restored != nil && restored.SavedAt > 0 {
		repairSessionStart = time.Unix(restored.SavedAt, 0)
	}
	l.SetOutputHandler(func(line string) {
		if buf != nil {
			buf.Append(line)
			// «console:line» (живий потік для вікна/віджета) шлемо ЛИШЕ для
			// поточного буфера консолі — перемикання акаунтів не повинно
			// домішувати чужі логи у відображувану консоль. Скоупований
			// buildConsole:line йде завжди (для перемикача/архіву).
			//
			// ВАЖЛИВО: не a.emit() напряму — при великому потоці логів
			// (флуд/краш-дамп, сотні-тисячі рядків/сек) кожен a.emit це
			// окремий CGo-виклик + задача в черзі UI-потоку shell'а; черга
			// нічим не обмежена і росте швидше, ніж UI встигає її розібрати
			// — звідси «зависання» інтерфейсу і ріст пам'яті. emitConsoleLine/
			// emitBuildConsoleLine складають рядки в батчер (console_batch.go)
			// і шлють їх пачками (console:lines/buildConsole:lines), тому
			// кількість подій за секунду обмежена незалежно від того, скільки
			// пише гра.
			if buf == a.activeConsoleBuf() {
				a.emitConsoleLine(line)
			}
		}
		a.emitBuildConsoleLine(model.BuildConsoleLine{
			BuildID:   build.ID,
			AccountID: account.ID,
			Line:      line,
		})
	})
	l.SetExitHandler(func(code int) {
		// wasUserStop споживається РІВНО ОДИН раз на будь-якому виході
		// (Swap) — обчислюємо його ПЕРШИМ, до емісії подій, бо build:exit
		// тепер несе цей прапорець фронтенду (див. коментар при
		// model.BuildExitEvent.WasUserStop): без нього фронтенд бачив
		// лише ненульовий код виходу примусово вбитого процесу (звичайна
		// поведінка ОС для kill/taskkill, який і робить StopInstance) і
		// помилково показував «гра завершилась аварійно» навіть тоді,
		// коли гравець сам зупинив гру кнопкою в лаунчері.
		wasUserStop := a.stoppedByUser.Swap(false)
		a.lastExitWasUserStop.Store(wasUserStop)
		if buf != nil {
			lvl := console.LevelSystem
			if code != 0 && !wasUserStop {
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
		// Ігровий час ВІДНОВЛЕНОЇ сесії: у усиновленого процесу немає
		// власного onPlaytime (Launcher не бачив старту) — рахуємо від
		// збереженого моменту старту (SavedAt) і пишемо так само, як
		// звичайний playtime-хендлер нижче. Без цього час гри до
		// перезапуску лаунчера «губився» б з аналітики.
		if restored != nil && a.cfg.GetSettings().RecordGameTime {
			started := time.Unix(restored.SavedAt, 0)
			if restored.SavedAt <= 0 {
				started = time.Now()
			}
			if secs := int64(time.Since(started).Seconds()); secs > 0 {
				_ = a.cfg.AddPlaytime(restored.BuildID, secs)
				_ = a.cfg.AddPlaytimeSession(restored.BuildID, restored.AccountID, restored.AccountName, secs, started)
				a.scheduleCloudPlaytimeSync(15 * time.Second)
			}
		}
		a.stopGameMetrics(build.ID)
		a.onGameExitRepair(build, account, buf, code, wasUserStop, repairSessionStart)
		a.emit("game:exit", code)
		a.emit("build:exit", model.BuildExitEvent{
			BuildID:     build.ID,
			AccountID:   account.ID,
			Code:        code,
			WasUserStop: wasUserStop,
		})
		// Discord Rich Presence: збірка завершилась — прибираємо з присутності.
		if a.rpc != nil {
			a.rpc.RemoveBuild(build.ID)
		}
		a.socialGameExited(build.ID)
		a.sessionsM.Remove(account.ID, build.ID)
		a.stopLinkedServers(build.ID)
		// Гра завершилась (штатно/крашем/ручно) — прибираємо ЛИШЕ запис цієї
		// пари (build, account) зі списку запущених: інші паралельні сесії
		// (інші збірки і/або акаунти) лишаються в файлі недоторканими.
		a.clearRunningGame(build.ID, account.ID)
		// Скін збірки: повертаємо попередній скін/плащ акаунта. Асинхронно —
		// ретраї проти 429/5xx можуть зайняти до ~93с, не блокуємо горутину
		// exit-хендлера (guard у restoreBuildSkinAfterExit сам зупиниться,
		// якщо користувач одразу перезапустить ту саму збірку).
		{
			_f, _a0, _a1 := a.restoreBuildSkinAfterExit, build.ID, account.ID
			go func() {
				defer crashguard.Recover("a.restoreBuildSkinAfterExit")
				_f(_a0, _a1)
			}()
		}
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
		// Що робити з вікном після виходу з гри — рішення winbehavior
		// (gameExitAction + restoreOnCrash; не чіпає вікно, поки йде ще
		// якась паралельна гра). Раніше це робив фронтенд на game:exit
		// (restore/quit) плюс тут окрема гілка — тепер одне місце.
		a.applyGameExitWindow(code, wasUserStop)
		// Краш (ненульовий код) + showConsoleOnCrash → показати вікно
		// консолі. Штатний вихід
		// (код 0) + showConsoleOnClose → показати останні рядки логу.
		// Ручна зупинка (wasUserStop) НЕ вважається крашем.
		if code != 0 && !wasUserStop {
			a.maybeAutoShowConsole("crash", build.ID)
		} else if code == 0 {
			a.maybeAutoShowConsole("close", build.ID)
		}
	})
	// Ігровий час: записуємо тривалість сесії у playtime.json, якщо
	// увімкнено налаштування RecordGameTime. Разом з плоским лічильником
	// додаємо ЗАПИС СЕСІЇ (початок + тривалість) — з нього аналітика
	// рахує стовпчики «за 7 днів / тижні / місяці / роки» та середню сесію.
	l.SetPlaytimeHandler(func(instanceID string, seconds int64, startedAt time.Time) {
		if a.cfg.GetSettings().RecordGameTime && seconds > 0 {
			_ = a.cfg.AddPlaytime(instanceID, seconds)
			// Аналітика окремо по гравцях: сесія прив'язується до НІКА
			// (accountName) акаунта, активного на момент завершення гри —
			// нік переживає вихід з акаунта, дані не «губляться».
			acc := a.cfg.ActiveAccount()
			_ = a.cfg.AddPlaytimeSession(instanceID, acc.ID, acc.Username, seconds, startedAt)
			// Хмарне збереження (shaurma-редакція): загальний час по днях. З дебаунсом, тихо.
			a.scheduleCloudPlaytimeSync(15 * time.Second)
		}
	})
}

// ── Збереження PID гри (переживає перезапуск лаунчера) ──────────────────
// ТЗ: «якщо вбити трей і перезапустити лаунчер, він не має втратити
// зв'язок з грою і думати, що вона не запущена». Тому при успішному старті
// кожної гри дописуємо у <dataDir>/running-games.json запис (buildID,
// accountID, PID). Файл — СПИСОК: одночасно може йти скільки завгодно
// паралельних сесій (різні збірки і/або різні акаунти), кожна зі своїм
// записом. При старті лаунчера restoreRunningGame читає весь список і
// відновлює КОЖНУ живу сесію (не тільки одну) — жодна запущена гра не
// «губиться» після перезапуску лаунчера/трея. Запис конкретної пари
// (buildID, accountID) чиститься на build:exit саме цієї пари, решта
// записів лишаються недоторканими.

type runningGameEntry struct {
	BuildID   string `json:"buildId"`
	AccountID string `json:"accountId"`
	// AccountName — нік гравця, зафіксований на момент старту: для
	// аналітики ігрового часу відновленої сесії (нік переживає вихід
	// з акаунта, data не «губиться»).
	AccountName string `json:"accountName"`
	PID         int    `json:"pid"`
	SavedAt     int64  `json:"savedAt"`
}

// runningGamesPath — шлях до файлу стану ВСІХ запущених ігор.
func (a *App) runningGamesPath() string {
	return filepath.Join(a.cfg.Dir(), "running-games.json")
}

// legacyRunningGamePath — стара (одиничний-об'єкт) назва файлу, з якої
// мігруємо стан при першому старті після оновлення лаунчера.
func (a *App) legacyRunningGamePath() string {
	return filepath.Join(a.cfg.Dir(), "running-game.json")
}

// readRunningGamesLocked читає список запущених гри з диска. Викликач
// повинен тримати runningGamesMu. Якщо нового файлу немає, але є старий
// (одиничний-об'єкт) формат — мігрує його в список і одразу видаляє
// старий файл, щоб міграція відбулась рівно один раз.
func (a *App) readRunningGamesLocked() []runningGameEntry {
	data, err := os.ReadFile(a.runningGamesPath())
	if err == nil {
		var list []runningGameEntry
		if json.Unmarshal(data, &list) == nil {
			return list
		}
	}
	// Нового файлу немає/пошкоджений — спробувати стару одиничну форму.
	legacy, err := os.ReadFile(a.legacyRunningGamePath())
	if err != nil {
		return nil
	}
	var st runningGameEntry
	if json.Unmarshal(legacy, &st) != nil || st.BuildID == "" {
		os.Remove(a.legacyRunningGamePath())
		return nil
	}
	os.Remove(a.legacyRunningGamePath())
	return []runningGameEntry{st}
}

// writeRunningGamesLocked атомарно перезаписує файл стану. Викликач
// повинен тримати runningGamesMu.
func (a *App) writeRunningGamesLocked(list []runningGameEntry) {
	if len(list) == 0 {
		os.Remove(a.runningGamesPath())
		return
	}
	data, _ := json.Marshal(list)
	os.WriteFile(a.runningGamesPath(), data, 0644)
}

// saveRunningGame ДОДАЄ (або оновлює) запис для пари (buildID, accountID)
// у список — не зачіпає записи інших паралельних сесій.
func (a *App) saveRunningGame(buildID, accountID string, pid int) {
	a.runningGamesMu.Lock()
	defer a.runningGamesMu.Unlock()
	list := a.readRunningGamesLocked()
	out := make([]runningGameEntry, 0, len(list)+1)
	for _, e := range list {
		if e.BuildID != buildID || e.AccountID != accountID {
			out = append(out, e)
		}
	}
	accName := ""
	if acc, ok := a.cfg.GetAccountByID(accountID); ok {
		accName = acc.Username
	}
	out = append(out, runningGameEntry{BuildID: buildID, AccountID: accountID, AccountName: accName, PID: pid, SavedAt: time.Now().Unix()})
	a.writeRunningGamesLocked(out)
}

// clearRunningGame прибирає ЛИШЕ запис конкретної пари (buildID,
// accountID) — решта паралельних сесій лишаються в файлі недоторканими.
func (a *App) clearRunningGame(buildID, accountID string) {
	a.runningGamesMu.Lock()
	defer a.runningGamesMu.Unlock()
	list := a.readRunningGamesLocked()
	out := make([]runningGameEntry, 0, len(list))
	for _, e := range list {
		if e.BuildID != buildID || e.AccountID != accountID {
			out = append(out, e)
		}
	}
	a.writeRunningGamesLocked(out)
}

// restoreRunningGame намагається відновити стан «гра запущена» для КОЖНОЇ
// сесії зі списку після перезапуску лаунчера (див. коментар пакунка вище).
// Мертві PID (гра закінчилась, поки лаунчер був вимкнений) тихо
// прибираються без відновлення.
func (a *App) restoreRunningGame() {
	a.runningGamesMu.Lock()
	list := a.readRunningGamesLocked()
	a.runningGamesMu.Unlock()

	firstAccountSet := false
	for _, st := range list {
		if !isProcessAlive(st.PID) {
			a.clearRunningGame(st.BuildID, st.AccountID)
			continue
		}
		a.restoreOneRunningGame(st, &firstAccountSet)
	}
}

// restoredLaunchBuild повертає опис збірки для відновленої сесії. Збірку
// могли видалити/перейменувати, поки лаунчер був закритий — тоді
// повертаємо мінімальний опис лише з ID (гра все одно реально йде, і її
// консоль/зупинка мають працювати), а не nil, який обвалив би
// wireSessionLauncher на build.ID.
func (a *App) restoredLaunchBuild(st runningGameEntry) *launchBuild {
	if b, err := a.resolveLaunchBuild(st.BuildID); err == nil && b != nil {
		return b
	}
	return &launchBuild{ID: st.BuildID, Name: st.BuildID}
}

// restoredAccount повертає акаунт відновленої сесії. Акаунт могли
// видалити — тоді лишається мінімальний запис з ID/ніком зі збереженого
// стану, щоб wireSessionLauncher мав з чим працювати.
func (a *App) restoredAccount(st runningGameEntry) model.Account {
	if acc, ok := a.cfg.GetAccountByID(st.AccountID); ok {
		return acc
	}
	return model.Account{ID: st.AccountID, Username: st.AccountName}
}

// restoreOneRunningGame відновлює одну сесію зі списку. firstAccountSet
// гарантує, що активним акаунтом лаунчера стане лише ПЕРШИЙ відновлений
// живий процес (глобальний "активний акаунт" — не можна вибирати його на
// кожній ітерації, коли паралельно відновлюється кілька сесій різних
// акаунтів).
func (a *App) restoreOneRunningGame(st runningGameEntry, firstAccountSet *bool) {
	// Акаунт видалено або невідомий — все одно показуємо гру як запущену
	// (вона ж реально йде), але без прив'язки до акаунта. Сесію при цьому
	// ВСЕ ОДНО реєструємо (з невідомим AccountID): інакше менеджер сесій
	// не знав би про цю гру, і кнопка «Зупинити» / видалення збірки не
	// змогли б її зупинити — гра лишалась би «вічною» (баг: запустив
	// збірку → видалив → гра не зупиняється).
	accountKnown := true
	if _, exists := a.cfg.GetAccountByID(st.AccountID); st.AccountID == "" || !exists {
		accountKnown = false
	}
	launcher := minecraft.NewLauncher()
	// Буфер консолі для цієї (акаунт × збірка) пари — консоль після
	// перезапуску показує ту саму збірку як запущену.
	//
	// ПОРЯДОК ВАЖЛИВИЙ: спершу буфер + очищення + обробники виводу, і ЛИШЕ
	// ПОТІМ AdoptProcess. AdoptProcess одразу вичитує ІСТОРІЮ консольного
	// лог-файлу (усе, що гра написала до/під час відсутності лаунчера) в
	// onOutput; якщо обробник ще не підключено — історія піде в нікуди, і
	// консоль після перезапуску була б порожньою (це і був баг: «закрив
	// лаунчер повністю, відкрив — зв'язок з консоллю гри втрачено, логів
	// немає»).
	buf := a.consoleBufFor(st.AccountID, st.BuildID)
	// Буфер нового процесу лаунчера порожній, але на випадок повторного
	// виклику (напр. після SwitchConsoleAccount) чистимо, щоб історія з
	// файлу не задвоїлась.
	buf.Clear()
	buf.AppendLevel(console.LevelSystem, "Зв'язок з грою відновлено після перезапуску лаунчера.")
	restoredBuild := a.restoredLaunchBuild(st)
	a.wireSessionLauncher(launcher, a.restoredAccount(st), restoredBuild, buf, &st)
	// StartedAt = збережений час старту сесії (SavedAt, unix-сек): без
	// цього sessions.Start поставив би поточний час, і таймер «у сесії»
	// на кнопці «Зупинити» після рестарту лаунчера знову стартував би з нуля.
	startedAt := time.Unix(st.SavedAt, 0)
	if st.SavedAt == 0 {
		// Старий формат running-game.json міг не зберігати час — беремо
		// поточний, щоб StartedAt не дорівнював нулю (інакше таймер показує
		// 1970-й рік, а не час гри).
		startedAt = time.Now()
	}
	// Сесію реєструємо ДО AdoptProcess: AdoptProcess одразу запускає
	// стеження за процесом, і якщо гра вже встигла вмерти (JVM щойно
	// впала), onExit прибирає сесію через sessionsM.Remove. Зареєстрована
	// ПІЗНІШЕ, мертва сесія лишилась би в реєстрі назавжди (Remove
	// відпрацював би по порожньому місцю, а Start потім додав би привида).
	// sessionsM.Start безпечний до усиновлення: дублікат він відхиляє лише
	// якщо Launcher.IsRunning(), а до AdoptProcess він ще не running.
	sessErr := a.sessionsM.Start(&sessions.Session{
		AccountID:   st.AccountID,
		BuildID:     st.BuildID,
		AccountName: st.AccountName,
		BuildName:   restoredBuild.Name,
		Launcher:    launcher,
		StartedAt:   startedAt,
	})
	if !launcher.AdoptProcess(st.PID, a.gameDir(st.BuildID), isProcessAlive) {
		// Процес недоступний — відкочуємо реєстрацію сесії й стан.
		if sessErr == nil {
			a.sessionsM.Remove(st.AccountID, st.BuildID)
		}
		a.clearRunningGame(st.BuildID, st.AccountID)
		return
	}
	a.startGameMetrics(st.BuildID, st.PID)
	if sessErr == nil {
		a.lastInstanceID = st.BuildID
		a.activeInstanceID = st.BuildID
		// Активний акаунт лаунчера виставляємо лише для ПЕРШОЇ відновленої
		// сесії з РЕАЛЬНИМ акаунтом — решта паралельних сесій різних
		// акаунтів так само відновлюються (сесії/консоль/таймер живі),
		// просто не перемикають глобальний "активний акаунт" вдруге.
		if accountKnown && !*firstAccountSet {
			*firstAccountSet = true
			_ = a.cfg.SetActiveAccountID(st.AccountID)
		}
		// Для сесії БЕЗ акаунта фронтенд має побачити, що гра йде, — як це
		// робив старий шлях (події game:started/build:started без прив'язки
		// до акаунта). Для сесій з акаунтом події не дублюємо — фронтенд
		// отримує стан зі снапшота сесій.
		if !accountKnown {
			if a.rpc != nil {
				a.rpc.AddBuild(discordrpc.BuildInfo{ID: st.BuildID, StartTS: time.Now().Unix()})
			}
			a.socialGameStarted(st.BuildID, st.BuildID, a.cfg.GetInstanceConfig(st.BuildID).Color)
			a.emit("game:started", st.BuildID)
			a.emit("build:started", model.BuildStartEvent{
				BuildID:     st.BuildID,
				AccountID:   st.AccountID,
				AccountName: "",
				StartedAt:   st.SavedAt * 1000,
			})
		}
	}
	// Завершення відновленого процесу веде сам Launcher (AdoptProcess
	// поллить isProcessAlive, дочитує хвіст логу й викликає onExit), а
	// onExit — це той самий exit-хендлер wireSessionLauncher, що й у
	// звичайного запуску: код виходу в консоль, game:exit/build:exit,
	// прибирання сесії й запису running-games.json, повернення скіна,
	// авто-показ консолі. Власного дублюючого полінгу тут більше немає.
	// Ігровий час відновленої сесії (від збереженого SavedAt, а не від
	// моменту усиновлення) записує exit-хендлер wireSessionLauncher —
	// через параметр restored.
	// Відновлена сесія — теж з'являється в Discord-присутності.
	if a.rpc != nil {
		if b, err := a.resolveLaunchBuild(st.BuildID); err == nil {
			a.socialGameStarted(b.ID, b.Name, a.cfg.GetInstanceConfig(b.ID).Color)
			a.rpc.AddBuild(discordrpc.BuildInfo{ID: b.ID, Name: b.Name, Loader: b.Loader, MCVersion: b.MCVersion, StartTS: time.Now().Unix()})
		} else {
			a.rpc.AddBuild(discordrpc.BuildInfo{ID: st.BuildID, StartTS: time.Now().Unix()})
		}
	}
	a.emit("game:started", st.BuildID)
	// build:started зі збереженим часом старту сесії (SavedAt — unix-сек)
	// — фронтенд відновлює таймер «у сесії» без втрати вже прожитого часу
	// після перезапуску лаунчера.
	name := ""
	if acc, ok := a.cfg.GetAccountByID(st.AccountID); ok {
		name = acc.Username
	}
	a.emit("build:started", model.BuildStartEvent{
		BuildID:     st.BuildID,
		AccountID:   st.AccountID,
		AccountName: name,
		StartedAt:   st.SavedAt * 1000,
	})
}

// StopGame зупиняє "останню запущену" збірку на активному акаунті —
// застарілий шлях під одну гру на акаунт, лишений для сумісності (трей,
// та рідкі виклики без явного контексту конкретної збірки). Там, де
// відомий packId/buildId конкретної сторінки/картки — використовувати
// StopInstance, вона завжди влучає в потрібну сесію навіть коли на
// акаунті паралельно йде кілька збірок.
// CancelGameInstall — кнопка «Зупинити» під час встановлення компонентів
// гри (Minecraft jar, бібліотеки, асети, Java, лоадер): м'яко зупиняє
// поточне встановлення. Часткові файли (.part) лишаються на диску —
// наступний запуск докачає їх, а не почне з нуля. Після скасування
// LaunchInstance поверне ErrInstallCancelled, і гра просто не стартує
// (без пошкодження даних і без «напіввстановленого» стану).
//
// instanceID — ЯКУ збірку користувач хоче зупинити (з картки/сторінки,
// на якій клікнули). a.inst/a.javaInst ГЛОБАЛЬНІ на весь App (не
// заведений Installer на кожну збірку), тому без порівняння з
// activeInstallID цей виклик скасував би встановлення БУДЬ-ЯКОЇ збірки,
// яка зараз реально йде — навіть не тієї, з чиєї картки клікнули «Стоп».
// Реальний баг до цього фіксу: якщо збіги в часі (LaunchInstance
// дозволяє паралельний запуск різних збірок на одному акаунті), клік по
// іконці Java/лоадера на картці A міг обірвати встановлення картки B.
func (a *App) CancelGameInstall(instanceID string) {
	if instanceID == "" {
		return
	}
	active, _ := a.activeInstallID.Load().(string)
	if active != instanceID {
		// Ця збірка зараз НЕ власник глобального інсталера — реально
		// встановлюється щось інше. Мовчки нічого не робимо: краще
		// "Стоп" не спрацював на неактуальній картці (стан бекенду сам
		// оновить UI, коли реальне встановлення завершиться), ніж
		// випадково обірвати чужий, не той процес.
		return
	}
	// «Зупинити» під час встановлення компонентів: скасовуємо І встановлення
	// Minecraft (EnsureLoader/EnsureVersion), І качку Java — раніше Java
	// ігнорувала скасування, і «Стоп» під час качки Java нічого не робив.
	a.inst.RequestCancelInstall()
	a.javaInst.RequestCancelInstall()
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

// StopInstance зупиняє гру КОНКРЕТНОЇ збірки на активному акаунті. На
// відміну від StopGame (яка зупиняє «останню запущену» — застарілий підхід
// під одну гру на акаунт), ця функція завжди влучає в ту сесію, на яку
// користувач дивиться (кнопка «Стоп» на сторінці/картці конкретної
// збірки), навіть коли на тому самому акаунті паралельно йде кілька інших
// збірок — вони не зачіпаються.
func (a *App) StopInstance(instanceID string) error {
	if instanceID == "" {
		return fmt.Errorf("порожній instanceID")
	}
	a.stoppedByUser.Store(true)
	acc := a.cfg.ActiveAccount()
	if acc.ID == "" {
		a.stoppedByUser.Store(false)
		return fmt.Errorf("немає активного акаунта")
	}
	if err := a.sessionsM.Stop(acc.ID, instanceID); err != nil {
		a.stoppedByUser.Store(false)
		return err
	}
	return nil
}

// IsGameRunning — чи запущена будь-яка гра (на будь-якому акаунті).
func (a *App) IsGameRunning() bool { return a.sessionsM.AnyRunning() }

// WasLastExitUserStop — чи останній вихід гри (будь-якої збірки) стався
// через явну ручну зупинку гравцем (кнопка «Зупинити» / StopInstance), а
// не крах. Читає a.lastExitWasUserStop, яке оновлюється в exit-хендлері
// РАЗОМ з wasUserStop у model.BuildExitEvent (build:exit), але, на
// відміну від нього, НЕ прив'язане до buildId. Призначення — окреме
// плаваюче вікно консолі (packID=="", "слідує" за останньою активною
// грою): воно реагує на безадресну подію game:exit (просто код виходу,
// без buildId/wasUserStop у самій події — розширювати формат цієї події
// ризиковано, її вже читають кілька місць як просте число), тож у
// своєму обробнику game:exit викликає цей метод, щоб дізнатись, чи
// щойний вихід був ручною зупинкою, перш ніж показувати "гра завершилась
// аварійно" і запускати авто-аналіз крашу через AI.
func (a *App) WasLastExitUserStop() bool { return a.lastExitWasUserStop.Load() }

// HideToTray ховає вікно у системний трей. Викликається з window_behavior.go
// (gameLaunchAction=tray, minimizeAction=tray) і при закритті вікна (×)
// під час гри або за closeAction=tray. Повертається вікно через
// revealMainWindow (за gameExitAction, з трею або повторним запуском).
func (a *App) HideToTray() {
	a.hideToTray("frontend")
}

// hideToTray — ЄДИНЕ місце, що ховає головне вікно в трей. Кожне приховування
// лишає в лозі один рядок із причиною (раніше «лаунчер зник» не можна було
// пояснити).
func (a *App) hideToTray(reason string) {
	logHideToTray(reason)
	bridgeWindowOp("main", "hide")
	a.ensureTray()
	// Сховане вікно НЕ активне. WM_ACTIVATE(false) від shell не завжди
	// встигає/приходить при програмному hide(), а без цього прапорця
	// сповіщення чату вважали б, що користувач і так дивиться на лаунчер.
	setNotifyMainWindowActive(false)
	// Лаунчер згорнуто в трей — найкращий момент віддати ОС те, що
	// звільнилось під час останнього сканування/завантаження: далі ми не
	// працюємо з інтерфейсом, тож пік RSS backend.so тримати немає сенсу
	// (див. memtrim.go — раніше це робилось лише на окремі важкі операції).
	// ВАЖЛИВО: на Linux це звільняє лише Go-хіп/glibc-арени backend.so.
	// Окремий WebKitGTK-процес (webview) свою пам'ять цим НЕ звільняє —
	// win.Hide() лише ховає вікно, WebProcess лишається живим з усіма
	// буферами (детально — memtrim.go, шапка файлу).
	//
	// На Windows (де backend.dll живе В ОДНОМУ процесі з shell і нативним
	// WebView2) trimNowForHiddenWindow додатково віддає ОС робочі набори
	// нашого процесу і всіх msedgewebview2-нащадків — тобто в треї
	// лаунчер справді перестає тримати сотні мегабайт (memtrim_windows.go).
	trimNowForHiddenWindow()
}

// RestoreWindowAfterGame викликається фронтендом на подію game:exit.
// Якщо вікно було сховане у трей (гра йшла, лаунчер закрили), показує
// його назад — трей більше не потрібен, бо гра не запущена. Якщо
// вікно й так було видиме — просто no-op.
func (a *App) RestoreWindowAfterGame() {
	a.revealMainWindow()
}

// FocusMainWindow виводить головне вікно на передній план: показує (з трею),
// розгортає зі згорнутого стану і забирає фокус. Викликається фронтендом,
// коли лаунчер розпізнав посилання-запрошення в друзі, — модалка «Додати
// цього друга?» має з'явитись перед користувачем, а не під іншими вікнами.
func (a *App) FocusMainWindow() {
	// revealMainWindow (а не голі show/unminimise): скидає прапорець фонового
	// старту, щоб запізнілий UIReady не сховав вікно знову.
	a.revealMainWindow()
	bridgeWindowOp("main", "focus")
}

var (
	pendingFriendLink   string
	pendingFriendLinkMu sync.Mutex
)

// setPendingFriendLink запам'ятовує посилання-запрошення, відкрите через
// shaurma:// (останнє перемагає), поки фронтенд ще не підписаний на події.
func setPendingFriendLink(u string) {
	pendingFriendLinkMu.Lock()
	pendingFriendLink = u
	pendingFriendLinkMu.Unlock()
}

// TakePendingFriendLink повертає і очищає посилання-запрошення з shaurma://.
func (a *App) TakePendingFriendLink() string {
	pendingFriendLinkMu.Lock()
	defer pendingFriendLinkMu.Unlock()
	u := pendingFriendLink
	pendingFriendLink = ""
	return u
}

func (a *App) SearchContent(query string) ([]model.BrowserEntry, error) {
	return a.modrinth.Search(query, 20)
}

func (a *App) GetMods(instanceID string) ([]model.ModEntry, error) {
	return a.modMgr.ListMods(a.instanceModsDir(instanceID))
}

func (a *App) ToggleMod(instanceID, fileName string, enabled bool) error {
	if err := a.shareLockErr("build", instanceID); err != nil {
		return err
	}
	// Увімкнення/вимкнення мода = перейменування у .jar.disabled: Minecraft
	// вантажить моди лише при старті, тож це безпечно під час запущеної гри
	// (зміниться лише набір модів наступного запуску). Перевірки
	// errPackRunning немає — на відміну від DeleteMod/InstallModVersion,
	// які перезаписують/видаляють jar на диску.
	if err := a.modsLockedErr(instanceID, fileName); err != nil {
		return err
	}
	return a.modMgr.ToggleMod(a.instanceModsDir(instanceID), fileName, enabled)
}

func (a *App) GetSettings() model.Settings { return a.cfg.GetSettings() }

// GetPackLayout повертає збережений порядок і закріплення збірок з
// <instancesDir>/.build-layout.json (store.BuildLayout) — ОКРЕМИЙ файл,
// а не settings.json (docs/dev/DATA_ARCHITECTURE.md: «жодних центральних хабів,
// глобальний файл містить лише те, що спільне для всіх»). Фронтенд
// викликає це при старті замість localStorage.
//
// РЕГРЕСІЯ (виправлено): раніше GetPackLayout/SavePackLayout/
// GetPackFolders/SavePackFolders читали й писали s.PackOrder/PackPinned/
// PackFolders у settings.json, хоча store.GetBuildLayout()/
// SaveBuildLayout() (окремий файл .build-layout.json) уже існували й
// навіть заповнювались один раз при міграції з SQLite
// (migrateLayoutFromSettings) — але жоден наступний виклик після
// міграції в цей файл більше не писав, тож він одразу застарівав, а
// сортування/закріплення/папки продовжували жити в shaurma.json
// всупереч задокументованій архітектурі.
func (a *App) GetPackLayout() map[string]interface{} {
	layout := a.migrateLayoutFromSettingsOnce()
	order := layout.PackOrder
	pinned := layout.PackPinned
	if order == nil {
		order = []string{}
	}
	if pinned == nil {
		pinned = []string{}
	}
	return map[string]interface{}{"order": order, "pinned": pinned}
}

// SavePackLayout зберігає порядок і закріплення збірок у
// .build-layout.json. Викликається фронтендом після кожного
// перетягування або закріплення.
func (a *App) SavePackLayout(order []string, pinned []string) error {
	st := a.cfg.Store()
	if st == nil {
		return fmt.Errorf("сховище не ініціалізовано")
	}
	layout := st.GetBuildLayout()
	layout.PackOrder = order
	layout.PackPinned = pinned
	return st.SaveBuildLayout(layout)
}

// GetPackFolders повертає папки кастомних збірок з .build-layout.json
// (порожній масив, якщо ще не створювались).
func (a *App) GetPackFolders() []model.PackFolder {
	layout := a.migrateLayoutFromSettingsOnce()
	if layout.PackFolders == nil {
		return []model.PackFolder{}
	}
	return layout.PackFolders
}

// SavePackFolders зберігає папки кастомних збірок у .build-layout.json.
// Викликається фронтендом після кожної зміни (створення/перейменування/
// додавання збірки/видалення папки).
func (a *App) SavePackFolders(folders []model.PackFolder) error {
	if folders == nil {
		folders = []model.PackFolder{}
	}
	// Мінімальна валідація, щоб биті дані не сховали збірки з усіх списків:
	// унікальні ID папок, непусті назви, кожна збірка — не більше ніж в одній
	// папці (дублікати/порожні ID відсіваються).
	seen := map[string]bool{}
	seenPack := map[string]bool{}
	clean := make([]model.PackFolder, 0, len(folders))
	for _, f := range folders {
		name := strings.TrimSpace(f.Name)
		if name == "" || f.ID == "" || seen[f.ID] {
			continue
		}
		seen[f.ID] = true
		packs := make([]string, 0, len(f.PackIds))
		for _, p := range f.PackIds {
			if p == "" || seenPack[p] {
				continue
			}
			seenPack[p] = true
			packs = append(packs, p)
		}
		f.Name = name
		f.PackIds = packs
		clean = append(clean, f)
	}
	st := a.cfg.Store()
	if st == nil {
		return fmt.Errorf("сховище не ініціалізовано")
	}
	layout := st.GetBuildLayout()
	layout.PackFolders = clean
	return st.SaveBuildLayout(layout)
}

// GetServerFolders повертає папки серверів з .build-layout.json.
func (a *App) GetServerFolders() []model.ServerFolder {
	st := a.cfg.Store()
	if st == nil {
		return []model.ServerFolder{}
	}
	layout := st.GetBuildLayout()
	if layout.ServerFolders == nil {
		return []model.ServerFolder{}
	}
	return layout.ServerFolders
}

// SaveServerFolders зберігає папки серверів. Валідація як у SavePackFolders:
// унікальні ID папок, непусті назви, кожен сервер — максимум в одній папці.
func (a *App) SaveServerFolders(folders []model.ServerFolder) error {
	if folders == nil {
		folders = []model.ServerFolder{}
	}
	seen := map[string]bool{}
	seenSrv := map[string]bool{}
	clean := make([]model.ServerFolder, 0, len(folders))
	for _, f := range folders {
		name := strings.TrimSpace(f.Name)
		if name == "" || f.ID == "" || seen[f.ID] {
			continue
		}
		seen[f.ID] = true
		ids := make([]string, 0, len(f.ServerIds))
		for _, id := range f.ServerIds {
			if id == "" || seenSrv[id] {
				continue
			}
			seenSrv[id] = true
			ids = append(ids, id)
		}
		f.Name = name
		f.ServerIds = ids
		clean = append(clean, f)
	}
	st := a.cfg.Store()
	if st == nil {
		return fmt.Errorf("сховище не ініціалізовано")
	}
	layout := st.GetBuildLayout()
	layout.ServerFolders = clean
	return st.SaveBuildLayout(layout)
}

// ── «Останнє вікно» (сторінка, вкладка, відкрита збірка, скрол) ──────────
// Зберігається в .ui-state.json (instancesDir), НЕ в localStorage — див.
// store.UIState. Фронтенд викликає GetUIState() при старті замість
// читання localStorage, і SaveUIState() на кожну зміну сторінки/вкладки/
// скролу (debounced на боці фронтенду — див. queueSettingsSave/App.svelte).
func (a *App) GetUIState() store.UIState {
	st := a.cfg.Store()
	if st == nil {
		return store.UIState{}
	}
	return st.GetUIState()
}

func (a *App) SaveUIState(s store.UIState) error {
	st := a.cfg.Store()
	if st == nil {
		return fmt.Errorf("сховище не ініціалізовано")
	}
	return st.SaveUIState(s)
}

// migrateLayoutFromSettingsOnce — user-safety fallback: якщо
// .build-layout.json ще порожній (нового поля немає), а в settings.json
// (shaurma.json → об'єкт "settings") лишились packOrder/packPinned/
// packFolders з часів регресії (реальний приклад: shaurma.json з даними
// користувача мав ці поля ВСЕРЕДИНІ "settings", .build-layout.json був
// відсутній узагалі) — переносить їх у .build-layout.json ОДИН раз
// (перезаписуючи файл лише тоді, коли він справді був порожній) і чистить
// поля в settings.json, щоб дані не дублювались у двох місцях. Не чіпає
// .build-layout.json, якщо там уже щось є — цей файл уже джерело правди,
// settings.json міг відстати.
func (a *App) migrateLayoutFromSettingsOnce() store.BuildLayout {
	st := a.cfg.Store()
	if st == nil {
		// Немає сховища (dev/тести без ініціалізації) — працюємо з тим, що
		// встигло накопичитись у settings.json за поточну сесію, як фолбек.
		s := a.cfg.GetSettings()
		return store.BuildLayout{PackOrder: s.PackOrder, PackPinned: s.PackPinned, PackFolders: s.PackFolders}
	}
	layout := st.GetBuildLayout()
	if len(layout.PackOrder) > 0 || len(layout.PackPinned) > 0 || len(layout.PackFolders) > 0 {
		return layout
	}
	s := a.cfg.GetSettings()
	if len(s.PackOrder) == 0 && len(s.PackPinned) == 0 && len(s.PackFolders) == 0 {
		return layout
	}
	layout = store.BuildLayout{
		PackOrder:     s.PackOrder,
		PackPinned:    s.PackPinned,
		PackFolders:   s.PackFolders,
		ServerFolders: layout.ServerFolders, // папки серверів не з settings.json — не губимо
	}
	if err := st.SaveBuildLayout(layout); err != nil {
		log.Printf("migrateLayoutFromSettingsOnce: не вдалось зберегти .build-layout.json: %v", err)
		return store.BuildLayout{PackOrder: s.PackOrder, PackPinned: s.PackPinned, PackFolders: s.PackFolders}
	}
	// Чистимо settings.json — далі .build-layout.json єдине джерело правди,
	// лишати копію в двох місцях означало б два джерела правди, які можуть
	// розійтись знову тим самим шляхом.
	s.PackOrder = nil
	s.PackPinned = nil
	s.PackFolders = nil
	if err := a.cfg.UpdateSettings(s); err != nil {
		log.Printf("migrateLayoutFromSettingsOnce: не вдалось очистити settings.json: %v", err)
	}
	return layout
}

// SaveSettings зберігає налаштування і одразу застосовує ті, що впливають
// на поточні компоненти: ліміти завантажень (download-engine). Решта
// (акцент/тема/шрифт) застосовується фронтендом миттєво.
func (a *App) SaveSettings(s model.Settings) error {
	prevSettings := a.cfg.GetSettings()
	prevAllowAlphaBeta := prevSettings.AllowAlphaBetaModUpdates
	// Ручний статус для друзів («Відійшов» / «Невидимий») змінюється ТІЛЬКИ через
	// SetPresenceMode, а не через сторінку налаштувань. Фронтенд тримає знімок
	// settings, завантажений раніше, — без цього будь-яке збереження налаштувань
	// повертало б старий presenceMode і статус «відкочувався» до «В мережі».
	s.PresenceMode = prevSettings.PresenceMode
	// «Не турбувати» — частина того самого вибору статусу (SetPresenceMode): застарілий знімок
	// не повинен ні вмикати його назад, ні вимикати.
	s.NotifyDnd = prevSettings.NotifyDnd
	if err := a.cfg.UpdateSettings(s); err != nil {
		return err
	}
	// Прапорець alpha/beta оновлень модів змінився — старий кеш резолву
	// (за sha1) міг порахувати "hasUpdate" під попереднє значення, скидаємо.
	if s.AllowAlphaBetaModUpdates != prevAllowAlphaBeta {
		a.invalidateModResolveCache()
	}
	a.applyDownloadLimits()
	// Вимикач P2P застосовується одразу: вимкнено — слухач глушиться і
	// порт 45678 звільняється; увімкнено — піднімається назад. Без цього
	// зміна тумблера діяла б лише після перезапуску лаунчера.
	a.ApplyP2PToggle()
	// Сповіщення у трей: якщо користувач щойно вимкнув їх (або увімкнув
	// «Не турбувати»), вже показані картки скидаємо одразу — інакше
	// вимкнення виглядалося б так, ніби не спрацювало, а тост висів би
	// до кінця свого часу показу.
	a.notifyHideIfDisabled()
	// Після зміни налаштувань — оновлюємо ліміт буфера консолі.
	if a.consoleBuf != nil {
		a.consoleBuf.SetMaxLines(s.ConsoleMaxLines)
	}
	// Discord Rich Presence: зміна вимикача/текстів застосовується одразу.
	if a.rpc != nil {
		a.rpc.SetConfig(s.DiscordRPCEnabled, s.DiscordRPCDetails, s.DiscordRPCState)
	}
	// Канал оновлень (stable/beta): «Перевірити оновлення» в налаштуваннях
	// має одразу дивитись у той канал, який щойно обрав користувач, а не в
	// канал з попереднього старту лаунчера.
	a.updater.SetChannel(s.UpdateChannel)
	// Власний API-ключ CurseForge (порожній = вбудований ключ розробника).
	custompack.SetUserCurseForgeAPIKey(s.CurseForgeAPIKey)
	// Fallback на Modrinth для заблокованих модів: перереєструємо резолвер
	// з урахуванням поточного вимикача (nil резолвер = fallback вимкнено).
	a.applyModrinthBlockedFallback(s.UseModrinthForBlocked)
	// Автозапуск — застосовується одразу, без перезапуску лаунчера
	// (запис читається ОС при наступному вході в систему). Режим
	// «з вікном» ↔ «у фоні» міняє лише аргументи запису.
	a.syncAutostart()
	// Прапорці для shell (ui.json): GPU-прискорення і «економний режим» —
	// обидва впливають на те, як створюються вікна/WebKit-середовище, тож
	// потребують перезапуску. Файл оновлюється зараз, застосується при
	// наступному старті.
	a.writeUIFlagsFile(s)
	// Профіль памʼяті (GOGC) — єдина частина «економного режиму», яка
	// застосовується ОДРАЗУ, без перезапуску.
	a.applyMemoryProfile(s.LowMemoryMode)
	// Повідомляємо ВСІ вікна (окремі консолі гри/сервера — окремі документи
	// зі своїм <html>), що налаштування змінились: вони перечитують вигляд
	// (тема/акцент/шрифт) і перефарбовуються наживо, а не лишаються зі
	// старим виглядом до перевідкриття.
	a.emit("settings:saved")
	return nil
}
func (a *App) GetAppDir() string { return a.cfg.Dir() }

// SaveFolderPaths — залишено для сумісності зі старим wizard-флоу
// (finish() викликав його з instanceDir/javaPath). РІШЕННЯ: окремі папки
// збірок/Java більше не налаштовуються — вони завжди всередині папки
// даних лаунчера (baseDir/installations, baseDir/java), яку можна змінити
// цілком. Тому метод нічого не зберігає і повертає nil.
func (a *App) SaveFolderPaths(installations, javaPath string) error {
	return nil
}

// GetFolderPaths повертає поточні шляхи всіх тек лаунчера для вкладки
// «Теки та шляхи»: папка даних, installations, java, cache, logs, config.
// Усі підтеки — деривативи папки даних (окремих кастомних немає).
func (a *App) GetFolderPaths() model.FolderPaths {
	return a.cfg.FolderPaths()
}

// SetFolderPath змінює одну теку лаунчера за іменем (kind): baseDir —
// папку даних цілком (SetAppDir перемігрує всі дані і перебудовує
// компоненти); installations/java/cache/logs — кастомну теку користувача
// (перенесення даних + override у launcher-location.json).
//
// ВАЖЛИВО (ТЗ): теки НЕ можна міняти, поки запущена якась збірка —
// перенесення installations/minecraft/java під живою грою впало б на
// заблокованих файлах (JVM тримає java/, гра — installations/<id>/),
// а зміна теки збірок під грою «загубила» б активну збірку.
func (a *App) SetFolderPath(kind, path string) error {
	if err := a.assertFoldersChangeable(); err != nil {
		return err
	}
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
	return a.cfg.SetFolderPath(kind, path)
}

// assertFoldersChangeable — захист від зміни тек під час запущеної збірки.
// Повертає помилку, якщо будь-яка гра зараз активна: переносити/міняти
// теки в такому стані не можна (заблоковані файли + «загублена» збірка).

// syncFolderSettings підтягує актуальні шляхи збірок/Java у settings.json,
// щоб запуск гри використовував теки з нової папки даних.
func (a *App) syncFolderSettings() error {
	s := a.cfg.GetSettings()
	// Нічого не змінилось — не чіпаємо діск: UpdateSettings перезаписує ВЕСЬ
	// shaurma.json (+ ui.json), а це відбувалось на КОЖНОМУ старті лаунчера.
	if s.InstanceDir == a.cfg.InstancesDir() && s.JavaPath == a.cfg.JavaDir() {
		return nil
	}
	s.InstanceDir = a.cfg.InstancesDir()
	s.JavaPath = a.cfg.JavaDir()
	return a.cfg.UpdateSettings(s)
}

// SetAppDir змінює папку даних лаунчера (вибір у налаштуваннях/майстрі).
// ПЕРЕД зміною переносить УСІ дані зі старої папки даних у нову
// (installations, minecraft, java, cache, logs, skins, avatars) і видаляє
// стару теку — окремих кастомних папок підтек більше немає, усе живе
// всередині папки даних. Після перенесення перебудовує залежні компоненти
// (download-engine, minecraft-installer, java-installer), щоб вони качали
// й клали файли вже в нові теки.

// reconcileLegacyFolders — одноразова консолідація легасі-тек у папці
// даних .shaurm. УСЕ, що колись лежало у ВЛАСНИХ кастомних теках (старі
// settings.InstanceDir/JavaPath з попередньої версії лаунчера або override
// з launcher-location.json, напр. збірки у D:\ShaurmLauncher замість
// .shaurm\installations), переноситься ВСЕРЕДИНІ .shaurm — щоб уся тека
// даних відповідала єдиному шляху користувача і її можна було змінити
// цілком (SetAppDir). Після успіху settings скидаються на похідні .shaurm,
// а override прибираються.
//
// Fail-safe: якщо перенесення не вдалось (диск/права/заблоковані файли) —
// лишаємо легасі-шлях у дії (лаунчер продовжує ним користуватись, жодної
// втрати даних), консолідація повториться при наступному запуску.
// pathExists — чи існує шлях на диску (для консолідації легасі-тек).
func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func (a *App) reconcileLegacyFolders() {
	defInst := filepath.Join(a.cfg.Dir(), "installations")
	if inst := a.cfg.CustomInstancesDir(); inst != "" && !samePath(inst, defInst) && pathExists(inst) {
		if err := moveTree(inst, defInst); err != nil {
			log.Printf("reconcileLegacyFolders: перенесення збірок %s → %s: %v (лишаємо легасі-шлях)", inst, defInst, err)
		} else {
			s := a.cfg.GetSettings()
			s.InstanceDir = defInst
			_ = a.cfg.UpdateSettings(s)
		}
	}
	defJava := filepath.Join(a.cfg.Dir(), "java")
	if j := a.cfg.CustomJavaDir(); j != "" && !samePath(j, defJava) && pathExists(j) {
		if err := moveTree(j, defJava); err != nil {
			log.Printf("reconcileLegacyFolders: перенесення Java %s → %s: %v (лишаємо легасі-шлях)", j, defJava, err)
		} else {
			s := a.cfg.GetSettings()
			s.JavaPath = defJava
			_ = a.cfg.UpdateSettings(s)
		}
	}
	// Якщо обидва перенесення пройшли (або їх не було) — кастомні шляхи
	// більше не потрібні. При невдачі override лишається: лаунчер досі
	// користується легасі-текою (fail-safe).
	if !pathExists(a.cfg.CustomInstancesDir()) && !pathExists(a.cfg.CustomJavaDir()) {
		_ = a.cfg.ClearCustomDirs()
	}
}

// migrateDataDir переносить усі дані зі старої папки даних у нову при
// зміні папки лаунчера: installations (збірки), minecraft (версії/асети),
// java, cache, logs, skins (текстури гардеробу), fonts (шрифти лаунчера),
// skin-snapshots (знімки скінів збірок). Кожна підтека зливається
// рекурсивно (moveTree); після переносу стара папка даних прибирається.
//
// Кастомні підтеки користувача (installations/java/cache/logs, задані
// override у launcher-location.json або легасі settings) живуть ПОЗА
// папкою даних — тут вони не переносяться (вони й так на своєму місці),
// переносяться лише дефолтні підтеки всередині baseDir. Помилка переносу
// НЕ зупиняє решту підтек (moveTree повертає першу помилку, але
// продовжує) — користувач отримує зрозумілий опис.

// rewireComponents перестворює компоненти, чиї теки залежать від папки
// даних лаунчера, і перепідвішує прогрес-хендлери (вони єдині реєструються
// в startup, тому після SetAppDir це треба повторити).
func (a *App) rewireComponents() {
	a.dl = download.NewEngine()
	a.inst = minecraft.NewInstaller(filepath.Join(a.cfg.Dir(), "minecraft"))
	// Кеш binpatch-файлів Forge/NeoForge — у підтеці кешу лаунчера
	// (cache/forge), щоб жити з рештою даних і чиститися ClearCache,
	// а не у системному %TEMP% (може бути на іншому диску).
	a.inst.SetForgeCacheDir(filepath.Join(a.cfg.CacheDir(), "forge"))
	a.javaInst = java.NewInstaller(a.cfg.JavaDir())
	servers.SetLauncherJavaDir(a.cfg.JavaDir())
	a.srvAPI = servers.NewAPI(a.cfg.Dir(), a.javaInst, a.versionsProv)
	// P2P-лобі ("LAN через інтернет"): токен/base URL беремо лінькво через
	// update.Token() — той самий токен-gate, що й решта запитів лаунчера. База лобі
	// (/p2p/*) — вузол кластера (p2pLobbyBase), а не Worker: лобі переїхало на Postgres.
	a.p2pAPI = p2p.NewAPI(a.cfg.Dir(), update.Token, p2pLobbyBase)
	a.p2pAPI.SetAuth(a.p2pAuth)
	a.wireP2PEvents()
	// Живі логи консолі та зміни статусу серверів → події у фронтенд.
	// Без цього ServerConsolePanel підписаний на "server:line"/"server:status",
	// які ніхто не шле, і консоль лишається порожньою до ручного релоаду.
	a.srvAPI.Manager().SetCallbacks(
		func(serverID string, line model.ConsoleLine) {
			a.emit("server:line", map[string]any{
				"serverId": serverID,
				"id":       line.ID,
				"time":     line.Time,
				"level":    line.Level,
				"text":     line.Text,
			})
			a.remoteOnLine(serverID, line)
		},
		func(serverID string, status servers.ServerStatus) {
			a.emit("server:status", map[string]any{
				"serverId":  serverID,
				"status":    status.Status,
				"pid":       status.PID,
				"uptimeSec": status.UptimeSec,
				"exitCode":  status.ExitCode,
				"lastError": status.LastError,
				"recovered": status.Recovered,
			})
			a.remoteOnStatus(serverID, status)
			// P2P «разом із сервером» — на бекенді, незалежно від того, яка сторінка відкрита.
			switch status.Status {
			case "running", "stopped", "crashed", "error":
				a.P2PEnsureAuto(serverID)
			}
		},
	)
	// Структурований прогрес встановлення ядра → "server:install-progress".
	a.srvAPI.Manager().SetInstallProgressCallback(func(serverID string, p servers.ServerInstallProgress) {
		a.emit("server:install-progress", map[string]any{
			"serverId": serverID,
			"stage":    p.Stage,
			"percent":  p.Percent,
			"message":  p.Message,
		})
	})
	// Вхід/вихід гравця → "server:player". Фронтенд показує сповіщення з
	// ніком ("Steve приєднався") і тримає список онлайн-гравців.
	a.srvAPI.Manager().SetPlayerCallback(func(serverID string, ev servers.PlayerEvent) {
		a.emit("server:player", map[string]any{
			"serverId": serverID,
			"name":     ev.Name,
			"action":   ev.Action,
			"at":       ev.At,
			"count":    ev.Count,
			"reason":   ev.Reason,
			"list":     ev.List,
		})
	})
	// Прогрес створення бекапів світів → "server:backup-progress".
	// Раніше ручний бекап великого світу був "чорним ящиком" без жодного
	// зворотного зв'язку, а авто-бекап — узагалі невидимий.
	// Фонові ланцюги мережі Velocity (сервер не запустився разом із проксі
	// тощо) → "server:network-event".
	a.srvAPI.SetNetworkNotifyCallback(func(proxyID, serverID, message string) {
		a.emit("server:network-event", map[string]any{"proxyId": proxyID, "serverId": serverID, "message": message})
	})
	a.srvAPI.SetBackupProgressCallback(func(serverID string, p servers.BackupProgress) {
		a.emit("server:backup-progress", map[string]any{
			"serverId": serverID,
			"world":    p.World,
			"stage":    p.Stage,
			"files":    p.Files,
			"done":     p.Done,
			"percent":  p.Percent,
			"message":  p.Message,
			"path":     p.Path,
			"error":    p.Error,
			"auto":     p.Auto,
		})
	})
	// Краш лаунчера НЕ вбиває java-процеси серверів: після перезапуску
	// беремо під контроль ті, що лишились живими (runtime-запис на диску —
	// recover.go). Без цього власний сервер показувався як незапущений, а
	// спроба старту падала з «порт зайнятий», бо процес нікуди не зник.
	// Викликаємо ПІСЛЯ підключення колбеків, щоб UI одразу отримав
	// server:status для відновлених серверів.
	if ids := a.srvAPI.RecoverOrphanedServers(); len(ids) > 0 {
		log.Printf("recover: відновлено керування %d сервером(ами): %v", len(ids), ids)
	}
	a.avatars = avatar.NewService(filepath.Join(a.cfg.CacheDir(), "avatars"))
	// Персистентний кеш SHA1 модів (internal/mods/sha1cache_persist.go) —
	// теж прив'язаний до CacheDir, тож переносимо разом з рештою кешів
	// при зміні папки даних (SetAppDir), інакше modMgr продовжив би
	// писати за СТАРОЮ адресою після переїзду.
	a.modMgr.SetCacheDir(a.cfg.CacheDir())
	// Тека даних могла змінитись (SetAppDir) — avatar.Service пересоздано
	// заново вище, тож резолвер скінів Shaurma (якщо є — no-op у base)
	// треба підключити знову, інакше після rewire іконки Шаурма-акаунтів
	// відкотились би на звичайний Mojang-пошук до наступного рестарту.
	a.wireShaurmaAvatarResolver()
	// Реєстр збірок пересоздаємо: джерело правди — SQLite (builds_registry),
	// яка могла змінитись через SetAppDir; файловий фолбек — лише для тестів.
	a.buildsReg = a.newBuildsRegistry()
	a.buildsReg.Load()
	// custom-packs: instance.json — кожна збірка зберігає дані в собі.
	a.customPacks = a.newCustomPacksStore()
	// gallery-meta: SQLite (таблиця gallery_meta); фолбек — файл.
	a.gallery = a.newGalleryStore()
	// Гардероб: пресети в SQLite (wardrobe_presets), PNG текстури скінів —
	// у .shaurm/skins (постійна тека даних, а не кеш).
	a.wardrobe = a.newWardrobeService()

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

// ── Конструктори реєстрів (SQLite, з файловим фолбеком) ───────────────
// Використовуються в rewireComponents та при первинній ініціалізації.
// Якщо БД відкрита (cfg.Store() != nil) — реєстр читає/пише в SQLite;
// інакше — у файл (dev/тести).

func (a *App) newBuildsRegistry() *builds.Registry {
	if st := a.cfg.Store(); st != nil {
		r := builds.NewRegistry("")
		r.SetDBHooks(
			func() ([]builds.Entry, error) {
				rows, err := st.LoadBuildsRegistry()
				if err != nil {
					return nil, err
				}
				out := make([]builds.Entry, 0, len(rows))
				for _, e := range rows {
					out = append(out, builds.Entry{
						ID:               e.ID,
						Kind:             builds.Kind(e.Kind),
						InstalledVersion: e.InstalledVersion,
						InstalledAt:      time.Unix(e.InstalledAt, 0),
					})
				}
				return out, nil
			},
			func(list []builds.Entry) error {
				rows := make([]store.RegistryEntry, 0, len(list))
				for _, e := range list {
					rows = append(rows, store.RegistryEntry{
						ID:               e.ID,
						Kind:             string(e.Kind),
						InstalledVersion: e.InstalledVersion,
						InstalledAt:      e.InstalledAt.Unix(),
					})
				}
				return st.SaveBuildsRegistry(rows)
			},
			func(id string) error { return st.RemoveBuildRegistryEntry(id) },
		)
		return r
	}
	return builds.NewRegistry(filepath.Join(a.cfg.Dir(), "builds.json"))
}

func (a *App) newCustomPacksStore() *custompack.Store {
	return custompack.NewStore(a.cfg.InstancesDir())
}

func (a *App) newGalleryStore() *gallery.Store {
	if a.customPacks != nil {
		return gallery.NewStore(a.customPacks)
	}
	return gallery.NewStore(custompack.NewStore(a.cfg.InstancesDir()))
}

func (a *App) newWardrobeService() *wardrobe.Service {
	// PNG текстури пресетів гардеробу — постійні дані (не кеш), тому
	// живуть у .shaurm/skins, а не в cache/skins.
	if st := a.cfg.Store(); st != nil {
		return wardrobe.NewServiceDB(st, filepath.Join(a.cfg.Dir(), "skins"), a.refreshAccountForWardrobe)
	}
	return wardrobe.NewService(filepath.Join(a.cfg.Dir(), "skins"), a.refreshAccountForWardrobe)
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
	max := 10000
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
	// Панель завдань Windows: агрегований прогрес — найбільший % серед
	// активних завантажень (Shaurma-синк І таск-імпорт модпаків разом),
	// а не лише останньої події. Викликається під dlTransientMu.
	a.refreshTaskbarProgressLocked()
}

// refreshTaskbarProgressLocked перемальовує прогрес у панелі завдань за
// поточним станом УСІХ активних операцій: завантажень (dlTransient) і
// реєстру довгих операцій (activity_api.go) — показується найбільший %
// серед активних; якщо активних немає, але є призупинені — жовтий
// paused-бар; інакше прогрес зникає. Заодно планує розсилку
// activity:changed, щоб індикатор у верхній панелі бачив завантаження.
// Викликається ТІЛЬКИ під dlTransientMu.
func (a *App) refreshTaskbarProgressLocked() {
	a.drawTaskbarLocked()
	a.activityNotify()
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
	// Свіжовстановлена Java має одразу бачитись у списку серверів
	// (JavaInstalled) — інакше кеш FindJavaPath тримав би старий результат.
	servers.InvalidateJavaPathCache()
	return err
}

// GetSettingsSchema повертає декларативну схему налаштувань (див.
// internal/config/schema.go). Фронтенд рендерить сторінку «Налаштування»
// та майстер із цих даних — додавання нового параметра = один рядок у
// схемі, без дублювання у UI.
// GetSettingsSchema повертає схему налаштувань для UI (Налаштування + майстер).
// Окрім статичної схеми (config.SettingsSchema) додає шрифти з теки
// <dataDir>/fonts у відповідні селекти (font + усі шрифти консолі) —
// користувач кладе TTF/OTF у теку шрифтів, і вони з'являються у списках.
func (a *App) GetSettingsSchema() []model.SettingDef {
	schema := config.SettingsSchema()
	fonts := a.ListFonts()
	if len(fonts) > 0 {
		fontKeys := map[string]bool{
			"font": true, "consoleFontFamily": true,
			"consoleInfoFont": true, "consoleWarnFont": true, "consoleErrorFont": true,
		}
		for i := range schema {
			if schema[i].Type != "select" || !fontKeys[schema[i].Key] {
				continue
			}
			for _, f := range fonts {
				schema[i].Options = append(schema[i].Options, model.SettingOption{
					// Значення з префіксом file: — фронтенд розуміє, що це шлях
					// до файлу, а не вбудований шрифт (див. theme.svelte.ts).
					Value: "file:" + f.Path,
					Label: f.Name,
				})
			}
		}
	}
	return schema
}

// GetSystemInfo повертає інформацію про систему (CPU/RAM/диск) для
// кроку «Продуктивність» майстра та сторінки налаштувань.
func (a *App) GetSystemInfo() model.SystemInfo {
	return sysinfo.Collect(a.cfg.Dir())
}
func (a *App) GetAccounts() []model.Account {
	accounts := a.cfg.GetAccounts()
	// Позначаємо офлайн-акаунт, що зараз відповідає ніку активної
	// Google-сесії скін-системи, як «Шаурма-акаунт» (лише Shaurma-
	// редакція; у base-збірці хук — no-op, поле лишається false).
	a.markShaurmaSkinAccounts(accounts)
	return accounts
}

// GetVersion повертає поточну версію лаунчера для вкладки «Про лаунчер».
func (a *App) GetVersion() string {
	if a.updater != nil {
		return a.updater.CurrentVersion()
	}
	return appVersion
}

// GetPlatform повертає runtime.GOOS ("windows"/"linux"/"darwin") для
// фронтенду. Навмисно ОКРЕМО від GetSystemInfo: той збирає CPU/RAM/диск
// (важче, для вкладки «Про лаунчер» і майстра, ліниво) — тут лише дешевий
// runtime.GOOS, викликається одразу на старті (loadData), щоб UI міг
// підставити data-platform на <html> ДО першого рендеру важких елементів
// (backdrop-filter тощо), а не пост-фактум, коли вкладку «Про лаунчер»
// хтось відкриє.
func (a *App) GetPlatform() string {
	return runtime.GOOS
}

// GetPlaytime повертає записаний ігровий час (секунди на збірку) для
// вкладки «Ігровий час». Зберігається у playtime.json.
func (a *App) GetPlaytime() map[string]int64 {
	return a.cfg.GetPlaytime()
}

// GetAnalytics повертає повний знімок аналітики ігрового часу для розділу
// «Аналітика» у «Моїх збірках»: підсумки, стовпчики за останні 7 днів,
// серії тижнів/місяців/років та топ збірок. Дані — з таблиці сесій
// (playtime_sessions), тож статистика з'являється з моменту оновлення
// лаунчера (старі сумарні playtime.json до неї не входять).
//
// accountName — НІК гравця, чию аналітику показати (порожній = сумарно
// по всіх). Нік, а не ID акаунта: аналітика переживає вихід з акаунта.
func (a *App) GetAnalytics(accountName string) model.AnalyticsSummary {
	s, err := a.cfg.GetAnalytics(accountName)
	if err != nil {
		return model.AnalyticsSummary{}
	}
	return s
}

// GetStorageUsage підраховує зайняте місце по теках лаунчера для вкладки
// «Пам'ять і кеш»: installations, java, minecraft, cache, logs, avatars
// та решта в папці даних. Повертає також вільне місце на диску.
//
// ШВИДКІСТЬ: теки рахуються ПАРАЛЕЛЬНО (кожна своїм воркером), а результати
// кешуються на 45 секунд — повторне відкриття вкладки миттєве, поки нічого
// не змінилося. Старий послідовний обхід на багатогігабайтних теках міг
// висіти десятки секунд і «морозити» фронтенд (див. storageSizeCache).
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
		{"avatars", filepath.Join(a.cfg.CacheDir(), "avatars")},
	}
	info := model.StorageInfo{Entries: []model.StorageEntry{}}
	sizes := make([]int64, len(dirs))
	var wg sync.WaitGroup
	for i, d := range dirs {
		wg.Add(1)
		go func(idx int, name, path string) {
			defer crashguard.Recover("app.go:3551")
			defer wg.Done()
			sizes[idx] = cachedDirSize(path)
			// Живий прогрес (вкладка «Пам'ять і кеш»): UI показує, скільки
			// тек уже виміряно.
			a.emit("storage:usage-scan", map[string]any{"done": idx, "total": len(dirs), "label": name})
		}(i, d.name, d.path)
	}
	wg.Wait()
	for i, d := range dirs {
		info.Entries = append(info.Entries, model.StorageEntry{Name: d.name, Path: d.path, Bytes: sizes[i]})
		info.TotalBytes += sizes[i]
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
	// Спершу збираємо ВСТАНОВЛЕНІ збірки (для коректного done/total у
	// прогресі — раніше total рахував і невстановлені, і бар «доїжджав»
	// до 100% раніше, ніж вимірювались усі теки).
	type inst struct {
		id      string
		name    string
		iconURL string
		icon    string
		color   string
	}
	var list []inst
	for _, v := range a.GetBuilds() {
		if v.Installed {
			list = append(list, inst{id: v.Build.ID, name: v.Build.Name, iconURL: v.Build.IconURL, icon: v.Build.Icon, color: v.Build.Color})
		}
	}
	out := []model.StoragePackEntry{}
	sizes := make([]int64, len(list))
	var wg sync.WaitGroup
	for i, s := range list {
		dir := a.packDir(s.id, "")
		wg.Add(1)
		go func(idx int, s inst, dir string) {
			defer crashguard.Recover("app.go:3596")
			defer wg.Done()
			sizes[idx] = cachedDirSize(dir)
			// Прогрес обходу теки кожної збірки (та сама ідея, що в
			// GetStorageUsage): розміри великих збірок рахуються довго.
			a.emit("storage:packs-scan", map[string]any{"done": idx, "total": len(list), "label": s.name})
		}(i, s, dir)
	}
	wg.Wait()
	for i, s := range list {
		out = append(out, model.StoragePackEntry{
			ID:      s.id,
			Name:    s.name,
			Bytes:   sizes[i],
			IconURL: s.iconURL,
			Icon:    s.icon,
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
		root := a.packDir(v.Build.ID, string(v.Build.Kind))
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
	// Гарантуємо НЕ-nil слайси назовні: Go серіалізує nil-слайс у JSON як
	// null, а фронтенд у вкладці «Пам'ять і кеш» робить
	// unusedJava.installed.length / unusedJava.unused.length прямо в
	// шаблоні — null там кидав би TypeError "Cannot read properties of
	// null (reading 'length')" (розділ налаштувань переставав відкриватись
	// взагалі). Порівняй з тим самим захистом у ScanModsFull/модалках.
	info.Installed = []JavaMajorInfo{}
	info.Unused = []int{}
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
		// cachedDirSize (паралельний обхід + кеш 45с), а не dirSize
		// (однопотоковий обхід БЕЗ кешу): Java-тека може бути на сотні МБ,
		// і стара функція висіла на кожному мажорі по кілька секунд —
		// прогрес «зависав» на останньому кроці, а повторний аналіз
		// рахував ті самі теки заново.
		info.Installed = append(info.Installed, JavaMajorInfo{
			Major: d.major,
			Bytes: cachedDirSize(filepath.Join(a.cfg.JavaDir(), d.name)),
		})
	}
	// Потрібні мажори = Java, яку РЕАЛЬНО використовує кожна встановлена
	// збірка. Джерело істини — version.json профілю запуску збірки
	// (minecraft.EffectiveJavaMajor проходить увесь ланцюг InheritsFrom):
	// так аналіз бачить, що Forge 1.17.1 запускається саме на Java 16 (її
	// декларує vanilla-батько), а не «вгадує» 17 за номером версії MC.
	// Раніше тут був лише RecommendedMajor — тож Java 16 для 1.17.x
	// позначалася «невикористаною» і пропонувалась до видалення, хоча
	// збірка без неї не запускалась. Якщо version.json профілю немає на
	// диску або він не вказує javaVersion — падаємо на стару евристику.
	mcBase := filepath.Join(a.cfg.Dir(), "minecraft")
	needed := map[int]bool{}
	for _, v := range a.GetBuilds() {
		if !v.Installed {
			continue
		}
		m := 0
		if j, err := loadVersionJSON(mcBase, minecraft.LoaderVersionID(v.Build.Loader, v.Build.MCVersion, v.Build.LoaderVersion)); err == nil {
			m = minecraft.EffectiveJavaMajor(j, mcBase)
		}
		if m == 0 {
			if j, err := loadVersionJSON(mcBase, v.Build.MCVersion); err == nil {
				m = minecraft.EffectiveJavaMajor(j, mcBase)
			}
		}
		if m == 0 {
			m = java.RecommendedMajor(v.Build.MCVersion)
		}
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

// dirSize — звичайний послідовний підрахунок розміру теки (для точкових
// вимірів, де кеш не потрібен). Великі масиви теки в «Пам'ять і кеш»
// рахуються через cachedDirSize — паралельно і з кешем (див. storage_cleanup.go).
func dirSize(dir string) int64 {
	return dirSizeParallel(dir)
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

// StartMSLogin запускає спільний локальний callback-сервер (єдиний порт 29111, той самий,
// що й для входу через Google) і повертає URL для відкриття у системному браузері.
// Код авторизації захопиться автоматично — копіювати його вручну не треба.
func (a *App) StartMSLogin() (string, error) {
	// Shaurma-редакція: nonce комірки акаунта Шаурми кладемо прямо в цей вхід → один вхід у
	// Microsoft замість двох. База/без HWID повертає "" — вхід як раніше.
	return a.msAuth.StartLoginWithNonce(a.shaurmaLoginPrepare())
}

// AwaitMSLogin блокується, поки браузер не перенаправить код на локальний
// сервер, потім обмінює його на токени, завершує авторизацію і зберігає
// акаунт. Викликається фронтендом після StartMSLogin.
func (a *App) AwaitMSLogin() (SignInResult, error) {
	// Сесія спільного callback-сервера: через неї сторінка в браузері дізнається підсумок
	// («вхід» чи «реєстрація»). Беремо ДО AwaitLogin — після нього її вже може бути замінено.
	sess := a.msAuth.Session()
	account, err := a.msAuth.AwaitLogin()
	if err != nil {
		// Microsoft-вхід вдався, але Minecraft Java нема (або нема профілю Xbox): це НЕ збій —
		// пропонуємо неліцензійний акаунт Шаурми, прив'язаний до цього Microsoft.
		var nl *auth.NoLicenseError
		if errors.As(err, &nl) {
			r, nlErr := a.signInMicrosoftWithoutLicense(nl, sess)
			finishSignInPage(sess, r, nlErr)
			return r, nlErr
		}
		return SignInResult{}, err
	}
	// id_token живе лише тут, у пам'яті: на диск (конфіг) він не потрапляє (model.Account.MsIDToken
	// має json:"-"), а після реєстрації акаунта Шаурми забувається.
	idToken := account.MsIDToken
	account.MsIDToken = ""
	// Повторний вхід у ТОЙ САМИЙ Microsoft-акаунт: ID акаунта = UUID профілю,
	// тож пере-логін дає той самий ID. Оновлюємо запис замість того, щоб
	// дублювати його в списку (кнопка «Перезайти» в налаштуваннях акаунтів).
	var saveErr error
	if _, exists := a.cfg.GetAccountByID(account.ID); exists {
		saveErr = a.cfg.UpdateAccount(*account)
	} else {
		saveErr = a.cfg.AddAccount(*account)
	}
	// Новий/оновлений акаунт може мати доступ до приватних збірок, якого
	// не було в кеші списку — скидаємо кеш негайно, щоб гравець побачив
	// збірку одразу, без перезапуску лаунчера (замість очікування
	// packsIndexTTL).
	if saveErr == nil {
		a.invalidatePackCache()
		// Вхід у Microsoft = вхід у Шаурму: відновлюємо акаунт Шаурми цієї особи (новий ПК) або
		// створюємо його (id_token Шаурми — лише «паспорт», без Xbox/Minecraft/ліцензії).
		// Best-effort: збій соціального сервера не має валити вхід у гру.
		// Підсумок для сторінки в браузері (вхід / реєстрація) виставляє afterMicrosoftLogin.
		a.afterMicrosoftLogin(*account, idToken, sess)
	} else {
		finishSignInPage(sess, SignInResult{}, saveErr)
	}
	return SignInResult{Status: "signed_in", Provider: "microsoft", Nickname: account.Username, Licensed: true}, saveErr
}

// CancelMSLogin перериває очікування AwaitMSLogin (кнопка «Скасувати»).
func (a *App) CancelMSLogin() {
	a.msAuth.CancelLogin()
}

// OpenExternal відкриває URL у системному браузері користувача
// (а не у вбудованому вікні WebView2).
//
// ВАЖЛИВО: порожній/не-URL аргумент НЕ передається далі — інакше Windows
// відкриває Провідник на диску C (симптом «вхід в Microsoft-акаунт відкриває
// провідник»). Якщо StartMSLogin не зміг сформувати URL (порт зайнятий тощо),
// фронтенд отримує помилку через call() — сюди порожній рядок доходити не
// повинен, але захист має бути на кожному рівні.
func (a *App) OpenExternal(url string) {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return
	}
	bridgeBrowserOpenURL(url)
}

// OpenPackFolder відкриває теку встановленої збірки у системному файловому
// менеджері (Провідник Windows / файловий менеджер Linux). Якщо теки ще
// немає (збірку ще жодного разу не качали) — повертає зрозумілу помилку,
// фронт покаже її тостом.
func (a *App) OpenPackFolder(packID string) error {
	if packID == "" {
		return nil
	}
	// Теку збірки відкриваємо в корені installations/<packID>; якщо гра
	// вже встановлена — відкриваємо .minecraft (там реальні файли: моди,
	// конфіги, saves), як у старому лаунчері.
	packRoot := a.packDir(packID, "")
	dir := filepath.Join(packRoot, ".minecraft")
	if _, err := os.Stat(dir); err != nil {
		dir = packRoot
	}
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("теку збірки ще не створено — спершу завантажте збірку")
	}
	if runtime.GOOS == "windows" {
		return openPathInExplorer(dir)
	}
	return xdgOpen(dir)
}

// openPathInExplorer відкриває теку у системному файловому менеджері
// (Провідник Windows). Спільний хелпер для OpenPackFolder/OpenFontsFolder.
func openPathInExplorer(dir string) error {
	if runtime.GOOS == "windows" {
		return exec.Command("explorer", dir).Start()
	}
	return xdgOpen(dir)
}

// OpenInstancesFolder відкриває ТЕКУ ЗІ ВСІМА ЗБІРКАМИ (installations/) у
// системному файловому менеджері — кнопка «Тека зі збірками» у «Моїх
// збірках»: на відміну від OpenPackFolder (одна збірка), тут відкривається
// корінь, де лежать усі встановлені збірки лаунчера.
func (a *App) OpenInstancesFolder() error {
	dir := a.cfg.InstancesDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return openPathInExplorer(dir)
}

// OpenCacheFolder / OpenLogsFolder відкривають відповідні теки лаунчера
// (див. GetFolderPaths) у системному файловому менеджері — кнопки «Відкрити
// теку» у вкладці «Пам'ять і кеш».
func (a *App) OpenCacheFolder() error {
	dir := a.cfg.FolderPaths().Cache
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return openPathInExplorer(dir)
}

func (a *App) OpenLogsFolder() error {
	dir := a.cfg.FolderPaths().Logs
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return openPathInExplorer(dir)
}

// OpenFolder відкриває ДОВІЛЬНУ теку у системному файловому менеджері
// (для менеджера ярликів — «Відкрити теку» з ярликом; для інших
// довільних шляхів, що не є теками лаунчера). Безпечно: лише explorer.
func (a *App) OpenFolder(path string) error {
	if path == "" {
		return errors.New("порожній шлях")
	}
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		// Ярлик може лежати у теці, яку ще не створили (напр. обрана
		// довільна тека) — відкриваємо батька, а не помилкуємось.
		dir := filepath.Dir(path)
		return openPathInExplorer(dir)
	}
	return openPathInExplorer(path)
}

// ListInstanceWorlds повертає назви тек одиночних світів (saves/<world>) для
// збірки — використовується у списку вибору світу для автоприєднання
// («Гра» → «Автоприєднання» → «Одиночна гра»). Порожній список — не
// помилка: збірку могли ще не запускати або в ній ще немає жодного світу.
func (a *App) ListInstanceWorlds(packID string) []string {
	if packID == "" {
		return []string{}
	}
	savesDir := filepath.Join(a.gameDir(packID), "saves")
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

// ── Розділ "Сервери" на сторінці збірки ─────────────────────────────────
// Єдине джерело правди — servers.dat самої збірки (той самий файл, який
// редагує гра на екрані Multiplayer). Лаунчер лише читає/пише його та
// додатково зберігає "закріплені" IP окремо в InstanceConfig
// (PinnedServerIPs), бо гра поняття "закріпити" не має.

// serversDatPath — шлях до servers.dat конкретної збірки.
func (a *App) serversDatPath(packID string) string {
	return filepath.Join(a.gameDir(packID), "servers.dat")
}

// ensureOfficialServer додає офіційний сервер збірки Шаурма (serverIp з
// packs-index.json / manifest.json) у servers.dat збірки.
//
// Два режими:
//   - syncAlways=false (за замовчуванням): сервер додається лише коли файлу
//     ще немає або список порожній (див. minecraft.EnsureOfficialServer) —
//     список, який гравець наповнив, не чіпаємо.
//   - syncAlways=true (опція збірки officialServerSync): офіційний сервер
//     ЗАВЖДИ є у списку з актуальним IP із CDN — додається, якщо видалений,
//     і оновлюється, якщо IP на CDN змінився (minecraft.SyncOfficialServer).
//     Інші сервери гравця не чіпаються. Викликається при кожному запуску.
func (a *App) ensureOfficialServer(packID, serverName, serverIP string, syncAlways bool) {
	serverIP = strings.TrimSpace(serverIP)
	if serverIP == "" {
		return
	}
	path := a.serversDatPath(packID)
	if !syncAlways {
		if _, err := minecraft.EnsureOfficialServer(path, serverName, serverIP); err != nil {
			log.Printf("servers.dat: додавання офіційного сервера %s у збірку %s: %v", serverIP, packID, err)
		}
		return
	}

	icfg := a.cfg.GetInstanceConfig(packID)
	changed, oldIP, err := minecraft.SyncOfficialServer(path, serverName, serverIP, icfg.OfficialServerIP)
	if err != nil {
		log.Printf("servers.dat: оновлення офіційного сервера %s у збірці %s: %v", serverIP, packID, err)
		return
	}
	if changed {
		log.Printf("servers.dat: офіційний сервер збірки %s → %s (було %q)", packID, serverIP, oldIP)
		// Гравцю показуємо тост ЛИШЕ коли існуючий запис справді переїхав на
		// новий IP (oldIP != ""): коли запис просто додали/повернули, нічого
		// «оновлювати» немає. Дублів не буде — повторний виклик (після
		// синку й у LaunchInstance) уже не змінює файл.
		if oldIP != "" && !strings.EqualFold(strings.TrimSpace(oldIP), serverIP) {
			a.emit("server:ip-updated", map[string]string{
				"packId":   packID,
				"packName": serverName,
				"oldIp":    oldIP,
				"newIp":    serverIP,
			})
		}
	}
	// Запам'ятовуємо останній IP, який ми ставили, — за ним знайдемо СВІЙ
	// запис, коли IP на CDN знову зміниться. Закріплення (Pinned) переносимо
	// на нову адресу, як це робить UpdatePackServer.
	if icfg.OfficialServerIP != serverIP {
		for i, ip := range icfg.PinnedServerIPs {
			if oldIP != "" && ip == oldIP {
				icfg.PinnedServerIPs[i] = serverIP
			}
		}
		icfg.OfficialServerIP = serverIP
		_ = a.cfg.SaveInstanceConfig(icfg)
	}
}

// ListPackServers читає servers.dat збірки і повертає список у форматі
// фронтенду, з проставленим Pinned за збереженим InstanceConfig. Порожній
// результат — не помилка: servers.dat міг ще не створитись (збірку жодного
// разу не запускали / гра ще не заходила на Multiplayer).
func (a *App) ListPackServers(packID string) []model.InstanceServer {
	if packID == "" {
		return nil
	}
	entries, err := minecraft.ReadServersDat(a.serversDatPath(packID))
	if err != nil {
		return []model.InstanceServer{}
	}
	icfg := a.cfg.GetInstanceConfig(packID)
	pinned := make(map[string]bool, len(icfg.PinnedServerIPs))
	for _, ip := range icfg.PinnedServerIPs {
		pinned[ip] = true
	}
	// Тимчасові сервери (P2P-кімнати) — щоб UI показав їх окремою міткою:
	// людина має бачити, що цей рядок зникне сам, коли доступ закриють.
	temp := make(map[string]bool, len(icfg.TempServerIPs))
	for _, ip := range icfg.TempServerIPs {
		temp[ip] = true
	}
	out := make([]model.InstanceServer, 0, len(entries))
	for _, e := range entries {
		icon := ""
		if e.Icon != "" {
			icon = "data:image/png;base64," + e.Icon
		}
		out = append(out, model.InstanceServer{
			ID: "srv-" + e.IP, Name: e.Name, Address: e.IP, Icon: icon,
			Pinned: pinned[e.IP], Temp: temp[e.IP],
		})
	}
	return out
}

// AddTempPackServer додає сервер у список збірки ЯК ТИМЧАСОВИЙ: гравець
// приєднався до P2P-кімнати (співхост або запрошення) і хоче запускати гру
// прямо зі списку серверів збірки, а не вставляти адресу в «Пряме
// підключення» щоразу. Запис позначається в InstanceConfig.TempServerIPs,
// тож лаунчер прибирає його сам, щойно доступу не стане
// (App.SyncTempPackServers) — гравець не мусить чистити список руками.
func (a *App) AddTempPackServer(packID, name, address string) error {
	address = strings.TrimSpace(address)
	if address == "" {
		return fmt.Errorf("порожня адреса сервера")
	}
	if err := a.AddPackServer(packID, name, address); err != nil {
		return err
	}
	a.setTempPackServer(packID, address, true)
	return nil
}

// setTempPackServer додає/прибирає адресу у списку тимчасових серверів
// збірки (ідемпотентно).
func (a *App) setTempPackServer(packID, address string, add bool) {
	if packID == "" || address == "" {
		return
	}
	icfg := a.cfg.GetInstanceConfig(packID)
	out := make([]string, 0, len(icfg.TempServerIPs)+1)
	already := false
	for _, ip := range icfg.TempServerIPs {
		if ip == address {
			already = true
			if !add {
				continue
			}
		}
		out = append(out, ip)
	}
	if add && !already {
		out = append(out, address)
	}
	if !add && !already {
		return // нічого не змінилось
	}
	icfg.TempServerIPs = out
	_ = a.cfg.SaveInstanceConfig(icfg)
}

// TempPackServerAddresses — усі тимчасові адреси цієї збірки (для UI: щоб
// показати мітку «тимчасово» навіть там, де список приходить не з
// ListPackServers).
func (a *App) TempPackServerAddresses(packID string) []string {
	if packID == "" {
		return nil
	}
	icfg := a.cfg.GetInstanceConfig(packID)
	out := make([]string, 0, len(icfg.TempServerIPs))
	out = append(out, icfg.TempServerIPs...)
	return out
}

// ListPinnedServers збирає ВСІ закріплені сервери по всіх збірках (Шаурма
// + кастомні) для секції «Закріплені сервери» над картками збірок на
// сторінці «Мої збірки». Закріплення живе в InstanceConfig.PinnedServerIPs,
// назва/іконка сервера — у servers.dat тієї ж збірки. До кожної позиції
// додаємо метадані збірки (назва, колір, іконка, тип), щоб у UI було видно,
// з якої збірки цей сервер.
//
// Порядок результату — ЗБЕРЕЖЕНИЙ порядок користувача (Settings.PinnedServerOrder,
// drag&drop у секції), якщо він є; нові закріплені сервери (без запису в
// порядку) дописуються в кінець у природному порядку.
func (a *App) ListPinnedServers() []model.PinnedServerView {
	// Карта збірок за ID: назва/колір/іконка/тип.
	packMeta := make(map[string]builds.Build, 16)
	for _, v := range a.GetBuilds() {
		packMeta[v.Build.ID] = v.Build
	}

	// packIDs у стабільному (відсортованому) порядку: AllInstanceConfigs()
	// повертає map, а порядок ітерації по map у Go НЕ гарантований і
	// змінюється від виклику до виклику. Раніше цикл нижче йшов прямо
	// `for packID, icfg := range a.cfg.AllInstanceConfigs()`, тому список
	// закріплених серверів на «Моїх збірках» щоразу перебудовувався у
	// випадковому порядку — саме тому закріплений сервер "не піднімався
	// вгору стабільно": він міг опинитись першим, а міг і не першим,
	// незалежно від того, коли його закріпили.
	allCfgs := a.cfg.AllInstanceConfigs()
	packIDs := make([]string, 0, len(allCfgs))
	for packID := range allCfgs {
		packIDs = append(packIDs, packID)
	}
	sort.Strings(packIDs)

	out := []model.PinnedServerView{}
	for _, packID := range packIDs {
		icfg := allCfgs[packID]
		if len(icfg.PinnedServerIPs) == 0 {
			continue
		}
		entries, err := minecraft.ReadServersDat(a.serversDatPath(packID))
		if err != nil {
			continue
		}
		byIP := make(map[string]minecraft.ServerEntry, len(entries))
		for _, e := range entries {
			byIP[e.IP] = e
		}
		meta, ok := packMeta[packID]
		// Порядок серед серверів ОДНІЄЇ збірки — саме порядок закріплення
		// (PinnedServerIPs), а не порядок у servers.dat: користувач
		// закріплює сервери в певній послідовності і очікує бачити їх у
		// тій самій послідовності, а не в порядку, як вони лежать у
		// списку серверів гри.
		for _, ip := range icfg.PinnedServerIPs {
			e, found := byIP[ip]
			if !found {
				continue
			}
			icon := ""
			if e.Icon != "" {
				icon = "data:image/png;base64," + e.Icon
			}
			item := model.PinnedServerView{
				PackID:    packID,
				Address:   e.IP,
				Name:      e.Name,
				Icon:      icon,
				PackKind:  "custom",
				PackColor: meta.Color,
			}
			if ok {
				item.PackName = meta.Name
				item.PackColor = meta.Color
				item.PackIconURL = meta.IconURL
				item.PackIcon = meta.Icon
				if meta.Kind == builds.KindShaurma {
					item.PackKind = "shaurma"
				}
			} else {
				// Збірка видалена або недоступна — все одно показуємо сервер
				// (користувач зможе скопіювати адресу), але без метаданих.
				item.PackName = packID
			}
			out = append(out, item)
		}
	}

	// Застосовуємо ЗБЕРЕЖЕНИЙ порядок (Settings.PinnedServerOrder, ключі
	// "packId|address"). Сервери, яких у ньому немає (нові закріплення),
	// лишаються в кінці у своєму природному порядку.
	if order := a.cfg.GetSettings().PinnedServerOrder; len(order) > 0 {
		keyOf := func(v model.PinnedServerView) string { return v.PackID + "|" + v.Address }
		byKey := make(map[string]model.PinnedServerView, len(out))
		used := make(map[string]bool, len(out))
		sorted := make([]model.PinnedServerView, 0, len(out))
		for _, v := range out {
			byKey[keyOf(v)] = v
		}
		for _, key := range order {
			if v, ok := byKey[key]; ok && !used[key] {
				sorted = append(sorted, v)
				used[key] = true
			}
		}
		for _, v := range out {
			if !used[keyOf(v)] {
				sorted = append(sorted, v)
			}
		}
		out = sorted
	}
	return out
}

// SavePinnedServerOrder зберігає порядок закріплених серверів у секції
// «Закріплені сервери» (drag&drop, ключі "packId|address"). Викликається
// фронтендом після кожного перетягування — той самий підхід, що
// SavePackLayout для збірок.
func (a *App) SavePinnedServerOrder(keys []string) error {
	s := a.cfg.GetSettings()
	s.PinnedServerOrder = keys
	return a.cfg.UpdateSettings(s)
}

// AddPackServer додає новий сервер у КІНЕЦЬ servers.dat збірки. Якщо
// адреса вже присутня у файлі — оновлює лише назву наявного запису
// (не плодить дублікатів). packID без встановлених файлів — теку
// .minecraft створюємо самі (гра могла ще не запускатись).
func (a *App) AddPackServer(packID, name, address string) error {
	address = strings.TrimSpace(address)
	if address == "" {
		return fmt.Errorf("порожня адреса сервера")
	}
	path := a.serversDatPath(packID)
	entries, _ := minecraft.ReadServersDat(path) // помилка = файлу ще нема, це ок

	for i, e := range entries {
		if e.IP == address {
			entries[i].Name = firstNonEmpty(name, e.Name, address)
			return a.saveServersDat(path, entries)
		}
	}
	entries = append(entries, minecraft.ServerEntry{
		Name: firstNonEmpty(name, address), IP: address,
	})
	return a.saveServersDat(path, entries)
}

// UpdatePackServer редагує назву/адресу наявного сервера (за поточною
// адресою — унікальний ключ у servers.dat). Якщо адреса змінюється,
// закріплення (Pinned) переноситься на нову адресу автоматично.
func (a *App) UpdatePackServer(packID, oldAddress, newName, newAddress string) error {
	newAddress = strings.TrimSpace(newAddress)
	if newAddress == "" {
		return fmt.Errorf("порожня адреса сервера")
	}
	path := a.serversDatPath(packID)
	entries, err := minecraft.ReadServersDat(path)
	if err != nil {
		return fmt.Errorf("сервер не знайдено: %w", err)
	}
	found := false
	for i, e := range entries {
		if e.IP == oldAddress {
			entries[i].Name = newName
			entries[i].IP = newAddress
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("сервер з адресою %q не знайдено", oldAddress)
	}
	if oldAddress != newAddress {
		icfg := a.cfg.GetInstanceConfig(packID)
		for i, ip := range icfg.PinnedServerIPs {
			if ip == oldAddress {
				icfg.PinnedServerIPs[i] = newAddress
				_ = a.cfg.SaveInstanceConfig(icfg)
				break
			}
		}
	}
	return a.saveServersDat(path, entries)
}

// RemovePackServer видаляє сервер зі servers.dat за адресою і прибирає
// закріплення для неї, якщо було.
func (a *App) RemovePackServer(packID, address string) error {
	path := a.serversDatPath(packID)
	entries, err := minecraft.ReadServersDat(path)
	if err != nil {
		return fmt.Errorf("servers.dat не знайдено: %w", err)
	}
	out := entries[:0:0]
	for _, e := range entries {
		if e.IP != address {
			out = append(out, e)
		}
	}
	if err := a.saveServersDat(path, out); err != nil {
		return err
	}
	icfg := a.cfg.GetInstanceConfig(packID)
	newPinned := icfg.PinnedServerIPs[:0:0]
	for _, ip := range icfg.PinnedServerIPs {
		if ip != address {
			newPinned = append(newPinned, ip)
		}
	}
	icfg.PinnedServerIPs = newPinned
	return a.cfg.SaveInstanceConfig(icfg)
}

// ReorderPackServers перезаписує servers.dat у ПОРЯДКУ переданого списку
// адрес (drag&drop у списку серверів лаунчера). Адреси, що не входять у
// addresses (малоймовірно — розсинхрон з фронтендом), дописуються в кінець
// у своєму попередньому порядку, щоб жоден сервер не загубився.
func (a *App) ReorderPackServers(packID string, addresses []string) error {
	path := a.serversDatPath(packID)
	entries, err := minecraft.ReadServersDat(path)
	if err != nil {
		return fmt.Errorf("servers.dat не знайдено: %w", err)
	}
	byIP := make(map[string]minecraft.ServerEntry, len(entries))
	for _, e := range entries {
		byIP[e.IP] = e
	}
	used := make(map[string]bool, len(addresses))
	out := make([]minecraft.ServerEntry, 0, len(entries))
	for _, addr := range addresses {
		if e, ok := byIP[addr]; ok && !used[addr] {
			out = append(out, e)
			used[addr] = true
		}
	}
	for _, e := range entries {
		if !used[e.IP] {
			out = append(out, e)
		}
	}
	return a.saveServersDat(path, out)
}

// TogglePinPackServer перемикає закріплення сервера (UI-стан лаунчера,
// поза servers.dat — гра про закріплення не знає).
func (a *App) TogglePinPackServer(packID, address string) error {
	icfg := a.cfg.GetInstanceConfig(packID)
	pinned := false
	newList := make([]string, 0, len(icfg.PinnedServerIPs)+1)
	for _, ip := range icfg.PinnedServerIPs {
		if ip == address {
			pinned = true
			continue
		}
		newList = append(newList, ip)
	}
	if !pinned {
		newList = append(newList, address)
	}
	icfg.PinnedServerIPs = newList
	return a.cfg.SaveInstanceConfig(icfg)
}

// saveServersDat — гарантує наявність теки .minecraft перед записом
// (збірку могли ще не якати, коли користувач вручну додає сервер).
func (a *App) saveServersDat(path string, entries []minecraft.ServerEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("створення теки збірки: %w", err)
	}
	return minecraft.WriteServersDat(path, entries)
}

// CopyServerToPack копіює один сервер (за адресою) з поточної збірки в
// ІНШУ збірку (dstPackID). Якщо в цільовій збірці вже є сервер з такою
// адресою — оновлює лише назву (як і AddPackServer).
func (a *App) CopyServerToPack(srcPackID, address, dstPackID string) error {
	entries, err := minecraft.ReadServersDat(a.serversDatPath(srcPackID))
	if err != nil {
		return fmt.Errorf("вихідна збірка: %w", err)
	}
	for _, e := range entries {
		if e.IP == address {
			return a.AddPackServer(dstPackID, e.Name, e.IP)
		}
	}
	return fmt.Errorf("сервер з адресою %q не знайдено у вихідній збірці", address)
}

// ServerCopyResult — результат копіювання одного сервера в іншу збірку
// (батч CopyServersToPack). Exists = сервер уже був у цільовій збірці
// (назву оновлено, дублікатів не створено).
type ServerCopyResult struct {
	Address string `json:"address"`
	Name    string `json:"name,omitempty"`
	Exists  bool   `json:"exists"`
	Copied  bool   `json:"copied"`
	Error   string `json:"error,omitempty"`
}

// CopyServersToPack копіює КІЛЬКА серверів (за адресами) з вихідної збірки
// в іншу ОДНИМ викликом і повертає результат по кожній адресі. Адреси, яких
// немає у вихідній збірці, — Error. Якщо сервер уже є у цільовій — назва
// оновлюється (як AddPackServer), Exists=true. Файл цільової збірки
// перезаписується один раз, а не на кожну адресу (на відміну від циклу з
// CopyServerToPack), і порядок уже наявних записів зберігається.
func (a *App) CopyServersToPack(srcPackID string, addresses []string, dstPackID string) []ServerCopyResult {
	results := make([]ServerCopyResult, 0, len(addresses))
	if len(addresses) == 0 {
		return results
	}

	srcPath := a.serversDatPath(srcPackID)
	srcEntries, err := minecraft.ReadServersDat(srcPath)
	if err != nil {
		for _, addr := range addresses {
			results = append(results, ServerCopyResult{Address: addr, Error: "вихідна збірка: файл серверів не знайдено"})
		}
		return results
	}
	srcByIP := make(map[string]minecraft.ServerEntry, len(srcEntries))
	for _, e := range srcEntries {
		srcByIP[e.IP] = e
	}

	dstPath := a.serversDatPath(dstPackID)
	dstEntries, _ := minecraft.ReadServersDat(dstPath) // помилка = файлу ще нема, це ок

	changed := false
	for _, addr := range addresses {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			results = append(results, ServerCopyResult{Address: addr, Error: "порожня адреса"})
			continue
		}
		src, ok := srcByIP[addr]
		if !ok {
			results = append(results, ServerCopyResult{Address: addr, Error: "сервер не знайдено у вихідній збірці"})
			continue
		}
		exists := false
		found := false
		for i := range dstEntries {
			if dstEntries[i].IP == addr {
				dstEntries[i].Name = firstNonEmpty(src.Name, dstEntries[i].Name, addr)
				found = true
				exists = true
				break
			}
		}
		if !found {
			dstEntries = append(dstEntries, minecraft.ServerEntry{Name: firstNonEmpty(src.Name, addr), IP: addr})
		}
		changed = true
		results = append(results, ServerCopyResult{Address: addr, Name: src.Name, Copied: true, Exists: exists})
	}

	if changed {
		if err := a.saveServersDat(dstPath, dstEntries); err != nil {
			for i := range results {
				results[i].Copied = false
				results[i].Error = err.Error()
			}
		}
	}
	return results
}

// PingServer перевіряє доступність Minecraft-сервера (status ping).
func (a *App) PingServer(address string) minecraft.ServerPing {
	addr := strings.TrimSpace(address)
	if addr == "" {
		return minecraft.ServerPing{Online: false, Error: "порожня адреса"}
	}
	return minecraft.PingMinecraftServer(addr, 3*time.Second)
}

// PingServers пінгує КІЛЬКА серверів паралельно (кнопка "Пінг усіх" у
// розділі "Сервери") і повертає мапу адреса → результат. Обмеження
// паралелізму не потрібне: кожен пінг — коротке TCP-з'єднання з таймаутом
// 3с, а список серверів однієї збірки типово невеликий (одиниці-десятки).
func (a *App) PingServers(addresses []string) map[string]minecraft.ServerPing {
	out := make(map[string]minecraft.ServerPing, len(addresses))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, addr := range addresses {
		addr := strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer crashguard.Recover("app.go:4456")
			defer wg.Done()
			res := minecraft.PingMinecraftServer(addr, 3*time.Second)
			mu.Lock()
			out[addr] = res
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}

// LaunchInstanceOnServer запускає збірку і одразу приєднує гравця до
// вказаного сервера через --quickPlayMultiplayer — НЕЗАЛЕЖНО від
// збереженого автоприєднання у налаштуваннях збірки і незалежно від
// того, чи сервер зараз онлайн (гра сама покаже "Не вдалось підключитись",
// якщо сервер недоступний, так само як зі звичайного меню Multiplayer).
// Це кнопка "Грати" прямо в рядку сервера в розділі "Сервери".
func (a *App) LaunchInstanceOnServer(instanceID, address string) error {
	if err := a.shareLockErr("build", instanceID); err != nil {
		a.emitLaunchError(instanceID, err)
		return err
	}
	address = strings.TrimSpace(address)
	if address == "" {
		return fmt.Errorf("порожня адреса сервера")
	}
	a.launchServerOverrideMu.Lock()
	if a.launchServerOverride == nil {
		a.launchServerOverride = map[string]string{}
	}
	a.launchServerOverride[instanceID] = address
	a.launchServerOverrideMu.Unlock()
	defer func() {
		a.launchServerOverrideMu.Lock()
		delete(a.launchServerOverride, instanceID)
		a.launchServerOverrideMu.Unlock()
	}()
	return a.LaunchInstance(instanceID)
}

// SetLaunchClickAccount фіксує акаунт, активний на боці фронтенду САМЕ В
// МОМЕНТ кліку «Грати» — викликається СИНХРОННО перед LaunchInstance
// (App.svelte doLaunch), щоб зменшити вікно гонитви до мінімуму: між цим
// викликом і моментом, коли LaunchInstance доходить до читання акаунта,
// минають лічені мілісекунди Wails IPC, а не весь час перевірки/
// встановлення файлів збірки (секунди-хвилини), як було раніше, коли
// акаунт читався лише всередині самого LaunchInstance.
//
// instanceID+accountID — одноразові: LaunchInstance споживає й одразу
// видаляє запис (див. launchClickAccount), тож зависла пара тут не
// вплине на наступний, окремий запуск тієї самої збірки.
func (a *App) SetLaunchClickAccount(instanceID, accountID string) {
	if instanceID == "" || accountID == "" {
		return
	}
	a.launchClickAccountMu.Lock()
	if a.launchClickAccount == nil {
		a.launchClickAccount = map[string]string{}
	}
	a.launchClickAccount[instanceID] = accountID
	a.launchClickAccountMu.Unlock()
}

// LaunchInstanceShortcut запускає збірку з ЯРЛИКА з персональними опціями
// ярлика (як у Prism): accountID — з яким акаунтом, server — автоприєднання
// до сервера. Ці опції перекривають акаунт збірки/лаунчера та
// автоприєднання збірки РІВНО для цього запуску; решта параметрів
// (Java, RAM, моди, попередні перевірки) — як при звичайному запуску.
//
// Викликається фронтендом з launchFromShortcut, коли pending-запуск з
// ярлика має власні опції (TakePendingShortcutLaunch / подія shortcut:launch).
func (a *App) LaunchInstanceShortcut(instanceID, accountID, autoJoinServer string) error {
	if err := a.shareLockErr("build", instanceID); err != nil {
		a.emitLaunchError(instanceID, err)
		return err
	}
	if accountID != "" {
		a.launchAccountOverrideMu.Lock()
		if a.launchAccountOverride == nil {
			a.launchAccountOverride = map[string]string{}
		}
		a.launchAccountOverride[instanceID] = accountID
		a.launchAccountOverrideMu.Unlock()
		defer func() {
			a.launchAccountOverrideMu.Lock()
			delete(a.launchAccountOverride, instanceID)
			a.launchAccountOverrideMu.Unlock()
		}()
	}
	if autoJoinServer != "" {
		a.launchServerOverrideMu.Lock()
		if a.launchServerOverride == nil {
			a.launchServerOverride = map[string]string{}
		}
		a.launchServerOverride[instanceID] = autoJoinServer
		a.launchServerOverrideMu.Unlock()
		defer func() {
			a.launchServerOverrideMu.Lock()
			delete(a.launchServerOverride, instanceID)
			a.launchServerOverrideMu.Unlock()
		}()
	}
	return a.LaunchInstance(instanceID)
}

func (a *App) LoginPirate(username string) error {
	account, err := a.pirate.Login(username)
	if err != nil {
		return err
	}
	if err := a.cfg.AddAccount(*account); err != nil {
		return err
	}
	// Новий офлайн-акаунт може вже бути в acl.json (прямий нік/група) —
	// скидаємо кеш списку збірок, щоб приватна збірка з'явилась одразу.
	a.invalidatePackCache()
	return nil
}

// RemoveAccount видаляє акаунт. Скидаємо кеш списку збірок і тут: приватна
// збірка, доступна саме через ЦЕЙ акаунт, має зникнути зі списку одразу
// (а не чекати packsIndexTTL і показувати те, до чого вже немає доступу).
//
// БАГ (виправлено): раніше видалення офлайн-акаунта зі списку в лаунчері
// автоматично звільняло нік і на сервері скінів (releaseSkinNicknameIfMatches
// → DELETE /api/nickname → users.nickname = NULL), навіть коли причина
// видалення — просто «прибрати запис зі списку локально» (наприклад, щоб
// потім увійти під ним же наново). Це змішувало дві незалежні дії:
//  1. видалити ЛОКАЛЬНИЙ запис офлайн-акаунта в лаунчері (нешкідливо —
//     лаунчер просто «забуває» про нього, все на сервері лишається як є);
//  2. звільнити нік на сервері скінів (Google-акаунт втрачає прив'язку,
//     наступний вхід через ту саму пошту знову запитує вигадати нік) —
//     це має бути окрема, явна дія користувача («Вийти зі скін-акаунта»
//     в SkinAccountPanel вже саме так і працює — LogoutSkinAccount НЕ
//     чіпає нік на сервері, лише видаляє локальний JWT).
//
// Тому видалення акаунта зі списку більше НЕ звільняє нік на сервері —
// ця дія лишається виключно за явним запитом користувача (майбутня кнопка
// «Звільнити нік» у самому SkinAccountPanel, якщо вона знадобиться), а не
// за побічним ефектом видалення локального запису.
func (a *App) RemoveAccount(accountID string) error {
	acc, _ := a.cfg.GetAccountByID(accountID)
	if err := a.cfg.RemoveAccount(accountID); err != nil {
		return err
	}
	_ = acc // acc.Username більше не використовується тут (див. коментар вище)
	a.invalidatePackCache()
	// Ліцензійний акаунт зник — прибираємо його соціальну сесію; друзі
	// активного акаунта (він міг змінитись) перечитуються.
	a.socialForgetMS(acc)
	a.socialOnAccountChanged()
	return nil
}

// RenameOfflineAccount перейменовує ІСНУЮЧИЙ офлайн-акаунт: новий нік —
// це новий ID і новий офлайн-UUID (вони рахуються з ніка, як і при
// звичайному вході, generateOfflineUUID), тому "перейменування" технічно
// означає заміну запису акаунта на новий і перенесення всього, що було
// прив'язане до старого ID/ніка, на нові координати:
//
//  1. валідація нового ніка (ті самі правила, що при вході);
//  2. якщо новий нік зайнятий ІНШИМ локальним акаунтом — помилка одразу,
//     без жодного звернення до серверів;
//  3. якщо в цього офлайн-акаунта активна сесія скін-системи (Google) з
//     ніком, що збігається зі старим — пробуємо переназвати й там
//     (renameSkinNickname, no-op у base-редакції; rate-limit 2/тиждень
//     повертається як звичайна помилка, перейменування локально НЕ
//     відкочується — скін-нік можна поправити пізніше в «Скін-акаунті»);
//  4. заміна запису акаунта в accounts.json (той самий рядок, ReplaceAccountID);
//  5. перенесення прив'язок: AccountIDOverride у кастомних збірках і
//     пресети гардеробу (ключуються за ніком, не за ID).
//
// Повертає новий model.Account (з новим ID) — фронтенд повинен оновити
// activeAccountId на нього.
// RenameOfflineAccountResult — результат RenameOfflineAccount. Окреме
// поле SkinWarning (а не error) свідомо: локальне перейменування вже
// ГАРАНТОВАНО відбулось успішно, коли ми доходимо до формування
// результату — фронтенд має оновити activeAccountId на Account.ID у
// БУДЬ-ЯКОМУ разі. Якби ми повертали попередження про скін-нік як error,
// Wails на фронті трактував би весь виклик як провал і не застосував би
// вже виконане перейменування — акаунт і UI розійшлися б.
type RenameOfflineAccountResult struct {
	Account     model.Account `json:"account"`
	SkinWarning string        `json:"skinWarning,omitempty"`
}

// RenameOfflineAccount перейменовує ІСНУЮЧИЙ офлайн (Шаурма) акаунт: новий
// нік — це новий ID і новий офлайн-UUID (вони рахуються з ніка, як і при
// звичайному вході, generateOfflineUUID), тому "перейменування" технічно
// означає заміну запису акаунта на новий і перенесення всього, що було
// прив'язане до старого ID/ніка, на нові координати:
//
//  1. валідація нового ніка (ті самі правила, що при вході);
//  2. якщо новий нік зайнятий ІНШИМ локальним акаунтом — помилка одразу,
//     без жодного звернення до серверів;
//  3. якщо в цього акаунта активна сесія скін-системи (Google) з ніком,
//     що збігається зі старим — пробуємо переназвати й там
//     (renameSkinNicknameIfMatches, no-op у base-редакції; rate-limit
//     2/тиждень повертається окремим попередженням — перейменування
//     локально НЕ відкочується, скін-нік можна поправити пізніше в
//     «Скін-акаунті»);
//  4. заміна запису акаунта в accounts.json (той самий рядок, ReplaceAccountID);
//  5. перенесення прив'язок: AccountIDOverride у кастомних збірках і
//     пресети гардеробу (ключуються за ніком, не за ID).
//
// Помилка повертається ЛИШЕ якщо перейменування НЕ відбулось (акаунт не
// знайдено, невалідний нік, колізія з іншим локальним акаунтом). Якщо
// локальна частина вдалась, але синхронізація скін-ніка на сервері — ні
// (напр. rate-limit), це не error, а Result.SkinWarning: фронтенд все
// одно повинен прийняти новий Account.ID як активний.
func (a *App) RenameOfflineAccount(oldAccountID, newUsername string) (RenameOfflineAccountResult, error) {
	old, ok := a.cfg.GetAccountByID(oldAccountID)
	if !ok {
		return RenameOfflineAccountResult{}, fmt.Errorf("акаунт не знайдено")
	}
	if old.IsLicensed {
		return RenameOfflineAccountResult{}, fmt.Errorf("перейменування доступне лише для Шаурма-акаунтів")
	}
	newAccount, err := a.pirate.Login(newUsername) // валідація + генерація ID/UUID
	if err != nil {
		return RenameOfflineAccountResult{}, err
	}
	if newAccount.Username == old.Username {
		return RenameOfflineAccountResult{Account: old}, nil // нік не змінився — нічого робити не треба
	}
	for _, other := range a.cfg.GetAccounts() {
		if other.ID != old.ID && strings.EqualFold(other.Username, newAccount.Username) {
			return RenameOfflineAccountResult{}, fmt.Errorf("акаунт з ніком «%s» вже є в лаунчері", newAccount.Username)
		}
	}

	// Скін-система: переносимо нік ТАМ ЖЕ, де він зареєстрований — лише
	// якщо активна сесія прив'язана саме до старого ніка цього акаунта.
	// Помилку (напр. rate-limit 2 зміни/тиждень) повертаємо як попередження,
	// АЛЕ не блокуємо локальне перейменування: інакше людина не змогла б
	// виправити банальну помилку в ніку лише через те, що на сервері
	// скінів ліміт ще не минув.
	skinErr := a.renameSkinNicknameIfMatches(old.Username, newAccount.Username)

	if err := a.cfg.ReplaceAccountID(old.ID, *newAccount); err != nil {
		return RenameOfflineAccountResult{}, err
	}
	if a.customPacks != nil {
		_ = a.customPacks.RenameAccountRefs(old.ID, newAccount.ID)
	}
	if a.wardrobe != nil {
		_ = a.wardrobe.RenameAccountPresets(old.Username, newAccount.Username)
	}
	a.invalidatePackCache()

	result := RenameOfflineAccountResult{Account: *newAccount}
	if skinErr != nil {
		result.SkinWarning = fmt.Sprintf("нік акаунта змінено, але нік скін-акаунта — ні: %s (скін і надалі роздається за старим ніком; поправте вручну в «Скін-акаунт»)", skinErr.Error())
	}
	return result, nil
}

// ReorderAccounts змінює порядок акаунтів у списку (наприклад, у
// налаштуваннях — кнопки вгору/вниз). Активний акаунт не змінюється.
func (a *App) ReorderAccounts(ids []string) error { return a.cfg.ReorderAccounts(ids) }

// SetActiveAccount перемикає акаунт, який використовується для запуску
// гри (клік по акаунту у дропдауні сайдбара). Список Шаурма-збірок
// орієнтується на УСІ акаунти одразу (isAllowedAny на worker), тож
// перемикання активного акаунта саме по собі не змінює видимі приватні
// збірки — але кеш все одно скидаємо: це дешева операція, а користувач
// міг щойно додати акаунт і одразу перемкнутись, тож перестрахуємось.
func (a *App) SetActiveAccount(accountID string) error {
	if err := a.cfg.SetActiveAccountID(accountID); err != nil {
		return err
	}
	a.invalidatePackCache()
	// P2P вимагає Shaurma/ліцензійного акаунта, тож перемикання акаунта
	// переоцінює і гостьовий слухач ОДРАЗУ: увійшли підхожим акаунтом —
	// слухач піднімається; перемкнулись на офлайн — глушиться. Без цього
	// після входу довелось би перезапускати лаунчер заради P2P.
	a.ApplyP2PToggle()
	// Друзі й чати — окремі на кожен акаунт: перемикаємо особистість.
	a.socialOnAccountChanged()
	return nil
}

// ── Друзі Xbox Live (розділ «Друзі» на віджеті акаунта) ──────────────────
// Соціальні токени (XblToken/XblUHS) отримуються під час входу Microsoft
// (auth.authenticateSocialXSTS, релей http://xboxlive.com). Якщо їх немає
// (старий акаунт) або вони прострочені — пере-авторизуємо XBL→XSTS за
// MsAccessToken і оновлюємо запис у accounts.json.

// socialTokens повертає дійсну пару (token, uhs) соціального XSTS для
// активного ліцензійного акаунта. force=true примусово пере-авторизує.
//
// ВАЖЛИВО (баг «XBL error (401)»): 401 у XBL виникає, коли прострочений
// сам Microsoft access token (TTL ~1 год) — а RefreshSocial авторизує XBL
// САМЕ ним. Тому перед мінтом соціальних токенів обов'язково оновлюємо
// акаунт через refresh token (той самий шлях, що й запуск гри):
// RefreshTokens оновлює і MS access token, і Minecraft-токен, і (best-effort)
// соціальні XSTS. Лише якщо після цього XblToken досі порожній (соціальний
// XSTS не вийшов під час входу) — мінтимо явно через RefreshSocial.
func (a *App) socialTokens(force bool) (string, string, error) {
	acc := a.cfg.ActiveAccount()
	if !acc.IsLicensed {
		return "", "", fmt.Errorf("друзі доступні лише для ліцензійних акаунтів Microsoft")
	}
	// Збережені соціальні токени ще дійсні — повертаємо без мережі.
	if !force && acc.XblToken != "" && acc.XblUHS != "" {
		return acc.XblToken, acc.XblUHS, nil
	}
	if acc.RefreshToken == "" {
		return "", "", fmt.Errorf("немає Microsoft-токена — увійдіть в акаунт заново")
	}
	// Свіжий MS access token → свіжі XBL/XSTS. Якщо refresh не вийшов
	// (напр. refresh token відкликано) — показуємо зрозумілу помилку
	// замість сирого "XBL error (401)".
	refreshed, err := a.msAuth.RefreshTokens(acc.RefreshToken)
	if err != nil {
		return "", "", fmt.Errorf("Xbox соціальні токени недоступні: %w", err)
	}
	acc = *refreshed
	if acc.ID != "" {
		_ = a.cfg.UpdateAccount(acc)
	}
	// RefreshTokens (completeAuth) вже спробував отримати соціальний XSTS.
	if acc.XblToken != "" && acc.XblUHS != "" {
		return acc.XblToken, acc.XblUHS, nil
	}
	if acc.MsAccessToken == "" {
		return "", "", fmt.Errorf("немає Microsoft-токена — увійдіть в акаунт заново")
	}
	token, uhs, xuid, gt, err := a.msAuth.RefreshSocial(acc.MsAccessToken)
	if err != nil {
		return "", "", fmt.Errorf("Xbox соціальні токени недоступні: %w", err)
	}
	acc.XblToken, acc.XblUHS, acc.XboxUserID, acc.Gamertag = token, uhs, xuid, gt
	if acc.ID != "" {
		_ = a.cfg.UpdateAccount(acc)
	}
	return token, uhs, nil
}

// xboxAcceptLanguage перетворює мову UI ("uk_ua" / "en_us") на значення
// Accept-Language для соціальних Xbox API (People Hub відхиляє запити без
// цього заголовка — HTTP 400). Підкреслення → дефіс, порожнє → "en-US".
func xboxAcceptLanguage(lang string) string {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		return "en-US"
	}
	return strings.ReplaceAll(lang, "_", "-")
}

// GetXboxFriends повертає список друзів активного акаунта (People Hub) з
// присутністю. Присутність — другорядна: якщо вона впаде, друзі все одно
// повертаються (з presenceState з People Hub), просто без статусів/ігор.
func (a *App) GetXboxFriends() ([]model.XboxFriend, error) {
	a.xbox.SetLang(xboxAcceptLanguage(a.cfg.GetSettings().Language))
	token, uhs, err := a.socialTokens(false)
	if err != nil {
		return nil, err
	}
	people, err := a.xbox.People(token, uhs)
	if err != nil && strings.Contains(err.Error(), "401") {
		// Соціальний XSTS прострочений — пере-авторизуємось один раз.
		token, uhs, err = a.socialTokens(true)
		if err == nil {
			people, err = a.xbox.People(token, uhs)
		}
	}
	if err != nil {
		return nil, err
	}

	xuids := make([]string, 0, len(people))
	for _, p := range people {
		if p.XUID != "" {
			xuids = append(xuids, p.XUID)
		}
	}
	pres, err := a.xbox.Presence(token, uhs, xuids)
	if err != nil {
		pres = nil
	}
	presByXUID := map[string]xbox.PresenceInfo{}
	for _, p := range pres {
		presByXUID[p.XUID] = p
	}

	out := make([]model.XboxFriend, 0, len(people))
	for _, p := range people {
		f := model.XboxFriend{
			XUID:     p.XUID,
			Gamertag: p.Gamertag,
			Avatar:   p.DisplayPicRaw,
			State:    "offline",
		}
		if pr, ok := presByXUID[p.XUID]; ok {
			switch pr.State {
			case "Online", "Away":
				f.State = "online"
			}
			if pr.TitleID != 0 {
				f.State = "in-game"
				f.Title = pr.Title
				f.TitleID = pr.TitleID
				f.RichPresence = pr.RichPresence
				if xbox.IsMinecraft(pr.TitleID) {
					if addr := xbox.JoinAddress(pr.RichPresence); addr != "" {
						f.Joinable = true
						f.JoinAddress = addr
					}
				}
			}
		} else if p.PresenceState == "Online" || p.PresenceState == "Away" {
			f.State = "online"
			f.RichPresence = p.PresenceText
		}
		out = append(out, f)
	}
	return out, nil
}

// AddXboxFriend додає друга активного акаунта за gamertag: People Hub
// пошук знаходить гравця, Social API додає його в друзі. Повертає
// знайденого друга (щоб одразу оновити список без повторного запиту).
func (a *App) AddXboxFriend(gamertag string) (model.XboxFriend, error) {
	a.xbox.SetLang(xboxAcceptLanguage(a.cfg.GetSettings().Language))
	token, uhs, err := a.socialTokens(false)
	if err != nil {
		return model.XboxFriend{}, err
	}
	f, err := a.xbox.AddFriend(token, uhs, gamertag)
	if err != nil && strings.Contains(err.Error(), "401") {
		token, uhs, err = a.socialTokens(true)
		if err == nil {
			f, err = a.xbox.AddFriend(token, uhs, gamertag)
		}
	}
	if err != nil {
		return model.XboxFriend{}, err
	}
	return model.XboxFriend{
		XUID:     f.XUID,
		Gamertag: f.Gamertag,
		Avatar:   f.DisplayPicRaw,
		State:    "offline",
	}, nil
}

// GetXboxPrivacy повертає налаштування приватності Xbox + локальний
// перемикач сповіщень про онлайн.
func (a *App) GetXboxPrivacy() (*model.XboxPrivacy, error) {
	a.xbox.SetLang(xboxAcceptLanguage(a.cfg.GetSettings().Language))
	token, uhs, err := a.socialTokens(false)
	if err != nil {
		return nil, err
	}
	p, err := a.xbox.Privacy(token, uhs)
	if err != nil && strings.Contains(err.Error(), "401") {
		token, uhs, err = a.socialTokens(true)
		if err == nil {
			p, err = a.xbox.Privacy(token, uhs)
		}
	}
	if err != nil {
		return nil, err
	}
	return &model.XboxPrivacy{
		ShareFriendsList:    p.ShareFriendsList,
		AllowFriendRequests: p.AllowFriendRequests,
		OnlineNotify:        a.cfg.GetSettings().XboxOnlineNotify,
	}, nil
}

// SetXboxOnlineNotify зберігає локальний перемикач сповіщень про онлайн
// друзів (settings.json).
func (a *App) SetXboxOnlineNotify(enabled bool) error {
	s := a.cfg.GetSettings()
	s.XboxOnlineNotify = enabled
	return a.cfg.UpdateSettings(s)
}

// SetXboxPrivacy змінює налаштування приватності Xbox Live акаунта:
//   - shareFriendsList:    "Everyone" | "FriendsOnly" | "Blocked"
//   - allowFriendRequests: "Everyone" | "FriendsOnly" | "Blocked"
//
// Значення "" — не змінювати це поле.
func (a *App) SetXboxPrivacy(shareFriendsList, allowFriendRequests string) error {
	a.xbox.SetLang(xboxAcceptLanguage(a.cfg.GetSettings().Language))
	token, uhs, err := a.socialTokens(false)
	if err != nil {
		return err
	}
	settings := map[string]string{}
	if shareFriendsList != "" {
		settings["ShareFriendsList"] = shareFriendsList
	}
	if allowFriendRequests != "" {
		settings["AllowFriendRequests"] = allowFriendRequests
	}
	if len(settings) == 0 {
		return nil
	}
	err = a.xbox.SetPrivacy(token, uhs, settings)
	if err != nil && strings.Contains(err.Error(), "401") {
		token, uhs, err = a.socialTokens(true)
		if err == nil {
			err = a.xbox.SetPrivacy(token, uhs, settings)
		}
	}
	return err
}
func (a *App) CheckUpdate() (*update.UpdateInfo, error) {
	// Канал оновлень — ЗАВЖДИ зі свіжих налаштувань, а не з того, що
	// застосувалось при старті чи останньому SaveSettings: якщо settings.json
	// має "beta", а користувач натиснув «Перевірити оновлення» до зміни
	// налаштувань, чекер має дивитись саме в beta-канал (та сама логіка, що
	// в апдейтера — він читає settings.json напряму).
	a.updater.SetChannel(a.cfg.GetSettings().UpdateChannel)
	info, err := a.updater.Check()
	if err != nil {
		return nil, err
	}
	// Записуємо час ОСТАННЬОЇ перевірки у settings.json (вкладка
	// «Про лаунчер» показує «Остання перевірка: …»). Лише після успіху —
	// невдала спроба не обманює користувача «свіжою» датою.
	s := a.cfg.GetSettings()
	s.LastUpdateCheck = time.Now().Format(time.RFC3339)
	_ = a.cfg.UpdateSettings(s)
	return info, nil
}

// UpdateNow — РУЧНЕ оновлення лаунчера (кнопка «Оновити» в налаштуваннях).
// На відміну від автоматичного (апдейтер при старті поважає вимкнене
// autoUpdate), тут користувач ЯВНО натиснув «Оновити»: просимо shell
// перейти у фазу оновлення (run_updater), і shell завантажує updater.dll
// з finalize=true — апдейтер встановлює нову версію БЕЗУМОВНО, навіть
// якщо автооновлення вимкнене. Лаунчер сам завершиться/перезапуститься
// через апдейтер (той самий цикл, що при старті: splash → оновлення →
// backend), тож після цього виклику відповіді фронтенду немає сенсу
// чекати — повертаємо void.
//
// Повертає помилку, якщо run_updater недоступний (bridge старого
// формату/не ініціалізований) — фронтенд має показати «не вдалося
// запустити оновлення», а не залипати зі спінером назавжди.
func (a *App) UpdateNow() error {
	// Оновлення перезапускає лаунчер. Запущені сервери після цього
	// усиновлюються (recover.go) — але сама зупинка/старт серверів на час
	// апдейта не має залежати від таймінгу: просимо зупинити їх явно.
	if a.srvAPI != nil && a.srvAPI.HasRunningServers() {
		return fmt.Errorf("зупиніть запущені сервери перед оновленням лаунчера")
	}
	return bridgeRunUpdater()
}

func (a *App) GetJavaVersions() []string { return []string{"21", "17", "11", "8"} }
func (a *App) GetRAMOptions() []int      { return []int{1024, 2048, 3072, 4096, 6144, 8192, 12288, 16384} }

func (a *App) IsFirstRun() bool { return a.cfg.IsFirstRun() }

func (a *App) CompleteSetup(s model.Settings) error {
	if err := a.cfg.SetupComplete(s); err != nil {
		return err
	}
	if a.rpc != nil {
		a.rpc.SetConfig(s.DiscordRPCEnabled, s.DiscordRPCDetails, s.DiscordRPCState)
	}
	return nil
}

// GetWizardDefaults повертає дефолтні значення майстра першого запуску:
// тека збірок = папка даних/installations (завжди вказана за замовчуванням),
// Java = власна вбудована тека лаунчера папка даних/java (лаунчер сам
// поставить її, якщо відсутня), RAM — консервативна рекомендація
// autoGameRAM під фізичну пам'ять (6 ГБ → 2560 МБ, а не половина 3072:
// разом з грою живуть Windows + лаунчер, див. docs/dev/OPTIMIZATION_PLAN.md),
// акцент — помаранчевий. Фронтенд використовує це для першого кроку
// майстра, якщо збереженого прогресу ще немає.
func (a *App) GetWizardDefaults() model.SetupProgress {
	si := sysinfo.Collect(a.cfg.Dir())
	ram := autoGameRAM(si.TotalRAMMB)
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
	return bridgeDialogOpenFile("Виберіть теку для збірок", true, nil), nil
}

// BrowseFontFile відкриває діалог вибору кастомного шрифту (.ttf/.otf).
// Повертає повний шлях до файлу — зберігається в Settings.FontPath.
func (a *App) BrowseFontFile() (string, error) {
	return bridgeDialogOpenFile("Виберіть файл шрифту", false, map[string][]string{
		"Шрифти (*.ttf;*.otf)": {"ttf", "otf"},
	}), nil
}

// BrowseLanguageFile відкриває діалог вибору JSON-файлу мови (спільнотний
// переклад лаунчера). Повертає повний шлях до файлу — фронтенд читає його
// через LoadLanguageFile і реєструє як імпортовану мову.
func (a *App) BrowseLanguageFile() (string, error) {
	return bridgeDialogOpenFile("Виберіть файл мови (JSON)", false, map[string][]string{
		"Мови (*.json)": {"json"},
	}), nil
}

// LoadLanguageFile повертає ВМІСТ JSON-файлу мови як рядок. WebView не має
// доступу до довільних шляхів ФС, тому текст передається через бекенд.
// Повертає порожній рядок, якщо файл нечитабельний або більший за 5МБ
// (розумний ліміт для словника перекладів).
func (a *App) LoadLanguageFile(path string) string {
	if path == "" {
		return ""
	}
	st, err := os.Stat(path)
	if err != nil || st.Size() > 5*1024*1024 {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// FontFile — один шрифт із теки шрифтів лаунчера (dataDir/fonts).
// Фронтенд додає їх у випадаючі списки шрифтів (інтерфейс і консоль),
// щоб користувач міг обрати будь-який TTF/OTF, поклавши його в теку,
// без ручного вибору файлу через діалог.
type FontFile struct {
	Name string `json:"name"` // ім'я файлу (без розширення) — для списку
	Path string `json:"path"` // повний шлях — значення налаштування
}

// FontsDir повертає теку шрифтів лаунчера (<dataDir>/fonts), створюючи її
// за потреби. Саме сюди користувач кладе .ttf/.otf — і вони одразу
// з'являються у списках шрифтів.
func (a *App) FontsDir() string {
	// Шрифти — частина програми (встановлюються автоматично, як у
	// Modrinth: копіюються в теку програми при першому запуску), а не
	// дані користувача. Тому живуть у ConfigDir (папка встановлення exe),
	// а не в папці даних .shaurm (яка лишається при видаленні лаунчера).
	dir := filepath.Join(a.cfg.ConfigDir(), "fonts")
	os.MkdirAll(dir, 0755)
	return dir
}

// ListFonts сканує теку шрифтів лаунчера (dataDir/fonts) на .ttf/.otf/
// .woff/.woff2 і повертає список для UI. Порожня тека → порожній список
// (фронтенд просто не додає зайвих опцій).
func (a *App) ListFonts() []FontFile {
	dir := a.FontsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	exts := map[string]bool{".ttf": true, ".otf": true, ".woff": true, ".woff2": true}
	out := make([]FontFile, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if !exts[ext] {
			continue
		}
		out = append(out, FontFile{
			Name: strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())),
			Path: filepath.Join(dir, e.Name()),
		})
	}
	return out
}

// OpenFontsFolder відкриває теку шрифтів лаунчера у Провіднику — щоб
// користувач знав, куди класти TTF/OTF, і міг одразу туди перейти.
func (a *App) OpenFontsFolder() error {
	return openPathInExplorer(a.FontsDir())
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
				return model.Account{}, wrapRefreshErr("wardrobe: token refresh", err)
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
	acc := a.accountByID(accountID)
	return a.wardrobe.ListPresets(acc.Username)
}

// DeleteWardrobePreset видаляє пресет за ID в межах акаунта.
func (a *App) DeleteWardrobePreset(accountID, presetID string) error {
	acc := a.accountByID(accountID)
	return a.wardrobe.DeletePreset(acc.Username, presetID)
}

// ReorderWardrobePresets переставляє пресети акаунта у порядок, заданий
// повним списком presetIDs (drag&drop у гардеробі). Повертає оновлений
// список у новому порядку.
func (a *App) ReorderWardrobePresets(accountID string, presetIDs []string) []model.SkinPreset {
	acc := a.accountByID(accountID)
	return a.wardrobe.ReorderPresets(acc.Username, presetIDs)
}

// UpdateWardrobePreset оновлює існуючий пресет: назва, тип рук і плащ
// (кнопка «Зберегти» у редакторі пресету). Плащ за URL завантажується в
// локальний кеш і прив'язується до пресету, щоб картка одразу оновилась.
func (a *App) UpdateWardrobePreset(accountID, presetID, name string, slim bool, capeURL string) (model.SkinPreset, error) {
	acc := a.accountByID(accountID)
	return a.wardrobe.UpdatePreset(acc.Username, presetID, name, slim, capeURL)
}

// SaveWardrobeLocalSkin створює пресет із власноруч завантаженого PNG
// (кнопка "Додати скін" / drag&drop у діалозі редактора пресету).
// skinPNGBase64 — вміст файлу, закодований у base64 (без префіксу data:).
func (a *App) SaveWardrobeLocalSkin(accountID, name, skinPNGBase64 string, slim bool) (model.SkinPreset, error) {
	data, err := base64.StdEncoding.DecodeString(skinPNGBase64)
	if err != nil {
		return model.SkinPreset{}, fmt.Errorf("wardrobe: invalid skin data: %w", err)
	}
	acc := a.accountByID(accountID)
	return a.wardrobe.SaveLocalSkin(acc.Username, name, data, slim)
}

// UpdateWardrobePresetSkin оновлює СКІН існуючого пресету (кнопка «Обрати
// файл» у редакторі пресету, коли пресет уже є): новий PNG записується в
// кеш, а SkinPath/SlimArms оновлюються НА МІСЦІ — без створення нового
// пресету (баг: «редагую пресет і змінюю скін — створюється НОВИЙ пресет,
// а не змінюється скін цього пресету»).
func (a *App) UpdateWardrobePresetSkin(accountID, presetID, skinPNGBase64 string, slim bool) (model.SkinPreset, error) {
	data, err := base64.StdEncoding.DecodeString(skinPNGBase64)
	if err != nil {
		return model.SkinPreset{}, fmt.Errorf("wardrobe: invalid skin data: %w", err)
	}
	acc := a.accountByID(accountID)
	return a.wardrobe.UpdatePresetSkin(acc.Username, presetID, data, slim)
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
	acc := a.accountByID(accountID)
	return a.wardrobe.CopyPlayerSkin(acc.Username, profile, includeCape)
}

// ApplyWardrobePreset вивантажує скін пресету на вказаний ліцензійний
// акаунт (Mojang API), і намагається активувати плащ пресету, якщо він
// вже виданий акаунту. Після успіху скидає кеш аватарки акаунта і шле
// подію avatar:changed — картка профілю гравця одразу показує новий скін
// (без цього голова лишалась би старою до закінчення TTL кешу).
func (a *App) ApplyWardrobePreset(accountID, presetID string) error {
	// Офлайн-акаунт (обидві редакції): скін іде на сервер скінів під
	// Google-сесією, а не на Mojang. У base-збірці хук завжди handled=false.
	handled, err := a.applyPresetToSkinServer(accountID, presetID)
	if err != nil {
		return err
	}
	if !handled {
		if err := a.wardrobe.ApplyPreset(accountID, presetID); err != nil {
			return err
		}
	}
	// Скін змінився (на Mojang для ліцензійного акаунта, або на сервері
	// скінів Shaurma для офлайн-акаунта) — знаходимо UUID І нік акаунта,
	// чия аватарка кешується, і скидаємо ОБИДВА можливих записи кеша.
	//
	// БАГ (виправлено): раніше тут завжди передавався порожній username,
	// тому для офлайн/Shaurma-акаунтів чистився лише кеш під ключем uuid —
	// а headFromShaurmaSkin (internal/avatar) кешує такі голови під ключем
	// "shaurma_"+нік, який після ApplyWardrobePreset так і лишався
	// незмінним аж до TTL (1 година). Саме тому гравець і всі інші бачили
	// старий скін навіть після успішного оновлення на сервері.
	uuid, username := "", ""
	for _, acc := range a.cfg.GetAccounts() {
		if acc.ID == accountID {
			uuid = acc.UUID
			username = acc.Username
			break
		}
	}
	if a.avatars != nil && (uuid != "" || username != "") {
		a.avatars.Invalidate(uuid, username)
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
	// Офлайн-акаунт (обидві редакції): порівнюємо зі скіном на
	// нашому сервері скінів, а не на Mojang (там у такого акаунта нічого
	// немає). У base-збірці хук завжди handled=false.
	if id, handled, err := a.activeMatchingPresetFromSkinServer(accountID); handled {
		return id, err
	}
	return a.wardrobe.ActiveMatchingPresetID(accountID)
}

// GetWardrobeActiveLook повертає поточний вигляд акаунта (активний скін +
// плащ) як data URL. Для ліцензійних акаунтів — з Mojang; для офлайн-
// (Google/Шаурма) акаунтів — з нашого сервера скінів (лише Shaurma-
// редакція; у base-збірці хук завжди handled=false). Використовується
// гардеробом, щоб показати в 3D-переглядачі скін, який реально стоїть на
// акаунті, коли жоден пресет не збігається (як це робив старий лаунчер).
func (a *App) GetWardrobeActiveLook(accountID string) (*model.ActiveLook, error) {
	if look, handled, err := a.activeLookFromSkinServer(accountID); handled {
		return look, err
	}
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
