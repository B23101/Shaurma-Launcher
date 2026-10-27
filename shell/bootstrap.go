package main

// bootstrap.go — перший запуск після чистого встановлення. setup.exe
// ставить ОДНОРАЗОВИЙ Shell A (bootstrap/main.go, чисто-Go), який при
// першому запуску підміняє СЕБЕ на цей реальний shell (Shell B: качає з
// CDN, rename вбік «~», .new на місце, рестарт) — див. bootstrap/.
// ЦЕЙ файл (Shell B) — повний Wails-shell; на диску немає ЖОДНОГО
// компонента лаунчера, тож він сам качає з CDN у splash-вікні з прогресом
// ВСІ компоненти (updater.dll, backend.dll, frontend.zip...), розпаковує
// архіви і записує локальний launcher-version.json. Після цього
// завантажується апдейтер — той бачить «все актуально» і одразу передає
// керування backend.dll.
//
// Це НЕ «лаунчер оновлює» — це початкова качка при встановленні, без якої
// не можна навіть запустити лаунчер. Після першого запуску ВСІМА оновленнями
// займається виключно апдейтер (включно з оновленням самого себе — качає
// нову версію як updater.dll.new, а shell при наступному старті
// перейменовує .new на місце).

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// cdnBase — базова адреса CDN worker'а. Та сама, що в апдейтера
// (updater-backend/internal/core/core.go: CDNBaseURL). Може бути
// перевизначена через -X shaurma-shell.cdnBase=... (dev/тести).
var cdnBase = "https://shaurma-proxy.shaurmaofficial2210.workers.dev"

// cdnToken — токен каналу (shaurma/base), вшивається на етапі збірки:
// -X shaurma-shell.cdnToken=$TokenShaurma (або $TokenBase для base).
// Без токена worker віддає 401, тож bootstrap без нього не працює.
var cdnToken = ""

// shellEmit — надсилає подію у Wails EventManager (той самий канал, що
// cbEmit для DLL). Використовується bootstrap-качкою: splash слухає
// updater:status / updater:progress (ті самі імена, що шле апдейтер).
func shellEmit(event, dataJSON string) {
	if app := application.Get(); app != nil {
		app.Event.Emit(event, json.RawMessage(dataJSON))
	}
}

// bootstrapComponent — компонент маніфесту launcher-version.json (той самий
// контракт, що manifest.Component в апдейтера).
type bootstrapComponent struct {
	Path      string `json:"path"`
	URL       string `json:"url"`
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
	Kind      string `json:"kind,omitempty"`
	ExtractTo string `json:"extractTo,omitempty"`
}

// bootstrapRemoteManifest — повний віддалений маніфест: після bootstrap
// зберігається як локальний launcher-version.json, щоб апдейтер при старті
// побачив «все встановлено» і не качав нічого зайвого.
type bootstrapRemoteManifest struct {
	Version      string               `json:"version"`
	ReleaseNotes string               `json:"releaseNotes"`
	Components   []bootstrapComponent `json:"components"`
}

// validateManifestArchitecture — перевірка, що віддалений маніфест описує
// НОВУ архітектуру (shell.exe + backend.dll + updater.dll + frontend.zip),
// а не стару монолітну (launcher.exe + shrm-updater.exe).
//
// НАВІЩО: у серпні 2026 на CDN випадково потрапив старий маніфест
// (помилковий деплой із застарілої staging-теки), і новий shell за його
// списком качав launcher.exe — старий моноліт, несумісний з DLL-
// архітектурою (замість backend.dll/frontend.zip у встановлення падав
// чужий exe). Цей захист — останній рубіж: якщо на CDN знову опиниться
// старий чи неповний маніфест, bootstrap відмовиться качати з чіткою
// помилкою, а не тихо самопошкодить установку.
func validateManifestArchitecture(m *bootstrapRemoteManifest) error {
	seen := make(map[string]bool, len(m.Components))
	for _, c := range m.Components {
		seen[strings.ToLower(filepath.Base(filepath.FromSlash(c.Path)))] = true
	}
	// Стара архітектура — категорична відмова: качати launcher.exe нема
	// чим запускати, він просто забруднить встановлення.
	for _, old := range []string{"launcher.exe", "shrm-updater.exe"} {
		if seen[old] {
			return fmt.Errorf("маніфест CDN описує СТАРУ архітектуру лаунчера (містить %s) — на CDN застарілий деплой. Оновлення відхилено; повідом адміністратора", old)
		}
	}
	// Нова архітектура обов'язково має три компоненти: без будь-якого з
	// них встановлення неповне (немає апдейтера / немає UI / немає фасаду).
	// Імена backend/updater платформозалежні (backend.dll/updater.dll на
	// Windows, backend.so/updater.so на Linux) — маніфест з CDN описує
	// компоненти саме тієї платформи, під яку його завантажив shell
	// (manifest.PlatformSegment на боці апдейтера/pack-manager розводить
	// Windows і Linux по різних шляхах CDN), тож тут звіряємо проти імен
	// СВОЄЇ поточної ОС, а не проти буквальних .dll.
	for _, r := range []string{backendLibraryName(), updaterLibraryName(), "frontend.zip"} {
		if !seen[r] {
			return fmt.Errorf("маніфест CDN неповний: немає компонента %s (застарілий чи битий деплой). Оновлення відхилено", r)
		}
	}
	return nil
}

