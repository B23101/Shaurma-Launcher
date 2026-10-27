<script lang="ts">
  // ── Картка сервера в бібліотеці ───────────────────────────────────────
  // Редизайн: раніше це був компактний .inst-tile (іконка + назва + ▶),
  // який виглядав «сухо» й нічого не казав про стан сервера. Тепер картка:
  //   • шапка — іконка з живою крапкою, назва, ядро · версія, пігулка статусу;
  //   • чіпи — порт, Java, RAM, що є на сервері (моди/плагіни);
  //   • ТІЛО ЗАЛЕЖИТЬ ВІД СТАНУ:
  //       запущений    → живі смужки CPU / RAM (купа JVM) / гравці + uptime;
  //       встановлення → прогрес завантаження ядра/Java;
  //       краш/помилка → текст помилки;
  //       зупинений    → спокійна підказка;
  //   • футер — Старт/Стоп, окреме вікно консолі, тека, меню.
  // Живі цифри приходять із бібліотеки (одне опитування на всі картки).
  import type { ServerView, ServerInstallProgress, ServerLiveStats } from '../../types'
  import { serverIcon, coreLabel, formatUptimeShort } from '../utils'
  import { t } from '../../i18n.svelte'
  import { tip } from '../../friends/tooltip'
  import ServerStatusBadge from './ServerStatusBadge.svelte'

  let {
    srv,
    stats = null,
    installProgress = null,
    onOpen = () => {},
    onStart = () => {},
    onStop = () => {},
    onOpenFolder = () => {},
    onOpenConsole = () => {},
    onContextMenu = (_e: MouseEvent) => {},
    remote = null,
  }: {
    srv: ServerView
    stats?: ServerLiveStats | null
    installProgress?: ServerInstallProgress | null
    onOpen?: () => void
    onStart?: () => void
    onStop?: () => void
    onOpenFolder?: () => void
    onOpenConsole?: () => void
    onContextMenu?: (e: MouseEvent) => void
    // Сервер друга, до якого мені дали доступ: та сама картка, але без метрик/папки/меню,
    // кнопки — лише те, що дозволив власник.
    remote?: { nick: string; online: boolean; canStart: boolean; canStop: boolean; canConsole: boolean; busy: boolean } | null
  } = $props()

  const ic = $derived(serverIcon(srv))
  const pcol = $derived(ic.color || srv.color || 'var(--orange)')
  const running = $derived(srv.status === 'running')
  const installing = $derived(srv.status === 'installing')
  const transient = $derived(srv.status === 'starting' || srv.status === 'stopping')
  const failed = $derived(srv.status === 'crashed' || srv.status === 'error')
  const isVelocity = $derived(srv.core === 'velocity')

  // Живі показники. RAM — КУПА JVM (heapSource==='jvm'), а не RSS процесу:
  // RSS завищений (метадані, off-heap, резерв JVM) і розходився з цифрою на
  // сторінці сервера. Поки GC-лог порожній — показуємо RSS без відсотка.
  const cpu = $derived(Math.min(100, Math.round(stats?.cpuPercent ?? 0)))
  const heap = $derived(stats?.heapSource === 'jvm')
  const ramUsed = $derived(heap ? (stats?.heapUsedMB ?? 0) : (stats?.ramMB ?? 0))
  const ramPct = $derived(heap && srv.ramMB > 0 ? Math.min(100, Math.round((ramUsed / srv.ramMB) * 100)) : 0)
  const players = $derived(stats?.playerCount ?? 0)
  const maxPlayers = $derived(stats?.maxPlayers || 20)
  const playersPct = $derived(Math.min(100, Math.round((players / Math.max(1, maxPlayers)) * 100)))
  const gb = (mb: number) => (mb / 1024).toFixed(mb >= 10240 ? 0 : 1)
  const ramText = $derived(`${gb(ramUsed)}/${gb(srv.ramMB)} GB`)

  const uptime = $derived(running ? formatUptimeShort(stats?.uptimeSec || srv.uptimeSec || 0) : '')
  const ramLabel = $derived(`${(srv.ramMB / 1024).toFixed(srv.ramMB % 1024 ? 1 : 0)} GB`)

  // Кнопки футера не повинні «відкривати» картку (клік піднімається до <article>).
  function act(e: Event, fn: () => void) { e.stopPropagation(); fn() }
