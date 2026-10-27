package shaurmaid

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

type fixedHWID string

func (f fixedHWID) HWID() (string, error) { return string(f), nil }

const (
	pcA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	pcB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// fakeVPS — мінімальний сервер за документованим протоколом: перевіряє підписи Ed25519.
type fakeVPS struct {
	mu       sync.Mutex
	users    map[string]*fu
	nonces   map[string]bool
	logins   int
	lastIDTk string
	seenTok  []string
	lastReg  map[string]any
	mojang   map[string]string // uuid → ім'я в Mojang
}
type fu struct {
	pub  ed25519.PublicKey
	hwid string
	nick string
	typ  string
	uuid string
	// ident — «особа» акаунта (provider|known) для сценаріїв відновлення
	ident string
}

func (f *fakeVPS) Do(_ context.Context, method, path string, h map[string]string, body []byte) (int, []byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var q map[string]any
	_ = json.Unmarshal(body, &q)
	s := func(k string) string { v, _ := q[k].(string); return v }
	n := func(k string) int64 { v, _ := q[k].(float64); return int64(v) }
	reply := func(st int, v any) (int, []byte, error) { b, _ := json.Marshal(v); return st, b, nil }
	hwid := h["X-Shaurma-HWID"]
	switch path {
	case "/api/account/register":
		pub, _ := b64.DecodeString(s("publicKey"))
		sig, _ := b64.DecodeString(s("sig"))
		f.lastIDTk = s("idToken")
		msg := registerMessage(s("nickname"), s("publicKey"), s("slot"), s("provider"), s("type"), s("uuid"), s("idToken"), n("ts"), s("nonce"), hwid)
		if !ed25519.Verify(pub, []byte(msg), sig) {
			return reply(401, map[string]string{"error": "bad_signature"})
		}
		if s("idToken") != "tok:"+OIDCNonce(s("publicKey"), s("slot")) {
			return reply(401, map[string]string{"error": "bad_identity"})
		}
		f.lastReg = q
		typ, uuid, nick := s("type"), s("uuid"), s("nickname")
		switch typ {
		case TypeUnlicensed:
			if uuid != OfflineUUID(nick) {
				return reply(400, map[string]string{"error": "bad_uuid"})
			}
		case TypeLicensed:
			if NormalizeUUID(uuid) != uuid {
				return reply(400, map[string]string{"error": "bad_uuid"})
			}
			if mojang, ok := f.mojang[uuid]; ok { // сервер бере нік з Mojang, а не від клієнта
				nick = mojang
			}
		default:
			return reply(400, map[string]string{"error": "bad_type"})
		}
		id := "11111111-2222-4333-8444-" + strings.Repeat("0", 11) + string(rune('a'+len(f.users)))
		f.users[id] = &fu{pub: pub, hwid: hwid, nick: nick, typ: typ, uuid: uuid}
		return reply(200, sessionResp{AccountID: id, Nickname: nick, AccessToken: "jwt-" + id, ExpiresInS: 3600})
	case "/api/account/recover":
		pub, _ := b64.DecodeString(s("publicKey"))
		sig, _ := b64.DecodeString(s("sig"))
		msg := recoverMessage(s("publicKey"), s("slot"), s("provider"), s("idToken"), n("ts"), s("nonce"), hwid)
		if n("v") >= 2 {
			msg = recoverMessageV2(s("publicKey"), s("slot"), s("provider"), s("idToken"), n("ts"), s("nonce"), hwid, s("revokeDeviceId"), s("deviceName"))
		}
		if !ed25519.Verify(pub, []byte(msg), sig) {
			return reply(401, map[string]string{"error": "bad_signature"})
		}
		if s("idToken") != "tok:"+OIDCNonce(s("publicKey"), s("slot")) {
			return reply(401, map[string]string{"error": "bad_identity"})
		}
		for id, u := range f.users {
			if u.ident == s("provider")+"|known" {
				u.pub, u.hwid = pub, hwid // сервер прив'язує акаунт до нового ключа/ПК
				return reply(200, sessionResp{AccountID: id, Nickname: u.nick, AccessToken: "jwt-" + id, ExpiresInS: 3600,
					Type: u.typ, UUID: u.uuid})
			}
		}
		return reply(404, map[string]string{"error": "no_account"})
	case "/api/account/nick-check":
		if strings.EqualFold(s("nickname"), "Notch") {
			return reply(200, NickCheck{Available: false, Reason: "mojang"})
		}
		return reply(200, NickCheck{Available: true})
	case "/api/account/login":
		f.logins++
		u := f.users[s("accountId")]
		if u == nil {
			return reply(401, map[string]string{"error": "unauthorized"})
		}
		if u.hwid != hwid {
			return reply(403, map[string]string{"error": "hwid_mismatch"})
		}
		sig, _ := b64.DecodeString(s("sig"))
		if !ed25519.Verify(u.pub, []byte(loginMessage(s("accountId"), hwid, n("ts"), s("nonce"))), sig) || f.nonces[s("nonce")] {
			return reply(401, map[string]string{"error": "unauthorized"})
		}
		f.nonces[s("nonce")] = true
		return reply(200, sessionResp{AccountID: s("accountId"), Nickname: u.nick, AccessToken: "jwt-" + s("accountId"), ExpiresInS: 3600})
	case "/api/account/nickname/sync":
		for id, u := range f.users {
			if h["Authorization"] == "Bearer jwt-"+id {
				if u.typ != TypeLicensed {
					return reply(400, map[string]string{"error": "nick_not_synced"})
				}
				if n, ok := f.mojang[u.uuid]; ok {
					u.nick = n
				}
				return reply(200, map[string]any{"ok": true, "nickname": u.nick})
			}
		}
		return reply(401, map[string]string{"error": "unauthorized"})
	case "/api/account/me":
		for id, u := range f.users {
			if h["Authorization"] == "Bearer jwt-"+id {
				return reply(200, Profile{AccountID: id, Nickname: u.nick, Type: u.typ, UUID: u.uuid,
					CreatedAt: 1700000000000, NickLocked: u.typ == TypeLicensed, NickChangesLeft: 2})
			}
		}
		return reply(401, map[string]string{"error": "unauthorized"})
	case "/api/account/nickname":
		f.seenTok = append(f.seenTok, h["Authorization"])
		for id, u := range f.users {
			if h["Authorization"] == "Bearer jwt-"+id {
				if u.typ == TypeLicensed {
					return reply(403, map[string]string{"error": "nick_locked"})
				}
				u.nick = s("nickname")
				return reply(200, map[string]bool{"ok": true})
			}
		}
		return reply(401, map[string]string{"error": "unauthorized"})
	case "/api/account/delete":
		for id := range f.users {
			if h["Authorization"] == "Bearer jwt-"+id {
				delete(f.users, id)
				return reply(200, map[string]bool{"ok": true})
			}
		}
		return reply(401, map[string]string{"error": "unauthorized"})
	}
	return reply(404, map[string]string{"error": "nf"})
}

func newMgr(t *testing.T, hw string, vps *fakeVPS, st LocalSecureStorage) (*Manager, LocalSecureStorage) {
	if st == nil {
		var err error
		st, err = NewSecureStorage(t.TempDir(), fixedHWID(hw))
		if err != nil {
			t.Fatal(err)
		}
	}
	return NewManager(Config{HWID: fixedHWID(hw), Crypto: NewCrypto(), Storage: st, Transport: vps}), st
}

func src(_ context.Context, _ string, nonce string) (string, error) { return "tok:" + nonce, nil }

func TestRegisterCreatesIsolatedCells(t *testing.T) {
	vps := &fakeVPS{users: map[string]*fu{}, nonces: map[string]bool{}}
	m, _ := newMgr(t, pcA, vps, nil)
	ctx := context.Background()
	a, err := m.Register(ctx, "launcher-acc-1", RegisterOpts{Nickname: "Steve", Provider: "google", Type: TypeUnlicensed}, src)
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.Register(ctx, "launcher-acc-2", RegisterOpts{Nickname: "Steve", Provider: "google", Type: TypeUnlicensed}, src)
	if err != nil || a.AccountID == b.AccountID {
		t.Fatalf("кожен акаунт має свою комірку: %v", err)
	}
	// третій лаунчер-акаунт «лише з ніком» — комірки немає
	if _, ok := m.Cell("offline-nick-only"); ok {
		t.Fatal("акаунт лише з ніком не має мати комірки")
	}
	if _, err := m.Session(ctx, "offline-nick-only"); !errors.Is(err, ErrNoCell) {
		t.Fatalf("сесії без комірки не буває: %v", err)
	}
	if _, err := m.Register(ctx, "launcher-acc-1", RegisterOpts{Nickname: "Again", Provider: "google", Type: TypeUnlicensed}, src); !errors.Is(err, ErrCellExists) {
		t.Fatalf("повторна реєстрація: %v", err)
	}
	if _, err := m.Register(ctx, "x", RegisterOpts{Nickname: "bad nick!", Provider: "google", Type: TypeUnlicensed}, src); !errors.Is(err, ErrNoNickname) {
		t.Fatal("нік має валідуватись до мережі")
	}
}

func TestSessionCachedInMemoryAndReloginBySignature(t *testing.T) {
	vps := &fakeVPS{users: map[string]*fu{}, nonces: map[string]bool{}}
	dir := t.TempDir()
	st, _ := NewSecureStorage(dir, fixedHWID(pcA))
	m, _ := newMgr(t, pcA, vps, st)
	ctx := context.Background()
	if _, err := m.Register(ctx, "acc", RegisterOpts{Nickname: "Alex", Provider: "microsoft", Type: TypeUnlicensed}, src); err != nil {
		t.Fatal(err)
	}
	s1, err := m.Session(ctx, "acc")
	if err != nil || vps.logins != 0 {
		t.Fatalf("після реєстрації сесія вже є: logins=%d %v", vps.logins, err)
	}
	// «перезапуск лаунчера»: нова пам'ять, те саме сховище → вхід підписом, без refresh-токена
	m2, _ := newMgr(t, pcA, vps, st)
	s2, err := m2.Session(ctx, "acc")
	if err != nil || vps.logins != 1 || s2.AccountID != s1.AccountID {
		t.Fatalf("relogin: %v logins=%d", err, vps.logins)
	}
	if _, err := m2.Session(ctx, "acc"); err != nil || vps.logins != 1 {
		t.Fatal("друга сесія — з кешу в пам'яті")
	}
}

func TestWrongDeviceAndGone(t *testing.T) {
	vps := &fakeVPS{users: map[string]*fu{}, nonces: map[string]bool{}}
	dir := t.TempDir()
	stA, _ := NewSecureStorage(dir, fixedHWID(pcA))
	mA, _ := newMgr(t, pcA, vps, stA)
	ctx := context.Background()
	if _, err := mA.Register(ctx, "acc", RegisterOpts{Nickname: "Alex", Provider: "google", Type: TypeUnlicensed}, src); err != nil {
		t.Fatal(err)
	}
	// ті самі файли, але інший ПК (інший HWID)
	mB, _ := newMgr(t, pcB, vps, stA)
	if _, err := mB.Session(ctx, "acc"); !errors.Is(err, ErrWrongDevice) {
		t.Fatalf("інший ПК: %v", err)
	}
	// сервер забув акаунт
	vps.users = map[string]*fu{}
	mC, _ := newMgr(t, pcA, vps, stA)
	if _, err := mC.Session(ctx, "acc"); !errors.Is(err, ErrGone) {
		t.Fatalf("gone: %v", err)
	}
}

func TestNicknameAndDeleteWipeEverything(t *testing.T) {
	vps := &fakeVPS{users: map[string]*fu{}, nonces: map[string]bool{}}
	m, st := newMgr(t, pcA, vps, nil)
	ctx := context.Background()
	m.Register(ctx, "acc", RegisterOpts{Nickname: "Alex", Provider: "google", Type: TypeUnlicensed}, src)
	if err := m.SetNickname(ctx, "acc", "Neo"); err != nil {
		t.Fatal(err)
	}
	if c, _ := m.Cell("acc"); c.Nickname != "Neo" {
		t.Fatal("нік має оновитись локально")
	}
	if err := m.Delete(ctx, "acc"); err != nil {
		t.Fatal(err)
	}
	if len(vps.users) != 0 {
		t.Fatal("акаунт має зникнути на сервері")
	}
	if _, err := st.Get(key("acc")); !errors.Is(err, ErrNotFound) {
		t.Fatal("ключ має бути стертий локально")
	}
	if _, ok := m.Cell("acc"); ok {
		t.Fatal("комірки немає")
	}
}

func TestServerNeverSeesPrivateKeyOrLicenseData(t *testing.T) {
	vps := &fakeVPS{users: map[string]*fu{}, nonces: map[string]bool{}}
	var captured [][]byte
	tr := transportFunc(func(ctx context.Context, method, path string, h map[string]string, body []byte) (int, []byte, error) {
		captured = append(captured, append([]byte(path+"|"), body...))
		return vps.Do(ctx, method, path, h, body)
	})
	st, _ := NewSecureStorage(t.TempDir(), fixedHWID(pcA))
	m := NewManager(Config{HWID: fixedHWID(pcA), Crypto: NewCrypto(), Storage: st, Transport: tr})
	ctx := context.Background()
	m.Register(ctx, "acc", RegisterOpts{Nickname: "Alex", Provider: "google", Type: TypeUnlicensed}, src)
	raw, _ := st.Get(key("acc"))
	var rec cellRecord
	json.Unmarshal(raw, &rec)
	id, _ := ParseIdentity([]byte(rec.Identity))
	priv := b64.EncodeToString(id.private)
	for _, c := range captured {
		s := string(c)
		if strings.Contains(s, priv) || strings.Contains(s, priv[:20]) {
			t.Fatal("приватний ключ не має йти на сервер")
		}
		for _, bad := range []string{"accessToken", "refreshToken", "minecraft", "xbox", "mojang", "msAccessToken", "xbl"} {
			if strings.Contains(strings.ToLower(s), strings.ToLower(bad)) && bad != "accessToken" {
				t.Fatalf("у запиті є %q: %s", bad, s)
			}
		}
	}
}

type transportFunc func(ctx context.Context, method, path string, h map[string]string, body []byte) (int, []byte, error)

func (f transportFunc) Do(ctx context.Context, m, p string, h map[string]string, b []byte) (int, []byte, error) {
	return f(ctx, m, p, h, b)
}

func TestRegisterCleansUpServerOnLocalSaveFailure(t *testing.T) {
	vps := &fakeVPS{users: map[string]*fu{}, nonces: map[string]bool{}}
	m := NewManager(Config{HWID: fixedHWID(pcA), Crypto: NewCrypto(), Storage: failingStore{}, Transport: vps})
	if _, err := m.Register(context.Background(), "acc", RegisterOpts{Nickname: "Alex", Provider: "google", Type: TypeUnlicensed}, src); err == nil {
		t.Fatal("очікував помилку збереження")
	}
	if len(vps.users) != 0 {
		t.Fatal("сирота на сервері має бути прибраний")
	}
}

type failingStore struct{}

func (failingStore) Put(string, []byte) error   { return errors.New("disk full") }
func (failingStore) Get(string) ([]byte, error) { return nil, ErrNotFound }
func (failingStore) Delete(string) error        { return nil }

func TestStorageEncryptedAndDeviceBound(t *testing.T) {
	dir := t.TempDir()
	st, _ := NewSecureStorage(dir, fixedHWID(pcA))
	if err := st.Put("k", []byte("super-secret-material")); err != nil {
		t.Fatal(err)
	}
	if got, err := st.Get("k"); err != nil || string(got) != "super-secret-material" {
		t.Fatal(err)
	}
	if _, err := st.Get("missing"); !errors.Is(err, ErrNotFound) {
		t.Fatal("not found")
	}
	if runtime.GOOS == "windows" {
		// Windows: DPAPI прив'язує секрет до КОРИСТУВАЧА (HWID не входить), тож
		// перевіряємо інше — на диску немає відкритого тексту.
		ents, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range ents {
			b, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(b), "super-secret-material") {
				t.Fatalf("секрет лежить на диску відкритим текстом: %s", e.Name())
			}
		}
	} else {
		// Не-Windows: AES-GCM із ключем від HWID — копія файлів на іншому ПК не читається.
		if st2, err := NewSecureStorage(dir, fixedHWID(pcB)); err == nil {
			if got, err := st2.Get("k"); err == nil && string(got) == "super-secret-material" {
				t.Fatal("чужий ПК не повинен розшифрувати")
			}
		}
	}
	if err := st.Delete("k"); err != nil {
		t.Fatal(err)
	}
}

