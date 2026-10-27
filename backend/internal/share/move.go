package share

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// MoveTree переносить прийняту теку src у dst. dst не має існувати. Спершу
// rename (миттєво на одному диску), інакше — копіювання з наступним видаленням src.
func MoveTree(src, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("теку призначення вже зайнято: %s", dst)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		return copyFile(p, target)
	})
	if err != nil {
		_ = os.RemoveAll(dst) // не лишаємо напів-копію
		return err
	}
	return os.RemoveAll(src)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
