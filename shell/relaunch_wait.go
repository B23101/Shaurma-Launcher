package main

// relaunch_wait.go — захист від гонки single-instance при САМОПЕРЕЗАПУСКУ.
//
// СИМПТОМ, ЯКИЙ ЦЕ ЛІКУЄ (випадкове «лаунчер запустився і зник», порожні
// логи/краші): усі сценарії самооновлення/перезапуску (applyPendingUpdates,
// bootstrapAndLoadUpdater, transitionToBackend) роблять так —
//
//	relaunchExe(exe)  // стартуємо новий процес
//	app.Quit()        // завершуємо поточний
//
// Проблема в тому, КОЛИ саме поточний процес звільняє single-instance lock.
// Wails тримає його у вигляді named-мутексу (application/single_instance_windows.go:
// CreateMutex "wails-app-<UniqueID>-sim"), а звільняє ЛИШЕ всередині
// App.cleanup(). А cleanup() спершу СИНХРОННО проганяє ВСІ shutdown-задачі
// (Application.cleanup → for _, t := range shutdownTasks { InvokeSync(t) }) —
// тобто наш shutdownShell → backend_shutdown, який робить saveWindowState,
// AutoSaveLauncherErrorLog (мережа), StopAllP2PHosts/StopP2PGuestService
// (tsnet) і т.д. — і аж ПОТІМ доходить до singleInstanceManager.cleanup().
//
// На повільному оточенні (VPS, слабкий диск/мережа, та й просто активні
// качки/P2P) цей shutdown займає секунди. Новий процес за цей час уже
// встигає дійти до application.New → newSingleInstanceManager → acquire —
// бачить зайнятий мутекс → notifyFirstInstance() (шле дані у ДОМЕРЛИЙ
// процес, який їх ігнорує) → os.Exit(ExitCode). Разом: старий процес
// вийшов, новий вийшов — лаунчера немає, у логах і крашах порожньо
// (os.Exit(0) — штатний вихід, жодної паніки). Це і є «випадкове
// зникнення»: воно залежить лише від таймінгу → «зловити неможливо».
//
// РІШЕННЯ (тут): старий процес передає новому СВІЙ PID (--wait-pid <pid>),
// а новий на старті ЧЕКАЄ, поки попередній процес реально завершиться,
// перш ніж створювати Wails-застосунок. Оскільки мутекс звільняється
// ДО виходу процесу, очікування саме його смерті гарантує вільний лок.
// Очікування обмежене таймаутом: якщо попередній процес застряг — не
// блокуємо старт назавжди (тоді просто повертаємось до старої поведінки).

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// waitPIDFlag — прапорець командного рядка, яким relaunchExe повідомляє
// новому процесу PID процесу-попередника. Внутрішній: користувач його не
// задає, інші парсери аргументів (parseLaunchArg/parsePackFilePathArg)
// його безпечно ігнорують (значення — число, не файл збірки).
const waitPIDFlag = "--wait-pid"

// relaunchWaitTimeout — максимум, скільки новий процес чекає завершення
// попереднього. Свідомо з запасом: це не «звичайне» очікування (зазвичай
// десятки-сотні мс), а межа проти вічного зависання, якщо старий процес
// застряг на shutdown-хуках.
const relaunchWaitTimeout = 60 * time.Second

// relaunchWaitPoll — інтервал опитування живості попереднього процесу.
const relaunchWaitPoll = 50 * time.Millisecond

// waitPIDFromArgs витягує PID з "--wait-pid <pid>" серед аргументів
// (0 — прапорця немає або значення некоректне). Беремо ОСТАННЄ входження:
// аргументи успадковуються ланцюжком перезапусків, і для нового процесу
// актуальний саме той PID, який додав безпосередній батько (див.
// stripWaitPIDArg).
func waitPIDFromArgs(args []string) int {
	found := 0
	for i := 0; i < len(args)-1; i++ {
		if args[i] != waitPIDFlag {
			continue
		}
		if pid, err := strconv.Atoi(strings.TrimSpace(args[i+1])); err == nil && pid > 0 {
			found = pid
		}
	}
	return found
}

// stripWaitPIDArg прибирає з аргументів усі пари "--wait-pid <pid>".
// Потрібно перед додаванням СВОГО PID у relaunchExe: інакше ланцюжок
// перезапусків накопичував би прапорці і новий процес чекав би не того
// попередника.
func stripWaitPIDArg(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == waitPIDFlag {
			i++ // пропускаємо прапорець і його значення
			continue
		}
		out = append(out, args[i])
	}
	return out
}

// relaunchArgs — аргументи для нового процесу при самоперезапуску
// (оновлення, self-update shell, ручний relaunchSelf).
//
// СИМПТОМ, ЯКИЙ ЦЕ ЛІКУЄ («після оновлення відкривається У ФОН, а не вікно»):
// раніше новий процес отримував os.Args[1:] як є. Для лаунчера, який ОС
// запустила автозапуском (`--autostart --background`), це означало, що навіть
// коли користувач уже відкрив вікно і натиснув «Оновити», нова копія
// стартувала СХОВАНОЮ: успадкований --background перекривав фактичний стан
// вікна (бо --background — це «не показувати вікно ЗАРАЗ», а не «ніколи»).
//
// Тепер --background визначає не історія запуску, а ПОТОЧНИЙ стан вікна
// (маркер window_hidden.flag, який shell ставить на hide і знімає на show):
//   - вікно видиме → прапорець знімається, новий процес покаже вікно;
//   - вікно в треї / фоновий старт → прапорець лишається, новий процес тихий.
func relaunchArgs(in []string, windowHidden bool, pid int) []string {
	args := restartArgs(stripWaitPIDArg(in), windowHidden)
	return append(args, waitPIDFlag, strconv.Itoa(pid))
}

// waitForPreviousInstance — якщо цей процес запущено з --wait-pid, чекає
// завершення вказаного процесу-попередника. Викликати ПЕРШИМ у main(), ДО
// applyPendingUpdates і ДО application.New (саме там створюється
// single-instance лок). No-op, якщо прапорця немає (звичайний запуск).
func waitForPreviousInstance() {
	pid := waitPIDFromArgs(os.Args)
	if pid == 0 {
		return
	}
	deadline := time.Now().Add(relaunchWaitTimeout)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return
		}
		time.Sleep(relaunchWaitPoll)
	}
	// Попередній процес не завершився за таймаут — не блокуємо старт
	// назавжди. Найгірший випадок — повертаємось до старого ризику гонки.
	bootstrapLog(exeDir(),
		"relaunch: попередній процес %d ще живий через %s — продовжую старт (можливий single-instance конфлікт)",
		pid, relaunchWaitTimeout)
}
