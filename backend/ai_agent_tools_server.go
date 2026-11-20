package main

// ai_agent_tools_server.go — плагіни/моди серверів і зв'язка «збірка + сервер».
//
//  • Мод-менеджер лаунчера обслуговує і сервери: ID таргету "srv:<id>" — mods/ сервера,
//    "srvp:<id>" — plugins/. Тут — зручні обгортки, щоб агенту не треба було знати префікси.
//  • create_server_for_build створює сервер під збірку (ядро за лоадером, та сама версія гри),
//    переносить моди, відсікаючи клієнтські (environment=client у метаданих jar), і зв'язує їх.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"shaurma-launcher-backend/internal/model"
	"shaurma-launcher-backend/internal/servers"
)

// aiServerTarget повертає ID таргету мод-менеджера для сервера: kind = mods | plugins.
func (a *App) aiServerTarget(serverID, kind string) (string, error) {
	if serverID == "" {
		return "", errors.New("server_id обовʼязковий")
	}
	if _, err := a.srvAPI.GetServer(serverID); err != nil {
		return "", fmt.Errorf("сервер не знайдено: %s", serverID)
	}
	switch kind {
	case "plugins":
		return modTargetServerPlugins + serverID, nil
	case "mods", "":
		return modTargetServerMods + serverID, nil
	}
	return "", errors.New("kind: mods | plugins")
}

func aiCopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// aiServerCoreFor підбирає ядро сервера за лоадером збірки серед доступних для версії гри.
func aiServerCoreFor(loader string, avail []string) (string, error) {
	want := []string{strings.ToLower(loader)}
	if loader == "" || strings.EqualFold(loader, "vanilla") {
		want = []string{"vanilla", "paper"}
	}
	for _, w := range want {
		for _, c := range avail {
			if strings.EqualFold(c, w) {
				return c, nil
			}
		}
	}
	return "", fmt.Errorf("для версії гри немає ядра сервера під лоадер %q (доступні: %s)", loader, strings.Join(avail, ", "))
}

