package social

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	meID   = "aaaa1111"
	peerID = "bbbb2222"
)

// fakeKS — сервер конвертів (VPS). Поведінка повторює vps/internal/accounts: версії підряд, перший виграє.
type fakeKS struct {
	mu      sync.Mutex
	keys    []RemoteKey
	mode    string // "" | disabled | unavailable | revoked
	gets    int
	puts    int
	onPut   func(f *fakeKS) // хук «інший пристрій встиг раніше»
	pubSink func(string)    // дзеркало Worker: що опубліковано друзям
}

func (f *fakeKS) cur() int { return len(f.keys) }

func (f *fakeKS) all() *RemoteKeys {
	out := &RemoteKeys{Current: f.cur()}
	out.Keys = append(out.Keys, f.keys...)
	return out
}

func (f *fakeKS) modeErr() error {
	switch f.mode {
	case "disabled":
		return ErrRemoteDisabled
	case "unavailable":
		return ErrRemoteUnavailable
	case "revoked":
		return ErrDeviceRevoked
	}
	return nil
}

func newKS() *fakeKS { return &fakeKS{} }

func (f *fakeKS) seed(version int) (PublicKey, SecretKey) {
	pub, sec, _ := GenerateIdentity()
	f.keys = append(f.keys, RemoteKey{Version: version, PublicB64: EncodePublicKey(pub), SecretB64: b64StdEncode(sec[:]), CreatedAt: 1})
	return pub, sec
}

// ── адаптер KeyServer ────────────────────────────────────────────────────

type ksAdapter struct{ f *fakeKS }

func (a ksAdapter) GetKeys(_ context.Context) (*RemoteKeys, error) {
	a.f.mu.Lock()
	defer a.f.mu.Unlock()
	a.f.gets++
	if err := a.f.modeErr(); err != nil {
		return nil, err
	}
	if len(a.f.keys) == 0 {
		return nil, ErrRemoteNoKey
	}
	return a.f.all(), nil
}

func (a ksAdapter) PutKey(_ context.Context, version int, pub, sec string) (*RemoteKeys, error) {
	a.f.mu.Lock()
	if a.f.onPut != nil {
		hook := a.f.onPut
		a.f.onPut = nil
		a.f.mu.Unlock()
		hook(a.f)
		a.f.mu.Lock()
	}
	defer a.f.mu.Unlock()
	a.f.puts++
	if err := a.f.modeErr(); err != nil {
		return nil, err
	}
	if version <= len(a.f.keys) {
		return nil, &RemoteConflict{Keys: a.f.all()}
	}
	if version != len(a.f.keys)+1 {
		return nil, ErrRemoteUnavailable
	}
	a.f.keys = append(a.f.keys, RemoteKey{Version: version, PublicB64: pub, SecretB64: sec, CreatedAt: int64(version)})
	if a.f.pubSink != nil {
		a.f.pubSink(pub)
	}
	return a.f.all(), nil
}

// ── фейковий Worker (публічні ключі) ─────────────────────────────────────

type keyWorker struct {
	mu        sync.Mutex
	pub       map[string]string   // поточні опубліковані ключі
	hist      map[string][]string // історія (нові першими)
	posts     []string
	managedBy *fakeKS // якщо задано і є ключі — POST /friends/key → 409 key_managed
	down      bool
}

func newKeyWorker() *keyWorker {
	return &keyWorker{pub: map[string]string{}, hist: map[string][]string{}}
}

func (w *keyWorker) requester() Requester {
	return func(method, path string, body []byte) (*http.Response, error) {
		w.mu.Lock()
		defer w.mu.Unlock()
		if w.down {
			return nil, errors.New("network down")
		}
		switch {
		case method == http.MethodGet && strings.HasPrefix(path, "/friends/keys/"):
			id := strings.TrimPrefix(path, "/friends/keys/")
			h := w.hist[id]
			if len(h) == 0 {
				if p, ok := w.pub[id]; ok {
					h = []string{p}
				} else {
					return stubResponse(404, `{"error":"key_not_found"}`), nil
				}
			}
			var items []string
			for _, k := range h {
				items = append(items, `{"publicKey":"`+k+`"}`)
			}
			return stubResponse(200, `{"keys":[`+strings.Join(items, ",")+`]}`), nil
		case method == http.MethodGet && strings.HasPrefix(path, "/friends/key/"):
			id := strings.TrimPrefix(path, "/friends/key/")
			if p, ok := w.pub[id]; ok {
				return stubResponse(200, `{"accountId":"`+id+`","publicKey":"`+p+`"}`), nil
			}
			return stubResponse(404, `{"error":"key_not_found"}`), nil
		case method == http.MethodPost && path == "/friends/key":
			w.posts = append(w.posts, string(body))
			if w.managedBy != nil && len(w.managedBy.keys) > 0 {
				return stubResponse(409, `{"error":"key_managed"}`), nil
			}
			return stubResponse(200, `{"ok":true}`), nil
		}
		return stubResponse(404, `{"error":"nf"}`), nil
	}
}

