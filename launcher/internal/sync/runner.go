package sync

import (
	"archive/zip"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

const (
	backoffBase = 400 * time.Millisecond
	backoffMax  = 8 * time.Second
)

// RunMode — як завершився/зупинився Run. Розрізняє два принципово різні
// стани, які користувач бачить по-різному в UI:
//
//   - ModeCancelled: користувач натиснув "Скасувати" — усі часткові файли
//     (.part, тимчасові архіви, вже розпаковані override-folders цього
//     запуску) видаляються, InstallState відкочується назад, кнопка в UI
//     повертається на "Встановити" (не "Продовжити").
//   - ModePaused: користувач натиснув "Зупинити", ЗАКРИВ лаунчер, або
//     процес впав (крах) — прогрес ЗБЕРІГАЄТЬСЯ (.part-файли лишаються на
//     диску, InstallState.Stage лишається як був), у UI показується
//     кнопка "Продовжити". Крах кваліфікується як Pause, а НЕ Cancel: у
//     StateStore.Load ми ніколи не бачимо "control-запиту" на скасування,
//     тож якщо стан на диску не StageComplete/StageNone — це завжди
//     відновлюваний Pause, навіть якщо процес просто зник без явного
//     виклику Stop.
type RunMode string

const (
	ModeNone      RunMode = ""
	ModeRunning   RunMode = "running"
	ModePaused    RunMode = "paused"
	ModeCancelled RunMode = "cancelled"
	ModeComplete  RunMode = "complete"
	ModeError     RunMode = "error"
)

// ProgressReporter — узагальнений прогрес обох черг РАЗОМ (агрегатор,
// розділ 1.1 V2: "обидві черги звітують у СПІЛЬНИЙ агрегатор, який дає
// користувачу один зрозумілий загальний %"). Викликається часто (кожні
// ~300мс) — саме це живить sidebar, картку збірки в "Моїх збірках" і
// сторінку збірки одночасно, тож усі три місця UI завжди синхронні.
type ProgressReporter func(p RunnerProgress)

type RunnerProgress struct {
	PackID          string  `json:"packId"`
	PackName        string  `json:"packName"`
	Mode            RunMode `json:"mode"`
	FilesTotal      int     `json:"filesTotal"`
	FilesCompleted  int     `json:"filesCompleted"`
	BytesTotal      int64   `json:"bytesTotal"`
	BytesDownloaded int64   `json:"bytesDownloaded"`
	OverallPercent  int     `json:"overallPercent"`
	CurrentFile     string  `json:"currentFile"`
	SpeedBPS        float64 `json:"speedBps"`
	// Stage — стадія InstallState, показується в UI ("Завантаження",
	// "Розпаковка", "Завершення").
	Stage Stage `json:"stage"`
	// NoInternet — true, якщо остання спроба зафіксувала повну відсутність
	// мережі (а не тимчасовий збій сервера/Worker). UI показує явний банер
	// "Немає з'єднання з інтернетом", а не загальне "помилка завантаження".
	NoInternet bool   `json:"noInternet"`
	Done       bool   `json:"done"`
	Error      error  `json:"-"`
	ErrorMsg   string `json:"errorMsg,omitempty"`
}

// Submitter — інтерфейс над download.Engine.Submit, щоб sync-пакет не
// імпортував download напряму (уникаємо циклічного імпорту) і щоб тести
// могли підмінити чергу звичайним goroutine-пулом.
type Submitter interface {
	Submit(fn func())
}

// Runner — двочерговий рушій однієї сесії синхронізації (одна збірка).
// Черга А (hash-моди) і Черга Б (worker-файли) стартують ОДНОЧАСНО, кожна
// зі своїм лімітом потоків (розділ 1.1 V2), обидві діляться СПІЛЬНИМ
// пулом Engine (через Submitter) — тому паралельні сесії/черги не
// подвоюють реальний мережевий паралелізм, і кілька збірок можна якати
// одночасно без взаємного "з'їдання" лімітів.
type Runner struct {
	PackID      string
	PackName    string
	InstallDir  string // корінь установки збірки на диску
	WorkerBase  string // базовий URL Worker (для workerFiles.Path)
	WorkerToken string
	Client      *http.Client
	Submitter   Submitter
	State       *InstallState
	StateStore  *StateStore
	MaxRetries  int
	OnProgress  ProgressReporter

	mu             sync.Mutex
	bytesTotal     int64
	bytesDone      int64
	filesTotal     int
	filesDone      int32
	currentFile    string
	lastNoInternet int32
	speedLastBytes int64
	speedLastCheck time.Time
	speedBPS       float64

	// control — керування ЗЗОВНІ (Stop/Cancel/панічне закриття) поки Run
	// виконується. pauseRequested/cancelRequested виставляються атомарно й
	// перевіряються в гарячому циклі downloadTask/циклі черг — це дешевше й
	// надійніше за скасування контексту, бо дає розрізнити ДВІ різні дії
	// (Cancel прибирає часткові файли, Pause — ні), тоді як ctx.Done() сам
	// по собі кодує лише "зупинись", без наміру.
	pauseRequested  int32
	cancelRequested int32
	mode            RunMode
	// partialPaths — усі .part/тимчасові файли й новостворені override-теки
	// ЦЬОГО запуску Run — потрібні тільки для Cancel (щоб точково прибрати
	// саме те, що встигли створити зараз, а не чіпати файли, які лежали на
	// диску до старту синхронізації).
	partialPaths   map[string]bool
	partialPathsMu sync.Mutex
}

// NewRunner створює Runner у стані ModeNone; PackName можна лишити
// порожнім — тоді sidebar/картка покажуть лише PackID.
func NewRunner(packID, packName, installDir, workerBase, workerToken string,
	client *http.Client, submitter Submitter, state *InstallState, store *StateStore, maxRetries int,
	onProgress ProgressReporter) *Runner {
	return &Runner{
		PackID:       packID,
		PackName:     packName,
		InstallDir:   installDir,
		WorkerBase:   workerBase,
		WorkerToken:  workerToken,
		Client:       client,
		Submitter:    submitter,
		State:        state,
		StateStore:   store,
		MaxRetries:   maxRetries,
		OnProgress:   onProgress,
		mode:         ModeNone,
		partialPaths: map[string]bool{},
	}
}

// Pause запитує м'яку зупинку: активні мережеві читання завершаться на
// найближчій межі буфера (не обриваються посеред байта), .part-файли
// лишаються на диску, InstallState.Stage НЕ змінюється — Run поверне
// ErrPaused, і наступний виклик Run з тим самим Runner/State продовжить
// точно з того місця (черги перебудують SyncPlan і побачать, що частина
// файлів уже done за FileRecord, а недокачані .part підхоплять resume).
//
// Той самий шлях використовується і при panic-recovery/закритті процесу:
// оскільки ми НІКОЛИ не пишемо "cancel" в InstallState сам по собі (лише
// Stage), крах процесу без явного Cancel завжди читається при наступному
// старті як Pause, а не Cancel — незалежно від того, встиг доброчесно
// відпрацювати цей метод чи ні.
func (r *Runner) Pause() {
	atomic.StoreInt32(&r.pauseRequested, 1)
}

// Cancel запитує повне скасування: після зупинки поточних завантажень усі
// часткові файли ЦЬОГО запуску (.part, тимчасові .zip override-архівів,
// уже розпаковані в цьому запуску override-теки) видаляються, а
// InstallState цієї збірки повертається в StageNone (так, ніби синхронізація
// й не починалась) — кнопка в UI повертається на "Встановити".
func (r *Runner) Cancel() {
	atomic.StoreInt32(&r.cancelRequested, 1)
	atomic.StoreInt32(&r.pauseRequested, 1) // Cancel теж зупиняє гарячий цикл негайно
}

func (r *Runner) isPauseRequested() bool  { return atomic.LoadInt32(&r.pauseRequested) == 1 }
func (r *Runner) isCancelRequested() bool { return atomic.LoadInt32(&r.cancelRequested) == 1 }

// ErrPaused — Run повернула цю помилку, бо викликали Pause() (або крах
// був симульований ззовні як graceful-stop). Це НЕ помилка в розумінні
// UI: викликач (app_shaurma_full.go) ловить її окремо й показує кнопку
// "Продовжити", а не банер помилки.
var ErrPaused = fmt.Errorf("синхронізацію призупинено")

// ErrCancelled — Run повернула цю помилку, бо викликали Cancel(). UI
// показує кнопку "Встановити" (стан скинуто повністю).
var ErrCancelled = fmt.Errorf("синхронізацію скасовано")

func (r *Runner) trackPartial(path string) {
	r.partialPathsMu.Lock()
	r.partialPaths[path] = true
	r.partialPathsMu.Unlock()
}

// Run виконує повний цикл: обчислює SyncPlan, розкладає на дві черги з
// лімітами "N/2 і N/2" (максимум(1, maxConcurrentDownloads/2) кожна —
// розділ 1.1 V2), запускає їх паралельно, розпаковує override-folder
// архіви, застосовує onMissing і прибирає осиротілі файли, і зберігає
// InstallState на кожному значущому кроці.
//
// Повертає ErrPaused / ErrCancelled окремо від "справжніх" помилок мережі
// чи хешу — виклик має розрізняти ці три випадки.
func (r *Runner) Run(ctx context.Context, manifest *PackManifest, maxConcurrentDownloads int) error {
	r.setMode(ModeRunning)

	r.setStage(StageManifestFetched)
	r.State.ManifestUpdatedAt = manifest.UpdatedAt
	r.State.ManifestContentHash = manifest.ContentHash
	r.StateStore.Save(r.State)

	plan := BuildPlan(manifest, r.State, r.localExists, r.localHashMatches)

	r.mu.Lock()
	r.bytesTotal = plan.TotalBytes
	r.filesTotal = len(plan.HashModTasks) + len(plan.WorkerTasks) + len(plan.OnMissingTasks)
	r.mu.Unlock()

	if r.filesTotal == 0 {
		// Нічого якати — тільки прибрати осиротілих і позначити Complete.
		r.removeOrphans(plan.OrphansToRemove)
		r.setStage(StageComplete)
		r.StateStore.Save(r.State)
		r.setMode(ModeComplete)
		r.report(true, nil)
		return nil
	}

	r.setStage(StageDownloading)
	r.StateStore.Save(r.State)

	// "5 і 5" — дефолт max(1, N/2) для кожної черги (розділ 1.1 V2).
	hashConc := max(1, maxConcurrentDownloads/2)
	workerConc := max(1, maxConcurrentDownloads-hashConc)

	var wg sync.WaitGroup
	errCh := make(chan error, 2)

	// Кожна черга має свій семафор (max(1, N/2) одночасно) і ділить СПІЛЬНИЙ
	// пул Engine (Submitter) зі стелею N. Раніше тут були прапорці
	// hashDone/workerDone, які нібито мали "перетікати" вільні потоки між
	// чергами, але вони записувались і НІКОЛИ не читались — семафор кожної
	// черги фіксований на своєму conc, тож мертвий код жодного перетікання
	// не давав. Прибрано; реальний паралелізм обмежений спільним пулом.
	runQueue := func(tasks []Task, conc int) error {
		defer wg.Done()
		if len(tasks) == 0 {
			return nil
		}
		sem := make(chan struct{}, conc)
		var qWg sync.WaitGroup
		var firstErr error
		var errMu sync.Mutex

		for _, t := range tasks {
			if r.isPauseRequested() {
				break // м'яко зупиняємось: те, що вже в польоті, донакачується/переривається нижче
			}
			select {
			case <-ctx.Done():
				qWg.Wait()
				return ctx.Err()
			default:
			}

			qWg.Add(1)
			sem <- struct{}{}
			task := t
			r.Submitter.Submit(func() {
				defer qWg.Done()
				defer func() { <-sem }()
				if r.isPauseRequested() {
					return // Pause/Cancel запитано, поки завдання чекало в черзі Submitter'а
				}
				if err := r.downloadTask(ctx, task); err != nil {
					if err == errTaskPaused {
						return
					}
					errMu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					errMu.Unlock()
					return
				}
				r.markTaskComplete(task)
			})
		}
		qWg.Wait()
		return firstErr
	}

	wg.Add(2)
	go func() { errCh <- runQueue(plan.HashModTasks, hashConc) }()
	go func() { errCh <- runQueue(plan.WorkerTasks, workerConc) }()

	// Прогрес-тікер: емітить агрегований % що ~300мс (живить sidebar,
	// картку в "Моїх збірках" і сторінку збірки одночасно) — незалежно від
	// того, яка черга активна. Тікер працює до САМОГО кінця Run (defer
	// close), щоб стадії onMissing/розпаковка/фіналізація теж рухали
	// прогрес-бар — інакше UI «зависав» після завершення всіх качок.
	stopTicker := make(chan struct{})
	go r.progressTicker(stopTicker)
	defer close(stopTicker)

	wg.Wait()

	var runErr error
	for i := 0; i < 2; i++ {
		if e := <-errCh; e != nil && runErr == nil {
			runErr = e
		}
	}

	// ── Скасування: прибрати все часткове й відкотити стан ────────────────
	if r.isCancelRequested() {
		r.cleanupPartials()
		r.setStage(StageNone)
		r.StateStore.Save(r.State)
		r.setMode(ModeCancelled)
		r.report(true, ErrCancelled)
		return ErrCancelled
	}

	// ── Пауза (користувач натиснув "Зупинити", або процес завершується) ──
	if r.isPauseRequested() {
		// Stage лишається StageDownloading — саме так наступний запуск
		// зрозуміє, що це відновлюваний стан (NeedsRecovery()==true), а не
		// щойно почата синхронізація.
		r.StateStore.Save(r.State)
		r.setMode(ModePaused)
		r.report(true, ErrPaused)
		return ErrPaused
	}

	if runErr != nil {
		noInternet := IsNoInternet(runErr)
		atomic.StoreInt32(&r.lastNoInternet, boolToInt32(noInternet))
		r.setMode(ModeError)
		r.report(false, runErr)
		return runErr
	}

	// onMissing — довстановлення файлів, які гравець не мав ще ніколи.
	for _, t := range plan.OnMissingTasks {
		if r.isPauseRequested() {
			r.StateStore.Save(r.State)
			r.setMode(ModePaused)
			r.report(true, ErrPaused)
			return ErrPaused
		}
		r.setCurrentFile(filepath.Base(t.DestRelPath))
		if err := r.downloadTask(ctx, t); err != nil {
			r.setMode(ModeError)
			r.report(false, err)
			return err
		}
		r.markOnMissingComplete(t)
	}

	// Розпаковка override-folder архівів (StageExtracting) — Cancel і тут
	// має сенс (розпаковка на повільному диску може тривати), Pause після
	// цього кроку вже не потрібен: залишок роботи дешевий. Показуємо
	// користувачу, ЩО саме розпаковується (currentFile) — раніше розпаковка
	// йшла «мовчки», і прогрес-бар на 99% виглядав завислим.
	r.setStage(StageExtracting)
	r.StateStore.Save(r.State)
	for _, t := range plan.WorkerTasks {
		if t.IsArchive {
			if r.isCancelRequested() {
				r.cleanupPartials()
				r.setStage(StageNone)
				r.StateStore.Save(r.State)
				r.setMode(ModeCancelled)
				r.report(true, ErrCancelled)
				return ErrCancelled
			}
			// Pause під час розпаковки: зупиняємось ПЕРЕД наступним архівом
			// (поточний архів — синхронний, він дозавершується). Архіви не
			// пишуться в State.Files (markTaskComplete для IsArchive повертає
			// рано), тож наступний запуск переробить їх з нуля — безпечно.
			if r.isPauseRequested() {
				r.StateStore.Save(r.State)
				r.setMode(ModePaused)
				r.report(true, ErrPaused)
				return ErrPaused
			}
			r.setCurrentFile("Розпаковка: " + filepath.Base(t.DestRelPath))
			r.emitProgress(false, nil)
			if err := r.extractOverrideFolder(t); err != nil {
				r.setMode(ModeError)
				r.report(false, err)
				return err
			}
		}
	}

	// Фіналізація: осиротілі файли, StageComplete.
	r.setStage(StageFinalizing)
	r.StateStore.Save(r.State)
	r.setCurrentFile("")
	r.emitProgress(false, nil)
	r.removeOrphans(plan.OrphansToRemove)

	r.setStage(StageComplete)
	r.StateStore.Save(r.State)
	r.setMode(ModeComplete)
	r.report(true, nil)
	return nil
}

func (r *Runner) setMode(m RunMode) {
	r.mu.Lock()
	r.mode = m
	r.mu.Unlock()
}

// setStage записує State.Stage під м'ютексом: emitProgress читає
// State.Stage під r.mu, тож усі записи стадії мають іти тим самим шляхом
// (інакше — data race, який став би реальним тепер, коли тікер працює
// крізь стадії розпаковки/фіналізації).
func (r *Runner) setStage(s Stage) {
	r.mu.Lock()
	r.State.Stage = s
	r.mu.Unlock()
}

// Mode повертає поточний режим (для UI-опитування поза progress-колбеком).
func (r *Runner) Mode() RunMode {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.mode
}

// errTaskPaused — внутрішній сигнал "перервано через Pause/Cancel
// посеред читання файлу", НЕ пробрасується користувачу як firstErr черги
// (бо це не помилка мережі/хешу) — обробляється прямо в колбеку Submit.
var errTaskPaused = fmt.Errorf("перервано (pause/cancel)")

// downloadTask — завантаження одного файлу з retry+backoff+resume, у тому
// ж дусі, що й download.Engine.downloadFile, але узагальнене на Task
// (hashMods з зовнішнім URL і workerFiles з Worker-шляхом однаково).
// Override-folder архіви тимчасово пишуться поруч (.zip.part), а не одразу
// в DestRelPath — розпаковуються окремим кроком (extractOverrideFolder).
func (r *Runner) downloadTask(ctx context.Context, t Task) error {
	fullURL := t.URL
	isWorker := t.Kind != "" // workerFiles завжди мають Kind; hashMods — ""
	if isWorker {
		fullURL = r.WorkerBase + "/" + t.URL
	}

	var destPath string
	if t.IsArchive {
		destPath = filepath.Join(r.InstallDir, ".override-archives", sanitizeName(t.ManifestKey)+".zip")
	} else {
		destPath = filepath.Join(r.InstallDir, t.DestRelPath)
	}
	os.MkdirAll(filepath.Dir(destPath), 0755)

	r.setCurrentFile(filepath.Base(t.DestRelPath))

	tmpPath := destPath + ".part"
	r.trackPartial(tmpPath)
	var existingBytes int64
	if fi, err := os.Stat(tmpPath); err == nil {
		existingBytes = fi.Size()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "ShaurmaLauncher/2.0")
	if isWorker {
		req.Header.Set("X-Shaurma-Token", r.WorkerToken)
	}
	if existingBytes > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", existingBytes))
	}

	retries := r.MaxRetries
	if retries <= 0 {
		retries = 6
	}

	var resp *http.Response
	lastStatus := 0
	for attempt := 0; attempt < retries; attempt++ {
		if r.isPauseRequested() {
			return errTaskPaused
		}
		if attempt > 0 {
			backoff := time.Duration(math.Min(
				float64(backoffBase)*math.Pow(2, float64(attempt-1)),
				float64(backoffMax)))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}
		resp, err = r.Client.Do(req)
		if err != nil {
			err = classifyNetErr(err)
			if IsNoInternet(err) {
				return err // мережі взагалі немає — не сенс ретраїти мовчки далі
			}
			continue
		}
		lastStatus = resp.StatusCode
		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent {
			break
		}
		resp.Body.Close()
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			return fmt.Errorf("HTTP %d: %s", resp.StatusCode, fullURL)
		}
	}
	if err != nil {
		return fmt.Errorf("завантаження не вдалось після %d спроб: %w", retries, err)
	}
	if resp == nil {
		return fmt.Errorf("немає відповіді від сервера: %s", fullURL)
	}
	// Усі спроби вичерпано, але 2xx так і не прийшов (наприклад, весь час
	// 5xx) — НЕ читаємо тіло помилки як файл: це писало б HTML-сторінку
	// помилки замість мода і «тихо» псувало файл (SHA-перевірка не встигала
	// б спрацювати для файлів без хешу в маніфесті).
	if lastStatus != 0 && lastStatus != http.StatusOK && lastStatus != http.StatusPartialContent {
		resp.Body.Close()
		return fmt.Errorf("HTTP %d після %d спроб: %s", lastStatus, retries, fullURL)
	}
	defer resp.Body.Close()

	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}

	hasher1 := sha1.New()
	hasher256 := sha256.New()
	if existingBytes > 0 {
		if rf, err := os.Open(tmpPath); err == nil {
			io.Copy(io.MultiWriter(hasher1, hasher256), rf)
			rf.Close()
		}
	}

	buf := make([]byte, 65536)
	for {
		if r.isPauseRequested() {
			f.Close()
			return errTaskPaused
		}
		select {
		case <-ctx.Done():
			f.Close()
			return ctx.Err()
		default:
		}
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			f.Write(buf[:n])
			hasher1.Write(buf[:n])
			hasher256.Write(buf[:n])
			atomic.AddInt64(&r.bytesDone, int64(n))
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			f.Close()
			return classifyNetErr(readErr)
		}
	}
	f.Close()

	// Верифікація хешу — ЗАВЖДИ коли сервер його дає, навіть для файлу,
	// докачаного з .part (розділ 3.5, Рівень 1 V1-документа: .part міг
	// пошкодитись і при цьому випадково збігтись розміром).
	if t.SHA1 != "" {
		got := fmt.Sprintf("%x", hasher1.Sum(nil))
		if got != t.SHA1 {
			os.Remove(tmpPath)
			return fmt.Errorf("SHA1 mismatch для %s: очікував %s, отримав %s", t.DestRelPath, t.SHA1, got)
		}
	}
	if t.SHA256 != "" {
		got := fmt.Sprintf("%x", hasher256.Sum(nil))
		if got != t.SHA256 {
			os.Remove(tmpPath)
			return fmt.Errorf("SHA256 mismatch для %s: очікував %s, отримав %s", t.DestRelPath, t.SHA256, got)
		}
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		return err
	}
	atomic.AddInt32(&r.filesDone, 1)
	return nil
}

