// process.go — запуск/зупинка/консоль процесу Minecraft-сервера.
package servers

import (
	"shaurma-launcher-backend/internal/crashguard"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/console"
	"shaurma-launcher-backend/internal/model"
)

// processInfo — внутрішня інформація про запущений процес сервера.
type processInfo struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	startTime time.Time
	status    ServerStatus
	core      string // ядро сервера (для правильної команди graceful-stop)
	// dir — тека сервера (потрібна, щоб прибрати runtime-запис після
	// завершення процесу: сам watchProcess отримує лише ID і *exec.Cmd).
	dir string
	// recovered — процес «усиновлено» після краша лаунчера (recover.go):
	// stdout/stdin утрачені, статус відновлено з диска.
	recovered bool
	mu        sync.Mutex
	// exited закривається РІВНО ОДИН РАЗ — коли watchProcess завершує
	// cmd.Wait(). Це єдине місце в усьому коді, де викликається Wait()
	// на процесі: exec.Cmd.Wait можна викликати лише раз, тож Stop/
	// killProcess чекають саме цей канал, а не роблять власний Wait.
	exited chan struct{}
}

// Manager — менеджер запущених процесів серверів.
type Manager struct {
	mu                sync.RWMutex
	running           map[string]*processInfo // serverID → processInfo
	bufers            map[string]*console.Buffer
	onLine            func(serverID string, line model.ConsoleLine)
	onStatus          func(serverID string, status ServerStatus)
	onInstallProgress func(serverID string, p ServerInstallProgress)
	// onPlayer — вхід/вихід гравця на сервері ("server:player"). Джерело —
	// рядки логу java (див. players.go), а не окремий опитувач.
	onPlayer func(serverID string, ev PlayerEvent)
}

// NewManager створює новий менеджер процесів.
func NewManager() *Manager {
	return &Manager{
		running: make(map[string]*processInfo),
		bufers:  make(map[string]*console.Buffer),
	}
}

// SetCallbacks встановлює обробники подій.
func (m *Manager) SetCallbacks(onLine func(string, model.ConsoleLine), onStatus func(string, ServerStatus)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onLine = onLine
	m.onStatus = onStatus
}

// SetInstallProgressCallback встановлює обробник структурованого прогресу
// встановлення ядра ("server:install-progress").
func (m *Manager) SetInstallProgressCallback(fn func(string, ServerInstallProgress)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onInstallProgress = fn
}

// SetPlayerCallback встановлює обробник подій входу/виходу гравців
// ("server:player"). Окремий сетер, а не розширення SetCallbacks: той самий
// підхід уже використано для install-progress, щоб не ламати наявні виклики.
func (m *Manager) SetPlayerCallback(fn func(string, PlayerEvent)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onPlayer = fn
}

// notifyPlayer розсилає подію входу/виходу. Best-effort: без підписника
// (тести, CLI) просто нічого не станеться.
func (m *Manager) notifyPlayer(serverID string, ev PlayerEvent) {
	m.mu.RLock()
	fn := m.onPlayer
	m.mu.RUnlock()
	if fn != nil {
		fn(serverID, ev)
	}
}

// IsRunning повертає true, якщо сервер запущено.
func (m *Manager) IsRunning(serverID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	pi, ok := m.running[serverID]
	if !ok {
		return false
	}
	pi.mu.Lock()
	defer pi.mu.Unlock()
	return pi.status.Status == "running" || pi.status.Status == "starting"
}

// GetStatus повертає статус сервера.
func (m *Manager) GetStatus(serverID string) ServerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	pi, ok := m.running[serverID]
	if !ok {
		return ServerStatus{Status: "stopped"}
	}
	pi.mu.Lock()
	defer pi.mu.Unlock()
	s := pi.status
	if s.Status == "running" {
		s.UptimeSec = int64(time.Since(pi.startTime).Seconds())
	}
	return s
}

