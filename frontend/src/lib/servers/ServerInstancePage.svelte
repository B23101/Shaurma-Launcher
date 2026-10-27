<script lang="ts">
  // ── Сторінка сервера ──────────────────────────────────────────────────
  // Тонка обгортка: банер (ServerBanner) + вкладки (.tabbar.tb-slide —
  // ТОЙ САМИЙ глобальний клас, що на сторінці збірки, не власний
  // винахід) + вміст поточної вкладки з окремого файлу в tabs/. Жодної
  // бізнес-логіки конкретної вкладки тут немає — лише спільний стан
  // (srv, живий статус/статистика, яка вкладка активна) і роутинг між
  // tabs/*.svelte.
  //
  // Живий статус/статистика — через хуки useServerStatus/useServerStats:
  // сторінка НЕ застигає на моменті GetServer().
  import { App } from '../wails'
  import { toast, confirmDialog } from '../toast.svelte'
  import { t } from '../i18n.svelte'
  import type { ServerView } from '../types'
  import { MOD_CORES, PLUGIN_CORES } from './constants'
  import { serverVisual } from './serverTargets'
  import { useServerStatus } from './hooks/useServerStatus.svelte'
  import { useServerStats } from './hooks/useServerStats.svelte'
  import ServerBanner from './components/ServerBanner.svelte'
  import ServerPropertiesPage from './ServerPropertiesPage.svelte'
  import ServerPreflightModal from './components/ServerPreflightModal.svelte'
  import type { ServerPreflight } from '../types'
  import ServerConsoleTab from './tabs/ServerConsoleTab.svelte'
  import ServerModsTab from './tabs/ServerModsTab.svelte'
  import ServerPluginsTab from './tabs/ServerPluginsTab.svelte'
  import ServerWorldsTabPanel from './tabs/ServerWorldsTabPanel.svelte'
  import ServerNetworkTabPanel from './tabs/ServerNetworkTabPanel.svelte'
  import ServerSettingsTab from './tabs/ServerSettingsTab.svelte'
  import RemoteAccessPanel from './components/RemoteAccessPanel.svelte'
  import ServerAppearanceTab from './tabs/ServerAppearanceTab.svelte'
  import ServerFilesTab from './tabs/ServerFilesTab.svelte'
  import ShareModal from '../ShareModal.svelte'

  let {
    serverId = '',
    onBack = () => {},
    // autoStart — сторінка щойно відкрилась одразу після створення сервера
    // з увімкненою опцією «Створити й одразу запустити».
    autoStart = false,
    // onSearchOnline — кнопка «Знайти онлайн» у менеджері модів/плагінів
    // (ServerContentTab): відкрити «Браузер контенту» з фільтром на цей
    // сервер. Прокидається аж з App.svelte (там живе сам ContentBrowser і
    // навігація сторінок) — тут лише прохідний пропс. Опційно: якщо не
    // задано, кнопки «Знайти онлайн» на вкладках не показуються.
    onSearchOnline = null,
    // «Поділитися» сервером з друзями (лише Shaurma-редакція).
    canShare = false,
  }: {
    serverId?: string
    onBack?: () => void
    autoStart?: boolean
    onSearchOnline?: ((serverId: string, kind: 'mods' | 'plugins') => void) | null
    canShare?: boolean
  } = $props()

  let srv = $state<ServerView | null>(null)
  let shareOpen = $state(false)
  let loading = $state(true)
  let ipBarKey = $state(0)
  type TabId = 'console' | 'mods' | 'plugins' | 'worlds' | 'network' | 'appearance' | 'access' | 'settings' | 'files'
  let tab = $state<TabId>('console')
  let actionBusy = $state(false)
  // 'properties' — окрема сторінка server.properties (як налаштування лаунчера)
  let view = $state<'main' | 'properties'>('main')

  async function loadServer() {
    try { srv = await App.GetServer(serverId) } catch (e) { console.error(e) }
    finally { loading = false }
  }

  $effect(() => {
    if (!serverId) return
    loading = true
    loadServer()
  })

  // ── Живий статус (з подій, не застигає) ──
  const live = useServerStatus(() => serverId)
  const displayStatus = $derived(live.status || srv?.status || 'stopped')
  const isRunning = $derived(displayStatus === 'running')

  // Після зупинки/краху — тихо перечитуємо GetServer (CoreInstalled/
  // LoaderVer/JavaInstalled можуть змінитись після встановлення ядра).
  let lastStatusSeen = ''
  $effect(() => {
    const s = live.status
    if (lastStatusSeen && lastStatusSeen !== s && (s === 'stopped' || s === 'crashed' || s === 'error')) loadServer()
    lastStatusSeen = s
  })

  // Консоль сама запускає сервер при autoStart — коли команда прийшла НЕ
  // з вкладки консолі (напр. кнопка «Старт» на сторінці налаштувань
  // колись у майбутньому), просто перемикаємось на неї.
  let pendingAutoStart = $state(false)
  // autoStartConsumed — автозапуск ОДНОРАЗОВИЙ. autoStart приходить зі
  // App.svelte і лишається true, поки сторінка сервера відкрита (скидається
  // лише кнопкою «Назад»). Без цього прапорця будь-яке ПОВТОРНЕ монтування
  // вкладки консолі (перемикання вкладок, повернення на сервер) знову
  // стартувало б уже зупинений сервер — це і був баг «зупинив сервер,
  // а він сам запустився». Щойно сервер реально пішов у запуск/працює —
  // вважаємо намір вичерпаним.
  let autoStartConsumed = $state(false)
  $effect(() => {
    if (displayStatus === 'starting' || displayStatus === 'running' || displayStatus === 'installing') {
      autoStartConsumed = true
      pendingAutoStart = false
    }
  })
  const wantAutoStart = $derived((autoStart || pendingAutoStart) && !autoStartConsumed)

  // ── Live-статистика для банера (CPU/RAM/гравці) ──
  const liveStats = useServerStats(() => serverId, () => isRunning)

  // ── Розумний аналізатор запуску ────────────────────────────────────
  // Перевірки перед стартом (порт/RAM/Java/EULA/диск/ядро/краш) з
  // урахуванням налаштувань лаунчера. runPreflight() повертає Promise:
  // true — стартувати, false — людина скасувала (або є блокер).
  // Обидва шляхи старту (кнопка на банері й консоль) йдуть через неї.
  let pfResult = $state<ServerPreflight | null>(null)
  let pfResolve: ((ok: boolean) => void) | null = null

  async function runPreflight(): Promise<boolean> {
    if (displayStatus === 'running' || displayStatus === 'starting') return true
    let res: ServerPreflight | null = null
    try {
      res = await App.GetServerLaunchWarnings(serverId)
    } catch {
      // Аналізатор недоступний — не блокуємо запуск (стара поведінка).
      return true
    }
    if (!res || !res.warnings || res.warnings.length === 0) return true
    pfResult = res
    return new Promise<boolean>((resolve) => { pfResolve = resolve })
  }

  function pfProceed() { pfResolve?.(true); pfResolve = null; pfResult = null }
  function pfCancel() { pfResolve?.(false); pfResolve = null; pfResult = null }

  // ── Дії банера ──
  async function startServer() {
    const st = displayStatus
    if (st === 'running' || st === 'starting' || st === 'stopping' || st === 'installing') return
    if (tab !== 'console') { pendingAutoStart = true; tab = 'console'; return } // консоль стартує сама при монтуванні

    if (srv?.core !== 'velocity' && velocityProxyRunning) {
      toast(t('servers.page.viaProxy', { port: srv?.port ?? 25565 }), 'info')
    }

    actionBusy = true
    try {
      // Розумний аналізатор: порт/RAM/Java/EULA/диск — спершу показуємо
      // те, що заважає, і лиш потім реально стартуємо.
      if (!(await runPreflight())) return
      await App.StartServer(serverId)
      // Перемонтовуємо IP-рядок/віджети, щоб вони одразу перечитали стан
      // (адреса сервера, P2P-тунель) після старту.
      ipBarKey++
    } catch (e: any) {
      toast(String(e), 'error')
    } finally {
      actionBusy = false
    }
  }

  async function stopServer() {
    if (displayStatus !== 'running' && displayStatus !== 'starting') return
    actionBusy = true
    try {
      await App.StopServer(serverId, true)
      toast(t('servers.page.stopping'), 'info')
    } catch (e: any) {
      toast(String(e), 'error')
    } finally {
      actionBusy = false
    }
  }

  async function killServer() {
    const proceed = await confirmDialog(
      t('servers.page.killTitle'),
      t('servers.page.killBody'),
      { confirmLabel: t('servers.page.killConfirm'), cancelLabel: t('servers.page.cancel'), danger: true },
    )
    if (!proceed) return
    try {
      await App.StopServer(serverId, false)
      toast(t('servers.page.killed'), 'info')
    } catch (e: any) { toast(String(e), 'error') }
  }

  async function openFolder() {
    if (srv) App.OpenFolder(srv.dir).catch(() => {})
  }

  // Velocity — це не «окремий сервер», а ПЛАТФОРМА мережі: при відкритті
  // без автозапуску одразу показуємо «Мережу» (проксі + звʼязки з
  // backend-серверами).
  let platformTabApplied = $state(false)
  $effect(() => {
    if (!platformTabApplied && srv?.core === 'velocity' && !autoStart && !pendingAutoStart && tab === 'console') {
      platformTabApplied = true
      tab = 'network'
    }
  })

  // Чи є запущений Velocity проксі десь у бібліотеці (для IP-бару й
  // підказки при старті backend-сервера).
  let velocityProxyRunning = $state(false)
  $effect(() => {
    if (!serverId) return
    App.ListServers().then((allServers: any[]) => {
      velocityProxyRunning = allServers.some((s: any) => s.core === 'velocity' && (s.status === 'running' || s.status === 'starting'))
    }).catch(() => {})
  })

  // P2P «разом із сервером» тепер керує БЕКЕНД (backend/p2p_voice.go: p2pAutoSync, викликається
  // зі зміни статусу сервера) — працює, навіть коли сторінку сервера не відкрито, і сам бере
  // правильний UDP-порт. Помилки автозапуску показує картка діагностики у вкладці «Мережа».

  // ── Вкладки, доступні для цього ядра ──
  const isVelocity = $derived(srv?.core === 'velocity')
  const hasMods = $derived(!!srv && MOD_CORES.has(srv.core))
  const hasPlugins = $derived(!!srv && PLUGIN_CORES.has(srv.core))
  const hasWorlds = $derived(!!srv && srv.core !== 'velocity')

  // ЄДИНИЙ візуал сервера (той самий порядок пріоритету, що в ServerBanner,
  // картці бібліотеки та списку цілей встановлення):
  // PNG лаунчера → пресет → протокольна server-icon.png → іконка ядра.
  // Раніше тут був прямий CORE_META — через це колір сторінки ігнорував
  // пресет/PNG сервера, якщо колір явно не задано.
  const vis = $derived(serverVisual({
    core: srv?.core || '',
    color: srv?.color,
    launcherIconData: srv?.launcherIconData,
    icon: srv?.icon,
    iconData: srv?.iconData,
  }))
  const pcol = $derived(vis.color)

  // Таббар з ковзною пігулкою — та сама поведінка, що на сторінці
  // збірки (.tabbar.tb-slide), тут лише вимірюємо позицію активної
  // кнопки для CSS-переміних --tb-ind-l/--tb-ind-w.
  let tabbarEl = $state<HTMLElement | null>(null)
  let tbIndL = $state(0)
  let tbIndW = $state(0)
  function measureTabPill() {
    const bar = tabbarEl
    if (!bar) return
    const btn = bar.querySelector<HTMLElement>('.tab-btn.active')
    if (!btn) return
    const br = bar.getBoundingClientRect()
    const r = btn.getBoundingClientRect()
    tbIndL = Math.max(0, r.left - br.left - (bar.clientLeft || 0))
    tbIndW = r.width
  }
  $effect(() => {
    const t = tab
    if (!tabbarEl) return
    const id = requestAnimationFrame(() => requestAnimationFrame(measureTabPill))
    window.addEventListener('resize', measureTabPill)
    return () => {
      cancelAnimationFrame(id)
      window.removeEventListener('resize', measureTabPill)
    }
  })

  function openTab(id: TabId) {
    tab = id
  }
