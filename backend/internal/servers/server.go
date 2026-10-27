// Package servers — менеджер серверів Minecraft у лаунчері.
//
// Підтримуємо ядра: vanilla, forge, neoforge, fabric, quilt, paper, purpur, velocity.
// Зберігання: .shaurma/server.json у папці кожного сервера.
// Сервери знаходяться скануванням папок — жодного центрального index.json.
package servers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Supported cores.
const (
	CoreVanilla  = "vanilla"
	CoreForge    = "forge"
	CoreNeoForge = "neoforge"
	CoreFabric   = "fabric"
	CoreQuilt    = "quilt"
	CorePaper    = "paper"
	CorePurpur   = "purpur"
	CoreVelocity = "velocity"
)

// Server — повна конфігурація одного сервера, збережена в .shaurma/server.json.
type Server struct {
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	Dir            string       `json:"dir"`         // абсолютний шлях до папки сервера
	Core           string       `json:"core"`        // vanilla|forge|neoforge|fabric|quilt|paper|purpur|velocity
	MCVersion      string       `json:"mcVersion"`   // "1.20.1", "26.2", ...
	LoaderVer      string       `json:"loaderVer"`   // версія лоадера (Forge "47.4.10", Fabric loader, ...)
	JavaMajor      int          `json:"javaMajor"`   // 8|16|17|21|25
	RAMMB          int          `json:"ramMB"`       // Xmx/Xms
	JvmArgs        string       `json:"jvmArgs"`     // додаткові JVM-аргументи (порожньо = пресет за Java)
	ProgramArgs    string       `json:"programArgs"` // "--nogui" тощо
	Port           int          `json:"port"`        // server.properties: server-port
	IconPath       string       `json:"iconPath"`    // server-icon.png (протокольна іконка для гри)
	// Icon — пресет-іконка (Tabler, напр. "ti-server-2") для UI ЛАУНЧЕРА.
	// Дзеркало custompack.Pack.Icon: той самий компонент вибору, той самий
	// контракт, той самий вигляд у формі й у списках. PNG (LauncherIconPath)
	// має пріоритет над пресетом.
	Icon             string       `json:"icon,omitempty"`
	// LauncherIconPath — ВІДНОСНИЙ шлях (лише ім'я файлу) до PNG/зображення
	// іконки лаунчера у <serverDir>/.shaurma/launcher-icon.<ext>.
	// ВАЖЛИВО: це НЕ IconPath (server-icon.png для гравців у грі) — окремі
	// речі з різними вимогами до формату й розміру.
	LauncherIconPath string       `json:"launcherIconPath,omitempty"`
	Color          string       `json:"color"`       // колір у бібліотеці
	Notes          string       `json:"notes"`
	NetworkID      string       `json:"networkId,omitempty"`
	NetworkRole    string       `json:"networkRole,omitempty"` // proxy|lobby|minigame|other
	NetworkOrder   int          `json:"networkOrder,omitempty"`
	NetworkWaitSec int          `json:"networkWaitSec,omitempty"`
	AutoBackup     BackupConfig `json:"autoBackup"`
	// AutoStartP2P — піднімати P2P-тунель автоматично разом із сервером
	// (перемикач у віджеті «Мережа»). Зберігається в самому сервері, тому
	// переживає перезапуск лаунчера — на відміну від localStorage-ключів.
	AutoStartP2P    bool         `json:"autoStartP2P,omitempty"`
	CreatedAt      int64        `json:"createdAt"`
}

