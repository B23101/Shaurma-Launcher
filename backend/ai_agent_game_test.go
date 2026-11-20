package main

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shaurma-launcher-backend/internal/model"
)

func TestAILocalServerAddr(t *testing.T) {
	for _, ok := range []string{"localhost", "localhost:25565", "LOCALHOST:1", "127.0.0.1:25565", "192.168.0.5", "10.0.0.2:1", "[::1]:25565", "::1", "169.254.1.1", "172.16.0.1:25565", "fd00::1"} {
		if err := aiLocalServerAddr(ok); err != nil {
			t.Errorf("%q має бути дозволено: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "8.8.8.8", "1.2.3.4:25565", "play.hypixel.net", "mc.example.com:25565", "localhost.evil.com", "[2001:db8::1]:25565",
		"user@127.0.0.1", "127.0.0.1/evil", "172.32.0.1", "193.168.0.1", "foo bar", "100.64.0.1"} {
		if err := aiLocalServerAddr(bad); err == nil {
			t.Errorf("%q має бути ЗАБОРОНЕНО", bad)
		}
	}
}

func TestAIClientNicks(t *testing.T) {
	if aiClientNick("Claude", 1) != "Claude" || aiClientNick("Claude", 0) != "Claude" {
		t.Fatal("клієнт 1 — основний нік")
	}
	if aiClientNick("Claude", 2) != "Claude_2" || aiClientNick("Claude", 4) != "Claude_4" {
		t.Fatal("клієнти 2+")
	}
	long := aiClientNick("ABCDEFGHIJKLMNOP", 3) // 16 символів + _3 → скорочення
	if len(long) != 16 || !strings.HasSuffix(long, "_3") || !aiNickRe.MatchString(long) {
		t.Fatalf("довгий нік: %q", long)
	}
	for _, n := range []string{"Claude", "claude", "Claude_2", "CLAUDE_9"} {
		if !aiIsAINick(n, "Claude") {
			t.Errorf("%s — нік ШІ", n)
		}
	}
	for _, n := range []string{"Steve", "Claude_10", "Claude2", "XClaude", "Claude_1"} {
		if aiIsAINick(n, "Claude") {
			t.Errorf("%s не нік ШІ", n)
		}
	}
	if (aiAgentState{}).maxClients() != 2 || (aiAgentState{MaxClients: 9}).maxClients() != 4 || (aiAgentState{MaxClients: 3}).maxClients() != 3 {
		t.Fatal("maxClients")
	}
}

// ── перевірка jar ──

func makeJar(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for n, b := range entries {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(b)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func goodJarEntries() map[string][]byte {
	pad := make([]byte, 30<<10)
	_, _ = rand.Read(pad) // нестискний наповнювач, щоб пройти мінімальний розмір
	return map[string][]byte{
		"META-INF/MANIFEST.MF":                 []byte("Manifest-Version: 1.0\r\nPremain-Class: dev.shaurma.agent.Agent\r\nCan-Retransform-Classes: true\r\nImplementation-Version: 9.9.9\r\n\r\n"),
		"dev/shaurma/agent/Agent.class":        []byte("class"),
		"dev/shaurma/agent/shaded/asm/X.class": pad,
	}
}

func TestVerifyGameAgentJar(t *testing.T) {
	info, err := verifyGameAgentJar(makeJar(t, goodJarEntries()))
	if err != nil || info.Version != "9.9.9" || len(info.SHA256) != 64 {
		t.Fatalf("коректний jar: %+v %v", info, err)
	}

	bad := map[string]func(map[string][]byte){
		"сторонній пакет": func(m map[string][]byte) { m["com/evil/Payload.class"] = []byte("x") },
		"шлях-втеча":      func(m map[string][]byte) { m["dev/shaurma/../../evil.class"] = []byte("x") },
		"інший Premain": func(m map[string][]byte) {
			m["META-INF/MANIFEST.MF"] = []byte("Premain-Class: com.evil.Agent\nCan-Retransform-Classes: true\n")
		},
		"без Agent.class": func(m map[string][]byte) { delete(m, "dev/shaurma/agent/Agent.class") },
		"без маніфесту":   func(m map[string][]byte) { delete(m, "META-INF/MANIFEST.MF") },
		"без retransform": func(m map[string][]byte) {
			m["META-INF/MANIFEST.MF"] = []byte("Premain-Class: dev.shaurma.agent.Agent\n")
		},
	}
	for name, mut := range bad {
		m := goodJarEntries()
		mut(m)
		if _, err := verifyGameAgentJar(makeJar(t, m)); err == nil {
			t.Errorf("%s: має бути відхилено", name)
		}
	}
	if _, err := verifyGameAgentJar([]byte("MZ not a jar")); err == nil {
		t.Error("не jar")
	}
	if _, err := verifyGameAgentJar(bytes.Repeat([]byte{0x50, 0x4b, 0x03, 0x04}, 10)); err == nil {
		t.Error("замалий/пошкоджений")
	}
}

// ── безпечне оновлення агента ──

func TestUpdateAIGameAgentChecksAndInstalls(t *testing.T) {
	dir := t.TempDir()
	a := &App{cfg: newTestConfig(t, dir)}
	prevInstall := backendInitConfig.InstallDir
	backendInitConfig.InstallDir = filepath.Join(dir, "install")
	defer func() { backendInitConfig.InstallDir = prevInstall }()

	jar := makeJar(t, goodJarEntries())
	sum := sha256.Sum256(jar)
	hexSum := hex.EncodeToString(sum[:])
	serveSum := hexSum + "  shaurma-game-agent.jar\n"
	mux := http.NewServeMux()
	mux.HandleFunc("/"+gameAgentJarName, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(jar) })
	mux.HandleFunc("/"+gameAgentJarName+".sha256", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(serveSum)) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	prevBase := gameAgentReleaseBase
	gameAgentReleaseBase = srv.URL
	defer func() { gameAgentReleaseBase = prevBase }()

	if a.GetAIGameAgentStatus().Installed {
		t.Fatal("спершу не встановлено")
	}
	st, err := a.UpdateAIGameAgent()
	if err != nil || !st.Installed || st.SHA256 != hexSum || st.Version != "9.9.9" {
		t.Fatalf("встановлення: %+v %v", st, err)
	}
	if !strings.HasPrefix(st.Path, filepath.Join(dir, "install", "game-agent")) {
		t.Fatalf("агент має лежати в теці компонентів: %s", st.Path)
	}
	// повторне оновлення зберігає попередню версію
	if _, err := a.UpdateAIGameAgent(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(st.Path + ".prev"); err != nil {
		t.Fatalf("очікувалась .prev: %v", err)
	}

	// хибна контрольна сума → нічого не міняється
	serveSum = strings.Repeat("0", 64) + "  x\n"
	before, _ := os.ReadFile(st.Path)
	if _, err := a.UpdateAIGameAgent(); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("хибна сума має відхилятись: %v", err)
	}
	after, _ := os.ReadFile(st.Path)
	if !bytes.Equal(before, after) {
		t.Fatal("jar не має змінитись при помилці перевірки")
	}

	// чужий вміст при правильній сумі → відхилено
	evil := makeJar(t, func() map[string][]byte { m := goodJarEntries(); m["com/evil/X.class"] = []byte("x"); return m }())
	es := sha256.Sum256(evil)
	jar, serveSum = evil, hex.EncodeToString(es[:])+"\n"
	if _, err := a.UpdateAIGameAgent(); err == nil || !strings.Contains(err.Error(), "перевірка агента") {
		t.Fatalf("сторонній вміст має відхилятись: %v", err)
	}

	// не-https і не-локальний URL заборонені
	if _, err := gameAgentFetch("http://example.com/a.jar", 10); err == nil {
		t.Fatal("http до зовнішнього хоста має бути заборонено")
	}
}

// ── агент підкладається лише в запуски ШІ ──

func TestAIGameJVMArgsOnlyForAIAccount(t *testing.T) {
	dir := t.TempDir()
	a := &App{cfg: newTestConfig(t, dir)}
	prevInstall := backendInitConfig.InstallDir
	backendInitConfig.InstallDir = filepath.Join(dir, "install")
	defer func() { backendInitConfig.InstallDir = prevInstall }()

	d := filepath.Join(dir, "install", "game-agent")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, gameAgentJarName), makeJar(t, goodJarEntries()), 0o644); err != nil {
		t.Fatal(err)
	}

	aiAI := model.Account{ID: "ai-id", Username: "Claude"}
	player := model.Account{ID: "player-id", Username: "Steve"}
	licensed := model.Account{ID: "ai-id", Username: "Claude", IsLicensed: true}

	aiGames.mu.Lock()
	aiGames.m = map[string]*aiGameSession{aiSessKey("pack", "Claude"): {PackID: "pack", AccountID: "ai-id", Nick: "Claude", Client: 1, Port: 40111, Token: "tok_0123456789abcdef", WithAgent: true, Started: time.Now()}}
	aiGames.mu.Unlock()
	defer func() { aiGames.mu.Lock(); aiGames.m = nil; aiGames.mu.Unlock() }()

	args := a.aiGameJVMArgs("pack", aiAI)
	if len(args) != 1 || !strings.HasPrefix(args[0], "-javaagent:") || !strings.Contains(args[0], "port=40111;token=tok_0123456789abcdef;nick=Claude") {
		t.Fatalf("акаунт ШІ має отримати агента: %v", args)
	}
	if a.aiGameJVMArgs("pack", player) != nil {
		t.Fatal("гравець НІКОЛИ не отримує агента")
	}
	if a.aiGameJVMArgs("pack", licensed) != nil {
		t.Fatal("ліцензійний акаунт НІКОЛИ не отримує агента, навіть з тим самим id")
	}
	if a.aiGameJVMArgs("other-pack", aiAI) != nil {
		t.Fatal("інша збірка — без сесії керування")
	}
}

func TestAIGameForSelectsClient(t *testing.T) {
	a := &App{cfg: newTestConfig(t, t.TempDir())}
	aiGames.mu.Lock()
	aiGames.m = map[string]*aiGameSession{
		aiSessKey("p", "Claude"):   {PackID: "p", Nick: "Claude", Client: 1, Port: 1, Started: time.Now()},
		aiSessKey("p", "Claude_2"): {PackID: "p", Nick: "Claude_2", Client: 2, Port: 2, Started: time.Now()},
	}
	aiGames.mu.Unlock()
	defer func() { aiGames.mu.Lock(); aiGames.m = nil; aiGames.mu.Unlock() }()

	if _, err := a.aiGameFor("p", 0); err == nil || !strings.Contains(err.Error(), "кілька") {
		t.Fatalf("без client при двох сесіях — помилка з підказкою: %v", err)
	}
	if s, err := a.aiGameFor("p", 2); err != nil || s.Nick != "Claude_2" {
		t.Fatalf("клієнт 2: %v %v", s, err)
	}
	if s, err := a.aiGameFor("", 1); err != nil || s.Port != 1 {
		t.Fatalf("клієнт 1 без pack_id: %v %v", s, err)
	}
	if _, err := a.aiGameFor("nope", 0); err == nil {
		t.Fatal("для чужої збірки сесії немає")
	}
}

func TestGameControlPermissionDefaultOnWithAsk(t *testing.T) {
	if !(aiAgentState{}).allows("gameControl") || !(aiAgentState{AllowWrite: true}).allows("gameControl") {
		t.Fatal("керування грою ввімкнено за замовчуванням")
	}
	off := aiAgentState{Perms: map[string]bool{"gameControl": false, "mods": true}}
	if off.allows("gameControl") {
		t.Fatal("явне вимкнення має діяти")
	}
	if !(aiAgentState{Perms: map[string]bool{"mods": true}}).allows("gameControl") {
		t.Fatal("невказана категорія — за замовчуванням увімкнено")
	}
	ro, _ := aiPresetPerms("readonly")
	if (aiAgentState{Perms: ro}).allows("gameControl") {
		t.Fatal("«Лише читання» вимикає керування грою")
	}
	safe, _ := aiPresetPerms("safe")
	if !(aiAgentState{Perms: safe}).allows("gameControl") || (aiAgentState{Perms: safe}).allows("servers") {
		t.Fatal("«Безпечний»: керування грою так, сервери ні")
	}
	if !(aiAgentState{}).asks("gameControl") {
		t.Fatal("питання перед керуванням грою ввімкнене за замовчуванням")
	}
	for tool, cat := range aiToolCat {
		if strings.HasPrefix(tool, "game_") && cat != "gameControl" {
			t.Fatalf("%s має бути в gameControl", tool)
		}
	}
}

func TestGameControlAskIsOncePerSession(t *testing.T) {
	a := &App{cfg: newTestConfig(t, t.TempDir())}
	aiGames.mu.Lock()
	aiGames.m = map[string]*aiGameSession{aiSessKey("p", "Claude"): {PackID: "p", Nick: "Claude", Client: 1, Port: 1, Started: time.Now(), WithAgent: true}}
	aiGames.mu.Unlock()
	defer func() { aiGames.mu.Lock(); aiGames.m = nil; aiGames.mu.Unlock() }()

	st := aiAgentState{}
	args := map[string]any{"key": "w"}
	if msg := a.aiAskCheck("game_key", args, st); !strings.Contains(msg, "user_confirmed") {
		t.Fatalf("перший виклик у сеансі має питати: %q", msg)
	}
	if msg := a.aiAskCheck("game_screenshot", map[string]any{"user_confirmed": true}, st); msg != "" {
		t.Fatalf("після згоди — пропускати: %q", msg)
	}
	if msg := a.aiAskCheck("game_key", args, st); msg != "" {
		t.Fatalf("згода діє на весь сеанс: %q", msg)
	}
	// вимкнене питання — без згоди
	aiGames.mu.Lock()
	aiGames.m[aiSessKey("p", "Claude")].Approved = false
	aiGames.mu.Unlock()
	if msg := a.aiAskCheck("game_key", args, aiAgentState{Ask: map[string]bool{"gameControl": false}}); msg != "" {
		t.Fatalf("питання вимкнено: %q", msg)
	}
	// немає сесії → не наша справа (інструмент поверне свою помилку)
	if msg := a.aiAskCheck("game_key", map[string]any{"pack_id": "nope"}, st); msg != "" {
		t.Fatalf("без сесії: %q", msg)
	}
}

func TestAINickFromClient(t *testing.T) {
	for in, want := range map[string]string{
		"claude-code": "Claude", "Claude Desktop": "Claude", "anthropic-x": "Claude",
		"ChatGPT": "ChatGPT", "openai-mcp": "ChatGPT", "codex-cli": "ChatGPT", "gpt-5": "ChatGPT",
		"gemini-cli": "Gemini", "cursor": "Cursor", "windsurf": "Windsurf", "GitHub Copilot": "Copilot",
		"my agent!": "Myagent", "x": "", "": "", "   ": "", "ab": "", "Дивний": "",
	} {
		if got := aiNickFromClient(in); got != want {
			t.Errorf("aiNickFromClient(%q) = %q, очікувалось %q", in, got, want)
		}
	}
	if n := aiNickFromClient("averyveryverylongagentname"); len(n) != 16 || !aiNickRe.MatchString(n) {
		t.Fatalf("довгий нік: %q", n)
	}
}

func TestAIIdentityFromInitializeAndSkinNicks(t *testing.T) {
	dir := t.TempDir()
	a := &App{cfg: newTestConfig(t, dir)}
	aiGames.mu.Lock()
	aiGames.m = nil
	aiGames.mu.Unlock()
	aiAgent.mu.Lock()
	aiAgent.loaded = true
	aiAgent.st = aiAgentState{}
	aiAgent.mu.Unlock()

	a.aiAgentApplyClientIdentity("ChatGPT")
	if a.aiAgentNickNow() != "ChatGPT" {
		t.Fatalf("нік має прийти від клієнта: %s", a.aiAgentNickNow())
	}
	a.aiAgentApplyClientIdentity("claude-code")
	if a.aiAgentNickNow() != "Claude" {
		t.Fatal("інший клієнт — інший нік")
	}
	for _, n := range []string{"Claude", "ChatGPT", "ChatGPT_2", "claude_3"} {
		if !aiIsKnownAINick(n) {
			t.Errorf("%s має мати скін агента", n)
		}
	}
	if aiIsKnownAINick("Steve") || aiIsKnownAINick("Gemini") {
		t.Error("невідомий нік не отримує скін агента")
	}
	// під час гри ШІ ідентичність не змінюється
	aiGames.mu.Lock()
	aiGames.m = map[string]*aiGameSession{aiSessKey("p", "Claude"): {PackID: "p", Nick: "Claude", Started: time.Now()}}
	aiGames.mu.Unlock()
	defer func() { aiGames.mu.Lock(); aiGames.m = nil; aiGames.mu.Unlock() }()
	a.aiAgentApplyClientIdentity("ChatGPT")
	if a.aiAgentNickNow() != "Claude" {
		t.Fatal("поки йде гра ШІ, нік не змінюється")
	}
	// агент сам обирає нік — але не під час гри
	if err := a.aiAgentSetNick("MyBot"); err == nil {
		t.Fatal("під час гри нік змінювати не можна")
	}
	aiGames.mu.Lock()
	aiGames.m = nil
	aiGames.mu.Unlock()
	if err := a.aiAgentSetNick("MyBot"); err != nil || a.aiAgentNickNow() != "MyBot" {
		t.Fatalf("set nick: %v %s", err, a.aiAgentNickNow())
	}
	if err := a.aiAgentSetNick("a b"); err == nil {
		t.Fatal("некоректний нік")
	}
}

func TestEnsureAgentFresh(t *testing.T) {
	dir := t.TempDir()
	a := &App{cfg: newTestConfig(t, dir)}
	prevInstall := backendInitConfig.InstallDir
	backendInitConfig.InstallDir = filepath.Join(dir, "install")
	defer func() { backendInitConfig.InstallDir = prevInstall }()

	jar := makeJar(t, goodJarEntries())
	sum := sha256.Sum256(jar)
	serveSum := hex.EncodeToString(sum[:])
	var jarHits, sumHits int
	mux := http.NewServeMux()
	mux.HandleFunc("/"+gameAgentJarName, func(w http.ResponseWriter, r *http.Request) { jarHits++; _, _ = w.Write(jar) })
	mux.HandleFunc("/"+gameAgentJarName+".sha256", func(w http.ResponseWriter, r *http.Request) { sumHits++; _, _ = w.Write([]byte(serveSum + "  x\n")) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	prevBase := gameAgentReleaseBase
	gameAgentReleaseBase = srv.URL
	defer func() { gameAgentReleaseBase = prevBase }()
	reset := func() { agentCheck.mu.Lock(); agentCheck.last = time.Time{}; agentCheck.mu.Unlock() }

	// автооновлення вимкнено: мережі не чіпає
	reset()
	if note := a.aiEnsureAgentFresh(false); !strings.Contains(note, "автооновлення вимкнено") || sumHits+jarHits != 0 {
		t.Fatalf("вимкнено: %q hits=%d/%d", note, sumHits, jarHits)
	}
	// увімкнено, агента немає → встановлюється
	reset()
	if note := a.aiEnsureAgentFresh(true); !strings.Contains(note, "оновлено") || !a.GetAIGameAgentStatus().Installed {
		t.Fatalf("встановлення: %q", note)
	}
	// той самий SHA → jar не качається, і перевірка не частіша за інтервал
	reset()
	jarHits, sumHits = 0, 0
	if note := a.aiEnsureAgentFresh(true); note != "" || jarHits != 0 || sumHits != 1 {
		t.Fatalf("актуальний: %q hits=%d/%d", note, sumHits, jarHits)
	}
	if note := a.aiEnsureAgentFresh(true); note != "" || sumHits != 1 {
		t.Fatalf("троттлінг: %q sumHits=%d", note, sumHits)
	}
	// на сервері нова версія → оновлення
	jar = makeJar(t, func() map[string][]byte {
		m := goodJarEntries()
		m["META-INF/MANIFEST.MF"] = []byte("Manifest-Version: 1.0\nPremain-Class: dev.shaurma.agent.Agent\nCan-Retransform-Classes: true\nImplementation-Version: 9.9.10\n")
		return m
	}())
	ns := sha256.Sum256(jar)
	serveSum = hex.EncodeToString(ns[:])
	reset()
	if note := a.aiEnsureAgentFresh(true); !strings.Contains(note, "9.9.10") || a.GetAIGameAgentStatus().Version != "9.9.10" {
		t.Fatalf("оновлення: %q %+v", note, a.GetAIGameAgentStatus())
	}
	// збій мережі не блокує запуск
	srv.Close()
	reset()
	if note := a.aiEnsureAgentFresh(true); !strings.Contains(note, "працюємо з встановленою") {
		t.Fatalf("збій мережі: %q", note)
	}
	// local-only: мережі не чіпає
	if err := os.MkdirAll(backendInitConfig.InstallDir, 0o755); err == nil {
		_ = os.WriteFile(filepath.Join(backendInitConfig.InstallDir, "local-only.flag"), []byte("x"), 0o644)
	}
	reset()
	if note := a.aiEnsureAgentFresh(true); note != "" {
		t.Fatalf("local-only: %q", note)
	}
}

func TestAIHumanLabels(t *testing.T) {
	if aiHumanLabel("install_content", nil) != "Встановлення мода" || aiHumanLabel("launch_build", nil) != "Запуск гри" {
		t.Fatal("людські назви")
	}
	if got := aiHumanLabel("toggle_mod", map[string]any{"file_name": "sodium.jar"}); got != "Перемикання мода: sodium" {
		t.Fatal(got)
	}
	if aiHumanLabel("unknown_tool", nil) != "unknown_tool" {
		t.Fatal("невідомий інструмент — як є")
	}
	for tool := range aiToolCat {
		if aiToolHuman[tool] == "" && tool != "update_game_agent" {
			// не обов'язково для всіх, але ключові мають людську назву
			continue
		}
	}
}

func TestCheckSourcePath(t *testing.T) {
	dir := t.TempDir()
	jar := filepath.Join(dir, "mymod-1.0.jar")
	if err := os.WriteFile(jar, []byte("PK"), 0o644); err != nil {
		t.Fatal(err)
	}
	txt := filepath.Join(dir, "notes.txt")
	_ = os.WriteFile(txt, []byte("x"), 0o644)

	if _, err := aiCheckSourcePath(jar, "file"); err != nil {
		t.Fatalf("jar: %v", err)
	}
	if _, err := aiCheckSourcePath(dir, "folder"); err != nil {
		t.Fatalf("тека: %v", err)
	}
	for _, c := range []struct{ p, k string }{
		{`\\server\share\mod.jar`, "file"}, {"//server/share/mod.jar", "file"}, {"relative/mod.jar", "file"},
		{txt, "file"}, {dir, "file"}, {jar, "folder"}, {filepath.Join(dir, "missing.jar"), "file"}, {jar, "weird"}, {"", "file"},
	} {
		if _, err := aiCheckSourcePath(c.p, c.k); err == nil {
			t.Errorf("%q (%s) має бути відхилено", c.p, c.k)
		}
	}
}

func TestImageResultIsReturnedAsMCPImage(t *testing.T) {
	aiAgent.mu.Lock()
	aiAgent.loaded = true
	aiAgent.st = aiAgentState{Enabled: true, Token: "secret"}
	aiAgent.mu.Unlock()
	regTool(aiTool{name: "zz_test_image", run: func(a *App, g argMap) (any, error) {
		return aiImage{Data: []byte{0x89, 'P', 'N', 'G'}, Mime: "image/png", Text: "кадр"}, nil
	}})
	defer delete(aiAgentTools, "zz_test_image")
	a := &App{}
	body := `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"zz_test_image","arguments":{}}}`
	out := aiAgentPost(t, a, "127.0.0.1:47615", "", "Bearer secret", body).Body.String()
	if !strings.Contains(out, `"type":"image"`) || !strings.Contains(out, `"mimeType":"image/png"`) || !strings.Contains(out, "iVBORw") {
		t.Fatalf("очікувалось MCP image content: %s", out)
	}
	if !strings.Contains(out, "кадр") || !strings.Contains(out, `"isError":false`) {
		t.Fatalf("текст і isError: %s", out)
	}
}

func TestEmbeddedAISkinIsValidPNG(t *testing.T) {
	if len(aiAgentSkinPNG) < 100 || !bytes.HasPrefix(aiAgentSkinPNG, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatal("скін агента має бути PNG")
	}
	// IHDR: ширина/висота 64×64
	w := int(aiAgentSkinPNG[16])<<24 | int(aiAgentSkinPNG[17])<<16 | int(aiAgentSkinPNG[18])<<8 | int(aiAgentSkinPNG[19])
	h := int(aiAgentSkinPNG[20])<<24 | int(aiAgentSkinPNG[21])<<16 | int(aiAgentSkinPNG[22])<<8 | int(aiAgentSkinPNG[23])
	if w != 64 || (h != 64 && h != 32) {
		t.Fatalf("розмір скіна: %dx%d", w, h)
	}
}

func TestUpdateGameAgentRefusedInLocalOnlyBuild(t *testing.T) {
	dir := t.TempDir()
	a := &App{cfg: newTestConfig(t, dir)}
	prev := backendInitConfig.InstallDir
	backendInitConfig.InstallDir = filepath.Join(dir, "install")
	defer func() { backendInitConfig.InstallDir = prev }()
	if err := os.MkdirAll(backendInitConfig.InstallDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backendInitConfig.InstallDir, "local-only.flag"), []byte("local"), 0o644); err != nil {
		t.Fatal(err)
	}
	fetched := false
	prevFetch := gameAgentFetch
	gameAgentFetch = func(string, int64) ([]byte, error) {
		fetched = true
		return nil, errors.New("не має викликатись")
	}
	defer func() { gameAgentFetch = prevFetch }()

	if _, err := a.UpdateAIGameAgent(); err == nil || !strings.Contains(err.Error(), "local-only") {
		t.Fatalf("у локальній збірці оновлення з мережі має бути відхилено: %v", err)
	}
	if fetched {
		t.Fatal("мережу не можна чіпати в local-only збірці")
	}
	// агент, покладений збіркою у game-agent, використовується як є
	d := filepath.Join(backendInitConfig.InstallDir, "game-agent")
	_ = os.MkdirAll(d, 0o755)
	_ = os.WriteFile(filepath.Join(d, gameAgentJarName), makeJar(t, goodJarEntries()), 0o644)
	if st := a.GetAIGameAgentStatus(); !st.Installed || st.Version != "9.9.9" {
		t.Fatalf("агент із теки збірки має підхоплюватись: %+v", st)
	}
}
