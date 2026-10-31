package main

// skin_proxy.go — ЛОКАЛЬНИЙ injector з двома потоками скінів.
//
// Гра (через authlib-injector) ходить ЗА СКІНАМИ в один-єдиний місцевий
// Yggdrasil-API на 127.0.0.1:<порт>. Далі запит розділяється на два джерела:
//
//	гра ──► локальний проксі ──┬─► 1) сервер Shaurma (VPS/Worker)  — завжди першим
//	                           └─► 2) Mojang (api.mojang.com +      — лише для того,
//	                                  sessionserver.mojang.com)        чого в Shaurma немає
//
// Навіщо: у піратів (офлайн-акаунтів) гра бачить лише те, що віддає «сесійний
// сервер». Воркер знає тільки зареєстровані в Shaurma ніки, тож гравець з
// ліцензійним ніком, якого в Shaurma немає, був Steve. Тепер, якщо Shaurma не
// дала скін (або дала тільки плащ), проксі бере скін у Mojang ЗА НІКОМ.
//
// ПІДПИС. Клієнт перевіряє підпис властивості textures ключем з metadata.
// Підпис Mojang не збігся б з ключем Shaurma, а приватного ключа Shaurma в
// лаунчері немає. Тому проксі має СВІЙ RSA-ключ (генерується один раз,
// <dataDir>/java/skinproxy-key.pem), віддає його публічну частину в metadata
// і підписує ним ВСІ профілі — і шаурмівські, і мойданські. Для гри це один
// цілісний «сесійний сервер».
//
// join/hasJoined та все інше проксі прозоро пересилає на Shaurma — там уже є
// логіка форварду на справжній Mojang для ліцензійних ніків.
//
// ХТО ЦЕЙ ГРАВЕЦЬ. Для чужих скінів клієнт питає профіль за UUID, а не за
// ніком. Офлайн-UUID = MD5("OfflinePlayer:"+нік), назад його не порахувати,
// тож проксі ВЧИТЬ ніки: власні акаунти лаунчера, пакетні запити
// POST /api/profiles/minecraft та рядки логу гри («X joined the game»,
// «[CHAT] <X>», «Setting user: X»). UUID→нік для відомих ніків — у таблиці.
// Обмеження: гравець, нік якого лаунчер ще не бачив, лишається зі Steve.
//
// Вимкнення: налаштування DisableMojangSkinFallback (тоді працює старий
// прямий шлях гра → Shaurma). Збій старту проксі теж ТИХО повертає на нього.

import (
	"bytes"
	"crypto"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/console"
	"shaurma-launcher-backend/internal/crashguard"
	"shaurma-launcher-backend/internal/launchlog"
)

const (
	skinProxyKeyFile = "skinproxy-key.pem"

	mojangProfilesAPI = "https://api.mojang.com/users/profiles/minecraft/"
	mojangSessionAPI  = "https://sessionserver.mojang.com/session/minecraft/profile/"
	mojangServicesAPI = "https://api.minecraftservices.com/"

	// Скільки тримати відповідь Mojang. Позитивна — довго (скін змінюється
	// рідко), негативна — коротше, помилка/ліміт — дуже коротко, щоб не
	// «залипнути» на тимчасовому збої, але й не молотити API (429).
	skinProxyTTLFound   = 15 * time.Minute
	skinProxyTTLMissing = 3 * time.Minute
	skinProxyTTLError   = 30 * time.Second

	skinProxyMaxNicks = 4096
)

// ── Типи відповіді Yggdrasil ────────────────────────────────────────────

type ygProp struct {
	Name      string `json:"name"`
	Value     string `json:"value"`
	Signature string `json:"signature,omitempty"`
}

type ygProfile struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Properties []ygProp `json:"properties"`
}

// texturesDoc — розпакований JSON усередині властивості textures.
type texturesDoc struct {
	Timestamp   int64                      `json:"timestamp"`
	ProfileID   string                     `json:"profileId"`
	ProfileName string                     `json:"profileName"`
	Textures    map[string]json.RawMessage `json:"textures"`
}

func decodeTextures(value string) *texturesDoc {
	raw, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil
	}
	var d texturesDoc
	if json.Unmarshal(raw, &d) != nil {
		return nil
	}
	return &d
}

// ── Проксі ──────────────────────────────────────────────────────────────

type mojangEntry struct {
	doc *texturesDoc
	at  time.Time
	ttl time.Duration
}

