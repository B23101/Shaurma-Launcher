// ── Підказки (tooltips) для кнопок соціальних екранів ─────────────────────
// Нативний title з'являється із затримкою ~1 с і не стилізується. Тут —
// власна підказка: рендериться у <body> (не обрізається overflow батьків),
// з'являється за ~120 мс з м'якою анімацією, сама перевертається вниз, якщо
// зверху немає місця, і зникає при втраті фокусу/скролі.
//
// Використання: <button use:tip={'Копіювати'}> або use:tip={{ text, side }}.

type TipArg = string | { text: string; side?: 'top' | 'bottom' } | null | undefined

const STYLE_ID = 'fr-tip-style'

function ensureStyle() {
  if (typeof document === 'undefined' || document.getElementById(STYLE_ID)) return
  const s = document.createElement('style')
  s.id = STYLE_ID
  s.textContent = `
  .fr-tip {
    position: fixed; z-index: 100000; pointer-events: none;
    max-width: 240px; padding: 6px 10px; border-radius: 8px;
    background: var(--bg-input-hi, #282828); color: var(--text, #fff);
    border: 1px solid var(--border-hi, rgba(255,255,255,.12));
    box-shadow: 0 8px 22px rgba(0,0,0,.45);
    font: 600 11px/1.35 var(--font-ui, 'Inter', sans-serif);
    opacity: 0; transform: translateY(var(--fr-tip-from, 4px)) scale(.96);
    transition: opacity .14s ease, transform .18s cubic-bezier(.2,1.2,.4,1);
    white-space: normal; text-align: center;
  }
  .fr-tip.on { opacity: 1; transform: none; }
  .fr-tip::after {
    content: ''; position: absolute; left: var(--fr-tip-ax, 50%); width: 8px; height: 8px;
    background: inherit; border: inherit; border-width: 0 1px 1px 0;
    transform: translateX(-50%) rotate(45deg);
  }
  .fr-tip[data-side='top']::after { bottom: -5px; }
  .fr-tip[data-side='bottom']::after { top: -5px; transform: translateX(-50%) rotate(225deg); }
  @media (prefers-reduced-motion: reduce) { .fr-tip { transition: opacity .1s; transform: none; } }
  `
  document.head.appendChild(s)
}

export function tip(node: HTMLElement, arg: TipArg) {
  let cur = arg
  let el: HTMLDivElement | null = null
  let showTimer: ReturnType<typeof setTimeout> | undefined

  const text = () => (typeof cur === 'string' ? cur : cur?.text ?? '')
  const pref = () => (typeof cur === 'object' && cur ? cur.side : undefined)

  function place() {
    if (!el) return
    const r = node.getBoundingClientRect()
    const tw = el.offsetWidth
    const th = el.offsetHeight
    const gap = 9
    let side: 'top' | 'bottom' = pref() ?? 'top'
    if (side === 'top' && r.top - th - gap < 6) side = 'bottom'
    if (side === 'bottom' && r.bottom + th + gap > window.innerHeight - 6) side = 'top'
    const cx = r.left + r.width / 2
    let left = cx - tw / 2
    left = Math.max(8, Math.min(left, window.innerWidth - tw - 8))
    const top = side === 'top' ? r.top - th - gap : r.bottom + gap
    el.dataset.side = side
    el.style.left = `${Math.round(left)}px`
    el.style.top = `${Math.round(top)}px`
    el.style.setProperty('--fr-tip-ax', `${Math.round(cx - left)}px`)
    el.style.setProperty('--fr-tip-from', side === 'top' ? '4px' : '-4px')
  }

  function show() {
    clearTimeout(showTimer)
    if (!text()) return
    showTimer = setTimeout(() => {
      ensureStyle()
      hide(true)
      el = document.createElement('div')
      el.className = 'fr-tip'
      el.setAttribute('role', 'tooltip')
      el.textContent = text()
      document.body.appendChild(el)
      place()
      requestAnimationFrame(() => el?.classList.add('on'))
    }, 120)
  }

  function hide(instant = false) {
    clearTimeout(showTimer)
    if (!el) return
    const gone = el
    el = null
    if (instant) { gone.remove(); return }
    gone.classList.remove('on')
    setTimeout(() => gone.remove(), 160)
  }

  const onLeave = () => hide()
  node.addEventListener('mouseenter', show)
  node.addEventListener('mouseleave', onLeave)
  node.addEventListener('focus', show)
  node.addEventListener('blur', onLeave)
  node.addEventListener('mousedown', onLeave)
  window.addEventListener('scroll', onLeave, true)

  return {
    update(next: TipArg) {
      cur = next
      if (el) { el.textContent = text(); place() }
    },
    destroy() {
      hide(true)
      node.removeEventListener('mouseenter', show)
      node.removeEventListener('mouseleave', onLeave)
      node.removeEventListener('focus', show)
      node.removeEventListener('blur', onLeave)
      node.removeEventListener('mousedown', onLeave)
      window.removeEventListener('scroll', onLeave, true)
    },
  }
}
