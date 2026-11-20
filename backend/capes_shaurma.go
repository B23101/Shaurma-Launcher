package main

// Плащі Шаурми на боці лаунчера. Лаунчер нічого не вирішує сам: сервер знає, які плащі є в
// акаунта, і сам віддає обраний плащ у Yggdrasil-профіль. Тут — лише запити від імені
// Шаурма-акаунта (JWT + HWID) і превʼю текстур.
//
// Ліцензійний гравець обирає джерело плаща: mojang (як і раніше — те, що вибрано на Mojang; на
// Mojang нічого не змінюється) або shaurma (плащ із каталогу Шаурми чи «без плаща»). Інжектор
// лаунчера (skin_proxy.go) показує в грі саме обране джерело.

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/model"
)

// ShaurmaCape — плащ із каталогу, яким володіє акаунт.
type ShaurmaCape struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Kind    string `json:"kind"` // default | code
	Texture string `json:"texture"`
}

// ShaurmaCapes — стан плащів акаунта.
type ShaurmaCapes struct {
	Licensed bool          `json:"licensed"`
	Source   string        `json:"source"` // mojang | shaurma
	Active   string        `json:"active"` // id плаща або "" (без плаща Шаурми)
	Capes    []ShaurmaCape `json:"capes"`
}

// CapeRedeemResult — результат активації коду.
type CapeRedeemResult struct {
	Result        string       `json:"result"` // granted | invalid | already_owned | already_redeemed | too_many_attempts | bad_format
	RetryAfterSec int          `json:"retryAfterSec,omitempty"`
	Cape          *ShaurmaCape `json:"cape,omitempty"`
}

var capeHashRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

type capeAPIError struct {
	OK            bool   `json:"ok"`
	Error         string `json:"error"`
	RetryAfterSec int    `json:"retryAfterSec"`
}

// capesLauncherID — акаунт Шаурми, від імені якого йдуть запити плащів: комірка САМЕ цього
// акаунта лаунчера (accountID); порожній accountID — активний акаунт.
func (a *App) capesLauncherID(accountID string) (string, error) {
	if accountID != "" {
		if _, ok := a.shaurmaCell(accountID); ok {
			return accountID, nil
		}
		if acc := a.accountByID(accountID); acc.ID != "" && acc.ID != a.cfg.ActiveAccount().ID {
			return "", errors.New("у цього акаунта немає акаунта Шаурми — плащі Шаурми недоступні")
		}
	}
	return a.activeShaurmaID()
}

func (a *App) capesCall(method, path string, body any, out any) (int, capeAPIError, error) {
	return a.capesCallFor("", method, path, body, out)
}

