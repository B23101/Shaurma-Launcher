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
}

export interface Settings {
  language: string
  accent: string
  accentCustom: string
  theme: string
  font: string
  fontPath: string
  autoUpdate: boolean
  updateChannel: string
  closeOnLaunch: boolean
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
  consoleAI: boolean
  showConsoleOnLaunch: boolean
  showConsoleOnCrash: boolean
  showConsoleOnClose: boolean

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
  hideOnGameOpen: boolean
  exitOnGameClose: boolean
  // Показувати підтвердження перед зупинкою запущеної збірки (кнопка
  // «Зупинити»). Вимкнено = зупиняти одразу, без діалогу.
  confirmOnStop: boolean

  // Ігровий час
  showGameTime: boolean
  recordGameTime: boolean
  showTotalGameTime: boolean
  gameTimeInHours: boolean
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

export interface SystemInfo {
  cpuCount: number
  totalRAMMB: number
  freeDiskMB: number
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
}

export interface SettingDef {
  key: string
  type: string // bool | string | int | color | select | accent | language | ram
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
  likes: number
  iconUrl: string
  source: string
}

export interface UpdateInfo {
  available: boolean
  version: string
  downloadUrl: string
  changelogUrl: string
  isCritical: boolean
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
  summary: string
  facts: string[]
  culpritLines: string[]
  rawExcerpt: string
  crashReportFile: string
  currentRamMb: number
  // RepairSuggested — пошкоджені файли оточення (Java/нативки/асети/jar),
  // виправляються кнопкою «Полагодити»; RepairHint — де вона знаходиться
  // (розділ «Продуктивність» на сторінці збірки / прямо в консолі).
  repairSuggested: boolean
  repairHint: string
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

export interface ModEntry {
  id: string
  fileName: string
  name: string
  version: string
  loader: string
  enabled: boolean
  hasUpdate: boolean
  source: string // modrinth | curseforge | unknown
  author?: string
  description?: string
  iconUrl?: string
  iconData?: string
  fileSize?: number
  installedDate?: string
  projectSlug?: string
  projectId?: string
  mcVersion?: string
  latestVersion?: string
  latestUrl?: string
  dependsOn?: string[]
  hasMissingDeps?: boolean
  missingDeps?: string[]
  sha1?: string
  status?: string // checking | done | error
  error?: string
  isDuplicate?: boolean
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
}

export interface ServerPing {
  online: boolean
  version?: string
  protocol?: number
  players?: number
  maxPlayers?: number
  motd?: string
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
}

// ── Архітектура збірок (GetBuilds) ──────────────────────────────────────

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
  serverIp: string
  tags: string[]
}

export interface BuildView {
  build: Build
  status: BuildStatus
  installed: boolean
  progress?: BuildProgress
  runningOn?: string
  error?: string
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
}

export interface BuildStartEvent {
  buildId: string
  accountId: string
  accountName: string
}

export interface BuildExitEvent {
  buildId: string
  accountId: string
  code: number
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
  pinned: boolean
  official: boolean
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
  servers: InstanceServer[]
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
  source: string // manual | import
  importFile: string
  createdAt: string
  updatedAt: string
}

export interface CustomPackFormParams {
  name: string
  description: string
  iconSrcPath: string
  backgroundSrcPath: string
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
}

export type ModpackFormat = 'mrpack' | 'curseforge' | 'generic'

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

// ── Файловий провідник збірки ─────────────────────────────────────────

export interface PackFileEntry {
  name: string
  path: string // відносно .minecraft, зі слешами
  isDir: boolean
  size: number
  modified: string // RFC3339
}

export interface PackDirListing {
  packId: string
  path: string
  entries: PackFileEntry[]
}

export interface PackFileContent {
  content: string
  size: number
  binary: boolean
  mime?: string
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
