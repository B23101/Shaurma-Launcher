// ── rangeFx: заповнення доріжки для ВСІХ повзунків лаунчера ─────────────
// Єдиний стиль повзунка живе в style.css (input[type=range]). У Chromium
// (WebView2) і WebKitGTK немає псевдоелемента «заповненої частини», тож
// відсоток кладемо в CSS-змінну --range-fill, а доріжка малює градієнт.
//
// Без жодних обробників на окремих повзунках: один делегований слухач
// input (миттєво для того, що тягнуть) + перерахунок усіх у наступному
// кадрі після будь-якого вводу/кліку чи зміни DOM — так підхоплюються й
// значення, змінені кодом (ввели МБ у поле → повзунок поруч оновився).
// Перерахунок дешевий: кілька елементів, лише запис CSS-змінної.

let installed = false
let raf = 0

function paint(el: HTMLInputElement) {
  const min = Number(el.min || 0)
  const max = Number(el.max || 100)
  const v = Number(el.value)
  const pct = max > min ? Math.max(0, Math.min(100, ((v - min) / (max - min)) * 100)) : 0
  const next = `${pct}%`
  if (el.style.getPropertyValue('--range-fill') !== next) el.style.setProperty('--range-fill', next)
}

function paintAll() {
  raf = 0
  for (const el of document.querySelectorAll<HTMLInputElement>('input[type="range"]')) paint(el)
}

function schedule() {
  if (!raf) raf = requestAnimationFrame(paintAll)
}

function onInput(e: Event) {
  const el = e.target as HTMLInputElement | null
  if (el?.type === 'range') paint(el)
  schedule()
}

export function installRangeFx(): void {
  if (installed || typeof document === 'undefined') return
  installed = true
  document.addEventListener('input', onInput, true)
  document.addEventListener('change', schedule, true)
  document.addEventListener('click', schedule, true)
  document.addEventListener('keyup', schedule, true)
  new MutationObserver(schedule).observe(document.body, { childList: true, subtree: true })
  schedule()
}
