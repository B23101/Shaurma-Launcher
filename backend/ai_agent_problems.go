package main

// ai_agent_problems.go — інструменти «проблеми збірок» і finish_editing для ШІ-агента.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type aiProblem struct {
	Severity string `json:"severity"` // error | warn | info
	Kind     string `json:"kind"`
	Detail   string `json:"detail"`
	Hint     string `json:"hint,omitempty"` // який інструмент допоможе
}

// aiBuildProblems збирає проблеми однієї збірки. deep=true — важчі перевірки (повний скан модів, health-check).
func aiBuildProblems(a *App, id string, deep bool) []aiProblem {
	var out []aiProblem
	add := func(sev, kind, detail, hint string) { out = append(out, aiProblem{sev, kind, detail, hint}) }

	// Дублікати, заблоковані моди.
	pre := a.CheckPreLaunchModIssues(id)
	for _, d := range pre.Duplicates {
		var files []string
		for _, m := range d.Mods {
			files = append(files, m.FileName)
		}
		add("warn", "duplicate_mod", "той самий мод встановлено двічі: "+strings.Join(files, ", "), "toggle_mod (вимкнути зайву копію)")
	}
	for _, b := range pre.BlockedMods {
		add("warn", "blocked_mod", "автор заборонив автозавантаження, потрібно скачати вручну: "+b, "")
	}

	// Попередження перед запуском (RAM).
	w := a.GetLaunchWarnings(id)
	if w.ShowLowRam {
		add("warn", "low_free_ram", "виділена RAM перевищує вільну пам'ять системи", "set_build_ram")
	}
	if w.ShowRecommended {
		add("info", "below_recommended_ram", "RAM нижча за рекомендовану для збірки", "set_build_ram")
	}

	// Свіжі краші.
	gd := a.gameDir(id)
	cutoff := time.Now().Add(-14 * 24 * time.Hour)
	var newest string
	var newestT time.Time
	n := 0
	if es, err := os.ReadDir(filepath.Join(gd, "crash-reports")); err == nil {
		for _, e := range es {
			if info, err := e.Info(); err == nil && !e.IsDir() && info.ModTime().After(cutoff) {
				n++
				if info.ModTime().After(newestT) {
					newest, newestT = e.Name(), info.ModTime()
				}
			}
		}
	}
	if n > 0 {
		add("error", "recent_crashes", "краш-репортів за 14 днів: "+itoa(n)+", останній "+newest+" ("+newestT.Format("02.01 15:04")+")", "read_log_file → diagnose_pack")
	}
	if es, err := os.ReadDir(gd); err == nil {
		for _, e := range es {
			if strings.HasPrefix(e.Name(), "hs_err") {
				add("error", "jvm_crash", "аварія JVM: "+e.Name(), "read_log_file")
				break
			}
		}
	}

	if deep {
		for _, m := range a.ScanModsFull(id) {
			if !m.Enabled {
				continue
			}
			if m.Incompatible {
				add("error", "incompatible_mod", m.FileName+" не підходить під версію/лоадер збірки", "get_mod_versions → install_mod_version")
			}
			if m.HasMissingDeps {
				add("error", "missing_dependency", m.FileName+" потребує відсутніх залежностей", "get_suggested_mods → install_suggested_mod")
			}
			if m.HasUpdate {
				add("info", "update_available", m.FileName+" має нову версію", "get_mod_versions")
			}
		}
		if rep, err := a.DiagnosePack(id); err == nil {
			for _, is := range rep.Issues {
				var ids []string
				for _, f := range is.Fixes {
					ids = append(ids, f.ID)
				}
				hint := ""
				if len(ids) > 0 {
					hint = "apply_repair fix_ids=" + strings.Join(ids, ",")
				}
				add("error", "diagnosed:"+is.ID, is.Title+": "+is.Detail, hint)
			}
		}
	}
	return out
}

func itoa(n int) string {
	var b [20]byte
	i := len(b)
	if n == 0 {
		return "0"
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func init() {
	regTool(aiTool{
		name:  "list_build_problems",
		desc:  "Проблеми збірок: дублікати модів, заблоковані моди, краші за 14 днів, мало RAM, а з pack_id ще й несумісні моди, відсутні залежності, наявні оновлення та діагностика. Без pack_id — швидкий огляд усіх встановлених збірок.",
		props: map[string]string{"pack_id": "id збірки (порожньо — усі встановлені, без глибокої перевірки)"},
		run: func(a *App, g argMap) (any, error) {
			if id := g.str("pack_id"); id != "" {
				return map[string]any{"pack_id": id, "problems": aiBuildProblems(a, id, true)}, nil
			}
			type row struct {
				ID       string      `json:"id"`
				Name     string      `json:"name"`
				Problems []aiProblem `json:"problems"`
			}
			var rows []row
			errors_, warns := 0, 0
			for _, v := range a.GetBuilds() {
				if !v.Installed {
					continue
				}
				ps := aiBuildProblems(a, v.Build.ID, false)
				if v.Status == "error" || v.Error != "" {
					ps = append(ps, aiProblem{"error", "build_error", v.Error, ""})
				}
				if v.Status == "needs-update" {
					ps = append(ps, aiProblem{"info", "needs_update", "є оновлення збірки", "update_pack_from_source"})
				}
				if len(ps) == 0 {
					continue
				}
				for _, p := range ps {
					switch p.Severity {
					case "error":
						errors_++
					case "warn":
						warns++
					}
				}
				rows = append(rows, row{v.Build.ID, v.Build.Name, ps})
			}
			return map[string]any{"builds_with_problems": rows, "errors": errors_, "warnings": warns,
				"note": "для детальної діагностики однієї збірки викличте list_build_problems з pack_id"}, nil
		},
	})
	regTool(aiTool{
		name: "finish_editing", desc: "Повідомити, що ви закінчили редагувати збірку: знімає блокування для гравця й закриває вікно редагування. Викликайте після завершення робіт (інакше блокування спадає саме через ~90 с бездіяльності).",
		props: map[string]string{"pack_id": "id збірки"}, required: []string{"pack_id"},
		run: func(a *App, g argMap) (any, error) {
			if g.str("pack_id") == "" {
				return nil, errors.New("pack_id обовʼязковий")
			}
			a.aiEditFinish(g.str("pack_id"))
			return "ok", nil
		},
	})
}
