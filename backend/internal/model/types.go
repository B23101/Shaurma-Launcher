package model

import "time"

type Instance struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Loader        string `json:"loader"`
	MCVersion     string `json:"mcVersion"`
	LoaderVersion string `json:"loaderVersion"`
	Icon          string `json:"icon"`
	Color         string `json:"color"`
	Type          string `json:"type"`
	Status        string `json:"status"`
	Downloaded    int64  `json:"downloaded"`
	TotalBytes    int64  `json:"totalBytes"`
	IsShaurma     bool   `json:"isShaurma"`
	GameDir       string `json:"gameDir"`
}

// GameLanguage — одна мова гри з ОРИГІНАЛЬНОГО asset index Mojang
// (minecraft/lang/<code>.json). Name — читабельна назва з самого файлу
// мови (language.name + language.region); може бути порожньою, тоді UI
// показує код.
type GameLanguage struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type Account struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	Type          string `json:"type"`
	AccessToken   string `json:"accessToken"`
	MsAccessToken string `json:"msAccessToken"`
	RefreshToken  string `json:"refreshToken"`
	ExpiresAt     int64  `json:"expiresAt"`
	UUID          string `json:"uuid"`
	IsLicensed    bool   `json:"isLicensed"`
	// MsIDToken — id_token Microsoft (openid), отриманий під час входу. Це
	// ЄДИНЕ, що лаунчер віддає на сервер Шаурми для входу: сервер сам
	// перевіряє підпис id_token (JWKS Microsoft) і бере з нього `sub` як
	// особу. Жодних Xbox Live / XUID / Gamertag / Minecraft-токенів на
	// сервер не йде. Токен ТРАНЗИТНИЙ: живе лише в пам'яті між входом у
	// Microsoft і реєстрацією акаунта Шаурми (`json:"-"` — ні в конфіг,
	// ні у фронтенд він не потрапляє).
	MsIDToken string `json:"-"`
	// IsShaurmaSkinAccount — true, якщо цей (неліцензійний) акаунт зараз
	// прив'язаний до активної Google-сесії скін-системи Shaurma (нік
	// акаунта збігається з ніком, зареєстрованим у скін-акаунті). Лише
	// Shaurma-редакція заповнює це поле (див. backend/wardrobe_inject.go);
	// у base-збірці завжди false. Використовується у списку акаунтів,
	// щоб показувати такий акаунт як «Шаурма-акаунт», а не «Не ліцензійний».
	IsShaurmaSkinAccount bool `json:"isShaurmaSkinAccount"`
	// LicensedNickMine — true, якщо нік цього (неліцензійного) акаунта
	// зайнятий ЛІЦЕНЗІЙНИМ акаунтом із ЦЬОГО Ж лаунчера. Пріоритет
	// ліцензійного: у грі скін беруть з Mojang (worker/yggdrasil.js), а
	// завантаження скіна та застосування пресету під цим ніком
	// відхиляються (backend/wardrobe_inject.go + 409 від воркера). UI
	// показує бейдж, щоб гравець не дивувався відмові. Лише Shaurma-
	// редакція заповнює це поле; у base-збірці завжди false.
	LicensedNickMine bool `json:"licensedNickMine"`
	// Xbox Live social-токени для розділу «Друзі»: XblToken — XSTS-токен
	// з релеєм http://xboxlive.com (People Hub), XblUHS — user hash,
	// XboxUserID (xuid) і Gamertag — з claims XSTS. Отримуються під час
	// входу Microsoft (див. auth.Authenticator.authenticateSocialXSTS).
	// Для неліцензійних/старих акаунтів порожні — друзі недоступні.
	XblToken   string `json:"xblToken"`
	XblUHS     string `json:"xblUHS"`
	XboxUserID string `json:"xboxUserID"`
	Gamertag   string `json:"gamertag"`
}

// XboxFriend — друг з People Hub + присутність (одна сутність для UI).
// State: "online" | "in-game" | "offline". Joinable — друг у грі Minecraft
// (відомий titleId) — для кнопки «Приєднатися».
type XboxFriend struct {
	XUID         string `json:"xuid"`
	Gamertag     string `json:"gamertag"`
	Avatar       string `json:"avatar"` // displayPicRaw (gamerpic URL)
	State        string `json:"state"`
	Title        string `json:"title"`        // назва гри, якщо в грі
	TitleID      uint32 `json:"titleId"`      // Xbox titleId (0 = не в грі)
	RichPresence string `json:"richPresence"` // текст активності
	Joinable     bool   `json:"joinable"`     // Minecraft + є адреса сервера
	JoinAddress  string `json:"joinAddress"`  // IP/домен з richPresence
}

// XboxPrivacy — налаштування приватності Xbox + локальний перемикач
// сповіщень. shareFriendsList/allowFriendRequests — з privacy API
// (Everyone/OnlyMe/Blocked...); onlineNotify — з settings.json лаунчера.
type XboxPrivacy struct {
	ShareFriendsList    string `json:"shareFriendsList"`
	AllowFriendRequests string `json:"allowFriendRequests"`
	OnlineNotify        bool   `json:"onlineNotify"`
}

// BuildConsoleLine — рядок консолі з контекстом збірки/акаунта (подія
// buildConsole:line). Потрібен для «одна консоль на збірку з перемикачем
// акаунтів»: фронтенд фільтрує по buildID і групує по accountID.
type BuildConsoleLine struct {
	BuildID   string `json:"buildId"`
	AccountID string `json:"accountId"`
	Line      string `json:"line"`
}

// BuildStartEvent — контекст запуску збірки (подія build:started).
// StartedAt — unix-мілісекунди моменту старту сесії: для ПЕРШОГО запуску
// це момент старту процесу гри, а для ВІДНОВЛЕНОЇ сесії (після перезапуску
// лаунчера, restoreRunningGame) — збережений час старту з running-games.json.
// Фронтенд використовує його для таймера «у сесії» — щоб кнопка «Зупинити»
// не втрачала вже прожитий час після перезапуску лаунчера.
type BuildStartEvent struct {
	BuildID     string `json:"buildId"`
	AccountID   string `json:"accountId"`
	AccountName string `json:"accountName"`
	StartedAt   int64  `json:"startedAt"`
}

// BuildExitEvent — контекст завершення збірки (подія build:exit).
type BuildExitEvent struct {
	BuildID   string `json:"buildId"`
	AccountID string `json:"accountId"`
	Code      int    `json:"code"`
	// WasUserStop — true, якщо вихід стався через явну ручну зупинку
	// гравцем (кнопка «Зупинити» / StopInstance), а не через крах.
	// ВАЖЛИВО: примусово вбитий процес (kill/taskkill), який робить
	// StopInstance, майже завжди повертає НЕНУЛЬОВИЙ код виходу на
	// рівні ОС — це нормальна поведінка для вбитого процесу, а не
	// ознака краху. Без цього поля фронтенд бачив лише code!=0 і
	// показував "гра завершилась аварійно" навіть тоді, коли гравець
	// сам зупинив гру кнопкою в лаунчері.
	WasUserStop bool `json:"wasUserStop"`
}

// ConsoleAIEvent — новий AI-діагноз крашу для КОНКРЕТНОЇ збірки (подія
// console:ai). buildID дозволяє у режимі «кілька вікон консолі по збірках»
// реагувати лише консолям ЦІЄЇ збірки: вікно/вкладка іншої збірки не
// отримує чужий діагноз.
type ConsoleAIEvent struct {
	BuildID   string      `json:"buildId"`
	Diagnosis AIDiagnosis `json:"diagnosis"`
}

