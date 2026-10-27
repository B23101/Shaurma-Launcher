; Shaurm Launcher — InnoSetup installer script (Go/Wails архітектура)
;
; ПРИНЦИПОВА ЗМІНА порівняно зі старим installer.iss (JavaFX-ера):
; тоді лаунчер складався з launcher-core.exe (Go, supervisor) +
; shaurma-launcher-jvm.exe (JavaFX + вбудований JRE) + game-monitor.exe +
; download-engine.exe + mods-service.exe, які спілкувались через
; TCP-loopback IPC — звідси купа Firewall-правил і CloseApplicationsFilter
; нижче в старій версії.
;
; Тепер (НОВА АРХІТЕКТУРА): shell.exe (тонкий Wails-виконавець, жодної
; бізнес-логіки; змінюється ВКРАЙ рідко і оновлюється self-update через
; апдейтер, див. крок 6 нижче) + backend.dll (весь бізнес-код
; лаунчера, C ABI bridge.h) + updater.dll (перевірка/встановлення
; оновлень) + frontend.zip / updater-frontend.zip (UI, kind=archive
; розпаковуються у frontend/ і updater-frontend/). Усі підсистеми —
; горутини всередині того самого процесу. Немає TCP IPC між процесами
; лаунчера -> немає loopback firewall-правил для internal IPC. Виходить
; назовні лише ОДИН процес — shell.exe (Modrinth API, CDN) — одне правило.
;
; СТАБІЛЬНИЙ setup.exe: інсталятор вбудовує ЛИШЕ ОДНОРАЗОВИЙ bootstrap
; (Shell A — dist/app-<edition>/bootstrap/ShaurmLauncher.exe, чисто-Go
; bootstrap/main.go). При першому запуску bootstrap качає з CDN РЕАЛЬНИЙ
; shell (Shell B — той самий файл ShaurmLauncher.exe, але повний
; Wails-shell) за токеном каналу (вшитий у bootstrap через -X main.cdnToken),
; перейменовує СЕБЕ в <name>~ і ставить новий на місце, перезапускає
; лаунчер. Далі Shell B при першому запуску САМ качає решту компонентів
; (updater.dll, backend.dll, frontend.zip...). Постійний хеш setup =
; репутація SmartScreen не скидається між релізами (той самий принцип,
; що й з фіксованою версією).
;
; Перший запуск exe (bootstrap = Shell A):
;   0. качає з CDN реальний shell (Shell B, той самий файл) у <name>.new,
;      перейменовує СЕБЕ (активний exe) у <name>~, ставить .new на місце і
;      перезапускає лаунчер з НОВОГО файлу. Далі — як раніше:
;   1. застосовує залишені .new-компоненти (updater.dll.new і т.д.);
;   2. якщо updater.dll немає зовсім (щойно встановлений setup) — качає
;      його з CDN сам (bootstrap, вікно прогресу в базовому стилі
;      лаунчера, shell/bootstrap.go);
;   3. завантажує updater.dll — той звіряє launcher-version.json з CDN
;      проти локального і качає/розпаковує відсутні чи змінені
;      компоненти (backend.dll, frontend.zip...); ОНОВЛЮЄ ЛИШЕ
;      ОНОВЛЮВАЧ — лаунчер жодних оновлень не робить;
;   4. після завершення (phase_complete) shell перемикає міст на
;      backend.dll і показує головне вікно лаунчера (DLL при цьому НЕ
;      вивантажуються — FreeLibrary на c-shared Go-бібліотеці це
;      crash-ризик, причина «зникання» лаунчера після перевірки оновлень).
;   5. оновлення самого updater.dll: апдейтер качає нову версію як
;      updater.dll.new (завантажену DLL Windows не дає перезаписати), а
;      лаунчер при наступному старті видаляє старий файл і перейменовує
;      .new на його місце (applyPendingUpdates).
;   6. SELF-UPDATE shell (рідко — тільки коли shell реально змінився):
;      апдейтер качає нову версію exe як <name>.new (Windows не дає
;      перезаписати запущений exe), і shell застосовує її ЗА РАХУНОК
;      rename активного exe вбік як <name>~ (перейменувати запущений
;      exe Windows ДОЗВОЛЯЄ) → .new на оригінальне місце → перезапуск
;      самого себе. Старий <name>~ видаляється при наступному старті
;      (див. shell/updater_phase.go: applyShellSelfUpdate).
;      Це оновлює Shell B (реальний); Shell A (bootstrap у setup) НЕ
;      оновлюється НІКОЛИ.

#ifndef MyAppVersion
  ; СТАЛИЙ setup.exe (ТЗ): версія в інсталяторі — ФІКСОВАНА "1.0.0", а не
  ; актуальний реліз лаунчера. setup.exe містить лише shell.exe (теж
  ; версії "1.0.0" — build.ps1), сам реальний номер версії лаунчера читає
  ; runtime з launcher-version.json на CDN/диску. Якби AppVersion/
  ; VersionInfoVersion/назва файлу залежали від номера релізу — ISCC
  ; перекомпільовував би setup.exe (інша назва, інший version-ресурс,
  ; інший хеш) під КОЖЕН реліз, хоча вміст (shell.exe) не змінюється
  ; взагалі. build.ps1 більше НЕ передає сюди справжній номер релізу.
  #define MyAppVersion "1.0.0"
