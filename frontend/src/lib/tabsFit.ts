// ── Поступове стискання таббара (.tabbar.tb-slide) ───────────────────────
// Svelte-дія для сторінок збірки й сервера. Коли вкладки не вміщаються в ширину,
// стискаємо ПОВІЛЬНО, рівно настільки, скільки бракує:
//   1) горизонтальні відступи вкладок зменшуються плавно, по 1px (--tbf-px на кожній кнопці,
//      клас .tbf-fit на барі; змінна на кнопках, бо Svelte перезаписує style самого бару);
//   2) відступи вже мінімальні — щільніші шрифт/іконки/проміжки (клас .tbf-tight);
//   3) і лише якщо й так критично — підписи вкладок ховаються ПО ОДНОМУ з кінця
//      (клас .tbf-ico на кнопці; лишається іконка, підпис — у підказці).
//      Активна вкладка завжди з підписом.
// Коли місця знову вистачає, усе повертається назад (кожен прохід рахується з нуля).
//
// Міряємо НЕ scrollWidth (його роздувають абсолютні псевдоелементи-«лінза» та transform,
// через що вкладки стискались, коли місця вистачало), а справжню потрібну ширину кнопок
// проти внутрішньої ширини батьківського блоку.
// Заміри — у requestAnimationFrame, а не прямо в колбеку ResizeObserver: зміна розмітки
// в колбеку спричиняла б «ResizeObserver loop completed with undelivered notifications».
// Стилі рівнів — у style.css (блок «Поступове стискання таббара»).
//
// Розмітка: кнопки .tab-btn — прямі діти бару; підпис — у <span> (не голий текст),
// щоб CSS міг його сховати у режимі іконки.

export interface TabsFitOptions {
  /** Мінімальний горизонтальний відступ кнопки (px). */
  minPx?: number
  /** Викликається після кожного перерахунку (напр., зсунути «пігулку» активної вкладки). */
  onFit?: () => void
}

export function tabsFit(bar: HTMLElement, options: TabsFitOptions = {}) {
  let opts = options
  let raf = 0
  bar.classList.add('tbf')

  const buttons = () => Array.from(bar.querySelectorAll<HTMLElement>(':scope > .tab-btn'))

  // Скільки місця потрібно кнопкам (разом із рамкою й відступами бару) без прокрутки.
  function neededW(bs: HTMLElement[]): number {
    const last = bs[bs.length - 1]
    const cs = getComputedStyle(bar)
    return last.offsetLeft + last.offsetWidth + parseFloat(cs.paddingRight || '0')
      + parseFloat(cs.borderLeftWidth || '0') + parseFloat(cs.borderRightWidth || '0')
  }
  // Скільки місця дає батьківський блок (внутрішня ширина).
  function availW(): number {
    const p = bar.parentElement
    if (!p) return bar.clientWidth
    const cs = getComputedStyle(p)
    return p.clientWidth - parseFloat(cs.paddingLeft || '0') - parseFloat(cs.paddingRight || '0')
  }

  function fit() {
    const bs = buttons()
    if (!bs.length) return
    // Старт: усі підписи, звичайні відступи.
    bar.classList.remove('tbf-fit', 'tbf-tight')
    for (const b of bs) { b.classList.remove('tbf-ico'); b.style.removeProperty('--tbf-px') }
    const setPx = (px: number) => { for (const b of bs) b.style.setProperty('--tbf-px', `${px}px`) }

    const avail = availW()
    // Прихований (display:none) бар чи нульова ширина — не міряємо, щоб не стиснути даремно.
    if (avail > 0 && bar.offsetWidth > 0) {
      const over = () => neededW(bs) > avail + 0.5
      if (over()) {
        // 1) плавно зменшуємо відступи. Спершу змінна, потім клас: без --tbf-px
        //    правило .tbf-fit обнулило б відступи.
        let px = Math.round(parseFloat(getComputedStyle(bs[0]).paddingLeft || '16')) || 16
        const min = opts.minPx ?? 7
        setPx(px)
        bar.classList.add('tbf-fit')
        while (px > min && over()) { px -= 1; setPx(px) }
        // 2) відступи мінімальні — щільніші шрифт/іконки/проміжки.
        if (over()) bar.classList.add('tbf-tight')
        // 3) критично — підписи в іконки по одному з кінця (активна лишається з підписом).
        if (over()) {
          const order = bs.filter(b => !b.classList.contains('active')).reverse()
          for (const b of order) { if (!over()) break; b.classList.add('tbf-ico') }
        }
      }
    }
    // Наші власні зміни класів не мають викликати новий перерахунок.
    mo.takeRecords()
    opts.onFit?.()
  }
  function schedule() {
    if (raf) return
    raf = requestAnimationFrame(() => { raf = 0; fit() })
  }

  // Змінилась активна вкладка, набір вкладок, підпис чи лічильник — міряємо наново.
  const mo = new MutationObserver(schedule)
  mo.observe(bar, { subtree: true, childList: true, characterData: true, attributes: true, attributeFilter: ['class'] })
  const ro = typeof ResizeObserver !== 'undefined' ? new ResizeObserver(schedule) : null
  ro?.observe(bar)
  if (bar.parentElement) ro?.observe(bar.parentElement)
  // Шрифти/іконки можуть змінити ширину вкладок уже після першого кадру.
  const late = setTimeout(schedule, 200)
  schedule()

  return {
    update(next: TabsFitOptions = {}) { opts = next; schedule() },
    destroy() {
      if (raf) cancelAnimationFrame(raf)
      clearTimeout(late)
      mo.disconnect()
      ro?.disconnect()
    },
  }
}
