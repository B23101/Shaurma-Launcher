// api.go — Wails API-методи для серверного менеджера.
// Ці методи реєструються в App і викликаються з фронтенду через Wails Binding.
package servers

import (
	"shaurma-launcher-backend/internal/crashguard"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"shaurma-launcher-backend/internal/custompack"
	"shaurma-launcher-backend/internal/java"
	"shaurma-launcher-backend/internal/model"
	"shaurma-launcher-backend/internal/modsource"
)

// API — загальний контекст для серверних API-викликів.
type API struct {
	store    *Store
	manager  *Manager
	dataDir  string
	javaInst *java.Installer
	// backupProgress — колбек подій прогресу бекапів світів (ставить
	// app.go → a.emit("server:backup-progress", …)). Раніше створення бекапу
	// було синхронним чорним ящиком без жодного зворотного зв'язку.
	backupMu       sync.RWMutex
	backupProgress func(serverID string, p BackupProgress)
	// backupLocks — по одному м'ютексу на сервер. Два бекапи ОДНОГО сервера
	// одночасно (напр. таймер і «бекап при зупинці») писали б у той самий
	// файл — ім'я zip має секундну точність, тож колізія імен цілком
	// реальна, а результат — побитий архів замість двох бекапів.
	backupLocksMu sync.Mutex
	backupLocks   map[string]*sync.Mutex
	// netNotify — повідомлення фонових ланцюгів запуску мережі Velocity
	// (network_api.go → app.go → подія "server:network-event").
	netNotifyMu sync.RWMutex
	netNotify   func(proxyID, serverID, message string)
	// versions — той самий провайдер MC-версій/лоадер-версій, що юзає
	// створення збірок (custompack.VersionsProvider): переліки vanilla/
	// fabric/forge/neoforge/quilt для UI створення сервера НЕ дублюються,
	// а Paper/Purpur (яких немає в клієнтському лоадер-переліку) якаються
	// окремо у versions.go цього пакета.
	versions *custompack.VersionsProvider
}

// NewAPI створює серверний API з поточним dataDir та java-інсталером.
func NewAPI(dataDir string, javaInst *java.Installer, versions *custompack.VersionsProvider) *API {
	return &API{
		store:    NewStore(dataDir),
		manager:  NewManager(),
		dataDir:  dataDir,
		javaInst: javaInst,
		versions: versions,
	}
}

// Store повертає store для зовнішнього використання (App).
func (a *API) Store() *Store { return a.store }

// Manager повертає менеджер процесів.
func (a *API) Manager() *Manager { return a.manager }

// ── CRUD ────────────────────────────────────────────────────────────────

// ListServers повертає список всіх серверів зі статусами.
func (a *API) ListServers() ([]ServerView, error) {
	servers := a.store.LoadAll()
	out := make([]ServerView, 0, len(servers))
	for _, srv := range servers {
		view := a.toView(srv)
		out = append(out, view)
	}
	return out, nil
}

// GetServer повертає сервер за ID.
func (a *API) GetServer(id string) (ServerView, error) {
	srv, err := a.store.Get(id)
	if err != nil {
		return ServerView{}, err
	}
	return a.toView(srv), nil
}

// DetectCore аналізує папку за маркерами ядра.
func (a *API) DetectCore(dir string) (CoreDetectResult, error) {
	return DetectCore(dir)
}

// AddServerFromFolder імпортує існуючий сервер з папки.
func (a *API) AddServerFromFolder(dir string, name string, ramMB int, acceptEULA bool) (ServerView, error) {
	// Детекція ядра.
	det, err := DetectCore(dir)
	if err != nil {
		return ServerView{}, err
	}
	if det.Confidence == "low" {
		return ServerView{}, fmt.Errorf("низька впевненість у визначенні ядра: %s", strings.Join(det.Messages, "; "))
	}

	// EULA — лише для серверів Minecraft: Velocity не запускає server.jar
	// Mojang і eula.txt не має.
	if !acceptEULA && det.Core != CoreVelocity {
		eulaPath := filepath.Join(dir, "eula.txt")
		if _, err := os.Stat(eulaPath); os.IsNotExist(err) {
			return ServerView{}, fmt.Errorf("прийміть EULA Minecraft (створіть eula.txt з eula=true)")
		}
		data, _ := os.ReadFile(eulaPath)
		if !strings.Contains(string(data), "eula=true") {
			return ServerView{}, fmt.Errorf("прийміть EULA Minecraft (eula.txt має містити eula=true)")
		}
	}

	if name == "" {
		name = filepath.Base(dir)
	}
	if ramMB == 0 {
		ramMB = RecommendRAMMB(8192, det.Core) // дефолт
	}

	srv := &Server{
		ID:        generateID(),
		Name:      name,
		Dir:       dir,
		Core:      det.Core,
		MCVersion: det.MCVersion,
		LoaderVer: det.LoaderVer,
		JavaMajor: det.JavaMajor,
		RAMMB:     ramMB,
		Port:      det.Port,
		CreatedAt: NowMs(),
	}

	if err := a.store.Save(srv); err != nil {
		return ServerView{}, fmt.Errorf("збереження сервера: %w", err)
	}

	return a.toView(srv), nil
}

