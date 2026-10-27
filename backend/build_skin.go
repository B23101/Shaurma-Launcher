package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/atomicfile"
	"shaurma-launcher-backend/internal/console"
	"shaurma-launcher-backend/internal/model"
)

// ── Скін збірки ─────────────────────────────────────────────────────────
// Фіча «Скін збірки» в розділі «Гра»: при запуску збірки обраний пресет
// з Гардероба автоматично застосовується на ліцензійний акаунт гравця
// (через Mojang API), а при зупинці збірки попередній скін і плащ
// акаунта повертаються (навіть якщо той вигляд не збережений як пресет).
//
// Надійність (вимога «повернення працює на 100%»):
//   - Перед застосуванням знімається СНАПШОТ поточного вигляду акаунта
//     (скін PNG + активний плащ) і зберігається НА ДИСКУ.
//   - Повернення робиться з ретраями (до 6 спроб з бек-офом) — API Mojang
//     може короткочасно віддавати 429/5xx/мережеві помилки.
//   - Якщо повернення так і не вдалось (лаунчер убито під час гри, немає
//     мережі) — снапшот ЛИШАЄТЬСЯ на диску, і наступний запуск лаунчера
//     (restoreStaleBuildSkins) повторить спробу. Поки лаунчер/трей живі —
//     повернення виконується в exit-хендлері сесії одразу після виходу гри.

// buildSkinSnapshot — збережений вигляд акаунта ДО застосування пресета.
// Лежить у <dataDir>/skin-snapshots/<accountID>_<buildID>.json.
type buildSkinSnapshot struct {
	AccountID string `json:"accountId"`
	BuildID   string `json:"buildId"`
	PresetID  string `json:"presetId"`
	// SkinPNG — попередній скін акаунта (base64 PNG). Порожній = власного
	// скіна не було (дефолтний Steve — API не вміє його «зняти»).
	SkinPNG string `json:"skinPng,omitempty"`
	Slim    bool   `json:"slim"`
	// CapeID — ID активного плаща ДО застосування; HasCape=false — плаща
	// не було (при поверненні активний плащ знімається).
	CapeID  string `json:"capeId,omitempty"`
	HasCape bool   `json:"hasCape"`
	// CreatedAt — коли знято снапшот (для діагностики).
	CreatedAt int64 `json:"createdAt"`
}

// skinSnapshotsDir — тека снапшотів (поруч зі skins/, переживає чистку cache/).
func (a *App) skinSnapshotsDir() string {
	return filepath.Join(a.cfg.Dir(), "skin-snapshots")
}

// skinSnapshotPath — шлях до снапшота конкретної (акаунт, збірка) пари.
// Ім'я файлу — КОРОТКИЙ SHA-256 від пари, а не конкатенація імен:
// акаунт/збірка можуть містити "_" (UUID/слаги), і конкатенація давала б
// колізії ("a_b"+"c" vs "a"+"b_c" → один файл, снапшоти перезаписували
// б одне одного). Хеш унікальний для пари і безпечний у файловій системі.
func (a *App) skinSnapshotPath(accountID, buildID string) string {
	sum := sha256.Sum256([]byte(accountID + "|" + buildID))
	return filepath.Join(a.skinSnapshotsDir(), hex.EncodeToString(sum[:8])+".json")
}

func (a *App) saveSkinSnapshot(snap buildSkinSnapshot) error {
	if err := os.MkdirAll(a.skinSnapshotsDir(), 0o755); err != nil {
		return err
	}
	return atomicfile.WriteJSONAtomic(a.skinSnapshotPath(snap.AccountID, snap.BuildID), snap)
}

