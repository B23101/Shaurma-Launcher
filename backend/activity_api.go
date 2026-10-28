package main

// activity_api.go — усі довгі операції лаунчера в одному місці.
//
// Кожна операція й так шле власну подію прогресу (share:in, server:install-
// progress, migrate:progress…), на яку підписані її сторінки. Щоб не
// дублювати звіти в десятках місць, App.emit передає кожну подію в
// observeActivity — той перекладає відомі події в реєстр internal/activity.
// Завантаження збірок живуть у dlTransient (як і раніше) і додаються до
// списку при читанні. Зі зведеного списку малюється прогрес у панелі
// завдань і розсилається activity:changed для індикатора у верхній панелі.

import (
	"strings"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/activity"
	"shaurma-launcher-backend/internal/launchermigrate"
)

// Види операцій (фронтенд мапить їх на іконки й підписи activity.kind.*).
const (
	actDownload      = "download"
	actShareIn       = "share-in"
	actShareOut      = "share-out"
	actServerInstall = "server-install"
	actServerBackup  = "server-backup"
	actLaunchPrep    = "launch"
	actPackInstall   = "pack-install"
	actModsMigrate   = "mods-migrate"
	actMigrate       = "migrate"
	actDataMigration = "data-migration"
	actDuplicate     = "duplicate"
	actExport        = "export"
	actRepair        = "repair"
	actModSources    = "mod-sources"
	actBundleExport  = "bundle-export"
)

// activityEmitDelay — розсилка activity:changed не частіше за цей інтервал:
// прогрес-події йдуть десятками на секунду, а COM-виклик таскбару не безкоштовний.
const activityEmitDelay = 250 * time.Millisecond

var (
	activities = activity.New()

	activityEmitMu     sync.Mutex
	activityEmitTimer  *time.Timer // запланована розсилка (nil — немає)
	activityLingerTime *time.Timer // перемальовка, коли зникне завершена операція
)

// ListActivities — поточні довгі операції (активні та щойно завершені).
func (a *App) ListActivities() []activity.Activity {
	list, _ := a.activitySnapshot()
	return list
}

// activitySnapshot — завантаження збірок (dlTransient) + реєстр.
func (a *App) activitySnapshot() ([]activity.Activity, time.Duration) {
	type dl struct {
		id      string
		percent int
		mode    string
		stage   string
	}
	a.dlTransientMu.Lock()
	dls := make([]dl, 0, len(a.dlTransient))
	for id, p := range a.dlTransient {
		if p != nil {
			dls = append(dls, dl{id, p.Percent, p.Mode, p.Stage})
		}
	}
	a.dlTransientMu.Unlock()

	reg, next := activities.Snapshot()
	out := make([]activity.Activity, 0, len(dls)+len(reg))
	for _, d := range dls {
		st := activity.StateRunning
		if d.mode == "paused" {
			st = activity.StatePaused
		}
		out = append(out, activity.Activity{ID: actDownload + ":" + d.id, Kind: actDownload, Title: a.activityPackName(d.id),
			Detail: d.stage, Percent: d.percent, State: st, Target: d.id})
	}
	return append(out, reg...), next
}

// drawTaskbarLocked малює прогрес у панелі завдань за ВСІМА операціями:
// завантаження збірок (dlTransient) і реєстр activity. Викликається під
// dlTransientMu (порядок м'ютексів: dlTransientMu → м'ютекс реєстру).
func (a *App) drawTaskbarLocked() {
	var g activity.Aggregate
	for _, p := range a.dlTransient {
		if p == nil {
			continue
		}
		// % може бути 0 на старті (ще немає метаданих файлів) — «нескінченний» бар.
		pct := p.Percent
		if pct <= 0 {
			pct = -1
		}
		g.Add(p.Mode, pct)
	}
	reg, _ := activities.Snapshot()
	for _, it := range reg {
		g.Add(it.State, it.Percent)
	}
	switch {
	case g.Running && g.RunningPercent < 0:
		updateTaskbarProgress(-1, taskbarIndeterminate)
	case g.Running:
		updateTaskbarProgress(g.RunningPercent, taskbarNormal)
	case g.Paused:
		updateTaskbarProgress(g.PausedPercent, taskbarPaused)
	case g.Error:
		updateTaskbarProgress(100, taskbarError)
	default:
		updateTaskbarProgress(0, taskbarNone)
	}
}

