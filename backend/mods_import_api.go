package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ── Імпорт модів у теку збірки ──────────────────────────────────────────
// Дві точки входу з UI (кнопка "Додати мод" у тулбарі менеджера модів):
//   - PickModFiles: системний діалог вибору файлів (.jar), повертає шляхи;
//   - ImportModFiles: копіює вже вибрані шляхи (з діалогу АБО drag&drop) у
//     mods/ збірки.
// Розділені навмисно: drag&drop на фронтенді дає шляхи без діалогу, тож
// ImportModFiles має приймати шляхи з будь-якого джерела, а не лише
// власного PickModFiles.

// PickModFiles відкриває системний діалог вибору файлів, дозволяє обрати
// один або кілька .jar (generic bridge-діалог, виконує shell.exe).
// Повертає порожній слайс (не помилку), якщо користувач закрив діалог без
// вибору — це нормальний стан, не збій.
func (a *App) PickModFiles() ([]string, error) {
	files := bridgeDialogOpenFiles("Оберіть jar-файли модів", map[string][]string{
		"Mod files (*.jar)": {"jar"},
	})
	return files, nil
}

// ImportModFiles копіює перелік файлів (шляхи з ОС) у теку mods/ збірки.
// Не-.jar файли пропускаються мовчки (лишається лічильник Skipped), щоб
// один зайвий файл у виборі (напр. картинка, README) не зривав увесь
// імпорт. Ім'я файлу зберігається як є; при колізії з уже наявним файлом
// додається суфікс "-2", "-3"... щоб не затерти існуючий мод ненароком.
func (a *App) ImportModFiles(packID string, paths []string) (ModImportResult, error) {
	// НЕ nil-слайси: Go серіалізує nil у JSON як null, а фронтенд читає
	// res.imported/skipped/failed як масиви (`.length`) — null валив би
	// тостом "Cannot read properties of null (reading 'length')" одразу
	// після "Додано модів: 1". Контракт: це ЗАВЖДИ масиви, хоч і порожні.
	res := ModImportResult{Imported: []string{}, Skipped: []string{}, Failed: []string{}}
	if packID == "" {
		return res, fmt.Errorf("не вказано packID")
	}
	if err := a.errPackRunning(packID); err != nil {
		return res, err
	}
	if err := a.modsLockedErr(packID, ""); err != nil {
		return res, err
	}
	modsDir := a.instanceModsDir(packID)
	if err := os.MkdirAll(modsDir, 0755); err != nil {
		return res, fmt.Errorf("не вдалось створити теку модів: %v", err)
	}
	for _, src := range paths {
		if !strings.EqualFold(filepath.Ext(src), ".jar") {
			res.Skipped = append(res.Skipped, filepath.Base(src))
			continue
		}
		name := uniqueModFileName(modsDir, filepath.Base(src))
		dst := filepath.Join(modsDir, name)
		if err := copyModFile(src, dst); err != nil {
			res.Failed = append(res.Failed, fmt.Sprintf("%s: %v", filepath.Base(src), err))
			continue
		}
		res.Imported = append(res.Imported, name)
	}
	if len(res.Imported) > 0 {
		a.invalidateModUpdateCache(packID)
	}
	return res, nil
}

// ModImportResult — підсумок масового імпорту jar-файлів у теку модів.
type ModImportResult struct {
	Imported []string `json:"imported"`
	Skipped  []string `json:"skipped"`
	Failed   []string `json:"failed"`
}

// uniqueModFileName повертає ім'я файлу, гарантовано вільне в modsDir —
// якщо файл з такою назвою вже є (увімкнений чи вимкнений), додає
// суфікс "-2", "-3"... перед ".jar", щоб імпорт ніколи не перезаписував
// уже встановлений мод мовчки.
func uniqueModFileName(modsDir, name string) string {
	base := strings.TrimSuffix(name, ".jar")
	candidate := name
	for i := 2; ; i++ {
		_, errPlain := os.Stat(filepath.Join(modsDir, candidate))
		_, errDisabled := os.Stat(filepath.Join(modsDir, candidate+".disabled"))
		if os.IsNotExist(errPlain) && os.IsNotExist(errDisabled) {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d.jar", base, i)
	}
}

// copyModFile копіює файл побайтово (не rename — джерело може бути на
// іншому диску/розділі, ніж тека модів).
func copyModFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}
