package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var skinFileNameRE = regexp.MustCompile(`[^\p{L}\p{N}_\- ]+`)

// ExportWardrobeSkinPNG зберігає PNG скіна (data URL, як його показує гардероб) у файл, який
// обере користувач. Приймаємо лише справжній PNG скіна 64×64 або 64×32 — довільні байти з
// фронтенду на диск не пишемо. Повертає шлях або "" (скасовано).
func (a *App) ExportWardrobeSkinPNG(dataURL, name string) (string, error) {
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(dataURL, prefix) || len(dataURL) > 200<<10 {
		return "", errors.New("це не PNG скіна")
	}
	raw, err := base64.StdEncoding.DecodeString(dataURL[len(prefix):])
	if err != nil {
		return "", errors.New("це не PNG скіна")
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil || cfg.Width != 64 || (cfg.Height != 64 && cfg.Height != 32) {
		return "", errors.New("скін має бути PNG 64×64 або 64×32")
	}
	base := strings.TrimSpace(skinFileNameRE.ReplaceAllString(name, ""))
	if base == "" {
		base = "skin"
	}
	if len([]rune(base)) > 48 {
		base = string([]rune(base)[:48])
	}
	path := bridgeDialogSaveFile("Зберегти скін", base+".png", map[string][]string{"PNG (*.png)": {"png"}})
	if path == "" {
		return "", nil
	}
	if !strings.EqualFold(filepath.Ext(path), ".png") {
		path += ".png"
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return "", err
	}
	return path, nil
}
