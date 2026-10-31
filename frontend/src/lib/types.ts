export interface Instance {
  id: string
  name: string
  loader: string
  mcVersion: string
  loaderVersion: string
  icon: string
  color: string
  type: string
  status: string
  downloaded: number
  totalBytes: number
  isShaurma: boolean
  gameDir: string
}

export interface Account {
  id: string
  username: string
  type: string
  uuid: string
  isLicensed: boolean
  // Система скінів ліцензійного акаунта: "mojang" (дефолт) | "shaurma".
  // isShaurmaSkinAccount — true, якщо цей (неліцензійний) акаунт зараз
  // прив'язаний до активної Google-сесії скін-системи Shaurma (нік
  // акаунта збігається з ніком скін-акаунта). У списку акаунтів такий
  // акаунт показується як «Шаурма-акаунт», а не «Не ліцензійний».
  // Base-збірка ніколи не встановлює це поле (завжди false/undefined).
  isShaurmaSkinAccount?: boolean
  // licensedNickMine — нік цього офлайн-акаунта зайнятий ЛІЦЕНЗІЙНИМ
  // акаунтом із цього ж лаунчера. Пріоритет ліцензійного: у грі
  // показується скін з Mojang, а редагування скіна/пресету під цим ніком
  // відхиляється (бекенд + 409 від воркера). UI показує бейдж з поясненням.
  licensedNickMine?: boolean
  // Xbox Live соціальні токени (XblToken/XblUHS) — заповнені лише для
  // ліцензійних Microsoft-акаунтів, увійдених після додавання розділу
  // «Друзі». Їх наявність вмикає кнопку друзів на віджеті акаунта.
  xblToken?: string
  xblUHS?: string
  gamertag?: string
  xboxUserID?: string
}

export interface Settings {
  language: string
  accent: string
  accentCustom: string
  // accentCustom2/accentCustomMode — другий колір і режим (flat/gradient)
  // для акценту "custom". Опційні: старі збережені стани без цих полів
  // трактуються як flat (applyAccent за замовчуванням дає flat-поведінку).
  accentCustom2?: string
  accentCustomMode?: string
  theme: string
  animationsEnabled: boolean
  // Анімувати іконки збірок у sidebar завжди (не лише для вибраної збірки).
  sidebarAnimAlways?: boolean
  pinnedServersCompact?: boolean
  // Банер сторінки збірки на всю ширину (повноширинний геро-блок).
  packBannerFull?: boolean
  font: string
  fontPath: string
  // Sidebar: ширина, масштаб елементів (повзунок), «лише закріплені».
  sidebarWidth: number
  sidebarScale: number
  sidebarOnlyPinned: boolean
  // Кастомна тема: колір фону, карток, тексту.
  themeCustomBg: string
  themeCustomCard: string
  themeCustomText: string
  autoUpdate: boolean
  updateChannel: string
  // Час останньої успішної перевірки оновлень (RFC3339; '' = ще не було).
  lastUpdateCheck?: string
  showConsole: boolean
  saveLogs: boolean
  instanceDir: string
  javaPath: string
  maxRAM: number
  javaArgs: string
  activeAccountId: string

  // Консоль
  consoleMaxLines: number
  consoleInfoColor: string
  consoleWarnColor: string
  consoleErrorColor: string
  consoleWrap: boolean
  consoleFontSize: number
  consoleFontFamily: string
  // Режим шрифту консолі: 'all' — один шрифт для всіх типів;
  // 'perType' — окремий шрифт для info/warn/error (поля console*Font нижче).
  consoleFontMode: string
  consoleInfoFont: string
  consoleWarnFont: string
  consoleErrorFont: string
  // Шлях до власного TTF/OTF шрифту консолі (коли обрано 'custom').
  consoleFontPath: string
  consoleAI: boolean
  // Власний Gemini API key (Налаштування → Консоль). Порожнє = вбудований дефолт.
  consoleAIKey: string
  showConsoleOnLaunch: boolean
  showConsoleOnCrash: boolean
  showConsoleOnClose: boolean
  // Вимкнути авто-діагностику після краху (діалог «Авто-починка»).
  disableAutoRepair: boolean

  // Завдання
  maxConcurrentDownloads: number
  maxRetries: number
  httpTimeoutSec: number

  // Команди
  preLaunchCommand: string
  wrapperCommand: string
  postExitCommand: string
  envVars: string

  // Вікно гри
  fullscreen: boolean
  windowWidth: number
  windowHeight: number
  // Поведінка вікна лаунчера (backend: internal/winbehavior).
  startupMode: 'never' | 'window' | 'background'
  closeAction: 'quit' | 'tray'
  minimizeAction: 'taskbar' | 'tray'
  gameLaunchAction: 'keep' | 'minimize' | 'tray'
  gameLaunchDelaySec: number
  gameExitAction: 'restore' | 'stay' | 'quit'
  restoreOnCrash: boolean
  // Показувати підтвердження перед зупинкою запущеної збірки (кнопка
  // «Зупинити»). Вимкнено = зупиняти одразу, без діалогу.
  confirmOnStop: boolean

  // Попередження перед запуском
  // Глобальний вимикач усіх RAM-попереджень (і про рекомендовану RAM
  // збірки, і про нестачу фізичної оперативки). Per-pack «не показувати
  // знову» живе в InstanceConfig (recommendedRamDismissed/lowRamDismissed).
  showRamWarnings: boolean
  // Розумний аналізатор запуску СЕРВЕРА (порт/RAM/Java/EULA/диск/краш).
  // Головний вимикач: вимкнено = показується ЛИШЕ порт (він не ігнорується
  // ніколи — на зайнятому порту сервер фізично не стартує).
  showServerWarnings: boolean
  // Окремі «більше не показувати» для точок, які можна ігнорувати.
  serverWarnIgnoreRam: boolean
  serverWarnIgnoreJava: boolean
  serverWarnIgnoreDisk: boolean
  serverWarnIgnoreCrash: boolean

  // Ігровий час
  showGameTime: boolean
  recordGameTime: boolean
  showTotalGameTime: boolean
  gameTimeInHours: boolean
  // Показувати на сторінці збірки, скільки гра вже запущена на поточному
  // акаунті (живий лічильник сесії).
  showSessionTime: boolean
  // Показувати на сторінці збірки загальний час, проведений у цій збірці.
  showGameTimeOnPage: boolean
  // Тимчасові моди («становлення на строк»): true = видаляти файл
  // повністю за замовчуванням, коли настає дата; false = лише вимикати
  // (безпечніший дефолт — мод можна повернути вручну).
  tempModsDeleteByDefault: boolean

  // Discord Rich Presence
  // Показувати у профілі Discord, що гравець зараз запущений у збірці
  // (або в кількох одночасно — «N збірок» + список). Увімкнено за
  // замовчуванням. Тексти — шаблони з плейсхолдерами {pack} / {list} /
  // {count} / {loader} / {mc}.
  discordRpcEnabled: boolean
  discordRPCDetails: string
  discordRPCState: string

  // Авто-видалення версій Minecraft, які після видалення збірки більше
  // не використовуються (versions + бібліотеки й асети).
  cleanupAfterPackDelete: boolean

  // Сповіщення про онлайн друзів Xbox (перемикач у шухляді «Друзі»).
  xboxOnlineNotify?: boolean

  // Авто-відправка звітів про помилки (перемикач у «Загальні»).

  // Вимикачі систем, які можна прибрати повністю (перемикачі у «Загальні»):
  // disableSkins — лаунчер не підключає java-агент authlib-injector і не
  // звертається до Yggdrasil-API (скіни офлайн-акаунтів не працюють);
  // disableP2P — «LAN через інтернет» зникає з інтерфейсу і фоновий слухач
  // на 127.0.0.1:45678 не піднімається.
  // Серіалізація з Go відбувається за json-тегами (model.Settings), тому тут
  // імена полів мусять збігатися з ними, а не з порядком оголошення.
  disableSkins?: boolean
  // disableMojangSkinFallback — вимкнути другий потік локального skin-injector
  // (скін із Mojang за ніком, коли в Shaurma його немає).
  disableMojangSkinFallback?: boolean
  disableP2P?: boolean
  // disableCloudPlaytime — не копіювати загальний ігровий час у хмару (акаунт Шаурми).
  disableCloudPlaytime?: boolean

  // ── Сповіщення (віджет у трей; розділ «Сповіщення» в налаштуваннях) ──
  // notifyEnabled — головний вимикач тостів; notifyDnd — «Не турбувати»
  // (швидкий режим тиші, не змінює решту налаштувань); notifyDurationSec —
  // скільки секунд картка лишається на екрані (3..60, див. config.normalize).
  // Опційні: старий збережений стан без цих полів → бекенд підставляє
  // дефолти (loadSettings + normalize в internal/config).
  notifyEnabled?: boolean
  notifyDurationSec?: number
  notifyDnd?: boolean
}

export interface FolderPaths {
  baseDir: string
  installations: string
  java: string
  cache: string
  logs: string
  config: string
  installationsCustom: string
  javaCustom: string
  cacheCustom: string
  logsCustom: string
  defaultBaseDir: string
}

// FontFile — один шрифт із теки шрифтів лаунчера (<dataDir>/fonts).
// Фронтенд додає їх у випадаючі списки шрифтів (інтерфейс і консоль)
// зі значенням "file:<path>" — theme.svelte.ts розуміє такий префікс
// і завантажує файл як @font-face з ім'ям з назви файлу.
export interface FontFile {
  name: string
  path: string
}

export interface StorageEntry {
  name: string
  path: string
  bytes: number
}

export interface StorageInfo {
  entries: StorageEntry[]
  totalBytes: number
  freeDiskMB: number
}

export interface StoragePackEntry {
  id: string
  name: string
  bytes: number
  iconUrl: string
  // Пресет-іконка збірки (Tabler); iconUrl (PNG) має пріоритет.
  icon: string
  color: string
}

// Один встановлений мажор Java в теці лаунчера (рядок модалки аналізу):
// номер, розмір на диску і чи потрібен він хоч одній встановленій збірці.
export interface JavaMajorInfo {
  major: number
  bytes: number
  used: boolean
}

// Результат аналізу невикористовуваної Java (вкладка «Пам'ять і кеш»):
// які мажори встановлені в теці лаунчера (з розмірами) і які з них не
// потрібні жодній встановленій збірці (кандидати на видалення).
export interface UnusedJavaInfo {
  installed: JavaMajorInfo[]
  unused: number[]
  totalBytes: number
}

export interface StorageVersionEntry {
  id: string
  type: string
  bytes: number
  used: boolean
  inherit?: string
}

export interface StorageVersionsInfo {
  installed: StorageVersionEntry[]
  unused: string[]
  totalBytes: number
  autoDelete: boolean
  librariesMB: number
  assetsMB: number
}

export interface StorageLibrariesInfo {
  usedPaths: number
  orphanFiles: number
  orphanBytes: number
  usedBytes: number
  scanDone: boolean
  error?: string
  removedFiles: number
  removedBytes: number
}

export interface StorageAssetsInfo {
  usedObjects: number
  orphanObjects: number
  orphanBytes: number
  removed: number
  removedBytes: number
  scanDone: boolean
  error?: string
}

export interface StorageSuggestion {
  id: string
  name: string
  iconUrl: string
  // Пресет-іконка збірки (Tabler); iconUrl (PNG) має пріоритет.
  icon: string
  color: string
  bytes: number
  playSeconds: number
  recommended: boolean
}

export interface SystemInfo {
  cpuCount: number
  totalRAMMB: number
  freeDiskMB: number
  // ОС/архітектура для вкладки «Про лаунчер» (з App.GetSystemInfo).
  os?: string
  arch?: string
  platform?: string
}

export interface JavaStatus {
  installed: boolean
  dir: string
  exePath: string
  version: string
}

export interface SettingOption {
  value: string
  label: string
  // Для type 'choice': i18n-ключ пояснення та клас tabler-іконки варіанта.
  desc?: string
  icon?: string
}

// Умова показу налаштування (SettingDef.showIf): settings[key] ∈ in
// (або ∉, якщо not). Усі умови рядка мають виконуватись.
export interface SettingCond {
  key: string
  in: string[]
  not?: boolean
}

// Налаштування гри «за замовчуванням» (options.txt) — те, що лаунчер
// застосовує до нових збірок. Enabled=false = функцію вимкнено.
export interface MinecraftDefaults {
  enabled: boolean
  language: string
  guiScale: number // 0=auto, 1-N
  soundLevels: Record<string, number>
  keyBindings: Record<string, string>
  fullscreen: boolean
  enableVsync: boolean
  maxFps: number
  autoJump: boolean
  mouseSensitivity: number // 0.0-2.0 (0.5 = 100%)
  invertYMouse: boolean
  fov: number // normalized 0.0=(70°) .. 1.0=(110°)
  darkMojangStudiosBackground: boolean
}

// Знімок options.txt збірки для розділу «Гра» на сторінці збірки.
export interface PackOptionsView {
  exists: boolean
  options: Record<string, string>
  path: string
}

// Справжній (з lang-файлу мода) підпис бінда клавіші, повернутий
// App.GetKeyBindLabels — див. backend/internal/mods/manager.go.
export interface KeyBindLabel {
  bind: string // "key.iris.reload"
  modId: string // "iris"
  modName: string // людяна назва мода (з метаданих jar)
  label: string // перекладений підпис самого біндингу
  category?: string // перекладена назва категорії біндів мода, якщо є
}

export interface SettingDef {
  key: string
  type: string // bool | string | int | color | select | choice | accent | language | ram
  group: string
  label: string
  desc: string
  default: any
  options?: SettingOption[]
  min?: number
  max?: number
  step?: number
  placeholder?: string
  action?: string // browse | detectJava
  multiline?: boolean
  showIf?: SettingCond[]
}