// BackupConfig — налаштування періодичних бекапів світів.
type BackupConfig struct {
	Enabled     bool   `json:"enabled"`
	IntervalMin int    `json:"intervalMin"` // 0 = тільки ручні
	KeepCount   int    `json:"keepCount"`   // лишити N останніх (0 = безліміт)
	KeepDays    int    `json:"keepDays"`    // видалити старші за N днів (0 = безліміт)
	// MaxPerDay — скільки авто-бекапів на добу максимум (0 = без ліміту).
	// Захист від «бекап що 5 хвилин залив увесь диск» при короткому
	// IntervalMin: ліміт рахується за календарною добою (UTC-дата файлу).
	MaxPerDay int    `json:"maxPerDay"`
	WorldDir  string `json:"worldDir"` // зазвичай = level-name з server.properties
	// BackupOnStop — бекап світів після ЗВИЧАЙНОЇ зупинки сервера. Це
	// найцінніший знімок: Java вже закрила регіони й дописала level.dat, тож
	// копія консистентна (на відміну від бекапу під час роботи). Kill цю
	// гілку НЕ тригерить — примусове завершення може лишити світ битим.
	BackupOnStop bool `json:"backupOnStop,omitempty"`
	// BackupOnStart — бекап світів при старті сервера (знімок «як було до
	// сесії»). Щойно створений світ не існує — бекап просто пропускається.
	BackupOnStart bool `json:"backupOnStart,omitempty"`
}

// ServerStatus — поточний статус процесу сервера.
type ServerStatus struct {
	Status    string `json:"status"` // stopped|starting|running|stopping|crashed|error
	PID       int    `json:"pid,omitempty"`
	UptimeSec int64  `json:"uptimeSec,omitempty"`
	ExitCode  int    `json:"exitCode,omitempty"`
	LastError string `json:"lastError,omitempty"`
	// Recovered — керування процесом відновлено після перезапуску
	// лаунчера (recover.go): сервер справді працює, але консоль і команди
	// недоступні, бо pipe помер разом із попереднім процесом лаунчера.
	Recovered bool `json:"recovered,omitempty"`
}

// ServerView — те, що бачить фронтенд.
type ServerView struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Dir           string `json:"dir"`
	Core          string `json:"core"`
	CoreLabel     string `json:"coreLabel"`
	MCVersion     string `json:"mcVersion"`
	LoaderVer     string `json:"loaderVer"`
	JavaMajor     int    `json:"javaMajor"`
	JavaInstalled bool   `json:"javaInstalled"`
	CoreInstalled bool   `json:"coreInstalled"` // false = ядро довантажиться при першому старті (як збірка)
	RAMMB         int    `json:"ramMB"`
	Port          int    `json:"port"`
	Status        string `json:"status"`
	PID           int    `json:"pid,omitempty"`
	UptimeSec     int64  `json:"uptimeSec,omitempty"`
	ExitCode      int    `json:"exitCode,omitempty"`
	// Recovered — сервер працює, але керування відновлено після
	// перезапуску лаунчера (recover.go). UI має попередити, що консоль і
	// команди недоступні, а «Стоп» лишається робочим (через kill PID).
	Recovered bool `json:"recovered,omitempty"`
	// Конфіг-поля, які редактор налаштувань повертає у форму при відкритті
	// (щоб збереження JVM-аргументів не затирало їх порожнім рядком).
	JvmArgs     string       `json:"jvmArgs"`
	ProgramArgs string       `json:"programArgs"`
	Notes       string       `json:"notes"`
	IconData    string       `json:"iconData,omitempty"` // data URL 64x64 (протокольна server-icon.png)
	// Icon — пресет-іконка лаунчера (Tabler).
	Icon string `json:"icon,omitempty"`
	// LauncherIconData — data URL іконки ЛАУНЧЕРА (окремий файл, без
	// ресайзу до 64×64) — пріоритетніша за Icon/IconData у UI.
	LauncherIconData string `json:"launcherIconData,omitempty"`
	Color            string `json:"color,omitempty"`
	NetworkID        string `json:"networkId,omitempty"`
	NetworkRole      string `json:"networkRole,omitempty"`
	HasWorlds        bool   `json:"hasWorlds"`
	HasMods          bool   `json:"hasMods"`
	HasPlugins       bool   `json:"hasPlugins"`
	// AutoStartP2P — автозапуск P2P-тунеля разом із сервером (див. Server).
	AutoStartP2P bool         `json:"autoStartP2P"`
	LastError    string       `json:"lastError,omitempty"`
	CreatedAt    int64        `json:"createdAt"`
	AutoBackup   BackupConfig `json:"autoBackup"`
}

