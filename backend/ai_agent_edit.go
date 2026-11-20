package main

// ai_agent_edit.go — «сесія редагування» збірки ШІ-агентом.
//
// Поки агент змінює збірку, вона заблокована для гравця: у UI з'являється вікно
// «ШІ-агент редагує збірку» зі стрічкою змін (мод, налаштування, оформлення…),
// а виклики лаунчера з інтерфейсу, що змінюють/запускають цю збірку, відхиляються
// (див. aiEditGuard у dispatch). Так гравець і ШІ не правлять одну збірку одночасно.
//
// Сесія тримається, поки агент активний, і сама закінчується через aiEditIdle
// без нових змін, або по finish_editing / відключенню агента.

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	aiEditMaxChanges = 200
)

// AIEditChange — один запис стрічки змін.
type AIEditChange struct {
	Time    int64  `json:"time"`
	Kind    string `json:"kind"` // mod | setting | style | create | test | other
	Summary string `json:"summary"`
	File    string `json:"file,omitempty"` // для іконки мода (GetModIcons)
	OK      bool   `json:"ok"`
}

// AIEditSession — стан редагування однієї збірки.
type AIEditSession struct {
	PackID  string         `json:"packId"`
	Name    string         `json:"name"`
	Client  string         `json:"client"`
	Started int64          `json:"started"`
	Last    int64          `json:"last"`
	Active  bool           `json:"active"`
	Current string         `json:"current"`
	Changes []AIEditChange `json:"changes"`
}

var aiEdit struct {
	mu      sync.Mutex
	m       map[string]*AIEditSession
	started bool
}

func aiEditCopy(s *AIEditSession) AIEditSession {
	c := *s
	c.Changes = append([]AIEditChange{}, s.Changes...)
	return c
}

func (a *App) aiEditName(id string) string {
	if a != nil && a.cfg != nil {
		if n := a.GetInstanceConfig(id).Name; n != "" {
			return n
		}
	}
	return id
}

func (a *App) aiEditEmit(s *AIEditSession) {
	a.emit("aiagent:edit", aiEditCopy(s))
}

func (a *App) aiEditStartTicker() {
	if aiEdit.started {
		return
	}
	aiEdit.started = true
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for range t.C {
			now := time.Now().UnixMilli()
			// 1) кандидати на завершення за бездіяльністю (мережі/сесій під мʼютексом не чіпаємо)
			var idle []string
			aiEdit.mu.Lock()
			for id, s := range aiEdit.m {
				if s.Active && now-s.Last > aiEditIdleMs() {
					idle = append(idle, id)
				}
			}
			aiEdit.mu.Unlock()
			// 2) поки гра ШІ на цій збірці ще йде, збірка лишається «під керуванням ШІ»
			playing := map[string]bool{}
			for _, id := range idle {
				if a.aiPackHasAIClient(id) {
					playing[id] = true
				}
			}
			var changed []AIEditSession
			aiEdit.mu.Lock()
			for id, s := range aiEdit.m {
				if playing[id] && s.Active {
					s.Last, s.Current = now, "Гра запущена (тест ШІ)"
					changed = append(changed, aiEditCopy(s))
					continue
				}
				if s.Active && now-s.Last > aiEditIdleMs() {
					s.Active, s.Current = false, ""
					changed = append(changed, aiEditCopy(s))
				}
				// Старі завершені сесії, які користувач не закрив, прибираємо.
				if !s.Active && now-s.Last > (30*time.Minute).Milliseconds() {
					delete(aiEdit.m, id)
				}
			}
			aiEdit.mu.Unlock()
			for _, c := range changed {
				a.emit("aiagent:edit", c)
			}
		}
	}()
}

// aiPackHasAIClient — чи йде на збірці гра будь-якого клієнта ШІ.
func (a *App) aiPackHasAIClient(packID string) bool {
	if a == nil || a.cfg == nil || a.sessionsM == nil {
		return false
	}
	base := a.aiAgentNickNow()
	for i := 1; i <= 4; i++ {
		if a.aiClientRunning(packID, aiClientNick(base, i)) {
			return true
		}
	}
	return false
}

