package avatar

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	sessionURL    = "https://sessionserver.mojang.com/session/minecraft/profile/%s"
	nameLookupURL = "https://api.mojang.com/users/profiles/minecraft/%s"
	cacheTTL      = 1 * time.Hour
	nameTTL       = 30 * time.Minute

	// MHF_Steve — стабільний ванільний акаунт Mojang зі скіном за замовчуванням.
	mhfSteve = "c06f89064c8a49119c29ea1dbd1aab82"
)

var errNoSkin = errors.New("sessionserver: no skin texture")

type Service struct {
	cacheDir string
	client   *http.Client

	mu        sync.Mutex
	nameCache map[string]nameEntry
}

type nameEntry struct {
	uuid string
	at   time.Time
}

type metaInfo struct {
	TextureURL string    `json:"textureUrl"`
	FetchedAt  time.Time `json:"fetchedAt"`
}

func NewService(cacheDir string) *Service {
	return &Service{
		cacheDir:  cacheDir,
		client:    &http.Client{Timeout: 15 * time.Second},
		nameCache: map[string]nameEntry{},
	}
}

// Head повертає data URL 2D-голови (64x64 PNG) для акаунта.
//
// Для ліцензійних акаунтів uuid надійний — шкіра береться напряму за ним.
// Для офлайн/піратських нік пробивається на Mojang (як PrismLauncher):
//   - нік належить реальному гравцю — показуємо його реальний скін (Алекс тощо);
//   - гравця немає — дефолтна голова Стіва/Алекса за парністю UUID.
//
// Будь-яка мережева помилка повертає "" (фронтенд показує запасну голову).
func (s *Service) Head(uuid, username string, licensed bool) string {
	if s.cacheDir == "" {
		return ""
	}
	os.MkdirAll(s.cacheDir, 0o755)

	if !licensed || uuid == "" {
		if real := s.resolveName(username); real != "" {
			uuid = real
		} else {
			return s.defaultHead(uuid)
		}
	}

	pngPath := filepath.Join(s.cacheDir, uuid+".png")
	metaPath := filepath.Join(s.cacheDir, uuid+".json")

	meta, _ := s.readMeta(metaPath)

	// Свіжий кеш — віддаємо без мережі.
	if meta != nil && time.Since(meta.FetchedAt) < cacheTTL {
		if data, err := os.ReadFile(pngPath); err == nil {
			return dataURL(data)
		}
	}

	texURL, err := s.fetchTextureURL(uuid)
	if err != nil {
		if errors.Is(err, errNoSkin) {
			return s.defaultHead(uuid)
		}
		if data, e := os.ReadFile(pngPath); e == nil {
			return dataURL(data)
		}
		return ""
	}

	// Скін не змінився — оновлюємо час і віддаємо кеш.
	if meta != nil && meta.TextureURL == texURL {
		s.writeMeta(metaPath, metaInfo{TextureURL: texURL, FetchedAt: time.Now()})
		if data, e := os.ReadFile(pngPath); e == nil {
			return dataURL(data)
		}
	}

	// Скін змінився або кешу немає — рендеримо заново.
	head, err := s.renderHead(texURL)
	if err != nil {
		if data, e := os.ReadFile(pngPath); e == nil {
			return dataURL(data)
		}
		return ""
	}
	os.WriteFile(pngPath, head, 0o644)
	s.writeMeta(metaPath, metaInfo{TextureURL: texURL, FetchedAt: time.Now()})
	return dataURL(head)
}

// resolveName пробує знайти реальний UUID за ніком на Mojang.
// Порожній рядок — гравця з таким ніком не існує (або мережева помилка).
func (s *Service) resolveName(username string) string {
	if username == "" {
		return ""
	}

	s.mu.Lock()
	if e, ok := s.nameCache[username]; ok && time.Since(e.at) < nameTTL {
		s.mu.Unlock()
		return e.uuid
	}
	s.mu.Unlock()

	resp, err := s.client.Get(fmt.Sprintf(nameLookupURL, url.PathEscape(username)))
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// 204 — нік не існує; кешуємо порожній результат, щоб не бити API.
		if resp.StatusCode == http.StatusNoContent {
			s.mu.Lock()
			s.nameCache[username] = nameEntry{uuid: "", at: time.Now()}
			s.mu.Unlock()
		}
		return ""
	}

	var r struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return ""
	}
	real := formatUUID(r.ID)
	s.mu.Lock()
	s.nameCache[username] = nameEntry{uuid: real, at: time.Now()}
	s.mu.Unlock()
	return real
}