// GetBuffer повертає консольний буфер сервера.
func (m *Manager) GetBuffer(serverID string) *console.Buffer {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.bufers[serverID]
}

// Start запускає сервер.
func (m *Manager) Start(srv *Server, javaPath string) error {
	m.mu.Lock()
	if _, ok := m.running[srv.ID]; ok {
		m.mu.Unlock()
		return fmt.Errorf("сервер %s вже запущено", srv.ID)
	}
	m.mu.Unlock()

	// Формуємо команду.
	args := buildCommand(srv)
	if len(args) == 0 {
		return fmt.Errorf("неможливо сформувати команду запуску для ядра %s", srv.Core)
	}

	// Тека .shaurma потрібна під GC-лог (наш звіт пам'яті JVM) і вже
	// використовується під launch_config.json/network.json.
	_ = os.MkdirAll(filepath.Join(srv.Dir, ".shaurma"), 0755)
	// GC-лог попереднього запуску прибираємо: кожен старт сервера має
	// свій, чистий файл (інакше показники попереднього запуску змішувались
	// би з новими, а файл pic би безмежно).
	_ = os.Remove(GCLogPath(srv.Dir))

	cmd := exec.Command(javaPath, args...)
	cmd.Dir = srv.Dir
	// CREATE_NEW_PROCESS_GROUP (Windows) — винесено в setNewProcessGroup
	// (procgroup_windows.go/procgroup_other.go), той самий platform-split
	// підхід, що вже використовує hideConsoleWindow нижче: пряме
	// syscall.CREATE_NEW_PROCESS_GROUP тут не компілювалось поза Windows.
	setNewProcessGroup(cmd)
	// Приховуємо консольне вікно java.exe (лаунчер зібраний без власної
	// консолі; без цього кожен запуск сервера відкривав би видиме вікно
	// терміналу — сервер працює з --nogui, GUI йому не потрібне взагалі).
	hideConsoleWindow(cmd)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}
	// Вивід — у ФАЙЛ, а не в анонімний pipe (console_file.go): дескриптор
	// успадковується java і лишається валідним після смерті лаунчера, тож
	// після перезапуску консоль показує ВЕСЬ вивід, включно з тим, що сервер
	// написав, поки лаунчера не було (раніше читався лише logs/latest.log).
	// stdin лишається pipe — команди працюють, поки лаунчер живий.
	consoleFile, err := openServerConsoleLog(srv.Dir)
	if err != nil {
		stdin.Close()
		return fmt.Errorf("console log: %w", err)
	}
	cmd.Stdout = consoleFile
	cmd.Stderr = consoleFile

	// Буфер консолі — перевикористовуємо, якщо install-крок уже писав у
	// нього SYSTEM-рядки прогресу (EnsureBuffer), інакше створюємо новий.
	// Так install-логи ("Завантаження server.jar...") лишаються видимими
	// в консолі поруч зі звичайним виводом java, а не стираються.
	// ЗБЕРІГАЄМО існуючий буфер з попередніх запусків — історія консолі не має обнулятись!
	buf := m.EnsureBuffer(srv.ID)
	m.mu.Lock()
	pi := &processInfo{
		cmd:       cmd,
		stdin:     stdin,
		startTime: time.Now(),
		status:    ServerStatus{Status: "starting", PID: 0},
		core:      srv.Core,
		dir:       srv.Dir,
		exited:    make(chan struct{}),
	}
	m.running[srv.ID] = pi
	m.mu.Unlock()

	// Запускаємо процес.
	if err := cmd.Start(); err != nil {
		stdin.Close()
		consoleFile.Close()
		m.mu.Lock()
		delete(m.running, srv.ID)
		delete(m.bufers, srv.ID)
		m.mu.Unlock()
		return fmt.Errorf("cmd.Start: %w", err)
	}

	// Оновлюємо PID.
	pi.mu.Lock()
	pi.status.PID = cmd.Process.Pid
	pi.status.Status = "starting"
	statusSnapshot := pi.status
	pi.mu.Unlock()
	m.notifyStatus(srv.ID, statusSnapshot)

	log.Printf("[servers] запущено %s (PID %d) у %s", srv.Name, cmd.Process.Pid, srv.Dir)

	// Дочірній процес отримав власну копію дескриптора — батьківська більше
	// не потрібна (тримати її означало б блокувати файл на Windows).
	_ = consoleFile.Close()

	// Консоль читаємо «хвостом» файлу — так само, як для відновленого сервера
	// (recover_console.go). Статус "running" виставляється на рядок "Done …!"
	// всередині followLogFile.
	go func(id string, info *processInfo, b *console.Buffer, path string) {
		defer crashguard.Recover("m.followLogFile")
		m.followLogFile(id, info, b, path, 0)
	}(srv.ID, pi, buf, serverConsoleLogPath(srv.Dir))

	// runtime-запис на диск: якщо лаунчер аварійно завершиться, цей сервер
	// можна буде «усиновити» при наступному старті (recover.go) замість
	// того, щоб вважати його порт чужим. Час створення процесу — захист від
	// перевикористання PID іншим процесом.
	writeRuntimeRecord(srv.Dir, runtimeRecord{
		PID:       cmd.Process.Pid,
		CreatedNs: snapshotProcess(cmd.Process.Pid).CreateNs,
		StartedAt: pi.startTime.UnixMilli(),
		Core:      srv.Core,
		Port:      srv.Port,
		Java:      javaPath,
	})

	// Запускаємо збір статистики (порт потрібен для живих status-ping-ів,
	// які дають точну кількість гравців онлайн; ramMB — це -Xmx, тобто
	// максимум купи, від якого UI рахує відсоток використання пам'яті).
	StartStatsCollection(srv.ID, pi, srv.Port, srv.Dir, srv.RAMMB)

	// Спостерігаємо за завершенням у фоні.
	{
		_f, _a0, _a1 := m.watchProcess, srv.ID, cmd
		go func() {
			defer crashguard.Recover("m.watchProcess")
			_f(_a0, _a1)
		}()
	}

	return nil
}

