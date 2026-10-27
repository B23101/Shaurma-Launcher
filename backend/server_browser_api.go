package main

// ── Браузер контенту ДЛЯ СЕРВЕРІВ ───────────────────────────────────────
// Раніше Браузер умів встановлювати контент лише у ЗБІРКИ (packID +
// contentDir). Для серверів відповідних методів не існувало взагалі —
// звідси «немає у браузері вкладки встановлення плагінів чи модів для
// серверів». Тут — та сама логіка (пошук версій тим самим
// GetBrowserProjectVersions + завантаження тим самим downloadContentFile),
// але ціль — тека конкретного сервера (mods/ або plugins/).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ServerBrowserContextView — що браузер має знати про сервер, щоб шукати
// СУМІСНИЙ з ним контент (версія MC + лоадер + які теки взагалі доступні).
type ServerBrowserContextView struct {
	ServerID  string `json:"serverId"`
	MCVersion string `json:"mcVersion"`
	Core      string `json:"core"`
	// ModsKind / PluginsKind — який projectType/loader передавати у пошук
	// (порожній рядок = цей тип контенту серверу не підходить).
	ModLoader    string `json:"modLoader"`
	PluginLoader string `json:"pluginLoader"`
	// DefaultKind — що це ядро тримає за замовчуванням: "mods" для
	// Fabric/Forge/NeoForge/Quilt, "plugins" для Paper/Purpur/Velocity,
	// "" для vanilla. Фронтенд відкриває браузер одразу на потрібній
	// вкладці (Velocity → «Плагіни»), замість вкладки «Моди» з помилкою
	// «ядро не підтримує моди».
	DefaultKind string `json:"defaultKind"`
	// Встановлені імена файлів (для бейджа «вже встановлено» у списку).
	InstalledMods    []string `json:"installedMods"`
	InstalledPlugins []string `json:"installedPlugins"`
}

// serverBrowserTarget відображає (ядро сервера, тип контенту) → параметри
// пошуку й теку встановлення. Помилка = цей сервер такий контент не
// підтримує (ванільне ядро не запустить ні мод, ні плагін).
func serverBrowserTarget(core, kind string) (projectType, loader, contentDir string, err error) {
	switch kind {
	case "mods":
		c := strings.ToLower(core)
		switch c {
		case "fabric", "forge", "neoforge", "quilt":
			return "mod", c, "mods", nil
		}
		if isPluginCore(c) {
			return "", "", "", fmt.Errorf("ядро «%s» працює з плагінами, а не з модами — шукайте на вкладці «Плагіни»", core)
		}
		return "", "", "", fmt.Errorf("ядро «%s» не підтримує моди — потрібне Fabric, Forge, NeoForge або Quilt", core)
	case "plugins":
		// Modrinth-лоадер може відрізнятись від id ядра (paper-сумісні
		// плагіни позначаються як bukkit/spigot/paper), тому мапимо явно.
		switch strings.ToLower(core) {
		case "paper":
			return "plugin", "paper", "plugins", nil
		case "purpur":
			return "plugin", "purpur", "plugins", nil
		case "velocity":
			return "plugin", "velocity", "plugins", nil
		}
		switch strings.ToLower(core) {
		case "fabric", "forge", "neoforge", "quilt":
			return "", "", "", fmt.Errorf("ядро «%s» працює з модами, а не з плагінами — шукайте на вкладці «Моди»", core)
		}
		return "", "", "", fmt.Errorf("ядро «%s» не підтримує плагіни — потрібне Paper, Purpur або Velocity", core)
	}
	return "", "", "", fmt.Errorf("невідомий тип контенту сервера: %s", kind)
}

// normalizeServerKind повертає kind, який ядро реально підтримує: якщо
// запитаний вид контенту ядру не підходить, а протилежний — підходить,
// береться протилежний. Для ядер без жодного (vanilla) kind лишається як
// є — serverBrowserTarget поверне зрозумілу помилку.
func normalizeServerKind(core, kind string) string {
	c := strings.ToLower(core)
	switch kind {
	case "mods":
		if isPluginCore(c) {
			return "plugins"
		}
	case "plugins":
		switch c {
		case "fabric", "forge", "neoforge", "quilt":
			return "mods"
		}
	}
	return kind
}

// isPluginCore — чи ядро тримає плагіни (а не моди).
func isPluginCore(core string) bool {
	switch strings.ToLower(core) {
	case "paper", "purpur", "velocity":
		return true
	}
	return false
}

// ServerBrowserContext — контекст сервера для вкладки «Знайти онлайн».
func (a *App) ServerBrowserContext(serverID string) (ServerBrowserContextView, error) {
	srv, err := a.srvAPI.GetServer(serverID)
	if err != nil {
		return ServerBrowserContextView{}, err
	}
	out := ServerBrowserContextView{
		ServerID:  srv.ID,
		MCVersion: srv.MCVersion,
		Core:      srv.Core,
	}
	if _, loader, _, err := serverBrowserTarget(srv.Core, "mods"); err == nil {
		out.ModLoader = loader
		out.DefaultKind = "mods"
	}
	if _, loader, _, err := serverBrowserTarget(srv.Core, "plugins"); err == nil {
		out.PluginLoader = loader
		out.DefaultKind = "plugins"
	}
	if mods, err := a.srvAPI.ListServerMods(serverID); err == nil {
		for _, m := range mods {
			out.InstalledMods = append(out.InstalledMods, m.FileName)
		}
	}
	if plugins, err := a.srvAPI.ListServerPlugins(serverID); err == nil {
		for _, p := range plugins {
			out.InstalledPlugins = append(out.InstalledPlugins, p.FileName)
		}
	}
	return out, nil
}

