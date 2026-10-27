package social

// keystore.go — зберігання пари ключів E2E на диску гравця.
//
// Вимоги безпеки:
//   - приватний ключ НІКОЛИ не покидає диск (ні в лог, ні в мережу, ні в UI);
//   - тека — права 0700, файл — 0600 (на Unix). На Windows права не діють,
//     але файл лишається у профілі користувача;
//   - запис АТОМАРНИЙ (temp у тій самій теці → rename): обірване живлення не
//     лишає напівзаписаного ключа, який неможливо прочитати;
//   - якщо файл Є, але пошкоджений — повертаємо помилку, а НЕ генеруємо новий
//     ключ: інакше мовчки втратили б доступ до всієї старої переписки.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	identityFileName = "identity.json"
	identityVersion  = 1
)

// KeyStore — пара ключів на диску. dir — тека ключів (напр.
// <дані>/.shaurma/keys).
type KeyStore struct {
	dir string
}

func NewKeyStore(dir string) *KeyStore { return &KeyStore{dir: dir} }

// identityFile — формат файлу. Version — для майбутньої міграції; невідома
// версія трактується як помилка (краще відмовити, ніж неправильно прочитати).
type identityFile struct {
	Version   int    `json:"version"`
	PublicKey string `json:"publicKey"`
	SecretKey string `json:"secretKey"`
	CreatedAt int64  `json:"createdAt"`
}

// path повертає шлях до файлу ідентичності.
func (k *KeyStore) path() string {
	return filepath.Join(k.dir, identityFileName)
}

// LoadOrCreate читає наявну пару або створює нову. Ідемпотентно: повторний
// виклик повертає той самий ключ.
func (k *KeyStore) LoadOrCreate() (PublicKey, SecretKey, error) {
	if k == nil || k.dir == "" {
		return PublicKey{}, SecretKey{}, ErrBadInput
	}
	pub, sec, err := k.Load()
	if err == nil {
		return pub, sec, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		// Файл є, але нечитабельний/пошкоджений — НЕ перезаписуємо.
		return PublicKey{}, SecretKey{}, err
	}
	pub, sec, err = GenerateIdentity()
	if err != nil {
		return PublicKey{}, SecretKey{}, err
	}
	if err := k.save(pub, sec); err != nil {
		return PublicKey{}, SecretKey{}, err
	}
	return pub, sec, nil
}

// Load читає наявну пару. os.ErrNotExist, якщо файлу немає.
func (k *KeyStore) Load() (PublicKey, SecretKey, error) {
	if k == nil || k.dir == "" {
		return PublicKey{}, SecretKey{}, ErrBadInput
	}
	data, err := os.ReadFile(k.path())
	if err != nil {
		return PublicKey{}, SecretKey{}, err
	}
	var f identityFile
	if err := json.Unmarshal(data, &f); err != nil {
		return PublicKey{}, SecretKey{}, fmt.Errorf("social:identity_corrupt")
	}
	if f.Version != identityVersion {
		return PublicKey{}, SecretKey{}, fmt.Errorf("social:identity_version")
	}
	pub, err := DecodePublicKey(f.PublicKey)
	if err != nil {
		return PublicKey{}, SecretKey{}, fmt.Errorf("social:identity_corrupt")
	}
	raw, err := b64StdDecodeExact(f.SecretKey, SecretKeyBytes)
	if err != nil {
		return PublicKey{}, SecretKey{}, fmt.Errorf("social:identity_corrupt")
	}
	var sec SecretKey
	copy(sec[:], raw)

	// Публічний ключ мусить відповідати приватному — інакше підпис/узгодження
	// ключа мовчки розійшлись (напр. файл підмінили). Відновлюємо пару з
	// приватного й порівнюємо; невідповідність = пошкоджений файл.
	derived, ok := publicFromSecret(sec)
	if !ok || derived != pub {
		return PublicKey{}, SecretKey{}, fmt.Errorf("social:identity_mismatch")
	}
	return pub, sec, nil
}

// Exists повідомляє, чи є файл ключа (не читаючи його).
func (k *KeyStore) Exists() bool {
	if k == nil || k.dir == "" {
		return false
	}
	st, err := os.Stat(k.path())
	return err == nil && !st.IsDir()
}

// save пише файл атомарно з правами 0600 (тека 0700).
func (k *KeyStore) save(pub PublicKey, sec SecretKey) error {
	if err := os.MkdirAll(k.dir, 0o700); err != nil {
		return fmt.Errorf("social:identity_mkdir")
	}
	// На випадок, якщо тека вже існувала з ширшими правами.
	_ = os.Chmod(k.dir, 0o700)

	f := identityFile{
		Version:   identityVersion,
		PublicKey: EncodePublicKey(pub),
		SecretKey: b64StdEncode(sec[:]),
		CreatedAt: time.Now().UnixMilli(),
	}
	data, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("social:identity_encode")
	}

	// Існуючий файл ніколи не затираємо без архіву.
	if err := archiveFile(k.path()); err != nil {
		return err
	}
	return writeFileAtomic(k.dir, identityFileName, data)
}
