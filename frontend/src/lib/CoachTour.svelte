<script lang="ts">
  import { tip as launcherTip } from './friends/tooltip'

  // Покрокові підказки поверх сторінки: підсвічує елемент з data-tour="<id>"
  // і пояснює, що це за кнопка й навіщо вона. Перший запуск: створення
  // першої збірки після майстра. Кроки, чиїх елементів немає на сторінці,
  // пропускаються самі.
  import { onDestroy, onMount, tick } from 'svelte'
  import { fade, fly } from 'svelte/transition'
  import Portal from './Portal.svelte'
  import { t } from './i18n.svelte'

  type Step = { id: string; icon: string; title: string; text: string }
  let { steps = [], onDone }: { steps?: Step[]; onDone: () => void } = $props()

  let idx = $state(0)
  let rect = $state<{ x: number; y: number; w: number; h: number } | null>(null)
  let vw = $state(window.innerWidth)
  let vh = $state(window.innerHeight)
  let raf = 0

  let live = $state<Step[]>([])
  const cur = $derived(live[Math.min(idx, live.length - 1)])

  async function measure(scroll = false) {
    await tick()
    const el = cur ? document.querySelector(`[data-tour="${cur.id}"]`) as HTMLElement | null : null
    if (!el) { rect = null; return }
    if (scroll) {
      // Миттєво: плавна прокрутка зсуває елемент після виміру. Рух дає перехід підсвітки.
      const tall = el.getBoundingClientRect().height > window.innerHeight * 0.7
      el.scrollIntoView({ block: tall ? 'start' : 'center', behavior: 'instant' as ScrollBehavior })
      await new Promise(r => setTimeout(r, 30))
    }
    const r = el.getBoundingClientRect()
    const pad = 8
    rect = { x: r.left - pad, y: r.top - pad, w: r.width + pad * 2, h: r.height + pad * 2 }
  }
  function onResize() {
    vw = window.innerWidth; vh = window.innerHeight
    cancelAnimationFrame(raf); raf = requestAnimationFrame(() => measure())
  }
  function onScroll() { cancelAnimationFrame(raf); raf = requestAnimationFrame(() => measure()) }
  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape') onDone()
    else if (e.key === 'ArrowRight' || e.key === 'Enter') { e.preventDefault(); next() }
    else if (e.key === 'ArrowLeft') back()
  }

  onMount(() => {
    window.addEventListener('resize', onResize)
    window.addEventListener('scroll', onScroll, true)
    window.addEventListener('keydown', onKey)
    // Сторінка ще монтується (анімація появи) — даємо їй стати на місце.
    setTimeout(() => {
      live = steps.filter(s => document.querySelector(`[data-tour="${s.id}"]`))
      if (live.length === 0) { onDone(); return }
      measure(true)
    }, 450)
  })
  onDestroy(() => {
    window.removeEventListener('resize', onResize)
    window.removeEventListener('scroll', onScroll, true)
    window.removeEventListener('keydown', onKey)
    cancelAnimationFrame(raf)
  })

  function next() { if (idx >= live.length - 1) onDone(); else { idx++; measure(true) } }
  function back() { if (idx > 0) { idx--; measure(true) } }

  // Картка підказки — праворуч від елемента, якщо влазить; інакше ліворуч, знизу чи зверху.
  const CARD_W = 330
  const card = $derived.by(() => {
    if (!rect) return { x: vw / 2 - CARD_W / 2, y: vh / 2 - 90, side: 'center' }
    const gap = 16, h = 210
    if (rect.x + rect.w + gap + CARD_W < vw - 12) return { x: rect.x + rect.w + gap, y: clamp(rect.y, 12, vh - h - 12), side: 'right' }
    if (rect.x - gap - CARD_W > 12) return { x: rect.x - gap - CARD_W, y: clamp(rect.y, 12, vh - h - 12), side: 'left' }
    const x = clamp(rect.x + rect.w / 2 - CARD_W / 2, 12, vw - CARD_W - 12)
    if (rect.y + rect.h + gap + h < vh) return { x, y: rect.y + rect.h + gap, side: 'bottom' }
    return { x, y: Math.max(12, rect.y - gap - h), side: 'top' }
  })
  function clamp(v: number, a: number, b: number) { return Math.max(a, Math.min(b, v)) }
</script>

