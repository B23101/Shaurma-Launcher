// gclog.go — СПРАВЖНЯ пам'ять JVM для сторінки сервера.
//
// Проблема: раніше «RAM сервера» на фронтенді показувалась як RSS процесу
// (GetProcessMemoryInfo → WorkingSetSize). Для JVM це майже не залежить від
// того, скільки сервер РЕАЛЬНО тримає об'єктів: RSS ≈ закомічена купа
// (+метаспейс/потоки/native), тобто «скільки виділено», а не «скільки
// використовується». Саме на це й скаржились: «показує скільки виділено для
// сервера оперативки, а не скільки наразі жере».
//
// Рішення: лаунчер САМ запускає java-процес, тож може попросити JVM, щоб
// вона вела свій власний звіт про пам'ять — GC-лог у файл у теці сервера.
// З рядків GC береться ЖИВА (після збирання) та ЗАКОМІЧЕНА купа, кількість
// збирань і паузи. Це числа САМОЇ JVM (той самий звіт, який користувач
// бачить, увімкнувши GC-лог руками), а не вимір ОС.
//
// Чому не hsperfdata/jcmd: у Aikar's flags (DefaultJVMArgs) стоїть
// -XX:+PerfDisableSharedMem, який вимикає саме перфдані; а jcmd/jstat у
// JRE-збірках, які качає лаунчер, відсутні. GC-лог працює завжди.
package servers

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// gcLogRelPath — шлях GC-логу ВІДНОСНО теки сервера. Робоча тека java-процесу
// дорівнює теці сервера (Manager.Start: cmd.Dir = srv.Dir), тож відносний
// шлях працює завжди — і не ламається на пробілах/двокрапках у абсолютному
// шляху Windows (двокрапка в -Xlog:... — це розділювач підопцій).
const gcLogRelPath = ".shaurma/gc.log"

// GCLogPath повертає абсолютний шлях GC-логу сервера.
func GCLogPath(serverDir string) string {
	return filepath.Join(serverDir, filepath.FromSlash(gcLogRelPath))
}

// hasOwnGCLogging — чи користувач сам налаштував GC-логування у JVM-аргументах.
// Якщо так — свої прапорці НЕ додаємо (не дублюємо вивід і не перебиваємо
// вибір користувача); статистика пам'яті тоді лишається на RSS.
func hasOwnGCLogging(args []string) bool {
	for _, a := range args {
		l := strings.ToLower(a)
		switch {
		case strings.HasPrefix(l, "-xlog"), strings.HasPrefix(l, "-xloggc"),
			strings.HasPrefix(l, "-verbose:gc"),
			strings.HasPrefix(l, "-xx:+printgc"), strings.HasPrefix(l, "-xx:+printgcdetails"):
			return true
		}
	}
	return false
}

// GCLogArgs повертає JVM-прапорці, які змушують JVM писати свій GC-звіт у
// файл (<тека сервера>/.shaurma/gc.log). Порожній результат = у користувача
// вже є власне GC-логування.
//
// Пишемо саме у ФАЙЛ, а не в stdout: інакше кожне збирання сміття додавало б
// по рядку в консоль сервера (для G1 на активному сервері це постійний шум
// серед звичайних логів Minecraft).
func GCLogArgs(javaMajor int, userArgs []string) []string {
	if hasOwnGCLogging(userArgs) {
		return nil
	}
	if javaMajor > 0 && javaMajor <= 8 {
		// Unified logging (-Xlog) з'явився у JDK 9; для Java 8 — старий API.
		return []string{
			"-Xloggc:" + gcLogRelPath,
			"-XX:+PrintGCDetails",
			"-XX:+PrintGCTimeStamps",
		}
	}
	return []string{
		"-Xlog:gc:file=" + gcLogRelPath + ":uptime,level,tags",
	}
}

// ── Розбір GC-логу ──────────────────────────────────────────────────────

