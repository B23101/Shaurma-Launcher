// servers_api.go — методи App для серверного менеджера (викликаються з фронтенду).
package main

import (
	"strings"

	"shaurma-launcher-backend/internal/api"
	"shaurma-launcher-backend/internal/custompack"
	"shaurma-launcher-backend/internal/model"
	"shaurma-launcher-backend/internal/servers"
)

// ── Список / отримання ──────────────────────────────────────────────────

func (a *App) ListServers() ([]servers.ServerView, error) {
	return a.srvAPI.ListServers()
}

func (a *App) GetServer(id string) (servers.ServerView, error) {
	return a.srvAPI.GetServer(id)
}

func (a *App) DetectServerCore(dir string) (servers.CoreDetectResult, error) {
	return a.srvAPI.DetectCore(dir)
}

// ── Версії для селектора "Новий сервер" ──────────────────────────────

func (a *App) GetServerMCVersions() ([]custompack.MCVersionEntry, error) {
	return a.srvAPI.GetServerMCVersions()
}

func (a *App) GetServerDefaultMCVersion() (string, error) {
	return a.srvAPI.GetServerDefaultMCVersion()
}

func (a *App) GetServerCoreVersions(core, mcVersion string) ([]servers.ServerCoreVersionEntry, error) {
	return a.srvAPI.GetServerCoreVersions(core, mcVersion)
}

func (a *App) GetServerAvailableCores(mcVersion string) []string {
	return a.srvAPI.GetServerAvailableCores(mcVersion)
}

// ── CRUD ────────────────────────────────────────────────────────────────

func (a *App) AddServerFromFolder(dir, name string, ramMB int, acceptEULA bool) (servers.ServerView, error) {
	return a.srvAPI.AddServerFromFolder(dir, name, ramMB, acceptEULA)
}

func (a *App) CreateServer(req servers.NewServerRequest) (servers.ServerView, error) {
	return a.srvAPI.CreateServer(req)
}

// CheckServerFolder — вільний/коректний ID сервера (ім'я теки) до створення.
func (a *App) CheckServerFolder(name string) servers.ServerFolderCheck {
	return a.srvAPI.CheckServerFolder(name)
}

// RenameServerFolder — змінити ID сервера (ім'я теки).
func (a *App) RenameServerFolder(id, newFolder string) (servers.ServerView, error) {
	return a.srvAPI.RenameServerFolder(id, newFolder)
}

func (a *App) DeleteServer(id string) error {
	return a.srvAPI.DeleteServer(id)
}

// DeleteServerWithFiles — те саме, що DeleteServer, але з фізичним
// видаленням теки сервера на диску (незворотно).
func (a *App) DeleteServerWithFiles(id string) error {
	return a.srvAPI.DeleteServerWithFiles(id)
}

func (a *App) SaveServerConfig(id string, patch servers.ServerConfigPatch) (servers.ServerView, error) {
	return a.srvAPI.SaveServerConfig(id, patch)
}

// ChangeServerCore — змінити ядро/версію гри/лоадера існуючого сервера
// (світи, моди, плагіни й конфіги зберігаються). Сервер має бути зупинений.
func (a *App) ChangeServerCore(id string, req servers.ChangeCoreRequest) (servers.ServerView, error) {
	return a.srvAPI.ChangeServerCore(id, req)
}

// ── Процеси ─────────────────────────────────────────────────────────────

func (a *App) StartServer(id string) error {
	if err := a.shareLockErr("server", id); err != nil {
		return err
	}
	return a.srvAPI.StartServer(id)
}

func (a *App) StopServer(id string, graceful bool) error {
	return a.srvAPI.StopServer(id, graceful)
}

func (a *App) SendServerCommand(id, cmd string) error {
	return a.srvAPI.SendCommand(id, cmd)
}

func (a *App) GetServerStatus(id string) servers.ServerStatus {
	return a.srvAPI.ServerStatus(id)
}

// ── Діагностика ─────────────────────────────────────────────────────────

func (a *App) CheckServerPort(port int) servers.CheckPortResult {
	return a.srvAPI.CheckPortFreeAPI(port)
}

// CheckServerPortForServer перевіряє порт, ігноруючи власний порт сервера
// serverID (щоб редагування налаштувань конкретного сервера не показувало
// "зайнято" через самого себе).
func (a *App) CheckServerPortForServer(serverID string, port int) servers.CheckPortResult {
	return a.srvAPI.CheckPortFreeForServer(serverID, port)
}

func (a *App) ScanServerFreePort(preferred int) int {
	return a.srvAPI.ScanFreePortAPI(preferred)
}

func (a *App) CheckServerRAM(ramMB int) servers.CheckRAMResult {
	return a.srvAPI.CheckRAMAPI(ramMB)
}

