<script lang="ts">
  import { tip as launcherTip } from './friends/tooltip'

  import { toasts, dismissToast, toastAnchor } from './toast.svelte'
  import { t as translate } from './i18n.svelte'
  import { App } from './wails'

  const icons: Record<string, string> = {
    success: 'ti-circle-check',
    error: 'ti-alert-circle',
    info: 'ti-info-circle',
    warn: 'ti-alert-triangle'
  }

  // Помилки/попередження можуть містити суть проблеми (стек, шлях, код
  // виходу) — користувачу треба скопіювати це одним кліком, не набираючи
  // текст руками. Клік по самому тосту закриває його, тому копіювання —
  // окрема кнопка, яка НЕ закриває тост і не плутається з довільною
  // action-кнопкою (напр. «Відкрити теку»).
  let copiedId = $state<number | null>(null)

  async function copyToast(e: MouseEvent, id: number, message: string) {
    e.stopPropagation()
    try {
      await App.CopyToClipboard(message)
      copiedId = id
      setTimeout(() => { if (copiedId === id) copiedId = null }, 1500)
    } catch { /* буфер обміну недоступний */ }
  }
</script>

{#if toasts.length > 0}
  <div class="toast-stack" class:stack-top={toastAnchor.pos === 'top'}>
    {#each toasts as t (t.id)}
      <div class="toast toast-{t.kind}" role="status" onclick={() => dismissToast(t.id)}>
        <span class="toast-ico"><i class="ti {icons[t.kind] ?? 'ti-info-circle'}"></i></span>
        <span class="toast-msg">{t.message}</span>
        {#if t.kind === 'error' || t.kind === 'warn'}
          <button
            class="toast-copy"
            class:copied={copiedId === t.id}
            use:launcherTip={translate('error.copy')}
            onclick={(e) => copyToast(e, t.id, t.message)}
          >
            <i class="ti {copiedId === t.id ? 'ti-check' : 'ti-copy'}"></i>
            {copiedId === t.id ? translate('error.copied') : translate('error.copy')}
          </button>
        {/if}
        {#if t.action}
          <button
            class="toast-act"
            onclick={(e) => { e.stopPropagation(); dismissToast(t.id); t.action?.onClick() }}
          >
            {t.action.label}
          </button>
        {/if}
        <i class="ti ti-x toast-x"></i>
      </div>
    {/each}
  </div>
{/if}

<style>
  .toast-stack {
    position: fixed;
    bottom: 18px;
    right: 18px;
    /* Має перекривати навіть повноекранний фоторедактор (.editor-overlay,
       z-index:2000000) — раніше тости мали нижчий z-index за редактор,
       тож повідомлення про збереження/копіювання, викликані З редактора,
       фізично малювались ПІД ним і були невидимі, поки редактор не
       закриєш. */
    z-index: 2100000;
    display: flex;
    flex-direction: column;
    gap: 8px;
    max-width: 380px;
  }
  /* Повноекранний майстер налаштування: тости зверху справа, щоб не
     перекривати кнопку «Далі» у футері майстра (див. toastAnchor у
     toast.svelte.ts). */
  .toast-stack.stack-top {
    bottom: auto;
    top: 18px;
  }
  .toast {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 12px 10px 10px;
    border-radius: 12px;
    background: var(--bg-card);
    border: 1px solid var(--border-hi);
    box-shadow: 0 18px 40px rgba(0, 0, 0, .6);
    font-size: 12.5px;
    font-weight: 600;
    cursor: pointer;
    animation: toastIn .42s cubic-bezier(.22, 1.2, .36, 1);
    backdrop-filter: blur(10px);
  }
  .toast-ico {
    flex-shrink: 0; width: 30px; height: 30px; border-radius: 8px; display: grid; place-items: center; font-size: 16px;
    animation: toastPop .45s cubic-bezier(.34, 1.56, .64, 1) backwards .1s;
  }
  @keyframes toastPop { from { transform: scale(.4); opacity: 0 } }
  .toast .toast-msg { min-width: 0; }
  .toast .toast-x { margin-left: auto; color: var(--text-dim); font-size: 13px; flex-shrink: 0; }
  .toast-act {
    flex-shrink: 0;
    padding: 5px 10px; border-radius: 8px;
    background: rgba(var(--orange-rgb), 0.14);
    border: 1px solid rgba(var(--orange-rgb), 0.4);
    color: var(--orange);
    font-family: inherit; font-size: 11.5px; font-weight: 700;
    cursor: pointer;
    transition: background .15s, transform .12s;
  }
  .toast-act:hover { background: rgba(var(--orange-rgb), 0.24); }
  .toast-act:active { transform: scale(.95); }
  .toast-copy {
    flex-shrink: 0;
    display: inline-flex; align-items: center; gap: 5px;
    padding: 5px 9px; border-radius: 8px;
    background: var(--bg-input); border: 1px solid var(--border);
    color: var(--text-mute);
    font-family: inherit; font-size: 11.5px; font-weight: 700;
    cursor: pointer;
    transition: background .15s, color .15s, border-color .15s, transform .12s;
  }
  .toast-copy i { font-size: 13px; }
  .toast-copy:hover { color: var(--text); border-color: var(--border-hi); background: var(--hover); }
  .toast-copy:active { transform: scale(.95); }
  .toast-copy.copied { color: var(--green); border-color: var(--green); }
  .toast:hover { border-color: var(--border-hi); }
  .toast-success .toast-ico { color: var(--green); background: rgba(16, 185, 129, .16); }
  .toast-error .toast-ico { color: var(--red); background: rgba(239, 68, 68, .15); }
  .toast-warn .toast-ico { color: var(--yellow); background: rgba(234, 179, 8, .15); }
  .toast-info .toast-ico { color: var(--blue); background: rgba(59, 130, 246, .15); }
  @keyframes toastIn { from { opacity: 0; transform: translateX(44px); } to { opacity: 1; transform: translateX(0); } }
  @media (prefers-reduced-motion: reduce) { .toast, .toast-ico { animation: none; } }
</style>