func TestHWIDStableOrderIndependentNoMAC(t *testing.T) {
	a, err := computeHWID(map[string]string{"machine": "ABC", "cpu": "Ryzen", "board": "B550"})
	if err != nil || len(a) != 64 {
		t.Fatal(err)
	}
	b, _ := computeHWID(map[string]string{"board": "b550", "cpu": " ryzen ", "machine": "abc", "empty": ""})
	if a != b {
		t.Fatal("HWID не залежить від регістру/пробілів/порядку/порожніх полів")
	}
	if c, _ := computeHWID(map[string]string{"machine": "OTHER", "cpu": "Ryzen", "board": "B550"}); c == a {
		t.Fatal("інший ПК — інший HWID")
	}
	if _, err := computeHWID(map[string]string{"cpu": "x"}); err == nil {
		t.Fatal("без machine-id відбиток ненадійний")
	}
	if _, has := collectHWIDParts()["mac"]; has {
		t.Fatal("MAC не використовується")
	}
}

// ── тип акаунта, UUID, один вхід ────────────────────────────────────────

const steveUUID = "069a79f444e94726a5befca90e38aaf5"

func TestUnlicensedSendsTypeAndOfflineUUID(t *testing.T) {
	vps := &fakeVPS{users: map[string]*fu{}, nonces: map[string]bool{}}
	m, _ := newMgr(t, pcA, vps, nil)
	c, err := m.Register(context.Background(), "acc", RegisterOpts{Nickname: "Alex", Provider: "google", Type: TypeUnlicensed, UUID: steveUUID}, src)
	if err != nil {
		t.Fatal(err)
	}
	// UUID, який передав виклик, ігнорується: для неліцензійного він завжди = офлайн-UUID ніка.
	if vps.lastReg["type"] != TypeUnlicensed || vps.lastReg["uuid"] != OfflineUUID("Alex") {
		t.Fatalf("на сервер пішло: %v", vps.lastReg)
	}
	if c.Type != TypeUnlicensed || c.UUID != OfflineUUID("Alex") || c.Licensed() || c.CreatedAt == 0 {
		t.Fatalf("комірка: %+v", c)
	}
}