// extractOverrideFolder розпаковує завантажений .zip override-теки
// (розділ 3.3 V2 — "bubble mod") у DestRelPath, ПОВНІСТЮ замінюючи вміст
// теки (аналог force_archive зі старої схеми). Якщо теки не було раніше —
// вона трекається як partial, щоб Cancel міг прибрати саме її.
func (r *Runner) extractOverrideFolder(t Task) error {
	archivePath := filepath.Join(r.InstallDir, ".override-archives", sanitizeName(t.ManifestKey)+".zip")
	destDir := filepath.Join(r.InstallDir, t.DestRelPath)

	preExisted := r.localExists(t.DestRelPath)

	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("відкриття архіву %s: %w", t.DestRelPath, err)
	}
	defer zr.Close()

	os.RemoveAll(destDir)
	os.MkdirAll(destDir, 0755)
	if !preExisted {
		r.trackPartial(destDir)
	}

	for _, f := range zr.File {
		target := filepath.Join(destDir, f.Name)
		if f.FileInfo().IsDir() {
			os.MkdirAll(target, 0755)
			continue
		}
		os.MkdirAll(filepath.Dir(target), 0755)
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(target)
		if err != nil {
			rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, rc)
		rc.Close()
		out.Close()
		if copyErr != nil {
			return copyErr
		}
	}

	os.Remove(archivePath)
	return nil
}