type Settings struct {
	Language     string `json:"language"`
	Accent       string `json:"accent"`
	AccentCustom string `json:"accentCustom"` // hex-колір 1 для акценту "custom" (пікер)
	// AccentCustom2 — другий колір градієнта для кастомного акценту.
	// Використовується лише коли AccentCustomMode == "gradient"; при
	// "flat" ігнорується (обидві точки CSS-градієнта = AccentCustom,
	// тобто фактично суцільний колір без видимого переходу).
	AccentCustom2 string `json:"accentCustom2"`
	// AccentCustomMode — "gradient" (два кольори, лінія 135deg) чи "flat"
	// (один колір, другий тон вираховується автозатемненням, як раніше).
	AccentCustomMode string `json:"accentCustomMode"`
	Theme            string `json:"theme"` // dark | light
	// SidebarAnimAlways — анімувати іконки збірок у sidebar ЗАВЖДИ
	// (не лише для вибраної збірки). За замовчуванням GIF/анімовані іконки
	// у sidebar грають лише для активної (вибраної) збірки, щоб не
	// навантажувати слабкі ПК.
	SidebarAnimAlways bool `json:"sidebarAnimAlways"`
	// PinnedServersCompact — компактний вигляд закріплених серверів (лише
	// іконки в один ряд). Керується з Налаштування → Вигляд.
	PinnedServersCompact bool `json:"pinnedServersCompact"`
	// AnimationsEnabled — вмикає/вимикає декоративні UI-анімації (поява
	// карток, pop/deactivate у sidebar, переходи вкладок налаштувань
	// тощо). За замовчуванням true; вимикається в «Вигляд» для тих, кому
	// незручно/некомфортно (укачування, слабший ПК, особисті переваги).
	// НЕ впливає на функціональні transition'и (наприклад toast'и) —
	// лише на «косметичні» списки/hover/активацію.
	AnimationsEnabled bool `json:"animationsEnabled"`
	// PackBannerFull — банер сторінки збірки на всю ширину сторінки
	// (повноширинний геро-блок замість компактного в колонці контенту).
	// Керується з Налаштування → Вигляд; за замовчуванням вимкнено.
	PackBannerFull bool   `json:"packBannerFull"`
	Font           string `json:"font"`     // system | inter | jetbrains | custom
	FontPath       string `json:"fontPath"` // шлях до кастомного TTF (font=custom)
	AutoUpdate     bool   `json:"autoUpdate"`
	UpdateChannel  string `json:"updateChannel"`
	// LastUpdateCheck — час останньої успішної перевірки оновлень
	// (RFC3339; порожнє = ще не перевірялось). Показується у вкладці
	// «Про лаунчер». Пишеться з App.CheckUpdate.
	LastUpdateCheck string `json:"lastUpdateCheck"`
	ShowConsole     bool   `json:"showConsole"`
	SaveLogs        bool   `json:"saveLogs"`
	// AutoDownloadAfterCreate — одразу після створення/імпорту збірки
	// завантажити ВСЕ необхідне (Java, лоадер, клієнт Minecraft,
	// бібліотеки, асети) у фоні — як Prism, який питає «download all
	// necessary files now?». Перший запуск стає миттєвим. УВІМКНЕНО за
	// замовчуванням (користувач завжди може зняти галочку).
	AutoDownloadAfterCreate bool   `json:"autoDownloadAfterCreate"`
	InstanceDir             string `json:"instanceDir"`
	JavaPath                string `json:"javaPath"`
	MaxRAM                  int    `json:"maxRAM"`
	JavaArgs                string `json:"javaArgs"`
	// Активний (обраний для гри) акаунт. Зберігається в settings.json —
	// перемикання між акаунтами переживає перезапуск лаунчера.
	ActiveAccountID string `json:"activeAccountId"`

	// ── Вимкнення окремих підсистем ──
	// ІНВЕРТОВАНІ назви ("disable…"), а не "enable…": у Go відсутнє поле
	// в старому settings.json дає false, тому пряме "skinsEnabled" вимкнуло б
	// фічу всім користувачам при оновленні лаунчера. Так само й у схемі
	// (config/schema.go) Default: false = «увімкнено».
	//
	// DisableSkins — не чіпати скін-систему: лаунчер не додає java-агент
	// authlib-injector і не показує скін-UI. Корисно, якщо користувач не
	// хоче, щоб лаунчер взагалі ходив у Yggdrasil-API.
	DisableSkins bool `json:"disableSkins"`
	// DisableMojangSkinFallback — вимкнути другий потік локального injector'а
	// (скін із Mojang за ніком, коли в Shaurma його немає). Тоді гра, як і
	// раніше, ходить напряму на сервер Shaurma. Інвертована назва з тієї ж
	// причини, що й DisableSkins: відсутнє поле в старому settings.json = false
	// = «увімкнено».
	DisableMojangSkinFallback bool `json:"disableMojangSkinFallback"`
	// DisableP2P — не показувати й не піднімати «LAN через інтернет»
	// (Headscale/tsnet). Фіча не працює, поки на worker'і не налаштований
	// Headscale, тож її можна вимкнути, щоб UI не показував мертвих кнопок.
	DisableP2P bool `json:"disableP2P"`
	// DisableCloudPlaytime — НЕ зберігати загальний ігровий час у хмарі (shaurma-акаунт).
	// Інвертована назва (як DisableP2P): відсутнє поле в старому settings.json = false = «увімкнено».
	// Локальна аналітика по збірках від цього не залежить.
	DisableCloudPlaytime bool `json:"disableCloudPlaytime"`

	// ── Згоди на телеметрію ──
	// AnalyticsEnabled — користувач погодився надсилати анонімну статистику (analytics.go).
	// За замовчуванням false: без явної згоди жодного запиту не робиться.
	AnalyticsEnabled bool `json:"analyticsEnabled"`
	// ErrorReportsEnabled — користувач погодився, щоб його помилки лаунчера аналізував розробник
	// (errorreports_api.go). За замовчуванням false.
	ErrorReportsEnabled bool `json:"errorReportsEnabled"`
	// ErrorReportsPromptShown — модалку згоди на звіти про помилки вже показано (більше не спливає).
	ErrorReportsPromptShown bool `json:"errorReportsPromptShown"`

	// ── Сповіщення ──
	// Віджет сповіщень у трей (notify_window.go): тост у правому нижньому
	// куті з превʼю повідомлення в чаті чи запитом у друзі.
	//
	// NotifyEnabled — головний вимикач. ВИМКНЕНО = тости взагалі не
	// створюються (вікно віджета навіть не породжується), тож вимкнення
	// економить і памʼять, і увагу користувача. За замовчуванням УВІМКНЕНО.
	NotifyEnabled bool `json:"notifyEnabled"`
	// NotifyDurationSec — скільки секунд лишається картка на екрані.
	// 0 у збереженому файлі означає «не задано» — normalize() підставить
	// дефолт (7 с), а після збереження з UI значення завжди у діапазоні
	// 3..60 (межі зафіксовані в SettingsSchema).
	NotifyDurationSec int `json:"notifyDurationSec"`
	// NotifyDnd — «Не турбувати»: тимчасовий режим тиші. Відмінність від
	// NotifyEnabled у тому, що це ШВИДКИЙ перемикач на час — користувач
	// може вимкнути його пізніше, а налаштування вище (вимкнення/час
	// показу) лишаться такими, як він їх налаштував. За замовчуванням
	// вимкнено (сповіщення показуються).
	NotifyDnd bool `json:"notifyDnd"`
	// PresenceMode — ручний статус для друзів: "" (в мережі), "away" (відійшов) або
	// "invisible" (невидимий: друзі бачать офлайн). «Не турбувати» — окремо, NotifyDnd.
	PresenceMode string `json:"presenceMode"`
	// ── Мути чату ──
	// ChatMuteAll — загальний мут чатів: повідомлення з усіх діалогів не
	// показуються як тости (сам чат і історія лишаються доступними — це
	// НЕ втрата даних, а вибір «зараз граю, не відволікати»). Діє разом із
	// NotifyDnd (два незалежних вимикачі: DND — тимчасовий режим, цей —
	// стійке налаштування чатів).
	ChatMuteAll bool `json:"chatMuteAll"`
	// ChatMuteSound — мут ЗВУКУ сповіщень чату (окремо від видимості
	// тостів: звук приходить потім, а перемикач потрібен уже зараз — інакше
	// мут «по звуку» і мут «по видимості» довелось би мішати в один).
	ChatMuteSound bool `json:"chatMuteSound"`
	// MutedChats — акаунти, чиї повідомлення не сповіщають (мут ОДНОГО
	// діалогу). Список зберігається локально і не залежить від дружби —
	// чат лишається мутнутим і після видалення друга.
	MutedChats []string `json:"mutedChats"`

	// ── Консоль ──
	// Ліміт буфера — у РЯДКАХ (consoleMaxLines), не символах: користувачу
	// зрозуміліше, і Prism/термінали теж рахують рядками. Розмір консолі
	// (consoleWidth) прибрано: консоль розтягується по розміру вікна.
	ConsoleMaxLines   int    `json:"consoleMaxLines"`   // обмеження буфера консолі (рядків)
	ConsoleInfoColor  string `json:"consoleInfoColor"`  // hex кольору звичайних повідомлень
	ConsoleWarnColor  string `json:"consoleWarnColor"`  // hex кольору попереджень
	ConsoleErrorColor string `json:"consoleErrorColor"` // hex кольору помилок
	ConsoleWrap       bool   `json:"consoleWrap"`       // перенесення довгих рядків (як у Prism)
	ConsoleAI         bool   `json:"consoleAI"`         // AI-аналіз помилок крашу  // ConsoleAIKey — власний Gemini API key користувача (поле у
	// Налаштуваннях → Консоль). Порожнє = використовувати файл gemini.key,
	// env GEMINI_API_KEY або вбудований дефолт (див. ai.ResolveAPIKey).
	ConsoleAIKey string `json:"consoleAIKey"`
	// Розмір і шрифт тексту консолі (дефолт 12px / моноширинний).
	ConsoleFontSize   int    `json:"consoleFontSize"`   // розмір шрифту консолі (px)
	ConsoleFontFamily string `json:"consoleFontFamily"` // mono | sans (шрифт для ВСІХ типів)
	// Шрифт консолі окремо для кожного типу повідомлення (ТЗ: «має бути
	// режим, де шрифт консолі різний для info/warn/error»).
	// ConsoleFontMode: "all" — один шрифт (consoleFontFamily) для всіх
	// типів; "perType" — кожен тип використовує свій (поля нижче).
	// Значення полів: "mono" | "sans" (ті самі опції, що consoleFontFamily).
	ConsoleFontMode  string `json:"consoleFontMode"`  // all | perType
	ConsoleInfoFont  string `json:"consoleInfoFont"`  // mono | sans (коли mode=perType)
	ConsoleWarnFont  string `json:"consoleWarnFont"`  // mono | sans
	ConsoleErrorFont string `json:"consoleErrorFont"` // mono | sans
	// Шлях до власного TTF/OTF шрифту консолі (коли обрано 'custom').
	// WebView завантажує його через App.LoadFontFile (data: URL) — сам
	// шлях на фронтенді не читається напряму.
	ConsoleFontPath     string `json:"consoleFontPath"`
	ShowConsoleOnLaunch bool   `json:"showConsoleOnLaunch"`
	ShowConsoleOnCrash  bool   `json:"showConsoleOnCrash"`
	ShowConsoleOnClose  bool   `json:"showConsoleOnClose"`
	// DisableAutoRepair — вимкнути авто-діагностику після краху гри
	// (діалог «Авто-починка»). false = увімкнено. Ручна діагностика працює завжди.
	DisableAutoRepair bool `json:"disableAutoRepair"`

	// ── Завдання (ліміти завантажень) ──
	MaxConcurrentDownloads int `json:"maxConcurrentDownloads"`
	MaxRetries             int `json:"maxRetries"`
	HTTPTimeoutSec         int `json:"httpTimeoutSec"`

	// ── Команди ──
	PreLaunchCommand string `json:"preLaunchCommand"` // виконується ДО запуску гри
	WrapperCommand   string `json:"wrapperCommand"`   // обгортка запуску (optirun тощо)
	PostExitCommand  string `json:"postExitCommand"`  // виконується ПІСЛЯ виходу з гри
	EnvVars          string `json:"envVars"`          // KEY=VALUE, по одному в рядку

	// ── Вікно гри ──
	Fullscreen   bool `json:"fullscreen"`
	WindowWidth  int  `json:"windowWidth"`
	WindowHeight int  `json:"windowHeight"`
	// ── Поведінка вікна лаунчера (див. internal/winbehavior) ──
	// Один параметр на етап замість трьох незв'язаних булів.
	//
	// StartupMode — запуск разом із системою: never | window | background.
	// «background» = без вікна, лише трей (чати/сповіщення працюють).
	StartupMode string `json:"startupMode"`
	// CloseAction — що робить кнопка ×: quit (вийти) | tray (сховати в
	// трей, лаунчер лишається у фоні).
	CloseAction string `json:"closeAction"`
	// MinimizeAction — що робить кнопка «Згорнути»: taskbar | tray.
	MinimizeAction string `json:"minimizeAction"`
	// GameLaunchAction — що робити з вікном лаунчера після старту гри:
	// keep | minimize | tray.
	GameLaunchAction string `json:"gameLaunchAction"`
	// GameLaunchDelaySec — через скільки секунд після старту гри
	// згортати/ховати вікно (0 = одразу). Дає встигнути побачити
	// прогрес/помилку запуску.
	GameLaunchDelaySec int `json:"gameLaunchDelaySec"`
	// GameExitAction — що робити, коли гра закрилась: restore (повернути
	// вікно) | stay (нічого) | quit (закрити лаунчер повністю).
	GameExitAction string `json:"gameExitAction"`
	// RestoreOnCrash — якщо гра впала, повернути вікно навіть при
	// «залишити»/«закрити лаунчер», щоб користувач побачив помилку.
	RestoreOnCrash bool `json:"restoreOnCrash"`
	// ConfirmOnStop — показувати вікно підтвердження перед зупинкою
	// запущеної збірки (кнопка «Зупинити»). Вимкнено = зупиняти одразу.
	// За замовчуванням увімкнено (ТЗ: «вікно чи хочете зупинити збірку»).
	ConfirmOnStop bool `json:"confirmOnStop"`

	// ── Ігровий час ──
	ShowGameTime      bool `json:"showGameTime"`
	RecordGameTime    bool `json:"recordGameTime"`
	ShowTotalGameTime bool `json:"showTotalGameTime"`
	GameTimeInHours   bool `json:"gameTimeInHours"`
	// ShowSessionTime — показувати на сторінці збірки, скільки гра вже
	// запущена на поточному акаунті (живий лічильник сесії).
	ShowSessionTime bool `json:"showSessionTime"`
	// ShowGameTimeOnPage — показувати на сторінці збірки загальний час,
	// проведений у цій збірці (з playtime.json).
	ShowGameTimeOnPage bool `json:"showGameTimeOnPage"`

	// ── Тимчасові моди («становлення на строк») ──────────────────────
	// TempModsDeleteByDefault — що робити з тимчасовим модом за
	// замовчуванням, коли настає його дата: true = видалити файл
	// повністю, false = вимкнути (перейменувати на .jar.disabled — мод
	// лишається на диску, можна повернути вручну без повторного
	// завантаження). Впливає лише на НОВІ тимчасові мітки, що ставляться
	// без явного вибору дії (див. custompack.TempModEntry.Action) — уже
	// поставлені мітки зберігають свій вибір незалежно від подальшої
	// зміни цієї настройки. За замовчуванням false (вимикати) — «за
	// замовчуванням вимикаємо», як просив ужиток: вимкнення безпечніше
	// (мод не губиться безповоротно при помилковій даті).
	TempModsDeleteByDefault bool `json:"tempModsDeleteByDefault"`

	// ── Попередження перед запуском ──
	// ShowRamWarnings — головний вимикач усіх RAM-попереджень (і про
	// рекомендовану, і про нестачу фізичної оперативки). Вимкнено =
	// попередження взагалі не показуються (ТЗ: «у налаштуваннях лаунчера
	// можна виключити це попередження»).
	ShowRamWarnings bool `json:"showRamWarnings"`
	// ── Попередження перед запуском СЕРВЕРА ──
	// ShowServerWarnings — головний вимикач «розумного аналізатора»
	// запуску сервера (порт/RAM/Java/EULA/диск/минулий краш). ВАЖЛИВО:
	// навіть із вимкненим головним перемикачем ПОРТ показується завжди —
	// на зайнятому порту сервер фізично не стартує, приховувати причину
	// означало б лишити людину з мовчазною відмовою.
	ShowServerWarnings bool `json:"showServerWarnings"`
	// Окремі «більше не показувати» для точок, які можна ігнорувати
	// (порт і EULA свідомо НЕ мають таких перемикачів — вони блокери).
	ServerWarnIgnoreRAM   bool `json:"serverWarnIgnoreRam"`
	ServerWarnIgnoreJava  bool `json:"serverWarnIgnoreJava"`
	ServerWarnIgnoreDisk  bool `json:"serverWarnIgnoreDisk"`
	ServerWarnIgnoreCrash bool `json:"serverWarnIgnoreCrash"`

	// ── Discord Rich Presence ──
	// DiscordRPCEnabled — показувати присутність у Discord за запущеними
	// збірками. За замовчуванням увімкнено (як у старого лаунчера).
	DiscordRPCEnabled bool `json:"discordRpcEnabled"`
	// DiscordRPCDetails / DiscordRPCState — шаблони тексту присутності.
	// Плейсхолдери: {pack} (назва збірки або «N збірок»), {list} (список
	// збірок), {count}, {loader}, {mc}. Порожнє значення = дефолтний шаблон.
	DiscordRPCDetails string `json:"discordRPCDetails,omitempty"`
	DiscordRPCState   string `json:"discordRPCState,omitempty"`

	// ── Пам'ять і кеш ──
	// CleanupAfterPackDelete — автоматично видаляти версії Minecraft, які
	// після видалення збірки більше не використовує жодна інша збірка
	// (versions + їх бібліотеки й асети). Увімкнено за замовчуванням.
	CleanupAfterPackDelete bool `json:"cleanupAfterPackDelete"`

	// ── CurseForge ──
	// CurseForgeAPIKey — ВЛАСНИЙ API-ключ користувача (введений у
	// налаштуваннях), який перекриває ключ, вшитий у збірку. Порожній =
	// використовувати вбудований/дефолтний ключ розробника. Дає змогу
	// користувачу працювати зі своїм лімітом запитів (CurseForge Core API
	// лімітує 50 запитів/10с на ключ).
	CurseForgeAPIKey string `json:"curseForgeApiKey"`
	// UseModrinthForBlocked — брати з Modrinth моди, які автор заборонив
	// качати стороннім клієнтам на CurseForge ("3rd party download
	// opt-out"). Той самий підхід, що FallbackMRBlockedMods у Prism
	// Launcher: мод шукається за sha1, тож файл побайтово ідентичний.
	// Вимкнено = такі моди позначаються помилкою при імпорті.
	UseModrinthForBlocked bool `json:"useModrinthForBlocked"`

	// ── Продуктивність лаунчера ──
	// HardwareAcceleration — GPU-прискорення WebView2-фронтенду
	// (вимикається `--disable-gpu` для сумісності зі старими драйверами
	// / слабкими iGPU). Зміна вимагає перезапуску лаунчера — застосовується
	// при наступному старті shell'а.
	HardwareAcceleration bool `json:"hardwareAcceleration"`
	// UseDedicatedGPU — запускати ГРУ на дискретній графіці на ноутбуках
	// з двома GPU (перемикач Windows "High performance"). Надає Java-процесу
	// GPU_PREFERENCE_HIGH_PERFORMANCE при CreateProcess — той самий механізм,
	// що кнопка "Висока продуктивність" у Windows Graphics settings.
	UseDedicatedGPU bool `json:"useDedicatedGPU"`
	// PerformanceMode — прибирає blur/backdrop-filter, тяжкі анімації й
	// фонові ефекти по всьому UI лаунчера (не грі!) для слабких машин.
	// На відміну від AnimationsEnabled (просто вимикає переходи), цей
	// режим ще й знімає backdrop-filter: blur() з модалок/топбарів —
	// найдорожчу для GPU композицію в Chromium/WebView2.
	PerformanceMode bool `json:"performanceMode"`
	// LowMemoryMode — «економний режим» для слабких машин і Linux.
	// На Linux він додатково: вимикає композитинг/dma-buf у WebKitGTK
	// (менше пам'яті під софтверні буфери й менше допоміжних процесів),
	// ставить вікнам WebviewGpuPolicyNever і опускає GOGC (менший пік
	// купи). Вимагає перезапуску лаунчера — значення потрапляє до shell
	// через ui.json (writeUIFlagsFile) ще до створення першого вікна.
	LowMemoryMode bool `json:"lowMemoryMode"`
	// DisableModSuggestions — ховає блок рекомендованих модів на вкладці
	// «Контент» інстансу (менше мережевих запитів + чистіший список для
	// тих, хто хоче керувати модами вручну).
	DisableModSuggestions bool `json:"disableModSuggestions"`
	// AllowAlphaBetaModUpdates — під час перевірки й встановлення оновлень
	// модів враховує alpha/beta версії, а не тільки release. Вимкнено за
	// замовчуванням: alpha/beta можуть ламати збірку без попередження.
	AllowAlphaBetaModUpdates bool `json:"allowAlphaBetaModUpdates"`

	// ── Вигляд: sidebar та кастомна тема ──
	// SidebarWidth — ширина сайдбара (220–400px). Зберігається у БД
	// (settings.json), а не в localStorage: переживає перезапуск лаунчера
	// і синхронізується між вікнами.
	SidebarWidth int `json:"sidebarWidth"`
	// SidebarScale — масштаб елементів сайдбара (0.85–1.25). Це число, а не
	// три фіксовані кнопки: повзунок у «Вигляд» дає будь-який проміжний
	// розмір, як просив користувач.
	SidebarScale float64 `json:"sidebarScale"`
	// SidebarOnlyPinned — показувати у sidebar лише закріплені збірки.
	SidebarOnlyPinned bool `json:"sidebarOnlyPinned"`
	// ── Кастомна тема (Налаштування → Вигляд → Тема = custom) ──
	// Кольори власної теми користувача: фон застосунку, картки/панелі,
	// основний текст. Застосовуються через CSS-змінні (applyTheme).
	ThemeCustomBg   string `json:"themeCustomBg"`
	ThemeCustomCard string `json:"themeCustomCard"`
	ThemeCustomText string `json:"themeCustomText"`

	// ── Порядок і закріплення збірок ──
	// PackOrder — список ID збірок у порядку, який встановив користувач.
	// Нові збірки (без запису тут) відображаються в кінці, як прийшли
	// з бекенду. Зберігається у settings.json, а не в localStorage.
	PackOrder []string `json:"packOrder,omitempty"`
	// PackPinned — список ID закріплених збірок у порядку закріплення.
	// Закріплені збірки ігноруються в PackOrder при відображенні і
	// показуються окремим блоком зверху у своєму порядку.
	PackPinned []string `json:"packPinned,omitempty"`
	// PackFolders — папки кастомних збірок (косметична організація
	// «Мої збірки»/sidebar). Збірка належить максимум одній папці;
	// збірки в папках приховані з головного списку і показуються
	// всередині папки. Порядок папок у масиві — порядок відображення;
	// порядок збірок у папці — packIds.
	PackFolders []PackFolder `json:"packFolders,omitempty"`
	// PinnedServerOrder — порядок закріплених серверів у секції
	// «Закріплені сервери» на «Моїх збірках» (ключі "packId|address",
	// той самий формат, що й ключ ітерації ListPinnedServers).
	// Користувач переставляє закріплені сервери drag&drop, як збірки;
	// без цього порядок був би прив'язаний до алфавітного порядку
	// packID і не піддавався ручному сортуванню.
	PinnedServerOrder []string `json:"pinnedServerOrder,omitempty"`

	// ── Друзі Xbox (розділ «Друзі» на віджеті акаунта) ──
	// XboxOnlineNotify — сповіщати тостом, коли друг стає онлайн.
	// Локальний перемикач лаунчера (не плутати з налаштуваннями
	// приватності Xbox — ті живуть на account.xbox.com).
	XboxOnlineNotify bool `json:"xboxOnlineNotify"`
	// McOnlineNotify — сповіщати тостом коли друг (нова система
	// Minecraft friends 1.21.6+) з'являється онлайн. Зберігається
	// локально у settings.json, бо сервер Mojang не тримає це поле.
	McOnlineNotify bool `json:"mcOnlineNotify"`

	// ── Міграція збірок з інших лаунчерів (Prism / Modrinth App) ──
	// MigrationPromptShown — вікно міграції вже показувалось користувачу
	// після входу (він відмовився або завершив міграцію). Після цього вікно
	// автоматично більше не спливає — віджет міграції завжди доступний у
	// Налаштуваннях. Це та сама «галочка в базі даних, що ми показали» з ТЗ.
	MigrationPromptShown bool `json:"migrationPromptShown,omitempty"`
}

