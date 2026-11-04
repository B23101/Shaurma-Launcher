// Package safedel — єдине місце, через яке код лаунчера видаляє теки.
//
// Правило: корінь даних (.shaurm), контейнери збірок/серверів (instances,
// installations, servers, minecraft, ...), теку резервних копій, домашню
// теку та корінь диска видаляти ЗАБОРОНЕНО — ні повністю, ні через батьківську
// теку. Видалити можна лише конкретну підпапку всередині (збірку, сервер,
// версію, кеш). Спроба видалити захищений шлях повертає ErrProtected,
// нічого не чіпаючи.
package safedel

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var ErrProtected = errors.New("safedel: шлях захищено від видалення (дані користувача)")

var (
	mu       sync.RWMutex
	resolver func() []string
)

// SetResolver задає функцію, що повертає актуальні захищені корені.
func SetResolver(f func() []string) {
	mu.Lock()
	resolver = f
	mu.Unlock()
}

func norm(p string) string {
	a, err := filepath.Abs(p)
	if err != nil {
		a = p
	}
	a = filepath.Clean(a)
	if r, err := filepath.EvalSymlinks(a); err == nil {
		a = r
	}
	return strings.ToLower(a)
}

// IsProtected: true, якщо target збігається з захищеним коренем або є його
// предком (видалення предка знищило б корінь).
func IsProtected(target string) bool {
	if strings.TrimSpace(target) == "" {
		return true
	}
	t := norm(target)
	if filepath.Dir(t) == t { // корінь диска
		return true
	}
	if home, err := os.UserHomeDir(); err == nil && norm(home) == t {
		return true
	}
	mu.RLock()
	f := resolver
	mu.RUnlock()
	if f == nil {
		return false
	}
	for _, r := range f() {
		if strings.TrimSpace(r) == "" {
			continue
		}
		n := norm(r)
		if t == n || strings.HasPrefix(n, t+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// RemoveAll — безпечна заміна os.RemoveAll.
func RemoveAll(target string) error {
	if IsProtected(target) {
		return fmt.Errorf("%w: %s", ErrProtected, target)
	}
	return os.RemoveAll(target)
}

// RemoveIfEmpty видаляє теку лише якщо вона порожня (після повної міграції).
func RemoveIfEmpty(dir string) error {
	ents, err := os.ReadDir(dir)
	if err != nil || len(ents) > 0 {
		return err
	}
	return os.Remove(dir)
}
