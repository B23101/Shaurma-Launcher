package main

import (
	"shaurma-launcher-backend/internal/crashguard"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/atomicfile"
	"shaurma-launcher-backend/internal/builds"
	"shaurma-launcher-backend/internal/custompack"
)

// ── Авто-збірки «Vanilla — актуальна версія» / «Snapshot — нова версія» ──
//
// Дві особливі кастомні збірки, які створюються автоматично ПІСЛЯ того, як
// користувач уперше створив збірку в майстрі (коли в «Кастомних» ще нічого
// нема). Вони завжди «стежать» за найновішою версією Minecraft:
//
//   - release-збірка — за останнім стабільним релізом;
//   - snapshot-збірка — за найновішою версією типу snapshot за версією
//     Mojang-маніфесту (latest.snapshot).
//
// ІСТОРІЯ БАГУ («авто-снапшот залип на піврічної давнини»). Раніше
// snapshot-збірка шукала перший запис типу snapshot, який підходив під
// regex тижневих снапшотів `^[0-9]{2}w[0-9]{2}` (24w40a). Mojang у 2026
// змінив схему назв на «<major>.<minor>-snapshot-N» / «-pre-N» / «-rc-N»
// (26.3-snapshot-10, 26.3-pre-3, 26.3-rc-3), і жодна сучасна версія під
// старий шаблон уже не підходить. Манифест відсортований від найновішого,
// тож єдиний збіг лишався далеко вглибині списку — на ОСТАННЬОМУ тижневому
// снапшоті, який Mojang випускав до зміни схеми. Звідси «пів року тому»
// (а нові версії просто не потрапляли у вибір).
//
// Тепер беремо latest.snapshot з маніфесту — те саме, що офіційний лаунчер
// показує як «останній снапшот». Mojang віддає типом «snapshot» і звичайні
// снапшоти, і pre-release, і release-candidate, тож одна авто-збірка
// auto-snapshot веде за найновішою ВЗАГАЛІ передрелізною версією
// (26.3-rc-3), а стабільна auto-vanilla — окремо за релізами.
//
// Коли Mojang випускає нову версію — конфіг збірки оновлюється сам
// (MCVersion), а картка показує стан «є нова версія → Завантажити» (файли
// гри качаються по кнопці, RefreshAutoPackFiles). Збірки можна вимкнути
// або видалити в налаштуваннях лаунчера.
//
// Стан живе в .autopacks.json біля даних лаунчера: запис про створення
// (CreatedOnce), ID/назва кожної збірки, прапорець enabled та остання
// відома цільова версія (для статусу «є нова версія» без повторних
// мережевих запитів на кожен перелік збірок).

type autoPackKind string

const (
	autoKindRelease  autoPackKind = "release"
	autoKindSnapshot autoPackKind = "snapshot"
)

// Власні назви авто-збірок — видно на картці/в сайдбарі/у налаштуваннях.
// Користувач просить саме такі: auto-vanilla (стежить за релізами) та
// auto-snapshot (стежить за чистими тижневими снапшотами).
const autoNameRelease = "auto-vanilla"
const autoNameSnapshot = "auto-snapshot"

type autoPackRecord struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	// Target — остання версія, яку авто-збірка має «наздогнати» (записана
	// під час перевірки оновлень; порожня — перевірка ще не вдавалась).
	Target string `json:"target,omitempty"`
}

type autoPacksFile struct {
	CreatedOnce bool           `json:"createdOnce"`
	Release     autoPackRecord `json:"release"`
	Snapshot    autoPackRecord `json:"snapshot"`
}

// autoPacksView — рядок для розділу налаштувань «Авто-збірки».
type autoPacksView struct {
	Kind          string `json:"kind"` // release | snapshot
	ID            string `json:"id"`
	Name          string `json:"name"`
	Enabled       bool   `json:"enabled"`
	Created       bool   `json:"created"`
	MCVersion     string `json:"mcVersion,omitempty"`
	Loader        string `json:"loader,omitempty"`
	IconURL       string `json:"iconUrl,omitempty"`
	Icon          string `json:"icon,omitempty"`
	Color         string `json:"color,omitempty"`
	TargetVersion string `json:"targetVersion,omitempty"`
	// HasNewVersion — конфіг збірки ще на старій версії (оновлення чекає
	// на кнопку «Завантажити»).
	HasNewVersion bool `json:"hasNewVersion"`
}

