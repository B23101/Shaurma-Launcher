<script lang="ts">
  // ── Сторінка сервера ──────────────────────────────────────────────────
  // Тонка обгортка: банер (ServerBanner, той самий вигляд, що в збірки) +
  // горизонтальний ковзний таббар розділів (.tabbar.tb-slide, як у збірки) +
  // пошук параметра в рядку дій (класи .st-search* із налаштувань) + вміст
  // поточного розділу з окремого файлу в tabs/. Жодної бізнес-логіки конкретної вкладки тут немає — лише
  // спільний стан (srv, живий статус/статистика, активний розділ), роутинг
  // між tabs/*.svelte і пошук, що веде до потрібного поля.
  //
  // Живий статус/статистика — через хуки useServerStatus/useServerStats:
  // сторінка НЕ застигає на моменті GetServer().
  //
  // Сервер друга (remoteOwnerId) — ТА САМА сторінка: той самий банер,
  // таббар і вкладки, але лише ті розділи, які дозволив власник (консоль,
  // мережа з P2P, мережа серверів проксі), а дані йдуть через віддалений
  // канал (remoteServers.svelte.ts), а не з локального бекенду.
  import { tick } from 'svelte'
  import { App } from '../wails'
  import { toast, confirmDialog } from '../toast.svelte'
  import { t } from '../i18n.svelte'
  import type { ServerView } from '../types'
  import { MOD_CORES, PLUGIN_CORES, PROP_HIDDEN } from './constants'
  import '../settings/settings.css'
  import { serverVisual } from './serverTargets'
  import { useServerStatus } from './hooks/useServerStatus.svelte'
  import { useServerStats } from './hooks/useServerStats.svelte'
  import ServerBanner from './components/ServerBanner.svelte'
  import ServerPropertiesPage from './ServerPropertiesPage.svelte'
  import ServerPreflightModal from './components/ServerPreflightModal.svelte'
  import type { ServerPreflight } from '../types'
  import ServerConsoleTab from './tabs/ServerConsoleTab.svelte'
  import ServerContentTab from './tabs/ServerContentTab.svelte'
  import ServerProxyTab from './tabs/ServerProxyTab.svelte'
  import ServerWorldsTabPanel from './tabs/ServerWorldsTabPanel.svelte'
  import ServerNetworkTabPanel from './tabs/ServerNetworkTabPanel.svelte'
  import ServerSettingsTab from './tabs/ServerSettingsTab.svelte'
  import RemoteAccessPanel from './components/RemoteAccessPanel.svelte'
  import ServerAppearanceTab from './tabs/ServerAppearanceTab.svelte'
  import ServerFilesTab from './tabs/ServerFilesTab.svelte'
  import ShareModal from '../ShareModal.svelte'
  import { slideTabs } from '../slideTabs'
  import { remote, remoteAct, remoteAsView, startRemote } from './remoteServers.svelte'

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
    // «Поділитися» сервером з друзями (обидві редакції).
    canShare = false,
    // Перейти на сторінку іншого сервера (учасники мережі проксі).
    onOpenServer = null,
    // Власник сервера друга ("" — свій сервер). serverId тоді — id сервера у власника.
    remoteOwnerId = '',
  }: {
    serverId?: string
    onBack?: () => void
    autoStart?: boolean
    onSearchOnline?: ((serverId: string, kind: 'mods' | 'plugins') => void) | null
    canShare?: boolean
    onOpenServer?: ((id: string) => void) | null
    remoteOwnerId?: string
  } = $props()

  // ── Сервер друга ──
  const isRemote = $derived(!!remoteOwnerId)
  $effect(() => { if (isRemote) return startRemote() })
  const rs = $derived(isRemote ? remote.servers.find(r => r.ownerId === remoteOwnerId && r.id === serverId) ?? null : null)
  const remoteOnline = $derived(!!rs?.ownerOnline)
  const remoteNick = $derived(remote.nicks[remoteOwnerId] ?? t('servers.remote.friend'))
  const remoteMembers = $derived(rs ? remote.servers.filter(r => r.ownerId === remoteOwnerId && r.network === rs.id) : [])

  let localSrv = $state<ServerView | null>(null)
  const srv = $derived(isRemote ? (rs ? remoteAsView(rs) : null) : localSrv)
  let shareOpen = $state(false)
  let loading = $state(true)
  let ipBarKey = $state(0)
  type TabId = 'proxy' | 'console' | 'mods' | 'plugins' | 'worlds' | 'network' | 'appearance' | 'access' | 'settings' | 'files'
  let tab = $state<TabId>('console')
  let actionBusy = $state(false)
  // 'properties' — окрема сторінка server.properties (як налаштування лаунчера)
  let view = $state<'main' | 'properties'>('main')

  async function loadServer() {
    if (isRemote) { loading = false; return }
    try { localSrv = await App.GetServer(serverId) } catch (e) { console.error(e) }
    finally { loading = false }
  }

  $effect(() => {
    if (!serverId) return
    loading = !isRemote
    loadServer()
  })

  // ── Живий статус (з подій, не застигає) ──
  const live = useServerStatus(() => serverId)
  const displayStatus = $derived(isRemote ? (remoteOnline ? rs?.state || 'stopped' : 'stopped') : (live.status || srv?.status || 'stopped'))
  const isRunning = $derived(displayStatus === 'running')

  // Після зупинки/краху — тихо перечитуємо GetServer (CoreInstalled/
  // LoaderVer/JavaInstalled можуть змінитись після встановлення ядра).
  let lastStatusSeen = ''
  $effect(() => {
    const s = live.status
    if (!isRemote && lastStatusSeen && lastStatusSeen !== s && (s === 'stopped' || s === 'crashed' || s === 'error')) loadServer()
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
  const liveStats = useServerStats(() => serverId, () => isRunning && !isRemote)

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
    if (isRemote) { if (rs) await remoteAct(rs, 'start'); return }
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
    if (isRemote) { if (rs) await remoteAct(rs, 'stop'); return }
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

  // Velocity — це не «окремий сервер», а система серверів: при відкритті
  // без автозапуску одразу показуємо «Мережу серверів».
  let platformTabApplied = $state(false)
  $effect(() => {
    if (!platformTabApplied && srv?.core === 'velocity' && !autoStart && !pendingAutoStart && tab === 'console') {
      platformTabApplied = true
      tab = 'proxy'
    }
  })

  // Чи є запущений Velocity проксі десь у бібліотеці (для IP-бару й
  // підказки при старті backend-сервера).
  let velocityProxyRunning = $state(false)
  $effect(() => {
    if (!serverId || isRemote) return
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

  // ── Розділи ─────────────────────────────────────────────────────────
  // Групи лишились (з них будуються підписи шляхів у пошуку), а показуються
  // розділи одним рядком вкладок, як на сторінці збірки. У вузькому вікні
  // таббар гортається, активна вкладка сама під'їжджає у видиму зону.
  // server.properties — теж вкладка (відкриває окрему сторінку).
  type NavItem = { id: TabId | 'properties'; icon: string; label: string }
  const navGroups = $derived.by(() => {
    if (isRemote) {
      // Друг бачить лише дозволене: мережу серверів проксі, консоль, P2P.
      const items: NavItem[] = []
      if (remoteMembers.length > 0) items.push({ id: 'proxy', icon: 'ti-topology-star-3', label: t('proxy.tab') })
      if (rs?.canConsole) items.push({ id: 'console', icon: 'ti-terminal-2', label: t('servers.tabs.console') })
      if (rs?.canStart || rs?.canStop) items.push({ id: 'network', icon: 'ti-network', label: t('servers.tabs.network') })
      return [{ id: 'manage', title: t('servers.nav.manage'), items }]
    }
    const manage: NavItem[] = [
      ...(isVelocity ? [{ id: 'proxy' as const, icon: 'ti-topology-star-3', label: t('proxy.tab') }] : []),
      { id: 'console', icon: 'ti-terminal-2', label: t('servers.tabs.console') },
      { id: 'network', icon: 'ti-network', label: t('servers.tabs.network') },
      { id: 'access', icon: 'ti-users-group', label: t('servers.nav.access') },
    ]
    const content: NavItem[] = []
    if (hasMods) content.push({ id: 'mods', icon: 'ti-puzzle', label: t('servers.tabs.mods') })
    if (hasPlugins) content.push({ id: 'plugins', icon: 'ti-plug', label: t('servers.tabs.plugins') })
    if (hasWorlds) content.push({ id: 'worlds', icon: 'ti-world', label: t('servers.tabs.worlds') })
    content.push({ id: 'files', icon: 'ti-files', label: t('servers.tabs.files') })
    const config: NavItem[] = [{ id: 'settings', icon: 'ti-adjustments-horizontal', label: t('servers.nav.basic') }]
    if (!isVelocity) config.push({ id: 'properties', icon: 'ti-file-settings', label: t('servers.tabs.properties') })
    config.push({ id: 'appearance', icon: 'ti-palette', label: t('servers.tabs.appearance') })
    return [
      { id: 'manage', title: t('servers.nav.manage'), items: manage },
      { id: 'content', title: t('servers.nav.content'), items: content },
      { id: 'config', title: t('servers.nav.config'), items: config },
    ]
  })
  const tabItems = $derived(navGroups.flatMap(g => g.items))
  // Друг: вкладка, на яку зникло право (або якої в нього нема), — на першу доступну.
  $effect(() => {
    if (isRemote && tabItems.length > 0 && !tabItems.some(i => i.id === tab)) tab = tabItems[0].id as TabId
  })

  let contentEl = $state<HTMLElement | null>(null)
  let propsFocus = $state('')

  function openTab(id: TabId | 'properties') {
    if (id === 'properties') { propsFocus = ''; view = 'properties'; return }
    tab = id
  }

  // ── Пошук параметра ─────────────────────────────────────────────────
  // «Де тут змінюється порт / пам'ять / хто з друзів має доступ?» — один
  // рядок пошуку шукає по всіх розділах сервера і по ключах
  // server.properties. Вибір відкриває розділ, прокручує до поля й коротко
  // підсвічує його (data-skey у вкладках, data-pkey у властивостях).
  type Hit = { id: string; section: TabId | 'properties'; key: string; icon: string; name: string; path: string; terms: string }
  const hit = (section: TabId | 'properties', key: string, icon: string, name: string, extra = ''): Hit => {
    const path = navGroups.flatMap(g => g.items).find(i => i.id === section)?.label ?? ''
    return { id: `${section}:${key}`, section, key, icon, name, path, terms: `${name} ${path} ${extra}`.toLowerCase() }
  }
  const staticHits = $derived.by<Hit[]>(() => {
    const out: Hit[] = [
      hit('settings', 'core', 'ti-cpu-2', t('servers.settings.core'), 'core version loader paper fabric forge ядро версія'),
      hit('settings', 'java', 'ti-coffee', 'Java', 'jdk jre джава'),
      hit('settings', 'ram', 'ti-device-sd-card', t('servers.settings.ram'), 'ram memory xmx heap памʼять пам\'ять озу гб'),
      hit('settings', 'args', 'ti-code', t('servers.settings.args'), `${t('servers.settings.jvmArgs')} ${t('servers.settings.programArgs')} jvm flags аргументи`),
      hit('settings', 'delete', 'ti-trash', t('servers.settings.delete'), `${t('servers.settings.danger')} delete remove видалити`),
      hit('appearance', 'name', 'ti-id', t('servers.settings.name'), 'name rename назва перейменувати'),
      hit('appearance', 'id', 'ti-folder', t('servers.settings.folderId'), 'id folder тека папка'),
      hit('appearance', 'notes', 'ti-notes', t('servers.settings.notes'), 'notes нотатки опис'),
      hit('appearance', 'look', 'ti-palette', t('servers.nav.hit.look'), 'color icon колір іконка вигляд'),
      hit('appearance', 'gameicon', 'ti-photo', t('servers.settings.gameIcon'), 'server-icon.png icon іконка'),
      hit('network', 'addr', 'ti-world', t('servers.net.access'), 'ip address адреса p2p підключення порт'),
      hit('access', 'friends', 'ti-users', t('servers.nav.hit.friends'), 'friends grant access друзі доступ права керування'),
      hit('access', 'pause', 'ti-player-pause', t('servers.nav.hit.pause'), 'pause disable призупинити вимкнути'),
      hit('access', 'stopplayers', 'ti-users-minus', t('servers.nav.hit.stopPlayers'), 'players stop гравці зупинка'),
      hit('access', 'privileged', 'ti-shield-lock', t('servers.nav.hit.privileged'), 'op deop whitelist оператор привілейовані'),
      hit('access', 'audit', 'ti-history', t('servers.nav.hit.audit'), 'log audit history журнал історія'),
      hit('console', '', 'ti-terminal-2', t('servers.tabs.console'), 'console log commands консоль лог команди'),
      hit('files', '', 'ti-files', t('servers.tabs.files'), 'files configs файли конфіги'),
    ]
    if (!isVelocity) out.push(hit('settings', 'port', 'ti-plug', t('servers.settings.port'), 'port порт 25565'))
    if (hasWorlds) out.push(hit('worlds', '', 'ti-world', t('servers.tabs.worlds'), 'world backup світ бекап резервна копія'))
    if (hasMods) out.push(hit('mods', '', 'ti-puzzle', t('servers.tabs.mods'), 'mods моди'))
    if (hasPlugins) out.push(hit('plugins', '', 'ti-plug', t('servers.tabs.plugins'), 'plugins плагіни'))
    return out
  })

  // Ключі server.properties підвантажуємо лише при першому фокусі пошуку.
  let propHits = $state<Hit[]>([])
  let propHitsFor = ''
  async function loadPropHits() {
    if (isVelocity || propHitsFor === serverId) return
    propHitsFor = serverId
    try {
      const doc = await App.GetServerProperties(serverId)
      const label = (k: string, kind: 'name' | 'hint') => { const key = `props.${k}.${kind}`; const v = t(key); return v === key ? '' : v }
      propHits = (doc?.entries ?? [])
        .filter(e => !e.key.startsWith('#') && !PROP_HIDDEN.has(e.key))
        .map(e => {
          const name = label(e.key, 'name') || e.key
          return { id: `p:${e.key}`, section: 'properties' as const, key: e.key, icon: 'ti-file-settings', name,
            path: name === e.key ? 'server.properties' : `server.properties · ${e.key}`,
            terms: `${name} ${e.key} ${label(e.key, 'hint')}`.toLowerCase() }
        })
    } catch { propHitsFor = '' }
  }

  let query = $state('')
  let searchFocused = $state(false)
  let activeHit = $state(0)
  let searchEl = $state<HTMLInputElement | null>(null)
  const hits = $derived.by<Hit[]>(() => {
    const q = query.trim().toLowerCase()
    if (!q) return []
    const words = q.split(/\s+/)
    const score = (h: Hit) => (h.name.toLowerCase().startsWith(q) ? 0 : h.name.toLowerCase().includes(q) ? 1 : 2)
    return [...staticHits, ...propHits]
      .filter(h => words.every(w => h.terms.includes(w)))
      .sort((a, b) => score(a) - score(b))
      .slice(0, 12)
  })
  $effect(() => { void query; activeHit = 0 })

  function splitMatch(name: string) {
    const q = query.trim()
    const i = q ? name.toLowerCase().indexOf(q.toLowerCase()) : -1
    if (i < 0) return { pre: name, mid: '', post: '' }
    return { pre: name.slice(0, i), mid: name.slice(i, i + q.length), post: name.slice(i + q.length) }
  }

  async function pickHit(h: Hit) {
    query = ''
    searchEl?.blur()
    if (h.section === 'properties') { propsFocus = h.key; view = 'properties'; return }
    tab = h.section
    if (!h.key) return
    // Вкладка монтується й підвантажує дані — даємо їй кілька кадрів.
    for (let i = 0; i < 20; i++) {
      await tick()
      const el = contentEl?.querySelector<HTMLElement>(`[data-skey="${CSS.escape(h.key)}"]`)
      if (el) {
        el.scrollIntoView({ block: 'center', behavior: 'smooth' })
        el.classList.remove('sv-flash'); void el.offsetWidth; el.classList.add('sv-flash')
        setTimeout(() => el.classList.remove('sv-flash'), 1800)
        return
      }
      await new Promise(r => setTimeout(r, 50))
    }
  }

  function onSearchKey(e: KeyboardEvent) {
    if (e.key === 'ArrowDown') { e.preventDefault(); activeHit = Math.min(hits.length - 1, activeHit + 1) }
    else if (e.key === 'ArrowUp') { e.preventDefault(); activeHit = Math.max(0, activeHit - 1) }
    else if (e.key === 'Enter' && hits[activeHit]) { e.preventDefault(); void pickHit(hits[activeHit]) }
    else if (e.key === 'Escape') { query = ''; searchEl?.blur() }
  }

  // Ctrl+K — до пошуку параметрів (Ctrl+F лишається пошуком у консолі).
  $effect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && !e.shiftKey && !e.altKey && e.key.toLowerCase() === 'k' && view === 'main') {
        e.preventDefault(); searchEl?.focus(); searchEl?.select()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })
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
      focusKey={propsFocus}
      onBack={() => { view = 'main'; propsFocus = '' }}
      onOpenSettings={() => { tab = 'settings'; view = 'main'; propsFocus = '' }}
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
      onShare={canShare && !isVelocity && !isRemote ? () => { shareOpen = true } : null}
      onOpenConsole={() => { App.ToggleServerConsoleWindow(serverId).catch((e: any) => toast(String(e?.message ?? e), 'error')) }}
      remoteOwner={isRemote ? remoteNick : ''}
      canStart={!isRemote || (remoteOnline && !!rs?.canStart)}
      canStop={!isRemote || (remoteOnline && !!rs?.canStop)}
    >
      {#snippet toolbarExtra()}
        {#if !isRemote}
        <!-- Пошук параметра по всіх розділах і ключах server.properties -->
        <div class="st-search sv-search">
          <i class="ti ti-search st-search-ic"></i>
          <input type="text" placeholder={t('servers.nav.searchPh')} bind:value={query} bind:this={searchEl}
                 autocomplete="off" spellcheck="false" aria-label={t('servers.nav.searchPh')} onkeydown={onSearchKey}
                 onfocus={() => { searchFocused = true; void loadPropHits() }}
                 onblur={() => setTimeout(() => (searchFocused = false), 150)} />
          {#if query}
            <button class="st-search-clear" onmousedown={(e) => e.preventDefault()} onclick={() => (query = '')} aria-label={t('servers.lib.clear')}><i class="ti ti-x"></i></button>
          {:else}
            <kbd class="sv-kbd">Ctrl K</kbd>
          {/if}
          {#if query.trim() && searchFocused}
            <div class="st-search-menu" role="listbox">
              {#each hits as h, i (h.id)}
                {@const name = splitMatch(h.name)}
                <button class="st-hit" class:active={i === activeHit} role="option" aria-selected={i === activeHit}
                        onmousedown={(e) => e.preventDefault()} onclick={() => pickHit(h)} onmouseenter={() => (activeHit = i)}>
                  <i class="ti {h.icon}"></i>
                  <div>
                    <div class="st-hit-name">{name.pre}{#if name.mid}<mark>{name.mid}</mark>{/if}{name.post}</div>
                    <div class="st-hit-path">{h.path}</div>
                  </div>
                </button>
              {:else}
                <div class="st-search-empty">{t('st.search.none')}</div>
              {/each}
            </div>
          {/if}
        </div>
        {/if}
      {/snippet}
    </ServerBanner>

    <!-- ═══ ВКЛАДКИ (той самий ковзний таббар, що в збірки) ═══ -->
    <div class="tabbar tb-slide sv-tabs" use:slideTabs aria-label={srv.name} role="tablist">
      {#each tabItems as it (it.id)}
        <button class="tab-btn" class:active={tab === it.id} role="tab" aria-selected={tab === it.id} onclick={() => openTab(it.id)}>
          <i class="ti {it.icon}"></i> {it.label}
          {#if it.id === 'properties'}<i class="ti ti-arrow-up-right sv-tab-out"></i>{/if}
        </button>
      {/each}
    </div>

    <!-- ═══ ВМІСТ РОЗДІЛУ ═══ -->
    <div class="sv-content" bind:this={contentEl}>
      {#if isRemote && !remoteOnline}
        <div class="sv-card sv-msg"><i class="ti ti-plug-off"></i> {t('servers.remote.offline')}</div>
      {:else if isRemote && tabItems.length === 0}
        <div class="sv-card sv-msg"><i class="ti ti-lock"></i> {t('servers.remote.noConsole')}</div>
      {:else if tab === 'console'}
        {#key `console:${remoteOwnerId}:${serverId}`}
          <div class="sv-console-wrap">
            <ServerConsoleTab {serverId} serverColor={pcol} autoStart={wantAutoStart && !isRemote} preflight={runPreflight} remoteServer={rs} />
          </div>
        {/key}
      {:else if tab === 'proxy' && (isVelocity || isRemote)}
        <div class="sv-scroll"><ServerProxyTab {srv} {onOpenServer} remoteServer={rs} /></div>
      {:else if tab === 'mods'}
        <!-- Спільний менеджер модів збірок (PackModsSection) — він не має
             власного скролу, тому в .sv-scroll, а не в .sv-panel. -->
        <div class="sv-scroll"><ServerContentTab {srv} kind="mods" running={isRunning} {onSearchOnline} /></div>
      {:else if tab === 'plugins'}
        <div class="sv-scroll"><ServerContentTab {srv} kind="plugins" running={isRunning} {onSearchOnline} /></div>
      {:else if tab === 'worlds'}
        <div class="sv-panel"><ServerWorldsTabPanel {serverId} running={isRunning} /></div>
      {:else if tab === 'network'}
        <div class="sv-scroll">
          <ServerNetworkTabPanel
            {srv}
            remoteServer={rs}
            {isRunning}
            refreshKey={ipBarKey}
            {velocityProxyRunning}
            onAutoStartChange={(v) => { if (localSrv) localSrv = { ...localSrv, autoStartP2P: v } }}
          />
        </div>
      {:else if tab === 'appearance'}
        <div class="sv-scroll">
          <ServerAppearanceTab
            {srv}
            {isRunning}
            onUpdated={(updated) => { localSrv = updated }}
          />
        </div>
      {:else if tab === 'access'}
        <div class="sv-scroll">
          <div class="sv-card"><RemoteAccessPanel {serverId} isProxy={isVelocity} /></div>
        </div>
      {:else if tab === 'settings'}
        <div class="sv-scroll">
          <ServerSettingsTab
            {srv}
            {isRunning}
            {isVelocity}
            onUpdated={(updated) => { localSrv = updated }}
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
  .sv { display: flex; flex-direction: column; height: 100%; min-width: 0; overflow: hidden; }
  .sv-loading { display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 12px; height: 100%; color: var(--text-mute); font-size: 13px; }
  .sv-loading i { font-size: 28px; color: var(--orange); }

  /* Пошук параметра — компактне поле в рядку дій банера. */
  .sv-search.st-search { flex: 0 1 260px; min-width: 170px; max-width: 260px; margin: 0; }
  .sv-search.st-search input { height: 32px; padding-right: 58px; }
  .sv-kbd {
    position: absolute; right: 9px; top: 50%; transform: translateY(-50%); pointer-events: none;
    padding: 3px 6px; border-radius: 6px; border: 1px solid var(--border); background: var(--hover);
    font: 600 var(--fs-min)/1 var(--font-mono); color: var(--text-dim);
  }
  /* Вкладки — акцент кольором сервера. */
  .sv-tabs { --tb-acc: var(--pcol); flex-shrink: 0; margin-bottom: 14px; }
  .sv-tab-out { font-size: 12px !important; opacity: .55; margin-left: -3px; }
  .sv-content { flex: 1; min-height: 0; min-width: 0; display: flex; flex-direction: column; }
  /* Підсвітка поля, до якого перейшли з пошуку. */
  .sv-content :global(.sv-flash) { animation: sv-flash 1.6s ease; border-radius: 12px; }
  @keyframes sv-flash {
    0%, 35% { box-shadow: 0 0 0 2px var(--pcol), 0 0 0 7px color-mix(in srgb, var(--pcol) 22%, transparent); }
    100% { box-shadow: 0 0 0 0 transparent; }
  }
  /* Консоль мусить займати ВСЮ ширину вкладки: flex + min-width:0
     знімає «тіньову» мінімальну ширину, яку давали довгі рядки логу
     (word-break: break-all) і тулбар з фільтрами/пошуком. */
  .sv-console-wrap { flex: 1; min-height: 0; min-width: 0; width: 100%; display: flex; }
  .sv-panel { flex: 1; min-height: 0; overflow: hidden; padding: 2px; display: flex; flex-direction: column; }
  .sv-scroll { flex: 1; overflow-y: auto; padding: 4px 2px 20px; }
  .sv-card { padding: 18px; border-radius: var(--r-lg, 16px); background: var(--bg-card); border: 1px solid var(--border); }
  .sv-msg { display: flex; align-items: center; gap: 10px; color: var(--text-mute); font-size: 13px; }
  .sv-msg i { font-size: 20px; }
</style>