export interface SetupProgress {
  step: number
  instanceDir: string
  javaPath: string
  language: string
  accent: string
  maxRAM: number
  javaArgs: string
}

export interface DownloadProgress {
  instanceId: string
  fileName: string
  downloaded: number
  total: number
  speed: number
  percent: number
}

export interface BrowserEntry {
  id: string
  name: string
  type: string
  version: string
  loader: string
  mcVersion: string
  description: string
  downloads: number
  followers: number
  likes: number
  iconUrl: string
  // bannerUrl — широке зображення проєкту для режиму «Сітка» (може бути порожнім)
  bannerUrl?: string
  source: string
  // projectId — числовий/hex ID проєкту (для встановлення), author —
  // автор, categories/loaders — теги для бейджів картки.
  projectId: string
  author: string
  categories: string[]
  loaders: string[]
  dateCreated: string
  dateModified: string
  // compatibilityUnconfirmed — див. коментар на BrowserEntry (Go): або
  // перевірка сумісності ще ТРИВАЄ у фоні (дотягується подією
  // "browser:compat"), або завершилась, але не змогла підтвердити елемент
  // через мережеву помилку (fail-open — не викинуто, але й не гарантовано
  // сумісний).
  compatibilityUnconfirmed?: boolean
  // compatibilityChecking — перевірка сумісності ще йде у фоні. Це НЕ
  // помилка: кнопка «Встановити» лишається звичайною (реальну перевірку
  // бекенд усе одно робить при встановленні).
  compatibilityChecking?: boolean
}

// BrowserCompatEvent — подія "browser:compat", яку шле бекенд по кожному
// хіту пошуку окремо, коли фонова перевірка сумісності з активним фільтром
// збірки (mcVersion/loader) завершується. ContentBrowser.svelte слухає її
// й оновлює/ховає конкретну картку без перезапуску всього пошуку.
export interface BrowserCompatEvent {
  source: string
  projectId: string
  compatible: boolean
  unconfirmed: boolean
}

export interface UpdateInfo {
  available: boolean
  version: string
  downloadUrl: string
  changelogUrl: string
  isCritical: boolean
}

// ── Браузер контенту (моди/модпаки/ресурспаки/шейдери/датапаки) ────────

// BrowserProjectDetail — опис проєкту для сторінки модпаку (джерело
// Modrinth або CurseForge — поля уніфіковані бекендом).
export interface BrowserProjectDetail {
  source: string // modrinth | curseforge
  projectId: string
  slug: string
  projectType: string // mod | modpack | resourcepack | shader | datapack
  name: string
  description: string
  // body — markdown (Modrinth) або HTML (CurseForge) опис проєкту.
  body: string
  iconUrl: string
  downloads: number
  followers: number
  categories: string[]
  mcVersions: string[]
  loaders: string[]
  gallery: BrowserGalleryImage[]
  projectUrl: string
  // Секція «Деталі» (як ProjectSidebarDetails у Modrinth app):
  // дати публікації/оновлення, ліцензія.
  published: string
  updated: string
  licenseId: string
  licenseName: string
  licenseUrl: string
  // Секція «Сумісність»: середовища виконання (client/server).
  clientSide: string // required | optional | unsupported | unknown
  serverSide: string
  // Секція «Посилання» (як ProjectSidebarLinks у Modrinth app).
  issuesUrl: string
  sourceUrl: string
  discordUrl: string
  wikiUrl: string
  // Автор проєкту (секція «Автор»).
  author: string
  // authorId/authorAvatar/authorUrl — ідентичність автора для сторінки
  // автора (клік по автору → профіль з аватаром і всіма проєктами, як
  // UserPage у Modrinth app). CurseForge не віддає аватар — authorAvatar
  // порожній, фронт показує запасну літеру.
  authorId?: string
  authorAvatar?: string
  authorUrl?: string
  // creators — усі автори: організація-власник (якщо є), далі учасники
  // команди (власник першим), як ProjectSidebarCreators у Modrinth app.
  creators?: BrowserCreator[] | null
}

export interface BrowserCreator {
  id: string
  name: string
  avatar: string
  role: string
  isOwner: boolean
  isOrg: boolean
  url: string
}

// BrowserTranslation — результат ШІ-перекладу сторінки проєкту (Groq).
// Розмітка (markdown/HTML), кольори, посилання зберігаються — перекладено
// лише текст. Кешується на бекенді: повторне відкриття не б'є в API.
export interface BrowserTranslation {
  lang: string
  description: string
  body: string
  fromCache: boolean
  created: string
}

// BrowserReview — AI-відгук про проєкт (аналіз + веб-пошук). Генерується
// ОДИН раз і кешується: перегенерація заборонена (лише при помилці).
export interface BrowserReview {
  lang: string
  review: string
  sources: string[]
  fromCache: boolean
  created: string
}

// BrowserAuthor — профіль автора/організації для сторінки автора
// (аватар, біо, дата реєстрації + усі його проєкти).
export interface BrowserAuthor {
  source: string // modrinth | curseforge
  authorId: string
  username: string
  name: string
  avatar: string
  bio: string
  role: string
  created: string
  isOrg: boolean
  // hasProfile — чи API джерела віддає профіль автора. CurseForge не має
  // endpoint-а користувача — лише список проєктів через authorId.
  hasProfile: boolean
  profileUrl: string
}

export interface BrowserGalleryImage {
  url: string
  // rawUrl — оригінальне зображення для повноекранного лайтбокса.
  rawUrl?: string
  title: string
  featured?: boolean
}

// BrowserVersionView — версія проєкту (сторінка модпаку / «Оновити з джерела»).
export interface BrowserVersionView {
  id: string
  name: string
  versionNumber: string
  versionType: string // release | beta | alpha
  gameVersions: string[]
  loaders: string[]
  datePublished: string
  changelog: string
  fileName: string
  fileUrl: string
  downloadUrl: string
  fileSize: number
  sha1: string
}

// BrowserSearchResult — результати пошуку, згруповані по джерелах
// (Modrinth і CurseForge показуються окремими секціями).
export interface BrowserSearchResult {
  modrinth: BrowserEntry[]
  curseforge: BrowserEntry[]
  cfDisabled: boolean
  cfError?: string
  // modrinthError — помилка запиту Modrinth (мережа/API). Показується
  // замість порожнього списку («Немає інтернету» в офлайн-режимі тощо).
  modrinthError?: string
  // totalHits/hasMore — пагінація (Modrinth-джерело): загальна кількість
  // результатів і чи є ще сторінки після поточного offset+limit.
  totalHits: number
  hasMore: boolean
  // cfTotalHits/cfHasMore — те саме для CurseForge (окремий запит).
  cfTotalHits?: number
  cfHasMore?: boolean
  // smartSearchTerms — непорожньо, якщо запит розпізнано «розумним
  // пошуком» як тему (напр. «меблі») і розширено цими англійськими
  // термінами. UI показує підказку, чому видача відрізняється від
  // дослівного запиту.
  smartSearchTerms?: string[]
}

// BrowserCompatiblePack — кастомна збірка, сумісна з контентом
// (є версія під її mcVersion/loader). For діалогу встановлення.
// BrowserServerCompat — сумісність контенту з серверами лаунчера
// (GetBrowserServerCompat): версія під (mc, ядро) і сторона мода.
export interface BrowserServerCompatEntry {
  serverId: string
  compatible: boolean
  unconfirmed?: boolean
  // reason — clientOnly | noVersion | unsupportedCore
  reason?: string
  alreadyInstalled?: boolean
  installedFileName?: string
}

export interface BrowserServerCompat {
  clientSide: string
  serverSide: string
  clientOnly: boolean
  serverOnly: boolean
  servers: BrowserServerCompatEntry[] | null
}

export interface BrowserCompatiblePack {
  packId: string
  name: string
  iconUrl: string
  // Пресет-іконка (Tabler, напр. "ti-puzzle") збірки; iconUrl (PNG) має
  // пріоритет у UI, коли є.
  icon?: string
  color: string
  mcVersion: string
  loader: string
  alreadyInstalled: boolean
  installedFileName?: string
  // Whitelisted — це шаурма-збірка (не кастомна), автор якої явно
  // дозволив саме цей мод через Pack Manager → Білий список. Такі
  // збірки не входять у звичайний packPicker (лише kind==='custom') —
  // фронтенд показує їх ОКРЕМОЮ групою з іншим стилем, а не змішує зі
  // списком кастомних збірок.
  whitelisted?: boolean
  // Unconfirmed — бекенд НЕ зміг перевірити сумісність (мережева
  // помилка / немає CurseForge API-ключа / rate limit), а не «перевірив
  // і несумісна». Збірка все одно показана в списку (fail-open), але UI
  // має позначити її окремо, щоб не видавати непідтверджений результат
  // за гарантовану сумісність.
  unconfirmed?: boolean
}

export interface BrowserInstallResult {
  packId?: string
  fileName?: string
  installedTo?: string
  message: string
}

// DesktopShortcut — стан ярлика збірки на робочому столі (як у Modrinth
// App: ПКМ по збірці → «Створити ярлик на робочому столі»).
export interface DesktopShortcut {
  packId: string
  name: string
  path: string
  exists: boolean
  // Персональні опції запуску ярлика (акаунт/автоприєднання) — для
  // передзаповнення діалогу при редагуванні.
  accountId?: string
  autoJoinServer?: string
}

// PendingShortcutLaunch — дані запуску з ярлика: packID + персональні
// опції ярлика (акаунт, автоприєднання). Повертає TakePendingShortcutLaunch
// на маунті і шле подія shortcut:launch (запуск з ярлика при вже
// відкритому лаунчері).
export interface PendingShortcutLaunch {
  packId: string
  accountId?: string
  autoJoinServer?: string
}

// BrowserCategory — тег-категорія у фільтрі браузера (джерело + назва +
// іконка) — ті самі теги з іконками, що у SearchSidebarFilter веб-апки
// Modrinth; показуються окремо по джерелах.
export interface BrowserCategory {
  source: string // modrinth | curseforge
  name: string
  icon: string
  // header — нормалізована група категорії ("category" | "feature" |
  // "performance-impact" | "resolutions" | ...), спільна для обох джерел,
  // щоб «Оптимізація» з Modrinth і CurseForge потрапляли в одну групу.
  header: string
  // cfId — числовий ID категорії CurseForge (0 для Modrinth-категорій).
  // CurseForge Search API фільтрує за categoryId(s), а НЕ за назвою тегу —
  // без цього поля фронтенд міг слати лише людську назву («Технології»),
  // яка на CurseForge не є ідентифікатором фільтра і мовчки ігнорувалась
  // бекендом (постфактум-збіг по строці на вже обрізаній лімітом видачі).
  cfId: number
}

// BrowserInstalledItem — встановлений файл контенту у збірці, зіставлений
// з проєктом (аналіз «вже встановлено» за sha1, як у Modrinth app).
export interface BrowserInstalledItem {
  projectId: string
  projectSlug: string
  versionId: string
  versionNumber: string
  fileName: string
  dir: string // mods | resourcepacks | shaderpacks | datapacks
  source: string
}

// BrowserGameVersion — версія гри для фільтра браузера. Джерело: теги
// Modrinth /tag/game_version (лише версії з контентом) або Mojang-маніфест.
export interface BrowserGameVersion {
  id: string
  versionType: string // release | snapshot | beta | alpha
  date: string
  major?: boolean
}

// BrowserLoaderTag — лоадер для фільтра браузера з /tag/loader Modrinth.
// types — SupportedProjectTypes (для яких типів контенту лоадер підходить).
export interface BrowserLoaderTag {
  name: string
  icon: string
  types: string[]
}

// BrowserModpackInstallCheck — результат CheckBrowserModpackInstalled:
// чи модпак (source, projectId) уже доданий у «Моїх збірках».
export interface BrowserModpackInstallCheck {
  alreadyInstalled: boolean
  existingPackId?: string
  existingPackName?: string
}

// BrowserProgress — прогрес встановлення контенту (подія browser:progress).
export interface BrowserProgress {
  projectId: string
  packId: string
  fileName: string
  downloaded: number
  total: number
  percent: number
}

// ── Розумна консоль гри ─────────────────────────────────────────────────

export interface ConsoleLine {
  id: number
  time: string
  level: string // info | warn | error | system
  text: string
}

export interface ConsoleContext {
  instanceId: string
  name: string
  mcVersion: string
  loader: string
  running: boolean
  ramMb: number
  // Іконка збірки для панелі запуску консолі (окреме вікно не отримує
  // пропси головного вікна — бекенд віддає їх у контексті).
  iconUrl?: string
  icon?: string
  color?: string
  // Active — чи ця збірка АКТИВНА у головному вікні (вибрана/відкрита на
  // сторінці деталей) або запущена зараз. Окреме вікно консолі НЕ пропонує
  // «Запустити» для неактивної збірки (захист від «запустилась інша збірка»).
  active?: boolean
  // Status — РЕАЛЬНИЙ стан збірки (той самий, що в sidebar/«Мої збірки»):
  // not-installed | needs-update | ready | downloading | updating | running |
  // error. Панель запуску консолі показує правильну дію (Завантажити /
  // Оновити / Запустити) за ним, а не фіктивну.
  status?: string
  // Progress — активний прогрес завантаження/оновлення, якщо йде.
  progress?: BuildProgress
  // Акаунт, чию консоль показуємо (після перемикання може відрізнятись
  // від активного акаунта) + список акаунтів з логом для перемикача.
  accountId?: string
  accountName?: string
  accounts?: ConsoleAccountInfo[]
  // Launching — активна стадія запуску (checking/worker_update/java/loader)
  // з останньої події launch:progress: консоль показує реальний стан,
  // навіть якщо її відкрили посеред запуску.
  launching?: boolean
  launchStage?: string
  launchMessage?: string
  launchPercent?: number
  // Стан краху + останній AI-діагноз — СПІЛЬНІ для всіх копій консолі
  // (вікно і вкладка): консоль, відкрита після краху, показує той самий
  // віджет ШІ, що й та, яка була відкрита під час нього.
  crashed?: boolean
  lastExitCode?: number
  diagnosis?: AIDiagnosis | null
}

