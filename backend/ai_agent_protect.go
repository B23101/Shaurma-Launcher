package main

// ai_agent_protect.go — захист даних користувача та копіювання серверів.
//
// За замовчуванням агент НЕ змінює збірки й сервери, які створив не він: інструмент
// відхиляється з інструкцією спитати користувача «редагуємо поточну збірку чи копію?».
//  • «копію» → duplicate_build / duplicate_server, далі працювати на копії (вона створена
//    агентом, тож змінювати її можна);
//  • «поточну» → повторити виклик із confirm_edit_original=true ТІЛЬКИ після прямої відповіді
//    користувача.
// Вимикається в налаштуваннях («Питати перед зміною моїх даних»).

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"shaurma-launcher-backend/internal/servers"
)

// Інструменти, що не псують наявні дані (читають джерело й створюють нове або лише запускають).
var aiProtectExempt = map[string]bool{
	"start_server": true, "stop_server": true,
	"create_server": true, "create_server_for_build": true, "duplicate_server": true,
}

// Категорії, у яких інструменти змінюють уже наявні збірки/сервери.
var aiProtectedCats = map[string]bool{"mods": true, "buildSettings": true, "servers": true}

func aiToolProtected(name string) bool {
	if aiProtectExempt[name] {
		return false
	}
	return aiProtectedCats[aiToolCat[name]] || name == "start_world_test"
}

// aiProtectTarget — що саме змінює інструмент: ("build"|"server", id). Порожньо — нічого наявного.
func aiProtectTarget(args map[string]any) (kind, id string) {
	if v := aiEditPackFor(args); v != "" {
		switch {
		case strings.HasPrefix(v, modTargetServerPlugins):
			return "server", strings.TrimPrefix(v, modTargetServerPlugins)
		case strings.HasPrefix(v, modTargetServerMods):
			return "server", strings.TrimPrefix(v, modTargetServerMods)
		}
		return "build", v
	}
	if v, ok := args["server_id"].(string); ok && strings.TrimSpace(v) != "" {
		return "server", strings.TrimSpace(v)
	}
	if v, ok := args["proxy_id"].(string); ok && strings.TrimSpace(v) != "" {
		return "server", strings.TrimSpace(v)
	}
	return "", ""
}

// Команди консолі сервера, що лише читають стан (не потребують підтвердження).
var aiReadOnlyCmds = map[string]bool{
	"list": true, "tps": true, "mspt": true, "spark": true, "plugins": true, "pl": true, "version": true, "ver": true,
	"about": true, "help": true, "seed": true, "banlist": true, "forge": true, "neoforge": true, "mods": true,
}

func aiReadOnlyCommand(tool string, args map[string]any) bool {
	if tool != "server_command" {
		return false
	}
	c, _ := args["command"].(string)
	c = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(c), "/")))
	f := strings.Fields(c)
	if len(f) == 0 {
		return false
	}
	if aiReadOnlyCmds[f[0]] {
		return true
	}
	return (f[0] == "whitelist" && len(f) > 1 && f[1] == "list") || (f[0] == "time" && len(f) > 1 && f[1] == "query") ||
		(f[0] == "data" && len(f) > 1 && f[1] == "get")
}

// aiProtectCheck повертає текст відмови, якщо виклик зачіпає дані користувача всупереч політиці.
func (a *App) aiProtectCheck(tool string, args map[string]any, st aiAgentState) string {
	pol := st.editPolicy()
	if pol == "original" || !aiToolProtected(tool) || aiReadOnlyCommand(tool, args) {
		return ""
	}
	if ok, _ := args["confirm_edit_original"].(bool); ok && pol == "ask" {
		return ""
	}
	kind, id := aiProtectTarget(args)
	if id == "" || a.aiProvenanceOf(kind, id) != "" {
		return "" // нова збірка/сервер агента — можна
	}
	what, dup := "збірку", "duplicate_build"
	if kind == "server" {
		what, dup = "сервер", "duplicate_server"
	}
	if pol == "copy" {
		return fmt.Sprintf("ПОЛІТИКА КОРИСТУВАЧА: «%s» — це %s користувача, і він налаштував «завжди працювати на копії». "+
			"НЕ питайте: викличте %s і виконуйте зміни на копії (повідомте користувачу, що працюєте на копії).", id, what, dup)
	}
	return fmt.Sprintf("ЗУПИНІТЬСЯ І СПИТАЙТЕ КОРИСТУВАЧА. «%s» — це %s користувача, не створена агентом. "+
		"Запитайте: «Редагуємо поточну чи робимо копію?». "+
		"Якщо КОПІЮ: викличте %s і виконуйте зміни на копії. "+
		"Якщо ПОТОЧНУ: повторіть цей виклик із confirm_edit_original=true лише після прямої відповіді користувача. "+
		"(Налаштування → ШІ-агент → «Зміни моїх даних».)", id, what, dup)
}