// PackFolder — папка кастомних збірок у «Мої збірки»/sidebar.
type PackFolder struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
	// Icon — Tabler-іконка папки (напр. "ti-sword"); порожньо = стандартна ti-folder.
	Icon    string   `json:"icon,omitempty"`
	PackIds []string `json:"packIds"`
}

// ServerFolder — папка серверів у «Бібліотеці серверів» (аналог PackFolder).
// Сервер належить максимум одній папці; сервери в папках не показуються
// в головному списку, а лише всередині папки.
type ServerFolder struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Color     string   `json:"color,omitempty"`
	Icon      string   `json:"icon,omitempty"`
	ServerIds []string `json:"serverIds"`
}

// MinecraftDefaults — налаштування гри «за замовчуванням», які лаунчер
// застосовує до options.txt збірок (при скачуванні збірки Shaurma, імпорті
// mrpack/zip і перед першим запуском). Зберігається JSON-блобом у SQLite.
// Опції з цього конфігу — саме та підмножина options.txt, яку лаунчер
// бачить/редагує («видимі» опції); решту файла (моди тощо) не чіпаємо.
type MinecraftDefaults struct {
	Enabled     bool               `json:"enabled"`
	Language    string             `json:"language"` // "uk_ua", "en_us", ...
	GuiScale    int                `json:"guiScale"` // 0=auto, 1-N
	SoundLevels map[string]float64 `json:"soundLevels"`
	KeyBindings map[string]string  `json:"keyBindings"`

	Fullscreen  bool `json:"fullscreen"`
	EnableVsync bool `json:"enableVsync"`
	MaxFps      int  `json:"maxFps"`

	AutoJump         bool    `json:"autoJump"`
	MouseSensitivity float64 `json:"mouseSensitivity"` // 0.0-2.0 (0.5 = 100%)
	InvertYMouse     bool    `json:"invertYMouse"`

	Fov                         float64 `json:"fov"` // normalized 0.0=(70°) .. 1.0=(110°)
	DarkMojangStudiosBackground bool    `json:"darkMojangStudiosBackground"`
}

