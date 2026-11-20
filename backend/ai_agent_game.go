package main

// ai_agent_game.go — керування грою для ШІ-агента через Shaurma Game Agent (game-agent/).
//
// Принципи:
//   - Лише ШІ, не гравець: агент (java -javaagent) підкладається ТІЛЬКИ у запуски, ініційовані ШІ-агентом,
//     і лише під окремим акаунтом (за замовчуванням нік «Claude», офлайн), ніколи під акаунтом гравця.
//   - Лише локально: ШІ може заходити лише на локальні адреси (127.x, приватні мережі, link-local) і в одиночні
//     світи. Це перевіряється тут і ще раз усередині агента (мережева заборона на рівні JVM).
//   - Агент лежить у теці компонентів лаунчера (окремо від оновлення лаунчера) і оновлюється лише цим
//     інструментом: завантаження з фіксованого місця (GitHub release цього репозиторію) з перевірками.

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"shaurma-launcher-backend/internal/console"
	"shaurma-launcher-backend/internal/launchlog"
	"shaurma-launcher-backend/internal/model"
)

//go:embed ai_agent_skin.png
var aiAgentSkinPNG []byte

const (
	gameAgentJarName = "shaurma-game-agent.jar"
	gameAgentMinSize = 20 << 10
	gameAgentMaxSize = 4 << 20
	gameAgentMaxUnz  = 24 << 20 // стеля розпакованого розміру (захист від zip-бомби)
	aiDefaultNick    = "Claude"
)

var aiNickRe = regexp.MustCompile(`^[A-Za-z0-9_]{3,16}$`)

// nickOrDefault — нік основного акаунта ШІ зі стану (валідний або типовий).
func (s aiAgentState) nickOrDefault() string {
	if aiNickRe.MatchString(s.AgentNick) {
		return s.AgentNick
	}
	return aiDefaultNick
}

// aiClientNick — нік клієнта №idx: 1 → основний; n>1 → <основний>_n (із скороченням до 16 символів).
func aiClientNick(base string, idx int) string {
	if idx <= 1 {
		return base
	}
	suf := "_" + strconv.Itoa(idx)
	if len(base)+len(suf) > 16 {
		base = base[:16-len(suf)]
	}
	return base + suf
}

// aiIsAINick — чи нік належить ШІ: основний або <основний>_N (N=2..9).
func aiIsAINick(nick, base string) bool {
	if strings.EqualFold(nick, base) {
		return true
	}
	for n := 2; n <= 9; n++ {
		if strings.EqualFold(nick, aiClientNick(base, n)) {
			return true
		}
	}
	return false
}

var (
	// Фіксоване місце завантаження: GitHub release цього репозиторію (збирає й публікує CI game-agent.yml).
	gameAgentReleaseBase = "https://github.com/B23101/Shaurma-Launcher/releases/download/game-agent"
	// gameAgentFetch — точка підміни мережі в тестах.
	gameAgentFetch = func(url string, limit int64) ([]byte, error) {
		if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://127.0.0.1:") {
			return nil, errors.New("дозволено лише https")
		}
		c := &http.Client{Timeout: 60 * time.Second}
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "shaurma-launcher-game-agent-updater")
		resp, err := c.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
		if err != nil {
			return nil, err
		}
		if int64(len(body)) > limit {
			return nil, fmt.Errorf("файл більший за %d байт", limit)
		}
		return body, nil
	}
)

// ── Компонент: тека, перевірка, оновлення ────────────────────────────────

// GameAgentStatus — стан компонента для UI та інструментів.
type GameAgentStatus struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	SHA256    string `json:"sha256,omitempty"`
	SizeKB    int64  `json:"sizeKb,omitempty"`
	// AutoUpdate — чи увімкнено автоперевірку й автооновлення при запуску гри ШІ.
	AutoUpdate bool   `json:"autoUpdate"`
	Path       string `json:"path,omitempty"`
	Dir        string `json:"dir"`
	Source     string `json:"source"`
	Error      string `json:"error,omitempty"`
}

