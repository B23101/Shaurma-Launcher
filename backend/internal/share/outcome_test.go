package share

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestValidateRelPathWindowsNames(t *testing.T) {
	bad := []string{"con", "CON.txt", "mods/nul.jar", "a/Com1.log", "lpt9", "a<b", "a>b", "a|b", "a?b", "a*b", "a\"b", "a\x01b", "x/aux .txt"}
	for _, p := range bad {
		if err := ValidateRelPath(p); err == nil {
			t.Errorf("%q мав би відхилятись", p)
		}
	}
	good := []string{"config/console.toml", "mods/conquest.jar", "auxiliary/a.txt", "com10.txt", "saves/World 1/level.dat"}
	for _, p := range good {
		if err := ValidateRelPath(p); err != nil {
			t.Errorf("%q мав би проходити: %v", p, err)
		}
	}
}

func stateOf(h *Host, id string) RecipientState {
	for _, s := range h.Snapshot() {
		if s.ID == id {
			return s
		}
	}
	return RecipientState{}
}

func TestRevokeGivesSenderCancelledReason(t *testing.T) {
	root := makeTree(t, 1000)
	m := buildM(t, root)
	r := startHost(t, root, m, false)
	k, _ := r.host.AddRecipient("a")
	r.host.RevokeAll(ReasonCancelledSender)
	err := NewDownloader(Options{Dial: r.dial(false), Key: k, StageDir: t.TempDir()}).Run(context.Background())
	if !errors.Is(err, ErrAccessClosed) || ReasonOf(err) != ReasonCancelledSender {
		t.Fatalf("err=%v reason=%q", err, ReasonOf(err))
	}
	if s := stateOf(r.host, "a"); s.State != StateRevoked || s.Reason != ReasonCancelledSender {
		t.Fatalf("стан: %+v", s)
	}
}

func TestDeclineIsFinalForLaterAttempts(t *testing.T) {
	root := makeTree(t, 1000)
	m := buildM(t, root)
	r := startHost(t, root, m, false)
	k, _ := r.host.AddRecipient("a")
	if err := NewDownloader(Options{Dial: r.dial(false), Key: k, StageDir: t.TempDir()}).Decline(context.Background()); err != nil {
		t.Fatal(err)
	}
	err := NewDownloader(Options{Dial: r.dial(false), Key: k, StageDir: t.TempDir()}).Run(context.Background())
	if ReasonOf(err) != ReasonDeclined {
		t.Fatalf("після відмови: %v (%s)", err, ReasonOf(err))
	}
}

