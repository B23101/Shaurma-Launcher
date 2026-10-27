<script lang="ts">
  // ══════════════════════════════════════════════════════════════════════
  // WardrobeModal — модальне вікно гардероба (редизайн 2026).
  //
  //   • fixed-overlay на весь в'юпорт (без pf-лобової роботи з portal:
  //     предки .content/.main не мають transform/filter, тож position:fixed
  //     рахується від вікна, а з високим z-index оверлей накриває і сайдбар);
  //   • backdrop blur + click-to-close;
  //   • spring scale-in панелі, короткий fade-out на закритті;
  //   • ESC-закриття + блокування скролу сторінки під час показу;
  //   • draggable-шапка (pointer capture, без прив'язки до курсору на move);
  //   • тема: усі кольори — з глобальних токенів (--bg-panel/--border/
  //     --text/--orange…), тож модалка стежить за dark/light/OLED/акцентом;
  //   • accent='orange'|'violet' фарбує іконковий чіп у шапці;
  //   • size='md'|'lg'|'xl' задає ширину панелі.
  //
  // Клієнти передають вміст через снипети body() і foot(), а заголовок —
  // через пропси title/icon (як-от <WardrobeModal title={t(...)} icon="ti-edit">).
  // ══════════════════════════════════════════════════════════════════════

  import { tick } from 'svelte'
  import { t } from './i18n.svelte'

  let {
    open = false,
    onClose = () => {},
    title = '',
    icon = 'ti-box',
    accent = 'orange',
    size = 'md',
    bodyNoScroll = false,
    body,
    foot,
  }: {
    open?: boolean
    onClose?: () => void
    title?: string
    icon?: string
    accent?: 'orange' | 'violet'
    size?: 'md' | 'lg' | 'xl'
    // Коли вміст сам керує скролом (напр. редактор пресету: 3D-картка
    // розтягується на доступне місце, права колонка скролиться сама) —
    // тіло модалки НЕ скролиться і не має власних відступів.
    bodyNoScroll?: boolean
    body?: import('svelte').Snippet
    foot?: import('svelte').Snippet
  } = $props()

  // Чи панель реально змонтована (на час fade-out анімації). closeAnim —
  // стадія «виїжджаємо»: по її завершенню викликаємо onClose, і батько
  // скидає open → ефект розмонтовує панель.
  let shown = $state(false)
  let closing = $state(false)

  // Зміщення панелі від центру (drag за шапку).
  let dx = $state(0)
  let dy = $state(0)
  let dragStart = $state<{ x: number; y: number; dx: number; dy: number } | null>(null)

  let panelEl = $state<HTMLDivElement | undefined>()

  // Синхронізація з пропом open: монтуємо панель, блокуємо скрол сторінки
  // і фокусуємо панель (для коректної роботи ESC + a11y).
  $effect(() => {
    if (open) {
      shown = true
      closing = false
      // Блокуємо вертикальний скрол фону під модалкою.
      document.body.style.overflow = 'hidden'
      tick().then(() => panelEl?.focus())
      return () => {
        document.body.style.overflow = ''
      }
    } else {
      shown = false
      closing = false
    }
  })

  // Закриття з анімацією: ставимо клас closing, чекаємо 160ms (fade-out)
  // і лише тоді повідомляємо батька.
  function requestClose() {
    if (closing) return
    closing = true
    setTimeout(() => {
      closing = false
      dx = 0
      dy = 0
      onClose()
    }, 160)
  }

  // ── Потягнути за шапку (pointer capture) ──
  function onHeadDown(e: PointerEvent) {
    if ((e.target as HTMLElement).closest('.wmd-close')) return
    dragStart = { x: e.clientX, y: e.clientY, dx, dy }
    ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
    // will-change під час drag лише (гладко, без постійного розмиття тексту).
    panelEl?.classList.add('dragging')
  }
  function onHeadMove(e: PointerEvent) {
    if (!dragStart) return
    dx = dragStart.dx + (e.clientX - dragStart.x)
    dy = dragStart.dy + (e.clientY - dragStart.y)
  }
  function endDrag(e: PointerEvent) {
    dragStart = null
    panelEl?.classList.remove('dragging')
    if (e.currentTarget instanceof HTMLElement && e.currentTarget.hasPointerCapture(e.pointerId)) {
      e.currentTarget.releasePointerCapture(e.pointerId)
    }
  }

  // ── Клік по фону закриває модалку, але ТІЛЬКИ якщо весь клік (down + up)
  //    стався саме на оверлеї. Без цього перетягування 3D-моделі в сцені
  //    (mousedown на canvas → mousemove/mouseup слухаються на window, бо
  //    інакше drag губився б при виході курсору за canvas) генерує click
  //    з target === overlay, коли відпускаєш мишку поза панеллю — і
  //    модалка передчасно закривалась посеред обертання/копіювання/
  //    редагування скіна.
  let overlayMouseDownOnSelf = false
  function onOverlayMouseDown(e: MouseEvent) {
    overlayMouseDownOnSelf = e.target === e.currentTarget
  }
  function onOverlayClick(e: MouseEvent) {
    if (e.target === e.currentTarget && overlayMouseDownOnSelf) requestClose()
    overlayMouseDownOnSelf = false
  }

  // ESC — закрити. Появляється лише коли панель відкрита.
  $effect(() => {
    if (!open) return
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') requestClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })
</script>

{#if shown}
  <!-- Оверлей: fixed на весь в'юпорт. Клік по САМОМУ оверлею (не панелі)
       закриває модалку. class:closing відтворює fade-out. -->
  <div
    class="wmd-overlay {closing ? 'closing' : ''}"
    onmousedown={onOverlayMouseDown}
    onclick={onOverlayClick}
  >
    <!-- Тримач по центру; drag-зміщення dx/dy накладається через transform.
         Scale-in/out анімації живуть на внутрішній панелі (щоб не конфлік-
         тувати з drag-трансформом). -->
    <div
      class="wmd-holder"
      style={`transform: translate(calc(-50% + ${dx}px), calc(-50% + ${dy}px))`}
    >
      <div
        class="wmd-panel wmd-accent-{accent} wmd-size-{size}"
        class:closing={closing}
        role="dialog"
        aria-modal="true"
        aria-label={title || undefined}
        tabindex="-1"
        bind:this={panelEl}
      >
        <!-- Шапка: елемент drag'у (пропускає кліки на .wmd-close). -->
        <div
          class="wmd-head"
          onpointerdown={onHeadDown}
          onpointermove={onHeadMove}
          onpointerup={endDrag}
          onpointercancel={endDrag}
        >
          <div class="wmd-head-icon"><i class="ti {icon}"></i></div>
          <div class="wmd-title">{title}</div>
          <button
            class="wmd-close icon-btn"
            onclick={requestClose}
            aria-label={t('wardrobe.modal.close')}
            title={t('wardrobe.modal.close')}
          >
            <i class="ti ti-x"></i>
          </button>
        </div>

        <!-- Контент: скролиться незалежно від футера. Якщо bodyNoScroll —
             скролом керує сам вміст (3D картка + права колонка редактора). -->
        <div class="wmd-body {bodyNoScroll ? 'noscroll' : ''}">
          {#if body}{@render body()}{:else}<div class="wmd-empty">—</div>{/if}
        </div>

        <!-- Футер (кнопки дій). -->
        {#if foot}
          <div class="wmd-foot">{@render foot()}</div>
        {/if}
      </div>
    </div>
  </div>
{/if}

<style>
  /* ══════════════ ОВЕРЛЕЙ ══════════════ */
  .wmd-overlay {
    position: fixed; inset: 0;
    z-index: 10000;
    display: flex; align-items: center; justify-content: center;
    background: color-mix(in srgb, var(--bg-app) 55%, rgba(0, 0, 0, 0.35));
    backdrop-filter: blur(6px);
    -webkit-backdrop-filter: blur(6px);
    animation: wmd-fade-in 0.18s ease both;
  }
  .wmd-overlay.closing { animation: wmd-fade-out 0.16s ease both; }
  @keyframes wmd-fade-in  { from { opacity: 0; } to { opacity: 1; } }
  @keyframes wmd-fade-out { from { opacity: 1; } to { opacity: 0; } }

  /* ══════════════ ПАНЕЛЬ ══════════════ */
  .wmd-holder {
    position: fixed; left: 50%; top: 50%;
    z-index: 10001;
  }
  /* Під час перетягування модалки даємо композитору підказку (плавний drag);
     у спокої will-change прибрано — інакше текст у WebView2 лишається
     розмитим через повторну растерізацію шару. */
  .wmd-panel.dragging { will-change: transform; }
  .wmd-panel {
    position: relative;
    width: min(var(--wmd-w, 520px), calc(100vw - 40px));
    max-height: min(88vh, 900px);
    display: flex; flex-direction: column;
    border-radius: var(--wr2-radius, 18px);
    background: var(--bg-panel);
    border: 1px solid var(--border-hi);
    box-shadow: 0 28px 90px rgba(0, 0, 0, 0.45);
    overflow: hidden;
    /* М'яка поява без overshoot — з will-change/spring текст у WebView2
       розмивається під час та після анімації. */
    animation: wmd-scale-in 0.22s cubic-bezier(.25, .9, .35, 1) both;
  }
  .wmd-panel.closing { animation: wmd-scale-out 0.15s ease both; }

  .wmd-size-md { --wmd-w: 520px; }
  .wmd-size-lg { --wmd-w: 680px; }
  .wmd-size-xl { --wmd-w: 940px; }

  @keyframes wmd-scale-in {
    from { opacity: 0; transform: scale(0.97) translateY(6px); }
    to   { opacity: 1; transform: scale(1) translateY(0); }
  }
  @keyframes wmd-scale-out {
    from { opacity: 1; transform: scale(1); }
    to   { opacity: 0; transform: scale(0.97) translateY(4px); }
  }

  /* ══════════════ ШАПКА ══════════════ */
  .wmd-head {
    display: flex; align-items: center; gap: 12px;
    padding: 16px 16px 14px 18px;
    cursor: grab;
    user-select: none;
    flex-shrink: 0;
    border-bottom: 1px solid var(--border);
    touch-action: none; /* drag не конфліктує зі скролом/жестами WebView */
  }
  .wmd-head:active { cursor: grabbing; }

  .wmd-head-icon {
    width: 36px; height: 36px; flex-shrink: 0;
    display: flex; align-items: center; justify-content: center;
    border-radius: 10px;
    background: linear-gradient(135deg, var(--wmd-accent, var(--orange)), var(--wmd-accent-deep, var(--orange-2)));
    color: #fff;
    font-size: 17px;
    box-shadow: 0 6px 18px rgba(var(--wmd-rgb, 255, 138, 0), 0.35);
  }
  .wmd-title {
    flex: 1; min-width: 0;
    font-size: 14px; font-weight: 800; letter-spacing: -0.1px;
    color: var(--text);
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .wmd-close { border: none; }

  /* Акцентні варіанти чіпа (проп accent). */
  .wmd-accent-orange { --wmd-accent: var(--orange); --wmd-accent-deep: var(--orange-2); --wmd-rgb: var(--orange-rgb); }
  .wmd-accent-violet { --wmd-accent: #7b4dff; --wmd-accent-deep: #9d7cff; --wmd-rgb: 123, 77, 255; }

  /* ══════════════ КОНТЕНТ + ФУТЕР ══════════════ */
  .wmd-body {
    flex: 1; min-height: 0;
    overflow-y: auto;
    padding: 18px;
    color: var(--text);
    overscroll-behavior: contain;
    scrollbar-width: thin;
    scrollbar-color: color-mix(in srgb, var(--wr2-white, #fff) 10%, transparent) transparent;
  }
  .wmd-body::-webkit-scrollbar { width: 6px; }
  .wmd-body::-webkit-scrollbar-thumb { background: color-mix(in srgb, var(--wr2-white, #fff) 10%, transparent); border-radius: 3px; }
  /* bodyNoScroll: вміст займає рівно всю висоту тіла і сам розкладається
     (без подвійного скролу модалки й внутрішніх колонок). */
  .wmd-body.noscroll {
    overflow: hidden;
    padding: 0;
    display: flex;
    flex-direction: column;
  }

  .wmd-foot {
    display: flex; align-items: center; justify-content: flex-end; gap: 10px;
    padding: 14px 18px;
    border-top: 1px solid var(--border);
    background: color-mix(in srgb, var(--bg-card) 55%, transparent);
    flex-shrink: 0;
  }

  .wmd-empty { color: var(--text-dim); font-size: 12px; }

  /* Швидкі пресети кнопок зі стилістикою гардероба (wr2-btn-*) — на випадок,
     коли футер містить самі голі <button>, без класів. */
  .wmd-foot :global(.btn), .wmd-foot :global(button) {
    font-family: inherit;
  }
</style>