func (a *App) capesCallFor(accountID, method, path string, body any, out any) (int, capeAPIError, error) {
	id, err := a.capesLauncherID(accountID)
	if err != nil {
		return 0, capeAPIError{}, err
	}
	var raw []byte
	if body != nil {
		if raw, err = json.Marshal(body); err != nil {
			return 0, capeAPIError{}, err
		}
	}
	resp, err := a.shaurmaRequest(id, method, path, raw)
	if err != nil {
		return 0, capeAPIError{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, capeAPIError{}, err
	}
	if resp.StatusCode == http.StatusOK {
		return resp.StatusCode, capeAPIError{OK: true}, json.Unmarshal(data, out)
	}
	var e capeAPIError
	_ = json.Unmarshal(data, &e)
	if e.Error == "" {
		e.Error = fmt.Sprintf("http_%d", resp.StatusCode)
	}
	return resp.StatusCode, e, nil
}

func capeErr(e capeAPIError) error { return errors.New("capes:" + e.Error) }

// GetShaurmaCapes — плащі Шаурми активного акаунта (з превʼю) і поточний вибір.
func (a *App) GetShaurmaCapes() (ShaurmaCapes, error) { return a.GetShaurmaCapesFor("") }

// GetShaurmaCapesFor — плащі Шаурми конкретного акаунта лаунчера.
func (a *App) GetShaurmaCapesFor(accountID string) (ShaurmaCapes, error) {
	var in struct {
		Licensed bool   `json:"licensed"`
		Source   string `json:"source"`
		Active   string `json:"active"`
		Capes    []struct {
			ID          string `json:"id"`
			Title       string `json:"title"`
			Kind        string `json:"kind"`
			TextureHash string `json:"textureHash"`
		} `json:"capes"`
	}
	_, e, err := a.capesCallFor(accountID, http.MethodGet, "/api/capes", nil, &in)
	if err != nil {
		return ShaurmaCapes{}, err
	}
	if !e.OK {
		return ShaurmaCapes{}, capeErr(e)
	}
	out := ShaurmaCapes{Licensed: in.Licensed, Source: in.Source, Active: in.Active, Capes: []ShaurmaCape{}}
	var wg sync.WaitGroup
	out.Capes = make([]ShaurmaCape, len(in.Capes))
	for i, c := range in.Capes {
		out.Capes[i] = ShaurmaCape{ID: c.ID, Title: c.Title, Kind: c.Kind}
		wg.Add(1)
		go func(i int, hash string) {
			defer wg.Done()
			out.Capes[i].Texture = a.capeTextureDataURL(hash)
		}(i, c.TextureHash)
	}
	wg.Wait()
	return out, nil
}

// SelectShaurmaCape — що показувати в грі: source mojang|shaurma, capeID "" = без плаща Шаурми.
func (a *App) SelectShaurmaCape(source, capeID string) error {
	return a.SelectShaurmaCapeFor("", source, capeID)
}

// SelectShaurmaCapeFor — вибір плаща (mojang|shaurma) для конкретного акаунта лаунчера.
func (a *App) SelectShaurmaCapeFor(accountID, source, capeID string) error {
	var out map[string]any
	_, e, err := a.capesCallFor(accountID, http.MethodPost, "/api/capes/select", map[string]string{"source": source, "capeId": capeID}, &out)
	if err != nil {
		return err
	}
	if !e.OK {
		return capeErr(e)
	}
	invalidateGameCapes()
	a.emit("capes:changed", nil)
	return nil
}

// RedeemShaurmaCape — активувати ключ або промокод (активний акаунт).
func (a *App) RedeemShaurmaCape(code string) (CapeRedeemResult, error) { return a.RedeemShaurmaCapeFor("", code) }

// RedeemShaurmaCapeFor — активувати код на конкретному акаунті лаунчера.
func (a *App) RedeemShaurmaCapeFor(accountID, code string) (CapeRedeemResult, error) {
	code = strings.TrimSpace(code)
	if code == "" || len(code) > 80 {
		return CapeRedeemResult{Result: "bad_format"}, nil
	}
	var in struct {
		Result string `json:"result"`
		Cape   struct {
			ID          string `json:"id"`
			Title       string `json:"title"`
			Kind        string `json:"kind"`
			TextureHash string `json:"textureHash"`
		} `json:"cape"`
	}
	_, e, err := a.capesCallFor(accountID, http.MethodPost, "/api/capes/redeem", map[string]string{"code": code}, &in)
	if err != nil {
		return CapeRedeemResult{}, err
	}
	if !e.OK {
		switch e.Error {
		case "invalid", "already_owned", "already_redeemed", "too_many_attempts", "bad_format":
			return CapeRedeemResult{Result: e.Error, RetryAfterSec: e.RetryAfterSec}, nil
		}
		return CapeRedeemResult{}, capeErr(e)
	}
	c := &ShaurmaCape{ID: in.Cape.ID, Title: in.Cape.Title, Kind: in.Cape.Kind, Texture: a.capeTextureDataURL(in.Cape.TextureHash)}
	a.emit("capes:changed", nil)
	return CapeRedeemResult{Result: "granted", Cape: c}, nil
}

// capeTextureDataURL — PNG плаща як data URL. Кеш на диску за sha256; вміст звіряється з хешем,
// тож підмінений файл у кеші чи у відповіді сервера просто не покажеться.
func (a *App) capeTextureDataURL(hash string) string {
	if !capeHashRE.MatchString(hash) {
		return ""
	}
	dir := filepath.Join(a.cfg.CacheDir(), "capes")
	p := filepath.Join(dir, hash+".png")
	ok := func(b []byte) bool {
		sum := sha256.Sum256(b)
		return hex.EncodeToString(sum[:]) == hash
	}
	if b, err := os.ReadFile(p); err == nil && ok(b) {
		return "data:image/png;base64," + base64.StdEncoding.EncodeToString(b)
	}
	resp, err := a.skinRequest(http.MethodGet, "/api/yggdrasil/texture/"+hash+".png", nil)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 600<<10))
	if err != nil || !ok(b) {
		return ""
	}
	if os.MkdirAll(dir, 0o755) == nil {
		_ = os.WriteFile(p, b, 0o644)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(b)
}

// SetWardrobePresetCape — система плаща пресету: mojang (плащ пресету з Mojang, як було) або
// shaurma (capeID із каталогу Шаурми; "" — без плаща).
func (a *App) SetWardrobePresetCape(accountID, presetID, system, shaurmaCapeID string) (model.SkinPreset, error) {
	if system != "mojang" && system != "shaurma" {
		return model.SkinPreset{}, errors.New("невідома система плаща")
	}
	acc, err := a.refreshAccountForWardrobe(accountID)
	if err != nil {
		return model.SkinPreset{}, err
	}
	return a.wardrobe.SetPresetCapeSystem(acc.Username, presetID, system, shaurmaCapeID)
}