// Один вхід у Microsoft: Prepare дає nonce ДО входу, id_token приходить із цього самого входу.
func TestLicensedOneLoginWithNonceAndLockedNick(t *testing.T) {
	vps := &fakeVPS{users: map[string]*fu{}, nonces: map[string]bool{}, mojang: map[string]string{steveUUID: "Real_Steve"}}
	m, _ := newMgr(t, pcA, vps, nil)
	ctx := context.Background()
	p, err := m.Prepare()
	if err != nil || len(p.Nonce()) != 43 {
		t.Fatalf("Prepare: %v nonce=%q", err, p.Nonce())
	}
	// Імітуємо вхід у Microsoft, у запит якого лаунчер поклав p.Nonce(): id_token містить цей nonce.
	idToken := "tok:" + p.Nonce()
	c, err := m.RegisterWithToken(ctx, "acc", RegisterOpts{Nickname: "Whatever", Provider: "microsoft", Type: TypeLicensed, UUID: "069A79F4-44E9-4726-A5BE-FCA90E38AAF5"}, p, idToken)
	if err != nil {
		t.Fatal(err)
	}
	if vps.lastReg["type"] != TypeLicensed || vps.lastReg["uuid"] != steveUUID {
		t.Fatalf("на сервер пішло: %v", vps.lastReg)
	}
	if c.Nickname != "Real_Steve" || !c.Licensed() || c.UUID != steveUUID {
		t.Fatalf("комірка ліцензійного: %+v", c)
	}
	// нік не міняється вручну — відмова ДО мережі
	before := len(vps.seenTok)
	if err := m.SetNickname(ctx, "acc", "Hacker"); !errors.Is(err, ErrNickLocked) || len(vps.seenTok) != before {
		t.Fatalf("SetNickname ліцензійного: %v", err)
	}
	// Mojang змінив ім'я → синхронізація оновлює локальну копію
	vps.mojang[steveUUID] = "Renamed"
	if n, err := m.SyncNickname(ctx, "acc"); err != nil || n != "Renamed" {
		t.Fatalf("sync: %q %v", n, err)
	}
	if cc, _ := m.Cell("acc"); cc.Nickname != "Renamed" {
		t.Fatalf("локальний нік: %q", cc.Nickname)
	}
	pr, err := m.Profile(ctx, "acc")
	if err != nil || pr.Type != TypeLicensed || !pr.NickLocked || pr.UUID != steveUUID {
		t.Fatalf("profile: %+v %v", pr, err)
	}
}

