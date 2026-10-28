// diagnostics.go — діагностика: вільний порт, перевірка RAM, Java.
package servers

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CheckPortResult — результат перевірки порту.
type CheckPortResult struct {
	Free      bool   `json:"free"`
	Holder    string `json:"holder,omitempty"`
	OurServer string `json:"ourServer,omitempty"`
}

// RunningChecker визначає, чи сервер із даним ID зараз запущений
// (реальний живий процес, а не просто запис у сторі). Реалізується
// Manager.IsRunning; передається окремо, щоб diagnostics.go не залежав
// від Manager напряму і легко тестувався.
type RunningChecker func(serverID string) bool

// isPortActuallyBound перевіряє реальну зайнятість TCP-порту.
//
// ГОЛОВНЕ ПРАВИЛО: порт «зайнятий» лише тоді, коли його ЗАРАЗ активно
// слухає якийсь процес. Сам факт того, що net.Listen не вдається, — НЕ
// доказ: після зупинки сервера ОС (Windows тим паче) тримає сокет у
// стані TIME_WAIT ще 30–120 секунд, і Listen на цей час хибно відмовляє.
// Саме через це лаунчер вічно писав «порт зайнятий» на ПОВНІСТЮ вільних
// портах — «вічно», бо кожна перевірка знову бачила TIME_WAIT-примару.
//
// Тому першочергово питаємо ОС, чи є СПРАВЖНІЙ LISTENING-сокет:
//   - Windows: netstat -ano (рядки з LISTENING);
//   - Linux:   ss -ltnH (fallback: lsof).
// Якщо утиліта працює — її відповідь і є істиною (LISTENING є → зайнято,
// немає → вільно, незалежно від TIME_WAIT). Ліші фолбеки (dial/listen)
// використовуються лише там, де утиліт немає (мінімальні системи).
func isPortActuallyBound(port int) bool {
	// 1. Пряма мова ОС про активні слухачі.
	if busy, holder, ok := activeListener(port); ok {
		_ = holder
		return busy
	}

	// 2. Фолбек без утиліт: dial — хтось приймає з'єднання на loopback.
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if err == nil {
		conn.Close()
		return true
	}

	// Listen-тест: вдається → вільно; ні — з великою ймовірністю зайнято
	// (але без утиліт ОС ми не можемо відрізнити LISTENING від TIME_WAIT,
	// тому тут ідемо на компроміс: для запуску сервера краще дізнатись про
	// реальну зайнятість із джерела, якому довіряємо — крок 1).
	ln, lErr := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if lErr == nil {
		ln.Close()
		return false
	}
	return true
}

// activeListener шукає СПРАВЖНІЙ активний слухач порту через утиліти ОС.
// Повертає ok=false, якщо жодна утиліта недоступна (тоді фолбек вище).
func activeListener(port int) (busy bool, holder string, ok bool) {
	if runtime.GOOS == "windows" {
		return activeListenerWindows(port)
	}
	return activeListenerUnix(port)
}

// ── Кеш знімка портів (1 c) ──────────────────────────────────────────
// netstat/ss — процеси ОС, їх запускати на кожен ітер порту у ScanFreePort
// (а там десятки ітерацій) надто дорого. Знімок валідний 1 секунду: цього
// достатньо для швидкого підбору, але достатньо свіжо, щоб не збрехати.
var (
	portSnapMu    sync.Mutex
	portSnapLines []string
	portSnapAt    time.Time
	portSnapOK    bool
)

func portSnapshot() ([]string, bool) {
	portSnapMu.Lock()
	defer portSnapMu.Unlock()
	if portSnapOK && time.Since(portSnapAt) < time.Second {
		return portSnapLines, true
	}
	var out []byte
	var err error
	if runtime.GOOS == "windows" {
		cmd := exec.Command("netstat", "-ano")
		hideConsoleWindow(cmd)
		out, err = cmd.CombinedOutput()
	} else {
		out, err = exec.Command("ss", "-ltnH").CombinedOutput()
	}
	if err != nil {
		portSnapOK = false
		portSnapLines = nil
		return nil, false
	}
	portSnapLines = strings.Split(string(out), "\n")
	portSnapAt = time.Now()
	portSnapOK = true
	return portSnapLines, true
}