</script>

{#if loading}
  <div class="sv-loading"><i class="ti ti-loader spin"></i> {t('servers.page.loading')}</div>
{:else if !srv}
  <div class="sv-loading"><i class="ti ti-alert-triangle"></i> {t('servers.page.notFound')}</div>
{:else if view === 'properties'}
  <div class="sv" style="--pcol:{pcol}">
    <ServerPropertiesPage
      serverId={srv.id}
      serverName={srv.name}
      running={isRunning}
      color={pcol}
      onBack={() => { view = 'main' }}
      onOpenSettings={() => { tab = 'settings'; view = 'main' }}
    />
  </div>
{:else}
  <div class="sv" style="--pcol:{pcol}">
    <!-- Банер — ЄДИНИЙ блок ідентичності + статусу + керування + навантаження
         (ServerBanner). Раніше тут ПОВЕРХ банера рендерився ще й окремий
         ServerToolbar з тими самими іконкою/назвою/Старт-Стоп/навантаженням
         — то й був той «подвійний, завеликий» блок на сторінці сервера:
         одна й та сама інформація і кнопки двічі поспіль. ServerToolbar
         прибрано, лишився один блок, як і задумано в самому ServerBanner. -->
    <ServerBanner
      {srv}
      {displayStatus}
      {actionBusy}
      {velocityProxyRunning}
      stats={liveStats.stats}
      installProgress={live.progress}
      lastError={live.lastError}
      {onBack}
      onStart={startServer}
      onStop={stopServer}
      onKill={killServer}
      onOpenFolder={openFolder}
      onShare={canShare && !isVelocity ? () => { shareOpen = true } : null}
      onOpenProperties={isVelocity ? null : () => { view = 'properties' }}
      onOpenConsole={() => { App.ToggleServerConsoleWindow(serverId).catch((e: any) => toast(String(e?.message ?? e), 'error')) }}
    />

    <!-- ═══ ВКЛАДКИ — глобальний .tabbar.tb-slide, як на сторінці збірки ═══ -->
    <div class="tabbar tb-slide" bind:this={tabbarEl} style="--tb-ind-l:{tbIndL}px;--tb-ind-w:{tbIndW}px">
      <button class="tab-btn" class:active={tab === 'console'} onclick={() => openTab('console')}><i class="ti ti-terminal-2"></i> {t('servers.tabs.console')}</button>
      {#if hasMods}
        <button class="tab-btn" class:active={tab === 'mods'} onclick={() => openTab('mods')}><i class="ti ti-puzzle"></i> {t('servers.tabs.mods')}</button>
      {/if}
      {#if hasPlugins}
        <button class="tab-btn" class:active={tab === 'plugins'} onclick={() => openTab('plugins')}><i class="ti ti-plug"></i> {t('servers.tabs.plugins')}</button>
      {/if}
      {#if hasWorlds}
        <button class="tab-btn" class:active={tab === 'worlds'} onclick={() => openTab('worlds')}><i class="ti ti-world"></i> {t('servers.tabs.worlds')}</button>
      {/if}
      <!-- «Мережа» — ДЛЯ ВСІХ ядер: локальна адреса, P2P-доступ і детальна
           панель навантаження. Коротка пігулка навантаження тепер у
           тулбарі над банером. -->
      <button class="tab-btn" class:active={tab === 'network'} onclick={() => openTab('network')}><i class="ti ti-network"></i> {t('servers.tabs.network')}</button>
      <button class="tab-btn" class:active={tab === 'appearance'} onclick={() => openTab('appearance')}><i class="ti ti-palette"></i> {t('servers.tabs.appearance')}</button>
      <button class="tab-btn" class:active={tab === 'access'} onclick={() => openTab('access')}><i class="ti ti-users-group"></i> {t('servers.tabs.access')}</button>
      <button class="tab-btn" class:active={tab === 'settings'} onclick={() => openTab('settings')}><i class="ti ti-settings"></i> {t('servers.tabs.settings')}</button>
      <button class="tab-btn" class:active={tab === 'files'} onclick={() => openTab('files')}><i class="ti ti-files"></i> {t('servers.tabs.files')}</button>
    </div>

    <!-- ═══ ВМІСТ ВКЛАДКИ ═══ -->
    <div class="sv-content">
      {#if tab === 'console'}
        {#key `console:${serverId}`}
          <div class="sv-console-wrap">
            <ServerConsoleTab {serverId} serverColor={pcol} autoStart={wantAutoStart} preflight={runPreflight} />
          </div>
        {/key}
      {:else if tab === 'mods'}
        <!-- serverDir — щоб менеджер міг відкрити саме теку mods/ цього
             сервера (кнопка «Відкрити теку»), не роблячи зайвих запитів. -->
        <div class="sv-panel"><ServerModsTab {serverId} running={isRunning} serverDir={srv.dir} {onSearchOnline} /></div>
      {:else if tab === 'plugins'}
        <div class="sv-panel"><ServerPluginsTab {serverId} running={isRunning} serverDir={srv.dir} {onSearchOnline} /></div>
      {:else if tab === 'worlds'}
        <div class="sv-panel"><ServerWorldsTabPanel {serverId} running={isRunning} /></div>
      {:else if tab === 'network'}
        <div class="sv-scroll">
          <ServerNetworkTabPanel
            {srv}
            {isRunning}
            refreshKey={ipBarKey}
            {velocityProxyRunning}
            onAutoStartChange={(v) => { if (srv) srv = { ...srv, autoStartP2P: v } }}
          />
        </div>
      {:else if tab === 'appearance'}
        <div class="sv-scroll">
          <ServerAppearanceTab
            {srv}
            {isRunning}
            onUpdated={(updated) => { srv = updated }}
          />
        </div>
      {:else if tab === 'access'}
        <div class="sv-scroll">
          <div class="sv-card"><RemoteAccessPanel {serverId} /></div>
        </div>
      {:else if tab === 'settings'}
        <div class="sv-scroll">
          <ServerSettingsTab
            {srv}
            {isRunning}
            {isVelocity}
            onUpdated={(updated) => { srv = updated }}
            onDeleted={onBack}
            onOpenProperties={() => { view = 'properties' }}
          />
        </div>
      {:else if tab === 'files'}
        <div class="sv-panel"><ServerFilesTab {serverId} serverName={srv.name} running={isRunning} /></div>
      {/if}
    </div>
  </div>
{/if}

{#if shareOpen && srv}
  <ShareModal kind="server" refId={serverId} name={srv.name} onClose={() => shareOpen = false} />
{/if}

<!-- Розумний аналізатор запуску (модалка перед стартом) -->
{#if pfResult}
  <ServerPreflightModal
    result={pfResult}
    serverName={srv?.name ?? ''}
    onstart={pfProceed}
    onclose={pfCancel}
  />
{/if}

<style>
  .sv { display: flex; flex-direction: column; height: 100%; min-width: 0; gap: 10px; overflow: hidden; }
  .sv-loading { display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 12px; height: 100%; color: var(--text-mute); font-size: 13px; }
  .sv-loading i { font-size: 28px; color: var(--orange); }

  .sv-content { flex: 1; min-height: 0; min-width: 0; display: flex; flex-direction: column; }
  /* Консоль мусить займати ВСЮ ширину вкладки: flex + min-width:0
     знімає «тіньову» мінімальну ширину, яку давали довгі рядки логу
     (word-break: break-all) і тулбар з фільтрами/пошуком. */
  .sv-console-wrap { flex: 1; min-height: 0; min-width: 0; width: 100%; display: flex; }
  .sv-panel { flex: 1; min-height: 0; overflow: hidden; padding: 2px; display: flex; flex-direction: column; }
  .sv-scroll { flex: 1; overflow-y: auto; padding: 4px 2px 20px; }
  .sv-card { padding: 18px; border-radius: var(--r-lg, 16px); background: var(--bg-card); border: 1px solid var(--border); }
</style>
