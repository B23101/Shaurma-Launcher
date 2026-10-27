// preflight.go — «розумний аналізатор» перед запуском сервера.
//
// Той самий принцип, що й перевірки збірки перед «Грати» (GetLaunchWarnings
// + модалка на фронтенді), тільки для серверів: перелік того, що ЗАВАЖАЄ
// або ЗНЕЙМОВНО зламає старт — порт, RAM, Java, EULA, диск, минулий краш.
//
// ВАЖЛИВО про «ігнорувати надалі»: саме рішення, що показувати, а що —
// ні, приймає ШАР З НАЛАШТУВАННЯМИ (backend/servers_api.go), не цей пакет:
// тут лише чесний перелік фактів із прапорцями Ignorable/Fatal. Порт завжди
// показується — його неможливо «вимкнути», бо на зайнятому порту сервер
// фізично не стартує.
package servers

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"shaurma-launcher-backend/internal/sysinfo"
)

// Типи попереджень (kind) — за ними живуть перемикачі в налаштуваннях.
const (
	WarnKindPort  = "port"  // порт зайнятий (завжди показується)
	WarnKindRAM   = "ram"   // мало вільної оперативки
	WarnKindJava  = "java"  // Java не та / не встановлена
	WarnKindEULA  = "eula"  // EULA не прийнято (блокує старт)
	WarnKindDisk  = "disk"  // мало місця на диску
	WarnKindCore  = "core"  // ядра ще немає (довантажиться при старті)
	WarnKindCrash = "crash" // попередній запуск завершився аварійно
)

// ServerWarning — одне попередження аналізатора.
type ServerWarning struct {
	// Kind — машинний тип (порт/рам/java/eula/disk/core/crash), за нього
	// ж «не показувати знову» у налаштуваннях лаунчера.
	Kind string `json:"kind"`
	// Level: "error" (старту не буде) | "warn" (запуститься, але ризик) |
	// "info" (просто до відома).
	Level string `json:"level"`
	// Message — готове людське повідомлення українською.
	Message string `json:"message"`
	// Hint — що з цим робити (опційно).
	Hint string `json:"hint,omitempty"`
	// Fatal — без цього старту не буде (порту/файлам не «повіримо на слово»).
	Fatal bool `json:"fatal"`
	// Ignorable — чи можна вимкнути ЦЕЙ тип у налаштуваннях лаунчера.
	// Порт і EULA — ні: це фізичні блокери.
	Ignorable bool `json:"ignorable"`
}

// PreflightResult — результат аналізатора.
type PreflightResult struct {
	Warnings []ServerWarning `json:"warnings"`
	// CanStart — чи ВЗАГАЛІ є сенс тиснути «Старт» (немає fatal-блокерів).
	CanStart bool `json:"canStart"`
	// CheckedAt — коли виконувалась перевірка (для діагностики в UI).
	CheckedAt int64 `json:"checkedAt"`
}