// activeListenerWindows — netstat -ano: рядок із LISTENING і локальним
// адресом :<port>. Якщо netstat запустився — ми довіряємо його відповіді
// (ok=true), навіть коли LISTENING не знайдено: це й означає «вільно».
func activeListenerWindows(port int) (bool, string, bool) {
	lines, ok := portSnapshot()
	if !ok {
		return false, "", false
	}
	suffix := fmt.Sprintf(":%d", port)
	for _, line := range lines {
		lower := strings.ToLower(line)
		if !strings.Contains(lower, "listening") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		// Локальна адреса — друге поле (TCP 0.0.0.0:25565 LISTENING 1234).
		local := fields[1]
		if !strings.HasSuffix(local, suffix) {
			continue
		}
		return true, fmt.Sprintf("PID %s", fields[len(fields)-1]), true
	}
	return false, "", true
}

// activeListenerUnix — ss -ltnH (fallback: lsof). Порожній вивід = ніхто
// не слухає → вільно.
func activeListenerUnix(port int) (bool, string, bool) {
	suffix := fmt.Sprintf(":%d", port)
	if lines, ok := portSnapshot(); ok {
		for _, line := range lines {
			fields := strings.Fields(line)
			if len(fields) < 4 || fields[0] != "LISTEN" {
				continue
			}
			// ss: State Recv-Q Send-Q Local:Port Peer:Port
			if !strings.HasSuffix(fields[3], suffix) {
				continue
			}
			return true, firstLineHolder(line), true
		}
		return false, "", true
	}
	// ss недоступний — пробуємо lsof.
	if out, err := exec.Command("lsof", "-n", "-P", fmt.Sprintf("-iTCP:%d", port), "-sTCP:LISTEN").CombinedOutput(); err == nil {
		text := strings.TrimSpace(string(out))
		if text == "" {
			return false, "", true
		}
		return true, firstLineHolder(text), true
	}
	return false, "", false
}

func firstLineHolder(text string) string {
	line := text
	if i := strings.IndexByte(line, '\n'); i > 0 {
		line = line[:i]
	}
	line = strings.TrimSpace(line)
	if len(line) > 80 {
		line = line[:80]
	}
	return line
}

// CheckPortFree перевіряє, чи вільний порт для запуску сервера.
// excludeServerID — ID сервера, який ми зараз намагаємось запустити;
// його власний запис у ourServers ігнорується (інакше сервер завжди
// «займає» свій же порт, навіть коли він вимкнений).
// isRunning — визначає, чи сервер із map ourServers реально запущений
// (nil = вважати всіх нерунаючими і покладатись лише на реальний
// TCP-байнд).
func CheckPortFree(port int, ourServers map[string]*Server, excludeServerID string, isRunning RunningChecker) CheckPortResult {
	// Спершу перевіряємо НАШІ ЗАПУЩЕНІ сервери — це швидко і дає
	// зрозуміле ім'я замість голого "порт зайнятий".
	for _, srv := range ourServers {
		if srv.ID == excludeServerID {
			continue
		}
		if srv.Port != port {
			continue
		}
		if isRunning != nil && !isRunning(srv.ID) {
			// Запис у сторі є, але процес не запущений — порт цим
			// сервером зараз не займається.
			continue
		}
		return CheckPortResult{
			Free:      false,
			OurServer: srv.ID,
			Holder:    fmt.Sprintf("наш сервер «%s»", srv.Name),
		}
	}

	// Якщо САМ сервер (excludeServerID) зараз запущений — цей порт
	// тримає саме він, і для своєї ж перевірки (налаштування/діагностика)
	// він «вільний»: інакше чинний сервер вічно сварився б із самим собою.
	if excludeServerID != "" && isRunning != nil && isRunning(excludeServerID) {
		return CheckPortResult{Free: true}
	}

	// Реальна перевірка: хтось слухає порт прямо зараз?
	if isPortActuallyBound(port) {
		holder := detectPortHolder(port)
		return CheckPortResult{Free: false, Holder: holder}
	}
	return CheckPortResult{Free: true}
}