// aiToolHuman — людська назва дії для UI (бокова панель, кнопка «Грати», накладка).
var aiToolHuman = map[string]string{
	"delete_mod": "Видалення мода", "delete_content": "Видалення контенту",
	"install_content": "Встановлення мода", "install_mod_version": "Оновлення мода", "toggle_mod": "Перемикання мода",
	"update_mods": "Оновлення модів", "install_suggested_mod": "Додавання залежності", "restore_mod_backup": "Відкат мода",
	"migrate_mods": "Перенесення модів", "update_pack_from_source": "Оновлення модпаку", "install_content_version": "Оновлення контенту",
	"toggle_content": "Перемикання контенту", "copy_content_between_builds": "Копіювання контенту", "install_datapack_to_worlds": "Встановлення датапака",
	"add_mod_source": "Джерело мода", "detach_mod_source": "Відкріплення джерела", "sync_mod_sources": "Синхронізація джерел",
	"set_build_ram": "Налаштування пам'яті", "set_build_min_ram": "Налаштування пам'яті", "set_build_jvm_args": "Налаштування Java",
	"apply_performance_preset": "Пресет графіки", "write_pack_file": "Зміна конфігу", "set_build_autojoin": "Автовхід у світ",
	"apply_repair": "Автовиправлення", "run_health_fix": "Виправлення збірки", "set_resource_packs": "Ресурспаки", "set_shader_pack": "Шейдер",
	"create_build": "Створення збірки", "duplicate_build": "Копіювання збірки", "create_modpack_from_project": "Створення збірки з модпаку",
	"set_build_style": "Оформлення збірки", "launch_build": "Запуск гри", "stop_build": "Зупинка гри", "start_world_test": "Тест у світі",
	"create_world_snapshot": "Знімок світів", "delete_ai_created_build": "Видалення збірки ШІ", "create_server_for_build": "Створення сервера",
	"game_status": "Стан гри", "game_screenshot": "Скріншот гри", "game_key": "Керування грою", "game_mouse_move": "Керування грою",
	"game_mouse_click": "Керування грою", "game_scroll": "Керування грою", "game_type": "Керування грою", "game_chat": "Чат у грі",
}

func aiHumanLabel(tool string, args map[string]any) string {
	l := aiToolHuman[tool]
	if l == "" {
		l = tool
	}
	if f, ok := args["file_name"].(string); ok && f != "" {
		return l + ": " + strings.TrimSuffix(f, ".jar")
	}
	return l
}

// aiEditIdleMs — скільки мс без дій агента збірка лишається заблокованою (налаштовується).
func aiEditIdleMs() int64 { return aiLockSecs.Load() * 1000 }

// aiEditBegin бере/поновлює блокування збірки й показує, що агент робить зараз.
func (a *App) aiEditBegin(packID, current string) {
	if packID == "" {
		return
	}
	client := ""
	aiAgent.actMu.Lock()
	client = aiAgent.client
	aiAgent.actMu.Unlock()
	now := time.Now().UnixMilli()
	aiEdit.mu.Lock()
	if aiEdit.m == nil {
		aiEdit.m = map[string]*AIEditSession{}
	}
	s := aiEdit.m[packID]
	if s == nil || !s.Active {
		// нова сесія (стару завершену історію замінюємо)
		s = &AIEditSession{PackID: packID, Started: now}
		aiEdit.m[packID] = s
	}
	s.Name, s.Client, s.Last, s.Active, s.Current = a.aiEditName(packID), client, now, true, current
	cp := aiEditCopy(s)
	a.aiEditStartTicker()
	aiEdit.mu.Unlock()
	a.emit("aiagent:edit", cp)
}

// aiEditRecord додає запис у стрічку (і поновлює блокування).
func (a *App) aiEditRecord(packID string, c AIEditChange) {
	if packID == "" {
		return
	}
	c.Time = time.Now().UnixMilli()
	aiEdit.mu.Lock()
	s := aiEdit.m[packID]
	if s == nil {
		aiEdit.mu.Unlock()
		return
	}
	s.Name, s.Last, s.Current = a.aiEditName(packID), c.Time, ""
	s.Changes = append([]AIEditChange{c}, s.Changes...)
	if len(s.Changes) > aiEditMaxChanges {
		s.Changes = s.Changes[:aiEditMaxChanges]
	}
	cp := aiEditCopy(s)
	aiEdit.mu.Unlock()
	a.emit("aiagent:edit", cp)
}

func (a *App) aiEditFinish(packID string) {
	aiEdit.mu.Lock()
	s := aiEdit.m[packID]
	if s == nil || !s.Active {
		aiEdit.mu.Unlock()
		return
	}
	s.Active, s.Current, s.Last = false, "", time.Now().UnixMilli()
	cp := aiEditCopy(s)
	aiEdit.mu.Unlock()
	a.emit("aiagent:edit", cp)
}

func (a *App) aiEditFinishAll() {
	aiEdit.mu.Lock()
	var ids []string
	for id, s := range aiEdit.m {
		if s.Active {
			ids = append(ids, id)
		}
	}
	aiEdit.mu.Unlock()
	for _, id := range ids {
		a.aiEditFinish(id)
	}
}

// aiEditActiveFor — активна сесія для збірки (для guard).
func aiEditActiveFor(packID string) (AIEditSession, bool) {
	aiEdit.mu.Lock()
	defer aiEdit.mu.Unlock()
	s := aiEdit.m[packID]
	if s == nil || !s.Active || time.Now().UnixMilli()-s.Last > aiEditIdleMs() {
		return AIEditSession{}, false
	}
	return aiEditCopy(s), true
}