func dirWritable(dir string) bool {
	if os.MkdirAll(dir, 0o755) != nil {
		return false
	}
	f, err := os.CreateTemp(dir, ".w-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	_ = os.Remove(name)
	return true
}

// gameAgentDirs — теки пошуку/встановлення: біля компонентів лаунчера, фолбек — тека конфігурації.
func (a *App) gameAgentDirs() []string {
	var out []string
	if d := backendInitConfig.InstallDir; d != "" {
		out = append(out, filepath.Join(d, "game-agent"))
	}
	out = append(out, filepath.Join(a.cfg.ConfigDir(), "components", "game-agent"))
	return out
}

// gameAgentInstallDir — куди писати (перша тека, у яку можна).
func (a *App) gameAgentInstallDir() string {
	dirs := a.gameAgentDirs()
	for _, d := range dirs {
		if dirWritable(d) {
			return d
		}
	}
	return dirs[len(dirs)-1]
}

type gameAgentInfo struct {
	Version string
	SHA256  string
}

// verifyGameAgentJar перевіряє вміст jar: формат, межі розміру, відсутність чужих пакетів і шляхів-втеч,
// маніфест агента. Повертає версію й SHA-256.
func verifyGameAgentJar(body []byte) (gameAgentInfo, error) {
	var info gameAgentInfo
	if len(body) < gameAgentMinSize || len(body) > gameAgentMaxSize {
		return info, fmt.Errorf("підозрілий розмір агента: %d байт", len(body))
	}
	if !bytes.HasPrefix(body, []byte{0x50, 0x4b, 0x03, 0x04}) {
		return info, errors.New("не jar/zip")
	}
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return info, fmt.Errorf("jar пошкоджений: %w", err)
	}
	var unz uint64
	var manifest string
	hasAgent := false
	for _, f := range zr.File {
		n := f.Name
		if strings.Contains(n, "..") || strings.HasPrefix(n, "/") || strings.Contains(n, "\\") {
			return info, fmt.Errorf("небезпечний шлях у jar: %q", n)
		}
		if !(n == "dev/" || strings.HasPrefix(n, "dev/shaurma/") || n == "META-INF/" || strings.HasPrefix(n, "META-INF/")) {
			return info, fmt.Errorf("у jar є сторонній вміст: %q", n)
		}
		unz += f.UncompressedSize64
		if unz > gameAgentMaxUnz {
			return info, errors.New("розпакований розмір завеликий")
		}
		if n == "dev/shaurma/agent/Agent.class" {
			hasAgent = true
		}
		if n == "META-INF/MANIFEST.MF" {
			rc, err := f.Open()
			if err != nil {
				return info, err
			}
			b, _ := io.ReadAll(io.LimitReader(rc, 64<<10))
			rc.Close()
			manifest = string(b)
		}
	}
	if !hasAgent {
		return info, errors.New("у jar немає dev/shaurma/agent/Agent.class")
	}
	m := strings.ReplaceAll(manifest, "\r\n", "\n")
	if !strings.Contains(m, "\nPremain-Class: dev.shaurma.agent.Agent") {
		return info, errors.New("маніфест: Premain-Class не dev.shaurma.agent.Agent")
	}
	if !strings.Contains(m, "\nCan-Retransform-Classes: true") {
		return info, errors.New("маніфест: немає Can-Retransform-Classes")
	}
	for _, l := range strings.Split(m, "\n") {
		if strings.HasPrefix(l, "Implementation-Version:") {
			info.Version = strings.TrimSpace(strings.TrimPrefix(l, "Implementation-Version:"))
		}
	}
	sum := sha256.Sum256(body)
	info.SHA256 = hex.EncodeToString(sum[:])
	return info, nil
}

// gameAgentJarPath повертає шлях до встановленого й перевіреного jar.
func (a *App) gameAgentJarPath() (string, GameAgentStatus) {
	st := GameAgentStatus{Dir: a.gameAgentInstallDir(), Source: gameAgentReleaseBase}
	for _, d := range a.gameAgentDirs() {
		p := filepath.Join(d, gameAgentJarName)
		body, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		info, err := verifyGameAgentJar(body)
		if err != nil {
			st.Error = err.Error()
			continue
		}
		st.Installed, st.Version, st.SHA256, st.SizeKB, st.Path, st.Dir, st.Error = true, info.Version, info.SHA256, int64(len(body)>>10), p, d, ""
		return p, st
	}
	return "", st
}

