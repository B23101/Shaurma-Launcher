package wardrobe

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"shaurma-launcher-wails/internal/atomicfile"
	"shaurma-launcher-wails/internal/model"
)

// Store — локальне сховище пресетів гардеробу (~/.shaurma/skins/presets.json).
//
// Пресети зберігаються ОКРЕМО для кожного ліцензійного (Microsoft) акаунта:
// `presets.json` має формат
//
//	{ "version": 2, "byAccount": { "<accountID>": [ SkinPreset, ... ], ... } }
//
// і при зміні активного акаунта гардероб показує саме пресети цього акаунта.
//
// Міграція: старий лаунчер писав плоский масив SkinPreset. Такий файл
// розпізнається при завантаженні і віддається першому акаунту, який
// відкриє гардероб (запозичення старого інвентаря без втрати даних),
// після чого зберігається вже у per-account форматі.
type Store struct {
	dir  string // ~/.shaurma/skins
	path string // .../presets.json

	mu        sync.Mutex
	byAccount map[string][]model.SkinPreset
	legacy    []model.SkinPreset // presets.json старого (плоского) формату
}

func NewStore(skinsDir string) *Store {
	s := &Store{
		dir:       skinsDir,
		path:      filepath.Join(skinsDir, "presets.json"),
		byAccount: map[string][]model.SkinPreset{},
	}
	_ = os.MkdirAll(skinsDir, 0o755)
	s.load()
	return s
}

type storeDoc struct {
	Version   int                        `json:"version"`
	ByAccount map[string][]model.SkinPreset `json:"byAccount"`
	Legacy    []model.SkinPreset         `json:"legacy,omitempty"`
}

func (s *Store) load() {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if err != nil {
		s.byAccount = map[string][]model.SkinPreset{}
		return
	}

	// Новий формат (v2): { version, byAccount, legacy }
	var doc storeDoc
	if json.Unmarshal(data, &doc) == nil && doc.ByAccount != nil {
		s.byAccount = doc.ByAccount
		s.legacy = doc.Legacy
		return
	}

	// Старий формат: плоский масив пресетів → legacy-інвентар.
	var list []model.SkinPreset
	if json.Unmarshal(data, &list) == nil && list != nil {
		s.byAccount = map[string][]model.SkinPreset{}
		s.legacy = list
		return
	}

	s.byAccount = map[string][]model.SkinPreset{}
}

func (s *Store) save() error {
	doc := storeDoc{
		Version:   2,
		ByAccount: s.byAccount,
		Legacy:    s.legacy,
	}
	// Атомарний запис (tmp + rename): краш посеред запису не має лишити
	// обрізаний presets.json — інакше гардероб «забув» би всі пресети.
	return atomicfile.WriteJSONAtomic(s.path, doc)
}

// List повертає пресети конкретного акаунта (копія, безпечна для фронтенду).
// Якщо в акаунта ще немає своїх пресетів, а в файлі лишився legacy-інвентар
// старого лаунчера — він "усиновлюється" цим акаунтом і файл переписується
// вже у per-account форматі.
func (s *Store) List(accountID string) []model.SkinPreset {
	s.mu.Lock()
	defer s.mu.Unlock()

	list := s.byAccount[accountID]
	if len(list) == 0 && len(s.legacy) > 0 {
		s.byAccount[accountID] = s.legacy
		s.legacy = nil
		list = s.byAccount[accountID]
		_ = s.save()
	}

	out := make([]model.SkinPreset, len(list))
	copy(out, list)
	return out
}

// Save додає новий пресет або оновлює існуючий за ID в межах акаунта.
//
// Дедуплікація: skinPath/capePath — це ~/.shaurma/skins/{sha256}.png,
// тобто однаковий вміст файлу завжди дає однаковий шлях (writeTexture).
// Тому "два однакових пресети" означає (skinPath, capePath, slimArms)
// вже присутні під ІНШИМ ID — у такому разі новий запис не створюється,
// повертається наявний пресет (як того вимагає правило "2 однакових
// пресетів бути не може").
func (s *Store) Save(accountID string, p model.SkinPreset) (model.SkinPreset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	list := s.byAccount[accountID]

	if p.ID == "" {
		p.ID = genID()
	}
	if p.CreatedAt == 0 {
		p.CreatedAt = time.Now().Unix()
	}

	for i, existing := range list {
		if existing.ID == p.ID {
			list[i] = p
			s.byAccount[accountID] = list
			return p, s.save()
		}
	}

	for _, existing := range list {
		if isSameLook(existing, p) {
			return existing, nil
		}
	}

	list = append(list, p)
	s.byAccount[accountID] = list
	return p, s.save()
}

// isSameLook порівнює "вигляд" двох пресетів: однакова текстура скіна,
// однаковий плащ (або обидва без плаща) і однаковий тип рук.
func isSameLook(a, b model.SkinPreset) bool {
	return a.SkinPath == b.SkinPath && a.CapePath == b.CapePath && a.SlimArms == b.SlimArms
}

// Delete видаляє пресет за ID в межах акаунта. Не повертає помилку, якщо
// ID не знайдено.
func (s *Store) Delete(accountID, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	list := s.byAccount[accountID]
	out := list[:0]
	for _, p := range list {
		if p.ID != id {
			out = append(out, p)
		}
	}
	s.byAccount[accountID] = out
	return s.save()
}

// SkinsDir — каталог, куди зберігаються PNG-файли скінів/плащів пресетів.
func (s *Store) SkinsDir() string { return s.dir }

func genID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "preset_" + time.Now().Format("20060102150405") + "_" + hex.EncodeToString(b)
}
