package modgraph

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MaxMods — стеля кількості jar, які аналізуємо (захист від гігантських тек).
const MaxMods = 600

// Формат вимкненого мода в лаунчері: "X.jar.disabled" (+ застарілий префікс
// ".disabled." зі старого JavaFX-лаунчера).
const (
	disabledSuffix       = ".disabled"
	legacyDisabledPrefix = ".disabled."
)

// ModFile — один jar у теці mods/ з розібраним маніфестом.
type ModFile struct {
	File    string
	Enabled bool
	Manifest
	// MTime — час зміни файлу (unix-секунди): за ним «найновіша копія» дубліката.
	MTime int64
}

// Scan читає теку mods/ і повертає розібрані jar (відсортовані за іменем).
// Підтеки, симлінки та не-jar/zip файли пропускаються.
func Scan(modsDir string) ([]ModFile, error) {
	info, err := os.Lstat(modsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("каталог модів недоступний: %s", modsDir)
	}
	entries, err := os.ReadDir(modsDir)
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
	})
	var out []ModFile
	for _, e := range entries {
		if len(out) >= MaxMods {
			break
		}
		name := e.Name()
		lower := strings.ToLower(name)
		enabled := true
		base := lower
		for strings.HasSuffix(base, disabledSuffix) {
			enabled = false
			base = strings.TrimSuffix(base, disabledSuffix)
		}
		if strings.HasPrefix(base, legacyDisabledPrefix) {
			enabled = false
			base = strings.TrimPrefix(base, legacyDisabledPrefix)
		}
		if !strings.HasSuffix(base, ".jar") && !strings.HasSuffix(base, ".zip") {
			continue
		}
		fi, err := e.Info()
		if err != nil || fi.Mode()&os.ModeSymlink != 0 || !fi.Mode().IsRegular() {
			continue
		}
		mf := ModFile{File: name, Enabled: enabled, MTime: fi.ModTime().Unix()}
		mf.Manifest = ReadJar(filepath.Join(modsDir, name))
		out = append(out, mf)
	}
	return out, nil
}

// Node — вузол графа (один jar).
type Node struct {
	ID           string       `json:"id"`
	File         string       `json:"file"`
	DisplayName  string       `json:"displayName"`
	Version      string       `json:"version"`
	Enabled      bool         `json:"enabled"`
	Loader       string       `json:"loader"`
	Corrupted    bool         `json:"corrupted"`
	// Description — опис із маніфесту (може бути порожнім).
	Description string `json:"description,omitempty"`
	// Color — домінантний колір іконки (#RRGGBB); заповнює бекенд-обгортка,
	// порожній, якщо в моду нема іконки.
	Color        string       `json:"color,omitempty"`
	Dependencies []Dependency `json:"dependencies"`
	// Dependents — скільки увімкнених модів вимагають цей (для швидкого огляду).
	Dependents int `json:"dependents"`
}

// Edge — залежність «From потребує To».
type Edge struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Optional   bool   `json:"optional"`
	Constraint string `json:"constraint"`
}

// Missing — залежність, якої немає серед УВІМКНЕНИХ модів.
type Missing struct {
	ID         string `json:"id"`
	RequiredBy string `json:"requiredBy"`
	// RequiredByAll — усі мод-ID, яким вона потрібна (RequiredBy — перший).
	RequiredByAll []string `json:"requiredByAll"`
	Optional      bool     `json:"optional"`
	// DisabledFile — якщо залежність є, але вимкнена: ім'я її файлу.
	DisabledFile string `json:"disabledFile,omitempty"`
}

// Graph — повний граф залежностей збірки.
type Graph struct {
	Nodes   []Node    `json:"nodes"`
	Edges   []Edge    `json:"edges"`
	Missing []Missing `json:"missing"`
	// Cycles — кола обов'язкових залежностей (рідкість, але лоадер їх не любить).
	Cycles [][]string `json:"cycles"`
}

// BuildGraph будує граф за текою mods/.
func BuildGraph(modsDir string) (Graph, error) {
	mods, err := Scan(modsDir)
	if err != nil {
		return Graph{}, err
	}
	return FromMods(mods), nil
}