// ── Кеш Mojang-маніфесту (release + найновіший snapshot) ──────────────
var autoManifestCache struct {
	mu      sync.Mutex
	at      time.Time
	release string
	snap    string
}

const autoManifestTTL = 4 * time.Hour
const mojangManifestURL = "https://launchermeta.mojang.com/mc/game/version_manifest_v2.json"

type autoVersionManifest struct {
	// Latest — «офіційний» поточний стан: саме ці два рядки показує
	// лаунчер Mojang, тому саме їм і довіряємо (список versions лишається
	// фолбеком для старих/неповних відповідей і для випадку, коли latest
	// чомусь порожній).
	Latest struct {
		Release  string `json:"release"`
		Snapshot string `json:"snapshot"`
	} `json:"latest"`
	Versions []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
	} `json:"versions"`
}

// fetchAutoVersions повертає (останній release, найновіший snapshot) з
// офіційного Mojang-маніфесту з кешем на autoManifestTTL.
func fetchAutoVersions() (release, snapshot string, err error) {
	autoManifestCache.mu.Lock()
	defer autoManifestCache.mu.Unlock()
	if time.Since(autoManifestCache.at) < autoManifestTTL && autoManifestCache.release != "" {
		return autoManifestCache.release, autoManifestCache.snap, nil
	}
	client := &http.Client{Timeout: 20 * time.Second}
	req, err := http.NewRequest(http.MethodGet, mojangManifestURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "shaurma-team/shaurma-launcher/2.0 (autopacks)")
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("Mojang manifest: HTTP %d", resp.StatusCode)
	}
	var m autoVersionManifest
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return "", "", err
	}
	release, snapshot = m.Latest.Release, m.Latest.Snapshot
	// Фолбек: якщо latest у відповіді нема/порожній — беремо перший запис
	// відповідного типу зі списку (він відсортований від найновішого).
	// Тип «snapshot» у Mojang означає і звичайні снапшоти, і pre-release, і
	// release-candidate — навмисно НЕ фільтруемо їх: auto-snapshot веде за
	// найновішою передрелізною версією загалом.
	for _, v := range m.Versions {
		switch v.Type {
		case "release":
			if release == "" {
				release = v.ID
			}
		case "snapshot":
			if snapshot == "" {
				snapshot = v.ID
			}
		}
	}
	if release == "" {
		return "", "", fmt.Errorf("у Mojang-маніфесті немає жодного релізу")
	}
	// snapshot може бути порожнім (Mojang прибирає latest.snapshot у
	// вікні між циклами) — це не помилка: snapshot-авто-збірку тоді просто
	// не створюємо/не рухаємо (див. виклики: `snapshot != ""`).
	autoManifestCache.release = release
	autoManifestCache.snap = snapshot
	autoManifestCache.at = time.Now()
	return release, snapshot, nil
}

// ── App-сторони ──────────────────────────────────────────────────────────

func (a *App) autoPacksPath() string {
	return filepath.Join(a.cfg.Dir(), "autopacks.json")
}

func (a *App) autoPacksLocked() *autoPacksFile {
	if a.autoPacksData != nil {
		return a.autoPacksData
	}
	d := &autoPacksFile{}
	if data, err := os.ReadFile(a.autoPacksPath()); err == nil {
		_ = json.Unmarshal(data, d)
	}
	a.autoPacksData = d
	return d
}

func (a *App) saveAutoPacksLocked() {
	if a.autoPacksData == nil {
		return
	}
	_ = atomicfile.WriteJSONAtomic(a.autoPacksPath(), a.autoPacksData)
}

func (a *App) autoPacks() *autoPacksFile {
	a.autoPacksMu.Lock()
	defer a.autoPacksMu.Unlock()
	return a.autoPacksLocked()
}

