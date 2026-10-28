<script lang="ts">
  // ── Бібліотека серверів ────────────────────────────────────────────────
  // Редизайн за скаргою «сухо, не красиво, лише існує, а не аналізує».
  //
  // Що змінилось:
  //   • ПАНЕЛЬ АНАЛІТИКИ тепер реально працює. Раніше: (1) перше опитування
  //     приходило лише через 3 с після старту, до того показувались нулі;
  //     (2) «Пам'ять» складала RSS процесів (java.exe), тож показувала 5.2 ГБ
  //     там, де на сторінці сервера — 2.7/4.5 ГБ (це купа JVM); (3) жодної
  //     історії — лише поточне число. Тепер: опитування стартує одразу й при
  //     кожній зміні набору запущених серверів, RAM — купа JVM / виділено /
  //     системна, CPU і RAM мають міні-графіки (останні ~90 с).
  //   • БЛОК «АНАЛІЗ»: перевіряє ВСІ сервери й показує, що не так — краші,
  //     відсутня Java/ядро, конфлікти портів, перевиділення пам'яті, купа
  //     майже повна, перевантажений CPU, довгі паузи GC, сервер, що не
  //     відповідає на порту, вимкнений автобекап. Клік — відкриває сервер.
  //   • Картки — ServerCard із живими смужками CPU/RAM/гравців.
  //   • Усі тексти — через i18n (раніше жорстко українською).
  //
  // ЖИВИЙ СТАН: підписка на server:status / server:install-progress +
  // тихе перечитування списку, поки щось запущене.
  import { untrack } from 'svelte'
  import { App, on } from '../wails'
  import { toast } from '../toast.svelte'
  import { t } from '../i18n.svelte'
  import type { ServerView, ServerInstallProgress, ServerLiveStats, SystemInfo, ServerFolder, ServerPreflight, RemoteServer } from '../types'
  import { CORE_META, ONLINE_STATUSES } from './constants'
  import SelectField from '../SelectField.svelte'
  import ServerCard from './components/ServerCard.svelte'
  import ServerDeleteModal from './components/ServerDeleteModal.svelte'
  import ServerPreflightModal from './components/ServerPreflightModal.svelte'
  import Portal from '../Portal.svelte'
  import FolderViewModal from '../FolderViewModal.svelte'
  import FolderEditorModal from '../FolderEditorModal.svelte'
  import FolderTile from '../FolderTile.svelte'
  import { safeFolderIcon } from '../folderMeta'
  import { serverIcon } from './utils'
  import { remote as rs, remoteKey, remoteAsView, remoteAct, startRemote } from './remoteServers.svelte'

  let {
    onOpenServer = () => {},
    onCreateServer = () => {},
    onOpenRemote = () => {},
  }: {
    onOpenServer?: (id: string, autoStart?: boolean) => void
    onCreateServer?: () => void
    // Сервер друга → окрема сторінка (старт/стоп/консоль — лише з дозволеного власником).
    onOpenRemote?: (ownerId: string, serverId: string) => void
  } = $props()

  let servers = $state<ServerView[]>([])
  let loading = $state(true)
  let search = $state('')
  let filterCore = $state('all')
  // Фільтр стану — групами «онлайн / офлайн» (взаємовиключні).
  let filterStatus = $state<'all' | 'online' | 'offline'>('all')
  // Сортування списку; за замовчуванням — запущені першими.
  let sortBy = $state<'status' | 'name' | 'core' | 'version' | 'port'>('status')

  // Прогрес встановлення ядра на картці (server:install-progress).
  let installProgress = $state<Record<string, ServerInstallProgress>>({})

  async function loadServers(silent = false) {
    if (!silent) loading = true
    try {
      servers = (await App.ListServers()) ?? []
    } catch (e) {
      console.error(e)
      if (!silent) servers = []
    } finally {
      if (!silent) loading = false
    }
  }

  const runningServers = $derived(servers.filter(s => s.status === 'running'))
  const runningCount = $derived(runningServers.length)
  // Сервери друзів, до яких мені дали доступ, — у бібліотеці як свої: рахуються у вкладках
  // «Усі / Онлайн / Офлайн» і не дають показати «серверів ще немає».
  const remoteOn = (r: RemoteServer) => r.ownerOnline && ONLINE_STATUSES.has(r.state)
  const totalCount = $derived(servers.length + rs.servers.length)
  const onlineCount = $derived(servers.filter(s => ONLINE_STATUSES.has(s.status)).length + rs.servers.filter(remoteOn).length)
  const offlineCount = $derived(totalCount - onlineCount)
  const busyCount = $derived(servers.filter(s => ['running', 'starting', 'stopping', 'installing'].includes(s.status)).length)

  // ── Живі показники ──────────────────────────────────────────────────
  const HIST = 30                       // точок історії (30 × 3 с ≈ 90 с)
  const POLL_MS = 3000
  let statsById = $state<Record<string, ServerLiveStats>>({})
  let pingById = $state<Record<string, number>>({})      // id → RTT мс (-1 — не відповів)
  let netStats = $state<{ rtt: number; jitter: number; loss: number } | null>(null)
  let cpuHist = $state<number[]>([])
  let ramHist = $state<number[]>([])
  let hostRamMB = $state(0)
  // Скільки підряд невдалих пінгів по кожному серверу (один збій — це ще не
  // «не відповідає»: порт міг бути зайнятий збиранням сміття).
  const pingFails = new Map<string, number>()
  const rttRing = new Map<string, number[]>()
  let analyticsBusy = false

  async function refreshAnalytics() {
    if (analyticsBusy) return
    const running = servers.filter(s => s.status === 'running')
    if (running.length === 0) {
      statsById = {}; pingById = {}; netStats = null; cpuHist = []; ramHist = []
      rttRing.clear(); pingFails.clear()
      return
    }
    analyticsBusy = true
    try {
      const results = await Promise.all(running.map(async (s) => {
        const stats = await App.GetServerStats(s.id).catch(() => null as ServerLiveStats | null)
        // Час відповіді status-ping — «стабільність передачі пакетів»:
        // локальний сервер відповідає за одиниці мс; втрата відповідей або
        // стрибки RTT = нестабільний канал/перевантажений сервер.
        // PingServer НЕ кидає виняток, коли сервер мовчить — він повертає
        // {online:false}. Без перевірки online «відмову в з'єднанні» (яка
        // приходить за 1 мс) було б пораховано як ідеальний пінг, і ні втрата
        // відповідей, ні «сервер не відповідає» ніколи б не спрацювали.
        let rtt = -1
        const t0 = performance.now()
        try {
          const res = await App.PingServer(`127.0.0.1:${s.port || 25565}`)
          rtt = res?.online ? Math.round(performance.now() - t0) : -1
        } catch { rtt = -1 }
        return { s, stats, rtt }
      }))

      const nextStats: Record<string, ServerLiveStats> = {}
      const nextPing: Record<string, number> = {}
      let cpu = 0, ram = 0, loss = 0
      for (const { s, stats, rtt } of results) {
        if (stats) {
          nextStats[s.id] = stats
          cpu += stats.cpuPercent || 0
          // Купа JVM, а не RSS (див. шапку файлу). Поки GC-лог порожній — RSS.
          ram += stats.heapSource === 'jvm' ? (stats.heapUsedMB || 0) : (stats.ramMB || 0)
        }
        nextPing[s.id] = rtt
        if (rtt >= 0) {
          pingFails.set(s.id, 0)
          const ring = rttRing.get(s.id) ?? []
          ring.push(rtt); if (ring.length > 8) ring.shift()
          rttRing.set(s.id, ring)
        } else {
          loss++
          pingFails.set(s.id, (pingFails.get(s.id) ?? 0) + 1)
        }
      }
      statsById = nextStats
      pingById = nextPing
      cpuHist = [...cpuHist, cpu].slice(-HIST)
      ramHist = [...ramHist, ram].slice(-HIST)

      // Середній RTT + джитер (розкид) + частка серверів без відповіді.
      const all: number[] = []
      for (const ring of rttRing.values()) all.push(...ring)
      if (all.length > 0) {
        const avg = all.reduce((a, b) => a + b, 0) / all.length
        const jitter = Math.sqrt(all.reduce((a, b) => a + (b - avg) ** 2, 0) / all.length)
        netStats = { rtt: Math.round(avg), jitter: Math.round(jitter), loss: Math.round((loss / results.length) * 100) }
      } else {
        netStats = { rtt: 0, jitter: 0, loss: 100 }
      }
    } finally {
      analyticsBusy = false
    }
  }

  // Набір запущених серверів змінився → одразу міряємо (а не чекаємо таймер).
  const runningKey = $derived(runningServers.map(s => s.id).sort().join(','))
  let lastRunningKey = '\u0000'
  $effect(() => {
    const key = runningKey
    if (key === lastRunningKey) return
    lastRunningKey = key
    // untrack: refreshAnalytics синхронно читає servers/статистику — без цього
    // вони ставали б залежностями ефекту.
    untrack(() => { void refreshAnalytics() })
  })

  // Системна пам'ять — для оцінки перевиділення (один раз).
  $effect(() => {
    App.GetSystemInfo().then((si: SystemInfo) => { hostRamMB = si?.totalRAMMB || 0 }).catch(() => {})
  })

  // ── Підписка на живі події ──
  $effect(() => {
    const offStatus = on('server:status', (data: any) => {
      const srv = servers.find(s => s.id === data.serverId)
      if (!srv) return
      const prev = srv.status
      srv.status = data.status || 'stopped'
      srv.pid = data.pid || 0
      srv.uptimeSec = data.uptimeSec || 0
      srv.exitCode = data.exitCode || 0
      srv.lastError = data.lastError || ''
      if (data.status !== 'installing') delete installProgress[data.serverId]
      // Ядро могло щойно довантажитись/змінитись loaderVer — після переходу
      // в stopped тихо перечитаємо список (без спінера).
      if (prev !== 'stopped' && data.status === 'stopped') {
        setTimeout(() => loadServers(true), 400)
      }
    })
    const offInstall = on('server:install-progress', (data: any) => {
      if (!servers.find(s => s.id === data.serverId)) return
      installProgress[data.serverId] = {
        stage: data.stage || 'core',
        percent: data.percent ?? -1,
        message: data.message || '',
      }
    })
    return () => { offStatus?.(); offInstall?.() }
  })

  // Поки є «живі» сервери — тихо перечитуємо список (uptime/статуси);
  // поки є ЗАПУЩЕНІ — опитуємо навантаження. Таймери окремі: стартуючий
  // сервер ще не має статистики, а запущений не потребує частого списку.
  $effect(() => {
    if (busyCount === 0) return
    const id = setInterval(() => loadServers(true), 2000)
    return () => clearInterval(id)
  })
  $effect(() => {
    if (runningCount === 0) return
    const id = setInterval(() => { void refreshAnalytics() }, POLL_MS)
    return () => clearInterval(id)
  })

  $effect(() => { loadServers() })

  // ── Фільтри + сортування ──
  const statusRank: Record<string, number> = { running: 0, starting: 1, stopping: 2, installing: 3, crashed: 4, error: 5, stopped: 6 }
  const filtered = $derived.by(() => {
    let result = servers
    if (search) {
      const q = search.toLowerCase()
      result = result.filter(s => s.name.toLowerCase().includes(q) || s.dir.toLowerCase().includes(q))
    }
    if (filterCore !== 'all') result = result.filter(s => s.core === filterCore)
    if (filterStatus === 'online') result = result.filter(s => ONLINE_STATUSES.has(s.status))
    else if (filterStatus === 'offline') result = result.filter(s => !ONLINE_STATUSES.has(s.status))
    return sortServers(result)
  })

  // Сервери друзів фільтруються тими ж пошуком/ядром/станом, що й власні.
  const remoteVisible = $derived.by(() => {
    let result = rs.servers
    if (search) {
      const q = search.toLowerCase()
      result = result.filter(r => r.name.toLowerCase().includes(q) || (rs.nicks[r.ownerId] ?? '').toLowerCase().includes(q))
    }
    if (filterCore !== 'all') result = result.filter(r => r.core === filterCore)
    if (filterStatus === 'online') result = result.filter(remoteOn)
    else if (filterStatus === 'offline') result = result.filter(r => !remoteOn(r))
    return [...result].sort((a, b) => (statusRank[a.state] ?? 9) - (statusRank[b.state] ?? 9) || a.name.localeCompare(b.name))
  })
  $effect(() => startRemote())

  function sortServers(list: ServerView[]): ServerView[] {
    const arr = [...list]
    switch (sortBy) {
      case 'name':
        arr.sort((a, b) => a.name.localeCompare(b.name))
        break
      case 'core':
        arr.sort((a, b) => (a.coreLabel || a.core).localeCompare(b.coreLabel || b.core) || a.name.localeCompare(b.name))
        break
      case 'version':
        arr.sort((a, b) => String(a.mcVersion || '').localeCompare(String(b.mcVersion || ''), undefined, { numeric: true }) || a.name.localeCompare(b.name))
        break
      case 'port':
        arr.sort((a, b) => (a.port || 0) - (b.port || 0) || a.name.localeCompare(b.name))
        break
      case 'status':
      default:
        arr.sort((a, b) => (statusRank[a.status] ?? 9) - (statusRank[b.status] ?? 9) || a.name.localeCompare(b.name))
        break
    }
    return arr
  }

  const cores = $derived([...new Set(servers.map(s => s.core))].sort())
  const coreOptions = $derived([
    { value: 'all', label: t('servers.lib.allCores', { n: servers.length }) },
    ...cores.map(c => ({ value: c, label: `${CORE_META[c]?.label ?? c} (${servers.filter(s => s.core === c).length})` })),
  ])
  const sortOptions = $derived([
    { value: 'status', label: t('servers.lib.sort.status'), icon: 'ti-player-play' },
    { value: 'name', label: t('servers.lib.sort.name'), icon: 'ti-sort-ascending' },
    { value: 'core', label: t('servers.lib.sort.core'), icon: 'ti-server-2' },
    { value: 'version', label: t('servers.lib.sort.version'), icon: 'ti-versions' },
    { value: 'port', label: t('servers.lib.sort.port'), icon: 'ti-plug' },
  ])

  // ── Підсумки для панелі ──
  const totalCpu = $derived(cpuHist.length ? cpuHist[cpuHist.length - 1] : 0)
  const totalRamUsed = $derived(ramHist.length ? ramHist[ramHist.length - 1] : 0)
  const allocRam = $derived(runningServers.reduce((n, s) => n + (s.ramMB || 0), 0))
  const playersNow = $derived(Object.values(statsById).reduce((n, s) => n + (s.playerCount || 0), 0))
  const playersMax = $derived(Object.values(statsById).reduce((n, s) => n + (s.maxPlayers || 0), 0))
  const gb = (mb: number) => (mb / 1024).toFixed(mb >= 10240 ? 0 : 1)
  const netQuality = $derived<'ok' | 'warn' | 'bad' | null>(
    !netStats ? null : (netStats.loss >= 34 || netStats.rtt >= 400) ? 'bad'
      : (netStats.loss > 0 || netStats.jitter > 60 || netStats.rtt >= 150) ? 'warn' : 'ok'
  )

  // Міні-графік (SVG-полілінія) для CPU/RAM: значення → точки 0..100×0..28.
  function sparkPath(values: number[], max: number): string {
    if (values.length < 2) return ''
    const top = Math.max(max, ...values, 1)
    const step = 100 / (HIST - 1)
    const off = (HIST - values.length) * step
    return values.map((v, i) => `${i === 0 ? 'M' : 'L'}${(off + i * step).toFixed(1)},${(27 - (v / top) * 25).toFixed(1)}`).join(' ')
  }
  const cpuSpark = $derived(sparkPath(cpuHist, 100))
  const ramSpark = $derived(sparkPath(ramHist, allocRam))

  // ── Аналіз усієї бібліотеки ─────────────────────────────────────────
  type Insight = { id: string; level: 'error' | 'warn' | 'info'; serverId: string; icon: string; text: string; detail?: string }
  const insights = $derived.by<Insight[]>(() => {
    const out: Insight[] = []
    const byPort = new Map<number, ServerView[]>()
    for (const s of servers) {
      if (s.port) byPort.set(s.port, [...(byPort.get(s.port) ?? []), s])
    }

    for (const s of servers) {
      const st = statsById[s.id]
      if (s.status === 'crashed' || s.status === 'error') {
        out.push({ id: `fail:${s.id}`, level: 'error', serverId: s.id, icon: 'ti-alert-octagon',
          text: t(s.status === 'crashed' ? 'servers.lib.ins.crashed' : 'servers.lib.ins.error', { name: s.name }), detail: s.lastError || undefined })
      }
      if (s.javaInstalled === false) {
        out.push({ id: `java:${s.id}`, level: 'warn', serverId: s.id, icon: 'ti-coffee',
          text: t('servers.lib.ins.javaMissing', { name: s.name, v: s.javaMajor }) })
      }
      if (!s.coreInstalled && s.status === 'stopped') {
        out.push({ id: `core:${s.id}`, level: 'info', serverId: s.id, icon: 'ti-download',
          text: t('servers.lib.ins.coreMissing', { name: s.name }) })
      }
      if (s.status === 'running' && st) {
        if (st.heapSource === 'jvm' && s.ramMB > 0) {
          const p = Math.round((st.heapUsedMB / s.ramMB) * 100)
          if (p >= 90) out.push({ id: `heap:${s.id}`, level: 'warn', serverId: s.id, icon: 'ti-device-sd-card', text: t('servers.lib.ins.heapHigh', { name: s.name, p }) })
        }
        if ((st.cpuPercent || 0) > 85) {
          out.push({ id: `cpu:${s.id}`, level: 'warn', serverId: s.id, icon: 'ti-cpu', text: t('servers.lib.ins.cpuHigh', { name: s.name, p: Math.round(st.cpuPercent) }) })
        }
        if ((st.gcLastPauseMs || 0) >= 1000) {
          out.push({ id: `gc:${s.id}`, level: 'warn', serverId: s.id, icon: 'ti-recycle', text: t('servers.lib.ins.gcPause', { name: s.name, ms: Math.round(st.gcLastPauseMs) }) })
        }
        if ((pingFails.get(s.id) ?? 0) >= 2 && (st.uptimeSec || s.uptimeSec || 0) > 60) {
          out.push({ id: `ping:${s.id}`, level: 'error', serverId: s.id, icon: 'ti-plug-x', text: t('servers.lib.ins.unreachable', { name: s.name, port: s.port || 25565 }) })
        }
      }
    }

    // Конфлікт портів: два сервери на одному порту. Якщо обидва ЗАПУЩЕНІ —
    // це вже помилка (другий не стартує), якщо ні — лише застереження.
    for (const [port, list] of byPort) {
      if (list.length < 2) continue
      const live = list.filter(s => ONLINE_STATUSES.has(s.status)).length > 1
      out.push({ id: `port:${port}`, level: live ? 'error' : 'warn', serverId: list[0].id, icon: 'ti-plug-connected-x',
        text: t(live ? 'servers.lib.ins.portConflictLive' : 'servers.lib.ins.portConflict', { port, names: list.map(s => s.name).join(', ') }) })
    }

    // Перевиділення пам'яті: сума -Xmx запущених серверів проти системної RAM.
    if (hostRamMB > 0 && allocRam > 0) {
      const ratio = allocRam / hostRamMB
      if (ratio >= 0.85) {
        out.push({ id: 'ramOver', level: ratio >= 1 ? 'error' : 'warn', serverId: runningServers[0]?.id ?? '', icon: 'ti-chart-pie',
          text: t('servers.lib.ins.ramOver', { a: gb(allocRam), h: gb(hostRamMB) }) })
      }
    }

    // Світи без автобекапу — одним зауваженням, а не по рядку на сервер.
    const noBackup = servers.filter(s => s.hasWorlds && !s.autoBackup?.enabled)
    if (noBackup.length > 0) {
      out.push({ id: 'backup', level: 'info', serverId: noBackup[0].id, icon: 'ti-database-off',
        text: t('servers.lib.ins.noBackup', { names: noBackup.slice(0, 3).map(s => s.name).join(', ') + (noBackup.length > 3 ? ` +${noBackup.length - 3}` : '') }) })
    }

    const rank = { error: 0, warn: 1, info: 2 } as const
    return out.sort((a, b) => rank[a.level] - rank[b.level])
  })
  const errCount = $derived(insights.filter(i => i.level === 'error').length)
  const warnCount = $derived(insights.filter(i => i.level === 'warn').length)
  const infoCount = $derived(insights.filter(i => i.level === 'info').length)
  let insightsOpen = $state(false)
  const INS_LIMIT = 4
  const shownInsights = $derived(insightsOpen ? insights : insights.slice(0, INS_LIMIT))

  // ── Дії ──
  // Старт ПРЯМО З БІБЛІОТЕКИ — без переходу на сторінку сервера (раніше тут
  // був onOpenServer(id, true): відкривалась сторінка з консоллю, яка і
  // стартувала сервер). Той самий шлях, що в консолі/банері сторінки сервера:
  // спершу розумний аналізатор (порт/RAM/Java/EULA/диск — модалка лише якщо
  // є що показати), потім App.StartServer. Статус приїздить подією
  // server:status (див. підписку нижче).
  let pfResult = $state<ServerPreflight | null>(null)
  let pfServerName = $state('')
  let pfResolve: ((ok: boolean) => void) | null = null
  // Захист від подвійного кліку, поки App.StartServer ще не відповів.
  const startingIds = new Set<string>()

  async function runPreflight(id: string, name: string): Promise<boolean> {
    let res: ServerPreflight | null = null
    try {
      res = await App.GetServerLaunchWarnings(id)
    } catch {
      // Аналізатор недоступний — не блокуємо запуск (як на сторінці сервера).
      return true
    }
    if (!res || !res.warnings || res.warnings.length === 0) return true
    pfServerName = name
    pfResult = res
    return new Promise<boolean>((resolve) => { pfResolve = resolve })
  }
  function pfProceed() { pfResolve?.(true); pfResolve = null; pfResult = null }
  function pfCancel() { pfResolve?.(false); pfResolve = null; pfResult = null }

  async function startServer(id: string) {
    const srv = servers.find(s => s.id === id)
    if (!srv) return
    if (['running', 'starting', 'stopping', 'installing'].includes(srv.status)) return
    if (startingIds.has(id) || pfResult) return
    startingIds.add(id)
    try {
      if (!(await runPreflight(id, srv.name))) return
      await App.StartServer(id)
      setTimeout(() => loadServers(true), 500)
    } catch (e: any) {
      toast(String(e?.message ?? e), 'error')
    } finally {
      startingIds.delete(id)
    }
  }

  async function stopServer(id: string) {
    try {
      await App.StopServer(id, true)
      toast(t('servers.page.stopping'), 'info')
      setTimeout(() => loadServers(true), 500)
    } catch (e: any) {
      toast(String(e), 'error')
    }
  }

  function openFolder(id: string) {
    const srv = servers.find(s => s.id === id)
    if (srv) App.OpenFolder(srv.dir).catch(() => {})
  }

  function openConsole(id: string) {
    App.ToggleServerConsoleWindow(id).catch((e: any) => toast(String(e?.message ?? e), 'error'))
  }

  // ── Видалення ──
  let deleteModal = $state<{ open: boolean; id: string; name: string }>({ open: false, id: '', name: '' })
  function askDeleteServer(id: string, name: string) {
    deleteModal = { open: true, id, name }
  }
  async function confirmDeleteServer(deleteFiles: boolean) {
    const { id } = deleteModal
    deleteModal = { open: false, id: '', name: '' }
    try {
      if (deleteFiles) {
        await App.DeleteServerWithFiles(id)
        toast(t('servers.lib.deletedFiles'), 'success')
      } else {
        await App.DeleteServer(id)
        toast(t('servers.lib.deletedKeep'), 'success')
      }
      loadServers()
    } catch (e: any) {
      toast(String(e), 'error')
    }
  }

  // ── ПАПКИ СЕРВЕРІВ ──────────────────────────────────────────────────────
  // Як папки збірок: сервер — максимум в одній папці; сервери з папок не
  // показуються в головному списку, а лише всередині папки. Зберігається
  // в .build-layout.json (App.GetServerFolders/SaveServerFolders).
  let folders = $state<ServerFolder[]>([])
  let openFolderId = $state<string | null>(null)
  let folderEditor = $state<{ id: string; name: string; color: string; icon: string; mode: 'create' | 'edit'; selected: Set<string> } | null>(null)

  async function loadFolders() {
    try { folders = (await App.GetServerFolders()) ?? [] } catch (e) { console.error('GetServerFolders:', e) }
  }
  $effect(() => { untrack(() => { void loadFolders() }) })

  function persistFolders(next: ServerFolder[]) {
    folders = next
    App.SaveServerFolders(next.map(f => ({ ...f, serverIds: [...f.serverIds] }))).catch((e: any) => {
      console.error('SaveServerFolders:', e)
      toast(String(e?.message ?? e), 'error')
    })
  }

  const serverById = $derived(new Map(servers.map(s => [s.id, s])))
  // Лише існуючі сервери (видалений сервер просто зникає з папки).
  const folderViews = $derived(folders.map(f => ({
    folder: f,
    servers: sortServers(f.serverIds.map(id => serverById.get(id)).filter((s): s is ServerView => !!s)),
  })))
  const inFolder = $derived(new Set(folderViews.flatMap(fv => fv.servers.map(s => s.id))))
  const folderOf = (id: string) => folders.find(f => f.serverIds.includes(id)) ?? null
  // Папки видно лише без активних фільтрів/пошуку — інакше плоский список.
  const filtersActive = $derived(search !== '' || filterCore !== 'all' || filterStatus !== 'all')
  // Вкладка «Онлайн» порожня сама по собі (жодного запущеного) — не «нічого не знайдено».
  const noneOnline = $derived(totalCount > 0 && filterStatus === 'online' && !search && filterCore === 'all')
  const looseServers = $derived(filtered.filter(s => !inFolder.has(s.id)))
  const openView = $derived(folderViews.find(fv => fv.folder.id === openFolderId) ?? null)

  function moveToFolder(srvId: string, folderId: string) {
    const target = folders.find(f => f.id === folderId)
    if (!target || target.serverIds.includes(srvId)) return
    persistFolders(folders.map(f => f.id === folderId
      ? { ...f, serverIds: [...f.serverIds, srvId] }
      : { ...f, serverIds: f.serverIds.filter(x => x !== srvId) }))
    toast(t('packs.folderAdded', { name: target.name }), 'success')
  }
  function takeOutOfFolder(srvId: string) {
    if (!folderOf(srvId)) return
    persistFolders(folders.map(f => ({ ...f, serverIds: f.serverIds.filter(x => x !== srvId) })))
    toast(t('packs.folderRemoved'), 'success')
  }
  function deleteFolder(id: string) {
    persistFolders(folders.filter(f => f.id !== id))
    if (openFolderId === id) openFolderId = null
    toast(t('folder.servers.deleted'), 'success')
  }

  function openCreateFolder(preselect: string[] = []) {
    folderEditor = { id: '', name: t('packs.folderNewName'), color: '#ff8a00', icon: 'ti-folder', mode: 'create', selected: new Set(preselect) }
  }
  function openEditFolder(id: string) {
    const f = folders.find(x => x.id === id)
    if (!f) return
    folderEditor = { id, name: f.name, color: f.color || '#ff8a00', icon: f.icon || 'ti-folder', mode: 'edit', selected: new Set(f.serverIds) }
  }
  function saveFolderEditor() {
    const fe = folderEditor
    if (!fe || !fe.name.trim()) return
    const name = fe.name.trim()
    const icon = fe.icon && fe.icon !== 'ti-folder' ? fe.icon : undefined
    const picked = [...fe.selected].filter(id => serverById.has(id))
    // Сервер — лише в одній папці: забираємо вибрані з інших.
    const stripped = folders.map(f => ({ ...f, serverIds: f.serverIds.filter(id => !picked.includes(id) || f.id === fe.id) }))
    if (fe.mode === 'create') {
      const id = 'sfld_' + Math.random().toString(36).slice(2, 10)
      persistFolders([...stripped, { id, name, color: fe.color, icon, serverIds: picked }])
      toast(t('folder.servers.created'), 'success')
    } else {
      persistFolders(stripped.map(f => f.id === fe.id
        // Редагування: склад задає вибір (додати/прибрати), порядок зберігаємо.
        ? { ...f, name, color: fe.color, icon, serverIds: [...f.serverIds.filter(id => picked.includes(id)), ...picked.filter(id => !f.serverIds.includes(id))] }
        : f))
    }
    folderEditor = null
  }
  // Кандидати в папку: сервери без папки + ті, що вже в цій (при редагуванні).
  const editorCandidates = $derived.by(() => {
    const fe = folderEditor
    if (!fe) return []
    return sortServers(servers.filter(s => {
      const f = folderOf(s.id)
      return !f || (fe.mode === 'edit' && f.id === fe.id)
    }))
  })
  function toggleEditorServer(id: string) {
    const fe = folderEditor
    if (!fe) return
    const next = new Set(fe.selected)
    if (next.has(id)) next.delete(id); else next.add(id)
    folderEditor = { ...fe, selected: next }
  }

  // ── Drag-and-drop серверів (pointer-based, як у збірках) ─────────────────
  // HTML5 draggable у WebView2 ненадійний (dragstart не стартує з вкладених
  // кнопок/іконок картки) — тому, як і для збірок, власне перетягування на
  // pointerdown/move/up з порогом 6px: клік відкриває сервер, рух — тягне.
  //   • картка → плитка папки: покласти (або перенести з іншої папки);
  //   • картка з відкритої папки за межі вікна папки: витягнути з папки
  //     (вікно ховається, щойно курсор його покинув — плитки за ним доступні);
  //   • Esc — скасувати. Дотик (touch) не перехоплюємо, щоб не ламати скрол.
  let dragSrv = $state<{ id: string; fromFolder: boolean; x: number; y: number } | null>(null)
  let dropFolderId = $state<string | null>(null)
  let scrollEl = $state<HTMLElement | null>(null)
  let pendingDrag: { id: string; x: number; y: number; fromFolder: boolean } | null = null
  let autoScrollSpeed = 0
  let autoScrollRaf = 0

  function srvPointerDown(e: PointerEvent, id: string, fromFolder: boolean) {
    if (e.button !== 0 || e.pointerType === 'touch') return
    pendingDrag = { id, x: e.clientX, y: e.clientY, fromFolder }
    window.addEventListener('pointermove', onDragMove)
    window.addEventListener('pointerup', onDragUp)
    window.addEventListener('pointercancel', endDrag)
    window.addEventListener('keydown', onDragKey, true)
  }

  function dragAutoScroll(y: number) {
    const r = scrollEl?.getBoundingClientRect()
    if (!r) { autoScrollSpeed = 0; return }
    const edge = 70
    autoScrollSpeed = y < r.top + edge ? -Math.ceil((r.top + edge - y) / 6)
      : y > r.bottom - edge ? Math.ceil((y - (r.bottom - edge)) / 6) : 0
    if (autoScrollSpeed && !autoScrollRaf) {
      const tick = () => {
        if (!dragSrv || !autoScrollSpeed) { autoScrollRaf = 0; return }
        scrollEl?.scrollBy(0, autoScrollSpeed)
        autoScrollRaf = requestAnimationFrame(tick)
      }
      autoScrollRaf = requestAnimationFrame(tick)
    }
  }

  function onDragMove(e: PointerEvent) {
    if (!pendingDrag) return
    if (!dragSrv) {
      const dx = e.clientX - pendingDrag.x, dy = e.clientY - pendingDrag.y
      if (dx * dx + dy * dy < 36) return
      dragSrv = { id: pendingDrag.id, fromFolder: pendingDrag.fromFolder, x: e.clientX, y: e.clientY }
      document.body.classList.add('sl-dragging')
      window.getSelection()?.removeAllRanges()
    } else {
      dragSrv = { ...dragSrv, x: e.clientX, y: e.clientY }
    }
    const el = document.elementFromPoint(e.clientX, e.clientY)
    // Курсор покинув вікно папки — ховаємо його, щоб плитки за ним стали цілями.
    if (dragSrv.fromFolder && openFolderId && !el?.closest('.folder-modal')) openFolderId = null
    const tid = (el?.closest('[data-srv-folder]') as HTMLElement | null)?.dataset.srvFolder ?? null
    dropFolderId = tid && folderOf(dragSrv.id)?.id !== tid ? tid : null
    dragAutoScroll(e.clientY)
  }

  function onDragUp(e: PointerEvent) {
    const d = dragSrv
    if (d) {
      const el = document.elementFromPoint(e.clientX, e.clientY)
      const tid = (el?.closest('[data-srv-folder]') as HTMLElement | null)?.dataset.srvFolder
      if (tid) { if (folderOf(d.id)?.id !== tid) moveToFolder(d.id, tid) }
      else if (d.fromFolder && !el?.closest('.folder-modal')) takeOutOfFolder(d.id)
      // Після перетягування браузер генерує click — гасимо його, щоб картка не відкрилась.
      const swallow = (ev: Event) => { ev.stopPropagation(); ev.preventDefault() }
      window.addEventListener('click', swallow, { capture: true, once: true })
      setTimeout(() => window.removeEventListener('click', swallow, true), 0)
    }
    endDrag()
  }

  function onDragKey(e: KeyboardEvent) {
    if (e.key === 'Escape' && dragSrv) { e.stopPropagation(); endDrag() }
  }

  function endDrag() {
    pendingDrag = null
    dragSrv = null
    dropFolderId = null
    autoScrollSpeed = 0
    cancelAnimationFrame(autoScrollRaf); autoScrollRaf = 0
    document.body.classList.remove('sl-dragging')
    window.removeEventListener('pointermove', onDragMove)
    window.removeEventListener('pointerup', onDragUp)
    window.removeEventListener('pointercancel', endDrag)
    window.removeEventListener('keydown', onDragKey, true)
  }
  $effect(() => () => endDrag())

  // ПКМ по плитці папки.
  let folderCtx = $state<{ id: string; x: number; y: number } | null>(null)
  function openFolderCtx(e: MouseEvent, id: string) {
    e.preventDefault()
    folderCtx = { id, x: Math.max(4, Math.min(e.clientX, window.innerWidth - 228)), y: Math.max(4, Math.min(e.clientY, window.innerHeight - 140)) }
  }
  $effect(() => {
    if (!folderCtx) return
    const close = () => { folderCtx = null }
    const timer = setTimeout(() => window.addEventListener('click', close), 0)
    window.addEventListener('blur', close)
    return () => { clearTimeout(timer); window.removeEventListener('click', close); window.removeEventListener('blur', close) }
  })

  // ── ПКМ-меню на картці сервера ──
  let ctxMenu = $state<{ id: string; x: number; y: number } | null>(null)
  const ctxSrv = $derived(ctxMenu ? servers.find(s => s.id === ctxMenu?.id) ?? null : null)

  // Реальні розміри меню (bind:clientHeight/Width): висота залежить від
  // кількості папок і стану сервера, тож захардкоджене H=250 не рахувало
  // нижні пункти — меню «провалювалось» під край екрана. Тепер позиція
  // клампиться по виміряному розміру, а дуже високе меню скролиться.
  let ctxH = $state(0)
  let ctxW = $state(0)
  const ctxLeft = $derived(ctxMenu ? Math.max(4, Math.min(ctxMenu.x, window.innerWidth - (ctxW || 220) - 8)) : 0)
  const ctxTop = $derived(ctxMenu ? Math.max(4, Math.min(ctxMenu.y, window.innerHeight - (ctxH || 250) - 8)) : 0)

  function openCtxMenu(e: MouseEvent, id: string) {
    e.preventDefault()
    ctxH = 0
    ctxW = 0
    ctxMenu = { id, x: e.clientX, y: e.clientY }
  }

  $effect(() => {
    if (!ctxMenu) return
    // Кнопка «⋯» на картці відкриває меню тим самим кліком, що й закриває
    // його глобальний слухач — тому закриваємо на наступному тіку.
    const close = () => { ctxMenu = null }
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') ctxMenu = null }
    const timer = setTimeout(() => window.addEventListener('click', close), 0)
    window.addEventListener('blur', close)
    window.addEventListener('keydown', onKey)
    return () => {
      clearTimeout(timer)
      window.removeEventListener('click', close)
      window.removeEventListener('blur', close)
      window.removeEventListener('keydown', onKey)
    }
  })

  function ctxAction(action: 'open' | 'start' | 'stop' | 'folder' | 'console' | 'delete', id: string) {
    ctxMenu = null
    const srv = servers.find(s => s.id === id)
    if (!srv) return
    if (action === 'open') onOpenServer(id)
    else if (action === 'start') startServer(id)
    else if (action === 'stop') stopServer(id)
    else if (action === 'folder') openFolder(id)
    else if (action === 'console') openConsole(id)
    else if (action === 'delete') askDeleteServer(id, srv.name)
  }
