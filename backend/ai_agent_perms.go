package main

// ai_agent_perms.go — дрібні права ШІ-агента за категоріями дій.
//
// Користувач вмикає/вимикає кожну категорію окремо (Налаштування → ШІ-агент → Права).
// Інструменти лише для читання доступні завжди (поки сервер увімкнено). Старе поле
// allowWrite лишилось як «головний» вимикач для вже збережених налаштувань: якщо
// perms ще не задано, діє воно для всіх категорій.

import (
	"sort"
	"strings"
	"sync/atomic"
)

// AIPermCategory — категорія прав для UI.
type AIPermCategory struct {
	ID      string `json:"id"`
	Allowed bool   `json:"allowed"`
}

// aiPermOrder — порядок категорій у UI.
var aiPermOrder = []string{"mods", "buildSettings", "create", "deleteOwn", "style", "launch", "snapshots", "servers", "launcherSettings", "gameControl"}

// aiPresets — готові набори прав.
var aiPresets = map[string][]string{
	"readonly": {},
	"safe":     {"mods", "buildSettings", "create", "deleteOwn", "style", "launch", "snapshots", "gameControl"},
	"full":     aiPermOrder,
}

// aiToolCat — категорія кожного інструмента, що змінює дані.
var aiToolCat = map[string]string{
	// моди та контент
	"delete_mod": "mods", "delete_content": "mods",
	"install_content": "mods", "install_mod_version": "mods", "toggle_mod": "mods", "update_mods": "mods",
	"install_suggested_mod": "mods", "restore_mod_backup": "mods", "migrate_mods": "mods", "update_pack_from_source": "mods",
	"add_mod_source": "mods", "detach_mod_source": "mods", "sync_mod_sources": "mods",
	"install_content_version": "mods", "toggle_content": "mods", "copy_content_between_builds": "mods", "install_datapack_to_worlds": "mods",
	// налаштування збірки
	"set_build_ram": "buildSettings", "set_build_min_ram": "buildSettings", "set_build_jvm_args": "buildSettings",
	"apply_performance_preset": "buildSettings", "write_pack_file": "buildSettings", "set_build_autojoin": "buildSettings",
	"apply_repair": "buildSettings", "run_health_fix": "buildSettings", "set_resource_packs": "buildSettings", "set_shader_pack": "buildSettings",
	// створення
	"create_build": "create", "create_modpack_from_project": "create", "duplicate_build": "create",
	// видалення власних
	"delete_ai_created_build": "deleteOwn",
	// оформлення і порядок
	"set_build_style": "style", "sort_builds": "style", "set_build_order": "style", "set_build_folder": "style", "dissolve_build_folder": "style",
	// запуск і тести
	"launch_build": "launch", "stop_build": "launch", "start_world_test": "launch",
	// знімки й бекапи
	"create_world_snapshot": "snapshots", "backup_server_world": "snapshots",
	// сервери
	"start_server": "servers", "duplicate_server": "servers", "stop_server": "servers", "server_command": "servers", "create_server": "servers",
	"set_server_config": "servers", "set_server_properties": "servers", "install_server_content": "servers",
	"write_server_config_file": "servers", "update_server_content": "servers", "toggle_server_content": "servers", "create_server_for_build": "servers", "network_save_members": "servers", "network_save_settings": "servers",
	"network_repair": "servers", "network_start": "servers", "network_stop": "servers",
	// налаштування лаунчера
	"game_status": "gameControl", "game_screenshot": "gameControl", "game_key": "gameControl", "game_mouse_move": "gameControl",
	"game_mouse_click": "gameControl", "game_scroll": "gameControl", "game_type": "gameControl", "game_chat": "gameControl", "update_game_agent": "gameControl",
	"set_launcher_setting": "launcherSettings", "set_global_jvm_args": "launcherSettings", "ensure_java": "launcherSettings",
}

// allows — чи дозволена категорія. Порожня категорія (невідомий write-інструмент) завжди заборонена.
func (s aiAgentState) allows(cat string) bool {
	if cat == "" {
		return false
	}
	// Керування грою ввімкнено за замовчуванням, але з обов'язковим запитом до користувача (див. ask «gameControl»).
	// Явне вимкнення в налаштуваннях діє.
	if cat == "gameControl" {
		if s.Perms != nil {
			if v, ok := s.Perms[cat]; ok {
				return v
			}
		}
		return true
	}
	if s.Perms == nil {
		return s.AllowWrite
	}
	return s.Perms[cat]
}

// effectivePerms — повна карта для UI.
func (s aiAgentState) effectivePerms() map[string]bool {
	out := make(map[string]bool, len(aiPermOrder))
	for _, c := range aiPermOrder {
		out[c] = s.allows(c)
	}
	return out
}

func (s aiAgentState) anyWrite() bool {
	for _, c := range aiPermOrder {
		if s.allows(c) {
			return true
		}
	}
	return false
}

// aiPermNames — для повідомлення про відмову.
func aiPermLabel(cat string) string {
	switch cat {
	case "mods":
		return "Моди та контент"
	case "buildSettings":
		return "Налаштування збірок"
	case "create":
		return "Створення збірок"
	case "deleteOwn":
		return "Видалення власних збірок"
	case "style":
		return "Оформлення й порядок збірок"
	case "launch":
		return "Запуск і тести"
	case "snapshots":
		return "Знімки світів і бекапи"
	case "servers":
		return "Сервери й проксі"
	case "launcherSettings":
		return "Налаштування лаунчера"
	case "gameControl":
		return "Керування грою"
	}
	return cat
}

// Час блокування збірки (секунди бездіяльності) — атомарно, бо читається з різних горутин.
var aiLockSecs atomic.Int64

func init() { aiLockSecs.Store(90) }

func aiLockSecondsClamp(n int) int {
	if n < 30 {
		return 30
	}
	if n > 900 {
		return 900
	}
	return n
}

// aiUnknownWriteTools повертає write-інструменти без категорії (для тесту).
func aiUnknownWriteTools() []string {
	var out []string
	for n, t := range aiAgentTools {
		if t.write && aiToolCat[n] == "" {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

func aiPresetPerms(name string) (map[string]bool, bool) {
	list, ok := aiPresets[strings.ToLower(name)]
	if !ok {
		return nil, false
	}
	m := make(map[string]bool, len(aiPermOrder))
	for _, c := range aiPermOrder {
		m[c] = false
	}
	for _, c := range list {
		m[c] = true
	}
	return m, true
}
