// Package core — головна логіка shrm-updater: перевірка оновлень,
// докачування, встановлення, запуск launcher.exe, health-check з
// авто-rollback якщо нова версія не змогла стартувати.
package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"shrm-updater/internal/download"
	"shrm-updater/internal/install"
	"shrm-updater/internal/manifest"
)

// CDNBaseURL — базова адреса worker'а. Той самий worker, що вже
// обслуговує /packs, /java і т.д. (shrm-cdn-worker/worker.js) — шлях
// launcher/launcher-version.json вже підтримується resolveR2Key без
// жодних змін на боці worker.
const CDNBaseURL = "https://cdn.shaurma.app"

const launcherExeName = "launcher.exe"

// tokenShaurma/tokenBase — токени CDN, вшиті В САМ БІНАРНИК на етапі
// збірки через -ldflags "-X", а НЕ прочитані з env/файлу під час
// роботи. Причина: shrm-updater.exe ставиться на комп'ютери всіх
// користувачів — токен обов'язково опиниться "в чиїхось руках" рано чи
// пізно (можна дістати strings.exe по бінарнику), це прийнятний рівень
// захисту (той самий, що й у worker.js: "усуває 99% автоматизованих
// сканерів", не криптографічну таємницю). Але порожні env-змінні,
// якщо забути їх встановити при інсталяції — гарантований 401 для
// КОЖНОГО користувача, а не для зловмисника. Вшивання на етапі build.ps1
// прибирає цю причину відмови повністю.
//
// build.ps1 викликає:
//   go build -ldflags "-X shrm-updater/internal/core.tokenShaurma=... -X shrm-updater/internal/core.tokenBase=..."
// Значення беруться з build.ps1 -TokenShaurma/-TokenBase (або з
// SHRM_BUILD_TOKEN_SHAURMA/SHRM_BUILD_TOKEN_BASE в оточенні, якщо
// прапорці не передані — див. build.ps1).
var (
	tokenShaurma string
	tokenBase    string
)

// Channel — канал оновлення, відповідає токену на worker'і
// (shrm-cdn-worker/worker.js: TOKEN_SHAURMA → 'shaurma', TOKEN_BASE →
// 'base'). Токен ВИЗНАЧАЄ канал на боці worker'а (resolveTokenType).
type Channel string

const (
	ChannelShaurma Channel = "shaurma"
	ChannelBase    Channel = "base"
)

// token повертає вшитий build-time токен для каналу.
func (c Channel) token() string {
	if c == ChannelBase {
		return tokenBase
	}
	return tokenShaurma
}

// Status — стан процесу оновлення, для прив'язки до UI (webview).
type Status int

const (
	StatusChecking Status = iota
	StatusUpToDate
	StatusUpdateAvailable
	StatusDownloading
	StatusInstalling
	StatusLaunching
	StatusError
)

type Callbacks struct {
	OnStatus   func(Status, string)
	OnProgress func(download.Progress)
	// OnUpdateAvailable викликається, коли знайдено оновлення, до старту
	// завантаження: віддає нову версію та сумарний розмір файлів, які
	// треба завантажити (для UI "Нова версія X.Y.Z, завантажити N МБ").
	OnUpdateAvailable func(version string, bytesTotal int64, fileCount int)
	Lang              string // "uk" | "en" — мова статус-повідомлень (з settings.json)
}

// launchPause — пауза між повідомленням "Запуск лаунчера..." і реальним
// стартом launcher.exe. Дає frontend-у час домальовувати прогрес-бар до
// 100%: користувач бачить повний бар, і ЛИШЕ після цього відкривається
// вікно лаунчера.
const launchPause = 900 * time.Millisecond

// tr повертає статус-повідомлення потрібною мовою. Якщо мова не en —
// використовуємо український варіант за замовчуванням. args підставляються
// у шаблон (%s), як у fmt.Sprintf.
func (cb Callbacks) tr(uk, en string, args ...interface{}) string {
	s := uk
	if cb.Lang == "en" {
		s = en
	}
	if len(args) > 0 {
		return fmt.Sprintf(s, args...)
	}
	return s
}