// validateCoreCompatible перевіряє, що обране ядро реально підтримує
// вказану версію Minecraft, ЗАПИТУЮЧИ джерело правди (Fabric/Quilt/Forge/
// NeoForge meta API або Paper/Purpur API), а не покладаючись на те, що
// фронтенд правильно відфільтрував список. Порожній список версій ядра
// для цієї MC — ознака несумісності (Fabric ніколи не підтримував 1.8,
// Paper не публікує білди для дуже старих версій тощо).
func (a *API) validateCoreCompatible(core, mcVersion, loaderVer string) error {
	// Статична межа — перше й найнадійніше джерело: якщо лоадер історично
	// не міг існувати для цієї версії MC, відхиляємо без мережі.
	if !minCoreMCVersionOK(core, mcVersion) {
		return fmt.Errorf("%s не підтримує Minecraft %s — оберіть іншу версію гри або інше ядро", CoreLabel(core), mcVersion)
	}
	switch core {
	case CoreFabric, CoreForge, CoreNeoForge, CoreQuilt:
		if a.versions == nil {
			return nil // провайдер недоступний — не блокуємо, install сам провалиться з чіткою помилкою
		}
		list, err := a.versions.GetLoaderVersions(custompack.Loader(core), mcVersion)
		if err != nil {
			return nil // мережева помилка при валідації ≠ несумісність — не блокуємо створення
		}
		if len(list) == 0 {
			return fmt.Errorf("%s не підтримує Minecraft %s — оберіть іншу версію гри або інше ядро", CoreLabel(core), mcVersion)
		}
		if loaderVer != "" {
			found := false
			for _, v := range list {
				if v.Version == loaderVer {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("версія %s ядра %s не існує для Minecraft %s", loaderVer, CoreLabel(core), mcVersion)
			}
		}
		return nil
	case CorePaper:
		list, err := getPaperBuilds(mcVersion)
		if err != nil {
			return nil
		}
		if len(list) == 0 {
			return fmt.Errorf("Paper не має білдів для Minecraft %s — оберіть іншу версію гри", mcVersion)
		}
		return nil
	case CorePurpur:
		list, err := getPurpurVersions(mcVersion)
		if err != nil {
			return nil
		}
		if len(list) == 0 {
			return fmt.Errorf("Purpur не має білдів для Minecraft %s — оберіть іншу версію гри", mcVersion)
		}
		return nil
	default:
		return nil
	}
}

// CreateServer створює новий сервер (встановлює ядро).
func (a *API) CreateServer(req NewServerRequest) (ServerView, error) {
	if req.Name == "" {
		return ServerView{}, fmt.Errorf("назва сервера обов'язкова")
	}
	if req.Core == "" {
		return ServerView{}, fmt.Errorf("ядро сервера обов'язкове")
	}
	if req.MCVersion == "" && req.Core != CoreVelocity {
		return ServerView{}, fmt.Errorf("версія Minecraft обов'язкова")
	}
	if !req.AcceptEULA {
		return ServerView{}, fmt.Errorf("прийміть EULA Minecraft")
	}

	// Перевірка сумісності ядра з версією Minecraft — ОСТАННЯ лінія
	// захисту, незалежна від фронтенду: навіть якщо селектор помилково
	// дозволив обрати несумісну пару (напр. Fabric під MC 1.8, якого
	// Fabric ніколи не підтримував), CreateServer відхилить її тут, а не
	// мовчки створить сервер, що не зможе встановити ядро при старті.
	if req.Core != CoreVanilla && req.Core != CoreVelocity {
		if err := a.validateCoreCompatible(req.Core, req.MCVersion, req.LoaderVer); err != nil {
			return ServerView{}, err
		}
	}

	// Визначаємо Java.
	javaMajor := req.JavaMajor
	if javaMajor == 0 {
		if req.Core == CoreVelocity {
			// Velocity: мінімальна Java залежить від версії проксі (4.x → 25,
			// 3.5.x → 21) — беремо з API; дефолт Java 25 безпечний для обох
			// гілок (25 ≥ будь-якого мінімуму), якщо API недоступний.
			javaMajor = velocityJavaForVersion(req.LoaderVer)
		} else {
			javaMajor = JavaMajorForVersion(req.MCVersion)
		}
	}

	// Визначаємо RAM.
	ramMB := req.RAMMB
	if ramMB == 0 {
		ramMB = RecommendRAMMB(8192, req.Core)
	}

	// Визначаємо порт.
	//
	// ПОРТ — це властивість САМОГО server.properties (єдине джерело правди:
	// туди його пише лаунчер при першому запуску, там його редагує гравець і
	// гра). Окремий порт у лаунчері — лише кеш для списку серверів.
	//
	// Зайнятий порт БІЛЬШЕ НЕ ВАЛИТЬ СТВОРЕННЯ: раніше людина, що тримала
	// запущений сервер на 25565, отримувала «Не вдалося створити сервер:
	// порт 25565 зайнятий: наш сервер «Forge 26.3»» — хоча новий сервер
	// навіть не встановлений, а порт займав ЇЇ Ж власний сервер. Тепер беремо
	// наступний вільний порт, а гравець у будь-який момент міняє його в
	// server.properties (вкладка «Властивості» → server-port).
	port := req.Port
	if port == 0 {
		port = ScanFreePort(25565, a.store.LoadAll(), "", a.manager.IsRunning)
	} else if check := CheckPortFree(port, a.store.LoadAll(), "", a.manager.IsRunning); !check.Free {
		port = ScanFreePort(port+1, a.store.LoadAll(), "", a.manager.IsRunning)
	}

	// Визначаємо папку.
	//
	// ID сервера (ім'я теки) може задати сам гравець — як ID збірки. Якщо
	// ні — виводимо з назви через SanitizeServerFolder (кирилиця й пробіли
	// зберігаються, на відміну від slugify). Однакова тека означала б, що
	// два сервери лежать в одній папці й перетирають світи/конфіги — тому
	// зайнятий ID це помилка, а не мовчазне злиття.
	dir := req.Dir
	if dir == "" {
		folder := strings.TrimSpace(req.ID)
		if folder == "" {
			folder = SanitizeServerFolder(req.Name)
		} else {
			folder = SanitizeServerFolder(folder)
		}
		dir = filepath.Join(a.dataDir, "servers", folder)
		if _, err := os.Stat(filepath.Join(dir, ".shaurma", "server.json")); err == nil {
			return ServerView{}, fmt.Errorf("ID «%s» уже зайнятий — оберіть інший", folder)
		}
	}

	// Створюємо папку.
	if err := os.MkdirAll(dir, 0755); err != nil {
		return ServerView{}, fmt.Errorf("створення папки: %w", err)
	}

	// EULA.
	eulaPath := filepath.Join(dir, "eula.txt")
	if err := os.WriteFile(eulaPath, []byte("eula=true\n"), 0644); err != nil {
		return ServerView{}, fmt.Errorf("запис eula.txt: %w", err)
	}

	srv := &Server{
		ID:          generateID(),
		Name:        req.Name,
		Dir:         dir,
		Core:        req.Core,
		MCVersion:   req.MCVersion,
		LoaderVer:   req.LoaderVer,
		JavaMajor:   javaMajor,
		RAMMB:       ramMB,
		JvmArgs:     req.JvmArgs,
		ProgramArgs: req.ProgramArgs,
		Port:        port,
		CreatedAt:   NowMs(),
	}

	if err := a.store.Save(srv); err != nil {
		return ServerView{}, fmt.Errorf("збереження: %w", err)
	}

	return a.toView(srv), nil
}

// CheckServerFolder перевіряє ID сервера (ім'я теки) до створення: чи він
// коректний після санітизації і чи вільний. Фронтенд кличе це на кожне
// натискання клавіші в полі «ID сервера» — як у діалозі дублювання збірки.
func (a *API) CheckServerFolder(name string) ServerFolderCheck {
	raw := strings.TrimSpace(name)
	res := ServerFolderCheck{Folder: SanitizeServerFolder(raw)}
	if raw == "" {
		return res
	}
	if res.Folder != raw {
		res.Reason = "ID не можна використати як є — заборонені символи прибрано"
	}
	dir := filepath.Join(a.dataDir, "servers", res.Folder)
	if _, err := os.Stat(filepath.Join(dir, ".shaurma", "server.json")); err == nil {
		res.Free = false
		res.Reason = "ID уже зайнятий іншим сервером"
		return res
	}
	res.Free = true
	return res
}

// RenameServerFolder змінює ID сервера = ім'я його теки на диску (як зміна
// ID збірки). Сервер має бути ЗУПИНЕНИЙ: Java тримає відкритими файли
// світу й логів, і rename під час гри зламав би і світ, і процес.
func (a *API) RenameServerFolder(id, newFolder string) (ServerView, error) {
	srv, err := a.store.Get(id)
	if err != nil {
		return ServerView{}, err
	}
	if a.manager.IsRunning(id) {
		return ServerView{}, fmt.Errorf("зупиніть сервер перед зміною ID")
	}
	if strings.TrimSpace(newFolder) == "" {
		return ServerView{}, fmt.Errorf("вкажіть ID сервера")
	}
	folder := SanitizeServerFolder(newFolder)
	if filepath.Base(srv.Dir) == folder {
		return a.toView(srv), nil // нічого не змінилось
	}
	newDir := filepath.Join(filepath.Dir(srv.Dir), folder)
	if _, err := os.Stat(newDir); err == nil {
		return ServerView{}, fmt.Errorf("тека «%s» вже зайнята", folder)
	}
	oldDir := srv.Dir
	if err := os.Rename(oldDir, newDir); err != nil {
		return ServerView{}, fmt.Errorf("перейменування теки: %w", err)
	}
	// Єдине джерело шляху — сам server.json: усе інше (світи, моди,
	// server.properties, бекапи) читається через srv.Dir, тож перезапису
	// абсолютного Dir достатньо.
	srv.Dir = newDir
	if err := a.store.Save(srv); err != nil {
		// Відкат rename, щоб не лишити сервер із битим Dir.
		_ = os.Rename(newDir, oldDir)
		srv.Dir = oldDir
		return ServerView{}, fmt.Errorf("збереження: %w", err)
	}
	return a.toView(srv), nil
}

// DeleteServer видаляє сервер з індексу (файли на диску НЕ чіпаються —
// .shaurma/server.json прибирається, сама тека лишається на місці).
func (a *API) DeleteServer(id string) error {
	// Зупиняємо, якщо запущено.
	if a.manager.IsRunning(id) {
		_ = a.manager.Stop(id, false)
	}
	if err := a.store.Delete(id); err != nil {
		return err
	}
	a.SyncNetworksFor(id) // проксі забуває видалений сервер
	return nil
}

// DeleteServerWithFiles зупиняє сервер, прибирає його з бібліотеки І
// фізично видаляє всю теку сервера (world, mods, логі, конфіги,
// .shaurma/). Викликається з діалогу видалення з галочкою «Видалити
// файли на диску» — дія незворотна, про що попереджає сам діалог.
func (a *API) DeleteServerWithFiles(id string) error {
	srv, err := a.store.Get(id)
	if err != nil {
		return err
	}
	if a.manager.IsRunning(id) {
		_ = a.manager.Stop(id, false)
	}
	if err := a.store.Delete(id); err != nil {
		return err
	}
	a.SyncNetworksFor(id) // проксі забуває видалений сервер
	return os.RemoveAll(srv.Dir)
}

// SaveServerConfig оновлює конфігурацію сервера.
func (a *API) SaveServerConfig(id string, patch ServerConfigPatch) (ServerView, error) {
	srv, err := a.store.Get(id)
	if err != nil {
		return ServerView{}, err
	}

	if patch.Name != nil {
		srv.Name = *patch.Name
	}
	if patch.RAMMB != nil {
		srv.RAMMB = *patch.RAMMB
	}
	if patch.JvmArgs != nil {
		srv.JvmArgs = *patch.JvmArgs
	}
	if patch.ProgramArgs != nil {
		srv.ProgramArgs = *patch.ProgramArgs
	}
	if patch.Port != nil {
		if a.manager.IsRunning(id) {
			return ServerView{}, fmt.Errorf("неможливо змінити порт поки сервер запущено")
		}
		if *patch.Port != srv.Port {
			check := CheckPortFree(*patch.Port, a.store.LoadAll(), id, a.manager.IsRunning)
			if !check.Free {
				return ServerView{}, fmt.Errorf("порт %d зайнятий: %s", *patch.Port, check.Holder)
			}
		}
		srv.Port = *patch.Port
	}
	if patch.Icon != nil {
		srv.Icon = *patch.Icon
		// Явний вибір пресету скасовує лаунчер-PNG (той самий принцип, що
		// в custompack.Update(), але тут БЕЗ бага: код виконується лише
		// коли patch.Icon != nil, тобто користувач реально щось обрав у
		// цьому виклику). Файл може не існувати — помилку ігноруємо.
		if *patch.Icon != "" {
			_ = ResetLauncherIcon(srv.Dir)
			srv.LauncherIconPath = ""
		}
	}
	if patch.Color != nil {
		srv.Color = *patch.Color
	}
	if patch.Notes != nil {
		srv.Notes = *patch.Notes
	}
	if patch.JavaMajor != nil {
		srv.JavaMajor = *patch.JavaMajor
	}
	if patch.AutoBackup != nil {
		cfg := *patch.AutoBackup
		// Саніта: некоректні значення не мають потрапити у файл — інакше
		// таймер бігав би з interval=0 (щосекунди) або зберігав мільйон копій.
		if cfg.IntervalMin < 0 {
			cfg.IntervalMin = 0
		}
		if cfg.IntervalMin > 0 && cfg.IntervalMin < 5 {
			cfg.IntervalMin = 5
		}
		if cfg.KeepCount < 0 {
			cfg.KeepCount = 0
		}
		if cfg.KeepDays < 0 {
			cfg.KeepDays = 0
		}
		if cfg.MaxPerDay < 0 {
			cfg.MaxPerDay = 0
		}
		if cfg.MaxPerDay > 96 {
			cfg.MaxPerDay = 96
		}
		srv.AutoBackup = cfg
	}
	if patch.AutoStartP2P != nil {
		srv.AutoStartP2P = *patch.AutoStartP2P
	}

	if err := a.store.Save(srv); err != nil {
		return ServerView{}, err
	}
	if patch.Port != nil || patch.Name != nil {
		a.SyncNetworksFor(id) // проксі одразу знає новий порт сервера
	}
	return a.toView(srv), nil
}

// ── Зміна ядра ──────────────────────────────────────────────────────────

// coreArtifactFiles — файли, які створює САМЕ ядро (не гравець). Їх
// зносимо при зміні ядра. Жоден із них не є даними гравця: світи, моди,
// плагіни, конфіги, server.properties, eula.txt і .shaurma — НЕ тут.
var coreArtifactFiles = []string{
	"server.jar",
	"fabric-server-launch.jar",
	"fabric-installer.jar",
	"quilt-server-launch.jar",
	"quilt-installer.jar",
	"velocity.jar",
	"launch_config.json",
	"run.bat",
	"run.sh",
	"user_jvm_args.txt",
}

// coreArtifactDirs — теки, які цілком належать ядру (кеш бібліотек і
// завантаженого серверного jar). Їх безпечно видаляти: інсталятор
// завантажить усе заново. Сюди НЕ входять mods/plugins/world/config тощо.
var coreArtifactDirs = []string{
	"libraries",
	"versions",
	"bundler",
	".fabric",
	".quilt",
}

// removeCoreArtifacts прибирає слід старого ядра перед встановленням
// нового. Без цього CoreInstalled() вважав би нове ядро встановленим
// (напр. колишній Paper «server.jar» підійшов би під Forge-перевірку
// fallback-ом) і сервер стартував би зі старим ядром.
func removeCoreArtifacts(dir string) error {
	for _, name := range coreArtifactFiles {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("%s: %w", name, err)
		}
	}

	// Файли з версією в імені: forge-<ver>-installer.jar,
	// neoforge-<ver>-universal.jar, fabric-server-launch.jar (уже вище),
	// paper-*.jar, purpur-*.jar тощо.
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		lower := strings.ToLower(e.Name())
		if !strings.HasSuffix(lower, ".jar") {
			continue
		}
		isCoreJar := false
		for _, p := range []string{"forge-", "neoforge-", "fabric-", "quilt-", "paper-", "purpur-"} {
			if strings.HasPrefix(lower, p) {
				isCoreJar = true
				break
			}
		}
		if !isCoreJar {
			continue
		}
		// fabric/quilt-server-launch.jar — це РОБОЧІ jar'и, їх уже прибрали
		// вище; у масці їх пропускаємо, щоб не плутати з інсталяторами.
		if lower == "fabric-server-launch.jar" || lower == "quilt-server-launch.jar" {
			continue
		}
		_ = os.Remove(filepath.Join(dir, e.Name()))
	}

	for _, name := range coreArtifactDirs {
		if err := os.RemoveAll(filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

// ChangeServerCore замінює ядро (і/або версію гри/лоадера) ВЖЕ створеного
// сервера, НЕ втрачаючи світи, моди, плагіни й конфіги.
//
// Чому не просто «змінити три поля в конфізі»: файли ядра (server.jar,
// libraries/, fabric-server-launch.jar, launch_config.json...) належать
// САМЕ старому ядру. Якщо їх не прибрати, CoreInstalled() вважатиме нове
// ядро встановленим і сервер стартував би зі старим jar під новою назвою.
// Тому: сервер має бути зупинений → перевіряємо сумісність → зносимо
// старий core-footprint → пишемо нові Core/MCVersion/LoaderVer/JavaMajor →
// одразу встановлюємо нове ядро (з тим самим прогресом, що при старті),
// щоб «перемкнув і забув», а не довантаження аж при наступному запуску.
func (a *API) ChangeServerCore(id string, req ChangeCoreRequest) (ServerView, error) {
	srv, err := a.store.Get(id)
	if err != nil {
		return ServerView{}, err
	}
	if a.manager.IsRunning(id) {
		return ServerView{}, fmt.Errorf("зупиніть сервер перед зміною ядра")
	}

	core := strings.TrimSpace(req.Core)
	if core == "" {
		core = srv.Core
	}
	if _, ok := coreInstallers[core]; !ok {
		return ServerView{}, fmt.Errorf("невідоме ядро: %s", core)
	}

	mcVersion := strings.TrimSpace(req.MCVersion)
	if mcVersion == "" {
		mcVersion = srv.MCVersion
	}
	if mcVersion == "" && core != CoreVelocity {
		return ServerView{}, fmt.Errorf("вкажіть версію Minecraft")
	}

	loaderVer := strings.TrimSpace(req.LoaderVer)
	// Порожній LoaderVer = «рекомендована» лише коли ядро/версія РЕАЛЬНО
	// змінюються; якщо все те саме — лишаємо наявну версію лоадера, щоб не
	// переустановлювати те саме.
	if loaderVer == "" && core == srv.Core && mcVersion == srv.MCVersion {
		loaderVer = srv.LoaderVer
	}

	if core == srv.Core && mcVersion == srv.MCVersion && loaderVer == srv.LoaderVer {
		return a.toView(srv), nil // нічого не змінилось — не чіпаємо файли
	}

	// Сумісність — та сама перевірка, що при створенні сервера.
	if core != CoreVanilla && core != CoreVelocity {
		if err := a.validateCoreCompatible(core, mcVersion, loaderVer); err != nil {
			return ServerView{}, err
		}
	}

	// Java: явна з запиту, інакше авто за версією гри (як CreateServer).
	javaMajor := req.JavaMajor
	if javaMajor == 0 {
		if core == CoreVelocity {
			javaMajor = velocityJavaForVersion(loaderVer)
		} else {
			javaMajor = JavaMajorForVersion(mcVersion)
		}
	}

	// Зносимо старий core-footprint ПЕРЕД записом конфіга: якщо видалення
	// впаде — сервер лишиться зі СТАРИМ робочим ядром, а не в напівстані.
	if err := removeCoreArtifacts(srv.Dir); err != nil {
		return ServerView{}, fmt.Errorf("прибирання старого ядра: %w", err)
	}

	srv.Core = core
	srv.MCVersion = mcVersion
	srv.LoaderVer = loaderVer
	srv.JavaMajor = javaMajor
	if err := a.store.Save(srv); err != nil {
		return ServerView{}, fmt.Errorf("збереження: %w", err)
	}

	// Java потрібного мажора (та сама послідовність, що prepareAndStart,
	// але без самого запуску сервера).
	var javaPath string
	if a.javaInst != nil {
		a.manager.NotifyInstallStatus(id, "installing")
		a.manager.NotifyInstallProgress(id, ServerInstallProgress{Stage: "java", Percent: -1, Message: fmt.Sprintf("Перевірка Java %d...", srv.JavaMajor)})
		prevHandler := a.javaInst.ProgressHandler()
		a.javaInst.SetProgressHandler(func(p java.Progress) {
			a.manager.NotifyInstallProgress(id, ServerInstallProgress{Stage: "java", Percent: p.Percent, Message: p.Message})
		})
		path, javaErr := a.javaInst.EnsureMajorWithComponent(srv.JavaMajor, "")
		a.javaInst.SetProgressHandler(prevHandler)
		InvalidateJavaPathCache()
		if javaErr != nil {
			a.manager.NotifyInstallStatus(id, "error")
			a.manager.NotifyInstallProgress(id, ServerInstallProgress{Stage: "java", Percent: -1, Message: "Помилка: " + javaErr.Error()})
			return a.toView(srv), fmt.Errorf("Java %d не вдалося встановити: %w", srv.JavaMajor, javaErr)
		}
		javaPath = path
	} else {
		javaPath, _ = FindJavaPath(srv.JavaMajor)
	}

	a.manager.NotifyInstallStatus(id, "installing")
	if err := InstallCore(srv, javaPath, func(p ServerInstallProgress) {
		a.manager.NotifyInstallProgress(id, p)
	}); err != nil {
		a.manager.NotifyInstallStatus(id, "error")
		a.manager.NotifyInstallProgress(id, ServerInstallProgress{Stage: "core", Percent: -1, Message: "Помилка: " + err.Error()})
		return a.toView(srv), fmt.Errorf("встановлення ядра: %w", err)
	}

	// InstallCore міг сам визначити фактичну версію лоадера (порожній
	// LoaderVer → «рекомендована») — зберігаємо її назад у конфіг.
	if err := a.store.Save(srv); err != nil {
		return ServerView{}, fmt.Errorf("збереження: %w", err)
	}
	a.manager.NotifyInstallStatus(id, "stopped")
	return a.toView(srv), nil
}

// ── Процеси ─────────────────────────────────────────────────────────────

// StartServer запускає сервер.
func (a *API) StartServer(id string) error {
	srv, err := a.store.Get(id)
	if err != nil {
		return err
	}

	if a.manager.IsRunning(id) {
		return fmt.Errorf("сервер вже запущено")
	}

	// Порт беремо з server.properties (джерело правди), а не з кешу в
	// store: гравець міг змінити server-port файлом або самою грою після
	// останнього збереження лаунчером.
	port := a.effectiveServerPort(srv)
	// excludeServerID=srv.ID — сервер не повинен блокувати сам себе своїм же
	// портом, коли він ще не запущений.
	portCheck := CheckPortFree(port, a.store.LoadAll(), srv.ID, a.manager.IsRunning)
	if !portCheck.Free {
		return fmt.Errorf(
			"порт %d зайнятий: %s. Змініть server-port у server.properties (вкладка «Властивості») або зупиніть «%s»",
			port, portCheck.Holder, portCheck.Holder)
	}

	// Перевірка RAM.
	allRunning := a.getUsedRAM()
	ramCheck := CheckRAM(srv.RAMMB, allRunning)
	if !ramCheck.Ok {
		return fmt.Errorf("недостатньо пам'яті: потрібно %d МБ, доступно %d МБ", srv.RAMMB, ramCheck.Available)
	}

	// Знаходимо/встановлюємо Java → ліниво ставимо ядро → запускаємо.
	// (Спільний шлях з NetworkStart — кожен сервер мережі стартує через
	// той самий prepareAndStart, щоб логіка не дублювалась.)
	// Проксі стартує зі свіжим конфігом мережі (порти й імена серверів
	// могли змінитись, поки він стояв).
	if srv.Core == CoreVelocity {
		if net, err := LoadNetwork(srv.Dir); err == nil {
			if err := a.applyNetwork(srv, net); err != nil {
				log.Printf("[network] %s: %v", srv.Name, err)
			}
		}
	}
	if err := a.prepareAndStart(srv); err != nil {
		return err
	}
	// Ланцюги мережі Velocity: проксі піднімає свої сервери, сервер із
	// «запускає мережу» — проксі (network_api.go).
	a.afterUserStart(srv)
	return nil
}

// effectiveServerPort повертає АКТУАЛЬНИЙ порт сервера: server-port із
// server.properties, якщо файл існує (це джерело правди — туди пише і
// лаунчер, і сама гра), інакше — збережений у store srv.Port (сервер ще
// жодного разу не запускався, тому файлу немає; у проксі Velocity замість
// server.properties свій velocity.toml — там лишається srv.Port).
//
// Знайдене значення синхронізуємо в store, щоб список серверів і UI
// показували те саме, що реально стоїть у файлі.
//
// Свідомо НЕ викликається з ListServers/toView: читання файлів на кожен
// сервер у списку — саме те, через що список вантажився довго.
func (a *API) effectiveServerPort(srv *Server) int {
	path := filepath.Join(srv.Dir, "server.properties")
	if _, err := os.Stat(path); err != nil {
		return srv.Port
	}
	props := parsePropertiesFile(path)
	raw, ok := props["server-port"]
	if !ok {
		return srv.Port
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return srv.Port
	}
	if port != srv.Port {
		srv.Port = port
		_ = a.store.Save(srv)
	}
	return port
}

// prepareAndStart готує і запускає ОДИН сервер: встановлює Java потрібного
// мажора через java.Installer (з прогресами в "server:install-progress"),
// ліниво доставляє ядро (якщо його ще немає на диску — InstallCore) і
// запускає java через manager. Використовується і StartServer, і
// NetworkStart (мережа Velocity: проксі та кожен backend-сервер).
func (a *API) prepareAndStart(srv *Server) error {
	id := srv.ID
	// «Єдине джерело» модів/плагінів: перед запуском звіряємо хеші джерел із
	// mods/plugins і копіюємо змінене (прогрес — server:install-progress зі
	// stage "mod_sources"). Збої не блокують старт — лише попередження в лог.
	if len(a.SyncServerSources(srv)) > 0 {
		log.Printf("[servers] %s: синхронізація джерел модів завершилась із попередженнями", srv.Name)
	}
	var javaPath string
	if a.javaInst != nil {
		a.manager.NotifyInstallStatus(id, "installing")
		a.manager.NotifyInstallProgress(id, ServerInstallProgress{Stage: "java", Percent: -1, Message: fmt.Sprintf("Перевірка Java %d...", srv.JavaMajor)})
		prevHandler := a.javaInst.ProgressHandler()
		a.javaInst.SetProgressHandler(func(p java.Progress) {
			a.manager.NotifyInstallProgress(id, ServerInstallProgress{
				Stage:   "java",
				Percent: p.Percent,
				Message: p.Message,
			})
		})
		path, javaErr := a.javaInst.EnsureMajorWithComponent(srv.JavaMajor, "")
		a.javaInst.SetProgressHandler(prevHandler)
		// Java могла щойно з'явитись/оновитись — скидаємо кеш FindJavaPath,
		// щоб список серверів одразу показував javaInstalled=true, а не чекав
		// докинутий TTL.
		InvalidateJavaPathCache()
		if javaErr != nil {
			a.manager.NotifyInstallStatus(id, "error")
			a.manager.NotifyInstallProgress(id, ServerInstallProgress{Stage: "java", Percent: -1, Message: "Помилка: " + javaErr.Error()})
			return fmt.Errorf("Java %d не вдалося встановити: %w", srv.JavaMajor, javaErr)
		}
		javaPath = path
	} else {
		// Fallback якщо javaInst не ініціалізовано (не повинно траплятись).
		javaPath, _ = FindJavaPath(srv.JavaMajor)
		if javaPath == "" {
			return fmt.Errorf("Java %d не знайдена", srv.JavaMajor)
		}
	}

	// Встановлення ядра — ЛІНИВЕ: CreateServer зберіг конфіг, але не качав
	// нічого. Якщо ядра немає на диску — качаємо зараз, синхронно.
	if !CoreInstalled(srv) {
		a.manager.NotifyInstallStatus(id, "installing")
		if err := InstallCore(srv, javaPath, func(p ServerInstallProgress) {
			a.manager.NotifyInstallProgress(id, p)
		}); err != nil {
			a.manager.NotifyInstallStatus(id, "error")
			a.manager.NotifyInstallProgress(id, ServerInstallProgress{Stage: "core", Percent: -1, Message: "Помилка: " + err.Error()})
			return fmt.Errorf("встановлення ядра: %w", err)
		}
		// installFabricCore/installForgeCore тощо можуть визначити
		// LoaderVer автоматично (порожній → "рекомендована") — зберігаємо
		// фактично встановлену версію назад у конфіг, щоб перезапуск не
		// тригерив повторне встановлення і щоб UI показував реальну версію.
		if err := a.store.Save(srv); err != nil {
			log.Printf("[servers] не вдалося зберегти LoaderVer після встановлення: %v", err)
		}
	}

	startErr := a.manager.Start(srv, javaPath)
	if startErr == nil {
		// Авто-бекапи світів: фоновий цикл читає AutoBackup з диска, тож
		// гравець може увімкнути/налаштувати їх просто під час роботи.
		a.scheduleAutoBackup(id)
		// Бекап ПРИ СТАРТІ (якщо увімкнено у вкладці «Світи») — знімок стану
		// світу до того, як сервер почне його змінювати. У фоні: пакування
		// великого світу не має затримувати відповідь StartServer.
		if cfg := srv.AutoBackup; cfg.Enabled && cfg.BackupOnStart {
			{
				_f, _a0, _a1 := a.runAutoBackups, srv, cfg
				go func() {
					defer crashguard.Recover("a.runAutoBackups")
					_f(_a0, _a1)
				}()
			}
		}
	}
	return startErr
}

// StopServer зупиняє сервер.
//
// Після УСПІШНОЇ звичайної зупинки (не Kill) робимо бекап світів, якщо
// гравець увімкнув «бекап при зупинці»: саме ЦЕЙ знімок консистентний
// (Java вже закрила регіони й дописала level.dat), на відміну від бекапу
// під час роботи. Kill цю гілку не тригерить — примусове вбивство процесу
// може лишити світ у недописаному стані, і мовчки знімати таку копію
// означало б плодити бекапи, які неможливо відновити.
func (a *API) StopServer(id string, graceful bool) error {
	if graceful {
		if srv, ok := a.store.GetNoErr(id); ok {
			a.beforeUserStop(srv) // проксі спершу зупиняє свою мережу
		}
	}
	err := a.manager.Stop(id, graceful)
	if err != nil || !graceful {
		return err
	}
	srv, ok := a.store.GetNoErr(id)
	if !ok {
		return nil
	}
	cfg := srv.AutoBackup
	if cfg.Enabled && cfg.BackupOnStop {
		{
			_f, _a0, _a1 := a.runAutoBackups, srv, cfg
			go func() {
				defer crashguard.Recover("a.runAutoBackups")
				_f(_a0, _a1)
			}()
		}
	}
	return nil
}

// SendCommand надсилає команду в консоль сервера.
func (a *API) SendCommand(id string, cmd string) error {
	return a.manager.SendCommand(id, cmd)
}

// ServerStatus повертає статус сервера.
func (a *API) ServerStatus(id string) ServerStatus {
	return a.manager.GetStatus(id)
}

// ── Діагностика ─────────────────────────────────────────────────────────

// CheckPortFreeAPI перевіряє порт (для UI — майстра створення й налаштувань,
// коли ще немає конкретного serverID, з яким порт асоційований).
func (a *API) CheckPortFreeAPI(port int) CheckPortResult {
	return CheckPortFree(port, a.store.LoadAll(), "", a.manager.IsRunning)
}

// CheckPortFreeForServer перевіряє порт, виключаючи власний сервер
// (щоб редагування налаштувань не показувало «зайнято» через себе ж).
func (a *API) CheckPortFreeForServer(serverID string, port int) CheckPortResult {
	return CheckPortFree(port, a.store.LoadAll(), serverID, a.manager.IsRunning)
}

// ScanFreePortAPI шукає вільний порт.
func (a *API) ScanFreePortAPI(preferred int) int {
	return ScanFreePort(preferred, a.store.LoadAll(), "", a.manager.IsRunning)
}

// CheckRAMAPI перевіряє RAM.
func (a *API) CheckRAMAPI(ramMB int) CheckRAMResult {
	return CheckRAM(ramMB, a.getUsedRAM())
}

// ── server.properties ───────────────────────────────────────────────────

// GetServerProperties читає server.properties.
func (a *API) GetServerProperties(id string) (PropertiesDoc, error) {
	srv, err := a.store.Get(id)
	if err != nil {
		return PropertiesDoc{}, err
	}
	// Версія гри потрібна, щоб позначити ключі, які в ній уже не діють (pvp →
	// gamerule з 1.21.9 тощо), і підставити правильні default'и.
	return ReadServerPropertiesForVersion(srv.Dir, srv.MCVersion)
}

// SaveServerProperties зберігає зміни server.properties.
func (a *API) SaveServerProperties(id string, updates []PropUpdate) error {
	srv, err := a.store.Get(id)
	if err != nil {
		return err
	}

	// Спершу пишемо файл: WriteServerProperties валідує значення (діапазони
	// портів тощо) і може відхилити виклик. Раніше Server.Port у сторі
	// оновлювався ДО цього, тож відхилене збереження все одно міняло порт.
	if err := WriteServerPropertiesForVersion(srv.Dir, srv.MCVersion, updates); err != nil {
		return err
	}

	// Порт у сторі — кеш server-port з файлу.
	for _, u := range updates {
		if u.Key == "server-port" {
			var p int
			n, _ := fmt.Sscanf(u.Value, "%d", &p)
			if n == 1 && p > 0 {
				srv.Port = p
				_ = a.store.Save(srv)
				a.SyncNetworksFor(id) // проксі одразу знає новий порт сервера
			}
		}
	}
	return nil
}

// ── Іконки ──────────────────────────────────────────────────────────────

// SetServerIconAPI встановлює іконку сервера.
func (a *API) SetServerIconAPI(id string, imageDataBase64 string) (string, error) {
	srv, err := a.store.Get(id)
	if err != nil {
		return "", err
	}
	return SetServerIcon(srv.Dir, imageDataBase64)
}

// GetServerIconAPI отримує іконку сервера.
func (a *API) GetServerIconAPI(id string) (string, error) {
	srv, err := a.store.Get(id)
	if err != nil {
		return "", err
	}
	return GetServerIcon(srv.Dir)
}

// ResetServerIconAPI видаляє server-icon.png (сервер повертається до
// стандартної іконки Mojang / іконки ядра).
func (a *API) ResetServerIconAPI(id string) error {
	srv, err := a.store.Get(id)
	if err != nil {
		return err
	}
	return ResetServerIcon(srv.Dir)
}

// SetLauncherIconAPI встановлює іконку ЛАУНЧЕРА (картка/сайдбар/списки в
// UI) з локального файлу — окремо від протокольної server-icon.png.
// Файл копіюється як є (без ресайзу), шлях фіксується в конфізі сервера.
func (a *API) SetLauncherIconAPI(id, srcPath string) (ServerView, error) {
	srv, err := a.store.Get(id)
	if err != nil {
		return ServerView{}, err
	}
	rel, err := SetLauncherIcon(srv.Dir, srcPath)
	if err != nil {
		return ServerView{}, err
	}
	srv.LauncherIconPath = rel
	// PNG лаунчера має пріоритет над пресетом — чистимо пресет, щоб у UI не
	// було двох «джерел правди» (той самий принцип, що при виборі PNG у збірці).
	srv.Icon = ""
	if err := a.store.Save(srv); err != nil {
		return ServerView{}, err
	}
	return a.toView(srv), nil
}

// GetLauncherIconAPI отримує іконку лаунчера як data URL.
func (a *API) GetLauncherIconAPI(id string) (string, error) {
	srv, err := a.store.Get(id)
	if err != nil {
		return "", err
	}
	return GetLauncherIcon(srv.Dir, srv.LauncherIconPath)
}

// ResetLauncherIconAPI видаляє іконку лаунчера (пресет/іконка ядра
// повертаються як fallback у UI).
func (a *API) ResetLauncherIconAPI(id string) (ServerView, error) {
	srv, err := a.store.Get(id)
	if err != nil {
		return ServerView{}, err
	}
	_ = ResetLauncherIcon(srv.Dir)
	srv.LauncherIconPath = ""
	if err := a.store.Save(srv); err != nil {
		return ServerView{}, err
	}
	return a.toView(srv), nil
}

// ── Live-статистика ─────────────────────────────────────────────────────

// GetServerStats повертає live-статистику сервера (RAM, CPU, гравці, uptime).
func (a *API) GetServerStats(id string) LiveStats {
	return GetStats(id)
}

// HasRunningServers повертає true, якщо хоча б один сервер запущено.
func (a *API) HasRunningServers() bool {
	a.manager.mu.RLock()
	defer a.manager.mu.RUnlock()
	return len(a.manager.running) > 0
}

// StopAllServers зупиняє всі запущені сервери (graceful).
func (a *API) StopAllServers() {
	a.manager.StopAll()
}

// ── Допоміжні ───────────────────────────────────────────────────────────

func (a *API) toView(srv *Server) ServerView {
	status := a.manager.GetStatus(srv.ID)
	iconData, _ := GetServerIcon(srv.Dir)
	launcherIcon, _ := GetLauncherIcon(srv.Dir, srv.LauncherIconPath)
	javaPath, _ := FindJavaPath(srv.JavaMajor)
	javaInstalled := javaPath != ""

	return ServerView{
		ID:               srv.ID,
		Name:             srv.Name,
		Dir:              srv.Dir,
		Core:             srv.Core,
		CoreLabel:        CoreLabel(srv.Core),
		MCVersion:        srv.MCVersion,
		LoaderVer:        srv.LoaderVer,
		JavaMajor:        srv.JavaMajor,
		JavaInstalled:    javaInstalled,
		CoreInstalled:    CoreInstalled(srv),
		RAMMB:            srv.RAMMB,
		Port:             srv.Port,
		JvmArgs:          srv.JvmArgs,
		ProgramArgs:      srv.ProgramArgs,
		Notes:            srv.Notes,
		Status:           status.Status,
		PID:              status.PID,
		UptimeSec:        status.UptimeSec,
		ExitCode:         status.ExitCode,
		Recovered:        status.Recovered,
		IconData:         iconData,
		Icon:             srv.Icon,
		LauncherIconData: launcherIcon,
		Color:            srv.Color,
		NetworkID:        srv.NetworkID,
		NetworkRole:      srv.NetworkRole,
		HasWorlds:        HasWorlds(srv.Core),
		HasMods:          HasMods(srv.Core),
		HasPlugins:       HasPlugins(srv.Core),
		LastError:        status.LastError,
		CreatedAt:        srv.CreatedAt,
		AutoBackup:       srv.AutoBackup,
		AutoStartP2P:     srv.AutoStartP2P,
	}
}

func (a *API) getUsedRAM() int {
	servers := a.store.LoadAll()
	total := 0
	for id, srv := range servers {
		if a.manager.IsRunning(id) {
			total += srv.RAMMB
		}
	}
	return total
}

func generateID() string {
	b := make([]byte, 16)
	// Генеруємо випадковий ID.
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	var buf []byte
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			buf = append(buf, byte(c))
		}
	}
	if len(buf) == 0 {
		return "server"
	}
	return string(buf)
}

