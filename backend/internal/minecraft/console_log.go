package minecraft

// ── Консольний лог гри, що ПЕРЕЖИВАЄ лаунчер ─────────────────────────────
//
// ПРОБЛЕМА: раніше stdout/stderr Java-процесу були анонімними pipe'ами
// (cmd.StdoutPipe/StderrPipe) з читачем ВСЕРЕДИНІ процесу лаунчера. Якщо
// лаунчер закривали повністю (трей → «Вийти», вбивство процесу, краш,
// оновлення) а гра при цьому лишалась жити, то:
//
//  1. читаюча сторона pipe зникала разом з лаунчером — усе, що гра писала
//     далі, губилось (або, гірше, гра діставала EPIPE на кожен запис);
//  2. після перезапуску лаунчер міг лише «усиновити» PID (AdoptProcess), але
//     НЕ мав жодного способу знову отримати вивід — pipe вже мертвий, а
//     нового каналу немає. Консоль була порожня: «зв'язок з консоллю гри
//     втрачено, логів немає взагалі».
//
// РІШЕННЯ: JVM отримує як stdout/stderr НЕ pipe, а ФАЙЛ. Дескриптор файлу
// успадковується дочірнім процесом і залишається валідним, навіть коли
// лаунчер помирає. Лаунчер читає цей файл «хвостом» (tail -f):
//
//   - у живому запуску — з початку файлу;
//   - після перезапуску лаунчера — той самий файл: спершу вичитується вся
//     історія (те, що гра написала, поки лаунчера не було), далі стеження за
//     новими рядками. Консоль виглядає так, ніби лаунчер ніколи не закривався.
//
// Файл лежить у теці збірки (logs/shaurma-console.log) і ПЕРЕЗАПИСУЄТЬСЯ на
// кожному новому запуску (як і буфер консолі). Він об'єднує stdout і stderr
// в один потік ОДНИМ дескриптором (так порядок рядків збігається з тим, як їх
// вивела гра); stderr-рядки, які раніше отримували префікс «[ERR] », тепер
// не відрізнити від stdout на рівні файлу — рівень (error/warn) консоль
// визначає за самим текстом (console.DetectLevel), тож на UI це не впливає.

import (
	"shaurma-launcher-backend/internal/crashguard"
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ConsoleLogName — ім'я файлу з виводом гри в теці logs/ збірки. Окремо від
// latest.log (його пише сама гра/Log4j і, за налаштуванням SaveLogs, лаунчер):
// це службовий канал зв'язку лаунчер↔гра, він є ЗАВЖДИ, а не лише коли
// увімкнено «Зберігати логи».
const ConsoleLogName = "shaurma-console.log"

// ConsoleLogPath повертає шлях до консольного лог-файлу збірки за її GameDir.
func ConsoleLogPath(gameDir string) string {
	return filepath.Join(gameDir, "logs", ConsoleLogName)
}

// openConsoleLog створює (перезаписуючи) консольний лог-файл. Повертає файл
// для передачі в cmd.Stdout/cmd.Stderr. os/exec для *os.File НЕ створює pipe
// і горутину-копіювальник — дескриптор напряму дублюється в дочірній процес,
// тому запис іде навіть коли лаунчера вже немає.
func openConsoleLog(gameDir string) (*os.File, error) {
	p := ConsoleLogPath(gameDir)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return nil, err
	}
	// O_APPEND: stdout і stderr — два різні дескриптори НА ОДИН файл у
	// дочірньому процесі (exec дублює один *os.File для обох). Без O_APPEND
	// вони б ділили один зсув (це теж працює, бо дублювання спільне), але
	// O_APPEND додатково гарантує, що запис завжди йде в кінець — навіть
	// якщо хтось поруч (наприклад, сам Minecraft при ротації) чіпає файл.
	return os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|os.O_APPEND, 0o644)
}

// tailPollInterval — як часто перевіряємо, чи дописав щось процес у файл.
// Нижче за 100 мс сенсу нема (UI батчить по 40 мс), а більше — відчутне
// запізнення живого логу.
const tailPollInterval = 100 * time.Millisecond

// tailMaxLine — максимальна довжина одного рядка. Краш-дампи Minecraft
// бувають з довжелезними рядками (серіалізований NBT/JSON) — 1 МБ як і в
// попередньому bufio.Scanner (Buffer(…, 1024*1024)).
const tailMaxLine = 1024 * 1024

