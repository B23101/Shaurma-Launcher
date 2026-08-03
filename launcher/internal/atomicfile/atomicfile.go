// Package atomicfile надає атомарний запис JSON на диск (tmp + rename).
// Єдиний загальний хелпер для ВСІХ персистентних файлів лаунчера: якщо
// процес впаде (крах, вимкнення живлення, forced kill від апдейтера) саме
// в момент запису settings.json/accounts.json/builds.json тощо — файл не
// лишиться обрізаним, і наступний старт не «втратить» акаунти (разом з
// refresh-токенами), налаштування чи playtime мовчки.
package atomicfile

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// WriteJSONAtomic пише v у path у JSON-форматі (з відступами) через
// тимчасовий файл і os.Rename — атомарно на тій же файловій системі.
// Директорія створюється за потреби.
func WriteJSONAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
