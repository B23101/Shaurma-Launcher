package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func texturesValueFor(t *testing.T, textures map[string]string) string {
	t.Helper()
	m := map[string]json.RawMessage{}
	for k, u := range textures {
		b, _ := json.Marshal(map[string]string{"url": u})
		m[k] = b
	}
	raw, _ := json.Marshal(texturesDoc{Timestamp: 1, Textures: m})
	return base64.StdEncoding.EncodeToString(raw)
}

type proxyFixture struct {
	p          *skinProxy
	mojangHits atomic.Int32
	mojangURL  string
	// Ліцензійні гравці зі справжнім (v4) UUID: uuid → нік, uuid → URL скіна.
	realNames map[string]string
	realSkins map[string]string
	realCapes map[string]string // uuid → URL плаща Mojang
	// Імена, які мойданський bulk-пошук побачив у запитах.
	bulkAsked []string
	bulkKnown map[string]string // нік → uuid
}

// newProxyFixture: підроблені Shaurma (profiles: uuid → профіль) і Mojang
// (mojangSkins: нік → URL скіна; відсутній нік = немає ліцензії).
func newProxyFixture(t *testing.T, shaurma map[string]ygProfile, mojangSkins map[string]string) *proxyFixture {
	t.Helper()
	fx := &proxyFixture{realNames: map[string]string{}, realSkins: map[string]string{}, realCapes: map[string]string{}, bulkKnown: map[string]string{}}

	sh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "/api/yggdrasil/sessionserver/session/minecraft/profile/"
		if strings.HasPrefix(r.URL.Path, prefix) {
			if prof, ok := shaurma[strings.TrimPrefix(r.URL.Path, prefix)]; ok {
				_ = json.NewEncoder(w).Encode(prof)
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(sh.Close)

	mj := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fx.mojangHits.Add(1)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/bulk":
			var asked []string
			_ = json.NewDecoder(r.Body).Decode(&asked)
			fx.bulkAsked = append(fx.bulkAsked, asked...)
			out := []map[string]string{}
			for _, n := range asked {
				if id, ok := fx.bulkKnown[n]; ok {
					out = append(out, map[string]string{"id": id, "name": n})
				}
			}
			_ = json.NewEncoder(w).Encode(out)
		case strings.HasPrefix(r.URL.Path, "/session/minecraft/profile/") && fx.realNames[strings.TrimPrefix(r.URL.Path, "/session/minecraft/profile/")] != "":
			id := strings.TrimPrefix(r.URL.Path, "/session/minecraft/profile/")
			tex := map[string]string{"SKIN": fx.realSkins[id]}
			if c := fx.realCapes[id]; c != "" {
				tex["CAPE"] = c
			}
			_ = json.NewEncoder(w).Encode(ygProfile{ID: id, Name: fx.realNames[id], Properties: []ygProp{{
				Name: "textures", Value: texturesValueFor(t, tex),
			}}})
		case strings.HasPrefix(r.URL.Path, "/users/profiles/minecraft/"):
			nick := strings.TrimPrefix(r.URL.Path, "/users/profiles/minecraft/")
			if _, ok := mojangSkins[nick]; !ok {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"id": offlineUUIDHex("mojang:" + nick), "name": nick})
		case strings.HasPrefix(r.URL.Path, "/session/minecraft/profile/"):
			for nick, skin := range mojangSkins {
				if strings.TrimPrefix(r.URL.Path, "/session/minecraft/profile/") == offlineUUIDHex("mojang:"+nick) {
					_ = json.NewEncoder(w).Encode(ygProfile{Name: nick, Properties: []ygProp{{
						Name: "textures", Value: texturesValueFor(t, map[string]string{"SKIN": skin}),
					}}})
					return
				}
			}
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(mj.Close)

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pub, _ := pubPEMOf(key)
	fx.mojangURL = mj.URL
	fx.p = &skinProxy{
		key: key, pubPEM: pub,
		upstream:   sh.URL + yggdrasilAPIPath,
		nicks:      map[string]string{},
		cache:      map[string]mojangEntry{},
		inflight:   map[string]*inflightCall{},
		client:     &http.Client{Timeout: 3 * time.Second},
		mojangAPI:  mj.URL + "/users/profiles/minecraft/",
		mojangSess: mj.URL + "/session/minecraft/profile/",
	}
	return fx
}

func getProfile(t *testing.T, p *skinProxy, uuid string) (int, ygProfile) {
	t.Helper()
	rec := httptest.NewRecorder()
	p.handleProfile(rec, uuid)
	var prof ygProfile
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &prof); err != nil {
			t.Fatalf("некоректна відповідь: %v", err)
		}
	}
	return rec.Code, prof
}