func (a *App) recordForKind(kind autoPackKind) *autoPackRecord {
	d := a.autoPacks()
	switch kind {
	case autoKindRelease:
		return &d.Release
	case autoKindSnapshot:
		return &d.Snapshot
	}
	return nil
}

// isAutoPackID — чи packID належить одній з авто-збірок (для фільтрації
// списків та логіки «оновлення версії»).
func (a *App) isAutoPackID(packID string) (autoPackKind, bool) {
	d := a.autoPacks()
	if packID != "" {
		if d.Release.ID == packID {
			return autoKindRelease, true
		}
		if d.Snapshot.ID == packID {
			return autoKindSnapshot, true
		}
	}
	return "", false
}

// autoEnabled — чи увімкнена авто-збірка (існує, не видалена, enabled).
func (a *App) autoEnabled(kind autoPackKind) bool {
	rec := a.recordForKind(kind)
	return rec != nil && rec.ID != "" && rec.Enabled
}

// EnsureAutoVersionPacks створює ті авто-збірки, яких ще немає (після
// завершення майстра ПЕРШОЇ збірки — викликається один раз). Іконки
// передаються з фронтенду як data-URL PNG (картинки vanilla_icon.png /
// shaphot_icon.png зібрані у фронтенд-бандл — бекенд їх фізично не має).
// Запис у d.CreatedOnce лише розмежовує «сам після майстра» від ручного
// вмикання в налаштуваннях (SetAutoPackEnabled) — там кожен тумблер сам
// реєструє свою збірку.
func (a *App) EnsureAutoVersionPacks(vanillaIconDataURL, snapshotIconDataURL string, force bool) ([]autoPacksView, error) {
	a.autoPacksMu.Lock()
	defer a.autoPacksMu.Unlock()
	d := a.autoPacksLocked()
	if d.CreatedOnce && !force {
		return a.autoPacksViewsLocked(), nil
	}
	// Тільки коли в користувача вже є хоч одна кастомна збірка (це
	// «після майстра»), а не порожньому списку.
	if len(a.customPacks.List()) == 0 {
		return nil, fmt.Errorf("авто-збірки створюються після першої збірки користувача")
	}
	release, snapshot, err := fetchAutoVersions()
	if err != nil {
		return nil, fmt.Errorf("не вдалось отримати актуальні версії Minecraft: %w", err)
	}

	d.CreatedOnce = true
	// Створюємо лише те, чого ще немає (після видалення однієї збірки
	// друга не дублюється). Release — завжди, snapshot — лише якщо зараз
	// є тижневий снапшот.
	if d.Release.ID == "" {
		if rec, perr := a.createAutoPackLocked(autoKindRelease, autoNameRelease, release, vanillaIconDataURL); perr != nil {
			return nil, perr
		} else {
			d.Release = rec
		}
	}
	if d.Snapshot.ID == "" && snapshot != "" {
		if rec, perr := a.createAutoPackLocked(autoKindSnapshot, autoNameSnapshot, snapshot, snapshotIconDataURL); perr != nil {
			return nil, perr
		} else {
			d.Snapshot = rec
		}
	}
	a.saveAutoPacksLocked()
	// Іконки/стан у «Моїх збірках» одразу.
	if d.Release.ID != "" {
		a.emit("builds:changed", d.Release.ID)
	}
	if d.Snapshot.ID != "" {
		a.emit("builds:changed", d.Snapshot.ID)
	}
	return a.autoPacksViewsLocked(), nil
}

func (a *App) autoPacksViewsLocked() []autoPacksView {
	return []autoPacksView{
		a.autoPacksViewOfLocked(autoKindRelease),
		a.autoPacksViewOfLocked(autoKindSnapshot),
	}
}

