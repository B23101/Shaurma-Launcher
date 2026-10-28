<script lang="ts">
  // ── Вкладка "Консоль" сторінки сервера ───────────────────────────────
  // Обгортка навколо ConsoleView + ConsoleInput. Буфер логів і підписка
  // на server:line/server:status/server:install-progress живуть у хуку
  // useServerConsole — цей файл лише малює toolbar/термінал/інпут і
  // тримає локальний UI-стан (фільтр, пошук, ШІ-аналіз крашу).
  //
  // remoteServer — ця сама вкладка для сервера друга: рядки й команди йдуть
  // через віддалений канал (remoteServers.svelte.ts), а дії, яких у друга
  // нема (окреме вікно, ШІ-аналіз, гравці), приховано.
  import { App } from '../../wails'
  import { toast } from '../../toast.svelte'
  import { t } from '../../i18n.svelte'
  import ConsoleView from '../components/ConsoleView.svelte'
  import ConsoleInput from '../components/ConsoleInput.svelte'
  import ServerPlayersStrip from '../components/ServerPlayersStrip.svelte'
  import { useServerConsole } from '../hooks/useServerConsole.svelte'
  import { onDestroy, untrack } from 'svelte'
  import { remote, remoteOpenConsole, remoteCloseConsole, remoteSend } from '../remoteServers.svelte'
  import type { RemoteServer } from '../../types'
  import type { LogLine } from '../components/ConsoleView.svelte'

  let {
    serverId = '',
    serverColor = 'var(--orange)',
    autoStart = false,
    // standalone — ця сама консоль у ОКРЕМОМУ вікні (ServerConsoleWindow):
    // тут немає банера сторінки сервера, тож кнопки Старт/Стоп/Kill —
    // ЄДИНЕ місце, де їх видно. У вбудованій вкладці вони навмисно
    // приховані (дублювали банер і лише заплутували — «нащо кнопки
    // запуску в розділі, коли поруч банер з тими самими»).
    standalone = false,
    // preflight — «розумний аналізатор» перед стартом (див.
    // ServerPreflightModal): повертає false, якщо людина скасувала запуск
    // або аналізатор знайшов блокер. Пусто/null = стартувати одразу
    // (окреме вікно консолі працює без модалки — там її ніде показати).
    preflight = null,
    remoteServer = null,
  }: {
    serverId?: string
    serverColor?: string
    autoStart?: boolean
    standalone?: boolean
    preflight?: (() => Promise<boolean>) | null
    remoteServer?: RemoteServer | null
  } = $props()

  // Режим фіксований на час монтування (сторінка перемонтовує вкладку за ключем).
  const isRemote = !!untrack(() => remoteServer)
  const console_ = isRemote ? null : useServerConsole(() => serverId)

  // ── Віддалена консоль (сервер друга) ──
  const remoteMine = $derived(!!remoteServer && remote.consoleFor?.ownerId === remoteServer.ownerId && remote.consoleFor?.id === remoteServer.id)
  let remoteCleared = $state(0) // «Очистити» в друга — локально ховаємо вже показане
  const remoteLines = $derived<LogLine[]>(remoteMine
    ? remote.lines.slice(remoteCleared).map(l => ({ id: l.i, time: l.t, level: l.l, text: l.x }))
    : [])
  $effect(() => {
    if (!isRemote) return
    const r = remoteServer
    if (r?.ownerOnline && r.canConsole) untrack(() => { if (!remoteMine) void remoteOpenConsole(r) })
    else untrack(() => { if (remoteMine) void remoteCloseConsole() })
  })
  onDestroy(() => { if (isRemote) void remoteCloseConsole() })

  const lines = $derived(isRemote ? remoteLines : console_!.lines)
  const status = $derived(isRemote ? (remoteServer?.state || 'stopped') : console_!.status)
  const installProgress = $derived(console_?.installProgress ?? null)
  const canSend = $derived(!isRemote || (!!remoteServer?.canExec && !remote.ended))
  function addLine(level: string, text: string) { console_?.addLine(level, text) }
  function clearLines() {
    if (isRemote) remoteCleared = remote.lines.length
    else console_!.clear()
  }

  let filter = $state<'all' | 'info' | 'warn' | 'error'>('all')
  let search = $state('')
  let command = $state('')
  let aiState = $state<'idle' | 'thinking' | 'done'>('idle')
  let aiText = $state('')

  const installing = $derived(status === 'installing')
  const running = $derived(status === 'running')
  const errorCount = $derived(lines.filter(l => l.level === 'error').length)
  const warnCount = $derived(lines.filter(l => l.level === 'warn').length)

  // Автоскрол живе всередині ConsoleView (він сам знає, чи користувач
  // унизу, і сам тримає «прилипання» до дна). Тут лише передаємо розмір
  // шрифту — і жодних requestAnimationFrame з батька.

  // autoStart — сторінка щойно відкрилась одразу після створення сервера
  // з увімкненою опцією «Створити й одразу запустити» — стартуємо самі,
  // щойно бачимо, що сервер ще stopped.
  //
  // ВАЖЛИВО: чекаємо console_.statusLoaded, а не лише console_.status
  // === 'stopped' — доки реальний статус не прийшов з бекенду
  // (GetServerStatus), хук показує ДЕФОЛТНЕ значення "stopped", яке НЕ
  // означає, що сервер справді зупинений. Без цієї перевірки автостарт
  // міг спрацювати ще раз для вже запущеного сервера (напр. якщо
  // сторінку відкрили повторно, поки автозапуск з попереднього разу ще
  // триває) — бекенд тоді відповідає помилкою "сервер вже запущено".
  let autoStartFired = false
  $effect(() => {
    if (autoStart && !autoStartFired && serverId && console_?.statusLoaded && status === 'stopped') {
      autoStartFired = true
      startServer()
    }
  })

  // starting — захист від подвійного натискання/подвійного виклику:
  // поки попередній App.StartServer ще не відповів, повторні виклики
  // (кнопка "Старт" ще раз, або автостарт) ігноруються замість того,
  // щоб бекенд повертав "сервер вже запущено".
  let starting = false
  async function startServer() {
    if (starting || running || installing) return
    // Аналізатор перед стартом: людина бачить порт/RAM/Java/EULA/диск і
    // вирішує — продовжити чи ні (можна ще й «більше не показувати»).
    if (preflight) {
      let ok = false
      try { ok = await preflight() } catch { ok = true }
      if (!ok) return
    }
    starting = true
    try { addLine('system', t('servers.console.sysStarting')); await App.StartServer(serverId) }
    catch (e: any) { addLine('error', `[ERROR] ${e}`); toast(String(e), 'error') }
    finally { starting = false }
  }
  async function stopServer() {
    try { addLine('system', t('servers.console.sysStopping')); await App.StopServer(serverId, true) }
    catch (e: any) { addLine('error', `[ERROR] ${e}`) }
  }
  async function killServer() {
    // Kill доступний і під час starting/installing (саме тоді він єдиний
    // спосіб зупинити сервер, що «завис» на встановленні ядра/Java).
    try { await App.StopServer(serverId, false) }
    catch (e: any) { addLine('error', `[ERROR] ${e}`) }
  }

  // Кнопка «у вікні»: у вбудованій вкладці відкриває/фокусує окреме
  // вікно консолі САМЕ цього сервера (як у збірках); у standalone-режимі
  // кнопки немає — це вже те вікно.
  function toggleWindow() {
    App.ToggleServerConsoleWindow(serverId).catch((e: any) => toast(String(e), 'error'))
  }

  function sendCmd(cmd: string) {
    if (!running || !canSend) return
    if (isRemote) { void remoteSend(cmd); return }
    addLine('system', `> ${cmd}`)
    App.SendServerCommand(serverId, cmd).catch((e: any) => addLine('error', `[ERROR] ${e}`))
  }

  async function analyzeCrash() {
    aiState = 'thinking'; aiText = ''
    try {
      const res = await App.AnalyzeServerCrash(serverId)
      aiText = !res || !res.hasCrash
        ? (res?.error || t('servers.console.noCrashReports'))
        : `${t('servers.console.file', { name: res.fileName })}${res.cacheHit ? ' ' + t('servers.console.fromCache') : ''}\n\n${res.analysis}`
      aiState = 'done'
    } catch (e: any) { aiState = 'idle'; toast(String(e), 'error') }
  }

  function copyAll() {
    const text = lines.map(l => `[${l.time}] [${l.level.toUpperCase()}] ${l.text}`).join('\n')
    navigator.clipboard.writeText(text).catch(() => {})
    toast(t('servers.console.copied'), 'success')
  }

  // Очищення буфера перегляду — та сама дія, що «Кошик» у консолі збірок.
  function clearLog() {
    clearLines()
    filter = 'all'
    search = ''
  }

  const emptyText = $derived(installing ? t('servers.console.installing') : running ? t('servers.console.waiting') : t('servers.console.empty'))