// Stop зупиняє сервер.
func (m *Manager) Stop(serverID string, graceful bool) error {
	m.mu.RLock()
	pi, ok := m.running[serverID]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("сервер %s не запущено", serverID)
	}

	pi.mu.Lock()
	if pi.status.Status == "stopping" || pi.status.Status == "stopped" {
		pi.mu.Unlock()
		return nil
	}
	pi.status.Status = "stopping"
	statusSnapshot := pi.status
	pi.mu.Unlock()
	m.notifyStatus(serverID, statusSnapshot)

	if graceful {
		// Команда graceful-зупинки залежить від ядра ("stop" скрізь, "end"
		// у Velocity) — розділ 2.3: винесено в CoreForStopCommand (server.go),
		// а не хардкодиться тут inline, як було раніше.
		stopCmd := "stop\n"
		if srv, err := m.serverCoreOf(serverID); err == nil {
			stopCmd = CoreForStopCommand(srv) + "\n"
		}
		_, err := pi.stdin.Write([]byte(stopCmd))
		if err == nil {
			// Чекаємо завершення процесу (watchProcess закриє exited —
			// НЕ робимо власний cmd.Wait(), він одноразовий на *exec.Cmd).
			select {
			case <-pi.exited:
				return nil
			case <-time.After(30 * time.Second):
				log.Printf("[servers] graceful stop timeout для %s, kill", serverID)
			}
		} else {
			log.Printf("[servers] graceful stop: не вдалось написати в stdin %s: %v — kill", serverID, err)
		}
	}

	// Force kill.
	m.killProcess(serverID)
	// Дочекатись реального завершення процесу (watchProcess прибере його
	// з running і надішле фінальний статус), щоб виклик Stop не повертався
	// раніше, ніж порт справді звільниться.
	select {
	case <-pi.exited:
	case <-time.After(10 * time.Second):
		log.Printf("[servers] %s не завершився навіть після kill за 10с", serverID)
	}
	return nil
}