#endif
#ifndef MyVersionInfoVersion
  ; VersionInfoVersion вимагає ЧИСТО числову версію (ISCC: "Value of ...
  ; directive 'VersionInfoVersion' is invalid" на "1.0.0-test"). Так само
  ; фіксована — не залежить від MyAppVersion реального релізу.
  #define MyVersionInfoVersion MyAppVersion
#endif
#ifndef AppImageDir
  ; У monorepo build.ps1 передає АБСОЛЮТНИЙ шлях через /DAppImageDir=...
  ; (staging-тека dist\app-<edition> в корені репозиторію). Цей дефолт — лише для
  ; ручного прогону ISCC.exe з теки installer\ без build.ps1.
  #define AppImageDir "..\dist\app-shaurma"
#endif

; Відбитки (SHA-1, hex, через кому) ОФІЦІЙНИХ сертифікатів підпису коду. Якщо
; задано (ISCC /DOfficialSignerThumbprints=AAA,BBB або змінна середовища
; SHAURMA_SIGNER_THUMBPRINTS у pack-manager) — інсталятор попереджає і тоді,
; коли підпис дійсний, але належить ІНШОМУ видавцю. Порожньо = перевіряється
; лише наявність дійсного підпису.
#ifndef OfficialSignerThumbprints
  #define OfficialSignerThumbprints ""
#endif

#define MyAppPublisher  "SHAURMA TEAM"
#define MyAppURL        "https://shaurma.app"
#define OutputDir       "..\dist"

; ── Двоверсійність (shaurma/base) ─────────────────────────────────────
; Цей installer.iss компілюється ДВІЧІ — по разу на edition — через
; /DMyEdition=shaurma чи /DMyEdition=base (build.ps1 -Edition).
;
; ОДИН СПІЛЬНИЙ AppId і ОДНА СПІЛЬНА installDir на обидві редакції —
; НАВМИСНО: редакція (shaurma/base) — внутрішній канал збірки/оновлень,
; не окремий «продукт». Користувач бачить ОДИН запис у «Програми і
; компоненти» й ОДНУ теку встановлення; встановлення іншої редакції
; поверх наявної Windows штатно розпізнає як АПГРЕЙД того самого
; застосунку (той самий AppId → IsUpgrade()=True, без діалогу «виберіть
; іншу теку»), а CurStepChanged/RegisterFirewallRule нижче прибирає
; артефакти (.exe, firewall-правило) редакції, яку витісняють — див.
; CleanupOtherEditionArtifacts. Раніше тут були РІЗНІ AppId і РІЗНІ
; installDir (ShaurmLauncher/ShaurmLauncherBase) — обидві редакції
; ставились ПОРУЧ як два незалежних застосунки, і саме це й був баг.
;
; Різниця між edition лишається лише в:
;   * імені shell.exe (ShaurmLauncher.exe vs ShaurmLauncherBase.exe —
;     build.ps1 збирає shell двічі з -X shaurma-shell.editionChannel=...),
;   * backend.dll (build tag shaurma vs !shaurma, new build/backend/),
;   * каналі: shell визначає channel з імені exe і передає його в
;     updater.dll, той використовує вшитий build-time токен (worker
;     вибирає TOKEN_SHAURMA чи TOKEN_BASE і гілку launchers/shaurma|base).
; updater.dll — ОДИН спільний файл на обидва edition (в ньому вшиті
; обидва токени через -ldflags), тому без змін копіюється в обидві
; staging теки.
#ifdef MyEdition
  #if MyEdition == "base"
    #define MyAppName         "Shaurm Launcher"
    #define MyInstallDir      "ShaurmLauncher"
    #define MyChannel         "base"
    #define MyShellExeName    "ShaurmLauncherBase.exe"
    #define MyOtherShellExeName "ShaurmLauncher.exe"
    #define MyOutputBaseFilename "ShaurmLauncherBase-setup"
    #define MyRulePrefix      "Shaurm Launcher"
  #else
    #define MyAppName         "Shaurm Launcher"
    #define MyInstallDir      "ShaurmLauncher"
    #define MyChannel         "shaurma"
    #define MyShellExeName    "ShaurmLauncher.exe"
    #define MyOtherShellExeName "ShaurmLauncherBase.exe"
    #define MyOutputBaseFilename "ShaurmLauncher-setup"
    #define MyRulePrefix      "Shaurm Launcher"
  #endif
#else
  ; Дефолт без /DMyEdition — повна shaurma-версія (як і до двоверсійності).
  #define MyAppName         "Shaurm Launcher"
  #define MyInstallDir      "ShaurmLauncher"
  #define MyChannel         "shaurma"
  #define MyShellExeName    "ShaurmLauncher.exe"
  #define MyOtherShellExeName "ShaurmLauncherBase.exe"
  #define MyOutputBaseFilename "ShaurmLauncher-setup"
  #define MyRulePrefix      "Shaurm Launcher"
#endif

; AppId в Inno Setup пишеться як {{GUID} (перша дужка екранується через
; подвійну, бо { починає Inno-константу). GUID продубльований у MyAppId
; (з дужками) і MyAppIdGUID (без, для реєстрового ключа в IsUpgrade).
;
; ОДИН GUID на ОБИДВІ редакції (раніше — різний для shaurma/base): саме
; так Windows/Inno розпізнає встановлення іншої редакції як апгрейд
; того самого застосунку, а не як окремий продукт поруч.
#define MyAppIdGUID "9A8B2C3D-4E5F-6789-ABCD-EF0123456789"
#define MyAppId     "{{9A8B2C3D-4E5F-6789-ABCD-EF0123456789}"

; setup.exe постійного розміру містить ЛИШЕ ОДНОРАЗОВИЙ bootstrap (Shell A):
; при першому запуску він качає реальний shell (Shell B) з CDN і
; підміняє себе (rename вбік «~» + .new на місце, див. bootstrap/main.go),
; а решту компонентів довантажує сам exe (bootstrap у shell + апдейтер).
; Наступні оновлення — через апдейтер, включно з самим Shell B
; (self-update через .new, див. коментар до кроку 6 вище).

[Setup]
AppId={#MyAppId}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppVerName={#MyAppName} {#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}
AppUpdatesURL={#MyAppURL}
; Дефолт — папка Local користувача: %LOCALAPPDATA%\ShaurmLauncher (не
; Program Files) — апдейтер (updater.dll) має вміти заміняти компоненти
; (backend.dll, frontend.zip...) без прав адміністратора при кожному
; оновленні. Program Files вимагає admin для запису, що зламало б
; self-update.
;
; АЛЕ користувач САМ вибирає теку встановлення: DisableDirPage=no —
; сторінка «Виберіть теку встановлення» майстра завжди показана, і
; DefaultDirName — лише початкове значення. AlwaysShowDirOnReadyPage —
; обрана тека видно на сторінці «Готово до встановлення», щоб юзер точно
; бачив, куди ставиться лаунчер. Програма (shell/backend) завжди розуміє
; свою реальну теку: shell визначає exeDir() з os.Executable і передає його
; в backend_init як installDir, там живуть launcher-location.json і база.
DefaultDirName={localappdata}\{#MyInstallDir}
DisableDirPage=no
AlwaysShowDirOnReadyPage=yes
DefaultGroupName={#MyAppName}
AllowNoIcons=yes
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=dialog
OutputDir={#OutputDir}
OutputBaseFilename={#MyOutputBaseFilename}
SetupIconFile={#AppImageDir}\build\windows\icon.ico
; lzma2/normal, а НЕ lzma2/ultra64: максимальне стиснення тригерить у
; антивірусних ML-евристик класичне «стиснутий/упакований виконуваний
; файл» (типовий фейл-позитив на абсолютно чистому інсталяторі — див.
; ANTIVIRUS_FALSE_POSITIVES.md). Setup усього ~2-3 МБ, виграш у розмірі
; від ultra64 мізерний, а підозра в антивірусів реальна.
Compression=lzma2/normal
SolidCompression=yes
WizardStyle=modern
WizardResizable=no
UninstallDisplayIcon={app}\{#MyShellExeName}
UninstallDisplayName={#MyAppName}
; Перевстановлення поверх ЗАПУЩЕНОГО лаунчера: [Files] пише exe, який може
; бути заблокований процесом (Windows тримає лок на запущений exe).
; CloseApplications=yes (Inno 6.1+) — Inno сам закриває процеси, що мають
; відкриті встановлювані файли, і продовжує копіювання (без нього
; перевстановлення падало б з «file in use»). RestartApplications=no —
; перезапускати сам не треба, [Run] і так запустить після встановлення.
CloseApplications=yes
RestartApplications=no
VersionInfoVersion={#MyVersionInfoVersion}
VersionInfoCompany={#MyAppPublisher}
VersionInfoCopyright=© SHAURMA TEAM
VersionInfoDescription={#MyAppName} Setup
; SignPath Foundation enforces ONE product name/version on every signed file (see .signpath/).
VersionInfoProductName=Shaurma Launcher
VersionInfoProductVersion={#MyVersionInfoVersion}
; Privacy policy + list of system changes, shown before installation (SignPath Foundation conditions).
InfoBeforeFile=SYSTEM-CHANGES.txt
; Примітка про старі JavaFX-версії: там CloseApplicationsFilter був
; обов'язковим (купа *.exe/*.jar, що блокували перезапис). Тепер у
; звичайних оновленнях (апдейтер install.replaceFile ретраїть і кладе
; запущені файли поруч як .new) CloseApplications не потрібен — він тут
; лише для RERUN-сценарію (перевстановлення setup.exe поверх запущеного
; лаунчера), де [Files] пишуть exe напряму.

[Languages]
Name: "english";   MessagesFile: "compiler:Default.isl"
Name: "ukrainian"; MessagesFile: "compiler:Languages\Ukrainian.isl"

[CustomMessages]
; Попередження про неофіційний (непідписаний) інсталятор. ЛИШЕ інформує —
; встановлення НЕ блокується.
english.UnsignedWarnText=WARNING: this installer is NOT signed by SHAURMA TEAM.%n%nThis usually means the launcher was built by someone else (for example, from the open-source code), not by the official team. An unofficial build may harm your computer.%n%nIf you expected the official installer, close this window and download it from the official site: https://shaurma-launcher.org/%n%nYou can still continue the installation if you trust the source.
english.ForeignSignerWarnText=WARNING: this installer is signed by a publisher other than SHAURMA TEAM.%n%nThis means the launcher was not built by the official team and may harm your computer.%n%nIf you expected the official installer, close this window and download it from https://shaurma-launcher.org/%n%nYou can still continue the installation if you trust the source.
ukrainian.UnsignedWarnText=УВАГА: цей інсталятор НЕ підписаний SHAURMA TEAM.%n%nЗазвичай це означає, що лаунчер зібрала не офіційна команда (наприклад, зі свого відкритого коду). Неофіційна збірка може зашкодити вашому комп'ютеру.%n%nЯкщо ви очікували офіційний інсталятор — закрийте це вікно й завантажте його з офіційного сайту: https://shaurma-launcher.org/%n%nЯкщо ви довіряєте джерелу, можна продовжити встановлення — воно не заблоковане.
ukrainian.ForeignSignerWarnText=УВАГА: цей інсталятор підписаний іншим видавцем, а не SHAURMA TEAM.%n%nЦе означає, що лаунчер зібрала не офіційна команда, і він може зашкодити вашому комп'ютеру.%n%nЯкщо ви очікували офіційний інсталятор — закрийте це вікно й завантажте його з https://shaurma-launcher.org/%n%nЯкщо ви довіряєте джерелу, можна продовжити встановлення — воно не заблоковане.

; ── Сторінка про Windows App Control (Smart App Control) ───────────────
; Показується одразу після привітання. Пояснює, що блокування нового exe
; через App Control НЕ пов'язане з вірусами, як його вимкнути і що робити,
; а кнопка відкриває офіційну документацію Microsoft.
english.AppControlPageTitle=Windows may block the launcher (App Control)
english.AppControlPageDesc=Please read this before installing. It is not a virus.
english.AppControlDocButton=Read the official Microsoft documentation
english.AppControlDocUrl=https://support.microsoft.com/en-us/topic/what-is-smart-app-control-285ea03d-fa88-4d56-882e-6698afdb7003
english.AppControlText=WHAT THIS IS%nWindows 11 has a protection feature called App Control (its official name is Smart App Control). It lets only programs that Microsoft already knows run: widely used programs, or programs with a signature Microsoft trusts. A new or not-yet-popular program such as {#MyAppName} can be blocked simply because Microsoft does not yet have enough reputation data about it.%n%nIT IS NOT RELATED TO VIRUSES%nApp Control is not an antivirus. A block by App Control does not mean a virus or data theft was found in the launcher. It only means the program is not yet popular enough for Microsoft. The block disappears as the program becomes more widely used.%n%nHOW TO TELL IT HAPPENED%nAfter installation the launcher does not start, disappears, or Windows shows a message or notification that mentions Smart App Control / App Control (for example "Smart App Control blocked part of this app"), or the launcher file vanishes from the installation folder.%n%nWHAT TO DO%n1. Click Start and type "Smart App Control". Open "Smart App Control settings". (The same place: Windows Security > App & browser control > Smart App Control settings.)%n2. Select "Off" and confirm.%n3. Start the launcher again. If its file disappeared, run this installer once more.%n%nMicrosoft states that it is not possible to allow a single program in Smart App Control, so turning it off is the way to run such a program. Per Microsoft's documentation you can turn it back on later in the same place. Turn it off only if you trust where you got this installer (the official site: https://shaurma-launcher.org/).%n%nTo make sure this text is honest, press the button below: it opens the official Microsoft documentation about App Control.
ukrainian.AppControlPageTitle=Windows може заблокувати лаунчер (App Control)
ukrainian.AppControlPageDesc=Прочитайте це перед встановленням. Це не вірус.
ukrainian.AppControlDocButton=Прочитати офіційну документацію Microsoft
ukrainian.AppControlDocUrl=https://support.microsoft.com/uk-ua/topic/what-is-smart-app-control-285ea03d-fa88-4d56-882e-6698afdb7003
ukrainian.AppControlText=ЩО ЦЕ ТАКЕ%nУ Windows 11 є функція захисту App Control (офіційна назва — Smart App Control). Вона дозволяє запуск лише тих програм, які Microsoft уже «знає»: широко поширених або з підписом, якому Microsoft довіряє. Нову чи ще не популярну програму, як-от {#MyAppName}, можуть заблокувати просто тому, що в Microsoft поки немає достатньо даних про її репутацію.%n%nЦЕ НЕ ПОВ'ЯЗАНО З ВІРУСАМИ%nApp Control — не антивірус. Блокування через App Control не означає, що в лаунчері знайдено вірус чи крадіжку даних. Воно означає лише те, що програма ще недостатньо відома Microsoft. Коли програмою користуватиметься більше людей, блокування зникне.%n%nЯК ПОНЯТИ, ЩО ЦЕ СТАЛОСЯ%nПісля встановлення лаунчер не запускається, зникає, або Windows показує повідомлення чи сповіщення, де згадується Smart App Control / App Control (наприклад, «Smart App Control заблокував частину цієї програми»), або файл лаунчера зникає з теки встановлення.%n%nЩО РОБИТИ%n1. Натисніть «Пуск» і введіть «Smart App Control». Відкрийте «Параметри Smart App Control» (Smart App Control settings). Те саме місце: «Безпека Windows» > «Керування програмами та браузером» (App & browser control) > «Параметри Smart App Control».%n2. Оберіть «Вимк.» (Off) і підтвердіть.%n3. Запустіть лаунчер знову. Якщо його файл зник — запустіть цей інсталятор ще раз.%n%nMicrosoft зазначає, що дозволити окрему програму в Smart App Control неможливо, тому щоб запустити таку програму, його потрібно вимкнути. За документацією Microsoft, пізніше його можна ввімкнути назад у тому самому місці. Вимикайте його, лише якщо довіряєте джерелу, з якого завантажено цей інсталятор (офіційний сайт: https://shaurma-launcher.org/).%n%nЩоб переконатися, що цей текст не вигадка, натисніть кнопку нижче: вона відкриє офіційну документацію Microsoft про App Control.

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"; Flags: checkedonce

[Files]
; ── Інсталятор містить ЛИШЕ ОДНОРАЗОВИЙ bootstrap (Shell A) ────────────
; bootstrap (dist/app-<edition>/bootstrap/ShaurmLauncher.exe, збирається
; build.ps1 з bootstrap/ — чисто-Go, без Wails) вбудовується в setup.exe.
; При ПЕРШОМУ запуску він качає з CDN реальний shell (Shell B — той самий
; файл, але повний Wails-shell), перейменовує СЕБЕ в <name>~ і ставить
; новий на місце, перезапускає лаунчер. Так setup.exe НАЗАВЖДИ
; стабільний (постійний хеш = SmartScreen-репутація не скидається), але
; на відміну від CDN-качалки в [Code] — встановлення НЕ залежить від
; мережі: файл уже на диску. Усі інші компоненти (updater.dll,
; backend.dll, frontend.zip) качає САМ (реальний) shell при першому
; запуску.
Source: "{#AppImageDir}\bootstrap\{#MyShellExeName}"; DestDir: "{app}"; DestName: "{#MyShellExeName}"; Flags: ignoreversion

[Dirs]
Name: "{app}"

[Icons]
; Ярлик веде ПРЯМО на shell — жодних параметрів: installDir і channel
; shell визначає сам (власна тека exe + ім'я/зашитий build-тег; див.
; shell/updater_phase.go: exeDir/currentChannel). Обидві редакції
; ставляться в ОДНУ installDir (див. коментар про AppId вище) — при
; перемиканні редакції CurStepChanged/CleanupOtherEditionArtifacts
; прибирає .exe і ярлики попередньої редакції ще до створення нових.
; Ярлик "Видалити {#MyAppName}" у групі Пуск свідомо ПРИБРАНО — щоб
; звичайний користувач не тицяв на нього випадково. Сам unins000.exe і
; запис у "Програми і компоненти" НІКУДИ не діваються (Uninstallable за
; замовчуванням = yes) — деінсталяція, як і раніше, повністю робоча
; (Панель керування → Програми і компоненти → Shaurm Launcher), включно
; з CurUninstallStepChanged (taskkill, RemoveFirewallRule, [UninstallDelete]).
Name: "{group}\{#MyAppName}";                       Filename: "{app}\{#MyShellExeName}"
Name: "{autodesktop}\{#MyAppName}";                 Filename: "{app}\{#MyShellExeName}"; Tasks: desktopicon

[Registry]
; ── Асоціація файлів .shaurmpack/.shaurmbundle (власні формати збірок) ──
; Двічі клік у Провіднику (або «Відкрити за допомогою → {#MyAppName}»)
; запускає shell.exe зі шляхом до файлу як аргументом; shell форвардить
; подію packfile:open у backend, фронтенд автоматично імпортує збірку
; (див. shell/main.go: parsePackFilePathArg + backend: handleShellEvent
; "packfile:open" / ImportShaurmbundleFile).
; Реєструємо в HKCU\Software\Classes (не HKLM): установка за замовчуванням
; пер-юзірська ({localappdata}), PrivilegesRequired=lowest — асоціація не
; потребує прав адміністратора, працює для «Open with» і подвійного кліку.
; Flags: uninsdeletekey* — деінсталяція прибирає ключі повністю (щоб
; видалений лаунчер не лишав у реєстрі «мертву» асоціацію).
Root: HKCU; Subkey: "Software\Classes\.shaurmpack";   ValueType: string; ValueName: ""; ValueData: "ShaurmLauncher.Shaurmpack";   Flags: uninsdeletekeyifempty
Root: HKCU; Subkey: "Software\Classes\.shaurmbundle"; ValueType: string; ValueName: ""; ValueData: "ShaurmLauncher.Shaurbundle"; Flags: uninsdeletekeyifempty
Root: HKCU; Subkey: "Software\Classes\ShaurmLauncher.Shaurmpack";   ValueType: string; ValueName: ""; ValueData: "Shaurma збірка";   Flags: uninsdeletekeyifempty
Root: HKCU; Subkey: "Software\Classes\ShaurmLauncher.Shaurbundle"; ValueType: string; ValueName: ""; ValueData: "Shaurma набір збірок"; Flags: uninsdeletekeyifempty
Root: HKCU; Subkey: "Software\Classes\ShaurmLauncher.Shaurmpack\DefaultIcon";   ValueType: string; ValueName: ""; ValueData: "{app}\{#MyShellExeName},0";   Flags: uninsdeletevalue
Root: HKCU; Subkey: "Software\Classes\ShaurmLauncher.Shaurbundle\DefaultIcon"; ValueType: string; ValueName: ""; ValueData: "{app}\{#MyShellExeName},0"; Flags: uninsdeletevalue
Root: HKCU; Subkey: "Software\Classes\ShaurmLauncher.Shaurmpack\shell\open\command";   ValueType: string; ValueName: ""; ValueData: """{app}\{#MyShellExeName}"" ""%1""";   Flags: uninsdeletevalue
Root: HKCU; Subkey: "Software\Classes\ShaurmLauncher.Shaurbundle\shell\open\command"; ValueType: string; ValueName: ""; ValueData: """{app}\{#MyShellExeName}"" ""%1"""; Flags: uninsdeletevalue

; ── Схема shaurma:// (запрошення в друзі з сайту) ──────────────────────────
; Сторінка https://shaurma-launcher.org/friend/@handle запускає
; shaurma://friend/@handle?n=Nick — браузер питає «Відкрити Shaurma Launcher?»
; і стартує shell.exe з URL як аргументом (shell/main.go: parseFriendLinkArg →
; подія friend:open → модалка «Додати друга»). Лише Shaurma-редакція: друзі є
; тільки в ній, а Base не має перехоплювати схему.
#if MyChannel == "shaurma"
Root: HKCU; Subkey: "Software\Classes\shaurma"; ValueType: string; ValueName: ""; ValueData: "URL:Shaurma Launcher"; Flags: uninsdeletekey
Root: HKCU; Subkey: "Software\Classes\shaurma"; ValueType: string; ValueName: "URL Protocol"; ValueData: ""
Root: HKCU; Subkey: "Software\Classes\shaurma\DefaultIcon"; ValueType: string; ValueName: ""; ValueData: "{app}\{#MyShellExeName},0"
Root: HKCU; Subkey: "Software\Classes\shaurma\shell\open\command"; ValueType: string; ValueName: ""; ValueData: """{app}\{#MyShellExeName}"" ""%1"""
#endif

[Run]
Filename: "{app}\{#MyShellExeName}"; \
  Description: "{cm:LaunchProgram,{#MyAppName}}"; \
  Flags: nowait postinstall skipifsilent

[UninstallDelete]
; Видалення лаунчера має прибирати ВСІ компоненти, які встановив/скачав
; сам лаунчер: exe, DLL, zip-архіви і розпаковані теки фронтенду. Раніше
; тут нічого не було, і після унінсталу в installDir лишались backend.dll,
; updater.dll, frontend.zip, frontend/, updater-frontend/ — «мертвий»
; лаунчер важив десятки МБ.
;
; ЗБЕРІГАЄМО лише НАЛАШТУВАННЯ користувача: launcher-location.json
; (шляхи до папки даних/збірок/Java), shaurma.db (SQLite: налаштування,
; акаунти, ігровий час, per-instance конфіги), а також легасі-JSON
; (accounts.json, settings.json, custom-packs.json, gallery-meta.json,
; instance-configs.json, playtime.json), які дублюють дані в БД і
; лишились від старіших версій. Вони не перелічені нижче, тож Inno їх
; не чіпає — після перевстановлення користувач отримає свій профіль
; назад. Папка даних (збірки, моди, Java, кеш, логи — baseDir, за
; замовчуванням %APPDATA%\.shaurm) теж лишається недоторканою:
; видалення лаунчера не повинно нищити збережені збірки користувача.
;
; ВАЖЛИВО: список нижче має синхронізуватись з компонентами маніфесту
; build.ps1 (launcher-version.json) — якщо додаси новий компонент
; (напр. runtime.zip), додай його і сюди, інакше він не видалиться при
; деінсталяції.
;
; Використовуємо filesandordirs (а не files) — треки видаляються
; рекурсивно разом зі всім вмістом (frontend/, updater-frontend/).
Type: filesandordirs; Name: "{app}\{#MyShellExeName}"
; Друге можливе ім'я shell.exe (інша редакція) — на випадок, якщо в теці
; лишився .exe іншої редакції (не мало б траплятись після
; CleanupOtherEditionArtifacts, але деінсталяція прибирає його про
; всяк випадок: одна тека — один застосунок, після видалення вона має
; лишитись порожньою від виконуваних файлів лаунчера).
Type: filesandordirs; Name: "{app}\{#MyOtherShellExeName}"
Type: filesandordirs; Name: "{app}\backend.dll"
Type: filesandordirs; Name: "{app}\updater.dll"
Type: filesandordirs; Name: "{app}\frontend.zip"
Type: filesandordirs; Name: "{app}\updater-frontend.zip"
Type: filesandordirs; Name: "{app}\frontend"
Type: filesandordirs; Name: "{app}\updater-frontend"
Type: filesandordirs; Name: "{app}\launcher-version.json"
Type: filesandordirs; Name: "{app}\launcher-version-beta.json"
Type: filesandordirs; Name: "{app}\bootstrap.log"
; Кеш WebView2 (двигун фронтенду) — теж компонент, видаляється повністю.
Type: filesandordirs; Name: "{app}\webview2"
; Тимчасові/проміжні файли апдейтера, що могли лишитись після обриву:
; .part (недокачані), .new (підготовлені до довстановлення), .previous
; (backup перед заміною файлу — включно з легасі launcher.exe.previous
; від монолітної версії).
Type: filesandordirs; Name: "{app}\*.part"
Type: filesandordirs; Name: "{app}\*.new"
Type: filesandordirs; Name: "{app}\*.previous"
; Файл <shell>~ — старий exe, відкладений self-update'ом shell'а вбік під час
; оновлення (див. applyShellSelfUpdate: активний exe -> ~, .new на місце).
; Зазвичай видаляється при наступному старті лаунчера, але якщо процес
; впав між двома rename (вікно краху) — ~ лишається на диску і без цього
; правила пережив би деінсталяцію.
Type: filesandordirs; Name: "{app}\*~"

[Code]
// ── Деінсталяція: збереження даних користувача ─────────────────────────
// Рішення: лаунчер при видаленні прибирає ЛИШЕ компоненти (exe/dll/UI),
// а дані користувача (папка даних .shaurm зі збірками, акаунтами,
// налаштуваннями, гардеробом) зберігаються — НЕ можна нищити збережені
// збірки мовчки. Але користувач може явно обрати «видалити все» — тоді
// папка даних теж прибирається. Для цього в деінсталяторі додана
// сторінка з чекбоксом (за замовчуванням ЗАЛИШИТИ дані).
var
  UninstallDeleteData: Boolean;
  UninstallDataDir: String;

// Папка даних лаунчера: беремо з launcher-location.json (що лежить біля
// exe і зберігає baseDir), а якщо його нема — системний дефолт
// %APPDATA%\.shaurm.
function GetDataDir(): String;
var
  JsonFile: String;
  BaseDir: String;
  // RawContent — AnsiString: LoadStringFromFile вимагає саме цей тип
  // (сигнатура (const FileName: String; var S: AnsiString)), тож читаємо
  // файл байт-в-байт сюди. Далі РУЧНО декодуємо як UTF-8 в Content
  // (звичайний Unicode String), інакше кирилиця/інші не-ASCII символи
  // в baseDir (напр. C:\Users\Олександр\...) перетворяться на кракозябри
  // і DelTree потім мовчки не знайде папку.
  RawContent: AnsiString;
  Content: String;
  Pos1, Pos2: Integer;
begin
  JsonFile := ExpandConstant('{app}\launcher-location.json');
  RawContent := '';
  if FileExists(JsonFile) then
    LoadStringFromFile(JsonFile, RawContent);
  // launcher пише файл у UTF-8 (encoding/json у Go); Utf8Decode перетворює
  // сирі UTF-8 байти (прочитані як AnsiString) у коректний Unicode String.
  Content := Utf8Decode(RawContent);
  Pos1 := Pos('"baseDir"', Content);
  if Pos1 > 0 then
  begin
    // Inno-шний Pos не підтримує 3-й аргумент (offset) — шукаємо ':'
    // у хвості рядка, починаючи з позиції знайденого ключа.
    Pos1 := Pos(':', Copy(Content, Pos1, Length(Content) - Pos1 + 1)) + Pos1;
    while (Pos1 <= Length(Content)) and ((Content[Pos1] = ' ') or (Content[Pos1] = '"')) do
      Pos1 := Pos1 + 1;
    Pos2 := Pos1;
    while (Pos2 <= Length(Content)) and (Content[Pos2] <> '"') do
      Pos2 := Pos2 + 1;
    BaseDir := Copy(Content, Pos1, Pos2 - Pos1);
    if BaseDir <> '' then
    begin
      Result := BaseDir;
      Exit;
    end;
  end;
  Result := ExpandConstant('{userappdata}\.shaurm');
end;

function InitializeUninstall: Boolean;
var
  Answer: Integer;
begin
  UninstallDataDir := GetDataDir();
  // Питання про видалення даних користувача — ПЕРЕД початком видалення.
  //
  // ВАЖЛИВО: робимо це через MsgBox, а НЕ через CreateCustomPage. Inno
  // Setup забороняє CreateCustomPage у фазі Uninstall (помилка "Cannot
  // call CreateCustomPage function during Uninstall"): кастомні сторінки
  // доступні лише майстру ВСТАНОВЛЕННЯ (InitializeWizard). Раніше тут
  // була кастомна сторінка з чекбоксом — і деінсталяція падала одразу
  // при запуску, лаунчер було неможливо видалити.
  Answer := MsgBox('Видалити також всі дані користувача (збірки, акаунти, налаштування, гардероб)?' + #13#10 + #13#10 + 'Якщо Ні — дані збережуться і будуть доступні після повторного встановлення лаунчера.', mbConfirmation, MB_YESNO or MB_DEFBUTTON2);
  UninstallDeleteData := (Answer = IDYES);
  // Обов'язковий результат функції (Inno вимагає InitializeUninstall: Boolean).
  Result := True;
end;

// ── Виявлення апгрейду (для welcome-екрану майстра) ────────────────────
function IsUpgrade(): Boolean;
begin
  Result := RegKeyExists(HKCU, 'Software\Microsoft\Windows\CurrentVersion\Uninstall\{#MyAppIdGUID}_is1');
end;

// ── Перевірка підпису САМОГО інсталятора ───────────────────────────────
// Раніше тут запускався прихований `powershell.exe -ExecutionPolicy Bypass`
// (Get-AuthenticodeSignature) — функція була ТИМЧАСОВО вимкнена, але сам код
// лишався в setup.exe, а «PowerShell + Bypass + SW_HIDE» у скрипті
// інсталятора — типовий тригер антивірусних ML-евристик («Trojan/Stealer»).
// Тому код прибрано повністю. Справжній захист — Authenticode-підпис
// setup.exe (Windows сама показує видавця у вікні UAC/SmartScreen).
// CustomMessages UnsignedWarnText/ForeignSignerWarnText лишені на випадок,
// якщо перевірку повернуть нативним способом (без PowerShell).

// Кнопка на сторінці App Control: відкриває офіційну документацію Microsoft
// у браузері за замовчуванням. Якщо відкрити не вдалося — показуємо
// посилання текстом, щоб його можна було скопіювати.
procedure AppControlDocButtonClick(Sender: TObject);
var
  ErrorCode: Integer;
begin
  if not ShellExec('open', CustomMessage('AppControlDocUrl'), '', '', SW_SHOWNORMAL, ewNoWait, ErrorCode) then
    MsgBox(CustomMessage('AppControlDocUrl'), mbInformation, MB_OK);
end;

procedure InitializeWizard;
var
  AppControlPage: TWizardPage;
  AppControlMemo: TNewMemo;
  AppControlDocBtn: TNewButton;
begin
  // Сторінка про Windows App Control (Smart App Control) — одразу після
  // привітання. У silent-режимі кастомні сторінки не показуються.
  AppControlPage := CreateCustomPage(wpWelcome,
    CustomMessage('AppControlPageTitle'), CustomMessage('AppControlPageDesc'));

  AppControlDocBtn := TNewButton.Create(AppControlPage);
  AppControlDocBtn.Parent := AppControlPage.Surface;
  AppControlDocBtn.Caption := CustomMessage('AppControlDocButton');
  AppControlDocBtn.Width := ScaleX(300);
  AppControlDocBtn.Height := ScaleY(26);
  AppControlDocBtn.Left := 0;
  AppControlDocBtn.Top := AppControlPage.SurfaceHeight - AppControlDocBtn.Height;
  AppControlDocBtn.OnClick := @AppControlDocButtonClick;

  AppControlMemo := TNewMemo.Create(AppControlPage);
  AppControlMemo.Parent := AppControlPage.Surface;
  AppControlMemo.Left := 0;
  AppControlMemo.Top := 0;
  AppControlMemo.Width := AppControlPage.SurfaceWidth;
  AppControlMemo.Height := AppControlDocBtn.Top - ScaleY(8);
  AppControlMemo.ScrollBars := ssVertical;
  AppControlMemo.ReadOnly := True;
  AppControlMemo.WordWrap := True;
  AppControlMemo.Text := CustomMessage('AppControlText');

  if IsUpgrade() then
  begin
    // ПРИМІТКА: setup.exe більше НЕ знає реальну версію релізу (AppVersion
    // фіксована "1.0.0" заради сталого хешу файлу — див. коментар вище),
    // тому текст без номера версії: перевстановлення exe → bootstrap при
    // першому запуску підмінить себе на актуальний shell і той довантажить
    // актуальні компоненти з CDN.
    WizardForm.WelcomeLabel2.Caption :=
      'Це оновить ' + ExpandConstant('{#MyAppName}') + ' до останньої версії.' + #13#10 +
      'Ваші дані та налаштування будуть збережені.';
  end;
end;

// ── Перемикання редакції (shaurma ↔ base) в тій самій installDir ───────
// Обидві редакції ставляться в ОДНУ теку (спільний AppId/DefaultDirName —
// див. коментар на початку файлу). Якщо в цій теці лежить .exe ІНШОЇ
// редакції — це залишок від попередньої інсталяції, яку зараз витісняють
// (поставили shaurma поверх base чи навпаки). Перед тим, як [Files]
// покладе новий bootstrap, прибираємо: сам чужий .exe, його firewall-
// правило і будь-які artefacts незавершеної заміни (.previous/.new),
// що могли лишитись від нього.
//
// НЕ ЧІПАЄ: launcher-location.json, ui.json, shaurma.db та інші дані
// користувача, backend.dll/updater.dll/frontend.zip/frontend/ — усе це
// СПІЛЬНІ файли з однаковим іменем для обох редакцій (перезаписуються
// звичайним [Files]/апдейтером, тут видаляти їх не треба).
procedure CleanupOtherEditionArtifacts;
var
  OtherExePath: String;
  ResultCode: Integer;
begin
  OtherExePath := ExpandConstant('{app}\{#MyOtherShellExeName}');
  if not FileExists(OtherExePath) then
    Exit;
  // Якщо інша редакція запущена зараз — завершуємо процес, інакше
  // DeleteFile нижче мовчки не спрацює (Windows тримає лок на запущений
  // exe).
  Exec('taskkill.exe', '/IM {#MyOtherShellExeName} /F', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  // Знімаємо firewall-правило попередньої редакції — інакше воно лишиться
  // висіти, вказуючи на вже видалений файл.
  Exec('netsh.exe',
    'advfirewall firewall delete rule name="{#MyRulePrefix} Out-{#MyOtherShellExeName}"',
    '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  DeleteFile(OtherExePath);
  DeleteFile(OtherExePath + '.previous');
  DeleteFile(OtherExePath + '.new');
  DeleteFile(OtherExePath + '~');
end;

// ── Windows Firewall ────────────────────────────────────────────────────
// Лише ОДИН процес виходить у мережу: shell.exe (Modrinth/CurseForge API,
// завантаження збірок, перевірка оновлень — і backend.dll, і updater.dll
// живуть у тому самому процесі). Loopback-правила для internal IPC не
// потрібні — ніякого TCP між процесами немає.
procedure RegisterFirewallRule;
var
  ResultCode: Integer;
begin
  Exec('netsh.exe',
    'advfirewall firewall delete rule name="{#MyRulePrefix} Out"',
    '', SW_HIDE, ewWaitUntilTerminated, ResultCode);

  Exec('netsh.exe',
    'advfirewall firewall delete rule name="{#MyRulePrefix} Out-{#MyShellExeName}"',
    '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  Exec('netsh.exe',
    'advfirewall firewall add rule' +
    ' name="{#MyRulePrefix} Out-{#MyShellExeName}"' +
    ' dir=out' +
    ' action=allow' +
    ' program="' + ExpandConstant('{app}\{#MyShellExeName}') + '"' +
    ' profile=private' +
    ' description="Outbound access for {#MyShellExeName} (CDN/API)"',
    '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
end;

procedure RemoveFirewallRule;
var
  ResultCode: Integer;
begin
  Exec('netsh.exe',
    'advfirewall firewall delete rule name="{#MyRulePrefix} Out-{#MyShellExeName}"',
    '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  // Про всяк випадок — правило іншої редакції, якщо колись лишилось
  // невитерте (див. CleanupOtherEditionArtifacts, яка мала б знімати
  // його ще при встановленні наступної редакції).
  Exec('netsh.exe',
    'advfirewall firewall delete rule name="{#MyRulePrefix} Out-{#MyOtherShellExeName}"',
    '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
end;

// ── Після встановлення: лише firewall-правило ───────────────────────────
// Решту компонентів (updater.dll, backend.dll, frontend.zip...) качає
// САМ (реальний) shell.exe при першому запуску (bootstrap/main.go:
// ОДНОРАЗОВИЙ Shell A підміняє себе на Shell B; далі Shell B качає
// updater.dll з прогресом у splash, потім апдейтер качає решту).
// Інсталятор не знає про хеші цих компонентів — тому setup.exe завжди
// одного розміру і навіть річний setup поставить актуальну версію.
//
// CleanupOtherEditionArtifacts викликається ПЕРШОЮ — до реєстрації
// нового firewall-правила, — щоб прибрати .exe/правило попередньої
// редакції, якщо в цій самій installDir щойно було виявлено іншу
// (перемикання shaurma ↔ base в спільній теці).
procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssPostInstall then
  begin
    CleanupOtherEditionArtifacts();
    RegisterFirewallRule();
  end;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  ResultCode: Integer;
begin
  // Якщо лаунчер ЗАПУЩЕНИЙ під час видалення, Windows не дасть видалити
  // завантажені файли (ShaurmLauncher.exe, backend.dll, updater.dll — усі
  // в пам'яті процесу), і [UninstallDelete] мовчки їх пропустить. Тому
  // ПЕРЕД видаленням файлів примусово завершуємо процес (taskkill /F,
  // помилка ігнорується — процесу могло й не бути). Обидва можливих
  // імені .exe — на випадок, якщо в теці лишився файл іншої редакції
  // (див. [UninstallDelete]: MyOtherShellExeName).
  if CurUninstallStep = usUninstall then
  begin
    Exec('taskkill.exe', '/IM {#MyShellExeName} /F', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
    Exec('taskkill.exe', '/IM {#MyOtherShellExeName} /F', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  end;

  if CurUninstallStep = usPostUninstall then
  begin
    RemoveFirewallRule();
    // Користувач явно обрав «видалити все» — прибираємо папку даних
    // (.shaurm: збірки, акаунти, налаштування, гардероб). Інакше вона
    // зберігається (див. коментар до InitializeUninstall).
    if UninstallDeleteData then
    begin
      // Папка даних може бути поза {app} (користувач сам обрав baseDir).
      if Length(UninstallDataDir) > 0 then
        DelTree(UninstallDataDir, True, True, True);
      // База даних і конфіг у теці встановлення теж дані користувача.
      DeleteFile(ExpandConstant('{app}\shaurma.db'));
      DeleteFile(ExpandConstant('{app}\shaurma.db-wal'));
      DeleteFile(ExpandConstant('{app}\shaurma.db-shm'));
      DeleteFile(ExpandConstant('{app}\launcher-location.json'));
      DeleteFile(ExpandConstant('{app}\ui.json'));
    end;
  end;
end;
