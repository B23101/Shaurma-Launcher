// Package core — головна логіка shrm-updater: перевірка оновлень,
// докачування, встановлення, запуск launcher.exe, health-check з
// авто-rollback якщо нова версія не змогла стартувати.
package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"shrm-updater-backend/internal/download"
	"shrm-updater-backend/internal/install"
	"shrm-updater-backend/internal/manifest"
	"shrm-updater-backend/internal/neterr"
	"shrm-updater-backend/internal/sigverify"
)

// CDNBaseURL — базова адреса worker'а. Той самий worker, що вже
// обслуговує /packs, /java і т.д. (shrm-cdn-worker/worker.js) — шлях
// launcher/launcher-version.json вже підтримується resolveR2Key без
// жодних змін на боці worker.
const CDNBaseURL = "https://shaurma-proxy.shaurmaofficial2210.workers.dev"

// launcherExeName — ім'я виконуваного файлу поточної редакції (shell).
// НОВА АРХІТЕКТУРА: shell компілюється двічі (ShaurmLauncher /
// ShaurmLauncherBase), тож ім'я залежить від каналу. Використовується
// лише для перевірки «чи встановлений лаунчер» — запуск shell як
// окремого процесу апдейтер більше не виконує (це робить shell сам через
// phase_complete → підвантаження backend.dll/backend.so).
//
// Розширення платформо-залежне (manifest.PlatformSegment підказує ОС):
// "ShaurmLauncher.exe" на Windows, просто "ShaurmLauncher" (без
// розширення — стандарт ELF-виконуваних файлів) на Linux.
func launcherExeName(ch Channel) string {
	name := "ShaurmLauncher"
	if ch == ChannelBase {
		name = "ShaurmLauncherBase"
	}
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

// tokenShaurma/tokenBase — токени CDN, вшиті В САМ БІНАРНИК на етапі
// збірки через -ldflags "-X", а НЕ прочитані з env/файлу під час
// роботи. Причина: shrm-updater.exe ставиться на комп'ютери всіх
// користувачів — токен обов'язково опиниться "в чиїхось руках" рано чи
// пізно (можна дістати strings.exe по бінарнику), це прийнятний рівень
// захисту (той самий, що й у worker.js: "усуває 99% автоматизованих
// сканерів", не криптографічну таємницю). Але порожні env-змінні,
// якщо забути їх встановити при інсталяції — гарантований 401 для
// КОЖНОГО користувача, а не для зловмисника. Вшивання на етапі build.ps1
// прибирає цю причину відмови повністю.
//
// build.ps1 викликає:
//
//	go build -ldflags "-X shrm-updater-backend/internal/core.tokenShaurma=... -X shrm-updater-backend/internal/core.tokenBase=..."
//
// Значення беруться з build.ps1 -TokenShaurma/-TokenBase (або з
// SHRM_BUILD_TOKEN_SHAURMA/SHRM_BUILD_TOKEN_BASE в оточенні, якщо
// прапорці не передані — див. build.ps1).
var (
	tokenShaurma string
	tokenBase    string
)

// Channel — канал оновлення, відповідає токену на worker'і
// (shrm-cdn-worker/worker.js: TOKEN_SHAURMA → 'shaurma', TOKEN_BASE →
// 'base'). Токен ВИЗНАЧАЄ канал на боці worker'а (resolveTokenType).
type Channel string

const (
	ChannelShaurma Channel = "shaurma"
	ChannelBase    Channel = "base"
)

// token повертає вшитий build-time токен для каналу.
func (c Channel) token() string {
	if c == ChannelBase {
		return tokenBase
	}
	return tokenShaurma
}

// Status — стан процесу оновлення, для прив'язки до UI (webview).
type Status int

const (
	StatusChecking Status = iota
	StatusUpToDate
	StatusUpdateAvailable
	StatusDownloading
	StatusInstalling
	StatusLaunching
	StatusError
)

type Callbacks struct {
	OnStatus   func(Status, string)
	OnProgress func(download.Progress)
	// OnUpdateAvailable викликається, коли знайдено оновлення, до старту
	// завантаження: віддає нову версію та сумарний розмір файлів, які
	// треба завантажити (для UI "Нова версія X.Y.Z, завантажити N МБ").
	OnUpdateAvailable func(version string, bytesTotal int64, fileCount int)
	Lang              string // "uk" | "en" — мова статус-повідомлень (з settings.json)
}

// launchPause — пауза між повідомленням "Запуск лаунчера..." і реальним
// стартом launcher.exe. Дає frontend-у час домальовувати прогрес-бар до
// 100%: користувач бачить повний бар, і ЛИШЕ після цього відкривається
// вікно лаунчера.
const launchPause = 900 * time.Millisecond

// minSplashShow — мінімальний час показу splash-екрана у "порожніх"
// сценаріях (немає оновлень / автооновлення вимкнене / CDN недоступний):
// раніше там була лише launchPause (900мс) — splash миготів як спалах і
// користувач не встигав зрозуміти, що відбулось. Тепер екран тримається
// щонайменше 3 секунди від старту перевірки (запити до CDN теж ідуть у
// цей час), потім phase_complete → backend. При РЕАЛЬНОМУ оновленні
// (качка/встановлення) splash і так висить довше — ця константа не чіпає
// той шлях.
const minSplashShow = 3 * time.Second

// holdSplash — тримає splash на екрані щонайменше min від моменту start
// (якщо з моменту start вже минуло більше — нічого не робить, без
// зайвих затримок). Використовується замість time.Sleep(launchPause) у
// випадках, коли оновлення не відбувається: користувач має бачити
// сплеш і прогрес-бар мінімально достатній час, а не спалах на півсекунди.
func holdSplash(start time.Time, min time.Duration) {
	elapsed := time.Since(start)
	if elapsed < min {
		time.Sleep(min - elapsed)
	}
}

// tr повертає статус-повідомлення потрібною мовою. Якщо мова не en —
// використовуємо український варіант за замовчуванням. args підставляються
// у шаблон (%s), як у fmt.Sprintf.
func (cb Callbacks) tr(uk, en string, args ...interface{}) string {
	s := uk
	if cb.Lang == "en" {
		s = en
	}
	if len(args) > 0 {
		return fmt.Sprintf(s, args...)
	}
	return s
}

// tr2 — те саме, що tr, але без varargs: підходить під сигнатуру
// neterr.TrFunc, яку neterr.Friendly приймає, щоб не імпортувати
// internal/core і не створювати цикл пакетів.
func (cb Callbacks) tr2(uk, en string) string {
	return cb.tr(uk, en)
}

// ── Мемні фрази замість сухих статусів ──────────────────────────────────
// Користувач просив: «оновлювач має писати мемні фрази, а не двічі
// „активно активно“». Тому замість одноманітних статусів крутимо веселі
// фрази по колу (для кожного статусу — свій набір). Усі пари (uk/en).
type memePhrase struct {
	uk, en string
}

var memes = map[Status][]memePhrase{
	StatusChecking: {
		{"Питаємо сервер, чи є щось новеньке…", "Asking the server for anything new…"},
		{"Свербимо CDN лаунчера…", "Tickling the launcher CDN…"},
		{"Шаримо по світу, чи вийшов апдейт…", "Checking if the update dropped…"},
	},
	StatusDownloading: {
		{"Тягнемо файли зі швидкістю світла…", "Downloading at the speed of light…"},
		{"Качаємо, як шаурму о 3 ночі…", "Downloading like shawarma at 3am…"},
		{"Ловимо байти щастя…", "Catching bytes of happiness…"},
		{"Завантажуємо оновлення, ще трохи…", "Downloading the update, almost there…"},
	},
	StatusInstalling: {
		{"Розкладаємо файли по поличках…", "Putting files on their shelves…"},
		{"Збираємо лаунчер, як лего…", "Assembling the launcher like LEGO…"},
		{"Прилаштовуємо нові деталі…", "Fitting in the new parts…"},
	},
	StatusLaunching: {
		{"Розігріваємо лаунчер…", "Warming up the launcher…"},
		{"Запускаємо, тримай кулачки…", "Launching — fingers crossed…"},
		{"Магія майже сталася…", "The magic is almost here…"},
	},
}

// meme повертає чергову мемну фразу для статусу (крутимо по колу).
func (cb Callbacks) meme(s Status, idx int) string {
	list := memes[s]
	if len(list) == 0 {
		return ""
	}
	p := list[idx%len(list)]
	if cb.Lang == "en" {
		return p.en
	}
	return p.uk
}

// Run — головний вхід. installDir — %LOCALAPPDATA%\ShaurmLauncher.
// edition визначає, який вшитий токен (і відповідно яка гілка
// launchers/shaurma чи launchers/base на R2) використовується для
// перевірки і завантаження оновлення. updateChannel ("stable" | "beta")
// — з якого файлу(ів) під тією гілкою брати версію: stable читає лише
// launcher-version.json; beta якає ОБИДВА (launcher-version.json і
// launcher-version-beta.json) і бере той, чия version вища — так beta-
// канал ніколи не «скочується» на старішу beta-версію, якщо остання
// стабільна вже випередила її номером.
//
// installUpdates — чи дозволено ВСТАНОВЛЮВАТИ оновлення (true — апдейтер
// запущено лаунчером --finalize, або автооновлення увімкнено). Коли false,
// віддалену новішу версію НЕ качаємо й НЕ встановлюємо, а лишаємо поточну
// — але все одно показуємо сплеш з прогрес-баром до 100% і, якщо
// launcher.exe узагалі немає (перший запуск/інсталяція), качаємо його:
// апдейтер виконує також роль інсталятора лаунчера, і вимкнене
// автооновлення його не стосується.
func Run(ctx context.Context, installDir string, edition Channel, updateChannel string, installUpdates bool, cb Callbacks) error {
	// Момент старту перевірки: у "порожніх" сценаріях (немає оновлень,
	// автооновлення вимкнене, CDN недоступний) splash має триматись
	// щонайменше minSplashShow — див. holdSplash нижче.
	start := time.Now()

	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return fmt.Errorf("%s: %w", cb.tr("створення", "creating")+" "+installDir, err)
	}

	token := edition.token()
	if token == "" {
		return fmt.Errorf("%s %q %s (build.ps1 -TokenShaurma/-TokenBase)", cb.tr("токен для каналу", "token for channel"), edition, cb.tr("не вшито в білд — перевір", "not embedded in build — check"))
	}

	localManifestPath := filepath.Join(installDir, "launcher-version.json")
	local, err := manifest.Load(localManifestPath)
	if err != nil {
		return fmt.Errorf("%s: %w", cb.tr("читання локального маніфесту", "reading local manifest"), err)
	}

	notify(cb, StatusChecking, cb.meme(StatusChecking, 0))
	remote, err := fetchBestRemoteManifest(ctx, token, updateChannel, cb)
	if err != nil {
		// Застарілий/битий маніфест на CDN — НЕ мережева помилка: качати з
		// нього не можна (validateRemoteArchitecture), але встановлена
		// версія при цьому справна — запускаємо її, щоб користувач бачив
		// справжню причину («застарілий деплой»), а не «немає мережі».
		if errors.Is(err, ErrStaleCDNManifest) {
			if exeExists(installDir, edition) {
				notify(cb, StatusUpToDate, cb.tr("На CDN застарілий маніфест лаунчера — запускаємо поточну версію", "Stale launcher manifest on CDN — launching the current version"))
				holdSplash(start, minSplashShow)
				return nil
			}
			return fmt.Errorf("%s: %w", cb.tr("на CDN застарілий маніфест лаунчера", "the CDN has a stale launcher manifest"), err)
		}
		// Немає мережі / CDN недоступний: якщо launcher.exe вже стоїть
		// на диску — просто запускаємо те, що є, а не блокуємо
		// користувача через тимчасову недоступність CDN. Якщо ж
		// launcher.exe взагалі немає (щойно встановлений з нуля, перед
		// першим запуском) — тут справді нічого вдіяти, повертаємо
		// помилку.
		if exeExists(installDir, edition) {
			notify(cb, StatusUpToDate, cb.tr("CDN недоступний, запускаємо поточну версію", "CDN unreachable, launching the current version"))
			holdSplash(start, minSplashShow)
			// У DLL-моделі «запуск лаунчера» = phase_complete → shell
			// підвантажує backend.dll. Повертаємо nil, викликач (App)
			// сам перейде до backend.
			return nil
		}
		return fmt.Errorf("%s: %w", cb.tr("немає мережі і немає встановленої версії", "no network and no installed version"), err)
	}

	// Що качати: manifest.Diff — версія лише тригер «є оновлення чи ні»,
	// а сам список — ЛИШЕ змінені компоненти (sha256/розмір порівнюються
	// з локальним маніфестом). Незмінені файли (напр. frontend.zip) при
	// бампі версії не перекачуються заново.
	//
	// SELF-UPDATE shell: НОВА МОДЕЛЬ (з 2026-08) — оновлювач качає і новий
	// shell.exe теж. Windows не дає перезаписати запущений exe, тому
	// install.Apply (IsLockedComponent) кладе його поруч як
	// ShaurmLauncher.exe.new, а сам shell при наступному старті (або одразу
	// після оновлення через transitionToBackend) застосовує його за схемою
	// «rename активного exe вбік (~) → .new на місце» — перейменувати
	// запущений exe Windows ДОЗВОЛЯЄ (на відміну від перезапису), тож це
	// безпечно. Після застосування shell перезапускає сам себе (relaunchSelf)
	// і працює вже нова версія. Більше НЕ фільтруємо shell зі списку качки.
	toDownload := manifest.Diff(local, remote)
	// У Flatpak shell оновлює `flatpak update` (лежить у read-only /app/bin),
	// тому його компонент зі списку качки прибираємо — див. flatpak.go.
	toDownload = dropFlatpakShellComponent(toDownload, edition)

	launcherInstalled := exeExists(installDir, edition)

	// Перемикання редакції в тій самій installDir (поставили shaurma поверх
	// base чи навпаки): якщо .exe ІНШОЇ редакції лежить тут же — прибираємо
	// його зараз, а не лишаємо два .exe і два ярлики в одній теці. JSON-дані
	// (launcher-version.json, launcher-location.json) і спільні компоненти
	// (backend.dll/.so, updater.dll/.so, frontend.zip/frontend/) не чіпаються
	// — вони й так спільні між редакціями за іменем файлу.
	cleanupOtherEditionExe(installDir, edition, cb)

	// Оновлення є за версією, але встановлювати його заборонено
	// (installUpdates=false, автооновлення вимкнене) — і лаунчер уже
	// стоїть на диску: запускаємо поточну версію БЕЗ завантаження.
	// Сплеш при цьому теж проходить: бар доходить до 100% (status
	// launching), користувач бачить звичний інтерфейс апдейтера.
	if len(toDownload) > 0 && !installUpdates && launcherInstalled {
		notify(cb, StatusUpdateAvailable, cb.tr("Доступне оновлення: %s (автооновлення вимкнено)", "Update available: %s (auto-update disabled)", remote.Version))
		notify(cb, StatusLaunching, cb.meme(StatusLaunching, 0))
		holdSplash(start, minSplashShow)
		return nil
	}

	// Додатково докачуємо компоненти, яких ФІЗИЧНО бракує на диску
	// (видалені вручну чи після краху). Diff йде за sha256 локального
	// маніфесту, тож при незміненій версії відсутній файл інакше НІКОЛИ
	// не відновився б (лаунчер лишався з розбитим UI).
	if installUpdates {
		toDownload = mergeUnique(toDownload, missingRequiredComponents(installDir, remote))
	}

	if len(toDownload) == 0 {
		notify(cb, StatusUpToDate, cb.tr("Лаунчер актуальний", "Launcher is up to date"))
		// Пауза до переходу: splash має триматись щонайменше minSplashShow
		// (користувач бачить «Лаунчер актуальний» + бар до 100%), перш ніж
		// shell перейде до backend.dll. Це було launchPause (900мс) — замало:
		// splash миготів як спалах при кожному запуску.
		holdSplash(start, minSplashShow)
		return nil
	}

	// Сумарний розмір оновлення — для UI ("Нова версія X, завантажити N МБ").
	var totalBytes int64
	for _, c := range toDownload {
		totalBytes += c.Size
	}
	if cb.OnUpdateAvailable != nil {
		cb.OnUpdateAvailable(remote.Version, totalBytes, len(toDownload))
	}

	notify(cb, StatusUpdateAvailable, cb.tr("Доступне оновлення: %s", "Update available: %s", remote.Version))
	notify(cb, StatusDownloading, cb.meme(StatusDownloading, 0))

	// Тимчасова тека завантажень — ВСЕРЕДИНІ теки встановлення лаунчера
	// (installDir\.update-tmp), а не у системному %TEMP%. ФІКС «не вдалось
	// встановити frontend.zip: rename C:\\...\\Temp\\shrm-update\\...»:
	// %TEMP% живе на C:, а лаунчер може стояти на іншому диску (D:) —
	// os.Rename МІЖ томами на Windows завжди падає (ERROR_NOT_SAME_DEVICE),
	// і заміна компонентів вмирала на першому ж rename. Той самий том =
	// rename атомарний і безпечний. Тека чиститься на початку (залишки від
	// обірваного оновлення) і після завершення (див. install.Apply).
	tmpDir := filepath.Join(installDir, ".update-tmp")
	_ = os.RemoveAll(tmpDir)
	defer os.RemoveAll(tmpDir)
	downloaded, err := download.Download(ctx, CDNBaseURL, token, tmpDir, toDownload, cb.Lang, func(p download.Progress) {
		if cb.OnProgress != nil {
			cb.OnProgress(p)
		}
	})
	if err != nil {
		// Немає інтернету / CDN пропав саме ПІД ЧАС качання оновлення
		// (а не на етапі перевірки версії — той шлях уже мав fallback
		// вище). Якщо стара робоча версія лежить на диску — не блокуємо
		// користувача помилкою «i/o timeout», а тихо запускаємо те, що
		// вже встановлено. Оновиться наступного разу, коли мережа буде.
		if launcherInstalled {
			notify(cb, StatusUpToDate, cb.tr(
				"Не вдалося завантажити оновлення (немає мережі) — запускаємо поточну версію",
				"Couldn't download the update (no network) — launching the current version",
			))
			holdSplash(start, minSplashShow)
			return nil
		}
		notify(cb, StatusError, err.Error())
		return err
	}

	notify(cb, StatusInstalling, cb.meme(StatusInstalling, 0))

	// НОВА АРХІТЕКТУРА: апдейтер — DLL усередині процесу shell'а, а не
	// окремий exe поряд з launcher.exe. backend.dll під час фази оновлення
	// НЕ завантажена (shell завантажує або updater.dll, або backend.dll),
	// тож install.Apply може замінити її безпосередньо — «зупиняти
	// запущений лаунчер» (stopRunningLauncher) більше не потрібно.

	if err := install.Apply(ctx, installDir, downloaded, remote, cb.Lang, func(msg string) {
		notify(cb, StatusInstalling, msg)
	}); err != nil {
		notify(cb, StatusError, err.Error())
		return err
	}

	notify(cb, StatusLaunching, cb.meme(StatusLaunching, 0))
	// Пауза до переходу (див. launchPause): бар має дійти до 100% перш,
	// ніж shell підвантажить backend.dll.
	time.Sleep(launchPause)
	return nil
}

