<script lang="ts">
  // ── Герой сторінки сервера ─────────────────────────────────────────────
  // Той самий вигляд, що й на сторінці збірки: рядок дій (.pack-toolbar —
  // «Назад» зліва, кнопки й Старт/Стоп справа) і банер-картка .inst-banner
  // з іконкою, назвою та бейджами. Різниця лише в інформації: замість
  // версії/лоадера збірки — ядро, порт, Java, пам'ять, статус сервера і
  // живе навантаження (CPU/RAM/гравці/uptime) праворуч у банері.
  import type { Snippet } from 'svelte'
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
    onShare = null,
    toolbarExtra,
    // Сервер друга (ServerInstancePage з remoteOwnerId): той самий банер, але без дій, які
    // має лише хост (тека, окрема консоль, поділитися, Kill), і без даних,
    // яких друг не бачить (Java, RAM).
    remoteOwner = '',
    canStart = true,
    canStop = true,
    // Превʼю в майстрі створення: лише сам банер, без рядка дій.
    hideToolbar = false,
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
    // «Поділитися» сервером із друзями (null — кнопки немає).
    onShare?: (() => void) | null
    // Додатковий вміст рядка дій одразу після «Назад» (пошук параметра).
    toolbarExtra?: Snippet
    remoteOwner?: string
    canStart?: boolean
    canStop?: boolean
    hideToolbar?: boolean
  } = $props()

  const isVelocity = $derived(srv.core === 'velocity')
  const isRunning = $derived(displayStatus === 'running')
  const installing = $derived(displayStatus === 'installing')
  // ЄДИНИЙ візуал сервера: PNG лаунчера → пресет → протокольна іконка →
  // іконка ядра → заглушка (той самий порядок, що в списках і бібліотеці).
  const vis = $derived(serverIcon(srv))
  const core = $derived(coreLabel(srv))
  const pcol = $derived(vis.color)
  const ramGb = $derived((srv.ramMB / 1024).toFixed(srv.ramMB % 1024 ? 1 : 0))
  // Метрики праворуч у банері — лише коли є що показати (працює, ставиться
  // ядро або остання помилка); у спокої банер чистий, як у збірки.
  const showMetrics = $derived((isRunning && !!stats) || (installing && !!installProgress) || (!!lastError && !isRunning))
</script>

