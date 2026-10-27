package update

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strconv"
	"strings"
	"time"
)

// cdnBaseURL — той самий Worker, що обслуговує /packs, /java і
// launcher/launcher-version.json (shrm-cdn-worker/worker.js), а не
// сторонній api.shaurma.lol — раніше тут стояв інший, застарілий домен,
// і перевірка версії лаунчера ходила не туди, куди сам апдейтер.
const cdnBaseURL = "https://shaurma-proxy.shaurmaofficial2210.workers.dev"

// token — токен CDN для ПОТОЧНОЇ редакції лаунчера, вшивається на етапі
// збірки через -ldflags (build.ps1: -X shaurma-launcher-backend/internal/update.token=...).
// Worker (worker.js: resolveTokenType/resolveR2Key) визначає РЕДАКЦІЮ
// (shaurma/base) саме по цьому токену і за ним же вирішує, який
// launchers/<edition>/launcher-version.json віддати на один і той самий
// шлях /launcher/launcher-version.json. Без токена — і без правильного
// User-Agent — Worker відповідає 404/401 ще до порівняння версій (див.
// worker.js, розділи 5-7), тож перевірка оновлень завжди провалювалась
// би, хоч для shaurma, хоч для base.
var token string

type Checker struct {
	client        *http.Client
	currentVer    string
	updateURL     string
	updateBetaURL string
	baseURL       string // origin CDN (для SetUpdateURL у тестах/діагностиці)
	token         string
	channel       string // "stable" | "beta" — з settings.json (SetChannel)
}

type UpdateInfo struct {
	Available    bool   `json:"available"`
	Version      string `json:"version"`
	DownloadURL  string `json:"downloadUrl"`
	ChangelogURL string `json:"changelogUrl"`
	IsCritical   bool   `json:"isCritical"`
}

// remoteManifest — ТОЙ САМИЙ контракт, що updater/internal/manifest.Manifest
// (обидва читають той самий launcher-version.json з Worker'а). Раніше тут
// був інший, вигаданий формат (latest/url/changelog/critical), який ніколи
// не збігався з реальним файлом — Latest завжди виходив порожнім рядком,
// і кнопка «Перевірити оновлення» в налаштуваннях завжди мовчки казала
// "оновлень немає", навіть коли вони були.
type remoteManifest struct {
	Version      string `json:"version"`
	ReleaseNotes string `json:"releaseNotes"`
}

func NewChecker(currentVersion string) *Checker {
	return &Checker{
		client:        &http.Client{Timeout: 15 * time.Second},
		currentVer:    currentVersion,
		updateURL:     cdnBaseURL + "/launcher/launcher-version.json",
		updateBetaURL: cdnBaseURL + "/launcher/launcher-version-beta.json",
		baseURL:       cdnBaseURL,
		token:         token,
		channel:       "stable",
	}
}

// SetChannel виставляє канал (stable/beta) — викликається з app.go при
// старті з settings.UpdateChannel, і з фронтенду при зміні налаштування,
// щоб «Перевірити оновлення» одразу дивилось у той самий канал, який
// реально використає checkAutoUpdate()/апдейтер.
func (c *Checker) SetChannel(ch string) {
	if ch == "beta" {
		c.channel = "beta"
		return
	}
	c.channel = "stable"
}

func (c *Checker) Check() (*UpdateInfo, error) {
	// Захист від «тихого» регресу: якщо хтось зібрав launcher.exe без
	// -ldflags -X internal/update.token=..., порожній токен призведе до
	// гарантованого 401 від Worker для КОЖНОГО користувача — робимо
	// причину явною одразу, а не після незрозумілої помилки парсингу JSON.
	if c.token == "" {
		return nil, fmt.Errorf("update: token is empty — build launcher with -ldflags -X shaurma-launcher-backend/internal/update.token=...")
	}

	stable, err := c.fetchOne(c.updateURL)
	if err != nil {
		return nil, err
	}

	best := stable
	bestURL := c.updateURL
	if c.channel == "beta" {
		if beta, err := c.fetchOne(c.updateBetaURL); err == nil && Compare(beta.Version, stable.Version) > 0 {
			best = beta
			bestURL = c.updateBetaURL
		}
		// beta-файл відсутній/недоступний — тихо лишаємось на stable,
		// так само як у shrm-updater (fetchBestRemoteManifest).
	}

	available := best.Version != "" && Compare(best.Version, c.currentVer) > 0
	return &UpdateInfo{
		Available:    available,
		Version:      best.Version,
		DownloadURL:  bestURL,
		ChangelogURL: best.ReleaseNotes,
		IsCritical:   false,
	}, nil
}