// cleanupPartials видаляє ВСІ файли/теки, які цей конкретний запуск Run
// створив (трекнуті через trackPartial) — викликається лише при Cancel,
// НІКОЛИ при Pause. Файли, що існували на диску ДО старту синхронізації
// (наприклад, override-тека, яку успішно розпакували в попередньому
// запуску і зараз лише перевіряємо на дельту), не чіпаються.
func (r *Runner) cleanupPartials() {
	r.partialPathsMu.Lock()
	paths := make([]string, 0, len(r.partialPaths))
	for p := range r.partialPaths {
		paths = append(paths, p)
	}
	r.partialPathsMu.Unlock()

	for _, p := range paths {
		os.RemoveAll(p)
	}
	os.Remove(filepath.Join(r.InstallDir, ".override-archives"))
}

func (r *Runner) markTaskComplete(t Task) {
	if t.IsArchive {
		return // FileRecord пишеться нижче спільно, після розпаковки не потрібно окремо
	}
	sha := t.SHA256
	if sha == "" {
		sha = t.SHA1
	}
	r.mu.Lock()
	r.State.Files[t.ManifestKey] = FileRecord{
		Path:      t.ManifestKey,
		SHA:       sha,
		UpdatedAt: t.UpdatedAt,
	}
	// Save під r.mu: кілька черг завершують файли паралельно, а Save читає
	// карту State.Files — запис іншої горутини в ту саму карту (під тим же
	// r.mu) не має перетинатись із читанням поза локом (конкурентний
	// read+write по Go-map — паніка).
	r.StateStore.Save(r.State)
	r.mu.Unlock()
}

