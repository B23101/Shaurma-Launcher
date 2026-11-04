package newupdate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// Фази двофазного оновлення (§8.3).
const (
	PhaseNone      = ""
	PhaseSwapped   = "swapped"   // файли замінено, чекаємо підтвердження від backend
	PhaseCommitted = "committed" // нова версія стартувала, старе прибрано
)

// State — update-state.json. Пишеться атомарно (tmp + fsync + rename): обрив
// живлення на будь-якому кроці закінчується продовженням або відкатом.
type State struct {
	HighestSeen   string `json:"highest_seen_version,omitempty"`
	KeysetVersion int64  `json:"keyset_version,omitempty"`
	// Keyset/KeysetSig — найновіший прийнятий набір ключів (кеш, перевіряється
	// root при кожному читанні — файлу на диску не довіряємо).
	Keyset    []byte `json:"keyset,omitempty"`
	KeysetSig string `json:"keyset_sig,omitempty"`

	CertSecured    bool   `json:"cert_secured,omitempty"`
	MigrationState string `json:"migration_state,omitempty"`

	Phase           string    `json:"phase,omitempty"`
	PendingVersion  string    `json:"pending_version,omitempty"`
	PreviousVersion string    `json:"previous_version,omitempty"`
	PendingPaths    []string  `json:"pending_paths,omitempty"`
	Attempts        int       `json:"attempts,omitempty"`
	SwappedAt       time.Time `json:"swapped_at,omitempty"`

	// FailedVersion — версія, з якої вже відкочувались: повторно її не ставимо
	// (інакше вічне коло «оновив → не стартує → відкат → оновив»).
	FailedVersion string `json:"failed_version,omitempty"`
	// DeferredSince — коли сервер уперше сказав «пізніше» для DeferredVersion.
	DeferredVersion string    `json:"deferred_version,omitempty"`
	DeferredSince   time.Time `json:"deferred_since,omitempty"`
}

func loadState(dir string) *State {
	var s State
	data, err := os.ReadFile(filepath.Join(dir, StateFileName))
	if err == nil {
		if json.Unmarshal(data, &s) != nil {
			s = State{}
		}
	}
	return &s
}

func saveState(dir string, s *State) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, StateFileName), data, 0o644)
}

// writeFileAtomic — запис у tmp, fsync, rename на місце.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// Health — те, що пише backend після успішного старту версії.
type Health struct {
	Version string    `json:"version"`
	At      time.Time `json:"at"`
}

func loadHealth(dir string) *Health {
	data, err := os.ReadFile(filepath.Join(dir, HealthFileName))
	if err != nil {
		return nil
	}
	var h Health
	if json.Unmarshal(data, &h) != nil {
		return nil
	}
	return &h
}

// MarkHealthy — викликає backend, коли версія стартувала (ініціалізація
// пройшла). Окрема експортована функція, щоб контракт був в одному місці.
func MarkHealthy(dir, version string) error {
	data, _ := json.Marshal(Health{Version: version, At: now()})
	return writeFileAtomic(filepath.Join(dir, HealthFileName), data, 0o644)
}