// GetAIGameAgentStatus — стан агента (компонент у теці компонентів).
func (a *App) GetAIGameAgentStatus() GameAgentStatus {
	_, st := a.gameAgentJarPath()
	aiAgent.mu.Lock()
	a.aiAgentLoad()
	st.AutoUpdate = !aiAgent.st.NoAgentAutoUpdate
	aiAgent.mu.Unlock()
	return st
}

var agentCheck struct {
	mu   sync.Mutex
	last time.Time
}

// agentCheckEvery — як часто перевіряти оновлення агента при запусках гри ШІ.
const agentCheckEvery = 15 * time.Minute

// aiEnsureAgentFresh — автоперевірка компонента перед запуском гри ШІ. auto=false (вимкнено в налаштуваннях):
// мережі не чіпає. У local-only збірці мережі не чіпає. Збій мережі не блокує запуск (працюємо з встановленим).
// Повертає коротку примітку для користувача/агента або "".
func (a *App) aiEnsureAgentFresh(auto bool) string {
	if isLocalOnlyBuild() {
		return ""
	}
	_, cur := a.gameAgentJarPath()
	if !auto {
		if !cur.Installed {
			return "агент не встановлено, автооновлення вимкнено (встановіть вручну: update_game_agent)"
		}
		return ""
	}
	agentCheck.mu.Lock()
	recent := time.Since(agentCheck.last) < agentCheckEvery
	agentCheck.mu.Unlock()
	if recent && cur.Installed {
		return ""
	}
	sumTxt, err := gameAgentFetch(gameAgentReleaseBase+"/"+gameAgentJarName+".sha256", 4096)
	if err != nil {
		if cur.Installed {
			return "оновлення агента перевірити не вдалося, працюємо з встановленою версією: " + err.Error()
		}
		return "агента немає і завантажити не вдалося: " + err.Error()
	}
	agentCheck.mu.Lock()
	agentCheck.last = time.Now()
	agentCheck.mu.Unlock()
	want := strings.ToLower(strings.TrimSpace(strings.Fields(string(sumTxt) + " ")[0]))
	if cur.Installed && cur.SHA256 == want {
		return ""
	}
	st, err := a.UpdateAIGameAgent()
	if err != nil {
		if cur.Installed {
			return "автооновлення агента не вдалося, працюємо з встановленою версією: " + err.Error()
		}
		return "агента не встановлено: " + err.Error()
	}
	return "компонент агента автоматично оновлено до версії " + st.Version
}

// UpdateAIGameAgent безпечно завантажує агента з фіксованого релізу GitHub і встановлює його.
// Перевірки: лише https; ліміт розміру; SHA-256 із файлу .sha256 цього ж релізу; структура jar
// (маніфест, відсутність чужого коду й шляхів-втеч); атомарна заміна із збереженням попередньої
// версії (.prev). Оновлення лаунчера цього не стосується.
func (a *App) UpdateAIGameAgent() (GameAgentStatus, error) {
	if isLocalOnlyBuild() {
		return a.GetAIGameAgentStatus(), errors.New("локальна збірка (local-only.flag): оновлення з мережі вимкнено, агент береться з теки game-agent " +
			"поруч із лаунчером. Його збирає тестовий білд Pack Manager або scripts/build-local-full.ps1 (потрібні JDK 17+ і Maven)")
	}
	jar, err := gameAgentFetch(gameAgentReleaseBase+"/"+gameAgentJarName, gameAgentMaxSize)
	if err != nil {
		return a.GetAIGameAgentStatus(), fmt.Errorf("завантаження агента: %w", err)
	}
	sumTxt, err := gameAgentFetch(gameAgentReleaseBase+"/"+gameAgentJarName+".sha256", 4096)
	if err != nil {
		return a.GetAIGameAgentStatus(), fmt.Errorf("завантаження контрольної суми: %w", err)
	}
	want := strings.ToLower(strings.TrimSpace(strings.Fields(string(sumTxt) + " ")[0]))
	got := sha256.Sum256(jar)
	if len(want) != 64 || want != hex.EncodeToString(got[:]) {
		return a.GetAIGameAgentStatus(), errors.New("SHA-256 агента не збігається з опублікованим: файл не встановлено")
	}
	if _, err := verifyGameAgentJar(jar); err != nil {
		return a.GetAIGameAgentStatus(), fmt.Errorf("перевірка агента: %w", err)
	}
	dir := a.gameAgentInstallDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return a.GetAIGameAgentStatus(), err
	}
	dst := filepath.Join(dir, gameAgentJarName)
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, jar, 0o644); err != nil {
		return a.GetAIGameAgentStatus(), err
	}
	if _, err := os.Stat(dst); err == nil {
		_ = os.Remove(dst + ".prev")
		_ = os.Rename(dst, dst+".prev")
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return a.GetAIGameAgentStatus(), err
	}
	launchlog.Component("AIAgent").Info("встановлено Shaurma Game Agent (%s)", want[:12])
	return a.GetAIGameAgentStatus(), nil
}

