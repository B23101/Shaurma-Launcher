// Package store — JSON-сховище даних лаунчера.
//
// Архітектура:
//   - shaurma.json (installDir) — ГОЛОВНИЙ файл: settings, accounts, playtime,
//     sessions, registry, meta, gallery, shortcuts, AI cache тощо.
//   - <instanceDir>/<packId>/instance.json — per-instance конфіг збірки.
//
// Атомарний запис (.tmp + rename). Бекапи: не створюються для пустих
// даних. Конвертація зі старого SQLite: при першому запуску shaurma.db
// конвертується в shaurma.json + instance.json, після чого .db ВИДАЛЯЄТЬСЯ.
package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/atomicfile"
	"shaurma-launcher-backend/internal/model"
)

// FileName — ім'я головного JSON-файлу даних (поруч із DLL).
const FileName = "shaurma.json"

// InstanceFileName — ім'я per-instance конфігу.
const InstanceFileName = "instance.json"

// LayoutFileName — файл сортування sidebar (поруч із installations).
const LayoutFileName = ".build-layout.json"

// UIStateFileName — файл «останнього вікна» (поруч із installations,
// той самий каталог, що LayoutFileName — обидва змінюються за життя
// сесії частіше, ніж shaurma.json).
const UIStateFileName = ".ui-state.json"

// BookmarksFileName — файл закладок браузера контенту (поруч із
// installations, окремо від shaurma.json — закладки не пов'язані з
// жодною конкретною збіркою, і читаються/пишуться незалежно від
// головного файлу налаштувань).
const BookmarksFileName = ".bookmarks.json"

// SearchHistoryFileName — файл історії пошукових запитів браузера
// контенту (лише ті, що ДАЛИ результат — див. коментар на SearchHistoryEntry).
const SearchHistoryFileName = ".search-history.json"

// BackupSuffix — суфікс резервної копії.
const BackupSuffix = ".bak"

// BackupMaxAge — максимальний вік резервної копії (12 годин).
const BackupMaxAge = 12 * time.Hour

// ── Типи даних ─────────────────────────────────────────────────────

// DBHealth — стан здоров'я при відкритті.
type DBHealth struct {
	RestoredFromBackup  bool   `json:"restoredFromBackup"`
	BackupAt            string `json:"backupAt"`
	ErasedIntentionally bool   `json:"erasedIntentionally"`
	ErasedAt            string `json:"erasedAt"`
	MigratedFromDB      bool   `json:"migratedFromDB"`
}

// globalData — структура shaurma.json (всі дані лаунчера).
type globalData struct {
	Settings          string                  `json:"settings,omitempty"`
	MinecraftDefaults string                  `json:"minecraftDefaults,omitempty"`
	Accounts          []model.Account         `json:"accounts,omitempty"`
	Playtime          map[string]int64        `json:"playtime,omitempty"`
	PlaytimeSessions  []PlaytimeSession       `json:"playtimeSessions,omitempty"`
	SetupProgress     *model.SetupProgress    `json:"setupProgress,omitempty"`
	Meta              map[string]string       `json:"meta,omitempty"`
	BuildsRegistry    []RegistryEntry         `json:"buildsRegistry,omitempty"`
	GalleryMeta       []GalleryMetaEntry      `json:"galleryMeta,omitempty"`
	Shortcuts         map[string]string       `json:"shortcuts,omitempty"`
	AICache           map[string]aiCacheEntry `json:"aiCache,omitempty"`
	WardrobePresets   map[string]string       `json:"wardrobePresets,omitempty"`
	ExportSettings    map[string]string       `json:"exportSettings,omitempty"`
	DuplicateSettings map[string]string       `json:"duplicateSettings,omitempty"`
}

// BuildLayout — порядок збірок у sidebar (окремий файл).
type BuildLayout struct {
	PackOrder   []string           `json:"packOrder,omitempty"`
	PackPinned  []string           `json:"packPinned,omitempty"`
	PackFolders []model.PackFolder `json:"packFolders,omitempty"`
	// ServerFolders — папки серверів (Бібліотека серверів).
	ServerFolders []model.ServerFolder `json:"serverFolders,omitempty"`
}

// UIState — «останнє вікно»: де зупинився користувач (сторінка, вкладка
// перемикача збірок, вкладка налаштувань, відкрита збірка з вкладкою і
// скролом по розділах, скрол кожної сторінки). Окремий файл
// (.ui-state.json), НЕ shaurma.json — пишеться на КОЖЕН клік/скрол, а
// shaurma.json (акаунти, buildsRegistry тощо) не мусить перезаписуватись
// так часто заради цього.
//
// РЕГРЕСІЯ (виправлено): раніше весь цей стан жив ТІЛЬКИ в localStorage
// браузерного профілю WebView2 (lib/uiState.ts, ключ "shaurma:uiState") —
// усупереч задокументованій архітектурі («ніяких даних у localStorage»).
// Наслідок: стан губився при очищенні профілю WebView2, не переживав
// зміну теки даних (moveTreeWithProgress переносить лише
// installations/skins, localStorage лишається у старому профілі), і
// був єдиним місцем в усьому лаунчері, де дані НЕ доступні для бекапу/
// перенесення разом з рештою.
type UIState struct {
	Page        string                 `json:"page,omitempty"`
	InstanceTab string                 `json:"instanceTab,omitempty"`
	SettingsTab string                 `json:"settingsTab,omitempty"`
	OpenPackID  string                 `json:"openPackId,omitempty"`
	LastPacks   []string               `json:"lastPacks,omitempty"`
	Pages       map[string]float64     `json:"pages,omitempty"`
	Packs       map[string]PackUIState `json:"packs,omitempty"`
}

// PackUIState — стан UI сторінки ОДНІЄЇ збірки: активний розділ (вкладка)
// і позиція скролу в кожному розділі окремо (не одна спільна позиція —
// інакше повернення в інший розділ тієї ж збірки скидало б скрол на
// позицію останнього відвіданого розділу).
type PackUIState struct {
	Tab     string             `json:"tab,omitempty"`
	Scroll  float64            `json:"scroll,omitempty"`
	Scrolls map[string]float64 `json:"scrolls,omitempty"`
}

// ── Закладки браузера контенту ("зірочка") ──────────────────────────────
// Bookmark — один збережений проєкт браузера контенту (мод/модпак/
// ресурспак/шейдер/датапак з Modrinth чи CurseForge). Ідентифікується
// парою (Source, ProjectID) — той самий ключ, за яким браузер контенту
// (browser_api.go) звертається до API джерела. Дані картки (назва/іконка/
// тип) кешуються тут же — щоб розділ "Збережено" міг відрендерити список
// одразу, без повторного запиту деталей кожного проєкту при відкритті.
type Bookmark struct {
	Source      string `json:"source"` // modrinth | curseforge
	ProjectID   string `json:"projectId"`
	ProjectType string `json:"projectType"` // mod | modpack | resourcepack | shader | datapack
	Name        string `json:"name"`
	IconURL     string `json:"iconUrl,omitempty"`
	Author      string `json:"author,omitempty"`
	// CollectionID — порожньо для звичайної закладки-зірочки; непорожнє,
	// коли ця закладка є ЗАПИСОМ усередині колекції (групового набору
	// контенту для встановлення однією кнопкою, див. Collection нижче).
	// Той самий тип запису Bookmark використовується і для окремих
	// закладок, і для елементів колекції — не дублюємо структуру.
	CollectionID string `json:"collectionId,omitempty"`
	// VersionID — конкретна версія контенту, закріплена за цим записом
	// КОЛЕКЦІЇ (порожньо для звичайної закладки-зірочки, де версія завжди
	// "найновіша сумісна на момент встановлення"). Дозволяє колекції
	// встановлювати саме перевірену комбінацію версій, а не завжди
	// найсвіжішу — те, що просив ужиток: "аналіз чи є мод уже, чи треба
	// оновити на нову версію" звіряється саме з цим значенням.
	VersionID     string    `json:"versionId,omitempty"`
	VersionNumber string    `json:"versionNumber,omitempty"`
	AddedAt       time.Time `json:"addedAt"`
}