func texturesOf(t *testing.T, prof ygProfile) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, pr := range prof.Properties {
		if pr.Name != "textures" {
			continue
		}
		d := decodeTextures(pr.Value)
		if d == nil {
			t.Fatal("textures не розпакувались")
		}
		for k, raw := range d.Textures {
			var u struct {
				URL string `json:"url"`
			}
			_ = json.Unmarshal(raw, &u)
			out[k] = u.URL
		}
	}
	return out
}

func verifySig(t *testing.T, p *skinProxy, prof ygProfile) {
	t.Helper()
	pr := prof.Properties[0]
	sig, err := base64.StdEncoding.DecodeString(pr.Signature)
	if err != nil {
		t.Fatal(err)
	}
	h := sha1.Sum([]byte(pr.Value))
	if err := rsa.VerifyPKCS1v15(&p.key.PublicKey, crypto.SHA1, h[:], sig); err != nil {
		t.Fatalf("підпис не збігається з публічним ключем проксі: %v", err)
	}
}

// Потік 1: скін Shaurma віддається як є (перепідписаний), Mojang не чіпаємо.
func TestSkinProxy_ShaurmaSkinWinsAndMojangIsNotAsked(t *testing.T) {
	id := offlineUUIDHex("Pirate")
	fx := newProxyFixture(t, map[string]ygProfile{
		id: {ID: id, Name: "Pirate", Properties: []ygProp{{Name: "textures",
			Value: texturesValueFor(t, map[string]string{"SKIN": "https://shaurma.example/tex/a.png"})}}},
	}, map[string]string{"Pirate": "https://textures.minecraft.net/texture/zzz"})

	code, prof := getProfile(t, fx.p, id)
	if code != 200 {
		t.Fatalf("код %d", code)
	}
	if got := texturesOf(t, prof)["SKIN"]; got != "https://shaurma.example/tex/a.png" {
		t.Fatalf("скін не з Shaurma: %q", got)
	}
	verifySig(t, fx.p, prof)
	if fx.mojangHits.Load() != 0 {
		t.Fatal("Mojang не мав питатись, коли Shaurma дала скін")
	}
}

// Потік 2: Shaurma не знає гравця, нік відомий проксі → скін з Mojang.
func TestSkinProxy_FallsBackToMojangByKnownNick(t *testing.T) {
	fx := newProxyFixture(t, nil, map[string]string{"Notch": "https://textures.minecraft.net/texture/abc"})
	fx.p.learn("Notch")

	code, prof := getProfile(t, fx.p, offlineUUIDHex("Notch"))
	if code != 200 {
		t.Fatalf("код %d", code)
	}
	if got := texturesOf(t, prof)["SKIN"]; got != "https://textures.minecraft.net/texture/abc" {
		t.Fatalf("скін не з Mojang: %q", got)
	}
	if prof.Name != "Notch" || prof.ID != offlineUUIDHex("Notch") {
		t.Fatalf("профіль має віддаватись під офлайн-UUID: %+v", prof)
	}
	verifySig(t, fx.p, prof)
}

// Shaurma дала лише плащ → скін добираємо в Mojang, плащ Shaurma лишається.
func TestSkinProxy_MergesCapeFromShaurmaWithMojangSkin(t *testing.T) {
	id := offlineUUIDHex("Dream")
	fx := newProxyFixture(t, map[string]ygProfile{
		id: {ID: id, Name: "Dream", Properties: []ygProp{{Name: "textures",
			Value: texturesValueFor(t, map[string]string{"CAPE": "https://shaurma.example/cape.png"})}}},
	}, map[string]string{"Dream": "https://textures.minecraft.net/texture/dream"})

	_, prof := getProfile(t, fx.p, id)
	tx := texturesOf(t, prof)
	if tx["CAPE"] != "https://shaurma.example/cape.png" || tx["SKIN"] != "https://textures.minecraft.net/texture/dream" {
		t.Fatalf("очікував плащ Shaurma + скін Mojang, отримав %v", tx)
	}
}

// Невідомий проксі гравець без запису в Shaurma → 204 (Steve), Mojang не питаємо.
func TestSkinProxy_UnknownPlayerGetsNothing(t *testing.T) {
	fx := newProxyFixture(t, nil, map[string]string{"Notch": "https://textures.minecraft.net/texture/abc"})
	code, _ := getProfile(t, fx.p, offlineUUIDHex("Stranger"))
	if code != http.StatusNoContent {
		t.Fatalf("очікував 204, код %d", code)
	}
	if fx.mojangHits.Load() != 0 {
		t.Fatal("без відомого ніка до Mojang ходити не можна")
	}
}

