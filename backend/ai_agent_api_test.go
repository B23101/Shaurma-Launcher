package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func aiAgentPost(t *testing.T, a *App, host, origin, auth, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "http://"+host+"/mcp", strings.NewReader(body))
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if auth != "" {
		r.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	a.aiAgentHandler(w, r)
	return w
}

func TestAIAgentHandlerSecurity(t *testing.T) {
	aiAgent.mu.Lock()
	aiAgent.loaded = true
	aiAgent.st = aiAgentState{Enabled: true, AllowWrite: false, Token: "secret"}
	aiAgent.mu.Unlock()
	a := &App{}
	list := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`

	if c := aiAgentPost(t, a, "127.0.0.1:47615", "", "", list).Code; c != http.StatusUnauthorized {
		t.Fatalf("без токена: %d", c)
	}
	if c := aiAgentPost(t, a, "127.0.0.1:47615", "", "Bearer wrong", list).Code; c != http.StatusUnauthorized {
		t.Fatalf("хибний токен: %d", c)
	}
	if c := aiAgentPost(t, a, "evil.example:47615", "", "Bearer secret", list).Code; c != http.StatusForbidden {
		t.Fatalf("чужий Host: %d", c)
	}
	if c := aiAgentPost(t, a, "127.0.0.1:47615", "https://evil.example", "Bearer secret", list).Code; c != http.StatusForbidden {
		t.Fatalf("Origin з браузера: %d", c)
	}
	w := aiAgentPost(t, a, "127.0.0.1:47615", "", "Bearer secret", list)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "search_content") {
		t.Fatalf("tools/list: %d %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, name := range []string{"delete_ai_created_build", "delete_mod", "delete_content"} {
		body = strings.ReplaceAll(body, `"name":"`+name+`"`, "")
	}
	if strings.Contains(body, `"name":"delete`) {
		t.Fatal("незареєстрований інструмент видалення")
	}
}

func TestAIAgentWriteGate(t *testing.T) {
	aiAgent.mu.Lock()
	aiAgent.loaded = true
	aiAgent.st = aiAgentState{Enabled: true, AllowWrite: false, Token: "secret"}
	aiAgent.mu.Unlock()
	a := &App{}
	body := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"launch_build","arguments":{"pack_id":"x"}}}`
	w := aiAgentPost(t, a, "localhost:47615", "", "Bearer secret", body)
	if !strings.Contains(w.Body.String(), `"isError":true`) || !strings.Contains(w.Body.String(), "не дозволив") {
		t.Fatalf("запис без дозволу має бути заборонений: %s", w.Body.String())
	}
}

func TestAIAgentAllWriteToolsGated(t *testing.T) {
	aiAgent.mu.Lock()
	aiAgent.loaded = true
	aiAgent.st = aiAgentState{Enabled: true, AllowWrite: false, Token: "secret", Perms: map[string]bool{"gameControl": false}}
	aiAgent.mu.Unlock()
	a := &App{}
	writes := 0
	for name, tl := range aiAgentTools {
		if (strings.HasPrefix(name, "delete") && name != "delete_ai_created_build" && name != "delete_mod" && name != "delete_content") || strings.Contains(name, "remove") || strings.Contains(name, "account") || strings.Contains(name, "token") {
			t.Fatalf("небезпечний інструмент зареєстровано: %s", name)
		}
		if !tl.write {
			continue
		}
		writes++
		body := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"` + name + `","arguments":{}}}`
		w := aiAgentPost(t, a, "127.0.0.1:47615", "", "Bearer secret", body)
		if !strings.Contains(w.Body.String(), "не дозволив") {
			t.Fatalf("%s не заблоковано без дозволу: %s", name, w.Body.String())
		}
	}
	if writes < 20 {
		t.Fatalf("очікувалось багато write-інструментів, є %d", writes)
	}
}

func TestAIAgentPackFilePaths(t *testing.T) {
	a := &App{}
	for _, bad := range []string{"../x.toml", "config/../../a.toml", "mods/a.jar", "config/a.jar", "saves/level.dat", "/etc/passwd", "config"} {
		if _, err := aiPackFile(a, "p", bad, true); err == nil {
			t.Fatalf("шлях %q має бути відхилений", bad)
		}
	}
}

func TestAIAgentLogPathSafety(t *testing.T) {
	a := &App{}
	for _, bad := range []string{"../a.log", "a/b.log", "x.jar", "secrets.txt", ""} {
		if _, err := aiLogPath(a, "p", bad); err == nil {
			t.Fatalf("файл %q має бути відхилений", bad)
		}
	}
}

