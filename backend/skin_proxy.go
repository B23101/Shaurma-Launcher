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
// ЩО КУДИ ЙДЕ. Проксі — це «ванільний» сесійний сервер з однією відмінністю:
// скіни піратів беруться з Shaurma. Усе інше — як у справжньої гри:
//
//	join / hasJoined / minecraftservices (чат-ключ, privileges…)  → Mojang, з токеном
//	профіль за UUID, ліцензійний (справжній Mojang-UUID, v4)       → Shaurma, якщо там є;
//	                                                                 інакше Mojang НАПРЯМУ за UUID
//	профіль за UUID, офлайн (UUIDv3 від ніка, пірат)               → Shaurma, якщо там є;
//	                                                                 інакше Mojang за ніком (якщо відомий)
//	пошук UUID за ніком (одиночний і пакетний)                     → Shaurma + Mojang (що не знайшла Shaurma)
//
// Так ліцензійні гравці бачать скіни піратів (їх віддає Shaurma), а пірати
// бачать скіни ліцензійних (їх віддає Mojang). Токени гравця йдуть ЛИШЕ до
// Mojang, Shaurma отримує тільки UUID/ніки.
//
// ХТО ЦЕЙ ГРАВЕЦЬ. Справжній Mojang-UUID питає Mojang сам — нік не потрібен.
// Офлайн-UUID = MD5("OfflinePlayer:"+нік), назад його не порахувати, тож
// проксі ВЧИТЬ ніки: власні акаунти лаунчера, пакетні запити
// POST /api/profiles/minecraft та рядки логу гри («X joined the game»,
// «[CHAT] <X>», «Setting user: X»). Обмеження: пірат, нік якого лаунчер ще не
// бачив, лишається зі Steve.
//
// DisableMojangSkinFallback вимикає лише підбір скіна Mojang за НІКОМ для
// офлайн-UUID. Запити за справжнім Mojang-UUID — це ванільна поведінка гри, їх
// не чіпаємо.
// При збої проксі ліцензійна гра стартує без інжектора. Прямий запасний
// шлях до Shaurma дозволено лише для офлайн-акаунтів без справжніх токенів.

