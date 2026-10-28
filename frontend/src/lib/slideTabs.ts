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
