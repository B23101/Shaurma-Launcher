//go:build windows

package main

// taskbar_overlay_windows.go — бейдж непрочитаних повідомлень на іконці лаунчера в панелі завдань
// (ITaskbarList3::SetOverlayIcon): червоний кружок із числом, як у Discord/Telegram.
//
// ЧОМУ З backend.dll, а не з shell: shell.exe «назавжди незмінний» (див. taskbar_windows.go), а
// backend.dll живе в тому самому процесі й уже вміє знаходити HWND головного вікна та кликати
// ITaskbarList3. Команда window_op("main", "badge:N") лишається для майбутньої оболонки, але
// нинішня її ігнорує — тому без цього файлу число в панелі завдань не з'являлось ніколи.
//
// Іконка малюється в пам'яті (кружок + піксельні цифри 3×5), без шрифтів і зовнішніх залежностей.

import (
	"fmt"
	"log"
	"math"
	"runtime"
	"strconv"
	"syscall"
	"unsafe"
)

var (
	gdi32dll              = syscall.NewLazyDLL("gdi32.dll")
	user32overlay         = syscall.NewLazyDLL("user32.dll")
	procCreateBitmap      = gdi32dll.NewProc("CreateBitmap")
	procDeleteObject      = gdi32dll.NewProc("DeleteObject")
	procCreateIconIndirec = user32overlay.NewProc("CreateIconIndirect")
	procDestroyIcon       = user32overlay.NewProc("DestroyIcon")
)

const overlaySize = 32 // 32×32: Windows сама зменшує до 16 при 100% масштабі й лишає чіткість на HiDPI

// iconInfo — ICONINFO (fIcon BOOL, hotspot, маска, колір).
type iconInfo struct {
	fIcon    int32
	xHotspot uint32
	yHotspot uint32
	hbmMask  uintptr
	hbmColor uintptr
}

func (t *itaskbarList3) setOverlayIcon(hwnd, hicon uintptr, desc *uint16) error {
	r, _, _ := syscall.SyscallN(t.lpVtbl.setOverlayIcon, uintptr(unsafe.Pointer(t)), hwnd, hicon,
		uintptr(unsafe.Pointer(desc)))
	return hresultErr(uint32(r))
}

// Шрифт 3×5: кожен рядок — 3 біти (старший = лівий піксель).
var overlayGlyphs = map[rune][5]uint8{
	'0': {0b111, 0b101, 0b101, 0b101, 0b111},
	'1': {0b010, 0b110, 0b010, 0b010, 0b111},
	'2': {0b111, 0b001, 0b111, 0b100, 0b111},
	'3': {0b111, 0b001, 0b111, 0b001, 0b111},
	'4': {0b101, 0b101, 0b111, 0b001, 0b001},
	'5': {0b111, 0b100, 0b111, 0b001, 0b111},
	'6': {0b111, 0b100, 0b111, 0b101, 0b111},
	'7': {0b111, 0b001, 0b001, 0b001, 0b001},
	'8': {0b111, 0b101, 0b111, 0b101, 0b111},
	'9': {0b111, 0b101, 0b111, 0b001, 0b111},
	'+': {0b000, 0b010, 0b111, 0b010, 0b000},
}

// overlayLabel — що писати в кружку: 1..99, далі «99+».
func overlayLabel(n int) string {
	if n > 99 {
		return "99+"
	}
	return strconv.Itoa(n)
}

