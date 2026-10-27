<script lang="ts">
  // ── FModal — легка модалка розділу «Друзі» ───────────────────────────────
  // Те саме обличчя, що й у лаунчері (іконка в кольоровому квадраті, заголовок,
  // підзаголовок, хрестик), але без важких ефектів:
  //   • немає backdrop-filter / filter: blur — вони змушують WebView
  //     перемальовувати весь екран під модалкою кожен кадр анімації;
  //   • анімуються лише opacity і transform (композитор GPU, без layout);
  //   • `contain: layout paint` ізолює вміст — зміни всередині не чіпають сторінку;
  //   • will-change живе тільки на час появи/зникнення.
  // Фокус: при відкритті — у вікно, Tab зациклений, при закритті повертається
  // туди, де був. Esc і клік по тлі закривають (якщо не locked).
  import { onMount, tick } from 'svelte'
  import Portal from '../Portal.svelte'
  import { t } from '../i18n.svelte'

  let {
    open = false,
    title = '',
    subtitle = '',
    icon = 'ti-users',
    color = 'var(--orange)',
    width = 480,
    locked = false,
    zIndex = 1000001,
    onclose = () => {},
    children,
    footer = undefined,
  }: {
    open?: boolean
    title?: string
    subtitle?: string
    icon?: string
    color?: string
    width?: number
    locked?: boolean
    zIndex?: number
    onclose?: () => void
    children: import('svelte').Snippet
    footer?: import('svelte').Snippet
  } = $props()

  // Лишаємо вміст у DOM на час анімації зникнення.
  let mounted = $state(false)
  let closing = $state(false)
  let timer: ReturnType<typeof setTimeout> | undefined
  let dlg = $state<HTMLDivElement | null>(null)
  let prevFocus: HTMLElement | null = null

  $effect(() => {
    if (open) {
      clearTimeout(timer)
      closing = false
      if (!mounted) {
        prevFocus = (document.activeElement as HTMLElement) ?? null
        mounted = true
        tick().then(() => dlg?.focus({ preventScroll: true }))
      }
    } else if (mounted && !closing) {
      closing = true
      timer = setTimeout(() => {
        mounted = false
        closing = false
        prevFocus?.focus?.({ preventScroll: true })
        prevFocus = null
      }, 150)
    }
  })

  function request() { if (!locked) onclose() }

  onMount(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && open && !locked) { e.stopPropagation(); request() }
    }
    window.addEventListener('keydown', onKey, true)
    return () => { window.removeEventListener('keydown', onKey, true); clearTimeout(timer) }
  })

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

{#if mounted}
  <Portal>
    <div class="fm-overlay" class:out={closing} style="z-index:{zIndex}" role="presentation"
         onpointerdown={(e) => { if (e.target === e.currentTarget) request() }}>
      <div class="fm" class:out={closing} role="dialog" aria-modal="true" aria-label={title} tabindex="-1" bind:this={dlg}
           style="--fm-w:{width}px; --fm-c:{color}" onkeydown={trapTab}>
        <header class="fm-head">
          <span class="fm-ico"><i class="ti {icon}"></i></span>
          <div class="fm-tt">
            <div class="fm-t">{title}</div>
            {#if subtitle}<div class="fm-s">{subtitle}</div>{/if}
          </div>
          <button class="fm-x" onclick={request} disabled={locked} aria-label={t('friends.close')}><i class="ti ti-x"></i></button>
        </header>
        <div class="fm-body">{@render children()}</div>
        {#if footer}<footer class="fm-foot">{@render footer()}</footer>{/if}
      </div>
    </div>
  </Portal>
{/if}

<style>
  .fm-overlay {
    position: fixed; inset: 0; display: flex; align-items: center; justify-content: center;
    padding: 24px; background: rgba(0, 0, 0, .58);
    animation: fm-fade .18s ease-out both;
  }
  .fm-overlay.out { animation: fm-fade-out .15s ease-in both; }
  .fm {
    display: flex; flex-direction: column; width: 100%; max-width: var(--fm-w); max-height: min(720px, calc(100vh - 48px));
    background: var(--bg-panel); border: 1px solid var(--border-hi); border-radius: 24px;
    box-shadow: 0 30px 80px rgba(0, 0, 0, .6); outline: none;
    contain: layout paint; will-change: transform, opacity;
    animation: fm-in .22s var(--ff-ease) both;
  }
  .fm.out { animation: fm-out .15s ease-in both; }
  @keyframes fm-fade { from { opacity: 0 } to { opacity: 1 } }
  @keyframes fm-fade-out { from { opacity: 1 } to { opacity: 0 } }
  @keyframes fm-in { from { opacity: 0; transform: translateY(12px) scale(.97); } to { opacity: 1; transform: none; } }
  @keyframes fm-out { from { opacity: 1; transform: none; } to { opacity: 0; transform: translateY(6px) scale(.98); } }

  .fm-head { display: flex; align-items: center; gap: 14px; padding: 20px 18px 16px 22px; border-bottom: 1px solid var(--border); flex: 0 0 auto; background: radial-gradient(360px 90px at 0% 0%, color-mix(in srgb, var(--fm-c) 16%, transparent), transparent 75%); }
  .fm-ico { flex: 0 0 auto; width: 46px; height: 46px; border-radius: 15px; display: grid; place-items: center; font-size: 22px; color: #fff;
    background: linear-gradient(135deg, var(--fm-c), color-mix(in srgb, var(--fm-c) 65%, #000)); box-shadow: 0 10px 22px color-mix(in srgb, var(--fm-c) 35%, transparent); }
  .fm-ico :global(.ti) { display: block; line-height: 1; }
  .fm-tt { flex: 1; min-width: 0; }
  .fm-t { font: 800 17px/1.25 var(--font-ui); color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .fm-s { margin-top: 2px; font: 500 11.5px/1.35 var(--font-ui); color: var(--text-mute); }
  .fm-x { flex: 0 0 auto; width: 32px; height: 32px; display: grid; place-items: center; border: 0; border-radius: 9px; background: transparent; color: var(--text-mute); font-size: 17px; cursor: pointer; transition: background-color .14s, color .14s, transform .18s; }
  .fm-x:hover:not(:disabled) { background: var(--hover); color: var(--text); transform: rotate(90deg); }
  .fm-x:focus-visible { outline: 2px solid color-mix(in srgb, var(--orange) 70%, transparent); outline-offset: 1px; }

  .fm-body { flex: 1 1 auto; min-height: 0; overflow-y: auto; overscroll-behavior: contain; padding: 20px 22px; }
  .fm-foot { flex: 0 0 auto; display: flex; justify-content: flex-end; align-items: center; gap: 8px; padding: 14px 22px; border-top: 1px solid var(--border); }

  @media (max-width: 520px) { .fm-overlay { padding: 10px; } .fm-body { padding: 14px; } }
  @media (prefers-reduced-motion: reduce) {
    .fm-overlay, .fm, .fm-overlay.out, .fm.out { animation-duration: .01s; }
    .fm-x:hover:not(:disabled) { transform: none; }
  }
</style>
