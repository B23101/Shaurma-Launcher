<script lang="ts" module>
  // ── Термінал-логи сервера ─────────────────────────────────────────────
  // Той самий візуальний словник, що в GameConsole (сторінка збірки):
  // рядок = .line з .l-ts (час) + .l-lvl (INFO/WARN/ERR/SYS) + .l-msg,
  // підсвітка знайденого через <mark>, шрифт/кольори/перенос рядків — з
  // налаштувань консолі (consoleFontSize/consoleInfoColor/.../consoleWrap).
  // Раніше це був власний стиль (.cv-*) з часом у рядку і без підсвітки
  // пошуку — саме через це консоль сервера «не відповідала стилю консолі
  // збірок».
  export type LogLine = { id: number; time: string; level: string; text: string }
</script>

<script lang="ts">
  import { App } from '../../wails'
  import { t } from '../../i18n.svelte'
  import type { Settings } from '../../types'

  let {
    lines = [] as LogLine[],
    filter = 'all' as 'all' | 'info' | 'warn' | 'error',
    search = '',
    emptyIcon = 'ti-terminal',
    emptyText = t('servers.console.waiting'),
    onScrollBottomChange = (_atBottom: boolean) => {},
  }: {
    lines?: LogLine[]
    filter?: 'all' | 'info' | 'warn' | 'error'
    search?: string
    emptyIcon?: string
    emptyText?: string
    onScrollBottomChange?: (atBottom: boolean) => void
  } = $props()

  let termEl: HTMLDivElement | undefined = $state()
  let atBottom = $state(true)

  // ── Налаштування консолі (ті самі поля, що читає GameConsole) ──
  let consoleFontSize = $state(12)
  let consoleWrap = $state(false)
  let colorInfo = $state('#8fd3ff')
  let colorWarn = $state('#ffd166')
  let colorError = $state('#ff6b6b')

  $effect(() => {
    App.GetSettings().then((s: Settings) => {
      if (!s) return
      consoleFontSize = s.consoleFontSize || 12
      consoleWrap = !!s.consoleWrap
      colorInfo = s.consoleInfoColor || '#8fd3ff'
      colorWarn = s.consoleWarnColor || '#ffd166'
      colorError = s.consoleErrorColor || '#ff6b6b'
    }).catch(() => {})
  })

  function escapeHtml(s: string): string {
    return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')
  }

  // Підсвітка збігів пошуку — та сама реалізація, що в GameConsole
  // (регістронезалежний пошук по вже екранованому тексту).
  function highlight(text: string): string {
    const esc = escapeHtml(text)
    if (!search) return esc
    const q = search.toLowerCase()
    const lower = esc.toLowerCase()
    let out = ''
    let i = 0
    let idx = lower.indexOf(q)
    while (idx !== -1) {
      out += esc.slice(i, idx) + '<mark>' + esc.slice(idx, idx + q.length) + '</mark>'
      i = idx + q.length
      idx = lower.indexOf(q, i)
    }
    return out + esc.slice(i)
  }

  function levelLabel(lvl: string): string {
    if (lvl === 'error') return 'ERR'
    if (lvl === 'warn') return 'WARN'
    if (lvl === 'system') return 'SYS'
    return 'INFO'
  }

  function levelColor(lvl: string): string {
    if (lvl === 'error') return colorError
    if (lvl === 'warn') return colorWarn
    if (lvl === 'system') return 'var(--text-dim)'
    return colorInfo
  }

  // Толерантність «ми внизу»: 48px покриває fractional zoom/округлення,
  // коли scrollTop формально не дотягується до scrollHeight.
  const BOTTOM_SLACK = 48

  const filtered = $derived.by(() => {
    let result = lines
    if (filter !== 'all') result = result.filter(l => l.level === filter)
    if (search) {
      const q = search.toLowerCase()
      result = result.filter(l => l.text.toLowerCase().includes(q))
    }
    return result
  })

  // ── Віртуалізація ──────────────────────────────────────────────────────
  // Раніше у DOM лежали ВСІ рядки буфера (до 8000) з {@html} — відкриття вкладки
  // з великим логом «вішало» інтерфейс. Тепер рендериться лише видиме вікно
  // (висота рядка фіксована — 22px), а решту заміщають відступи.
  // У режимі переносу рядків висота змінна, тож там рендеримо «хвіст» буфера
  // з кнопкою «показати раніше».
  const ROW = 22
  const VIRT_MIN = 150
  const OVERSCAN = 40
  const WRAP_TAIL = 600
  let scrollTop = $state(0)
  let viewH = $state(600)
  let tailExtra = $state(0)
  let winRaf = 0

  function syncWindow() {
    if (!termEl) return
    scrollTop = termEl.scrollTop
    viewH = termEl.clientHeight || viewH
  }
  function scheduleWindow() {
    if (winRaf) return
    winRaf = requestAnimationFrame(() => { winRaf = 0; syncWindow() })
  }

  const virtual = $derived(!consoleWrap && filtered.length > VIRT_MIN)
  const vStart = $derived(virtual ? Math.max(0, Math.floor(scrollTop / ROW) - OVERSCAN) : 0)
  const vEnd = $derived(virtual ? Math.min(filtered.length, Math.ceil((scrollTop + viewH) / ROW) + OVERSCAN) : filtered.length)
  const wrapStart = $derived(consoleWrap ? Math.max(0, filtered.length - WRAP_TAIL - tailExtra) : 0)
  const visible = $derived(virtual ? filtered.slice(vStart, vEnd) : consoleWrap ? filtered.slice(wrapStart) : filtered)
  const padTop = $derived(virtual ? vStart * ROW : 0)
  const padBottom = $derived(virtual ? Math.max(0, filtered.length - vEnd) * ROW : 0)

  $effect(() => {
    if (!termEl || typeof ResizeObserver === 'undefined') return
    const ro = new ResizeObserver(() => scheduleWindow())
    ro.observe(termEl)
    return () => { ro.disconnect(); if (winRaf) cancelAnimationFrame(winRaf); winRaf = 0 }
  })

  function isNearBottom(el: HTMLDivElement): boolean {
    return el.scrollHeight - el.clientHeight - el.scrollTop <= BOTTOM_SLACK
  }

  // ── «Ми внизу» (sticky bottom) ─────────────────────────────────────────
  // Скрол-подія сама по собі НЕ означає «користувач пішов угору»: таку ж
  // подію викликає наш автоскрол і scroll-anchoring браузера (коли на капі
  // буфера зверху відсікаються старі рядки). Раніше така подія переводила
  // atBottom у false — і при ДУЖЕ великому потоці логів консоль раптово
  // «відклеювалась» від дна (автоскрол зупинявся), доки користувач не
  // натисне «До кінця». Тому:
  //   • явні дії користувача (wheel/touch/клавіші/повзунок) фіксуємо
  //     окремо — ЛИШЕ вони можуть поставити паузу;
  //   • скролі у вікні після оновлення логу вважаємо наслідком контенту
  //     чи нашого автоскролу — стан не чіпаємо.
  let userIntentAt = 0
  let contentChangedAt = 0
  const USER_INTENT_WINDOW_MS = 150
  const CONTENT_WINDOW_MS = 200
  function markUserIntent() { userIntentAt = performance.now() }

  function setAtBottom(v: boolean) {
    if (v === atBottom) return
    atBottom = v
    onScrollBottomChange(v)
  }

  function onScroll() {
    if (!termEl) return
    scheduleWindow()
    // Уже внизу (наш автоскрол довів) — фіксуємо «слідуємо за дном».
    if (isNearBottom(termEl)) { setAtBottom(true); return }
    const now = performance.now()
    // Свідомо піднялись вгору (wheel/дотик/клавіші/повзунок) — пауза.
    if (now - userIntentAt < USER_INTENT_WINDOW_MS) { setAtBottom(false); return }
    // Скрол стався відразу після оновлення логу (наш автоскрол або
    // anchoring) — це НЕ рішення користувача: лишаємось «внизу».
    if (now - contentChangedAt < CONTENT_WINDOW_MS) return
    setAtBottom(false)
  }

  export function scrollToBottom() {
    if (!termEl) return
    termEl.scrollTop = termEl.scrollHeight
    syncWindow()
    setAtBottom(true)
  }

  // Сигнал «з'явився новий рядок», що працює І на капі буфера:
  // filtered.length після CONSOLE_MAX_LINES постійний (8000) — ефект, що
  // залежить лише від нього, на довгому потоці більше не спрацьовував.
  // id монотонний (seq у useServerConsole), тож змінюється на КОЖЕН новий
  // рядок незалежно від обрізки старих зверху.
  const lastLineId = $derived(filtered.length ? filtered[filtered.length - 1].id : -1)

  // Автоскрол — ТУТ, а не в батьківському компоненті: рішення «ми внизу»
  // і сама прокрутка живуть в одному місці. Скролимо СИНХРОННО (без RAF):
  // вікно до наступного кадру давало скрол-події від anchoring встигнути
  // «відчепити» консоль від дна раніше, ніж ми до неї доїхали б.
  $effect(() => {
    void lastLineId
    void filter
    void search
    if (!termEl || !atBottom) return
    contentChangedAt = performance.now()
    termEl.scrollTop = termEl.scrollHeight
    syncWindow()
  })
