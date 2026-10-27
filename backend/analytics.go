package main

import (
	"shaurma-launcher-backend/internal/crashguard"
	"bytes"
	"encoding/json"
	"net/http"
	"runtime"
	"strings"
	"time"

	"shaurma-launcher-backend/internal/update"
)

// ═══════════════════════════════════════════════════════════════════════
// Аналітика використання лаунчера — БІК ЛАУНЧЕРА (opt-in)
// ═══════════════════════════════════════════════════════════════════════
//
// Що це: один маленький звіт на worker (POST /telemetry) — «з цього
// пристрою грали стільки-то, нік такий-то, встановлено тоді-то». Потрібно,
// щоб автор лаунчера бачив реальну кількість гравців і активність, а не
// лише лічильник завантажень (/install).
//
// ГОЛОВНЕ ПРАВИЛО — ЗГОДА. Звіти надсилаються ЛИШЕ якщо користувач явно
// погодився (Settings.AnalyticsEnabled = true, за замовчуванням FALSE).
// Поки згоди немає, у мережу не йде жодного байта: функція виходить на
// першій же перевірці, ще до DeviceID/сесій. Питання ставиться один раз —
// модалка в App.svelte (див. AnalyticsConsent* методи нижче), стан «уже
// питали» лежить у meta shaurma.json (ConsentMetaKey), а не в Settings:
// це не налаштування користувача, а одноразовий факт «питання показано».
//
// АНОНІМНІСТЬ. Лаунчер не має й не може мати ключа шифрування — він лише
// передає нік на worker по HTTPS, де той ОДРАЗУ шифрується AES-256-GCM і в
// базу потрапляє вже шифротекстом (див. worker/analytics.js). Ідентифікатор
// пристрою — той самий анонімний sha256(DeviceID), що в /install: сирий
// апаратний ID не покидає машину.
//
// ЧОГО ТУТ НЕМАЄ (свідомо): жодних токенів акаунтів, UUID, email, шляхів
// на диску, назв збірок чи списку модів. Тільки агрегати часу + нік +
// версія/редакція/платформа.
// ═══════════════════════════════════════════════════════════════════════

// analyticsConsentMetaKey — meta-ключ «питання про статистику вже показано».
// Значення — RFC3339 момент показу (зручно бачити в shaurma.json).
const analyticsConsentMetaKey = "analyticsConsentAskedAt"

// analyticsLastSentMetaKey — коли востаннє успішно надсилали звіт (антиспам:
// не смикаємо мережу на кожен старт лаунчера).
const analyticsLastSentMetaKey = "analyticsLastSentAt"

// analyticsSendInterval — не частіше одного звіту на N. Звіт кумулятивний
// (загальний час + к-сть сесій), тому пропущені спроби нічого не втрачають:
// наступний успішний звіт усе одно містить підсумок.
const analyticsSendInterval = 3 * time.Hour

// ── Стан згоди для UI ────────────────────────────────────────────────

// AnalyticsConsentState повертає для фронтенду: чи вже питали і чи увімкнено.
// Окремий метод, а не поле в Settings, бо «питали» — не налаштування, а факт.
type AnalyticsConsentState struct {
	Asked   bool `json:"asked"`
	Enabled bool `json:"enabled"`
}

func (a *App) GetAnalyticsConsent() AnalyticsConsentState {
	if a.cfg == nil {
		return AnalyticsConsentState{}
	}
	return AnalyticsConsentState{
		Asked:   a.analyticsConsentAsked(),
		Enabled: a.cfg.GetSettings().AnalyticsEnabled,
	}
}

// SetAnalyticsConsent зберігає вибір користувача з модалки: enabled=true —
// погодився надсилати статистику, false — відмовився. В обох випадках
// позначаємо, що питання показано (більше не питаємо). При згоді одразу
// надсилаємо перший звіт (force=true), щоб дані не чекали наступного старту.
func (a *App) SetAnalyticsConsent(enabled bool) error {
	if a.cfg == nil {
		return nil
	}
	s := a.cfg.GetSettings()
	s.AnalyticsEnabled = enabled
	if err := a.cfg.UpdateSettings(s); err != nil {
		return err
	}
	a.markAnalyticsConsentAsked()
	if enabled {
		a.sendAnalyticsReport(true)
	}
	return nil
}

func (a *App) analyticsConsentAsked() bool {
	st := a.cfg.Store()
	if st == nil {
		return false
	}
	_, ok, err := st.GetMeta(analyticsConsentMetaKey)
	return err == nil && ok
}

