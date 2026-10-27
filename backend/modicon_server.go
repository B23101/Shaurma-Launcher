package main

// modicon_server.go — іконки модів як ЗВИЧАЙНІ URL, а не base64 через міст.
//
// ПРОБЛЕМА: GetModIcons віддавав іконки як data URL через Wails IPC. Кожна
// іконка проходила Go-рядок → cgo/C-рядок → JSON → JS-рядок (кілька копій у
// різних купах), а потім ще жила в JS-кеші та в <img> як ДРУГА копія
// (рядок + декодований бітмап).
//
// ТЕПЕР: бекенд один раз підіймає loopback-сервер (127.0.0.1, випадковий
// порт, випадковий токен у шляху) і віддає байти іконки як
// `image/png` з Cache-Control. У JS іде лише короткий URL, а картинку
// кешує/евіктить сам WebView (дисковий/пам'ятевий кеш браузера), без жодної
// копії в купі JS і без роботи моста на кожну іконку.
//
// Безпека (сервер слухає лише loopback, але на машині є інші процеси і
// браузерні вкладки):
//   - токен у шляху (crypto/rand) — стороння сторінка не вгадає URL;
//   - перевірка Host (захист від DNS rebinding);
//   - лише GET/HEAD, жодних листингів тек, packID/fileName — тільки basename;
//   - віддаємо виключно вбудовану іконку jar, довільні файли не читаємо.

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const modIconRoutePrefix = "/modicon/"

var modIconSrv struct {
	once  sync.Once
	base  string // http://127.0.0.1:PORT/modicon/TOKEN — порожньо, якщо не вдалось стартувати
	token string
	port  int
	srv   *http.Server
}

// GetModIconBase повертає базовий URL локального сервера іконок
// (`http://127.0.0.1:<port>/modicon/<token>`) або "" — тоді UI падає назад
// на GetModIcons. Сервер стартує ліниво, при першому виклику.
func (a *App) GetModIconBase() string {
	modIconSrv.once.Do(func() { a.startModIconServer() })
	return modIconSrv.base
}

func (a *App) startModIconServer() {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		_ = ln.Close()
		return
	}
	token := hex.EncodeToString(raw)
	port := ln.Addr().(*net.TCPAddr).Port

	mux := http.NewServeMux()
	mux.HandleFunc(modIconRoutePrefix, func(w http.ResponseWriter, r *http.Request) {
		a.serveModIcon(w, r, token, port)
	})
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	modIconSrv.srv = srv
	modIconSrv.token = token
	modIconSrv.port = port
	modIconSrv.base = fmt.Sprintf("http://127.0.0.1:%d%s%s", port, modIconRoutePrefix, token)
	go func() { _ = srv.Serve(ln) }()
}

// stopModIconServer закриває сервер при вимкненні бекенду (безпечно, якщо
// він так і не стартував).
func stopModIconServer() {
	if modIconSrv.srv != nil {
		_ = modIconSrv.srv.Close()
	}
}

func hostAllowed(host string, port int) bool {
	p := strconv.Itoa(port)
	return host == "127.0.0.1:"+p || host == "localhost:"+p
}

func (a *App) serveModIcon(w http.ResponseWriter, r *http.Request, token string, port int) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !hostAllowed(r.Host, port) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	// /modicon/<token>/<packID>/<fileName>
	rest := strings.TrimPrefix(r.URL.Path, modIconRoutePrefix)
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) != 3 {
		http.NotFound(w, r)
		return
	}
	if subtle.ConstantTimeCompare([]byte(parts[0]), []byte(token)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	packID, fileName := parts[1], parts[2]
	for _, s := range []string{packID, fileName} {
		if s == "" || s == "." || s == ".." || s != filepath.Base(s) || strings.ContainsAny(s, `\/`) {
			http.NotFound(w, r)
			return
		}
	}

	jar := a.modMgr.ResolveJarPath(a.instanceModsDir(packID), fileName)
	if jar == "" {
		http.NotFound(w, r)
		return
	}
	dataURL := a.modMgr.IconDataOf(jar)
	mime, body, ok := decodeImageDataURL(dataURL)
	if !ok {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Content-Type", mime)
	h.Set("Content-Length", strconv.Itoa(len(body)))
	// URL несе ревізію файлу (?v=<розмір>), тож безпечно кешувати надовго.
	h.Set("Cache-Control", "private, max-age=86400")
	h.Set("X-Content-Type-Options", "nosniff")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body)
}

// decodeImageDataURL розбирає `data:image/<type>;base64,<payload>`.
// Приймаємо лише image/*, щоб сервер ніколи не віддав html/js.
func decodeImageDataURL(s string) (mime string, body []byte, ok bool) {
	const pfx = "data:"
	if !strings.HasPrefix(s, pfx) {
		return "", nil, false
	}
	comma := strings.IndexByte(s, ',')
	if comma < 0 {
		return "", nil, false
	}
	meta := s[len(pfx):comma]
	if !strings.HasSuffix(meta, ";base64") {
		return "", nil, false
	}
	mime = strings.TrimSuffix(meta, ";base64")
	switch mime {
	case "image/png", "image/jpeg", "image/gif", "image/webp":
	default:
		return "", nil, false
	}
	b, err := base64.StdEncoding.DecodeString(s[comma+1:])
	if err != nil || len(b) == 0 {
		return "", nil, false
	}
	return mime, b, true
}
