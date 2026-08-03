package mods

import (
	"archive/zip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-wails/internal/model"
)

// ── Менеджер модів збірки ───────────────────────────────────────────────
// Повний сканер jar-файлів: читає метадані (fabric.mod.json / quilt.mod.json
// / META-INF/mods.toml / META-INF/neoforge.mods.toml / mcmod.info), іконки,
// SHA1 та залежності. Формат вимкненого мода — суфікс ".jar.disabled"
// (узгоджено зі старим JavaFX-лаунчером).

const disabledSuffix = ".jar.disabled"
const legacyDisabledPrefix = ".disabled."

type Manager struct{}

func NewManager() *Manager {
	return &Manager{}
}

// ── Кеш метаданих jar (path+size+mtime → метадані) ──────────────────────
// Повторні скани (після вимкнення/увімкнення мода, оновлення) НЕ читають
// zip заново для незмінених файлів — це те, що в старому лаунчері
// «підвішувало» завантаження сторінки на сотнях модів.

type jarCacheKey struct {
	path  string
	size  int64
	mtime int64
}

type jarMeta struct {
	ID          string
	Name        string
	Version     string
	Description string
	Author      string
	LoaderType  string // fabric | quilt | forge | neoforge | unknown
	Loaders     []string
	IconData    string // data URL (base64) або ""
	IconPath    string // шлях іконки ВСЕРЕДИНІ jar, з метаданих (logoFile/icon) — заповнюється парсером, IconData рахує attachIcon
	Depends     []string
}

var (
	jarMetaCacheMu sync.Mutex
	jarMetaCache   = map[jarCacheKey]*jarMeta{}
)

func cacheKey(path string) (jarCacheKey, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return jarCacheKey{}, false
	}
	return jarCacheKey{path: path, size: info.Size(), mtime: info.ModTime().Unix()}, true
}

// ── ListMods: легкий список (для сумісності з консоллю/старими викликами) ──
func (m *Manager) ListMods(modsDir string) ([]model.ModEntry, error) {
	if err := os.MkdirAll(modsDir, 0755); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(modsDir)
	if err != nil {
		return nil, err
	}
	migrateLegacyDisabled(modsDir, entries)
	entries, _ = os.ReadDir(modsDir)

	var mods []model.ModEntry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		enabled := true
		fileName := name
		if strings.HasSuffix(name, disabledSuffix) {
			enabled = false
			fileName = strings.TrimSuffix(name, ".disabled")
		} else if !strings.HasSuffix(name, ".jar") {
			continue
		}
		mod := model.ModEntry{
			FileName: name,
			Name:     strings.TrimSuffix(fileName, ".jar"),
			Enabled:  enabled,
		}
		if meta := m.readJarCached(filepath.Join(modsDir, name)); meta != nil {
			mod.ID = meta.ID
			mod.Name = meta.Name
			mod.Version = meta.Version
		}
		mods = append(mods, mod)
	}
	return mods, nil
}

// ── ScanModsFull: повне сканування з метаданими/іконками/SHA1 ───────────
func (m *Manager) ScanModsFull(modsDir string) ([]model.ModEntry, error) {
	if err := os.MkdirAll(modsDir, 0755); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(modsDir)
	if err != nil {
		return nil, err
	}
	migrateLegacyDisabled(modsDir, entries)
	entries, _ = os.ReadDir(modsDir)

	var mods []model.ModEntry
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		enabled := true
		fileName := name
		if strings.HasSuffix(name, disabledSuffix) {
			enabled = false
			fileName = strings.TrimSuffix(name, ".disabled")
		} else if !strings.HasSuffix(name, ".jar") {
			continue
		}
		full := filepath.Join(modsDir, name)
		info, _ := os.Stat(full)

		mod := model.ModEntry{
			FileName:      name,
			Name:          strings.TrimSuffix(fileName, ".jar"),
			Enabled:       enabled,
			InstalledDate: info.ModTime().Format(time.RFC3339),
		}
		if info != nil {
			mod.FileSize = info.Size()
		}
		if meta := m.readJarCached(full); meta != nil {
			mod.ID = meta.ID
			mod.Name = meta.Name
			mod.Version = meta.Version
			mod.Description = meta.Description
			mod.Author = meta.Author
			mod.Loader = meta.LoaderType
			mod.IconData = meta.IconData
			mod.DependsOn = meta.Depends
		}
		mods = append(mods, mod)
	}
	sort.Slice(mods, func(i, j int) bool {
		return mods[i].InstalledDate < mods[j].InstalledDate
	})
	return mods, nil
}