// createAutoPackLocked створює одну авто-збірку. Викликається під
// autoPacksMu. Іконка (PNG data-URL) пишеться в теку збірки як icon.png
// та фіксується в записі збірки (IconPath) — той самий спосіб, яким
// лаунчер знаходить кастомні іконки.
func (a *App) createAutoPackLocked(kind autoPackKind, name, mcVersion, iconDataURL string) (autoPackRecord, error) {
	rec := autoPackRecord{Name: name, Enabled: true, Target: mcVersion}
	if a.customPacks == nil {
		return rec, fmt.Errorf("сховище збірок не ініціалізовано")
	}
	// Іконка (vanilla_icon.png / shaphot_icon.png) приходить з фронтенду як
	// data-URL PNG — зберігаємо байти, щоб після Create покласти їх у теку
	// збірки як icon.png і прописати в записі.
	var iconBytes []byte
	if strings.HasPrefix(iconDataURL, "data:image/") {
		if idx := strings.IndexByte(iconDataURL, ','); idx >= 0 {
			if raw, derr := base64.StdEncoding.DecodeString(iconDataURL[idx+1:]); derr == nil && len(raw) > 0 {
				iconBytes = raw
			}
		}
	}
	params := custompack.CreateParams{
		Name:      name,
		MCVersion: mcVersion,
		Loader:    custompack.LoaderVanilla,
	}
	pack, err := custompack.Create(a.customPacks, a.cfg.InstancesDir(), params)
	if err != nil {
		return rec, err
	}
	// Іконка гарантовано лежить у теці збірки і прописана в записі — тоді
	// картки «Моїх збірок»/сторінка показують PNG користувача.
	if len(iconBytes) > 0 {
		iconAbs := filepath.Join(a.packDir(pack.ID, string(builds.KindCustom)), "icon.png")
		if werr := os.WriteFile(iconAbs, iconBytes, 0644); werr == nil {
			pack.IconPath = "icon.png"
			_ = a.customPacks.Save(pack)
		}
	}
	rec.ID = pack.ID
	// Збірка вважається «встановленою» (компоненти з'являться при першому
	// запуску/оновленні), версія — та, під яку створена: поки не вийде нова
	// версія, картка показує «Готова».
	a.buildsReg.SetInstalled(pack.ID, builds.KindCustom, mcVersion)
	return rec, nil
}

func (a *App) autoPacksViewOfLocked(kind autoPackKind) autoPacksView {
	d := a.autoPacksData
	if d == nil {
		return autoPacksView{}
	}
	rec := &d.Release
	name := autoNameRelease
	if kind == autoKindSnapshot {
		rec = &d.Snapshot
		name = autoNameSnapshot
	}
	v := autoPacksView{Kind: string(kind), ID: rec.ID, Name: rec.Name, Enabled: rec.Enabled,
		TargetVersion: rec.Target, Created: rec.ID != ""}
	// Ще не зареєстрована збірка (ID порожній) — показуємо її власну назву,
	// щоб рядок у налаштуваннях був зрозумілий ще до створення.
	if rec.Name == "" {
		v.Name = name
	}
	if rec.ID != "" {
		if cp, ok := a.customPacks.Get(rec.ID); ok {
			v.MCVersion = cp.MCVersion
			v.Loader = string(cp.Loader)
			v.Icon = cp.Icon
			v.Color = cp.Color
			if cp.IconPath != "" {
				v.IconURL = a.packAssetURL(cp, cp.IconPath)
			}
		}
		v.HasNewVersion = rec.Target != "" && v.MCVersion != "" && rec.Target != v.MCVersion
	}
	return v
}

// GetAutoPacks повертає стан обох авто-збірок для розділу налаштувань.
// Перед відповіддю запускає фонову перевірку нових версій (якщо дані
// застаріли) — так розділ завжди показує актуальний стан без окремого
// виклику, а на «Мої збірки» статус оновлення приїжджає подією builds:changed.
func (a *App) GetAutoPacks() []autoPacksView {
	a.maybeCheckAutoVersions()
	a.autoPacksMu.Lock()
	defer a.autoPacksMu.Unlock()
	a.autoPacksLocked()
	// Завжди обидва рядки (auto-vanilla / auto-snapshot), навіть якщо ще
	// не створені — щоб у налаштуваннях їх можна було ввімкнути й
	// зареєструвати (див. SetAutoPackEnabled).
	return []autoPacksView{
		a.autoPacksViewOfLocked(autoKindRelease),
		a.autoPacksViewOfLocked(autoKindSnapshot),
	}
}

