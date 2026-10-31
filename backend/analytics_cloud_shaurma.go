package main

// analytics_cloud_shaurma.go — хмарне збереження ігрового часу в обох редакціях.
// Локально час, як і раніше, лежить по збірках; у хмару йде лише ЗАГАЛЬНЕ по днях (див. playtime_cloud.go).
// Друзі цей час поки не бачать — сервер віддає його лише власнику.

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/crashguard"
)

// playtimeSyncPath — маршрут vps/internal/playtime (mux.Handle("/friends/playtime/")).
const playtimeSyncPath = "/friends/playtime/sync"

var (
	playtimeSyncMu    sync.Mutex
	playtimeSyncTimer *time.Timer
	playtimeSyncBusy  bool
)

// scheduleCloudPlaytimeSync — з дебаунсом: кілька завершених сесій поспіль = одна відправка.
func (a *App) scheduleCloudPlaytimeSync(delay time.Duration) {
	playtimeSyncMu.Lock()
	defer playtimeSyncMu.Unlock()
	if playtimeSyncTimer != nil {
		playtimeSyncTimer.Stop()
	}
	playtimeSyncTimer = time.AfterFunc(delay, func() {
		defer crashguard.Recover("analytics_cloud_shaurma.go:scheduleCloudPlaytimeSync")
		a.syncPlaytimeToCloud()
	})
}

func playtimeStatePath(dir string) string { return filepath.Join(dir, "playtime-cloud.json") }

func loadPlaytimeState(dir string) playtimeCloudState {
	var st playtimeCloudState
	if b, err := os.ReadFile(playtimeStatePath(dir)); err == nil {
		_ = json.Unmarshal(b, &st)
	}
	return st
}

func savePlaytimeState(dir string, st playtimeCloudState) {
	b, _ := json.Marshal(st)
	tmp := playtimeStatePath(dir) + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, playtimeStatePath(dir))
	}
}

// syncPlaytimeToCloud — одна спроба синхронізації. Тиха: помилка мережі/сервера лише в лог,
// повторимо при наступній сесії або старті. Одночасно виконується не більше однієї.
func (a *App) syncPlaytimeToCloud() {
	if s := a.cfg.GetSettings(); s.DisableCloudPlaytime || !s.RecordGameTime {
		return
	}
	id := a.currentSocialIdent()
	if id.key == "" {
		return // нема акаунта Шаурми — нікуди слати
	}
	dir := a.socialDirFor(id.launcherID)
	if dir == "" {
		return
	}
	playtimeSyncMu.Lock()
	if playtimeSyncBusy {
		playtimeSyncMu.Unlock()
		return
	}
	playtimeSyncBusy = true
	playtimeSyncMu.Unlock()
	defer func() { playtimeSyncMu.Lock(); playtimeSyncBusy = false; playtimeSyncMu.Unlock() }()

	sessions, err := a.cfg.LoadAllPlaytimeSessions()
	if err != nil {
		return
	}
	now := time.Now()
	st := loadPlaytimeState(dir)
	req := buildPlaytimeSnapshot(sessions, st, now)
	req.Builds = len(a.cfg.GetPlaytime()) // ту саму цифру показує «Мій акаунт»; входить у хеш → зміна піде на сервер
	if !playtimeShouldSend(req, st, now) {
		return
	}
	body, err := json.Marshal(req)
	if err != nil {
		return
	}
	resp, err := a.shaurmaRequest(id.launcherID, http.MethodPost, playtimeSyncPath, body)
	if err != nil {
		log.Printf("playtime cloud: %v", err)
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode != http.StatusOK {
		// 404 — вузол ще без цієї функції (стара схема/версія); 429 — зачекаємо. Нічого не ламаємо.
		log.Printf("playtime cloud: HTTP %d", resp.StatusCode)
		return
	}
	hash := playtimeSnapshotHash(req)
	savePlaytimeState(dir, playtimeCloudState{
		Backfilled:  true,
		LastSyncDay: localDay(now).Format("2006-01-02"),
		LastHash:    hash,
		LastSentAt:  now.Unix(),
	})
}
