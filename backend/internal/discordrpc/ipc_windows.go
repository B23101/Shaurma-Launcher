//go:build windows

package discordrpc

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

// ── Windows named pipe клієнт Discord IPC ───────────────────────────────
// Discord слухає \\.\pipe\discord-ipc-0..9. Клієнт реалізований на голому
// x/sys/windows (без сторонніх залежностей, сумісно з CGO_ENABLED=0).
//
// Pipe відкривається з FILE_FLAG_OVERLAPPED: для синхронного (non-overlapped)
// хендла Windows серіалізує read і write на одному хендлі — блокуючий
// ReadFile у readLoop "вішав" WriteFile SET_ACTIVITY на невизначений час,
// тож Discord просто не отримував оновлення присутності. З overlapped I/O
// читання і запис виконуються незалежно, а write додатково обмежений
// таймаутом, щоб повільний Discord ніколи не стопорив лаунчер.

const writeTimeout = 10 * time.Second

// ipcConn — відкритий named pipe з overlapped I/O.
type ipcConn struct {
	handle   windows.Handle
	readEvt  windows.Handle
	writeEvt windows.Handle
	writeMu  sync.Mutex
	// closeMu серіалізує close(): він може прийти з двох сторін одночасно
	// (rebuild() → go m.client.conn.close() і serve() після cl.run()), і без
	// м'ютекса два паралельні close() могли б обидва пройти перевірку
	// handle != 0 і закрити той самий хендл двічі.
	closeMu sync.Mutex

	// OVERLAPPED і буфери ОБОВ'ЯЗКОВО живуть у об'єкті на купі Go, а НЕ на
	// стеку горутини. При ERROR_IO_PENDING ядро пише в OVERLAPPED
	// АСИНХРОННО — вже ПІСЛЯ повернення з ReadFile/WriteFile, а стек
	// горутини Go тим часом може переїхати (stack growth/copy). Стара
	// версія тримала `var ol windows.Overlapped` на стеку й поверталася з
	// функції до завершення I/O — тобто в гіршому разі ядро дописувало в
	// звільнену/переміщену пам'ять (пошкодження пам'яті процесу).
	// Купова алокація не рухається ніколи, тож ця адреса валідна до
	// GetOverlappedResult/CancelIoEx.
	writeOl  windows.Overlapped
	writeBuf []byte
	writeN   uint32

	readOl  windows.Overlapped
	readBuf []byte
	readN   uint32
}

// ipcDial намагається відкрити один із Discord IPC pipe (0..9).
func ipcDial() (*ipcConn, error) {
	var lastErr error = errors.New("discord ipc недоступний")
	for i := 0; i < 10; i++ {
		name, _ := windows.UTF16PtrFromString(fmt.Sprintf(`\\.\pipe\discord-ipc-%d`, i))
		h, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED, 0)
		if err == nil {
			re, e1 := windows.CreateEvent(nil, 1, 0, nil)
			we, e2 := windows.CreateEvent(nil, 1, 0, nil)
			if e1 != nil || e2 != nil {
				if re != 0 {
					windows.CloseHandle(re)
				}
				if we != 0 {
					windows.CloseHandle(we)
				}
				windows.CloseHandle(h)
				return nil, errors.New("discord ipc: CreateEvent failed")
			}
			return &ipcConn{handle: h, readEvt: re, writeEvt: we}, nil
		}
		if err == windows.ERROR_PIPE_BUSY {
			// Pipe існує, але зайнятий — короткі повторні спроби на цьому ж
			// номері (замість Windows WaitNamedPipe, якого немає в x/sys).
			for attempt := 0; attempt < 5; attempt++ {
				time.Sleep(200 * time.Millisecond)
				h2, err2 := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED, 0)
				if err2 == nil {
					re, e1 := windows.CreateEvent(nil, 1, 0, nil)
					we, e2 := windows.CreateEvent(nil, 1, 0, nil)
					if e1 != nil || e2 != nil {
						if re != 0 {
							windows.CloseHandle(re)
						}
						if we != 0 {
							windows.CloseHandle(we)
						}
						windows.CloseHandle(h2)
						return nil, errors.New("discord ipc: CreateEvent failed")
					}
					return &ipcConn{handle: h2, readEvt: re, writeEvt: we}, nil
				}
				if err2 != windows.ERROR_PIPE_BUSY {
					break
				}
			}
			lastErr = err
			continue
		}
		if err != windows.ERROR_FILE_NOT_FOUND {
			lastErr = err
		}
	}
	return nil, lastErr
}

