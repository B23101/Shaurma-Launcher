//go:build !windows

package access

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"os"
	"strings"
)

// Поза Windows: файл 0600, зашифрований ключем від machine-id. Це СЛАБШИЙ
// захист (план §3.4а): позначається в UI як «слабший захист». Шкідлива
// програма від імені користувача поза межами моделі будь-якої схеми.
func machineKey() []byte {
	id := ""
	for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		if b, err := os.ReadFile(p); err == nil {
			id = strings.TrimSpace(string(b))
			break
		}
	}
	sum := sha256.Sum256([]byte(id + "|shaurma-device-key-v1"))
	return sum[:]
}

func protect(plain []byte) ([]byte, bool, error) {
	block, err := aes.NewCipher(machineKey())
	if err != nil {
		return nil, true, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, true, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, true, err
	}
	return gcm.Seal(nonce, nonce, plain, nil), true, nil
}

func unprotect(sealed []byte) ([]byte, error) {
	block, err := aes.NewCipher(machineKey())
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(sealed) < gcm.NonceSize() {
		return nil, errors.New("пошкоджений файл ключа")
	}
	return gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], nil)
}
