//go:build !shaurma

package main

import (
	"fmt"

	"shaurma-launcher-wails/internal/api"
)

func newPackProvider(a *App) PackProvider {
	return &stubProvider{}
}

// IsShaurmaEdition повертає false у base-збірці: фронтенд приховує
// вкладку збірок Шаурма, бо відповідні бекенд-методи тут не
// скомпільовані взагалі (їх просто немає в цьому бінарнику).
func (a *App) IsShaurmaEdition() bool { return false }

type stubProvider struct{}

func (p *stubProvider) GetPackIndex() ([]api.PackIndexEntry, error) {
	return []api.PackIndexEntry{}, nil
}

func (p *stubProvider) GetPackByID(id string) (*api.PackIndexEntry, error) {
	return nil, fmt.Errorf("pack %s not found: launcher built without Shaurma pack support", id)
}

func (p *stubProvider) DownloadPack(packID string, a *App) {}

// PauseDownload — no-op у base-збірці: завантажень Шаурма тут немає,
// метод доданий лише для реалізації інтерфейсу PackProvider.
func (p *stubProvider) PauseDownload(packID string) {}

func (p *stubProvider) CancelDownload(packID string) {}

// AssetDataURL — у base-збірці Шаурма-асетів (іконки/фони) немає; метод
// доданий лише для реалізації інтерфейсу PackProvider.
func (p *stubProvider) AssetDataURL(assetURL string) (string, error) {
	return "", fmt.Errorf("shaurma assets not available in base build")
}