// defaultHead рендерить дефолтну голову з реального скіна Mojang (MHF_Steve).
// Дефолтні Стів та Алекс мають ідентичні голови (відрізняються лише руки), тож
// один рендер покриває обидві моделі; вибір Стів/Алекс — лише за парністю UUID,
// як це робить Minecraft для офлайн-профілів.
func (s *Service) defaultHead(uuid string) string {
	key := "_default_steve"
	if s.isAlexModel(uuid) {
		key = "_default_alex"
	}
	pngPath := filepath.Join(s.cacheDir, key+".png")
	metaPath := filepath.Join(s.cacheDir, key+".json")

	meta, _ := s.readMeta(metaPath)
	if meta != nil && time.Since(meta.FetchedAt) < cacheTTL {
		if data, err := os.ReadFile(pngPath); err == nil {
			return dataURL(data)
		}
	}

	texURL, err := s.fetchTextureURL(mhfSteve)
	if err != nil {
		return ""
	}
	head, err := s.renderHead(texURL)
	if err != nil {
		return ""
	}
	os.WriteFile(pngPath, head, 0o644)
	s.writeMeta(metaPath, metaInfo{TextureURL: texURL, FetchedAt: time.Now()})
	return dataURL(head)
}

// Invalidate видаляє закешовану голову акаунта (PNG + метадані), щоб
// наступний виклик Head() перерендерив її з поточного скіна на Mojang.
// Викликається після зміни скіна через гардероб — інакше аватарка
// показувала б старий скін до закінчення TTL-кешу (1 година).
func (s *Service) Invalidate(uuid string) {
	if uuid == "" || s.cacheDir == "" {
		return
	}
	os.Remove(filepath.Join(s.cacheDir, uuid+".png"))
	os.Remove(filepath.Join(s.cacheDir, uuid+".json"))
}

// isAlexModel — наближення Java String.hashCode для UUID: непарний хеш → slim (Алекс).
func (s *Service) isAlexModel(uuid string) bool {
	if uuid == "" {
		return false
	}
	h := 0
	for _, c := range uuid {
		h = (31*h + int(c)) & 0x7fffffff
	}
	return h&1 == 1
}

func formatUUID(id string) string {
	if len(id) != 32 {
		return id
	}
	return id[0:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:32]
}

func dataURL(data []byte) string {
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)
}

func (s *Service) readMeta(path string) (*metaInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m metaInfo
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Service) writeMeta(path string, m metaInfo) {
	data, _ := json.Marshal(m)
	os.WriteFile(path, data, 0o644)
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
			URL string `json:"url"`
		} `json:"SKIN"`
	} `json:"textures"`
}

func (s *Service) fetchTextureURL(uuid string) (string, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf(sessionURL, uuid), nil)
	if err != nil {
		return "", err
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", errors.New("sessionserver: " + resp.Status)
	}

	var sr sessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return "", err
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
		if tp.Textures.SKIN.URL != "" {
			return tp.Textures.SKIN.URL, nil
		}
	}
	return "", errNoSkin
}

func (s *Service) renderHead(texURL string) ([]byte, error) {
	req, err := http.NewRequest("GET", texURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("skin download: " + resp.Status)
	}
	skin, _, err := image.Decode(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}

	b := skin.Bounds()
	// Обличчя (8,8) та накладка капелюха (40,8) в 64x64 (і 64x32) скіні.
	face := crop(skin, b.Min.X+8, b.Min.Y+8, 8, 8)
	hat := crop(skin, b.Min.X+40, b.Min.Y+8, 8, 8)

	out := image.NewRGBA(image.Rect(0, 0, 64, 64))
	scale(face, out, false)
	scale(hat, out, true)

	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func crop(img image.Image, x, y, w, h int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), img, image.Point{x, y}, draw.Src)
	return dst
}

// scale копіює 8x8 регіон у 64x64 (nearest neighbor). overlay=true —
// прозорі пікселі джерела не копіюються (капелюх поверх обличчя).
func scale(src *image.RGBA, dst *image.RGBA, overlay bool) {
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			c := src.RGBAAt(x, y)
			if overlay && c.A == 0 {
				continue
			}
			c = color.RGBA{R: c.R, G: c.G, B: c.B, A: 255}
			for dy := 0; dy < 8; dy++ {
				for dx := 0; dx < 8; dx++ {
					dst.SetRGBA(x*8+dx, y*8+dy, c)
				}
			}
		}
	}
}