func (a *App) loadSkinSnapshot(accountID, buildID string) (*buildSkinSnapshot, error) {
	data, err := os.ReadFile(a.skinSnapshotPath(accountID, buildID))
	if err != nil {
		return nil, err
	}
	var snap buildSkinSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

func (a *App) deleteSkinSnapshot(accountID, buildID string) {
	_ = os.Remove(a.skinSnapshotPath(accountID, buildID))
}

// skinRestoreEnabled — чи треба повертати попередній вигляд після зупинки
// цієї збірки. nil = увімкнено (дефолт), явний false = скін лишається.
func skinRestoreEnabled(icfg model.InstanceConfig) bool {
	return icfg.SkinRestoreOnExit == nil || *icfg.SkinRestoreOnExit
}

// applyBuildSkinBeforeLaunch застосовує пресет «Скіна збірки» на акаунт
// ПЕРЕД стартом гри. Викликається з LaunchInstance після резолву акаунта.
//   - Пресет не заданий або акаунт не ліцензійний — нічого не робимо.
//   - Знімаємо снапшот поточного вигляду (щоб повернути після зупинки).
//   - Застосовуємо пресет з ретраями.
//
// Якщо застосування не вдалось — повертаємо помилку (консоль покаже
// попередження), але запуск гри НЕ блокується.
func (a *App) applyBuildSkinBeforeLaunch(buildID string, account model.Account, icfg model.InstanceConfig) error {
	if icfg.SkinPresetID == "" {
		return nil
	}
	if !account.IsLicensed {
		return fmt.Errorf("скіна збірки потребує ліцензійний (Microsoft) акаунт — пропущено")
	}

	// 1. Снапшот поточного вигляду акаунта.
	skinPNG, slim, capeID, hasCape, err := a.wardrobe.SnapshotActiveLook(account.ID)
	if err != nil {
		// Не змогли зняти снапшот — без нього не можна гарантувати
		// повернення; застосовувати пресет без снапшота небезпечно
		// (вигляд гравця може загубитись), тож чесно відмовляємось.
		return fmt.Errorf("скіна збірки: не вдалося зняти снапшот поточного вигляду: %w", err)
	}
	snap := buildSkinSnapshot{
		AccountID: account.ID,
		BuildID:   buildID,
		PresetID:  icfg.SkinPresetID,
		SkinPNG:   base64.StdEncoding.EncodeToString(skinPNG),
		Slim:      slim,
		CapeID:    capeID,
		HasCape:   hasCape,
		CreatedAt: time.Now().Unix(),
	}
	// ПЕРЕД застосуванням знімаємо снапшот: якщо гра вже йде (перезапуск
	// тієї самої збірки), попередній restore з минулого виходу міг ще
	// ретраїтись у фоні — новий снапшот стає єдиним джерелом правди, і
	// старий restore побачить IsRunning і припиниться (див. guard у
	// restoreBuildSkinAfterExit).
	if err := a.saveSkinSnapshot(snap); err != nil {
		return fmt.Errorf("скіна збірки: не вдалося зберегти снапшот: %w", err)
	}
	// Скидаємо старий статус повернення (failed з минулого виходу) —
	// починаємо новий цикл застосування; UI оновить панель на подію
	// skin:restore зі статусом applied.
	a.skinRestoreTracker.clear(account.ID, buildID)

	// 2. Застосовуємо пресет (ретраї на мережу/429/5xx).
	if err := withSkinRetries(4, 2*time.Second, func() error {
		return a.wardrobe.ApplyPreset(account.ID, icfg.SkinPresetID)
	}); err != nil {
		return fmt.Errorf("скіна збірки: не вдалося застосувати пресет: %w", err)
	}
	return nil
}

// restoreBuildSkinAfterExit повертає акаунту попередній скін і плащ після
// зупинки збірки. Викликається з exit-хендлера сесії (синхронно, але вже
// після того, як процес гри завершився) та з restoreStaleBuildSkins.
//
// Логіка:
//   - Снапшота немає → нічого робити (скіна збірки не було).
//   - Повернення вимкнене (галочка знята) → видаляємо снапшот, скін
//     лишається як змінився.
//   - Повернення увімкнене → відновлюємо вигляд з ретраями (до 6 спроб).
//     При успіху снапшот видаляється. При невдачі снапшот ЛИШАЄТЬСЯ —
//     наступний старт лаунчера спробує ще раз (restoreStaleBuildSkins).
func (a *App) restoreBuildSkinAfterExit(buildID, accountID string) {
	snap, err := a.loadSkinSnapshot(accountID, buildID)
	if err != nil {
		return // снапшота немає — нічого повертати
	}

	icfg := a.cfg.GetInstanceConfig(buildID)
	if !skinRestoreEnabled(icfg) {
		// Користувач вимкнув «повертати скін» — лишаємо як змінився.
		a.deleteSkinSnapshot(accountID, buildID)
		a.skinRestoreTracker.clear(buildID, accountID)
		return
	}

	// Повідомляємо UI: повернення почалось (ретраї можуть зайняти до ~93с).
	a.emitSkinRestore(SkinRestoreStatus{
		BuildID: buildID, AccountID: accountID,
		Status: "restoring", Attempt: 1, MaxAttempts: 6, RestoreOnExit: true,
	})

	skinPNG, _ := base64.StdEncoding.DecodeString(snap.SkinPNG)
	const maxAttempts = 6
	attempt := 0
	err = withSkinRetries(maxAttempts, 3*time.Second, func() error {
		attempt++
		// Guard від гонки з ПОВТОРНИМ запуском тієї самої збірки: поки цей
		// restore ретраїться (до ~93с бек-офа), користувач міг знову
		// запустити збірку — applyBuildSkinBeforeLaunch вже зняв НОВИЙ
		// снапшот і застосував пресет. Старий restore не повинен перебити
		// свіжий стан: якщо сесія знову жива — припиняємось БЕЗ видалення
		// снапшота (ним володіє новий запуск).
		if a.sessionsM.IsRunning(accountID, buildID) {
			return errSessionRestarted
		}
		// Прогрес спроб — кожна наступна спроба показує актуальний номер
		// у UI (спроба 2/6, 3/6…).
		a.emitSkinRestore(SkinRestoreStatus{
			BuildID: buildID, AccountID: accountID,
			Status: "restoring", Attempt: attempt, MaxAttempts: maxAttempts, RestoreOnExit: true,
		})
		return a.wardrobe.RestoreLook(accountID, skinPNG, snap.Slim, snap.CapeID, snap.HasCape)
	})
	if err == errSessionRestarted {
		// Новий запуск володіє снапшотом — чистимо трекер, його не чіпаємо.
		a.skinRestoreTracker.clear(buildID, accountID)
		return
	}
	if err != nil {
		// Снапшот не чіпаємо — повторимо при наступному старті лаунчера.
		if buf := a.consoleBufFor(accountID, buildID); buf != nil {
			buf.AppendLevel(console.LevelWarn, fmt.Sprintf("Не вдалося повернути попередній скін: %v (повториться при наступному запуску лаунчера)", err))
		}
		a.emitSkinRestore(SkinRestoreStatus{
			BuildID: buildID, AccountID: accountID,
			Status: "failed", Attempt: maxAttempts, MaxAttempts: maxAttempts,
			Error: err.Error(), RestoreOnExit: true,
		})
		return
	}
	a.deleteSkinSnapshot(accountID, buildID)
	a.skinRestoreTracker.clear(buildID, accountID)
	a.emitSkinRestore(SkinRestoreStatus{
		BuildID: buildID, AccountID: accountID,
		Status: "done", Attempt: 0, MaxAttempts: maxAttempts, RestoreOnExit: true,
	})
}

// restoreStaleBuildSkins викликається ПРИ СТАРТІ лаунчера (після
// restoreRunningGame): повертає скіни для снапшотів, чиї ігри вже
// завершились, поки лаунчер був мертвий (крах/перезапуск/вимкнення ПК).
// Снапшоти живих сесій не чіпаються — їх поверне exit-хендлер.
func (a *App) restoreStaleBuildSkins() {
	entries, err := os.ReadDir(a.skinSnapshotsDir())
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(a.skinSnapshotsDir(), e.Name()))
		if err != nil {
			continue
		}
		var snap buildSkinSnapshot
		if err := json.Unmarshal(data, &snap); err != nil {
			continue
		}
		// Гра ще жива (сесія відновлена) — поверненням займеться
		// exit-хендлер; снапшот не чіпаємо.
		if a.sessionsM.IsRunning(snap.AccountID, snap.BuildID) {
			continue
		}
		// Акаунт видалено — снапшот повернути неможливо, прибираємо, щоб
		// не накопичувався і не ретраївся на кожному старті.
		if _, ok := a.cfg.GetAccountByID(snap.AccountID); !ok {
			a.deleteSkinSnapshot(snap.AccountID, snap.BuildID)
			continue
		}
		a.restoreBuildSkinAfterExit(snap.BuildID, snap.AccountID)
	}
}

