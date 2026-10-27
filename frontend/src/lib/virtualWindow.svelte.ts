// virtualWindow.svelte.ts — віконна віртуалізація довгих списків.
//
// ПРОБЛЕМА: список модів збірки рендерив УСІ рядки (content-visibility лише
// пропускає їхній paint, але вузли DOM, Svelte-стан і <img> лишаються).
// На збірці з 300–1000 модів це тисячі вузлів і сотні <img> у WebView.
//
// ТЕПЕР: при довгому списку в DOM лежить лише видиме вікно + буфер; решту
// заміщають два відступи (padding-top/bottom на контейнері списку), тож
// висота скролу й положення смужки лишаються правильними.
//
// Висота рядка вимірюється (середнє по відрендерених), а не вгадується:
// рядки можуть трохи відрізнятись (бейджі), але в межах вікна це не видно,
// а відступи рахуються за середнім.
//
// Використання (див. PackModsSection.svelte):
//   const vw = new VirtualWindow({ min: 60, buffer: 6, gap: 8, rowSelector: '.mod-row' })
//   $effect(() => { if (listEl) { vw.attach(listEl); return () => vw.detach() } })
//   $effect(() => { vw.setTotal(items.length) })
//   const virtual = $derived(vw.active)
//   const rendered = $derived(virtual ? items.slice(vw.start, vw.end) : items)
//   <div style={vw.style}> … {#each rendered …} … </div>
import { untrack } from 'svelte'

export interface VirtualWindowOptions {
  /** Починаючи з скількох елементів вмикається віртуалізація. */
  min: number
  /** Скільки рядків рендерити поза видимою областю з кожного боку. */
  buffer: number
  /** Проміжок між рядками (px), як у CSS gap контейнера. */
  gap: number
  /** CSS-селектор рядка — для вимірювання реальної висоти. */
  rowSelector: string
  /** Початкова оцінка висоти рядка БЕЗ gap (px). */
  estimate?: number
}

function findScrollParent(el: HTMLElement): HTMLElement {
  let p = el.parentElement
  while (p && p !== document.body && p !== document.documentElement) {
    const oy = getComputedStyle(p).overflowY
    if (oy === 'auto' || oy === 'scroll' || oy === 'overlay') return p
    p = p.parentElement
  }
  return (document.scrollingElement as HTMLElement) ?? document.documentElement
}

export class VirtualWindow {
  start = $state(0)
  end = $state(0)
  /** Повна висота одного кроку: рядок + gap. */
  step = $state(0)
  /** Чи вимкнена повна відмальовка (елементів більше за min). */
  active = $state(false)

  #total = 0
  #opts: VirtualWindowOptions
  #list: HTMLElement | null = null
  #scroll: HTMLElement | null = null
  #raf = 0
  #ro: ResizeObserver | null = null
  #onScroll = () => this.#schedule()

  constructor(opts: VirtualWindowOptions) {
    this.#opts = opts
    this.step = (opts.estimate ?? 70) + opts.gap
  }

  /** Стиль-відступи для контейнера списку. */
  get style(): string {
    if (!this.active) return ''
    const top = this.start * this.step
    const bottom = Math.max(0, this.#total - this.end) * this.step
    return `padding-top:${top}px;padding-bottom:${bottom}px`
  }

  attach(list: HTMLElement) {
    this.detach()
    this.#list = list
    this.#scroll = findScrollParent(list)
    const target: EventTarget =
      this.#scroll === document.scrollingElement ? window : this.#scroll
    target.addEventListener('scroll', this.#onScroll, { passive: true })
    if (typeof ResizeObserver !== 'undefined') {
      this.#ro = new ResizeObserver(() => this.#schedule())
      this.#ro.observe(this.#scroll)
    }
    this.#schedule()
  }

  detach() {
    if (this.#scroll) {
      const target: EventTarget =
        this.#scroll === document.scrollingElement ? window : this.#scroll
      target.removeEventListener('scroll', this.#onScroll)
    }
    this.#ro?.disconnect()
    this.#ro = null
    if (this.#raf) cancelAnimationFrame(this.#raf)
    this.#raf = 0
    this.#list = null
    this.#scroll = null
  }

  setTotal(n: number) {
    this.#total = n
    untrack(() => this.#recalc())
    this.#schedule()
  }

  #schedule() {
    if (this.#raf) return
    this.#raf = requestAnimationFrame(() => {
      this.#raf = 0
      untrack(() => {
        this.#measure()
        this.#recalc()
      })
    })
  }

  /** Середня реальна висота кроку по відрендерених рядках. */
  #measure() {
    const list = this.#list
    if (!list || !this.active) return
    const rows = list.querySelectorAll<HTMLElement>(this.#opts.rowSelector)
    if (rows.length < 2) return
    const first = rows[0]
    const last = rows[rows.length - 1]
    const span = last.offsetTop + last.offsetHeight - first.offsetTop
    const step = (span + this.#opts.gap) / rows.length
    if (step > 8 && Math.abs(step - this.step) > 1) this.step = step
  }

  #set(start: number, end: number, active: boolean) {
    if (this.start !== start) this.start = start
    if (this.end !== end) this.end = end
    if (this.active !== active) this.active = active
  }

  #recalc() {
    const total = this.#total
    const { min, buffer } = this.#opts
    if (total <= min) {
      this.#set(0, total, false)
      return
    }
    const list = this.#list
    const scroll = this.#scroll
    if (!list || !scroll) {
      // Ще не змонтовано: рендеримо перший екран, віконце уточниться в attach().
      this.#set(0, Math.min(total, min), true)
      return
    }
    const isDoc = scroll === document.scrollingElement
    const viewTop = isDoc ? 0 : scroll.getBoundingClientRect().top
    const viewH = isDoc ? window.innerHeight : scroll.clientHeight
    // Скільки пікселів списку вже «над» верхнім краєм в'юпорту.
    const scrolledIn = viewTop - list.getBoundingClientRect().top
    const step = this.step
    let start = Math.floor(scrolledIn / step) - buffer
    let end = Math.ceil((scrolledIn + viewH) / step) + buffer
    start = Math.max(0, Math.min(start, total - 1))
    end = Math.min(total, Math.max(end, start + 1, buffer * 2))
    this.#set(start, end, true)
  }
}