// bootstrapUpdater — точка входу bootstrap-качки: качає маніфест з CDN і
// передає цикл завантаження в bootstrapAll (спільний з тестами).
func bootstrapUpdater() error {
	if cdnToken == "" {
		return fmt.Errorf("токен CDN не вшитий у збірку (build.ps1: -X shaurma-shell.cdnToken=...)")
	}

	// 1. Качаємо маніфест — звідти беремо список компонентів. Сегмент
	// платформи (platformSegment) розводить Windows і Linux по різних
	// шляхах на CDN: Windows лишається на корені /launcher/ (зворотна
	// сумісність), Linux іде під /launcher/linux/ — той самий контракт,
	// що PlatformSegment/ManifestURLPath у updater-backend.
	shellEmit("updater:status", `{"status":"checking","message":"Перевірка оновлень..."}`)
	manifestURL := cdnBase + "/launcher/" + platformSegment() + "launcher-version.json"
	data, err := fetchWithToken(manifestURL)
	if err != nil {
		return fmt.Errorf("не вдалося завантажити маніфест: %v", err)
	}
	// БЕЗПЕКА: підпис маніфесту (Ed25519, ключі вшиті при збірці). Ми
	// ЗАПУСКАЄМО те, що тут завантажимо (updater.dll/backend.dll), а sha256
	// береться з маніфесту — без підпису підмінений маніфест на CDN дав би
	// виконання чужого коду. Підписується те, що ПРИЙШЛО (точні байти).
	if manifestSigEnabled() {
		sig, serr := fetchWithToken(manifestURL + ".sig")
		if serr != nil {
			bootstrapLog(exeDir(), "МАНІФЕСТ ВІДХИЛЕНО: підпис недоступний: %v", serr)
			return fmt.Errorf("підпис маніфесту недоступний: %v", serr)
		}
		if verr := verifyManifestSig(data, string(sig)); verr != nil {
			bootstrapLog(exeDir(), "МАНІФЕСТ ВІДХИЛЕНО: %v", verr)
			return verr
		}
	} else {
		bootstrapLog(exeDir(), "УВАГА: у збірку не вшито ключ підпису маніфесту — підпис НЕ перевіряється (лише dev-збірка)")
	}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	var m bootstrapRemoteManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("пошкоджений маніфест з CDN: %v", err)
	}
	if len(m.Components) == 0 {
		return fmt.Errorf("маніфест CDN порожній (немає компонентів)")
	}
	// Захист від застарілого деплою: стара монолітна архітектура
	// (launcher.exe) несумісна з новим shell — не качаємо її взагалі.
	if err := validateManifestArchitecture(&m); err != nil {
		bootstrapLog(exeDir(), "МАНІФЕСТ ВІДХИЛЕНО: %v", err)
		return err
	}

	shellEmit("updater:status", `{"status":"downloading","message":"Завантаження компонентів лаунчера..."}`)
	return bootstrapAll(exeDir(), &m)
}

