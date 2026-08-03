<script lang="ts">
  import './style.css'
  import { onMount } from 'svelte'
  import { t, setLanguage } from './lib/i18n.svelte'
  import { applyAccent, applyTheme, applyFont } from './lib/theme.svelte'
  import { App, on, Application, Window } from './lib/wails'
  import { toast, confirmDialog } from './lib/toast.svelte'
  import type { Account, Settings, SyncProgress, BrowserEntry, PackIndexEntry, SettingDef, StorageInfo, StoragePackEntry, UnusedJavaInfo, UpdateInfo, FolderPaths, Build, BuildView, BuildProgress, BuildStartEvent, BuildExitEvent, BuildStatus, LaunchProgress, PreLaunchModIssues } from './lib/types'
  import Wizard from './lib/Wizard.svelte'
  import Toasts from './lib/Toasts.svelte'
  import ConfirmModal from './lib/ConfirmModal.svelte'
  import SettingRow from './lib/SettingRow.svelte'
  import PackInstancePage from './lib/PackInstancePage.svelte'
  import CreatePackPage from './lib/CreatePackPage.svelte'
  import Wardrobe from './lib/Wardrobe.svelte'
  import { packAsset } from './lib/packAssets.svelte'
  import { launchStageLabel, launchStageColor, launchStagePct, downloadStageLabel } from './lib/launchStages'
  import { getUIState, updateUIState, pushPackHistory, removePackState, resolveOpenPackId, readContentScroll, restoreContentScroll, setPackUI } from './lib/uiState'
  import appIcon from './assets/images/appicon.png'
  import steveHead from './assets/images/steve-head.png'
  import fabricIcon from './assets/images/fabric_icon.png'
  import forgeIcon from './assets/images/forge_icon.png'
  import neoforgeIcon from './assets/images/neoforge_icon.png'
  import quiltIcon from './assets/images/quilt_icon.png'

  let currentPage = $state('instances')
  // Стиснутий sidebar (лише іконки). Розгорнутий має РЕГУЛЬОВАНУ ширину
  // (sidebarWidth, від 220 до 400px — перетягуванням правого краю). Обидва
  // стани зберігаються у localStorage: розмір переживає перезапуск лаунчера.
  let sidebarCollapsed = $state(false)
  let sidebarWidth = $state(220)
  let sbResizing = $state(false)
  let accDropdownOpen = $state(false)
  let settingsTab = $state('general')
  let instanceTab = $state('shaurma')

  let accounts = $state<Account[]>([])
  // Активний (обраний для гри) акаунт — зберігається у налаштуваннях.
  let activeAccountId = $state('')
  let settings = $state<Settings>({
    language: 'uk', accent: 'orange', accentCustom: '#ff8a00', theme: 'dark', font: 'inter', fontPath: '',
    autoUpdate: true, updateChannel: 'stable',
    closeOnLaunch: false, showConsole: false, saveLogs: true,
    instanceDir: '', javaPath: '', maxRAM: 4096, javaArgs: '', activeAccountId: '',
    consoleMaxLines: 3000, consoleInfoColor: '#8fd3ff', consoleWarnColor: '#ffd166', consoleErrorColor: '#ff6b6b', consoleWrap: false, consoleFontSize: 12, consoleFontFamily: 'mono',
    consoleFontMode: 'all', consoleInfoFont: 'mono', consoleWarnFont: 'mono', consoleErrorFont: 'mono',
    consoleAI: false, showConsoleOnLaunch: false, showConsoleOnCrash: true, showConsoleOnClose: false,
    maxConcurrentDownloads: 10, maxRetries: 6, httpTimeoutSec: 60,
    preLaunchCommand: '', wrapperCommand: '', postExitCommand: '', envVars: '',
    fullscreen: false, windowWidth: 854, windowHeight: 480, hideOnGameOpen: false, exitOnGameClose: false, confirmOnStop: true,
    showGameTime: false, recordGameTime: true, showTotalGameTime: false, gameTimeInHours: false
  })
  let settingsSchema = $state<SettingDef[]>([])
  let playtimes = $state<Record<string, number>>({})
  let storageInfo = $state<StorageInfo | null>(null)
  // Список встановлених збірок з розмірами (для швидкого видалення у
  // вкладці «Пам'ять і кеш»). Завантажується ліниво разом із storageInfo.
  // IconURL/Color — щоб рядок виглядав як картка sidebar (іконка + колір).
  let storagePacks = $state<StoragePackEntry[]>([])
  // Сканування тек «Пам'ять і кеш» у процесі: поки бекенд рахує розміри
  // великих збірок, розділ показує скелетон з прогрес-баром замість
  // «пустоти, яка потім заповнюється інформацією» (раніше весь вміст був
  // прихований за {#if storageInfo} і користувач бачив порожню картку).
  let storageLoading = $state(false)
  // Поточний прогрес обходу тек (події storage:usage-scan / packs-scan).
  let storageScan = $state<{ done: number; total: number; label: string } | null>(null)
  // Прогрес аналізу невикористовуваної Java (storage:java-scan): який мажор
  // зараз сканується і скільки лишилось.
  let javaProgress = $state<{ done: number; total: number; major: number } | null>(null)
  let folderPaths = $state<FolderPaths | null>(null)
  let aboutVersion = $state('')
  let updateResult = $state<UpdateInfo | null>(null)
  let searchResults = $state<BrowserEntry[]>([])
  let searchQuery = $state('')
  let gameRunning = $state(false)
  let gameCrashed = $state(false)
  // Яка збірка була запущена востаннє (для модалки краху: кнопка
  // «Відкрити консоль» переходить на її сторінку і відкриває вкладку
  // «Консоль»). consoleSignal — лічильник запитів відкрити консоль у
  // PackInstancePage (зростає на 1 при кожному кліку).
  let launchedPackId = $state<string | null>(null)
  let consoleSignal = $state(0)
  // Банер краху на головній: показується після аварійного завершення гри.
  // «Не показувати знову» — у localStorage, щоб нав'язливе повідомлення не
  // вилазило після кожного краху, поки користувач сам його не вимкне.
  let crashBannerHidden = $state(false)
  function crashBannerDontShow() {
    crashBannerHidden = true
    try { localStorage.setItem('shaurma.crashBannerHidden', '1') } catch (e) { /* ignore */ }
  }
  function crashBannerDismiss() {
    // Закриття банера (×) — лише для поточного краху, без зміни localStorage.
    gameCrashed = false
  }
  // Відкрити консоль з модалки краху: закриваємо модалку, переходимо на
  // сторінку збірки, яка впала, і відкриваємо її вкладку «Консоль».
  function crashBannerOpenConsole() {
    gameCrashed = false
    if (launchedPackId) {
      consoleSignal++
      openPackPage(launchedPackId)
    }
  }
  let modalLogin = $state(false)
  // "Ворота входу": поки немає жодного акаунта — модалка не закривається
  // і нічого іншого зробити не можна.
  let loginGate = $state(false)
  let pirateUsername = $state('')
  let isShaurmaEdition = $state(false)
  // ID збірки Шаурма, відкритої на сторінці деталей (null = список збірок).
  let openPackId = $state<string | null>(null)
  // Архітектура збірок: BuildView[] від GetBuilds (збірка + стан для
  // ПОТОЧНОГО акаунта). packIndex похідний для сторінки деталей.
  let builds = $state<BuildView[]>([])
  // ── Ручний порядок і закріплення збірок ──
  // Збірки НЕ сортуються (ні за назвою, ні за чимось ще): користувач сам
  // розкладає їх перетягуванням. Закріплені завжди перші (у своєму порядку),
  // решта — у збереженому порядку, НОВІ збірки додаються ВНИЗ (в порядку
  // бекенду). У sidebar закріплені можна перемішувати ЛИШЕ між собою —
  // незакріплені з закріпленими не мішаються (drop відхиляється).
  // Порядок і закріплення зберігаються у localStorage (shaurma.packLayout).
  let packLayout = $state<{ order: string[]; pinned: string[] }>({ order: [], pinned: [] })
  // Стан активного перетягування: яку збірку тягнемо (dragPackId), звідки
  // (dragSrcId — для прозорого «плейсхолдера» на місці картки), над якою
  // карткою курсор і з якого боку (dropTargetId/dropPos — для лінії-підказки
  // кольором збірки) + колір тягненої збірки (dragPackColor → --drop-c).
  let dragPackId = $state<string | null>(null)
  let dragSrcId = $state<string | null>(null)
  let dropTargetId = $state<string | null>(null)
  let dropPos = $state<'before' | 'after' | null>(null)
  let dragPackColor = $state('var(--orange)')
  // Pointer-based перетягування (див. packPointerDown/packPointerMove нижче):
  // dragPointerDown — точка натиску; dragPointerActive — рух перевищив поріг
  // 5px і це вже перетягування, а не клік.
  let dragPointerDown = $state<{ id: string; x: number; y: number; color: string } | null>(null)
  let dragPointerActive = $state(false)
  // suppressPackClick — після drag&drop WebView2 (як і деякі Chromium-версії)
  // ВСЕ ОДНО шле click по картці: користувача перекидало б на сторінку
  // збірки одразу після перетягування, і здавалося б, що порядок «не
  // змінився» або «drag зламаний». Прапорець ставиться в endPackDrag і
  // гасить наступний click (відкриття сторінки) на кілька мілісекунд.
  let suppressPackClick = $state(false)
  // Прогрес завантажень по збірках (живе оновлення з download:progress,
  // щоб прогрес було видно у sidebar на будь-якій вкладці).
  let downloadByPack = $state<Record<string, BuildProgress>>({})
  // packIndex похідний від builds — сторінка деталей і старі хелпери
  // працюють як раніше, але джерело одне (GetBuilds).
  const packIndex = $derived(builds.map(bv => buildToPack(bv.build)))
  const openPack = $derived(packIndex.find(p => p.id === openPackId) ?? null)
  const customBuildsCount = $derived(builds.filter(b => b.build.kind === 'custom').length)
  const shaurmaBuildsCount = $derived(builds.filter(b => b.build.kind === 'shaurma').length)
  let installerLog = $state('')
  // Стадії запуску конкретної збірки (launch:progress): checking →
  // worker_update → java → loader → done. Map по instanceId, бо кілька
  // збірок можуть запускатись (на різних акаунтах) одночасно.
  let launchProgress = $state<Record<string, LaunchProgress>>({})
  let loaded = $state(false)
  // «Останнє вікно» вже відновлено після першого завантаження даних
  // (захист від повторного застосування при повторних викликах loadData).
  let uiRestored = $state(false)
  let isMaximized = $state(false)

  // Wizard (перший запуск)
  let showWizard = $state(false)

  // Login modal
  let loginMsStep = $state<'idle' | 'waiting'>('idle')

  const pageTitles: Record<string, string> = {
    instances: 'nav.instances', browser: 'nav.browser',
    wardrobe: 'nav.wardrobe', settings: 'nav.settings'
  }
  const pageCrumbs: Record<string, string> = {
    instances: 'crumb.instances', browser: 'crumb.browser',
    wardrobe: 'crumb.wardrobe', settings: 'crumb.settings'
  }
  const pageSubs: Record<string, string> = {
    instances: 'instances.subtitle', browser: 'browser.subtitle',
    wardrobe: 'wardrobe.subtitle', settings: 'settings.subtitle'
  }
  const pageIcons: Record<string, string> = {
    instances: 'ti-layout-grid', browser: 'ti-package',
    wardrobe: 'ti-shirt', settings: 'ti-settings'
  }

  // ── Верхня панель: іконка РОЗДІЛУ (не лаунчера) + заголовок + опис ──
  // Заголовок і опис сторінок перенесені сюди з контенту (page-hero на
  // сторінках більше не дублює їх). Для сторінки збірки показуємо сам
  // РОЗДІЛ «Мої збірки», а не назву збірки: назва живе у банері на фоні
  // сторінки (PackInstancePage → inst-banner .ib-name), там їй і місце.
  const topbarIcon = $derived(
    showCreatePack ? 'ti-plus' : pageIcons[currentPage]
  )
  const topbarTitle = $derived(
    showCreatePack ? t('createPack.title') : t(pageTitles[currentPage])
  )
  const topbarSub = $derived(
    showCreatePack ? '' : t(pageSubs[currentPage])
  )

  // ── Стадії запуску збірки (launch:progress) ──
  // Кольори/назви/проценти стадій — у спільному модулі launchStages.ts
  // (однакові в sidebar, тайлах «Мої збірки», сторінці збірки і консолі).

  // Аватари акаунтів (data URL голів зі скінів Mojang; кешування на бекенді).
  let heads = $state<Record<string, string>>({})

  async function refreshHeads() {
    const next: Record<string, string> = {}
    await Promise.all(accounts.map(async (acc) => {
      try {
        const url = await App.GetAccountHead(acc.uuid, acc.username, !!acc.isLicensed)
        const key = acc.uuid || acc.username
        if (key && url) next[key] = url
      } catch (e) { /* запасна голова Стіва */ }
    }))
    heads = next
  }

  function headFor(uuid?: string): string {
    return (uuid && heads[uuid]) || steveHead
  }

  // Поточний акаунт для відображення у сайдбарі та гри: обраний у
  // дропдауні, а якщо не вибраний/видалений — перший у списку.
  function activeAccount(): Account | undefined {
    return accounts.find(a => a.id === activeAccountId) ?? accounts[0]
  }

  async function switchAccount(id: string) {
    activeAccountId = id
    accDropdownOpen = false
    try {
      await App.SetActiveAccount(id)
      settings = await App.GetSettings()
      toast(t('acc.switched'), 'success')
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  // Групи налаштувань (порядок вкладок на сторінці «Налаштування»).
  const settingsGroups = ['general', 'appearance', 'folders', 'performance', 'console', 'tasks', 'commands', 'game', 'gameTime', 'storage', 'about']
  const groupIcons: Record<string, string> = {
    general: 'ti-adjustments', appearance: 'ti-palette',
    folders: 'ti-folder', performance: 'ti-cpu',
    console: 'ti-terminal-2', tasks: 'ti-download',
    commands: 'ti-terminal', game: 'ti-window-maximize',
    gameTime: 'ti-clock', storage: 'ti-database', about: 'ti-info-circle'
  }
  const groupTitleKeys: Record<string, string> = {
    general: 'settings.tab.general', appearance: 'settings.tab.appearance',
    folders: 'settings.tab.folders', performance: 'settings.tab.performance',
    console: 'settings.tab.console', tasks: 'settings.tab.tasks',
    commands: 'settings.tab.commands', game: 'settings.tab.game',
    gameTime: 'settings.tab.gameTime', storage: 'settings.tab.storage', about: 'settings.tab.about'
  }

  // Гардероб працює лише для ліцензійних (Microsoft) акаунтів: пресети
  // зберігаються окремо для кожного такого акаунта, а застосування скіна
  // йде через Mojang API. Для неліцензійних акаунтів вкладку приховуємо.
  let wardrobeVisible = $derived(activeAccount()?.isLicensed === true)

  // Якщо активний акаунт став неліцензійним, а ми на сторінці гардероба —
  // викидаємо на сторінку збірок, бо гардероб для таких акаунтів недоступний
  // (спрацьовує також при зміні акаунта в дропдауні).
  $effect(() => {
    if (!wardrobeVisible && currentPage === 'wardrobe') {
      currentPage = 'instances'
    }
  })

  function navigate(page: string) { currentPage = page; openPackId = null; showCreatePack = false }
  function openPackPage(id: string) {
    pushPackHistory(id); openPackId = id; currentPage = 'instances'; showCreatePack = false
    // Позначаємо збірку АКТИВНОЮ для консолі: окреме вікно консолі
    // прив'язується до неї (а не до останньої ЗАПУЩЕНОЇ з минулого разу) —
    // інакше його кнопка «Запустити» запускала б стару збірку (баг
    // «запустилась інша збірка»). console:focus змушує вікно перечитати
    // знімок/контекст одразу.
    App.SelectActivePack(id)
  }

  // "Нова збірка" — окремий повноекранний режим над вкладкою "Мої збірки"
  // (не змінює currentPage, щоб сайдбар лишався підсвіченим на "Мої збірки").
  let showCreatePack = $state(false)
  // Яку вкладку форми відкрити: "manual" (кнопка «Створити збірку») чи
  // "import" (кнопка «Імпортувати») — передається в initialTab.
  let createPackTab = $state<'manual' | 'import'>('manual')
  function openCreatePack() { createPackTab = 'manual'; showCreatePack = true; openPackId = null; currentPage = 'instances' }
  function openImportPack() { createPackTab = 'import'; showCreatePack = true; openPackId = null; currentPage = 'instances' }
  function closeCreatePack() { showCreatePack = false }
  async function onPackCreated(packId: string) {
    showCreatePack = false
    instanceTab = 'custom'
    await loadData()
    openPackPage(packId)
  }
  function toggleSidebar() {
    sidebarCollapsed = !sidebarCollapsed
    try { localStorage.setItem('shaurma.sidebarCollapsed', sidebarCollapsed ? '1' : '0') } catch { /* ок */ }
  }

  // ── Налаштування sidebar (у «Вигляді» налаштувань лаунчера) ────────────
  // Зберігаються у localStorage (як ширина/стиснення). «Лише закріплені»
  // ховає незакріплені збірки з sidebar; «розмір елементів» масштабує
  // іконки/шрифти через --sb-scale (CSS).
  let sidebarOnlyPinned = $state(false)
  let sidebarScale = $state<'small' | 'normal' | 'large'>('normal')
  function toggleSidebarOnlyPinned() {
    sidebarOnlyPinned = !sidebarOnlyPinned
    try { localStorage.setItem('shaurma.sidebarOnlyPinned', sidebarOnlyPinned ? '1' : '0') } catch { /* ок */ }
  }
  function setSidebarScale(s: string) {
    sidebarScale = (['small', 'normal', 'large'].includes(s) ? s : 'normal') as 'small' | 'normal' | 'large'
    try { localStorage.setItem('shaurma.sidebarScale', s) } catch { /* ок */ }
  }

  // Ресайз sidebar перетягуванням правого краю (лише розгорнутий): ширина
  // від 220px (мінімум — теперішній розмір) до 400px. Зберігається при
  // відпусканні — повернутись до малого розміру можна кнопкою стиснення.
  function startSidebarResize(e: MouseEvent) {
    e.preventDefault()
    if (sidebarCollapsed) return
    const startX = e.clientX
    const startW = sidebarWidth
    sbResizing = true
    // Під час перетягування не виділяємо текст (інакше драг над текстом
    // збірок тягнув би за собою виділення).
    document.body.style.userSelect = 'none'
    const move = (ev: MouseEvent) => {
      sidebarWidth = Math.min(400, Math.max(220, startW + (ev.clientX - startX)))
    }
    const up = () => {
      sbResizing = false
      document.body.style.userSelect = ''
      window.removeEventListener('mousemove', move)
      window.removeEventListener('mouseup', up)
      try { localStorage.setItem('shaurma.sidebarWidth', String(sidebarWidth)) } catch { /* ок */ }
    }
    window.addEventListener('mousemove', move)
    window.addEventListener('mouseup', up)
  }
  function toggleAccDropdown() { accDropdownOpen = !accDropdownOpen }

  // Закриваємо дропдаун акаунтів кліком поза ним (документний слухач).
  // БАГ (виправлено): Svelte 5 флашить ефекти синхронно одразу після
  // хендлера кліку — тому клік, що ВІДКРИВ дропдаун (по .sidebar-footer),
  // продовжував спливати до document, де щойно доданий слухач закривав
  // його миттєво. Тепер ігноруємо кліки всередині футера/дропдауна.
  $effect(() => {
    if (!accDropdownOpen) return
    const close = (e: MouseEvent) => {
      const t = e.target as HTMLElement | null
      if (t?.closest('.sidebar-footer') || t?.closest('.acc-dropdown')) return
      accDropdownOpen = false
    }
    document.addEventListener('click', close)
    return () => document.removeEventListener('click', close)
  })

  // ── «Останнє вікно»: відновлення + збереження стану навігації ──────────
  // Після першого завантаження даних повертаємось на ту саму сторінку /
  // вкладку збірок / налаштування та у відкриту збірку (builds потрібні,
  // щоб перевірити, чи збережена збірка ще існує). Скрол сторінки збірки
  // відновлює сама PackInstancePage на маунті; скрол решти сторінок — тут.
  function restoreLastWindow() {
    const s = getUIState()
    const knownPages = ['instances', 'browser', 'wardrobe', 'settings']
    currentPage = knownPages.includes(s.page) ? s.page : 'instances'
    instanceTab = s.instanceTab === 'shaurma' && isShaurmaEdition ? 'shaurma' : 'custom'
    settingsTab = [...settingsGroups, 'accounts'].includes(s.settingsTab) ? s.settingsTab : 'general'
    const valid = new Set(builds.map(b => b.build.id))
    const savedPack = resolveOpenPackId(valid)
    if (savedPack) {
      openPackId = savedPack
      currentPage = 'instances'
      // Позначаємо відновлену збірку АКТИВНОЮ для консолі одразу (як це
      // робить openPackPage): інакше при старті окреме вікно консолі
      // (відкрите з трею) прив'язалось би до порожнього/застарілого ID і
      // його кнопка «Запустити» могла б запустити не ту збірку (баг
      // «запускається попередня збірка»).
      App.SelectActivePack(savedPack)
    }
    uiRestored = true
    if (!openPackId) restoreContentScroll(getUIState().pages[currentPage] ?? 0)
  }

  // Збереження навігації при кожній зміні (дебаунсу не треба — значення
  // змінюються рідко, а запис у localStorage дешевий).
  $effect(() => {
    if (!uiRestored) return
    const page = currentPage
    const tab = instanceTab
    const st = settingsTab
    const open = openPackId
    updateUIState({ page, instanceTab: tab, settingsTab: st, openPackId: open })
  })

  // Скрол «простих» сторінок відновлюється при поверненні на них; для
  // сторінки збірки тут пропускаємо — нею займається PackInstancePage.
  $effect(() => {
    if (!uiRestored || openPackId) return
    const page = currentPage
    restoreContentScroll(getUIState().pages[page] ?? 0)
  })

  // Збереження скролу «простих» сторінок з дебаунсом (скрол збірки
  // зберігає PackInstancePage — тут його ігноруємо через openPackId).
  let pageScrollTimer: ReturnType<typeof setTimeout> | undefined
  function onMainScroll() {
    if (openPackId) return
    clearTimeout(pageScrollTimer)
    pageScrollTimer = setTimeout(() => {
      const s = getUIState()
      updateUIState({ pages: { ...s.pages, [currentPage]: readContentScroll() } })
    }, 150)
  }
  function persistMainScrollNow() {
    if (openPackId) return
    const s = getUIState()
    updateUIState({ pages: { ...s.pages, [currentPage]: readContentScroll() } })
  }

  async function loadData() {
    // Кожен запит стартового завантаження незалежний (Promise.allSettled):
    // збій одного (напр. GetPackIndex — CDN збірок Шаурма, мережа) не
    // повинен обривати решту. Раніше одна помилка в спільному try/catch
    // вбивала весь ланцюжок: settingsSchema не завантажувалась (порожні
    // розділи налаштувань — лише заголовки) і refreshHeads() не
    // викликався (іконки акаунтів завжди Стів).
    const [accR, setR, buildR, schemaR, playR, verR, edR] = await Promise.allSettled([
      App.GetAccounts(),
      App.GetSettings(),
      App.GetBuilds(),
      App.GetSettingsSchema(),
      App.GetPlaytime(),
      App.GetVersion(),
      App.IsShaurmaEdition(),
    ])
    accounts = accR.status === 'fulfilled' ? (accR.value ?? []) : []
    settings = setR.status === 'fulfilled' ? setR.value : settings
    activeAccountId = settings.activeAccountId ?? ''
    setLanguage(settings.language)
    applyAccent(settings.accent, settings.accentCustom)
    applyTheme(settings.theme)
    applyFont(settings.font, settings.fontPath)
    builds = buildR.status === 'fulfilled' ? (buildR.value ?? []) : []
    settingsSchema = schemaR.status === 'fulfilled' ? schemaR.value : []
    playtimes = playR.status === 'fulfilled' ? playR.value : {}
    aboutVersion = verR.status === 'fulfilled' ? verR.value : ''
    // Прапорець збірки: у base-версії бекенд-методів Шаурма-збірки
    // немає взагалі (фізично не скомпільовані), тому вкладку/список
    // приховуємо і за замовчуванням показуємо вкладку "Кастомні".
    isShaurmaEdition = edR.status === 'fulfilled' ? edR.value : false
    if (!isShaurmaEdition) instanceTab = 'custom'
    // Відновлюємо «останнє вікно» одразу після першого завантаження даних.
    if (!uiRestored) restoreLastWindow()
    // Аватари пробуємо завжди, навіть якщо щось вище не завантажилось
    // (всередині вже свій try/catch на кожен акаунт).
    refreshHeads()
    loaded = true
    // Якщо акаунта немає — "ворота входу": головний екран недоступний,
    // поки не увійдеш хоча б в один акаунт. (Поки йде першозапускний
    // майстер, модалка не рендериться — у неї умова !showWizard.)
    loginGate = accounts.length === 0 && !showWizard
    if (loginGate) { loginMsStep = 'idle'; modalLogin = true }
  }

  async function downloadPack(id: string) { App.DownloadPack(id) }

  // Повний ануінстал збірки (файли + реєстр + конфіг). З підтвердженням:
  // видалення незворотне, тож питаємо перед тим, як прибрати файли.
  async function deletePack(id: string) {
    const pk = packIndex.find(p => p.id === id)
    const ok = await confirmDialog(t('pack.deleteConfirm'), `${pk?.name ?? id}\n\n${t('pack.deleteConfirmHint')}`, {
      confirmLabel: t('pack.delete'), cancelLabel: t('cancel'), danger: true
    })
    if (!ok) return
    try {
      await App.DeletePack(id)
      // Прибрати збережений стан «останнього вікна» видаленої збірки.
      removePackState(id)
      toast(t('pack.deleted'), 'success')
      // Лишаємось на сторінці збірки: Шаурма-збірка після видалення лишається
      // в каталозі, тож сторінка показує стан «не встановлена» з кнопкою
      // «Завантажити» (а не повертаємось у список збірок).
      openPackId = id
      await loadData()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  // ── Стани збірок (архітектура BuildView) ───────────────────────────────

  function buildToPack(b: Build): PackIndexEntry {
    return {
      id: b.id, name: b.name, description: b.description,
      mc_version: b.mcVersion, loader_type: b.loaderType, loader_version: b.loaderVersion,
      icon_url: b.iconUrl, background_url: b.backgroundUrl, mrpack_url: b.source,
      updated_at: b.version, tags: b.tags, color: b.color, version: b.version, icon: b.icon,
    }
  }

  // Перечит станів з бекенду (GetBuilds), зберігаючи живий прогрес.
  async function refreshBuilds() {
    try {
      const list = await App.GetBuilds()
      builds = list.map(b => ({ ...b, progress: b.progress ?? downloadByPack[b.build.id] }))
    } catch (e) { console.error(e) }
  }

  // Точкова зміна однієї збірки (події build:started/build:exit).
  function patchBuild(id: string, fn: (b: BuildView) => BuildView) {
    const i = builds.findIndex(b => b.build.id === id)
    if (i < 0) return
    builds = builds.map((b, idx) => idx === i ? fn(b) : b)
  }

  // ── Ручний порядок збірок (drag&drop) ──────────────────────────────────
  // Відсортований (НЕ за назвою!) список збірок: спершу ЗАКРІПЛЕНІ (у
  // збереженому порядку), потім решта у збереженому порядку, нові збірки
  // (без запису в layout) — внизу, як прийшли з бекенду. Після перетягування
  // layout перебудовується (persisted у localStorage).
  function savePackLayout() {
    try {
      localStorage.setItem('shaurma.packLayout', JSON.stringify(packLayout))
    } catch { /* ок */ }
  }

  function loadPackLayout() {
    try {
      const raw = localStorage.getItem('shaurma.packLayout')
      if (!raw) return
      const p = JSON.parse(raw)
      if (p && Array.isArray(p.order) && Array.isArray(p.pinned)) {
        packLayout = { order: p.order, pinned: p.pinned }
      }
    } catch { /* ок */ }
  }

  // Порядок збірок для ВІДОБРАЖЕННЯ: pinned перші, потім решта в
  // збереженому порядку, нові — внизу.
  const orderedBuildViews = $derived.by(() => {
    const byId = new Map(builds.map(bv => [bv.build.id, bv]))
    const known = new Set(packLayout.order)
    // Спершу ті, що в layout (у збереженому порядку), далі нові (в порядку бекенду).
    const ordered = packLayout.order.map(id => byId.get(id)).filter((b): b is BuildView => !!b)
    const rest = builds.filter(bv => !known.has(bv.build.id))
    const pinned = new Set(packLayout.pinned)
    const pinnedViews = ordered.filter(bv => pinned.has(bv.build.id))
    const unpinnedViews = ordered.filter(bv => !pinned.has(bv.build.id))
    return [...pinnedViews, ...unpinnedViews, ...rest]
  })

  // Видимі збірки для поточної вкладки (shaurma/custom) у ручному порядку.
  const tabBuilds = $derived.by(() =>
    orderedBuildViews.filter(b => (instanceTab === 'shaurma') === (b.build.kind === 'shaurma'))
  )
  const tabPinned = $derived.by(() => {
    const pin = new Set(packLayout.pinned)
    return tabBuilds.filter(b => pin.has(b.build.id))
  })
  const tabUnpinned = $derived.by(() => {
    const pin = new Set(packLayout.pinned)
    return tabBuilds.filter(b => !pin.has(b.build.id))
  })

  // Чи закріплена збірка (для кнопки-канцелярки та бейджа).
  function isPinned(id: string): boolean {
    return packLayout.pinned.includes(id)
  }

  function togglePin(id: string) {
    const pinned = packLayout.pinned.includes(id)
    // Закріплену збірку вже додаємо в layout.order (щоб вона не «стрибнула»
    // в кінець як «нова» після перезапуску) — закріплені перші в будь-якому
    // разі, але порядок серед закріплених зберігаємо.
    if (!packLayout.order.includes(id)) {
      packLayout.order = [...packLayout.order, id]
    }
    packLayout.pinned = pinned
      ? packLayout.pinned.filter(x => x !== id)
      : [...packLayout.pinned, id]
    savePackLayout()
  }

  // ── ПКМ-меню на картках збірок (sidebar + «Мої збірки») ────────────────
  // Клік ПКМ на картці відкриває контекстне меню зі швидкими діями:
  // відкрити сторінку, закріпити/відкріпити, запустити/зупинити,
  // завантажити/оновити, консоль, тека, полагодити, видалити. Позиція
  // кліпується до розмірів вікна — меню не вилазить за екран. Закривається
  // кліком будь-де, blur-ом вікна або клавішею Esc.
  let ctxMenu = $state<{ id: string; x: number; y: number } | null>(null)
  // Збірка під меню (BuildView — для status/installed/дій).
  const ctxPack = $derived(ctxMenu ? builds.find(b => b.build.id === ctxMenu.id) ?? null : null)

  function openCtxMenu(e: MouseEvent, id: string) {
    e.preventDefault()
    const W = 220, H = 380
    ctxMenu = {
      id,
      x: Math.max(4, Math.min(e.clientX, window.innerWidth - W - 8)),
      y: Math.max(4, Math.min(e.clientY, window.innerHeight - H - 8)),
    }
  }

  $effect(() => {
    if (!ctxMenu) return
    const close = () => { ctxMenu = null }
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') ctxMenu = null }
    window.addEventListener('click', close)
    window.addEventListener('blur', close)
    window.addEventListener('keydown', onKey)
    return () => {
      window.removeEventListener('click', close)
      window.removeEventListener('blur', close)
      window.removeEventListener('keydown', onKey)
    }
  })

  function ctxAction(act: string, id: string) {
    ctxMenu = null
    const bv = builds.find(b => b.build.id === id)
    switch (act) {
      case 'open': openPackPage(id); break
      case 'pin': togglePin(id); break
      case 'play': if (bv) buildAction(bv); break
      case 'download': downloadPack(id); break
      case 'console': App.OpenPackConsole(id); break
      case 'folder': App.OpenPackFolder(id).catch((e: any) => toast(t('error.withMessage', { message: e }), 'error')); break
      case 'repair': App.RepairPack(id).catch((e: any) => toast(t('error.withMessage', { message: e }), 'error')); break
      case 'delete': deletePack(id); break
    }
  }

  // ── Drag&drop збірок (pointer-based) ──
  // HTML5 DnD у WebView2 глючить: dragstart не спрацьовує, коли початок
  // перетягування припадає на вкладені елементи картки (кнопки, іконки),
  // тому після додавання закріплень збірки «перестали тягнутися». Замість
  // цього — власне перетягування на подіях pointerdown/pointermove/pointerup:
  // клік (відкриття збірки) і перетягування розрізняються за порогом руху
  // 5px. Ціль (before/after) рахується за позицією курсора над карткою
  // (sidebar — по Y, сітка «Мої збірки» — по X), лінія-підказка малюється
  // кольором збірки (--drop-c). Закріплені перемішуються лише із
  // закріпленими, незакріплені — лише із незакріпленими.
  function packPointerDown(e: PointerEvent, id: string, color: string) {
    if (e.button !== 0) return
    dragPointerDown = { id, x: e.clientX, y: e.clientY, color }
    dragPointerActive = false
    suppressPackClick = false
  }

  function packPointerMove(e: PointerEvent) {
    if (!dragPointerDown) return
    // Перехід «клік → перетягування»: лише після руху > 5px.
    if (!dragPointerActive) {
      const dx = e.clientX - dragPointerDown.x
      const dy = e.clientY - dragPointerDown.y
      if (dx * dx + dy * dy < 25) return
      dragPointerActive = true
      dragPackId = dragPointerDown.id
      dragSrcId = dragPointerDown.id
      dragPackColor = dragPointerDown.color
      window.addEventListener('mousemove', pointerDragMove)
      window.addEventListener('mouseup', pointerDragEnd, { once: true })
    }
    pointerDragMove(e)
  }

  function pointerDragMove(e: MouseEvent) {
    if (!dragPackId) { dropTargetId = null; dropPos = null; return }
    const el = document.elementFromPoint(e.clientX, e.clientY)
    const card = el?.closest?.('[data-pack-id]') as HTMLElement | null
    if (!card) { dropTargetId = null; dropPos = null; return }
    const tid = card.dataset.packId ?? ''
    if (!tid || tid === dragPackId) { dropTargetId = null; dropPos = null; return }
    // Різні групи (закріплені/незакріплені) не перемішуються.
    if (packLayout.pinned.includes(dragPackId) !== packLayout.pinned.includes(tid)) {
      dropTargetId = null
      dropPos = null
      return
    }
    const list = card.classList.contains('inst-tile') ? false : true
    const rect = card.getBoundingClientRect()
    const mid = list ? rect.top + rect.height / 2 : rect.left + rect.width / 2
    const cur = list ? e.clientY : e.clientX
    dropTargetId = tid
    dropPos = cur < mid ? 'before' : 'after'
  }

  function pointerDragEnd() {
    const dragged = dragPackId
    const target = dropTargetId
    const pos = dropPos
    const wasDrag = dragPointerActive
    window.removeEventListener('mousemove', pointerDragMove)
    dragPointerDown = null
    dragPointerActive = false
    // Гасимо наступний click після перетягування (WebView2 все одно шле
    // click по картці одразу після mouseup). 60 мс достатньо.
    endPackDrag(wasDrag)
    if (wasDrag) commitPackReorder(dragged, target, pos)
  }

  function endPackDrag(suppressClick = false) {
    dragPackId = null
    dragSrcId = null
    dropTargetId = null
    dropPos = null
    if (suppressClick) {
      suppressPackClick = true
      setTimeout(() => { suppressPackClick = false }, 60)
    }
  }

  // commitPackReorder — реальне переміщення (спільне для sidebar і сітки).
  function commitPackReorder(dragged: string | null, target: string | null, pos: 'before' | 'after' | null) {
    if (!dragged || !target || dragged === target || !pos) return
    const draggedPinned = packLayout.pinned.includes(dragged)
    const targetPinned = packLayout.pinned.includes(target)
    if (draggedPinned !== targetPinned) return

    // Працюємо з ПОТОЧНИМ відображуваним списком (та сама вкладка), щоб
    // не перемішувати порядок іншої вкладки.
    const list = draggedPinned ? tabPinned : tabUnpinned
    const ids = list.map(b => b.build.id)
    const without = ids.filter(id => id !== dragged)
    const ti = without.indexOf(target)
    if (ti < 0) return
    const at = pos === 'before' ? ti : ti + 1
    without.splice(at, 0, dragged)

    // Перебудовуємо глобальний layout: закріплені цієї вкладки (оновлені),
    // закріплені іншої вкладки (старий порядок), незакріплені цієї вкладки
    // (оновлені), незакріплені іншої вкладки (старий порядок), нові — вниз.
    const inTab = new Set(tabBuilds.map(b => b.build.id))
    const pinnedSet = new Set(packLayout.pinned)
    const thisPinned = draggedPinned ? without : tabPinned.map(b => b.build.id)
    const thisUnpinned = draggedPinned ? tabUnpinned.map(b => b.build.id) : without
    const otherPinned = packLayout.pinned.filter(id => !inTab.has(id))
    const otherUnpinned = packLayout.order.filter(id => !pinnedSet.has(id) && !inTab.has(id))
    const known = new Set([...thisPinned, ...thisUnpinned, ...otherPinned, ...otherUnpinned])
    const rest = builds.filter(bv => !known.has(bv.build.id)).map(bv => bv.build.id)
    packLayout = {
      order: [...thisPinned, ...otherPinned, ...thisUnpinned, ...otherUnpinned, ...rest],
      pinned: packLayout.pinned,
    }
    savePackLayout()
  }

  // Живий прогрес V2-синхронізації Шаурма-збірок (sync:progress від
  // sync.Runner). Без цього sidebar/тайли/сторінка збірки "зависають":
  // новий рушій шле RunnerProgress, а не SessionProgress старого двигуна.
  // Mode/Stage/NoInternet прокидаються в BuildProgress, щоб UI показував
  // «Розпаковка…», «Зупинено» чи «Немає інтернету» замість завислого 99%.
  function applySyncProgress(p: SyncProgress) {
    if (p.mode === 'complete' || p.mode === 'cancelled' || p.mode === 'error') {
      const { [p.packId]: _drop, ...rest } = downloadByPack
      downloadByPack = rest
      refreshBuilds()
      return
    }
    const prog: BuildProgress = {
      percent: p.overallPercent, bytesDone: p.bytesDownloaded, bytesTotal: p.bytesTotal,
      filesDone: p.filesCompleted, filesTotal: p.filesTotal,
      currentFile: p.currentFile, speedBps: p.speedBps,
      mode: p.mode, stage: p.stage, noInternet: p.noInternet,
    }
    downloadByPack = { ...downloadByPack, [p.packId]: prog }
    patchBuild(p.packId, b => ({
      ...b,
      build: {
        ...b.build,
        // Іконка з'являється у placeholder'а під час імпорту (вбудована
        // icon.png модпаку) — одразу оновлюємо картку/плітку.
        iconUrl: p.iconUrl ?? b.build.iconUrl,
      },
      status: b.status === 'running' ? b.status
        : (b.installed ? 'updating' : 'downloading'),
      progress: prog,
    }))
  }

  // Дія за станом збірки: зупинити / грати / оновити / завантажити.
  function buildAction(bv: BuildView) {
    const st = bv.status
    if (st === 'running') stopGame()
    else if (st === 'ready') launchInstance(bv.build.id)
    else if (st === 'not-installed' || st === 'needs-update') downloadPack(bv.build.id)
    else if (st === 'error') refreshBuilds()
  }

  // ── Кнопка під час качки: пауза / продовжити ──────────────────────────
  // Раніше під час downloading/updating кнопка показувала «мертвий» спінер
  // і нічого не робила. Тепер вона РЕАЛЬНО керує завантаженням: клік =
  // пауза, на паузі клік = продовжити (той самий DownloadPack, що й на
  // сторінці збірки).
  function downloadAction(bv: BuildView) {
    if (bv.progress?.mode === 'paused') downloadPack(bv.build.id)
    else App.PauseDownload(bv.build.id)
  }

  // Загальна дія кнопки (sidebar + тайли).
  function packBtnAction(bv: BuildView) {
    const st = bv.status
    if (st === 'downloading' || st === 'updating') downloadAction(bv)
    else buildAction(bv)
  }

  // Клас кольору кнопки: під час качки — жовта «пауза», на паузі — зелена
  // «продовжити» (як у кнопки «Грати»).
  // Клас «сяйва» іконки збірки у СТИСНЕНОМУ sidebar: реальний стан там не
  // видно, але ритмічне сяйво іконки підказує його (користувач просив
  // «сяйво іконки, яке ритмічно стухає і розтухається»):
  //   зелений  — гра запущена
  //   оранжевий — качає файли збірки (mrpack/Шаурма)
  //   червоний  — качає Java / лоадер (стадії java/loader запуску)
  //   синій     — оновлює файли збірки (worker_update / updating)
  function glowClass(bv: BuildView, lp?: LaunchProgress | null): string {
    if (lp && !lp.done) {
      if (lp.stage === 'java' || lp.stage === 'loader') return 'glow-red'
      if (lp.stage === 'worker_update') return 'glow-blue'
      return ''
    }
    if (bv.status === 'running') return 'glow-green'
    if (bv.status === 'downloading') return 'glow-orange'
    if (bv.status === 'updating') return 'glow-blue'
    return ''
  }

  // Іконка стадії запуску НА КАРТЦІ (sidebar/тайли): поки збірка
  // встановлює Java/лоадер/файли, кнопка дії зникає і показується
  // «жива» іконка без функцій:
  //   checking       → лупа, що слабо рухається вгору-вниз
  //   worker_update  → іконка оновлення, що крутиться 360°
  //   java           → ЧЕРВОНА іконка Java, рухається вгору-вниз
  //   loader         → іконка саме того лоадера (fabric/forge/…), вгору-вниз
  // Кнопка НЕ клікабельна (div, не button) — це індикатор, а не дія.
  function launchStageIcon(stage?: string): { icon: string; cls: string } | null {
    switch (stage) {
      case 'checking': return { icon: 'ti-search', cls: 'st-check' }
      case 'worker_update': return { icon: 'ti-refresh', cls: 'st-update' }
      // ti-brand-java НЕ існує в tabler-icons (є лише ti-brand-javascript) —
      // іконка Java в sidebar просто не рендерилась під час встановлення
      // Java. Використовуємо ti-coffee (чашка кави = Java), яка точно є.
      case 'java': return { icon: 'ti-coffee', cls: 'st-java' }
      case 'loader': return { icon: 'ti-package', cls: 'st-loader' }
      default: return null
    }
  }
  // Іконка-IMG конкретного лоадера (для стадії loader): у збірки
  // loader_type, тож показуємо САМЕ її значок (як у формі створення).
  function loaderImgFor(loaderType?: string): string | undefined {
    switch (loaderType) {
      case 'fabric': return fabricIcon
      case 'forge': return forgeIcon
      case 'neoforge': return neoforgeIcon
      case 'quilt': return quiltIcon
      default: return undefined
    }
  }

  function spBtnClass(bv: BuildView): string {
    const st = bv.status
    if (st === 'downloading' || st === 'updating') {
      return bv.progress?.mode === 'paused' ? 'play' : 'pause'
    }
    return statusBtnClass(st)
  }

  function spBtnTitle(bv: BuildView): string {
    const st = bv.status
    if (st === 'downloading' || st === 'updating') {
      return bv.progress?.mode === 'paused' ? t('pack.resume') : t('pack.pause')
    }
    return statusTitle(st)
  }

  // Текст кнопки тайла (з текстом).
  function tileBtnLabel(bv: BuildView, lp?: LaunchProgress | null): string {
    const st = bv.status
    if (lp && !lp.done) return launchStageLabel(lp.stage)
    if (st === 'downloading' || st === 'updating') {
      return bv.progress?.mode === 'paused' ? t('pack.resume') : t('pack.pause')
    }
    return statusTitle(st)
  }

  // Підпис під назвою збірки: під час качки — ЩО саме качається (поточний
  // файл або стадія), під час запуску — стадія, інакше «лоадер · версія».
  function packSubText(bv: BuildView, lp?: LaunchProgress | null): string {
    const st = bv.status
    if (lp && !lp.done) return launchStageLabel(lp.stage)
    if (st === 'downloading' || st === 'updating') {
      return bv.progress?.currentFile || downloadStageLabel(bv.progress?.stage)
    }
    return `${bv.build.loaderType} · ${bv.build.mcVersion}`
  }

  function statusBtnClass(st: BuildStatus): string {
    switch (st) {
      case 'running': return 'stop'
      case 'ready': return 'play'
      case 'needs-update': return 'download'
      case 'not-installed': return 'download'
      default: return 'disabled'
    }
  }

  function statusTitle(st: BuildStatus): string {
    switch (st) {
      case 'running': return t('pack.stop')
      case 'ready': return t('pack.play')
      case 'needs-update': return t('pack.update')
      case 'not-installed': return t('pack.download')
      case 'downloading': return t('build.status.downloading')
      case 'updating': return t('build.status.updating')
      default: return ''
    }
  }

  // ── Попередження при закритті лаунчера з активною качкою ──────────────
  // Бекенд (tray.go / QuitLauncher) шле sync:quit-warning, коли користувач
  // намагається закрити вікно/вийти, а збірки ще качаються. Показуємо
  // модалку підтвердження: «закриття перерве завантаження»; лише після
  // явного підтвердження (ConfirmQuitWhileSyncing) закриття проходить.
  async function confirmQuitWhileSyncing(packIDs: string[]) {
    const names = (packIDs || [])
      .map(id => packIndex.find(p => p.id === id)?.name ?? id)
      .join(', ')
    const ok = await confirmDialog(t('quit.syncTitle'), t('quit.syncMsg', { packs: names }), {
      confirmLabel: t('quit.syncConfirm'), cancelLabel: t('cancel'), danger: true
    })
    if (ok) {
      await App.ConfirmQuitWhileSyncing()
    }
  }

  // Акаунт за іменем — для голови скіна, на якому запущена збірка.
  function accForName(name: string): Account | undefined {
    return accounts.find(a => a.username === name)
  }

  // ── Pre-launch перевірка модів ──
  // Перед запуском перевіряємо збірку на дублікати модів і відсутні
  // залежності: якщо є проблеми — модалка зі швидкими діями (видалити
  // дублікати / відкрити розділ «Моди» / все одно запустити). Бекенд
  // рахує це локально і швидко, без мережі.
  let preLaunchIssues = $state<PreLaunchModIssues | null>(null)
  let pendingLaunchId = $state<string | null>(null)

  async function launchInstance(id: string) {
    try {
      // Позначаємо збірку активною для консолі ПЕРЕД запуском: коли гра
      // стартує зі списку (sidebar/«Мої збірки») без відкриття сторінки,
      // окреме вікно консолі має одразу знати, яка збірка запускається, і
      // не показувати «Запустити» для іншої (баг «запускається попередня
      // збірка»).
      App.SelectActivePack(id)
      const issues = await App.CheckPreLaunchModIssues(id).catch(() => null)
      if (issues?.hasIssues) {
        pendingLaunchId = id
        preLaunchIssues = issues
        return
      }
      await doLaunch(id)
    } catch (e: any) { toast(t('launchError', { message: e }), 'error') }
  }

  async function doLaunch(id: string) {
    try {
      await App.LaunchInstance(id)
      launchedPackId = id
      gameRunning = true
      gameCrashed = false
      // «Закривати лаунчер під час гри» / «Сховати, коли гра відкрилась»:
      // ховаємо вікно у трей. Повернеться автоматично на game:exit.
      if (settings.closeOnLaunch || settings.hideOnGameOpen) await App.HideToTray()
    } catch (e: any) { toast(t('launchError', { message: e }), 'error') }
  }

  // «Видалити дублікати»: для кожної групи лишаємо один увімкнений файл,
  // решту видаляємо і продовжуємо запуск.
  async function deleteDuplicatesAndLaunch() {
    const id = pendingLaunchId
    const issues = preLaunchIssues
    preLaunchIssues = null
    pendingLaunchId = null
    if (!id || !issues) return
    for (const g of issues.duplicates) {
      const keep = g.mods.find(m => m.enabled) ?? g.mods[0]
      for (const m of g.mods) {
        if (m.fileName !== keep.fileName) {
          try { await App.DeleteMod(id, m.fileName) } catch { /* не критично */ }
        }
      }
    }
    await doLaunch(id)
  }

  // «Все одно запустити».
  function launchAnyway() {
    const id = pendingLaunchId
    preLaunchIssues = null
    pendingLaunchId = null
    if (id) doLaunch(id)
  }

  // «Відкрити моди»: сторінка збірки одразу на вкладці «Моди» — там
  // користувач встановить залежності чи розбереться з дублікатами.
  function openModsToFix() {
    const id = pendingLaunchId
    preLaunchIssues = null
    pendingLaunchId = null
    if (id) {
      try { setPackUI(id, { tab: 'mods', scroll: 0 }) } catch { /* ок */ }
      openPackPage(id)
    }
  }

  async function stopGame() {
    // Підтвердження перед зупинкою (ТЗ: «вікно чи хочете зупинити збірку»).
    // Вимкнути у Налаштуваннях → «Вікно гри» → «Підтверджувати зупинку».
    if (settings.confirmOnStop) {
      const ok = await confirmDialog(t('pack.stopConfirm'), t('pack.stopConfirmHint'), {
        confirmLabel: t('pack.stop'), cancelLabel: t('cancel'), danger: true
      })
      if (!ok) return
    }
    try { await App.StopGame(); gameRunning = false }
    catch (e: any) { toast(t('error.withMessage', { message: e }), 'error') }
  }

  async function searchContent() {
    if (!searchQuery.trim()) return
    try { searchResults = await App.SearchContent(searchQuery) } catch (e) { console.error(e) }
  }

  // Авто-збереження: зміни налаштувань зберігаються одразу (без кнопки
  // «Зберегти») з маленькою дебаунс-затримкою, щоб не писати диск на
  // кожен клік. Візуальні ефекти (акцент/мова/тема/шрифт) застосовуються
  // миттєво.
  let saveTimer: ReturnType<typeof setTimeout> | undefined

  function updateSetting(key: string, v: any) {
    (settings as any)[key] = v
    if (key === 'accent' || key === 'accentCustom') applyAccent(settings.accent, settings.accentCustom)
    if (key === 'language') setLanguage(v)
    if (key === 'theme') applyTheme(v)
    if (key === 'font' || key === 'fontPath') applyFont((settings as any).font, (settings as any).fontPath).catch(() => {})
    clearTimeout(saveTimer)
    saveTimer = setTimeout(async () => {
      try {
        await App.SaveSettings(settings)
      } catch (e: any) {
        toast(t('error.withMessage', { message: e }), 'error')
      }
    }, 350)
  }

  // Теки та шляхи — ліниве завантаження: шляхи читаються лише коли
  // відкривається вкладка «Теки та шляхи» (реальні значення з бекенду,
  // з урахуванням кастомних override у launcher-location.json).
  let folderPathsLoaded = false
  async function loadFolderPaths(force = false) {
    if (folderPathsLoaded && !force) return
    folderPathsLoaded = true
    try { folderPaths = await App.GetFolderPaths() } catch (e) {
      // Помилка читання — дозволяємо повторну спробу, скидаючи прапорець.
      folderPathsLoaded = false
      console.error(e)
    }
  }

  // Зміна однієї теки лаунчера (baseDir/installations/java/cache/logs):
  // вибір у діалозі → збереження на бекенді → перечитування шляхів.
  async function setFolder(kind: string) {
    const p = await App.BrowseFolder()
    if (!p) return
    try {
      await App.SetFolderPath(kind, p)
      toast(t('settings.saved'), 'success')
      await loadFolderPaths(true)
      // Папка даних/збірок/Java змінилась — перечитуємо налаштування,
      // щоб поля instanceDir/javaPath показували актуальні шляхи.
      if (kind === 'baseDir' || kind === 'installations' || kind === 'java') {
        settings = await App.GetSettings()
      }
      // Зміна cache/logs впливає на розміри тек — змушуємо вкладку
      // «Пам'ять і кеш» перерахувати зайняте місце при наступному відкритті.
      if (kind === 'cache' || kind === 'logs') {
        storageLoaded = false
      }
    } catch (e: any) { toast(t('error.withMessage', { message: e }), 'error') }
  }

  // Повернути папку даних лаунчера до системного дефолту.
  async function resetBaseDir() {
    if (!folderPaths) return
    const ok = await confirmDialog(t('settings.folders.reset'), t('settings.folders.resetConfirm'), {
      confirmLabel: t('settings.folders.reset'), cancelLabel: t('cancel'), danger: false
    })
    if (!ok) return
    try {
      await App.SetFolderPath('baseDir', folderPaths.defaultBaseDir)
      toast(t('settings.saved'), 'success')
      await loadFolderPaths(true)
      settings = await App.GetSettings()
    } catch (e: any) { toast(t('error.withMessage', { message: e }), 'error') }
  }

  // Підрахунок розміру тек — лінивий: рахуємо лише коли відкривається
  // вкладка «Пам'ять і кеш» (повний обхід installations/java може бути
  // важким для багатогігабайтних збірок, тому не робимо це при старті
  // чи після кожного завантаження).
  let storageLoaded = false
  async function loadStorageInfo(force = false) {
    if (storageLoaded && !force) return
    storageLoaded = true
    // Поки бекенд рахує розміри (може бути довго на великих збірках) —
    // показуємо скелетон з прогрес-баром, а не порожню картку.
    storageLoading = true
    storageScan = null
    try { storageInfo = await App.GetStorageUsage() } catch (e) {
      // Помилка обходу тек (напр. недоступний диск) — дозволяємо повторну
      // спробу, скидаючи прапорець завантаженості.
      storageLoaded = false
      console.error(e)
    }
    try { storagePacks = await App.GetStoragePacks() } catch (e) { storagePacks = [] }
    storageLoading = false
    storageScan = null
  }

  // Швидке видалення збірки прямо зі списку «Встановлені збірки»
  // (вкладка «Пам'ять і кеш»): те саме підтвердження, що й зі сторінки
  // збірки (deletePack), і перерахування розмірів після видалення.
  async function deletePackFromStorage(id: string) {
    await deletePack(id)
    // Розміри могли змінитись — перечитуємо список і загальну статистику
    // (force — навіть якщо вкладку вже відкривали, щоб цифри не застаріли).
    storagePacks = await App.GetStoragePacks()
    loadStorageInfo(true)
    // Видалення збірки могло змінити набір «потрібної Java» — одразу
    // перезапускаємо аналіз, щоб інлайн-підсумок не лишався застарілим
    // (раніше javaAnalyzedOnce не скидався і авто-аналіз більше не йшов).
    unusedJava = null
    analyzeUnusedJava()
  }

  // Очистити логи лаунчера + логів/краш-репортів усіх збірок.
  async function clearPackLogs() {
    const ok = await confirmDialog(t('settings.storage.clearLogs'), t('settings.storage.clearLogsConfirm'), {
      confirmLabel: t('settings.storage.clearLogs'), cancelLabel: t('cancel'), danger: true
    })
    if (!ok) return
    try {
      await App.ClearPackLogs()
      toast(t('settings.storage.logsCleared'), 'success')
      loadStorageInfo(true)
    } catch (e: any) { toast(t('error.withMessage', { message: e }), 'error') }
  }

  // Аналіз невикористовуваної Java (вкладка «Пам'ять і кеш»): бекенд
  // сканує java-<major>/ підтеки і порівнює з потрібними мажорами всіх
  // ВСТАНОВЛЕНИХ збірок. Показуємо лише кандидатів на видалення; якщо
  // користувач підтвердить — видаляємо їх (потрібні перевстановляться
  // автоматично при наступному запуску).
  let unusedJava = $state<UnusedJavaInfo | null>(null)
  let javaBusy = $state(false)
  // javaModalOpen — модалка «Аналіз невикористовуваної Java»: показує ПРОЦЕС
  // аналізу (спінер, поки бекенд сканує java-<major>/ підтеки), а потім —
  // результати (кожен мажор з розміром і статусом «використовується/ні»).
  // javaAnalyzedOnce — авто-аналіз при відкритті вкладки «Пам'ять і кеш»
  // (користувач просив: «немає авто-аналізу, лише по кнопці» — тепер є),
  // щоб результати не перезапускались щоразу при відкритті вкладки.
  let javaModalOpen = $state(false)
  let javaAnalyzedOnce = $state(false)

  async function analyzeUnusedJava() {
    if (javaBusy) return
    javaBusy = true
    unusedJava = null
    javaProgress = null
    try {
      unusedJava = await App.AnalyzeUnusedJava()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      javaBusy = false
      javaProgress = null
    }
  }

  // Відкриття модалки запускає СВІЖИЙ аналіз: поки він іде — у модалці
  // видно анімацію процесу, потім — результати (ТЗ: «модульне вікно, де
  // показує процес аналізу Java з результатами»).
  function openJavaModal() {
    javaModalOpen = true
    analyzeUnusedJava()
  }

  // Авто-аналіз Java при ВХОДІ у вкладку «Пам'ять і кеш» БУДЬ-ЯКИМ шляхом:
  // клік по sub-nav, відновлення вкладки після перезапуску лаунчера тощо.
  // Раніше аналіз стартував лише з onclick sub-nav, тож після відновлення
  // вкладки (settingsTab з uiState) розділ показував лише кнопку «Перевірити».
  $effect(() => {
    if (settingsTab !== 'storage') return
    loadStorageInfo()
    if (!javaAnalyzedOnce) {
      javaAnalyzedOnce = true
      analyzeUnusedJava()
    }
  })

  async function cleanupUnusedJava() {
    if (!unusedJava || unusedJava.unused.length === 0 || javaBusy) return
    const majors = unusedJava.unused.join(', ')
    const ok = await confirmDialog(t('settings.storage.cleanJavaConfirm', { majors }), t('settings.storage.cleanJavaConfirmHint'), {
      confirmLabel: t('settings.storage.cleanJava'), cancelLabel: t('cancel'), danger: true
    })
    if (!ok) return
    javaBusy = true
    try {
      await App.CleanupUnusedJava(unusedJava.unused)
      toast(t('settings.storage.cleanJavaDone', { majors }), 'success')
      unusedJava = null
      javaModalOpen = false
      loadStorageInfo(true)
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      javaBusy = false
    }
  }

  // Шрифт консолі для конкретного типу повідомлення (ТЗ: «шрифт консолі
  // окремо, і режим різних шрифтів для info/warn/error»).
  // Режим 'perType' — кожен тип має свій шрифт (consoleInfoFont/Warn/Error);
  // режим 'all' — один спільний (consoleFontFamily). Використовується і в
  // живому переглядачі консолі в налаштуваннях.
  function consoleFontFor(lvl: 'info' | 'warn' | 'error'): string {
    if (settings.consoleFontMode === 'perType') {
      const f = lvl === 'error' ? settings.consoleErrorFont : lvl === 'warn' ? settings.consoleWarnFont : settings.consoleInfoFont
      return (f || 'mono') === 'sans' ? 'var(--font-sans)' : 'var(--font-mono)'
    }
    return settings.consoleFontFamily === 'sans' ? 'var(--font-sans)' : 'var(--font-mono)'
  }

  // Рядки вкладки «Теки та шляхи»: кожна тека лаунчера = один рядок
  // з назвою, описом, поточним шляхом і кнопкою «Огляд». Папка даних
  // додатково має кнопку «За замовчуванням»; тека конфігурації —
  // фіксована, її не можна змінити (як у старого лаунчера).
  const folderRows = $derived(folderPaths ? [
    { kind: 'baseDir', name: t('settings.folders.baseDir'), desc: t('settings.folders.baseDirDesc'), path: folderPaths.baseDir, custom: '', reset: true, fixed: false },
    { kind: 'installations', name: t('settings.folders.installations'), desc: t('settings.folders.installationsDesc'), path: folderPaths.installations, custom: folderPaths.installationsCustom, reset: false, fixed: false },
    { kind: 'java', name: t('settings.folders.java'), desc: t('settings.folders.javaDesc'), path: folderPaths.java, custom: folderPaths.javaCustom, reset: false, fixed: false },
    { kind: 'cache', name: t('settings.folders.cache'), desc: t('settings.folders.cacheDesc'), path: folderPaths.cache, custom: folderPaths.cacheCustom, reset: false, fixed: false },
    { kind: 'logs', name: t('settings.folders.logs'), desc: t('settings.folders.logsDesc'), path: folderPaths.logs, custom: folderPaths.logsCustom, reset: false, fixed: false },
    { kind: 'config', name: t('settings.folders.config'), desc: t('settings.folders.configDesc'), path: folderPaths.config, custom: '', reset: false, fixed: true }
  ] : [])

  async function clearCache() {
    const ok = await confirmDialog(t('settings.storage.clearCache'), t('settings.storage.clearCacheConfirm'), {
      confirmLabel: t('settings.storage.clearCache'), cancelLabel: t('cancel'), danger: true
    })
    if (!ok) return
    try {
      await App.ClearCache()
      toast(t('settings.storage.cleared'), 'success')
      // force=true: після очищення перераховуємо розміри, навіть якщо
      // вкладку вже відкривали (інакше лишилися б старі цифри).
      loadStorageInfo(true)
    } catch (e: any) { toast(t('error.withMessage', { message: e }), 'error') }
  }

  async function checkUpdates() {
    updateResult = null
    try {
      updateResult = await App.CheckUpdate()
    } catch (e: any) {
      toast(t('settings.about.checkError'), 'error')
    }
  }

  function fmtBytes(b: number): string {
    if (b < 1024) return `${b} Б`
    if (b < 1024 * 1024) return `${(b / 1024).toFixed(1)} КБ`
    if (b < 1024 * 1024 * 1024) return `${(b / 1024 / 1024).toFixed(1)} МБ`
    return `${(b / 1024 / 1024 / 1024).toFixed(2)} ГБ`
  }

  // Форматування ігрового часу: години завжди (gameTimeInHours) або
  // «1 год 30 хв» у звичайному режимі.
  function fmtPlaytime(seconds: number): string {
    if (!seconds || seconds <= 0) return '—'
    const h = Math.floor(seconds / 3600)
    const m = Math.floor((seconds % 3600) / 60)
    if (settings.gameTimeInHours) return `${(seconds / 3600).toFixed(1)} год`
    if (h === 0) return `${m} хв`
    return `${h} год ${m} хв`
  }

  // Консоль тепер живе у вкладці «Консоль» на сторінці збірки
  // (PackInstancePage), а не як віджет на «Моїх збірках». Тож логіки
  // consoleVisible тут більше немає — консоль завжди доступна через вкладку.

  // ── Wizard ──
  // Кроки та поля майстра персистяться на бекенді (setup-progress.json)
  // при КОЖНІЙ зміні — закриття лаунчера на кроці 3 відновить саме крок 3
  // з уже введеними даними, а не почне спочатку. Сам майстер живе у
  // Wizard.svelte, тут лише показ/завершення.
  async function startWizard() {
    const firstRun = await App.IsFirstRun()
    if (!firstRun) return
    showWizard = true
  }

  async function wizardFinished() {
    showWizard = false
    toast(t('settings.saved'), 'success')
    await loadData()
  }

  // ── Login modal ──
  async function loginMsStart() {
    if (loginMsStep === 'waiting') return
    loginMsStep = 'waiting'
    try {
      const url = await App.StartMSLogin()
      App.OpenExternal(url)
      await App.AwaitMSLogin()
      accounts = (await App.GetAccounts()) ?? []
      settings = await App.GetSettings()
      activeAccountId = settings.activeAccountId ?? ''
      refreshHeads()
      loginMsStep = 'idle'; modalLogin = false; loginGate = false
    } catch (e: any) {
      loginMsStep = 'idle'
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  function loginMsCancel() {
    App.CancelMSLogin()
    loginMsStep = 'idle'
  }

  async function loginPirate() {
    if (!pirateUsername.trim()) return
    try {
      await App.LoginPirate(pirateUsername)
      accounts = (await App.GetAccounts()) ?? []
      settings = await App.GetSettings()
      activeAccountId = settings.activeAccountId ?? ''
      refreshHeads()
      pirateUsername = ''; modalLogin = false; loginGate = false
    } catch (e: any) { toast(t('error.withMessage', { message: e }), 'error') }
  }

  function winMin()  { Window.Minimise() }
  function winMax()  {
    if (isMaximized) { Window.UnMaximise(); isMaximized = false }
    else { Window.Maximise(); isMaximized = true }
  }
  // Закриття через кнопку X топбара: йде через бекенд-метод QuitLauncher,
  // який виставляє прапорець consoleAppQuitting — інакше відкрите вікно
  // консолі (WindowClosing-хук) заблокувало б вихід з програми.
  function winClose() { App.QuitLauncher() }

  async function removeAccount(id: string) {
    const ok = await confirmDialog(t('acc.deleteConfirm'), t('acc.delete'), {
      confirmLabel: t('acc.delete'), cancelLabel: t('cancel'), danger: true
    })
    if (!ok) return
    await App.RemoveAccount(id)
    accounts = await App.GetAccounts()
    settings = await App.GetSettings()
    activeAccountId = settings.activeAccountId ?? ''
    refreshHeads()
    toast(t('settings.saved'), 'success')
    // Видалили останній акаунт — повертаємо "ворота входу".
    if (accounts.length === 0) {
      loginGate = true; loginMsStep = 'idle'; modalLogin = true
    }
  }

  onMount(async () => {
    // Відновлюємо розмір/стан sidebar з localStorage (ресайз переживає
    // перезапуск). Після зміни мови/акценту нічого не залежить від порядку
    // цих читань — вони чисті синхронні значення до першого рендера.
    try {
      const w = parseInt(localStorage.getItem('shaurma.sidebarWidth') || '', 10)
      if (w >= 220 && w <= 400) sidebarWidth = w
      sidebarCollapsed = localStorage.getItem('shaurma.sidebarCollapsed') === '1'
      sidebarOnlyPinned = localStorage.getItem('shaurma.sidebarOnlyPinned') === '1'
      const sc = localStorage.getItem('shaurma.sidebarScale')
      if (sc === 'small' || sc === 'large' || sc === 'normal') sidebarScale = sc
    } catch { /* ок */ }
    loadPackLayout()
    await startWizard()
    await loadData()
    // Скрол «простих» сторінок зберігається у localStorage; при закритті
    // лаунчера (pagehide) фіксуємо фінальну позицію.
    const contentEl = document.querySelector('.content') as HTMLElement | null
    contentEl?.addEventListener('scroll', onMainScroll, { passive: true })
    window.addEventListener('pagehide', persistMainScrollNow)
    // Прогрес синхронізації Шаурма-збірок (RunnerProgress від sync.Runner).
    on('sync:progress', (p: SyncProgress) => { applySyncProgress(p) })
    // Скін на акаунті змінився (гардероб) — перерендерюємо аватарки одразу:
    // бекенд уже скинув кеш голови (avatar.Service.Invalidate), тож
    // GetAccountHead поверне голову вже з нового скіна.
    on('avatar:changed', () => refreshHeads())
    on('download:complete', () => { loadData() })
    on('download:error', (err: string) => { installerLog = t('error.withMessage', { message: err }) })
    on('installer:log', (msg: string) => { installerLog = msg })
    // Стадії запуску (checking/worker_update/java/loader) — сегментний
    // прогрес-бар на сторінці збірки читає launchProgress[instanceId].
    // done=true (успіх або помилка) прибирається із мапи за секунду, щоб
    // прогрес встиг домалюватись на 100% перед тим, як зникнути.
    on('launch:progress', (p: LaunchProgress) => {
      launchProgress = { ...launchProgress, [p.instanceId]: p }
      if (p.done) {
        const id = p.instanceId
        setTimeout(() => {
          const next = { ...launchProgress }
          if (next[id] === p) { delete next[id]; launchProgress = next }
        }, p.error ? 4000 : 800)
      }
    })
    // Стани збірок: реєстр оновлено / збірка стартувала / завершилась.
    on('builds:changed', () => refreshBuilds())
    on('build:started', (e: BuildStartEvent) => patchBuild(e.buildId, b => ({ ...b, status: 'running', runningOn: undefined })))
    on('build:exit', () => refreshBuilds())
    // Закриття лаунчера з активною качкою → модалка попередження.
    on('sync:quit-warning', (packIDs: string[]) => { confirmQuitWhileSyncing(packIDs) })
    // Живий прогрес сканування «Пам'ять і кеш»: яка тека/збірка зараз
    // рахується (storage:usage-scan / packs-scan) і який мажор Java
    // сканується (storage:java-scan) — прогрес-бари замість пустого розділу.
    const onStorageScan = (p: { done: number; total: number; label: string }) => { storageScan = p }
    on('storage:usage-scan', onStorageScan)
    on('storage:packs-scan', onStorageScan)
    on('storage:java-scan', (p: { done: number; total: number; major: number }) => { javaProgress = p })
  // Консоль гри більше НЕ дублює рядки тут — нею займається компонент
  // GameConsole (підписки на console:line всередині нього, з віртуалізацією
  // та обрізанням буфера за consoleMaxLines). Консоль тепер живе у вкладці
  // «Консоль» на сторінці збірки. Тут лишається лише реакція на game:exit.
  // Гра завершилась: якщо лаунчер був схований у трей (закритий під
  // час гри), показуємо вікно назад автоматично. Ненульовий код виходу
  // = краш — вискакує модальне вікно з повідомленням (замість старого
  // банера на «Моїх збірках») поверх будь-якої сторінки.
  on('game:exit', (code: number) => {
    gameRunning = false
    if (code && code !== 0) {
      gameCrashed = true
      // Модалка краху: показуємо, якщо користувач не відмітив
      // «не показувати знову».
      try {
        crashBannerHidden = localStorage.getItem('shaurma.crashBannerHidden') === '1'
      } catch (e) { crashBannerHidden = false }
    }
    if (settings.exitOnGameClose) {
      // «Вийти з лаунчера, коли гра закрилась» — закриваємось повністю
      // після короткої паузи, щоб встиг записатись ігровий час.
      setTimeout(() => Application.Quit(), 500)
      return
    }
    App.RestoreWindowAfterGame()
    loadPlaytimes()
  })

  async function loadPlaytimes() {
    try { playtimes = await App.GetPlaytime() } catch (e) { console.error(e) }
  }
  })
</script>

<!-- ═══════════════════ FULL SCREEN WIZARD ═══════════════════ -->
{#if showWizard}
  <div class="wiz-wrap">
    <Wizard onFinished={wizardFinished} />
  </div>
{/if}

<!-- ═══════════════════ MAIN UI ═══════════════════ -->
<div class="launcher-window" class:maximized={isMaximized} id="win">
  <aside class="sidebar" class:collapsed={sidebarCollapsed} class:sb-sm={sidebarScale === 'small'} class:sb-lg={sidebarScale === 'large'} style="--sb-w:{sidebarWidth}px">
    <button class="sidebar-collapse-btn" onclick={toggleSidebar} title={t('sidebar.toggle')}>
      <i class="ti ti-chevron-left"></i>
    </button>
    <!-- Ресайз-хендл: тягни правий край, щоб розширити/звузити sidebar.
         Зберігається в localStorage. Стиснутий sidebar не ресайзиться. -->
    {#if !sidebarCollapsed}
      <div class="sb-resize" class:dragging={sbResizing} onmousedown={startSidebarResize} title={t('sidebar.resize')}></div>
    {/if}
      <div class="logo-area">
        <div class="logo-icon-box logo-icon-img"><img src={appIcon} alt="Shaurma Launcher"/></div>
        <div class="logo-text">
          <span class="logo-title">Shaurma</span>
          <span class="logo-sub">LAUNCHER</span>
        </div>
      </div>

      <div class="nav-list" style="padding-top:6px">
        {#each (wardrobeVisible ? ['instances', 'browser', 'wardrobe', 'settings'] : ['instances', 'browser', 'settings']) as page}
          <div class="nav-item" class:active={currentPage === page} onclick={() => navigate(page)}>
            <i class="ti {pageIcons[page]}"></i>
            <span>{t(pageTitles[page])}</span>
          </div>
        {/each}
      </div>

    <!-- Кнопка «Створити збірку» у sidebar — завжди видима (і в base, і в
         стисненому вигляді): плюс, що відкриває майстер збірки. -->
    <button class="sb-create" onclick={openCreatePack} title={t('instances.create')}>
      <i class="ti ti-plus"></i> <span>{t('instances.create')}</span>
    </button>

    {#if isShaurmaEdition && builds.length > 0}
      <div class="pack-switch">
        <button class:active={instanceTab === 'shaurma'} onclick={() => instanceTab = 'shaurma'}>
          <i class="ti ti-flame"></i> <span>{t('shaurma')}</span>
        </button>
        <button class="violet" class:active={instanceTab === 'custom'} onclick={() => instanceTab = 'custom'}>
          <i class="ti ti-tools"></i> <span>{t('custom')}</span>
        </button>
      </div>

      <div class="sb-packs">
        {#if tabPinned.length > 0}
          <div class="sb-pin-label"><i class="ti ti-pin"></i> {t('packs.pinned')}</div>
        {/if}
        {#each tabPinned as bv (bv.build.id)}
          {@const pk = bv.build}
          {@const status = bv.status}
          {@const prog = bv.progress}
          {@const pcol = pk.color || 'var(--orange)'}
          {@const onOther = bv.runningOn ? accForName(bv.runningOn) : undefined}
          {@const icon = packAsset(pk.iconUrl)}
          {@const lp = launchProgress[pk.id]}
          {@const sti = lp && !lp.done ? launchStageIcon(lp.stage) : null}
          {@const limg = lp && !lp.done && lp.stage === 'loader' ? loaderImgFor(pk.loaderType) : undefined}
          {@const glow = glowClass(bv, lp)}
          {@const isDragSrc = dragSrcId === pk.id}
          {@const isDropTarget = dropTargetId === pk.id}
          <div class="sb-pack" class:active={currentPage === 'instances' && openPackId === pk.id} class:running={status === 'running'} class:glow-green={glow === 'glow-green'} class:glow-orange={glow === 'glow-orange'} class:glow-red={glow === 'glow-red'} class:glow-blue={glow === 'glow-blue'} class:drag-src={isDragSrc} class:drop-before={isDropTarget && dropPos === 'before'} class:drop-after={isDropTarget && dropPos === 'after'} data-pack-id={pk.id} onpointerdown={(e) => packPointerDown(e, pk.id, pcol)} onpointermove={packPointerMove} onpointerup={pointerDragEnd} oncontextmenu={(e) => openCtxMenu(e, pk.id)} onclick={(e: any) => { if (suppressPackClick) return; if (e.button === 0) openPackPage(pk.id) }} style="--pcol:{pcol};--drop-c:{dragPackColor}" title={sidebarCollapsed ? pk.name : undefined}>
            <div class="sp-ic" class:has-icon={!!icon} class:blink={status === 'downloading' || status === 'updating'} style="--pcol:{pcol}">
              {#if icon}
                <img src={icon} alt="" loading="lazy" />
              {:else}
                <i class="ti {pk.icon || 'ti-puzzle'}"></i>
              {/if}
            </div>
            <div class="sp-info">
              <div class="sp-name">{pk.name}</div>
              <div class="sp-sub" class:live={!!lp && !lp.done || status === 'downloading' || status === 'updating'}>
                {#if lp && !lp.done}
                  <i class="ti ti-loader spin"></i>
                {:else if status === 'downloading' || status === 'updating'}
                  <i class="ti {prog?.mode === 'paused' ? 'ti-player-pause' : 'ti-download'}"></i>
                {/if}
                <span>{packSubText(bv, lp)}</span>
              </div>
              {#if lp && !lp.done && lp.message}
                <div class="sp-stage"><i class="ti ti-file"></i> {lp.message}</div>
              {/if}
              {#if onOther}
                <div class="sp-run">
                  <img src={headFor(onOther.uuid)} alt="" />
                  <span>{t('pack.runningOn', { acc: bv.runningOn ?? '' })}</span>
                </div>
              {/if}
            </div>
            {#if sti}
              <!-- Під час встановлення Java/лоадера кнопка замінюється
                   на «живу» іконку стадії (без функцій): червона Java,
                   іконка лоадера збірки, крутіння оновлення чи лупа. -->
              <div class="sp-st st-{lp.stage}" title={launchStageLabel(lp.stage)}>
                {#if limg}
                  <img src={limg} alt="" />
                {:else}
                  <i class="ti {sti.icon}"></i>
                {/if}
              </div>
            {:else}
              <button class="sp-btn sp-{spBtnClass(bv)}" onclick={(e: any) => { e.stopPropagation(); packBtnAction(bv) }} title={spBtnTitle(bv)}>
                {#if status === 'running'}<i class="ti ti-player-stop"></i>
                {:else if status === 'ready'}<i class="ti ti-player-play"></i>
                {:else if status === 'needs-update'}<i class="ti ti-refresh"></i>
                {:else if status === 'not-installed'}<i class="ti ti-download"></i>
                {:else if prog?.mode === 'paused'}<i class="ti ti-player-play"></i>
                {:else if status === 'downloading' || status === 'updating'}<i class="ti ti-player-pause"></i>
                    {:else}<i class="ti ti-loader spin"></i>
                {/if}
              </button>
            {/if}
            {#if lp && !lp.done}
              <div class="sp-prog"><div class="sp-prog-fill" style="width:{launchStagePct(lp)}%;background:{launchStageColor(lp.stage)}"></div></div>
            {:else if prog && (status === 'downloading' || status === 'updating')}
              <div class="sp-prog"><div class="sp-prog-fill" style="width:{prog.percent}%"></div></div>
            {/if}
          </div>
        {/each}
        {#each (sidebarOnlyPinned ? [] : tabUnpinned) as bv (bv.build.id)}
          {@const pk = bv.build}
          {@const status = bv.status}
          {@const prog = bv.progress}
          {@const pcol = pk.color || 'var(--orange)'}
          {@const onOther = bv.runningOn ? accForName(bv.runningOn) : undefined}
          {@const icon = packAsset(pk.iconUrl)}
          {@const lp = launchProgress[pk.id]}
          {@const sti = lp && !lp.done ? launchStageIcon(lp.stage) : null}
          {@const limg = lp && !lp.done && lp.stage === 'loader' ? loaderImgFor(pk.loaderType) : undefined}
          {@const glow = glowClass(bv, lp)}
          {@const isDragSrc = dragSrcId === pk.id}
          {@const isDropTarget = dropTargetId === pk.id}
          <div class="sb-pack" class:active={currentPage === 'instances' && openPackId === pk.id} class:running={status === 'running'} class:glow-green={glow === 'glow-green'} class:glow-orange={glow === 'glow-orange'} class:glow-red={glow === 'glow-red'} class:glow-blue={glow === 'glow-blue'} class:drag-src={isDragSrc} class:drop-before={isDropTarget && dropPos === 'before'} class:drop-after={isDropTarget && dropPos === 'after'} data-pack-id={pk.id} onpointerdown={(e) => packPointerDown(e, pk.id, pcol)} onpointermove={packPointerMove} onpointerup={pointerDragEnd} oncontextmenu={(e) => openCtxMenu(e, pk.id)} onclick={(e: any) => { if (suppressPackClick) return; if (e.button === 0) openPackPage(pk.id) }} style="--pcol:{pcol};--drop-c:{dragPackColor}" title={sidebarCollapsed ? pk.name : undefined}>
            <div class="sp-ic" class:has-icon={!!icon} class:blink={status === 'downloading' || status === 'updating'} style="--pcol:{pcol}">
              {#if icon}
                <img src={icon} alt="" loading="lazy" />
              {:else}
                <i class="ti {pk.icon || 'ti-puzzle'}"></i>
              {/if}
            </div>
            <div class="sp-info">
              <div class="sp-name">{pk.name}</div>
              <div class="sp-sub" class:live={!!lp && !lp.done || status === 'downloading' || status === 'updating'}>
                {#if lp && !lp.done}
                  <i class="ti ti-loader spin"></i>
                {:else if status === 'downloading' || status === 'updating'}
                  <i class="ti {prog?.mode === 'paused' ? 'ti-player-pause' : 'ti-download'}"></i>
                {/if}
                <span>{packSubText(bv, lp)}</span>
              </div>
              {#if lp && !lp.done && lp.message}
                <div class="sp-stage"><i class="ti ti-file"></i> {lp.message}</div>
              {/if}
              {#if onOther}
                <div class="sp-run">
                  <img src={headFor(onOther.uuid)} alt="" />
                  <span>{t('pack.runningOn', { acc: bv.runningOn ?? '' })}</span>
                </div>
              {/if}
            </div>
            {#if sti}
              <!-- Під час встановлення Java/лоадера кнопка замінюється
                   на «живу» іконку стадії (без функцій): червона Java,
                   іконка лоадера збірки, крутіння оновлення чи лупа. -->
              <div class="sp-st st-{lp.stage}" title={launchStageLabel(lp.stage)}>
                {#if limg}
                  <img src={limg} alt="" />
                {:else}
                  <i class="ti {sti.icon}"></i>
                {/if}
              </div>
            {:else}
              <button class="sp-btn sp-{spBtnClass(bv)}" onclick={(e: any) => { e.stopPropagation(); packBtnAction(bv) }} title={spBtnTitle(bv)}>
                {#if status === 'running'}<i class="ti ti-player-stop"></i>
                {:else if status === 'ready'}<i class="ti ti-player-play"></i>
                {:else if status === 'needs-update'}<i class="ti ti-refresh"></i>
                {:else if status === 'not-installed'}<i class="ti ti-download"></i>
                {:else if prog?.mode === 'paused'}<i class="ti ti-player-play"></i>
                {:else if status === 'downloading' || status === 'updating'}<i class="ti ti-player-pause"></i>
                    {:else}<i class="ti ti-loader spin"></i>
                {/if}
              </button>
            {/if}
            {#if lp && !lp.done}
              <div class="sp-prog"><div class="sp-prog-fill" style="width:{launchStagePct(lp)}%;background:{launchStageColor(lp.stage)}"></div></div>
            {:else if prog && (status === 'downloading' || status === 'updating')}
              <div class="sp-prog"><div class="sp-prog-fill" style="width:{prog.percent}%"></div></div>
            {/if}
          </div>
        {/each}
        {#if sidebarOnlyPinned && tabPinned.length === 0}
          <div class="sb-empty-hint"><i class="ti ti-pin-off"></i> {t('sidebar.onlyPinnedEmpty')}</div>
        {/if}
      </div>
    {/if}

    <div class="sidebar-footer" onclick={toggleAccDropdown}>
      {#if accDropdownOpen}
        <div class="acc-dropdown" onclick={(e: any) => e.stopPropagation()}>
          <div class="acc-dd-label">{t('accounts')}</div>
          {#each accounts as acc}
            <div class="acc-dd-item" class:active={acc.id === activeAccountId} onclick={() => switchAccount(acc.id)}>
              <div class="adi-av"><img src={headFor(acc.uuid)} alt="" /></div>
              <div class="adi-info">
                <div class="adi-name">{acc.username}</div>
                <div class="adi-sub" class:licensed={acc.isLicensed} class:pirated={!acc.isLicensed}>
                  <i class="ti {acc.isLicensed ? 'ti-rosette-discount-check' : 'ti-alert-triangle'}"></i>
                  {acc.isLicensed ? t('licensed') : t('notLicensed')}
                </div>
              </div>
              <button class="acc-dd-remove" onclick={(e: any) => { e.stopPropagation(); removeAccount(acc.id) }} title={t('acc.delete')}>
                <i class="ti ti-trash"></i>
              </button>
            </div>
          {/each}
          <div class="acc-dd-sep"></div>
          <div class="acc-dd-add" onclick={() => { accDropdownOpen = false; modalLogin = true; loginMsStep = 'idle' }}>
            <i class="ti ti-plus"></i> {t('addAccount')}
          </div>
        </div>
      {/if}
      <div class="user-avatar"><img src={headFor(activeAccount()?.uuid)} alt="" /></div>
      <div class="user-meta">
        <div class="un">{activeAccount()?.username || t('notLoggedIn')}</div>
        <div class="ue">{activeAccount() ? t('account.type.' + activeAccount()?.type) : t('offline')}</div>
      </div>
      <button class="dot-btn" title={t('accounts')}><i class="ti ti-chevron-up"></i></button>
    </div>
  </aside>

  <div class="main">
    <div class="topbar">
      <!-- Іконка РОЗДІЛУ (не лаунчера): на сторінці збірки показуємо
           розділ «Мої збірки», бо назва самої збірки у банері на фоні
           сторінки; «Нова збірка» — плюс. Заголовок + опис розділу живуть
           тут (перенесені з контенту сторінок), тому панель вища і має
           відступи, щоб контроли вікна «дихали». -->
      <div class="tb-icon"><i class="ti {topbarIcon}"></i></div>
      <div class="tb-titles">
        <div class="topbar-title">{topbarTitle}</div>
        {#if topbarSub}<div class="topbar-sub">{topbarSub}</div>{/if}
      </div>
      <div class="tb-sep"></div>
      <div class="topbar-crumbs"><i class="ti ti-chevron-right"></i> {showCreatePack ? (createPackTab === 'import' ? t('createPack.tab.import') : t('createPack.tab.manual')) : t(pageCrumbs[currentPage])}</div>
      <div class="topbar-spacer"></div>
      <div class="search-top no-drag" onclick={() => navigate('browser')}>
        <i class="ti ti-search"></i>
        <span>{t('topbar.search')}</span>
        <kbd>Ctrl+K</kbd>
      </div>
      <div class="ctrl-box">
        <button class="ctrl-btn" title={t('win.minimize')} onclick={winMin}><i class="ti ti-minus"></i></button>
        <button class="ctrl-btn" title={t('win.maximize')} onclick={winMax}><i class="ti {isMaximized ? 'ti-copy' : 'ti-square'}"></i></button>
        <button class="ctrl-btn close" title={t('win.close')} onclick={winClose}><i class="ti ti-x"></i></button>
      </div>
    </div>

    <div class="content {currentPage === 'wardrobe' ? 'content-no-scroll' : ''}">
      {#if currentPage === 'instances' && showCreatePack}
        <CreatePackPage {settings} initialTab={createPackTab} onCancel={closeCreatePack} onCreated={onPackCreated} />
      {:else if currentPage === 'instances' && openPack}
        <!-- Кожна збірка = своє власне «вікно»: {#key openPack.id} перемонтовує
             сторінку при перемиканні збірок через сайдбар, щоб під старою
             сторінкою (напр. екран завантаження попередньої збірки) не лишалась
             видимою. Прогрес качки не губиться — він живе глобально у
             downloadByPack/buildView.progress і показується при поверненні. -->
        {#key openPack.id}
          <PackInstancePage
            pack={openPack}
            {settings}
            buildView={builds.find(b => b.build.id === openPack.id) ?? null}
            launchProgress={launchProgress[openPack.id] ?? null}
            playtimeSeconds={playtimes[openPack.id] ?? 0}
            {gameRunning}
            {consoleSignal}
            onBack={() => openPackId = null}
            onLaunch={launchInstance}
            onStop={stopGame}
            onDownload={downloadPack}
            onDelete={deletePack}
            onPackUpdated={refreshBuilds}
            {fmtPlaytime}
          />
        {/key}
      {:else if currentPage === 'instances'}
        <div class="page-actions">
          <button class="btn btn-ghost" onclick={() => navigate('browser')}><i class="ti ti-world-search"></i> {t('search')}</button>
          <button class="btn btn-ghost" onclick={openImportPack}><i class="ti ti-download"></i> {t('createPack.tab.import')}</button>
          <button class="btn btn-primary" onclick={openCreatePack}><i class="ti ti-plus"></i> {t('instances.create')}</button>
        </div>

        <div class="tabbar">
          {#if isShaurmaEdition && builds.length > 0}
            <button class="tab-btn" class:active={instanceTab === 'shaurma'} onclick={() => instanceTab = 'shaurma'}>
              <i class="ti ti-flame"></i> {t('shaurma')} <span class="num">{shaurmaBuildsCount}</span>
            </button>
          {/if}
          <button class="tab-btn" class:active={instanceTab === 'custom'} onclick={() => instanceTab = 'custom'}>
            <i class="ti ti-tools"></i> {t('custom')} <span class="num">{customBuildsCount}</span>
          </button>
        </div>

        <div class="tile-grid">
          {#each tabBuilds as bv (bv.build.id)}
              {@const pk = bv.build}
              {@const status = bv.status}
              {@const prog = bv.progress}
              {@const pcol = pk.color || 'var(--orange)'}
              {@const onOther = bv.runningOn ? accForName(bv.runningOn) : undefined}
              {@const icon = packAsset(pk.iconUrl)}
              {@const lp = launchProgress[pk.id]}
              {@const limg = lp && !lp.done && lp.stage === 'loader' ? loaderImgFor(pk.loaderType) : undefined}
              {@const isDragSrc = dragSrcId === pk.id}
              {@const isDropTarget = dropTargetId === pk.id}
              <div class="inst-tile" class:running={status === 'running'} class:pin-badge={isPinned(pk.id)} class:drag-src={isDragSrc} class:drop-before={isDropTarget && dropPos === 'before'} class:drop-after={isDropTarget && dropPos === 'after'} data-pack-id={pk.id} onpointerdown={(e) => packPointerDown(e, pk.id, pcol)} onpointermove={packPointerMove} onpointerup={pointerDragEnd} oncontextmenu={(e) => openCtxMenu(e, pk.id)} style="--pcol:{pcol};--drop-c:{dragPackColor}">
                <div class="it-ic" class:has-icon={!!icon} class:blink={status === 'downloading' || status === 'updating'} style="--pcol:{pcol}">
                  {#if icon}
                    <img src={icon} alt="" loading="lazy" />
                  {:else}
                    <i class="ti {pk.icon || 'ti-puzzle'}" style="color:var(--pcol)"></i>
                  {/if}
                </div>
                <div class="it-info" onclick={() => openPackPage(pk.id)}>
                  <div class="it-name">{pk.name}</div>
                  <div class="it-sub" class:live={!!lp && !lp.done || status === 'downloading' || status === 'updating'}>
                    {#if lp && !lp.done}
                      <i class="ti ti-loader spin"></i> {launchStageLabel(lp.stage)}
                    {:else if status === 'downloading' || status === 'updating'}
                      <i class="ti {prog?.mode === 'paused' ? 'ti-player-pause' : 'ti-download'}"></i> {packSubText(bv, lp)}
                    {:else}
                      <i class="ti ti-cube"></i> {pk.loaderType} {pk.mcVersion}
                    {/if}
                  </div>
                  {#if settings.showGameTime}
                    <div class="it-time"><i class="ti ti-clock"></i> {fmtPlaytime(playtimes[pk.id] ?? 0)}</div>
                  {/if}
                  {#if onOther}
                    <div class="it-run"><img src={headFor(onOther.uuid)} alt="" /> {t('pack.runningOn', { acc: bv.runningOn ?? '' })}</div>
                  {/if}
                  {#if lp && !lp.done}
                    <div class="it-prog"><div class="it-prog-fill" style="width:{launchStagePct(lp)}%;background:{launchStageColor(lp.stage)}"></div></div>
                    {#if lp.message}<div class="it-stage"><i class="ti ti-file"></i> {lp.message}</div>{/if}
                  {:else if prog && (status === 'downloading' || status === 'updating')}
                    <div class="it-prog"><div class="it-prog-fill" style="width:{prog.percent}%"></div></div>
                  {/if}
                </div>
                <button class="btn btn-primary btn-sm" onclick={(e: any) => { e.stopPropagation(); packBtnAction(bv) }} title={spBtnTitle(bv)}>
                  {#if limg}
                    <img src={limg} alt="" style="width:14px;height:14px;object-fit:contain" />
                  {:else if lp && !lp.done && lp.stage === 'java'}<i class="ti ti-coffee" style="color:#ff6b6b"></i>
                  {:else if lp && !lp.done && lp.stage === 'worker_update'}<i class="ti ti-refresh spin"></i>
                  {:else if lp && !lp.done && lp.stage === 'checking'}<i class="ti ti-search"></i>
                  {:else if lp && !lp.done}<i class="ti ti-loader spin"></i>
                  {:else if status === 'running'}<i class="ti ti-player-stop"></i>
                  {:else if status === 'ready'}<i class="ti ti-player-play"></i>
                  {:else if status === 'needs-update'}<i class="ti ti-refresh"></i>
                  {:else if status === 'not-installed'}<i class="ti ti-download"></i>
                  {:else if prog?.mode === 'paused'}<i class="ti ti-player-play"></i>
                  {:else if status === 'downloading' || status === 'updating'}<i class="ti ti-player-pause"></i>
                  {:else}<i class="ti ti-loader spin"></i>
                  {/if}
                  <span>{tileBtnLabel(bv, lp)}</span>
                </button>
              </div>
            {/each}
            {#if tabBuilds.length === 0}
              <div class="add-inst-card" style="min-height:auto; padding:12px; flex-direction:row; gap:10px;" onclick={openCreatePack} role="button" tabindex="0">
                <i class="ti ti-plus" style="font-size:22px"></i>
                <div style="text-align:left"><div class="ai-l">{t('instances.create')}</div></div>
              </div>
            {/if}
          </div>

        {#if settings.showTotalGameTime}
          <div class="total-time-banner"><i class="ti ti-clock-hour-4"></i> {t('instances.totalTime')}: <b>{fmtPlaytime(Object.values(playtimes).reduce((a, b) => a + (b || 0), 0))}</b></div>
        {/if}

        {#if installerLog}
          <div style="font-size:11px;color:var(--text-mute);font-family:var(--font-mono);padding:8px 0">{installerLog}</div>
        {/if}
      {/if}

      {#if currentPage === 'browser'}
        <div class="browser-top">
          <div class="browser-search">
            <i class="ti ti-search"></i>
            <input placeholder={t('browser.searchPlaceholder')} bind:value={searchQuery}
                   onkeydown={(e: any) => e.key === 'Enter' && searchContent()}/>
          </div>
          <button class="btn btn-primary" onclick={searchContent}><i class="ti ti-search"></i> {t('search')}</button>
        </div>
        <div class="bw-grid">
          {#each searchResults as item}
            <div class="bw-card">
              <div class="bw-ic" style="background:linear-gradient(135deg,var(--violet),var(--orange))">
                <i class="ti ti-cube"></i>
              </div>
              <div class="bw-info">
                <div class="bw-name">{item.name} <span class="chip g">{item.source}</span></div>
                <div class="bw-meta">{item.type} · {item.loader || ''} {item.mcVersion || ''}</div>
                <div class="bw-desc">{item.description}</div>
                <div class="bw-stats"><span><i class="ti ti-download"></i> {(item.downloads / 1000).toFixed(1)}K</span></div>
              </div>
              <div class="bw-acts">
                <button class="btn btn-primary btn-sm"><i class="ti ti-download"></i> {t('install')}</button>
              </div>
            </div>
          {/each}
        </div>
      {/if}

      {#if currentPage === 'wardrobe'}
        <Wardrobe activeAccount={activeAccount()} />
      {/if}

      {#if currentPage === 'settings'}
        <div class="page-actions settings-actions">
          <span class="auto-save-hint"><i class="ti ti-device-floppy"></i> {t('settings.autoSave')}</span>
        </div>
        <div class="settings-grid">
          <div class="sub-nav">
            {#each [...settingsGroups, 'accounts'] as tab}
              <div class="sni" class:active={settingsTab === tab} onclick={() => { settingsTab = tab; if (tab === 'folders') loadFolderPaths() }}>
                <i class="ti {tab === 'accounts' ? 'ti-users' : groupIcons[tab]}"></i>
                {tab === 'accounts' ? t('settings.tab.accounts') : t(groupTitleKeys[tab])}
              </div>
            {/each}
          </div>
          <div class="sub-content">
            {#if settingsTab === 'accounts'}
              <div class="card">
                <div class="card-head"><div class="ci violet"><i class="ti ti-users"></i></div>
                  <div class="ctw"><div class="ct">{t('settings.accounts.title')}</div></div>
                </div>
                {#each accounts as acc}
                  <div class="row">
                    <div class="acc-row-av"><img src={headFor(acc.uuid)} alt="" /></div>
                    <div class="row-label"><div class="rl-name">{acc.username} <span class="chip {acc.isLicensed ? 'g' : ''}">{acc.type}</span> {#if acc.id === activeAccountId}<span class="chip on">{t('acc.active')}</span>{/if}</div></div>
                    {#if acc.id !== activeAccountId}
                      <button class="btn btn-ghost btn-sm" onclick={() => switchAccount(acc.id)}><i class="ti ti-user-check"></i> {t('acc.use')}</button>
                    {/if}
                    <button class="btn btn-danger btn-sm" onclick={() => removeAccount(acc.id)}><i class="ti ti-trash"></i></button>
                  </div>
                {/each}
                <div class="row">
                  <button class="btn btn-primary btn-sm" onclick={() => { modalLogin = true; loginMsStep = 'idle' }}><i class="ti ti-plus"></i> {t('add')}</button>
                </div>
              </div>
            {:else if settingsTab === 'storage'}
              <div class="card">
                <div class="card-head"><div class="ci green"><i class="ti ti-database"></i></div>
                  <div class="ctw"><div class="ct">{t('settings.storage.title')}</div></div>
                  <button class="btn btn-danger btn-sm" onclick={clearCache}><i class="ti ti-eraser"></i> {t('settings.storage.clearCache')}</button>
                </div>
                <div style="padding:18px 20px">
                  {#if storageInfo}
                    <div class="storage-bar">
                      {#each storageInfo.entries as e}
                        {#if e.bytes > 0}
                          <div class="sb-seg" style="width:{Math.max(2, e.bytes * 100 / Math.max(1, storageInfo.totalBytes))}%" title="{e.name}: {fmtBytes(e.bytes)}"></div>
                        {/if}
                      {/each}
                    </div>
                    <div class="storage-legend">
                      {#each storageInfo.entries as e}
                        <div class="sl-item"><span class="sl-dot"></span><span class="sl-name">{e.name}</span><span class="sl-size">{fmtBytes(e.bytes)}</span></div>
                      {/each}
                    </div>
                    <div class="storage-total">
                      <span>{t('settings.storage.total')}: <b>{fmtBytes(storageInfo.totalBytes)}</b></span>
                      <span>{t('settings.storage.free')}: <b>{(storageInfo.freeDiskMB / 1024).toFixed(1)} ГБ</b></span>
                    </div>
                    <div class="storage-total" style="justify-content:flex-start;gap:10px;margin-top:14px;border-top:1px solid var(--border);padding-top:12px">
                      <button class="btn btn-ghost btn-sm" onclick={clearPackLogs}><i class="ti ti-file-text"></i> {t('settings.storage.clearLogs')}</button>
                    </div>
                    <div style="margin-top:16px">
                      <div class="rl-name" style="font-size:12px;font-weight:700;margin-bottom:4px">{t('settings.storage.packs')}</div>
                      <div class="rl-sub" style="font-size:11px;margin-bottom:10px">{t('settings.storage.packsHint')}</div>
                      {#if storagePacks.length === 0}
                        <div style="font-size:12px;color:var(--text-mute)">{t('settings.storage.noPacks')}</div>
                      {:else}
                        <div class="storage-packs">
                          {#each storagePacks as sp}
                            {@const sic = packAsset(sp.iconUrl)}
                            <div class="sp-row" title={sp.name} style="--pcol:{sp.color || 'var(--orange)'}">
                              <!-- Іконка + колір збірки (як картка sidebar): БАГ —
                                   раніше рядок був голим текстом без стилю. -->
                              <span class="sp-ic" class:has-icon={!!sic} style="--pcol:{sp.color || 'var(--orange)'}">
                                {#if sic}<img src={sic} alt="" />{:else}<i class="ti ti-package"></i>{/if}
                              </span>
                              <span class="sp-name">{sp.name}</span>
                              <span class="sp-size">{fmtBytes(sp.bytes)}</span>
                              <button class="icon-btn danger" title={t('pack.delete')} onclick={() => deletePackFromStorage(sp.id)}><i class="ti ti-trash"></i></button>
                            </div>
                          {/each}
                        </div>
                      {/if}
                    </div>
                    <div style="margin-top:16px;border-top:1px solid var(--border);padding-top:14px">
                      <div class="rl-name" style="font-size:12px;font-weight:700;margin-bottom:4px"><i class="ti ti-coffee" style="color:var(--orange)"></i> {t('settings.storage.cleanJava')}</div>
                      <div class="rl-sub" style="font-size:11px;margin-bottom:10px">{t('settings.storage.cleanJavaHint')}</div>
                      <!-- Авто-аналіз при відкритті вкладки: поки бекенд сканує
                           підтеки java-<major> — показуємо анімацію процесу. -->
                      {#if javaBusy && !unusedJava}
                        <!-- Прогрес-бар аналізу: бекенд шле storage:java-scan
                             (який мажор зараз сканується, скільки лишилось). -->
                        <div class="java-analyzing">
                          <div class="ja-title"><i class="ti ti-loader-2 spin"></i> {t('settings.storage.javaAnalyzing')}</div>
                          {#if javaProgress && javaProgress.total > 0}
                            <div class="ja-bar"><div style="width:{Math.round(((javaProgress.done + 1) / javaProgress.total) * 100)}%"></div></div>
                            <div class="ja-sub">{t('settings.storage.javaScanItem', { major: javaProgress.major, n: javaProgress.done + 1, total: javaProgress.total })}</div>
                          {/if}
                        </div>
                      {:else if unusedJava && unusedJava.installed.length === 0}
                        <!-- Вбудованої Java ще немає зовсім — показуємо порожній
                             стан, а не «зайвої Java немає» (БАГ, знайдений рев'ю). -->
                        <div style="font-size:12px;color:var(--text-mute);display:flex;align-items:center;gap:6px;margin-bottom:10px"><i class="ti ti-coffee-off"></i> {t('settings.storage.javaEmpty')}</div>
                        <button class="btn btn-ghost btn-sm" onclick={openJavaModal}><i class="ti ti-search"></i> {t('settings.storage.javaView')}</button>
                      {:else if unusedJava && unusedJava.unused.length === 0}
                        <div style="font-size:12px;color:var(--green);display:flex;align-items:center;gap:6px;margin-bottom:10px"><i class="ti ti-circle-check"></i> {t('settings.storage.cleanJavaNone')}</div>
                        <button class="btn btn-ghost btn-sm" onclick={openJavaModal}><i class="ti ti-search"></i> {t('settings.storage.javaView')}</button>
                      {:else if unusedJava}
                        <div style="font-size:12px;margin-bottom:10px;display:flex;align-items:center;gap:8px">
                          <span style="color:var(--red);font-weight:700">{t('settings.storage.javaUnusedCount', { n: unusedJava.unused.length })}</span>
                          <span style="color:var(--text-mute)">· {t('settings.storage.javaCanFree', { size: fmtBytes(unusedJava.totalBytes) })}</span>
                        </div>
                        <button class="btn btn-ghost btn-sm" onclick={openJavaModal}><i class="ti ti-search"></i> {t('settings.storage.cleanJavaAnalyze')}</button>
                      {:else}
                        <button class="btn btn-ghost btn-sm" onclick={openJavaModal}><i class="ti ti-search"></i> {t('settings.storage.cleanJavaAnalyze')}</button>
                      {/if}
                    </div>
                  {:else if storageLoading}
                    <!-- Скелетон сканування тек: поки бекенд рахує розміри
                         (може бути довго на великих збірках) — одразу видно
                         процес, а не порожню картку, яка «потім заповнюється». -->
                    <div class="storage-loading">
                      <div class="sl-title"><i class="ti ti-loader-2 spin"></i> {t('settings.storage.scanning')}</div>
                      {#if storageScan}
                        <div class="sl-bar"><div style="width:{storageScan.total > 0 ? Math.round(((storageScan.done + 1) / storageScan.total) * 100) : 8}%"></div></div>
                        <div class="sl-sub">{storageScan.label} · {storageScan.done + 1}/{storageScan.total}</div>
                      {/if}
                    </div>
                  {:else}
                    <button class="btn btn-ghost btn-sm" onclick={() => loadStorageInfo(true)}><i class="ti ti-refresh"></i> {t('settings.storage.title')}</button>
                  {/if}
                </div>
              </div>

              <!-- ══ Модалка: Аналіз невикористовуваної Java ══
                   Показує ПРОЦЕС аналізу (анімація, поки бекенд сканує підтеки
                   java-<major>), а потім результати: кожен встановлений мажор
                   з розміром і статусом «використовується / ні», загальний
                   обсяг, який можна звільнити, і кнопка видалення. -->
              {#if javaModalOpen}
                <div class="modal-overlay" onclick={(e) => { if (e.target === e.currentTarget) javaModalOpen = false }}>
                  <div class="modal" style="max-width:480px">
                    <div class="modal-head">
                      <i class="ti ti-coffee" style="font-size:20px; color:var(--orange)"></i>
                      <h3>{t('settings.storage.javaModalTitle')}</h3>
                      <button class="icon-btn" onclick={() => (javaModalOpen = false)}><i class="ti ti-x"></i></button>
                    </div>
                    <div class="modal-body">
                      {#if javaBusy && !unusedJava}
                        <!-- Процес аналізу: спінер + ЖИВИЙ прогрес (який мажор
                             сканується, скільки лишилось) поки бекенд рахує. -->
                        <div class="java-modal-analyzing">
                          <div class="jma-ring"></div>
                          <div class="jma-title"><i class="ti ti-coffee"></i> {t('settings.storage.javaAnalyzing')}</div>
                          <div class="jma-sub">{t('settings.storage.javaAnalyzingHint')}</div>
                          {#if javaProgress && javaProgress.total > 0}
                            <div class="ja-bar" style="width:100%;margin-top:16px"><div style="width:{Math.round(((javaProgress.done + 1) / javaProgress.total) * 100)}%"></div></div>
                            <div class="ja-sub" style="text-align:center">{t('settings.storage.javaScanItem', { major: javaProgress.major, n: javaProgress.done + 1, total: javaProgress.total })}</div>
                          {/if}
                        </div>
                      {:else if unusedJava}
                        {#if unusedJava.installed.length === 0}
                          <div class="java-none"><i class="ti ti-coffee-off"></i> {t('settings.storage.javaEmpty')}</div>
                        {:else}
                          <div class="java-list">
                            {#each unusedJava.installed as j (j.major)}
                              <div class="java-row" class:unused={!j.used}>
                                <i class="ti ti-coffee ji"></i>
                                <span class="j-name">Java {j.major}</span>
                                <span class="j-size">{fmtBytes(j.bytes)}</span>
                                {#if j.used}
                                  <span class="j-badge used"><i class="ti ti-check"></i> {t('settings.storage.javaUsed')}</span>
                                {:else}
                                  <span class="j-badge unused"><i class="ti ti-alert-triangle"></i> {t('settings.storage.javaUnused')}</span>
                                {/if}
                              </div>
                            {/each}
                          </div>
                          {#if unusedJava.unused.length > 0}
                            <div class="java-total">
                              <span>{t('settings.storage.javaCanFreeTotal')}</span>
                              <b style="color:var(--red)">{fmtBytes(unusedJava.totalBytes)}</b>
                            </div>
                            <button class="btn btn-danger" style="width:100%; justify-content:center; margin-top:14px" disabled={javaBusy} onclick={cleanupUnusedJava}>
                              <i class="ti {javaBusy ? 'ti-loader-2 spin' : 'ti-trash'}"></i>
                              {t('settings.storage.cleanJavaModalBtn', { n: unusedJava.unused.length })}
                            </button>
                          {:else}
                            <div class="java-none"><i class="ti ti-circle-check" style="color:var(--green)"></i> {t('settings.storage.cleanJavaNone')}</div>
                          {/if}
                          <button class="btn btn-ghost btn-sm" style="margin-top:12px" onclick={analyzeUnusedJava}>
                            <i class="ti {javaBusy ? 'ti-loader-2 spin' : 'ti-refresh'}"></i> {t('settings.storage.javaReanalyze')}
                          </button>
                        {/if}
                      {:else}
                        <div class="java-none"><i class="ti ti-alert-circle"></i> {t('settings.storage.javaFailed')}</div>
                      {/if}
                    </div>
                  </div>
                </div>
              {/if}
            {:else if settingsTab === 'about'}
              <div class="card">
                <div class="card-head"><div class="ci violet"><i class="ti ti-info-circle"></i></div>
                  <div class="ctw"><div class="ct">{t('settings.about.title')}</div></div>
                </div>
                <div style="padding:18px 20px; display:flex; flex-direction:column; gap:14px">
                  <div class="row" style="border:none">
                    <div class="row-label"><div class="rl-name">{t('settings.about.version')}</div></div>
                    <span class="chip on" style="font-family:var(--font-mono)">{aboutVersion || '2.0.0'}</span>
                  </div>
                  <div style="display:flex; gap:10px; align-items:center; flex-wrap:wrap">
                    <button class="btn btn-primary btn-sm" onclick={checkUpdates}><i class="ti ti-refresh"></i> {t('settings.about.checkUpdates')}</button>
                    {#if updateResult}
                      {@const ur = updateResult}
                      {#if ur.available}
                        <span style="font-size:12px;color:var(--text)">{t('settings.about.available', { version: ur.version })}</span>
                        <button class="btn btn-ghost btn-sm" onclick={() => App.OpenExternal(ur.downloadUrl)}><i class="ti ti-download"></i> {t('settings.about.download')}</button>
                        {#if ur.changelogUrl}<a class="ab-link" href="javascript:void(0)" onclick={() => App.OpenExternal(ur.changelogUrl)}>{t('settings.about.changelog')}</a>{/if}
                      {:else}
                        <span style="font-size:12px;color:var(--green)"><i class="ti ti-circle-check"></i> {t('settings.about.upToDate')}</span>
                      {/if}
                    {/if}
                  </div>
                </div>
              </div>
            {:else if settingsTab === 'folders'}
              <div class="card">
                <div class="card-head"><div class="ci"><i class="ti ti-folder"></i></div>
                  <div class="ctw"><div class="ct">{t('settings.tab.folders')}</div></div>
                </div>
                {#if folderPaths}
                  {#each folderRows as f}
                    <div class="row">
                      <div class="row-label">
                        <div class="rl-name">{f.name} {#if f.custom}<span class="chip on" style="font-size:10px">{t('settings.folders.customBadge')}</span>{/if}</div>
                        <div class="rl-sub">{f.desc}</div>
                        <div class="fp-path" title={f.path}>{f.path}</div>
                      </div>
                      {#if !f.fixed}
                        <button class="btn btn-ghost" onclick={() => setFolder(f.kind)} title={t('settings.folders.browse')} aria-label={t('settings.folders.browse')}>
                          <i class="ti ti-folder"></i>
                        </button>
                        {#if f.reset}
                          <button class="btn btn-ghost" disabled={folderPaths.baseDir === folderPaths.defaultBaseDir} onclick={resetBaseDir} title={t('settings.folders.reset')} aria-label={t('settings.folders.reset')}>
                            <i class="ti ti-rotate"></i>
                          </button>
                        {/if}
                      {:else}
                        <span class="chip" style="flex-shrink:0">{t('settings.folders.fixed')}</span>
                      {/if}
                    </div>
                  {/each}
                {:else}
                  <div style="padding:18px 22px">
                    <button class="btn btn-ghost btn-sm" onclick={() => loadFolderPaths(true)}><i class="ti ti-refresh"></i> {t('settings.folders.title')}</button>
                  </div>
                {/if}
              </div>
            {:else}
              <div class="card">
                <div class="card-head"><div class="ci {settingsTab === 'appearance' ? 'violet' : settingsTab === 'performance' ? 'green' : ''}"><i class="ti {groupIcons[settingsTab]}"></i></div>
                  <div class="ctw"><div class="ct">{t(groupTitleKeys[settingsTab])}</div></div>
                </div>
                {#each settingsSchema.filter((d) =>
                  d.group === settingsTab &&
                  (d.key !== 'accentCustom' || settings.accent === 'custom') &&
                  // Поля окремих шрифтів info/warn/error показуємо ЛИШЕ в
                  // режимі 'perType' (коли шрифт консолі різний по типах).
                  !(['consoleInfoFont', 'consoleWarnFont', 'consoleErrorFont'].includes(d.key) && settings.consoleFontMode !== 'perType')
                ) as def}
                  <SettingRow def={def} value={(settings as any)[def.key]} onchange={(v) => updateSetting(def.key, v)} />
                {/each}
                {#if settingsTab === 'console'}
                  <!-- Візуальний переглядач: одразу видно, як консоль виглядатиме
                       з поточними кольорами/шрифтом/розміром тексту. -->
                  <div class="row" style="display:block;border-bottom:none">
                    <div class="row-label" style="margin-bottom:10px">
                      <div class="rl-name">{t('settings.consolePreview')}</div>
                      <div class="rl-sub">{t('settings.consolePreviewDesc')}</div>
                    </div>
                    <div class="console-preview"
                         style="font-size:{Math.max(9, Math.min(24, settings.consoleFontSize || 12))}px;font-family:{settings.consoleFontFamily === 'sans' ? 'var(--font-sans)' : 'var(--font-mono)'};--cp-h:{Math.max(9, Math.min(24, settings.consoleFontSize || 12)) + 10}px">
                      <!-- Кожен рядок бере СВІЙ шрифт (consoleFontFor): у режимі
                           'perType' INFO/WARN/ERROR рендеряться різними шрифтами
                           — так само, як у реальній консолі. -->
                      <div class="cp-line" style="color:{settings.consoleInfoColor || '#8fd3ff'};font-family:{consoleFontFor('info')}"><span class="cp-ts">12:00:01</span><span class="cp-lvl">INFO</span><span>Завантаження збірки…</span></div>
                      <div class="cp-line" style="color:{settings.consoleWarnColor || '#ffd166'};font-family:{consoleFontFor('warn')}"><span class="cp-ts">12:00:05</span><span class="cp-lvl">WARN</span><span>Can't keep up! Is the server overloaded?</span></div>
                      <div class="cp-line" style="color:{settings.consoleErrorColor || '#ff6b6b'};font-family:{consoleFontFor('error')}"><span class="cp-ts">12:00:09</span><span class="cp-lvl">ERROR</span><span>java.lang.OutOfMemoryError: Java heap space</span></div>
                    </div>
                  </div>
                {/if}
                {#if settingsTab === 'appearance'}
                  <div class="row" style="border-top:1px solid var(--border)">
                    <div class="row-label"><div class="rl-name">{t('sidebar.onlyPinned')}</div><div class="rl-sub">{t('sidebar.onlyPinnedDesc')}</div></div>
                    <div class="toggle" class:on={sidebarOnlyPinned} onclick={toggleSidebarOnlyPinned}></div>
                  </div>
                  <div class="row" style="border-bottom:none">
                    <div class="row-label"><div class="rl-name">{t('sidebar.scale')}</div><div class="rl-sub">{t('sidebar.scaleDesc')}</div></div>
                    <div class="seg-ctl">
                      {#each ['small', 'normal', 'large'] as s}
                        <button class="seg-btn" class:on={sidebarScale === s} onclick={() => setSidebarScale(s)}>{t('sidebar.scale.' + s)}</button>
                      {/each}
                    </div>
                  </div>
                {/if}
              </div>
            {/if}
          </div>
        </div>
      {/if}
    </div>
  </div>
</div>

<!-- ═══ PRE-LAUNCH MODS WARNING ═══
     Перед запуском збірки з модами перевіряємо дублікати (однаковий sha1,
     різні назви файлів) і відсутні залежності. Якщо є проблеми — модалка
     зі швидкими діями: видалити дублікати, відкрити «Моди» або все одно
     запустити. -->
{#if preLaunchIssues}
  {@const dupCount = preLaunchIssues.duplicates.reduce((n, g) => n + g.mods.length - 1, 0)}
  <div class="modal-overlay">
    <div class="modal" onclick={(e: any) => e.stopPropagation()}>
      <div class="modal-head">
        <h3><i class="ti ti-alert-triangle" style="color:var(--yellow);margin-right:8px"></i>{t('mods.preLaunch.title')}</h3>
        <button class="crash-modal-close" onclick={launchAnyway} title={t('win.close')}>
          <i class="ti ti-x"></i>
        </button>
      </div>
      <div class="modal-body">
        <div style="display:flex;flex-direction:column;gap:14px">
          {#if preLaunchIssues.duplicates.length > 0}
            <div>
              <div class="pl-warn-title"><i class="ti ti-copy"></i> {t('mods.preLaunch.duplicatesTitle', { n: dupCount })}</div>
              {#each preLaunchIssues.duplicates as g}
                <div class="pl-dup">
                  {#each g.mods as m, i}
                    <span class="pl-mod-chip">
                      {m.name}{#if !m.enabled}<i class="ti ti-player-pause"></i>{/if}
                    </span>
                    {#if i < g.mods.length - 1}<span class="pl-dup-eq">=</span>{/if}
                  {/each}
                </div>
              {/each}
            </div>
          {/if}
          {#if preLaunchIssues.missingDeps.length > 0}
            <div>
              <div class="pl-warn-title"><i class="ti ti-link"></i> {t('mods.preLaunch.depsTitle')}</div>
              <div class="pl-deps">
                {#each preLaunchIssues.missingDeps as d}
                  <span class="pl-mod-chip">{d}</span>
                {/each}
              </div>
            </div>
          {/if}
          <div style="font-size:12px;color:var(--text-mute);line-height:1.6">{t('mods.preLaunch.hint')}</div>
        </div>
      </div>
      <div class="modal-foot">
        <button class="btn btn-ghost" onclick={launchAnyway}><i class="ti ti-player-play"></i> {t('mods.preLaunch.anyway')}</button>
        {#if dupCount > 0}
          <button class="btn btn-danger" onclick={deleteDuplicatesAndLaunch}><i class="ti ti-trash"></i> {t('mods.preLaunch.deleteDups', { n: dupCount })}</button>
        {/if}
        <button class="btn btn-primary" onclick={openModsToFix}><i class="ti ti-puzzle"></i> {t('mods.preLaunch.openMods')}</button>
      </div>
    </div>
  </div>
{/if}

<!-- ═══ CRASH MODAL (замість старого банера на «Моїх збірках»): показується
     поверх будь-якої сторінки, коли гра завершилась аварійно. Кнопка
     «Відкрити консоль» переходить на сторінку збірки, яка впала, і відкриває
     її вкладку «Консоль». -->
{#if gameCrashed && !crashBannerHidden}
  <div class="modal-overlay">
    <div class="modal" onclick={(e: any) => e.stopPropagation()}>
      <div class="modal-head">
        <h3><i class="ti ti-alert-triangle" style="color:var(--red);margin-right:8px"></i>{t('crashBanner.title')}</h3>
        <button class="crash-modal-close" onclick={crashBannerDismiss} title={t('win.close')}>
          <i class="ti ti-x"></i>
        </button>
      </div>
      <div class="modal-body">
        <div style="display:flex;flex-direction:column;gap:14px">
          <div style="font-size:13px;color:var(--text-mute);line-height:1.6">{t('crashBanner.text')}</div>
          <label class="cb-dont" style="align-self:flex-start;display:flex;align-items:center;gap:8px;font-size:11.5px;color:var(--text-mute);cursor:pointer;user-select:none">
            <input type="checkbox" checked={crashBannerHidden} onchange={crashBannerDontShow} style="accent-color:var(--red)" />
            {t('crashBanner.dontShowAgain')}
          </label>
        </div>
      </div>
      <div class="modal-foot">
        <button class="btn btn-ghost" onclick={crashBannerDismiss}><i class="ti ti-x"></i> {t('cancel')}</button>
        <button class="btn btn-primary" onclick={crashBannerOpenConsole}>
          <i class="ti ti-terminal-2"></i> {t('crashBanner.openConsole')}
        </button>
      </div>
    </div>
  </div>
{/if}

<!-- ═══ LOGIN MODAL ═══ -->
{#if modalLogin && !showWizard}
  <div class="modal-overlay" class:gate={loginGate} onclick={() => { if (!loginGate) modalLogin = false }}>
    <div class="modal" onclick={(e: any) => e.stopPropagation()}>
      <div class="modal-head">
        {#if loginGate}
          <div class="gate-av"><img src={steveHead} alt=""/></div>
        {/if}
        <h3>{t('modal.title')}</h3>
      </div>
      <div class="modal-body">
        {#if loginGate}
          <div class="gate-hint">
            <i class="ti ti-user-plus"></i> {@html t('modal.gate.hint')}
          </div>
        {/if}
        <div style="display:flex;flex-direction:column;gap:10px">
          {#if loginMsStep === 'waiting'}
            <div class="ms-waiting">
              <div class="spinner"></div>
              <span>{t('modal.waiting')}</span>
              {#if !loginGate}
                <button class="btn btn-ghost btn-sm" onclick={loginMsCancel}>{t('cancel')}</button>
              {/if}
            </div>
          {:else}
            <button class="btn btn-primary btn-lg" style="justify-content:center" onclick={loginMsStart}>
              <i class="ti ti-brand-windows"></i> {t('loginMicrosoft')}
            </button>
          {/if}
          <div class="wiz-divider">{t('orPirate')}</div>
          <div class="wiz-field-row">
            <input class="input" style="flex:1" placeholder={t('nickname')} bind:value={pirateUsername}
                   onkeydown={(e:any) => e.key==='Enter' && loginPirate()}/>
            <button class="btn btn-ghost" onclick={loginPirate}><i class="ti ti-user"></i> {t('login')}</button>
          </div>
        </div>
      </div>
      {#if !loginGate}
        <div class="modal-foot">
          <button class="btn btn-ghost" onclick={() => modalLogin = false}>{t('cancel')}</button>
        </div>
      {/if}
    </div>
  </div>
{/if}

<!-- ═══ TOASTS + CONFIRM (замість alert/confirm) ═══ -->
  {#if ctxMenu && ctxPack}
    <div class="ctx-menu" style="left:{ctxMenu.x}px;top:{ctxMenu.y}px" oncontextmenu={(e) => e.preventDefault()}>
      <div class="ctx-item" onclick={() => ctxAction('open', ctxPack.build.id)}>
        <i class="ti ti-external-link"></i> {t('pack.openPage')}
      </div>
      <div class="ctx-sep"></div>
      <div class="ctx-item" class:pin-on={isPinned(ctxPack.build.id)} onclick={() => ctxAction('pin', ctxPack.build.id)}>
        <i class="ti ti-pin"></i> {isPinned(ctxPack.build.id) ? t('packs.unpin') : t('packs.pin')}
      </div>
      {#if ctxPack.status === 'running'}
        <div class="ctx-item" onclick={() => ctxAction('play', ctxPack.build.id)}>
          <i class="ti ti-player-stop"></i> {t('pack.stop')}
        </div>
      {:else if ctxPack.status === 'ready'}
        <div class="ctx-item" onclick={() => ctxAction('play', ctxPack.build.id)}>
          <i class="ti ti-player-play"></i> {t('pack.play')}
        </div>
      {/if}
      {#if ctxPack.status === 'not-installed' || ctxPack.status === 'needs-update'}
        <div class="ctx-item" onclick={() => ctxAction('download', ctxPack.build.id)}>
          <i class="ti ti-download"></i> {ctxPack.status === 'needs-update' ? t('pack.update') : t('pack.download')}
        </div>
      {/if}
      <div class="ctx-sep"></div>
      <div class="ctx-item" onclick={() => ctxAction('console', ctxPack.build.id)}>
        <i class="ti ti-terminal-2"></i> {t('pack.openConsole')}
      </div>
      {#if ctxPack.installed}
        <div class="ctx-item" onclick={() => ctxAction('folder', ctxPack.build.id)}>
          <i class="ti ti-folder"></i> {t('pack.openFolder')}
        </div>
        <div class="ctx-item" onclick={() => ctxAction('repair', ctxPack.build.id)}>
          <i class="ti ti-tools"></i> {t('pack.repair')}
        </div>
        <div class="ctx-sep"></div>
        <div class="ctx-item danger" onclick={() => ctxAction('delete', ctxPack.build.id)}>
          <i class="ti ti-trash"></i> {t('pack.delete')}
        </div>
      {/if}
    </div>
  {/if}

  <Toasts />
  <ConfirmModal />

<style>
  .launcher-window {
    width:100%; height:100vh; max-width:100%; max-height:100vh;
    background:var(--bg-window);
    overflow:hidden; display:flex; position:relative;
  }
  .launcher-window:not(.maximized) { border-radius:10px; }
  .launcher-window.maximized { border-radius:0; }

  .topbar {
    /* Вища панель (72px): кнопки вікна «дихають» — відступи між верхом і
       низом, а іконка РОЗДІЛУ + заголовок + опис сторінки живуть тут
       (заголовки/описи з контенту сторінок прибрані). */
    height:72px; min-height:72px;
    display:flex; align-items:center;
    padding:0 18px 0 20px; gap:12px;
    background:var(--bg-card);
    border-bottom:1px solid var(--border);
    user-select:none;
    -webkit-user-select:none;
    --wails-draggable: drag;
  }
  .tb-icon {
    width:40px; height:40px; border-radius:12px;
    background:linear-gradient(135deg,var(--orange),var(--orange-2));
    display:flex; align-items:center; justify-content:center;
    font-size:19px; color:#fff;
    flex-shrink:0;
    box-shadow:0 6px 16px rgba(255,138,0,.28);
  }
  .logo-icon-img {
    background:transparent !important;
    box-shadow:none !important;
    overflow:hidden;
    padding:0;
  }
  .logo-icon-img img { width:100%; height:100%; object-fit:contain; display:block; }
  .tb-titles { display:flex; flex-direction:column; gap:2px; min-width:0; }
  .topbar-title { font-size:16px; font-weight:700; white-space:nowrap; line-height:1.2; }
  .topbar-sub {
    font-size:11px; color:var(--text-mute); line-height:1.3;
    white-space:nowrap; overflow:hidden; text-overflow:ellipsis; max-width:440px;
  }
  .topbar-crumbs { font-size:11px; color:var(--text-mute); display:flex; align-items:center; gap:6px; white-space:nowrap; }
  .topbar-crumbs i { font-size:12px; }
  .tb-sep { width:1px; height:26px; background:var(--border); margin:0 4px; }

  .ctrl-box {
    display:flex; align-items:center;
    background:var(--hover);
    border:1px solid var(--border);
    border-radius:12px;
    padding:5px;
    margin-left:auto; margin-right:0;
    --wails-draggable: no-drag;
  }
  .ctrl-btn {
    width:34px; height:34px; border-radius:8px;
    border:none; background:transparent; cursor:pointer;
    color:var(--text-mute); font-size:15px;
    display:flex; align-items:center; justify-content:center;
    transition:background .15s, color .15s;
    --wails-draggable: no-drag;
  }
  .ctrl-btn:hover { background:var(--hover-strong); color:var(--text); }
  .ctrl-btn.close:hover { background:var(--red); color:#fff; }
  .no-drag { --wails-draggable: no-drag; }

  .wiz-wrap {
    position:fixed; inset:0; z-index:99999;
    background:
      radial-gradient(900px 500px at 12% -5%, rgba(var(--orange-rgb),.06), transparent 60%),
      radial-gradient(900px 600px at 100% 110%, rgba(123,77,255,.07), transparent 55%),
      var(--bg-app);
    display:flex; align-items:center; justify-content:center;
  }

  .gate-av {
    width:40px; height:40px; border-radius:10px; overflow:hidden;
    border:1px solid var(--border); flex-shrink:0;
  }
  .gate-av img { width:100%; height:100%; object-fit:cover; display:block; }
  .gate-hint {
    display:flex; align-items:center; gap:8px;
    font-size:12.5px; color:var(--text-mute); line-height:1.5;
    background:rgba(var(--orange-rgb),.07); border:1px solid rgba(var(--orange-rgb),.2);
    border-radius:10px; padding:10px 14px; margin-bottom:12px;
  }
  .gate-hint i { color:var(--orange); font-size:16px; flex-shrink:0; }

  .auto-save-hint {
    display:flex; align-items:center; gap:7px;
    font-size:11.5px; color:var(--text-mute);
    background:var(--bg-input); border:1px solid var(--border);
    padding:8px 12px; border-radius:10px;
  }
  .auto-save-hint i { color:var(--green); }

  .acc-row-av {
    width:34px; height:34px; border-radius:9px; overflow:hidden;
    border:1px solid var(--border-hi); flex-shrink:0; margin-right:2px;
  }
  .acc-row-av img { width:100%; height:100%; object-fit:cover; display:block; }

  .it-time {
    display:flex; align-items:center; gap:5px;
    font-size:10.5px; color:var(--text-dim); margin-top:4px; font-family:var(--font-mono);
  }
  .it-time i { font-size:11px; color:var(--orange); }

  .total-time-banner {
    display:flex; align-items:center; gap:8px;
    background:rgba(var(--orange-rgb),.08); border:1px solid rgba(var(--orange-rgb),.22);
    border-radius:12px; padding:10px 16px; margin-bottom:12px;
    font-size:12px; color:var(--text);
  }
  .total-time-banner i { color:var(--orange); font-size:15px; }
  .total-time-banner b { font-family:var(--font-mono); margin-left:auto; }

  .crash-modal-close {
    width:26px; height:26px; border:none; background:transparent;
    color:var(--text-dim); cursor:pointer; border-radius:7px;
    display:flex; align-items:center; justify-content:center; flex-shrink:0;
    transition:background .15s, color .15s;
  }
  .crash-modal-close:hover { background:rgba(239,68,68,.15); color:#ef4444; }

  .storage-bar { display:flex; height:14px; border-radius:8px; overflow:hidden; gap:2px; background:var(--bg-input); }
  .storage-bar .sb-seg { height:100%; background:linear-gradient(180deg, var(--orange), var(--orange-2)); border-radius:3px; min-width:4px; }
  /* Прев'ю консолі на вкладці «Консоль» налаштувань: живий рядок за рядком,
     як у реальному логу (час/рівень/текст), висота рядка = --cp-h. */
  .console-preview {
    background: color-mix(in srgb, var(--bg-panel) 55%, transparent);
    border: 1px solid var(--border); border-radius: var(--r-md);
    padding: 10px 14px; overflow: hidden;
    font-family: var(--font-mono); line-height: var(--cp-h, 22px);
    user-select: none;
  }
  .console-preview .cp-line { display: flex; gap: 10px; align-items: baseline; white-space: nowrap; overflow: hidden; }
  .console-preview .cp-ts { color: var(--text-dim); flex-shrink: 0; font-size: .85em; }
  .console-preview .cp-lvl { flex-shrink: 0; font-weight: 700; width: 40px; font-size: .85em; opacity: .85; }
  .console-preview .cp-line span:last-child { overflow: hidden; text-overflow: ellipsis; }
  .storage-bar .sb-seg:nth-child(6n+2) { background:linear-gradient(180deg,#7b4dff,#5a2eff); }
  .storage-bar .sb-seg:nth-child(6n+3) { background:linear-gradient(180deg,#10b981,#059669); }
  .storage-bar .sb-seg:nth-child(6n+4) { background:linear-gradient(180deg,#3b82f6,#2563eb); }
  .storage-bar .sb-seg:nth-child(6n+5) { background:linear-gradient(180deg,#ef4444,#dc2626); }
  .storage-bar .sb-seg:nth-child(6n+6) { background:linear-gradient(180deg,#eab308,#ca8a04); }
  .storage-legend { display:grid; grid-template-columns:1fr 1fr; gap:6px 18px; margin-top:16px; }
  .sl-item { display:flex; align-items:center; gap:8px; font-size:11.5px; color:var(--text-mute); }
  .sl-dot { width:9px; height:9px; border-radius:3px; background:var(--orange); flex-shrink:0; }
  .sl-item:nth-child(6n+2) .sl-dot { background:#7b4dff; }
  .sl-item:nth-child(6n+3) .sl-dot { background:#10b981; }
  .sl-item:nth-child(6n+4) .sl-dot { background:#3b82f6; }
  .sl-item:nth-child(6n+5) .sl-dot { background:#ef4444; }
  .sl-item:nth-child(6n+6) .sl-dot { background:#eab308; }
  .sl-name { flex:1; }
  .sl-size { font-family:var(--font-mono); color:var(--text); }

  /* Список встановлених збірок у «Пам'ять і кеш»: рядок = назва + розмір
     + кнопка швидкого видалення (збірка лишається в каталозі Shaurma). */
  .storage-packs { display:flex; flex-direction:column; gap:6px; max-height:280px; overflow-y:auto; }
  .storage-packs .sp-row {
    display:flex; align-items:center; gap:10px; padding:8px 10px;
    background:var(--bg-input); border:1px solid var(--border); border-radius:var(--r-sm);
    transition:border-color .15s;
  }
  .storage-packs .sp-row:hover { border-color:var(--border-hi); }
  .sp-row .sp-name { flex:1; min-width:0; font-size:12px; font-weight:600; white-space:nowrap; overflow:hidden; text-overflow:ellipsis; }
  .sp-row .sp-size { font-family:var(--font-mono); font-size:11.5px; color:var(--text-mute); flex-shrink:0; }
  .sp-row .icon-btn { width:26px; height:26px; flex-shrink:0; }
  .sp-row .icon-btn.danger:hover { color:var(--red); border-color:rgba(239,68,68,.4); background:rgba(239,68,68,.1); }
  /* Іконка збірки в рядку «Пам'ять і кеш» — як картка sidebar: кольоровий
     квадрат на --pcol (колір збірки) + PNG-іконка поверх (якщо є). */
  .storage-packs .sp-row .sp-ic {
    width:30px; height:30px; min-width:30px; border-radius:8px;
    background:color-mix(in srgb, var(--pcol, var(--orange)) 16%, var(--bg-input-hi));
    display:flex; align-items:center; justify-content:center;
    font-size:14px; color:var(--pcol, var(--orange)); overflow:hidden;
  }
  .storage-packs .sp-row .sp-ic img { width:100%; height:100%; object-fit:cover; }
  .storage-packs .sp-row .sp-ic.has-icon { background:transparent; }
  /* Базовий .icon-btn (використовується в модалці Java і рядках збірок). */
  .icon-btn {
    width:30px; height:30px; border-radius:9px; border:none;
    background:var(--hover); color:var(--text-mute); cursor:pointer;
    display:flex; align-items:center; justify-content:center;
    transition:background .15s, color .15s; flex-shrink:0; font-size:15px;
  }
  .icon-btn:hover { background:var(--hover-strong); color:var(--text); }
  .icon-btn.danger:hover { background:rgba(239,68,68,.15); color:var(--red); }

  /* ── Аналіз Java: інлайн-анімація на вкладці ── */
  .java-analyzing { display:flex; flex-direction:column; gap:8px; padding:8px 0; }
  .java-analyzing .ja-title { display:flex; align-items:center; gap:8px; font-size:12px; font-weight:600; color:var(--text); }
  .java-analyzing .ja-title i { color:var(--orange); font-size:15px; }
  /* Прогрес-бар аналізу Java (інлайн + модалка): живий прогрес від бекенду
     (storage:java-scan) — який мажор сканується, скільки лишилось. */
  .ja-bar { height:8px; border-radius:6px; background:var(--bg-input); overflow:hidden; }
  .ja-bar div { height:100%; border-radius:6px; background:linear-gradient(90deg, var(--orange), var(--orange-2)); transition:width .2s ease; }
  .ja-sub { font-size:11px; color:var(--text-mute); font-family:var(--font-mono); }

  /* ── Скелетон сканування «Пам'ять і кеш» ──
     Поки бекенд рахує розміри великих тек, розділ показує прогрес-бар
     (storage:usage-scan) замість порожньої картки, яка «потім заповнюється». */
  .storage-loading { display:flex; flex-direction:column; gap:8px; padding:6px 2px; }
  .storage-loading .sl-title { display:flex; align-items:center; gap:8px; font-size:12px; font-weight:600; color:var(--text); }
  .storage-loading .sl-title i { color:var(--orange); font-size:15px; }
  .storage-loading .sl-bar { height:8px; border-radius:6px; background:var(--bg-input); overflow:hidden; }
  .storage-loading .sl-bar div { height:100%; border-radius:6px; background:linear-gradient(90deg, var(--orange), var(--orange-2)); transition:width .2s ease; }
  .storage-loading .sl-sub { font-size:11px; color:var(--text-mute); font-family:var(--font-mono); }

  /* ── Модалка аналізу Java ── */
  .java-modal-analyzing { display:flex; flex-direction:column; align-items:center; gap:10px; padding:26px 10px; text-align:center; }
  .jma-ring {
    width:44px; height:44px; border-radius:50%;
    border:3px solid rgba(var(--orange-rgb),.2); border-top-color:var(--orange);
    animation:sb-spin 1s linear infinite;
  }
  .jma-title { font-size:13px; font-weight:700; display:flex; align-items:center; gap:7px; }
  .jma-title i { color:var(--orange); }
  .jma-sub { font-size:11px; color:var(--text-mute); max-width:300px; line-height:1.5; }
  .java-list { display:flex; flex-direction:column; gap:6px; }
  .java-row {
    display:flex; align-items:center; gap:10px; padding:9px 11px;
    background:var(--bg-input); border:1px solid var(--border); border-radius:var(--r-sm);
  }
  .java-row .ji { font-size:15px; color:var(--orange); flex-shrink:0; }
  .java-row .j-name { flex:1; min-width:0; font-size:12.5px; font-weight:700; }
  .java-row .j-size { font-family:var(--font-mono); font-size:11.5px; color:var(--text-mute); flex-shrink:0; }
  .java-row .j-badge {
    display:inline-flex; align-items:center; gap:4px; font-size:10px; font-weight:700;
    padding:3px 8px; border-radius:999px; flex-shrink:0;
  }
  .java-row .j-badge.used { background:rgba(16,185,129,.12); color:var(--green); border:1px solid rgba(16,185,129,.3); }
  .java-row .j-badge.unused { background:rgba(239,68,68,.12); color:var(--red); border:1px solid rgba(239,68,68,.35); }
  .java-row.unused { border-color:rgba(239,68,68,.25); }
  .java-total {
    display:flex; justify-content:space-between; align-items:center;
    margin-top:14px; padding:10px 12px;
    background:rgba(239,68,68,.06); border:1px solid rgba(239,68,68,.25);
    border-radius:var(--r-sm); font-size:12px; color:var(--text-mute);
  }
  .java-total b { font-family:var(--font-mono); }
  .java-none {
    display:flex; align-items:center; justify-content:center; gap:8px;
    padding:22px 10px; font-size:12.5px; color:var(--text-mute);
  }
  .java-none i { font-size:16px; color:var(--text-dim); }
  .storage-total {
    display:flex; justify-content:space-between; align-items:center;
    margin-top:16px; padding-top:12px; border-top:1px solid var(--border);
    font-size:12px; color:var(--text-mute);
  }  .storage-total b { color: var(--text); font-family: var(--font-mono); }

  /* ── Pre-launch модалка проблем з модами ── */
  .pl-warn-title {
    display: flex; align-items: center; gap: 7px;
    font-size: 12.5px; font-weight: 700; color: var(--yellow); margin-bottom: 8px;
  }
  .pl-warn-title i { font-size: 14px; }
  .pl-dup, .pl-deps { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; margin-bottom: 8px; }
  .pl-mod-chip {
    display: inline-flex; align-items: center; gap: 5px;
    font-size: 11.5px; font-weight: 600; color: var(--text);
    background: var(--bg-input); border: 1px solid var(--border);
    padding: 4px 9px; border-radius: 999px;
  }
  .pl-mod-chip i { font-size: 11px; color: var(--text-mute); }
  .pl-dup-eq { color: var(--text-dim); font-size: 12px; font-weight: 700; }

  .ab-link {
    font-size:11.5px; color:var(--orange); cursor:pointer; text-decoration:underline; text-underline-offset:2px;
  }

  .fp-path {
    margin-top:6px;
    font-family: var(--font-mono); font-size:10.5px;
    color: var(--text-dim);
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
    max-width: 480px;
  }
</style>
