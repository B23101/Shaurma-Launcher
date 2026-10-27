//go:build !windows

package main

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os/exec"
	"strings"
)

// ── Буфер обміну на Linux (пара до clipboard_windows.go) ──────────────────
//
// НАВІЩО ЦЕЙ ФАЙЛ. На Windows усі кнопки копіювання ходять у власну
// Win32-реалізацію (clipboard_windows.go) з конкретної причини: WebView2
// там «копіює в ніщо» — тост «Скопійовано» є, а тексту в буфері ОС немає.
// На Linux інший рушій (WebKitGTK), але методи App викликаються з
// фронтенду через reflect (main.go: dispatch), тож відсутній метод — це
// не помилка компіляції, а РАНТАЙМ-помилка «метод не знайдено» на кожне
// натискання «Копіювати». Саме тому тут є і setClipboardText/
// setClipboardImage, і сам метод App.CopyToClipboard — рівно ті символи,
// яких бракувало Linux-збірці backend.so:
//
//	screenshots.go:606:9: undefined: setClipboardText
//	screenshots.go:629:9: undefined: setClipboardImage
//
// РЕАЛІЗАЦІЯ — через звичайні утиліти сесії, без нових Go-залежностей:
//   - Wayland: wl-copy (пакет wl-clipboard) — вміє і текст, і image/png;
//   - X11: xclip (пакет xclip) або xsel (пакет xsel) — текст, xclip ще й
//     image/png (xsel зображення не вміє).
//
// Якщо жодної з них немає — повертаємо ЗРОЗУМІЛУ помилку з назвою пакета, а
// не мовчки вдаємо успіх: інакше лаунчер показував би «Скопійовано», а
// буфер лишався порожнім — рівно та брехня, від якої цей код і з'явився на
// Windows. Фронтенд (wails.ts → CopyToClipboard) додатково має фолбек на
// navigator.clipboard, тож навіть без цих утиліт копіювання працює через
// WebKitGTK.

// clipboardTool — знайдена в системі утиліта для буфера обміну.
// imageArgs == nil означає, що утиліта не вміє класти зображення.
type clipboardTool struct {
	name      string
	textArgs  []string
	imageArgs []string
}

// clipboardCandidates — у порядку пріоритету: Wayland → X11.
func clipboardCandidates() []clipboardTool {
	return []clipboardTool{
		{
			name:      "wl-copy",
			textArgs:  []string{"--type", "text/plain"},
			imageArgs: []string{"--type", "image/png"},
		},
		{
			name:      "xclip",
			textArgs:  []string{"-selection", "clipboard", "-in"},
			imageArgs: []string{"-selection", "clipboard", "-t", "image/png", "-in"},
		},
		{
			name:     "xsel",
			textArgs: []string{"--clipboard", "--input"},
		},
	}
}

// findClipboardTool повертає першу доступну утиліту. needImage=true
// пропускає ті, що не вміють зображення (xsel).
func findClipboardTool(needImage bool) (clipboardTool, error) {
	for _, c := range clipboardCandidates() {
		if needImage && c.imageArgs == nil {
			continue
		}
		if p, err := exec.LookPath(c.name); err == nil {
			c.name = p
			return c, nil
		}
	}
	if needImage {
		return clipboardTool{}, errors.New(
			"копіювання зображення недоступне: постав пакет wl-clipboard (Wayland) або xclip (X11)")
	}
	return clipboardTool{}, errors.New(
		"копіювання недоступне: постав пакет wl-clipboard (Wayland) або xclip/xsel (X11)")
}

// runClipboardTool прогоняє утиліту, подаючи їй вміст через stdin.
// stdout/stderr збираємо разом: wl-copy/xclip пишуть причину невдачі туди,
// і без цього користувач бачив би голе «exit status 1».
func runClipboardTool(tool clipboardTool, args []string, in *bytes.Reader) error {
	cmd := exec.Command(tool.name, args...)
	cmd.Stdin = in
	if out, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return fmt.Errorf("не вдалось записати в буфер обміну (%s): %w", tool.name, err)
		}
		return fmt.Errorf("не вдалось записати в буфер обміну (%s): %w — %s", tool.name, err, msg)
	}
	return nil
}

// setClipboardText кладе текст у системний буфер обміну (Ctrl+V у будь-якій
// програмі сесії). Порожній рядок — валідний вміст: очищаємо буфер і
// повертаємо успіх, як і Windows-варіант.
func setClipboardText(text string) error {
	tool, err := findClipboardTool(false)
	if err != nil {
		return err
	}
	return runClipboardTool(tool, tool.textArgs, bytes.NewReader([]byte(text)))
}

// setClipboardImage кладе зображення у буфер обміну як PNG (image/png) —
// формат, який приймають і Wayland-, і X11-застосунки (браузер, Telegram,
// Discord, GIMP тощо). Windows-варіант кладе CF_DIB із тієї ж причини
// сумісності, але DIB — суто Windows-формат, тому тут PNG.
func setClipboardImage(img image.Image) error {
	if img == nil {
		return errors.New("порожнє зображення")
	}
	bounds := img.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return errors.New("порожнє зображення")
	}
	tool, err := findClipboardTool(true)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return fmt.Errorf("не вдалось закодувати зображення у PNG: %w", err)
	}
	return runClipboardTool(tool, tool.imageArgs, bytes.NewReader(buf.Bytes()))
}

// CopyToClipboard — метод, доступний фронтенду (call 'CopyToClipboard').
// Той самий контракт, що в clipboard_windows.go: сюди ведуть усі кнопки
// копіювання лаунчера, щоб не залежати від clipboard рушія WebView.
func (a *App) CopyToClipboard(text string) error {
	return setClipboardText(text)
}

// ── Читання буфера обміну (пара до Windows-варіанта) ─────────────────────
//
// Потрібне для посилань на друзів: лаунчер сам дивиться, чи не скопіював
// гравець посилання-запрошення (Discord/Telegram), і показує вікно «додати
// цього друга». Читаємо ті самі утиліти, що й пишемо, лише у режимі виводу.

type clipboardReader struct {
	name string
	args []string
}

func clipboardReaderCandidates() []clipboardReader {
	return []clipboardReader{
		{name: "wl-paste", args: []string{"--no-newline"}},
		{name: "xclip", args: []string{"-selection", "clipboard", "-out"}},
		{name: "xsel", args: []string{"--clipboard", "--output"}},
	}
}

// getClipboardText повертає текст із буфера обміну. Порожній буфер — це не
// помилка (повертаємо "", nil), як і у Windows-варіанті.
func getClipboardText() (string, error) {
	for _, c := range clipboardReaderCandidates() {
		p, err := exec.LookPath(c.name)
		if err != nil {
			continue
		}
		out, err := exec.Command(p, c.args...).Output()
		if err != nil {
			// xclip/xsel повертають помилку, якщо в буфері немає тексту —
			// це не привід показувати щось користувачу, просто «порожньо».
			return "", nil
		}
		return string(out), nil
	}
	return "", errors.New(
		"читання буфера обміну недоступне: постав пакет wl-clipboard (Wayland) або xclip/xsel (X11)")
}

// ClipboardText — метод для фронтенду (той самий контракт, що на Windows):
// текст із буфера обміну або порожній рядок.
func (a *App) ClipboardText() string {
	// Безпечний режим (safe_mode.go): без зовнішніх процесів і зайвої роботи
	// на старті. Кнопки копіювання працюють як завжди.
	if safeModeActive() {
		return ""
	}
	s, err := getClipboardText()
	if err != nil {
		return ""
	}
	return s
}
