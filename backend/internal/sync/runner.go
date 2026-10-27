package sync

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"shaurma-launcher-backend/internal/crashguard"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"shaurma-launcher-backend/internal/launchlog"
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

// SyncReport — підсумок ОДНІЄЇ сесії Run: що саме лаунчер оновив/пропустив/
// видалив на диску. НЕ персиститься (на відміну від InstallState) — живе
// лише в пам'яті Runner-а протягом сесії й повертається викликачу після
// завершення. Кожен запис тут ЗАВЖДИ дублюється в internal/launchlog
// (logs/launcher.log, компонент "Sync") у момент події — тому навіть якщо
// UI ніколи не прочитає SyncReport, повний слід лишається на диску і
// прикладається до звернень у підтримку.
type SyncReport struct {
	// UpdatedFiles — файли (hash-моди, override-файли, bundled-моди),
	// які реально завантажились/перезаписались цього разу.
	UpdatedFiles []string `json:"updatedFiles"`
	// UpdatedFolders — override-теки (KindOverrideFolder, force_archive),
	// які реально РОЗПАКУВАЛИСЬ (перезаписані) цього разу.
	UpdatedFolders []string `json:"updatedFolders"`
	// SkippedFolders — override-теки з правилом on_missing/
	// first_install_only, які лаунчер СВІДОМО НЕ ЧІПАВ, бо вони вже
	// існують на диску — саме той запис, що прямо підтверджує чи
	// спрацювало правило "не оновлювати" для config/ тощо.
	SkippedFolders []string `json:"skippedFolders"`
	// RemovedFiles — осиротілі файли, видалені як зайві (їх більше нема
	// в маніфесті збірки, і вони не в білому списку).
	RemovedFiles []string `json:"removedFiles"`
}

func (r *Runner) logUpdatedFile(path string) {
	r.syncLogMu.Lock()
	r.syncLog.UpdatedFiles = append(r.syncLog.UpdatedFiles, path)
	r.syncLogMu.Unlock()
	launchlog.Info("Sync", "[%s] оновлено файл: %s", r.PackID, path)
}

func (r *Runner) logUpdatedFolder(path string) {
	r.syncLogMu.Lock()
	r.syncLog.UpdatedFolders = append(r.syncLog.UpdatedFolders, path)
	r.syncLogMu.Unlock()
	launchlog.Info("Sync", "[%s] оновлено теку: %s", r.PackID, path)
}

func (r *Runner) logSkippedFolder(path, reason string) {
	r.syncLogMu.Lock()
	r.syncLog.SkippedFolders = append(r.syncLog.SkippedFolders, path)
	r.syncLogMu.Unlock()
	launchlog.Info("Sync", "[%s] НЕ чіпаю теку (%s): %s", r.PackID, reason, path)
}

func (r *Runner) logRemovedFile(path string) {
	r.syncLogMu.Lock()
	r.syncLog.RemovedFiles = append(r.syncLog.RemovedFiles, path)
	r.syncLogMu.Unlock()
	launchlog.Info("Sync", "[%s] видалено осиротілий файл: %s", r.PackID, path)
}

