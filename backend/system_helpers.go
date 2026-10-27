package main

import (
	"shaurma-launcher-backend/internal/sysinfo"
)

// osVersion повертає людську назву ОС (напр. "Windows 11") для логів
// старту. Повертає порожній рядок, якщо визначити не вдалось — викликач
// (ServiceStartup) пише лише runtime.GOOS.
func osVersion() string {
	return sysinfo.Collect("").OS
}

// totalMemoryMiB повертає загальну фізичну оперативну пам'ять системи
// (МБ) або 0, якщо визначити не вдалось.
func totalMemoryMiB() int64 {
	return sysinfo.Collect("").TotalRAMMB
}

// autoGameRAM повертає РЕКОМЕНДОВАНУ RAM гри (МБ) під фізичну пам'ять
// системи — щоб на слабких ПК гра не штовхала Windows у своп разом з
// лаунчером (див. docs/dev/OPTIMIZATION_PLAN.md, бюджет 6-ГБ машини).
//
// Формула консервативна: грі віддаємо менше, ніж половина системної
// пам'яті, бо разом з нею живуть Windows (~1.5–2 ГБ), WebView2 лаунчера
// (~300–500 МБ) і Go-бекенд. Використовується ЛИШЕ як підказка за
// замовчуванням — явний вибір користувача (MaxRAMOverride / UseCustomRAM /
// змінене глобальне значення) завжди має пріоритет.
func autoGameRAM(totalMB int64) int {
	// Невідома пам'ять (0) — не вгадуємо, лишаємо класичний дефолт.
	if totalMB <= 0 {
		return 4096
	}
	switch {
	case totalMB <= 4096:
		return 2048 // 4 ГБ → 2 ГБ грі
	case totalMB <= 6144:
		return 2560 // 6 ГБ → 2.5 ГБ грі
	case totalMB <= 8192:
		return 3072 // 8 ГБ → 3 ГБ грі
	case totalMB <= 12288:
		return 3584 // 12 ГБ → 3.5 ГБ грі
	default:
		return 4096 // 16+ ГБ → класичний дефолт
	}
}
