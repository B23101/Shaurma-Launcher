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
)

// Pack — повний опис кастомної (не-Shaurma) збірки користувача. Джерело
// правди — файл instance.json у папці збірки (instancesDir/<id>/instance.json).
type Pack struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Description    string `json:"description,omitempty"`
	IconPath       string `json:"iconPath,omitempty"`
	BackgroundPath string `json:"backgroundPath,omitempty"`
	Icon           string `json:"icon,omitempty"`
	Color          string `json:"color,omitempty"`

	MCVersion     string `json:"mcVersion"`
	Loader        string `json:"loader"`
	LoaderVersion string `json:"loaderVersion,omitempty"`

	UseCustomRAM bool `json:"useCustomRam"`
	MinRAMMB     int  `json:"minRamMb,omitempty"`
	MaxRAMMB     int  `json:"maxRamMb,omitempty"`

	UseSeparateJava bool   `json:"useSeparateJava"`
	JavaPath        string `json:"javaPath,omitempty"`

	Source     string `json:"source"`
	ImportFile string `json:"importFile,omitempty"`

	MigratedFrom *MigrationOrigin `json:"migratedFrom,omitempty"`

	// SharedFrom — ім'я друга, від якого збірку отримано через «Поділитися»
	// (позначка походження; для решти збірок порожня).
	SharedFrom string `json:"sharedFrom,omitempty"`

	ArchivePath string `json:"archivePath,omitempty"`

	SourceProjectID   string   `json:"sourceProjectId,omitempty"`
	SourceVersionID   string   `json:"sourceVersionId,omitempty"`
	SourceProjectSlug string   `json:"sourceProjectSlug,omitempty"`
	SourceArchiveURL  string   `json:"sourceArchiveUrl,omitempty"`
	SourceFiles       []string `json:"sourceFiles,omitempty"`

	ImportState *ImportState `json:"importState,omitempty"`

	BlockedMods []BlockedModInfo `json:"blockedMods,omitempty"`

	// ── Параметри запуску (per-pack override) ──
	PlaytimeSeconds      int64 `json:"playtimeSeconds,omitempty"`
	FullscreenOverride   *bool `json:"fullscreenOverride,omitempty"`
	WindowWidthOverride  int   `json:"windowWidthOverride"`
	WindowHeightOverride int   `json:"windowHeightOverride"`

	PreLaunchCommandOverride string `json:"preLaunchCommandOverride"`
	WrapperCommandOverride   string `json:"wrapperCommandOverride"`
	PostExitCommandOverride  string `json:"postExitCommandOverride"`
	EnvVarsOverride          string `json:"envVarsOverride"`
	UseCustomCommands        bool   `json:"useCustomCommands"`

	JavaArgsOverride    string `json:"javaArgsOverride"`
	AccountIDOverride   string `json:"accountIDOverride"`
	LastDisclaimerShown string `json:"lastDisclaimerShown"`

	// ── Галерея (пер-збірка, замість спільного gallery-meta.json) ──
	GalleryMeta map[string]GalleryEntry `json:"galleryMeta,omitempty"`

	// ── Тимчасові моди («становлення на строк») ──────────────────────
	// TempMods — моди цієї збірки, на які гравець встановив дату
	// автоматичного видалення/вимкнення. Ключ мапи — FileName мода (той
	// самий ідентифікатор, що ModEntry.FileName), бо саме файл на диску є
	// стабільним ідентифікатором конкретного встановленого мода, а не
	// projectID (той самий mod міг бути встановлений вручну, без Modrinth/
	// CurseForge джерела). Стан прострочення обробляється в maintenance.go
	// (runMaintenance → cleanupExpiredTempMods), а не при кожному
	// скануванні списку модів — прибирання файлу з диска не повинно
	// залежати від того, чи відкрита зараз сторінка модів.
	TempMods map[string]TempModEntry `json:"tempMods,omitempty"`

	// ── Sidebar: чи розгорнута папка, що містить цю збірку ──
	SidebarFolderOpen bool `json:"sidebarFolderOpen,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// GalleryEntry — метадані одного скріншоту для пер-збіркової галереї.
type GalleryEntry struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

// TempModEntry — один тимчасовий мод: гравець поставив дату, після якої
// мод автоматично видаляється (або вимикається — залежно від
// GlobalSettings.TempModsDeleteByDefault/пер-запису Action нижче) з цієї
// конкретної збірки.
type TempModEntry struct {
	// ExpiresAt — момент спрацювання (UTC). Порівнюється з time.Now().UTC()
	// у cleanupExpiredTempMods — не датою без часу, щоб «1 вересня 2026»
	// однозначно означало опівночі 1 вересня за UTC, а не залежало від
	// часового поясу машини, на якій запущено перевірку.
	ExpiresAt time.Time `json:"expiresAt"`
	// Action — що робити при спрацюванні: "delete" (видалити файл мода
	// повністю) або "disable" (перейменувати на .jar.disabled, як звичайне
	// вимкнення з менеджера модів). Порожньо → бере значення з
	// GlobalSettings.TempModsDeleteByDefault (за замовчуванням — вимикати,
	// «за замовчуванням вимикаємо», як просив ужиток) — записується явно
	// в момент встановлення дати, щоб зміна глобальної настройки ПІСЛЯ
	// того, як гравець уже поставив дату конкретному моду, не змінювала
	// заднім числом його вже зроблений вибір.
	Action string `json:"action"`
	// AddedByDependency — true, якщо цей запис з'явився АВТОМАТИЧНО, бо
	// мод був підтягнутий як залежність тимчасового мода (той самий
	// ланцюжок, що встановлення з браузера контенту тягне залежності
	// разом з основним модом). Такі записи мають ту саму ExpiresAt, що
	// «батьківський» тимчасовий мод — коли він видаляється/вимикається,
	// залежності, підтягнуті ЛИШЕ заради нього (не потрібні жодному
	// іншому увімкненому моду), йдуть тим самим шляхом. Ручні (гравець сам
	// поставив дату) записи мають AddedByDependency=false і ніколи не
	// прибираються автоматично як «більше нікому не потрібна залежність».
	AddedByDependency bool `json:"addedByDependency,omitempty"`
	// RequiredBy — FileName тимчасового мода, ЗАРАДИ якого цю залежність
	// було підтягнуто (порожньо для ручних записів). Використовується,
	// щоб при видаленні батьківського тимчасового мода перевірити, чи
	// залежність усе ще потрібна ІНШИМ (постійним чи іншим тимчасовим із
	// пізнішим строком) модам, перш ніж прибирати і її теж.
	RequiredBy string `json:"requiredBy,omitempty"`
}

type MigrationOrigin struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	Launcher string `json:"launcher,omitempty"`
	OrigID   string `json:"origId,omitempty"`
	OrigPath string `json:"origPath,omitempty"`
}

type ImportState struct {
	Stage           string    `json:"stage"`
	ManifestPath    string    `json:"manifestPath,omitempty"`
	FilesCompleted  int       `json:"filesCompleted,omitempty"`
	FilesTotal      int       `json:"filesTotal,omitempty"`
	BytesDownloaded int64     `json:"bytesDownloaded,omitempty"`
	BytesTotal      int64     `json:"bytesTotal,omitempty"`
	ArchivePath     string    `json:"archivePath,omitempty"`
	UpdatedAt       time.Time `json:"updatedAt,omitempty"`
}

// BlockedModInfo — інформація про заблокований мод після імпорту.
type BlockedModInfo struct {
	FileName   string `json:"fileName"`
	WebsiteURL string `json:"websiteUrl"`
	Required   bool   `json:"required"`
	Sha1       string `json:"sha1,omitempty"`
	Downloaded bool   `json:"downloaded,omitempty"`
}

// Store — персистентний реєстр кастомних збірок. Джерело правди — файли
// instance.json у папці кожної збірки (instancesDir/<id>/instance.json).
// Жодних окремих реєстрів або хабів — дані ТІЛЬКИ в папці збірки.
type Store struct {
	mu           sync.Mutex
	instancesDir string
}

// NewStore створює Store з вказівником на папку installations/.
func NewStore(instancesDir string) *Store {
	return &Store{instancesDir: instancesDir}
}

func (s *Store) packPath(id string) string {
	return filepath.Join(s.instancesDir, id, "instance.json")
}

func (s *Store) load() map[string]Pack {
	out := make(map[string]Pack)
	if s.instancesDir == "" {
		return out
	}
	entries, err := os.ReadDir(s.instancesDir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := s.loadPack(e.Name())
		if p.ID != "" {
			out[p.ID] = p
		}
	}
	return out
}

func (s *Store) loadPack(id string) Pack {
	data, err := os.ReadFile(s.packPath(id))
	if err != nil {
		return Pack{}
	}
	var p Pack
	if json.Unmarshal(data, &p) != nil {
		return Pack{}
	}
	if p.ID == "" {
		p.ID = id
	}
	return p
}

func (s *Store) savePack(p Pack) error {
	if p.ID == "" {
		return fmt.Errorf("custompack: empty pack ID")
	}
	dir := filepath.Join(s.instancesDir, p.ID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.packPath(p.ID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, s.packPath(p.ID))
}

// List повертає всі кастомні збірки.
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

// Get повертає одну збірку за ID.
func (s *Store) Get(id string) (Pack, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.load()
	p, ok := all[id]
	return p, ok
}

// Save створює/оновлює запис збірки в instance.json.
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
	return s.savePack(p)
}

// RenameAccountRefs переносить прив'язку "запускати саме під акаунтом X"
// (AccountIDOverride) з oldAccountID на newAccountID в усіх кастомних
// збірках — виклик при перейменуванні офлайн-акаунта (див.
// App.RenameOfflineAccount), інакше збірки, прив'язані до старого ID,
// лишились би прив'язані до вже неіснуючого акаунта.
func (s *Store) RenameAccountRefs(oldAccountID, newAccountID string) error {
	if oldAccountID == "" || newAccountID == "" || oldAccountID == newAccountID {
		return nil
	}
	s.mu.Lock()
	all := s.load()
	var touched []Pack
	for _, p := range all {
		if p.AccountIDOverride == oldAccountID {
			p.AccountIDOverride = newAccountID
			touched = append(touched, p)
		}
	}
	s.mu.Unlock()
	for _, p := range touched {
		if err := s.Save(p); err != nil {
			return err
		}
	}
	return nil
}

// Delete видаляє instance.json збірки з диску.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id == "" {
		return nil
	}
	return os.Remove(s.packPath(id))
}

// slugInvalidRe — символи, які НЕ допустимі в іменах тек Windows.
var slugInvalidRe = regexp.MustCompile(`[<>:"/\\\\|?*\x00-\x1f]+`)

// SlugifyName перетворює назву збірки на ID для теки.
func SlugifyName(name string) string {
	s := strings.TrimSpace(name)
	s = slugInvalidRe.ReplaceAllString(s, "")
	s = strings.TrimRight(s, " .!~`@#$%^&()+=[]{}';,")
	s = strings.TrimLeft(s, " .")
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimSpace(s)
	if s == "" {
		s = "pack"
	}
	return s
}

// UniqueID повертає slug назви з числовим суфіксом при колізії.
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
	return base + "-" + randomHex(4)
}