func (w *keyWorker) publish(id string, pub PublicKey) {
	w.mu.Lock()
	k := EncodePublicKey(pub)
	w.pub[id] = k
	w.hist[id] = append([]string{k}, w.hist[id]...)
	w.mu.Unlock()
}

// device — «пристрій» акаунта me: свій каталог ключів, спільний сервер.
func device(t *testing.T, ks *fakeKS, w *keyWorker) (*ChatCrypto, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "keys")
	c := NewChatCrypto(NewKeyStore(dir), NewClient(w.requester()))
	if ks != nil {
		c.WithKeyServer(ksAdapter{ks}, func() string { return meID })
	}
	return c, dir
}

// peerCrypto — співрозмовник (локальний ключ, без сервера).
func peerCrypto(t *testing.T, w *keyWorker) *ChatCrypto {
	t.Helper()
	c := NewChatCrypto(NewKeyStore(filepath.Join(t.TempDir(), "peer")), NewClient(w.requester()))
	if err := c.Init(); err != nil {
		t.Fatal(err)
	}
	pub, _ := DecodePublicKey(c.PublicKeyB64())
	w.publish(peerID, pub)
	return c
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// ── сценарії ──────────────────────────────────────────────────────────────

// ГОЛОВНЕ: новий пристрій не публікує свій ключ, а бере існуючий із сервера.
func TestNewDeviceAdoptsServerKeyAndNeverGeneratesOwn(t *testing.T) {
	ks, w := newKS(), newKeyWorker()
	pub, _ := ks.seed(1)
	dev, dir := device(t, ks, w)
	if err := dev.Init(); err != nil {
		t.Fatal(err)
	}
	if dev.PublicKeyB64() != EncodePublicKey(pub) {
		t.Fatal("має використовувати ключ сервера")
	}
	if ks.puts != 0 {
		t.Fatalf("новий пристрій не мав нічого заливати на сервер: %d", ks.puts)
	}
	if fileExists(filepath.Join(dir, identityFileName)) {
		t.Fatal("власна пара не мала створюватись")
	}
	if !fileExists(filepath.Join(dir, keyringFileName)) {
		t.Fatal("кеш зв'язки має збережеться")
	}
	if st := dev.Status(); st.State != KeysReady || !st.Managed || st.Versions != 1 {
		t.Fatalf("%+v", st)
	}
	if err := dev.PublishOwnKey(); err != nil || len(w.posts) != 0 {
		t.Fatalf("сервер сам публікує публічний ключ; клієнт не втручається: %v %v", err, w.posts)
	}
}

// Історія переписки читається на другому пристрої так само.
func TestHistoryReadableOnSecondDevice(t *testing.T) {
	ks, w := newKS(), newKeyWorker()
	devA, _ := device(t, ks, w)
	w.pubSink(ks)
	if err := devA.Init(); err != nil {
		t.Fatal(err)
	}
	peer := peerCrypto(t, w)
	ap, _ := DecodePublicKey(devA.PublicKeyB64())
	w.publish(meID, ap)

	ct, nonce, err := devA.encryptE2E(peerID, "привіт, старий чате")
	if err != nil {
		t.Fatal(err)
	}
	reply, rn, _ := peer.encryptE2E(meID, "відповідь")

	// Новий ПК (порожній диск) — ключ із сервера.
	devB, _ := device(t, ks, w)
	if err := devB.Init(); err != nil {
		t.Fatal(err)
	}
	if pt, err := devB.DecryptFrom(peerID, ct, nonce); err != nil || pt != "привіт, старий чате" {
		t.Fatalf("власне старе повідомлення на новому ПК: %q %v", pt, err)
	}
	if pt, err := devB.DecryptFrom(peerID, reply, rn); err != nil || pt != "відповідь" {
		t.Fatalf("чуже повідомлення на новому ПК: %q %v", pt, err)
	}
}

func (w *keyWorker) pubSink(ks *fakeKS) {
	ks.pubSink = func(pub string) {
		w.mu.Lock()
		w.pub[meID] = pub
		w.hist[meID] = append([]string{pub}, w.hist[meID]...)
		w.mu.Unlock()
	}
	w.managedBy = ks
}

// Гонка двох нових пристроїв: перший виграє, другий усиновлює, свій лишає лише для читання.
func TestTwoNewDevicesRaceFirstWins(t *testing.T) {
	ks, w := newKS(), newKeyWorker()
	w.pubSink(ks)
	devA, _ := device(t, ks, w)
	devB, dirB := device(t, ks, w)
	// Поки B вирішує створити ключ, A встигає залити свій.
	var winner string
	ks.onPut = func(f *fakeKS) {
		f.mu.Lock()
		pub, sec, _ := GenerateIdentity()
		f.keys = append(f.keys, RemoteKey{Version: 1, PublicB64: EncodePublicKey(pub), SecretB64: b64StdEncode(sec[:])})
		winner = EncodePublicKey(pub)
		f.mu.Unlock()
	}
	if err := devB.Init(); err != nil {
		t.Fatal(err)
	}
	if devB.PublicKeyB64() != winner {
		t.Fatal("B має усиновити ключ переможця")
	}
	if err := devA.Init(); err != nil || devA.PublicKeyB64() != winner {
		t.Fatalf("A теж на ключі переможця: %v", err)
	}
	if len(ks.keys) != 1 {
		t.Fatalf("на сервері має лишитись одна версія: %d", len(ks.keys))
	}
	// Власну (програшну) пару B зберігає лише для читання.
	ring, err := NewKeyring(dirB).Load()
	if err != nil || len(ring.Entries) != 2 {
		t.Fatalf("зв'язка B: %v %+v", err, ring)
	}
	if ring.Entries[0].Kind != kindServer || ring.Entries[1].Kind != kindLegacy {
		t.Fatalf("%+v", ring.Entries)
	}
}

// Збій сервера/мережі НІКОЛИ не призводить до нового ключа.
func TestServerOutageNeverCreatesKey(t *testing.T) {
	for _, mode := range []string{"unavailable"} {
		ks, w := newKS(), newKeyWorker()
		ks.mode = mode
		dev, dir := device(t, ks, w)
		if err := dev.Init(); !errors.Is(err, ErrKeysPending) {
			t.Fatalf("%s: %v", mode, err)
		}
		if ks.puts != 0 || fileExists(filepath.Join(dir, identityFileName)) || fileExists(filepath.Join(dir, keyringFileName)) {
			t.Fatalf("%s: нічого не мало створюватись (puts=%d)", mode, ks.puts)
		}
		if _, _, err := dev.encryptE2E(peerID, "x"); !errors.Is(err, ErrKeysPending) {
			t.Fatalf("без ключів чат не пише: %v", err)
		}
		// Сервер повернувся — все налагоджується без втрат.
		ks.mode = ""
		ks.seed(1)
		dev.lastResync = time.Time{}
		dev.Resync()
		if err := dev.Init(); err != nil || dev.Status().State != KeysReady {
			t.Fatalf("після відновлення: %v", err)
		}
	}
	// Сервер у стані «не знаю про ключ, бо мережа недоступна» при вже наявному Worker-ключі — теж ні.
	ks, w := newKS(), newKeyWorker()
	w.down = true
	dev, _ := device(t, ks, w)
	if err := dev.Init(); err == nil && ks.puts != 0 {
		t.Fatal("при недоступному Worker ключ створювати не можна")
	}
}

func TestOfflineUsesCachedKeyring(t *testing.T) {
	ks, w := newKS(), newKeyWorker()
	ks.seed(1)
	dev, dir := device(t, ks, w)
	if err := dev.Init(); err != nil {
		t.Fatal(err)
	}
	pubOnline := dev.PublicKeyB64()
	// Перезапуск лаунчера без мережі: працюємо з кешу.
	ks.mode = "unavailable"
	dev2 := NewChatCrypto(NewKeyStore(dir), NewClient(w.requester())).WithKeyServer(ksAdapter{ks}, func() string { return meID })
	if err := dev2.Init(); err != nil {
		t.Fatalf("offline: %v", err)
	}
	if st := dev2.Status(); st.State != KeysReady || !st.Offline || dev2.PublicKeyB64() != pubOnline {
		t.Fatalf("%+v", st)
	}
	if !dev2.NeedsSync() {
		t.Fatal("має просити повторну синхронізацію")
	}
	// Тим часом інший пристрій ротував ключ; після повернення мережі підтягуємо v2.
	ks.mode = ""
	ks.seed(2)
	dev2.lastResync = time.Time{}
	dev2.Resync()
	if dev2.PublicKeyB64() == pubOnline || dev2.Status().Offline {
		t.Fatal("мала підтягнутись нова версія")
	}
}

// Міграція: ключ лежав лише на пристрої й збігається з опублікованим → заливається як v1.
func TestLegacyKeyMigratesToServerWhenItIsThePublishedOne(t *testing.T) {
	ks, w := newKS(), newKeyWorker()
	w.pubSink(ks)
	dev, dir := device(t, ks, w)
	pub, sec, _ := GenerateIdentity()
	if err := NewKeyStore(dir).save(pub, sec); err != nil {
		t.Fatal(err)
	}
	w.publish(meID, pub) // друзі шифрують саме на нього
	if err := dev.Init(); err != nil {
		t.Fatal(err)
	}
	if len(ks.keys) != 1 || ks.keys[0].PublicB64 != EncodePublicKey(pub) || ks.keys[0].SecretB64 != b64StdEncode(sec[:]) {
		t.Fatalf("v1 на сервері має бути спадковий ключ: %+v", ks.keys)
	}
	if dev.PublicKeyB64() != EncodePublicKey(pub) {
		t.Fatal("ключ не мав змінитись")
	}
	// identity.json лишається недоторканим.
	if p2, s2, err := NewKeyStore(dir).Load(); err != nil || p2 != pub || s2 != sec {
		t.Fatalf("identity.json змінено: %v", err)
	}
}

// Ключ друзів лишився на іншому пристрої: НЕ створюємо новий мовчки, чат у режимі «потрібне рішення».
func TestStaleLegacyKeyDoesNotOverwritePublishedKey(t *testing.T) {
	ks, w := newKS(), newKeyWorker()
	dev, dir := device(t, ks, w)
	stalePub, staleSec, _ := GenerateIdentity()
	_ = NewKeyStore(dir).save(stalePub, staleSec)
	livePub, _, _ := GenerateIdentity()
	w.publish(meID, livePub) // опубліковано інший (ключ на втраченому пристрої)

	if err := dev.Init(); err != nil {
		t.Fatalf("%v", err)
	}
	if ks.puts != 0 {
		t.Fatal("не можна заливати застарілий ключ: друзі шифрують на інший")
	}
	if st := dev.Status(); st.State != KeysNeedRecovery {
		t.Fatalf("%+v", st)
	}
	if _, _, err := dev.encryptE2E(peerID, "x"); !errors.Is(err, ErrKeysNeedRecovery) {
		t.Fatalf("писати не можна: %v", err)
	}
	// Читати старе, зашифроване на цей (спадковий) ключ, ще можна.
	peer := peerCrypto(t, w)
	peerPub, _ := DecodePublicKey(peer.PublicKeyB64())
	sk := Precompute(stalePub, func() SecretKey { return SecretKey{} }())
	_ = sk
	oldCT, oldN, err := Encrypt(Precompute(stalePub, peerSecretOf(peer)), []byte("старе"))
	if err != nil {
		t.Fatal(err)
	}
	_ = peerPub
	if pt, err := dev.DecryptFrom(peerID, oldCT, oldN); err != nil || pt != "старе" {
		t.Fatalf("читання старого спадковим ключем: %q %v", pt, err)
	}
	// Користувач вирішив почати заново.
	if err := dev.ResetKeys(); err != nil {
		t.Fatal(err)
	}
	if dev.Status().State != KeysReady || len(ks.keys) != 1 {
		t.Fatalf("%+v keys=%d", dev.Status(), len(ks.keys))
	}
	// ResetKeys поза станом need_recovery — відмова (не можна випадково ротувати).
	if err := dev.ResetKeys(); !errors.Is(err, ErrBadInput) {
		t.Fatalf("%v", err)
	}
}

func peerSecretOf(c *ChatCrypto) SecretKey { c.mu.Lock(); defer c.mu.Unlock(); return c.keys[0].sec }

func TestNoPublishedKeyCreatesFirstOne(t *testing.T) {
	ks, w := newKS(), newKeyWorker()
	w.pubSink(ks)
	dev, _ := device(t, ks, w)
	if err := dev.Init(); err != nil || len(ks.keys) != 1 || dev.Status().State != KeysReady {
		t.Fatalf("перший ключ: %v keys=%d", err, len(ks.keys))
	}
}

func TestServerDisabledFallsBackToLocalKeyAsBefore(t *testing.T) {
	ks, w := newKS(), newKeyWorker()
	ks.mode = "disabled"
	dev, dir := device(t, ks, w)
	if err := dev.Init(); err != nil || dev.Status().Managed {
		t.Fatalf("%v", err)
	}
	if !fileExists(filepath.Join(dir, identityFileName)) {
		t.Fatal("локальний режим як раніше")
	}
	if err := dev.PublishOwnKey(); err != nil || len(w.posts) != 1 {
		t.Fatalf("публікація як раніше: %v %v", err, w.posts)
	}
}

func TestRevokedDeviceReportsClearly(t *testing.T) {
	ks, w := newKS(), newKeyWorker()
	ks.mode = "revoked"
	dev, _ := device(t, ks, w)
	if err := dev.Init(); !errors.Is(err, ErrDeviceRevoked) {
		t.Fatalf("%v", err)
	}
}

func TestCorruptFilesAreNeverReplaced(t *testing.T) {
	ks, w := newKS(), newKeyWorker()
	ks.seed(1)
	dev, dir := device(t, ks, w)
	_ = os.MkdirAll(dir, 0o700)
	bad := []byte("{ не json")
	_ = os.WriteFile(filepath.Join(dir, identityFileName), bad, 0o600)
	if err := dev.Init(); err == nil {
		t.Fatal("пошкоджений identity.json — помилка, а не новий ключ")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, identityFileName)); string(got) != string(bad) {
		t.Fatal("файл не можна чіпати")
	}
	if ks.puts != 0 {
		t.Fatal("на сервер нічого не йде")
	}
	// те саме для keyring.json
	_ = os.Remove(filepath.Join(dir, identityFileName))
	_ = os.WriteFile(filepath.Join(dir, keyringFileName), bad, 0o600)
	dev2 := NewChatCrypto(NewKeyStore(dir), NewClient(w.requester())).WithKeyServer(ksAdapter{ks}, func() string { return meID })
	if err := dev2.Init(); err == nil {
		t.Fatal("пошкоджена зв'язка — помилка")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, keyringFileName)); string(got) != string(bad) {
		t.Fatal("keyring.json не можна затирати")
	}
}