func TestRegisterWithTokenRejectsBadInput(t *testing.T) {
	vps := &fakeVPS{users: map[string]*fu{}, nonces: map[string]bool{}}
	m, _ := newMgr(t, pcA, vps, nil)
	ctx := context.Background()
	p, _ := m.Prepare()
	ok := RegisterOpts{Nickname: "Alex", Provider: "microsoft", Type: TypeLicensed, UUID: steveUUID}
	for name, f := range map[string]func() error{
		"без заготовки": func() error { _, e := m.RegisterWithToken(ctx, "a", ok, nil, "tok"); return e },
		"без id_token":  func() error { _, e := m.RegisterWithToken(ctx, "a", ok, p, ""); return e },
		"тип": func() error {
			o := ok
			o.Type = "premium"
			_, e := m.RegisterWithToken(ctx, "a", o, p, "tok")
			return e
		},
		"ліцензійний без uuid": func() error { o := ok; o.UUID = ""; _, e := m.RegisterWithToken(ctx, "a", o, p, "tok"); return e },
		"кривий uuid":          func() error { o := ok; o.UUID = "zz"; _, e := m.RegisterWithToken(ctx, "a", o, p, "tok"); return e },
	} {
		if err := f(); err == nil {
			t.Errorf("%s: очікував помилку", name)
		}
	}
	if len(vps.users) != 0 {
		t.Fatal("на сервер нічого не мало піти")
	}
}

