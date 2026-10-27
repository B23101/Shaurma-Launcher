// Пакет xbox реалізує клієнти соціальних Xbox Live API для розділу
// «Друзі» лаунчера: список друзів (People Hub), присутність (presence) і
// налаштування приватності. Авторизація — XBL3.0 токеном, який лаунчер
// отримує під час входу Microsoft (див. auth.Authenticator.authenticateSocialXSTS):
//
//	Authorization: XBL3.0 x=<uhs>;<xsts-token>
package xbox

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	peopleHubURL = "https://peoplehub.xboxlive.com/users/me/people/social"
	presenceURL  = "https://userpresence.xboxlive.com/users/batch"
	privacyURL   = "https://privacy.xboxlive.com/users/me/privacy/settings"
	// Minecraft Bedrock (Xbox) titleId — для кнопки «Приєднатися».
	minecraftTitleID uint32 = 438140
)

// Домен (напр. hypixel.net) або IPv4 з валідними октетами (0–255), щоб
// не ловити дати/версії на кшталт 1.20.1.0 чи 2025.12.31.5.
var hostRe = regexp.MustCompile(`(?i)\b((?:[a-z0-9-]+\.)+[a-z]{2,}|(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(?:\.(?:25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3})(?::\d{1,5})?\b`)

type Client struct {
	hc   *http.Client
	lang string // Accept-Language (мова UI лаунчера)
}

func NewClient() *Client {
	return &Client{hc: &http.Client{Timeout: 20 * time.Second}}
}

// SetLang задає значення Accept-Language для запитів (мова UI лаунчера).
// People Hub відхиляє запити БЕЗ цього заголовка (HTTP 400 «Required header
// Accept-Language must have a value») — значення обов'язкове, тож при
// порожній мові використовується "en-US".
func (c *Client) SetLang(l string) { c.lang = l }

// auth — заголовки соціальних API Xbox.
func authHeader(uhs, token string) string { return "XBL3.0 x=" + uhs + ";" + token }

func (c *Client) do(token, uhs, method, url, contract string, body io.Reader) ([]byte, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", authHeader(uhs, token))
	req.Header.Set("x-xbl-contract-version", contract)
	// Accept-Language ОБОВ'ЯЗКОВИЙ для People Hub (без нього HTTP 400).
	lang := c.lang
	if lang == "" {
		lang = "en-US"
	}
	req.Header.Set("Accept-Language", lang)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("xbox API %s: HTTP %d: %s", url, resp.StatusCode, string(data))
	}
	return data, nil
}

// Friend — людина зі списку друзів People Hub.
type Friend struct {
	XUID          string `json:"xuid"`
	Gamertag      string `json:"gamertag"`
	DisplayPicRaw string `json:"displayPicRaw"`
	PresenceState string `json:"presenceState"` // Online | Away | Offline
	PresenceText  string `json:"presenceText"`
	DisplayName   string `json:"displayName"`
	RealName      string `json:"realName"`
}

