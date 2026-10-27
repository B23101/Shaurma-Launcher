// Package manifest описує launcher-version.json — контракт між
// shrm-updater і CDN (worker.js, шлях launcher/launcher-version.json).
//
// Формат навмисно мінімальний: раніше (JavaFX-ера) лаунчер складався з
// купи файлів (jar, kілька exe, css) і манiфест описував масив
// компонентів. Тепер увесь лаунчер — ОДИН файл launcher.exe (Wails,
// embed frontend), тож компонент рівно один. Масив лишили (не одиничне
// поле) свідомо: якщо в майбутньому з'явиться другий незалежно
// оновлюваний файл (напр. окремий ресурс), формат не доведеться міняти
// і pack-manager/worker лишаються сумісні без правок.
package manifest

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Component — один компонент поставки лаунчера.
//
// kind:
//
//	""      — звичайний файл (за замовчуванням);
//	"file"  — те саме, явно;
//	"archive" — zip-архів, розпаковується в extractTo (напр. frontend.zip
//	            -> installDir/frontend/). Сам архів після розпаковування
//	            також зберігається на місці компонента (щоб наступний
//	            Diff звірив його за sha256 і не перекачував заново).
type Component struct {
	Path      string `json:"path"`   // відносний шлях від installDir, напр. "backend.dll"
	SHA256    string `json:"sha256"` // hex-encoded
	Size      int64  `json:"size"`
	URL       string `json:"url"`                 // відносний до CDN base, напр. "/launcher/backend.dll"
	Kind      string `json:"kind,omitempty"`      // "" | "file" | "archive"
	ExtractTo string `json:"extractTo,omitempty"` // для kind=archive: цільова тека в installDir
}

// Manifest — launcher-version.json, і локальний, і віддалений (CDN).
type Manifest struct {
	Version      string      `json:"version"`
	ReleaseNotes string      `json:"releaseNotes"`
	Components   []Component `json:"components"`
}

// Load читає локальний launcher-version.json. Відсутність файлу (перший
// запуск: немає ні launcher.exe, ні цього маніфесту в installDir) — НЕ
// помилка, повертаємо порожній Manifest{} і nil: виклик має трактувати
// це як "качати все з нуля".
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Manifest{}, nil
	}
	if err != nil {
		return nil, err
	}
	// build.ps1/installer пишуть launcher-version.json з UTF-8 BOM
	// (U+FEFF), а Go json.Unmarshal на BOM падає. Якби не прибирали BOM,
	// маніфест вважався б пошкодженим -> "нічого не встановлено" ->
	// updater перекачував би launcher.exe при кожному запуску.
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		// Пошкоджений локальний маніфест не повинен блокувати
		// оновлення назавжди — трактуємо як "нічого не встановлено".
		return &Manifest{}, nil
	}
	return &m, nil
}

// Save атомарно перезаписує launcher-version.json (пишемо у .tmp і
// переносимо — щоб крах процесу посеред запису не лишив биту чернетку,
// через яку наступний запуск подумав би, що нічого не встановлено, і
// перекачав все заново).
func Save(path string, m *Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Diff повертає компоненти, які треба завантажити, щоб оновитись до
// remote-версії. Версія — ЛИШЕ тригер («є нова версія чи ні»), а САМ
// список качки визначається за вмістом (sha256/розмір):
//
//	remote новіша за local → качаємо ЛИШЕ компоненти, які реально
//	змінились (sha256 або розмір відрізняється від локального маніфесту,
//	або компонент у local взагалі відсутній);
//	remote та сама або старіша → нічого (актуальна версія).
//
// Це означає: якщо в новій версії змінився лише backend.dll, frontend.zip
// (багато мегабайт) НЕ перекачується заново. Раніше Diff повертав ВСІ
// компоненти при будь-якому бампі версії — кожне оновлення качало все
// з нуля («усе видаляє і качає, навіть якщо там нічого не оновилося»).
//
// Безпечно повертати sha-диференс саме зараз: качалка НЕ звіряє sha256
// після завантаження (download.Download), тож неузгоджений CDN не може
// зламати качку на півшляху — на відміну від старого sha-Diff, де
// download падав на SHA mismatch і оновлення зупинялось до
// updater-frontend.zip.
func Diff(local, remote *Manifest) []Component {
	if CompareVersions(remote.Version, local.Version) <= 0 {
		return nil
	}
	localByPath := make(map[string]Component, len(local.Components))
	for _, c := range local.Components {
		localByPath[filepath.ToSlash(c.Path)] = c
	}
	var out []Component
	for _, rc := range remote.Components {
		lc, ok := localByPath[filepath.ToSlash(rc.Path)]
		if !ok || !strings.EqualFold(lc.SHA256, rc.SHA256) || lc.Size != rc.Size {
			out = append(out, rc)
		}
	}
	return out
}

// SafeRelPath перевіряє, що p — відносний шлях ВСЕРЕДИНІ installDir:
// без абсолютного шляху, диска/«:», «..» та зворотних слешів-обходів.
// Маніфест — недовірений вхід: навіть підписаний, він не повинен мати змоги
// вивести запис за межі теки лаунчера (захист у глибину).
func SafeRelPath(p string) bool {
	if p == "" || strings.ContainsAny(p, ":\x00") || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) {
		return false
	}
	for _, seg := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." || seg == "." {
			return false
		}
	}
	return true
}