// ScanFreePort шукає вільний порт починаючи з preferred, ігноруючи
// порти власних запущених серверів (excludeServerID виключає себе).
func ScanFreePort(preferred int, ourServers map[string]*Server, excludeServerID string, isRunning RunningChecker) int {
	if preferred <= 0 || preferred > 65535 {
		preferred = 25565
	}
	for port := preferred; port < 65536; port++ {
		res := CheckPortFree(port, ourServers, excludeServerID, isRunning)
		if res.Free {
			return port
		}
	}
	return preferred
}

// CheckRAMResult — результат перевірки пам'яті.
type CheckRAMResult struct {
	Ok             bool `json:"ok"`
	Available      int  `json:"available"`
	Total          int  `json:"total"`
	UsedByLauncher int  `json:"usedByLauncher"`
	UsedByServers  int  `json:"usedByServers"`
}

// CheckRAM перевіряє, чи вистачить RAM для сервера.
func CheckRAM(ramMB int, usedByServers int) CheckRAMResult {
	totalMB, availMB := getSystemRAM()

	// Залишаємо ОС 1.5 ГБ.
	osReserveMB := 1500
	available := availMB - osReserveMB - usedByServers
	if available < 0 {
		available = 0
	}

	return CheckRAMResult{
		Ok:            ramMB <= available,
		Available:     available,
		Total:         totalMB,
		UsedByServers: usedByServers,
	}
}

// RecommendRAMMB рекомендує RAM для сервера за фізичною пам'яттю та ядром.
func RecommendRAMMB(totalPhysMB int, core string) int {
	// Базова рекомендація за обсягом пам'яті.
	var base int
	switch {
	case totalPhysMB >= 16384:
		base = 4096
	case totalPhysMB >= 12288:
		base = 3584
	case totalPhysMB >= 8192:
		base = 3072
	case totalPhysMB >= 6144:
		base = 2560
	default:
		base = 2048
	}

	// Paper/Purpur потребують більше.
	if core == CorePaper || core == CorePurpur {
		base += 512
	}
	// Проксі не тримає світів: Velocity рекомендує 512 МБ, на великих
	// мережах (сотні гравців) — 1 ГБ.
	if core == CoreVelocity {
		if totalPhysMB >= 16384 {
			return 1024
		}
		return 512
	}

	// Не виділяємо більше 75% фізичної.
	maxAllowed := totalPhysMB * 75 / 100
	if maxAllowed < 1024 {
		maxAllowed = 1024
	}
	if base > maxAllowed {
		base = maxAllowed
	}

	return base
}

// detectPortHolder намагається визначити, який процес займає порт.
func detectPortHolder(port int) string {
	if runtime.GOOS == "windows" {
		return detectPortHolderWindows(port)
	}
	return fmt.Sprintf("порт %d зайнятий", port)
}

func detectPortHolderWindows(port int) string {
	suffix := fmt.Sprintf(":%d", port)
	if lines, ok := portSnapshot(); ok {
		for _, line := range lines {
			lower := strings.ToLower(line)
			if !strings.Contains(lower, "listening") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) > 4 && strings.HasSuffix(fields[1], suffix) {
				return fmt.Sprintf("PID %s", fields[len(fields)-1])
			}
		}
		return fmt.Sprintf("порт %d зайнятий", port)
	}
	cmd := exec.Command("netstat", "-ano")
	hideConsoleWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("порт %d зайнятий", port)
	}
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		if strings.Contains(line, suffix) && strings.Contains(line, "LISTENING") {
			fields := strings.Fields(line)
			if len(fields) > 4 {
				pid := fields[len(fields)-1]
				return fmt.Sprintf("PID %s", pid)
			}
		}
	}
	return fmt.Sprintf("порт %d зайнятий", port)
}