func TestInconsistentServerPairIsNotTrusted(t *testing.T) {
	ks, w := newKS(), newKeyWorker()
	pubA, _, _ := GenerateIdentity()
	_, secB, _ := GenerateIdentity()
	ks.keys = []RemoteKey{{Version: 1, PublicB64: EncodePublicKey(pubA), SecretB64: b64StdEncode(secB[:])}}
	dev, dir := device(t, ks, w)
	if err := dev.Init(); !errors.Is(err, ErrKeysPending) {
		t.Fatalf("суперечлива пара не усиновлюється: %v", err)
	}
	if fileExists(filepath.Join(dir, keyringFileName)) {
		t.Fatal("сміття не кешуємо")
	}
}

// Ротація: нова версія; старі повідомлення (шифровані v1) лишаються читабельними.
func TestRotationKeepsOldMessagesReadable(t *testing.T) {
	ks, w := newKS(), newKeyWorker()
	w.pubSink(ks)
	dev, dir := device(t, ks, w)
	if err := dev.Init(); err != nil {
		t.Fatal(err)
	}
	peer := peerCrypto(t, w)
	p1, _ := DecodePublicKey(dev.PublicKeyB64())
	w.publish(meID, p1)
	oldCT, oldN, _ := peer.encryptE2E(meID, "до ротації")

	if err := dev.Rotate(0); err != nil {
		t.Fatal(err)
	}
	if len(ks.keys) != 2 || dev.PublicKeyB64() == EncodePublicKey(p1) {
		t.Fatalf("v2 має стати поточною: keys=%d", len(ks.keys))
	}
	if pt, err := dev.DecryptFrom(peerID, oldCT, oldN); err != nil || pt != "до ротації" {
		t.Fatalf("історія після ротації: %q %v", pt, err)
	}
	// Нове повідомлення співрозмовника на новий ключ (він бере його з Worker).
	p2, _ := DecodePublicKey(dev.PublicKeyB64())
	w.publish(meID, p2)
	peer.Forget(meID)
	newCT, newN, _ := peer.encryptE2E(meID, "після ротації")
	if pt, err := dev.DecryptFrom(peerID, newCT, newN); err != nil || pt != "після ротації" {
		t.Fatalf("нове: %q %v", pt, err)
	}
	// Інший пристрій (чистий диск) бачить обидві версії.
	dev2, _ := device(t, ks, w)
	_ = dev2.Init()
	for _, c := range [][2]string{{oldCT, oldN}, {newCT, newN}} {
		if _, err := dev2.DecryptFrom(peerID, c[0], c[1]); err != nil {
			t.Fatalf("другий пристрій: %v", err)
		}
	}
	// Зв'язка на диску містить обидві версії + архів попереднього файлу.
	if baks, _ := filepath.Glob(filepath.Join(dir, keyringFileName+".bak-*")); len(baks) == 0 {
		t.Fatal("попередню зв'язку мало заархівовано")
	}
}