// GetAIAgentEditSessions — для UI при відкритті сторінки збірки.
func (a *App) GetAIAgentEditSessions() []AIEditSession {
	aiEdit.mu.Lock()
	defer aiEdit.mu.Unlock()
	out := make([]AIEditSession, 0, len(aiEdit.m))
	for _, s := range aiEdit.m {
		out = append(out, aiEditCopy(s))
	}
	return out
}

// DismissAIAgentEdit — гравець закрив вікно завершеної сесії.
func (a *App) DismissAIAgentEdit(packID string) {
	aiEdit.mu.Lock()
	if s := aiEdit.m[packID]; s != nil && !s.Active {
		delete(aiEdit.m, packID)
	}
	aiEdit.mu.Unlock()
}

// aiEditGuardedMethods — виклики з UI, що змінюють/запускають збірку; перший
// аргумент — id збірки. Виклики самого агента йдуть повз dispatch і не блокуються.
var aiEditGuardedMethods = map[string]bool{
	"LaunchInstance": true, "LaunchInstanceOnServer": true, "LaunchInstanceShortcut": true,
	"InstallModVersion": true, "DeleteMod": true, "ToggleMod": true, "UpdateAllMods": true,
	"UpdateOneModBackup": true, "RestoreModBackup": true, "InstallSuggestedMod": true,
	"InstallBrowserContent": true, "InstallBrowserContentVersion": true,
	"ToggleContentFile": true, "DeleteContentFile": true, "ImportContentFiles": true, "InstallContentVersion": true,
	"UpdateCustomPack": true, "SetPackRAM": true, "SetConsoleRAM": true, "ApplyPerformancePreset": true,
	"MigrateMods": true, "StartModMigration": true, "UpdatePackFromSource": true, "DeletePack": true,
	"ApplyPackRepairs": true, "RunHealthCheckFix": true, "WritePackTextFile": true, "DeletePackEntry": true,
}

// aiEditGuard повертає помилку, якщо UI намагається змінити збірку, яку зараз редагує агент.
func (a *App) aiEditGuard(method string, rawArgs []json.RawMessage) error {
	if !aiEditGuardedMethods[method] || len(rawArgs) == 0 {
		return nil
	}
	var id string
	if json.Unmarshal(rawArgs[0], &id) != nil || id == "" {
		return nil
	}
	if s, ok := aiEditActiveFor(id); ok {
		return fmt.Errorf("ШІ-агент зараз редагує збірку «%s». Зачекайте завершення або відключіть агента в налаштуваннях", s.Name)
	}
	return nil
}

