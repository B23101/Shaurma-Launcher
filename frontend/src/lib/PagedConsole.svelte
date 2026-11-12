<script lang="ts">
  import { onMount, tick, untrack } from 'svelte'
  import type { ConsolePage, ConsolePageLoader } from './consolePage'
  import { t } from './i18n.svelte'
  let { load, filter = 'all', search = '', wrap = false, fontSize = 12,
    fontFamily = 'var(--font-mono)', colorInfo = '#8fd3ff', colorWarn = '#ffd166', colorError = '#ff6b6b',
    lineFont = (_level: string) => fontFamily, lineColor = (level: string) => level === 'error' ? colorError : level === 'warn' ? colorWarn : level === 'system' ? 'var(--text-dim)' : colorInfo, after = 0, revision = 0, onStats = (_p: ConsolePage) => {} }:
    { load: ConsolePageLoader; filter?: string; search?: string; wrap?: boolean; fontSize?: number;
      fontFamily?: string; colorInfo?: string; colorWarn?: string; colorError?: string;
      lineFont?: (level: string) => string; lineColor?: (level: string) => string; after?: number; revision?: number; onStats?: (p: ConsolePage) => void } = $props()
  let el: HTMLDivElement | undefined = $state()
  let page = $state.raw<ConsolePage>({ lines: [], total: 0, offset: 0, count: 0, errors: 0, warnings: 0, lastId: 0 })
  let following = $state(true)
  let failed = $state(false)
  let alive = true, busy = false, generation = 0
  // Запит прийшов, поки йшов попередній — не губимо його, а повторюємо одразу після.
  let again = false, retries = 0
  let programmaticUntil = 0
  let timer: ReturnType<typeof setTimeout> | undefined
  const limit = 300
  const row = $derived(fontSize + 10)
  // Only numeric heights survive eviction of text. Unknown wrapped rows use an estimate.
  let heights = $state.raw<Map<number, number>>(new Map())
  const positions = $derived.by(() => {
    const out = [0]
    for (let i = 0; i < page.total; i++) out.push(out[i] + (wrap ? heights.get(i) ?? row : row))
    return out
  })
  function indexAt(top: number) {
    let lo = 0, hi = page.total
    while (lo < hi) { const mid = (lo + hi) >>> 1; if (positions[mid] < top) lo = mid + 1; else hi = mid }
    return Math.max(0, lo - 1)
  }
  function parts(text: string) {
    if (!search) return [{ text, match: false }]
    const result: { text: string; match: boolean }[] = []
    const lower = text.toLowerCase(), query = search.toLowerCase()
    let from = 0, at = lower.indexOf(query)
    while (at >= 0) {
      result.push({ text: text.slice(from, at), match: false }, { text: text.slice(at, at + query.length), match: true })
      from = at + query.length; at = lower.indexOf(query, from)
    }
    result.push({ text: text.slice(from), match: false })
    return result
  }
  // Throttle, а не debounce: під час безперервної прокрутки (перетягування скролбара)
  // ділянки мають підвантажуватись, а не чекати, поки користувач зупиниться.
  function schedule() {
    if (timer) return
    timer = setTimeout(() => { timer = undefined; void refresh() }, 40)
  }
  // 'miss' — у вікні перегляду є рядки поза завантаженою ділянкою (порожнеча);
  // 'near' — наближаємось до краю ділянки, час підвантажувати.
  function coverage(): 'ok' | 'near' | 'miss' {
    if (!el || page.total === 0) return 'ok'
    const start = indexAt(el.scrollTop), end = indexAt(el.scrollTop + el.clientHeight)
    const first = page.offset, last = page.offset + page.lines.length
    if (start < first || end >= last) return 'miss'
    if ((first > 0 && start < first + 15) || (last < page.total && end > last - 16)) return 'near'
    return 'ok'
  }
  async function refresh() {
    if (!alive || document.hidden || !el) return
    if (busy) { again = true; return }
    // Користувач виділяє текст — не перемальовуємо рядки, інакше виділення зникає.
    // Але якщо у вікні перегляду порожнеча — підвантажуємо попри виділення.
    const sel = window.getSelection()
    if (sel && !sel.isCollapsed && sel.anchorNode && el.contains(sel.anchorNode) && coverage() !== 'miss') return
    busy = true
    again = false
    const token = generation
    const offset = following ? -1 : Math.max(0, indexAt(el.scrollTop) - 100)
    try {
      const next = await load(offset, limit, filter, search, after)
      if (!alive || token !== generation || document.hidden) return
      programmaticUntil = performance.now() + 150
      if (next.lastId - next.count !== page.lastId - page.count) heights = new Map()
      page = next
      failed = false
      onStats(next)
      await tick()
      if (!alive || token !== generation || !el) return
      if (wrap) {
        const measured = new Map(heights)
        el.querySelectorAll<HTMLElement>('[data-row]').forEach(node => measured.set(Number(node.dataset.row), node.getBoundingClientRect().height))
        heights = measured
        await tick()
      }
      if (following && el) el.scrollTop = el.scrollHeight
      // Поки вантажилось, користувач міг прокрутити далі — перевіряємо, що екран не порожній.
      if (!following && coverage() === 'miss' && retries < 5) { retries++; again = true } else retries = 0
    } catch { if (alive && token === generation) failed = true }
    finally { busy = false; if (alive && (again || token !== generation)) schedule() }
  }
  function scroll() {
    if (!el) return
    // Програмний scrollTop не повинен змінювати режим «слідкувати за кінцем»,
    // але перевірку покриття пропускати не можна — інакше рядки не підвантажуються.
    if (performance.now() >= programmaticUntil) following = el.scrollHeight - el.clientHeight - el.scrollTop < 48
    if (coverage() !== 'ok') schedule()
  }
  export function scrollToBottom() { following = true; schedule() }
  $effect(() => {
    void load; void filter; void search; void after; void revision; void wrap; void fontSize
    generation++
    untrack(() => { heights = new Map(); following = true; retries = 0; schedule() })
  })
  onMount(() => {
    const poll = setInterval(() => { void refresh() }, 1000)
    const visibility = () => {
      generation++
      if (document.hidden) page = { ...page, lines: [] }
      else schedule()
    }
    document.addEventListener('visibilitychange', visibility)
    const ro = new ResizeObserver(() => { heights = new Map(); schedule() })
    if (el) ro.observe(el)
    schedule()
    return () => { alive = false; generation++; clearTimeout(timer); timer = undefined; clearInterval(poll); ro.disconnect(); document.removeEventListener('visibilitychange', visibility) }
  })
