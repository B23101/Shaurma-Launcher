package main

// assetfs.go — обслуговування фронтенду з ДИСКА (не з embed!). Це ключова
// зміна проти оригіналу (//go:embed all:frontend/dist): тепер фронтенд — це
// окремий компонент (frontend.zip у маніфесті), який installer розпаковує в
// installDir/frontend/. Тому shell.exe НЕ перекомпілюється, коли змінюється
// фронтенд. Порядок пошуку (для dev/зібраної структури):
//
//	1. <exeDir>/frontend/            — встановлена структура (frontend.zip)
//	2. <exeDir>/frontend/dist/       — dev: exe лежить поруч з launcher/
//
// Додатково віддається віртуальний файл updater-splash.html — мінімальний
// splash-екран апдейтера (Part 2; окремий updater-frontend.zip з'явиться
// пізніше за тим самим механізмом, що frontend.zip).

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// frontendRoot — тека, з якої обслуговується фронтенд (порожньо = немає).
//
// ВАЖЛИВО: тут навмисно використовується exeDir(), а НЕ
// filepath.Dir(os.Executable()) напряму. На Linux (AppImage) ці два шляхи
// різні: os.Executable() вказує всередину read-only /tmp/.mount_XXXXXX/,
// куди updater фізично не може розпакувати frontend.zip (та сама причина,
// що й у "read-only file system" при завантаженні backend.so/updater.so
// — див. коментар при exeDir() у exedir_linux.go). installDir, куди
// install.Apply() розпаковує компонент "frontend" (kind=archive), і тека,
// з якої тут читається фронтенд, мусять бути ОДНИМ і тим самим шляхом —
// інакше після першого ж оновлення фронтенду лаунчер продовжить
// показувати стару (або взагалі відсутню) версію, шукаючи її не там, куди
// вона реально лягла.
func frontendRoot() string {
	dir := exeDir()
	// Встановлена структура: installDir/frontend/**
	if st, err := os.Stat(filepath.Join(dir, "frontend")); err == nil && st.IsDir() {
		return filepath.Join(dir, "frontend")
	}
	// Dev-структура: exe у build/bin, dist у frontend/dist поруч.
	if st, err := os.Stat(filepath.Join(dir, "frontend", "dist")); err == nil && st.IsDir() {
		return filepath.Join(dir, "frontend", "dist")
	}
	return ""
}

// updaterSplashHTML — мінімальний splash апдейтера (обслуговується як
// віртуальний файл, коли повного updater-frontend немає). Прогрес іде
// подіями Wails updater:status / updater:progress — тими самими, що шле
// апдейтер (updater-backend/main.go: runUpdate) і bootstrap-качка
// (shell/bootstrap.go). Стиль — базовий лаунчерний: логотип, спінер,
// статус-рядок і прогрес-бар (старий стиль splash оновлювача).
const updaterSplashHTML = `<!doctype html>
<html lang="uk">
<head>
<meta charset="UTF-8">
<title>Shaurma Launcher — Оновлення</title>
<style>
  body { margin:0; background:#14181d; color:#e8e8e8; font-family:Segoe UI,Arial,sans-serif;
         display:flex; align-items:center; justify-content:center; height:100vh; overflow:hidden; }
  .box { text-align:center; padding:16px; width:100%; box-sizing:border-box; }
  .logo { font-size:22px; font-weight:700; margin-bottom:14px; letter-spacing:.5px; }
  .logo span { color:#ffa62b; }
  .status { font-size:13px; opacity:.85; margin-top:12px; min-height:18px; }
  .track { width:100%; height:8px; background:#ffffff14; border-radius:4px; margin-top:14px; overflow:hidden; }
  .bar { width:0%; height:100%; background:#ffa62b; border-radius:4px; transition:width .25s ease; }
  .pct { font-size:12px; opacity:.6; margin-top:6px; }
  .spin { width:22px; height:22px; border:3px solid #ffa62b33; border-top-color:#ffa62b;
          border-radius:50%; margin:0 auto; animation:s 1s linear infinite; }
  @keyframes s { to { transform:rotate(360deg); } }
  /* Правило «вікно неактивне → анімації на паузу»: коли splash-вікно
     поза фокусом (подія window:active:updater-splash від shell / fallback
     blur), спінер ставиться на паузу на поточному кадрі. */
  .spin.paused { animation-play-state: paused; }
  .hidden { display:none; }
</style>
</head>
<body>
  <div class="box">
    <div class="logo">Shaurma<span>Launcher</span></div>
    <div id="spin" class="spin"></div>
    <div class="status" id="st">Перевірка оновлень...</div>
    <div class="track"><div class="bar" id="bar"></div></div>
    <div class="pct" id="pct"></div>
  </div>
  <script>
    (function () {
      var st = document.getElementById('st');
      var bar = document.getElementById('bar');
      var pct = document.getElementById('pct');
      var spin = document.getElementById('spin');
      function setPct(p) {
        if (!bar) return;
        if (spin) spin.style.display = 'none';
        bar.style.width = Math.max(0, Math.min(100, p)) + '%';
        if (pct) pct.textContent = Math.round(p) + '%';
      }
      if (window.runtime && window.runtime.Events) {
        // Правило «вікно неактивне → анімації на паузу»: скопована подія
        // від shell (WM_ACTIVATE, window_exec.go шле window:active:<id> для
        // не-головних вікон). Fallback — blur/focus/visibilitychange на
        // випадок dev-режиму без нативного каналу.
        var setPaused = function (p) {
          if (spin) spin.classList.toggle('paused', !!p);
        };
        var onActive = function (active) { setPaused(!active); };
        if (window.runtime.Events.On) {
          window.runtime.Events.On('window:active:updater-splash', function (e) {
            if (e && e.data) onActive(e.data.active !== false);
          });
        }
        window.addEventListener('focus', function () { onActive(true); });
        window.addEventListener('blur', function () { onActive(false); });
        document.addEventListener('visibilitychange', function () {
          if (document.hidden) onActive(false); else onActive(true);
        });
        window.runtime.Events.On('updater:status', function (e) {
          var m = e.data && e.data.message;
          if (st && m) st.textContent = m;
          if (e.data && e.data.status === 'up_to_date') setPct(100);
          if (e.data && e.data.status === 'launching') setPct(100);
          if (e.data && e.data.status === 'installing') setPct(100);
        });
        window.runtime.Events.On('updater:progress', function (e) {
          var p = e.data;
          if (p && p.bytesTotal > 0) setPct(p.bytesDone / p.bytesTotal * 100);
        });
      }
    })();
  </script>
</body>
</html>`

