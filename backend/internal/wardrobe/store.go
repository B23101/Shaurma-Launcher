package wardrobe

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/atomicfile"
	"shaurma-launcher-backend/internal/model"
	"shaurma-launcher-backend/internal/store"
)

// Store — локальне сховище пресетів гардеробу. Джерело правди — SQLite
// (таблиця wardrobe_presets, один JSON-блоб на акаунт); файловий фолбек
// (st == nil) — для тестів.
//
// Пресети зберігаються ОКРЕМО для кожного ліцензійного (Microsoft) акаунта:
// формат блоба —
//
//	{ "version": 2, "byAccount": { "<accountID>": [ SkinPreset, ... ], ... } }
//
// і при зміні активного акаунта гардероб показує саме пресети цього акаунта.
//
// Міграція: старий лаунчер писав плоский масив SkinPreset. Такий документ
// розпізнається при завантаженні і віддається першому акаунту, який
// відкриє гардероб (запозичення старого інвентаря без втрати даних),
// після чого зберігається вже у per-account форматі.
type Store struct {
	dir  string // теку, де лежать PNG-файли скінів/плащів (кеш)
	path string // файловий фолбек (st == nil): .../presets.json
	st   *store.Store

	mu        sync.Mutex
	username  string // акаунт, чиї пресети завантажені в пам'ять
	byAccount map[string][]model.SkinPreset
	legacy    []model.SkinPreset // presets.json старого (плоского) формату
}

// NewStore створює сховище з файловим бекендом (тести/dev).
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

// NewStoreDB створює сховище з бекендом у SQLite (таблиця wardrobe_presets).
// Блоб на акаунт зберігається в БД; PNG-файли скінів — як і раніше в
// skinsDir (це кеш, а не «дані»).
func NewStoreDB(st *store.Store, skinsDir string) *Store {
	s := &Store{
		dir:       skinsDir,
		st:        st,
		byAccount: map[string][]model.SkinPreset{},
	}
	_ = os.MkdirAll(skinsDir, 0o755)
	s.load()
	return s
}

type storeDoc struct {
	Version   int                           `json:"version"`
	ByAccount map[string][]model.SkinPreset `json:"byAccount"`
	Legacy    []model.SkinPreset            `json:"legacy,omitempty"`
}

func (s *Store) load() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loadLocked()
}

// loadLocked читає пресети з БД (блоб акаунта = byAccount[accountID])
// або з файлу. Для БД весь byAccount зберігається як ОДИН блоб на акаунт
// (кожен акаунт — окремий рядок), тому в пам'ять завантажуємо лише
// активний акаунт; legacy-інвентар (з файлу) лишається у файловому режимі.
func (s *Store) loadLocked() {
	if s.st != nil {
		// БД: шукаємо будь-який збережений блоб — їх може бути кілька
		// (по акаунту), тому тягнемо всі і збираємо в byAccount.
		all, err := s.st.LoadAllWardrobePresets()
		if err != nil {
			s.byAccount = map[string][]model.SkinPreset{}
			return
		}
		out := map[string][]model.SkinPreset{}
		legacy := []model.SkinPreset{}
		for uname, data := range all {
			var doc storeDoc
			if json.Unmarshal([]byte(data), &doc) == nil && doc.ByAccount != nil {
				for name, presets := range doc.ByAccount {
					out[name] = presets
				}
				legacy = append(legacy, doc.Legacy...)
				continue
			}
			// Старий формат у блобі: плоский масив.
			var list []model.SkinPreset
			if json.Unmarshal([]byte(data), &list) == nil && list != nil {
				out[uname] = list
			}
		}
		s.byAccount = out
		s.legacy = legacy
		return
	}
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
	if s.st != nil {
		// В БД кожен акаунт зберігаємо окремим рядком з ЙОГО блобом (за ніком).
		for uname, presets := range s.byAccount {
			sub := storeDoc{Version: 2, ByAccount: map[string][]model.SkinPreset{uname: presets}, Legacy: s.legacy}
			subData, err := json.Marshal(sub)
			if err != nil {
				return err
			}
			if err := s.st.SaveWardrobePresets(uname, string(subData)); err != nil {
				return err
			}
		}
		return nil
	}
	// Атомарний запис (tmp + rename): краш посеред запису не має лишити
	// обрізаний presets.json — інакше гардероб «забув» би всі пресети.
	return atomicfile.WriteJSONAtomic(s.path, doc)
}

