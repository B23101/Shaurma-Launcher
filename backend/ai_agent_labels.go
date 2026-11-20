package main

// ai_agent_labels.go — людські підписи дій ШІ для плашки/журналу: назва дії українською,
// справжня назва цілі (збірка, мод, запит), а не id, і іконка Tabler.

import (
	"encoding/json"
	"strings"
)

// aiToolHumanRead — назви інструментів лише для читання (змінні — в aiToolHuman).
var aiToolHumanRead = map[string]string{
	"get_instructions": "Читання інструкції", "get_overview": "Огляд лаунчера", "list_builds": "Список збірок",
	"list_mods": "Перегляд модів", "find_duplicate_mods": "Пошук дублікатів", "check_mod_updates": "Перевірка оновлень",
	"search_content": "Пошук у Modrinth/CurseForge", "get_project": "Опис проєкту", "get_project_versions": "Версії проєкту",
	"get_console": "Читання консолі", "read_log_file": "Читання логу", "list_log_files": "Список логів",
	"wait_for_console": "Очікування рядка в консолі", "diagnose_pack": "Діагностика збірки", "list_build_problems": "Пошук проблем збірки",
	"review_build": "Перевірка якості збірки", "get_system_info": "Характеристики ПК", "list_ai_clients": "Клієнти ШІ",
	"list_content": "Перегляд контенту", "read_pack_file": "Читання файлу збірки", "list_build_worlds": "Світи збірки",
	"list_servers": "Список серверів", "get_server_console": "Консоль сервера", "list_world_snapshots": "Знімки світів",
	"game_status": "Стан гри", "list_mod_sources": "Джерела модів", "validate_jvm_args": "Перевірка Java-аргументів",
}

// aiToolIcons — іконки Tabler для інструментів; решта — за замовчуванням (змінна/читання).
var aiToolIcons = map[string]string{
	"delete_mod": "ti-trash", "delete_content": "ti-trash",
	"install_content": "ti-package", "install_mod_version": "ti-refresh", "toggle_mod": "ti-toggle-right", "update_mods": "ti-refresh",
	"install_suggested_mod": "ti-package", "restore_mod_backup": "ti-history", "migrate_mods": "ti-arrows-right-left",
	"install_content_version": "ti-package", "toggle_content": "ti-toggle-right", "copy_content_between_builds": "ti-copy",
	"set_build_ram": "ti-cpu", "set_build_min_ram": "ti-cpu", "set_build_jvm_args": "ti-coffee", "apply_performance_preset": "ti-bolt",
	"write_pack_file": "ti-file-pencil", "set_shader_pack": "ti-sun", "set_resource_packs": "ti-photo",
	"create_build": "ti-plus", "duplicate_build": "ti-copy", "create_modpack_from_project": "ti-plus", "set_build_style": "ti-palette",
	"launch_build": "ti-player-play", "stop_build": "ti-player-stop", "start_world_test": "ti-world",
	"create_world_snapshot": "ti-shield-check", "delete_ai_created_build": "ti-trash",
	"search_content": "ti-search", "get_project": "ti-info-circle", "get_project_versions": "ti-versions",
	"get_console": "ti-terminal-2", "read_log_file": "ti-file-text", "wait_for_console": "ti-hourglass", "diagnose_pack": "ti-stethoscope",
	"list_build_problems": "ti-alert-triangle", "review_build": "ti-clipboard-check", "list_mods": "ti-list", "find_duplicate_mods": "ti-copy",
	"check_mod_updates": "ti-refresh", "get_instructions": "ti-book", "add_lesson": "ti-bulb", "forget_lesson": "ti-bulb-off",
	"game_screenshot": "ti-camera", "game_status": "ti-activity", "game_key": "ti-keyboard", "game_chat": "ti-message",
	"game_mouse_move": "ti-pointer", "game_mouse_click": "ti-click", "game_scroll": "ti-arrows-vertical", "game_type": "ti-keyboard",
	"get_overview": "ti-layout-dashboard", "list_builds": "ti-layout-grid", "get_system_info": "ti-device-desktop",
	"start_server": "ti-server", "stop_server": "ti-server-off", "server_command": "ti-terminal", "create_server_for_build": "ti-server",
}

func aiToolIcon(tool string, write bool) string {
	if ic, ok := aiToolIcons[tool]; ok {
		return ic
	}
	if write {
		return "ti-tool"
	}
	return "ti-eye"
}

func aiToolTitle(tool string) string {
	if l := aiToolHuman[tool]; l != "" {
		return l
	}
	if l := aiToolHumanRead[tool]; l != "" {
		return l
	}
	s := strings.ReplaceAll(tool, "_", " ")
	if s != "" {
		s = strings.ToUpper(s[:1]) + s[1:]
	}
	return s
}

// aiTargetName — справжня назва цілі виклику: назва збірки замість id, запит, імʼя файлу.
func (a *App) aiTargetName(args map[string]any) string {
	short := func(s string) string {
		if r := []rune(s); len(r) > 48 {
			return string(r[:48]) + "…"
		}
		return s
	}
	g := argMap(args)
	if f := g.str("file_name"); f != "" {
		return short(strings.TrimSuffix(f, ".jar"))
	}
	if q := g.str("query"); q != "" {
		return short(q)
	}
	for _, k := range []string{"pack_id", "build_id"} {
		if id := g.str(k); id != "" {
			if a != nil && a.customPacks != nil {
				if b, err := a.resolveLaunchBuild(id); err == nil && b != nil && b.Name != "" {
					return short(b.Name)
				}
			}
			return short(id)
		}
	}
	for _, k := range []string{"server_id", "source_id", "key", "file", "shader", "name"} {
		if v := g.str(k); v != "" {
			return short(strings.TrimSuffix(v, ".jar"))
		}
	}
	return ""
}

// aiResultTarget — після виконання уточнює ціль реальною назвою (напр. встановлений файл мода).
func aiResultTarget(tool string, res any) string {
	if tool != "install_content" && tool != "install_mod_version" && tool != "install_content_version" {
		return ""
	}
	b, err := json.Marshal(res)
	if err != nil {
		return ""
	}
	var r struct {
		FileName string `json:"fileName"`
	}
	if json.Unmarshal(b, &r) != nil {
		return ""
	}
	return strings.TrimSuffix(r.FileName, ".jar")
}
