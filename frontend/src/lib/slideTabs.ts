// ── Ковзна пігулка для .tabbar.tb-slide ──────────────────────────────────
// Svelte-дія: міряє активну .tab-btn і кладе її позицію в CSS-змінні
// --tb-ind-l / --tb-ind-w (їх читають ::before/::after з style.css).
// Сама стежить за зміною активної вкладки (class), набору вкладок і
// розміру бару, а активну вкладку тримає в зоні видимості при прокрутці.
export function slideTabs(bar: HTMLElement) {
  let raf = 0
  function measure() {
    const btn = bar.querySelector<HTMLElement>('.tab-btn.active')
    if (!btn) { bar.style.setProperty('--tb-ind-w', '0px'); return }
    const br = bar.getBoundingClientRect()
    const r = btn.getBoundingClientRect()
    // Пігулка позиціонується від padding-боксу (всередині рамки) і лежить
    // у контейнері прокрутки — тому мінус clientLeft і плюс scrollLeft.
    bar.style.setProperty('--tb-ind-l', `${Math.max(0, r.left - br.left - (bar.clientLeft || 0) + bar.scrollLeft)}px`)
    bar.style.setProperty('--tb-ind-w', `${r.width}px`)
    const want = btn.offsetLeft - (bar.clientWidth - btn.offsetWidth) / 2
    if (bar.scrollWidth > bar.clientWidth && Math.abs(bar.scrollLeft - want) > 1) {
      bar.scrollTo({ left: Math.max(0, want), behavior: 'smooth' })
    }
  }
  const schedule = () => { cancelAnimationFrame(raf); raf = requestAnimationFrame(measure) }
  const mo = new MutationObserver(schedule)
  mo.observe(bar, { subtree: true, childList: true, attributes: true, attributeFilter: ['class'], characterData: true })
  const ro = typeof ResizeObserver !== 'undefined' ? new ResizeObserver(schedule) : null
  ro?.observe(bar)
  window.addEventListener('resize', schedule)
  // Шрифти/іконки можуть змінити ширину вкладок уже після першого кадру.
  const late = setTimeout(schedule, 150)
  schedule()
  return {
    destroy() {
      cancelAnimationFrame(raf)
      clearTimeout(late)
      mo.disconnect()
      ro?.disconnect()
      window.removeEventListener('resize', schedule)
    },
  }
}

// Плавна горизонтальна прокрутка таббара колесом миші: вертикальне колесо
// гортає вкладки вбік з інерційним доганянням цілі (а не різкими стрибками).
// Якщо бар не переповнений або досягнуто краю — подія йде далі (гортається
// сторінка). Окрема дія: вішається на будь-який .tabbar.
export function smoothTabsWheel(bar: HTMLElement) {
  let target = 0
  let raf = 0
  function step() {
    const diff = target - bar.scrollLeft
    if (Math.abs(diff) < 0.5) { bar.scrollLeft = target; raf = 0; return }
    bar.scrollLeft += diff * 0.2
    raf = requestAnimationFrame(step)
  }
  function onWheel(e: WheelEvent) {
    const max = bar.scrollWidth - bar.clientWidth
    if (max <= 1) return
    const delta = Math.abs(e.deltaX) > Math.abs(e.deltaY) ? e.deltaX : e.deltaY
    if (!delta) return
    const base = raf ? target : bar.scrollLeft
    const next = Math.min(max, Math.max(0, base + delta))
    if (next === base) return // край — нехай гортається сторінка
    e.preventDefault()
    target = next
    if (!raf) raf = requestAnimationFrame(step)
  }
  bar.addEventListener('wheel', onWheel, { passive: false })
  return {
    destroy() {
      cancelAnimationFrame(raf)
      bar.removeEventListener('wheel', onWheel)
    },
  }
}