// ── Акаунт агента ────────────────────────────────────────────────────────

func (a *App) aiAgentNickNow() string {
	aiAgent.mu.Lock()
	a.aiAgentLoad()
	n := aiAgent.st.nickOrDefault()
	known := aiAgent.st.knownNicks()
	aiAgent.mu.Unlock()
	aiSkinBase.Store(n)
	aiKnownNicks.Store(known)
	return n
}

var aiSkinBase atomic.Value

// aiSkinBaseNick — основний нік ШІ для скін-проксі (оновлюється при кожному зверненні до стану).
func aiSkinBaseNick() string {
	if v, ok := aiSkinBase.Load().(string); ok && v != "" {
		return v
	}
	return aiDefaultNick
}

// aiMaxClients — дозволена кількість одночасних клієнтів ШІ.
func (a *App) aiMaxClients() int {
	aiAgent.mu.Lock()
	a.aiAgentLoad()
	n := aiAgent.st.maxClients()
	aiAgent.mu.Unlock()
	return n
}

// aiNickFor — нік клієнта №client (1..MaxClients); помилка, якщо номер поза межами.
func (a *App) aiNickFor(client int) (string, error) {
	if client < 1 {
		client = 1
	}
	if m := a.aiMaxClients(); client > m {
		return "", fmt.Errorf("клієнт %d не дозволений: максимум %d (Налаштування → ШІ-агент → «Клієнтів гри»)", client, m)
	}
	return aiClientNick(a.aiAgentNickNow(), client), nil
}

// aiNickFromClient — нік гравця ШІ за назвою MCP-клієнта: його визначає сам агент, не користувач
// (Claude → Claude, ChatGPT/Codex → ChatGPT, …). Невідомі клієнти: очищена назва.
func aiNickFromClient(client string) string {
	c := strings.ToLower(strings.TrimSpace(client))
	switch {
	case c == "":
		return ""
	case strings.Contains(c, "claude") || strings.Contains(c, "anthropic"):
		return "Claude"
	case strings.Contains(c, "chatgpt") || strings.Contains(c, "openai") || strings.Contains(c, "codex") || c == "gpt" || strings.HasPrefix(c, "gpt-"):
		return "ChatGPT"
	case strings.Contains(c, "gemini"):
		return "Gemini"
	case strings.Contains(c, "copilot"):
		return "Copilot"
	case strings.Contains(c, "cursor"):
		return "Cursor"
	case strings.Contains(c, "windsurf"):
		return "Windsurf"
	}
	var b strings.Builder
	for _, r := range client {
		if r < 128 && (r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
			b.WriteRune(r)
		}
	}
	n := b.String()
	if len(n) > 16 {
		n = n[:16]
	}
	if len(n) < 3 {
		return ""
	}
	return strings.ToUpper(n[:1]) + n[1:]
}

var aiKnownNicks atomic.Value // []string — усі ніки ШІ для скін-проксі

func aiKnownNickList() []string {
	if v, ok := aiKnownNicks.Load().([]string); ok && len(v) > 0 {
		return v
	}
	return []string{aiDefaultNick}
}

