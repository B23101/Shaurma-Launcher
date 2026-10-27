<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import { t, setLanguage } from './i18n.svelte'
  import { syncAppearance, loadConsoleFonts, consoleFontValues, consoleFontStack as consoleFontStackShared, fontFamilyFromPath, registerBundledFonts } from './theme.svelte'
  import { App, on, Window, Application } from './wails'
  import { toast, confirmDialog } from './toast.svelte'
  import Toasts from './Toasts.svelte'
  import ConfirmModal from './ConfirmModal.svelte'
  import { packAsset } from './packAssets.svelte'
  import steveHead from '../assets/images/steve-head.png'
  import type {
    Settings, ConsoleLine, ConsoleContext, AIDiagnosis, AIFixResult, AIRecommendedAction,
    LaunchProgress, SyncProgress, BuildProgress, ConsoleAIEvent
  } from './types'

  // ── Пропси ──
  // standalone=true → окреме вікно консолі (на весь розмір, зі своїми
  // контролами вікна); false → вкладка «Консоль» на сторінці збірки.
  // packID → ID збірки, до якої ЖОРСТКО прив'язана консоль (вкладка):
  // показує саме її буфер/стан і ігнорує чужі запуски. Порожній packID
  // (окреме вікно) → консоль СЛІДУЄ за збіркою, що запускається/останньою
  // запущеною.
  let { standalone = false, packID = '', packIconUrl = '', packColor = '', packIcon = '' }: { standalone?: boolean; packID?: string; packIconUrl?: string; packColor?: string; packIcon?: string } = $props()

  // ── Стан ──
  let settings = $state<Settings>({
    language: 'uk', accent: 'orange', accentCustom: '#ff8a00', theme: 'dark', font: 'inter', fontPath: '',
    animationsEnabled: true,
    sidebarWidth: 220, sidebarScale: 1, sidebarOnlyPinned: false,
    themeCustomBg: '#0f0f0f', themeCustomCard: '#161616', themeCustomText: '#ffffff',
    autoUpdate: true, updateChannel: 'stable',
    startupMode: 'window', closeAction: 'quit', minimizeAction: 'taskbar', gameLaunchAction: 'keep', gameLaunchDelaySec: 0, gameExitAction: 'restore', restoreOnCrash: true,
    showConsole: false, saveLogs: true,
    instanceDir: '', javaPath: '', maxRAM: 4096, javaArgs: '', activeAccountId: '',    consoleMaxLines: 10000, consoleInfoColor: '#8fd3ff', consoleWarnColor: '#ffd166', consoleErrorColor: '#ff6b6b', consoleWrap: false, consoleFontSize: 12, consoleFontFamily: 'mono',
    consoleFontMode: 'all', consoleInfoFont: 'mono', consoleWarnFont: 'mono', consoleErrorFont: 'mono', consoleFontPath: '',
    consoleAI: false, consoleAIKey: '', showConsoleOnLaunch: false, showConsoleOnCrash: true, showConsoleOnClose: false, disableAutoRepair: false,
    maxConcurrentDownloads: 10, maxRetries: 6, httpTimeoutSec: 60,
    preLaunchCommand: '', wrapperCommand: '', postExitCommand: '', envVars: '',
    fullscreen: false, windowWidth: 854, windowHeight: 480, confirmOnStop: false,
    showRamWarnings: true,
    showServerWarnings: true,
    serverWarnIgnoreRam: false, serverWarnIgnoreJava: false, serverWarnIgnoreDisk: false, serverWarnIgnoreCrash: false,
    showGameTime: false, recordGameTime: true, showTotalGameTime: false, gameTimeInHours: false,
    showSessionTime: true, showGameTimeOnPage: true,
    tempModsDeleteByDefault: false,
    discordRpcEnabled: true, discordRPCDetails: '{pack}', discordRPCState: '{list}',
    cleanupAfterPackDelete: false
  })

  let lines = $state<ConsoleLine[]>([])
  let ctx = $state<ConsoleContext>({ instanceId: '', name: '', mcVersion: '', loader: '', running: false, ramMb: 4096 })
  // Іконка збірки в шапці панелі запуску — 1:1 як sidebar (.sp-ic):
  // PNG через packAsset, інакше шрифт-іконка pack.icon на кольоровому фоні.
  // Окреме вікно консолі не отримує пропси головного вікна — іконка/колір
  // приходять у ConsoleContext з бекенду (ctx.iconUrl/icon/color).
  const glIcon = $derived(packAsset(ctx.iconUrl || packIconUrl))
  // Fallback-іконка — ti-puzzle (як і в sidebar/картках збірок), а не
  // ti-box «кубик»: коли у збірки обрано пресет «пазл» або взагалі нічого,
  // консоль має показувати ту саму іконку, що й решта лаунчера.
  const glIconFallback = $derived(ctx.icon || packIcon || 'ti-puzzle')
  const glColor = $derived(ctx.color || packColor || 'var(--orange)')
  let gameRunning = $state(false)
  let gameCrashed = $state(false)
  let lastExitCode = $state(0)
  // Прогрес запуску збірки (launch:progress) — стадії ensure-Java/Loader/
  // worker-перевірка ПЕРЕД стартом процесу гри. Консоль показує їх у
  // панелі запуску, щоб юзер бачив живий стан, а не фіктивне «Запустити».
  let launchProgress = $state<LaunchProgress | null>(null)
  let filter = $state<'all' | 'info' | 'warn' | 'error'>('all')
  let search = $state('')
  // Автоскрол БЕЗ кнопки (як у Prism): скрол донизу відбувається лише
  // коли користувач уже внизу — onScroll оновлює цей прапорець.
  let autoscroll = $state(true)
  let maximized = $state(false)
  let loaded = $state(false)

  // AI-панель
  let aiState = $state<'idle' | 'thinking' | 'done' | 'basic'>('idle')
  let aiDiag = $state<AIDiagnosis | null>(null)
  // AI-відповідь стискабельна: кнопка «Згорнути/Розгорнути» в шапці панелі
  // ховає тіло аналізу (cause/explanation/факти), лишаючи заголовок —
  // щоб великий діагноз не займав півконсолі.
  let aiCollapsed = $state(false)
  // consoleTab — «Консоль» (живий потік логу) vs «Файл краху» (сирий
  // crash-report/вирізка WARN-ERROR, той самий текст, що бачила ШІ —
  // моноширинний стиль, зручний і людині для читання, і для копіювання
  // в стороннього ШІ-асистента вручну, якщо вбудований аналіз не
  // допоміг). Друга вкладка з'являється лише коли є що показати
  // (aiDiag?.rawLogText) — не займає місце в звичайному стані гри.
  let consoleTab = $state<'console' | 'crashfile'>('console')
  // aiDismissed — користувач натиснув «Сховати»: панель не показується до
  // НАСТУПНОГО крашу, навіть якщо gameCrashed лишився true (інакше після
  // сховання панель поверталась би з підказкою «тут з'явиться аналіз»).
  let aiDismissed = $state(false)
  let aiBusy = $state(false)
  let aiActionState = $state<'idle' | 'working' | 'done' | 'failed'>('idle')
  let aiActionResult = $state<AIFixResult | null>(null)
  let ramValue = $state(8) // ГБ, для increase_ram

  // ── Голови (PNG) акаунтів у перемикачі консолі ──
  // Ті самі data URL голів зі скінів Mojang, що й у сайдбарі (бекенд
  // кешує їх у App.GetAccountHead). Якщо не завантажились — голова Стіва.
  let accHeads = $state<Record<string, string>>({})
  $effect(() => {
    const accs = ctx.accounts || []
    if (!accs.length) return
    let cancelled = false
    ;(async () => {
      const next: Record<string, string> = {}
      await Promise.all(accs.map(async (acc) => {
        try {
          const url = await App.GetAccountHead(acc.uuid || '', acc.accountName, !!acc.licensed)
          // Ключ — accountId (ним же працює перемикач консолі і ctx.accountId).
          if (acc.accountId && url) next[acc.accountId] = url
        } catch { /* запасна голова Стіва */ }
      }))
      if (!cancelled) accHeads = next
    })()
    return () => { cancelled = true }
  })
  function headForAcc(accId?: string): string {
    return (accId && accHeads[accId]) || steveHead
  }

  // ── Віртуалізація: рендеримо лише видимі рядки ──
  // Висота рядка ЗАЛЕЖИТЬ від налаштованого розміру шрифту консолі
  // (consoleFontSize): LINE_H = шрифт + 10px вертикального дихания, щоб
  // математика віртуалізації (startIdx * LINE_H) збігалася з реальною
  // висотою рядків у DOM. Дефолт 12px → LINE_H 22px (як раніше).
  const consoleFontSize = $derived(Math.max(9, Math.min(24, settings.consoleFontSize || 12)))
  const LINE_H = $derived(consoleFontSize + 10)
  const consoleFontStack = $derived(consoleFontStackShared(settings.consoleFontFamily || 'mono'))

  // Шрифт РЯДКА: у режимі 'perType' (налаштування → Консоль → «Окремо для
  // кожного типу») INFO/WARN/ERROR рендеряться РІЗНИМИ шрифтами
  // (consoleInfoFont/WarnFont/ErrorFont); у режимі 'all' — спільним
  // (consoleFontFamily). Делегує в consoleFontStack() з theme.svelte.ts
  // (БАГ, виправлено: тут раніше була ОКРЕМА локальна копія цієї логіки
  // без гілки для вбудованих шрифтів (FONTS[f]) — Minecraft/Marker Felt
  // коректно показувались у списку вибору, але візуально нічого не
  // змінювали: consoleFontStackFor()/lineFont() падали на дефолтний
  // var(--font-mono) для будь-якого значення, якого явно не очікували).
  function lineFont(lvl: string): string {
    const f = settings.consoleFontMode === 'perType'
      ? (lvl === 'error' ? settings.consoleErrorFont : lvl === 'warn' ? settings.consoleWarnFont : settings.consoleInfoFont) || 'mono'
      : settings.consoleFontFamily || 'mono'
    return consoleFontStackShared(f)
  }
  const OVERSCAN = 30
  let termEl: HTMLDivElement | undefined = $state()
  let scrollTop = $state(0)
  let viewportH = $state(600)

  let nextLocalId = 1_000_000

  function now(): string {
    return new Date().toTimeString().slice(0, 8)
  }

  // Визначення рівня рядка на клієнті (для живого потоку console:line;
  // рядки зі знімка вже мають рівень з бекенду).
  function detectLevel(text: string): string {
    if (/error|exception|fatal|crash|failed|unable to|outofmemory|severe|\[stderr\]|\[error\]|throwable/i.test(text)) return 'error'
    if (/warn|warning|can't keep up/i.test(text)) return 'warn'
    return 'info'
  }

  // Додає рядок у буфер з обрізанням за consoleMaxLines (рядків).
  //
  // ВАЖЛИВО (фікс «підвисає при флуді логів»): при швидкому потоці (Forge/
  // Fabric під час завантаження великої збірки чи краш-дампу видає сотні
  // рядків/сек) кожен push у $state-масив раніше тригерив ПОВНИЙ
  // перерахунок filtered/errorCount ($derived — Array.filter по всьому
  // lines, до consoleMaxLines елементів) і DOM-мутацію автоскролу
  // ($effect) ОКРЕМО на кожен рядок. Сотні таких перерахунків за секунду —
  // і UI-потік вкладки (рендер, скрол, інші реактивні ефекти) захлинався.
  // Тепер вхідні рядки складаються в pendingLines і скидаються в lines
  // ОДНІЄЮ пачкою раз на кадр (requestAnimationFrame) — Svelte тоді робить
  // один прохід filtered/errorCount/автоскролу на кадр незалежно від того,
  // скільки рядків прийшло за цей час, замість одного проходу на рядок.
  let pendingLines: ConsoleLine[] = []
  let flushScheduled = false

  let flushRafHandle = 0

  function flushPendingLines() {
    flushScheduled = false
    if (pendingLines.length === 0) return
    lines.push(...pendingLines)
    pendingLines = []
    const max = Math.max(100, settings.consoleMaxLines || 10000)
    if (lines.length > max) lines.splice(0, lines.length - max)
  }

  function addLine(level: string, text: string) {
    pendingLines.push({ id: nextLocalId++, time: now(), level, text })
    if (!flushScheduled) {
      flushScheduled = true
      flushRafHandle = requestAnimationFrame(flushPendingLines)
    }
  }

  // ── Підписки на події бекенду ──
  let unsubs: (() => void)[] = []

  // Перечит знімка буфера + контексту (після фокусу на збірку або
  // перемикання акаунта в консолі). Стан краху та AI-діагноз відновлюємо
  // з БЕКЕНДУ (consoleDiag — спільний для всіх копій консолі), а не з
  // локального стану: тому консоль, відкрита ПІСЛЯ краху, показує той
  // самий віджет ШІ, що й та, яка бачила крах наживо. Діагноз іншої
  // збірки/акаунта при цьому не лишається під шапкою нової.
  // targetID — яку збірку показати: явно переданий ID (вкладка консолі
  // зі своєї сторінки збірки) або порожньо → останню запущену. Параметр
  // НАВМИСНО називається не так, як проп packID, щоб у коді нижче не було
  // неоднозначності «проп чи аргумент» (крихкий shadowing, який ловив
  // рев'юер).
  async function reloadConsole(targetID?: string) {
    aiActionState = 'idle'
    aiActionResult = null
    accMenuOpen = false
    try {
      const snap = await App.GetConsoleSnapshot(targetID ?? '')
      lines = snap?.length ? snap : [] // порожній буфер → очищаємо, а не лишаємо старі рядки
      // Той самий ризик "ожилих" рядків, що в console:cleared/clearLog —
      // pendingLines міг накопичити рядки старого контексту (інша збірка/
      // акаунт) до перечитування знімка.
      pendingLines = []
      // Запам'ятовуємо ПОПЕРЕДНЮ збірку ДО перезапису ctx: за нею
      // визначаємо, чи це «та сама консоль» (повторний фокус — зберігаємо
      // AI-панель) чи інша збірка/акаунт (старий діагноз прибираємо).
      const prevId = ctx.instanceId
      const c = await App.GetConsoleContext(targetID ?? '')
      ctx = c
      gameRunning = c.running
      // Відновлюємо стан крашу (навіть якщо подія game:exit пройшла, поки
      // консоль була закрита). Новий запуск гри (crashed=false) прибирає
      // AI-панель.
      gameCrashed = !!c.crashed
      lastExitCode = c.lastExitCode || 0
      if (c.diagnosis) {
        aiDiag = c.diagnosis
        aiState = diagState(c.diagnosis)
        aiDismissed = false
      } else if (targetID && prevId !== targetID) {
        // Діагнозу немає І збірка/акаунт змінились — прибираємо стару
        // AI-панель (інакше діагноз попередньої збірки лишився б під
        // шапкою нової).
        aiState = 'idle'
        aiDiag = null
        aiDismissed = false
      }
      if (c.running) ramValue = Math.max(1, Math.round(c.ramMb / 1024))
      // Консоль знає РЕАЛЬНИЙ стан запуску, навіть якщо відкрилась посеред
      // нього: бекенд повертає останню стадію launch:progress (вони могли
      // пройти, поки вікно консолі було закрите).
      launchProgress = c.launching
        ? { instanceId: c.instanceId, stage: (c.launchStage || 'checking') as LaunchProgress['stage'], message: c.launchMessage, percent: c.launchPercent, done: false }
        : null
    } catch { /* ок */ }
  }

  // ── Стан збірки в панелі запуску ──
  // Консоль знає РЕАЛЬНИЙ стан збірки (ctx.status з бекенду — той самий,
  // що в sidebar/«Мої збірки») і показує правильну дію: Завантажити /
  // Оновити / Запустити / живий прогрес. Жодного фіктивного «Запустити»
  // для збірки, яка ще не встановлена або застаріла.
  const consoleAction = $derived.by((): 'download' | 'update' | 'play' | 'syncing' | 'launching' | 'disabled' => {
    if (!ctx.instanceId) return 'disabled'
    // Окреме вікно консолі БЕЗ жорсткої прив'язки (packID порожній — старе
    // відкриття без ?build=), прив'язане до ЗАСТАРІЛОЇ збірки (fallback на
    // lastInstanceID — збірку, яку користувач зараз НЕ вибрав у головному
    // вікні): НЕ пропонуємо ЖОДНИХ дій (ні «Запустити», ні
    // «Завантажити»/«Оновити») — консоль стає переглядачем логу. Інакше
    // клік запускав би не ту збірку (баг «запустилась інша збірка»:
    // консоль була прив'язана до BlockFront зі старої сесії, і її кнопка
    // запускала саме його). Вікно З прив'язкою (packID задано — бекенд
    // відкриває його через ?build=) відповідає ЗА СВОЮ збірку: дії
    // дозволені, бо вікно показує саме її стан. Вкладка на сторінці збірки
    // (packID задано) прив'язана до вибраної збірки — для неї Active=true,
    // тому обмеження і так не застосовується.
    if (standalone && !packID && !ctx.active && !gameRunning) return 'disabled'
    if (launchProgress) return 'launching'
    const st = ctx.status
    if (st === 'downloading' || st === 'updating' || (ctx.progress && (st === 'ready' || st === 'needs-update' || st === 'not-installed'))) return 'syncing'
    if (st === 'not-installed') return 'download'
    if (st === 'needs-update') return 'update'
    if (st === 'error') return 'disabled'
    // Невідомий/порожній стан (старий бекенд, гонка) — НЕ показуємо
    // фіктивне «Запустити»: консоль має показувати лише реальний стан.
    if (!st) return 'disabled'
    return 'play'
  })

  function statusLabel(st?: string): string {
    switch (st) {
      case 'not-installed': return t('console.status.notInstalled')
      case 'needs-update': return t('console.status.needsUpdate')
      case 'ready': return t('console.status.ready')
      case 'downloading': return t('console.status.downloading')
      case 'updating': return t('console.status.updating')
      case 'error': return t('console.status.error')
      case 'running': return t('console.gameRunning')
      default: return t('console.notRunning')
    }
  }

  // Підпис стадії запуску (launch:progress) — короткі, людські.
  function launchStageLabel(stage?: string): string {
    switch (stage) {
      case 'checking': return t('launch.stage.checking')
      case 'worker_update': return t('launch.stage.workerUpdate')
      case 'java': return t('launch.stage.java')
      case 'loader': return t('launch.stage.loader')
      default: return t('console.launching')
    }
  }

  // Підпис стадії якання файлів збірки (sync:progress) — щоб розпаковка/
  // фіналізація не виглядали «завислими».
  function syncStageLabel(stage?: string): string {
    switch (stage) {
      case 'manifest_fetched': return t('sync.stage.manifest')
      case 'extracting': return t('sync.stage.extracting')
      case 'finalizing': return t('sync.stage.finalizing')
      default: return t('sync.stage.downloading')
    }
  }

  // ── Скасування зависаючих кроків (встановлення лоадера/Java/якання) ──
  // cancellingInstall лише блокує повторний клік, поки бекенд обробляє
  // скасування — сам факт, що consoleAction перестане бути 'launching'/
  // 'syncing', прийде через launch:progress / sync:progress events.
  let cancellingInstall = $state(false)

  async function cancelLaunchClick() {
    if (cancellingInstall) return
    if (!ctx.instanceId) return
    cancellingInstall = true
    try {
      await App.CancelGameInstall(ctx.instanceId)
    } catch (e) {
      toast(t('error.withMessage', { message: errText(e) }), 'error')
    } finally {
      cancellingInstall = false
    }
  }

  async function cancelSyncClick() {
    const id = ctx.instanceId
    if (!id || cancellingInstall) return
    cancellingInstall = true
    try {
      await App.CancelDownload(id)
    } catch (e) {
      toast(t('error.withMessage', { message: errText(e) }), 'error')
    } finally {
      cancellingInstall = false
    }
  }

  // Головна дія панелі запуску: збірка не встановлена → Завантажити,
  // є оновлення → Оновити, інакше → Запустити. Усі три шляхи запускають
  // один і той самий бекенд (LaunchInstance сам звіряє версію і якає
  // зміни), але кнопка чесно показує, що станеться далі.
  async function primaryConsoleAction() {
    const id = ctx.instanceId
    if (!id || consoleAction === 'disabled' || consoleAction === 'syncing' || consoleAction === 'launching') return
    if (consoleAction === 'download' || consoleAction === 'update') {
      try {
        await App.DownloadPack(id)
      } catch (e) {
        toast(t('error.withMessage', { message: errText(e) }), 'error')
      }
      return
    }
    await launchGame()
  }

  // Перемикач консолі: лог іншого акаунта для цієї самої збірки.
  async function switchConsoleAccount(accountID: string) {
    if (!ctx.instanceId) return
    accMenuOpen = false
    try {
      await App.SwitchConsoleAccount(accountID, ctx.instanceId)
      await reloadConsole(ctx.instanceId)
    } catch { /* ок */ }
  }

  let accMenuOpen = $state(false)

  onMount(async () => {
    try {
      // ВИГЛЯД (тема/акцент/шрифт) чіпаємо ЛИШЕ в окремому вікні
      // (standalone): там свій <html>, і його треба налаштувати самому.
      // Коли консоль — вкладка сторінки збірки, вона живе в ГОЛОВНОМУ
      // вікні, чиїм виглядом володіє App.svelte. Раніше тут теж викликався
      // applyTheme(s.theme) без кольорів користувача — для власної теми це
      // ЗНІМАЛО inline-змінні (--bg-app/--text/...) з <html> головного
      // вікна, і власний стиль скидався щойно відкриєш розділ «Консоль».
      let s: Awaited<ReturnType<typeof App.GetSettings>> | null
      if (standalone) {
        const r = await syncAppearance()
        unsubs.push(r.off)
        s = r.settings
      } else {
        s = await App.GetSettings()
      }
      if (!s) throw new Error('no settings')
      settings = s
      setLanguage(s.language)
      // Вбудовані шрифти (assets/ttf) — faces у документі вікна консолі.
      registerBundledFonts()
      // Шрифт(и) консолі (custom TTF або file:-шрифти з теки fonts) —
      // завантажуємо при старті консолі.
      loadConsoleFonts(consoleFontValues(s), (s as any).consoleFontPath).catch(() => {})
    } catch { /* налаштування вже мають дефолти */ }

    // Знімок буфера з бекенду: усе, що записалось, поки консоль була
    // закрита (гра активна — запис іде незалежно від вікна). Вкладка
    // прив'язується до СВОЄЇ збірки (packID), вікно — до останньої
    // запущеної (порожній packID).
    await reloadConsole(packID || undefined)
    loaded = true

    unsubs.push(
      // Живий потік рядків: окреме вікно (packID не задано) читає
      // console:lines — бекенд шле його ПАЧКАМИ (console_batch.go: раз на
      // 40 мс або коли пачка виросла до 500 рядків). Одиночних console:line
      // бекенд БІЛЬШЕ не надсилає (зроблено навмисно — див. коментар у
      // wireSessionLauncher: поодинокі CGo-виклики на рядок зависали UI).
      // Тому підписка ЛИШЕ на одиночні (як було до батчування) означала,
      // що відкрита вкладка консолі не отримувала ЖОДНОГО живого рядка:
      // контент з'являвся лише при перечитуванні знімка (onMount при
      // поверненні в розділ) — звідси і симптом «консоль мертва, доки не
      // перейдеш на інший розділ і назад». Вкладка (packID задано) читає
      // buildConsole:lines і фільтрує рядки СВОЄЇ збірки — запуск іншої
      // збірки не домішує чужі логи у вкладку.
      // Одиночні console:line/buildConsole:line лишаємо на випадок збірок
      // з бекендом, що ще шле їх поодинці (shaurma-linux-bridge).
      on('console:line', (line: string) => {
        if (packID) return
        addLine(detectLevel(line), String(line))
      }),
      on('console:lines', (batch: unknown) => {
        if (packID) return
        if (!Array.isArray(batch)) return
        for (const line of batch) {
          if (line == null) continue
          addLine(detectLevel(String(line)), String(line))
        }
      }),
      on('buildConsole:line', (e: { buildId: string; accountId: string; line: string }) => {
        if (!packID || e.buildId !== packID) return
        // Консоль показує лог ОДНОГО акаунта (перемикач): рядки сесій інших
        // акаунтів тієї самої збірки не домішуємо в буфер на екрані.
        if (ctx.accountId && e.accountId && e.accountId !== ctx.accountId) return
        addLine(detectLevel(e.line), String(e.line))
      }),
      on('buildConsole:lines', (batch: unknown) => {
        if (!packID || !Array.isArray(batch)) return
        for (const e of batch as Array<{ buildId: string; accountId: string; line: string } | null>) {
          if (!e || e.buildId !== packID) continue
          // Той самий фільтр акаунта, що й в одиночному buildConsole:line.
          if (ctx.accountId && e.accountId && e.accountId !== ctx.accountId) continue
          addLine(detectLevel(e.line), String(e.line))
        }
      }),
      on('game:exit', async (code: number) => {
        if (packID) return // вкладка реагує на build:exit своєї збірки
        lastExitCode = code
        gameRunning = false
        // WasLastExitUserStop — та сама причина, що build:exit нижче:
        // примусово вбитий процес (kill/taskkill, який робить
        // StopInstance при «Зупинити») майже завжди повертає ненульовий
        // код виходу на рівні ОС — нормальна поведінка для вбивства
        // процесу, а не ознака краху. game:exit — безадресна подія
        // (просто код, без прапорця), тож дізнаємось про ручну зупинку
        // окремим викликом замість розширення формату самої події.
        let wasUserStop = false
        try { wasUserStop = await App.WasLastExitUserStop() } catch { /* лишається false */ }
        gameCrashed = code !== 0 && !wasUserStop
        launchProgress = null
        const s = settings
        if (gameCrashed && settings.consoleAI) {
          // Краш — AI-панель показується САМА (видно її лише при краші,
          // не завжди) і одразу авто-активує аналіз: користувач нічого
          // не натискає, діагностика стартує через 400 мс. Аналіз робить
          // ЛИШЕ ШІ (Gemini) — машинного аналізу немає; якщо ШІ вимкнено
          // в налаштуваннях (consoleAI=false) — нічого не запускаємо.
          aiDismissed = false
          setTimeout(() => analyzeCrash(), 400)
        }
      }),
      // Вихід збірки, до якої прив'язана вкладка консолі (build:exit несе
      // buildId — game:exit його не несе). Лише для акаунта, чий лог
      // показуємо: сесія тієї самої збірки на іншому акаунті — окрема гра.
      on('build:exit', (e: { buildId: string; accountId: string; code: number; wasUserStop?: boolean }) => {
        if (!packID || e.buildId !== packID) return
        if (ctx.accountId && e.accountId && e.accountId !== ctx.accountId) return
        lastExitCode = e.code
        gameRunning = false
        // !e.wasUserStop — див. коментар у game:exit вище: ручна зупинка
        // (StopInstance) не є крашем, навіть якщо код виходу ненульовий.
        gameCrashed = e.code !== 0 && !e.wasUserStop
        launchProgress = null
        if (gameCrashed && settings.consoleAI) {
          // Аналіз крашу — ЛИШЕ через ШІ (Gemini), і лише коли consoleAI
          // увімкнено (див. коментар у game:exit).
          aiDismissed = false
          setTimeout(() => analyzeCrash(), 400)
        }
      }),
      on('game:started', (packIDArg: string) => {
        if (packID) return // вкладка реагує на build:started своєї збірки
        gameRunning = true
        gameCrashed = false
        launchProgress = null
        aiState = 'idle'
        aiDiag = null
        consoleTab = 'console'
        aiActionState = 'idle'
        aiActionResult = null
        // Окреме вікно СЛІДУЄ за збіркою, що запустилась: якщо воно було
        // прив'язане до ІНШОЇ (напр. останньої запущеної раніше) —
        // переприв'язуємось. Інакше після запуску нової збірки консоль
        // показувала б старий лог «чужої» збірки, і користувач думав би,
        // що запустилась не та.
        if (packIDArg && packIDArg !== ctx.instanceId) {
          reloadConsole(packIDArg)
        }
      }),
      on('build:started', (e: { buildId: string; accountId: string; accountName: string }) => {
        if (!packID || e.buildId !== packID) return
        // Лише сесія акаунта, чий лог показуємо: запуск тієї самої збірки
        // на ІНШОМУ акаунті не має позначати цю консоль як «гра йде».
        if (ctx.accountId && e.accountId && e.accountId !== ctx.accountId) return
        gameRunning = true
        gameCrashed = false
        launchProgress = null
        aiState = 'idle'
        aiDiag = null
        consoleTab = 'console'
        aiActionState = 'idle'
        aiActionResult = null
      }),
      // Прогрес запуску збірки (launch:progress): стадії ensure-Java/Loader/
      // worker-перевірка. Консоль показує їх у панелі запуску — замість
      // фіктивного «Запустити» юзер бачить, що саме робить лаунчер зараз
      // (Перевірка збірки… / Оновлення файлів… / Перевірка Java… 45%…).
      on('launch:progress', (p: LaunchProgress) => {
        // Вкладка (packID задано): показуємо прогрес лише своєї збірки.
        if (packID) {
          if (p.instanceId !== packID) return
          if (p.error || p.done) {
            launchProgress = null
            reloadConsole(packID)
          } else {
            launchProgress = p
          }
          return
        }
        // Окреме вікно: якщо запускається ІНША збірка, ніж прив'язана —
        // переприв'язуємось до неї одразу (та сама гарантія «консоль
        // показує реальний стан збірки, що запускається»).
        if (p.instanceId && p.instanceId !== ctx.instanceId) {
          ctx = { ...ctx, instanceId: p.instanceId }
          reloadConsole(p.instanceId)
          return
        }
        if (!ctx.instanceId || p.instanceId !== ctx.instanceId) return
        if (p.error || p.done) {
          launchProgress = null
          // Запуск завершився (успіх або помилка) — перечитуємо контекст,
          // щоб панель показала свіжий стан збірки.
          reloadConsole(ctx.instanceId)
        } else {
          launchProgress = p
        }
      }),
      // Прогрес синхронізації файлів збірки (sync:progress): поки збірка
      // качається/оновлюється — живлю ctx.progress і статус, щоб панель
      // консолі показувала реальний % у реальному часі.
      on('sync:progress', (p: SyncProgress) => {
        if (!ctx.instanceId || p.packId !== ctx.instanceId) return
        if (p.mode === 'running' || p.mode === 'paused') {
          const prog: BuildProgress = {
            percent: p.overallPercent,
            bytesDone: p.bytesDownloaded,
            bytesTotal: p.bytesTotal,
            filesDone: p.filesCompleted,
            filesTotal: p.filesTotal,
            currentFile: p.currentFile,
            speedBps: p.speedBps,
            mode: p.mode,
            stage: p.stage,
            noInternet: p.noInternet
          }
          const st = ctx.status === 'not-installed' ? 'downloading' : (ctx.status === 'updating' || ctx.status === 'downloading' ? ctx.status : 'updating')
          ctx = { ...ctx, status: st, progress: prog }
        } else if (p.mode === 'complete' || p.mode === 'cancelled' || p.mode === 'error') {
          // Синхронізація закінчилась — свіжий статус з бекенду (ready /
          // needs-update / not-installed після скасування).
          reloadConsole(ctx.instanceId)
        }
      }),
      // Реєстр збірок змінився (встановлено/оновлено/видалено) — консоль
      // перечитує контекст, щоб статус і кнопка дії були актуальними.
      on('builds:changed', (changedID: string) => {
        if (changedID && changedID === ctx.instanceId) reloadConsole(ctx.instanceId)
      }),
      // Перемикання консолі на конкретну збірку (кнопка «Консоль» на
      // сторінці збірки / вибір збірки в головному вікні): перечитуємо
      // знімок буфера і контекст, щоб показувати логи саме цієї збірки,
      // а не останньої запущеної. Вкладка (packID задано) прив'язана до
      // СВОЄЇ збірки: реагує лише на фокус своєї збірки, щоб відкриття
      // сторінки іншої (SelectActivePack → console:focus) не переприв'язало
      // її на чужу збірку. Окреме вікно (packID порожній) слідує за фокусом.
      on('console:focus', (focusID: string) => {
        if (packID) {
          if (focusID === packID) reloadConsole(focusID)
        } else {
          reloadConsole(focusID || undefined)
        }
      }),
      // Новий AI-діагноз (хтось натиснув «Проаналізувати» у будь-якій
      // копії консолі — вікні чи вкладці). Подія несе buildID: у режимі
      // «кілька вікон консолі по збірках» реагуємо лише на діагноз СВОЄЇ
      // збірки (packID для вкладки/вікна з ?build=, інакше — поточної
      // прив'язаної). Чужий діагноз не показується в консолі іншої збірки.
      on('console:ai', (e: ConsoleAIEvent) => {
        if (packID && e.buildId !== packID) return
        if (!packID && ctx.instanceId && e.buildId && e.buildId !== ctx.instanceId) return
        const d = e.diagnosis
        aiDiag = d
        aiState = diagState(d)
        aiDismissed = false
        aiCollapsed = false
        if (d.currentRamMb) ramValue = Math.max(1, Math.round(d.currentRamMb / 1024))
      }),
      // Буфер очищено (з будь-якої копії) — прибираємо AI-панель лише в
      // консолях тієї самої збірки (console:cleared несе buildID).
      // buildID порожній = старе очищення «поточного» буфера: реагуємо лише
      // якщо консоль НЕ жорстко прив'язана до конкретної збірки.
      on('console:cleared', (buildId: string) => {
        if (packID && buildId && buildId !== packID) return
        if (!packID && ctx.instanceId && buildId && buildId !== ctx.instanceId) return
        if (buildId === '' && (packID || ctx.instanceId)) return
        lines = []
        // Рядки, що встигли накопичитись у pendingLines до цього
        // requestAnimationFrame, інакше "ожили" б наступного кадру вже
        // ПІСЛЯ очищення — старі рядки знову з'явились би у списку.
        pendingLines = []
        aiDiag = null
        aiState = 'idle'
        consoleTab = 'console'
        aiActionState = 'idle'
        aiActionResult = null
        aiDismissed = false
        gameCrashed = false
        lastExitCode = 0
      })
    )

    // Висота в'юпорта для віртуалізації.
    requestAnimationFrame(() => {
      if (termEl) {
        viewportH = termEl.clientHeight
        termEl.scrollTop = termEl.scrollHeight
      }
    })
  })

  onDestroy(() => {
    unsubs.forEach((u) => { try { u?.() } catch { /* ок */ } })
    if (flushScheduled) cancelAnimationFrame(flushRafHandle)
  })

  // ── Фільтр + пошук ──
  const filtered = $derived(
    lines.filter((l) => {
      if (filter !== 'all' && l.level !== filter) return false
      if (search && !l.text.toLowerCase().includes(search.toLowerCase())) return false
      return true
    })
  )

  const errorCount = $derived(lines.filter((l) => l.level === 'error').length)

  // Видимий діапазон рядків (віртуалізація).
  const startIdx = $derived(Math.max(0, Math.floor(scrollTop / LINE_H) - OVERSCAN))
  const endIdx = $derived(Math.min(filtered.length, Math.ceil((scrollTop + viewportH) / LINE_H) + OVERSCAN))
  const visible = $derived(filtered.slice(startIdx, endIdx))

  function onScroll() {
    if (!termEl) return
    scrollTop = termEl.scrollTop
    viewportH = termEl.clientHeight
    // Біля дна → автопрокрутка; інакше користувач переглядає історію.
    autoscroll = termEl.scrollTop + termEl.clientHeight >= termEl.scrollHeight - 4
  }

  // Сигнал «з'явився новий рядок» за id ОСТАНЬОГО рядка, а не за
  // filtered.length: коли буфер упирався в consoleMaxLines, length більше
  // не змінюється (нові рядки приходять разом з обрізкою старих) — ефект
  // нижче мав би тоді залежати лише від нього і зупинявся б на довгому
  // потоці логів. id монотонний, тож змінюється на кожен новий рядок.
  const lastLineId = $derived(filtered.length ? filtered[filtered.length - 1].id : -1)

  // Після додавання рядків, якщо автоскрол — донизу. Під час пошуку не
  // скролимо (користувач переглядає результати, а не стежить за потоком).
  $effect(() => {
    void lastLineId
    if (!autoscroll || !termEl || search) return
    if (filtered.length) termEl.scrollTop = termEl.scrollHeight
  })

  function setFilter(f: 'all' | 'info' | 'warn' | 'error') { filter = f }

  // Закриття меню акаунтів консолі по кліку поза ним.
  $effect(() => {
    if (!accMenuOpen) return
    const close = () => { accMenuOpen = false }
    document.addEventListener('click', close)
    return () => document.removeEventListener('click', close)
  })

  function clearSearch() { search = '' }

  function escapeHtml(s: string): string {
    return s.replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]!))
  }

  // Підсвітка пошуку у видимому рядку (без ламання кольорового рівня).
  function highlight(text: string): string {
    const esc = escapeHtml(text)
    if (!search) return esc
    const q = search.toLowerCase()
    const lower = esc.toLowerCase()
    let out = ''
    let i = 0
    let idx = lower.indexOf(q)
    while (idx !== -1) {
      out += esc.slice(i, idx) + '<mark>' + esc.slice(idx, idx + q.length) + '</mark>'
      i = idx + q.length
      idx = lower.indexOf(q, i)
    }
    return out + esc.slice(i)
  }

  function levelLabel(lvl: string): string {
    if (lvl === 'error') return 'ERR'
    if (lvl === 'warn') return 'WARN'
    if (lvl === 'system') return 'SYS'
    return 'INFO'
  }

  function levelColor(lvl: string): string {
    if (lvl === 'error') return settings.consoleErrorColor || '#ff6b6b'
    if (lvl === 'warn') return settings.consoleWarnColor || '#ffd166'
    if (lvl === 'system') return 'var(--text-dim)'
    return settings.consoleInfoColor || '#8fd3ff'
  }

  // Людський текст з Error/string (для toast-повідомлень — не "[object Object]").
  function errText(e: unknown): string {
    if (e instanceof Error) return e.message
    return String(e)
  }

  // ── Дії з логом ──
  function logText(): string {
    return filtered.map((l) => `[${l.time}] [${l.level.toUpperCase()}] ${l.text}`).join('\n')
  }

  let copiedTick = $state(0)
  async function copyLog() {
    try {
      await App.CopyToClipboard(logText())
      copiedTick = Date.now()
      toast(t('console.copied'), 'success', 1600)
    } catch (e) {
      toast(t('error.withMessage', { message: errText(e) }), 'error')
    }
  }

  async function saveLog() {
    try {
      // SaveConsoleLog відкриває ДІАЛОГ збереження — юзер сам вибирає теку
      // і назву файлу. Скасування повертає '' — мовчки нічого не робимо.
      const path = await App.SaveConsoleLog(logText())
      if (!path) return
      toast(
        t('console.savedTo', { path }),
        'success',
        5000,
        { label: t('console.openFolder'), onClick: () => App.RevealPath(path).catch(() => {}) },
      )
    } catch (e) {
      toast(t('error.withMessage', { message: errText(e) }), 'error')
    }
  }

  // ── Вкладка «Файл краху» ────────────────────────────────────────────
  // copyCrashLog/saveCrashLog — ті самі дії, що copyLog/saveLog вище,
  // але для aiDiag.rawLogText (сирий crash-report/вирізка WARN-ERROR,
  // ТОЧНО той самий текст, що пішов у промт моделі) замість поточного
  // видимого потоку консолі. Окремий copiedTick (crashCopiedTick), щоб
  // «✓ скопійовано» на кнопці вкладки «Файл краху» не блимало заразом з
  // кнопкою «Консоль», якщо обидві клікнули майже одночасно.
  let crashCopiedTick = $state(0)
  async function copyCrashLog() {
    if (!aiDiag?.rawLogText) return
    try {
      await App.CopyToClipboard(aiDiag.rawLogText)
      crashCopiedTick = Date.now()
      toast(t('console.copied'), 'success', 1600)
    } catch (e) {
      toast(t('error.withMessage', { message: errText(e) }), 'error')
    }
  }

  async function saveCrashLog() {
    if (!aiDiag?.rawLogText) return
    try {
      const path = await App.SaveConsoleLog(aiDiag.rawLogText)
      if (!path) return
      toast(
        t('console.savedTo', { path }),
        'success',
        5000,
        { label: t('console.openFolder'), onClick: () => App.RevealPath(path).catch(() => {}) },
      )
    } catch (e) {
      toast(t('error.withMessage', { message: errText(e) }), 'error')
    }
  }

  // Вивантаження на mclo.gs — З ПОПЕРЕДЖЕННЯМ про приватні дані (завжди).
  async function shareLog() {
    const ok = await confirmDialog(
      t('console.shareWarningTitle'),
      t('console.shareWarningText', { service: 'mclo.gs' }),
      { confirmLabel: t('console.shareYes'), cancelLabel: t('cancel'), danger: true }
    )
    if (!ok) return
    try {
      toast(t('console.shareUploading'), 'info', 0)
      const url = await App.UploadConsoleLog(logText())
      toast(t('console.shareDone'), 'success')
      App.OpenExternal(url)
    } catch (e) {
      toast(t('console.shareError', { message: errText(e) }), 'error')
    }
  }

  function clearLog() {
    lines = []
    // Див. коментар у console:cleared — той самий ризик "ожилих" рядків.
    pendingLines = []
    aiDiag = null
    aiState = 'idle'
    consoleTab = 'console'
    App.ClearConsoleBuffer(packID || '')
  }

  // ── Запуск/зупинка ──
  async function launchGame() {
    const id = ctx.instanceId
    if (!id) { toast(t('console.noInstance'), 'warn'); return }
    try {
      // Той самий захист від гонитви акаунта, що в App.svelte doLaunch:
      // фіксуємо акаунт СИНХРОННО перед довгим LaunchInstance. Тут це
      // ctx.accountId — акаунт, показаний У ЦЬОМУ вікні консолі (не
      // обов'язково поточний активний акаунт головного вікна, яке може
      // бути десь перемкнене окремо), бо саме його гравець бачить на
      // екрані, натискаючи «Перезапустити».
      if (ctx.accountId) await App.SetLaunchClickAccount(id, ctx.accountId)
      await App.LaunchInstance(id)
      gameRunning = true
      gameCrashed = false
      aiState = 'idle'
      aiDiag = null
      aiActionState = 'idle'
      aiActionResult = null
    } catch (e) {
      toast(t('launchError', { message: errText(e) }), 'error')
    }
  }

  async function stopGame() {
    // Підтвердження перед зупинкою — та сама настройка, що й на сторінці
    // збірки (Налаштування → Вікно гри → «Підтверджувати зупинку»).
    if (settings.confirmOnStop) {
      const ok = await confirmDialog(
        t('pack.stopConfirm'),
        t('pack.stopConfirmHint'),
        { confirmLabel: t('pack.stop'), cancelLabel: t('cancel'), danger: true }
      )
      if (!ok) return
    }
    try {
      await App.StopGame()
      gameRunning = false
    } catch (e) { /* ок */ }
  }

  // ── AI-аналіз крашу ──
  // Стан AI-панелі за діагнозом: done — відповідь ШІ є; basic — ШІ
  // ввімкнений, але не відповів (показуємо помилку з «Повторити»); idle —
  // ШІ вимкнено в налаштуваннях (лишаємо підказку, без «базового аналізу»).
  function diagState(d: AIDiagnosis): 'idle' | 'done' | 'basic' {
    if (d.aiResult) return 'done'
    return d.aiEnabled ? 'basic' : 'idle'
  }

  async function analyzeCrash() {
    const id = ctx.instanceId || ''
    if (!id) { toast(t('console.noInstance'), 'warn'); return }
    if (aiBusy) return
    aiBusy = true
    aiState = 'thinking'
    aiDiag = null
    aiActionState = 'idle'
    aiActionResult = null
    try {
      const diag = await App.AnalyzeCrash(id)
      aiDiag = diag
      aiState = diagState(diag)
      aiCollapsed = false
      // Автоперемикання на вкладку «Файл краху» — саме заради неї
      // прийшли сюди (гра впала). Не форсуємо, якщо крашу насправді
      // немає (rawLogText порожній — напр. вихід без аварії).
      if (diag.rawLogText) consoleTab = 'crashfile'
      if (diag.currentRamMb) ramValue = Math.max(1, Math.round(diag.currentRamMb / 1024))
    } catch (e) {
      aiState = 'basic'
      aiDiag = null
      toast(t('console.aiError', { message: errText(e) }), 'error')
    } finally {
      aiBusy = false
    }
  }

  // Застосування рекомендованої дії.
  async function applyAction() {
    const id = ctx.instanceId || ''
    const action = aiDiag?.aiResult?.recommendedAction
    if (!id || !action || aiBusy) return
    aiBusy = true
    aiActionState = 'working'
    aiActionResult = null
    try {
      let payload: AIRecommendedAction = action
      if (action.type === 'increase_ram') {
        payload = { ...action, suggestedRamMb: ramValue * 1024 }
      }
      const res = (await App.ApplyAIFix(id, payload)) ?? { success: false, message: '' }
      aiActionResult = res
      aiActionState = res.success ? 'done' : 'failed'
      if (res.success) {
        toast(res.message, 'success', 4000)
        if (action.type === 'increase_ram') {
          gameRunning = true
          gameCrashed = false
        }
      } else {
        toast(res.message, 'error', 5000)
      }
    } catch (e) {
      aiActionState = 'failed'
      toast(t('error.withMessage', { message: errText(e) }), 'error')
    } finally {
      aiBusy = false
    }
  }

  // «Скопіювати важливе» — копіює copyText від ШІ (машинного уривка більше немає).
  async function copyImportant() {
    const text = aiDiag?.aiResult?.copyText || ''
    if (!text) return
    try {
      await App.CopyToClipboard(text)
      copiedTick = Date.now()
      toast(t('console.copied'), 'success', 1600)
    } catch { /* ок */ }
  }

  // ── Контроли вікна (standalone) ──
  function winMin() { Window.Minimise() }
  function winMax() {
    if (maximized) { Window.UnMaximise(); maximized = false }
    else { Window.Maximise(); maximized = true }
  }
  // Закриття вікна консолі ховає ЛИШЕ це вікно (пакID жорстко прив'язаний
  // до збірки): інші відкриті вікна консолей інших збірок не зачіпаються.
  function winClose() {
    if (standalone) App.HideConsoleWindowFor(packID || '')
  }

  const actionType = $derived(aiDiag?.aiResult?.recommendedAction.type ?? 'none')
  const actionTarget = $derived(aiDiag?.aiResult?.recommendedAction.targetMod ?? '')
  const actionDep = $derived(aiDiag?.aiResult?.recommendedAction.dependencyName ?? '')

  function toggleAICollapse() {
    // Під час «thinking» згортати нічого (спінер маленький і сам по собі).
    if (aiState === 'thinking') return
    aiCollapsed = !aiCollapsed
  }