func TestUnlicensedNicknameChangeMovesUUID(t *testing.T) {
	vps := &fakeVPS{users: map[string]*fu{}, nonces: map[string]bool{}}
	m, st := newMgr(t, pcA, vps, nil)
	ctx := context.Background()
	m.Register(ctx, "acc", RegisterOpts{Nickname: "Alex", Provider: "google", Type: TypeUnlicensed}, src)
	if err := m.SetNickname(ctx, "acc", "Neo"); err != nil {
		t.Fatal(err)
	}
	c, _ := m.Cell("acc")
	if c.UUID != OfflineUUID("Neo") {
		t.Fatalf("UUID має слідувати за ніком: %q", c.UUID)
	}
	// старий запис без type/uuid (до цієї версії) читається як неліцензійний
	raw, _ := st.Get(key("acc"))
	var rec cellRecord
	json.Unmarshal(raw, &rec)
	rec.Type, rec.UUID, rec.CreatedAt = "", "", 0
	b, _ := json.Marshal(rec)
	st.Put(key("acc"), b)
	if c, _ := m.Cell("acc"); c.Type != TypeUnlicensed || c.UUID != OfflineUUID("Neo") {
		t.Fatalf("legacy: %+v", c)
	}
}

func TestOfflineUUIDKnownAnswer(t *testing.T) {
	// UUID.nameUUIDFromBytes("OfflinePlayer:Notch") у Java
	if got := OfflineUUID("Notch"); got != "b50ad385829d3141a2167e7d7539ba7f" {
		t.Fatalf("OfflineUUID(Notch) = %s", got)
	}
	if NormalizeUUID("069A79F4-44E9-4726-A5BE-FCA90E38AAF5") != steveUUID || NormalizeUUID("xx") != "" || NormalizeUUID("") != "" {
		t.Fatal("NormalizeUUID")
	}
	want := "shaurma-register-v3\nn\npk\nsl\nmicrosoft\nlicensed\nU\n" + hexOf(func() []byte { h := sha256.Sum256([]byte("t")); return h[:] }()) + "\n5\nno\nhw"
	if registerMessage("n", "pk", "sl", "microsoft", "licensed", "U", "t", 5, "no", "hw") != want {
		t.Fatal("registerMessage має збігатись з accounts.RegisterMessage на VPS (v3)")
	}
}

