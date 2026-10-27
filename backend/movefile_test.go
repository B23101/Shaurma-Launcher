package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMoveFileCopyFallback — коли os.Rename не вдається (напр. файл
// відкритий процесом на Windows, або різні томи), moveFile має скопіювати
// вміст і видалити джерело — дані не губляться. Симулюємо «rename не
// вийшов» через неможливий призначений шлях (немає батьківської теки) —\n// але копія теж не вийде. Тому симулюємо інакше: перевіряємо, що для
// звичайного файлу moveFile працює (rename), а для теки з вкладеними
// файлами moveTree копіює, коли призначення на іншій структурі.
func TestMoveFileRename(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	if err := os.WriteFile(src, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := moveFile(src, dst); err != nil {
		t.Fatalf("moveFile: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil || string(data) != "hello" {
		t.Fatalf("вміст не перенесений: %q, err=%v", data, err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("джерело не видалене: %v", err)
	}
}

// TestMoveTreeMerges — moveTree переносить вкладену структуру і не
// перезаписує файли, що вже є в призначенні (злиття).
func TestMoveTreeMerges(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	os.MkdirAll(filepath.Join(src, "sub"), 0755)
	os.MkdirAll(dst, 0755)
	os.WriteFile(filepath.Join(src, "a.txt"), []byte("A"), 0644)
	os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("B"), 0644)
	os.WriteFile(filepath.Join(dst, "a.txt"), []byte("EXISTING"), 0644) // конфлікт імені

	if err := moveTree(src, dst); err != nil {
		t.Fatalf("moveTree: %v", err)
	}
	// Конфліктний файл у призначенні НЕ перезаписаний.
	if data, _ := os.ReadFile(filepath.Join(dst, "a.txt")); string(data) != "EXISTING" {
		t.Errorf("a.txt перезаписаний: %q", data)
	}
	// Решта файлів перенесена у вкладену структуру.
	if data, _ := os.ReadFile(filepath.Join(dst, "sub", "b.txt")); string(data) != "B" {
		t.Errorf("b.txt не перенесений: %q", data)
	}
	// Конфліктний файл ЛИШАЄТЬСЯ в джерелі (його не переносили, бо в
	// призначенні вже є такий) — це дизайн: чужі дані не перезаписуємо.
	if data, _ := os.ReadFile(filepath.Join(src, "a.txt")); string(data) != "A" {
		t.Errorf("конфліктний файл пропав з джерела: %q", data)
	}
	// Підтеки без конфліктів переїхали повністю (src/sub зникла).
	if _, err := os.Stat(filepath.Join(src, "sub")); !os.IsNotExist(err) {
		t.Errorf("підтека sub лишилась у джерелі")
	}
}

// TestMoveFileCopyFallbackOpenFile — «заблокований» файл (відкритий на
// читання/запис, як launcher.log): rename на Windows для такого падає,
// тож moveFile має впасти на копію. Тут ми перевіряємо сам фолбек:
// створюємо файл, «ламаємо» rename, підмінюючи неможливим призначенням
// (батьківська тека — це сам файл) — moveFile має повернути помилку, але
// НЕ загубити джерело. Гарантія «не втрачаємо дані» важливіша за успіх.
func TestMoveFileCopyFallbackNoDataLoss(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "data.bin")
	dst := filepath.Join(dir, "data.bin", "impossible") // dst — шлях всередині файлу
	content := []byte("important-data")
	os.WriteFile(src, content, 0644)

	err := moveFile(src, dst)
	if err == nil {
		t.Fatalf("очікували помилку при неможливому призначенні")
	}
	// Джерело має лишитись недоторканим — фолбек-копія не має знищити дані.
	if data, rerr := os.ReadFile(src); rerr != nil || string(data) != string(content) {
		t.Errorf("джерело пошкоджене після невдалого переносу: %v", rerr)
	}
}