import (
	"bytes"
	"context"
	"crypto"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
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
	// Пакетний пошук UUID за ніками (до 10 за запит) — те, що ванільний клієнт
	// використовує для SkullOwner-голів тощо.
	mojangBulkNamesAPI = "https://api.minecraftservices.com/minecraft/profile/lookup/bulk/byname"

	// Скільки тримати відповідь Mojang. Позитивна — довго (скін змінюється
	// рідко), негативна — коротше, помилка/ліміт — дуже коротко, щоб не
	// «залипнути» на тимчасовому збої, але й не молотити API (429).
	skinProxyTTLFound   = 15 * time.Minute
	skinProxyTTLMissing = 3 * time.Minute
	skinProxyTTLError   = 30 * time.Second

	skinProxyMaxNicks = 4096
	skinProxyMaxCache = 4096

	// Скільки ніків за один пакетний запит ми готові віддати Mojang.
	mojangBulkChunk = 10
	mojangBulkMax   = 50
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
	mu                    sync.Mutex
	upstream              string // база Yggdrasil Shaurma БЕЗ кінцевого слеша
	disableMojangFallback bool
	dead                  bool // server exited; do not reuse its closed loopback endpoint
	domains               []string
	key                   *rsa.PrivateKey
	pubPEM                string
	port                  int
	nicks                 map[string]string // uuidHex -> нік
	cache                 map[string]mojangEntry
	inflight              map[string]*inflightCall

	client      *http.Client
	mojangAPI   string
	mojangSess  string
	mojangBulk  string // пакетний пошук ніків у Mojang; порожньо — лише Shaurma
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

	if theSkinProxy != nil {
		theSkinProxy.mu.Lock()
		dead := theSkinProxy.dead
		theSkinProxy.mu.Unlock()
		if dead {
			theSkinProxy = nil
		}
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
	p.mu.Lock()
	p.disableMojangFallback = a.cfg.GetSettings().DisableMojangSkinFallback
	p.mu.Unlock()
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
		mojangBulk: mojangBulkNamesAPI,
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
		defer func() { p.mu.Lock(); p.dead = true; p.mu.Unlock() }()
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
	domains = append(domains, "textures.minecraft.net", ".minecraft.net", "127.0.0.1") // 127.0.0.1 — скін агента (ai_agent_game.go)
	return json.Marshal(map[string]any{
		"meta": map[string]any{
			"serverName":              "Shaurma (local)",
			"implementationName":      "shaurma-skin-proxy",
			"implementationVersion":   "1.0.0",
			"feature.non_email_login": false,
			"feature.username_check":  false,
			// Дозволяє injector'у віддавати клієнту чат-ключ профілю (player/certificates)
			// з нашого API root, а не вимикати його. Ключ ми беремо у справжнього Mojang
			// (див. forwardMojang), тож сервери з enforce-secure-profile його приймають.
			"feature.enable_profile_key": true,
			// Без цього authlib-injector підставляє заглушку замість служб Mojang
			// (налаштування гравця, список друзів, blocklist, скарги): вибір не
			// зберігається, і гра перепитує його щоразу. З прапорцем гра говорить
			// зі справжнім Mojang, як без інжектора.
			"feature.enable_mojang_anti_features": true,
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
	if !hostAllowed(r.Host, p.port) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
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
	case r.Method == http.MethodGet && path == "/ai-skin.png":
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "max-age=3600")
		_, _ = w.Write(aiAgentSkinPNG)
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/sessionserver/session/minecraft/profile/"):
		p.handleProfile(w, strings.TrimPrefix(path, "/sessionserver/session/minecraft/profile/"))
	case r.Method == http.MethodPost && path == "/api/profiles/minecraft":
		p.handleBatch(w, r)
	case r.Method == http.MethodPost && path == "/sessionserver/session/minecraft/join":
		p.forwardMojang(w, r, "https://sessionserver.mojang.com/session/minecraft/join")
	case r.Method == http.MethodGet && path == "/sessionserver/session/minecraft/hasJoined":
		p.forwardMojang(w, r, "https://sessionserver.mojang.com/session/minecraft/hasJoined")
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/api/users/profiles/minecraft/"):
		// Пошук UUID за ніком (моди на кшталт ukulib). Без токена і query: це публічний запит.
		p.lookupNameMojang(w, r, strings.TrimPrefix(path, "/api/users/profiles/minecraft/"))
	case strings.HasPrefix(path, "/minecraftservices/"):
		// Сервіси Minecraft (player/certificates — чат-ключ профілю, publickeys,
		// privileges тощо) НЕ належать Shaurma: їх має віддавати справжній Mojang
		// з токеном гравця. Інакше клієнт не отримує підписаний ключ і сервери з
		// enforce-secure-profile=true кажуть «Недійсний підпис публічного ключа».
		p.forwardMojang(w, r, mojangServicesAPI+strings.TrimPrefix(path, "/minecraftservices/"))
	default:
		// Усе, чого проксі не знає, свідомо відкидається (токени не мають іти кудись навмання),
		// але тепер це видно в лозі: клієнт, якому відмовили, часто просто перепитує (як екран «Список друзів»).
		launchlog.Warn("SkinProxy", "невідомий запит відкинуто: %s %s", r.Method, path)
		http.NotFound(w, r)
	}
}

// lookupNameMojang — GET /api/users/profiles/minecraft/{нік} → публічний api.mojang.com без заголовків авторизації.
func (p *skinProxy) lookupNameMojang(w http.ResponseWriter, r *http.Request, name string) {
	if !skinNickValid.MatchString(name) {
		http.Error(w, "invalid profile name", http.StatusBadRequest)
		return
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, mojangProfilesAPI+name, nil)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	req.Header.Set("User-Agent", "ShaurmaLauncher/2.0")
	req.Header.Set("Accept", "application/json")
	client := *p.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		launchlog.Warn("SkinProxy", "Mojang недоступний (пошук нікнейма): %v", err)
		http.Error(w, "mojang: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		http.Error(w, "redirect refused", http.StatusBadGateway)
		return
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(resp.Body, 64<<10))
}

// forwardMojang — пересилка на справжній Mojang із передачею Authorization.
// (forward() заголовок Authorization не передає — він лише для Shaurma.)
func (p *skinProxy) forwardMojang(w http.ResponseWriter, r *http.Request, target string) {
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
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
	// A 307/308 redirect can resend the token-bearing body to another host.
	// Authentication endpoints must never redirect credentials, even over HTTPS.
	client := *p.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		launchlog.Warn("SkinProxy", "Mojang недоступний (%s): %v", r.URL.Path, err)
		http.Error(w, "mojang: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		http.Error(w, "authentication redirect refused", http.StatusBadGateway)
		return
	}
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

// postUpstreamBatch sends only the validated nickname batch to Shaurma; never
// session requests, never credentials.
func (p *skinProxy) postUpstreamBatch(ctx context.Context, names []string) (int, []byte, error) {
	base := p.upstreamBase()
	if base == "" {
		return 0, nil, errors.New("upstream не задано")
	}
	body, _ := json.Marshal(names)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/profiles/minecraft", bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("User-Agent", "ShaurmaLauncher/2.0")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return resp.StatusCode, out, nil
}

type nameRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// mojangBulkLookup — ніки, яких не знає Shaurma, питаємо у справжнього Mojang
// (так працює ванільна гра: голова за SkullOwner, пошук гравця за ніком).
// Без токенів і без query. Збій Mojang — не помилка: повертаємо, що встигли.
func (p *skinProxy) mojangBulkLookup(ctx context.Context, names []string) []nameRef {
	if p.mojangBulk == "" || len(names) == 0 {
		return nil
	}
	if len(names) > mojangBulkMax {
		names = names[:mojangBulkMax]
	}
	client := *p.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	var out []nameRef
	for i := 0; i < len(names); i += mojangBulkChunk {
		end := i + mojangBulkChunk
		if end > len(names) {
			end = len(names)
		}
		body, _ := json.Marshal(names[i:end])
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.mojangBulk, bytes.NewReader(body))
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "ShaurmaLauncher/2.0")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			launchlog.Warn("SkinProxy", "Mojang недоступний (пакетний пошук ніків): %v", err)
			continue
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			continue
		}
		var got []nameRef
		if json.Unmarshal(raw, &got) != nil {
			continue
		}
		for _, g := range got {
			if id := normalizeUUIDHex(g.ID); id != "" && skinNickValid.MatchString(g.Name) {
				out = append(out, nameRef{ID: id, Name: g.Name})
			}
		}
	}
	return out
}

