package servers

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Рядки взяті з РЕАЛЬНИХ прогонів локальної JVM (JDK 25): G1, Serial і ZGC —
// та з документації формату Java 8 (unified logging -Xlog з'явився лише в 9).
var gcLineCases = []struct {
	name        string
	line        string
	wantOK      bool
	usedMB      float64
	committedMB float64
	pauseMs     float64
	count       int
}{
	{
		name: "G1 young (JDK 9+ unified logging)",
		line: "[0.189s][info][gc] GC(0) Pause Young (Normal) (G1 Evacuation Pause) 5M->1M(10M) 2.542ms",
		wantOK: true, usedMB: 1, committedMB: 10, pauseMs: 2.542, count: 1,
	},
	{
		name: "G1 mixed, великі числа",
		line: "[2.393s][info][gc] GC(30) Pause Young (Mixed) (G1 Evacuation Pause) 41M->31M(67M) 6.704ms",
		wantOK: true, usedMB: 31, committedMB: 67, pauseMs: 6.704, count: 31,
	},
	{
		name: "Serial young",
		line: "[0.105s][info][gc] GC(0) Pause Young (Allocation Failure) 3M->1M(7M) 1.413ms",
		wantOK: true, usedMB: 1, committedMB: 7, pauseMs: 1.413, count: 1,
	},
	{
		name: "ZGC major (відсотки замість закоміченої купи, пауза в секундах)",
		line: "[0.327s][info][gc] GC(0) Major Collection (Warmup) 10M(10%)->8M(8%) 0.012s",
		wantOK: true, usedMB: 8, committedMB: 0, pauseMs: 12, count: 1,
	},
	{
		name: "ZGC minor",
		line: "[2.474s][info][gc] GC(12) Minor Collection (Allocation Rate) 512M(33%)->128M(8%) 0.055s",
		wantOK: true, usedMB: 128, committedMB: 0, pauseMs: 55, count: 13,
	},
	{
		name: "Java 8 Parallel (кілька груп — підсумок останній)",
		line: "2026-09-19T12:00:00.123+0300: 12.345: [GC (Allocation Failure) [PSYoungGen: 524288K->131072K(611840K)] 524288K->131072K(2010112K), 0.0123456 secs] [Times: user=0.05 sys=0.00, real=0.01 secs]",
		wantOK: true, usedMB: 128, committedMB: 1963, pauseMs: 12.3456, count: 0,
	},
	{
		name: "Java 8 Full GC",
		line: "2026-09-19T12:01:00.500+0300: 72.501: [Full GC (Ergonomics) [PSYoungGen: 1024K->0K(611840K)] [ParOldGen: 2048000K->1048576K(2097152K)] 2049024K->1048576K(2708992K), 0.8451234 secs] [Times: user=3.10 sys=0.02, real=0.85 secs]",
		wantOK: true, usedMB: 1024, committedMB: 2645, pauseMs: 845.1234, count: 0,
	},
	{
		name: "рядок без купи — не помилка парсингу, а «не про купу»",
		line: "[0.537s][info][gc] GC(4) Concurrent Mark Cycle",
		wantOK: false,
	},
	{
		name: "шапка логу (Using G1)",
		line: "[0.015s][info][gc] Using G1",
		wantOK: false,
	},
	{
		name: "Java 8 шапка",
		line: "Java HotSpot(TM) 64-Bit Server VM (25.0.2+10-LTS-69) for windows-amd64 JRE (25.0.2+10-LTS-69), built on 2026-01-20",
		wantOK: false,
	},
}

func TestParseGCLine(t *testing.T) {
	for _, tc := range gcLineCases {
		t.Run(tc.name, func(t *testing.T) {
			s, ok := parseGCLine(tc.line)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, очікували %v (рядок: %s)", ok, tc.wantOK, tc.line)
			}
			if !tc.wantOK {
				return
			}
			if diff := s.UsedMB - tc.usedMB; diff > 0.5 || diff < -0.5 {
				t.Errorf("used = %.3f МБ, очікували %.3f", s.UsedMB, tc.usedMB)
			}
			if diff := s.CommittedMB - tc.committedMB; diff > 1.5 || diff < -1.5 {
				t.Errorf("committed = %.3f МБ, очікували %.3f", s.CommittedMB, tc.committedMB)
			}
			if diff := s.PauseMs - tc.pauseMs; diff > 1 || diff < -1 {
				t.Errorf("pause = %.4f мс, очікували %.4f", s.PauseMs, tc.pauseMs)
			}
			if s.Count != tc.count {
				t.Errorf("count = %d, очікували %d", s.Count, tc.count)
			}
		})
	}
}

