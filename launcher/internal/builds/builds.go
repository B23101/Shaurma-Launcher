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

	"shaurma-launcher-wails/internal/atomicfile"
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
	Version  string   `json:"version,omitempty"`
	Source   string   `json:"source,omitempty"` // mrpack URL або локальний файл
	ServerIP string   `json:"serverIp,omitempty"`
	Tags     []string `json:"tags,omitempty"`
}

// View — стан збірки для ПОТОЧНОГО (активного) акаунта.
type View struct {
	Build  Build  `json:"build"`
	Status Status `json:"status"`
	// Installed — файли збірки присутні локально (незалежно від версії).
	Installed bool `json:"installed"`
	// Progress — активний прогрес завантаження/оновлення, якщо йде.
	Progress *Progress `json:"progress,omitempty"`
	// RunningOn — ім'я акаунта, на якому збірка ЗАРАЗ запущена, якщо це
	// НЕ поточний акаунт. Для поточного акаунта Status = running, а
	// RunningOn порожній. Фронтенд показує іконку голови скіна цього
	// акаунта замість опису/кнопки «Грати».
	RunningOn string `json:"runningOn,omitempty"`
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
type Registry struct {
	mu      sync.Mutex
	path    string
	entries map[string]Entry
}

// NewRegistry створює реєстр із вказаним шляхом до builds.json.
// Дані читаються ліниво через Load().
func NewRegistry(path string) *Registry {
	return &Registry{path: path, entries: map[string]Entry{}}
}

// Load читає реєстр з диска. Помилки читання не фатальні: реєстр
// стартує порожнім (напр. файл ще не існує).
func (r *Registry) Load() {
	r.mu.Lock()
	defer r.mu.Unlock()
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
	r.saveLocked()
}

// saveLocked пише реєстр на диск АТОМАРНО (tmp + rename) — краш у момент
// запису не має лишити обрізаний builds.json, інакше лаунчер «забув» би
// встановлені збірки (мовчазний відкат до порожнього реєстру при старті).
func (r *Registry) saveLocked() {
	list := make([]Entry, 0, len(r.entries))
	for _, e := range r.entries {
		list = append(list, e)
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
