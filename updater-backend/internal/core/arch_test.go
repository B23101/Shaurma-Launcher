package core

import (
	"errors"
	"testing"

	"shrm-updater-backend/internal/manifest"
)

// TestValidateRemoteArchitecture — маніфест старої монолітної архітектури
// (launcher.exe + shrm-updater.exe) має бути відхилений ще до Diff:
// інакше Diff «побачив» би старі файли як змінені і качав би їх у нову
// DLL-установку (реальний інцидент серпня 2026 — застарілий деплой на
// CDN перетворив оновлення на завантаження чужого launcher.exe).
func TestValidateRemoteArchitecture(t *testing.T) {
	var cb Callbacks // порожні колбеки: tr() має повернути український варіант

	oldArch := &manifest.Manifest{Version: "2.0.3", Components: []manifest.Component{
		{Path: "launcher.exe"},
		{Path: "shrm-updater.exe"},
	}}
	if err := cb.validateRemoteArchitecture(oldArch); err == nil {
		t.Fatal("стара архітектура (launcher.exe) має бути відхилена")
	} else if !errors.Is(err, ErrStaleCDNManifest) {
		t.Fatalf("відхилення має обгортати ErrStaleCDNManifest (для коректного повідомлення в Run), отримано: %v", err)
	}

	missing := &manifest.Manifest{Version: "2.1.0", Components: []manifest.Component{
		{Path: "backend.dll"},
		{Path: "updater.dll"},
	}}
	if err := cb.validateRemoteArchitecture(missing); err == nil {
		t.Fatal("неповний маніфест (без frontend.zip) має бути відхилений")
	} else if !errors.Is(err, ErrStaleCDNManifest) {
		t.Fatalf("відхилення має обгортати ErrStaleCDNManifest, отримано: %v", err)
	}

	ok := &manifest.Manifest{Version: "2.1.0", Components: []manifest.Component{
		{Path: "ShaurmLauncher.exe"},
		{Path: "backend.dll"},
		{Path: "updater.dll"},
		{Path: "frontend.zip"},
		{Path: "updater-frontend.zip"},
	}}
	if err := cb.validateRemoteArchitecture(ok); err != nil {
		t.Fatalf("нова архітектура має пройти валідацію: %v", err)
	}
}