// NewServerRequest — запит на створення нового сервера.
type NewServerRequest struct {
	Core      string `json:"core"`
	MCVersion string `json:"mcVersion"`
	LoaderVer string `json:"loaderVer"` // порожньо = рекомендована
	Name      string `json:"name"`
	// ID — ім'я теки сервера, яке гравець може задати сам (як ID збірки).
	// Порожнє = вивести з Name. Дозволено кирилицю й пробіли (див.
	// SanitizeServerFolder) — це НЕ технічний хеш, а людська назва теки.
	ID          string `json:"id"`
	Dir         string `json:"dir"` // порожньо = auto
	RAMMB       int    `json:"ramMB"`
	Port        int    `json:"port"` // 0 = авто-підбір
	AcceptEULA  bool   `json:"acceptEula"`
	JavaMajor   int    `json:"javaMajor"` // 0 = авто за MC
	JvmArgs     string `json:"jvmArgs"`
	ProgramArgs string `json:"programArgs"`
}

// ChangeCoreRequest — запит на зміну ядра/версії ВЖЕ створеного сервера
// (вкладка «Налаштування» → «Ядро»). Порожні поля означають «лишити як є»:
// порожній Core — поточне ядро, порожня MCVersion — поточна версія гри
// (для Velocity версія гри взагалі не застосовна).
type ChangeCoreRequest struct {
	Core      string `json:"core"`
	MCVersion string `json:"mcVersion"`
	LoaderVer string `json:"loaderVer"` // порожньо = рекомендована (лише якщо ядро/версія реально змінюються)
	JavaMajor int    `json:"javaMajor"` // 0 = авто за версією гри
}

// CoreDetectResult — результат аналізу папки.
type CoreDetectResult struct {
	Core         string   `json:"core"`
	MCVersion    string   `json:"mcVersion"`
	LoaderVer    string   `json:"loaderVer"`
	JavaMajor    int      `json:"javaMajor"`
	Port         int      `json:"port"`
	Confidence   string   `json:"confidence"` // high|medium|low
	Messages     []string `json:"messages"`
	ServerJar    string   `json:"serverJar,omitempty"`
	InstallerJar string   `json:"installerJar,omitempty"`
}

// ServerFolderCheck — результат перевірки ID (імені теки) сервера.
type ServerFolderCheck struct {
	Folder string `json:"folder"`           // як ID виглядатиме на диску
	Free   bool   `json:"free"`             // чи вільний (немає сервера в цій теці)
	Reason string `json:"reason,omitempty"` // чому ID змінили/зайнято
}

// SanitizeServerFolder приводить довільний рядок до БЕЗПЕЧНОГО імені теки
// сервера.
//
// На відміну від slugify (латиниця + дефіс, потрібний для сумісності зі
// старими теками), тут ДОЗВОЛЕНО кирилицю й пробіли: гравець має право
// назвати сервер «Мій Сервер З Друзями» і бачити той самий ID — а не
// безлике «server» (саме цим slugify дратував: усі кириличні назви
// злипались в одну теку). Забороняємо ТІЛЬКИ те, що фізично ламає шлях у
// Windows: керуючі символи, <>:"/\|?*, службові імена (CON, NUL, COM1…) і
// кінцеві крапки/пробіли.
const maxServerFolderRunes = 120

func SanitizeServerFolder(s string) string {
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		switch {
		case r < 0x20 || r == 0x7f:
			continue
		case strings.ContainsRune(`<>:"/\|?*`, r):
			continue
		case unicode.IsLetter(r) || unicode.IsDigit(r) ||
			r == '-' || r == '_' || r == '.' || r == '(' || r == ')' ||
			r == '+' || r == '&' || r == '\'' || r == '!':
			b.WriteRune(r)
			prevSpace = false
		case unicode.IsSpace(r):
			if !prevSpace && b.Len() > 0 {
				b.WriteRune(' ')
			}
			prevSpace = true
		default:
			// Емодзі та рідкісні символи — у дефіс, щоб не лишати дірку.
			if !prevSpace && b.Len() > 0 {
				b.WriteRune('-')
			}
			prevSpace = true
		}
	}
	out := strings.Trim(b.String(), " .")
	// Windows обмежує ім'я теки 255 символами, а всередині ще лежить
	// .shaurma/server.json — тримаємо запас і не даємо CreateServer впасти
	// з незрозумілою помилкою ОС на абсурдно довгій назві.
	if r := []rune(out); len(r) > maxServerFolderRunes {
		out = strings.Trim(string(r[:maxServerFolderRunes]), " .")
	}
	if out == "" {
		return "server"
	}
	if isReservedWindowsName(out) {
		// «CON»/«NUL»/«COM1» Windows не дасть створити як теку — додаємо префікс.
		return "srv-" + out
	}
	return out
}

