package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeRuntime(t *testing.T, dir string, port int) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"pid": 1, "port": port, "url": "http://127.0.0.1:" + itoaPort(port) + "/mcp"})
	if err := os.WriteFile(filepath.Join(dir, aiRuntimeFile), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func itoaPort(p int) string {
	b, _ := json.Marshal(p)
	return string(b)
}

func decodeOut(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("вивід не JSON: %q (%v)", buf.String(), err)
	}
	return m
}

func TestAIStartAlreadyRunningDoesNotSpawn(t *testing.T) {
	dir := t.TempDir()
	writeRuntime(t, dir, 47615)
	spawned := false
	var out bytes.Buffer
	code := runAIStart(&out, dir, func() error { spawned = true; return nil }, time.Second, func(string) bool { return true })
	m := decodeOut(t, &out)
	if code != 0 || spawned || m["running"] != true || m["started"] != false || m["port"].(float64) != 47615 {
		t.Fatalf("code=%d spawned=%v out=%v", code, spawned, m)
	}
	if strings.Contains(out.String(), "token") {
		t.Fatal("токен не має друкуватись")
	}
}

func TestAIStartSpawnsAndWaitsForServer(t *testing.T) {
	dir := t.TempDir()
	spawned := false
	probes := 0
	var out bytes.Buffer
	code := runAIStart(&out, dir, func() error {
		spawned = true
		go func() { time.Sleep(200 * time.Millisecond); writeRuntime(t, dir, 47616) }()
		return nil
	}, 5*time.Second, func(string) bool { probes++; return true })
	m := decodeOut(t, &out)
	if code != 0 || !spawned || m["started"] != true || m["port"].(float64) != 47616 {
		t.Fatalf("code=%d out=%v", code, m)
	}
}

func TestAIStartStaleRuntimeIsIgnored(t *testing.T) {
	dir := t.TempDir()
	writeRuntime(t, dir, 47615) // файл є, але сервер мертвий
	spawned := false
	var out bytes.Buffer
	code := runAIStart(&out, dir, func() error { spawned = true; return nil }, 700*time.Millisecond, func(string) bool { return false })
	m := decodeOut(t, &out)
	if code != 2 || !spawned || m["running"] != false || !strings.Contains(m["error"].(string), "ввімкнено") {
		t.Fatalf("code=%d spawned=%v out=%v", code, spawned, m)
	}
}

func TestAIStartSpawnFailure(t *testing.T) {
	var out bytes.Buffer
	code := runAIStart(&out, t.TempDir(), func() error { return errors.New("boom") }, time.Second, func(string) bool { return false })
	m := decodeOut(t, &out)
	if code != 1 || m["running"] != false || !strings.Contains(m["error"].(string), "boom") {
		t.Fatalf("code=%d out=%v", code, m)
	}
}

func TestReadAIRuntimeRejectsGarbage(t *testing.T) {
	dir := t.TempDir()
	for _, body := range []string{"", "not json", `{"port":80,"url":"x"}`, `{"port":47615,"url":""}`} {
		_ = os.WriteFile(filepath.Join(dir, aiRuntimeFile), []byte(body), 0o644)
		if _, ok := readAIRuntime(dir); ok {
			t.Fatalf("%q має бути відхилено", body)
		}
	}
}
