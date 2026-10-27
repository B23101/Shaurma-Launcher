//go:build !windows

package shaurmaid

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
)

type gcmProtector struct{ aead cipher.AEAD }

func newProtector(dir string, hw HWIDGenerator) (protector, error) {
	hwid, err := hw.HWID()
	if err != nil {
		return nil, err
	}
	saltPath := filepath.Join(dir, ".salt")
	salt, err := os.ReadFile(saltPath)
	if err != nil || len(salt) != 32 {
		salt = make([]byte, 32)
		if _, err := rand.Read(salt); err != nil {
			return nil, err
		}
		if err := os.WriteFile(saltPath, salt, 0o600); err != nil {
			return nil, err
		}
	}
	key, err := hkdf.Key(sha256.New, []byte(hwid), salt, "shaurma-secure-storage-v1", 32)
	if err != nil {
		return nil, err
	}
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(blk)
	if err != nil {
		return nil, err
	}
	return &gcmProtector{g}, nil
}

func (p *gcmProtector) protect(plain []byte) ([]byte, error) {
	n := make([]byte, p.aead.NonceSize())
	if _, err := rand.Read(n); err != nil {
		return nil, err
	}
	return p.aead.Seal(n, n, plain, []byte("shaurma")), nil
}

func (p *gcmProtector) unprotect(sealed []byte) ([]byte, error) {
	ns := p.aead.NonceSize()
	if len(sealed) < ns+p.aead.Overhead() {
		return nil, errors.New("shaurmaid: sealed blob too short")
	}
	return p.aead.Open(nil, sealed[:ns], sealed[ns:], []byte("shaurma"))
}