export interface AIRecommendedAction {
  type: string // increase_ram | disable_mod | download_dependency | repair | none
  targetMod: string
  dependencyName: string
  suggestedRamMb: number
}

export interface AIResult {
  // kind — тип проблеми, який ВИЗНАЧИВ САМ ШІ (MISSING_DEPENDENCY |
  // MOD_CONFLICT | SINGLE_MOD_ERROR | ENV_CORRUPTION | JAVA_VERSION |
  // UNKNOWN). Машинного аналізу на бекенді немає — все вирішує Gemini.
  kind: string
  cause: string
  explanation: string
  facts: string[]
  actionable: boolean
  recommendedAction: AIRecommendedAction
  copyText: string
  model: string
}

export interface AIDiagnosis {
  kind: string
  crashReportFile: string
  currentRamMb: number
  // rawLogText — той самий текст, що пішов у промт моделі (crash-report
  // файл цілком, або вирізка останніх WARN/ERROR рядків консолі).
  // Заповнюється ЗАВЖДИ (навіть коли aiEnabled=false) — це просто
  // читання файлу, без мережевого виклику.
  rawLogText?: string
  aiEnabled: boolean
  aiAvailable: boolean
  aiError: string
  aiResult?: AIResult
}

export interface AIFixResult {
  success: boolean
  message: string
  projectUrl: string
  source: string
  installedTo: string
}

// Опції експорту списку модів (Prism ExportToModList): формат + поля.
export interface ModListExportOptions {
  format: 'html' | 'markdown' | 'plain' | 'json' | 'csv'
  authors: boolean
  url: boolean
  version: boolean
  fileName: boolean
  onlyEnabled: boolean
  // Обмеження експорту лише цими файлами модів (порожній = всі).
  // Використовується «Експортувати вибране» у панелі масових дій.
  fileNames?: string[]
}

// Запис встановленого контенту збірки (вкладка «Контент»: ресурспаки /
// датапаки / шейдери). fileName — реальне ім'я на диску (може містити
// .disabled), Name — показове (без розширення і без .disabled). Поля версії
// (sha1..iconUrl) заповнюються App.RefreshContentUpdates для ресурспаків/
// датапаків/шейдерів — аналог даних менеджера модів.
export interface ContentFileEntry {
  fileName: string
  name: string
  enabled: boolean
  isDir: boolean
  fileSize?: number
  sha1?: string
  projectId?: string
  slug?: string
  source?: string // modrinth | curseforge | ""
  version?: string
  latestVersion?: string
  hasUpdate?: boolean
  author?: string
  iconUrl?: string
}

// WhitelistEntry — один запис білого списку моду шаурма-збірки (JSON з
// CDN, той самий контракт, що pack-manager/internal/model.WhitelistEntry
// публікує в global-whitelist.json / packs/<id>/whitelist.json). Лаунчер
// НЕ видаляє jar, чий ModID тут є, під час перевірки цілісності файлів,
// навіть якщо його немає в маніфесті збірки.
export interface WhitelistEntry {
  modId: string
  name?: string
  source?: string // "jar" | "url" | "manual" | "modrinth" | "curseforge"
  sourceRef?: string
  // Прив'язка до Modrinth/CurseForge — заповнена, коли source —
  // "modrinth"/"curseforge" (автор збірки додав запис за посиланням на
  // сторінку проєкту в Pack Manager, а не з локального jar). Дає змогу
  // показати картку мода з іконкою та переходом на сторінку.
  platform?: string // "modrinth" | "curseforge"
  projectId?: string
  iconUrl?: string
  projectUrl?: string
  addedAt?: string
}

// PackWikiDoc — вікі шаурма-збірки (packs/<id>/wiki.json), яку адмін
// створив у Pack Manager. Дзеркало backend/internal/sync.PackWikiDoc.
// Розділи показуються у лаунчері: картинка зліва, HTML/Markdown справа.
export interface PackWikiSection {
  id: string
  title: string
  icon?: string
  image?: string // data:image/...;base64,...
  body?: string
  format?: string // "" | "html" | "md"
}
export interface PackWikiDoc {
  enabled: boolean
  title?: string
  description?: string
  updatedAt?: string
  sections: PackWikiSection[]
}

// ── «Єдине джерело» модів ────────────────────────────────────────────────
// Прикріплене до збірки/сервера джерело: окремий jar або уся тека модів.
export type ModSourceKind = 'file' | 'folder'

export interface ModSourceView {
  id: string
  kind: ModSourceKind
  path: string
  fileName?: string
  addedAt: number
  // Джерела зараз немає на диску.
  missing: boolean
}

export interface ModSourcesView {
  sources: ModSourceView[]
  // Прикріплено ТЕКУ: жодних ручних змін модів цієї збірки/сервера.
  locked: boolean
  folderPath?: string
}

export interface ModSourcesProgress {
  packId: string
  done: number
  total: number
  file: string
  action: 'check' | 'copy' | 'remove'
  percent: number
}

export interface ModEntry {
  id: string
  fileName: string
  name: string
  version: string
  loader: string
  enabled: boolean
  hasUpdate: boolean
  source: string // modrinth | curseforge | unknown
  // Мод походить із «єдиного джерела»: його не можна вимкнути, видалити чи
  // оновити вручну (див. ModSourcesDialog). folder = уся тека модів іншої збірки.
  sourceKind?: ModSourceKind
  sourcePath?: string
  sourceMissing?: boolean
  author?: string
  description?: string
  iconUrl?: string
  // hasIcon — у jar є вбудована іконка; сам base64 у списку НЕ приходить
  // (лінивий GetModIcons, див. modIcon.ts). iconData лишається для сумісності.
  hasIcon?: boolean
  iconData?: string
  fileSize?: number
  installedDate?: string
  projectSlug?: string
  projectId?: string
  mcVersion?: string
  latestVersion?: string
  latestUrl?: string
  dependsOn?: string[]
  provides?: string[]
  hasMissingDeps?: boolean
  missingDeps?: string[]
  sha1?: string
  status?: string // checking | done | error
  error?: string
  isDuplicate?: boolean
  incompatible?: boolean
  incompatibleReason?: string
  // viaConnector — Fabric/Quilt-мод, що працює у Forge/NeoForge-збірці через Sinytra Connector
  viaConnector?: boolean
  // loaders — повний список лоадерів, під які jar реально придатний
  // (мультизавантажувальні jar-и, напр. AmbientSounds, містять
  // метадані одразу і Forge, і Fabric в одному файлі). loader (однина)
  // лишається для бейджа/іконки; сумісність на бекенді рахується саме
  // за цим повним списком.
  loaders?: string[]
  // environment — сторона, на якій працює мод: "client" | "server" |
  // "*" (обидві) | undefined (невідомо — лоадер не декларує статично,
  // типово Forge/NeoForge). Читається лише з fabric.mod.json/
  // quilt.mod.json поля "environment".
  environment?: string
}

export interface ModVersionOption {
  id: string
  name: string
  versionNumber: string
  gameVersion: string
  loader: string
  url: string
  filename: string
  sha1: string
  datePublished: string
  isCurrent?: boolean
  versionType?: string // release | beta | alpha
  changelog?: string
}

// SuggestedMod — рекомендація на вкладці «Контент»: hard-залежність уже
// встановлених модів, якої самої ще немає в збірці (App.GetSuggestedMods).
export interface SuggestedMod {
  depId: string
  projectId: string
  slug: string
  name: string
  iconUrl: string
  requiredBy: string[]
}

// DependencyResult — результат пошуку/завантаження залежності (Modrinth),
// повертається InstallSuggestedMod (і рештою dependency-пайплайну).
export interface DependencyResult {
  success: boolean
  source: string // modrinth | curseforge
  message: string
  projectUrl?: string
  installedTo?: string
}

export interface ModImportResult {
  imported: string[]
  skipped: string[]
  failed: string[]
}

export interface ModDependency {
  modId: string
  name: string
  version: string
  downloadUrl: string
  filename: string
  iconUrl: string
  resolved: boolean
  error?: string
}

export interface ModBackupEntry {
  id: string
  modId: string
  name: string
  version: string
  fileName: string
  backupFile: string
  date: string
  sourceUrl?: string
}

export interface DuplicateModGroup {
  sha1: string
  mods: ModEntry[]
}

export interface ModUpdateResult {
  updated: string[]
  failed: string[]
  backedUp: boolean
}

export interface PreLaunchModIssues {
  hasIssues: boolean
  duplicates: DuplicateModGroup[]
  missingDeps: string[]
  blockedMods?: string[]
}

// ── Гнучкі ярлики збірок (як діалог CreateShortcutDialog у Prism) ──────
// Параметри створення ярлика: назва + місце (робочий стіл / меню «Пуск» /
// довільна тека).
export interface ShortcutOptions {
  name: string
  // "desktop" | "startmenu" | абсолютний шлях до теки.
  location: string
  // Персональні опції запуску ярлика (як у Prism): перекривають акаунт
  // та автоприєднання збірки рівно для запуску з цього ярлика.
  accountId?: string
  autoJoinServer?: string
}

// ── Міграція збірок з інших лаунчерів (Prism / Modrinth App) ───────────
// Окрема система від міграції модів: переноситься ЦІЛА збірка з іншого
// лаунчера — файли, налаштування, іконка, ID/назва (крім кешів у теці
// Minecraft). Дзеркалює launchermigrate (backend).

// MigrationPackItem — одна збірка іншого лаунчера, знайдена при скануванні.
export interface MigrationPackItem {
  kind: string // prism | modrinth
  id: string
  name: string
  mcVersion: string
  loader: string
  loaderVersion: string
  iconPath?: string // абсолютний шлях до PNG (порожній = іконки немає)
  alreadyImported?: boolean // чи вже перенесена у цей лаунчер раніше
  gameDir: string
  sizeBytes: number
  maxRamMb?: number
  minRamMb?: number
  javaPath?: string
  jvmArgs?: string
  hasConfig: boolean
}

// MigrationLauncher — знайдений інший лаунчер зі списком його збірок.
export interface MigrationLauncher {
  kind: string
  name: string
  dataDir: string
  found: boolean
  packs: MigrationPackItem[]
}

// MigrationScanResult — результат сканування (перемикачі між лаунчерами).
export interface MigrationScanResult {
  launchers: MigrationLauncher[]
  foundLaunchers: number
}

// MigrateStatus — стан міграції (пост-логін вікно + віджет у налаштуваннях).
export interface MigrateStatus {
  shouldShow: boolean
  promptShown: boolean
  hasAccounts: boolean
  foundLaunchers: number
  /** Скільки ще не перенесених збірок знайдено в інших лаунчерах. */
  packCount: number
  launchers: MigrationLauncher[]
}

// MigrateImportProgress — прогрес імпорту ОДНІЄЇ збірки (подія migrate:progress).
export interface MigrateImportProgress {
  launcher: string
  packId: string
  packName: string
  stage: string // copying | icon | done | error
  filesDone: number
  filesTotal: number
  newPackId?: string
  error?: string
}

// MigrateImportRequest — вибір користувача: збірки одного лаунчера (вкладки).
export interface MigrateImportRequest {
  launcher: string
  packIds: string[]
}

// MigrateImportResult — підсумок імпорту (подія migrate:done).
export interface MigrateImportResult {
  imported: string[]
  failed: string[]
  errors: Record<string, string>
}

// DBHealth — стан бази даних при старті (відновлення з бекупу / навмисне
// скидання / робота в пам'яті) — модальне вікно з поясненням.
export interface DBHealth {
  restoredFromBackup: boolean
  backupAt?: string
  erasedIntentionally: boolean
  erasedAt?: string
  fallbackMemory: boolean
}

// ── Міграція модів між збірками ─────────────────────────────────────────
// Той самий сценарій, що ModMigrationScreen у старому лаунчері: вибір
// «Звідки/Куди», аналіз (Modrinth + CurseForge) зі звітом, міграція.

export interface MigrationPack {
  packId: string
  name: string
  mcVersion: string
  loader: string
  iconUrl: string
  // Пресет-іконка (Tabler, напр. "ti-puzzle") кастомної збірки; iconUrl
  // (PNG) має пріоритет, коли є. Shaurma-каталог пресет-іконки не має.
  icon?: string
  // Колір збірки (для кастомних збірок — кольоровий аватар у селекторі).
  color: string
  // Кількість увімкнених .jar у теці модів (підказка в селекторі).
  modCount: number
  // 'server' — таргет-сервер ("srv:<id>"/"srvp:<id>", modTarget.ts).
  kind?: string
}

export interface MigrationItem {
  sourceName: string
  sourceFileName: string
  sourceSha1: string
  sourceModId?: string
  // found | not-found | already | update
  status: string
  projectId: string
  projectSlug: string
  resolvedName: string
  resolvedVersion: string
  resolvedFilename: string
  resolvedUrl: string
  resolvedSha1: string
  source: string // modrinth | curseforge
  iconUrl: string
  willReplace: boolean
  // localCopy: джерело і ціль на однаковому loader+MC-версії — файл
  // переноситься прямим копіюванням з диска джерела, без перевірки на
  // Modrinth/CurseForge (важливо показати це в UI окремо від "found").
  localCopy?: boolean
  // sourcePackId: ID збірки-джерела — потрібен бекенду, щоб знайти jar
  // на диску, коли localCopy=true. Має бути переданий назад у
  // MigrateMods/StartModMigration разом з рештою полів item.
  sourcePackId?: string
  // side: де працює мод — лише в звіті портування на сервер
  // (AnalyzeModPort): 'server' | 'client' | 'unknown'.
  side?: 'server' | 'client' | 'unknown'
}

