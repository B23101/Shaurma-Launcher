package main

// ai_agent_tools_ext.go — розширений набір MCP-інструментів ШІ-агента:
// сервери, налаштування лаунчера, логи й краші, версії модів, авторемонт,
// конфіги збірок, дублювання збірок для безпечних експериментів.
//
// Межі (див. також ai_agent_api.go): жодного видалення збірок/модів/серверів/світів,
// жодного відновлення світів, жодних акаунтів/токенів/ключів, жодних рядків-команд
// (pre/post-launch, wrapper, env) і шляхів у налаштуваннях лаунчера. Усе, що
// пише на диск, — лише для текстових конфігів із білого списку й з резервною копією.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"shaurma-launcher-backend/internal/autorepair"
	"shaurma-launcher-backend/internal/config"
	"shaurma-launcher-backend/internal/jvmargs"
	"shaurma-launcher-backend/internal/model"
	"shaurma-launcher-backend/internal/servers"
)

const (
	aiMaxLogBytes    = 48 << 10
	aiMaxConfigBytes = 512 << 10
)

// Налаштування лаунчера, які ШІ змінювати не може навіть якщо вони «безпечного» типу.
var aiDeniedSettings = map[string]bool{
	"autoUpdate": true, "updateChannel": true, "activeAccountId": true,
	"disableSkins": true, "disableMojangSkinFallback": true,
}

var aiSettingTypes = map[string]bool{
	"bool": true, "int": true, "select": true, "choice": true, "accent": true,
	"language": true, "color": true, "ram": true,
}

func aiConfigExtOK(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".toml", ".json", ".json5", ".cfg", ".conf", ".properties", ".yml", ".yaml", ".txt", ".ini":
		return true
	}
	return false
}

// aiLogFileOK — лише logs/*.log(.gz не читаємо) і crash-reports/*.txt.
func aiLogPath(a *App, packID, name string) (string, error) {
	if packID == "" {
		return "", errPackID()
	}
	if name == "" || filepath.Base(name) != name || strings.ContainsAny(name, `\/:`) {
		return "", errors.New("file має бути іменем файлу з list_log_files")
	}
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".log"):
		return filepath.Join(a.gameDir(packID), "logs", name), nil
	case strings.HasSuffix(lower, ".txt") && strings.HasPrefix(lower, "crash-"):
		return filepath.Join(a.gameDir(packID), "crash-reports", name), nil
	case strings.HasSuffix(lower, ".txt") && strings.HasPrefix(lower, "hs_err"):
		return filepath.Join(a.gameDir(packID), name), nil
	}
	return "", errors.New("дозволені лише .log та crash-*.txt")
}

func tailFile(path string, maxBytes int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	start := int64(0)
	if st.Size() > maxBytes {
		start = st.Size() - maxBytes
	}
	buf := make([]byte, st.Size()-start)
	if _, err := f.ReadAt(buf, start); err != nil && err.Error() != "EOF" {
		return "", err
	}
	s := string(buf)
	if start > 0 {
		s = "…[початок обрізано]\n" + s
	}
	return s, nil
}

func aiPackFile(a *App, packID, rel string, write bool) (string, error) {
	if packID == "" {
		return "", errPackID()
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel)))
	top := strings.SplitN(clean, "/", 2)[0]
	if clean == "options.txt" {
		return resolveRelPath(a.gameDir(packID), clean)
	}
	if top != "config" && top != "defaultconfigs" {
		return "", errors.New("доступні лише config/…, defaultconfigs/… та options.txt")
	}
	if !aiConfigExtOK(clean) {
		return "", errors.New("дозволені лише текстові конфіги (.toml .json .cfg .properties .yml …)")
	}
	return resolveRelPath(a.gameDir(packID), clean)
}