// Run — головний вхід. installDir — %LOCALAPPDATA%\ShaurmLauncher.
// channel визначає, який вшитий токен (і відповідно яка гілка
// launchers/shaurma чи launchers/base на R2) використовується для
// перевірки і завантаження оновлення.
func Run(ctx context.Context, installDir string, channel Channel, cb Callbacks) error {
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return fmt.Errorf("%s: %w", cb.tr("створення", "creating")+" "+installDir, err)
	}

	token := channel.token()
	if token == "" {
		return fmt.Errorf("%s %q %s (build.ps1 -TokenShaurma/-TokenBase)", cb.tr("токен для каналу", "token for channel"), channel, cb.tr("не вшито в білд — перевір", "not embedded in build — check"))
	}

	localManifestPath := filepath.Join(installDir, "launcher-version.json")
	local, err := manifest.Load(localManifestPath)
	if err != nil {
		return fmt.Errorf("%s: %w", cb.tr("читання локального маніфесту", "reading local manifest"), err)
	}

	notify(cb, StatusChecking, cb.tr("Перевірка оновлень...", "Checking for updates..."))
	remote, err := fetchRemoteManifest(ctx, token, cb)
	if err != nil {
		// Немає мережі / CDN недоступний: якщо launcher.exe вже стоїть
		// на диску — просто запускаємо те, що є, а не блокуємо
		// користувача через тимчасову недоступність CDN. Якщо ж
		// launcher.exe взагалі немає (щойно встановлений з нуля, перед
		// першим запуском) — тут справді нічого вдіяти, повертаємо
		// помилку.
		if exeExists(installDir) {
			notify(cb, StatusUpToDate, cb.tr("CDN недоступний, запускаємо поточну версію", "CDN unreachable, launching the current version"))
			time.Sleep(launchPause)
			return launchAndWait(installDir)
		}
		return fmt.Errorf("%s: %w", cb.tr("немає мережі і немає встановленої версії", "no network and no installed version"), err)
	}

	toDownload := manifest.Diff(local, remote, installDir)

	if len(toDownload) == 0 {
		notify(cb, StatusUpToDate, cb.tr("Лаунчер актуальний", "Launcher is up to date"))
		// Пауза до старту: показуємо користувачу 100% прогрес-бара і
		// повідомлення, перш ніж відкриється вікно лаунчера.
		time.Sleep(launchPause)
		return launchAndWait(installDir)
	}

	// Сумарний розмір оновлення — для UI ("Нова версія X, завантажити N МБ").
	var totalBytes int64
	for _, c := range toDownload {
		totalBytes += c.Size
	}
	if cb.OnUpdateAvailable != nil {
		cb.OnUpdateAvailable(remote.Version, totalBytes, len(toDownload))
	}

	notify(cb, StatusUpdateAvailable, cb.tr("Доступне оновлення: %s", "Update available: %s", remote.Version))
	notify(cb, StatusDownloading, cb.tr("Завантаження оновлення...", "Downloading update..."))

	tmpDir := filepath.Join(os.TempDir(), "shrm-update")
	downloaded, err := download.Download(ctx, CDNBaseURL, token, tmpDir, toDownload, cb.Lang, func(p download.Progress) {
		if cb.OnProgress != nil {
			cb.OnProgress(p)
		}
	})
	if err != nil {
		notify(cb, StatusError, err.Error())
		return err
	}

	notify(cb, StatusInstalling, cb.tr("Встановлення...", "Installing..."))

	// launcher.exe може ще працювати, якщо updater запущено вручну
	// поки лаунчер відкритий (а не через його власний RUN_UPDATER,
	// який гарантує, що процес уже зупинений) — тому перед install
	// намагаємось коректно завершити launcher.exe, якщо він живий.
	if err := stopRunningLauncher(installDir); err != nil {
		notify(cb, StatusError, cb.tr("Не вдалось зупинити запущений лаунчер: ", "Failed to stop the running launcher: ")+err.Error())
		return err
	}

	if err := install.Apply(ctx, installDir, downloaded, remote, cb.Lang, func(msg string) {
		notify(cb, StatusInstalling, msg)
	}); err != nil {
		notify(cb, StatusError, err.Error())
		return err
	}

	notify(cb, StatusLaunching, cb.tr("Запуск лаунчера...", "Launching launcher..."))
	// Пауза до старту (див. launchPause): бар має дійти до 100% перш,
	// ніж відкриється вікно лаунчера.
	time.Sleep(launchPause)
	return launchAndHealthCheck(installDir, cb)
}

func notify(cb Callbacks, s Status, msg string) {
	if cb.OnStatus != nil {
		cb.OnStatus(s, msg)
	}
}

func fetchRemoteManifest(ctx context.Context, token string, cb Callbacks) (*manifest.Manifest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, CDNBaseURL+"/launcher/launcher-version.json", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ShaurmaLauncher/updater")
	req.Header.Set("X-Shaurma-Token", token)

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %d", cb.tr("HTTP %d при отриманні launcher-version.json", "HTTP %d fetching launcher-version.json"), resp.StatusCode)
	}

	var m manifest.Manifest
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, fmt.Errorf("%s: %w", cb.tr("парсинг launcher-version.json", "parsing launcher-version.json"), err)
	}
	return &m, nil
}

func exeExists(installDir string) bool {
	_, err := os.Stat(filepath.Join(installDir, launcherExeName))
	return err == nil
}