export interface ModMigrationReport {
  items: MigrationItem[]
  found: number
  notFound: number
  already: number
  needsUpdate: number
  sourcePackName: string
  targetMcVersion: string
  targetLoader: string
  // Аналіз перервано кнопкою «Скасувати» — items містить частковий результат.
  cancelled: boolean
}

export interface ModMigrationResult {
  migrated: string[]
  failed: string[]
  skipped: number
}

// ── Порівнювач модів двох збірок (CompareModPacks) ───────────────────────
// На відміну від MigrationPack/MigrationItem вище (одностороння міграція:
// джерело → ціль), тут обидві збірки рівноправні — кожен рядок несе стан
// ОБОХ сторін окремо, і встановити/оновити можна в будь-яку з них.

export interface ModComparePack {
  packId: string
  name: string
  mcVersion: string
  loader: string
  iconUrl?: string
  icon?: string
  color?: string
}

export interface ModCompareSide {
  // present (мод стоїть тут) | outdated (стоїть, але інша сторона новіша) |
  // missing (мода тут немає)
  status: 'present' | 'outdated' | 'missing'

  // Заповнено, коли status = present | outdated (файл реально на диску).
  fileName?: string
  version?: string
  enabled?: boolean
  sha1?: string
  modId?: string

  // Заповнено, коли status = missing | outdated (що можна встановити).
  canInstall?: boolean
  // "noCompatVersion" — мод є на маркетплейсі, але немає версії під
  // loader/MC саме цієї сторони; порожньо — мода взагалі не знайдено.
  compatNote?: 'noCompatVersion' | ''
  // Обидві збірки на однаковому loader+MC — пряме копіювання з диска,
  // без мережі.
  localCopy?: boolean
  source?: 'modrinth' | 'curseforge' | ''
  projectId?: string
  projectSlug?: string
  resolvedName?: string
  resolvedVersion?: string
  resolvedFilename?: string
  resolvedUrl?: string
  resolvedSha1?: string
  sourcePackId?: string
  sourceFileName?: string
}

export interface ModCompareRow {
  key: string
  name: string
  author?: string
  iconUrl?: string
  left: ModCompareSide
  right: ModCompareSide
  sameVersion: boolean
}

export interface ModCompareReport {
  rows: ModCompareRow[]
  bothSame: number
  diff: number
  onlyLeft: number
  onlyRight: number
  sameLoaderVersion: boolean
}

export interface CompareInstallRequest {
  targetPackId: string
  side: ModCompareSide
  sourceName: string
  replace: boolean
}

// Перевірка ID збірки перед створенням (CheckCustomPackID): slug з назви,
// чи вже зайнятий, і перший вільний варіант (id, id-2, id-3…).
// ── Заблоковані автором моди (імпорт CurseForge zip) ─────────────────────
// Моди, чиї файли автор заборонив качати зі сторонніх джерел (3rd party
// download opt-out): CurseForge не віддає пряме посилання. Імпорт стає на
// паузу і показує діалог (як BlockedModsDialog у Prism): Modrinth fallback
// за sha1 / пропустити / скасувати. Див. backend custompack.BlockedCFMod.
export interface BlockedCFMod {
  projectId: number
  fileId: number
  fileName: string
  sha1: string
  websiteUrl: string
  required: boolean
}

// BlockedModInfo — мод, який не вдалося завантажити при імпорті (зберігається
// в Pack.BlockedMods). Див. backend custompack.BlockedModInfo.
export interface BlockedModInfo {
  fileName: string
  websiteUrl?: string
  required: boolean
  sha1?: string
  downloaded?: boolean
}

// GameLanguage — одна мова гри з оригінального asset index Mojang
// (minecraft/lang/<code>.json). Name — читабельна назва з самого файлу
// мови (language.name + language.region), може бути порожньою.
export interface GameLanguage {
  code: string
  name: string
}

export interface CustomPackIDCheck {
  id: string
  exists: boolean
  suggested: string
}

// ── RAM-попередження перед запуском збірки ──────────────────────────────
// Результат App.GetLaunchWarnings(packID): фронтенд викликає його ПЕРЕД
// LaunchInstance і, якщо ShowRecommended/ShowLowRam — показує модалку.
// Dismissed-прапорці = «не показувати знову» для ЦІЄЇ збірки
// (InstanceConfig.recommendedRamDismissed/lowRamDismissed).
export interface LaunchWarnings {
  packId: string
  // Ефективна RAM збірки: per-instance override або глобальна (МБ).
  effectiveRamMb: number
  // Рекомендована RAM збірки з packs-index.json (0 = вимоги немає).
  recommendedRamMb: number
  // Фізично доступна оперативна пам'ять системи (МБ).
  availableRamMb: number
  // don't-show-again прапорці для ЦІЄЇ збірки.
  recommendedDismissed: boolean
  lowRamDismissed: boolean
  // Глобальний вимикач попереджень (Settings.showRamWarnings).
  warningsEnabled: boolean
  // Чи показувати попередження (розраховано бекендом з урахуванням
  // прапорців вище — фронтенд просто рендерить).
  showRecommended: boolean
  showLowRam: boolean
}

export interface ServerPing {
  online: boolean
  version?: string
  protocol?: number
  players?: number
  maxPlayers?: number
  motd?: string
  // favicon — data URL іконки, яку віддає сам сервер у status-відповіді.
  favicon?: string
  error?: string
}

// Результат копіювання одного сервера в іншу збірку (батч CopyServersToPack).
// exists = сервер уже був у цільовій збірці (назву оновлено, дублікатів нема).
export interface ServerCopyResult {
  address: string
  name?: string
  exists: boolean
  copied: boolean
  error?: string
}

// ── Прогрес V2-синхронізації (sync:progress від sync.Runner) ─────────────
// Новий двочерговий рушій Шаурма-збірок шле прогрес у форматі RunnerProgress
// (а не SessionProgress старого download.Engine). mode: running | paused |
// cancelled | complete | error. stage: downloading | extracting | finalizing…
export interface SyncProgress {
  packId: string
  packName: string
  mode: string
  filesTotal: number
  filesCompleted: number
  bytesTotal: number
  bytesDownloaded: number
  overallPercent: number
  currentFile: string
  speedBps: number
  stage: string
  noInternet: boolean
  done: boolean
  errorMsg?: string
  // iconUrl — іконка модпаку (local-file://…), якщо з'явилась у placeholder'а
  // під час імпорту (вбудована icon.png) — оновлює картку одразу.
  iconUrl?: string
}

export interface PackIndexEntry {
  id: string
  name: string
  description: string
  mc_version: string
  loader_type: string
  loader_version: string
  icon_url: string
  background_url: string
  mrpack_url: string
  updated_at: string
  tags: string[]
  color?: string
  version?: string
  icon?: string
  // server_ip — офіційний сервер збірки Shaurma (з packs-index.json,
  // JSON-тег "server_ip"). Не плутати з InstanceServer у списку "Сервери":
  // це окрема сутність, що показується віджетом біля кнопки "Грати" і НЕ
  // з'являється у server.dat автоматично (лише пропонується додати).
  server_ip?: string
  // ── Серверна папка групування (packs-index.json → folderId/...) ──
  // Заданий folder_id — усі збірки з тим самим folder_id групуються в
  // одну папку sidebar (назва/колір беруться з folder_name/folder_color,
  // сортування всередині папки — folder_order, позиція самої папки серед
  // інших — folder_position). Порожній — збірка поза папками, як зараз.
  folder_id?: string
  folder_name?: string
  folder_color?: string
  folder_order?: number
  folder_position?: number
}

// ── Архітектура збірок (GetBuilds) ──────────────────────────────────────

// PackFolder — папка кастомних збірок («Мої збірки»/sidebar). Збірка
// належить максимум одній папці; збірки в папках приховані з головного
// списку і показуються всередині папки. Зберігається в settings.json.
export interface PackFolder {
  id: string
  name: string
  color?: string
  // Tabler-іконка папки (напр. 'ti-sword'); порожньо — стандартна 'ti-folder'.
  icon?: string
  packIds: string[]
  // serverManaged — папка прийшла з packs-index.json (Pack Manager,
  // folder_id/folder_name/...), а не створена користувачем локально.
  // Членство (які збірки в папці), назва, колір і сортування ВСЕРЕДИНІ
  // такої папки диктує сервер — користувач НЕ може вручну витягнути з
  // неї збірку чи докинути стороннє (ТЗ: «якщо користувач захоче сам
  // там змінити положення то він може це зробити але не витягувати з
  // папки чи кидати туди» — «положення» тут означає позицію самої
  // папки серед інших елементів, не її вміст). Локальні папки (без
  // цього прапорця) лишаються повністю керованими користувачем, як і
  // раніше.
  serverManaged?: boolean
}

// ServerFolder — папка серверів у «Бібліотеці серверів» (як PackFolder для
// збірок). Сервер — максимум в одній папці; зберігається в .build-layout.json.
export interface ServerFolder {
  id: string
  name: string
  color?: string
  icon?: string
  serverIds: string[]
}

// ── «Останнє вікно» (сторінка, вкладка, відкрита збірка, скрол) ─────────
// Зберігається на диску через App.GetUIState()/SaveUIState() (бекенд:
// store.UIState, файл .ui-state.json) — НЕ в localStorage.
export interface PackUIState {
  tab?: string
  scroll?: number
  // Скрол по РОЗДІЛАХ (вкладках) сторінки збірки: scrolls[розділ] = позиція.
  scrolls?: Record<string, number>
}

export interface UIState {
  page?: string
  instanceTab?: string
  settingsTab?: string
  openPackId?: string
  lastPacks?: string[]
  pages?: Record<string, number>
  packs?: Record<string, PackUIState>
}

export type BuildKind = 'shaurma' | 'custom'

export type BuildStatus =
  | 'not-installed'
  | 'needs-update'
  | 'ready'
  | 'downloading'
  | 'updating'
  | 'running'
  | 'error'

export interface BuildProgress {
  percent: number
  bytesDone: number
  bytesTotal: number
  filesDone: number
  filesTotal: number
  currentFile: string
  speedBps: number
  // Mode — "running" | "paused" | "cancelled" | "complete" | "error"
  // (sync.RunMode) — щоб UI показав «Зупинено»/«Продовжити» замість
  // завислого прогресу.
  mode?: string
  // Stage — "downloading" | "extracting" | "finalizing" тощо (sync.Stage),
  // відображається як підпис під прогрес-баром.
  stage?: string
  noInternet?: boolean
}

export interface Build {
  id: string
  kind: BuildKind
  name: string
  description: string
  mcVersion: string
  loaderType: string
  loaderVersion: string
  iconUrl: string
  backgroundUrl: string
  // icon — пресет-іконка (Tabler, напр. "ti-puzzle"); PNG (iconUrl) має пріоритет.
  icon?: string
  color: string
  version: string
  source: string
  // Джерело «інтернет-збірки» (modrinth/curseforge): проєкт і встановлена
  // версія — для списку версій у кнопці «Оновити» на сторінці збірки.
  sourceProjectId?: string
  sourceVersionId?: string
  serverIp: string
  tags: string[]
  // ── Серверна папка групування (див. PackIndexEntry.folder_id вище) ──
  folderId?: string
  folderName?: string
  folderColor?: string
  folderOrder?: number
  folderPosition?: number
}

export interface BuildView {
  build: Build
  status: BuildStatus
  installed: boolean
  progress?: BuildProgress
  // Акаунти, на яких збірка ЗАРАЗ запущена (крім поточного — він і так
  // бачить status='running'). Та сама збірка може бути запущена на кількох
  // акаунтах одночасно (1 збірка × 1 акаунт) — UI показує їх голови.
  runningOn?: RunningAccount[]
  // unix-мс моменту старту сесії поточного акаунта (лише коли status='running').
  // Фолбек для таймера «у сесії», якщо подію build:started пропущено.
  startedAtMs?: number
  error?: string
}

// Акаунт, на якому збірка запущена на іншому акаунті (для голів у UI).
export interface RunningAccount {
  accountId: string
  accountName: string
}

// ── Прогрес запуску збірки (launch:progress від App.LaunchInstance) ──────
// Стадії ensure-Java/ensure-Loader/worker-перевірки ПЕРЕД стартом процесу
// гри — не плутати з BuildProgress (той про якання файлів модпаку).
// Кожна стадія має закріплений колір у UI:
//   checking      → жовтий (перевірка збірки/акаунта/токена)
//   worker_update → синій  (звірка версії файлів збірки з Шаурма-worker)
//   java          → червоний (перевірка/встановлення Java)
//   loader        → зелений (встановлення Fabric/Quilt/Forge/NeoForge)
export type LaunchStage = 'checking' | 'worker_update' | 'java' | 'loader' | 'done' | ''

export interface LaunchProgress {
  instanceId: string
  stage: LaunchStage
  message?: string
  // Реальний % виконання поточної стадії (0-100), коли він відомий (качка
  // клієнта/бібліотек/асетів, розпаковка Java); -1 = стадія без шкали.
  percent?: number
  done: boolean
  error?: string
  // Warning — попередження, яке НЕ зупиняє запуск (напр. "немає
  // інтернету — B2310 запуститься офлайн"). done лишається false в
  // цьому конкретному повідомленні; наступні launch:progress йдуть як
  // звичайно. Фронтенд показує модалку і продовжує слухати подію.
  warning?: string
}

export interface BuildStartEvent {
  buildId: string
  accountId: string
  accountName: string
  // unix-мс моменту старту сесії (для відновлених сесій — збережений час).
  startedAt?: number
}

export interface BuildExitEvent {
  buildId: string
  accountId: string
  code: number
  // wasUserStop — true, якщо вихід стався через явну ручну зупинку
  // гравцем (кнопка «Зупинити»), а не крах. Див. коментар у
  // model.BuildExitEvent (backend/internal/model/types.go).
  wasUserStop?: boolean
}