// Collection — груповий набір контенту ("тег-архів"): гравець складає
// пачку модів/ресурспаків/шейдерів/датапаків (напр. "Декорації") один
// раз і потім встановлює її ОДНІЄЮ кнопкою в будь-яку сумісну збірку.
// Записи колекції зберігаються як звичайні Bookmark з непорожнім
// CollectionID — так само, як звичайні закладки, лише прив'язані до
// групи (уникає дублювання структури/коду роботи із закладками).
type Collection struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Icon        string    `json:"icon,omitempty"`
	Color       string    `json:"color,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// BookmarksData — вміст .bookmarks.json: плоский список закладок
// (Bookmark.CollectionID == "") + список колекцій + записи всередині
// кожної колекції (Bookmark.CollectionID == Collection.ID).
type BookmarksData struct {
	Bookmarks   []Bookmark   `json:"bookmarks,omitempty"`
	Collections []Collection `json:"collections,omitempty"`
}

// ── Історія пошукових запитів браузера контенту ─────────────────────────
// SearchHistoryEntry — один пошуковий запит, який ДАВ РЕЗУЛЬТАТ (список
// не порожній на момент завершення пошуку). Запити без результату
// свідомо НЕ зберігаються — історія «останніх пошукових запитів, які
// дали результат» безпосередньо корисна для повторного пошуку («що я
// шукав минулого разу і воно спрацювало»), тоді як запит з друкарською
// помилкою чи занадто вузьким фільтром лише засмічував би список.
type SearchHistoryEntry struct {
	Query string `json:"query"`
	// ProjectType — вкладка типу контенту на момент пошуку (mod, modpack,
	// resourcepack, shader, datapack) — повторний клік на записі історії
	// відновлює і сам запит, і вкладку, в якій він був виконаний.
	ProjectType string `json:"projectType"`
	// ResultCount — скільки хітів було знайдено (сума Modrinth+CurseForge
	// на момент запису) — суто інформативне, для показу «меблі (24)» в
	// списку історії.
	ResultCount int       `json:"resultCount"`
	SearchedAt  time.Time `json:"searchedAt"`
}

// SearchHistoryData — вміст .search-history.json.
type SearchHistoryData struct {
	Entries []SearchHistoryEntry `json:"entries,omitempty"`
}

// Публічні типи (відповідають API, що використовується ззовні).

type PlaytimeSession struct {
	InstanceID  string    `json:"instanceId"`
	AccountID   string    `json:"accountId"`
	AccountName string    `json:"accountName"`
	StartedAt   time.Time `json:"startedAt"`
	Seconds     int64     `json:"seconds"`
	// Sessions — скільки реальних сесій стоїть за записом. 0 = звичайна
	// одна сесія. > 1 — це МІСЯЧНИЙ ПІДСУМОК (див. CompactPlaytimeSessions):
	// StartedAt тоді — середина 1-го числа місяця, Seconds — сума за місяць.
	Sessions int64 `json:"sessions,omitempty"`
}

type aiCacheEntry struct {
	CacheKey  string    `json:"cacheKey"`
	Kind      string    `json:"kind"`
	Data      string    `json:"data"`
	CreatedAt time.Time `json:"createdAt"`
}

type RegistryEntry struct {
	ID               string `json:"id"`
	Kind             string `json:"kind"`
	InstalledVersion string `json:"installedVersion"`
	InstalledAt      int64  `json:"installedAt"`
}

type AICacheEntry struct {
	CacheKey  string
	Kind      string
	Data      string
	CreatedAt time.Time
}

type GalleryMetaEntry struct {
	PackID   string
	FileName string
	Data     string
}

// ── Store ───────────────────────────────────────────────────────────

type Store struct {
	mu                  sync.Mutex
	globalPath          string
	instancesDir        string // кастомні збірки: <instancesDir>/<id>/instance.json
	shaurmaInstancesDir string // офіційні Shaurma-збірки: <shaurmaInstancesDir>/<id>/instance.json
	health              DBHealth
	data                globalData
	knownShaurmaIDs     map[string]bool // кеш Shaurma ID з registry
}

// SetShaurmaInstancesDir підключає другу теку збірок (офіційні Shaurma,
// config.Manager.ShaurmaPacksDir()) — окрему від instancesDir (кастомні).
// Без цього виклику Store поводиться як раніше (лише кастомні збірки),
// що зберігає зворотну сумісність для викликів Open() без цього кроку
// (напр. у тестах).
//
// РЕГРЕСІЯ (виправлено): loadInstanceConfigs/saveInstanceConfig/
// deleteInstanceConfig раніше ЗАВЖДИ читали й писали лише в instancesDir
// (кастомні збірки), навіть коли ID належав офіційній Shaurma-збірці, що
// фізично лежить у ShaurmaPacksDir(). RAM/Java/Fullscreen/AccountID
// override для таких збірок писались у instance.json ЗА НЕПРАВИЛЬНОЮ
// адресою — installations/<id>/instance.json (сирітський файл, там, де
// реальних даних збірки взагалі немає), а не у власну теку збірки. Читання
// й запис були симетрично неправильними, тож функціонально override
// застосовувались (без видимого бага для користувача), але дані для
// офіційних збірок опинялись НЕ там, де фізично лежить сама збірка —
// пряме порушення принципу «вся дані ОДНІЄЇ збірки — в ОДНОМУ instance.json
// у ЇЇ ВЛАСНІЙ папці».
func (s *Store) SetShaurmaInstancesDir(dir string) {
	s.mu.Lock()
	s.shaurmaInstancesDir = dir
	s.mu.Unlock()
}

// resolveInstanceDir повертає ТЕКУ (не файл), у якій ЦЯ конкретна збірка
// фізично зберігає instance.json. Kind визначається з knownShaurmaIDs
// (той самий кеш реєстру, що addOrphanedBuild/ScanAndRegisterInstances
// використовують для sidebar) — якщо ID невідомий, вважаємо custom
// (безпечний дефолт: саме там жила логіка ДО цього фіксу).
//
// ВАЖЛИВО: коли для ID є ОБИДВІ теки (перехідний стан — наприклад одразу
// після зміни Kind у store.go:ScanAndRegisterInstances, або стара
// installations/<id>/instance.json лишилась з часів цього самого багу) —
// перевага фізичній наявності теки в shaurmaInstancesDir, оскільки Kind
// із registry вже й так похідний ВІД фізичного розташування (після
// фіксу sidebar-класифікації) — розбіжність тут означає залишок старих
// (неправильно записаних) даних, які варто прочитати з правильного місця,
// а не продовжувати писати у застарілу адресу.
func (s *Store) resolveInstanceDir(id string) string {
	if s.shaurmaInstancesDir != "" {
		shaurmaPath := filepath.Join(s.shaurmaInstancesDir, id)
		if _, err := os.Stat(shaurmaPath); err == nil {
			return s.shaurmaInstancesDir
		}
		if s.knownShaurmaIDs[id] {
			return s.shaurmaInstancesDir
		}
	}
	return s.instancesDir
}

// Open відкриває JSON-сховище. dir — папка з DLL (де shaurma.json).
// instancesDir — папка збірок (де <id>/instance.json).
func Open(dir, instancesDir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	s := &Store{
		globalPath:   filepath.Join(dir, FileName),
		instancesDir: instancesDir,
		data:         globalData{},
	}
	s.loadFromDisk()
	s.knownShaurmaIDs = make(map[string]bool)
	for _, e := range s.data.BuildsRegistry {
		if e.Kind == "shaurma" {
			s.knownShaurmaIDs[e.ID] = true
		}
	}
	return s, nil
}

func (s *Store) Close() error                { return nil }
func (s *Store) Path() string                { return s.globalPath }
func (s *Store) Health() DBHealth            { return s.health }
func (s *Store) SchemaVersion() (int, error) { return 1, nil }
func (s *Store) SetInstancesDir(dir string)  { s.instancesDir = dir }

// ── Завантаження / збереження ─────────────────────────────────────

func (s *Store) loadFromDisk() {
	raw, err := os.ReadFile(s.globalPath)
	if err != nil {
		return
	}
	if err := json.Unmarshal(raw, &s.data); err != nil {
		// Файл пошкоджений — спроба відновити з бекапу
		bakPath := s.globalPath + BackupSuffix
		if raw2, rerr := os.ReadFile(bakPath); rerr == nil {
			var testData globalData
			if json.Unmarshal(raw2, &testData) == nil && !s.isEmptyData(testData) {
				s.data = testData
				_ = os.WriteFile(s.globalPath, raw2, 0644)
				s.health.RestoredFromBackup = true
				s.health.BackupAt = time.Now().UTC().Format(time.RFC3339)
				log.Printf("store: відновлено з резервної копії (%s)", bakPath)
			}
		}
	}
	// Прапорець «стерто навмисно»
	if v, ok := s.data.Meta["db_erased"]; ok && v != "" {
		s.health.ErasedIntentionally = true
		s.health.ErasedAt = v
	}
}

// isEmptyData перевіряє, чи дані порожні/дефолтні (не варто бекапити).
func (s *Store) isEmptyData(d globalData) bool {
	return d.Settings == "" &&
		d.MinecraftDefaults == "" &&
		len(d.Accounts) == 0 &&
		len(d.Playtime) == 0 &&
		len(d.PlaytimeSessions) == 0 &&
		len(d.BuildsRegistry) == 0 &&
		len(d.WardrobePresets) == 0
}

// saveGlobal атомарно записує shaurma.json.
func (s *Store) saveGlobal() error {
	// Не бекапимо пусті дані
	if !s.isEmptyData(s.data) {
		s.backupBeforeWrite()
	}

	data, err := json.Marshal(s.data) // компактно: без відступів файл і тимчасові буфери менші
	if err != nil {
		return fmt.Errorf("store: marshal: %w", err)
	}

	tmpPath := s.globalPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("store: write tmp: %w", err)
	}
	if err := os.Rename(tmpPath, s.globalPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("store: rename: %w", err)
	}

	// Перший запис з даними — створюємо .bak
	bakPath := s.globalPath + BackupSuffix
	if _, err := os.Stat(bakPath); err != nil {
		tmpBak := bakPath + ".tmp"
		if err := os.WriteFile(tmpBak, data, 0644); err == nil {
			os.Rename(tmpBak, bakPath)
		}
	}

	return nil
}

// clearableInstanceKeys — omitempty-ключі InstanceConfig, відсутність яких у
// збережуваному конфігу означає «значення скинуто», а не «поле не чіпали».
var clearableInstanceKeys = []string{"skinPresetId", "skinRestoreOnExit", "fullscreenOverride"}

// saveInstanceConfig атомарно записує instance.json у теці збірки —
// resolveInstanceDir вибирає ПРАВИЛЬНУ теку (shaurma vs custom) за
// фактичним розташуванням/Kind цього ID (див. коментар при
// SetShaurmaInstancesDir вище).
func (s *Store) saveInstanceConfig(cfg model.InstanceConfig) error {
	baseDir := s.resolveInstanceDir(cfg.ID)
	if baseDir == "" {
		return nil
	}
	dir := filepath.Join(baseDir, cfg.ID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("store: mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, InstanceFileName)

	// Читаємо існуючий файл щоб НЕ перезаписати унікальні поля Pack
	// (importState, blockedMods, sourceProjectId тощо).
	existing := make(map[string]json.RawMessage)
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &existing)
	}

	// Конвертуємо InstanceConfig → map і змішуємо з існуючим
	cfgBytes, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	cfgMap := make(map[string]json.RawMessage)
	if err := json.Unmarshal(cfgBytes, &cfgMap); err != nil {
		return err
	}
	// Журнал сесій і місячні підсумки пишуть ЛИШЕ appendPlaytimeSession…/
	// CompactPlaytimeSessions. Застарілий cfg (прочитаний до стиснення) не
	// має права їх перезаписати: інакше згорнуті сесії «воскресли» б поруч
	// зі своїм підсумком і порахувались двічі.
	for k, v := range cfgMap {
		if k == "playtimeSessions" || k == "playtimeRollups" {
			continue
		}
		existing[k] = v
	}
	// Поля з omitempty, які користувач може ЗНЯТИ (порожній рядок / nil),
	// у cfgMap просто відсутні — і без явного видалення старе значення
	// лишалось би у файлі та «воскресало» після перезавантаження (так не
	// вимикався «Скін збірки»: skinPresetId="" не записувався, а стара
	// назва пресета залишалась в instance.json).
	for _, k := range clearableInstanceKeys {
		if _, present := cfgMap[k]; !present {
			delete(existing, k)
		}
	}
	// Видаляємо nil-значення (щоб omitempty працював)
	for k, v := range existing {
		if string(v) == "null" {
			delete(existing, k)
		}
	}

	data, _ := json.MarshalIndent(existing, "", "  ")
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// deleteInstanceConfig видаляє override-поля з instance.json теки збірки
// (НЕ весь файл — instance.json несе й інші, не-override дані для
// shaurma-збірок; повне видалення файлу для custom-збірок відбувається
// деінде, при видаленні самої збірки).
func (s *Store) deleteInstanceConfig(id string) error {
	baseDir := s.resolveInstanceDir(id)
	if baseDir == "" {
		return nil
	}
	path := filepath.Join(baseDir, id, InstanceFileName)
	return os.Remove(path)
}

// loadInstanceConfigs сканує ОБИДВІ теки збірок (custom + shaurma, якщо
// SetShaurmaInstancesDir викликано) — instance.json кожної.
func (s *Store) loadInstanceConfigs() (map[string]model.InstanceConfig, error) {
	out := map[string]model.InstanceConfig{}
	scanDir := func(dir string) {
		if dir == "" {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return // папки немає — ОК
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			id := e.Name()
			path := filepath.Join(dir, id, InstanceFileName)
			raw, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var cfg model.InstanceConfig
			if err := json.Unmarshal(raw, &cfg); err != nil {
				continue
			}
			if cfg.ID == "" {
				cfg.ID = id
			}
			out[id] = cfg
		}
	}
	scanDir(s.instancesDir)
	scanDir(s.shaurmaInstancesDir)
	return out, nil
}

// ── Бекапи ─────────────────────────────────────────────────────────

func (s *Store) BackupNow() {
	if s.isEmptyData(s.data) {
		return
	}
	raw, err := os.ReadFile(s.globalPath)
	if err != nil {
		return
	}
	bakPath := s.globalPath + BackupSuffix
	tmpBak := bakPath + ".tmp"
	if err := os.WriteFile(tmpBak, raw, 0644); err == nil {
		os.Rename(tmpBak, bakPath)
	}
}

func (s *Store) backupBeforeWrite() {
	bakPath := s.globalPath + BackupSuffix
	if info, err := os.Stat(bakPath); err == nil && time.Since(info.ModTime()) <= BackupMaxAge {
		return
	}
	raw, err := os.ReadFile(s.globalPath)
	if err != nil {
		return
	}
	tmpBak := bakPath + ".tmp"
	if err := os.WriteFile(tmpBak, raw, 0644); err != nil {
		return
	}
	os.Rename(tmpBak, bakPath)
}

func (s *Store) MarkDBErased() error {
	return s.SetMeta("db_erased", time.Now().UTC().Format(time.RFC3339))
}

// ── Налаштування ───────────────────────────────────────────────────

func (s *Store) GetSettingsRaw() (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Settings == "" {
		return "", false, nil
	}
	return s.data.Settings, true, nil
}

func (s *Store) SaveSettingsRaw(value string) error {
	s.mu.Lock()
	s.data.Settings = value
	err := s.saveGlobal()
	s.mu.Unlock()
	return err
}

func (s *Store) GetMinecraftDefaultsRaw() (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.MinecraftDefaults == "" {
		return "", false, nil
	}
	return s.data.MinecraftDefaults, true, nil
}

func (s *Store) SaveMinecraftDefaultsRaw(value string) error {
	s.mu.Lock()
	s.data.MinecraftDefaults = value
	err := s.saveGlobal()
	s.mu.Unlock()
	return err
}

// ── Акаунти ────────────────────────────────────────────────────────

func (s *Store) LoadAccounts() ([]model.Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.Account, len(s.data.Accounts))
	copy(out, s.data.Accounts)
	return out, nil
}

func (s *Store) SaveAccounts(accounts []model.Account) error {
	s.mu.Lock()
	s.data.Accounts = accounts
	err := s.saveGlobal()
	s.mu.Unlock()
	return err
}

// ── Ігровий час ────────────────────────────────────────────────────

func (s *Store) LoadPlaytime() (map[string]int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]int64)
	// Читаємо playtime з per-instance JSON файлів
	if s.instancesDir != "" {
		entries, err := os.ReadDir(s.instancesDir)
		if err == nil {
			for _, e := range entries {
				if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
					continue
				}
				path := filepath.Join(s.instancesDir, e.Name(), InstanceFileName)
				raw, err := os.ReadFile(path)
				if err != nil {
					continue
				}
				var cfg model.InstanceConfig
				if json.Unmarshal(raw, &cfg) == nil && cfg.PlaytimeSeconds > 0 {
					out[cfg.ID] = cfg.PlaytimeSeconds
				}
			}
		}
	}
	// Фолбек: якщо є глобальний playtime (старий формат після міграції)
	for k, v := range s.data.Playtime {
		if _, exists := out[k]; !exists && v > 0 {
			out[k] = v
		}
	}
	return out, nil
}

func (s *Store) AddPlaytime(instanceID string, seconds int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if instanceID == "" {
		return nil
	}
	// Фолбек: якщо немає instancesDir — пишемо в глобальний
	if s.instancesDir == "" {
		if s.data.Playtime == nil {
			s.data.Playtime = map[string]int64{}
		}
		s.data.Playtime[instanceID] += seconds
		return s.saveGlobal()
	}
	// Оновлюємо per-instance JSON
	dir := filepath.Join(s.instancesDir, instanceID)
	path := filepath.Join(dir, InstanceFileName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("store: mkdir %s: %w", dir, err)
	}
	var cfg model.InstanceConfig
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &cfg)
	}
	if cfg.ID == "" {
		cfg.ID = instanceID
	}
	cfg.PlaytimeSeconds += seconds
	data, _ := json.MarshalIndent(cfg, "", "  ")
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func (s *Store) RenamePlaytime(oldID, newID string) error {
	if oldID == newID || oldID == "" || newID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Переносимо playtime зі старого instance.json в новий
	if s.instancesDir != "" {
		oldPath := filepath.Join(s.instancesDir, oldID, InstanceFileName)
		var oldCfg model.InstanceConfig
		if raw, err := os.ReadFile(oldPath); err == nil {
			_ = json.Unmarshal(raw, &oldCfg)
		}
		if oldCfg.PlaytimeSeconds > 0 {
			newPath := filepath.Join(s.instancesDir, newID, InstanceFileName)
			var newCfg model.InstanceConfig
			if raw, err := os.ReadFile(newPath); err == nil {
				_ = json.Unmarshal(raw, &newCfg)
			}
			if newCfg.ID == "" {
				newCfg.ID = newID
			}
			newCfg.PlaytimeSeconds += oldCfg.PlaytimeSeconds
			data, _ := json.MarshalIndent(newCfg, "", "  ")
			_ = os.MkdirAll(filepath.Dir(newPath), 0755)
			tmpPath := newPath + ".tmp"
			if err := os.WriteFile(tmpPath, data, 0644); err == nil {
				_ = os.Rename(tmpPath, newPath)
			}
		}
	}
	return nil
}

// ── Ігрові сесії ──────────────────────────────────────────────────
// Кожна сесія пишеться в instance.json ВЛАСНЕ ЗБІРКИ (поле
// playtimeSessions) — не в спільний масив shaurma.json. instanceID
// визначає, яку теку (custom/shaurma) шукати через resolveInstanceDir,
// той самий метод, що GetInstanceConfig/SaveInstanceConfig.

// AddPlaytimeSession дописує ОДНУ сесію в playtimeSessions відповідного
// instance.json (append, не read-modify-write усього файлу глобально —
// читає/пише лише JSON цієї конкретної збірки).
func (s *Store) AddPlaytimeSession(instanceID, accountID, accountName string, seconds int64, startedAt time.Time) error {
	s.mu.Lock()
	err := s.appendPlaytimeSessionToInstance(instanceID, PlaytimeSessionEntry{
		AccountID:   accountID,
		AccountName: accountName,
		StartedAt:   startedAt,
		Seconds:     seconds,
	})
	s.mu.Unlock()
	return err
}

// appendPlaytimeSessionToInstance читає instance.json збірки, дописує
// сесію в playtimeSessions і пише назад — той самий merge-through-raw-map
// підхід, що saveInstanceConfig (не зачіпає жодне інше поле файлу,
// зокрема унікальні поля custompack.Pack, про які store/model нічого не
// знають).
func (s *Store) appendPlaytimeSessionToInstance(instanceID string, entry PlaytimeSessionEntry) error {
	baseDir := s.resolveInstanceDir(instanceID)
	if baseDir == "" {
		return nil
	}
	dir := filepath.Join(baseDir, instanceID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("store: mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, InstanceFileName)

	existing := make(map[string]json.RawMessage)
	if raw, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(raw, &existing)
	}

	var sessions []PlaytimeSessionEntry
	if raw, ok := existing["playtimeSessions"]; ok {
		_ = json.Unmarshal(raw, &sessions)
	}
	sessions = append(sessions, entry)
	sessBytes, err := json.Marshal(sessions)
	if err != nil {
		return err
	}
	existing["playtimeSessions"] = sessBytes
	if _, ok := existing["id"]; !ok {
		idBytes, _ := json.Marshal(instanceID)
		existing["id"] = idBytes
	}

	data, _ := json.MarshalIndent(existing, "", "  ")
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// LoadPlaytimeSessions сканує playtimeSessions з КОЖНОГО instance.json
// (обидві теки — custom і shaurma) плюс, для зворотної сумісності,
// СТАРИЙ спільний масив у shaurma.json (s.data.PlaytimeSessions) —
// записи, зроблені до переносу на per-instance зберігання. Новий код
// більше НЕ пише в s.data.PlaytimeSessions (лишається порожнім для всіх
// сесій після оновлення), тож ця гілка з часом природно спорожніє.
func (s *Store) LoadPlaytimeSessions(accountName string) ([]PlaytimeSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []PlaytimeSession

	scanDir := func(dir string) {
		if dir == "" {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			instanceID := e.Name()
			path := filepath.Join(dir, instanceID, InstanceFileName)
			raw, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var wrapper struct {
				PlaytimeSessions []PlaytimeSessionEntry `json:"playtimeSessions"`
				PlaytimeRollups  []PlaytimeRollupEntry  `json:"playtimeRollups"`
			}
			if err := json.Unmarshal(raw, &wrapper); err != nil {
				continue
			}
			// Місячні підсумки старих сесій (див. CompactPlaytimeSessions)
			// віддаємо як «синтетичні» записи: аналітика (дні/тижні/місяці/
			// роки, топ збірок) рахує їх так само, як сирі сесії.
			for _, ru := range wrapper.PlaytimeRollups {
				if accountName != "" && ru.AccountName != accountName {
					continue
				}
				month, err := time.ParseInLocation("2006-01", ru.Month, time.Local)
				if err != nil || ru.Seconds <= 0 {
					continue
				}
				out = append(out, PlaytimeSession{
					InstanceID:  instanceID,
					AccountID:   ru.AccountID,
					AccountName: ru.AccountName,
					StartedAt:   month.Add(12 * time.Hour),
					Seconds:     ru.Seconds,
					Sessions:    ru.Sessions,
				})
			}
			for _, rec := range wrapper.PlaytimeSessions {
				if accountName != "" && rec.AccountName != accountName {
					continue
				}
				out = append(out, PlaytimeSession{
					InstanceID:  instanceID,
					AccountID:   rec.AccountID,
					AccountName: rec.AccountName,
					StartedAt:   rec.StartedAt,
					Seconds:     rec.Seconds,
				})
			}
		}
	}
	scanDir(s.instancesDir)
	scanDir(s.shaurmaInstancesDir)

	// Легасі: сесії, записані ДО переносу на per-instance зберігання.
	for _, rec := range s.data.PlaytimeSessions {
		if accountName != "" && rec.AccountName != accountName {
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}

// PlaytimeSessionEntry — форма ОДНОГО запису playtimeSessions усередині
// instance.json (без InstanceID — контекст файлу вже його визначає).
// Дзеркалить model.PlaytimeSessionRecord (store не імпортує model для
// цього дрібного типу, щоб уникнути циклічної залежності — model уже й
// так не залежить від store, тому дублювання тут навмисне, не
// недогляд).
type PlaytimeSessionEntry struct {
	AccountID   string    `json:"accountId,omitempty"`
	AccountName string    `json:"accountName,omitempty"`
	StartedAt   time.Time `json:"startedAt"`
	Seconds     int64     `json:"seconds"`
}

// PlaytimeRollupEntry — МІСЯЧНИЙ підсумок старих сесій однієї збірки для
// одного гравця. Лежить у instance.json (поле playtimeRollups) і замінює
// сирі сесії, старші за поріг стиснення: файл не росте нескінченно, а
// загальні цифри (сума, кількість сесій, топ збірок, графіки місяців/років)
// лишаються точними — губиться тільки розбивка по днях/годинах.
type PlaytimeRollupEntry struct {
	Month       string `json:"month"` // "2006-01", локальний час гравця
	AccountID   string `json:"accountId,omitempty"`
	AccountName string `json:"accountName,omitempty"`
	Seconds     int64  `json:"seconds"`
	Sessions    int64  `json:"sessions"`
}

// CompactPlaytimeSessions згортає сирі сесії, що почались РАНІШЕ olderThan,
// у місячні підсумки (playtimeRollups) в instance.json кожної збірки.
// Повертає, скільки сесій згорнуто. Ідемпотентно: повторний виклик нічого не
// робить, поки не з'являться нові «застарілі» сесії; нові підсумки
// додаються до наявних того ж (місяць, гравець).
//
// Порог мусить бути більшим за найдовше вікно, якому потрібна денна
// точність (аналітика показує 7 днів і 12 тижнів = 84 дні) — див.
// PlaytimeCompactAfter.
func (s *Store) CompactPlaytimeSessions(olderThan time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := 0
	var firstErr error
	for _, dir := range []string{s.instancesDir, s.shaurmaInstancesDir} {
		if dir == "" {
			continue
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			n, err := compactInstancePlaytime(filepath.Join(dir, e.Name(), InstanceFileName), olderThan)
			total += n
			if err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return total, firstErr
}

// PlaytimeCompactAfter — вік сесії, з якого її згортають у місяць. 120 днів
// > 84 днів (12 тижневих стовпчиків аналітики), тож жоден стовпчик, який
// користувач бачить по днях/тижнях, не втрачає точності.
const PlaytimeCompactAfter = 120 * 24 * time.Hour

func compactInstancePlaytime(path string, olderThan time.Time) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, nil // збірка без instance.json — нічого стискати
	}
	existing := make(map[string]json.RawMessage)
	if json.Unmarshal(raw, &existing) != nil {
		return 0, nil // битий файл не чіпаємо
	}
	rawSess, ok := existing["playtimeSessions"]
	if !ok {
		return 0, nil
	}
	var sessions []PlaytimeSessionEntry
	if json.Unmarshal(rawSess, &sessions) != nil {
		return 0, nil
	}
	var rollups []PlaytimeRollupEntry
	if r, ok := existing["playtimeRollups"]; ok {
		_ = json.Unmarshal(r, &rollups)
	}

	type key struct{ month, id, name string }
	idx := make(map[key]int, len(rollups))
	for i, r := range rollups {
		idx[key{r.Month, r.AccountID, r.AccountName}] = i
	}

	keep := sessions[:0:0]
	folded := 0
	for _, se := range sessions {
		if se.StartedAt.IsZero() || !se.StartedAt.Before(olderThan) {
			keep = append(keep, se)
			continue
		}
		k := key{se.StartedAt.In(time.Local).Format("2006-01"), se.AccountID, se.AccountName}
		if i, ok := idx[k]; ok {
			rollups[i].Seconds += se.Seconds
			rollups[i].Sessions++
		} else {
			idx[k] = len(rollups)
			rollups = append(rollups, PlaytimeRollupEntry{
				Month: k.month, AccountID: k.id, AccountName: k.name,
				Seconds: se.Seconds, Sessions: 1,
			})
		}
		folded++
	}
	if folded == 0 {
		return 0, nil
	}

	sb, err := json.Marshal(keep)
	if err != nil {
		return 0, err
	}
	rb, err := json.Marshal(rollups)
	if err != nil {
		return 0, err
	}
	existing["playtimeSessions"] = sb
	existing["playtimeRollups"] = rb
	data, _ := json.MarshalIndent(existing, "", "  ")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return 0, err
	}
	if err := os.Rename(tmp, path); err != nil {
		return 0, err
	}
	return folded, nil
}

// ── Instance configs (per-instance JSON) ───────────────────────────

func (s *Store) GetInstanceConfig(id string) (model.InstanceConfig, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Спочатку скануємо instance.json
	cfgs, _ := s.loadInstanceConfigs()
	if cfg, ok := cfgs[id]; ok {
		return cfg, true, nil
	}
	return model.InstanceConfig{}, false, nil
}

func (s *Store) LoadInstanceConfigs() (map[string]model.InstanceConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadInstanceConfigs()
}

func (s *Store) SaveInstanceConfig(cfg model.InstanceConfig) error {
	s.mu.Lock()
	err := s.saveInstanceConfig(cfg)
	s.mu.Unlock()
	return err
}

func (s *Store) DeleteInstanceConfig(id string) error {
	s.mu.Lock()
	err := s.deleteInstanceConfig(id)
	s.mu.Unlock()
	return err
}

// ── Setup progress ─────────────────────────────────────────────────

func (s *Store) LoadSetupProgress() (model.SetupProgress, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.SetupProgress == nil {
		return model.SetupProgress{}, false, nil
	}
	return *s.data.SetupProgress, true, nil
}

func (s *Store) SaveSetupProgress(p model.SetupProgress) error {
	s.mu.Lock()
	s.data.SetupProgress = &p
	err := s.saveGlobal()
	s.mu.Unlock()
	return err
}

func (s *Store) DeleteSetupProgress() error {
	s.mu.Lock()
	s.data.SetupProgress = nil
	err := s.saveGlobal()
	s.mu.Unlock()
	return err
}

// ── Meta ───────────────────────────────────────────────────────────

func (s *Store) GetMeta(key string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.Meta == nil {
		return "", false, nil
	}
	v, ok := s.data.Meta[key]
	if !ok {
		return "", false, nil
	}
	return v, true, nil
}

func (s *Store) SetMeta(key, value string) error {
	s.mu.Lock()
	if s.data.Meta == nil {
		s.data.Meta = map[string]string{}
	}
	s.data.Meta[key] = value
	err := s.saveGlobal()
	s.mu.Unlock()
	return err
}

// CleanupStaleFields видаляє застарілі поля з shaurma.json,
// які більше не використовуються (customPacks, installState).
func (s *Store) CleanupStaleFields() {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.globalPath)
	if err != nil {
		return
	}
	// Швидкий вихід: на кожному старті файл уже чистий — не парсимо весь
	// shaurma.json ще раз (його вже розібрав Open) заради порожнього результату.
	if !strings.Contains(string(data), `"customPacks"`) && !strings.Contains(string(data), `"installState"`) {
		return
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil {
		return
	}

	changed := false
	for _, key := range []string{"customPacks", "installState"} {
		if _, exists := raw[key]; exists {
			delete(raw, key)
			changed = true
		}
	}
	if !changed {
		return
	}
	cleaned, _ := json.MarshalIndent(raw, "", "  ")
	tmpPath := s.globalPath + ".tmp"
	if err := os.WriteFile(tmpPath, cleaned, 0644); err == nil {
		_ = os.Rename(tmpPath, s.globalPath)
		log.Printf("store: очищено застарілі поля з %s", s.globalPath)
	}
	// Видаляємо застарілий custom-packs.json ( дані тепер в instance.json)
	cfgDir := filepath.Dir(s.globalPath)
	cpPath := filepath.Join(cfgDir, "custom-packs.json")
	if err := os.Remove(cpPath); err == nil {
		log.Printf("store: видалено застарілий %s", cpPath)
	}
}

// ── МІГРАЦІЯ З SQLite ──────────────────────────────────────────────

// MigrateFromSQLiteIfAvailable конвертує shaurma.db → shaurma.json + instance.json.
// Після успішної конвертації АБО якщо JSON вже є з даними — .db ВИДАЛЯЄТЬСЯ.
func (s *Store) MigrateFromSQLiteIfAvailable(configDir string) {
	// Шукаємо shaurma.db в configDir
	dbPath := filepath.Join(configDir, "shaurma.db")
	if _, err := os.Stat(dbPath); err != nil {
		return // БД немає — нічого робити
	}

	// Перевіряємо: якщо shaurma.json вже є з даними — конвертація не потрібна,
	// але стару БД все одно видаляємо.
	if _, err := os.Stat(s.globalPath); err == nil {
		if raw, rerr := os.ReadFile(s.globalPath); rerr == nil && len(raw) > 10 {
			log.Printf("store: shaurma.json вже існує — видаляємо застару БД")
			s.removeDBFiles(dbPath)
			return
		}
	}

	log.Printf("store: знайдено shaurma.db — конвертація в JSON...")
	if err := s.migrateFromDB(dbPath); err != nil {
		log.Printf("store: помилка конвертації: %v — видаляємо БД все одно", err)
		s.removeDBFiles(dbPath)
		return
	}

	log.Printf("store: конвертація завершена, видаляємо shaurma.db")
	s.removeDBFiles(dbPath)
	s.health.MigratedFromDB = true
}

// removeDBFiles видаляє shaurma.db та WAL/SHM файли.
func (s *Store) removeDBFiles(dbPath string) {
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(dbPath + suffix); err == nil {
			log.Printf("store: видалено %s", dbPath+suffix)
		}
	}
}

// migrateFromDB читає ВСІ дані з SQLite і пише в JSON.
func (s *Store) migrateFromDB(dbPath string) error {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("відкриття БД: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	// Settings
	if raw, ok, _ := sqliteStr(db, `SELECT value FROM settings WHERE id = 1`); ok {
		s.data.Settings = raw
	}

	// Minecraft defaults
	if raw, ok, _ := sqliteStr(db, `SELECT value FROM minecraft_defaults WHERE id = 1`); ok {
		s.data.MinecraftDefaults = raw
	}

	// Accounts
	if acc, err := sqliteAccounts(db); err == nil && len(acc) > 0 {
		s.data.Accounts = acc
	}

	// Playtime → переносимо в per-instance JSON
	if pt, err := sqlitePlaytime(db); err == nil && len(pt) > 0 {
		for id, seconds := range pt {
			s.migratePlaytimeToInstance(id, seconds)
		}
	}

	// Playtime sessions
	if sessions, err := sqliteSessions(db); err == nil && len(sessions) > 0 {
		s.data.PlaytimeSessions = sessions
	}

	// Instance configs → per-instance JSON файли
	if cfgs, err := sqliteInstanceConfigs(db); err == nil {
		for id, cfg := range cfgs {
			if cfg.ID == "" {
				cfg.ID = id
			}
			// Пишемо в per-instance JSON
			if err := s.saveInstanceConfig(cfg); err != nil {
				log.Printf("store: конвертація instance %s: %v", id, err)
			}
		}
	}

	// Setup progress
	if raw, ok, _ := sqliteStr(db, `SELECT value FROM setup_progress WHERE id = 1`); ok {
		var p model.SetupProgress
		if json.Unmarshal([]byte(raw), &p) == nil && p.Step > 0 {
			s.data.SetupProgress = &p
		}
	}

	// Meta
	if meta, err := sqliteStrMap(db, `SELECT key, value FROM app_meta`); err == nil {
		s.data.Meta = meta
	}

	// Builds registry
	if builds, err := sqliteBuildsRegistry(db); err == nil {
		s.data.BuildsRegistry = builds
	}

	// Custom packs → custom-packs.json (file-based)
	if packs, err := sqliteStrMap(db, `SELECT id, data FROM custom_packs`); err == nil && len(packs) > 0 {
		for id, rawJSON := range packs {
			// Копіюємо metadata в instance.json (виконується далі в MigrateCustomPacksToInstance)
			_ = id
			_ = rawJSON
		}
		// Записуємо в custom-packs.json через atomicfile
		if err := s.writeCustomPacksJSON(packs); err != nil {
			log.Printf("store: конвертація custom_packs: %v", err)
		}
	}

	// Gallery meta
	if gallery, err := sqliteGalleryMeta(db); err == nil {
		s.data.GalleryMeta = gallery
	}

	// Shortcuts
	if shortcuts, err := sqliteStrMap(db, `SELECT pack_id, lnk_name FROM shortcuts`); err == nil {
		s.data.Shortcuts = shortcuts
	}

	// AI cache
	if ai, err := sqliteAICache(db); err == nil {
		s.data.AICache = ai
	}

	// Install state → install-state/<packID>.json файли
	if state, err := sqliteStrMap(db, `SELECT pack_id, data FROM install_state`); err == nil && len(state) > 0 {
		_ = s.writeInstallStateJSON(state)
	}

	// Wardrobe presets
	if wp, err := sqliteStrMap(db, `SELECT account_id, data FROM wardrobe_presets`); err == nil {
		s.data.WardrobePresets = wp
	}

	// Export settings
	if es, err := sqliteStrMap(db, `SELECT pack_id, data FROM export_settings`); err == nil {
		s.data.ExportSettings = es
	}

	// Duplicate settings
	if ds, err := sqliteStrMap(db, `SELECT pack_id, data FROM duplicate_settings`); err == nil {
		s.data.DuplicateSettings = ds
	}

	// Записуємо все в shaurma.json
	if err := s.saveGlobal(); err != nil {
		return err
	}

	// Витягуємо PackOrder/PackPinned/PackFolders з settings → .build-layout.json
	s.migrateLayoutFromSettings()

	// Копіюємо іконки/фони та метадані з custom_packs в instance.json
	s.migrateAssetPaths(db)

	return nil
}

// writeCustomPacksJSON записує custom packs з map[id]json в custom-packs.json.
// Використовується при міграції з SQLite: дані одразу пишуться в файл,
// а НЕ в globalData (bo CustomPacks прибрано з globalData).
func (s *Store) writeCustomPacksJSON(packs map[string]string) error {
	if len(packs) == 0 {
		return nil
	}
	// Формат custom-packs.json — масив об'єктів.
	type packEntry struct {
		ID   string          `json:"id"`
		Rest json.RawMessage `json:"-"`
	}
	var list []json.RawMessage
	for _, raw := range packs {
		var obj map[string]json.RawMessage
		if json.Unmarshal([]byte(raw), &obj) == nil {
			list = append(list, json.RawMessage(raw))
		}
	}
	if len(list) == 0 {
		return nil
	}
	cfgDir := filepath.Dir(s.globalPath)
	cpPath := filepath.Join(cfgDir, "custom-packs.json")
	return atomicfile.WriteJSONAtomic(cpPath, list)
}

// writeInstallStateJSON записує install state з map[packID]json в
// install-state/<packID>.json файли.
func (s *Store) writeInstallStateJSON(states map[string]string) error {
	if len(states) == 0 {
		return nil
	}
	cfgDir := filepath.Dir(s.globalPath)
	dir := filepath.Join(cfgDir, "install-state")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	for packID, raw := range states {
		tmp := filepath.Join(dir, packID+".json.tmp")
		final := filepath.Join(dir, packID+".json")
		if err := os.WriteFile(tmp, []byte(raw), 0644); err != nil {
			continue
		}
		_ = os.Rename(tmp, final)
	}
	return nil
}

// migrateLayoutFromSettings витягує PackOrder/PackPinned/PackFolders з settings
// JSON і пише в .build-layout.json (поруч із installations).
func (s *Store) migrateLayoutFromSettings() {
	if s.data.Settings == "" || s.instancesDir == "" {
		return
	}
	var settingsMap map[string]json.RawMessage
	if err := json.Unmarshal([]byte(s.data.Settings), &settingsMap); err != nil {
		return
	}

	var layout BuildLayout

	if raw, ok := settingsMap["packOrder"]; ok {
		_ = json.Unmarshal(raw, &layout.PackOrder)
	}
	if raw, ok := settingsMap["packPinned"]; ok {
		_ = json.Unmarshal(raw, &layout.PackPinned)
	}
	if raw, ok := settingsMap["packFolders"]; ok {
		_ = json.Unmarshal(raw, &layout.PackFolders)
	}

	if len(layout.PackOrder) > 0 || len(layout.PackPinned) > 0 || len(layout.PackFolders) > 0 {
		_ = s.saveBuildLayout(layout)
		log.Printf("store: layout (order/pinned/folders) перенесено в %s", LayoutFileName)
	}
}

// migrateAssetPaths копіює іконки/фони зі старих шляхів custom_packs
// поруч із instance.json (якщо файли ще є на диску) та записує
// метадані збірки (MCVersion, Loader, Color...) в instance.json.
func (s *Store) migrateAssetPaths(db *sql.DB) {
	if s.instancesDir == "" {
		return
	}
	rows, err := db.Query(`SELECT id, data FROM custom_packs`)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			continue
		}
		// Повний запис з custom_packs (всі метадані)
		var pack struct {
			Name           string `json:"name"`
			Description    string `json:"description"`
			IconPath       string `json:"iconPath"`
			BackgroundPath string `json:"backgroundPath"`
			MCVersion      string `json:"mcVersion"`
			Loader         string `json:"loader"`
			LoaderVersion  string `json:"loaderVersion"`
			Color          string `json:"color"`
			Source         string `json:"source"`
		}
		if err := json.Unmarshal([]byte(raw), &pack); err != nil {
			continue
		}

		instDir := filepath.Join(s.instancesDir, id)
		if err := os.MkdirAll(instDir, 0755); err != nil {
			continue
		}

		instPath := filepath.Join(instDir, InstanceFileName)
		var icfg model.InstanceConfig
		if data, rerr := os.ReadFile(instPath); rerr == nil {
			_ = json.Unmarshal(data, &icfg)
		}
		if icfg.ID == "" {
			icfg.ID = id
		}
		// Заповнюємо метадані збірки (тільки якщо порожні — не перезаписуємо
		// зміни користувача)
		if icfg.Name == "" {
			icfg.Name = pack.Name
		}
		if icfg.Description == "" {
			icfg.Description = pack.Description
		}
		if icfg.MCVersion == "" {
			icfg.MCVersion = pack.MCVersion
		}
		if icfg.Loader == "" {
			icfg.Loader = pack.Loader
		}
		if icfg.LoaderVersion == "" {
			icfg.LoaderVersion = pack.LoaderVersion
		}
		if icfg.Color == "" {
			icfg.Color = pack.Color
		}
		if icfg.Source == "" {
			icfg.Source = pack.Source
		}

		// Копіюємо іконку та записуємо відносний шлях
		if pack.IconPath != "" {
			if copyFileToDir(pack.IconPath, filepath.Join(instDir, "icon.png")) {
				icfg.IconPath = "icon.png"
				log.Printf("store: іконка %s → %s", pack.IconPath, filepath.Join(instDir, "icon.png"))
			}
		}
		// Копіюємо фон та записуємо відносний шлях
		if pack.BackgroundPath != "" {
			if copyFileToDir(pack.BackgroundPath, filepath.Join(instDir, "background.png")) {
				icfg.BackgroundPath = "background.png"
				log.Printf("store: фон %s → %s", pack.BackgroundPath, filepath.Join(instDir, "background.png"))
			}
		}

		// Записуємо instance.json
		data, _ := json.MarshalIndent(icfg, "", "  ")
		tmpPath := instPath + ".tmp"
		if err := os.WriteFile(tmpPath, data, 0644); err == nil {
			_ = os.Rename(tmpPath, instPath)
		}
	}
}

// MigrateCustomPacksToInstance переносить метадані збірок з custom-packs.json
// в окремі instance.json файли. Викликається при старті після openStore.
func (s *Store) MigrateCustomPacksToInstance() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.instancesDir == "" {
		return
	}

	// Читаємо custom packs з custom-packs.json (файл).
	cfgDir := filepath.Dir(s.globalPath)
	cpPath := filepath.Join(cfgDir, "custom-packs.json")
	cpData, err := os.ReadFile(cpPath)
	if err != nil {
		return // Файл не існує — нічого мігрувати
	}
	var packsList []json.RawMessage
	if json.Unmarshal(cpData, &packsList) != nil || len(packsList) == 0 {
		return
	}

	migrated := 0
	for _, rawJSON := range packsList {
		var idHolder struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(rawJSON, &idHolder) != nil || idHolder.ID == "" {
			continue
		}
		id := idHolder.ID
		var pack struct {
			Name           string `json:"name"`
			Description    string `json:"description"`
			IconPath       string `json:"iconPath"`
			BackgroundPath string `json:"backgroundPath"`
			MCVersion      string `json:"mcVersion"`
			Loader         string `json:"loader"`
			LoaderVersion  string `json:"loaderVersion"`
			Color          string `json:"color"`
			Source         string `json:"source"`
		}
		if json.Unmarshal([]byte(rawJSON), &pack) != nil {
			continue
		}

		instDir := filepath.Join(s.instancesDir, id)
		instPath := filepath.Join(instDir, InstanceFileName)

		var icfg model.InstanceConfig
		if data, err := os.ReadFile(instPath); err == nil {
			_ = json.Unmarshal(data, &icfg)
		}
		if icfg.ID == "" {
			icfg.ID = id
		}

		// Заповнюємо тільки порожні поля (не перезаписуємо зміни користувача)
		if icfg.Name == "" {
			icfg.Name = pack.Name
		}
		if icfg.Description == "" {
			icfg.Description = pack.Description
		}
		if icfg.MCVersion == "" {
			icfg.MCVersion = pack.MCVersion
		}
		if icfg.Loader == "" {
			icfg.Loader = pack.Loader
		}
		if icfg.LoaderVersion == "" {
			icfg.LoaderVersion = pack.LoaderVersion
		}
		if icfg.Color == "" {
			icfg.Color = pack.Color
		}
		if icfg.Source == "" {
			icfg.Source = pack.Source
		}

		// Копіюємо іконку
		if pack.IconPath != "" && icfg.IconPath == "" {
			if copyFileToDir(pack.IconPath, filepath.Join(instDir, "icon.png")) {
				icfg.IconPath = "icon.png"
			}
		}
		if pack.BackgroundPath != "" && icfg.BackgroundPath == "" {
			if copyFileToDir(pack.BackgroundPath, filepath.Join(instDir, "background.png")) {
				icfg.BackgroundPath = "background.png"
			}
		}

		// Записуємо instance.json
		_ = os.MkdirAll(instDir, 0755)
		data, _ := json.MarshalIndent(icfg, "", "  ")
		tmpPath := instPath + ".tmp"
		if err := os.WriteFile(tmpPath, data, 0644); err == nil {
			_ = os.Rename(tmpPath, instPath)
			migrated++
		}
	}

	if migrated > 0 {
		log.Printf("store: перенесено %d збірок з custom-packs.json → instance.json", migrated)
	}
}

// migratePlaytimeToInstance переносить playtime секунди в per-instance JSON.
func (s *Store) migratePlaytimeToInstance(id string, seconds int64) {
	baseDir := s.resolveInstanceDir(id)
	if baseDir == "" || id == "" {
		return
	}
	instPath := filepath.Join(baseDir, id, InstanceFileName)
	// Читаємо існуючий instance.json або створюємо новий
	var cfg model.InstanceConfig
	if raw, err := os.ReadFile(instPath); err == nil {
		_ = json.Unmarshal(raw, &cfg)
	}
	if cfg.ID == "" {
		cfg.ID = id
	}
	cfg.PlaytimeSeconds = seconds
	data, _ := json.MarshalIndent(cfg, "", "  ")
	_ = os.MkdirAll(filepath.Dir(instPath), 0755)
	tmpPath := instPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err == nil {
		_ = os.Rename(tmpPath, instPath)
	}
}

// MigratePlaytimeSessionsToInstances переносить старі сесії зі спільного
// shaurma.json.playtimeSessions у playtimeSessions КОЖНОГО instance.json
// (групування за InstanceID) — одноразово, при відкритті Store. Записи
// без InstanceID (биті/дуже старі дані) пропускаються — переносити
// нікуди. Після успішного перенесення старий масив очищається, щоб дані
// не жили одночасно у двох місцях.
func (s *Store) MigratePlaytimeSessionsToInstances() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.data.PlaytimeSessions) == 0 {
		return
	}

	byInstance := map[string][]PlaytimeSession{}
	var skipped int
	for _, rec := range s.data.PlaytimeSessions {
		if rec.InstanceID == "" {
			skipped++
			continue
		}
		byInstance[rec.InstanceID] = append(byInstance[rec.InstanceID], rec)
	}

	migrated := 0
	for instanceID, recs := range byInstance {
		baseDir := s.resolveInstanceDir(instanceID)
		if baseDir == "" {
			continue
		}
		dir := filepath.Join(baseDir, instanceID)
		path := filepath.Join(dir, InstanceFileName)

		existing := make(map[string]json.RawMessage)
		if raw, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(raw, &existing)
		}
		var sessions []PlaytimeSessionEntry
		if raw, ok := existing["playtimeSessions"]; ok {
			_ = json.Unmarshal(raw, &sessions)
		}
		for _, rec := range recs {
			sessions = append(sessions, PlaytimeSessionEntry{
				AccountID:   rec.AccountID,
				AccountName: rec.AccountName,
				StartedAt:   rec.StartedAt,
				Seconds:     rec.Seconds,
			})
		}
		sessBytes, err := json.Marshal(sessions)
		if err != nil {
			continue
		}
		existing["playtimeSessions"] = sessBytes
		if _, ok := existing["id"]; !ok {
			idBytes, _ := json.Marshal(instanceID)
			existing["id"] = idBytes
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			continue
		}
		data, _ := json.MarshalIndent(existing, "", "  ")
		tmpPath := path + ".tmp"
		if err := os.WriteFile(tmpPath, data, 0644); err != nil {
			continue
		}
		if err := os.Rename(tmpPath, path); err != nil {
			continue
		}
		migrated += len(recs)
	}

	if migrated > 0 {
		s.data.PlaytimeSessions = nil
		_ = s.saveGlobalLocked()
		log.Printf("store: перенесено %d ігрових сесій зі shaurma.json → instance.json (%d записів без instanceId пропущено)", migrated, skipped)
	}
}

// copyFileToDir копіює файл src до dst (замінює якщо є).
func copyFileToDir(src, dst string) bool {
	data, err := os.ReadFile(src)
	if err != nil {
		return false
	}
	return os.WriteFile(dst, data, 0644) == nil
}

// ── АВТО-ВИЯВЛЕННЯ ЗБІРОК ─────────────────────────────────────────

// RefreshKnownShaurmaIDs оновлює кеш відомих Shaurma ID з поточного
// BuildsRegistry (викликається після міграції з SQLite).
func (s *Store) RefreshKnownShaurmaIDs() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.knownShaurmaIDs = make(map[string]bool)
	for _, e := range s.data.BuildsRegistry {
		if e.Kind == "shaurma" {
			s.knownShaurmaIDs[e.ID] = true
		}
	}
}

// ScanAndRegisterInstances сканує instancesDir та shaurma_packs/ на
// наявність папок з .minecraft/. Для кожної знайденої збірки без
// instance.json — створює конфіг за замовчуванням і реєструє в buildsRegistry.
func (s *Store) ScanAndRegisterInstances() int {
	if s.instancesDir == "" {
		return 0
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Set для швидкого пошуку вже наявних в registry
	knownIDs := map[string]bool{}
	for _, e := range s.data.BuildsRegistry {
		knownIDs[e.ID] = true
	}

	registered := 0
	var toNormalize []string
	// Скануємо дві теки: installations/ (кастомні) та installations/shaurma_packs/
	dirs := []string{s.instancesDir}
	shaurmaDir := filepath.Join(s.instancesDir, "shaurma_packs")
	if fi, err := os.Stat(shaurmaDir); err == nil && fi.IsDir() {
		dirs = append(dirs, shaurmaDir)
	}

	for _, scanDir := range dirs {
		entries, err := os.ReadDir(scanDir)
		if err != nil {
			continue
		}

		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			id := e.Name()
			instDir := filepath.Join(scanDir, id)

			// Ігноруємо службові папки (.download, .tmp тощо)
			if strings.HasPrefix(id, ".") {
				continue
			}

			// Якщо це installations/ і папка вже є в shaurma_packs/ — пропускаємо
			// (міграція перенесла збірку, але стара папка лишилась).
			if scanDir == s.instancesDir {
				duplicate := filepath.Join(shaurmaDir, id)
				if _, err := os.Stat(duplicate); err == nil {
					continue
				}
			}

			// Перевіряємо наявність instance.json АБО .minecraft папки
			cfgPath := filepath.Join(instDir, InstanceFileName)
			mcDir := filepath.Join(instDir, ".minecraft")
			hasInstanceJSON := fileExists(cfgPath)
			hasMC := dirExists(mcDir)

			if !hasInstanceJSON && !hasMC {
				continue // Не збірка — просто папка
			}

			// Створюємо instance.json з дефолтами якщо його немає
			if !hasInstanceJSON {
				cfg := model.InstanceConfig{ID: id, Name: id, Loader: "vanilla"}
				data, _ := json.MarshalIndent(cfg, "", "  ")
				_ = os.WriteFile(cfgPath, data, 0644)
			} else {
				// Назву та loader (якщо порожні) доповнюємо у ФОНІ: раніше тут
				// на кожному старті під s.mu читались і парсились усі
				// instance.json — з десятками збірок це десятки зайвих
				// файлових операцій у критичному шляху запуску.
				toNormalize = append(toNormalize, cfgPath)
			}

			// Додаємо в buildsRegistry якщо ще немає
			if !knownIDs[id] {
				// Визначаємо Kind: ПЕРШОЧЕРГОВО за тим, у якій теці фізично
				// лежить збірка (scanDir == shaurmaDir → "shaurma") — це
				// надійне джерело правди, бо саме лаунчер сам кладе збірку
				// в shaurma_packs/ при встановленні з офіційного каталогу.
				// knownShaurmaIDs — лише fallback для нетипових шляхів
				// (напр. якщо колись installationsDir сканується без
				// підпапки shaurma_packs/).
				//
				// РЕГРЕСІЯ: раніше Kind визначався ВИКЛЮЧНО за
				// knownShaurmaIDs — кешем, що наповнюється один раз при
				// старті ДО запиту packs-index.json з CDN. Якщо на той
				// момент id ще не був у кеші (нова Shaurma-збірка, перший
				// запуск на новому ПК, повільна мережа) — збірка з
				// shaurma_packs/ помилково і НАЗАВЖДИ (знайдений id більше
				// не проходить !knownIDs[id]) реєструвалась як "custom",
				// хоча фізично лежала в офіційній теці. Це і призводило до
				// того, що sidebar показував офіційну Shaurma-збірку у
				// вкладці "Кастомні".
				kind := "custom"
				if scanDir == shaurmaDir {
					kind = "shaurma"
				} else if s.knownShaurmaIDs[id] {
					kind = "shaurma"
				}
				s.data.BuildsRegistry = append(s.data.BuildsRegistry, RegistryEntry{
					ID:               id,
					Kind:             kind,
					InstalledVersion: "",
					InstalledAt:      time.Now().Unix(),
				})
				knownIDs[id] = true
				registered++
			}
		}
	}

	// Самовиправлення старих неправильних записів: збірка, яку раніше
	// (до фіксу вище) помилково зареєстрували як "custom", хоча вона
	// фізично лежить у shaurma_packs/ — виправляємо Kind на "shaurma".
	// Без цього кроку люди, хто вже зіткнувся з регресією, лишились би з
	// невірним записом назавжди (гілка реєстрації нових ID більше не
	// зачіпає вже наявні в registry).
	fixed := 0
	if _, err := os.Stat(shaurmaDir); err == nil {
		shaurmaOnDisk := map[string]bool{}
		if entries, err := os.ReadDir(shaurmaDir); err == nil {
			for _, e := range entries {
				if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
					shaurmaOnDisk[e.Name()] = true
				}
			}
		}
		for i, e := range s.data.BuildsRegistry {
			if e.Kind == "custom" && shaurmaOnDisk[e.ID] {
				s.data.BuildsRegistry[i].Kind = "shaurma"
				s.knownShaurmaIDs[e.ID] = true
				fixed++
			}
		}
	}
	if fixed > 0 {
		log.Printf("store: виправлено Kind на \"shaurma\" для %d збірок (фізично в shaurma_packs/, помилково позначені custom)", fixed)
	}

	if registered > 0 || fixed > 0 {
		_ = s.saveGlobalLocked()
		log.Printf("store: авто-виявлено %d збірок в %s", registered, s.instancesDir)
	}

	if len(toNormalize) > 0 {
		go s.normalizeInstanceFiles(toNormalize)
	}

	return registered
}

// normalizeInstanceFiles доповнює порожні name/loader в instance.json
// (колись робилось синхронно в ScanAndRegisterInstances). Кожен файл
// перечитується під s.mu — так само, як і решта записів instance.json,
// тож паралельне збереження конфігу збірки не буде перезаписане застарілим
// вмістом.
func (s *Store) normalizeInstanceFiles(paths []string) {
	defer func() { _ = recover() }()
	for _, cfgPath := range paths {
		s.mu.Lock()
		raw, err := os.ReadFile(cfgPath)
		if err == nil {
			var cfg model.InstanceConfig
			if json.Unmarshal(raw, &cfg) == nil {
				changed := false
				if cfg.Name == "" {
					cfg.Name = cfg.ID
					changed = true
				}
				if cfg.Loader == "" {
					cfg.Loader = "vanilla"
					changed = true
				}
				if changed {
					data, _ := json.MarshalIndent(cfg, "", "  ")
					_ = os.WriteFile(cfgPath, data, 0644)
				}
			}
		}
		s.mu.Unlock()
	}
}

// ── Build Layout (sidebar sorting) ───────────────────────────────

func (s *Store) loadBuildLayout() BuildLayout {
	if s.instancesDir == "" {
		return BuildLayout{}
	}
	path := filepath.Join(s.instancesDir, LayoutFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		return BuildLayout{}
	}
	var layout BuildLayout
	_ = json.Unmarshal(raw, &layout)
	return layout
}

func (s *Store) saveBuildLayout(layout BuildLayout) error {
	if s.instancesDir == "" {
		return nil
	}
	path := filepath.Join(s.instancesDir, LayoutFileName)
	data, err := json.MarshalIndent(layout, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// GetBuildLayout повертає порядок збірок у sidebar.
func (s *Store) GetBuildLayout() BuildLayout {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadBuildLayout()
}

// SaveBuildLayout зберігає порядок збірок у sidebar.
func (s *Store) SaveBuildLayout(layout BuildLayout) error {
	s.mu.Lock()
	err := s.saveBuildLayout(layout)
	s.mu.Unlock()
	return err
}

// ── UI State («останнє вікно») ───────────────────────────────────

func (s *Store) loadUIState() UIState {
	if s.instancesDir == "" {
		return UIState{}
	}
	path := filepath.Join(s.instancesDir, UIStateFileName)
	raw, err := os.ReadFile(path)
	if err != nil {
		return UIState{}
	}
	var st UIState
	_ = json.Unmarshal(raw, &st)
	return st
}

func (s *Store) saveUIState(st UIState) error {
	if s.instancesDir == "" {
		return nil
	}
	path := filepath.Join(s.instancesDir, UIStateFileName)
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// GetUIState повертає «останнє вікно» користувача.
func (s *Store) GetUIState() UIState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadUIState()
}

// SaveUIState зберігає «останнє вікно» користувача. Пишеться часто
// (кожен скрол/перемикання вкладки) — file write з .tmp+rename той
// самий, що й для BuildLayout, тому кожен запис лишається атомарним
// навіть при частих викликах.
func (s *Store) SaveUIState(st UIState) error {
	s.mu.Lock()
	err := s.saveUIState(st)
	s.mu.Unlock()
	return err
}

// ── Закладки браузера контенту ────────────────────────────────────
// .bookmarks.json, окремо від shaurma.json — той самий принцип, що для
// UIState/BuildLayout: закладки читаються/пишуться незалежно від
// головного файлу налаштувань і не змушують його перезаписуватись при
// кожному кліку на зірочку.

func (s *Store) bookmarksPath() string {
	return filepath.Join(s.instancesDir, BookmarksFileName)
}

func (s *Store) loadBookmarksData() BookmarksData {
	if s.instancesDir == "" {
		return BookmarksData{}
	}
	raw, err := os.ReadFile(s.bookmarksPath())
	if err != nil {
		return BookmarksData{}
	}
	var d BookmarksData
	_ = json.Unmarshal(raw, &d)
	return d
}

func (s *Store) saveBookmarksData(d BookmarksData) error {
	if s.instancesDir == "" {
		return nil
	}
	return atomicfile.WriteJSONAtomic(s.bookmarksPath(), d)
}

// GetBookmarksData повертає всі закладки та колекції одним знімком —
// внутрішній хелпер для App-методів, що читають обидва списки разом
// (напр. розділ «Збережено» у браузері контенту показує і те, і те).
func (s *Store) GetBookmarksData() BookmarksData {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadBookmarksData()
}

// bookmarkKey — унікальний ключ закладки/запису колекції: (source,
// projectId) для звичайної закладки, (source, projectId, collectionId)
// для запису всередині колекції — той самий проєкт МОЖЕ одночасно бути
// і окремою закладкою, і частиною кількох колекцій.
func bookmarkKey(b Bookmark) string {
	return b.Source + "|" + b.ProjectID + "|" + b.CollectionID
}

// AddBookmark додає закладку (CollectionID="") або запис колекції
// (CollectionID=ID колекції). Ідемпотентно: повторний виклик з тим самим
// (source, projectId, collectionId) оновлює існуючий запис (напр. нову
// versionId), а не створює дублікат.
func (s *Store) AddBookmark(b Bookmark) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.loadBookmarksData()
	key := bookmarkKey(b)
	if b.AddedAt.IsZero() {
		b.AddedAt = time.Now()
	}
	replaced := false
	for i, existing := range d.Bookmarks {
		if bookmarkKey(existing) == key {
			// Зберігаємо оригінальний AddedAt — «додано» не повинно
			// зсуватись наперед лише тому, що юзер оновив версію запису.
			b.AddedAt = existing.AddedAt
			d.Bookmarks[i] = b
			replaced = true
			break
		}
	}
	if !replaced {
		d.Bookmarks = append(d.Bookmarks, b)
	}
	return s.saveBookmarksData(d)
}

// RemoveBookmark прибирає закладку/запис колекції за (source, projectId,
// collectionId). collectionId="" — звичайна закладка-зірочка.
func (s *Store) RemoveBookmark(source, projectID, collectionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.loadBookmarksData()
	key := source + "|" + projectID + "|" + collectionID
	out := d.Bookmarks[:0]
	for _, b := range d.Bookmarks {
		if bookmarkKey(b) == key {
			continue
		}
		out = append(out, b)
	}
	d.Bookmarks = out
	return s.saveBookmarksData(d)
}

// IsBookmarked — чи є (source, projectId) серед звичайних закладок
// (CollectionID=""). Використовується для зірочки на картці браузера.
func (s *Store) IsBookmarked(source, projectID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.loadBookmarksData()
	for _, b := range d.Bookmarks {
		if b.CollectionID == "" && b.Source == source && b.ProjectID == projectID {
			return true
		}
	}
	return false
}

// SaveCollection створює нову колекцію (Collection.ID порожній —
// генерується) або оновлює наявну (ID вже є серед d.Collections).
func (s *Store) SaveCollection(c Collection) (Collection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.loadBookmarksData()
	now := time.Now()
	if c.ID == "" {
		c.ID = randomHex(8)
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	updated := false
	for i, existing := range d.Collections {
		if existing.ID == c.ID {
			if c.CreatedAt.IsZero() {
				c.CreatedAt = existing.CreatedAt
			}
			d.Collections[i] = c
			updated = true
			break
		}
	}
	if !updated {
		if c.CreatedAt.IsZero() {
			c.CreatedAt = now
		}
		d.Collections = append(d.Collections, c)
	}
	if err := s.saveBookmarksData(d); err != nil {
		return Collection{}, err
	}
	return c, nil
}

// DeleteCollection видаляє колекцію і ВСІ її записи (Bookmark з тим
// самим CollectionID) — групу не можна лишити «осиротілою» з записами,
// що вказують на неіснуючий ID.
func (s *Store) DeleteCollection(collectionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.loadBookmarksData()
	outColl := d.Collections[:0]
	for _, c := range d.Collections {
		if c.ID == collectionID {
			continue
		}
		outColl = append(outColl, c)
	}
	d.Collections = outColl
	outBookmarks := d.Bookmarks[:0]
	for _, b := range d.Bookmarks {
		if b.CollectionID == collectionID {
			continue
		}
		outBookmarks = append(outBookmarks, b)
	}
	d.Bookmarks = outBookmarks
	return s.saveBookmarksData(d)
}

// ── Історія пошукових запитів браузера контенту ─────────────────────────

// maxSearchHistoryEntries — межа розміру файлу історії. Найновіші
// записи лишаються, найстаріші відкидаються при перевищенні — «останні
// пошукові запити» за визначенням обмежені, необмежений журнал ніхто не
// просив і він лише роздував би файл роками використання лаунчера.
const maxSearchHistoryEntries = 30

func (s *Store) searchHistoryPath() string {
	return filepath.Join(s.instancesDir, SearchHistoryFileName)
}

func (s *Store) loadSearchHistoryData() SearchHistoryData {
	if s.instancesDir == "" {
		return SearchHistoryData{}
	}
	raw, err := os.ReadFile(s.searchHistoryPath())
	if err != nil {
		return SearchHistoryData{}
	}
	var d SearchHistoryData
	_ = json.Unmarshal(raw, &d)
	return d
}

func (s *Store) saveSearchHistoryData(d SearchHistoryData) error {
	if s.instancesDir == "" {
		return nil
	}
	return atomicfile.WriteJSONAtomic(s.searchHistoryPath(), d)
}

// GetSearchHistory повертає історію, НАЙНОВІШІ ЗАПИСИ ПЕРШИМИ — фронтенд
// рендерить список як є, без додаткового сортування.
func (s *Store) GetSearchHistory() []SearchHistoryEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.loadSearchHistoryData()
	out := make([]SearchHistoryEntry, len(d.Entries))
	copy(out, d.Entries)
	return out
}

// AddSearchHistory записує успішний пошуковий запит (той, що дав хоч
// один результат). Дедуплікує за (query normalized, projectType) —
// повторний ідентичний пошук піднімає ІСНУЮЧИЙ запис угору (оновлює
// SearchedAt/ResultCount) замість створення дубліката: інакше гравець,
// що шукає «шейдери» щотижня, мав би десяток однакових рядків у списку
// замість одного актуального. Порожній query ігнорується — «Рекомендовані»
// (пошук без введеного тексту) не засмічує історію.
func (s *Store) AddSearchHistory(query, projectType string, resultCount int) error {
	q := strings.TrimSpace(query)
	if q == "" || resultCount <= 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.loadSearchHistoryData()

	qLower := strings.ToLower(q)
	entries := d.Entries[:0]
	for _, e := range d.Entries {
		if strings.ToLower(e.Query) == qLower && e.ProjectType == projectType {
			continue // існуючий запис для цієї пари — прибираємо, додамо оновлений нижче
		}
		entries = append(entries, e)
	}
	// Новий/оновлений запис — на початок (найновіший перший).
	entries = append([]SearchHistoryEntry{{
		Query:       q,
		ProjectType: projectType,
		ResultCount: resultCount,
		SearchedAt:  time.Now(),
	}}, entries...)

	if len(entries) > maxSearchHistoryEntries {
		entries = entries[:maxSearchHistoryEntries]
	}
	d.Entries = entries
	return s.saveSearchHistoryData(d)
}

// RemoveSearchHistoryEntry прибирає один запис історії («х» на рядку
// списку — гравець може вручну приховати конкретний запит).
func (s *Store) RemoveSearchHistoryEntry(query, projectType string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.loadSearchHistoryData()
	qLower := strings.ToLower(strings.TrimSpace(query))
	out := d.Entries[:0]
	for _, e := range d.Entries {
		if strings.ToLower(e.Query) == qLower && e.ProjectType == projectType {
			continue
		}
		out = append(out, e)
	}
	d.Entries = out
	return s.saveSearchHistoryData(d)
}

// ClearSearchHistory очищає всю історію («Очистити історію» в UI).
func (s *Store) ClearSearchHistory() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveSearchHistoryData(SearchHistoryData{})
}

// saveGlobalLocked — запис з припущеним lock (викликається зсередини lock).
func (s *Store) saveGlobalLocked() error {
	if !s.isEmptyData(s.data) {
		s.backupBeforeWrite()
	}
	data, err := json.Marshal(s.data) // компактно: без відступів файл і тимчасові буфери менші
	if err != nil {
		return err
	}
	tmpPath := s.globalPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, s.globalPath)
}

// ── Допоміжні ────────────────────────────────────────────────────

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// randomHex повертає n випадкових байтів у hex-кодуванні — для коротких
// ID (напр. Collection.ID), той самий підхід, що custompack.randomHex.
func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