// serverCoreOf повертає ядро запущеного сервера за його ID (для вибору
// команди graceful-зупинки: stop у Vanilla/Paper/Forge, end у Velocity).
func (m *Manager) serverCoreOf(serverID string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	pi, ok := m.running[serverID]
	if !ok {
		return "", fmt.Errorf("не запущено")
	}
	pi.mu.Lock()
	defer pi.mu.Unlock()
	return pi.core, nil
}

// SendCommand надсилає команду в stdin сервера.
func (m *Manager) SendCommand(serverID, cmd string) error {
	m.mu.RLock()
	pi, ok := m.running[serverID]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("сервер %s не запущено", serverID)
	}
	pi.mu.Lock()
	status := pi.status.Status
	pi.mu.Unlock()
	if status != "running" {
		return fmt.Errorf("сервер %s не запущено", serverID)
	}
	if cmd == "" {
		return nil
	}
	// Консоль відновленого процесу (recover.go): канал утрачено разом із
	// попереднім лаунчером — повертаємо помилку від заглушки stdin.
	_, err := pi.stdin.Write([]byte(cmd + "\n"))
	return err
}

// killProcess примусово завершує процес і всіх нащадків. НЕ викликає
// cmd.Wait() — це виключна відповідальність watchProcess (Wait можна
// викликати лише раз на *exec.Cmd за весь його життєвий цикл). Виклик
// цієї функції лише ІНІЦІЮЄ вбивство; фактичне звільнення ресурсів і
// оновлення статусу станеться, коли watchProcess побачить вихід процесу.
func (m *Manager) killProcess(serverID string) error {
	m.mu.RLock()
	pi, ok := m.running[serverID]
	m.mu.RUnlock()
	if !ok || pi.cmd.Process == nil {
		return nil
	}

	if runtime.GOOS == "windows" {
		// taskkill /T /F /PID — дерево процесів (щоб не лишались
		// осиротілі дочірні процеси/тримання порту після kill).
		cmd := exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprintf("%d", pi.cmd.Process.Pid))
		hideConsoleWindow(cmd)
		if out, err := cmd.CombinedOutput(); err != nil {
			log.Printf("[servers] taskkill %s: %v (%s)", serverID, err, strings.TrimSpace(string(out)))
		}
	} else {
		// kill -TERM → (за 3с) kill -KILL, якщо процес не відреагував.
		_ = pi.cmd.Process.Signal(os.Interrupt)
		pid := pi.cmd.Process.Pid
		time.AfterFunc(3*time.Second, func() {
			if !m.IsRunning(serverID) {
				return // вже завершився після SIGINT
			}
			if p, err := os.FindProcess(pid); err == nil {
				_ = p.Kill()
			}
		})
	}
	return nil
}

// markServerReady переводить статус у "running", коли в лозі з'явився рядок
// "Done …!". Викликається з followLogFile — єдиного джерела виводу сервера
// (раніше це робив readOutput, який читав анонімний pipe).
func (m *Manager) markServerReady(serverID string) {
	m.mu.Lock()
	pi, ok := m.running[serverID]
	m.mu.Unlock()
	if !ok {
		return
	}
	pi.mu.Lock()
	if pi.status.Status != "starting" {
		pi.mu.Unlock()
		return
	}
	pi.status.Status = "running"
	s := pi.status
	pi.mu.Unlock()
	m.notifyStatus(serverID, s)
	log.Printf("[servers] %s готовий (running)", serverID)
}