// renderOverlayBGRA малює кружок із текстом у premultiplied BGRA (формат 32-бітних іконок із альфою).
func renderOverlayBGRA(label string) []byte {
	const size = overlaySize
	px := make([]byte, size*size*4)
	// Червоний кружок зі згладженим краєм.
	const cx, cy, radius = float64(size)/2 - 0.5, float64(size)/2 - 0.5, float64(size)/2 - 0.5
	const r, g, b = 239, 68, 68
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			d := math.Hypot(float64(x)-cx, float64(y)-cy)
			a := math.Max(0, math.Min(1, radius-d+0.5))
			i := (y*size + x) * 4
			px[i+0] = byte(float64(b) * a)
			px[i+1] = byte(float64(g) * a)
			px[i+2] = byte(float64(r) * a)
			px[i+3] = byte(255 * a)
		}
	}
	// Масштаб за довжиною тексту, щоб вміщалось у кружок.
	scale := 4
	switch len(label) {
	case 2:
		scale = 3
	case 3:
		scale = 2
	}
	gap := scale
	w := len(label)*3*scale + (len(label)-1)*gap
	h := 5 * scale
	x0, y0 := (size-w)/2, (size-h)/2
	for ci, ch := range label {
		glyph, ok := overlayGlyphs[ch]
		if !ok {
			continue
		}
		gx := x0 + ci*(3*scale+gap)
		for row := 0; row < 5; row++ {
			for col := 0; col < 3; col++ {
				if glyph[row]&(1<<(2-col)) == 0 {
					continue
				}
				for dy := 0; dy < scale; dy++ {
					for dx := 0; dx < scale; dx++ {
						i := ((y0+row*scale+dy)*size + gx + col*scale + dx) * 4
						px[i], px[i+1], px[i+2], px[i+3] = 255, 255, 255, 255
					}
				}
			}
		}
	}
	return px
}

// makeOverlayIcon створює HICON; викликач мусить викликати DestroyIcon.
func makeOverlayIcon(n int) (uintptr, error) {
	px := renderOverlayBGRA(overlayLabel(n))
	color, _, _ := procCreateBitmap.Call(overlaySize, overlaySize, 1, 32, uintptr(unsafe.Pointer(&px[0])))
	if color == 0 {
		return 0, fmt.Errorf("CreateBitmap(color)")
	}
	defer procDeleteObject.Call(color)
	mask := make([]byte, overlaySize*overlaySize/8) // усе 0: видимість задає альфа кольорової частини
	hmask, _, _ := procCreateBitmap.Call(overlaySize, overlaySize, 1, 1, uintptr(unsafe.Pointer(&mask[0])))
	if hmask == 0 {
		return 0, fmt.Errorf("CreateBitmap(mask)")
	}
	defer procDeleteObject.Call(hmask)
	ii := iconInfo{fIcon: 1, hbmMask: hmask, hbmColor: color}
	hicon, _, _ := procCreateIconIndirec.Call(uintptr(unsafe.Pointer(&ii)))
	if hicon == 0 {
		return 0, fmt.Errorf("CreateIconIndirect")
	}
	return hicon, nil
}

// setTaskbarOverlay показує бейдж із числом n на іконці лаунчера (n <= 0 — прибирає).
// Повертає false, якщо не вдалось (вікна ще немає, COM недоступний) — викликач спробує знову пізніше.
func setTaskbarOverlay(n int) bool {
	if safeModeActive() {
		return true // безпечний режим: COM не чіпаємо (як і прогрес), повторювати немає сенсу
	}
	hwnd := mainWindowHwnd()
	if hwnd == 0 {
		log.Printf("taskbar badge: головне вікно не знайдено")
		return false
	}
	// Та сама вимога, що й в updateTaskbarProgress: CoInitialize → виклики → Release на ОДНОМУ OS-потоці.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	iface, coOwned, err := newTaskbarIface()
	if err != nil {
		log.Printf("taskbar badge: ITaskbarList3: %v", err)
		uninitializeComIfOwned(coOwned)
		return false
	}
	defer func() {
		syscall.SyscallN(iface.lpVtbl.release, uintptr(unsafe.Pointer(iface)))
		uninitializeComIfOwned(coOwned)
	}()
	if n <= 0 {
		return logOverlayErr(iface.setOverlayIcon(hwnd, 0, nil))
	}
	hicon, err := makeOverlayIcon(n)
	if err != nil {
		log.Printf("taskbar badge: %v", err)
		return false
	}
	defer procDestroyIcon.Call(hicon) // панель завдань тримає власну копію
	desc, _ := syscall.UTF16PtrFromString("Непрочитані повідомлення: " + strconv.Itoa(n))
	return logOverlayErr(iface.setOverlayIcon(hwnd, hicon, desc))
}

func logOverlayErr(err error) bool {
	if err != nil {
		log.Printf("taskbar badge: SetOverlayIcon: %v", err)
		return false
	}
	return true
}
