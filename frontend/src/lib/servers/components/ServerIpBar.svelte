<script lang="ts">
  // ── IP-рядок під банером ──────────────────────────────────────────────
  // ЛИШЕ локальна адреса сервера (або адреса проксі, коли є Velocity).
  // Публічний доступ до сервера з інтернету — це власна P2P-система
  // (ServerP2PWidget, Headscale/tsnet), окремо від цього рядка.
  import { toast } from '../../toast.svelte'
  import { t } from '../../i18n.svelte'

  let {
    port = 25565,
    isRunning = false,
    isVelocity = false,
    velocityProxyRunning = false,
  }: {
    port?: number
    isRunning?: boolean
    isVelocity?: boolean
    velocityProxyRunning?: boolean
  } = $props()

  async function copy(text: string) {
    try { await navigator.clipboard.writeText(text); toast(t('servers.ip.copied'), 'success') }
    catch { toast(t('servers.ip.copyFailed'), 'error') }
  }
</script>

<!-- Адреса показується ЗАВЖДИ, а не лише коли сервер запущено: локальний
     IP (localhost:порт) — це те, що гравець вставляє в гру, і копіювати
     його має бути можна до старту (скарга «локальний ip можна лише
     копіювати коли запущений p2p»). Запущений сервер лише додає позначку
     «через проксі» для backend-серверів Velocity. -->
<div class="sip">
  {#if isVelocity}
    <div class="sip-chip">
      <i class="ti ti-router"></i>
      <span class="sip-lbl">{t('servers.ip.proxy')}</span>
      <span class="sip-addr">localhost:{port}</span>
      <button class="sip-copy" onclick={() => copy(`localhost:${port}`)} title={t('servers.ip.copyAddr')} aria-label={t('servers.ip.copyAddr')}><i class="ti ti-copy"></i></button>
    </div>
  {:else if velocityProxyRunning && isRunning}
    <div class="sip-chip velocity">
      <i class="ti ti-network"></i>
      <span class="sip-lbl">{t('servers.ip.viaProxy')}</span>
      <span class="sip-addr">localhost:{port}</span>
      <span class="sip-note">{t('servers.ip.proxyRunning')}</span>
    </div>
  {:else}
    <div class="sip-chip">
      <i class="ti ti-world"></i>
      <span class="sip-lbl">{t('servers.ip.local')}</span>
      <span class="sip-addr">localhost:{port}</span>
      <button class="sip-copy" onclick={() => copy(`localhost:${port}`)} title={t('servers.ip.copyAddr')} aria-label={t('servers.ip.copyAddr')}><i class="ti ti-copy"></i></button>
      {#if !isRunning}<span class="sip-note">{t('servers.ip.notRunning')}</span>{/if}
    </div>
  {/if}
</div>

<style>
  .sip { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; padding: 2px 2px 0; }
  .sip-chip {
    display: inline-flex; align-items: center; gap: 6px;
    padding: 4px 9px; border-radius: 8px;
    background: var(--bg-card); border: 1px solid var(--border);
    font: 500 11px/1 var(--font-mono); color: var(--text-mute);
  }
  .sip-chip i { color: var(--pcol, var(--orange)); font-size: 12px; }
  .sip-lbl { font-size: var(--fs-min); color: var(--text-dim); }
  .sip-addr { color: var(--text); font-weight: 600; }
  .sip-note { color: var(--text-dim); font-size: var(--fs-min); font-style: italic; }
  .sip-copy {
    border: none; background: transparent; color: var(--text-dim);
    font-size: 11px; cursor: pointer; padding: 2px; border-radius: 4px;
  }
  .sip-copy:hover { color: var(--text); background: var(--hover); }
  .sip-copy.danger:hover { color: var(--red); }
</style>