// ── Світи + бекапи ──────────────────────────────────────────────────────

// SetBackupProgressCallback встановлює отримувача подій прогресу бекапів.
func (a *API) SetBackupProgressCallback(fn func(serverID string, p BackupProgress)) {
	a.backupMu.Lock()
	a.backupProgress = fn
	a.backupMu.Unlock()
}

func (a *API) notifyBackupProgress(serverID string, p BackupProgress) {
	a.backupMu.RLock()
	fn := a.backupProgress
	a.backupMu.RUnlock()
	if fn != nil {
		fn(serverID, p)
	}
}

// backupLock повертає м'ютекс сервера для створення бекапів (див. backupLocks).
func (a *API) backupLock(serverID string) *sync.Mutex {
	a.backupLocksMu.Lock()
	defer a.backupLocksMu.Unlock()
	if a.backupLocks == nil {
		a.backupLocks = make(map[string]*sync.Mutex)
	}
	mu, ok := a.backupLocks[serverID]
	if !ok {
		mu = &sync.Mutex{}
		a.backupLocks[serverID] = mu
	}
	return mu
}

func (a *API) ListWorlds(serverID string) ([]WorldInfo, error) {
	srv, err := a.store.Get(serverID)
	if err != nil {
		return nil, err
	}
	return ListWorlds(srv.Dir), nil
}

