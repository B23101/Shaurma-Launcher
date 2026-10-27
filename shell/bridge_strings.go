package main

// bridge_strings.go — платформонезалежна межа роботи з C-рядками між
// shell і backend.so/dll: NUL-термінація на вихід (cstrPtr) і безпечне
// читання C-рядка за адресою на вхід (goStr). Обидві функції — чиста
// робота з пам'яттю Go/unsafe, однакова на Windows і Linux; логіка
// ЗАВАНТАЖЕННЯ модуля (LoadLibrary/dlopen) — у bridge_host_windows.go /
// bridge_host_linux.go.

import "unsafe"

// cstrPtr повертає NUL-завершену КОПІЮ рядка для передачі через C ABI
// (друге значення — тримач пам'яті, який викликач МУСИТЬ тримати живим
// через runtime.KeepAlive до завершення виклику, інакше GC може забрати
// буфер раніше).
//
// ВАЖЛИВО (причина «метод не знайдено»): ця межа потребує NUL-завершення
// ОБОВ'ЯЗКОВО — DLL/so читає рядки через C.GoString, який читає ДО
// першого '\0'. unsafe.StringData оригінального рядка НЕ гарантує
// завершального NUL: модуль прочитав би ім'я методу разом із сусіднім
// вмістом купи. Копія з гарантованим нулем усуває це повністю.
func cstrPtr(s string) (unsafe.Pointer, []byte) {
	if s == "" {
		return nil, nil
	}
	b := make([]byte, len(s)+1) // останній байт — 0 (make обнуляє)
	copy(b, s)
	return unsafe.Pointer(&b[0]), b
}

// goStr читає NUL-завершений C-рядок за адресою p. Байт-в-байт з recover:
// якщо вказівник битий/короткий — повертаємо те, що встигли прочитати,
// замість падіння всього процесу.
//
// ВАЖЛИВО (виправлення «Unterminated string in JSON at position 1048575»):
// ліміт читання був 1<<20 (рівно 1 МБ) — будь-яка відповідь модуля
// більша за 1 МБ (список модів, індекс Шаурма-збірок з іконками, резервні
// копії) обрізалась РІВНО на межі 1 МБ. Тепер до 64 МБ.
//
// //go:nocheckptr — свідомо: uintptr-арифметика (p+i) на адресі з
// модуля навмисна (межа процесу); без директиви checkptr (аналіз при
// -race) падав би на кожному читанні C-рядка.
//go:nocheckptr
func goStr(p uintptr) string {
	if p == 0 {
		return ""
	}
	// 64 МБ — верхня межа розміру JSON-відповіді через міст (байти).
	const maxReadBytes = 1 << 26
	b := make([]byte, 0, 256)
	defer func() { _ = recover() }()
	for i := 0; i < maxReadBytes; i++ {
		c := *(*byte)(unsafe.Pointer(p + uintptr(i)))
		if c == 0 {
			return string(b)
		}
		b = append(b, c)
	}
	return string(b)
}
