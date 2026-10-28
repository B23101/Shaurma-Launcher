package autorepair

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"shaurma-launcher-backend/internal/modgraph"
)

const maxCrashReportSize = 2 * 1024 * 1024

func modsDirOf(gameDir string) string { return filepath.Join(gameDir, "mods") }

func dependencyID(m []string) string {
	for _, i := range []int{5, 6, 7, 4} {
		if i < len(m) && m[i] != "" {
			return strings.ToLower(strings.TrimSpace(m[i]))
		}
	}
	return ""
}

func isWrongLoader(mod modgraph.ModFile, loader string, byID map[string]modgraph.ModFile) bool {
	if len(mod.Loaders) == 0 || mod.Corrupted {
		return false
	}
	has := func(names ...string) bool {
		for _, l := range mod.Loaders {
			for _, n := range names {
				if l == n {
					return true
				}
			}
		}
		return false
	}
	_, connector := byID["connector"]
	if _, ok := byID["connectormod"]; ok {
		connector = true // новіші збірки Sinytra Connector
	}
	switch loader {
	case "fabric":
		return !has("fabric")
	case "quilt":
		return !has("fabric", "quilt")
	case "forge":
		// Sinytra Connector запускає Fabric-моди на Forge — тоді це не помилка.
		if connector && has("fabric", "quilt") {
			return false
		}
		return !has("forge")
	case "neoforge":
		if connector && has("fabric", "quilt") {
			return false
		}
		return !has("forge", "neoforge")
	}
	return false
}