// ── «Коли питати користувача» ─────────────────────────────────────────────

// aiAskOrder — порядок перемикачів у UI; aiAskDefault — значення за замовчуванням.
var aiAskOrder = []string{"bulk", "serverCreate", "delete", "launcherSettings", "launch", "gameControl", "commands", "createBuild"}
var aiAskDefault = map[string]bool{"bulk": true, "serverCreate": true, "delete": true, "launcherSettings": true, "launch": false, "gameControl": true, "commands": false, "createBuild": false}

var aiAskWhat = map[string]string{
	"bulk":             "масову зміну (оновлення всіх модів, міграція модів, оновлення модпаку)",
	"serverCreate":     "створення або копіювання сервера (і прийняття EULA Mojang)",
	"delete":           "видалення збірки",
	"launcherSettings": "зміну налаштувань лаунчера",
	"launch":           "запуск гри",
	"gameControl":      "керування запущеною грою (скріншоти, клавіші, миша, чат): питається один раз на сеанс гри",
	"commands":         "команду в консоль сервера",
	"createBuild":      "створення нової збірки",
}

// aiAskKeyFor — до якого перемикача належить виклик ("" — питати не треба).
func aiAskKeyFor(tool string, args map[string]any) string {
	switch tool {
	case "update_mods", "update_server_content", "update_pack_from_source":
		return "bulk"
	case "migrate_mods":
		if d, ok := args["dry_run"].(bool); ok && !d {
			return "bulk"
		}
		if args == nil {
			return "bulk" // для схеми інструмента
		}
	case "create_server", "create_server_for_build", "duplicate_server":
		return "serverCreate"
	case "delete_ai_created_build":
		return "delete"
	case "set_launcher_setting", "set_global_jvm_args", "ensure_java":
		return "launcherSettings"
	case "launch_build", "start_world_test":
		return "launch"
	case "game_status", "game_screenshot", "game_key", "game_mouse_move", "game_mouse_click", "game_scroll", "game_type", "game_chat":
		return "gameControl"
	case "server_command":
		if args != nil && aiReadOnlyCommand(tool, args) {
			return ""
		}
		return "commands"
	case "create_build", "create_modpack_from_project", "duplicate_build":
		return "createBuild"
	}
	return ""
}

// aiAskCheck повертає текст відмови, якщо за налаштуваннями користувача перед дією треба спитати.
func (a *App) aiAskCheck(tool string, args map[string]any, st aiAgentState) string {
	key := aiAskKeyFor(tool, args)
	if key == "" || !st.asks(key) {
		return ""
	}
	if key == "gameControl" {
		return a.aiGameControlAsk(tool, args)
	}
	if ok, _ := args["user_confirmed"].(bool); ok {
		return ""
	}
	return fmt.Sprintf("ЗУПИНІТЬСЯ І СПИТАЙТЕ КОРИСТУВАЧА. За його налаштуваннями перед дією «%s» (%s) треба спитати. "+
		"Коротко опишіть, що саме збираєтесь зробити, і дочекайтесь прямої згоди; потім повторіть виклик із user_confirmed=true. "+
		"(Налаштування → ШІ-агент → «Коли питати».)", tool, aiAskWhat[key])
}

// ── Копіювання серверів ───────────────────────────────────────────────────

const aiDupServerMaxBytes = 6 << 30 // 6 ГБ

// aiCopyTree копіює теку, пропускаючи вказані імена й не-звичайні файли (символьні посилання тощо).
func aiCopyTree(src, dst string, skip func(rel string, d fs.DirEntry) bool) (int64, error) {
	var total int64
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if rel == "." {
			return os.MkdirAll(dst, 0o755)
		}
		if skip != nil && skip(filepath.ToSlash(rel), d) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		total += info.Size()
		return aiCopyFile(p, target)
	})
	return total, err
}