// DefaultMinecraftDefaults повертає значення «за замовчуванням» для
// налаштувань гри (як у старого лаунчера). Enabled=false — функцію вимкнено.
func DefaultMinecraftDefaults() MinecraftDefaults {
	return MinecraftDefaults{
		Enabled:                     false,
		Language:                    "uk_ua",
		GuiScale:                    0,
		Fullscreen:                  false,
		EnableVsync:                 true,
		MaxFps:                      120,
		AutoJump:                    false,
		MouseSensitivity:            0.5,
		InvertYMouse:                false,
		Fov:                         0.0,
		DarkMojangStudiosBackground: false,
		SoundLevels: map[string]float64{
			"master":  1.0,
			"music":   0.0,
			"record":  1.0,
			"weather": 1.0,
			"block":   1.0,
			"hostile": 1.0,
			"neutral": 1.0,
			"player":  1.0,
			"ambient": 1.0,
			"voice":   1.0,
		},
		KeyBindings: map[string]string{
			"key.attack":               "key.mouse.left",
			"key.use":                  "key.mouse.right",
			"key.forward":              "key.keyboard.w",
			"key.back":                 "key.keyboard.s",
			"key.left":                 "key.keyboard.a",
			"key.right":                "key.keyboard.d",
			"key.jump":                 "key.keyboard.space",
			"key.sneak":                "key.keyboard.left.shift",
			"key.sprint":               "key.keyboard.left.control",
			"key.inventory":            "key.keyboard.e",
			"key.swapOffhand":          "key.keyboard.f",
			"key.drop":                 "key.keyboard.q",
			"key.chat":                 "key.keyboard.t",
			"key.command":              "key.keyboard.slash",
			"key.playerlist":           "key.keyboard.tab",
			"key.pickItem":             "key.mouse.middle",
			"key.screenshot":           "key.keyboard.f2",
			"key.togglePerspective":    "key.keyboard.f5",
			"key.fullscreen":           "key.keyboard.f11",
			"key.spectatorOutlines":    "key.keyboard.unknown",
			"key.smoothCamera":         "key.keyboard.unknown",
			"key.advancements":         "key.keyboard.l",
			"key.saveToolbarActivator": "key.keyboard.c",
			"key.loadToolbarActivator": "key.keyboard.x",
			"key.hotbar.1":             "key.keyboard.1",
			"key.hotbar.2":             "key.keyboard.2",
			"key.hotbar.3":             "key.keyboard.3",
			"key.hotbar.4":             "key.keyboard.4",
			"key.hotbar.5":             "key.keyboard.5",
			"key.hotbar.6":             "key.keyboard.6",
			"key.hotbar.7":             "key.keyboard.7",
			"key.hotbar.8":             "key.keyboard.8",
			"key.hotbar.9":             "key.keyboard.9",
		},
	}
}

// NormalizeMinecraftDefaults доповнює відсутні поля дефолтами (старий/новий
// блоб у SQLite без якихось полів) — щоб жодне поле не лишалось нульовим.
func NormalizeMinecraftDefaults(d *MinecraftDefaults) {
	def := DefaultMinecraftDefaults()
	if d.Language == "" {
		d.Language = def.Language
	}
	if d.SoundLevels == nil {
		d.SoundLevels = def.SoundLevels
	} else {
		for k, v := range def.SoundLevels {
			if _, ok := d.SoundLevels[k]; !ok {
				d.SoundLevels[k] = v
			}
		}
	}
	if d.KeyBindings == nil {
		d.KeyBindings = def.KeyBindings
	} else {
		for k, v := range def.KeyBindings {
			if _, ok := d.KeyBindings[k]; !ok {
				d.KeyBindings[k] = v
			}
		}
	}
}

// PackOptionsView — знімок options.txt збірки для розділу «Гра» на сторінці
// збірки: чи файл взагалі існує і які «видимі» (відомі лаунчеру) опції в
// ньому стоять. Path — абсолютний шлях до файла (для показу користувачу).
type PackOptionsView struct {
	Exists  bool              `json:"exists"`
	Options map[string]string `json:"options"`
	Path    string            `json:"path"`
}

// LaunchWarnings — результат GetLaunchWarnings: чи потрібно показати
// RAM-попередження перед запуском збірки. Фронтенд викликає це ПЕРЕД
// LaunchInstance і, якщо ShowRecommended/ShowLowRam — показує модалку
// (з кнопками «Запустити»/«Скасувати», слайдером RAM і галочкою
// «не показувати знову для цієї збірки»).
type LaunchWarnings struct {
	PackID string `json:"packId"`
	// Ефективна RAM збірки: per-instance override або глобальна (МБ).
	EffectiveRAMMB int `json:"effectiveRamMb"`
	// Рекомендована RAM збірки з packs-index.json (0 = вимоги немає).
	RecommendedRAMMB int `json:"recommendedRamMb"`
	// Оперативна пам'ять, ВІЛЬНА ЗАРАЗ (МБ) — не загальний обсяг
	// встановленої RAM, а те, скільки реально можна виділити цієї миті
	// (див. SystemInfo.FreeRAMMB і коментар у GetLaunchWarnings).
	AvailableRAMMB int64 `json:"availableRamMb"`
	// don't-show-again прапорці для ЦІЄЇ збірки (InstanceConfig).
	RecommendedDismissed bool `json:"recommendedDismissed"`
	LowRamDismissed      bool `json:"lowRamDismissed"`
	// Глобальний вимикач попереджень (Settings.ShowRamWarnings).
	WarningsEnabled bool `json:"warningsEnabled"`
	// Підсумок (рахується на бекенді): чи показувати кожне попередження.
	ShowRecommended bool `json:"showRecommended"`
	ShowLowRam      bool `json:"showLowRam"`
}

// SystemInfo — зведення про систему для кроку "Продуктивність" майстра
// та сторінки налаштувань (обчислюється на бекенді).
type SystemInfo struct {
	CPUCount   int   `json:"cpuCount"`
	TotalRAMMB int64 `json:"totalRAMMB"`
	// FreeRAMMB — оперативна пам'ять, ВІЛЬНА ЗАРАЗ (а не загальний обсяг
	// встановленої RAM). Потрібна окремо від TotalRAMMB: користувач може
	// мати 16 ГБ фізичної пам'яті, але 14 з них уже зайняті іншими
	// програмами — попередження "мало вільної RAM" має звірятись саме з
	// цим числом, інакше воно не спрацює навіть коли вільної пам'яті
	// критично мало (див. GetLaunchWarnings).
	FreeRAMMB  int64 `json:"freeRAMMB"`
	FreeDiskMB int64 `json:"freeDiskMB"`
	// OS — «людська» назва ОС (напр. "Windows 11"), Arch — архітектура
	// (напр. "x64"), Platform — низькорівневий runtime.GOOS ("windows"),
	// з якого фронтенд будує "windows-x64" для вкладки «Про лаунчер».
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Platform string `json:"platform"`
}

// JavaStatus — стан вбудованої Java лаунчера (для кроку «Теки та Java»
// майстра та сторінки налаштувань). Installed=false означає, що Java ще
// не розкладена у теку лаунчера і її треба встановити (EnsureJava).
// Dir — ПАПКА Java (та, що показується користувачу в полі), ExePath —
// виконуваний файл усередині неї для запуску гри.
type JavaStatus struct {
	Installed bool   `json:"installed"`
	Dir       string `json:"dir"`
	ExePath   string `json:"exePath"`
	Version   string `json:"version"`
}

// SettingOption — один варіант вибору для налаштування типу select/accent/language.
type SettingOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
	// Desc — i18n-ключ короткого пояснення варіанта. Використовується
	// контролом типу "choice" (картки вибору): користувач бачить, що
	// саме робить кожен варіант, ще до натискання.
	Desc string `json:"desc,omitempty"`
	// Icon — клас tabler-іконки варіанта (для "choice").
	Icon string `json:"icon,omitempty"`
}

// SettingCond — умова показу налаштування: рядок видно лише коли
// значення settings[Key] входить до In (або НЕ входить, якщо Not).
// Дозволяє описувати залежні параметри («що робити після гри» лише коли
// вікно взагалі згортається) прямо в схемі, а не хардкодити у UI.
type SettingCond struct {
	Key string   `json:"key"`
	In  []string `json:"in"`
	Not bool     `json:"not,omitempty"`
}

// SettingDef — декларативний опис одного налаштування (архітектура
// налаштувань). Фронтенд отримує повний список через GetSettingsSchema()
// і малює контроли сам: щоб додати нове налаштування, достатньо додати
// один рядок у config.SettingsSchema() + ключі в i18n — решта (збереження
// в settings.json, відображення, дефолти) підхоплюється автоматично.
type SettingDef struct {
	Key         string          `json:"key"`
	Type        string          `json:"type"` // bool | string | int | color | select | choice | accent | language | ram
	Group       string          `json:"group"`
	Label       string          `json:"label"`
	Desc        string          `json:"desc"`
	Default     interface{}     `json:"default"`
	Options     []SettingOption `json:"options,omitempty"`
	Min         *int            `json:"min,omitempty"`
	Max         *int            `json:"max,omitempty"`
	Step        *int            `json:"step,omitempty"`
	Placeholder string          `json:"placeholder,omitempty"`
	Action      string          `json:"action,omitempty"`    // browse | detectJava — кнопка поруч із полем
	Multiline   bool            `json:"multiline,omitempty"` // багаторядкове поле (команди/env)
	// ShowIf — умови, за яких рядок видно (усі мають виконуватись).
	ShowIf []SettingCond `json:"showIf,omitempty"`
}