// Report повертає копію накопиченого звіту цієї сесії (безпечно для
// виклику як під час Run, так і після завершення).
func (r *Runner) Report() SyncReport {
	r.syncLogMu.Lock()
	defer r.syncLogMu.Unlock()
	return SyncReport{
		UpdatedFiles:   append([]string(nil), r.syncLog.UpdatedFiles...),
		UpdatedFolders: append([]string(nil), r.syncLog.UpdatedFolders...),
		SkippedFolders: append([]string(nil), r.syncLog.SkippedFolders...),
		RemovedFiles:   append([]string(nil), r.syncLog.RemovedFiles...),
	}
}

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

	// syncLog — звіт "що саме змінилось на диску" за цю сесію синку: які
	// файли якались/оновлювались, які override-теки розпаковувались (і
	// які, навпаки, ПРОПУЩЕНІ бо вже існують — on_missing/
	// first_install_only, саме те розрізнення, яке важко перевірити
	// "на око"), і які осиротілі файли видалені. Пишеться і в
	// internal/launchlog (logs/launcher.log, компонент "Sync"), і
	// зберігається тут же, щоб її можна було віддати викликачу після
	// завершення Run (наприклад для показу у консолі встановлення).
	syncLogMu sync.Mutex
	syncLog   SyncReport
	// actuallyDownloaded — ManifestKey задач, які цього запуску РЕАЛЬНО
	// якались з мережі (на відміну від тих, що downloadTask пропустив
	// раннім виходом за збігом хешу — файл і так вже актуальний на
	// диску). markTaskComplete звіряється з цією мапою, щоб у
	// SyncReport/лог потрапляли лише файли, які справді змінились, а не
	// кожен файл плану (інакше "оновлено" писалось би навіть коли
	// локальний файл не чіпався взагалі — оманливий звіт).
	actuallyDownloaded   map[string]bool
	actuallyDownloadedMu sync.Mutex

	// IsWhitelisted — перевірка «чи дозволено лишити цей jar у mods/,
	// навіть якщо його немає в маніфесті збірки» (за іменем файлу —
	// сам колбек читає mod-id ЗСЕРЕДИНИ jar і звіряє з білим списком,
	// buildPlanWithModsAndWhitelist нічого не знає про формат jar).
	// nil — білий список не завантажено/порожній: поведінка як була
	// (строге видалення без винятків). Встановлюється ЗЗОВНІ (як
	// OnProgress у sync.Manager.Start) — Runner сам мережу за білим
	// списком не тягне, лише застосовує вже завантажений результат.
	IsWhitelisted func(fileName string) bool

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

	// hashCache — кеш "хеш файлу за (шлях, розмір, mtime)", щоб
	// localHashMatches не перечитувала й не перехешовувала jar-файли, які
	// не змінювались з минулого разу (див. filehashcache.go). Лінива
	// ініціалізація через hashCacheOf() — Runner можна створювати і в
	// тестах, де InstallDir ще невідомий/тимчасовий.
	hashCacheMu sync.Mutex
	hashCache   *fileHashCache
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

	// Кеш хешів (filehashcache.go) накопичує зміни в пам'яті протягом
	// усього Run (buildPlanWithModsAndWhitelist нижче й downloadTask під
	// час якань — усі звертання йдуть через localHashMatches) і
	// зберігається ОДНИМ атомарним записом тут, незалежно від того, чим
	// закінчився прогін (успіх/помилка/Pause/Cancel) — навіть перервана
	// синхронізація встигла порахувати частину хешів, і ці дані варто
	// зберегти для наступного разу.
	defer r.FlushHashCache()

	r.setStage(StageManifestFetched)
	r.State.ManifestUpdatedAt = manifest.UpdatedAt
	r.State.ManifestContentHash = manifest.ContentHash
	r.StateStore.Save(r.State)

	plan := buildPlanWithModsAndWhitelist(manifest, r.State, r.localExists, r.localHashMatches, r.listLocalMods, r.IsWhitelisted)

	// Логуємо ОДРАЗУ, до якання: які on_missing-теки (config/ тощо) цей
	// прогін свідомо НЕ чіпає, бо вони вже на диску — правило спрацювало.
	// Записуємо це до старту черг, а не постфактум, щоб лог був і при
	// Pause/Cancel/помилці посеред синку (не лише при повному Complete).
	for _, path := range plan.SkippedOnMissingFolders {
		r.logSkippedFolder(path, "on_missing/first_install_only — вже на диску")
	}

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
	{
		_f, _a0 := r.progressTicker, stopTicker
		go func() {
			defer crashguard.Recover("r.progressTicker")
			_f(_a0)
		}()
	}
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

	// onMissing — довстановлення файлів/тек, які гравець не мав ще ніколи.
	// Для OnMissingFolders (IsArchive=true) після качки .zip одразу
	// розпаковуємо теку (регресія 2026-08-09: лаунчер не читав
	// manifest.OnMissingFolders взагалі, теки config/ не встановлювались).
	for _, t := range plan.OnMissingTasks {
		if r.isCancelRequested() {
			// Cancel під час onMissing-фази має поводитись як Cancel (прибрати
			// часткові файли), а не як Pause — інакше .part/щойно розпаковані
			// onMissing-теки лишались би на диску, а кнопка показувала б
			// «Продовжити». Той самий порядок, що в фазі розпаковки нижче.
			r.cleanupPartials()
			r.setStage(StageNone)
			r.StateStore.Save(r.State)
			r.setMode(ModeCancelled)
			r.report(true, ErrCancelled)
			return ErrCancelled
		}
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
		if t.IsArchive {
			r.setCurrentFile("Розпаковка: " + filepath.Base(t.DestRelPath))
			r.emitProgress(false, nil)
			if err := r.extractOverrideFolder(t); err != nil {
				r.setMode(ModeError)
				r.report(false, err)
				return err
			}
			// Архів розпаковано успішно — трекаємо у State.Files, щоб
			// наступний запуск не качав його знову (навіть якщо InstalledOnMissing
			// випадково скинеться).
			r.markArchiveComplete(t)
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
			// Розпаковано успішно — трекаємо архів у State.Files, щоб
			// наступний запуск не качав його знову.
			r.markArchiveComplete(t)
		}
	}

	// Тимчасова тека з .zip архівами більше не потрібна (всі розпаковані
	// та видалені) — прибираємо, щоб не лишати сміття в теці збірки.
	os.RemoveAll(filepath.Join(r.InstallDir, ".override-archives"))

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