type inflightCall struct {
	done chan struct{}
	doc  *texturesDoc
}

type skinProxy struct {
	mu       sync.Mutex
	upstream string // база Yggdrasil Shaurma БЕЗ кінцевого слеша
	domains  []string
	key      *rsa.PrivateKey
	pubPEM   string
	port     int
	nicks    map[string]string // uuidHex -> нік
	cache    map[string]mojangEntry
	inflight map[string]*inflightCall

	client      *http.Client
	mojangAPI   string
	mojangSess  string
	srv         *http.Server
	listenAddr  string
	metadataTpl []byte
}

var (
	skinProxyMu  sync.Mutex
	theSkinProxy *skinProxy
)

// ensureSkinProxy піднімає (один раз на процес) локальний проксі, оновлює його
// upstream і повертає API root для -javaagent та metadata (base64) для
// -Dauthlibinjector.yggdrasil.prefetched.
//
// upstreamBase — база Shaurma з yggdrasilEndpoint() (VPS або Worker),
// upstreamMetaB64 — її metadata (потрібні skinDomains для текстур Shaurma).
func (a *App) ensureSkinProxy(upstreamBase, upstreamMetaB64 string, nicks []string) (apiRoot, metaB64 string, err error) {
	skinProxyMu.Lock()
	defer skinProxyMu.Unlock()

	var meta struct {
		SkinDomains []string `json:"skinDomains"`
	}
	raw, derr := base64.StdEncoding.DecodeString(upstreamMetaB64)
	if derr != nil || json.Unmarshal(raw, &meta) != nil {
		return "", "", fmt.Errorf("metadata Shaurma не розібрано")
	}

	if theSkinProxy == nil {
		p, perr := a.newSkinProxy()
		if perr != nil {
			return "", "", perr
		}
		theSkinProxy = p
		// Рядки логу гри → навчання ніків (лише коли проксі існує).
		console.SetLineObserver(skinLearnLine)
	}
	p := theSkinProxy
	p.configure(upstreamBase, meta.SkinDomains)
	for _, n := range nicks {
		p.learn(n)
	}
	m, err := p.metadataJSON()
	if err != nil {
		return "", "", err
	}
	return fmt.Sprintf("http://127.0.0.1:%d/", p.port), base64.StdEncoding.EncodeToString(m), nil
}

func (a *App) newSkinProxy() (*skinProxy, error) {
	key, pubPEM, err := a.loadOrCreateSkinProxyKey()
	if err != nil {
		return nil, fmt.Errorf("ключ локального skin-проксі: %w", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("порт локального skin-проксі: %w", err)
	}
	p := &skinProxy{
		key:        key,
		pubPEM:     pubPEM,
		port:       ln.Addr().(*net.TCPAddr).Port,
		nicks:      map[string]string{},
		cache:      map[string]mojangEntry{},
		inflight:   map[string]*inflightCall{},
		client:     &http.Client{Timeout: 8 * time.Second},
		mojangAPI:  mojangProfilesAPI,
		mojangSess: mojangSessionAPI,
		listenAddr: ln.Addr().String(),
	}
	p.srv = &http.Server{
		Handler:           p,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
	}
	go func() {
		defer crashguard.Recover("skinProxy.Serve")
		if e := p.srv.Serve(ln); e != nil && e != http.ErrServerClosed {
			launchlog.Warn("SkinProxy", "сервер зупинився: %v", e)
		}
	}()
	launchlog.Info("SkinProxy", "локальний skin-injector слухає %s (Shaurma → Mojang)", p.listenAddr)
	return p, nil
}

func (a *App) loadOrCreateSkinProxyKey() (*rsa.PrivateKey, string, error) {
	dir := a.javaToolsDir()
	path := filepath.Join(dir, skinProxyKeyFile)
	if b, err := os.ReadFile(path); err == nil {
		if blk, _ := pem.Decode(b); blk != nil {
			if k, e := x509.ParsePKCS8PrivateKey(blk.Bytes); e == nil {
				if rk, ok := k.(*rsa.PrivateKey); ok {
					pub, perr := pubPEMOf(rk)
					return rk, pub, perr
				}
			}
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, "", err
	}
	rk, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, "", err
	}
	der, err := x509.MarshalPKCS8PrivateKey(rk)
	if err != nil {
		return nil, "", err
	}
	out := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	// Не вдалося зберегти — не страшно: ключ житиме в пам'яті цього запуску.
	_ = os.WriteFile(path, out, 0o600)
	pub, err := pubPEMOf(rk)
	return rk, pub, err
}

func pubPEMOf(k *rsa.PrivateKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(&k.PublicKey)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})), nil
}

