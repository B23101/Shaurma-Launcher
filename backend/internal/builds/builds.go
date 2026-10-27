// Пакет builds — архітектурне ядро керування збірками лаунчера.
//
// Розділення відповідальності:
//   - Kind — тип збірки: Шаурма (офіційні, качаються з індексу worker)
//     та Кастомні (власні/локальні/mrpack-імпорт). Завантаження
//     Шаурма-збірок відбувається лише для KindShaurma.
//   - Build — уніфікований опис збірки незалежно від джерела (index.json
//     worker, конфіг кастомної збірки, імпортований mrpack). Фронтенд
//     отримує саме []View і не знає, звідки прийшла збірка.
//   - Registry — персистентний реєстр встановлених збірок (builds.json у
//     теці даних). Тут зберігається встановлена версія — з неї
//     обчислюється стан «Оновити».
//   - Status — стан збірки для КОНКРЕТНОГО акаунта. Стан файлів
//     (завантажити/оновити/готова) спільний для всіх акаунтів; стан
//     запуску (running) — окремий на акаунт (див. internal/sessions).
package builds

import (
	"encoding/json"
	"os"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/atomicfile"
)

// Kind — тип збірки.
type Kind string

const (
	KindShaurma Kind = "shaurma"
	KindCustom  Kind = "custom"
)

// Status — стан збірки для поточного акаунта.
type Status string

const (
	StatusNotInstalled Status = "not-installed"
	StatusNeedsUpdate  Status = "needs-update"
	StatusReady        Status = "ready"
	StatusDownloading  Status = "downloading"
	StatusUpdating     Status = "updating"
	StatusRunning      Status = "running"
	StatusError        Status = "error"
)

// Progress — прогрес завантаження/оновлення збірки (для sidebar і тайлів).
type Progress struct {
	Percent     int    `json:"percent"`
	BytesDone   int64  `json:"bytesDone"`
	BytesTotal  int64  `json:"bytesTotal"`
	FilesDone   int    `json:"filesDone"`
	FilesTotal  int    `json:"filesTotal"`
	CurrentFile string `json:"currentFile"`
	SpeedBPS    int64  `json:"speedBps"`
	// Mode — "running" | "paused" | "cancelled" | "complete" | "error"
	// (sync.RunMode). Разом з NoInternet дає UI показати правильний банер
	// замість здогадуватись з мовчазного нульового прогресу ("зависло").
	Mode string `json:"mode,omitempty"`
	// Stage — "downloading" | "extracting" | "finalizing" тощо
	// (sync.Stage) — відображається як короткий підпис під прогрес-баром.
	Stage string `json:"stage,omitempty"`
	// NoInternet — true, якщо остання спроба виявила повну відсутність
	// мережі (а не тимчасовий збій сервера) — показується як окремий банер.
	NoInternet bool `json:"noInternet,omitempty"`
}

