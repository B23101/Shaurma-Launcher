// ── Wails v3 frontend shim ──────────────────────────────────────────────
// Замість згенерованих wailsjs/ (v2) або @wailsio/runtime використовуємо
// вбудований JS-runtime, який Wails v3 віддає за адресою /wails/runtime.js.
//
// Call.ByName('main.App.MethodName', ...args) — виклик Go-методу бекенду
// (у v2 це було window.go.main.App.MethodName(...)). Повертає Promise.
//
// Events.On(name, cb) — підписка на подію. У v3 колбек отримує об'єкт
// WailsEvent {name, data}, тому тут ми розпаковуємо .data, щоб колбеки
// працювали так само, як у v2 (window.runtime.EventsOn).
//
// Application / Window — аналог window.runtime.* методів v2.

import { Call, Events as RtEvents, Application as RtApp, Window as RtWindow } from '/wails/runtime.js'
import type {
  Account, BrowserEntry, AIDiagnosis, AIFixResult, AIRecommendedAction, ConsoleContext, ConsoleLine,
  FolderPaths, InstanceConfig, JavaStatus, PackIndexEntry, ModEntry, BuildView,
  SettingDef, Settings, SetupProgress, StorageInfo, StoragePackEntry, UnusedJavaInfo, SystemInfo, UpdateInfo,
  SkinPreset, CapeInfo, PlayerProfile, ActiveLook,
  MCVersionEntry, LoaderVersionEntry, CustomPack, CustomPackFormParams, DetectedPack,
  PackDirListing, PackFileContent, ConsoleAccountInfo,
  ModVersionOption, ModDependency, ModBackupEntry, DuplicateModGroup, ModUpdateResult, PreLaunchModIssues, ServerPing,
  InstanceServer
} from './types'

function call(method: string, ...args: unknown[]): Promise<any> {
  return Call.ByName(`main.App.${method}`, ...args) as Promise<any>
}

