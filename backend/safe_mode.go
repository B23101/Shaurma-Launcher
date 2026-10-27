package main

// safe_mode.go — БЕЗПЕЧНИЙ РЕЖИМ (SHAURMA_SAFE_MODE=1).
//
// Вмикається НЕ користувачем і не налаштуванням, а наглядом (shell/supervisor.go):
// якщо дочірній процес лаунчера вмер кілька разів підряд, не проживши хвилини,
// нагляд перезапускає його з цим прапорцем. Мета одна: прибрати з процесу ті
// нативні шляхи, які могли отруїти купу Windows (0xC0000374), щоб лаунчер усе
// одно піднявся з робочим UI.
//
// Що вимикається (і чому саме це):
//   - GPU-композитинг WebView2 — у shell/ui_flags.go (--disable-gpu): найбільша
//     нативна підсистема в нашому процесі;
//   - автоматичне опитування буфера обміну — тут, App.ClipboardText: єдине місце,
//     де наш код торкається NT-купи (GetClipboardData/GlobalLock) без дії
//     користувача;
//   - Discord RPC — app.go: overlapped named pipe + фонова горутина;
//   - прогрес у панелі завдань (COM ITaskbarList3) — taskbar_windows.go.
//
// Кнопки копіювання працюють як завжди: це явна дія користувача (CopyToClipboard),
// і без неї лаунчер став би незручним. У безпечному режимі вона лишається —
// ризик там той самий, але користувач сам його ініціює, а не процес «на старті».

import "os"

// safeModeEnv — те саме ім'я змінної, що supSafeEnv у shell/supervisor.go
// (shell і backend — різні модулі, спільна константа неможлива).
const safeModeEnv = "SHAURMA_SAFE_MODE"

// safeModeActive — чи процес запущено у безпечному режимі. Порожнє значення й
// «0» — звичайний режим (щоб `SHAURMA_SAFE_MODE=0` у скриптах не вмикав його).
func safeModeActive() bool {
	switch os.Getenv(safeModeEnv) {
	case "", "0", "false":
		return false
	}
	return true
}