// bootstrapLog — діагностичний лог bootstrap-качки (installDir/bootstrap.log):
// пишемо мінімум — початок, кожен файл, помилки. Раніше всі помилки йшли
// лише в println (невидимий у GUI), і «лаунчер зник без пояснень».
func bootstrapLog(installDir, format string, args ...interface{}) {
	f, err := os.OpenFile(filepath.Join(installDir, "bootstrap.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, time.Now().Format("15:04:05")+" "+format+"\n", args...)
}

// bootstrapAll качає всі компоненти маніфесту в installDir і розпаковує
// архіви. Поточний exe пропускається (він уже є — це цей самий запущений
// файл; апдейтер його також не оновлює). Використовується і bootstrapUpdater,
// і тестами (бізнес-логіка — в одному місці).
func bootstrapAll(installDir string, m *bootstrapRemoteManifest) error {
	bootstrapLog(installDir, "bootstrap start: %d компонентів", len(m.Components))
	selfExe := ""
	if exe, err := os.Executable(); err == nil {
		selfExe = filepath.Base(exe)
	}

	for _, c := range m.Components {
		if filepath.Base(c.Path) == selfExe {
			bootstrapLog(installDir, "пропускаю %s (поточний exe)", c.Path)
			continue
		}
		if err := validateComponent(c); err != nil {
			bootstrapLog(installDir, "ПОМИЛКА %s: %v", c.Path, err)
			return err
		}
		bootstrapLog(installDir, "качаю %s з %s", c.Path, urlOf(c))
		// kind=archive (frontend.zip): сам архів кладемо на місце компонента
		// (щоб наступний Diff звірив його за sha256) І розпаковуємо у
		// ExtractTo. Перед розпакуванням архів має повністю лягти — качаємо
		// у .part і перейменовуємо, потім розпаковуємо.
		if err := downloadComponentRetry(urlOf(c), c, installDir); err != nil {
			bootstrapLog(installDir, "ПОМИЛКА %s: %v", c.Path, err)
			return err
		}
		bootstrapLog(installDir, "OK %s", c.Path)
		if c.Kind == "archive" {
			shellEmit("updater:status", fmt.Sprintf(`{"status":"installing","message":"Встановлення %s..."}`, c.Path))
			zipPath := filepath.Join(installDir, filepath.FromSlash(c.Path))
			if err := extractZipTo(zipPath, filepath.Join(installDir, filepath.FromSlash(c.ExtractTo))); err != nil {
				return fmt.Errorf("не вдалося розпакувати %s: %v", c.Path, err)
			}
			// Архів розпаковано, локальний маніфест буде записано нижче — сам
			// zip у теці встановлення більше не потрібен (Diff порівнює
			// маніфести, а не файли на диску), тож прибираємо його, щоб
			// installDir не смітився багатомегабайтними архівами фронтенду.
			_ = os.Remove(zipPath)
		}
	}

	// Записуємо локальний маніфест (той самий, що на CDN): апдейтер при
	// старті побачить, що все стоїть з правильним sha256, і не качатиме
	// нічого зайвого.
	if err := saveLocalManifest(installDir, m); err != nil {
		return fmt.Errorf("не вдалося записати маніфест: %v", err)
	}
	bootstrapLog(installDir, "bootstrap done")
	shellEmit("updater:status", `{"status":"installing","message":"Встановлення..."}`)
	return nil
}

// urlOf повертає абсолютний URL компонента (відносні url — до cdnBase).
func urlOf(c bootstrapComponent) string {
	if strings.HasPrefix(c.URL, "http") {
		return c.URL
	}
	return cdnBase + c.URL
}

// fetchWithToken — HTTP GET з токеном (X-Shaurma-Token) та UA апдейтера.
func fetchWithToken(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Shaurma-Token", cdnToken)
	req.Header.Set("User-Agent", "ShaurmaLauncher/updater")
	resp, err := cdnClient(60 * time.Second).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

// cdnClient — клієнт для CDN з токеном каналу: редиректи лише на ТОЙ САМИЙ
// хост і лише https (loopback для тестів/dev — http), щоб заголовок
// X-Shaurma-Token не потрапив на сторонній сервер.
func cdnClient(timeout time.Duration) *http.Client {
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

// safeRelPath — шлях лише всередині installDir (без абсолютних, диска, «..»).
// Маніфест — недовірений вхід; та сама логіка, що manifest.SafeRelPath в
// updater-backend.
func safeRelPath(p string) bool {
	if p == "" || strings.ContainsAny(p, ":\x00") || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) {
		return false
	}
	for _, seg := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." || seg == "." {
			return false
		}
	}
	return true
}

// validateComponent відхиляє компонент із небезпечним шляхом, чужою адресою
// або без нормального sha256/розміру — ДО будь-якого завантаження.
func validateComponent(c bootstrapComponent) error {
	if !safeRelPath(c.Path) {
		return fmt.Errorf("небезпечний шлях компонента %q", c.Path)
	}
	if c.Kind == "archive" && !safeRelPath(c.ExtractTo) {
		return fmt.Errorf("%s: небезпечна тека розпакування %q", c.Path, c.ExtractTo)
	}
	if len(c.SHA256) != 64 {
		return fmt.Errorf("%s: у маніфесті немає коректного sha256 — встановлення відхилено", c.Path)
	}
	if _, err := hex.DecodeString(c.SHA256); err != nil {
		return fmt.Errorf("%s: sha256 у маніфесті не hex", c.Path)
	}
	if c.Size <= 0 {
		return fmt.Errorf("%s: у маніфесті немає розміру", c.Path)
	}
	return checkDownloadURL(urlOf(c))
}

// checkDownloadURL: токен каналу і виконувані файли ходять лише на НАШ CDN
// (cdnBase) і лише по HTTPS (loopback — для тестів/dev).
func checkDownloadURL(u string) error {
	if !strings.HasPrefix(u, cdnBase+"/") {
		return fmt.Errorf("відхилено адресу поза довіреним CDN: %s", u)
	}
	pu, err := neturl.Parse(u)
	if err != nil {
		return err
	}
	if pu.Scheme != "https" {
		h := pu.Hostname()
		if !(pu.Scheme == "http" && (h == "127.0.0.1" || h == "localhost" || h == "::1")) {
			return fmt.Errorf("відхилено небезпечну адресу завантаження (потрібен https): %s", u)
		}
	}
	return nil
}

// bootstrapRetryDelay — пауза між спробами завантаження компонента (тести
// ставлять 0).
var bootstrapRetryDelay = 2 * time.Second

// downloadComponentRetry — до 3 спроб: обрив мережі чи тимчасово «битий»
// файл на CDN не мають одразу ламати встановлення. Після 3 невдач
// повертається остання помилка (встановлення НЕ продовжується з
// неперевіреним файлом).
func downloadComponentRetry(url string, comp bootstrapComponent, installDir string) error {
	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		if err = downloadComponent(url, comp, installDir); err == nil {
			return nil
		}
		bootstrapLog(installDir, "спроба %d/3 для %s: %v", attempt, comp.Path, err)
		if attempt < 3 {
			time.Sleep(bootstrapRetryDelay * time.Duration(attempt))
		}
	}
	return err
}

