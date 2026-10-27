// owner.go — локальний секрет власника іменованого піддомену. Живе у
// .shaurma/p2p/owner-secret, ОКРЕМО від settings.json: він не є
// налаштуванням гравця (не має відображатись/редагуватись у звичайних
// налаштуваннях), а є радше приватним ключем — генерується один раз,
// ніколи не бекапиться і ніколи не покидає цей файл, крім як у тілі
// /p2p/host-update і /p2p/release запитів (де Worker бачить лише його,
// а зберігає — лише sha256-хеш, не сам секрет).
package p2p

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const ownerSecretFileName = "owner-secret"

// loadOrCreateOwnerSecret читає існуючий секрет із stateDir, або генерує
// новий (32 випадкові байти → 64 hex-символи) і зберігає його, якщо
// файла ще немає. Права доступу 0600 — лише власник файлу може читати.
func loadOrCreateOwnerSecret(stateDir string) (string, error) {
	if err := os.MkdirAll(stateDir, 0700); err != nil {
		return "", fmt.Errorf("p2p: не вдалося створити %s: %w", stateDir, err)
	}
	path := filepath.Join(stateDir, ownerSecretFileName)

	if data, err := os.ReadFile(path); err == nil {
		secret := strings.TrimSpace(string(data))
		if len(secret) >= 32 {
			return secret, nil
		}
		// Файл є, але щось з ним не так (пошкоджений/порожній) — генеруємо новий.
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("p2p: читання %s: %w", path, err)
	}

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("p2p: генерація секрету: %w", err)
	}
	secret := hex.EncodeToString(buf)

	// 0600 — секрет читається лише власником файлу (на Windows права
	// POSIX умовні, але це best-effort і на Windows теж не шкодить).
	if err := os.WriteFile(path, []byte(secret), 0600); err != nil {
		return "", fmt.Errorf("p2p: збереження %s: %w", path, err)
	}
	return secret, nil
}
