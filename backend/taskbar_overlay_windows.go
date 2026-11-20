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
	"math"
	"runtime"
	"strconv"
	"syscall"
	"unsafe"

	"shaurma-launcher-backend/internal/launchlog"
)

var (
	gdi32dll               = syscall.NewLazyDLL("gdi32.dll")
	user32overlay          = syscall.NewLazyDLL("user32.dll")
	procCreateBitmap       = gdi32dll.NewProc("CreateBitmap")
	procCreateDIBSection   = gdi32dll.NewProc("CreateDIBSection")
	procCreateCompatibleDC = gdi32dll.NewProc("CreateCompatibleDC")
	procDeleteDC           = gdi32dll.NewProc("DeleteDC")
	procSelectObject       = gdi32dll.NewProc("SelectObject")
	procCreateFont         = gdi32dll.NewProc("CreateFontW")
	procSetTextColor       = gdi32dll.NewProc("SetTextColor")
	procSetBkMode          = gdi32dll.NewProc("SetBkMode")
	procDrawText           = user32overlay.NewProc("DrawTextW")
	procDeleteObject       = gdi32dll.NewProc("DeleteObject")
	procCreateIconIndirec  = user32overlay.NewProc("CreateIconIndirect")
	procDestroyIcon        = user32overlay.NewProc("DestroyIcon")
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

type bitmapInfoHeader struct {
	size            uint32
	width           int32
	height          int32
	planes          uint16
	bitCount        uint16
	compression     uint32
	imageSize       uint32
	xPelsPerMeter   int32
	yPelsPerMeter   int32
	colorsUsed      uint32
	colorsImportant uint32
}

type bitmapInfo struct {
	header bitmapInfoHeader
	colors [1]uint32
}

func (t *itaskbarList3) setOverlayIcon(hwnd, hicon uintptr, desc *uint16) error {
	r, _, _ := syscall.SyscallN(t.lpVtbl.setOverlayIcon, uintptr(unsafe.Pointer(t)), hwnd, hicon,
		uintptr(unsafe.Pointer(desc)))
	return hresultErr(uint32(r))
}

// overlayLabel — що писати в кружку: 1..99, далі «99+».
func overlayLabel(n int) string {
	if n > 99 {
		return "99+"
	}
	return strconv.Itoa(n)
}

// renderOverlayBGRA малює кружок із текстом у premultiplied BGRA (формат 32-бітних іконок із альфою).
func renderOverlayBGRA() []byte {
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
	return px
}

func renderOverlayLabel(bitmap uintptr, label string) error {
	dc, _, _ := procCreateCompatibleDC.Call(0)
	if dc == 0 {
		return fmt.Errorf("CreateCompatibleDC")
	}
	defer procDeleteDC.Call(dc)
	oldBitmap, _, _ := procSelectObject.Call(dc, bitmap)
	if oldBitmap == 0 || oldBitmap == ^uintptr(0) {
		return fmt.Errorf("SelectObject(bitmap)")
	}
	defer procSelectObject.Call(dc, oldBitmap)

	height := int32(20)
	if len(label) == 2 {
		height = 16
	} else if len(label) >= 3 {
		height = 12
	}
	font, _, _ := procCreateFont.Call(
		uintptr(uint32(-height)), 0, 0, 0, 700, 0, 0, 0,
		1, 0, 0, 4 /*ANTIALIASED_QUALITY*/, 0, 0,
	)
	if font == 0 {
		return fmt.Errorf("CreateFontW")
	}
	defer procDeleteObject.Call(font)
	oldFont, _, _ := procSelectObject.Call(dc, font)
	if oldFont == 0 || oldFont == ^uintptr(0) {
		return fmt.Errorf("SelectObject(font)")
	}
	defer procSelectObject.Call(dc, oldFont)
	procSetBkMode.Call(dc, 1 /*TRANSPARENT*/)
	procSetTextColor.Call(dc, 0x00FFFFFF)
	text, _ := syscall.UTF16PtrFromString(label)
	rect := struct{ left, top, right, bottom int32 }{0, 0, overlaySize, overlaySize}
	flags := uintptr(0x1 | 0x4 | 0x20) // DT_CENTER | DT_VCENTER | DT_SINGLELINE
	if drawn, _, _ := procDrawText.Call(dc, uintptr(unsafe.Pointer(text)), uintptr(len(label)), uintptr(unsafe.Pointer(&rect)), flags); drawn == 0 {
		return fmt.Errorf("DrawTextW")
	}
	return nil
}

// makeOverlayIcon створює HICON; викликач мусить викликати DestroyIcon.
func makeOverlayIcon(n int) (uintptr, error) {
	label := overlayLabel(n)
	px := renderOverlayBGRA()
	bmi := bitmapInfo{header: bitmapInfoHeader{
		size: uint32(unsafe.Sizeof(bitmapInfoHeader{})), width: overlaySize,
		height: -overlaySize, planes: 1, bitCount: 32, imageSize: uint32(len(px)),
	}}
	var dibBits unsafe.Pointer
	color, _, _ := procCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&bmi)), 0 /*DIB_RGB_COLORS*/, uintptr(unsafe.Pointer(&dibBits)), 0, 0)
	if color == 0 {
		return 0, fmt.Errorf("CreateDIBSection(color)")
	}
	defer procDeleteObject.Call(color)
	if dibBits == nil {
		return 0, fmt.Errorf("CreateDIBSection returned no pixel buffer")
	}
	dibPixels := unsafe.Slice((*byte)(dibBits), len(px))
	copy(dibPixels, px)
	alpha := make([]byte, len(px)/4)
	for i := range alpha {
		alpha[i] = dibPixels[i*4+3]
	}
	if err := renderOverlayLabel(color, label); err != nil {
		return 0, err
	}
	for i, a := range alpha {
		dibPixels[i*4+3] = a
	}
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
		launchlog.Warn("TaskbarBadge", "головне вікно не знайдено (n=%d)", n)
		return false
	}
	// Та сама вимога, що й в updateTaskbarProgress: CoInitialize → виклики → Release на ОДНОМУ OS-потоці.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	iface, coOwned, err := newTaskbarIface()
	if err != nil {
		launchlog.Warn("TaskbarBadge", "ITaskbarList3: %v", err)
		uninitializeComIfOwned(coOwned)
		return false
	}
	defer func() {
		syscall.SyscallN(iface.lpVtbl.release, uintptr(unsafe.Pointer(iface)))
		uninitializeComIfOwned(coOwned)
	}()
	if n <= 0 {
		ok := logOverlayErr(iface.setOverlayIcon(hwnd, 0, nil))
		if ok {
			launchlog.Info("TaskbarBadge", "бейдж прибрано (hwnd=0x%X)", hwnd)
		}
		return ok
	}
	hicon, err := makeOverlayIcon(n)
	if err != nil {
		launchlog.Warn("TaskbarBadge", "створення іконки: %v", err)
		return false
	}
	defer procDestroyIcon.Call(hicon) // панель завдань тримає власну копію
	desc, _ := syscall.UTF16PtrFromString("Непрочитані повідомлення: " + strconv.Itoa(n))
	ok := logOverlayErr(iface.setOverlayIcon(hwnd, hicon, desc))
	if ok {
		launchlog.Info("TaskbarBadge", "бейдж показано: %d (hwnd=0x%X)", n, hwnd)
	}
	return ok
}

func logOverlayErr(err error) bool {
	if err != nil {
		launchlog.Warn("TaskbarBadge", "SetOverlayIcon: %v", err)
		return false
	}
	return true
}
