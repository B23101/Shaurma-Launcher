package share

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func writeRand(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, n)
	_, _ = rand.Read(b)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// makeTree — невелике «збірка»: дрібні файли в теках, один великий, порожній, і ті, що мають пропускатись.
func makeTree(t *testing.T, bigSize int) string {
	t.Helper()
	root := t.TempDir()
	for i := 0; i < 40; i++ {
		n, _ := rand.Int(rand.Reader, big.NewInt(30000))
		writeRand(t, filepath.Join(root, "mods", fmt.Sprintf("mod%02d.jar", i)), int(n.Int64())+1)
	}
	writeRand(t, filepath.Join(root, "config", "a", "b", "c.toml"), 700)
	writeRand(t, filepath.Join(root, "saves", "w", "region", "r.0.0.mca"), bigSize)
	if err := os.WriteFile(filepath.Join(root, "empty.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	writeRand(t, filepath.Join(root, "evil.exe"), 100)           // має бути пропущено
	writeRand(t, filepath.Join(root, "logs", "latest.log"), 100) // пропускаємо через Include
	return root
}

func buildM(t *testing.T, root string) *Manifest {
	t.Helper()
	m, skipped, err := BuildManifest(context.Background(), root, KindBuild, "Тест", map[string]string{"mc": "1.20.1"}, BuildOptions{
		Include: func(rel string, isDir bool) bool { return !(isDir && rel == "logs") },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(skipped) != 1 || skipped[0] != "evil.exe" {
		t.Fatalf("skipped = %v, очікували [evil.exe]", skipped)
	}
	return m
}

type rig struct {
	host *Host
	addr string
	m    *Manifest
	root string
}

func startHost(t *testing.T, root string, m *Manifest, handshake bool) *rig {
	t.Helper()
	h, err := NewHost(root, m, nil)
	if err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go h.Serve(l, handshake)
	t.Cleanup(h.Close)
	return &rig{host: h, addr: l.Addr().String(), m: m, root: root}
}

func (r *rig) dial(handshake bool) func(ctx context.Context) (net.Conn, error) {
	return func(ctx context.Context) (net.Conn, error) {
		if handshake {
			return DialMC(ctx, r.addr, "abc-123456.play.example.org", 1)
		}
		var d net.Dialer
		return d.DialContext(ctx, "tcp", r.addr)
	}
}

func compareTrees(t *testing.T, m *Manifest, srcRoot, got string) {
	t.Helper()
	for _, f := range m.Files {
		a, err := os.ReadFile(filepath.Join(srcRoot, filepath.FromSlash(f.Path)))
		if err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(got, filepath.FromSlash(f.Path)))
		if err != nil {
			t.Fatalf("немає %s: %v", f.Path, err)
		}
		if !bytes.Equal(a, b) {
			t.Fatalf("вміст %s відрізняється", f.Path)
		}
	}
	// у прийнятій теці не має бути зайвого (.part, evil.exe, logs)
	filepath.WalkDir(got, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(got, p)
			rel = filepath.ToSlash(rel)
			found := false
			for _, f := range m.Files {
				if f.Path == rel {
					found = true
				}
			}
			if !found {
				t.Errorf("зайвий файл у прийнятій теці: %s", rel)
			}
		}
		return nil
	})
}

func TestEndToEnd(t *testing.T) {
	root := makeTree(t, 9<<20+12345) // великий файл понад BigFileThreshold
	m := buildM(t, root)
	r := startHost(t, root, m, true)
	key, err := r.host.AddRecipient("friend1")
	if err != nil {
		t.Fatal(err)
	}
	stage := t.TempDir()
	var last Progress
	d := NewDownloader(Options{Dial: r.dial(true), Key: key, StageDir: stage, ExpectManifestID: m.ID(),
		OnProgress: func(p Progress) { last = p }})
	if err := d.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	compareTrees(t, m, root, FilesDir(stage))
	if last.State != ProgDone || last.BytesDone != m.TotalBytes {
		t.Fatalf("прогрес наприкінці: %+v (total %d)", last, m.TotalBytes)
	}
	st := r.host.Snapshot()[0]
	if st.State != StateDone || st.BytesDone != m.TotalBytes || st.FilesDone != len(m.Files) {
		t.Fatalf("стан відправника: %+v", st)
	}
	if info, ok := ReadStage(stage); !ok || !info.Complete || info.FilesDone != len(m.Files) {
		t.Fatalf("журнал: %+v ok=%v", info, ok)
	}
	if !r.host.AllSettled() {
		t.Fatal("AllSettled має бути true")
	}
}

// cutConn обриває з'єднання після limit прочитаних байтів і скасовує контекст.
type cutConn struct {
	net.Conn
	read   *atomic.Int64
	limit  int64
	cancel context.CancelFunc
}

func (c *cutConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if c.read.Add(int64(n)) > c.limit {
		c.cancel()
		c.Conn.Close()
		return n, io.ErrClosedPipe
	}
	return n, err
}

func TestResumeAfterCancel(t *testing.T) {
	root := t.TempDir()
	writeRand(t, filepath.Join(root, "world", "big.bin"), 30<<20)
	writeRand(t, filepath.Join(root, "small.txt"), 500)
	m, _, err := BuildManifest(context.Background(), root, KindBuild, "R", nil, BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	r := startHost(t, root, m, true)
	key, _ := r.host.AddRecipient("f")
	stage := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	var cnt atomic.Int64
	d1 := NewDownloader(Options{
		Key: key, StageDir: stage, Parallel: 1, StallTimeout: time.Second, RetryBase: 10 * time.Millisecond,
		Dial: func(c context.Context) (net.Conn, error) {
			conn, err := r.dial(true)(c)
			if err != nil {
				return nil, err
			}
			return &cutConn{Conn: conn, read: &cnt, limit: 15 << 20, cancel: cancel}, nil
		},
	})
	if err := d1.Run(ctx); err == nil {
		t.Fatal("перший прогін мав перерватись")
	}
	part := filepath.Join(FilesDir(stage), "world", "big.bin.part")
	st, err := os.Stat(part)
	if err != nil || st.Size() == 0 || st.Size() >= 30<<20 {
		t.Fatalf("очікували частково прийнятий файл, маємо %v err=%v", st, err)
	}
	partial := st.Size()

	// Друга спроба — новий Downloader, той самий stage: докачує з місця обриву.
	// Доказ докачування: з мережі прочитано лише залишок файлу, а не весь файл заново.
	var cnt2 atomic.Int64
	d2 := NewDownloader(Options{Key: key, StageDir: stage, ExpectManifestID: m.ID(),
		Dial: func(c context.Context) (net.Conn, error) {
			conn, err := r.dial(true)(c)
			if err != nil {
				return nil, err
			}
			return &slowConn{Conn: conn, read: &cnt2, noDelay: true}, nil
		}})
	if err := d2.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	compareTrees(t, m, root, FilesDir(stage))
	if _, err := os.Stat(part); !os.IsNotExist(err) {
		t.Fatal(".part має зникнути після завершення")
	}
	remaining := int64(30<<20) - partial
	if got := cnt2.Load(); got > remaining+(1<<20) {
		t.Fatalf("друга спроба прочитала %d Б, а лишалось ≈ %d Б — докачування не спрацювало (качало заново)", got, remaining)
	}
	t.Logf("частково прийнято %d Б; друга спроба прочитала %d Б із %d", partial, cnt2.Load(), 30<<20)
}

func TestUnsafeManifestsRejected(t *testing.T) {
	h := strings.Repeat("a", 64)
	bad := []string{"../evil", "a/../../b", "/abs", "C:/x", "a\\b", "x/./y", "con:ads", "a//b", "mods/x.exe", "dir/ ", "dir/name.", ""}
	for _, p := range bad {
		m := &Manifest{V: 1, Kind: KindBuild, Name: "x", Files: []FileEntry{{Path: p, Size: 1, SHA256: h}}, TotalBytes: 1}
		if err := m.Validate(); err == nil {
			t.Errorf("шлях %q мав бути відхилений", p)
		}
	}
	dup := &Manifest{V: 1, Kind: KindBuild, Name: "x", TotalBytes: 2, Files: []FileEntry{
		{Path: "a/B.jar", Size: 1, SHA256: h}, {Path: "a/b.jar", Size: 1, SHA256: h}}}
	if dup.Validate() == nil {
		t.Error("дубль за регістром має бути відхилений")
	}
	sum := &Manifest{V: 1, Kind: KindBuild, Name: "x", TotalBytes: 5, Files: []FileEntry{{Path: "a", Size: 1, SHA256: h}}}
	if sum.Validate() == nil {
		t.Error("неправильна сума має бути відхилена")
	}
	ok := &Manifest{V: 1, Kind: KindServer, Name: "x", TotalBytes: 1, Files: []FileEntry{{Path: "world/level.dat", Size: 1, SHA256: h}}}
	if err := ok.Validate(); err != nil {
		t.Errorf("нормальний маніфест відхилено: %v", err)
	}
}

func TestChangedContentDetected(t *testing.T) {
	root := t.TempDir()
	writeRand(t, filepath.Join(root, "a.bin"), 4000)
	m, _, err := BuildManifest(context.Background(), root, KindBuild, "x", nil, BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	r := startHost(t, root, m, false)
	// Файл підмінили після побудови маніфесту (той самий розмір, інші байти).
	writeRand(t, filepath.Join(root, "a.bin"), 4000)
	key, _ := r.host.AddRecipient("f")
	stage := t.TempDir()
	d := NewDownloader(Options{Dial: r.dial(false), Key: key, StageDir: stage, RetryBase: 5 * time.Millisecond})
	err = d.Run(context.Background())
	var hf hashFail
	if err == nil || !errors.As(err, &hf) {
		t.Fatalf("очікували помилку контрольної суми, маємо %v", err)
	}
	if _, serr := os.Stat(filepath.Join(FilesDir(stage), "a.bin")); !os.IsNotExist(serr) {
		t.Fatal("зіпсований файл не має потрапити у фінальну теку")
	}
}

func TestWrongKeyAndMismatch(t *testing.T) {
	root := makeTree(t, 1000)
	m := buildM(t, root)
	r := startHost(t, root, m, true)
	key, _ := r.host.AddRecipient("f")

	d := NewDownloader(Options{Dial: r.dial(true), Key: strings.Repeat("0", 32), StageDir: t.TempDir()})
	if err := d.Run(context.Background()); !errors.Is(err, ErrAccessClosed) {
		t.Fatalf("неправильний ключ: %v", err)
	}
	d = NewDownloader(Options{Dial: r.dial(true), Key: key, StageDir: t.TempDir(), ExpectManifestID: "deadbeef"})
	if err := d.Run(context.Background()); !errors.Is(err, ErrManifestMismatch) {
		t.Fatalf("інший ID вмісту: %v", err)
	}
}

func TestSenderPauseAndResume(t *testing.T) {
	root := makeTree(t, 2<<20)
	m := buildM(t, root)
	r := startHost(t, root, m, true)
	key, _ := r.host.AddRecipient("f")
	r.host.SetPaused("f", true)

	var mu sync.Mutex
	var last Progress
	d := NewDownloader(Options{Dial: r.dial(true), Key: key, StageDir: t.TempDir(), RetryBase: 10 * time.Millisecond,
		OnProgress: func(p Progress) { mu.Lock(); last = p; mu.Unlock() }})
	errc := make(chan error, 1)
	go func() { errc <- d.Run(context.Background()) }()

	time.Sleep(1500 * time.Millisecond)
	select {
	case err := <-errc:
		t.Fatalf("під час паузи відправника передача не мала завершитись: %v", err)
	default:
	}
	mu.Lock()
	if last.State != ProgPaused || last.PausedBy != "sender" {
		t.Fatalf("очікували паузу від відправника, маємо %+v", last)
	}
	mu.Unlock()
	if s := r.host.Snapshot()[0]; s.State != StatePaused || s.PausedBy != "sender" {
		t.Fatalf("стан у відправника: %+v", s)
	}
	r.host.SetPaused("f", false)
	select {
	case err := <-errc:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("після відновлення передача не завершилась")
	}
}

func TestReceiverPauseResume(t *testing.T) {
	root := t.TempDir()
	writeRand(t, filepath.Join(root, "big.bin"), 40<<20)
	m, _, _ := BuildManifest(context.Background(), root, KindBuild, "x", nil, BuildOptions{})
	r := startHost(t, root, m, true)
	key, _ := r.host.AddRecipient("f")

	var cnt atomic.Int64
	stage := t.TempDir()
	d := NewDownloader(Options{Key: key, StageDir: stage, RetryBase: 10 * time.Millisecond,
		Dial: func(c context.Context) (net.Conn, error) {
			conn, err := r.dial(true)(c)
			if err != nil {
				return nil, err
			}
			return &slowConn{Conn: conn, read: &cnt}, nil
		}})
	errc := make(chan error, 1)
	go func() { errc <- d.Run(context.Background()) }()
	for cnt.Load() < 2<<20 {
		time.Sleep(10 * time.Millisecond)
	}
	d.Pause()
	time.Sleep(300 * time.Millisecond)
	at := cnt.Load()
	time.Sleep(500 * time.Millisecond)
	if cnt.Load() != at {
		t.Fatalf("під час паузи одержувача байти мають перестати надходити: %d → %d", at, cnt.Load())
	}
	select {
	case err := <-errc:
		t.Fatalf("Run завершився під час паузи: %v", err)
	default:
	}
	d.Resume()
	select {
	case err := <-errc:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("після Resume передача не завершилась")
	}
	compareTrees(t, m, root, FilesDir(stage))
}

// slowConn гальмує читання, щоб тест встиг натиснути «пауза».
type slowConn struct {
	net.Conn
	read    *atomic.Int64
	noDelay bool
}

func (c *slowConn) Read(p []byte) (int, error) {
	if !c.noDelay {
		if len(p) > 64<<10 {
			p = p[:64<<10]
		}
		time.Sleep(2 * time.Millisecond)
	}
	n, err := c.Conn.Read(p)
	c.read.Add(int64(n))
	return n, err
}

func TestMultipleRecipientsAndLimit(t *testing.T) {
	root := makeTree(t, 3<<20)
	m := buildM(t, root)
	r := startHost(t, root, m, true)
	keys := map[string]string{}
	for _, id := range []string{"a", "b", "c"} {
		k, err := r.host.AddRecipient(id)
		if err != nil {
			t.Fatal(err)
		}
		keys[id] = k
	}
	if _, err := r.host.AddRecipient("d"); !errors.Is(err, ErrTooManyRecipients) {
		t.Fatalf("четвертий одержувач має бути відхилений: %v", err)
	}
	if k, _ := r.host.AddRecipient("a"); k != keys["a"] {
		t.Fatal("повторне додавання має віддати той самий ключ")
	}
	var wg sync.WaitGroup
	stages := map[string]string{}
	errs := make(chan error, 3)
	for id, k := range keys {
		stage := t.TempDir()
		stages[id] = stage
		wg.Add(1)
		go func(k, stage string) {
			defer wg.Done()
			d := NewDownloader(Options{Dial: r.dial(true), Key: k, StageDir: stage})
			errs <- d.Run(context.Background())
		}(k, stage)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, st := range stages {
		compareTrees(t, m, root, FilesDir(st))
	}
	if !r.host.AllSettled() {
		t.Fatal("усі завершили — AllSettled має бути true")
	}
}

func TestDeclineAndRevoke(t *testing.T) {
	root := makeTree(t, 1000)
	m := buildM(t, root)
	r := startHost(t, root, m, true)
	k1, _ := r.host.AddRecipient("a")
	k2, _ := r.host.AddRecipient("b")

	d := NewDownloader(Options{Dial: r.dial(true), Key: k1, StageDir: t.TempDir()})
	if err := d.Decline(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.host.Revoke("b")
	d2 := NewDownloader(Options{Dial: r.dial(true), Key: k2, StageDir: t.TempDir()})
	if err := d2.Run(context.Background()); !errors.Is(err, ErrAccessClosed) {
		t.Fatalf("після Revoke: %v", err)
	}
	states := map[string]string{}
	for _, s := range r.host.Snapshot() {
		states[s.ID] = s.State
	}
	if states["a"] != StateDeclined || states["b"] != StateRevoked {
		t.Fatalf("стани: %v", states)
	}
	if !r.host.AllSettled() {
		t.Fatal("відмовився + скасований = усе вирішено")
	}
}

func TestChangedManifestDropsOldStage(t *testing.T) {
	root := t.TempDir()
	writeRand(t, filepath.Join(root, "a.txt"), 100)
	m1, _, _ := BuildManifest(context.Background(), root, KindBuild, "x", nil, BuildOptions{})
	r1 := startHost(t, root, m1, false)
	k1, _ := r1.host.AddRecipient("f")
	stage := t.TempDir()
	if err := NewDownloader(Options{Dial: r1.dial(false), Key: k1, StageDir: stage}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Вміст змінився → інший ID → старий stage не змішується з новим.
	writeRand(t, filepath.Join(root, "b.txt"), 50)
	m2, _, _ := BuildManifest(context.Background(), root, KindBuild, "x", nil, BuildOptions{})
	if m1.ID() == m2.ID() {
		t.Fatal("ID вмісту має змінитись")
	}
	r2 := startHost(t, root, m2, false)
	k2, _ := r2.host.AddRecipient("f")
	if err := NewDownloader(Options{Dial: r2.dial(false), Key: k2, StageDir: stage}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	compareTrees(t, m2, root, FilesDir(stage))
}

func TestMoveTree(t *testing.T) {
	src := t.TempDir()
	writeRand(t, filepath.Join(src, "x", "y.bin"), 100)
	dst := filepath.Join(t.TempDir(), "out", "pack")
	if err := MoveTree(src, dst); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "x", "y.bin")); err != nil {
		t.Fatal(err)
	}
	if err := MoveTree(t.TempDir(), dst); err == nil {
		t.Fatal("існуюче призначення має відхилятись")
	}
}