// isReservedWindowsName — імена, заборонені у Windows (регістр не важливий,
// розширення ігнорується: «con.txt» теж заборонене).
func isReservedWindowsName(name string) bool {
	base := name
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	switch strings.ToUpper(strings.TrimSpace(base)) {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5",
		"COM6", "COM7", "COM8", "COM9", "LPT1", "LPT2", "LPT3", "LPT4", "LPT5",
		"LPT6", "LPT7", "LPT8", "LPT9":
		return true
	}
	return false
}

// ServerConfigPatch — часткове оновлення конфігу сервера.
type ServerConfigPatch struct {
	Name        *string `json:"name,omitempty"`
	RAMMB       *int    `json:"ramMB,omitempty"`
	JvmArgs     *string `json:"jvmArgs,omitempty"`
	ProgramArgs *string `json:"programArgs,omitempty"`
	Port        *int    `json:"port,omitempty"`
	// Icon — пресет-іконка лаунчера. Патч-семантика (*string): nil =
	// «не чіпати», порожній рядок = «прибрати пресет». Це структурно
	// захищає від того самого класу бага, що був у custompack.Update()
	// (плаский CreateParams тягнув застаріле поле форми при кожному
	// збереженні) — тут поле рухається лише коли його реально прислали.
	Icon  *string `json:"icon,omitempty"`
	Color *string `json:"color,omitempty"`
	Notes       *string `json:"notes,omitempty"`
	JavaMajor   *int    `json:"javaMajor,omitempty"`
	// AutoBackup — налаштування періодичних бекапів (окремий об'єкт,
	// тому його можна зберегти НЕ чіпаючи решту конфігу — так і робить
	// картка «Авто-бекап» на вкладці «Світи»).
	AutoBackup   *BackupConfig `json:"autoBackup,omitempty"`
	// AutoStartP2P — піднімати P2P-тунель разом із сервером.
	AutoStartP2P *bool    `json:"autoStartP2P,omitempty"`
}

// ── Зберігання ──────────────────────────────────────────────────────────

// Store — потоко-безпечне зберігання серверів.
// Кожен сервер зберігає конфігурацію в <serverDir>/.shaurma/server.json.
// НІЯКОГО центрального index.json — сервери знаходяться скануванням папок.
//
// ВИНЯТОК — сервери, додані «з папки» поза <dataDir>/servers/ (напр.
// D:\\mc\\myserver): їх НЕМАЄ у головній теці, тож простого сканування
// недостатньо. Раніше такі сервери зникали після перезапуску лаунчера
// (шлях «губився», реєстрація в бібліотеці пропадала) — тепер кожен
// зовнішній шлях додається у реєстр servers-external.json поруч із
// <dataDir>/servers/ і переживає будь-яку кількість перезаходів.
type Store struct {
	mu       sync.RWMutex
	servers  map[string]*Server // id → Server
	dataDir  string             // коренева тека лаунчера (<dataDir>/servers/ сканується)
	scanDirs []string           // додаткові теки для сканування (абсолютні шляхи)
	// extDirs — АБСОЛЮТНІ шляхи САМИХ тек серверів поза <dataDir>/servers/.
	// На відміну від scanDirs (база, чиї ДІТИ скануються) — це готові теки:
	// перевіряється одразу <dir>/.shaurma/server.json.
	extDirs []string
}