// CreateWorldBackup — ручний бекап світу (кнопка у вкладці «Світи»).
// Звітує прогресс через "server:backup-progress", тож гравець бачить
// прогрес-бар, а не «нічого не відбувається» на великому світі.
func (a *API) CreateWorldBackup(serverID, worldName string) (string, error) {
	srv, err := a.store.Get(serverID)
	if err != nil {
		return "", err
	}
	// Один бекап на сервер за раз (див. backupLocks): ручна кнопка не має
	// зіштовхуватись із авто-бекапом на тому самому світі.
	mu := a.backupLock(serverID)
	mu.Lock()
	defer mu.Unlock()
	return CreateBackupWithProgress(srv.Dir, worldName, "", false, func(p BackupProgress) {
		a.notifyBackupProgress(serverID, p)
	})
}

func (a *API) ListWorldBackups(serverID, worldName string) ([]BackupInfo, error) {
	srv, err := a.store.Get(serverID)
	if err != nil {
		return nil, err
	}
	return ListBackups(srv.Dir, worldName, ""), nil
}

// RestoreWorldBackup відновлює світ з бекапу. Сервер МУСИТЬ бути
// зупинений: Java тримає відкритими файли регіонів, і підміна теки під
// живим процесом ламає і світ, і сервер. UI цю кнопку й так вимикає, але
// перевірка тут — остання лінія захисту (виклик міг прийти з-поза UI).
func (a *API) RestoreWorldBackup(serverID, worldName, backupID string) error {
	srv, err := a.store.Get(serverID)
	if err != nil {
		return err
	}
	if a.manager.IsRunning(serverID) {
		return fmt.Errorf("зупиніть сервер перед відновленням світу з бекапу")
	}
	return RestoreBackup(srv.Dir, worldName, "", backupID)
}