// ── server.properties ───────────────────────────────────────────────────

func (a *App) GetServerProperties(id string) (servers.PropertiesDoc, error) {
	return a.srvAPI.GetServerProperties(id)
}

func (a *App) SaveServerProperties(id string, updates []servers.PropUpdate) error {
	return a.srvAPI.SaveServerProperties(id, updates)
}

// ── Іконки ──────────────────────────────────────────────────────────────

func (a *App) SetServerIcon(id, imageDataBase64 string) (string, error) {
	return a.srvAPI.SetServerIconAPI(id, imageDataBase64)
}

func (a *App) GetServerIconData(id string) (string, error) {
	return a.srvAPI.GetServerIconAPI(id)
}

func (a *App) ResetServerIcon(id string) error {
	return a.srvAPI.ResetServerIconAPI(id)
}

// ── Іконка ЛАУНЧЕРА (окремо від протокольної server-icon.png) ──────────

// SetLauncherIcon — іконка сервера в UI лаунчера (картка/списки) з
// локального файлу, обраного через BrowseImageFile().
func (a *App) SetLauncherIcon(id, srcPath string) (servers.ServerView, error) {
	return a.srvAPI.SetLauncherIconAPI(id, srcPath)
}

// GetLauncherIconData — іконка лаунчера як data URL.
func (a *App) GetLauncherIconData(id string) (string, error) {
	return a.srvAPI.GetLauncherIconAPI(id)
}

// ResetLauncherIcon — прибрати іконку лаунчера (лишається пресет ядра).
func (a *App) ResetLauncherIcon(id string) (servers.ServerView, error) {
	return a.srvAPI.ResetLauncherIconAPI(id)
}

// ── Live-статистика ───────────────────────────────────────────────────

func (a *App) GetServerStats(id string) servers.LiveStats {
	return a.srvAPI.GetServerStats(id)
}

func (a *App) HasRunningServers() bool {
	return a.srvAPI.HasRunningServers()
}

func (a *App) StopAllServers() {
	a.srvAPI.StopAllServers()
}

// GetServerPlayers — хто зараз онлайн (нік + час входу). Список веде
// бекенд, читаючи рядки логу java (див. internal/servers/players.go):
// фронтенд лише малює його та показує сповіщення на події server:player.
func (a *App) GetServerPlayers(serverID string) []servers.PlayerPresence {
	return a.srvAPI.OnlinePlayers(serverID)
}

// GetServerPlayerPlaytimes — скільки кожен гравець награв на сервері за весь
// час. Джерело — <світ>/stats/<uuid>.json (internal/servers/playtime.go),
// тож працює й для тих, хто грав ще до появи лаунчера.
func (a *App) GetServerPlayerPlaytimes(serverID string) ([]servers.PlayerPlaytime, error) {
	return a.srvAPI.PlayerPlaytimes(serverID)
}

// ── Світи + бекапи ───────────────────────────────────────────────────
func (a *App) ListWorlds(serverID string) ([]servers.WorldInfo, error) {
	return a.srvAPI.ListWorlds(serverID)
}
func (a *App) CreateWorldBackup(serverID, worldName string) (string, error) {
	return a.srvAPI.CreateWorldBackup(serverID, worldName)
}
func (a *App) ListWorldBackups(serverID, worldName string) ([]servers.BackupInfo, error) {
	return a.srvAPI.ListWorldBackups(serverID, worldName)
}
func (a *App) RestoreWorldBackup(serverID, worldName, backupID string) error {
	return a.srvAPI.RestoreWorldBackup(serverID, worldName, backupID)
}
func (a *App) DeleteWorldBackup(serverID, worldName, backupID string) error {
	return a.srvAPI.DeleteWorldBackup(serverID, worldName, backupID)
}

// ── Моди сервера ────────────────────────────────────────────────────
func (a *App) ListServerMods(serverID string) ([]model.ModEntry, error) {
	return a.srvAPI.ListServerMods(serverID)
}
func (a *App) ToggleServerMod(serverID, fileName string) error {
	return a.srvAPI.ToggleServerMod(serverID, fileName)
}
func (a *App) DeleteServerMod(serverID, fileName string) error {
	return a.srvAPI.DeleteServerMod(serverID, fileName)
}

