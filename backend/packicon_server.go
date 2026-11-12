package main

// packicon_server.go — іконки файлів збірки для анімації експорту.
//
// Сервер іконок модів (modicon_server.go) віддає лише моди з mods/. Анімація
// експорту показує ще й ресурспаки (pack.png усередині .zip або в теці) —
// тому додатковий маршрут /packicon/<token>/<packID>/<відносний шлях>.
// Той самий loopback-сервер, той самий токен і перевірка Host; читаємо лише
// mods/, resourcepacks/, shaderpacks/ у теці збірки, шлях не виходить за її
// межі, віддаємо тільки зображення (до 96 px).

import (
	"archive/zip"
	"crypto/subtle"
	"encoding/base64"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const packIconRoutePrefix = "/packicon/"

// packIconMaxBytes — стеля на розмір pack.png (захист від «зіпбомби»).
const packIconMaxBytes = 4 << 20

func (a *App) servePackIcon(w http.ResponseWriter, r *http.Request, token string, port int) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !hostAllowed(r.Host, port) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, packIconRoutePrefix)
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) != 3 {
		http.NotFound(w, r)
		return
	}
	if subtle.ConstantTimeCompare([]byte(parts[0]), []byte(token)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	packID, rel := parts[1], parts[2]
	if packID == "" || packID == "." || packID == ".." || packID != filepath.Base(packID) || strings.ContainsAny(packID, `\/`) {
		http.NotFound(w, r)
		return
	}
	segs := strings.Split(rel, "/")
	if len(segs) < 2 || strings.Contains(rel, `\`) {
		http.NotFound(w, r)
		return
	}
	for _, s := range segs {
		if s == "" || s == "." || s == ".." {
			http.NotFound(w, r)
			return
		}
	}
	var dataURL string
	switch segs[0] {
	case "mods":
		if len(segs) != 2 {
			http.NotFound(w, r)
			return
		}
		if jar := a.modMgr.ResolveJarPath(filepath.Join(a.gameDir(packID), "mods"), segs[1]); jar != "" {
			dataURL = a.modMgr.IconDataOf(jar)
		}
	case "resourcepacks":
		dataURL = assetThumbnail(resourcePackIcon(filepath.Join(a.gameDir(packID), "resourcepacks", segs[1])), 96)
	default:
		http.NotFound(w, r)
		return
	}
	mime, body, ok := decodeImageDataURL(dataURL)
	if !ok {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", mime)
	h.Set("Content-Length", strconv.Itoa(len(body)))
	h.Set("Cache-Control", "private, max-age=3600")
	h.Set("X-Content-Type-Options", "nosniff")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body)
}

// resourcePackIcon повертає pack.png ресурспаку (тека або .zip) як
// data:image/png URL; порожньо, якщо іконки нема. .disabled не зустрічається
// у ресурспаків, тож додаткових суфіксів не обробляємо.
func resourcePackIcon(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return ""
	}
	var data []byte
	if st.IsDir() {
		f, err := os.Open(filepath.Join(path, "pack.png"))
		if err != nil {
			return ""
		}
		defer f.Close()
		data, err = io.ReadAll(io.LimitReader(f, packIconMaxBytes+1))
		if err != nil {
			return ""
		}
	} else {
		zr, err := zip.OpenReader(path)
		if err != nil {
			return ""
		}
		defer zr.Close()
		for _, f := range zr.File {
			if f.Name != "pack.png" {
				continue
			}
			if f.UncompressedSize64 > packIconMaxBytes {
				return ""
			}
			rc, err := f.Open()
			if err != nil {
				return ""
			}
			data, err = io.ReadAll(io.LimitReader(rc, packIconMaxBytes+1))
			rc.Close()
			if err != nil {
				return ""
			}
			break
		}
	}
	if len(data) == 0 || len(data) > packIconMaxBytes {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)
}
