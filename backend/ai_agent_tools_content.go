package main

// ai_agent_tools_content.go — шейдери, ресурспаки та датапаки для ШІ-агента:
// перегляд, увімкнення/вимкнення, оновлення, версії, перенесення між збірками,
// встановлення датапаків у світи й вибір активних ресурспаків/шейдера.
// Видалення контенту використовує той самий менеджер, що й інтерфейс лаунчера.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func aiContentDir(g argMap) (string, error) {
	d := g.str("content_dir")
	switch d {
	case "mods", "resourcepacks", "shaderpacks", "datapacks":
		return d, nil
	}
	return "", errors.New("content_dir: mods | resourcepacks | shaderpacks | datapacks")
}

// aiSafeFileName — просте імʼя файлу без шляху й лапок.
func aiSafeFileName(n string) bool {
	return n != "" && n != "." && n != ".." && filepath.Base(n) == n && !strings.ContainsAny(n, "\\/:\"\r\n")
}

// aiSetLine замінює (або додає) рядок `key<sep>value` у тексті конфіга з рядків.
func aiSetLine(text, key, sep, value string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	found := false
	for i, l := range lines {
		if strings.HasPrefix(l, key+sep) {
			lines[i] = key + sep + value
			found = true
		}
	}
	if !found {
		if n := len(lines); n > 0 && lines[n-1] == "" {
			lines = append(lines[:n-1], key+sep+value, "")
		} else {
			lines = append(lines, key+sep+value)
		}
	}
	return strings.Join(lines, "\n")
}