// errSessionRestarted — сигнальна помилка для restore: сесія тієї самої
// (акаунт, збірка) знову запущена, старий restore має зупинитись без
// видалення снапшота.
var errSessionRestarted = fmt.Errorf("build skin: session restarted")

// ── Статус повернення для UI ────────────────────────────────────────────
// Фронтенд показує користувачу, що відбувається зі скіном після зупинки
// збірки: повернення в процесі (спроба X/6), вдалось, або не вдалось
// (повториться при наступному запуску лаунчера).

// SkinRestoreStatus — стан повернення скіна для конкретної (акаунт, збірка).
type SkinRestoreStatus struct {
	BuildID   string `json:"buildId"`
	AccountID string `json:"accountId"`
	// Status: "none" | "applied" | "restoring" | "done" | "failed" | "pending"
	//   none      — скіна збірки немає (ні снапшота, ні процесу)
	//   applied   — пресет застосовано, гра йде; повернення буде після зупинки
	//   restoring — гра зупинена, повернення в процесі
	//   done      — повернення успішне (снапшот видалено)
	//   failed    — повернення не вдалось, снапшот лишився (повториться)
	//   pending   — гра зупинена, снапшот є, але restore ще не стартував
	//               (напр. лаунчер щойно запустився, черга на старт)
	Status      string `json:"status"`
	Attempt     int    `json:"attempt"`
	MaxAttempts int    `json:"maxAttempts"`
	Error       string `json:"error,omitempty"`
	// RestoreOnExit — чи увімкнене повернення зараз (для виведення підказки
	// «повернення вимкнене — скін лишиться зміненим»).
	RestoreOnExit bool `json:"restoreOnExit"`
}