// DeleteWorldBackup видаляє бекап. ВАЖЛИВО: тека береться з ТЕКИ СЕРВЕРА
// (<serverDir>/.shaurma/backups), а не з глобальної теки даних лаунчера —
// раніше тут був саме останній варіант, і «видалений» бекап лишався на диску.
func (a *API) DeleteWorldBackup(serverID, worldName, backupID string) error {
	srv, err := a.store.Get(serverID)
	if err != nil {
		return err
	}
	return DeleteBackupForServer(srv.Dir, backupID)
}

// OnlinePlayers — хто зараз на сервері (нік + час входу). Реєстр веде
// internal/servers/players.go з рядків логу java.
func (a *API) OnlinePlayers(serverID string) []PlayerPresence {
	return ListOnlinePlayers(serverID)
}

// AddServerModFile зберігає jar-файл (base64 від input[type=file] UI) у
// теку mods/ або plugins/ сервера. kind: "mods" | "plugins".
func (a *API) AddServerModFile(serverID, kind, fileName, dataBase64 string) error {
	if kind != "mods" && kind != "plugins" {
		return fmt.Errorf("невідомий тип контенту: %s (очікували mods|plugins)", kind)
	}
	srv, err := a.store.Get(serverID)
	if err != nil {
		return err
	}
	if err := serverContentLockedErr(srv, kind, ""); err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Ext(fileName), ".jar") {
		return fmt.Errorf("дозволені лише .jar файли")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(dataBase64, "data:application/octet-stream;base64,"))
	if err != nil {
		// Можливо прийшов data URL — пробуємо розібрати після коми.
		if idx := strings.Index(dataBase64, ","); idx > 0 {
			raw, err = base64.StdEncoding.DecodeString(dataBase64[idx+1:])
		}
	}
	if err != nil {
		return fmt.Errorf("декодування файлу: %w", err)
	}
	if len(raw) == 0 {
		return fmt.Errorf("файл порожній")
	}

	dir := filepath.Join(srv.Dir, kind)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("створення теки %s: %w", kind, err)
	}
	safe := filepath.Base(fileName)
	if safe == "." || safe == string(filepath.Separator) || safe == "" {
		return fmt.Errorf("некоректне ім'я файлу")
	}
	return os.WriteFile(filepath.Join(dir, safe), raw, 0644)
}

