package social

// keyring.go — локальний кеш УСІХ версій ключа чату (зв'язка ключів).
//
// Джерело правди — сервер («Простий» режим: конверт зберігає VPS). Локальний файл — кеш, щоб чат
// працював без мережі, і місце, де лежить «спадковий» ключ пристрою (identity.json до оновлення):
// ним ще можна розшифрувати старі повідомлення, навіть якщо сервер видає вже інший ключ.
//
// Правила безпеки такі ж, як у keystore.go:
//   - файл 0600, тека 0700, запис атомарний;
//   - пошкоджений файл → помилка, а НЕ новий порожній (інакше втратили б ключі);
//   - перед КОЖНОЮ заміною попередня версія файлу архівується (keyring.json.bak-<мс>), лишаємо 5.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	keyringFileName = "keyring.json"
	keyringVersion  = 1
	keepBackups     = 5

	kindServer = "server" // версія, що лежить на сервері (джерело правди)
	kindLegacy = "legacy" // ключ, що був лише на цьому пристрої: лише для розшифрування старого
)

type keyringEntry struct {
	Version   int    `json:"version"` // 0 для legacy
	Kind      string `json:"kind"`
	PublicKey string `json:"publicKey"`
	SecretKey string `json:"secretKey"`
	CreatedAt int64  `json:"createdAt"`
}

type keyringFile struct {
	Version int            `json:"version"`
	Current int            `json:"current"`
	Entries []keyringEntry `json:"entries"`
}

// Keyring — файл зв'язки ключів у теці ключів.
type Keyring struct{ dir string }

func NewKeyring(dir string) *Keyring { return &Keyring{dir: dir} }

func (k *Keyring) path() string { return filepath.Join(k.dir, keyringFileName) }

// Load читає зв'язку. os.ErrNotExist — файлу немає; інша помилка — файл пошкоджений (НЕ перезаписувати).
func (k *Keyring) Load() (*keyringFile, error) {
	if k == nil || k.dir == "" {
		return nil, ErrBadInput
	}
	data, err := os.ReadFile(k.path())
	if err != nil {
		return nil, err
	}
	var f keyringFile
	if json.Unmarshal(data, &f) != nil || f.Version != keyringVersion {
		return nil, fmt.Errorf("social:keyring_corrupt")
	}
	for _, e := range f.Entries {
		pub, err := DecodePublicKey(e.PublicKey)
		if err != nil {
			return nil, fmt.Errorf("social:keyring_corrupt")
		}
		raw, err := b64StdDecodeExact(e.SecretKey, SecretKeyBytes)
		if err != nil {
			return nil, fmt.Errorf("social:keyring_corrupt")
		}
		var sec SecretKey
		copy(sec[:], raw)
		if d, ok := publicFromSecret(sec); !ok || d != pub {
			return nil, fmt.Errorf("social:keyring_mismatch")
		}
	}
	return &f, nil
}

// Save пише зв'язку атомарно; попередній файл спершу архівується.
func (k *Keyring) Save(f *keyringFile) error {
	if k == nil || k.dir == "" || f == nil {
		return ErrBadInput
	}
	f.Version = keyringVersion
	data, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("social:keyring_encode")
	}
	if err := archiveFile(k.path()); err != nil {
		return err // не вдалось заархівувати — не перезаписуємо
	}
	return writeFileAtomic(k.dir, keyringFileName, data)
}

// archiveFile копіює наявний файл у <path>.bak-<мс> (якщо він є) і залишає лише keepBackups останніх.
func archiveFile(path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("social:archive_read")
	}
	dir, base := filepath.Split(path)
	bak := filepath.Join(dir, base+".bak-"+strconv.FormatInt(time.Now().UnixMilli(), 10))
	if err := os.WriteFile(bak, data, 0o600); err != nil {
		return fmt.Errorf("social:archive_write")
	}
	ents, _ := filepath.Glob(path + ".bak-*")
	sort.Strings(ents) // мс однакової довжини → лексикографічно = за часом
	for len(ents) > keepBackups {
		_ = os.Remove(ents[0])
		ents = ents[1:]
	}
	return nil
}

// writeFileAtomic — temp у тій самій теці → fsync → rename; права 0600, тека 0700.
func writeFileAtomic(dir, name string, data []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("social:identity_mkdir")
	}
	_ = os.Chmod(dir, 0o700)
	tmp, err := os.CreateTemp(dir, name+".tmp-*")
	if err != nil {
		return fmt.Errorf("social:identity_tmp")
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("social:identity_chmod")
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("social:identity_write")
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("social:identity_sync")
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("social:identity_close")
	}
	if err := os.Rename(tmpName, filepath.Join(dir, name)); err != nil {
		return fmt.Errorf("social:identity_rename")
	}
	tmpName = ""
	return nil
}

// ── in-memory представлення ───────────────────────────────────────────────

type ringKey struct {
	version int
	kind    string
	pub     PublicKey
	sec     SecretKey
	created int64
}

func (e keyringEntry) key() (ringKey, bool) {
	pub, err := DecodePublicKey(e.PublicKey)
	if err != nil {
		return ringKey{}, false
	}
	raw, err := b64StdDecodeExact(e.SecretKey, SecretKeyBytes)
	if err != nil {
		return ringKey{}, false
	}
	var sec SecretKey
	copy(sec[:], raw)
	return ringKey{version: e.Version, kind: e.Kind, pub: pub, sec: sec, created: e.CreatedAt}, true
}

func (r ringKey) entry() keyringEntry {
	return keyringEntry{Version: r.version, Kind: r.kind, PublicKey: EncodePublicKey(r.pub),
		SecretKey: b64StdEncode(r.sec[:]), CreatedAt: r.created}
}

// orderKeys: [0] — поточний (ним шифруємо), далі server за спаданням версії, потім legacy.
func orderKeys(keys []ringKey, current int) []ringKey {
	out := append([]ringKey(nil), keys...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.kind == kindServer) != (b.kind == kindServer) {
			return a.kind == kindServer
		}
		if (a.version == current) != (b.version == current) {
			return a.version == current
		}
		return a.version > b.version
	})
	return out
}

func keyIDs(keys []ringKey) string {
	var p []string
	for _, k := range keys {
		p = append(p, k.kind+":"+strconv.Itoa(k.version))
	}
	return strings.Join(p, ",")
}