// ── Кеш результатів FindJavaPath ─────────────────────────────────────────
//
// КРИТИЧНО ДЛЯ ШВИДКОСТІ. FindJavaPath СПАВНИТЬ процеси (`java -version` по
// кандидатах), а ListServers → toView викликав його для КОЖНОГО сервера на
// КОЖНОМУ опитуванні списку. Бібліотека серверів перепитує список раз на
// 2 секунди, поки хоч один сервер живий — тобто на слабкому залізі це були
// десятки запусків Java за секунди (найдовша частина «завантаження
// серверів»). Кеш з TTL прибирає процеси з гарячого шляху повністю:
// позитивний результат перевикористовується, негативний теж (щоб не шукати
// кожні 2 секунди те, чого немає).
type javaPathCacheEntry struct {
	path string
	at   time.Time
}

var (
	javaPathCache    sync.Map // javaMajor(int) → javaPathCacheEntry
	javaPathCacheTTL = 60 * time.Second
)

// InvalidateJavaPathCache скидає кеш (викликати після встановлення Java,
// щоб свіжовстановлена версія підхопилась одразу, а не через TTL).
func InvalidateJavaPathCache() {
	javaPathCache.Range(func(k, _ any) bool { javaPathCache.Delete(k); return true })
}

// FindJavaPath шукає java за major версією (з кешем, див. вище).
func FindJavaPath(javaMajor int) (string, error) {
	if cached, ok := javaPathCache.Load(javaMajor); ok {
		e := cached.(javaPathCacheEntry)
		if time.Since(e.at) < javaPathCacheTTL {
			if e.path == "" {
				return "", fmt.Errorf("Java %d не знайдена", javaMajor)
			}
			return e.path, nil
		}
		javaPathCache.Delete(javaMajor)
	}
	path, err := findJavaPathUncached(javaMajor)
	if err != nil {
		javaPathCache.Store(javaMajor, javaPathCacheEntry{path: "", at: time.Now()})
		return "", err
	}
	javaPathCache.Store(javaMajor, javaPathCacheEntry{path: path, at: time.Now()})
	return path, nil
}

// findJavaPathUncached — власне пошук (без кешу): саме тут спавняться процеси.
func findJavaPathUncached(javaMajor int) (string, error) {
	// Спроба 0: Java лаунчера (встановлена через java.Installer).
	// Перевіряємо ПЕРШОЮ, бо лаунчер завжди має потрібну версію.
	for _, c := range getLauncherJavaCandidates(javaMajor) {
		checkCmd := exec.Command(c, "-version")
		hideConsoleWindow(checkCmd)
		if _, err := checkCmd.CombinedOutput(); err == nil {
			return c, nil
		}
	}

	// Спроба 1: java -version з PATH.
	verCmd := exec.Command("java", "-version")
	hideConsoleWindow(verCmd)
	out, err := verCmd.CombinedOutput()
	if err == nil {
		major := parseJavaMajorFromOutput(string(out))
		if major == javaMajor || javaMajor == 0 {
			javaPath, _ := exec.LookPath("java")
			if javaPath != "" {
				return javaPath, nil
			}
		}
	}

	// Спроба 2: шукаємо у типових місцях.
	candidates := javaCandidates(javaMajor)
	for _, c := range candidates {
		checkCmd := exec.Command(c, "-version")
		hideConsoleWindow(checkCmd)
		if _, err := checkCmd.CombinedOutput(); err == nil {
			return c, nil
		}
	}

	return "", fmt.Errorf("Java %d не знайдена", javaMajor)
}