func TestReceiverCancelReportedAndResumable(t *testing.T) {
	root := makeTree(t, 6<<20)
	m := buildM(t, root)
	r := startHost(t, root, m, false)
	k, _ := r.host.AddRecipient("a")
	stage := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	d := NewDownloader(Options{Dial: slowDial(r.dial(false)), Key: k, StageDir: stage})
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for stateOf(r.host, "a").State != StateTransferring {
		if time.Now().After(deadline) {
			t.Fatal("одержувач так і не почав")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Run: %v", err)
	}
	d.ReportCancelled(context.Background())
	if s := stateOf(r.host, "a"); s.State != StateCancelled || s.Reason != ReasonCancelledReceiver {
		t.Fatalf("стан після зупинки: %+v", s)
	}
	if r.host.AllSettled() != true {
		t.Fatal("зупинка одержувачем — фінальний стан")
	}
	// Передумав — продовжує з того самого stage.
	if err := NewDownloader(Options{Dial: r.dial(false), Key: k, StageDir: stage}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s := stateOf(r.host, "a"); s.State != StateDone {
		t.Fatalf("після продовження: %+v", s)
	}
	compareTrees(t, m, root, FilesDir(stage))
}

// slowDial — з'єднання, що читає повільно (щоб встигнути скасувати посеред передачі).
func slowDial(dial func(ctx context.Context) (net.Conn, error)) func(ctx context.Context) (net.Conn, error) {
	return func(ctx context.Context) (net.Conn, error) {
		c, err := dial(ctx)
		if err != nil {
			return nil, err
		}
		return &slowConn{Conn: c, read: new(atomic.Int64)}, nil
	}
}

func TestSweepTimeoutsAndOffline(t *testing.T) {
	root := makeTree(t, 1000)
	m := buildM(t, root)
	r := startHost(t, root, m, false)
	r.host.SetTimeouts(Timeouts{Accept: time.Minute, OfflineAfter: 20 * time.Second, Offline: time.Minute})
	kA, _ := r.host.AddRecipient("never")
	kB, _ := r.host.AddRecipient("gone")
	_ = kA

	dB := NewDownloader(Options{Dial: r.dial(false), Key: kB, StageDir: t.TempDir()})
	if _, err := dB.Manifest(context.Background()); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	r.host.Sweep(now.Add(30 * time.Second))
	if s := stateOf(r.host, "gone"); !s.Offline || s.State != StateTransferring {
		t.Fatalf("мав би бути офлайн: %+v", s)
	}
	if s := stateOf(r.host, "never"); s.State != StateWaiting {
		t.Fatalf("ще чекаємо: %+v", s)
	}
	r.host.Sweep(now.Add(2 * time.Minute))
	if s := stateOf(r.host, "never"); s.State != StateTimeout || s.Reason != ReasonTimeout {
		t.Fatalf("не прийняв вчасно: %+v", s)
	}
	if s := stateOf(r.host, "gone"); s.State != StateTimeout || s.Reason != ReasonReceiverOffline {
		t.Fatalf("зник: %+v", s)
	}
	if !r.host.AllSettled() {
		t.Fatal("усі тайм-аути — фінальні")
	}
	err := NewDownloader(Options{Dial: r.dial(false), Key: kB, StageDir: t.TempDir()}).Run(context.Background())
	if ReasonOf(err) != ReasonReceiverOffline {
		t.Fatalf("повернувся запізно: %v (%s)", err, ReasonOf(err))
	}
}

func TestOfflineFlagClearsOnReturn(t *testing.T) {
	root := makeTree(t, 1000)
	m := buildM(t, root)
	r := startHost(t, root, m, false)
	r.host.SetTimeouts(Timeouts{OfflineAfter: time.Second, Offline: time.Hour})
	k, _ := r.host.AddRecipient("a")
	d := NewDownloader(Options{Dial: r.dial(false), Key: k, StageDir: t.TempDir()})
	if _, err := d.Manifest(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.host.Sweep(time.Now().Add(5 * time.Second))
	if !stateOf(r.host, "a").Offline {
		t.Fatal("має бути офлайн")
	}
	if err := d.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s := stateOf(r.host, "a"); s.Offline || s.State != StateDone {
		t.Fatalf("повернувся й завершив: %+v", s)
	}
}

func TestAddRecipientNotifiesAndKeepsKey(t *testing.T) {
	root := makeTree(t, 1000)
	m := buildM(t, root)
	var got []RecipientState
	h, err := NewHost(root, m, func(s RecipientState) { got = append(got, s) })
	if err != nil {
		t.Fatal(err)
	}
	k1, _ := h.AddRecipient("a")
	if len(got) != 1 || got[0].ID != "a" || got[0].State != StateWaiting {
		t.Fatalf("новий одержувач має одразу з'явитись у UI: %+v", got)
	}
	k2, _ := h.AddRecipient("a")
	if k1 != k2 || len(got) != 1 {
		t.Fatal("повторне запрошення — той самий ключ і без зайвих подій")
	}
	h.AddRecipient("b")
	h.AddRecipient("c")
	if _, err := h.AddRecipient("d"); !errors.Is(err, ErrTooManyRecipients) {
		t.Fatalf("ліміт одержувачів: %v", err)
	}
}

func TestApplyControl(t *testing.T) {
	root := makeTree(t, 1000)
	m := buildM(t, root)
	r := startHost(t, root, m, false)
	r.host.AddRecipient("a")
	r.host.AddRecipient("b")
	r.host.AddRecipient("c")
	if !r.host.ApplyControl("a", CtlDeclined, "") || stateOf(r.host, "a").State != StateDeclined {
		t.Fatal("declined")
	}
	if !r.host.ApplyControl("b", CtlFailed, ReasonDiskFull) || stateOf(r.host, "b").Reason != ReasonDiskFull {
		t.Fatalf("failed: %+v", stateOf(r.host, "b"))
	}
	if !r.host.ApplyControl("c", CtlFailed, "щось нове") || stateOf(r.host, "c").Reason != ReasonError {
		t.Fatalf("невідома причина → error: %+v", stateOf(r.host, "c"))
	}
	if r.host.ApplyControl("c", CtlCancelled, "") {
		t.Fatal("фінальний стан не має перезаписуватись")
	}
	if r.host.ApplyControl("zzz", CtlDeclined, "") {
		t.Fatal("чужий id")
	}
}

func TestDiskFullPrecheck(t *testing.T) {
	root := makeTree(t, 1000)
	m := buildM(t, root)
	r := startHost(t, root, m, false)
	k, _ := r.host.AddRecipient("a")
	var last Progress
	var mu sync.Mutex
	d := NewDownloader(Options{Dial: r.dial(false), Key: k, StageDir: t.TempDir(),
		FreeSpace:  func(string) int64 { return 1 << 20 },
		OnProgress: func(p Progress) { mu.Lock(); last = p; mu.Unlock() }})
	err := d.Run(context.Background())
	if !errors.Is(err, ErrDiskFull) || ReasonOf(err) != ReasonDiskFull {
		t.Fatalf("err=%v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if last.State != ProgError || last.Reason != ReasonDiskFull {
		t.Fatalf("UI-прогрес: %+v", last)
	}
	if s := stateOf(r.host, "a"); s.State != StateFailed || s.Reason != ReasonDiskFull {
		t.Fatalf("відправник має знати причину: %+v", s)
	}
}

func TestCardSizeMismatchRejected(t *testing.T) {
	root := makeTree(t, 1000)
	m := buildM(t, root)
	r := startHost(t, root, m, false)
	k, _ := r.host.AddRecipient("a")
	err := NewDownloader(Options{Dial: r.dial(false), Key: k, StageDir: t.TempDir(), ExpectBytes: m.TotalBytes - 1}).Run(context.Background())
	if ReasonOf(err) != ReasonChanged {
		t.Fatalf("err=%v", err)
	}
}

func TestSenderGoneFailsFast(t *testing.T) {
	dead := func(ctx context.Context) (net.Conn, error) { return nil, errors.New("connection refused") }
	d := NewDownloader(Options{Dial: dead, Key: strings.Repeat("a", 32), StageDir: t.TempDir(),
		RetryBase: 5 * time.Millisecond, StallTimeout: time.Hour,
		SenderGone: func() bool { return true }, SenderGoneGrace: 50 * time.Millisecond})
	start := time.Now()
	err := d.Run(context.Background())
	if ReasonOf(err) != ReasonSenderOffline {
		t.Fatalf("err=%v (%s)", err, ReasonOf(err))
	}
	if time.Since(start) > 10*time.Second {
		t.Fatal("мало здатись швидко")
	}
}

func TestReconnectingFlagDuringOutage(t *testing.T) {
	root := makeTree(t, 6<<20)
	m := buildM(t, root)
	r := startHost(t, root, m, false)
	k, _ := r.host.AddRecipient("a")
	var broken sync.Map
	dial := func(ctx context.Context) (net.Conn, error) {
		if _, ok := broken.Load("x"); ok {
			return nil, errors.New("refused")
		}
		return slowDial(r.dial(false))(ctx)
	}
	var mu sync.Mutex
	saw := false
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d := NewDownloader(Options{Dial: dial, Key: k, StageDir: t.TempDir(), RetryBase: 5 * time.Millisecond,
		OnProgress: func(p Progress) {
			mu.Lock()
			saw = saw || p.Reconnecting
			mu.Unlock()
		}})
	if _, err := d.Manifest(ctx); err != nil {
		t.Fatal(err)
	}
	broken.Store("x", true)
	d.client.CloseIdleConnections()
	go func() { _ = d.Run(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		mu.Lock()
		ok := saw
		mu.Unlock()
		if ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("UI не дізнався про обрив зв'язку")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestResumeRehashesCorruptedFile(t *testing.T) {
	root := makeTree(t, 1000)
	m := buildM(t, root)
	r := startHost(t, root, m, false)
	k, _ := r.host.AddRecipient("a")
	stage := t.TempDir()
	if err := NewDownloader(Options{Dial: r.dial(false), Key: k, StageDir: stage}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	// «Вимкнули живлення»: розмір правильний, вміст — нулі. Журнал каже «прийнято».
	var victim FileEntry
	for _, f := range m.Files {
		if f.Size > 0 {
			victim = f
			break
		}
	}
	if err := os.WriteFile(filepath.Join(FilesDir(stage), filepath.FromSlash(victim.Path)), make([]byte, victim.Size), 0o644); err != nil {
		t.Fatal(err)
	}
	r2 := startHost(t, root, m, false)
	k2, _ := r2.host.AddRecipient("a")
	if err := NewDownloader(Options{Dial: r2.dial(false), Key: k2, StageDir: stage}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	compareTrees(t, m, root, FilesDir(stage))
}

func TestIntegrityFailureReason(t *testing.T) {
	root := t.TempDir()
	writeRand(t, filepath.Join(root, "a.bin"), 4000)
	m, _, err := BuildManifest(context.Background(), root, KindBuild, "x", nil, BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// Файл змінився тихо (той самий розмір) — відправник віддає не те, що в маніфесті.
	writeRand(t, filepath.Join(root, "a.bin"), 4000)
	r := startHost(t, root, m, false)
	k, _ := r.host.AddRecipient("a")
	err = NewDownloader(Options{Dial: r.dial(false), Key: k, StageDir: t.TempDir(), RetryBase: time.Millisecond}).Run(context.Background())
	if ReasonOf(err) != ReasonIntegrity {
		t.Fatalf("err=%v (%s)", err, ReasonOf(err))
	}
	if s := stateOf(r.host, "a"); s.State != StateFailed || s.Reason != ReasonIntegrity {
		t.Fatalf("відправник: %+v", s)
	}
}

func TestControlRoundTrip(t *testing.T) {
	sid := strings.Repeat("ab", 16)
	txt := EncodeControl(Control{SID: sid, Event: CtlFailed, Reason: ReasonDiskFull, Name: "Збірка"}, "Не вдалося прийняти")
	c, ok := ParseControl(txt)
	if !ok || c.SID != sid || c.Event != CtlFailed || c.Reason != ReasonDiskFull || c.Name != "Збірка" {
		t.Fatalf("%+v %v", c, ok)
	}
	if !strings.HasPrefix(txt, "Не вдалося прийняти\n") {
		t.Fatalf("людський рядок: %q", txt)
	}
	for _, bad := range []string{
		"просто текст",
		ControlPrefix + "!!!",
		EncodeControl(Control{SID: "short", Event: CtlDeclined}, ""),
		EncodeControl(Control{SID: sid, Event: "explode"}, ""),
	} {
		if _, ok := ParseControl(bad); ok {
			t.Errorf("мало відхилитись: %q", bad)
		}
	}
	c, ok = ParseControl(EncodeControl(Control{SID: sid, Event: CtlCancelled, Reason: "???"}, ""))
	if !ok || c.Reason != ReasonError {
		t.Fatalf("невідома причина: %+v", c)
	}
}

func TestHistoryPersistsAndPrunes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "h.json")
	h := NewHistory(path, 3)
	type v struct{ N int }
	for i := 0; i < 5; i++ {
		if err := h.Put("in", strings.Repeat(string(rune('a'+i)), 4), v{i}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond)
	}
	_ = h.Put("out", "x", v{9})
	h2 := NewHistory(path, 3)
	if got := len(h2.List("in")); got != 3 {
		t.Fatalf("лишилось %d, очікували 3", got)
	}
	var x v
	if !h2.Get("in", "eeee", &x) || x.N != 4 {
		t.Fatalf("найновіший запис: %+v", x)
	}
	if h2.Get("in", "aaaa", &x) {
		t.Fatal("найстаріший мав зникнути")
	}
	if !h2.Get("out", "x", &x) || x.N != 9 {
		t.Fatal("інший вид не чіпаємо")
	}
}

func TestRecoverInstall(t *testing.T) {
	mk := func() (stage, dst string) {
		stage = t.TempDir()
		writeRand(t, filepath.Join(FilesDir(stage), "mods", "a.jar"), 100)
		dst = filepath.Join(t.TempDir(), "packs", "p", ".minecraft")
		return
	}
	// 1. Перенесли, але не зареєстрували → файли повертаються в stage.
	stage, dst := mk()
	if err := BeginInstall(stage, InstallMarker{Dst: dst, ResultID: "p"}); err != nil {
		t.Fatal(err)
	}
	if err := MoveTree(FilesDir(stage), dst); err != nil {
		t.Fatal(err)
	}
	_, done, err := RecoverInstall(stage, func(InstallMarker) bool { return false })
	if err != nil || done {
		t.Fatalf("done=%v err=%v", done, err)
	}
	if _, err := os.Stat(filepath.Join(FilesDir(stage), "mods", "a.jar")); err != nil {
		t.Fatal("файли мали повернутись у stage")
	}
	if _, err := os.Stat(dst); err == nil {
		t.Fatal("тека призначення мала зникнути")
	}
	if _, ok := ReadInstall(stage); ok {
		t.Fatal("маркер мав зникнути")
	}

	// 2. Реєстрація встигла, маркер — ні → вважаємо встановленим.
	stage, dst = mk()
	_ = BeginInstall(stage, InstallMarker{Dst: dst, ResultID: "p"})
	_ = MoveTree(FilesDir(stage), dst)
	m, done, err := RecoverInstall(stage, func(m InstallMarker) bool { return m.ResultID == "p" })
	if err != nil || !done || m.ResultID != "p" {
		t.Fatalf("done=%v err=%v", done, err)
	}

	// 3. Обірване копіювання: оригінали в stage, у призначенні уламок → уламок видаляється.
	stage, dst = mk()
	_ = BeginInstall(stage, InstallMarker{Dst: dst})
	writeRand(t, filepath.Join(dst, "mods", "half.jar"), 10)
	if _, done, err := RecoverInstall(stage, nil); err != nil || done {
		t.Fatalf("done=%v err=%v", done, err)
	}
	if _, err := os.Stat(dst); err == nil {
		t.Fatal("уламок мав зникнути")
	}

	// 4. Явно позначено встановленим.
	stage, dst = mk()
	_ = MarkInstalled(stage, InstallMarker{Dst: dst, ResultID: "q"})
	if _, done, _ := RecoverInstall(stage, nil); !done {
		t.Fatal("registered=true → done")
	}
	if err := BeginInstall(stage, InstallMarker{Dst: "relative/path"}); err == nil {
		t.Fatal("відносний шлях призначення не дозволено")
	}
}
