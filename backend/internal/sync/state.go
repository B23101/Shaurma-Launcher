package sync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Stage — стадія встановлення/оновлення збірки. Дозволяє відрізнити
// "усі байти якані, але розпаковка override-архівів ще не завершена" від
// "справді готово" — саме та прогалина, яку описував користувач
// ("лаунчер наче оновлює, але після оновлення знову оновлює").
type Stage string

const (
	StageNone            Stage = "" // нічого не почато
	StageManifestFetched Stage = "manifest_fetched"
	StageDownloading     Stage = "downloading" // hashMods+workerFiles тривають
	StageExtracting      Stage = "extracting"  // розпаковка override-folders/*.zip
	StageFinalizing      Stage = "finalizing"  // onMissing + запис builds.Registry
	StageComplete        Stage = "complete"    // усе готово, безпечно грати
)

// FileRecord — локальний запис про один файл збірки: яку версію (за
// updatedAt) ми востаннє успішно встановили і з яким хешем. Це і є
// "locallyRecordedUpdatedAt" з формули розділу 3.2 V2-документа: якщо
// remote.UpdatedAt не новіший — файл вважається актуальним без хешування.
type FileRecord struct {
	Path string `json:"path"` // R2-ключ / ідентифікатор файлу в маніфесті
	// LocalPath — ЛОКАЛЬНИЙ шлях відносно кореня установки збірки
	// ("mods/foo.jar", "config/options.txt"...). Потрібен для видалення
	// осиротілих файлів: сам Path — це R2-ключ (для bundled-модів він
	// виглядає як "packs/<id>/mods/x.jar"), який НЕ співпадає зі шляхом на
	// диску гравця. Раніше removeOrphans видаляв за R2-ключем — файл
	// лишався на диску (РЕГРЕСІЯ 2026-08-09).
	LocalPath string `json:"localPath,omitempty"`
	SHA       string `json:"sha"`       // sha1/sha256 залежно від категорії
	UpdatedAt string `json:"updatedAt"` // останній відомий updatedAt з маніфесту
}

// InstallState — персистентний стан однієї встановленої/встановлюваної
// збірки. Зберігається окремо від download-сесій (рівень 3 персистенції,
// розділ 3.5 V1) — це те, що ловить крах МІЖ кроками синхронізації, коли
// файлова сесія вже "completed", але сама збірка ще не в консистентному
// стані.
type InstallState struct {
	PackID              string                `json:"packId"`
	Stage               Stage                 `json:"stage"`
	ManifestUpdatedAt   string                `json:"manifestUpdatedAt"`
	ManifestContentHash string                `json:"manifestContentHash"`
	Files               map[string]FileRecord `json:"files"` // ключ: Path з маніфесту
	InstalledOnMissing  map[string]bool       `json:"installedOnMissing"`
	UpdatedAt           time.Time             `json:"updatedAt"`
}// StateStore — збереження/читання InstallState. Джерело правди — файли
// install-state/<packID>.json у dataDir (temp+rename для атомарності).
type StateStore struct {
	mu      sync.Mutex
	dir     string
	dataDir string // корінь папки даних лаунчера (батько install-state/) — потрібен іншим компонентам (напр. кеш хешів модів), щоб класти СВОЇ службові файли поруч, а не в install-state/ і НЕ в теку самої збірки.
}

func NewStateStore(dataDir string) *StateStore {
	dir := filepath.Join(dataDir, "install-state")
	os.MkdirAll(dir, 0755)
	return &StateStore{dir: dir, dataDir: dataDir}
}

// DataDir повертає корінь папки даних лаунчера (той самий dataDir, що
// передали в NewStateStore) — НЕ install-state/ підтеку. Використовується
// Runner-ом, щоб знайти службову теку для кешу хешів файлів
// (filehashcache.go), яка мусить лежати ПОРЯД з даними лаунчера, а не
// всередині теки встановленої збірки (там лише файли самої збірки —
// mods/, config/ тощо, службові файли лаунчера там не місце).
func (s *StateStore) DataDir() string { return s.dataDir }

func (s *StateStore) path(packID string) string {
	return filepath.Join(s.dir, packID+".json")
}

// Load читає стан збірки зі сховища. Якщо запису немає — повертає порожній
// стан (Stage=StageNone), це нормальний випадок "збірка ще не встановлена".
func (s *StateStore) Load(packID string) *InstallState {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path(packID))
	if err != nil {
		return &InstallState{
			PackID:             packID,
			Stage:              StageNone,
			Files:              map[string]FileRecord{},
			InstalledOnMissing: map[string]bool{},
		}
	}
	return parseInstallState(packID, data)
}

func parseInstallState(packID string, data []byte) *InstallState {
	var st InstallState
	if json.Unmarshal(data, &st) != nil {
		return &InstallState{
			PackID:             packID,
			Stage:              StageNone,
			Files:              map[string]FileRecord{},
			InstalledOnMissing: map[string]bool{},
		}
	}
	if st.Files == nil {
		st.Files = map[string]FileRecord{}
	}
	if st.InstalledOnMissing == nil {
		st.InstalledOnMissing = map[string]bool{}
	}
	return &st
}

// Delete видаляє збережений стан збірки (ануінстал). Якщо запису немає —
// не помилка (збірка могла ще ніколи не качатись).
func (s *StateStore) Delete(packID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := os.Remove(s.path(packID))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Save записує стан у сховище (SQLite — UPSERT рядка; файловий фолбек —
// атомарний temp+rename).
func (s *StateStore) Save(st *InstallState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	final := s.path(st.PackID)
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, final)
}

// NeedsRecovery — true, якщо збірка була посеред синхронізації, коли
// процес закінчився (крах чи force-quit), і не досягла StageComplete.
func (st *InstallState) NeedsRecovery() bool {
	return st.Stage != StageNone && st.Stage != StageComplete
}

// CleanupOrphaned видаляє install-state файли для збірок, яких немає
// в activePackIDs (видалені/фантомні збірки).
func (s *StateStore) CleanupOrphaned(activePackIDs func() map[string]bool) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return 0
	}
	active := activePackIDs()
	removed := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		packID := strings.TrimSuffix(e.Name(), ".json")
		if !active[packID] {
			_ = os.Remove(filepath.Join(s.dir, e.Name()))
			removed++
		}
	}
	return removed
}