// syncStallTimeout — скільки часу sync-завантаження може мовчати (жодного
// байта) перед тим, як downloadTask вважає з'єднання завислим і перезапускає
// файл з місця зупинки (Range + .part). Раніше завмерлий CDN тримав
// заблокований Read ВІЧНО (client без Timeout), і пауза/скасування не могли
// його розірвати — «швидкість 0.0 МБ/с, кнопки не працюють». var (не const):
// тест зменшує для швидкої перевірки.
var syncStallTimeout = 15 * time.Second

// pauseStallReader — обгортка resp.Body: закриває тіло, коли байти не йдуть
// syncStallTimeout (стіл з'єднання), АБО коли stopped() вертає true
// (запитано паузу/скасування). Закриття тіла розблоковує завислий Read —
// саме це робить паузу/скасування робочими навіть на завмерлому CDN
// (раніше прапорець перевірявся лише МІЖ читаннями і не міг розірвати
// блокований Read).
type pauseStallReader struct {
	r       io.ReadCloser
	mu      sync.Mutex
	timer   *time.Timer
	timeout time.Duration
	closed  bool
	done    chan struct{}
	once    sync.Once
}

func newPauseStallReader(ctx context.Context, stopped func() bool, r io.ReadCloser, timeout time.Duration) *pauseStallReader {
	s := &pauseStallReader{r: r, timeout: timeout, done: make(chan struct{})}
	s.timer = time.AfterFunc(timeout, s.closeBody)
	go func() {
		defer crashguard.Recover("runner.go:632")
		t := time.NewTicker(150 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				s.closeBody()
				return
			case <-s.done:
				return
			case <-t.C:
				if stopped != nil && stopped() {
					s.closeBody()
					return
				}
			}
		}
	}()
	return s
}

func (s *pauseStallReader) closeBody() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	s.r.Close()
}

func (s *pauseStallReader) Read(b []byte) (int, error) {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return 0, io.ErrClosedPipe
	}
	n, err := s.r.Read(b)
	if n > 0 {
		// Reset під м'ютексом і лише коли тіло ще відкрите: конкурувати з
		// таймером-колбеком (closeBody) заборонено за документацією Go
		// (Timer.Reset не можна кликати, поки таймер уже спрацював), а
		// найгірший випадок тут — зайвий closeBody, який Read перетворить
		// на retryable-обрив. Прапорець closed під тим самим м'ютексом, що
		// й closeBody, виключає reset «після» закриття.
		s.mu.Lock()
		if !s.closed {
			s.timer.Reset(s.timeout)
		}
		s.mu.Unlock()
	}
	return n, err
}

func (s *pauseStallReader) Close() error {
	s.timer.Stop()
	s.once.Do(func() { close(s.done) })
	return s.r.Close()
}