// SetAutoPackEnabled вмикає/вимикає авто-збірку. Якщо збірку ще НЕ
// створено (користувач не проходив перший майстер або видалив її раніше) —
// ввімкнення тумблера РЕЄСТРУЄ її: створює запис (custom pack з конфігом
// під актуальну версію Mojang). Фізичних файлів гри при цьому немає —
// це авто-збірка, вона реєструється в налаштуваннях лаунчера, а файли
// з'являться по кнопці «Завантажити»/при запуску.
func (a *App) SetAutoPackEnabled(kind string, enabled bool, iconDataURL string) error {
	apk := autoPackKind(kind)
	if apk != autoKindRelease && apk != autoKindSnapshot {
		return fmt.Errorf("невідомий тип авто-збірки: %s", kind)
	}
	a.autoPacksMu.Lock()
	d := a.autoPacksLocked()
	var rec *autoPackRecord
	if apk == autoKindRelease {
		rec = &d.Release
	} else {
		rec = &d.Snapshot
	}
	// Вмикання ще не створеної збірки = реєстрація (створення).
	if enabled && rec.ID == "" {
		release, snapshot, err := fetchAutoVersions()
		if err != nil {
			a.autoPacksMu.Unlock()
			return fmt.Errorf("не вдалось отримати актуальні версії Minecraft: %w", err)
		}
		name := autoNameRelease
		ver := release
		if apk == autoKindSnapshot {
			if snapshot == "" {
				a.autoPacksMu.Unlock()
				return fmt.Errorf("зараз немає тижневого снапшота Minecraft")
			}
			name = autoNameSnapshot
			ver = snapshot
		}
		created, cerr := a.createAutoPackLocked(apk, name, ver, iconDataURL)
		if cerr != nil {
			a.autoPacksMu.Unlock()
			return cerr
		}
		rec.ID = created.ID
		rec.Name = created.Name
		rec.Target = created.Target
	}
	if rec.ID == "" {
		a.autoPacksMu.Unlock()
		return fmt.Errorf("авто-збірку ще не створено")
	}
	if rec.Enabled != enabled {
		rec.Enabled = enabled
		a.saveAutoPacksLocked()
	}
	id := rec.ID
	a.autoPacksMu.Unlock()
	a.emit("builds:changed", id)
	return nil
}

// RefreshAutoPackFiles встановлює компоненти ПОТОЧНОЇ версії авто-збірки
// (кнопка «Завантажити»/«Оновити» на картці, коли конфіг уже перемкнувся
// на нову версію Mojang): Java + клієнт Minecraft качаються у фоні з
// прогресом installer:progress, а картка показує «оновлення». Поки йде
// качка — запуск цієї збірки заблоковано (і UI, і бекенд-гард).
func (a *App) RefreshAutoPackFiles(packID string) error {
	kind, ok := a.isAutoPackID(packID)
	if !ok {
		return fmt.Errorf("збірка %s не є авто-збіркою", packID)
	}
	if !a.autoEnabled(kind) {
		return fmt.Errorf("авто-збірку вимкнено в налаштуваннях")
	}
	cp, ok := a.customPacks.Get(packID)
	if !ok {
		return fmt.Errorf("збірку не знайдено: %s", packID)
	}
	if err := a.errPackRunning(packID); err != nil {
		return err
	}
	a.autoRefreshMu.Lock()
	if a.autoRefreshBusy[packID] {
		a.autoRefreshMu.Unlock()
		return fmt.Errorf("оновлення авто-збірки вже йде")
	}
	a.autoRefreshBusy[packID] = true
	a.autoRefreshMu.Unlock()

	// Якщо користувач сам змінив версію в налаштуваннях збірки після
	// авто-перемикання — конфіг уже містить ціль, ставимо її.
	go func() {
		defer crashguard.Recover("autopacks.go:492")
		defer func() {
			a.autoRefreshMu.Lock()
			delete(a.autoRefreshBusy, packID)
			a.autoRefreshMu.Unlock()
		}()
		installedVersion := cp.MCVersion
		a.prepareCustomPackComponents(cp, func() {
			// Компоненти нової версії готові — фіксуємо «встановлено»
			// саме під неї, щоб статус став «Готова» (не «є оновлення»).
			a.buildsReg.SetInstalled(packID, builds.KindCustom, installedVersion)
			a.emit("builds:changed", packID)
		})
	}()
	return nil
}

