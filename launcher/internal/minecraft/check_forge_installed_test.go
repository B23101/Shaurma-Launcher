package minecraft

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

// TestCheckForgeInstalledFindsInstallerProfileID — регресійний тест на баг,
// зафіксований користувачем: Forge/NeoForge виконували ВЕСЬ install-шлях
// (розпаковка installer.jar, перевірка всіх бібліотек/асетів і важкі
// processors) при КОЖНОМУ запуску гри, бо checkForgeInstalled шукав профіль
// під ID формату "mc-loaderVersion" (напр. 1.20.1-47.4.10), а на диску
// профілі лежать під ID інсталера:
//
//	Forge    -> versions/1.20.1-forge-47.4.10/  (client.jar у
//	            libraries/net/minecraftforge/forge/1.20.1-47.4.10/)
//	NeoForge -> versions/neoforge-21.1.233/     (client.jar у
//	            libraries/net/neoforged/neoforge/21.1.233/ — БЕЗ mc-префікса)
//
// Тепер checkForgeInstalled шукає реальний профіль серед versions/ і будує
// правильний шлях client.jar для NeoForge.
func TestCheckForgeInstalledFindsInstallerProfileID(t *testing.T) {
	baseDir := t.TempDir()
	inst := NewInstaller(baseDir)

	makeClientJar := func(t *testing.T, path string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		zw := zip.NewWriter(f)
		if _, err := zw.Create("META-INF/MANIFEST.MF"); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	writeVersionJSON := func(t *testing.T, profileID, inheritsFrom string) {
		t.Helper()
		dir := filepath.Join(baseDir, "versions", profileID)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		jsonStr := `{"id":"` + profileID + `","inheritsFrom":"` + inheritsFrom + `"}`
		if err := os.WriteFile(filepath.Join(dir, profileID+".json"), []byte(jsonStr), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Реальна структура диска користувача (підтверджена на живій машині):
	//   Forge    1.20.1-forge-47.4.10 + forge/1.20.1-47.4.10/forge-1.20.1-47.4.10-client.jar
	//   NeoForge neoforge-21.1.233    + neoforge/21.1.233/neoforge-21.1.233-client.jar
	writeVersionJSON(t, "1.20.1-forge-47.4.10", "1.20.1")
	writeVersionJSON(t, "neoforge-21.1.233", "1.21.1")
	makeClientJar(t, filepath.Join(baseDir, "libraries", "net", "minecraftforge", "forge", "1.20.1-47.4.10", "forge-1.20.1-47.4.10-client.jar"))
	makeClientJar(t, filepath.Join(baseDir, "libraries", "net", "neoforged", "neoforge", "21.1.233", "neoforge-21.1.233-client.jar"))

	// Forge: має знайти профіль і повернути його ІНСТАЛЕРНИЙ id.
	if id, ok := inst.checkForgeInstalled("1.20.1", "47.4.10", false); !ok {
		t.Fatalf("forge checkForgeInstalled = false, очікували true")
	} else if id != "1.20.1-forge-47.4.10" {
		t.Fatalf("forge profileID = %q, очікували %q", id, "1.20.1-forge-47.4.10")
	}

	// NeoForge: той самий випадок (раніше завжди false через mc-префікс у
	// шляху client.jar і неправильний ID профілю).
	if id, ok := inst.checkForgeInstalled("1.21.1", "21.1.233", true); !ok {
		t.Fatalf("neoforge checkForgeInstalled = false, очікували true")
	} else if id != "neoforge-21.1.233" {
		t.Fatalf("neoforge profileID = %q, очікували %q", id, "neoforge-21.1.233")
	}

	// Невідома версія лоадера — install ще не виконувався: має бути false.
	if _, ok := inst.checkForgeInstalled("1.20.1", "99.0.0", false); ok {
		t.Fatalf("checkForgeInstalled для невстановленого лоадера = true, очікували false")
	}
}

// TestCheckForgeInstalledRejectsBrokenClientJar — санітарна перевірка вмісту
// client.jar (zip.OpenReader): обірваний/битий jar може мати ненульовий
// розмір, але бути невалідним архівом (напр. процес впав під час запису
// processor'ом). Такий файл НЕ має прийматися як «усе встановлено» — інакше
// запуск гри падав би на завантаженні класів, а install не перезапускався б,
// щоб полагодити output.
func TestCheckForgeInstalledRejectsBrokenClientJar(t *testing.T) {
	baseDir := t.TempDir()
	inst := NewInstaller(baseDir)

	profileID := "1.20.1-forge-47.4.10"
	dir := filepath.Join(baseDir, "versions", profileID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	jsonStr := `{"id":"` + profileID + `","inheritsFrom":"1.20.1"}`
	if err := os.WriteFile(filepath.Join(dir, profileID+".json"), []byte(jsonStr), 0o644); err != nil {
		t.Fatal(err)
	}

	// client.jar існує і має ненульовий розмір, але це не валідний zip
	// (обірваний запис — симулює аварію під час запису processor'ом).
	jarPath := filepath.Join(baseDir, "libraries", "net", "minecraftforge", "forge", "1.20.1-47.4.10", "forge-1.20.1-47.4.10-client.jar")
	if err := os.MkdirAll(filepath.Dir(jarPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jarPath, []byte("PK\x03\x04 broken truncated archive content..."), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, ok := inst.checkForgeInstalled("1.20.1", "47.4.10", false); ok {
		t.Fatalf("checkForgeInstalled з битим client.jar = true, очікували false (має перевстановити)")
	}
}