<Portal>
  <div class="ct" transition:fade={{ duration: 220 }} role="dialog" aria-modal="true" aria-label={cur?.title ?? ''}>
    {#if rect}
      <div class="spot" style="left:{rect.x}px;top:{rect.y}px;width:{rect.w}px;height:{rect.h}px"></div>
      <div class="ring" style="left:{rect.x}px;top:{rect.y}px;width:{rect.w}px;height:{rect.h}px"></div>
    {:else}
      <div class="dim"></div>
    {/if}

    {#if cur}
      {#key idx}
        <div class="card" style="left:{card.x}px;top:{card.y}px;width:{CARD_W}px"
             in:fly={{ x: card.side === 'right' ? -14 : card.side === 'left' ? 14 : 0, y: card.side === 'bottom' ? -10 : card.side === 'top' ? 10 : 0, duration: 380, delay: 120 }}>
          <div class="c-top">
            <span class="c-ico"><i class="ti {cur.icon}"></i></span>
            <span class="c-step">{t('tour.stepOf', { cur: idx + 1, total: live.length })}</span>
            <button class="c-x" onclick={onDone} aria-label={t('tour.skip')} use:launcherTip={t('tour.skip')}><i class="ti ti-x"></i></button>
          </div>
          <b class="c-title">{cur.title}</b>
          <p class="c-text">{cur.text}</p>
          <div class="c-bar"><span style="width:{((idx + 1) / live.length) * 100}%"></span></div>
          <div class="c-foot">
            <button class="btn btn-ghost btn-sm" class:hide={idx === 0} onclick={back}><i class="ti ti-arrow-left"></i></button>
            <button class="skip" onclick={onDone}>{t('tour.skip')}</button>
            <button class="btn btn-primary btn-sm" onclick={next}>
              {idx >= live.length - 1 ? t('tour.done') : t('tour.next')} <i class="ti {idx >= live.length - 1 ? 'ti-check' : 'ti-arrow-right'}"></i>
            </button>
          </div>
        </div>
      {/key}
    {/if}
  </div>
</Portal>

<style>
  .ct { position: fixed; inset: 0; z-index: 99990; }
  .dim { position: absolute; inset: 0; background: rgba(0, 0, 0, .62); }
  .spot {
    position: absolute; border-radius: 16px; pointer-events: none;
    box-shadow: 0 0 0 9999px rgba(0, 0, 0, .62);
    transition: left .5s cubic-bezier(.34, 1.2, .64, 1), top .5s cubic-bezier(.34, 1.2, .64, 1), width .5s cubic-bezier(.34, 1.2, .64, 1), height .5s cubic-bezier(.34, 1.2, .64, 1);
  }
  .ring {
    position: absolute; border-radius: 16px; pointer-events: none; border: 2px solid var(--orange);
    transition: left .5s cubic-bezier(.34, 1.2, .64, 1), top .5s cubic-bezier(.34, 1.2, .64, 1), width .5s cubic-bezier(.34, 1.2, .64, 1), height .5s cubic-bezier(.34, 1.2, .64, 1);
    animation: ring 2s ease-out infinite;
  }
  @keyframes ring { 0% { box-shadow: 0 0 0 0 rgba(var(--orange-rgb), .55) } 100% { box-shadow: 0 0 0 14px rgba(var(--orange-rgb), 0) } }
  .card {
    position: absolute; padding: 14px 16px 12px; border-radius: 16px;
    background: var(--bg-window); border: 1px solid var(--border-hi);
    box-shadow: 0 24px 60px rgba(0, 0, 0, .5);
    transition: left .45s cubic-bezier(.22, 1, .36, 1), top .45s cubic-bezier(.22, 1, .36, 1);
  }
  .c-top { display: flex; align-items: center; gap: 10px; margin-bottom: 10px; }
  .c-ico { width: 32px; height: 32px; border-radius: 10px; display: grid; place-items: center; font-size: 17px; color: #fff; background: var(--orange); box-shadow: 0 6px 14px rgba(var(--orange-rgb), .4); animation: pop .45s cubic-bezier(.34, 1.56, .64, 1) backwards .2s; }
  @keyframes pop { from { transform: scale(.4) rotate(-20deg); opacity: 0 } }
  .c-step { flex: 1; font-size: 11px; font-weight: 700; letter-spacing: .5px; text-transform: uppercase; color: var(--text-dim); }
  .c-x { border: none; background: none; color: var(--text-dim); cursor: pointer; font-size: 16px; padding: 4px; border-radius: 6px; }
  .c-x:hover { color: var(--text); background: var(--hover); }
  .c-title { display: block; font-size: 15px; font-weight: 800; margin-bottom: 5px; }
  .c-text { margin: 0 0 12px; font-size: 12.5px; line-height: 1.55; color: var(--text-mute); }
  .c-bar { height: 3px; border-radius: 2px; background: var(--hover-strong); overflow: hidden; margin-bottom: 10px; }
  .c-bar span { display: block; height: 100%; background: var(--orange); transition: width .4s cubic-bezier(.22, 1, .36, 1); }
  .c-foot { display: flex; align-items: center; gap: 8px; }
  .c-foot .hide { visibility: hidden; }
  .skip { margin-left: auto; border: none; background: none; color: var(--text-mute); font: inherit; font-size: 12px; cursor: pointer; }
  .skip:hover { color: var(--text); }
  @media (prefers-reduced-motion: reduce) { .spot, .ring, .card { transition: none; animation: none; } }
</style>