func (a *App) aiWriteWithBackup(path, content string) error {
	if old, err := os.ReadFile(path); err == nil {
		_ = os.WriteFile(path+".ai-bak", old, 0o644)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func (a *App) aiDeleteContent(g argMap) (any, error) {
	if err := g.require("pack_id", "content_dir", "file_name"); err != nil {
		return nil, err
	}
	dir, err := aiContentDir(g)
	if err != nil {
		return nil, err
	}
	id, name := g.str("pack_id"), g.str("file_name")
	base := strings.TrimSuffix(name, contentDisabledSuffix)
	if !aiSafeFileName(id) || !aiSafeFileName(name) || !aiSafeFileName(base) ||
		strings.TrimSpace(id) != id || strings.HasSuffix(id, ".") ||
		strings.TrimSpace(name) != name || strings.HasSuffix(base, ".") || strings.HasSuffix(base, " ") {
		return nil, errors.New("pack_id і file_name мають бути точними назвами без шляхів")
	}
	if dir == "mods" && !strings.HasSuffix(base, ".jar") {
		return nil, errors.New("delete_mod: дозволені лише .jar та .jar.disabled")
	}
	if _, err := a.resolveLaunchBuild(id); err != nil {
		return nil, err
	}
	root, err := filepath.EvalSymlinks(a.gameDir(id))
	if err != nil {
		return nil, err
	}
	// Перевіряємо обидва варіанти до видалення: менеджер прибирає також .disabled.
	found := false
	for _, n := range []string{base, base + contentDisabledSuffix} {
		p := filepath.Join(a.instanceContentDir(id, dir), n)
		st, err := os.Lstat(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		resolved, err := filepath.EvalSymlinks(p)
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(root, resolved)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return nil, errors.New("контент виходить за межі теки збірки")
		}
		if st.Mode()&os.ModeSymlink != 0 || (dir == "mods" && !st.Mode().IsRegular()) ||
			(!st.IsDir() && !isContentArchive(n)) {
			return nil, errors.New("можна видаляти лише файли контенту та розпаковані паки, без посилань")
		}
		found = true
	}
	if !found {
		return nil, fmt.Errorf("контент не знайдено: %s", name)
	}
	if err := a.shareLockErr("build", id); err != nil {
		return nil, err
	}
	if err := a.DeleteContentFile(id, dir, name); err != nil {
		return nil, err
	}
	return map[string]any{"deleted": true, "content_dir": dir, "file_name": name}, nil
}

func init() {
	regTool(aiTool{
		name: "delete_mod", desc: "Видалити непотрібний мод зі збірки, включно з його .jar.disabled копією. file_name з list_mods. Для тимчасових тестів використовуйте toggle_mod; остаточно відхилений мод видаляйте. Перед видаленням модів світу створіть create_world_snapshot або duplicate_build. Гра має бути зупинена.",
		props:    map[string]string{"pack_id": "id збірки", "file_name": "точне імʼя файлу з list_mods, включно з .disabled"},
		required: []string{"pack_id", "file_name"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			g["content_dir"] = "mods"
			return a.aiDeleteContent(g)
		},
	})
	regTool(aiTool{
		name: "delete_content", desc: "Видалити непотрібний мод, ресурспак, шейдер або датапак зі збірки (архів чи розпаковану теку), включно з .disabled копією. file_name з list_content. Для тимчасового вимкнення є toggle_content/toggle_mod. Гра має бути зупинена; перед ризикованими змінами зробіть знімок світів або копію збірки.",
		props:    map[string]string{"pack_id": "id збірки", "content_dir": "mods | resourcepacks | shaderpacks | datapacks", "file_name": "точне імʼя файлу чи теки з list_content"},
		required: []string{"pack_id", "content_dir", "file_name"}, write: true,
		run: func(a *App, g argMap) (any, error) { return a.aiDeleteContent(g) },
	})
	regTool(aiTool{
		name:  "list_content",
		desc:  "Встановлені ресурспаки / шейдери / датапаки (а також моди) збірки: файл, назва, увімкнено, розмір, проєкт на Modrinth/CurseForge, чи є оновлення.",
		props: map[string]string{"pack_id": "id збірки", "content_dir": "resourcepacks | shaderpacks | datapacks | mods"}, required: []string{"pack_id", "content_dir"},
		run: func(a *App, g argMap) (any, error) {
			dir, err := aiContentDir(g)
			if err != nil {
				return nil, err
			}
			if g.str("pack_id") == "" {
				return nil, errPackID()
			}
			return a.ScanContentFiles(g.str("pack_id"), dir)
		},
	})
	regTool(aiTool{
		name: "check_content_updates", desc: "Які ресурспаки / шейдери / датапаки збірки мають оновлення (потрібна версія Minecraft збірки).",
		props:    map[string]string{"pack_id": "id збірки", "content_dir": "resourcepacks | shaderpacks | datapacks", "mc_version": "версія Minecraft збірки"},
		required: []string{"pack_id", "content_dir", "mc_version"},
		run: func(a *App, g argMap) (any, error) {
			dir, err := aiContentDir(g)
			if err != nil {
				return nil, err
			}
			if err := g.require("pack_id", "mc_version"); err != nil {
				return nil, err
			}
			return a.RefreshContentUpdates(g.str("pack_id"), dir, g.str("mc_version"))
		},
	})
	regTool(aiTool{
		name: "get_content_versions", desc: "Доступні версії встановленого ресурспака / шейдера / датапака.",
		props:    map[string]string{"pack_id": "id збірки", "content_dir": "resourcepacks | shaderpacks | datapacks", "file_name": "файл з list_content", "mc_version": "версія Minecraft"},
		required: []string{"pack_id", "content_dir", "file_name", "mc_version"},
		run: func(a *App, g argMap) (any, error) {
			dir, err := aiContentDir(g)
			if err != nil {
				return nil, err
			}
			if err := g.require("pack_id", "file_name", "mc_version"); err != nil {
				return nil, err
			}
			vs := a.GetContentVersions(g.str("pack_id"), dir, g.str("file_name"), g.str("mc_version"))
			for i := range vs {
				if len(vs[i].Changelog) > 300 {
					vs[i].Changelog = vs[i].Changelog[:300] + "…"
				}
			}
			return vs, nil
		},
	})
	regTool(aiTool{
		name: "install_content_version", desc: "Замінити встановлений ресурспак / шейдер / датапак вказаною версією (version_id з get_content_versions).",
		props:    map[string]string{"pack_id": "id збірки", "content_dir": "resourcepacks | shaderpacks | datapacks", "file_name": "файл з list_content", "version_id": "id версії", "mc_version": "версія Minecraft"},
		required: []string{"pack_id", "content_dir", "file_name", "version_id", "mc_version"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			dir, err := aiContentDir(g)
			if err != nil {
				return nil, err
			}
			if err := g.require("pack_id", "file_name", "version_id", "mc_version"); err != nil {
				return nil, err
			}
			for _, v := range a.GetContentVersions(g.str("pack_id"), dir, g.str("file_name"), g.str("mc_version")) {
				if v.ID == g.str("version_id") {
					return "ok", a.InstallContentVersion(g.str("pack_id"), dir, g.str("file_name"), v)
				}
			}
			return nil, errors.New("version_id не знайдено серед версій цього контенту")
		},
	})
	regTool(aiTool{
		name: "toggle_content", desc: "Увімкнути/вимкнути ресурспак / шейдер / датапак (суфікс .disabled, оборотно). Для остаточного видалення використовуйте delete_content.",
		props:    map[string]string{"pack_id": "id збірки", "content_dir": "resourcepacks | shaderpacks | datapacks", "file_name": "файл з list_content", "enabled": "(bool) true — увімкнути"},
		required: []string{"pack_id", "content_dir", "file_name"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			dir, err := aiContentDir(g)
			if err != nil || dir == "mods" {
				return nil, errors.New("content_dir: resourcepacks | shaderpacks | datapacks (для модів — toggle_mod)")
			}
			if err := g.require("pack_id", "file_name"); err != nil {
				return nil, err
			}
			return "ok", a.ToggleContentFile(g.str("pack_id"), dir, g.str("file_name"), g.boolean("enabled", true))
		},
	})
	regTool(aiTool{
		name: "copy_content_between_builds", desc: "Скопіювати один ресурспак / шейдер / датапак з однієї збірки в іншу (з урахуванням версії Minecraft цілі).",
		props:    map[string]string{"source_id": "збірка-джерело", "target_id": "збірка-ціль", "content_dir": "resourcepacks | shaderpacks | datapacks", "file_name": "файл у джерелі"},
		required: []string{"source_id", "target_id", "content_dir", "file_name"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			dir, err := aiContentDir(g)
			if err != nil || dir == "mods" {
				return nil, errors.New("content_dir: resourcepacks | shaderpacks | datapacks (моди — migrate_mods)")
			}
			if err := g.require("source_id", "target_id", "file_name"); err != nil {
				return nil, err
			}
			tcfg := a.GetInstanceConfig(g.str("target_id"))
			return "ok", a.SmartCopyContentFile(g.str("source_id"), dir, g.str("file_name"), g.str("target_id"), tcfg.MCVersion)
		},
	})
	regTool(aiTool{
		name: "install_datapack_to_worlds", desc: "Встановити датапак з Modrinth/CurseForge прямо в обрані одиночні світи збірки (saves/<світ>/datapacks). Світи — з list_build_worlds. Перед цим бажано create_world_snapshot.",
		props:    map[string]string{"pack_id": "id збірки", "worlds": "назви світів через кому", "source": "modrinth | curseforge", "project_id": "id датапака", "version_id": "id версії (порожньо — найкраща сумісна)"},
		required: []string{"pack_id", "worlds", "source", "project_id"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			if err := g.require("pack_id", "worlds", "source", "project_id"); err != nil {
				return nil, err
			}
			return a.InstallDatapackToWorlds(g.str("pack_id"), aiCSV(g.str("worlds")), g.str("source"), g.str("project_id"), g.str("version_id"))
		},
	})
	regTool(aiTool{
		name: "set_resource_packs", desc: "Задати активні ресурспаки збірки (options.txt): файли з resourcepacks/ у порядку від найнижчого пріоритету до найвищого. Порожній список — лише стандартний. Гра має бути закрита; створюється копія options.txt.ai-bak.",
		props: map[string]string{"pack_id": "id збірки", "packs": "імена файлів через кому (з list_content, увімкнені)"}, required: []string{"pack_id", "packs"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			id := g.str("pack_id")
			if id == "" {
				return nil, errPackID()
			}
			if a.aiPackRunning(id) {
				return nil, errors.New("спершу зупиніть гру (stop_build)")
			}
			rp := filepath.Join(a.gameDir(id), "resourcepacks")
			items := []string{`"vanilla"`}
			for _, n := range aiCSV(g.str("packs")) {
				if !aiSafeFileName(n) {
					return nil, fmt.Errorf("недопустиме імʼя: %q", n)
				}
				if _, err := os.Stat(filepath.Join(rp, n)); err != nil {
					return nil, fmt.Errorf("ресурспак не знайдено або вимкнений: %s", n)
				}
				items = append(items, `"file/`+n+`"`)
			}
			opt := filepath.Join(a.gameDir(id), "options.txt")
			old, _ := os.ReadFile(opt)
			text := aiSetLine(string(old), "resourcePacks", ":", "["+strings.Join(items, ",")+"]")
			return map[string]any{"resourcePacks": items}, a.aiWriteWithBackup(opt, text)
		},
	})
	regTool(aiTool{
		name: "set_shader_pack", desc: "Обрати шейдер для Iris (config/iris.properties): shaderPack і enableShaders. Працює, якщо в збірці встановлено Iris. Порожній shader — вимкнути шейдери. Гра має бути закрита.",
		props: map[string]string{"pack_id": "id збірки", "shader": "імʼя файлу з shaderpacks/ (порожньо — вимкнути)"}, required: []string{"pack_id"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			id := g.str("pack_id")
			if id == "" {
				return nil, errPackID()
			}
			if a.aiPackRunning(id) {
				return nil, errors.New("спершу зупиніть гру (stop_build)")
			}
			shader := g.str("shader")
			enable := "false"
			if shader != "" {
				if !aiSafeFileName(shader) {
					return nil, fmt.Errorf("недопустиме імʼя: %q", shader)
				}
				if _, err := os.Stat(filepath.Join(a.gameDir(id), "shaderpacks", shader)); err != nil {
					return nil, fmt.Errorf("шейдер не знайдено або вимкнений: %s", shader)
				}
				enable = "true"
			}
			cfg := filepath.Join(a.gameDir(id), "config", "iris.properties")
			// Oculus (Forge) тримає ті самі ключі в config/oculus.properties.
			if _, err := os.Stat(filepath.Join(a.gameDir(id), "config", "oculus.properties")); err == nil {
				cfg = filepath.Join(a.gameDir(id), "config", "oculus.properties")
			}
			old, _ := os.ReadFile(cfg)
			text := aiSetLine(string(old), "enableShaders", "=", enable)
			text = aiSetLine(text, "shaderPack", "=", shader)
			return map[string]any{"shaderPack": shader, "enableShaders": enable == "true",
				"note": "діє, якщо встановлено Iris (або сумісний мод); перевірте list_mods"}, a.aiWriteWithBackup(cfg, text)
		},
	})
}
