<script lang="ts">
  // ── Тулбар сторінки сервера (НАД банером) ──────────────────────────────
  //
  // Навіщо: у збірок керування зроблено саме так — кнопка запуску, «папка»,
  // «окреме вікно консолі» й навантаження (CPU/RAM/гравці/uptime) живуть у
  // рядку НАД банером, а сам банер показує лише ідентичність (іконка,
  // назва, бейджі, статус). У серверів кнопки були ВСЕРЕДИНІ банера, а
  // навантаження — у вкладці «Мережа», тому сторінка виглядала інакше й
  // гірше за сторінку збірки.
  //
  // Тепер так само, як у збірках:
  //   [← До серверів] [іконка + назва + ядро/версія (зʼявляється при скролі)]
  //   [мʼяка пігулка навантаження] [папка] [консоль] [Старт/Стоп/Kill]
  //
  // Коли банер (з великою назвою) виїжджає за верх при скролі — компактна
  // ідентичність у тулбарі проявляється (class:show), щоб гравець завжди
  // бачив, на сторінці якого сервера він перебуває. Той самий прийом, що
  // .pt-id на сторінці збірки.
  import type { ServerView, ServerLiveStats, ServerInstallProgress } from '../../types'
  import { t } from '../../i18n.svelte'
  import { coreLabel, serverIcon } from '../utils'
  import ServerActionsBar from './ServerActionsBar.svelte'
  import ServerMetricsPanel from './ServerMetricsPanel.svelte'

  let {
    srv,
    displayStatus = 'stopped',
    actionBusy = false,
    stats = null,
    installProgress = null,
    lastError = '',
    velocityProxyRunning = false,
    onBack = () => {},
    onStart = () => {},
    onStop = () => {},
    onKill = () => {},
    onOpenFolder = () => {},
    onOpenConsole = () => {},
  }: {
    srv: ServerView
    displayStatus?: string
    actionBusy?: boolean
    stats?: ServerLiveStats | null
    installProgress?: ServerInstallProgress | null
    lastError?: string
    velocityProxyRunning?: boolean
    onBack?: () => void
    onStart?: () => void
    onStop?: () => void
    onKill?: () => void
    onOpenFolder?: () => void
    onOpenConsole?: () => void
  } = $props()

  const isRunning = $derived(displayStatus === 'running')
  const installing = $derived(displayStatus === 'installing' || displayStatus === 'starting')
  const isVelocity = $derived(srv.core === 'velocity')
  // ЄДИНИЙ візуал сервера (PNG лаунчера → пресет → протокольна іконка →
  // іконка ядра → заглушка) — не дублюємо пріоритет тут вручну.
  const vis = $derived(serverIcon(srv))
  const core = $derived(coreLabel(srv))

  // Компактна ідентичність поруч із кнопками. Вона НЕ ховається за скролом
  // (у серверів сторінка скролиться всередині вкладки, а не вікном, тож
  // «зʼявитись при скролі» тут було б мертвою логікою): гравець завжди бачить
  // назву сервера, його ядро й версію прямо над кнопками — як у збірках, де
  // тулбар тримає ідентичність поруч із запуском.
</script>

