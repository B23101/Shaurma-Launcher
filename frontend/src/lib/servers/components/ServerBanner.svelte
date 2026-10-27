<script lang="ts">
  // ── Герой сторінки сервера ─────────────────────────────────────────────
  // Єдиний блок ідентичності + статусу + керування. Раніше це було
  // розкидано між окремим тулбаром (над банером) і самим банером — назва
  // дублювалась, тож сторінка виглядала розбалансовано. Тепер один
  // цілісний герой у стилі лаунчера.
  //
  // Навантаження (CPU/RAM/гравці/uptime) — ТУТ, у банері (ServerMetricsPanel
  // внизу героя). Раніше воно висіло на вкладці «Мережа»: щоб побачити, чи
  // сервер не перевантажений, треба було перейти в мережевий розділ — для
  // «продуктивність сервера» це неправильне місце (у мережі має бути
  // мережа). Тепер цифри видно одразу під назвою сервера.
  import type { ServerView, ServerLiveStats, ServerInstallProgress } from '../../types'
  import { bannerBg, coreLabel, serverIcon } from '../utils'
  import { t } from '../../i18n.svelte'
  import { tip } from '../../friends/tooltip'
  import ServerStatusBadge from './ServerStatusBadge.svelte'
  import ServerActionsBar from './ServerActionsBar.svelte'
  import ServerMetricsPanel from './ServerMetricsPanel.svelte'

  let {
    srv,
    displayStatus = 'stopped',
    actionBusy = false,
    velocityProxyRunning = false,
    stats = null,
    installProgress = null,
    lastError = '',
    onBack = () => {},
    onStart = () => {},
    onStop = () => {},
    onKill = () => {},
    onOpenFolder = () => {},
    onOpenConsole = () => {},
    onOpenProperties = null,
    onShare = null,
  }: {
    srv: ServerView
    displayStatus?: string
    actionBusy?: boolean
    velocityProxyRunning?: boolean
    stats?: ServerLiveStats | null
    installProgress?: ServerInstallProgress | null
    lastError?: string
    onBack?: () => void
    onStart?: () => void
    onStop?: () => void
    onKill?: () => void
    onOpenFolder?: () => void
    onOpenConsole?: () => void
    // Відкрити окрему сторінку server.properties (у Velocity її немає).
    onOpenProperties?: (() => void) | null
    // «Поділитися» сервером із друзями (null — кнопки немає).
    onShare?: (() => void) | null
  } = $props()

  const isVelocity = $derived(srv.core === 'velocity')
  const isRunning = $derived(displayStatus === 'running')
  // ЄДИНИЙ візуал сервера: PNG лаунчера → пресет → протокольна іконка →
  // іконка ядра → заглушка (той самий порядок, що в списках і бібліотеці).
  const vis = $derived(serverIcon(srv))
  const core = $derived(coreLabel(srv))
  const pcol = $derived(vis.color)
</script>