// Preflight аналізує сервер ПЕРЕД запуском і повертає перелік проблем.
// Викликається з UI одразу перед StartServer; нічого не змінює і не блокує
// сам старт (рішення — за людиною, яка бачить модалку).
func (a *API) Preflight(id string) (PreflightResult, error) {
	srv, err := a.store.Get(id)
	if err != nil {
		return PreflightResult{}, err
	}
	res := PreflightResult{Warnings: []ServerWarning{}, CanStart: true, CheckedAt: NowMs()}
	add := func(w ServerWarning) {
		if w.Fatal {
			res.CanStart = false
		}
		res.Warnings = append(res.Warnings, w)
	}

	running := a.manager.IsRunning(id)

	// ── 1. ПОРТ ────────────────────────────────────────────────────────
	// Найдорожча й найчастіша помилка: «порт зайнятий» (чи навпаки —
	// хибне «вільно»). Запущений сервер уже тримає свій порт — тоді
	// перевірку не робимо (він і так працює).
	if !running {
		port := a.effectiveServerPort(srv)
		if check := CheckPortFree(port, a.store.LoadAll(), id, a.manager.IsRunning); !check.Free {
			holder := check.Holder
			if holder == "" {
				holder = "інший процес"
			}
			add(ServerWarning{
				Kind:      WarnKindPort,
				Level:     "error",
				Message:   fmt.Sprintf("Порт %d зайнятий: %s", port, holder),
				Hint:      "Зупиніть процес, що тримає порт, або змініть server-port у вкладці «Властивості».",
				Fatal:     true,
				Ignorable: false, // порт НІКОЛИ не ігнорується
			})
		}
	}

	// ── 2. RAM ─────────────────────────────────────────────────────────
	if !running {
		ramCheck := CheckRAM(srv.RAMMB, a.getUsedRAM())
		if !ramCheck.Ok {
			add(ServerWarning{
				Kind:      WarnKindRAM,
				Level:     "warn",
				Message:   fmt.Sprintf("Мало вільної оперативки: серверу потрібно %d МБ, доступно %d МБ", srv.RAMMB, ramCheck.Available),
				Hint:      "Можливі крахи або зависання світу. Зменшіть RAM сервера або закрийте інші програми.",
				Fatal:     false,
				Ignorable: true,
			})
		}
	}

	// ── 3. JAVA ────────────────────────────────────────────────────────
	// Рекомендована версія за MC-версією: розбіжність — типова причина
	// «UnsupportedClassVersion» / «class file version does not match».
	if srv.MCVersion != "" {
		if rec := JavaMajorForVersion(srv.MCVersion); rec > 0 && srv.JavaMajor > 0 && rec != srv.JavaMajor {
			// Старша Java зазвичай сумісна (Java 21 запускає те, що й 17),
			// небезпечною є ЛИШЕ старша за рекомендовану.
			if srv.JavaMajor < rec {
				add(ServerWarning{
					Kind:      WarnKindJava,
					Level:     "warn",
					Message:   fmt.Sprintf("Java %d, а для Minecraft %s рекомендована Java %d", srv.JavaMajor, srv.MCVersion, rec),
					Hint:      "Стара Java може не запустити це ядро. Змініть версію Java у налаштуваннях сервера.",
					Fatal:     false,
					Ignorable: true,
				})
			}
		}
	}
	if path, _ := FindJavaPath(srv.JavaMajor); path == "" {
		add(ServerWarning{
			Kind:      WarnKindJava,
			Level:     "info",
			Message:   fmt.Sprintf("Java %d ще не встановлена — лаунчер завантажить її перед стартом", srv.JavaMajor),
			Fatal:     false,
			Ignorable: false,
		})
	}

	// ── 4. EULA ────────────────────────────────────────────────────────
	if srv.Core != CoreVelocity {
		eulaPath := filepath.Join(srv.Dir, "eula.txt")
		ok := false
		if data, err := os.ReadFile(eulaPath); err == nil {
			ok = containsEULAtrue(string(data))
		}
		if !ok {
			add(ServerWarning{
				Kind:      WarnKindEULA,
				Level:     "error",
				Message:   "EULA Minecraft не прийнято (eula.txt)",
				Hint:      "Відкрийте вкладку «Файли» або створіть eula.txt з рядком eula=true.",
				Fatal:     true,
				Ignorable: false,
			})
		}
	}

	// ── 5. ДИСК ────────────────────────────────────────────────────────
	// Світ + логи + бекапи можуть з'їсти гігабайти: попереджуємо ЗАЗДАЛЕГІДЬ.
	if !running {
		if free := sysinfo.Collect(srv.Dir).FreeDiskMB; free > 0 && free < 2048 {
			add(ServerWarning{
				Kind:      WarnKindDisk,
				Level:     "warn",
				Message:   fmt.Sprintf("На диску залишилось лише %d МБ вільного місця", free),
				Hint:      "Світ і логи пишуться на цей диск — при нестачі місця сервер може впасти.",
				Fatal:     false,
				Ignorable: true,
			})
		}
	}

	// ── 6. ЯДРО ────────────────────────────────────────────────────────
	if !CoreInstalled(srv) {
		add(ServerWarning{
			Kind:      WarnKindCore,
			Level:     "info",
			Message:   fmt.Sprintf("Ядро %s ще не встановлено — лаунчер завантажить його перед першим стартом", CoreLabel(srv.Core)),
			Fatal:     false,
			Ignorable: false,
		})
	}

	// ── 7. МИНУЛИЙ КРАШ ────────────────────────────────────────────────
	if st := a.manager.GetStatus(id); st.Status == "crashed" || st.Status == "error" {
		msg := "Попередній запуск цього сервера завершився аварійно"
		if st.LastError != "" {
			msg += ": " + st.LastError
		}
		add(ServerWarning{
			Kind:      WarnKindCrash,
			Level:     "warn",
			Message:   msg,
			Hint:      "Відкрийте консоль — там останні рядки перед падінням.",
			Fatal:     false,
			Ignorable: true,
		})
	}

	return res, nil
}

// containsEULAtrue — чи в файлі є рядок eula=true (з пробілами навколо =).
func containsEULAtrue(data string) bool {
	for _, line := range strings.Split(data, "\n") {
		if strings.TrimSpace(line) == "eula=true" {
			return true
		}
	}
	return false
}
