package export

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
)

// ── .zip експорт (generic, сумісний з нашим імпортером) ────────────────
// Простий архів ВСІХ вибраних файлів у структурі .minecraft (файли в
// корені архіву, як MultiMC/Prism generic export). Наш імпортер
// розпізнає такий zip як FormatGeneric: весь вміст архіву копіюється в
// .minecraft/ збірки (imports.go: `FormatGeneric → overridesPrefixes
// = [""]`), тож round-trip «експорт → імпорт» працює без втрат.
//
// На відміну від .mrpack тут НЕ розділяємо моди/overrides: це повний
// знімок теки, який можна віддати другу або лишити як бекап. Моди йдуть
// тілом (jar у архіві), а не посиланнями.

// writeZip створює .zip архів з усіма включеними файлами.
func writeZip(outPath, gameDir string, allFiles []string, progress ProgressFn) error {
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	for i, p := range allFiles {
		entry := p // файли в корені архіву у структурі .minecraft
		if err := zipAddFile(zw, entry, filepath.Join(gameDir, filepath.FromSlash(p))); err != nil {
			return fmt.Errorf("zip %s: %w", p, err)
		}
		if progress != nil {
			progress(Progress{Stage: "pack", Current: i + 1, Total: len(allFiles), FileName: p})
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return f.Close()
}
