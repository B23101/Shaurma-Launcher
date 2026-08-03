package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"shrm-updater/internal/core"
	"shrm-updater/internal/download"
	"shrm-updater/internal/manifest"
)

//go:embed all:frontend
var assets embed.FS

// App — Wails-біндинг для updater-вікна. Тонкий шар над internal/core:
// уся логіка перевірки/завантаження/встановлення лишається в core, тут
// тільки прокидування прогресу в UI через events і команди Retry/Close
// з кнопок.
type App struct {
	ctx        context.Context
	installDir string
	channel    core.Channel
	lang       string
}

// GetPrefs повертає налаштування персоналізації лаунчера (мова, акцент,
// тема) з settings.json — щоб splash-екран апдейтера виглядав в унісон з
// основними екранами: той самий акцентний колір і тема.
func (a *App) GetPrefs() map[string]string {
	return map[string]string{
		"lang":   a.GetLanguage(),
		"accent": readPref(a.installDir, "accent", "orange"),
		"theme":  readPref(a.installDir, "theme", "dark"),
	}
}

// readPref читає рядкове поле з settings.json лаунчера з дефолтом.
func readPref(installDir, key, def string) string {
	data, err := os.ReadFile(filepath.Join(installDir, "settings.json"))
	if err != nil {
		return def
	}
	var s map[string]any
	if err := json.Unmarshal(data, &s); err != nil {
		return def
	}
	if v, ok := s[key].(string); ok && v != "" {
		return v
	}
	return def
}

// GetLanguage повертає мову інтерфейсу для i18n у frontend: читається з
// settings.json лаунчера (%LOCALAPPDATA%\ShaurmLauncher\settings.json,
// поле language). Якщо файлу немає або значення порожнє — «uk».
func (a *App) GetLanguage() string {
	if a.lang != "" {
		return a.lang
	}
	return detectLanguage(a.installDir)
}

// detectLanguage читає мову з settings.json лаунчера. Це мова, яку
// користувач обрав у налаштуваннях — апдейтер має говорити тою самою
// мовою, що й основний лаунчер.
func detectLanguage(installDir string) string {
	data, err := os.ReadFile(filepath.Join(installDir, "settings.json"))
	if err != nil {
		return "uk"
	}
	var s struct {
		Language string `json:"language"`
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return "uk"
	}
	if s.Language == "en" || s.Language == "uk" {
		return s.Language
	}
	// 'auto' або будь-що інше — найпоширеніший сценарій для цього
	// продукту (укр. аудиторія), тому за замовчуванням uk.
	return "uk"
}

// ServiceStartup викликається Wails v3 на старті застосунку (App
// реалізує application.ServiceStartup). Тут — те, що в v2 було в OnStartup.
func (a *App) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	a.ctx = ctx
	a.lang = detectLanguage(a.installDir)

	go a.runUpdate()
	return nil
}

// emit шле подію на фронтенд через глобальний EventManager v3.
func (a *App) emit(name string, data ...any) {
	if app := application.Get(); app != nil {
		app.Event.Emit(name, data...)
	}
}

func (a *App) runUpdate() {
	cb := core.Callbacks{
		Lang: a.lang,
		OnStatus: func(s core.Status, msg string) {
			a.emit("updater:status", map[string]any{
				"status":  statusToString(s),
				"message": msg,
			})
		},
		OnProgress: func(p download.Progress) {
			a.emit("updater:progress", map[string]any{
				"fileIndex":  p.FileIndex,
				"fileTotal":  p.FileTotal,
				"fileName":   p.FileName,
				"bytesDone":  p.BytesDone,
				"bytesTotal": p.BytesTotal,
			})
		},
		OnUpdateAvailable: func(version string, bytesTotal int64, fileCount int) {
			a.emit("updater:update", map[string]any{
				"version":    version,
				"bytesTotal": bytesTotal,
				"fileCount":  fileCount,
			})
		},
	}

	// Версійний рядок для верхнього напису "1.2.3 → 1.3.0" — читаємо
	// локальний манiфест окремо тут (core.Run теж читає його
	// внутрішньо, невелике дублювання свідоме: не хочемо ускладнювати
	// сигнатуру core.Run поверненням проміжного стану лише заради UI).
	if local, err := manifest.Load(filepath.Join(a.installDir, "launcher-version.json")); err == nil {
		a.emit("updater:version", map[string]any{
			"current": local.Version,
		})
	}

	if err := core.Run(a.ctx, a.installDir, a.channel, cb); err != nil {
		a.emit("updater:status", map[string]any{
			"status":  "error",
			"message": err.Error(),
		})
		// НЕ виходимо автоматично при помилці — користувач бачить
		// картку помилки з кнопками "Повторити"/"Закрити" (index.html),
		// і сам вирішує. Автовихід тут ховав би критичну інформацію
		// про те, чому оновлення не вдалось.
		return
	}

	// Успішний шлях (з оновленням чи без): core.Run запустив launcher.exe
	// і повернувся БЕЗ помилки — тільки тепер updater може помирати.
	// ВАЖЛИВО не виходити раніше (як було за статусом StatusLaunching):
	// Quit завершує застосунок, а якби launcher.exe запускався через
	// exec.CommandContext(ctx,...), його старт був би прив'язаний до
	// життя updater і міг не встигнути. Тепер порядок гарантований:
	// спершу cmd.Start() для launcher.exe, потім — завершення updater.
	go func() {
		// Коли прогрес-бар уже дійшов до 100% (frontend показує фінальний
		// статус), launcher.exe запущено. Тепер убиваємо updater майже
		// миттєво — невеличка затримка в пару сотень мілісекунд лише для
		// того, щоб останній статус ("Запуск лаунчера...") встиг
		// відрендеритись, а не зникнув мигцем. Жодного фіксованого
		// мінімального часу показу splash більше немає: користувач
		// бачить швидку роботу, а не зависле вікно.
		time.Sleep(300 * time.Millisecond)
		application.Get().Quit()
	}()
}