// activityNotify планує перемальовку таскбару й розсилку activity:changed
// (злиття частих оновлень в одне за activityEmitDelay).
func (a *App) activityNotify() {
	activityEmitMu.Lock()
	defer activityEmitMu.Unlock()
	if activityEmitTimer != nil {
		return
	}
	activityEmitTimer = time.AfterFunc(activityEmitDelay, a.flushActivity)
}

func (a *App) flushActivity() {
	activityEmitMu.Lock()
	activityEmitTimer = nil
	activityEmitMu.Unlock()

	list, next := a.activitySnapshot()
	a.dlTransientMu.Lock()
	a.drawTaskbarLocked()
	a.dlTransientMu.Unlock()
	a.emit("activity:changed", list)

	// Завершена операція зникає через activity.Linger — тоді треба ще раз
	// перемалювати список і таскбар (червоний бар помилки теж гасне).
	if next > 0 {
		activityEmitMu.Lock()
		if activityLingerTime != nil {
			activityLingerTime.Stop()
		}
		activityLingerTime = time.AfterFunc(next+50*time.Millisecond, a.activityNotify)
		activityEmitMu.Unlock()
	}
}

func (a *App) actUpdate(it activity.Activity) {
	if activities.Update(it) {
		a.activityNotify()
	}
}

func (a *App) actFinish(id, errText string) {
	if activities.Finish(id, errText) {
		a.activityNotify()
	}
}

func (a *App) actRemove(id string) {
	if activities.Remove(id) {
		a.activityNotify()
	}
}

// actTitle повертає назву лише для нової операції — пошук назви (диск,
// індекс збірок) не повторюється на кожну подію прогресу.
func actTitle(id string, lookup func() string) string {
	if activities.Has(id) {
		return ""
	}
	return lookup()
}

func (a *App) activityPackName(packID string) string {
	if packID == "" {
		return ""
	}
	if a.customPacks != nil {
		if p, ok := a.customPacks.Get(packID); ok && p.Name != "" {
			return p.Name
		}
	}
	if e, err := a.GetPackByID(packID); err == nil && e != nil && e.Name != "" {
		return e.Name
	}
	return packID
}

func (a *App) activityServerName(serverID string) string {
	if a.srvAPI != nil {
		if v, err := a.srvAPI.GetServer(serverID); err == nil && v.Name != "" {
			return v.Name
		}
	}
	return serverID
}

