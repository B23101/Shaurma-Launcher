package main

import (
	"errors"
	"image"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

// Над-начення копіювання у системний буфер обміну Windows через Win32 API.
//
// ЧОМУ не navigator.clipboard у фронтенді: WebView2 у цьому лаунчері
// копіює «нікуди» — тост «Скопійовано» з'являється, але текст у буфер
// обміну Windows (що видно у Win+V / панелі буфера) НЕ потрапляє. Тому всі
// кнопки копіювання (лог, «важливе», адреса сервера, посилання на мод,
// текст файла) ходять сюди — надійний запис через WM_CLIPBOARD.
var (
	user32               = syscall.NewLazyDLL("user32.dll")
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	procOpenClipboard    = user32.NewProc("OpenClipboard")
	procEmptyClipboard   = user32.NewProc("EmptyClipboard")
	procSetClipboardData = user32.NewProc("SetClipboardData")
	procCloseClipboard   = user32.NewProc("CloseClipboard")
	procGetClipboardData = user32.NewProc("GetClipboardData")
	procIsFmtAvailable   = user32.NewProc("IsClipboardFormatAvailable")
	procGlobalAlloc      = kernel32.NewProc("GlobalAlloc")
	procGlobalLock       = kernel32.NewProc("GlobalLock")
	procGlobalUnlock     = kernel32.NewProc("GlobalUnlock")
	procGlobalFree       = kernel32.NewProc("GlobalFree")
)

const (
	cfUnicodeText = uintptr(13) // CF_UNICODETEXT
	cfDIB         = uintptr(8)  // CF_DIB — растрове зображення (BITMAPINFOHEADER + пікселі)
	gmemMoveable  = uintptr(0x0002)
)

// openClipboardRetrying — те саме ретраювання OpenClipboard, що
// використовує setClipboardText, винесене в окрему функцію, щоб
// setClipboardImage міг скористатись тим самим підходом.
func openClipboardRetrying() error {
	for i := 0; i < 5; i++ {
		if r, _, _ := procOpenClipboard.Call(0); r != 0 {
			return nil
		}
		time.Sleep(60 * time.Millisecond)
	}
	return errors.New("буфер обміну зайнятий іншою програмою")
}

// setClipboardText кладе UTF-16 текст у системний буфер обміну.
// Буфер може бути тимчасово захоплений іншою програмою — OpenClipboard
// робимо з невеликими ретраями; якщо й далі зайнятий — повертаємо помилку.
func setClipboardText(text string) error {
	if err := openClipboardRetrying(); err != nil {
		return err
	}
	defer procCloseClipboard.Call()

	if r, _, _ := procEmptyClipboard.Call(); r == 0 {
		return errors.New("не вдалось очистити буфер обміну")
	}

	utf16s := utf16.Encode([]rune(text))
	if len(utf16s) == 0 {
		// Порожній текст — вже «скопійовано» (нічого писати).
		return nil
	}
	utf16s = append(utf16s, 0) // нуль-термінатор
	nBytes := len(utf16s) * 2

	hMem, _, _ := procGlobalAlloc.Call(gmemMoveable, uintptr(nBytes))
	if hMem == 0 {
		return errors.New("не вдалось виділити пам'ять для буфера обміну")
	}
	owned := false
	defer func() {
		// У разі УСПІХУ SetClipboardData забирає володіння пам'яттю —
		// тоді її НЕ звільняємо (інакше буфер обміну отримає вивільнений
		// вказівник). Звільняємо лише при помилці до передачі.
		if !owned {
			procGlobalFree.Call(hMem)
		}
	}()

	pMem, _, _ := procGlobalLock.Call(hMem)
	if pMem == 0 {
		return errors.New("не вдалось заблокувати пам'ять буфера обміну")
	}
	// uintptr з syscall → вказівник через мемну переінтерпретацію: так і
	// vet, і checkptr не бачать «підозрілої» конвертації uintptr→Pointer.
	var memPtr unsafe.Pointer
	*(*uintptr)(unsafe.Pointer(&memPtr)) = pMem
	dst := unsafe.Slice((*uint16)(memPtr), len(utf16s))
	copy(dst, utf16s)
	procGlobalUnlock.Call(hMem)

	if r, _, _ := procSetClipboardData.Call(cfUnicodeText, hMem); r == 0 {
		return errors.New("не вдалось записати текст у буфер обміну")
	}
	owned = true
	return nil
}

// CopyToClipboard — метод, доступний фронтенду (call 'CopyToClipboard').
// Всі кнопки копіювання у лаунчері ведуть сюди замість WebView2-clipboard.
func (a *App) CopyToClipboard(text string) error {
	return setClipboardText(text)
}

// clipboardMaxChars — верхня межа довжини прочитаного тексту (UTF-16 символів).
// Нам потрібні лише короткі посилання-запрошення, тож 1M символів (2 МБ) —
// із запасом, а читання завжди обмежене.
const clipboardMaxChars = 1 << 20

var (
	procGetClipboardSeq = user32.NewProc("GetClipboardSequenceNumber")

	clipCacheMu   sync.Mutex
	clipCacheSeq  uintptr
	clipCacheText string
	clipCacheOK   bool
)

// getClipboardText читає текст (CF_UNICODETEXT) із системного буфера обміну.
// Порожній буфер / буфер без тексту — це НЕ помилка: повертаємо "", nil
// (інакше фронтенд показував би помилку щоразу, коли в буфері картинка).
//
// 0xC0000374 (див. розбір краш-дампів): колись тут викликався GlobalSize на
// хендлі з GetClipboardData — саме в ньому купа Windows виявляла пошкодження,
// а опитування буфера йде автоматично (старт +1.2 с, далі кожні 1.5 с).
// Тому: (1) GlobalSize прибрано — CF_UNICODETEXT за специфікацією завершений
// нулем, довжину шукаємо сканом із жорсткою межею; (2) буфер не відкривається
// взагалі, поки не змінився GetClipboardSequenceNumber — це на порядки
// зменшує кількість дотиків до купи.
func getClipboardText() (text string, err error) {
	defer func() {
		if r := recover(); r != nil {
			text, err = "", errors.New("не вдалось прочитати буфер обміну")
		}
	}()

	seq, _, _ := procGetClipboardSeq.Call()
	if seq != 0 {
		clipCacheMu.Lock()
		if clipCacheOK && clipCacheSeq == seq {
			t := clipCacheText
			clipCacheMu.Unlock()
			return t, nil
		}
		clipCacheMu.Unlock()
	}

	if r, _, _ := procIsFmtAvailable.Call(cfUnicodeText); r == 0 {
		storeClipCache(seq, "")
		return "", nil
	}
	if err := openClipboardRetrying(); err != nil {
		return "", err
	}
	defer procCloseClipboard.Call()

	hMem, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if hMem == 0 {
		return "", errors.New("не вдалось прочитати буфер обміну")
	}
	pMem, _, _ := procGlobalLock.Call(hMem)
	if pMem == 0 {
		return "", errors.New("не вдалось заблокувати пам'ять буфера обміну")
	}
	defer procGlobalUnlock.Call(hMem)

	var memPtr unsafe.Pointer
	*(*uintptr)(unsafe.Pointer(&memPtr)) = pMem
	n := 0
	for n < clipboardMaxChars && *(*uint16)(unsafe.Add(memPtr, n*2)) != 0 {
		n++
	}
	u16 := make([]uint16, n)
	for i := 0; i < n; i++ {
		u16[i] = *(*uint16)(unsafe.Add(memPtr, i*2))
	}
	out := string(utf16.Decode(u16))
	storeClipCache(seq, out)
	return out, nil
}

func storeClipCache(seq uintptr, text string) {
	if seq == 0 {
		return
	}
	clipCacheMu.Lock()
	clipCacheSeq, clipCacheText, clipCacheOK = seq, text, true
	clipCacheMu.Unlock()
}

// ClipboardText — метод для фронтенду: повертає текст із буфера обміну
// (порожній рядок, якщо там не текст або буфер недоступний). Потрібен, щоб
// лаунчер сам бачив посилання-запрошення в друзі, яке гравець скопіював
// з Discord/Telegram, і показав вікно «додати цього друга».
func (a *App) ClipboardText() string {
	// Безпечний режим (safe_mode.go): автоперевірка буфера вимкнена — саме
	// вона робила перший дотик до купи Windows на старті без жодної дії
	// користувача. Кнопки копіювання (CopyToClipboard) при цьому працюють.
	if safeModeActive() {
		return ""
	}
	s, err := getClipboardText()
	if err != nil {
		return ""
	}
	return s
}

// setClipboardImage кладе растрове зображення у буфер обміну Windows як
// CF_DIB (BITMAPINFOHEADER + пікселі) — формат, який приймає як «картинку»
// переважна більшість Windows-програм при Ctrl+V (Paint, Word, Discord,
// Telegram, поштові клієнти тощо), на відміну від PNG-кодування, яке
// підтримує значно менше застосунків.
//
// DIB зберігає рядки пікселів СНИЗУ ВГОРУ (bottom-up) і в порядку каналів
// BGR (не RGB), кожен рядок доповнюється нулями до кратності 4 байтам —
// це вимоги самого формату BITMAPINFOHEADER, без цього зображення вийде
// перевернутим/зі зсувом кольорів у програмі, що приймає вставку.
func setClipboardImage(img image.Image) error {
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w <= 0 || h <= 0 {
		return errors.New("порожнє зображення")
	}
	const bytesPerPixel = 3 // BGR, без альфа-каналу (CF_DIB класичний, максимальна сумісність)
	rowSize := (w*bytesPerPixel + 3) &^ 3
	pixelBytes := rowSize * h

	const headerSize = 40 // sizeof(BITMAPINFOHEADER)
	buf := make([]byte, headerSize+pixelBytes)

	putU32 := func(off int, v uint32) {
		buf[off] = byte(v)
		buf[off+1] = byte(v >> 8)
		buf[off+2] = byte(v >> 16)
		buf[off+3] = byte(v >> 24)
	}
	putU16 := func(off int, v uint16) {
		buf[off] = byte(v)
		buf[off+1] = byte(v >> 8)
	}
	putU32(0, uint32(headerSize))       // biSize
	putU32(4, uint32(w))                // biWidth
	putU32(8, uint32(h))                // biHeight (позитивне = bottom-up)
	putU16(12, 1)                       // biPlanes
	putU16(14, uint16(bytesPerPixel*8)) // biBitCount (24)
	putU32(16, 0)                       // biCompression = BI_RGB
	putU32(20, uint32(pixelBytes))      // biSizeImage
	// biXPelsPerMeter, biYPelsPerMeter, biClrUsed, biClrImportant лишаємо 0.

	for y := 0; y < h; y++ {
		// DIB зберігає рядки знизу вгору: рядок y картинки (згори) пишемо
		// у позицію (h-1-y) буфера пікселів.
		dstRow := headerSize + (h-1-y)*rowSize
		for x := 0; x < w; x++ {
			r, g, b, _ := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			o := dstRow + x*bytesPerPixel
			buf[o] = byte(b >> 8)
			buf[o+1] = byte(g >> 8)
			buf[o+2] = byte(r >> 8)
		}
	}

	if err := openClipboardRetrying(); err != nil {
		return err
	}
	defer procCloseClipboard.Call()

	if r, _, _ := procEmptyClipboard.Call(); r == 0 {
		return errors.New("не вдалось очистити буфер обміну")
	}

	hMem, _, _ := procGlobalAlloc.Call(gmemMoveable, uintptr(len(buf)))
	if hMem == 0 {
		return errors.New("не вдалось виділити пам'ять для буфера обміну")
	}
	owned := false
	defer func() {
		if !owned {
			procGlobalFree.Call(hMem)
		}
	}()

	pMem, _, _ := procGlobalLock.Call(hMem)
	if pMem == 0 {
		return errors.New("не вдалось заблокувати пам'ять буфера обміну")
	}
	var memPtr unsafe.Pointer
	*(*uintptr)(unsafe.Pointer(&memPtr)) = pMem
	dst := unsafe.Slice((*byte)(memPtr), len(buf))
	copy(dst, buf)
	procGlobalUnlock.Call(hMem)

	if r, _, _ := procSetClipboardData.Call(cfDIB, hMem); r == 0 {
		return errors.New("не вдалось записати зображення у буфер обміну")
	}
	owned = true
	return nil
}
