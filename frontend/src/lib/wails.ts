// ── Wails v3 frontend shim ──────────────────────────────────────────────
// Замість згенерованих wailsjs/ (v2) або @wailsio/runtime використовуємо
// вбудований JS-runtime, який Wails v3 віддає за адресою /wails/runtime.js.
//
// НОВА АРХІТЕКТУРА (shell.exe + backend.dll): Call.ByName('main.Bridge.Invoke',
// method, argsJson) — ЄДИНИЙ Wails-метод, який реєструє shell.exe РАЗ І
// НАЗАВЖДИ. Він просто пересилає рядок method+argsJson у backend.dll
// (backend_invoke) і повертає рядок назад. Нові методи з'являються ЛИШЕ в
// backend.dll — shell.exe більше ніколи не потребує перекомпіляції заради
// нової фічі фронтенду. Формат відповіді: {"ok":true,"result":..} або
// {"ok":false,"error":..} — розпаковується тут ОДИН РАЗ у call() нижче,
// тому кожен App.X() нижче й далі повертає Promise<T>, як і раніше.
//
// Events.On(name, cb) — підписка на подію. У v3 колбек отримує об'єкт
// WailsEvent {name, data}, тому тут ми розпаковуємо .data, щоб колбеки
// працювали так само, як у v2 (window.runtime.EventsOn).
//
// Application / Window — аналог window.runtime.* методів v2.

import { Call, Events as RtEvents, Application as RtApp, Window as RtWindow } from '/wails/runtime.js'
import type { ShareStartRequest, ShareOutView, ShareInView, ShareRecoverable, SharePreviewView } from './friends/shareMsg'
import type {
  FontFile, RepairReport, RepairFix, RepairFixResult, RepairIssueKind, ModDependencyGraph, ModRemovalImpact, PackMetrics,
  Account, BrowserEntry, AIDiagnosis, AIFixResult, AIRecommendedAction, ConsoleContext, ConsoleLine,
  FolderPaths, InstanceConfig, JavaStatus, PackIndexEntry, ModEntry, BuildView,
  SettingDef, Settings, SetupProgress, StorageAssetsInfo, StorageInfo, StorageLibrariesInfo, StoragePackEntry,
  P2PStatus,
  StorageSuggestion, StorageVersionsInfo, UnusedJavaInfo, SystemInfo, UpdateInfo,
  SkinPreset, CapeInfo, PlayerProfile, ActiveLook, SkinRestoreStatus, SkinAuthStatus, NicknameCheckResult,
  ShaurmaAccountInfo, ShaurmaProfile,
  RenameOfflineAccountResult,
  MCVersionEntry, LoaderVersionEntry, CustomPack, CustomPackFormParams, DetectedPack,
  ShaurmpackPreview, ExportShaurmpackRequest, ExportShaurmpackResult,
  ShaurmbundlePreview, ShaurmbundleImportResult,
  ExportShaurmbundleRequest, ExportShaurmbundleResult,
  PackDirListing, PackFileContent, ConsoleAccountInfo,
  ScreenshotEntry, NewScreenshotEvent,
  ModSourcesView, ModVersionOption, ModBackupEntry, DuplicateModGroup, ModUpdateResult, PreLaunchModIssues, LaunchWarnings, ServerPing,
  SuggestedMod, DependencyResult,
  InstanceServer, ModImportResult, ServerCopyResult, PinnedServerView,
  ServerView, CoreDetectResult, NewServerRequest, ServerConfigPatch, ChangeCoreRequest, ServerStatus, ServerFolderCheck,
  CheckPortResult, CheckRAMResult, PropertiesDoc, PropUpdate, ServerLiveStats, ServerPreflight,
  ServerCoreVersionEntry,
  P2PVoiceState,
  ClaimResult, P2PHostStatus, P2PUDPStatus, P2PClaimedAddress, P2PConnection,
  P2PAccess, P2PEntry, P2PACLList, P2PApplyResult, P2PCohostList, P2PSharedRoom,
  ServerModInfo, ServerPluginInfo, WorldInfo, BackupInfo, ServerPlayer, ServerPlayerPlaytime, ServerBackupProgress, CrashAnalysis, ServerBrowserContextView,
  ServerModsCompareResult,
  NetworkView, NetworkMemberView,
  BrowserSearchResult, BrowserProjectDetail, BrowserVersionView, BrowserCompatiblePack, BrowserInstallResult,
  DesktopShortcut, PendingShortcutLaunch,
  BrowserCategory, BrowserInstalledItem, BrowserInstalledInPack, BrowserProgress, BrowserModpackInstallCheck,
  ContentFileEntry,
  BrowserGameVersion, BrowserLoaderTag,
  BrowserTranslation, BrowserReview, BrowserAuthor,
  MigrationPack, ModMigrationReport, MigrationItem, ModMigrationResult,
  ModCompareReport, CompareInstallRequest,
  CustomPackIDCheck, PackFolder, ServerFolder,
  MinecraftDefaults, PackOptionsView, KeyBindLabel,
  ExportCandidate, ExportPackRequest, ExportPackResult,
  DuplicatePackRequest, DuplicatePackResult, DuplicatePackIDCheck,
  GameLanguage,
  AnalyticsSummary,
  MigrationScanResult, MigrateStatus, MigrateImportRequest, MigrateImportResult,
  DBHealth, ShortcutOptions, ModListExportOptions, LauncherErrorEntry,
  HealthCheckReport, UIState,
  BrowserCollection, CollectionItemVersion, CollectionInstallPlanItem, TempModInfo, SearchHistoryItem,
  AutoPackView, WhitelistEntry, PackWikiDoc,
  SocialState, SocialInvite, SocialPrivacy, SocialBlockedAccount, SocialPeerProfile, SocialFriendProfile, SocialProfileVisibility, SocialUserHit, SocialChatPage, SocialNotifyPayload, ChatMuteState,
  LocalServer, TempInvite, TempInviteState,
  RemoteServer, RemoteAccessView, RemoteAuditEntry
} from './types'

// ЄДИНЕ МІСЦЕ ЗМІНИ у всьому фронтенді: call() тепер іде через
// Bridge.Invoke(method, argsJson). Усе інше (сигнатури App.X, on/emit,
// Application/Window) — БЕЗ ЖОДНИХ ЗМІН, Svelte-компоненти нічого не знають.
async function call(method: string, ...args: unknown[]): Promise<any> {
  const argsJson = JSON.stringify(args)
  const resultJson: string = await Call.ByName('main.Bridge.Invoke', method, argsJson)
  const parsed = JSON.parse(resultJson)
  if (!parsed.ok) {
    const err = new Error(parsed.error ?? `Bridge.Invoke(${method}) failed`)
    // Автолог: кожна помилка App.* (кнопка/дія → метод) тихо пишеться у
    // журнал помилок лаунчера з КОНТЕКСТОМ — ім'ям методу як source. Так
    // «натиснув кнопку, а вона дала помилку» фіксується з місцем і суттю
    // навіть там, де catch у UI не додав AppendLauncherError вручну.
    // Guard: самі логуючі методи не перелогуються (рекурсія).
    //
    // Очікувані помилки: друзі Minecraft недоступні для акаунта без
    // ліцензії (Get/SetMinecraftFriendsSettings) — це стан акаунта, а не
    // баг; UI і так показує зрозумілу помилку. Без скіпа вони шуміли в
    // журналі на кожному заході в налаштування друзів.
    // Друзі Shaurma: «already_friends», «rate_limited» тощо — очікувані відповіді
    // (UI показує переклад за кодом), а не збої лаунчера.
    const skipAutolog = new Set([
      'RemoteStartServer', 'RemoteStopServer', 'RemoteOpenConsole', 'RemoteConsoleExec', 'RemoteCloseConsole',
      'GetFriendsState', 'RefreshFriends', 'SendFriendInvite', 'SendFriendInviteByHandle', 'SetShaurmaHandle', 'SetShaurmaProfileStyle', 'RespondFriendInvite', 'ListSentFriendInvites', 'RemoveFriend', 'SetFriendsPrivacy', 'GetFriendProfile', 'GetProfileVisibility', 'SetProfileVisibility',
    ])
    if (method !== 'AppendLauncherError' && !skipAutolog.has(method)) {
      App.AppendLauncherError(method, err.message, '').catch(() => {})
    }
    throw err
  }
  return parsed.result
}

// withCallTimeout — «сторожовий таймер» навколо виклику App.*.
//
// Навіщо: якщо відповідь з бекенду не приходить ЗОВСІМ (зависла мережа,
// DLL не відповіла, виклик загубився в транспорті), проміс App.X() ніколи
// не завершується — сторінка або список результатів вічно лишаються
// в стані завантаження, без жодної підказки про причину. Обгортка
// гарантує, що місце виклику отримає помилку (і покаже її з кнопкою
// повтору), а сам виклик дозавершиться у фоні — тому місце виклику
// ЗОБОВ'ЯЗАНО захищається від застарілої відповіді (seq/loadId-перевірки).
export function withCallTimeout<T>(p: Promise<T>, ms: number, message: string): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    const timer = setTimeout(() => reject(new Error(message)), ms)
    p.then(
      v => { clearTimeout(timer); resolve(v) },
      e => { clearTimeout(timer); reject(e) },
    )
  })
}

/** Підсумок кроку входу (Google / Microsoft). need_nick — акаунта Шаурми ще нема, треба обрати нік. */
export interface SignInResult {
  status: 'signed_in' | 'need_nick' | 'device_limit'
  provider: 'google' | 'microsoft'
  nickname?: string
  licensed: boolean
  existing: boolean
  noLicense?: 'no_license' | 'no_xbox' | ''
  /** device_limit: в акаунті вже максимум пристроїв — обрати, який вилучити (ReplaceShaurmaDevice). */
  devices?: DeviceChoice[]
  maxDevices?: number
}
/** Пристрій у списку «кого вилучити» при досягнутому ліміті (без IP). */
export interface DeviceChoice { id: string; name: string; clientName: string; os: string; lastSeenAt: number; country: string }
/** Пристрій акаунта (екран «Пристрої»). */
export interface ShaurmaDevice {
  id: string; name: string
  clientKind: 'launcher' | 'browser' | 'unknown' | ''
  clientName: string; clientVersion: string; os: string
  createdAt: number; lastSeenAt: number
  createdIp: string; lastIp: string; createdCountry: string; lastCountry: string
  status: 'active' | 'revoked'; revokedAt: number; revokeReason: string
  current: boolean; online: boolean
}
export interface ShaurmaDeviceList { devices: ShaurmaDevice[]; current: string; max: number; restoreUntil: number }
export interface ShaurmaRevokeOthers { revoked: number; restoreUntil: number }
export interface ShaurmaAuditEvent { id: number; deviceId: string; actorId: string; event: string; detail: string; ip: string; country: string; at: number }
/** Стан ключів чату: ready | pending | need_recovery. */
export interface ChatKeyStatus { state: 'ready' | 'pending' | 'need_recovery'; managed: boolean; versions: number; offline: boolean }
export interface NickCheckResult { available: boolean; reason?: string; message?: string }