// Нік без ліцензії в Mojang → 204, і повторний запит іде з кешу.
func TestSkinProxy_MojangMissCachedAndNoLicense(t *testing.T) {
	fx := newProxyFixture(t, nil, map[string]string{})
	fx.p.learn("NoLicense")
	for i := 0; i < 3; i++ {
		if code, _ := getProfile(t, fx.p, offlineUUIDHex("NoLicense")); code != http.StatusNoContent {
			t.Fatalf("очікував 204, код %d", code)
		}
	}
	if n := fx.mojangHits.Load(); n != 1 {
		t.Fatalf("негативна відповідь має кешуватись: звернень до Mojang %d, очікував 1", n)
	}
}

// Рядки логу гри вчать ніки.
func TestSkinLearnLine(t *testing.T) {
	skinProxyMu.Lock()
	old := theSkinProxy
	theSkinProxy = &skinProxy{nicks: map[string]string{}}
	p := theSkinProxy
	skinProxyMu.Unlock()
	t.Cleanup(func() { skinProxyMu.Lock(); theSkinProxy = old; skinProxyMu.Unlock() })

	for _, line := range []string{
		"[12:00:01] [Render thread/INFO]: [CHAT] Alex_01 joined the game",
		"[12:00:02] [Render thread/INFO]: [CHAT] <Steve_2> привіт",
		"[12:00:00] [main/INFO]: Setting user: Herobrine",
		"[12:00:03] [Server thread/INFO]: unrelated line",
	} {
		skinLearnLine(line)
	}
	for _, n := range []string{"Alex_01", "Steve_2", "Herobrine"} {
		if p.nickFor(offlineUUIDHex(n)) != n {
			t.Errorf("нік %s не вивчено", n)
		}
	}
	if len(p.nicks) != 3 {
		t.Errorf("вивчено зайве: %v", p.nicks)
	}
}

func TestOfflineUUIDHex(t *testing.T) {
	id := offlineUUIDHex("Notch")
	if len(id) != 32 || id[12] != '3' {
		t.Fatalf("це не UUID v3: %s", id)
	}
	if normalizeUUIDHex(id[:8]+"-"+id[8:12]+"-"+id[12:16]+"-"+id[16:20]+"-"+id[20:]) != id {
		t.Fatal("normalizeUUIDHex має прибирати дефіси")
	}
	if normalizeUUIDHex("zz") != "" {
		t.Fatal("сміття має відкидатись")
	}
}

func TestSkinProxyRejectsUntrustedHost(t *testing.T) {
	p := &skinProxy{port: 12345}
	for _, host := range []string{"evil.test:12345", "127.0.0.1:9999", "127.0.0.1:12345.evil.test"} {
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:12345/", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("host %s: status %d", host, rec.Code)
		}
	}
}

// ── Маршрутизація: ліцензійні (справжній Mojang-UUID) і пірати ──────────

// Справжній v4-UUID, якого Shaurma не знає → профіль напряму з Mojang, без жодного ніка.
func TestSkinProxy_LicensedPlayerComesFromMojangByRealUUID(t *testing.T) {
	const real = "069a79f444e94726a5befca90e38aaf5" // v4
	fx := newProxyFixture(t, nil, nil)
	fx.realNames[real] = "Notch"
	fx.realSkins[real] = "https://textures.minecraft.net/texture/notch"
	// Навіть коли додатковий підбір за ніком вимкнено: це ванільний запит гри.
	fx.p.disableMojangFallback = true

	code, prof := getProfile(t, fx.p, real)
	if code != 200 {
		t.Fatalf("код %d", code)
	}
	if got := texturesOf(t, prof)["SKIN"]; got != "https://textures.minecraft.net/texture/notch" {
		t.Fatalf("скін не з Mojang: %q", got)
	}
	if prof.ID != real || prof.Name != "Notch" {
		t.Fatalf("профіль має віддаватись під справжнім UUID і нікі Mojang: %+v", prof)
	}
	verifySig(t, fx.p, prof)
}