// UniqueIDPackage — package-level функція для генерації унікального ID,
// що враховує вже зайняті ID з переданого списку (напр., buildsReg.AllIDs()).
func UniqueIDPackage(existingIDs []string, name string) string {
	base := SlugifyName(name)
	taken := make(map[string]bool, len(existingIDs))
	for _, id := range existingIDs {
		taken[id] = true
	}
	if !taken[base] {
		return base
	}
	for i := 2; i < 1000; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if !taken[candidate] {
			return candidate
		}
	}
	return base + "-" + randomHex(4)
}

// UniqueIDAndName — те саме, що UniqueID, але повертає і ID, і назву з суфіксом.
func (s *Store) UniqueIDAndName(name string) (string, string) {
	base := SlugifyName(name)
	displayName := strings.TrimSpace(name)
	if displayName == "" {
		displayName = base
	}
	s.mu.Lock()
	all := s.load()
	s.mu.Unlock()
	if _, taken := all[base]; !taken {
		return base, displayName
	}
	for i := 2; i < 1000; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		candidateName := fmt.Sprintf("%s (%d)", displayName, i)
		if _, taken := all[candidate]; !taken {
			return candidate, candidateName
		}
	}
	return base + "-" + randomHex(4), displayName + " " + randomHex(4)
}