</script>

<article
  class="sc rise-in"
  class:running
  class:installing
  class:failed
  onclick={onOpen}
  oncontextmenu={onContextMenu}
  style="--pcol:{pcol}"
  role="button"
  tabindex="0"
  onkeydown={(e) => { if (e.key === 'Enter' && e.target === e.currentTarget) onOpen() }}
>
  <header class="sc-head">
    <div class="sc-ic">
      {#if ic.src}
        <img src={ic.src} alt="" />
      {:else}
        <i class="ti {ic.ti || 'ti-server-2'}"></i>
      {/if}
      {#if running}<span class="sc-live"></span>{/if}
    </div>
    <div class="sc-id">
      <div class="sc-name" title={srv.name}>{srv.name}</div>
      <div class="sc-sub">{#if remote}{remote.nick}{#if coreLabel(srv)} · {coreLabel(srv)}{/if}{:else}{isVelocity ? t('servers.common.velocityProxy') : coreLabel(srv)}{srv.mcVersion && !isVelocity ? ' · ' + srv.mcVersion : ''}{/if}</div>
    </div>
    {#if remote && !remote.online}
      <span class="sc-offpill"><i class="ti ti-plug-off"></i> {t('servers.remote.offlinePill')}</span>
    {:else}
      <ServerStatusBadge status={srv.status} size="sm" />
    {/if}
  </header>

  <div class="sc-chips">
    {#if remote}
      <span class="sc-chip sc-chip-feat"><i class="ti ti-user"></i>{remote.nick}</span>
      {#if remote.canStart}<span class="sc-chip"><i class="ti ti-player-play"></i>{t('servers.remote.can.start')}</span>{/if}
      {#if remote.canStop}<span class="sc-chip"><i class="ti ti-player-stop"></i>{t('servers.remote.can.stop')}</span>{/if}
      {#if remote.canConsole}<span class="sc-chip"><i class="ti ti-terminal-2"></i>{t('servers.remote.can.console')}</span>{/if}
    {:else}
    {#if srv.port}<span class="sc-chip"><i class="ti ti-plug"></i>:{srv.port}</span>{/if}
    <span class="sc-chip"><i class="ti ti-coffee"></i>Java {srv.javaMajor}</span>
    <span class="sc-chip"><i class="ti ti-device-desktop"></i>{ramLabel}</span>
    {#if srv.hasMods}<span class="sc-chip sc-chip-feat"><i class="ti ti-puzzle"></i>{t('servers.tabs.mods')}</span>{/if}
    {#if srv.hasPlugins}<span class="sc-chip sc-chip-feat"><i class="ti ti-plug-connected"></i>{t('servers.tabs.plugins')}</span>{/if}
    {/if}
  </div>

  <div class="sc-body">
    {#if remote && !remote.online}
      <div class="sc-idle"><i class="ti ti-plug-off"></i> {t('servers.remote.offline')}</div>
    {:else if remote && running}
      <div class="sc-idle"><i class="ti ti-server-bolt"></i> {t('servers.remote.running')}</div>
    {:else if installing}
      <div class="sc-bar">
        {#if installProgress && installProgress.percent >= 0}
          <div class="sc-fill" style="width:{installProgress.percent}%"></div>
        {:else}
          <div class="sc-fill indet"></div>
        {/if}
      </div>
      <div class="sc-install-msg"><i class="ti ti-download"></i> {installProgress?.message || t('servers.status.installing')}</div>
    {:else if running}
      <div class="sc-metrics">
        <div class="sc-m" class:hot={cpu > 85}>
          <div class="sc-m-h"><span><i class="ti ti-cpu"></i> CPU</span><b>{stats ? cpu + '%' : '—'}</b></div>
          <div class="sc-bar"><div class="sc-fill" style="width:{cpu}%"></div></div>
        </div>
        <div class="sc-m" class:hot={heap && ramPct >= 95}>
          <div class="sc-m-h"><span><i class="ti ti-device-sd-card"></i> RAM</span><b>{stats ? ramText : '—'}</b></div>
          <div class="sc-bar">{#if heap}<div class="sc-fill" style="width:{ramPct}%"></div>{:else}<div class="sc-fill indet"></div>{/if}</div>
        </div>
        <div class="sc-m">
          <div class="sc-m-h"><span><i class="ti ti-users"></i> {t('servers.metrics.players')}</span><b>{stats ? `${players}/${maxPlayers}` : '—'}</b></div>
          <div class="sc-bar"><div class="sc-fill ok" style="width:{playersPct}%"></div></div>
        </div>
      </div>
      {#if uptime}<div class="sc-uptime"><i class="ti ti-clock"></i> {t('servers.card.uptime', { time: uptime })}</div>{/if}
    {:else if failed}
      <div class="sc-fail"><i class="ti ti-alert-triangle"></i><span>{srv.lastError || t(srv.status === 'crashed' ? 'servers.status.crashed' : 'servers.status.error')}</span></div>
    {:else if transient}
      <div class="sc-idle"><i class="ti ti-loader-2 spin"></i> {t(srv.status === 'starting' ? 'servers.status.starting' : 'servers.status.stopping')}</div>
    {:else}
      <div class="sc-idle">
        <i class="ti ti-moon-stars"></i>
        {#if !srv.coreInstalled}{t('servers.card.coreMissing')}{:else}{t('servers.card.idle')}{/if}
      </div>
    {/if}
  </div>

  <footer class="sc-foot">
    {#if remote}
      {#if remote.online && running && remote.canStop}
        <button class="sc-btn sc-stop" disabled={remote.busy} onclick={(e) => act(e, onStop)}><i class="ti ti-player-stop"></i> {t('servers.card.stop')}</button>
      {:else if remote.online && transient}
        <button class="sc-btn" disabled><i class="ti ti-loader-2 spin"></i> {t('servers.actions.working')}</button>
      {:else if remote.online && !running && remote.canStart}
        <button class="sc-btn sc-play" disabled={remote.busy} onclick={(e) => act(e, onStart)}><i class="ti ti-player-play"></i> {t('servers.card.start')}</button>
      {/if}
      <span class="sc-spacer"></span>
      {#if remote.online && remote.canConsole}
        <button class="sc-ico" onclick={(e) => act(e, onOpenConsole)} use:tip={t('servers.card.console')} aria-label={t('servers.card.console')}><i class="ti ti-terminal-2"></i></button>
      {/if}
    {:else}
    {#if running}
      <button class="sc-btn sc-stop" onclick={(e) => act(e, onStop)}><i class="ti ti-player-stop"></i> {t('servers.card.stop')}</button>
    {:else if transient || installing}
      <button class="sc-btn" disabled><i class="ti ti-loader-2 spin"></i> {t('servers.actions.working')}</button>
    {:else}
      <button class="sc-btn sc-play" onclick={(e) => act(e, onStart)}><i class="ti ti-player-play"></i> {t('servers.card.start')}</button>
    {/if}
    <span class="sc-spacer"></span>
    <button class="sc-ico" onclick={(e) => act(e, onOpenConsole)} use:tip={t('servers.card.console')} aria-label={t('servers.card.console')}><i class="ti ti-terminal-2"></i></button>
    <button class="sc-ico" onclick={(e) => act(e, onOpenFolder)} use:tip={t('servers.card.folder')} aria-label={t('servers.card.folder')}><i class="ti ti-folder-open"></i></button>
    <button class="sc-ico" onclick={(e) => act(e, () => onContextMenu(e as MouseEvent))} use:tip={t('servers.card.more')} aria-label={t('servers.card.more')}><i class="ti ti-dots"></i></button>
    {/if}
  </footer>
</article>

<style>
  .sc {
    position: relative; display: flex; flex-direction: column; gap: 11px; min-width: 0;
    padding: 14px 14px 12px; border-radius: var(--r-lg); cursor: pointer; overflow: hidden; isolation: isolate;
    background:
      radial-gradient(120% 90% at 0% 0%, color-mix(in srgb, var(--pcol) 11%, transparent), transparent 60%),
      var(--bg-card);
    border: 1px solid var(--border);
    transition: border-color .2s, transform .2s cubic-bezier(.34, 1.56, .64, 1), box-shadow .25s;
  }
  .sc:hover { transform: translateY(-2px); border-color: color-mix(in srgb, var(--pcol) 45%, var(--border)); box-shadow: 0 14px 30px -18px color-mix(in srgb, var(--pcol) 55%, #000); }
  .sc:focus-visible { outline: 2px solid var(--pcol); outline-offset: 2px; }
  .sc.running { border-color: color-mix(in srgb, #22c55e 38%, var(--border)); }
  .sc.installing { border-color: color-mix(in srgb, var(--orange) 38%, var(--border)); }
  .sc.failed { border-color: color-mix(in srgb, #ef4444 45%, var(--border)); }

  .sc-head { display: flex; align-items: center; gap: 11px; min-width: 0; }
  .sc-ic {
    position: relative; width: 44px; height: 44px; flex-shrink: 0; border-radius: 13px; display: grid; place-items: center;
    font-size: 21px; color: var(--pcol);
    background: color-mix(in srgb, var(--pcol) 14%, var(--bg-input));
    border: 1px solid color-mix(in srgb, var(--pcol) 35%, var(--border));
  }
  .sc-ic img { width: 100%; height: 100%; object-fit: contain; border-radius: 12px; padding: 3px; box-sizing: border-box; }
  .sc-live { position: absolute; right: -3px; bottom: -3px; width: 12px; height: 12px; border-radius: 50%; background: #22c55e; border: 2.5px solid var(--bg-card); box-shadow: 0 0 8px rgba(34, 197, 94, .7); }
  .sc-id { flex: 1; min-width: 0; }
  .sc-name { font: 750 14.5px/1.2 var(--font-ui); color: var(--text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .sc-sub { margin-top: 3px; font: 500 11px/1.2 var(--font-mono); color: var(--text-mute); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

  .sc-chips { display: flex; flex-wrap: wrap; gap: 5px; }
  .sc-chip {
    display: inline-flex; align-items: center; gap: 4px; padding: 4px 8px; border-radius: 7px;
    font: 600 10px/1 var(--font-mono); color: var(--text-mute); background: var(--hover); border: 1px solid var(--border);
  }
  .sc-chip i { font-size: 11px; color: var(--text-dim); }
  .sc-chip-feat { font-family: var(--font-ui); color: var(--pcol); background: color-mix(in srgb, var(--pcol) 10%, transparent); border-color: color-mix(in srgb, var(--pcol) 30%, transparent); }
  .sc-chip-feat i { color: var(--pcol); }

  .sc-body { min-height: 66px; display: flex; flex-direction: column; justify-content: center; gap: 8px; }
  .sc-metrics { display: flex; flex-direction: column; gap: 7px; }
  .sc-m-h { display: flex; align-items: center; justify-content: space-between; gap: 8px; margin-bottom: 4px; }
  .sc-m-h span { display: inline-flex; align-items: center; gap: 5px; font: 700 9.5px/1 var(--font-ui); letter-spacing: .5px; text-transform: uppercase; color: var(--text-dim); }
  .sc-m-h span i { font-size: 12px; color: var(--pcol); }
  .sc-m-h b { font: 700 10.5px/1 var(--font-mono); color: var(--text); }
  .sc-bar { height: 5px; border-radius: 3px; background: color-mix(in srgb, var(--text) 10%, transparent); overflow: hidden; }
  .sc-fill { height: 100%; border-radius: 3px; background: linear-gradient(90deg, var(--pcol), color-mix(in srgb, var(--pcol) 60%, #fff)); transition: width .45s cubic-bezier(.22, 1, .36, 1); }
  .sc-fill.ok { background: linear-gradient(90deg, #22c55e, #86efac); }
  .sc-fill.indet { width: 40%; animation: sc-slide 1.6s ease-in-out infinite; }
  .sc-m.hot .sc-fill { background: linear-gradient(90deg, #ef4444, #ff7a45); }
  .sc-m.hot .sc-m-h b { color: #ff7a7a; }
  @keyframes sc-slide { 0% { transform: translateX(-110%); } 100% { transform: translateX(360%); } }
  .sc-uptime { display: flex; align-items: center; gap: 5px; font: 600 10.5px/1 var(--font-ui); color: var(--text-mute); }
  .sc-uptime i { font-size: 12px; color: var(--green); }

  .sc-install-msg { display: flex; align-items: center; gap: 6px; font: 500 11px/1.3 var(--font-ui); color: var(--text-mute); min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .sc-install-msg i { color: var(--orange); font-size: 13px; flex-shrink: 0; }
  .sc-fail { display: flex; align-items: flex-start; gap: 8px; padding: 9px 11px; border-radius: 10px; font: 500 11.5px/1.4 var(--font-ui); color: #ff9a9a; background: rgba(239, 68, 68, .1); border: 1px solid rgba(239, 68, 68, .3); }
  .sc-fail i { margin-top: 1px; font-size: 14px; color: #ef4444; flex-shrink: 0; }
  .sc-fail span { display: -webkit-box; -webkit-line-clamp: 2; line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; word-break: break-word; }
  .sc-idle { display: flex; align-items: center; gap: 9px; font: 500 12px/1.4 var(--font-ui); color: var(--text-dim); }
  .sc-idle i { font-size: 18px; }

  .sc-foot { display: flex; align-items: center; gap: 6px; padding-top: 10px; border-top: 1px solid var(--border); }
  .sc-spacer { flex: 1; }
  .sc-btn {
    height: 32px; padding: 0 14px; border-radius: 9px; cursor: pointer; border: 1px solid transparent;
    display: inline-flex; align-items: center; gap: 6px; font: 700 12px/1 var(--font-ui);
    color: var(--text); background: var(--hover); transition: background .15s, transform .15s, box-shadow .2s;
  }
  .sc-btn i { font-size: 15px; }
  .sc-btn:not(:disabled):hover { transform: translateY(-1px); }
  .sc-btn:not(:disabled):active { transform: scale(.96); }
  .sc-btn:disabled { opacity: .7; cursor: default; }
  .sc-play { background: #22c55e; color: #06230f; box-shadow: 0 8px 18px -8px rgba(34, 197, 94, .7); }
  .sc-play:hover { background: #2ee56c; }
  .sc-stop { background: #eab308; color: #241a02; box-shadow: 0 8px 18px -8px rgba(234, 179, 8, .6); }
  .sc-stop:hover { background: #facc15; }
  .sc-ico {
    width: 32px; height: 32px; border-radius: 9px; cursor: pointer; display: grid; place-items: center;
    border: 1px solid transparent; background: transparent; color: var(--text-mute); font-size: 16px;
    transition: background .14s, color .14s, border-color .14s;
  }
  .sc-ico:hover { background: var(--hover); color: var(--text); border-color: var(--border); }

  @media (prefers-reduced-motion: reduce) { .sc-fill.indet { animation: none !important; } .sc { transition: border-color .2s; } .sc:hover { transform: none; } }
  .sc-offpill {
    display: inline-flex; align-items: center; gap: 4px; padding: 2px 7px; border-radius: 999px;
    font: 700 10px/1 var(--font-ui); white-space: nowrap; color: var(--text-dim);
    border: 1px solid var(--border); background: rgba(0, 0, 0, .35);
  }
</style>
