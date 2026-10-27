package autorepair

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Стан авто-починки живе у службовій теці лаунчера (stateDir), а НЕ в теці
// гри: експорт/копіювання збірки не повинні підхоплювати службові файли.

var safeName = regexp.MustCompile(`^[\w.\-]{1,120}$`)

type packState struct {
	LastSuccess int64 `json:"lastSuccess"`
}

func statePath(stateDir, packID string) string {
	if !safeName.MatchString(packID) || packID == "." || packID == ".." {
		return ""
	}
	return filepath.Join(stateDir, "packs", packID+".json")
}

// LastSuccess повертає час останнього успішного запуску збірки (zero — невідомо).
func LastSuccess(stateDir, packID string) time.Time {
	p := statePath(stateDir, packID)
	if p == "" {
		return time.Time{}
	}
	data, err := os.ReadFile(p)
	if err != nil || len(data) > 64*1024 {
		return time.Time{}
	}
	var st packState
	if json.Unmarshal(data, &st) != nil || st.LastSuccess <= 0 {
		return time.Time{}
	}
	return time.Unix(st.LastSuccess, 0)
}

// RecordLaunchResult запам'ятовує момент успішного запуску: за ним шукаються
// «нові» моди. Ненульовий код виходу нічого не пише.
func RecordLaunchResult(stateDir, packID string, exitCode int) {
	if exitCode != 0 {
		return
	}
	p := statePath(stateDir, packID)
	if p == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	data, _ := json.Marshal(packState{LastSuccess: time.Now().Unix()})
	_ = os.WriteFile(p, data, 0o644)
}

// ── приховані види проблем ─────────────────────────────────────────────────

func mutedPath(stateDir string) string { return filepath.Join(stateDir, "muted.json") }

// NormalizeMuted лишає тільки відомі ID видів проблем (у порядку показу).
func NormalizeMuted(values []string) []string {
	want := map[string]bool{}
	for _, v := range values {
		want[v] = true
	}
	out := []string{}
	for _, k := range issueKinds {
		if want[k.ID] {
			out = append(out, k.ID)
		}
	}
	return out
}

// LoadMuted читає список прихованих видів проблем.
func LoadMuted(stateDir string) []string {
	data, err := os.ReadFile(mutedPath(stateDir))
	if err != nil || len(data) > 64*1024 {
		return []string{}
	}
	var list []string
	if json.Unmarshal(data, &list) != nil {
		return []string{}
	}
	return NormalizeMuted(list)
}

// SetMuted приховує (muted=true) або повертає види проблем і повертає новий список.
func SetMuted(stateDir string, kinds []string, muted bool) ([]string, error) {
	current := LoadMuted(stateDir)
	set := map[string]bool{}
	for _, c := range current {
		set[c] = true
	}
	for _, k := range NormalizeMuted(kinds) {
		if muted {
			set[k] = true
		} else {
			delete(set, k)
		}
	}
	list := make([]string, 0, len(set))
	for k := range set {
		list = append(list, k)
	}
	sort.Strings(list)
	list = NormalizeMuted(list)
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return current, err
	}
	data, _ := json.Marshal(list)
	if err := os.WriteFile(mutedPath(stateDir), data, 0o644); err != nil {
		return current, err
	}
	return list, nil
}

// ── резервні копії конфігів ─────────────────────────────────────────────────

// BackupConfigs переносить конфіги (відносні шляхи "config/…" від gameDir) у
// backupRoot/<мітка часу>/config/…; повертає перенесені. Шляхи за межами
// config/ та симлінки ігноруються.
func BackupConfigs(gameDir, backupRoot string, files []string) ([]string, error) {
	root := filepath.Join(gameDir, "config")
	stamp := filepath.Join(backupRoot, time.Now().Format("20060102-150405"))
	var moved []string
	for _, name := range files {
		rel := strings.TrimPrefix(strings.ReplaceAll(name, "\\", "/"), "config/")
		src := SafeChild(root, rel)
		if src == "" || !isFile(src) {
			continue
		}
		dst := filepath.Join(stamp, "config", filepath.FromSlash(rel))
		if _, err := os.Lstat(dst); err == nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return moved, err
		}
		if err := os.Rename(src, dst); err != nil {
			// інший том — копіюємо й видаляємо
			data, rerr := os.ReadFile(src)
			if rerr != nil {
				return moved, rerr
			}
			if werr := os.WriteFile(dst, data, 0o644); werr != nil {
				return moved, werr
			}
			if derr := os.Remove(src); derr != nil {
				return moved, derr
			}
		}
		moved = append(moved, "config/"+rel)
	}
	return moved, nil
}