func (c *Checker) fetchOne(url string) (*remoteManifest, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	// Worker (worker.js) відкидає запити без цих двох заголовків ще до
	// перевірки версії: без User-Agent, що починається на
	// "ShaurmaLauncher/", — 404 (маскування ендпоінта від сканерів); без
	// X-Shaurma-Token (мін. 16 символів, звірка timing-safe) — 401/403.
	req.Header.Set("User-Agent", "ShaurmaLauncher/2.0")
	req.Header.Set("X-Shaurma-Token", c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update: HTTP %d від %s (%s)", resp.StatusCode, url, strings.TrimSpace(string(body)))
	}

	var m remoteManifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("update: парсинг %s: %w", url, err)
	}
	return &m, nil
}

func (c *Checker) SetUpdateURL(url string) {
	c.updateURL = url
	// Одночасно підміняємо origin — щоб тести/діагностика ходили в той
	// самий сервер.
	if u, err := neturl.Parse(url); err == nil && u.Scheme != "" && u.Host != "" {
		c.baseURL = u.Scheme + "://" + u.Host
		// І beta-файл — на той самий origin (та сама схема шляху
		// /launcher/launcher-version-beta.json). Раніше updateBetaURL
		// лишався вшитим на продакшн-CDN, тож у тестах/діагностиці
		// beta-маніфест запитувався не з того сервера, що stable —
		// бета-канал у таких запусках «мовчазно» падав на stable.
		c.updateBetaURL = c.baseURL + "/launcher/launcher-version-beta.json"
	}
}

// SetToken дозволяє підмінити токен (тести/діагностика); у звичайній
// роботі токен береться з build-time вшитого пакетного token.
func (c *Checker) SetToken(t string) { c.token = t }

// CDNBaseURL — публічний доступ до вшитого origin воркера (для суміжних
// систем, що ходять на той самий CDN: відправка журналу помилок на
// POST /errors у launcher_errors.go).
func CDNBaseURL() string { return cdnBaseURL }

// Token — публічний доступ до токена CDN поточної редакції (той самий,
// що для перевірки оновлень). Воркер визначає редакцію по ньому і без
// нього відмовив би запис журналу помилок (401/403).
func Token() string { return token }

func (c *Checker) CurrentVersion() string {
	if c.currentVer == "" {
		return "2.0.0"
	}
	return c.currentVer
}

func (c *Checker) SetVersion(v string) { c.currentVer = v }

func GetLatestVersion() string {
	return "2.0.0"
}

// Compare — семверне порівняння "a.b.c[.d...]" рядків (без суфіксів
// pre-release/build). Повертає >0 якщо v1 новіша за v2, <0 якщо старіша,
// 0 якщо однакові. Некоректні/відсутні сегменти трактуються як 0, щоб
// "2.0" і "2.0.0" вважались рівними.
//
// ВАЖЛИВО: порівнюються ВСІ сегменти, не лише 3 — 4-й сегмент це бета-
// суфікс ("2.1.5.1" > "2.1.5"), а "2.1.5.0" рівнозначне "2.1.5" (точковий
// 0 у кінці не робить версію бета).
func Compare(v1, v2 string) int {
	p1, p2 := splitVersion(v1), splitVersion(v2)
	n := len(p1)
	if len(p2) > n {
		n = len(p2)
	}
	for i := 0; i < n; i++ {
		a, b := 0, 0
		if i < len(p1) {
			a = p1[i]
		}
		if i < len(p2) {
			b = p2[i]
		}
		if a != b {
			if a > b {
				return 1
			}
			return -1
		}
	}
	return 0
}

func splitVersion(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	parts := strings.SplitN(v, "-", 2)[0] // відкидаємо pre-release суфікс типу "-beta.1"
	segs := strings.Split(parts, ".")
	out := make([]int, 0, len(segs))
	for _, s := range segs {
		n, err := strconv.Atoi(s)
		if err != nil {
			n = 0
		}
		out = append(out, n)
	}
	return out
}