// Build — уніфікований опис збірки.
type Build struct {
	ID            string `json:"id"`
	Kind          Kind   `json:"kind"`
	Name          string `json:"name"`
	Description   string `json:"description,omitempty"`
	MCVersion     string `json:"mcVersion"`
	Loader        string `json:"loaderType"`
	LoaderVersion string `json:"loaderVersion"`
	IconURL       string `json:"iconUrl,omitempty"`
	BackgroundURL string `json:"backgroundUrl,omitempty"`
	// Icon — пресет-іконка (Tabler, напр. "ti-puzzle") для кастомних збірок;
	// PNG з IconURL має пріоритет. Для Shaurma-збірок порожній.
	Icon  string `json:"icon,omitempty"`
	Color string `json:"color,omitempty"`
	// Version — остання відома версія збірки на сервері; порівнюється з
	// Registry.InstalledVersion для стану «Оновити».
	Version string `json:"version,omitempty"`
	Source  string `json:"source,omitempty"` // mrpack URL або локальний файл
	// SourceProjectID/SourceVersionID — джерело «інтернет-збірки»
	// (modrinth/curseforge проєкт і встановлена версія). Потрібні фронтенду
	// для «Оновити»: список версій з джерела + підсвітка поточної.
	SourceProjectID string   `json:"sourceProjectId,omitempty"`
	SourceVersionID string   `json:"sourceVersionId,omitempty"`
	ServerIP        string   `json:"serverIp,omitempty"`
	Tags            []string `json:"tags,omitempty"`
	// ── Серверна папка групування (packs-index.json → folderId/...) ──
	// FolderID порожній — збірка НЕ в жодній серверній папці. Заданий —
	// фронтенд групує усі збірки з однаковим FolderID в одну папку
	// sidebar із FolderName/FolderColor. Це ОКРЕМИЙ шар від локальних
	// кастомних папок користувача (PackFolder у settings.json) — сервер
	// диктує ЧЛЕНСТВО (яка збірка в якій папці) і назву/колір/сортування
	// вмісту, а відкрито/закрито і позицію самої папки серед інших
	// вирішує лаунчер локально (packLayout).
	FolderID       string `json:"folderId,omitempty"`
	FolderName     string `json:"folderName,omitempty"`
	FolderColor    string `json:"folderColor,omitempty"`
	FolderOrder    int    `json:"folderOrder,omitempty"`
	FolderPosition int    `json:"folderPosition,omitempty"`
}

// RunningAccount — акаунт, на якому збірка ЗАРАЗ запущена (для голів
// акаунтів у sidebar/картці/сторінці збірки). Та сама збірка може бути
// запущена на кількох акаунтах одночасно (1 збірка × 1 акаунт), тому
// View.RunningOn — список.
type RunningAccount struct {
	AccountID   string `json:"accountId"`
	AccountName string `json:"accountName"`
}

// View — стан збірки для ПОТОЧНОГО (активного) акаунта.
type View struct {
	Build  Build  `json:"build"`
	Status Status `json:"status"`
	// Installed — файли збірки присутні локально (незалежно від версії).
	Installed bool `json:"installed"`
	// Progress — активний прогрес завантаження/оновлення, якщо йде.
	Progress *Progress `json:"progress,omitempty"`
	// RunningOn — акаунти, на яких збірка ЗАРАЗ запущена, КРІМ поточного.
	// Для поточного акаунта Status = running, а сам він сюди не входить.
	// Фронтенд показує іконки голів цих акаунтів замість опису/кнопки «Грати».
	RunningOn []RunningAccount `json:"runningOn,omitempty"`
	// StartedAtMs — unix-мс моменту старту сесії ПОТОЧНОГО акаунта (коли
	// Status = running). Фронтенд використовує його, щоб відновити таймер
	// «у сесії» на кнопці «Стоп» навіть якщо подію build:started пропущено
	// (напр. подія прийшла до підписки після рестарту лаунчера).
	StartedAtMs int64 `json:"startedAtMs,omitempty"`
	// Error — повідомлення помилки (для стану error).
	Error string `json:"error,omitempty"`
}

// Entry — запис реєстру встановленої збірки.
type Entry struct {
	ID               string    `json:"id"`
	Kind             Kind      `json:"kind"`
	InstalledVersion string    `json:"installedVersion"`
	InstalledAt      time.Time `json:"installedAt"`
}

// Registry — персистентний реєстр встановлених збірок. Файли збірок
// спільні для всіх акаунтів, тому і стан файлів зберігається один раз
// (завантажити/оновити/готова), а запуск процесів відстежує sessions.
//
// Сховище: SQLite (shaurma.db, таблиця builds_registry) — єдина база
// даних лаунчера. Бекенд задається функціями-хуками (loadDB/saveDB/
// removeDB), які підключає App з main-пакета — це обходить import cycle
// (model → builds → store → model). Якщо хуки не задані (тести, dev-без
// БД) — фолбек на файл builds.json.
type Registry struct {
	mu       sync.Mutex
	path     string // файловий фолбек (без DB-хуків)
	db       bool
	loadDB   func() ([]Entry, error)
	saveDB   func([]Entry) error
	removeDB func(id string) error
	entries  map[string]Entry
}