// aliases повертає варіанти запису ID (дефіс/підкреслення взаємозамінні).
func aliases(id string) []string {
	id = strings.ToLower(strings.TrimSpace(id))
	return []string{id, strings.ReplaceAll(id, "-", "_"), strings.ReplaceAll(id, "_", "-")}
}

// Index — швидкий пошук мода за ID з урахуванням provides та вкладених jar.
type Index map[string]int

// BuildIndex індексує моди (за індексом у зрізі) за основним ID, provides та
// embedded. Основний ID має пріоритет над provides.
func BuildIndex(mods []ModFile, enabledOnly bool) Index {
	idx := Index{}
	add := func(id string, i int, override bool) {
		for _, a := range aliases(id) {
			if a == "" {
				continue
			}
			if _, ok := idx[a]; ok && !override {
				continue
			}
			idx[a] = i
		}
	}
	for i, m := range mods {
		if enabledOnly && !m.Enabled {
			continue
		}
		if m.ModID != "" {
			add(m.ModID, i, true)
		}
	}
	for i, m := range mods {
		if enabledOnly && !m.Enabled {
			continue
		}
		for _, p := range m.Provides {
			add(p, i, false)
		}
		for _, p := range m.Embedded {
			add(p, i, false)
		}
	}
	return idx
}

// Lookup шукає мод за ID.
func (idx Index) Lookup(id string) (int, bool) {
	for _, a := range aliases(id) {
		if i, ok := idx[a]; ok {
			return i, true
		}
	}
	return 0, false
}

// IsNoise — ID лоадера/гри/Java, який не має лежати в mods/.
func IsNoise(id string) bool {
	id = strings.ToLower(strings.TrimSpace(id))
	return NoiseIDs[id] || strings.HasPrefix(id, "java")
}

// FromMods будує граф зі вже розібраних модів.
func FromMods(mods []ModFile) Graph {
	g := Graph{Nodes: []Node{}, Edges: []Edge{}, Missing: []Missing{}, Cycles: [][]string{}}
	enabled := BuildIndex(mods, true)
	all := BuildIndex(mods, false)

	nodeID := func(m ModFile) string {
		if m.ModID != "" {
			return m.ModID
		}
		return strings.ToLower(strings.TrimSuffix(strings.TrimSuffix(strings.ToLower(m.File), disabledSuffix), ".jar"))
	}
	dependents := map[string]int{}
	missingIdx := map[string]int{}

	for _, m := range mods {
		if !m.Enabled || m.Corrupted {
			continue
		}
		from := nodeID(m)
		for _, d := range m.Dependencies {
			did := strings.ToLower(strings.TrimSpace(d.ID))
			if did == "" || IsNoise(did) || did == m.ModID {
				continue
			}
			if i, ok := enabled.Lookup(did); ok {
				to := nodeID(mods[i])
				if to == from {
					continue
				}
				g.Edges = append(g.Edges, Edge{From: from, To: to, Optional: d.Optional, Constraint: d.Constraint})
				if !d.Optional {
					dependents[to]++
				}
				continue
			}
			// Під-модулі Fabric API лежать jar-in-jar усередині самого API.
			if strings.HasPrefix(did, "fabric-") && did != "fabric-api" {
				if _, ok := enabled.Lookup("fabric-api"); ok {
					continue
				}
			}
			key := did
			if k, ok := missingIdx[key]; ok {
				cur := &g.Missing[k]
				cur.RequiredByAll = appendUnique(cur.RequiredByAll, from)
				// обов'язкова вимога «перебиває» опціональну
				cur.Optional = cur.Optional && d.Optional
				continue
			}
			miss := Missing{ID: did, RequiredBy: from, RequiredByAll: []string{from}, Optional: d.Optional}
			if i, ok := all.Lookup(did); ok && !mods[i].Enabled {
				miss.DisabledFile = mods[i].File
			}
			missingIdx[key] = len(g.Missing)
			g.Missing = append(g.Missing, miss)
		}
	}
	for _, m := range mods {
		g.Nodes = append(g.Nodes, Node{
			ID: nodeID(m), File: m.File, DisplayName: firstNonEmpty(m.Name, nodeID(m)),
			Version: m.Version, Enabled: m.Enabled, Loader: m.Loader, Corrupted: m.Corrupted, Description: m.Description,
			Dependencies: nonNilDeps(m.Dependencies), Dependents: dependents[nodeID(m)],
		})
	}
	g.Cycles = findCycles(g.Edges)
	return g
}

