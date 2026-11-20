package main

// ai_agent_review.go — review_build: «модератор якості» збірки для ШІ. Читає збірку (нічого не змінює)
// і дає оцінку: кількість і вага модів, схожі за роллю моди, відсутній фундамент оптимізації,
// підозрілі версії (alpha/dev, інша версія гри в імені файлу), рекомендована RAM, проблеми лаунчера.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type aiReviewItem struct {
	Severity string `json:"severity"` // error | warn | info
	Kind     string `json:"kind"`
	Detail   string `json:"detail"`
	Fix      string `json:"fix,omitempty"`
}

// aiRoleGroups — моди, що виконують одну роль: більше одного в збірці — майже завжди зайве.
var aiRoleGroups = []struct {
	Role string
	Keys []string
}{
	{"мінімапа", []string{"xaerominimap", "xaero's minimap", "journeymap", "voxelmap"}},
	{"карта світу", []string{"xaeroworldmap", "xaero's world map", "journeymap"}},
	{"перегляд рецептів", []string{"jei", "just enough items", "rei", "roughly enough items", "emi"}},
	{"сортування інвентарю", []string{"inventoryprofilesnext", "inventory profiles next", "inventorysorter", "invtweaks", "inventory tweaks"}},
	{"оптимізація рендера", []string{"embeddium", "rubidium", "sodium", "magnesium"}},
	{"підказки про блоки", []string{"jade", "waila", "hwyla", "the one probe", "theoneprobe"}},
	{"шейдери", []string{"oculus", "iris"}},
}

var aiMCVerRe = regexp.MustCompile(`1\.(\d{1,2})(?:\.(\d{1,2}))?`)
var aiPreRe = regexp.MustCompile(`(?i)(alpha|early[-_ ]?dev|snapshot|pre[-_ ]?release|-dev\b|\bdev\b|nightly|\brc\d)`)

// aiBaselineMods — фундамент оптимізації за лоадером (достатньо одного з кожної групи).
func aiBaselineMods(loader string) [][]string {
	switch strings.ToLower(loader) {
	case "forge":
		return [][]string{{"embeddium", "rubidium"}, {"ferritecore"}, {"modernfix"}}
	case "neoforge":
		return [][]string{{"embeddium", "sodium"}, {"ferritecore"}, {"modernfix"}}
	case "fabric", "quilt":
		return [][]string{{"sodium"}, {"lithium"}, {"ferritecore"}}
	}
	return nil
}

func aiReviewRAMHint(count int) (min, rec int) {
	switch {
	case count <= 40:
		return 3, 4
	case count <= 90:
		return 4, 6
	case count <= 150:
		return 6, 8
	}
	return 8, 10
}