// NewRegistry створює реєстр із вказаним шляхом до builds.json (файловий
// бекенд — для тестів і середовищ без SQLite).
func NewRegistry(path string) *Registry {
	return &Registry{path: path, entries: map[string]Entry{}}
}

// SetDBHooks підключає SQLite-бекенд (викликається App з main-пакета).
// Після виклику реєстр читає/пише через хуки замість файлу.
func (r *Registry) SetDBHooks(loadDB func() ([]Entry, error), saveDB func([]Entry) error, removeDB func(id string) error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.db = true
	r.loadDB = loadDB
	r.saveDB = saveDB
	r.removeDB = removeDB
}

// Load читає реєстр зі сховища. Помилки читання не фатальні: реєстр
// стартує порожнім (напр. база/файл ще не існує).
func (r *Registry) Load() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.db && r.loadDB != nil {
		entries, err := r.loadDB()
		if err != nil {
			return
		}
		r.entries = make(map[string]Entry, len(entries))
		for _, e := range entries {
			r.entries[e.ID] = e
		}
		return
	}
	data, err := os.ReadFile(r.path)
	if err != nil {
		return
	}
	var list []Entry
	if json.Unmarshal(data, &list) != nil {
		return
	}
	r.entries = make(map[string]Entry, len(list))
	for _, e := range list {
		r.entries[e.ID] = e
	}
}

// SetInstalled записує/оновлює запис встановленої збірки і зберігає на диск.
func (r *Registry) SetInstalled(id string, kind Kind, version string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.entries == nil {
		r.entries = map[string]Entry{}
	}
	r.entries[id] = Entry{ID: id, Kind: kind, InstalledVersion: version, InstalledAt: time.Now()}
	r.saveLocked()
}

// Get повертає запис збірки.
func (r *Registry) Get(id string) (Entry, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[id]
	return e, ok
}

// Remove видаляє запис збірки (напр. при видаленні локальної збірки).
func (r *Registry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, id)
	if r.db && r.removeDB != nil {
		_ = r.removeDB(id)
		return
	}
	r.saveLocked()
}

// saveLocked пише реєстр у сховище. У SQLite — через saveDB-хук
// (транзакція delete+insert), у файловому фолбеку — атомарно (tmp +
// rename), щоб краш у момент запису не лишив обрізаний builds.json
// (лаунчер «забув» би встановлені збірки).
func (r *Registry) saveLocked() {
	list := make([]Entry, 0, len(r.entries))
	for _, e := range r.entries {
		list = append(list, e)
	}
	if r.db && r.saveDB != nil {
		_ = r.saveDB(list)
		return
	}
	_ = atomicfile.WriteJSONAtomic(r.path, list)
}

// ComputeFileStatus обчислює стан ФАЙЛІВ збірки (спільний для всіх
// акаунтів) за встановленою версією та останньою версією з сервера.
func ComputeFileStatus(installed bool, installedVersion, remoteVersion string) Status {
	if !installed {
		return StatusNotInstalled
	}
	if remoteVersion != "" && installedVersion != remoteVersion {
		return StatusNeedsUpdate
	}
	return StatusReady
}

// Exists повертає true якщо збірка з таким ID зареєстрована.
func (r *Registry) Exists(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.entries[id]
	return ok
}

// AllIDs повертає зріз всіх зареєстрованих ID.
func (r *Registry) AllIDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]string, 0, len(r.entries))
	for id := range r.entries {
		ids = append(ids, id)
	}
	return ids
}

// RenameID перейменовує запис реєстру з oldID → newID.
// Якщо oldID не існує — no-op. Якщо newID вже є — перезаписує.
func (r *Registry) RenameID(oldID, newID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[oldID]
	if !ok {
		return
	}
	e.ID = newID
	delete(r.entries, oldID)
	r.entries[newID] = e
	r.saveLocked()
}