// ── Моди сервера ──────────────────────────────────────────────────────

func (a *API) ListServerMods(serverID string) ([]model.ModEntry, error) {
	srv, err := a.store.Get(serverID)
	if err != nil {
		return nil, err
	}
	mods := ListServerMods(srv.Dir)
	annotateModEntries(sourceStoreFor(srv, modsource.NameMods), mods)
	return mods, nil
}

func (a *API) ToggleServerMod(serverID, fileName string) error {
	srv, err := a.store.Get(serverID)
	if err != nil {
		return err
	}
	if err := serverContentLockedErr(srv, modsource.NameMods, fileName); err != nil {
		return err
	}
	return ToggleServerMod(srv.Dir, fileName)
}

func (a *API) DeleteServerMod(serverID, fileName string) error {
	srv, err := a.store.Get(serverID)
	if err != nil {
		return err
	}
	if err := serverContentLockedErr(srv, modsource.NameMods, fileName); err != nil {
		return err
	}
	return DeleteServerMod(srv.Dir, fileName)
}

// ── Плагіни сервера ──────────────────────────────────────────────────

func (a *API) ListServerPlugins(serverID string) ([]ServerPluginInfo, error) {
	srv, err := a.store.Get(serverID)
	if err != nil {
		return nil, err
	}
	plugins := ListServerPlugins(srv.Dir)
	annotatePluginInfos(sourceStoreFor(srv, modsource.NamePlugins), plugins)
	return plugins, nil
}