/** Типізований доступ до Go-бекенду (main.App). */
export const App = {
  AwaitMSLogin: (): Promise<SignInResult> => call('AwaitMSLogin'),
  SignInGoogle: (): Promise<SignInResult> => call('SignInGoogle'),
  CompleteSignIn: (nickname: string): Promise<SignInResult> => call('CompleteSignIn', nickname),
  CancelSignIn: (): Promise<void> => call('CancelSignIn'),
  CheckShaurmaNick: (nickname: string): Promise<NickCheckResult> => call('CheckShaurmaNick', nickname),
  BrowseFolder: (): Promise<string> => call('BrowseFolder'),
  BrowseFontFile: (): Promise<string> => call('BrowseFontFile'),
  // Шрифти з теки лаунчера <dataDir>/fonts — додаються у списки шрифтів
  // (інтерфейс + консоль), щоб користувач міг обрати покладений TTF/OTF.
  ListFonts: (): Promise<FontFile[]> => call('ListFonts'),
  // Шлях до теки шрифтів лаунчера (для підказки в налаштуваннях).
  GetFontsDir: (): Promise<string> => call('FontsDir'),
  // Відкрити теку шрифтів у Провіднику.
  OpenFontsFolder: (): Promise<void> => call('OpenFontsFolder'),
  OpenCacheFolder: (): Promise<void> => call('OpenCacheFolder'),
  OpenLogsFolder: (): Promise<void> => call('OpenLogsFolder'),
  // ── Імпорт спільнотної мови (JSON) ──
  BrowseLanguageFile: (): Promise<string> => call('BrowseLanguageFile'),
  // LoadLanguageFile повертає ВМІСТ JSON-файлу мови як рядок (WebView не
  // має доступу до довільних шляхів ФС). Фронтенд парсить його і реєструє
  // через registerCustomLanguage.
  LoadLanguageFile: (path: string): Promise<string> => call('LoadLanguageFile', path),
  CancelMSLogin: (): Promise<void> => call('CancelMSLogin'),
  CheckUpdate: (): Promise<UpdateInfo> => call('CheckUpdate'),
  // Ручне оновлення лаунчера (кнопка «Оновити» в налаштуваннях): просить
  // shell перейти у фазу оновлення (run_updater), апдейтер встановлює
  // нову версію безумовно (finalize=true) — навіть при вимкненому
  // автооновленні. Лаунчер сам завершується і перезапускається через
  // апдейтер, тому цей виклик повертається одразу.
  UpdateNow: (): Promise<void> => call('UpdateNow'),
  ClearCache: (): Promise<void> => call('ClearCache'),
  CompleteSetup: (s: Settings): Promise<void> => call('CompleteSetup', s),
  DetectJava: (): Promise<string> => call('DetectJava'),
  DownloadPack: (id: string): Promise<void> => call('DownloadPack', id),
  // М'яка зупинка качки збірки: прогрес зберігається, кнопка стає «Продовжити».
  PauseDownload: (packID: string): Promise<void> => call('PauseDownload', packID),
  // Повне скасування качки: часткові файли видаляються, кнопка стає «Завантажити».
  CancelDownload: (packID: string): Promise<void> => call('CancelDownload', packID),
  // Користувач підтвердив закриття попри активну качку — бекенд паузить
  // синхронізації і закриває вікно. Список активних збірок приходить
  // подією sync:quit-warning, окремий запит не потрібен.
  ConfirmQuitWhileSyncing: (): Promise<void> => call('ConfirmQuitWhileSyncing'),
  // Системний тост Windows (завершення качки/оновлення в неактивному вікні,
  // помилки встановлення) — App.NotifyOS у backend/notify.go.
  NotifyOS: (title: string, message: string): Promise<void> => call('NotifyOS', title, message),
  // Повний ануінстал збірки: файли + реєстр + конфіг + стан синхронізації.
  DeletePack: (id: string): Promise<void> => call('DeletePack', id),
  // ── Авто-збірки «Vanilla / Snapshot» ──
  EnsureAutoVersionPacks: (vanillaIconDataURL: string, snapshotIconDataURL: string, force: boolean): Promise<AutoPackView[]> =>
    call('EnsureAutoVersionPacks', vanillaIconDataURL, snapshotIconDataURL, force),
  GetAutoPacks: (): Promise<AutoPackView[]> => call('GetAutoPacks'),
  SetAutoPackEnabled: (kind: string, enabled: boolean, iconDataURL: string): Promise<void> => call('SetAutoPackEnabled', kind, enabled, iconDataURL),
  RefreshAutoPackFiles: (packId: string): Promise<void> => call('RefreshAutoPackFiles', packId),
  // ── Ярлики збірок на робочому столі (як у Modrinth App) ──
  // Створити/оновити ярлик збірки (<Назва>.lnk з іконкою збірки; подвійний
  // клік запускає лаунчер з --launch <packID>). Ідемпотентно.
  CreateDesktopShortcut: (packID: string): Promise<DesktopShortcut> => call('CreateDesktopShortcut', packID),
  // Гнучкий ярлик (як Prism CreateShortcutDialog): назва + місце
  // ("desktop" | "startmenu" | шлях до теки) + персональні опції запуску
  // ярлика (accountId / autoJoinServer — перекривають акаунт та
  // автоприєднання збірки рівно для запуску з цього ярлика).
  CreateShortcut: (packID: string, opts: ShortcutOptions): Promise<DesktopShortcut> =>
    call('CreateShortcut', packID, opts),
  RemoveDesktopShortcut: (packID: string): Promise<void> => call('RemoveDesktopShortcut', packID),
  HasDesktopShortcut: (packID: string): Promise<boolean> => call('HasDesktopShortcut', packID),
  GetDesktopShortcut: (packID: string): Promise<DesktopShortcut> => call('GetDesktopShortcut', packID),
  // Усі створені ярлики (для менеджера ярликів у налаштуваннях).
  GetAllShortcuts: (): Promise<DesktopShortcut[]> => call('GetAllShortcuts'),
  // Відкрити теку у Провіднику (Windows).
  OpenFolder: (path: string): Promise<void> => call('OpenFolder', path),
  // Експорт списку модів збірки у форматі (html/markdown/plain/json/csv).
  ExportModList: (packID: string, opts: ModListExportOptions): Promise<string> =>
    call('ExportModList', packID, opts),
  // Забрати дані запуску з ярлика (packID + персональні опції ярлика).
  // Фронтенд викликає один раз на маунті — збірка одразу відкриється і
  // запуститься (з опціями ярлика, якщо вони задані).
  TakePendingShortcutLaunch: (): Promise<PendingShortcutLaunch> => call('TakePendingShortcutLaunch'),
  GetAccountHead: (uuid: string, username: string, isLicensed: boolean): Promise<string> => call('GetAccountHead', uuid, username, isLicensed),
  GetAccounts: (): Promise<Account[]> => call('GetAccounts'),
  GetAppDir: (): Promise<string> => call('GetAppDir'),
  // ── Розумна консоль гри ──
  // buildID: вкладка консолі на сторінці збірки показує буфер САМЕ цієї
  // збірки; порожній рядок (окреме вікно) — поточний активний буфер.
  GetConsoleSnapshot: (buildID: string): Promise<ConsoleLine[]> => call('GetConsoleSnapshot', buildID),
  ClearConsoleBuffer: (buildID: string): Promise<void> => call('ClearConsoleBuffer', buildID),
  GetConsoleContext: (instanceID: string): Promise<ConsoleContext> => call('GetConsoleContext', instanceID),
  AnalyzeCrash: (instanceID: string): Promise<AIDiagnosis> => call('AnalyzeCrash', instanceID),
  ApplyAIFix: (instanceID: string, action: AIRecommendedAction): Promise<AIFixResult> => call('ApplyAIFix', instanceID, action),
  SetConsoleRAM: (instanceID: string, ramMB: number): Promise<void> => call('SetConsoleRAM', instanceID, ramMB),
  UploadConsoleLog: (text: string): Promise<string> => call('UploadConsoleLog', text),
  SaveConsoleLog: (text: string): Promise<string> => call('SaveConsoleLog', text),
  RevealPath: (path: string): Promise<void> => call('RevealPath', path),
  OpenInstancesFolder: (): Promise<void> => call('OpenInstancesFolder'),
  // ── Окреме вікно консолі: КОЖНА збірка має СВОЄ вікно (див.
  // console_window.go). Усі методи приймають buildID — вікно конкретної
  // збірки; без buildID (старий виклик) — вікно поточної активної збірки.
  ShowConsoleWindow: (): Promise<void> => call('ShowConsoleWindow'),
  ShowConsoleWindowFor: (buildID: string): Promise<void> => call('ShowConsoleWindowFor', buildID),
  HideConsoleWindow: (): Promise<void> => call('HideConsoleWindow'),
  HideConsoleWindowFor: (buildID: string): Promise<void> => call('HideConsoleWindowFor', buildID),
  ToggleConsoleWindow: (): Promise<void> => call('ToggleConsoleWindow'),
  ToggleConsoleWindowFor: (buildID: string): Promise<void> => call('ToggleConsoleWindowFor', buildID),
  IsConsoleWindowOpen: (): Promise<boolean> => call('IsConsoleWindowOpen'),
  IsConsoleWindowOpenFor: (buildID: string): Promise<boolean> => call('IsConsoleWindowOpen', buildID),
  // Перемикає консоль на конкретну збірку і відкриває вікно консолі.
  OpenPackConsole: (packID: string): Promise<void> => call('OpenPackConsole', packID),
  SelectActivePack: (packID: string): Promise<void> => call('SelectActivePack', packID),
  GetMods: (instanceID: string): Promise<ModEntry[]> => call('GetMods', instanceID),
  ToggleMod: (instanceID: string, fileName: string, enabled: boolean): Promise<void> => call('ToggleMod', instanceID, fileName, enabled),
  // ── Менеджер модів (вкладка «Моди») ──
  ScanModsFull: (packID: string): Promise<ModEntry[]> => call('ScanModsFull', packID),
  // ScanModsWithCachedUpdates — скан з диска + вже ВІДОМИЙ (закешований)
  // статус оновлень, без походу в мережу. Використовується при звичайному
  // відкритті сторінки модів, щоб не бити Modrinth/CurseForge щоразу.
  ScanModsWithCachedUpdates: (packID: string, mcVersion: string, loader: string): Promise<ModEntry[]> =>
    call('ScanModsWithCachedUpdates', packID, mcVersion, loader),
  // VerifyIncompatibleMods — друга, мережева хвиля перевірки: питає
  // Modrinth/CurseForge за sha1/fingerprint лише для модів, які локальний
  // скан (ScanModsWithCachedUpdates) уже позначив «несумісний», і знімає
  // хибну позначку, якщо реальний файл на Modrinth/CurseForge заявлений
  // під сумісний лоадер. Викликається окремо й асинхронно ПІСЛЯ звичайного
  // скану (як RefreshModUpdates для оновлень) — не блокує відкриття
  // сторінки. Повертає лише ЗМІНЕНІ записи (порожній масив = нічого не
  // змінилось).
  VerifyIncompatibleMods: (packID: string, mcVersion: string, loader: string): Promise<ModEntry[]> =>
    call('VerifyIncompatibleMods', packID, mcVersion, loader),
  CheckModUpdates: (packID: string, mcVersion: string, loader: string): Promise<ModEntry[]> => call('CheckModUpdates', packID, mcVersion, loader),
  // RefreshModUpdates — примусова МЕРЕЖЕВА перевірка (кнопка «Перевірити
  // оновлення»); результат кешується і потім читається ScanModsWithCachedUpdates.
  RefreshModUpdates: (packID: string, mcVersion: string, loader: string): Promise<ModEntry[]> =>
    call('RefreshModUpdates', packID, mcVersion, loader),
  // GetModIcons — іконки модів пачкою (fileName -> data URL) для ВИДИМИХ
  // рядків списку; у самому списку модів base64-іконок більше нема.
  GetModIcons: (packID: string, fileNames: string[]): Promise<Record<string, string>> =>
    call('GetModIcons', packID, fileNames),
  // GetModIconBase — базовий URL локального loopback-сервера іконок модів
  // (http://127.0.0.1:<port>/modicon/<token>) або '' (тоді modIcon.ts падає
  // назад на GetModIcons). Іконка = звичайний <img src>, її кешує WebView.
  GetModIconBase: (): Promise<string> => call('GetModIconBase'),
  // UIReady — фронтенд змонтовано: просимо shell показати головне вікно
  // (воно створюється прихованим, щоб не «просвічувати» під час завантаження).
  UIReady: (): Promise<void> => call('UIReady'),
  GetModVersions: (packID: string, fileName: string, mcVersion: string, loader: string): Promise<ModVersionOption[]> =>
    call('GetModVersions', packID, fileName, mcVersion, loader),
  InstallModVersion: (packID: string, fileName: string, opt: ModVersionOption): Promise<void> => call('InstallModVersion', packID, fileName, opt),
  // Посилання на сторінку мода (Modrinth/CurseForge) для меню «⋯» —
  // користувач може швидко відкрити мод у браузері. Порожній рядок =
  // мод знайти не вдалось.
  GetModPageLink: (packID: string, fileName: string, mcVersion: string, loader: string): Promise<string> =>
    call('GetModPageLink', packID, fileName, mcVersion, loader),
  // ── Рекомендовані моди (вкладка «Контент») ──
  // Hard-залежності вже встановлених модів, яких самих ще немає в збірці.
  // Поважає Settings.disableModSuggestions (бекенд повертає [] одразу).
  GetSuggestedMods: (packID: string, mcVersion: string, loader: string): Promise<SuggestedMod[]> =>
    call('GetSuggestedMods', packID, mcVersion, loader),
  // Встановити одну рекомендацію одним кліком (той самий пайплайн, що й
  // автовстановлення відсутніх залежностей — Modrinth search → найновіша
  // release-версія під mcVersion/loader → файл у теку модів збірки).
  InstallSuggestedMod: (packID: string, depID: string, mcVersion: string, loader: string): Promise<DependencyResult> =>
    call('InstallSuggestedMod', packID, depID, mcVersion, loader),
  UpdateAllMods: (packID: string, mcVersion: string, loader: string, backup: boolean): Promise<ModUpdateResult> => call('UpdateAllMods', packID, mcVersion, loader, backup),
  // Оновити ОДИН мод (кнопка на картці) — з опційною резервною копією
  // поточного файлу перед заміною (те саме, що робить UpdateAllMods,
  // але для одного мода й без мережевого проходу по всіх інших).
  UpdateOneMod: (packID: string, mod: ModEntry, backup: boolean): Promise<void> => call('UpdateOneMod', packID, mod, backup),
  ListModBackups: (packID: string): Promise<ModBackupEntry[]> => call('ListModBackups', packID),
  RestoreModBackup: (packID: string, entry: ModBackupEntry): Promise<void> => call('RestoreModBackup', packID, entry),
  DeleteMod: (packID: string, fileName: string): Promise<void> => call('DeleteMod', packID, fileName),
  FindDuplicateMods: (packID: string): Promise<DuplicateModGroup[]> => call('FindDuplicateMods', packID),
  CheckPreLaunchModIssues: (packID: string): Promise<PreLaunchModIssues> => call('CheckPreLaunchModIssues', packID),
  // ── Міграція модів між збірками ──
  // Список збірок з теками модів для селекторів «Звідки/Куди» в діалозі
  // міграції. excludePackId виключає поточну збірку зі списку джерел.
  GetMigrationPacks: (excludePackId: string): Promise<MigrationPack[]> => call('GetMigrationPacks', excludePackId),
  // Аналіз міграції: сканує моди збірки-джерела, шукає сумісні версії
  // для цільової (mcVersion+loader) на Modrinth (batch) / CurseForge і
  // будує звіт. targetPackId може бути порожнім (сценарій «створюю
  // збірку» — тоді не буде статусів already/update).
  AnalyzeModMigration: (sourcePackId: string, targetPackId: string, targetMcVersion: string, targetLoader: string): Promise<ModMigrationReport> =>
    call('AnalyzeModMigration', sourcePackId, targetPackId, targetMcVersion, targetLoader),
  CancelModMigrationAnalysis: (packId: string): Promise<void> => call('CancelModMigrationAnalysis', packId),
  // Виконує міграцію: качає підтверджені моди (статус found/update) у
  // теки модів цільової збірки. Прогрес — подією mods:migrate-progress.
  MigrateMods: (targetPackId: string, items: MigrationItem[]): Promise<ModMigrationResult> =>
    call('MigrateMods', targetPackId, items),
  // Асинхронна міграція (сценарій «створення збірки»): повертається одразу,
  // прогрес іде подіями mods:migrate-progress, завершення — mods:migrate-done.
  StartModMigration: (targetPackId: string, items: MigrationItem[]): Promise<string> =>
    call('StartModMigration', targetPackId, items),
  // ── Порівнювач модів двох збірок ──
  // Двосторонній звіт: для кожного мода — стан ОБОХ збірок одночасно.
  CompareModPacks: (leftPackId: string, rightPackId: string): Promise<ModCompareReport> =>
    call('CompareModPacks', leftPackId, rightPackId),
  // Встановити/оновити один мод в ОДНУ конкретну сторону порівняння.
  InstallCompareMod: (req: CompareInstallRequest): Promise<void> =>
    call('InstallCompareMod', req),
  // Перевірка ID збірки перед створенням (конфлікт → модалка переназвати/цифра).
  CheckCustomPackID: (name: string): Promise<CustomPackIDCheck> =>
    call('CheckCustomPackID', name),
  // Перевірка нового ID збірки при редагуванні (рядок «ID збірки» на
  // сторінці збірки): поточна збірка не вважається конфліктом сама собі.
  CheckCustomPackIDChange: (oldID: string, newID: string): Promise<CustomPackIDCheck> =>
    call('CheckCustomPackIDChange', oldID, newID),
  // Зміна ID (і відповідно теки) кастомної збірки. Повертає оновлений
  // запис збірки; бекенд емітить builds:changed з новим ID.
  ChangeCustomPackID: (oldID: string, newID: string): Promise<CustomPack> =>
    call('ChangeCustomPackID', oldID, newID),
  // ── RAM-попередження перед запуском ──
  // GetLaunchWarnings аналізує збірку ПЕРЕД запуском (рекомендована RAM з
  // packs-index vs ефективна; виділена vs фізична пам'ять системи).
  GetLaunchWarnings: (packID: string): Promise<LaunchWarnings> => call('GetLaunchWarnings', packID),
  // «Не показувати знову» для попередження (kind: recommended | lowram),
  // ЛИШЕ для цієї збірки (InstanceConfig).
  DismissPackWarning: (packID: string, kind: string): Promise<void> => call('DismissPackWarning', packID, kind),
  // Зберегти RAM саме для ЦІЄЇ збірки (MaxRAMOverride) — слайдер у модалці
  // попередження пише саме сюди, щоб збірка більше не попереджала.
  SetPackRAM: (packID: string, ramMB: number): Promise<void> => call('SetPackRAM', packID, ramMB),
  // Відкрити теку mods/ збірки в Провіднику (кнопка в тулбарі менеджера модів).
  OpenModsFolder: (packID: string): Promise<void> => call('OpenModsFolder', packID),
  // Показати конкретний jar мода виділеним у Провіднику (кнопка на картці мода).
  RevealModFile: (packID: string, fileName: string): Promise<void> => call('RevealModFile', packID, fileName),
  // Системний діалог вибору jar-файлів (кнопка «Додати мод» у тулбарі).
  PickModFiles: (): Promise<string[]> => call('PickModFiles'),
  // Копіює обрані шляхи (з діалогу або drag&drop) у теку mods/ збірки.
  ImportModFiles: (packID: string, paths: string[]): Promise<ModImportResult> => call('ImportModFiles', packID, paths),
  // ── «Єдине джерело» модів (збірки) ──
  GetModSources: (packID: string): Promise<ModSourcesView> => call('GetModSources', packID),
  PickModSourceFile: (): Promise<string> => call('PickModSourceFile'),
  PickModSourceFolder: (): Promise<string> => call('PickModSourceFolder'),
  AddModSource: (packID: string, kind: string, path: string): Promise<ModSourcesView> => call('AddModSource', packID, kind, path),
  RemoveModSource: (packID: string, sourceID: string): Promise<ModSourcesView> => call('RemoveModSource', packID, sourceID),
  SyncModSources: (packID: string): Promise<{ checked: number; copied: number; restored: number; removed: number; moved: number; missing: any[]; errors: string[] }> => call('SyncModSources', packID),
  // ── «Єдине джерело» модів/плагінів (сервери; name = 'mods' | 'plugins') ──
  GetServerModSources: (serverID: string, name: string): Promise<ModSourcesView> => call('GetServerModSources', serverID, name),
  AddServerModSource: (serverID: string, name: string, kind: string, path: string): Promise<ModSourcesView> => call('AddServerModSource', serverID, name, kind, path),
  RemoveServerModSource: (serverID: string, name: string, sourceID: string): Promise<ModSourcesView> => call('RemoveServerModSource', serverID, name, sourceID),
  // ── ШІ браузера контенту: переклад сторінки + відгук про проєкт ──
  // Переклад опису (markdown/HTML розмітка зберігається) через Groq.
  // Кешується на диску — повторне відкриття сторінки без запиту до API.
  TranslateBrowserProject: (source: string, projectId: string, lang: string): Promise<BrowserTranslation> =>
    call('TranslateBrowserProject', source, projectId, lang),
  // AI-відгук про проєкт: аналіз даних + веб-пошук, людською мовою.
  // Генерується ОДИН раз і кешується — перегенерація заборонена.
  GetBrowserProjectReview: (source: string, projectId: string, lang: string): Promise<BrowserReview> =>
    call('GetBrowserProjectReview', source, projectId, lang),
  // ── Сторінка автора (як Modrinth app): профіль + усі проєкти ──
  GetBrowserAuthor: (source: string, authorId: string, fallbackName: string): Promise<BrowserAuthor> =>
    call('GetBrowserAuthor', source, authorId, fallbackName),
  GetBrowserAuthorProjects: (source: string, authorId: string): Promise<BrowserEntry[]> =>
    call('GetBrowserAuthorProjects', source, authorId),
  // ── Автоприєднання: сервери з server.dat + пінг ──
  ListPackServers: (packID: string): Promise<InstanceServer[]> => call('ListPackServers', packID),
  ListPinnedServers: (): Promise<PinnedServerView[]> => call('ListPinnedServers'),
  // Порядок закріплених серверів у секції «Закріплені сервери» (drag&drop,
  // ключі "packId|address") — зберігається у settings.json, як packLayout.
  SavePinnedServerOrder: (keys: string[]): Promise<void> => call('SavePinnedServerOrder', keys),
  PingServer: (address: string): Promise<ServerPing> => call('PingServer', address),
  // ── Міграція збірок з інших лаунчерів (Prism / Modrinth App) ──
  // Сканування шукає лаунчери ЛИШЕ у їхніх стандартних теках даних
  // (жодного обходу дисків — антивірусна безпека).
  MigrateScan: (): Promise<MigrationScanResult> => call('MigrateScan'),
  // Стан міграції: чи показувати пост-логін вікно, які лаунчери знайдено.
  MigrateStatus: (): Promise<MigrateStatus> => call('MigrateStatus'),
  // «Ми показали вікно» — галочка у БД; вікно більше не спливає автоматично.
  MigrateDismiss: (): Promise<void> => call('MigrateDismiss'),
  // Перенести вибрані збірки одного лаунчера. Прогрес — подією
  // migrate:progress, підсумок — migrate:done (результат повертається
  // теж, але події — основний канал для модалки).
  MigrateImport: (req: MigrateImportRequest): Promise<MigrateImportResult> =>
    call('MigrateImport', req),
  // Прев'ю PNG-іконки збірки іншого лаунчера (data URL через бекенд,
  // для карток у модалці міграції).
  MigrateIconDataURL: (path: string): Promise<string> => call('MigrateIconDataURL', path),
  // Стан бази даних (відновлення з резервної копії / навмисне скидання).
  GetDBHealth: (): Promise<DBHealth> => call('GetDBHealth'),
  // ── Розділ "Сервери": повний CRUD над servers.dat збірки ──
  AddPackServer: (packID: string, name: string, address: string): Promise<void> => call('AddPackServer', packID, name, address),
  // AddTempPackServer — додати сервер у список збірки ЯК ТИМЧАСОВИЙ
  // (P2P-кімната). Лаунчер сам прибере запис, коли доступу не стане.
  AddTempPackServer: (packID: string, name: string, address: string): Promise<void> => call('AddTempPackServer', packID, name, address),
  // TempPackServerAddresses — тимчасові адреси цієї збірки (для мітки в UI).
  TempPackServerAddresses: (packID: string): Promise<string[]> => call('TempPackServerAddresses', packID),
  // SyncTempPackServers — прибрати тимчасові сервери, які вже недоступні
  // (кімнату закрито / оренду відкликано) і повернути ті, що лишились.
  SyncTempPackServers: (): Promise<string[]> => call('SyncTempPackServers'),
  UpdatePackServer: (packID: string, oldAddress: string, newName: string, newAddress: string): Promise<void> => call('UpdatePackServer', packID, oldAddress, newName, newAddress),
  RemovePackServer: (packID: string, address: string): Promise<void> => call('RemovePackServer', packID, address),
  ReorderPackServers: (packID: string, addresses: string[]): Promise<void> => call('ReorderPackServers', packID, addresses),
  TogglePinPackServer: (packID: string, address: string): Promise<void> => call('TogglePinPackServer', packID, address),
  CopyServerToPack: (srcPackID: string, address: string, dstPackID: string): Promise<void> => call('CopyServerToPack', srcPackID, address, dstPackID),
  // Копіює КІЛЬКА серверів в іншу збірку одним викликом; результат по кожній адресі.
  CopyServersToPack: (srcPackID: string, addresses: string[], dstPackID: string): Promise<ServerCopyResult[]> => call('CopyServersToPack', srcPackID, addresses, dstPackID),
  PingServers: (addresses: string[]): Promise<Record<string, ServerPing>> => call('PingServers', addresses),
  // Запустити збірку і одразу зайти на конкретний сервер (незалежно від
  // збереженого автоприєднання і незалежно від того, чи сервер онлайн).
  LaunchInstanceOnServer: (instanceID: string, address: string): Promise<void> => call('LaunchInstanceOnServer', instanceID, address),
  // Запуск збірки З ЯРЛИКА з персональними опціями ярлика (як у Prism):
  // accountID перекриває акаунт, autoJoinServer — автоприєднання; решта
  // параметрів запуску — як при звичайному запуску.
  LaunchInstanceShortcut: (instanceID: string, accountId: string, autoJoinServer: string): Promise<void> =>
    call('LaunchInstanceShortcut', instanceID, accountId, autoJoinServer),
  // ── Консоль: згортання окремого вікна у «шапку» ──
  SetConsoleCollapsed: (buildID: string, collapsed: boolean): Promise<void> => call('SetConsoleCollapsed', buildID, collapsed),
  // Надійне копіювання у системний буфер обміну Windows (WebView2 clipboard
  // «копіює» у ніщо — текст не потрапляє в буфер ОС). Бекенд через Win32 API.
  //
  // Фолбек на navigator.clipboard — для Linux: там бекенд пише в буфер через
  // wl-copy/xclip/xsel (clipboard_other.go), і якщо жодної утиліти в системі
  // немає (напр. WSL2/WSLg без wl-clipboard), виклик повертає зрозумілу
  // помилку. Кидати її користувачу не варто: у WebKitGTK — на відміну від
  // WebView2, через який і з'явився бекендний шлях, — clipboard рушія
  // працює, тож пробуємо його другим кроком.
  CopyToClipboard: async (text: string): Promise<void> => {
    try {
      await call('CopyToClipboard', text)
    } catch (e) {
      if (!navigator.clipboard?.writeText) throw e
      await navigator.clipboard.writeText(text)
    }
  },
  // Читання системного буфера обміну (порожній рядок — там не текст або
  // читання недоступне). Потрібне, щоб лаунчер сам бачив посилання-запрошення
  // в друзі, яке гравець скопіював з Discord/Telegram.
  ClipboardText: (): Promise<string> => call('ClipboardText'),
  // ── Налаштування гри «за замовчуванням» (вкладка налаштувань) ──
  GetMinecraftDefaults: (): Promise<MinecraftDefaults> => call('GetMinecraftDefaults'),
  SaveMinecraftDefaults: (d: MinecraftDefaults): Promise<void> => call('SaveMinecraftDefaults', d),
  ResetMinecraftDefaults: (): Promise<void> => call('ResetMinecraftDefaults'),
  ImportMinecraftDefaultsFromPack: (packID: string): Promise<MinecraftDefaults> => call('ImportMinecraftDefaultsFromPack', packID),
  // ── Розділ «Гра» на сторінці збірки (пряме читання/редагування options.txt) ──
  GetPackOptions: (packID: string): Promise<PackOptionsView> => call('GetPackOptions', packID),
  SavePackOptions: (packID: string, options: Record<string, string>): Promise<void> => call('SavePackOptions', packID, options),
  // Мови гри з ОРИГІНАЛЬНОГО asset index Mojang (не захардкоджений список).
  GetGameLanguagesForPack: (packID: string): Promise<GameLanguage[]> => call('GetGameLanguagesForPack', packID),
  // Порожній рядок = «версія за умовч.» (бекенд сам визначить версію).
  GetGameLanguages: (mcVersion?: string): Promise<GameLanguage[]> => call('GetGameLanguages', mcVersion ?? ''),
  CopyPackOptionsFromPack: (srcPackID: string, dstPackID: string): Promise<void> => call('CopyPackOptionsFromPack', srcPackID, dstPackID),
  ApplyMinecraftDefaultsToPack: (packID: string): Promise<void> => call('ApplyMinecraftDefaultsToPack', packID),
  // Справжні (з lang-файлу мода) підписи біндів клавіш модів — див.
  // internal/mods/manager.go: KeyBindLabels.
  GetKeyBindLabels: (packID: string, modBindKeys: string[]): Promise<Record<string, KeyBindLabel>> => call('GetKeyBindLabels', packID, modBindKeys),
  GetBuilds: (): Promise<BuildView[]> => call('GetBuilds'),
  GetFolderPaths: (): Promise<FolderPaths> => call('GetFolderPaths'),
  GetInstanceConfig: (id: string): Promise<InstanceConfig> => call('GetInstanceConfig', id),
  GetJavaStatus: (): Promise<JavaStatus> => call('GetJavaStatus'),
  GetPackIndex: (): Promise<PackIndexEntry[]> => call('GetPackIndex'),
  // data URL асета збірки (іконка/фон) — качається через бекенд, бо браузер
  // не може додати токен-заголовки до <img> (див. lib/packAssets.svelte.ts).
  GetPackAsset: (url: string): Promise<string> => call('GetPackAsset', url),
  // Динамічний банер головного екрана: data URL картинки останнього
  // офіційного оновлення Minecraft (бекенд сам питає Mojang-маніфест +
  // launchercontent.mojang.com/news.json і кешує файл на диску — див.
  // backend/news_banner.go). Помилка = просто не показуємо банер.
  GetNewsBanner: (): Promise<string> => call('GetNewsBanner'),
  // Аргументи, які запуск відхилить (з причиною) — для JvmArgsEditor.
  ValidateJvmArgs: (raw: string): Promise<{ arg: string; reason: string }[]> => call('ValidateJvmArgs', raw),
  // Картинка найновішого СНАПШОТА (стаття minecraft.net) — для auto-snapshot.
  GetSnapshotBanner: (): Promise<string> => call('GetSnapshotBanner'),
  // Повні записи білого списку моду (глобальний + per-pack, дедупльовані
  // за ModID) для картки в UI сторінки шаурма-збірки — іконка/назва/
  // посилання на сторінку Modrinth/CurseForge, коли автор збірки додав
  // запис за посиланням на проєкт у Pack Manager (див. GetPackWhitelist
  // у backend). Порожній масив — білий список пустий, це не помилка.
  GetPackWhitelist: (packId: string): Promise<WhitelistEntry[]> => call('GetPackWhitelist', packId),
  // Вікі шаурма-збірки (packs/<id>/wiki.json з CDN). null — вікі ще не
  // створено (404): вкладку «Вікі» показувати немає сенсу.
  GetPackWiki: (packId: string): Promise<PackWikiDoc | null> => call('GetPackWiki', packId),
  GetPlaytime: (): Promise<Record<string, number>> => call('GetPlaytime'),
  // Повний знімок аналітики ігрового часу (розділ «Аналітика» у «Моїх
  // збірках»): підсумки, стовпчики за 7 днів, серії тижнів/місяців/років,
  // топ збірок. Дані — з таблиці сесій (playtime_sessions).
  // accountName — нік гравця (порожній = сумарно по всіх); бекенд фільтрує
  // аналітику за ніком, щоб дані переживали вихід з акаунта.
  GetAnalytics: (accountName: string): Promise<AnalyticsSummary> => call('GetAnalytics', accountName),
  ReorderAccounts: (ids: string[]): Promise<void> => call('ReorderAccounts', ids),
  GetSettings: (): Promise<Settings> => call('GetSettings'),
  GetSettingsSchema: (): Promise<SettingDef[]> => call('GetSettingsSchema'),
  GetPackLayout: (): Promise<{ order: string[]; pinned: string[] }> => call('GetPackLayout'),
  SavePackLayout: (order: string[], pinned: string[]): Promise<void> => call('SavePackLayout', order, pinned),
  GetPackFolders: (): Promise<PackFolder[]> => call('GetPackFolders'),
  SavePackFolders: (folders: PackFolder[]): Promise<void> => call('SavePackFolders', folders),
  GetServerFolders: (): Promise<ServerFolder[]> => call('GetServerFolders'),
  SaveServerFolders: (folders: ServerFolder[]): Promise<void> => call('SaveServerFolders', folders),
  // «Останнє вікно» — сторінка, вкладка, відкрита збірка, скрол (файл
  // .ui-state.json, НЕ localStorage — див. lib/uiState.ts).
  GetUIState: (): Promise<UIState> => call('GetUIState'),
  SaveUIState: (state: UIState): Promise<void> => call('SaveUIState', state),
  GetSetupProgress: (): Promise<SetupProgress> => call('GetSetupProgress'),
  GetStorageUsage: (): Promise<StorageInfo> => call('GetStorageUsage'),
  // Список встановлених збірок з розмірами (швидке видалення у «Пам'ять і кеш»).
  GetStoragePacks: (): Promise<StoragePackEntry[]> => call('GetStoragePacks'),
  // Очистити логи лаунчера + логів/краш-репортів усіх збірок.
  ClearPackLogs: (): Promise<void> => call('ClearPackLogs'),
  // ── Тихий журнал помилок лаунчера (launcher_errors.go) ──
  // Фронтенд пише сюди JS-помилки (unhandledrejection / window.onerror /
  // catch-блоки), бекенд — свої. Юзер може експортувати журнал у JSON
  // через Save-діалог (кнопка «Поділитись логом» у Налаштуваннях → Про
  // застосунок / журнал помилок).
  AppendLauncherError: (source: string, message: string, detail: string): Promise<void> =>
    call('AppendLauncherError', source, message, detail),
  // Згода на звіти про помилки (backend/errorreports_api.go). Binding був
  // відсутній тут, тож модалка ловила TS-помилку, а виклик ішов у
  // Bridge.Invoke «наосліп» — додаємо явні сигнатури.
  ErrorReportsStatus: (): Promise<{ shouldShow: boolean; promptShown: boolean; hasAccounts: boolean; enabled: boolean }> =>
    call('ErrorReportsStatus'),
  ErrorReportsSetConsent: (enabled: boolean): Promise<void> =>
    call('ErrorReportsSetConsent', enabled),
  ErrorReportsDismiss: (): Promise<void> =>
    call('ErrorReportsDismiss'),
  GetLauncherErrors: (): Promise<LauncherErrorEntry[]> => call('GetLauncherErrors'),
  GetLauncherErrorCount: (): Promise<number> => call('GetLauncherErrorCount'),
  // Відкриває Save-діалог і записує JSON-журнал помилок; повертає шлях
  // до файлу або "" (скасовано).
  ExportLauncherErrors: (): Promise<string> => call('ExportLauncherErrors'),
  // Аналіз/видалення невикористовуваної Java (мажори, яких немає в жодної
  // встановленої збірки) — вкладка «Пам'ять і кеш».
  AnalyzeUnusedJava: (): Promise<UnusedJavaInfo> => call('AnalyzeUnusedJava'),
  CleanupUnusedJava: (majors: number[]): Promise<void> => call('CleanupUnusedJava', majors),
  AnalyzeStorageVersions: (): Promise<StorageVersionsInfo> => call('AnalyzeStorageVersions'),
  CleanupUnusedVersions: (ids: string[]): Promise<void> => call('CleanupUnusedVersions', ids),
  AnalyzeOrphanLibraries: (): Promise<StorageLibrariesInfo> => call('AnalyzeOrphanLibraries'),
  CleanupOrphanLibraries: (): Promise<StorageLibrariesInfo> => call('CleanupOrphanLibraries'),
  AnalyzeOrphanAssets: (): Promise<StorageAssetsInfo> => call('AnalyzeOrphanAssets'),
  CleanupOrphanAssets: (): Promise<StorageAssetsInfo> => call('CleanupOrphanAssets'),
  GetStorageSuggestions: (): Promise<StorageSuggestion[]> => call('GetStorageSuggestions'),
  GetSystemInfo: (): Promise<SystemInfo> => call('GetSystemInfo'),
  GetVersion: (): Promise<string> => call('GetVersion'),
  // Дешевий runtime.GOOS ("windows"/"linux"/"darwin") — окремо від
  // GetSystemInfo (CPU/RAM/диск, важче, ліниво лише на вкладці «Про
  // лаунчер»), щоб викликати одразу при старті й підставити data-platform
  // на <html> ДО першого рендеру важких CSS-ефектів.
  GetPlatform: (): Promise<string> => call('GetPlatform'),
  // Одноразова перевірка стану мережі (true = інтернет є). Фоновий пінгер
  // бекенду і так шле network:status, але фронтенд може перепитати після
  // відновлення підключення без очікування наступного тіку.
  CheckNetwork: (): Promise<boolean> => call('CheckNetwork'),
  GetWizardDefaults: (): Promise<SetupProgress> => call('GetWizardDefaults'),
  HideToTray: (): Promise<void> => call('HideToTray'),
  // Кнопка «Згорнути»: на панель завдань або в трей (minimizeAction).
  MinimizeLauncher: (): Promise<void> => call('MinimizeLauncher'),
  IsFirstRun: (): Promise<boolean> => call('IsFirstRun'),
  IsGameRunning: (): Promise<boolean> => call('IsGameRunning'),
  // Чи останній вихід гри (будь-якої збірки) стався через ручну зупинку
  // (StopInstance), а не крах — читає App.lastExitWasUserStop (backend/
  // app.go). Використовує окреме плаваюче вікно консолі (packID=="") у
  // своєму обробнику безадресної game:exit, бо ця подія несе лише код
  // виходу, без прапорця (на відміну від build:exit).
  WasLastExitUserStop: (): Promise<boolean> => call('WasLastExitUserStop'),
  IsShaurmaEdition: (): Promise<boolean> => call('IsShaurmaEdition'),
  LaunchInstance: (id: string): Promise<void> => call('LaunchInstance', id),
  // Фіксує акаунт, активний У МОМЕНТ КЛІКУ «Грати» — викликати СИНХРОННО
  // (await) ПЕРЕД LaunchInstance, щоб мінімізувати вікно гонитви: якщо
  // гравець перемкне акаунт, поки на екрані триває довга перевірка/
  // встановлення файлів попереднього запуску, гра все одно стартує на
  // акаунті, що був активний саме в момент кліку, а не на тому, що став
  // активним пізніше (backend/app.go LaunchInstance, launchClickAccount).
  SetLaunchClickAccount: (instanceId: string, accountId: string): Promise<void> => call('SetLaunchClickAccount', instanceId, accountId),
  LoadFontFile: (path: string): Promise<string> => call('LoadFontFile', path),
  LoginPirate: (username: string): Promise<void> => call('LoginPirate', username),
  OpenExternal: (url: string): Promise<void> => call('OpenExternal', url),
  // Відповідь на діалог заблокованих модів (імпорт CurseForge zip,
  // подія import:blocked): 'modrinth' | 'skip' | 'cancel'.
  RespondBlockedMods: (packId: string, decision: string, checkedMods: string): Promise<void> => call('RespondBlockedMods', packId, decision, checkedMods),
  // Відкрити теку встановленої збірки у Провіднику.
  OpenPackFolder: (id: string): Promise<void> => call('OpenPackFolder', id),
  // Список тек одиночних світів (saves/) збірки — для вибору світу в
  // автоприєднанні на сторінці збірки, вкладка «Гра».
  ListInstanceWorlds: (id: string): Promise<string[]> => call('ListInstanceWorlds', id),
  RemoveAccount: (id: string): Promise<void> => call('RemoveAccount', id),
  // Перейменування офлайн (Шаурма) акаунта: новий нік = новий ID/UUID.
  // Повертає вже готовий акаунт + окреме попередження про скін-нік
  // (backend/app.go: RenameOfflineAccountResult).
  RenameOfflineAccount: (oldAccountID: string, newUsername: string): Promise<RenameOfflineAccountResult> =>
    call('RenameOfflineAccount', oldAccountID, newUsername),
  RestoreWindowAfterGame: (): Promise<void> => call('RestoreWindowAfterGame'),
  // Показати/розгорнути/сфокусувати головне вікно (напр. коли розпізнано лінк друга).
  FocusMainWindow: (): Promise<void> => call('FocusMainWindow'),
  TakePendingFriendLink: (): Promise<string> => call('TakePendingFriendLink'),
  SaveFolderPaths: (instanceDir: string, javaPath: string): Promise<void> => call('SaveFolderPaths', instanceDir, javaPath),
  SaveInstanceConfig: (cfg: InstanceConfig): Promise<void> => call('SaveInstanceConfig', cfg),
  GetBuildSkinRestoreStatus: (buildID: string, accountID: string): Promise<SkinRestoreStatus> => call('GetBuildSkinRestoreStatus', buildID, accountID),
  // ── Скін-система (офлайн-акаунти, ЛИШЕ Shaurma-редакція) ────────────
  // Base-редакція не має цих методів взагалі (backend.dll зібраний без
  // тегу shaurma) — викликати їх можна лише коли isShaurmaEdition() === true,
  // інакше bridge поверне помилку «метод не знайдено».
  // Google OAuth: старт флоу → URL для браузера; Await — чекає токен.
  StartGoogleSkinLogin: (): Promise<string> => call('StartGoogleSkinLogin'),
  AwaitGoogleSkinLogin: (): Promise<SkinAuthStatus> => call('AwaitGoogleSkinLogin'),
  CancelGoogleSkinLogin: (): Promise<void> => call('CancelGoogleSkinLogin'),
  // Стан сесії скін-системи (loggedIn/email/nickname/expired).
  GetSkinAuthStatus: (): Promise<SkinAuthStatus> => call('GetSkinAuthStatus'),
  // Система скінів ліцензійного акаунта: 'mojang' | 'shaurma' (лише Shaurma-редакція).
  // ── Акаунт Шаурми (окремий для кожного акаунта лаунчера) ──
  // Стан акаунта Шаурми АКТИВНОГО акаунта лаунчера (є/нема, тип, нік, ID).
  GetShaurmaAccount: (): Promise<ShaurmaAccountInfo> => call('GetShaurmaAccount'),
  // Створити акаунт Шаурми для активного акаунта: лаунчер підставляє все,
  // користувач лише підтверджує особу в браузері (Google або Microsoft).
  CreateShaurmaAccount: (nickname: string, provider: string): Promise<void> => call('CreateShaurmaAccount', nickname, provider),
  CancelShaurmaLogin: (): Promise<void> => call('CancelShaurmaLogin'),
  // Нік акаунта Шаурми: для неліцензійного — вільна зміна, для ліцензійного
  // бекенд відмовить (нік береться лише з Mojang).
  SetShaurmaNickname: (nickname: string): Promise<void> => call('SetShaurmaNickname', nickname),
  SetShaurmaHandle: (handle: string): Promise<void> => call('SetShaurmaHandle', handle),
  // Біо (≤ 120 символів) і колір фону профілю (#RRGGBB або '' — стандартний).
  SetShaurmaProfileStyle: (bio: string, color: string): Promise<void> => call('SetShaurmaProfileStyle', bio, color),
  DeleteShaurmaAccount: (): Promise<void> => call('DeleteShaurmaAccount'),
  // ── Пристрої акаунта й ключі чату ──
  GetShaurmaDevices: (): Promise<ShaurmaDeviceList> => call('GetShaurmaDevices'),
  RenameShaurmaDevice: (deviceId: string, name: string): Promise<void> => call('RenameShaurmaDevice', deviceId, name),
  RevokeShaurmaDevice: (deviceId: string, notMe: boolean): Promise<void> => call('RevokeShaurmaDevice', deviceId, notMe),
  RevokeOtherShaurmaDevices: (): Promise<ShaurmaRevokeOthers> => call('RevokeOtherShaurmaDevices'),
  RestoreShaurmaDevices: (): Promise<number> => call('RestoreShaurmaDevices'),
  ShaurmaEmergencyReset: (): Promise<void> => call('ShaurmaEmergencyReset'),
  GetShaurmaAudit: (limit: number): Promise<ShaurmaAuditEvent[]> => call('GetShaurmaAudit', limit),
  ReplaceShaurmaDevice: (deviceId: string): Promise<SignInResult> => call('ReplaceShaurmaDevice', deviceId),
  GetChatKeyStatus: (): Promise<ChatKeyStatus> => call('GetChatKeyStatus'),
  ResetChatKeys: (): Promise<void> => call('ResetChatKeys'),
  // Підтягнути актуальний нік з Mojang (ліцензійний тип). Повертає новий нік.
  SyncShaurmaNickname: (): Promise<string> => call('SyncShaurmaNickname'),
  // Повний профіль (серверні дані + локальна статистика) для сторінки «Акаунт».
  GetShaurmaProfile: (): Promise<ShaurmaProfile> => call('GetShaurmaProfile'),
  // ── Друзі Shaurma (лише Shaurma-редакція; потрібна скін-сесія) ──
  GetFriendsState: (): Promise<SocialState> => call('GetFriendsState'),
  // ── Віддалене керування серверами друзів ─────────────────────────────
  // ВЛАСНИК: хто з друзів що може на моєму сервері (VPS перевіряє дружбу й права, лаунчер — ще раз).
  GetRemoteAccess: (): Promise<RemoteAccessView> => call('GetRemoteAccess'),
  SetRemoteGrant: (serverId: string, friendId: string, start: boolean, stop: boolean, consoleView: boolean, exec: boolean): Promise<void> =>
    call('SetRemoteGrant', serverId, friendId, start, stop, consoleView, exec),
  SetRemoteAllowStopWithPlayers: (serverId: string, v: boolean): Promise<void> => call('SetRemoteAllowStopWithPlayers', serverId, v),
  SetRemoteAllowPrivileged: (serverId: string, v: boolean): Promise<void> => call('SetRemoteAllowPrivileged', serverId, v),
  SetRemotePaused: (v: boolean): Promise<void> => call('SetRemotePaused', v),
  GetRemoteAudit: (serverId: string, limit: number): Promise<RemoteAuditEntry[]> => call('GetRemoteAudit', serverId, limit),
  // ДРУГ: сервери друзів, до яких мені дали доступ; дії повертають reqId (результат — подія remote:result).
  GetRemoteServers: (): Promise<RemoteServer[]> => call('GetRemoteServers'),
  RemoteStartServer: (ownerId: string, serverId: string): Promise<string> => call('RemoteStartServer', ownerId, serverId),
  RemoteStopServer: (ownerId: string, serverId: string): Promise<string> => call('RemoteStopServer', ownerId, serverId),
  RemoteOpenConsole: (ownerId: string, serverId: string): Promise<string> => call('RemoteOpenConsole', ownerId, serverId),
  RemoteConsoleExec: (sid: string, cmd: string): Promise<void> => call('RemoteConsoleExec', sid, cmd),
  RemoteCloseConsole: (sid: string): Promise<void> => call('RemoteCloseConsole', sid),
  RefreshFriends: (): Promise<SocialState> => call('RefreshFriends'),
  SendFriendInvite: (accountId: string): Promise<void> => call('SendFriendInvite', accountId),
  SendFriendInviteByHandle: (handle: string): Promise<void> => call('SendFriendInviteByHandle', handle),
  RespondFriendInvite: (inviteId: string, accept: boolean): Promise<void> => call('RespondFriendInvite', inviteId, accept),
  ListSentFriendInvites: (): Promise<SocialInvite[]> => call('ListSentFriendInvites'),
  RemoveFriend: (accountId: string): Promise<void> => call('RemoveFriend', accountId),
  SetFriendsPrivacy: (p: SocialPrivacy): Promise<void> => call('SetFriendsPrivacy', p),
  // ── Мути чатів (chatmute_shaurma.go в бекенді) ──
  // Три рівні: мут усього чату (all), мут звуку (sound), мут одного
  // діалогу (muted — переживає видалення з друзів).
  GetChatMuteState: (): Promise<ChatMuteState> => call('GetChatMuteState'),
  SearchFriendCandidates: (query: string): Promise<SocialUserHit[]> => call('SearchFriendCandidates', query),
  BlockAccount: (accountID: string): Promise<void> => call('BlockAccount', accountID),
  UnblockAccount: (accountID: string): Promise<void> => call('UnblockAccount', accountID),
  GetBlockedAccounts: (): Promise<SocialBlockedAccount[]> => call('GetBlockedAccounts'),
  GetChatPeerProfile: (accountID: string): Promise<SocialPeerProfile> => call('GetChatPeerProfile', accountID),
  // Профіль друга: приватність застосовує сервер (hidden=true — друг закрив профіль).
  GetFriendProfile: (accountID: string): Promise<SocialFriendProfile> => call('GetFriendProfile', accountID),
  GetProfileVisibility: (): Promise<SocialProfileVisibility> => call('GetProfileVisibility'),
  SetProfileVisibility: (v: SocialProfileVisibility): Promise<void> => call('SetProfileVisibility', v),
  SetPresenceMode: (mode: 'online' | 'away' | 'dnd' | 'invisible'): Promise<string> => call('SetPresenceMode', mode),
  SetChatMuteAll: (on: boolean): Promise<ChatMuteState> => call('SetChatMuteAll', on),
  SetChatMuteSound: (on: boolean): Promise<ChatMuteState> => call('SetChatMuteSound', on),
  SetChatMuted: (accountID: string, muted: boolean): Promise<ChatMuteState> => call('SetChatMuted', accountID, muted),
  // ── Чат Shaurma ──
  // SendChatMessage — повертає clientMessageId (для зіставлення з chat:ack).
  SendChatMessage: (peerAccountId: string, text: string): Promise<string> => call('SendChatMessage', peerAccountId, text),
  // Стан відправки повідомлення: status = pending | acked | failed | unknown.
  GetChatSendState: (clientMessageId: string): Promise<{ status: string; chatId?: string; id?: number; ts?: number; code?: string }> => call('GetChatSendState', clientMessageId),
  // Повторна відправка того самого повідомлення (той самий clientMessageId → без дубля).
  RetryChatMessage: (peerAccountId: string, text: string, clientMessageId: string): Promise<string> => call('RetryChatMessage', peerAccountId, text, clientMessageId),
  // GetChatHistory — історія вже розшифрована локально; before=0 → остання сторінка.
  GetChatHistory: (peerAccountId: string, before: number, limit: number): Promise<SocialChatPage> => call('GetChatHistory', peerAccountId, before, limit),
  // Сторінка НОВІШИХ за after (unix мс) повідомлень, за зростанням (прокрутка вниз після переходу).
  GetChatHistoryAfter: (peerAccountId: string, after: number, limit: number): Promise<SocialChatPage> => call('GetChatHistoryAfter', peerAccountId, after, limit),
  // Видалити СВОЄ повідомлення в обох (і з серверу). Помилка: social:forbidden | not_found | ...
  EditChatMessage: (peerAccountId: string, messageId: number, text: string): Promise<void> => call('EditChatMessage', peerAccountId, messageId, text),
  DeleteChatMessage: (peerAccountId: string, messageId: number): Promise<void> => call('DeleteChatMessage', peerAccountId, messageId),
  PinChatMessage: (peerAccountId: string, messageId: number): Promise<void> => call('PinChatMessage', peerAccountId, messageId),
  UnpinChatMessage: (peerAccountId: string, messageId: number): Promise<void> => call('UnpinChatMessage', peerAccountId, messageId),
  // SendChatMessageReply — повідомлення-відповідь на конкретне повідомлення (replyToId, як у Telegram).
  SendChatMessageReply: (peerAccountId: string, text: string, replyToId: number): Promise<string> => call('SendChatMessageReply', peerAccountId, text, replyToId),
  // ReactChatMessage — реакція на повідомлення (один емодзі на акаунт; порожній emoji = прибрати).
  ReactChatMessage: (peerAccountId: string, messageId: number, emoji: string): Promise<void> => call('ReactChatMessage', peerAccountId, messageId, emoji),
  // SetSocialView — що користувач бачить зараз (шухляда «Друзі» / діалог із ким): щоб
  // тост у правому нижньому куті не дублював видиме, але з'являвся в усіх інших випадках.
  SetSocialView: (drawerOpen: boolean, peerAccountId: string): Promise<void> => call('SetSocialView', drawerOpen, peerAccountId),
  SendChatTyping: (peerAccountId: string): Promise<void> => call('SendChatTyping', peerAccountId),
  MarkChatRead: (peerAccountId: string, upToMessageId: number): Promise<void> => call('MarkChatRead', peerAccountId, upToMessageId),
  SendChatServerInvite: (peerAccountId: string, serverAddr: string, packId: string, packName: string, ttlMs: number): Promise<void> => call('SendChatServerInvite', peerAccountId, serverAddr, packId, packName, ttlMs),
  // ── Тимчасові запрошення (LAN-світ / локальний сервер через P2P, лише для запрошених) ──
  ScanLocalServers: (): Promise<LocalServer[]> => call('ScanLocalServers'),
  ProbeLocalServer: (port: number): Promise<LocalServer> => call('ProbeLocalServer', port),
  ListTempInvites: (): Promise<TempInvite[]> => call('ListTempInvites'),
  CreateTempInvite: (port: number, friendIds: string[], packId: string, packName: string): Promise<TempInvite> => call('CreateTempInvite', port, friendIds, packId, packName),
  InviteToTempInvite: (id: string, friendIds: string[]): Promise<TempInvite> => call('InviteToTempInvite', id, friendIds),
  CloseTempInvite: (id: string): Promise<void> => call('CloseTempInvite', id),
  TempInviteStates: (addresses: string[]): Promise<TempInviteState[]> => call('TempInviteStates', addresses),
  JoinTempInvite: (address: string): Promise<void> => call('JoinTempInvite', address),
  // ── Поділитися збіркою / сервером (повне копіювання другові через P2P) ──
  ShareStart: (req: ShareStartRequest): Promise<ShareOutView> => call('ShareStart', req),
  ShareInviteMore: (id: string, friendIds: string[]): Promise<void> => call('ShareInviteMore', id, friendIds),
  ShareStop: (id: string): Promise<void> => call('ShareStop', id),
  ShareSenderPause: (id: string, accountId: string, paused: boolean): Promise<void> => call('ShareSenderPause', id, accountId, paused),
  ShareRevokeRecipient: (id: string, accountId: string): Promise<void> => call('ShareRevokeRecipient', id, accountId),
  ListShareOut: (): Promise<ShareOutView[]> => call('ListShareOut'),
  ShareAccept: (encoded: string, fromAccountId: string, fromName: string): Promise<ShareInView> => call('ShareAccept', encoded, fromAccountId, fromName),
  SharePause: (sid: string): Promise<void> => call('SharePause', sid),
  ShareResume: (sid: string): Promise<void> => call('ShareResume', sid),
  ShareCancelIn: (sid: string): Promise<void> => call('ShareCancelIn', sid),
  ShareDeviceTag: (): Promise<string> => call('ShareDeviceTag'),
  GetShareServerTree: (serverId: string): Promise<{ items: ExportCandidate[]; required: string[] }> => call('GetShareServerTree', serverId),
  SharePreview: (encoded: string, fromAccountId: string): Promise<SharePreviewView> => call('SharePreview', encoded, fromAccountId),
  ShareDiscard: (sid: string, encoded: string, fromAccountId: string): Promise<void> => call('ShareDiscard', sid, encoded, fromAccountId),
  ListShareIn: (): Promise<ShareInView[]> => call('ListShareIn'),
  ListShareRecoverable: (): Promise<ShareRecoverable[]> => call('ListShareRecoverable'),
  ShareResumeRecovered: (mid: string): Promise<ShareInView> => call('ShareResumeRecovered', mid),
  ShareDiscardRecovered: (mid: string): Promise<void> => call('ShareDiscardRecovered', mid),
  // ── Віджет сповіщень у трей (notify.html) ──
  // TakeNotifyPopup — накопичені сповіщення при монтуванні вікна (подія
  // notify:push могла прийти раніше, ніж вікно реєструвало слухача).
  TakeNotifyPopup: (): Promise<SocialNotifyPayload[]> => call('TakeNotifyPopup'),
  // SetNotifyPopupSize — фактична висота стосу карток: бекенд ставить розмір
  // вікна і привʼязує його до правого нижнього кута; h<=0 → сховати вікно.
  SetNotifyPopupSize: (w: number, h: number): Promise<void> => call('SetNotifyPopupSize', w, h),
  // Клік по картці: показати/активізувати головне вікно і відкрити діалог
  // (порожній peerAccountId — просто відкрити шухляду «Друзі»).
  OpenChatFromNotification: (peerAccountId: string): Promise<void> => call('OpenChatFromNotification', peerAccountId),
  LogoutSkinAccount: (): Promise<void> => call('LogoutSkinAccount'),
  // Перевірка ніка ДО Google-логіну (available/reason/message) — щоб не
  // ганяти юзера через весь OAuth-браузер лише для того, щоб дізнатись,
  // що нік зайнятий. Дешевий read-only виклик, без сесії.
  CheckSkinNickname: (nickname: string): Promise<NicknameCheckResult> => call('CheckSkinNickname', nickname),
  // Реєстрація ніка і завантаження скіна (base64 PNG, model: classic|slim).
  SetSkinNickname: (nickname: string): Promise<void> => call('SetSkinNickname', nickname),
  // Нік ПОТОЧНОЇ особистості Шаурми (Microsoft або Google) — один вхід, щоб
  // фронтенду не треба знати, який саме вхід пройдено. Без ніка акаунт не
  // отримує ні друзів, ні скінів.
  SetSocialNickname: (nickname: string): Promise<void> => call('SetSocialNickname', nickname),
  // Яка особистість зараз прив'язана до активного акаунта: "microsoft" |
  // "google" | "" (нічого).
  GetSocialIdentity: (): Promise<string> => call('GetSocialIdentity'),
  // Явне звільнення поточного ніка на сервері скінів (кнопка «Звільнити
  // нік» у SkinAccountPanel) — окрема дія від видалення локального
  // акаунта з лаунчера (RemoveAccount більше НЕ звільняє нік автоматично).
  ReleaseSkinNickname: (): Promise<void> => call('ReleaseSkinNickname'),
  UploadSkin: (pngBase64: string, model: string): Promise<void> => call('UploadSkin', pngBase64, model),
  SaveSettings: (s: Settings): Promise<void> => call('SaveSettings', s),
  SaveWindowPosition: (x: number, y: number): Promise<void> => call('SaveWindowPosition', x, y),
  // Поточний стан вікна {maximised, fullscreen} — для кнопки «На весь
  // розмір» при старті (подія window:ui-state могла прийти до підписки).
  GetWindowUIState: (): Promise<{ maximised: boolean; fullscreen: boolean }> => call('GetWindowUIState'),
  SaveSetupProgress: (p: SetupProgress): Promise<void> => call('SaveSetupProgress', p),
  SearchContent: (query: string): Promise<BrowserEntry[]> => call('SearchContent', query),
  SetActiveAccount: (id: string): Promise<void> => call('SetActiveAccount', id),
  SetAppDir: (p: string): Promise<void> => call('SetAppDir', p),
  SetFolderPath: (kind: string, p: string): Promise<void> => call('SetFolderPath', kind, p),
  StartMSLogin: (): Promise<string> => call('StartMSLogin'),
  StopGame: (): Promise<void> => call('StopGame'),
  CancelGameInstall: (instanceId: string): Promise<void> => call('CancelGameInstall', instanceId),
  // Зупиняє КОНКРЕТНУ збірку на активному акаунті — на відміну від
  // StopGame (яка зупиняє «останню запущену»), завжди влучає в потрібну
  // сесію навіть коли на акаунті паралельно йде кілька збірок одночасно.
  StopInstance: (instanceID: string): Promise<void> => call('StopInstance', instanceID),
  // Повне завершення процесу (кнопка X топбара): виставляє прапорець,
  // щоб відкрите вікно консолі не заблокувало вихід з програми.
  QuitLauncher: (): Promise<void> => call('QuitLauncher'),

  // ── Сервери ──
  ListServers: (): Promise<ServerView[]> => call('ListServers'),
  GetServer: (id: string): Promise<ServerView> => call('GetServer', id),
  DetectServerCore: (dir: string): Promise<CoreDetectResult> => call('DetectServerCore', dir),
  AddServerFromFolder: (dir: string, name: string, ramMB: number, acceptEula: boolean): Promise<ServerView> =>
    call('AddServerFromFolder', dir, name, ramMB, acceptEula),
  CreateServer: (req: NewServerRequest): Promise<ServerView> => call('CreateServer', req),
  DeleteServer: (id: string): Promise<void> => call('DeleteServer', id),
  SaveServerConfig: (id: string, patch: Partial<ServerConfigPatch>): Promise<ServerView> => call('SaveServerConfig', id, patch),
  // ChangeServerCore — зміна ядра/версії гри/лоадера ВЖЕ створеного сервера
  // (світи, моди, плагіни й конфіги зберігаються). Сервер має бути зупинений.
  ChangeServerCore: (id: string, req: ChangeCoreRequest): Promise<ServerView> => call('ChangeServerCore', id, req),
  // CheckServerFolder — ID сервера (ім'я теки) коректний і вільний?
  CheckServerFolder: (name: string): Promise<ServerFolderCheck> => call('CheckServerFolder', name),
  // RenameServerFolder — змінити ID сервера (теку на диску). Лише коли
  // сервер зупинено (Java тримає файли світу відкритими).
  RenameServerFolder: (id: string, newFolder: string): Promise<ServerView> => call('RenameServerFolder', id, newFolder),
  StartServer: (id: string): Promise<void> => call('StartServer', id),
  StopServer: (id: string, graceful: boolean): Promise<void> => call('StopServer', id, graceful),
  SendServerCommand: (id: string, cmd: string): Promise<void> => call('SendServerCommand', id, cmd),
  GetServerStatus: (id: string): Promise<ServerStatus> => call('GetServerStatus', id),
  GetServerConsoleSnapshot: (id: string): Promise<ConsoleLine[]> => call('GetServerConsoleSnapshot', id),
  // ── Окреме вікно консолі СЕРВЕРА (як у збірок: console_window.go).
  // Кожен сервер має власне вікно — воно показує буфер/стан САМЕ цього
  // сервера, незалежно від того, скільки серверів запущено паралельно.
  OpenServerConsole: (id: string): Promise<void> => call('OpenServerConsole', id),
  ShowServerConsoleWindowFor: (id: string): Promise<void> => call('ShowServerConsoleWindowFor', id),
  HideServerConsoleWindowFor: (id: string): Promise<void> => call('HideServerConsoleWindowFor', id),
  ToggleServerConsoleWindow: (id: string): Promise<void> => call('ToggleServerConsoleWindow', id),
  IsServerConsoleWindowOpen: (id: string): Promise<boolean> => call('IsServerConsoleWindowOpen', id),
  CheckServerPort: (port: number): Promise<CheckPortResult> => call('CheckServerPort', port),
  CheckServerPortForServer: (serverID: string, port: number): Promise<CheckPortResult> =>
    call('CheckServerPortForServer', serverID, port),
  ScanServerFreePort: (preferred: number): Promise<number> => call('ScanServerFreePort', preferred),
  CheckServerRAM: (ramMB: number): Promise<CheckRAMResult> => call('CheckServerRAM', ramMB),
  // ── Розумний аналізатор запуску сервера ──
  // Перевірки ПЕРЕД стартом (порт/RAM/Java/EULA/диск/ядро/минулий краш) з
  // урахуванням налаштувань лаунчера: показує лише те, що людина не
  // вимкнула (порт показується завжди).
  GetServerLaunchWarnings: (id: string): Promise<ServerPreflight> => call('GetServerLaunchWarnings', id),
  GetServerProperties: (id: string): Promise<PropertiesDoc> => call('GetServerProperties', id),
  SaveServerProperties: (id: string, updates: PropUpdate[]): Promise<void> => call('SaveServerProperties', id, updates),
  // ── Протокольна іконка (server-icon.png 64×64 — бачать гравці в грі) ──
  SetServerIcon: (id: string, data: string): Promise<string> => call('SetServerIcon', id, data),
  GetServerIconData: (id: string): Promise<string> => call('GetServerIconData', id),
  ResetServerIcon: (id: string): Promise<void> => call('ResetServerIcon', id),
  // ── Іконка ЛАУНЧЕРА (картка/списки в UI — окремо від протокольної) ──
  // srcPath — шлях, який повертає BrowseImageFile(); файл копіюється як є.
  SetLauncherIcon: (id: string, srcPath: string): Promise<ServerView> => call('SetLauncherIcon', id, srcPath),
  GetLauncherIconData: (id: string): Promise<string> => call('GetLauncherIconData', id),
  ResetLauncherIcon: (id: string): Promise<ServerView> => call('ResetLauncherIcon', id),
  GetServerStats: (id: string): Promise<ServerLiveStats> => call('GetServerStats', id),
  HasRunningServers: (): Promise<boolean> => call('HasRunningServers'),
  StopAllServers: (): Promise<void> => call('StopAllServers'),
  // P2P ("LAN через інтернет")
  // ClaimP2PAddress — застовбити/отримати адресу. Якщо цей акаунт уже має
  // адресу з цією базовою назвою, повертається ВОНА (reused=true), без нового
  // claim'у — саме тому адреса не губиться між запусками лаунчера.
  ClaimP2PAddress: (baseName: string, label = ''): Promise<ClaimResult> =>
    call('ClaimP2PAddress', baseName, label),
  // ListP2PAddresses — усі адреси, зайняті цим акаунтом.
  ListP2PAddresses: (): Promise<P2PClaimedAddress[]> => call('ListP2PAddresses'),
  // SetP2PRejectTexts — переклади повідомлень, які лаунчер показує гравцеві в самій грі, коли його
  // не пустили на P2P-сервер (мова = мова лаунчера, включно з імпортованими мовами).
  SetP2PRejectTexts: (texts: Record<string, string>): Promise<void> => call('SetP2PRejectTexts', texts),
  // AssignP2PAddress — закріпити одну зі збережених адрес за конкретним
  // сервером (гравець обрав її у списку).
  AssignP2PAddress: (subdomain: string, serverId: string): Promise<void> =>
    call('AssignP2PAddress', subdomain, serverId),
  // TCP-порт = порт сервера. udpPort — порт UDP-тунелю (0 = вимкнено);
  // дефолт у UI = порт сервера, користувач може змінити на будь-який.
  // Сам тунель слухає на порту гри (однаковість портів).
  StartP2PHost: (minecraftPort: string, subdomain: string, udpPort = 0): Promise<P2PHostStatus> =>
    call('StartP2PHost', minecraftPort, subdomain, udpPort),
  // StopP2PHost — зупинити ОДНУ конкретну картку (мультихостинг).
  StopP2PHost: (subdomain: string): Promise<void> => call('StopP2PHost', subdomain),
  GetP2PHostStatus: (subdomain: string): Promise<P2PHostStatus> => call('GetP2PHostStatus', subdomain),
  // ListP2PHosts — усі активні хостингові сесії цього лаунчера.
  ListP2PHosts: (): Promise<P2PHostStatus[]> => call('ListP2PHosts'),
  // HostP2PConnections — хто зараз під'єднаний до цього хоста і з яким пінгом.
  HostP2PConnections: (subdomain: string): Promise<P2PConnection[]> =>
    call('HostP2PConnections', subdomain),
  // Голосовий чат через P2P (UDP): галочка в віджеті P2P-хостингу.
  // Хостингова сторона передає свій порт у StartP2PHost, гостьова — сюди.
  SetP2PUDP: (enabled: boolean, voicePort: number): Promise<void> =>
    call('SetP2PUDP', enabled, voicePort),
  GetP2PUDPStatus: (): Promise<P2PUDPStatus> => call('GetP2PUDPStatus'),
  // Голос + жива діагностика P2P одним викликом (бекенд — єдине джерело правди).
  // force=true — повторити самоперевірку lobby, а не брати кеш на 20 с.
  P2PGetVoiceState: (serverId: string, force = false): Promise<P2PVoiceState> =>
    call('P2PGetVoiceState', serverId, force),
  // Зберігає налаштування голосу й ОДРАЗУ застосовує до запущеного тунелю (без перезапуску).
  // customPort 0 = «Авто» (порт береться з конфігу голосового моду).
  P2PSetVoice: (serverId: string, enabled: boolean, customPort = 0): Promise<P2PVoiceState> =>
    call('P2PSetVoice', serverId, enabled, customPort),
  // Запустити/перезапустити тунель сервера з правильним UDP-портом.
  P2PStartForServer: (serverId: string): Promise<P2PHostStatus> => call('P2PStartForServer', serverId),
  // Синхронізувати тунель зі станом сервера за прапорцем автозапуску.
  P2PEnsureAuto: (serverId: string): Promise<void> => call('P2PEnsureAuto', serverId),
  // Постійне збереження UDP-налаштувань по серверу (замість localStorage).
  GetP2PUDPPref: (serverId: string): Promise<{ found: boolean; enabled: boolean; port: number }> =>
    call('GetP2PUDPPref', serverId),
  SetP2PUDPPref: (serverId: string, enabled: boolean, port: number): Promise<void> =>
    call('SetP2PUDPPref', serverId, enabled, port),
  // GetP2PStatus — чи вмикнена фіча в налаштуваннях і чи готовий до неї
  // сервер (Headscale). UI ховає блок P2P, коли фіча гарантовано не працює.
  GetP2PStatus: (): Promise<P2PStatus> => call('GetP2PStatus'),
  ReleaseP2PAddress: (subdomain: string): Promise<void> => call('ReleaseP2PAddress', subdomain),
  EnsureP2PGuestService: (): Promise<void> => call('EnsureP2PGuestService'),
  // Режим доступу, списки, співхости, спільні кімнати (оренда хоста).
  P2PSetAccess: (subdomain: string, access: P2PAccess): Promise<void> => call('P2PSetAccess', subdomain, access),
  // P2PRoomAccess — поточний режим доступу кімнати. На відміну від статусу
  // хоста, працює й коли сервер/хостинг не запущені (перемикач у UI має
  // показувати справжній режим, а не порожнечу).
  P2PRoomAccess: (subdomain: string): Promise<string> => call('P2PRoomAccess', subdomain),
  P2PACL: (address: string): Promise<P2PACLList> => call('P2PACL', address),
  P2PACLApply: (address: string, add: P2PEntry[], remove: P2PEntry[]): Promise<P2PApplyResult> =>
    call('P2PACLApply', address, add, remove),
  P2PCohosts: (address: string): Promise<P2PCohostList> => call('P2PCohosts', address),
  P2PCohostsApply: (address: string, add: P2PEntry[], remove: P2PEntry[]): Promise<P2PApplyResult> =>
    call('P2PCohostsApply', address, add, remove),
  P2PKick: (address: string): Promise<boolean> => call('P2PKick', address),
  P2PShared: (): Promise<P2PSharedRoom[]> => call('P2PShared'),
  P2PHostStartCohost: (minecraftPort: string, address: string, udpPort = 0): Promise<P2PHostStatus> =>
    call('P2PHostStartCohost', minecraftPort, address, udpPort),
  P2PHostStop: (address: string): Promise<void> => call('P2PHostStop', address),
  DeleteServerWithFiles: (id: string): Promise<void> => call('DeleteServerWithFiles', id),
  // Моди / плагіни сервера
  // ListServerMods повертає ПОВНИЙ ModEntry (а не тонкий ServerModInfo):
  // бекенд сканує mods/ сервера тим самим парсером, що й менеджер модів
  // збірки (internal/servers/mods.go → mods.Manager.ScanModsFull), тож
  // приходять іконка/версія/автор/environment/sha1. Тип у цьому файлі був
  // ServerModInfo[] — через це svelte-check падав на ServerContentTab
  // (Type 'ServerModInfo[]' is not assignable to type 'ModEntry[]'), а дані
  // з бекенду нікуди не дівались.
  ListServerMods: (serverID: string): Promise<ModEntry[]> => call('ListServerMods', serverID),
  // ExportServerModList — список модів СЕРВЕРА у тому самому форматі, що
  // ExportModList для збірок (html/markdown/plain/json/csv).
  ExportServerModList: (serverID: string, opts: ModListExportOptions): Promise<string> =>
    call('ExportServerModList', serverID, opts),
  // CompareServerMods — порівняння модів двох серверів (що є лише на
  // одному і де розійшлися версії).
  CompareServerMods: (serverA: string, serverB: string): Promise<ServerModsCompareResult> =>
    call('CompareServerMods', serverA, serverB),
  // CheckServerModUpdates — друга, мережева хвиля (єдиний batch-запит до
  // Modrinth за SHA1) поверх уже показаного списку з диска: патчить
  // hasUpdate/latestVersion/latestUrl/iconUrl в картках.
  CheckServerModUpdates: (serverID: string): Promise<ModEntry[]> => call('CheckServerModUpdates', serverID),
  ToggleServerMod: (serverID: string, fileName: string): Promise<void> => call('ToggleServerMod', serverID, fileName),
  DeleteServerMod: (serverID: string, fileName: string): Promise<void> => call('DeleteServerMod', serverID, fileName),
  ListServerPlugins: (serverID: string): Promise<ServerPluginInfo[]> => call('ListServerPlugins', serverID),
  ToggleServerPlugin: (serverID: string, fileName: string): Promise<void> => call('ToggleServerPlugin', serverID, fileName),
  DeleteServerPlugin: (serverID: string, fileName: string): Promise<void> => call('DeleteServerPlugin', serverID, fileName),
  // Світи + бекапи сервера
  ListWorlds: (serverID: string): Promise<WorldInfo[]> => call('ListWorlds', serverID),
  CreateWorldBackup: (serverID: string, worldName: string): Promise<string> => call('CreateWorldBackup', serverID, worldName),
  ListWorldBackups: (serverID: string, worldName: string): Promise<BackupInfo[]> => call('ListWorldBackups', serverID, worldName),
  // GetServerPlayers — хто зараз на сервері (нік + час входу). Реєстр веде
  // бекенд, читаючи рядки логу java; події входу/виходу летять у "server:player".
  GetServerPlayers: (serverID: string): Promise<ServerPlayer[]> => call('GetServerPlayers', serverID),
  // GetServerPlayerPlaytimes — накопичений час гри кожного гравця (stats світу).
  GetServerPlayerPlaytimes: (serverID: string): Promise<ServerPlayerPlaytime[]> => call('GetServerPlayerPlaytimes', serverID),
  RestoreWorldBackup: (serverID: string, worldName: string, backupID: string): Promise<void> => call('RestoreWorldBackup', serverID, worldName, backupID),
  DeleteWorldBackup: (serverID: string, worldName: string, backupID: string): Promise<void> => call('DeleteWorldBackup', serverID, worldName, backupID),
  // ШІ-аналіз крашів сервера
  AnalyzeServerCrash: (serverID: string): Promise<CrashAnalysis> => call('AnalyzeServerCrash', serverID),
  HasCrashReport: (serverID: string): Promise<boolean> => call('HasCrashReport', serverID),
  // Мережа Velocity (проксі + backend-сервери)
  NetworkGetView: (proxyServerId: string): Promise<NetworkView> => call('NetworkGetView', proxyServerId),
  NetworkSave: (proxyServerId: string, members: NetworkMemberView[]): Promise<void> => call('NetworkSave', proxyServerId, members),
  NetworkStart: (proxyServerId: string): Promise<void> => call('NetworkStart', proxyServerId),
  NetworkStop: (proxyServerId: string): Promise<void> => call('NetworkStop', proxyServerId),
  AddServerModFile: (serverID: string, kind: string, fileName: string, dataBase64: string): Promise<void> =>
    call('AddServerModFile', serverID, kind, fileName, dataBase64),
  // ── Браузер контенту ДЛЯ СЕРВЕРІВ ──
  // ServerBrowserContext — версія MC / лоадер / уже встановлені файли,
  // щоб вкладка «Знайти онлайн» шукала сумісний з САМЕ ЦИМ сервером
  // контент (раніше для серверів можна було лише підсунути .jar з диска).
  ServerBrowserContext: (serverID: string): Promise<ServerBrowserContextView> => call('ServerBrowserContext', serverID),
  // InstallServerBrowserVersion — встановлення конкретної версії
  // Modrinth/CurseForge у mods/ або plugins/ сервера (kind: "mods" |
  // "plugins"). versionID="" = найкраща сумісна версія.
  InstallServerBrowserVersion: (serverID: string, source: string, projectID: string, versionID: string, kind: string, replace: boolean): Promise<BrowserInstallResult> =>
    call('InstallServerBrowserVersion', serverID, source, projectID, versionID, kind, replace),
  // Версії для селектора "Новий сервер" — той самий провайдер, що й
  // "Нова збірка → Вручну" (vanilla/fabric/forge/neoforge/quilt), плюс
  // власні Paper/Purpur білди.
  GetServerMCVersions: (): Promise<MCVersionEntry[]> => call('GetServerMCVersions'),
  GetServerDefaultMCVersion: (): Promise<string> => call('GetServerDefaultMCVersion'),
  GetServerCoreVersions: (core: string, mcVersion: string): Promise<ServerCoreVersionEntry[]> =>
    call('GetServerCoreVersions', core, mcVersion),
  GetServerAvailableCores: (mcVersion: string): Promise<string[]> => call('GetServerAvailableCores', mcVersion),

  // ── Гардероб ──
  // Пресети зберігаються окремо для кожного ліцензійного акаунта, тому
  // майже всі методи приймають accountID активного акаунта (з пропа
  // <Wardrobe activeAccount>).
  GetWardrobePresets: (accountID: string): Promise<SkinPreset[]> => call('GetWardrobePresets', accountID),
  DeleteWardrobePreset: (accountID: string, presetId: string): Promise<void> => call('DeleteWardrobePreset', accountID, presetId),
  // Переставляння пресетів drag&drop: повний список presetIDs у новому
  // порядку → бекенд зберігає і повертає оновлений список.
  ReorderWardrobePresets: (accountID: string, presetIds: string[]): Promise<SkinPreset[]> =>
    call('ReorderWardrobePresets', accountID, presetIds),
  // Оновлення існуючого пресету (назва, тип рук, плащ) — кнопка «Зберегти»
  // в редакторі пресету. Плащ за URL качається в локальний кеш на бекенді,
  // щоб картка пресету одразу показала новий вигляд.
  UpdateWardrobePreset: (accountID: string, presetId: string, name: string, slim: boolean, capeUrl: string): Promise<SkinPreset> =>
    call('UpdateWardrobePreset', accountID, presetId, name, slim, capeUrl),
  SaveWardrobeLocalSkin: (accountID: string, name: string, skinPngBase64: string, slim: boolean): Promise<SkinPreset> =>
    call('SaveWardrobeLocalSkin', accountID, name, skinPngBase64, slim),
  // Оновити СКІН існуючого пресету (кнопка «Обрати файл» у редакторі,
  // коли пресет уже є): новий PNG на місце старого, БЕЗ створення нового
  // пресету (баг: «редагую пресет і змінюю скін — створюється новий»).
  UpdateWardrobePresetSkin: (accountID: string, presetId: string, skinPngBase64: string, slim: boolean): Promise<SkinPreset> =>
    call('UpdateWardrobePresetSkin', accountID, presetId, skinPngBase64, slim),
  LookupWardrobePlayer: (nickname: string): Promise<PlayerProfile> => call('LookupWardrobePlayer', nickname),
  CopyWardrobePlayerSkin: (accountID: string, profile: PlayerProfile, includeCape: boolean): Promise<SkinPreset> =>
    call('CopyWardrobePlayerSkin', accountID, profile, includeCape),
  ApplyWardrobePreset: (accountID: string, presetId: string): Promise<void> => call('ApplyWardrobePreset', accountID, presetId),
  GetWardrobeOwnedCapes: (accountID: string): Promise<CapeInfo[]> => call('GetWardrobeOwnedCapes', accountID),
  SetWardrobeActiveCape: (accountID: string, capeId: string): Promise<void> => call('SetWardrobeActiveCape', accountID, capeId),
  GetWardrobeTextureDataURL: (path: string): Promise<string> => call('GetWardrobeTextureDataURL', path),
  GetWardrobeCapeDataURL: (capeUrl: string): Promise<string> => call('GetWardrobeCapeDataURL', capeUrl),
  GetWardrobeRemoteSkinDataURL: (skinUrl: string): Promise<string> => call('GetWardrobeRemoteSkinDataURL', skinUrl),
  // Пресет, чий скін+плащ побайтово збігається з тим, що зараз реально
  // застосовано на акаунт на Mojang — щоб виділити його як активний.
  GetWardrobeActivePresetID: (accountID: string): Promise<string> => call('GetWardrobeActivePresetID', accountID),
  // Поточний вигляд акаунта на Mojang (активний скін+плащ) для прев'ю у
  // 3D-переглядачі, коли жоден пресет не збігається з виглядом акаунта.
  GetWardrobeActiveLook: (accountID: string): Promise<ActiveLook | null> => call('GetWardrobeActiveLook', accountID),
  GetFriendLook: (nickname: string): Promise<ActiveLook | null> => call('GetFriendLook', nickname),
  // Створити пресет з поточного вигляду акаунта (кнопка "Редагувати скін",
  // коли поточний скін/плащ не мають збереженого пресету).
  SaveWardrobeActiveLookPreset: (accountID: string, name: string): Promise<SkinPreset> =>
    call('SaveWardrobeActiveLookPreset', accountID, name),

  // ── Кастомні збірки (створення вручну / імпорт .mrpack, .zip) ──
  GetCustomPackMCVersions: (): Promise<MCVersionEntry[]> => call('GetCustomPackMCVersions'),
  GetCustomPackLoaderVersions: (loader: string, mcVersion: string): Promise<LoaderVersionEntry[]> =>
    call('GetCustomPackLoaderVersions', loader, mcVersion),
  GetCustomPackLoaderAvailability: (mcVersion: string): Promise<string[]> =>
    call('GetCustomPackLoaderAvailability', mcVersion),
  GetCustomPackDefaultMCVersion: (): Promise<string> => call('GetCustomPackDefaultMCVersion'),
  HasCurseForgeAPIKey: (): Promise<boolean> => call('HasCurseForgeAPIKey'),
  BrowseImageFile: (): Promise<string> => call('BrowseImageFile'),
  BrowseModpackArchive: (): Promise<string> => call('BrowseModpackArchive'),
  CreateCustomPack: (p: CustomPackFormParams): Promise<CustomPack> => call('CreateCustomPack', p),
  UpdateCustomPack: (id: string, p: CustomPackFormParams): Promise<CustomPack> => call('UpdateCustomPack', id, p),
  GetCustomPack: (id: string): Promise<CustomPack> => call('GetCustomPack', id),
  ToggleBlockedModDownloaded: (packId: string, fileName: string, downloaded: boolean): Promise<void> =>
    call('ToggleBlockedModDownloaded', packId, fileName, downloaded),
  // Кнопка «Полагодити»: перевстановлює залежності Minecraft і перевіряє
  // файли збірки на пошкодження (кастомні — синхронно, Shaurma — через синк).
  RepairPack: (id: string): Promise<void> => call('RepairPack', id),
  // Кнопка «Переставити заново»: видаляє всі файли збірки, зберігаючи
  // налаштування лаунчера (per-instance конфіг + асети).
  ReinstallPack: (id: string): Promise<void> => call('ReinstallPack', id),
  DetectModpackArchive: (archivePath: string): Promise<DetectedPack> => call('DetectModpackArchive', archivePath),
  ImportModpackArchive: (archivePath: string, detected: DetectedPack, name: string, color: string, iconSrcPath: string, icon: string): Promise<CustomPack> =>
    call('ImportModpackArchive', archivePath, detected, name, color, iconSrcPath, icon),
  CancelModpackImport: (packID: string): Promise<void> => call('CancelModpackImport', packID),
  ListExportCandidates: (packId: string): Promise<ExportCandidate[]> => call('ListExportCandidates', packId),
  ExportPack: (req: ExportPackRequest): Promise<ExportPackResult> => call('ExportPack', req),
  // ── .shaurmpack — власний формат (окремий від ExportPack/mrpack вище) ──
  BrowseShaurmpackFile: (): Promise<string> => call('BrowseShaurmpackFile'),
  PreviewShaurmpack: (path: string): Promise<ShaurmpackPreview> => call('PreviewShaurmpack', path),
  ImportShaurmpackFile: (path: string, name: string, color: string, iconSrcPath: string, icon: string): Promise<CustomPack> =>
    call('ImportShaurmpackFile', path, name, color, iconSrcPath, icon),
  // ФОНОВИЙ імпорт .shaurmpack «як модпак з браузера» (двічі клік по файлу):
  // повертається одразу, прогрес іде подіями sync:progress на картці збірки.
  StartShaurmpackImport: (path: string): Promise<CustomPack> => call('StartShaurmpackImport', path),
  ExportShaurmpack: (req: ExportShaurmpackRequest): Promise<ExportShaurmpackResult> => call('ExportShaurmpack', req),
  // ── .shaurmbundle — груповий формат (кілька збірок в одному файлі:
  // експорт папки «Мої збірки», бекап/перенесення набору) ──
  PreviewShaurmbundle: (path: string): Promise<ShaurmbundlePreview> => call('PreviewShaurmbundle', path),
  ImportShaurmbundleFile: (path: string, folderId: string): Promise<ShaurmbundleImportResult> =>
    call('ImportShaurmbundleFile', path, folderId),
  ExportShaurmbundle: (req: ExportShaurmbundleRequest): Promise<ExportShaurmbundleResult> => call('ExportShaurmbundle', req),
  // Шлях файлу збірки (.shaurmpack/.shaurmbundle), відкритого через
  // Windows-асоціацію («Open with»): фронтенд забирає один раз на маунті
  // і авто-імпортує збірку (подія packfile:open могла прийти ДО підписки).
  TakePendingPackImportFile: (): Promise<string> => call('TakePendingPackImportFile'),
  GetExportSettings: (packId: string): Promise<string> => call('GetExportSettings', packId),
  SaveExportSettings: (packId: string, data: string): Promise<void> => call('SaveExportSettings', packId, data),

  // ── Дублювання збірки ──
  // Дерево вибору файлів для дублювання (той самий формат, що експорт).
  GetDuplicatePackTree: (packId: string): Promise<ExportCandidate[]> => call('GetDuplicatePackTree', packId),
  // Перевірка, чи запропонований ID копії вже зайнятий (і підказка вільного).
  CheckDuplicatePackID: (id: string): Promise<DuplicatePackIDCheck> => call('CheckDuplicatePackID', id),
  // Створити копію збірки (вибір файлів + іконка/колір + налаштування).
  DuplicatePack: (req: DuplicatePackRequest): Promise<DuplicatePackResult> => call('DuplicatePack', req),
  // Збережені параметри останнього дублювання — окремо для КОЖНОЇ збірки-
  // джерела (pack_id у duplicate_settings), той самий принцип, що
  // Get/SaveExportSettings нижче.
  GetDuplicateSettings: (packId: string): Promise<string> => call('GetDuplicateSettings', packId),
  SaveDuplicateSettings: (packId: string, data: string): Promise<void> => call('SaveDuplicateSettings', packId, data),

  // ── Файловий провідник збірки (модовані/кастомні збірки) ──
  ListPackFiles: (packID: string, relPath: string): Promise<PackDirListing> => call('ListPackFiles', packID, relPath),
  ReadPackTextFile: (packID: string, relPath: string): Promise<PackFileContent> => call('ReadPackTextFile', packID, relPath),
  WritePackTextFile: (packID: string, relPath: string, content: string): Promise<void> => call('WritePackTextFile', packID, relPath, content),
  RenamePackEntry: (packID: string, relPath: string, newName: string): Promise<void> => call('RenamePackEntry', packID, relPath, newName),
  CreatePackFolder: (packID: string, relPath: string, folderName: string): Promise<void> => call('CreatePackFolder', packID, relPath, folderName),
  CreatePackFile: (packID: string, relPath: string, fileName: string): Promise<void> => call('CreatePackFile', packID, relPath, fileName),
  DeletePackEntry: (packID: string, relPath: string): Promise<void> => call('DeletePackEntry', packID, relPath),
  RevealPackFile: (packID: string, relPath: string): Promise<void> => call('RevealPackFile', packID, relPath),
  GetPackFileAsset: (packID: string, relPath: string): Promise<string> => call('GetPackFileAsset', packID, relPath),

  // ── Файловий провідник сервера ──
  ListServerFiles: (serverID: string, relPath: string): Promise<PackDirListing> => call('ListServerFiles', serverID, relPath),
  ReadServerTextFile: (serverID: string, relPath: string): Promise<PackFileContent> => call('ReadServerTextFile', serverID, relPath),
  WriteServerTextFile: (serverID: string, relPath: string, content: string): Promise<void> => call('WriteServerTextFile', serverID, relPath, content),
  RenameServerEntry: (serverID: string, relPath: string, newName: string): Promise<void> => call('RenameServerEntry', serverID, relPath, newName),
  CreateServerFolder: (serverID: string, relPath: string, folderName: string): Promise<void> => call('CreateServerFolder', serverID, relPath, folderName),
  CreateServerFile: (serverID: string, relPath: string, fileName: string): Promise<void> => call('CreateServerFile', serverID, relPath, fileName),
  DeleteServerEntry: (serverID: string, relPath: string): Promise<void> => call('DeleteServerEntry', serverID, relPath),
  RevealServerFile: (serverID: string, relPath: string): Promise<void> => call('RevealServerFile', serverID, relPath),
  GetServerFileAsset: (serverID: string, relPath: string): Promise<string> => call('GetServerFileAsset', serverID, relPath),

  // ── Галерея скріншотів збірки (Шаурма і кастомні) ──
  ListScreenshots: (packID: string): Promise<ScreenshotEntry[]> => call('ListScreenshots', packID),
  ScreenshotsSignature: (packID: string): Promise<string> => call('ScreenshotsSignature', packID),
  GetScreenshotAsset: (packID: string, fileName: string): Promise<string> => call('GetScreenshotAsset', packID, fileName),
  GetScreenshotThumbnail: (packID: string, fileName: string): Promise<string> => call('GetScreenshotThumbnail', packID, fileName),
  RenameScreenshot: (packID: string, fileName: string, newName: string): Promise<void> => call('RenameScreenshot', packID, fileName, newName),
  DeleteScreenshot: (packID: string, fileName: string): Promise<void> => call('DeleteScreenshot', packID, fileName),
  SetScreenshotMeta: (packID: string, fileName: string, title: string, description: string): Promise<void> => call('SetScreenshotMeta', packID, fileName, title, description),
  RevealScreenshot: (packID: string, fileName: string): Promise<void> => call('RevealScreenshot', packID, fileName),
  OpenScreenshotInDefaultApp: (packID: string, fileName: string): Promise<void> => call('OpenScreenshotInDefaultApp', packID, fileName),
  CopyScreenshotPathToClipboard: (packID: string, fileName: string): Promise<void> => call('CopyScreenshotPathToClipboard', packID, fileName),
  CopyScreenshotToClipboard: (packID: string, fileName: string): Promise<void> => call('CopyScreenshotToClipboard', packID, fileName),
  CopyEditedImageToClipboard: (dataURL: string): Promise<void> => call('CopyEditedImageToClipboard', dataURL),
  SaveEditedScreenshot: (packID: string, fileName: string, dataURL: string): Promise<void> => call('SaveEditedScreenshot', packID, fileName, dataURL),
  SaveEditedScreenshotAsCopy: (packID: string, fileName: string, dataURL: string): Promise<string> => call('SaveEditedScreenshotAsCopy', packID, fileName, dataURL),

  // ── Консоль: перемикач акаунтів ──
  GetConsoleAccounts: (buildID: string): Promise<ConsoleAccountInfo[]> => call('GetConsoleAccounts', buildID),
  // (accountID, buildID) — порядок аргументів як у Go-сигнатурі бекенду.
  SwitchConsoleAccount: (accountID: string, buildID: string): Promise<void> => call('SwitchConsoleAccount', accountID, buildID),

  // ── Браузер контенту ──
  // categories — кома-розділений список тегів-категорій (порожній =
  // без фільтра). Сорт «featured» (рекомендовані) доступний для Modrinth.
  // client/server/openSource — Modrinth-стиль фільтри середовища та
  // «Open source» (ліцензія); застосовуються лише до Modrinth-результатів.
  BrowserSearch: (query: string, projectType: string, source: string, mcVersion: string, loader: string, categories: string, sort: string, client: boolean, server: boolean, openSource: boolean, limit: number, offset: number): Promise<BrowserSearchResult> =>
    call('BrowserSearch', query, projectType, source, mcVersion, loader, categories, sort, client, server, openSource, limit, offset),
  // ── Пошук по джерелах ОКРЕМО, без взаємного очікування ──
  // На відміну від BrowserSearch (чекає ОБИДВА джерела), ці два методи
  // викликаються паралельно (Promise.allSettled/без await одне одного) —
  // кожен повертає свій результат щойно готовий: якщо Modrinth відповів
  // за 300мс, а CurseForge гальмує кілька секунд (rate limit 4 запити/с),
  // список Modrinth рендериться одразу, не чекаючи CurseForge.
  BrowserSearchModrinth: (query: string, projectType: string, mcVersion: string, loader: string, categories: string, sort: string, client: boolean, server: boolean, openSource: boolean, limit: number, offset: number, packID: string): Promise<BrowserSearchResult> =>
    call('BrowserSearchModrinth', query, projectType, mcVersion, loader, categories, sort, client, server, openSource, limit, offset, packID),
  BrowserSearchCurseForge: (query: string, projectType: string, mcVersion: string, loader: string, categories: string, sort: string, limit: number, packID: string): Promise<BrowserSearchResult> =>
    call('BrowserSearchCurseForge', query, projectType, mcVersion, loader, categories, sort, limit, packID),
  // Те саме, але без суто клієнтських модів — для серверної цілі (CurseForge
  // не віддає поле «Environment» в API, тож відсів іде за тегами файлів).
  BrowserSearchCurseForgeForServer: (query: string, projectType: string, mcVersion: string, loader: string, categories: string, sort: string, limit: number, packID: string): Promise<BrowserSearchResult> =>
    call('BrowserSearchCurseForgeForServer', query, projectType, mcVersion, loader, categories, sort, limit, packID),
  GetBrowserProjectDetail: (source: string, projectId: string): Promise<BrowserProjectDetail> =>
    call('GetBrowserProjectDetail', source, projectId),
  // projectType — тип контенту (mod/modpack/resourcepack/shader/datapack):
  // від нього залежить, чи фільтрує бекенд версії за лоадером збірки
  // (для ресурспаків/датапаків/шейдерів лоадер не застосовується).
  GetBrowserProjectVersions: (source: string, projectId: string, projectType: string, mcVersion: string, loader: string): Promise<BrowserVersionView[]> =>
    call('GetBrowserProjectVersions', source, projectId, projectType, mcVersion, loader),
  // «Найкраща» сумісна версія під (mc, loader) збірки для типу контенту
  // (release → beta → alpha, як Modrinth app). Використовується для
  // АВТОМАТИЧНОГО підбору версії, коли обрано збірку — ручний вибір
  // потрібен лише без збірки.
  GetBrowserRecommendedVersion: (source: string, projectId: string, projectType: string, mcVersion: string, loader: string): Promise<BrowserVersionView | null> =>
    call('GetBrowserRecommendedVersion', source, projectId, projectType, mcVersion, loader),
  // projectType — тип контенту (mod/modpack/resourcepack/shader/datapack):
  // від нього залежить, чи фільтруємо сумісні збірки за лоадером
  // (для ресурспаків/датапаків/шейдерів лоадер не враховується).
  GetBrowserCompatiblePacks: (source: string, projectId: string, projectType: string, contentDir: string, versionId?: string): Promise<BrowserCompatiblePack[]> =>
    call('GetBrowserCompatiblePacks', source, projectId, projectType, contentDir, versionId ?? ''),
  // Теги-категорії для сайдбара фільтра (Modrinth + CurseForge, з іконками).
  GetBrowserCategories: (projectType: string): Promise<BrowserCategory[]> =>
    call('GetBrowserCategories', projectType),
  // Версії гри для фільтра браузера: пріоритет — теги Modrinth /tag/game_version
  // (лише версії з контентом), fallback — повний Mojang-маніфест.
  GetBrowserGameVersions: (): Promise<BrowserGameVersion[]> =>
    call('GetBrowserGameVersions'),
  // Лоадери для фільтра браузера з /tag/loader Modrinth (з SupportedProjectTypes),
  // щоб фільтр показував лише релевантні поточному типу контенту.
  GetBrowserLoaders: (): Promise<BrowserLoaderTag[]> =>
    call('GetBrowserLoaders'),
  // Аналіз «вже встановлено» у збірці (по sha1, як Modrinth app): мапа
  // projectId → встановлений файл. Для бейджів на картках і сторінці.
  GetBrowserInstalledMap: (packId: string): Promise<Record<string, BrowserInstalledItem>> =>
    call('GetBrowserInstalledMap', packId),
  // Те саме, але для СЕРВЕРА (mods/ + plugins/ у теці сервера, а не
  // збірки) — сторінка проєкту в браузері, відкрита з packContext.isServer
  // (див. ModpackPage.svelte), показує бейдж «Встановлено»/«Оновити» саме
  // для цього виклику.
  GetServerBrowserInstalledMap: (serverId: string): Promise<Record<string, BrowserInstalledItem>> =>
    call('GetServerBrowserInstalledMap', serverId),
  // Обернений запит: у яких збірках користувача вже встановлено конкретний
  // проєкт (сторінка контенту, компактний блок «Також встановлено у»).
  GetBrowserProjectInstalledPacks: (projectId: string): Promise<BrowserInstalledInPack[]> =>
    call('GetBrowserProjectInstalledPacks', projectId),

  // ── Закладки («зірочка») та колекції (групи контенту) ──────────────
  // Закладка не прив'язана до збірки — той самий проєкт видно в розділі
  // "Збережено" незалежно від того, у скількох збірках він встановлений.
  IsBookmarked: (source: string, projectId: string): Promise<boolean> =>
    call('IsBookmarked', source, projectId),
  // Один виклик на весь видимий список результатів пошуку — картки
  // звіряють свій (source,id) з цим набором локально, замість окремого
  // IPC-виклику IsBookmarked на кожну з ~30 карток сторінки.
  GetBookmarkedKeys: (): Promise<string[]> =>
    call('GetBookmarkedKeys'),
  ToggleBookmark: (entry: BrowserEntry): Promise<boolean> =>
    call('ToggleBookmark', entry),
  GetBookmarkedEntries: (): Promise<BrowserEntry[]> =>
    call('GetBookmarkedEntries'),
  GetCollections: (): Promise<BrowserCollection[]> =>
    call('GetCollections'),
  // ID колекцій, де ЦЕЙ проєкт уже є — використовується меню «Додати в
  // колекцію», щоб позначити пункти, куди додавати повторно не потрібно.
  GetEntryCollectionIDs: (source: string, projectId: string): Promise<string[]> =>
    call('GetEntryCollectionIDs', source, projectId),
  CreateCollection: (name: string, description: string, icon: string, color: string): Promise<BrowserCollection> =>
    call('CreateCollection', name, description, icon, color),
  RenameCollection: (id: string, name: string, description: string, icon: string, color: string): Promise<BrowserCollection> =>
    call('RenameCollection', id, name, description, icon, color),
  DeleteCollection: (id: string): Promise<void> =>
    call('DeleteCollection', id),
  GetCollectionItems: (collectionId: string): Promise<BrowserEntry[]> =>
    call('GetCollectionItems', collectionId),
  GetCollectionVersionInfo: (collectionId: string): Promise<Record<string, CollectionItemVersion>> =>
    call('GetCollectionVersionInfo', collectionId),
  AddToCollection: (collectionId: string, entry: BrowserEntry, versionId: string, versionNumber: string): Promise<void> =>
    call('AddToCollection', collectionId, entry, versionId, versionNumber),
  RemoveFromCollection: (collectionId: string, source: string, projectId: string): Promise<void> =>
    call('RemoveFromCollection', collectionId, source, projectId),
  // План ПЕРЕД встановленням: показує що станеться з кожним записом
  // (нове/оновлення/вже стоїть/несумісне) — виклик нічого не змінює.
  PlanCollectionInstall: (collectionId: string, packId: string): Promise<CollectionInstallPlanItem[]> =>
    call('PlanCollectionInstall', collectionId, packId),
  // skipProjectIds — записи, які юзер обрав пропустити після перегляду
  // плану (напр. позначені "incompatible").
  InstallCollectionToPack: (collectionId: string, packId: string, skipProjectIds: string[]): Promise<BrowserInstallResult[]> =>
    call('InstallCollectionToPack', collectionId, packId, skipProjectIds),
  // Те саме, але ціль — СЕРВЕР (моди/плагіни; решту типів сервер не тримає).
  PlanCollectionInstallToServer: (collectionId: string, serverId: string): Promise<CollectionInstallPlanItem[]> =>
    call('PlanCollectionInstallToServer', collectionId, serverId),
  InstallCollectionToServer: (collectionId: string, serverId: string, skipProjectIds: string[]): Promise<BrowserInstallResult[]> =>
    call('InstallCollectionToServer', collectionId, serverId, skipProjectIds),

  // ── Тимчасові моди («становлення на строк») ──────────────────────
  // Прострочення обробляється у фоні на бекенді (раз/годину + одразу при
  // старті лаунчера) — фронтенд лише читає поточний стан і ставить/знімає
  // мітки, не рахує самостійно, коли саме мод має зникнути.
  GetTempMods: (packId: string): Promise<TempModInfo[]> =>
    call('GetTempMods', packId),
  // expiresAtISO — RFC3339 (напр. new Date(...).toISOString()). action:
  // 'delete' | 'disable' | '' (порожньо = взяти дефолт з налаштувань).
  SetModExpiry: (packId: string, fileName: string, expiresAtISO: string, action: string, includeDependencies: boolean): Promise<void> =>
    call('SetModExpiry', packId, fileName, expiresAtISO, action, includeDependencies),
  RemoveModExpiry: (packId: string, fileName: string): Promise<void> =>
    call('RemoveModExpiry', packId, fileName),

  // Скидає кеш аналізу «вже встановлено» для збірки (після встановлення/
  // оновлення контенту) — щоб наступний аналіз одразу показав нові бейджі.
  InvalidateBrowserInstalledCache: (packId: string): Promise<void> =>
    call('InvalidateBrowserInstalledCache', packId),

  // ── Історія пошукових запитів («останні, що дали результат») ──────
  GetSearchHistory: (): Promise<SearchHistoryItem[]> =>
    call('GetSearchHistory'),
  // Викликається фронтендом лише коли пошук реально дав результат — не
  // на кожен символ під час набору.
  AddSearchHistory: (query: string, projectType: string, resultCount: number): Promise<void> =>
    call('AddSearchHistory', query, projectType, resultCount),
  RemoveSearchHistoryEntry: (query: string, projectType: string): Promise<void> =>
    call('RemoveSearchHistoryEntry', query, projectType),
  ClearSearchHistory: (): Promise<void> =>
    call('ClearSearchHistory'),

  // Встановлення модпаку як «інтернет-збірки» — ДВА кроки (як Modrinth
  // app: спершу картка «не завантажено», потім клік «Завантажити»):
  //   1) CreateBrowserModpack — реєструє Pack з іконкою/кольором і
  //      прямим посиланням на архів, файлів гри ще немає;
  //   2) DownloadBrowserModpack — качає архів за збереженим URL і
  //      імпортує в ТОЙ САМИЙ ID (прогрес — подіями sync:progress).
  // Перевірка ПЕРЕД створенням картки: чи модпак (source, projectId) уже
  // є в «Моїх збірках» — щоб показати діалог «Перейти до збірки /
  // Створити копію / Скасувати» замість мовчазних копій -2/-3.
  CheckBrowserModpackInstalled: (source: string, projectId: string): Promise<BrowserModpackInstallCheck> =>
    call('CheckBrowserModpackInstalled', source, projectId),
  CreateBrowserModpack: (source: string, projectId: string, versionId: string, force: boolean): Promise<CustomPack> =>
    call('CreateBrowserModpack', source, projectId, versionId, force),
  DownloadBrowserModpack: (packId: string): Promise<CustomPack> =>
    call('DownloadBrowserModpack', packId),
  // Встановлення мода/ресурспаку/шейдера у конкретну збірку (прогрес —
  // подіями browser:progress).
  InstallBrowserContent: (packId: string, source: string, projectId: string, contentDir: string, replace: boolean): Promise<BrowserInstallResult> =>
    call('InstallBrowserContent', packId, source, projectId, contentDir, replace),
  // Встановлення КОНКРЕТНОЇ версії контенту у збірку (кнопка на сторінці
  // проєкту: фільтр збірки задає ціль, а версію обирає користувач).
  InstallBrowserContentVersion: (packId: string, source: string, projectId: string, versionId: string, contentDir: string, replace: boolean): Promise<BrowserInstallResult> =>
    call('InstallBrowserContentVersion', packId, source, projectId, versionId, contentDir, replace),
  // Встановлення датапака в один або КІЛЬКА світів збірки одразу
  // (saves/<world>/datapacks для кожного). Архів качається один раз і
  // копіюється в кожен вибраний світ. versionId='' → найсвіжіша сумісна
  // версія. Companion-ресурспаки версії ставляться в resourcepacks/ разом
  // з ним (як у Modrinth app).
  InstallDatapackToWorlds: (packId: string, worlds: string[], source: string, projectId: string, versionId: string): Promise<BrowserInstallResult> =>
    call('InstallDatapackToWorlds', packId, worlds, source, projectId, versionId),
  // «Оновити з джерела»: версії модпаку для інтернет-збірки + оновлення.
  GetPackSourceVersions: (packId: string): Promise<BrowserVersionView[]> => call('GetPackSourceVersions', packId),
  UpdatePackFromSource: (packId: string, versionId: string): Promise<CustomPack> => call('UpdatePackFromSource', packId, versionId),

  // ── Менеджер контенту збірки (вкладка «Контент»: ресурспаки/датапаки/шейдери) ──
  // contentDir: mods | resourcepacks | shaderpacks | datapacks (для mods
  // методи делегують у спеціалізований менеджер модів).
  // Список встановленого контенту; вимкнені (.disabled) — Enabled=false.
  ScanContentFiles: (packID: string, contentDir: string): Promise<ContentFileEntry[]> =>
    call('ScanContentFiles', packID, contentDir),
  // Увімкнути/вимкнути (перейменування з .disabled).
  ToggleContentFile: (packID: string, contentDir: string, fileName: string, enabled: boolean): Promise<void> =>
    call('ToggleContentFile', packID, contentDir, fileName, enabled),
  // Видалити (разом з .disabled-двійником).
  DeleteContentFile: (packID: string, contentDir: string, fileName: string): Promise<void> =>
    call('DeleteContentFile', packID, contentDir, fileName),
  // Перенести ОДИН ресурс у іншу збірку (копія зі збереженням стану).
  TransferContentFile: (packID: string, contentDir: string, fileName: string, targetPackID: string, overwrite: boolean): Promise<void> =>
    call('TransferContentFile', packID, contentDir, fileName, targetPackID, overwrite),
  // Відкрити теку контенту збірки у Провіднику.
  OpenContentFolder: (packID: string, contentDir: string): Promise<void> =>
    call('OpenContentFolder', packID, contentDir),
  // Сканувати контент з підтягуванням версій з Modrinth (поточна версія,
  // чи є оновлення, автор, іконка). Аналог перевірки оновлень модів.
  RefreshContentUpdates: (packID: string, contentDir: string, mcVersion: string): Promise<ContentFileEntry[]> =>
    call('RefreshContentUpdates', packID, contentDir, mcVersion),
  // Список доступних версій ресурсу (модалка «Змінити версію»).
  GetContentVersions: (packID: string, contentDir: string, fileName: string, mcVersion: string): Promise<ModVersionOption[]> =>
    call('GetContentVersions', packID, contentDir, fileName, mcVersion),
  // Завантажити обрану версію ресурсу і замінити нею поточний файл.
  InstallContentVersion: (packID: string, contentDir: string, fileName: string, opt: ModVersionOption): Promise<void> =>
    call('InstallContentVersion', packID, contentDir, fileName, opt),
  // URL сторінки ресурсу на Modrinth (для «Скопіювати посилання»).
  GetContentFileLink: (packID: string, contentDir: string, fileName: string, mcVersion: string): Promise<string> =>
    call('GetContentFileLink', packID, contentDir, fileName, mcVersion),
  // ── Pack Doctor (Health Check збірки) ──────────────────────────────────
  // Виконує всі перевірки збірки і повертає зведений звіт.
  RunPackHealthCheck: (packID: string): Promise<HealthCheckReport> =>
    call('RunPackHealthCheck', packID),
  // ── Авто-починка (порт логіки Nivora) ──
  // Діагностика логів/crash-report/mods збірки → проблеми зі способами лагодження.
  DiagnosePack: (packID: string): Promise<RepairReport> => call('DiagnosePack', packID),
  // Застосувати вибрані лагодження (параметри бекенд перевіряє наново).
  ApplyPackRepairs: (packID: string, fixes: RepairFix[]): Promise<RepairFixResult[]> =>
    call('ApplyPackRepairs', packID, fixes),
  GetRepairIssueKinds: (): Promise<RepairIssueKind[]> => call('GetRepairIssueKinds'),
  GetRepairMuted: (): Promise<string[]> => call('GetRepairMuted'),
  SetRepairMuted: (kinds: string[], muted: boolean): Promise<string[]> => call('SetRepairMuted', kinds, muted),
  // ── Дерево залежностей модів збірки ──
  GetModDependencyGraph: (packID: string): Promise<ModDependencyGraph> => call('GetModDependencyGraph', packID),
  GetModRemovalImpact: (packID: string, modID: string): Promise<ModRemovalImpact> =>
    call('GetModRemovalImpact', packID, modID),
  // ── Метрика ресурсів гри (CPU/RAM/диск) ──
  GetPackMetrics: (packID: string): Promise<PackMetrics> => call('GetPackMetrics', packID),
  // Виконує один автофікс-екшен (cleanup_storage / remove_duplicate_mods / install_java).
  RunHealthCheckFix: (packID: string, actionID: string): Promise<void> =>
    call('RunHealthCheckFix', packID, actionID),

  // Відкрити діалог вибору файлів контенту (.zip).
  PickContentFiles: (contentDir: string): Promise<string[]> =>
    call('PickContentFiles', contentDir),
  // Імпортувати файли (.zip) у теку контенту збірки.
  ImportContentFiles: (packID: string, contentDir: string, paths: string[]): Promise<ModImportResult> =>
    call('ImportContentFiles', packID, contentDir, paths),
  // Перевірити наявність файлу у всіх збірках (buildID → bool).
  CheckContentInBuilds: (fileName: string, contentDir: string): Promise<Record<string, boolean>> =>
    call('CheckContentInBuilds', fileName, contentDir),
  // Розумне копіювання: якщо є сумісна версія — завантажує її, інакше копіює файл.
  SmartCopyContentFile: (srcPackID: string, contentDir: string, fileName: string, targetPackID: string, targetMcVersion: string): Promise<void> =>
    call('SmartCopyContentFile', srcPackID, contentDir, fileName, targetPackID, targetMcVersion),
}