// watchProcess спостерігає за завершенням процесу. Це ЄДИНЕ місце у
// всьому пакеті, яке викликає cmd.Wait() — виклик, дозволений рівно
// один раз за життя *exec.Cmd. Stop()/killProcess() лише ІНІЦІЮЮТЬ
// завершення (stdin "stop" або сигнал/taskkill) і чекають закриття
// pi.exited, яке гарантовано настає тут.
func (m *Manager) watchProcess(serverID string, cmd *exec.Cmd) {
	waitErr := cmd.Wait()

	m.mu.Lock()
	pi, ok := m.running[serverID]
	if !ok {
		m.mu.Unlock()
		return
	}

	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	} else if waitErr != nil {
		exitCode = -1
	}

	// Визначаємо статус.
	pi.mu.Lock()
	prevStatus := pi.status.Status
	if prevStatus == "stopping" {
		pi.status.Status = "stopped"
	} else if exitCode == 0 {
		pi.status.Status = "stopped"
	} else {
		pi.status.Status = "crashed"
		pi.status.LastError = fmt.Sprintf("exit code %d", exitCode)
	}
	pi.status.ExitCode = exitCode
	s := pi.status
	pi.mu.Unlock()

	delete(m.running, serverID)
	// ЗБЕРІГАЄМО буфер консолі після зупинки сервера — історія має залишатися доступною
	// при поверненні на сторінку сервера. Буфер видаляється ТІЛЬКИ при видаленні сервера.
	// m.bufers[serverID] НЕ видаляємо тут!
	m.mu.Unlock()

	// Процес завершився (штатно чи з падінням) — runtime-запис більше не
	// описує живий процес, тож при наступному старті лаунчера він не буде
	// помилково «усиновлений».
	removeRuntimeRecord(pi.dir)

	StopStatsCollection(serverID)
	// Сервер завершився — усі, хто був онлайн, вже не онлайн. Без скидання
	// список гравців показував би "залишки" до наступного /list-рядка, а
	// після наступного старту — чужих гравців з попередньої сесії.
	ClearOnlinePlayers(serverID)
	m.notifyPlayer(serverID, PlayerEvent{Action: "reset", At: time.Now().UnixMilli(), Count: 0, List: []string{}})
	m.notifyStatus(serverID, s)
	log.Printf("[servers] %s завершено (exit %d, status: %s)", serverID, exitCode, s.Status)

	// Останнім кроком — розбудити всіх, хто чекає на pi.exited у Stop().
	// Робимо це ПІСЛЯ видалення з running і нотифікації статусу, щоб
	// CheckPortFree (яке дивиться IsRunning) вже бачило сервер зупиненим
	// до того, як Stop() поверне керування викликачу.
	close(pi.exited)
}

// EnsureBuffer повертає буфер консолі сервера, створюючи його за потреби.
// Використовується для install-прогресу, який пишеться в консоль ЩЕ ДО
// того, як з'явився java-процес — щоб рядки не губились і мали правильні
// Time/ID (ті самі, що для звичайного виводу процесу), буфер має існувати
// заздалегідь, а не створюватись лише в Start().
//
// ВАЖЛИВО: буфер повинен зберігатися навіть після зупинки сервера, щоб
// при поверненні на сторінку сервера консоль показувала історію.
// Буфер видаляється ТІЛЬКИ при видаленні сервера або якщо буфер пустий
// і процес завершено (очищення ресурсів).
func (m *Manager) EnsureBuffer(serverID string) *console.Buffer {
	m.mu.Lock()
	defer m.mu.Unlock()
	if buf, ok := m.bufers[serverID]; ok {
		return buf
	}
	// Створюємо новий буфер з більшим лімітом для історії консолі
	buf := console.NewBuffer(10000)
	m.bufers[serverID] = buf
	return buf
}

// KeepBufferAfterStop повертає true, якщо буфер консолі має зберігатися
// після зупинки сервера (завжди true — історія має залишатися доступною).
func (m *Manager) KeepBufferAfterStop(serverID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.bufers[serverID]
	return ok // завжди зберігаємо, якщо буфер існує
}