// launchAndWait запускає launcher.exe і одразу завершує updater —
// звичайний happy-path, коли оновлення не було. Updater — це bootstrap,
// не резидентний процес: після успішного запуску йому нема чого робити.
//
// ВАЖЛИВО: launcher.exe запускається звичайним exec.Command, БЕЗ
// прив'язки до ctx застосунку — інакше скасування ctx (а Quit-вихід
// Wails його скасовує) могло б вбити щойно стартований launcher.exe
// разом з updater. Updater завжди мусить запустити launcher до того,
// як сам завершиться.
func launchAndWait(installDir string) error {
	exePath := filepath.Join(installDir, launcherExeName)
	cmd := exec.Command(exePath)
	cmd.Dir = installDir
	return cmd.Start()
}

// launchAndHealthCheck — те саме, але після свіжого оновлення: чекає
// коротко, чи процес не впав одразу (типова ознака биту білду —
// відсутня DLL, пошкоджений embed фронтенду тощо), і якщо впав —
// відкочує launcher.exe на .previous і повідомляє помилку, замість
// того щоб лишити користувача з непрацюючим лаунчером після
// "успішного" оновлення.
func launchAndHealthCheck(installDir string, cb Callbacks) error {
	exePath := filepath.Join(installDir, launcherExeName)
	cmd := exec.Command(exePath)
	cmd.Dir = installDir
	if err := cmd.Start(); err != nil {
		return rollbackAndReport(installDir, cb, fmt.Errorf("%s: %w", cb.tr("launcher.exe не запустився", "launcher.exe failed to start"), err))
	}

	// Health-check вікно: якщо процес живий довше цього — вважаємо
	// запуск успішним. Свідомо коротке (2с) — Wails-вікно піднімається
	// швидко; довший health-check лише сповільнював би звичайний
	// щасливий шлях без користі (крах "після 5 секунд роботи" однаково
	// краще ловити через окремий crash-report, не тут).
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		// Процес завершився майже одразу — це крах, не штатне
		// закриття (штатно користувач ще нічого не встиг натиснути).
		return rollbackAndReport(installDir, cb, fmt.Errorf("%s: %w", cb.tr("launcher.exe завершився одразу після старту", "launcher.exe exited right after start"), err))
	case <-time.After(2 * time.Second):
		return nil
	}
}

func rollbackAndReport(installDir string, cb Callbacks, cause error) error {
	if rbErr := install.Rollback(installDir, launcherExeName); rbErr != nil {
		return fmt.Errorf("%w (%s: %v)", cause, cb.tr("rollback теж не вдався", "rollback also failed"), rbErr)
	}
	// Відкат вдався — пробуємо запустити стару версію одразу, щоб
	// користувач не лишився взагалі без робочого лаунчера після
	// невдалого оновлення.
	exePath := filepath.Join(installDir, launcherExeName)
	_ = exec.Command(exePath).Start()
	return fmt.Errorf("%s: %w", cb.tr("нова версія не запустилась, відкочено до попередньої", "new version failed to start, rolled back to the previous one"), cause)
}

// stopRunningLauncher намагається коректно закрити launcher.exe, якщо
// він працює. Використовується, коли updater запущено вручну поки
// лаунчер відкритий (звичайний RUN_UPDATER-шлях від самого лаунчера
// вже гарантує зупинку до старту updater — тут підстраховка для
// випадку прямого запуску updater).
//
// Спершу — М'ЯКИЙ запит через флаг-файл (shrm-quit.flag у теці
// конфігурації, це та сама тека, де стоїть launcher.exe): сучасні версії
// лаунчера стежать за ним і самі виконують graceful shutdown (PauseAll
// синхронізацій, збереження стану) перед виходом. Раніше тут одразу
// стояв taskkill /F — форс-кілл не дає процесу жодного шансу
// відреагувати, тож .part-файли обривались посеред чанка. taskkill
// лишається лише як останній засіб для старих версій лаунчера (без
// watcher-а) або завислого процесу.
func stopRunningLauncher(installDir string) error {
	if !launcherRunning() {
		return nil
	}

	// Прапорець м'якого завершення; лаунчер сам прибере його, коли
	// помітить. Якщо лаунчер впаде між записом і видаленням — наступний
	// запуск проігнорує застарілий прапорець (freshness-перевірка).
	flagPath := filepath.Join(installDir, "shrm-quit.flag")
	_ = os.WriteFile(flagPath, []byte("quit"), 0o644)

	// Чекаємо, поки лаунчер завершиться сам (м'який шлях), з таймаутом.
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if !launcherRunning() {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}

	// Не завершився (стара версія / завис) — форс-кілл як останній засіб.
	cmd := exec.Command("taskkill", "/IM", launcherExeName, "/F")
	// Ігноруємо помилку "процес не знайдено" (exit code 128) — це
	// нормальний випадок "лаунчер і так не був запущений" (міг
	// завершитись сам, поки ми чекали).
	_ = cmd.Run()
	return nil
}

// launcherRunning перевіряє, чи є процес launcher.exe у системі.
func launcherRunning() bool {
	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq "+launcherExeName, "/NH").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), launcherExeName)
}
