//go:build shaurma

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
}

// newProxyFixture: підроблені Shaurma (profiles: uuid → профіль) і Mojang
// (mojangSkins: нік → URL скіна; відсутній нік = немає ліцензії).
func newProxyFixture(t *testing.T, shaurma map[string]ygProfile, mojangSkins map[string]string) *proxyFixture {
	t.Helper()
	fx := &proxyFixture{}

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
			var u struct{ URL string `json:"url"` }
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