// handleBatch — POST /api/profiles/minecraft ([ "нік", ... ]): вчимо ніки,
// питаємо Shaurma (піратів), а те, чого вона не знає, — справжній Mojang.
func (p *skinProxy) handleBatch(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	var names []string
	if err != nil || json.Unmarshal(body, &names) != nil {
		http.Error(w, "invalid profile names", http.StatusBadRequest)
		return
	}
	for _, n := range names {
		if !skinNickValid.MatchString(n) {
			http.Error(w, "invalid profile name", http.StatusBadRequest)
			return
		}
	}
	for _, n := range names {
		p.learn(n)
	}

	status, upBody, upErr := p.postUpstreamBatch(r.Context(), names)
	if p.mojangBulk == "" {
		// Лише Shaurma (старий режим / тести без Mojang).
		if upErr != nil {
			http.Error(w, "upstream: "+upErr.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(status)
		_, _ = w.Write(upBody)
		return
	}

	found := []nameRef{}
	if upErr == nil && status == http.StatusOK {
		_ = json.Unmarshal(upBody, &found)
		if found == nil {
			found = []nameRef{}
		}
	} else if upErr != nil {
		launchlog.Warn("SkinProxy", "Shaurma недоступна (пакетний пошук ніків): %v", upErr)
	}
	have := make(map[string]bool, len(found))
	for _, f := range found {
		have[strings.ToLower(f.Name)] = true
	}
	var missing []string
	for _, n := range names {
		if k := strings.ToLower(n); !have[k] {
			have[k] = true // не питаємо двічі про дубль
			missing = append(missing, n)
		}
	}
	found = append(found, p.mojangBulkLookup(r.Context(), missing)...)
	writeYgJSON(w, http.StatusOK, found)
}

// fetchUpstreamProfile — профіль із Shaurma. nil, якщо 204/404/помилка.
// upstreamStaleTTL — скільки тримати останню відповідь сервера Шаурми на випадок, якщо він на
// мить недоступний (перезапуск вузла, ліміт): гра лишається з вибором сервера (скін і плащ
// Шаурми), а не провалюється у вигляд Mojang.
const upstreamStaleTTL = 30 * time.Minute

type upstreamEntry struct {
	prof *ygProfile
	at   time.Time
}

var (
	upstreamLastMu sync.Mutex
	upstreamLast   = map[string]upstreamEntry{}
)

func (p *skinProxy) fetchUpstreamProfile(uuidHex string) *ygProfile {
	base := p.upstreamBase()
	if base == "" {
		return nil
	}
	for attempt := 1; attempt <= 3; attempt++ {
		prof, status, err := p.fetchUpstreamOnce(base, uuidHex)
		switch {
		case prof != nil:
			upstreamLastMu.Lock()
			upstreamLast[uuidHex] = upstreamEntry{prof: prof, at: time.Now()}
			upstreamLastMu.Unlock()
			return prof
		case err == nil && status == http.StatusNoContent:
			// Сервер точно знає: такого гравця в Шаурмі немає — це не збій.
			return nil
		case err != nil:
			launchlog.Warn("SkinProxy", "Shaurma недоступна (спроба %d/3): %v", attempt, err)
		default:
			launchlog.Warn("SkinProxy", "Shaurma відповіла HTTP %d (спроба %d/3)", status, attempt)
		}
		if attempt < 3 {
			time.Sleep(time.Duration(attempt) * 700 * time.Millisecond)
		}
	}
	upstreamLastMu.Lock()
	e, ok := upstreamLast[uuidHex]
	upstreamLastMu.Unlock()
	if ok && time.Since(e.at) < upstreamStaleTTL {
		launchlog.Warn("SkinProxy", "профіль %s: сервер не відповів — остання відповідь сервера Шаурми", uuidHex)
		return e.prof
	}
	return nil
}

func (p *skinProxy) fetchUpstreamOnce(base, uuidHex string) (*ygProfile, int, error) {
	req, err := http.NewRequest(http.MethodGet, base+"/sessionserver/session/minecraft/profile/"+uuidHex, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "ShaurmaLauncher/2.0")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, nil
	}
	var prof ygProfile
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&prof); err != nil {
		return nil, resp.StatusCode, err
	}
	return &prof, resp.StatusCode, nil
}

