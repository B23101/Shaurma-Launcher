package main

// auto_repair_api.go — склейка App з трьома пакетами, портованими з Nivora:
//
//   - internal/autorepair — авто-починка (діагностика запуску + лагодження);
//   - internal/modgraph   — дерево залежностей модів збірки;
//   - internal/procmetrics — метрика CPU/RAM/диска гри.
//
// Усе нове — лише тонкий шар: рушії чисті й знають про App тільки через
// autorepair.Executor, яким тут є repairExecutor (на наявних методах App:
// ToggleMod, InstallSuggestedMod, SetPackRAM, RepairPack тощо).

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/autorepair"
	"shaurma-launcher-backend/internal/console"
	"shaurma-launcher-backend/internal/crashguard"
	"shaurma-launcher-backend/internal/custompack"
	"shaurma-launcher-backend/internal/minecraft"
	"shaurma-launcher-backend/internal/model"
	"shaurma-launcher-backend/internal/modgraph"
	"shaurma-launcher-backend/internal/procmetrics"
	"shaurma-launcher-backend/internal/sysinfo"
)

// RepairReportEvent — подія repair:report після краху (інтерфейс відкриває діалог).
type RepairReportEvent struct {
	PackID string            `json:"packId"`
	Report autorepair.Report `json:"report"`
}

// repairRuntime — живі трекери метрик (ключ — ID збірки).
type repairRuntime struct {
	mu       sync.Mutex
	trackers map[string]*procmetrics.Tracker
	cancels  map[string]context.CancelFunc
}

func (a *App) repairStateDir() string { return filepath.Join(a.cfg.Dir(), "autorepair") }

// ── діагностика ────────────────────────────────────────────────────────────

