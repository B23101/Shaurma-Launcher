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
    maxHeight = 'calc(100vh - 60px)',
    // Блокувати закриття по кліку на тлі / Esc (під час довгих операцій).
    locked = false,
    // Сховати шапку повністю (пошук-оверлеї, прогрес без заголовка).
    noHeader = false,
    // Сховати іконку (лише заголовок).
    noIcon = false,
    // z-index (за замовчуванням 1000000; модалки над модалками — вище).
    zIndex = '1000000',
    // Додатковий клас тіла (наприклад, сітка/пошук).
    bodyClass = '',
    // shake=true → модалка трясеться при появі (для попереджень / краху).
    shake = false,
    onclose = () => {},
    children,
    footer = null,
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
    noHeader?: boolean
    noIcon?: boolean
    zIndex?: string
    bodyClass?: string
    shake?: boolean
    onclose?: () => void
    children: import('svelte').Snippet
    footer?: import('svelte').Snippet | null
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
    if (locked || closing) return
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

  $effect(() => {
    if (!open) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !locked) requestClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })
</script>

{#if open}
  <Portal>
    <div class="ui-overlay ui-out" class:closing style="z-index:{zIndex}" onclick={(e) => { if (e.target === e.currentTarget) requestClose() }}>
      <div class="ui-modal" class:closing class:shake style="--ui-w:{effWidth};--ui-mh:{maxHeight};--ui-c:{color}" onclick={(e: any) => e.stopPropagation()}>
        {#if !noHeader}
          <div class="ui-head">
            <div class="ui-title-row">
              {#if !noIcon}
                <span class="ui-ico"><i class="ti {icon}"></i></span>
              {/if}
              <div class="ui-ttw">
                <div class="ui-tt">{title}</div>
                {#if subtitle}<div class="ui-td">{subtitle}</div>{/if}
              </div>
            </div>
            <button class="md-close ui-x" onclick={requestClose} disabled={locked} title={t('win.close')}>
              <i class="ti ti-x"></i>
            </button>
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
    display: flex; align-items: center; justify-content: center; padding: 30px;
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
    overflow: hidden;
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
  .ui-title-row { display: flex; gap: 11px; align-items: center; min-width: 0; }
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
  .ui-foot {
    flex: 0 0 auto; display: flex; gap: 8px; justify-content: flex-end; align-items: center;
    padding: 13px 20px; border-top: 1px solid var(--border);
  }
</style>