// maybeCheckAutoVersions — фонова перевірка нових версій для увімкнених
// авто-збірок (не частіше ніж раз на автоCheckTTL). Якщо вийшла нова
// версія — конфіг збірки оновлюється сам; статус «є нова версія →
// Завантажити» з'являється сам, бо в реєстрі записана стара версія.
func (a *App) maybeCheckAutoVersions() {
	if !a.autoCheckDue() {
		return
	}
	go func() {
		defer crashguard.Recover("a.checkAutoVersionPacks")
		a.checkAutoVersionPacks()
	}()
}

const autoCheckTTL = 6 * time.Hour

// autoCheckRetryDelay — пауза між спробами, якщо попередня НЕ дісталась
// Mojang (launchermeta недоступний). Раніше autoCheckAt ставився ДО запиту,
// тож перша ж невдала спроба (типовий випадок — лаунчер стартував раніше,
// ніж піднялась мережа) «з'їдала» весь 6-годинний TTL: ані стартова
// перевірка, ані розділ «Авто-збірки», ані повторні GetAutoPacks уже не
// робили жодного запиту — конфіг auto-vanilla/auto-snapshot лишався на
// старій версії доти, доки збірку не запустять (LaunchInstance викликає
// синхронний ensureAutoVersionSync). Тепер TTL відраховується лише від
// УСПІШНОГО циклу, а невдалі спроби повторюються раз на autoCheckRetryDelay.
const autoCheckRetryDelay = 2 * time.Minute

// autoCheckDue — чи час запускати перевірку: минув autoCheckTTL від
// останнього успіху і минув autoCheckRetryDelay від останньої спроби.
// Ставить autoCheckAttemptAt, тож конкурентні виклики (старт + GetAutoPacks)
// не запускають перевірку двічі.
func (a *App) autoCheckDue() bool {
	a.autoCheckMu.Lock()
	defer a.autoCheckMu.Unlock()
	now := time.Now()
	if !a.autoCheckAt.IsZero() && now.Sub(a.autoCheckAt) < autoCheckTTL {
		return false
	}
	if !a.autoCheckAttemptAt.IsZero() && now.Sub(a.autoCheckAttemptAt) < autoCheckRetryDelay {
		return false
	}
	a.autoCheckAttemptAt = now
	return true
}

// ensureAutoVersionsAtStartup — перевірка авто-збірок РІВНО раз при старті
// лаунчера, у фоні, незалежно від autoCheckTTL. Раніше єдиним викликачем
// checkAutoVersionPacks() був GetAutoPacks() (розділ налаштувань "Авто-
// збірки") — якщо користувач туди жодного разу не заходив, конфіг
// auto-vanilla/auto-snapshot ніколи не оновлювався: збірка назавжди
// лишалась на версії, актуальній у момент СТВОРЕННЯ, навіть коли Mojang
// вже випустив новішу. Викликається з ServiceStartup — до першого показу
// "Моїх збірок" картки вже можуть встигнути показати актуальний стан,
// а якщо мережа повільна — builds:changed прилетить, коли перевірка
// завершиться, і оновить сайдбар/сторінку без дій користувача.
// Спроба позначається через autoCheckDue: якщо на старті мережі ще немає,
// перевірка повториться через autoCheckRetryDelay (а не через 6 годин).
func (a *App) ensureAutoVersionsAtStartup() {
	go func() {
		defer crashguard.Recover("autopacks.go:564")
		for attempt := 0; attempt < autoCheckStartupAttempts; attempt++ {
			if attempt > 0 {
				// Мережа на старті лаунчера часто піднімається на кілька
				// секунд пізніше за нас — тому не здаємось після першої
				// невдачі, а пробуємо ще, з паузою autoCheckRetryDelay.
				time.Sleep(autoCheckRetryDelay)
			}
			if !a.autoCheckDue() {
				return // хтось інший уже довів перевірку до успіху
			}
			if a.checkAutoVersionPacks() {
				return
			}
		}
	}()
}