/** Типізований доступ до Go-бекенду (main.App). */
export const App = {
  AwaitMSLogin: (): Promise<void> => call('AwaitMSLogin'),
  BrowseFolder: (): Promise<string> => call('BrowseFolder'),
  BrowseFontFile: (): Promise<string> => call('BrowseFontFile'),
  CancelMSLogin: (): Promise<void> => call('CancelMSLogin'),
  CheckUpdate: (): Promise<UpdateInfo> => call('CheckUpdate'),
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
  // Повний ануінстал збірки: файли + реєстр + конфіг + стан синхронізації.
  DeletePack: (id: string): Promise<void> => call('DeletePack', id),
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
  CheckModUpdates: (packID: string, mcVersion: string, loader: string): Promise<ModEntry[]> => call('CheckModUpdates', packID, mcVersion, loader),
  // RefreshModUpdates — примусова МЕРЕЖЕВА перевірка (кнопка «Перевірити
  // оновлення»); результат кешується і потім читається ScanModsWithCachedUpdates.
  RefreshModUpdates: (packID: string, mcVersion: string, loader: string): Promise<ModEntry[]> =>
    call('RefreshModUpdates', packID, mcVersion, loader),
  GetModVersions: (packID: string, fileName: string, mcVersion: string, loader: string): Promise<ModVersionOption[]> =>
    call('GetModVersions', packID, fileName, mcVersion, loader),
  InstallModVersion: (packID: string, fileName: string, opt: ModVersionOption): Promise<void> => call('InstallModVersion', packID, fileName, opt),
  ResolveModDependencies: (packID: string, mcVersion: string, loader: string, fileName?: string): Promise<ModDependency[]> =>
    call('ResolveModDependencies', packID, mcVersion, loader, fileName ?? ''),
  InstallModDependencies: (packID: string, deps: ModDependency[]): Promise<void> => call('InstallModDependencies', packID, deps),
  UpdateAllMods: (packID: string, mcVersion: string, loader: string, backup: boolean): Promise<ModUpdateResult> => call('UpdateAllMods', packID, mcVersion, loader, backup),
  ListModBackups: (packID: string): Promise<ModBackupEntry[]> => call('ListModBackups', packID),
  RestoreModBackup: (packID: string, entry: ModBackupEntry): Promise<void> => call('RestoreModBackup', packID, entry),
  DeleteMod: (packID: string, fileName: string): Promise<void> => call('DeleteMod', packID, fileName),
  FindDuplicateMods: (packID: string): Promise<DuplicateModGroup[]> => call('FindDuplicateMods', packID),
  CheckPreLaunchModIssues: (packID: string): Promise<PreLaunchModIssues> => call('CheckPreLaunchModIssues', packID),
  // Відкрити теку mods/ збірки в Провіднику (кнопка в тулбарі менеджера модів).
  OpenModsFolder: (packID: string): Promise<void> => call('OpenModsFolder', packID),
  // Показати конкретний jar мода виділеним у Провіднику (кнопка на картці мода).
  RevealModFile: (packID: string, fileName: string): Promise<void> => call('RevealModFile', packID, fileName),
  // ── Автоприєднання: сервери з server.dat + пінг ──
  ListPackServers: (packID: string): Promise<InstanceServer[]> => call('ListPackServers', packID),
  PingServer: (address: string): Promise<ServerPing> => call('PingServer', address),
  // ── Консоль: згортання окремого вікна у «шапку» ──
  SetConsoleCollapsed: (buildID: string, collapsed: boolean): Promise<void> => call('SetConsoleCollapsed', buildID, collapsed),
  GetBuilds: (): Promise<BuildView[]> => call('GetBuilds'),
  GetFolderPaths: (): Promise<FolderPaths> => call('GetFolderPaths'),
  GetInstanceConfig: (id: string): Promise<InstanceConfig> => call('GetInstanceConfig', id),
  GetJavaStatus: (): Promise<JavaStatus> => call('GetJavaStatus'),
  GetPackIndex: (): Promise<PackIndexEntry[]> => call('GetPackIndex'),
  // data URL асета збірки (іконка/фон) — качається через бекенд, бо браузер
  // не може додати токен-заголовки до <img> (див. lib/packAssets.svelte.ts).
  GetPackAsset: (url: string): Promise<string> => call('GetPackAsset', url),
  GetPlaytime: (): Promise<Record<string, number>> => call('GetPlaytime'),
  GetSettings: (): Promise<Settings> => call('GetSettings'),
  GetSettingsSchema: (): Promise<SettingDef[]> => call('GetSettingsSchema'),
  GetSetupProgress: (): Promise<SetupProgress> => call('GetSetupProgress'),
  GetStorageUsage: (): Promise<StorageInfo> => call('GetStorageUsage'),
  // Список встановлених збірок з розмірами (швидке видалення у «Пам'ять і кеш»).
  GetStoragePacks: (): Promise<StoragePackEntry[]> => call('GetStoragePacks'),
  // Очистити логи лаунчера + логів/краш-репортів усіх збірок.
  ClearPackLogs: (): Promise<void> => call('ClearPackLogs'),
  // Аналіз/видалення невикористовуваної Java (мажори, яких немає в жодної
  // встановленої збірки) — вкладка «Пам'ять і кеш».
  AnalyzeUnusedJava: (): Promise<UnusedJavaInfo> => call('AnalyzeUnusedJava'),
  CleanupUnusedJava: (majors: number[]): Promise<void> => call('CleanupUnusedJava', majors),
  GetSystemInfo: (): Promise<SystemInfo> => call('GetSystemInfo'),
  GetVersion: (): Promise<string> => call('GetVersion'),
  GetWizardDefaults: (): Promise<SetupProgress> => call('GetWizardDefaults'),
  HideToTray: (): Promise<void> => call('HideToTray'),
  IsFirstRun: (): Promise<boolean> => call('IsFirstRun'),
  IsShaurmaEdition: (): Promise<boolean> => call('IsShaurmaEdition'),
  LaunchInstance: (id: string): Promise<void> => call('LaunchInstance', id),
  LoadFontFile: (path: string): Promise<string> => call('LoadFontFile', path),
  LoginPirate: (username: string): Promise<void> => call('LoginPirate', username),
  OpenExternal: (url: string): Promise<void> => call('OpenExternal', url),
  // Відкрити теку встановленої збірки у Провіднику.
  OpenPackFolder: (id: string): Promise<void> => call('OpenPackFolder', id),
  // Список тек одиночних світів (saves/) збірки — для вибору світу в
  // автоприєднанні на сторінці збірки, вкладка «Гра».
  ListInstanceWorlds: (id: string): Promise<string[]> => call('ListInstanceWorlds', id),
  RemoveAccount: (id: string): Promise<void> => call('RemoveAccount', id),
  RestoreWindowAfterGame: (): Promise<void> => call('RestoreWindowAfterGame'),
  SaveFolderPaths: (instanceDir: string, javaPath: string): Promise<void> => call('SaveFolderPaths', instanceDir, javaPath),
  SaveInstanceConfig: (cfg: InstanceConfig): Promise<void> => call('SaveInstanceConfig', cfg),
  SaveSettings: (s: Settings): Promise<void> => call('SaveSettings', s),
  SaveSetupProgress: (p: SetupProgress): Promise<void> => call('SaveSetupProgress', p),
  SearchContent: (query: string): Promise<BrowserEntry[]> => call('SearchContent', query),
  SetActiveAccount: (id: string): Promise<void> => call('SetActiveAccount', id),
  SetAppDir: (p: string): Promise<void> => call('SetAppDir', p),
  SetFolderPath: (kind: string, p: string): Promise<void> => call('SetFolderPath', kind, p),
  StartMSLogin: (): Promise<string> => call('StartMSLogin'),
  StopGame: (): Promise<void> => call('StopGame'),
  // Повне завершення процесу (кнопка X топбара): виставляє прапорець,
  // щоб відкрите вікно консолі не заблокувало вихід з програми.
  QuitLauncher: (): Promise<void> => call('QuitLauncher'),

  // ── Гардероб ──
  // Пресети зберігаються окремо для кожного ліцензійного акаунта, тому
  // майже всі методи приймають accountID активного акаунта (з пропа
  // <Wardrobe activeAccount>).
  GetWardrobePresets: (accountID: string): Promise<SkinPreset[]> => call('GetWardrobePresets', accountID),
  DeleteWardrobePreset: (accountID: string, presetId: string): Promise<void> => call('DeleteWardrobePreset', accountID, presetId),
  // Оновлення існуючого пресету (назва, тип рук, плащ) — кнопка «Зберегти»
  // в редакторі пресету. Плащ за URL качається в локальний кеш на бекенді,
  // щоб картка пресету одразу показала новий вигляд.
  UpdateWardrobePreset: (accountID: string, presetId: string, name: string, slim: boolean, capeUrl: string): Promise<SkinPreset> =>
    call('UpdateWardrobePreset', accountID, presetId, name, slim, capeUrl),
  SaveWardrobeLocalSkin: (accountID: string, name: string, skinPngBase64: string, slim: boolean): Promise<SkinPreset> =>
    call('SaveWardrobeLocalSkin', accountID, name, skinPngBase64, slim),
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
  // Створити пресет з поточного вигляду акаунта (кнопка "Редагувати скін",
  // коли поточний скін/плащ не мають збереженого пресету).
  SaveWardrobeActiveLookPreset: (accountID: string, name: string): Promise<SkinPreset> =>
    call('SaveWardrobeActiveLookPreset', accountID, name),

  // ── Кастомні збірки (створення вручну / імпорт .mrpack, .zip) ──
  GetCustomPackMCVersions: (): Promise<MCVersionEntry[]> => call('GetCustomPackMCVersions'),
  GetCustomPackLoaderVersions: (loader: string, mcVersion: string): Promise<LoaderVersionEntry[]> =>
    call('GetCustomPackLoaderVersions', loader, mcVersion),
  GetCustomPackDefaultMCVersion: (): Promise<string> => call('GetCustomPackDefaultMCVersion'),
  HasCurseForgeAPIKey: (): Promise<boolean> => call('HasCurseForgeAPIKey'),
  BrowseImageFile: (): Promise<string> => call('BrowseImageFile'),
  BrowseModpackArchive: (): Promise<string> => call('BrowseModpackArchive'),
  CreateCustomPack: (p: CustomPackFormParams): Promise<CustomPack> => call('CreateCustomPack', p),
  UpdateCustomPack: (id: string, p: CustomPackFormParams): Promise<CustomPack> => call('UpdateCustomPack', id, p),
  GetCustomPack: (id: string): Promise<CustomPack> => call('GetCustomPack', id),
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

  // ── Файловий провідник збірки (модовані/кастомні збірки) ──
  ListPackFiles: (packID: string, relPath: string): Promise<PackDirListing> => call('ListPackFiles', packID, relPath),
  ReadPackTextFile: (packID: string, relPath: string): Promise<PackFileContent> => call('ReadPackTextFile', packID, relPath),
  WritePackTextFile: (packID: string, relPath: string, content: string): Promise<void> => call('WritePackTextFile', packID, relPath, content),
  RenamePackEntry: (packID: string, relPath: string, newName: string): Promise<void> => call('RenamePackEntry', packID, relPath, newName),
  CreatePackFolder: (packID: string, relPath: string, folderName: string): Promise<void> => call('CreatePackFolder', packID, relPath, folderName),
  DeletePackEntry: (packID: string, relPath: string): Promise<void> => call('DeletePackEntry', packID, relPath),
  RevealPackFile: (packID: string, relPath: string): Promise<void> => call('RevealPackFile', packID, relPath),
  GetPackFileAsset: (packID: string, relPath: string): Promise<string> => call('GetPackFileAsset', packID, relPath),

  // ── Консоль: перемикач акаунтів ──
  GetConsoleAccounts: (buildID: string): Promise<ConsoleAccountInfo[]> => call('GetConsoleAccounts', buildID),
  // (accountID, buildID) — порядок аргументів як у Go-сигнатурі бекенду.
  SwitchConsoleAccount: (accountID: string, buildID: string): Promise<void> => call('SwitchConsoleAccount', accountID, buildID),
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
