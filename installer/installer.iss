; Shaurm Launcher — InnoSetup installer script (Go/Wails архітектура)
;
; ПРИНЦИПОВА ЗМІНА порівняно зі старим installer.iss (JavaFX-ера):
; тоді лаунчер складався з launcher-core.exe (Go, supervisor) +
; shaurma-launcher-jvm.exe (JavaFX + вбудований JRE) + game-monitor.exe +
; download-engine.exe + mods-service.exe, які спілкувались через
; TCP-loopback IPC — звідси купа Firewall-правил і CloseApplicationsFilter
; нижче в старій версії.
;
; Тепер: ОДИН файл launcher.exe (Wails: Go backend + embedded HTML/JS
; frontend, усі підсистеми — горутини всередині того самого процесу).
; Немає TCP IPC між процесами лаунчера -> немає loopback firewall-правил
; для internal IPC. Виходить назовні лише launcher.exe (Modrinth API,
; CDN) і shrm-updater.exe (CDN) — по одному правилу на кожен.
;
; Точка входу для користувача — ЯРЛИК ЗАВЖДИ ВЕДЕ НА shrm-updater.exe,
; не на launcher.exe напряму. shrm-updater.exe:
;   1. Перевіряє launcher-version.json на CDN проти локального
;   2. Якщо є новий launcher.exe — качає й ставить його
;   3. Запускає launcher.exe і виходить
; Це і є "перезапускач", про який йшлось у ТЗ: якщо launcher.exe чи
; launcher-version.json відсутні в installDir взагалі (перший запуск
; одразу після InnoSetup, до першого реального старту) — shrm-updater.exe
; трактує installDir як порожній і качає повний launcher.exe з CDN.

#ifndef MyAppVersion
  #define MyAppVersion "0.0.0"
