// Package pathlink — посилання на теки (junction на Windows, symlink деінде).
// Дозволяє тримати збірку в будь-якій теці: installations/<id> лишається
// «воротами», і весь код, що ходить у installations/<id>, працює без змін.
package pathlink

import (
	"os"
	"path/filepath"
)

// IsLink — чи path є посиланням (symlink/junction).
func IsLink(path string) bool {
	fi, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0
}

// Resolve повертає реальну теку, на яку вказує посилання (або сам path).
func Resolve(path string) string {
	// EvalSymlinks на Windows не розкриває junction, тож спершу Readlink.
	if t, err := os.Readlink(path); err == nil && t != "" {
		if !filepath.IsAbs(t) {
			t = filepath.Join(filepath.Dir(path), t)
		}
		return filepath.Clean(t)
	}
	if r, err := filepath.EvalSymlinks(path); err == nil {
		return r
	}
	return path
}
