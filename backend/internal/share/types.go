// Package share — передача збірки або локального сервера від одного гравця до
// іншого (фаза J плану, п. 7.4 PLAN_sever_system.md).
//
// Модель «pull»: ВІДПРАВНИК піднімає локальний HTTP-сервер (Host) на 127.0.0.1 і
// відкриває його друзям через P2P-тунель (той самий механізм, що тимчасові
// запрошення), ОДЕРЖУВАЧ сам підключається й качає (Downloader). Хост нічого не
// ініціює, тож ACL «гість → хост» достатньо.
//
// Пакет навмисно залежить лише від стандартної бібліотеки: логіка передачі
// перевіряється тестами окремо від лаунчера й P2P.
//
// Протокол (усе під заголовком X-Share-Key — ключ одного одержувача):
//
//	GET  /v1/manifest          список файлів (шлях, розмір, sha256)
//	GET  /v1/file?i=N          один файл, підтримує Range (докачування)
//	POST /v1/bundle {"i":[…]}  партія дрібних файлів одним tar-потоком (без
//	                           тимчасових файлів на диску відправника)
//	POST /v1/progress          одержувач повідомляє, скільки ПІДТВЕРДЖЕНО
//	POST /v1/decline           одержувач відмовився
package share

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	ProtocolVersion = 1

	KindBuild  = "build"
	KindServer = "server"

	// HeaderKey — ключ одержувача (видається відправником, їде в зашифрованому чаті).
	HeaderKey = "X-Share-Key"

	// BigFileThreshold — файли від цього розміру качаються окремо (з Range),
	// дрібніші групуються в tar-партії.
	BigFileThreshold = 8 << 20
	// Ліміти партії.
	BundleMaxBytes = 24 << 20
	BundleMaxFiles = 1500

	MaxFiles      = 200_000
	MaxTotalBytes = int64(256) << 30
	MaxPathLen    = 400
	MaxRecipients = 3
)

// FileEntry — один файл передачі. Шлях завжди з '/' і відносний.
type FileEntry struct {
	Path   string `json:"p"`
	Size   int64  `json:"s"`
	SHA256 string `json:"h"`
}

// Manifest — повний опис того, що передається.
type Manifest struct {
	V    int    `json:"v"`
	Kind string `json:"kind"`
	Name string `json:"name"`
	// Meta — довільні поля для лаунчера (версія MC, лоадер, ядро сервера, RAM…).
	// Рушій їх не інтерпретує.
	Meta       map[string]string `json:"meta,omitempty"`
	Files      []FileEntry       `json:"files"`
	TotalBytes int64             `json:"total"`
}

// ID — стабільний ідентифікатор ВМІСТУ (kind + відсортований список шлях/розмір/хеш).
// Та сама збірка без змін → той самий ID, тож недокачана передача продовжується
// навіть після нової сесії; змінена збірка → інший ID, старі шматки не змішуються.
func (m *Manifest) ID() string {
	files := make([]FileEntry, len(m.Files))
	copy(files, m.Files)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	h := sha256.New()
	fmt.Fprintf(h, "shaurma-share-v%d\n%s\n", ProtocolVersion, m.Kind)
	for _, f := range files {
		fmt.Fprintf(h, "%s\x00%d\x00%s\n", f.Path, f.Size, f.SHA256)
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

// blockedExt — розширення, яким не місце в збірці/сервері (дзеркало sync.blockedExt:
// пакет свідомо без залежностей). Відправник такі файли пропускає, одержувач відхиляє.
var blockedExt = map[string]bool{
	".exe": true, ".com": true, ".scr": true, ".pif": true, ".msi": true, ".msp": true,
	".bat": true, ".cmd": true, ".ps1": true, ".psm1": true, ".psd1": true,
	".vbs": true, ".vbe": true, ".wsf": true, ".wsh": true, ".hta": true,
	".lnk": true, ".cpl": true, ".reg": true, ".gadget": true, ".appx": true,
	".msix": true, ".application": true, ".jnlp": true,
}

// IsBlockedName — чи заборонене ім'я файлу за розширенням.
func IsBlockedName(rel string) bool {
	i := strings.LastIndexByte(rel, '.')
	if i < 0 {
		return false
	}
	return blockedExt[strings.ToLower(rel[i:])]
}

// ErrUnsafePath — шлях із маніфесту небезпечний.
var ErrUnsafePath = errors.New("небезпечний шлях у передачі")

// ValidateRelPath — шлях із мережі вважається ворожим: без виходу за теку,
// абсолютних шляхів, дисків, NTFS-потоків, '\\' та порожніх сегментів.
func ValidateRelPath(rel string) error {
	if rel == "" || len(rel) > MaxPathLen {
		return fmt.Errorf("%w: порожній або задовгий шлях", ErrUnsafePath)
	}
	if strings.ContainsRune(rel, 0) || strings.ContainsRune(rel, '\\') {
		return fmt.Errorf("%w: недозволений символ у %q", ErrUnsafePath, rel)
	}
	if strings.HasPrefix(rel, "/") {
		return fmt.Errorf("%w: абсолютний шлях %q", ErrUnsafePath, rel)
	}
	for _, part := range strings.Split(rel, "/") {
		switch {
		case part == "" || part == "." || part == "..":
			return fmt.Errorf("%w: %q", ErrUnsafePath, rel)
		case strings.Contains(part, ":"):
			return fmt.Errorf("%w: двокрапка в %q", ErrUnsafePath, rel)
		case strings.HasSuffix(part, ".") || strings.HasSuffix(part, " "):
			return fmt.Errorf("%w: кінцева крапка/пробіл у %q", ErrUnsafePath, rel)
		}
	}
	if IsBlockedName(rel) {
		return fmt.Errorf("%w: заборонений тип файлу %q", ErrUnsafePath, rel)
	}
	return nil
}

func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// Validate — перевірка маніфесту, отриманого з мережі.
func (m *Manifest) Validate() error {
	if m.V != ProtocolVersion {
		return fmt.Errorf("непідтримувана версія протоколу передачі: %d (оновіть лаунчер)", m.V)
	}
	if m.Kind != KindBuild && m.Kind != KindServer {
		return fmt.Errorf("невідомий тип передачі %q", m.Kind)
	}
	if len(m.Name) == 0 || len(m.Name) > 200 {
		return errors.New("некоректна назва в маніфесті")
	}
	if len(m.Meta) > 32 {
		return errors.New("забагато метаданих у маніфесті")
	}
	for k, v := range m.Meta {
		if len(k) > 64 || len(v) > 2048 {
			return errors.New("задовге поле метаданих")
		}
	}
	if len(m.Files) > MaxFiles {
		return fmt.Errorf("забагато файлів у передачі: %d", len(m.Files))
	}
	seen := make(map[string]struct{}, len(m.Files))
	var total int64
	for _, f := range m.Files {
		if err := ValidateRelPath(f.Path); err != nil {
			return err
		}
		if f.Size < 0 || !isHex64(f.SHA256) {
			return fmt.Errorf("некоректний запис файлу %q", f.Path)
		}
		// Нечутливо до регістру: на Windows два шляхи, що різняться регістром, — один файл.
		k := strings.ToLower(f.Path)
		if _, dup := seen[k]; dup {
			return fmt.Errorf("дубль шляху в маніфесті: %q", f.Path)
		}
		seen[k] = struct{}{}
		total += f.Size
		if total > MaxTotalBytes {
			return errors.New("передача завелика")
		}
	}
	if total != m.TotalBytes {
		return errors.New("сума розмірів у маніфесті не збігається")
	}
	return nil
}