// FolderPaths — поточні шляхи тек лаунчера для вкладки «Теки та шляхи».
// BaseDir — папка даних лаунчера; Installations/Java/Cache/Logs — підтеки
// (кастомні значення з launcher-location.json або дефолти відносно BaseDir);
// Config — стабільна тека конфігурації (ніколи не змінюється).
type FolderPaths struct {
	BaseDir       string `json:"baseDir"`
	Installations string `json:"installations"`
	Java          string `json:"java"`
	Cache         string `json:"cache"`
	Logs          string `json:"logs"`
	Config        string `json:"config"`
	// Кастомні значення, збережені у launcher-location.json ("" = дефолт).
	InstallationsCustom string `json:"installationsCustom"`
	JavaCustom          string `json:"javaCustom"`
	CacheCustom         string `json:"cacheCustom"`
	LogsCustom          string `json:"logsCustom"`
	// DefaultBaseDir — системний дефолт папки даних (для кнопки
	// «За замовчуванням» на вкладці «Теки та шляхи»).
	DefaultBaseDir string `json:"defaultBaseDir"`
}

// StorageEntry — одна тека на вкладці «Пам'ять і кеш»: назва, шлях, розмір.
type StorageEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}

// StorageInfo — зведення зайнятого місця для вкладки «Пам'ять і кеш».
type StorageInfo struct {
	Entries    []StorageEntry `json:"entries"`
	TotalBytes int64          `json:"totalBytes"`
	FreeDiskMB int64          `json:"freeDiskMB"`
}

// StoragePackEntry — одна ВСТАНОВЛЕНА збірка у списку «Пам'ять і кеш»
// (для швидкого видалення зайвого місця): id, назва, розмір теки.
// IconURL/Color додатково — щоб рядок у списку виглядав як картка
// у sidebar (іконка + колір збірки), а не голий текст.
type StoragePackEntry struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Bytes   int64  `json:"bytes"`
	IconURL string `json:"iconUrl"`
	// Icon — пресет-іконка збірки (Tabler, напр. "ti-puzzle"); PNG (IconURL)
	// має пріоритет. Потрібно, щоб список у «Пам'ять і кеш» показував
	// пресет-іконку замість порожнього ti-package, коли PNG немає.
	Icon  string `json:"icon"`
	Color string `json:"color"`
}

// SetupProgress — чернетка майстра першого запуску (wizard). Пишеться на
// диск при КОЖНІЙ зміні кроку чи поля, а не лише в кінці — тому якщо
// користувач закриє лаунчер (або вкладку) на кроці 2, наступний запуск
// відновлює саме крок 2 з уже введеними даними, а не починає з кроку 1.
// Файл видаляється тільки після успішного CompleteSetup().
type SetupProgress struct {
	Step        int    `json:"step"`
	InstanceDir string `json:"instanceDir"`
	JavaPath    string `json:"javaPath"`
	Language    string `json:"language"`
	Accent      string `json:"accent"`
	MaxRAM      int    `json:"maxRAM"`
	JavaArgs    string `json:"javaArgs"`
}

type DownloadProgress struct {
	InstanceID string  `json:"instanceId"`
	FileName   string  `json:"fileName"`
	Downloaded int64   `json:"downloaded"`
	Total      int64   `json:"total"`
	Speed      float64 `json:"speed"`
	Percent    int     `json:"percent"`
}

// InstanceConfig — редаговані per-instance параметри збірки. Для збірок
// Шаурма НЕ можна міняти фон, лоадер чи версію гри (це фіксовано
// маніфестом збірки), тому цих полів тут навмисно немає — редагувати
// можна лише те, що нижче: RAM, аргументи JVM, вікно гри, команди,
// список закріплених серверів.
//
// Порожній рядок / нульове значення в *Override-полях означає
// "успадкувати з глобальних Settings" — так збірка одразу працює без
// збереженого файлу конфігурації, аж поки користувач щось не перевизначить.
type InstanceConfig struct {
	ID string `json:"id"`
	// Name — назва збірки. Якщо порожня — використовується ID.
	Name string `json:"name,omitempty"`

	// ── Метадані збірки (заповнюються при створенні/імпорті/міграції) ──
	MCVersion      string `json:"mcVersion,omitempty"`
	Loader         string `json:"loader,omitempty"` // vanilla|fabric|forge|neoforge|quilt
	LoaderVersion  string `json:"loaderVersion,omitempty"`
	Color          string `json:"color,omitempty"`          // hex-колір картки
	IconPath       string `json:"iconPath,omitempty"`       // відносний шлях до icon.png
	BackgroundPath string `json:"backgroundPath,omitempty"` // відносний шлях до background.png
	Source         string `json:"source,omitempty"`         // manual|import|modrinth|curseforge
	Description    string `json:"description,omitempty"`

	// PlaytimeSeconds — ігровий час у цій збірці (секунди).
	// Зберігається в per-instance JSON, а НЕ в глобальному файлі.
	PlaytimeSeconds int64 `json:"playtimeSeconds,omitempty"`

	MaxRAMOverride int `json:"maxRamOverride"`
	// MinRAMOverride — стартовий розмір купи (-Xms) для ЦІЄЇ збірки. 0 =
	// успадкувати правило "глобальний Xms = MaxRAM/2" (стара поведінка).
	// Якщо задано (>0) — використовується саме це значення замість
	// автоматичного ram/2, як окреме поле "Мінімальна RAM" у Prism/старому
	// лаунчері.
	MinRAMOverride   int    `json:"minRamOverride"`
	JavaArgsOverride string `json:"javaArgsOverride"`

	FullscreenOverride   *bool `json:"fullscreenOverride,omitempty"`
	WindowWidthOverride  int   `json:"windowWidthOverride"`
	WindowHeightOverride int   `json:"windowHeightOverride"`

	PreLaunchCommandOverride string `json:"preLaunchCommandOverride"`
	WrapperCommandOverride   string `json:"wrapperCommandOverride"`
	PostExitCommandOverride  string `json:"postExitCommandOverride"`
	EnvVarsOverride          string `json:"envVarsOverride"`
	// UseCustomCommands — явний прапорець «використовувати свої команди».
	// Без нього тогл у «Продуктивності» не міг би вимкнутись, коли ВСІ
	// глобальні команди порожні (порожні override = «успадкувати»), тож
	// «свої» значення неможливо було б увімкнути для збірки. false =
	// глобальні команди лаунчера.
	UseCustomCommands bool `json:"useCustomCommands"`

	UseSeparateJava  bool   `json:"useSeparateJava"`
	JavaPathOverride string `json:"javaPathOverride"`

	// AccountIDOverride — якщо задано, ЦЯ збірка завжди запускається під
	// вказаним акаунтом, ігноруючи активний акаунт лаунчера (аналог
	// "Override Default Account" у Prism). Порожній рядок = використовувати
	// активний акаунт лаунчера, як і раніше.
	AccountIDOverride string `json:"accountIdOverride"`

	// ── Автоприєднання (Auto-join) ──
	// Аналог "Enable Auto-join" у Prism: одразу після завантаження світу
	// гра або заходить в обраний одиночний світ, або одразу конектиться на
	// вказаний сервер. AutoJoinType: "world" | "server".
	AutoJoinEnabled bool   `json:"autoJoinEnabled"`
	AutoJoinType    string `json:"autoJoinType"`
	// AutoJoinWorld — назва теки світу в saves/ (для типу "world").
	AutoJoinWorld string `json:"autoJoinWorld"`
	// AutoJoinServer — адреса сервера host[:port] (для типу "server").
	AutoJoinServer string `json:"autoJoinServer"`

	// ── Скін збірки ──
	// SkinPresetID — ID пресета з Гардероба, який автоматично застосовується
	// на акаунт при запуску цієї збірки (фіча «Скін збірки», лише для
	// ліцензійних Microsoft-акаунтів). Порожній рядок = вимкнено.
	SkinPresetID string `json:"skinPresetId,omitempty"`
	// SkinRestoreOnExit — чи повертати ПОПЕРЕДНІЙ скін і плащ акаунта після
	// зупинки збірки (навіть якщо той вигляд не збережений як пресет).
	// nil = увімкнено (дефолт), false = лишити скін як змінився.
	SkinRestoreOnExit *bool `json:"skinRestoreOnExit,omitempty"`

	// PinnedServerIPs — адреси серверів (з servers.dat), закріплених
	// користувачем "нагорі" списку/для швидкого запуску. Порівнюється з
	// ServerEntry.IP; порядок цього списку значення не має (сортування —
	// за порядком у самому servers.dat, закріплені лише спливають угору).
	PinnedServerIPs []string `json:"pinnedServerIps,omitempty"`
	// OfficialServerIP — останній IP офіційного сервера збірки, який лаунчер
	// сам записав у servers.dat (режим «офіційний сервер завжди в списку»).
	// Потрібен, щоб при зміні IP на CDN знайти СВІЙ запис у списку гравця й
	// оновити його адресу, а не плодити дублікат.
	OfficialServerIP string `json:"officialServerIp,omitempty"`

	// TempServerIPs — адреси серверів, доданих у список ЗБІРКИ ТИМЧАСОВО:
	// гравець приєднався до P2P-кімнати (співхост/запрошення) і хоче бачити
	// її поруч зі своїми серверами. Такі записи лаунчер сам прибирає зі
	// servers.dat, коли доступу більше немає (кімнату закрито / оренду
	// відкликано) — саме тому їх треба відрізняти від звичайних серверів,
	// які гравець додав назавжди.
	TempServerIPs []string `json:"tempServerIps,omitempty"`

	// ── RAM-попередження (don't-show-again, ЛИШЕ для цієї збірки) ──
	// RecommendedRamDismissed — «не показувати знову» для попередження
	// «нижче рекомендованої RAM». LowRamDismissed — для попередження
	// «мало фізичної оперативки» (ТЗ: галочка діє лише для цієї збірки,
	// глобальний вимикач — Settings.ShowRamWarnings).
	RecommendedRamDismissed bool `json:"recommendedRamDismissed,omitempty"`
	LowRamDismissed         bool `json:"lowRamDismissed,omitempty"`

	// MinecraftDefaultsApplied — true, якщо дефолтні налаштування гри
	// (mcoptions.ApplyNow/ApplyBeforeLaunch) вже застосовувались до
	// options.txt ЦІЄЇ збірки хоч раз. РАНІШЕ цей прапорець жив ЯК РЯДОК
	// ВСЕРЕДИНІ САМОГО options.txt (маркер "shaurma_pack:true" у
	// internal/mcoptions) — службовий стан лаунчера лежав у файлі, яким
	// володіє гравець. Будь-яке місце коду, що ще раз викликало ApplyNow/
	// ApplyBeforeLaunch (напр. по недогляду при звичайному оновленні
	// збірки чи на кожному запуску гри), знову дописувало маркер, і файл
	// гравця повторно скидався до дефолтів — гравець бачив, що його
	// налаштування (fov, чутливість миші, бінди тощо) злітають щоразу
	// після оновлення чи навіть при запуску.
	//
	// Маркер у options.txt ПРИБРАНО ПОВНІСТЮ: mcoptions більше нічого не
	// пише і не читає в самому файлі гри для цієї мети. Це ЄДИНЕ і
	// достатнє джерело правди, зберігається в instance.json ЦІЄЇ збірки
	// (internal/store: <InstanceDir>/<packID>/instance.json — правильна
	// тека для shaurma- чи custom-збірки визначається автоматично,
	// resolveInstanceDir), РАЗОМ з рештою налаштувань цієї ж збірки, і
	// видаляється РАЗОМ з ними при видаленні збірки (DeletePack →
	// DeleteInstanceConfig), тож повторне встановлення коректно знову
	// вважається "першим запуском".
	MinecraftDefaultsApplied bool `json:"minecraftDefaultsApplied,omitempty"`

	// Deprecated: Servers — старий per-instance список серверів (до
	// переходу на servers.dat як єдине джерело правди). Лишено для
	// зворотної сумісності зі старими збереженими конфігами; новий UI
	// його більше не пише, лише читає при міграції (див.
	// migrateLegacyServers в app.go).
	Servers []InstanceServer `json:"servers,omitempty"`

	// PlaytimeSessions — журнал ігрових сесій ЦІЄЇ збірки (для аналітики
	// ігрового часу по гравцях, config.GetAnalytics). InstanceID окремо не
	// зберігається в кожному записі — контекст (id цього instance.json) уже
	// його визначає.
	//
	// РЕГРЕСІЯ (виправлено): раніше весь журнал сесій УСІХ збірок лежав
	// ОДНИМ спільним масивом у shaurma.json (store.globalData.
	// PlaytimeSessions) — усупереч принципу «дані ОДНІЄЇ збірки — лише в
	// ЇЇ instance.json». AddPlaytimeSession/GetAnalytics тепер пишуть і
	// читають по кожній збірці окремо (store.go:
	// appendPlaytimeSessionToInstance/loadAllPlaytimeSessions).
	PlaytimeSessions []PlaytimeSessionRecord `json:"playtimeSessions,omitempty"`
}

