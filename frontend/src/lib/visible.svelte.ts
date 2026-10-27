// ── Видимість вікна (спільний store) ────────────────────────────────────
// Оптимізація для слабких ПК (docs/dev/OPTIMIZATION_PLAN.md §3): коли вікно
// лаунчера згорнуте/приховане (типовий сценарій — гра запущена, лаунчер
// у фоні), фонові таймери й rAF-цикли не мають «крутитись» даремно:
// вони їдять CPU, який потрібен грі.
//
// Один слухач document visibilitychange на весь додаток; компоненти
// читають стан (Svelte 5 сам перерендерить залежних при зміні).
//
// Два API (обидва підтримуються):
//   import { windowVisibility } from './visible.svelte'  → об'єкт { visible }
//   import { isWindowVisible } from './visible.svelte'   → функція-читач
// Використання:
//   if (!windowVisibility.visible) return   // пропустити тик таймера
//   if (!isWindowVisible()) return          // той самий стан
//
// ВАЖЛИВО (Svelte 5 runes): `$state` на рівні модуля створює константу,
// яку не можна переназначати напряму (`Cannot assign to ... because it
// is a constant`), а `$derived` з модуля експортувати заборонено
// (derived_invalid_export). Тому стан живе ВСЕРЕДИНІ об'єкта
// windowVisibility (мутуємо його поле — як toast.svelte.ts з toastAnchor),
// а isWindowVisible — це простий читач того самого значення.
const windowVisibility = $state({ visible: true })

export { windowVisibility }

// Функція-читач булевого стану (для нових компонентів). Svelte 5
// відстежує доступ до windowVisibility.visible всередині, тож читачі
// перерендеряться при зміні.
export function isWindowVisible(): boolean {
  return windowVisibility.visible
}

if (typeof document !== 'undefined') {
  windowVisibility.visible = !document.hidden
  document.addEventListener('visibilitychange', () => {
    windowVisibility.visible = !document.hidden
  })
}
