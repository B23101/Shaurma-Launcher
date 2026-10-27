package main

// bootstrap.go — Shell A: ОДНОРАЗОВИЙ мінімальний завантажувач, вбудований
// у setup.exe.
//
// СХЕМА (за ТЗ користувача «~»):
//   setup.exe ставить лише цей файл як ShaurmLauncher.exe (або
//   ShaurmLauncherBase.exe для base-редакції) — легкий, без Wails/WebView2,
//   без бізнес-логіки. При ПЕРШОМУ запуску він:
//     1. качає з CDN РЕАЛЬНИЙ shell (той самий файл, але повний Wails-shell,
//        який вміє завантажувати updater.dll/backend.dll/frontend і показувати
//        головне вікно) у <self>.new;
//     2. перейменовує СЕБЕ (активний процес) у <self>~ — Windows дозволяє
//        rename запущеного exe;
//     3. ставить .new на оригінальне місце;
//     4. перезапускає лаунчер з НОВОГО файлу і завершується. Далі — все як
//        раніше: реальний shell сам качає оновлювач і компоненти.
//
// НАВІЩО ТАК: setup.exe завжди маленький і ОДНАКОВИЙ (цей bootstrap не
// змінюється ніколи — лише якщо зміниться CDN/токен, а це -X ldflags, не
// код) → хеш setup стабільний → SmartScreen-репутація накопичується і не
// скидається між релізами. Реальний shell оновлюється через апдейтер
// (self-update через .new, див. shell/updater_phase.go).
//
// Збірка: CGO_ENABLED=0 (чистий syscall-білд), `go build` з -H windowsgui
// (без консольного вікна). Токен каналу вшивається через -X main.cdnToken.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// cdnBase — базова адреса CDN worker'а. Перевизначається на етапі збірки:
// -X main.cdnBase=...
var cdnBase = "https://shaurma-proxy.shaurmaofficial2210.workers.dev"

// cdnToken — токен каналу (shaurma/base), вшивається на етапі збірки:
// -X main.cdnToken=$TokenShaurma (або $TokenBase). Без токена worker віддає
// 401/403, тож bootstrap без нього не працює.
var cdnToken = ""

// shellOldSuffix — суфікс, під яким лишається цей (активний) exe після
// self-swap. Реальний shell при наступному старті прибирає його
// (той самий механізм, що в shell/updater_phase.go).
const shellOldSuffix = "~"

// minExeSize — мінімальний розмір «схожого на exe» файлу (MZ + розмір) —
// захист від того, що CDN віддав помилку/HTML замість бінарника.
const minExeSize = 1024 * 1024

// ── Маніфест (той самий контракт, що в апдейтера) ───────────────────────

