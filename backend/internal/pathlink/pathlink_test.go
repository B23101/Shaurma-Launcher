package pathlink

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMakeDirLink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "real")
	os.MkdirAll(target, 0755)
	link := filepath.Join(root, "gate")
	if err := MakeDirLink(link, target); err != nil {
		t.Skipf("посилання недоступні в цьому середовищі: %v", err)
	}
	if err := os.WriteFile(filepath.Join(link, "a.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "a.txt")); err != nil {
		t.Fatal("файл мав з'явитись у цільовій теці")
	}
	if !IsLink(link) {
		t.Fatal("IsLink має бути true")
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "a.txt")); err != nil {
		t.Fatal("видалення посилання не має чіпати вміст")
	}
}
