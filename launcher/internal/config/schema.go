package config

import "shaurma-launcher-wails/internal/model"

func intPtr(v int) *int { return &v }

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
				{Value: "dev", Label: "settings.channel.dev"},
			}},
		{Key: "closeOnLaunch", Type: "bool", Group: "general", Label: "settings.closeOnLaunch", Desc: "settings.closeOnLaunchDesc", Default: false},
		{Key: "showConsole", Type: "bool", Group: "general", Label: "settings.showConsole", Desc: "settings.showConsoleDesc", Default: false},
		{Key: "saveLogs", Type: "bool", Group: "general", Label: "settings.saveLogs", Desc: "settings.saveLogsDesc", Default: true},

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
				{Value: "custom", Label: "accent.custom"},
			}},
		// Кастомний акцент: колір, який користувач обрав у пікері. Показується
		// на вкладці «Вигляд» лише коли accent === "custom".
		{Key: "accentCustom", Type: "color", Group: "appearance", Label: "settings.accentCustom", Desc: "settings.accentCustomDesc", Default: "#ff8a00", Multiline: false},
		{Key: "theme", Type: "select", Group: "appearance", Label: "settings.theme", Desc: "settings.themeDesc", Default: "dark",
			Options: []model.SettingOption{
				{Value: "dark", Label: "settings.theme.dark"},
				{Value: "light", Label: "settings.theme.light"},
			}},
		{Key: "font", Type: "select", Group: "appearance", Label: "settings.font", Desc: "settings.fontDesc", Default: "inter",
			Options: []model.SettingOption{
				{Value: "inter", Label: "settings.font.inter"},
				{Value: "system", Label: "settings.font.system"},
				{Value: "jetbrains", Label: "settings.font.jetbrains"},
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

		// ── Продуктивність ──
		{Key: "maxRAM", Type: "ram", Group: "performance", Label: "settings.ram", Desc: "settings.ramDesc", Default: 4096, Min: intPtr(1024), Max: intPtr(16384), Step: intPtr(512)},
		{Key: "javaArgs", Type: "string", Group: "performance", Label: "settings.javaArgs", Desc: "settings.javaArgsDesc", Default: "", Placeholder: "-XX:+UseG1GC -XX:+ParallelRefProcEnabled"},

		// ── Консоль ──
		// Ліміт буфера — рядками; розміру консолі НЕМАЄ (розтягується по
		// вікну); режиму консолі НЕМАЄ (є окремі перемикачі showConsoleOn*).
		{Key: "consoleMaxLines", Type: "int", Group: "console", Label: "settings.consoleMaxLines", Desc: "settings.consoleMaxLinesDesc", Default: 3000, Min: intPtr(100), Max: intPtr(50000), Step: intPtr(100)},
		{Key: "consoleInfoColor", Type: "color", Group: "console", Label: "settings.consoleInfoColor", Desc: "settings.consoleInfoColorDesc", Default: "#8fd3ff", Multiline: false},
		{Key: "consoleWarnColor", Type: "color", Group: "console", Label: "settings.consoleWarnColor", Desc: "settings.consoleWarnColorDesc", Default: "#ffd166", Multiline: false},
		{Key: "consoleErrorColor", Type: "color", Group: "console", Label: "settings.consoleErrorColor", Desc: "settings.consoleErrorColorDesc", Default: "#ff6b6b", Multiline: false},
		{Key: "consoleWrap", Type: "bool", Group: "console", Label: "settings.consoleWrap", Desc: "settings.consoleWrapDesc", Default: false},
		{Key: "consoleFontSize", Type: "int", Group: "console", Label: "settings.consoleFontSize", Desc: "settings.consoleFontSizeDesc", Default: 12, Min: intPtr(9), Max: intPtr(24), Step: intPtr(1)},
		{Key: "consoleFontFamily", Type: "select", Group: "console", Label: "settings.consoleFontFamily", Desc: "settings.consoleFontFamilyDesc", Default: "mono",
			Options: []model.SettingOption{
				{Value: "mono", Label: "settings.consoleFont.mono"},
				{Value: "sans", Label: "settings.consoleFont.sans"},
			}},
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
			}},
		{Key: "consoleWarnFont", Type: "select", Group: "console", Label: "settings.consoleWarnFont", Desc: "settings.consoleWarnFontDesc", Default: "mono",
			Options: []model.SettingOption{
				{Value: "mono", Label: "settings.consoleFont.mono"},
				{Value: "sans", Label: "settings.consoleFont.sans"},
			}},
		{Key: "consoleErrorFont", Type: "select", Group: "console", Label: "settings.consoleErrorFont", Desc: "settings.consoleErrorFontDesc", Default: "mono",
			Options: []model.SettingOption{
				{Value: "mono", Label: "settings.consoleFont.mono"},
				{Value: "sans", Label: "settings.consoleFont.sans"},
			}},
		{Key: "consoleAI", Type: "bool", Group: "console", Label: "settings.consoleAI", Desc: "settings.consoleAIDesc", Default: false},
		{Key: "showConsoleOnLaunch", Type: "bool", Group: "console", Label: "settings.showConsoleOnLaunch", Desc: "settings.showConsoleOnLaunchDesc", Default: false},
		{Key: "showConsoleOnCrash", Type: "bool", Group: "console", Label: "settings.showConsoleOnCrash", Desc: "settings.showConsoleOnCrashDesc", Default: true},
		{Key: "showConsoleOnClose", Type: "bool", Group: "console", Label: "settings.showConsoleOnClose", Desc: "settings.showConsoleOnCloseDesc", Default: false},

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
		{Key: "hideOnGameOpen", Type: "bool", Group: "game", Label: "settings.hideOnGameOpen", Desc: "settings.hideOnGameOpenDesc", Default: false},
		{Key: "exitOnGameClose", Type: "bool", Group: "game", Label: "settings.exitOnGameClose", Desc: "settings.exitOnGameCloseDesc", Default: false},
		{Key: "confirmOnStop", Type: "bool", Group: "game", Label: "settings.confirmOnStop", Desc: "settings.confirmOnStopDesc", Default: true},

		// ── Ігровий час ──
		{Key: "showGameTime", Type: "bool", Group: "gameTime", Label: "settings.showGameTime", Desc: "settings.showGameTimeDesc", Default: false},
		{Key: "recordGameTime", Type: "bool", Group: "gameTime", Label: "settings.recordGameTime", Desc: "settings.recordGameTimeDesc", Default: true},
		{Key: "showTotalGameTime", Type: "bool", Group: "gameTime", Label: "settings.showTotalGameTime", Desc: "settings.showTotalGameTimeDesc", Default: false},
		{Key: "gameTimeInHours", Type: "bool", Group: "gameTime", Label: "settings.gameTimeInHours", Desc: "settings.gameTimeInHoursDesc", Default: false},
	}
}