type manifestComponent struct {
	Path   string `json:"path"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type remoteManifest struct {
	Version    string              `json:"version"`
	Components []manifestComponent `json:"components"`
}

// fetchManifest качає launcher-version.json з CDN і знаходить компонент,
// що відповідає імені цього exe (ShaurmLauncher.exe / ShaurmLauncherBase.exe
// — реальний shell на CDN має ТОЙ САМИЙ шлях, що й ми).
func fetchManifest(exeName string) (*manifestComponent, error) {
	manifestURL := cdnBase + "/launcher/launcher-version.json"
	data, err := httpGetLimited(manifestURL, 4<<20, 30*time.Second)
	if err != nil {
		return nil, fmt.Errorf("не вдалося отримати маніфест: %v", err)
	}
	// БЕЗПЕКА: bootstrap ЗАПУСКАЄ те, що скачав, а sha256 бере з маніфесту —
	// тож без перевірки підпису маніфесту хеш нічого не захищає. Ed25519-
	// підпис перевіряється над ТОЧНИМИ байтами, що прийшли (до BOM-обрізки).
	// Ключі вшиті при збірці (-X main.trustedKeys); без ключа (dev-збірка)
	// перевірка пропускається — release-збірка pack-manager без ключа не
	// створюється.
	if manifestSigEnabled() {
		sig, err := httpGetLimited(manifestURL+".sig", 4096, 30*time.Second)
		if err != nil {
			return nil, fmt.Errorf("підпис маніфесту недоступний: %v", err)
		}
		if err := verifyManifestSig(data, string(sig)); err != nil {
			return nil, err
		}
	}
	// build.ps1/інсталятор пишуть launcher-version.json з UTF-8 BOM, а
	// json.Unmarshal на BOM падає («пошкоджений маніфест» при першому
	// встановленні). Той самий TrimPrefix, що в updater.
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	var m remoteManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("пошкоджений маніфест: %v", err)
	}
	for _, c := range m.Components {
		if strings.EqualFold(filepath.Base(c.Path), exeName) {
			return &c, nil
		}
	}
	return nil, fmt.Errorf("маніфест не містить компонент %s (застарілий деплой на CDN?)", exeName)
}

// cdnHTTPClient — клієнт для CDN з токеном: редиректи лише на https і ТОЙ
// САМИЙ хост (інакше X-Shaurma-Token пішов би на сторонній сервер). Для
// loopback (тести/dev) http дозволено.
func cdnHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("забагато редиректів")
			}
			if req.URL.Host != via[0].URL.Host {
				return fmt.Errorf("редирект на інший хост %q відхилено", req.URL.Host)
			}
			if req.URL.Scheme != "https" && via[0].URL.Scheme != "http" {
				return fmt.Errorf("редирект на небезпечну схему %q відхилено", req.URL.Scheme)
			}
			return nil
		},
	}
}

// httpGetLimited — GET з токеном каналу і лімітом розміру відповіді.
func httpGetLimited(url string, limit int64, timeout time.Duration) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Shaurma-Token", cdnToken)
	req.Header.Set("User-Agent", "ShaurmaLauncher/bootstrap")
	resp, err := cdnHTTPClient(timeout).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d (перевір токен/канал збірки)", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// downloadStream качає компонент з CDN у dest (через .part + rename),
// оновлюючи прогрес у вікні. Повертає помилку при збої мережі/HTTP.
func downloadStream(comp *manifestComponent, dest string, onProgress func(pct int)) error {
	url := comp.URL
	if !strings.HasPrefix(url, "http") {
		url = cdnBase + url
	}
	// Токен віддаємо лише НАШОМУ CDN: абсолютний URL з маніфесту на
	// стороннього хоста — відмова.
	if !strings.HasPrefix(url, cdnBase+"/") {
		return fmt.Errorf("відхилено адресу поза довіреним CDN")
	}
	// БЕЗПЕКА: токен CDN і виконуваний файл — лише через HTTPS.
	if !strings.HasPrefix(strings.ToLower(url), "https://") {
		return fmt.Errorf("відхилено небезпечну адресу завантаження (потрібен https)")
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Shaurma-Token", cdnToken)
	req.Header.Set("User-Agent", "ShaurmaLauncher/bootstrap")
	resp, err := cdnHTTPClient(5 * time.Minute).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", comp.Path, resp.StatusCode)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	part := dest + ".part"
	os.Remove(part)
	out, err := os.Create(part)
	if err != nil {
		return err
	}

	total := resp.ContentLength
	if total <= 0 {
		total = comp.Size
	}
	var done int64
	h := sha256.New()
	buf := make([]byte, 128*1024)
	lastPct := -1
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if comp.Size > 0 && done+int64(n) > comp.Size {
				out.Close()
				os.Remove(part)
				return fmt.Errorf("%s: файл більший, ніж заявлено в маніфесті", comp.Path)
			}
			if _, werr := out.Write(buf[:n]); werr != nil {
				out.Close()
				os.Remove(part)
				return werr
			}
			h.Write(buf[:n])
			done += int64(n)
			if total > 0 {
				pct := int(done * 100 / total)
				if pct != lastPct {
					lastPct = pct
					onProgress(pct)
				}
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			out.Close()
			os.Remove(part)
			return rerr
		}
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(part)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(part)
		return err
	}

	// ЖОРСТКА перевірка цілісності: sha256 і розмір з маніфесту. Файл, що
	// не збігся (підміна на CDN/MitM/обрив), не лишається на диску і
	// ніколи не стає виконуваним.
	if comp.SHA256 == "" {
		os.Remove(part)
		return fmt.Errorf("%s: у маніфесті немає sha256 — встановлення відхилено", comp.Path)
	}
	if comp.Size > 0 && done != comp.Size {
		os.Remove(part)
		return fmt.Errorf("%s: розмір %d замість %d — перевірка цілісності не пройдена", comp.Path, done, comp.Size)
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, comp.SHA256) {
		os.Remove(part)
		return fmt.Errorf("%s: sha256 не збігається — перевірка цілісності не пройдена", comp.Path)
	}

	// Атомарна заміна з ретраями: антивірус сканує щойно записаний файл і
	// тримає його без FILE_SHARE_DELETE — перший rename часто падає.
	var rerr error
	for i := 0; i < 20; i++ {
		_ = os.Remove(dest)
		if rerr = os.Rename(part, dest); rerr == nil {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if rerr != nil {
		os.Remove(part)
		return fmt.Errorf("%s: не вдалося зберегти: %v", comp.Path, rerr)
	}
	onProgress(100)
	return nil
}

// looksLikeExecutable — саніті-чек: MZ-заголовок + розмір ≥ 1 МБ.
func looksLikeExecutable(path string) bool {
	fi, err := os.Stat(path)
	if err != nil || fi.Size() < minExeSize {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	var magic [2]byte
	if _, err := f.Read(magic[:]); err != nil {
		return false
	}
	return magic[0] == 'M' && magic[1] == 'Z'
}

// applySwap — перейменовує АКТИВНИЙ exe вбік (<self>~) і ставить новий
// (завантажений) на оригінальне місце. Повертає шлях, який тепер вказує на
// НОВИЙ файл (його і треба запустити через relaunch).
func applySwap() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(exe)
	base := filepath.Base(exe)
	newPath := filepath.Join(dir, base+".new")
	oldPath := filepath.Join(dir, base+shellOldSuffix)

	_ = os.Remove(oldPath)
	// Крок 1: активний exe -> ~ (rename запущеного exe Windows ДОЗВОЛЯЄ).
	if err := os.Rename(exe, oldPath); err != nil {
		return "", fmt.Errorf("не вдалося перейменувати активний exe: %v", err)
	}
	// Крок 2: новий exe на оригінальне місце. При невдачі — відкат.
	if err := os.Rename(newPath, exe); err != nil {
		_ = os.Rename(oldPath, exe)
		return "", fmt.Errorf("не вдалося поставити новий exe: %v", err)
	}
	return exe, nil
}

func main() {
	// Канал (shaurma/base) — за іменем цього exe (той самий евристик, що
	// в shell: currentChannel). Токен уже вшитий під конкретну редакцію.
	if cdnToken == "" {
		showFatal("Токен CDN не вшитий у збірку (build.ps1: -X main.cdnToken=...) — worker віддає 401/403")
		os.Exit(1)
	}

	exe, err := os.Executable()
	if err != nil {
		showFatal("Неможливо визначити шлях exe: " + err.Error())
		os.Exit(1)
	}
	exeName := filepath.Base(exe)
	dir := filepath.Dir(exe)

	// 1. Маніфест + компонент реального shell (той самий шлях, що й ми).
	comp, err := fetchManifest(exeName)
	if err != nil {
		showFatal("Не вдалося підготувати оновлення: " + err.Error())
		os.Exit(1)
	}

	// 2. Вікно прогресу (нативне, мінімальне).
	win := newProgressWindow("Shaurma Launcher", "Встановлення компонентів лаунчера...")
	defer win.destroy()

	// 3. Качаємо реальний shell у <self>.new.
	newPath := filepath.Join(dir, exeName+".new")
	if err := downloadStream(comp, newPath, func(pct int) { win.setProgress(pct) }); err != nil {
		os.Remove(newPath)
		showFatal("Не вдалося завантажити лаунчер: " + err.Error())
		os.Exit(1)
	}
	if !looksLikeExecutable(newPath) {
		os.Remove(newPath)
		showFatal("Завантажений файл не схожий на програму (CDN віддав не те?)")
		os.Exit(1)
	}

	// 4. Swap: себе -> ~, новий -> на місце.
	selfPath, err := applySwap()
	if err != nil {
		os.Remove(newPath)
		showFatal("Не вдалося оновити лаунчер: " + err.Error())
		os.Exit(1)
	}

	// 5. Перезапуск з НОВОГО файлу (selfPath тепер вказує на новий exe —
	//    Windows не оновлює шлях запуску при rename, тож на оригінальному
	//    шляху вже лежить новий файл).
	cmd := exec.Command(selfPath, os.Args[1:]...)
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		showFatal("Оновлення готове, але не вдалося перезапустити лаунчер: " + err.Error())
		os.Exit(1)
	}
	_ = cmd.Process.Release()
	os.Exit(0)
}

// ── Мінімальне нативне вікно прогресу (WinAPI, без Wails) ────────────────

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	comctl32 = syscall.NewLazyDLL("comctl32.dll")

	procRegisterClassExW     = user32.NewProc("RegisterClassExW")
	procCreateWindowExW      = user32.NewProc("CreateWindowExW")
	procDefWindowProcW       = user32.NewProc("DefWindowProcW")
	procPeekMessageW         = user32.NewProc("PeekMessageW")
	procTranslateMessage     = user32.NewProc("TranslateMessage")
	procDispatchMessageW     = user32.NewProc("DispatchMessageW")
	procShowWindow           = user32.NewProc("ShowWindow")
	procUpdateWindow         = user32.NewProc("UpdateWindow")
	procSendMessageW         = user32.NewProc("SendMessageW")
	procPostMessageW         = user32.NewProc("PostMessageW")
	procPostQuitMessage      = user32.NewProc("PostQuitMessage")
	procDestroyWindow        = user32.NewProc("DestroyWindow")
	procLoadCursorW          = user32.NewProc("LoadCursorW")
	procSetTextColor         = gdi32.NewProc("SetTextColor")
	procSetBkColor           = gdi32.NewProc("SetBkColor")
	procCreateSolidBrush     = gdi32.NewProc("CreateSolidBrush")
	procInitCommonControlsEx = comctl32.NewProc("InitCommonControlsEx")
)

const (
	wmApp            = 0x8000
	wmAppProgress    = wmApp + 1
	wmAppDone        = wmApp + 2
	wmDestroy        = 0x0002
	wmQuit           = 0x0012
	wmCtlColorStatic = 0x0138
	pbmSetRange32    = 0x0406
	pbmSetPos        = 0x0402
	wsOverlapped     = 0x00000000
	wsCaption        = 0x00C00000
	wsSysMenu        = 0x00080000
	wsMinimizeBox    = 0x00020000
	wsChild          = 0x40000000
	wsVisible        = 0x10000000
	ssCenter         = 0x00000001
	swShow           = 5
	cwUsedefault     = 0x80000000
	idcArrow         = 32512
	iccProgress      = 0x00000020
	pmRemove         = 0x0001
)

var (
	classCreated bool
	barHWND      uintptr
	bgBrush      uintptr
	progressMu   sync.Mutex
	lastProgress int
)

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ x, y int32 }
}

func wndProc(hwnd uintptr, m uint32, wParam, lParam uintptr) uintptr {
	switch m {
	case wmAppProgress:
		progressMu.Lock()
		pct := lastProgress
		progressMu.Unlock()
		procSendMessageW.Call(barHWND, pbmSetPos, uintptr(pct), 0)
		return 0
	case wmAppDone:
		procPostQuitMessage.Call(0)
		return 0
	case wmCtlColorStatic:
		// Текст статусу — світлим на темному фоні.
		procSetTextColor.Call(wParam, 0x00E8E8E8)
		procSetBkColor.Call(wParam, 0x00141418)
		if bgBrush != 0 {
			return bgBrush
		}
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(m), wParam, lParam)
	return r
}

type progressWindow struct{ hwnd uintptr }

// newProgressWindow створює і показує маленьке вікно зі статусом і
// прогрес-баром (темна тема, як у splash апдейтера).
func newProgressWindow(title, status string) *progressWindow {
	// InitCommonControlsEx — для msctls_progress32.
	icc := struct {
		dwSize uint32
		dwICC  uint32
	}{uint32(unsafe.Sizeof(struct {
		dwSize uint32
		dwICC  uint32
	}{})), iccProgress}
	procInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc)))

	hInst := syscall.NewLazyDLL("kernel32.dll").NewProc("GetModuleHandleW")
	inst, _, _ := hInst.Call(0)

	if !classCreated {
		bgBrush, _, _ = procCreateSolidBrush.Call(0x00141418) // RGB(24,20,20)
		clsName, _ := syscall.UTF16PtrFromString("ShBootstrapWnd")
		wcex := wndClassEx{
			cbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
			lpfnWndProc:   syscall.NewCallback(wndProc),
			hInstance:     inst,
			hCursor:       loadCursor(idcArrow),
			hbrBackground: bgBrush,
			lpszClassName: clsName,
		}
		procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wcex)))
		classCreated = true
	}

	clsName, _ := syscall.UTF16PtrFromString("ShBootstrapWnd")
	titlePtr, _ := syscall.UTF16PtrFromString(title)
	style := wsOverlapped | wsCaption | wsSysMenu | wsMinimizeBox
	hwnd, _, _ := procCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(clsName)),
		uintptr(unsafe.Pointer(titlePtr)),
		uintptr(style),
		cwUsedefault, cwUsedefault, 440, 132,
		0, 0, inst, 0)

	// Статус-текст.
	statusPtr, _ := syscall.UTF16PtrFromString(status)
	procCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(mustUTF16("STATIC"))),
		uintptr(unsafe.Pointer(statusPtr)),
		wsChild|wsVisible|ssCenter,
		16, 14, 408, 24,
		hwnd, 0, inst, 0)

	// Прогрес-бар.
	barHWND, _, _ = procCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(mustUTF16("msctls_progress32"))),
		0,
		wsChild|wsVisible,
		16, 52, 408, 24,
		hwnd, 0, inst, 0)
	if barHWND != 0 {
		procSendMessageW.Call(barHWND, pbmSetRange32, 0, 100)
		procSendMessageW.Call(barHWND, pbmSetPos, 0, 0)
	}

	procShowWindow.Call(hwnd, swShow)
	procUpdateWindow.Call(hwnd)
	return &progressWindow{hwnd: hwnd}
}

func (w *progressWindow) setProgress(pct int) {
	if w.hwnd == 0 {
		return
	}
	progressMu.Lock()
	lastProgress = pct
	progressMu.Unlock()
	procPostMessageW.Call(w.hwnd, wmAppProgress, uintptr(pct), 0)
	// Дренуємо наявні повідомлення НЕБЛОКУЮЧЕ (PeekMessage): GetMessageW
	// БЛОКУВАВ би тут назавжди — після обробки прогресу черга порожня, і
	// цикл качки (що викликав setProgress) ніколи б не продовжився.
	pumpMessages()
}

func (w *progressWindow) destroy() {
	if w.hwnd == 0 {
		return
	}
	procPostMessageW.Call(w.hwnd, wmAppDone, 0, 0)
	// Проженемо цикл повідомлень до WM_QUIT, щоб вікно коректно зникло.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !pumpMessages() {
			break
		}
	}
	procDestroyWindow.Call(w.hwnd)
	w.hwnd = 0
}

// pumpMessages обробляє НАЯВНІ повідомлення вікна без блокування:
// PeekMessageW(PM_REMOVE) повертає 0, коли черга порожня — тоді виходимо.
// Повертає false після WM_QUIT / wmAppDone (вікно закрите).
func pumpMessages() bool {
	var m msg
	for {
		ret, _, _ := procPeekMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0, pmRemove)
		if ret == 0 {
			return true // черга порожня — нічого більше обробляти
		}
		if m.message == wmQuit {
			return false
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		if m.message == wmAppDone {
			return false
		}
	}
}

func loadCursor(id uint32) uintptr {
	c, _, _ := procLoadCursorW.Call(0, uintptr(id))
	return c
}

func mustUTF16(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		return nil
	}
	return p
}

// showFatal — просте вікно помилки (MessageBox).
func showFatal(text string) {
	procMessageBoxW := user32.NewProc("MessageBoxW")
	title, _ := syscall.UTF16PtrFromString("Shaurma Launcher")
	msg, _ := syscall.UTF16PtrFromString(text)
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(msg)), uintptr(unsafe.Pointer(title)), 0x10 /* MB_ICONERROR */)
}
