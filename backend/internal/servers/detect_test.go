package servers

import (
	"os"
	"path/filepath"
	"testing"
)

// Створюємо фейкові папки серверів для тестів.

func TestDetectCore_Velocity(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "velocity.toml"), []byte(`bind = "0.0.0.0:25565"`), 0644)
	os.WriteFile(filepath.Join(dir, "velocity.jar"), []byte("fake"), 0644)

	result, err := DetectCore(dir)
	if err != nil {
		t.Fatalf("неочікувана помилка: %v", err)
	}
	if result.Core != CoreVelocity {
		t.Errorf("очікували velocity, отримали %s", result.Core)
	}
	if result.Port != 25565 {
		t.Errorf("очікували порт 25565, отримали %d", result.Port)
	}
	if result.Confidence != "high" {
		t.Errorf("очікували high confidence, отримали %s", result.Confidence)
	}
}

func TestDetectCore_Fabric(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "fabric-server-launch.jar"), []byte("fake"), 0644)

	result, err := DetectCore(dir)
	if err != nil {
		t.Fatalf("неочікувана помилка: %v", err)
	}
	if result.Core != CoreFabric {
		t.Errorf("очікували fabric, отримали %s", result.Core)
	}
	if result.Confidence != "high" {
		t.Errorf("очікували high confidence, отримали %s", result.Confidence)
	}
}

func TestDetectCore_Paper(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "paper.yml"), []byte("some config"), 0644)

	result, err := DetectCore(dir)
	if err != nil {
		t.Fatalf("неочікувана помилка: %v", err)
	}
	if result.Core != CorePaper {
		t.Errorf("очікували paper, отримали %s", result.Core)
	}
}

func TestDetectCore_Purpur(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "purpur.yml"), []byte("some config"), 0644)

	result, err := DetectCore(dir)
	if err != nil {
		t.Fatalf("неочікувана помилка: %v", err)
	}
	if result.Core != CorePurpur {
		t.Errorf("очікували purpur, отримали %s", result.Core)
	}
}

func TestDetectCore_Forge(t *testing.T) {
	dir := t.TempDir()
	forgeDir := filepath.Join(dir, "libraries", "net", "minecraftforge", "forge", "1.20.1-47.4.10")
	os.MkdirAll(forgeDir, 0755)
	os.WriteFile(filepath.Join(forgeDir, "win_args.txt"), []byte("--nogui"), 0644)

	result, err := DetectCore(dir)
	if err != nil {
		t.Fatalf("неочікувана помилка: %v", err)
	}
	if result.Core != CoreForge {
		t.Errorf("очікували forge, отримали %s", result.Core)
	}
	if result.LoaderVer != "1.20.1-47.4.10" {
		t.Errorf("очікували 1.20.1-47.4.10, отримали %s", result.LoaderVer)
	}
}

func TestDetectCore_NeoForge(t *testing.T) {
	dir := t.TempDir()
	neoDir := filepath.Join(dir, "libraries", "net", "neoforged", "neoforge", "21.1.95")
	os.MkdirAll(neoDir, 0755)
	os.WriteFile(filepath.Join(neoDir, "win_args.txt"), []byte("--nogui"), 0644)

	result, err := DetectCore(dir)
	if err != nil {
		t.Fatalf("неочікувана помилка: %v", err)
	}
	if result.Core != CoreNeoForge {
		t.Errorf("очікували neoforge, отримали %s", result.Core)
	}
}

func TestDetectCore_Vanilla(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "server.jar"), []byte("fake jar"), 0644)

	result, err := DetectCore(dir)
	if err != nil {
		t.Fatalf("неочікувана помилка: %v", err)
	}
	if result.Core != CoreVanilla {
		t.Errorf("очікували vanilla, отримали %s", result.Core)
	}
}

func TestDetectCore_Empty(t *testing.T) {
	dir := t.TempDir()
	_, err := DetectCore(dir)
	if err == nil {
		t.Error("очікували помилку для порожньої папки")
	}
}

func TestDetectCore_PortDetection(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "server.jar"), []byte("fake"), 0644)
	os.WriteFile(filepath.Join(dir, "server.properties"), []byte("server-port=25566\ngamemode=creative\n"), 0644)

	result, err := DetectCore(dir)
	if err != nil {
		t.Fatalf("неочікувана помилка: %v", err)
	}
	if result.Port != 25566 {
		t.Errorf("очікували порт 25566, отримали %d", result.Port)
	}
}

func TestDetectCore_VelocityPort(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "velocity.toml"), []byte(`bind = "0.0.0.0:25577"`), 0644)
	os.WriteFile(filepath.Join(dir, "velocity.jar"), []byte("fake"), 0644)

	result, err := DetectCore(dir)
	if err != nil {
		t.Fatalf("неочікувана помилка: %v", err)
	}
	if result.Port != 25577 {
		t.Errorf("очікували порт 25577, отримали %d", result.Port)
	}
}

func TestDetectCore_Quilt(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "quilt-server-launch.jar"), []byte("fake"), 0644)

	result, err := DetectCore(dir)
	if err != nil {
		t.Fatalf("неочікувана помилка: %v", err)
	}
	if result.Core != CoreQuilt {
		t.Errorf("очікували quilt, отримали %s", result.Core)
	}
}

func TestDetectCore_ForgeInstallerOnly(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "forge-1.20.1-47.4.10-installer.jar"), []byte("fake"), 0644)

	result, err := DetectCore(dir)
	if err != nil {
		t.Fatalf("неочікувана помилка: %v", err)
	}
	if result.Core != CoreForge {
		t.Errorf("очікували forge, отримали %s", result.Core)
	}
	if result.Confidence != "low" {
		t.Errorf("очікували low confidence (неінстальований), отримали %s", result.Confidence)
	}
	if result.InstallerJar == "" {
		t.Error("очікували installerJar")
	}
}

func TestDetectCore_PaperPriorityOverPurpur(t *testing.T) {
	// Purpur має вищий пріоритет за Paper (purpur.yml або paper.yml)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "paper.yml"), []byte("paper config"), 0644)
	os.WriteFile(filepath.Join(dir, "purpur.yml"), []byte("purpur config"), 0644)

	result, err := DetectCore(dir)
	if err != nil {
		t.Fatalf("неочікувана помилка: %v", err)
	}
	if result.Core != CorePurpur {
		t.Errorf("очікували purpur (вищий пріоритет), отримали %s", result.Core)
	}
}
