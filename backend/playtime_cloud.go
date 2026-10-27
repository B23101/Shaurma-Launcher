package main

// playtime_cloud.go — ЧИСТА логіка хмарного ігрового часу (без мережі й без build-тегів,
// щоб тестуватися окремо). Мережа й акаунт — у analytics_cloud_shaurma.go.
//
// Що йде в хмару: ЗАГАЛЬНИЙ час акаунта по днях, БЕЗ розбивки по збірках (вона лишається
// локально в instance.json кожної збірки). Сервер: vps/internal/playtime.
//
// Протокол (ідемпотентний):
//   - days — АБСОЛЮТНІ значення за останні N днів (вікно). Повторна відправка нічого не
//     подвоює (сервер робить upsert за пристрій+день).
//   - months — історія СТАРША за вікно, місячними підсумками. Шлемо ОДИН раз (backfill):
//     сервер вставляє їх лише якщо такого місяця ще нема.
//   - Далі старі дні сервер сам згортає в місяці (playtime.Compact), а клієнт у вікні лише
//     ≤ playtimeMaxWindowDays < 90 діб, тож згорнутий день повторно не надсилається.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"

	"shaurma-launcher-backend/internal/store"
)

const (
	// playtimeMinWindowDays — скільки останніх днів шлемо завжди (пересилання дешеве).
	playtimeMinWindowDays = 14
	// playtimeMaxWindowDays — стеля вікна. МУСИТЬ бути < vps/internal/playtime.DailyRetentionDays (90):
	// сервер відкидає дні старші за 90 діб, бо вони вже згорнуті в місяці.
	playtimeMaxWindowDays = 80
	// playtimeResendAfter — навіть без змін шлемо знімок раз на тиждень (самолікування після
	// відновлення БД сервера з бекапу).
	playtimeResendAfter = 7 * 24 * time.Hour
)

type playtimeDay struct {
	D string `json:"d"` // 2006-01-02, локальний календарний день гравця
	S int64  `json:"s"` // секунди
	N int    `json:"n"` // сесії
}

type playtimeMonth struct {
	M string `json:"m"` // 2006-01
	S int64  `json:"s"`
	N int    `json:"n"`
}

type playtimeSyncReq struct {
	Days   []playtimeDay   `json:"days"`
	Months []playtimeMonth `json:"months,omitempty"`
	// Builds — скільки збірок має ігровий час. Лічильник живе лише тут (локально), тож без нього
	// друзі не побачили б «Збірок» у профілі. 0 не шлемо (omitempty): нічого й показувати.
	Builds int `json:"builds,omitempty"`
}

// playtimeCloudState — що ми пам'ятаємо про попередню синхронізацію (файл поруч із даними
// соціальної системи акаунта; кожен акаунт Шаурми має свій).
type playtimeCloudState struct {
	Backfilled  bool   `json:"backfilled"`
	LastSyncDay string `json:"lastSyncDay"` // 2006-01-02 останньої УСПІШНОЇ відправки
	LastHash    string `json:"lastHash"`
	LastSentAt  int64  `json:"lastSentAt"` // unix сек
}

func localDay(t time.Time) time.Time {
	y, m, d := t.In(time.Local).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}

// playtimeWindowDays — розмір вікна: від останньої успішної синхронізації (+3 дні запасу), у межах
// [min, max]. Перша синхронізація — мінімальне вікно + backfill історії.
func playtimeWindowDays(st playtimeCloudState, now time.Time) int {
	w := playtimeMinWindowDays
	if last, err := time.ParseInLocation("2006-01-02", st.LastSyncDay, time.Local); err == nil {
		gap := int(localDay(now).Sub(last).Hours()/24) + 3
		if gap > w {
			w = gap
		}
	}
	if w > playtimeMaxWindowDays {
		w = playtimeMaxWindowDays
	}
	return w
}

// buildPlaytimeSnapshot збирає знімок із сирих сесій і місячних підсумків (store.PlaytimeSession.Sessions>0).
// Дні — лише вікно; місяці — лише якщо !st.Backfilled, і лише дані СТАРШІ за вікно (щоб ніщо не рахувалось двічі).
func buildPlaytimeSnapshot(sessions []store.PlaytimeSession, st playtimeCloudState, now time.Time) playtimeSyncReq {
	windowStart := localDay(now).AddDate(0, 0, -(playtimeWindowDays(st, now) - 1))
	days := map[string]*playtimeDay{}
	months := map[string]*playtimeMonth{}

	for _, s := range sessions {
		if s.Seconds <= 0 || s.StartedAt.IsZero() {
			continue
		}
		n := int(s.Sessions)
		if n <= 0 {
			n = 1
		}
		isRollup := s.Sessions > 0
		if !isRollup && !s.StartedAt.Before(windowStart) {
			k := localDay(s.StartedAt).Format("2006-01-02")
			d := days[k]
			if d == nil {
				d = &playtimeDay{D: k}
				days[k] = d
			}
			d.S += s.Seconds
			d.N += n
			continue
		}
		if st.Backfilled {
			continue
		}
		k := s.StartedAt.In(time.Local).Format("2006-01")
		m := months[k]
		if m == nil {
			m = &playtimeMonth{M: k}
			months[k] = m
		}
		m.S += s.Seconds
		m.N += n
	}

	out := playtimeSyncReq{Days: make([]playtimeDay, 0, len(days))}
	for _, d := range days {
		out.Days = append(out.Days, *d)
	}
	sort.Slice(out.Days, func(i, j int) bool { return out.Days[i].D < out.Days[j].D })
	for _, m := range months {
		out.Months = append(out.Months, *m)
	}
	sort.Slice(out.Months, func(i, j int) bool { return out.Months[i].M < out.Months[j].M })
	return out
}

// playtimeSnapshotHash — відбиток знімка: однакові дані → нема сенсу слати знов.
func playtimeSnapshotHash(r playtimeSyncReq) string {
	b, _ := json.Marshal(r)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:8])
}

// playtimeShouldSend — чи варто слати знімок. Порожній знімок не шлемо (нічого зберігати);
// без змін — не частіше, ніж раз на playtimeResendAfter.
func playtimeShouldSend(r playtimeSyncReq, st playtimeCloudState, now time.Time) bool {
	if len(r.Days) == 0 && len(r.Months) == 0 {
		return false
	}
	if playtimeSnapshotHash(r) != st.LastHash {
		return true
	}
	return now.Sub(time.Unix(st.LastSentAt, 0)) >= playtimeResendAfter
}