// aiIsKnownAINick — чи нік належить будь-якому відомому агенту (основний або _2.._9).
func aiIsKnownAINick(nick string) bool {
	for _, b := range aiKnownNickList() {
		if aiIsAINick(nick, b) {
			return true
		}
	}
	return false
}

func (st aiAgentState) knownNicks() []string {
	out := []string{st.nickOrDefault()}
	for _, n := range st.AgentNicks {
		if aiNickRe.MatchString(n) {
			dup := false
			for _, o := range out {
				dup = dup || strings.EqualFold(o, n)
			}
			if !dup {
				out = append(out, n)
			}
		}
	}
	return out
}

// aiAgentApplyClientIdentity — при initialize агент «представляється»: нік береться з назви MCP-клієнта.
// Поки йде гра ШІ, ідентичність не змінюється (сесії й акаунти прив'язані до ніка).
func (a *App) aiAgentApplyClientIdentity(client string) {
	nick := aiNickFromClient(client)
	if nick == "" {
		return
	}
	aiGames.mu.Lock()
	busy := len(aiGames.m) > 0
	aiGames.mu.Unlock()
	aiAgent.mu.Lock()
	a.aiAgentLoad()
	if !busy && aiAgent.st.AgentNick != nick {
		aiAgent.st.AgentNick = nick
	}
	have := false
	for _, n := range aiAgent.st.AgentNicks {
		have = have || strings.EqualFold(n, nick)
	}
	if !have && len(aiAgent.st.AgentNicks) < 8 {
		aiAgent.st.AgentNicks = append(aiAgent.st.AgentNicks, nick)
	}
	_ = a.aiAgentSave()
	known := aiAgent.st.knownNicks()
	cur := aiAgent.st.nickOrDefault()
	aiAgent.mu.Unlock()
	aiKnownNicks.Store(known)
	aiSkinBase.Store(cur)
}

// aiAgentSetNick — агент сам обирає нік (інструмент set_agent_nick). Не під час гри ШІ; не нік ліцензійного акаунта.
func (a *App) aiAgentSetNick(nick string) error {
	nick = strings.TrimSpace(nick)
	if !aiNickRe.MatchString(nick) {
		return errors.New("нік: 3–16 символів, лише латиниця, цифри й _")
	}
	for _, acc := range a.cfg.GetAccounts() {
		if (acc.IsLicensed || acc.Type == "microsoft") && aiIsAINick(acc.Username, nick) {
			return errors.New("цей нік (або його варіант _2.._9) належить ліцензійному акаунту гравця: оберіть інший")
		}
	}
	aiGames.mu.Lock()
	a.aiPruneGames()
	busy := len(aiGames.m) > 0
	aiGames.mu.Unlock()
	if busy {
		return errors.New("зараз працює гра ШІ: змінити нік можна після її завершення")
	}
	aiAgent.mu.Lock()
	a.aiAgentLoad()
	aiAgent.st.AgentNick = nick
	have := false
	for _, n := range aiAgent.st.AgentNicks {
		have = have || strings.EqualFold(n, nick)
	}
	if !have && len(aiAgent.st.AgentNicks) < 8 {
		aiAgent.st.AgentNicks = append(aiAgent.st.AgentNicks, nick)
	}
	err := a.aiAgentSave()
	known := aiAgent.st.knownNicks()
	aiAgent.mu.Unlock()
	aiKnownNicks.Store(known)
	aiSkinBase.Store(nick)
	return err
}

// ensureAIAccount повертає ВНУТРІШНІЙ офлайн-акаунт агента з цим ніком. Він живе лише в пам'яті:
// не потрапляє в accounts.json, не стає активним і не показується в списку акаунтів лаунчера.
// Ліцензійний акаунт гравця використати не можна ніколи.
func (a *App) ensureAIAccount(nick string) (model.Account, error) {
	for _, acc := range a.cfg.GetUserAccounts() {
		if strings.EqualFold(acc.Username, nick) && (acc.IsLicensed || acc.Type == "microsoft") {
			return model.Account{}, fmt.Errorf("нік «%s» належить ліцензійному акаунту гравця: змініть нік агента в налаштуваннях", nick)
		}
	}
	acc, err := a.pirate.Login(nick)
	if err != nil {
		return model.Account{}, err
	}
	acc.ID = "ai-agent-" + strings.ToLower(nick)
	a.cfg.SetAgentAccount(*acc)
	return *acc, nil
}