// fatalErrorHTML — віртуальна сторінка помилки: показується, коли критичний
// компонент (backend.dll/backend.so) не вдалось завантажити. Раніше це був
// невидимий println — користувач бачив «лаунчер зник» без жодного пояснення.
//
// ВАЖЛИВО (кросплатформність): раніше назва компонента ("backend.dll") і
// назва інсталятора ("setup.exe") були захардкоджені прямо в HTML, тому на
// Linux користувач бачив текст про backend.dll/setup.exe, навіть якщо
// реально не завантажився backend.so і перевстановлювати треба інакше
// (немає setup.exe — є install.sh/пакет). showFatalError передає реальні
// component/detail рядком, тож сторінка тепер читає їх з query-параметрів
// URL (?component=...&detail=...&installer=...) і підставляє в текст через
// JS — сама сторінка лишається статичним asset (без //go:embed шаблонів чи
// текстової підстановки на боці Go).
const fatalErrorHTML = `<!doctype html>
<html lang="uk">
<head>
<meta charset="UTF-8">
<title>Shaurma Launcher — помилка</title>
<style>
  body { margin:0; background:#14181d; color:#e8e8e8; font-family:Segoe UI,Arial,sans-serif;
         display:flex; align-items:center; justify-content:center; height:100vh; }
  .box { max-width:480px; text-align:center; padding:24px; }
  .title { font-size:18px; font-weight:700; color:#ff6b6b; margin-bottom:10px; }
  .text { font-size:13px; line-height:1.5; opacity:.9; }
  .detail { margin-top:14px; font-size:11px; opacity:.6; word-break:break-word; white-space:pre-wrap; }
</style>
</head>
<body>
  <div class="box">
    <div class="title">Не вдалося запустити лаунчер</div>
    <div class="text" id="text"></div>
    <div class="detail" id="detail"></div>
  </div>
  <script>
    (function () {
      var q = new URLSearchParams(location.search);
      var component = q.get('component') || 'критичний компонент';
      var detail = q.get('detail') || '';
      var installer = q.get('installer') || 'встановлювач лаунчера';
      var el = document.getElementById('text');
      // Побудова через createElement/textContent (не innerHTML) — component
      // і detail приходять від showFatalError (Go), не від мережі чи
      // фронтенду, але про всяк випадок не варто інтерпретувати їх як HTML
      // (наприклад, детальний текст помилки може містити символи "<"/">").
      var b = document.createElement('b');
      b.textContent = component;
      el.appendChild(document.createTextNode('Компонент '));
      el.appendChild(b);
      el.appendChild(document.createTextNode(' не завантажився. Швидше за все, встановлення було перервано або файл пошкоджено.'));
      el.appendChild(document.createElement('br'));
      el.appendChild(document.createElement('br'));
      el.appendChild(document.createTextNode('Перевстановіть лаунчер заново (' + installer + ').'));
      if (detail) {
        document.getElementById('detail').textContent = detail;
      }
    })();
  </script>
</body>
</html>`

