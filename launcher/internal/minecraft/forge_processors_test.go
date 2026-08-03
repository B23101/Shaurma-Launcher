package minecraft

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

// TestResolveProcValueDataFileNotSharedAcrossVersions -- регресійний тест
// на баг, зафіксований користувачем: встановлення Forge/NeoForge для ОДНІЄЇ
// версії (напр. 1.21.1-neoforge), а потім для ІНШОЇ (напр. forge 1.16.5)
// підряд на тій самій машині незмінно падало з "Patch expected checksum X
// but was Y" на РІВНО ОДНОМУ й тому ж класі (RenderTarget) для обох версій
// -- підозріло однаковий симптом для двох геть різних Minecraft-версій.
//
// Причина: resolveProcValue кешував файли install_profile.json (напр.
// data/client.lzma -- сам набір binpatch-ів) за їхнім ВІДНОСНИМ шляхом
// усередині installer.jar у СПІЛЬНІЙ теці %TEMP%/shaurma-forge/. Цей
// відносний шлях (data/client.lzma) ЗАВЖДИ однаковий у будь-якого
// Forge/NeoForge installer.jar незалежно від версії -- це конвенція
// формату. Тому другий запуск install (інша версія) бачив os.Stat("файл
// уже є") і брав ЧУЖИЙ, застарілий client.lzma від попередньої версії, а
// не свій власний -- getResolveProcValue мав ключувати кеш per-версія.
func TestResolveProcValueDataFileNotSharedAcrossVersions(t *testing.T) {
	baseDir := t.TempDir()
	inst := NewInstaller(baseDir)

	// Прибираємо загальний temp перед тестом, щоб не зачепити сліди
	// попередніх прогонів тестів/реального лаунчера на цій машині.
	os.RemoveAll(filepath.Join(os.TempDir(), "shaurma-forge"))
	defer os.RemoveAll(filepath.Join(os.TempDir(), "shaurma-forge"))

	makeFakeInstaller := func(t *testing.T, path, dataContent string) *zip.ReadCloser {
		t.Helper()
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		zw := zip.NewWriter(f)
		w, err := zw.Create("data/client.lzma")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(dataContent)); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		f.Close()

		zr, err := zip.OpenReader(path)
		if err != nil {
			t.Fatal(err)
		}
		return zr
	}

	// "Installer A" -- симулює Forge 1.16.5: свій унікальний вміст
	// data/client.lzma, свій MCP MINECRAFT_VERSION.
	zrA := makeFakeInstaller(t, filepath.Join(baseDir, "installer-a.jar"), "PATCHES-FOR-1.16.5")
	defer zrA.Close()
	varsA := map[string]string{"MINECRAFT_VERSION": "1.16.5-20210115.111550"}
	gotA := inst.resolveProcValue("data/client.lzma", varsA, zrA)
	contentA, err := os.ReadFile(gotA)
	if err != nil {
		t.Fatalf("не вдалося прочитати кешований файл A: %v", err)
	}
	if string(contentA) != "PATCHES-FOR-1.16.5" {
		t.Fatalf("вміст кешованого файлу A = %q, want PATCHES-FOR-1.16.5", contentA)
	}

	// "Installer B" -- симулює NeoForge 1.21.1: ІНШИЙ вміст
	// data/client.lzma (справжні різні binpatch-и), ІНША MCP-версія.
	zrB := makeFakeInstaller(t, filepath.Join(baseDir, "installer-b.jar"), "PATCHES-FOR-1.21.1")
	defer zrB.Close()
	varsB := map[string]string{"MINECRAFT_VERSION": "1.21.1-20240808.144430"}
	gotB := inst.resolveProcValue("data/client.lzma", varsB, zrB)
	contentB, err := os.ReadFile(gotB)
	if err != nil {
		t.Fatalf("не вдалося прочитати кешований файл B: %v", err)
	}

	// РЕГРЕСІЯ: якщо кеш спільний за голим відносним шляхом, gotB буде
	// тим самим файлом, що й gotA, і міститиме "PATCHES-FOR-1.16.5" --
	// тобто patch-дані попередньої версії видаються за дані поточної.
	if string(contentB) != "PATCHES-FOR-1.21.1" {
		t.Fatalf("вміст кешованого файлу B = %q, want PATCHES-FOR-1.21.1 (отримано дані ІНШОЇ версії -- кеш не ізольований per-версія)", contentB)
	}
	if gotA == gotB {
		t.Fatalf("resolveProcValue повернув ОДНАКОВИЙ шлях для двох різних версій: %q -- кеш не ізольований per-версія", gotA)
	}
}