// downloadTask — завантаження одного файлу з retry+backoff+resume, у тому
// ж дусі, що й download.Engine.downloadFile, але узагальнене на Task
// (hashMods з зовнішнім URL і workerFiles з Worker-шляхом однаково).
// Override-folder архіви тимчасово пишуться поруч (.zip.part), а не одразу
// в DestRelPath — розпаковуються окремим кроком (extractOverrideFolder).
//
// Стіл з'єднання (0 байт за syncStallTimeout) і обрив читання тепер
// RETRYABLE: спроба завершується зі збереженням .part, наступна докачує з
// місця зупинки через Range. Пауза/скасування (Pause()/Cancel()) розриває
// навіть заблокований Read через pauseStallReader.
func (r *Runner) downloadTask(ctx context.Context, t Task) error {
	fullURL := t.URL
	isWorker := t.Kind != "" // workerFiles завжди мають Kind; hashMods — ""
	if isWorker {
		fullURL = r.WorkerBase + "/" + t.URL
	}

	// ── Захист завантаження: нічому з маніфесту не довіряємо наосліп ──
	if !isWorker {
		if err := checkDownloadURL(fullURL); err != nil {
			return fmt.Errorf("%s: %w", t.DestRelPath, err)
		}
	}
	if RequireHashes && !hasAnyHash(t) {
		return fmt.Errorf("%s: у маніфесті немає хешу — файл без підтвердження не завантажується", t.DestRelPath)
	}
	var destPath string
	if t.IsArchive {
		// Куди розпаковувати — теж із маніфесту: перевіряємо ДО качання.
		if _, err := safeJoinDir(r.InstallDir, t.DestRelPath); err != nil {
			return err
		}
		destPath = filepath.Join(r.InstallDir, ".override-archives", sanitizeName(t.ManifestKey)+".zip")
	} else {
		p, err := SafeJoin(r.InstallDir, t.DestRelPath)
		if err != nil {
			return err
		}
		destPath = p
	}
	os.MkdirAll(filepath.Dir(destPath), 0755)
	httpClient := clientWithSafeRedirects(r.Client)

	// Resume/повторний запуск: файл уже ПОВНІСТЮ на диску (destPath існує і
	// збігається за хешем з маніфестом) — НЕ качаємо знову. Раніше
	// пауза→продовження (і крах між завершенням качки і записом у State)
	// ПЕРЕкачувало вже готові файли з нуля: .part немає (перейменований у
	// destPath), тож перша ж спроба стартувала з Range=0. Для архівів
	// (.override-archives/*.zip) це ще й рятує від повторного качання zip-а,
	// якщо розпаковка перервалась — архів лишається на диску до успіху
	// (див. extractOverrideFolder), і downloadTask пропускає його за хешем.
	if fi, err := os.Stat(destPath); err == nil && !fi.IsDir() && fi.Size() > 0 {
		if (t.SHA256 != "" && fileMatchesSHA256(destPath, t.SHA256)) ||
			(t.SHA256 == "" && t.SHA1 != "" && fileMatchesSHA1(destPath, t.SHA1)) {
			atomic.AddInt32(&r.filesDone, 1)
			return nil
		}
	}

	r.setCurrentFile(filepath.Base(t.DestRelPath))

	tmpPath := destPath + ".part"
	r.trackPartial(tmpPath)

	retries := r.MaxRetries
	if retries <= 0 {
		retries = 6
	}

	var lastErr error
	for attempt := 0; attempt < retries; attempt++ {
		if r.isPauseRequested() {
			return errTaskPaused
		}
		if ctx.Err() != nil {
			return ctx.Err()
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

		resp, err := httpClient.Do(req)
		if err != nil {
			if r.isPauseRequested() {
				return errTaskPaused
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			err = classifyNetErr(err)
			if IsNoInternet(err) {
				return err // мережі взагалі немає — не сенс ретраїти мовчки далі
			}
			lastErr = err
			continue
		}

		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent {
			err = r.writeDownloadedBody(ctx, resp, tmpPath, destPath, t, existingBytes)
			resp.Body.Close()
			if err == nil {
				r.markActuallyDownloaded(t.ManifestKey)
				return nil
			}
			if err == errTaskPaused {
				return errTaskPaused
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// Стіл/обрив читання — retryable: .part лишається, Range докачає.
			lastErr = err
			continue
		}

		// 416: Range неприйнятний. Реальний сценарій з retry-циклом: попередня
		// спроба СКАЧАЛА ВЕСЬ файл у .part і впала до перейменування — тоді
		// 416 означає «файл уже повний»: перевіряємо SHA1 і перейменовуємо.
		if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
			resp.Body.Close()
			if t.SHA1 != "" && fileExists(tmpPath) && fileMatchesSHA1(tmpPath, t.SHA1) {
				if err := os.Rename(tmpPath, destPath); err == nil {
					atomic.AddInt32(&r.filesDone, 1)
					r.markActuallyDownloaded(t.ManifestKey)
					return nil
				}
			}
			os.Remove(tmpPath)
			lastErr = fmt.Errorf("Range-запит не задоволено (HTTP 416)")
			continue
		}

		lastStatus := resp.StatusCode
		resp.Body.Close()
		if lastStatus >= 400 && lastStatus < 500 {
			return fmt.Errorf("HTTP %d: %s", lastStatus, fullURL)
		}
		lastErr = fmt.Errorf("HTTP %d: %s", lastStatus, fullURL)
	}
	return fmt.Errorf("завантаження не вдалось після %d спроб: %w", retries, lastErr)
}

// writeDownloadedBody — читання тіла відповіді (через pauseStallReader) і
// запис у .part з верифікацією хешу. Повертає nil при успіху (файл уже
// перейменований у destPath), errTaskPaused при паузі/скасуванні, або
// retryable-помилку при стілі/обриві (викликач докачує через Range).
func (r *Runner) writeDownloadedBody(ctx context.Context, resp *http.Response, tmpPath, destPath string, t Task, existingBytes int64) error {
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	// Сервер проігнорував Range (200 замість 206) — віддає повний контент:
	// стираємо старий .part, щоб не лишити хвіст старого вмісту.
	if resp.StatusCode == http.StatusOK && existingBytes > 0 {
		// Windows: f.Truncate(0) через відкритий O_APPEND-хендл падає з
		// "Access is denied" — append-хендл має лише FILE_APPEND_DATA, без
		// права SetEndOfFile, тож truncate неможливий. Раніше докачка файлу
		// після стілу (сервер відповів 200 замість 206) вмирала саме тут на
		// Windows: «завантаження не вдалось після N спроб: truncate ...
		// Access is denied» — і виглядало як зависання на одному моді.
		f.Close()
		if err := os.Truncate(tmpPath, 0); err != nil {
			return err
		}
		f, err = os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return err
		}
		existingBytes = 0
	}

	hasher1 := sha1.New()
	hasher256 := sha256.New()
	hasher512 := sha512.New()
	if existingBytes > 0 {
		if rf, err := os.Open(tmpPath); err == nil {
			io.Copy(io.MultiWriter(hasher1, hasher256, hasher512), rf)
			rf.Close()
		}
	}
	// Стеля розміру: файл не може бути більшим за заявлений у маніфесті
	// (захист від нескінченного потоку/забивання диска).
	sizeCap := maxAllowedBytes(t)
	written := existingBytes

	// stall/pause-обгортка: закриває тіло, коли байти не йдуть syncStallTimeout
	// АБО запитано паузу/скасування — заблокований Read розблоковується.
	body := newPauseStallReader(ctx, func() bool { return r.isPauseRequested() || r.isCancelRequested() }, resp.Body, syncStallTimeout)
	defer body.Close()

	// ПРИСКОРЕННЯ: буферизований запис замість прямого f.Write на кожен
	// 64КБ-шматок. Без bufio кожен виклик f.Write — окремий syscall
	// (write(2)/WriteFile), і при десятках паралельних завантажень (пул
	// Engine — до maxConcurrentDownloads одночасно) це помітне
	// навантаження на диск/ОС, особливо на HDD, мережевих теках чи під
	// антивірусом. bufio.Writer накопичує 256КБ у пам'яті користувацького
	// процесу і скидає на диск рідше — менше syscall на той самий обсяг
	// даних. Розмір хешу/перевірка/Rename нижче — без змін, лише сам
	// запис проходить через буфер; bw.Flush() перед f.Close() гарантує,
	// що жоден байт не лишиться "застряглим" у буфері непереданим на диск.
	bw := bufio.NewWriterSize(f, 256*1024)

	buf := make([]byte, 65536)
	for {
		n, readErr := body.Read(buf)
		if n > 0 {
			written += int64(n)
			if written > sizeCap {
				f.Close()
				os.Remove(tmpPath)
				return fmt.Errorf("%s: розмір перевищує заявлений у маніфесті (%d байт)", t.DestRelPath, sizeCap)
			}
			bw.Write(buf[:n])
			hasher1.Write(buf[:n])
			hasher256.Write(buf[:n])
			hasher512.Write(buf[:n])
			atomic.AddInt64(&r.bytesDone, int64(n))
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			f.Close()
			if r.isPauseRequested() || r.isCancelRequested() {
				return errTaskPaused
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// Стіл/обрив — retryable (Range-резюме підхопить .part).
			return fmt.Errorf("обрив під час завантаження: %w", classifyNetErr(readErr))
		}
	}
	if err := bw.Flush(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	// Верифікація хешу — ЗАВЖДИ коли сервер його дає, навіть для файлу,
	// докачаного з .part (розділ 3.5, Рівень 1 V1-документа: .part міг
	// пошкодитись і при цьому випадково збігтись розміром).
	if t.SHA512 != "" {
		got := fmt.Sprintf("%x", hasher512.Sum(nil))
		if !strings.EqualFold(got, t.SHA512) {
			os.Remove(tmpPath)
			return fmt.Errorf("SHA512 mismatch для %s", t.DestRelPath)
		}
	}
	if t.SHA1 != "" {
		got := fmt.Sprintf("%x", hasher1.Sum(nil))
		if !strings.EqualFold(got, t.SHA1) {
			os.Remove(tmpPath)
			return fmt.Errorf("SHA1 mismatch для %s: очікував %s, отримав %s", t.DestRelPath, t.SHA1, got)
		}
	}
	if t.SHA256 != "" {
		got := fmt.Sprintf("%x", hasher256.Sum(nil))
		if !strings.EqualFold(got, t.SHA256) {
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
	// destDir іде в os.RemoveAll нижче — ОБОВ'ЯЗКОВО всередині теки збірки й не
	// сама тека збірки.
	destDir, err := safeJoinDir(r.InstallDir, t.DestRelPath)
	if err != nil {
		return err
	}

	// Архів видаляємо ЛИШЕ при успішній розпаковці. Раніше `defer os.Remove`
	// (реєструвався одразу) стирав zip навіть коли розпаковка падала — тоді
	// resume був змушений КАЧАТИ архів знову (стан не записаний, файла немає).
	// Тепер при помилці/перериванні zip лишається у .override-archives:
	// наступний запуск пропустить качку (downloadTask: destPath збігається за
	// хешем) і просто розпакує заново. Deferred-видалення виконується ПІСЛЯ
	// zr.Close() (LIFO: zr.Close реєструється пізніше) — на Windows відкритий
	// файл не можна видалити, тож порядок обов'язковий.
	ok := false
	defer func() {
		if ok {
			os.Remove(archivePath)
		}
	}()

	preExisted := r.localExists(t.DestRelPath)

	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("відкриття архіву %s: %w", t.DestRelPath, err)
	}
	defer zr.Close()

	// Перевіряємо ВЕСЬ архів до першого запису на диск: zip-slip, виконувані
	// файли, «бомба» за кількістю/розміром.
	if err := vetZipEntries(zr.File); err != nil {
		return fmt.Errorf("архів %s відхилено: %w", t.DestRelPath, err)
	}

	// Resume: чи цей архів УЖЕ розпакований повністю (всі записи є на диску
	// з тим самим розміром)? Раніше повторна розпаковка (пауза→продовження,
	// крах між розпаковкою і записом у State) СТИРАЛА destDir і робила все
	// заново — «повертає до розпакування, хоча все вже розпаковано».
	alreadyComplete := true
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		fi, err := os.Stat(filepath.Join(destDir, filepath.FromSlash(f.Name)))
		if err != nil || fi.IsDir() || fi.Size() != int64(f.UncompressedSize64) {
			alreadyComplete = false
			break
		}
	}
	if alreadyComplete {
		ok = true // розпаковано раніше — архів більше не потрібен
		return nil
	}

	os.RemoveAll(destDir)
	os.MkdirAll(destDir, 0755)
	if !preExisted {
		r.trackPartial(destDir)
	}

	// ПРИСКОРЕННЯ: розпаковка файлів архіву ПАРАЛЕЛЬНО замість одного
	// файлу за раз. Override-архіви (config/, resourcepacks/ тощо) часто
	// містять сотні дрібних файлів — послідовний io.Copy файл-за-файлом
	// не використовує паралелізм диска/CPU (кожен io.Copy — окремий
	// syscall-по-syscall цикл, чекаємо його завершення, і лише тоді
	// відкриваємо наступний файл). Розпаковка одного архіву — окрема,
	// самодостатня дія (див. коментар вище про DEFER-порядок і "ok"): тут
	// НЕ чіпаємо семафор/пул Engine (той призначений для мережевих
	// завантажень), а заводимо власний невеликий пул горутин, обмежений
	// runtime.NumCPU() — розпаковка вперлася б у диск/CPU куди раніше, ніж
	// у мережевий ліміт maxConcurrentDownloads.
	extractConc := runtime.NumCPU()
	if extractConc < 2 {
		extractConc = 2
	}
	if extractConc > 8 {
		extractConc = 8 // не сенсу плодити більше воркерів, ніж встигає диск
	}
	sem := make(chan struct{}, extractConc)
	var ewg sync.WaitGroup
	var errMu sync.Mutex
	var firstErr error

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			target, _ := SafeJoin(destDir, f.Name) // вже перевірено vetZipEntries
			if target != "" {
				os.MkdirAll(target, 0755)
			}
			continue
		}
		// Директорію призначення файлу створюємо ДО запуску горутини —
		// MkdirAll для того самого шляху з кількох горутин безпечний
		// (os.MkdirAll ідемпотентний), але так простіше гарантувати, що
		// тека вже існує до першого os.Create нижче.
		if isBlockedEntry(f.Name) {
			continue // .exe/.bat/.ps1 тощо зі збірки не розпаковуємо
		}
		target, terr := SafeJoin(destDir, f.Name)
		if terr != nil {
			errMu.Lock()
			if firstErr == nil {
				firstErr = terr
			}
			errMu.Unlock()
			break
		}
		os.MkdirAll(filepath.Dir(target), 0755)

		zf := f
		sem <- struct{}{}
		ewg.Add(1)
		go func() {
			defer crashguard.Recover("runner.go:1049")
			defer ewg.Done()
			defer func() { <-sem }()

			errMu.Lock()
			failed := firstErr != nil
			errMu.Unlock()
			if failed {
				return // хтось інший уже провалився — не сенс качати далі
			}

			rc, err := zf.Open()
			if err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
				return
			}
			out, err := os.Create(target)
			if err != nil {
				rc.Close()
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
				return
			}
			_, copyErr := io.Copy(out, io.LimitReader(rc, maxExtractFileBytes))
			rc.Close()
			out.Close()
			if copyErr != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = copyErr
				}
				errMu.Unlock()
			}
		}()
	}
	ewg.Wait()
	if firstErr != nil {
		return firstErr
	}

	ok = true
	// Тека РЕАЛЬНО перезаписана вмістом нового архіву (не resume вже
	// готового стану — той випадок вище повертає рано без логу).
	r.logUpdatedFolder(t.DestRelPath)
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