// List повертає пресети конкретного акаунта (копія, безпечна для фронтенду).
// Якщо в акаунта ще немає своїх пресетів, а в файлі лишився legacy-інвентар
// старого лаунчера — він "усиновлюється" цим акаунтом і файл переписується
// вже у per-account форматі.
func (s *Store) List(username string) []model.SkinPreset {
	s.mu.Lock()
	defer s.mu.Unlock()

	list := s.byAccount[username]
	if len(list) == 0 && len(s.legacy) > 0 {
		s.byAccount[username] = s.legacy
		s.legacy = nil
		list = s.byAccount[username]
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
func (s *Store) Save(username string, p model.SkinPreset) (model.SkinPreset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	list := s.byAccount[username]

	if p.ID == "" {
		p.ID = genID()
	}
	if p.CreatedAt == 0 {
		p.CreatedAt = time.Now().Unix()
	}

	for i, existing := range list {
		if existing.ID == p.ID {
		list[i] = p
		s.byAccount[username] = list
		return p, s.save()
		}
	}

	for _, existing := range list {
		if isSameLook(existing, p) {
			return existing, nil
		}
	}

	list = append(list, p)
	s.byAccount[username] = list
	return p, s.save()
}

// isSameLook порівнює "вигляд" двох пресетів: однакова текстура скіна,
// однаковий плащ (або обидва без плаща) і однаковий тип рук.
func isSameLook(a, b model.SkinPreset) bool {
	return a.SkinPath == b.SkinPath && a.CapePath == b.CapePath && a.SlimArms == b.SlimArms
}

// Reorder переставляє пресети акаунта у порядок, заданий списком presetIDs
// (повний список, не дельта). Відсутні у списку пресети лишаються в кінці
// у старому порядку; невідомі ID ігноруються. Зберігає файл і повертає
// новий (уже впорядкований) список.
func (s *Store) Reorder(username string, presetIDs []string) []model.SkinPreset {
	s.mu.Lock()
	defer s.mu.Unlock()

	list := s.byAccount[username]
	order := make(map[string]int, len(presetIDs))
	for i, id := range presetIDs {
		order[id] = i
	}

	// Стабільне сортування за індексом у новому порядку; пресети, яких
	// немає у presetIDs (напр. щойно доданий), отримують великий індекс і
	// лишаються в кінці у старому відносному порядку.
	sorted := make([]model.SkinPreset, len(list))
	copy(sorted, list)
	sort.SliceStable(sorted, func(i, j int) bool {
		ai, aok := order[sorted[i].ID]
		bi, bok := order[sorted[j].ID]
		if aok && bok {
			return ai < bi
		}
		if aok {
			return true
		}
		if bok {
			return false
		}
		return false // обидва невідомі — стабільний порядок зберігає поточний
	})

	s.byAccount[username] = sorted
	_ = s.save()
	return sorted
}

// Delete видаляє пресет за ID в межах акаунта. Не повертає помилку, якщо
// ID не знайдено.
func (s *Store) Delete(username, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	list := s.byAccount[username]
	out := list[:0]
	for _, p := range list {
		if p.ID != id {
			out = append(out, p)
		}
	}
	s.byAccount[username] = out
	return s.save()
}

// RenameAccount переносить усі пресети з одного ключа (старий нік офлайн-
// акаунта) на інший (новий нік) — викликається при перейменуванні офлайн-
// акаунта (див. App.RenameOfflineAccount), щоб гардероб не "загубив"
// збережені пресети через те, що вони лежали під старим ніком.
// Якщо новий нік уже мав свої пресети — старі дописуються ПІСЛЯ них
// (дедуп у Save тут не застосовується: це прямий перенос списку).
// no-op, якщо oldUsername == newUsername або в старого ніка нема пресетів.
func (s *Store) RenameAccount(oldUsername, newUsername string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if oldUsername == "" || newUsername == "" || oldUsername == newUsername {
		return nil
	}
	old := s.byAccount[oldUsername]
	if len(old) == 0 {
		return nil
	}
	s.byAccount[newUsername] = append(s.byAccount[newUsername], old...)
	delete(s.byAccount, oldUsername)
	return s.save()
}

// SkinsDir — каталог, куди зберігаються PNG-файли скінів/плащів пресетів.
func (s *Store) SkinsDir() string { return s.dir }

// AllReferencedTextures повертає всі шляхи до PNG (skin/cape), на які
// посилається ХОЧА Б один пресет (будь-якого акаунта, включно з legacy).
// Використовується maintenance-ботом: файл у skins/, якого тут НЕМАЄ, —
// сирота (його останній пресет видалили), його можна безпечно прибрати.
// Один і той самий PNG може згадуватись у кількох пресетах (текстури
// дедуплікуються за вмістом), тому саме цей перелік — джерело правди.
func (s *Store) AllReferencedTextures() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	seen := map[string]struct{}{}
	add := func(p model.SkinPreset) {
		if p.SkinPath != "" {
			seen[p.SkinPath] = struct{}{}
		}
		if p.CapePath != "" {
			seen[p.CapePath] = struct{}{}
		}
	}
	for _, presets := range s.byAccount {
		for _, p := range presets {
			add(p)
		}
	}
	for _, p := range s.legacy {
		add(p)
	}

	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	return out
}

func genID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "preset_" + time.Now().Format("20060102150405") + "_" + hex.EncodeToString(b)
}
