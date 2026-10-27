package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMigrateLegacyLocationConfig перевіряє міграцію launcher-location.json
// зі старого шляху %LOCALAPPDATA%\ShaurmLauncher (версія ≤2.0.x) у нову
// теку біля exe (installDir): якщо біля exe файлу немає, а у старому місці
// є — конфіг підхоплюється і переноситься (щоб збережені користувачем
// шляхи baseDir/installations/java не «зникли» після оновлення).
func TestMigrateLegacyLocationConfig(t *testing.T) {
	dir := t.TempDir()
	installDir := filepath.Join(dir, "install")
	legacyDir := filepath.Join(dir, "legacy-config")

	// Симулюємо старий стан: launcher-location.json у %LOCALAPPDATA%\ShaurmLauncher.
	legacyPath := filepath.Join(legacyDir, "ShaurmLauncher", "launcher-location.json")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0755); err != nil {
		t.Fatalf("mkdir legacy: %v", err)
	}
	const legacyJSON = `{"baseDir":"C:\\Games\\Data","installations":"D:\\Mine","java":"E:\\Java"}`
	if err := os.WriteFile(legacyPath, []byte(legacyJSON), 0644); err != nil {
		t.Fatalf("write legacy: %v", err)
	}

	t.Setenv("LOCALAPPDATA", legacyDir)
	t.Setenv("APPDATA", filepath.Join(dir, "_roaming"))

	m := NewManager(installDir)
	// РІШЕННЯ: окремі кастомні папки (installations/java) більше не
	// підтримуються — теки ЗАВЖДИ всередині папки даних (baseDir). Тому
	// навіть якщо в старому конфігу були override — вони ігноруються,
	// шляхи розраховуються від baseDir.
	if got := m.InstancesDir(); got != filepath.Join(`C:\Games\Data`, "installations") {
		t.Errorf("InstancesDir = %q, want C:\\Games\\Data\\installations (завжди всередині baseDir)", got)
	}
	if got := m.JavaDir(); got != filepath.Join(`C:\Games\Data`, "java") {
		t.Errorf("JavaDir = %q, want C:\\Games\\Data\\java", got)
	}

	// Міграція: файл має бути перенесений біля exe.
	newPath := filepath.Join(installDir, "launcher-location.json")
	data, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("launcher-location.json не перенесений у installDir: %v", err)
	}
	if string(data) != legacyJSON {
		t.Errorf("вміст перенесеного файлу відрізняється: %s", string(data))
	}
}

// TestNoMigrateWhenLocalExists — якщо біля exe файл уже є, legacy НЕ
// переноситься і не перезаписує новий (пріоритет у нового місця).
func TestNoMigrateWhenLocalExists(t *testing.T) {
	dir := t.TempDir()
	installDir := filepath.Join(dir, "install")
	legacyDir := filepath.Join(dir, "legacy-config")

	if err := os.MkdirAll(installDir, 0755); err != nil {
		t.Fatalf("mkdir install: %v", err)
	}
	const localJSON = `{"baseDir":"C:\\New\\Place"}`
	if err := os.WriteFile(filepath.Join(installDir, "launcher-location.json"), []byte(localJSON), 0644); err != nil {
		t.Fatalf("write local: %v", err)
	}

	legacyPath := filepath.Join(legacyDir, "ShaurmLauncher", "launcher-location.json")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0755); err != nil {
		t.Fatalf("mkdir legacy: %v", err)
	}
	if err := os.WriteFile(legacyPath, []byte(`{"baseDir":"C:\\Old"}`), 0644); err != nil {
		t.Fatalf("write legacy: %v", err)
	}

	t.Setenv("LOCALAPPDATA", legacyDir)
	t.Setenv("APPDATA", filepath.Join(dir, "_roaming"))

	m := NewManager(installDir)
	if got := m.Dir(); got != `C:\New\Place` {
		t.Errorf("Dir = %q, want C:\\New\\Place (локальний файл має пріоритет)", got)
	}
	// Legacy файл не повинен бути видалений (він не наш — просто читаємо).
	if _, err := os.Stat(legacyPath); err != nil {
		t.Errorf("legacy файл зник: %v", err)
	}
}

// TestSetupCompleteForcesDerivedPaths — майстер завершується, але шляхи
// installations/java ВСЕ ОДНО мають стати деривативами папки даних, а не
// тим, що надіслав фронтенд. Це фікс «java not found at D:\java»: якщо на
// фронтенді GetFolderPaths впав і fallback порахував шлях БЕЗ .shaurm
// (D:\java замість D:\\Games\\.shaurm\\java), бекенд має перезаписати
// неправильне значення на актуальне, а не зберегти битий шлях.
func TestSetupCompleteForcesDerivedPaths(t *testing.T) {
	dir := t.TempDir()

	m := NewManager(dir)
	if err := m.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer func() { _ = m.st.Close() }()

	// Симулюємо майстер: користувач обрав D:\Games як папку даних
	// (бекенд App.SetAppDir додав .shaurm), а фронтенд з якоїсь причини
	// надіслав биті шляхи БЕЗ суфікса.
	gamesDir := filepath.Join(dir, "Games")
	if err := m.SetAppDir(filepath.Join(gamesDir, ".shaurm")); err != nil {
		t.Fatalf("SetAppDir: %v", err)
	}

	bad := m.GetSettings()
	bad.InstanceDir = filepath.Join(gamesDir, "installations") // без .shaurm
	bad.JavaPath = filepath.Join(gamesDir, "java")            // без .shaurm
	if err := m.SetupComplete(bad); err != nil {
		t.Fatalf("SetupComplete: %v", err)
	}

	got := m.GetSettings()
	wantInst := filepath.Join(gamesDir, ".shaurm", "installations")
	if got.InstanceDir != wantInst {
		t.Errorf("InstanceDir = %q, want %q (бекенд має підставити дериватив .shaurm)", got.InstanceDir, wantInst)
	}
	wantJava := filepath.Join(gamesDir, ".shaurm", "java")
	if got.JavaPath != wantJava {
		t.Errorf("JavaPath = %q, want %q (бекенд має підставити дериватив .shaurm)", got.JavaPath, wantJava)
	}
}