// mergeUnique зливає два списки компонентів без дублікатів (по шляху).
// Diff (змінені за версією) і missingRequiredComponents (відсутні на
// диску) можуть перетинатись — однаковий компонент має качатись один раз.
func mergeUnique(a, b []manifest.Component) []manifest.Component {
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]manifest.Component, 0, len(a)+len(b))
	for _, c := range append(a, b...) {
		key := filepath.ToSlash(c.Path)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, c)
	}
	return out
}

// missingRequiredComponents повертає компоненти remote-маніфесту, яких на
// диску бракує (файл не існує, або для архіву — тека extractTo порожня чи
// відсутня). Виконуваний зараз shell (launcherExeName) виключається — його
// відсутність лікує bootstrap/інсталятор, а качка через .new тут
// безглузда (файлу немає що замінювати). Використовується для
// відновлення втрачених файлів при незміненій версії (див. Run).
func missingRequiredComponents(installDir string, remote *manifest.Manifest) []manifest.Component {
	shellNames := map[string]bool{
		strings.ToLower(launcherExeName(ChannelShaurma)): true,
		strings.ToLower(launcherExeName(ChannelBase)):    true,
	}
	var missing []manifest.Component
	for _, c := range remote.Components {
		base := filepath.Base(c.Path)
		if shellNames[strings.ToLower(base)] {
			continue
		}
		if c.Kind == "archive" {
			dir := filepath.Join(installDir, c.ExtractTo)
			entries, err := os.ReadDir(dir)
			if err == nil && len(entries) > 0 {
				continue
			}
		} else {
			if _, err := os.Stat(filepath.Join(installDir, c.Path)); err == nil {
				continue
			}
		}
		missing = append(missing, c)
	}
	return missing
}