// serverJSON — те, що зберігається в .shaurma/server.json.
type serverJSON = Server

// NewStore створює store і сканує <dataDir>/servers/ для серверів.
func NewStore(dataDir string) *Store {
	s := &Store{
		servers: make(map[string]*Server),
		dataDir: dataDir,
	}
	s.loadExternalDirs()
	s.scanServersDirs()
	return s
}

// externalRegistryPath — реєстр зовнішніх тек серверів (поруч із текою
// servers/, а НЕ всередині неї: сканувати <dataDir>/servers/ — це читати
// ДІТИ каталогу, файл у ньому просто ігнорується, але тримати реєстр
// окремо зрозуміліше й безпечніше).
func (s *Store) externalRegistryPath() string {
	return filepath.Join(s.dataDir, "servers-external.json")
}

// loadExternalDirs читає реєстр зовнішніх тек у пам'ять (старий/битий
// файл — не помилка, просто порожній реєстр).
func (s *Store) loadExternalDirs() {
	data, err := os.ReadFile(s.externalRegistryPath())
	if err != nil {
		return
	}
	var dirs []string
	if json.Unmarshal(data, &dirs) != nil {
		return
	}
	s.extDirs = dirs
}

// saveExternalDirs атомарно пише реєстр зовнішніх тек (викликається під
// м'ютексем або з гарантією, що паралельних записів немає).
func (s *Store) saveExternalDirs() {
	data, err := json.MarshalIndent(s.extDirs, "", "  ")
	if err != nil {
		return
	}
	path := s.externalRegistryPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	_ = writeFileAtomic(path, data, 0644)
}

// isManagedDir — тека сервера лежить у стандартній <dataDir>/servers/.
func (s *Store) isManagedDir(dir string) bool {
	base := filepath.Join(s.dataDir, "servers")
	rel, err := filepath.Rel(base, dir)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// registerExternal додає теку сервера в реєстр (якщо вона поза стандартною)
// і зберігає реєстр на диск.
func (s *Store) registerExternal(dir string) {
	clean := filepath.Clean(dir)
	if s.isManagedDir(clean) {
		return
	}
	for _, d := range s.extDirs {
		if filepath.Clean(d) == clean {
			return
		}
	}
	s.extDirs = append(s.extDirs, clean)
	s.saveExternalDirs()
}

// unregisterExternal прибирає теку з реєстру (сервер видалено/перенесено).
func (s *Store) unregisterExternal(dir string) {
	clean := filepath.Clean(dir)
	out := s.extDirs[:0]
	changed := false
	for _, d := range s.extDirs {
		if filepath.Clean(d) == clean {
			changed = true
			continue
		}
		out = append(out, d)
	}
	if changed {
		s.extDirs = out
		s.saveExternalDirs()
	}
}

// AddScanDir додає додаткову теку для сканування серверів.
func (s *Store) AddScanDir(dir string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scanDirs = append(s.scanDirs, dir)
	s.scanServersDirsLocked()
}

// scanServersDirs сканує <dataDir>/servers/ + додаткові теки на наявність
// серверів (папки з .shaurma/server.json).
func (s *Store) scanServersDirs() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scanServersDirsLocked()
}

