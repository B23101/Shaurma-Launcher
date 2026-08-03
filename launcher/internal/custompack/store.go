package custompack

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-wails/internal/atomicfile"
)

// Pack — повний опис кастомної (не-Shaurma) збірки користувача. Джерело
// правди для сторінки "Нова збірка" та для картки/сторінки збірки в
// "Моїх збірках" (розділ instanceTab === 'custom').
type Pack struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	IconPath    string `json:"iconPath,omitempty"`       // локальний шлях до іконки (256×256), скопійований у теку збірки
	BackgroundPath string `json:"backgroundPath,omitempty"` // локальний шлях до фону (16:9)
	Icon        string `json:"icon,omitempty"`           // пресет-іконка (Tabler, напр. "ti-puzzle"); PNG з IconPath має пріоритет
	Color       string `json:"color,omitempty"`          // hex, підсвітка картки/glow

	MCVersion     string `json:"mcVersion"`
	Loader        string `json:"loader"`        // vanilla | fabric | forge | neoforge | quilt
	LoaderVersion string `json:"loaderVersion,omitempty"`

	// RAM: якщо UseCustomRAM=false — успадковує глобальні налаштування
	// лаунчера (як і override-и звичайних збірок).
	UseCustomRAM bool `json:"useCustomRam"`
	MinRAMMB     int  `json:"minRamMb,omitempty"`
	MaxRAMMB     int  `json:"maxRamMb,omitempty"`

	UseSeparateJava bool   `json:"useSeparateJava"`
	JavaPath        string `json:"javaPath,omitempty"`

	// Source — звідки з'явилась збірка: "manual" (створена вручну) або
	// "import" (з .mrpack/.zip). Для import також зберігаємо назву
	// вихідного файлу — суто інформативно, для сторінки збірки.
	Source     string `json:"source"` // manual | import
	ImportFile string `json:"importFile,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Store — персистентний реєстр кастомних збірок (custom-packs.json у
// теці конфігурації лаунчера). Окремий файл від builds.json
// (builds.Registry) — там лише InstalledVersion/InstalledAt для стану
// "потрібне оновлення", тут — повний редагований опис збірки.
type Store struct {
	mu   sync.Mutex
	path string
}

func NewStore(configDir string) *Store {
	return &Store{path: filepath.Join(configDir, "custom-packs.json")}
}

func (s *Store) load() map[string]Pack {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return map[string]Pack{}
	}
	var list []Pack
	if err := json.Unmarshal(data, &list); err != nil {
		return map[string]Pack{}
	}
	out := make(map[string]Pack, len(list))
	for _, p := range list {
		out[p.ID] = p
	}
	return out
}

func (s *Store) saveLocked(all map[string]Pack) error {
	list := make([]Pack, 0, len(all))
	for _, p := range all {
		list = append(list, p)
	}
	// Атомарний запис (tmp + rename): краш посеред запису не має лишити
	// обрізаний custom-packs.json — інакше лаунчер «забув» би кастомні збірки.
	return atomicfile.WriteJSONAtomic(s.path, list)
}

// List повертає всі кастомні збірки користувача (порядок не гарантується
// — фронтенд сортує сам за потреби).
func (s *Store) List() []Pack {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.load()
	out := make([]Pack, 0, len(all))
	for _, p := range all {
		out = append(out, p)
	}
	return out
}

// Get повертає одну кастомну збірку за ID.
func (s *Store) Get(id string) (Pack, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.load()
	p, ok := all[id]
	return p, ok
}

// Save створює/оновлює запис збірки.
func (s *Store) Save(p Pack) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.load()
	if _, exists := all[p.ID]; !exists {
		p.CreatedAt = time.Now()
	} else {
		p.CreatedAt = all[p.ID].CreatedAt
	}
	p.UpdatedAt = time.Now()
	all[p.ID] = p
	return s.saveLocked(all)
}

// Delete видаляє запис збірки з реєстру (файли на диску прибирає викликач).
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.load()
	delete(all, id)
	return s.saveLocked(all)
}

// slugInvalidRe — символи, які НЕ допустимі в іменах тек Windows.
// На відміну від старої версії (що транслітерувала все в a-z0-9), тут ми
// тримаємо пробіли й кирилицю (як Prism Launcher): ID береться з назви
// збірки, а прибираються лише символи, які файлова система просто не
// прийме в імені папки.
var slugInvalidRe = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]+`)

// SlugifyName перетворює назву збірки на ID для теки на диску — той самий
// підхід, що в Prism Launcher: якщо індифікатор не задано, він береться з
// назви збірки, і неважливо, що там пробіли чи кирилиця — головне прибрати
// проблемні символи. Назва "Моя збірка (2)?" → "Моя збірка (2)". Якщо після
// прибирання лишився порожній рядок (напр. лише символи) — "pack".
func SlugifyName(name string) string {
	s := strings.TrimSpace(name)
	s = slugInvalidRe.ReplaceAllString(s, "")
	// Windows не приймає назви, що закінчуються крапкою чи пробілом.
	s = strings.Trim(s, " .")
	if s == "" {
		s = "pack"
	}
	return s
}

// UniqueID повертає slug назви, з числовим суфіксом при колізії
// ("skyblock-adventures", "skyblock-adventures-2", ...) — картки й теки
// на диску не повинні плутатись між збірками з однаковою назвою.
func (s *Store) UniqueID(name string) string {
	base := SlugifyName(name)
	s.mu.Lock()
	all := s.load()
	s.mu.Unlock()
	if _, taken := all[base]; !taken {
		return base
	}
	for i := 2; i < 1000; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if _, taken := all[candidate]; !taken {
			return candidate
		}
	}
	// Практично недосяжно, але про всяк випадок — випадковий хвіст.
	return base + "-" + randomHex(4)
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "x"
	}
	return hex.EncodeToString(b)
}