// applyPresetCape застосовує вибір плаща пресету (mojang|shaurma) на акаунт — на сервері Шаурми,
// який і віддає плащ грі (інжектор) та всім моделям гравця. Старий пресет без вибору нічого не міняє.
func (a *App) applyPresetCape(accountID string, p model.SkinPreset) error {
	if p.CapeSystem == "" {
		return nil
	}
	return a.SelectShaurmaCapeFor(accountID, p.CapeSystem, p.ShaurmaCapeID)
}

// ── Плащ так, як його бачить гра ─────────────────────────────────────────
// Усі моделі гравця в лаунчері (профіль, акаунт, друзі) беруть плащ із ТОГО САМОГО
// Yggdrasil-профілю вузла, який отримує гра через інжектор: shaurma — плащ Шаурми (або
// без плаща), mojang — плащ Mojang. Нічого локально не вирішуємо — лише читаємо сервер.

var capeTextureURLRE = regexp.MustCompile(`/texture/([0-9a-f]{64})\.png$`)

type gameCape struct {
	mode string // shaurma | mojang | "" (сервер не віддає плащів Шаурми)
	data string // data URL плаща Шаурми (лише для shaurma; "" — без плаща)
	at   time.Time
}

var (
	gameCapeMu    sync.Mutex
	gameCapeCache = map[string]gameCape{}
)

// gameProfileCape читає профіль гравця з вузла (як гра) і повертає режим і плащ Шаурми.
func (a *App) gameProfileCape(uuidHex string) (gameCape, bool) {
	uuidHex = normalizeUUIDHex(uuidHex)
	if uuidHex == "" {
		return gameCape{}, false
	}
	gameCapeMu.Lock()
	if c, ok := gameCapeCache[uuidHex]; ok && time.Since(c.at) < 20*time.Second {
		gameCapeMu.Unlock()
		return c, true
	}
	gameCapeMu.Unlock()
	resp, err := a.skinRequest(http.MethodGet, "/api/yggdrasil/sessionserver/session/minecraft/profile/"+uuidHex, nil)
	if err != nil {
		return gameCape{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return gameCape{}, false
	}
	var prof ygProfile
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&prof) != nil {
		return gameCape{}, false
	}
	c := gameCape{at: time.Now()}
	capeHash := ""
	for _, pr := range prof.Properties {
		switch pr.Name {
		case "shaurma:cape":
			c.mode = pr.Value
		case "textures":
			if d := decodeTextures(pr.Value); d != nil {
				var cape struct {
					URL string `json:"url"`
				}
				if raw, ok := d.Textures["CAPE"]; ok && json.Unmarshal(raw, &cape) == nil {
					if m := capeTextureURLRE.FindStringSubmatch(cape.URL); m != nil {
						capeHash = m[1]
					}
				}
			}
		}
	}
	if c.mode == "none" {
		c.mode = "shaurma"
	}
	if c.mode == "shaurma" && capeHash != "" {
		c.data = a.capeTextureDataURL(capeHash)
	}
	gameCapeMu.Lock()
	gameCapeCache[uuidHex] = c
	gameCapeMu.Unlock()
	return c, true
}

// invalidateGameCapes — після зміни вибору плаща моделі мають перечитати сервер одразу.
func invalidateGameCapes() {
	gameCapeMu.Lock()
	gameCapeCache = map[string]gameCape{}
	gameCapeMu.Unlock()
}

// applyGameCape підставляє в вигляд плащ, який бачить гра. mojangCape — як отримати плащ Mojang,
// якщо гравець лишив плащ Mojang (nil — лишити те, що вже є у вигляді).
func (a *App) applyGameCape(look *model.ActiveLook, uuidHex string, mojangCape func() string) {
	if look == nil {
		return
	}
	c, ok := a.gameProfileCape(uuidHex)
	if !ok {
		return
	}
	switch c.mode {
	case "shaurma":
		look.CapeDataURL = c.data
	case "mojang":
		if mojangCape != nil {
			look.CapeDataURL = mojangCape()
		}
	}
}

// accountGameUUID — UUID, під яким акаунт з'являється в грі (офлайн — UUIDv3 від ніка).
func accountGameUUID(acc model.Account) string {
	if u := normalizeUUIDHex(acc.UUID); u != "" && (acc.IsLicensed || acc.Type == "microsoft") {
		return u
	}
	if acc.Username == "" {
		return ""
	}
	return offlineUUIDHex(acc.Username)
}