// readJarCached повертає метадані jar (з кешу, якщо файл не змінювався).
func (m *Manager) readJarCached(path string) *jarMeta {
	key, ok := cacheKey(path)
	if !ok {
		return nil
	}
	jarMetaCacheMu.Lock()
	if c, hit := jarMetaCache[key]; hit {
		jarMetaCacheMu.Unlock()
		return c
	}
	jarMetaCacheMu.Unlock()

	meta := parseJar(path)
	jarMetaCacheMu.Lock()
	jarMetaCache[key] = meta
	// Простий захист від безмежного росту: старі записи (після оновлень
	// модів імена/розміри файлів змінюються, старі ключі стають непотрібні)
	// періодично вичищаємо цілком.
	if len(jarMetaCache) > 2000 {
		jarMetaCache = map[jarCacheKey]*jarMeta{}
	}
	jarMetaCacheMu.Unlock()
	return meta
}

func migrateLegacyDisabled(modsDir string, entries []os.DirEntry) {
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, legacyDisabledPrefix) && strings.HasSuffix(name, ".jar") {
			newName := strings.TrimPrefix(name, legacyDisabledPrefix)
			if strings.HasSuffix(newName, ".jar") {
				newName = newName[:len(newName)-len(".jar")] + disabledSuffix
			}
			if newName != name {
				_ = os.Rename(filepath.Join(modsDir, name), filepath.Join(modsDir, newName))
			}
		}
	}
}

// ── Парсинг jar ─────────────────────────────────────────────────────────
// УВАГА: SHA1 НЕ рахується під час сканування — це читання всього файлу
// (гігабайти на великих збірках). Хеш потрібен лише для точного матчингу
// на Modrinth за /version_file/{hash}, але пошук за назвою з фасетами
// (версія+лоадер) дає той самий результат без важкого I/O. Дублікати
// виявляються за modId (швидко, без хешування).
func parseJar(jarPath string) *jarMeta {
	zr, err := zip.OpenReader(jarPath)
	if err != nil {
		return &jarMeta{ID: baseNameNoExt(jarPath), Name: baseNameNoExt(jarPath), Version: "unknown", LoaderType: "unknown"}
	}
	defer zr.Close()

	// fabric/quilt спершу, потім Forge/NeoForge TOML, потім legacy mcmod.info.
	for _, f := range zr.File {
		if f.Name == "fabric.mod.json" {
			if meta := parseFabricJSON(f, jarPath); meta != nil {
				attachIcon(zr, meta)
				return meta
			}
		}
	}
	for _, f := range zr.File {
		if f.Name == "quilt.mod.json" {
			if meta := parseQuiltJSON(f, jarPath); meta != nil {
				attachIcon(zr, meta)
				return meta
			}
		}
	}
	for _, f := range zr.File {
		if f.Name == "META-INF/neoforge.mods.toml" || f.Name == "META-INF/mods.toml" {
			if meta := parseModsToml(f, jarPath); meta != nil {
				attachIcon(zr, meta)
				return meta
			}
		}
	}
	for _, f := range zr.File {
		if f.Name == "mcmod.info" {
			if meta := parseMcmodInfo(f, jarPath); meta != nil {
				attachIcon(zr, meta)
				return meta
			}
		}
	}
	return &jarMeta{ID: baseNameNoExt(jarPath), Name: baseNameNoExt(jarPath), Version: "unknown", LoaderType: "unknown"}
}

func baseNameNoExt(path string) string {
	name := path
	if idx := strings.LastIndexAny(path, `/\\`); idx >= 0 {
		name = path[idx+1:]
	}
	return strings.TrimSuffix(name, ".jar")
}