// Співрозмовник ротував ключ: старі повідомлення читаються за історією його публічних ключів.
func TestPeerRotationOldMessagesStillDecrypt(t *testing.T) {
	ks, w := newKS(), newKeyWorker()
	w.pubSink(ks)
	dev, _ := device(t, ks, w)
	_ = dev.Init()
	me1, _ := DecodePublicKey(dev.PublicKeyB64())
	w.publish(meID, me1)

	peer := peerCrypto(t, w)
	oldCT, oldN, _ := peer.encryptE2E(meID, "ще зі старим ключем співрозмовника")

	// співрозмовник створює нову пару й публікує її
	newPeer := NewChatCrypto(NewKeyStore(filepath.Join(t.TempDir(), "peer2")), NewClient(w.requester()))
	_ = newPeer.Init()
	np, _ := DecodePublicKey(newPeer.PublicKeyB64())
	w.publish(peerID, np)
	dev.Forget(peerID)

	if pt, err := dev.DecryptFrom(peerID, oldCT, oldN); err != nil || !strings.Contains(pt, "старим ключем") {
		t.Fatalf("старе повідомлення при новому ключі співрозмовника: %q %v", pt, err)
	}
	// Нове — звичайним шляхом.
	ct, n, _ := newPeer.encryptE2E(meID, "нове")
	if pt, err := dev.DecryptFrom(peerID, ct, n); err != nil || pt != "нове" {
		t.Fatalf("%q %v", pt, err)
	}
}

