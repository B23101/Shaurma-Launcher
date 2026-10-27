//go:build linux

package discordrpc

// ── Linux Unix-сокет клієнт Discord IPC ─────────────────────────────────
// На Linux Discord (і клони — Vesktop, ArmCord тощо) слухають Unix domain
// socket $XDG_RUNTIME_DIR/discord-ipc-0..9 замість named pipe на Windows,
// але формат самого кадру IPC ідентичний: [opcode uint32 LE][length
// uint32 LE][payload], тому manager.go (спільний для обох платформ)
// працює без жодних змін — різниться лише транспорт (Частина 2.4 плану).
//
// На відміну від Windows-версії тут не потрібне ручне overlapped I/O:
// звичайний net.Conn з таймаутами через SetWriteDeadline/SetReadDeadline
// дає ту саму гарантію "write ніколи не блокує лаунчер назавжди" значно
// простішим кодом — Go net-пакет сам ховає epoll-механіку.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const writeTimeout = 10 * time.Second

// ipcConn — відкритий Unix-сокет до Discord IPC.
type ipcConn struct {
	conn net.Conn
	// closeMu серіалізує close(): той самий захист від подвійного
	// закриття, що й у Windows-версії (rebuild() і serve() можуть
	// покликати close() з двох горутин одночасно).
	closeMu sync.Mutex
	closed  bool
}

// socketCandidates повертає можливі базові теки для Discord IPC-сокетів
// у порядку пріоритету. $XDG_RUNTIME_DIR — стандартне місце; snap/flatpak
// збірки Discord іноді кладуть сокет у піддеку app/, тому додатково
// перевіряємо і її, перш ніж здатись.
func socketCandidates() []string {
	var dirs []string
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		dirs = append(dirs, xdg, filepath.Join(xdg, "app", "com.discordapp.Discord"))
	}
	// /tmp — fallback для середовищ без XDG_RUNTIME_DIR (рідкісне, але
	// трапляється в мінімальних/контейнерних сесіях).
	dirs = append(dirs, "/tmp")
	return dirs
}

// ipcDial перебирає всі можливі сокети discord-ipc-0..9 у кожній з
// кандидатних тек — той самий діапазон номерів, що й на Windows
// (Discord відкриває перший вільний, клони можуть займати інші).
func ipcDial() (*ipcConn, error) {
	var lastErr error = errors.New("discord ipc недоступний")
	for _, dir := range socketCandidates() {
		for i := 0; i < 10; i++ {
			path := filepath.Join(dir, fmt.Sprintf("discord-ipc-%d", i))
			if _, err := os.Stat(path); err != nil {
				continue
			}
			c, err := net.DialTimeout("unix", path, 2*time.Second)
			if err != nil {
				lastErr = err
				continue
			}
			return &ipcConn{conn: c}, nil
		}
	}
	return nil, lastErr
}

// write шле фрейм [opcode][length][payload] з таймаутом запису — той
// самий контракт, що й Windows-версія: блокує щонайбільше writeTimeout,
// ніколи не вішає лаунчер, якщо Discord перестав вичитувати сокет.
func (c *ipcConn) write(op uint32, payload []byte) error {
	if c.conn == nil {
		return errors.New("discord ipc: not connected")
	}
	buf := make([]byte, 8+len(payload))
	binary.LittleEndian.PutUint32(buf[0:4], op)
	binary.LittleEndian.PutUint32(buf[4:8], uint32(len(payload)))
	copy(buf[8:], payload)

	if err := c.conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		return err
	}
	_, err := c.conn.Write(buf)
	return err
}

// read читає сирі байти (блокуючий, без дедлайну — той самий контракт,
// що й Windows ReadFile з INFINITE-очікуванням; readLoop у manager.go
// сам працює у власній горутині й розраховує саме на блокуюче read).
func (c *ipcConn) read(buf []byte) (int, error) {
	if c.conn == nil {
		return 0, errors.New("discord ipc: not connected")
	}
	// Знімаємо будь-який попередній дедлайн читання — close() розриває
	// з'єднання явним Close(), не покладаючись на дедлайн-таймаут.
	if err := c.conn.SetReadDeadline(time.Time{}); err != nil {
		return 0, err
	}
	return c.conn.Read(buf)
}

// close розриває з'єднання. net.Conn.Close() на Unix-сокеті одразу
// розблоковує будь-який паралельний блокуючий Read() з помилкою — той
// самий ефект, що й CancelIoEx на Windows, тільки без потреби викликати
// його окремо: закриття хендла саме по собі й є скасуванням I/O на POSIX.
func (c *ipcConn) close() {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	if c.closed || c.conn == nil {
		return
	}
	_ = c.conn.Close()
	c.closed = true
}