// ── Лише локальні адреси ─────────────────────────────────────────────────

// aiLocalServerAddr перевіряє, що адреса сервера локальна: localhost або IP з loopback, приватних
// мереж, link-local, IPv6 ULA. Імена хостів (крім localhost) заборонені.
func aiLocalServerAddr(addr string) error {
	addr = strings.TrimSpace(addr)
	if addr == "" || len(addr) > 255 || strings.ContainsAny(addr, " \r\n\t\"'/\\@") {
		return errors.New("некоректна адреса сервера")
	}
	host := addr
	if h, _, err := net.SplitHostPort(addr); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("ШІ може заходити лише на локальні адреси (IP з локальної мережі або localhost), а не на %q", host)
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return nil
	}
	return fmt.Errorf("ШІ може заходити лише на локальні адреси: %s не локальна", ip)
}

// ── Сесії запусків ШІ (кілька клієнтів) ──────────────────────────────────

type aiGameSession struct {
	PackID      string
	AccountID   string
	Nick        string
	Client      int
	Port        int
	Token       string
	Started     time.Time
	SeenRunning bool
	WithAgent   bool
	Approved    bool // користувач дозволив керування грою в цьому сеансі
}

var aiGames struct {
	mu sync.Mutex
	m  map[string]*aiGameSession // ключ: packID|нік
}

func aiSessKey(packID, nick string) string { return packID + "|" + strings.ToLower(nick) }

func freeLoopbackPort() (int, error) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func randomToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// aiLaunchPrepare готує запуск клієнта ШІ №client: акаунт, перевірка локальної адреси, сесія керування (якщо дозволено).
func (a *App) aiLaunchPrepare(packID, server string, client int) (model.Account, map[string]any, error) {
	info := map[string]any{}
	if server != "" {
		if err := aiLocalServerAddr(server); err != nil {
			return model.Account{}, nil, err
		}
	}
	if client < 1 {
		client = 1
	}
	nick, err := a.aiNickFor(client)
	if err != nil {
		return model.Account{}, nil, err
	}
	if a.aiClientRunning(packID, nick) {
		return model.Account{}, nil, fmt.Errorf("клієнт %d (%s) на цій збірці вже запущений", client, nick)
	}
	acc, err := a.ensureAIAccount(nick)
	if err != nil {
		return model.Account{}, nil, err
	}
	info["account"] = acc.Username
	info["client"] = client
	info["note"] = "гру запущено під окремим акаунтом ШІ, не під вашим"

	aiAgent.mu.Lock()
	a.aiAgentLoad()
	st := aiAgent.st
	aiAgent.mu.Unlock()

	// Компонент агента: автоперевірка й автооновлення перед запуском (поза мʼютексом сесій: це мережа).
	if st.allows("gameControl") {
		if note := a.aiEnsureAgentFresh(!st.NoAgentAutoUpdate); note != "" {
			info["agent_update"] = note
		}
	}

	aiGames.mu.Lock()
	defer aiGames.mu.Unlock()
	if aiGames.m == nil {
		aiGames.m = map[string]*aiGameSession{}
	}
	delete(aiGames.m, aiSessKey(packID, nick))
	if st.allows("gameControl") {
		if _, gs := a.gameAgentJarPath(); !gs.Installed {
			info["game_control"] = "недоступне: агент не встановлено (викличте update_game_agent)"
		} else if port, err := freeLoopbackPort(); err != nil {
			info["game_control"] = "недоступне: " + err.Error()
		} else if tok, err := randomToken(); err != nil {
			info["game_control"] = "недоступне: " + err.Error()
		} else {
			aiGames.m[aiSessKey(packID, nick)] = &aiGameSession{PackID: packID, AccountID: acc.ID, Nick: acc.Username, Client: client,
				Port: port, Token: tok, Started: time.Now(), WithAgent: true}
			info["game_control"] = "підключиться після завантаження гри: game_status, game_screenshot, game_key… (для клієнта " + strconv.Itoa(client) + " — параметр client)"
		}
	} else {
		info["game_control"] = "вимкнено правами (Налаштування → ШІ-агент → Керування грою)"
	}
	return acc, info, nil
}

