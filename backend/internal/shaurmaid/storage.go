package shaurmaid

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
)

var ErrNotFound = errors.New("shaurmaid: not found")

// protector «запечатує» байти засобами ОС. Windows: DPAPI (прив'язка до користувача Windows).
// Інші ОС: AES-256-GCM з ключем із HWID та випадкової «солі» (захищає від копіювання файлів на
// інший ПК; від процесу, що працює від імені того самого користувача, — ні, як і будь-яке
// файлове сховище без системного keyring).
type protector interface {
	protect(plain []byte) ([]byte, error)
	unprotect(sealed []byte) ([]byte, error)
}

type fileStorage struct {
	dir string
	p   protector
}

// NewSecureStorage — сховище секретів у каталозі dir (права 0700/0600).
func NewSecureStorage(dir string, hw HWIDGenerator) (LocalSecureStorage, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	p, err := newProtector(dir, hw)
	if err != nil {
		return nil, err
	}
	return &fileStorage{dir: dir, p: p}, nil
}

func (s *fileStorage) path(key string) string {
	h := sha256.Sum256([]byte("shaurma-store-v1|" + key))
	return filepath.Join(s.dir, hex.EncodeToString(h[:12])+".bin")
}

func (s *fileStorage) Put(key string, value []byte) error {
	sealed, err := s.p.protect(value)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, werr := tmp.Write(sealed)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		os.Remove(name)
		if werr != nil {
			return werr
		}
		return cerr
	}
	_ = os.Chmod(name, 0o600)
	if err := os.Rename(name, s.path(key)); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

func (s *fileStorage) Get(key string) ([]byte, error) {
	b, err := os.ReadFile(s.path(key))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return s.p.unprotect(b)
}

func (s *fileStorage) Delete(key string) error {
	err := os.Remove(s.path(key))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