func init() {
	// ═══ Загальний стан ═══
	regTool(aiTool{
		name: "get_overview", desc: "Короткий огляд лаунчера: версія, чи йде гра, збірки, сервери та їх стан. Починайте розмову з цього виклику.",
		run: func(a *App, g argMap) (any, error) {
			bl := []map[string]any{}
			for _, v := range a.GetBuilds() {
				if v.Installed || v.Status != "" {
					bl = append(bl, map[string]any{"id": v.Build.ID, "name": v.Build.Name, "mc": v.Build.MCVersion, "loader": v.Build.Loader, "status": v.Status,
						"created_by": aiAuthorOrUser(a.aiProvenanceOf("build", v.Build.ID))})
				}
			}
			sl := []map[string]any{}
			if list, err := a.ListServers(); err == nil {
				for _, s := range list {
					sl = append(sl, map[string]any{"id": s.ID, "name": s.Name, "status": a.GetServerStatus(s.ID)})
				}
			}
			return map[string]any{"launcher_version": a.GetVersion(), "game_running": a.IsGameRunning(),
				"builds": bl, "servers": sl, "account": a.aiAccountKind()}, nil
		},
	})

	// ═══ Налаштування лаунчера ═══
	regTool(aiTool{
		name: "list_launcher_settings", desc: "Схема налаштувань лаунчера (ключ, тип, опис, допустимі значення) і поточні значення. Змінювати можна лише безпечні типи (bool/int/select/…).",
		run: func(a *App, g argMap) (any, error) {
			cur := map[string]any{}
			if b, err := json.Marshal(a.GetSettings()); err == nil {
				_ = json.Unmarshal(b, &cur)
			}
			out := []map[string]any{}
			for _, d := range config.SettingsSchema() {
				opts := []string{}
				for _, o := range d.Options {
					opts = append(opts, o.Value)
				}
				out = append(out, map[string]any{"key": d.Key, "type": d.Type, "label": d.Label, "desc": d.Desc,
					"options": opts, "min": d.Min, "max": d.Max, "value": cur[d.Key],
					"editable": aiSettingTypes[d.Type] && !aiDeniedSettings[d.Key]})
			}
			return out, nil
		},
	})
	regTool(aiTool{
		name: "set_launcher_setting", desc: "Змінити одне налаштування лаунчера за ключем зі list_launcher_settings (лише безпечні типи).",
		props:    map[string]string{"key": "ключ налаштування", "value": "нове значення (рядок; для bool — true/false, для int — число)"},
		required: []string{"key", "value"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			key := g.str("key")
			var def *model.SettingDef
			for _, d := range config.SettingsSchema() {
				if d.Key == key {
					d := d
					def = &d
					break
				}
			}
			if def == nil {
				return nil, fmt.Errorf("невідомий ключ %q", key)
			}
			if !aiSettingTypes[def.Type] || aiDeniedSettings[key] {
				return nil, fmt.Errorf("налаштування %q змінювати через ШІ заборонено", key)
			}
			raw := fmt.Sprint(g["value"])
			var val any
			switch def.Type {
			case "bool":
				if raw != "true" && raw != "false" {
					return nil, errors.New("очікується true або false")
				}
				val = raw == "true"
			case "int", "ram":
				n := g.integer("value", -1<<31)
				if n == -1<<31 {
					return nil, errors.New("очікується число")
				}
				if def.Min != nil && n < *def.Min || def.Max != nil && n > *def.Max {
					return nil, errors.New("значення поза допустимим діапазоном")
				}
				if def.Type == "ram" && (n < 1024 || n > 65536) {
					return nil, errors.New("RAM має бути 1024..65536 МБ")
				}
				val = n
			case "select", "choice":
				ok := len(def.Options) == 0
				for _, o := range def.Options {
					if o.Value == raw {
						ok = true
					}
				}
				if !ok {
					return nil, errors.New("значення не входить до options")
				}
				val = raw
			default: // accent, language, color
				if len(raw) > 64 || strings.ContainsAny(raw, "\r\n<>\"'") {
					return nil, errors.New("недопустиме значення")
				}
				val = raw
			}
			cur := map[string]any{}
			b, _ := json.Marshal(a.GetSettings())
			_ = json.Unmarshal(b, &cur)
			cur[key] = val
			b, _ = json.Marshal(cur)
			var s model.Settings
			if err := json.Unmarshal(b, &s); err != nil {
				return nil, err
			}
			if err := a.SaveSettings(s); err != nil {
				return nil, err
			}
			return map[string]any{"key": key, "value": val}, nil
		},
	})
	regTool(aiTool{
		name: "set_global_jvm_args", desc: "Глобальні Java-аргументи за замовчуванням (лише дозволені білим списком).",
		props: map[string]string{"args": "рядок JVM-аргументів"}, required: []string{"args"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			raw := strings.TrimSpace(g.str("args"))
			if _, rej := jvmargs.SanitizeUser(raw); len(rej) > 0 {
				return nil, fmt.Errorf("відхилені аргументи: %v", rej)
			}
			s := a.GetSettings()
			s.JavaArgs = raw
			return "ok", a.SaveSettings(s)
		},
	})
	regTool(aiTool{
		name: "ensure_java", desc: "Завантажити/підготувати Java для лаунчера, якщо її немає.",
		write: true,
		run:   func(a *App, g argMap) (any, error) { return "ok", a.EnsureJava() },
	})
	regTool(aiTool{
		name: "get_storage_suggestions", desc: "Підказки щодо звільнення місця (невикористані версії, бібліотеки). Лише перегляд.",
		run: func(a *App, g argMap) (any, error) { return a.GetStorageSuggestions(), nil },
	})

	// ═══ Логи, краші, авторемонт ═══
	regTool(aiTool{
		name: "list_log_files", desc: "Файли логів і крашів збірки: logs/*.log та crash-reports (нові першими).",
		props: map[string]string{"pack_id": "id збірки"}, required: []string{"pack_id"},
		run: func(a *App, g argMap) (any, error) {
			if g.str("pack_id") == "" {
				return nil, errPackID()
			}
			gd := a.gameDir(g.str("pack_id"))
			type fi struct {
				Name string `json:"name"`
				Kind string `json:"kind"`
				Size int64  `json:"size"`
				Time string `json:"modified"`
				t    time.Time
			}
			var out []fi
			for _, d := range []struct{ dir, kind, ext string }{{"logs", "log", ".log"}, {"crash-reports", "crash", ".txt"}, {".", "jvm-crash", ".log"}} {
				es, _ := os.ReadDir(filepath.Join(gd, d.dir))
				for _, e := range es {
					n := e.Name()
					if e.IsDir() || !strings.HasSuffix(strings.ToLower(n), d.ext) {
						continue
					}
					if d.kind == "jvm-crash" && !strings.HasPrefix(n, "hs_err") {
						continue
					}
					if info, err := e.Info(); err == nil {
						out = append(out, fi{n, d.kind, info.Size(), info.ModTime().Format(time.RFC3339), info.ModTime()})
					}
				}
			}
			sort.Slice(out, func(i, j int) bool { return out[i].t.After(out[j].t) })
			if len(out) > 40 {
				out = out[:40]
			}
			return out, nil
		},
	})
	regTool(aiTool{
		name: "read_log_file", desc: "Прочитати кінець лог-файлу або crash-report збірки (до ~48 КБ). Файли — з list_log_files (latest.log, crash-…txt).",
		props:    map[string]string{"pack_id": "id збірки", "file": "імʼя файлу, напр. latest.log", "grep": "якщо задано — повернути лише рядки з цим підрядком (без регістру) + 2 рядки контексту"},
		required: []string{"pack_id", "file"},
		run: func(a *App, g argMap) (any, error) {
			p, err := aiLogPath(a, g.str("pack_id"), g.str("file"))
			if err != nil {
				return nil, err
			}
			txt, err := tailFile(p, aiMaxLogBytes*4)
			if err != nil {
				return nil, err
			}
			if q := strings.ToLower(g.str("grep")); q != "" {
				lines := strings.Split(txt, "\n")
				var out []string
				last := -10
				for i, l := range lines {
					if strings.Contains(strings.ToLower(l), q) {
						from := i - 2
						if from <= last {
							from = last + 1
						}
						if from < 0 {
							from = 0
						}
						to := i + 2
						if to >= len(lines) {
							to = len(lines) - 1
						}
						out = append(out, lines[from:to+1]...)
						last = to
					}
				}
				txt = strings.Join(out, "\n")
			}
			if len(txt) > aiMaxLogBytes {
				txt = "…[початок обрізано]\n" + txt[len(txt)-aiMaxLogBytes:]
			}
			return txt, nil
		},
	})
	regTool(aiTool{
		name: "wait_for_console", desc: "Чекати (до 90 с) появи тексту в консолі гри — напр. після launch_build чекати «Done»/«Sound engine started» або помилку. Повертає знайдені рядки або таймаут.",
		props:    map[string]string{"build_id": "id збірки", "contains": "підрядок для очікування", "timeout_sec": "(int) 1..90, за замовч. 60", "client": "(int) номер клієнта ШІ (за замовч. 1)"},
		required: []string{"build_id", "contains"},
		run: func(a *App, g argMap) (any, error) {
			if err := g.require("build_id", "contains"); err != nil {
				return nil, err
			}
			to := g.integer("timeout_sec", 60)
			if to < 1 || to > 90 {
				to = 60
			}
			deadline := time.Now().Add(time.Duration(to) * time.Second)
			for {
				pg := a.aiConsolePageFor(g.str("build_id"), g.integer("client", 1), 0, 20, "", g.str("contains"), 0)
				if len(pg.Lines) > 0 {
					return map[string]any{"found": true, "lines": pg.Lines, "errors": pg.Errors}, nil
				}
				if time.Now().After(deadline) {
					return map[string]any{"found": false, "game_running": a.IsGameRunning(), "errors": pg.Errors}, nil
				}
				time.Sleep(1500 * time.Millisecond)
			}
		},
	})
	regTool(aiTool{
		name: "get_launch_warnings", desc: "Попередження перед запуском (мало RAM, стара Java, проблемні моди) + pre-launch перевірка модів.",
		props: map[string]string{"pack_id": "id збірки"}, required: []string{"pack_id"},
		run: func(a *App, g argMap) (any, error) {
			if g.str("pack_id") == "" {
				return nil, errPackID()
			}
			return map[string]any{"warnings": a.GetLaunchWarnings(g.str("pack_id")), "mod_issues": a.CheckPreLaunchModIssues(g.str("pack_id"))}, nil
		},
	})
	regTool(aiTool{
		name: "apply_repair", desc: "Застосувати автоматичні виправлення зі свіжої diagnose_pack за id лагоджень (fix.id). Параметри лагоджень лаунчер бере сам, не агент.",
		props:    map[string]string{"pack_id": "id збірки", "fix_ids": "id лагоджень через кому (з diagnose_pack → issues[].fixes[].id)"},
		required: []string{"pack_id", "fix_ids"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			if err := g.require("pack_id", "fix_ids"); err != nil {
				return nil, err
			}
			rep, err := a.DiagnosePack(g.str("pack_id"))
			if err != nil {
				return nil, err
			}
			want := map[string]bool{}
			for _, id := range strings.Split(g.str("fix_ids"), ",") {
				want[strings.TrimSpace(id)] = true
			}
			var fixes []autorepair.Fix
			for _, is := range rep.Issues {
				for _, f := range is.Fixes {
					if want[f.ID] {
						fixes = append(fixes, f)
					}
				}
			}
			if len(fixes) == 0 {
				return nil, errors.New("жодного з fix_ids немає у свіжій діагностиці")
			}
			return a.ApplyPackRepairs(g.str("pack_id"), fixes)
		},
	})
	regTool(aiTool{
		name: "run_health_fix", desc: "Застосувати виправлення з health_check за id дії (actionId).",
		props: map[string]string{"pack_id": "id збірки", "action_id": "id дії зі звіту health_check"}, required: []string{"pack_id", "action_id"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			if err := g.require("pack_id", "action_id"); err != nil {
				return nil, err
			}
			return "ok", a.RunHealthCheckFix(g.str("pack_id"), g.str("action_id"))
		},
	})

	// ═══ Моди: версії, залежності, відкат ═══
	regTool(aiTool{
		name: "get_mod_versions", desc: "Доступні версії встановленого мода (для оновлення, відкату чи пошуку сумісної).",
		props:    map[string]string{"pack_id": "id збірки", "file_name": "файл мода з list_mods", "mc_version": "версія Minecraft", "loader": "лоадер"},
		required: []string{"pack_id", "file_name", "mc_version", "loader"},
		run: func(a *App, g argMap) (any, error) {
			if err := g.require("pack_id", "file_name", "mc_version", "loader"); err != nil {
				return nil, err
			}
			vs := a.GetModVersions(g.str("pack_id"), g.str("file_name"), g.str("mc_version"), g.str("loader"))
			for i := range vs {
				if len(vs[i].Changelog) > 300 {
					vs[i].Changelog = vs[i].Changelog[:300] + "…"
				}
			}
			return vs, nil
		},
	})
	regTool(aiTool{
		name: "install_mod_version", desc: "Замінити встановлений мод конкретною версією (version_id з get_mod_versions). Старий файл лаунчер зберігає в резерві.",
		props:    map[string]string{"pack_id": "id збірки", "file_name": "файл мода", "version_id": "id версії", "mc_version": "версія Minecraft", "loader": "лоадер"},
		required: []string{"pack_id", "file_name", "version_id", "mc_version", "loader"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			if err := g.require("pack_id", "file_name", "version_id", "mc_version", "loader"); err != nil {
				return nil, err
			}
			for _, v := range a.GetModVersions(g.str("pack_id"), g.str("file_name"), g.str("mc_version"), g.str("loader")) {
				if v.ID == g.str("version_id") {
					return "ok", a.InstallModVersion(g.str("pack_id"), g.str("file_name"), v)
				}
			}
			return nil, errors.New("version_id не знайдено серед версій цього мода")
		},
	})
	regTool(aiTool{
		name: "get_suggested_mods", desc: "Рекомендовані та відсутні залежності модів збірки.",
		props: map[string]string{"pack_id": "id збірки", "mc_version": "версія Minecraft", "loader": "лоадер"}, required: []string{"pack_id", "mc_version", "loader"},
		run: func(a *App, g argMap) (any, error) {
			if err := g.require("pack_id", "mc_version", "loader"); err != nil {
				return nil, err
			}
			return a.GetSuggestedMods(g.str("pack_id"), g.str("mc_version"), g.str("loader")), nil
		},
	})
	regTool(aiTool{
		name: "install_suggested_mod", desc: "Встановити мод/залежність за id (Modrinth slug або project id) з автопідбором сумісної версії.",
		props:    map[string]string{"pack_id": "id збірки", "dep_id": "id залежності", "mc_version": "версія Minecraft", "loader": "лоадер"},
		required: []string{"pack_id", "dep_id", "mc_version", "loader"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			if err := g.require("pack_id", "dep_id", "mc_version", "loader"); err != nil {
				return nil, err
			}
			return a.InstallSuggestedMod(g.str("pack_id"), g.str("dep_id"), g.str("mc_version"), g.str("loader")), nil
		},
	})
	regTool(aiTool{
		name: "list_mod_backups", desc: "Резервні копії модів (зберігаються перед оновленням) для відкату.",
		props: map[string]string{"pack_id": "id збірки"}, required: []string{"pack_id"},
		run: func(a *App, g argMap) (any, error) {
			if g.str("pack_id") == "" {
				return nil, errPackID()
			}
			return a.ListModBackups(g.str("pack_id")), nil
		},
	})
	regTool(aiTool{
		name: "restore_mod_backup", desc: "Відкотити мод з резервної копії (backup_id зі list_mod_backups) — напр. якщо після оновлення гра перестала запускатись.",
		props: map[string]string{"pack_id": "id збірки", "backup_id": "id копії"}, required: []string{"pack_id", "backup_id"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			if err := g.require("pack_id", "backup_id"); err != nil {
				return nil, err
			}
			for _, b := range a.ListModBackups(g.str("pack_id")) {
				if b.ID == g.str("backup_id") {
					return "ok", a.RestoreModBackup(g.str("pack_id"), b)
				}
			}
			return nil, errors.New("backup_id не знайдено")
		},
	})
	regTool(aiTool{
		name: "compare_builds", desc: "Порівняти моди двох збірок (чим відрізняються).",
		props: map[string]string{"left_id": "id першої збірки", "right_id": "id другої"}, required: []string{"left_id", "right_id"},
		run: func(a *App, g argMap) (any, error) {
			if err := g.require("left_id", "right_id"); err != nil {
				return nil, err
			}
			return a.CompareModPacks(g.str("left_id"), g.str("right_id"))
		},
	})

	// ═══ Збірки: оновлення, експерименти, оптимізація ═══
	regTool(aiTool{
		name: "duplicate_build", desc: "Зробити копію збірки для безпечних експериментів (оновлення модів, нові версії) — оригінал не чіпається.",
		props: map[string]string{"source_id": "id збірки-джерела", "new_name": "назва копії"}, required: []string{"source_id"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			if g.str("source_id") == "" {
				return nil, errors.New("source_id обовʼязковий")
			}
			res, err := a.DuplicatePack(DuplicatePackRequest{SourcePackID: g.str("source_id"), NewName: g.str("new_name"), CopyIcon: true})
			if err == nil {
				a.aiProvenanceMark("build", res.Pack.ID)
			}
			return res, err
		},
	})
	regTool(aiTool{
		name: "get_pack_source_versions", desc: "Версії модпаку з Modrinth/CurseForge, з якого створено збірку (для оновлення модпаку).",
		props: map[string]string{"pack_id": "id збірки"}, required: []string{"pack_id"},
		run: func(a *App, g argMap) (any, error) {
			if g.str("pack_id") == "" {
				return nil, errPackID()
			}
			return a.GetPackSourceVersions(g.str("pack_id"))
		},
	})
	regTool(aiTool{
		name: "update_pack_from_source", desc: "Оновити збірку до іншої версії її модпаку (version_id з get_pack_source_versions). Світи збірки — рекомендується спершу create_world_snapshot.",
		props: map[string]string{"pack_id": "id збірки", "version_id": "id версії модпаку"}, required: []string{"pack_id", "version_id"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			if err := g.require("pack_id", "version_id"); err != nil {
				return nil, err
			}
			return a.UpdatePackFromSource(g.str("pack_id"), g.str("version_id"))
		},
	})
	regTool(aiTool{
		name: "create_world_snapshot", desc: "Зберегти знімок світів збірки (перед ризикованими змінами). Відновлення — лише вручну в лаунчері.",
		props: map[string]string{"pack_id": "id збірки"}, required: []string{"pack_id"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			if g.str("pack_id") == "" {
				return nil, errPackID()
			}
			name, err := a.CreatePackWorldSnapshot(g.str("pack_id"))
			if err == nil && name != "" {
				a.aiProvenanceMark("world-snapshot", g.str("pack_id")+"|"+name)
			}
			return name, err
		},
	})
	regTool(aiTool{
		name: "apply_performance_preset", desc: "Застосувати пресет продуктивності графіки (low | balanced) — змінює options.txt збірки. Гра має бути закрита.",
		props: map[string]string{"pack_id": "id збірки", "preset": "low | balanced"}, required: []string{"pack_id", "preset"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			if err := g.require("pack_id", "preset"); err != nil {
				return nil, err
			}
			if p := g.str("preset"); p != "low" && p != "balanced" {
				return nil, errors.New("preset: low або balanced")
			}
			return "ok", a.ApplyPerformancePreset(g.str("pack_id"), g.str("preset"))
		},
	})
	regTool(aiTool{
		name: "set_build_min_ram", desc: "Стартова памʼять (-Xms) збірки в МБ; 0 = автоматично.",
		props: map[string]string{"pack_id": "id збірки", "ram_mb": "(int) МБ"}, required: []string{"pack_id", "ram_mb"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			if g.str("pack_id") == "" {
				return nil, errPackID()
			}
			n := g.integer("ram_mb", 0)
			if n < 0 || n > 65536 {
				return nil, errors.New("ram_mb поза діапазоном")
			}
			cfg := a.GetInstanceConfig(g.str("pack_id"))
			cfg.ID = g.str("pack_id")
			cfg.MinRAMOverride = n
			return "ok", a.SaveInstanceConfig(cfg)
		},
	})
	regTool(aiTool{
		name: "list_pack_configs", desc: "Список текстових конфігів збірки (config/ і defaultconfigs/) для тонкого налаштування модів.",
		props: map[string]string{"pack_id": "id збірки"}, required: []string{"pack_id"},
		run: func(a *App, g argMap) (any, error) {
			if g.str("pack_id") == "" {
				return nil, errPackID()
			}
			gd := a.gameDir(g.str("pack_id"))
			var out []string
			for _, top := range []string{"config", "defaultconfigs"} {
				_ = filepath.WalkDir(filepath.Join(gd, top), func(p string, d os.DirEntry, err error) error {
					if err != nil || len(out) >= 400 {
						return nil
					}
					if !d.IsDir() && aiConfigExtOK(d.Name()) {
						if rel, e := filepath.Rel(gd, p); e == nil {
							out = append(out, filepath.ToSlash(rel))
						}
					}
					return nil
				})
			}
			return out, nil
		},
	})
	regTool(aiTool{
		name: "read_pack_file", desc: "Прочитати конфіг збірки (config/…, defaultconfigs/…, options.txt).",
		props: map[string]string{"pack_id": "id збірки", "path": "відносний шлях, напр. config/sodium-options.json"}, required: []string{"pack_id", "path"},
		run: func(a *App, g argMap) (any, error) {
			p, err := aiPackFile(a, g.str("pack_id"), g.str("path"), false)
			if err != nil {
				return nil, err
			}
			st, err := os.Stat(p)
			if err != nil || st.IsDir() || st.Size() > aiMaxConfigBytes {
				return nil, errors.New("файл не знайдено або завеликий")
			}
			b, err := os.ReadFile(p)
			return string(b), err
		},
	})
	regTool(aiTool{
		name: "write_pack_file", desc: "Перезаписати існуючий конфіг збірки повним новим вмістом (config/…, defaultconfigs/…, options.txt). Поруч створюється копія .ai-bak. Гра має бути закрита.",
		props: map[string]string{"pack_id": "id збірки", "path": "відносний шлях", "content": "повний новий текст файлу"}, required: []string{"pack_id", "path", "content"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			p, err := aiPackFile(a, g.str("pack_id"), g.str("path"), true)
			if err != nil {
				return nil, err
			}
			content, _ := g["content"].(string)
			if len(content) > aiMaxConfigBytes {
				return nil, errors.New("вміст завеликий")
			}
			for _, s := range a.sessionsM.Snapshot() {
				if s.BuildID == g.str("pack_id") {
					return nil, errors.New("спочатку зупиніть гру (stop_build)")
				}
			}
			old, err := os.ReadFile(p)
			if err != nil {
				return nil, errors.New("файл має існувати (створення нових файлів недоступне)")
			}
			if err := os.WriteFile(p+".ai-bak", old, 0o644); err != nil {
				return nil, err
			}
			return "ok", os.WriteFile(p, []byte(content), 0o644)
		},
	})

	// ═══ Сервери ═══
	regTool(aiTool{
		name: "list_servers", desc: "Усі сервери Minecraft лаунчера (id, ядро, версія, порт, RAM).",
		run: func(a *App, g argMap) (any, error) { return a.ListServers() },
	})
	regTool(aiTool{
		name: "get_server", desc: "Деталі сервера + статус, живі показники й гравці онлайн.",
		props: map[string]string{"server_id": "id сервера"}, required: []string{"server_id"},
		run: func(a *App, g argMap) (any, error) {
			id := g.str("server_id")
			sv, err := a.GetServer(id)
			if err != nil {
				return nil, err
			}
			return map[string]any{"server": sv, "status": a.GetServerStatus(id), "stats": a.GetServerStats(id), "players": a.GetServerPlayers(id)}, nil
		},
	})
	regTool(aiTool{
		name: "get_server_console", desc: "Рядки консолі сервера (фільтр за рівнем/текстом; offset<0 — кінець логу).",
		props:    map[string]string{"server_id": "id сервера", "level": "error/warn/info або порожньо", "query": "підрядок", "limit": "(int) за замовч. 200", "offset": "(int) -1 = хвіст"},
		required: []string{"server_id"},
		run: func(a *App, g argMap) (any, error) {
			if g.str("server_id") == "" {
				return nil, errors.New("server_id обовʼязковий")
			}
			lim := g.integer("limit", 200)
			if lim < 1 || lim > 1000 {
				lim = 200
			}
			return a.GetServerConsolePage(g.str("server_id"), g.integer("offset", -1), lim, g.str("level"), g.str("query"), 0), nil
		},
	})
	regTool(aiTool{
		name: "server_preflight", desc: "Перевірка перед запуском сервера (RAM, Java, порт, EULA, попередній краш).",
		props: map[string]string{"server_id": "id сервера"}, required: []string{"server_id"},
		run: func(a *App, g argMap) (any, error) { return a.GetServerLaunchWarnings(g.str("server_id")) },
	})
	regTool(aiTool{
		name: "read_server_crash", desc: "Останній crash-report сервера (текст) для аналізу. Зовнішній ШІ-сервіс лаунчера не викликається.",
		props: map[string]string{"server_id": "id сервера"}, required: []string{"server_id"},
		run: func(a *App, g argMap) (any, error) {
			root, err := a.serverRootDir(g.str("server_id"))
			if err != nil {
				return nil, err
			}
			es, _ := os.ReadDir(filepath.Join(root, "crash-reports"))
			var best string
			var bt time.Time
			for _, e := range es {
				if info, err := e.Info(); err == nil && !e.IsDir() && strings.HasSuffix(e.Name(), ".txt") && info.ModTime().After(bt) {
					best, bt = e.Name(), info.ModTime()
				}
			}
			if best == "" {
				return "краш-репортів немає (дивіться get_server_console та read_server_file logs/latest.log)", nil
			}
			txt, err := tailFile(filepath.Join(root, "crash-reports", best), aiMaxLogBytes)
			return best + "\n" + txt, err
		},
	})
	regTool(aiTool{
		name: "read_server_file", desc: "Прочитати текстовий файл сервера (logs/latest.log, server.properties, конфіги плагінів тощо). Шлях відносно теки сервера.",
		props: map[string]string{"server_id": "id сервера", "path": "напр. logs/latest.log"}, required: []string{"server_id", "path"},
		run: func(a *App, g argMap) (any, error) {
			if err := g.require("server_id", "path"); err != nil {
				return nil, err
			}
			root, err := a.serverRootDir(g.str("server_id"))
			if err != nil {
				return nil, err
			}
			p, err := resolveRelPath(root, g.str("path"))
			if err != nil {
				return nil, err
			}
			if strings.HasSuffix(strings.ToLower(p), ".log") {
				return tailFile(p, aiMaxLogBytes)
			}
			fc, err := a.ReadServerTextFile(g.str("server_id"), g.str("path"))
			if err != nil {
				return nil, err
			}
			return fc, nil
		},
	})
	regTool(aiTool{
		name: "get_server_properties", desc: "server.properties сервера (ключі та значення).",
		props: map[string]string{"server_id": "id сервера"}, required: []string{"server_id"},
		run: func(a *App, g argMap) (any, error) { return a.GetServerProperties(g.str("server_id")) },
	})
	regTool(aiTool{
		name: "list_server_content", desc: "Моди та плагіни сервера.",
		props: map[string]string{"server_id": "id сервера"}, required: []string{"server_id"},
		run: func(a *App, g argMap) (any, error) {
			m, _ := a.ListServerMods(g.str("server_id"))
			p, _ := a.ListServerPlugins(g.str("server_id"))
			return map[string]any{"mods": m, "plugins": p}, nil
		},
	})
	regTool(aiTool{
		name: "list_server_worlds", desc: "Світи сервера та їхні бекапи.",
		props: map[string]string{"server_id": "id сервера"}, required: []string{"server_id"},
		run: func(a *App, g argMap) (any, error) { return a.ListWorlds(g.str("server_id")) },
	})
	regTool(aiTool{
		name: "start_server", desc: "Запустити сервер. Стежте через get_server / get_server_console.",
		props: map[string]string{"server_id": "id сервера"}, required: []string{"server_id"}, write: true,
		run: func(a *App, g argMap) (any, error) { return "ok", a.StartServer(g.str("server_id")) },
	})
	regTool(aiTool{
		name: "stop_server", desc: "Зупинити сервер (graceful за замовчуванням — з збереженням світу).",
		props: map[string]string{"server_id": "id сервера", "graceful": "(bool) за замовч. true"}, required: []string{"server_id"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			return "ok", a.StopServer(g.str("server_id"), g.boolean("graceful", true))
		},
	})
	regTool(aiTool{
		name: "server_command", desc: "Надіслати команду в консоль запущеного сервера (без «/») і повернути її вивід, напр. «list», «tps», «say hi», «whitelist add Nick», «save-all». Команди клієнта гри (чат у світі) надіслати неможливо.",
		props: map[string]string{"server_id": "id сервера", "command": "команда", "wait_seconds": "(int) скільки секунд чекати вивід, 0..15, за замовч. 2"}, required: []string{"server_id", "command"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			if err := g.require("server_id", "command"); err != nil {
				return nil, err
			}
			c := g.str("command")
			if len(c) > 400 || strings.ContainsAny(c, "\r\n") {
				return nil, errors.New("команда має бути одним рядком до 400 символів")
			}
			sid := g.str("server_id")
			wait := g.integer("wait_seconds", 2)
			if wait < 0 || wait > 15 {
				wait = 2
			}
			last := a.GetServerConsolePage(sid, -1, 1, "", "", 0).LastID
			if err := a.SendServerCommand(sid, strings.TrimPrefix(c, "/")); err != nil {
				return nil, err
			}
			if wait == 0 {
				return "команду надіслано", nil
			}
			time.Sleep(time.Duration(wait) * time.Second)
			pg := a.GetServerConsolePage(sid, 0, 60, "", "", last)
			out := make([]string, 0, len(pg.Lines))
			for _, l := range pg.Lines {
				out = append(out, l.Text)
			}
			return map[string]any{"sent": c, "output": out}, nil
		},
	})
	regTool(aiTool{
		name: "create_server", desc: "Створити сервер (paper/purpur/fabric/forge/… — див. get_server_cores). Прийняття EULA Mojang — рішення користувача: accept_eula=true ставте лише якщо користувач прямо погодився.",
		props:    map[string]string{"name": "назва", "core": "ядро", "mc_version": "версія Minecraft", "ram_mb": "(int) RAM", "port": "(int) 0 = автопідбір", "accept_eula": "(bool) користувач прийняв EULA Mojang"},
		required: []string{"name", "core", "mc_version"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			if err := g.require("name", "core", "mc_version"); err != nil {
				return nil, err
			}
			v, err := a.CreateServer(servers.NewServerRequest{Name: g.str("name"), Core: g.str("core"), MCVersion: g.str("mc_version"),
				RAMMB: g.integer("ram_mb", 4096), Port: g.integer("port", 0), AcceptEULA: g.boolean("accept_eula", false)})
			if err == nil {
				a.aiProvenanceMark("server", v.ID)
			}
			return v, err
		},
	})
	regTool(aiTool{
		name: "get_server_cores", desc: "Доступні ядра серверів для версії Minecraft і їх версії.",
		props: map[string]string{"mc_version": "версія Minecraft", "core": "якщо задано — версії цього ядра"}, required: []string{"mc_version"},
		run: func(a *App, g argMap) (any, error) {
			if c := g.str("core"); c != "" {
				return a.GetServerCoreVersions(c, g.str("mc_version"))
			}
			return a.GetServerAvailableCores(g.str("mc_version")), nil
		},
	})
	regTool(aiTool{
		name: "set_server_config", desc: "Змінити RAM / назву / порт / Java-аргументи сервера (сервер краще зупинити). Передавайте лише потрібні поля.",
		props:    map[string]string{"server_id": "id сервера", "ram_mb": "(int) RAM", "name": "нова назва", "port": "(int) порт", "jvm_args": "JVM-аргументи (білий список)"},
		required: []string{"server_id"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			if g.str("server_id") == "" {
				return nil, errors.New("server_id обовʼязковий")
			}
			var patch servers.ServerConfigPatch
			if _, ok := g["ram_mb"]; ok {
				n := g.integer("ram_mb", 0)
				if n < 512 || n > 65536 {
					return nil, errors.New("ram_mb: 512..65536")
				}
				patch.RAMMB = &n
			}
			if v := g.str("name"); v != "" {
				patch.Name = &v
			}
			if _, ok := g["port"]; ok {
				n := g.integer("port", 0)
				if n < 1024 || n > 65535 {
					return nil, errors.New("port: 1024..65535")
				}
				patch.Port = &n
			}
			if _, ok := g["jvm_args"]; ok {
				raw := g.str("jvm_args")
				if _, rej := jvmargs.SanitizeUser(raw); len(rej) > 0 {
					return nil, fmt.Errorf("відхилені аргументи: %v", rej)
				}
				patch.JvmArgs = &raw
			}
			return a.SaveServerConfig(g.str("server_id"), patch)
		},
	})
	regTool(aiTool{
		name: "set_server_properties", desc: "Змінити ключі server.properties (view-distance, max-players, motd, difficulty …). Набір {\"key\":\"value\"} у JSON-рядку. Сервер краще перезапустити після.",
		props: map[string]string{"server_id": "id сервера", "updates_json": "JSON-обʼєкт ключ→значення, напр. {\"view-distance\":\"8\"}"}, required: []string{"server_id", "updates_json"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			m := map[string]any{}
			if err := json.Unmarshal([]byte(g.str("updates_json")), &m); err != nil || len(m) == 0 || len(m) > 40 {
				return nil, errors.New("updates_json має бути JSON-обʼєктом з 1..40 ключів")
			}
			var ups []servers.PropUpdate
			for k, v := range m {
				if strings.ContainsAny(k, "\r\n=#") || strings.ContainsAny(fmt.Sprint(v), "\r\n") {
					return nil, fmt.Errorf("недопустимий ключ/значення: %q", k)
				}
				ups = append(ups, servers.PropUpdate{Key: k, Value: fmt.Sprint(v)})
			}
			return "ok", a.SaveServerProperties(g.str("server_id"), ups)
		},
	})
	regTool(aiTool{
		name: "install_server_content", desc: "Встановити мод/плагін на сервер з Modrinth/CurseForge.",
		props:    map[string]string{"server_id": "id сервера", "source": "modrinth | curseforge", "project_id": "id проєкту", "kind": "mods | plugins", "version_id": "порожньо — найкраща сумісна"},
		required: []string{"server_id", "source", "project_id", "kind"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			if err := g.require("server_id", "source", "project_id", "kind"); err != nil {
				return nil, err
			}
			if k := g.str("kind"); k != "mods" && k != "plugins" {
				return nil, errors.New("kind: mods або plugins")
			}
			return a.InstallServerBrowserVersion(g.str("server_id"), g.str("source"), g.str("project_id"), g.str("version_id"), g.str("kind"), false)
		},
	})
	regTool(aiTool{
		name: "backup_server_world", desc: "Створити бекап світу сервера (перед змінами).",
		props: map[string]string{"server_id": "id сервера", "world": "назва світу з list_server_worlds"}, required: []string{"server_id", "world"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			if err := g.require("server_id", "world"); err != nil {
				return nil, err
			}
			sid := g.str("server_id")
			before := map[string]bool{}
			if l, err := a.srvAPI.ListWorldBackups(sid, g.str("world")); err == nil {
				for _, b := range l {
					before[b.ID] = true
				}
			}
			res, err := a.CreateWorldBackup(sid, g.str("world"))
			if err == nil {
				if l, e := a.srvAPI.ListWorldBackups(sid, g.str("world")); e == nil {
					for _, b := range l {
						if !before[b.ID] {
							a.aiProvenanceMark("server-backup", sid+"|"+b.ID)
						}
					}
				}
			}
			return res, err
		},
	})
	regTool(aiTool{
		name: "write_server_config_file", desc: "Перезаписати ІСНУЮЧИЙ текстовий конфіг сервера повним вмістом (.yml .yaml .json .toml .properties .conf .txt). Сервер має бути зупинений.",
		props: map[string]string{"server_id": "id сервера", "path": "відносний шлях", "content": "повний новий текст"}, required: []string{"server_id", "path", "content"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			if err := g.require("server_id", "path"); err != nil {
				return nil, err
			}
			if !aiConfigExtOK(g.str("path")) {
				return nil, errors.New("дозволені лише текстові конфіги")
			}
			root, err := a.serverRootDir(g.str("server_id"))
			if err != nil {
				return nil, err
			}
			p, err := resolveRelPath(root, g.str("path"))
			if err != nil {
				return nil, err
			}
			if old, err := os.ReadFile(p); err == nil {
				_ = os.WriteFile(p+".ai-bak", old, 0o644)
			}
			content, _ := g["content"].(string)
			return "ok", a.WriteServerTextFile(g.str("server_id"), g.str("path"), content)
		},
	})
}