// Новий AI-діагноз крашу для КОНКРЕТНОЇ збірки (подія console:ai):
// buildId дозволяє у режимі «кілька вікон консолі по збірках» реагувати
// лише консолям цієї збірки.
export interface ConsoleAIEvent {
  buildId: string
  diagnosis: AIDiagnosis
}

export interface BuildConsoleLine {
  buildId: string
  accountId: string
  line: string
}

export interface InstanceServer {
  id: string
  name: string
  address: string
  // icon — data URL (data:image/png;base64,....) з servers.dat, якщо
  // збережена (гра пише її туди сама, а лаунчер може оновити з пінгу).
  icon?: string
  pinned: boolean
  official: boolean
  // temp — сервер додано ТИМЧАСОВО (P2P-кімната: співхост або запрошення).
  // Лаунчер сам прибирає такий запис, коли доступу не стане
  // (App.SyncTempPackServers), тож UI має позначити його візуально.
  temp?: boolean
}

// Закріплений сервер разом із інформацією про збірку, з якої він
// походить (секція «Закріплені сервери» над картками на «Моїх збірках»).
export interface PinnedServerView {
  packId: string
  packName: string
  packKind: 'shaurma' | 'custom'
  packColor: string
  packIconUrl: string
  // Пресет-іконка (Tabler, напр. "ti-puzzle") збірки; packIconUrl (PNG)
  // має пріоритет у UI, коли є.
  packIcon?: string
  address: string
  name: string
  // icon — data URL іконки сервера з servers.dat, якщо збережена.
  icon?: string
}

// Редаговані per-instance параметри збірки. Версія гри, лоадер і фон
// свідомо відсутні — для збірок Шаурма вони фіксовані маніфестом і на
// сторінці збірки не редагуються. 0 / "" / null означає "успадкувати
// глобальні налаштування лаунчера".
export interface InstanceConfig {
  id: string
  maxRamOverride: number
  minRamOverride: number
  javaArgsOverride: string
  fullscreenOverride: boolean | null
  windowWidthOverride: number
  windowHeightOverride: number
  preLaunchCommandOverride: string
  wrapperCommandOverride: string
  postExitCommandOverride: string
  envVarsOverride: string
  useCustomCommands: boolean
  useSeparateJava: boolean
  javaPathOverride: string
  accountIdOverride: string
  autoJoinEnabled: boolean
  autoJoinType: string
  autoJoinWorld: string
  autoJoinServer: string
  // Скін збірки: пресет з Гардероба, який застосовується на акаунт при
  // запуску (лише для ліцензійних акаунтів). Порожній = вимкнено.
  skinPresetId: string
  // Чи повертати попередній скін/плащ після зупинки збірки.
  // null = увімкнено (дефолт), false = лишити як змінився.
  skinRestoreOnExit: boolean | null
  // pinnedServerIps — адреси серверів (зі servers.dat), закріплених у
  // розділі "Сервери" (UI-стан лаунчера, гра про це не знає).
  pinnedServerIps?: string[]
  // @deprecated — старий per-instance список серверів; новий UI читає
  // сервери напряму зі servers.dat через App.ListPackServers.
  servers?: InstanceServer[]
}

// ── Кастомні збірки (створення вручну / імпорт .mrpack, .zip) ──────────

export interface MCVersionEntry {
  id: string
  type: string // release | snapshot | old_beta | old_alpha
  releaseTime: string
}

export interface LoaderVersionEntry {
  version: string
  stable: boolean
  recommended: boolean
}

export type CustomLoader = 'vanilla' | 'fabric' | 'forge' | 'neoforge' | 'quilt'

export interface CustomPack {
  id: string
  name: string
  description: string
  iconPath: string
  backgroundPath: string
  icon?: string
  color: string
  mcVersion: string
  loader: string
  loaderVersion: string
  useCustomRam: boolean
  minRamMb: number
  maxRamMb: number
  useSeparateJava: boolean
  javaPath: string
  // source — manual | import | modrinth | curseforge («інтернет-збірка»).
  source: string
  importFile: string
  // Джерело «інтернет-збірки» — для кнопки «Оновити з джерела».
  sourceProjectId?: string
  sourceVersionId?: string
  sourceProjectSlug?: string
  // sourceFiles — шляхи файлів, що прийшли з маніфесту (для diff при оновленні).
  sourceFiles?: string[]
  // blockedMods — моди, які не вдалося завантажити при імпорті (автор
  // заблокував, Modrinth не знайдено). Сторінка збірки показує
  // попередження зі списком та посиланнями для ручного скачування.
  blockedMods?: BlockedModInfo[]
  createdAt: string
  updatedAt: string
}

export interface CustomPackFormParams {
  name: string
  description: string
  iconSrcPath: string
  backgroundSrcPath: string
  backgroundRemove?: boolean
  icon?: string
  // iconChanged — true лише коли користувач у ЦЬОМУ збереженні явно
  // торкнувся вибору іконки (пресет або новий файл). Форма тримає
  // iconName у стані завжди, тож без цього прапорця бекенд не міг
  // відрізнити "не чіпали" від "свідомо обрали пресет" і затирав
  // робочий PNG при звичайному збереженні назви/опису.
  iconChanged?: boolean
  color: string
  mcVersion: string
  loader: string
  loaderVersion: string
  useCustomRam: boolean
  minRamMb: number
  maxRamMb: number
  useSeparateJava: boolean
  javaPath: string
}

export type ModpackFormat = 'mrpack' | 'curseforge' | 'generic' | 'shaurmpack'

export interface DetectedPack {
  format: ModpackFormat
  suggestedName: string
  mcVersion: string
  loader: string
  loaderVersion: string
  modCount: number
  hasOverrides: boolean
  unresolvedCFCount: number
  // iconDataUrl — вбудована іконка модпаку (data URL) для прев'ю у формі.
  iconDataUrl?: string
}

// ── .shaurmpack — власний формат модпаків (окремий шлях від DetectedPack
// вище: .mrpack/.zip проходять через DetectModpackArchive/ImportModpackArchive,
// .shaurmpack — через PreviewShaurmpack/ImportShaurmpackFile). Формат файлу
// на фронтенді визначається за розширенням шляху (.shaurmpack), а не по
// полю format, як для DetectedPack. ──
export interface ShaurmpackPreview {
  packId: string
  name: string
  description?: string
  versionId: string
  authors?: string[]
  gameVersion: string
  loader: string
  loaderVersion?: string
  color?: string
  fileCount: number
  bundledCount: number
  resolvedCount: number
  launcherName?: string
  // iconDataUrl — вбудована іконка збірки (data URL) для прев'ю у формі.
  iconDataUrl?: string
}

export interface ExportShaurmpackRequest {
  packId: string
  name: string
  version?: string
  description?: string
  authors?: string[]
  outDir: string
  // included/excluded — той самий вибір папок/файлів, що й для mrpack
  // (ExportPackRequest). Без них бекенд пакував усю теку збірки, ігноруючи
  // дерево вибору в модалці експорту.
  included?: string[]
  excluded?: string[]
  force?: boolean
}

export interface ExportShaurmpackResult {
  outPath: string
  conflict?: boolean
  totalFiles: number
  bundledFiles: number
  resolvedFiles: number
}

// ── .shaurmbundle — груповий формат: ОДНА збірка у файлі — це .shaurmpack,
// а .shaurmbundle містить КІЛЬКА збірок (експорт цілої папки «Мої збірки»
// / бекап набору). Прев'ю читає bundle.json БЕЗ розпаковування (швидко),
// імпорт розпаковує кожну збірку в окрему теку та реєструє її. ──
export interface ShaurmbundlePreviewPack {
  packId: string
  name: string
  description?: string
  color?: string
  mcVersion: string
  loader: string
  loaderVersion?: string
  fileCount: number
  bundledCount: number
  resolvedCount: number
}

export interface ShaurmbundlePreview {
  format: string
  description?: string
  launcherName?: string
  createdAt?: string
  packCount: number
  totalFiles: number
  bundledFiles: number
  resolvedFiles: number
  packs: ShaurmbundlePreviewPack[]
}

export interface ShaurmbundleImportPack {
  packId: string
  name: string
}

export interface ShaurmbundleImportResult {
  packs: ShaurmbundleImportPack[]
  totalFiles: number
  filesCopied: number
  folderId?: string
}

export interface ExportShaurmbundleRequest {
  folderId: string
  outDir: string
  name: string
  force?: boolean
}

export interface ExportShaurmbundleResult {
  outPath: string
  conflict?: boolean
  packCount: number
  totalFiles: number
  totalSize: number
}

// ── Файловий провідник збірки ─────────────────────────────────────────

export interface PackFileEntry {
  name: string
  path: string // відносно .minecraft, зі слешами
  isDir: boolean
  size: number
  modified: string // RFC3339
}

export interface PackDirListing {
  rootId: string
  path: string
  entries: PackFileEntry[]
}

export interface PackFileContent {
  content: string
  size: number
  binary: boolean
  mime?: string
}

// ── Галерея скріншотів збірки ──────────────────────────────────────────

export interface ScreenshotEntry {
  fileName: string
  size: number
  modified: string // RFC3339
  modifiedTs: number // unix-мс
  title?: string
  description?: string
  width?: number
  height?: number
}

// Подія 'gallery:new-screenshot' — новий скріншот запущеної збірки
// (плаваючий віджет знизу справа, NewScreenshotWidget.svelte).
export interface NewScreenshotEvent {
  packId: string
  packName: string
  entry: ScreenshotEntry
}

// ── Консоль (перемикач акаунтів) ──────────────────────────────────────

export interface ConsoleAccountInfo {
  accountId: string
  accountName: string
  uuid: string
  licensed: boolean
  running: boolean
}

// ── Гардероб ─────────────────────────────────────────────────────────

export interface SkinPreset {
  id: string
  name: string
  skinPath: string
  slimArms: boolean
  capePath?: string
  capeUrl?: string
  source: string // "local" | "mojang-copy" | "default"
  sourceNick?: string
  createdAt: number
}

// Стан повернення скіна збірки після зупинки гри (бекенд → UI).
// status: "none" | "applied" | "restoring" | "done" | "failed" | "pending"
export interface SkinRestoreStatus {
  buildId: string
  accountId: string
  status: string
  attempt?: number
  maxAttempts?: number
  error?: string
  restoreOnExit: boolean
}

// ── Акаунт Шаурми (окремий для КОЖНОГО акаунта лаунчера) ──────────────
// Що UI знає про акаунт Шаурми активного акаунта лаунчера. Тип спільний для
// обох редакцій (у base-збірці hasAccount завжди false).
export interface ShaurmaAccountInfo {
  hasAccount: boolean
  accountId?: string
  nickname?: string
  launcherAccountId?: string
  launcherNick?: string
  isLicensed: boolean
  type?: 'licensed' | 'unlicensed'
  uuid?: string
  createdAt?: number
  nickLocked: boolean
  canUpgrade: boolean
  reason?: 'no_launcher_account' | 'no_hwid'
}

// Профіль акаунта Шаурми для сторінки «Акаунт»: серверні дані (тип, UUID,
// дата створення, ліміт змін ніка) + локальна статистика лаунчера. Скін тут
// НЕ показується — ним керує гардероб.
export interface ShaurmaProfile {
  hasAccount: boolean
  accountId?: string
  nickname?: string
  type?: 'licensed' | 'unlicensed'
  licensed: boolean
  uuid?: string
  createdAt?: number
  nickLocked: boolean
  nickChangesLeft: number
  nickResetAt?: number
  /** Публічний @handle для пошуку друзів (без «@»). */
  handle?: string
  /** unix мс, коли handle можна змінити знову (0/відсутнє — зараз). */
  handleNextChangeAt?: number
  /** Біо (до 120 символів) і колір фону профілю (#RRGGBB); порожні — не задано. */
  bio?: string
  color?: string
  launcherAccountId?: string
  launcherNick?: string
  canUpgrade: boolean
  gameSeconds: number
  buildsCount: number
  friends: number
  reason?: 'no_launcher_account' | 'no_hwid' | 'no_shaurma_account'
}

// ── Спільна система скінів обох редакцій ────────────────────────────
// Стан сесії скін-системи (бекенд → UI).
export interface SkinAuthStatus {
  loggedIn: boolean
  email?: string
  nickname?: string
  expiresAt?: number
  expired?: boolean
}

// Відповідь CheckSkinNickname — перевірка ніка ДО Google-логіну (без
// JWT/сесії на бекенді, дешевий GET на воркер). reason заповнений лише
// коли available=false: 'taken' (зайнятий іншим) або 'invalid' (формат).
// mojangOwned=true — нік є в реєстрі Mojang (належить ліцензійному
// акаунту). Нік зареєструвати можна, але пріоритет ліцензійного: у грі
// показується ЙОГО скін з Mojang (worker/yggdrasil.js), а завантаження
// скіна під таким ніком сервер відхиляє 409 (worker/skin.js). Фронт
// показує попередження заздалегідь — до Google-логіну.
export interface NicknameCheckResult {
  available: boolean
  reason?: 'taken' | 'invalid'
  message?: string
  mojangOwned?: boolean
  mojangName?: string
}

// Результат App.RenameOfflineAccount (backend/app.go): акаунт уже з НОВИМ
// ID/UUID + окреме попередження про скін-нік (не помилка: локальне
// перейменування вже виконано, фронт зобов'язаний прийняти новий ID навіть
// із попередженням).
export interface RenameOfflineAccountResult {
  account: Account
  skinWarning?: string
}


// ── Експорт збірки у .mrpack / .zip ────────────────────────────────────
export interface ExportCandidate {
  path: string            // відносно .minecraft, Unix '/'
  type: 'directory' | 'file'
  size?: number
  modified?: number
  defaultSelected: boolean
}