// ClearBuffer видаляє буфер консолі сервера (використовується при видаленні сервера).
func (m *Manager) ClearBuffer(serverID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.bufers, serverID)
}

// NotifyInstallLine пише SYSTEM-рядок install-прогресу в буфер консолі і
// шле його тим самим каналом подій "server:line", яким живиться
// ServerConsolePanel — завантаження ядра видно в консолі так само, як
// звичайний вивід java-процесу.
func (m *Manager) NotifyInstallLine(serverID, text string) {
	buf := m.EnsureBuffer(serverID)
	line := buf.AppendLevel(console.LevelSystem, text)
	m.notifyLine(serverID, line)
}

// NotifyInstallProgress шле структурований прогрес встановлення ядра
// через окремий канал "server:install-progress". Консоль сервера
// лишається чистою до першого реального stdout від java.
func (m *Manager) NotifyInstallProgress(serverID string, p ServerInstallProgress) {
	m.mu.RLock()
	fn := m.onInstallProgress
	m.mu.RUnlock()
	if fn != nil {
		fn(serverID, p)
	}
}

// NotifyInstallStatus шле проміжний статус (наприклад "installing") в
// підписку "server:status", поки процес ще не запущено.
func (m *Manager) NotifyInstallStatus(serverID, status string) {
	m.notifyStatus(serverID, ServerStatus{Status: status})
}

func (m *Manager) notifyLine(serverID string, line model.ConsoleLine) {
	// Рядок УЖЕ покладено в буфер викликачем (readOutput → buf.AppendLevel,
	// NotifyInstallLine → buf.AppendLevel). Раніше тут додатково викликався
	// EnsureBuffer().AppendRaw(line), і кожен рядок лягав у буфер ДВІЧІ —
	// знімок консолі показував кожен рядок двічі. Саме звідси скарга про
	// «подвійне [SYSTEM] Сервер: stopped (PID: ...)». Тут лишається ТІЛЬКИ
	// розсилка події назовні.
	m.mu.RLock()
	fn := m.onLine
	m.mu.RUnlock()
	if fn != nil {
		fn(serverID, line)
	}
}

// GetServerConsoleSnapshot повертає знімок буфера консолі сервера —
// ті самі рядки, що вже були відправлені (або ще не відправлені,
// якщо підписка ще не створена). Викликається при монтуванні
// ServerConsolePanel, щоб консоль НЕ очищалась при перемиканні між
// серверами/вкладками.
func (m *Manager) GetServerConsoleSnapshot(serverID string) []model.ConsoleLine {
	return m.EnsureBuffer(serverID).Snapshot()
}

func (m *Manager) notifyStatus(serverID string, status ServerStatus) {
	m.mu.RLock()
	fn := m.onStatus
	m.mu.RUnlock()
	if fn != nil {
		fn(serverID, status)
	}
	// Пишемо статус у консоль як SYSTEM-рядок для історії
	// (лише для фінальних статусів, не для проміжних like "starting")
	if status.Status == "running" || status.Status == "stopped" || status.Status == "crashed" || status.Status == "error" {
		m.NotifyInstallLine(serverID, fmt.Sprintf("[SYSTEM] Сервер: %s (PID: %d)", status.Status, status.PID))
	}
}

// StopAll зупиняє всі запущені сервери (graceful).
func (m *Manager) StopAll() {
	m.mu.RLock()
	ids := make([]string, 0, len(m.running))
	for id := range m.running {
		ids = append(ids, id)
	}
	m.mu.RUnlock()
	for _, id := range ids {
		_ = m.Stop(id, true)
	}
}

