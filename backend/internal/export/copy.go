package export

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ── Копіювання файлів за selection (дублювання збірок) ───────────────────
// CopySelected копіює файли з srcDir у dstDir, поважаючи той самий вибір
// included/excluded, що й експорт (дерево правил exportSelection). На
// відміну від експорту, never-exportable файли (logs/, crash-reports/,
// лаунчерові тимчасові) НЕ відсікаються: дублікат має бути ПОВНОЮ робочою
// копією збірки, а не чистим модпаком для публікації.
//
// Використовується DuplicatePack: користувач обирає в модалці дублювання
// ті самі папки/файли, що й при експорті (дерево з каскадними чекбоксами),
// і копія створюється з того самого вибору.
func CopySelected(srcDir, dstDir string, included, excluded []string, progress ProgressFn) (int, error) {
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return 0, fmt.Errorf("CopySelected: %w", err)
	}
	sel := newExportSelection(included, excluded)
	files, err := collectCopyFiles(srcDir, sel)
	if err != nil {
		return 0, err
	}
	total := len(files)
	copied := 0
	for i, rel := range files {
		src := filepath.Join(srcDir, filepath.FromSlash(rel))
		dst := filepath.Join(dstDir, filepath.FromSlash(rel))
		if err := copyOneFile(src, dst); err != nil {
			return copied, fmt.Errorf("копіювання %s: %w", rel, err)
		}
		copied++
		if progress != nil {
			progress(Progress{Stage: "copy", Current: i + 1, Total: total, FileName: rel})
		}
	}
	return copied, nil
}

// collectCopyFiles обходить srcDir і повертає файли, що входять у selection.
// Пустий included (жодного правила) = копіюємо ВСЕ (на відміну від
// експорту, де порожній вибір означає "нічого") — бо в модалці дублювання
// за замовчуванням вибрано все.
func collectCopyFiles(srcDir string, sel *exportSelection) ([]string, error) {
	var out []string
	err := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if path == srcDir || d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(srcDir, path)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		// Порожній included = все; інакше — за правилами selection.
		if len(sel.include) == 0 || sel.included(rel) {
			out = append(out, rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}

// copyOneFile копіює один файл, створюючи батьківські теки.
func copyOneFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	// Зберігаємо час зміни вихідного файлу — інакше в дублікаті всі файли
	// мали б час КОПІЮВАННЯ, і сортування "нещодавно встановлені" в
	// мод-менеджері показало б їх усі "щойно доданими".
	if st, err := os.Stat(src); err == nil {
		_ = os.Chtimes(dst, st.ModTime(), st.ModTime())
	}
	return nil
}

// PathInCopy — чи відносний шлях підпадає під правила включення/виключення
// (перевикористовується модалкою дублювання для підрахунку).
func PathInCopy(included, excluded []string, rel string) bool {
	if len(included) == 0 && len(excluded) == 0 {
		return true
	}
	sel := newExportSelection(included, excluded)
	rel = strings.ReplaceAll(rel, "\\", "/")
	rel = strings.TrimPrefix(rel, "/")
	return sel.included(rel)
}
