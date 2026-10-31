package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const maxPageDesign = 4 << 20

// GetPackPageDesign resolves metadata from the authorized catalog, never from
// a caller-supplied URL. Content-addressed cache entries survive restarts.
func (a *App) GetPackPageDesign(packID string) (string, error) {
	if a.packs == nil {
		return "", nil
	}
	p, err := a.packs.GetPackByID(packID)
	if err != nil {
		return "", err
	}
	if p == nil || p.PageDesignPath == "" {
		return "", nil
	}
	hash := strings.ToLower(p.PageDesignSHA256)
	sum, err := hex.DecodeString(hash)
	if err != nil || len(sum) != sha256.Size {
		return "", fmt.Errorf("invalid page design SHA-256")
	}
	dir := filepath.Join(a.cfg.CacheDir(), "pack-page-designs")
	target := filepath.Join(dir, hash+".json")
	type cachedDesign struct {
		UpdatedAt string `json:"updatedAt"`
		Archive   string `json:"archive"`
	}
	if data, e := os.ReadFile(target); e == nil {
		var cached cachedDesign
		if json.Unmarshal(data, &cached) == nil {
			raw, e := base64.StdEncoding.DecodeString(cached.Archive)
			if e == nil {
				if html, e := unpackPageDesign(raw, hash); e == nil {
					return html, nil
				}
			}
		}
	}
	assetURL, err := url.Parse(p.PageDesignPath)
	if err != nil {
		return "", err
	}
	query := assetURL.Query()
	query.Set("design", hash)
	assetURL.RawQuery = query.Encode()
	encoded, err := a.packs.AssetDataURL(assetURL.String())
	if err != nil {
		return "", err
	}
	_, body, ok := strings.Cut(encoded, ",")
	if !ok || len(body) > (maxPageDesign+2)/3*4 {
		return "", fmt.Errorf("invalid page design payload")
	}
	raw, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		return "", err
	}
	html, err := unpackPageDesign(raw, hash)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	data, _ := json.Marshal(cachedDesign{UpdatedAt: p.PageDesignUpdatedAt, Archive: body})
	// Never leave a partially written cache entry after an interrupted download.
	f, err := os.CreateTemp(dir, "page-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	if err := os.Rename(f.Name(), target); err != nil {
		return "", err
	}
	return html, nil
}

func unpackPageDesign(raw []byte, hash string) (string, error) {
	if len(raw) > maxPageDesign {
		return "", fmt.Errorf("page design exceeds 4 MiB")
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != hash {
		return "", fmt.Errorf("page design checksum mismatch")
	}
	html := raw
	if bytes.HasPrefix(raw, []byte("PK")) {
		z, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
		if err != nil {
			return "", err
		}
		// A self-contained HTML entry avoids traversal, partial asset updates and
		// arbitrary local-file references. Nothing from the archive is executed.
		if len(z.File) != 1 || z.File[0].Name != "index.html" {
			return "", fmt.Errorf("design ZIP must contain only index.html")
		}
		r, err := z.File[0].Open()
		if err != nil {
			return "", err
		}
		defer r.Close()
		html, err = io.ReadAll(io.LimitReader(r, maxPageDesign+1))
		if err != nil {
			return "", err
		}
	}
	if len(html) == 0 || len(html) > maxPageDesign || !utf8.Valid(html) {
		return "", fmt.Errorf("invalid page design HTML")
	}
	return string(html), nil
}