// Ротацію зробив інший пристрій: невдале розшифрування тягне нову версію.
func TestUndecryptableTriggersResyncOnRotationElsewhere(t *testing.T) {
	ks, w := newKS(), newKeyWorker()
	w.pubSink(ks)
	dev, _ := device(t, ks, w)
	_ = dev.Init()
	peer := peerCrypto(t, w)
	p1, _ := DecodePublicKey(dev.PublicKeyB64())
	w.publish(meID, p1)

	// Інший пристрій створює v2.
	other, _ := device(t, ks, w)
	_ = other.Init()
	if err := other.Rotate(0); err != nil {
		t.Fatal(err)
	}
	p2, _ := DecodePublicKey(other.PublicKeyB64())
	w.publish(meID, p2)
	peer.Forget(meID)
	ct, n, _ := peer.encryptE2E(meID, "шифровано на v2")

	dev.Forget(peerID)
	if _, err := dev.DecryptFrom(peerID, ct, n); !errors.Is(err, ErrCryptoDecryptFail) {
		t.Fatalf("v2 цьому пристрою ще невідома: %v", err)
	}
	// Wait for the actual background resync; its duration depends on disk/crypto speed.
	deadline := time.Now().Add(5 * time.Second)
	for dev.PublicKeyB64() != other.PublicKeyB64() {
		if time.Now().After(deadline) {
			t.Fatal("фоновий Resync не завантажив новий ключ")
		}
		time.Sleep(10 * time.Millisecond)
	}
	dev.Forget(peerID)
	if pt, err := dev.DecryptFrom(peerID, ct, n); err != nil || pt != "шифровано на v2" {
		t.Fatalf("після Resync: %q %v", pt, err)
	}
}