func (r *Runner) markOnMissingComplete(t Task) {
	r.mu.Lock()
	r.State.InstalledOnMissing[t.DestRelPath] = true
	r.mu.Unlock()
	r.StateStore.Save(r.State)
}

func (r *Runner) removeOrphans(paths []string) {
	for _, p := range paths {
		full := filepath.Join(r.InstallDir, p)
		os.Remove(full)
		r.mu.Lock()
		delete(r.State.Files, p)
		r.mu.Unlock()
	}
	r.StateStore.Save(r.State)
}

func (r *Runner) localExists(destRelPath string) bool {
	_, err := os.Stat(filepath.Join(r.InstallDir, destRelPath))
	return err == nil
}

func (r *Runner) localHashMatches(destRelPath, expectedHash string) bool {
	f, err := os.Open(filepath.Join(r.InstallDir, destRelPath))
	if err != nil {
		return false
	}
	defer f.Close()
	// Довжина expectedHash визначає алгоритм: 40 hex = sha1, 64 hex = sha256.
	if len(expectedHash) == 40 {
		h := sha1.New()
		io.Copy(h, f)
		return fmt.Sprintf("%x", h.Sum(nil)) == expectedHash
	}
	h := sha256.New()
	io.Copy(h, f)
	return fmt.Sprintf("%x", h.Sum(nil)) == expectedHash
}

