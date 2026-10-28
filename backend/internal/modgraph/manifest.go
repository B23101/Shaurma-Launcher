// Package modgraph — граф залежностей модів за маніфестами всередині jar.
//
// Порт логіки «дерева залежностей» з Nivora Launcher (mod_dependencies.py):
// читаємо fabric.mod.json, quilt.mod.json, META-INF/(neoforge.)mods.toml та
// mcmod.info БЕЗ мережі, тож граф будується і для локальних модів. Сам
// лоадер, гра та Java (minecraft, fabricloader, java…) відфільтровані як шум.
//
// Розширення відносно Nivora (потрібні, щоб не показувати хибних «відсутніх»):
//   - враховуються "provides" (Fabric API закриває десятки під-ID);
//   - враховуються вкладені jar (jar-in-jar: META-INF/jars, META-INF/jarjar);
//   - вимкнені моди (*.jar.disabled) не задовольняють залежності, але
//     підказується, що залежність лише вимкнена.
//
// Пакет не залежить від решти лаунчера (лише stdlib), тож тестується окремо
// і використовується і графом, і рушієм авто-починки (internal/autorepair).
package modgraph

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"sort"
	"strings"
)

const (
	// maxManifestBytes — ліміт розміру одного маніфесту (захист від zip-бомб).
	maxManifestBytes = 256 * 1024
	// maxNestedJars / maxNestedJarBytes — обмеження на jar-in-jar.
	maxNestedJars     = 160
	maxNestedJarBytes = 12 * 1024 * 1024
	// maxPackages — скільки пакетів класів мода запам'ятовуємо (для зведення
	// стека помилки до конкретного jar).
	maxPackages = 64
)

// NoiseIDs — ідентифікатори, які завжди «є» (лоадер, гра, Java) і ніколи не
// лежать у mods/ окремим jar; у графі й діагностиці вони ігноруються.
var NoiseIDs = map[string]bool{
	"minecraft": true, "fabricloader": true, "fabric-loader": true, "fabric_loader": true,
	"quilt_loader": true, "quilt-loader": true, "quiltloader": true, "quilt_base": true,
	"forge": true, "neoforge": true, "minecraftforge": true, "fml": true, "javafml": true,
	"java": true, "mcp": true,
}

// Dependency — одна залежність мода.
type Dependency struct {
	ID         string `json:"id"`
	Constraint string `json:"constraint"`
	Optional   bool   `json:"optional"`
}

// Manifest — розібрані метадані одного jar.
type Manifest struct {
	ModID   string `json:"modId"`
	Name    string `json:"name"`
	Version string `json:"version"`
	// Description — короткий опис мода з маніфесту (для підказки в дереві).
	Description string `json:"description,omitempty"`
	// Loader — лоадер основного маніфесту: fabric | quilt | forge | neoforge.
	Loader string `json:"loader"`
	// Loaders — усі лоадери, формати яких присутні в jar (мульти-лоадерні моди).
	Loaders      []string     `json:"loaders"`
	Dependencies []Dependency `json:"dependencies"`
	Breaks       []string     `json:"breaks,omitempty"`
	// Provides — додаткові mod-ID, які цей jar теж закриває (fabric "provides").
	Provides []string `json:"provides,omitempty"`
	// Embedded — mod-ID вкладених jar (jar-in-jar), які теж доступні грі.
	Embedded []string `json:"embedded,omitempty"`
	// MixinConfigs — *.mixins.json у корені jar (імена в нижньому регістрі).
	MixinConfigs []string `json:"mixinConfigs,omitempty"`
	// Packages — перші 3 сегменти пакетів класів (для зіставлення зі стеком).
	Packages []string `json:"packages,omitempty"`
	// Corrupted — jar не відкривається як zip-архів.
	Corrupted bool `json:"corrupted"`
}

// ReadJar відкриває jar і повертає його маніфест. Ніколи не панікує: для
// пошкодженого файлу повертає Manifest{Corrupted: true}.
func ReadJar(path string) Manifest {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return Manifest{Corrupted: true}
	}
	defer zr.Close()
	return readArchive(&zr.Reader, true)
}

