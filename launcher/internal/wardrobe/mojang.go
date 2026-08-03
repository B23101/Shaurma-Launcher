package wardrobe

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"time"

	"shaurma-launcher-wails/internal/model"
)

const (
	nameLookupURL  = "https://api.mojang.com/users/profiles/minecraft/%s"
	sessionURL     = "https://sessionserver.mojang.com/session/minecraft/profile/%s"
	uploadSkinURL  = "https://api.minecraftservices.com/minecraft/profile/skins"
	activeCapeURL  = "https://api.minecraftservices.com/minecraft/profile/capes/active"
	profileURL     = "https://api.minecraftservices.com/minecraft/profile"
	// Mojang CDN (textures.minecraft.net) віддає 403 без браузерного
	// User-Agent (Go-http-client/1.1 не приймає), тож слаємо звичайний.
	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0 Safari/537.36"
)

// uaRoundTripper підставляє браузерний User-Agent у всі запити Mojang.
type uaRoundTripper struct{ inner http.RoundTripper }

func (t *uaRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", userAgent)
	}
	return t.inner.RoundTrip(req)
}

var (
	// ErrPlayerNotFound — гравця з таким ніком не існує (Mojang 204/404).
	ErrPlayerNotFound = errors.New("wardrobe: player not found")
	// ErrNoOwnSkin — гравець існує, але sessionserver не повернув текстуру.
	ErrNoOwnSkin = errors.New("wardrobe: player has no custom skin")
)

type mojangClient struct {
	http *http.Client
}

func newMojangClient() *mojangClient {
	return &mojangClient{http: &http.Client{
		Timeout:   12 * time.Second,
		Transport: &uaRoundTripper{inner: http.DefaultTransport},
	}}
}

// LookupByNickname реалізує фічу "Скопіювати скін гравця":
//  1. api.mojang.com/users/profiles/minecraft/{nick} → UUID
//  2. sessionserver.mojang.com/session/minecraft/profile/{uuid} → властивість
//     "textures" (Base64 JSON) → SKIN.url (+ metadata.model) і CAPE.url.
func (c *mojangClient) LookupByNickname(nick string) (*model.PlayerProfile, error) {
	uuid, realNick, err := c.resolveUUID(nick)
	if err != nil {
		return nil, err
	}

	texURL, capeURL, slim, err := c.fetchTextures(uuid)
	if err != nil {
		return nil, err
	}
	if texURL == "" {
		return nil, ErrNoOwnSkin
	}

	return &model.PlayerProfile{
		Nickname: realNick,
		UUID:     uuid,
		SlimArms: slim,
		SkinURL:  texURL,
		CapeURL:  capeURL,
	}, nil
}

func (c *mojangClient) resolveUUID(nick string) (uuid, realNick string, err error) {
	req, err := http.NewRequest("GET", fmt.Sprintf(nameLookupURL, url.PathEscape(nick)), nil)
	if err != nil {
		return "", "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("mojang: network error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound {
		return "", "", ErrPlayerNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("mojang: unexpected status %s", resp.Status)
	}

	var r struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", "", err
	}
	if r.ID == "" {
		return "", "", ErrPlayerNotFound
	}
	return formatUUID(r.ID), r.Name, nil
}

