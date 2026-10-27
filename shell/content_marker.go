package main

// content_marker.go — механізм «маркер контенту» для WebView2-профілю.
//
// ПРОБЛЕМА. WebView2 кешує фронтенд (index.html + JS-бандли) у своєму
// профілі (%LOCALAPPDATA%\<InstallDir>\webview2\current\EBWebView). Коли
// backend.dll / frontend.zip оновлюються, а профіль лишається — WebView2
// показує СТАРИЙ закешований фронт: старі помилки JS (trim of undefined,
// each_key_duplicate, ...), старі бандли без packAsset-логіки → картинки
// не видно, сторінки контенту не вантажаться. Це проявлялось після
// перезбірки з ТІЄЮ САМОЮ версією (стара схема клала версію в шлях
// профілю webview2\v2.1.0 — версія не змінилась, тож профіль не
// перестворювався). Але стирати профіль на КОЖЕН запуск не можна — це
// знищує логіни/cookies користувача (Microsoft-сесія, налаштування).
//
// РІШЕННЯ. Один фіксований шлях профілю webview2\current (без хешів і
// без версійного номера в шляху — вимога ТЗ). Поруч лежить маленький
// файл-маркер webview2\.content-marker з sha256 від усіх контент-файлів,
// що впливають на UI:
//
//	launcher: frontend/index.html + backend.dll
//	updater:  updater-frontend/index.html + updater.dll
//
// (Wails v3 тримає ОДИН WebView2 user-data на весь процес, тож splash
// апдейтера і головне вікно ділять один профіль — маркер враховує обидва
// контент-набори; окрема тека webview2\updater\ для окремого процесу не
// потрібна, бо окремого WebView2-процесу в архітектурі немає).
//
// При старті shell.exe, ДО створення будь-якого вікна WebView2
// (syncWebViewProfile викликається з main() до application.New):
//   - маркер відсутній або НЕ збігається з поточним контентом → всю теку
//     webview2\current видаляємо рекурсивно, створюємо заново, пишемо
//     новий маркер;
//   - маркер збігається → НІЧОГО не чіпаємо: профіль лишається з усіма
//     кешами/логінами/cookies користувача.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// contentMarkerFileName — ім'я файлу-маркера (лежить у webview2\ поруч з current).
const contentMarkerFileName = ".content-marker"

// webviewContentSources — контент-файли, зміна яких інвалідує WebView2-кеш.
// Порядок фіксований — маркер детермінований.
func webviewContentSources() []string {
	dir := exeDir()
	return []string{
		filepath.Join(dir, "frontend", "index.html"),
		filepath.Join(dir, backendLibraryName()),
		filepath.Join(dir, "updater-frontend", "index.html"),
		filepath.Join(dir, updaterLibraryName()),
	}
}

// computeContentMarker — sha256 (hex) об'єднаного контенту всіх джерел.
// Відсутній файл позначається "missing:<path>" — поява файлу також змінює
// маркер, тож перший запуск після чистого встановлення (коли DLL/фронт ще
// не скачані) коректно створює профіль з нуля, а після бутстрапу — знову
// перестворює (контент з'явився).
func computeContentMarker() string {
	h := sha256.New()
	for _, p := range webviewContentSources() {
		b, err := os.ReadFile(p)
		if err != nil {
			fmt.Fprintf(h, "missing:%s\n", p)
			continue
		}
		sum := sha256.Sum256(b)
		fmt.Fprintf(h, "%s:%x\n", p, sum)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// migrateLegacyLocalAppDataProfile — одноразовий перенос старого
// WebView2-профілю з %LOCALAPPDATA%\<InstallDir>\webview2 у теку біля exe
// (новий дім профілю). Раніше профіль жив у %LOCALAPPDATA%, тепер — поруч
// з exe (webviewUserDataPath). Якщо в новому місці профілю ще немає, а в
// LOCALAPPDATA є — переносимо цілком (логіни/cookies не губляться при
// оновленні на версію з новим розташуванням). Викликається до створення
// будь-якого вікна WebView2, поки профіль ніхто не відкрив.
func migrateLegacyLocalAppDataProfile() {
	target := webviewUserDataPath()
	if _, err := os.Stat(target); err == nil {
		return // профіль біля exe вже є — нічого переносити
	}
	installName := installDirName()
	var legacy string
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		legacy = filepath.Join(local, installName, "webview2", "current")
	}
	if legacy == "" {
		return
	}
	if _, err := os.Stat(legacy); err != nil {
		return // старого профілю немає — нічого переносити
	}
	// Переносимо ПОВНУ теки current (включно з EBWebView/). Файли WebView2
	// не відкриті (нічого ще не створено), тож rename безпечний. Маркер
	// вмісту переїжджає разом — наступний syncWebViewProfile звірить його
	// з актуальним контентом і, якщо треба, перестворить профіль.
	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return
	}
	if err := os.Rename(legacy, target); err != nil {
		// rename між дисками (C: → D:) падає — тоді копіюємо.
		if err := copyTree(legacy, target); err != nil {
			return
		}
		_ = os.RemoveAll(legacy)
	}
}

// copyTree рекурсивно копіює теку (фолбек для rename при переносі
// профілю WebView2 з %LOCALAPPDATA% у теку exe на іншому диску).
func copyTree(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		from := filepath.Join(src, e.Name())
		to := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copyTree(from, to); err != nil {
				return err
			}
			continue
		}
		data, err := os.ReadFile(from)
		if err != nil {
			return err
		}
		if err := os.WriteFile(to, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// syncWebViewProfile — маркер-перевірка WebView2-профілю. Викликається з
// main() ДО application.New / до створення будь-якого вікна WebView2
// (видаляти теку, яку WebView2 вже відкрив, Windows не дасть).
//
// Додатково прибирає застарілі версіоновані профілі (webview2\v2.1.0 і
// т.п.) — пережитки колишньої схеми «версія в шляху»; вони лише з'їдали
// місце і більше ніколи не використовуються.
func syncWebViewProfile() {
	profile := webviewUserDataPath()
	markerPath := filepath.Join(filepath.Dir(profile), contentMarkerFileName)
	want := computeContentMarker()

	cleanupLegacyProfiles(filepath.Dir(profile))

	// Читаємо старий маркер (файлу може не бути — перший запуск).
	old := ""
	if b, err := os.ReadFile(markerPath); err == nil {
		old = string(b)
	}

	if old == want {
		return // контент не змінився — профіль НЕ чіпаємо
	}

	// Маркер відсутній або не збігається — контент реально змінився:
	// старий кеш WebView2 видаляємо ПОВНІСТЮ (ще нічого не відкрито).
	if err := os.RemoveAll(profile); err != nil {
		// Не змогли видалити (файл зайнятий) — наступного запуску
		// спробуємо знову; WebView2 сам перестворить теку за потреби.
		return
	}
	if err := os.MkdirAll(profile, 0o755); err != nil {
		return
	}
	_ = os.WriteFile(markerPath, []byte(want), 0o644)
}

// cleanupLegacyProfiles видаляє webview2\v<...> теки (колишня версіонована
// схема профілю), лишаючи лише current. Помилки ігноруються — це не
// критично для роботи, лише гігієна диска.
func cleanupLegacyProfiles(webviewRoot string) {
	entries, err := os.ReadDir(webviewRoot)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "current" {
			continue
		}
		if len(e.Name()) > 1 && e.Name()[0] == 'v' {
			_ = os.RemoveAll(filepath.Join(webviewRoot, e.Name()))
		}
	}
}