// backendLibraryName — ім'я файлу backend-модуля для поточної ОС:
// "backend.dll" на Windows, "backend.so" на Linux. Дзеркало
// manifest.UpdaterLibraryName, тримається тут (не в manifest), бо
// потрібне лише для validateRemoteArchitecture.
func backendLibraryName() string {
	if runtime.GOOS == "windows" {
		return "backend.dll"
	}
	return "backend.so"
}

func notify(cb Callbacks, s Status, msg string) {
	if cb.OnStatus != nil {
		cb.OnStatus(s, msg)
	}
}

// ErrStaleCDNManifest — віддалений маніфест CDN належить СТАРІЙ монолітній
// архітектурі або неповний (validateRemoteArchitecture відхилив його).
// Окремий сентинел, щоб Run показав справжню причину («застарілий деплой
// на CDN»), а не хибне «CDN недоступний» — це різні ситуації: мережа жива,
// просто качати з цього маніфесту не можна.
var ErrStaleCDNManifest = errors.New("маніфест CDN застарілої/неповної архітектури")

// validateRemoteArchitecture — перевірка віддаленого маніфесту на
// відповідність НОВІЙ архітектурі (shell + backend + updater +
// frontend.zip, з платформо-залежними розширеннями: .exe/.dll на
// Windows, без розширення/.so на Linux). Маніфест старої монолітної
// епохи (launcher.exe + shrm-updater.exe) відхиляється: інакше Diff
// «побачив» би старі файли як змінені і качав би їх у нову установку,
// ламаючи її (реальний інцидент серпня 2026 — застарілий деплой на CDN
// перетворив оновлення на завантаження чужого launcher.exe).
//
// ВАЖЛИВО (виправлено разом з підтримкою Linux): required-список раніше
// був захардкожений на "backend.dll"/"updater.dll" — Linux-маніфест
// (backend.so/updater.so, шлях /launcher/linux/...) НІКОЛИ не мав жодного
// шансу пройти цю перевірку, кожен коректний Linux-деплой відхилявся б
// як "неповний". Список тепер будується з runtime.GOOS через
// manifest.UpdaterLibraryName і backendLibraryName нижче.
func (cb Callbacks) validateRemoteArchitecture(m *manifest.Manifest) error {
	seen := make(map[string]bool, len(m.Components))
	for _, c := range m.Components {
		seen[strings.ToLower(filepath.Base(filepath.FromSlash(c.Path)))] = true
	}
	oldNames := []string{"launcher.exe", "shrm-updater.exe"}
	if runtime.GOOS != "windows" {
		// Стара монолітна епоха існувала лише на Windows — на Linux
		// немає "застарілого" імені для відхилення, перевірка старих
		// назв тут суто заради симетрії з Windows-гілкою й нічого не
		// відхиляє на практиці.
		oldNames = nil
	}
	for _, old := range oldNames {
		if seen[old] {
			return fmt.Errorf("%w: %s %s %s", ErrStaleCDNManifest, cb.tr("маніфест CDN застарілої архітектури (містить", "CDN manifest is old architecture (contains"), old, cb.tr(") — оновлення відхилено", ") — update rejected"))
		}
	}
	required := []string{backendLibraryName(), manifest.UpdaterLibraryName(), "frontend.zip"}
	for _, r := range required {
		if !seen[r] {
			return fmt.Errorf("%w: %s %s %s", ErrStaleCDNManifest, cb.tr("маніфест CDN неповний (немає", "CDN manifest incomplete (missing"), r, cb.tr(") — оновлення відхилено", ") — update rejected"))
		}
	}
	return nil
}