// ── buildCommand — формує аргументи запуску за ядром ─────────────────────
//
// Розділ 2.1: для Forge/NeoForge читає LaunchConfig (launch_config.json,
// через resolveLaunchConfig — самозцілення розділу 1.2), замість того щоб
// самому сканувати диск на win_args.txt/forge-*-universal.jar при
// КОЖНОМУ виклику, як робили buildForgeArgs/buildNeoForgeArgs раніше.
// Встановлення (install.go) вирішує ЩО запускати один раз і зберігає це;
// buildCommand лише БЕРЕ готове рішення і будує з нього java-аргументи.
func buildCommand(srv *Server) []string {
	jvmArgs := parseArgs(srv.JvmArgs)
	if srv.JvmArgs == "" {
		jvmArgs = parseArgs(DefaultJVMArgs(srv.JavaMajor, srv.RAMMB))
	}

	// Просимо JVM вести власний GC-звіт у файл — з нього сторінка сервера
	// бере СПРАВЖНЮ пам'ять JVM (жива/закомічена купа) замість RSS процесу.
	// Прапорці додаються лише коли користувач не налаштував GC-логування сам
	// (див. GCLogArgs) і завжди — ПЕРЕД `-jar`/`@args.txt` (JVM-опції
	// мусять іти перед -jar; у Forge-args-файлі вони теж лишаються
	// найпершими, бо @file розкривається там, де стоїть).
	jvmArgs = append(jvmArgs, GCLogArgs(srv.JavaMajor, jvmArgs)...)

	var jarArgs []string

	switch srv.Core {
	case CoreVelocity:
		// java <jvm> -jar velocity.jar (без --nogui)
		jarArgs = []string{"-jar", filepath.Join(srv.Dir, "velocity.jar")}

	case CoreForge:
		jarArgs = buildModdedLoaderArgs(srv, "net/minecraftforge/forge")

	case CoreNeoForge:
		jarArgs = buildModdedLoaderArgs(srv, "net/neoforged/neoforge")

	case CoreFabric:
		jarArgs = buildLoaderJarArgs(srv, "fabric-server-launch.jar")

	case CoreQuilt:
		jarArgs = buildLoaderJarArgs(srv, "quilt-server-launch.jar")

	case CorePaper, CorePurpur, CoreVanilla:
		jar := "server.jar"
		if srv.Core == CorePaper || srv.Core == CorePurpur {
			// Шукаємо actual jar файл
			jar = findServerJar(srv.Dir)
		}
		jarArgs = []string{"-jar", filepath.Join(srv.Dir, jar), "--nogui"}

	default:
		jarArgs = []string{"-jar", filepath.Join(srv.Dir, "server.jar"), "--nogui"}
	}

	// Додаємо program args (порожньо = нічого).
	if srv.ProgramArgs != "" {
		jarArgs = append(jarArgs, parseArgs(srv.ProgramArgs)...)
	}

	return append(jvmArgs, jarArgs...)
}

// buildLoaderJarArgs — Fabric/Quilt: команда запуску не залежить від
// вмісту LaunchConfig (обидва завжди запускаються через самодостатній
// launch-jar на корені, встановлений install.go), тож тут просто
// перевіряємо наявність очікуваного jar-файлу за іменем — розділ 2.2.
func buildLoaderJarArgs(srv *Server, jarName string) []string {
	return []string{"-jar", filepath.Join(srv.Dir, jarName), "nogui"}
}

