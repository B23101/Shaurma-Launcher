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
//	POST /v1/progress          одержувач повідомляє, скільки ПІДТВЕРДЖЕНО, швидкість
//	                           і фінальний стан (done | error + reason | cancelled)
//	POST /v1/decline           одержувач відмовився
//	GET  /v1/preview           той самий маніфест для перегляду ДО прийняття: стан
//	                           одержувача не змінює (новіші відправники; старі — 404)
//
// Файл маніфесту може мати src — ту саму річ на CDN Modrinth (знайдено відправником за
// sha1). Одержувач качає такі файли напряму з cdn.modrinth.com паралельно з P2P і звіряє
// з sha256 маніфесту; будь-який збій — той самий файл через P2P. Старі одержувачі src
// ігнорують. src не входить у Manifest.ID (вміст від нього не залежить).
//
// Закритий доступ — 410 Gone із заголовком X-Share-Reason (код Reason*), щоб
// одержувач показав точну причину, а не загальне «доступ закрито». Старі
// відправники заголовка не шлють — тоді причина ReasonAccess.
package share

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
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

	// HeaderReason — причина 410 Gone (див. Reason*).
	HeaderReason = "X-Share-Reason"
)

// Коди причин завершення передачі. Їх бачать обидві сторони (RecipientState.Reason,
// Progress.Reason), а UI перекладає код у текст, тож це стабільний контракт між
// версіями лаунчера (невідомий код показується як ReasonError).
const (
	ReasonDeclined          = "declined"           // одержувач відмовився
	ReasonCancelledSender   = "cancelled_sender"   // відправник скасував (усім або одному)
	ReasonCancelledReceiver = "cancelled_receiver" // одержувач зупинив прийом
	ReasonSenderOffline     = "sender_offline"     // відправник зник із мережі / закрив лаунчер
	ReasonReceiverOffline   = "receiver_offline"   // одержувач зник посеред передачі й не повернувся
	ReasonTimeout           = "timeout"            // одержувач так і не прийняв вчасно
	ReasonIntegrity         = "integrity"          // файл не пройшов перевірку sha256
	ReasonDiskFull          = "disk_full"          // бракує місця на диску одержувача
	ReasonChanged           = "changed"            // збірку змінили після надсилання
	ReasonAccess            = "access_closed"      // доступ закрито (причина невідома)
	ReasonError             = "error"              // інша помилка
	ReasonInterrupted       = "interrupted"        // лаунчер цієї сторони закрився/впав посеред передачі
)

// KnownReason — чи код причини відомий цій версії.
func KnownReason(r string) bool {
	switch r {
	case ReasonDeclined, ReasonCancelledSender, ReasonCancelledReceiver, ReasonSenderOffline,
		ReasonReceiverOffline, ReasonTimeout, ReasonIntegrity, ReasonDiskFull, ReasonChanged,
		ReasonAccess, ReasonError, ReasonInterrupted:
		return true
	}
	return false
}

// CleanReason — код причини з мережі: невідоме перетворюється на ReasonError.
func CleanReason(r string) string {
	if KnownReason(r) {
		return r
	}
	return ReasonError
}

// FileEntry — один файл передачі. Шлях завжди з '/' і відносний.
type FileEntry struct {
	Path   string `json:"p"`
	Size   int64  `json:"s"`
	SHA256 string `json:"h"`
	// Src — де ще лежить цей самий файл (необов'язково, див. опис пакета).
	Src *FileSource `json:"src,omitempty"`
}

// FileSource — публічне джерело файлу (зараз лише Modrinth).
type FileSource struct {
	// Modrinth — id проєкту, Version — id версії (для переходу в браузер контенту й іконки).
	Modrinth string `json:"mr"`
	Version  string `json:"mv,omitempty"`
	// URL — пряме посилання на файл; дозволено лише https://cdn.modrinth.com/…
	URL string `json:"u"`
}

// CDNHost — єдиний хост, з якого одержувач качає файли напряму. Обмеження важливе:
// інакше ворожий маніфест змусив би лаунчер ходити на довільні адреси (витік IP, SSRF).
const CDNHost = "cdn.modrinth.com"

var modrinthIDRe = regexp.MustCompile(`^[A-Za-z0-9]{1,24}$`)

// ValidCDNURL — https://cdn.modrinth.com/… без порту, логіна й фрагмента.
func ValidCDNURL(raw string) bool {
	if len(raw) == 0 || len(raw) > 1000 {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != CDNHost || u.User != nil || u.Fragment != "" || u.Opaque != "" {
		return false
	}
	return strings.HasPrefix(u.Path, "/")
}

// Valid — коректний id проєкту/версії і дозволений URL CDN.
func (s *FileSource) Valid() bool {
	return s != nil && modrinthIDRe.MatchString(s.Modrinth) && (s.Version == "" || modrinthIDRe.MatchString(s.Version)) && ValidCDNURL(s.URL)
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

// reservedNames — імена пристроїв Windows: файл «con.txt» чи «nul» не можна ні створити,
// ні видалити звичайними засобами, а «com1» відкриває порт. Порівнюємо без розширення.
var reservedNames = map[string]bool{
	"con": true, "prn": true, "aux": true, "nul": true, "conin$": true, "conout$": true,
	"com1": true, "com2": true, "com3": true, "com4": true, "com5": true, "com6": true, "com7": true, "com8": true, "com9": true,
	"lpt1": true, "lpt2": true, "lpt3": true, "lpt4": true, "lpt5": true, "lpt6": true, "lpt7": true, "lpt8": true, "lpt9": true,
}

func isReservedName(part string) bool {
	base := strings.ToLower(part)
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	return reservedNames[strings.TrimRight(base, " ")]
}

// ErrUnsafePath — шлях із маніфесту небезпечний.
var ErrUnsafePath = errors.New("небезпечний шлях у передачі")

// ValidateRelPath — шлях із мережі вважається ворожим: без виходу за теку,
// абсолютних шляхів, дисків, NTFS-потоків, '\\' та порожніх сегментів.
func ValidateRelPath(rel string) error {
	if rel == "" || len(rel) > MaxPathLen {
		return fmt.Errorf("%w: порожній або задовгий шлях", ErrUnsafePath)
	}
	// Керівні символи та <>:"|?* Windows не дозволяє в іменах (':' ловимо нижче окремо).
	if strings.ContainsAny(rel, "\\<>\"|?*") || strings.IndexFunc(rel, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
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
		case isReservedName(part):
			return fmt.Errorf("%w: зарезервоване ім'я %q", ErrUnsafePath, rel)
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
		if f.Src != nil && !f.Src.Valid() {
			return fmt.Errorf("некоректне джерело файлу %q", f.Path)
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
