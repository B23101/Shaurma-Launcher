package shaurmaid

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// devVPS — фейк VPS з пристроями: той самий протокол, що в vps/internal/accounts (recover v2 додає
// пристрій, login перевіряє підпис пристрою, токен прив'язаний до пристрою, ключі чату — first-wins).
type devVPS struct {
	mu       sync.Mutex
	acc      string
	nick     string
	devs     []*dDev
	max      int
	nonces   map[string]bool
	seq      int
	keys     []ChatKey
	keyMode  string // "" | disabled | unavailable | old (404)
	audit    []AuditEvent
	hdrs     []map[string]string
	lastBody map[string]any
}

type dDev struct {
	id, name, hwid string
	pub            ed25519.PublicKey
	status         string
	reason, by     string
}

func newDevVPS() *devVPS {
	return &devVPS{acc: "11111111-2222-4333-8444-000000000009", nick: "Alex", max: 3, nonces: map[string]bool{}}
}

func (f *devVPS) active() int {
	n := 0
	for _, d := range f.devs {
		if d.status == "active" {
			n++
		}
	}
	return n
}

func (f *devVPS) byToken(h map[string]string) *dDev {
	for _, d := range f.devs {
		if h["Authorization"] == "Bearer jwt-"+d.id {
			return d
		}
	}
	return nil
}