// aiLaunchRun запускає гру під акаунтом агента (блокує до завершення запуску); при помилці знімає сесію.
func (a *App) aiLaunchRun(packID string, acc model.Account, server string) error {
	err := a.LaunchInstanceShortcut(packID, acc.ID, server)
	if err != nil {
		aiGames.mu.Lock()
		delete(aiGames.m, aiSessKey(packID, acc.Username))
		aiGames.mu.Unlock()
	}
	return err
}

// aiGameJVMArgs — аргументи -javaagent: ЛИШЕ коли запуск ініційовано ШІ і йде під акаунтом агента.
func (a *App) aiGameJVMArgs(buildID string, account model.Account) []string {
	aiGames.mu.Lock()
	s := aiGames.m[aiSessKey(buildID, account.Username)]
	aiGames.mu.Unlock()
	if s == nil || !s.WithAgent || s.AccountID != account.ID || account.IsLicensed || account.Type == "microsoft" {
		return nil
	}
	jar, st := a.gameAgentJarPath()
	if !st.Installed {
		launchlog.Component("AIAgent").Warn("агент не встановлено — керування грою недоступне")
		return nil
	}
	return []string{"-javaagent:" + jar + "=port=" + strconv.Itoa(s.Port) + ";token=" + s.Token + ";nick=" + s.Nick}
}

// aiPruneGames прибирає завершені сесії (мʼютекс aiGames.mu має тримати викликач).
func (a *App) aiPruneGames() {
	for k, s := range aiGames.m {
		running := a.aiClientRunning(s.PackID, s.Nick)
		if running {
			s.SeenRunning = true
		}
		if (s.SeenRunning && !running) || time.Since(s.Started) > 45*time.Minute {
			delete(aiGames.m, k)
		}
	}
}

// aiGameFor знаходить сесію керування: за pack_id і номером клієнта (0 — не вказано).
func (a *App) aiGameFor(packID string, client int) (*aiGameSession, error) {
	aiGames.mu.Lock()
	defer aiGames.mu.Unlock()
	a.aiPruneGames()
	var cand []*aiGameSession
	for _, s := range aiGames.m {
		if (packID == "" || s.PackID == packID) && (client <= 0 || s.Client == client) {
			cand = append(cand, s)
		}
	}
	switch len(cand) {
	case 1:
		return cand[0], nil
	case 0:
		return nil, errors.New("немає сесії керування для цього клієнта: запустіть гру через launch_build (client=N) з правом «Керування грою» і встановленим агентом")
	}
	var names []string
	for _, s := range cand {
		names = append(names, s.PackID+" (клієнт "+strconv.Itoa(s.Client)+", "+s.Nick+")")
	}
	return nil, errors.New("активних сесій кілька — вкажіть pack_id і client: " + strings.Join(names, "; "))
}

// gameCall викликає команду агента всередині гри клієнта №client (0 — єдина активна сесія).
func (a *App) gameCall(packID string, client int, req map[string]any) (map[string]any, error) {
	s, err := a.aiGameFor(packID, client)
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(req)
	hreq, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:"+strconv.Itoa(s.Port)+"/cmd", bytes.NewReader(body))
	hreq.Header.Set("X-Shaurma-Token", s.Token)
	hreq.Header.Set("Content-Type", "application/json")
	cl := &http.Client{Timeout: 45 * time.Second}
	resp, err := cl.Do(hreq)
	if err != nil {
		return nil, errors.New("агент у грі не відповідає: гра ще завантажується, закрита або запущена без агента")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("некоректна відповідь агента (HTTP %d)", resp.StatusCode)
	}
	if ok, _ := out["ok"].(bool); !ok {
		msg, _ := out["error"].(string)
		if msg == "" {
			msg = "помилка агента (HTTP " + strconv.Itoa(resp.StatusCode) + ")"
		}
		return nil, errors.New(msg)
	}
	return out, nil
}