export interface ExportPackRequest {
  packId: string
  name: string            // назва файлу (без розширення)
  version?: string        // mrpack versionId
  summary?: string
  formats: ('mrpack' | 'zip' | 'curseforge')[]
  outDir: string
  included: string[]
  excluded: string[]
  force?: boolean
}

export interface ExportPackResult {
  files?: string[]
  conflicts?: string[]
}

// Подія export:progress (бекенд шле на кожен файл).
export interface ExportProgress {
  packId: string
  stage: 'mod_sources' | 'scan' | 'hash' | 'resolve' | 'pack' | 'done'
  current: number
  total: number
  fileName: string
  modName?: string
  iconUrl?: string
  percent?: number
}

// ── Дублювання збірки (DuplicatePackModal) ──────────────────────────────
// Копія збірки з вибором файлів (як в експорті), іконкою/кольором,
// назвою/ID та опційним копіюванням налаштувань.

export interface DuplicatePackRequest {
  sourcePackId: string
  // Бажаний ID копії (slug). Порожній/зайнятий — бекенд підбере вільний.
  newId: string
  // Назва копії. Порожня → "<Назва> (2)" / "(3)"…
  newName: string
  description: string
  // Іконка/колір: copyIcon — скопіювати іконку джерела; інакше — Icon
  // (пресет) + Color (з useCustomColor). iconSrcPath — свій файл (пріоритет).
  copyIcon: boolean
  iconSrcPath: string
  icon: string
  useCustomColor: boolean
  color: string
  // Скопіювати налаштування збірки (per-instance конфіг: RAM, JVM, команди,
  // автоприєднання, скін збірки, закріплені сервери).
  copySettings: boolean
  // Вибір файлів (той самий контракт, що в експорті).
  included: string[]
  excluded: string[]
}

export interface DuplicatePackResult {
  pack: CustomPack
  filesCopied: number
}

// Перевірка ID перед дублюванням (CheckDuplicatePackID).
export interface DuplicatePackIDCheck {
  id: string
  exists: boolean
  suggested: string
}

// Подія duplicate:progress (бекенд шле на кожен скопійований файл).
export interface DuplicateProgress {
  stage: 'copy' | 'done'
  current: number
  total: number
  fileName?: string
}

export interface CapeInfo {
  id: string
  alias: string
  url: string
  active: boolean
}

export interface PlayerProfile {
  nickname: string
  uuid: string
  slimArms: boolean
  skinUrl: string
  capeUrl?: string
}

export interface ActiveLook {
  skinDataUrl: string
  capeDataUrl?: string
  slimArms: boolean
}

// ── Аналітика ігрового часу («Мої збірки» → Аналітика) ────────────────

// AnalyticsPoint — один стовпчик діаграми. start — ISO дата початку бакета
// (день/тиждень/місяць: "2026-08-06"; рік: "2026"); секунди гри в ньому.
// Підпис бакета (день тижня, короткий місяць тощо) локалізується на
// фронтенді через Intl.DateTimeFormat — бекенд віддає лише сиру дату.
export interface AnalyticsPoint {
  start: string
  seconds: number
}

// AnalyticsInstance — агрегат по одній збірці для топу «найбільше грав».
// Назва/іконка/колір збірки домішуються з BuildView на фронтенді.
export interface AnalyticsInstance {
  instanceId: string
  seconds: number
  sessions: number
}

// ── Тихий журнал помилок лаунчера (launcher_errors.go) ────────────────
// Один запис: час (RFC3339), джерело (звідки прийшла помилка), суть і
// опціональна деталь (stack/raw error). Фронтенд передає сюди JS-помилки
// (unhandledrejection / window.onerror / catch), бекенд — свої.
export interface LauncherErrorEntry {
  time: string
  source: string
  message: string
  detail?: string
}

// AnalyticsSummary — повний знімок аналітики ігрового часу: підсумки +
// три серії діаграм (тижні/місяці/роки) + топ збірок за одним викликом.
export interface AnalyticsSummary {
  totalSeconds: number
  totalSessions: number
  playedInstances: number
  avgSessionSeconds: number
  last7Days: AnalyticsPoint[]
  weeks: AnalyticsPoint[]
  months: AnalyticsPoint[]
  years: AnalyticsPoint[]
  topInstances: AnalyticsInstance[]
}

// ── Pack Doctor (Health Check) ───────────────────────────────────────────

export type HealthCheckSeverity = 'ok' | 'warning' | 'error'

export interface HealthCheckItem {
  id: string
  severity: HealthCheckSeverity
  title: string
  detail?: string
  fixable: boolean
  fixActionId?: string
}

export interface HealthCheckReport {
  packId: string
  items: HealthCheckItem[]
  overall: HealthCheckSeverity
}

// ── Additional types not in linux-bridge copy ──────────────────────────

// BrowserInstalledInPack — одна збірка користувача, у якій знайдено
// поточний проєкт (сторінка контенту: «Також встановлено у: ...»).
export interface BrowserInstalledInPack {
  packId: string
  packName: string
  versionNumber: string
}

// ── Авто-збірки «Vanilla / Snapshot» ───────────────────────────────────
// Дві кастомні збірки, які лаунчер створює автоматично після першого
// майстра і які самі стежать за найновішими версіями Minecraft:
// «Vanilla» — за стабільними релізами, «Snapshot» — за тижневими
// снапшотами Mojang. Конфіг збірки перемикається на нову версію сам,
// файли качаються по кнопці «Завантажити» (RefreshAutoPackFiles).
export interface AutoPackView {
  // kind: 'release' | 'snapshot'
  kind: string
  id: string
  name: string
  enabled: boolean
  created: boolean
  mcVersion?: string
  loader?: string
  iconUrl?: string
  icon?: string
  color?: string
  targetVersion?: string
  hasNewVersion?: boolean
}

// ── Закладки та колекції (групи контенту) ────────────────────────────

// BrowserCollection — груповий набір контенту («тег-архів»): гравець
// складає пачку модів/ресурспаків/шейдерів/датапаків один раз і потім
// встановлює її однією кнопкою в будь-яку сумісну збірку.
export interface BrowserCollection {
  id: string
  name: string
  description?: string
  icon?: string
  color?: string
  itemCount: number
  createdAt: string
  updatedAt: string
}

// CollectionItemVersion — версія, ЗАКРІПЛЕНА за конкретним записом
// колекції (на відміну від звичайного пошуку, де завжди береться
// найновіша сумісна версія на момент встановлення).
export interface CollectionItemVersion {
  versionId?: string
  versionNumber?: string
}

// CollectionInstallPlanItem — один рядок плану групового встановлення
// (PlanCollectionInstall), що показується ПЕРЕД реальним встановленням:
// що станеться з кожним записом колекції і чому.
export interface CollectionInstallPlanItem {
  source: string
  projectId: string
  name: string
  projectType: string
  versionId?: string
  versionNumber?: string
  // status: 'install' | 'update' | 'already' | 'incompatible'
  status: string
  message?: string
}

// ── Тимчасові моди («становлення на строк») ──────────────────────────

// TempModInfo — тимчасова мітка одного мода: коли і як (видалити чи
// вимкнути) лаунчер автоматично прибере його зі збірки.
export interface TempModInfo {
  fileName: string
  expiresAt: string // RFC3339
  action: string // 'delete' | 'disable'
  addedByDependency?: boolean
  requiredBy?: string
  secondsRemaining: number
}

// ── Історія пошукових запитів браузера контенту ──────────────────────

// SearchHistoryItem — один запис історії: запит, що дав результат.
export interface SearchHistoryItem {
  query: string
  projectType: string
  resultCount: number
  searchedAt: string // RFC3339
}

// ── Сервери (Server Manager) ─────────────────────────────────────────

export interface ServerView {
  id: string
  name: string
  dir: string
  core: string
  coreLabel: string
  mcVersion: string
  loaderVer: string
  javaMajor: number
  javaInstalled: boolean
  coreInstalled: boolean
  ramMB: number
  port: number
  jvmArgs?: string
  programArgs?: string
  notes?: string
  status: string
  pid?: number
  uptimeSec?: number
  exitCode?: number
  // recovered — сервер пережив краш/перезапуск лаунчера: процес живий,
  // керування відновлено з runtime-запису (recover.go в бекенді).
  // Консоль і команди в такому разі недоступні (pipe утрачено).
  recovered?: boolean
  // iconData — ПРОТОКОЛЬНА іконка (server-icon.png 64×64), яку бачать гравці
  // у списку серверів у самій грі.
  iconData?: string
  // icon — пресет-іконка ЛАУНЧЕРА (Tabler, напр. "ti-server-2").
  icon?: string
  // launcherIconData — PNG/зображення іконки ЛАУНЧЕРА (окремий файл у
  // теці сервера, без ресайзу до 64×64). Пріоритетне над icon.
  launcherIconData?: string
  color?: string
  networkId?: string
  networkRole?: string
  hasWorlds: boolean
  hasMods: boolean
  hasPlugins: boolean
  lastError?: string
  createdAt: number
  autoBackup: ServerBackupConfig
  // Автозапуск P2P-тунеля разом із сервером (перемикач у «Мережі»).
  autoStartP2P?: boolean
}

export interface ServerBackupConfig {
  enabled: boolean
  intervalMin: number
  keepCount: number
  keepDays: number
  // maxPerDay — скільки авто-бекапів на добу максимум (0 = без ліміту).
  maxPerDay: number
  worldDir: string
  // backupOnStop — бекап світів після ЗВИЧАЙНОЇ зупинки сервера (найцінніший
  // знімок: Java вже закрила регіони). Kill цю гілку не тригерить.
  backupOnStop?: boolean
  // backupOnStart — бекап світів при старті (знімок «як було до сесії»).
  backupOnStart?: boolean
}

// ── Розумний аналізатор запуску сервера ─────────────────────────────────
// Дзеркало backend/internal/servers/preflight.go: перелік того, що заважає
// або зламає старт (порт/RAM/Java/EULA/диск/ядро/минулий краш).
export interface ServerWarning {
  kind: string
  level: 'error' | 'warn' | 'info'
  message: string
  hint?: string
  fatal: boolean
  ignorable: boolean
}

export interface ServerPreflight {
  warnings: ServerWarning[]
  canStart: boolean
  checkedAt: number
}

export interface NewServerRequest {
  core: string
  mcVersion: string
  loaderVer: string
  name: string
  // id — власний ID сервера (ім'я теки). Порожньо = виведеться з назви.
  // Дозволено кирилицю та пробіли (backend: SanitizeServerFolder).
  id?: string
  dir: string
  ramMB: number
  port: number
  acceptEula: boolean
  javaMajor: number
  jvmArgs: string
  programArgs: string
}

// ServerFolderCheck — стан ID сервера (ім'я теки на диску): як він
// виглядатиме після санітизації і чи не зайнятий іншим сервером.
// Дзеркало backend/internal/servers.ServerFolderCheck.
export interface ServerFolderCheck {
  folder: string
  free: boolean
  reason?: string
}

export interface CoreDetectResult {
  core: string
  mcVersion: string
  loaderVer: string
  javaMajor: number
  port: number
  confidence: 'high' | 'medium' | 'low'
  messages: string[]
  serverJar?: string
  installerJar?: string
}

// ChangeCoreRequest — зміна ядра/версії гри/лоадера існуючого сервера.
// Порожні поля = «лишити як є».
export interface ChangeCoreRequest {
  core?: string
  mcVersion?: string
  loaderVer?: string
  // 0 = авто за версією гри
  javaMajor?: number
}

export interface ServerConfigPatch {
  name?: string
  ramMB?: number
  jvmArgs?: string
  programArgs?: string
  port?: number
  // icon — пресет-іконка лаунчера. Не чіпаємо, якщо не передано (undefined);
  // порожній рядок = прибрати пресет.
  icon?: string
  color?: string
  notes?: string
  javaMajor?: number
  // Авто-бекапи (окремий об'єкт — зберігається карткою «Світи»).
  autoBackup?: ServerBackupConfig
  // Піднімати P2P-тунель разом із сервером.
  autoStartP2P?: boolean
}

// ServerCoreVersionEntry — одна версія ядра сервера (Paper build, Forge
// версія тощо) для селектора у створенні сервера — та сама форма даних,
// що LoaderVersionEntry у створенні збірки (аліас для ясності на фронтенді).
export type ServerCoreVersionEntry = LoaderVersionEntry

export interface ServerStatus {
  status: string
  pid: number
  uptimeSec: number
  exitCode: number
  lastError: string
}

export interface ServerLiveStats {
  ramMB: number
  cpuPercent: number
  // Пам'ять КУПИ JVM (з GC-звіту самого JVM, backend/internal/servers/stats.go
  // LiveStats) — це те, скільки сервер РЕАЛЬНО ВИКОРИСТОВУЄ під об'єкти, на
  // відміну від ramMB (RSS процесу): RSS показує, скільки ОС виділила/
  // закомітила java.exe, і завжди помітно більший за фактичне використання
  // (JVM тримає резерв, метадані, off-heap буфери тощо) — тому показувати
  // гравцю RSS як «використання RAM» вводить в оману: сервер із -Xmx 4 ГБ і
  // легким навантаженням показував би «3.8/4 ГБ використано», хоча реально
  // зайнята купа — 900 МБ.
  heapUsedMB: number
  heapCommittedMB: number
  heapMaxMB: number
  // heapSource: "jvm" — дані з реального GC-логу (надійні, показуємо купу);
  // "rss" — GC-лог ще порожній (сервер щойно стартував, жодного збирання
  // ще не було) — чесно показуємо RSS як тимчасовий замінник, а не купу
  // нуля/невідомості.
  heapSource: 'jvm' | 'rss'
  gcCount: number
  gcLastPauseMs: number
  gcPauseTotalMs: number
  gcLastAt: number
  playerCount: number
  maxPlayers: number
  uptimeSec: number
  pid: number
}

