package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// contentTestApp створює App з двома збірками (src-pack/targetPack) і текою
// контенту resourcepacks у джерела. Повертає App і шлях до InstanceDir
// (береться з налаштувань — у тестах він може бути не dir/installations).
func contentTestApp(t *testing.T, dir string) (*App, string, string) {
	t.Helper()
	app, _ := newTestApp(t, dir, "src-pack", "1.20.1")
	// Ефективна тека збірок (та сама, що використовує content_api.go).
	inst := app.cfg.InstancesDir()
	src := filepath.Join(inst, "src-pack", ".minecraft", "resourcepacks")
	target := filepath.Join(inst, "tgt-pack", ".minecraft", "resourcepacks")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatalf("mkdir src: %v", err)
	}
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	return app, inst, target
}

func TestContentScanToggle(t *testing.T) {
	app, inst, _ := contentTestApp(t, t.TempDir())
	rpDir := filepath.Join(inst, "src-pack", ".minecraft", "resourcepacks")

	for _, f := range []string{"better-details.zip", "faithful-32x.zip", "my-pack.zip.disabled"} {
		if err := os.WriteFile(filepath.Join(rpDir, f), []byte("data-"+f), 0644); err != nil {
			t.Fatalf("write %s: %v", f, err)
		}
	}

	// Сканування: 3 файли, один вимкнений, увімкнені зверху.
	list, err := app.ScanContentFiles("src-pack", "resourcepacks")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("очікували 3 записи, отримали %d: %+v", len(list), list)
	}
	if list[0].Enabled != true || list[2].Enabled != false {
		t.Fatalf("порядок Enabled неправильний: %+v", list)
	}
	// Вимкнений у списку: Name без .disabled та розширення, Enabled=false.
	foundDisabled := false
	for _, e := range list {
		if e.Name == "my-pack" && !e.Enabled {
			foundDisabled = true
		}
	}
	if !foundDisabled {
		t.Fatalf("вимкнений файл не знайдено у списку: %+v", list)
	}

	// Вимкнути кращий-details.zip.
	if err := app.ToggleContentFile("src-pack", "resourcepacks", "better-details.zip", false); err != nil {
		t.Fatalf("toggle off: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rpDir, "better-details.zip.disabled")); err != nil {
		t.Fatalf(".disabled-файл не створено: %v", err)
	}

	// Увімкнути назад.
	if err := app.ToggleContentFile("src-pack", "resourcepacks", "better-details.zip.disabled", true); err != nil {
		t.Fatalf("toggle on: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rpDir, "better-details.zip")); err != nil {
		t.Fatalf("файл не повернено: %v", err)
	}
}

// TestContentScanFiltersJunk — у списку встановленого контенту не має бути
// файлів, які Minecraft не вантажить (txt-налаштування/нотатки): лише
// архіви (.zip/.jar) і теки. Регресія на скаргу «у шейдерах показуються
// txt-файли налаштувань».
func TestContentScanFiltersJunk(t *testing.T) {
	app, inst, _ := contentTestApp(t, t.TempDir())
	spDir := filepath.Join(inst, "src-pack", ".minecraft", "shaderpacks")
	if err := os.MkdirAll(filepath.Join(spDir, "BSL"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"complementary.zip", "shader-settings.txt", "note.txt.disabled", "README.md"} {
		if err := os.WriteFile(filepath.Join(spDir, f), []byte("x"), 0644); err != nil {
			t.Fatalf("write %s: %v", f, err)
		}
	}

	list, err := app.ScanContentFiles("src-pack", "shaderpacks")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	// Лишитись мають лише архів complementary.zip і тека BSL — txt/md ховаються.
	if len(list) != 2 {
		t.Fatalf("очікували 2 записи (zip + тека), отримали %d: %+v", len(list), list)
	}
	names := map[string]bool{}
	for _, e := range list {
		names[e.FileName] = true
	}
	if !names["complementary.zip"] || !names["BSL"] {
		t.Fatalf("у списку немає zip/теки: %+v", list)
	}
}

func TestContentToggleIdempotent(t *testing.T) {
	app, inst, _ := contentTestApp(t, t.TempDir())
	rpDir := filepath.Join(inst, "src-pack", ".minecraft", "resourcepacks")
	if err := os.WriteFile(filepath.Join(rpDir, "a.zip"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	// Повторне вимкнення — не помилка, не подвійний суфікс.
	if err := app.ToggleContentFile("src-pack", "resourcepacks", "a.zip", false); err != nil {
		t.Fatal(err)
	}
	if err := app.ToggleContentFile("src-pack", "resourcepacks", "a.zip", false); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(rpDir)
	if len(entries) != 1 || entries[0].Name() != "a.zip.disabled" {
		t.Fatalf("подвійний суфікс або зайві файли: %v", entries)
	}
}

func TestContentDeleteTwin(t *testing.T) {
	app, inst, _ := contentTestApp(t, t.TempDir())
	rpDir := filepath.Join(inst, "src-pack", ".minecraft", "resourcepacks")
	for _, f := range []string{"x.zip", "x.zip.disabled", "y.disabled"} {
		if err := os.WriteFile(filepath.Join(rpDir, f), []byte("d"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := app.DeleteContentFile("src-pack", "resourcepacks", "x.zip"); err != nil {
		t.Fatalf("delete x.zip: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rpDir, "x.zip")); !os.IsNotExist(err) {
		t.Fatalf("x.zip лишився: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rpDir, "x.zip.disabled")); !os.IsNotExist(err) {
		t.Fatalf("x.zip.disabled (двійник) лишився: %v", err)
	}
	if err := app.DeleteContentFile("src-pack", "resourcepacks", "y.disabled"); err != nil {
		t.Fatalf("delete y.disabled: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rpDir, "y")); !os.IsNotExist(err) {
		t.Fatalf("y (двійник) лишився: %v", err)
	}
}

func TestContentTransfer(t *testing.T) {
	app, inst, _ := contentTestApp(t, t.TempDir())
	rpSrc := filepath.Join(inst, "src-pack", ".minecraft", "resourcepacks")
	rpDst := filepath.Join(inst, "tgt-pack", ".minecraft", "resourcepacks")

	if err := os.WriteFile(filepath.Join(rpSrc, "cool-pack.zip"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := app.TransferContentFile("src-pack", "resourcepacks", "cool-pack.zip", "tgt-pack", false); err != nil {
		t.Fatalf("transfer: %v", err)
	}
	// Скопійовано в ціль, джерело лишилось.
	if _, err := os.Stat(filepath.Join(rpDst, "cool-pack.zip")); err != nil {
		t.Fatalf("не скопійовано в ціль: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rpSrc, "cool-pack.zip")); err != nil {
		t.Fatalf("джерело зникло: %v", err)
	}

	// Повторний трансфер без overwrite — помилка конфлікту.
	if err := app.TransferContentFile("src-pack", "resourcepacks", "cool-pack.zip", "tgt-pack", false); err == nil {
		t.Fatal("очікували помилку конфлікту без overwrite")
	}
	// З overwrite — успіх.
	if err := app.TransferContentFile("src-pack", "resourcepacks", "cool-pack.zip", "tgt-pack", true); err != nil {
		t.Fatalf("transfer overwrite: %v", err)
	}
	// У ту саму збірку — помилка.
	if err := app.TransferContentFile("src-pack", "resourcepacks", "cool-pack.zip", "src-pack", true); err == nil {
		t.Fatal("очікували помилку «та сама збірка»")
	}
}

func TestContentTransferDisabledState(t *testing.T) {
	app, inst, _ := contentTestApp(t, t.TempDir())
	rpSrc := filepath.Join(inst, "src-pack", ".minecraft", "resourcepacks")
	rpDst := filepath.Join(inst, "tgt-pack", ".minecraft", "resourcepacks")

	// Вимкнений файл переноситься як вимкнений (.disabled у назві).
	if err := os.WriteFile(filepath.Join(rpSrc, "off.zip.disabled"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := app.TransferContentFile("src-pack", "resourcepacks", "off.zip.disabled", "tgt-pack", false); err != nil {
		t.Fatalf("transfer disabled: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rpDst, "off.zip.disabled")); err != nil {
		t.Fatalf("вимкнений стан не збережено: %v", err)
	}
}

func TestContentDatapackDir(t *testing.T) {
	app, inst, _ := contentTestApp(t, t.TempDir())
	dpDir := filepath.Join(inst, "src-pack", ".minecraft", "datapacks")
	if err := os.MkdirAll(filepath.Join(dpDir, "my-datapack"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dpDir, "my-datapack", "pack.mcmeta"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	list, err := app.ScanContentFiles("src-pack", "datapacks")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(list) != 1 || !list[0].IsDir || list[0].Name != "my-datapack" {
		t.Fatalf("датапак-тека не розпізнана: %+v", list)
	}

	// Вимкнути теку: перейменовується вся тека.
	if err := app.ToggleContentFile("src-pack", "datapacks", "my-datapack", false); err != nil {
		t.Fatalf("toggle dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dpDir, "my-datapack.disabled")); err != nil {
		t.Fatalf("тека не перейменована з .disabled: %v", err)
	}
	// Видалити.
	if err := app.DeleteContentFile("src-pack", "datapacks", "my-datapack.disabled"); err != nil {
		t.Fatalf("delete dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dpDir, "my-datapack.disabled")); !os.IsNotExist(err) {
		t.Fatalf("тека не видалена: %v", err)
	}
}

func TestContentPathTraversal(t *testing.T) {
	app, inst, _ := contentTestApp(t, t.TempDir())
	rpDir := filepath.Join(inst, "src-pack", ".minecraft", "resourcepacks")
	// Вимкнення з ../ не має вийти за межі теки.
	err := app.ToggleContentFile("src-pack", "resourcepacks", "../../evil.zip", false)
	if err == nil {
		t.Fatal("очікували помилку для ../")
	}
	if _, statErr := os.Stat(filepath.Join(rpDir, "..", "evil.zip")); !os.IsNotExist(statErr) {
		t.Fatalf("файл створено поза текою: %v", statErr)
	}

	// Трансфер з ../ — помилка, не копіює.
	err = app.TransferContentFile("src-pack", "resourcepacks", "../../evil.zip", "tgt-pack", false)
	if err == nil {
		t.Fatal("очікували помилку трансферу з ../")
	}
}

func TestContentScanNotInstalledPack(t *testing.T) {
	app, _, _ := contentTestApp(t, t.TempDir())
	// Збірки немає на диску — порожній список, а не помилка.
	list, err := app.ScanContentFiles("ghost-pack", "resourcepacks")
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if list == nil || len(list) != 0 {
		t.Fatalf("очікували порожній список, отримали %+v", list)
	}
	// Невідома тека — помилка.
	_, badErr := app.ScanContentFiles("src-pack", "configs")
	if badErr == nil {
		t.Fatal("очікували помилку невідомої теки")
	}
	if !strings.Contains(badErr.Error(), "configs") {
		t.Fatalf("помилка не називає теку: %v", badErr)
	}
}

func TestContentFileSHA1(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.bin")
	if err := os.WriteFile(p, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	sum, err := contentFileSHA1(p)
	if err != nil {
		t.Fatal(err)
	}
	// SHA1("hello") = aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d
	if sum != "aaf4c61ddcc5e8a2dabede0f3b482cd9aea9434d" {
		t.Fatalf("sha1 неправильний: %s", sum)
	}
}
