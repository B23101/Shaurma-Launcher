package main

import (
	"strings"
	"testing"
	"time"
)

func TestRemoteActionText(t *testing.T) {
	cases := []struct {
		op, result string
		want       string // підрядок; "" — тост не показується
	}{
		{"start", "accepted", "запускає"},
		{"stop", "accepted", "зупиняє"},
		{"p2p_on", "ok", "увімкнув"},
		{"console_open", "ok", "консоль"},
		{"start", "not_allowed", "заблоковано"},
		{"stop", "players_online", "заблоковано"},
		{"start", "already_running", ""},
		{"console_exec", "ok", ""},
	}
	for _, c := range cases {
		got := remoteActionText(c.op, c.result, "Іван", "Мій сервер")
		if c.want == "" && got != "" || c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("%s/%s: %q", c.op, c.result, got)
		}
	}
}

func TestRemoteNotifyThrottle(t *testing.T) {
	now := time.Now()
	if !remoteNotifyAllow("k-test", now) {
		t.Fatal("перше має пройти")
	}
	if remoteNotifyAllow("k-test", now.Add(time.Second)) {
		t.Fatal("повтор за 1с має відсіктись")
	}
	if !remoteNotifyAllow("k-test", now.Add(remoteNotifyThrottle+time.Second)) {
		t.Fatal("після інтервалу має пройти")
	}
}
