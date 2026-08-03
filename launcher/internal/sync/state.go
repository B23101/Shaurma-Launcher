package sync

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	Path      string `json:"path"`      // R2-ключ / ідентифікатор файлу в маніфесті
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
}

// StateStore — атомарне збереження/читання InstallState на диск
// (temp+rename, як і решта персистентних файлів движка — крах процесу
// посеред запису не лишає пошкодженого стану).
type StateStore struct {
	mu  sync.Mutex
	dir string
}

func NewStateStore(dataDir string) *StateStore {
	dir := filepath.Join(dataDir, "install-state")
	os.MkdirAll(dir, 0755)
	return &StateStore{dir: dir}
}

func (s *StateStore) path(packID string) string {
	return filepath.Join(s.dir, packID+".json")
}

// Load читає стан збірки з диска. Якщо файлу немає — повертає порожній
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

// Delete видаляє збережений стан збірки (ануінстал). Якщо файлу немає —
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

// Save атомарно записує стан на диск (temp+rename).
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
