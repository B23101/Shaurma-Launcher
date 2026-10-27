//go:build !windows

package servers

import (
	"os"
	"strconv"
	"strings"
	"syscall"
)

// processSnapshot — ідентичність процесу на момент знімка (аналог
// windows-реалізації; див. procident_windows.go для повного пояснення,
// навіщо потрібен час створення).
type processSnapshot struct {
	Alive    bool
	CreateNs int64
	Image    string
}

// snapshotProcess читає ідентичність процесу з /proc (Linux) або через
// kill(pid, 0) (macOS/BSD — там час створення недоступний, тож
// використовується лише факт існування процесу).
func snapshotProcess(pid int) processSnapshot {
	if pid <= 0 {
		return processSnapshot{}
	}
	snap := processSnapshot{}
	// kill(pid, 0): немає помилки → процес існує (або належить нам).
	if err := syscall.Kill(pid, 0); err != nil {
		return processSnapshot{}
	}
	snap.Alive = true

	if _, err := os.Stat("/proc"); err == nil {
		if raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat"); err == nil {
			// starttime — 22-ге поле, АЛЕ comm (2-ге) може містити пробіли,
			// тож спершу відрізаємо все до останньої дужки.
			s := string(raw)
			if i := strings.LastIndex(s, ")"); i >= 0 && i+2 < len(s) {
				fields := strings.Fields(s[i+2:])
				// fields[0] — стан процесу. Z (zombie) / X (dead): процес уже
				// завершився, лишився лише запис у таблиці процесів, а
				// kill(pid, 0) на ньому ще успішний — вважати його живим не
				// можна (інакше «усиновився» б мертвий сервер).
				if len(fields) > 0 && (fields[0] == "Z" || fields[0] == "X") {
					return processSnapshot{}
				}
				if len(fields) >= 20 {
					if ticks, err := strconv.ParseInt(fields[19], 10, 64); err == nil {
						snap.CreateNs = ticks
					}
				}
			}
		}
		if exe, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe"); err == nil {
			snap.Image = exe
		}
	}
	return snap
}

// isJavaImage — чи виконуваний файл процесу — java.
func isJavaImage(image string) bool {
	if image == "" {
		// На macOS/BSD шлях до exe не читається — не блокуємо відновлення
		// лише через це (PID + час створення вже звірені).
		return true
	}
	base := strings.ToLower(image)
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	return base == "java" || base == "java.exe" || base == "javaw.exe"
}