func filesForIDs(ids []string, byID map[string]modgraph.ModFile) []string {
	var files []string
	for _, id := range ids {
		key := strings.ToLower(strings.TrimSpace(id))
		for _, k := range []string{key, strings.ReplaceAll(key, "-", "_"), strings.ReplaceAll(key, "_", "-")} {
			if m, ok := byID[k]; ok {
				files = append(files, m.File)
				break
			}
		}
	}
	return unique(files)
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]`)

func filesForNames(names []string, mods []modgraph.ModFile) []string {
	var files []string
	for _, name := range names {
		needle := nonAlnum.ReplaceAllString(strings.ToLower(name), "")
		if len(needle) < 3 {
			continue
		}
		for _, mod := range mods {
			first := strings.Split(strings.ToLower(mod.File), "-")[0]
			if needle == nonAlnum.ReplaceAllString(mod.ModID, "") || needle == nonAlnum.ReplaceAllString(first, "") {
				files = append(files, mod.File)
				break
			}
		}
	}
	return unique(files)
}

func crashReportSuspects(report string, mods []modgraph.ModFile) []string {
	if report == "" {
		return nil
	}
	byID := map[string]modgraph.ModFile{}
	byFile := map[string]string{}
	for _, m := range mods {
		if m.ModID != "" {
			byID[m.ModID] = m
		}
		byFile[strings.ToLower(m.File)] = m.File
	}
	var files []string
	if block := suspectBlock.FindStringSubmatch(report); block != nil {
		var ids []string
		for _, m := range crashSuspectID.FindAllStringSubmatch(block[1], -1) {
			ids = append(ids, strings.ToLower(m[1]))
		}
		files = append(files, filesForIDs(ids, byID)...)
		ids = nil
		for _, m := range parenID.FindAllStringSubmatch(block[1], -1) {
			ids = append(ids, strings.ToLower(m[1]))
		}
		files = append(files, filesForIDs(ids, byID)...)
	}
	for _, m := range crashModFile.FindAllStringSubmatch(report, -1) {
		if f, ok := byFile[strings.ToLower(m[1])]; ok {
			files = append(files, f)
		}
	}
	var out []string
	for _, f := range unique(files) {
		// Ванільні/завантажувальні файли не вважаємо винуватцями.
		if !loaderFileName.MatchString(f) {
			out = append(out, f)
		}
	}
	return head(out, 10)
}

func latestCrashReport(gameDir string, since time.Time) string {
	if gameDir == "" {
		return ""
	}
	dir := filepath.Join(gameDir, "crash-reports")
	if fi, err := os.Lstat(dir); err != nil || fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		return ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	var newest string
	var newestTime int64 = -1
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".txt") {
			continue
		}
		fi, err := e.Info()
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		mt := fi.ModTime().Unix()
		if !since.IsZero() && mt < since.Unix() {
			continue
		}
		if mt > newestTime {
			newestTime, newest = mt, e.Name()
		}
	}
	if newest == "" {
		return ""
	}
	f, err := os.Open(filepath.Join(dir, newest))
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, maxCrashReportSize)
	n, _ := f.Read(buf)
	return strings.ToValidUTF8(string(buf[:n]), "�")
}

func brokenConfigs(text, gameDir string) []string {
	if gameDir == "" {
		return nil
	}
	root := filepath.Join(gameDir, "config")
	if fi, err := os.Lstat(root); err != nil || fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		return nil
	}
	var candidates []string
	for _, m := range configPath.FindAllStringSubmatch(text, -1) {
		candidates = append(candidates, m[1])
	}
	for _, m := range forgeConfigFile.FindAllStringSubmatch(text, -1) {
		candidates = append(candidates, m[1])
	}
	var found []string
	for _, c := range candidates {
		rel := strings.TrimLeft(strings.TrimSpace(strings.ReplaceAll(c, "\\", "/")), "/")
		path := SafeChild(root, rel)
		if path == "" || !isFile(path) {
			// Forge пише лише ім'я файлу — шукаємо його в корені config.
			path = SafeChild(root, filepath.Base(rel))
			if path == "" || !isFile(path) {
				continue
			}
		}
		r, err := filepath.Rel(root, path)
		if err != nil {
			continue
		}
		found = append(found, "config/"+filepath.ToSlash(r))
	}
	return unique(found)
}

func isFile(p string) bool {
	fi, err := os.Lstat(p)
	return err == nil && fi.Mode().IsRegular()
}

// SafeChild повертає root/relative, якщо шлях не виходить за root і не є
// симлінком; інакше "".
func SafeChild(root, relative string) string {
	if relative == "" || strings.ContainsRune(relative, 0) || filepath.IsAbs(relative) {
		return ""
	}
	for _, part := range strings.Split(filepath.ToSlash(relative), "/") {
		if part == ".." || part == "" {
			return ""
		}
	}
	candidate := filepath.Join(root, filepath.FromSlash(relative))
	absRoot, err1 := filepath.Abs(root)
	absCand, err2 := filepath.Abs(candidate)
	if err1 != nil || err2 != nil {
		return ""
	}
	if rel, err := filepath.Rel(absRoot, absCand); err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	if fi, err := os.Lstat(candidate); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return ""
	}
	return candidate
}

func evidenceOf(text string, needles []string) []string {
	if text == "" {
		return nil
	}
	var lowered []string
	for _, n := range needles {
		if n != "" {
			lowered = append(lowered, strings.ToLower(n))
		}
	}
	var out []string
	for _, line := range strings.Split(text, "\n") {
		l := strings.ToLower(line)
		for _, n := range lowered {
			if strings.Contains(l, n) {
				s := strings.TrimSpace(line)
				if s != "" && !contains(out, s) {
					out = append(out, clipStr(s, 300))
				}
				break
			}
		}
		if len(out) >= maxEvidence {
			break
		}
	}
	return out
}

func tailEvidence(lines []string) []string {
	for i := len(lines) - 1; i > maxInt(-1, len(lines)-200); i-- {
		if errorWord.MatchString(lines[i]) {
			var out []string
			for _, l := range lines[i:minInt(len(lines), i+maxEvidence)] {
				if s := strings.TrimSpace(l); s != "" {
					out = append(out, clipStr(s, 300))
				}
			}
			return out
		}
	}
	var out []string
	for _, l := range lines[maxInt(0, len(lines)-maxEvidence):] {
		if s := strings.TrimSpace(l); s != "" {
			out = append(out, clipStr(s, 300))
		}
	}
	return out
}

func clipStr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func unique(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, v := range values {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func head(values []string, n int) []string {
	if len(values) > n {
		return values[:n]
	}
	return values
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// stringList приймає []string або []any (JSON з інтерфейсу).
func stringList(v any) []string {
	switch x := v.(type) {
	case []string:
		return append([]string(nil), x...)
	case []any:
		out := make([]string, 0, len(x))
		for _, i := range x {
			if s, ok := i.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func paramsEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok || fmt.Sprint(va) != fmt.Sprint(vb) {
			return false
		}
	}
	return true
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var firstIntRe = regexp.MustCompile(`\d{1,2}`)

func firstInt(v string) int {
	if m := firstIntRe.FindString(v); m != "" {
		n, _ := strconv.Atoi(m)
		return n
	}
	return 0
}

var firstVersionRe = regexp.MustCompile(`\d+(?:\.\d+)+(?:[-+][\w.]+)?|\d+`)

func firstVersion(v string) string { return firstVersionRe.FindString(v) }

var prerelease = regexp.MustCompile(`(?i)alpha|beta|rc|pre|snapshot|dev`)
var digitsRe = regexp.MustCompile(`\d+`)

// versionKey: 0.16.10 > 0.16.9, 47.3.0 > 47.2.20; пре-релізи нижчі за стабільні.
func versionKey(v string) ([]int, int) {
	core := v
	if i := strings.IndexAny(core, "-+"); i >= 0 {
		core = core[:i]
	}
	var nums []int
	for _, m := range digitsRe.FindAllString(core, 6) {
		n, _ := strconv.Atoi(m)
		nums = append(nums, n)
	}
	stable := 1
	if prerelease.MatchString(v) {
		stable = 0
	}
	return nums, stable
}

func cmpNums(a, b []int) int {
	for i := 0; i < maxInt(len(a), len(b)); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func cmpVersion(a, b string) int {
	na, sa := versionKey(a)
	nb, sb := versionKey(b)
	if c := cmpNums(na, nb); c != 0 {
		return c
	}
	return sa - sb
}

// PickLoaderVersion вибирає версію завантажувача: найновіша стабільна, не
// нижча за minimum. Списки версій у Fabric/Quilt ідуть від нових до старих, у
// Forge/NeoForge — навпаки, тому порядок входу не використовується.
func PickLoaderVersion(choices []string, minimum string) string {
	var values []string
	for _, c := range choices {
		if c = strings.TrimSpace(c); c != "" {
			values = append(values, c)
		}
	}
	values = unique(values)
	if len(values) == 0 {
		return ""
	}
	var minNums []int
	if minimum != "" {
		minNums, _ = versionKey(minimum)
	}
	var eligible, stable []string
	for _, v := range values {
		n, _ := versionKey(v)
		if minNums == nil || cmpNums(n, minNums) >= 0 {
			eligible = append(eligible, v)
			if !prerelease.MatchString(v) {
				stable = append(stable, v)
			}
		}
	}
	pool := stable
	if len(pool) == 0 {
		pool = eligible
	}
	if len(pool) == 0 {
		return ""
	}
	best := pool[0]
	for _, v := range pool[1:] {
		if cmpVersion(v, best) > 0 {
			best = v
		}
	}
	return best
}

func humanMemory(mb int) string {
	gib := float64(mb) / 1024
	if gib >= 1 {
		s := fmt.Sprintf("%.1f", gib)
		s = strings.TrimSuffix(s, "0")
		s = strings.TrimSuffix(s, ".")
		return s + " ГБ"
	}
	return fmt.Sprintf("%d МБ", mb)
}
