// purpur.go — встановлення Purpur сервера з API purpurmc.org.
package servers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
)

// installPurpur завантажує останній stable build Purpur для вказаної MC-версії.
func installPurpur(t interface{ Logf(string, ...any) }, dir, mcVersion string) error {
	type purgeLogf = func(string, ...any)

	// Purpur API v2 /latest віддає build НА ВЕРХНЬОМУ рівні рядком
	// ({"project":"purpur","version":"1.20.1","build":"2062",...}),
	// без розділу downloads — сам jar качається з .../{build}/download.
	url := fmt.Sprintf("https://api.purpurmc.org/v2/purpur/%s/latest", mcVersion)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("purpur request: %w", err)
	}
	req.Header.Set("User-Agent", "shaurma-launcher/1.0.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("purpur api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("purpur API HTTP %d для %s", resp.StatusCode, mcVersion)
	}

	var meta struct {
		Build string `json:"build"`
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("purpur read body: %w", err)
	}

	if err := json.Unmarshal(body, &meta); err != nil {
		return fmt.Errorf("purpur decode: %w", err)
	}
	if meta.Build == "" || meta.Build == "0" {
		return fmt.Errorf("purpur API не повернув номер build для %s", mcVersion)
	}

	if t != nil {
		t.Logf("Purpur %s: build %s", mcVersion, meta.Build)
	}

	// Завантажуємо як server.jar.
	jarPath := filepath.Join(dir, "server.jar")
	if err := downloadToFile(fmt.Sprintf("https://api.purpurmc.org/v2/purpur/%s/%s/download", mcVersion, meta.Build), jarPath); err != nil {
		return fmt.Errorf("purpur download: %w", err)
	}

	return nil
}

// PurpurAvailableVersions повертає список MC-версій, доступних для Purpur.
func PurpurAvailableVersions() ([]string, error) {
	url := "https://api.purpurmc.org/v2/purpur"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "shaurma-launcher/1.0.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("purpur API HTTP %d", resp.StatusCode)
	}

	var meta struct {
		Versions struct {
			All    []string `json:"all"`
			Stable []string `json:"stable"`
		} `json:"versions"`
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(body, &meta); err != nil {
		return nil, err
	}

	// Пріоритет stable.
	if len(meta.Versions.Stable) > 0 {
		return meta.Versions.Stable, nil
	}
	return meta.Versions.All, nil
}

// DetectCoreForDir — розширений детектор (додає Purpur).
func DetectCoreForDir(dir string) string {
	lowerDir := strings.ToLower(dir)
	_ = lowerDir

	// Purpur: purpur.yml
	if fileExists(filepath.Join(dir, "purpur.yml")) {
		return CorePurpur
	}
	// Velocity: velocity.toml + velocity.jar
	if fileExists(filepath.Join(dir, "velocity.toml")) && fileExists(filepath.Join(dir, "velocity.jar")) {
		return CoreVelocity
	}
	// Paper: paper.yml або paper-server-*.jar
	if fileExists(filepath.Join(dir, "paper.yml")) || fileExists(filepath.Join(dir, "paper-server.jar")) {
		return CorePaper
	}
	return ""
}

func downloadToFile(url, path string) error {
	return downloadFileWithProgress(url, path, nil)
}
