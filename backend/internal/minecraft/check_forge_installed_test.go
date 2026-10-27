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
	writeVersionJSON := func(t *testing.T, profileID, inheritsFrom, libsJSON string) {
		t.Helper()
		dir := filepath.Join(baseDir, "versions", profileID)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		jsonStr := `{"id":"` + profileID + `","inheritsFrom":"` + inheritsFrom + `","libraries":` + libsJSON + `}`
		if err := os.WriteFile(filepath.Join(dir, profileID+".json"), []byte(jsonStr), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Реальна структура диска користувача (підтверджена на живій машині):
	//   Forge           1.20.1-forge-47.4.10 + forge/1.20.1-47.4.10/forge-1.20.1-47.4.10-client.jar
	//   NeoForge 1.20.2+ neoforge-21.1.233    + neoforge/21.1.233/neoforge-21.1.233-client.jar
	//   NeoForge 1.20.1  1.20.1-forge-47.1.106 + forge/1.20.1-47.1.106/forge-1.20.1-47.1.106-client.jar
	//                    (артефакт "forge" — той самий Forge-формат!)
	// libraries задаються реалістично: NeoForge-профілі несуть
	// net.neoforged.fancymodloader:* (по них розрізняємо Forge ↔ NeoForge 1.20.1).
	forgeLibs := `[{"name":"net.minecraftforge:fmlloader:1.20.1-47.4.10"}]`
	neoLibs := `[{"name":"net.neoforged.fancymodloader:core:47.2.2"}]`
	neoModernLibs := `[{"name":"net.neoforged.fancymodloader:core:21.1.233"}]`
	writeVersionJSON(t, "1.20.1-forge-47.4.10", "1.20.1", forgeLibs)
	writeVersionJSON(t, "neoforge-21.1.233", "1.21.1", neoModernLibs)
	writeVersionJSON(t, "1.20.1-forge-47.1.106", "1.20.1", neoLibs)
	makeClientJar(t, filepath.Join(baseDir, "libraries", "net", "minecraftforge", "forge", "1.20.1-47.4.10", "forge-1.20.1-47.4.10-client.jar"))
	makeClientJar(t, filepath.Join(baseDir, "libraries", "net", "neoforged", "neoforge", "21.1.233", "neoforge-21.1.233-client.jar"))
	makeClientJar(t, filepath.Join(baseDir, "libraries", "net", "neoforged", "forge", "1.20.1-47.1.106", "forge-1.20.1-47.1.106-client.jar"))

	// Forge: має знайти профіль і повернути його ІНСТАЛЕРНИЙ id.
	if id, ok := inst.checkForgeInstalled("1.20.1", "47.4.10", false); !ok {
		t.Fatalf("forge checkForgeInstalled = false, очікували true")
	} else if id != "1.20.1-forge-47.4.10" {
		t.Fatalf("forge profileID = %q, очікували %q", id, "1.20.1-forge-47.4.10")
	}

	// NeoForge 1.20.2+: той самий випадок (раніше завжди false через
	// mc-префікс у шляху client.jar і неправильний ID профілю).
	if id, ok := inst.checkForgeInstalled("1.21.1", "21.1.233", true); !ok {
		t.Fatalf("neoforge checkForgeInstalled = false, очікували true")
	} else if id != "neoforge-21.1.233" {
		t.Fatalf("neoforge profileID = %q, очікували %q", id, "neoforge-21.1.233")
	}

	// NeoForge 1.20.1 (47.x): профіль і client.jar лежать у Forge-форматі
	// (артефакт "forge") — перевірка має це знайти.
	if id, ok := inst.checkForgeInstalled("1.20.1", "47.1.106", true); !ok {
		t.Fatalf("neoforge 1.20.1 checkForgeInstalled = false, очікували true")
	} else if id != "1.20.1-forge-47.1.106" {
		t.Fatalf("neoforge 1.20.1 profileID = %q, очікували %q", id, "1.20.1-forge-47.1.106")
	}

	// Невідома версія лоадера — install ще не виконувався: має бути false.
	if _, ok := inst.checkForgeInstalled("1.20.1", "99.0.0", false); ok {
		t.Fatalf("checkForgeInstalled для невстановленого лоадера = true, очікували false")
	}
}

// TestCheckForgeInstalledDistinguishesForgeFromNeoForge1201 — Forge і
// NeoForge 1.20.1 зберігають профілі під ОДНАКОВИМ форматом імен
// "<mc>-forge-<version>" (версії 47.x перетинаються — 47.1.106 існує в
// обох лінійках). checkForgeInstalled має розрізняти їх за вмістом
// version.json (net.neoforged.fancymodloader:* бібліотеки = NeoForge), щоб
// не зарахувати Forge-профіль як встановлений NeoForge і навпаки.
func TestCheckForgeInstalledDistinguishesForgeFromNeoForge1201(t *testing.T) {
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
	writeVersionJSON := func(t *testing.T, profileID, inheritsFrom, libsJSON string) {
		t.Helper()
		dir := filepath.Join(baseDir, "versions", profileID)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		jsonStr := `{"id":"` + profileID + `","inheritsFrom":"` + inheritsFrom + `","libraries":` + libsJSON + `}`
		if err := os.WriteFile(filepath.Join(dir, profileID+".json"), []byte(jsonStr), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// На диску лише РЕГУЛЯРНИЙ Forge 47.1.106 (fmlloader, без fancymodloader).
	forgeLibs := `[{"name":"net.minecraftforge:fmlloader:1.20.1-47.1.106"}]`
	writeVersionJSON(t, "1.20.1-forge-47.1.106", "1.20.1", forgeLibs)
	makeClientJar(t, filepath.Join(baseDir, "libraries", "net", "minecraftforge", "forge", "1.20.1-47.1.106", "forge-1.20.1-47.1.106-client.jar"))

	// Запит Forge 47.1.106 → має знайти свій профіль.
	if id, ok := inst.checkForgeInstalled("1.20.1", "47.1.106", false); !ok || id != "1.20.1-forge-47.1.106" {
		t.Fatalf("forge 47.1.106 = %q/%v, очікували знайденим 1.20.1-forge-47.1.106", id, ok)
	}
	// Запит NeoForge 47.1.106 за тих самих файлів на диску → це НЕ NeoForge:
	// не має зарахувати Forge-профіль як встановлений NeoForge.
	if _, ok := inst.checkForgeInstalled("1.20.1", "47.1.106", true); ok {
		t.Fatalf("neoforge 47.1.106 зарахував Forge-профіль як встановлений, очікували false")
	}
}

// TestCheckForgeInstalledOldForge — старі версії Forge (1.8.9/1.7.10)
// зберігають профіль під СТАРИМ форматом ID (версія вклеєна БЕЗ дефіса
// після "forge"), а патчений jar — як universal jar без -client.jar:
//
//	1.7.10 -> versions/1.7.10-Forge10.13.4.1614-1.7.10/  (капітальна "Forge")
//	         libraries/net/minecraftforge/forge/1.7.10-10.13.4.1614-1.7.10/
//	         forge-1.7.10-10.13.4.1614-1.7.10-universal.jar
//
// Раніше checkForgeInstalled сканував лише суфікс "-forge-<ver>", тож
// старий профіль ніколи не знаходився і install перезапускався при
// КОЖНОМУ запуску гри.
func TestCheckForgeInstalledOldForge(t *testing.T) {
	baseDir := t.TempDir()
	inst := NewInstaller(baseDir)

	makeJar := func(t *testing.T, path string) {
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
	writeVersionJSON := func(t *testing.T, profileID, inheritsFrom, libsJSON string) {
		t.Helper()
		dir := filepath.Join(baseDir, "versions", profileID)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		jsonStr := `{"id":"` + profileID + `","inheritsFrom":"` + inheritsFrom + `","libraries":` + libsJSON + `}`
		if err := os.WriteFile(filepath.Join(dir, profileID+".json"), []byte(jsonStr), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// 1.7.10: профіль "1.7.10-Forge10.13.4.1614-1.7.10", forge-бібліотека з
	// :universal classifier (нормалізована normalizeOldForgeLibraries), шлях
	// артефакта записаний у downloads.artifact.path (persisted ensureVersionStart).
	// 1.8.9: профіль "1.8.9-forge1.8.9-11.15.1.2318-1.8.9" (рядковий "forge").
	libs1710 := `[{"name":"net.minecraftforge:forge:1.7.10-10.13.4.1614-1.7.10:universal","downloads":{"artifact":{"path":"net/minecraftforge/forge/1.7.10-10.13.4.1614-1.7.10/forge-1.7.10-10.13.4.1614-1.7.10-universal.jar"}}}]`
	libs189 := `[{"name":"net.minecraftforge:forge:1.8.9-11.15.1.2318-1.8.9:universal","downloads":{"artifact":{"path":"net/minecraftforge/forge/1.8.9-11.15.1.2318-1.8.9/forge-1.8.9-11.15.1.2318-1.8.9-universal.jar"}}}]`
	writeVersionJSON(t, "1.7.10-Forge10.13.4.1614-1.7.10", "1.7.10", libs1710)
	writeVersionJSON(t, "1.8.9-forge1.8.9-11.15.1.2318-1.8.9", "1.8.9", libs189)
	makeJar(t, filepath.Join(baseDir, "libraries", "net", "minecraftforge", "forge", "1.7.10-10.13.4.1614-1.7.10", "forge-1.7.10-10.13.4.1614-1.7.10-universal.jar"))
	makeJar(t, filepath.Join(baseDir, "libraries", "net", "minecraftforge", "forge", "1.8.9-11.15.1.2318-1.8.9", "forge-1.8.9-11.15.1.2318-1.8.9-universal.jar"))

	// 1.7.10 (капітальна "Forge" в ID) має знайтись.
	if id, ok := inst.checkForgeInstalled("1.7.10", "10.13.4.1614", false); !ok {
		t.Fatalf("1.7.10 checkForgeInstalled = false, очікували true")
	} else if id != "1.7.10-Forge10.13.4.1614-1.7.10" {
		t.Fatalf("1.7.10 profileID = %q, очікували %q", id, "1.7.10-Forge10.13.4.1614-1.7.10")
	}
	// 1.8.9 (рядкова "forge") теж.
	if id, ok := inst.checkForgeInstalled("1.8.9", "11.15.1.2318", false); !ok {
		t.Fatalf("1.8.9 checkForgeInstalled = false, очікували true")
	} else if id != "1.8.9-forge1.8.9-11.15.1.2318-1.8.9" {
		t.Fatalf("1.8.9 profileID = %q, очікували %q", id, "1.8.9-forge1.8.9-11.15.1.2318-1.8.9")
	}

	// Інша версія лоадера — install не виконувався: має бути false.
	if _, ok := inst.checkForgeInstalled("1.8.9", "99.99.99", false); ok {
		t.Fatalf("checkForgeInstalled для невстановленого старого Forge = true, очікували false")
	}
}

// TestCheckForgeInstalledForge1122 — Forge 1.12.2 має СУЧАСНИЙ формат ID
// ("1.12.2-forge-14.23.5.2860", дефіс після "forge"), але jar бандлиться
// БЕЗ classifier (forge-1.12.2-14.23.5.2860.jar) і версія.json вказує на
// нього через downloads.artifact.path — -client.jar не генерується. Тож
// перевірка client.jar не спрацьовує, і раніше Forge 1.12.2 перевстановлювався
// при кожному запуску гри. Fallback має знайти патчений jar за шляхом з
// version.json профілю.
func TestCheckForgeInstalledForge1122(t *testing.T) {
	baseDir := t.TempDir()
	inst := NewInstaller(baseDir)

	makeJar := func(t *testing.T, path string) {
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

	profileID := "1.12.2-forge-14.23.5.2860"
	dir := filepath.Join(baseDir, "versions", profileID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	libs := `[{"name":"net.minecraftforge:forge:1.12.2-14.23.5.2860","downloads":{"artifact":{"path":"net/minecraftforge/forge/1.12.2-14.23.5.2860/forge-1.12.2-14.23.5.2860.jar"}}}]`
	jsonStr := `{"id":"` + profileID + `","inheritsFrom":"1.12.2","libraries":` + libs + `}`
	if err := os.WriteFile(filepath.Join(dir, profileID+".json"), []byte(jsonStr), 0o644); err != nil {
		t.Fatal(err)
	}
	// Патчений jar 1.12.2 лежить за шляхом з version.json (БЕЗ -client.jar).
	makeJar(t, filepath.Join(baseDir, "libraries", "net", "minecraftforge", "forge", "1.12.2-14.23.5.2860", "forge-1.12.2-14.23.5.2860.jar"))

	if id, ok := inst.checkForgeInstalled("1.12.2", "14.23.5.2860", false); !ok {
		t.Fatalf("1.12.2 checkForgeInstalled = false, очікували true")
	} else if id != profileID {
		t.Fatalf("1.12.2 profileID = %q, очікували %q", id, profileID)
	}

	// Без патченого jar (лише профіль) — має бути false: jar лоадера не на місці.
	os.Remove(filepath.Join(baseDir, "libraries", "net", "minecraftforge", "forge", "1.12.2-14.23.5.2860", "forge-1.12.2-14.23.5.2860.jar"))
	if _, ok := inst.checkForgeInstalled("1.12.2", "14.23.5.2860", false); ok {
		t.Fatalf("1.12.2 checkForgeInstalled без патченого jar = true, очікували false")
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