func (p *skinProxy) configure(upstreamBase string, domains []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.upstream = strings.TrimRight(upstreamBase, "/") + yggdrasilAPIPath
	p.domains = append([]string(nil), domains...)
}

// metadataJSON — metadata локального «сесійного сервера». skinDomains =
// домени Shaurma + Mojang, інакше authlib-injector відкине текстуру.
func (p *skinProxy) metadataJSON() ([]byte, error) {
	p.mu.Lock()
	domains := append([]string(nil), p.domains...)
	p.mu.Unlock()
	domains = append(domains, "textures.minecraft.net", ".minecraft.net")
	return json.Marshal(map[string]any{
		"meta": map[string]any{
			"serverName":            "Shaurma (local)",
			"implementationName":    "shaurma-skin-proxy",
			"implementationVersion": "1.0.0",
			"feature.non_email_login": false,
			"feature.username_check":  false,
			// Дозволяє injector'у віддавати клієнту чат-ключ профілю (player/certificates)
			// з нашого API root, а не вимикати його. Ключ ми беремо у справжнього Mojang
			// (див. forwardMojang), тож сервери з enforce-secure-profile його приймають.
			"feature.enable_profile_key": true,
		},
		"skinDomains":        domains,
		"signaturePublickey": p.pubPEM,
	})
}

// ── Нікнейми ────────────────────────────────────────────────────────────

var skinNickValid = regexp.MustCompile(`^[A-Za-z0-9_]{1,16}$`)