</script>

{#snippet remoteCard(r: RemoteServer)}
  <div class="sl-cardwrap" role="presentation">
    <ServerCard
      srv={remoteAsView(r)}
      remote={{ nick: rs.nicks[r.ownerId] ?? t('servers.remote.friend'), online: r.ownerOnline, canStart: r.canStart, canStop: r.canStop, canConsole: !!r.canConsole, busy: !!rs.pending[remoteKey(r)] }}
      onOpen={() => onOpenRemote(r.ownerId, r.id)}
      onStart={() => remoteAct(r, 'start')}
      onStop={() => remoteAct(r, 'stop')}
      onOpenConsole={() => onOpenRemote(r.ownerId, r.id)}
    />
  </div>
{/snippet}

{#snippet srvCard(srv: ServerView, fromFolder: boolean)}
  <div class="sl-cardwrap" class:dragging={dragSrv?.id === srv.id && !!dragSrv} data-srv-id={srv.id}
       onpointerdown={(e) => srvPointerDown(e, srv.id, fromFolder)} role="presentation">
    <ServerCard
      {srv}
      stats={statsById[srv.id] ?? null}
      installProgress={installProgress[srv.id] ?? null}
      onOpen={() => { openFolderId = null; onOpenServer(srv.id) }}
      onStart={() => startServer(srv.id)}
      onStop={() => stopServer(srv.id)}
      onOpenFolder={() => openFolder(srv.id)}
      onOpenConsole={() => openConsole(srv.id)}
      onContextMenu={(e) => openCtxMenu(e, srv.id)}
    />
  </div>
{/snippet}

<div class="sl">
  <!-- ═══ ДІЇ СТОРІНКИ ═══
       Назву розділу й іконку вже показує верхня панель — окремий заголовок
       «Сервери» під нею дублював її. Тут лише дії, як на сторінці збірок. -->
  <div class="page-actions sl-actions">
    <button class="btn btn-ghost" onclick={() => loadServers()} title={t('servers.lib.refresh')} aria-label={t('servers.lib.refresh')}><i class="ti ti-refresh"></i></button>
    <button class="btn btn-ghost" onclick={() => openCreateFolder()}><i class="ti ti-folder-plus"></i> {t('folder.servers.create')}</button>
    <button class="btn btn-primary" onclick={onCreateServer}><i class="ti ti-plus"></i> {t('servers.lib.create')}</button>
  </div>

  <div class="sl-scroll" bind:this={scrollEl}>
    <!-- ═══ ЖИВА АНАЛІТИКА ═══ -->
    {#if servers.length > 0}
      <section class="sl-stats">
        <div class="sls" title={t('servers.lib.stat.serversTip')}>
          <div class="sls-h"><i class="ti ti-server"></i> {t('servers.lib.stat.servers')}</div>
          <div class="sls-v"><b>{servers.length}</b>{#if runningCount > 0}<span class="ok">{t('servers.lib.stat.working', { n: runningCount })}</span>{/if}</div>
          <div class="sls-f">
            <div class="sls-seg">
              <span class="on" style="flex:{runningCount}"></span><span class="mid" style="flex:{Math.max(0, onlineCount - runningCount)}"></span><span style="flex:{offlineCount}"></span>
            </div>
          </div>
        </div>

        <div class="sls" title={t('servers.lib.stat.cpuTip')}>
          <div class="sls-h"><i class="ti ti-cpu"></i> {t('servers.lib.stat.cpu')}</div>
          <div class="sls-v"><b class:warn={totalCpu > 60} class:bad={totalCpu > 85}>{runningCount ? totalCpu.toFixed(totalCpu >= 10 ? 0 : 1) : '0'}%</b></div>
          <div class="sls-f"><svg viewBox="0 0 100 28" preserveAspectRatio="none" aria-hidden="true">{#if cpuSpark}<path d="{cpuSpark} L100,28 L0,28 Z" class="area" /><path d={cpuSpark} class="line" />{:else}<path d="M0,26 L100,26" class="line dim" />{/if}</svg></div>
        </div>

        <div class="sls" title={t('servers.lib.stat.ramTip')}>
          <div class="sls-h"><i class="ti ti-device-sd-card"></i> {t('servers.lib.stat.ram')}</div>
          <div class="sls-v">
            <b>{runningCount ? gb(totalRamUsed) : '0.0'}</b><span>{t('servers.lib.stat.ramOf', { a: runningCount ? gb(allocRam) : '0' })}</span>
          </div>
          <div class="sls-f"><svg viewBox="0 0 100 28" preserveAspectRatio="none" aria-hidden="true">{#if ramSpark}<path d="{ramSpark} L100,28 L0,28 Z" class="area" /><path d={ramSpark} class="line" />{:else}<path d="M0,26 L100,26" class="line dim" />{/if}</svg></div>
          {#if hostRamMB > 0 && runningCount > 0}<div class="sls-sub" class:bad={allocRam / hostRamMB >= 0.85}>{t('servers.lib.stat.hostRam', { a: gb(allocRam), h: gb(hostRamMB) })}</div>{/if}
        </div>

        <div class="sls" title={t('servers.lib.stat.playersTip')}>
          <div class="sls-h"><i class="ti ti-users"></i> {t('servers.lib.stat.players')}</div>
          <div class="sls-v"><b>{playersNow}</b><span>/ {playersMax}</span></div>
          <div class="sls-f"><div class="sls-bar"><div style="width:{playersMax ? Math.min(100, Math.round((playersNow / playersMax) * 100)) : 0}%"></div></div></div>
        </div>

        <div class="sls" title={t('servers.lib.stat.netTip')}>
          <div class="sls-h"><i class="ti ti-wifi"></i> {t('servers.lib.stat.network')}</div>
          <div class="sls-v">
            {#if !netStats}
              <b>—</b>
            {:else}
              <b class:warn={netQuality === 'warn'} class:bad={netQuality === 'bad'}>{t('servers.lib.stat.ms', { n: netStats.rtt })}</b>
              {#if netStats.loss > 0}<span class="bad">{t('servers.lib.stat.loss', { n: netStats.loss })}</span>
              {:else}<span class="ok">{t('servers.lib.stat.stable')}</span>{/if}
            {/if}
          </div>
          <div class="sls-f">
            {#if netStats}<div class="sls-sub">{t('servers.lib.stat.jitter', { n: netStats.jitter })}</div>{:else}<div class="sls-sub">{t('servers.lib.stat.netIdle')}</div>{/if}
          </div>
        </div>
      </section>

      <!-- ═══ АНАЛІЗ ═══ -->
      <section class="sl-ins" class:ok={insights.length === 0}>
        <div class="sl-ins-h">
          <div class="sl-ins-ic" class:err={errCount > 0} class:warn={errCount === 0 && warnCount > 0}><i class="ti ti-stethoscope"></i></div>
          <div class="sl-ins-t">
            <b>{t('servers.lib.ins.title')}</b>
            <span>{insights.length === 0 ? t('servers.lib.ins.ok') : t('servers.lib.ins.sub')}</span>
          </div>
          {#if insights.length > 0}
            <div class="sl-ins-counts">
              {#if errCount > 0}<span class="c err"><i class="ti ti-circle-x"></i>{errCount}</span>{/if}
              {#if warnCount > 0}<span class="c warn"><i class="ti ti-alert-triangle"></i>{warnCount}</span>{/if}
              {#if infoCount > 0}<span class="c info"><i class="ti ti-info-circle"></i>{infoCount}</span>{/if}
            </div>
          {:else}
            <i class="ti ti-circle-check sl-ins-okic"></i>
          {/if}
        </div>
        {#if insights.length > 0}
          <div class="sl-ins-list">
            {#each shownInsights as ins (ins.id)}
              <button class="sl-in {ins.level}" onclick={() => ins.serverId && onOpenServer(ins.serverId)} disabled={!ins.serverId}>
                <i class="ti {ins.icon}"></i>
                <span class="sl-in-t">{ins.text}{#if ins.detail}<em>{ins.detail}</em>{/if}</span>
                {#if ins.serverId}<i class="ti ti-chevron-right sl-in-go"></i>{/if}
              </button>
            {/each}
          </div>
          {#if insights.length > INS_LIMIT}
            <button class="sl-ins-more" onclick={() => insightsOpen = !insightsOpen}>
              <i class="ti {insightsOpen ? 'ti-chevron-up' : 'ti-chevron-down'}"></i>
              {insightsOpen ? t('servers.lib.ins.less') : t('servers.lib.ins.more', { n: insights.length - INS_LIMIT })}
            </button>
          {/if}
        {/if}
      </section>
    {/if}

    <!-- ═══ ФІЛЬТРИ: таби як у збірках + пошук і вибори в одному рядку ═══ -->
    <div class="sl-bar">
      <div class="tabbar sl-tabs" role="group" aria-label={t('servers.lib.filter.aria')}>
        <button class="tab-btn" class:active={filterStatus === 'all'} onclick={() => filterStatus = 'all'}>
          <i class="ti ti-server-2"></i> {t('servers.lib.filter.all')} <span class="num">{totalCount}</span>
        </button>
        <button class="tab-btn" class:active={filterStatus === 'online'} onclick={() => filterStatus = 'online'}>
          <i class="ti ti-player-play"></i> {t('servers.lib.filter.online')} <span class="num">{onlineCount}</span>
        </button>
        <button class="tab-btn" class:active={filterStatus === 'offline'} onclick={() => filterStatus = 'offline'}>
          <i class="ti ti-player-stop"></i> {t('servers.lib.filter.offline')} <span class="num">{offlineCount}</span>
        </button>
      </div>
      <div class="sl-tools">
        <label class="sl-search">
          <i class="ti ti-search"></i>
          <input placeholder={t('servers.lib.search')} bind:value={search} />
          {#if search}
            <button class="sl-x" onclick={() => search = ''} title={t('servers.lib.clear')} aria-label={t('servers.lib.clear')}><i class="ti ti-x"></i></button>
          {/if}
        </label>
        <SelectField value={filterCore} options={coreOptions} onchange={(v) => filterCore = v} ariaLabel={t('servers.lib.coreAria')} minWidth="160px" />
        <SelectField value={sortBy} options={sortOptions} onchange={(v) => sortBy = v as typeof sortBy} ariaLabel={t('servers.lib.sortAria')} minWidth="190px" />
        {#if filtersActive}
          <button class="sl-reset" onclick={() => { search = ''; filterCore = 'all'; filterStatus = 'all' }}>
            <i class="ti ti-filter-off"></i> {t('servers.lib.resetFilters')}
          </button>
        {/if}
      </div>
    </div>

    <!-- ═══ СІТКА ═══ -->
    {#if loading}
      <div class="sl-loading"><i class="ti ti-loader spin"></i> {t('servers.lib.loading')}</div>
    {:else if filtered.length === 0 && remoteVisible.length === 0}
      <div class="sl-empty">
        <div class="sl-empty-ic"><i class="ti ti-server-2"></i></div>
        <div class="sl-empty-t">{totalCount === 0 ? t('servers.lib.empty') : noneOnline ? t('servers.lib.noneOnline') : t('servers.lib.nothing')}</div>
        {#if totalCount === 0}
          <div class="sl-empty-s">{t('servers.lib.emptySub')}</div>
          <button class="btn btn-primary" onclick={onCreateServer}><i class="ti ti-plus"></i> {t('servers.lib.create')}</button>
        {:else if filtersActive}
          <!-- Фільтр нічого не лишив — не порожнеча, а підказка і вихід. -->
          <button class="btn" onclick={() => { search = ''; filterCore = 'all'; filterStatus = 'all' }}>
            <i class="ti ti-filter-off"></i> {noneOnline ? t('servers.lib.showAll') : t('servers.lib.resetFilters')}
          </button>
        {/if}
      </div>
    {:else}
      {#if !filtersActive && folderViews.length > 0}
        <div class="fm-grid sl-folders">
          {#each folderViews as fv (fv.folder.id)}
            {@const f = fv.folder}
            {@const col = f.color || '#ff8a00'}
            {@const live = fv.servers.filter(s => ONLINE_STATUSES.has(s.status)).length}
            <FolderTile class="rise-in {dropFolderId === f.id ? 'folder-drop' : ''}" data-srv-folder={f.id}
                        name={f.name} color={col} icon={f.icon} total={fv.servers.length}
                        icons={fv.servers.slice(0, 3).map(s => { const ic = serverIcon(s); return { src: ic.src, pcol: ic.color || col, preset: ic.ti } })}
                        role="button" tabindex="0"
                        onclick={() => (openFolderId = f.id)}
                        onkeydown={(e: KeyboardEvent) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); openFolderId = f.id } }}
                        oncontextmenu={(e: MouseEvent) => openFolderCtx(e, f.id)}>
              {#snippet sub()}
                {fv.servers.length} {t('folder.servers.count')}{#if live > 0}<span class="ft-live"> · <i class="ti ti-circle-filled"></i> {live}</span>{/if}
              {/snippet}
            </FolderTile>
          {/each}
        </div>
      {/if}
      <!-- Сервери друзів — у тій самій сітці, що й власні: друг бачить їх як свої картки. -->
      <div class="sl-grid">
        {#each (filtersActive ? filtered : looseServers) as srv (srv.id)}
          {@render srvCard(srv, false)}
        {/each}
        {#each remoteVisible as r (remoteKey(r))}
          {@render remoteCard(r)}
        {/each}
        {#if servers.length === 0 && !filtersActive}
          <!-- Власних серверів немає, але є сервери друзів: замість банера «серверів ще немає» —
               тиха плитка «створити свій» поруч із ними. -->
          <button class="sl-newtile" onclick={onCreateServer}>
            <span class="sl-newtile-ic"><i class="ti ti-plus"></i></span>
            <b>{t('servers.lib.createOwn')}</b>
            <span>{t('servers.lib.createOwnSub')}</span>
          </button>
        {/if}
      </div>
    {/if}
  </div>
</div>

{#if dragSrv}
  {@const gs = serverById.get(dragSrv.id)}
  {#if gs}
    {@const gi = serverIcon(gs)}
    <Portal>
      <div class="sl-ghost" style="transform:translate3d({dragSrv.x + 14}px,{dragSrv.y + 14}px,0)" aria-hidden="true">
        <span class="sl-ghost-ic" style="--mc:{gi.color || 'var(--orange)'}">{#if gi.src}<img src={gi.src} alt="" />{:else}<i class="ti {gi.ti}"></i>{/if}</span>
        <span class="sl-ghost-n">{gs.name}</span>
      </div>
    </Portal>
  {/if}
{/if}

{#if openView}
  {@const of = openView.folder}
  <FolderViewModal
    name={of.name}
    color={of.color || '#ff8a00'}
    icon={of.icon}
    count={openView.servers.length}
    countLabel={t('folder.servers.count')}
    emptyText={t('folder.servers.empty')}
    footHint={t('folder.servers.rightClickHint')}
    actions={[
      { icon: 'ti-edit', label: t('folder.edit'), onclick: () => openEditFolder(of.id) },
      { icon: 'ti-trash', label: t('folder.delete'), danger: true, onclick: () => deleteFolder(of.id) },
    ]}
    onclose={() => (openFolderId = null)}
  >
    <div class="sl-grid sl-grid-modal">
      {#each openView.servers as srv (srv.id)}
        {@render srvCard(srv, true)}
      {/each}
    </div>
  </FolderViewModal>
{/if}

{#if folderEditor}
  {@const fe = folderEditor}
  <FolderEditorModal
    mode={fe.mode}
    bind:name={fe.name}
    bind:color={fe.color}
    bind:icon={fe.icon}
    count={fe.selected.size}
    countLabel={t('folder.servers.count')}
    pickerLabel={t('folder.servers.addServers')}
    showPicker={true}
    onsave={saveFolderEditor}
    oncancel={() => (folderEditor = null)}
  >
    {#if editorCandidates.length === 0}
      <div class="sl-pick-empty">{t('folder.servers.noCandidates')}</div>
    {:else}
      <div class="sl-pick">
        {#each editorCandidates as s (s.id)}
          {@const ic = serverIcon(s)}
          {@const on = fe.selected.has(s.id)}
          <button class="sl-pick-row" class:on onclick={() => toggleEditorServer(s.id)} aria-pressed={on}>
            <span class="sl-pick-chk"><i class="ti ti-check"></i></span>
            <span class="sl-pick-ic" style="--mc:{ic.color || 'var(--orange)'}">
              {#if ic.src}<img src={ic.src} alt="" />{:else}<i class="ti {ic.ti}"></i>{/if}
            </span>
            <span class="sl-pick-n">{s.name}</span>
            <span class="sl-pick-s">{s.coreLabel || s.core} · {s.mcVersion}</span>
          </button>
        {/each}
      </div>
    {/if}
  </FolderEditorModal>
{/if}

<ServerDeleteModal
  open={deleteModal.open}
  name={deleteModal.name}
  onclose={() => deleteModal = { ...deleteModal, open: false }}
  onconfirm={confirmDeleteServer}
/>

<!-- Розумний аналізатор запуску (модалка перед стартом із бібліотеки) -->
{#if pfResult}
  <ServerPreflightModal
    result={pfResult}
    serverName={pfServerName}
    onstart={pfProceed}
    onclose={pfCancel}
  />
{/if}

{#if ctxMenu && ctxSrv}
  <div class="ctx-menu" bind:clientHeight={ctxH} bind:clientWidth={ctxW}
       style="left:{ctxLeft}px;top:{ctxTop}px;z-index:1000002;max-height:calc(100vh - 16px);overflow-y:auto;visibility:{ctxH ? 'visible' : 'hidden'}"
       oncontextmenu={(e) => e.preventDefault()}>
    <div class="ctx-item" onclick={() => ctxAction('open', ctxSrv.id)}>
      <i class="ti ti-arrow-right"></i> {t('servers.ctx.open')}
    </div>
    <div class="ctx-sep"></div>
    {#if ctxSrv.status === 'running'}
      <div class="ctx-item" onclick={() => ctxAction('stop', ctxSrv.id)}>
        <i class="ti ti-player-stop"></i> {t('servers.card.stop')}
      </div>
    {:else if !['starting', 'stopping', 'installing'].includes(ctxSrv.status)}
      <div class="ctx-item" onclick={() => ctxAction('start', ctxSrv.id)}>
        <i class="ti ti-player-play"></i> {t('servers.card.start')}
      </div>
    {/if}
    <div class="ctx-item" onclick={() => ctxAction('console', ctxSrv.id)}>
      <i class="ti ti-terminal-2"></i> {t('servers.toolbar.openConsole')}
    </div>
    <div class="ctx-item" onclick={() => ctxAction('folder', ctxSrv.id)}>
      <i class="ti ti-folder-open"></i> {t('servers.card.folder')}
    </div>
    <div class="ctx-sep"></div>
    {#if folderOf(ctxSrv.id)}
      <div class="ctx-item" onclick={() => { const id = ctxSrv.id; ctxMenu = null; takeOutOfFolder(id) }}>
        <i class="ti ti-folder-minus"></i> {t('folder.servers.takeOut')}
      </div>
    {/if}
    {#each folders.filter(f => !f.serverIds.includes(ctxSrv.id)) as f (f.id)}
      <div class="ctx-item" onclick={() => { const id = ctxSrv.id; ctxMenu = null; moveToFolder(id, f.id) }}>
        <i class="ti {safeFolderIcon(f.icon)}" style="color:{f.color || 'var(--orange)'}"></i> {t('folder.servers.moveTo')} «{f.name}»
      </div>
    {/each}
    <div class="ctx-item" onclick={() => { const id = ctxSrv.id; ctxMenu = null; openCreateFolder([id]) }}>
      <i class="ti ti-folder-plus"></i> {t('folder.servers.create')}
    </div>
    <div class="ctx-sep"></div>
    <div class="ctx-item danger" onclick={() => ctxAction('delete', ctxSrv.id)}>
      <i class="ti ti-trash"></i> {t('delete')}
    </div>
  </div>
{/if}

{#if folderCtx}
  <div class="ctx-menu" style="left:{folderCtx.x}px;top:{folderCtx.y}px;z-index:1000002" oncontextmenu={(e) => e.preventDefault()}>
    <div class="ctx-item" onclick={() => { const id = folderCtx!.id; folderCtx = null; openFolderId = id }}><i class="ti ti-folder-open"></i> {t('folder.open')}</div>
    <div class="ctx-item" onclick={() => { const id = folderCtx!.id; folderCtx = null; openEditFolder(id) }}><i class="ti ti-edit"></i> {t('folder.edit')}</div>
    <div class="ctx-sep"></div>
    <div class="ctx-item danger" onclick={() => { const id = folderCtx!.id; folderCtx = null; deleteFolder(id) }}><i class="ti ti-trash"></i> {t('folder.delete')}</div>
  </div>
{/if}

<style>
  .sl { display: flex; flex-direction: column; gap: 14px; padding: 0 8px; height: 100%; overflow: hidden; }
  .sl-scroll { flex: 1; min-height: 0; overflow-y: auto; display: flex; flex-direction: column; gap: 14px; padding: 2px 2px 24px; }

  /* ── Дії сторінки (як «Мої збірки») ── */
  .sl-actions { margin: 0; padding-top: 2px; }

  /* ── Аналітика ── */
  /* 148px — усі п'ять плиток лишаються в одному рядку вже з ~790px вмісту
     (ноутбук 1100–1280px із sidebar), а не 4+1, що з'їдало пів екрана. */
  .sl-stats { display: grid; grid-template-columns: repeat(auto-fit, minmax(148px, 1fr)); gap: 10px; }
  .sls {
    display: flex; flex-direction: column; gap: 6px; min-width: 0; padding: 12px 14px 10px;
    border-radius: var(--r-lg); background: var(--bg-card); border: 1px solid var(--border);
  }
  .sls-h { display: flex; align-items: center; gap: 6px; font: 700 var(--fs-min)/1 var(--font-ui); letter-spacing: .5px; text-transform: uppercase; color: var(--text-dim); }
  .sls-h i { font-size: 14px; color: var(--orange); }
  .sls-v { display: flex; align-items: baseline; gap: 6px; white-space: nowrap; overflow: hidden; }
  .sls-v b { font: 800 22px/1.05 var(--font-mono); letter-spacing: -.5px; color: var(--text); }
  .sls-v b.warn { color: var(--yellow); }
  .sls-v b.bad { color: var(--red); }
  .sls-v span { font: 600 11px/1 var(--font-mono); color: var(--text-dim); }
  .sls-v span.ok { color: var(--green); }
  .sls-v span.bad { color: var(--red); }
  .sls-f { height: 28px; display: flex; align-items: flex-end; }
  .sls-f svg { width: 100%; height: 28px; overflow: visible; }
  .sls-f .line { fill: none; stroke: var(--orange); stroke-width: 1.6; vector-effect: non-scaling-stroke; stroke-linejoin: round; }
  .sls-f .line.dim { stroke: var(--border); }
  .sls-f .area { fill: color-mix(in srgb, var(--orange) 16%, transparent); stroke: none; }
  .sls-sub { font: 600 var(--fs-min)/1.2 var(--font-mono); color: var(--text-dim); }
  .sls-sub.bad { color: var(--red); }
  .sls-bar { width: 100%; height: 6px; border-radius: 3px; background: var(--hover); overflow: hidden; }
  .sls-bar > div { height: 100%; border-radius: 3px; background: linear-gradient(90deg, #22c55e, #86efac); transition: width .45s; }
  .sls-seg { display: flex; width: 100%; height: 6px; border-radius: 3px; overflow: hidden; gap: 2px; background: transparent; }
  .sls-seg span { background: var(--hover); border-radius: 3px; transition: flex .4s; }
  .sls-seg span.on { background: #22c55e; }
  .sls-seg span.mid { background: #eab308; }

  /* ── Аналіз ── */
  .sl-ins { border-radius: var(--r-lg); background: var(--bg-card); border: 1px solid var(--border); overflow: hidden; }
  .sl-ins.ok { border-color: color-mix(in srgb, #22c55e 28%, var(--border)); }
  .sl-ins-h { display: flex; align-items: center; gap: 12px; padding: 12px 16px; }
  .sl-ins-ic { width: 34px; height: 34px; border-radius: 10px; display: grid; place-items: center; font-size: 18px; color: var(--green); background: rgba(34, 197, 94, .13); flex-shrink: 0; }
  .sl-ins-ic.warn { color: #eab308; background: rgba(234, 179, 8, .14); }
  .sl-ins-ic.err { color: var(--red); background: rgba(239, 68, 68, .14); }
  .sl-ins-t { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 3px; }
  .sl-ins-t b { font: 700 13px/1.2 var(--font-ui); color: var(--text); }
  .sl-ins-t span { font: 400 11.5px/1.3 var(--font-ui); color: var(--text-mute); }
  .sl-ins-okic { font-size: 22px; color: var(--green); }
  .sl-ins-counts { display: flex; gap: 6px; }
  .sl-ins-counts .c { display: inline-flex; align-items: center; gap: 4px; padding: 4px 9px; border-radius: 99px; font: 700 11px/1 var(--font-mono); border: 1px solid; }
  .sl-ins-counts .c.err { color: var(--red); background: rgba(239, 68, 68, .1); border-color: rgba(239, 68, 68, .35); }
  .sl-ins-counts .c.warn { color: #eab308; background: rgba(234, 179, 8, .1); border-color: rgba(234, 179, 8, .35); }
  .sl-ins-counts .c.info { color: var(--blue, #3b82f6); background: rgba(59, 130, 246, .1); border-color: rgba(59, 130, 246, .35); }
  .sl-ins-list { display: flex; flex-direction: column; border-top: 1px solid var(--border); }
  .sl-in {
    display: flex; align-items: center; gap: 11px; width: 100%; text-align: left; cursor: pointer;
    padding: 10px 16px; border: 0; border-bottom: 1px solid var(--hover); background: transparent; color: var(--text);
    font: 500 12.5px/1.4 var(--font-ui); transition: background .14s;
  }
  .sl-in:last-child { border-bottom: 0; }
  .sl-in:hover:not(:disabled) { background: var(--hover); }
  .sl-in:disabled { cursor: default; }
  .sl-in > i:first-child { font-size: 17px; flex-shrink: 0; }
  .sl-in.error > i:first-child { color: var(--red); }
  .sl-in.warn > i:first-child { color: #eab308; }
  .sl-in.info > i:first-child { color: var(--blue, #3b82f6); }
  .sl-in-t { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
  .sl-in-t em { font: 400 11px/1.35 var(--font-mono); font-style: normal; color: var(--text-dim); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .sl-in-go { font-size: 14px; color: var(--text-dim); }
  .sl-ins-more { width: 100%; padding: 9px; border: 0; border-top: 1px solid var(--border); background: transparent; color: var(--text-mute); font: 600 11.5px/1 var(--font-ui); cursor: pointer; display: flex; align-items: center; justify-content: center; gap: 6px; }
  .sl-ins-more:hover { color: var(--text); background: var(--hover); }

  /* ── Фільтри ── */
  .sl-bar { display: flex; align-items: center; justify-content: space-between; gap: 12px; flex-wrap: wrap; }
  .sl-tabs { margin: 0; }
  .sl-tools { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; margin-left: auto; }
  .sl-search {
    position: relative; display: flex; align-items: center; gap: 8px; width: 240px; max-width: 100%; height: 40px; padding: 0 13px;
    border-radius: 12px; background: var(--bg-input); border: 1px solid var(--border); cursor: text;
    transition: border-color .15s, box-shadow .15s;
  }
  .sl-search:focus-within { border-color: var(--orange); box-shadow: 0 0 0 3px rgba(var(--orange-rgb), .14); }
  .sl-search i { color: var(--text-dim); font-size: 16px; }
  .sl-search input { flex: 1; min-width: 0; background: transparent; border: none; outline: none; color: var(--text); font: 500 13px var(--font-ui); }
  .sl-search input::placeholder { color: var(--text-dim); }
  .sl-x { background: none; border: none; color: var(--text-dim); cursor: pointer; padding: 2px; display: flex; }
  .sl-x:hover { color: var(--text); }
  .sl-reset { background: none; border: none; color: var(--text-dim); font: 600 12px/1 var(--font-ui); cursor: pointer; display: inline-flex; align-items: center; gap: 5px; padding: 8px 4px; }
  .sl-reset:hover { color: var(--orange); }

  /* ── Сітка / стани ── */
  .sl-newtile {
    display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 6px; min-height: 180px;
    padding: 18px; border-radius: var(--r-lg); cursor: pointer; text-align: center; color: var(--text-mute);
    background: transparent; border: 1px dashed var(--border-hi, var(--border)); font: 500 11.5px/1.4 var(--font-ui);
    transition: border-color .15s, color .15s, background .15s;
  }
  .sl-newtile b { font: 700 13px/1.2 var(--font-ui); color: var(--text); }
  .sl-newtile:hover { border-color: rgba(var(--orange-rgb), .55); background: rgba(var(--orange-rgb), .05); color: var(--text); }
  .sl-newtile-ic { width: 40px; height: 40px; border-radius: 12px; display: grid; place-items: center; font-size: 20px; color: var(--orange); background: rgba(var(--orange-rgb), .1); }
  .sl-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(290px, 1fr)); gap: 14px; align-content: start; }
  .sl-loading { display: flex; align-items: center; justify-content: center; gap: 10px; padding: 60px 0; color: var(--text-mute); font-size: 13px; }
  .sl-empty { display: flex; flex-direction: column; align-items: center; gap: 10px; padding: 56px 20px; text-align: center; }
  .sl-empty-ic { width: 64px; height: 64px; border-radius: 20px; display: grid; place-items: center; font-size: 30px; color: var(--orange); background: rgba(var(--orange-rgb), .1); border: 1px dashed rgba(var(--orange-rgb), .35); }
  .sl-empty-t { font: 700 15px/1.2 var(--font-ui); color: var(--text); }
  .sl-empty-s { font: 400 12px/1.5 var(--font-ui); color: var(--text-mute); max-width: 360px; margin-bottom: 6px; }

  /* ── Папки серверів: плитка, міні-іконки, вибір у редакторі ── */
  .sl-cardwrap { min-width: 0; border-radius: 16px; }
  .sl-cardwrap.dragging { opacity: .35; }
  .sl-grid-modal { grid-template-columns: repeat(auto-fill, minmax(min(100%, 270px), 1fr)); }
  .sl-folders { margin-bottom: 14px; padding: 0; }
  .sl-ghost {
    position: fixed; left: 0; top: 0; z-index: 1000010; pointer-events: none; display: flex; align-items: center; gap: 9px;
    max-width: min(260px, 70vw); padding: 8px 12px 8px 8px; border-radius: 12px;
    background: var(--bg-card); border: 1px solid var(--orange); color: var(--text);
    box-shadow: 0 14px 34px rgba(0, 0, 0, .55), 0 0 0 3px rgba(var(--orange-rgb), .18);
  }
  .sl-ghost-ic { width: 30px; height: 30px; border-radius: 9px; display: grid; place-items: center; overflow: hidden; flex: none; color: var(--mc); background: color-mix(in srgb, var(--mc) 16%, var(--bg)); }
  .sl-ghost-ic img { width: 100%; height: 100%; object-fit: cover; image-rendering: pixelated; }
  .sl-ghost-n { font-size: 13px; font-weight: 700; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  :global(body.sl-dragging), :global(body.sl-dragging *) { cursor: grabbing !important; user-select: none !important; }

  .sl-pick { display: flex; flex-direction: column; gap: 4px; max-height: min(34vh, 280px); overflow-y: auto; padding: 4px; margin: 0 -4px; scrollbar-width: thin; }
  .sl-pick-empty { padding: 14px; text-align: center; font-size: 12px; color: var(--text-dim); border: 1px dashed var(--border-hi); border-radius: 10px; }
  .sl-pick-row {
    all: unset; box-sizing: border-box; display: flex; align-items: center; gap: 10px; padding: 7px 10px; border-radius: 10px; cursor: pointer;
    border: 1.5px solid transparent; transition: background .12s, border-color .12s;
  }
  .sl-pick-row:hover { background: var(--hover); }
  .sl-pick-row.on { background: color-mix(in srgb, var(--orange) 10%, transparent); border-color: color-mix(in srgb, var(--orange) 45%, transparent); }
  .sl-pick-chk {
    width: 18px; height: 18px; border-radius: 6px; display: grid; place-items: center; font-size: 12px; flex: none; color: transparent;
    border: 1.5px solid var(--border-hi); transition: all .12s;
  }
  .sl-pick-row.on .sl-pick-chk { background: var(--orange); border-color: var(--orange); color: #fff; }
  .sl-pick-ic { width: 30px; height: 30px; border-radius: 9px; display: grid; place-items: center; overflow: hidden; flex: none; color: var(--mc); background: color-mix(in srgb, var(--mc) 16%, var(--bg)); }
  .sl-pick-ic img { width: 100%; height: 100%; object-fit: cover; image-rendering: pixelated; }
  .sl-pick-n { flex: 1; min-width: 0; font-size: 13px; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .sl-pick-s { font-size: 11px; color: var(--text-mute); flex: none; }
</style>
