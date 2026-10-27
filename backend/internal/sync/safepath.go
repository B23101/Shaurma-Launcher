package sync

// safepath.go — захист завантаження збірок.
//
// Усе, що приходить з маніфесту чи з архіву, вважається НЕДОВІРЕНИМ:
//   - шлях призначення не може вийти за межі теки збірки (path traversal,
//     zip-slip), бути абсолютним, містити диск/UNC чи NUL;
//   - у збірку не потрапляють виконувані файли Windows (.exe, .bat, .ps1 …) —
//     моди це .jar, а запускає гру лаунчер, не файли зі збірки;
//   - файл без жодного хешу не приймається (інакше його вміст нічим не
//     підтверджений);
//   - розмір ліміт: завантажений файл не може бути більшим за заявлений у
//     маніфесті, а архів — «бомбою» на гігабайти;
//   - при перенаправленні (redirect) токен воркера не йде на інший хост, а
//     https не знижується до http.

import (
	"archive/zip"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"
)

// Ліміти розпаковки override-архівів (config/, resourcepacks/ …).
const (
	maxExtractEntries    = 20000          // файлів в одному архіві
	maxExtractTotalBytes = 4 << 30        // 4 ГБ розпакованого
	maxExtractFileBytes  = 2 << 30        // 2 ГБ на один файл
	maxRedirects         = 5              // ланцюжок перенаправлень
	sizeSlackBytes       = 0              // допуск понад заявлений розмір
	maxUnknownSizeBytes  = int64(2) << 30 // стеля, якщо маніфест не дав розмір
)

// RequireHashes — відмовлятись від файлів без хешу в маніфесті. Лишається
// змінною лише для тестів; у проді завжди true.
var RequireHashes = true

// blockedExt — розширення, яким не місце у збірці Minecraft.
var blockedExt = map[string]bool{
	".exe": true, ".com": true, ".scr": true, ".pif": true, ".msi": true, ".msp": true,
	".bat": true, ".cmd": true, ".ps1": true, ".psm1": true, ".psd1": true,
	".vbs": true, ".vbe": true, ".wsf": true, ".wsh": true, ".hta": true,
	".lnk": true, ".cpl": true, ".reg": true, ".gadget": true, ".appx": true,
	".msix": true, ".application": true, ".jnlp": true,
}

// ErrUnsafePath — шлях із маніфесту/архіву небезпечний.
var ErrUnsafePath = errors.New("небезпечний шлях у збірці")

// ValidateRelPath перевіряє відносний шлях з маніфесту (без звернення до диска).
func ValidateRelPath(rel string) error {
	if rel == "" {
		return fmt.Errorf("%w: порожній шлях", ErrUnsafePath)
	}
	if strings.ContainsRune(rel, 0) {
		return fmt.Errorf("%w: NUL у шляху", ErrUnsafePath)
	}
	// Уніфікуємо роздільники: на Windows '\' теж роздільник, на Unix — ні,
	// тож перевіряємо найстрогіший варіант (обидва).
	norm := strings.ReplaceAll(rel, "\\", "/")
	if strings.HasPrefix(norm, "/") {
		return fmt.Errorf("%w: абсолютний шлях %q", ErrUnsafePath, rel)
	}
	if len(norm) >= 2 && norm[1] == ':' { // C:foo, C:/foo
		return fmt.Errorf("%w: шлях з диском %q", ErrUnsafePath, rel)
	}
	for _, part := range strings.Split(norm, "/") {
		if part == ".." {
			return fmt.Errorf("%w: вихід за межі теки %q", ErrUnsafePath, rel)
		}
		// NTFS-альтернативні потоки та кінцеві крапки/пробіли обходять фільтри.
		if strings.Contains(part, ":") {
			return fmt.Errorf("%w: двокрапка в імені %q", ErrUnsafePath, rel)
		}
	}
	if err := checkBlockedExt(norm); err != nil {
		return err
	}
	return nil
}

func checkBlockedExt(norm string) error {
	base := path.Base(norm)
	base = strings.TrimRight(base, ". ") // "evil.exe." / "evil.exe " на Windows = evil.exe
	if blockedExt[strings.ToLower(path.Ext(base))] {
		return fmt.Errorf("%w: виконуваний файл заборонено (%s)", ErrUnsafePath, path.Base(norm))
	}
	return nil
}

// SafeJoin склеює базову теку та відносний шлях з маніфесту і гарантує, що
// результат лежить усередині base.
func SafeJoin(base, rel string) (string, error) {
	if err := ValidateRelPath(rel); err != nil {
		return "", err
	}
	full := filepath.Join(base, filepath.FromSlash(strings.ReplaceAll(rel, "\\", "/")))
	return full, ensureInside(base, full)
}