func (r *Runner) markActuallyDownloaded(manifestKey string) {
	r.actuallyDownloadedMu.Lock()
	if r.actuallyDownloaded == nil {
		r.actuallyDownloaded = map[string]bool{}
	}
	r.actuallyDownloaded[manifestKey] = true
	r.actuallyDownloadedMu.Unlock()
}

func (r *Runner) wasActuallyDownloaded(manifestKey string) bool {
	r.actuallyDownloadedMu.Lock()
	defer r.actuallyDownloadedMu.Unlock()
	return r.actuallyDownloaded[manifestKey]
}

func (r *Runner) markTaskComplete(t Task) {
	if t.IsArchive {
		return // архіви трекаються окремо (markArchiveComplete) ПІСЛЯ розпаковки
	}
	r.markStateFile(t.ManifestKey, t.DestRelPath, t.SHA256, t.SHA1, t.UpdatedAt)
	// Логуємо лише те, що РЕАЛЬНО якалось з мережі цього разу — файл, що
	// пройшов ранній вихід за збігом хешу (downloadTask), на диску не
	// змінювався взагалі, і показувати його як "оновлено" було б оманливо.
	if r.wasActuallyDownloaded(t.ManifestKey) {
		r.logUpdatedFile(t.DestRelPath)
	}
}