func nonNilDeps(d []Dependency) []Dependency {
	if d == nil {
		return []Dependency{}
	}
	out := make([]Dependency, 0, len(d))
	for _, x := range d {
		if IsNoise(x.ID) {
			continue
		}
		out = append(out, x)
	}
	return out
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

// Impact — що зламається, якщо прибрати мод.
type Impact struct {
	ModID string `json:"modId"`
	// Breaks — моди, які ВИМАГАЮТЬ цей мод (транзитивно) і перестануть
	// завантажуватись; у порядку віддалення від видаленого.
	Breaks []string `json:"breaks"`
	// Degrades — моди, що лише опційно використовують його (втратять
	// інтеграцію, але запустяться). Лише прямі опційні зв'язки.
	Degrades []string `json:"degrades"`
}

// RemovalImpact — зворотний обхід графа за ОБОВ'ЯЗКОВИМИ ребрами (метод
// Nivora removal_impact), опційні зв'язки виділено окремо.
func RemovalImpact(g Graph, modID string) Impact {
	start := strings.ToLower(strings.TrimSpace(modID))
	reverse := map[string][]string{}
	optional := map[string][]string{}
	for _, e := range g.Edges {
		to := strings.ToLower(e.To)
		if e.Optional {
			optional[to] = append(optional[to], e.From)
		} else {
			reverse[to] = append(reverse[to], e.From)
		}
	}
	imp := Impact{ModID: modID, Breaks: []string{}, Degrades: []string{}}
	seen := map[string]bool{start: true}
	queue := []string{start}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, dep := range reverse[cur] {
			k := strings.ToLower(dep)
			if seen[k] {
				continue
			}
			seen[k] = true
			imp.Breaks = append(imp.Breaks, dep)
			queue = append(queue, k)
		}
	}
	for _, dep := range optional[start] {
		if !seen[strings.ToLower(dep)] {
			imp.Degrades = appendUnique(imp.Degrades, dep)
		}
	}
	return imp
}

// findCycles знаходить кола обов'язкових залежностей (DFS за кольорами).
func findCycles(edges []Edge) [][]string {
	adj := map[string][]string{}
	for _, e := range edges {
		if !e.Optional {
			adj[e.From] = append(adj[e.From], e.To)
		}
	}
	keys := make([]string, 0, len(adj))
	for k := range adj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	const (
		white = iota
		grey
		black
	)
	color := map[string]int{}
	var stack []string
	cycles := [][]string{}
	seenCycle := map[string]bool{}
	var visit func(string)
	visit = func(n string) {
		color[n] = grey
		stack = append(stack, n)
		for _, next := range adj[n] {
			switch color[next] {
			case white:
				visit(next)
			case grey:
				// коло: від next до вершини стека
				for i := len(stack) - 1; i >= 0; i-- {
					if stack[i] == next {
						cyc := append([]string{}, stack[i:]...)
						sig := canonicalCycle(cyc)
						if !seenCycle[sig] && len(cycles) < 20 {
							seenCycle[sig] = true
							cycles = append(cycles, cyc)
						}
						break
					}
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[n] = black
	}
	for _, k := range keys {
		if color[k] == white {
			visit(k)
		}
	}
	return cycles
}

func canonicalCycle(c []string) string {
	min := 0
	for i := range c {
		if c[i] < c[min] {
			min = i
		}
	}
	rot := append(append([]string{}, c[min:]...), c[:min]...)
	return strings.Join(rot, ">")
}