export interface CheckPortResult {
  free: boolean
  // holder — хто займає («PID 1234», «наш сервер «X»»).
  holder?: string
  // ourServer — ID нашого сервера, якщо порт зайнято саме ним.
  ourServer?: string
}

// Дзеркало backend/internal/servers/diagnostics.go CheckRAMResult.
// ВАЖЛИВО: поля саме такі (available/total/...), а НЕ availableMB —
// раніше тип містив availableMB, якого бекенд ніколи не надсилав, тому
// попередження у майстрі показувало «Доступно лише NaN ГБ».
export interface CheckRAMResult {
  ok: boolean
  available: number
  total: number
  usedByLauncher: number
  usedByServers: number
}

// ── P2P ("LAN через інтернет") ──────────────────────────────────────────
// Адреса = базова назва + випадковий 6-значний цифровий суфікс, який
// Worker генерує сам (напр. "prlool-482917") — суфікс слугує одночасно
// і унікальним ідентифікатором, і захистом від випадкового підключення:
// щоб приєднатись, гість має ввести ПОВНУ адресу в Minecraft Direct
// Connect, включно з суфіксом.
export interface ClaimResult {
  subdomain: string // повна адреса з суфіксом, напр. "prlool-482917"
  address: string   // готове для показу "<subdomain>.play.shaurma-launcher.org"
  // reused — адресу взято з локальної книги (вона вже була застовблена
  // цим акаунтом), а не зайнято заново. UI може написати «адреса вже ваша».
  reused?: boolean
}

// P2PClaimedAddress — адреса, зайнята цим акаунтом і збережена локально
// (backend/internal/p2p/store.go). Саме тому її не треба займати щораз
// заново після перезапуску лаунчера.
export interface P2PClaimedAddress {
  subdomain: string
  baseName: string
  accountId?: string
  label?: string
  createdAt: number
  lastUsedAt: number
}

export type P2PAccess = 'all' | 'accounts' | 'friends' | 'list'
// P2PEntry — запис списку: повний id акаунта АБО кастомний handle (VPS зводить його до id). Нік не передаємо.
export interface P2PEntry { id?: string; handle?: string; nick?: string; kind?: 'allow' | 'deny' }
export interface P2PEntryResult { key: string; op: string; status: string }
export interface P2PApplyResult { ok: boolean; results: P2PEntryResult[] }
export interface P2PACLItem { id: string; nick: string; handle?: string; color?: string }
export interface P2PACLList { allow: P2PACLItem[]; deny: P2PACLItem[] }
export interface P2PCohostRow { id: string; nick: string; handle?: string; color?: string; addedAt: number }
export interface P2PCohostList {
  cohosts: P2PCohostRow[]
  host: { accountId: string; nick: string; leaseExpiresAt: number } | null
  max: number
}
export interface P2PSharedRoom {
  subdomain: string; address: string; ownerNick: string; access: string
  // Хто дав цю адресу (щоб було видно, що вона чужа): id, @handle, колір профілю власника.
  ownerId?: string; ownerHandle?: string; ownerColor?: string
  disabled: boolean; busy: boolean; mine: boolean; hostNick: string
}

export interface P2PHostStatus {
  access?: string
  active: boolean
  // subdomain — повна адреса без суфікса .play... Потрібна, щоб зупиняти
  // саме цю картку (мультихостинг: кілька серверів одночасно).
  subdomain?: string
  address: string // "<subdomain>.play.shaurma-launcher.org"
  // udpPort — порт, на якому РЕАЛЬНО піднявся UDP-тунель голосового чату
  // ('' = голос через P2P не тунелюється). Порт дорівнює порту гри
  // (однаковість портів), а локально форвардиться в порт голосового моду.
  udpPort?: string
  udpError?: string
  // playerCount — скільки гостей зараз під'єднано (бейдж на картці).
  playerCount?: number
  // disabled — тунель вимкнено адміном із Pack Manager.
  disabled?: boolean
  error?: string
  activeCount?: number
  maxCount?: number
}

// P2PConnection — гість, під'єднаний до хост-сесії (backend:
// App.HostP2PConnections). Власник бачить, хто саме зайшов і який пінг.
export interface P2PConnection {
  id: string
  guestIp?: string
  remoteAddr: string
  connectedAt: number
  lastSeen: number
  bytesIn: number
  bytesOut: number
  pingMs: number
}

// P2PUDPStatus — стан UDP-тунелю голосового чату (backend:
// App.GetP2PUDPStatus). reason заповнений, коли галочка стоїть, але голос
// поки нікуди не йде (напр. гравець ще не заходив у P2P-кімнату — в UDP
// немає хендшейку, тож адреса хоста береться з останнього TCP-з'єднання).
export interface P2PUDPStatus {
  enabled: boolean
  voicePort: number
  // tunnelPort — порт тунелю (дорівнює порту сервера хоста).
  tunnelPort?: number
  roomAddress?: string
  reason?: string
}

// P2PStatus — чи може P2P працювати взагалі: чи ввімкнена фіча в
// налаштуваннях лаунчера і чи налаштований Headscale на сервері.
// Потрібен, щоб UI показував пояснення замість кнопок, які гарантовано
// впадуть (backend: App.GetP2PStatus).
export interface P2PStatus {
  configured: boolean
  roomDomain?: string
  reason?: string
  disabledByUser: boolean
  // accountOk — чи є активний Shaurma/ліцензійний акаунт. P2P прив'язує
  // зайняті адреси до акаунта, тож без нього фіча недоступна.
  accountOk: boolean
  accountReason?: string
}

export interface PropEntry {
  key: string
  value: string
  // int | bool | string | enum — тип зі схеми бекенду (невідомі ключі: виводиться зі значення).
  type: string
  description?: string
  hint?: string
  comment?: string
  enum?: string[]
  // Межі int-значень. Відсутня межа = без обмеження з цього боку.
  min?: number
  max?: number
  default?: string
  // id групи (props.group.<id>); для ключів поза схемою — "other".
  group?: string
  // true — ключ описано у схемі бекенду.
  known?: boolean
  // Одиниця виміру («чанків», «мс»…) для підпису поруч із полем.
  unit?: string
  slider?: boolean
  step?: number
  // Значення маскується (паролі, токени).
  secret?: boolean
  // Читається лише при створенні світу.
  newWorldOnly?: boolean
  // Enum, що дозволяє власні значення (плагіни додають типи світу).
  open?: boolean
  since?: string
  until?: string
  // "removed" | "new" — ключ не діє у версії цього сервера.
  status?: string
  // Позиція у схемі — для стабільного порядку показу.
  order?: number
}

export interface PropertiesDoc {
  entries: PropEntry[]
  raw: string
  mcVersion?: string
}

export interface PropUpdate {
  key: string
  value: string
}

export interface LinkedPack {
  packId: string
  launchWithPack: boolean
  stopWithPack: boolean
}

export interface ServerPresetIcon {
  name: string
  color: string
  icon: string
}

// ── Контент сервера: моди / плагіни / світи / бекапи ────────────────────
export interface ServerModInfo {
  name: string
  version: string
  fileName: string   // реальне ім'я на диску (.disabled присутній у вимкненого)
  enabled: boolean
  sizeMB: number
  // Файл походить із «єдиного джерела» (див. ModSourcesDialog).
  sourceKind?: ModSourceKind
  sourcePath?: string
  sourceMissing?: boolean
  source?: string    // "modrinth" | "curseforge" | "local"
}

export type ServerPluginInfo = ServerModInfo // той самий формат, що й моди

export interface WorldInfo {
  name: string
  path: string
  sizeMB: number
  modified: string   // RFC3339
}

export interface BackupInfo {
  id: string
  name: string
  sizeMB: number
  created: string    // RFC3339
  world: string
  // files — скільки файлів в архіві (0/відсутнє = невідомо).
  files?: number
  // auto — копія створена автоматично (таймер / зупинка / старт).
  auto?: boolean
}

// ServerBackupProgress — дзеркало backend/internal/servers/worlds.go
// BackupProgress. Приходить подією "server:backup-progress" на кожному кроці
// створення бекапу (і ручного, і авто), щоб UI малював прогрес-бар.
export interface ServerBackupProgress {
  serverId: string
  world: string
  // stage: preparing | packing | done | error
  stage: string
  files: number
  done: number
  percent: number   // 0-100, -1 = невідомо
  message: string
  path?: string
  error?: string
  auto?: boolean
}

// ServerPlayer — гравець, який зараз на сервері (App.GetServerPlayers).
export interface ServerPlayer {
  name: string
  joinedAt?: number
  source?: string
}

// ServerPlayerPlaytime — накопичений час гри гравця на сервері
// (App.GetServerPlayerPlaytimes, з <світ>/stats/*.json).
export interface ServerPlayerPlaytime {
  name: string
  uuid: string
  seconds: number
  lastSeen?: number
  online?: boolean
  sessionStart?: number
}

// ServerPlayerEvent — подія "server:player" (вхід/вихід гравця).
// action === 'reset' приходить при зупинці сервера — UI чистить список.
export interface ServerPlayerEvent {
  serverId: string
  name: string
  action: 'join' | 'leave' | 'reset'
  at: number
  count: number
  reason?: string
  list: string[]
}

export interface CrashAnalysis {
  hasCrash: boolean
  fileName: string
  error: string
  analysis: string
  cacheHit: boolean
}

// ── Прогрес встановлення ядра сервера (server:install-progress) ─────────
// Дзеркалює структуру LaunchProgress для клієнтського запуску.
// Стадії: "java" → "core" → done (відсутній, бо onDone = подія status).
// percent: 0-100 = реальний % (завантаження jar за Content-Length),
//          -1 = стадія без шкали (зовнішній процес java -jar installer.jar).
// ServerBrowserContextView — контекст сервера для вкладки «Знайти онлайн»
// (сторінка сервера → Моди/Плагіни): версія MC і лоадер, під які шукаємо
// сумісний контент, плюс уже встановлені імена файлів для бейджів.
export interface ServerBrowserContextView {
  serverId: string
  mcVersion: string
  core: string
  // Порожній рядок = цей тип контенту серверу не підходить (ванільне ядро
  // не запустить ні мод, ні плагін).
  modLoader: string
  pluginLoader: string
  installedMods: string[]
  installedPlugins: string[]
}

export interface ServerInstallProgress {
  stage: string    // "java" | "core"
  percent: number  // 0-100 або -1
  message: string
}

// ── Мережа Velocity (проксі + backend-сервери) ──────────────────────────
// Управління через Velocity-сервер: він є «центром» своєї мережі, а склад
// мережі зберігається у <папка проксі>/.shaurma/network.json.

// Мережа Velocity (backend/internal/servers/network*.go).
export interface NetworkMemberView {
  serverId: string
  name: string
  core: string
  mcVersion: string
  port: number         // актуальний порт (з server.properties)
  running: boolean
  status: string
  inNetwork: boolean   // учасник мережі?
  order: number        // порядок запуску й входу
  delaySec: number     // пауза перед стартом
  alias: string        // ім'я в мережі (/server <alias>)
  autoStart: boolean   // стартує разом із проксі
  startsNetwork: boolean // його запуск піднімає проксі
  inTry: boolean       // приймає гравців при вході
  forcedHost: string   // домен прямого входу на цей сервер
  otherNetwork?: string // уже в мережі іншого проксі
  forwarding: '' | 'paper' | 'fabric-proxy' | 'pcf'
}

export interface ProxySettings {
  motd: string
  showMaxPlayers: number
  onlineMode: boolean
  forceKeyAuthentication: boolean
  preventClientProxyConnections: boolean
  forwardingMode: 'modern' | 'legacy' | 'bungeeguard' | 'none'
  announceForge: boolean
  kickExistingPlayers: boolean
  pingPassthrough: 'disabled' | 'mods' | 'description' | 'all'
  failoverOnUnexpectedDisconnect: boolean
  compressionThreshold: number
  compressionLevel: number
  loginRatelimit: number
  connectionTimeout: number
  readTimeout: number
  haproxyProtocol: boolean
  tcpFastOpen: boolean
  logCommandExecutions: boolean
  logPlayerConnections: boolean
  queryEnabled: boolean
  queryPort: number
}

export interface NetworkSettings {
  proxyPort: number
  proxy: ProxySettings
  stopMembersWithProxy: boolean
  lockBackends: boolean
}

export interface NetworkView {
  proxyServerId: string
  proxyName: string
  proxyPort: number
  proxyRunning: boolean
  proxyStatus: string
  networkSaved: boolean
  members: NetworkMemberView[]  // інші сервери (кандидати + учасники)
  external: ExternalServer[]    // сервери мережі поза лаунчером
  settings: NetworkSettings
}

// Запис [servers] velocity.toml, якого немає в лаунчері (інша машина тощо).
export interface ExternalServer {
  alias: string
  address: string // host:порт
  inTry: boolean
  forcedHost?: string
}

// Готова мережа в одній теці (проксі й сервери в підтеках).
export interface NetworkFolderServer {
  dir: string
  name: string
  core: string
  mcVersion: string
  port: number
  known: boolean   // уже доданий у лаунчер
  skip?: string    // чому не буде додано
}
export interface NetworkFolderInfo {
  dir: string
  hasProxy: boolean
  servers: NetworkFolderServer[]
}
export interface NetworkImportResult {
  proxyId: string
  added: number
  errors: string[]
}

export interface NetworkIssue {
  level: 'error' | 'warn' | 'info' | 'ok'
  serverId?: string
  title: string
  detail?: string
  fixable?: boolean
}

// ── Друзі Shaurma (internal/social) ─────────────────────────────────────
export type SocialStatus = 'online' | 'away' | 'in_game' | 'offline'
export type SocialConn = 'off' | 'connecting' | 'online'
export type SocialShowTo = 'everyone' | 'friends' | 'nobody'
export type SocialWhoCanMessage = 'friends' | 'everyone' | 'nobody'
export type SocialFriendRequests = 'everyone' | 'friends_of_friends' | 'nobody'