func (a *API) ToggleServerPlugin(serverID, fileName string) error {
	srv, err := a.store.Get(serverID)
	if err != nil {
		return err
	}
	if err := serverContentLockedErr(srv, modsource.NamePlugins, fileName); err != nil {
		return err
	}
	return ToggleServerPlugin(srv.Dir, fileName)
}

func (a *API) DeleteServerPlugin(serverID, fileName string) error {
	srv, err := a.store.Get(serverID)
	if err != nil {
		return err
	}
	if err := serverContentLockedErr(srv, modsource.NamePlugins, fileName); err != nil {
		return err
	}
	return DeleteServerPlugin(srv.Dir, fileName)
}

// ── ШІ-аналіз крашів ──────────────────────────────────────────────────

func (a *API) AnalyzeServerCrash(serverID string) (CrashAnalysis, error) {
	srv, err := a.store.Get(serverID)
	if err != nil {
		return CrashAnalysis{}, err
	}
	return AnalyzeServerCrash(srv.Dir), nil
}

func (a *API) HasCrashReport(serverID string) bool {
	srv, err := a.store.Get(serverID)
	if err != nil {
		return false
	}
	return HasCrashReport(srv.Dir)
}


// ── Зв'язок зі збірками ───────────────────────────────────────────────

func (a *API) GetPackServerLinks(packID string) []PackServerLink {
	store := NewPackLinkStore(a.store.dataDir)
	return store.GetLinksForPack(packID)
}

