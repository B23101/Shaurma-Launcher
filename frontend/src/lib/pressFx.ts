// ── pressFx: відгук на КОЖЕН клік по всьому лаунчеру ─────────────────────
// Один делегований слухач pointerdown на документі (а не тисячі обробників
// на кнопках). На натискання елемент «вдавлюється», на відпускання —
// пружно повертається з легким перескоком, а в кнопках з position≠static
// розходиться хвиля (ripple) від точки кліку.
//
// Чому так, а не CSS :active:
//  • анімуємо ВЛАСТИВІСТЬ `scale` через Web Animations API. Вона окрема від
//    `transform`, тож не ламає власні transform/animation елементів (вхідні
//    анімації карток не перезапускаються), а WAAPI-анімація після кінця
//    просто зникає — нічого не лишається в стилях;
//  • сила стиску залежить від розміру: маленька іконка помітно «клацає»,
//    а широкий рядок списку ледь осідає — без стрибків великих блоків;
//  • `scale`/`opacity`/`transform` рахує композитор GPU, без layout і
//    перемальовки сторінки — тому це дешево навіть на слабких ПК.
//
// Вимикається повністю (жодного DOM чи анімацій), коли користувач вимкнув
// «Анімації інтерфейсу», увімкнув «Режим продуктивності» або в ОС задано
// prefers-reduced-motion.

const CLICKABLE = [
  'button', 'a[href]', 'summary', 'label[for]',
  '[role="button"]', '[role="tab"]', '[role="switch"]', '[role="checkbox"]',
  '[role="radio"]', '[role="menuitem"]', '[role="option"]',
].join(',')

const SPRING = 'cubic-bezier(.34,1.56,.64,1)'

let installed = false
let reduceMq: MediaQueryList | null = null

function enabled(): boolean {
  const ds = document.documentElement.dataset
  if (ds.animations === 'off' || ds.performanceMode === 'on') return false
  return !reduceMq?.matches
}

function pressScale(w: number, h: number): number {
  if (w <= 48 && h <= 48) return 0.86
  if (w <= 200) return 0.94
  if (w <= 420) return 0.97
  return 0.985
}

function target(e: PointerEvent): HTMLElement | null {
  const el = (e.target as Element | null)?.closest?.<HTMLElement>(CLICKABLE)
  if (!el || el.closest('[data-no-press]')) return null
  if ((el as HTMLButtonElement).disabled || el.getAttribute('aria-disabled') === 'true') return null
  return el
}

function ripple(el: HTMLElement, e: PointerEvent, r: DOMRect) {
  // Хвиля лише там, де вона не змінює верстку: потрібен позиціонований
  // батько. position самі не чіпаємо — це зсунуло б абсолютних нащадків.
  const cs = getComputedStyle(el)
  if (cs.position === 'static' || cs.display === 'contents') return
  const host = document.createElement('span')
  host.className = 'ui-ripple'
  host.setAttribute('aria-hidden', 'true')
  const dot = document.createElement('i')
  const size = Math.hypot(r.width, r.height) * 2
  dot.style.width = dot.style.height = `${size}px`
  dot.style.left = `${e.clientX - r.left - size / 2}px`
  dot.style.top = `${e.clientY - r.top - size / 2}px`
  host.appendChild(dot)
  el.appendChild(host)
  const a = dot.animate(
    [{ transform: 'scale(0)', opacity: 0.32 }, { transform: 'scale(1)', opacity: 0 }],
    { duration: 520, easing: 'cubic-bezier(.2,.7,.3,1)' },
  )
  const done = () => host.remove()
  a.onfinish = done
  a.oncancel = done
  setTimeout(done, 900) // запобіжник, якщо елемент прибрали з DOM посеред анімації
}

function onDown(e: PointerEvent) {
  if (e.button !== 0 || !e.isPrimary || !enabled()) return
  const el = target(e)
  if (!el) return
  const r = el.getBoundingClientRect()
  if (!r.width || !r.height) return
  const s = pressScale(r.width, r.height)
  let press: Animation | null = null
  try {
    press = el.animate([{ scale: '1' }, { scale: String(s) }], { duration: 110, easing: 'cubic-bezier(.2,0,0,1)', fill: 'forwards' })
  } catch { return } // старий рушій без окремої властивості scale — просто без ефекту
  // Хвиля — лише для «справжніх» кнопок, не для перемикачів і галочок.
  const role = el.getAttribute('role')
  if (role !== 'switch' && role !== 'checkbox' && role !== 'radio') ripple(el, e, r)

  const release = () => {
    window.removeEventListener('pointerup', release, true)
    window.removeEventListener('pointercancel', release, true)
    window.removeEventListener('blur', release)
    press?.cancel()
    if (!el.isConnected) return
    try {
      el.animate([{ scale: String(s) }, { scale: String(1 + (1 - s) * 0.35) }, { scale: '1' }], { duration: 300, easing: SPRING })
    } catch { /* ignore */ }
  }
  window.addEventListener('pointerup', release, true)
  window.addEventListener('pointercancel', release, true)
  window.addEventListener('blur', release)
}

/** Вмикає відгук на кліки для всього застосунку. Повторний виклик нічого не робить. */
export function installPressFx(): () => void {
  if (installed || typeof window === 'undefined') return () => {}
  installed = true
  reduceMq = window.matchMedia?.('(prefers-reduced-motion: reduce)') ?? null
  document.addEventListener('pointerdown', onDown, { capture: true, passive: true })
  return () => {
    installed = false
    document.removeEventListener('pointerdown', onDown, { capture: true })
  }
}
