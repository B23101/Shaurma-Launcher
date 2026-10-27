// recover_console.go — консоль сервера, що читається з ФАЙЛУ (див. recover.go).
//
// Джерело — logs/shaurma-console.log (console_file.go): туда пише сам сервер
// через успадкований дескриптор, тому файл живий і тоді, коли лаунчера немає.
// Працює однаково в обох випадках:
//   - звичайний запуск: tail стартує з нуля — консоль жива з першого рядка;
//   - після краша/перезапуску: спершу історія (усе, що сервер написав, поки
//     лаунчера не було), далі — нові рядки наживо.
//
// Fallback на logs/latest.log — для серверів, запущених ПОПЕРЕДНЬОЮ версією
// лаунчера (тоді файлу shaurma-console.log ще немає).
//
// Не працює лише ВВІД команд: stdin — анонімний pipe, який помирає разом із
// лаунчером. Для команд потрібен окремий хост-процес-володар stdin.
package servers

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"shaurma-launcher-backend/internal/console"
)

const (
	// recoveredHistoryLines — скільки останніх рядків файлу виводу показати
	// одразу (після відновлення це вся історія, поки лаунчера не було).
	recoveredHistoryLines = 500
	// recoveredHistoryBytes — скільки байтів з кінця файлу читати для історії.
	recoveredHistoryBytes = 256 * 1024
	// followChunkBytes — макс. обсяг, що читається за один тік «хвоста».
	followChunkBytes = 1 << 20
	// followInterval — 200 мс: файл читається локально, а людина чекає
	// реакції консолі (500 мс відчувалось як «лог гальмує»).
	followInterval = 200 * time.Millisecond
)

// consoleSourcePath — звідки читати консоль сервера: свій
// shaurma-console.log (повний вивід, console_file.go), а якщо його немає —
// logs/latest.log (сервер запущено попередньою версією лаунчера).
func consoleSourcePath(dir string) string {
	if p := serverConsoleLogPath(dir); fileExists(p) {
		return p
	}
	return latestLogPath(dir)
}

// latestLogPath — logs/latest.log сервера.
func latestLogPath(dir string) string {
	return filepath.Join(dir, "logs", "latest.log")
}

// splitLogChunk додає chunk до partial (недописаний хвіст попереднього
// читання) і повертає повні рядки та новий partial. Кінці рядків \r\n і \n
// нормалізуються; порожні рядки пропускаються.
func splitLogChunk(partial, chunk string) (lines []string, rest string) {
	data := partial + chunk
	i := strings.LastIndexByte(data, '\n')
	if i < 0 {
		return nil, data
	}
	for _, l := range strings.Split(data[:i], "\n") {
		l = strings.TrimRight(l, "\r")
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines, data[i+1:]
}

// readTailLines читає останні n рядків файлу (не більше maxBytes з кінця).
// offset — позиція, з якої продовжувати читання: кінець останнього ПОВНОГО
// рядка (недописаний хвіст буде прочитаний наступним тіком).
func readTailLines(path string, n int, maxBytes int64) (lines []string, offset int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	size := st.Size()
	start := int64(0)
	if size > maxBytes {
		start = size - maxBytes
	}
	data := make([]byte, size-start)
	if _, err := f.ReadAt(data, start); err != nil && int64(len(data)) > 0 {
		return nil, 0, err
	}
	text := string(data)
	if start > 0 {
		// Перший рядок міг бути обрізаний посередині — відкидаємо його.
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			text = text[i+1:]
		} else {
			text = ""
		}
	}
	last := strings.LastIndexByte(string(data), '\n')
	if last < 0 {
		return nil, start, nil
	}
	offset = start + int64(last) + 1
	all, _ := splitLogChunk("", text)
	if len(all) > n {
		all = all[len(all)-n:]
	}
	return all, offset, nil
}

// loadRecoveredHistory кладе в буфер консолі хвіст файлу виводу (лише якщо
// буфер порожній — напр. після перезапуску лаунчера) і повертає offset, з
// якого слід «хвостити» файл далі. Помилки не фатальні: без файлу консоль
// просто лишається порожньою.
func (m *Manager) loadRecoveredHistory(serverID, dir string, buf *console.Buffer) int64 {
	lines, offset, err := readTailLines(consoleSourcePath(dir), recoveredHistoryLines, recoveredHistoryBytes)
	if err != nil {
		return 0
	}
	if len(buf.Snapshot()) == 0 {
		for _, l := range lines {
			buf.AppendLevel(console.DetectLevel(l), l)
		}
	}
	return offset
}

// followLogFile «хвостить» файл виводу сервера й подає нові рядки в консоль:
// буфер, лічильник гравців, події входу/виходу і — якщо побачив "Done …!" —
// переводить статус у "running". Завершується, коли процес зник із running.
func (m *Manager) followLogFile(serverID string, pi *processInfo, buf *console.Buffer, path string, offset int64) {
	partial := ""
	ticker := time.NewTicker(followInterval)
	defer ticker.Stop()
	for {
		select {
		case <-pi.exited:
			return
		case <-ticker.C:
		}
		m.mu.RLock()
		cur, ok := m.running[serverID]
		m.mu.RUnlock()
		if !ok || cur != pi {
			return
		}

		f, err := os.Open(path)
		if err != nil {
			continue
		}
		st, err := f.Stat()
		if err != nil {
			f.Close()
			continue
		}
		size := st.Size()
		if size < offset { // файл перестворено/усічено — читаємо з початку
			offset, partial = 0, ""
		}
		if size == offset {
			f.Close()
			continue
		}
		n := size - offset
		if n > followChunkBytes {
			n = followChunkBytes
		}
		data := make([]byte, n)
		read, _ := f.ReadAt(data, offset)
		f.Close()
		offset += int64(read)

		var lines []string
		lines, partial = splitLogChunk(partial, string(data[:read]))
		for _, text := range lines {
			line := buf.AppendLevel(console.DetectLevel(text), text)
			m.notifyLine(serverID, line)
			UpdatePlayerCount(serverID, text)
			if ev, ok := UpdatePlayersFromLog(serverID, text); ok {
				m.notifyPlayer(serverID, ev)
			}
			// "Done (X.XXXs)! For help, type \"help\"" — сервер готовий.
			if strings.Contains(text, "Done") && strings.Contains(text, "!") {
				m.markServerReady(serverID)
			}
		}
	}
}