// TestGCLogReaderIncremental — читач має дочитувати ФАЙЛ, а не перечитувати
// його з нуля: заголовок нирого запуску не має збивати накопичені показники,
// а недописаний рядок — губитись.
func TestGCLogReaderIncremental(t *testing.T) {
	dir := t.TempDir()
	path := GCLogPath(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	w, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	// Перший запис: два повних рядки + недописаний третій.
	first := "[0.189s][info][gc] GC(0) Pause Young (Normal) 5M->1M(10M) 2.542ms\n" +
		"[0.392s][info][gc] GC(1) Pause Young (Normal) 4M->2M(10M) 2.219ms\n" +
		"[0.505s][info][gc] GC(2) Pause Young (Norm"
	if _, err := w.WriteString(first); err != nil {
		t.Fatalf("write: %v", err)
	}

	r := NewGCLogReader(dir)
	r.Poll()
	sample, count, pauseTotal, valid := r.Snapshot()
	if !valid {
		t.Fatal("після першого запису має бути валідний зразок")
	}
	if sample.UsedMB != 2 || sample.CommittedMB != 10 {
		t.Fatalf("used/committed = %.1f/%.1f, очікували 2/10 (останній ПОВНИЙ рядок)", sample.UsedMB, sample.CommittedMB)
	}
	if count != 2 {
		t.Fatalf("count = %d, очікували 2", count)
	}
	if pauseTotal < 4.7 || pauseTotal > 4.8 {
		t.Fatalf("pauseTotal = %.3f, очікували ≈4.761", pauseTotal)
	}

	// Дописуємо решту третього рядка і ще один: має врахуватись лише нове.
	if _, err := w.WriteString("al) (G1 Evacuation Pause) 5M->3M(10M) 1.606ms\n" +
		"[0.542s][info][gc] GC(4) Pause Cleanup 3M->3M(10M) 0.008ms\n"); err != nil {
		t.Fatalf("write 2: %v", err)
	}
	w.Close()

	r.Poll()
	sample, count, pauseTotal, _ = r.Snapshot()
	if sample.UsedMB != 3 || sample.CommittedMB != 10 {
		t.Fatalf("після допису used/committed = %.1f/%.1f, очікували 3/10", sample.UsedMB, sample.CommittedMB)
	}
	if count != 5 { // GC(4) → 5 збирань
		t.Fatalf("count = %d, очікували 5", count)
	}
	if pauseTotal < 6.3 || pauseTotal > 6.5 {
		t.Fatalf("pauseTotal = %.3f, очікували ≈6.375", pauseTotal)
	}

	// Новий запуск сервера: файл перезаписано з нуля → читач має скинутись.
	if err := os.WriteFile(path, []byte("[0.100s][info][gc] GC(0) Pause Young (Normal) 8M->4M(16M) 1.000ms\n"), 0o644); err != nil {
		t.Fatalf("truncate write: %v", err)
	}
	r.Poll()
	sample, count, pauseTotal, valid = r.Snapshot()
	if !valid || sample.UsedMB != 4 || sample.CommittedMB != 16 {
		t.Fatalf("після перезапису отримали used/committed = %.1f/%.1f (valid=%v)", sample.UsedMB, sample.CommittedMB, valid)
	}
	if count != 1 || pauseTotal < 0.9 || pauseTotal > 1.1 {
		t.Fatalf("після перезапису count = %d, pauseTotal = %.3f — очікували 1 та ≈1.0", count, pauseTotal)
	}
}

// TestGCLogReaderMissingFile — до першого збирання файлу може не бути:
// читач має просто не мати зразка (а не панікувати).
func TestGCLogReaderMissingFile(t *testing.T) {
	r := NewGCLogReader(t.TempDir())
	r.Poll()
	if _, _, _, valid := r.Snapshot(); valid {
		t.Fatal("без файлу зразка бути не може")
	}
}

// TestGCLogArgs — прапорці GC-логу додаються лише коли користувач не
// налаштував логування сам, і різняться для Java 8 та 9+.
func TestGCLogArgs(t *testing.T) {
	modern := GCLogArgs(21, nil)
	if len(modern) != 1 || !containsSubstr(modern[0], "-Xlog:gc:file=.shaurma/gc.log") {
		t.Fatalf("Java 21: отримали %v", modern)
	}
	legacy := GCLogArgs(8, []string{"-Xmx2G"})
	if len(legacy) != 3 || !containsSubstr(legacy[0], "-Xloggc:") {
		t.Fatalf("Java 8: отримали %v", legacy)
	}
	if got := GCLogArgs(21, []string{"-Xlog:gc"}); got != nil {
		t.Fatalf("власне GC-логування користувача має скасовувати наші прапорці, отримали %v", got)
	}
	if got := GCLogArgs(21, []string{"-XX:+PrintGCDetails"}); got != nil {
		t.Fatalf("власне Java 8-логування має скасовувати наші прапорці, отримали %v", got)
	}
}

// TestGCLogEndToEndWithRealJVM — найцінніша перевірка ланцюга
// «прапорці → JVM → файл → парсер»: запускаємо СПРАВЖНЮ java з ТИМИ
// САМИМИ прапорцями, які додає лаунчер (GCLogArgs) і з тим самим робочим
// каталогом (cmd.Dir = тека сервера, шлях логу відносний), а потім читаємо
// файл нашим GCLogReader. Без java/JDK — skip (не валимо CI).
func TestGCLogEndToEndWithRealJVM(t *testing.T) {
	javaPath, err := exec.LookPath("java")
	if err != nil {
		t.Skip("java не знайдено — e2e GC-логу пропущено")
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".shaurma"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Простенька програма: маленький живий набір + багато сміття — саме той
	// профіль, на якому G1/Serial друкують young-збирання з [used->committed(M)].
	prog := `public class MemHog {
  public static void main(String[] a) throws Exception {
    byte[] keep = new byte[8 * 1024 * 1024];
    long sum = 0;
    for (int i = 0; i < 3000; i++) {
      byte[] tmp = new byte[384 * 1024];
      tmp[0] = (byte) i;
      sum += tmp[0];
      if (i % 500 == 0) { Thread.sleep(5); }
    }
    System.out.println("ok " + keep.length + " " + sum);
  }
}
`
	src := filepath.Join(dir, "MemHog.java")
	if err := os.WriteFile(src, []byte(prog), 0o644); err != nil {
		t.Fatalf("write java: %v", err)
	}

	args := append([]string{"-Xmx64m", "-XX:+UseSerialGC"}, GCLogArgs(21, nil)...)
	args = append(args, src)
	cmd := exec.Command(javaPath, args...)
	cmd.Dir = dir
	out, runErr := cmd.CombinedOutput()

	r := NewGCLogReader(dir)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r.Poll()
		if _, _, _, valid := r.Snapshot(); valid {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	sample, count, _, valid := r.Snapshot()
	if !valid {
		// Сама java не запустилась (JRE без jdk.compiler — однофайловий
		// режим недоступний). Перевірити ланцюг нічим — не валимо CI.
		if runErr != nil {
			t.Skipf("java не виконав програму (%v): %s", runErr, strings.TrimSpace(string(out)))
		}
		t.Fatalf("справжня JVM не залишила жодного рядка з купою у %s", GCLogPath(dir))
	}
	if sample.UsedMB <= 0 {
		t.Fatalf("used = %.2f МБ — підозріло", sample.UsedMB)
	}
	if sample.CommittedMB < sample.UsedMB {
		t.Fatalf("закомічена купа (%.2f) менша за живу (%.2f)", sample.CommittedMB, sample.UsedMB)
	}
	if sample.CommittedMB > 64+1 {
		t.Fatalf("закомічена купа (%.2f) більша за -Xmx 64 МБ", sample.CommittedMB)
	}
	if count < 1 {
		t.Fatalf("кількість збирань = %d", count)
	}
}

func containsSubstr(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