// PlaytimeSessionRecord — один запис ігрової сесії всередині
// instance.json (без InstanceID — визначається контекстом файлу).
type PlaytimeSessionRecord struct {
	AccountID   string    `json:"accountId,omitempty"`
	AccountName string    `json:"accountName,omitempty"`
	StartedAt   time.Time `json:"startedAt"`
	Seconds     int64     `json:"seconds"`
}

// InstanceServer — один сервер у списку "Сервери" на сторінці збірки.
// Джерело правди — servers.dat самої збірки (App.ListPackServers/
// App.AddPackServer/... у internal/minecraft/servers_dat.go): лаунчер
// читає і пише той самий файл, який редагує гра, тому список завжди
// синхронний з тим, що бачить гравець на екрані Multiplayer. Pinned —
// суто UI-стан лаунчера (закріплення нагорі/для швидкого запуску), він
// НЕ зберігається у самому servers.dat (гра про нього не знає), а живе
// окремо в InstanceConfig.PinnedServerIPs.
type InstanceServer struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
	// Icon — data URL іконки сервера (з servers.dat, якщо збережена, або
	// підтягнута з останнього пінгу).
	Icon     string `json:"icon,omitempty"`
	Pinned   bool   `json:"pinned"`
	Official bool   `json:"official"`
	// Temp — сервер додано ТИМЧАСОВО (P2P-кімната: співхост або запрошення).
	// UI має показати це візуально, а лаунчер — сам прибрати запис, коли
	// доступу більше немає (див. InstanceConfig.TempServerIPs).
	Temp bool `json:"temp,omitempty"`
	// Online/Motd/Players/MaxPlayers/Ping — заповнюються лише результатом
	// останнього App.PingServer на фронтенді (тут не зберігаються), поле
	// лишене для зручності передачі одним об'єктом при пакетному пінгу.
}

// PinnedServerView — закріплений сервер разом із інформацією про збірку,
// з якої він походить. Повертається App.ListPinnedServers для секції
// «Закріплені сервери» над картками збірок на сторінці «Мої збірки».
type PinnedServerView struct {
	PackID      string `json:"packId"`
	PackName    string `json:"packName"`
	PackKind    string `json:"packKind"`    // "shaurma" | "custom"
	PackColor   string `json:"packColor"`   // hex, для акцентів
	PackIconURL string `json:"packIconUrl"` // asset:// ... для іконки збірки
	// PackIcon — пресет-іконка (Tabler, напр. "ti-puzzle") збірки;
	// PackIconURL (PNG) має пріоритет у UI, коли є.
	PackIcon string `json:"packIcon,omitempty"`
	Address  string `json:"address"`
	Name     string `json:"name"`
	Icon     string `json:"icon,omitempty"` // data URL іконки сервера
}

// ModEntry — мод у папці модів збірки. Поле ID — modId з метаданих jar
// (fabric.mod.json / mods.toml), Name — людська назва, Version — версія,
// прочитана з jar (реальна, а не назва файлу). Решта полів заповнюється
// сканнером (sha1/іконка/автор/залежності) і перевіркою оновлень на
// Modrinth/CurseForge (hasUpdate/latestVersion/source/projectSlug).
// ContentFileEntry — встановлений файл контенту в теці збірки
// (mods/resourcepacks/shaderpacks/datapacks). Для немод-контенту це
// «сирий» список: ім'я файлу, увімкнений/вимкнений (.disabled-суфікс),
// розмір. Джерело (Modrinth/CurseForge проєкт) зіставляється окремо.
type ContentFileEntry struct {
	// FileName — ім'я файлу в теці (з .disabled, якщо вимкнений).
	FileName string `json:"fileName"`
	// Name — ім'я без .disabled та розширення (для показу).
	Name string `json:"name"`
	// Enabled — увімкнений (немає .disabled-суфікса).
	Enabled bool `json:"enabled"`
	// IsDir — датапаки можуть бути теками, а не лише .zip.
	IsDir bool `json:"isDir"`
	// FileSize — розмір файлу в байтах (0 для тек).
	FileSize int64 `json:"fileSize"`

	// ── Дані версії/оновлень (Modrinth/CurseForge, як у менеджері модів) ──
	// Заповнюються ScanContentFilesWithUpdates для ресурспаків/датапаків/
	// шейдерів. Для модів / тек / нерозпізнаних файлів порожні.
	Sha1          string `json:"sha1,omitempty"`
	ProjectID     string `json:"projectId,omitempty"`
	Slug          string `json:"slug,omitempty"`
	Source        string `json:"source,omitempty"` // modrinth | curseforge | ""
	Version       string `json:"version,omitempty"`
	LatestVersion string `json:"latestVersion,omitempty"`
	HasUpdate     bool   `json:"hasUpdate"`
	Author        string `json:"author,omitempty"`
	IconURL       string `json:"iconUrl,omitempty"`
}

type ModEntry struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	Loader    string `json:"loader"`
	Enabled   bool   `json:"enabled"`
	HasUpdate bool   `json:"hasUpdate"`
	Source    string `json:"source"`
	FileName  string `json:"fileName"`

	Author      string `json:"author,omitempty"`
	Description string `json:"description,omitempty"`
	IconURL     string `json:"iconUrl,omitempty"`  // віддалена іконка (Modrinth/CF)
	IconData    string `json:"iconData,omitempty"` // data URL іконки з jar (у списку для UI НЕ віддається — див. HasIcon/GetModIcons)
	// HasIcon — у jar є вбудована іконка. Сама іконка в списку модів НЕ
	// передається (base64 по кожному моду × кілька повних сканів за відкриття
	// сторінки роздувало пам'ять); UI довантажує її лише для видимих рядків
	// через GetModIcons.
	HasIcon        bool     `json:"hasIcon,omitempty"`
	FileSize       int64    `json:"fileSize"`
	InstalledDate  string   `json:"installedDate"` // RFC3339 час створення файлу
	ProjectSlug    string   `json:"projectSlug,omitempty"`
	ProjectID      string   `json:"projectId,omitempty"`
	McVersion      string   `json:"mcVersion,omitempty"`
	LatestVersion  string   `json:"latestVersion,omitempty"`
	LatestURL      string   `json:"latestUrl,omitempty"`
	DependsOn      []string `json:"dependsOn,omitempty"` // id залежностей з метаданих
	Provides       []string `json:"provides,omitempty"`  // додаткові mod-ID, що їх цей jar теж закриває (fabric.mod.json "provides")
	HasMissingDeps bool     `json:"hasMissingDeps"`
	MissingDeps    []string `json:"missingDeps,omitempty"`
	Sha1           string   `json:"sha1,omitempty"`
	// SourceKind/SourcePath/SourceMissing — мод походить із «єдиного
	// джерела» (internal/modsource): "file" (окремий jar) або "folder"
	// (уся тека модів іншої збірки). Такий мод не можна вимикати,
	// видаляти чи оновлювати вручну; SourceMissing — джерела зараз нема.
	SourceKind    string `json:"sourceKind,omitempty"`
	SourcePath    string `json:"sourcePath,omitempty"`
	SourceMissing bool   `json:"sourceMissing,omitempty"`
	Status        string `json:"status,omitempty"` // checking | done | error (перевірка оновлень)
	// IsDuplicate — той самий modId встановлено кількома файлами
	// одночасно (різні версії/копії одного мода). Рахується на бекенді
	// при скануванні (ScanModsFull), щоб список модів міг одразу
	// підсвітити такі рядки — раніше про дублікати можна було дізнатись
	// лише з окремої модалки CheckPreLaunchModIssues/FindDuplicateMods
	// перед запуском гри, а не безпосередньо в списку модів.
	IsDuplicate bool   `json:"isDuplicate"`
	Error       string `json:"error,omitempty"`

	// Incompatible — мод скомпільовано під ІНШИЙ лоадер (fabric/forge/
	// quilt/neoforge), ніж лоадер поточної збірки — визначається на
	// бекенді при скануванні (ScanModsFull) з метаданих jar. Такий мод
	// у грі просто не завантажиться, тож рядок у списку модів блимає.
	Incompatible       bool   `json:"incompatible"`
	IncompatibleReason string `json:"incompatibleReason,omitempty"`
	// ViaConnector — мод іншого лоадера (Fabric/Quilt), що працює в
	// Forge/NeoForge-збірці лише завдяки встановленому Sinytra Connector.
	ViaConnector bool `json:"viaConnector,omitempty"`
	// Loaders — ПОВНИЙ список лоадерів, під які jar реально придатний
	// (з усіх знайдених у ньому файлів метаданих: mods.toml/
	// neoforge.mods.toml/fabric.mod.json/quilt.mod.json/mcmod.info).
	// Мультизавантажувальні моди (типово Architectury-моди на кшталт
	// AmbientSounds) поширюють ОДИН jar, що містить одразу і Forge-,
	// і Fabric-метадані — Loader (однина) лишається для бейджа в UI,
	// а перевірка сумісності (attachLoaderCompat) звіряється саме з
	// цим повним списком, щоб такий мод не позначався несумісним
	// лише тому, що випадково "перший перевірений" формат метаданих
	// не збігся з лоадером збірки.
	Loaders []string `json:"loaders,omitempty"`
	// Environment — сторона, на якій працює мод: "client" | "server" |
	// "*" (обидві) | "" (невідомо, метадані лоадера цього не декларують
	// — типово Forge/NeoForge). Читається лише з fabric.mod.json/
	// quilt.mod.json поля "environment" (parseJar). Менеджер модів
	// сервера використовує це поле, щоб позначити клієнтські моди
	// ("client") як зайві на сервері — вони не даватимуть жодного
	// ефекту, встановлені в server-теку mods/.
	Environment string `json:"environment,omitempty"`
}