// buildModdedLoaderArgs — спільний хелпер для Forge й NeoForge (розділ
// 2.1): читає LaunchConfig через resolveLaunchConfig (самозцілення розділу
// 1.2 — якщо збережений конфіг порожній, а лоадер насправді встановлений
// на диску, пересканує й зберігає перед тим, як повернути результат) і
// будує аргументи з нього. vendorPath ("net/minecraftforge/forge" чи
// "net/neoforged/neoforge") — єдина відмінність між двома викликами;
// buildForgeArgs/buildNeoForgeArgs (майже ідентичні в старій версії,
// різнились лише цим шляхом) зведені до одного хелпера.
func buildModdedLoaderArgs(srv *Server, vendorPath string) []string {
	cfg, err := resolveLaunchConfig(srv.Dir, srv.Core, srv.LoaderVer)
	if err != nil {
		log.Printf("[servers] resolveLaunchConfig(%s): %v — фолбек на server.jar", srv.ID, err)
		return []string{"-jar", filepath.Join(srv.Dir, "server.jar"), "nogui"}
	}

	if cfg.ArgsFile != "" {
		// @user_jvm_args.txt (якщо є) @libraries/.../win_args.txt nogui —
		// та сама нова система запуску (MC ≥1.17), що й раніше.
		argsFile := filepath.Join(srv.Dir, filepath.FromSlash(cfg.ArgsFile))
		userArgsFile := filepath.Join(srv.Dir, "user_jvm_args.txt")
		var result []string
		if fileExists(userArgsFile) {
			result = append(result, "@"+userArgsFile)
		}
		result = append(result, "@"+argsFile, "nogui")
		return result
	}

	if cfg.JarPath != "" {
		// Forge launcher-jar (шим/universal) — самодостатній, без args-файлу.
		return []string{"-jar", filepath.Join(srv.Dir, cfg.JarPath), "nogui"}
	}

	// Перевірка fallback для NeoForge: якщо vendorPath указує на neoforged,
	// але конфіг порожній, пробуємо знайти args-файл безпосередньо на диску.
	// Це виправляє баг, коли NeoForge запускався як Paper через порожній
	// launch_config.json після ручного копіювання папки сервера.
	if vendorPath == "net/neoforged/neoforge" {
		if argsFile, ok := findLoaderArgsFile(srv.Dir, filepath.Join("net", "neoforged", "neoforge"), srv.LoaderVer); ok {
			log.Printf("[servers] NeoForge fallback: знайдено args-файл %s на диску", argsFile)
			cfg.ArgsFile = argsFile
			// Зберігаємо знайдений конфіг для майбутніх запусків
			if err := saveLaunchConfig(srv.Dir, LaunchConfig{ArgsFile: argsFile}); err == nil {
				log.Printf("[servers] NeoForge: збережено launch_config.json з args-файлом")
			}
			// Будемо використовувати знайдений файл
			userArgsFile := filepath.Join(srv.Dir, "user_jvm_args.txt")
			var result []string
			if fileExists(userArgsFile) {
				result = append(result, "@"+userArgsFile)
			}
			result = append(result, "@"+argsFile, "nogui")
			return result
		}
	}

	// LaunchConfig нічого не називає навіть після resolveLaunchConfig —
	// лоадер справді не встановлений на диску (розділ 1.2: у цьому
	// випадку конфіг лишається порожнім навмисно, щоб зламана установка
	// проявлялась як помилка, а не мовчки ставала vanilla). buildCommand
	// не має способу повернути помилку (сигнатура []string), тому
	// віддаємо той самий vanilla-фолбек, що й раніше — Manager.Start()
	// однаково побачить нездатність java знайти клас модів і впаде з
	// осмисленою помилкою в консолі сервера, а не мовчки запуститься.
	_ = vendorPath // залишено як параметр для симетрії двох викликів і майбутніх діагностичних повідомлень
	return []string{"-jar", filepath.Join(srv.Dir, "server.jar"), "nogui"}
}

// findServerJar шукає основний jar-файл у папці сервера.
func findServerJar(dir string) string {
	// Шукаємо paper-*.jar або purpur-*.jar або server.jar
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "server.jar"
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "paper-") && strings.HasSuffix(name, ".jar") {
			return name
		}
		if strings.HasPrefix(name, "purpur-") && strings.HasSuffix(name, ".jar") {
			return name
		}
	}
	return "server.jar"
}

// parseArgs розбиває рядок аргументів на слайс (простий спліт по пробілах).
func parseArgs(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return strings.Fields(s)
}