// autoCheckStartupAttempts — скільки разів стартова перевірка авто-збірок
// пробує дістатись Mojang, перш ніж здатись до наступного тригера
// (відкриття налаштувань / запуск збірки).
const autoCheckStartupAttempts = 3

// ensureAutoVersionSync — синхронна (блокуюча) перевірка та, за потреби,
// перемикання КОНКРЕТНОЇ авто-збірки на найновішу версію Mojang. На
// відміну від maybeCheckAutoVersions/ensureAutoVersionsAtStartup (фонові,
// «best effort»), ця викликається прямо перед запуском збірки
// (LaunchInstance): фонова перевірка могла ще не встигнути завершитись
// (щойно стартував лаунчер) або user запустив збірку одразу після
// пробудження ПК з вимкненим інтернетом — у будь-якому разі гравець не
// має отримати «завислу» на старій версії авто-збірку лише тому, що
// фонова перевірка не встигла чи не змогла спрацювати. Мережевий виклик
// тут дешевий — fetchAutoVersions кешує маніфест Mojang на 4 години, тож
// повторний виклик відразу після фонової перевірки не робить зайвого
// запиту. Помилки мережі тут навмисно проковтуються (offline-запуск не
// повинен ламатись через недоступність launchermeta.mojang.com — збірка
// просто запуститься на тій версії, що вже стоїть у конфігу).
func (a *App) ensureAutoVersionSync(packID string) {
	// isAutoPackID/autoEnabled самі беруть autoPacksMu (через autoPacks()) —
	// НЕ обгортаємо їх власним Lock() тут, інакше sync.Mutex (нереентерабельний)
	// одразу здедлочиться сам на собі.
	kind, ok := a.isAutoPackID(packID)
	if !ok {
		return
	}
	if !a.autoEnabled(kind) {
		return
	}
	release, snapshot, err := fetchAutoVersions()
	if err != nil {
		return // офлайн/мережева помилка — тихо лишаємось на вже встановленій версії
	}
	target := release
	if kind == autoKindSnapshot {
		if snapshot == "" {
			return
		}
		target = snapshot
	}
	a.autoPacksMu.Lock()
	defer a.autoPacksMu.Unlock()
	d := a.autoPacksLocked()
	rec := &d.Release
	if kind == autoKindSnapshot {
		rec = &d.Snapshot
	}
	if rec.ID != packID || !rec.Enabled {
		return // стан змінився, поки чекали на мережу (вимкнули/видалили) — не чіпаємо
	}
	cp, ok := a.customPacks.Get(packID)
	if !ok {
		return
	}
	rec.Target = target
	if cp.MCVersion == target {
		a.saveAutoPacksLocked()
		return
	}
	cp.MCVersion = target
	if err := a.customPacks.Save(cp); err != nil {
		return
	}
	a.saveAutoPacksLocked()
	a.emit("builds:changed", packID)
}