// SafeComponentURL перевіряє відносну адресу компонента: це має бути шлях
// на тому самому CDN ("/launcher/..."), а не чужий хост. Конкатенація
// baseURL+URL з URL на кшталт "@evil.com/x" перетворила б хост на evil.com
// (userinfo-трюк) і віддала б йому токен каналу.
func SafeComponentURL(u string) bool {
	if !strings.HasPrefix(u, "/") || strings.HasPrefix(u, "//") {
		return false
	}
	if strings.ContainsAny(u, "@\\\x00 \t\r\n#?") {
		return false
	}
	for _, seg := range strings.Split(u, "/") {
		if seg == ".." {
			return false
		}
	}
	return true
}

// Validate відхиляє маніфест із небезпечними шляхами/адресами/хешами.
// Викликається одразу після розбору віддаленого маніфесту — ДО Diff і
// будь-якого завантаження.
func (m *Manifest) Validate() error {
	for _, c := range m.Components {
		if !SafeRelPath(c.Path) {
			return fmt.Errorf("небезпечний шлях компонента %q", c.Path)
		}
		if !SafeComponentURL(c.URL) {
			return fmt.Errorf("небезпечна адреса компонента %q", c.URL)
		}
		if c.Kind == "archive" && !SafeRelPath(c.ExtractTo) {
			return fmt.Errorf("небезпечна тека розпакування %q", c.ExtractTo)
		}
		if len(c.SHA256) != 64 {
			return fmt.Errorf("компонент %q: sha256 має бути 64 hex-символи", c.Path)
		}
		if _, err := hex.DecodeString(c.SHA256); err != nil {
			return fmt.Errorf("компонент %q: sha256 не hex", c.Path)
		}
		if c.Size <= 0 {
			return fmt.Errorf("компонент %q: розмір має бути додатнім", c.Path)
		}
	}
	return nil
}

// IsLockedComponent — чи файл належить процесу, що зараз виконується
// (updater.dll/updater.so — ця сама бібліотека; shell.exe/shell — поточний
// виконуваний файл). Такі файли замінюються через <name>.new, не на місці.
func IsLockedComponent(path string) bool {
	base := filepath.Base(path)
	if base == UpdaterLibraryName() {
		return true
	}
	if exe, err := os.Executable(); err == nil {
		if base == filepath.Base(exe) {
			return true
		}
	}
	return false
}

// UpdaterLibraryName — ім'я файлу апдейтера для поточної ОС: "updater.dll"
// на Windows, "updater.so" на Linux (та сама c-shared збірка,
// -buildmode=c-shared природно дає розширення .so поза Windows). Раніше
// це була константа "updater.dll" — на Linux вона б ніколи не збіглася з
// filepath.Base(path), тож IsLockedComponent мовчки повертав би false для
// updater.so і той намагався б замінитись НА МІСЦІ, поки процес сам його
// виконує (crash/зіпсований файл), замість коректного шляху через .new.
func UpdaterLibraryName() string {
	if runtime.GOOS == "windows" {
		return "updater.dll"
	}
	return "updater.so"
}

// PlatformSegment — сегмент шляху URL-запиту маніфесту для поточної ОС:
// "" (порожньо, зворотна сумісність) на Windows, "linux/" на Linux.
// Windows свідомо лишається на КОРЕНІ /launcher/ — там уже роками лежать
// задеплоєні launcher-version.json (Частина 4.2 плану), переносити їх під
// /launcher/windows/ означало б синхронно міняти й worker.js, і R2-бакет,
// і всіх існуючих Windows-клієнтів одночасно. Замість цього тільки НОВА
// платформа (Linux) отримує власний підпрефікс — Windows-контракт
// лишається побайтово тим самим, яким був до підтримки Linux.
func PlatformSegment() string {
	if runtime.GOOS == "windows" {
		return ""
	}
	return runtime.GOOS + "/"
}

// ManifestURLPath будує відносний (від CDNBaseURL) шлях запиту одного
// файлу маніфесту, з урахуванням платформи: "launcher/launcher-version.json"
// на Windows (як і завжди), "launcher/linux/launcher-version.json" на
// Linux. fileName — "launcher-version.json" або "launcher-version-beta.json".
func ManifestURLPath(fileName string) string {
	return "launcher/" + PlatformSegment() + fileName
}

// CompareVersions — семверне порівняння "a.b.c[.d...]" рядків (без префікса
// v і pre-release суфіксів). Повертає >0 якщо v1 новіша за v2, <0 якщо
// старіша, 0 якщо рівні. Некоректні сегменти трактуються як 0, тож
// "2.0" == "2.0.0".
//
// ВАЖЛИВО: порівнюються ВСІ сегменти, не лише 3. Четвертий сегмент — це
// бета-суфікс схеми версій лаунчера ("2.1.5.1" → beta вища за stable
// "2.1.5"), а ТОЧКОВИЙ 0 рівнозначний відсутності ("2.1.5.0" == "2.1.5"):
// pack-manager публікує стабільну збірку як X.Y.Z.0 і вона МАЄ вважатись
// стабільною, а не «меншою» за X.Y.Z.
func CompareVersions(v1, v2 string) int {
	p1, p2 := splitVersion(v1), splitVersion(v2)
	n := len(p1)
	if len(p2) > n {
		n = len(p2)
	}
	for i := 0; i < n; i++ {
		a, b := 0, 0
		if i < len(p1) {
			a = p1[i]
		}
		if i < len(p2) {
			b = p2[i]
		}
		if a != b {
			if a > b {
				return 1
			}
			return -1
		}
	}
	return 0
}

func splitVersion(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	parts := strings.SplitN(v, "-", 2)[0] // відкидаємо pre-release суфікс типу "-beta.1"
	segs := strings.Split(parts, ".")
	out := make([]int, 0, len(segs))
	for _, s := range segs {
		n, err := strconv.Atoi(s)
		if err != nil {
			n = 0
		}
		out = append(out, n)
	}
	return out
}