</script>

<div class="cv-wrap" class:wrap={consoleWrap}>
  <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
  <!-- Обробники на скрол-контейнері — це ДЕТЕКЦІЯ НАМІРУ користувача
       (wheel/touch/pointer/клавіші) для sticky-bottom автоскролу, а не
       інтерактивний контент: div не може бути «інтерактивним» за ARIA, тож
       правило вимкнено свідомо. role="log" — семантика консолі. -->
  <div
    class="cv-term"
    role="log"
    bind:this={termEl}
    onscroll={onScroll}
    onwheel={markUserIntent}
    ontouchstart={markUserIntent}
    onpointerdown={markUserIntent}
    onkeydown={markUserIntent}
    style="font-size:{consoleFontSize}px"
  >
    {#if filtered.length === 0}
      <div class="cv-empty">
        <i class="ti {emptyIcon}"></i>
        <div>{search ? t('servers.console.notFound', { q: search }) : emptyText}</div>
      </div>
    {:else}
      {#if consoleWrap && wrapStart > 0}
        <button class="cv-earlier" onclick={() => (tailExtra += WRAP_TAIL)}>
          <i class="ti ti-chevrons-up"></i> {wrapStart}
        </button>
      {/if}
      <div class="cv-body" style="padding-top:{padTop}px;padding-bottom:{padBottom}px">
        {#each visible as line (line.id)}
          <div class="line {line.level}" style="color:{levelColor(line.level)}">
            <span class="l-ts">{line.time}</span>
            <span class="l-lvl">{levelLabel(line.level)}</span>
            <span class="l-msg">{@html highlight(line.text)}</span>
          </div>
        {/each}
      </div>
    {/if}
  </div>

  <!-- Повернення до кінця логу: показуємо лише коли користувач пішов
       вгору (тоді автоскрол навмисно на паузі). -->
  {#if !atBottom}
    <button class="cv-tobottom" onclick={scrollToBottom} title={t('servers.console.toBottom')}>
      <i class="ti ti-arrow-down"></i> {t('servers.console.toBottomShort')}
    </button>
  {/if}
</div>

<style>
  .cv-wrap { position: relative; flex: 1; min-height: 0; min-width: 0; display: flex; flex-direction: column; padding: 10px 12px 0; }

  /* Термінал: як .gc-term у GameConsole — вертикальний скрол для рядків і
     горизонтальний для дуже довгих (у wrap-режимі — без горизонтального). */
  .cv-term {
    flex: 1; min-height: 0; min-width: 0;
    overflow-y: auto; overflow-x: auto;
    padding: 10px 14px;
    font-family: var(--font-mono); font-size: 12px;
    /* «Картка» терміналу — як .gc-term у консолі збірки. */
    border-radius: 14px; border: 1px solid var(--border);
    background: color-mix(in srgb, var(--bg-app) 72%, var(--bg-panel));
    box-shadow: inset 0 1px 0 rgba(255,255,255,.035), 0 10px 30px rgba(0,0,0,.18);
  }
  .cv-term::-webkit-scrollbar { width: 9px; height: 9px; }
  .cv-term::-webkit-scrollbar-track { background: transparent; }
  .cv-term::-webkit-scrollbar-thumb { background: var(--hover-strong); border-radius: 6px; }
  .cv-term::-webkit-scrollbar-thumb:hover { background: var(--border-hi); }

  /* Рядок логу: рівно одна висота рядка, без переносу (як у Prism) —
     довгі рядки йдуть горизонтальним скролом контейнера. */
  .line {
    display: flex; gap: 10px; align-items: baseline;
    white-space: pre; line-height: 22px;
    user-select: text; -webkit-user-select: text;
    margin: 0 -8px; padding: 0 8px; border-radius: 6px;
    transition: background-color .12s ease;
  }
  .line:hover { background: color-mix(in srgb, var(--text) 5%, transparent); }
  .line .l-ts { color: var(--text-dim); flex-shrink: 0; font-size: 10.5px; user-select: none; -webkit-user-select: none; opacity: .8; font-variant-numeric: tabular-nums; }
  .line .l-lvl {
    flex-shrink: 0; width: 40px; text-align: center; font-weight: 800; font-size: 9.5px; letter-spacing: .4px;
    line-height: 16px; height: 16px; border-radius: 5px; align-self: center;
    background: color-mix(in srgb, currentColor 14%, transparent);
    box-shadow: inset 0 0 0 1px color-mix(in srgb, currentColor 22%, transparent);
  }
  .line.warn { background: color-mix(in srgb, #ffd166 6%, transparent); box-shadow: inset 2px 0 0 color-mix(in srgb, #ffd166 70%, transparent); }
  .line.error { background: color-mix(in srgb, #ff6b6b 9%, transparent); box-shadow: inset 2px 0 0 color-mix(in srgb, #ff6b6b 80%, transparent); }
  .line .l-msg { flex: 1; min-width: 0; }
  .line .l-msg :global(mark) { background: rgba(var(--orange-rgb), .4); color: #fff; border-radius: 2px; }

  /* Перенос довгих рядків (налаштування consoleWrap, як у Prism/GameConsole) */
  .cv-wrap.wrap .line {
    white-space: pre-wrap; word-break: break-word;
    height: auto; align-items: flex-start;
  }
  .cv-wrap.wrap .line .l-lvl { align-self: flex-start; margin-top: 3px; }

  .cv-body { min-width: max-content; }
  .cv-wrap.wrap .cv-body { min-width: 0; }
  .line { height: 22px; box-sizing: border-box; contain: layout style; }
  .cv-earlier {
    display: flex; align-items: center; justify-content: center; gap: 6px; width: 100%;
    margin: 0 0 6px; padding: 4px; cursor: pointer; border-radius: 8px;
    background: var(--hover); border: 1px dashed var(--border-hi); color: var(--text-dim);
    font: 600 10.5px/1 var(--font-ui);
  }
  .cv-earlier:hover { color: var(--text); }
  .cv-empty {
    display: flex; flex-direction: column; align-items: center; justify-content: center;
    height: 100%; gap: 10px; color: var(--text-dim);
    font-family: var(--font-mono);
  }
  .cv-empty i { font-size: 32px; }
  .cv-empty div { font-size: 12px; }

  .cv-tobottom {
    position: absolute; right: 28px; bottom: 14px; z-index: 2;
    display: inline-flex; align-items: center; gap: 5px;
    padding: 5px 10px; border-radius: 20px; cursor: pointer;
    background: color-mix(in srgb, var(--pcol, var(--orange)) 18%, var(--bg-panel));
    border: 1px solid color-mix(in srgb, var(--pcol, var(--orange)) 40%, transparent);
    color: var(--text); font: 600 10.5px/1 var(--font-ui);
    box-shadow: 0 6px 18px rgba(0, 0, 0, .35);
    transition: background .14s, color .14s;
  }
  .cv-tobottom:hover { background: color-mix(in srgb, var(--pcol, var(--orange)) 30%, var(--bg-panel)); }
  .cv-tobottom i { font-size: 12px; }
</style>