// markArchiveComplete записує FileRecord для override-folder архіву ПІСЛЯ
// успішної розпаковки. Ключ — той самий wf.Path (ManifestKey), за яким
// BuildPlan шукає hasRec: наступний запуск синку бачить запис і не качає
// архів знову (раніше markTaskComplete повертав рано для IsArchive, тож
// State.Files не мав запису, і архів перекачувався при КОЖНОМУ запуску
// гри — а .override-archives лишав zip-файли).
func (r *Runner) markArchiveComplete(t Task) {
	r.markStateFile(t.ManifestKey, t.DestRelPath, t.SHA256, t.SHA1, t.UpdatedAt)
}

func (r *Runner) markStateFile(key, localPath, sha256, sha1, updatedAt string) {
	sha := sha256
	if sha == "" {
		sha = sha1
	}
	r.mu.Lock()
	r.State.Files[key] = FileRecord{
		Path:      key,
		LocalPath: localPath,
		SHA:       sha,
		UpdatedAt: updatedAt,
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
		// p — ЛОКАЛЬНИЙ шлях відносно кореня установки ("mods/foo.jar").
		// Раніше сюди потрапляв R2-ключ ("packs/<id>/mods/x.jar") або
		// FileName без префікса mods/ — os.Remove по такому шляху нічого
		// не знаходив, і моди, що зникли з маніфесту, назавжди лишались на
		// диску (РЕГРЕСІЯ 2026-08-09).
		full, jerr := SafeJoin(r.InstallDir, p)
		if jerr != nil {
			continue // шлях зі стану/маніфесту виходить за теку збірки — не чіпаємо
		}
		if err := os.Remove(full); err == nil {
			r.mu.Lock()
			// Прибираємо і запис стану: ключ шукаємо серед Files, у яких
			// LocalPath збігається (сам p може бути і R2-ключем, і локальним
			// шляхом — див. BuildPlan/OrphansToRemove).
			for k, rec := range r.State.Files {
				if k == p || rec.LocalPath == p {
					delete(r.State.Files, k)
				}
			}
			r.mu.Unlock()
			r.logRemovedFile(p)
		}
	}
	r.StateStore.Save(r.State)
}