// aiImage — результат інструмента із зображенням (MCP image content).
type aiImage struct {
	Data []byte
	Mime string
	Text string
}

// ── Сесії ШІ vs сесії гравця ─────────────────────────────────────────────

// aiAccountIDFor повертає id акаунта агента з цим ніком, якщо він існує (без створення).
func (a *App) aiAccountIDFor(nick string) (string, bool) {
	for _, acc := range a.cfg.GetAccounts() {
		if strings.EqualFold(acc.Username, nick) && !acc.IsLicensed && acc.Type != "microsoft" && strings.HasPrefix(acc.ID, "ai-agent-") {
			return acc.ID, true
		}
	}
	return "", false
}

// aiClientRunning — чи йде гра цього клієнта ШІ (за ніком) на збірці. Гру гравця не враховує.
func (a *App) aiClientRunning(packID, nick string) bool {
	id, ok := a.aiAccountIDFor(nick)
	if !ok {
		return false
	}
	for _, s := range a.sessionsM.Snapshot() {
		if s.BuildID == packID && s.AccountID == id {
			return true
		}
	}
	return false
}

// aiSessionRunning — клієнт №1 (основний).
func (a *App) aiSessionRunning(packID string) bool {
	return a.aiClientRunning(packID, a.aiAgentNickNow())
}

// aiStopClient зупиняє ЛИШЕ гру клієнта ШІ №client; гру гравця на тій самій збірці не чіпає.
func (a *App) aiStopClient(packID string, client int) error {
	nick, err := a.aiNickFor(client)
	if err != nil {
		return err
	}
	id, ok := a.aiAccountIDFor(nick)
	if !ok {
		return errors.New("акаунта цього клієнта ШІ ще немає: гру не запущено")
	}
	for _, s := range a.sessionsM.Snapshot() {
		if s.BuildID == packID && s.AccountID == id {
			a.stoppedByUser.Store(true)
			if err := a.sessionsM.Stop(s.AccountID, s.BuildID); err != nil {
				a.stoppedByUser.Store(false)
				return err
			}
			return nil
		}
	}
	if a.aiPackRunning(packID) {
		return errors.New("ця збірка запущена гравцем або іншим клієнтом, а не цим клієнтом ШІ: агент її не зупиняє")
	}
	return errors.New("гра цього клієнта ШІ на цій збірці не запущена")
}

func (a *App) aiStopBuild(packID string) error { return a.aiStopClient(packID, 1) }

// aiConsolePageFor — сторінка консолі клієнта ШІ (буфер акаунта агента); якщо його немає — стандартна консоль збірки.
func (a *App) aiConsolePageFor(buildID string, client int, offset, limit int, level, query string, after uint64) console.Page {
	if nick, err := a.aiNickFor(client); err == nil {
		if id, ok := a.aiAccountIDFor(nick); ok {
			a.consoleBufMu.Lock()
			b := a.consoleBufs[consoleKey{accountID: id, buildID: buildID}]
			a.consoleBufMu.Unlock()
			if b != nil {
				return b.Page(offset, limit, level, query, after)
			}
		}
	}
	return a.GetConsolePage(buildID, offset, limit, level, query, after)
}

func (a *App) aiConsolePage(buildID string, offset, limit int, level, query string, after uint64) console.Page {
	return a.aiConsolePageFor(buildID, 1, offset, limit, level, query, after)
}

// aiClearConsole очищає буфер консолі клієнта №1 ШІ. Консоль гравця не чіпає ніколи.
func (a *App) aiClearConsole(buildID string) {
	if id, ok := a.aiAccountIDFor(a.aiAgentNickNow()); ok {
		a.consoleBufMu.Lock()
		b := a.consoleBufs[consoleKey{accountID: id, buildID: buildID}]
		a.consoleBufMu.Unlock()
		if b != nil {
			b.Clear()
		}
	}
}

// isLocalOnlyBuild — локальна збірка без оновлень з мережі (маркер local-only.flag біля лаунчера, див. updater).
func isLocalOnlyBuild() bool {
	d := backendInitConfig.InstallDir
	if d == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(d, "local-only.flag"))
	return err == nil
}