// ── fabric.mod.json / quilt.mod.json ────────────────────────────────────
// Виключаємо лише те, що ЗАВЖДИ присутнє за визначенням лоадера/рушія
// і ніколи не встановлюється як окремий jar у mods/ — сам лоадер, гру,
// Java. "fabric-api" (і Quilt-аналог "qsl"/"quilted_fabric_api") НЕ
// виключаємо: це звичайний мод, який треба поставити окремо, і якщо
// його немає — мод не запуститься, тому це має лишатись справжньою
// залежністю для аналізу/встановлення.
var excludedDeps = map[string]bool{
	"minecraft": true, "fabricloader": true,
	"java": true, "quilt_loader": true,
}

type fabricModJSON struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Description string            `json:"description"`
	Icon        json.RawMessage   `json:"icon"`
	Authors     []json.RawMessage `json:"authors"`
	Depends     map[string]any    `json:"depends"`
}

func parseFabricJSON(f *zip.File, jarPath string) *jarMeta {
	rc, err := f.Open()
	if err != nil {
		return nil
	}
	defer rc.Close()
	var meta fabricModJSON
	if json.NewDecoder(rc).Decode(&meta) != nil {
		return nil
	}
	name := meta.Name
	if name == "" {
		name = meta.ID
	}
	author := ""
	if len(meta.Authors) > 0 {
		var s string
		if json.Unmarshal(meta.Authors[0], &s) == nil {
			author = s
		} else {
			var obj struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(meta.Authors[0], &obj) == nil {
				author = obj.Name
			}
		}
	}
	id := meta.ID
	if id == "" {
		id = baseNameNoExt(jarPath)
	}
	// "depends" у fabric.mod.json — це саме ОБОВ'ЯЗКОВІ (hard) залежності
	// специфікації Fabric (на відміну від "recommends"/"suggests"/
	// "conflicts"/"breaks", які тут навмисно не читаються — це не
	// обов'язкові вимоги, встановлювати їх примусово не треба).
	// Виключаємо власний ID мода (де-не-де трапляється в кривих jar) і
	// завжди-присутні псевдо-залежності лоадера/рушія.
	var deps []string
	for key := range meta.Depends {
		if key == id || excludedDeps[key] {
			continue
		}
		deps = append(deps, key)
	}
	version := meta.Version
	if version == "" {
		version = "unknown"
	}
	loaderType := "fabric"
	loaders := []string{"fabric"}
	if strings.HasPrefix(id, "quilt") {
		loaderType, loaders = "quilt", []string{"quilt"}
	}
	return &jarMeta{
		ID: id, Name: name, Version: version, Description: meta.Description,
		Author: author, LoaderType: loaderType, Loaders: loaders, Depends: deps,
		IconPath: fabricIconPath(meta.Icon),
	}
}

// fabricIconPath дістає шлях іконки з поля "icon" fabric.mod.json — воно
// буває або простим рядком-шляхом ("assets/modid/icon.png"), або мапою
// розмір→шлях ({"16": "...", "32": "...", "128": "..."}), у такому разі
// беремо найбільший (краще підходить для показу в UI). Раніше поле
// "icon" декодувалось у struct, але ніде не використовувалось — тому
// іконка мода бралась лише за жорстко зашитими іменами pack.png/icon.png
// у КОРЕНІ jar, а більшість fabric-модів кладуть іконку в assets/<id>/...
// і взагалі не мають файлу з такою назвою в корені — іконка не
// показувалась, хоча в jar вона фактично є.
func fabricIconPath(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		return s
	}
	var sizes map[string]string
	if json.Unmarshal(raw, &sizes) == nil && len(sizes) > 0 {
		bestSize, bestPath := -1, ""
		for k, v := range sizes {
			n, err := strconv.Atoi(k)
			if err == nil && n > bestSize {
				bestSize, bestPath = n, v
			}
		}
		return bestPath
	}
	return ""
}