func (r *Runner) localExists(destRelPath string) bool {
	_, err := os.Stat(filepath.Join(r.InstallDir, filepath.FromSlash(destRelPath)))
	return err == nil
}

// hashCacheOf повертає (створюючи за потреби) кеш хешів для цього
// Runner. Файл кешу лежить у СЛУЖБОВІЙ теці лаунчера (StateStore.DataDir(),
// той самий корінь, де install-state/), а НЕ всередині InstallDir
// збірки — тека збірки належить гравцю/маніфесту, а не лаунчеру, і
// службові файли там неприпустимі (див. коментар на початку
// filehashcache.go). Лінива ініціалізація: створення Runner (NewRunner)
// не має торкатись диска, кеш "прогрівається" лише при першому
// реальному зверненні до localHashMatches. Якщо StateStore чомусь nil
// (напр. в тестах, що не налаштовують повний Runner) — кеш просто не
// створюється, і localHashMatches прозоро падає на старий шлях
// (завжди рахувати хеш з диска), нічого не ламаючи.
func (r *Runner) hashCacheOf() *fileHashCache {
	r.hashCacheMu.Lock()
	defer r.hashCacheMu.Unlock()
	if r.hashCache == nil && r.StateStore != nil {
		r.hashCache = newFileHashCache(r.StateStore.DataDir(), r.PackID, r.InstallDir)
	}
	return r.hashCache
}