// TestForgeProcessorVarsUseCorrectVersions -- регресійний тест на плутанину
// vanilla vs MCP версії у runForgeProcessors: MINECRAFT_JAR має вказувати
// на справжній vanilla client jar (mcVersion), а MINECRAFT_VERSION -- на
// MCP-рядок (profile.Minecraft), навіть коли вони різні. Раніше обидва
// токени помилково рахувались від mcVersion, тому шляхи, які processors
// будують із {MINECRAFT_VERSION} (напр. вихідні MC_SLIM/MC_SRG/MC_EXTRA
// jar-и в libraries/net/minecraft/client/<MCP-версія>/...), вказували не
// туди, де реально мала бути ця версія -- і client-*-srg.jar/-extra.jar/
// forge-*-client.jar так ніколи й не з'являлись на очікуваному шляху.
func TestForgeProcessorVarsUseCorrectVersions(t *testing.T) {
	baseDir := t.TempDir()
	mcVersion := "1.20.1"
	mcpVersion := "1.20.1-20230612.114412"

	// Готуємо vanilla client jar заздалегідь під ГОЛОЮ версією -- саме
	// сюди має вказати MINECRAFT_JAR.
	vanillaJar := filepath.Join(baseDir, "versions", mcVersion, mcVersion+".jar")
	if err := os.MkdirAll(filepath.Dir(vanillaJar), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vanillaJar, []byte("fake vanilla jar"), 0644); err != nil {
		t.Fatal(err)
	}
	// Мінімальний валідний ванілла version.json, щоб EnsureVersion (якщо
	// раптом викликаний) не бив у мережу.
	writeJSON(t, filepath.Join(baseDir, "versions", mcVersion, mcVersion+".json"), VersionJSON{
		ID:        mcVersion,
		MainClass: "net.minecraft.client.main.Main",
	})

	inst := NewInstaller(baseDir)

	profile := &installProfile{
		Minecraft: mcpVersion,
		Data: map[string]forgeDatum{
			// Типовий формат data-запису: [maven:coords] -> шлях у
			// libraries/, з підстановкою {MINECRAFT_VERSION} усередині
			// координат -- саме так офіційний Forge installer задає
			// вихідний шлях MC_SLIM/MC_SRG для конкретної MCP-версії.
			"MC_SLIM": {Client: "[net.minecraft:client:{MINECRAFT_VERSION}:slim]"},
		},
	}

	// Викликаємо тільки підготовчу частину (vars) -- відтворюємо перші
	// кроки runForgeProcessors без запуску processors.Java (яких тут
	// немає), щоб перевірити саме резолв змінних.
	mcJar := filepath.Join(inst.baseDir, "versions", mcVersion, mcVersion+".jar")
	vars := map[string]string{
		"SIDE":              "client",
		"ROOT":              inst.baseDir,
		"LIBRARY_DIR":       filepath.Join(inst.baseDir, "libraries"),
		"MINECRAFT_VERSION": mcpVersion,
		"MINECRAFT_JAR":     mcJar,
	}
	for key, datum := range profile.Data {
		if datum.Client == "" {
			continue
		}
		vars[key] = inst.resolveProcValue(datum.Client, vars, nil)
	}

	if vars["MINECRAFT_JAR"] != vanillaJar {
		t.Errorf("MINECRAFT_JAR = %q, want %q (справжній vanilla jar)", vars["MINECRAFT_JAR"], vanillaJar)
	}
	if vars["MINECRAFT_VERSION"] != mcpVersion {
		t.Errorf("MINECRAFT_VERSION = %q, want %q (MCP-версія)", vars["MINECRAFT_VERSION"], mcpVersion)
	}
	wantSlim := filepath.Join(inst.baseDir, "libraries", "net", "minecraft", "client", mcpVersion, "client-"+mcpVersion+"-slim.jar")
	if vars["MC_SLIM"] != wantSlim {
		t.Errorf("MC_SLIM = %q, want %q (шлях під MCP-версією, не голою mcVersion)", vars["MC_SLIM"], wantSlim)
	}
}