{#if !hideToolbar}
<div class="pack-toolbar svb-toolbar">
  <button class="btn btn-ghost btn-sm" onclick={onBack} title={t('servers.banner.back')}><i class="ti ti-arrow-left"></i> {t('servers.toolbar.back')}</button>
  {@render toolbarExtra?.()}
  <div class="pt-spacer"></div>
  {#if velocityProxyRunning && !isVelocity}
    <span class="svb-proxy" title={t('servers.toolbar.proxyHint', { port: srv.port ?? 25565 })}><i class="ti ti-router"></i> Velocity</span>
  {/if}
  {#if !remoteOwner}
    <button class="btn btn-ghost btn-sm" onclick={onOpenFolder} title={t('servers.toolbar.openFolder')}><i class="ti ti-folder-open"></i> {t('pack.openFolder')}</button>
    <button class="btn btn-ghost btn-sm" onclick={onOpenConsole} title={t('servers.toolbar.openConsole')}><i class="ti ti-terminal-2"></i> {t('pack.openConsole')}</button>
    {#if onShare}
      <button class="btn btn-ghost btn-sm" onclick={onShare} title="Поділитися з друзями"><i class="ti ti-share-3"></i> Поділитися</button>
    {/if}
  {/if}
  <ServerActionsBar {displayStatus} {actionBusy} {onStart} {onStop} {onKill} {canStart} {canStop} canKill={!remoteOwner} />
</div>
{/if}

<div class="inst-banner svb" class:live={isRunning} style="background:{bannerBg(pcol)};--pcol:{pcol}">
  <div class="ib-shine" aria-hidden="true"></div>
  <div class="svb-glow" aria-hidden="true"></div>

  <div class="svb-status">
    {#if srv.recovered && isRunning}
      <span class="svb-recov" use:tip={t('servers.banner.recoveredHint')}><i class="ti ti-refresh-dot"></i> {t('servers.banner.recovered')}</span>
    {/if}
    <ServerStatusBadge status={displayStatus} />
  </div>

  <div class="ib-title">
    <span class="ib-ic svb-ic">
      {#if vis.src}<img src={vis.src} alt="" />{:else}<i class="ti {vis.ti || 'ti-server-2'}" style="color:#fff"></i>{/if}
      {#if isRunning}<span class="svb-dot"></span>{/if}
    </span>
    <div class="ib-meta">
      <div class="ib-name" title={srv.name}>{srv.name}</div>
      <div class="ib-badges">
        {#if isVelocity}
          <span class="badge-mc"><i class="ti ti-router"></i> {t('servers.banner.proxy')}{#if srv.loaderVer} · {srv.loaderVer}{/if}</span>
        {:else}
          <span class="badge-mc"><i class="ti ti-cube"></i> {srv.mcVersion || '—'} · {core}</span>
          {#if srv.loaderVer}<span class="ib-tag svb-tag">{srv.loaderVer}</span>{/if}
        {/if}
        {#if remoteOwner}
          <span class="ib-tag svb-tag"><i class="ti ti-user"></i> {t('servers.remote.ownerTag', { nick: remoteOwner })}</span>
        {:else}
          {#if srv.port}<span class="ib-tag svb-tag"><i class="ti ti-plug"></i> :{srv.port}</span>{/if}
          {#if srv.javaMajor}<span class="ib-tag svb-tag"><i class="ti ti-coffee"></i> Java {srv.javaMajor}</span>{/if}
          <span class="ib-tag svb-tag"><i class="ti ti-device-desktop"></i> {ramGb} GB</span>
        {/if}
      </div>
    </div>
  </div>

  {#if showMetrics}
    <div class="svb-metrics">
      <ServerMetricsPanel
        running={isRunning}
        {installing}
        {installProgress}
        {stats}
        allocRAM={srv.ramMB || 0}
        uptimeSecFallback={srv.uptimeSec || 0}
        velocity={isVelocity}
        {lastError}
      />
    </div>
  {/if}
</div>

<style>
  .svb-toolbar { flex-wrap: wrap; row-gap: 8px; }
  .svb-proxy {
    display: inline-flex; align-items: center; gap: 4px; padding: 5px 10px; border-radius: 999px;
    font: 700 var(--fs-min)/1 var(--font-ui); color: #8db4ff;
    background: rgba(59, 130, 246, .14); border: 1px solid rgba(59, 130, 246, .45);
  }

  /* Банер — той самий .inst-banner, що в збірки; висота росте, якщо
     метрики праворуч не влізли в один рядок із назвою. */
  .svb { height: auto; min-height: 150px; flex-wrap: wrap; align-items: flex-end; gap: 14px 18px; flex-shrink: 0; }
  .svb.live { box-shadow: 0 0 0 1px color-mix(in srgb, #22c55e 35%, transparent), 0 14px 34px -20px rgba(34, 197, 94, .55); }
  /* Кольорове сяйво сервера в правому куті (у збірки там арт). */
  .svb-glow {
    position: absolute; inset: 0; pointer-events: none; z-index: 0;
    background:
      radial-gradient(60% 120% at 100% 0%, color-mix(in srgb, var(--pcol) 45%, transparent), transparent 60%),
      repeating-linear-gradient(135deg, rgba(255, 255, 255, .025) 0 2px, transparent 2px 14px);
  }
  .svb-status { position: absolute; z-index: 2; top: 14px; right: 16px; display: flex; align-items: center; gap: 8px; }
  .svb-recov {
    display: inline-flex; align-items: center; gap: 5px;
    font: 700 var(--fs-min)/1 var(--font-mono); color: #facc15;
    padding: 5px 10px; border-radius: 999px;
    background: rgba(0, 0, 0, .45); border: 1px solid rgba(234, 179, 8, .5);
    backdrop-filter: blur(6px); cursor: help; white-space: nowrap;
  }
  .svb-recov i { font-size: 12px; animation: svb-spin 3s linear infinite; }
  @keyframes svb-spin { to { transform: rotate(360deg); } }

  .svb-ic { position: relative; flex-shrink: 0; overflow: visible !important; }
  .svb-ic img { border-radius: 15px; object-fit: contain !important; }
  .svb-dot {
    position: absolute; right: -3px; bottom: -3px; width: 14px; height: 14px; border-radius: 50%;
    background: #22c55e; border: 2.5px solid #0b0b0b; box-shadow: 0 0 10px rgba(34, 197, 94, .8);
    animation: svb-pulse 2.2s ease-in-out infinite;
  }
  @keyframes svb-pulse { 0%, 100% { box-shadow: 0 0 6px rgba(34, 197, 94, .6); } 50% { box-shadow: 0 0 14px rgba(34, 197, 94, 1); } }
  /* Бейджі серверу — нейтральні скляні (у збірки — помаранчеві теги). */
  .svb-tag {
    display: inline-flex; align-items: center; gap: 4px;
    font-family: var(--font-mono); color: rgba(255, 255, 255, .9) !important;
    background: rgba(0, 0, 0, .38) !important; border-color: rgba(255, 255, 255, .14) !important;
  }
  .svb-tag i { font-size: 12px; color: var(--pcol); filter: brightness(1.4); }

  .svb-metrics { position: relative; z-index: 1; margin-left: auto; min-width: 0; max-width: 100%; }
  .svb-metrics :global(.smp) { margin-top: 0; justify-content: flex-end; }
  @media (prefers-reduced-motion: reduce) { .svb-dot, .svb-recov i { animation: none; } }
  /* Низьке вікно ноутбука: та сама висота, що в банера збірки (style.css). */
  @media (max-height: 760px) { .svb { min-height: 112px; } }
</style>
