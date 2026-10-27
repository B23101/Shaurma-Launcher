package main

// taskbar.go — спільний (платформонезалежний) контракт прогресу в панелі
// завдань. Реалізації: taskbar_windows.go (ITaskbarList3, реальний прогрес)
// й taskbar_other.go (no-op на не-Windows). app.go кличе updateTaskbar з
// цих станів; сама реалізація Windows мапить їх на TBPFLAG.

// taskbarState — стан прогрес-бара в таскбарі.
type taskbarState int

const (
	// TaskbarNormal — детермінований бар з percent (0..100).
	taskbarNormal taskbarState = iota
	// TaskbarIndeterminate — «заповнення біжить» (% ще невідомий).
	taskbarIndeterminate
	// TaskbarPaused — жовтий (користувач поставив на паузу).
	taskbarPaused
	// TaskbarError — червоний (завершилося помилкою).
	taskbarError
	// TaskbarNone — прибрати прогрес з кнопки.
	taskbarNone
)

// updateTaskbarProgress малює прогрес у панелі завдань. percent має
// значення лише для taskbarNormal. Викликається з recordSyncProgress на
// кожен тік (~300мс) — усередині дешево (кешований COM-інтерфейс).