func TestKeyringArchivesAndPrunesBackups(t *testing.T) {
	dir := t.TempDir()
	k := NewKeyring(dir)
	for i := 0; i < 9; i++ {
		pub, sec, _ := GenerateIdentity()
		f := &keyringFile{Current: 1, Entries: []keyringEntry{{Version: 1, Kind: kindServer, PublicKey: EncodePublicKey(pub), SecretKey: b64StdEncode(sec[:])}}}
		if err := k.Save(f); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond)
	}
	baks, _ := filepath.Glob(filepath.Join(dir, keyringFileName+".bak-*"))
	if len(baks) != keepBackups {
		t.Fatalf("архівів %d, очікував %d", len(baks), keepBackups)
	}
	if st, err := os.Stat(filepath.Join(dir, keyringFileName)); err != nil || (runtime.GOOS != "windows" && st.Mode().Perm() != 0o600) {
		t.Fatalf("права: %v %v", err, st)
	}
}

func TestSelfEchoIsRoutedToCorrectPeer(t *testing.T) {
	// Ехо власного повідомлення: співрозмовник = інша сторона чату, лічильник непрочитаного не росте.
	ks, w := newKS(), newKeyWorker()
	w.pubSink(ks)
	var events []ChatEvent
	var mu sync.Mutex
	svc := NewService(Deps{
		Request: w.requester(), Token: func() (string, error) { return "", ErrNotLoggedIn },
		KeyDir: filepath.Join(t.TempDir(), "k"), KeyServer: ksAdapter{ks},
		Emit: func(ev string, p any) {
			if ce, ok := p.(ChatEvent); ok && ev == "chat:message" {
				mu.Lock()
				events = append(events, ce)
				mu.Unlock()
			}
		},
	})
	svc.mu.Lock()
	svc.st.Me = meID
	svc.mu.Unlock()
	if err := svc.crypto.Init(); err != nil {
		t.Fatal(err)
	}
	peer := peerCrypto(t, w)
	mp, _ := DecodePublicKey(svc.crypto.PublicKeyB64())
	w.publish(meID, mp)
	// Я (з іншого пристрою) пишу другові: шифруємо на ключ друга спільним секретом.
	ct, n, err := svc.crypto.encryptE2E(peerID, "написав із ноутбука")
	if err != nil {
		t.Fatal(err)
	}
	_ = peer
	chat := meID + "_" + peerID
	svc.onIncomingMessage(IncomingMessage{ChatID: chat, From: meID, BodyType: "text", Ciphertext: ct, Nonce: n, TS: 1, ID: 7})
	mu.Lock()
	defer mu.Unlock()
	if len(events) != 1 || !events[0].Mine || events[0].Plaintext != "написав із ноутбука" || events[0].DecryptFailed {
		t.Fatalf("%+v", events)
	}
	if svc.st.Unread[peerID] != 0 || svc.st.Unread[meID] != 0 {
		t.Fatalf("власне повідомлення не має збільшувати непрочитане: %v", svc.st.Unread)
	}
}