func (s *Store) scanServersDirsLocked() {
	// Головна тека серверів
	allDirs := []string{}
	serversBase := filepath.Join(s.dataDir, "servers")
	allDirs = append(allDirs, serversBase)
	allDirs = append(allDirs, s.scanDirs...)

	knownIDs := map[string]bool{}
	for id := range s.servers {
		knownIDs[id] = true
	}

	for _, baseDir := range allDirs {
		entries, err := os.ReadDir(baseDir)
		if err != nil {
			continue // папки немає — ОК
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			dir := filepath.Join(baseDir, e.Name())
			jsonPath := filepath.Join(dir, ".shaurma", "server.json")
			raw, err := os.ReadFile(jsonPath)
			if err != nil {
				continue // сервера тут немає
			}
			var srv Server
			if err := json.Unmarshal(raw, &srv); err != nil {
				continue
			}
			if srv.ID == "" {
				continue
			}
			if !knownIDs[srv.ID] {
				s.servers[srv.ID] = &srv
				knownIDs[srv.ID] = true
			}
		}
	}

	// Зовнішні теки (додані «з папки»): перевіряємо КОЖЕН реєстрований
	// шлях напряму. Биті/вилучені записи прибираємо з реєстру, щоб файл
	// не ріс сміттям із роками.
	stale := false
	for _, dir := range s.extDirs {
		jsonPath := filepath.Join(dir, ".shaurma", "server.json")
		raw, err := os.ReadFile(jsonPath)
		if err != nil {
			stale = true
			continue
		}
		var srv Server
		if err := json.Unmarshal(raw, &srv); err != nil || srv.ID == "" {
			stale = true
			continue
		}
		if !knownIDs[srv.ID] {
			s.servers[srv.ID] = &srv
			knownIDs[srv.ID] = true
		}
	}
	if stale {
		out := s.extDirs[:0]
		for _, dir := range s.extDirs {
			if _, err := os.Stat(filepath.Join(dir, ".shaurma", "server.json")); err == nil {
				out = append(out, dir)
			}
		}
		s.extDirs = out
		s.saveExternalDirs()
	}
}

// ServerJSONPath повертає шлях до .shaurma/server.json для сервера.
func (s *Store) ServerJSONPath(serverDir string) string {
	return filepath.Join(serverDir, ".shaurma", "server.json")
}

// LoadAll сканує індекс і завантажує всі сервери.
func (s *Store) LoadAll() map[string]*Server {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]*Server, len(s.servers))
	for id, srv := range s.servers {
		out[id] = srv
	}
	return out
}

// Get повертає сервер за ID.
func (s *Store) Get(id string) (*Server, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	srv, ok := s.servers[id]
	if !ok {
		return nil, fmt.Errorf("сервер не знайдено: %s", id)
	}
	return srv, nil
}

// GetNoErr повертає сервер і прапорець успіху (без помилки) — зручно в
// циклах, де сервер може бути відсутнім.
func (s *Store) GetNoErr(id string) (*Server, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	srv, ok := s.servers[id]
	return srv, ok
}

// Save зберігає сервер: пише .shaurma/server.json та оновлює індекс.
func (s *Store) Save(srv *Server) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Створюємо .shaurma/ якщо немає.
	shaurmaDir := filepath.Join(srv.Dir, ".shaurma")
	if err := os.MkdirAll(shaurmaDir, 0755); err != nil {
		return fmt.Errorf("створення .shaurma: %w", err)
	}

	// Записуємо server.json.
	data, err := json.MarshalIndent(srv, "", "  ")
	if err != nil {
		return fmt.Errorf("серіалізація: %w", err)
	}
	jsonPath := filepath.Join(shaurmaDir, "server.json")
	if err := os.WriteFile(jsonPath, data, 0644); err != nil {
		return fmt.Errorf("запис %s: %w", jsonPath, err)
	}

	// Тека поза <dataDir>/servers/ → реєструємо в servers-external.json,
	// щоб сервер пережив перезапуск лаунчера (інакше сканування головної
	// теки його просто не побачить і «реєстрація зникне»).
	s.registerExternal(srv.Dir)

	// Оновлюємо кеш у пам'яті.
	s.servers[srv.ID] = srv
	return nil
}

// Delete видаляє сервер з індексу (файли сервера НЕ видаляються).
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	srv, ok := s.servers[id]
	if !ok {
		return fmt.Errorf("сервер не знайдено: %s", id)
	}

	// Видаляємо .shaurma/server.json.
	jsonPath := filepath.Join(srv.Dir, ".shaurma", "server.json")
	os.Remove(jsonPath) // не критично, якщо не вдалось

	// Зовнішня тека більше не сервер — прибираємо і з реєстру.
	s.unregisterExternal(srv.Dir)

	delete(s.servers, id)
	return nil
}

// Refresh сканує теки заново для оновлення списку серверів.
func (s *Store) Refresh() {
	s.scanServersDirs()
}

// ── Допоміжні ───────────────────────────────────────────────────────────