func TestAIAgentStyleAndCSVHelpers(t *testing.T) {
	for _, ok := range []string{"#aabbcc", "#123ABC"} {
		if !aiHexColor.MatchString(ok) {
			t.Fatalf("%s має бути валідним", ok)
		}
	}
	for _, bad := range []string{"red", "#fff", "#12345g", "#aabbccdd", "javascript:1"} {
		if aiHexColor.MatchString(bad) {
			t.Fatalf("%s має бути відхилений", bad)
		}
	}
	if !aiTablerIc.MatchString("ti-sword") || aiTablerIc.MatchString("../x") || aiTablerIc.MatchString("ti-<b>") {
		t.Fatal("перевірка іконки")
	}
	if got := aiCSV(" a, b ,,c "); len(got) != 3 || got[2] != "c" {
		t.Fatalf("aiCSV: %v", got)
	}
	if (&App{}).aiProvenanceOf("x", "y") != "" {
		t.Fatal("без cfg має бути порожньо")
	}
}

func TestAIAgentRejectsBrokenEncoding(t *testing.T) {
	aiAgent.mu.Lock()
	aiAgent.loaded = true
	aiAgent.st = aiAgentState{Enabled: true, AllowWrite: true, Token: "secret"}
	aiAgent.mu.Unlock()
	a := &App{}
	body := `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"create_build","arguments":{"name":"��","mc_version":"1.20.1","loader":"fabric"}}}`
	w := aiAgentPost(t, a, "127.0.0.1:47615", "", "Bearer secret", body)
	if !strings.Contains(w.Body.String(), "кодування") {
		t.Fatalf("биту назву має бути відхилено: %s", w.Body.String())
	}
}

func TestAIEditGuardBlocksUIWhileAgentEdits(t *testing.T) {
	aiEdit.mu.Lock()
	aiEdit.m = map[string]*AIEditSession{}
	aiEdit.started = true // не запускати тикер у тесті
	aiEdit.mu.Unlock()
	a := &App{}
	id := json.RawMessage(`"zombie"`)
	if err := a.aiEditGuard("LaunchInstance", []json.RawMessage{id}); err != nil {
		t.Fatalf("без сесії не блокувати: %v", err)
	}
	a.aiEditBegin("zombie", "install_content")
	if err := a.aiEditGuard("LaunchInstance", []json.RawMessage{id}); err == nil {
		t.Fatal("запуск збірки під час редагування агентом має бути заблокований")
	}
	if err := a.aiEditGuard("ToggleMod", []json.RawMessage{id, json.RawMessage(`"a.jar"`), json.RawMessage(`false`)}); err == nil {
		t.Fatal("зміна модів під час редагування має бути заблокована")
	}
	if err := a.aiEditGuard("LaunchInstance", []json.RawMessage{json.RawMessage(`"other"`)}); err != nil {
		t.Fatal("інші збірки не блокуються")
	}
	if err := a.aiEditGuard("GetBuilds", nil); err != nil {
		t.Fatal("читання не блокується")
	}
	a.aiEditRecord("zombie", AIEditChange{Kind: "mod", Summary: "x", OK: true})
	if s, ok := aiEditActiveFor("zombie"); !ok || len(s.Changes) != 1 {
		t.Fatalf("стрічка змін: %+v %v", s, ok)
	}
	a.aiEditFinish("zombie")
	if err := a.aiEditGuard("LaunchInstance", []json.RawMessage{id}); err != nil {
		t.Fatalf("після завершення сесії блокування має зняти: %v", err)
	}
}

func TestAIEditSessionExpires(t *testing.T) {
	aiEdit.mu.Lock()
	aiEdit.m = map[string]*AIEditSession{"old": {PackID: "old", Active: true, Last: time.Now().Add(-5 * time.Minute).UnixMilli()}}
	aiEdit.started = true
	aiEdit.mu.Unlock()
	if _, ok := aiEditActiveFor("old"); ok {
		t.Fatal("сесія без активності довше за aiEditIdle не має блокувати")
	}
}

func TestAIAgentEveryWriteToolHasCategory(t *testing.T) {
	if missing := aiUnknownWriteTools(); len(missing) > 0 {
		t.Fatalf("write-інструменти без категорії прав: %v", missing)
	}
	valid := map[string]bool{}
	for _, c := range aiPermOrder {
		valid[c] = true
	}
	for n, c := range aiToolCat {
		if !valid[c] {
			t.Fatalf("%s має невідому категорію %q", n, c)
		}
		if tl, ok := aiAgentTools[n]; ok && !tl.write {
			t.Fatalf("%s має категорію, але не write", n)
		}
	}
}

