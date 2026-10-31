<script lang="ts">
  import UiModal from './lib/UiModal.svelte'
  import './lib/settings/settings.css'
  import './style.css'
  import { onMount, onDestroy } from 'svelte'
  import { fade, fly } from 'svelte/transition'
  import { flip } from 'svelte/animate'
  import { cubicOut, quintOut } from 'svelte/easing'
  import { t, setLanguage, registerCustomLanguage, removeCustomLanguage, getCustomLanguages, onLanguageChange, p2pRejectTexts } from './lib/i18n.svelte'
  import { pirateNicknameError } from './lib/validation'
  import { toBrowserServer, type BrowserServer } from './lib/servers/serverTargets'
  import { applyAccent, applyTheme, applyFont, applyAnimations, applyPerformanceMode, applyPlatform, loadConsoleFonts, consoleFontValues, fontFamilyFromPath, registerBundledFonts, BUNDLED_FONTS } from './lib/theme.svelte'
  import { App, on, Application, Window, type SignInResult, type DeviceChoice } from './lib/wails'
  import { flushAllPendingConfigs } from './lib/pendingConfigFlush'
  import { toast, confirmDialog } from './lib/toast.svelte'
  import type { Account, Settings, SyncProgress, BrowserEntry, BrowserVersionView, PackIndexEntry, SettingDef, StorageInfo, StoragePackEntry, StorageVersionsInfo, StorageSuggestion, StorageLibrariesInfo, StorageAssetsInfo, UnusedJavaInfo, UpdateInfo, FolderPaths, Build, BuildView, BuildProgress, BuildStartEvent, BuildExitEvent, BuildStatus, LaunchProgress, PreLaunchModIssues, LaunchWarnings, ServerPing, PinnedServerView, PackFolder, BlockedCFMod, SystemInfo, AnalyticsSummary, SkinPreset, DBHealth, AutoPackView, ServerPlayerEvent } from './lib/types'
  import { autoPackIcons } from './lib/autoPackIcons'
  import type { PackRow, PackFolderRow, PackPickerItem } from './lib/PackPicker.svelte'
  import PackPicker from './lib/PackPicker.svelte'
  import FolderViewModal from './lib/FolderViewModal.svelte'
  import FolderEditorModal from './lib/FolderEditorModal.svelte'
  import { safeFolderIcon } from './lib/folderMeta'
  import LauncherMigrationModal from './lib/LauncherMigrationModal.svelte'
  import ShortcutDialog from './lib/ShortcutDialog.svelte'
  import LaunchProgressOverlay from './lib/LaunchProgressOverlay.svelte'
  import Wizard from './lib/Wizard.svelte'
  import BirthdayMode from './lib/BirthdayMode.svelte'
  import Toasts from './lib/Toasts.svelte'
  import NewScreenshotWidget from './lib/NewScreenshotWidget.svelte'
  import { initShareStore, shareRecoverable, shareUi } from './lib/friends/shareStore.svelte'
  import ConfirmModal from './lib/ConfirmModal.svelte'
  import ErrorPanel from './lib/ErrorPanel.svelte'
  import PackInstancePage from './lib/PackInstancePage.svelte'
  import LaunchIntro from './lib/LaunchIntro.svelte'
  import CreatePackPage from './lib/CreatePackPage.svelte'
  import AnalyticsPanel from './lib/AnalyticsPanel.svelte'
  import FriendsPage from './lib/FriendsPage.svelte'
  import AddFriendModal from './lib/friends/AddFriendModal.svelte'
  import { parseFriendLink, friendLinkKey, type FriendLink } from './lib/friends/friendLink'
  import Wardrobe from './lib/Wardrobe.svelte'
  import PinnedServersSection from './lib/PinnedServersSection.svelte'
  import ContentBrowser from './lib/ContentBrowser.svelte'
  import ModpackPage from './lib/ModpackPage.svelte'
  import AuthorPage from './lib/AuthorPage.svelte'
  import ExportPackModal from './lib/ExportPackModal.svelte'
  import DuplicatePackModal from './lib/DuplicatePackModal.svelte'
  import Topbar from './lib/Topbar.svelte'
  import Sidebar from './lib/Sidebar.svelte'
  import BlockedModsDialog from './lib/BlockedModsDialog.svelte'
  import PreLaunchModsWarning from './lib/PreLaunchModsWarning.svelte'
  import NewsBanner from './lib/NewsBanner.svelte'
  import PackHealthCheck from './lib/PackHealthCheck.svelte'
  import RamWarningModal from './lib/RamWarningModal.svelte'
  import CrashModal from './lib/CrashModal.svelte'
  import AutoRepairDialog from './lib/AutoRepairDialog.svelte'
  import LoginModal from './lib/LoginModal.svelte'
  import SkinNickModal from './lib/SkinNickModal.svelte'
  import NickChoiceModal from './lib/NickChoiceModal.svelte'
  import DeviceLimitModal from './lib/DeviceLimitModal.svelte'
  import SkinOfferModal from './lib/SkinOfferModal.svelte'
  import SkinReloginModal from './lib/SkinReloginModal.svelte'
  import RenameAccountModal from './lib/RenameAccountModal.svelte'
  import GlobalSearchModal from './lib/GlobalSearchModal.svelte'
  import SettingsPage from './lib/SettingsPage.svelte'
  import ServersLibraryPage from './lib/servers/ServerLibraryPage.svelte'
  import ServerInstancePage from './lib/servers/ServerInstancePage.svelte'
  import ServerWizard from './lib/ServerWizard.svelte'
  import { grpTitleKeys, normalizeSectionId } from './lib/settingsMeta'
  import { searchSettings as searchSettingsIndex, labelKeyOf, revealSetting } from './lib/settings/registry'
  import { packAsset } from './lib/packAssets.svelte'
  import { orderMainBuildViews, orderFlatBuildViews, deriveServerFolders, mergeFolders } from './lib/packOrder'
  import AnimatedIcon from './lib/AnimatedIcon.svelte'
  import FolderTile from './lib/FolderTile.svelte'
  import { windowVisibility } from './lib/visible.svelte'
  import { windowActive as windowActiveState, initWindowActive } from './lib/windowActive.svelte'
  import { launchStageLabel, launchStageColor, launchStagePct, downloadStageLabel, downloadStageColor, downloadStageIcon, downloadStageGlow } from './lib/launchStages'
  import { getUIState, updateUIState, pushPackHistory, removePackState, resolveOpenPackId, readContentScroll, restoreContentScroll, setPackUI, initUIState, flushUIState } from './lib/uiState'
  import steveHead from './assets/images/steve-head.png'
  import fabricIcon from './assets/images/fabric_icon.png'
  import forgeIcon from './assets/images/forge_icon.png'
  import neoforgeIcon from './assets/images/neoforge_icon.png'
  import quiltIcon from './assets/images/quilt_icon.png'

  let currentPage = $state('instances')
  // ── Серверний розділ (завжди доступний у sidebar) ──
  let openServerId = $state('')
  // Сторінка сервера друга: сам сервер береться зі спільного списку remote.servers (живий статус і права).
  let openRemote = $state<{ ownerId: string; serverId: string } | null>(null)
  let openServerAutoStart = $state(false)
  let showServerWizard = $state(false)
  // Стиснутий sidebar (лише іконки). Розгорнутий має РЕГУЛЬОВАНУ ширину
  // (sidebarWidth, від 220 до 400px — перетягуванням правого краю) і
  // масштаб елементів (sidebarScale, 0.8–1.25 — повзунок у «Вигляд»). Обидва
  // значення зберігаються у БД через settings (див. derived нижче) — на
  // відміну від старого localStorage, вони переживають перезапуск лаунчера.
  let sidebarCollapsed = $state(false)
  let sbResizing = $state(false)
  let accDropdownOpen = $state(false)
  let settingsTab = $state('general')
  let instanceTab = $state('shaurma')

  let accounts = $state<Account[]>([])
  // Стан мережі (network:status з бекенду): true = інтернет є. Коли false —
  // позначка «офлайн» під назвою лаунчера у sidebar. Стартує як true:
  // перша подія (або App.CheckNetwork на маунті) виставить реальний стан.
  let networkOnline = $state(true)
  // Активний (обраний для гри) акаунт — зберігається у налаштуваннях.
  let activeAccountId = $state('')
  let settings = $state<Settings>({
    language: 'uk', accent: 'orange', accentCustom: '#ff8a00', theme: 'dark', font: 'inter', fontPath: '',
    animationsEnabled: true,
    sidebarWidth: 220, sidebarScale: 1, sidebarOnlyPinned: false,
    themeCustomBg: '#0f0f0f', themeCustomCard: '#161616', themeCustomText: '#ffffff',
    autoUpdate: true, updateChannel: 'stable',
    startupMode: 'window', closeAction: 'quit', minimizeAction: 'taskbar', gameLaunchAction: 'keep', gameLaunchDelaySec: 0, gameExitAction: 'restore', restoreOnCrash: true,
    showConsole: false, saveLogs: true,
    instanceDir: '', javaPath: '', maxRAM: 4096, javaArgs: '', activeAccountId: '',
    consoleMaxLines: 10000, consoleInfoColor: '#8fd3ff', consoleWarnColor: '#ffd166', consoleErrorColor: '#ff6b6b', consoleWrap: false, consoleFontSize: 12, consoleFontFamily: 'mono',
    consoleFontMode: 'all', consoleInfoFont: 'mono', consoleWarnFont: 'mono', consoleErrorFont: 'mono', consoleFontPath: '',
    consoleAI: true, consoleAIKey: '', showConsoleOnLaunch: false, showConsoleOnCrash: true, showConsoleOnClose: false, disableAutoRepair: false,
    maxConcurrentDownloads: 10, maxRetries: 6, httpTimeoutSec: 60,
    preLaunchCommand: '', wrapperCommand: '', postExitCommand: '', envVars: '',
    fullscreen: false, windowWidth: 854, windowHeight: 480, confirmOnStop: true,
    showRamWarnings: true,
    showServerWarnings: true,
    serverWarnIgnoreRam: false, serverWarnIgnoreJava: false, serverWarnIgnoreDisk: false, serverWarnIgnoreCrash: false,
    showGameTime: false, recordGameTime: true, showTotalGameTime: false, gameTimeInHours: false,
    showSessionTime: true, showGameTimeOnPage: true,
    tempModsDeleteByDefault: false,
    discordRpcEnabled: true, discordRPCDetails: '{pack}', discordRPCState: '{list}',
    cleanupAfterPackDelete: true
  })
  let settingsSchema = $state<SettingDef[]>([])

  // Вбудовані шрифти (assets/ttf) домішуються у списки шрифтів схеми:
  // «Шрифт інтерфейсу» і всі шрифти консолі (спільний + perType).
  function withBundledFonts(schema: SettingDef[]): SettingDef[] {
    const fontKeys = new Set(['font', 'consoleFontFamily', 'consoleInfoFont', 'consoleWarnFont', 'consoleErrorFont'])
    return schema.map((d) => {
      if (!fontKeys.has(d.key)) return d
      const opts = [...(d.options ?? [])]
      for (const bf of BUNDLED_FONTS) {
        if (!opts.some((o) => o.value === bf.value)) opts.push({ value: bf.value, label: bf.label })
      }
      return { ...d, options: opts }
    })
  }
  let playtimes = $state<Record<string, number>>({})
  // Аналітика ігрового часу (розділ «Аналітика» у «Моїх збірках»): підсумки,
  // стовпчики за 7 днів / тижні / місяці / роки та топ збірок. Один виклик
  // GetAnalytics (бекенд рахує з таблиці сесій playtime_sessions); назви/
  // іконки збірок домішуються на фронтенді з builds.
  let analytics = $state<AnalyticsSummary | null>(null)
  // Закріплені сервери з усіх збірок (секція над картками на «Моїх збірках»).
  let pinnedServers = $state<PinnedServerView[]>([])
  // Результати пінгів закріплених серверів (адреса → ServerPing).
  let pinnedPings = $state<Record<string, ServerPing | null>>({})
  let pinnedPinging = $state(false)
  let storageInfo = $state<StorageInfo | null>(null)
  // Аналіз версій Minecraft (вкладка «Пам'ять і кеш»): які версії
  // встановлені, скільки займають і які вже нікому не потрібні. Модалка
  // «Версії Minecraft» показує список з розмірами і статусом.
  let storageVersions = $state<StorageVersionsInfo | null>(null)
  let versionsBusy = $state(false)
  let versionsModalOpen = $state(false)
  // Рекомендації: збірки, які займають багато місця, але майже не
  // запускаються (playtime). Показуються карткою у вкладці «Пам'ять і кеш».
  let storageSuggestions = $state<StorageSuggestion[]>([])
  // Список встановлених збірок з розмірами (для швидкого видалення у
  // вкладці «Пам'ять і кеш»). Завантажується ліниво разом із storageInfo.
  // IconURL/Color — щоб рядок виглядав як картка sidebar (іконка + колір).
  let storagePacks = $state<StoragePackEntry[]>([])
  // Сканування тек «Пам'ять і кеш» у процесі: поки бекенд рахує розміри
  // великих збірок, розділ показує скелетон з прогрес-баром замість
  // «пустоти, яка потім заповнюється інформацією» (раніше весь вміст був
  // прихований за {#if storageInfo} і користувач бачив порожню картку).
  let storageLoading = $state(false)
  // Поточний прогрес обходу тек (події storage:usage-scan / packs-scan).
  let storageScan = $state<{ done: number; total: number; label: string } | null>(null)
  // Прогрес аналізу невикористовуваної Java (storage:java-scan): який мажор
  // зараз сканується і скільки лишилось.
  let javaProgress = $state<{ done: number; total: number; major: number } | null>(null)
  // Аналіз «сирітських» бібліотек/асетів (minecraft/libraries, minecraft/
  // assets/objects) — файли, що лишились на диску, але жодна встановлена
  // версія їх більше не використовує (напр. після оновлення лоадера).
  // Окремі модалки у вкладці «Пам'ять і кеш», аналогічно Java/версіям.
  let orphanLibs = $state<StorageLibrariesInfo | null>(null)
  let orphanLibsBusy = $state(false)
  let orphanLibsModalOpen = $state(false)
  let orphanAssets = $state<StorageAssetsInfo | null>(null)
  let orphanAssetsBusy = $state(false)
  let orphanAssetsModalOpen = $state(false)
  let folderPaths = $state<FolderPaths | null>(null)
  // Тека шрифтів лаунчера (<dataDir>/fonts) — для підказки в «Вигляді»:
  // шрифти, покладені сюди, з'являються у списках шрифтів (інтерфейс+консоль).
  let fontsDir = $state('')
  let fontsDirLoaded = false
  async function loadFontsDir(force = false) {
    if (fontsDirLoaded && !force) return
    fontsDirLoaded = true
    try { fontsDir = await App.GetFontsDir() } catch (e) {
      fontsDirLoaded = false
      console.error('loadFontsDir:', e)
    }
  }
  let aboutVersion = $state('')
  let updateResult = $state<UpdateInfo | null>(null)
  let gameRunning = $state(false)
  // ── Браузер контенту ──
  // browserPackContext — контекст збірки (кнопка «Шукати моди» / «Додати з
  // браузера» у вкладці «Контент»): версія/лоадер закріплені фільтрами,
  // встановлення йде одразу в цю збірку. projectType — тип контенту, на якому
  // браузер відкриється ('mod' | 'resourcepack' | 'datapack' | 'shader');
  // порожній = «Моди» (як раніше).
  let browserPackContext = $state<{ id: string; name: string; mcVersion: string; loader: string; projectType?: string; isServer?: boolean } | null>(null)
  // browserServerContext — те саме, але для СЕРВЕРА (кнопка «Знайти
  // онлайн» у менеджері модів/плагінів сторінки сервера,
  // ServerContentTab.svelte). kind визначає, чи браузер відкриється на
  // вкладці «Моди» чи «Плагіни». На відміну від browserPackContext, ці
  // два контексти ВЗАЄМОВИКЛЮЧНІ (ContentBrowser сам перевіряє
  // serverContext раніше за packContext) — виставляємо/скидаємо їх
  // разом, щоб застарілий контекст збірки не «просвічував» після
  // переходу з менеджера сервера, і навпаки.
  let browserServerContext = $state<{ id: string; name: string; mcVersion: string; loader: string; kind: 'mods' | 'plugins' } | null>(null)
  // browserServers — список власних серверів для перемикача цілі
  // встановлення «Збірки ↔ Сервери» у фільтрі браузера контенту.
  // Вантажимо ЛІНИВО при вході в браузер (ListServers сканує теки на
  // бекенді — тримати його завжди в пам'яті лаунчера не потрібно) і не
  // частіше ніж раз на хвилину, щоб перехід між сторінками не тягнув
  // список серверів щоразу.
  let browserServers = $state<BrowserServer[]>([])
  // Кеш на 60 с раніше означав: щойно створений сервер (або зміна ядра/
  // назви) не з'являвся у фільтрі браузера до хвилини — «не можу
  // вибрати сервер у фільтрі». Тепер список перечитується при КОЖНОМУ
  // вході на сторінку браузера (перехід із іншої сторінки); не частіше
  // ніж раз на 3 с, щоб швидкі перемикання не спамили ListServers.
  let browserServersAt = 0
  let browserServersPrevPage = ''

  // ── Сповіщення «гравець зайшов/вийшов» (server:player) ────────────────
  // Подія несе лише serverId, а показувати «Гравець Steve приєднався до
  // сервера» без назви — гірше, ніж із нею. Тримаємо простий кеш
  // id → назва: читається тільки в обробнику події, реактивність не потрібна.
  let serverNamesCache: Record<string, string> = {}
  let serverNamesBusy = false
  function loadServerNames() {
    if (serverNamesBusy) return
    serverNamesBusy = true
    App.ListServers()
      .then((list) => {
        const map: Record<string, string> = {}
        for (const s of list ?? []) map[s.id] = s.name
        serverNamesCache = map
      })
      .catch(() => {})
      .finally(() => { serverNamesBusy = false })
  }

  function reloadBrowserServers(force = false) {
    if (!force && Date.now() - browserServersAt < 3_000) return
    browserServersAt = Date.now()
    App.ListServers()
      .then((list) => {
        browserServers = (list ?? []).map(toBrowserServer)
      })
      .catch((e) => { browserServersAt = 0; console.error(e) })
  }
  $effect(() => {
    const page = currentPage
    const entered = page === 'browser' && browserServersPrevPage !== 'browser'
    browserServersPrevPage = page
    // Лише вхід на сторінку. browserServers тут НЕ читаємо: інакше ефект
    // підписався б на нього і перезапускався при кожному оновленні списку.
    if (entered) reloadBrowserServers(true)
  })
  // browserProject — відкрита сторінка модпаку (інтернет-збірка чекає
  // «Завантажити»). Поки заданий — показуємо ModpackPage замість пошуку.
  let browserProject = $state<{ source: string; projectId: string } | null>(null)
  // browserAuthor — відкрита сторінка АВТОРА (клік по автору на сторінці
  // проєкту). Поки заданий — показуємо AuthorPage (поверх проєкту: при
  // поверненні «назад» користувач потрапляє на той самий проєкт).
  let browserAuthor = $state<{ source: string; authorId: string; authorName: string } | null>(null)

  // browserVisited — браузер контенту ЛИШАЄТЬСЯ змонтованим після першого
  // відвідування (ховається через hidden, а не {#if}): пошук, фільтри,
  // активна вкладка і прокрутка не злітають при переході на іншу сторінку
  // і назад (раніше кожен вхід у «Пошук» починав усе з нуля).
  let browserVisited = $state(false)
  $effect(() => {
    if (currentPage === 'browser') browserVisited = true
  })
  // Браузер контенту: чи результати проскролені вниз (подія browser:scroll
  // від ContentBrowser). На сторінці контенту topbar у ВЕРХУ ховає
  // іконку/назву розділу (вони й так видно в табах браузера), а при
  // прокрутці вниз — показує (компактний закріплений заголовок).
  let browserScrolled = $state(false)
  $effect(() => {
    const onScroll = (e: Event) => {
      browserScrolled = !!((e as CustomEvent<{ scrolled: boolean }>).detail?.scrolled)
    }
    window.addEventListener('browser:scroll', onScroll)
    return () => window.removeEventListener('browser:scroll', onScroll)
  })

  // Діалог «Оновити з джерела» (контекстне меню 3 крапки інтернет-збірки):
  // список версій модпаку з сервісу, з позначкою поточної встановленої.
  let updateSourceDialog = $state<{ packId: string; name: string; versions: BrowserVersionView[]; loading: boolean; selected: string; running: boolean } | null>(null)
  // Редактор папки (перейменування/колір) — відкривається ПКМ по папці
  // або з модального вікна папки. Спрощений: назва + колір, збереження
  // живе в renameFolder.
  // Редактор папки. mode='create' — створення НОВОЇ папки з вибором збірок
  // для додавання (раніше папка створювалась одразу порожньою, а юзер хотів
  // вибрати збірки при створенні); mode='edit' — перейменування/колір.
  // selected — множина ID збірок, що увійдуть у папку (лише для create).
  let folderEditor = $state<{ id: string; name: string; color: string; icon: string; mode: 'create' | 'edit'; selected: Set<string> } | null>(null)
  let gameCrashed = $state(false)
  // buildId → час старту сесії (Date.now()) на активному акаунті. Мапа, а
  // не одне число: кілька збірок можуть паралельно йти одночасно на тому
  // самому акаунті, кожна має свій незалежний таймер сесії. Заповнюється в
  // build:started (e.buildId), очищається в build:exit (e.buildId).
  let sessionStartedAtByBuild = $state<Record<string, number>>({})
  // Яка збірка була запущена востаннє (для модалки краху: кнопка
  // «Відкрити консоль» переходить на її сторінку і відкриває вкладку
  // «Консоль»). consoleSignal — лічильник запитів відкрити консоль у
  // PackInstancePage (зростає на 1 при кожному кліку).
  let launchedPackId = $state<string | null>(null)
  // Синхронний «замок» проти накрутки кнопки «Грати»: launchInstance —
  // асинхронна (CheckPreLaunchModIssues до 15с + сам LaunchInstance), а
  // build.status стає 'running' лише коли бекенд підтвердить реальний
  // старт JVM. У цьому вікні bv.status все ще 'ready', тож нетерплячий
  // клац по кнопці 10-1000 разів засилав стільки ж паралельних запусків —
  // sessions.Manager.Start() відхиляв усі, окрім першого, з «build already
  // running», і гравець бачив зливу тостів помилки замість одного запуску.
  // launchStarting виставляється СИНХРОННО на самому вході в
  // launchWithHealthGuard/launchInstance (до першого await) і знімається,
  // коли запуск завершився (успіхом чи помилкою) — тому другий клік під
  // час якщо він трапиться, просто ігнорується.
  let launchStarting = $state<Set<string>>(new Set())
  function isLaunchStarting(id: string) { return launchStarting.has(id) }
  function markLaunchStarting(id: string) {
    if (launchStarting.has(id)) return false
    launchStarting = new Set(launchStarting).add(id)
    return true
  }
  function clearLaunchStarting(id: string) {
    if (!launchStarting.has(id)) return
    const next = new Set(launchStarting)
    next.delete(id)
    launchStarting = next
  }
  let consoleSignal = $state(0)
  // Аналогічний лічильник для розділу «Моди»: кнопка «Відкрити моди» у
  // модалці передзапускової перевірки (дублікати/залежності) переходить
  // на сторінку збірки і одразу відкриває вкладку «Моди» з розгорнутим
  // віджетом дублікатів — той самий підхід, що consoleSignal/gallerySignal.
  // Окремий сигнал потрібен, бо сторінка збірки може вже бути відкрита
  // (запуск іде саме звідти): setPackUI читається лише при маунті, тож
  // без сигналу вкладка б не перемкнулась.
  let modsFocusSignal = $state(0)
  // Аналогічний лічильник для розділу «Галерея»: кнопка «Відкрити
  // галерею» на плаваючому віджеті нового скріншоту (NewScreenshotWidget)
  // переходить на сторінку збірки і одразу перемикає її на вкладку
  // «Галерея» — той самий підхід, що consoleSignal вище.
  let gallerySignal = $state(0)
  function openGalleryFor(packId: string) {
    gallerySignal++
    openPackPage(packId)
  }
  // Банер краху на головній: показується після аварійного завершення гри.
  // «Не показувати знову» — у localStorage, щоб нав'язливе повідомлення не
  // вилазило після кожного краху, поки користувач сам його не вимкне.
  let crashBannerHidden = $state(false)
  function crashBannerDontShow() {
    crashBannerHidden = true
    try { localStorage.setItem('shaurma.crashBannerHidden', '1') } catch (e) { /* ignore */ }
  }
  function crashBannerDismiss() {
    // Закриття банера (×) — лише для поточного краху, без зміни localStorage.
    gameCrashed = false
  }
  // Відкрити консоль з модалки краху: закриваємо модалку, переходимо на
  // сторінку збірки, яка впала, і відкриваємо її вкладку «Консоль».
  function crashBannerOpenConsole() {
    gameCrashed = false
    if (launchedPackId) {
      consoleSignal++
      openPackPage(launchedPackId)
    }
  }
  // Авто-починка: звіт діагностики після краху/помилки запуску (подія repair:report)
  // відкриває діалог лагоджень поверх усього; модалку краху ховаємо, щоб не дублювати.
  const AUTO_REPAIR_OFF = true // false — повернути вікно авто-починки
  let repairDialog = $state<null | { packId: string; report: import('./lib/types').RepairReport }>(null)
  // Остання закрита користувачем пропозиція (щоб повторна подія не відкривала її знову).
  let repairDismissed = { sig: '', at: 0 }
  function closeRepairDialog() {
    if (repairDialog) {
      repairDismissed = {
        sig: repairDialog.packId + '|' + (repairDialog.report?.issues ?? []).map((i: any) => i.id).sort().join(','),
        at: Date.now()
      }
    }
    repairDialog = null
  }
  let modalLogin = $state(false)
  // "Ворота входу": поки немає жодного акаунта — модалка не закривається
  // і нічого іншого зробити не можна.
  let loginGate = $state(false)
  // ── Міграція збірок з інших лаунчерів (Prism / Modrinth App) ──
  // migrationOpen — модалка міграції (пост-логін або віджет у налаштуваннях);
  // migrationPromptDone — «вікно вже спливало цього запуску» (щоб не
  // вискакувати знову після закриття). dbHealthInfo — стан бази при старті
  // (відновлено з бекупу / стерто навмисно) для модального пояснення.
  let migrationOpen = $state(false)
  let migrationInitial = $state<import('./lib/types').MigrationLauncher[]>([])
  let migrationPromptDone = $state(false)
  let dbHealthInfo = $state<DBHealth | null>(null)
  let isShaurmaEdition = $state(false)
  // Скін-система (Google-вхід, скін-акаунт, гардероб офлайн-акаунтів) доступна
  // у Shaurma-редакції. Налаштування «Вимкнути скіни офлайн-акаунтів»
  // (disableSkins) СЮДИ НЕ ВХОДИТЬ: воно лише каже, чи підключати при запуску
  // гри java-агент, щоб бачити скіни офлайн-гравців у грі — а не блокує
  // жодного інтерфейсу лаунчера.
  let skinSupport = $state(false)
  const skinsEnabled = $derived(skinSupport)
  // ID збірки Шаурма, відкритої на сторінці деталей (null = список збірок).
  let openPackId = $state<string | null>(null)
  // Архітектура збірок: BuildView[] від GetBuilds (збірка + стан для
  // ПОТОЧНОГО акаунта). packIndex похідний для сторінки деталей.
  let builds = $state<BuildView[]>([])

  // ── Ручний порядок і закріплення збірок ──
  // Збірки НЕ сортуються (ні за назвою, ні за чимось ще): користувач сам
  // розкладає їх перетягуванням. Закріплені завжди перші (у своєму порядку),
  // решта — у збереженому порядку, НОВІ збірки додаються ВНИЗ (в порядку
  // бекенду). У sidebar закріплені можна перемішувати ЛИШЕ між собою —
  // незакріплені з закріпленими не мішаються (drop відхиляється).
  // Порядок і закріплення зберігаються у settings.json через бекенд (GetPackLayout/SavePackLayout).
  let packLayout = $state<{ order: string[]; pinned: string[] }>({ order: [], pinned: [] })
  // Папки кастомних збірок («Мої збірки»/sidebar): косметична організація,
  // зберігається в settings.json через GetPackFolders/SavePackFolders.
  // Збірка в папці прихована з головного списку (sidebar/сітка) і показується
  // всередині папки; папка виглядає як стек з трьох іконок збірок.
  let packFolders = $state<PackFolder[]>([])
  // Прапор «йде зміна ID збірки»: під час rename локальний список builds ще
  // містить СТАРИЙ id, а папки вже мігрували на НОВИЙ. Без цього прапора
  // $effect очищення папок (нижче) побачив би, що newId ще не серед кастомних
  // збірок, і ВИКИНУВ би збірку з папки. Притримуємо очищення, доки builds
  // не перечитаються (там вже буде newId) — тоді збірка лишається на місці.
  let packRenamePending = $state(false)
  // Які папки розгорнуті в sidebar. Розгорнути можна КІЛЬКА одночасно
  // (раніше був акордеон на одну); стан зберігається в localStorage і
  // переживає перезапуск лаунчера. Ініціалізуємо синхронно одразу з
  // localStorage, щоб $effect-збереження нижче не встигло затерти стан
  // порожнім масивом до асинхронного відновлення.
  let openFolderIds = $state<string[]>(loadOpenFolderIds())

  function loadOpenFolderIds(): string[] {
    try {
      const raw = localStorage.getItem('shaurma.openFolders')
      if (raw) {
        const arr = JSON.parse(raw)
        if (Array.isArray(arr)) return arr.filter((x): x is string => typeof x === 'string')
      }
    } catch { /* ок */ }
    return []
  }
  // Яка папка відкрита в модальному вікні «Мої збірки» (квадрат папки).
  let folderModalId = $state<string | null>(null)
  // Чи активне вікно лаунчера (має фокус). Анімовані іконки (GIF) та
  // декоративні CSS-анімації не відтворюються, поки вікно неактивне —
  // економія CPU на слабких ПК (правило «не видно/не активно → не граємо»).
  // Стан — у спільному модулі windowActive.svelte.ts (той самий ланцюг:
  // shell WM_ACTIVATE → window:active → data-window-active на <html>),
  // щоб і App.svelte, і wardrobe3d читали ОДНЕ джерело істини.
  const windowActive = $derived(windowActiveState.active)
  // Чи увімкнено «анімувати іконки збірок у sidebar завжди» (Налаштування →
  // Вигляд). За замовчуванням у sidebar GIF-іконка грає лише для ВИБРАНОЇ
  // збірки; у «Мої збірки» іконки анімуються завжди.
  let sidebarAnimAlways = $state(false)
  // Компактний вигляд закріплених серверів (Налаштування → Вигляд,
  // ключ pinnedServersCompact) — проп для PinnedServersSection.
  let pinnedServersCompact = $state(false)
  // Стан активного перетягування: яку збірку тягнемо (dragPackId), звідки
  // (dragSrcId — для прозорого «плейсхолдера» на місці картки), над якою
  // карткою курсор і з якого боку (dropTargetId/dropPos — для лінії-підказки
  // кольором збірки) + колір тягненої збірки (dragPackColor → --drop-c).
  let dragPackId = $state<string | null>(null)
  let dragSrcId = $state<string | null>(null)
  let dropTargetId = $state<string | null>(null)
  let dropPos = $state<'before' | 'after' | null>(null)
  let dragPackColor = $state('var(--orange)')
  // «Реальна картка» за курсором під час перетягування збірки/папки
  // (як у Modrinth): позиція привида, за яким слідує міні-картка.
  let dragGhostPos = $state<{ x: number; y: number } | null>(null)
  // Дані для привида: назва + іконка збірки/папки, яку тягнемо.
  const dragGhostInfo = $derived.by(() => {
    const id = dragPackId ?? dragFolderId
    if (!id) return null
    const bv = builds.find(b => b.build.id === id)
    if (bv) {
      const icon = packIconSrc(packAsset(bv.build.iconUrl))
      return { id, name: bv.build.name, icon, ti: icon ? '' : (bv.build.icon || 'ti-puzzle'), color: bv.build.color || 'var(--orange)' }
    }
    const folder = packFolders.find(f => f.id === id)
    if (folder) return { id, name: folder.name, icon: '', ti: 'ti-folder', color: folder.color || 'var(--violet)' }
    return null
  })
  // Pointer-based перетягування (див. packPointerDown/packPointerMove нижче):
  // dragPointerDown — точка натиску; dragPointerActive — рух перевищив поріг
  // 5px і це вже перетягування, а не клік.
  let dragPointerDown = $state<{ id: string; x: number; y: number; color: string } | null>(null)
  // Папка-джерело збірки під час витягування (збірка тягнулась з папки
  // або з модального вікна папки). Ставиться packPointerDown для карток
  // усередині папки; очищається в endPackDrag. Потрібно, щоб drop поза
  // папкою означав «витягнути з папки», а не «нічого не робити».
  let dragFromFolderId = $state<string | null>(null)
  let dragPointerActive = $state(false)
  // suppressPackClick — після drag&drop WebView2 (як і деякі Chromium-версії)
  // ВСЕ ОДНО шле click по картці: користувача перекидало б на сторінку
  // збірки одразу після перетягування, і здавалося б, що порядок «не
  // змінився» або «drag зламаний». Прапорець ставиться в endPackDrag і
  // гасить наступний click (відкриття сторінки) на кілька мілісекунд.
  let suppressPackClick = $state(false)
  // Прогрес завантажень по збірках (живе оновлення з download:progress,
  // щоб прогрес було видно у sidebar на будь-якій вкладці).
  let downloadByPack = $state<Record<string, BuildProgress>>({})
  // Авто-збірки «Vanilla / Snapshot»: стан для розділу налаштувань
  // (GetAutoPacks). autoPackIds — ID створених авто-збірок (для маршрутизації
  // кнопки «Завантажити» в RefreshAutoPackFiles і статусів).
  let autoPacksInfo = $state<AutoPackView[]>([])
  const autoPackIds = $derived(new Set(autoPacksInfo.filter(v => v.created && v.id).map(v => v.id)))
  // Прогрес міграції модів після створення збірки (mods:migrate-progress) —
  // по збірках. Живе у картки збірки (анімована іконка унікального кольору
  // + міні-бар) та на сторінці збірки (банер з назвою/іконкою поточного
  // мода), поки йде StartModMigration.
  let migrateByPack = $state<Record<string, { done: number; total: number; currentFile: string; name: string; iconUrl: string; status: string }>>({})
  // packIndex похідний від builds — сторінка деталей і старі хелпери
  // працюють як раніше, але джерело одне (GetBuilds).
  const packIndex = $derived(builds.map(bv => buildToPack(bv.build)))
  const foundOpenPack = $derived(packIndex.find(p => p.id === openPackId) ?? null)
  // lastOpenPack — останній УСПІШНО знайдений обʼєкт збірки для openPackId.
  //
  // Навіщо: builds перечитуються (loadData/refreshBuilds) після rename,
  // видалення, синку з каталогу, імпорту — і в мить перечитування
  // `packIndex.find(...)` може повернути null, хоча користувач дивиться
  // сторінку САМЕ цієї збірки. Раніше openPack миттєво ставав null, а
  // дочірні компоненти в тому самому флаші ще читали `pack.id` у своїх
  // props-гетерах (напр. PackOfficialServerWidget packId={pack.id}) —
  // звідси «Cannot read properties of null (reading 'id')» і викидання
  // користувача зі сторінки. Тепер сторінка тримає останній відомий стан,
  // поки перечитування не поверне свіжі дані (або збірка зникне назавжди —
  // тоді openPackId явно чиститься в deletePack).
  let lastOpenPack = $state<PackIndexEntry | null>(null)
  $effect(() => { if (foundOpenPack) lastOpenPack = foundOpenPack })
  const openPack = $derived(
    foundOpenPack ?? (openPackId && lastOpenPack?.id === openPackId ? lastOpenPack : null),
  )
  // stablePack — те саме, що openPack, але НІКОЛИ не null, поки відкрита
  // сторінка збірки. Пропси-гетери PackInstancePage читають pack.id у
  // наступному флаші, коли openPack уже міг тимчасово стати null під час
  // перечитування builds («Cannot read properties of null (reading 'id')»).
  // Тримаємо останній відомий об'єкт ПРЯМО тут (не через $effect, який
  // спрацьовує вже після рендера), тож null до дитини просто не доходить.
  let _stableCache: PackIndexEntry | null = null
  const stablePack = $derived.by((): PackIndexEntry => {
    const cur = openPack
    if (cur) { _stableCache = cur; return cur }
    if (_stableCache && (!openPackId || _stableCache.id === openPackId)) return _stableCache
    return (_stableCache ?? ({ id: openPackId ?? '' } as PackIndexEntry))
  })
  const customBuildsCount = $derived(builds.filter(b => b.build.kind === 'custom').length)
  const shaurmaBuildsCount = $derived(builds.filter(b => b.build.kind === 'shaurma').length)
  // Помилка встановлення (collapsible ErrorPanel знизу «Моїх збірок»).
  // Самі логи інсталера більше не показуються — лише помилка, яку можна
  // розгорнути і скопіювати. `false` відрізнити неможливо — завжди рядок.
  let installError = $state('')
  // ── Віджет-тост встановлення ───────────────────────────────────────────
  // Помилки/завершення встановлення показуються ТУТ (фіксований віджет
  // праворуч знизу), а не панеллю в «Моїх збірках». Auto-зникає через 7с,
  // клік по ньому — закрити. kind: 'error' (червоний) | 'done' (зелений).
  let toastMsg = $state<{ kind: 'error' | 'done'; title: string; text: string } | null>(null)
  let toastTimer: ReturnType<typeof setTimeout> | null = null
  function showToast(kind: 'error' | 'done', title: string, text: string) {
    toastMsg = { kind, title, text }
    if (toastTimer) clearTimeout(toastTimer)
    toastTimer = setTimeout(() => { toastMsg = null }, 7000)
  }
  // Стадії запуску конкретної збірки (launch:progress): checking →
  // worker_update → java → loader → done. Map по instanceId, бо кілька
  // збірок можуть запускатись (на різних акаунтах) одночасно.
  let launchProgress = $state<Record<string, LaunchProgress>>({})
  // Офлайн-попередження запуску (LaunchProgress.warning з бекенду, напр.
  // «Немає інтернету — B2310 запуститься в офлайн-режимі»). Модалка з
  // одною кнопкою «Зрозуміло»; done у тому повідомленні false, тож
  // обробник launch:progress продовжує слухати подальші стадії як звичайно.
  let launchWarning = $state<string | null>(null)
  // Скасовані користувачем запуски (подія launch:cancelled з бекенду —
  // кнопка «Зупинити» під час встановлення компонентів). Зберігаємо id,
  // щоб catch у doLaunch міг відрізнити СКАСУВАННЯ від реальної помилки
  // і не показувати червону помилку запуску там, де користувач сам
  // зупинив качку Java/лоадера/Minecraft.
  let launchCancelled = $state<Set<string>>(new Set())
  // Мінімальне вікно прогресу запуску замість повного UI: показується,
  // коли лаунчер стартував з ярлика збірки (--launch <packID>). Поки гра
  // не стартувала/не впала — бачимо лише іконку+назву+прогрес-бар, а не
  // весь лаунчер навколо (як просили: «вікно прогресу запуску без
  // повного запуску вікна лаунчера»).
  // iconUrl — data URL/шлях PNG-іконки збірки (порожній = пресет);
  // presetIcon — Tabler-клас пресет-іконки (напр. "ti-puzzle"), якщо
  // власної картинки немає. Оверлей малює пресет-гліф кольором збірки —
  // як картка сайдбара (баг: раніше сюди падав ti-клас як src картинки,
  // і пресет-іконка взагалі не показувалась).
  let launchOverlay = $state<{ packId: string; name: string; iconUrl: string; presetIcon: string; color: string } | null>(null)
  // Персональні опції запуску з ЯРЛИКА (як у Prism): акаунт + автоприєднання
  // до сервера. Ставляться launchFromShortcut (подія shortcut:launch або
  // TakePendingShortcutLaunch на маунті) і споживаються doLaunch: якщо для
  // збірки є такі опції — викликаємо App.LaunchInstanceShortcut замість
  // LaunchInstance (бекенд перекриває акаунт/автоприєднання рівно на цей
  // запуск, решта — як при звичайному запуску).
  let shortcutLaunchOpts = $state<{ accountId: string; autoJoinServer: string }>({ accountId: '', autoJoinServer: '' })
  let loaded = $state(false)
  // «Останнє вікно» вже відновлено після першого завантаження даних
  // (захист від повторного застосування при повторних викликах loadData).
  let uiRestored = $state(false)
  let isMaximized = $state(false)

  // Wizard (перший запуск)
  let showWizard = $state(false)

  // Заставка «Команда «Шаурма» представляє» після завершення майстра.
  let showIntro = $state(false)

  // Login modal
  let loginMsStep = $state<'idle' | 'waiting'>('idle')
  // Google-флоу скін-системи (незалежний від Microsoft-кроку вище):
  // 'waiting' — чекаємо повернення з браузера; 'need-nick' — OAuth
  // пройдено, а в акаунта ще нема ніка на сервері (лише тоді питаємо).
  let skinLoginStep = $state<'idle' | 'waiting' | 'need-nick'>('idle')
  let skinNickBusy = $state(false)
  let skinLoginEmail = $state('')
  // Яка саме особистість ставить нік на кроці 'need-nick': Google-скін-
  // акаунт (після Google OAuth) чи Microsoft-акаунт Шаурми (після Microsoft
  // входу). Від цього залежить і формулювання підказки в модалці, і куди
  // йде збереження (SetSocialNickname сам диспетчеризує на сервер).
  let skinNickIdentity = $state<'google' | 'microsoft'>('google')

  const pageTitles: Record<string, string> = {
    instances: 'nav.instances', browser: 'nav.browser',
    analytics: 'nav.analytics', wardrobe: 'nav.wardrobe', settings: 'nav.settings', servers: 'nav.servers',
    friends: 'nav.friends'
  }
  const pageIcons: Record<string, string> = {
    instances: 'ti-layout-grid', browser: 'ti-package',
    analytics: 'ti-chart-bar', wardrobe: 'ti-shirt', settings: 'ti-settings', servers: 'ti-server',
    friends: 'ti-users'
  }

  // ── Верхня панель: іконка РОЗДІЛУ (не лаунчера) + заголовок ──
  // Заголовок сторінок перенесений сюди з контенту (page-hero на
  // сторінках більше не дублює його). Для сторінки збірки показуємо сам
  // РОЗДІЛ «Мої збірки», а не назву збірки: назва живе у банері на фоні
  // сторінки (PackInstancePage → inst-banner .ib-name), там їй і місце.
  // "Нова збірка" — окремий повноекранний режим над вкладкою "Мої збірки"
  // (не змінює currentPage, щоб сайдбар лишався підсвіченим на "Мої збірки").
  // Оголошено раніше за topbarIcon/topbarTitle/pageKey, бо ті $derived
  // читають showCreatePack (Svelte 5 компілює $derived у порядку оголошення).
  let showCreatePack = $state(false)
  const topbarIcon = $derived(
    showCreatePack ? 'ti-plus' : pageIcons[currentPage]
  )
  const topbarTitle = $derived(
    showCreatePack ? t('createPack.title') : t(pageTitles[currentPage])
  )
  // Крихта topbar: реальна вкладеність усередині розділу (відкрита збірка,
  // «Нова збірка», аналітика). Для аналітики — кнопка-повернення в «Мої
  // збірки»; для решти — просто текст. Використовується у Topbar.svelte.
  const topbarCrumb = $derived.by(() => {
    if (!showCreatePack && !openPackId && currentPage !== 'analytics') return null
    if (showCreatePack) {
      return { text: createPackTab === 'import' ? t('createPack.tab.import') : t('createPack.tab.manual'), back: false, onback: null }
    }
    if (currentPage === 'analytics') {
      return { text: t('nav.instances'), back: true, onback: () => navigate('instances') }
    }
    return { text: openPack?.name ?? '', back: false, onback: null }
  })

  // ── Стадії запуску збірки (launch:progress) ──
  // Кольори/назви/проценти стадій — у спільному модулі launchStages.ts
  // (однакові в sidebar, тайлах «Мої збірки», сторінці збірки і консолі).

  // Аватари акаунтів (data URL голів зі скінів Mojang; кешування на бекенді).
  let heads = $state<Record<string, string>>({})

  async function refreshHeads() {
    const next: Record<string, string> = {}
    await Promise.all(accounts.map(async (acc) => {
      try {
        const url = await App.GetAccountHead(acc.uuid, acc.username, !!acc.isLicensed)
        const key = acc.uuid || acc.username
        if (key && url) next[key] = url
      } catch (e) { /* запасна голова Стіва */ }
    }))
    heads = next
  }

  function headFor(uuid?: string): string {
    return (uuid && heads[uuid]) || steveHead
  }

  // Поточний акаунт для відображення у сайдбарі та гри: обраний у
  // дропдауні, а якщо не вибраний/видалений — перший у списку.
  function activeAccount(): Account | undefined {
    return accounts.find(a => a.id === activeAccountId) ?? accounts[0]
  }

  async function switchAccount(id: string) {
    activeAccountId = id
    accDropdownOpen = false
    socialChatTarget = ''
    socialActiveId = ''
    try {
      await App.SetActiveAccount(id)
      settings = await App.GetSettings()
      toast(t('acc.switched'), 'success')
      // Стани збірок залежать від активного акаунта (running/RunningOn):
      // після зміни акаунта перечитуємо, інакше sidebar показував би
      // статуси старого акаунта («збірка запущена» на всіх акаунтах).
      refreshBuilds()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  // Ціль із тосту вже спожита — при виході зі сторінки скидаємо, щоб наступний
  // вхід не повертав у старий діалог.
  $effect(() => { if (currentPage !== 'friends') socialChatTarget = '' })
  // Раніше відкривала шухляду; тепер «Друзі» — окремий розділ sidebar.
  // Лишилась під тим самим іменем: її викликають Налаштування.
  function toggleMinecraftDrawer() {
    accDropdownOpen = false
    navigate('friends')
  }

  // ── Розділи налаштувань ────────────────────────────────────────────────
  // (settingsSections/settingsSectionIds/grpTitleKeys тепер у lib/settingsMeta.ts
  // — SettingsPage імпортує їх звідти; тут лишаються лише для глобального
  // пошуку/відновлення вкладки).

  // ── Google-сесія скін-системи (для гардероба офлайн-акаунтів) ───────────
  // Гардероб офлайн-акаунта має сенс лише коли він реально підключений до
  // скін-акаунта Google: скін іде на сервер під цією сесією й роздається
  // за її ніком. Звичайний офлайн-нік без Google-входу гардероба не має.
  let skinAuth = $state<{ ok: boolean; nick: string; expired: boolean; email: string }>({ ok: false, nick: '', expired: false, email: '' })
  // false, поки стан сесії ще не читали: не викидаємо зі сторінки гардероба
  // під час запуску, коли відновлюється остання відкрита сторінка.
  let skinAuthKnown = $state(false)
  // Друзі доступні в обох редакціях; потрібен живий вхід у Shaurma-акаунт.
  // (його JWT — єдина ідентичність для соціальної системи). Лічильник
  // вхідних запитів живе тут, щоб крапка на кнопці працювала й при закритій
  // шухляді.
  const shaurmaFriendsReady = $derived(skinAuth.ok)
  // Кнопка друзів видима в обох редакціях ще до входу в акаунт.
  const shaurmaFriendsVisible = true
  let socialInvites = $state(0)
  // Непрочитані повідомлення за accountId (джерело — бекенд, події friends:unread).
  let socialUnread = $state<Record<string, number>>({})
  const socialUnreadTotal = $derived(Object.values(socialUnread).reduce((a, b) => a + (b || 0), 0))
  // Єдиний бейдж «є що подивитись»: запити в друзі + непрочитані повідомлення.
  const socialBadge = $derived(socialInvites + socialUnreadTotal)
  // Ціль відкриття діалогу з віджета сповіщень у трей (notify_window.go):
  // accountId друга, чий тост щойно натиснули. Передається в шухляду і далі
  // в ShaurmaFriends; скидається при закритті шухляди, щоб наступне
  // відкриття не стрибало в старий діалог.
  let socialChatTarget = $state('')
  // Відкритий діалог на сторінці «Друзі» (переживає перехід на інші розділи).
  let socialActiveId = $state('')
  $effect(() => {
    if (!shaurmaFriendsReady) { socialInvites = 0; socialUnread = {}; return }
    App.GetFriendsState().then(s => {
      socialInvites = s?.invites?.length ?? 0
      socialUnread = s?.unread ?? {}
    }).catch(() => {})
  })

  async function refreshSkinAuth() {
    if (!skinsEnabled) { skinAuth = { ok: false, nick: '', expired: false, email: '' }; skinAuthKnown = true; return }
    try {
      // Готовність друзів/гардероба — це наявність АКАУНТА ШАУРМИ у
      // АКТИВНОГО акаунта лаунчера (кожен акаунт має свій: окремі друзі,
      // чат, P2P, скін). Google-сесії скін-системи більше не існує —
      // раніше тут викликався неіснуючий GetSkinAuthStatus, тому ok завжди
      // був false і друзі/гардероб ніколи не вважали акаунт увімкненим.
      const info = await App.GetShaurmaAccount()
      skinAuth = {
        ok: !!info?.hasAccount,
        nick: (info?.nickname ?? '').trim(),
        expired: false,
        email: '',
      }
    } catch {
      skinAuth = { ok: false, nick: '', expired: false, email: '' }
    }
    skinAuthKnown = true
  }
  // Перечитуємо при зміні редакції/налаштування скінів і активного акаунта.
  $effect(() => {
    void skinsEnabled
    void activeAccount()?.id
    refreshSkinAuth()
  })
  // Сторінка «Акаунт» (створення/видалення/зміна ніка акаунта Шаурми)
  // повідомляє про зміну цією подією.
  $effect(() => {
    const h = () => { refreshSkinAuth() }
    window.addEventListener('skin-auth:changed', h)
    return () => window.removeEventListener('skin-auth:changed', h)
  })

  // ── Автоматичний перезахід у Google, коли скін-сесія завершилась ───────
  // Раніше при протухлій сесії друзі/гардероб лише писали «увійдіть» і
  // людина мусила сама шукати кнопку входу. Тепер лаунчер сам показує
  // модальне вікно перезаходу (SkinReloginModal) — одразу, як тільки бекенд
  // повідомив про завершення сесії (подія skin-auth:expired) або статус
  // показав expired. «Пізніше» вимикає автопоказ до наступного завершення.
  let skinReloginOpen = $state(false)
  let skinReloginWaiting = $state(false)
  let skinReloginDismissed = $state(false)
  // true — вікно відкрито після входу в Microsoft як «прив'язка Google» (а не як перезахід).
  let skinLinkMode = $state(false)
  // Номер спроби: відповідь застарілої спроби (після «Спробувати ще раз» чи
  // «Скасувати») ігноруємо мовчки, без хибного тосту про помилку.
  let skinReloginSeq = 0

  function openSkinRelogin(force = false) {
    if (!skinsEnabled) return
    if (!force && skinReloginDismissed) return
    skinReloginOpen = true
  }

  async function skinReloginStart() {
    // Повторний клік під час очікування = «Спробувати ще раз».
    if (skinReloginWaiting) { try { await App.CancelGoogleSkinLogin() } catch { /* вже завершився */ } }
    const mine = ++skinReloginSeq
    skinReloginWaiting = true
    try {
      const url = await App.StartGoogleSkinLogin()
      if (url) App.OpenExternal(url).catch(() => {})
      const st = await App.AwaitGoogleSkinLogin()
      if (mine !== skinReloginSeq) return
      await refreshSkinAuth()
      window.dispatchEvent(new CustomEvent('skin-auth:changed'))
      skinReloginOpen = false
      skinReloginDismissed = false
      skinLinkMode = false
      toast(st?.email ? `Вхід виконано: ${st.email}` : 'Вхід виконано', 'success')
      if (!(st?.nickname ?? '').trim()) {
        // Інший Google-акаунт без ніка на сервері: нік задається в
        // налаштуваннях → «Скін-акаунт».
        window.dispatchEvent(new CustomEvent('open-settings-skin-account'))
      }
    } catch (e: any) {
      if (mine === skinReloginSeq) toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      if (mine === skinReloginSeq) skinReloginWaiting = false
    }
  }

  function skinReloginCancel() {
    skinReloginSeq++
    if (skinReloginWaiting) App.CancelGoogleSkinLogin()
    skinReloginWaiting = false
  }

  function skinReloginLater() {
    skinReloginCancel()
    skinReloginOpen = false
    skinReloginDismissed = true
    skinLinkMode = false
  }

  // Статус показав завершену сесію — відкриваємо вікно автоматично.
  $effect(() => {
    if (!skinAuthKnown) return
    if (skinAuth.expired) { skinLinkMode = false; openSkinRelogin() }
    else if (skinAuth.ok) { skinLinkMode = false; skinReloginDismissed = false; if (!skinReloginWaiting) skinReloginOpen = false }
  })
  // Тиха періодична перевірка (GetSkinAuthStatus сама оновлює access-токен):
  // завершення сесії помічаємо ще до того, як людина відкриє друзів.
  $effect(() => {
    if (!skinsEnabled) return
    const id = setInterval(() => { if (!document.hidden) refreshSkinAuth() }, 10 * 60 * 1000)
    return () => clearInterval(id)
  })
  // Кнопки «Увійти знову» у шухляді друзів просять відкрити вікно явно.
  $effect(() => {
    const h = () => openSkinRelogin(true)
    window.addEventListener('skin-relogin:open', h)
    return () => window.removeEventListener('skin-relogin:open', h)
  })
  // Те саме для входу в Microsoft-акаунт (ліцензійний без дійсної сесії).
  $effect(() => {
    const h = () => { accDropdownOpen = false; loginMsStep = 'idle'; modalLogin = true }
    window.addEventListener('open-login-modal', h)
    return () => window.removeEventListener('open-login-modal', h)
  })

  // Банер розбіжності ніків у гардеробі веде сюди: перемикаємось на
  // Налаштування → «Акаунт» і прокручуємо до картки профілю
  // (id="settings-account" у SettingsPage.svelte). rAF twice — чекаємо,
  // поки {#if currentPage === 'settings'} і {#if settingsTab === 'account'}
  // реально змонтують картку в DOM, перш ніж її шукати.
  $effect(() => {
    const h = () => {
      currentPage = 'settings'
      settingsTab = 'account'
      requestAnimationFrame(() => requestAnimationFrame(() => {
        document.getElementById('settings-account')?.scrollIntoView({ behavior: 'smooth', block: 'start' })
      }))
    }
    window.addEventListener('open-settings-skin-account', h)
    return () => window.removeEventListener('open-settings-skin-account', h)
  })

  // Гардероб — видимість пункту меню VS реальна робочість:
  //  • ліцензійним (Microsoft) акаунтам — завжди видимий, скін через Mojang API;
  //  • Шаурма-акаунту — видимий, щойно увімкнена скін-система і є активна
  //    Google-сесія (skinAuth.ok), НЕЗАЛЕЖНО від того, чи нік скін-акаунта
  //    збігається з ніком поточного Шаурма-акаунта лаунчера. Раніше пункт
  //    меню ховався мовчки при розбіжності — гравець бачив «гардероб не
  //    працює» без жодної причини. Тепер пункт видно завжди (коли є
  //    сесія), а саму розбіжність показує wardrobeNickMismatch — Wardrobe.svelte
  //    малює банер із поясненням замість сітки пресетів, а не ховається.
  //  • Шаурма-акаунту без Google-сесії взагалі — теж видимий: сторінка
  //    сама покаже заглушку «увійдіть через Скін-акаунт» з кнопкою.
  let wardrobeVisible = $derived.by(() => {
    const acc = activeAccount()
    if (!acc) return false
    if (acc.isLicensed === true) return true
    return !!skinsEnabled
  })

  // Розбіжність ніків: є активна Google-сесія скін-системи, але її нік не
  // збігається з ніком поточного Шаурма-акаунта лаунчера — скін із
  // гардероба в такому разі НЕ застосується цьому акаунту (сервер видає
  // скін за ніком, applyPresetToSkinServer у backend/wardrobe_inject.go).
  let wardrobeNickMismatch = $derived.by(() => {
    const acc = activeAccount()
    if (!acc || acc.isLicensed === true || !skinsEnabled) return false
    if (!skinAuth.ok || !skinAuth.nick) return false
    return skinAuth.nick.toLowerCase() !== (acc.username ?? '').trim().toLowerCase()
  })

  // Якщо гардероб став недоступним (вийшли зі скін-акаунта, змінили акаунт,
  // вимкнули скіни) — викидаємо на сторінку збірок. Поки стан Google-сесії
  // невідомий (старт лаунчера) — не чіпаємо, щоб не збивати відновлення
  // останньої сторінки.
  $effect(() => {
    if (currentPage !== 'wardrobe' || wardrobeVisible) return
    if (activeAccount()?.isLicensed !== true && skinsEnabled && !skinAuthKnown) return
    currentPage = 'instances'
  })

  // ── Посилання-запрошення в друзі (https://shaurma-launcher.org/friend/…) ─
  // Лаунчер сам «бачить» скопійоване посилання (перевірка буфера обміну при
  // поверненні фокуса вікна) і показує вікно «Додати цього друга?». Деталі
  // формату — lib/friends/friendLink.ts. Уже показані лінки запам'ятовуємо в
  // localStorage, щоб не відкривати модалку щоразу при копіюванні того ж лінка.
  const FRIEND_LINKS_SEEN_KEY = 'shaurma.friendLinks.seen'
  let friendInvite = $state<FriendLink | null>(null)

  function seenFriendLinks(): Set<string> {
    try {
      const raw = localStorage.getItem(FRIEND_LINKS_SEEN_KEY)
      const arr = raw ? JSON.parse(raw) : []
      return new Set(Array.isArray(arr) ? arr.filter((x) => typeof x === 'string') : [])
    } catch { return new Set() }
  }
  function rememberFriendLink(id: string) {
    try {
      const s = seenFriendLinks()
      s.add(id.toLowerCase())
      // Тримаємо лише останні 200, щоб сховище не росло безмежно.
      localStorage.setItem(FRIEND_LINKS_SEEN_KEY, JSON.stringify([...s].slice(-200)))
    } catch { /* приватний режим — просто покажемо ще раз */ }
  }

  // Явне відкриття з браузера (shaurma://friend/…): гравець сам натиснув
  // «Відкрити в Shaurma», тож «вже показували» не враховуємо.
  function openFriendLinkFromOS(url: string | undefined | null) {
    const l = parseFriendLink(url)
    if (!l) return
    friendInvite = l
    void App.FocusMainWindow().catch(() => {})
  }

  function maybeOpenFriendLink(text: string | undefined | null) {
    const l = parseFriendLink(text)
    if (!l) return
    if (seenFriendLinks().has(friendLinkKey(l))) return
    friendInvite = l
    // Лінк спрацював → виводимо вікно лаунчера на передній план (воно могло
    // бути згорнуте, у треї або під іншими вікнами).
    void App.FocusMainWindow().catch(() => {})
  }

  // Перевірка буфера обміну при поверненні фокуса. Помилки читання (нема
  // утиліти / буфер зайнятий) навмисно моловемо — це фонова перевірка.
  async function checkClipboardForFriendLink() {
    if (friendInvite) return
    try {
      const text = await App.ClipboardText()
      maybeOpenFriendLink(text)
    } catch { /* ігноруємо */ }
  }
  function closeFriendInvite() {
    if (friendInvite) rememberFriendLink(friendLinkKey(friendInvite))
    friendInvite = null
  }
  $effect(() => {
    const onFocus = () => { void checkClipboardForFriendLink() }
    window.addEventListener('focus', onFocus)
    const timer = setTimeout(() => { void checkClipboardForFriendLink() }, 1200)
    // Поки вікно неактивне (згорнуте/у треї/під іншими), подія focus не
    // прийде — тому лінк, скопійований у браузері/Discord, лишався б
    // непоміченим. Легке опитування буфера лише в неактивному стані.
    const poll = setInterval(() => {
      if (!windowActive) void checkClipboardForFriendLink()
    }, 1500)
    return () => { window.removeEventListener('focus', onFocus); clearTimeout(timer); clearInterval(poll) }
  })

  // ── Анімація «деактивації» пунктів sidebar (nav-item / sb-pack) ────────
  // Коли переключаємось на інший розділ/картку, СТАРИЙ активний елемент
  // має програти коротке «згасання» замість миттєвого зникнення підсвітки
  // (симетрично до pop-анімації активації). CSS-анімація на клас .active
  // тут не підходить: втрата класу — не подія, яку можна «зловити»
  // ключами CSS, тож клас .just-deactivated додає/знімає сам JS, рівно на
  // момент реального переходу.
  let deactivatedNavPage = $state<string | null>(null)
  let deactivatedPackId = $state<string | null>(null)
  let deactivateTimer: ReturnType<typeof setTimeout> | undefined
  function flashDeactivate(prevPage: string | null, prevPackId: string | null) {
    clearTimeout(deactivateTimer)
    deactivatedNavPage = prevPage
    deactivatedPackId = prevPackId
    deactivateTimer = setTimeout(() => {
      deactivatedNavPage = null
      deactivatedPackId = null
    }, 240)
  }

  // resetBrowser — «почати заново»: скидає відкриту сторінку проєкту/
  // автора і фільтр збірки в Браузері. За замовчуванням false — звичайна
  // навігація по вкладках НЕ чіпає стан браузера (щоб повернення з іншого
  // розділу показувало ту саму сторінку контенту, яку переглядали, а не
  // список карток спочатку — раніше це й було багом). true передають лише
  // місця свідомого «чистого» входу: пункт меню «Браузер» у сайдбарі й
  // кнопка «Пошук» на сторінці «Мої збірки» (як натискання «Home»).
  function navigate(page: string, resetBrowser = false) {
    if (page === currentPage && openPackId === null) {
      // Навіть без зміни сторінки клік «Home» по вже активному Браузеру
      // має скинути його до списку — інакше кнопка виглядає неробочою.
      if (resetBrowser && page === 'browser') { browserProject = null; browserAuthor = null; browserPackContext = null; browserServerContext = null }
      return
    }
    // Запам'ятовуємо, ЩО САМЕ було активним до переходу: якщо це була
    // сторінка збірки — «згасає» картка збірки в sidebar, інакше — пункт
    // верхнього меню (Мої збірки/Браузер/Гардероб/Налаштування).
    if (currentPage === 'instances' && openPackId) flashDeactivate(null, openPackId)
    else flashDeactivate(currentPage, null)
    currentPage = page; openPackId = null; showCreatePack = false
    if (page === 'browser' && resetBrowser) { browserProject = null; browserAuthor = null; browserPackContext = null; browserServerContext = null }
  }

  // ── Глобальний пошук (модалка Ctrl+K / клік по полю пошуку) ──────────
  // Шукає: назви збірок («Мої збірки»), контент у браузері (Modrinth) і
  // налаштування (за назвами полів). Клік по результату миттєво переводить
  // на потрібний розділ. Хоткей Ctrl+K — глобальний (window keydown).
  let searchOpen = $state(false)
  let searchQuery = $state('')
  let searchBrowserLoading = $state(false)
  // Результати: локальні (збірки/пресети гардероба/налаштування) — миттєво,
  // браузер — async.
  let searchPacks = $state<BuildView[]>([])
  let searchPresets = $state<SkinPreset[]>([])
  // Пресети гардероба у пошуку: голова скіна (2D, обидва слої) + картка
  // плаща (обрізана передня панель) — ті самі canvas-рендери, що й у
  // прев'ю гардероба. Кеш по id пресета.
  let searchPresetHeads = $state<Record<string, string>>({})
  let searchPresetCapes = $state<Record<string, string>>({})
  let searchBrowser = $state<BrowserEntry[]>([])
  let searchSettings = $state<SettingDef[]>([])
  // Пресети для пошуку: підвантажуються при відкритті пошуку для активного
  // акаунта, якому доступний гардероб (ліцензійний або офлайн зі скін-системою) (у гардероба вони свіжі — GetWardrobePresets).
  async function loadSearchPresets() {
    const acc = activeAccount()
    if (!wardrobeVisible || !acc?.id) { searchPresets = []; return }
    try {
      searchPresets = await App.GetWardrobePresets(acc.id)
    } catch { searchPresets = [] }
  }
  // 2D-голова скіна: обидва слої (8,8)+(40,8) — той самий рендер, що у
  // модалці «Скопіювати скін» (без верхнього шару деталі губляться).
  async function skinHeadDataUrl(skinDataUrl: string): Promise<string> {
    return new Promise((resolve) => {
      const img = new Image()
      img.onload = () => {
        try {
          const c = document.createElement('canvas')
          c.width = 52; c.height = 52
          const ctx = c.getContext('2d')
          if (!ctx) { resolve(''); return }
          ctx.imageSmoothingEnabled = false
          ctx.drawImage(img, 8, 8, 8, 8, 0, 0, 52, 52)
          ctx.drawImage(img, 40, 8, 8, 8, 0, 0, 52, 52)
          resolve(c.toDataURL('image/png'))
        } catch { resolve('') }
      }
      img.onerror = () => resolve('')
      img.src = skinDataUrl
    })
  }
  // Прямокутна «картка плаща»: передня панель (1,2,10,15) — та сама
  // обрізка, що у списку плащів гардероба.
  async function capeFrontDataUrl(capeDataUrl: string): Promise<string> {
    return new Promise((resolve) => {
      const img = new Image()
      img.onload = () => {
        try {
          const c = document.createElement('canvas')
          c.width = 50; c.height = 60
          const ctx = c.getContext('2d')
          if (!ctx) { resolve(''); return }
          ctx.imageSmoothingEnabled = false
          ctx.drawImage(img, 1, 2, 10, 15, 0, 0, c.width, c.height)
          resolve(c.toDataURL('image/png'))
        } catch { resolve('') }
      }
      img.onerror = () => resolve('')
      img.src = capeDataUrl
    })
  }

  const qNorm = (s: string) => s.toLowerCase().trim()
  // Група схеми → ID розділу налаштувань (для переходу з пошуку).
  
  // Відфільтровані пресети гардероба за запитом. Окрема $derived (НЕ
  // перезаписуємо searchPresets усередині ефекту, що його читає — інакше
  // Svelte 5 ловить нескінченний цикл оновлень і пошук «зависає» порожнім).
  let searchPresetResults = $derived(
    searchPresets.filter(p => qNorm(p.name).includes(qNorm(searchQuery))).slice(0, 5)
  )

  $effect(() => {
    if (!searchOpen) return
    // Порядок — як у sidebar (orderedAllBuildViews): збірки з папок теж
    // шукаються і стоять на місці своєї папки, а не в кінці списку.
    searchPacks = orderedAllBuildViews.filter(b => qNorm(b.build.name).includes(qNorm(searchQuery))).slice(0, 8)
    // Міні-прев'ю для знайдених пресетів (голова + плащ), з кешем по id.
    for (const p of searchPresetResults) {
      if (searchPresetHeads[p.id] !== undefined) continue
      App.GetWardrobeTextureDataURL(p.skinPath)
        .then((d) => skinHeadDataUrl(d))
        .then((head) => { if (head) searchPresetHeads[p.id] = head })
        .catch(() => {})
      if (p.capePath) {
        App.GetWardrobeTextureDataURL(p.capePath)
          .then((d) => capeFrontDataUrl(d))
          .then((cape) => { if (cape) searchPresetCapes[p.id] = cape })
          .catch(() => {})
      }
    }
    // Налаштування: індекс пошуку з settings/registry.ts (назва, підказка,
    // синоніми). «group» у псевдо-описі = ID розділу, щоб модалка показала
    // назву розділу.
    searchSettings = searchSettingsIndex(searchQuery, t, 6).map((h) => ({
      key: h.id, type: 'bool', group: h.section, label: labelKeyOf(h.id), desc: '', default: null,
    }) as SettingDef)
    // Браузер-пошук — лише якщо є запит ≥2 символи (інакше даремні мережеві хіти).
    const q = searchQuery.trim()
    if (q.length < 2) { searchBrowser = []; searchBrowserLoading = false; return }
    searchBrowserLoading = true
    let cancelled = false
    const timer = setTimeout(async () => {
      try {
        // Порожній projectType = всі типи (моди, модпаки, ресурспаки, шейдери,
        // датапаки) — див. SearchBrowser: фасет project_type додається лише коли
        // тип заданий. Раніше тут було 'mod' — ресурспаки/шейдери/модпаки
        // ніколи не з'являлись у глобальному пошуку.
        const res = await App.BrowserSearch(q, '', 'all', '', '', '', 'relevance', false, false, false, 8, 0)
        if (!cancelled) searchBrowser = [...(res.modrinth ?? []), ...(res.curseforge ?? [])].slice(0, 8)
      } catch { if (!cancelled) searchBrowser = [] }
      if (!cancelled) searchBrowserLoading = false
    }, 250)
    return () => { cancelled = true; clearTimeout(timer) }
  })

  function openGlobalSearch() {
    searchQuery = ''; searchBrowser = []; searchPacks = []; searchSettings = []; searchPresets = []; searchOpen = true
    // Пресети (для ліцензійного акаунта) перечитаємо при кожному відкритті —
    // користувач міг додати новий у гардеробі.
    searchPresetHeads = {}; searchPresetCapes = {}
    loadSearchPresets()
  }
  function closeGlobalSearch() { searchOpen = false }

  // Ctrl+K — відкрити глобальний пошук (з будь-якого місця лаунчера).
  // Раніше хоткей був написаний на полі пошуку, але обробника не було.
  $effect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && (e.key === 'k' || e.key === 'K' || e.key === 'ы' || e.key === 'К')) {
        e.preventDefault()
        if (searchOpen) closeGlobalSearch()
        else openGlobalSearch()
      } else if (e.key === 'Escape' && searchOpen) {
        closeGlobalSearch()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })

  // Перехід з пошуку: збірка → її сторінка, контент → сторінка ПРОЄКТУ,
  // пресет гардероба → гардероб з вибором пресета, налаштування → вкладка
  // налаштувань + скрол до конкретного рядка.
  function searchGoPack(id: string) {
    searchOpen = false
    openPackPage(id)
  }
  function searchGoBrowser(be: BrowserEntry) {
    searchOpen = false
    // navigate('browser') СКИДАЄ browserProject — тому спершу переходимо,
    // потім відкриваємо проєкт: користувач потрапляє на сторінку модпака.
    navigate('browser')
    browserProject = { source: be.source, projectId: be.id }
    browserAuthor = null
  }
  function searchGoSettings(sectionId: string, key: string) {
    searchOpen = false
    navigate('settings')
    settingsTab = sectionId
    revealSetting(key)
  }
  function searchGoPreset(id: string) {
    searchOpen = false
    navigate('wardrobe')
    // Гардероб сам підхопить подію і вибере пресет (див. Wardrobe.svelte).
    window.dispatchEvent(new CustomEvent('wardrobe:select', { detail: id }))
  }
  // Через модалку передаємо запит у браузер як одноразовий стан.
  let searchTargetQuery = $state('')
  $effect(() => { if (searchTargetQuery) { const q = searchTargetQuery; searchTargetQuery = ''; window.dispatchEvent(new CustomEvent('global-search-query', { detail: q })) } })
  function openPackPage(id: string) {
    if (id !== openPackId || currentPage !== 'instances') {
      if (currentPage === 'instances' && openPackId) flashDeactivate(null, openPackId)
      else if (currentPage !== 'instances') flashDeactivate(currentPage, null)
    }
    pushPackHistory(id); openPackId = id; currentPage = 'instances'; showCreatePack = false
    // Позначаємо збірку АКТИВНОЮ для консолі: окреме вікно консолі
    // прив'язується до неї (а не до останньої ЗАПУЩЕНОЇ з минулого разу) —
    // інакше його кнопка «Запустити» запускала б стару збірку (баг
    // «запустилась інша збірка»). console:focus змушує вікно перечитати
    // знімок/контекст одразу.
    App.SelectActivePack(id)
  }

  // ── Ключ сторінки для анімації перемикання розділів sidebar ───────────
  // Кожна зміна currentPage/showCreatePack/openPackId перемонтовує обгортку
  // .page-view (див. {#key pageKey} у розмітці) і програє плавну анімацію
  // появи: fade + slide + легкий blur (page-view-in у style.css). Так
  // перехід «Мої збірки → Браузер → Налаштування» виглядає преміум-м'яко,
  // а не миттєвим стрибком контенту.
  const pageKey = $derived(
    showCreatePack ? 'instances:create'
      : openPackId ? `instances:pack:${openPackId}`
      : currentPage === 'servers' && showServerWizard ? 'servers:create'
      : currentPage === 'servers' && openRemote ? `servers:remote:${openRemote.ownerId}:${openRemote.serverId}`
      : currentPage === 'servers' && openServerId ? `servers:instance:${openServerId}`
      : currentPage
  )
  // Яку вкладку форми відкрити: "manual" (кнопка «Створити збірку») чи
  // "import" (кнопка «Імпортувати») — передається в initialTab.
  let createPackTab = $state<'manual' | 'import'>('manual')
  function openCreatePack() { createPackTab = 'manual'; showCreatePack = true; openPackId = null; currentPage = 'instances' }
  function openImportPack() { createPackTab = 'import'; showCreatePack = true; openPackId = null; currentPage = 'instances' }
  function closeCreatePack() { showCreatePack = false }
  async function onPackCreated(packId: string, folderId?: string) {
    showCreatePack = false
    instanceTab = 'custom'
    // Чи це ПЕРША збірка користувача (в «Кастомних» ще 0 збірок) і її
    // створили вручну в майстрі — тоді після майстра лаунчер додає дві
    // авто-збірки «Vanilla latest» / «Snapshot» (див. ensureAutoPacksFirstTime).
    // Лічильник читаємо ДО loadData (builds ще зі старим списком).
    const isFirstCustom = !!packId && createPackTab === 'manual' && customBuildsCount === 0
    // Збірка створена — одразу кладемо її в обрану папку (якщо обрана),
    // поки папки ще не перезавантажені з бекенду. Для .shaurmbundle
    // packId порожній: набір збірок у папку додав сам бекенд
    // (ImportShaurmbundleFile folderId), відкривати сторінку однієї
    // збірки не треба — лишаємось на списку «Моїх збірок».
    if (packId && folderId && packFolders.some(f => f.id === folderId)) addPackToFolder(folderId, packId)
    await loadData()
    if (packId) openPackPage(packId)
    // Авто-збірки — «ЦЕ ЛИШЕ ПІСЛЯ wizard»: створюються в кастомних після
    // завершення майстра першої збірки (бекенд сам не дасть створити
    // повторно/без збірок).
    if (isFirstCustom) {
      await ensureAutoPacksFirstTime()
    }
  }

  // ── Відкриття .shaurmpack/.shaurmbundle через Windows-асоціацію ──────────
  // (двічі клік у Провіднику / «Відкрити за допомогою» → лаунчер запускається
  // зі шляхом як аргументом; shell → подія packfile:open, backend кладе шлях
  // як pending на випадок холодного старту). БЕЗ модалки: поводимось так само,
  // як при завантаженні модпаку з браузера — збірка одразу з'являється в
  // «Моїх збірках», відкривається її сторінка, а розпакування й завантаження
  // модів ідуть у фоні з прогресом на картці (подія sync:progress, пауза/
  // скасування — ті самі кнопки).
  async function openPackFile(path: string) {
    if (!path) return
    try {
      if (/\.shaurmpack$/i.test(path)) {
        const pack = await App.StartShaurmpackImport(path)
        toast(t('osPack.started', { name: pack?.name ?? '' }), 'info')
        instanceTab = 'custom'
        navigate('instances')
        await loadData()
        if (pack?.id) openPackPage(pack.id)
      } else if (/\.shaurmbundle$/i.test(path)) {
        // Бандл — набір збірок: імпортуємо у фоні, без модалки; про початок і
        // кінець повідомляє тост, після завершення відкриваємо список збірок.
        toast(t('osPack.bundleStarted'), 'info')
        const res = await App.ImportShaurmbundleFile(path, '')
        toast(t('osPack.bundleDone', { count: res?.packs?.length ?? 0 }), 'success')
        instanceTab = 'custom'
        navigate('instances')
        await loadData()
      } else {
        toast(t('osPack.unknown'), 'error')
      }
    } catch (e: any) {
      toast(t('osPack.error', { message: String(e?.message ?? e ?? '') }), 'error')
    }
  }
  function toggleSidebar() {
    sidebarCollapsed = !sidebarCollapsed
    try { localStorage.setItem('shaurma.sidebarCollapsed', sidebarCollapsed ? '1' : '0') } catch { /* ок */ }
  }

  // ── Налаштування sidebar (у «Вигляді» налаштувань лаунчера) ────────────
  // Зберігаються у БД (settings.json через updateSetting/SaveSettings), а
  // НЕ в localStorage: ширина/масштаб переживають перезапуск лаунчера й
  // не губляться (баг: значення могли не зберегтись у базі даних).
  // «Лише закріплені» ховає незакріплені збірки з sidebar; «розмір
  // елементів» масштабує іконки/шрифти через --sb-scale (CSS).
  const sidebarOnlyPinned = $derived(!!settings.sidebarOnlyPinned)
  const sidebarScale = $derived((settings.sidebarScale ?? 1) as number)
  const sidebarWidth = $derived((settings.sidebarWidth ?? 220) as number)
  function toggleSidebarOnlyPinned() {
    updateSetting('sidebarOnlyPinned', !settings.sidebarOnlyPinned)
  }
  function setSidebarScale(v: number) {
    updateSetting('sidebarScale', Math.min(1.25, Math.max(0.8, v)))
  }
  // Повзунок ширини сайдбара в налаштуваннях: змінює той самий sidebarWidth,
  // що й перетягування правого краю. Зберігає у БД через updateSetting
  // (з дебаунсом) — значення не губляться після закриття лаунчера.
  function saveSidebarWidth() {
    updateSetting('sidebarWidth', sidebarWidth)
  }

  // Ресайз sidebar перетягуванням правого краю (лише розгорнутий): ширина
  // від 220px (мінімум — теперішній розмір) до 400px. Зберігається при
  // відпусканні — повернутись до малого розміру можна кнопкою стиснення.
  function startSidebarResize(e: MouseEvent) {
    e.preventDefault()
    if (sidebarCollapsed) return
    const startX = e.clientX
    const startW = sidebarWidth
    sbResizing = true
    // Під час перетягування не виділяємо текст (інакше драг над текстом
    // збірок тягнув би за собою виділення).
    document.body.style.userSelect = 'none'
    const move = (ev: MouseEvent) => {
      updateSetting('sidebarWidth', Math.min(400, Math.max(220, startW + (ev.clientX - startX))))
    }
    const up = () => {
      sbResizing = false
      document.body.style.userSelect = ''
      window.removeEventListener('mousemove', move)
      window.removeEventListener('mouseup', up)
      saveSidebarWidth()
    }
    window.addEventListener('mousemove', move)
    window.addEventListener('mouseup', up)
  }
  function toggleAccDropdown() { accDropdownOpen = !accDropdownOpen }

  // Закриваємо дропдаун акаунтів кліком поза ним (документний слухач).
  // БАГ (виправлено): Svelte 5 флашить ефекти синхронно одразу після
  // хендлера кліку — тому клік, що ВІДКРИВ дропдаун (по .sidebar-footer),
  // продовжував спливати до document, де щойно доданий слухач закривав
  // його миттєво. Тепер ігноруємо кліки всередині футера/дропдауна.
  $effect(() => {
    if (!accDropdownOpen) return
    const close = (e: MouseEvent) => {
      const t = e.target as HTMLElement | null
      if (t?.closest('.sidebar-footer') || t?.closest('.acc-dropdown')) return
      accDropdownOpen = false
    }
    document.addEventListener('click', close)
    return () => document.removeEventListener('click', close)
  })

  // ── «Останнє вікно»: відновлення + збереження стану навігації ──────────
  // Після першого завантаження даних повертаємось на ту саму сторінку /
  // вкладку збірок / налаштування та у відкриту збірку (builds потрібні,
  // щоб перевірити, чи збережена збірка ще існує). Скрол сторінки збірки
  // відновлює сама PackInstancePage на маунті; скрол решти сторінок — тут.
  function restoreLastWindow() {
    const s = getUIState()
    const knownPages = ['instances', 'browser', 'analytics', 'wardrobe', 'settings', 'servers', 'friends']
    currentPage = knownPages.includes(s.page) ? s.page : 'instances'
    // Збережений вибір поважаємо; на ПЕРШІЙ установці (нічого не збережено)
    // Shaurma-редакція відкривається на «Шаурма», базова — на «Кастомні».
    if (s.instanceTab === 'custom') instanceTab = 'custom'
    else if (s.instanceTab === 'shaurma') instanceTab = isShaurmaEdition ? 'shaurma' : 'custom'
    else instanceTab = isShaurmaEdition ? 'shaurma' : 'custom'
    settingsTab = normalizeSectionId(s.settingsTab)
    const valid = new Set(builds.map(b => b.build.id))
    const savedPack = resolveOpenPackId(valid)
    if (savedPack) {
      openPackId = savedPack
      currentPage = 'instances'
      // Позначаємо відновлену збірку АКТИВНОЮ для консолі одразу (як це
      // робить openPackPage): інакше при старті окреме вікно консолі
      // (відкрите з трею) прив'язалось би до порожнього/застарілого ID і
      // його кнопка «Запустити» могла б запустити не ту збірку (баг
      // «запускається попередня збірка»).
      App.SelectActivePack(savedPack)
    }
    uiRestored = true
    if (!openPackId) restoreContentScroll(getUIState().pages[currentPage] ?? 0)
  }

  // Збереження навігації при кожній зміні (дебаунсу не треба — значення
  // змінюються рідко, а запис у localStorage дешевий).
  $effect(() => {
    if (!uiRestored) return
    const page = currentPage
    const tab = instanceTab
    const st = settingsTab
    const open = openPackId
    updateUIState({ page, instanceTab: tab, settingsTab: st, openPackId: open })
  })

  // Скрол «простих» сторінок відновлюється при поверненні на них; для
  // сторінки збірки тут пропускаємо — нею займається PackInstancePage.
  $effect(() => {
    if (!uiRestored || openPackId) return
    const page = currentPage
    restoreContentScroll(getUIState().pages[page] ?? 0)
  })

  // Збереження скролу «простих» сторінок з дебаунсом (скрол збірки
  // зберігає PackInstancePage — тут його ігноруємо через openPackId).
  let pageScrollTimer: ReturnType<typeof setTimeout> | undefined
  function onMainScroll() {
    if (openPackId) return
    clearTimeout(pageScrollTimer)
    pageScrollTimer = setTimeout(() => {
      const s = getUIState()
      updateUIState({ pages: { ...s.pages, [currentPage]: readContentScroll() } })
    }, 150)
  }
  function persistMainScrollNow() {
    if (openPackId) return
    const s = getUIState()
    updateUIState({ pages: { ...s.pages, [currentPage]: readContentScroll() } })
  }

  async function loadData() {
    // Кожен запит стартового завантаження незалежний (Promise.allSettled):
    // збій одного (напр. GetPackIndex — CDN збірок Шаурма, мережа) не
    // повинен обривати решту. Раніше одна помилка в спільному try/catch
    // вбивала весь ланцюжок: settingsSchema не завантажувалась (порожні
    // розділи налаштувань — лише заголовки) і refreshHeads() не
    // викликався (іконки акаунтів завжди Стів).
    const [accR, setR, buildR, schemaR, playR, anR, verR, edR, autoR, platR, skinR] = await Promise.allSettled([
      App.GetAccounts(),
      App.GetSettings(),
      App.GetBuilds(),
      App.GetSettingsSchema(),
      App.GetPlaytime(),
      App.GetAnalytics(''),
      App.GetVersion(),
      App.IsShaurmaEdition(),
      App.GetAutoPacks(),
      App.GetPlatform(),
      App.HasSkinSupport(),
    ])
    // data-platform якомога раніше (не чекаючи інших полів settings) —
    // на Linux одразу знімає дорогу backdrop-filter-композицію (WebKitGTK,
    // див. applyPlatform), перш ніж почнуть рендеритись важкі елементи UI.
    applyPlatform(platR.status === 'fulfilled' ? platR.value : '')
    accounts = accR.status === 'fulfilled' ? (accR.value ?? []) : []
    settings = setR.status === 'fulfilled' ? setR.value : settings
    activeAccountId = settings.activeAccountId ?? ''
    // Повідомлення «чому не пустило» гравцеві в грі — мовою лаунчера: бекенд тримає їх у пам'яті
    // й віддає прямо в Minecraft, тож після кожної зміни мови надсилаємо йому свіжий переклад.
    onLanguageChange('p2p-reject', () => { App.SetP2PRejectTexts(p2pRejectTexts()).catch(() => {}) })
    setLanguage(settings.language)
    applyAccent(settings.accent, settings.accentCustom, (settings as any).accentCustomMode, (settings as any).accentCustom2)
    applyTheme(settings.theme, { bg: settings.themeCustomBg, card: settings.themeCustomCard, text: settings.themeCustomText })
    // За замовчуванням увімкнено (undefined трактуємо як true — старі
    // збережені стани/тестові моки без цього поля не мають раптово
    // вимикати анімації всім).
    applyAnimations((settings as any).animationsEnabled !== false)
    // Performance Mode: за замовчуванням вимкнено (undefined → false, на
    // відміну від animationsEnabled — тут дефолт саме «вимкнено», щоб
    // не позбавляти блюру всіх існуючих користувачів без свідомого вибору).
    applyPerformanceMode(!!(settings as any).performanceMode)
    sidebarAnimAlways = !!((settings as any).sidebarAnimAlways)
    pinnedServersCompact = !!((settings as any).pinnedServersCompact)
    applyFont(settings.font, settings.fontPath)
    // Шрифт(и) консолі (custom TTF або file:-шрифти з теки fonts) —
    // завантажуються при старті.
    loadConsoleFonts(consoleFontValues(settings), (settings as any).consoleFontPath).catch(() => {})
    builds = buildR.status === 'fulfilled' ? (buildR.value ?? []) : []
    // Авто-збірки (для налаштувань + маршрутизації «Завантажити»);
    // GetAutoPacks ще й тригерить фонову перевірку нових версій Mojang
    // (обмежену по часу на бекенді).
    autoPacksInfo = autoR.status === 'fulfilled' ? (autoR.value ?? []) : autoPacksInfo
    // Таймер «у сесії» на кнопці «Зупинити»: якщо на момент цього першого
    // завантаження (напр. одразу після рестарту лаунчера з уже запущеною
    // грою) подія build:started вже пройшла повз (бекенд шле її під час
    // ServiceStartup, задовго до onMount/підписки на події) — беремо час
    // старту напряму з GetBuilds, інакше кнопка показує «Зупинити» без
    // часу, поки щось не викличе refreshBuilds() вдруге.
    applySessionTimerFallback(builds)
    settingsSchema = schemaR.status === 'fulfilled' ? withBundledFonts(schemaR.value ?? []) : []
    playtimes = playR.status === 'fulfilled' ? (playR.value ?? {}) : {}
    analytics = anR.status === 'fulfilled' ? (anR.value ?? null) : null
    loadPinnedServers()
    aboutVersion = verR.status === 'fulfilled' ? verR.value : ''    // Прапорець збірки: у base-версії бекенд-методів Шаурма-збірки
    // немає взагалі (фізично не скомпільовані), тому вкладку/список
    // приховуємо і за замовчуванням показуємо вкладку "Кастомні".
    isShaurmaEdition = edR.status === 'fulfilled' ? edR.value : false
    skinSupport = skinR.status === 'fulfilled' ? skinR.value : isShaurmaEdition
    if (!isShaurmaEdition) instanceTab = 'custom'
    // Відновлюємо «останнє вікно» одразу після першого завантаження даних.
    // initUIState() підвантажує реальний стан з .ui-state.json (диск) — до
    // цього getUIState() повертав би лише дефолти в пам'яті.
    if (!uiRestored) { await initUIState(); restoreLastWindow() }
    // Аватари пробуємо завжди, навіть якщо щось вище не завантажилось
    // (всередині вже свій try/catch на кожен акаунт).
    refreshHeads()
    loaded = true
    // Якщо акаунта немає — "ворота входу": головний екран недоступний,
    // поки не увійдеш хоча б в один акаунт. (Поки йде першозапускний
    // майстер, модалка не рендериться — у неї умова !showWizard.)
    loginGate = accounts.length === 0 && !showWizard
    if (loginGate) { loginMsStep = 'idle'; modalLogin = true }
  }

  // Завантаження збірки: Шаурма-збірки (server-backed) якають через
  // App.DownloadPack (packs-сервіс з api.PackIndexEntry). Кастомні збірки,
  // створені з браузера контенту (source modrinth/curseforge, ще без
  // файлів — картка «Не завантажено» одразу після встановлення з
  // ContentBrowser/ModpackPage), качаються ІНШИМ методом —
  // App.DownloadBrowserModpack, бо в них немає запису в packs-індексі.
  // Раніше тут завжди викликався App.DownloadPack, який для кастомної
  // збірки просто нічого не знаходив і мовчки нічого не робив — кнопка
  // «Завантажити» була фактично мертва для модпаків з браузера.
  async function downloadPack(id: string) {
    const bv = builds.find(b => b.build.id === id)
    if (bv?.build.kind === 'custom' && (bv.build.source === 'modrinth' || bv.build.source === 'curseforge')) {
      try {
        await App.DownloadBrowserModpack(id)
      } catch (e: any) {
        toast(t('error.withMessage', { message: e }), 'error')
      }
      return
    }
    // Авто-збірка з новою версією Minecraft: качаємо компоненти саме її
    // поточної версії (фоном, з прогресом на картці; запуск заблоковано,
    // поки йде оновлення).
    if (bv?.build.kind === 'custom' && autoPackIds.has(id)) {
      try {
        await App.RefreshAutoPackFiles(id)
      } catch (e: any) {
        toast(t('error.withMessage', { message: e }), 'error')
      }
      return
    }
    App.DownloadPack(id)
  }

  // ── Авто-збірки «Vanilla / Snapshot»: керування ────────────────────────
  // Створюються автоматично ПІСЛЯ завершення майстра першої збірки
  // (onPackCreated), керуються в налаштуваннях (ввімкнути/вимкнути,
  // видалити, створити заново).
  async function loadAutoPacks() {
    try {
      autoPacksInfo = (await App.GetAutoPacks()) ?? []
    } catch (e) { console.error(e) }
  }

  async function ensureAutoPacksFirstTime() {
    try {
      const created = (await App.EnsureAutoVersionPacks(autoPackIcons.vanilla, autoPackIcons.shaphot, false)) ?? []
      await loadAutoPacks()
      await loadData()
      const names = created.filter(v => v.created).map(v => v.name)
      if (names.length > 0) {
        toast(t('settings.autopacks.created', { names: names.join(', ') }), 'success')
      }
    } catch (e: any) {
      // Помилка (напр. мережа недоступна і не вдалось дізнатись останню
      // версію Minecraft) не ламає створення основної збірки — лише
      // повідомляємо, що авто-збірки з'являться пізніше.
      console.error('EnsureAutoVersionPacks', e)
      toast(t('settings.autopacks.createFailed', { message: String(e) }), 'error')
    }
  }

  async function autoPacksToggle(kind: string, enabled: boolean) {
    try {
      // Іконка потрібна бекенду, коли тумблер РЕЄСТРУЄ ще не створену
      // авто-збірку (auto-vanilla / auto-snapshot) — вона пише PNG у теку
      // збірки як icon.png. Для вже створеної — просто вмикає/вимикає.
      const icon = kind === 'release' ? autoPackIcons.vanilla : autoPackIcons.shaphot
      await App.SetAutoPackEnabled(kind, enabled, icon)
      await loadAutoPacks()
      loadData()
      // Рядок-«тумблер, який реєструє збірку» — після реєстрації картка
      // збірки з'являється у «Моїх збірках» одразу.
      if (enabled) {
        const v = autoPacksInfo.find(x => x.kind === kind)
        if (v?.created) toast(t('settings.autopacks.registered', { name: v.name }), 'success')
      }
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  async function autoPacksDelete(view: AutoPackView) {
    const ok = await confirmDialog(
      t('settings.autopacks.deleteConfirmTitle', { name: view.name }),
      t('settings.autopacks.deleteConfirmHint'),
      { confirmLabel: t('pack.delete'), cancelLabel: t('pack.cancel'), danger: true },
    )
    if (!ok) return
    try {
      await App.DeletePack(view.id)
      await loadAutoPacks()
      await loadData()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  // Закріплені сервери з усіх збірок (App.ListPinnedServers) для секції
  // над картками на «Моїх збірках». Перечитається після зміни акаунта або
  // після будь-якої зміни закріплень у розділі «Сервери».
  async function loadPinnedServers() {
    try { pinnedServers = (await App.ListPinnedServers()) ?? [] } catch (e) { console.error(e) }
  }

  // Перечитання закріплених серверів при кожному показі списку збірок
  // (вхід на «Мої збірки», повернення зі сторінки збірки): закріплення
  // могли змінитись у розділі «Сервери». Після перечитання — пінг усіх,
  // щоб одразу були MOTD/онлайн-статуси.
  $effect(() => {
    if (currentPage === 'instances' && !openPackId && !showCreatePack) {
      loadPinnedServers().then(() => pingAllPinned())
    }
  })

  // Оптимізація (розділ 3): pingAllPinned() пропускається, коли вікно
  // приховане — але сам $effect на видимість не підписаний, тож пінг
  // міг би не повторитись ніколи, якби «Мої збірки» показались у
  // прихованому вікні. Повторний пінг при поверненні у фокус: якщо
  // закріплені сервери ще «завантажуються» (null-пінги) — перезапускаємо
  // пінг, щоб статуси не застрягали навічно.
  $effect(() => {
    if (!windowVisibility.visible) return
    if (currentPage !== 'instances' || openPackId || showCreatePack) return
    const stale = pinnedServers.some(s => pinnedPings[s.address] === undefined)
    if (stale) pingAllPinned()
  })

  // ── Закріплені сервери: пінг / копіювання / відкріплення ──
  async function pingAllPinned() {
    // Оптимізація (розділ 3): вікно згорнуте/приховане — мережеві пінги
    // не потрібні; при поверненні у фокус $effect нижче викличе перечит
    // закріплених серверів і пінг заново.
    if (!windowVisibility.visible) return
    const addrs = pinnedServers.map(s => s.address)
    if (!addrs.length || pinnedPinging) return
    pinnedPinging = true
    for (const a of addrs) pinnedPings = { ...pinnedPings, [a]: null }
    try {
      const result = await App.PingServers(addrs)
      pinnedPings = { ...pinnedPings, ...result }
    } catch {
      const fb: Record<string, ServerPing> = {}
      for (const a of addrs) fb[a] = { online: false, error: t('pack.game.pingError') }
      pinnedPings = { ...pinnedPings, ...fb }
    }
    pinnedPinging = false
  }

  async function pingOnePinned(address: string) {
    if (!windowVisibility.visible) return
    pinnedPings = { ...pinnedPings, [address]: null }
    try { pinnedPings = { ...pinnedPings, [address]: await App.PingServer(address) } }
    catch { pinnedPings = { ...pinnedPings, [address]: { online: false, error: t('pack.game.pingError') } } }
  }

  // Запустити гру і одразу приєднати до сервера — ЄДИНА точка входу для
  // БУДЬ-ЯКОЇ кнопки «Приєднатись»/«Грати на сервері» (закріплені сервери
  // в сайдбарі, розділ «Сервери» на сторінці збірки, віджет офіційного
  // сервера). Делегує в launchWithHealthGuard — ТОЙ САМИЙ вхід, яким
  // користується головна кнопка «Грати» на сторінці збірки: health-guard
  // (Pack Doctor), перевірка модів, RAM-попередження, launchProgress,
  // gameRunning/launchedPackId, HideToTray — усе однаково, лише
  // останній крок — LaunchInstanceOnServer замість LaunchInstance
  // (див. doLaunch).
  //
  // РАНІШЕ ця й аналогічні функції (PackOfficialServerWidget.join,
  // PackServersSection.playOn) викликали App.LaunchInstanceOnServer
  // НАПРЯМУ, в обхід усього ланцюга launchWithHealthGuard → launchInstance
  // → ramCheckOrLaunch → doLaunch — тобто без health-guard, без
  // перевірки модів і без RAM-попередження. На бекенді LaunchInstanceOnServer
  // так само викликає повний LaunchInstance (з оновленням Shaurma-паку),
  // тож файли збірки не могли «застрягнути» самі по собі — але користувач
  // не бачив жодного індикатора прогресу під час якання оновлення (не було
  // launchProgress/launchedPackId/gameRunning), тож клік виглядав так, ніби
  // «нічого не відбувається» на великих оновленнях. Тепер усі кнопки
  // запуску — головна, з консолі, з ярлика і з будь-якого сервера —
  // проходять один і той самий видимий шлях.
  async function joinPinnedServer(packId: string, address: string) {
    await launchWithHealthGuard(packId, address)
  }

  // Переставляння закріплених серверів drag&drop (той самий підхід, що
  // для збірок через packLayout): новий порядок ключів "packId|address"
  // зберігається на бекенді (Settings.PinnedServerOrder), список перечит
  // навмисно НЕ робимо — бекенд поверне сервери вже у збереженому порядку
  // при наступному заході на «Мої збірки».
  async function reorderPinnedServers(keys: string[]) {
    // Оптимістичне оновлення локального списку: інакше drag&drop зберігав
    // порядок у БД (SavePinnedServerOrder), але UI лишався в старому
    // порядку до перезавантаження — «сервери взагалі не сортуються як
    // користувач захоче». Переставляємо pinnedServers за новими ключами
    // ("packId|address"), невідомі (нові закріплення) лишаються в кінці.
    const byKey = new Map(pinnedServers.map(s => [s.packId + '|' + s.address, s]))
    const next: PinnedServerView[] = []
    for (const k of keys) {
      const s = byKey.get(k)
      if (s) next.push(s)
    }
    for (const s of pinnedServers) {
      if (!keys.includes(s.packId + '|' + s.address)) next.push(s)
    }
    pinnedServers = next
    try {
      await App.SavePinnedServerOrder(keys)
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
      await loadPinnedServers()
    }
  }

  async function copyAddress(address: string) {
    try {
      await App.CopyToClipboard(address)
      toast(t('pack.servers.copied'), 'success')
    } catch { /* буфер обміну недоступний */ }
  }

  async function unpinPinnedServer(packId: string, address: string) {
    try {
      await App.TogglePinPackServer(packId, address)
      await loadPinnedServers()
      pinnedPings = {}
    } catch (e: any) { toast(t('error.withMessage', { message: e }), 'error') }
  }
  // Повний ануінстал збірки (файли + реєстр + конфіг). З підтвердженням:
  // видалення незворотне, тож питаємо перед тим, як прибрати файли.
  async function deletePack(id: string) {
    const pk = packIndex.find(p => p.id === id)
    const bv = builds.find(b => b.build.id === id)
    const ok = await confirmDialog(t('pack.deleteConfirm'), `${pk?.name ?? id}\n\n${t('pack.deleteConfirmHint')}`, {
      confirmLabel: t('pack.delete'), cancelLabel: t('cancel'), danger: true
    })
    if (!ok) return
    try {
      await App.DeletePack(id)
      // Прибрати збережений стан «останнього вікна» видаленої збірки.
      removePackState(id)
      // Прибрати id видаленої збірки з ручного порядку/закріплених/
      // локальних папок — інакше .build-layout.json назавжди накопичує
      // «сміття» (id, для яких немає жодної збірки). Візуально це не
      // видно (orderMainBuildViews і так фільтрує невідомі id при
      // рендері), але файл на диску продовжує рости і будь-який
      // сторонній інструмент, що читає його напряму, бачить привидів.
      // Shaurma-збірку НЕ прибираємо з order — DeletePack лишає її «не
      // встановленою» (openPackId = id нижче), а не видаленою з каталогу,
      // тож користувач цілком може завантажити її знову на тому самому
      // місці в sidebar.
      if (bv?.build.kind !== 'shaurma') {
        packLayout = {
          order: packLayout.order.filter(x => x !== id),
          pinned: packLayout.pinned.filter(x => x !== id),
        }
        savePackLayout()
        if (packFolders.some(f => !f.serverManaged && f.packIds.includes(id))) {
          packFolders = packFolders.map(f =>
            f.serverManaged ? f : { ...f, packIds: f.packIds.filter(x => x !== id) }
          )
          savePackFolders()
        }
      }
      toast(t('pack.deleted'), 'success')
      // Лишаємось на сторінці збірки: Шаурма-збірка після видалення лишається
      // в каталозі, тож сторінка показує стан «не встановлена» з кнопкою
      // «Завантажити» (а не повертаємось у список збірок).
      // Кастомна збірка після видалення ЗНИКАЄ зі списку збірок — тримати
      // її сторінку відкритою немає сенсу (раніше тут лишався id-«фантом»,
      // і сторінка або падала, або показувала чужі дані, коли builds
      // перечитувались). Шаурма-збірка лишається в каталозі — тож її
      // сторінка просто переходить у стан «не встановлена».
      if (bv?.build.kind === 'shaurma') openPackId = id
      else if (openPackId === id) openPackId = null
      await loadData()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  // ── Стани збірок (архітектура BuildView) ───────────────────────────────

  function buildToPack(b: Build): PackIndexEntry {
    return {
      id: b.id, name: b.name, description: b.description,
      mc_version: b.mcVersion, loader_type: b.loaderType, loader_version: b.loaderVersion,
      icon_url: b.iconUrl, background_url: b.backgroundUrl, mrpack_url: b.source,
      updated_at: b.version, tags: b.tags, color: b.color, version: b.version, icon: b.icon,
      // server_ip — офіційний сервер збірки (Build.serverIp з packs-index.json,
      // serverIp у Go); без цього віджет офіційного сервера на сторінці
      // збірки (PackOfficialServerWidget) ніколи не бачив адресу, бо pack
      // тут похідний саме від Build, а не напряму від App.GetPackIndex().
      server_ip: b.serverIp,
    }
  }

  // Фолбек для таймера «у сесії» на кнопці «Зупинити»: якщо подію
  // build:started пропущено (вона прийшла з бекенду ДО того, як фронтенд
  // встиг підписатись — саме так і буває при рестарті лаунчера з живою
  // грою, бо restoreRunningGame шле build:started ще в ServiceStartup,
  // задовго до монтування App.svelte), відновлюємо час старту з GetBuilds
  // (View.StartedAtMs, який бекенд рахує з sessionsM незалежно від подій).
  // Не затираємо вже наявний штамп — подія точніша (вона була при
  // свіжому запуску). Викликається і з loadData() (перший рендер після
  // старту/рестарту лаунчера), і з refreshBuilds() (усі наступні
  // перечити) — раніше жила лише в refreshBuilds(), тому одразу після
  // рестарту лаунчера перший рендер сторінки збірки бачив status=running,
  // але sessionStartedAtByBuild лишався порожнім: кнопка показувала
  // просто «Зупинити» без таймера, поки щось не викликало refreshBuilds
  // вдруге (баг: «лаунчер забуває час сесії після перезапуску»).
  function applySessionTimerFallback(list: BuildView[]) {
    const patch: Record<string, number> = {}
    for (const b of list) {
      if (b.status === 'running' && b.startedAtMs && !(b.build.id in sessionStartedAtByBuild)) {
        patch[b.build.id] = b.startedAtMs
      }
    }
    if (Object.keys(patch).length > 0) sessionStartedAtByBuild = { ...sessionStartedAtByBuild, ...patch }
  }

  // Перечит станів з бекенду (GetBuilds), зберігаючи живий прогрес.
  async function refreshBuilds() {
    try {
      const list = (await App.GetBuilds()) ?? []
      builds = list.map(b => ({ ...b, progress: b.progress ?? downloadByPack[b.build.id] }))
      applySessionTimerFallback(list)
    } catch (e) { console.error(e) }
  }

  // packsPollTimer — id setInterval для тихого поллінгу каталогу збірок
  // (запускається в onMount, нижче — packsPollTimer = window.setInterval(...)).
  // Оголошено тут, на рівні компонента, а не локальною змінною всередині
  // onMount: onDestroy нижче — синхронний виклик на верхньому рівні
  // <script> (виконується одразу при ініціалізації компонента, ДО
  // будь-яких await), тому лишається валідним незалежно від того, що сам
  // onMount — async і втрачає lifecycle-контекст Svelte після першого await.
  let packsPollTimer = 0
  onDestroy(() => window.clearInterval(packsPollTimer))

  // Точкова зміна однієї збірки (події build:started/build:exit).
  function patchBuild(id: string, fn: (b: BuildView) => BuildView) {
    const i = builds.findIndex(b => b.build.id === id)
    if (i < 0) return
    builds = builds.map((b, idx) => idx === i ? fn(b) : b)
  }

  // ── Ручний порядок збірок (drag&drop) ──────────────────────────────────
  // Відсортований (НЕ за назвою!) список збірок: спершу ЗАКРІПЛЕНІ (у
  // збереженому порядку), потім решта у збереженому порядку, нові збірки
  // (без запису в layout) — внизу, як прийшли з бекенду. Після перетягування
  // layout перебудовується (persisted у settings.json через бекенд).
  // ── Впорядкована черга збережень layout/папок ─────────────────────────
  // Fire-and-forget виклики до бекенду — окремі RPC, які можуть дійти НЕ в
  // порядку дій користувача. Через це видалення папки могло «не зберегтись»:
  // попередній rename/create (ще в польоті) перезаписував видалений список,
  // і папка поверталась після перезапуску. Черга + знімок на момент виклику
  // гарантують FIFO-порядок записів на бекенді.
  let settingsSaveChain: Promise<unknown> = Promise.resolve()
  function queueSettingsSave<T>(fn: () => Promise<T>): Promise<T> {
    const p = settingsSaveChain.then(fn, fn)
    settingsSaveChain = p.catch(() => {})
    return p
  }

  function savePackLayout() {
    const order = [...packLayout.order]
    const pinned = [...packLayout.pinned]
    queueSettingsSave(() => App.SavePackLayout(order, pinned)).catch((e: any) => console.error('savePackLayout:', e))
  }

  async function loadPackLayout() {
    try {
      const p = await App.GetPackLayout()
      if (p && Array.isArray(p.order) && Array.isArray(p.pinned)) {
        packLayout = { order: p.order, pinned: p.pinned }
      }
    } catch (e) { console.error('loadPackLayout:', e) }
  }

  // ── Папки кастомних збірок ──────────────────────────────────────────────
  function savePackFolders() {
    // Знімок (deep-copy) на момент виклику: навіть якщо до виконання черги
    // стан зміниться, у БД піде саме той список, що був на момент дії.
    const snapshot = packFolders.map(f => ({ ...f, packIds: [...f.packIds] }))
    queueSettingsSave(() => App.SavePackFolders(snapshot)).catch((e: any) => console.error('savePackFolders:', e))
  }

  async function loadPackFolders() {
    try {
      const f = await App.GetPackFolders()
      if (Array.isArray(f)) packFolders = f
    } catch (e) { console.error('loadPackFolders:', e) }
  }

  // ОЧИЩЕННЯ від старої помилки: раніше можна було додати офіційну
  // Shaurma-збірку в папку кастомних збірок (через модалку створення папки),
  // і вона зникала з розділу «Шаурма» в sidebar. Як тільки builds завантажені —
  // викидаємо Shaurma-паки з усіх папок (і зберігаємо), щоб вони повернулись
  // у свій розділ. Далі додавання Shaurma в папку заблоковане (addPackToFolderAt).
  $effect(() => {
    if (builds.length === 0) return
    // Під час зміни ID збірки builds ще старий (локальна міграція папок
    // вже на новий id) — очищення викинуло б збірку з папки. Чекаємо, поки
    // builds перечитаються (див. packRenamePending в onPackRenamed).
    if (packRenamePending) return
    const customIds = new Set(builds.filter(b => b.build.kind === 'custom').map(b => b.build.id))
    let changed = false
    const cleaned = packFolders.map(f => {
      const packIds = f.packIds.filter(id => customIds.has(id))
      if (packIds.length !== f.packIds.length) changed = true
      return { ...f, packIds }
    })
    if (changed) {
      packFolders = cleaned
      savePackFolders()
    }
  })

  // Папка, якій належить збірка (або null).
  function folderOfPack(packId: string): PackFolder | null {
    return packFolders.find(f => f.packIds.includes(packId)) ?? null
  }

  // ── Правила відтворення анімованих іконок (GIF) ────────────────────────
  // Загальне правило: анімація грає лише коли вікно активне І контент
  // видимий. У sidebar GIF-іконка за замовчуванням грає лише для ВИБРАНОЇ
  // збірки (sidebarAnimAlways — «анімувати завжди»); у «Мої збірки»/
  // модалці папки — завжди, якщо вікно активне.
  // isGifUrl — чи це потенційно АНІМОВАНЕ зображення: GIF або WebP
  // (анімований WebP зберігається тепер як є — див. Баг 7, раніше все
  // конвертувалось у PNG і анімація втрачалась). Статичний WebP, якщо
  // трапиться, так само безпечно проходить цю гілку: пауза покаже його
  // як є (картинка і так статична), що візуально ідентично.
  function isGifUrl(url: string | undefined | null): boolean {
    if (!url) return false
    return url.startsWith('data:image/gif') || url.startsWith('data:image/webp') || /[.?&_]?\.(gif|webp)($|\?)/i.test(url)
  }

  // Чи можна показувати АНІМОВАНИЙ варіант іконки в цьому контексті.
  // animatedUrl — data URL (packAsset), статичною заміною буде пресет-іконка.
  function canAnimateIcon(animatedUrl: string | undefined, scope: 'sidebar' | 'grid' | 'folder', isSelected: boolean): boolean {
    if (!animatedUrl || !isGifUrl(animatedUrl)) return true // не-GIF не «зупиняється»
    if (!windowActive) return false                        // вікно неактивне → не граємо
    if (scope === 'sidebar') return isSelected || sidebarAnimAlways
    return true // grid/folder: «Мої збірки» — завжди, якщо вікно активне
  }

  // packIconSrc вирішує, який src показувати: для анімованих повертаємо
  // АНІМОВАНИЙ src завжди — паузу виконує AnimatedIcon (заморожує
  // ПОТОЧНИЙ кадр через canvas), а не підміна на перший кадр (вона
  // смикала картинку і створювала «дригання» при перемиканні фокуса).
  function packIconSrc(icon: string | undefined): string {
    return icon || ''
  }

  // shouldPauseIcon — чи має AnimatedIcon заморозити цю іконку: анімований
  // GIF/WebP у контексті, де анімація заборонена (вікно неактивне, або
  // sidebar-іконка невибраної збірки за правилом canAnimateIcon).
  function shouldPauseIcon(icon: string | undefined, scope: 'sidebar' | 'grid' | 'folder', isSelected: boolean): boolean {
    return !!icon && isGifUrl(icon) && !canAnimateIcon(icon, scope, isSelected)
  }

  // Додати збірку в папку (якщо вже в іншій — переміщується).
  function addPackToFolder(folderId: string, packId: string) {
    addPackToFolderAt(folderId, packId, null, null)
  }

  // Додати збірку в папку. При targetPackId/pos — НА МІСЦЕ вказаної збірки
  // (drop на картку всередині папки): збірка вставляється в packIds перед/
  // після цільової, а не в кінець. Інакше — додається в кінець.
  //
  // ВАЖЛИВО: папки — це організація КАСТОМНИХ збірок. Офіційні Shaurma-збірки
  // не можна класти в папки: вони живуть у своєму розділі (sidebar), а папка
  // ховає збірку з головного списку — Shaurma-пак «зникав» з розділу (баг).
  function addPackToFolderAt(folderId: string, packId: string, targetPackId: string | null, pos: 'before' | 'after' | null) {
    const pk = builds.find(b => b.build.id === packId)
    if (pk && pk.build.kind !== 'custom') {
      toast(t('packs.folderNoShaurma'), 'error')
      return
    }
    packFolders = packFolders.map(f => {
      if (f.id !== folderId) return { ...f, packIds: f.packIds.filter(x => x !== packId) }
      let ids = f.packIds.includes(packId) ? f.packIds : [...f.packIds]
      if (targetPackId && ids.includes(targetPackId)) {
        const without = ids.filter(x => x !== packId)
        const ti = without.indexOf(targetPackId)
        if (ti >= 0) {
          const at = pos === 'before' ? ti : ti + 1
          without.splice(at, 0, packId)
          ids = without
        } else if (!ids.includes(packId)) {
          ids = [...ids, packId]
        }
      } else if (!ids.includes(packId)) {
        ids = [...ids, packId]
      }
      return { ...f, packIds: ids }
    })
    // Збірка в папці не може бути закріпленою в головному списку — інакше
    // вона показувалась би і в папці, і в закріплених одночасно.
    if (packLayout.pinned.includes(packId)) {
      packLayout = { ...packLayout, pinned: packLayout.pinned.filter(x => x !== packId) }
      savePackLayout()
    }
    savePackFolders()
  }

  // Перемістити ПАПКУ на іншу позицію (drag&drop заголовка/плитки): папки
  // стоять у ЄДИНОМУ порядку зі збірками (packLayout.order), тож це
  // звичайний reorder — папку можна поставити між двома збірками і навпаки.
  // Збірки всередині папки не чіпаються.
  function moveFolder(fromId: string, toId: string, pos: 'before' | 'after') {
    commitPackReorder(fromId, toId, pos)
  }

  // Витягнути збірку з папки (без видалення папки).
  function removePackFromFolder(packId: string) {
    const f = folderOfPack(packId)
    if (!f) return
    packFolders = packFolders.map(x =>
      x.id === f.id ? { ...x, packIds: x.packIds.filter(p => p !== packId) } : x
    )
    savePackFolders()
  }

  // Створити папку (з можливістю одразу додати збірку). Папка з'являється
  // НА МІСЦІ першої збірки, яку в неї додали (позиція в packFolders
  // відповідає позиції збірки в головному списку), а не в кінці списку папок.
  function createFolder(name: string, color: string, packIds: string[] = [], icon = '') {
    const id = 'fld_' + Math.random().toString(36).slice(2, 10)
    const folder: PackFolder = { id, name, color, packIds, ...(icon && icon !== 'ti-folder' ? { icon } : {}) }
    const next = [...packFolders]
    if (packIds.length > 0) {
      const idx = orderedBuildViews.findIndex(b => b.build.id === packIds[0])
      if (idx >= 0) next.splice(Math.min(idx, next.length), 0, folder)
      else next.push(folder)
    } else {
      next.push(folder)
    }
    packFolders = next
    // Папка одразу потрапляє в ЄДИНИЙ порядок (packLayout.order) — інакше
    // вона рендерилась би «збоку» (в кінці списку) і її позицію неможливо
    // було б змінити перетягуванням до першого drag-комміта.
    if (!packLayout.order.some(isFolderId)) {
      // Міграція-вставка (якщо порядок ще без папок): папка i — після збірки i.
      const pinnedSet = new Set(packLayout.pinned)
      const inFolder = new Set(next.flatMap(f => f.packIds))
      const packSeq = packLayout.order.filter(x => !pinnedSet.has(x) && !inFolder.has(x))
      const folderIds = next.map(f => f.id)
      const out: string[] = []
      for (let i = 0; i < folderIds.length; i++) {
        if (packSeq[i]) out.push(packSeq[i])
        out.push(folderIds[i])
      }
      for (let i = folderIds.length; i < packSeq.length; i++) out.push(packSeq[i])
      packLayout = { ...packLayout, order: out }
    } else {
      // Папки вже в порядку — просто додаємо нову в кінець.
      packLayout = { ...packLayout, order: [...packLayout.order, id] }
    }
    savePackLayout()
    savePackFolders()
    openFolderIds = [...openFolderIds, id]
    return id
  }

  // Відкрити модалку СТВОРЕННЯ папки: назва, колір + вибір збірок, які
  // одразу додадуться в папку. Папка створюється лише по «Створити» —
  // скасування не лишає порожньої папки.
  function openCreateFolderModal() {
    folderEditor = {
      id: '',
      name: t('packs.folderNewName'),
      color: '#ff8a00',
      icon: 'ti-folder',
      mode: 'create',
      selected: new Set(),
    }
  }

  // Перейменувати/перефарбувати папку.
  function renameFolder(folderId: string, name: string, color: string, icon = '') {
    packFolders = packFolders.map(f => f.id === folderId ? { ...f, name, color, icon: icon && icon !== 'ti-folder' ? icon : undefined } : f)
    savePackFolders()
  }

  // Відкрити редактор папки (назва/колір) з поточними значеннями.
  function openFolderEditor(folderId: string) {
    const f = packFolders.find(x => x.id === folderId)
    if (!f) return
    folderEditor = { id: folderId, name: f.name, color: f.color || '#ff8a00', icon: f.icon || 'ti-folder', mode: 'edit', selected: new Set(f.packIds ?? []) }
  }

  function saveFolderEditor() {
    if (!folderEditor) return
    const fe = folderEditor
    if (!fe.name.trim()) return
    if (fe.mode === 'create') {
      // Нову папку створюємо з вибраними збірками (якщо вибрані збірки вже
      // в інших папках — прибираємо їх звідти, збірка може бути лише в одній).
      const packIds = [...fe.selected]
      const id = createFolder(fe.name.trim(), fe.color, packIds, fe.icon)
      for (const pid of packIds) {
        const cur = folderOfPack(pid)
        if (cur && cur.id !== id) removePackFromFolder(pid)
      }
      folderEditor = null
      toast(t('packs.folderCreated'), 'success')
    } else {
      renameFolder(fe.id, fe.name.trim(), fe.color, fe.icon)
      folderEditor = null
    }
  }

  // Видалити папку — збірки повертаються в головний список.
  function deleteFolder(folderId: string) {
    packFolders = packFolders.filter(f => f.id !== folderId)
    // Прибираємо папку з ЄДИНОГО порядку (packLayout.order) — інакше вона
    // залишилась би «висячим» id у збереженому порядку.
    packLayout = { ...packLayout, order: packLayout.order.filter(id => id !== folderId) }
    openFolderIds = openFolderIds.filter(id => id !== folderId)
    if (folderModalId === folderId) folderModalId = null
    savePackLayout()
    savePackFolders()
  }

  // Розгорнути/згорнути папку в sidebar. Розгорнути можна КІЛЬКА папок
  // одночасно (не акордеон) — просто додаємо/прибираємо ID з масиву.
  function toggleFolderOpen(folderId: string) {
    openFolderIds = openFolderIds.includes(folderId)
      ? openFolderIds.filter(id => id !== folderId)
      : [...openFolderIds, folderId]
  }

  // Зберігаємо стан розгорнутих папок при КОЖНІЙ зміні (і з App.svelte
  // через toggleFolderOpen, і з Sidebar через bind: — там стан мутується
  // напряму, без проміжних функцій).
  $effect(() => {
    const ids = openFolderIds
    try { localStorage.setItem('shaurma.openFolders', JSON.stringify(ids)) } catch { /* ок */ }
  })

  // Порядок збірок для ВІДОБРАЖЕННЯ:
  // - Закріплені: у порядку packLayout.pinned (окремий список)
  // - Незакріплені: у порядку packLayout.order (виключаючи pinned)
  // - Нові (без запису): в кінці, як прийшли з бекенду
  // - Збірки в папках (packFolders[].packIds) — НЕ показуються в головному
  //   списку (вони живуть усередині папки).
  // Порядок збірок у ГОЛОВНОМУ списку (sidebar/сітка) — спільний модуль
  // lib/packOrder.ts, щоб у всіх списках збірок було те саме сортування.
  // Збірки з папок тут не показуються (вони живуть усередині папок).
  // Серверні папки шаурма-збірок: Pack Manager проставляє folderId/
  // folderName/folderColor/folderOrder/folderPosition кожній збірці
  // packs-index.json (розділ «Папки» → «Застосувати», див.
  // pack-manager/internal/packfolder), а лаунчер лише ЧИТАЄ ці поля й
  // будує з них PackFolder — жодного локального редагування, жодного
  // окремого стилювання на кожну збірку. deriveServerFolders/mergeFolders
  // (lib/packOrder.ts) написані саме для цього; раніше вони імпортувались,
  // але ніде не викликались — тому серверні папки не групувались і зовсім
  // не працювали. allFolders — те, що бачать orderMainBuildViews/
  // orderFlatBuildViews: кастомні (packFolders) + серверні (serverFolders)
  // РАЗОМ, щоб і ті, і ті збірки ховались з плоского списку однаково.
  const serverFolders = $derived(deriveServerFolders(builds))
  const allFolders = $derived(mergeFolders(packFolders, serverFolders))
  const orderedBuildViews = $derived(orderMainBuildViews(builds, packLayout, allFolders))
  // Плоский порядок (збірки з папок — на місці своєї папки): для модалки
  // «Копіювати сервер в іншу збірку», глобального пошуку тощо.
  const orderedAllBuildViews = $derived(orderFlatBuildViews(builds, packLayout, allFolders))

  // Папки з розв'язаними BuildView (для sidebar/сітки): кожна папка тримає
  // впорядковані збірки (packIds), неіснуючі ID відсіваються.
  // «Кастомні» — packFolders (локально створені, редаговані в лаунчері;
  // ПОРОЖНІ теж показуються — користувач міг щойно створити папку й ще не
  // встиг докинути збірки, і папку можна видалити з контекстного меню).
  // «Шаурма» — serverFolders (з folderId у packs-index.json, керуються
  // ВИКЛЮЧНО Pack Manager: локально їх не можна перейменувати/видалити,
  // лише відкрити — див. folderCtxMenu нижче, там пункти Rename/Delete
  // сховані для serverManaged). Порожніх серверних папок не буває —
  // deriveServerFolders будує їх лише з фактично призначених збірок.
  const folderViews = $derived.by(() => {
    const byId = new Map(builds.map(bv => [bv.build.id, bv]))
    const source = instanceTab === 'shaurma' ? serverFolders : packFolders
    return source
      .map(f => ({ folder: f, packs: f.packIds.map(id => byId.get(id)).filter((b): b is BuildView => !!b) }))
  })

  // ── ЄДИНИЙ впорядкований список кастомного таба (збірки + папки) ──────
  // Папки стоять В ОДНОМУ списку зі збірками: packLayout.order може містити
  // і ID папок (fld_*), тож папку можна поставити МІЖ двома збірками (і
  // навпаки) — і в sidebar, і в «Мої збірки». Цей список — єдине джерело
  // порядку для обох (замість старої схеми «папки окремим списком у кінці
  // sidebar / на початку сітки»). Закріплені збірки рендеряться ОКРЕМО
  // зверху (папки не піняться); збірки всередині папок у цьому списку НЕ
  // повторюються — вони живуть у папці і рендеряться всередині неї.
  function isFolderId(id: string): boolean { return id.startsWith('fld_') }

  type LayoutItem = { kind: 'pack' | 'folder'; id: string }
  const customLayoutItems = $derived.by((): LayoutItem[] => {
    const items: LayoutItem[] = []
    const byId = new Map(builds.map(bv => [bv.build.id, bv]))
    const folderById = new Map(packFolders.map(f => [f.id, f]))
    const pinnedSet = new Set(packLayout.pinned)
    const inFolder = new Set(packFolders.flatMap(f => f.packIds))
    const placed = new Set<string>()
    // Порядок з packLayout.order (може містити і збірки, і папки).
    for (const id of packLayout.order) {
      if (!id) continue
      if (placed.has(id)) continue
      if (isFolderId(id)) {
        if (folderById.has(id)) { items.push({ kind: 'folder', id }); placed.add(id) }
      } else {
        if (byId.has(id) && byId.get(id)!.build.kind === 'custom' && !inFolder.has(id) && !pinnedSet.has(id)) {
          items.push({ kind: 'pack', id }); placed.add(id)
        }
      }
    }
    // Папки, яких ще немає в порядку (нові) — в кінець.
    for (const f of packFolders) {
      if (!placed.has(f.id)) { items.push({ kind: 'folder', id: f.id }); placed.add(f.id) }
    }
    // Нові збірки (без запису в порядку) — в кінець.
    for (const bv of builds) {
      const id = bv.build.id
      if (bv.build.kind !== 'custom') continue
      if (inFolder.has(id) || pinnedSet.has(id) || placed.has(id)) continue
      items.push({ kind: 'pack', id }); placed.add(id)
    }
    return items
  })

  // Список для sidebar: таб-залежний. На вкладці «Кастомні» — ЄДИНИЙ
  // ручний порядок збірок і папок (customLayoutItems). На «Шаурма» —
  // незакріплені збірки таба (tabUnpinned вже не містить збірок, що
  // потрапили в serverFolders — вони виключені на рівні orderedBuildViews)
  // + серверні папки в кінці списку, у порядку folderPosition
  // (deriveServerFolders вже сортує serverFolders за ним). Закріплені
  // збірки sidebar рендерить окремо (tabPinned), тому тут їх немає;
  // серверна папка закріпленою бути не може (Pack Manager не керує
  // локальним закріпленням).
  const sidebarLayoutItems = $derived.by((): LayoutItem[] => {
    if (instanceTab !== 'custom') {
      const packs: LayoutItem[] = (sidebarOnlyPinned ? [] : tabUnpinned).map(bv => ({ kind: 'pack' as const, id: bv.build.id }))
      if (sidebarOnlyPinned) return packs
      const folders: LayoutItem[] = serverFolders.map(f => ({ kind: 'folder' as const, id: f.id }))
      return [...packs, ...folders]
    }
    return customLayoutItems
  })

  // Одноразова міграція: у збереженому порядку (packLayout.order) ще немає
  // жодної папки (стара модель тримала папки окремим списком) — вставляємо
  // їх у порядок за правилом старої сітки «папка i — після збірки i».
  // Після цього папки можна перетягувати між збірками як завгодно.
  let folderOrderMigrated = false
  $effect(() => {
    if (folderOrderMigrated) return
    if (packFolders.length === 0) return
    // Ще не завантажено (порожній layout до приходу з бекенду).
    if (packLayout.order.length === 0 && packLayout.pinned.length === 0) return
    if (packLayout.order.some(isFolderId)) { folderOrderMigrated = true; return }
    const pinnedSet = new Set(packLayout.pinned)
    const inFolder = new Set(packFolders.flatMap(f => f.packIds))
    const packSeq = packLayout.order.filter(id => !pinnedSet.has(id) && !inFolder.has(id))
    const folderIds = packFolders.map(f => f.id)
    const out: string[] = []
    for (let i = 0; i < folderIds.length; i++) {
      if (packSeq[i]) out.push(packSeq[i])
      out.push(folderIds[i])
    }
    for (let i = folderIds.length; i < packSeq.length; i++) out.push(packSeq[i])
    packLayout = { ...packLayout, order: out }
    savePackLayout()
    folderOrderMigrated = true
  })

  // browserPacks — кастомні збірки для фільтра «Збірка» в браузері
  // (фолбек-список; основний рендер — packPicker нижче, з папками).
  const browserPacks = $derived(
    orderedAllBuildViews
      .filter(b => b.build.kind === 'custom')
      .map(b => ({
        id: b.build.id,
        name: b.build.name,
        mcVersion: b.build.mcVersion,
        loader: b.build.loaderType,
        iconUrl: b.build.iconUrl,
        icon: b.build.icon,
        color: b.build.color,
      }))
  )

  // packPicker — дані для спільного списку збірок з ПАПКАМИ (PackPicker):
  // ЄДИНИЙ порядок збірок і папок (customLayoutItems) + рядки + папки.
  // Використовується у фільтрі «Збірка» браузера контенту (і може
  // підключатись в інші меню вибору) — збірки з папок видно й можна
  // вибрати, папки розкриваються як у sidebar.
  const packPicker = $derived.by(() => {
    const rowById = new Map<string, PackRow>()
    for (const bv of builds) {
      if (bv.build.kind !== 'custom') continue
      rowById.set(bv.build.id, {
        name: bv.build.name,
        color: bv.build.color,
        iconUrl: bv.build.iconUrl,
        icon: bv.build.icon,
        loader: bv.build.loaderType,
        mcVersion: bv.build.mcVersion,
      })
    }
    const folderById = new Map<string, PackFolderRow>()
    for (const f of packFolders) {
      folderById.set(f.id, {
        name: f.name,
        color: f.color,
        packIds: f.packIds.filter(id => rowById.has(id)),
      })
    }
    // Лише кастомні збірки та папки (Шаурма-таб керується синком).
    const items: PackPickerItem[] = customLayoutItems
      .filter(it => it.kind === 'folder' ? folderById.has(it.id) : rowById.has(it.id))
      .map(it => ({ kind: it.kind, id: it.id }))
    return { items, rowById, folderById }
  })

  // Видимі збірки для поточної вкладки (shaurma/custom) у ручному порядку.
  const tabBuilds = $derived.by(() =>
    orderedBuildViews.filter(b => (instanceTab === 'shaurma') === (b.build.kind === 'shaurma'))
  )
  const tabPinned = $derived.by(() => {
    const pin = new Set(packLayout.pinned)
    return tabBuilds.filter(b => pin.has(b.build.id))
  })
  const tabUnpinned = $derived.by(() => {
    const pin = new Set(packLayout.pinned)
    return tabBuilds.filter(b => !pin.has(b.build.id))
  })

  // Чи закріплена збірка (для кнопки-канцелярки та бейджа).
  function isPinned(id: string): boolean {
    return packLayout.pinned.includes(id)
  }

  function togglePin(id: string) {
    const wasPinned = packLayout.pinned.includes(id)
    if (wasPinned) {
      // Відкріплюємо: прибираємо з pinned, додаємо в order (в кінець незакріплених)
      packLayout = {
        order: packLayout.order.includes(id) ? packLayout.order : [...packLayout.order, id],
        pinned: packLayout.pinned.filter(x => x !== id),
      }
    } else {
      // Закріплюємо: прибираємо з order (там не має бути pinned), додаємо в pinned
      packLayout = {
        order: packLayout.order.filter(x => x !== id),
        pinned: [...packLayout.pinned, id],
      }
    }
    savePackLayout()
  }

  // ── ПКМ-меню на картках збірок (sidebar + «Мої збірки») ────────────────
  // Клік ПКМ на картці відкриває контекстне меню зі швидкими діями:
  // відкрити сторінку, закріпити/відкріпити, запустити/зупинити,
  // завантажити/оновити, консоль, тека, полагодити, видалити. Позиція
  // кліпується до розмірів вікна — меню не вилазить за екран. Закривається
  // кліком будь-де, blur-ом вікна або клавішею Esc.
  let ctxMenu = $state<{ id: string; x: number; y: number } | null>(null)
  // ПКМ-меню ПАПКИ (sidebar, правий клік по заголовку папки): відкрити
  // папку, перейменувати, змінити колір, видалити (збірки повертаються
  // в головний список).
  let folderCtxMenu = $state<{ id: string; x: number; y: number } | null>(null)
  // Модалка експорту, відкрита з контекстного меню сайдбару (правий клік
  // по картці збірки → «Експортувати»). Окремий стан від того, що живе
  // в PackInstancePage, бо контекстне меню доступне і коли сторінка
  // збірки не відкрита (клік по будь-якій картці в sidebar/сітці).
  let ctxExportPackId = $state<string | null>(null)
  // Модалка дублювання, відкрита з контекстного меню (правий клік по картці
  // збірки → «Дублювати»). Працює для БУДЬ-ЯКОЇ встановленої збірки
  // (Shaurma і кастомної): результат — нова незалежна кастомна збірка.
  let ctxDuplicatePackId = $state<string | null>(null)
  // Збірка під меню (BuildView — для status/installed/дій).
  // Усередині колбеку find svelte-check не звужує $state ctxMenu до
  // non-null, тому доступаємось через ctxMenu?.id (безпечно й у рантаймі).
  const ctxPack = $derived<BuildView | null>(
    ctxMenu ? builds.find(b => b.build.id === ctxMenu?.id) ?? null : null
  )
  // Чи є ярлик цієї збірки на робочому столі (для пункту «Створити/
  // Видалити ярлик» у ПКМ-меню). Оновлюється при відкритті меню.
  let shortcutByPack = $state<Record<string, boolean>>({})
  // Діалог гнучкого створення ярлика (назва + місце) — як у Prism.
  let shortcutDialogId = $state<string | null>(null)

  function openCtxMenu(e: MouseEvent, id: string) {
    e.preventDefault()
    const W = 220, H = 380
    ctxMenu = {
      id,
      x: Math.max(4, Math.min(e.clientX, window.innerWidth - W - 8)),
      y: Math.max(4, Math.min(e.clientY, window.innerHeight - H - 8)),
    }
    // Стан ярлика підтягуємо асинхронно (пункт меню покаже «Створити» чи
    // «Видалити» за фактом наявності .lnk на диску).
    App.HasDesktopShortcut(id).then((has: boolean) => { shortcutByPack = { ...shortcutByPack, [id]: has } }).catch(() => {})
  }

  $effect(() => {
    if (!ctxMenu) return
    const close = () => { ctxMenu = null }
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') ctxMenu = null }
    window.addEventListener('click', close)
    window.addEventListener('blur', close)
    window.addEventListener('keydown', onKey)
    return () => {
      window.removeEventListener('click', close)
      window.removeEventListener('blur', close)
      window.removeEventListener('keydown', onKey)
    }
  })

  function ctxAction(act: string, id: string) {
    ctxMenu = null
    const bv = builds.find(b => b.build.id === id)
    switch (act) {
      case 'open': openPackPage(id); break
      case 'pin': togglePin(id); break
      case 'play': if (bv) buildAction(bv); break
      case 'download': downloadPack(id); break
      case 'console': App.OpenPackConsole(id); break
      case 'folder': App.OpenPackFolder(id).catch((e: any) => toast(t('error.withMessage', { message: e }), 'error')); break
      case 'repair': App.RepairPack(id).catch((e: any) => toast(t('error.withMessage', { message: e }), 'error')); break
      case 'export': ctxExportPackId = id; break
      case 'duplicate': ctxDuplicatePackId = id; break
      case 'updateSource': openUpdateSource(id); break
      case 'shortcut': openShortcutDialog(id); break
      case 'shortcutRemove': removePackShortcut(id); break
      case 'delete': deletePack(id); break
    }
  }

  // Гнучке створення ярлика: відкриває діалог (назва + місце), як
  // CreateShortcutDialog у Prism. Якщо ярлик уже є — одразу пропонуємо
  // видалення з окремого пункту меню (shortcutRemove).
  function openShortcutDialog(id: string) {
    shortcutDialogId = id
  }

  // Видалити ярлик збірки (робочий стіл / меню «Пуск» / довільна тека).
  async function removePackShortcut(id: string) {
    try {
      await App.RemoveDesktopShortcut(id)
      toast(t('pack.shortcutRemoved'), 'success')
      shortcutByPack = { ...shortcutByPack, [id]: false }
    } catch (e: any) {
      toast(t('pack.shortcutError', { message: e }), 'error')
    }
  }

  // Створити ярлик на робочому столі одним кліком (стара поведінка) —
  // залишаємо для зручності, без діалогу.
  async function toggleDesktopShortcut(id: string) {
    try {
      const has = await App.HasDesktopShortcut(id)
      if (has) {
        await App.RemoveDesktopShortcut(id)
        toast(t('pack.shortcutRemoved'), 'success')
      } else {
        await App.CreateDesktopShortcut(id)
        toast(t('pack.shortcutCreated'), 'success')
      }
      shortcutByPack = { ...shortcutByPack, [id]: !has }
    } catch (e: any) {
      toast(t('pack.shortcutError', { message: e }), 'error')
    }
  }

  // ПКМ по заголовку папки в sidebar: те саме позиціонування, що й у
  // карток, але дії інші (папка не запускається і не видаляє файли).
  function openFolderCtxMenu(e: MouseEvent, id: string) {
    e.preventDefault()
    e.stopPropagation()
    const W = 200, H = 200
    folderCtxMenu = {
      id,
      x: Math.max(4, Math.min(e.clientX, window.innerWidth - W - 8)),
      y: Math.max(4, Math.min(e.clientY, window.innerHeight - H - 8)),
    }
  }

  $effect(() => {
    if (!folderCtxMenu) return
    const close = () => { folderCtxMenu = null }
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') folderCtxMenu = null }
    window.addEventListener('click', close)
    window.addEventListener('blur', close)
    window.addEventListener('keydown', onKey)
    return () => {
      window.removeEventListener('click', close)
      window.removeEventListener('blur', close)
      window.removeEventListener('keydown', onKey)
    }
  })

  function folderCtxAction(act: string, id: string) {
    folderCtxMenu = null
    switch (act) {
      case 'open': toggleFolderOpen(id); break
      case 'rename': openFolderEditor(id); break
      case 'delete': deleteFolder(id); toast(t('packs.folderDeleted'), 'success'); break
    }
  }

  // ── Експорт папки «Мої збірки» → .shaurmbundle ──
  // Усі встановлені кастомні збірки папки запаковуються в ОДИН файл
  // (бекап/перенесення/шер набору — той самий формат, що його лаунчер
  // потім імпортує через .shaurmbundle). Прогрес по збірках — подіями
  // shaurmbundle:export:progress.
  let folderBundleExport = $state<{
    folderId: string
    name: string
    outDir: string
    busy: boolean
    done: number
    total: number
  } | null>(null)
  function openFolderBundleExport(id: string) {
    const fv = folderViews.find(v => v.folder.id === id)
    // Закриваємо модалку папки: її overlay рендериться в <Portal>
    // (пізніше в DOM, той самий z-index) і ПЕРЕКРИВАВ нову модалку
    // експорту — та відкривалась «за» нею й виглядала як «не
    // відкрилась» (скарга «немає модалки експорту шаурма-групи»).
    folderModalId = null
    folderBundleExport = {
      folderId: id,
      name: fv?.folder.name ?? '',
      outDir: '', busy: false, done: 0, total: 0,
    }
  }

  async function pickBundleExportDir() {
    const st = folderBundleExport
    if (!st) return
    try {
      const p = await App.BrowseFolder()
      if (p) st.outDir = p
    } catch { /* діалог скасовано */ }
  }

  async function startFolderBundleExport() {
    const st = folderBundleExport
    if (!st || st.busy) return
    if (!st.name.trim()) { toast(t('packs.bundleExportNameReq'), 'error'); return }
    if (!st.outDir) { toast(t('packs.bundleExportDirReq'), 'error'); return }
    st.busy = true
    st.done = 0
    st.total = 0
    try {
      let res = await App.ExportShaurmbundle({ folderId: st.folderId, outDir: st.outDir, name: st.name.trim() })
      if (res.conflict) {
        st.busy = false
        const ok = await confirmDialog(
          t('packs.bundleExportOverwriteTitle'),
          t('packs.bundleExportOverwriteBody', { path: res.outPath }),
          { confirmLabel: t('packs.bundleExportOverwriteOk'), cancelLabel: t('cancel'), danger: true }
        )
        if (!ok) return
        st.busy = true
        res = await App.ExportShaurmbundle({ folderId: st.folderId, outDir: st.outDir, name: st.name.trim(), force: true })
      }
      const out = res.outPath
      folderBundleExport = null
      toast(t('packs.bundleExportDone', { path: out }), 'success')
    } catch (e: any) {
      toast(t('packs.bundleExportError', { message: String(e) }), 'error')
    } finally {
      if (folderBundleExport) folderBundleExport.busy = false
    }
  }

  // Відкрити діалог «Оновити з джерела» для інтернет-збірки (Modrinth/
  // CurseForge): завантажуємо список версій модпаку з сервісу, поточна
  // встановлена версія позначається.
  async function openUpdateSource(id: string) {
    const bv = builds.find(b => b.build.id === id)
    updateSourceDialog = { packId: id, name: bv?.build.name ?? id, versions: [], loading: true, selected: '', running: false }
    try {
      const versions = (await App.GetPackSourceVersions(id)) ?? []
      const rec = versions.find(v => v.versionType === 'release') ?? versions[0]
      if (updateSourceDialog) {
        updateSourceDialog.versions = versions
        updateSourceDialog.selected = rec ? rec.id : ''
        updateSourceDialog.loading = false
      }
    } catch (e: any) {
      updateSourceDialog = null
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  async function runUpdateFromSource() {
    const d = updateSourceDialog
    if (!d || !d.selected || d.running) return
    d.running = true
    try {
      await App.UpdatePackFromSource(d.packId, d.selected)
      toast(t('browser.update.done', { v: d.versions.find(v => v.id === d.selected)?.versionNumber || d.selected }), 'success')
      updateSourceDialog = null
      refreshBuilds()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      if (updateSourceDialog) updateSourceDialog.running = false
    }
  }

  // ── Drag&drop збірок (pointer-based) ──
  // HTML5 DnD у WebView2 глючить: dragstart не спрацьовує, коли початок
  // перетягування припадає на вкладені елементи картки (кнопки, іконки),
  // тому після додавання закріплень збірки «перестали тягнутися». Замість
  // цього — власне перетягування на подіях pointerdown/pointermove/pointerup:
  // клік (відкриття збірки) і перетягування розрізняються за порогом руху
  // 5px. Ціль (before/after) рахується за позицією курсора над карткою
  // (sidebar — по Y, сітка «Мої збірки» — по X), лінія-підказка малюється
  // кольором збірки (--drop-c). Закріплені перемішуються лише із
  // закріпленими, незакріплені — лише із незакріпленими.
  function packPointerDown(e: PointerEvent, id: string, color: string, fromFolder?: boolean) {
    if (e.button !== 0) return
    dragPointerDown = { id, x: e.clientX, y: e.clientY, color }
    dragPointerActive = false
    suppressPackClick = false
    // Прапорець «збірка з папки»: ставиться картками всередині папки
    // (sidebar розгорнута папка + модальне вікно «Мої збірки»).
    dragFromFolderId = fromFolder ? (folderOfPack(id)?.id ?? null) : null
  }

  function packPointerMove(e: PointerEvent) {
    if (!dragPointerDown) return
    // Перехід «клік → перетягування»: лише після руху > 5px.
    if (!dragPointerActive) {
      const dx = e.clientX - dragPointerDown.x
      const dy = e.clientY - dragPointerDown.y
      if (dx * dx + dy * dy < 25) return
      dragPointerActive = true
      dragPackId = dragPointerDown.id
      dragSrcId = dragPointerDown.id
      dragPackColor = dragPointerDown.color
      // Модальне вікно папки НЕ закриваємо при старті перетягування:
      // доки курсор у межах модалки — це зміна порядку збірок у папці;
      // модалка закривається лише якщо збірку витягли ЗА її межі (див.
      // pointerDragEnd — перевірка .folder-modal у точці відпускання).
      window.addEventListener('mousemove', pointerDragMove)
      window.addEventListener('mouseup', pointerDragEnd, { once: true })
    }
    pointerDragMove(e)
  }

  // Автоскрол під час перетягування: коли курсор ближче 60px до краю
  // scrollable-контейнера (sb-packs або .content) — повільно прокручуємо.
  let dragScrollRaf = 0
  function startDragScroll(el: Element, vertical: boolean, speed: number) {
    cancelAnimationFrame(dragScrollRaf)
    function tick() {
      if (!dragPackId) return
      if (vertical) el.scrollTop += speed
      else (el as HTMLElement).scrollLeft += speed
      dragScrollRaf = requestAnimationFrame(tick)
    }
    dragScrollRaf = requestAnimationFrame(tick)
  }
  function stopDragScroll() { cancelAnimationFrame(dragScrollRaf); dragScrollRaf = 0 }

  // Папка, над якою курсор під час перетягування (drop-ціль папки).
  let dragFolderTarget = $state<string | null>(null)
  // Тип цільової КАРТКИ під курсором під час перетягування: 'pack' (збірка)
  // або 'folder' (плитка/рядок папки) — дозволяє перекидати папки між
  // збірками в сітці й додавати збірку в папку НА МІСЦЕ іншої.
  let dropTargetKind = $state<'pack' | 'folder' | null>(null)

  // ── Перетягування ПАПОК (рядок у sidebar + плитка в «Мої збірки») ──
  // Окремий стан від збірок (dragPackId): папка тягнеться щоб переставити
  // її між іншими папками/збірками; дроп збірки на папку додає збірку.
  let dragFolderId = $state<string | null>(null)
  let dragFolderDown = $state<{ id: string; x: number; y: number } | null>(null)

  function folderPointerDown(e: PointerEvent, id: string) {
    if (e.button !== 0) return
    dragFolderDown = { id, x: e.clientX, y: e.clientY }
    dragPointerDown = null
    dragPointerActive = false
    suppressPackClick = false
    // Pointer capture: onpointermove на рядку/плитці папки викликається
    // лише поки курсор НАД самим елементом. Якщо користувач тягне папку
    // і курсор швидко залишає її межі ДО перевищення порогу 5px — події
    // руху більше не приходять на елемент, drag ніколи не стартує (папка
    // «не хапається»). setPointerCapture перенаправляє всі pointermove/
    // pointerup на елемент, поки кнопка не відпущена — так само, як
    // працює RAM-слайдер у майстрі та drag акаунтів у налаштуваннях.
    const target = e.currentTarget as HTMLElement | null
    try { target?.setPointerCapture?.(e.pointerId) } catch (err) { /* ignore */ }
  }

  function folderPointerMove(e: PointerEvent) {
    if (!dragFolderDown) return
    if (!dragFolderId) {
      const dx = e.clientX - dragFolderDown.x
      const dy = e.clientY - dragFolderDown.y
      if (dx * dx + dy * dy < 25) return
      dragFolderId = dragFolderDown.id
      // Папки підсвічуються фіолетовим під час перетягування (відмінно від
      // збірок, чия лінія-підказка — колір самої збірки).
      dragPackColor = 'var(--violet)'
      window.addEventListener('mousemove', pointerDragMove)
      window.addEventListener('mouseup', pointerDragEnd, { once: true })
    }
    pointerDragMove(e)
  }

  function pointerDragMove(e: MouseEvent) {
    if (!dragPackId && !dragFolderId) { dropTargetId = null; dropPos = null; dropTargetKind = null; dragFolderTarget = null; stopDragScroll(); return }
    // Оновлюємо позицію «реальної картки», що слідує за курсором (Modrinth-style).
    dragGhostPos = { x: e.clientX, y: e.clientY }
    const el = document.elementFromPoint(e.clientX, e.clientY)
    const draggingFolder = !!dragFolderId
    const draggedId = dragPackId ?? dragFolderId
    // Картка має ПРІОРИТЕТ над дроп-зоною папки: курсор над карткою
    // ВСЕРЕДИНІ папки = reorder у папці, над карткою глобального списку =
    // звичайний reorder. Папка ловить drop лише коли курсор НЕ над карткою
    // (заголовок папки або порожнє місце всередині неї).
    // Витягування збірки ЗА межі модального вікна папки: ховаємо модалку
    // ОДРАЗУ під час перетягування (а не тільки при відпусканні), щоб
    // плейсхолдери й картки списку/сітки позаду стали видимі й доступні
    // для drop. Раніше модалка з затемненням перекривала цілі до самого
    // mouseup — поставити збірку в потрібне місце було неможливо.
    if (dragPackId && folderModalId && dragFromFolderId) {
      if (!el?.closest?.('.folder-modal')) folderModalId = null
    }
    const card = el?.closest?.('[data-pack-id]') as HTMLElement | null
    if (card) {
      dragFolderTarget = null
      const tid = card.dataset.packId ?? ''
      if (!tid || tid === draggedId) { dropTargetId = null; dropPos = null; dropTargetKind = null; }
      else if (!draggingFolder && packLayout.pinned.includes(dragPackId ?? '') !== packLayout.pinned.includes(tid)) {
        dropTargetId = null; dropPos = null; dropTargetKind = null;
      } else {
        // Позиція before/after — ЗА ВІДНОШЕННЯМ курсора до розміру картки,
        // а не порівнянням з серединою у координатах в'юпорта: якщо елементи
        // sidebar збільшені (--sb-scale) або будь-як трансформовані,
        // getBoundingClientRect повертає скалований прямокутник, і поріг
        // «середини» у clientX/clientY роз'їжджався — збірка падала не туди
        // у різних сегментах списку. Відношення до розміру коректне за
        // будь-якого масштабу (transform/zoom/calc-розміри).
        const isList = !card.classList.contains('inst-tile')
        const rect = card.getBoundingClientRect()
        const ratio = isList
          ? (e.clientY - rect.top) / (rect.height || 1)
          : (e.clientX - rect.left) / (rect.width || 1)
        dropTargetId = tid
        dropTargetKind = 'pack'
        dropPos = ratio < 0.5 ? 'before' : 'after'
      }
    } else {
      dropTargetId = null
      dropPos = null
      dropTargetKind = null
      dragFolderTarget = null
      // Папка, що тягнеться, може впасти ПЕРЕД/ПІСЛЯ плитки папки в сітці
      // (перестановка папок між собою) — плитка папки стає карткою.
      if (draggingFolder) {
        const folderCard = el?.closest?.('[data-folder-id]') as HTMLElement | null
        if (folderCard) {
          const tid = folderCard.dataset.folderId ?? ''
          if (tid && tid !== draggedId) {
            const rect = folderCard.getBoundingClientRect()
            const ratio = !folderCard.classList.contains('inst-tile')
              ? (e.clientY - rect.top) / (rect.height || 1)
              : (e.clientX - rect.left) / (rect.width || 1)
            dropTargetId = tid
            dropTargetKind = 'folder'
            dropPos = ratio < 0.5 ? 'before' : 'after'
          }
        } else {
          const folderEl = el?.closest?.('[data-folder-drop]') as HTMLElement | null
          dragFolderTarget = folderEl?.dataset?.folderDrop ?? null
          // ФІКС «папка повертається назад»: курсор над дроп-ЗОНОЮ папки
          // (контейнер .sb-folder / .folder-tile), а не над її рядком
          // [data-folder-id] — раніше встановлювався лише dragFolderTarget,
          // а dropTargetId/kind лишались null, тому pointerDragEnd не
          // викликав moveFolder і порядок «відкатувався». Тепер дроп-зона
          // папки теж вважається ціллю перестановки папок (after — папка
          // встає після цільової), якщо це не сама тягнута папка.
          if (dragFolderTarget && dragFolderTarget !== draggedId) {
            dropTargetId = dragFolderTarget
            dropTargetKind = 'folder'
            dropPos = 'after'
          }
        }
      } else {
        // ЗБІРКА над папкою: крайові зони рядка/плитки папки = поставити
        // збірку ПЕРЕД/ПІСЛЯ папки, середина = додати збірку В папку (як
        // раніше). Раніше БУДЬ-яке наведення на папку означало «додати в
        // папку», тому при папці на ПЕРШІЙ позиції списку збірку було
        // неможливо поставити перед нею — тепер верхній (у сітці — лівий)
        // край папки ставить збірку перед нею.
        const folderRow = el?.closest?.('[data-folder-id]') as HTMLElement | null
        if (folderRow) {
          const fid = folderRow.dataset.folderId ?? ''
          if (fid && fid !== draggedId) {
            const rect = folderRow.getBoundingClientRect()
            const isTile = folderRow.classList.contains('inst-tile')
            const ratio = isTile
              ? (e.clientX - rect.left) / (rect.width || 1)
              : (e.clientY - rect.top) / (rect.height || 1)
            if (ratio < 0.3) {
              dropTargetId = fid
              dropTargetKind = 'folder'
              dropPos = 'before'
            } else if (ratio > 0.7) {
              dropTargetId = fid
              dropTargetKind = 'folder'
              dropPos = 'after'
            } else {
              dragFolderTarget = fid
            }
          }
        } else {
          const folderEl = el?.closest?.('[data-folder-drop]') as HTMLElement | null
          dragFolderTarget = folderEl?.dataset?.folderDrop ?? null
        }
      }
      // ПРОВУЛОК: курсор над контейнером списку/сітки, але НЕ над карткою —
      // рахуємо слот вставки за позицією курсора МІЖ елементами (раніше
      // drop ловився лише точно над карткою, тому лінія-підказка в зазорі
      // не спрацьовувала і збірку треба було «цілити» в картку). Покриває
      // і «поставити перед першою папкою» у sidebar.
      if (!dropTargetId && !dragFolderTarget) {
        const container = el?.closest?.('.sb-packs, .sf-packs-inner, .tile-grid, .fm-grid') as HTMLElement | null
        if (container) {
          const slot = dropSlotFromPointer(container, e.clientX, e.clientY, draggedId, draggingFolder)
          if (slot) {
            dropTargetId = slot.id
            dropTargetKind = slot.kind
            dropPos = slot.pos
          }
        }
      }
    }
    // Автоскрол: шукаємо найближчий scrollable предок
    const zone = 60, maxSpeed = 8
    let scrolled = false
    // sidebar sb-packs (вертикальний)
    const sbPacks = document.querySelector('.sb-packs') as HTMLElement | null
    if (sbPacks) {
      const r = sbPacks.getBoundingClientRect()
      if (e.clientX >= r.left && e.clientX <= r.right) {
        if (e.clientY < r.top + zone && sbPacks.scrollTop > 0) {
          const speed = -Math.round(maxSpeed * (1 - (e.clientY - r.top) / zone))
          startDragScroll(sbPacks, true, speed); scrolled = true
        } else if (e.clientY > r.bottom - zone && sbPacks.scrollTop < sbPacks.scrollHeight - sbPacks.clientHeight) {
          const speed = Math.round(maxSpeed * (1 - (r.bottom - e.clientY) / zone))
          startDragScroll(sbPacks, true, speed); scrolled = true
        }
      }
    }
    // .content грід (вертикальний)
    if (!scrolled) {
      const contentEl = document.querySelector('.content') as HTMLElement | null
      if (contentEl) {
        const r = contentEl.getBoundingClientRect()
        if (e.clientX >= r.left && e.clientX <= r.right) {
          if (e.clientY < r.top + zone && contentEl.scrollTop > 0) {
            const speed = -Math.round(maxSpeed * (1 - (e.clientY - r.top) / zone))
            startDragScroll(contentEl, true, speed); scrolled = true
          } else if (e.clientY > r.bottom - zone && contentEl.scrollTop < contentEl.scrollHeight - contentEl.clientHeight) {
            const speed = Math.round(maxSpeed * (1 - (r.bottom - e.clientY) / zone))
            startDragScroll(contentEl, true, speed); scrolled = true
          }
        }
      }
    }
    if (!scrolled) stopDragScroll()
  }

  // ── Слот вставки за позицією курсора В ПРОМІЖКУ між картками ──────────
  // Використовується коли курсор над контейнером списку/сітки, але не над
  // карткою (тобто «в провулку» між елементами). Замість «цілитися» точно
  // в картку тепер можна кинути збірку/папку в зазор: слот обчислюється за
  // координатами курсора відносно середин елементів. Повертає
  // { id, kind, pos } для dropTarget-стану або null, якщо валідного місця
  // немає (напр. перетин закріплених і незакріплених збірок).
  function dropSlotFromPointer(
    container: HTMLElement,
    x: number,
    y: number,
    draggedId: string | null,
    draggingFolder: boolean
  ): { id: string; kind: 'pack' | 'folder'; pos: 'before' | 'after' } | null {
    const isGrid = container.classList.contains('tile-grid') || container.classList.contains('fm-grid')
    // Збираємо елементи-«картки» контейнера: збірки ([data-pack-id]) та
    // рядки/плитки папок ([data-folder-id]). Для sidebar-папки це .sf-row
    // (рядок у .sb-folder), для сітки — .folder-tile.
    const items: { el: HTMLElement; id: string; kind: 'pack' | 'folder' }[] = []
    const push = (el: HTMLElement) => {
      const pid = el.dataset.packId
      const fid = el.dataset.folderId
      if (pid) items.push({ el, id: pid, kind: 'pack' })
      else if (fid) items.push({ el, id: fid, kind: 'folder' })
    }
    for (const el of Array.from(container.children) as HTMLElement[]) {
      if (isGrid) {
        if (el.matches('.inst-tile')) push(el)
      } else if (el.matches('.sb-pack, .sf-row')) {
        push(el)
      } else if (el.classList.contains('sb-folder')) {
        // Папка в sidebar: позиція рядка-заголовка = позиція папки.
        const row = el.querySelector(':scope > .sf-row') as HTMLElement | null
        if (row) items.push({ el: row, id: row.dataset.folderId ?? '', kind: 'folder' })
      }
    }
    const cands = items.filter(it => it.id && it.id !== draggedId)
    if (!cands.length) return null

    const rects = cands.map(it => ({ it, r: it.el.getBoundingClientRect() }))
    let target: { it: typeof cands[number]; r: DOMRect } | null = null
    let pos: 'before' | 'after' = 'before'

    if (isGrid) {
      // Сітка (горизонтальний потік з переносом): знаходимо рядок, у якому
      // курсор (найближчий за вертикаллю), і всередині рядка — слот по X.
      const rows: { top: number; items: typeof rects }[] = []
      for (const q of [...rects].sort((a, b) => a.r.top - b.r.top)) {
        const row = rows.find(r => Math.abs(r.top - q.r.top) < 10)
        if (row) row.items.push(q)
        else rows.push({ top: q.r.top, items: [q] })
      }
      let row = rows.find(r => y >= r.top && y <= r.top + r.items[0].r.height) ?? null
      if (!row) {
        row = [...rows].sort((a, b) => {
          const ca = a.top + a.items[0].r.height / 2
          const cb = b.top + b.items[0].r.height / 2
          return Math.abs(y - ca) - Math.abs(y - cb)
        })[0] ?? null
      }
      if (row) {
        row.items.sort((a, b) => a.r.left - b.r.left)
        let idx = row.items.findIndex(q => x < q.r.left + q.r.width / 2)
        if (idx === -1) { target = row.items[row.items.length - 1]; pos = 'after' }
        else { target = row.items[idx]; pos = 'before' }
      }
    } else {
      // Вертикальний список: слот = скільки середин карток вище курсора.
      const sorted = [...rects].sort((a, b) => a.r.top + a.r.height / 2 - (b.r.top + b.r.height / 2))
      const idx = sorted.findIndex(q => y < q.r.top + q.r.height / 2)
      if (idx === -1) { target = sorted[sorted.length - 1]; pos = 'after' }
      else if (idx === 0) { target = sorted[0]; pos = 'before' }
      else { target = sorted[idx]; pos = 'before' }
    }
    if (!target) return null

    // Перетин закріплених і незакріплених (як у картковому шляху): папки
    // не піняться — для збірки-папки ціль завжди валідна.
    if (!draggingFolder && dragPackId) {
      const draggedPinned = packLayout.pinned.includes(dragPackId)
      const targetPinned = target.it.kind === 'pack' && packLayout.pinned.includes(target.it.id)
      if (draggedPinned !== targetPinned) return null
    }
    return { id: target.it.id, kind: target.it.kind, pos }
  }

  // Переупорядкування збірок ВСЕРЕДИНІ папки (drop на картку тієї ж папки):
  // спільний код для модального вікна папки і розгорнутої папки в sidebar.
  function reorderInFolder(folderId: string, dragged: string, target: string, pos: 'before' | 'after') {
    const folder = packFolders.find(f => f.id === folderId)
    if (!folder) return
    const without = folder.packIds.filter(x => x !== dragged)
    const ti = without.indexOf(target)
    if (ti < 0) return
    const at = pos === 'before' ? ti : ti + 1
    without.splice(at, 0, dragged)
    packFolders = packFolders.map(f => f.id === folderId ? { ...f, packIds: without } : f)
    savePackFolders()
  }

  function pointerDragEnd(e?: MouseEvent) {
    const dragged = dragPackId
    const draggedFolder = dragFolderId
    // ВАЖЛИВО: захоплюємо папку-джерело ДО endPackDrag — він обнуляє
    // dragFromFolderId (і всі drag-стани), а гілка витягування з папки
    // нижче читає його ПІСЛЯ. Без цього копіювання «витягнути збірку з
    // папки» було мертвим кодом: прапорець завжди встигав обнулитись,
    // і дроп поза папкою нічого не робив.
    const fromFolderId = dragFromFolderId
    const target = dropTargetId
    const targetKind = dropTargetKind
    const pos = dropPos
    const folderTarget = dragFolderTarget
    const wasDrag = dragPointerActive || !!draggedFolder
    window.removeEventListener('mousemove', pointerDragMove)
    dragPointerDown = null
    dragPointerActive = false
    dragFolderDown = null
    // Гасимо наступний click після перетягування (WebView2 все одно шле
    // click по картці одразу після mouseup). 60 мс достатньо.
    endPackDrag(wasDrag)
    if (!wasDrag) return

    // ── Збірка з МОДАЛЬНОГО ВІКНА папки (воно ще відкрите) ──
    // Доки курсор у межах модалки — це ПЕРЕСТАНОВКА збірок у папці (drop на
    // картку тієї ж папки) або просто «нічого» (порожнє місце). Модалка
    // закривається і збірка ВИТЯГУЄТЬСЯ з папки лише коли відпускання сталося
    // ЗА межами модального вікна (на затемненому тлі або поза ним).
    if (dragged && fromFolderId && folderModalId) {
      const insideModal = !!(e?.target && (e.target as HTMLElement).closest?.('.folder-modal'))
      if (insideModal) {
        const srcFolder = folderOfPack(dragged)
        if (srcFolder && target && targetKind === 'pack' && folderOfPack(target)?.id === srcFolder.id) {
          reorderInFolder(srcFolder.id, dragged, target, pos ?? 'after')
        }
        return
      }
      folderModalId = null
      removePackFromFolder(dragged)
      commitPackReorder(dragged, target, pos)
      return
    }

    // ── Перетягування ПАПКИ ──
    if (draggedFolder) {
      if (target && pos) {
        // Папка на папку або папка на ЗБІРКУ (sidebar і «Мої збірки»):
        // папки стоять у ЄДИНОМУ порядку зі збірками (packLayout.order),
        // тож папку можна поставити МІЖ двома збірками і навпаки.
        moveFolder(draggedFolder, target, pos)
      }
      return
    }

    if (dragged && folderTarget) {
      // Дроп на папку = додати збірку в папку. Модалка папки, з якої
      // тягнули, закривається (збірка переміщається в іншу папку/список).
      if (folderModalId && dragged) folderModalId = null
      addPackToFolder(folderTarget, dragged)
      return
    }

    // Дроп збірки на плитку ПАПКИ в сітці (folder-card у режимі картки).
    // Раніше це завжди означало «додати в папку»; тепер крайові зони
    // плитки/рядка папки дають dropPos — тоді це ПЕРЕСТАНОВКА збірки
    // перед/після папки в спільному списку (папки стоять в packLayout.order
    // нарівні зі збірками), а середина (pos == null) — як раніше, додати в
    // папку. Це виправляє «при папці на першому місці збірку неможливо
    // поставити перед нею».
    if (dragged && target && targetKind === 'folder') {
      if (folderModalId) folderModalId = null
      if (pos) {
        // Папка виступає звичайним елементом списку: ставимо збірку
        // перед/після неї (commitPackReorder працює і з папками — вони в
        // customLayoutItems). Якщо збірка була В ПАПЦІ — спершу витягуємо
        // її звідти (commitPackReorder не бачить збірок із папок).
        if (folderOfPack(dragged)) removePackFromFolder(dragged)
        commitPackReorder(dragged, target, pos)
      } else {
        addPackToFolder(target, dragged)
      }
      return
    }

    // Дроп збірки на збірку ВСЕРЕДИНІ папки (sidebar розгорнута папка або
    // модальне вікно): збірку, якої ще немає в цій папці, ДОДАЄМО в папку
    // НА МІСЦЕ цільової (а не в кінець списку папки).
    if (dragged && target && targetKind === 'pack') {
      const targetFolder = folderOfPack(target)
      if (targetFolder) {
        const srcFolder = folderOfPack(dragged)
        if (!srcFolder || srcFolder.id !== targetFolder.id) {
          if (folderModalId) folderModalId = null
          addPackToFolderAt(targetFolder.id, dragged, target, pos)
          return
        }
      }
    }

    // Витягування з папки: тягнули збірку, що БУЛА в папці, і дропнули
    // поза папкою (на картку або в порожнє місце головного списку).
    // Папка-джерело розпізнається за прапорцем dragFromFolder, що ставить
    // packPointerDown для карток усередині папки (sidebar + модалка).
    // fromFolderId захоплено на початку pointerDragEnd — endPackDrag його
    // вже обнулив, тож використовуємо копію (інакше гілка мертва).
    if (dragged && fromFolderId) {
      const srcFolder = folderOfPack(dragged)
      if (srcFolder) {
        // Якщо дропнули В ТУ Ж папку (переупорядкування всередині) —
        // commitPackReorder нічого не зробить для карток папки, але ми
        // НЕ витягуємо збірку: дроп на картку папки = reorder у папці.
        const targetFolder = target ? folderOfPack(target) : null
        if (targetFolder && targetFolder.id === srcFolder.id) {
          // Переупорядкування всередині папки.
          reorderInFolder(srcFolder.id, dragged, target!, pos ?? 'after')
        } else {
          // Дроп поза папкою (на іншу картку або порожнє місце) —
          // витягуємо збірку з папки в головний список.
          removePackFromFolder(dragged)
          commitPackReorder(dragged, target, pos)
        }
      }
      return
    }

    // Звичайний reorder у головному списку.
    commitPackReorder(dragged, target, pos)
  }

  function endPackDrag(suppressClick = false) {
    stopDragScroll()
    dragPackId = null
    dragSrcId = null
    dropTargetId = null
    dropPos = null
    dropTargetKind = null
    dragFolderTarget = null
    dragFromFolderId = null
    dragFolderId = null
    dragFolderDown = null
    dragGhostPos = null
    if (suppressClick) {
      suppressPackClick = true
      setTimeout(() => { suppressPackClick = false }, 60)
    }
  }

  // Порядок сітки «Мої збірки» — ЄДИНИЙ список customLayoutItems
  // (збірки + папки в одному порядку, див. вище): папку можна перетягнути
  // МІЖ збірками і навпаки; збірки всередині папки рендеряться всередині
  // розгорнутої секції папки (як акордеон у sidebar). Закріплені збірки
  // йдуть зверху (tabPinned), далі — незакріплені збірки і папки.
  // На вкладці «Шаурма»: tabBuilds уже не містить збірок, згрупованих у
  // serverFolders (виключені на рівні orderedBuildViews через allFolders),
  // тож лишається додати самі плитки папок — так само, як sidebarLayoutItems.
  const gridItems = $derived.by((): LayoutItem[] => {
    if (instanceTab !== 'custom') {
      const packs: LayoutItem[] = tabBuilds.map(bv => ({ kind: 'pack' as const, id: bv.build.id }))
      const folders: LayoutItem[] = serverFolders.map(f => ({ kind: 'folder' as const, id: f.id }))
      return [...packs, ...folders]
    }
    const pinned: LayoutItem[] = tabPinned.map(bv => ({ kind: 'pack' as const, id: bv.build.id }))
    return [...pinned, ...customLayoutItems]
  })

  // commitPackReorder — реальне переміщення (спільне для sidebar і сітки).
  // Закріплені тягнуться лише між закріпленими → оновлюємо packLayout.pinned.
  // Незакріплені (ЗБІРКИ І ПАПКИ в одному списку customLayoutItems)
  // тягнуться лише між незакріпленими → оновлюємо packLayout.order.
  // Папки тепер стоять В order нарівні зі збірками, тож папку можна
  // поставити між двома збірками (і навпаки) — і в sidebar, і в «Мої збірки».
  function commitPackReorder(dragged: string | null, target: string | null, pos: 'before' | 'after' | null) {
    if (!dragged || !target || dragged === target || !pos) return
    const draggedPinned = packLayout.pinned.includes(dragged)
    const targetPinned = packLayout.pinned.includes(target)
    if (draggedPinned !== targetPinned) return

    if (draggedPinned) {
      // Переставляємо в списку закріплених (папки не піняться — сюди
      // потрапляють лише збірки).
      const without = packLayout.pinned.filter(id => id !== dragged)
      const ti = without.indexOf(target)
      if (ti < 0) return
      const at = pos === 'before' ? ti : ti + 1
      without.splice(at, 0, dragged)
      packLayout = { order: packLayout.order, pinned: without }
      savePackLayout()
      return
    }

    // Незакріплені: ЄДИНИЙ порядок збірок і папок (customLayoutItems).
    // Нова позиція рахується по ВИДИМОМУ списку — папка встає саме туди,
    // куди її кинули (перед/після цілі), а не «приблизно в кінець папок».
    const items = customLayoutItems
    const di = items.findIndex(it => it.id === dragged)
    const ti = items.findIndex(it => it.id === target)
    if (di < 0 || ti < 0) return
    const without = items.filter(it => it.id !== dragged)
    const ti2 = without.findIndex(it => it.id === target)
    if (ti2 < 0) return
    const at = pos === 'before' ? ti2 : ti2 + 1
    without.splice(at, 0, items[di])
    // order = нова послідовність (збірки + папки). Закріплені тримаються
    // окремо в pinned; збірки з папок керуються packIds папки.
    packLayout = { order: without.map(it => it.id), pinned: packLayout.pinned }
    savePackLayout()
  }

  // Живий прогрес V2-синхронізації Шаурма-збірок (sync:progress від
  // sync.Runner). Без цього sidebar/тайли/сторінка збірки "зависають":
  // новий рушій шле RunnerProgress, а не SessionProgress старого двигуна.
  // Mode/Stage/NoInternet прокидаються в BuildProgress, щоб UI показував
  // «Розпаковка…», «Зупинено» чи «Немає інтернету» замість завислого 99%.
  function applySyncProgress(p: SyncProgress) {
    if (p.mode === 'complete' || p.mode === 'cancelled' || p.mode === 'error') {
      const { [p.packId]: _drop, ...rest } = downloadByPack
      downloadByPack = rest
      refreshBuilds()
      return
    }
    const prog: BuildProgress = {
      percent: p.overallPercent, bytesDone: p.bytesDownloaded, bytesTotal: p.bytesTotal,
      filesDone: p.filesCompleted, filesTotal: p.filesTotal,
      currentFile: p.currentFile, speedBps: p.speedBps,
      mode: p.mode, stage: p.stage, noInternet: p.noInternet,
    }
    downloadByPack = { ...downloadByPack, [p.packId]: prog }
    patchBuild(p.packId, b => ({
      ...b,
      build: {
        ...b.build,
        // Іконка з'являється у placeholder'а під час імпорту (вбудована
        // icon.png модпаку) — одразу оновлюємо картку/плітку.
        iconUrl: p.iconUrl ?? b.build.iconUrl,
      },
      status: b.status === 'running' ? b.status
        : (b.installed ? 'updating' : 'downloading'),
      progress: prog,
    }))
  }

  // Дія за станом збірки: зупинити / грати / оновити / завантажити.
  // «Стоп» завжди влучає в сесію САМЕ ЦІЄЇ картки (bv.build.id) — навіть
  // коли паралельно на тому самому акаунті йде ще кілька інших збірок.
  function buildAction(bv: BuildView) {
    const st = bv.status
    if (st === 'running') stopInstance(bv.build.id)
    // isLaunchStarting: запуск уже триває (клік прийнявся, йде перевірка
    // модів/RAM/сам LaunchInstance) — ігноруємо повторний клік, поки
    // bv.status ще не встиг стати 'running' з боку бекенду. Без цього
    // нетерплячий гравець міг наклацати «Грати» сотні разів і засипати
    // sessions.Manager помилками «build already running».
    else if (st === 'ready' && !isLaunchStarting(bv.build.id)) launchWithHealthGuard(bv.build.id)
    else if (st === 'not-installed' || st === 'needs-update') downloadPack(bv.build.id)
    else if (st === 'error') refreshBuilds()
  }

  // ── Кнопка під час качки: пауза / продовжити ──────────────────────────
  // Раніше під час downloading/updating кнопка показувала «мертвий» спінер
  // і нічого не робила. Тепер вона РЕАЛЬНО керує завантаженням: клік =
  // пауза, на паузі клік = продовжити (той самий DownloadPack, що й на
  // сторінці збірки).
  function downloadAction(bv: BuildView) {
    if (bv.progress?.mode === 'paused') downloadPack(bv.build.id)
    else App.PauseDownload(bv.build.id)
  }

  // Загальна дія кнопки (sidebar + тайли).
  function packBtnAction(bv: BuildView) {
    // Під час міграції модів запуск неможливий — кнопка дії блокується
    // (показує іконку міграції), щоб випадковий клік не запустив гру
    // «напівпорожньою» без модів.
    if (migrateByPack[bv.build.id]) return
    const st = bv.status
    if (st === 'downloading' || st === 'updating') downloadAction(bv)
    else buildAction(bv)
  }

  // Клас кольору кнопки: під час качки — жовта «пауза», на паузі — зелена
  // «продовжити» (як у кнопки «Грати»).
  // Клас «сяйва» іконки збірки: реальний стан підказується ритмічним
  // сяйвом і у стисненому sidebar, і (за рішенням додати всюди) в
  // .inst-tile теж:
  //   зелений  — гра запущена / завершення якання файлів (finalizing)
  //   оранжевий — качає файли збірки (downloading)
  //   жовтий    — розпаковує файли (extracting)
  //   червоний  — качає Java / лоадер (стадії java/loader запуску)
  //   синій     — отримує маніфест (manifest_fetched) / оновлює файли
  //               збірки (worker_update / updating)
  function glowClass(bv: BuildView, lp?: LaunchProgress | null): string {
    if (lp && !lp.done) {
      if (lp.stage === 'java' || lp.stage === 'loader') return 'glow-red'
      if (lp.stage === 'worker_update') return 'glow-blue'
      return ''
    }
    if (bv.status === 'running') return 'glow-green'
    if (bv.status === 'downloading' || bv.status === 'updating') {
      return downloadStageGlow(bv.progress?.stage)
    }
    return ''
  }

  // Іконка стадії запуску НА КАРТЦІ (sidebar/тайли): поки збірка
  // встановлює Java/лоадер/файли, кнопка дії зникає і показується
  // «жива» іконка без функцій:
  //   checking       → лупа, що слабо рухається вгору-вниз
  //   worker_update  → іконка оновлення, що крутиться 360°
  //   java           → ЧЕРВОНА іконка Java, рухається вгору-вниз
  //   loader         → іконка саме того лоадера (fabric/forge/…), вгору-вниз
  // Кнопка НЕ клікабельна (div, не button) — це індикатор, а не дія.
  function launchStageIcon(stage?: string): { icon: string; cls: string } | null {
    switch (stage) {
      case 'checking': return { icon: 'ti-search', cls: 'st-check' }
      case 'worker_update': return { icon: 'ti-refresh', cls: 'st-update' }
      // Іконка Java в sidebar — ti-coffee (чашка кави = Java), яка точно
      // існує у Tabler 3.46 (старий ti-brand-java був прибраний з набору).
      case 'java': return { icon: 'ti-coffee', cls: 'st-java' }
      case 'loader': return { icon: 'ti-package', cls: 'st-loader' }
      default: return null
    }
  }
  // Іконка стадії СИНХРОНІЗАЦІЇ ФАЙЛІВ (downloading/manifest_fetched/
  // extracting/finalizing) — той самий принцип, що launchStageIcon, але
  // з downloadStageIcon(): своя іконка+клас на кожну стадію якання.
  function syncStageIcon(bv: BuildView): { icon: string; cls: string } | null {
    if (bv.status !== 'downloading' && bv.status !== 'updating') return null
    if (bv.progress?.mode === 'paused') return null
    return downloadStageIcon(bv.progress?.stage)
  }
  // Іконка-IMG конкретного лоадера (для стадії loader): у збірки
  // loader_type, тож показуємо САМЕ її значок (як у формі створення).
  function loaderImgFor(loaderType?: string): string | undefined {
    switch (loaderType) {
      case 'fabric': return fabricIcon
      case 'forge': return forgeIcon
      case 'neoforge': return neoforgeIcon
      case 'quilt': return quiltIcon
      default: return undefined
    }
  }


  function spBtnClass(bv: BuildView): string {
    const st = bv.status
    // «Качається» і «поставлено на паузу» — обидва показують СИНЮ кнопку
    // паузи (з пульсацією у стані паузи). Раніше пауза давала ЗЕЛЕНУ
    // «грати», що плуталась з «готова до гри» — користувач не бачив, що
    // качка стоїть на паузі.
    if (st === 'downloading' || st === 'updating') return 'pause'
    return statusBtnClass(st)
  }

  function spBtnTitle(bv: BuildView): string {
    const st = bv.status
    if (st === 'downloading' || st === 'updating') {
      return bv.progress?.mode === 'paused' ? t('pack.resume') : t('pack.pause')
    }
    return statusTitle(st)
  }

  // Текст кнопки тайла (з текстом).
  function tileBtnLabel(bv: BuildView, lp?: LaunchProgress | null): string {
    const st = bv.status
    if (lp && !lp.done) return launchStageLabel(lp.stage)
    // Запуск уже прийнятий (лок launchStarting стоїть), але lp вже
    // прибрано (перевірка модів завершилась) і bv.status з бекенду ще не
    // наздогнав 'running' — без цього кнопка на мить знову показувала
    // «Грати», наче клік не зарахувався, і провокувала повторний клац.
    if (st === 'ready' && isLaunchStarting(bv.build.id)) return t('pack.starting')
    if (st === 'downloading' || st === 'updating') {
      return bv.progress?.mode === 'paused' ? t('pack.resume') : t('pack.pause')
    }
    return statusTitle(st)
  }

  // Підпис під назвою збірки: під час качки — ЩО саме качається (поточний
  // файл або стадія), під час запуску — стадія, інакше «лоадер · версія».
  // ВАЖЛИВО (скарга «під час оновлення показується стан завантаження,
  // а не оновлення збірки»): для status === 'updating' на ранніх стадіях
  // (manifest_fetched / downloading без конкретного файла, напр. резолв
  // модів CurseForge) показуємо «Оновлення…»; конкретний файл або стадію
  // (extracting/finalizing) — лише коли вони реально відбуваються.
  function packSubText(bv: BuildView, lp?: LaunchProgress | null): string {
    const st = bv.status
    if (lp && !lp.done) return launchStageLabel(lp.stage)
    if (st === 'updating') {
      const stage = bv.progress?.stage
      const cur = bv.progress?.currentFile
      // Стадія 'downloading' = качаються файли, але без імені файла — це
      // фаза планування/резолву (напр. «Резолв модів CurseForge…»).
      if ((stage === 'extracting' || stage === 'finalizing' || stage === 'manifest_fetched') && cur) {
        return cur
      }
      if (stage === 'extracting' || stage === 'finalizing' || stage === 'manifest_fetched') {
        return downloadStageLabel(stage)
      }
      return t('build.status.updating')
    }
    if (st === 'downloading') {
      return bv.progress?.currentFile || downloadStageLabel(bv.progress?.stage)
    }
    return `${bv.build.loaderType} · ${bv.build.mcVersion}`
  }

  function statusBtnClass(st: BuildStatus): string {
    switch (st) {
      case 'running': return 'stop'
      case 'ready': return 'play'
      case 'needs-update': return 'download'
      case 'not-installed': return 'download'
      default: return 'disabled'
    }
  }

  function statusTitle(st: BuildStatus): string {
    switch (st) {
      case 'running': return t('pack.stop')
      case 'ready': return t('pack.play')
      case 'needs-update': return t('pack.update')
      case 'not-installed': return t('pack.download')
      case 'downloading': return t('build.status.downloading')
      case 'updating': return t('build.status.updating')
      default: return ''
    }
  }

  // ── Попередження при закритті лаунчера з активною качкою ──────────────
  // Бекенд (tray.go / QuitLauncher) шле sync:quit-warning, коли користувач
  // намагається закрити вікно/вийти, а збірки ще качаються. Показуємо
  // модалку підтвердження: «закриття перерве завантаження»; лише після
  // явного підтвердження (ConfirmQuitWhileSyncing) закриття проходить.
  async function confirmQuitWhileSyncing(packIDs: string[]) {
    const names = (packIDs || [])
      .map(id => packIndex.find(p => p.id === id)?.name ?? id)
      .join(', ')
    const ok = await confirmDialog(t('quit.syncTitle'), t('quit.syncMsg', { packs: names }), {
      confirmLabel: t('quit.syncConfirm'), cancelLabel: t('cancel'), danger: true
    })
    if (ok) {
      await App.ConfirmQuitWhileSyncing()
    }
  }

  // ── «Поділитися»: закриття лаунчера під час передачі + продовження після краху ──
  async function confirmQuitWhileSharing() {
    const ok = await confirmDialog(
      t('share.quit.title'),
      t('share.quit.body'),
      { confirmLabel: t('share.quit.confirm'), cancelLabel: t('share.quit.stay'), danger: true }
    )
    if (ok) await App.ConfirmQuitWhileSyncing()
  }

  // Недокачане з попереднього сеансу (краш, закриття): вікно ShareRecoveryModal пропонує
  // продовжити / встановити / стерти (нічого не робиться без кліку).
  async function offerShareRecovery() {
    await initShareStore()
    if (shareRecoverable.items.length > 0) shareUi.recoverOpen = true
  }

  // Акаунт за іменем — для голови скіна, на якому запущена збірка.
  function accForName(name: string): Account | undefined {
    return accounts.find(a => a.username === name)
  }

  // Акаунти (знайдені у списку), на яких збірка запущена на ІНШИХ акаунтах,
  // крім поточного — для голів у sidebar/картці/сторінці збірки. Поточний
  // акаунт відфільтровуємо і тут (страховка: build:started може тимчасово
  // додати його в runningOn до перечитування з бекенду).
  function otherRunningAccounts(bv: BuildView | null | undefined): Account[] {
    if (!bv) return []
    const me = activeAccountId
    return (bv.runningOn ?? [])
      .filter(r => r.accountId !== me)
      .map(r => accounts.find(a => a.id === r.accountId)
        ?? (r.accountName ? accounts.find(a => a.username === r.accountName) : undefined))
      .filter((a): a is Account => !!a)
  }

  // ── Pre-launch перевірка модів ──
  // Перед запуском перевіряємо збірку на дублікати модів і відсутні
  // залежності: якщо є проблеми — модалка зі швидкими діями (видалити
  // дублікати / відкрити розділ «Моди» / все одно запустити). Бекенд
  // рахує це локально і швидко, без мережі.
  let preLaunchIssues = $state<PreLaunchModIssues | null>(null)
  // ── Діалог заблокованих модів (імпорт CurseForge zip, як у Prism) ────
  // Подія import:blocked з бекенду: імпорт СТАЄ НА ПАУЗУ і чекає рішення.
  // Користувач обирає: Modrinth fallback / пропустити / скачати вручну
  // (прямі посилання на CurseForge) / скасувати.
  // allowModrinth=false = другий раунд (Modrinth уже спробований, усе, що
  // там було, вже скачано) — діалог ховає кнопку «Завантажити з Modrinth»
  // і лишає лише ручне встановлення / пропустити / скасувати.
  let blockedModsDialog = $state<{ packId: string; mods: BlockedCFMod[]; allowModrinth: boolean } | null>(null)
  function respondBlocked(decision: 'modrinth' | 'skip' | 'manual' | 'cancel', checked: Set<string>) {
    const d = blockedModsDialog
    if (!d) return
    blockedModsDialog = null
    App.RespondBlockedMods(d.packId, decision, [...checked].join(','))
  }
  let pendingLaunchId = $state<string | null>(null)

  // Рекурсивне витягування читабельного повідомлення з помилки, яку може
  // прислати бекенд/Wails у різних формах: рядок, {message: string},
  // {message: {…}}, масив з них тощо. String(err) на об'єкті дає
  // "[object Object]" — саме це користувачі бачили у «Помилці встановлення»
  // при імпорті модпаків, і через це імпорт здавався завислим «без причини».
  function extractErrMessage(err: any, depth = 0): string {
    if (err == null) return ''
    if (typeof err === 'string') return err
    if (depth > 6) return ''
    if (Array.isArray(err)) {
      for (const x of err) {
        const s = extractErrMessage(x, depth + 1)
        if (s) return s
      }
      return ''
    }
    if (typeof err === 'object') {
      // Стандартні ключі Wails/JS: message, error, msg, reason, detail.
      for (const k of ['message', 'error', 'msg', 'reason', 'detail']) {
        if (err[k] != null) {
          const s = extractErrMessage(err[k], depth + 1)
          if (s) return s
        }
      }
      // Кінець гілки: перебираємо значення об'єкта (Go-структура без
      // message — напр. {Message: "…"} великою літерою чи вкладені поля).
      for (const k of Object.keys(err)) {
        const s = extractErrMessage(err[k], depth + 1)
        if (s) return s
      }
    }
    return ''
  }

  // Wails-виклики не мають власного таймауту: якщо бекенд зависне, Promise
  // не зрезолвиться ніколи, і кнопка «Грати» заблокувалась би назавжди.
  // Обгортаємо найважливіші виклики таймаутом, щоб запуск гарантовано не
  // застряг (перевірка модів — порадник, а не ворота до гри).
  function withTimeout<T>(p: Promise<T>, ms: number): Promise<T> {
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error(`timeout after ${ms}ms`)), ms)
      p.then(
        v => { clearTimeout(timer); resolve(v) },
        e => { clearTimeout(timer); reject(e) },
      )
    })
  }

  // ── Pack Doctor — модалка перед запуском ─────────────────────────────────
  // Показується, якщо RunPackHealthCheck повернув overall !== 'ok'.
  // Якщо overall === 'ok' — запуск відбувається без будь-якого тертя.
  let healthModal     = $state(false)
  let healthPackId    = $state('')     // id збірки для модалки
  let healthPackName  = $state('')     // назва для заголовку

  // ── RAM-попередження перед запуском (ТЗ) ──
  // Модалка показується, коли GetLaunchWarnings знайшла проблему:
  //  • showRecommended — ефективна RAM нижча за рекомендовану збірки;
  //  • showLowRam — виділена RAM перевищує фізичну пам'ять (можливий краш).
  // Кнопки: «Запустити» / «Скасувати», слайдер RAM (SetPackRAM — зберігає
  // per-pack override, і збірка більше не попереджає), галочка «не
  // показувати знову для цієї збірки» (DismissPackWarning).
  let ramWarnings = $state<LaunchWarnings | null>(null)
  let ramWarningDismiss = $state(false)
  // Редагована RAM у модалці (МБ). Початкове значення = ефективна RAM.
  let ramWarningRamMb = $state(0)
  // Адреса сервера (кнопка «Приєднатись»), яку треба донести крізь модалку
  // RAM-попередження до фактичного doLaunch — інакше «Запустити все одно»
  // після попередження про RAM забувала б, що це був запуск із
  // приєднанням до конкретного сервера, і стартувала б збірку без нього.
  let pendingLaunchServerAddress = $state('')

  // Відповісти на модалку попередження: proceed=true → запустити.
  // Спершу зберігаємо стан (dismiss / нову RAM), потім запускаємо.
  async function answerRamWarning(proceed: boolean) {
    const w = ramWarnings
    if (!w) return
    ramWarnings = null
    const serverAddress = pendingLaunchServerAddress || undefined
    pendingLaunchServerAddress = ''
    // «Не показувати знову» — ЛИШЕ для цієї збірки (ТЗ: галочка діє лише
    // для цієї збірки; глобальний вимикач — окремо в Налаштуваннях).
    if (ramWarningDismiss) {
      try {
        if (w.showRecommended && !w.recommendedDismissed) await App.DismissPackWarning(w.packId, 'recommended')
        if (w.showLowRam && !w.lowRamDismissed) await App.DismissPackWarning(w.packId, 'lowram')
      } catch { /* не критично */ }
    }
    // Користувач змінив RAM слайдером — зберігаємо як per-pack override:
    // збірка тепер використовує СВОЮ RAM (не з загальних налаштувань).
    if (ramWarningRamMb > 0 && ramWarningRamMb !== w.effectiveRamMb) {
      try { await App.SetPackRAM(w.packId, ramWarningRamMb) } catch { /* не критично */ }
    }
    if (proceed) doLaunch(w.packId, serverAddress)
  }

  // Перевірка RAM-попереджень + запуск. Винесена окремо від launchInstance,
  // щоб шляхи «після виправлення модів» (deleteDuplicatesAndLaunch /
  // launchAnyway) теж проходили крізь неї — інакше збірка з дублікатами
  // модів І RAM-попередженням запускалась би без попередження (баг).
  async function ramCheckOrLaunch(id: string, serverAddress?: string) {
    // RAM-попередження (рекомендована / фізична пам'ять). Бекенд сам
    // враховує глобальний вимикач і «не показувати знову» для збірки.
    const w = await App.GetLaunchWarnings(id).catch(() => null)
    if (w && (w.showRecommended || w.showLowRam)) {
      // RAM-попередження перехоплює запуск: мінімальне вікно прогресу
      // прибираємо, щоб воно не застрягало позаду модалки (запуск ще
      // не почався — графіка прогресу безглузда). Разом з ним прибираємо
      // й індикатор «Перевірка збірки…», щоб кнопка не лишалась у стані
      // «перевіряє» позаду модалки після скасування.
      launchOverlay = null
      const next = { ...launchProgress }
      delete next[id]
      launchProgress = next
      ramWarningDismiss = false
      ramWarningRamMb = w.effectiveRamMb || settings.maxRAM || 4096
      ramWarnings = w
      // «Запустити все одно» (answerRamWarning) не знає про serverAddress —
      // зберігаємо його в pendingLaunchServerAddress, щоб дістати після
      // закриття модалки.
      pendingLaunchServerAddress = serverAddress ?? ''
      return
    }
    await doLaunch(id, serverAddress)
  }

  // Перехоплює запуск: якщо health check повертає warning/error — показує
  // модалку Pack Doctor. При overall==='ok' запускає одразу без тертя.
  // serverAddress — див. коментар у doLaunch: та сама кнопка «Приєднатись»
  // тепер проходить ТОЧНО той самий шлях (health-guard → mod-check →
  // ram-check → launch), що й головна кнопка «Грати».
  async function launchWithHealthGuard(id: string, serverAddress?: string) {
    // Синхронний замок — перший клік проходить, усі наступні (поки цей
    // ще не завершився) одразу виходять, нічого не шлючи на бекенд.
    if (!markLaunchStarting(id)) return
    // Візуальний фідбек ОДРАЗУ, до першого await: RunPackHealthCheck
    // (місце на диску, моди, RAM) може займати секунди на великих збірках,
    // і без індикатора кнопка «Грати» весь цей час виглядала «вільною»
    // для повторного кліку. Стадію показуємо з САМОГО початку, а не лише
    // після завершення перевірки здоров'я (раніше launchInstance ставила
    // checking уже після RunPackHealthCheck — кнопка лишалась «Грати» на
    // всю тривалість перевірки).
    launchProgress = { ...launchProgress, [id]: { instanceId: id, stage: 'checking', done: false, percent: -1 } }
    // Flush pending instance configs (RAM/JVM/window changes) to disk
    // BEFORE the health check — otherwise the backend reads stale values
    // from disk (350ms debounce in PackInstancePage.saveConfig).
    await flushAllPendingConfigs()
    // Перевірка здоров'я ВЖЕ включає сканування модів на дублікати (той
    // самий CheckPreLaunchModIssues). Якщо вона відпрацювала — pre-launch
    // перевірка в launchInstance робила б ТОЙ САМИЙ скан теки вдруге поспіль
    // (скарга: «health check і pre-launch перевірка сканують теку двічі»).
    // healthChecked=true каже launchInstance пропустити повторний скан.
    let healthChecked = false
    try {
      const report = await App.RunPackHealthCheck(id).catch(() => null)
      if (report) {
        healthChecked = true
        if (report.overall !== 'ok') {
          const bv = orderedAllBuildViews.find(b => b.build.id === id)
          healthPackId   = id
          healthPackName = bv?.build.name ?? id
          healthModal    = true
          // serverAddress зберігаємо так само, як для RAM-модалки: Pack
          // Doctor нічого не знає про параметри виклику launchInstance,
          // тож «Запустити все одно» (onLaunchAnyway нижче) дістає адресу
          // звідси, а не забуває про запит «Приєднатись».
          pendingLaunchServerAddress = serverAddress ?? ''
          // Модалка здоров'я чекає на явний вибір користувача (Продовжити/
          // Скасувати) — знімаємо замок тут, а не в finally launchInstance,
          // бо launchInstance в цій гілці взагалі не викликається. Разом із
          // замком прибираємо й індикатор «Перевірка збірки…», інакше кнопка
          // лишилась би в стані «перевіряє» позаду модалки.
          const next = { ...launchProgress }
          delete next[id]
          launchProgress = next
          clearLaunchStarting(id)
          return
        }
      }
    } catch { /* не блокуємо запуск при збої перевірки */ }
    try {
      await launchInstance(id, healthChecked, false, serverAddress)
    } finally {
      // ВАЖЛИВО (баг «другий запуск не працює»): замок поставив САМЕ
      // launchWithHealthGuard викликом markLaunchStarting вище — всередині
      // launchInstance повторний markLaunchStarting(id) повертає false
      // (замок вже стоїть), тож ownLock=false і його finally замок НЕ
      // знімає. Без цього finally після ПЕРШОГО успішного запуску через
      // кнопку «Грати» launchStarting назавжди лишав би packId у мережі
      // заблокованих, і після виходу з гри (status повертається до 'ready')
      // buildAction бачив isLaunchStarting(id) === true і МОВЧКИ ігнорував
      // повторний клік — запускати можна було лише з консолі (вона кличе
      // LaunchInstance напряму, без замка).
      clearLaunchStarting(id)
    }
  }

  async function launchInstance(id: string, skipModCheck = false, skipRamCheck = false, serverAddress?: string) {
    // launchWithHealthGuard вже поставив замок; але launchInstance також
    // викликається напряму (onLaunchAnyway із модалки здоров'я) — там
    // замка ще нема, тож ставимо його і тут (markLaunchStarting ідемпо-
    // тентний: якщо вже стоїть, поверне false і ми просто продовжимо з
    // тим самим замком, не знімаючи його передчасно).
    // skipModCheck — health-guard (launchWithHealthGuard) вже прогнав
    // RunPackHealthCheck, який включає той самий CheckPreLaunchModIssues:
    // тека модів щойно відсканована, повторний скан тут був би дублюванням.
    // skipRamCheck — користувач ЩОЙНО натиснув «Запустити все одно» в
    // модалці Pack Doctor, яка вже показала попередження саме про RAM
    // (пункт "ram" з RunPackHealthCheck). Без цього прапорця
    // ramCheckOrLaunch нижче одразу після закриття Pack Doctor знову
    // запитував GetLaunchWarnings і — оскільки користувач нічого не
    // змінював у RAM — миттєво показував ДРУГЕ, окреме модальне вікно
    // (RamWarningModal) з тим самим по суті питанням. Користувач уже
    // відповів «запустити все одно» один раз; повторне питання про той
    // самий брак RAM у новій модалці — то дублювання, а не нова інформація.
    const ownLock = markLaunchStarting(id)
    try {
      // Позначаємо збірку активною для консолі ПЕРЕД запуском: коли гра
      // стартує зі списку (sidebar/«Мої збірки») без відкриття сторінки,
      // окреме вікно консолі має одразу знати, яка збірка запускається, і
      // не показувати «Запустити» для іншої (баг «запускається попередня
      // збірка»).
      App.SelectActivePack(id)
      // Візуальний фідбек ОДРАЗУ: поки йде перевірка модів, збірка вже
      // показує стадію «Перевірка збірки…» (sidebar/тайли/сторінка через
      // launchProgress). Раніше на великих збірках кнопка «Грати» виглядала
      // «мертвою» без жодного індикатора на час перевірки, і здавалось,
      // що запуск не почався.
      launchProgress = { ...launchProgress, [id]: { instanceId: id, stage: 'checking', done: false, percent: -1 } }
      let issues: PreLaunchModIssues | null = null
      // Дублікати вже перевірені health-guard'ом — пропускаємо повторний скан.
      if (!skipModCheck) {
        try {
          issues = await withTimeout(App.CheckPreLaunchModIssues(id), 15000)
        } catch (err) {
          // Збій або таймаут перевірки НЕ повинен блокувати запуск: гра
          // все одно має стартувати. Помилку не ховаємо повністю — пишемо
          // в консоль, щоб було видно при діагностиці.
          console.warn('[launch] pre-launch mod check failed, launching anyway:', err)
          issues = null
        }
      }
      if (issues?.hasIssues) {
        pendingLaunchId = id
        preLaunchIssues = issues
        // Модалка проблем модів перехоплює запуск — забираємо індикатор
        // «Перевірка збірки…» (той самий принцип, що для модалки здоров'я).
        const next = { ...launchProgress }
        delete next[id]
        launchProgress = next
        // Так само як у гілці health-модалки: чекаємо явного рішення
        // користувача (доіграти проблеми/скасувати) — знімаємо замок,
        // інакше кнопка «Грати» лишиться «мертвою» до наступного кліку.
        if (ownLock) clearLaunchStarting(id)
        return
      }
      // Нормальний шлях: індикатор «Перевірка збірки…» НЕ прибираємо —
      // він тримається до першого launch:progress з бекенду (LaunchInstance
      // одразу шле стадію checking), тож кнопка не мигає «Грати» між
      // перевірками і реальним стартом.
      if (skipRamCheck) {
        await doLaunch(id, serverAddress)
      } else {
        await ramCheckOrLaunch(id, serverAddress)
      }
    } catch (e: any) { toast(t('launchError', { message: e }), 'error') }
    finally {
      // doLaunch() всередині ramCheckOrLaunch виставляє launchedPackId/
      // gameRunning ще до цього finally — замок можна знімати завжди:
      // або гра вже стартувала (bv.status невдовзі стане 'running' і
      // кнопка сама заблокується тим шляхом), або стався збій і треба
      // дозволити повторний клік.
      if (ownLock) clearLaunchStarting(id)
    }
  }

  // doLaunch — єдина точка виклику бекенд-запуску гри для БУДЬ-ЯКОЇ
  // кнопки (головна «Грати» на сторінці збірки, «Перезапустити» в
  // консолі, «Приєднатись» до сервера в розділі «Сервери» чи у віджеті
  // офіційного сервера, закріплені сервери в сайдбарі). serverAddress —
  // необов'язкова адреса сервера, до якого одразу приєднати гравця
  // (--quickPlayMultiplayer): якщо задана, викликаємо
  // App.LaunchInstanceOnServer замість App.LaunchInstance, а решта
  // поведінки (launchedPackId/gameRunning/HideToTray/обробка скасування)
  // лишається АБСОЛЮТНО такою самою — це і є вимога «кожна кнопка
  // запуску має працювати як головна на сторінці збірки»: раніше
  // «Приєднатись» викликало App.LaunchInstanceOnServer НАПРЯМУ з
  // дочірніх компонентів (PackOfficialServerWidget/PackServersSection/
  // joinPinnedServer), в обхід усього ланцюга launchWithHealthGuard →
  // launchInstance → ramCheckOrLaunch → doLaunch — тобто без health-guard
  // (Pack Doctor), без перевірки модів, без RAM-попередження і, найголовніше,
  // навіть у самому doLaunch раніше не було спільного коду для serverAddress,
  // тож кожен виклик сам вирішував, як саме викликати бекенд. Тепер усі ці
  // кнопки проходять ту саму гілку serverAddress нижче — той самий шлях,
  // що й головна кнопка, лише з address на останньому кроці.
  async function doLaunch(id: string, serverAddress?: string) {
    try {
      // Ярлик збірки з персональними опціями (акаунт/автоприєднання) —
      // передаємо їх у LaunchInstanceShortcut: бекенд перекриває акаунт та
      // автоприєднання збірки РІВНО для цього запуску, решта параметрів
      // (Java, RAM, моди, попередні перевірки) — як при звичайному запуску.
      // Опції одноразові — споживаються лише для наступного запуску з
      // ярлика; звичайний «Грати» їх не бачить (вони скидаються нижче).
      // serverAddress (кнопка «Приєднатись») сильніший за ярлик: явний клік
      // саме по цьому серверу зараз важливіший за збережені опції ярлика.
      const so = shortcutLaunchOpts
      shortcutLaunchOpts = { accountId: '', autoJoinServer: '' }
      if (serverAddress) {
        const acc = activeAccount()
        if (acc) await App.SetLaunchClickAccount(id, acc.id)
        await App.LaunchInstanceOnServer(id, serverAddress)
      } else if (so.accountId || so.autoJoinServer) {
        await App.LaunchInstanceShortcut(id, so.accountId, so.autoJoinServer)
      } else {
        // Фіксуємо акаунт РІВНО в момент кліку, ДО await LaunchInstance —
        // раніше LaunchInstance сам читав активний акаунт на бекенді, а
        // це могло статись уже ПІСЛЯ того, як гравець встиг перемкнути
        // акаунт (напр. поки тривала перевірка/встановлення файлів
        // попереднього запуску іншої збірки) — гра стартувала не на тому
        // акаунті, на якому натискали «Грати». SetLaunchClickAccount —
        // окремий, швидкий синхронний виклик перед довгим LaunchInstance.
        const acc = activeAccount()
        if (acc) await App.SetLaunchClickAccount(id, acc.id)
        await App.LaunchInstance(id)
      }
      launchedPackId = id
      gameRunning = true
      gameCrashed = false
      // Вікно лаунчера під час гри (gameLaunchAction) згортає/ховає
      // backend — див. window_behavior.go.
    } catch (e: any) {
      // Скасування користувачем (кнопка «Зупинити» під час качки
      // компонентів) — не помилка: тост «Запуск скасовано» вже показав
      // обробник launch:cancelled, червону помилку не дублюємо.
      const wasCancelled = launchCancelled.has(id)
      if (wasCancelled) {
        const cancelled = new Set(launchCancelled)
        cancelled.delete(id)
        launchCancelled = cancelled
        return
      }
      toast(t('launchError', { message: e }), 'error')
    }
  }

  // Запуск збірки з ярлика на робочому столі (--launch <packID> [--account
  // <id>] [--server <host>]): показуємо мінімальне вікно прогресу запуску і
  // запускаємо через звичайний launch-шлях (перевірки RAM/модів/акаунта —
  // ті самі, що й з кнопки «Грати»). Персональні опції ЯРЛИКА (акаунт/
  // автоприєднання) зберігаємо у shortcutLaunchOpts — doLaunch передасть їх
  // у LaunchInstanceShortcut, і вони перекриють акаунт/автоприєднання збірки
  // рівно для цього запуску (як у Prism); решта параметрів — звичайні.
  //
  // ВАЖЛИВО: на сторінку збірки НЕ переходимо (баг «переносить на сторінку
  // збірки»): користувач клікнув ярлик і хоче одразу гру, а не сторінку.
  // Якщо збірка ще не завантажена — показуємо оверлей зі станом, юзер
  // побачить помилку/прогрес і зможе скасувати (CancelGameInstall).
  async function launchFromShortcut(pid: string, opts?: { accountId?: string; autoJoinServer?: string }) {
    shortcutLaunchOpts = {
      accountId: opts?.accountId || '', autoJoinServer: opts?.autoJoinServer || '',
    }
    const bv = builds.find(b => b.build.id === pid)
    // Іконка: PNG збірки (data URL через packAsset) або пресет-гліф.
    const iconUrl = bv?.build.iconUrl ? packAsset(bv.build.iconUrl) || '' : ''
    const presetIcon = bv?.build.icon && !bv.build.iconUrl ? bv.build.icon : ''
    if (bv?.status === 'ready') {
      // Показуємо мінімальне вікно прогресу запуску (без повного UI):
      // назва/іконка збірки + прогрес-бар стадій. Зникне на game:started
      // (гра пішла), на помилку запуску або коли юзер натисне «Скасувати».
      launchOverlay = {
        packId: pid, name: bv.build.name,
        iconUrl, presetIcon, color: bv.build.color || '',
      }
      try { await ramCheckOrLaunch(pid) } catch (e: any) {
        launchOverlay = null
        toast(t('launchError', { message: e }), 'error')
      }
    } else {
      // Збірка не завантажена (або ще качається). Показуємо оверлей і
      // запускаємо через buildAction — він сам скачає і запустить.
      launchOverlay = {
        packId: pid, name: bv?.build.name || pid,
        iconUrl, presetIcon, color: bv?.build.color || '',
      }
      try { await ramCheckOrLaunch(pid) } catch (e: any) {
        launchOverlay = null
        toast(t('launchError', { message: e }), 'error')
      }
    }
  }

  // «Скасувати» на мінімальному вікні прогресу запуску: зупиняємо запуск
  // цієї збірки і повертаємось до звичайного інтерфейсу.
  async function cancelOverlayLaunch() {
    const pid = launchOverlay?.packId
    launchOverlay = null
    if (!pid) return
    // Під час встановлення компонентів (Java/лоадер/Minecraft) — м'яко
    // скасовуємо качку (раніше тут був StopInstance, який у фазі
    // встановлення НІЧОГО не робив — гра ще не запущена як процес, і
    // «Скасувати» просто закривало вікно, а качка тривала далі).
    try { await App.CancelGameInstall(pid) } catch { /* вже зупинено */ }
    // Якщо гра все ж встигла стартувати (сесія вже зареєстрована) —
    // зупиняємо її як звичайно (помилка ігнорується: нічого не запущено).
    try { await App.StopInstance(pid) } catch { /* вже зупинено */ }
  }

  // «Видалити дублікати»: для кожної групи лишаємо один увімкнений файл,
  // решту видаляємо і продовжуємо запуск.
  async function deleteDuplicatesAndLaunch() {
    const id = pendingLaunchId
    const issues = preLaunchIssues
    preLaunchIssues = null
    pendingLaunchId = null
    if (!id || !issues) return
    for (const g of issues.duplicates ?? []) {
      const keep = g.mods.find(m => m.enabled) ?? g.mods[0]
      for (const m of g.mods) {
        if (m.fileName !== keep.fileName) {
          try { await App.DeleteMod(id, m.fileName) } catch { /* не критично */ }
        }
      }
    }
    // Після виправлення модів знову проходимо RAM-попередження
    // (ramCheckOrLaunch), інакше воно мовчки пропустилось би.
    await ramCheckOrLaunch(id)
  }

  // «Все одно запустити»: користувач вирішив ігнорувати проблеми модів —
  // але RAM-попередження все одно має пройти (окрема перевірка).
  function launchAnyway() {
    const id = pendingLaunchId
    preLaunchIssues = null
    pendingLaunchId = null
    if (id) ramCheckOrLaunch(id)
  }

  // «Відкрити моди»: сторінка збірки одразу на вкладці «Моди» — там
  // користувач встановить залежності чи розбереться з дублікатами.
  // Якщо сторінка збірки ВЖЕ відкрита (запуск іде саме з неї), просте
  // setPackUI не спрацює — вкладка читається лише при маунті. Тому
  // додатково збільшуємо modsFocusSignal: PackInstancePage на це
  // перемикається на «Моди» навіть без ремаунта, а PackModsSection
  // одразу розгортає і підсвічує віджет дублікатів.
  function openModsToFix() {
    const id = pendingLaunchId
    preLaunchIssues = null
    pendingLaunchId = null
    if (id) {
      try { setPackUI(id, { tab: 'mods', scroll: 0 }) } catch { /* ок */ }
      modsFocusSignal++
      openPackPage(id)
    }
  }

  // Застарілий шлях без контексту конкретної збірки («зупинити останню
  // запущену») — лишений там, де ID збірки справді невідомий (напр. трей).
  // Скрізь, де ID картки/сторінки відомий, використовувати stopInstance.
  async function stopGame() {
    if (settings.confirmOnStop) {
      const ok = await confirmDialog(t('pack.stopConfirm'), t('pack.stopConfirmHint'), {
        confirmLabel: t('pack.stop'), cancelLabel: t('cancel'), danger: true
      })
      if (!ok) return
    }
    try { await App.StopGame() }
    catch (e: any) { toast(t('error.withMessage', { message: e }), 'error') }
  }

  // Зупиняє КОНКРЕТНУ збірку (packId) — завжди влучає в потрібну сесію,
  // навіть коли на тому самому акаунті паралельно йде кілька інших
  // збірок. Це основний шлях для кнопок "Стоп" на сторінці/картці/
  // закріплених серверах — контекст packId там завжди відомий.
  // Скасувати встановлення компонентів гри (кнопка «Стоп» під час
  // launchProgress): м'яко зупиняє качку, .part лишається для резюме.
  async function cancelGameInstall(instanceId: string) {
    try { await App.CancelGameInstall(instanceId) }
    catch (e: any) { toast(t('error.withMessage', { message: e }), 'error') }
  }

  async function stopInstance(packId: string) {
    if (settings.confirmOnStop) {
      const ok = await confirmDialog(t('pack.stopConfirm'), t('pack.stopConfirmHint'), {
        confirmLabel: t('pack.stop'), cancelLabel: t('cancel'), danger: true
      })
      if (!ok) return
    }
    try { await App.StopInstance(packId) }
    catch (e: any) { toast(t('error.withMessage', { message: e }), 'error') }
  }

  // Відкрити браузер з контекстом збірки (кнопка «Шукати моди» в
  // менеджері модів / «Додати з браузера» у вкладці «Контент»). Назва/
  // версія/лоадер беруться зі збірки, щоб пошук одразу був відфільтрований
  // під неї. projectType — тип контенту, на якому відкриється браузер
  // ('mod' | 'resourcepack' | 'datapack' | 'shader'); порожній = «Моди».
  function openBrowserForPack(id: string, projectType?: string) {
    const bv = builds.find(b => b.build.id === id)
    // ВАЖЛИВО: navigate('browser') скидає browserPackContext/browserProject
    // (це треба для входу з сайдбара), тому спершу навігуємо, а ЛИШЕ ПОТІМ
    // виставляємо контекст збірки — інакше «Шукати моди» відкривало браузер
    // без контексту (рев'ю: баг втрати контексту).
    navigate('browser')
    browserPackContext = bv
      ? { id: bv.build.id, name: bv.build.name, mcVersion: bv.build.mcVersion, loader: bv.build.loaderType, projectType }
      : null
    // Контексти збірки і сервера взаємовиключні (ContentBrowser сам надає
    // пріоритет serverContext, коли обидва задані) — скидаємо інший явно,
    // щоб «Шукати моди» після відвідування сторінки сервера не лишало
    // застарілий browserServerContext, який непомітно продовжив би
    // фільтрувати пошук під той сервер.
    browserServerContext = null
  }

  // Спільний резолвер цілі-сервера: за запитаним видом контенту (mods/
  // plugins) і контекстом ядра повертає реальний (kind, loader). Якщо
  // запитаний вид ядру не підходить (Velocity/Paper на «Моди», Fabric на
  // «Плагіни»), береться той, що ядро підтримує (ctx.defaultKind), —
  // замість помилки «ядро не підтримує моди» для сервера, який просто
  // працює з плагінами. Порожній loader = ядро не підтримує НІЧОГО
  // (vanilla).
  type ServerCtx = { mcVersion: string; modLoader: string; pluginLoader: string; defaultKind?: string }
  function resolveServerTarget(ctx: ServerCtx | null, kind: 'mods' | 'plugins'): { kind: 'mods' | 'plugins'; loader: string } {
    if (!ctx) return { kind, loader: '' }
    const loaderOf = (k: 'mods' | 'plugins') => (k === 'mods' ? ctx.modLoader : ctx.pluginLoader) || ''
    if (loaderOf(kind)) return { kind, loader: loaderOf(kind) }
    const alt: 'mods' | 'plugins' = kind === 'mods' ? 'plugins' : 'mods'
    if (loaderOf(alt)) return { kind: alt, loader: loaderOf(alt) }
    return { kind, loader: '' }
  }

  // Відкрити браузер з контекстом СЕРВЕРА (кнопка «Знайти онлайн» у
  // менеджері модів/плагінів сторінки сервера, ServerContentTab.svelte).
  // На відміну від openBrowserForPack: версія/лоадер беруться не зі
  // списку builds (сервери там не зберігаються — інша сутність на
  // бекенді), а з App.ServerBrowserContext(id) — той самий виклик, який
  // ServerContentTab сам робить, щоб визначити browseLoader/
  // browseUnsupported для дизейблу кнопки; тут ідентична логіка потрібна
  // ще раз, бо App.svelte і ServerContentTab — окремі компоненти без
  // спільного стану сервера.
  async function openBrowserForServer(id: string, kind: 'mods' | 'plugins') {
    let ctx: (ServerCtx & { name?: string }) | null = null
    let name = id
    try {
      const [c, srv] = await Promise.all([App.ServerBrowserContext(id), App.GetServer(id)])
      ctx = c
      name = srv?.name || id
    } catch (e) { console.error(e) }
    const resolved = resolveServerTarget(ctx, kind)
    const loader = resolved.loader
    kind = resolved.kind
    if (!ctx || !loader) {
      // Те саме «це ядро не підтримує моди/плагіни», яке ServerContentTab
      // сам показує біля кнопки (browseUnsupported) — тут дублюємо як
      // toast, бо кнопка технічно вже задизейблена в цьому стані і клік
      // сюди дійти не повинен; це підстраховка на випадок гонки станів
      // (контекст сервера ще не встиг завантажитись при кліку). Хардкод
      // рядка, а не t(...) — сторінка серверів взагалі не заведена в
      // систему перекладів (ServerContentTab.svelte та сусідні файли
      // пишуть українські рядки прямо в шаблон), тож єдиний t()-ключ тут
      // виглядав би чужорідно й однаково не мав би пари в інших мовах.
      toast(kind === 'mods' ? 'Це ядро не підтримує моди' : 'Це ядро не підтримує плагіни', 'error')
      return
    }
    navigate('browser')
    browserServerContext = { id, name, mcVersion: ctx.mcVersion, loader, kind }
    // Дзеркально до openBrowserForPack вище: скидаємо контекст ЗБІРКИ,
    // щоб застарілий browserPackContext (з попереднього візиту в
    // менеджер модів якоїсь збірки) не лишався і не переплутався з
    // щойно заданим контекстом сервера.
    browserPackContext = null
  }

  // Обрати сервер ціллю встановлення ПРЯМО у браузері контенту (перемикач
  // «Збірки ↔ Сервери» у фільтрі ContentBrowser). На відміну від
  // openBrowserForServer вище НЕ навігує — користувач уже в браузері, і
  // перехід знищив би його пошук/фільтри. Логіка контексту — та сама:
  // loader залежить від вкладки (modLoader для «Модів», pluginLoader для
  // «Плагінів»), бо сервер може тримати їх різними ядрами.
  async function pickBrowserServer(id: string, kind: 'mods' | 'plugins') {
    let ctx: ServerCtx | null = null
    let name = id
    try {
      const [c, srv] = await Promise.all([App.ServerBrowserContext(id), App.GetServer(id)])
      ctx = c
      name = srv?.name || id
    } catch (e) { console.error(e) }
    const resolved = resolveServerTarget(ctx, kind)
    const loader = resolved.loader
    kind = resolved.kind
    if (!ctx || !loader) {
      toast(kind === 'mods' ? 'Це ядро не підтримує моди' : 'Це ядро не підтримує плагіни', 'error')
      return
    }
    browserPackContext = null
    browserServerContext = { id, name, mcVersion: ctx.mcVersion, loader, kind }
  }

  // Обрати сервер ціллю встановлення зі СТОРІНКИ ПРОЄКТУ (ModpackPage).
  // Раніше сторінка вміла вибирати лише збірки — сервер не можна було
  // вибрати взагалі. Сервер записуємо в той самий browserPackContext із
  // isServer:true (ModpackPage розгалужує встановлення за цим прапорцем),
  // а loader залежить від типу контенту: mod → modLoader, plugin →
  // pluginLoader. Якщо ядро цей вид контенту не підтримує — цілі не міняємо.
  async function pickProjectServer(id: string, kind: 'mods' | 'plugins') {
    let ctx: ServerCtx | null = null
    let name = id
    try {
      const [c, srv] = await Promise.all([App.ServerBrowserContext(id), App.GetServer(id)])
      ctx = c
      name = srv?.name || id
    } catch (e) { console.error(e) }
    const resolved = resolveServerTarget(ctx, kind)
    const loader = resolved.loader
    if (!ctx || !loader) {
      toast(kind === 'mods' ? 'Це ядро не підтримує моди' : 'Це ядро не підтримує плагіни', 'error')
      return
    }
    browserPackContext = { id, name, mcVersion: ctx.mcVersion, loader, isServer: true }
  }

  // Відкрити сторінку модпаку з браузера (клік по картці модпаку).
  //
  // packCtx — контекст збірки, з якою була відкрита картка (переданий
  // ContentBrowser: або зовнішній packContext, або фільтр збірки, який
  // користувач сам обрав на сторінці браузера прямо зараз). Присвоюємо
  // ЗАВЖДИ (а не лише коли browserPackContext порожній) — інакше перший-
  // ліпший клік по картці «застряг» би своїм контекстом назавжди: зміна
  // фільтра збірки в ContentBrowser після повернення назад не діяла б на
  // вже відкриту раніше сторінку проєкту.
  // openBrowserProject — сторінка проєкту (ModpackPage) відкривається з
  // packCtx, отриманим від ContentBrowser.openEntry: для звичайної збірки
  // це browserPackContext-подібний об'єкт; для сервера — ТОЙ САМИЙ тип
  // полів, але з isServer:true (ContentBrowser формує його сам із
  // serverContext, коли той активний — див. openEntry у ContentBrowser.
  // svelte). Записуємо в browserPackContext незалежно від isServer:
  // ModpackPage сама розгалужує встановлення за цим прапорцем, тож тут
  // окремого browserServerContext-запису не потрібно — сторінка проєкту
  // отримує контекст одним полем, яким би він не був.
  function openBrowserProject(source: string, projectId: string, packCtx?: { id: string; name: string; mcVersion: string; loader: string; isServer?: boolean } | null) {
    browserProject = { source, projectId }
    browserAuthor = null
    browserPackContext = packCtx ?? null
  }

  // Клік по моду в менеджері модів (сторінка збірки) → перейти на сторінку
  // «Браузер контенту» й одразу відкрити сторінку цього мода. На відміну
  // від openBrowserProject (використовується вже НА сторінці браузера),
  // тут ще й треба ПЕРЕЙТИ на сторінку — аналогічно navigate('browser'),
  // але без скидання browserProject одразу після встановлення.
  function openModPageFromPack(source: string, projectId: string) {
    if (currentPage === 'instances' && openPackId) flashDeactivate(null, openPackId)
    else if (currentPage !== 'browser') flashDeactivate(currentPage, null)
    currentPage = 'browser'; openPackId = null; showCreatePack = false
    browserAuthor = null
    browserProject = { source, projectId }
  }

  // Відкрити сторінку автора (клік по автору на сторінці проєкту):
  // показується поверх проєкту, «назад» повертає на той самий проєкт.
  function openBrowserAuthor(source: string, authorId: string, authorName: string) {
    browserAuthor = { source, authorId, authorName }
  }

  // Авто-збереження: зміни налаштувань зберігаються одразу (без кнопки
  // «Зберегти») з маленькою дебаунс-затримкою, щоб не писати диск на
  // кожен клік. Візуальні ефекти (акцент/мова/тема/шрифт) застосовуються
  // миттєво.
  let saveTimer: ReturnType<typeof setTimeout> | undefined

  // ── Готові пресети власної теми ──
  // «Споріднені» набори кольорів (фон/картка/текст), які гармонують між
  // собою, — на відміну від трьох окремих довільних кольорів. Кожен
  // ── Забавна анімація логотипа (топ sidebar) ──
  // Клік по логотипу → «поп» самої іконки (стрибок + хитання + спалах
  // яскравості) і пушка з 6 райдужних цяток навколо неї (напрямок/дистанція/
  // колір — у CSS-змінних, цятки самі видаляються по onanimationend).
  // Глобальний вимикач анімацій (Налаштування → Вигляд) обнулить її до 1ms.
  function updateSetting(key: string, v: any) {
    (settings as any)[key] = v
    if (key === 'accent' || key === 'accentCustom' || key === 'accentCustom2' || key === 'accentCustomMode') {
      applyAccent(settings.accent, settings.accentCustom, (settings as any).accentCustomMode, (settings as any).accentCustom2)
    }
    if (key === 'language') setLanguage(v)
    if (key === 'theme' || key === 'themeCustomBg' || key === 'themeCustomCard' || key === 'themeCustomText') {
      applyTheme((settings as any).theme, { bg: (settings as any).themeCustomBg, card: (settings as any).themeCustomCard, text: (settings as any).themeCustomText })
    }
    if (key === 'animationsEnabled') applyAnimations(v !== false)
    if (key === 'performanceMode') applyPerformanceMode(!!v)
    if (key === 'sidebarAnimAlways') sidebarAnimAlways = !!v
    if (key === 'pinnedServersCompact') pinnedServersCompact = !!v
    if (key === 'font' || key === 'fontPath') applyFont((settings as any).font, (settings as any).fontPath).catch(() => {})
    // Шрифт(и) консолі: завантажуємо faces одразу при зміні будь-якого
    // налаштування шрифту — консоль і переглядач оновляться на льоту.
    if (key === 'consoleFontPath' || key === 'consoleFontFamily' || key === 'consoleFontMode' ||
        key === 'consoleInfoFont' || key === 'consoleWarnFont' || key === 'consoleErrorFont') {
      loadConsoleFonts(consoleFontValues(settings), (settings as any).consoleFontPath).catch(() => {})
    }
    clearTimeout(saveTimer)
    saveTimer = setTimeout(async () => {
      try {
        await App.SaveSettings(settings)
      } catch (e: any) {
        toast(t('error.withMessage', { message: e }), 'error')
      }
    }, 350)
  }

  // Теки та шляхи — ліниве завантаження: шляхи читаються лише коли
  // відкривається вкладка «Теки та шляхи» (реальні значення з бекенду,
  // з урахуванням кастомних override у launcher-location.json).
  let folderPathsLoaded = false
  async function loadFolderPaths(force = false) {
    if (folderPathsLoaded && !force) return
    folderPathsLoaded = true
    try { folderPaths = await App.GetFolderPaths() } catch (e) {
      // Помилка читання — дозволяємо повторну спробу, скидаючи прапорець.
      folderPathsLoaded = false
      console.error(e)
    }
  }

  // Зміна папки даних лаунчера (baseDir): вибір у діалозі → бекенд
  // перемігрує всі дані (збірки, Java, кеш, логи) у нову теку і видаляє
  // стару → перечитування шляхів.
  async function setFolder(kind: string) {
    // ТЗ: теки не можна міняти, поки запущена якась збірка — перенесення
    // даних під живою грою впаде на заблокованих файлах, а зміна теки
    // збірок «загубила» б активну збірку.
    if (gameRunning) { toast(t('settings.folders.runningBlocked'), 'warn'); return }
    if (kind !== 'baseDir') return
    const p = await App.BrowseFolder()
    if (!p) return
    try {
      await App.SetFolderPath(kind, p)
      toast(t('settings.saved'), 'success')
      await loadFolderPaths(true)
      // Папка даних змінилась — перечитуємо налаштування, щоб поля
      // instanceDir/javaPath показували актуальні шляхи.
      settings = await App.GetSettings()
      // Зміна папки даних впливає на розміри тек — змушуємо вкладку
      // «Пам'ять і кеш» перерахувати зайняте місце при наступному відкритті.
      storageLoaded = false
      // Список збірок перечитуємо: шляхи іконок/фонів кастомних збірок у
      // БД пере-маплені на нову теку — свіжий GetBuilds віддасть актуальні
      // IconURL, і кеш асетів (ключ = URL) підтягне нові файли.
      await refreshBuilds()
    } catch (e: any) { toast(t('error.withMessage', { message: e }), 'error') }
  }

  // Повернути папку даних лаунчера до системного дефолту.
  async function resetBaseDir() {
    if (!folderPaths) return
    const ok = await confirmDialog(t('settings.folders.reset'), t('settings.folders.resetConfirm'), {
      confirmLabel: t('settings.folders.reset'), cancelLabel: t('cancel'), danger: false
    })
    if (!ok) return
    if (gameRunning) { toast(t('settings.folders.runningBlocked'), 'warn'); return }
    try {
      await App.SetFolderPath('baseDir', folderPaths.defaultBaseDir)
      toast(t('settings.saved'), 'success')
      await loadFolderPaths(true)
      settings = await App.GetSettings()
    } catch (e: any) { toast(t('error.withMessage', { message: e }), 'error') }
  }

  // Підрахунок розміру тек — лінивий: рахуємо лише коли відкривається
  // вкладка «Пам'ять і кеш» (повний обхід installations/java може бути
  // важким для багатогігабайтних збірок, тому не робимо це при старті
  // чи після кожного завантаження).
  let storageLoaded = false
  // Скільки чекати на аналіз вкладки «Пам'ять і кеш» перед тим, як визнати
  // його «завислим» (перевикористовуємо наявний withTimeout, див. вище).
  // Сканування текст великих збірок чесно може йти довго, тому таймаут
  // щедрий — це не обмеження швидкості, а лише гарантія «не назавжди»:
  // навіть якщо бекенд-скан застряг (зациклений InheritsFrom у version.json,
  // недоступний диск тощо), UI ЗАВЖДИ завершує його за розумний час —
  // прапорці busy/inFlight скидаються, і вкладка відкривається знову,
  // замість «заголовок міняється, а контент ні» назавжди.
  const STORAGE_SCAN_TIMEOUT = 120_000
  const JAVA_SCAN_TIMEOUT = 60_000
  // Фікс «фрізу вкладки при виході під час аналізу»: сканування розмірів
  // тепер не запускається повторно, поки попереднє ще в роботі (runId —
  // який виклик «останній»; застарілі відповіді, що прийшли після повторного
  // входу у вкладку, не перезаписують свіжі дані). Без цього повторний вхід
  // у «Пам'ять і кеш» під час аналізу лишав вкладку в стані вічного
  // скелетона — допомагав лише перезапуск лаунчера.
  let storageLoadRun = 0
  let storageLoadInFlight = false
  async function loadStorageInfo(force = false) {
    if ((storageLoaded && !force) || storageLoadInFlight) return
    storageLoadInFlight = true
    storageLoaded = true
    const run = ++storageLoadRun
    // Поки бекенд рахує розміри (може бути довго на великих збірках) —
    // показуємо скелетон з прогрес-баром, а не порожню картку.
    storageLoading = true
    storageScan = null
    try {
      try {
        storageInfo = await withTimeout(App.GetStorageUsage(), STORAGE_SCAN_TIMEOUT)
      } catch (e) {
        // Помилка обходу тек (напр. недоступний диск) або завислий скан —
        // дозволяємо повторну спробу, скидаючи прапорець завантаженості.
        storageLoaded = false
        console.error(e)
      }
      try {
        storagePacks = (await withTimeout(App.GetStoragePacks(), STORAGE_SCAN_TIMEOUT)) ?? []
      } catch (e) {
        storagePacks = []
        storageLoaded = false
        console.error(e)
      }
    } finally {
      // Завершення ЗАВЖДИ, навіть якщо один із сканів завис назавжди:
      // скидаємо прапорець, щоб вкладка могла відкриватись знову.
      if (run === storageLoadRun) {
        storageLoading = false
        storageScan = null
      }
      storageLoadInFlight = false
    }
  }

  // Швидке видалення збірки прямо зі списку «Встановлені збірки»
  // (вкладка «Пам'ять і кеш»): те саме підтвердження, що й зі сторінки
  // збірки (deletePack), і перерахування розмірів після видалення.
  async function deletePackFromStorage(id: string) {
    await deletePack(id)
    // Розміри могли змінитись — перечитуємо список і загальну статистику
    // (force — навіть якщо вкладку вже відкривали, щоб цифри не застаріли).
    storagePacks = (await App.GetStoragePacks()) ?? []
    loadStorageInfo(true)
    // Видалення збірки могло змінити набір «потрібної Java» — одразу
    // перезапускаємо аналіз, щоб інлайн-підсумок не лишався застарілим
    // (раніше javaAnalyzedOnce не скидався і авто-аналіз більше не йшов).
    unusedJava = null
    analyzeUnusedJava()
  }

  // Очистити логи лаунчера + логів/краш-репортів усіх збірок.
  async function clearPackLogs() {
    const ok = await confirmDialog(t('settings.storage.clearLogs'), t('settings.storage.clearLogsConfirm'), {
      confirmLabel: t('settings.storage.clearLogs'), cancelLabel: t('cancel'), danger: true
    })
    if (!ok) return
    try {
      await App.ClearPackLogs()
      toast(t('settings.storage.logsCleared'), 'success')
      loadStorageInfo(true)
    } catch (e: any) { toast(t('error.withMessage', { message: e }), 'error') }
  }

  // Експорт тихого журналу помилок лаунчера (launcher_errors.go).
  // Відкриває Save-діалог і записує JSON з усіма накопиченими помилками.
  // Після збереження — тост з RevealPath, щоб одразу відкрити теку.
  let exportingErrors = $state(false)
  async function exportLauncherErrors() {
    if (exportingErrors) return
    exportingErrors = true
    try {
      const path = await App.ExportLauncherErrors()
      if (path) {
        toast(t('settings.about.errorLogSaved'), 'success')
        App.RevealPath(path).catch(() => {})
      }
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      exportingErrors = false
    }
  }

  // Аналіз невикористовуваної Java (вкладка «Пам'ять і кеш»): бекенд
  // сканує java-<major>/ підтеки і порівнює з потрібними мажорами всіх
  // ВСТАНОВЛЕНИХ збірок. Показуємо лише кандидатів на видалення; якщо
  // користувач підтвердить — видаляємо їх (потрібні перевстановляться
  // автоматично при наступному запуску).
  let unusedJava = $state<UnusedJavaInfo | null>(null)
  let javaBusy = $state(false)
  // javaModalOpen — модалка «Аналіз невикористовуваної Java»: показує ПРОЦЕС
  // аналізу (спінер, поки бекенд сканує java-<major>/ підтеки), а потім —
  // результати (кожен мажор з розміром і статусом «використовується/ні»).
  // javaAnalyzedOnce — авто-аналіз при відкритті вкладки «Пам'ять і кеш»
  // (користувач просив: «немає авто-аналізу, лише по кнопці» — тепер є),
  // щоб результати не перезапускались щоразу при відкритті вкладки.
  let javaModalOpen = $state(false)
  let javaAnalyzedOnce = $state(false)

  async function analyzeUnusedJava() {
    if (javaBusy) return
    javaBusy = true
    unusedJava = null
    javaProgress = null
    try {
      unusedJava = await withTimeout(App.AnalyzeUnusedJava(), JAVA_SCAN_TIMEOUT)
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      javaBusy = false
      javaProgress = null
    }
  }

  // Відкриття модалки запускає СВІЖИЙ аналіз: поки він іде — у модалці
  // видно анімацію процесу, потім — результати (ТЗ: «модульне вікно, де
  // показує процес аналізу Java з результатами»).
  function openJavaModal() {
    javaModalOpen = true
    analyzeUnusedJava()
  }

  // Аналіз версій Minecraft (вкладка «Пам'ять і кеш»): бекенд сканує
  // minecraft/versions, рахує розміри і визначає невикористовувані.
  // Викликається по кнопці (швидко — версії невеликі) і після очищення.
  async function analyzeStorageVersions() {
    if (versionsBusy) return
    versionsBusy = true
    try {
      storageVersions = await withTimeout(App.AnalyzeStorageVersions(), JAVA_SCAN_TIMEOUT)
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      versionsBusy = false
    }
  }

  function openVersionsModal() {
    versionsModalOpen = true
    analyzeStorageVersions()
  }

  // Видалення невикористовуваних версій Minecraft (підтверджені юзером).
  // Після видалення оновлюємо і аналіз, і загальні розміри (версії тягнуть
  // за собою бібліотеки й асети, тож дані могли змінитися).
  async function cleanupUnusedVersions() {
    if (!storageVersions || (storageVersions.unused ?? []).length === 0 || versionsBusy) return
    const n = storageVersions.unused.length
    const ok = await confirmDialog(
      t('settings.storage.cleanVersionsConfirm', { n }),
      t('settings.storage.cleanVersionsConfirmHint'),
      { confirmLabel: t('settings.storage.cleanVersions'), cancelLabel: t('cancel'), danger: true })
    if (!ok) return
    versionsBusy = true
    try {
      await App.CleanupUnusedVersions(storageVersions.unused)
      toast(t('settings.storage.cleanVersionsDone', { n }), 'success')
      storageVersions = null
      versionsModalOpen = false
      loadStorageInfo(true)
      analyzeUnusedJava()
      analyzeStorageVersions() // перезапускаємо аналіз після видалення
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
      App.AppendLauncherError('cleanupUnusedVersions', String(e), '')
    } finally {
      versionsBusy = false
    }
  }

  // Аналіз «сирітських» бібліотек (minecraft/libraries): файли, що більше
  // не згадуються жодною ВИКОРИСТОВУВАНОЮ версією (напр. після оновлення
  // лоадера з іншим набором залежностей). Лише аналіз — видалення окремо.
  async function analyzeOrphanLibs() {
    if (orphanLibsBusy) return
    orphanLibsBusy = true
    try {
      orphanLibs = await withTimeout(App.AnalyzeOrphanLibraries(), JAVA_SCAN_TIMEOUT)
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      orphanLibsBusy = false
    }
  }

  function openOrphanLibsModal() {
    orphanLibsModalOpen = true
    analyzeOrphanLibs()
  }

  async function cleanupOrphanLibs() {
    if (!orphanLibs || orphanLibs.orphanFiles === 0 || orphanLibsBusy) return
    const ok = await confirmDialog(
      t('settings.storage.cleanLibsConfirm', { n: orphanLibs.orphanFiles }),
      t('settings.storage.cleanLibsConfirmHint'),
      { confirmLabel: t('settings.storage.cleanLibs'), cancelLabel: t('cancel'), danger: true })
    if (!ok) return
    orphanLibsBusy = true
    try {
      orphanLibs = await withTimeout(App.CleanupOrphanLibraries(), JAVA_SCAN_TIMEOUT)
      toast(t('settings.storage.cleanLibsDone', { n: orphanLibs.removedFiles }), 'success')
      loadStorageInfo(true)
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
      App.AppendLauncherError('cleanupOrphanLibs', String(e), '')
    } finally {
      orphanLibsBusy = false
    }
  }

  // Аналіз «сирітських» об'єктів асетів (minecraft/assets/objects): те
  // саме для звуків/текстур/шрифтів гри — бекенд сам захищає від масового
  // видалення, якщо не знайдено жодної використовуваної версії.
  async function analyzeOrphanAssets() {
    if (orphanAssetsBusy) return
    orphanAssetsBusy = true
    try {
      orphanAssets = await withTimeout(App.AnalyzeOrphanAssets(), JAVA_SCAN_TIMEOUT)
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      orphanAssetsBusy = false
    }
  }

  function openOrphanAssetsModal() {
    orphanAssetsModalOpen = true
    analyzeOrphanAssets()
  }

  async function cleanupOrphanAssets() {
    if (!orphanAssets || orphanAssets.orphanObjects === 0 || orphanAssetsBusy) return
    const ok = await confirmDialog(
      t('settings.storage.cleanAssetsConfirm', { n: orphanAssets.orphanObjects }),
      t('settings.storage.cleanAssetsConfirmHint'),
      { confirmLabel: t('settings.storage.cleanAssets'), cancelLabel: t('cancel'), danger: true })
    if (!ok) return
    orphanAssetsBusy = true
    try {
      orphanAssets = await withTimeout(App.CleanupOrphanAssets(), JAVA_SCAN_TIMEOUT)
      toast(t('settings.storage.cleanAssetsDone', { n: orphanAssets.removed }), 'success')
      loadStorageInfo(true)
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
      App.AppendLauncherError('cleanupOrphanAssets', String(e), '')
    } finally {
      orphanAssetsBusy = false
    }
  }

  // Рекомендації: збірки, що займають багато місця, але майже не граються.
  async function loadStorageSuggestions() {
    try {
      storageSuggestions = (await withTimeout(App.GetStorageSuggestions(), JAVA_SCAN_TIMEOUT)) ?? []
    } catch (e) { storageSuggestions = [] }
  }

  // Авто-аналіз Java при ВХОДІ у вкладку «Пам'ять і кеш» БУДЬ-ЯКИМ шляхом:
  // клік по sub-nav, відновлення вкладки після перезапуску лаунчера тощо.
  // Раніше аналіз стартував лише з onclick sub-nav, тож після відновлення
  // вкладки (settingsTab з uiState) розділ показував лише кнопку «Перевірити».
  // Запускаємо важкі аналізи ПОСЛІДОВНО (не всі разом): спершу рахуємо розміри
  // тек, а вже ПІСЛЯ — рекомендації та Java-аналіз. Раніше всі чотири важкі
  // запити стартували одночасно і до того, як кеш розмірів заповнився, кожен
  // спавнив десятки воркерів обходу — разом вони «морозили» весь UI лаунчера
  // (інші розділи налаштувань не завантажувались, аналіз версій виглядав
  // безкінечним). Поки цикл іде, повторний вхід просто повертається.
  let storageTabSetupRunning = false
  async function storageTabSetup() {
    if (storageTabSetupRunning) return
    storageTabSetupRunning = true
    try {
      // Жорсткий бар'єр на сам набір запусків: навіть якщо щось у
      // loadStorageInfo/loadStorageSuggestions зненацька не віддасться,
      // прапорець storageTabSetupRunning скидається, і вкладку можна буде
      // відкривати знову, а не лишити її назавжди «занятою».
      await withTimeout(Promise.resolve().then(async () => {
        await loadStorageInfo()
        loadStorageSuggestions()
        if (!javaAnalyzedOnce) {
          javaAnalyzedOnce = true
          analyzeUnusedJava()
        }
      }), STORAGE_SCAN_TIMEOUT)
    } finally {
      storageTabSetupRunning = false
    }
  }

  $effect(() => {
    if (settingsTab !== 'storage') return
    storageTabSetup()
  })

  async function cleanupUnusedJava() {
    if (!unusedJava || (unusedJava.unused ?? []).length === 0 || javaBusy) return
    const majors = unusedJava.unused.join(', ')
    const ok = await confirmDialog(t('settings.storage.cleanJavaConfirm', { majors }), t('settings.storage.cleanJavaConfirmHint'), {
      confirmLabel: t('settings.storage.cleanJava'), cancelLabel: t('cancel'), danger: true
    })
    if (!ok) return
    javaBusy = true
    try {
      await App.CleanupUnusedJava(unusedJava.unused)
      toast(t('settings.storage.cleanJavaDone', { majors }), 'success')
      unusedJava = null
      javaModalOpen = false
      loadStorageInfo(true)
      analyzeUnusedJava() // перезапускаємо аналіз після видалення
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
      App.AppendLauncherError('cleanupUnusedJava', String(e), '')
    } finally {
      javaBusy = false
    }
  }

  // Шрифт консолі для конкретного типу повідомлення (ТЗ: «шрифт консолі
  // окремо, і режим різних шрифтів для info/warn/error»).
  // Режим 'perType' — кожен тип має свій шрифт (consoleInfoFont/Warn/Error);
  // режим 'all' — один спільний (consoleFontFamily). Використовується і в
  // живому переглядачі консолі в налаштуваннях.
  // Рядки вкладки «Теки та шляхи»: змінюється ЛИШЕ папка даних лаунчера
  // (baseDir) — усе інше (installations/java/cache/logs) завжди всередині
  // неї і показується як похідні read-only шляхи.
  // Рядки вкладки «Теки та шляхи»: змінюється папка даних лаунчера
  // (baseDir); installations/java/cache/logs показують ЕФЕКТИВНІ теки
  // (кастомні шляхи користувача мають пріоритет над похідними baseDir —
  // фікс «показує .shaurm, а збірки у D:\ShaurmLauncher»), з бейджем
  // «власна тека», якщо користувач задав її вручну.
  const folderRows = $derived(folderPaths ? [
    { kind: 'baseDir', name: t('settings.folders.baseDir'), desc: t('settings.folders.baseDirDesc'), path: folderPaths.baseDir, custom: '', reset: true, fixed: false },
    { kind: 'installations', name: t('settings.folders.installations'), desc: t('settings.folders.installationsDesc'), path: folderPaths.installations, custom: folderPaths.installationsCustom, reset: false, fixed: true },
    { kind: 'java', name: t('settings.folders.java'), desc: t('settings.folders.javaDesc'), path: folderPaths.java, custom: folderPaths.javaCustom, reset: false, fixed: true },
    { kind: 'cache', name: t('settings.folders.cache'), desc: t('settings.folders.cacheDesc'), path: folderPaths.cache, custom: folderPaths.cacheCustom, reset: false, fixed: true },
    { kind: 'logs', name: t('settings.folders.logs'), desc: t('settings.folders.logsDesc'), path: folderPaths.logs, custom: folderPaths.logsCustom, reset: false, fixed: true }
  ] : [])

  async function clearCache() {
    const ok = await confirmDialog(t('settings.storage.clearCache'), t('settings.storage.clearCacheConfirm'), {
      confirmLabel: t('settings.storage.clearCache'), cancelLabel: t('cancel'), danger: true
    })
    if (!ok) return
    try {
      await App.ClearCache()
      toast(t('settings.storage.cleared'), 'success')
      // force=true: після очищення перераховуємо розміри, навіть якщо
      // вкладку вже відкривали (інакше лишилися б старі цифри).
      loadStorageInfo(true)
    } catch (e: any) { toast(t('error.withMessage', { message: e }), 'error') }
  }

  async function checkUpdates() {
    if (updatingCheck) return
    updatingCheck = true
    updateResult = null
    try {
      updateResult = await App.CheckUpdate()
      // Час останньої перевірки записує і бекенд (settings.json), тут
      // оновлюємо копію в пам'яті, щоб «Остання перевірка: …» у вкладці
      // «Про лаунчер» оновилась одразу, без перезапуску налаштувань.
      if (updateResult) (settings as any).lastUpdateCheck = new Date().toISOString()
    } catch (e: any) {
      toast(t('settings.about.checkError'), 'error')
    } finally {
      updatingCheck = false
    }
  }

  // ── Вкладка «Про лаунчер» ──
  // ОС/архітектура тягнуться з бекенду ліниво (при першому відкритті
  // вкладки) — системні Win32-виклики на старті лаунчера не потрібні.
  let aboutSys = $state<SystemInfo>({ cpuCount: 0, totalRAMMB: 0, freeDiskMB: 0 })
  let aboutSysLoaded = false
  let updatingCheck = $state(false)
  async function loadAboutSys() {
    if (aboutSysLoaded) return
    try {
      aboutSys = await App.GetSystemInfo()
      aboutSysLoaded = true
    } catch { /* інфо про ОС некритичне — лишиться заглушка */ }
  }
  $effect(() => { if (settingsTab === 'about') loadAboutSys() })

  // ── Ручне оновлення лаунчера (кнопка «Оновити» в «Про лаунчер») ──
  // На відміну від автооновлення, це ЯВНИЙ запит користувача: лаунчер
  // завершується, апдейтер встановлює нову версію безумовно (навіть якщо
  // автооновлення вимкнене) і повертається в основний UI.
  let updateStarting = $state(false)
  async function updateNow() {
    if (updateStarting) return
    updateStarting = true
    try {
      await App.UpdateNow()
      // Після успішного запиту лаунчер ось-ось закриється (перехід в
      // апдейтер) — нічого більше не робимо.
    } catch (e: any) {
      updateStarting = false
      toast(t('settings.about.updateError'), 'error')
    }
  }

  // Форматування ігрового часу: години завжди (gameTimeInHours) або
  // «1 год 30 хв» у звичайному режимі.
  function fmtPlaytime(seconds: number): string {
    if (!seconds || seconds <= 0) return '—'
    const h = Math.floor(seconds / 3600)
    const m = Math.floor((seconds % 3600) / 60)
    if (settings.gameTimeInHours) return `${(seconds / 3600).toFixed(1)} год`
    if (h === 0) return `${m} хв`
    return `${h} год ${m} хв`
  }

  // Живий таймер поточної сесії (кнопка «Стоп»): на відміну від fmtPlaytime
  // (загальний накопичений час — округлений до хвилин), тут потрібні
  // секунди — інакше перші 59 секунд гри показували б «0 хв» замість
  // реального відліку. Формат: мм:сс, а після години — гг:мм:сс.
  function fmtSessionTime(seconds: number): string {
    if (!seconds || seconds <= 0) return '0:00'
    const h = Math.floor(seconds / 3600)
    const m = Math.floor((seconds % 3600) / 60)
    const s = Math.floor(seconds % 60)
    const mm = h > 0 ? String(m).padStart(2, '0') : String(m)
    const ss = String(s).padStart(2, '0')
    return h > 0 ? `${h}:${mm}:${ss}` : `${mm}:${ss}`
  }

  // Консоль тепер живе у вкладці «Консоль» на сторінці збірки
  // (PackInstancePage), а не як віджет на «Моїх збірках». Тож логіки
  // consoleVisible тут більше немає — консоль завжди доступна через вкладку.

  // ── Wizard ──
  // Кроки та поля майстра персистяться на бекенді (setup-progress.json)
  // при КОЖНІЙ зміні — закриття лаунчера на кроці 3 відновить саме крок 3
  // з уже введеними даними, а не почне спочатку. Сам майстер живе у
  // Wizard.svelte, тут лише показ/завершення.
  async function startWizard() {
    const firstRun = await App.IsFirstRun()
    if (!firstRun) return
    showWizard = true
  }

  async function wizardFinished() {
    showWizard = false
    toast(t('settings.saved'), 'success')
    await loadData()
    // Після майстра — повноекранна заставка (LaunchIntro). Показуємо поверх
    // основного UI; її завершення керується всередині компонента (onDone).
    showIntro = true
  }

  // ── Login modal ──
  // msLoginTimer — автоскасування очікування Microsoft-входу. Якщо
  // користувач закрив вкладку браузера (або вкладка впала), локальний
  // callback-сервер так і не отримає код, і AwaitMSLogin висів би до
  // таймауту (5 хв). Замість цього фронтенд сам скидає стан через 90 с —
  // модалка повертається до кнопки «Увійти через Microsoft», і вхід можна
  // почати заново.
  let msLoginTimer: ReturnType<typeof setTimeout> | null = null

  async function loginMsStart() {
    // Повторний клік під час очікування = «Спробувати ще раз»: спершу
    // скасовуємо поточне очікування (закриває старий listener на порту),
    // потім починаємо новий цикл з нуля.
    if (loginMsStep === 'waiting') {
      App.CancelMSLogin()
      if (msLoginTimer) { clearTimeout(msLoginTimer); msLoginTimer = null }
      loginMsStep = 'idle'
    }
    loginMsStep = 'waiting'
    try {
      const url = await App.StartMSLogin()
      // URL завжди має бути https:// — якщо StartMSLogin не зміг сформувати
      // його, OpenExternal з порожнім рядком відкрив би Провідник на C:.
      if (!url) throw new Error(t('login.noUrl'))
      App.OpenExternal(url)
      // Автоскасування: 90 секунд — достатньо на вхід у браузері, після
      // чого модалка повертається до початкового стану (можна натиснути
      // «Спробувати ще раз»).
      msLoginTimer = setTimeout(() => {
        if (loginMsStep === 'waiting') {
          App.CancelMSLogin()
          loginMsStep = 'idle'
          toast(t('login.timedOut'), 'info')
        }
        msLoginTimer = null
      }, 90_000)
      const r = await App.AwaitMSLogin()
      if (msLoginTimer) { clearTimeout(msLoginTimer); msLoginTimer = null }
      loginMsStep = 'idle'
      await handleSignIn(r)
    } catch (e: any) {
      if (msLoginTimer) { clearTimeout(msLoginTimer); msLoginTimer = null }
      loginMsStep = 'idle'
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  // ── Єдиний вхід: Microsoft / Google → (якщо акаунта Шаурми ще нема) нік → готово ──
  let signInNick = $state<null | { provider: string; noLicense: string }>(null)
  let skinOfferNick = $state('')

  async function refreshAccountsAfterSignIn() {
    accounts = (await App.GetAccounts()) ?? []
    settings = await App.GetSettings()
    activeAccountId = settings.activeAccountId ?? ''
    refreshHeads()
  }

  // Ліміт пристроїв: особу підтверджено, але в акаунті вже максимум — обираємо, кого вилучити.
  let deviceLimit = $state<null | { devices: DeviceChoice[]; max: number }>(null)
  async function pickDeviceToReplace(id: string) {
    const r = await App.ReplaceShaurmaDevice(id) // кидає помилку → модалка покаже її й дасть обрати знову
    deviceLimit = null
    await handleSignIn(r)
  }
  function cancelDeviceLimit() {
    try { App.CancelSignIn() } catch { /* ignore */ }
    deviceLimit = null
  }

  async function handleSignIn(r: SignInResult) {
    if (r.status === 'device_limit') {
      deviceLimit = { devices: r.devices ?? [], max: r.maxDevices ?? 10 }
      return
    }
    if (r.status === 'need_nick') {
      // Особу підтверджено, акаунта Шаурми ще нема — питаємо нік (окреме вікно).
      signInNick = { provider: r.provider, noLicense: r.noLicense ?? '' }
      return
    }
    signInNick = null
    await refreshAccountsAfterSignIn()
    modalLogin = false; loginGate = false
    // Новий НЕліцензійний акаунт → одразу пропонуємо поставити скін (гардероб відкриється з ним).
    if (!r.licensed && !r.existing && r.nickname) skinOfferNick = r.nickname
    else maybeShowPostLoginPrompts()
  }

  async function loginGoogleStart() {
    googleShaurmaStep = 'waiting'
    try {
      const r = await App.SignInGoogle()
      if (googleShaurmaStep !== 'waiting') return // скасовано користувачем
      googleShaurmaStep = 'idle'
      await handleSignIn(r)
    } catch (e: any) {
      if (googleShaurmaStep === 'waiting') toast(t('error.withMessage', { message: e }), 'error')
      googleShaurmaStep = 'idle'
    }
  }
  function loginGoogleCancel() {
    try { App.CancelSignIn() } catch { /* вже завершився */ }
    googleShaurmaStep = 'idle'
  }
  // Кидає помилку при невдачі — NickChoiceModal покаже її під полем і дасть обрати інший нік.
  async function confirmSignInNick(nick: string) {
    const r = await App.CompleteSignIn(nick)
    await handleSignIn(r)
  }
  function cancelSignInNick() {
    try { App.CancelSignIn() } catch { /* ignore */ }
    signInNick = null
  }
  function skinOfferOpenWardrobe() {
    skinOfferNick = ''
    maybeShowPostLoginPrompts()
    navigate('wardrobe')
  }
  function skinOfferLater() { skinOfferNick = ''; maybeShowPostLoginPrompts() }

  // Microsoft-акаунт — ТЕЖ особистість Шаурми: лаунчер уже обміняв його
  // id_token на сесію Шаурми (без Xbox/XUID/ліцензії). Тому після входу
  // перевіряємо СТАН ЦІЄЇ особистості:
  //   - friends розлогінені → нік уже є, нічого не питати;
  //   - diag = needs_nickname → сесія є, але ніка ще нема: питаємо його в
  //     тій самій модалці, що й для Google, але з підказкою Microsoft;
  //   - інакше (сервер Шаурми недоступний) — запасний шлях: пропонуємо
  //     Google-вхід, як і раніше.
  async function promptSocialAfterMsLogin() {
    try {
      const st = await App.GetFriendsState()
      if (st?.loggedIn) return
      if (st?.diag === 'needs_nickname') {
        skinLoginEmail = ''
        skinNickIdentity = 'microsoft'
        modalLogin = true; loginGate = false
        skinLoginStep = 'need-nick'
        return
      }
    } catch { /* стан не отримали — не блокуємо, ідемо запасним шляхом */ }
    promptGoogleLink()
  }

  // Тільки ПІСЛЯ Microsoft-входу: якщо акаунта Шаурми ще немає (напр. вхід
  // стався до Shaurma-редакції), ведемо гравця на сторінку «Акаунт».
  async function promptGoogleLink() {
    if (!skinsEnabled) return
    await refreshSkinAuth()
    if (skinAuth.ok) return
    window.dispatchEvent(new CustomEvent('open-settings-skin-account'))
  }

  function loginMsCancel() {
    if (msLoginTimer) { clearTimeout(msLoginTimer); msLoginTimer = null }
    App.CancelMSLogin()
    loginMsStep = 'idle'
  }

  // Закриття модалки входу (клік поза нею / кнопка «Скасувати» внизу):
  // якщо в цей момент іде очікування Microsoft-авторизації (loginMsStep ===
  // 'waiting'), локальний callback-сервер на порту 29111-29115 ЛИШАЄТЬСЯ
  // запущеним, поки AwaitMSLogin не завершиться таймаутом (5 хв). Кожне
  // таке закриття займало би порт назавжди — після 5 спроб усі порти
  // зайняті самим лаунчером, і вхід у Microsoft ламається («жоден порт не
  // вільний»). Тому закриття обов'язково скидає чекання через CancelMSLogin
  // (він закриває listener), і лише потім ховає модалку.
  function closeLoginModal() {
    if (loginMsStep === 'waiting') loginMsCancel()
    if (skinLoginStep !== 'idle') loginSkinGoogleCancel()
    modalLogin = false
  }

  // Пост-логін черга модалок: єдиний крок — міграція збірок з інших лаунчерів
  // (якщо знайдено лаунчер і ще не питали). Жодних запитів на згоду
  // надсилати дані немає: лаунчер нічого не збирає й не передає.
  async function maybeShowPostLoginPrompts() {
    maybeShowMigration()
  }

  // Міграція збірок: якщо на ПК знайдено інший лаунчер і вікно ще не
  // показувалось (галочка MigrateDismiss у БД) — відкриваємо модалку
  // міграції. Один раз за запуск: після закриття migrationPromptDone
  // лишає вимкненим до наступного старту.
  async function maybeShowMigration() {
    if (migrationPromptDone || migrationOpen) return
    try {
      const st = await App.MigrateStatus()
      // Вікно лише коли реально є що переносити (packCount — не перенесені
      // збірки). Дані вже просканованого стану передаємо в модалку, щоб
      // вона не сканувала диск удруге (довге «Сканування…»).
      if (st.shouldShow && st.packCount > 0) {
        migrationPromptDone = true
        migrationInitial = st.launchers ?? []
        migrationOpen = true
      }
    } catch (e) { /* сканування впало — міграція доступна з налаштувань */ }
  }

  // Офлайн-вхід: нік приходить з LoginModal (там він уже валідований і
  // санітайзернутий тими самими правилами lib/validation.ts, що й візза),
  // тут лише страховка на випадок зміни правил у майбутньому.
  async function loginPirate(username: string): Promise<boolean> {
    const name = (username ?? '').trim()
    if (!name) return false
    const err = pirateNicknameError(name)
    if (err) { toast(err, 'error'); return false }
    try {
      await App.LoginPirate(name)
      accounts = (await App.GetAccounts()) ?? []
      settings = await App.GetSettings()
      activeAccountId = settings.activeAccountId ?? ''
      refreshHeads()
      modalLogin = false; loginGate = false
      maybeShowPostLoginPrompts()
      return true
    } catch (e: any) { toast(t('error.withMessage', { message: e }), 'error'); return false }
  }

  // Вхід у СКІН-систему з модалки входу (для офлайн-акаунтів). Порядок —
  // «Google спочатку»: кнопка без жодного поля ніка → браузер (OAuth) →
  // сесія. Якщо в цього Google-акаунта на сервері ВЖЕ є нік (він приходить
  // у JWT → st.nickname), довходимо одразу і нічого не питаємо. Лише для
  // нового акаунта (ніка нема) переходимо в крок 'need-nick', де
  // LoginModal показує поле «придумайте нік» (confirmNewSkinNickname).
  // Акаунт Шаурми через Google: піратка з цим ніком + комірка. Модалку не закриваємо до кінця
  // (loginPirate закрив би її одразу). Якщо Google не вдався — піратка лишається, а акаунт
  // Шаурми можна створити пізніше в «Налаштування → Акаунт».
  let googleShaurmaStep = $state<'idle' | 'waiting'>('idle')
  async function loginGoogleShaurma(username: string) {
    const name = (username ?? '').trim()
    const err = pirateNicknameError(name)
    if (!name || err) { if (err) toast(err, 'error'); return }
    googleShaurmaStep = 'waiting'
    try {
      await App.LoginPirate(name)
      accounts = (await App.GetAccounts()) ?? []
      settings = await App.GetSettings()
      activeAccountId = settings.activeAccountId ?? ''
      refreshHeads()
    } catch (e: any) {
      googleShaurmaStep = 'idle'
      toast(t('error.withMessage', { message: e }), 'error')
      return
    }
    try {
      await App.CreateShaurmaAccount(name, 'google')
      if (googleShaurmaStep !== 'waiting') return // скасовано користувачем
      toast('Акаунт Шаурми підключено через Google', 'success')
    } catch (e: any) {
      if (googleShaurmaStep === 'waiting') toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      const wasWaiting = googleShaurmaStep === 'waiting'
      googleShaurmaStep = 'idle'
      if (wasWaiting) { modalLogin = false; loginGate = false; maybeShowPostLoginPrompts() }
    }
  }
  function cancelGoogleShaurma() {
    try { App.CancelShaurmaLogin() } catch { /* вже завершився */ }
    googleShaurmaStep = 'idle'
    // піратка вже створена й активна, тож «ворота» входу більше не потрібні
    modalLogin = false; loginGate = false
  }

  async function loginSkinGoogleStart() {
    // Повторний клік під час очікування = «Спробувати ще раз»: спершу
    // скасовуємо старий callback-сервер (звільняє порт).
    if (skinLoginStep === 'waiting') App.CancelGoogleSkinLogin()
    skinLoginStep = 'waiting'
    try {
      const url = await App.StartGoogleSkinLogin()
      if (url) App.OpenExternal(url).catch(() => {})
      const st = await App.AwaitGoogleSkinLogin()
      // Користувач міг натиснути «Скасувати» / закрити модалку, поки ми чекали.
      if (skinLoginStep !== 'waiting') return
      await refreshSkinAuth()
      const nick = (st?.nickname ?? '').trim()
      if (nick) {
        // Нік уже є на сервері — довходимо одразу.
        skinLoginStep = 'idle'
        if (await loginPirate(nick)) {
          toast(st?.email ? `Скін-акаунт: ${st.email}` : 'Скін-акаунт підключено', 'success')
        }
      } else {
        // Новий акаунт без ніка — питаємо його ЛИШЕ тепер (окреме вікно).
        skinLoginEmail = st?.email ?? ''
        skinNickIdentity = 'google'
        skinLoginStep = 'need-nick'
      }
    } catch (e: any) {
      // Якщо очікування скасував сам користувач — це не помилка.
      if (skinLoginStep === 'waiting') toast(t('error.withMessage', { message: e }), 'error')
      skinLoginStep = 'idle'
    }
  }

  // Скасування Google-флоу: під час очікування закриваємо callback-сервер
  // (порти 29121-29125 інакше лишились би зайняті до таймауту), на кроці
  // need-nick — просто виходимо (сесія JWT лишається, нік можна задати
  // пізніше в панелі скін-акаунта).
  function loginSkinGoogleCancel() {
    if (skinLoginStep === 'waiting') App.CancelGoogleSkinLogin()
    skinLoginStep = 'idle'
  }

  // Скасування КРОКУ НІКА: для Google просто ховаємо крок (модалка входу
  // під нею лишиться), для Microsoft закриваємо вікно цілком — активний
  // Microsoft-акаунт уже в списку, повертати людину в «вхід» без потреби.
  // Нік можна поставити й пізніше зі сторінки «Друзі» (сторінка показує
  // той самий стан needs_nickname).
  function skinNickCancel() {
    if (skinNickIdentity === 'microsoft') {
      skinLoginStep = 'idle'
      modalLogin = false
      return
    }
    loginSkinGoogleCancel()
  }

  // Крок 2 (лише коли skinLoginStep === 'need-nick'): користувач ввів
  // новий нік. Остання перевірка зайнятості → SetSkinNickname (закріплює
  // нік за акаунтом) → офлайн-вхід із цим ніком.
  async function confirmNewSkinNickname(username: string) {
    const name = (username ?? '').trim()
    const nickErr = pirateNicknameError(name)
    if (nickErr) { toast(nickErr, 'error'); return }
    if (skinNickBusy) return
    skinNickBusy = true
    try {
      // SetSocialNickname — ЄДИНИЙ авторитетний крок: він сам диспетчеризує
      // запит на Microsoft- чи Google-особистість. Для власного ніка він
      // проходить (ідемпотентно), для чужого сервер повертає 409 «уже
      // зайнятий». Окреме «чи вільний нік» тут не блокує: живу перевірку
      // показує саме вікно, а хибне «зайнято» не має ламати вхід.
      await App.SetSocialNickname(name)
      if (skinNickIdentity === 'microsoft') {
        // Microsoft-акаунт УЖЕ в списку лаунчера: окремий офлайн-акаунт під
        // цим ніком НЕ створюємо, а одразу вмикаємо друзів/чат/P2P під
        // новою особистістю і закриваємо крок ніка.
        skinLoginStep = 'idle'
        modalLogin = false
        toast('Нік збережено', 'success')
        App.RefreshFriends().catch(() => {})
        return
      }
      await refreshSkinAuth()
      // loginPirate сам показує помилку й повертає false — тоді вікно
      // лишається відкритим (нік уже на сервері, повтор безпечний).
      if (!(await loginPirate(name))) return
      skinLoginStep = 'idle'
      toast('Нік збережено', 'success')
    } catch (e: any) {
      // Вікно лишається відкритим — можна виправити нік і спробувати знову.
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      skinNickBusy = false
    }
  }

  // Імпорт спільнотної мови: діалог вибору JSON → читання через бекенд →
  // парсинг → реєстрація в i18n → одразу перемикаємо на неї.
  async function importLanguageFile() {
    try {
      const path = await App.BrowseLanguageFile()
      if (!path) return
      const content = await App.LoadLanguageFile(path)
      if (!content) { toast(t('settings.importLangError'), 'error'); return }
      let parsed: unknown
      try { parsed = JSON.parse(content) } catch { toast(t('settings.importLangBadJson'), 'error'); return }
      // Назва мови — з імені файлу (напр. polish.json → Polish).
      const name = path.split(/[\\/]/).pop()?.replace(/\.json$/i, '') ?? 'Custom'
      const code = registerCustomLanguage(name, parsed)
      if (!code) { toast(t('settings.importLangInvalid'), 'error'); return }
      setLanguage(code)
      updateSetting('language', code)
      toast(t('settings.importLangDone', { name }), 'success')
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  function winMin()  { App.MinimizeLauncher() }
  function winMax()  {
    if (isMaximized) { Window.UnMaximise(); isMaximized = false }
    else { Window.Maximise(); isMaximized = true }
  }
  // Закриття через кнопку X топбара: йде через бекенд-метод QuitLauncher,
  // який виставляє прапорець consoleAppQuitting — інакше відкрите вікно
  // консолі (WindowClosing-хук) заблокувало б вихід з програми.
  // flushUIState() — форсований (без дебаунсу 250мс) запис «останнього
  // вікна» перед виходом: відкладений таймер persist() міг не встигнути,
  // якщо остання зміна (скрол/вкладка) сталась прямо перед закриттям.
  function winClose() { flushUIState(); App.QuitLauncher() }

  async function removeAccount(id: string) {
    const ok = await confirmDialog(t('acc.deleteConfirm'), t('acc.delete'), {
      confirmLabel: t('acc.delete'), cancelLabel: t('cancel'), danger: true
    })
    if (!ok) return
    await App.RemoveAccount(id)
    accounts = await App.GetAccounts()
    settings = await App.GetSettings()
    activeAccountId = settings.activeAccountId ?? ''
    refreshHeads()
    toast(t('settings.saved'), 'success')
    // Видалили останній акаунт — повертаємо "ворота входу".
    if (accounts.length === 0) {
      loginGate = true; loginMsStep = 'idle'; modalLogin = true
    }
  }

  // ── Перейменування Шаурма-акаунта (неліцензійного) ───────────────────
  // Новий нік технічно означає новий ID/UUID (App.RenameOfflineAccount на
  // бекенді сам переносить прив'язки збірок і пресетів гардероба) — тут
  // лишається тільки підхопити результат: перезавантажити список
  // акаунтів/налаштування (як і після removeAccount) і показати окреме
  // попередження, якщо скін-нік не вдалось синхронізувати (rate-limit).
  let renameAccountOpen = $state(false)
  let renameAccountTarget = $state<{ id: string; nick: string } | null>(null)

  function openRenameAccount(acc: Account) {
    renameAccountTarget = { id: acc.id, nick: acc.username }
    renameAccountOpen = true
  }

  async function onAccountRenamed(newAccountId: string, skinWarning: string) {
    renameAccountOpen = false
    renameAccountTarget = null
    accounts = await App.GetAccounts()
    settings = await App.GetSettings()
    activeAccountId = settings.activeAccountId ?? ''
    refreshHeads()
    if (skinWarning) {
      toast(skinWarning, 'error')
    } else {
      toast(t('settings.saved'), 'success')
    }
  }

  // dragAcc — індекс акаунта, що зараз перетягується (для drag&drop у
  // списку акаунтів: підсвітка джерела/цілі та drop-обробка).
  let dragAcc: number | null = $state(null)
  // dragOverRow — індекс рядка-ЦІЛІ, над яким зараз курсор під час
  // перетягування (pointer-drag, тому що HTML5 draggable не працював:
  // preventDefault на mousedown блокує нативний drag).
  let dragOverRow: number | null = $state(null)

  // Переміщення акаунта на ДОВІЛЬНЕ місце (drag&drop, ⠿-хендл ліворуч у
  // списку акаунтів — єдиний спосіб змінити порядок; раніше поруч були ще
  // й дублюючі кнопки ▲/▼ (moveAccount), прибрані як зайва плутанина).
  // Порядок зберігається через App.ReorderAccounts — активний не міняється.
  async function moveAccountTo(from: number, to: number) {
    if (from === to || from < 0 || to < 0 || from >= accounts.length || to >= accounts.length) return
    const next = accounts.slice()
    const [moved] = next.splice(from, 1)
    // Drop на рядок = поставити ПЕРЕД ним. Після видалення джерела індекси
    // зсуваються: якщо тягнемо вниз (from < to), ціль стає to-1.
    const insertAt = from < to ? to - 1 : to
    next.splice(insertAt, 0, moved)
    try {
      await App.ReorderAccounts(next.map(a => a.id))
      accounts = await App.GetAccounts()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  // Перезайти в акаунт: активуємо його і запускаємо MS-вхід. Бекенд при
  // завершенні оновлює запис з тим самим ID (не дублює) — токени свіжі.
  function reloginAccount(acc: Account) {
    if (acc.id !== activeAccountId) switchAccount(acc.id)
    loginMsStep = 'idle'
    modalLogin = true
  }

  onMount(async () => {
    // Глобальний тихий журнал помилок: всі необроблені Promise-відхилення
    // та синхронні помилки пишуться у бекендовий кільцевий буфер
    // (launcher_errors.go). Юзер нічого не бачить — може лише Export.
    window.addEventListener('unhandledrejection', (ev) => {
      const msg = ev.reason instanceof Error
        ? ev.reason.message
        : String(ev.reason ?? 'unhandledrejection')
      const detail = ev.reason instanceof Error ? (ev.reason.stack ?? '') : ''
      App.AppendLauncherError('unhandledrejection', msg, detail).catch(() => {})
    })
    window.addEventListener('error', (ev) => {
      const msg = ev.message || 'unknown error'
      const detail = [ev.filename, ev.lineno, ev.colno].filter(Boolean).join(':')
      App.AppendLauncherError('window.onerror', msg, detail).catch(() => {})
    })

    // Вбудовані шрифти (assets/ttf) — реєструємо @font-face у цьому документі.
    registerBundledFonts()
    // Стиснений стан sidebar зберігається у localStorage (поточний сеанс),
    // а ширина/масштаб/«лише закріплені» — у БД через settings: вони
    // застосовуються з settings одразу після loadData().
    try { sidebarCollapsed = localStorage.getItem('shaurma.sidebarCollapsed') === '1' } catch { /* ок */ }
    await loadPackLayout()
    await loadPackFolders()
    await startWizard()
    await loadData()
    // Лаунчер запущено з ярлика на робочому столі (shell передав
    // "--launch <packID> [--account <id>] [--server <host>]" у backend ще до
    // підписки фронтенду на події — значення лежить pending на бекенді).
    // Забираємо його і запускаємо збірку з персональними опціями ярлика
    // (функція ігнорує порожній packId — звичайний запуск без ярлика).
    // Тимчасові сервери у списках збірок (P2P-кімнати, куди гравець зайшов
    // як співхост або за запрошенням): прибираємо ті, доступу до яких більше
    // немає. Робимо це один раз при старті — саме тому такий сервер у списку
    // позначений як тимчасовий і зникає сам, без ручного чищення.
    App.SyncTempPackServers().catch(() => {})
    const pendingSc = await App.TakePendingShortcutLaunch().catch(() => null)
    if (pendingSc?.packId) {
      launchFromShortcut(pendingSc.packId, {
        accountId: pendingSc.accountId, autoJoinServer: pendingSc.autoJoinServer,
      })
    }
    // Лаунчер запущено з файлу збірки (.shaurmpack/.shaurmbundle через
    // Windows-асоціацію) — подія packfile:open могла прийти ДО підписки
    // фронтенду, шлях лежить pending на бекенді. Забираємо і запускаємо
    // імпорт у фоні (без модалки — як завантаження модпаку з браузера).
    const pendingPackFile = await App.TakePendingPackImportFile().catch(() => '')
    if (pendingPackFile) void openPackFile(pendingPackFile)
    // Холодний старт із shaurma://friend/… (подія friend:open могла прийти раніше).
    const pendingFriend = await App.TakePendingFriendLink().catch(() => '')
    if (pendingFriend) openFriendLinkFromOS(pendingFriend)
    // Відстежуємо позицію вікна через window.screenX/Y (надійний спосіб
    // отримати координати у WebView2) і зберігаємо на бекенд з debounce.
    //
    // ТІЛЬКИ WINDOWS. У WebKitGTK (Linux) window.screenX/screenY не дають
    // реальних екранних координат вікна і типово повертають 0 — на бекенд
    // летіла позиція 0,0 з hasPos=true, і після перезапуску лаунчер
    // «відновлював» вікно притиснутим до лівого верхнього кута екрана.
    // На Linux позицію і так шле сам shell (WindowDidMove →
    // "window:state", див. shell/window_exec.go), причому з реальними
    // координатами GTK і з правильним hasPos — дублювати це з фронтенду
    // не потрібно.
    //
    // Слухач на 'mousemove' прибрано і на Windows теж: він смикав
    // clearTimeout/setTimeout на КОЖЕН рух миші по вікну (сотні разів на
    // секунду) заради події, яка реально стається лише при перетягуванні
    // вікна. Достатньо 'resize' + завершення перетягування ('mouseup').
    if ((document.documentElement.dataset.platform ?? '') !== 'linux') {
      let winPosTimer = 0
      const trackWindowPos = () => {
        clearTimeout(winPosTimer)
        winPosTimer = window.setTimeout(() => {
          App.SaveWindowPosition(window.screenX, window.screenY).catch(() => {})
        }, 600)
      }
      window.addEventListener('resize', trackWindowPos, { passive: true })
      window.addEventListener('mouseup', trackWindowPos, { passive: true })
    }
    // Правило «вікно неактивне → анімації не грають» — у спільному модулі
    // windowActive.svelte.ts: нативна подія "window:active" від shell
    // (WM_ACTIVATE, shell/window_exec.go) + fallback focus/blur/visibilitychange
    // для dev-режиму. Ставить data-window-active на <html> (CSS гасить
    // keyframes, AnimatedIcon заморожує GIF/WebP-іконки) і глобально
    // заморожує решту анімованих <img>/фонів.
    initWindowActive('window:active')
    // Поллінг списку збірок (GetBuilds → App.packs.GetPackIndex, кеш на
    // бекенді 15с — див. shaurma_packs_full.go packsIndexTTL). Раніше
    // каталог Шаурма-збірок читався ЛИШЕ один раз при старті (loadData) —
    // якщо адміністратор додавав гравця у приватну групу на сайті, нова
    // збірка з'являлась у лаунчері тільки після повного перезапуску.
    // Тепер список тихо оновлюється сам, без дій користувача: 25с — довше
    // за клієнтський TTL (15с), щоб кожен тик реально йшов у мережу, а не
    // впирався у свіжий кеш; досить рідко, щоб не бути помітним
    // навантаженням. Пауза, коли вікно згорнуте/на фоні (document.hidden) —
    // не варто бити мережу, поки лаунчер невидимий; при поверненні у фокус
    // наступний тик все одно підхопить актуальний стан.
    //
    // ВАЖЛИВО: таймер запускається тут (усередині async onMount), а
    // прибирається ОКРЕМИМ синхронним onDestroy на верхньому рівні
    // <script> (packsPollTimer — component-рівня змінна, оголошена вище,
    // біля refreshBuilds), а НЕ через onDestroy(...) прямо в цьому місці:
    // після await вище (loadPackLayout/loadPackFolders/startWizard/
    // loadData) контекст компонента Svelte вже втрачено, і onDestroy(),
    // викликаний тут, впав би з lifecycle-помилкою або тихо не
    // зареєструвався.
    packsPollTimer = window.setInterval(() => {
      if (document.hidden) return
      refreshBuilds()
    }, 25000)
    const contentEl = document.querySelector('.content') as HTMLElement | null
    contentEl?.addEventListener('scroll', onMainScroll, { passive: true })
    window.addEventListener('pagehide', persistMainScrollNow)
    // Прогрес синхронізації Шаурма-збірок (RunnerProgress від sync.Runner).
    on('sync:progress', (p: SyncProgress) => { applySyncProgress(p) })
    // Міграція модів після створення збірки (StartModMigration): живий
    // прогрес на картці та сторінці збірки. Подія несе packId, лічильники
    // та назву/іконку поточного мода — банер показує «що саме качається».
    on('mods:migrate-progress', (p: any) => {
      const id = p?.packId
      if (!id) return
      migrateByPack = {
        ...migrateByPack,
        [id]: {
          done: p.current ?? 0, total: p.total ?? 0,
          currentFile: p.fileName ?? '', name: p.name ?? '',
          iconUrl: p.iconUrl ?? '', status: p.status ?? '',
        },
      }
    })
    on('mods:migrate-done', (p: any) => {
      const id = p?.packId
      if (!id) return
      const migrated = (p.migrated ?? []) as string[]
      const failed = (p.failed ?? []) as string[]
      const bv = builds.find(b => b.build.id === id)
      if (migrated.length > 0) {
        toast(t('mods.migrateDone', { n: migrated.length, name: bv?.build.name ?? id }), failed.length > 0 ? 'error' : 'success')
      } else if (failed.length > 0) {
        toast(t('mods.migrateFailed', { n: failed.length }), 'error')
      }
      // Знімаємо прогрес-банер через 2.5с (щоб користувач встиг побачити
      // 100%), ПОРІВНЮЮЧИ знімок стану: видаляємо лише якщо запис не
      // оновлювався після done (тобто це та сама міграція). Якщо за ці
      // 2.5с стартувала НОВА міграція у ту саму збірку — її done/total
      // вже інші, і старий таймаут її не зітре. На відміну від старої
      // логіки зі збігом total, тут міграція, що завершилась з помилками
      // на середині (total у done менший за total прогресу), ТЕЖ завжди
      // знімається — картка не «зависає» назавжди.
      const snap = migrateByPack[id]
      setTimeout(() => {
        const next = { ...migrateByPack }
        const cur = next[id]
        if (cur && snap && cur.done === snap.done && cur.total === snap.total && cur.currentFile === snap.currentFile) {
          delete next[id]
        }
        migrateByPack = next
      }, 2500)
      refreshBuilds()
    })
    // Скін на акаунті змінився (гардероб) — перерендерюємо аватарки одразу:
    // бекенд уже скинув кеш голови (avatar.Service.Invalidate), тож
    // GetAccountHead поверне голову вже з нового скіна.
    on('avatar:changed', () => refreshHeads())
    // Бекенд дізнався, що refresh-токен Google-сесії відхилено: оновлюємо
    // статус і самі показуємо вікно перезаходу (див. skinReloginStart).
    on('skin-auth:expired', async () => {
      await refreshSkinAuth()
      if (skinAuth.expired) openSkinRelogin()
    })
    // Заблоковані автором моди при імпорті CurseForge zip: показуємо діалог
    // (як BlockedModsDialog у Prism) і чекаємо рішення користувача.
    on('import:blocked', (payload: any) => {
      if (payload && Array.isArray(payload.mods)) {
        blockedModsDialog = {
          packId: String(payload.packId ?? ''),
          mods: payload.mods,
          allowModrinth: payload.allowModrinth !== false,
        }
      }
    })
    // Офіційний сервер збірки переїхав на новий IP (опція officialServerSync
    // у Pack Manager): лаунчер сам оновив запис у servers.dat — повідомляємо.
    on('server:ip-updated', (p: any) => {
      if (!p || !p.newIp) return
      const msg = t('pack.servers.official.ipUpdated', { name: String(p.packName ?? ''), ip: String(p.newIp) })
      toast(msg, 'info', 7000)
      if (!windowActive) App.NotifyOS(t('notify.title'), msg)
    })
    on('download:complete', () => {
      installError = ''
      loadData()
      // Фонове завантаження/оновлення завершилось, а користувач не дивиться
      // на лаунчер (вікно неактивне) — системний тост Windows замість
      // пропущеного моменту.
      if (!windowActive) {
        App.NotifyOS(t('notify.title'), t('notify.downloadDone'))
      }
    })
    // Помилка встановлення: показуємо у віджеті-тості (і в OS-сповіщенні),
    // а НЕ згортуваною панеллю в низу «Моїх збірок». installer:log несе і
    // звичайні рядки прогресу, і рядки «Помилка: …» — показуємо лише останні
    // (по-користувацькому).
    // Прогрес фонового передзавантаження компонентів щойно створеної
    // збірки (AutoDownloadAfterCreate — Java/лоадер/клієнт Minecraft).
    // Подія installer:progress несе {packId, message, percent}; картка/
    // сторінка збірки показує «Завантаження…» з прогрес-баром, як для
    // звичайних збірок. Ті самі installer:progress від запуску гри (без
    // packId) ігноруються — їх показує launch:progress.
    on('installer:progress', (p: any) => {
      if (!p || !p.packId || typeof p.percent !== 'number') return
      const prog: BuildProgress = {
        percent: p.percent, currentFile: p.message || '',
        bytesDone: 0, bytesTotal: 0, filesDone: 0, filesTotal: 0, speedBps: 0,
        mode: 'downloading', stage: 'loader',
      }
      downloadByPack = { ...downloadByPack, [p.packId]: prog }
      patchBuild(p.packId, b => ({
        ...b,
        status: b.status === 'running' ? b.status : 'downloading',
        progress: prog,
      }))
    })
    // Завершення/помилка передзавантаження: прибираємо прогрес з картки і
    // оновлюємо стан збірки (лишається «встановленою» — просто готова).
    on('installer:done', (packId: string) => {
      if (!packId) return
      const next = { ...downloadByPack }
      delete next[packId]
      downloadByPack = next
      refreshBuilds()
    })
    on('download:error', (err: any) => {
      // Бекенд шле як рядок (старий формат), так і об'єкт {packId, message}
      // (імпорт модпаків) — витягуємо читабельне повідомлення в обох
      // випадках, інакше користувач бачив би "[object Object]" замість
      // причини помилки (через це імпорт виглядав «завислим без причини»).
      // УВАГА: (err as any).message може САМ бути об'єктом (Wails серіалізує
      // Go-помилку вкладено), тож рекурсивно шукаємо перший рядковий
      // «смисловий» рівень — String(object) давав би "[object Object]".
      const msg = extractErrMessage(err)
      installError = msg
      showToast('error', t('notify.installFailed'), msg)
      // Помилка встановлення — сповіщаємо ЗАВЖДИ (не лише в неактивному
      // вікні): користувач може бути в іншій вкладці UI.
      App.NotifyOS(t('notify.installFailed'), msg)
    })
    on('installer:log', (msg: string) => {
      if (typeof msg === 'string' && /Помилка|помилка|error|Error|ERROR|failed|Failed/i.test(msg)) {
        installError = msg
        showToast('error', t('notify.installFailed'), msg)
      }
    })
    // Стадії запуску (checking/worker_update/java/loader) — сегментний
    // прогрес-бар на сторінці збірки читає launchProgress[instanceId].
    // done=true (успіх або помилка) прибирається із мапи за секунду, щоб
    // прогрес встиг домалюватись на 100% перед тим, як зникнути.
    on('launch:progress', (p: LaunchProgress) => {
      launchProgress = { ...launchProgress, [p.instanceId]: p }
      // Офлайн-попередження (warning, done=false): показуємо модалку і
      // ПРОДОВЖУЄМО слухати наступні події — це не кінець прогресу.
      if (p.warning && !p.done) {
        launchWarning = p.warning
      }
      if (p.done) {
        const id = p.instanceId
        setTimeout(() => {
          const next = { ...launchProgress }
          if (next[id] === p) { delete next[id]; launchProgress = next }
        }, p.error ? 4000 : 800)
      }
    })
    // Скасування запуску користувачем (кнопка «Зупинити» під час качки
    // Java/лоадера/Minecraft): це НЕ помилка — прибираємо прогрес-бар з
    // тайла, показуємо нейтральний тост і оновлюємо стан збірки (вона
    // лишається «встановленою», просто запуск не відбувся).
    on('launch:cancelled', (id: string) => {
      const next = { ...launchProgress }
      delete next[id]
      launchProgress = next
      const cancelled = new Set(launchCancelled)
      cancelled.add(id)
      launchCancelled = cancelled
      toast(t('launch.cancelled'), 'info')
      refreshBuilds()
    })
    // Стани збірок: реєстр оновлено / збірка стартувала / завершилась.
    // Стан вікна (розгорнуте/повноекранне) з нативного боку: оновлюємо
    // кнопку «На весь розмір», щоб вона показувала ПРАВИЛЬНИЙ стан, а не
    // лише те, що задав останній клік (баг: після рестарту з розгорнутою
    // сесією кнопка показувала «розгорнути», хоча вікно вже на весь екран).
    on('window:ui-state', (st: { maximised?: boolean; fullscreen?: boolean }) => {
      if (typeof st?.maximised === 'boolean') isMaximized = st.maximised
    })
    // Дублюючий запит стану: подія могла прийти до підписки (відновлення
    // розгорнутого вікна робиться ще на WindowRuntimeReady) — підтягуємо
    // актуальний стан опитуванням, щоб кнопка була правильною одразу.
    App.GetWindowUIState().then((st) => {
      if (typeof st?.maximised === 'boolean') isMaximized = st.maximised
    }).catch(() => {})
    on('network:status', (st: { online?: boolean }) => {
      if (typeof st?.online === 'boolean') networkOnline = st.online
    })
    // Шаурма-збірка: синк зафіксував повну відсутність мережі (manifest не
    // докачався) — показуємо позначку «офлайн» і зрозумілий тост замість
    // голої помилки мережі.
    on('sync:no-internet', (packID: string) => {
      networkOnline = false
      const name = packIndex.find(p => p.id === packID)?.name ?? packID
      toast(t('offline.syncFailed', { name }), 'warn', 6000)
    })
    // Перша перевірка мережі одразу (подія могла прийти до підписки).
    App.CheckNetwork().then((ok) => { networkOnline = ok }).catch(() => {})
    on('builds:changed', () => refreshBuilds())
    on('friends:update', (s: { invites?: unknown[]; unread?: Record<string, number> }) => {
      socialInvites = Array.isArray(s?.invites) ? s.invites.length : 0
      if (s?.unread && typeof s.unread === 'object') socialUnread = s.unread
    })
    on('friends:unread', (m: Record<string, number>) => { socialUnread = m && typeof m === 'object' ? m : {} })
    // Клік по картці віджета сповіщень у трей: показати шухляду «Друзі» й
    // (коли відомий accountId) одразу відкрити конкретний діалог.
    on('social:open-chat', (accountId: string) => {
      socialChatTarget = accountId || ''
      if (accountId) socialActiveId = accountId
      navigate('friends')
    })
    on('build:started', (e: BuildStartEvent) => {
      patchBuild(e.buildId, b => {
        // Збірка запущена на акаунті e.accountId. Якщо це поточний акаунт —
        // status=running (кнопка «Стоп»); якщо ІНШИЙ (напр. прив'язаний) —
        // додаємо його в runningOn, а стан файлів лишаємо (у чужих акаунтах
        // збірка виглядає не запущеною, з головою того, хто грає).
        const isHere = e.accountId === activeAccountId
        // Мапа по buildId: кожна паралельно запущена збірка на активному
        // акаунті отримує свій незалежний штамп старту сесії, замінюючи
        // старий на одне глобальне sessionStartedAt.
        //
        // startedAt (unix-мс) приходить з бекенду: для свіжого запуску — це
        // момент старту гри, а для ВІДНОВЛЕНОЇ сесії (лаунчер перезапустили,
        // гра все ще йде) — збережений час старту. Без цього після рестарту
        // лаунчера кнопка «Зупинити» показувала б таймер з нуля, хоч гра
        // йде вже давно (баг: «втрачається час сесії»).
        // startedAt === 0 можливий лише для старого running-game.json без
        // збереженого часу — у такому разі беремо поточний момент (0 був би
        // спавном 1970 року, а не часом гри).
        if (isHere) sessionStartedAtByBuild = { ...sessionStartedAtByBuild, [e.buildId]: (e.startedAt || Date.now()) }
        const others = (b.runningOn ?? []).filter(r => r.accountId !== e.accountId)
        return {
          ...b,
          status: isHere ? 'running' : b.status,
          runningOn: isHere ? others : [...others, { accountId: e.accountId, accountName: e.accountName }],
        }
      })
    })
    on('build:exit', (e: { buildId: string; accountId: string; code: number; wasUserStop?: boolean }) => {
      refreshBuilds()
      if (e.accountId === activeAccountId) {
        // Прибираємо ЛИШЕ таймер ЦІЄЇ збірки — інші паралельні сесії на
        // тому самому акаунті лишаються з власним живим таймером.
        const next = { ...sessionStartedAtByBuild }
        delete next[e.buildId]
        sessionStartedAtByBuild = next
      }
      // build:exit несе buildId — на відміну від безадресної game:exit,
      // тут точно відомо, ЯКА збірка завершилась (важливо, коли паралельно
      // йде кілька інших: крах Y не повинен зачіпати банер/консоль для X).
      //
      // !e.wasUserStop — БЕЗ ЦІЄЇ ПЕРЕВІРКИ ненульовий код виходу
      // примусово вбитого процесу (kill/taskkill, який і робить
      // StopInstance при натисканні «Зупинити» в лаунчері) виглядав так
      // само, як реальний крах гри: ОС майже завжди повертає ненульовий
      // код для вбитого процесу — це нормальна поведінка вбивства
      // процесу, а не ознака аварійного завершення. Раніше тут
      // перевірявся лише e.code !== 0, тож штатна ручна зупинка гри
      // показувала користувачу банер «гра завершилась аварійно», хоча
      // нічого аварійного не сталось.
      if (e.accountId === activeAccountId && e.code !== 0 && !e.wasUserStop) {
        launchedPackId = e.buildId
        gameCrashed = true
        try {
          crashBannerHidden = localStorage.getItem('shaurma.crashBannerHidden') === '1'
        } catch (err) { crashBannerHidden = false }
      }
    })
    on('repair:report', (e: import('./lib/types').RepairReportEvent) => {
      if (AUTO_REPAIR_OFF) return // авто-починка тимчасово вимкнена
      if (!e?.packId || !e.report?.issues?.length) return
      // Вікно вже відкрите — не підміняємо звіт під руками (це скидало вибір
      // і перезапускало діалог). Закрите користувачем — не відкриваємо знову
      // той самий набір проблем протягом хвилини (повторні події краху).
      if (repairDialog) return
      const sig = e.packId + '|' + e.report.issues.map((i: any) => i.id).sort().join(',')
      if (repairDismissed.sig === sig && Date.now() - repairDismissed.at < 60_000) return
      repairDialog = { packId: e.packId, report: e.report }
      if (launchedPackId === e.packId) gameCrashed = false
    })
    // game:started — БУДЬ-ЯКИЙ шлях запуску гри (кнопка «Грати», автозапуск,
    // LaunchInstanceOnServer з розділу «Сервери» чи віджета офіційного
    // сервера). Раніше gameRunning виставлялось ЛИШЕ у doLaunch(), тож після
    // «Приєднатись» кнопка «Грати» і кнопки приєднання не деактивувались —
    // лічильник гри не знав, що гра вже йде. Тепер стан єдиний для всіх шляхів.
    on('game:started', () => {
      gameRunning = true; gameCrashed = false
      // Мінімальне вікно прогресу запуску свою роботу зробило — гра пішла.
      launchOverlay = null
    })
    // Лаунчер запущено з ярлика на робочому столі (або юзер клікнув ярлик,
    // поки лаунчер уже працював — SingleInstance передав дані сюди):
    // відкриваємо збірку і запускаємо гру з персональними опціями ярлика
    // (акаунт/автоприєднання — як у Prism; решта — як звичайний запуск).
    on('shortcut:launch', (sc: any) => {
      const pid = typeof sc === 'string' ? sc : sc?.packId
      if (pid) {
        launchFromShortcut(pid, {
          accountId: typeof sc === 'string' ? '' : sc?.accountId,
          autoJoinServer: typeof sc === 'string' ? '' : sc?.autoJoinServer,
        })
      }
    })
    // Файл збірки відкрито через Windows-асоціацію, поки лаунчер УЖЕ
    // запущений (другий клік по .shaurmpack/.shaurmbundle — SingleInstance
    // передав шлях у shell → подія packfile:open) — запускаємо імпорт у
    // фоні (без модалки).
    on('friend:open', (url: any) => {
      openFriendLinkFromOS(typeof url === 'string' ? url : url?.url)
    })
    on('packfile:open', (path: any) => {
      const p = typeof path === 'string' ? path : path?.path
      if (p) void openPackFile(p)
    })
    // Живий прогрес експорту папки у .shaurmbundle (модалка експорту
    // оновлює свій прогрес-бар; події поза модалкою просто ігноруються).
    on('shaurmbundle:export:progress', (p: any) => {
      if (folderBundleExport) {
        folderBundleExport.done = p?.done ?? folderBundleExport.done
        folderBundleExport.total = p?.total ?? folderBundleExport.total
      }
    })
    // Закриття лаунчера з активною качкою → модалка попередження.
    on('sync:quit-warning', (packIDs: string[]) => { confirmQuitWhileSyncing(packIDs) })
    on('share:quit-warning', () => { confirmQuitWhileSharing() })
    offerShareRecovery()
    // Закриття лаунчера з запущеним сервером → модалка попередження.
    on('server:quit-warning', async () => {
      const ok = await confirmDialog('Сервери запущені', 'Закриття лаунчера зупинить усі запущені сервери. Продовжити?', {
        confirmLabel: 'Закрити', cancelLabel: 'Скасувати', danger: true
      })
      if (ok) {
        await App.StopAllServers()
        await App.QuitLauncher()
      }
    })
    // ── Гравець зайшов / вийшов на сервер ─────────────────────────────
    // Нік гравця у сповіщенні. Працює з будь-якої сторінки лаунчера, а не
    // лише коли відкрита сторінка цього сервера — саме цього бракувало
    // раніше (єдине місце, де згадувався вхід гравця, — рядок логу java).
    loadServerNames()
    // Автостворення акаунта Шаурми після входу в Microsoft (бекенд: afterMicrosoftLogin).
    // Вхід через Microsoft відбувається у браузері: ліміт пристроїв приходить подією.
    on('shaurma-account:device-limit', (r: SignInResult) => {
      deviceLimit = { devices: r?.devices ?? [], max: r?.maxDevices ?? 10 }
    })
    // До акаунта додано інший пристрій: питаємо «Це не ви?» (одне натискання вилучає його й ротує ключ чату).
    on('account:device_added', async (e: { deviceId: string; name: string; os: string; country: string; clientName: string }) => {
      if (!e?.deviceId) return
      const nm = e.name || e.clientName || '?'
      const notMe = await confirmDialog(t('dev.new.title'),
        t('dev.new.text', { name: nm, os: e.os || '—', country: e.country || '?' }),
        { confirmLabel: t('dev.new.open'), cancelLabel: 'OK' })
      if (!notMe) return
      try { await App.RevokeShaurmaDevice(e.deviceId, true) }
      catch (err: any) { toast(String(err?.message ?? err), 'error') }
    })
    on('shaurma-account:autocreate', (ev: { status: string; message?: string }) => {
      if (!ev) return
      if (ev.status === 'start') toast('Підтвердіть вхід у Microsoft у браузері, щоб створити акаунт Шаурми', 'info', 8000)
      else if (ev.status === 'ok') toast('Акаунт Шаурми створено', 'success', 4000)
      else if (ev.status === 'error') toast(`Акаунт Шаурми не створено: ${ev.message || 'помилка'}. Спробуйте в Налаштування → Скін-акаунт`, 'error', 9000)
    })
    on('server:player', (ev: ServerPlayerEvent) => {
      if (!ev || !ev.name || ev.action === 'reset') return
      // Сервер міг з'явитися вже після старту лаунчера — догружаємо назви.
      if (!serverNamesCache[ev.serverId]) loadServerNames()
      const srvName = serverNamesCache[ev.serverId] || ''
      const where = srvName ? ` сервера «${srvName}»` : ' сервера'
      if (ev.action === 'join') {
        toast(`Гравець ${ev.name} приєднався до${where}`, 'success', 5000)
      } else {
        const reason = ev.reason ? ` (${ev.reason})` : ''
        toast(`Гравець ${ev.name} вийшов із${where}${reason}`, 'info', 5000)
      }
    })
    // Живий прогрес сканування «Пам'ять і кеш»: яка тека/збірка зараз
    // рахується (storage:usage-scan / packs-scan) і який мажор Java
    // сканується (storage:java-scan) — прогрес-бари замість пустого розділу.
    const onStorageScan = (p: { done: number; total: number; label: string }) => { storageScan = p }
    on('storage:usage-scan', onStorageScan)
    on('storage:packs-scan', onStorageScan)
    on('storage:java-scan', (p: { done: number; total: number; major: number }) => { javaProgress = p })
    // Стан бази даних при старті (відновлено з резервної копії / стерто
    // навмисно / працює в пам'яті) — модальне вікно з поясненням, щоб не
    // мовчати про втрату/відновлення даних користувача.
    on('db:health', (h: DBHealth) => { if (h && (h.restoredFromBackup || h.erasedIntentionally || h.fallbackMemory)) dbHealthInfo = h })
  // Консоль гри більше НЕ дублює рядки тут — нею займається компонент
  // Сервер «разом зі збіркою» не стартував (порт зайнятий, мало RAM…) —
  // гра все одно запускається, а користувач бачить причину.
  // Ланцюг мережі Velocity не вдався у фоні (сервер не стартував разом із
  // проксі тощо) — показуємо причину.
  on('server:network-event', (e: { message: string }) => {
    if (e?.message) toast(e.message, 'error')
  })
  on('server:linked-error', (e: { serverName: string; message: string }) => {
    toast(t('packlink.startFailed', { name: e?.serverName ?? '', message: e?.message ?? '' }), 'error')
  })

  // GameConsole (підписки на console:line всередині нього, з віртуалізацією
  // та обрізанням буфера за consoleMaxLines). Консоль тепер живе у вкладці
  // «Консоль» на сторінці збірки. Тут лишається лише реакція на game:exit.
  // Гра завершилась: якщо лаунчер був схований у трей (закритий під
  // час гри), показуємо вікно назад автоматично. Ненульовий код виходу
  // = краш — вискакує модальне вікно з повідомленням (замість старого
  // банера на «Моїх збірках») поверх будь-якої сторінки.
  on('game:exit', async (code: number) => {
    // Мінімальне вікно прогресу запуску: гра завершилась (у т.ч. краш)
    // — прибираємо оверлей, повертаємось до звичайного інтерфейсу.
    launchOverlay = null
    // game:exit — безадресна, глобальна подія («якась гра завершилась»);
    // детальний стан/крах конкретної збірки вже опрацьовано в build:exit
    // (вище, з buildId). Тут лишились ЛИШЕ дії, що дійсно глобальні:
    // чи є ще хоч якась гра, і чи час закрити/повернути вікно лаунчера.
    gameRunning = await App.IsGameRunning().catch(() => false)
    // Захист від "застряглого" таймера, якщо десь build:exit не прийшов:
    // коли взагалі нічого не запущено, мапа таймерів точно має бути пуста.
    if (!gameRunning) sessionStartedAtByBuild = {}
    // Повернути вікно / закрити лаунчер після гри вирішує backend
    // (gameExitAction + restoreOnCrash) — тут лише оновлюємо дані.
    loadPlaytimes()
    loadAnalytics()
  })

  async function loadPlaytimes() {
    try { playtimes = (await App.GetPlaytime()) ?? {} } catch (e) { console.error(e) }
  }

  // Аналітика ігрового часу перечитується після виходу з гри (build:exit
  // викликає обидва) — щоб стовпчики/топ оновились одразу після сесії.
  async function loadAnalytics() {
    // Дефолтний знімок — активний ГРАВЕЦЬ (за ніком, а не ID акаунта:
    // нік переживає вихід з акаунта); сама панель (AnalyticsPanel) дозволяє
    // перемкнутись на іншого гравця/всіх і перекачає свої дані.
    try { analytics = (await App.GetAnalytics(activeAccount()?.username ?? '')) ?? null } catch (e) { console.error(e) }
  }
  })
</script>

<!-- Картка збірки — ОДИН сніпет для головного списку й для вікна папки: папка показує
     ту саму картку, що й «Мої збірки» (inFolder лише міняє drag-out і закриття папки при кліку). -->
{#snippet packTile(bv: BuildView, gi: number, inFolder: boolean)}
  {@const pk = bv.build}
  {@const status = bv.status}
  {@const prog = bv.progress}
  {@const pcol = pk.color || 'var(--orange)'}
  {@const othersRunning = otherRunningAccounts(bv)}
  {@const icon = packAsset(pk.iconUrl)}
  {@const iconSrc = packIconSrc(icon)}
  {@const lp = launchProgress[pk.id]}
  {@const sti = lp && !lp.done ? launchStageIcon(lp.stage) : null}
  {@const dsti = !sti ? syncStageIcon(bv) : null}
  {@const limg = lp && !lp.done && lp.stage === 'loader' ? loaderImgFor(pk.loaderType) : undefined}
  {@const glow = glowClass(bv, lp)}
  {@const mig = migrateByPack[pk.id]}
  {@const isDragSrc = dragSrcId === pk.id}
  {@const isDropTarget = dropTargetId === pk.id}
  <div class="inst-tile rise-in" class:running={status === 'running'} class:pin-badge={isPinned(pk.id)} class:glow-green={glow === 'glow-green'} class:glow-orange={glow === 'glow-orange'} class:glow-red={glow === 'glow-red'} class:glow-blue={glow === 'glow-blue'} class:glow-yellow={glow === 'glow-yellow'} class:glow-mig={!!mig && !lp} class:drag-src={isDragSrc} class:drop-before={isDropTarget && dropPos === 'before'} class:drop-after={isDropTarget && dropPos === 'after'} data-pack-id={pk.id} onpointerdown={(e) => packPointerDown(e, pk.id, pcol, inFolder)} onpointermove={packPointerMove} onpointerup={pointerDragEnd} oncontextmenu={(e) => openCtxMenu(e, pk.id)} onclick={() => { if (inFolder) { if (suppressPackClick) return; folderModalId = null } openPackPage(pk.id) }} style="--pcol:{pcol};--drop-c:{dragPackColor};animation-delay:{Math.min(gi,10)*30}ms">
    <div class="it-ic" class:has-icon={!!iconSrc} class:blink={status === 'downloading' || status === 'updating'} style="--pcol:{pcol}">
      {#if iconSrc}
        <AnimatedIcon src={iconSrc} paused={shouldPauseIcon(icon, inFolder ? 'folder' : 'grid', true)} alt="" />
      {:else}
        <i class="ti {pk.icon || 'ti-puzzle'}" style="color:var(--pcol)"></i>
      {/if}
      {#if mig && !lp}
        <!-- Іконка міграції модів поверх іконки збірки: видна
             завжди під час міграції (навіть коли збірка ще
             качається і підпис зайнятий прогресом качки). -->
        <span class="it-mig-badge" title={t('mods.migratingTitle')}><i class="ti ti-arrows-right-left"></i></span>
      {/if}
    </div>
    <div class="it-info">
      <div class="it-name">{pk.name}</div>
      <div class="it-sub" class:live={!!lp && !lp.done || status === 'downloading' || status === 'updating'}>
        {#if lp && !lp.done}
          <i class="ti ti-loader spin"></i>
          <span>{launchStageLabel(lp.stage)}</span>
        {:else if mig && !lp}
          <!-- Міграція модів має ПРІОРИТЕТ над прогресом качки:
               після створення збірки з міграцією підпис показує
               саме її (а не «качається файли»), бо для гравця
               важливо бачити, що моди переносяться. -->
          <span class="mig-chip" title={mig.currentFile}>
            <i class="ti ti-arrows-right-left spin-slow"></i>
            <span>{t('mods.migratingTitle')} {mig.done}/{mig.total}</span>
          </span>
        {:else if status === 'downloading' || status === 'updating'}
          <i class="ti {prog?.mode === 'paused' ? 'ti-player-pause' : 'ti-download'}"></i>
          <span>{packSubText(bv, lp)}</span>
        {:else if othersRunning.length}
          <span class="it-heads" title={othersRunning.map(o => o.username).join(', ')}>
            {#each othersRunning as o (o.id)}
              <img src={headFor(o.uuid)} alt="" title={o.username} />
            {/each}
          </span>
        {:else}
          <span>{pk.loaderType} {pk.mcVersion}</span>
        {/if}
      </div>
      {#if mig && !lp && mig.name}
        <div class="it-mig-name" title={mig.currentFile}>
          {#if mig.iconUrl}<img src={mig.iconUrl} alt="" />{/if}
          <span>{mig.name}</span>
        </div>
      {/if}
      {#if lp && !lp.done && lp.message}
        <div class="it-stage"><i class="ti ti-file"></i> {lp.message}</div>
      {/if}
      {#if settings.showGameTime && playtimes[pk.id]}
        <div class="it-time"><i class="ti ti-clock"></i> {fmtPlaytime(playtimes[pk.id])}</div>
      {/if}
    </div>
    {#if sti}
      <!-- Під час встановлення Java/лоадера/оновлення файлів
           кнопка стає «Стоп» (скасувати встановлення) — 1:1 як
           у sidebar (.sp-st). -->
      <button class="it-st st-{lp.stage} sp-stop" onclick={(e: any) => { e.stopPropagation(); cancelGameInstall(pk.id) }} title={launchStageLabel(lp.stage) + ' — ' + t('pack.stop')}>
        {#if limg}
          <img src={limg} alt="" />
        {:else}
          <i class="ti {sti.icon}"></i>
        {/if}
      </button>
    {:else if dsti}
      <!-- Під час синхронізації файлів (downloading/
           manifest_fetched/extracting/finalizing) — так само
           живий індикатор стадії, окрім паузи (там лишається
           кнопка «продовжити»). -->
      <div class="it-st {dsti.cls}" title={downloadStageLabel(prog?.stage)}>
        <i class="ti {dsti.icon}"></i>
      </div>
    {:else if mig && !lp}
      <button class="it-btn it-mig" disabled title={t('mods.migratingTitle')}>
        <i class="ti ti-arrows-right-left"></i>
      </button>
    {:else}
      <button class="it-btn it-{spBtnClass(bv)}" onclick={(e: any) => { e.stopPropagation(); packBtnAction(bv) }} title={spBtnTitle(bv)}>
        {#if status === 'running'}<i class="ti ti-player-stop"></i>
        {:else if status === 'ready'}<i class="ti ti-player-play"></i>
        {:else if status === 'needs-update'}<i class="ti ti-refresh"></i>
        {:else if status === 'not-installed'}<i class="ti ti-download"></i>
        {:else if prog?.mode === 'paused'}<i class="ti ti-player-pause"></i>
        {:else if status === 'downloading' || status === 'updating'}<i class="ti ti-player-pause"></i>
        {:else}<i class="ti ti-loader spin"></i>
        {/if}
      </button>
    {/if}
    {#if lp && !lp.done}
      <div class="it-prog"><div class="it-prog-fill" style="width:{launchStagePct(lp)}%;background:{launchStageColor(lp.stage)}"></div></div>
    {:else if prog && (status === 'downloading' || status === 'updating')}
      <div class="it-prog"><div class="it-prog-fill" style="width:{prog.percent}%;background:{downloadStageColor(prog.stage)}"></div></div>
    {:else if mig && !lp}
      <div class="it-prog"><div class="it-prog-fill mig-fill" style="width:{mig.total ? Math.round(mig.done / mig.total * 100) : 0}%"></div></div>
    {/if}
  </div>

{/snippet}

<!-- ═══════════════════ FULL SCREEN WIZARD ═══════════════════ -->
{#if showWizard}
  <div class="wiz-wrap">
    <Wizard onFinished={wizardFinished} />
  </div>
{/if}

<!-- ═══════════ СТАРТОВА АНІМАЦІЯ ПІСЛЯ МАЙСТРА ═══════════ -->
{#if showIntro}
  <LaunchIntro onDone={() => (showIntro = false)} />
{/if}

<!-- ═══════════════════ MAIN UI ═══════════════════ -->
<div class="launcher-window" class:maximized={isMaximized} id="win">
  <!-- Святковий режим 22.10 (кульки, гірлянда, конфеті) -->
  <BirthdayMode />
  {#if toastMsg}
    <!-- Віджет-тост встановлення: помилка або успішне завершення фонового
         завантаження. Фіксований праворуч знизу, auto-зникає за 7с,
         клік — закрити одразу. -->
    <div class="install-toast {toastMsg.kind}" role="alert" onclick={() => (toastMsg = null)}>
      <div class="it-icon">
        {#if toastMsg.kind === 'error'}
          <i class="ti ti-alert-triangle"></i>
        {:else}
          <i class="ti ti-check"></i>
        {/if}
      </div>
      <div class="it-body">
        <div class="it-title">{toastMsg.title}</div>
        <div class="it-text">{toastMsg.text}</div>
      </div>
      <button class="it-close" aria-label="close"><i class="ti ti-x"></i></button>
    </div>
  {/if}
  <Sidebar
    bind:instanceTab={instanceTab}
    bind:openFolderIds={openFolderIds}
    bind:accDropdownOpen={accDropdownOpen}
    sidebarCollapsed={sidebarCollapsed}
    sidebarWidth={sidebarWidth}
    sidebarScale={sidebarScale}
    sbResizing={sbResizing}
    currentPage={currentPage}
    deactivatedNavPage={deactivatedNavPage}
    deactivatedPackId={deactivatedPackId}
    wardrobeVisible={wardrobeVisible}
    isShaurmaEdition={isShaurmaEdition}
    builds={builds}
    autoPackIds={autoPackIds}
    tabPinned={tabPinned}
    tabUnpinned={tabUnpinned}
    sidebarOnlyPinned={sidebarOnlyPinned}
    folderViews={folderViews}
    layoutItems={sidebarLayoutItems}
    launchProgress={launchProgress}
    migrateByPack={migrateByPack}
    dragSrcId={dragSrcId}
    dropTargetId={dropTargetId}
    dropPos={dropPos}
    dragPackColor={dragPackColor}
    dragFolderId={dragFolderId}
    dragFolderTarget={dragFolderTarget}
    openPackId={openPackId}
    suppressPackClick={suppressPackClick}
    accounts={accounts}
    activeAccountId={activeAccountId}
    shaurmaFriendsVisible={shaurmaFriendsVisible}
    friendsBadge={socialBadge}
    networkOnline={networkOnline}
    pageIcons={pageIcons}
    pageTitles={pageTitles}
    {navigate}
    {openCreatePack}
    {openImportPack}
    {openCreateFolderModal}
    openPacksFolder={() => App.OpenInstancesFolder().catch((e: any) => toast(t('error.withMessage', { message: e }), 'error'))}
    {toggleSidebar}
    {startSidebarResize}
    {toggleAccDropdown}
    {switchAccount}
    {removeAccount}
    {openRenameAccount}
    {openCtxMenu}
    {openFolderCtxMenu}
    {packPointerDown}
    {packPointerMove}
    {folderPointerDown}
    {folderPointerMove}
    {pointerDragEnd}
    {cancelGameInstall}
    {packBtnAction}
    {openPackPage}
    {packIconSrc}
    {shouldPauseIcon}
    {loaderImgFor}
    {glowClass}
    {syncStageIcon}
    {launchStageIcon}
    {spBtnClass}
    {spBtnTitle}
    {packSubText}
    {otherRunningAccounts}
    {isPinned}
    {headFor}
    {activeAccount}
    onaddaccount={() => { accDropdownOpen = false; modalLogin = true; loginMsStep = 'idle' }}
  />

  <div class="main">
    <Topbar
      icon={topbarIcon}
      title={topbarTitle}
      minimized={currentPage === 'browser' && !browserProject && !browserScrolled}
      crumb={topbarCrumb}
      isMaximized={isMaximized}
      onsearch={openGlobalSearch}
      onmin={winMin}
      onmax={winMax}
      onclose={winClose}
      onopenmod={openModPageFromPack}
    />

    <div class="content {currentPage === 'wardrobe' || currentPage === 'browser' || currentPage === 'friends' || currentPage === 'settings' || (currentPage === 'servers' && !showServerWizard) ? 'content-no-scroll' : ''} {currentPage === 'instances' && openPackId ? 'content-pack-open' : ''} {currentPage === 'friends' ? 'content-friends' : ''}">
      {#key pageKey}
      <!-- .page-view: ключова обгортка для анімації перемикання розділів
           sidebar (див. pageKey вище) — плавний fade+slide+blur при зміні
           сторінки/відкритті збірки/«Нової збірки». Перемонтування тут
           безпечне:           стани сторінок зберігаються глобально (uiState,
           downloadByPack, browserVisited…), а скрол відновлюється ефектами.
           hidden: коли браузер показує свій змонтований назавжди холдер
           (.cb-page-holder поза обгорткою) або сторінка (гардероб, settings)
           рендериться ОДРАЗУ під .content (не тут), ця обгортка порожня —
           ховаємо, щоб вона не забирала flex-висоту в режимі content-no-scroll
           (інакше порожній flex:1 .page-view штовхає контент на середину
           вікна — той самий баг, що був у браузера). -->
      <div class="page-view" hidden={(currentPage === 'browser' && !browserProject && !browserAuthor) || currentPage === 'wardrobe' || currentPage === 'settings' || currentPage === 'analytics' || currentPage === 'friends'}>
      {#if currentPage === 'instances' && showCreatePack}
        <CreatePackPage {settings} initialTab={createPackTab} onTabChange={(v) => (createPackTab = v)} folders={packFolders} onCancel={closeCreatePack} onCreated={onPackCreated} />
      {:else if currentPage === 'instances' && openPack}
        <!-- Кожна збірка = своє власне «вікно»: {#key openPack.id} перемонтовує
             сторінку при перемиканні збірок через сайдбар, щоб під старою
             сторінкою (напр. екран завантаження попередньої збірки) не лишалась
             видимою. Прогрес качки не губиться — він живе глобально у
             downloadByPack/buildView.progress і показується при поверненні. -->
        <!-- Ключ — сам openPackId (рядок), а не openPack.id: якщо в мить
             перечитування збірок openPack тимчасово null, вираз ключа НЕ
             повинен падати на читанні .id. -->
        {#key openPackId}
          <PackInstancePage
            pack={stablePack}
            canShare={shaurmaFriendsVisible}
            {settings}
            buildView={builds.find(b => b.build.id === stablePack.id) ?? null}
            allBuilds={orderedAllBuildViews}
            packPicker={packPicker}
            runningAccounts={otherRunningAccounts(builds.find(b => b.build.id === stablePack.id))}
            headOf={(a: Account) => headFor(a.uuid)}
            activeAccount={activeAccount()}
            launchProgress={launchProgress[stablePack.id] ?? null}
            playtimeSeconds={playtimes[stablePack.id] ?? 0}
            {gameRunning}
            sessionStartedAt={sessionStartedAtByBuild[stablePack.id] ?? null}
            {consoleSignal}
            {gallerySignal}
            {modsFocusSignal}
            installError={installError}
            onBack={() => openPackId = null}
            onLaunch={launchWithHealthGuard}
            onLaunchOnServer={joinPinnedServer}
            onStop={() => stopInstance(stablePack.id)}
            {cancelGameInstall}
            onDownload={downloadPack}
            onDelete={deletePack}
            onPackUpdated={refreshBuilds}
            // Зміна ID кастомної збірки: перемикаємо відкриту сторінку на
            // новий ID (старий вже не існує), інакше openPack стає null і
            // користувача викидає в список «Мої збірки». Бекенд при rename
            // переносить усі посилання (папка/порядок/закріплення/ярлик),
            // але фронтенд має СИНХРОННО оновити свої локальні копії — бо
            // $effect очищення папок спрацьовує на builds:changed і може
            // встигнути ВИКИНУТИ збірку з папки (локальні packFolders ще зі
            // старим ID, а builds вже з новим) і ЗБЕРЕГТИ зіпсований стан,
            // перезаписавши міграцію бекенду. Тому спершу мігруємо папки/
            // порядок тут (до будь-якого перечитування), потім підстрахуємось
            // loadPackLayout()/loadPackFolders().
            onPackRenamed={(oldId, newId) => {
              // Папки: замінюємо старий ID на новий (збірка лишається у
              // своїй папці, не створюється «фантом» зі старим ID).
              if (oldId !== newId) {
                // Захист від очищення папок, доки builds ще зі старим id:
                // без цього $effect нижче викинув би збірку з папки (див.
                // packRenamePending). Знімаємо прапор ПІСЛЯ перечитання builds.
                packRenamePending = true
                const foldersMigrated = packFolders.map(f => ({
                  ...f,
                  packIds: f.packIds.map(pid => pid === oldId ? newId : pid),
                }))
                packFolders = foldersMigrated
                savePackFolders()
                // Порядок і закріплення: та сама позиція, те саме закріплення.
                const orderMigrated = packLayout.order.map(id => id === oldId ? newId : id)
                const pinnedMigrated = packLayout.pinned.map(id => id === oldId ? newId : id)
                if (orderMigrated.join('\u0000') !== packLayout.order.join('\u0000') ||
                    pinnedMigrated.join('\u0000') !== packLayout.pinned.join('\u0000')) {
                  packLayout = { order: orderMigrated, pinned: pinnedMigrated }
                  savePackLayout()
                }
                openPackId = newId
                pushPackHistory(newId)
                // Поки відбувається перечитування (builds зі старим id),
                // очищення папок притримується прапором packRenamePending.
                Promise.all([loadPackLayout(), loadPackFolders(), refreshBuilds()])
                  .catch(() => {})
                  .finally(() => { packRenamePending = false })
              }
            }}
            onSearchMods={() => openBrowserForPack(stablePack.id)}
            // «Додати з браузера» у вкладці «Контент»: браузер відкривається
            // одразу на потрібному типі контенту (моди/ресурспаки/шейдери/
            // датапаки) з контекстом цієї збірки.
            onSearchContent={(pt: string) => openBrowserForPack(stablePack.id, pt)}
            onOpenModPage={openModPageFromPack}
            migration={migrateByPack[stablePack.id] ?? null}
            {fmtPlaytime}
            {fmtSessionTime}
          />
        {/key}
      {:else if currentPage === 'instances'}
        <div class="page-actions">
          <button class="btn btn-ghost" onclick={() => navigate('analytics')}><i class="ti ti-chart-bar"></i> {t('nav.analytics')}</button>
          <button class="btn btn-ghost" onclick={() => navigate('browser', true)}><i class="ti ti-world-search"></i> {t('search')}</button>
          <button class="btn btn-ghost" onclick={openImportPack}><i class="ti ti-download"></i> {t('createPack.tab.import')}</button>
          <!-- Створити папку: модалка з назвою/кольором і вибором збірок,
               які одразу додадуться в папку. Лише для КАСТОМНИХ збірок —
               папки в Шаурма-каталозі не мають сенсу (той список керується
               синком з каталогу, а сам редактор папки все одно пропонує на
               вибір лише кастомні збірки, див. feCandidates нижче). Раніше
               кнопка була видима завжди (над таббаром, поза instanceTab),
               тож відкривалась і на вкладці «Шаурма», де їй бути не мало. -->
          {#if instanceTab === 'custom'}
            <button class="btn btn-ghost" onclick={openCreateFolderModal}><i class="ti ti-folder-plus"></i> {t('packs.folderCreate')}</button>
          {/if}
          <!-- Відкрити теку, де лежать УСІ встановлені збірки. -->
          <button class="btn btn-ghost" onclick={() => App.OpenInstancesFolder().catch((e: any) => toast(t('error.withMessage', { message: e }), 'error'))}><i class="ti ti-folder-open"></i> {t('packs.openInstancesFolder')}</button>
          <button class="btn btn-primary" onclick={openCreatePack}><i class="ti ti-plus"></i> {t('instances.create')}</button>
        </div>

        <!-- Динамічний банер останнього офіційного оновлення Minecraft:
             картинку підтягує бекенд (news_banner.go) — Mojang-маніфест
             (latest.release) + news.json (новина з цією версією в title),
             файл кешується в <cache>/banners/ і оновлюється сам при кожному
             запуску лаунчера, тож авто-збірки auto-vanilla/auto-snapshot і
             банер завжди показують одну й ту саму актуальну версію гри. -->
        <NewsBanner />

        <!-- Закріплені сервери: усі закріплені сервери з будь-яких збірок
             вище за картки збірок. Мінімалістичні рядки з іконкою, MOTD,
             бейджем збірки і меню «⋯» (скопіювати адресу / відкрити збірку /
             пінганути / відкріпити). -->
         {#if pinnedServers.length}
          <PinnedServersSection
            servers={pinnedServers}
            pings={pinnedPings}
            pinging={pinnedPinging}
            onCopy={(a) => copyAddress(a)}
            onOpenPack={(id) => openPackPage(id)}
            onPing={(a) => pingOnePinned(a)}
            onUnpin={(packId, addr) => unpinPinnedServer(packId, addr)}
            onPingAll={() => pingAllPinned()}
            onJoin={(packId, addr) => joinPinnedServer(packId, addr)}
            onReorder={reorderPinnedServers}
            compact={pinnedServersCompact}
            {builds}
            launchProgress={launchProgress}
          />
        {/if}

        <div class="tabbar">
          {#if isShaurmaEdition && builds.length > 0}
            <button class="tab-btn" class:active={instanceTab === 'shaurma'} onclick={() => instanceTab = 'shaurma'}>
              <i class="ti ti-flame"></i> {t('shaurma')} <span class="num">{shaurmaBuildsCount}</span>
            </button>
          {/if}
          <button class="tab-btn" class:active={instanceTab === 'custom'} onclick={() => instanceTab = 'custom'}>
            <i class="ti ti-tools"></i> {t('custom')} <span class="num">{customBuildsCount}</span>
          </button>
        </div>

        <div class="tile-grid">
          {#each gridItems as item, gi (item.kind === 'folder' ? 'fld:' + item.id : 'pk:' + item.id)}
            {#if item.kind === 'folder'}
              <!-- Плитка ПАПКИ в «Мої збірки»: картка як у сітці (не на
                   всю ширину). Клік ВІДКРИВАЄ МОДАЛЬНЕ ВІКНО папки зі
                   збірками всередині (fm-grid). Тягнеться нарівні зі
                   збірками (перестановка серед папок і збірок — папка може
                   стояти МІЖ двома збірками); дроп збірки на плитку додає
                   її в папку. -->
              {@const fv = folderViews.find(f => f.folder.id === item.id)}
              {#if fv}
                {@const folder = fv.folder}
                {@const fpcol = folder.color || 'var(--orange)'}
                {@const fIsDropTarget = dragFolderTarget === folder.id}
                {@const fIsDragSrc = dragFolderId === folder.id}
                {@const fIsDropTarget2 = dropTargetId === folder.id}
                {@const fIcons = fv.packs.slice(0, 3).map((p: BuildView) => {
                  const fic = packAsset(p.build.iconUrl)
                  return { src: packIconSrc(fic), paused: shouldPauseIcon(fic, 'grid', false), pcol: p.build.color || fpcol, preset: p.build.icon || 'ti-puzzle' }
                })}
                <FolderTile class="rise-in {fIsDropTarget || (fIsDropTarget2 && (dropPos === 'before' || dropPos === 'after')) ? 'folder-drop' : ''} {fIsDropTarget2 && dropPos === 'before' ? 'drop-before' : ''} {fIsDropTarget2 && dropPos === 'after' ? 'drop-after' : ''} {fIsDragSrc ? 'drag-src' : ''}"
                            data-folder-id={folder.id} data-folder-drop={folder.id}
                            style="--drop-c:{dragPackColor};animation-delay:{Math.min(gi,10)*30}ms"
                            name={folder.name} color={fpcol} icon={folder.icon} icons={fIcons} total={fv.packs.length} animated
                            onpointerdown={(e: PointerEvent) => folderPointerDown(e, folder.id)} onpointermove={folderPointerMove} onpointerup={pointerDragEnd}
                            onclick={() => { if (suppressPackClick) return; folderModalId = folder.id }}
                            oncontextmenu={(e: MouseEvent) => { e.preventDefault(); openFolderCtxMenu(e, folder.id) }}>
                  {#snippet sub()}{fv.packs.length} {t('packs.folderCount')}{/snippet}
                </FolderTile>
              {/if}
            {:else}
              {@const bv = builds.find(b => b.build.id === item.id)}
              {#if bv}
              {@render packTile(bv, gi, false)}
              {/if}
            {/if}
          {/each}
          </div>

        {#if gridItems.length === 0}
          <!-- Порожній розділ «Мої збірки»: замість одної картки-плюсика —
               іконка, напис і кнопки «Створити збірку» / «Імпортувати». -->
          <div class="empty-packs">
            <div class="empty-packs-ic"><i class="ti ti-packages"></i></div>
            <div class="empty-packs-title">{t('instances.empty')}</div>
            <div class="empty-packs-acts">
              <button class="btn btn-primary" onclick={openCreatePack}><i class="ti ti-plus"></i> {t('instances.create')}</button>
              <button class="btn btn-ghost" onclick={openImportPack}><i class="ti ti-download"></i> {t('createPack.tab.import')}</button>
            </div>
          </div>
        {/if}

        {#if settings.showTotalGameTime}
          <div class="total-time-banner"><i class="ti ti-clock-hour-4"></i> {t('instances.totalTime')}: <b>{fmtPlaytime(Object.values(playtimes).reduce((a, b) => a + (b || 0), 0))}</b></div>
        {/if}
        <!-- Помилки встановлення ТУТ не показуються: вони у віджеті-тості
             (праворуч знизу) та OS-сповіщенні — щоб «Мої збірки» не
             засмічувались панелями помилок. -->
      {/if}

      {#if currentPage === 'browser' && browserAuthor}
        <!-- Сторінка АВТОРА (клік по автору на сторінці проєкту): аватар,
             біо + всі його проєкти — як UserPage у Modrinth app. Показується
             поверх проєкту: «назад» повертає на той самий проєкт. -->
        <AuthorPage
          source={browserAuthor.source}
          authorId={browserAuthor.authorId}
          authorName={browserAuthor.authorName}
          onBack={() => browserAuthor = null}
          openProject={(s, id) => { browserAuthor = null; openBrowserProject(s, id) }}
        />
      {:else if currentPage === 'browser' && browserProject}
        <!-- Сторінка проєкту (модпак/мод/ресурспак/шейдер/датапак):
             банер, версії, встановлення з прогресом і позначкою
             «вже встановлено» (Modrinth app-стиль). -->
        <ModpackPage
          source={browserProject.source}
          projectId={browserProject.projectId}
          onBack={() => browserProject = null}
          packContext={browserPackContext}
          packs={browserPacks}
          packPicker={packPicker}
          servers={browserServers}
          onOpenPack={(id) => { browserProject = null; openPackPage(id) }}
          onAuthor={(authorId, authorName) => { if (browserProject) openBrowserAuthor(browserProject.source, authorId, authorName) }}
          onChangePackContext={(ctx) => browserPackContext = ctx}
          onPickServerContext={pickProjectServer}
          bannerFull={!!(settings as any).packBannerFull}
          canShare={shaurmaFriendsVisible}
          onGoFriends={() => navigate('friends')}
        />
      {/if}
      </div>
      {/key}
      {#if browserVisited}
        <!-- Браузер контенту змонтований ЗАВЖДИ (після першого відвідування) і
             лише ховається: стан пошуку/фільтрів/вкладки зберігається між
             переходами. hidden забирає його з розкладки (див. .cb-page-holder). -->
        <div class="cb-page-holder" hidden={currentPage !== 'browser' || !!browserProject}>
          <ContentBrowser
            packContext={browserPackContext}
            serverContext={browserServerContext}
            servers={browserServers}
            packs={browserPacks}
            packPicker={packPicker}
            openProject={openBrowserProject}
            onOpenPack={(id) => { browserProject = null; openPackPage(id) }}
            onPickServer={pickBrowserServer}
            onClearServerContext={() => { browserServerContext = null }}
            visible={currentPage === 'browser' && !browserProject}
          />
        </div>
      {/if}

      {#if currentPage === 'wardrobe'}
        <!-- shaurma — гейт для скін-системи (Google-вхід / нік / власний
             скін офлайн-акаунта): у базовій редакції цих методів у
             backend.dll немає взагалі. Передаємо skinsEnabled, а не сам
             isShaurmaEdition. Вимикач disableSkins тут не задіяний: він лише
             керує java-агентом при запуску гри, а не інтерфейсом. -->
        <Wardrobe activeAccount={activeAccount()} shaurma={skinsEnabled} nickMismatch={wardrobeNickMismatch} skinAuthNick={skinAuth.ok ? skinAuth.nick : ''} />
      {/if}

      {#if currentPage === 'friends'}
        <!-- Розділ «Друзі»: список + чат (Shaurma) і друзі Minecraft. -->
        <FriendsPage
          shaurmaAvailable={shaurmaFriendsVisible}
          openAccountId={socialChatTarget}
          bind:activeId={socialActiveId}
          onOpenMod={openModPageFromPack}
        />
      {/if}

      {#if currentPage === 'analytics'}
        <!-- Розділ «Аналітика»: підсумки ігрового часу, стовпчики за 7 днів /
             тижні / місяці / роки та топ збірок. Оновлюється після кожної
             завершеної сесії (loadAnalytics у build:exit). -->
        <div class="analytics-page">
          <AnalyticsPanel summary={analytics} builds={builds} accounts={accounts} activeAccountName={activeAccount()?.username ?? ''} lang={settings.language} heads={heads} />
        </div>
      {/if}

      {#if currentPage === 'settings'}
      <SettingsPage
        bind:settingsTab={settingsTab}
        settings={settings}
        settingsSchema={settingsSchema}
        bind:versionsModalOpen={versionsModalOpen}
        bind:javaModalOpen={javaModalOpen}
        bind:dragAcc={dragAcc}
        bind:dragOverRow={dragOverRow}
        accounts={accounts}
        activeAccountId={activeAccountId}
        skinAuthNick={skinAuth.ok ? skinAuth.nick : ''}
        skinsEnabled={skinsEnabled}
        storageInfo={storageInfo}
        storageScan={storageScan}
        storageLoading={storageLoading}
        storageVersions={storageVersions}
        storagePacks={storagePacks}
        storageSuggestions={storageSuggestions}
        orphanLibs={orphanLibs}
        orphanLibsBusy={orphanLibsBusy}
        bind:orphanLibsModalOpen={orphanLibsModalOpen}
        orphanAssets={orphanAssets}
        orphanAssetsBusy={orphanAssetsBusy}
        bind:orphanAssetsModalOpen={orphanAssetsModalOpen}
        aboutSys={aboutSys}
        aboutVersion={aboutVersion}
        updateResult={updateResult}
        updatingCheck={updatingCheck}
        updateStarting={updateStarting}
        javaBusy={javaBusy}
        javaProgress={javaProgress}
        unusedJava={unusedJava}
        versionsBusy={versionsBusy}
        folderPaths={folderPaths}
        folderRows={folderRows}
        fontsDir={fontsDir}
        sidebarOnlyPinned={sidebarOnlyPinned}
        sidebarScale={sidebarScale}
        sidebarWidth={sidebarWidth}
        headFor={headFor}
        fmtPlaytime={fmtPlaytime}
        {updateSetting}
        {loadStorageInfo}
        {clearCache}
        {clearPackLogs}
        {deletePackFromStorage}
        {openVersionsModal}
        {analyzeStorageVersions}
        {cleanupUnusedVersions}
        {openJavaModal}
        {analyzeUnusedJava}
        {cleanupUnusedJava}
        {openOrphanLibsModal}
        {analyzeOrphanLibs}
        {cleanupOrphanLibs}
        {openOrphanAssetsModal}
        {analyzeOrphanAssets}
        {cleanupOrphanAssets}
        {loadFolderPaths}
        {setFolder}
        {resetBaseDir}
        {loadFontsDir}
        {checkUpdates}
        {updateNow}
        {toggleMinecraftDrawer}
        {moveAccountTo}
        onaccountschanged={async () => { accounts = (await App.GetAccounts()) ?? [] }}
        {reloginAccount}
        {switchAccount}
        {removeAccount}
        {openRenameAccount}
        {toggleSidebarOnlyPinned}
        {setSidebarScale}
        {saveSidebarWidth}
        {importLanguageFile}
        {exportLauncherErrors}
        exportingErrors={exportingErrors}
        onaddaccount={() => { modalLogin = true; loginMsStep = 'idle' }}
        autoPacks={autoPacksInfo}
        onAutoPacksToggle={autoPacksToggle}
        onAutoPacksDelete={autoPacksDelete}
        onAutoPacksOpen={(id) => openPackPage(id)}
      />
      {/if}

      {#if currentPage === 'servers' && showServerWizard}
        <ServerWizard
          onclose={() => { showServerWizard = false }}
          onCreated={(id, launchNow) => { showServerWizard = false; openServerId = id; openServerAutoStart = launchNow }}
        />
      {:else if currentPage === 'servers' && openRemote}
        <!-- Сервер друга — та сама сторінка сервера, лише з дозволеними вкладками -->
        {#key `${openRemote.ownerId}:${openRemote.serverId}`}
        <ServerInstancePage
          serverId={openRemote.serverId}
          remoteOwnerId={openRemote.ownerId}
          onBack={() => { openRemote = null }}
        />
        {/key}
      {:else if currentPage === 'servers' && openServerId}
        <!-- key: перехід на інший сервер (з мережі проксі) — свіжа сторінка
             з вкладкою за замовчуванням для його ядра -->
        {#key openServerId}
        <ServerInstancePage
          serverId={openServerId}
          autoStart={openServerAutoStart}
          onBack={() => { openServerId = ''; openServerAutoStart = false }}
          onSearchOnline={openBrowserForServer}
          canShare={shaurmaFriendsVisible}
          onOpenServer={(id) => { openServerId = id; openServerAutoStart = false }}
        />
        {/key}
      {:else if currentPage === 'servers'}
        <ServersLibraryPage
          onOpenServer={(id, autoStart) => { openServerId = id; openServerAutoStart = !!autoStart }}
          onCreateServer={() => { showServerWizard = true }}
          onOpenRemote={(ownerId, serverId) => { openRemote = { ownerId, serverId } }}
        />
      {/if}
    </div>
  </div>
</div>

<!-- ═══ BLOCKED MODS DIALOG (імпорт CurseForge zip) ═══
     Автор(-и) цих модів заборонили завантаження зі сторонніх джерел —
     CurseForge не віддає пряме посилання. Як BlockedModsDialog у Prism:
     імпорт стоїть на паузі, поки користувач не обере рішення. -->
{#if blockedModsDialog}
  <BlockedModsDialog
    mods={blockedModsDialog.mods}
    allowModrinth={blockedModsDialog.allowModrinth}
    onresolve={respondBlocked}
  />
{/if}

<!-- ═══ PRE-LAUNCH MODS WARNING ═══
     Перед запуском збірки з модами перевіряємо дублікати (однаковий sha1,
     різні назви файлів) і відсутні залежності. Якщо є проблеми — модалка
     зі швидкими діями: видалити дублікати, відкрити «Моди» або все одно
     запустити. -->
{#if preLaunchIssues}
  <PreLaunchModsWarning
    issues={preLaunchIssues}
    onproceed={launchAnyway}
    ondeletedup={deleteDuplicatesAndLaunch}
    onmods={openModsToFix}
  />
{/if}

<!-- ═══ RAM WARNING MODAL (ТЗ) ═══
     Показується перед запуском, коли:
       • showRecommended — ефективна RAM нижча за рекомендовану збірки
         (рекомендовану пише Pack Manager у packs-index.json);
       • showLowRam — виділена RAM перевищує фізичну пам'ять системи
         (можливий краш).
     Дії: «Скасувати» / «Запустити», слайдер RAM (SetPackRAM — per-pack
     override), галочка «не показувати знову для цієї збірки»
     (DismissPackWarning — лише для цієї збірки, глобальний вимикач —
     у Налаштуваннях → «Попередження»). -->
{#if ramWarnings}
  {@const rw = ramWarnings}
  <RamWarningModal
    warnings={ramWarnings}
    packName={packIndex.find(p => p.id === rw.packId)?.name ?? rw.packId}
    bind:ramMb={ramWarningRamMb}
    bind:dismiss={ramWarningDismiss}
    onproceed={() => answerRamWarning(true)}
    oncancel={() => answerRamWarning(false)}
  />
{/if}

<!-- ═══ PACK DOCTOR: модалка перевірки здоров'я збірки перед запуском.
     Показується, якщо RunPackHealthCheck повернув overall !== 'ok'.
     Кнопка «Запустити все одно» доступна якщо немає незафіксованих errors. -->
{#if healthModal}
  {@const _hpid = healthPackId}
  <UiModal open={true} onclose={() => { healthModal = false }} icon="ti-stethoscope" color="var(--green)"
           title={t('health.modalTitle')} subtitle={healthPackName} width="520px" maxHeight="90vh" shake>
    <PackHealthCheck
      packId={_hpid}
      modal={true}
      onClose={() => { healthModal = false }}
      onLaunchAnyway={() => {
      healthModal = false
      const addr = pendingLaunchServerAddress || undefined
      pendingLaunchServerAddress = ''
      launchInstance(_hpid, false, true, addr)
    }}
    />
  </UiModal>
{/if}

<!-- ═══ CRASH MODAL (замість старого банера на «Моїх збірках»): показується
     поверх будь-якої сторінки, коли гра завершилась аварійно. Кнопка
     «Відкрити консоль» переходить на сторінку збірки, яка впала, і відкриває
     її вкладку «Консоль». -->
<CrashModal
  crashed={gameCrashed}
  dontShow={crashBannerHidden}
  onclose={crashBannerDismiss}
  onconsole={crashBannerOpenConsole}
  ondontshow={crashBannerDontShow}
/>

{#if repairDialog}
  <AutoRepairDialog
    packId={repairDialog.packId}
    packName={builds.find(b => b.build.id === repairDialog!.packId)?.build.name ?? ''}
    initial={repairDialog.report}
    onclose={closeRepairDialog}
    onconsole={() => { const id = repairDialog!.packId; repairDialog = null; consoleSignal++; openPackPage(id) }}
    onlogin={() => { repairDialog = null; window.dispatchEvent(new CustomEvent('open-login-modal')) }}
  />
{/if}

<!-- ═══ LOGIN MODAL ═══ -->
<!-- Акаунт Шаурми більше НЕ створюється тут окремим Google-входом: для
     Microsoft-акаунта він створюється автоматично після входу, а для
     офлайн-акаунта — на сторінці «Налаштування → Акаунт». -->
{#if modalLogin && !showWizard && !showIntro && skinLoginStep !== 'need-nick' && !signInNick}
  <LoginModal
    open={modalLogin}
    gate={loginGate}
    step={loginMsStep}
    onclose={closeLoginModal}
    onlogin={loginMsStart}
    oncancelms={loginMsCancel}
    onpirate={loginPirate}
    skingate={skinsEnabled}
    skinstep={googleShaurmaStep}
    onskinlogin={loginGoogleStart}
    onskincancel={loginGoogleCancel}
  />
{/if}

<!-- ═══ «ПРИДУМАЙ НІК» ПІСЛЯ ВХОДУ (новий акаунт Шаурми; Microsoft без ліцензії теж тут) ═══ -->
<NickChoiceModal
  open={!!signInNick}
  provider={signInNick?.provider ?? 'google'}
  noLicense={signInNick?.noLicense ?? ''}
  onconfirm={confirmSignInNick}
  oncancel={cancelSignInNick}
/>
<DeviceLimitModal
  open={!!deviceLimit}
  devices={deviceLimit?.devices ?? []}
  max={deviceLimit?.max ?? 10}
  onpick={pickDeviceToReplace}
  oncancel={cancelDeviceLimit}
/>
<SkinOfferModal open={!!skinOfferNick} nickname={skinOfferNick} onwardrobe={skinOfferOpenWardrobe} onlater={skinOfferLater} />

<!-- Посилання-запрошення в друзі, яке лаунчер побачив у буфері обміну -->
<AddFriendModal link={friendInvite} onclose={closeFriendInvite} onadded={closeFriendInvite} />

<!-- ═══ СТАРЕ ВІКНО НІКА (застарілий скін-флоу) ═══ -->
<SkinNickModal
  open={modalLogin && skinLoginStep === 'need-nick'}
  busy={skinNickBusy}
  email={skinLoginEmail}
  identity={skinNickIdentity}
  onconfirm={confirmNewSkinNickname}
  oncancel={skinNickCancel}
/>

<!-- ═══ АВТОМАТИЧНИЙ ПЕРЕЗАХІД У GOOGLE (скін-сесія завершилась) ═══ -->
<SkinReloginModal
  open={skinReloginOpen && !modalLogin && !showWizard && !showIntro && skinLoginStep === 'idle'}
  waiting={skinReloginWaiting}
  email={skinAuth.email}
  link={skinLinkMode}
  onlogin={skinReloginStart}
  oncancel={skinReloginCancel}
  onlater={skinReloginLater}
/>

<!-- ═══ ПЕРЕЙМЕНУВАННЯ ШАУРМА-АКАУНТА ═══ -->
<RenameAccountModal
  open={renameAccountOpen}
  currentNick={renameAccountTarget?.nick ?? ''}
  accountId={renameAccountTarget?.id ?? ''}
  onrenamed={onAccountRenamed}
  oncancel={() => { renameAccountOpen = false; renameAccountTarget = null }}
/>

<!-- ═══ МІНІМАЛЬНЕ ВІКНО ПРОГРЕСУ ЗАПУСКУ (запуск з ярлика збірки) ═══ -->
{#if launchOverlay}
  <LaunchProgressOverlay
    packName={launchOverlay.name}
    packIcon={launchOverlay.iconUrl}
    presetIcon={launchOverlay.presetIcon}
    packColor={launchOverlay.color}
    progress={launchProgress[launchOverlay.packId] ?? null}
    onCancel={cancelOverlayLaunch}
  />
{/if}

<!-- ═══ ДІАЛОГ ЯРЛИКА ЗБІРКИ (назва + місце, як Prism) ═══ -->
{#if shortcutDialogId}
  <ShortcutDialog
    packId={shortcutDialogId}
    packName={builds.find(b => b.build.id === shortcutDialogId)?.build.name ?? ''}
    accounts={accounts}
    open={true}
    onClose={() => shortcutDialogId = null}
    onCreate={() => {
      shortcutByPack = { ...shortcutByPack, [shortcutDialogId!]: true }
      toast(t('pack.shortcutCreated'), 'success')
    }}
  />
{/if}

<!-- ═══ МІГРАЦІЯ ЗБІРОК З ІНШИХ ЛАУНЧЕРІВ (Prism / Modrinth App) ═══ -->
{#if migrationOpen && !showWizard && !showIntro && !modalLogin}
  <LauncherMigrationModal
    open={migrationOpen}
    dismissOnClose={true}
    initialLaunchers={migrationInitial}
    onClose={() => migrationOpen = false}
  />
{/if}

<!-- ═══ СТАН БАЗИ ДАНИХ (відновлення з бекупу / навмисне скидання) ═══ -->
{#if dbHealthInfo}
  {@const dbh = dbHealthInfo}
  <UiModal open={true} onclose={() => dbHealthInfo = null} size="sm"
           icon={dbh.restoredFromBackup ? 'ti-shield-check' : dbh.erasedIntentionally ? 'ti-eraser' : 'ti-alert-triangle'}
           color={dbh.restoredFromBackup ? 'var(--green)' : dbh.erasedIntentionally ? 'var(--orange)' : 'var(--red)'}
           title={dbh.restoredFromBackup ? t('migrate.dbRestoredTitle')
             : dbh.erasedIntentionally ? t('migrate.dbErasedTitle')
             : t('migrate.dbMemoryTitle')}>
    <div class="dbh-desc">
      {dbh.restoredFromBackup ? t('migrate.dbRestoredDesc', { at: dbh.backupAt ?? '' })
        : dbh.erasedIntentionally ? t('migrate.dbErasedDesc', { at: dbh.erasedAt ?? '' })
        : t('migrate.dbMemoryDesc')}
    </div>
    {#snippet footer()}
      <button class="btn btn-primary" onclick={() => dbHealthInfo = null}><i class="ti ti-check"></i> {t('migrate.dbOk')}</button>
    {/snippet}
  </UiModal>
{/if}

<!-- ═══ ГЛОБАЛЬНИЙ ПОШУК (Ctrl+K) ═══ -->
<GlobalSearchModal
  open={searchOpen}
  bind:query={searchQuery}
  packs={searchPacks}
  packPicker={packPicker}
  presetResults={searchPresetResults}
  presetHeads={searchPresetHeads}
  presetCapes={searchPresetCapes}
  browser={searchBrowser}
  browserLoading={searchBrowserLoading}
  settingsResults={searchSettings}
  grpTitleKeys={grpTitleKeys}
  onclose={closeGlobalSearch}
  ongopack={searchGoPack}
  ongopreset={searchGoPreset}
  ongosettings={(sd) => searchGoSettings(sd.group, sd.key)}
  ongobrowser={searchGoBrowser}
/>

<!-- ═══ TOASTS + CONFIRM (замість alert/confirm) ═══ -->
  {#if ctxMenu && ctxPack}
    <!-- Контекстне меню збірки: ЛИШЕ те, чого немає на самій картці/сторінці.
         Відкрити сторінку, грати/зупинити, консоль, полагодити — прибрані
         (дублюють клік по картці і tooltips). Лишаються: тека збірки,
         закріпити, завантажити/оновити, експорт, оновити з джерела,
         дублювати, видалити. -->
    <div class="ctx-menu" style="left:{ctxMenu.x}px;top:{ctxMenu.y}px" oncontextmenu={(e) => e.preventDefault()}>
      <div class="ctx-item" class:pin-on={isPinned(ctxPack.build.id)} onclick={() => ctxAction('pin', ctxPack.build.id)}>
        <i class="ti ti-pin"></i> {isPinned(ctxPack.build.id) ? t('packs.unpin') : t('packs.pin')}
      </div>
      {#if ctxPack.status === 'not-installed' || ctxPack.status === 'needs-update'}
        <div class="ctx-item" onclick={() => ctxAction('download', ctxPack.build.id)}>
          <i class="ti ti-download"></i> {ctxPack.status === 'needs-update' ? t('pack.update') : t('pack.download')}
        </div>
      {/if}
      {#if ctxPack.installed}
        <!-- Відкрити теку збірки у Провіднику: для всіх встановлених
             збірок (файли на диску є). Користувач просив — без цього
             пункту доводилось шукати теку вручну. -->
        <div class="ctx-sep"></div>
        <div class="ctx-item" onclick={() => ctxAction('folder', ctxPack.build.id)}>
          <i class="ti ti-folder-open"></i> {t('pack.openFolder')}
        </div>
        {#if ctxPack.build.kind === 'custom'}
          <!-- Експорт офіційних Shaurma-збірок заборонено на бекенді
               (ExportPack повертає errExportForbidden) — тому пункт
               меню взагалі не показуємо для не-custom збірок, щоб не
               давати клікати в кнопку, яка все одно завершиться помилкою. -->
          <div class="ctx-sep"></div>
          <div class="ctx-item" onclick={() => ctxAction('export', ctxPack.build.id)}>
            <i class="ti ti-package-export"></i> {t('pack.export')}
          </div>
        {/if}
        {#if ctxPack.build.source === 'modrinth' || ctxPack.build.source === 'curseforge'}
          <div class="ctx-sep"></div>
          <div class="ctx-item" onclick={() => ctxAction('updateSource', ctxPack.build.id)}>
            <i class="ti ti-refresh"></i> {t('browser.updateFromSource')}
          </div>
        {/if}
        <!-- Дублювання доступне лише для КАСТОМНИХ збірок: офіційні Shaurma-збірки
             керуються лаунчером і синкуються з каталогу, тож копія все одно була б
             перезаписана при оновленні (бекенд DuplicatePack теж відхиляє Shaurma). -->
        {#if ctxPack.build.kind === 'custom'}
          <div class="ctx-sep"></div>
          <div class="ctx-item" onclick={() => ctxAction('duplicate', ctxPack.build.id)}>
            <i class="ti ti-copy"></i> {t('pack.duplicate')}
          </div>
        {/if}
        <!-- Ярлик збірки (як Modrinth App / Prism): подвійний клік запускає
             лаунчер і одразу цю збірку. «Створити ярлик…» відкриває діалог
             з вибором назви і місця (як CreateShortcutDialog у Prism); якщо
             ярлик уже є — окремий пункт видаляє його. -->
        <div class="ctx-sep"></div>
        <div class="ctx-item" onclick={() => ctxAction('shortcut', ctxPack.build.id)}>
          <i class="ti ti-external-link"></i>
          {shortcutByPack[ctxPack.build.id] ? t('pack.shortcutEdit') : t('pack.shortcutCreate')}
        </div>
        {#if shortcutByPack[ctxPack.build.id]}
          <div class="ctx-item" onclick={() => ctxAction('shortcutRemove', ctxPack.build.id)}>
            <i class="ti ti-trash"></i> {t('pack.shortcutRemove')}
          </div>
        {/if}
      {/if}
      {#if ctxPack.build.kind === 'custom'}
        <!-- Створити папку: одразу з цією збіркою всередині й відкритим
             редактором назви/кольору. -->
        <div class="ctx-sep"></div>
        <div class="ctx-item" onclick={() => { const pid = ctxPack.build.id; ctxMenu = null; folderEditor = { id: '', name: t('packs.folderNewName'), color: '#ff8a00', icon: 'ti-folder', mode: 'create', selected: new Set([pid]) } }}>
          <i class="ti ti-folder-plus"></i> {t('packs.folderCreate')}
        </div>
        <!-- Видалення доступне і для кастомних збірок БЕЗ файлів (щойно
             створена «не завантажена» картка з браузера контенту, або
             ручна незаповнена збірка) — раніше пункт «Видалити» ховався
             усередині {#if ctxPack.installed}, тож такі збірки не можна
             було прибрати інакше, ніж вручну з файлової системи. -->
        <div class="ctx-sep"></div>
        <div class="ctx-item danger" onclick={() => ctxAction('delete', ctxPack.build.id)}>
          <i class="ti ti-trash"></i> {t('pack.delete')}
        </div>
      {/if}
    </div>
  {/if}

  <!-- Модалка експорту, відкрита з контекстного меню сайдбару. -->
  {#if ctxExportPackId}
    {@const bv = builds.find(b => b.build.id === ctxExportPackId)}
    <ExportPackModal packId={ctxExportPackId} packName={bv?.build.name ?? ''} onClose={() => ctxExportPackId = null} />
  {/if}

  <!-- Модалка дублювання збірки (ПКМ по картці → «Дублювати»). -->
  {#if ctxDuplicatePackId}
    {@const dup = builds.find(b => b.build.id === ctxDuplicatePackId)}
    <DuplicatePackModal
      packId={ctxDuplicatePackId}
      packName={dup?.build.name ?? ''}
      packIconUrl={dup?.build.iconUrl ?? ''}
      packIcon={dup?.build.icon ?? ''}
      packColor={dup?.build.color ?? '#ff8a00'}
      onClose={() => ctxDuplicatePackId = null}
      onDone={(newId) => {
        // Після створення копії — переходимо на її сторінку (копія вже
        // з'явилась у списку через builds:changed → refreshBuilds).
        loadData().then(() => openPackPage(newId))
      }}
    />
  {/if}

  <!-- ПКМ-меню ПАПКИ (sidebar/плитка «Мої збірки») -->	  {#if folderCtxMenu}
	    <!-- Null-safe: при закритті меню (напр. клік по «Експортувати
	         папку» — folderCtxMenu стає null в одному кадрі з відкриттям
	         модалки експорту) Svelte може оцінити const ще під час
	         teardown-рендеру, і folderCtxMenu.id на null падав би
	         TypeError «Cannot read properties of null (reading 'id')». -->
	    {@const fv = folderCtxMenu ? folderViews.find(v => v.folder.id === folderCtxMenu?.id) : undefined}
    {#if fv}
      <div class="ctx-menu" style="left:{folderCtxMenu.x}px;top:{folderCtxMenu.y}px" oncontextmenu={(e) => e.preventDefault()}>
        <div class="ctx-item" onclick={() => folderCtxAction('open', fv.folder.id)}>
          <i class="ti ti-folder-open"></i> {t('packs.folderOpen')}
        </div>
        {#if !fv.folder.serverManaged}
          <div class="ctx-item" onclick={() => folderCtxAction('rename', fv.folder.id)}>
            <i class="ti ti-edit"></i> {t('packs.folderRename')}
          </div>
          <div class="ctx-sep"></div>
          <div class="ctx-item" onclick={() => { folderCtxMenu = null; openFolderBundleExport(fv.folder.id) }}>
            <i class="ti ti-archive"></i> {t('packs.folderExportBundle')}
          </div>
          <div class="ctx-sep"></div>
          <div class="ctx-item danger" onclick={() => folderCtxAction('delete', fv.folder.id)}>
            <i class="ti ti-trash"></i> {t('packs.folderDelete')}
          </div>
        {:else}
          <!-- Серверна папка (Pack Manager, packs-index.json folderId):
               членство/назва/колір керуються ВИКЛЮЧНО звідти й
               перезаписуються з сервера на кожен GetBuilds — локальне
               перейменування/видалення тут нічого не змінило б насправді
               (renameFolder/deleteFolder шукають у packFolders, серверної
               папки там нема) і лише сплутало б користувача. -->
          <div class="ctx-hint">{t('packs.folderServerManaged')}</div>
        {/if}
      </div>	    {/if}
	  {/if}

	  {#if folderBundleExport}
	    {@const bse = folderBundleExport}
    <UiModal open={true} onclose={() => folderBundleExport = null} locked={bse.busy} zIndex="2100000" size="sm"
             icon="ti-archive" title={t('packs.bundleExportTitle')} subtitle={t('packs.bundleExportSub')}>
      <div class="bse-body">
        {#if bse.busy}
          <div class="fe-field">
            <div class="rl-sub" style="display:flex;align-items:center;gap:8px"><i class="ti ti-loader spin"></i> {t('packs.bundleExporting')}</div>
            {#if bse.total > 0}
              <div class="bundle-export-progress"><div class="bundle-export-fill" style="width:{Math.round((bse.done / bse.total) * 100)}%"></div></div>
              <div class="rl-sub mono" style="font-size:11px">{bse.done}/{bse.total}</div>
            {/if}
          </div>
        {:else}
          <div class="fe-field">
            <div class="fe-label">{t('packs.bundleExportName')}</div>
            <input class="st-input grow" bind:value={bse.name} placeholder={t('packs.bundleExportNamePlaceholder')} />
          </div>
          <div class="fe-field">
            <div class="fe-label">{t('packs.bundleExportDir')}</div>
            <div class="folder-row">
              <input class="st-input grow" bind:value={bse.outDir} placeholder={t('packs.bundleExportDirPlaceholder')} />
              <button class="btn btn-ghost" onclick={pickBundleExportDir} title={t('browse')}><i class="ti ti-folder"></i></button>
            </div>
          </div>
        {/if}
      </div>
      {#snippet footer()}
        <button class="btn btn-ghost" disabled={bse.busy} onclick={() => folderBundleExport = null}>{t('cancel')}</button>
        <button class="btn btn-primary" disabled={bse.busy} onclick={startFolderBundleExport}>
          <i class="ti {bse.busy ? 'ti-loader spin' : 'ti-archive'}"></i> {t('packs.bundleExportStart')}
        </button>
      {/snippet}
    </UiModal>
	  {/if}

  {#if folderEditor}
    {@const fe = folderEditor}
    <!-- Кандидати: збірки, яких НЕМАЄ в інших папках (щоб одну збірку не
         можна було розкласти в дві папки). У режимі редагування поточна
         папка виключається з фільтра — її власні збірки вже розкладені. -->
    {@const feInFolders = new Set<string>(
      folderViews
        .filter(fv => fe.mode !== 'edit' || fv.folder.id !== fe.id)
        .flatMap(fv => fv.packs.map(p => p.build.id))
    )}
    {@const feCandidates = orderedBuildViews.filter(bv => bv.build.kind === 'custom' && !feInFolders.has(bv.build.id))}
    {@const feCandidatesRowById = new Map<string, PackRow>(
      feCandidates.map(bv => [bv.build.id, {
        name: bv.build.name,
        color: bv.build.color,
        iconUrl: bv.build.iconUrl,
        icon: bv.build.icon,
        loader: bv.build.loaderType,
        mcVersion: bv.build.mcVersion,
      }])
    )}
    <FolderEditorModal
      mode={fe.mode}
      bind:name={fe.name}
      bind:color={fe.color}
      bind:icon={fe.icon}
      count={fe.selected.size}
      countLabel={t('packs.folderCount')}
      pickerLabel={t('packs.folderAddPacks')}
      showPicker={fe.mode === 'create'}
      onsave={saveFolderEditor}
      oncancel={() => (folderEditor = null)}
    >
      {#if feCandidates.length === 0}
        <div class="fe-pack-empty">{t('packs.folderNoPacks')}</div>
      {:else}
        <PackPicker
          items={feCandidates.map(bv => ({ kind: 'pack' as const, id: bv.build.id }))}
          rowById={feCandidatesRowById}
          multi
          selectedIds={fe.selected}
          ontoggle={(pid) => {
            const next = new Set(fe.selected)
            if (next.has(pid)) next.delete(pid); else next.add(pid)
            folderEditor = { ...fe, selected: next }
          }}
          class="fe-pp"
        />
      {/if}
    </FolderEditorModal>
  {/if}

  <!-- МОДАЛЬНЕ ВІКНО ПАПКИ: клік на плитку папки в «Мої збірки» відкриває
       його зі збірками всередині (fm-grid). Перетягування збірки за межі
       модалки витягує її в головний список; дроп на іншу плитку папки —
       переносить в іншу папку (pointerDragEnd обробляє .folder-modal). -->
  {#if folderModalId}
    {@const fmv = folderViews.find(f => f.folder.id === folderModalId)}
    {#if fmv}
      {@const mf = fmv.folder}
      <FolderViewModal
        name={mf.name}
        color={mf.color || '#ff8a00'}
        icon={mf.icon}
        count={fmv.packs.length}
        countLabel={t('packs.folderCount')}
        managed={!!mf.serverManaged}
        managedHint={t('packs.folderServerManaged')}
        footHint={mf.serverManaged ? '' : t('packs.folderDragHint')}
        emptyText={t('packs.folderEmpty')}
        actions={mf.serverManaged ? [] : [
          { icon: 'ti-archive', label: t('packs.folderExportBundle'), onclick: () => openFolderBundleExport(mf.id) },
          { icon: 'ti-edit', label: t('packs.folderRename'), onclick: () => openFolderEditor(mf.id) },
          { icon: 'ti-trash', label: t('packs.folderDelete'), danger: true, onclick: () => { folderModalId = null; deleteFolder(mf.id); toast(t('packs.folderDeleted'), 'success') } },
        ]}
        onclose={() => (folderModalId = null)}
      >
                <div class="fm-grid">
                  {#each fmv.packs as bpk, fi (bpk.build.id)}
                    {@render packTile(bpk, fi, true)}
                  {/each}
                </div>
      </FolderViewModal>
    {/if}
  {/if}

  <!-- Діалог «Оновити з джерела» (інтернет-збірка, 3 крапки → Оновити) -->
  {#if updateSourceDialog}
    {@const d = updateSourceDialog}
    <UiModal open={true} onclose={() => updateSourceDialog = null} icon="ti-refresh" width="520px" maxHeight="80vh"
             title={t('browser.update.title', { name: d.name })}>
      {#if d.loading}
        <div class="upsrc-loading"><i class="ti ti-loader spin"></i> {t('browser.update.installing')}</div>
      {:else if (d.versions ?? []).length === 0}
        <div class="upsrc-empty"><i class="ti ti-versions"></i><div>{t('browser.update.noVersions')}</div></div>
      {:else}
        <div class="upsrc-list">
          {#each d.versions ?? [] as v (v.id)}
            {@const sel = v.id === d.selected}
            <div class="upsrc-row" class:sel onclick={() => d.selected = v.id}>
              <div class="upsrc-info">
                <div class="upsrc-name">
                  <b>{v.name || v.versionNumber}</b>
                  {#if v.versionType === 'release'}<span class="chip g">{t('browser.project.recommended')}</span>{/if}
                </div>
                <div class="upsrc-meta">
                  <span><i class="ti ti-cube"></i> {(v.gameVersions ?? []).join(', ')}</span>
                  {#if v.loaders?.length}<span><i class="ti ti-plug"></i> {(v.loaders ?? []).join(', ')}</span>{/if}
                  <span class="upsrc-date">{v.datePublished ? new Date(v.datePublished).toLocaleDateString() : ''}</span>
                </div>
              </div>
              <div class="upsrc-radio" class:on={sel}><i class="ti ti-check"></i></div>
            </div>
          {/each}
        </div>
      {/if}
      {#snippet footer()}
        <button class="btn btn-ghost" onclick={() => updateSourceDialog = null}>{t('cancel')}</button>
        {#if !d.loading && (d.versions ?? []).length > 0}
          <button
            class="btn btn-primary"
            disabled={!d.selected || d.running}
            onclick={runUpdateFromSource}
          >
            {#if d.running}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-refresh"></i>{/if}
            {t('browser.update.install', { v: d.versions.find(v => v.id === d.selected)?.versionNumber || '' })}
          </button>
        {/if}
      {/snippet}
    </UiModal>
  {/if}

  <!-- «Реальна картка» за курсором під час перетягування збірки/папки -->
  {#if dragGhostPos && dragGhostInfo}
    <div
      class="pack-drag-ghost"
      style="left:{dragGhostPos.x + 12}px;top:{dragGhostPos.y + 8}px;--pcol:{dragGhostInfo.color}"
      aria-hidden="true"
    >
      <div class="pdg-ic">
        {#if dragGhostInfo.icon}
          <img src={dragGhostInfo.icon} alt="" />
        {:else}
          <i class="ti {dragGhostInfo.ti}"></i>
        {/if}
      </div>
      <div class="pdg-name">{dragGhostInfo.name}</div>
    </div>
  {/if}

  <Toasts />
  <ConfirmModal />
  <!-- Офлайн-попередження запуску (launch:progress.warning): модалка з
       одною кнопкою «Зрозуміло». Не плутати з помилкою — гра СТАРТУЄ
       в офлайн-режимі, це лише пояснення користувачу. -->
  {#if launchWarning}
    <UiModal open={true} onclose={() => launchWarning = null} icon="ti-wifi-off" title={t('offline.launchTitle')}>
      <div style="font-size:13px;color:var(--text-mute);line-height:1.6;white-space:pre-line">{launchWarning}</div>
      {#snippet footer()}
        <button class="btn btn-primary" onclick={() => launchWarning = null}>{t('offline.gotIt')}</button>
      {/snippet}
    </UiModal>
  {/if}
  <NewScreenshotWidget onOpenGallery={openGalleryFor} />

<!-- Скоуп-стилі, що лишились тільки для розмітки App (інші перенесені в
     style.css при рефакторингу SettingsPage — там вони глобальні й діють
     і на компоненти). Модалки App — на спільному UiModal. -->
<style>
  .dbh-desc { font-size: 12.5px; line-height: 1.5; color: var(--text-mute); }
  .bse-body { display: flex; flex-direction: column; gap: 14px; }
</style>