/**
 * Підписка на подію бекенду. Колбек отримує розпаковані дані (як у v2).
 * Повертає функцію відписки.
 */
export function on(event: string, cb: (data: any) => void): () => void {
  return RtEvents.On(event, (e: { name: string; data: any }) => cb(e.data))
}

/** Відправити подію на бекенд. */
export function emit(event: string, data?: unknown): void {
  RtEvents.Emit(event, data)
}

/** Аналог window.runtime.* (управління вікном / додатком). */
export const Application = {
  Quit: (): Promise<void> => RtApp.Quit() as Promise<void>,
  Hide: (): Promise<void> => RtApp.Hide() as Promise<void>,
  Show: (): Promise<void> => RtApp.Show() as Promise<void>,
}

export const Window = {
  Minimise: (): Promise<void> => RtWindow.Minimise() as Promise<void>,
  Maximise: (): Promise<void> => RtWindow.Maximise() as Promise<void>,
  UnMaximise: (): Promise<void> => RtWindow.UnMaximise() as Promise<void>,
  UnMinimise: (): Promise<void> => RtWindow.UnMinimise() as Promise<void>,
  Hide: (): Promise<void> => RtWindow.Hide() as Promise<void>,
  Show: (): Promise<void> => RtWindow.Show() as Promise<void>,
}