// downloadComponent качає компонент у installDir/<path> (через .part) і
// атомарно перейменовує. Прогрес шлеться подіями updater:progress
// (fileIndex/fileTotal по всіх файлах).
//
// БЕЗПЕКА (жорстка перевірка): sha256 і розмір з маніфесту звіряються ДО
// того, як файл стає на своє місце; хеш рахується на льоту по тих самих
// байтах, що пишуться на диск, а розмір обмежено заявленим у маніфесті.
// Файл, що не збігся, видаляється і НІКОЛИ не лишається в installDir —
// інакше підміна на CDN/MitM давала б виконання чужого коду (updater.dll і
// backend.dll завантажуються в процес). Раніше перевірка була «м'якою»
// (лише лог) — це було дірою. Захист від зупинки качки через застарілий
// деплой — на боці pack-manager: DeploySigned звіряє sha256 staging перед
// заливкою, а підписаний маніфест публікується останнім.
func downloadComponent(url string, comp bootstrapComponent, installDir string) error {
	if err := checkDownloadURL(url); err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Shaurma-Token", cdnToken)
	req.Header.Set("User-Agent", "ShaurmaLauncher/updater")
	resp, err := cdnClient(5 * time.Minute).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", comp.Path, resp.StatusCode)
	}

	dest := filepath.Join(installDir, filepath.FromSlash(comp.Path))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	part := dest + ".part"
	os.Remove(part)
	out, err := os.Create(part)
	if err != nil {
		return err
	}
	fail := func(e error) error {
		out.Close()
		os.Remove(part)
		return e
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
			if done+int64(n) > comp.Size {
				return fail(fmt.Errorf("%s: файл більший, ніж заявлено в маніфесті — перевірка цілісності не пройдена", comp.Path))
			}
			if _, werr := out.Write(buf[:n]); werr != nil {
				return fail(werr)
			}
			h.Write(buf[:n])
			done += int64(n)
			if total > 0 {
				pct := int(done * 100 / total)
				if pct != lastPct {
					lastPct = pct
					emitProgress(comp.Path, 0, 1, done, total)
				}
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return fail(rerr)
		}
	}
	if err := out.Sync(); err != nil {
		return fail(err)
	}
	if err := out.Close(); err != nil {
		os.Remove(part)
		return err
	}

	// ЖОРСТКА перевірка цілісності до перейменування.
	if done != comp.Size {
		os.Remove(part)
		return fmt.Errorf("%s: розмір %d замість %d — перевірка цілісності не пройдена", comp.Path, done, comp.Size)
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, comp.SHA256) {
		os.Remove(part)
		bootstrapLog(installDir, "ВІДХИЛЕНО %s: sha256 не збігається (очікувано %s, отримано %s)", comp.Path, comp.SHA256, got)
		return fmt.Errorf("%s: sha256 не збігається з підписаним маніфестом — перевірка цілісності не пройдена", comp.Path)
	}

	// Атомарна заміна з ретраями: антивірус сканує щойно записаний файл і
	// тримає його без FILE_SHARE_DELETE — перший rename часто падає.
	var mvErr error
	for i := 0; i < 20; i++ {
		_ = os.Remove(dest)
		if mvErr = os.Rename(part, dest); mvErr == nil {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if mvErr != nil {
		os.Remove(part)
		return fmt.Errorf("%s: не вдалося зберегти: %v", comp.Path, mvErr)
	}
	emitProgress(comp.Path, 0, 1, done, done)
	return nil
}

// extractZipTo розпаковує zip у destDir (перестворюючи теку), з захистом
// від zip-slip (шляхи типу ../ не виходять за межі) — та сама логіка, що
// в install.Apply апдейтера.
func extractZipTo(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	tmp := destDir + ".new"
	if err := os.RemoveAll(tmp); err != nil {
		return err
	}
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return err
	}

	if len(r.File) > 20000 {
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("archive has too many files (%d)", len(r.File))
	}
	var total int64
	const maxFile, maxTotal = int64(256 << 20), int64(1024 << 20) // захист від zip-бомби
	for _, f := range r.File {
		rel := filepath.Clean(filepath.FromSlash(f.Name))
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) ||
			filepath.VolumeName(rel) != "" || strings.Contains(f.Name, ":") {
			_ = os.RemoveAll(tmp)
			return fmt.Errorf("unsafe path in archive: %s", f.Name)
		}
		// Символічні посилання та спецфайли не розпаковуємо.
		if mode := f.Mode(); mode&os.ModeSymlink != 0 || (!mode.IsRegular() && !f.FileInfo().IsDir()) {
			_ = os.RemoveAll(tmp)
			return fmt.Errorf("unsupported entry type in archive: %s", f.Name)
		}
		target := filepath.Join(tmp, rel)
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				_ = os.RemoveAll(tmp)
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			_ = os.RemoveAll(tmp)
			return err
		}
		in, err := f.Open()
		if err != nil {
			_ = os.RemoveAll(tmp)
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			in.Close()
			_ = os.RemoveAll(tmp)
			return err
		}
		n, cerr := io.Copy(out, io.LimitReader(in, maxFile+1))
		out.Close()
		in.Close()
		if cerr != nil {
			_ = os.RemoveAll(tmp)
			return cerr
		}
		total += n
		if n > maxFile || total > maxTotal {
			_ = os.RemoveAll(tmp)
			return fmt.Errorf("archive exceeds size limit")
		}
	}

	if err := os.RemoveAll(destDir); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	if err := os.Rename(tmp, destDir); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	return nil
}

// saveLocalManifest атомарно записує launcher-version.json у installDir
// (через .tmp + rename — крах процесу посеред запису не лишить биту
// чернетку, яку апдейтер сприйняв би як «нічого не встановлено»).
func saveLocalManifest(installDir string, m *bootstrapRemoteManifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(installDir, "launcher-version.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// emitProgress — подія updater:progress у форматі, який розуміє splash
// (той самий, що шле апдейтер з download.Progress).
func emitProgress(fileName string, fileIndex, fileTotal int, bytesDone, bytesTotal int64) {
	payload := map[string]any{
		"fileIndex":  fileIndex,
		"fileTotal":  fileTotal,
		"fileName":   fileName,
		"bytesDone":  bytesDone,
		"bytesTotal": bytesTotal,
	}
	b, _ := json.Marshal(payload)
	shellEmit("updater:progress", string(b))
}