// ── quilt.mod.json ───────────────────────────────────────────────────────
// Формат Quilt відрізняється від Fabric: усе (id/version/depends/метадані)
// вкладено в об'єкт "quilt_loader", а "depends" — це МАСИВ (не мапа, як у
// Fabric), елементи якого — або рядок-ID мода (коротка форма, завжди
// обов'язкова), або об'єкт {"id": "...", "optional": true/false, ...}.
// Через цю відмінність структури парсинг через fabricModJSON (map)
// раніше просто нічого не знаходив на реальних Quilt-native модах.
type quiltDependEntry struct {
	// раз перевірка на "чистий рядок" (short-form), раз на об'єкт —
	// json.RawMessage дозволяє розрізнити обидва варіанти нижче.
	raw json.RawMessage
}

func (q *quiltDependEntry) UnmarshalJSON(b []byte) error {
	q.raw = append([]byte(nil), b...)
	return nil
}

type quiltModJSON struct {
	QuiltLoader struct {
		ID       string `json:"id"`
		Version  string `json:"version"`
		Metadata struct {
			Name         string          `json:"name"`
			Description  string          `json:"description"`
			Icon         json.RawMessage `json:"icon"`
			Contributors json.RawMessage `json:"contributors"`
		} `json:"metadata"`
		Depends []quiltDependEntry `json:"depends"`
	} `json:"quilt_loader"`
}

// excludedQuiltDeps — псевдо-залежності, завжди присутні за визначенням
// (сам лоадер, гра, Java). "fabric" (Quilt QSL-режим сумісності) НЕ
// виключаємо навмисно — якщо мод явно вимагає Fabric API/QSL, це
// реальна залежність, яку треба показати й дозволити встановити.
var excludedQuiltDeps = map[string]bool{
	"quilt_loader": true, "minecraft": true, "java": true,
}

func parseQuiltJSON(f *zip.File, jarPath string) *jarMeta {
	rc, err := f.Open()
	if err != nil {
		return nil
	}
	defer rc.Close()
	var meta quiltModJSON
	if json.NewDecoder(rc).Decode(&meta) != nil {
		return nil
	}
	id := meta.QuiltLoader.ID
	if id == "" {
		id = baseNameNoExt(jarPath)
	}
	name := meta.QuiltLoader.Metadata.Name
	if name == "" {
		name = id
	}
	version := meta.QuiltLoader.Version
	if version == "" {
		version = "unknown"
	}
	var deps []string
	for _, entry := range meta.QuiltLoader.Depends {
		var depID string
		var optional bool
		// Коротка форма: сам рядок — ID мода.
		var asString string
		if json.Unmarshal(entry.raw, &asString) == nil {
			depID = asString
		} else {
			var asObj struct {
				ID       string `json:"id"`
				Optional bool   `json:"optional"`
			}
			if json.Unmarshal(entry.raw, &asObj) == nil {
				depID = asObj.ID
				optional = asObj.Optional
			}
		}
		if depID == "" || optional || depID == id || excludedQuiltDeps[depID] {
			continue
		}
		deps = append(deps, depID)
	}
	return &jarMeta{
		ID: id, Name: name, Version: version, Description: meta.QuiltLoader.Metadata.Description,
		LoaderType: "quilt", Loaders: []string{"quilt"}, Depends: deps,
		IconPath: fabricIconPath(meta.QuiltLoader.Metadata.Icon), // формат "icon" однаковий у Fabric і Quilt
	}
}