</script>

<div class="sct" class:standalone style="--pcol:{serverColor}">
  <!-- Toolbar: рівні, пошук, дії. Стиль такий самий, що в GameConsole. -->
  <div class="sct-tools">
    <!-- Фільтри-пігулки як .fchip у GameConsole: нейтральні, без кольорових точок (OLED). -->
    <div class="gc-seg" role="tablist">
      <button class="fchip" class:active={filter === 'all'} onclick={() => filter = 'all'}>{t('servers.console.all')} <span class="fchip-n">{lines.length}</span></button>
      <button class="fchip" class:active={filter === 'info'} onclick={() => filter = 'info'}>Info</button>
      <button class="fchip fc-warn" class:active={filter === 'warn'} onclick={() => filter = 'warn'}>Warn <span class="fchip-n">{warnCount}</span></button>
      <button class="fchip fc-err" class:active={filter === 'error'} onclick={() => filter = 'error'}>Errors <span class="fchip-n">{errorCount}</span></button>
    </div>
    <div class="gc-search">
      <i class="ti ti-search"></i>
      <input placeholder={t('servers.console.search')} bind:value={search} />
      {#if search}
        <i class="ti ti-x gc-search-clear" onclick={() => search = ''} role="button" tabindex="0" onkeydown={() => {}}></i>
      {/if}
    </div>
    <div class="sct-right">
      <!-- Старт/Стоп/Kill — ЛИШЕ в окремому вікні (тут немає банера
           сторінки, який ними керує). У вбудованій вкладці вони зайві:
           ті самі кнопки є на банері сервера. -->
      {#if standalone}
        {#if running}
          <button class="btn btn-ghost btn-sm" onclick={stopServer}><i class="ti ti-player-stop"></i> {t('servers.console.stop')}</button>
        {:else if !installing}
          <button class="btn btn-primary btn-sm" onclick={startServer}><i class="ti ti-player-play"></i> {t('servers.console.start')}</button>
        {/if}
        {#if running || installing || status === 'starting'}
          <button class="btn btn-ghost btn-sm sct-danger" onclick={killServer} title={t('servers.console.kill')}><i class="ti ti-x"></i> Kill</button>
        {/if}
      {:else if isRemote}
        {#if remote.secure === true}
          <span class="st-tag ok" title={t('servers.remote.encryptedTip')}><i class="ti ti-lock"></i> {t('servers.remote.encrypted')}</span>
        {:else if remote.secure === false}
          <span class="st-tag" title={t('servers.remote.viaVpsTip')}><i class="ti ti-lock-open"></i> {t('servers.remote.viaVps')}</span>
        {/if}
        {#if remote.ended && remoteServer}
          <button class="tbtn" onclick={() => remoteServer && remoteOpenConsole(remoteServer)} title={t('servers.remote.reconnect')} aria-label={t('servers.remote.reconnect')}><i class="ti ti-refresh"></i></button>
        {/if}
      {:else}
        <button class="tbtn" onclick={toggleWindow} title={t('servers.console.separateWindow')} aria-label={t('servers.console.separateWindow')}>
          <i class="ti ti-arrows-maximize"></i>
        </button>
      {/if}
      <button class="tbtn" onclick={copyAll} title={t('servers.console.copyLogs')} aria-label={t('servers.console.copyLogs')}><i class="ti ti-copy"></i></button>
      <button class="tbtn" onclick={clearLog} title={t('servers.console.clearView')} aria-label={t('servers.console.clearView')}><i class="ti ti-trash"></i></button>
      {#if status === 'crashed' && !isRemote}
        <button class="btn btn-ghost btn-sm" onclick={analyzeCrash} disabled={aiState === 'thinking'}>
          {#if aiState === 'thinking'}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-sparkles"></i>{/if}
          {t('servers.console.aiAnalysis')}
        </button>
      {/if}
    </div>
  </div>

  <!-- Хто зараз на сервері (ніки). Сповіщення про вхід/вихід показує
       App.svelte глобально — тут лише живий список. -->
  {#if !isRemote}<ServerPlayersStrip {serverId} running={running} />{/if}

  {#if installing && installProgress}
    <div class="sct-install">
      <div class="sct-install-head">
        <i class="ti ti-server" style="color:var(--pcol, var(--orange))"></i>
        <span class="sct-install-stage">{installProgress.stage === 'java' ? 'Java' : t('servers.metrics.core')}</span>
        <span class="sct-install-msg">{installProgress.message}</span>
        {#if installProgress.percent >= 0}<span class="sct-install-pct">{installProgress.percent}%</span>{/if}
      </div>
      <div class="sct-install-bar">
        {#if installProgress.percent >= 0}
          <div class="sct-install-fill" style="width:{installProgress.percent}%"></div>
        {:else}
          <div class="sct-install-fill sct-install-indet"></div>
        {/if}
      </div>
    </div>
  {/if}

  <ConsoleView
    {lines}
    {filter}
    {search}
    emptyIcon={running ? 'ti-terminal' : 'ti-server-off'}
    {emptyText}
  />

  {#if aiState !== 'idle' && aiText}
    <div class="sct-ai">
      <div class="sct-ai-head"><i class="ti ti-sparkles"></i> {t('servers.console.crashAnalysis')}</div>
      <div class="sct-ai-body">{aiText}</div>
    </div>
  {/if}

  <ConsoleInput
    bind:value={command}
    disabled={!running || !canSend}
    placeholder={isRemote && !remoteServer?.canExec ? t('servers.remote.readOnly')
      : running ? t('servers.console.commandPlaceholder') : t('servers.console.empty')}
    onsubmit={sendCmd}
  />
</div>

<style>
  /* width:100% + min-width:0 — консоль у вкладці мусить займати ВСЮ
     ширину контейнера: раніше flex-ланцюг міг стискати її по контенту
     (довгі рядки з word-break розтягували «тіньову» мінімальну ширину). */
  .sct {
    display: flex; flex-direction: column; height: 100%; width: 100%;
    min-width: 0; min-height: 0;
    background: var(--bg-panel); border-radius: 14px; overflow: hidden;
    border: 1px solid var(--border);
  }
  /* Окреме вікно: без картки-рамки — консоль на все вікно, як .gc.standalone. */
  .sct.standalone {
    border: none; border-radius: 0;
    background:
      radial-gradient(900px 500px at 12% -5%, rgba(var(--orange-rgb), .06), transparent 60%),
      radial-gradient(900px 600px at 100% 110%, rgba(123, 77, 255, .07), transparent 55%),
      var(--bg-app);
  }

  /* Тулбар — той самий словник, що .gc-tools у GameConsole. */
  .sct-tools {
    display: flex; align-items: center; gap: 6px; padding: 9px 14px;
    border-bottom: 1px solid var(--border); flex-wrap: wrap;
    background: color-mix(in srgb, var(--bg-window) 92%, transparent);
    flex-shrink: 0;
  }
  .gc-seg {
    display: flex; gap: 2px; padding: 3px; border-radius: 12px;
    background: var(--bg-input); border: 1px solid var(--border);
  }
  .fchip {
    padding: 5px 12px; border-radius: 9px; font-size: var(--fs-min); font-weight: 700;
    background: transparent; border: none; color: var(--text-mute);
    cursor: pointer; transition: background-color .15s, color .15s, box-shadow .15s; display: flex; align-items: center; gap: 6px;
  }
  .fchip:hover { color: var(--text); background: color-mix(in srgb, var(--text) 6%, transparent); }
  .fchip.active { background: var(--hover-strong); color: var(--text); box-shadow: 0 1px 6px rgba(0,0,0,.28), inset 0 0 0 1px var(--border-hi); }
  .fchip.fc-warn.active { color: #ffd166; }
  .fchip.fc-err.active { color: #ff6b6b; }
  .fchip-n { font-family: var(--font-mono); font-size: var(--fs-min); opacity: .8; }
  .gc-search {
    display: flex; align-items: center; gap: 6px;
    background: var(--bg-input); border: 1px solid var(--border); border-radius: 12px;
    padding: 6px 12px; min-width: 150px; flex: 1; max-width: 280px;
    transition: border-color .15s, box-shadow .15s;
  }
  .gc-search:focus-within { border-color: rgba(var(--orange-rgb), .55); box-shadow: 0 0 0 3px rgba(var(--orange-rgb), .12); }
  .gc-search i { font-size: 13px; color: var(--text-mute); }
  .gc-search input { background: none; border: none; outline: none; color: var(--text); font: 400 11px var(--font-ui); width: 100%; }
  .gc-search input::placeholder { color: var(--text-dim); }
  .gc-search-clear { cursor: pointer; }
  .sct-right { display: flex; gap: 5px; margin-left: auto; align-items: center; }
  .tbtn {
    width: 27px; height: 27px; border-radius: var(--r-sm, 7px);
    background: var(--bg-panel); border: 1px solid var(--border);
    color: var(--text-mute); display: flex; align-items: center; justify-content: center;
    font-size: 13px; cursor: pointer; transition: .15s;
  }
  .tbtn:hover { color: var(--text); border-color: var(--border-hi); }
  .sct-danger { color: #ef4444 !important; border-color: rgba(239, 68, 68, .3) !important; }
  .sct-danger:hover { background: rgba(239, 68, 68, .1) !important; }

  .sct-install {
    padding: 10px 14px; border-bottom: 1px solid var(--border);
    background: color-mix(in srgb, var(--pcol, var(--orange)) 5%, var(--bg-panel));
  }
  .sct-install-head { display: flex; align-items: center; gap: 6px; margin-bottom: 6px; }
  .sct-install-head i { font-size: 14px; }
  .sct-install-stage { font: 700 11px/1 var(--font-ui); color: var(--pcol, var(--orange)); padding: 2px 6px; border-radius: 4px; background: color-mix(in srgb, var(--pcol, var(--orange)) 12%, transparent); }
  .sct-install-msg { font: 500 11px/1 var(--font-ui); color: var(--text); flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .sct-install-pct { font: 600 var(--fs-min) var(--font-mono); color: var(--pcol, var(--orange)); }
  .sct-install-bar { width: 100%; height: 4px; border-radius: 2px; background: var(--hover); overflow: hidden; }
  .sct-install-fill { height: 100%; border-radius: 2px; background: var(--pcol, var(--orange)); transition: width .3s ease; }
  .sct-install-fill.sct-install-indet { width: 40%; animation: sct-install-slide 1.8s ease-in-out infinite; }
  @keyframes sct-install-slide { 0% { transform: translateX(-100%); } 100% { transform: translateX(350%); } }

  .sct-ai {
    border-top: 1px solid var(--border); padding: 10px 12px;
    background: color-mix(in srgb, var(--orange) 4%, var(--bg-panel));
  }
  .sct-ai-head { font: 700 11px/1 var(--font-ui); color: var(--orange); margin-bottom: 6px; display: flex; align-items: center; gap: 6px; }
  .sct-ai-body { font-size: 12px; line-height: 1.5; color: var(--text); white-space: pre-wrap; }
</style>
