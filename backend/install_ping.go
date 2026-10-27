package main

import (
	"shaurma-launcher-backend/internal/crashguard"
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"shaurma-launcher-backend/internal/update"
)

// installPingSentMetaKey — ключ у shaurma.json (Store.GetMeta/SetMeta),
// що позначає «вже надсилали install-ping з цього профілю користувача».
// Це ЛИШЕ оптимізація (не шле мережевий запит на кожен старт лаунчера) —
// реальний захист від подвійного заліку живе на сервері (worker.js, дедуп
// за sha256(deviceID) у R2, той самий патерн, що й /errors). Навіть якщо
// цей запис видалено чи запит повториться — сервер порахує пристрій
// рівно один раз.
//
// РЕГРЕСІЯ (виправлено): раніше прапорець жив в окремому файлі
// install-ping-sent.txt поруч із settings.json — усупереч задокументованій
// архітектурі. installPingSentMarker (стара назва файла) лишається лише
// для одноразової міграції: якщо файл існує, а запис у shaurma.json ще
// нема, вважаємо пінг уже надісланим (не шлемо повторно) і видаляємо файл.
const installPingSentMetaKey = "installPingSentAt"
const installPingSentMarker = "install-ping-sent.txt"

// SendInstallPing надсилає один раз на пристрій позначку "лаунчер
// встановлено" на worker POST /install, з унікальним (для цього
// фізичного пристрою) DeviceID. Викликається при старті лаунчера
// (App.startup), не блокує запуск — мережева помилка чи відсутність
// токена просто пропускають відправку без сповіщення користувача.
//
// Анти-накрутка (детальніше — DeviceID() у internal/config/deviceid.go
// та обробник /install у worker.js):
//   - DeviceID стабільний для фізичного пристрою (переживає перевстановлення
//     лаунчера), тому перевстановлення НЕ рахується як новий гравець;
//   - сервер зберігає один запис на sha256(deviceID) — дублікат лише
//     оновлює lastSeen, ніколи не інкрементить лічильник вдруге;
//   - без валідного токена (X-Shaurma-Token) worker відхиляє запит —
//     той самий захист, що й для /errors, /batch тощо.
func (a *App) SendInstallPing() {
	if a.cfg == nil {
		return
	}
	st := a.cfg.Store()

	// Уже слали з цього профілю — не турбуємо мережу. Перевіряємо
	// shaurma.json (новий шлях) з фолбеком на старий install-ping-sent.txt
	// (міграція: юзер оновився зі старої версії лаунчера, файл на диску
	// ще лежить, у shaurma.json запису ще нема).
	if st != nil {
		if _, ok, err := st.GetMeta(installPingSentMetaKey); err == nil && ok {
			return
		}
	}
	legacyMarkerPath := filepath.Join(a.cfg.ConfigDir(), installPingSentMarker)
	if _, err := os.Stat(legacyMarkerPath); err == nil {
		if st != nil {
			// Переносимо факт «уже слали» в shaurma.json і прибираємо
			// старий файл — далі shaurma.json єдине джерело правди.
			_ = st.SetMeta(installPingSentMetaKey, time.Now().UTC().Format(time.RFC3339))
			_ = os.Remove(legacyMarkerPath)
		}
		return
	}

	deviceID, err := a.cfg.DeviceID()
	if err != nil || deviceID == "" {
		return
	}

	token := update.Token()
	if token == "" {
		return // збірка без токена — воркер все одно відхилить (401)
	}

	go func() {
		defer crashguard.Recover("install_ping.go:80")
		payload := map[string]string{
			"deviceId": deviceID,
			"version":  readLauncherVersion(a.cfg.InstallDir()), // InstallDir, не Dir — див. коментар у newUpdateChecker (app.go)
			"edition":  launcherEditionName(),
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return
		}

		req, err := http.NewRequest(http.MethodPost, update.CDNBaseURL()+"/install", bytes.NewReader(body))
		if err != nil {
			return
		}
		req.Header.Set("User-Agent", "ShaurmaLauncher/2.0")
		req.Header.Set("X-Shaurma-Token", token)
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			return // мережа недоступна — спробуємо за наступного старту
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return
		}

		// Успіх — ставимо прапорець у shaurma.json, щоб не слати повторно
		// на кожен старт.
		if st != nil {
			_ = st.SetMeta(installPingSentMetaKey, time.Now().UTC().Format(time.RFC3339))
		}
	}()
}