func (r *Runner) setCurrentFile(name string) {
	r.mu.Lock()
	r.currentFile = name
	r.mu.Unlock()
}

func (r *Runner) progressTicker(stop chan struct{}) {
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			r.emitProgress(false, nil)
			return
		case <-ticker.C:
			r.emitProgress(false, nil)
		}
	}
}

func (r *Runner) report(done bool, err error) {
	r.emitProgress(done, err)
}

func (r *Runner) emitProgress(done bool, err error) {
	if r.OnProgress == nil {
		return
	}
	r.mu.Lock()
	now := time.Now()
	bytesDone := atomic.LoadInt64(&r.bytesDone)
	elapsed := now.Sub(r.speedLastCheck).Seconds()
	if elapsed >= 0.5 {
		delta := bytesDone - r.speedLastBytes
		r.speedBPS = float64(delta) / elapsed
		r.speedLastBytes = bytesDone
		r.speedLastCheck = now
	}
	p := RunnerProgress{
		PackID:          r.PackID,
		PackName:        r.PackName,
		Mode:            r.mode,
		FilesTotal:      r.filesTotal,
		FilesCompleted:  int(atomic.LoadInt32(&r.filesDone)),
		BytesTotal:      r.bytesTotal,
		BytesDownloaded: bytesDone,
		CurrentFile:     r.currentFile,
		SpeedBPS:        r.speedBPS,
		Stage:           r.State.Stage,
		NoInternet:      atomic.LoadInt32(&r.lastNoInternet) == 1,
		Done:            done,
		Error:           err,
	}
	if err != nil {
		p.ErrorMsg = err.Error()
	}
	if p.BytesTotal > 0 {
		p.OverallPercent = int(p.BytesDownloaded * 100 / p.BytesTotal)
		if p.OverallPercent > 100 {
			p.OverallPercent = 100
		}
	} else if p.FilesTotal > 0 {
		p.OverallPercent = p.FilesCompleted * 100 / p.FilesTotal
	}
	// Ніколи не показуємо 100% до фактичного завершення сесії: bytesTotal з
	// маніфесту може бути неточним (розміри ≠ реальні), тож байтовий %
	// здатен дістати 100% заздалегідь, поки останні файли ще качаються чи
	// розпаковуються — саме це виглядало як «зависло на 100%».
	if !done && p.OverallPercent >= 100 {
		p.OverallPercent = 99
	}
	r.mu.Unlock()
	r.OnProgress(p)
}

func boolToInt32(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

func sanitizeName(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' {
			out = append(out, r)
		} else {
			out = append(out, '_')
		}
	}
	return string(out)
}
