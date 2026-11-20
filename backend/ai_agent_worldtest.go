package main

// ai_agent_worldtest.go — автоматичний тест збірки У СВІТІ.
//
// Багато крашів з'являються лише коли гра вже у світі (генерація чанків, спавн мобів,
// ночі, інфекція…), а не в головному меню. start_world_test:
//   1. робить знімок світів збірки (для режиму world — обов'язково),
//   2. тимчасово вмикає автовхід (quick-play Minecraft 1.20+) у світ або на сервер,
//   3. запускає гру, чекає входу, потім «вистоює» N секунд, знімаючи RAM/CPU,
//   4. збирає помилки консолі, «Can't keep up!», краш-репорти та аварії JVM,
//   5. зупиняє гру, повертає попередні налаштування автовходу й дає вердикт.
// Робота йде у фоні; результат — get_world_test(job_id).

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// AIWorldTest — стан/звіт одного тесту.
type AIWorldTest struct {
	ID          string   `json:"job_id"`
	PackID      string   `json:"pack_id"`
	Mode        string   `json:"mode"` // world | server
	Target      string   `json:"target"`
	Phase       string   `json:"phase"` // snapshot | launching | joining | soaking | stopping | done
	Done        bool     `json:"done"`
	Verdict     string   `json:"verdict,omitempty"` // ok | warnings | crashed | failed_to_join | failed_to_start | error
	Message     string   `json:"message,omitempty"`
	StartedAt   int64    `json:"started_at"`
	JoinSeconds float64  `json:"join_seconds,omitempty"`
	SoakSeconds int      `json:"soak_seconds"`
	PeakRAMMB   float64  `json:"peak_ram_mb,omitempty"`
	AvgCPU      float64  `json:"avg_cpu_percent,omitempty"`
	PeakCPU     float64  `json:"peak_cpu_percent,omitempty"`
	CantKeepUp  int      `json:"cant_keep_up,omitempty"`
	ErrorLines  []string `json:"error_lines,omitempty"`
	CrashReport string   `json:"crash_report,omitempty"` // назва файлу
	CrashTail   string   `json:"crash_excerpt,omitempty"`
	Snapshot    string   `json:"snapshot,omitempty"`
	Hints       []string `json:"hints,omitempty"`
}

var aiWT struct {
	mu   sync.Mutex
	jobs map[string]*AIWorldTest
}

func aiWTSet(id string, f func(j *AIWorldTest)) {
	aiWT.mu.Lock()
	defer aiWT.mu.Unlock()
	if j := aiWT.jobs[id]; j != nil {
		f(j)
	}
}

func (a *App) aiPackRunning(packID string) bool {
	for _, s := range a.sessionsM.Snapshot() {
		if s.BuildID == packID {
			return true
		}
	}
	return false
}

// aiNoisyError — відомі безпечні «помилки», які не слід вважати проблемою.
func aiNoisyError(l string) bool {
	for _, n := range []string{"No data fixer registered", "GL_INVALID_ENUM", "OpenGL debug message", "fabric-crash-report-info",
		"Error loading class:", "Incorrect key", "Failed to load mod icon"} {
		if strings.Contains(l, n) {
			return true
		}
	}
	return false
}