func statusToString(s core.Status) string {
	switch s {
	case core.StatusChecking:
		return "checking"
	case core.StatusUpToDate:
		return "up_to_date"
	case core.StatusUpdateAvailable:
		return "update_available"
	case core.StatusDownloading:
		return "downloading"
	case core.StatusInstalling:
		return "installing"
	case core.StatusLaunching:
		return "launching"
	default:
		return "error"
	}
}

// Retry — викликається кнопкою "Повторити" у вікні помилки. Перезапускає
// весь runUpdate() з нуля: якщо мережа щойно відновилась, наступна
// спроба піде тим самим шляхом, що і при штатному старті.
func (a *App) Retry() {
	go a.runUpdate()
}

// CloseUpdater — кнопка "Закрити" у вікні помилки. Якщо
// launcher.exe вже стоїть на диску (просто зараз не вдалось
// перевірити/докачати оновлення), запускаємо його як є, замість
// того щоб лишити користувача взагалі без робочого лаунчера через
// тимчасову проблему з оновленням.
func (a *App) CloseUpdater() {
	exePath := filepath.Join(a.installDir, "launcher.exe")
	if _, err := os.Stat(exePath); err == nil {
		_ = launchDetached(exePath, a.installDir)
	}
	application.Get().Quit()
}

func main() {
	finalize := flag.Bool("finalize", false, "Запущено самим лаунчером через RUN_UPDATER — launcher.exe вже зупинений")
	installDirFlag := flag.String("install-dir", "", "Корінь встановлення (за замовчуванням %LOCALAPPDATA%\\ShaurmLauncher)")
	channelFlag := flag.String("channel", "shaurma", "Канал оновлення: shaurma або base (відповідає TOKEN_SHAURMA/TOKEN_BASE на worker'і)")
	flag.Parse()

	var channel core.Channel
	switch *channelFlag {
	case "base":
		channel = core.ChannelBase
	case "shaurma":
		channel = core.ChannelShaurma
	default:
		println("Невідомий --channel:", *channelFlag, "(очікується shaurma або base)")
		os.Exit(1)
	}

	installDir := *installDirFlag
	if installDir == "" {
		localAppData := os.Getenv("LOCALAPPDATA")
		installDir = filepath.Join(localAppData, "ShaurmLauncher")
	}

	// finalize наразі впливає лише на те, що ми НЕ намагаємось
	// зупинити launcher.exe вдруге (core.stopRunningLauncher і так
	// безпечний no-op, якщо процес вже мертвий, — прапорець лишений
	// для майбутнього, коли з'явиться повідомлення в UI "завершуємо
	// оновлення, розпочате основним лаунчером" замість загального
	// "Перевірка оновлень...").
	_ = finalize

	app := &App{installDir: installDir, channel: channel}

	updaterApp := application.New(application.Options{
		Name:        "Shaurma Launcher — Оновлення",
		Description: "Shaurma Launcher Updater",
		Services: []application.Service{
			application.NewService(app),
		},
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(assets),
		},
	})

	updaterApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:           "Shaurma Launcher — Оновлення",
		Width:           280,
		Height:          340,
		MinWidth:        280,
		MinHeight:       340,
		MaxWidth:        280,
		MaxHeight:       340,
		DisableResize:   true,
		Frameless:       true,
		BackgroundType:  application.BackgroundTypeSolid,
		BackgroundColour: application.NewRGBA(15, 15, 15, 255),
	})

	if err := updaterApp.Run(); err != nil {
		println("Error:", err.Error())
		os.Exit(1)
	}
}

// launchDetached запускає launcher.exe як незалежний процес — updater
// не повинен лишатись батьківським процесом для нього (інакше закриття
// updater/термінала могло б потягнути за собою і лаунчер на деяких
// системах).
func launchDetached(exePath, workDir string) error {
	cmd := exec.Command(exePath)
	cmd.Dir = workDir
	return cmd.Start()
}