// CheckServerModUpdates перевіряє оновлення модів сервера на Modrinth
// (один batch-запит за SHA1 усіх увімкнених модів) — окремо від
// ListServerMods, щоб відкриття вкладки не блокувалось на мережі:
// фронтенд спершу показує список з диска (миттєво), тоді викликає цей
// метод і патчить HasUpdate/LatestVersion/LatestURL в уже показаних
// картках.
//
// Спрощено відносно CheckModUpdates (модпаки, вище в цьому файлі):
// лише 1 network-прохід за SHA1, БЕЗ per-mod fallback-пошуку за назвою
// і БЕЗ CurseForge — серверні mods/-теки типово на порядок менші за
// модпаки, а моди, не знайдені за хешем (перейменований файл), просто
// лишаються без бейджа оновлення замість повільного пошуку по імені на
// кожне відкриття вкладки.
func (a *App) CheckServerModUpdates(serverID string) ([]model.ModEntry, error) {
	return a.serverModsWithSources(serverID)
}

// serverModsWithSources — скан mods/ сервера + збагачення з Modrinth
// (source/projectId/projectSlug/iconUrl/hasUpdate). Виокремлено з
// CheckServerModUpdates, бо те саме збагачення потрібне ще експорту
// списку модів (ExportServerModList — щоб у файлі були справжні
// посилання на проєкти) і порівнянню серверів (CompareServerMods).
func (a *App) serverModsWithSources(serverID string) ([]model.ModEntry, error) {
	srv, err := a.srvAPI.GetServer(serverID)
	if err != nil {
		return nil, err
	}
	mods := servers.ListServerMods(srv.Dir)
	if len(mods) == 0 || a.modrinth == nil {
		return mods, nil
	}
	loaderLow := strings.ToLower(srv.Core)

	var hashes []string
	seenHash := map[string]bool{}
	// ВИМКНЕНІ моди теж перевіряємо: .jar.disabled — це той самий jar,
	// лише з суфіксом у імені, тож SHA1 і метадані в нього ті самі.
	// Раніше умова mods[i].Enabled відсіювала їх — через це у вимкненого
	// мода не було ні версії з Modrinth, ні іконки проєкту, ні позначки
	// оновлення (скарга «не перевіряє версію/картинку, коли він вимкнений»).
	// Саме для вимкнених модів це найпотрібніше: їх часто тримають
	// вимкненими, але хочуть бачити, яка версія вже встановлена і чи є
	// новіша — щоб увімкнути саме актуальну.
	for i := range mods {
		h := mods[i].Sha1
		if h != "" && !seenHash[h] {
			seenHash[h] = true
			hashes = append(hashes, h)
		}
	}
	if len(hashes) == 0 {
		return mods, nil
	}

	latestByHash, err := a.modrinth.GetLatestVersionsByHashes(hashes, []string{loaderLow}, []string{srv.MCVersion})
	if err != nil || len(latestByHash) == 0 {
		// Мережа впала — повертаємо список без позначок оновлень
		// замість помилки: сторінка й так вже показала моди з диска.
		return mods, nil
	}

	projIDs := []string{}
	seenProj := map[string]bool{}
	for _, fv := range latestByHash {
		if fv != nil && fv.ProjectID != "" && !seenProj[fv.ProjectID] {
			seenProj[fv.ProjectID] = true
			projIDs = append(projIDs, fv.ProjectID)
		}
	}
	projects := map[string]*api.ModrinthProjectInfo{}
	if len(projIDs) > 0 {
		projects, _ = a.modrinth.GetProjectsBatch(projIDs)
	}

	for i := range mods {
		m := &mods[i]
		fv, ok := latestByHash[m.Sha1]
		if !ok || fv == nil || fv.ProjectID == "" {
			continue
		}
		m.Source = "modrinth"
		m.ProjectID = fv.ProjectID
		if pr := projects[fv.ProjectID]; pr != nil {
			m.IconURL = pr.IconURL
			m.ProjectSlug = pr.Slug
		}
		if m.Version != "" && fv.VersionNumber != "" &&
			!isSameModVersion(m.FileName, m.Version, primaryFile(fv).Filename, fv.VersionNumber) {
			m.HasUpdate = true
			m.LatestVersion = fv.VersionNumber
			m.LatestURL = primaryFile(fv).URL
		}
	}
	return mods, nil
}

// AddServerModFile зберігає jar-файл (base64 від input[type=file] UI)
// у теку mods/ або plugins/ сервера. kind: "mods" | "plugins".
func (a *App) AddServerModFile(serverID, kind, fileName, dataBase64 string) error {
	return a.srvAPI.AddServerModFile(serverID, kind, fileName, dataBase64)
}

// ── Мережа Velocity ───────────────────────────────────────────────
// UI "Мережа" на Velocity-сервері: склад мережі (проксі + backend-сервери),
// ролі/порядок/паузи, запуск/зупинка ВСІЄЇ мережі одним натисканням.
func (a *App) NetworkGetView(proxyServerID string) (servers.NetworkView, error) {
	return a.srvAPI.NetworkGetView(proxyServerID)
}

