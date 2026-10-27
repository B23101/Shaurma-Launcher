package config

import (
	"shaurma-launcher-backend/internal/model"
	"shaurma-launcher-backend/internal/winbehavior"
)

func intPtr(v int) *int { return &v }

// notifyDurationRange — діапазон часу показу картки сповіщення (секунди).
// ЄДИНЕ джерело правди для меж: використовується і тут (Min/Max рядка
// схеми), і в normalize() при стисненні збереженого значення — інакше UI і
// нормалізація розійшлися б, і користувач отримував би число, яке схема
// «валідною» не вважає.
var notifyDurationRange = struct{ min, max int }{min: 3, max: 60}

// SettingsSchema — архітектура налаштувань лаунчера. Це ЄДИНЕ місце, де
// описується, які налаштування існують: ключ (поле Settings), тип
// контрола, група на сторінці «Налаштування», i18n-ключі підпису й
// опису, дефолтне значення, варіанти/межі.
//
// Фронтенд робить GetSettingsSchema() і РЕНДЕРИТЬ налаштування сам —
// щоб додати новий параметр, достатньо додати один рядок сюди та ключі
// в i18n. Нічого дублювати вручну в UI не треба (саме цього не вистачало
// старому лаунчеру: параметри існували, але не використовувалися).
func SettingsSchema() []model.SettingDef {
	return []model.SettingDef{
		// ── Загальні ──
		{Key: "autoUpdate", Type: "bool", Group: "general", Label: "settings.autoUpdate", Desc: "settings.autoUpdateDesc", Default: true},
		{Key: "updateChannel", Type: "select", Group: "general", Label: "settings.updateChannel", Desc: "settings.updateChannelDesc", Default: "stable",
			Options: []model.SettingOption{
				{Value: "stable", Label: "settings.channel.stable"},
				{Value: "beta", Label: "settings.channel.beta"},
			}},
		// ПРИМІТКА: поведінка вікна лаунчера (запуск із системою, кнопки ×/–,
		// вікно під час гри) раніше була трьома незв'язаними булами
		// (openOnStartup/closeOnLaunch/exitOnGameClose) + «привидом»
		// hideOnGameOpen. Тепер це окремий розділ «Запуск і вікно» (групи
		// startup / windowClose / windowGame нижче): один параметр на етап.
		{Key: "saveLogs", Type: "bool", Group: "general", Label: "settings.saveLogs", Desc: "settings.saveLogsDesc", Default: true},
		// Одразу після створення/імпорту збірки завантажити всі необхідні
		// компоненти (Java, лоадер, клієнт, бібліотеки, асети) у фоні —
		// перший запуск стає миттєвим (як Prism). УВІМКНЕНО за замовчуванням.
		{Key: "autoDownloadAfterCreate", Type: "bool", Group: "general", Label: "settings.autoDownloadAfterCreate", Desc: "settings.autoDownloadAfterCreateDesc", Default: true},
		// Відправка журналу помилок на сервер (worker POST /errors) для
		// діагностики. ВИМКНЕНО ЗА ЗАМОВЧУВАННЯМ: вмикається лише явною
		// згодою користувача (дехто не хоче, щоб його помилки передавались
		// назовні). Опис попереджає, ЩО саме надсилається і навіщо.
		// ── Вимкнення підсистем ──
		// Скіни офлайн-акаунтів (authlib-injector + Yggdrasil-API на worker'і)
		// та P2P («LAN через інтернет», Headscale/tsnet). Обидва параметри
		// ІНВЕРТОВАНІ (disable*): Default: false означає «увімкнено», бо в
		// старому settings.json цих полів немає і false = увімкнено.
		{Key: "disableSkins", Type: "bool", Group: "general", Label: "settings.disableSkins", Desc: "settings.disableSkinsDesc", Default: false},
		{Key: "disableMojangSkinFallback", Type: "bool", Group: "general", Label: "settings.disableMojangSkinFallback", Desc: "settings.disableMojangSkinFallbackDesc", Default: false},
		{Key: "disableP2P", Type: "bool", Group: "general", Label: "settings.disableP2P", Desc: "settings.disableP2PDesc", Default: false},
		// Згода на анонімну статистику використання (opt-in!). Default: false —
		// жодних мережевих запитів, поки користувач сам не увімкне. Питання
		// ставиться модалкою при першому запуску (App.svelte), після чого
		// факт «питали» зберігається в meta shaurma.json, а не тут.
		{Key: "analyticsEnabled", Type: "bool", Group: "general", Label: "settings.analyticsEnabled", Desc: "settings.analyticsEnabledDesc", Default: false},

		// ── Вигляд ──
		{Key: "language", Type: "language", Group: "appearance", Label: "settings.language", Desc: "settings.languageDesc", Default: "uk",
			Options: []model.SettingOption{
				{Value: "uk", Label: "language.uk"},
				{Value: "en", Label: "language.en"},
				{Value: "auto", Label: "language.auto"},
			}},
		{Key: "accent", Type: "accent", Group: "appearance", Label: "settings.accent", Desc: "settings.accentDesc", Default: "orange",
			Options: []model.SettingOption{
				{Value: "orange", Label: "accent.orange"},
				{Value: "violet", Label: "accent.violet"},
				{Value: "green", Label: "accent.green"},
				{Value: "blue", Label: "accent.blue"},
				{Value: "red", Label: "accent.red"},
				// Градієнтні акценти: акцент = ДВІ точки кольору (--orange i
				// --orange-2), тож surface-елементи з linear-gradient(135deg…)
				// показують видимий двоколірний градієнт замість гладкого тону.
				{Value: "sunset", Label: "accent.sunset"},
				{Value: "ocean", Label: "accent.ocean"},
				{Value: "aurora", Label: "accent.aurora"},
				{Value: "custom", Label: "accent.custom"},
			}},
		// Кастомний акцент: колір(и), які користувач обрав у пікері. Показується
		// на вкладці «Вигляд» лише коли accent === "custom".
		{Key: "accentCustom", Type: "color", Group: "appearance", Label: "settings.accentCustom", Desc: "settings.accentCustomDesc", Default: "#ff8a00", Multiline: false},
		// accentCustom2/accentCustomMode рендеряться разом з accentCustom
		// у спеціальному accent-custom-picker блоці (SettingRow.svelte),
		// в схему додані лише для того, щоб мати дефолти/дескрипшени й
		// потрапити в keys-diff нормалізації — окремого рядка в UI нема.
		{Key: "accentCustom2", Type: "color", Group: "appearance", Label: "settings.accentCustom2", Desc: "settings.accentCustom2Desc", Default: "#ff5e00", Multiline: false},
		{Key: "accentCustomMode", Type: "select", Group: "appearance", Label: "settings.accentCustomMode", Desc: "settings.accentCustomModeDesc", Default: "flat",
			Options: []model.SettingOption{
				{Value: "flat", Label: "settings.accentCustomMode.flat"},
				{Value: "gradient", Label: "settings.accentCustomMode.gradient"},
			}},
		{Key: "theme", Type: "select", Group: "appearance", Label: "settings.theme", Desc: "settings.themeDesc", Default: "dark",
			Options: []model.SettingOption{
				// system — слідує за темою ОС (prefers-color-scheme), див.
				// applyTheme у theme.svelte.ts.
				{Value: "system", Label: "settings.theme.system"},
				{Value: "dark", Label: "settings.theme.dark"},
				{Value: "light", Label: "settings.theme.light"},
				// OLED: справжній чистий чорний (#000) для OLED-дисплеїв —
				// батарея + максимально глибокий контраст.
				{Value: "oled", Label: "settings.theme.oled"},
				// Власна тема: користувач задає колір фону, карток і тексту
				// своїми кольорами (поля themeCustomBg/Card/Text нижче).
				{Value: "custom", Label: "settings.theme.custom"},
			}},
		// Кольори власної теми: показуються лише коли theme === "custom"
		// (той самий принцип, що accentCustom при accent === "custom").
		{Key: "themeCustomBg", Type: "color", Group: "appearance", Label: "settings.themeCustomBg", Desc: "settings.themeCustomBgDesc", Default: "#0f0f0f", Multiline: false},
		{Key: "themeCustomCard", Type: "color", Group: "appearance", Label: "settings.themeCustomCard", Desc: "settings.themeCustomCardDesc", Default: "#161616", Multiline: false},
		{Key: "themeCustomText", Type: "color", Group: "appearance", Label: "settings.themeCustomText", Desc: "settings.themeCustomTextDesc", Default: "#ffffff", Multiline: false},
		// Вимикач декоративних UI-анімацій (поява карток у sidebar,
		// pop/деактивація пунктів меню, перехід між вкладками налаштувань
		// тощо). За замовчуванням true; вимикається в «Вигляд» для тих, кому
		// анімації заважають (data-animations="off" обнуляє всі keyframes).
		// Раніше ключ існував лише в Settings/дефолтах, але НЕ в схемі —
		// перемикач не рендерився у «Вигляді» (картка «Анімації» була
		// закоментована/не підключена).
		{Key: "animationsEnabled", Type: "bool", Group: "appearance", Label: "settings.animationsEnabled", Desc: "settings.animationsEnabledDesc", Default: true},
		// Анімувати іконки збірок у sidebar ЗАВЖДИ (не лише для вибраної
		// збірки). Слабким ПК краще лишати вимкненим — GIF-іконки у sidebar
		// грають лише для активної збірки, решта статичні.
		{Key: "sidebarAnimAlways", Type: "bool", Group: "appearance", Label: "settings.sidebarAnimAlways", Desc: "settings.sidebarAnimAlwaysDesc", Default: false},
		// Компактний вигляд закріплених серверів (лише іконки в один ряд) —
		// керується з Налаштування → Вигляд, а не з розділу «Мої збірки»
		// (скарга користувача: перемикач був у шапці секції закріплених
		// серверів, хоча це налаштування вигляду).
		{Key: "pinnedServersCompact", Type: "bool", Group: "appearance", Label: "settings.pinnedServersCompact", Desc: "settings.pinnedServersCompactDesc", Default: false},
		// Сторінка збірки: банер на всю ширину сторінки (замість звичайного
		// банеру в колонці контенту). За замовчуванням false — поточний
		// компактний вигляд, перемикач у «Вигляді» для тих, хто хоче
		// повноширинний геро-банер.
		{Key: "packBannerFull", Type: "bool", Group: "appearance", Label: "settings.packBannerFull", Desc: "settings.packBannerFullDesc", Default: false},
		{Key: "font", Type: "select", Group: "appearance", Label: "settings.font", Desc: "settings.fontDesc", Default: "inter",
			Options: []model.SettingOption{
				{Value: "inter", Label: "settings.font.inter"},
				{Value: "system", Label: "settings.font.system"},
				{Value: "jetbrains", Label: "settings.font.jetbrains"},
				// Більше системних шрифтів (без додаткових файлів — це готові
				// шрифти Windows): користувачу є з чого вибрати.
				{Value: "segoe", Label: "settings.font.segoe"},
				{Value: "roboto", Label: "settings.font.roboto"},
				{Value: "consolas", Label: "settings.font.consolas"},
				{Value: "verdana", Label: "settings.font.verdana"},
				{Value: "georgia", Label: "settings.font.georgia"},
				{Value: "custom", Label: "settings.font.custom"},
			}},
		{Key: "fontPath", Type: "string", Group: "appearance", Label: "settings.fontPath", Desc: "settings.fontPathDesc", Default: "", Action: "fontFile", Placeholder: "C:\\...\\MyFont.ttf"},

		// ── Теки та шляхи ──
		// Вкладка «Теки та шляхи» має СПЕЦІАЛЬНИЙ рендеринг у фронтенді (як
		// «Пам'ять і кеш» / «Про лаунчер»): кожна тека (папка даних, збірки,
		// Java, кеш, логи, конфігурація) показується окремим рядком з кнопкою
		// «Огляд» та «За замовчуванням». Це як у старого лаунчера — всі теки
		// лаунчера видно і можна змінити. Шляхи беруться з App.GetFolderPaths(),
		// змінюються через App.SetFolderPath(kind, path).

		// ── CurseForge ──
		// Власний API-ключ користувача (перекриває вбудований ключ
		// розробника) + fallback на Modrinth для модів, заблокованих
		// автором на CurseForge ("3rd party download opt-out").
		{Key: "curseForgeApiKey", Type: "string", Group: "curseforge", Label: "settings.curseForgeApiKey", Desc: "settings.curseForgeApiKeyDesc", Default: "", Placeholder: "settings.curseForgeApiKeyPlaceholder", Multiline: false},
		{Key: "useModrinthForBlocked", Type: "bool", Group: "curseforge", Label: "settings.useModrinthForBlocked", Desc: "settings.useModrinthForBlockedDesc", Default: true},
		// disableModSuggestions — ховає блок рекомендованих модів на
		// вкладці «Контент» інстансу.
		{Key: "disableModSuggestions", Type: "bool", Group: "curseforge", Label: "settings.disableModSuggestions", Desc: "settings.disableModSuggestionsDesc", Default: false},
		// allowAlphaBetaModUpdates — при перевірці й встановленні
		// оновлень модів враховує alpha/beta версії, а не тільки release.
		{Key: "allowAlphaBetaModUpdates", Type: "bool", Group: "curseforge", Label: "settings.allowAlphaBetaModUpdates", Desc: "settings.allowAlphaBetaModUpdatesDesc", Default: false},

		// ── Стандарти Java (налаштування ГРИ за замовч.) ──
		// Ці параметри стосуються запуску Minecraft, а не лаунчера.
		// RAM, JVM-аргументи, дискретна GPU — все що впливає на процес гри.
		{Key: "maxRAM", Type: "ram", Group: "javaDefaults", Label: "settings.ram", Desc: "settings.ramDesc", Default: 4096, Min: intPtr(1024), Max: intPtr(16384), Step: intPtr(512)},
		{Key: "javaArgs", Type: "jvmargs", Group: "javaDefaults", Label: "settings.javaArgs", Desc: "settings.javaArgsDesc", Default: ""},
		// useDedicatedGPU — запуск гри на дискретній GPU ноутбука.
		// Стосується процесу гри (GPU_PREFERENCE env), не лаунчера.
		{Key: "useDedicatedGPU", Type: "bool", Group: "javaDefaults", Label: "settings.useDedicatedGPU", Desc: "settings.useDedicatedGPUDesc", Default: true},

		// ── Продуктивність лаунчера ──
		// Тільки параметри що впливають на сам лаунчер (не на гру).
		{Key: "performanceMode", Type: "bool", Group: "performance", Label: "settings.performanceMode", Desc: "settings.performanceModeDesc", Default: false},
		// GPU-прискорення WebView2-фронтенду (потребує рестарт лаунчера).
		{Key: "hardwareAcceleration", Type: "bool", Group: "performance", Label: "settings.hardwareAcceleration", Desc: "settings.hardwareAccelerationDesc", Default: true},
		// «Економний режим» — менша пам’ять лаунчера (Linux: WebKit без
		// композитингу/dma-buf + WebviewGpuPolicyNever + GOGC=50; тримання
		// RSS у треї/під час гри прибиральником). Потребує рестарту.
		{Key: "lowMemoryMode", Type: "bool", Group: "performance", Label: "settings.lowMemoryMode", Desc: "settings.lowMemoryModeDesc", Default: false},

		// ── Консоль ──
		// Ліміт буфера — рядками; розміру консолі НЕМАЄ (розтягується по
		// вікну); режиму консолі НЕМАЄ (є окремі перемикачі showConsoleOn*).
		{Key: "consoleMaxLines", Type: "int", Group: "console", Label: "settings.consoleMaxLines", Desc: "settings.consoleMaxLinesDesc", Default: 10000, Min: intPtr(100), Max: intPtr(50000), Step: intPtr(100)},
		{Key: "consoleInfoColor", Type: "color", Group: "console", Label: "settings.consoleInfoColor", Desc: "settings.consoleInfoColorDesc", Default: "#8fd3ff", Multiline: false},
		{Key: "consoleWarnColor", Type: "color", Group: "console", Label: "settings.consoleWarnColor", Desc: "settings.consoleWarnColorDesc", Default: "#ffd166", Multiline: false},
		{Key: "consoleErrorColor", Type: "color", Group: "console", Label: "settings.consoleErrorColor", Desc: "settings.consoleErrorColorDesc", Default: "#ff6b6b", Multiline: false},
		{Key: "consoleWrap", Type: "bool", Group: "console", Label: "settings.consoleWrap", Desc: "settings.consoleWrapDesc", Default: false},
		{Key: "consoleFontSize", Type: "int", Group: "console", Label: "settings.consoleFontSize", Desc: "settings.consoleFontSizeDesc", Default: 12, Min: intPtr(9), Max: intPtr(24), Step: intPtr(1)},
		{Key: "consoleFontFamily", Type: "select", Group: "console", Label: "settings.consoleFontFamily", Desc: "settings.consoleFontFamilyDesc", Default: "mono",
			Options: []model.SettingOption{
				{Value: "mono", Label: "settings.consoleFont.mono"},
				{Value: "sans", Label: "settings.consoleFont.sans"},
				{Value: "custom", Label: "settings.consoleFont.custom"},
			}},
		// Шлях до власного TTF/OTF шрифту консолі (поле показується лише коли
		// обрано "custom" у consoleFontFamily або будь-якому perType-шрифті).
		{Key: "consoleFontPath", Type: "string", Group: "console", Label: "settings.consoleFontPath", Desc: "settings.consoleFontPathDesc", Default: "", Action: "fontFile", Placeholder: "C:\\...\\MyMono.ttf"},
		// Режим шрифту консолі: один для всіх типів або окремий для кожного.
		// Поля consoleInfoFont/consoleWarnFont/consoleErrorFont (нижче) на
		// фронтенді показуються лише коли обрано "perType".
		{Key: "consoleFontMode", Type: "select", Group: "console", Label: "settings.consoleFontMode", Desc: "settings.consoleFontModeDesc", Default: "all",
			Options: []model.SettingOption{
				{Value: "all", Label: "settings.consoleFontMode.all"},
				{Value: "perType", Label: "settings.consoleFontMode.perType"},
			}},
		{Key: "consoleInfoFont", Type: "select", Group: "console", Label: "settings.consoleInfoFont", Desc: "settings.consoleInfoFontDesc", Default: "mono",
			Options: []model.SettingOption{
				{Value: "mono", Label: "settings.consoleFont.mono"},
				{Value: "sans", Label: "settings.consoleFont.sans"},
				{Value: "custom", Label: "settings.consoleFont.custom"},
			}},
		{Key: "consoleWarnFont", Type: "select", Group: "console", Label: "settings.consoleWarnFont", Desc: "settings.consoleWarnFontDesc", Default: "mono",
			Options: []model.SettingOption{
				{Value: "mono", Label: "settings.consoleFont.mono"},
				{Value: "sans", Label: "settings.consoleFont.sans"},
				{Value: "custom", Label: "settings.consoleFont.custom"},
			}},
		{Key: "consoleErrorFont", Type: "select", Group: "console", Label: "settings.consoleErrorFont", Desc: "settings.consoleErrorFontDesc", Default: "mono",
			Options: []model.SettingOption{
				{Value: "mono", Label: "settings.consoleFont.mono"},
				{Value: "sans", Label: "settings.consoleFont.sans"},
				{Value: "custom", Label: "settings.consoleFont.custom"},
			}},
		{Key: "consoleAI", Type: "bool", Group: "console", Label: "settings.consoleAI", Desc: "settings.consoleAIDesc", Default: true},
		{Key: "consoleAIKey", Type: "string", Group: "console", Label: "settings.consoleAIKey", Desc: "settings.consoleAIKeyDesc", Default: "", Placeholder: "settings.consoleAIKeyPlaceholder"},
		{Key: "showConsoleOnLaunch", Type: "bool", Group: "console", Label: "settings.showConsoleOnLaunch", Desc: "settings.showConsoleOnLaunchDesc", Default: false},
		{Key: "showConsoleOnCrash", Type: "bool", Group: "console", Label: "settings.showConsoleOnCrash", Desc: "settings.showConsoleOnCrashDesc", Default: true},
		{Key: "showConsoleOnClose", Type: "bool", Group: "console", Label: "settings.showConsoleOnClose", Desc: "settings.showConsoleOnCloseDesc", Default: false},
		{Key: "disableAutoRepair", Type: "bool", Group: "console", Label: "settings.disableAutoRepair", Desc: "settings.disableAutoRepairDesc", Default: false},

		// ── Завдання (обмеження завантажень) ──
		{Key: "maxConcurrentDownloads", Type: "int", Group: "tasks", Label: "settings.maxConcurrentDownloads", Desc: "settings.maxConcurrentDownloadsDesc", Default: 10, Min: intPtr(1), Max: intPtr(32), Step: intPtr(1)},
		{Key: "maxRetries", Type: "int", Group: "tasks", Label: "settings.maxRetries", Desc: "settings.maxRetriesDesc", Default: 6, Min: intPtr(1), Max: intPtr(20), Step: intPtr(1)},
		{Key: "httpTimeoutSec", Type: "int", Group: "tasks", Label: "settings.httpTimeout", Desc: "settings.httpTimeoutDesc", Default: 60, Min: intPtr(5), Max: intPtr(600), Step: intPtr(5)},

		// ── Команди ──
		{Key: "preLaunchCommand", Type: "string", Group: "commands", Label: "settings.preLaunchCommand", Desc: "settings.preLaunchCommandDesc", Default: "", Placeholder: "echo start", Multiline: true},
		{Key: "wrapperCommand", Type: "string", Group: "commands", Label: "settings.wrapperCommand", Desc: "settings.wrapperCommandDesc", Default: "", Placeholder: "optirun"},
		{Key: "postExitCommand", Type: "string", Group: "commands", Label: "settings.postExitCommand", Desc: "settings.postExitCommandDesc", Default: "", Placeholder: "echo exit", Multiline: true},
		{Key: "envVars", Type: "string", Group: "commands", Label: "settings.envVars", Desc: "settings.envVarsDesc", Default: "", Placeholder: "KEY=VALUE", Multiline: true},

		// ── Вікно гри ──
		{Key: "fullscreen", Type: "bool", Group: "game", Label: "settings.fullscreen", Desc: "settings.fullscreenDesc", Default: false},
		{Key: "windowWidth", Type: "int", Group: "game", Label: "settings.windowWidth", Desc: "settings.windowWidthDesc", Default: 854, Min: intPtr(480), Max: intPtr(7680), Step: intPtr(10)},
		{Key: "windowHeight", Type: "int", Group: "game", Label: "settings.windowHeight", Desc: "settings.windowHeightDesc", Default: 480, Min: intPtr(360), Max: intPtr(4320), Step: intPtr(10)},

		// ── Попередження ──
		// Група «Попередження»: глобальний вимикач RAM-попереджень (і про
		// рекомендовану RAM збірки, і про нестачу фізичної оперативки).
		// Per-pack «не показувати знову» живе у InstanceConfig, не тут.
		{Key: "showRamWarnings", Type: "bool", Group: "warnings", Label: "settings.showRamWarnings", Desc: "settings.showRamWarningsDesc", Default: true},
		// ── Розумний аналізатор запуску СЕРВЕРА ──
		// Головний вимикач + окремі пункти, які можна ігнорувати. Порт
		// показується ЗАВЖДИ (його неможливо вимкнути — це блокер).
		{Key: "showServerWarnings", Type: "bool", Group: "warnings", Label: "settings.showServerWarnings", Desc: "settings.showServerWarningsDesc", Default: true},
		{Key: "serverWarnIgnoreRam", Type: "bool", Group: "warnings", Label: "settings.serverWarnRam", Desc: "settings.serverWarnRamDesc", Default: false},
		{Key: "serverWarnIgnoreJava", Type: "bool", Group: "warnings", Label: "settings.serverWarnJava", Desc: "settings.serverWarnJavaDesc", Default: false},
		{Key: "serverWarnIgnoreDisk", Type: "bool", Group: "warnings", Label: "settings.serverWarnDisk", Desc: "settings.serverWarnDiskDesc", Default: false},
		{Key: "serverWarnIgnoreCrash", Type: "bool", Group: "warnings", Label: "settings.serverWarnCrash", Desc: "settings.serverWarnCrashDesc", Default: false},
		// Підтвердження зупинки гри: запит перед примусовою зупинкою
		// запущеної гри (перенесено з групи «Вікно гри» за запитом — це
		// теж попередження).
		{Key: "confirmOnStop", Type: "bool", Group: "warnings", Label: "settings.confirmOnStop", Desc: "settings.confirmOnStopDesc", Default: true},
		// Тимчасові моди («становлення на строк» — мод автоматично
		// прибирається зі збірки в задану дату): що робити за
		// замовчуванням, коли настає ця дата. False (вимикати, не
		// видаляти) — безпечніший дефолт, той самий принцип, що й
		// showRamWarnings/confirmOnStop у цій групі: не втратити щось
		// безповоротно через помилково вказану дату.
		{Key: "tempModsDeleteByDefault", Type: "bool", Group: "warnings", Label: "settings.tempModsDeleteByDefault", Desc: "settings.tempModsDeleteByDefaultDesc", Default: false},

		// ── Ігровий час ──
		{Key: "showGameTime", Type: "bool", Group: "gameTime", Label: "settings.showGameTime", Desc: "settings.showGameTimeDesc", Default: false},
		{Key: "recordGameTime", Type: "bool", Group: "gameTime", Label: "settings.recordGameTime", Desc: "settings.recordGameTimeDesc", Default: true},
		{Key: "disableCloudPlaytime", Type: "bool", Group: "gameTime", Label: "settings.disableCloudPlaytime", Desc: "settings.disableCloudPlaytimeDesc", Default: false},
		{Key: "showTotalGameTime", Type: "bool", Group: "gameTime", Label: "settings.showTotalGameTime", Desc: "settings.showTotalGameTimeDesc", Default: false},
		{Key: "gameTimeInHours", Type: "bool", Group: "gameTime", Label: "settings.gameTimeInHours", Desc: "settings.gameTimeInHoursDesc", Default: false},
		{Key: "showSessionTime", Type: "bool", Group: "gameTime", Label: "settings.showSessionTime", Desc: "settings.showSessionTimeDesc", Default: true},
		{Key: "showGameTimeOnPage", Type: "bool", Group: "gameTime", Label: "settings.showGameTimeOnPage", Desc: "settings.showGameTimeOnPageDesc", Default: true},

		// ── Discord Rich Presence ──
		// Показує у профілі Discord, що гравець зараз запущений у збірці
		// (або в кількох одночасно — «N збірок» + список). Перемикач
		// discordRpcEnabled дає гравцю змогу вимкнути присутність; за
		// замовчуванням увімкнено (як у старого лаунчера). Шаблони текстів
		// редагуються користувачем; плейсхолдери: {pack} / {list} / {count}
		// / {loader} / {mc}.
		{Key: "discordRpcEnabled", Type: "bool", Group: "discord", Label: "settings.discordRPCEnabled", Desc: "settings.discordRPCEnabledDesc", Default: true},
		{Key: "discordRPCDetails", Type: "string", Group: "discord", Label: "settings.discordRPCDetails", Desc: "settings.discordRPCDetailsDesc", Default: "{pack}", Placeholder: "{pack}"},
		{Key: "discordRPCState", Type: "string", Group: "discord", Label: "settings.discordRPCState", Desc: "settings.discordRPCStateDesc", Default: "{list}", Placeholder: "{list}"},

		// ── Пам'ять і кеш ──
		// Авто-видалення версій Minecraft, які більше не використовуються,
		// при видаленні збірки. Це окремий bool у групі «Пам'ять і кеш» —
		// увімкнено за замовчуванням (як у старого лаунчера: зайве не
		// накопичується).
		{Key: "cleanupAfterPackDelete", Type: "bool", Group: "storage", Label: "settings.storage.autoDeleteVersions", Desc: "settings.storage.autoDeleteVersionsDesc", Default: true},

		// ── Запуск і вікно ──
		// Один параметр на етап життя вікна (рішення — internal/winbehavior,
		// там же тести). Тип "choice" — картки вибору з поясненням кожного
		// варіанта, бо суть режимів («у фоні» vs «з вікном») не вгадати з
		// однієї назви.
		//
		// УВАГА: Default має збігатися з winbehavior.Defaults() і
		// DefaultSettings у config.go.
		{Key: "startupMode", Type: "choice", Group: "startup", Label: "settings.startupMode", Desc: "settings.startupModeDesc", Default: winbehavior.StartupWindow,
			Options: []model.SettingOption{
				{Value: winbehavior.StartupBackground, Label: "settings.startupMode.background", Desc: "settings.startupMode.backgroundDesc", Icon: "ti-bell-ringing"},
				{Value: winbehavior.StartupWindow, Label: "settings.startupMode.window", Desc: "settings.startupMode.windowDesc", Icon: "ti-app-window"},
				{Value: winbehavior.StartupNever, Label: "settings.startupMode.never", Desc: "settings.startupMode.neverDesc", Icon: "ti-player-stop"},
			}},

		{Key: "closeAction", Type: "choice", Group: "windowClose", Label: "settings.closeAction", Desc: "settings.closeActionDesc", Default: winbehavior.CloseQuit,
			Options: []model.SettingOption{
				{Value: winbehavior.CloseQuit, Label: "settings.closeAction.quit", Desc: "settings.closeAction.quitDesc", Icon: "ti-door-exit"},
				{Value: winbehavior.CloseTray, Label: "settings.closeAction.tray", Desc: "settings.closeAction.trayDesc", Icon: "ti-arrow-bar-to-down"},
			}},
		{Key: "minimizeAction", Type: "choice", Group: "windowClose", Label: "settings.minimizeAction", Desc: "settings.minimizeActionDesc", Default: winbehavior.MinimizeTaskbar,
			Options: []model.SettingOption{
				{Value: winbehavior.MinimizeTaskbar, Label: "settings.minimizeAction.taskbar", Desc: "settings.minimizeAction.taskbarDesc", Icon: "ti-layout-bottombar"},
				{Value: winbehavior.MinimizeTray, Label: "settings.minimizeAction.tray", Desc: "settings.minimizeAction.trayDesc", Icon: "ti-arrow-bar-to-down"},
			}},

		{Key: "gameLaunchAction", Type: "choice", Group: "windowGame", Label: "settings.gameLaunchAction", Desc: "settings.gameLaunchActionDesc", Default: winbehavior.LaunchKeep,
			Options: []model.SettingOption{
				{Value: winbehavior.LaunchKeep, Label: "settings.gameLaunchAction.keep", Desc: "settings.gameLaunchAction.keepDesc", Icon: "ti-app-window"},
				{Value: winbehavior.LaunchMinimize, Label: "settings.gameLaunchAction.minimize", Desc: "settings.gameLaunchAction.minimizeDesc", Icon: "ti-layout-bottombar"},
				{Value: winbehavior.LaunchTray, Label: "settings.gameLaunchAction.tray", Desc: "settings.gameLaunchAction.trayDesc", Icon: "ti-arrow-bar-to-down"},
			}},
		// Затримка має сенс лише коли вікно взагалі згортається.
		{Key: "gameLaunchDelaySec", Type: "int", Group: "windowGame", Label: "settings.gameLaunchDelaySec", Desc: "settings.gameLaunchDelaySecDesc", Default: 0,
			Min: intPtr(0), Max: intPtr(winbehavior.MaxLaunchDelaySec), Step: intPtr(1),
			ShowIf: []model.SettingCond{{Key: "gameLaunchAction", In: []string{winbehavior.LaunchMinimize, winbehavior.LaunchTray}}}},
		// Показується ЗАВЖДИ (а не лише при згортанні): «закрити лаунчер після
		// гри» корисне й коли вікно під час гри не чіпали (запуск з ярлика).
		{Key: "gameExitAction", Type: "choice", Group: "windowGame", Label: "settings.gameExitAction", Desc: "settings.gameExitActionDesc", Default: winbehavior.ExitRestore,
			Options: []model.SettingOption{
				{Value: winbehavior.ExitRestore, Label: "settings.gameExitAction.restore", Desc: "settings.gameExitAction.restoreDesc", Icon: "ti-arrow-back-up"},
				{Value: winbehavior.ExitStay, Label: "settings.gameExitAction.stay", Desc: "settings.gameExitAction.stayDesc", Icon: "ti-player-pause"},
				{Value: winbehavior.ExitQuit, Label: "settings.gameExitAction.quit", Desc: "settings.gameExitAction.quitDesc", Icon: "ti-door-exit"},
			}},
		// Страховка: при «нічого»/«закрити» крах гри не мусить пройти непоміченим.
		{Key: "restoreOnCrash", Type: "bool", Group: "windowGame", Label: "settings.restoreOnCrash", Desc: "settings.restoreOnCrashDesc", Default: true,
			ShowIf: []model.SettingCond{{Key: "gameExitAction", In: []string{winbehavior.ExitStay, winbehavior.ExitQuit}}}},

		// ── Сповіщення ──
		// Віджет у трей (правий нижній кут): тост із превʼю повідомлення в
		// чаті або запитом у друзі. Група окремим розділом меню
		// (settingsMeta.ts: секція notifications) — це щоденний «дзвінок»,
		// який має бути видно окремо, а не загубленим серед загальних.
		//
		// УВАГА: Default тут має збігатися з DefaultSettings у config.go —
		// ці два джерела дефолтів НЕ синхронізуються автоматично (схема —
		// для UI/першого показу, DefaultSettings — для реального стану).
		{Key: "notifyEnabled", Type: "bool", Group: "notifications", Label: "settings.notifyEnabled", Desc: "settings.notifyEnabledDesc", Default: true},
		{Key: "notifyDurationSec", Type: "int", Group: "notifications", Label: "settings.notifyDurationSec", Desc: "settings.notifyDurationSecDesc", Default: 7, Min: intPtr(notifyDurationRange.min), Max: intPtr(notifyDurationRange.max), Step: intPtr(1)},
		{Key: "notifyDnd", Type: "bool", Group: "notifications", Label: "settings.notifyDnd", Desc: "settings.notifyDndDesc", Default: false},
		// Мути чатів (ФАЗА: «не бачити повідомлень, коли граєш»). Окремо від
		// DND: DND — тимчасовий режим усіх сповіщень, ці два — стійкі
		// налаштування саме чатів.
		{Key: "chatMuteAll", Type: "bool", Group: "notifications", Label: "settings.chatMuteAll", Desc: "settings.chatMuteAllDesc", Default: false},
		{Key: "chatMuteSound", Type: "bool", Group: "notifications", Label: "settings.chatMuteSound", Desc: "settings.chatMuteSoundDesc", Default: false},
	}
}