// People повертає список друзів (соціальний граф) поточного користувача.
func (c *Client) People(token, uhs string) ([]Friend, error) {
	data, err := c.do(token, uhs, http.MethodGet, peopleHubURL, "3", nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		People []Friend `json:"people"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out.People, nil
}

// PresenceInfo — присутність одного друга (стан + поточна гра).
type PresenceInfo struct {
	XUID         string `json:"xuid"`
	State        string `json:"state"` // Online | Away | Offline
	Title        string `json:"title"`
	TitleID      uint32 `json:"titleId"`
	RichPresence string `json:"richPresence"`
}

// batchLimit — ліміт xuid в одному batch-запиті presence (API відкидає
// занадто великі пачки; для великих списків друзів ділимо на частини).
const batchLimit = 100

// Presence повертає присутність для списку xuid (batch-запит, з чанкінгом
// по 100, щоб не впертись у ліміт запиту на великих списках друзів).
func (c *Client) Presence(token, uhs string, xuids []string) ([]PresenceInfo, error) {
	out := make([]PresenceInfo, 0, len(xuids))
	for start := 0; start < len(xuids); start += batchLimit {
		end := start + batchLimit
		if end > len(xuids) {
			end = len(xuids)
		}
		chunk, err := c.presenceBatch(token, uhs, xuids[start:end])
		if err != nil {
			return nil, err
		}
		out = append(out, chunk...)
	}
	return out, nil
}

func (c *Client) presenceBatch(token, uhs string, xuids []string) ([]PresenceInfo, error) {
	users := make([]string, 0, len(xuids))
	for _, x := range xuids {
		users = append(users, "xuid("+x+")")
	}
	body, _ := json.Marshal(map[string][]string{"users": users})
	data, err := c.do(token, uhs, http.MethodPost, presenceURL, "3", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	var raw struct {
		Presence []struct {
			XUID    string `json:"xuid"`
			State   string `json:"state"`
			Devices []struct {
				Type   string `json:"type"`
				Titles []struct {
					ID           uint32 `json:"id"`
					Name         string `json:"name"`
					DisplayName  string `json:"displayName"`
					RichPresence string `json:"richPresence"`
				} `json:"titles"`
			} `json:"devices"`
		} `json:"presence"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	out := make([]PresenceInfo, 0, len(raw.Presence))
	for _, p := range raw.Presence {
		info := PresenceInfo{XUID: p.XUID, State: p.State}
		for _, d := range p.Devices {
			if len(d.Titles) == 0 {
				continue
			}
			t := d.Titles[0]
			info.Title = t.Name
			if t.DisplayName != "" {
				info.Title = t.DisplayName
			}
			info.TitleID = t.ID
			info.RichPresence = t.RichPresence
			break
		}
		out = append(out, info)
	}
	return out, nil
}

// SearchByGamertag знаходить людину за gamertag через People Hub search
// (для кнопки «Додати друга»). Повертає знайденого друга або помилку.
func (c *Client) SearchByGamertag(token, uhs, gamertag string) (Friend, error) {
	g := strings.TrimSpace(gamertag)
	if g == "" {
		return Friend{}, fmt.Errorf("порожній gamertag")
	}
	searchURL := "https://peoplehub.xboxlive.com/users/me/people/search/gt/" +
		url.PathEscape(g) + "?maxItems=1"
	data, err := c.do(token, uhs, http.MethodGet, searchURL, "2", nil)
	if err != nil {
		return Friend{}, fmt.Errorf("пошук гравця: %w", err)
	}
	var out struct {
		People []Friend `json:"people"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return Friend{}, err
	}
	if len(out.People) == 0 {
		return Friend{}, fmt.Errorf("гравця «%s» не знайдено на Xbox Live", g)
	}
	return out.People[0], nil
}

// AddFriend знаходить гравця за gamertag і додає його в друзі (соціальний
// граф через Social API: PUT users/me/people/xuid({xuid})).
func (c *Client) AddFriend(token, uhs, gamertag string) (Friend, error) {
	f, err := c.SearchByGamertag(token, uhs, gamertag)
	if err != nil {
		return Friend{}, err
	}
	if f.XUID == "" {
		return Friend{}, fmt.Errorf("гравець «%s» не має xuid", gamertag)
	}
	addURL := "https://social.xboxlive.com/users/me/people/xuid(" + f.XUID + ")"
	if _, err := c.do(token, uhs, http.MethodPut, addURL, "2", nil); err != nil {
		return Friend{}, fmt.Errorf("додавання в друзі: %w", err)
	}
	return f, nil
}

// PrivacySettings — глобальні дозволи приватності Xbox Live користувача.
type PrivacySettings struct {
	// ShareFriendsList — хто бачить твій список друзів (Everyone/OnlyMe/...).
	ShareFriendsList string `json:"shareFriendsList"`
	// AllowFriendRequests — хто може надсилати запити в друзі.
	AllowFriendRequests string `json:"allowFriendRequests"`
}

// Privacy повертає налаштування приватності (соціальні глобальні дозволи).
func (c *Client) Privacy(token, uhs string) (*PrivacySettings, error) {
	body, _ := json.Marshal(map[string][]string{
		"settings": {"ShareFriendsList", "AllowFriendRequests"},
	})
	data, err := c.do(token, uhs, http.MethodPost, privacyURL, "2", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	var raw struct {
		Settings []struct {
			Setting string `json:"setting"`
			Value   string `json:"value"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	out := &PrivacySettings{}
	for _, s := range raw.Settings {
		switch s.Setting {
		case "ShareFriendsList":
			out.ShareFriendsList = s.Value
		case "AllowFriendRequests":
			out.AllowFriendRequests = s.Value
		}
	}
	return out, nil
}

// SetPrivacy оновлює одне або кілька налаштувань приватності Xbox Live.
// Використовує PUT /users/me/privacy/settings з тілом:
//
//	{"settings":[{"setting":"ShareFriendsList","value":"Everyone"},...]}
//
// Допустимі значення: "Everyone" | "FriendsOnly" | "Blocked"
func (c *Client) SetPrivacy(token, uhs string, settings map[string]string) error {
	type entry struct {
		Setting string `json:"setting"`
		Value   string `json:"value"`
	}
	entries := make([]entry, 0, len(settings))
	for k, v := range settings {
		entries = append(entries, entry{Setting: k, Value: v})
	}
	body, _ := json.Marshal(map[string]any{"settings": entries})
	_, err := c.do(token, uhs, http.MethodPut, privacyURL, "2", bytes.NewReader(body))
	return err
}

// JoinAddress витягує IP/домен з тексту активності (richPresence), якщо
// там є адреса сервера (напр. "На сервері hypixel.net"). Повертає "" якщо
// адреси немає.
func JoinAddress(rich string) string {
	if rich == "" {
		return ""
	}
	m := hostRe.FindString(rich)
	if m == "" {
		return ""
	}
	// Відкидаємо підозрілі «домени» з крапкою всередині слова (без крапки).
	if !strings.Contains(m, ".") {
		return ""
	}
	return strings.TrimSpace(m)
}

// IsMinecraft — чи titleId належить Minecraft (для кнопки «Приєднатися»).
func IsMinecraft(titleID uint32) bool { return titleID == minecraftTitleID }