// fetchRemoteManifest якає один конкретний файл маніфесту за відносним
// шляхом під /launcher/ (напр. "launcher-version.json" або
// "launcher-version-beta.json"). Шлях платформо-залежний
// (manifest.ManifestURLPath): на Windows лишається /launcher/<file> як і
// завжди (зворотна сумісність з уже задеплоєним CDN), на Linux іде через
// /launcher/linux/<file> — той самий бінарник апдейтера на різних ОС сам
// звертається за правильним шляхом, без окремого параметра конфігурації.
func fetchRemoteManifest(ctx context.Context, token, fileName string, cb Callbacks) (*manifest.Manifest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, CDNBaseURL+"/"+manifest.ManifestURLPath(fileName), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ShaurmaLauncher/updater")
	req.Header.Set("X-Shaurma-Token", token)

	client := cdnClient(20 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return nil, neterr.Friendly(err, cb.tr2)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %d", cb.tr("HTTP %d при отриманні "+fileName, "HTTP %d fetching "+fileName), resp.StatusCode)
	}

	var m manifest.Manifest
	// build.ps1/інсталятор пишуть launcher-version.json з UTF-8 BOM
	// (U+FEFF), а Go json.Decoder на BOM падає. Той самий TrimPrefix, що
	// в manifest.Load — без нього CDN-маніфест з BOM вважався б битим і
	// оновлення ніколи б не знаходились.
	// Маніфест — крихітний JSON; обмежуємо читання, щоб підроблений
	// сервер не змусив лаунчер з'їсти пам'ять.
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	// БЕЗПЕКА: Ed25519-підпис САМЕ ТИХ байтів, що прийшли (до BOM-обрізки
	// і розбору JSON). Якщо в збірку вшито ключ — непідписаний або
	// підроблений маніфест відхиляється і нічого з нього не качається.
	if sigverify.Enabled() {
		if err := verifyManifestSignature(ctx, token, fileName, data, cb); err != nil {
			return nil, err
		}
	}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", cb.tr("парсинг "+fileName, "parsing "+fileName), err)
	}
	// Недовірений вхід: шляхи/адреси/хеші компонентів мають бути безпечними
	// ще ДО Diff і завантаження (захист у глибину поверх підпису).
	if err := m.Validate(); err != nil {
		return nil, fmt.Errorf("%s %s: %w", cb.tr("небезпечний маніфест", "unsafe manifest"), fileName, err)
	}
	// Захист від застарілого/битого деплою на CDN: стара монолітна
	// архітектура (launcher.exe) несумісна з DLL-установкою — відхиляємо
	// маніфест ще до Diff, щоб жоден компонент з нього не качався.
	if err := cb.validateRemoteArchitecture(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

// fetchBestRemoteManifest реалізує вибір каналу:
//   - stable: лише launcher-version.json.
//   - beta: якає ОБИДВА (launcher-version.json і launcher-version-beta.json)
//     і бере той, чия version вища за семвером. launcher-version-beta.json
//     може бути відсутнім (окремого beta-релізу ще не публікували) — це
//     НЕ помилка, тоді просто лишається stable-версія.
//
// Помилка повертається тільки якщо НЕ вдалось отримати навіть stable —
// це єдиний обов'язковий файл.
func fetchBestRemoteManifest(ctx context.Context, token, updateChannel string, cb Callbacks) (*manifest.Manifest, error) {
	stable, err := fetchRemoteManifest(ctx, token, "launcher-version.json", cb)
	if err != nil {
		return nil, err
	}
	if updateChannel != "beta" {
		return stable, nil
	}

	beta, err := fetchRemoteManifest(ctx, token, "launcher-version-beta.json", cb)
	if err != nil {
		// beta-файл відсутній/недоступний — не блокуємо канал beta,
		// просто лишаємось на stable-версії, поки beta-реліз не з'явиться.
		return stable, nil
	}
	if manifest.CompareVersions(beta.Version, stable.Version) > 0 {
		return beta, nil
	}
	return stable, nil
}

func exeExists(installDir string, ch Channel) bool {
	_, err := os.Stat(filepath.Join(installDir, launcherExeName(ch)))
	return err == nil
}

// otherChannel повертає протилежний канал (shaurma ↔ base) — потрібно для
// виявлення "чужого" .exe в тій самій installDir при перемиканні редакції.
func otherChannel(ch Channel) Channel {
	if ch == ChannelBase {
		return ChannelShaurma
	}
	return ChannelBase
}

// cleanupOtherEditionExe прибирає .exe ІНШОЇ редакції (і його .previous/
// .new артефакти заміни), якщо він лишився в installDir — сценарій
// "поставили shaurma поверх base (чи навпаки) в ту саму теку".
//
// НЕ ЧІПАЄ: launcher-version.json, launcher-location.json чи будь-які інші
// JSON-дані користувача, backend.dll/backend.so, frontend.zip/frontend/,
// updater.dll/updater.so — усе це СПІЛЬНІ файли з однаковим ім'ям для
// обох редакцій (не редакційно-специфічні), тож видаляти чи чіпати їх
// тут не потрібно: install.Apply нижче й так замінить/поновить те, що
// реально змінилось за маніфестом. Видаляється ЛИШЕ .exe редакції, яку
// зараз витісняють — це єдиний файл, чиє саме ІМ'Я відрізняється між
// shaurma і base (ShaurmLauncher.exe vs ShaurmLauncherBase.exe).
func cleanupOtherEditionExe(installDir string, current Channel, cb Callbacks) {
	otherName := launcherExeName(otherChannel(current))
	otherPath := filepath.Join(installDir, otherName)
	if _, err := os.Stat(otherPath); err != nil {
		return // іншої редакції в цій теці немає — нічого прибирати
	}
	if cb.OnStatus != nil {
		cb.OnStatus(StatusInstalling, cb.tr(
			"Знайдено іншу редакцію лаунчера в цій теці — замінюємо…",
			"Found another launcher edition in this folder — replacing…",
		))
	}
	// Сам .exe і всі артефакти незавершеної заміни, що могли лишитись від
	// нього (install.Apply/bootstrap кладуть .previous/.new поруч з
	// файлом-джерелом за таким самим базовим іменем).
	_ = os.Remove(otherPath)
	_ = os.Remove(otherPath + install.BackupSuffix)
	_ = os.Remove(otherPath + install.PendingSuffix)
}

// Примітка про здоров'я після оновлення (НОВА АРХІТЕКТУРА): старий
// launchAndHealthCheck запускав launcher.exe окремим процесом і відкочував
// биту версію на .previous. Тепер «запуск лаунчера» = phase_complete →
// shell.exe підвантажує backend.dll у ТОЙ САМИЙ процес. Якщо нова версія
// backend.dll не завантажується/не ініціалізується — shell логує помилку
// (backend.dll load error), і користувач бачить splash-помилку; відкат
// виконується вручну через інсталятор або заміну файлів .previous.

// verifyManifestSignature завантажує <fileName>.sig і перевіряє підпис
// маніфесту довіреним публічним ключем.
func verifyManifestSignature(ctx context.Context, token, fileName string, data []byte, cb Callbacks) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, CDNBaseURL+"/"+manifest.ManifestURLPath(fileName)+".sig", nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "ShaurmaLauncher/updater")
	req.Header.Set("X-Shaurma-Token", token)
	resp, err := cdnClient(20 * time.Second).Do(req)
	if err != nil {
		return neterr.Friendly(err, cb.tr2)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %w", cb.tr("підпис маніфесту відсутній на сервері", "manifest signature is missing on the server"), sigverify.ErrBadSignature)
	}
	sig, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return err
	}
	if err := sigverify.Verify(data, string(sig)); err != nil {
		return fmt.Errorf("%s: %w", fileName, err)
	}
	return nil
}

// cdnClient — HTTP-клієнт для запитів до CDN з токеном каналу: редиректи
// лише на https і ТОЙ САМИЙ хост (інакше заголовок X-Shaurma-Token
// пішов би на сторонній сервер).
func cdnClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" {
				return fmt.Errorf("редирект на небезпечну схему %q відхилено", req.URL.Scheme)
			}
			if len(via) >= 5 {
				return fmt.Errorf("забагато редиректів")
			}
			if req.URL.Host != via[0].URL.Host {
				return fmt.Errorf("редирект на інший хост %q відхилено", req.URL.Host)
			}
			return nil
		},
	}
}
