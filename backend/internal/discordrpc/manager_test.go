package discordrpc

import (
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

func TestApplyTpl(t *testing.T) {
	repl := map[string]string{"{pack}": "Pack A", "{list}": "Майнкрафт fabric 1.20"}
	if got := applyTpl("{pack}", repl); got != "Pack A" {
		t.Fatalf("applyTpl({pack}) = %q", got)
	}
	got := applyTpl(" {pack}  /  {list} ", repl)
	if want := "Pack A  /  Майнкрафт fabric 1.20"; got != want {
		t.Fatalf("applyTpl = %q, want %q", got, want)
	}
	if applyTpl("{missing}", repl) != "{missing}" {
		t.Fatal("unknown placeholder must remain intact")
	}
}

func TestActivityToPayload(t *testing.T) {
	a := &activity{Details: "details", State: "state", StartTS: 12345}
	p := a.toPayload()
	if p["details"] != "details" || p["state"] != "state" {
		t.Fatalf("toPayload = %v", p)
	}
	ts, ok := p["timestamps"].(map[string]any)
	if !ok || ts["start"] != int64(12345) {
		t.Fatalf("timestamps = %#v", p["timestamps"])
	}
	// порожні поля і нульовий StartTS не потрапляють у payload,
	// але type:0 (PLAYING) присутній завжди
	empty := (&activity{}).toPayload()
	if len(empty) != 1 || empty["type"] != 0 {
		t.Fatalf("empty toPayload = %v", empty)
	}
}

func TestRebuildSingleAndMulti(t *testing.T) {
	m := New("id", 1, true, "{pack}", "{list}")
	m.AddBuild(BuildInfo{ID: "a", Name: "Alpha", Loader: "fabric", MCVersion: "1.20", StartTS: time.Now().Unix()})
	if m.lastActivity == nil {
		t.Fatal("очікувалась активність для однієї збірки")
	}
	if m.lastActivity.Details != "Alpha" {
		t.Fatalf("single details = %q", m.lastActivity.Details)
	}
	m.mu.Lock()
	running := m.running
	m.mu.Unlock()
	if len(running) != 1 {
		t.Fatalf("running = %d", len(running))
	}

	// додаємо другу збірку → множина
	m.AddBuild(BuildInfo{ID: "b", Name: "Beta", Loader: "forge", MCVersion: "1.19"})
	if m.lastActivity.Details != "2 збірки" {
		t.Fatalf("multi details = %q", m.lastActivity.Details)
	}

	// прибираємо одну → знову поодинока
	m.RemoveBuild("b")
	if m.lastActivity.Details != "Alpha" {
		t.Fatalf("after remove details = %q", m.lastActivity.Details)
	}

	// прибираємо останню → активності НЕМАЄ (Discord прибирає статус)
	m.RemoveBuild("a")
	if m.lastActivity != nil {
		t.Fatalf("0 збірок: очікувалось очищення активності, отримано %+v", m.lastActivity)
	}
}

func TestRebuildDisabledClears(t *testing.T) {
	m := New("id", 1, true, "{pack}", "{list}")
	m.AddBuild(BuildInfo{ID: "a", Name: "Alpha", Loader: "fabric", MCVersion: "1.20", StartTS: time.Now().Unix()})
	if m.lastActivity == nil {
		t.Fatal("очікувалась активність при запущеній збірці")
	}
	// Вимкнення Discord-присутності теж має очистити активність.
	m.SetConfig(false, "", "")
	if m.lastActivity != nil {
		t.Fatalf("вимкнено: очікувалось очищення активності, отримано %+v", m.lastActivity)
	}
}

// Discord відхиляє всю активність через невалідну кнопку — тож перевіряємо
// ліміти тут, а не дізнаємось про них зі зламаного статусу в гравців.
func TestSiteButtonWithinDiscordLimits(t *testing.T) {
	if n := len([]rune(siteButtonLabel)); n < 1 || n > 32 {
		t.Fatalf("button label length %d, Discord allows 1..32", n)
	}
	if n := len(utf16.Encode([]rune(siteButtonLabel))); n > 32 {
		t.Fatalf("button label is %d UTF-16 units, Discord allows 32", n)
	}
	if !strings.HasPrefix(siteButtonURL, "https://") || len(siteButtonURL) > 512 {
		t.Fatalf("bad button url %q", siteButtonURL)
	}
	p := (&activity{Details: "d", WithSiteButton: true}).toPayload()
	b, ok := p["buttons"].([]map[string]string)
	if !ok || len(b) != 1 || b[0]["url"] != siteButtonURL || b[0]["label"] != siteButtonLabel {
		t.Fatalf("buttons = %#v", p["buttons"])
	}
	if _, has := (&activity{Details: "d"}).toPayload()["buttons"]; has {
		t.Fatal("button must be opt-in")
	}
}