// Shaurma знає цей UUID (акаунт пов'язаний) → скін Shaurma, Mojang не питаємо.
func TestSkinProxy_RealUUIDKnownToShaurmaStaysWithShaurma(t *testing.T) {
	const real = "069a79f444e94726a5befca90e38aaf5"
	fx := newProxyFixture(t, map[string]ygProfile{
		real: {ID: real, Name: "Notch", Properties: []ygProp{{Name: "textures",
			Value: texturesValueFor(t, map[string]string{"SKIN": "https://shaurma.example/tex/n.png"})}}},
	}, nil)
	fx.realNames[real] = "Notch"
	fx.realSkins[real] = "https://textures.minecraft.net/texture/notch"

	_, prof := getProfile(t, fx.p, real)
	if got := texturesOf(t, prof)["SKIN"]; got != "https://shaurma.example/tex/n.png" {
		t.Fatalf("скін не з Shaurma: %q", got)
	}
	if fx.mojangHits.Load() != 0 {
		t.Fatal("Mojang не мав питатись, коли Shaurma дала скін")
	}
}

// Невідомий і Shaurma, і Mojang → 204 (гра покаже стандартний скін за UUID); відповідь кешується.
func TestSkinProxy_RealUUIDUnknownEverywhere(t *testing.T) {
	fx := newProxyFixture(t, nil, nil)
	for i := 0; i < 3; i++ {
		if code, _ := getProfile(t, fx.p, "11111111111141118111111111111111"); code != http.StatusNoContent {
			t.Fatalf("очікував 204, код %d", code)
		}
	}
	if n := fx.mojangHits.Load(); n != 1 {
		t.Fatalf("негативна відповідь має кешуватись: звернень до Mojang %d, очікував 1", n)
	}
}

func TestIsOfflineUUIDShape(t *testing.T) {
	if !isOfflineUUIDShape(offlineUUIDHex("Notch")) {
		t.Fatal("офлайн-UUID не розпізнано")
	}
	if isOfflineUUIDShape("069a79f444e94726a5befca90e38aaf5") {
		t.Fatal("справжній Mojang-UUID (v4) прийнято за офлайн")
	}
}