#endif
#ifndef MyVersionInfoVersion
  ; VersionInfoVersion вимагає ЧИСТО числову версію (ISCC: "Value of ...
  ; directive 'VersionInfoVersion' is invalid" на "1.0.0-test"). build.ps1
  ; передає сюди числову частину MyAppVersion (суфікс -test/-alpha тощо
  ; залишається в AppVersion, але не в VersionInfoVersion).
  #define MyVersionInfoVersion MyAppVersion
#endif
#ifndef AppImageDir
  ; У monorepo build.ps1 передає АБСОЛЮТНИЙ шлях через /DAppImageDir=...
  ; (staging-тека dist\app-<edition> в корені репозиторію). Цей дефолт — лише для
  ; ручного прогону ISCC.exe з теки installer\ без build.ps1.
  #define AppImageDir "..\dist\app-shaurma"
#endif

#define MyAppPublisher  "SHAURMA TEAM"
#define MyAppURL        "https://shaurma.app"
#define UpdaterExeName  "shrm-updater.exe"
#define LauncherExeName "launcher.exe"
#define OutputDir       "..\dist"

; ── Двоверсійність (shaurma/base) ─────────────────────────────────────
; Цей installer.iss компілюється ДВІЧІ — по разу на edition — через
; /DMyEdition=shaurma чи /DMyEdition=base (build.ps1 -Edition). Різні
; AppId GUID -> Windows бачить два ОКРЕМИХ застосунки в "Програми й
; компоненти", які можуть стояти поруч (різні installDir). Різниця між
; edition лише в:
;   * бінарнику launcher.exe (build tag shaurma vs !shaurma, launcher/),
;   * --channel, який shrm-updater.exe передає на CDN (worker вибирає
;     TOKEN_SHAURMA чи TOKEN_BASE і гілку launchers/shaurma|base).
; Сам shrm-updater.exe — ОДИН спільний файл на обидва edition (в ньому
; вшиті обидва токени через -ldflags), тому без змін копіюється в обидві
; staging теки.
#ifdef MyEdition
  #if MyEdition == "base"
    #define MyAppIdGUID       "5E7D1A2B-3C4D-4E5F-9F8E-2A1B3C4D5E6F"
    #define MyAppName         "Shaurm Launcher (Base)"
    #define MyInstallDir      "ShaurmLauncherBase"
    #define MyChannel         "base"
    #define MyOutputBaseFilename "ShaurmLauncherBase-setup"
    #define MyRulePrefix      "Shaurm Launcher Base"
  #else
    #define MyAppIdGUID       "9A8B2C3D-4E5F-6789-ABCD-EF0123456789"
    #define MyAppName         "Shaurm Launcher"
    #define MyInstallDir      "ShaurmLauncher"
    #define MyChannel         "shaurma"
    #define MyOutputBaseFilename "ShaurmLauncher-setup"
    #define MyRulePrefix      "Shaurm Launcher"
  #endif
#else
  ; Дефолт без /DMyEdition — повна shaurma-версія (як і до двоверсійності).
  #define MyAppIdGUID       "9A8B2C3D-4E5F-6789-ABCD-EF0123456789"
  #define MyAppName         "Shaurm Launcher"
  #define MyInstallDir      "ShaurmLauncher"
  #define MyChannel         "shaurma"
  #define MyOutputBaseFilename "ShaurmLauncher-setup"
  #define MyRulePrefix      "Shaurm Launcher"
#endif

; AppId в Inno Setup пишеться як {{GUID} (перша дужка екранується через
; подвійну, бо { починає Inno-константу). GUID продубльований у MyAppId
; (з дужками) і MyAppIdGUID (без, для реєстрового ключа в IsUpgrade):
; ISPP-підстановка AppId={{#MyAppIdGUID} не працює (препроцесор ламає
; екранування), а вкладені define ({{#MyAppIdGUID} всередині іншого)
; ISPP не розкриває — тому пишемо значення напряму.
#ifdef MyEdition
  #if MyEdition == "base"
    #define MyAppId "{{5E7D1A2B-3C4D-4E5F-9F8E-2A1B3C4D5E6F}"
  #else
    #define MyAppId "{{9A8B2C3D-4E5F-6789-ABCD-EF0123456789}"
  #endif
#else
  #define MyAppId "{{9A8B2C3D-4E5F-6789-ABCD-EF0123456789}"
#endif

[Setup]
AppId={#MyAppId}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppVerName={#MyAppName} {#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}
AppUpdatesURL={#MyAppURL}
; %LOCALAPPDATA% (не Program Files) — shrm-updater має вміти
; заміняти launcher.exe без прав адміністратора при кожному оновленні.
; Program Files вимагає admin для запису, що зламало б self-update.
DefaultDirName={localappdata}\{#MyInstallDir}
DefaultGroupName={#MyAppName}
AllowNoIcons=yes
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=dialog
OutputDir={#OutputDir}
OutputBaseFilename={#MyOutputBaseFilename}-{#MyAppVersion}
SetupIconFile={#AppImageDir}\build\windows\icon.ico
Compression=lzma2/ultra64
SolidCompression=yes
WizardStyle=modern
WizardResizable=no
UninstallDisplayIcon={app}\{#UpdaterExeName}
UninstallDisplayName={#MyAppName}
VersionInfoVersion={#MyVersionInfoVersion}
VersionInfoCompany={#MyAppPublisher}
VersionInfoCopyright=© SHAURMA TEAM
VersionInfoDescription={#MyAppName} Setup
; Немає CloseApplicationsFilter/RestartApplications — старий JavaFX
; installer мусив явно закривати *.exe/*.jar через InnoSetup при
; апгрейді (запущений процес блокував перезапис файлів на диску).
; Тепер це відповідальність shrm-updater.exe (install.replaceFile,
; internal/install/install.go) — сам updater чекає і ретраїть заміну
; launcher.exe, це коректніше за грубе "закрий все, що видно InnoSetup"
; під час звичайного InnoSetup-інсталу (перший install / ручний rerun).

[Languages]
Name: "english";   MessagesFile: "compiler:Default.isl"
Name: "ukrainian"; MessagesFile: "compiler:Languages\Ukrainian.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: checkedonce

[Files]
; ── shrm-updater.exe — ТОЧКА ВХОДУ, ярлик завжди веде сюди ─────────────
; Маленький, рідко оновлюваний бінарник. Не входить у власний
; self-update цикл launcher-version.json (лише launcher.exe оновлюється
; так) — якщо колись знадобиться оновити сам updater, це робиться через
; повний InnoSetup-переустановлення (рідкісний випадок: логіка
; update/install тут стабільна і змінюється значно рідше за UI/логіку
; самого лаунчера).
Source: "{#AppImageDir}\shrm-updater.exe"; DestDir: "{app}"; Flags: ignoreversion

; ── launcher.exe — основний застосунок (перша версія "з коробки") ─────
; Після встановлення shrm-updater.exe при кожному запуску звіряє цей
; файл проти CDN і замінює за потреби — тому тут кладемо просто
; поточну версію на момент збірки інсталятора, не "найсвіжішу
; можливу": свіжість підтримує updater, не installer.
Source: "{#AppImageDir}\launcher.exe"; DestDir: "{app}"; Flags: ignoreversion

; ── launcher-version.json — початковий локальний маніфест ─────────────
; Описує SHA256/розмір launcher.exe з коробки. Перезаписується
; shrm-updater.exe після кожного успішного оновлення (internal/install
; Apply()) — тут лише стартовий стан, щоб перший запуск updater міг
; порівняти "що встановлено" з CDN без зайвого перекачування щойно
; встановленої версії.
Source: "{#AppImageDir}\launcher-version.json"; DestDir: "{app}"; Flags: ignoreversion skipifsourcedoesntexist

[Icons]
; --channel і --install-dir: див. updater/main.go (flag.Parse). Base-версія
; живе в окремому installDir, тому --install-dir обов'язковий — без нього
; updater (дефолт %LOCALAPPDATA%\ShaurmLauncher) стрибав би в чужий шлях.
Name: "{group}\{#MyAppName}";                       Filename: "{app}\{#UpdaterExeName}"; Parameters: "--channel {#MyChannel} --install-dir ""{localappdata}\{#MyInstallDir}"""
Name: "{group}\{cm:UninstallProgram,{#MyAppName}}"; Filename: "{uninstallexe}"
Name: "{autodesktop}\{#MyAppName}";                 Filename: "{app}\{#UpdaterExeName}"; Parameters: "--channel {#MyChannel} --install-dir ""{localappdata}\{#MyInstallDir}"""; Tasks: desktopicon

[Run]
Filename: "{app}\{#UpdaterExeName}"; \
  Parameters: "--channel {#MyChannel} --install-dir ""{localappdata}\{#MyInstallDir}"""; \
  Description: "{cm:LaunchProgram,{#MyAppName}}"; \
  Flags: nowait postinstall skipifsilent

[UninstallDelete]
; Дані користувача (профілі, збірки, моди) лишаються за замовчуванням —
; видалення лаунчера не повинно нищити збережені збірки. Розкоментувати
; за потреби повного очищення:
; Type: filesandordirs; Name: "{userappdata}\shaurm"

[Code]
// ── Виявлення апгрейду (для welcome-екрану майстра) ────────────────────
function IsUpgrade(): Boolean;
begin
  Result := RegKeyExists(HKCU, 'Software\Microsoft\Windows\CurrentVersion\Uninstall\{#MyAppIdGUID}_is1');
end;

procedure InitializeWizard;
begin
  if IsUpgrade() then
  begin
    WizardForm.WelcomeLabel2.Caption :=
      'Це оновить ' + ExpandConstant('{#MyAppName}') +
      ' до версії ' + ExpandConstant('{#MyAppVersion}') + '.' + #13#10 +
      'Ваші дані та налаштування будуть збережені.';
  end;
end;

// ── Windows Firewall ────────────────────────────────────────────────────
// Лише ДВА процеси виходять у мережу (на відміну від шести у старій
// багатопроцесній архітектурі): launcher.exe (Modrinth/CurseForge API,
// завантаження збірок, перевірка оновлень з UI) і shrm-updater.exe
// (перевірка/завантаження оновлення при старті). Loopback-правила для
// internal IPC більше не потрібні — процеси не спілкуються по TCP між
// собою, все відбувається горутинами всередині одного процесу.
procedure RegisterFirewallRule;
var
  ResultCode: Integer;
  ExeNames: array[0..1] of String;
  I: Integer;
  AppPath: String;
begin
  ExeNames[0] := '{#LauncherExeName}';
  ExeNames[1] := '{#UpdaterExeName}';

  Exec('netsh.exe',
    'advfirewall firewall delete rule name="{#MyRulePrefix} Out"',
    '', SW_HIDE, ewWaitUntilTerminated, ResultCode);

  for I := 0 to 1 do
  begin
    AppPath := ExpandConstant('{app}\' + ExeNames[I]);
    Exec('netsh.exe',
      'advfirewall firewall delete rule name="{#MyRulePrefix} Out-' + ExeNames[I] + '"',
      '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
    Exec('netsh.exe',
      'advfirewall firewall add rule' +
      ' name="{#MyRulePrefix} Out-' + ExeNames[I] + '"' +
      ' dir=out' +
      ' action=allow' +
      ' program="' + AppPath + '"' +
      ' profile=private' +
      ' description="Outbound access for ' + ExeNames[I] + ' (CDN/API)"',
      '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  end;
end;

procedure RemoveFirewallRule;
var
  ResultCode: Integer;
  ExeNames: array[0..1] of String;
  I: Integer;
begin
  ExeNames[0] := '{#LauncherExeName}';
  ExeNames[1] := '{#UpdaterExeName}';
  for I := 0 to 1 do
    Exec('netsh.exe',
      'advfirewall firewall delete rule name="{#MyRulePrefix} Out-' + ExeNames[I] + '"',
      '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssPostInstall then
    RegisterFirewallRule();
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usPostUninstall then
    RemoveFirewallRule();
end;