// ModVersionOption — варіант версії мода для модалки «Змінити версію».
type ModVersionOption struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	VersionNumber string `json:"versionNumber"`
	GameVersion   string `json:"gameVersion"`
	Loader        string `json:"loader"`
	URL           string `json:"url"`
	Filename      string `json:"filename"`
	Sha1          string `json:"sha1"`
	DatePublished string `json:"datePublished"`
	VersionType   string `json:"versionType,omitempty"` // release | beta | alpha
	Changelog     string `json:"changelog,omitempty"`
	// IsCurrent — ця версія збігається з тією, що зараз встановлена
	// (визначається на бекенді за sha1/іменем файлу, а не порівнянням
	// versionNumber — воно ненадійне: mods.toml і Modrinth/CurseForge
	// часто форматують номер версії по-різному, напр. "6.0.8" проти
	// "mc1.20.1-6.0.8", тож рядки не збігаються навіть для тієї самої
	// версії). Фронтенд має покладатись саме на цей прапорець.
	IsCurrent bool `json:"isCurrent"`
}

// ModDependency — знайдена залежність мода (для модалки встановлення).
type ModDependency struct {
	ModID       string `json:"modId"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	DownloadURL string `json:"downloadUrl"`
	Filename    string `json:"filename"`
	IconURL     string `json:"iconUrl"`
	Resolved    bool   `json:"resolved"`
	Error       string `json:"error,omitempty"`
}

// ModBackupEntry — запис резервної копії мода (mods-backup/manifest.json).
type ModBackupEntry struct {
	ID         string `json:"id"`
	ModID      string `json:"modId"`
	Name       string `json:"name"`
	Version    string `json:"version"`
	FileName   string `json:"fileName"`   // оригінальне ім'я файлу в mods/
	BackupFile string `json:"backupFile"` // шлях відносно mods-backup/
	Date       string `json:"date"`
	// SourceURL — сторінка мода (Modrinth/CurseForge) на момент створення
	// бек-апу. Сама стара версія ЗАВЖДИ зберігається копією у BackupFile;
	// це посилання — додаткова довідка «звідки мод» (якщо слага/id не було
	// відомо — порожнє, і єдиним джерелом відновлення лишається копія).
	SourceURL string `json:"sourceUrl,omitempty"`
}

// DuplicateModGroup — група дублікатів (однаковий sha1, різні файли).
type DuplicateModGroup struct {
	Sha1 string     `json:"sha1"`
	Mods []ModEntry `json:"mods"`
}

// ModUpdateResult — підсумок масового оновлення модів.
type ModUpdateResult struct {
	Updated  []string `json:"updated"`
	Failed   []string `json:"failed"`
	BackedUp bool     `json:"backedUp"`
}

// EmptyModUpdateResult повертає ненульовий результат щоб JSON не писав null
// у поля Updated/Failed (nil slice → null → JS .length crash).
func EmptyModUpdateResult() ModUpdateResult {
	return ModUpdateResult{Updated: []string{}, Failed: []string{}}
}

// ── Міграція модів між збірками ─────────────────────────────────────────
// Той самий UX, що був у старому лаунчері (ModMigrationScreen): вибираєш
// збірку-джерело і цільову збірку, запускаєш аналіз — лаунчер шукає
// сумісні версії кожного мода для цільової (MC-версія + лоадер) на
// Modrinth/CurseForge і будує звіт; після підтвердження качає моди в
// цільову збірку.

// ── Міграція збірок з інших лаунчерів (Prism / Modrinth App) ─────────
// Окрема система від міграції МОДІВ (нижче): тут переноситься ЦІЛА збірка
// з іншого лаунчера — файли, налаштування, іконка, ID/назва.
// Типи API (MigrateStatus, запит/результат імпорту) живуть у main
// (migrate_api.go) — вони посилаються на launchermigrate.Launcher, а model
// не може залежати від launchermigrate (цикл імпортів через custompack).

// MigrationPack — збірка для вибору як джерело/ціль міграції модів.
type MigrationPack struct {
	PackID    string `json:"packId"`
	Name      string `json:"name"`
	MCVersion string `json:"mcVersion"`
	Loader    string `json:"loader"`
	IconURL   string `json:"iconUrl"`
	// Icon — пресет-іконка (Tabler, напр. "ti-puzzle") кастомної збірки;
	// PNG (IconURL) має пріоритет, коли є. Shaurma-каталог пресет-іконок
	// не має (сервер віддає лише PNG), тому тут порожньо для них.
	Icon string `json:"icon,omitempty"`
	// Color — колір збірки (hex, як на картці у sidebar) — для селектора
	// міграції, щоб збірки виглядали однаково зі списком збірок.
	Color string `json:"color,omitempty"`
	// ModCount — кількість увімкнених .jar у теці модів (для підказки в
	// селекторі: "зі збірки X можна перенести N модів").
	ModCount int `json:"modCount"`
	// Kind — "server" для таргету-сервера (ID "srv:"/"srvp:", див.
	// mod_target.go); порожньо для збірки.
	Kind string `json:"kind,omitempty"`
}

// MigrationItem — один мод зі звіту аналізу міграції.
type MigrationItem struct {
	// SourceName/SourceFileName — мод у збірці-джерелі (з метаданих jar).
	SourceName     string `json:"sourceName"`
	SourceFileName string `json:"sourceFileName"`
	SourceSha1     string `json:"sourceSha1"`
	// SourceModID — modId з jar-метаданих джерела (fabric.mod.json/…).
	// Для видалення старих версій мода в цілі це надійніший ключ, ніж
	// ProjectID: ProjectID є лише після мережевого резолву, а modId
	// читається з диска одразу при скануванні.
	SourceModID string `json:"sourceModId"`

	// Status — вердикт аналізу: found (можна перенести) | not-found
	// (не знайдено сумісної версії) | already (вже встановлено такої ж
	// версії) | update (встановлено, але є новіша — буде оновлено).
	Status string `json:"status"`

	// Resolved* — що саме буде встановлено в цільову збірку.
	ProjectID        string `json:"projectId"`
	ProjectSlug      string `json:"projectSlug"`
	ResolvedName     string `json:"resolvedName"`
	ResolvedVersion  string `json:"resolvedVersion"`
	ResolvedFilename string `json:"resolvedFilename"`
	ResolvedURL      string `json:"resolvedUrl"`
	ResolvedSha1     string `json:"resolvedSha1"`
	Source           string `json:"source"` // modrinth | curseforge
	IconURL          string `json:"iconUrl"`
	// WillReplace — у цільовій збірці вже є файл з таким іменем/модом
	// (стара версія) — він буде замінений новою.
	WillReplace bool `json:"willReplace"`

	// LocalCopy — джерело і ціль мають однаковий loader+MC-версію, тому
	// мережевий резолв (Modrinth/CurseForge) не робився: jar переноситься
	// прямим копіюванням з диска джерела. Status при цьому все одно
	// "found" (щоб не міняти фронтенд-логіку фільтрації todo), але
	// ResolvedURL порожній — installMigratedMod бере файл із
	// SourceFileName у теці модів SourcePackID, а не якогось URL.
	// UI показує такі рядки з позначкою «без перевірки в мережі», щоб
	// користувач розумів: сумісність гарантована самим збігом
	// loader+версії джерела/цілі, а не тим, що мод знайдено на Modrinth.
	LocalCopy bool `json:"localCopy,omitempty"`
	// SourcePackID — ID збірки-джерела, звідки копіюється файл, коли
	// LocalCopy=true. Заповнюється при аналізі (AnalyzeModMigration),
	// зберігається на фронтенді і повертається назад разом з items у
	// MigrateMods/StartModMigration — так installMigratedMod може
	// знайти jar на диску без зміни сигнатури цих публічних методів.
	SourcePackID string `json:"sourcePackId,omitempty"`
	// Side — де працює мод (лише при портуванні на сервер, AnalyzeModPort):
	// "server" (потрібен серверу), "client" (лише клієнт — на сервер не
	// переноситься), "unknown" (не визначено — вирішує користувач).
	Side string `json:"side,omitempty"`
}

// ModMigrationReport — результат аналізу міграції (звіт для UI).
type ModMigrationReport struct {
	Items           []MigrationItem `json:"items"`
	Found           int             `json:"found"`
	NotFound        int             `json:"notFound"`
	Already         int             `json:"already"`
	NeedsUpdate     int             `json:"needsUpdate"`
	SourcePackName  string          `json:"sourcePackName"`
	TargetMCVersion string          `json:"targetMcVersion"`
	TargetLoader    string          `json:"targetLoader"`
	// Cancelled — аналіз перервано кнопкою «Скасувати»: items містить лише
	// встигнутий до скасування частковий результат.
	Cancelled bool `json:"cancelled"`
}

// ModMigrationResult — підсумок виконаної міграції модів.
type ModMigrationResult struct {
	Migrated []string `json:"migrated"`
	Failed   []string `json:"failed"`
	// Skipped — моди, пропущені через статус already (вже встановлені).
	Skipped int `json:"skipped"`
}

// ── Порівнювач модів двох збірок (CompareModPacks) ───────────────────────
// На відміну від односторонньої міграції (AnalyzeModMigration: джерело →
// ціль), порівнювач симетричний: обидві збірки видно одночасно, для
// кожного мода видно ОБИДВІ сторони окремо, і встановити/оновити можна
// в БУДЬ-ЯКУ з двох сторін незалежно (InstallCompareMod з явним targetPackId).
//
// Резолвер версій (sha1-batch, Modrinth/CurseForge fallback, локальне
// копіювання при однаковому loader+MC) перевикористовується з
// mods_migration.go — це та сама мережева інфраструктура, лише з
// розширеним три-станом сумісності (compatNote), якого одностороння
// міграція не потребувала: там "не знайшли версію для цілі" завжди
// вело до not-found, а тут важливо відрізнити "мода взагалі нема на
// маркетплейсі" від "мод є, але не випущений під loader/MC саме цієї
// сторони" — про це попереджав GGG: без цього розрізнення порівнювач
// показував би "не знайдено" там, де насправді "несумісно".

// ModComparePack — картка збірки в шапці порівнювача (PackCompareChip).
type ModComparePack struct {
	PackID    string `json:"packId"`
	Name      string `json:"name"`
	MCVersion string `json:"mcVersion"`
	Loader    string `json:"loader"`
	IconURL   string `json:"iconUrl,omitempty"`
	Icon      string `json:"icon,omitempty"`
	Color     string `json:"color,omitempty"`
}

// ModCompareSide — стан одного мода з ОДНОГО боку порівняння.
type ModCompareSide struct {
	// Status — present (мод стоїть тут) | outdated (стоїть, але на іншій
	// стороні новіша версія) | missing (мода тут немає).
	Status string `json:"status"`

	// ── Заповнюється, коли Status = present | outdated (файл РЕАЛЬНО
	// стоїть на диску цієї сторони) ──
	FileName string `json:"fileName,omitempty"`
	Version  string `json:"version,omitempty"`
	Enabled  bool   `json:"enabled,omitempty"`
	Sha1     string `json:"sha1,omitempty"`
	ModID    string `json:"modId,omitempty"`

	// ── Заповнюється, коли Status = missing | outdated (те, що МОЖНА
	// встановити з цієї сторони — резолв з мережі або локальна копія) ──
	// CanInstall — чи є що встановити (мод знайдено і сумісний з
	// loader/MC саме ЦІЄЇ сторони). false = compatNote пояснює чому.
	CanInstall bool `json:"canInstall,omitempty"`
	// CompatNote — чому не можна встановити, коли !CanInstall:
	// "noCompatVersion" (мод знайдено на маркетплейсі, але автор не
	// випустив версію під loader/MC цієї сторони) — порожньо, якщо
	// мода взагалі не знайдено на Modrinth/CurseForge (not-found).
	CompatNote string `json:"compatNote,omitempty"`
	// LocalCopy — обидві збірки на однаковому loader+MC: jar можна
	// скопіювати напряму з диска іншої сторони, без мережевого запиту.
	LocalCopy bool `json:"localCopy,omitempty"`
	// Source — modrinth | curseforge (порожньо для localCopy).
	Source          string `json:"source,omitempty"`
	ProjectID       string `json:"projectId,omitempty"`
	ProjectSlug     string `json:"projectSlug,omitempty"`
	ResolvedName    string `json:"resolvedName,omitempty"`
	ResolvedVersion string `json:"resolvedVersion,omitempty"`
	ResolvedFile    string `json:"resolvedFilename,omitempty"`
	ResolvedURL     string `json:"resolvedUrl,omitempty"`
	ResolvedSha1    string `json:"resolvedSha1,omitempty"`
	// SourcePackID — packId сторони, ЗВІДКИ копіюється файл, коли
	// LocalCopy=true (сама сторона в ModCompareSide не знає, з якою
	// збіркою вона порівнюється — це протилежна сторона рядка).
	// Потрібен InstallCompareMod, щоб знайти jar на диску напряму, без
	// перебору всіх встановлених збірок.
	SourcePackID   string `json:"sourcePackId,omitempty"`
	SourceFileName string `json:"sourceFileName,omitempty"`
}

// ModCompareRow — один мод у таблиці порівняння: та сама позиція зліва
// і справа (матчиться за modId з jar-метаданих, fallback — ProjectID/sha1
// коли modId порожній з одного боку).
type ModCompareRow struct {
	// Key — стабільний ключ рядка для UI (modId, або "sha1:"+sha1, або
	// "name:"+lowercase(name) як останній фолбек).
	Key         string         `json:"key"`
	Name        string         `json:"name"`
	Author      string         `json:"author,omitempty"`
	IconURL     string         `json:"iconUrl,omitempty"`
	Left        ModCompareSide `json:"left"`
	Right       ModCompareSide `json:"right"`
	SameVersion bool           `json:"sameVersion"`
}

// ModCompareReport — повний звіт порівняння двох збірок.
type ModCompareReport struct {
	Rows []ModCompareRow `json:"rows"`
	// BothSame/Diff/OnlyLeft/OnlyRight — підсумкові лічильники для чипів.
	BothSame  int `json:"bothSame"`
	Diff      int `json:"diff"`
	OnlyLeft  int `json:"onlyLeft"`
	OnlyRight int `json:"onlyRight"`
	// SameLoaderVersion — обидві збірки на однаковому loader+MC: усі
	// перенесення підуть прямим копіюванням jar, без мережі (банер у UI).
	SameLoaderVersion bool `json:"sameLoaderVersion"`
}

// CompareInstallRequest — запит на встановлення/оновлення ОДНОГО мода
// в ОДНУ конкретну сторону порівняння (кнопка на PackCompareCell).
type CompareInstallRequest struct {
	// TargetPackID — куди ставимо (packId тієї сторони, на кнопці якої
	// натиснули — може бути як лівою, так і правою збіркою).
	TargetPackID string `json:"targetPackId"`
	// Side — стан мода з боку, ЗВІДКИ беремо файл (протилежного від
	// TargetPackID) — містить Resolved*/LocalCopy/джерело для встановлення.
	Side ModCompareSide `json:"side"`
	// SourceName — назва мода (для тостів/помилок).
	SourceName string `json:"sourceName"`
	// Replace — true, якщо в цілі вже стоїть застаріла версія цього мода
	// (замінюємо з резервною копією), false — чистий "встановити новий".
	Replace bool `json:"replace"`
}

// PreLaunchModIssues — результат перевірки збірки перед запуском гри
// (дублікати + відсутні залежності) — модалка зі швидкими діями.
type PreLaunchModIssues struct {
	HasIssues   bool                `json:"hasIssues"`
	Duplicates  []DuplicateModGroup `json:"duplicates"`
	MissingDeps []string            `json:"missingDeps"`
	BlockedMods []string            `json:"blockedMods"`
}

// SuggestedMod — рекомендований мод на вкладці «Контент» збірки: мод, від
// якого залежить якийсь з уже встановлених модів (жорстка/hard-залежність
// з fabric.mod.json тощо), але сам ще не встановлений. На відміну від
// "схожих модів" (яких у Modrinth API немає штатно), це рекомендація на
// основі РЕАЛЬНИХ даних про збірку користувача, а не вгадування —
// resolved через Modrinth search за depID (SearchMods + pickBestMatch).
type SuggestedMod struct {
	// DepID — технічний ідентифікатор з fabric.mod.json/quilt.mod.json
	// залежного мода (не завжди збігається з slug/назвою на Modrinth).
	DepID     string `json:"depId"`
	ProjectID string `json:"projectId"`
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	IconURL   string `json:"iconUrl"`
	// RequiredBy — назви вже встановлених модів, які цього потребують
	// (напр. ["Create", "Farmer's Delight"] обидва вимагають Fabric API).
	RequiredBy []string `json:"requiredBy"`
}

type BrowserEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Version     string `json:"version"`
	Loader      string `json:"loader"`
	MCVersion   string `json:"mcVersion"`
	Description string `json:"description"`
	Downloads   int64  `json:"downloads"`
	Followers   int64  `json:"followers"`
	Likes       int64  `json:"likes"`
	IconURL     string `json:"iconUrl"`
	// BannerURL — широке зображення проєкту (Modrinth featured_gallery /
	// CurseForge screenshots[0]) для режиму «Сітка» у браузері контенту.
	// Порожньо, якщо в проєкту немає галереї — UI малює градієнт.
	BannerURL string `json:"bannerUrl,omitempty"`
	Source    string `json:"source"`
	// ProjectID — числовий/hex ID проєкту (для встановлення потрібен
	// саме він, а не slug: /project/{id}/version приймає обидва, але
	// Modrinth-посилання на завантаження прив'язані до project_id).
	ProjectID string `json:"projectId"`
	// Author — автор проєкту (для картки результату, як у веб-апки).
	Author string `json:"author"`
	// Categories — категорії/теги проєкту (для бейджів на картці).
	Categories []string `json:"categories"`
	// Loaders — лоадери, під які є версії (fabric/forge/...).
	Loaders []string `json:"loaders"`
	// DateCreated/DateModified — для сортування "новіші/оновлені".
	DateCreated  string `json:"dateCreated"`
	DateModified string `json:"dateModified"`
	// CompatibilityUnconfirmed — true у двох випадках:
	//  1) перевірка сумісності з (mcVersion, loader) активної збірки ЩЕ
	//     ТРИВАЄ у фоні (filterCompatibleEntriesAsync) — результат пошуку
	//     віддається одразу з усіма хітами позначеними так, а підтвердження
	//     «дотягується» подіями "browser:compat" (див. ContentBrowser.svelte);
	//  2) перевірка завершилась, але НЕ змогла підтвердити сумісність через
	//     мережеву помилку запиту — елемент навмисно НЕ викинуто (fail-open:
	//     не можемо підтвердити несумісність — не ховаємо), але й не
	//     підтверджено сумісним.
	// UI показує це як попередження/індикатор "перевіряється" на кнопці
	// "Встановити" замість звичайної активної кнопки: раніше такий елемент
	// виглядав ідентично гарантовано сумісному, і про реальну (не)сумісність
	// користувач дізнавався лише ПІСЛЯ кліку, з тексту помилки
	// InstallBrowserContentVersion.
	CompatibilityUnconfirmed bool `json:"compatibilityUnconfirmed,omitempty"`
	// CompatibilityChecking — true, поки фонова перевірка сумісності ще
	// ТРИВАЄ (випадок 1 вище). Окремий від CompatibilityUnconfirmed
	// прапорець: раніше обидва стани («ще перевіряємо» і «перевірка
	// не вдалась») були одним булевим полем, і кнопка «Встановити» на
	// КОЖНІЙ картці спершу показувала «Не підтверджено», хоча жодної
	// помилки не було — просто перевірка ще не дійшла до цього хіта.
	// UI показує для Checking нейтральну кнопку «Встановити» (без
	// попередження й без додаткового діалогу), а «Не підтверджено» —
	// лише коли перевірка справді завершилась мережевою помилкою.
	CompatibilityChecking bool `json:"compatibilityChecking,omitempty"`
}

type SkinEntry struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Variant string `json:"variant"`
	IsLocal bool   `json:"isLocal"`
}

// ── Аналітика ігрового часу (розділ «Аналітика» у «Моїх збірках») ─────

// AnalyticsPoint — один стовпчик діаграми: початок бакета (ISO "2006-01-02"
// для днів/тижнів/місяців, "2006" для років) і секунди гри в ньому.
// Фронтенд сам локалізує підпис бакета (день тижня, коротка назва місяця
// тощо) через Intl.DateTimeFormat — бекенд віддає лише сиру дату.
type AnalyticsPoint struct {
	Start   string `json:"start"`
	Seconds int64  `json:"seconds"`
}

// AnalyticsInstance — агрегат по ОДНІЙ збірці для топу «найбільше грав».
// Назва/іконка/колір додаються на фронтенді з GetBuilds.
type AnalyticsInstance struct {
	InstanceID string `json:"instanceId"`
	Seconds    int64  `json:"seconds"`
	Sessions   int64  `json:"sessions"`
}

// AnalyticsSummary — повний знімок аналітики ігрового часу для розділу
// «Аналітика»: підсумки + три серії діаграм (тижні/місяці/роки) + топ збірок.
// Всі серії приходять одним викликом — дані маленькі, а перемикання
// вкладок на фронтенді тоді миттєве (без повторних запитів).
type AnalyticsSummary struct {
	// TotalSeconds — усього секунд гри (усі сесії).
	TotalSeconds int64 `json:"totalSeconds"`
	// TotalSessions — кількість завершених ігрових сесій.
	TotalSessions int64 `json:"totalSessions"`
	// PlayedInstances — скільки РІЗНИХ збірок мають хоча б 1 секунду гри.
	PlayedInstances int64 `json:"playedInstances"`
	// AvgSessionSeconds — середня тривалість однієї сесії (0, якщо сесій нема).
	AvgSessionSeconds int64 `json:"avgSessionSeconds"`
	// Last7Days — 7 стовпчиків: останні 7 календарних днів, найстаріший перший.
	Last7Days []AnalyticsPoint `json:"last7Days"`
	// Weeks — 12 тижнів (ISO, з понеділка), поточний неповний тиждень останній.
	Weeks []AnalyticsPoint `json:"weeks"`
	// Months — 12 календарних місяців, поточний місяць останній.
	Months []AnalyticsPoint `json:"months"`
	// Years — 8 календарних років, поточний рік останній.
	Years []AnalyticsPoint `json:"years"`
	// TopInstances — топ збірок за сумарним часом гри, сортовано спаданням.
	TopInstances []AnalyticsInstance `json:"topInstances"`
}