// FindBySource шукає збірку за source + projectID.
func (s *Store) FindBySource(source, projectID string) (Pack, bool) {
	if source == "" || projectID == "" {
		return Pack{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.load()
	for _, p := range all {
		if p.Source == source && p.SourceProjectID == projectID {
			return p, true
		}
	}
	return Pack{}, false
}

// Rename перейменовує збірку (змінює ID, перейменовує папку).
func (s *Store) Rename(instancesDir, oldID, newID string) (Pack, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	all := s.load()
	p, ok := all[oldID]
	if !ok {
		return Pack{}, fmt.Errorf("збірку %q не знайдено", oldID)
	}

	// Перейменовуємо папку
	oldDir := filepath.Join(instancesDir, oldID)
	newDir := filepath.Join(instancesDir, newID)
	if err := os.MkdirAll(filepath.Dir(newDir), 0755); err != nil {
		return Pack{}, err
	}
	if err := os.Rename(oldDir, newDir); err != nil {
		return Pack{}, fmt.Errorf("перейменування папки: %w", err)
	}

	// Оновлюємо ID та ремапимо шляхи
	p.ID = newID
	p.UpdatedAt = time.Now()
	remapPath := func(p string) string {
		if p == "" {
			return p
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			return p
		}
		if samePathWithin(abs, oldDir) {
			rel, err := filepath.Rel(oldDir, abs)
			if err != nil {
				return p
			}
			return filepath.Join(newDir, rel)
		}
		return p
	}
	p.IconPath = remapPath(p.IconPath)
	p.BackgroundPath = remapPath(p.BackgroundPath)
	p.ArchivePath = remapPath(p.ArchivePath)

	if err := s.savePack(p); err != nil {
		return Pack{}, err
	}

	return p, nil
}

// RemapAssetPaths пере-мапить абсолютні шляхи асетів після зміни папки даних.
func (s *Store) RemapAssetPaths(oldBase, newBase string) error {
	if oldBase == "" || newBase == "" || oldBase == newBase {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.load()
	changed := false
	remap := func(p string) string {
		if p == "" {
			return p
		}
		if !samePathWithin(p, oldBase) {
			return p
		}
		rel, err := filepath.Rel(oldBase, p)
		if err != nil {
			return p
		}
		return filepath.Join(newBase, rel)
	}
	for _, p := range all {
		np := p
		np.IconPath = remap(p.IconPath)
		np.BackgroundPath = remap(p.BackgroundPath)
		np.ArchivePath = remap(p.ArchivePath)
		if np.IconPath != p.IconPath || np.BackgroundPath != p.BackgroundPath || np.ArchivePath != p.ArchivePath {
			_ = s.savePack(np)
			changed = true
		}
	}
	_ = changed
	return nil
}

// ── Галерея (пер-збіркові метадані скріншотів) ──────────────────────────

// GetGallery повертає метадані всіх скріншотів збірки.
func (s *Store) GetGallery(packID string) map[string]GalleryEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.loadPack(packID)
	if p.GalleryMeta == nil {
		return map[string]GalleryEntry{}
	}
	out := make(map[string]GalleryEntry, len(p.GalleryMeta))
	for k, v := range p.GalleryMeta {
		out[k] = v
	}
	return out
}

// SetGallery зберігає метадані одного скріншоту збірки.
func (s *Store) SetGallery(packID, fileName string, m GalleryEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.loadPack(packID)
	if p.GalleryMeta == nil {
		p.GalleryMeta = map[string]GalleryEntry{}
	}
	if m.Title == "" && m.Description == "" {
		delete(p.GalleryMeta, fileName)
	} else {
		p.GalleryMeta[fileName] = m
	}
	if len(p.GalleryMeta) == 0 {
		p.GalleryMeta = nil
	}
	return s.savePack(p)
}

// DeleteGallery видаляє метадані скріншоту збірки.
func (s *Store) DeleteGallery(packID, fileName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.loadPack(packID)
	if p.GalleryMeta != nil {
		delete(p.GalleryMeta, fileName)
		if len(p.GalleryMeta) == 0 {
			p.GalleryMeta = nil
		}
	}
	return s.savePack(p)
}

// RenameGallery перейменовує ключ метаданих скріншоту збірки.
func (s *Store) RenameGallery(packID, oldName, newName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.loadPack(packID)
	if p.GalleryMeta == nil {
		return nil
	}
	if m, ok := p.GalleryMeta[oldName]; ok {
		delete(p.GalleryMeta, oldName)
		p.GalleryMeta[newName] = m
		return s.savePack(p)
	}
	return nil
}

// ── Тимчасові моди («становлення на строк») ─────────────────────────────

// GetTempMods повертає копію мапи тимчасових модів збірки (fileName →
// TempModEntry) — той самий принцип копіювання, що GetGallery, щоб
// виклик, який тримає результат довше за один запит, не тримав живе
// посилання на внутрішній стан Store під м'ютексом.
func (s *Store) GetTempMods(packID string) map[string]TempModEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.loadPack(packID)
	if p.TempMods == nil {
		return map[string]TempModEntry{}
	}
	out := make(map[string]TempModEntry, len(p.TempMods))
	for k, v := range p.TempMods {
		out[k] = v
	}
	return out
}

// SetTempMod ставить/оновлює тимчасову мітку одного файлу мода.
func (s *Store) SetTempMod(packID, fileName string, e TempModEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.loadPack(packID)
	if p.TempMods == nil {
		p.TempMods = map[string]TempModEntry{}
	}
	p.TempMods[fileName] = e
	return s.savePack(p)
}

// RemoveTempMod знімає тимчасову мітку з файлу мода (мод лишається у
// збірці як звичайний — «зробити постійним»/скасувати помилково
// поставлену дату).
func (s *Store) RemoveTempMod(packID, fileName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.loadPack(packID)
	if p.TempMods == nil {
		return nil
	}
	delete(p.TempMods, fileName)
	if len(p.TempMods) == 0 {
		p.TempMods = nil
	}
	return s.savePack(p)
}

// AllPacksWithTempMods повертає (packID, fileName, TempModEntry) для
// КОЖНОЇ тимчасової мітки в КОЖНІЙ збірці одразу — для проходу
// прибиральника (maintenance.go → cleanupExpiredTempMods), якому треба
// перевірити строки всіх збірок за один прохід, не викликаючи GetTempMods
// в циклі по List() (що взяло б м'ютекс Store на кожну збірку окремо —
// цей метод бере його ОДИН РАЗ на весь прохід).
func (s *Store) AllPacksWithTempMods() map[string]map[string]TempModEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.load()
	out := map[string]map[string]TempModEntry{}
	for id, p := range all {
		if len(p.TempMods) == 0 {
			continue
		}
		m := make(map[string]TempModEntry, len(p.TempMods))
		for k, v := range p.TempMods {
			m[k] = v
		}
		out[id] = m
	}
	return out
}

// ── Sidebar folder state ───────────────────────────────────────────────

// GetSidebarFolderOpen повертає стан розгорнутості папки в sidebar.
func (s *Store) GetSidebarFolderOpen(packID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadPack(packID).SidebarFolderOpen
}

// SetSidebarFolderOpen зберігає стан розгорнутості папки в sidebar.
func (s *Store) SetSidebarFolderOpen(packID string, open bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.loadPack(packID)
	p.SidebarFolderOpen = open
	return s.savePack(p)
}

func samePathWithin(p, base string) bool {
	if p == "" || base == "" {
		return false
	}
	ap, err := filepath.Abs(p)
	if err != nil {
		return false
	}
	ab, err := filepath.Abs(base)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(ab, ap)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}

func relativeIfAbsolute(p, instanceDir, id string) string {
	if p == "" {
		return p
	}
	packDir := filepath.Join(instanceDir, id)
	if !filepath.IsAbs(p) {
		return p
	}
	if !samePathWithin(p, packDir) {
		return p
	}
	rel, err := filepath.Rel(packDir, p)
	if err != nil {
		return p
	}
	return rel
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