func TestPoWAndKnownAnswers(t *testing.T) {
	c := NewCrypto()
	id, _ := c.NewIdentity()
	ts := time.Now().UnixMilli()
	p, err := c.SolvePoW(id.PublicKey, ts, 12)
	if err != nil || p == "" {
		t.Fatal(err)
	}
	// known-answer: рядки мусять збігатися з VPS (accounts.PowInput / RegisterMessage / LoginMessage)
	if PowInput("pk", 123, "7") != "shaurma-pow-v1:pk:123:7" {
		t.Fatal("PowInput")
	}
	if loginMessage("acc", "hw", 5, "n") != "shaurma-login-v1\nacc\nhw\n5\nn" {
		t.Fatal("loginMessage")
	}
	if len(OIDCNonce("pk", "slot")) != 43 {
		t.Fatal("OIDCNonce")
	}
	sig, _ := c.Sign(id, "m")
	raw, _ := b64.DecodeString(sig)
	pub, _ := b64.DecodeString(id.PublicKey)
	if !ed25519.Verify(pub, []byte("m"), raw) {
		t.Fatal("підпис")
	}
	back, err := ParseIdentity(id.Export())
	if err != nil || back.PublicKey != id.PublicKey || back.Slot != id.Slot {
		t.Fatal("export/parse")
	}
}