// GetServerPackLinks — збірки, з якими запускається сервер.
func (a *API) GetServerPackLinks(serverID string) []PackServerLink {
	store := NewPackLinkStore(a.store.dataDir)
	return store.GetLinksForServer(serverID)
}

// SavePackServerLink додає або оновлює один зв'язок збірка ↔ сервер.
func (a *API) SavePackServerLink(link PackServerLink) error {
	if link.PackID == "" || link.ServerID == "" {
		return fmt.Errorf("оберіть збірку і сервер")
	}
	srv, err := a.store.Get(link.ServerID)
	if err != nil {
		return err
	}
	link.ServerName = srv.Name
	link.ServerAddr = FormatServerAddress(srv)
	return NewPackLinkStore(a.store.dataDir).AddLink(link)
}

// RemovePackServerLink прибирає зв'язок збірка ↔ сервер.
func (a *API) RemovePackServerLink(packID, serverID string) error {
	return NewPackLinkStore(a.store.dataDir).RemoveLink(packID, serverID)
}

func (a *API) SetPackServerLinks(packID string, links []PackServerLink) error {
	store := NewPackLinkStore(a.store.dataDir)
	// Видаляємо старі
	for _, l := range store.GetLinksForPack(packID) {
		store.RemoveLink(packID, l.ServerID)
	}
	// Додаємо нові
	for _, l := range links {
		l.PackID = packID
		store.AddLink(l)
	}
	return nil
}