func (a *App) aiRunWorldTest(j *AIWorldTest, soak int, snapshotFirst bool) {
	id := j.PackID
	set := func(f func(j *AIWorldTest)) { aiWTSet(j.ID, f) }
	fail := func(verdict, msg string) {
		set(func(j *AIWorldTest) { j.Verdict, j.Message, j.Phase, j.Done = verdict, msg, "done", true })
	}
	touch := func(phase string) {
		set(func(j *AIWorldTest) { j.Phase = phase })
		a.aiEditBegin(id, "тест у світі: "+phase)
	}
	a.aiEditBegin(id, "тест у світі")
	a.aiEditRecord(id, AIEditChange{Kind: "test", Summary: fmt.Sprintf("Тест у світі: %s %s, %d с", j.Mode, j.Target, soak), OK: true})

	// 1. Знімок світів.
	if snapshotFirst {
		touch("snapshot")
		name, err := a.CreatePackWorldSnapshot(id)
		if err != nil {
			fail("error", "не вдалося зробити знімок світів: "+err.Error())
			a.aiEditFinish(id)
			return
		}
		if name != "" {
			a.aiProvenanceMark("world-snapshot", id+"|"+name)
			set(func(j *AIWorldTest) { j.Snapshot = name })
		}
	}

	// 2. Автовхід (запам'ятовуємо старий стан).
	old := a.GetInstanceConfig(id)
	cfg := old
	cfg.ID = id
	cfg.AutoJoinEnabled = true
	if j.Mode == "world" {
		cfg.AutoJoinType, cfg.AutoJoinWorld = "world", j.Target
	} else {
		cfg.AutoJoinType, cfg.AutoJoinServer = "server", j.Target
	}
	if err := a.SaveInstanceConfig(cfg); err != nil {
		fail("error", "не вдалося ввімкнути автовхід: "+err.Error())
		a.aiEditFinish(id)
		return
	}
	restore := func() {
		c := a.GetInstanceConfig(id)
		c.ID = id
		c.AutoJoinEnabled, c.AutoJoinType, c.AutoJoinWorld, c.AutoJoinServer = old.AutoJoinEnabled, old.AutoJoinType, old.AutoJoinWorld, old.AutoJoinServer
		_ = a.SaveInstanceConfig(c)
	}
	defer func() { restore(); a.aiEditFinish(id) }()

	srvAddr := ""
	if j.Mode == "server" {
		srvAddr = j.Target
	}
	// 3. Запуск.
	touch("launching")
	a.aiClearConsole(id)
	start := time.Now()
	acc, _, perr := a.aiLaunchPrepare(id, srvAddr, 1)
	if perr != nil {
		fail("error", perr.Error())
		return
	}
	go func() {
		defer func() { _ = recover() }()
		_ = a.aiLaunchRun(id, acc, srvAddr)
	}()
	for !a.aiSessionRunning(id) {
		if time.Since(start) > 4*time.Minute {
			fail("failed_to_start", "гра не запустилась за 4 хвилини (перевірте get_console / list_build_problems)")
			return
		}
		time.Sleep(2 * time.Second)
	}

	// 4. Вхід у світ.
	touch("joining")
	joined := false
	joinMarker := "joined the game"
	if j.Mode == "server" {
		joinMarker = "Connecting to"
	}
	for time.Since(start) < 6*time.Minute {
		if !a.aiSessionRunning(id) {
			break
		}
		if pg := a.aiConsolePage(id, 0, 5, "", joinMarker, 0); len(pg.Lines) > 0 {
			joined = true
			break
		}
		a.aiEditBegin(id, "тест у світі: вхід")
		time.Sleep(2 * time.Second)
	}
	if joined {
		set(func(j *AIWorldTest) { j.JoinSeconds = time.Since(start).Seconds() })
	}

	// 5. Вистоювання.
	crashed := !a.aiSessionRunning(id)
	if joined && !crashed {
		touch("soaking")
		end := time.Now().Add(time.Duration(soak) * time.Second)
		for time.Now().Before(end) {
			if !a.aiSessionRunning(id) {
				crashed = true
				break
			}
			a.aiEditBegin(id, fmt.Sprintf("тест у світі: ще %d с", int(time.Until(end).Seconds())))
			time.Sleep(3 * time.Second)
		}
	}

	// 6. Збір показників, зупинка.
	m := a.GetPackMetrics(id)
	touch("stopping")
	if a.aiSessionRunning(id) {
		_ = a.aiStopBuild(id)
		for i := 0; i < 30 && a.aiSessionRunning(id); i++ {
			time.Sleep(time.Second)
		}
	}

	errPage := a.aiConsolePage(id, 0, 400, "error", "", 0)
	seen := map[string]bool{}
	var errs []string
	for _, l := range errPage.Lines {
		t := strings.TrimSpace(l.Text)
		if t == "" || aiNoisyError(t) || seen[t] {
			continue
		}
		seen[t] = true
		if r := []rune(t); len(r) > 220 {
			t = string(r[:220]) + "…"
		}
		errs = append(errs, t)
		if len(errs) >= 15 {
			break
		}
	}
	keepUp := len(a.aiConsolePage(id, 0, 500, "", "Can't keep up", 0).Lines)

	// Краш-репорти, створені під час тесту.
	var crashName, crashTail string
	gd := a.gameDir(id)
	type cf struct {
		n string
		t time.Time
	}
	var fresh []cf
	if es, err := os.ReadDir(filepath.Join(gd, "crash-reports")); err == nil {
		for _, e := range es {
			if info, err := e.Info(); err == nil && !e.IsDir() && info.ModTime().After(start.Add(-5*time.Second)) {
				fresh = append(fresh, cf{e.Name(), info.ModTime()})
			}
		}
	}
	if es, err := os.ReadDir(gd); err == nil {
		for _, e := range es {
			if info, err := e.Info(); err == nil && strings.HasPrefix(e.Name(), "hs_err") && info.ModTime().After(start.Add(-5*time.Second)) {
				fresh = append(fresh, cf{e.Name(), info.ModTime()})
			}
		}
	}
	if len(fresh) > 0 {
		sort.Slice(fresh, func(i, k int) bool { return fresh[i].t.After(fresh[k].t) })
		crashName = fresh[0].n
		dir := filepath.Join(gd, "crash-reports")
		if strings.HasPrefix(crashName, "hs_err") {
			dir = gd
		}
		if txt, err := tailFile(filepath.Join(dir, crashName), 3500); err == nil {
			crashTail = txt
		}
	}

	set(func(j *AIWorldTest) {
		j.PeakRAMMB, j.AvgCPU, j.PeakCPU = m.PeakRAMMB, m.AvgCPU, m.PeakCPU
		j.ErrorLines, j.CantKeepUp, j.CrashReport, j.CrashTail = errs, keepUp, crashName, crashTail
		j.Phase, j.Done = "done", true
		switch {
		case crashName != "" || (crashed && joined):
			j.Verdict, j.Message = "crashed", "гра впала під час тесту у світі"
			j.Hints = []string{"read_log_file (crash-report) → знайти мод-винуватця", "diagnose_pack", "toggle_mod підозрілого мода й повторити start_world_test"}
		case !joined:
			j.Verdict, j.Message = "failed_to_join", "гра запустилась, але вхід у світ не підтверджено (світ відсутній чи пошкоджений, або версія <1.20)"
			j.Hints = []string{"list_build_worlds", "get_console level=error", "read_log_file latest.log"}
		case len(errs) > 0 || keepUp > 5:
			j.Verdict, j.Message = "warnings", "у світі гра працює, але є помилки/просідання"
			j.Hints = []string{"перевірте error_lines", "cant_keep_up > 0 — перевантажена симуляція (орди, мало RAM): get_pack_metrics, set_build_ram"}
		default:
			j.Verdict, j.Message = "ok", "увійшли у світ і вистояли без крашів і серйозних помилок"
		}
	})
	a.aiEditRecord(id, AIEditChange{Kind: "test", OK: true, Summary: "Тест у світі завершено"})
}