// safeJoinDir — як SafeJoin, але для ТЕКИ (розширення не перевіряємо:
// "config" чи "resourcepacks" — не файл).
func safeJoinDir(base, rel string) (string, error) {
	norm := strings.ReplaceAll(rel, "\\", "/")
	if rel == "" || strings.ContainsRune(rel, 0) || strings.HasPrefix(norm, "/") ||
		(len(norm) >= 2 && norm[1] == ':') {
		return "", fmt.Errorf("%w: %q", ErrUnsafePath, rel)
	}
	for _, p := range strings.Split(norm, "/") {
		if p == ".." || strings.Contains(p, ":") {
			return "", fmt.Errorf("%w: %q", ErrUnsafePath, rel)
		}
	}
	full := filepath.Join(base, filepath.FromSlash(norm))
	if filepath.Clean(full) == filepath.Clean(base) {
		// "." / "a/.." — це сама тека збірки: її не можна чистити/замінювати.
		return "", fmt.Errorf("%w: %q збігається з текою збірки", ErrUnsafePath, rel)
	}
	return full, ensureInside(base, full)
}

func ensureInside(base, full string) error {
	b := filepath.Clean(base)
	rel, err := filepath.Rel(b, filepath.Clean(full))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("%w: %q виходить за %q", ErrUnsafePath, full, base)
	}
	return nil
}

// hasAnyHash — чи є хоч один хеш, яким можна підтвердити файл.
func hasAnyHash(t Task) bool {
	return t.SHA1 != "" || t.SHA256 != "" || t.SHA512 != ""
}

// maxAllowedBytes — скільки байтів маємо право прийняти для файлу.
func maxAllowedBytes(t Task) int64 {
	if t.SizeBytes > 0 {
		return t.SizeBytes + sizeSlackBytes
	}
	return maxUnknownSizeBytes
}

// clientWithSafeRedirects повертає копію клієнта з безпечною політикою
// перенаправлень: не більше maxRedirects, лише https (або той самий
// http-хост, що в запиті, для локальних тестів) і без витоку токена воркера
// на інший хост.
func clientWithSafeRedirects(c *http.Client) *http.Client {
	cp := *c
	cp.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("забагато перенаправлень (%d)", len(via))
		}
		prev := via[len(via)-1]
		if prev.URL.Scheme == "https" && req.URL.Scheme != "https" {
			return fmt.Errorf("перенаправлення з https на %s заборонено", req.URL.Scheme)
		}
		if !strings.EqualFold(req.URL.Host, via[0].URL.Host) {
			req.Header.Del("X-Shaurma-Token")
			req.Header.Del("X-Shaurma-Auth")
			req.Header.Del("Authorization")
			req.Header.Del("Cookie")
		}
		return nil
	}
	return &cp
}

// checkDownloadURL — зовнішні URL (CDN модів) лише https; http дозволений
// тільки для localhost (локальні тести/dev).
func checkDownloadURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("некоректне посилання на завантаження")
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if host == "localhost" {
			return nil
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return nil
		}
	}
	return fmt.Errorf("небезпечне посилання (дозволено лише https): %s", u.Scheme+"://"+u.Host)
}

// isBlockedEntry — запис архіву з виконуваним розширенням: такі файли не
// розпаковуємо (пропускаємо), решта архіву ставиться як зазвичай.
func isBlockedEntry(name string) bool {
	return checkBlockedExt(strings.ReplaceAll(name, "\\", "/")) != nil
}

// vetZipEntries перевіряє весь архів ДО запису на диск: жодного виходу за
// теку (zip-slip), без «бомби» за кількістю та розміром.
func vetZipEntries(files []*zip.File) error {
	if len(files) > maxExtractEntries {
		return fmt.Errorf("забагато файлів в архіві (%d)", len(files))
	}
	var total uint64
	for _, f := range files {
		name := strings.ReplaceAll(f.Name, "\\", "/")
		if strings.ContainsRune(name, 0) || strings.HasPrefix(name, "/") ||
			(len(name) >= 2 && name[1] == ':') {
			return fmt.Errorf("%w: %q", ErrUnsafePath, f.Name)
		}
		for _, p := range strings.Split(name, "/") {
			if p == ".." || strings.Contains(p, ":") {
				return fmt.Errorf("%w: %q", ErrUnsafePath, f.Name)
			}
		}
		if f.Mode()&0o170000 == 0o120000 { // символічне посилання
			return fmt.Errorf("%w: символічне посилання %q", ErrUnsafePath, f.Name)
		}
		if f.UncompressedSize64 > maxExtractFileBytes {
			return fmt.Errorf("файл %q завеликий (%d байт)", f.Name, f.UncompressedSize64)
		}
		total += f.UncompressedSize64
		if total > maxExtractTotalBytes {
			return fmt.Errorf("архів розпаковується в понад %d байт", uint64(maxExtractTotalBytes))
		}
	}
	return nil
}

// IsBlockedExecutable — чи є ім'я файлу виконуваним (для імпорту збірок у
// пакеті custompack, який не імпортує внутрішні функції цього пакета).
func IsBlockedExecutable(name string) bool { return isBlockedEntry(name) }