// ── META-INF/mods.toml (Forge/NeoForge) — міні-парсер ───────────────────
// Формат TOML для Forge/NeoForge:
//   [[mods]]
//     modId="mymod"          ← ID/назва/версія САМОГО мода
//     version="1.0"
//   [[dependencies.mymod]]   ← "mymod" тут — це modId мода, ЩО ОГОЛОШУЄ
//     modId="forge"          ←   залежність (той самий, що й вище!), а
//     mandatory=true         ←   РЕАЛЬНА залежність — це modId ВСЕРЕДИНІ
//                                блоку, не в заголовку [[dependencies.X]].
// Раніше код помилково брав X із заголовка [[dependencies.X]] як ID
// залежності — а це ID самого мода (він завжди дорівнює modId з [[mods]]),
// тому мод фактично "залежав сам від себе", а справжні залежності
// (forge/minecraft/інші моди, вказані через modId= у тілі блоку)
// повністю ігнорувались. Тепер: (1) метадані мода читаємо ТІЛЬКИ з
// блоку [[mods]], а не з усього файлу (інакше modId="forge" з блоку
// залежностей міг перезаписати ID самого мода); (2) залежність — це
// modId=, mandatory= З ТІЛА блоку [[dependencies.*]], а не заголовок.
//
// Go-регулярки (RE2) не підтримують зворотні посилання (\2) і lookahead
// (?=...), тому лапки (одинарні/подвійні) і межі блоків обробляються
// окремими регулярками/вручну по індексах — без цих конструкцій.
var tomlKeyReDouble = regexp.MustCompile(`(?m)^\s*([a-zA-Z0-9_]+)\s*=\s*"(.*?)"`)
var tomlKeyReSingle = regexp.MustCompile(`(?m)^\s*([a-zA-Z0-9_]+)\s*=\s*'(.*?)'`)
var tomlBoolReDouble = regexp.MustCompile(`(?m)^\s*([a-zA-Z0-9_]+)\s*=\s*(true|false)\s*$`)

// tomlBlockHeaderRe знаходить заголовок БУДЬ-якого TOML-блоку виду
// [[щось]] або [щось] (не лише dependencies) — потрібен, щоб коректно
// визначати межі кожного блоку (тіло = до наступного будь-якого
// заголовка, а не лише наступного [[dependencies.*]]).
var tomlBlockHeaderRe = regexp.MustCompile(`(?m)^\s*\[\[?([^\]]+)\]\]?\s*$`)

// tomlKeyValues парсить прості "key = value" (рядки/булі) у межах [start,end)
// рядка s — і double-quoted, і single-quoted, і булеві значення.
func tomlKeyValues(s string, start, end int) map[string]string {
	body := s[start:end]
	vals := map[string]string{}
	for _, m := range tomlKeyReDouble.FindAllStringSubmatch(body, -1) {
		if _, ok := vals[m[1]]; !ok {
			vals[m[1]] = m[2]
		}
	}
	for _, m := range tomlKeyReSingle.FindAllStringSubmatch(body, -1) {
		if _, ok := vals[m[1]]; !ok {
			vals[m[1]] = m[2]
		}
	}
	for _, m := range tomlBoolReDouble.FindAllStringSubmatch(body, -1) {
		if _, ok := vals[m[1]]; !ok {
			vals[m[1]] = m[2]
		}
	}
	return vals
}