func init() {
	regTool(aiTool{
		name: "start_world_test",
		desc: "ТЕСТ У СВІТІ: запускає збірку, автоматично заходить в одиночний світ (world) або на сервер (server), вистоює soak_seconds секунд і збирає краші, помилки, «Can't keep up!», RAM/CPU. Перед тестом у світ робиться знімок світів. Працює у фоні: повертає job_id, далі опитуйте get_world_test. Потрібна версія Minecraft 1.20+.",
		props: map[string]string{"pack_id": "id збірки", "world": "тека одиночного світу з list_build_worlds", "server": "host[:port] сервера (замість world; напр. локальний 127.0.0.1:25565)",
			"soak_seconds": "(int) скільки секунд тримати гру у світі: 30..900, за замовч. 120"},
		required: []string{"pack_id"}, write: true,
		run: func(a *App, g argMap) (any, error) {
			id := g.str("pack_id")
			if id == "" {
				return nil, errPackID()
			}
			soak := g.integer("soak_seconds", 120)
			if soak < 30 || soak > 900 {
				return nil, errors.New("soak_seconds: 30..900")
			}
			if a.aiPackRunning(id) {
				return nil, errors.New("збірка вже запущена — спершу stop_build")
			}
			mode, target := "", ""
			if w := g.str("world"); w != "" {
				found := false
				for _, x := range a.ListInstanceWorlds(id) {
					found = found || x == w
				}
				if !found {
					return nil, errors.New("світ не знайдено (list_build_worlds). Якщо світів немає: попросіть користувача створити один тестовий світ у збірці або використайте server=… з локальним сервером")
				}
				mode, target = "world", w
			} else if s := g.str("server"); s != "" {
				if len(s) > 255 || strings.ContainsAny(s, " \r\n\"'") {
					return nil, errors.New("server: host[:port] без пробілів")
				}
				mode, target = "server", s
			} else {
				return nil, errors.New("вкажіть world або server")
			}
			aiWT.mu.Lock()
			if aiWT.jobs == nil {
				aiWT.jobs = map[string]*AIWorldTest{}
			}
			for _, o := range aiWT.jobs {
				if !o.Done && o.PackID == id {
					aiWT.mu.Unlock()
					return nil, errors.New("тест цієї збірки вже йде: " + o.ID)
				}
			}
			if len(aiWT.jobs) > 20 { // не накопичувати історію
				for k, o := range aiWT.jobs {
					if o.Done {
						delete(aiWT.jobs, k)
					}
				}
			}
			j := &AIWorldTest{ID: fmt.Sprintf("wt%d", time.Now().UnixMilli()), PackID: id, Mode: mode, Target: target,
				Phase: "starting", StartedAt: time.Now().UnixMilli(), SoakSeconds: soak}
			aiWT.jobs[j.ID] = j
			aiWT.mu.Unlock()
			go func() {
				defer func() {
					if r := recover(); r != nil {
						aiWTSet(j.ID, func(j *AIWorldTest) {
							j.Verdict, j.Message, j.Phase, j.Done = "error", fmt.Sprint("внутрішня помилка: ", r), "done", true
						})
					}
				}()
				a.aiRunWorldTest(j, soak, mode == "world")
			}()
			return map[string]any{"job_id": j.ID, "note": "тест іде у фоні; опитуйте get_world_test кожні 15–30 с"}, nil
		},
	})
	regTool(aiTool{
		name: "get_world_test", desc: "Стан і звіт тесту у світі (start_world_test): фаза, вердикт (ok / warnings / crashed / failed_to_join / failed_to_start), час входу, RAM/CPU, помилки, уривок краш-репорту.",
		props: map[string]string{"job_id": "id з start_world_test"}, required: []string{"job_id"},
		run: func(a *App, g argMap) (any, error) {
			aiWT.mu.Lock()
			defer aiWT.mu.Unlock()
			j := aiWT.jobs[g.str("job_id")]
			if j == nil {
				return nil, errors.New("тест не знайдено")
			}
			c := *j
			c.ErrorLines = append([]string{}, j.ErrorLines...)
			return c, nil
		},
	})
}