// CoreLabel повертає локалізовану назву ядра.
func CoreLabel(core string) string {
	switch core {
	case CoreVanilla:
		return "Vanilla"
	case CoreForge:
		return "Forge"
	case CoreNeoForge:
		return "NeoForge"
	case CoreFabric:
		return "Fabric"
	case CoreQuilt:
		return "Quilt"
	case CorePaper:
		return "Paper"
	case CorePurpur:
		return "Purpur"
	case CoreVelocity:
		return "Velocity"
	default:
		return core
	}
}

// HasMods повертає true, якщо ядро підтримує моди.
func HasMods(core string) bool {
	switch core {
	case CoreForge, CoreNeoForge, CoreFabric, CoreQuilt:
		return true
	}
	return false
}

// HasPlugins повертає true, якщо ядро підтримує плагіни.
func HasPlugins(core string) bool {
	switch core {
	case CorePaper, CorePurpur, CoreVelocity:
		return true
	}
	return false
}

// HasWorlds повертає true, якщо сервер має світи (не velocity).
func HasWorlds(core string) bool {
	return core != CoreVelocity
}

// JavaMajorForVersion повертає рекомендований Java major за MC-версією.
// Формат: "1.20.1" (major=1, minor=20), "26.2" (major=26).
func JavaMajorForVersion(mcVersion string) int {
	var major, minor, patch int
	fmt.Sscanf(mcVersion, "%d.%d.%d", &major, &minor, &patch)
	if patch == 0 {
		fmt.Sscanf(mcVersion, "%d.%d", &major, &minor)
	}

	// Нова річна нумерація: 26.x → Java 25
	if major >= 26 {
		return 25
	}

	// MC-версія: "1.X.Y" → X; "26.1" → major=26 (вже оброблено).
	mcVer := minor // для "1.20.1" mcVer=20
	if major != 1 {
		mcVer = major
	}

	switch {
	case mcVer >= 21:
		return 21
	case mcVer == 20 && patch >= 5:
		return 21
	case mcVer == 20:
		return 17
	case mcVer == 19:
		return 17
	case mcVer == 18:
		return 17
	case mcVer == 17:
		return 16
	case mcVer >= 12:
		return 8
	default:
		return 8
	}
}

// CoreForStopCommand повертає команду зупинки для ядра.
func CoreForStopCommand(core string) string {
	if core == CoreVelocity {
		return "end"
	}
	return "stop"
}

// DefaultJVMArgs повертає пресет JVM-аргументів за Java major + RAM.
func DefaultJVMArgs(javaMajor, ramMB int) string {
	if javaMajor >= 25 {
		// ZGC для Java 25+ (MC 26.x)
		return fmt.Sprintf("-Xms%dM -Xmx%dM -XX:+UseZGC -XX:+ZGenerational -XX:+AlwaysPreTouch -XX:+UnlockExperimentalVMOptions",
			ramMB, ramMB)
	}
	// Aikar's flags для Java ≤21
	return fmt.Sprintf(`-Xms%dM -Xmx%dM -XX:+UseG1GC -XX:+ParallelRefProcEnabled -XX:MaxGCPauseMillis=200 -XX:+UnlockExperimentalVMOptions -XX:+DisableExplicitGC -XX:+AlwaysPreTouch -XX:G1NewSizePercent=30 -XX:G1MaxNewSizePercent=40 -XX:G1HeapRegionSize=8M -XX:G1ReservePercent=20 -XX:G1HeapWastePercent=5 -XX:G1MixedGCCountTarget=4 -XX:InitiatingHeapOccupancyPercent=15 -XX:G1MixedGCLiveThresholdPercent=90 -XX:G1RSetUpdatingPauseTimePercent=5 -XX:SurvivorRatio=32 -XX:+PerfDisableSharedMem -XX:MaxTenuringThreshold=1 -Dusing.aikars.flags=https://mcflags.emc.gs -Daikars.new.flags=true`,
		ramMB, ramMB)
}

// NowMs повертає поточний час як Unix milliseconds.
func NowMs() int64 {
	return time.Now().UnixMilli()
}