// skinRestoreTracker тримає ПОТОЧНИЙ стан повернень у пам'яті (щоб
// фронтенд бачив «restoring… спроба 2/6» навіть якщо сторінку збірки
// відкрили посеред процесу). Ключ — buildID + "|" + accountID.
type skinRestoreTracker struct {
	mu sync.Mutex
	on map[string]SkinRestoreStatus
}

func newSkinRestoreTracker() *skinRestoreTracker {
	return &skinRestoreTracker{on: map[string]SkinRestoreStatus{}}
}

func (t *skinRestoreTracker) key(buildID, accountID string) string {
	return buildID + "|" + accountID
}

func (t *skinRestoreTracker) set(s SkinRestoreStatus) {
	t.mu.Lock()
	t.on[t.key(s.BuildID, s.AccountID)] = s
	t.mu.Unlock()
}

// emitSkinRestore — оновлює трекер і шле подію skin:restore на фронтенд
// (сторінка збірки показує статус у блоці «Скін збірки»).
func (a *App) emitSkinRestore(s SkinRestoreStatus) {
	a.skinRestoreTracker.set(s)
	a.emit("skin:restore", s)
}

func (t *skinRestoreTracker) clear(buildID, accountID string) {
	t.mu.Lock()
	delete(t.on, t.key(buildID, accountID))
	t.mu.Unlock()
}

func (t *skinRestoreTracker) get(buildID, accountID string) (SkinRestoreStatus, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s, ok := t.on[t.key(buildID, accountID)]
	return s, ok
}

// GetBuildSkinRestoreStatus повертає стан повернення скіна для збірки
// (для панелі в розділі «Гра»). Фронтенд викликає при відкритті сторінки
// і при отриманні події skin:restore.
func (a *App) GetBuildSkinRestoreStatus(buildID, accountID string) SkinRestoreStatus {
	icfg := a.cfg.GetInstanceConfig(buildID)
	restoreOn := skinRestoreEnabled(icfg)

	// Активний процес повернення (restoring/failed) — живий стан з пам'яті.
	if s, ok := a.skinRestoreTracker.get(buildID, accountID); ok {
		s.RestoreOnExit = restoreOn
		return s
	}

	// Снапшот на диску = пресет був застосований і повернення ще не
	// завершене (або користувач вимкнув повернення — тоді лишиться).
	if _, err := a.loadSkinSnapshot(accountID, buildID); err == nil {
		if a.sessionsM.IsRunning(accountID, buildID) {
			return SkinRestoreStatus{Status: "applied", RestoreOnExit: restoreOn}
		}
		// Гра зупинена, снапшот ще є — повернення попереду (черга на старті
		// лаунчера) або не вдалось минулого разу; відрізнити без трекера
		// не можна, показуємо чесний pending.
		return SkinRestoreStatus{Status: "pending", RestoreOnExit: restoreOn}
	}

	// Снапшота немає і повернення зараз не йде — показувати нічого.
	// («done» приходить лише подією skin:restore одразу після реального
	// повернення; виводити його з конфігу не можна — банер з'являвся б
	// при кожному відкритті сторінки, навіть без жодного запуску.)
	return SkinRestoreStatus{Status: "none", RestoreOnExit: restoreOn}
}

// withSkinRetries повторює операцію з бек-офом: 1-ша спроба одразу, далі
// base*2^n. Спеціально для Mojang API (скин-аплоад) — тимчасові 429/5xx
// і мережеві збої не повинні вбивати «повернення на 100%».
func withSkinRetries(attempts int, base time.Duration, fn func() error) error {
	var err error
	for i := 0; i < attempts; i++ {
		if err = fn(); err == nil {
			return nil
		}
		if i < attempts-1 {
			time.Sleep(base * time.Duration(1<<uint(i)))
		}
	}
	return err
}