func (f *devVPS) Do(_ context.Context, method, path string, h map[string]string, body []byte) (int, []byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path, _, _ = strings.Cut(path, "?")
	f.hdrs = append(f.hdrs, h)
	var q map[string]any
	_ = json.Unmarshal(body, &q)
	f.lastBody = q
	s := func(k string) string { v, _ := q[k].(string); return v }
	n := func(k string) int64 { v, _ := q[k].(float64); return int64(v) }
	reply := func(st int, v any) (int, []byte, error) { b, _ := json.Marshal(v); return st, b, nil }
	errc := func(st int, code string) (int, []byte, error) { return reply(st, map[string]string{"error": code}) }
	hwid := h["X-Shaurma-HWID"]
	session := func(d *dDev) sessionResp {
		return sessionResp{AccountID: f.acc, Nickname: f.nick, AccessToken: "jwt-" + d.id, ExpiresInS: 3600,
			Type: TypeUnlicensed, UUID: OfflineUUID(f.nick), DeviceID: d.id, DeviceName: d.name, MaxDevices: f.max}
	}

	switch path {
	case "/api/account/recover":
		pub, _ := b64.DecodeString(s("publicKey"))
		sig, _ := b64.DecodeString(s("sig"))
		if n("v") != 2 {
			return errc(401, "bad_signature")
		}
		msg := recoverMessageV2(s("publicKey"), s("slot"), s("provider"), s("idToken"), n("ts"), s("nonce"), hwid, s("revokeDeviceId"), s("deviceName"))
		if !ed25519.Verify(pub, []byte(msg), sig) {
			return errc(401, "bad_signature")
		}
		if s("idToken") != "tok:"+OIDCNonce(s("publicKey"), s("slot")) {
			return errc(401, "bad_identity")
		}
		if f.acc == "" {
			return errc(404, "no_account")
		}
		if f.active() >= f.max {
			if rid := s("revokeDeviceId"); rid != "" {
				for _, d := range f.devs {
					if d.id == rid && d.status == "active" {
						d.status, d.reason = "revoked", "limit_replaced"
					}
				}
			}
		}
		if f.active() >= f.max {
			var lv []LimitDevice
			for _, d := range f.devs {
				if d.status == "active" {
					lv = append(lv, LimitDevice{ID: d.id, Name: d.name, ClientName: "Shaurma Launcher"})
				}
			}
			return reply(409, map[string]any{"error": "device_limit", "max": f.max, "devices": lv})
		}
		f.seq++
		d := &dDev{id: "dv-" + strings.Repeat("0", 7) + string(rune('a'+f.seq)), name: s("deviceName"), hwid: hwid, pub: pub, status: "active"}
		if d.name == "" {
			d.name = "Пристрій"
		}
		f.devs = append(f.devs, d)
		return reply(200, session(d))

	case "/api/account/login":
		if s("accountId") != f.acc {
			return errc(401, "unauthorized")
		}
		sig, _ := b64.DecodeString(s("sig"))
		msg := loginMessage(s("accountId"), hwid, n("ts"), s("nonce"))
		for _, d := range f.devs {
			if s("deviceId") != "" && s("deviceId") != d.id {
				continue
			}
			if !ed25519.Verify(d.pub, []byte(msg), sig) {
				continue
			}
			if d.status != "active" {
				return errc(401, "device_revoked")
			}
			if d.hwid != hwid {
				return errc(403, "hwid_mismatch")
			}
			if f.nonces[s("nonce")] {
				return errc(400, "replay")
			}
			f.nonces[s("nonce")] = true
			return reply(200, session(d))
		}
		return errc(401, "unauthorized")
	}

	cur := f.byToken(h)
	if cur == nil {
		return errc(401, "unauthorized")
	}
	if cur.status != "active" {
		return errc(401, "device_revoked")
	}
	switch path {
	case "/api/account/devices":
		l := DeviceList{Current: cur.id, Max: f.max, Devices: []Device{}}
		for _, d := range f.devs {
			l.Devices = append(l.Devices, Device{ID: d.id, Name: d.name, ClientKind: "launcher", ClientName: "Shaurma Launcher",
				LastIP: "203.0.113.5", LastCountry: "UA", Status: d.status, Current: d.id == cur.id, Online: d.id == cur.id, RevokeReason: d.reason})
		}
		return reply(200, l)
	case "/api/account/devices/rename":
		for _, d := range f.devs {
			if d.id == s("deviceId") {
				d.name = s("name")
				return reply(200, map[string]bool{"ok": true})
			}
		}
		return errc(404, "device_not_found")
	case "/api/account/devices/revoke", "/api/account/devices/not-me":
		for _, d := range f.devs {
			if d.id == s("deviceId") && d.status == "active" {
				d.status, d.reason, d.by = "revoked", "user", cur.id
				return reply(200, map[string]any{"ok": true, "rotateKey": path == "/api/account/devices/not-me", "nextVersion": len(f.keys) + 1})
			}
		}
		return errc(404, "device_not_found")
	case "/api/account/devices/revoke-others":
		c := 0
		for _, d := range f.devs {
			if d.id != cur.id && d.status == "active" {
				d.status, d.reason, d.by = "revoked", "revoke_others", cur.id
				c++
			}
		}
		return reply(200, RevokeOthersResult{Revoked: c, RestoreUntil: 1})
	case "/api/account/devices/restore":
		c := 0
		for _, d := range f.devs {
			if d.reason == "revoke_others" && d.by == cur.id && d.status != "active" {
				d.status, d.reason = "active", ""
				c++
			}
		}
		if c == 0 {
			return errc(404, "nothing_to_restore")
		}
		return reply(200, map[string]any{"ok": true, "restored": c})
	case "/api/account/emergency-reset":
		c := 0
		for _, d := range f.devs {
			if d.id != cur.id && d.status == "active" {
				d.status, d.reason = "revoked", "emergency"
				c++
			}
		}
		return reply(200, map[string]any{"ok": true, "revoked": c, "rotateKey": true, "nextVersion": len(f.keys) + 1})
	case "/api/account/audit":
		return reply(200, map[string]any{"events": append([]AuditEvent{}, f.audit...)})
	case "/api/account/chat-key":
		switch f.keyMode {
		case "disabled":
			return errc(503, "chat_key_disabled")
		case "unavailable":
			return errc(503, "key_unavailable")
		case "old":
			return errc(404, "nf")
		}
		if method == "GET" {
			if len(f.keys) == 0 {
				return errc(404, "no_chat_key")
			}
			return reply(200, ChatKeys{Keys: f.keys, Current: f.keys[len(f.keys)-1].Version})
		}
		v := int(n("version"))
		if v <= len(f.keys) {
			return reply(409, map[string]any{"error": "key_exists", "keys": f.keys})
		}
		f.keys = append(f.keys, ChatKey{Version: v, ChatPublicKey: s("chatPublicKey"), SecretKey: s("secretKey"), Current: true})
		return reply(200, ChatKeys{Keys: f.keys, Current: v})
	}
	return errc(404, "nf")
}