// offlineUUIDHex — UUID v3 від "OfflinePlayer:"+нік (як ванільний клієнт),
// без дефісів, нижній регістр.
func offlineUUIDHex(nick string) string {
	sum := md5.Sum([]byte("OfflinePlayer:" + nick))
	sum[6] = (sum[6] & 0x0f) | 0x30
	sum[8] = (sum[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x", sum[:])
}

func normalizeUUIDHex(raw string) string {
	s := strings.ToLower(strings.ReplaceAll(raw, "-", ""))
	if len(s) != 32 {
		return ""
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return ""
		}
	}
	return s
}

func (p *skinProxy) learn(nick string) {
	nick = strings.TrimSpace(nick)
	if !skinNickValid.MatchString(nick) {
		return
	}
	id := offlineUUIDHex(nick)
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.nicks[id]; !ok && len(p.nicks) >= skinProxyMaxNicks {
		for k := range p.nicks { // таблиця повна — викидаємо довільний запис
			delete(p.nicks, k)
			break
		}
	}
	p.nicks[id] = nick
}

func (p *skinProxy) nickFor(uuidHex string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.nicks[uuidHex]
}

var skinLogNickRes = []*regexp.Regexp{
	regexp.MustCompile(`\b([A-Za-z0-9_]{3,16}) (?:joined|left) the game\b`),
	regexp.MustCompile(`\[CHAT\] <([A-Za-z0-9_]{3,16})>`),
	regexp.MustCompile(`Setting user: ([A-Za-z0-9_]{3,16})`),
}

// skinLearnLine — спостерігач рядків консолі (console.LineObserver). Викликається
// для КОЖНОГО рядка, тому спершу дешева перевірка підрядків.
func skinLearnLine(text string) {
	if len(text) > 400 {
		return
	}
	if !strings.Contains(text, "the game") && !strings.Contains(text, "[CHAT]") && !strings.Contains(text, "Setting user") {
		return
	}
	skinProxyMu.Lock()
	p := theSkinProxy
	skinProxyMu.Unlock()
	if p == nil {
		return
	}
	for _, re := range skinLogNickRes {
		if m := re.FindStringSubmatch(text); m != nil {
			p.learn(m[1])
		}
	}
}

// ── HTTP ────────────────────────────────────────────────────────────────

func (p *skinProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case (path == "/" || path == "") && r.Method == http.MethodGet:
		m, err := p.metadataJSON()
		if err != nil {
			writeYgJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write(m)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/sessionserver/session/minecraft/profile/"):
		p.handleProfile(w, strings.TrimPrefix(path, "/sessionserver/session/minecraft/profile/"))
	case r.Method == http.MethodPost && path == "/api/profiles/minecraft":
		p.handleBatch(w, r)
	case strings.HasPrefix(path, "/minecraftservices/"):
		// Сервіси Minecraft (player/certificates — чат-ключ профілю, publickeys,
		// privileges тощо) НЕ належать Shaurma: їх має віддавати справжній Mojang
		// з токеном гравця. Інакше клієнт не отримує підписаний ключ і сервери з
		// enforce-secure-profile=true кажуть «Недійсний підпис публічного ключа».
		p.forwardMojang(w, r, mojangServicesAPI+strings.TrimPrefix(path, "/minecraftservices/"))
	default:
		launchlog.Info("SkinProxy", "forward → Shaurma: %s %s", r.Method, path)
		p.forward(w, r)
	}
}

// forwardMojang — пересилка на справжній Mojang із передачею Authorization.
// (forward() заголовок Authorization не передає — він лише для Shaurma.)
func (p *skinProxy) forwardMojang(w http.ResponseWriter, r *http.Request, target string) {
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	for _, h := range []string{"Authorization", "Content-Type", "Accept"} {
		if v := r.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	resp, err := p.client.Do(req)
	if err != nil {
		launchlog.Warn("SkinProxy", "Mojang недоступний (%s): %v", r.URL.Path, err)
		http.Error(w, "mojang: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	launchlog.Info("SkinProxy", "forward → Mojang: %s %s → %d", r.Method, r.URL.Path, resp.StatusCode)
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(resp.Body, 4<<20))
}

func writeYgJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (p *skinProxy) upstreamBase() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.upstream
}

// forward — прозора пересилка на Shaurma (join, hasJoined, усе невідоме).
func (p *skinProxy) forward(w http.ResponseWriter, r *http.Request) {
	base := p.upstreamBase()
	if base == "" {
		http.Error(w, "upstream не задано", http.StatusBadGateway)
		return
	}
	target := base + r.URL.Path
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	req.Header.Set("User-Agent", "ShaurmaLauncher/2.0")
	for _, h := range []string{"Content-Type", "Accept"} {
		if v := r.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	resp, err := p.client.Do(req)
	if err != nil {
		http.Error(w, "upstream: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(resp.Body, 4<<20))
}

// handleBatch — POST /api/profiles/minecraft ([ "нік", ... ]): вчимо ніки і
// пересилаємо на Shaurma без змін.
func (p *skinProxy) handleBatch(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	var names []string
	if json.Unmarshal(body, &names) == nil {
		for _, n := range names {
			p.learn(n)
		}
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	p.forward(w, r)
}

// fetchUpstreamProfile — профіль із Shaurma. nil, якщо 204/404/помилка.
func (p *skinProxy) fetchUpstreamProfile(uuidHex string) *ygProfile {
	base := p.upstreamBase()
	if base == "" {
		return nil
	}
	req, err := http.NewRequest(http.MethodGet, base+"/sessionserver/session/minecraft/profile/"+uuidHex, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "ShaurmaLauncher/2.0")
	resp, err := p.client.Do(req)
	if err != nil {
		launchlog.Warn("SkinProxy", "Shaurma недоступна: %v", err)
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var prof ygProfile
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&prof) != nil {
		return nil
	}
	return &prof
}

func (p *skinProxy) handleProfile(w http.ResponseWriter, rawUUID string) {
	uuidHex := normalizeUUIDHex(rawUUID)
	if uuidHex == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Потік 1: Shaurma.
	up := p.fetchUpstreamProfile(uuidHex)
	merged := &texturesDoc{Textures: map[string]json.RawMessage{}}
	nick := ""
	if up != nil {
		nick = up.Name
		for _, pr := range up.Properties {
			if pr.Name == "textures" {
				if d := decodeTextures(pr.Value); d != nil {
					merged.Timestamp = d.Timestamp
					for k, v := range d.Textures {
						merged.Textures[k] = v
					}
				}
			}
		}
	}

	// Потік 2: Mojang — лише якщо Shaurma не дала СКІН.
	source := "shaurma"
	if _, hasSkin := merged.Textures["SKIN"]; !hasSkin {
		if nick == "" {
			nick = p.nickFor(uuidHex)
		}
		if nick != "" {
			if md := p.mojangTextures(nick); md != nil {
				for k, v := range md.Textures {
					if _, have := merged.Textures[k]; !have { // плащ Shaurma не перетираємо
						merged.Textures[k] = v
					}
				}
				if merged.Timestamp == 0 {
					merged.Timestamp = md.Timestamp
				}
				source = "mojang"
			}
		}
	}

	if len(merged.Textures) == 0 {
		if up != nil { // профіль відомий, але без текстур — як віддала Shaurma
				writeYgJSON(w, http.StatusOK, ygProfile{ID: uuidHex, Name: up.Name, Properties: []ygProp{}})
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	merged.ProfileID = uuidHex
	merged.ProfileName = nick
	raw, err := json.Marshal(merged)
	if err != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	value := base64.StdEncoding.EncodeToString(raw)
	sig, err := p.sign(value)
	if err != nil {
		launchlog.Error("SkinProxy", "підпис textures: %v", err)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	launchlog.Debug("SkinProxy", "профіль %s (%s): джерело %s", uuidHex, nick, source)
	writeYgJSON(w, http.StatusOK, ygProfile{
		ID:         uuidHex,
		Name:       nick,
		Properties: []ygProp{{Name: "textures", Value: value, Signature: sig}},
	})
}

func (p *skinProxy) sign(value string) (string, error) {
	h := sha1.Sum([]byte(value))
	s, err := rsa.SignPKCS1v15(rand.Reader, p.key, crypto.SHA1, h[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(s), nil
}

// ── Mojang (потік 2) ────────────────────────────────────────────────────

// mojangTextures — текстури ліцензійного гравця з таким ніком (nil — немає/збій).
// Кеш + об'єднання одночасних запитів по одному ніку: сервер на 50 гравців не
// має породжувати 50 однакових звернень до Mojang (ліміти API).
func (p *skinProxy) mojangTextures(nick string) *texturesDoc {
	if !skinNickValid.MatchString(nick) {
		return nil
	}
	key := strings.ToLower(nick)

	p.mu.Lock()
	if e, ok := p.cache[key]; ok && time.Since(e.at) < e.ttl {
		p.mu.Unlock()
		return e.doc
	}
	if c, ok := p.inflight[key]; ok {
		p.mu.Unlock()
		<-c.done
		return c.doc
	}
	c := &inflightCall{done: make(chan struct{})}
	p.inflight[key] = c
	p.mu.Unlock()

	var doc *texturesDoc
	ttl := skinProxyTTLError
	defer func() {
		// Завжди звільняємо чекальників, навіть при паніці в fetch.
		_ = recover()
		p.mu.Lock()
		p.cache[key] = mojangEntry{doc: doc, at: time.Now(), ttl: ttl}
		delete(p.inflight, key)
		c.doc = doc
		p.mu.Unlock()
		close(c.done)
	}()
	doc, ttl = p.fetchMojang(nick)
	return doc
}

func (p *skinProxy) fetchMojang(nick string) (*texturesDoc, time.Duration) {
	// 1) нік → UUID Mojang
	resp, err := p.client.Get(p.mojangAPI + url.PathEscape(nick))
	if err != nil {
		return nil, skinProxyTTLError
	}
	idBody, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound:
		return nil, skinProxyTTLMissing // такого ліцензійного ніка немає
	case resp.StatusCode != http.StatusOK:
		return nil, skinProxyTTLError // 429/5xx — спробуємо пізніше
	}
	var id struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(idBody, &id) != nil || normalizeUUIDHex(id.ID) == "" {
		return nil, skinProxyTTLError
	}

	// 2) UUID → профіль із textures (без підпису Mojang: ми підписуємо самі)
	resp2, err := p.client.Get(p.mojangSess + normalizeUUIDHex(id.ID))
	if err != nil {
		return nil, skinProxyTTLError
	}
	profBody, _ := io.ReadAll(io.LimitReader(resp2.Body, 256<<10))
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		return nil, skinProxyTTLError
	}
	var prof ygProfile
	if json.Unmarshal(profBody, &prof) != nil {
		return nil, skinProxyTTLError
	}
	for _, pr := range prof.Properties {
		if pr.Name != "textures" {
			continue
		}
		d := decodeTextures(pr.Value)
		if d == nil || len(d.Textures) == 0 {
			return nil, skinProxyTTLMissing // ліцензія є, власного скіна немає
		}
		return d, skinProxyTTLFound
	}
	return nil, skinProxyTTLMissing
}