// checkAutoVersionPacks повертає true, якщо цикл доведено до успіху (дані
// Mojang отримано) або перевіряти нічого — саме це сигналізує стартовому
// автоповтору, що можна припинити спроби. false = немає мережі/Mojang
// недоступний, варто спробувати ще.
func (a *App) checkAutoVersionPacks() bool {
	a.autoPacksMu.Lock()
	d := a.autoPacksLocked()
	if !d.CreatedOnce {
		// Авто-збірок ще немає — перевіряти нічого (нові створяться вже
		// під актуальну версію). Це не помилка мережі: повтори не потрібні.
		a.autoPacksMu.Unlock()
		return true
	}
	changed := false
	var changedIDs []string
	release, snapshot, err := fetchAutoVersions()
	if err != nil {
		// Мережа/Mojang недоступні: TTL НЕ позначаємо успішним — інакше
		// наступна спроба (розділ «Авто-збірки», повторний GetAutoPacks)
		// мовчала б autoCheckTTL, і конфіг збірки лишався б на старій
		// версії до самого запуску гри (ensureAutoVersionSync).
		a.autoPacksMu.Unlock()
		return false
	}
	// Дістали дані Mojang — тільки тепер цикл вважається успішним
	// (autoCheckTTL відраховується від цього моменту).
	defer func() {
		a.autoCheckMu.Lock()
		a.autoCheckAt = time.Now()
		a.autoCheckMu.Unlock()
	}()
	for _, kind := range []autoPackKind{autoKindRelease, autoKindSnapshot} {
		rec := &d.Release
		if kind == autoKindSnapshot {
			rec = &d.Snapshot
		}
		if rec.ID == "" || !rec.Enabled {
			continue
		}
		cp, ok := a.customPacks.Get(rec.ID)
		if !ok {
			continue
		}
		target := release
		if kind == autoKindSnapshot {
			if snapshot == "" {
				continue
			}
			target = snapshot
		}
		rec.Target = target
		if cp.MCVersion == target {
			continue
		}
		// Нова версія Mojang: перемикаємо конфіг збірки самі. Файли гри
		// качаються по кнопці (RefreshAutoPackFiles) — див. статус картки.
		cp.MCVersion = target
		if err := a.customPacks.Save(cp); err != nil {
			continue
		}
		changed = true
		changedIDs = append(changedIDs, rec.ID)
	}
	a.saveAutoPacksLocked()
	a.autoPacksMu.Unlock()
	if changed {
		for _, id := range changedIDs {
			a.emit("builds:changed", id)
		}
	}
	return true
}

// autoFileTarget — цільова версія ФАЙЛІВ для статусу картки: для
// УВІМКНЕНИХ авто-збірок це її поточна версія в конфігу. Коли Mojang
// випускає нову версію, конфіг перемикається сам (checkAutoVersionPacks),
// а в реєстрі «встановленої версії» лишається стара — ComputeFileStatus
// тоді дає NeedsUpdate («є нова версія → Завантажити»), і лише
// RefreshAutoPackFiles після качки фіксує нову версію як встановлену.
// Для решти кастомних збірок повертає "" (вони завжди «готові», як і
// раніше — файли підхоплюються при запуску).
func (a *App) autoFileTarget(cp custompack.Pack) string {
	kind, ok := a.isAutoPackID(cp.ID)
	if !ok {
		return ""
	}
	rec := a.recordForKind(kind)
	if rec == nil || !rec.Enabled {
		return ""
	}
	return cp.MCVersion
}

// ── Сховання/видалення ──────────────────────────────────────────────────

// autoPacksDisabledSet — множина ID ВИМКНЕНИХ авто-збірок. Вимкнені в
// налаштуваннях авто-збірки ховаються зі списків «Мої збірки»/sidebar
// (див. GetBuilds), але лишаються на диску — їх можна ввімкнути назад.
func (a *App) autoPacksDisabledSet() map[string]bool {
	d := a.autoPacks()
	out := map[string]bool{}
	if d.Release.ID != "" && !d.Release.Enabled {
		out[d.Release.ID] = true
	}
	if d.Snapshot.ID != "" && !d.Snapshot.Enabled {
		out[d.Snapshot.ID] = true
	}
	return out
}

// forgetAutoPackID прибирає запис авто-збірки (після видалення збірки
// користувачем) — «видалити авто-збірку» в налаштуваннях.
func (a *App) forgetAutoPackID(packID string) {
	a.autoPacksMu.Lock()
	defer a.autoPacksMu.Unlock()
	d := a.autoPacksLocked()
	if d.Release.ID == packID {
		d.Release = autoPackRecord{}
	}
	if d.Snapshot.ID == packID {
		d.Snapshot = autoPackRecord{}
	}
	a.saveAutoPacksLocked()
}
