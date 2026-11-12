package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/jpeg"
	"image/png"
	"strings"
)

// GetPackAssetThumbnail reduces decoded image memory before crossing the bridge.
// Animated formats retain their existing path. Side is capped at 1920 so pack
// banners can be reduced to the size they are drawn at instead of the original.
func (a *App) GetPackAssetThumbnail(url string, side int) (string, error) {
	original, err := a.GetPackAsset(url)
	if err != nil {
		return original, err
	}
	return assetThumbnail(original, side), nil
}

func assetThumbnail(original string, side int) string {
	if side < 1 {
		return original
	}
	if side > 1920 {
		side = 1920
	}
	if !strings.HasPrefix(original, "data:image/png;base64,") && !strings.HasPrefix(original, "data:image/jpeg;base64,") {
		return original
	}
	parts := strings.SplitN(original, ",", 2)
	data, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return original
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (cfg.Width <= side && cfg.Height <= side) || int64(cfg.Width)*int64(cfg.Height) > 32*1024*1024 {
		return original
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return original
	}
	var out bytes.Buffer
	if strings.HasPrefix(original, "data:image/jpeg;base64,") {
		// JPEG-оригінал (фото/фон) лишаємо JPEG: PNG-копія фото була б у рази більшою.
		if err := jpeg.Encode(&out, resizeToFit(img, side), &jpeg.Options{Quality: 88}); err != nil {
			return original
		}
		return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(out.Bytes())
	}
	if err := png.Encode(&out, resizeToFit(img, side)); err != nil {
		return original
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(out.Bytes())
}