func parseModsToml(f *zip.File, jarPath string) *jarMeta {
	rc, err := f.Open()
	if err != nil {
		return nil
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil
	}
	s := string(data)

	// Знаходимо межі КОЖНОГО блоку [[...]]/[...] у файлі — потрібно, щоб
	// не змішувати поля різних блоків (мод сам може мати кілька
	// [[mods]] у модпаках, і кожен [[dependencies.X]] — окремий блок).
	headers := tomlBlockHeaderRe.FindAllStringSubmatchIndex(s, -1)
	type block struct {
		name      string // вміст дужок, напр. "mods" або "dependencies.mymod"
		bodyStart int
		bodyEnd   int
	}
	blocks := make([]block, 0, len(headers))
	for i, h := range headers {
		name := s[h[2]:h[3]]
		bodyStart := h[1]
		bodyEnd := len(s)
		if i+1 < len(headers) {
			bodyEnd = headers[i+1][0]
		}
		blocks = append(blocks, block{name: name, bodyStart: bodyStart, bodyEnd: bodyEnd})
	}

	// Метадані самого мода — ЛИШЕ з першого блоку [[mods]] (не з усього
	// файлу), щоб поля з блоків залежностей (теж modId=, version=) не
	// підмінили дані мода.
	var vals map[string]string
	for _, b := range blocks {
		if b.name == "mods" {
			vals = tomlKeyValues(s, b.bodyStart, b.bodyEnd)
			break
		}
	}
	if vals == nil {
		vals = map[string]string{}
	}

	name := vals["displayName"]
	if name == "" {
		name = vals["name"]
	}
	if name == "" {
		name = vals["modId"]
	}
	if name == "" {
		name = baseNameNoExt(jarPath)
	}

	selfID := vals["modId"]

	// Реальні залежності: для кожного блоку [[dependencies.<будь-що>]]
	// беремо modId= З ТІЛА блоку (не з заголовка!) — це і є ID мода, від
	// якого залежимо. mandatory=true (Forge) — обов'язкова залежність;
	// NeoForge з часом перейшов на type="required", тому перевіряємо
	// обидва варіанти. Власний ID мода (форвардна самозалежність,
	// зустрічається в деяких погано згенерованих mods.toml) і
	// службові псевдо-моди (forge/neoforge/minecraft — вони завжди
	// присутні за визначенням лоадера) виключаємо зі списку залежностей.
	depsExcluded := map[string]bool{"forge": true, "neoforge": true, "minecraft": true}
	if selfID != "" {
		depsExcluded[selfID] = true
	}
	seenDeps := map[string]bool{}
	var deps []string
	for _, b := range blocks {
		if !strings.HasPrefix(b.name, "dependencies.") {
			continue
		}
		bvals := tomlKeyValues(s, b.bodyStart, b.bodyEnd)
		depID := bvals["modId"]
		if depID == "" || depsExcluded[depID] || seenDeps[depID] {
			continue
		}
		mandatory := bvals["mandatory"] == "true" || bvals["type"] == "required"
		if mandatory {
			deps = append(deps, depID)
			seenDeps[depID] = true
		}
	}

	version := vals["version"]
	if version == "" {
		version = "unknown"
	}
	loaderType := "forge"
	if strings.Contains(f.Name, "neoforge") {
		loaderType = "neoforge"
	}
	return &jarMeta{
		ID: selfID, Name: name, Version: version,
		Description: vals["description"], Author: vals["authors"],
		LoaderType: loaderType, Loaders: []string{loaderType}, Depends: deps,
		IconPath: vals["logoFile"],
	}
}

// ── mcmod.info (старий Forge, MC 1.7–1.12) ──────────────────────────────
func parseMcmodInfo(f *zip.File, jarPath string) *jarMeta {
	rc, err := f.Open()
	if err != nil {
		return nil
	}
	defer rc.Close()
	var list []struct {
		ModID        string   `json:"modid"`
		Name         string   `json:"name"`
		Version      string   `json:"version"`
		Description  string   `json:"description"`
		AuthorList   []string `json:"authorList"`
		RequiredMods []string `json:"requiredMods"` // напр. "forge@[14.23,)" або просто "somemod"
	}
	if json.NewDecoder(rc).Decode(&list) != nil || len(list) == 0 {
		return nil
	}
	m := list[0]
	author := ""
	if len(m.AuthorList) > 0 {
		author = m.AuthorList[0]
	}
	name := m.Name
	if name == "" {
		name = m.ModID
	}
	version := m.Version
	if version == "" {
		version = "unknown"
	}
	// requiredMods містить modId (іноді з версійним обмеженням через "@",
	// напр. "forge@[14.23.5.2860,)") — залежність без псевдо-модів
	// лоадера/гри, які завжди присутні за визначенням.
	depsExcluded := map[string]bool{"forge": true, "minecraft": true, "mcp": true, "fml": true}
	if m.ModID != "" {
		depsExcluded[m.ModID] = true
	}
	var deps []string
	for _, raw := range m.RequiredMods {
		depID := raw
		if i := strings.IndexByte(depID, '@'); i >= 0 {
			depID = depID[:i]
		}
		depID = strings.TrimSpace(depID)
		if depID == "" || depsExcluded[depID] {
			continue
		}
		deps = append(deps, depID)
	}
	return &jarMeta{
		ID: m.ModID, Name: name, Version: version, Description: m.Description,
		Author: author, LoaderType: "forge", Loaders: []string{"forge"}, Depends: deps,
	}
}

