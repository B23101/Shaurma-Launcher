<script lang="ts">
  import { toasts, dismissToast } from './toast.svelte'

  const icons: Record<string, string> = {
    success: 'ti-circle-check',
    error: 'ti-alert-circle',
    info: 'ti-info-circle',
    warn: 'ti-alert-triangle'
  }
</script>

{#if toasts.length > 0}
  <div class="toast-stack">
    {#each toasts as t (t.id)}
      <div class="toast toast-{t.kind}" role="status" onclick={() => dismissToast(t.id)}>
        <i class="ti {icons[t.kind] ?? 'ti-info-circle'}"></i>
        <span>{t.message}</span>
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
    z-index: 999999;
    display: flex;
    flex-direction: column;
    gap: 8px;
    max-width: 380px;
  }
  .toast {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 12px 14px;
    border-radius: 12px;
    background: var(--bg-panel);
    border: 1px solid var(--border-hi);
    box-shadow: 0 18px 40px rgba(0, 0, 0, .6);
    font-size: 12.5px;
    font-weight: 600;
    cursor: pointer;
    animation: toastIn .25s ease;
    backdrop-filter: blur(10px);
  }
  .toast i:first-child { font-size: 17px; }
  .toast .toast-x { margin-left: auto; color: var(--text-dim); font-size: 13px; }
  .toast:hover { border-color: var(--border-hi); }
  .toast-success i:first-child { color: var(--green); }
  .toast-error i:first-child { color: var(--red); }
  .toast-warn i:first-child { color: var(--yellow); }
  .toast-info i:first-child { color: var(--blue); }
  @keyframes toastIn { from { opacity: 0; transform: translateX(20px); } to { opacity: 1; transform: translateX(0); } }
</style>