export interface SocialPresence {
  status: SocialStatus
  packName?: string
  packColor?: string
  updatedAt: number
  // lastSeenAt — unix мс, коли друг востаннє був у мережі (лише для offline).
  // Нема (0/undefined) — друг приховав статус або сервер ще не віддає.
  lastSeenAt?: number
}
// ── Віддалене керування серверами друзів ──────────────────────────────
export interface RemoteServer {
  ownerId: string; ownerOnline: boolean; id: string; name: string; state: string; color?: string; core?: string
  canStart: boolean; canStop: boolean; canConsole?: boolean; canExec?: boolean
  // network — serverId проксі, до мережі якого входить сервер; p2p/p2pAddr — публічна P2P-адреса.
  network?: string; p2p?: boolean; p2pAddr?: string
  // icon — пресет-іконка власника ("ti-…"); img — data URL мініатюри його картинки.
  icon?: string; img?: string
}
// network — лише для проксі: запуск/зупинка діє й на сервери його мережі.
export interface RemoteGrant { accountId: string; start: boolean; stop: boolean; console: boolean; exec: boolean; network?: boolean; added: number }
export interface RemoteServerAccess { serverId: string; allowStopWithPlayers: boolean; allowPrivileged: boolean; grants: RemoteGrant[] }
export interface RemoteAccessView { paused: boolean; servers: RemoteServerAccess[] }
export interface RemoteAuditEntry { ts: number; friend: string; nick: string; server: string; name: string; op: string; result: string; cmd?: string }
export interface RemoteResultEvent { reqId: string; ownerId: string; serverId: string; op: string; sid?: string; ok: boolean; code?: string; state?: string; final: boolean }
export interface RemoteConsoleEvent { sid: string; ownerId: string; serverId: string; lines?: { i: number; t: string; l: string; x: string }[]; end?: boolean; reason?: string }

export interface SocialFriend {
  accountId: string
  nickname: string
  handle?: string
  bio?: string
  color?: string
  since: number
  presence: SocialPresence
}
export interface SocialInvite {
  id: string
  fromAccountId: string
  fromNickname: string
  fromHandle?: string
  createdAt: number
}
export interface SocialPrivacy {
  showStatusTo: SocialShowTo
  showServerTo: SocialShowTo
  friendRequests: SocialFriendRequests
  // Хто може писати мені в чат. Відсутнє (старий бекенд) = 'friends'.
  whoCanMessage?: SocialWhoCanMessage
}

// ChatMuteState — три рівні муту чатів (бекенд chatmute_shaurma.go).
//   all     — загальний мут чатів (повідомлення не сповіщаються);
//   sound   — мут звуку сповіщень чату (звук додається пізніше);
//   muted   — список accountId діалогів, які мутнуті окремо.
export interface ChatMuteState {
  all: boolean
  sound: boolean
  muted: string[]
}
export interface SocialState {
  loggedIn: boolean
  conn: SocialConn
  me?: string
  // meStatus — МІЙ статус присутності з бекенду (online | away | in_game |
  // offline). Використовується віджетом акаунта: підпис показує реальний
  // стан («у грі» / «відійшов» / «у мережі»), а не статичне «В мережі».
  meStatus?: SocialStatus
  friends: SocialFriend[]
  invites: SocialInvite[]
  privacy: SocialPrivacy
  privacyLoaded: boolean
  // Непрочитані повідомлення за accountId друга (лише > 0). Рахує бекенд, тож
  // бейдж працює і коли діалог/шухляда закриті.
  unread?: Record<string, number>
  // Код помилки синхронізації ("network", "not_logged_in", ...). НЕ текст сервера.
  error?: string
  // Технічний код причини збою входу (для екрана «не підключились»).
  diag?: string
}
export interface SocialBlockedAccount {
  accountId: string
  nickname: string
  handle?: string
  since: number
}
export interface SocialUserHit {
  accountId: string
  nickname: string
  handle?: string
}
export interface SocialPeerProfile {
  accountId: string
  nickname: string
  handle?: string
}
// Профіль друга з СЕРВЕРА (GET /friends/profile/{id}). hidden=true — друг закрив профіль:
// біо/колір/статистики немає взагалі (їх не віддає сервер). Відсутнє число = сервер не знає → «—».
export interface SocialFriendProfile {
  accountId: string
  nickname: string
  handle?: string
  since: number
  hidden: boolean
  bio?: string
  color?: string
  createdAt?: number
  gameSeconds?: number
  buildsCount?: number
  friendsCount?: number
}
// Кому я показую свій профіль (біо, колір, статистику). Діє на сервері.
export type SocialProfileVisibility = 'friends' | 'nobody'
export interface SocialPresenceEvent {
  accountId: string
  presence: SocialPresence
}

// ── Чат Shaurma ──────────────────────────────────────────────────
// Сервер бачить лише ciphertext+nonce; plaintext приходить уже розшифрованим
// локально (internal/social). decryptFailed=true — розшифрувати не вдалось.
export interface SocialChatInvite {
  id: string
  serverAddr: string
  packId?: string
  packName?: string
  expiresAt: number
  revoked: boolean
  active: boolean
}
export interface SocialChatMessage {
  id: number
  from: string
  bodyType: 'text' | 'server_invite'
  ciphertext: string
  nonce: string
  createdAt: number
  plaintext?: string
  decryptFailed?: boolean
  invite?: SocialChatInvite
  /** Відповідь на повідомлення (0/відсутнє = ні). */
  replyToId?: number
  /** Час останньої правки тексту (мс); 0/відсутнє — не редагувалось. */
  editedAt?: number
  /** Реакції учасників діалогу (емодзі — відкритий текст). */
  reactions?: SocialReaction[]
}
/** Одна реакція на повідомлення (by = accountId того, хто відреагував). */
export interface SocialReaction {
  by: string
  emoji: string
}
export interface SocialChatPage {
  messages: SocialChatMessage[]
  hasMore: boolean
  // Закріплені діалогу (повні, за порядком закріплення) — лише на найновішій сторінці.
  pinned?: SocialChatMessage[]
  // Курсори «прочитано» (лише найновіша сторінка): до якого id дочитав кожен учасник.
  reads?: { by: string; upTo: number }[]
}
// chat:deleted — повідомлення видалено «для всіх».
export interface SocialDeletedEvent {
  chatId: string
  messageId: number
  by: string
}
// chat:edited — автор змінив текст повідомлення (plaintext уже розшифровано локально).
export interface SocialEditedEvent {
  chatId: string
  messageId: number
  by: string
  editedAt: number
  plaintext?: string
  decryptFailed?: boolean
}
// chat:pins — ПОВНИЙ актуальний список закріплених діалогу.
export interface SocialPinsEvent {
  chatId: string
  pinned: SocialChatMessage[]
  by: string
  action: 'pin' | 'unpin' | 'removed'
  messageId: number
}
export interface SocialChatEvent {
  chatId: string
  from: string
  id: number
  ts: number
  plaintext?: string
  decryptFailed?: boolean
  bodyType?: string
  /** Моє повідомлення, надіслане з іншого мого пристрою (from = я). */
  mine?: boolean
  /** Відповідь на повідомлення (0/відсутнє = ні). */
  replyToId?: number
}
// chat:reactions — ПОВНИЙ актуальний список реакцій одного повідомлення.
export interface SocialReactionsEvent {
  chatId: string
  messageId: number
  reactions: SocialReaction[]
  by: string
  emoji: string
  action: 'set' | 'removed'
}
export interface SocialChatAck {
  clientMessageId: string
  chatId: string
  id: number
  ts: number
}
export interface SocialTypingEvent {
  chatId: string
  accountId: string
}
export interface SocialReadEvent {
  chatId: string
  upToMessageId: number
}
export interface SocialServerInviteEvent {
  chatId: string
  from: string
  serverAddr: string
  packId?: string
  packName?: string
  inviteId: string
}

// ── Віджет сповіщень у трей ──────────────────────────────────────────────
// Окреме вікно (notify.html). Бекенд уже санітизував текст і обрізав його —
// тут ми ЛИШЕ показуємо. kind: "chat" (превʼю повідомлення) |
// "invite" (запит у друзі; body може бути порожнім — підставляє i18n).
export type SocialNotifyKind = 'chat' | 'invite'
export interface SocialNotifyPayload {
  id: string
  kind: SocialNotifyKind
  title: string
  body: string
  accountId: string
  ts: number
  // Час показу САМЕ цієї картки (мс) — бекенд підставляє з налаштувань
  // «Час показу» при постановці в чергу. Відсутнє → вікно бере своє
  // налаштування з GetSettings (запасний варіант).
  durationMs?: number
}


// ── Тимчасові запрошення (tinvite_shaurma.go) ──
export interface LocalServer { port: number; kind: 'lan' | 'server' | 'modded'; motd: string; version: string; players: number; max: number; lan: boolean }
export interface TempInvite { id: string; address: string; subdomain: string; meta: LocalServer; guests: string[]; packId: string; packName: string }
export interface TempInviteState { address: string; id: string; active: boolean; host: boolean; kind: string; motd: string; version: string; players: number; max: number; guests?: string[]; permanent?: boolean; status?: 'online' | 'offline' | 'deleted' }

// ── Авто-починка / дерево залежностей / метрика ресурсів (порт з Nivora) ──

export interface RepairFix {
  id: string
  action: string
  label: string
  steps: string[]
  params: Record<string, unknown>
  recommended: boolean
}

export interface RepairIssue {
  id: string
  title: string
  detail: string
  evidence: string[]
  fixes: RepairFix[]
  manual: string
}

export interface RepairReport {
  exitCode: number | null
  issues: RepairIssue[]
  mutedIssues: string[]
}

export interface RepairFixResult {
  id: string
  ok: boolean
  message: string
  needsLogin?: boolean
}

export interface RepairIssueKind {
  id: string
  title: string
}

export interface RepairReportEvent {
  packId: string
  report: RepairReport
}

// repair:progress — який крок лагодження виконується зараз (довгі кроки
// качають Minecraft/лоадер/Java хвилинами, тож модалка показує прогрес).
export interface RepairProgressEvent {
  packId: string
  done: number
  total: number
  label: string
}

export interface ModGraphDependency {
  id: string
  constraint: string
  optional: boolean
}

export interface ModGraphNode {
  id: string
  file: string
  displayName: string
  version: string
  enabled: boolean
  loader: string
  corrupted: boolean
  /** Опис із маніфесту jar (може бути порожнім). */
  description?: string
  /** Домінантний колір іконки (#RRGGBB), порожній — іконки нема. */
  color?: string
  dependencies: ModGraphDependency[] | null
  dependents: number
  /** Fabric/Quilt-мод, який у цій збірці запускає Sinytra Connector. */
  viaConnector?: boolean
}

export interface ModGraphEdge {
  from: string
  to: string
  optional: boolean
  constraint: string
}

export interface ModGraphMissing {
  id: string
  requiredBy: string
  requiredByAll: string[] | null
  optional: boolean
  disabledFile?: string
}

export interface ModDependencyGraph {
  nodes: ModGraphNode[] | null
  edges: ModGraphEdge[] | null
  missing: ModGraphMissing[] | null
  cycles: string[][] | null
  /** У збірці увімкнено Sinytra Connector (Fabric-моди на Forge/NeoForge). */
  connector?: boolean
}

export interface ModRemovalImpact {
  modId: string
  breaks: string[] | null
  degrades: string[] | null
}

export interface PackMetricSample {
  t: number
  cpu: number
  ramMb: number
  procs: number
}

export interface PackMetrics {
  packId: string
  running: boolean
  startedAt: number
  endedAt: number
  cores: number
  samples: PackMetricSample[] | null
  curCpu: number
  curRamMb: number
  peakCpu: number
  peakRamMb: number
  avgCpu: number
  avgRamMb: number
  diskMb: number
}


// ── P2P: голос + жива діагностика (backend/p2p_voice.go) ──
export interface P2PCheck {
  id: string
  level: 'ok' | 'info' | 'warn' | 'error'
  title: string
  detail?: string
  fix?: 'udp_on' | 'udp_auto' | 'start_p2p' | 'restart_p2p' | 'recheck' | ''
}
export interface VoiceSolution {
  kind: 'svc' | 'plasmo'
  name: string
  where: 'mod' | 'plugin'
  enabled: boolean
  file: string
  configPath?: string
  rawPort: number
  portSet: boolean
  port: number
  sameAsServer: boolean
  bindAddr?: string
}
export interface P2PUDPStats { sessions: number; active: number; pktsOut: number; pktsIn: number }
export interface P2PHostLive {
  active: boolean
  subdomain: string
  address: string
  alive: boolean
  tunnelIp?: string
  localPort?: string
  uptimeSec: number
  guests: number
  access?: string
  disabled?: boolean
  publishedAt?: number
  publishError?: string
  localGameOk: boolean
  localGameErr?: string
  udpPort?: string
  udpError?: string
  udp: P2PUDPStats
  self: { checked: boolean; ok: boolean; at: number; problem?: string }
}
export interface P2PVoiceState {
  serverId: string
  serverPort: number
  subdomain: string
  saved: boolean
  enabled: boolean
  customPort: number
  voice: {
    solutions: VoiceSolution[]
    autoPort: number
    effectivePort: number
    issues: { code: string; severity: 'info' | 'warn' | 'error'; message: string }[]
  }
  host: P2PHostLive
  serverStatus: string
  level: 'off' | 'ok' | 'warn' | 'error'
  headline: string
  checks: P2PCheck[]
  autoStart: boolean
  autoStartError?: string
}

// PackServerLink — сервер, що запускається разом зі збіркою
// (backend/internal/servers/packlink.go).
export interface PackServerLink {
  packId: string
  packName?: string
  serverId: string
  serverName?: string
  serverAddr?: string
  launchWithPack: boolean
  stopWithPack: boolean
  joinOnLaunch: boolean
}