func readArchive(zr *zip.Reader, withNested bool) Manifest {
	var m Manifest
	loaders := map[string]bool{}
	files := map[string]*zip.File{}
	packages := map[string]bool{}
	var nested []*zip.File

	for _, f := range zr.File {
		name := f.Name
		files[name] = f
		switch {
		case strings.HasSuffix(name, ".class") && !strings.HasPrefix(name, "META-INF/"):
			parts := strings.Split(name, "/")
			parts = parts[:len(parts)-1]
			if len(parts) >= 2 && len(packages) < maxPackages {
				if len(parts) > 3 {
					parts = parts[:3]
				}
				packages[strings.Join(parts, "/")] = true
			}
		case !strings.Contains(name, "/") && strings.HasSuffix(strings.ToLower(name), ".json") && strings.Contains(strings.ToLower(name), "mixin"):
			m.MixinConfigs = append(m.MixinConfigs, strings.ToLower(name))
		case withNested && (strings.HasPrefix(name, "META-INF/jars/") || strings.HasPrefix(name, "META-INF/jarjar/")) && strings.HasSuffix(name, ".jar"):
			if len(nested) < maxNestedJars {
				nested = append(nested, f)
			}
		}
	}
	for p := range packages {
		m.Packages = append(m.Packages, p)
	}

	// Лоадери за наявними форматами.
	if files["fabric.mod.json"] != nil {
		loaders["fabric"] = true
	}
	if files["quilt.mod.json"] != nil {
		loaders["quilt"] = true
	}
	if files["META-INF/neoforge.mods.toml"] != nil {
		loaders["neoforge"] = true
	}
	if files["META-INF/mods.toml"] != nil || files["mcmod.info"] != nil {
		loaders["forge"] = true
	}
	for _, l := range []string{"fabric", "quilt", "neoforge", "forge"} {
		if loaders[l] {
			m.Loaders = append(m.Loaders, l)
		}
	}

	// Пріоритет, як у mods.Manager: TOML > Fabric > Quilt > mcmod.info.
	parsers := []struct {
		name   string
		parser func([]byte) *Manifest
	}{
		{"META-INF/neoforge.mods.toml", parseTOML},
		{"META-INF/mods.toml", parseTOML},
		{"fabric.mod.json", parseFabric},
		{"quilt.mod.json", parseQuilt},
		{"mcmod.info", parseMcmod},
	}
	var primary *Manifest
	for _, p := range parsers {
		f := files[p.name]
		if f == nil {
			continue
		}
		if parsed := p.parser(readMember(f)); parsed != nil && parsed.ModID != "" {
			primary = parsed
			break
		}
	}
	if primary != nil {
		m.ModID = strings.ToLower(strings.TrimSpace(primary.ModID))
		m.Name = primary.Name
		m.Version = primary.Version
		m.Description = primary.Description
		m.Loader = primary.Loader
		m.Dependencies = primary.Dependencies
		m.Breaks = primary.Breaks
		m.Provides = primary.Provides
	}

	embedPath := ""
	if m.ModID == "" && len(nested) > 0 {
		embedPath = embeddedModPath(files)
	}
	for _, f := range nested {
		if f.UncompressedSize64 > maxNestedJarBytes {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(rc, maxNestedJarBytes))
		rc.Close()
		if err != nil {
			continue
		}
		inner, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			continue
		}
		im := readArchive(inner, false)
		// Обгортка без власних метаданих (Sinytra Connector): сам мод — це
		// вкладений jar з Embedded-Dependencies-Mod у MANIFEST.MF.
		if m.ModID == "" && im.ModID != "" && embedPath != "" && f.Name == embedPath {
			m.ModID, m.Name, m.Version, m.Description = im.ModID, im.Name, im.Version, im.Description
			m.Loader, m.Loaders = im.Loader, im.Loaders
			m.Dependencies, m.Breaks, m.Provides = im.Dependencies, im.Breaks, im.Provides
			continue
		}
		if im.ModID != "" {
			m.Embedded = append(m.Embedded, im.ModID)
		}
		m.Embedded = append(m.Embedded, im.Provides...)
	}
	return m
}