// Пакетний пошук: піратів віддає Shaurma, решту (ліцензійних) — Mojang; Mojang питаємо лише про те, чого не знає Shaurma.
func TestSkinProxy_BatchMergesShaurmaAndMojang(t *testing.T) {
	sh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"` + offlineUUIDHex("Pirate") + `","name":"Pirate"}]`))
	}))
	t.Cleanup(sh.Close)
	fx := newProxyFixture(t, nil, nil)
	fx.p.upstream = sh.URL + yggdrasilAPIPath
	fx.p.mojangBulk = fx.mojangURL + "/bulk"
	fx.bulkKnown["Notch"] = "069a79f444e94726a5befca90e38aaf5"

	req := httptest.NewRequest("POST", "http://127.0.0.1:1/api/profiles/minecraft", strings.NewReader(`["Pirate","Notch","Nobody"]`))
	rec := httptest.NewRecorder()
	fx.p.handleBatch(rec, req)
	if rec.Code != 200 {
		t.Fatalf("код %d", rec.Code)
	}
	var got []nameRef
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, g := range got {
		ids[g.Name] = g.ID
	}
	if ids["Pirate"] != offlineUUIDHex("Pirate") || ids["Notch"] != "069a79f444e94726a5befca90e38aaf5" || len(ids) != 2 {
		t.Fatalf("очікував Pirate (Shaurma) + Notch (Mojang), отримав %v", ids)
	}
	if strings.Join(fx.bulkAsked, ",") != "Notch,Nobody" {
		t.Fatalf("Mojang мав питатись лише про невідомих Shaurma: %v", fx.bulkAsked)
	}
}

// Shaurma впала → пакетний пошук усе одно працює через Mojang (ванільна поведінка не ламається).
func TestSkinProxy_BatchSurvivesShaurmaOutage(t *testing.T) {
	sh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	t.Cleanup(sh.Close)
	fx := newProxyFixture(t, nil, nil)
	fx.p.upstream = sh.URL + yggdrasilAPIPath
	fx.p.mojangBulk = fx.mojangURL + "/bulk"
	fx.bulkKnown["Notch"] = "069a79f444e94726a5befca90e38aaf5"

	rec := httptest.NewRecorder()
	fx.p.handleBatch(rec, httptest.NewRequest("POST", "http://127.0.0.1:1/api/profiles/minecraft", strings.NewReader(`["Notch"]`)))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Notch") {
		t.Fatalf("код %d, тіло %s", rec.Code, rec.Body.String())
	}
}

// Плащі Шаурми: сервер каже, яке джерело плаща обрав гравець (властивість shaurma:cape).
func TestSkinProxy_ShaurmaCapeModes(t *testing.T) {
	const licensed = "0f1e2d3c4b5a49788796a5b4c3d2e1f0" // v4
	const mojangCape = "https://textures.minecraft.net/texture/mojangcape"
	const shaurmaCape = "https://vps.example/api/yggdrasil/texture/shaurmacape.png"
	const shaurmaSkin = "https://vps.example/api/yggdrasil/texture/skin.png"
	cases := []struct {
		mode     string
		shTex    map[string]string
		wantCape string
	}{
		{"mojang", map[string]string{"SKIN": shaurmaSkin}, mojangCape},
		{"mojang", map[string]string{"SKIN": shaurmaSkin, "CAPE": shaurmaCape}, mojangCape},
		{"shaurma", map[string]string{"SKIN": shaurmaSkin, "CAPE": shaurmaCape}, shaurmaCape},
		{"shaurma", map[string]string{"SKIN": shaurmaSkin}, ""},
	}
	for _, c := range cases {
		fx := newProxyFixture(t, map[string]ygProfile{licensed: {ID: licensed, Name: "Lic", Properties: []ygProp{
			{Name: "textures", Value: texturesValueFor(t, c.shTex)},
			{Name: "shaurma:cape", Value: c.mode},
		}}}, nil)
		fx.realNames[licensed] = "Lic"
		fx.realSkins[licensed] = "https://textures.minecraft.net/texture/mojangskin"
		fx.realCapes[licensed] = mojangCape
		code, prof := getProfile(t, fx.p, licensed)
		if code != http.StatusOK {
			t.Fatalf("%s: status %d", c.mode, code)
		}
		tex := texturesOf(t, prof)
		if tex["CAPE"] != c.wantCape {
			t.Errorf("mode %s (%v): CAPE=%q want %q", c.mode, c.shTex, tex["CAPE"], c.wantCape)
		}
		if tex["SKIN"] != shaurmaSkin {
			t.Errorf("mode %s: SKIN=%q", c.mode, tex["SKIN"])
		}
		for _, pr := range prof.Properties {
			if pr.Name == "shaurma:cape" {
				t.Error("service property leaked to the game")
			}
		}
		verifySig(t, fx.p, prof)
	}
}

// Mojang-режим, а в Mojang плаща немає — у грі без плаща (плащ Шаурми не підставляється).
func TestSkinProxy_MojangModeWithoutMojangCape(t *testing.T) {
	const licensed = "0f1e2d3c4b5a49788796a5b4c3d2e1f0"
	fx := newProxyFixture(t, map[string]ygProfile{licensed: {ID: licensed, Name: "Lic", Properties: []ygProp{
		{Name: "textures", Value: texturesValueFor(t, map[string]string{"SKIN": "https://vps.example/s.png", "CAPE": "https://vps.example/c.png"})},
		{Name: "shaurma:cape", Value: "mojang"},
	}}}, nil)
	fx.realNames[licensed] = "Lic"
	fx.realSkins[licensed] = "https://textures.minecraft.net/texture/mojangskin"
	_, prof := getProfile(t, fx.p, licensed)
	if c := texturesOf(t, prof)["CAPE"]; c != "" {
		t.Fatalf("плащ має бути відсутній, а є %q", c)
	}
}

// Вузол Шаурми на мить недоступний (перезапуск) — інжектор повторює запит, а якщо сервер
// так і не відповів, бере його останню відповідь, а не вигляд Mojang.
func TestSkinProxy_UpstreamHiccupKeepsShaurmaCape(t *testing.T) {
	const id = "0f1e2d3c4b5a49788796a5b4c3d2e1f0"
	const cape = "https://vps.example/api/yggdrasil/texture/cape.png"
	var mode atomic.Int32 // 0 — 503 один раз, потім 200; 2 — завжди 503
	var hits atomic.Int32
	prof := ygProfile{ID: id, Name: "Lic", Properties: []ygProp{
		{Name: "textures", Value: texturesValueFor(t, map[string]string{"SKIN": "https://vps.example/s.png", "CAPE": cape})},
		{Name: "shaurma:cape", Value: "shaurma"},
	}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		if mode.Load() == 2 || n == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(prof)
	}))
	defer srv.Close()
	fx := newProxyFixture(t, nil, nil)
	fx.p.configure(srv.URL, nil)
	if _, got := getProfile(t, fx.p, id); texturesOf(t, got)["CAPE"] != cape {
		t.Fatalf("after retry: %v", texturesOf(t, got))
	}
	mode.Store(2)
	if _, got := getProfile(t, fx.p, id); texturesOf(t, got)["CAPE"] != cape {
		t.Fatalf("server down: expected last Shaurma answer, got %v", texturesOf(t, got))
	}
}