<div class="svh" class:live={isRunning} style="background:{bannerBg(pcol)};--pcol:{pcol}">
  <div class="svh-shine" aria-hidden="true"></div>

  <div class="svh-top">
    <button class="svh-back" onclick={onBack} title={t('servers.banner.back')} aria-label={t('servers.banner.back')}>
      <i class="ti ti-arrow-left"></i>
    </button>

    <div class="svh-id" title={srv.name}>
      <span class="svh-ic">
        {#if vis.src}
          <img src={vis.src} alt="" />
        {:else}
          <i class="ti {vis.ti || 'ti-server-2'}" style="color:#fff"></i>
        {/if}
        {#if isRunning}<span class="svh-dot"></span>{/if}
      </span>
      <div class="svh-meta">
        <div class="svh-name">{srv.name}</div>
        <div class="svh-badges">
          {#if isVelocity}
            <span class="svh-b svh-b-core"><i class="ti ti-router"></i> {t('servers.banner.proxy')}</span>
            {#if srv.loaderVer}<span class="svh-b">{srv.loaderVer}</span>{/if}
          {:else}
            {#if srv.mcVersion}<span class="svh-b svh-b-mc"><i class="ti ti-cube"></i> {srv.mcVersion}</span>{/if}
            <span class="svh-b svh-b-core">{core}</span>
            {#if srv.loaderVer}<span class="svh-b">{srv.loaderVer}</span>{/if}
          {/if}
          {#if srv.port}<span class="svh-b"><i class="ti ti-plug"></i> :{srv.port}</span>{/if}
          <span class="svh-b"><i class="ti ti-coffee"></i> Java {srv.javaMajor}</span>
          <span class="svh-b"><i class="ti ti-device-desktop"></i> {(srv.ramMB / 1024).toFixed(srv.ramMB % 1024 ? 1 : 0)} GB</span>
        </div>
      </div>
    </div>

    <div class="svh-right">
      {#if srv.recovered && isRunning}
        <span class="svh-recov" use:tip={t('servers.banner.recoveredHint')}>
          <i class="ti ti-refresh-dot"></i> {t('servers.banner.recovered')}
        </span>
      {/if}
      <ServerStatusBadge status={displayStatus} />
      <div class="svh-controls">
        {#if velocityProxyRunning && !isVelocity}
          <span class="svh-proxy" title={t('servers.toolbar.proxyHint', { port: srv.port ?? 25565 })}>
            <i class="ti ti-router"></i> Velocity
          </span>
        {/if}
        <button class="svh-btn" onclick={onOpenFolder} title={t('servers.toolbar.openFolder')} aria-label={t('servers.toolbar.openFolder')}>
          <i class="ti ti-folder"></i>
        </button>
        {#if onShare}
          <button class="svh-btn" onclick={onShare} title="Поділитися з друзями" aria-label="Поділитися з друзями"><i class="ti ti-share-3"></i></button>
        {/if}
        <button class="svh-btn" onclick={onOpenConsole} title={t('servers.toolbar.openConsole')} aria-label={t('servers.toolbar.openConsole')}>
          <i class="ti ti-terminal-2"></i>
        </button>
        {#if onOpenProperties && !isVelocity}
          <button class="svh-btn" onclick={onOpenProperties} title={t('servers.banner.properties')} aria-label={t('servers.banner.properties')} data-testid="server-properties-btn">
            <i class="ti ti-adjustments-horizontal"></i>
          </button>
        {/if}
        <ServerActionsBar {displayStatus} {actionBusy} {onStart} {onStop} {onKill} />
      </div>
    </div>
  </div>

  <!-- ── Навантаження сервера: CPU, RAM, гравці, uptime ──
       Живі бари просто в банері — це «як почувається сервер», а не мережа. -->
  <ServerMetricsPanel
    running={isRunning}
    installing={displayStatus === 'installing'}
    {installProgress}
    {stats}
    allocRAM={srv.ramMB || 0}
    uptimeSecFallback={srv.uptimeSec || 0}
    velocity={isVelocity}
    {lastError}
  />
</div>

<style>
  .svh {
    position: relative; overflow: hidden;
    display: flex; flex-direction: column; gap: 12px;
    padding: 16px 18px; border-radius: var(--r-lg);
    border: 1px solid var(--border-hi);
    box-shadow: 0 18px 40px -22px rgba(0, 0, 0, .8);
  }
  .svh-shine {
    position: absolute; inset: 0; pointer-events: none;
    background: radial-gradient(120% 80% at 18% -20%, rgba(255, 255, 255, .16), transparent 55%);
  }
  .svh.live { box-shadow: 0 0 0 1px color-mix(in srgb, #22c55e 30%, transparent), 0 18px 44px -20px rgba(0, 0, 0, .82); }

  .svh-top { position: relative; z-index: 1; display: flex; align-items: center; gap: 14px; min-width: 0; flex-wrap: wrap; }

  .svh-back {
    width: 34px; height: 34px; flex-shrink: 0; border-radius: 10px;
    background: rgba(0, 0, 0, .34); border: 1px solid rgba(255, 255, 255, .16); color: #fff;
    font-size: 16px; cursor: pointer; display: flex; align-items: center; justify-content: center;
    transition: background .15s, transform .15s;
  }
  .svh-back:hover { background: rgba(0, 0, 0, .55); transform: translateX(-2px); }

  .svh-id { display: flex; align-items: center; gap: 13px; min-width: 0; flex: 1 1 260px; }
  .svh-ic {
    position: relative; width: 54px; height: 54px; border-radius: 15px; flex-shrink: 0; overflow: hidden;
    background: rgba(0, 0, 0, .4); border: 1px solid rgba(255, 255, 255, .16);
    display: flex; align-items: center; justify-content: center; font-size: 24px;
    box-shadow: 0 12px 30px rgba(0, 0, 0, .5);
  }
  .svh-ic img { width: 100%; height: 100%; object-fit: contain; }
  .svh-dot {
    position: absolute; right: 2px; bottom: 2px; width: 12px; height: 12px; border-radius: 50%;
    background: #22c55e; border: 2px solid rgba(0, 0, 0, .55); box-shadow: 0 0 8px rgba(34, 197, 94, .7);
  }
  .svh-meta { min-width: 0; }
  .svh-name {
    font: 800 22px/1.1 var(--font-ui); letter-spacing: -.4px; color: #fff;
    text-shadow: 0 3px 18px rgba(0, 0, 0, .7);
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 100%;
  }
  .svh-badges { display: flex; gap: 5px; flex-wrap: wrap; align-items: center; margin-top: 7px; }
  .svh-b {
    display: inline-flex; align-items: center; gap: 4px;
    font: 600 10.5px/1 var(--font-mono); color: rgba(255, 255, 255, .9);
    padding: 4px 8px; border-radius: 7px;
    background: rgba(0, 0, 0, .32); border: 1px solid rgba(255, 255, 255, .12);
  }
  .svh-b i { font-size: 12px; color: rgba(255, 255, 255, .75); }
  .svh-b-mc { color: #fff; }
  /* Керування відновлено після краша лаунчера (recover.go в бекенді). */
  .svh-recov {
    display: inline-flex; align-items: center; gap: 5px;
    font: 700 10.5px/1 var(--font-mono); color: #facc15;
    padding: 5px 9px; border-radius: 7px;
    background: rgba(234, 179, 8, .14); border: 1px solid rgba(234, 179, 8, .45);
    cursor: help; white-space: nowrap;
  }
  .svh-recov i { font-size: 12px; animation: svh-spin 3s linear infinite; }
  @keyframes svh-spin { to { transform: rotate(360deg); } }
  .svh-b-core {
    color: #fff; font-family: var(--font-ui); font-weight: 700;
    background: color-mix(in srgb, var(--pcol) 30%, rgba(0, 0, 0, .35));
    border-color: color-mix(in srgb, var(--pcol) 55%, transparent);
  }
  .svh-b-core i { color: #fff; }

  .svh-right { display: flex; flex-direction: column; align-items: flex-end; gap: 10px; flex-shrink: 0; margin-left: auto; }
  .svh-controls { display: flex; align-items: center; gap: 6px; }
  .svh-btn {
    width: 34px; height: 34px; border-radius: 10px; cursor: pointer;
    background: rgba(0, 0, 0, .3); border: 1px solid rgba(255, 255, 255, .16); color: #fff;
    font-size: 15px; display: flex; align-items: center; justify-content: center;
    transition: background .14s, transform .14s, border-color .14s;
  }
  .svh-btn:hover { background: rgba(0, 0, 0, .5); border-color: rgba(255, 255, 255, .3); transform: translateY(-1px); }
  .svh-proxy {
    display: inline-flex; align-items: center; gap: 4px; padding: 5px 9px; border-radius: 999px;
    font: 700 10px/1 var(--font-ui); color: #cfe0ff;
    background: rgba(59, 130, 246, .22); border: 1px solid rgba(59, 130, 246, .5);
  }

  @media (max-width: 820px) {
    .svh-right { flex-direction: row; align-items: center; width: 100%; justify-content: space-between; }
  }
</style>