// observeActivity перекладає подію прогресу в реєстр операцій. Викликається
// з App.emit для КОЖНОЇ події, тож невідомі назви відсікаються одразу.
func (a *App) observeActivity(name string, payload any) {
	switch name {
	case "share:in", "share:out":
		a.observeShare(name, payload)
	case "share:out-closed":
		if sid, ok := payload.(string); ok {
			a.actFinish(actShareOut+":"+sid, "")
		}
	case "server:install-progress":
		m, _ := payload.(map[string]any)
		sid := mapStr(m, "serverId")
		if sid == "" {
			return
		}
		id := actServerInstall + ":" + sid
		a.actUpdate(activity.Activity{ID: id, Kind: actServerInstall, Target: sid, Percent: mapInt(m, "percent"),
			Detail: mapStr(m, "message"), Title: actTitle(id, func() string { return a.activityServerName(sid) })})
	case "server:status":
		// Встановлення ядра не має власної події завершення: його кінець —
		// перший статус сервера, відмінний від installing.
		m, _ := payload.(map[string]any)
		sid := mapStr(m, "serverId")
		st := mapStr(m, "status")
		if sid == "" || st == "installing" {
			return
		}
		errText := ""
		if st == "error" {
			errText = firstNonEmpty(mapStr(m, "lastError"), "error")
		}
		a.actFinish(actServerInstall+":"+sid, errText)
	case "server:backup-progress":
		m, _ := payload.(map[string]any)
		sid := mapStr(m, "serverId")
		id := actServerBackup + ":" + sid + ":" + mapStr(m, "world")
		switch mapStr(m, "stage") {
		case "done":
			a.actFinish(id, "")
		case "error":
			a.actFinish(id, firstNonEmpty(mapStr(m, "error"), "error"))
		default:
			a.actUpdate(activity.Activity{ID: id, Kind: actServerBackup, Target: sid, Percent: mapInt(m, "percent"),
				Detail: mapStr(m, "world"), Title: actTitle(id, func() string { return a.activityServerName(sid) })})
		}
	case "launch:progress":
		// Підготовка до запуску: Java і лоадер можуть качатись хвилинами.
		// Синхронізація файлів (worker_update) уже є завантаженням збірки.
		p, ok := payload.(LaunchProgress)
		if !ok || p.InstanceID == "" {
			return
		}
		id := actLaunchPrep + ":" + p.InstanceID
		if !p.Done && p.Error == "" && (p.Stage == LaunchStageJava || p.Stage == LaunchStageLoader) {
			a.actUpdate(activity.Activity{ID: id, Kind: actLaunchPrep, Target: p.InstanceID, Percent: p.Percent,
				Detail: p.Message, Title: actTitle(id, func() string { return a.activityPackName(p.InstanceID) })})
		} else if p.Warning == "" {
			// Помилку запуску показує сам екран запуску — тут лише прибираємо рядок.
			a.actRemove(id)
		}
	case "installer:progress":
		// Фонове встановлення компонентів нової збірки (Java, лоадер, клієнт).
		m, ok := payload.(map[string]any)
		if !ok {
			return
		}
		pid := mapStr(m, "packId")
		if pid == "" {
			return
		}
		id := actPackInstall + ":" + pid
		a.actUpdate(activity.Activity{ID: id, Kind: actPackInstall, Target: pid, Percent: mapInt(m, "percent"),
			Detail: mapStr(m, "message"), Title: actTitle(id, func() string { return a.activityPackName(pid) })})
	case "installer:done":
		if pid, ok := payload.(string); ok {
			a.actFinish(actPackInstall+":"+pid, "")
		}
	case "download:error":
		m, _ := payload.(map[string]any)
		if pid := mapStr(m, "packId"); pid != "" {
			a.actFinish(actPackInstall+":"+pid, firstNonEmpty(mapStr(m, "message"), "error"))
		}
	case "mods:migrate-progress":
		m, _ := payload.(map[string]any)
		pid := mapStr(m, "packId")
		a.actCounted(actModsMigrate, pid, mapInt(m, "current"), mapInt(m, "total"), mapStr(m, "name"), "")
	case "migrate:progress":
		p, ok := payload.(launchermigrate.ImportProgress)
		if !ok {
			return
		}
		id := actMigrate + ":" + p.Launcher + ":" + p.PackID
		switch p.Stage {
		case "done":
			a.actFinish(id, "")
		case "error":
			a.actFinish(id, firstNonEmpty(p.Error, "error"))
		default:
			a.actUpdate(activity.Activity{ID: id, Kind: actMigrate, Title: p.PackName, Target: p.NewPackID,
				Percent: pctOf(p.FilesDone, p.FilesTotal), Detail: p.Launcher})
		}
	case "migration:progress":
		p, ok := payload.(MigrationProgress)
		if !ok {
			return
		}
		switch p.Stage {
		case "done":
			a.actFinish(actDataMigration, "")
		case "error":
			a.actFinish(actDataMigration, firstNonEmpty(p.Error, "error"))
		default:
			pct := int(p.Percent)
			if p.Stage == "preparing" {
				pct = -1
			}
			a.actUpdate(activity.Activity{ID: actDataMigration, Kind: actDataMigration, Percent: pct, Detail: p.Current})
		}
	case "duplicate:progress":
		p, ok := payload.(DuplicateProgress)
		if !ok {
			return
		}
		if p.Stage == "done" {
			a.actFinish(actDuplicate, "")
			return
		}
		a.actUpdate(activity.Activity{ID: actDuplicate, Kind: actDuplicate, Percent: pctOf(p.Current, p.Total), Detail: p.FileName})
	case "export:progress":
		m, _ := payload.(map[string]any)
		pid := mapStr(m, "packId")
		id := actExport + ":" + pid
		stage := mapStr(m, "stage")
		cur, total := mapInt(m, "current"), mapInt(m, "total")
		// Експорт .shaurmpack не шле stage=done — кінець там, де пораховано все.
		if stage == "done" || (stage == "pack" && total > 0 && cur >= total) {
			a.actFinish(id, "")
			return
		}
		pct := pctOf(cur, total)
		if _, has := m["percent"]; has && mapInt(m, "percent") > 0 {
			pct = mapInt(m, "percent")
		}
		a.actUpdate(activity.Activity{ID: id, Kind: actExport, Target: pid, Percent: pct,
			Detail: firstNonEmpty(mapStr(m, "modName"), mapStr(m, "fileName")),
			Title:  actTitle(id, func() string { return a.activityPackName(pid) })})
	case "export:done":
		m, _ := payload.(map[string]any)
		a.actFinish(actExport+":"+mapStr(m, "packId"), "")
	case "export:error":
		if m, ok := payload.(map[string]string); ok {
			a.actFinish(actExport+":"+m["packId"], firstNonEmpty(m["message"], "error"))
		}
	case "mods:migrate-done":
		m, _ := payload.(map[string]any)
		a.actFinish(actModsMigrate+":"+mapStr(m, "packId"), "")
	case "repair:progress":
		m, _ := payload.(map[string]any)
		a.actCounted(actRepair, mapStr(m, "packId"), mapInt(m, "done"), mapInt(m, "total"), mapStr(m, "label"), "")
	case "mods:sources:progress":
		m, _ := payload.(map[string]any)
		a.actCounted(actModSources, mapStr(m, "packId"), mapInt(m, "done"), mapInt(m, "total"), mapStr(m, "file"), "")
	case "shaurmbundle:export:progress":
		m, _ := payload.(map[string]any)
		a.actCounted(actBundleExport, "", mapInt(m, "done"), mapInt(m, "total"), "", mapStr(m, "outPath"))
	}
}

// actCounted — операція «done з total» по збірці: завершується, коли
// пораховано все. title — явна назва (інакше назва збірки).
func (a *App) actCounted(kind, packID string, done, total int, detail, title string) {
	id := kind
	if packID != "" {
		id += ":" + packID
	}
	if total > 0 && done >= total {
		a.actFinish(id, "")
		return
	}
	if title == "" {
		title = actTitle(id, func() string { return a.activityPackName(packID) })
	}
	a.actUpdate(activity.Activity{ID: id, Kind: kind, Target: packID, Percent: pctOf(done, total), Detail: detail, Title: title})
}

func pctOf(done, total int) int {
	if total <= 0 {
		return -1
	}
	return done * 100 / total
}

func pctOf64(done, total int64) int {
	if total <= 0 {
		return -1
	}
	return int(done * 100 / total)
}

func mapStr(m map[string]any, k string) string {
	s, _ := m[k].(string)
	return strings.TrimSpace(s)
}

func mapInt(m map[string]any, k string) int {
	switch v := m[k].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case int32:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}