// updaterFrontendRoot — тека повного splash-фронтенду апдейтера
// (installDir/updater-frontend/**), якщо він встановлений (компонент
// updater-frontend.zip). Якщо його немає — використовується мінімальний
// вбудований splash (updaterSplashHTML).
//
// Як і у frontendRoot() — навмисно exeDir(), не os.Executable() напряму
// (той самий read-only-AppImage аргумент).
func updaterFrontendRoot() string {
	dir := filepath.Join(exeDir(), "updater-frontend")
	if st, err := os.Stat(dir); err == nil && st.IsDir() {
		return dir
	}
	return ""
}

// serveIndex — віддає index.html з указаної теки (або 404).
func serveIndex(w http.ResponseWriter, r *http.Request, root string) bool {
	return serveFile(w, r, root, "index.html")
}

// serveFile віддає файл з root, якщо він існує (не тека).
//
// НЕ використовуємо http.ServeFile: для шляху, що закінчується на
// "index.html", ServeFile робить 301-редирект на "./" (вважає index.html
// «дефолтною сторінкою» теки). У WebView2 це давало зайвий редирект, а
// тестованому handler-у — нестабільні відповіді. ServeContent віддає файл
// напряму з коректним Content-Type за розширенням.
func serveFile(w http.ResponseWriter, r *http.Request, root, rel string) bool {
	if root == "" {
		return false
	}
	full := filepath.Join(root, filepath.FromSlash(rel))
	f, err := os.Open(full)
	if err != nil {
		return false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		return false
	}
	w.Header().Set("Content-Type", mimeTypeFor(full))
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, filepath.Base(full), st.ModTime(), f)
	return true
}

// mimeTypeFor — простий Content-Type за розширенням (для asset-сервера).
func mimeTypeFor(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".js", ".mjs":
		return "application/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".ico":
		return "image/x-icon"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".ttf":
		return "font/ttf"
	case ".map":
		return "application/json"
	default:
		return "application/octet-stream"
	}
}

// assetHandler — disk-first asset-сервер для Wails (AssetOptions.Handler),
// з коренями, визначеними на диску (frontendRoot/updaterFrontendRoot).
//
// ВАЖЛИВО про порядок пошуку: основний фронтенд лаунчера (frontend/) і
// splash апдейтера (updater-frontend/) ОБИДВА містять index.html. Якщо
// шукати updater-frontend першим для всіх шляхів, головне вікно
// (URL "index.html") отримало б splash замість фронтенду — користувач
// бачив би «екран оновлення на 90%» замість головного екрана лаунчера.
// Тому:
//
//	updater-frontend віддається ЛИШЕ для віртуального шляху
//	"updater-splash.html" (його запитує тільки splash-вікно апдейтера);
//	для решти шляхів пріоритет у ОСНОВНОГО фронтенду (frontend/), і
//	лише якщо файлу там немає — фолбек на updater-frontend (асети
//	splash на кшталт fonts/, icon.png, які splash-сторінка тягне відносно
//	свого index.html).
func assetHandler() http.Handler {
	return assetHandlerWithRoots(frontendRoot(), updaterFrontendRoot())
}

// assetHandlerWithRoots — assetHandler з явно заданими теками (для тестів).
func assetHandlerWithRoots(feRoot, ueRoot string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		// Сторінка фатальної помилки (критичний компонент не завантажився,
		// деталі — у query-параметрах URL, див. showFatalError).
		if p == "fatal-error.html" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(fatalErrorHTML))
			return
		}
		// Splash апдейтера: повний updater-frontend, якщо встановлений,
		// інакше — мінімальний вбудований. Це ЄДИНИЙ шлях, який віддає
		// updater-frontend/index.html — головне вікно його не запитує.
		if p == "updater-splash.html" {
			if !serveIndex(w, r, ueRoot) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				_, _ = w.Write([]byte(updaterSplashHTML))
			}
			return
		}
		// Основний фронтенд лаунчера — ПЕРШИМ (index.html з frontend/
		// для головного вікна, assets/, console.html тощо).
		if serveFile(w, r, feRoot, p) {
			return
		}
		// Асети повного splash (fonts/, icon.png) — фолбек на updater-frontend.
		if serveFile(w, r, ueRoot, p) {
			return
		}
		http.NotFound(w, r)
	})
}
