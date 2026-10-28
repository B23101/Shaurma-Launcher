<script module lang="ts">
  // Стек відкритих модалок: Esc закриває лише ВЕРХНЮ (раніше одним Esc
  // закривались і модалка, і та, що під нею).
  const stack: symbol[] = []
</script>

<script lang="ts">
  // UiModal — СПІЛЬНА модалка лаунчера. Замінює 1000 копій
  // `.modal-overlay + .modal` з різними шапками в різних компонентах.
  //
  // Стиль шапки — як у «Міграції збірок» (LauncherMigrationModal):
  //   - іконка в кольоровому квадраті (акцент, градієнт),
  //   - заголовок + підзаголовок,
  //   - хрестик справа (обертається на hover).
  // Тіло скролиться, футер притиснутий донизу. Анімація появи — глобальна
  // (.modal-overlay/.modal у style.css), плюс ОПЦІЙНА анімація ЗНИКНЕННЯ
  // тут (fade+scale), якщо передати closing=true перед закриттям.
  //
  // Використання:
  //   <UiModal open={open} title={t('...')} subtitle={t('...')} icon="ti-box"
  //            color="var(--orange)" width="560px" onclose={close}>
  //     <div>тіло</div>
  //     {#snippet footer()}
  //       <button class="btn btn-primary" onclick={...}>OK</button>
  //     {/snippet}
  //   </UiModal>
  import { tick } from 'svelte'
  import Portal from './Portal.svelte'
  import { t } from './i18n.svelte'

  let {
    open = false,
    title = '',
    subtitle = '',
    icon = 'ti-box',
    color = 'var(--orange)',
    // Розмір: 'sm' (420) | 'md' (480) | 'lg' (600) | 'xl' (760) | числовий
    // рядок CSS (width перекриває).
    size = 'md',
    width = '',
    maxHeight = 'calc(100vh - 2 * var(--modal-gap, 30px))',
    // Блокувати закриття по кліку на тлі / Esc (під час довгих операцій).
    locked = false,
    // dismissible=false → вікно-вибір: без хрестика, Esc і клік по тлі не
    // закривають (лише кнопки у футері).
    dismissible = true,
    // Сховати шапку повністю (пошук-оверлеї, прогрес без заголовка).
    noHeader = false,
    // Сховати іконку (лише заголовок).
    noIcon = false,
    // z-index (за замовчуванням 1000000; модалки над модалками — вище).
    zIndex = '1000000',
    // Додатковий клас тіла (наприклад, сітка/пошук).
    bodyClass = '',
    // Додатковий клас самого вікна (напр. folder-modal — за ним drag&drop
    // розпізнає, що елемент відпустили всередині вікна).
    modalClass = '',
    // shake=true → модалка трясеться при появі (для попереджень / краху).
    shake = false,
    // draggable=true → вікно можна тягнути за шапку (гардероб).
    draggable = false,
    onclose = () => {},
    children,
    footer = null,
    // Кнопки в шапці праворуч від заголовка (перед хрестиком).
    actions = null,
  }: {
    open?: boolean
    title?: string
    subtitle?: string
    icon?: string
    color?: string
    size?: 'sm' | 'md' | 'lg' | 'xl'
    width?: string
    maxHeight?: string
    locked?: boolean
    dismissible?: boolean
    noHeader?: boolean
    noIcon?: boolean
    zIndex?: string | number
    bodyClass?: string
    modalClass?: string
    shake?: boolean
    draggable?: boolean
    onclose?: () => void
    children: import('svelte').Snippet
    footer?: import('svelte').Snippet | null
    actions?: import('svelte').Snippet | null
  } = $props()

  // Розміри за замовчуванням (можна перекрити width).
  const sizeMap: Record<string, string> = { sm: '420px', md: '480px', lg: '600px', xl: '760px' }
  const effWidth = $derived(width || sizeMap[size] || '480px')

  // ── Анімація зникнення ──
  // closing=true → наступний рендер додає клас .ui-out (fade+scale),
  // після чого батько має прибрати open. Тримаємо самі, щоб клік по тлі
  // і Esc грали ту саму анімацію.
  let closing = $state(false)
  let closeTimer: ReturnType<typeof setTimeout> | null = null

  function requestClose() {
    if (locked || !dismissible || closing) return
    closing = true
    closeTimer = setTimeout(() => {
      closing = false
      onclose()
    }, 160)
  }
  $effect(() => {
    if (!open) { closing = false; return }
    if (closeTimer) { clearTimeout(closeTimer); closeTimer = null }
  })

  // Esc: лише верхня модалка стеку; capture + stopPropagation, щоб ту саму
  // клавішу не обробили сторінка під модалкою чи модалка нижче.
  // Фокус: при відкритті — у вікно (якщо вміст не забрав його сам), Tab
  // зациклений усередині, при закритті повертається туди, де був.
  let dlg = $state<HTMLDivElement | null>(null)
  $effect(() => {
    if (!open) return
    const id = Symbol()
    stack.push(id)
    const prevFocus = document.activeElement as HTMLElement | null
    tick().then(() => {
      if (dlg && !dlg.contains(document.activeElement)) dlg.focus({ preventScroll: true })
    })
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape' || stack[stack.length - 1] !== id) return
      e.stopPropagation()
      requestClose()
    }
    window.addEventListener('keydown', onKey, true)
    return () => {
      window.removeEventListener('keydown', onKey, true)
      const i = stack.indexOf(id)
      if (i >= 0) stack.splice(i, 1)
      if (prevFocus?.isConnected) prevFocus.focus?.({ preventScroll: true })
    }
  })

  // Клік по тлі закриває, лише якщо і натискання, і відпускання були на тлі:
  // інакше drag усередині (3D-модель, повзунок, виділення тексту), відпущений
  // поза вікном, закривав модалку.
  let downOnOverlay = false
  function onOverlayDown(e: MouseEvent) { downOnOverlay = e.target === e.currentTarget }
  function onOverlayClick(e: MouseEvent) {
    if (e.target === e.currentTarget && downOnOverlay) requestClose()
    downOnOverlay = false
  }

  // ── Перетягування за шапку (draggable) ──
  // Зсув — через CSS translate (окрема властивість), щоб не конфліктувати
  // з анімаціями появи/зникнення, які працюють через transform.
  let dx = $state(0)
  let dy = $state(0)
  let drag: { x: number; y: number; dx: number; dy: number } | null = null
  $effect(() => { if (!open) { dx = 0; dy = 0 } })
  function onHeadDown(e: PointerEvent) {
    if (!draggable || (e.target as HTMLElement).closest('button, input, a')) return
    drag = { x: e.clientX, y: e.clientY, dx, dy }
    ;(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
  }
  function onHeadMove(e: PointerEvent) {
    if (!drag) return
    dx = drag.dx + (e.clientX - drag.x)
    dy = drag.dy + (e.clientY - drag.y)
  }
  function onHeadUp(e: PointerEvent) {
    drag = null
    const el = e.currentTarget as HTMLElement
    if (el.hasPointerCapture(e.pointerId)) el.releasePointerCapture(e.pointerId)
  }

  function trapTab(e: KeyboardEvent) {
    if (e.key !== 'Tab' || !dlg) return
    const els = [...dlg.querySelectorAll<HTMLElement>('button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), [href], [tabindex]:not([tabindex="-1"])')]
    if (!els.length) { e.preventDefault(); return }
    const first = els[0], last = els[els.length - 1]
    const a = document.activeElement
    if (e.shiftKey && (a === first || a === dlg)) { e.preventDefault(); last.focus() }
    else if (!e.shiftKey && a === last) { e.preventDefault(); first.focus() }
  }
</script>

{#if open}
  <Portal>
    <div class="ui-overlay ui-out" class:closing style="z-index:{zIndex}" role="presentation" onmousedown={onOverlayDown} onclick={onOverlayClick}>
      <div class="ui-modal {modalClass}" class:closing class:shake style="--ui-w:{effWidth};--ui-mh:{maxHeight};--ui-c:{color}{dx || dy ? `;translate:${dx}px ${dy}px` : ''}"
           role="dialog" aria-modal="true" aria-label={title || undefined} tabindex="-1" bind:this={dlg}
           onkeydown={trapTab} onclick={(e: any) => e.stopPropagation()}>
        {#if !noHeader}
          <div class="ui-head" class:drag={draggable} role="presentation"
               onpointerdown={onHeadDown} onpointermove={onHeadMove} onpointerup={onHeadUp} onpointercancel={onHeadUp}>
            <div class="ui-title-row">
              {#if !noIcon}
                <span class="ui-ico"><i class="ti {icon}"></i></span>
              {/if}
              <div class="ui-ttw">
                <div class="ui-tt">{title}</div>
                {#if subtitle}<div class="ui-td">{subtitle}</div>{/if}
              </div>
            </div>
            {#if actions}
              <div class="ui-acts">{@render actions()}</div>
            {/if}
            {#if dismissible}
              <button class="md-close ui-x" onclick={requestClose} disabled={locked} title={t('win.close')}>
                <i class="ti ti-x"></i>
              </button>
            {/if}
          </div>
        {/if}
        <div class="ui-body {bodyClass}">
          {@render children()}
        </div>
        {#if footer}
          <div class="ui-foot">
            {@render footer()}
          </div>
        {/if}
      </div>
    </div>
  </Portal>
{/if}

<style>
  .ui-overlay {
    position: fixed; inset: 0; z-index: 1000000;
    background: rgba(0,0,0,.65); backdrop-filter: blur(6px);
    display: flex; align-items: center; justify-content: center; padding: var(--modal-gap, 30px);
    animation: ui-overlay-in .22s ease-out both;
  }
  @keyframes ui-overlay-in { from { opacity: 0 } to { opacity: 1 } }
  .ui-overlay.closing { animation: ui-overlay-out .16s ease-in both; }
  @keyframes ui-overlay-out { from { opacity: 1 } to { opacity: 0 } }

  .ui-modal {
    background: var(--bg-panel); border: 1px solid var(--border-hi);
    border-radius: var(--r-lg); width: 100%; max-width: var(--ui-w);
    max-height: var(--ui-mh);
    box-shadow: 0 30px 60px rgba(0,0,0,.7);
    overflow: hidden; outline: none;
    display: flex; flex-direction: column;
    animation: ui-modal-in .26s cubic-bezier(.22,1.2,.36,1) both;
  }
  @keyframes ui-modal-in {
    from { opacity: 0; transform: translateY(14px) scale(.965); filter: blur(3px); }
    to   { opacity: 1; transform: none; filter: none; }
  }
  .ui-modal.closing { animation: ui-modal-out .16s ease-in both; }
  @keyframes ui-modal-out {
    from { opacity: 1; transform: none; filter: none; }
    to   { opacity: 0; transform: translateY(8px) scale(.98); filter: blur(2px); }
  }

  /* Shake — трясіння при появі для попереджень і краша */
  .ui-modal.shake {
    animation: ui-modal-in .26s cubic-bezier(.22,1.2,.36,1) both,
               ui-modal-shake .55s cubic-bezier(.36,.07,.19,.97) .2s both;
  }
  @keyframes ui-modal-shake {
    0%       { transform: none; }
    12%      { transform: translateX(-7px) rotate(-1.2deg); }
    24%      { transform: translateX( 6px) rotate( 1.0deg); }
    36%      { transform: translateX(-5px) rotate(-0.8deg); }
    48%      { transform: translateX( 4px) rotate( 0.6deg); }
    60%      { transform: translateX(-3px) rotate(-0.4deg); }
    72%      { transform: translateX( 2px) rotate( 0.2deg); }
    84%      { transform: translateX(-1px); }
    100%     { transform: none; }
  }

  @media (prefers-reduced-motion: reduce) {
    .ui-overlay, .ui-overlay.closing, .ui-modal, .ui-modal.closing { animation-duration: .01s; }
  }

  /* ── Шапка: як у міграції збірок ── */
  .ui-head {
    display: flex; align-items: flex-start; justify-content: space-between;
    gap: 10px; padding: 18px 20px 12px; border-bottom: 1px solid var(--border);
    flex: 0 0 auto;
  }
  .ui-head.drag { cursor: grab; user-select: none; touch-action: none; }
  .ui-head.drag:active { cursor: grabbing; }
  .ui-title-row { flex: 1 1 auto; display: flex; gap: 11px; align-items: center; min-width: 0; }
  .ui-acts { display: flex; flex-wrap: wrap; gap: 6px; justify-content: flex-end; align-items: center; }
  .ui-ico {
    width: 38px; height: 38px; border-radius: 10px; flex-shrink: 0;
    display: flex; align-items: center; justify-content: center;
    background: color-mix(in srgb, var(--ui-c, var(--orange)) 16%, transparent);
    color: var(--ui-c, var(--orange)); font-size: 19px;
  }
  /* Іконка в шапці модалки сиділа ВИЩЕ центру: у гліфів tabler вертикальні
     метрики зміщені відносно рядкового бокса, а flex центрує саме БОКС.
     line-height:1 + display:block прибирає зайвий «повітряний» рядок, а
     зсув на 1px вирівнює оптичний центр гліфа з центром плашки. */
  .ui-ico :global(.ti) { display: block; line-height: 1; transform: translateY(1px); }
  .ui-ttw { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .ui-tt { font-size: 15px; font-weight: 800; color: var(--text); }
  .ui-td { font-size: 11.5px; color: var(--text-mute); max-width: 420px; }
  .ui-x { flex-shrink: 0; }

  .ui-body { flex: 1; min-height: 0; overflow-y: auto; padding: 16px 20px; }
  .ui-body.no-pad { padding: 0; }
  /* Вміст сам керує скролом і висотою (редактор гардероба). */
  .ui-body.no-scroll { padding: 0; overflow: hidden; display: flex; flex-direction: column; }
  .ui-foot {
    flex: 0 0 auto; display: flex; gap: 8px; justify-content: flex-end; align-items: center;
    padding: 13px 20px; border-top: 1px solid var(--border);
  }
</style>