// aiEditPackFor — збірка, яку зачіпає інструмент (за аргументами).
func aiEditPackFor(args map[string]any) string {
	for _, k := range []string{"pack_id", "target_id"} {
		if v, ok := args[k].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// aiEditCreatedID дістає id нової збірки з результату інструментів-створювачів.
func aiEditCreatedID(res any) string {
	b, err := json.Marshal(res)
	if err != nil {
		return ""
	}
	var m struct {
		ID   string `json:"id"`
		Pack struct {
			ID string `json:"id"`
		} `json:"pack"`
	}
	if json.Unmarshal(b, &m) != nil {
		return ""
	}
	if m.ID != "" {
		return m.ID
	}
	return m.Pack.ID
}

// aiEditChangeFor описує виконану зміну людською мовою для стрічки.
func aiEditChangeFor(tool string, args map[string]any, res any, runErr error) AIEditChange {
	g := argMap(args)
	c := AIEditChange{OK: runErr == nil, Kind: "other"}
	short := func(s string) string {
		if r := []rune(s); len(r) > 80 {
			return string(r[:80]) + "…"
		}
		return s
	}
	switch tool {
	case "install_content":
		c.Kind = "mod"
		c.Summary = "Встановлено мод"
		var r struct {
			FileName string `json:"fileName"`
			Message  string `json:"message"`
		}
		if b, e := json.Marshal(res); e == nil && json.Unmarshal(b, &r) == nil && r.FileName != "" {
			c.File = r.FileName
			c.Summary = "Встановлено: " + r.FileName
			if i := strings.Index(r.Message, "+"); i >= 0 {
				c.Summary += " (+ " + short(strings.TrimSpace(r.Message[i+1:])) + ")"
			}
		}
	case "install_mod_version":
		c.Kind, c.Summary = "mod", "Замінено версію мода: "+short(g.str("file_name"))
	case "delete_mod", "delete_content":
		c.Kind, c.File = "mod", g.str("file_name")
		c.Summary = "Видалено: " + short(c.File)
	case "toggle_mod":
		c.Kind = "mod"
		c.File = g.str("file_name")
		if g.boolean("enabled", true) {
			c.Summary = "Мод увімкнено: " + short(c.File)
		} else {
			c.Summary = "Мод вимкнено: " + short(c.File)
		}
	case "install_content_version":
		c.Kind, c.Summary = "mod", "Оновлено/замінено: "+short(g.str("file_name"))
	case "toggle_content":
		c.Kind = "mod"
		if g.boolean("enabled", true) {
			c.Summary = "Увімкнено: " + short(g.str("file_name"))
		} else {
			c.Summary = "Вимкнено: " + short(g.str("file_name"))
		}
	case "copy_content_between_builds":
		c.Kind, c.Summary = "mod", "Скопійовано контент: "+short(g.str("file_name"))
	case "install_datapack_to_worlds":
		c.Kind, c.Summary = "mod", "Датапак у світи: "+short(g.str("worlds"))
	case "duplicate_server":
		c.Kind, c.Summary = "create", "Створено копію сервера"
	case "add_mod_source":
		c.Kind, c.Summary = "mod", "Прикріплено джерело мода: "+short(filepath.Base(g.str("path")))
	case "detach_mod_source":
		c.Kind, c.Summary = "mod", "Відкріплено джерело мода"
	case "sync_mod_sources":
		c.Kind, c.Summary = "mod", "Синхронізовано джерела модів"
	case "create_server_for_build":
		c.Kind, c.Summary = "create", "Створення сервера під збірку"
	case "set_resource_packs":
		c.Kind, c.Summary = "setting", "Активні ресурспаки змінено"
	case "set_shader_pack":
		c.Kind, c.Summary = "setting", "Шейдер: "+short(g.str("shader"))
	case "update_mods":
		c.Kind, c.Summary = "mod", "Оновлення всіх модів (з резервною копією)"
	case "install_suggested_mod":
		c.Kind, c.Summary = "mod", "Додано залежність: "+short(g.str("dep_id"))
	case "restore_mod_backup":
		c.Kind, c.Summary = "mod", "Відкат мода з резервної копії"
	case "migrate_mods":
		c.Kind, c.Summary = "mod", "Перенесення модів зі збірки «"+short(g.str("source_id"))+"»"
		if g.boolean("dry_run", true) {
			c.Summary = "Аналіз перенесення модів зі збірки «" + short(g.str("source_id")) + "»"
		}
	case "set_build_ram":
		c.Kind, c.Summary = "setting", fmt.Sprintf("Максимум RAM: %d МБ", g.integer("ram_mb", 0))
	case "set_build_min_ram":
		c.Kind, c.Summary = "setting", fmt.Sprintf("Стартова RAM: %d МБ", g.integer("ram_mb", 0))
	case "set_build_jvm_args":
		c.Kind, c.Summary = "setting", "Змінено Java-аргументи"
	case "apply_performance_preset":
		c.Kind, c.Summary = "setting", "Пресет графіки: "+g.str("preset")
	case "write_pack_file":
		c.Kind, c.Summary = "setting", "Змінено конфіг: "+short(filepath.ToSlash(g.str("path")))
	case "set_build_autojoin":
		c.Kind, c.Summary = "setting", "Автовхід: "+g.str("mode")
	case "set_build_style":
		c.Kind = "style"
		var parts []string
		for k, l := range map[string]string{"name": "назва", "description": "опис", "color": "колір", "icon": "іконка"} {
			if _, ok := args[k]; ok {
				parts = append(parts, l)
			}
		}
		c.Summary = "Оформлення: " + strings.Join(parts, ", ")
	case "create_build":
		c.Kind, c.Summary = "create", "Створено збірку"
	case "duplicate_build":
		c.Kind, c.Summary = "create", "Створено копію збірки"
	case "create_modpack_from_project":
		c.Kind, c.Summary = "create", "Створено збірку з модпаку"
	case "update_pack_from_source":
		c.Kind, c.Summary = "mod", "Оновлення модпаку до іншої версії"
	case "apply_repair":
		c.Kind, c.Summary = "setting", "Автовиправлення: "+short(g.str("fix_ids"))
	case "run_health_fix":
		c.Kind, c.Summary = "setting", "Виправлення health-check: "+short(g.str("action_id"))
	case "create_world_snapshot":
		c.Kind, c.Summary = "other", "Знімок світів (резервна копія)"
	case "launch_build":
		c.Kind, c.Summary = "test", "Тестовий запуск гри"
	case "stop_build":
		c.Kind, c.Summary = "test", "Гру зупинено"
	default:
		c.Summary = tool
	}
	if runErr != nil {
		c.Summary = "Помилка: " + c.Summary + " — " + short(runErr.Error())
	}
	return c
}