// InstallServerBrowserVersion встановлює КОНКРЕТНУ версію контенту у
// сервер (versionID="" = найкраща сумісна). kind — "mods" або "plugins":
// визначає і теку, і facets пошуку (mod/plugin + лоадер ядра).
func (a *App) InstallServerBrowserVersion(serverID, source, projectID, versionID, kind string, replace bool) (BrowserInstallResult, error) {
	srv, err := a.srvAPI.GetServer(serverID)
	if err != nil {
		return BrowserInstallResult{}, fmt.Errorf("сервер не знайдено: %w", err)
	}
	// Страховка від розсинхрону вкладки/типу: Modrinth-плагіни (Paper/
	// Velocity) у публічному API нерідко мають project_type="mod", тож
	// фронтенд міг вивести kind="mods" для сервера, ядро якого моди
	// взагалі не тримає. Замість помилки «ядро velocity не підтримує
	// моди» (яку користувач отримував при встановленні плагіна, знайденого
	// самим пошуком) — тихо перемикаємось на єдиний вид контенту, який
	// це ядро підтримує.
	kind = normalizeServerKind(srv.Core, kind)
	projectType, loader, contentDir, err := serverBrowserTarget(srv.Core, kind)
	if err != nil {
		return BrowserInstallResult{}, err
	}
	if err := a.srvAPI.ServerContentLocked(serverID, kind); err != nil {
		return BrowserInstallResult{}, err
	}

	versions, err := a.GetBrowserProjectVersions(source, projectID, projectType, srv.MCVersion, loader)
	if err != nil {
		return BrowserInstallResult{}, err
	}
	var v *BrowserVersionView
	if versionID != "" {
		for i := range versions {
			if versions[i].ID == versionID {
				v = &versions[i]
				break
			}
		}
		if v == nil {
			return BrowserInstallResult{}, fmt.Errorf("версія %s несумісна з Minecraft %s / %s сервера «%s»",
				versionID, srv.MCVersion, loader, srv.Name)
		}
	} else if len(versions) > 0 {
		v = pickBestBrowserVersion(versions)
	}
	if v == nil {
		return BrowserInstallResult{}, fmt.Errorf("немає версій під Minecraft %s / %s для сервера «%s»",
			srv.MCVersion, loader, srv.Name)
	}
	fileURLs := a.versionDownloadURLs(source, projectID, v)
	if len(fileURLs) == 0 {
		return BrowserInstallResult{}, fmt.Errorf("версія %s не має посилання на завантаження", v.VersionNumber)
	}

	destDir := filepath.Join(srv.Dir, contentDir)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return BrowserInstallResult{}, err
	}
	destFile := filepath.Join(destDir, safeFileName(v.FileName, v.VersionNumber+".jar"))
	if !replace {
		if _, err := os.Stat(destFile); err == nil {
			return BrowserInstallResult{}, fmt.Errorf("файл %s уже лежить у теці %s — оберіть «Замінити»", v.FileName, contentDir)
		}
	}
	// Якщо в теці вже є СТАРІША версія цього самого проєкту (файл іншого
	// імені, але з тим самим sha1 поміж встановлених — Modrinth віддає
	// sha1 версії) — прибираємо, щоб у mods/ не лишалось двох версій
	// одного контенту (дубль ламає запуск сервера).
	if v.SHA1 != "" {
		if mods, err := a.srvAPI.ListServerMods(serverID); err == nil {
			for _, m := range mods {
				if m.FileName == filepath.Base(destFile) {
					continue
				}
				if strings.EqualFold(m.Sha1, v.SHA1) {
					_ = os.Remove(filepath.Join(destDir, m.FileName))
				}
			}
		}
	}
	// Завантаження — тим самим шляхом, що й для збірок (ретраї, подія
	// browser:progress для індикатора).
	if err := a.downloadContentFileAny(fileURLs, destFile, projectID, "server:"+serverID, v.FileName); err != nil {
		return BrowserInstallResult{}, fmt.Errorf("не вдалося завантажити: %w", err)
	}
	// Кеш аналізу «вже встановлено» (сторінка проєкту в браузері, бейдж
	// «Встановлено»/«Оновити») інвалідуємо одразу — інакше після
	// встановлення з браузера бейдж лишався б старим до зміни mtime/
	// розміру, яку InvalidateBrowserInstalledCache саме й перевіряє
	// (див. GetServerBrowserInstalledMap / browserInstalledMapFor).
	a.InvalidateBrowserInstalledCache("srv-" + serverID)
	// Список модів/плагінів на сторінці сервера перечитується одразу.
	a.emit("server:mods-changed", serverID)
	return BrowserInstallResult{
		PackID:      serverID,
		FileName:    v.FileName,
		InstalledTo: contentDir + "/" + v.FileName,
		Message:     fmt.Sprintf("Встановлено у %s сервера «%s»", labelForKind(kind), srv.Name),
	}, nil
}

func labelForKind(kind string) string {
	if kind == "plugins" {
		return "плагіни"
	}
	return "моди"
}
