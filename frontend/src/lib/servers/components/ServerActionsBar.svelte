<script lang="ts">
  // ── Кнопки керування сервером (Старт/Стоп/Kill) ──────────────────────
  import { t } from '../../i18n.svelte'

  let {
    displayStatus = 'stopped',
    actionBusy = false,
    onStart = () => {},
    onStop = () => {},
    onKill = () => {},
    // Сервер друга: лише ті дії, на які є право; Kill — ніколи.
    canStart = true,
    canStop = true,
    canKill = true,
  }: {
    displayStatus?: string
    actionBusy?: boolean
    onStart?: () => void
    onStop?: () => void
    onKill?: () => void
    canStart?: boolean
    canStop?: boolean
    canKill?: boolean
  } = $props()

  const isRunning = $derived(displayStatus === 'running')
  const isBusyState = $derived(['starting', 'stopping', 'installing'].includes(displayStatus))
</script>

<div class="sab">
  {#if isRunning}
    {#if canStop}
      <button class="sab-btn stop" onclick={onStop} disabled={actionBusy} title={t('servers.actions.stopTitle')}><i class="ti ti-player-stop"></i> {t('servers.actions.stop')}</button>
    {/if}
    {#if canKill}
      <button class="sab-btn kill" onclick={onKill} title={t('servers.actions.kill')} aria-label={t('servers.actions.kill')}><i class="ti ti-skull"></i></button>
    {/if}
  {:else if isBusyState}
    <button class="sab-btn busy" disabled><i class="ti ti-loader-2 spin"></i> {displayStatus === 'stopping' ? t('servers.status.stopping') : t('servers.actions.working')}</button>
    {#if displayStatus !== 'stopping' && canKill}
      <button class="sab-btn kill" onclick={onKill} title={t('servers.actions.kill')} aria-label={t('servers.actions.kill')}><i class="ti ti-skull"></i></button>
    {/if}
  {:else if canStart}
    <button class="sab-btn primary" onclick={onStart} disabled={actionBusy}><i class="ti ti-player-play"></i> {t('servers.actions.start')}</button>
  {/if}
</div>

<style>
  .sab { display: flex; gap: 6px; }
  .sab-btn {
    height: 34px; padding: 0 15px; border-radius: 10px; font: 700 12.5px/1 var(--font-ui);
    display: inline-flex; align-items: center; gap: 6px; cursor: pointer;
    border: 1px solid transparent; color: #fff;
    transition: background .15s, transform .15s, box-shadow .2s, filter .15s;
  }
  .sab-btn i { font-size: 15px; }
  .sab-btn:not(:disabled):hover { transform: translateY(-1px); }
  .sab-btn:not(:disabled):active { transform: translateY(0) scale(.96); }
  .sab-btn.kill { width: 34px; padding: 0; justify-content: center; }

  .sab-btn.primary { background: #22c55e; box-shadow: 0 8px 20px -6px rgba(34, 197, 94, .7); color: #06230f; }
  .sab-btn.primary:hover { background: #2ee56c; }
  .sab-btn.stop { background: #eab308; color: #241a02; box-shadow: 0 8px 20px -6px rgba(234, 179, 8, .6); }
  .sab-btn.stop:hover { background: #facc15; }
  .sab-btn.busy { background: rgba(255, 255, 255, .12); border-color: rgba(255, 255, 255, .18); color: #fff; }
  .sab-btn.kill { background: rgba(239, 68, 68, .2); border-color: rgba(239, 68, 68, .55); color: #ffb0b0; }
  .sab-btn.kill:hover { background: #ef4444; color: #fff; }
  .sab-btn:disabled { opacity: .75; cursor: default; }
  .spin { animation: sab-spin .9s linear infinite; }
  @keyframes sab-spin { to { transform: rotate(360deg); } }
  @media (prefers-reduced-motion: reduce) { .spin { animation: none; } }
</style>