func init() {
	regTool(aiTool{
		name: "review_build",
		desc: "Оцінка якості збірки (лише читає): кількість і вага модів, схожі за роллю моди, відсутній фундамент оптимізації, alpha/dev-версії та файли для іншої версії гри, рекомендована RAM, шейдери, проблеми лаунчера. " +
			"Викликайте після кожної хвилі модів і перед тим, як назвати збірку готовою; усунення зауважень — на вас.",
		props:    map[string]string{"pack_id": "id збірки"},
		required: []string{"pack_id"},
		run: func(a *App, g argMap) (any, error) {
			id := g.str("pack_id")
			if id == "" {
				return nil, errPackID()
			}
			b, err := a.resolveLaunchBuild(id)
			if err != nil {
				return nil, err
			}
			mods := a.ScanModsFull(id)
			var items []aiReviewItem
			add := func(sev, kind, detail, fix string) { items = append(items, aiReviewItem{sev, kind, detail, fix}) }

			enabled := 0
			var totalBytes int64
			hay := make([]string, 0, len(mods))
			for _, m := range mods {
				if !m.Enabled {
					continue
				}
				enabled++
				totalBytes += m.FileSize
				hay = append(hay, strings.ToLower(m.Name+" "+m.ID+" "+m.FileName))
			}
			has := func(keys ...string) bool {
				for _, h := range hay {
					for _, k := range keys {
						if strings.Contains(h, k) {
							return true
						}
					}
				}
				return false
			}

			// Фундамент оптимізації.
			var missing []string
			for _, grp := range aiBaselineMods(b.Loader) {
				if !has(grp...) {
					missing = append(missing, grp[0])
				}
			}
			if len(missing) > 0 {
				add("warn", "no_perf_baseline", "немає фундаменту оптимізації: "+strings.Join(missing, ", "), "install_content для кожного з них")
			}

			// Моди з однаковою роллю.
			for _, grp := range aiRoleGroups {
				var found []string
				for _, k := range grp.Keys {
					if has(k) {
						found = append(found, k)
					}
				}
				// Oculus/Iris/Embeddium/Rubidium — різні роботи; сповіщаємо, лише коли їх >1 з однієї «родини».
				if len(found) > 1 && grp.Role != "карта світу" {
					add("warn", "overlapping_mods", fmt.Sprintf("кілька модів з роллю «%s»: %s", grp.Role, strings.Join(found, ", ")), "залиште один (toggle_mod)")
				}
			}

			// Версії: alpha/dev і файли для іншої версії гри.
			mcParts := aiMCVerRe.FindStringSubmatch(b.MCVersion)
			var risky, otherVer []string
			for _, m := range mods {
				if !m.Enabled {
					continue
				}
				label := m.FileName
				if aiPreRe.MatchString(m.FileName) || aiPreRe.MatchString(m.Version) {
					risky = append(risky, label)
				}
				if mcParts != nil {
					for _, v := range aiMCVerRe.FindAllStringSubmatch(m.FileName, -1) {
						// порівнюємо лише «1.xx» як мінорну версію гри; 1.20.1 vs 1.20.4 теж підозріло.
						full := v[0]
						if full != b.MCVersion && strings.HasPrefix(full, "1.") && len(v[1]) == 2 && v[1] == mcParts[1] && full != mcParts[0] && v[2] != "" && mcParts[2] != "" && v[2] != mcParts[2] {
							otherVer = append(otherVer, label)
							break
						}
					}
				}
			}
			if len(risky) > 0 {
				add("warn", "prerelease_mods", "alpha/dev/ранні білди: "+strings.Join(aiCap(risky, 6), "; "), "замініть стабільною версією або приберіть (особливо доповнення до інших модів)")
			}
			if len(otherVer) > 0 {
				add("warn", "other_mc_version_files", "в імені файлу інша підверсія гри: "+strings.Join(aiCap(otherVer, 6), "; "), "перевірте get_content_versions: бажано файл саме для "+b.MCVersion)
			}

			// Кількість, вага, RAM.
			mb := totalBytes / (1024 * 1024)
			minGB, recGB := aiReviewRAMHint(enabled)
			if enabled > 150 {
				add("warn", "too_many_mods", fmt.Sprintf("%d увімкнених модів — дуже багато", enabled), "приберіть непотрібне; кожен мод має причину бути")
			} else if enabled > 100 {
				add("info", "many_mods", fmt.Sprintf("%d модів: слабкому ПК буде важко", enabled), "порівняйте з потужністю ПК (get_system_info)")
			}
			ramNote := fmt.Sprintf("модів: %d (%d МБ); орієнтир RAM: мінімум %d ГБ, рекомендовано %d ГБ", enabled, mb, minGB, recGB)
			if b.UseCustomRAM && b.MaxRAMMB > 0 && b.MaxRAMMB < minGB*1024 {
				add("warn", "low_ram", fmt.Sprintf("виділено %d МБ RAM — менше за мінімум %d ГБ", b.MaxRAMMB, minGB), "set_build_ram")
			}
			if b.RecommendedRAMMB > 0 && b.UseCustomRAM && b.MaxRAMMB > 0 && b.MaxRAMMB < b.RecommendedRAMMB {
				add("info", "below_recommended_ram", fmt.Sprintf("RAM %d МБ нижче рекомендованої збіркою (%d МБ)", b.MaxRAMMB, b.RecommendedRAMMB), "set_build_ram")
			}

			// Шейдери увімкнено?
			for _, cfg := range []string{"oculus.properties", "iris.properties"} {
				if data, err := os.ReadFile(filepath.Join(a.gameDir(id), "config", cfg)); err == nil &&
					strings.Contains(strings.ReplaceAll(string(data), " ", ""), "enableShaders=true") {
					add("info", "shaders_on", "шейдери увімкнені — важкі для слабкого ПК; тримайте їх лише якщо користувач просив", "set_shader_pack з порожнім shader")
				}
			}

			// Проблеми лаунчера (залежності, дублікати, несумісність).
			problems := aiBuildProblems(a, id, true)
			for _, p := range problems {
				add(p.Severity, p.Kind, p.Detail, p.Hint)
			}

			errs, warns := 0, 0
			for _, it := range items {
				switch it.Severity {
				case "error":
					errs++
				case "warn":
					warns++
				}
			}
			verdict := "готова до тесту"
			switch {
			case errs > 0:
				verdict = "є помилки — виправте перед запуском"
			case warns > 0:
				verdict = "є зауваження — розгляньте виправлення перед тестом"
			}
			return map[string]any{
				"pack_id": id, "mc_version": b.MCVersion, "loader": b.Loader,
				"mods_enabled": enabled, "mods_size_mb": mb, "ram_hint": ramNote,
				"errors": errs, "warnings": warns, "verdict": verdict, "findings": items,
				"next": "після виправлень: launch_build, читання консолі (див. get_instructions launch_test), game_status (FPS ≥ 40)",
			}, nil
		},
	})
}

func aiCap(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	return append(append([]string{}, s[:n]...), fmt.Sprintf("…і ще %d", len(s)-n))
}