// attachIcon шукає іконку мода в zip і кодує в data URL (base64).
// Спершу — за шляхом з метаданих самого мода (logoFile у mods.toml для
// Forge/NeoForge, icon у fabric.mod.json/quilt.mod.json) — це ЄДИНИЙ
// надійний спосіб, бо автори кладуть іконку куди завгодно (типово
// assets/<modid>/icon.png, а не в корінь jar). Раніше код шукав лише
// файли з жорстко зашитими іменами pack.png/icon.png у КОРЕНІ архіву —
// спрацьовувало випадково (лише коли автор випадково поклав файл саме
// туди й так назвав), а для більшості модів (зокрема типових fabric-
// модів з іконкою в assets/) не знаходило нічого, хоча іконка в jar
// фізично була. Шлях з мета-даних (logoFile/icon) пробуємо і як є, і
// без ведучого "/" (деякі автори пишуть з ним, zip-записи — без).
func attachIcon(zr *zip.ReadCloser, meta *jarMeta) {
	if meta.IconPath != "" {
		want := strings.TrimPrefix(meta.IconPath, "/")
		for _, f := range zr.File {
			if f.Name == want || f.Name == meta.IconPath {
				if data := readZipFileDataURL(f); data != "" {
					meta.IconData = data
					return
				}
			}
		}
	}
	for _, f := range zr.File {
		if f.Name == "pack.png" || f.Name == "icon.png" {
			if data := readZipFileDataURL(f); data != "" {
				meta.IconData = data
				return
			}
		}
	}
}

func readZipFileDataURL(f *zip.File) string {
	if f.UncompressedSize64 > 3<<20 { // > 3 МБ — не іконка
		return ""
	}
	rc, err := f.Open()
	if err != nil {
		return ""
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil || len(data) == 0 {
		return ""
	}
	ext := strings.ToLower(filepath.Ext(f.Name))
	mime := "image/png"
	switch ext {
	case ".jpg", ".jpeg":
		mime = "image/jpeg"
	case ".gif":
		mime = "image/gif"
	case ".svg":
		mime = "image/svg+xml"
	case ".webp":
		mime = "image/webp"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// ── ToggleMod / DisableMod / EnableMod / ResolveJarPath ─────────────────
func (m *Manager) ToggleMod(modsDir, fileName string, enabled bool) error {
	path := filepath.Join(modsDir, fileName)
	if enabled {
		newName := strings.TrimSuffix(fileName, disabledSuffix)
		if newName == fileName {
			return nil
		}
		if _, err := os.Stat(path); err != nil {
			return err
		}
		return os.Rename(path, filepath.Join(modsDir, newName))
	}
	if strings.HasSuffix(fileName, disabledSuffix) {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		return err
	}
	return os.Rename(path, filepath.Join(modsDir, fileName+".disabled"))
}

func (m *Manager) DisableMod(modsDir, fileName string) error {
	return m.ToggleMod(modsDir, fileName, false)
}

func (m *Manager) EnableMod(modsDir, fileName string) error {
	return m.ToggleMod(modsDir, fileName, true)
}

func (m *Manager) ResolveJarPath(modsDir, fileName string) string {
	if strings.HasSuffix(fileName, disabledSuffix) {
		return filepath.Join(modsDir, fileName)
	}
	if strings.HasSuffix(fileName, ".jar") {
		return filepath.Join(modsDir, fileName)
	}
	return filepath.Join(modsDir, fileName+".jar")
}

// DeleteMod видаляє файл мода (і його .disabled-двійник, якщо є).
func (m *Manager) DeleteMod(modsDir, fileName string) error {
	paths := []string{filepath.Join(modsDir, fileName)}
	if strings.HasSuffix(fileName, ".jar") {
		paths = append(paths, filepath.Join(modsDir, fileName+".disabled"))
	} else if strings.HasSuffix(fileName, disabledSuffix) {
		paths = append(paths, filepath.Join(modsDir, strings.TrimSuffix(fileName, ".disabled")))
	}
	var firstErr error
	for _, p := range paths {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ReadModInfo — сумісний легкий читач метаданих (використовується
// AI-діагностикою консолі).
func (m *Manager) ReadModInfo(jarPath string) (modInfo, error) {
	meta := m.readJarCached(jarPath)
	if meta == nil {
		return modInfo{}, fmt.Errorf("no mod metadata found")
	}
	return modInfo{Name: meta.Name, Version: meta.Version}, nil
}

type modInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}