<div class="stb">
  <button class="btn btn-ghost btn-sm stb-back" onclick={onBack} title={t('servers.toolbar.back')}>
    <i class="ti ti-arrow-left"></i> <span class="stb-back-t">{t('servers.toolbar.back')}</span>
  </button>

  <div class="stb-id" title={srv.name}>
    <span class="stb-id-ic" style="--pcol:{vis.color}">
      {#if vis.src}
        <img src={vis.src} alt="" />
      {:else}
        <i class="ti {vis.ti || 'ti-server-2'}"></i>
      {/if}
    </span>
    <div class="stb-id-meta">
      <div class="stb-id-name">{srv.name}</div>
      <div class="stb-id-sub">
        <i class="ti {isVelocity ? 'ti-router' : 'ti-cube'}"></i>
        {#if isVelocity}
          {t('servers.common.velocityProxy')}{#if srv.loaderVer} · {srv.loaderVer}{/if}
        {:else}
          {core}{#if srv.mcVersion} · {srv.mcVersion}{/if}
        {/if}
      </div>
    </div>
  </div>

  <!-- Навантаження — тут, НАД банером: те саме місце, де воно у збірках. -->
  <div class="stb-metrics">
    <ServerMetricsPanel
      {stats}
      {installProgress}
      running={isRunning}
      {installing}
      velocity={isVelocity}
      allocRAM={srv.ramMB}
      uptimeSecFallback={srv.uptimeSec ?? 0}
      {lastError}
    />
  </div>

  <div class="stb-actions">
    {#if velocityProxyRunning && !isVelocity}
      <span class="stb-proxy" title={t('servers.toolbar.proxyHint', { port: srv.port ?? 25565 })}>
        <i class="ti ti-router"></i> Velocity
      </span>
    {/if}
    <button class="btn btn-ghost btn-sm" onclick={onOpenFolder} title={t('servers.toolbar.openFolder')} aria-label={t('servers.toolbar.openFolder')}>
      <i class="ti ti-folder"></i>
    </button>
    <button class="btn btn-ghost btn-sm" onclick={onOpenConsole} title={t('servers.toolbar.openConsole')} aria-label={t('servers.toolbar.openConsole')}>
      <i class="ti ti-terminal-2"></i>
    </button>
    <ServerActionsBar {displayStatus} {actionBusy} {onStart} {onStop} {onKill} />
  </div>
</div>

<style>
  .stb {
    display: flex; align-items: center; gap: 10px; flex-wrap: wrap;
    padding: 8px 10px; border-radius: 12px; min-width: 0;
    background: color-mix(in srgb, var(--bg-window, #14100c) 80%, transparent);
    border: 1px solid var(--border);
    position: sticky; top: 0; z-index: 6;
    transition: background .18s, border-color .18s, box-shadow .18s;
  }
  .stb-back { flex: 0 0 auto; }

  .stb-id { display: flex; align-items: center; gap: 8px; min-width: 0; flex: 0 1 auto; }
  .stb-id-ic {
    width: 26px; height: 26px; border-radius: 7px; overflow: hidden; flex: 0 0 auto;
    display: flex; align-items: center; justify-content: center;
    background: rgba(0, 0, 0, .35); border: 1px solid var(--border);
    color: var(--pcol, var(--orange)); font-size: 14px;
  }
  .stb-id-ic img { width: 100%; height: 100%; object-fit: contain; }
  .stb-id-meta { min-width: 0; }
  .stb-id-name { font: 700 12px/1.2 var(--font-ui); color: var(--text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 220px; }
  .stb-id-sub { font: 400 var(--fs-min)/1.3 var(--font-ui); color: var(--text-dim); display: flex; align-items: center; gap: 4px; }

  /* Метрики в тулбарі — компактніші за «банерні»: рядок, не карточки. */
  .stb-metrics { flex: 1 1 320px; min-width: 0; }
  .stb-metrics :global(.smp) { margin-top: 0; gap: 6px; flex-wrap: nowrap; overflow: hidden; }
  .stb-metrics :global(.sm) { min-width: 0; flex: 1 1 0; padding: 5px 9px; }
  .stb-metrics :global(.sm-wide) { min-width: 0; }
  .stb-metrics :global(.sm-val) { font-size: var(--fs-min); }
  .stb-metrics :global(.sm-warn) { display: none; }
  .stb-metrics :global(.sm-idle) { min-width: 0; }

  .stb-actions { display: flex; align-items: center; gap: 6px; flex: 0 0 auto; margin-left: auto; }
  .stb-proxy {
    display: inline-flex; align-items: center; gap: 4px; padding: 3px 7px; border-radius: 999px;
    font: 600 var(--fs-min)/1 var(--font-ui); color: #3b82f6;
    background: color-mix(in srgb, #3b82f6 12%, transparent); border: 1px solid color-mix(in srgb, #3b82f6 35%, transparent);
  }

  @media (max-width: 980px) {
    .stb-back-t { display: none; }
    .stb-metrics { order: 3; flex-basis: 100%; }
  }
</style>