// FlushHashCache зберігає на диск накопичені під час Run() записи кешу
// хешів (один атомарний запис на всю сесію, див. filehashcache.go).
// Викликається викликачем ПІСЛЯ Run() (успішного чи ні — кеш коректний
// у будь-якому випадку, бо кожен запис прив'язаний до конкретних
// розміру+mtime). Пропущений виклик не ламає коректність — лише
// означає, що наступний скан порахує ці хеші заново з диска.
func (r *Runner) FlushHashCache() {
	r.hashCacheMu.Lock()
	c := r.hashCache
	r.hashCacheMu.Unlock()
	if c != nil {
		c.flush()
	}
}

// listLocalMods повертає імена jar-файлів у mods/ на диску гравця. Потрібно
// BuildPlan'у для строгого видалення чужих модів: у mods/ офіційної збірки
// має бути лише те, що надав R2/маніфест — моди, які гравець доклав сам,
// видаляються (вимога 2026-08-09).
func (r *Runner) listLocalMods() []string {
	matches, err := filepath.Glob(filepath.Join(r.InstallDir, "mods", "*.jar"))
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, filepath.Base(m))
	}
	return out
}

// localHashMatches перевіряє, чи файл на диску відповідає очікуваному
// хешу з маніфесту. Довжина expectedHash визначає алгоритм: 40 hex =
// sha1, 64 hex = sha256.
//
// ПРИСКОРЕННЯ: спершу питаємо hashCache за (шлях, розмір, mtime) файлу —
// os.Stat замість повного читання й хешування jar. Якщо файл не
// змінювався з моменту, коли хеш рахували востаннє (той самий розмір і
// mtime), хеш береться з кешу МИТТЄВО, без відкриття файлу. Це і є
// різниця між "секундами на 100+ модів" і "долями секунди": замість
// сотень повних читань диска — сотні дешевих os.Stat + порівняння двох
// чисел. Лише при кеш-міс (новий/змінений файл) рахуємо хеш як і
// раніше, і одразу зберігаємо результат у кеш для наступного разу.
func (r *Runner) localHashMatches(destRelPath, expectedHash string) bool {
	fullPath := filepath.Join(r.InstallDir, filepath.FromSlash(destRelPath))

	fi, statErr := os.Stat(fullPath)
	if statErr != nil {
		return false // файлу немає — узгоджено зі старою поведінкою (os.Open впав би так само)
	}
	size := fi.Size()
	modNano := fi.ModTime().UnixNano()

	cache := r.hashCacheOf()
	if cache != nil {
		if cachedSHA1, cachedSHA256, ok := cache.get(destRelPath, size, modNano); ok {
			if len(expectedHash) == 40 && cachedSHA1 != "" {
				return cachedSHA1 == expectedHash
			}
			if len(expectedHash) == 64 && cachedSHA256 != "" {
				return cachedSHA256 == expectedHash
			}
			// У кеші є запис для цього (розмір, mtime), але саме потрібного
			// алгоритму хешу там ще нема (напр. раніше рахували лише sha1,
			// а зараз маніфест просить sha256) — рахуємо з диска нижче і
			// довповнюємо запис, не втрачаючи вже наявну половину.
		}
	}

	f, err := os.Open(fullPath)
	if err != nil {
		return false
	}
	defer f.Close()

	if len(expectedHash) == 40 {
		h := sha1.New()
		io.Copy(h, f)
		got := fmt.Sprintf("%x", h.Sum(nil))
		if cache != nil {
			cache.put(destRelPath, size, modNano, got, "")
		}
		return got == expectedHash
	}
	h := sha256.New()
	io.Copy(h, f)
	got := fmt.Sprintf("%x", h.Sum(nil))
	if cache != nil {
		cache.put(destRelPath, size, modNano, "", got)
	}
	return got == expectedHash
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

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// fileMatchesSHA1 — чи файл на диску відповідає очікуваному SHA1 (для 416:
// попередня спроба могла скачати весь файл у .part і впасти до перейменування).
func fileMatchesSHA1(path, wantSHA1 string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return false
	}
	return strings.EqualFold(fmt.Sprintf("%x", h.Sum(nil)), wantSHA1)
}

// fileMatchesSHA256 — чи файл на диску відповідає очікуваному SHA256
// (пропуск уже скачаного файла при resume — див. downloadTask).
func fileMatchesSHA256(path, wantSHA256 string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false
	}
	return strings.EqualFold(fmt.Sprintf("%x", h.Sum(nil)), wantSHA256)
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