// embeddedModPath — шлях вкладеного jar-мода з MANIFEST.MF
// (Embedded-Dependencies-Mod) або "", якщо такого ключа немає.
func embeddedModPath(files map[string]*zip.File) string {
	f := files["META-INF/MANIFEST.MF"]
	if f == nil {
		return ""
	}
	text := strings.ReplaceAll(string(readMember(f)), "\r\n", "\n")
	text = strings.ReplaceAll(text, "\n ", "")
	for _, line := range strings.Split(text, "\n") {
		if v, ok := strings.CutPrefix(line, "Embedded-Dependencies-Mod:"); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func readMember(f *zip.File) []byte {
	if f == nil || f.FileInfo().IsDir() || f.UncompressedSize64 > maxManifestBytes {
		return nil
	}
	rc, err := f.Open()
	if err != nil {
		return nil
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, maxManifestBytes))
	if err != nil {
		return nil
	}
	return data
}

// ── fabric.mod.json ──────────────────────────────────────────────────────

func constraintText(v any) string {
	switch x := v.(type) {
	case string:
		return clip(x, 80)
	case []any:
		parts := make([]string, 0, len(x))
		for _, item := range x {
			if s, ok := item.(string); ok {
				parts = append(parts, s)
			}
		}
		if len(parts) > 0 {
			return clip(strings.Join(parts, ","), 80)
		}
	}
	return "будь-яка"
}

func parseFabric(raw []byte) *Manifest {
	var v struct {
		ID         string         `json:"id"`
		Name       string         `json:"name"`
		Version    string         `json:"version"`
		Desc       string         `json:"description"`
		Depends    map[string]any `json:"depends"`
		Recommends map[string]any `json:"recommends"`
		Suggests   map[string]any `json:"suggests"`
		Breaks     map[string]any `json:"breaks"`
		Provides   []string       `json:"provides"`
	}
	if json.Unmarshal(raw, &v) != nil || v.ID == "" {
		return nil
	}
	m := &Manifest{ModID: v.ID, Name: firstNonEmpty(v.Name, v.ID), Version: v.Version, Description: clip(strings.TrimSpace(v.Desc), 300), Loader: "fabric", Provides: lowerAll(v.Provides)}
	for _, key := range sortedKeys(v.Depends) {
		m.Dependencies = append(m.Dependencies, Dependency{ID: key, Constraint: constraintText(v.Depends[key])})
	}
	for _, src := range []map[string]any{v.Recommends, v.Suggests} {
		for _, key := range sortedKeys(src) {
			m.Dependencies = append(m.Dependencies, Dependency{ID: key, Constraint: constraintText(src[key]), Optional: true})
		}
	}
	m.Breaks = sortedKeys(v.Breaks)
	return m
}

// ── quilt.mod.json ───────────────────────────────────────────────────────

func parseQuilt(raw []byte) *Manifest {
	var v struct {
		Loader struct {
			ID       string `json:"id"`
			Version  string `json:"version"`
			Metadata struct {
				Name string `json:"name"`
				Desc string `json:"description"`
			} `json:"metadata"`
			Depends  []json.RawMessage `json:"depends"`
			Provides []json.RawMessage `json:"provides"`
		} `json:"quilt_loader"`
	}
	if json.Unmarshal(raw, &v) != nil || v.Loader.ID == "" {
		return nil
	}
	m := &Manifest{ModID: v.Loader.ID, Name: firstNonEmpty(v.Loader.Metadata.Name, v.Loader.ID), Version: v.Loader.Version, Description: clip(strings.TrimSpace(v.Loader.Metadata.Desc), 300), Loader: "quilt"}
	for _, item := range v.Loader.Depends {
		var s string
		if json.Unmarshal(item, &s) == nil && s != "" {
			m.Dependencies = append(m.Dependencies, Dependency{ID: s, Constraint: "будь-яка"})
			continue
		}
		var obj struct {
			ID       string `json:"id"`
			Versions any    `json:"versions"`
			Optional bool   `json:"optional"`
		}
		if json.Unmarshal(item, &obj) == nil && obj.ID != "" {
			m.Dependencies = append(m.Dependencies, Dependency{ID: obj.ID, Constraint: constraintText(obj.Versions), Optional: obj.Optional})
		}
	}
	for _, item := range v.Loader.Provides {
		var s string
		if json.Unmarshal(item, &s) == nil && s != "" {
			m.Provides = append(m.Provides, strings.ToLower(s))
			continue
		}
		var obj struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(item, &obj) == nil && obj.ID != "" {
			m.Provides = append(m.Provides, strings.ToLower(obj.ID))
		}
	}
	return m
}

// ── META-INF/mods.toml та neoforge.mods.toml ─────────────────────────────

var (
	tomlSectionRe = regexp.MustCompile(`(?m)^\s*\[\[?[\w.\-"]+\]?\]\s*$`)
	tomlModsRe    = regexp.MustCompile(`(?m)^\s*\[\[mods\]\]\s*$`)
	tomlDepRe     = regexp.MustCompile(`(?m)^\s*\[\[dependencies\.[\w.\-"]+\]\]\s*$`)
	tomlModIDRe   = regexp.MustCompile(`(?m)^\s*modId\s*=\s*["']([\w.\-]+)["']`)
	tomlNameRe    = regexp.MustCompile(`(?m)^\s*displayName\s*=\s*(?:"([^"]*)"|'([^']*)')`)
	tomlDescRe    = regexp.MustCompile(`(?s)(?m)^\s*description\s*=\s*(?:"""(.*?)"""|\'\'\'(.*?)\'\'\'|"([^"\n]*)"|\'([^\'\n]*)\')`)
	tomlVersionRe = regexp.MustCompile(`(?m)^\s*version\s*=\s*["']([^"']+)["']`)
	tomlRangeRe   = regexp.MustCompile(`(?m)^\s*versionRange\s*=\s*["']([^"']*)["']`)
	tomlTypeRe    = regexp.MustCompile(`(?m)^\s*type\s*=\s*["'](\w+)["']`)
	tomlMandRe    = regexp.MustCompile(`(?m)^\s*mandatory\s*=\s*(\w+)`)
)

// sectionBody повертає текст від заголовка (loc — індекси збігу) до наступного
// заголовка секції.
func sectionBody(text string, loc []int) string {
	rest := text[loc[1]:]
	if next := tomlSectionRe.FindStringIndex(rest); next != nil {
		return rest[:next[0]]
	}
	return rest
}

func parseTOML(raw []byte) *Manifest {
	text := string(raw)
	if text == "" {
		return nil
	}
	lower := strings.ToLower(text)
	loader := "forge"
	if strings.Contains(lower, "neoforge") {
		loader = "neoforge"
	}
	m := &Manifest{Loader: loader}
	body := text
	if loc := tomlModsRe.FindStringIndex(text); loc != nil {
		body = sectionBody(text, loc)
	}
	idMatch := tomlModIDRe.FindStringSubmatch(body)
	if idMatch == nil {
		idMatch = tomlModIDRe.FindStringSubmatch(text)
	}
	if idMatch == nil {
		return nil
	}
	m.ModID = idMatch[1]
	m.Name = m.ModID
	if g := tomlNameRe.FindStringSubmatch(body); g != nil {
		m.Name = firstNonEmpty(g[1], g[2], m.ModID)
	}
	if g := tomlVersionRe.FindStringSubmatch(body); g != nil {
		m.Version = g[1]
	}
	if g := tomlDescRe.FindStringSubmatch(body); g != nil {
		m.Description = clip(strings.Join(strings.Fields(firstNonEmpty(g[1], g[2], g[3], g[4])), " "), 300)
	}
	for _, loc := range tomlDepRe.FindAllStringIndex(text, -1) {
		block := sectionBody(text, loc)
		id := tomlModIDRe.FindStringSubmatch(block)
		if id == nil {
			continue
		}
		dep := Dependency{ID: id[1]}
		if g := tomlRangeRe.FindStringSubmatch(block); g != nil {
			dep.Constraint = clip(g[1], 80)
		}
		if g := tomlTypeRe.FindStringSubmatch(block); g != nil {
			dep.Optional = !strings.EqualFold(g[1], "required")
		} else if g := tomlMandRe.FindStringSubmatch(block); g != nil {
			dep.Optional = !strings.EqualFold(g[1], "true")
		}
		m.Dependencies = append(m.Dependencies, dep)
	}
	return m
}

// ── mcmod.info ───────────────────────────────────────────────────────────

var mcmodIDRe = regexp.MustCompile(`^([A-Za-z][\w.\-]*)`)

func parseMcmod(raw []byte) *Manifest {
	var item struct {
		ModID           string   `json:"modid"`
		Name            string   `json:"name"`
		Version         string   `json:"version"`
		UseDependencies []string `json:"useDependencies"`
	}
	// mcmod.info — або масив, або {"modList":[...]}.
	var list []struct {
		ModID           string   `json:"modid"`
		Name            string   `json:"name"`
		Version         string   `json:"version"`
		UseDependencies []string `json:"useDependencies"`
	}
	if json.Unmarshal(raw, &list) == nil && len(list) > 0 {
		item = list[0]
	} else {
		var wrapped struct {
			ModList []struct {
				ModID           string   `json:"modid"`
				Name            string   `json:"name"`
				Version         string   `json:"version"`
				UseDependencies []string `json:"useDependencies"`
			} `json:"modList"`
		}
		if json.Unmarshal(raw, &wrapped) != nil || len(wrapped.ModList) == 0 {
			return nil
		}
		item = wrapped.ModList[0]
	}
	id := firstNonEmpty(item.ModID, item.Name)
	if id == "" {
		return nil
	}
	m := &Manifest{ModID: id, Name: item.Name, Version: item.Version, Loader: "forge"}
	for _, token := range item.UseDependencies {
		g := mcmodIDRe.FindStringSubmatch(token)
		if g == nil {
			continue
		}
		m.Dependencies = append(m.Dependencies, Dependency{
			ID: g[1], Constraint: clip(token, 80),
			Optional: strings.HasPrefix(token, "after:") || strings.HasPrefix(token, "after-"),
		})
	}
	return m
}

// ── допоміжне ────────────────────────────────────────────────────────────

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func lowerAll(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.ToLower(strings.TrimSpace(s)); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