func (p *skinProxy) handleProfile(w http.ResponseWriter, rawUUID string) {
	uuidHex := normalizeUUIDHex(rawUUID)
	if uuidHex == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Акаунти ШІ (Claude, Claude_2…) мають власний скін, який віддається локально, без Shaurma/Mojang.
	if aiNick := p.nickFor(uuidHex); aiNick != "" && aiIsKnownAINick(aiNick) {
		p.serveAIProfile(w, uuidHex, aiNick)
		return
	}

	// Потік 1: Shaurma.
	up := p.fetchUpstreamProfile(uuidHex)
	merged := &texturesDoc{Textures: map[string]json.RawMessage{}}
	nick := ""
	// capeMode — вибір гравця на сервері Шаурми, лише два режими: shaurma (плащ із textures Шаурми;
	// немає — без плаща) і mojang (плащ із профілю Mojang; немає там — без плаща).
	// Порожньо — старий сервер, поведінка як раніше.
	capeMode := ""
	if up != nil {
		nick = up.Name
		for _, pr := range up.Properties {
			if pr.Name == "shaurma:cape" {
				capeMode = pr.Value
			}
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
		var md *texturesDoc
		if isOfflineUUIDShape(uuidHex) {
			// Пірат, якого Shaurma не знає: пробуємо ліцензійний скін за НІКОМ.
			if nick == "" {
				nick = p.nickFor(uuidHex)
			}
			if nick != "" {
				md = p.mojangTextures(nick)
			}
		} else {
			// Справжній Mojang-UUID (ліцензійний гравець): як у ванільній грі —
			// профіль із Mojang напряму, нік не потрібен.
			md = p.mojangTexturesByUUID(uuidHex)
			if md != nil && nick == "" {
				nick = md.ProfileName
			}
		}
		if md != nil {
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

	switch capeMode {
	case "mojang":
		// Плащ — ЛИШЕ з профілю Mojang (на Mojang нічого не змінюється). Немає там плаща — без плаща.
		delete(merged.Textures, "CAPE")
		if !isOfflineUUIDShape(uuidHex) {
			if md := p.mojangTexturesByUUID(uuidHex); md != nil {
				if c, ok := md.Textures["CAPE"]; ok {
					merged.Textures["CAPE"] = c
				}
			}
		}
	case "shaurma", "none": // "none" — сумісність зі старим сервером
		// Плащ — ЛИШЕ з textures Шаурми; плащ Mojang не домішуємо. Немає — без плаща.
		if up == nil || !shaurmaHasCape(up) {
			delete(merged.Textures, "CAPE")
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

// shaurmaHasCape — чи є плащ у textures, які віддав сервер Шаурми.
func shaurmaHasCape(up *ygProfile) bool {
	for _, pr := range up.Properties {
		if pr.Name == "textures" {
			if d := decodeTextures(pr.Value); d != nil {
				if _, ok := d.Textures["CAPE"]; ok {
					return true
				}
			}
		}
	}
	return false
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

// isOfflineUUIDShape — UUIDv3 + варіант IETF: саме такий UUID генерує Java для
// офлайн-акаунтів (UUID.nameUUIDFromBytes). Справжні Mojang-UUID — це v4.
func isOfflineUUIDShape(hexs string) bool {
	return len(hexs) == 32 && hexs[12] == '3' && strings.ContainsRune("89ab", rune(hexs[16]))
}

// mojangTextures — текстури ліцензійного гравця з таким ніком (nil — немає/збій).
// Це додатковий підбір для офлайн-UUID, його можна вимкнути в налаштуваннях.
func (p *skinProxy) mojangTextures(nick string) *texturesDoc {
	if !skinNickValid.MatchString(nick) {
		return nil
	}
	p.mu.Lock()
	disabled := p.disableMojangFallback
	p.mu.Unlock()
	if disabled {
		return nil
	}
	return p.cachedMojang(strings.ToLower(nick), func() (*texturesDoc, time.Duration) {
		return p.fetchMojang(nick)
	})
}

// mojangTexturesByUUID — текстури за справжнім Mojang-UUID. Налаштування
// DisableMojangSkinFallback сюди не застосовується: це звичайний запит
// ванільної гри, без нього ліцензійні гравці стали б Steve.
func (p *skinProxy) mojangTexturesByUUID(uuidHex string) *texturesDoc {
	if normalizeUUIDHex(uuidHex) == "" {
		return nil
	}
	return p.cachedMojang("uuid:"+uuidHex, func() (*texturesDoc, time.Duration) {
		return p.fetchMojangProfile(uuidHex)
	})
}

// cachedMojang — кеш + об'єднання одночасних запитів по одному ключу: сервер на
// 50 гравців не має породжувати 50 однакових звернень до Mojang (ліміти API).
func (p *skinProxy) cachedMojang(key string, fetch func() (*texturesDoc, time.Duration)) *texturesDoc {
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
		if len(p.cache) >= skinProxyMaxCache {
			for k, e := range p.cache { // спершу прострочене, потім — усе
				if time.Since(e.at) >= e.ttl {
					delete(p.cache, k)
				}
			}
			if len(p.cache) >= skinProxyMaxCache {
				p.cache = map[string]mojangEntry{}
			}
		}
		p.cache[key] = mojangEntry{doc: doc, at: time.Now(), ttl: ttl}
		delete(p.inflight, key)
		c.doc = doc
		p.mu.Unlock()
		close(c.done)
	}()
	doc, ttl = fetch()
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
	// 2) UUID → профіль із textures
	return p.fetchMojangProfile(normalizeUUIDHex(id.ID))
}

// fetchMojangProfile — UUID → профіль із textures (без підпису Mojang: ми
// підписуємо самі, бо injector підміняє ключ перевірки на ключ проксі).
func (p *skinProxy) fetchMojangProfile(uuidHex string) (*texturesDoc, time.Duration) {
	resp2, err := p.client.Get(p.mojangSess + uuidHex)
	if err != nil {
		return nil, skinProxyTTLError
	}
	profBody, _ := io.ReadAll(io.LimitReader(resp2.Body, 256<<10))
	resp2.Body.Close()
	switch resp2.StatusCode {
	case http.StatusOK:
	case http.StatusNoContent, http.StatusNotFound:
		return nil, skinProxyTTLMissing // такого профілю в Mojang немає
	default:
		return nil, skinProxyTTLError // 429/5xx — спробуємо пізніше
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
			return nil, skinProxyTTLMissing // профіль є, власного скіна немає
		}
		if d.ProfileName == "" {
			d.ProfileName = prof.Name
		}
		return d, skinProxyTTLFound
	}
	return nil, skinProxyTTLMissing
}

// serveAIProfile — профіль акаунта ШІ з локальним підписаним скіном (PNG віддається маршрутом /ai-skin.png).
func (p *skinProxy) serveAIProfile(w http.ResponseWriter, uuidHex, nick string) {
	skin, _ := json.Marshal(map[string]string{"url": fmt.Sprintf("http://127.0.0.1:%d/ai-skin.png", p.port)})
	doc := &texturesDoc{Timestamp: time.Now().UnixMilli(), ProfileID: uuidHex, ProfileName: nick,
		Textures: map[string]json.RawMessage{"SKIN": skin}}
	raw, err := json.Marshal(doc)
	if err != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	value := base64.StdEncoding.EncodeToString(raw)
	sig, err := p.sign(value)
	if err != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeYgJSON(w, http.StatusOK, ygProfile{ID: uuidHex, Name: nick, Properties: []ygProp{{Name: "textures", Value: value, Signature: sig}}})
}
