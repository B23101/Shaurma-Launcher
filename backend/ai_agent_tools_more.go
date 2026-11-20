package main

// ai_agent_tools_more.go — ще інструменти ШІ-агента: шляхи, міграція модів між
// збірками, знімки світів з позначкою автора, аналітика й розміри, стиль збірки,
// сортування й групування, мережа проксі (Velocity) та файли серверів.
//
// Ті самі межі, що й в ai_agent_api.go: нічого не видаляється (розформування
// папки лише прибирає групування, самі збірки лишаються), жодних акаунтів/ключів.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"shaurma-launcher-backend/internal/model"
	"shaurma-launcher-backend/internal/servers"
)

var (
	aiHexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	aiTablerIc = regexp.MustCompile(`^ti-[a-z0-9-]{1,40}$`)
)

func aiCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func aiDirSize(root string) int64 {
	var n int64
	_ = filepath.WalkDir(root, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, e := d.Info(); e == nil {
				n += info.Size()
			}
		}
		return nil
	})
	return n
}

// aiPinned повертає закріплені збірки з розкладки (тип у карті — []string).
func aiPinned(a *App) []string {
	if p, ok := a.GetPackLayout()["pinned"].([]string); ok {
		return p
	}
	return []string{}
}

func init() {
	// ═══ Вхід у світ / на сервер при запуску (для тестів продуктивності) ═══
	regTool(aiTool{
		name: "list_build_worlds", desc: "Одиночні світи збірки (теки saves/) — з них можна обрати світ для автовходу.",
		props: map[string]string{"pack_id": "id збірки"}, required: []string{"pack_id"},
		run: func(a *App, g argMap) (any, error) {
			if g.str("pack_id") == "" {
				return nil, errPackID()
			}
			return a.ListInstanceWorlds(g.str("pack_id")), nil
		},
	})
	regTool(aiTool{
		name: "set_build_autojoin", desc: "Автовхід при запуску збірки (офіційний quick-play Minecraft 1.20+): у одиночний світ, на сервер або вимкнути. Після тесту поверніть mode=off.",
		props:    map[string]string{"pack_id": "id збірки", "mode": "world | server | off", "world": "тека світу з list_build_worlds (для world)", "server": "host[:port] (для server)"},
		required: []string{"pack_id", "mode"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			id := g.str("pack_id")
			if id == "" {
				return nil, errPackID()
			}
			cfg := a.GetInstanceConfig(id)
			cfg.ID = id
			switch g.str("mode") {
			case "off":
				cfg.AutoJoinEnabled = false
			case "world":
				w := g.str("world")
				found := false
				for _, x := range a.ListInstanceWorlds(id) {
					found = found || x == w
				}
				if !found {
					return nil, errors.New("світ не знайдено серед list_build_worlds")
				}
				cfg.AutoJoinEnabled, cfg.AutoJoinType, cfg.AutoJoinWorld = true, "world", w
			case "server":
				s := g.str("server")
				if s == "" || len(s) > 255 || strings.ContainsAny(s, " \r\n\"'") {
					return nil, errors.New("server: host[:port] без пробілів")
				}
				cfg.AutoJoinEnabled, cfg.AutoJoinType, cfg.AutoJoinServer = true, "server", s
			default:
				return nil, errors.New("mode: world | server | off")
			}
			return "ok", a.SaveInstanceConfig(cfg)
		},
	})

	// ═══ Шляхи ═══
	regTool(aiTool{
		name: "get_build_paths", desc: "Де на диску лежить збірка: коренева тека, .minecraft, mods, config, saves, logs, crash-reports, resourcepacks, shaderpacks.",
		props: map[string]string{"pack_id": "id збірки"}, required: []string{"pack_id"},
		run: func(a *App, g argMap) (any, error) {
			id := g.str("pack_id")
			if id == "" {
				return nil, errPackID()
			}
			gd := a.gameDir(id)
			return map[string]string{"pack_dir": a.packDir(id, ""), "game_dir": gd, "mods": filepath.Join(gd, "mods"),
				"config": filepath.Join(gd, "config"), "saves": filepath.Join(gd, "saves"), "logs": filepath.Join(gd, "logs"),
				"crash_reports": filepath.Join(gd, "crash-reports"), "resourcepacks": filepath.Join(gd, "resourcepacks"),
				"shaderpacks": filepath.Join(gd, "shaderpacks")}, nil
		},
	})
	regTool(aiTool{
		name: "get_server_paths", desc: "Де на диску лежить сервер: коренева тека, логи, плагіни/моди, server.properties.",
		props: map[string]string{"server_id": "id сервера"}, required: []string{"server_id"},
		run: func(a *App, g argMap) (any, error) {
			root, err := a.serverRootDir(g.str("server_id"))
			if err != nil {
				return nil, err
			}
			return map[string]string{"server_dir": root, "logs": filepath.Join(root, "logs"), "plugins": filepath.Join(root, "plugins"),
				"mods": filepath.Join(root, "mods"), "properties": filepath.Join(root, "server.properties"),
				"crash_reports": filepath.Join(root, "crash-reports")}, nil
		},
	})
	regTool(aiTool{
		name: "get_launcher_paths", desc: "Теки лаунчера: дані, збірки (instances), логи.",
		run: func(a *App, g argMap) (any, error) {
			return map[string]string{"data_dir": a.GetAppDir(), "instances": a.cfg.InstancesDir(), "logs": a.cfg.LogsDir()}, nil
		},
	})

	// ═══ Міграція модів між збірками ═══
	regTool(aiTool{
		name: "migrate_mods", desc: "Перенести моди з однієї збірки в іншу з підбором сумісних версій під версію MC і лоадер цілі. Спершу dry_run=true (за замовч.): показує, що знайдено / не знайдено / вже є.",
		props:    map[string]string{"source_id": "збірка-джерело", "target_id": "збірка-ціль", "dry_run": "(bool) true — лише звіт, нічого не ставити"},
		required: []string{"source_id", "target_id"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			if err := g.require("source_id", "target_id"); err != nil {
				return nil, err
			}
			if g.str("source_id") == g.str("target_id") {
				return nil, errors.New("source_id і target_id збігаються")
			}
			tcfg := a.GetInstanceConfig(g.str("target_id"))
			rep, err := a.AnalyzeModMigration(g.str("source_id"), g.str("target_id"), tcfg.MCVersion, tcfg.Loader)
			if err != nil {
				return nil, err
			}
			summary := map[string]any{"source": rep.SourcePackName, "target_mc": rep.TargetMCVersion, "target_loader": rep.TargetLoader,
				"found": rep.Found, "not_found": rep.NotFound, "already": rep.Already, "needs_update": rep.NeedsUpdate}
			var missing []string
			for _, it := range rep.Items {
				if it.Status == "not-found" {
					missing = append(missing, it.SourceName)
				}
			}
			summary["not_found_mods"] = missing
			if g.boolean("dry_run", true) {
				summary["dry_run"] = true
				return summary, nil
			}
			var todo []model.MigrationItem
			for _, it := range rep.Items {
				if it.Status == "found" || it.Status == "update" {
					todo = append(todo, it)
				}
			}
			if len(todo) == 0 {
				summary["migrated"] = 0
				return summary, nil
			}
			summary["result"] = a.MigrateMods(g.str("target_id"), todo)
			return summary, nil
		},
	})

	regTool(aiTool{
		name: "delete_ai_created_build", desc: "Видалити збірку, яку створив САМ агент (напр. помилково або тестову). Збірки користувача й каталожні видалити неможливо. Потрібно підтвердження: confirm_name = точна назва збірки.",
		props:    map[string]string{"pack_id": "id збірки (з list_builds, created_by не user)", "confirm_name": "точна назва збірки"},
		required: []string{"pack_id", "confirm_name"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			id := g.str("pack_id")
			if id == "" {
				return nil, errPackID()
			}
			if a.aiProvenanceOf("build", id) == "" {
				return nil, errors.New("відмовлено: цю збірку створив не ШІ-агент, видаляти її може лише користувач")
			}
			p, err := a.GetCustomPack(id)
			if err != nil {
				return nil, errors.New("збірку не знайдено серед кастомних")
			}
			if g.str("confirm_name") != p.Name {
				return nil, errors.New("confirm_name не збігається з назвою збірки")
			}
			if err := a.DeletePack(id); err != nil {
				return nil, err
			}
			a.aiProvenanceForget("build", id)
			a.emit("builds:changed", id)
			return "видалено: " + p.Name, nil
		},
	})

	// ═══ Знімки світів і бекапи (з автором) ═══
	regTool(aiTool{
		name: "list_world_snapshots", desc: "Знімки світів збірки: назва, дата, розмір і хто створив (user чи ШІ-агент).",
		props: map[string]string{"pack_id": "id збірки"}, required: []string{"pack_id"},
		run: func(a *App, g argMap) (any, error) {
			id := g.str("pack_id")
			if id == "" {
				return nil, errPackID()
			}
			names, err := a.ListPackWorldSnapshots(id)
			if err != nil {
				return nil, err
			}
			base := filepath.Join(a.packDir(id, ""), "world-snapshots")
			out := []map[string]any{}
			for _, n := range names {
				by := a.aiProvenanceOf("world-snapshot", id+"|"+n)
				if by == "" {
					by = "user"
				}
				e := map[string]any{"snapshot": n, "created_by": by, "size_mb": aiDirSize(filepath.Join(base, n)) >> 20}
				if t, err := time.Parse("20060102-150405.000000000", n); err == nil {
					e["created"] = t.Local().Format(time.RFC3339)
				}
				out = append(out, e)
			}
			return out, nil
		},
	})
	regTool(aiTool{
		name: "list_server_backups", desc: "Бекапи світу сервера з розміром, датою й автором (created_by: user / auto / ШІ-агент).",
		props: map[string]string{"server_id": "id сервера", "world": "назва світу"}, required: []string{"server_id", "world"},
		run: func(a *App, g argMap) (any, error) {
			if err := g.require("server_id", "world"); err != nil {
				return nil, err
			}
			list, err := a.ListWorldBackups(g.str("server_id"), g.str("world"))
			out := []map[string]any{}
			for _, b := range list {
				by := b.CreatedBy
				switch {
				case by != "":
				case b.Auto:
					by = "auto"
				default:
					by = "user"
				}
				out = append(out, map[string]any{"id": b.ID, "name": b.Name, "size_mb": b.SizeMB, "created": b.Created, "created_by": by})
			}
			return out, err
		},
	})

	// ═══ Аналітика та розміри ═══
	regTool(aiTool{
		name: "get_playtime_analytics", desc: "Аналітика ігрового часу за періодами (7 днів, тижні, місяці, роки), сесії, топ збірок. Показує, якими збірками справді користуються.",
		props: map[string]string{"account_name": "нік гравця (порожньо — усі)"},
		run:   func(a *App, g argMap) (any, error) { return a.GetAnalytics(g.str("account_name")), nil },
	})
	regTool(aiTool{
		name: "get_build_sizes", desc: "Скільки місця займає кожна збірка (від найбільшої), скільки годин награно й загальне використання диска лаунчером.",
		run: func(a *App, g argMap) (any, error) {
			packs := a.GetStoragePacks()
			sort.Slice(packs, func(i, j int) bool { return packs[i].Bytes > packs[j].Bytes })
			play := a.GetPlaytime()
			var total int64
			rows := []map[string]any{}
			for _, p := range packs {
				total += p.Bytes
				rows = append(rows, map[string]any{"id": p.ID, "name": p.Name, "size_mb": p.Bytes >> 20,
					"played_hours": float64(play[p.ID]/36) / 100})
			}
			return map[string]any{"builds": rows, "total_mb": total >> 20, "storage": a.GetStorageUsage()}, nil
		},
	})

	// ═══ Стиль збірки ═══
	regTool(aiTool{
		name: "set_build_style", desc: "Оформлення кастомної збірки: назва, опис, колір картки (#RRGGBB), іконка-пресет Tabler (напр. ti-sword, ti-flame, ti-cpu). Передавайте лише змінювані поля.",
		props:    map[string]string{"pack_id": "id збірки", "name": "нова назва", "description": "опис", "color": "#RRGGBB", "icon": "ti-… пресет"},
		required: []string{"pack_id"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			id := g.str("pack_id")
			if id == "" {
				return nil, errPackID()
			}
			p, err := a.GetCustomPack(id)
			if err != nil {
				return nil, errors.New("стиль можна змінювати лише у кастомних збірок")
			}
			f := CustomPackFormParams{Name: p.Name, Description: p.Description, Icon: p.Icon, Color: p.Color, MCVersion: p.MCVersion,
				Loader: string(p.Loader), LoaderVersion: p.LoaderVersion, UseCustomRAM: p.UseCustomRAM, MinRAMMB: p.MinRAMMB,
				MaxRAMMB: p.MaxRAMMB, UseSeparateJava: p.UseSeparateJava, JavaPath: p.JavaPath}
			if v := g.str("name"); v != "" {
				if len([]rune(v)) > 60 {
					return nil, errors.New("назва завдовга")
				}
				f.Name = v
			}
			if d, ok := g["description"].(string); ok {
				if len([]rune(d)) > 500 {
					return nil, errors.New("опис завдовгий")
				}
				f.Description = d
			}
			if v := g.str("color"); v != "" {
				if !aiHexColor.MatchString(v) {
					return nil, errors.New("color: формат #RRGGBB")
				}
				f.Color = v
			}
			if v := g.str("icon"); v != "" {
				if !aiTablerIc.MatchString(v) {
					return nil, errors.New("icon: формат ti-назва")
				}
				f.Icon, f.IconChanged = v, true
			}
			np, err := a.UpdateCustomPack(id, f)
			if err != nil {
				return nil, err
			}
			return map[string]any{"id": np.ID, "name": np.Name, "color": np.Color, "icon": np.Icon}, nil
		},
	})

	// ═══ Сортування й групування ═══
	regTool(aiTool{
		name: "get_build_layout", desc: "Порядок збірок у списку, закріплені та папки (групи) збірок.",
		run: func(a *App, g argMap) (any, error) {
			return map[string]any{"layout": a.GetPackLayout(), "folders": a.GetPackFolders()}, nil
		},
	})
	regTool(aiTool{
		name: "sort_builds", desc: "Відсортувати збірки у списку: за назвою, часом гри (найбільше першими), розміром на диску або версією Minecraft. Закріплені лишаються закріпленими.",
		props:    map[string]string{"by": "name | playtime | size | mc_version", "descending": "(bool) за замовч. true для playtime/size, інакше false"},
		required: []string{"by"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			by := g.str("by")
			if by != "name" && by != "playtime" && by != "size" && by != "mc_version" {
				return nil, errors.New("by: name | playtime | size | mc_version")
			}
			desc := g.boolean("descending", by == "playtime" || by == "size")
			play := a.GetPlaytime()
			size := map[string]int64{}
			for _, p := range a.GetStoragePacks() {
				size[p.ID] = p.Bytes
			}
			type row struct {
				id, name, mc string
			}
			var rows []row
			for _, v := range a.GetBuilds() {
				rows = append(rows, row{v.Build.ID, strings.ToLower(v.Build.Name), v.Build.MCVersion})
			}
			cmp := func(x, y row) int { // -1, 0, 1 у зростальному порядку
				switch by {
				case "playtime":
					return cmpInt64(play[x.id], play[y.id])
				case "size":
					return cmpInt64(size[x.id], size[y.id])
				case "mc_version":
					return strings.Compare(x.mc, y.mc)
				}
				return strings.Compare(x.name, y.name)
			}
			sort.SliceStable(rows, func(i, j int) bool {
				c := cmp(rows[i], rows[j])
				if desc {
					return c > 0
				}
				return c < 0
			})
			order := make([]string, 0, len(rows))
			for _, r := range rows {
				order = append(order, r.id)
			}
			if err := a.SavePackLayout(order, aiPinned(a)); err != nil {
				return nil, err
			}
			a.emit("layout:changed", nil)
			return map[string]any{"order": order}, nil
		},
	})
	regTool(aiTool{
		name: "set_build_order", desc: "Задати порядок збірок явно (id через кому) і, за потреби, закріплені.",
		props:    map[string]string{"order": "id збірок через кому в потрібному порядку", "pinned": "id закріплених через кому (не передавайте — лишити як є)"},
		required: []string{"order"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			order := aiCSV(g.str("order"))
			if len(order) == 0 {
				return nil, errors.New("order порожній")
			}
			pinned := aiPinned(a)
			if _, ok := g["pinned"]; ok {
				pinned = aiCSV(g.str("pinned"))
			}
			if err := a.SavePackLayout(order, pinned); err != nil {
				return nil, err
			}
			a.emit("layout:changed", nil)
			return "ok", nil
		},
	})
	regTool(aiTool{
		name: "set_build_folder", desc: "Створити або оновити папку (групу) кастомних збірок. Без folder_id створюється нова. Збірка може бути лише в одній папці.",
		props: map[string]string{"folder_id": "id існуючої папки (порожньо — нова)", "name": "назва папки", "color": "#RRGGBB", "icon": "ti-…",
			"pack_ids": "id збірок у папці через кому (повний список)"},
		required: []string{"name", "pack_ids"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			name := g.str("name")
			if name == "" || len([]rune(name)) > 40 {
				return nil, errors.New("name: 1..40 символів")
			}
			f := model.PackFolder{ID: g.str("folder_id"), Name: name, PackIds: aiCSV(g.str("pack_ids"))}
			if v := g.str("color"); v != "" {
				if !aiHexColor.MatchString(v) {
					return nil, errors.New("color: формат #RRGGBB")
				}
				f.Color = v
			}
			if v := g.str("icon"); v != "" {
				if !aiTablerIc.MatchString(v) {
					return nil, errors.New("icon: формат ti-назва")
				}
				f.Icon = v
			}
			if f.ID == "" {
				f.ID = fmt.Sprintf("f%d", time.Now().UnixMilli())
			}
			moving := map[string]bool{}
			for _, p := range f.PackIds {
				moving[p] = true
			}
			var out []model.PackFolder
			replaced := false
			for _, e := range a.GetPackFolders() {
				if e.ID == f.ID {
					out, replaced = append(out, f), true
					continue
				}
				var keep []string
				for _, p := range e.PackIds {
					if !moving[p] {
						keep = append(keep, p)
					}
				}
				e.PackIds = keep
				out = append(out, e)
			}
			if !replaced {
				out = append(out, f)
			}
			if err := a.SavePackFolders(out); err != nil {
				return nil, err
			}
			a.emit("layout:changed", nil)
			return map[string]any{"folder_id": f.ID}, nil
		},
	})
	regTool(aiTool{
		name: "dissolve_build_folder", desc: "Розформувати папку збірок: прибирає лише групування, самі збірки та їхні файли лишаються.",
		props: map[string]string{"folder_id": "id папки"}, required: []string{"folder_id"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			id := g.str("folder_id")
			var out []model.PackFolder
			found := false
			for _, e := range a.GetPackFolders() {
				if e.ID == id {
					found = true
					continue
				}
				out = append(out, e)
			}
			if !found {
				return nil, errors.New("папку не знайдено")
			}
			if err := a.SavePackFolders(out); err != nil {
				return nil, err
			}
			a.emit("layout:changed", nil)
			return "ok", nil
		},
	})

	// ═══ Проксі-мережа (Velocity) і файли серверів ═══
	regTool(aiTool{
		name: "network_get", desc: "Мережа проксі (Velocity): сам проксі, учасники-сервери з ролями, зовнішні сервери, налаштування.",
		props: map[string]string{"proxy_id": "id сервера-проксі"}, required: []string{"proxy_id"},
		run: func(a *App, g argMap) (any, error) { return a.NetworkGetView(g.str("proxy_id")) },
	})
	regTool(aiTool{
		name: "network_diagnose", desc: "Діагностика мережі проксі: forwarding, порти, secret, відсутні сервери.",
		props: map[string]string{"proxy_id": "id проксі"}, required: []string{"proxy_id"},
		run: func(a *App, g argMap) (any, error) { return a.NetworkDiagnose(g.str("proxy_id")) },
	})
	regTool(aiTool{
		name: "network_config_text", desc: "Згенерований velocity.toml (лише читання).",
		props: map[string]string{"proxy_id": "id проксі"}, required: []string{"proxy_id"},
		run: func(a *App, g argMap) (any, error) { return a.NetworkConfigText(g.str("proxy_id")) },
	})
	regTool(aiTool{
		name: "network_save_members", desc: "Зберегти склад мережі: JSON-масив учасників так, як віддає network_get (members). Змінюйте лише inNetwork/order/delaySec/alias/autoStart/startsNetwork.",
		props: map[string]string{"proxy_id": "id проксі", "members_json": "JSON-масив members"}, required: []string{"proxy_id", "members_json"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			var members []servers.NetworkMemberView
			if err := json.Unmarshal([]byte(g.str("members_json")), &members); err != nil || len(members) == 0 || len(members) > 100 {
				return nil, errors.New("members_json: некоректний JSON-масив")
			}
			for _, m := range members {
				if m.DelaySec < 0 || m.DelaySec > 600 || len(m.Alias) > 40 || strings.ContainsAny(m.Alias, "\r\n\"=#[]") {
					return nil, fmt.Errorf("недопустимі значення в учасника %q", m.ServerID)
				}
			}
			return "ok", a.NetworkSave(g.str("proxy_id"), members)
		},
	})
	regTool(aiTool{
		name: "network_save_settings", desc: "Зберегти налаштування проксі: JSON як у network_get → settings (proxyPort, proxy{motd, forwardingMode, onlineMode, …}).",
		props: map[string]string{"proxy_id": "id проксі", "settings_json": "JSON налаштувань"}, required: []string{"proxy_id", "settings_json"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			var s servers.NetworkSettings
			if err := json.Unmarshal([]byte(g.str("settings_json")), &s); err != nil {
				return nil, errors.New("settings_json: некоректний JSON")
			}
			if s.ProxyPort < 1024 || s.ProxyPort > 65535 || strings.ContainsAny(s.Proxy.Motd, "\r\n\x00") || len(s.Proxy.Motd) > 400 {
				return nil, errors.New("недопустимий порт або motd")
			}
			switch s.Proxy.ForwardingMode {
			case "modern", "legacy", "bungeeguard", "none":
			default:
				return nil, errors.New("forwardingMode: modern | legacy | bungeeguard | none")
			}
			return "ok", a.NetworkSaveSettings(g.str("proxy_id"), s)
		},
	})
	regTool(aiTool{
		name: "network_repair", desc: "Автоматично виправити проблеми мережі проксі, які network_diagnose позначив як fixable.",
		props: map[string]string{"proxy_id": "id проксі"}, required: []string{"proxy_id"}, write: true,
		run: func(a *App, g argMap) (any, error) { return "ok", a.NetworkRepair(g.str("proxy_id")) },
	})
	regTool(aiTool{
		name: "network_start", desc: "Запустити всю мережу (проксі + учасники за порядком і затримками).",
		props: map[string]string{"proxy_id": "id проксі"}, required: []string{"proxy_id"}, write: true,
		run: func(a *App, g argMap) (any, error) { return "ok", a.NetworkStart(g.str("proxy_id")) },
	})
	regTool(aiTool{
		name: "network_stop", desc: "Зупинити мережу проксі.",
		props: map[string]string{"proxy_id": "id проксі"}, required: []string{"proxy_id"}, write: true,
		run: func(a *App, g argMap) (any, error) { return "ok", a.NetworkStop(g.str("proxy_id")) },
	})
	regTool(aiTool{
		name: "list_server_files", desc: "Вміст теки сервера (файли/папки) для пошуку конфігів: plugins/…, config/… тощо.",
		props: map[string]string{"server_id": "id сервера", "path": "відносна тека (порожньо — коренева)"}, required: []string{"server_id"},
		run: func(a *App, g argMap) (any, error) { return a.ListServerFiles(g.str("server_id"), g.str("path")) },
	})
}

func cmpInt64(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
