// Package modstore — «хешування» модів старих збірок: мод, який Modrinth
// впізнав за sha1, переноситься у спільне сховище, а у збірці лишається
// жорстке посилання (hardlink) на той самий файл. Вміст файлів НЕ міняється,
// тож моди не ламаються; економія місця виходить, коли той самий jar лежить
// у кількох збірках. Моди, яких Modrinth не знає (або чий хеш не збігся),
// не чіпаються взагалі.
package modstore

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Entry — запис про захешований мод у маркері збірки.
type Entry struct {
	Sha1 string `json:"sha1"`
	Size int64  `json:"size"`
}

// Result — підсумок обробки однієї теки модів.
type Result struct {
	Hashed  int   // нових файлів переведено у сховище
	Already int   // уже були захешовані
	Skipped int   // не знає Modrinth / не вдалось створити посилання
	Saved   int64 // байт, які більше не дублюються (оцінка)
}

// KnownFunc повертає множину sha1 (нижній регістр), які знає Modrinth.
type KnownFunc func(hashes []string) (map[string]bool, error)

const markerName = ".shaurma-hashed-mods.json"

func markerPath(modsDir string) string {
	return filepath.Join(filepath.Dir(modsDir), markerName)
}

// ReadMarker читає маркер збірки (ім'я файлу → запис). Порожня мапа, якщо його нема.
func ReadMarker(modsDir string) map[string]Entry {
	out := map[string]Entry{}
	if b, err := os.ReadFile(markerPath(modsDir)); err == nil {
		_ = json.Unmarshal(b, &out)
	}
	return out
}

func writeMarker(modsDir string, m map[string]Entry) {
	p := markerPath(modsDir)
	if len(m) == 0 {
		_ = os.Remove(p)
		return
	}
	b, _ := json.MarshalIndent(m, "", " ")
	_ = os.WriteFile(p, b, 0644)
}

// CountHashed — скільки модів збірки зараз справді захешовані (файл існує і
// збігається за розміром із маркером).
func CountHashed(modsDir string) int {
	n := 0
	for name, e := range ReadMarker(modsDir) {
		if fi, err := os.Stat(filepath.Join(modsDir, name)); err == nil && fi.Size() == e.Size {
			n++
		}
	}
	return n
}

// HashedSet повертає імена файлів, які вважаються захешованими.
func HashedSet(modsDir string) map[string]bool {
	set := map[string]bool{}
	for name, e := range ReadMarker(modsDir) {
		if fi, err := os.Stat(filepath.Join(modsDir, name)); err == nil && fi.Size() == e.Size {
			set[name] = true
		}
	}
	return set
}

func fileSHA1(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func isModFile(name string) bool {
	l := strings.ToLower(name)
	return strings.HasSuffix(l, ".jar") || strings.HasSuffix(l, ".jar.disabled")
}

// HashDir обробляє одну теку модів. storeDir — спільне сховище (має бути на
// тому ж томі, що й збірка, інакше hardlink неможливий і мод лишається як є).
func HashDir(storeDir, modsDir string, known KnownFunc) (Result, error) {
	var res Result
	entries, err := os.ReadDir(modsDir)
	if err != nil {
		return res, nil // немає теки модів — нічого робити
	}
	marker := ReadMarker(modsDir)

	type cand struct {
		name string
		path string
		size int64
		sha  string
		mod  time.Time
	}
	var cands []cand
	var hashes []string
	for _, e := range entries {
		if e.IsDir() || !isModFile(e.Name()) {
			continue
		}
		p := filepath.Join(modsDir, e.Name())
		fi, err := os.Lstat(p)
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		if m, ok := marker[e.Name()]; ok && m.Size == fi.Size() {
			res.Already++
			continue
		}
		sha, err := fileSHA1(p)
		if err != nil {
			continue
		}
		cands = append(cands, cand{e.Name(), p, fi.Size(), sha, fi.ModTime()})
		hashes = append(hashes, sha)
	}
	if len(cands) == 0 {
		return res, nil
	}
	set, err := known(hashes)
	if err != nil {
		return res, err // мережа/Modrinth недоступні — нічого не чіпаємо
	}

	changed := false
	for _, c := range cands {
		if !set[c.sha] {
			res.Skipped++
			continue
		}
		store := filepath.Join(storeDir, c.sha[:2], c.sha+".jar")
		if fi, err := os.Stat(store); err != nil {
			if err := os.MkdirAll(filepath.Dir(store), 0755); err != nil {
				res.Skipped++
				continue
			}
			if err := os.Link(c.path, store); err != nil { // інший том / ФС без hardlink
				res.Skipped++
				continue
			}
		} else if fi.Size() != c.size {
			res.Skipped++ // сховище пошкоджене — не ризикуємо
			continue
		} else if a, err1 := os.Stat(c.path); err1 == nil && os.SameFile(a, fi) {
			// вже те саме посилання
		} else {
			// Перед заміною: сховище має мати РІВНО той самий вміст (sha1), а сам мод
			// не повинен змінитись після підрахунку хеша (наприклад, гра запущена).
			if got, err := fileSHA1(store); err != nil || got != c.sha {
				res.Skipped++
				continue
			}
			if now, err := os.Stat(c.path); err != nil || now.Size() != c.size || !now.ModTime().Equal(c.mod) {
				res.Skipped++
				continue
			}
			tmp := c.path + ".shaurma-tmp"
			_ = os.Remove(tmp)
			if err := os.Link(store, tmp); err != nil {
				res.Skipped++
				continue
			}
			if err := os.Rename(tmp, c.path); err != nil {
				_ = os.Remove(tmp)
				res.Skipped++
				continue
			}
			res.Saved += c.size
		}
		marker[c.name] = Entry{Sha1: c.sha, Size: c.size}
		res.Hashed++
		changed = true
	}
	if changed {
		writeMarker(modsDir, marker)
	}
	return res, nil
}

// LastActivity — коли збірку востаннє запускали/змінювали: найновіше з
// logs/latest.log та самої теки модів.
func LastActivity(gameDir string) time.Time {
	var t time.Time
	for _, p := range []string{filepath.Join(gameDir, "logs", "latest.log"), filepath.Join(gameDir, "mods")} {
		if fi, err := os.Stat(p); err == nil && fi.ModTime().After(t) {
			t = fi.ModTime()
		}
	}
	return t
}
