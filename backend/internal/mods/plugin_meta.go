// plugin_meta.go — метадані плагінів серверів (Bukkit/Paper, Velocity,
// BungeeCord) з jar: назва, версія, автори, опис, залежності й платформа.
// Раніше плагіни мали лише ім'я файлу — менеджер не бачив ні версії, ні
// автора, ні того, що Velocity-плагін у Paper-сервері не завантажиться.
package mods

import (
	"archive/zip"
	"encoding/json"
	"io"
	"strings"
)

// Платформи, на яких працює плагін кожного формату (лоадери в термінах
// менеджера модів — ними ж рахується сумісність із ядром сервера).
var (
	bukkitLoaders   = []string{"bukkit", "spigot", "paper", "purpur", "folia"}
	paperLoaders    = []string{"paper", "purpur", "folia"}
	velocityLoaders = []string{"velocity"}
	bungeeLoaders   = []string{"bungeecord", "waterfall"}
)

// Залежності, які завжди є на платформі (не окремі плагіни).
var excludedPluginDeps = map[string]bool{"velocity": true, "paper": true, "bukkit": true}

// pluginYAML — мінімальний розбір plugin.yml/bungee.yml: ключі верхнього
// рівня зі скалярами й списками (інлайн [a, b] або блоком «- a»).
// Вкладені мапи (commands, permissions) пропускаються.
func pluginYAML(text string) map[string][]string {
	out := map[string][]string{}
	key := ""
	for _, raw := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line := strings.TrimRight(raw, " \t")
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		indented := line[0] == ' ' || line[0] == '\t'
		if indented {
			if key != "" && strings.HasPrefix(t, "- ") {
				out[key] = append(out[key], unquoteYAML(strings.TrimSpace(t[2:])))
			}
			continue
		}
		key = ""
		i := strings.Index(t, ":")
		if i <= 0 {
			continue
		}
		k, v := strings.TrimSpace(t[:i]), strings.TrimSpace(t[i+1:])
		if v == "" {
			key = k // далі можуть іти елементи списку
			out[k] = nil
			continue
		}
		if strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") {
			for _, p := range strings.Split(strings.Trim(v, "[]"), ",") {
				if p = unquoteYAML(strings.TrimSpace(p)); p != "" {
					out[k] = append(out[k], p)
				}
			}
			continue
		}
		out[k] = []string{unquoteYAML(v)}
	}
	return out
}

func unquoteYAML(s string) string {
	if i := strings.Index(s, " #"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return strings.Trim(s, `"'`)
}

func readZipText(f *zip.File, limit int64) string {
	rc, err := f.Open()
	if err != nil {
		return ""
	}
	defer rc.Close()
	data, _ := io.ReadAll(io.LimitReader(rc, limit))
	return string(data)
}

func firstOf(m map[string][]string, key string) string {
	if v := m[key]; len(v) > 0 {
		return v[0]
	}
	return ""
}

// parsePluginYML — plugin.yml (Bukkit/Spigot/Paper), paper-plugin.yml або
// bungee.yml.
func parsePluginYML(f *zip.File, loaders []string) *jarMeta {
	m := pluginYAML(readZipText(f, 256<<10))
	name := firstOf(m, "name")
	if name == "" {
		return nil
	}
	authors := m["authors"]
	if a := firstOf(m, "author"); a != "" {
		authors = append([]string{a}, authors...)
	}
	var deps []string
	for _, k := range []string{"depend", "depends"} {
		for _, d := range m[k] {
			if d != "" && !excludedPluginDeps[strings.ToLower(d)] {
				deps = append(deps, d)
			}
		}
	}
	return &jarMeta{
		ID: strings.ToLower(name), Name: name, Version: firstOf(m, "version"),
		Description: firstOf(m, "description"), Author: strings.Join(authors, ", "),
		LoaderType: loaders[0], Loaders: loaders, Depends: deps, Environment: "server",
	}
}

// parseVelocityPluginJSON — velocity-plugin.json.
func parseVelocityPluginJSON(f *zip.File) *jarMeta {
	var meta struct {
		ID           string   `json:"id"`
		Name         string   `json:"name"`
		Version      string   `json:"version"`
		Description  string   `json:"description"`
		Authors      []string `json:"authors"`
		Dependencies []struct {
			ID       string `json:"id"`
			Optional bool   `json:"optional"`
		} `json:"dependencies"`
	}
	if json.Unmarshal([]byte(readZipText(f, 256<<10)), &meta) != nil || meta.ID == "" {
		return nil
	}
	name := meta.Name
	if name == "" {
		name = meta.ID
	}
	var deps []string
	for _, d := range meta.Dependencies {
		if !d.Optional && !excludedPluginDeps[d.ID] {
			deps = append(deps, d.ID)
		}
	}
	return &jarMeta{
		ID: meta.ID, Name: name, Version: meta.Version, Description: meta.Description,
		Author: strings.Join(meta.Authors, ", "), LoaderType: "velocity", Loaders: velocityLoaders,
		Depends: deps, Environment: "server",
	}
}

// parsePluginMeta — метадані плагіна з jar (nil — це не плагін). Порядок:
// paper-plugin.yml (новіший формат Paper) → plugin.yml → velocity → bungee.
func parsePluginMeta(zr *zip.ReadCloser) *jarMeta {
	byName := map[string]*zip.File{}
	for _, f := range zr.File {
		switch f.Name {
		case "paper-plugin.yml", "plugin.yml", "velocity-plugin.json", "bungee.yml":
			byName[f.Name] = f
		}
	}
	var primary *jarMeta
	loaders := map[string]bool{}
	var order []string
	add := func(m *jarMeta) {
		if m == nil {
			return
		}
		for _, l := range m.Loaders {
			if !loaders[l] {
				loaders[l] = true
				order = append(order, l)
			}
		}
		if primary == nil {
			primary = m
		}
	}
	if f := byName["paper-plugin.yml"]; f != nil {
		add(parsePluginYML(f, paperLoaders))
	}
	if f := byName["plugin.yml"]; f != nil {
		add(parsePluginYML(f, bukkitLoaders))
	}
	if f := byName["velocity-plugin.json"]; f != nil {
		add(parseVelocityPluginJSON(f))
	}
	if f := byName["bungee.yml"]; f != nil {
		add(parsePluginYML(f, bungeeLoaders))
	}
	if primary != nil {
		// Мультиплатформний jar (Bukkit + Velocity в одному) — сумісний
		// з кожною зі своїх платформ.
		primary.Loaders = order
	}
	return primary
}