func (a *App) markAnalyticsConsentAsked() {
	if st := a.cfg.Store(); st != nil {
		_ = st.SetMeta(analyticsConsentMetaKey, time.Now().UTC().Format(time.RFC3339))
	}
}

// ── Відправка ────────────────────────────────────────────────────────

// MaybeSendAnalytics — викликається при старті лаунчера і після завершення
// ігрової сесії. Не блокує виклик, помилки — лише в лог, без UI-шумів:
// телеметрія не має права ламати запуск гри.
func (a *App) MaybeSendAnalytics() {
	if a.cfg == nil {
		return
	}
	// ГОЛОВНА перевірка: без явної згоди — жодного запиту (див. шапку файлу).
	if !a.cfg.GetSettings().AnalyticsEnabled {
		return
	}
	a.sendAnalyticsReport(false)
}

func (a *App) sendAnalyticsReport(force bool) {
	if a.cfg == nil {
		return
	}
	// Повторюємо перевірку згоди всередині: сюди ведуть два шляхи (старт і
	// модалка), і згоду могли зняти між ними.
	if !force && !a.cfg.GetSettings().AnalyticsEnabled {
		return
	}

	st := a.cfg.Store()
	if !force && st != nil {
		if last, ok, err := st.GetMeta(analyticsLastSentMetaKey); err == nil && ok {
			if t, perr := time.Parse(time.RFC3339, last); perr == nil {
				if time.Since(t) < analyticsSendInterval {
					return
				}
			}
		}
	}

	deviceID, err := a.cfg.DeviceID()
	if err != nil || deviceID == "" {
		return
	}
	token := update.Token()
	if token == "" {
		return // збірка без токена — worker відхилить (401), не турбуємо мережу
	}

	// Знімок даних рахуємо в горутині: підрахунок сесій читає instance.json
	// усіх збірок (диск), і робити це в UI-потоці перед стартом гри — погано.
	accounts := a.cfg.GetAccounts()
	active := a.cfg.ActiveAccount()
	version := readLauncherVersion(a.cfg.InstallDir())
	edition := launcherEditionName()

	go func() {
		defer crashguard.Recover("analytics.go:162")
		playSeconds, sessions, todaySeconds, nickFromSessions := a.collectUsageStats(active.Username)

		nick := strings.TrimSpace(active.Username)
		if nick == "" {
			nick = nickFromSessions // грали з акаунта, якого вже немає в списку
		}

		payload := map[string]any{
			"installId":    deviceID,
			"nick":         nick,
			"version":      version,
			"edition":      edition,
			"platform":     runtime.GOOS,
			"playSeconds":  playSeconds,
			"sessions":     sessions,
			"todaySeconds": todaySeconds,
			"accounts":     len(accounts),
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return
		}

		req, err := http.NewRequest(http.MethodPost, update.CDNBaseURL()+"/telemetry", bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("User-Agent", "ShaurmaLauncher/2.0")
		req.Header.Set("X-Shaurma-Token", token)
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{Timeout: 15 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return // мережа недоступна — наступний звіт усе надолужить
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return
		}
		if st := a.cfg.Store(); st != nil {
			_ = st.SetMeta(analyticsLastSentMetaKey, time.Now().UTC().Format(time.RFC3339))
		}
	}()
}

// collectUsageStats рахує підсумки з ігрових сесій (усі збірки, усі акаунти):
// загальний час, кількість сесій, час ЗА СЬОГОДНІ і «останній нік, яким
// реально грали» — на випадок, коли активний акаунт щойно змінили й сесій
// під ним ще немає.
//
// Одна збірка може мати сесії під різними ніками (гравець перевходив) — тоді
// беремо нік із найсвіжішої сесії. Так у баті «хто користується лаунчером»
// опиняється справжній нік гравця, а не порожній рядок.
func (a *App) collectUsageStats(fallbackNick string) (playSeconds, sessions, todaySeconds int64, nick string) {
	recs, err := a.cfg.LoadAllPlaytimeSessions()
	if err != nil {
		return 0, 0, 0, strings.TrimSpace(fallbackNick)
	}
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	var latest time.Time

	for _, r := range recs {
		if r.Seconds > 0 {
			playSeconds += r.Seconds
		}
		sessions++
		if r.StartedAt.After(startOfDay) || r.StartedAt.Equal(startOfDay) {
			todaySeconds += r.Seconds
		}
		if r.AccountName != "" && r.StartedAt.After(latest) {
			latest = r.StartedAt
			nick = r.AccountName
		}
	}
	if nick == "" {
		nick = strings.TrimSpace(fallbackNick)
	}
	return playSeconds, sessions, todaySeconds, nick
}
