package share

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// journal — стан передачі на диску одержувача, щоб пережити краш лаунчера, паузу чи
// обрив мережі. Ключ — ID маніфесту (вміст): та сама збірка продовжується з того ж
// місця навіть через нову сесію відправника.
type journal struct {
	ManifestID string `json:"manifestId"`
	Done       []int  `json:"done"` // індекси файлів, що повністю прийняті й звірені
	Complete   bool   `json:"complete"`
}

func journalPath(stage string) string { return filepath.Join(stage, "journal.json") }

func loadJournal(stage string) (journal, bool) {
	raw, err := os.ReadFile(journalPath(stage))
	if err != nil {
		return journal{}, false
	}
	var j journal
	if json.Unmarshal(raw, &j) != nil {
		return journal{}, false
	}
	return j, true
}

// saveJournal пише атомарно (tmp + rename): обрив посеред запису не лишає half-JSON.
func saveJournal(stage string, j journal) error {
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return err
	}
	raw, err := json.Marshal(j)
	if err != nil {
		return err
	}
	tmp := journalPath(stage) + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, journalPath(stage))
}

// FilesDir — де лежать прийняті файли у stage.
func FilesDir(stage string) string { return filepath.Join(stage, "files") }

// StageInfo — що лишилось на диску від передачі (для «Продовжити?» після краху).
type StageInfo struct {
	ManifestID string
	FilesDone  int
	Complete   bool
}

// ReadStage читає журнал у stage; ok=false, якщо там нічого немає.
func ReadStage(stage string) (StageInfo, bool) {
	j, ok := loadJournal(stage)
	if !ok {
		return StageInfo{}, false
	}
	return StageInfo{ManifestID: j.ManifestID, FilesDone: len(j.Done), Complete: j.Complete}, true
}
