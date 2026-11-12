package main

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

func TestAssetThumbnailDimensionsAndPassthrough(t *testing.T) {
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 512, 256))); err != nil {
		t.Fatal(err)
	}
	src := "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.Bytes())
	got := assetThumbnail(src, 128)
	raw, err := base64.StdEncoding.DecodeString(strings.SplitN(got, ",", 2)[1])
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 128 || cfg.Height != 64 {
		t.Fatalf("wrong size: %+v", cfg)
	}
	if assetThumbnail(got, 128) != got {
		t.Fatal("small image changed")
	}
	for _, src := range []string{"data:image/gif;base64,AAAA", "data:image/webp;base64,AAAA", "data:image/png;base64,broken"} {
		if assetThumbnail(src, 128) != src {
			t.Fatal("fallback changed")
		}
	}
}

func TestAssetThumbnailKeepsJPEGAndAllowsBannerSize(t *testing.T) {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, 3840, 2160)), nil); err != nil {
		t.Fatal(err)
	}
	src := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(b.Bytes())
	got := assetThumbnail(src, 1920)
	if !strings.HasPrefix(got, "data:image/jpeg;base64,") {
		t.Fatal("JPEG має лишитись JPEG")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.SplitN(got, ",", 2)[1])
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != 1920 || cfg.Height != 1080 {
		t.Fatalf("wrong size: %+v", cfg)
	}
}