type sessionResponse struct {
	Properties []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"properties"`
}

type texturePayload struct {
	Textures struct {
		SKIN struct {
			URL      string `json:"url"`
			Metadata struct {
				Model string `json:"model"` // "" = classic, "slim" = slim
			} `json:"metadata"`
		} `json:"SKIN"`
		CAPE struct {
			URL string `json:"url"`
		} `json:"CAPE"`
	} `json:"textures"`
}

func (c *mojangClient) fetchTextures(uuid string) (skinURL, capeURL string, slim bool, err error) {
	req, err := http.NewRequest("GET", fmt.Sprintf(sessionURL, uuid), nil)
	if err != nil {
		return "", "", false, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", "", false, fmt.Errorf("mojang: network error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", false, fmt.Errorf("mojang: sessionserver %s", resp.Status)
	}

	var sr sessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return "", "", false, err
	}
	for _, p := range sr.Properties {
		if p.Name != "textures" {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(p.Value)
		if err != nil {
			continue
		}
		var tp texturePayload
		if err := json.Unmarshal(raw, &tp); err != nil {
			continue
		}
		return tp.Textures.SKIN.URL, tp.Textures.CAPE.URL, tp.Textures.SKIN.Metadata.Model == "slim", nil
	}
	return "", "", false, nil
}

// DownloadTexture завантажує байти PNG (скін або плащ) за URL з Mojang CDN.
func (c *mojangClient) DownloadTexture(texURL string) ([]byte, error) {
	req, err := http.NewRequest("GET", texURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mojang: texture download %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

// UploadSkin застосовує PNG-скін на ліцензійний акаунт гравця
// (api.minecraftservices.com/minecraft/profile/skins, POST multipart,
// потребує дійсний accessToken акаунта Minecraft — не MS access token).
func (c *mojangClient) UploadSkin(accessToken string, skinPNG []byte, slim bool) error {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	variant := "CLASSIC"
	if slim {
		variant = "SLIM"
	}
	if err := w.WriteField("variant", variant); err != nil {
		return err
	}
	fw, err := w.CreateFormFile("file", "skin.png")
	if err != nil {
		return err
	}
	if _, err := fw.Write(skinPNG); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}

	req, err := http.NewRequest("POST", uploadSkinURL, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("mojang: upload skin failed %s: %s", resp.Status, string(body))
	}
	return nil
}

// FetchOwnActiveTextures повертає URL активного скіна і активного плаща
// власного акаунта (profile-ендпоінт, поля skins[]/capes[] зі state=ACTIVE),
// а також тип моделі скіна (classic/slim) з поля variant.
// Використовується, щоб підсвітити у гардеробі пресет, який вже фактично
// застосований на акаунт (порівнянням хешів вмісту, а не URL), і щоб
// показати поточний вигляд акаунта, коли жоден пресет не збігається.
func (c *mojangClient) FetchOwnActiveTextures(accessToken string) (skinURL, capeURL string, slim bool, err error) {
	req, err := http.NewRequest("GET", profileURL, nil)
	if err != nil {
		return "", "", false, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", "", false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", false, fmt.Errorf("mojang: profile fetch %s", resp.Status)
	}

	var pr struct {
		Skins []struct {
			State   string `json:"state"`
			URL     string `json:"url"`
			Variant string `json:"variant"` // "CLASSIC" | "SLIM"
		} `json:"skins"`
		Capes []struct {
			State string `json:"state"`
			URL   string `json:"url"`
		} `json:"capes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return "", "", false, err
	}
	for _, s := range pr.Skins {
		if s.State == "ACTIVE" {
			skinURL = s.URL
			slim = s.Variant == "SLIM"
			break
		}
	}
	for _, cp := range pr.Capes {
		if cp.State == "ACTIVE" {
			capeURL = cp.URL
			break
		}
	}
	return skinURL, capeURL, slim, nil
}

// FetchOwnedCapes повертає плащі акаунта (з profile-ендпоінта, поле capes[]).
func (c *mojangClient) FetchOwnedCapes(accessToken string) ([]model.CapeInfo, error) {
	req, err := http.NewRequest("GET", profileURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("mojang: profile fetch %s", resp.Status)
	}

	var pr struct {
		Capes []struct {
			ID    string `json:"id"`
			State string `json:"state"` // "ACTIVE" | "INACTIVE"
			URL   string `json:"url"`
			Alias string `json:"alias"`
		} `json:"capes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return nil, err
	}

	out := make([]model.CapeInfo, 0, len(pr.Capes))
	for _, c := range pr.Capes {
		out = append(out, model.CapeInfo{
			ID:     c.ID,
			Alias:  c.Alias,
			URL:    c.URL,
			Active: c.State == "ACTIVE",
		})
	}
	return out, nil
}

// SetActiveCape вмикає плащ за ID (PUT) або знімає його (DELETE, capeID="").
func (c *mojangClient) SetActiveCape(accessToken, capeID string) error {
	var req *http.Request
	var err error

	if capeID == "" {
		req, err = http.NewRequest("DELETE", activeCapeURL, nil)
	} else {
		body, _ := json.Marshal(map[string]string{"capeId": capeID})
		req, err = http.NewRequest("PUT", activeCapeURL, bytes.NewReader(body))
		if req != nil {
			req.Header.Set("Content-Type", "application/json")
		}
	}
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("mojang: set cape failed %s: %s", resp.Status, string(body))
	}
	return nil
}

func formatUUID(id string) string {
	if len(id) != 32 {
		return id
	}
	return id[0:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:32]
}