</script>

<div class="gc" class:standalone class:maximized={maximized} class:wrap={settings.consoleWrap}>

  {#if standalone}
    <!-- Окреме вікно консолі не має App.svelte, тож тости та модалки
         підтвердження (копіювання, експорт, попередження про дані при
         вивантаженні на mclo.gs) монтуємо тут. У вкладці головного вікна
         вони вже є глобально — не дублюємо. -->
    <Toasts />
    <ConfirmModal />
  {/if}

  <!-- ═══ Шапка ═══ -->
  <div class="gc-head" class:drag={standalone}>
    <div class="gc-ico"><i class="ti ti-terminal-2"></i></div>

    <!-- У ВКЛАДЦІ сторінки збірки (standalone=false) іконку/назву/версію
         збірки НЕ дублюємо — сторінка вже показує їх у своєму закріпленому
         тулбарі/банері. Ідентичність із кнопкою запуску потрібна лише
         ОКРЕМОМУ вікну консолі, у якого немає контексту сторінки. -->
    {#if standalone}
    <!-- Іконка збірки — 1:1 як у sidebar (.sb-pack .sp-ic): PNG через
         packAsset, інакше шрифт-іконка pack.icon на кольоровому фоні. -->
    <div class="gl-ico" class:has-icon={!!glIcon} style="--pcol:{glColor}">
      {#if glIcon}<img src={glIcon} alt="" loading="lazy" />{:else}<i class="ti {glIconFallback}"></i>{/if}
    </div>

    <div class="gl-info">
      <div class="gl-name">
        <span class="status-dot" class:stopped={!gameRunning} class:crashed={gameCrashed}></span>
        {ctx.name || t('console.noInstance')}
      </div>
      <div class="gl-sub">
        {#if ctx.loader}<span class="gl-tag">{ctx.loader}</span>{/if}
        {#if ctx.mcVersion}<span class="gl-ver">{ctx.mcVersion}</span>{/if}
        {#if gameRunning}
          <span class="ok">{t('console.gameRunning')}</span>
        {:else if gameCrashed}
          <span class="err">{t('console.gameCrashed', { code: lastExitCode })}</span>
        {/if}
      </div>
    </div>
    {/if}

    <!-- Перемикач акаунтів консолі — справа в шапці: меню відкривається
         ПІД кнопкою (position:absolute у .acc-wrap), як типовий select
         лаунчера, а не «де завгодно» по вікну. -->
    {#if ctx.instanceId && ctx.accounts && ctx.accounts.length > 0}
      <div class="acc-wrap">
        <span class="acc-sel" class:open={accMenuOpen} role="button" tabindex="0"
              onclick={(e: any) => { e.stopPropagation(); accMenuOpen = !accMenuOpen }}
              onkeydown={(e: any) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); e.stopPropagation(); accMenuOpen = !accMenuOpen } }}>
          <img class="acc-head" src={headForAcc(ctx.accountId)} alt="" />
          <span class="acc-sel-name">{ctx.accountName || ctx.accountId}</span>
          <i class="ti ti-chevron-down"></i>
        </span>
        {#if accMenuOpen}
          <div class="acc-menu" onclick={(e: any) => e.stopPropagation()}>
            {#each ctx.accounts as acc (acc.accountId)}
              <button class="acc-item" class:active={acc.accountId === ctx.accountId}
                      onclick={() => switchConsoleAccount(acc.accountId)}>
                <img class="acc-head" src={headForAcc(acc.accountId)} alt="" />
                <span class="acc-dot" class:run={acc.running}></span>
                <span class="acc-name">{acc.accountName || acc.accountId}</span>
                {#if acc.running}<i class="ti ti-player-play acc-play"></i>{/if}
              </button>
            {/each}
          </div>
        {/if}
      </div>
    {/if}

    <div class="gc-actions">
      {#if standalone}
        <button class="gbtn" title={t('win.minimize')} onclick={winMin}><i class="ti ti-minus"></i></button>
        <button class="gbtn" title={t('win.maximize')} onclick={winMax}><i class="ti {maximized ? 'ti-copy' : 'ti-square'}"></i></button>
        <button class="gbtn close" title={t('win.close')} onclick={winClose}><i class="ti ti-x"></i></button>
      {:else}
        <!-- Кнопка «відкрити вікно» у вкладці: перемикає вікно САМЕ ЦІЄЇ
             збірки (packID) — кілька збірок = кілька незалежних вікон. -->
        <button class="gbtn" title={t('console.openWindow')} onclick={() => App.ToggleConsoleWindowFor(packID || '')}><i class="ti ti-arrows-maximize"></i></button>
      {/if}
    </div>
  </div>

  <!-- ═══ Панель запуску (гра не запущена) ═══
       Стан-залежна: консоль знає РЕАЛЬНИЙ стан збірки (ctx.status з бекенду)
       і показує правильну дію — Завантажити (не встановлена), Оновити (є
       нова версія), Запустити (готова) — або живий прогрес під час качки/
       запуску. Жодного фіктивного «Запустити» для збірки без файлів. -->
  {#if !gameRunning && standalone}
    <div class="gc-launch">
      <div class="gl-prog">
        {#if consoleAction === 'launching' && launchProgress}
          <div class="gl-sub">
            <i class="ti ti-loader spin"></i>
            {launchProgress.message || launchStageLabel(launchProgress.stage)}
            {#if launchProgress.percent != null && launchProgress.percent >= 0}
              <b class="gl-pct">{launchProgress.percent}%</b>
            {/if}
          </div>
          <div class="gl-track"><div class="gl-fill" style="width:{Math.max(0, launchProgress.percent ?? 0)}%"></div></div>
        {:else if consoleAction === 'syncing' && ctx.progress}
          <div class="gl-sub">
            <i class="ti ti-loader spin"></i>
            {ctx.progress.currentFile || syncStageLabel(ctx.progress.stage)}
            <b class="gl-pct">{ctx.progress.percent}%</b>
          </div>
          <div class="gl-track"><div class="gl-fill" style="width:{Math.max(0, ctx.progress.percent)}%"></div></div>
          <div class="gl-sub dim">
            {t('download.files', { done: ctx.progress.filesDone, total: ctx.progress.filesTotal })}
            · {(ctx.progress.speedBps / 1024 / 1024).toFixed(1)} {t('download.speedUnit')}
            {#if ctx.progress.noInternet}<span class="gl-nonet"><i class="ti ti-wifi-off"></i> {t('pack.noInternet')}</span>{/if}
          </div>
        {:else}
          <div class="gl-sub">{statusLabel(ctx.status)}</div>
        {/if}
        {#if launchProgress?.warning}
          <!-- Офлайн-попередження запуску: гра стартує без мережі, показуємо
               зрозумілий напис замість тиші (та сама подія launch:progress,
               що в модалці головного вікна). -->
          <div class="gl-warn"><i class="ti ti-wifi-off"></i> {launchProgress.warning}</div>
        {/if}
      </div>
      {#if consoleAction === 'syncing'}
        <div class="gl-act mono">{ctx.progress?.percent ?? 0}%</div>
        <!-- Раніше тут був лише текст «NN%» без жодної дії — під час
             завантаження/оновлення файлів збірки не було чим зупинити
             консоль, якщо процес завис або користувач передумав. Бекенд
             (App.CancelDownload) вже вміє це робити, кнопки просто не було. -->
        <button class="btn btn-ghost btn-sm" onclick={cancelSyncClick} disabled={cancellingInstall}>
          {#if cancellingInstall}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-player-stop"></i>{/if}
          {t('console.cancelDownload')}
        </button>
      {:else if consoleAction === 'launching'}
        <!-- Так само: стадія запуску (checking/java/loader…) мала кнопку-
             заглушку з disabled, тому лаунчер здавався «завислим» без
             жодного виходу — навіть якщо крок реально зациклився чи мережа
             пропала. App.CancelGameInstall зупиняє встановлення компонентів
             (Java/лоадер/асети) і LaunchInstance коректно завершується. -->
        <button class="btn btn-ghost btn-sm" onclick={cancelLaunchClick} disabled={cancellingInstall}>
          {#if cancellingInstall}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-player-stop"></i>{/if}
          {t('console.cancelInstall')}
        </button>
      {:else}
        <button class="btn {consoleAction === 'play' ? 'btn-primary' : 'btn-violet'}" onclick={primaryConsoleAction} disabled={consoleAction === 'disabled' || !ctx.instanceId}>
          {#if consoleAction === 'download'}
            <i class="ti ti-download"></i> {t('console.download')}
          {:else if consoleAction === 'update'}
            <i class="ti ti-refresh"></i> {t('console.update')}
          {:else}
            <i class="ti ti-player-play"></i> {t('console.launch')}
          {/if}
        </button>
      {/if}
    </div>
  {/if}

  <!-- ═══ Панель інструментів: фільтри + пошук + дії ═══ -->
  <!-- ═══ Вкладки: «Консоль» (живий лог) / «Файл краху» (сирий
       crash-report чи вирізка WARN-ERROR, той самий текст, що бачила ШІ)
       ═══ Друга вкладка з'являється лише коли справді є що показати. -->
  {#if aiDiag?.rawLogText}
    <div class="gc-tabs">
      <button class="gc-tab" class:active={consoleTab === 'console'} onclick={() => consoleTab = 'console'}>
        <i class="ti ti-terminal-2"></i> {t('console.tab.console')}
      </button>
      <button class="gc-tab" class:active={consoleTab === 'crashfile'} onclick={() => consoleTab = 'crashfile'}>
        <i class="ti ti-file-alert"></i> {t('console.tab.crashFile')}
        {#if gameCrashed && aiState !== 'idle'}<span class="gc-tab-dot"></span>{/if}
      </button>
    </div>
  {/if}

  {#if consoleTab === 'console' || !aiDiag?.rawLogText}
  <div class="gc-tools">
    <div class="gc-seg" role="tablist">
      <button class="fchip" class:active={filter === 'all'} onclick={() => setFilter('all')}>{t('console.filter.all')}</button>
      <button class="fchip" class:active={filter === 'info'} onclick={() => setFilter('info')}>{t('console.filter.info')}</button>
      <button class="fchip fc-warn" class:active={filter === 'warn'} onclick={() => setFilter('warn')}>{t('console.filter.warn')}</button>
      <button class="fchip fc-err" class:active={filter === 'error'} onclick={() => setFilter('error')}>{t('console.filter.error')}</button>
    </div>

    <div class="gc-search">
      <i class="ti ti-search"></i>
      <input bind:value={search} placeholder={t('console.searchPlaceholder')} />
      {#if search}
        <i class="ti ti-x gc-search-clear" onclick={clearSearch}></i>
      {/if}
    </div>

    <div class="gc-tool-right">
      <button class="tbtn" title={t('console.copyLog')} onclick={copyLog}>
        {#if copiedTick > 0 && Date.now() - copiedTick < 1500}<i class="ti ti-check" style="color:var(--green)"></i>{:else}<i class="ti ti-copy"></i>{/if}
      </button>
      <button class="tbtn" title={t('console.saveLog')} onclick={saveLog}><i class="ti ti-download"></i></button>
      <button class="tbtn" title={t('console.shareLog')} onclick={shareLog}><i class="ti ti-cloud-upload"></i></button>
      <button class="tbtn" title={t('console.clear')} onclick={clearLog}><i class="ti ti-trash"></i></button>
    </div>
  </div>

  <!-- ═══ Термінал (віртуалізований; при увімкненому перенесенні рядків
       рендеримо всі рядки, бо висота стає змінною) ═══ -->
  <div class="gc-term" bind:this={termEl} onscroll={onScroll} style="font-size:{consoleFontSize}px;font-family:{consoleFontStack};--cons-line:{LINE_H}px">
    {#if filtered.length === 0}
      <div class="gc-empty">
        {search ? t('console.noResults', { q: search }) : t('console.hint')}
      </div>
    {:else if settings.consoleWrap}
      {#each filtered as line (line.id)}
        <div class="line {line.level}" style="color:{levelColor(line.level)};font-family:{lineFont(line.level)}">
          <span class="l-ts">{line.time}</span>
          <span class="l-lvl">{levelLabel(line.level)}</span>
          <span class="l-msg">{@html highlight(line.text)}</span>
  </div>
      {/each}
    {:else}
      <div style="height:{startIdx * LINE_H}px;flex-shrink:0"></div>
      {#each visible as line (line.id)}
        <div class="line {line.level}" style="height:{LINE_H}px;color:{levelColor(line.level)};font-family:{lineFont(line.level)}">
          <span class="l-ts">{line.time}</span>
          <span class="l-lvl">{levelLabel(line.level)}</span>
          <span class="l-msg">{@html highlight(line.text)}</span>
  </div>
      {/each}
      <div style="height:{(filtered.length - endIdx) * LINE_H}px;flex-shrink:0"></div>
    {/if}
  </div>
  {:else}
  <!-- ═══ Вкладка «Файл краху» — сирий текст у моноширинному стилі
       консолі: та сама якісна читаність, що звичайний термінал, але без
       фільтрів/пошуку/віртуалізації (текст статичний, один раз
       завантажений разом із aiDiag, не потоковий). Формат подібний до
       raw crash-report Minecraft (Mojang-стиль: "---- Minecraft Crash
       Report ----"), тому просте моноширинне forматування читається
       людині так само природно, як і нейромережею при повторному
       вставлянні кудись іще. ═══ -->
  <div class="gc-tools">
    <div class="gc-crashfile-hint">
      <i class="ti ti-info-circle"></i>
      {aiDiag?.crashReportFile ? t('console.crashFile.fromReport') : t('console.crashFile.fromLog')}
    </div>
    <div class="gc-tool-right">
      <button class="tbtn" title={t('console.copyLog')} onclick={copyCrashLog}>
        {#if crashCopiedTick > 0 && Date.now() - crashCopiedTick < 1500}<i class="ti ti-check" style="color:var(--green)"></i>{:else}<i class="ti ti-copy"></i>{/if}
      </button>
      <button class="tbtn" title={t('console.saveLog')} onclick={saveCrashLog}><i class="ti ti-download"></i></button>
    </div>
  </div>
  <div class="gc-term gc-crashfile" style="font-size:{consoleFontSize}px;font-family:{consoleFontStack}">
    <pre class="gc-crashfile-pre">{aiDiag?.rawLogText ?? ''}</pre>
  </div>
  {/if}

  <!-- ═══ AI-панель ═══
       Видно ЛИШЕ при краші (або поки йде/показаний аналіз): у звичайному
       стані гри віджет не займає місце — він сам з'являється при краші
       і сам запускає діагностику (авто-активація, див. game:exit). -->
  {#snippet aiActions()}
    {#if aiState === 'basic'}
      <button class="ai-act ghost" onclick={analyzeCrash} disabled={aiBusy}><i class="ti ti-refresh"></i> {t('console.ai.retry')}</button>
      <button class="ai-act ghost" onclick={() => { aiState = 'idle'; aiDismissed = true }}><i class="ti ti-x"></i> {t('console.ai.dismiss')}</button>
    {:else if aiState === 'done' && aiDiag?.aiResult}
      {@const r = aiDiag.aiResult}
      {#if r.actionable && actionType === 'disable_mod' && actionTarget}
        <button class="ai-act primary" onclick={applyAction} disabled={aiBusy}>
          <i class="ti ti-power"></i> {t('console.ai.disableMod', { mod: actionTarget })}
        </button>
      {:else if r.actionable && actionType === 'download_dependency' && actionDep}
        <button class="ai-act primary" onclick={applyAction} disabled={aiBusy}>
          <i class="ti ti-download"></i> {t('console.ai.downloadDep', { dep: actionDep })}
        </button>
      {:else if r.actionable && actionType === 'increase_ram'}
        <button class="ai-act primary" onclick={applyAction} disabled={aiBusy}>
          <i class="ti ti-player-play"></i> {t('console.ai.applyAndRelaunch')}
        </button>
      {:else if r.actionable && actionType === 'repair'}
        <button class="ai-act repair" onclick={applyAction} disabled={aiBusy}>
          <i class="ti ti-tools"></i> {t('console.ai.repair')}
        </button>
      {/if}

      <!-- Після успішної дії — «Запустити знову» -->
      {#if aiActionState === 'done' && actionType !== 'increase_ram'}
        <button class="ai-act success" onclick={launchGame}><i class="ti ti-player-play"></i> {t('console.ai.relaunch')}</button>
      {/if}
      {#if aiActionState === 'failed' && aiActionResult?.projectUrl}
        <button class="ai-act ghost" onclick={() => App.OpenExternal(aiActionResult!.projectUrl)}>
          <i class="ti ti-external-link"></i> {t('console.ai.openPage')}
        </button>
      {/if}
      {#if aiDiag?.aiResult?.copyText}
        <button class="ai-act ghost" onclick={copyImportant}><i class="ti ti-copy"></i> {t('console.ai.copyImportant')}</button>
      {/if}
    {/if}
  {/snippet}

  {#if !aiDismissed && (gameCrashed || aiState !== 'idle')}
    <div class="gc-ai" class:collapsed={aiCollapsed}>
      <div class="ai-head" role="button" tabindex="0" onclick={toggleAICollapse}
           onkeydown={(e: any) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); toggleAICollapse() } }}>
        <div class="ai-ico"><i class="ti ti-sparkles"></i></div>
        <div class="ai-title">{t('console.ai.title')}</div>
        <div class="ai-badge">beta</div>
        {#if aiState === 'done' || aiState === 'basic'}
          <button class="ai-collapse" title={aiCollapsed ? t('console.ai.expand') : t('console.ai.collapse')}
                  aria-label={aiCollapsed ? t('console.ai.expand') : t('console.ai.collapse')}
                  onclick={(e: any) => { e.stopPropagation(); toggleAICollapse() }}>
            <i class="ti {aiCollapsed ? 'ti-chevron-down' : 'ti-chevron-up'}"></i>
          </button>
        {/if}
      </div>

      {#if aiState === 'thinking'}
        <div class="ai-thinking">
          <div class="ai-spinner"></div>           <span>{t('console.ai.thinking')}</span>
  </div>

      {:else if aiCollapsed}
        <div class="ai-collapsed-hint">{t('console.ai.collapsedHint')}</div>

      {:else if aiState === 'basic' || (aiState === 'done' && aiDiag?.aiResult)}
        <div class="ai-body">
            {#if aiState === 'basic'}
              <div class="ai-cause">{t('console.ai.basicNote')}</div>
              {#if aiDiag?.aiError}
                <div class="ai-note"><i class="ti ti-alert-triangle"></i> {aiDiag.aiError}</div>
              {/if}
            {:else}
              {@const r = aiDiag!.aiResult!}
              <div class="ai-cause">{r.cause}</div>
              <div class="ai-expl">{r.explanation}</div>
              {#if r.facts?.length}
                <ul class="ai-facts">
                  {#each r.facts as f}<li>{f}</li>{/each}
                </ul>
              {/if}

              <!-- increase_ram: повзунок RAM -->
              {#if actionType === 'increase_ram'}
                <div class="ram-adjust">
                  <div class="ram-label">{t('console.ai.ramLabel')}</div>
                  <input type="range" class="ram-input" min="1" max="16" step="1" bind:value={ramValue} />
                  <div class="ram-value">{ramValue} GB</div>
                </div>
              {/if}
            {/if}
        </div>

        <!-- Кнопки дій завжди видимі -->
        <div class="ai-actions">{@render aiActions()}</div>

        {#if aiState === 'done'}
          {#if aiActionState === 'working'}
            <div class="ai-working"><div class="ai-spinner sm"></div> <span>{t('console.ai.working')}</span></div>
          {/if}
          {#if aiActionResult}
            <div class="ai-result {aiActionState === 'done' ? 'ok' : 'fail'}">{aiActionResult.message}</div>
          {/if}
          {#if aiDiag?.aiResult}
            <div class="ai-model">{t('console.ai.model', { model: aiDiag.aiResult.model || 'gemini' })}</div>
          {/if}
        {/if}

      {:else}
        <div class="ai-hint">
          {#if !settings.consoleAI}
            <div class="ai-disabled"><i class="ti ti-sparkles-off"></i> {t('console.ai.disabled')}</div>
          {:else}
            {t('console.ai.hint')}
            <button class="btn btn-violet btn-sm" onclick={analyzeCrash} disabled={aiBusy || !ctx.instanceId}>
              <i class="ti ti-wand"></i> {t('console.ai.analyze')}
            </button>
          {/if}
        </div>
      {/if}
    </div>
  {/if}

  <!-- ═══ Футер ═══ -->
  <div class="gc-footer">
    <div class="gc-stats">
      <span><i class="ti ti-file-text"></i> {lines.length} {t('console.lines')}</span>
      <span><i class="ti ti-ruler-2"></i> {lines.length} / {Math.max(100, settings.consoleMaxLines || 10000)}</span>
      <span class={errorCount > 0 ? 'err' : ''}><i class="ti ti-alert-triangle"></i> {errorCount} {t('console.errors')}</span>
    </div>
    {#if gameRunning && standalone}
      <div class="gc-footer-actions">
        <button class="fbtn stop" onclick={stopGame}><i class="ti ti-player-stop"></i> {t('console.stop')}</button>
      </div>
    {/if}
  </div>
</div>

<style>
  .gc {
    display: flex; flex-direction: column;
    background: var(--bg-window);
    border: 1px solid var(--border);
    border-radius: var(--r-lg);
    overflow: hidden;
    height: 100%;
    flex: 1;
  }
  /* Повноекранне окреме вікно: без чорного фону — градієнт лаунчера */
  .gc.standalone {
    border: none; border-radius: 0;
    background:
      radial-gradient(900px 500px at 12% -5%, rgba(var(--orange-rgb), .06), transparent 60%),
      radial-gradient(900px 600px at 100% 110%, rgba(123, 77, 255, .07), transparent 55%),
      var(--bg-app);
  }
  .gc.maximized { border-radius: 0; }

  .gc-head {
    display: flex; align-items: center; gap: 10px;
    padding: 9px 14px;
    border-bottom: 1px solid var(--border);
    background: color-mix(in srgb, var(--bg-window) 82%, transparent);
    backdrop-filter: blur(10px);
    flex-shrink: 0;
    /* backdrop-filter створює власний stacking context — без явного z-index
       внутрішні absolute-елементи (меню акаунтів .acc-menu) малювалися б
       ПІД .gc-term. Піднімаємо шапку над терміналом. */
    position: relative; z-index: 10;
  }
  /* тонка акцентна лінія під шапкою — той самий прийом, що в шапці лаунчера */
  .gc-head::after {
    content: ''; position: absolute; left: 0; right: 0; bottom: -1px; height: 1px; pointer-events: none;
    background: linear-gradient(90deg, transparent, rgba(var(--orange-rgb), .55), transparent);
  }
  .gc-head.drag { --wails-draggable: drag; }
  .gc-head .gc-actions, .gc-head button, .gc-head .acc-sel, .gc-head .gl-ico { --wails-draggable: no-drag; }

  .gc-ico {
    width: 26px; height: 26px; border-radius: 7px;
    background: rgba(var(--orange-rgb), .13); color: var(--orange);
    display: flex; align-items: center; justify-content: center; font-size: 14px; flex-shrink: 0;
  }
  /* Іконка збірки в шапці — 1:1 як .sp-ic у sidebar: PNG або шрифт-іконка
     pack.icon на кольоровому фоні (--pcol). Розмір 28px той самий, що в
     sidebar (.sb-pack .sp-ic), щоб консоль виглядала «тією самою» збіркою. */
  .gl-ico {
    width: 28px; height: 28px; min-width: 28px; border-radius: 7px;
    background: color-mix(in srgb, var(--pcol, var(--orange)) 16%, var(--bg-input-hi));
    display: flex; align-items: center; justify-content: center;
    font-size: 13px; color: var(--pcol, var(--orange)); position: relative; overflow: hidden;
  }
  .gl-ico.has-icon { background: transparent; }
  .gl-ico img { width: 100%; height: 100%; object-fit: cover; border-radius: 7px; }
  .gl-ico i { display: block; }
  .gl-info { flex: 1; min-width: 0; }
  .gl-name { font-size: 12px; font-weight: 700; display: flex; align-items: center; gap: 8px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; min-width: 0; }
  .gl-sub { font-size: 10.5px; color: var(--text-mute); margin-top: 1px; display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
  .gl-sub .ok { color: var(--green); }
  .gl-sub .err { color: var(--red); }
  /* Лоадер/версія збірки під назвою — «пігулка» лоадера + моно-версія. */
  .gl-tag {
    padding: 1px 7px; border-radius: 6px; font-size: 9px; font-weight: 800;
    background: rgba(var(--orange-rgb), .13); color: var(--orange);
    text-transform: uppercase; letter-spacing: .4px; flex-shrink: 0;
  }
  .gl-ver { font-family: var(--font-mono); font-size: 10px; color: var(--text-dim); flex-shrink: 0; }

  /* Перемикач акаунтів консолі — типовий select лаунчера: «пігулка» з
     головою гравця + меню ПІД кнопкою (position:absolute у .acc-wrap,
     right:0 — прив'язане до правої межі кнопки). Раніше було fixed
     top:52px right:16px — меню «пливло» по вікну, а не відкривалось
     під кнопкою. */
  .acc-wrap { position: relative; flex-shrink: 0; }
  .acc-sel {
    display: inline-flex; align-items: center; gap: 6px; cursor: pointer;
    background: var(--bg-panel); border: 1px solid var(--border); border-radius: 9px;
    padding: 3px 9px 3px 4px; font-size: 11px; color: var(--text);
    transition: border-color .15s, background .15s, box-shadow .15s;
    max-width: 200px; white-space: nowrap; flex-shrink: 0;
  }
  .acc-sel:hover { border-color: var(--border-hi); background: var(--bg-input); }
  .acc-sel .acc-head {
    width: 20px; height: 20px; border-radius: 6px; object-fit: cover;
    background: var(--bg-input-hi); flex-shrink: 0;
  }
  .acc-sel .acc-sel-name { overflow: hidden; text-overflow: ellipsis; font-weight: 600; }
  .acc-sel > i:last-child { font-size: 10px; color: var(--text-dim); transition: transform .2s; }
  .acc-sel.open > i:last-child { transform: rotate(180deg); }
  .acc-menu {
    position: absolute; z-index: 20; top: calc(100% + 6px); right: 0; min-width: 210px;
    background: var(--bg-window); border: 1px solid var(--border-hi); border-radius: var(--r-md);
    box-shadow: 0 18px 46px rgba(0,0,0,.55); padding: 5px; display: flex; flex-direction: column; gap: 2px;
    /* Не вище за висоту вікна: у низьких вікнах .gc (overflow:hidden)
       зрізав би меню знизу без можливості дістатись до останніх акаунтів. */
    max-height: min(280px, calc(100vh - 120px)); overflow-y: auto;
    animation: acc-menu-in .16s cubic-bezier(.22,1.2,.36,1) both;
    transform-origin: top right;
  }
  @keyframes acc-menu-in {
    from { opacity: 0; transform: translateY(-6px) scale(.97); }
    to   { opacity: 1; transform: translateY(0) scale(1); }
  }
  .acc-item {
    display: flex; align-items: center; gap: 9px; width: 100%; text-align: left;
    background: transparent; border: none; border-radius: 8px; padding: 7px 9px; cursor: pointer;
    color: var(--text); font-size: 12px; font-weight: 600; font-family: inherit;
    transition: background .15s; white-space: nowrap;
  }
  .acc-item:hover { background: var(--hover); }
  .acc-item.active { background: rgba(var(--orange-rgb), .12); color: var(--orange); }
  .acc-item .acc-head {
    width: 26px; height: 26px; border-radius: 8px; object-fit: cover;
    background: var(--bg-input-hi); flex-shrink: 0;
  }
  .acc-dot { width: 7px; height: 7px; border-radius: 50%; background: var(--text-dim); flex-shrink: 0; }
  .acc-dot.run { background: var(--green); box-shadow: 0 0 6px rgba(16,185,129,.6); }
  .acc-name { flex: 1; overflow: hidden; text-overflow: ellipsis; }
  .acc-play { color: var(--green); font-size: 11px; }

  .status-dot {
    width: 8px; height: 8px; border-radius: 50%; flex-shrink: 0;
    background: var(--green); box-shadow: 0 0 8px rgba(16,185,129,.6);
  }
  .status-dot.stopped { background: var(--text-mute); box-shadow: none; }
  .status-dot.crashed { background: var(--red); box-shadow: 0 0 8px rgba(239,68,68,.6); }

  .gc-actions { display: flex; gap: 6px; flex-shrink: 0; }
  .gbtn {
    width: 30px; height: 30px; border-radius: var(--r-sm);
    background: var(--bg-panel); border: 1px solid var(--border);
    color: var(--text-mute); display: flex; align-items: center; justify-content: center;
    font-size: 14px; cursor: pointer; transition: .15s;
  }
  .gbtn:hover { color: #fff; border-color: var(--border-hi); background: var(--bg-input-hi); }
  .gbtn:active { transform: scale(.94); }
  .gbtn.close:hover { color: var(--red); border-color: rgba(239,68,68,.35); background: rgba(239,68,68,.1); }

  /* Панель запуску — прогрес + стан + кнопка дії. Іконка та назва збірки
     тепер у ШАПЦІ (.gl-ico/.gl-info), тут лишаються тільки прогрес-бари
     і кнопки — щоб дубль не «з'їдав» простір терміналу. */
  .gc-launch {
    padding: 6px 12px; display: flex; align-items: center; gap: 12px;
    border-bottom: 1px solid var(--border); flex-shrink: 0;
    background: rgba(var(--orange-rgb), .05);
  }
  .gl-prog { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
  .gl-prog .gl-sub { font-size: 10.5px; color: var(--text-mute); display: flex; align-items: center; gap: 6px; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .gl-sub .ti-loader { color: var(--orange); }
  .gl-sub.dim { font-size: 9.5px; font-family: var(--font-mono); }
  .gl-pct { color: var(--orange); font-family: var(--font-mono); font-size: 11px; }
  .gl-nonet { color: var(--red); display: inline-flex; align-items: center; gap: 4px; }
  .gl-warn {
    margin-top: 4px; font-size: 10px; line-height: 1.4; color: var(--orange);
    background: rgba(var(--orange-rgb), .08); border: 1px solid rgba(var(--orange-rgb), .25);
    border-radius: 6px; padding: 5px 8px; display: flex; align-items: flex-start; gap: 6px;
    max-width: 380px;
  }
  .gl-warn i { flex-shrink: 0; margin-top: 1px; }
  .gl-track {
    height: 4px; border-radius: 2px; margin-top: 2px;
    background: var(--bg-input-hi); overflow: hidden; max-width: 380px;
  }
  .gl-fill {
    height: 100%; border-radius: 2px;
    background: linear-gradient(90deg, var(--orange), var(--orange-2));
    transition: width .2s ease;
  }
  .gl-act {
    font-size: 13px; font-weight: 800; color: var(--orange);
    background: rgba(var(--orange-rgb), .12); border: 1px solid rgba(var(--orange-rgb), .3);
    padding: 2px 10px; border-radius: 18px; flex-shrink: 0;
  }

  /* Інструменти */
  .gc-tools {
    display: flex; gap: 6px; padding: 9px 14px;
    border-bottom: 1px solid var(--border); flex-wrap: wrap; align-items: center;
    background: color-mix(in srgb, var(--bg-window) 92%, transparent);
    flex-shrink: 0;
  }
  /* Сегментований перемикач фільтрів (єдина «капсула», як перемикачі лаунчера). */
  .gc-seg {
    display: flex; gap: 2px; padding: 3px; border-radius: 12px;
    background: var(--bg-input); border: 1px solid var(--border);
  }
  .fchip {
    padding: 5px 12px; border-radius: 9px; font-size: 10.5px; font-weight: 700;
    background: transparent; border: none; color: var(--text-mute);
    cursor: pointer; transition: background-color .15s, color .15s, box-shadow .15s; display: flex; align-items: center; gap: 6px;
  }
  .fchip:hover { color: var(--text); background: color-mix(in srgb, var(--text) 6%, transparent); }
  /* Фільтри нейтральні (без кольорів): на OLED кольорові пігулки різали око. */
  .fchip.active { background: var(--hover-strong); color: var(--text); box-shadow: 0 1px 6px rgba(0,0,0,.28), inset 0 0 0 1px var(--border-hi); }
  .fchip.fc-warn.active { color: #ffd166; }
  .fchip.fc-err.active { color: #ff6b6b; }

  .gc-search {
    display: flex; align-items: center; gap: 6px;
    background: var(--bg-input); border: 1px solid var(--border); border-radius: 12px;
    padding: 6px 12px; min-width: 150px; flex: 1; max-width: 280px;
    transition: border-color .15s, box-shadow .15s;
  }
  .gc-search:focus-within { border-color: rgba(var(--orange-rgb), .55); box-shadow: 0 0 0 3px rgba(var(--orange-rgb), .12); }
  .gc-search i { font-size: 13px; color: var(--text-mute); }
  .gc-search input { background: none; border: none; outline: none; color: #fff; font-size: 11px; font-family: inherit; width: 100%; }
  .gc-search input::placeholder { color: var(--text-dim); }
  .gc-search-clear { cursor: pointer; }

  .gc-tool-right { margin-left: auto; display: flex; gap: 5px; }
  .tbtn {
    width: 27px; height: 27px; border-radius: var(--r-sm);
    background: var(--bg-panel); border: 1px solid var(--border);
    color: var(--text-mute); display: flex; align-items: center; justify-content: center;
    font-size: 13px; cursor: pointer; transition: .15s;
  }
  .tbtn:hover { color: #fff; border-color: var(--border-hi); }

  /* Вкладки «Консоль» / «Файл краху» — той самий візуальний словник, що
     .fchip (закруглені пігулки), але окрема смужка над .gc-tools, бо це
     перемикання ЦІЛОГО вмісту нижче, а не фільтр видимих рядків. */
  .gc-tabs {
    display: flex; gap: 6px; padding: 8px 14px 0;
    border-bottom: 1px solid var(--border); flex-shrink: 0;
    background: color-mix(in srgb, var(--bg-window) 92%, transparent);
  }
  .gc-tab {
    display: flex; align-items: center; gap: 6px;
    padding: 7px 14px; border-radius: var(--r-sm) var(--r-sm) 0 0;
    font-size: 11px; font-weight: 600; color: var(--text-mute);
    background: transparent; border: 1px solid transparent; border-bottom: none;
    cursor: pointer; transition: .15s; position: relative;
  }
  .gc-tab:hover { color: #fff; }
  .gc-tab.active {
    color: #fff; background: var(--bg-panel);
    border-color: var(--border); border-bottom: 1px solid var(--bg-panel);
    margin-bottom: -1px;
  }
  .gc-tab-dot {
    width: 6px; height: 6px; border-radius: 50%; background: var(--red);
    box-shadow: 0 0 0 2px color-mix(in srgb, var(--red) 25%, transparent);
  }

  /* Підказка джерела на вкладці «Файл краху»: crash-report файл цілком
     vs вирізка з живого логу (той самий факт, що бачила ШІ). */
  .gc-crashfile-hint {
    display: flex; align-items: center; gap: 7px;
    font-size: 11px; color: var(--text-mute);
  }
  .gc-crashfile-hint i { font-size: 13px; color: var(--orange); }

  /* Вкладка «Файл краху»: <pre> замість віртуалізованих .line — текст
     статичний (один раз завантажений з aiDiag), не потоковий, тому
     проста моноширинна верстка без math віртуалізації. white-space: pre
     зберігає оригінальні відступи crash-report (Mojang-стиль:
     "---- Minecraft Crash Report ----", вирівняні "at ..." стек-трейси)
     — саме так, як їх звикли читати і людина, і нейромережа. */
  .gc-crashfile { padding: 14px; }
  .gc-crashfile-pre {
    margin: 0; white-space: pre; color: var(--text-mute);
    line-height: 1.55; user-select: text;
  }

  /* Термінал: вертикальний скрол для віртуалізованих рядків + горизон-
     тальний для дуже довгих рядків (краш-дамп 10 000 символів). */
  .gc-term {
    flex: 1; min-height: 0; overflow-y: auto; overflow-x: auto;
    padding: 10px 14px;
    /* Шрифт/розмір приходять inline з налаштувань консолі (consoleFontSize /
       consoleFontFamily); тут лише дефолт на випадок відсутності inline. */
    font-family: var(--font-mono); font-size: 12px;
    /* «Картка» терміналу: плаваюча панель із рамкою та м'якою тінню замість
       плоского блоку на всю ширину. */
    margin: 10px 12px 12px; border-radius: 14px;
    border: 1px solid var(--border);
    background: color-mix(in srgb, var(--bg-app) 72%, var(--bg-panel));
    box-shadow: inset 0 1px 0 rgba(255,255,255,.035), 0 10px 30px rgba(0,0,0,.18);
    position: relative;
  }
  .gc-term::-webkit-scrollbar { width: 9px; }
  .gc-term::-webkit-scrollbar-track { background: transparent; }
  .gc-term::-webkit-scrollbar-thumb { background: var(--hover-strong); border-radius: 6px; }
  .gc-term::-webkit-scrollbar-thumb:hover { background: var(--border-hi); }

  /* Кожен рядок логу = РІВНО один віртуальний ряд (22px), тому:
     - рядки НЕ переносяться (white-space: pre) — довгі рядки (краш-дамп
       на 10 000 символів) ідуть горизонтальним скролом контейнера, і
       математика віртуалізації (startIdx * LINE_H) завжди точна;
     - жодних заминок: віртуалізуються тільки видимі рядки, величезний
       рядок — це просто один широкий DOM-елемент. */
  .line {
    display: flex; gap: 10px; align-items: baseline;
    white-space: pre;
    /* Висота рядка = --cons-line (з налаштування розміру шрифту). */
    line-height: var(--cons-line, 22px);
    height: var(--cons-line, 22px);
    user-select: text;
    -webkit-user-select: text;
    /* Сучасний рядок: м'який hover, відступи всередині «картки» терміналу. */
    margin: 0 -8px; padding: 0 8px; border-radius: 6px;
    transition: background-color .12s ease;
  }
  .line:hover { background: color-mix(in srgb, var(--text) 5%, transparent); }
  .line .l-ts { color: var(--text-dim); flex-shrink: 0; user-select: none; -webkit-user-select: none; font-size: 10.5px; opacity: .8; font-variant-numeric: tabular-nums; }
  /* Рівень — кольоровий «бейдж» (колір береться з рядка через currentColor,
     тож налаштування кольорів консолі продовжують працювати). */
  .line .l-lvl {
    flex-shrink: 0; width: 40px; text-align: center; font-weight: 800; font-size: 9.5px; letter-spacing: .4px;
    line-height: 16px; height: 16px; border-radius: 5px; align-self: center;
    background: color-mix(in srgb, currentColor 14%, transparent);
    box-shadow: inset 0 0 0 1px color-mix(in srgb, currentColor 22%, transparent);
  }
  .line.warn { background: color-mix(in srgb, #ffd166 6%, transparent); box-shadow: inset 2px 0 0 color-mix(in srgb, #ffd166 70%, transparent); }
  .line.error { background: color-mix(in srgb, #ff6b6b 9%, transparent); box-shadow: inset 2px 0 0 color-mix(in srgb, #ff6b6b 80%, transparent); }
  .line.warn:hover, .line.error:hover { filter: brightness(1.15); }
  .line .l-msg { flex: 1; min-width: 0; }
  .line .l-msg :global(mark) { background: rgba(var(--orange-rgb), .4); color: #fff; border-radius: 2px; }

  /* Перенесення довгих рядків (налаштування consoleWrap, як у Prism):
     довгі рядки ломаються на нові рядки замість горизонтального скролу. */
  .gc.wrap .line {
    white-space: pre-wrap;
    word-break: break-word;
    height: auto;
    min-height: var(--cons-line, 22px);
    line-height: var(--cons-line, 22px);
    align-items: flex-start;
  }
  .gc.wrap .line .l-lvl { align-self: flex-start; margin-top: calc((var(--cons-line, 22px) - 16px) / 2); }

  .line.culprit {
    background: rgba(239, 68, 68, .16);
    border-left: 2px solid var(--red);
    margin-left: -14px; padding-left: 12px; margin-right: -14px; padding-right: 14px;
    animation: culpritFlash 1.1s ease-in-out 3;
  }
  @keyframes culpritFlash {
    0%, 100% { background: rgba(239, 68, 68, .16); }
    50% { background: rgba(239, 68, 68, .36); }
  }

  .gc-empty { color: var(--text-dim); font-size: 12px; padding: 8px 2px; font-family: var(--font-mono); }

  /* AI-панель */
  .gc-ai {
    margin: 0 14px 12px; flex-shrink: 0;
    background: rgba(123, 77, 255, .07);
    border: 1px solid rgba(123, 77, 255, .25);
    border-radius: var(--r-md);
    padding: 12px 14px;
    max-height: 320px; overflow-y: auto;
    transition: max-height .15s;
  }
  /* Згорнута AI-панель не має тіла, тож обмеження висоти/скролу не потрібні —
     лишається лише шапка + один рядок-підказка. */
  .gc-ai.collapsed { overflow: visible; max-height: none; }
  .ai-head { display: flex; align-items: center; gap: 9px; margin-bottom: 8px; cursor: pointer; user-select: none; }
  .ai-ico {
    width: 24px; height: 24px; border-radius: 7px;
    background: rgba(123, 77, 255, .18); color: var(--violet-l);
    display: flex; align-items: center; justify-content: center; font-size: 13px; flex-shrink: 0;
  }
  .ai-title { font-size: 12.5px; font-weight: 700; color: var(--violet-l); }
  .ai-badge {
    font-size: 9.5px; font-weight: 700; color: var(--violet-l);
    background: rgba(123, 77, 255, .16); padding: 2px 8px; border-radius: 20px; letter-spacing: .3px;
  }
  .ai-fold { margin-left: auto; }
  /* Кнопка «Згорнути/Розгорнути» в шапці AI-панелі. */
  .ai-collapse {
    margin-left: auto; display: flex; align-items: center; justify-content: center;
    width: 22px; height: 22px; border-radius: 6px; border: none; cursor: pointer;
    background: rgba(123, 77, 255, .12); color: var(--violet-l); font-size: 13px;
    transition: .15s;
  }
  .ai-collapse:hover { background: rgba(123, 77, 255, .26); }
  .ai-collapsed-hint {
    font-size: 11.5px; color: var(--text-dim); padding: 2px 0; cursor: pointer;
    display: flex; align-items: center; gap: 6px;
  }

  .ai-thinking { display: flex; align-items: center; gap: 10px; padding: 4px 0; color: var(--text-mute); font-size: 12px; }
  .ai-spinner {
    width: 15px; height: 15px; border-radius: 50%;
    border: 2px solid rgba(123, 77, 255, .25); border-top-color: var(--violet-l);
    animation: spin .7s linear infinite; flex-shrink: 0;
  }
  .ai-spinner.sm { width: 12px; height: 12px; }
  @keyframes spin { to { transform: rotate(360deg); } }

  .ai-body { font-size: 12.5px; line-height: 1.7; color: var(--text); }
  .ai-cause { font-weight: 600; color: var(--text); margin-bottom: 4px; }
  .ai-cause.one-line { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .ai-body.ai-collapsed { padding: 0 0 2px; }
  .ai-expl { color: var(--text-mute); font-size: 11.5px; margin-bottom: 6px; }
  .ai-facts { margin: 6px 0 0 18px; color: var(--text-mute); font-size: 12px; line-height: 1.8; }
  .ai-note { display: flex; align-items: center; gap: 6px; font-size: 11.5px; color: var(--yellow); margin-top: 4px; }
  .ai-hint { font-size: 11.5px; color: var(--text-dim); }
  .ai-disabled { display: flex; align-items: center; gap: 7px; color: var(--text-mute); }

  .ai-actions { display: flex; gap: 8px; margin-top: 12px; flex-wrap: wrap; }
  .ai-act {
    display: flex; align-items: center; gap: 6px;
    font-size: 11.5px; font-weight: 600; padding: 7px 12px; border-radius: var(--r-sm);
    background: rgba(123, 77, 255, .14); border: 1px solid rgba(123, 77, 255, .3); color: var(--violet-l);
    cursor: pointer; transition: .15s;
  }
  .ai-act:hover:not(:disabled) { background: rgba(123, 77, 255, .24); }
  .ai-act:disabled { opacity: .5; cursor: default; }
  .ai-act.ghost { background: var(--bg-panel); border-color: var(--border); color: var(--text-mute); }
  .ai-act.ghost:hover:not(:disabled) { color: var(--text); border-color: var(--border-hi); }
  .ai-act.primary { background: rgba(123, 77, 255, .2); border-color: rgba(123, 77, 255, .5); }
  .ai-act.success { background: rgba(16, 185, 129, .15); border-color: rgba(16, 185, 129, .4); color: var(--green); }
  /* «Полагодити» — дія для пошкоджених файлів оточення (Java/нативки/
     асети/jar): помаранчева, як головна кнопка ремонту на сторінці збірки. */
  .ai-act.repair {
    background: rgba(255, 138, 0, .16); border-color: rgba(255, 138, 0, .45); color: var(--orange);
  }
  .ai-act.repair:hover:not(:disabled) { background: rgba(255, 138, 0, .28); }

  .ai-working { display: flex; align-items: center; gap: 8px; margin-top: 10px; font-size: 11.5px; color: var(--text-mute); }
  .ai-result { margin-top: 10px; font-size: 11.5px; padding: 8px 10px; border-radius: var(--r-sm); line-height: 1.5; }
  .ai-result.ok { background: rgba(16, 185, 129, .1); border: 1px solid rgba(16, 185, 129, .3); color: var(--green); }
  .ai-result.fail { background: rgba(239, 68, 68, .1); border: 1px solid rgba(239, 68, 68, .3); color: var(--red); }
  .ai-model { margin-top: 8px; font-size: 9.5px; color: var(--text-dim); font-family: var(--font-mono); }

  .ram-adjust {
    display: flex; align-items: center; gap: 12px; margin-top: 12px;
    background: var(--bg-panel); border: 1px solid var(--border); border-radius: var(--r-md);
    padding: 10px 12px;
  }
  .ram-label { font-size: 11px; color: var(--text-mute); flex-shrink: 0; }
  .ram-input { flex: 1; accent-color: var(--violet); height: 4px; }
  .ram-value { font-size: 12px; font-weight: 700; color: #fff; font-family: var(--font-mono); width: 56px; text-align: right; flex-shrink: 0; }

  /* Футер */
  .gc-footer {
    display: flex; align-items: center; justify-content: space-between; gap: 12px;
    padding: 9px 16px;
    border-top: 1px solid var(--border);
    font-size: 11px; color: var(--text-mute); flex-wrap: wrap;
    background: color-mix(in srgb, var(--bg-window) 92%, transparent);
    flex-shrink: 0;
  }
  .gc-stats { display: flex; gap: 16px; flex-wrap: wrap; }
  .gc-stats span { display: flex; align-items: center; gap: 5px; font-family: var(--font-mono); }
  .gc-stats span.err { color: var(--red); }
  .gc-stats i { font-size: 13px; }
  .gc-footer-actions { display: flex; gap: 8px; }
  .fbtn {
    display: flex; align-items: center; gap: 6px;
    padding: 6px 12px; border-radius: var(--r-sm);
    background: var(--bg-panel); border: 1px solid var(--border);
    color: var(--text-mute); font-size: 11px; font-weight: 600; cursor: pointer;
    transition: .15s; font-family: inherit;
  }
  .fbtn:hover { color: #fff; border-color: var(--border-hi); }
  .fbtn.stop { color: var(--red); border-color: rgba(239, 68, 68, .25); }
  .fbtn.stop:hover { background: rgba(239, 68, 68, .1); border-color: rgba(239, 68, 68, .4); }
  .fbtn.play { color: var(--orange); border-color: rgba(var(--orange-rgb), .25); }
  .fbtn.play:hover { background: rgba(var(--orange-rgb), .1); border-color: rgba(var(--orange-rgb), .4); }
</style>