// Співрозмовник змінив ключ, а наш процес довго живе: кеш не має вічно слати на старий ключ.
func TestStalePeerKeyCacheDoesNotEncryptToOldKey(t *testing.T) {
	ks, w := newKS(), newKeyWorker()
	w.pubSink(ks)
	dev, _ := device(t, ks, w)
	_ = dev.Init()
	me, _ := DecodePublicKey(dev.PublicKeyB64())
	w.publish(meID, me)

	oldPeer := peerCrypto(t, w)
	_ = oldPeer
	if _, _, err := dev.encryptE2E(peerID, "перше"); err != nil { // кешує ключ друга
		t.Fatal(err)
	}
	newPeer := NewChatCrypto(NewKeyStore(filepath.Join(t.TempDir(), "peer-new")), NewClient(w.requester()))
	_ = newPeer.Init()
	np, _ := DecodePublicKey(newPeer.PublicKeyB64())
	w.publish(peerID, np) // без Forget — як у реальному житті

	dev.mu.Lock()
	dev.peerAt[peerID] = time.Now().Add(-time.Minute) // кеш прострочився
	dev.mu.Unlock()
	ct, n, err := dev.encryptE2E(peerID, "друге")
	if err != nil {
		t.Fatal(err)
	}
	if pt, err := newPeer.DecryptFrom(meID, ct, n); err != nil || pt != "друге" {
		t.Fatalf("новий пристрій друга мусить прочитати: %q %v", pt, err)
	}
}

// Отримувач із застарілим кешем ключа відправника все одно читає свіже повідомлення.
func TestReceiverRefreshesStalePeerKeyOnFailure(t *testing.T) {
	ks, w := newKS(), newKeyWorker()
	w.pubSink(ks)
	dev, _ := device(t, ks, w)
	_ = dev.Init()
	me, _ := DecodePublicKey(dev.PublicKeyB64())
	w.publish(meID, me)

	peer := peerCrypto(t, w)
	ct0, n0, _ := peer.encryptE2E(meID, "до зміни")
	if _, err := dev.DecryptFrom(peerID, ct0, n0); err != nil { // кешує ключ відправника
		t.Fatal(err)
	}
	newPeer := NewChatCrypto(NewKeyStore(filepath.Join(t.TempDir(), "peer-new")), NewClient(w.requester()))
	_ = newPeer.Init()
	np, _ := DecodePublicKey(newPeer.PublicKeyB64())
	w.publish(peerID, np)
	ct, n, _ := newPeer.encryptE2E(meID, "після зміни")
	if pt, err := dev.DecryptFrom(peerID, ct, n); err != nil || pt != "після зміни" {
		t.Fatalf("%q %v", pt, err)
	}
}