func (a *App) NetworkSave(proxyServerID string, members []servers.NetworkMemberView) error {
	return a.srvAPI.NetworkSave(proxyServerID, members)
}

func (a *App) NetworkStart(proxyServerID string) error {
	return a.srvAPI.NetworkStart(proxyServerID)
}

func (a *App) NetworkStop(proxyServerID string) error {
	return a.srvAPI.NetworkStop(proxyServerID)
}

// ── Плагіни сервера ────────────────────────────────────────────────
func (a *App) ListServerPlugins(serverID string) ([]servers.ServerPluginInfo, error) {
	return a.srvAPI.ListServerPlugins(serverID)
}
func (a *App) ToggleServerPlugin(serverID, fileName string) error {
	return a.srvAPI.ToggleServerPlugin(serverID, fileName)
}
func (a *App) DeleteServerPlugin(serverID, fileName string) error {
	return a.srvAPI.DeleteServerPlugin(serverID, fileName)
}

// ── ШІ-аналіз крашів ───────────────────────────────────────────────
func (a *App) AnalyzeServerCrash(serverID string) (servers.CrashAnalysis, error) {
	return a.srvAPI.AnalyzeServerCrash(serverID)
}
func (a *App) HasCrashReport(serverID string) bool { return a.srvAPI.HasCrashReport(serverID) }


// ── Консоль ─────────────────────────────────────────────────────────

// GetServerConsoleSnapshot повертає знімок буфера консолі сервера.
// Викликається при монтуванні ServerConsolePanel, щоб консоль НЕ
// очищалась при перемиканні між серверами/вкладками.
func (a *App) GetServerConsoleSnapshot(serverID string) []model.ConsoleLine {
	return a.srvAPI.Manager().GetServerConsoleSnapshot(serverID)
}

// ── Зв'язок зі збірками ────────────────────────────────────────────
func (a *App) GetPackServerLinks(packID string) []servers.PackServerLink {
	return a.srvAPI.GetPackServerLinks(packID)
}
func (a *App) SetPackServerLinks(packID string, links []servers.PackServerLink) error {
	return a.srvAPI.SetPackServerLinks(packID, links)
}

// ── Розумний аналізатор запуску сервера ───────────────────────────────
//
// GetServerLaunchWarnings виконує перевірки ПЕРЕД стартом (порт, RAM,
// Java, EULA, диск, ядро, минулий краш) і застосовує налаштування
// лаунчера:
//   - settings.showServerWarnings = false → показуємо ЛИШЕ порт
//     (його неможливо вимкнути: на зайнятому порту сервер не стартує,
//     і ховати причину — значити лишити людину з мовчазною відмовою);
//   - поодинокі «більше не показувати» (RAM/Java/диск/краш) фільтруються
//     окремо — EULA і ядро не мають перемикачів, бо без них старт або
//     неможливий, або безпечно відкладається.
func (a *App) GetServerLaunchWarnings(serverID string) (servers.PreflightResult, error) {
	res, err := a.srvAPI.Preflight(serverID)
	if err != nil {
		return res, err
	}
	s := a.cfg.GetSettings()

	ignored := map[string]bool{}
	if !s.ShowServerWarnings {
		// Головний вимикач: усе, крім порту, мовчить.
		for _, k := range []string{
			servers.WarnKindRAM, servers.WarnKindJava, servers.WarnKindEULA,
			servers.WarnKindDisk, servers.WarnKindCore, servers.WarnKindCrash,
		} {
			ignored[k] = true
		}
	} else {
		ignored[servers.WarnKindRAM] = s.ServerWarnIgnoreRAM
		ignored[servers.WarnKindJava] = s.ServerWarnIgnoreJava
		ignored[servers.WarnKindDisk] = s.ServerWarnIgnoreDisk
		ignored[servers.WarnKindCrash] = s.ServerWarnIgnoreCrash
	}

	visible := make([]servers.ServerWarning, 0, len(res.Warnings))
	for _, w := range res.Warnings {
		// Порт НІКОЛИ не фільтрується — він завжди показується.
		if w.Kind != servers.WarnKindPort && ignored[w.Kind] {
			continue
		}
		visible = append(visible, w)
	}

	// CanStart перераховуємо по ТОМУ, що людина реально бачить:
	// якщо fatal-попередження приховано налаштуваннями, кнопка «Старт»
	// не має блокуватись безпояснювано — бекенд все одно відповість
	// зрозумілою помилкою, якщо старт справді неможливий.
	res.Warnings = visible
	res.CanStart = true
	for _, w := range visible {
		if w.Fatal {
			res.CanStart = false
			break
		}
	}
	return res, nil
}