</script>

<div class="paged-wrap">
  <div class="paged-term" role="log" bind:this={el} onscroll={scroll} style="font-size:{fontSize}px;font-family:{fontFamily}">
    {#if failed}<div class="hint"><button onclick={() => refresh()}>{t('retry')}</button></div>{/if}
    {#if page.total === 0}<div class="hint">{search ? t('console.noResults', { q: search }) : t('console.hint')}</div>{/if}
    <div style="height:{positions[page.offset] ?? 0}px"></div>
    {#each page.lines as line, i (`${page.offset + i}:${line.id}`)}
      <div data-row={page.offset + i} class="paged-line" class:wrap style="font-family:{lineFont(line.level)};min-height:{row}px;height:{wrap ? 'auto' : row + 'px'};color:{lineColor(line.level)}">
        {#if line.level !== 'banner'}<span class="time">{line.time}</span><span class="level">{line.level.toUpperCase()}</span>{/if}
        <span class="message">{#each parts(line.text) as part}{#if part.match}<mark>{part.text}</mark>{:else}{part.text}{/if}{/each}</span>
      </div>
    {/each}
    <div style="height:{Math.max(0, positions[page.total] - (positions[page.offset + page.lines.length] ?? 0))}px"></div>
  </div>
  {#if !following}<button class="bottom" onclick={scrollToBottom}>↓ {t('servers.console.toBottomShort')}</button>{/if}
</div>
<style>
  .paged-wrap { position:relative;flex:1;min-height:0;display:flex;flex-direction:column; }
  /* У додатку глобально user-select:none — для консолі явно вмикаємо виділення й копіювання. */
  .paged-term { flex:1;min-height:0;overflow:auto;padding:10px 14px;overflow-anchor:none;line-height:1.6;border:1px solid var(--border);border-radius:14px;background:var(--bg-card);user-select:text;-webkit-user-select:text;cursor:text; }
  .paged-line { display:flex;gap:10px;box-sizing:border-box;white-space:pre;user-select:text;-webkit-user-select:text; }
  .time { opacity:.5;flex-shrink:0;user-select:none;-webkit-user-select:none; } .level { width:52px;flex-shrink:0;opacity:.8;user-select:none;-webkit-user-select:none; }
  .message { min-width:0;user-select:text;-webkit-user-select:text; } .wrap .message { white-space:pre-wrap;overflow-wrap:anywhere; }
  .hint { padding:20px;color:var(--text-dim); }
  .bottom {
    position:absolute;right:24px;bottom:14px;z-index:2;display:inline-flex;align-items:center;gap:5px;
    padding:5px 12px;border-radius:20px;cursor:pointer;user-select:none;
    background:color-mix(in srgb, var(--orange) 18%, var(--bg-panel));
    border:1px solid color-mix(in srgb, var(--orange) 40%, transparent);
    color:var(--text);font:600 var(--fs-min)/1 var(--font-ui);box-shadow:0 6px 18px rgba(0,0,0,.35);
    transition:background .14s;
  }
  .bottom:hover { background:color-mix(in srgb, var(--orange) 30%, var(--bg-panel)); }
</style>