func init() {
	regTool(aiTool{
		name: "list_server_mods_or_plugins", desc: "Моди (kind=mods) або плагіни (kind=plugins) сервера з версіями, увімкненістю, середовищем і проблемами.",
		props: map[string]string{"server_id": "id сервера", "kind": "mods | plugins (за замовч. mods)"}, required: []string{"server_id"},
		run: func(a *App, g argMap) (any, error) {
			t, err := a.aiServerTarget(g.str("server_id"), g.str("kind"))
			if err != nil {
				return nil, err
			}
			return a.ScanModsFull(t), nil
		},
	})
	regTool(aiTool{
		name: "check_server_updates", desc: "Які моди/плагіни сервера мають нові версії.",
		props: map[string]string{"server_id": "id сервера", "kind": "mods | plugins"}, required: []string{"server_id"},
		run: func(a *App, g argMap) (any, error) {
			t, err := a.aiServerTarget(g.str("server_id"), g.str("kind"))
			if err != nil {
				return nil, err
			}
			mc, loader := a.modTargetEnv(t)
			return a.CheckModUpdates(t, mc, loader), nil
		},
	})
	regTool(aiTool{
		name: "update_server_content", desc: "Оновити всі моди/плагіни сервера до новіших версій (з резервною копією). Сервер краще зупинити; після — start_server і перевірка консолі.",
		props: map[string]string{"server_id": "id сервера", "kind": "mods | plugins"}, required: []string{"server_id"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			t, err := a.aiServerTarget(g.str("server_id"), g.str("kind"))
			if err != nil {
				return nil, err
			}
			mc, loader := a.modTargetEnv(t)
			return a.UpdateAllMods(t, mc, loader, true), nil
		},
	})
	regTool(aiTool{
		name: "toggle_server_content", desc: "Увімкнути/вимкнути мод або плагін сервера (оборотно; видалення недоступне). Корисно, коли клієнтський мод поклав сервер.",
		props:    map[string]string{"server_id": "id сервера", "kind": "mods | plugins", "file_name": "файл з list_server_mods_or_plugins", "enabled": "(bool) true — увімкнути"},
		required: []string{"server_id", "file_name"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			t, err := a.aiServerTarget(g.str("server_id"), g.str("kind"))
			if err != nil {
				return nil, err
			}
			if g.str("file_name") == "" {
				return nil, errors.New("file_name обовʼязковий")
			}
			if a.srvAPI.Manager().IsRunning(g.str("server_id")) {
				return nil, errors.New("спершу зупиніть сервер (stop_server)")
			}
			return "ok", a.ToggleMod(t, g.str("file_name"), g.boolean("enabled", true))
		},
	})
	regTool(aiTool{
		name: "wait_for_server_console", desc: "Чекати (до 120 с) появи тексту в консолі сервера — напр. «Done» після start_server. Повертає знайдені рядки або таймаут із станом сервера.",
		props: map[string]string{"server_id": "id сервера", "contains": "підрядок", "timeout_sec": "(int) 1..120, за замовч. 90"}, required: []string{"server_id", "contains"},
		run: func(a *App, g argMap) (any, error) {
			if err := g.require("server_id", "contains"); err != nil {
				return nil, err
			}
			to := g.integer("timeout_sec", 90)
			if to < 1 || to > 120 {
				to = 90
			}
			deadline := time.Now().Add(time.Duration(to) * time.Second)
			for {
				pg := a.GetServerConsolePage(g.str("server_id"), 0, 10, "", g.str("contains"), 0)
				if len(pg.Lines) > 0 {
					return map[string]any{"found": true, "lines": pg.Lines}, nil
				}
				st := a.GetServerStatus(g.str("server_id"))
				if !a.srvAPI.Manager().IsRunning(g.str("server_id")) && time.Until(deadline) < time.Duration(to-8)*time.Second {
					return map[string]any{"found": false, "server_stopped": true, "status": st,
						"hint": "сервер не працює: read_server_crash, get_server_console level=error"}, nil
				}
				if time.Now().After(deadline) {
					return map[string]any{"found": false, "status": st}, nil
				}
				time.Sleep(2 * time.Second)
			}
		},
	})

	regTool(aiTool{
		name: "create_server_for_build",
		desc: "Створити сервер ПІД ЗБІРКУ: та сама версія Minecraft, ядро за лоадером збірки (fabric/forge/neoforge/quilt), моди збірки копіюються на сервер без клієнтських (environment=client), сервер зв'язується зі збіркою. Далі: start_server → wait_for_server_console(\"Done\") → start_world_test(server=127.0.0.1:порт). Прийняття EULA Mojang — рішення користувача: accept_eula=true лише після його згоди.",
		props: map[string]string{"pack_id": "id збірки", "name": "назва сервера (за замовч. «<збірка> · сервер»)", "ram_mb": "(int) RAM, за замовч. 4096", "port": "(int) 0 = автопідбір",
			"copy_mods": "(bool) переносити моди збірки (за замовч. true)", "accept_eula": "(bool) користувач прийняв EULA Mojang"},
		required: []string{"pack_id"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			id := g.str("pack_id")
			if id == "" {
				return nil, errPackID()
			}
			cfg := a.GetInstanceConfig(id)
			if cfg.MCVersion == "" {
				return nil, errors.New("не вдалося визначити версію Minecraft збірки")
			}
			core, err := aiServerCoreFor(cfg.Loader, a.GetServerAvailableCores(cfg.MCVersion))
			if err != nil {
				return nil, err
			}
			name := g.str("name")
			if name == "" {
				base := cfg.Name
				if base == "" {
					base = id
				}
				name = base + " · сервер"
			}
			ram := g.integer("ram_mb", 4096)
			if ram < 1024 || ram > 65536 {
				return nil, errors.New("ram_mb: 1024..65536")
			}
			a.aiEditBegin(id, "створення сервера під збірку")
			view, err := a.CreateServer(servers.NewServerRequest{Name: name, Core: core, MCVersion: cfg.MCVersion,
				RAMMB: ram, Port: g.integer("port", 0), AcceptEULA: g.boolean("accept_eula", false)})
			if err != nil {
				return nil, err
			}
			a.aiProvenanceMark("server", view.ID)

			res := map[string]any{"server_id": view.ID, "name": view.Name, "core": view.Core, "mc_version": view.MCVersion, "port": view.Port,
				"address": fmt.Sprintf("127.0.0.1:%d", view.Port)}

			// Моди: копіюємо файли збірки, крім клієнтських.
			modsCore := strings.EqualFold(core, "fabric") || strings.EqualFold(core, "forge") || strings.EqualFold(core, "neoforge") || strings.EqualFold(core, "quilt")
			if g.boolean("copy_mods", true) && modsCore {
				copied, skipped := 0, []map[string]string{}
				srcDir := a.instanceModsDir(id)
				dstDir := filepath.Join(view.Dir, "mods")
				for _, m := range a.ScanModsFull(id) {
					if !m.Enabled {
						continue
					}
					if m.Environment == "client" {
						skipped = append(skipped, map[string]string{"file": m.FileName, "reason": "клієнтський мод (environment=client)"})
						continue
					}
					if err := aiCopyFile(filepath.Join(srcDir, m.FileName), filepath.Join(dstDir, m.FileName)); err != nil {
						skipped = append(skipped, map[string]string{"file": m.FileName, "reason": "не скопійовано: " + err.Error()})
						continue
					}
					copied++
				}
				res["mods_copied"], res["mods_skipped"] = copied, skipped
				res["note"] = "метадані jar можуть не позначати деякі клієнтські моди: якщо сервер впаде при старті — read_server_crash і toggle_server_content для винуватця"
			} else if !modsCore {
				res["note"] = "ядро без модів (плагіни): моди збірки не переносились; плагіни — install_server_content kind=plugins"
			}

			// Зв'язок збірка ↔ сервер (як у «Сервер збірки» в інтерфейсі).
			link := servers.PackServerLink{PackID: id, PackName: cfg.Name, ServerID: view.ID, ServerName: view.Name,
				ServerAddr: fmt.Sprintf("127.0.0.1:%d", view.Port)}
			if err := a.SavePackServerLink(link); err != nil {
				res["link_error"] = err.Error()
			} else {
				res["linked"] = true
			}
			a.aiEditRecord(id, AIEditChange{Kind: "create", OK: true, Summary: "Створено сервер під збірку: " + view.Name})
			return res, nil
		},
	})
}

// aiAccountKind — чи активний акаунт ліцензійний (без жодних токенів/імен).
func (a *App) aiAccountKind() map[string]any {
	acc := a.cfg.ActiveAccount()
	return map[string]any{"has_account": acc.ID != "", "licensed": acc.IsLicensed, "offline": acc.ID != "" && !acc.IsLicensed}
}

var _ = model.ConsoleLine{}
