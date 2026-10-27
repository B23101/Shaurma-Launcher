// state.go — стан tsnet-ноди на диску. Винесено окремо, бо і Host, і Guest
// мають одну й ту саму проблему: унікальна тека на КОЖНУ сесію.
//
// ── ЧОМУ НЕ МОЖНА ПЕРЕВИКОРИСТОВУВАТИ ТУ САМУ ТЕКУ ─────────────────────
// tsnet зберігає в Dir свій node key (ідентичність ноди в Headscale). Ноди
// тут ЕФЕМЕРНІ (Ephemeral: true): Headscale прибирає їх із мережі після
// розриву з'єднання / 5 хв бездіяльності. Якщо наступного запуску взяти
// ТУ САМИЙ Dir, клієнт пред'являє вже видалений node key — Headscale
// відповідає «node not found» (у лаунчері це виглядало як «невідомий
// вузол»), нода не піднімається, і ніхто не може приєднатись. Симптом
// найгірший із можливих: лаунчер вважає, що хостинг працює, а гостей
// немає.
//
// Тому: свіжа унікальна тека на кожну сесію (новий node key + одноразовий
// auth key із Worker'а = завжди чиста реєстрація), а старі теки сесій
// прибираємо — щоб не рости на диску.
package p2p

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// nodeStateKeep — скільки останніх тек сесій лишаємо на диску (діагностика:
// якщо нода падає на реєстрації, корисно мати хоч один свіжий слід). Решта
// видаляється при наступному старті.
const nodeStateKeep = 2

// freshNodeStateDir створює НОВУ (унікальну) теку стану tsnet для однієї
// сесії: <base>/<prefix>-<випадковий суфікс>. Одночасно прибирає старі теки
// того самого префікса (лишає nodeStateKeep найновіших).
func freshNodeStateDir(base, prefix string) (string, error) {
	if err := os.MkdirAll(base, 0700); err != nil {
		return "", fmt.Errorf("p2p: не вдалося створити %s: %w", base, err)
	}

	dir := filepath.Join(base, prefix+"-"+randomSuffix())
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("p2p: не вдалося створити %s: %w", dir, err)
	}
	// Прибираємо ПІСЛЯ створення і НІКОЛИ не чіпаємо щойно створену теку
	// (усі теки можуть мати однаковий mtime у межах мілісекунди — без
	// захисту prune міг би видалити саме ту, з якою ми стартуємо).
	pruneNodeStateDirs(base, prefix, nodeStateKeep, filepath.Base(dir))
	return dir, nil
}

// dropNodeStateDir прибирає теку сесії (best-effort). Викликається після
// закриття tsnet-ноди: нода ефемерна, її стан нікому не потрібен.
func dropNodeStateDir(dir string) {
	if dir == "" {
		return
	}
	_ = os.RemoveAll(dir)
}

// pruneNodeStateDirs лишає теку keepName і (keep-1) найновіших інших тек із
// тим самим префіксом — решту видаляє. Помилки ігноруються: прибирання
// сміття не має ламати запуск хостингу.
func pruneNodeStateDirs(base, prefix string, keep int, keepName string) {
	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), prefix+"-") {
			dirs = append(dirs, e.Name())
		}
	}
	if len(dirs) <= keep {
		return
	}
	// Імена містять випадковий суфікс, тож сортуємо за часом зміни — і
	// лишаємо найновіші (окрім keepName, який лишається завжди).
	sort.Slice(dirs, func(i, j int) bool {
		fi, errI := os.Stat(filepath.Join(base, dirs[i]))
		fj, errJ := os.Stat(filepath.Join(base, dirs[j]))
		if errI != nil || errJ != nil {
			return dirs[i] < dirs[j]
		}
		return fi.ModTime().After(fj.ModTime())
	})

	kept := 1 // keepName уже лишається
	for _, name := range dirs {
		if name == keepName {
			continue
		}
		if kept < keep {
			kept++
			continue
		}
		_ = os.RemoveAll(filepath.Join(base, name))
	}
}