func TestAIAgentPerCategoryGating(t *testing.T) {
	aiAgent.mu.Lock()
	aiAgent.loaded = true
	aiAgent.st = aiAgentState{Enabled: true, Token: "secret", Perms: map[string]bool{"mods": true}}
	aiAgent.mu.Unlock()
	a := &App{}
	call := func(tool string) string {
		body := `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"` + tool + `","arguments":{}}}`
		return aiAgentPost(t, a, "127.0.0.1:47615", "", "Bearer secret", body).Body.String()
	}
	// launch не дозволено → відмова з назвою категорії
	if out := call("launch_build"); !strings.Contains(out, "не дозволив") || !strings.Contains(out, "Запуск") {
		t.Fatalf("launch має бути заборонений: %s", out)
	}
	// mods дозволено → доходить до перевірки аргументів, а не до відмови за правами
	if out := call("toggle_mod"); strings.Contains(out, "не дозволив") {
		t.Fatalf("mods дозволено, але відмовлено: %s", out)
	}
}

func TestAIAgentLegacyAllowWriteAndPresets(t *testing.T) {
	legacy := aiAgentState{AllowWrite: true} // Perms == nil → діє старий головний вимикач
	for _, c := range aiPermOrder {
		if !legacy.allows(c) {
			t.Fatalf("legacy allowWrite має відкривати %s", c)
		}
	}
	if (aiAgentState{}).allows("mods") || (aiAgentState{AllowWrite: true}).allows("") {
		t.Fatal("за замовчуванням і для порожньої категорії — заборонено")
	}
	ro, _ := aiPresetPerms("readonly")
	st := aiAgentState{Perms: ro}
	if st.anyWrite() {
		t.Fatal("readonly не має відкривати зміни")
	}
	safe, _ := aiPresetPerms("safe")
	st = aiAgentState{Perms: safe}
	if !st.allows("mods") || st.allows("servers") || st.allows("launcherSettings") {
		t.Fatal("safe: моди так, сервери й налаштування лаунчера — ні")
	}
	if _, ok := aiPresetPerms("nope"); ok {
		t.Fatal("невідомий пресет")
	}
}

func TestAIAgentContentHelpers(t *testing.T) {
	for _, bad := range []string{"", "..", "a/b.zip", `a\b.zip`, `x".zip`, "c:evil"} {
		if aiSafeFileName(bad) {
			t.Fatalf("%q має бути відхилене", bad)
		}
	}
	if !aiSafeFileName("Faithful 64x.zip") {
		t.Fatal("звичайне імʼя має проходити")
	}
	got := aiSetLine("a:1\nresourcePacks:[\"vanilla\"]\nb:2\n", "resourcePacks", ":", `["vanilla","file/X.zip"]`)
	if !strings.Contains(got, `resourcePacks:["vanilla","file/X.zip"]`) || !strings.Contains(got, "b:2") {
		t.Fatalf("aiSetLine: %q", got)
	}
	got = aiSetLine("a=1\n", "shaderPack", "=", "S.zip")
	if !strings.HasSuffix(got, "shaderPack=S.zip\n") {
		t.Fatalf("додавання рядка: %q", got)
	}
	if _, err := aiContentDir(argMap{"content_dir": "../etc"}); err == nil {
		t.Fatal("content_dir з шляхом має бути відхилений")
	}
}

func TestAIServerCoreFor(t *testing.T) {
	avail := []string{"paper", "fabric", "forge", "vanilla"}
	if c, err := aiServerCoreFor("Fabric", avail); err != nil || c != "fabric" {
		t.Fatalf("fabric: %q %v", c, err)
	}
	if c, err := aiServerCoreFor("vanilla", avail); err != nil || c != "vanilla" {
		t.Fatalf("vanilla: %q %v", c, err)
	}
	if c, err := aiServerCoreFor("", []string{"paper"}); err != nil || c != "paper" {
		t.Fatalf("порожній лоадер → paper: %q %v", c, err)
	}
	if _, err := aiServerCoreFor("quilt", avail); err == nil {
		t.Fatal("для невідомого лоадера має бути помилка")
	}
}

func TestAIReadOnlyCommands(t *testing.T) {
	for _, c := range []string{"list", "/tps", "  spark tps", "whitelist list", "time query daytime", "data get entity @s", "version"} {
		if !aiReadOnlyCommand("server_command", map[string]any{"command": c}) {
			t.Fatalf("%q — команда читання", c)
		}
	}
	for _, c := range []string{"op Nick", "stop", "whitelist add X", "kill @e", "execute as @a run say hi", "time set night", "", "data merge entity @s {}"} {
		if aiReadOnlyCommand("server_command", map[string]any{"command": c}) {
			t.Fatalf("%q не має вважатися безпечною", c)
		}
	}
	if aiReadOnlyCommand("start_server", map[string]any{"command": "list"}) {
		t.Fatal("лише server_command")
	}
}

