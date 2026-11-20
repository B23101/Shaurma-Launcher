package main

// ai_start.go — консольна команда лаунчера для ШІ: `ShaurmLauncher.exe --ai-start`.
//
// Для сценарію «лаунчер закритий, а ШІ (напр. Claude Code в IDE) має запустити гру»:
//   - якщо MCP-сервер ШІ-агента вже працює — лише друкує його адресу;
//   - інакше запускає лаунчер у фоні (як автозапуск, без вікна) і чекає, поки MCP підніметься.
// Результат — один JSON-рядок у stdout (працює, коли stdout підключено каналом, тобто з терміналу ШІ):
//   {"running":true,"started":true,"url":"http://127.0.0.1:47615/mcp","port":47615}
// Токен НЕ друкується: він у Налаштування → ШІ-агент. Команда нічого не вмикає за користувача:
// якщо ШІ-агента вимкнено в налаштуваннях, MCP не підніметься — команда про це скаже.
// Режим виконується до завантаження DLL і UI, як і --p2p-firewall-status.

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const aiStartFlag = "--ai-start"

// aiRuntimeFile — файл, який backend пише біля exe, коли MCP-сервер ШІ працює (без токена).
const aiRuntimeFile = "ai-agent-runtime.json"

type aiRuntimeInfo struct {
	PID  int    `json:"pid"`
	Port int    `json:"port"`
	URL  string `json:"url"`
}

func handleAIStartCommand() {
	if len(os.Args) != 2 || os.Args[1] != aiStartFlag {
		return
	}
	os.Exit(runAIStart(os.Stdout, exeDir(), spawnBackgroundSelf, 90*time.Second, probeAIAgent))
}

func readAIRuntime(dir string) (aiRuntimeInfo, bool) {
	b, err := os.ReadFile(filepath.Join(dir, aiRuntimeFile))
	if err != nil {
		return aiRuntimeInfo{}, false
	}
	var rt aiRuntimeInfo
	if json.Unmarshal(b, &rt) != nil || rt.Port < 1024 || rt.Port > 65535 || rt.URL == "" {
		return aiRuntimeInfo{}, false
	}
	return rt, true
}

// probeAIAgent — чи відповідає MCP-сервер: запит без токена має дати 401 (сервер живий, але авторизація потрібна).
func probeAIAgent(url string) bool {
	c := &http.Client{Timeout: 1500 * time.Millisecond}
	resp, err := c.Post(url, "application/json", nil)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusUnauthorized
}

// runAIStart — ядро команди (з підміною spawn/probe для тестів). Повертає код виходу: 0 — працює, 1 — не вдалося
// запустити лаунчер, 2 — MCP не піднявся за timeout (найімовірніше, ШІ-агента вимкнено в налаштуваннях).
func runAIStart(out io.Writer, dir string, spawn func() error, timeout time.Duration, probe func(string) bool) int {
	enc := json.NewEncoder(out)
	if rt, ok := readAIRuntime(dir); ok && probe(rt.URL) {
		_ = enc.Encode(map[string]any{"running": true, "started": false, "url": rt.URL, "port": rt.Port})
		return 0
	}
	if err := spawn(); err != nil {
		_ = enc.Encode(map[string]any{"running": false, "error": "не вдалося запустити лаунчер: " + err.Error()})
		return 1
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if rt, ok := readAIRuntime(dir); ok && probe(rt.URL) {
			_ = enc.Encode(map[string]any{"running": true, "started": true, "url": rt.URL, "port": rt.Port})
			return 0
		}
		time.Sleep(500 * time.Millisecond)
	}
	_ = enc.Encode(map[string]any{"running": false, "error": "MCP-сервер ШІ-агента не піднявся: перевірте, що ШІ-агента ввімкнено " +
		"(Налаштування → ШІ-агент), і що лаунчер не завис на оновленні"})
	return 2
}

// spawnBackgroundSelf запускає цей самий exe у фоні (--background: без вікна), відв'язаним від консолі.
func spawnBackgroundSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, "--background")
	detachProcess(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