func parseJavaMajorFromOutput(output string) int {
	// 'openjdk version "17.0.2"' or 'java version "1.8.0_361"'
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Шукаємо "version X.Y.Z" або "version X.Y"
		idx := strings.Index(line, "version \"")
		if idx < 0 {
			continue
		}
		rest := line[idx+9:]
		end := strings.Index(rest, "\"")
		if end < 0 {
			continue
		}
		ver := rest[:end]
		parts := strings.SplitN(ver, ".", 3)
		if len(parts) < 2 {
			continue
		}
		major, _ := strconv.Atoi(parts[0])
		minor, _ := strconv.Atoi(parts[1])
		if major == 1 && minor > 0 {
			return minor
		}
		return major
	}
	return 0
}

func javaCandidates(major int) []string {
	var paths []string
	if runtime.GOOS == "windows" {
		// Шукаємо в Program Files та .jdks
		programFiles := []string{
			`C:\Program Files\Eclipse Adoptium`,
			`C:\Program Files\AdoptOpenJDK`,
			`C:\Program Files\Java`,
			`C:\Program Files\Microsoft`,
			`C:\Program Files\Zulu`,
		}
		for _, pf := range programFiles {
			pattern := fmt.Sprintf("*jdk*%d*", major)
			matches, _ := filepath.Glob(filepath.Join(pf, pattern, "bin", "java.exe"))
			paths = append(paths, matches...)
		}
		// JAVA_HOME
		if jh := strings.TrimSpace(execEnv("JAVA_HOME")); jh != "" {
			paths = append(paths, filepath.Join(jh, "bin", "java.exe"))
		}
	} else {
		// Unix
		paths = append(paths,
			fmt.Sprintf("/usr/lib/jvm/java-%d-open/bin/java", major),
			fmt.Sprintf("/usr/lib/jvm/java-%d/bin/java", major),
		)
	}
	return paths
}

func execEnv(name string) string {
	// os.Getenv читає змінну оточення напряму — без спавну cmd.exe.
	// Раніше тут викликався `cmd /c echo %VAR%`, що на кожен виклик
	// відкривало власне консольне вікно (навіть з CREATE_NO_WINDOW це
	// зайвий процес на рівному місці — Go має прямий доступ до env).
	return strings.TrimSpace(os.Getenv(name))
}

// launcherJavaDir — тека Java лаунчера (встановлюється з app.go).
// FindJavaPath перевіряє її ПЕРШОЮ, бо лаунчер завжди ставить Java
// у свій javaDir/java-<major>/bin/javaw.exe.
var launcherJavaDir string

// SetLauncherJavaDir встановлює шлях до теки Java лаунчера.
// Викликається з rewireComponents/app.go при старті.
func SetLauncherJavaDir(dir string) { launcherJavaDir = dir }

// getLauncherJavaCandidates повертає кандидати Java з теки лаунчера.
// major=0 = будь-який Java (шукати у всіх java-*/ підтеках).
func getLauncherJavaCandidates(major int) []string {
	if launcherJavaDir == "" {
		return nil
	}
	exe := "java"
	if runtime.GOOS == "windows" {
		exe = "javaw.exe"
	}
	var paths []string
	// Спочатку — конкретний мажор (пріоритет).
	if major > 0 {
		paths = append(paths, filepath.Join(launcherJavaDir, fmt.Sprintf("java-%d", major), "bin", exe))
	}
	// Якщо major=0 — шукаємо в УСІХ java-*/ підтеках (базова + мажорні).
	if major == 0 {
		matches, _ := filepath.Glob(filepath.Join(launcherJavaDir, "java-*", "bin", exe))
		paths = append(paths, matches...)
	}
	// Корінь: javaDir/bin/javaw.exe (стара/базова Java 21 — ставиться майстром).
	paths = append(paths, filepath.Join(launcherJavaDir, "bin", exe))
	return paths
}