func aiTreeSize(src string, skip func(rel string, d fs.DirEntry) bool) int64 {
	var n int64
	_ = filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(src, p)
		if rel != "." && skip != nil && skip(filepath.ToSlash(rel), d) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() {
			if info, e := d.Info(); e == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}

func init() {
	regTool(aiTool{
		name: "duplicate_server",
		desc: "Зробити копію сервера (теки, моди/плагіни, конфіги, світи) під новим ID і новим вільним портом — щоб експериментувати, не псуючи оригінал. Оригінал має бути зупинений. Логи та краш-репорти не копіюються; include_worlds=false — без світів (швидше).",
		props: map[string]string{"server_id": "id сервера-оригіналу", "new_name": "назва копії (за замовч. «<назва> (копія)»)", "include_worlds": "(bool) копіювати світи, за замовч. true",
			"accept_eula": "(bool) лише якщо в оригіналі немає eula.txt: користувач прийняв EULA Mojang"},
		required: []string{"server_id"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			id := g.str("server_id")
			src, err := a.srvAPI.GetServer(id)
			if err != nil {
				return nil, fmt.Errorf("сервер не знайдено: %s", id)
			}
			if a.srvAPI.Manager().IsRunning(id) {
				return nil, errors.New("спершу зупиніть сервер (stop_server): копіювати світ під час гри небезпечно")
			}
			withWorlds := g.boolean("include_worlds", true)
			worldDirs := map[string]bool{}
			if withWorlds {
				// нічого пропускати зі світів
			} else if ws, err := a.ListWorlds(id); err == nil {
				for _, w := range ws {
					worldDirs[w.Name] = true
				}
			}
			skip := func(rel string, d fs.DirEntry) bool {
				top := strings.SplitN(rel, "/", 2)[0]
				switch top {
				case "logs", "crash-reports", ".shaurma":
					return true
				}
				if strings.HasSuffix(rel, "session.lock") {
					return true
				}
				if !withWorlds && worldDirs[top] {
					return true
				}
				return false
			}
			if sz := aiTreeSize(src.Dir, skip); sz > aiDupServerMaxBytes {
				return nil, fmt.Errorf("сервер завеликий для копіювання через агента (%d МБ). Спробуйте include_worlds=false", sz>>20)
			}
			name := g.str("new_name")
			if name == "" {
				name = src.Name + " (копія)"
			}
			if len([]rune(name)) > 60 {
				return nil, errors.New("new_name завдовга")
			}
			chk := a.CheckServerFolder(name)
			folder := chk.Folder
			for i := 2; !a.CheckServerFolder(folder).Free && i < 50; i++ {
				folder = fmt.Sprintf("%s %d", chk.Folder, i)
			}
			if !a.CheckServerFolder(folder).Free {
				return nil, errors.New("не вдалося підібрати вільну назву теки")
			}
			dst := filepath.Join(a.GetAppDir(), "servers", folder)
			if _, err := os.Stat(dst); err == nil {
				return nil, errors.New("тека призначення вже існує: " + dst)
			}
			copied, err := aiCopyTree(src.Dir, dst, skip)
			if err != nil {
				_ = os.RemoveAll(dst)
				return nil, fmt.Errorf("копіювання не вдалося: %w", err)
			}
			view, err := a.AddServerFromFolder(dst, name, src.RAMMB, g.boolean("accept_eula", false))
			if err != nil {
				_ = os.RemoveAll(dst)
				return nil, err
			}
			a.aiProvenanceMark("server", view.ID)
			// Новий вільний порт, щоб копія не конфліктувала з оригіналом.
			port := a.ScanServerFreePort(src.Port + 1)
			note := ""
			if port > 0 && port != view.Port {
				if err := a.SaveServerProperties(view.ID, []servers.PropUpdate{{Key: "server-port", Value: fmt.Sprint(port)}}); err != nil {
					note = "порт у server.properties не змінено: " + err.Error()
				}
				if _, err := a.SaveServerConfig(view.ID, servers.ServerConfigPatch{Port: &port}); err != nil {
					note += " порт у налаштуваннях не змінено: " + err.Error()
				}
				view.Port = port
			}
			return map[string]any{"server_id": view.ID, "name": view.Name, "port": view.Port, "address": fmt.Sprintf("127.0.0.1:%d", view.Port),
				"copied_mb": copied >> 20, "worlds_included": withWorlds, "note": strings.TrimSpace(note)}, nil
		},
	})
}

// aiGameControlAsk: згода на керування грою діє на весь поточний сеанс гри (питати перед кожним натисканням клавіші не можна).
func (a *App) aiGameControlAsk(tool string, args map[string]any) string {
	pack, _ := args["pack_id"].(string)
	client := 0
	if f, ok := args["client"].(float64); ok {
		client = int(f)
	}
	s, err := a.aiGameFor(pack, client)
	if err != nil {
		return "" // немає сесії: інструмент сам поверне зрозумілу помилку
	}
	confirmed, _ := args["user_confirmed"].(bool)
	aiGames.mu.Lock()
	defer aiGames.mu.Unlock()
	if s.Approved {
		return ""
	}
	if confirmed {
		s.Approved = true
		return ""
	}
	return "ЗУПИНІТЬСЯ І СПИТАЙТЕ КОРИСТУВАЧА. Перед керуванням запущеною грою (скріншоти, клавіші, миша, чат) за його налаштуваннями треба отримати згоду: " +
		"вона діє на весь поточний сеанс гри. Коротко опишіть, що збираєтесь робити в грі, і після прямої згоди повторіть виклик із user_confirmed=true. " +
		"(Налаштування → ШІ-агент → «Коли питати».)"
}
