package share

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// LoadStageManifest читає маніфест, збережений у stage при початку прийому
// (щоб завершити встановлення після краху без зв'язку з відправником).
func LoadStageManifest(stage string) (*Manifest, error) {
	raw, err := os.ReadFile(filepath.Join(stage, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}