// TailFile читає файл path від початку і викликає onLine для кожного
// завершеного рядка, а потім стежить за дописуванням (tail -f), поки не
// спрацює stop. Повертає nil при штатній зупинці (stop закрито) і помилку,
// лише якщо файл так і не вдалось відкрити.
//
// stop закривається викликачем, коли процес гри завершився: перед виходом
// TailFile робить ФІНАЛЬНЕ дочитування (drain) — хвіст логу, який гра
// встигла записати між останнім опитуванням і своїм виходом (напр. стек
// «Game crashed!»), не губиться. Саме цей хвіст найцінніший для аналізу
// краху.
//
// Неповний останній рядок (без \n) утримується, доки не дійде решта; при
// зупинці віддається як є — щоб обірваний краш-рядок теж потрапив у консоль.
//
// waitForFile — скільки чекати появи файлу (гра стартує асинхронно; після
// відновлення файл уже є, тож спрацьовує одразу). 0 = не чекати.
func TailFile(path string, waitForFile time.Duration, stop <-chan struct{}, onLine func(string)) error {
	f, err := openWithWait(path, waitForFile, stop)
	if err != nil {
		return err
	}
	defer f.Close()

	reader := bufio.NewReaderSize(f, 64*1024)
	var partial strings.Builder

	// readAvailable вичитує все, що є зараз, до EOF. EOF для звичайного файлу —
	// не кінець, а «поки нічого нового»: наступне опитування продовжить з
	// того ж місця, бо bufio.Reader після EOF готовий читати далі, якщо файл
	// виріс (на відміну від pipe).
	readAvailable := func() {
		for {
			chunk, err := reader.ReadString('\n')
			if len(chunk) > 0 {
				if strings.HasSuffix(chunk, "\n") {
					partial.WriteString(chunk[:len(chunk)-1])
					emitLine(&partial, onLine)
				} else {
					// Без \n — рядок ще пишеться, тримаємо.
					partial.WriteString(chunk)
					if partial.Len() > tailMaxLine {
						emitLine(&partial, onLine)
					}
				}
			}
			if err != nil {
				return // io.EOF або помилка читання — до наступного опитування
			}
		}
	}

	ticker := time.NewTicker(tailPollInterval)
	defer ticker.Stop()
	for {
		readAvailable()
		select {
		case <-stop:
			// Фінальний drain: дати процесу/ОС «дописати» і вичитати
			// останнє. Короткий сон — щоб буфери ядра встигли потрапити у
			// файл (на момент виходу процесу дані вже там, але це страховка
			// від гонки з нашим останнім опитуванням).
			time.Sleep(50 * time.Millisecond)
			readAvailable()
			if partial.Len() > 0 {
				emitLine(&partial, onLine)
			}
			return nil
		case <-ticker.C:
		}
	}
}

// emitLine віддає накопичений рядок, прибираючи хвостовий \r (Windows CRLF),
// і скидає накопичувач. Порожні рядки теж віддаються — консоль показує
// порожній рядок як є (як і bufio.Scanner раніше).
func emitLine(sb *strings.Builder, onLine func(string)) {
	line := strings.TrimSuffix(sb.String(), "\r")
	sb.Reset()
	if onLine != nil {
		onLine(line)
	}
}

// openWithWait відкриває файл на читання, за потреби чекаючи його появи.
func openWithWait(path string, wait time.Duration, stop <-chan struct{}) (*os.File, error) {
	deadline := time.Now().Add(wait)
	for {
		f, err := os.Open(path)
		if err == nil {
			return f, nil
		}
		if !errors.Is(err, os.ErrNotExist) || wait <= 0 || time.Now().After(deadline) {
			return nil, err
		}
		select {
		case <-stop:
			return nil, err
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// ── Очікування виходу процесу ────────────────────────────────────────────

// procExit — результат очікування виходу власного дочірнього процесу.
// cmd.Wait() можна викликати рівно один раз, а кодові шляхи (forge-retry
// детектує ранній вихід; finishLaunch чекає повний вихід) потребують того
// самого результату — тож Wait викликається в одній горутині, а всі решта
// чекають канал done і читають code.
type procExit struct {
	done chan struct{}
	code int // валідний лише після закриття done
}

func newProcExit(cmd *exec.Cmd) *procExit {
	pe := &procExit{done: make(chan struct{})}
	go func() {
		defer crashguard.Recover("console_log.go:197")
		code := 0
		if err := cmd.Wait(); err != nil {
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				code = ee.ExitCode()
			}
		}
		pe.code = code
		close(pe.done)
	}()
	return pe
}