// TestProcessorOutputsValidDetectsMissingFile -- processorOutputsValid має
// повертати false і шлях відсутнього файлу, якщо output не існує
// (регресія: раніше цю перевірку робили тільки ДО запуску processor'а,
// що дозволяло тихо "успішному" запуску без реального результату
// проскочити без помилки).
func TestProcessorOutputsValidDetectsMissingFile(t *testing.T) {
	baseDir := t.TempDir()
	inst := NewInstaller(baseDir)
	vars := map[string]string{"ROOT": baseDir}

	missing := filepath.Join(baseDir, "does-not-exist.jar")
	outputs := map[string]string{missing: ""}

	ok, gotMissing := inst.processorOutputsValid(outputs, vars)
	if ok {
		t.Fatal("processorOutputsValid = true для відсутнього файлу")
	}
	if gotMissing != missing {
		t.Errorf("шлях відсутнього output = %q, want %q", gotMissing, missing)
	}

	// Тепер створюємо файл — має стати валідним.
	if err := os.WriteFile(missing, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	ok, _ = inst.processorOutputsValid(outputs, vars)
	if !ok {
		t.Error("processorOutputsValid = false для існуючого непорожнього файлу")
	}
}

// TestProcessorOutputsValidEmptyOutputs -- processor БЕЗ оголошених outputs
// (напр. перші кроки installertools: EXTRACT_FILES/BUNDLER_EXTRACT/
// MCP_DATA/DOWNLOAD_MOJMAPS/MERGE_MAPPING у Forge 1.20.x install_profile.json)
// не має вважатись "невдалим" після успішного запуску. Раніше
// processorOutputsValid повертав (false, "") для порожньої мапи outputs --
// і весь install падав із хибною помилкою "processor ... відпрацював, але
// очікуваний файл не з'явився:" (порожній шлях), блокуючи встановлення
// Forge/NeoForge. Тепер порожні outputs = "перевіряти нічого" = валідно.
func TestProcessorOutputsValidEmptyOutputs(t *testing.T) {
	inst := NewInstaller(t.TempDir())
	ok, missing := inst.processorOutputsValid(map[string]string{}, map[string]string{"ROOT": t.TempDir()})
	if !ok {
		t.Fatalf("processorOutputsValid({}, ...) мав бути true, отримали false; missing=%q", missing)
	}
}

// TestReplaceTokensPreservesWindowsBackslashes -- replaceTokens має зберігати
// звичайні бекслеші (Windows-шляхи в outputs/args), ескейпити лише спецсимволи
// токенів. Раніше будь-який '\\' споживався як escape -- і літеральний
// Windows-шлях у outputs (напр. "C:\\Users\\...\\client.jar") втрачав усі
// бекслеші, через що перевірка outputs ніколи не знаходила файл.
func TestReplaceTokensPreservesWindowsBackslashes(t *testing.T) {
	vars := map[string]string{"ROOT": "C:\\games"}
	got := replaceTokens(vars, "{ROOT}\\minecraft\\client.jar")
	want := "C:\\games\\minecraft\\client.jar"
	if got != want {
		t.Errorf("replaceTokens() = %q, want %q", got, want)
	}
}

// TestRunForgeProcessorsFailsOnMissingProcessorJar -- якщо processor jar,
// заявлений у install_profile.json, фізично відсутній у libraries/,
// install має ПАДАТИ з чіткою помилкою, а не мовчки пропускати крок і
// продовжувати (це і призводило до запуску гри без потрібних srg/extra/
// client jar-ів).
func TestRunForgeProcessorsFailsOnMissingProcessorJar(t *testing.T) {
	baseDir := t.TempDir()
	mcVersion := "1.20.1"
	vanillaJar := filepath.Join(baseDir, "versions", mcVersion, mcVersion+".jar")
	os.MkdirAll(filepath.Dir(vanillaJar), 0755)
	os.WriteFile(vanillaJar, []byte("x"), 0644)
	writeJSON(t, filepath.Join(baseDir, "versions", mcVersion, mcVersion+".json"), VersionJSON{ID: mcVersion})

	inst := NewInstaller(baseDir)
	profile := &installProfile{
		Minecraft: mcVersion + "-20230612.114412",
		Processors: []forgeProcessor{
			{Jar: "net.minecraftforge:installertools:1.3.0:fatjar", Outputs: map[string]string{}},
		},
	}

	err := inst.runForgeProcessors(nil, filepath.Join(baseDir, "installer.jar"), profile, nil, "1.20.1-forge-47.4.10", mcVersion)
	if err == nil {
		t.Fatal("runForgeProcessors мав повернути помилку для відсутнього processor jar, а не пройти мовчки")
	}
}