// newDevMgr — як newMgr, але для devVPS.
func newDevMgr(t *testing.T, hw string, f *devVPS) *Manager {
	t.Helper()
	st, err := NewSecureStorage(t.TempDir(), fixedHWID(hw))
	if err != nil {
		t.Fatal(err)
	}
	return NewManager(Config{HWID: fixedHWID(hw), Crypto: NewCrypto(), Storage: st, Transport: f})
}

// двоє «ПК» з різними ключами/HWID, один акаунт
func devSetup(t *testing.T) (f *devVPS, m1, m2 *Manager) {
	t.Helper()
	f = newDevVPS()
	m1 = newDevMgr(t, pcA, f)
	m2 = newDevMgr(t, pcB, f)
	return
}

func recoverAdopt(t *testing.T, m *Manager, launcherID string, revoke string) (*Recovered, error) {
	t.Helper()
	p, _ := m.Prepare()
	r, err := m.RecoverReplacing(context.Background(), p, "google", "tok:"+p.Nonce(), revoke)
	if err != nil {
		return nil, err
	}
	if err := m.Adopt(launcherID, r); err != nil {
		t.Fatal(err)
	}
	return r, nil
}

func TestTwoDevicesBothWorkAfterRecover(t *testing.T) {
	f, m1, m2 := devSetup(t)
	ctx := context.Background()
	if _, err := recoverAdopt(t, m1, "pc1", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := recoverAdopt(t, m2, "pc2", ""); err != nil {
		t.Fatal(err)
	}
	// Обидва залишаються діючими: ПК1 НЕ вибито входом на ПК2 (раніше казало «Потрібен вхід»).
	m1.Invalidate("pc1")
	m2.Invalidate("pc2")
	s1, err1 := m1.Session(ctx, "pc1")
	s2, err2 := m2.Session(ctx, "pc2")
	if err1 != nil || err2 != nil || s1.DeviceID == "" || s1.DeviceID == s2.DeviceID {
		t.Fatalf("сесії: %v %v", err1, err2)
	}
	if got := m1.DeviceID("pc1"); got != s1.DeviceID {
		t.Fatalf("id пристрою має зберегтись локально: %q", got)
	}
	l, err := m1.Devices(ctx, "pc1")
	if err != nil || len(l.Devices) != 2 || l.Current != s1.DeviceID {
		t.Fatalf("list: %v %+v", err, l)
	}
	_ = f
}

func TestSessionErrorsAreSpecific(t *testing.T) {
	f, m1, m2 := devSetup(t)
	ctx := context.Background()
	recoverAdopt(t, m1, "pc1", "")
	r2, _ := recoverAdopt(t, m2, "pc2", "")
	m1.Invalidate("pc1")

	// ПК2 вилучає ПК1: ПК1 отримує ПОЯСНЕННЯ, а не «потрібен вхід», і нічого не стирає локально.
	if _, err := m2.RevokeDevice(ctx, "pc2", m1.DeviceID("pc1"), false); err != nil {
		t.Fatal(err)
	}
	if _, err := m1.Session(ctx, "pc1"); !errors.Is(err, ErrDeviceRevoked) {
		t.Fatalf("очікував ErrDeviceRevoked: %v", err)
	}
	if _, ok := m1.Cell("pc1"); !ok {
		t.Fatal("локальна комірка має ЛИШИТИСЬ після вилучення")
	}
	if _, err := m1.Devices(ctx, "pc1"); !errors.Is(err, ErrDeviceRevoked) {
		t.Fatalf("авторизовані виклики теж: %v", err)
	}
	// Повернення входом: новий пристрій, дані акаунта на місці, стара комірка заархівована.
	old, _ := m1.c.Storage.Get(key("pc1"))
	if _, err := recoverAdopt(t, m1, "pc1", ""); err != nil {
		t.Fatalf("повторний вхід: %v", err)
	}
	if bak, err := m1.c.Storage.Get(bakKey("pc1")); err != nil || string(bak) != string(old) {
		t.Fatalf("стару комірку не заархівовано: %v", err)
	}
	m1.Invalidate("pc1")
	if _, err := m1.Session(ctx, "pc1"); err != nil {
		t.Fatalf("після повторного входу: %v", err)
	}
	_ = r2

	// Інший ПК (HWID) з тим самим ключем.
	m3 := newDevMgr(t, pcB, f)
	_ = m3
	// Мережевий збій — НЕ вихід: помилка транспорту повертається як є, комірка лишається.
	down := NewManager(Config{HWID: fixedHWID(pcA), Crypto: NewCrypto(), Storage: m1.c.Storage,
		Transport: transportFunc(func(context.Context, string, string, map[string]string, []byte) (int, []byte, error) {
			return 0, nil, errors.New("network down")
		})})
	if _, err := down.Session(ctx, "pc1"); err == nil || errors.Is(err, ErrGone) || errors.Is(err, ErrDeviceRevoked) || errors.Is(err, ErrWrongDevice) {
		t.Fatalf("збій мережі не має виглядати як вихід: %v", err)
	}
	if _, ok := down.Cell("pc1"); !ok {
		t.Fatal("збій мережі не має стирати комірку")
	}
}

func TestDeviceLimitFlowReplacesChosenDevice(t *testing.T) {
	f, _, _ := devSetup(t)
	f.max = 2
	ctx := context.Background()
	ms := make([]*Manager, 3)
	for i := range ms {
		ms[i] = newDevMgr(t, pcA, f)
	}
	recoverAdopt(t, ms[0], "a", "")
	recoverAdopt(t, ms[1], "b", "")
	p, _ := ms[2].Prepare()
	_, err := ms[2].Recover(ctx, p, "google", "tok:"+p.Nonce())
	var le *DeviceLimitError
	if !errors.As(err, &le) || le.Max != 2 || len(le.Devices) != 2 {
		t.Fatalf("очікував DeviceLimitError зі списком: %v", err)
	}
	// Користувач обирає, кого вилучити; той самий id_token (другого входу в браузері нема).
	victim := le.Devices[0].ID
	r, err := ms[2].RecoverReplacing(ctx, p, "google", "tok:"+p.Nonce(), victim)
	if err != nil {
		t.Fatalf("replace: %v", err)
	}
	if r.Cell().AccountID != f.acc {
		t.Fatal("акаунт той самий")
	}
	for _, d := range f.devs {
		if d.id == victim && d.status != "revoked" {
			t.Fatal("обраний пристрій мав бути вилучений")
		}
	}
	// Підпис охоплює revokeDeviceId: підміна в дорозі ламає підпис.
	p2, _ := ms[2].Prepare()
	f.mu.Lock()
	f.max = 1
	f.mu.Unlock()
	if _, err := ms[2].RecoverReplacing(ctx, p2, "google", "tok:"+p2.Nonce(), victim); err == nil {
		t.Fatal("має бути або ліміт, або помилка, але не мовчазний успіх")
	}
}

func TestRevokeOthersAndRestoreAndEmergency(t *testing.T) {
	f, m1, m2 := devSetup(t)
	ctx := context.Background()
	recoverAdopt(t, m1, "pc1", "")
	recoverAdopt(t, m2, "pc2", "")
	r, err := m2.RevokeOthers(ctx, "pc2")
	if err != nil || r.Revoked != 1 {
		t.Fatalf("revoke-others: %v %+v", err, r)
	}
	m1.Invalidate("pc1")
	if _, err := m1.Session(ctx, "pc1"); !errors.Is(err, ErrDeviceRevoked) {
		t.Fatalf("%v", err)
	}
	if n, err := m2.RestoreDevices(ctx, "pc2"); err != nil || n != 1 {
		t.Fatalf("restore: %v %d", err, n)
	}
	if _, err := m1.Session(ctx, "pc1"); err != nil {
		t.Fatalf("повернений пристрій має працювати БЕЗ повторного входу: %v", err)
	}
	if _, err := m2.RestoreDevices(ctx, "pc2"); err == nil {
		t.Fatal("повторне повернення — помилка")
	}
	kr, err := m2.EmergencyReset(ctx, "pc2")
	if err != nil || !kr.RotateKey || kr.NextVersion != 1 {
		t.Fatalf("emergency: %v %+v", err, kr)
	}
	kr, err = m2.RevokeDevice(ctx, "pc2", m2.DeviceID("pc2"), true)
	if err != nil || !kr.RotateKey {
		t.Fatalf("not-me: %v %+v", err, kr)
	}
	if err := m2.RenameDevice(ctx, "pc2", m2.DeviceID("pc2"), "x"); !errors.Is(err, ErrDeviceRevoked) {
		t.Fatalf("після самовилучення: %v", err)
	}
	_ = f
}

func TestRenameAndAudit(t *testing.T) {
	f, m1, _ := devSetup(t)
	ctx := context.Background()
	recoverAdopt(t, m1, "pc1", "")
	if err := m1.RenameDevice(ctx, "pc1", m1.DeviceID("pc1"), "Домашній ПК"); err != nil {
		t.Fatal(err)
	}
	if f.devs[0].name != "Домашній ПК" {
		t.Fatal("не перейменовано")
	}
	f.audit = []AuditEvent{{ID: 1, Event: "device_added", Country: "UA"}}
	ev, err := m1.Audit(ctx, "pc1", 20)
	if err != nil || len(ev) != 1 || ev[0].Event != "device_added" {
		t.Fatalf("%v %+v", err, ev)
	}
}

func TestClientSendsDeviceDescriptionHeaders(t *testing.T) {
	f := newDevVPS()
	st, _ := NewSecureStorage(t.TempDir(), fixedHWID(pcA))
	m := NewManager(Config{HWID: fixedHWID(pcA), Crypto: NewCrypto(), Storage: st, Transport: f,
		ClientVersion: "1.4.2", OSName: "windows 11", DeviceName: func() string { return "ПК-Дім" }})
	recoverAdopt(t, m, "pc1", "")
	h := f.hdrs[0]
	if h["X-Shaurma-Client"] != "launcher/1.4.2" || h["X-Shaurma-OS"] != "windows 11" {
		t.Fatalf("%v", h)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(h["X-Shaurma-Device-Name"])
	if string(raw) != "ПК-Дім" || f.devs[0].name != "ПК-Дім" {
		t.Fatalf("назва: %q / %q", raw, f.devs[0].name)
	}
	if strings.Contains(url.QueryEscape(h["X-Shaurma-Device-Name"]), " ") {
		t.Fatal("заголовок має бути безпечним для HTTP")
	}
}

func TestClockSkewIsExplained(t *testing.T) {
	st, _ := NewSecureStorage(t.TempDir(), fixedHWID(pcA))
	tr := transportFunc(func(context.Context, string, string, map[string]string, []byte) (int, []byte, error) {
		return 400, []byte(`{"error":"stale_request"}`), nil
	})
	m := NewManager(Config{HWID: fixedHWID(pcA), Crypto: NewCrypto(), Storage: st, Transport: tr})
	p, _ := m.Prepare()
	if _, err := m.Recover(context.Background(), p, "google", "tok:"+p.Nonce()); !errors.Is(err, ErrClockSkew) {
		t.Fatalf("%v", err)
	}
}

// ── ключі чату ────────────────────────────────────────────────────────────

func TestChatKeyApiMapping(t *testing.T) {
	f, m1, m2 := devSetup(t)
	ctx := context.Background()
	recoverAdopt(t, m1, "pc1", "")
	recoverAdopt(t, m2, "pc2", "")
	if _, err := m1.GetChatKeys(ctx, "pc1"); !errors.Is(err, ErrNoChatKey) {
		t.Fatalf("%v", err)
	}
	ks, err := m1.PutChatKey(ctx, "pc1", 1, "PUB1", "SEC1")
	if err != nil || ks.Current != 1 {
		t.Fatalf("%v", err)
	}
	// Другий пристрій отримує той самий ключ — історія розшифровується.
	got, err := m2.GetChatKeys(ctx, "pc2")
	if err != nil || got.Keys[0].SecretKey != "SEC1" {
		t.Fatalf("%v %+v", err, got)
	}
	// first-wins: чужа пара тієї ж версії → конфлікт із ключами переможця.
	_, err = m2.PutChatKey(ctx, "pc2", 1, "PUB2", "SEC2")
	var c *ChatKeysConflict
	if !errors.As(err, &c) || c.Keys.Keys[0].ChatPublicKey != "PUB1" {
		t.Fatalf("очікував конфлікт: %v", err)
	}
	for mode, want := range map[string]error{"disabled": ErrChatKeyDisabled, "unavailable": ErrChatKeyUnavailable, "old": ErrChatKeyDisabled} {
		f.keyMode = mode
		if _, err := m1.GetChatKeys(ctx, "pc1"); !errors.Is(err, want) {
			t.Fatalf("%s: %v", mode, err)
		}
	}
}