// write шле фрейм [opcode uint32][length uint32][payload] через overlapped
// WriteFile з таймаутом. Блокує лише до writeTimeout, не блокуючи readLoop.
func (c *ipcConn) write(op uint32, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	h := c.handle
	if h == 0 {
		return errors.New("discord ipc: not connected")
	}
	need := 8 + len(payload)
	if cap(c.writeBuf) < need {
		c.writeBuf = make([]byte, need)
	}
	buf := c.writeBuf[:need]
	binary.LittleEndian.PutUint32(buf[0:4], op)
	binary.LittleEndian.PutUint32(buf[4:8], uint32(len(payload)))
	copy(buf[8:], payload)

	// ol і n — поля c (купа), див. коментар біля полів: ядро може писати в
	// них після повернення з WriteFile.
	ol := &c.writeOl
	*ol = windows.Overlapped{}
	ol.HEvent = c.writeEvt
	windows.ResetEvent(c.writeEvt)
	c.writeN = 0

	err := windows.WriteFile(h, buf, &c.writeN, ol)
	if err == windows.ERROR_IO_PENDING {
		w, werr := windows.WaitForSingleObject(c.writeEvt, uint32(writeTimeout/time.Millisecond))
		if werr != nil || w != windows.WAIT_OBJECT_0 {
			// КРИТИЧНО (пошкодження пам'яті): операція ще "висить" у ядрі,
			// яке по завершенню запише в ol і прочитає buf. Скасовуємо саме
			// цей запис і ДОЧІКУЄМОСЬ завершення (wait=true), і лише потім
			// відпускаємо ol/buf.
			_ = windows.CancelIoEx(h, ol)
			var dummy uint32
			_ = windows.GetOverlappedResult(h, ol, &dummy, true)
			if werr != nil {
				return werr
			}
			return fmt.Errorf("discord ipc: write timeout")
		}
		if gerr := windows.GetOverlappedResult(h, ol, &c.writeN, true); gerr != nil {
			return gerr
		}
		return nil
	}
	return err
}

// read читає сирі байти через overlapped ReadFile (блокуючий). Повертає
// (n, nil) або помилку, коли Discord від'єднався/pipe закрито.
func (c *ipcConn) read(buf []byte) (int, error) {
	h := c.handle
	if h == 0 {
		return 0, errors.New("discord ipc: not connected")
	}
	// Буфер теж мусить бути куповим: ядро пише в нього асинхронно, а buf
	// викликача може бути масивом на стеку горутини, що переїде.
	if cap(c.readBuf) < len(buf) {
		c.readBuf = make([]byte, len(buf))
	}
	rbuf := c.readBuf[:len(buf)]

	ol := &c.readOl
	*ol = windows.Overlapped{}
	ol.HEvent = c.readEvt
	windows.ResetEvent(c.readEvt)
	c.readN = 0

	err := windows.ReadFile(h, rbuf, &c.readN, ol)
	if err == windows.ERROR_IO_PENDING {
		if _, werr := windows.WaitForSingleObject(c.readEvt, windows.INFINITE); werr != nil {
			// Операція ще може бути в польоті — скасувати й дочекатись.
			_ = windows.CancelIoEx(h, ol)
			var dummy uint32
			_ = windows.GetOverlappedResult(h, ol, &dummy, true)
			return 0, werr
		}
		if gerr := windows.GetOverlappedResult(h, ol, &c.readN, true); gerr != nil {
			return 0, gerr
		}
	} else if err != nil {
		return 0, err
	}
	n := int(c.readN)
	if n > len(buf) {
		n = len(buf)
	}
	copy(buf, rbuf[:n])
	return n, nil
}

func (c *ipcConn) close() {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	if c.handle != 0 {
		// CancelIoEx скасовує ВСІ незавершені overlapped I/O на хендлі: блокуюче
		// читання readLoop завершується з помилкою і сигналізує readEvt. Без цього
		// закриття хендла/події, поки інша горутина чекає на readEvt
		// (WaitForSingleObject INFINITE), — невизначена поведінка Windows:
		// readLoop міг зависнути НАЗАВЖДИ, канал closed ніколи не закривався,
		// serve() намертво застрягав у cl.run() — Discord RPC «вмирав» після
		// першої ж сесії гри до перезапуску лаунчера (користувач бачив
		// завислу/мертву присутність і процес, що не відпускає pipe).
		_ = windows.CancelIoEx(c.handle, nil)
		windows.CloseHandle(c.handle)
		c.handle = 0
	}
	if c.readEvt != 0 {
		windows.CloseHandle(c.readEvt)
		c.readEvt = 0
	}
	if c.writeEvt != 0 {
		windows.CloseHandle(c.writeEvt)
		c.writeEvt = 0
	}
}