func TestAIEditPolicyProtect(t *testing.T) {
	a := &App{}
	args := map[string]any{"pack_id": "my-build"}
	ask := aiAgentState{}                       // за замовчуванням — питати
	copyPol := aiAgentState{EditPolicy: "copy"} // завжди копія
	orig := aiAgentState{EditPolicy: "original"}
	legacy := aiAgentState{AllowEditUserData: true}

	if msg := a.aiProtectCheck("toggle_mod", args, ask); !strings.Contains(msg, "СПИТАЙТЕ") || !strings.Contains(msg, "duplicate_build") {
		t.Fatalf("ask: %q", msg)
	}
	if msg := a.aiProtectCheck("toggle_mod", map[string]any{"pack_id": "my-build", "confirm_edit_original": true}, ask); msg != "" {
		t.Fatalf("ask + підтвердження має пропускати: %q", msg)
	}
	if msg := a.aiProtectCheck("toggle_mod", args, copyPol); !strings.Contains(msg, "НЕ питайте") {
		t.Fatalf("copy: %q", msg)
	}
	if msg := a.aiProtectCheck("toggle_mod", map[string]any{"pack_id": "my-build", "confirm_edit_original": true}, copyPol); msg == "" {
		t.Fatal("за політики «завжди копія» агент не може обійти захист прапорцем")
	}
	if a.aiProtectCheck("toggle_mod", args, orig) != "" || a.aiProtectCheck("toggle_mod", args, legacy) != "" {
		t.Fatal("original / legacy allowEditUserData: захисту немає")
	}
	// сервер
	if msg := a.aiProtectCheck("set_server_properties", map[string]any{"server_id": "s1"}, ask); !strings.Contains(msg, "duplicate_server") {
		t.Fatalf("server: %q", msg)
	}
	if msg := a.aiProtectCheck("toggle_mod", map[string]any{"pack_id": "srvp:s1"}, ask); !strings.Contains(msg, "duplicate_server") {
		t.Fatalf("srvp: має трактуватись як сервер: %q", msg)
	}
	// винятки
	for _, tool := range []string{"start_server", "stop_server", "create_server_for_build", "duplicate_server", "create_build", "set_build_style", "create_world_snapshot"} {
		if a.aiProtectCheck(tool, args, ask) != "" {
			t.Fatalf("%s не має потрапляти під захист", tool)
		}
	}
	if a.aiProtectCheck("server_command", map[string]any{"server_id": "s1", "command": "list"}, ask) != "" {
		t.Fatal("безпечна команда читання не питає")
	}
	if a.aiProtectCheck("server_command", map[string]any{"server_id": "s1", "command": "op Nick"}, ask) == "" {
		t.Fatal("op на сервері користувача має питати")
	}
}

func TestAIAskPolicy(t *testing.T) {
	a := &App{}
	def := aiAgentState{}
	if msg := a.aiAskCheck("update_mods", map[string]any{"pack_id": "x"}, def); !strings.Contains(msg, "user_confirmed") {
		t.Fatalf("масове оновлення питає за замовчуванням: %q", msg)
	}
	if a.aiAskCheck("update_mods", map[string]any{"pack_id": "x", "user_confirmed": true}, def) != "" {
		t.Fatal("після згоди пропускати")
	}
	off := aiAgentState{Ask: map[string]bool{"bulk": false}}
	if a.aiAskCheck("update_mods", map[string]any{"pack_id": "x"}, off) != "" {
		t.Fatal("вимкнений пункт не питає")
	}
	if a.aiAskCheck("launch_build", map[string]any{"pack_id": "x"}, def) != "" {
		t.Fatal("запуск не питає за замовчуванням")
	}
	if a.aiAskCheck("launch_build", map[string]any{"pack_id": "x"}, aiAgentState{Ask: map[string]bool{"launch": true}}) == "" {
		t.Fatal("увімкнений запуск має питати")
	}
	if a.aiAskCheck("migrate_mods", map[string]any{"source_id": "a", "target_id": "b"}, def) != "" {
		t.Fatal("міграція dry_run (за замовч.) не питає")
	}
	if a.aiAskCheck("migrate_mods", map[string]any{"source_id": "a", "target_id": "b", "dry_run": false}, def) == "" {
		t.Fatal("справжня міграція питає")
	}
	// усі ключі відомі UI
	known := map[string]bool{}
	for _, k := range aiAskOrder {
		known[k] = true
		if _, ok := aiAskWhat[k]; !ok {
			t.Fatalf("немає опису для %s", k)
		}
	}
	for tool := range aiAgentTools {
		if k := aiAskKeyFor(tool, nil); k != "" && !known[k] {
			t.Fatalf("%s: невідомий пункт %s", tool, k)
		}
	}
}