// Сервер ключів вимкнено, а друзям уже опубліковано чужий ключ: новий пристрій його НЕ затирає.
func TestServerDisabledNewDeviceDoesNotOverwritePublishedKey(t *testing.T) {
	ks, w := newKS(), newKeyWorker()
	ks.mode = "disabled"
	w.publish(meID, PublicKey{1, 2, 3})
	dev, dir := device(t, ks, w)
	if err := dev.Init(); err != nil {
		t.Fatal(err)
	}
	if dev.Status().State != KeysNeedRecovery || fileExists(filepath.Join(dir, identityFileName)) {
		t.Fatalf("очікував need_recovery без нового ключа: %+v", dev.Status())
	}
	if err := dev.PublishOwnKey(); err != nil || len(w.posts) != 0 {
		t.Fatalf("нічого не публікуємо: %v %v", err, w.posts)
	}
	if _, _, err := dev.encryptE2E(peerID, "x"); !errors.Is(err, ErrKeysNeedRecovery) {
		t.Fatalf("%v", err)
	}
}

// ── «простий» режим: ключі пристроїв не потрібні ──────────────────────────

// Будь-який пристрій (навіть без жодного ключа й без сервера ключів) читає повідомлення — і нові, і
// надіслані з іншого пристрою. Саме це ламалось: «зашифровано старим ключем».
func TestPlainModeReadableOnAnyDeviceWithoutKeys(t *testing.T) {
	w := newKeyWorker()
	sender := NewChatCrypto(NewKeyStore(filepath.Join(t.TempDir(), "a")), NewClient(w.requester()))
	fresh := NewChatCrypto(NewKeyStore(filepath.Join(t.TempDir(), "b")), NewClient(w.requester())) // «новий ПК»
	ct, n, err := sender.EncryptFor(peerID, "привіт 🔐 з будь-якого пристрою")
	if err != nil {
		t.Fatal(err)
	}
	if n != plainNonce || len(n) < 24 || len(n) > 40 || len(ct) > maxCiphertextB64Len {
		t.Fatalf("формат має проходити перевірки Worker: nonce=%q len(ct)=%d", n, len(ct))
	}
	got, err := fresh.DecryptFrom(meID, ct, n)
	if err != nil || got != "привіт 🔐 з будь-якого пристрою" {
		t.Fatalf("%q %v", got, err)
	}
	if fresh.loaded {
		t.Fatal("плейн-режим не має створювати ключі")
	}
}

func TestPlainModeLimitsAndBadInput(t *testing.T) {
	c := NewChatCrypto(NewKeyStore(filepath.Join(t.TempDir(), "k")), nil)
	if _, _, err := c.EncryptFor(peerID, "   "); !errors.Is(err, ErrBadInput) {
		t.Fatalf("порожнє: %v", err)
	}
	if _, _, err := c.EncryptFor("не-id!", "x"); !errors.Is(err, ErrBadInput) {
		t.Fatalf("поганий id: %v", err)
	}
	if _, _, err := c.EncryptFor(peerID, strings.Repeat("я", MaxPlaintextBytes)); err == nil {
		t.Fatal("надто довге мало б відхилитись")
	}
	big := strings.Repeat("a", MaxPlaintextBytes)
	ct, n, err := c.EncryptFor(peerID, big)
	if err != nil || len(ct) > maxCiphertextB64Len {
		t.Fatalf("максимум має влізати в ліміт Worker: %v len=%d", err, len(ct))
	}
	if got, err := c.DecryptFrom(peerID, ct, n); err != nil || got != big {
		t.Fatalf("roundtrip: %v", err)
	}
	if _, err := c.DecryptFrom(peerID, "!!!не-base64", plainNonce); !errors.Is(err, ErrCryptoDecryptFail) {
		t.Fatalf("сміття: %v", err)
	}
}

// Старі E2E-повідомлення, що вже лежать у D1, і далі читаються тим, чим могли (поряд із новими).
func TestLegacyE2EMessagesStillReadableAlongsidePlain(t *testing.T) {
	ks, w := newKS(), newKeyWorker()
	w.pubSink(ks)
	dev, _ := device(t, ks, w)
	_ = dev.Init()
	me, _ := DecodePublicKey(dev.PublicKeyB64())
	w.publish(meID, me)
	peer := peerCrypto(t, w)
	oldCT, oldN, err := peer.encryptE2E(meID, "старе E2E")
	if err != nil {
		t.Fatal(err)
	}
	if pt, err := dev.DecryptFrom(peerID, oldCT, oldN); err != nil || pt != "старе E2E" {
		t.Fatalf("%q %v", pt, err)
	}
	ct, n, _ := peer.EncryptFor(meID, "нове плейн")
	if pt, err := dev.DecryptFrom(peerID, ct, n); err != nil || pt != "нове плейн" {
		t.Fatalf("%q %v", pt, err)
	}
}