// readLogTail читає хвіст logs/latest.log збірки (до n рядків).
func readLogTail(gameDir string, n int) []string {
	f, err := os.Open(filepath.Join(gameDir, "logs", "latest.log"))
	if err != nil {
		return nil
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() > 4<<20 {
		_, _ = f.Seek(st.Size()-(4<<20), 0)
	}
	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		lines = append(lines, sc.Text())
		if len(lines) > n*2 {
			lines = lines[len(lines)-n:]
		}
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

func bufferLines(buf *console.Buffer, n int) []string {
	if buf == nil {
		return nil
	}
	snap := buf.Recent(n)
	out := make([]string, 0, len(snap))
	for _, l := range snap {
		out = append(out, l.Text)
	}
	return out
}

// buildRepairInput збирає вхід рушія з поточного стану збірки й акаунтів.
func (a *App) buildRepairInput(packID string, lines []string, errText string, exitCode *int, since time.Time, currentAccount string) (autorepair.Input, *launchBuild, error) {
	build, err := a.resolveLaunchBuild(packID)
	if err != nil {
		return autorepair.Input{}, nil, err
	}
	settings := a.cfg.GetSettings()
	icfg := a.cfg.GetInstanceConfig(packID)
	gameDir := a.gameDir(packID)
	if len(lines) == 0 {
		lines = readLogTail(gameDir, 1500)
	}
	javaPath := ""
	if build.UseSeparateJava && build.JavaPath != "" {
		javaPath = build.JavaPath
	} else if icfg.UseSeparateJava && icfg.JavaPathOverride != "" {
		javaPath = icfg.JavaPathOverride
	}
	if currentAccount == "" {
		currentAccount = a.cfg.ActiveAccount().ID
	}
	var accounts []autorepair.Account
	for _, acc := range a.cfg.GetAccounts() {
		accounts = append(accounts, autorepair.Account{
			ID: acc.ID, Name: acc.Username, Licensed: acc.Type == "microsoft" || acc.IsLicensed, Active: acc.ID == currentAccount,
		})
	}
	_, isAuto := a.isAutoPackID(packID)
	_, isCustom := a.customPacks.Get(packID)
	si := sysinfo.Collect(a.cfg.Dir())
	in := autorepair.Input{
		Lines:            lines,
		Error:            errText,
		ExitCode:         exitCode,
		GameDir:          gameDir,
		Loader:           build.Loader,
		MinecraftVersion: build.MCVersion,
		LoaderVersion:    build.LoaderVersion,
		Since:            since,
		LastSuccess:      autorepair.LastSuccess(a.repairStateDir(), packID),
		EffectiveRAMMB:   a.effectiveRAM(packID),
		TotalRAMMB:       int(si.TotalRAMMB),
		RecommendedRAMMB: build.RecommendedRAMMB,
		JavaPath:         javaPath,
		CustomJVM:        strings.TrimSpace(icfg.JavaArgsOverride) != "",
		JVMArgs:          strings.TrimSpace(icfg.JavaArgsOverride),
		DedicatedGPU:     settings.UseDedicatedGPU,
		CanUpdateLoader:  isCustom && !isAuto,
		Accounts:         accounts,
		CurrentAccountID: currentAccount,
		Muted:            autorepair.LoadMuted(a.repairStateDir()),
	}
	return in, build, nil
}

// DiagnosePack — ручна діагностика збірки («Діагностика» на сторінці збірки):
// аналізує логи останнього запуску, звіти про краш та вміст mods/.
func (a *App) DiagnosePack(packID string) (autorepair.Report, error) {
	var buf *console.Buffer
	a.consoleBufMu.Lock()
	for k, b := range a.consoleBufs {
		if k.buildID == packID {
			buf = b
		}
	}
	a.consoleBufMu.Unlock()
	in, _, err := a.buildRepairInput(packID, bufferLines(buf, 1500), "", nil, time.Time{}, "")
	if err != nil {
		return autorepair.Report{}, err
	}
	return autorepair.Diagnose(in), nil
}

// repairApplying — збірки, для яких лагодження зараз застосовуються. Вікно
// «Авто-починка» можна закрити під час роботи (воно більше не блокується),
// тож без цього повторне «Застосувати» запустило б другу копію, що видаляє
// й перекачує ті самі файли Minecraft/Java одночасно з першою.
var repairApplying sync.Map

// ApplyPackRepairs застосовує обрані користувачем лагодження.
func (a *App) ApplyPackRepairs(packID string, fixes []autorepair.Fix) ([]autorepair.FixResult, error) {
	if err := a.errPackRunning(packID); err != nil {
		return nil, err
	}
	if _, busy := repairApplying.LoadOrStore(packID, true); busy {
		return nil, errors.New("лагодження цієї збірки вже виконується — дочекайтесь завершення")
	}
	defer repairApplying.Delete(packID)
	build, err := a.resolveLaunchBuild(packID)
	if err != nil {
		return nil, err
	}
	// Подія на кожен крок: фронт показує «Крок 2 з 5: …», поки качається
	// Minecraft/лоадер/Java (це хвилини, і без прогресу модалка здається завислою).
	return autorepair.ApplyProgress(&repairExecutor{a: a, packID: packID, build: build}, fixes,
		func(done, total int, f autorepair.Fix) {
			label := strings.TrimSpace(f.Label)
			if label == "" {
				label = f.Action
			}
			a.emit("repair:progress", map[string]any{
				"packId": packID, "done": done, "total": total, "label": label,
			})
		})
}

// GetRepairIssueKinds — види проблем (для керування прихованими).
func (a *App) GetRepairIssueKinds() []autorepair.IssueKind { return autorepair.IssueKinds() }

// GetRepairMuted — види проблем, які користувач попросив не показувати.
func (a *App) GetRepairMuted() []string { return autorepair.LoadMuted(a.repairStateDir()) }

// SetRepairMuted приховує (muted=true) або повертає види проблем.
func (a *App) SetRepairMuted(kinds []string, muted bool) ([]string, error) {
	return autorepair.SetMuted(a.repairStateDir(), kinds, muted)
}

// ── хуки запуску/виходу гри ────────────────────────────────────────────────

// autoRepairTemporarilyOff — аварійний вимикач авто-починки (поставити false, щоб увімкнути).
const autoRepairTemporarilyOff = true

// onGameExitRepair викликається з exit-хендлера: фіксує успішний запуск,
// а після краху (не зупинки користувачем) у фоні діагностує й шле repair:report.
func (a *App) onGameExitRepair(build *launchBuild, account model.Account, buf *console.Buffer, code int, wasUserStop bool, startedAt time.Time) {
	if code == 0 {
		autorepair.RecordLaunchResult(a.repairStateDir(), build.ID, 0)
		return
	}
	// АВТО-ПОЧИНКА ТИМЧАСОВО ВИМКНЕНА (вікно діагностики не відкривається).
	if autoRepairTemporarilyOff || wasUserStop || a.cfg.GetSettings().DisableAutoRepair {
		return
	}
	lines := bufferLines(buf, 1500)
	go func() {
		defer crashguard.Recover("a.onGameExitRepair")
		exit := code
		in, _, err := a.buildRepairInput(build.ID, lines, "", &exit, startedAt.Add(-time.Minute), account.ID)
		if err != nil {
			return
		}
		report := autorepair.Diagnose(in)
		if len(report.Issues) > 0 {
			a.emit("repair:report", RepairReportEvent{PackID: build.ID, Report: report})
		}
	}()
}

// repairOnLaunchError — діагностика помилки, через яку гра навіть не стартувала
// (зламаний лоадер, Java тощо). Запасні «підозрюваних» тут не показуємо.
func (a *App) repairOnLaunchError(packID string, launchErr error) {
	if autoRepairTemporarilyOff || launchErr == nil || isLaunchCancelled(launchErr) || a.cfg.GetSettings().DisableAutoRepair {
		return
	}
	text := launchErr.Error()
	go func() {
		defer crashguard.Recover("a.repairOnLaunchError")
		in, _, err := a.buildRepairInput(packID, nil, text, nil, time.Time{}, "")
		if err != nil {
			return
		}
		in.Lines = nil
		report := autorepair.Diagnose(in)
		kept := report.Issues[:0]
		for _, is := range report.Issues {
			switch is.ID {
			case "unknown_crash", "recent_mods", "suspects":
				continue
			}
			kept = append(kept, is)
		}
		report.Issues = kept
		if len(kept) > 0 {
			a.emit("repair:report", RepairReportEvent{PackID: packID, Report: report})
		}
	}()
}

// ── метрика ресурсів ───────────────────────────────────────────────────────

// startGameMetrics починає семплінг дерева процесів гри.
func (a *App) startGameMetrics(packID string, pid int) {
	if pid <= 0 {
		return
	}
	rt := &a.repair
	rt.mu.Lock()
	if rt.trackers == nil {
		rt.trackers = map[string]*procmetrics.Tracker{}
		rt.cancels = map[string]context.CancelFunc{}
	}
	if c := rt.cancels[packID]; c != nil {
		c()
	}
	tr := procmetrics.New(packID, pid)
	ctx, cancel := context.WithCancel(context.Background())
	rt.trackers[packID], rt.cancels[packID] = tr, cancel
	rt.mu.Unlock()
	gameDir := a.gameDir(packID)
	go func() {
		defer crashguard.Recover("a.startGameMetrics")
		tr.SetDiskMB(procmetrics.DirSizeMB(gameDir, 200000))
		tr.Run(ctx, 2*time.Second, nil)
	}()
}

// stopGameMetrics завершує семплінг і зберігає знімок як «останню сесію».
func (a *App) stopGameMetrics(packID string) {
	rt := &a.repair
	rt.mu.Lock()
	tr := rt.trackers[packID]
	if c := rt.cancels[packID]; c != nil {
		c()
		delete(rt.cancels, packID)
	}
	rt.mu.Unlock()
	if tr == nil {
		return
	}
	procmetrics.SaveSnapshot(a.repairStateDir(), tr.Finish())
}

// GetPackMetrics — поточна (якщо гра йде) або остання збережена метрика збірки.
func (a *App) GetPackMetrics(packID string) procmetrics.Snapshot {
	rt := &a.repair
	rt.mu.Lock()
	tr := rt.trackers[packID]
	_, live := rt.cancels[packID]
	rt.mu.Unlock()
	if tr != nil && live {
		return tr.Snapshot()
	}
	if s, ok := procmetrics.LoadSnapshot(a.repairStateDir(), packID); ok {
		return s
	}
	return procmetrics.Snapshot{PackID: packID, Samples: []procmetrics.Sample{}}
}

// ── дерево залежностей ─────────────────────────────────────────────────────

// GetModDependencyGraph будує граф залежностей модів збірки (усі jar у mods/,
// у т.ч. вимкнені): вузли, ребра, відсутні залежності, цикли.
func (a *App) GetModDependencyGraph(packID string) (modgraph.Graph, error) {
	dir := a.instanceModsDir(packID)
	g, err := modgraph.BuildGraph(dir)
	if err != nil {
		return g, err
	}
	// Домінантний колір іконки кожного мода — для кольорових ребер і смужок у UI.
	for i := range g.Nodes {
		n := &g.Nodes[i]
		jar := a.modMgr.ResolveJarPath(dir, n.File)
		if jar == "" {
			continue
		}
		key := jar
		if fi, e := os.Stat(jar); e == nil {
			key = fmt.Sprintf("%s|%d|%d", jar, fi.Size(), fi.ModTime().UnixNano())
		}
		n.Color = a.colorOfJar(jar, key)
	}
	return g, nil
}

// GetModRemovalImpact — що зламається чи погіршиться, якщо прибрати мод modID.
func (a *App) GetModRemovalImpact(packID, modID string) (modgraph.Impact, error) {
	g, err := modgraph.BuildGraph(a.instanceModsDir(packID))
	if err != nil {
		return modgraph.Impact{}, err
	}
	return modgraph.RemovalImpact(g, modID), nil
}

// ── Executor ───────────────────────────────────────────────────────────────

type repairExecutor struct {
	a      *App
	packID string
	build  *launchBuild
}

var _ autorepair.Executor = (*repairExecutor)(nil)

func (e *repairExecutor) DisableMods(files []string) (done, skipped []string) {
	for _, f := range files {
		if err := e.a.ToggleMod(e.packID, f, false); err != nil {
			skipped = append(skipped, f)
			continue
		}
		done = append(done, f)
	}
	return
}

func (e *repairExecutor) InstallDependencies(ids []string) (installed, failed []string) {
	for _, id := range ids {
		res := e.a.InstallSuggestedMod(e.packID, id, e.build.MCVersion, e.build.Loader)
		if res.Success {
			installed = append(installed, id)
		} else {
			failed = append(failed, id)
		}
	}
	return
}

func (e *repairExecutor) SetMemory(mb int) error { return e.a.SetPackRAM(e.packID, mb) }

func (e *repairExecutor) SetJava(path string) error {
	icfg := e.a.cfg.GetInstanceConfig(e.packID)
	icfg.ID = e.packID
	icfg.UseSeparateJava = path != ""
	icfg.JavaPathOverride = path
	if err := e.a.cfg.SaveInstanceConfig(icfg); err != nil {
		return err
	}
	if cp, ok := e.a.customPacks.Get(e.packID); ok && (cp.UseSeparateJava || cp.JavaPath != "") {
		cp.UseSeparateJava = path != ""
		cp.JavaPath = path
		return e.a.customPacks.Save(cp)
	}
	return nil
}

func (e *repairExecutor) InstallJava(major int) error {
	if major == 0 {
		major = e.a.packJavaMajor(e.packID)
	}
	_, err := e.a.ensureJavaForLaunch(e.packID, "", major, "")
	return err
}

func (e *repairExecutor) ResetJVMArguments() error {
	icfg := e.a.cfg.GetInstanceConfig(e.packID)
	icfg.ID = e.packID
	icfg.JavaArgsOverride = ""
	return e.a.cfg.SaveInstanceConfig(icfg)
}

func (e *repairExecutor) RepairVersion() error { return e.a.RepairPack(e.packID) }

func (e *repairExecutor) UpdateLoader(minimum string) (string, error) {
	a := e.a
	if _, auto := a.isAutoPackID(e.packID); auto {
		return "", errors.New("версію завантажувача цієї збірки керує лаунчер")
	}
	cp, ok := a.customPacks.Get(e.packID)
	if !ok || cp.Loader == "" || cp.Loader == "vanilla" {
		return "", errors.New("у цієї збірки немає змінного завантажувача")
	}
	entries, err := a.versionsProv.GetLoaderVersions(custompack.Loader(cp.Loader), cp.MCVersion)
	if err != nil {
		return "", err
	}
	var stable, all []string
	for _, v := range entries {
		all = append(all, v.Version)
		if v.Stable {
			stable = append(stable, v.Version)
		}
	}
	pick := autorepair.PickLoaderVersion(stable, minimum)
	if pick == "" {
		pick = autorepair.PickLoaderVersion(all, minimum)
	}
	if pick == "" {
		return "", fmt.Errorf("не знайдено версії %s для Minecraft %s%s", cp.Loader, cp.MCVersion, minimumSuffix(minimum))
	}
	old := cp.LoaderVersion
	cp.LoaderVersion = pick
	if err := a.customPacks.Save(cp); err != nil {
		return "", err
	}
	// Новий профіль лоадера: старий прибираємо зі спільної бази й ставимо заново.
	mcBase := filepath.Join(a.cfg.Dir(), "minecraft")
	if old != "" && old != pick {
		removeMinecraftVersionFiles(mcBase, minecraft.LoaderVersionID(cp.Loader, cp.MCVersion, old))
	}
	a.inst.ResetCancelInstall()
	launchVersion, err := a.inst.EnsureLoader(cp.Loader, cp.MCVersion, pick)
	if err != nil {
		return "", fmt.Errorf("не вдалося встановити %s %s: %w", cp.Loader, pick, err)
	}
	if _, err := a.inst.EnsureVersion(launchVersion); err != nil {
		return "", err
	}
	a.invalidatePackCache()
	a.emit("builds:changed", e.packID)
	return pick, nil
}

func minimumSuffix(minimum string) string {
	if minimum == "" {
		return ""
	}
	return " (не нижче " + minimum + ")"
}

func (e *repairExecutor) BackupConfigs(files []string) ([]string, error) {
	root := filepath.Join(e.a.cfg.Dir(), autorepair.BackupDirName, e.packID)
	return autorepair.BackupConfigs(e.a.gameDir(e.packID), root, files)
}

func (e *repairExecutor) SetDedicatedGPU(enabled bool) error {
	s := e.a.cfg.GetSettings()
	s.UseDedicatedGPU = enabled
	return e.a.SaveSettings(s)
}

func (e *repairExecutor) SetAccount(id string) (string, error) {
	acc, ok := e.a.cfg.GetAccountByID(id)
	if !ok {
		return "", errors.New("акаунт не знайдено")
	}
	if err := e.a.SetActiveAccount(id); err != nil {
		return "", err
	}
	return acc.Username, nil
}

func (e *repairExecutor) TotalRAMMB() int {
	return int(sysinfo.Collect(e.a.cfg.Dir()).TotalRAMMB)
}