// heapPairRe — формат «used->usedAfter(committed)» з одиницями. Так пишуть
// усі «звичайні» колектори (G1/Parallel/Serial) і старий формат Java 8.
// У Java 8 рядок містить КІЛЬКА таких груп (спершу по просторах, потім
// підсумок), тому беремо ОСТАННЮ — вона і є підсумком по всій купі.
var heapPairRe = regexp.MustCompile(`(\d+(?:\.\d+)?)([BKMG])->(\d+(?:\.\d+)?)([BKMG])\((\d+(?:\.\d+)?)([BKMG])\)`)

// heapPairPercentRe — формат ZGC (Java 21+/25: -XX:+UseZGC): замість
// закоміченої купи друкується відсоток від максимуму:
//   GC(0) Minor Collection 512M(33%)->128M(8%) 0.945ms
// Закомічену купу тут дізнатись неможливо — лишаємо її нулем (UI покаже
// замість неї -Xmx сервера), а used беремо з другої групи.
var heapPairPercentRe = regexp.MustCompile(`(\d+(?:\.\d+)?)([BKMG])\(\d+(?:\.\d+)?%\)->(\d+(?:\.\d+)?)([BKMG])\(\d+(?:\.\d+)?%\)`)

// pauseMsRe / pauseSecRe — тривалість паузи: «8.123ms» (JDK 9+ unified
// logging) або «0.0123456 secs» (Java 8) чи «0.012s» (ZGC).
var (
	pauseMsRe  = regexp.MustCompile(`(\d+(?:\.\d+)?)ms\b`)
	pauseSecRe = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*(?:secs?|s)\b`)
	gcIDRe     = regexp.MustCompile(`GC\((\d+)\)`)
)

// stripDecorators прибирає ПРОВІДНІ декоратори unified logging:
//   «[0.537s][info][gc] GC(3) …» → «GC(3) …»
// Потрібно, бо uptime-декоратор має вигляд «0.537s» і міг би бути
// сприйнятий за тривалість паузи (див. pauseFrom).
func stripDecorators(line string) string {
	s := strings.TrimSpace(line)
	for strings.HasPrefix(s, "[") {
		i := strings.IndexByte(s, ']')
		if i < 0 {
			break
		}
		s = s[i+1:]
	}
	return s
}

// pauseFrom повертає тривалість паузи, яка стоїть ПІСЛЯ розібраної групи
// купи (from — індекс кінця цієї групи в body). Це важливо для Java 8:
// рядок містить і «0.0123 secs» (справжня пауза збирання), і хвіст
// «[Times: … real=0.01 secs]» — брати останній збіг означало б показати
// час стіни замість часу паузи.
func pauseFrom(body string, from int) float64 {
	if from < 0 {
		from = 0
	}
	if m := pauseMsRe.FindAllStringSubmatchIndex(body, -1); len(m) > 0 {
		for _, g := range m {
			if g[0] >= from {
				return parseFloat(body[g[2]:g[3]])
			}
		}
	}
	if m := pauseSecRe.FindAllStringSubmatchIndex(body, -1); len(m) > 0 {
		for _, g := range m {
			if g[0] >= from {
				return parseFloat(body[g[2]:g[3]]) * 1000
			}
		}
	}
	return 0
}

func unitToMB(v float64, unit string) float64 {
	switch unit {
	case "B":
		return v / (1024 * 1024)
	case "K":
		return v / 1024
	case "G":
		return v * 1024
	default: // "M"
		return v
	}
}

// GCSample — результат розбору одного GC-рядка.
type GCSample struct {
	UsedMB      float64
	CommittedMB float64 // 0 = колектор не друкує закомічену купу (ZGC)
	PauseMs     float64
	Count       int
	At          time.Time
}

// parseGCLine розбирає один рядок GC-логу. ok=false — рядок не про купу
// (наприклад «GC(4) Concurrent Mark Cycle» або «Using G1»).
func parseGCLine(line string) (GCSample, bool) {
	var s GCSample
	body := stripDecorators(line)

	pairEnd := -1
	if m := heapPairRe.FindAllStringSubmatchIndex(body, -1); len(m) > 0 {
		last := m[len(m)-1] // у Java 8 груп кілька — підсумок по купі останній
		s.UsedMB = unitToMB(parseFloat(body[last[6]:last[7]]), body[last[8]:last[9]])
		s.CommittedMB = unitToMB(parseFloat(body[last[10]:last[11]]), body[last[12]:last[13]])
		pairEnd = last[1]
	} else if m := heapPairPercentRe.FindStringSubmatchIndex(body); m != nil {
		// ZGC: закомічена купа не друкується — лишаємо 0 (UI покаже -Xmx).
		s.UsedMB = unitToMB(parseFloat(body[m[6]:m[7]]), body[m[8]:m[9]])
		pairEnd = m[1]
	} else {
		return s, false
	}

	s.PauseMs = pauseFrom(body, pairEnd)
	if m := gcIDRe.FindStringSubmatch(body); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			s.Count = n + 1 // GC(0) — перше збирання
		}
	}
	s.At = time.Now()
	return s, true
}

func parseFloat(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

// GCLogReader — ІНКРЕМЕНТАЛЬНИЙ читач GC-логу. Лог дописується JVM-ою під
// час роботи сервера, тому на кожному опитуванні (раз на секунду) читаються
// лише НОВІ байти, а накопичувальні показники (кількість збирань, сумарна
// пауза) оновлюються без повторного розбору всього файлу.
type GCLogReader struct {
	path string

	offset int64
	carry  []byte

	sample     GCSample
	valid      bool
	pauseTotal float64
	count      int
}

// NewGCLogReader створює читач для GC-логу сервера.
func NewGCLogReader(serverDir string) *GCLogReader {
	return &GCLogReader{path: GCLogPath(serverDir)}
}

// Reset скидає стан (новий запуск сервера = новий GC-лог).
func (r *GCLogReader) Reset() {
	r.offset = 0
	r.carry = nil
	r.valid = false
	r.pauseTotal = 0
	r.count = 0
	r.sample = GCSample{}
}

// Snapshot повертає накопичені показники (не читаючи файл).
func (r *GCLogReader) Snapshot() (GCSample, int, float64, bool) {
	return r.sample, r.count, r.pauseTotal, r.valid
}

// Poll дочитує файл з останньої позиції й оновлює показники.
func (r *GCLogReader) Poll() {
	st, err := os.Stat(r.path)
	if err != nil {
		// Файлу ще немає (JVM не встигла створити) або сервер зупинився.
		return
	}
	size := st.Size()
	if size < r.offset {
		// Файл перезаписано з нуля (новий запуск сервера / JVM створила лог
		// заново): скидаємо ПОВНІСТЮ — не лише позицію читання, а й
		// накопичені показники, інакше до нового запуску домішувались би
		// кількість збирань і паузи попереднього.
		r.Reset()
	}
	if size == r.offset {
		return
	}

	f, err := os.Open(r.path)
	if err != nil {
		return
	}
	defer f.Close()
	if _, err := f.Seek(r.offset, 0); err != nil {
		return
	}
	// Читаємо лише новий хвіст (між опитуваннями це десятки-сотні байтів),
	// але з обмеженням, щоб після тривалої паузи не завантажити весь файл.
	const maxChunk = 1 << 20 // 1 МБ за одне опитування
	buf := make([]byte, maxChunk)
	n, _ := f.Read(buf)
	if n <= 0 {
		return
	}
	r.offset += int64(n)

	data := append(r.carry, buf[:n]...)
	r.carry = nil
	for {
		idx := bytes.IndexByte(data, '\n')
		if idx < 0 {
			break
		}
		r.consumeLine(string(data[:idx]))
		data = data[idx+1:]
	}
	// Недописаний рядок лишаємо до наступного опитування.
	if len(data) > 0 {
		r.carry = append([]byte(nil), data...)
	}
}

func (r *GCLogReader) consumeLine(line string) {
	if strings.TrimSpace(line) == "" {
		return
	}
	s, ok := parseGCLine(line)
	if !ok {
		return
	}
	if s.Count > 0 {
		if s.Count > r.count {
			r.count = s.Count
		}
	} else {
		// Java 8 (без GC(id)) — рахуємо самі рядки зі підсумком купи.
		r.count++
	}
	if s.PauseMs > 0 {
		r.pauseTotal += s.PauseMs
	}
	r.sample = s
	r.valid = true
}