func TestRecoverBindsExistingAccountToNewDevice(t *testing.T) {
	vps := &fakeVPS{users: map[string]*fu{
		"11111111-2222-4333-8444-000000000001": {nick: "Old_Player", typ: TypeUnlicensed, uuid: OfflineUUID("Old_Player"), ident: "google|known"},
	}, nonces: map[string]bool{}}
	m, _ := newMgr(t, pcA, vps, nil)
	ctx := context.Background()

	// особа без акаунта → ErrNoAccount, а ключ заготовки лишається придатним для Register
	pNew, _ := m.Prepare()
	if _, err := m.Recover(ctx, pNew, "microsoft", "tok:"+pNew.Nonce()); !errors.Is(err, ErrNoAccount) {
		t.Fatalf("нова особа: %v", err)
	}
	if _, err := m.RegisterWithToken(ctx, "offline-New_One", RegisterOpts{Nickname: "New_One", Provider: "microsoft", Type: TypeUnlicensed}, pNew, "tok:"+pNew.Nonce()); err != nil {
		t.Fatalf("реєстрація тим самим id_token: %v", err)
	}

	// особа з акаунтом на новому ПК → той самий акаунт, нік і тип з сервера
	p, _ := m.Prepare()
	r, err := m.Recover(ctx, p, "google", "tok:"+p.Nonce())
	if err != nil {
		t.Fatal(err)
	}
	c := r.Cell()
	if c.AccountID != "11111111-2222-4333-8444-000000000001" || c.Nickname != "Old_Player" || c.Licensed() {
		t.Fatalf("відновлено не те: %+v", c)
	}
	if _, ok := m.Cell("offline-Old_Player"); ok {
		t.Fatal("до Adopt нічого не зберігається")
	}
	if err := m.Adopt("offline-Old_Player", r); err != nil {
		t.Fatal(err)
	}
	if s, err := m.Session(ctx, "offline-Old_Player"); err != nil || s.AccountID != c.AccountID {
		t.Fatalf("сесія після відновлення: %v", err)
	}
	// чужий id_token (інший nonce) сервер відхиляє
	p2, _ := m.Prepare()
	if _, err := m.Recover(ctx, p2, "google", "tok:other"); err == nil || errors.Is(err, ErrNoAccount) {
		t.Fatalf("чужий id_token має відхилятись: %v", err)
	}
}

func TestNickAvailable(t *testing.T) {
	vps := &fakeVPS{users: map[string]*fu{}, nonces: map[string]bool{}}
	m, _ := newMgr(t, pcA, vps, nil)
	if r, err := m.NickAvailable(context.Background(), "notch"); err != nil || r.Available || r.Reason != "mojang" {
		t.Fatalf("%+v %v", r, err)
	}
	if r, err := m.NickAvailable(context.Background(), "Free_One"); err != nil || !r.Available {
		t.Fatalf("%+v %v", r, err)
	}
}
