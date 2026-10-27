<script lang="ts">
  // Sidebar.svelte — ліва навігаційна колонка лаунчера (лого, розділи,
  // список збірок з перемикачем Shaurma/Кастомні, папки, футер акаунтів).
  // Вся логіка/стан лишаються в App.svelte — сюди приходять пропсами:
  // значення для рендеру + колбеки-дії (кліки, драг-н-дроп, контекстні меню).
  import { onMount } from 'svelte'
  import { fly, scale } from 'svelte/transition'
  import { flip } from 'svelte/animate'
  import { cubicOut } from 'svelte/easing'
  import { t } from './i18n.svelte'
  import { accountPrefs } from './accountPrefs.svelte'
  import { launchStageLabel, launchStageColor, launchStagePct, downloadStageLabel, downloadStageColor } from './launchStages'
  import { packAsset } from './packAssets.svelte'
  import AnimatedIcon from './AnimatedIcon.svelte'
  import FriendAvatar from './friends/FriendAvatar.svelte'
  import { serverIcon } from './servers/utils'
  import ProfileModal from './ProfileModal.svelte'
  import { safeFolderIcon } from './folderMeta'
  import { App, on } from './wails'
  import { toast } from './toast.svelte'
  import type { Account, BuildView, ServerView, LaunchProgress, PackFolder, SocialConn, SocialFriend, SocialPresenceEvent, SocialState } from './types'
  import appIcon from '../assets/images/appicon.png'

  // ── Локальний стан: конфеті-пуфи при кліку на лого (сайдбар-специфічне,
  // ніде більше не використовується) ──────────────────────────────────────
  let logoPuffs = $state<{ id: string; dx: number; dy: number; hue: number }[]>([])
  let logoBoxEl = $state<HTMLDivElement | undefined>()

  // ── Пасхалка «Бешанга»: 10 ШВИДКИХ кліків по логотипу → 3 секунди
  // шаленства всього вікна (обертання + веселкове блимання + пульс).
  // Лічильник скидається, якщо між кліками була пауза > 2.5 с — щоб
  // випадкові кліки протягом дня не накопичувались у випадковий запуск.
  const BESHANGA_CLICKS = 10
  const BESHANGA_MS = 3000
  let logoClickCount = 0
  let logoClickResetTimer: ReturnType<typeof setTimeout> | null = null
  let beshangaTimer: ReturnType<typeof setTimeout> | null = null
  let beshangaOn = $state(false)

  function triggerBeshanga() {
    document.documentElement.classList.add('beshanga')
    beshangaOn = true
    if (beshangaTimer) clearTimeout(beshangaTimer)
    beshangaTimer = setTimeout(() => {
      document.documentElement.classList.remove('beshanga')
      beshangaOn = false
    }, BESHANGA_MS)
  }

  function logoParty() {
    const el = logoBoxEl
    if (el) {
      el.classList.remove('sb-logo-pop')
      // Reflow: «обриваємо» анімацію, щоб повторний клік запустив її знову.
      void el.offsetWidth
      el.classList.add('sb-logo-pop')
    }
    const n = 6
    const base = Date.now()
    logoPuffs = Array.from({ length: n }, (_, i) => ({
      id: base + '-' + i,
      dx: Math.round(Math.cos((i * Math.PI * 2) / n - Math.PI / 2) * 56),
      dy: Math.round(Math.sin((i * Math.PI * 2) / n - Math.PI / 2) * 56),
      hue: Math.round((i / n) * 360),
    }))

    // Лічильник для пасхалки: серія швидких кліків по логотипу.
    logoClickCount++
    if (logoClickResetTimer) clearTimeout(logoClickResetTimer)
    logoClickResetTimer = setTimeout(() => { logoClickCount = 0 }, 2500)
    if (logoClickCount >= BESHANGA_CLICKS) {
      logoClickCount = 0
      triggerBeshanga()
    }
  }

  // ── Пропси: стан + дії. Логіка/стан лишаються в App.svelte; сюди
  // приходять значення для рендеру і колбеки-дії. Bindable — там, де
  // sidebar сам записує значення (перемикач розділу, розкриття папки,
  // дропдаун акаунтів).
  let {
    instanceTab = $bindable('shaurma'),
    // Розгорнуті папки: КІЛЬКА одночасно (масив ID). Стан зберігається в
    // App.svelte (localStorage) і відновлюється після перезапуску.
    openFolderIds = $bindable<string[]>([]),
    accDropdownOpen = $bindable(false),
    sidebarCollapsed,
    sidebarWidth,
    sidebarScale,
    sbResizing,
    currentPage,
    deactivatedNavPage,
    deactivatedPackId,
    wardrobeVisible,
    isShaurmaEdition,
    builds,
    // ID авто-збірок (auto-vanilla / auto-snapshot) — щоб показувати бейдж
    // «авто» біля їхніх назв і не плутати зі звичайними збірками.
    autoPackIds = new Set<string>(),
    tabPinned,
    tabUnpinned,
    sidebarOnlyPinned,
    folderViews,
    layoutItems,
    launchProgress,
    migrateByPack,
    dragSrcId,
    dropTargetId,
    dropPos,
    dragPackColor,
    dragFolderId,
    dragFolderTarget,
    openPackId,
    suppressPackClick,
    accounts,
    activeAccountId,
    shaurmaFriendsVisible = false,
    friendsBadge = 0,
    // Стан мережі (network:status з бекенду): коли офлайн — показуємо
    // позначку «офлайн» під назвою лаунчера (проп для кожного рендера,
    // App.svelte оновлює його з події network:status).
    networkOnline = true,
    pageIcons,
    pageTitles,
    navigate,
    openCreatePack,
    openImportPack,
    openCreateFolderModal,
    openPacksFolder,
    toggleSidebar,
    startSidebarResize,
    toggleAccDropdown,
    switchAccount,
    removeAccount,
    openRenameAccount,
    openCtxMenu,
    openFolderCtxMenu,
    packPointerDown,
    packPointerMove,
    folderPointerDown,
    folderPointerMove,
    pointerDragEnd,
    cancelGameInstall,
    packBtnAction,
    openPackPage,
    packIconSrc,
    shouldPauseIcon,
    loaderImgFor,
    glowClass,
    syncStageIcon,
    launchStageIcon,
    spBtnClass,
    spBtnTitle,
    packSubText,
    otherRunningAccounts,
    isPinned,
    headFor,
    activeAccount,
    // «Додати акаунт» — відкриває логін-модалку (стан у App).
    onaddaccount,
  }: {
    instanceTab?: string
    openFolderIds?: string[]
    accDropdownOpen?: boolean
    sidebarCollapsed: boolean
    sidebarWidth: number
    sidebarScale: number
    sbResizing: boolean
    currentPage: string
    deactivatedNavPage: string | null
    deactivatedPackId: string | null
    wardrobeVisible: boolean
    isShaurmaEdition: boolean
    builds: BuildView[]
    autoPackIds?: Set<string>
    tabPinned: BuildView[]
    tabUnpinned: BuildView[]
    sidebarOnlyPinned: boolean
    folderViews: { folder: PackFolder; packs: BuildView[] }[]
    // ЄДИНИЙ впорядкований список кастомного таба: збірки І папки в одному
    // порядку (папку можна поставити між двома збірками). Папки рендеряться
    // акордеоном у цьому списку (а не окремим блоком унизу), збірки —
    // рядками. Закріплені збірки вже виведені ОКРЕМО (tabPinned).
    layoutItems: { kind: 'pack' | 'folder'; id: string }[]
    launchProgress: Record<string, LaunchProgress>
    migrateByPack: Record<string, { done: number; total: number; currentFile: string; name: string; iconUrl: string; status: string }>
    dragSrcId: string | null
    dropTargetId: string | null
    dropPos: 'before' | 'after' | null
    dragPackColor: string
    dragFolderId: string | null
    dragFolderTarget: string | null
    openPackId: string | null
    suppressPackClick: boolean
    accounts: Account[]
    activeAccountId: string
    // Чи показувати вхід у соціальну систему (Shaurma-редакція) І лічильник
    // вхідних запитів для крапки на кнопці. Навмисно лише «редакція»: кнопку
    // показуємо навіть до входу в скін-акаунт, щоб гравець бачив, що фіча є —
    // сама шухляда покаже екран «Увійдіть у Shaurma-акаунт».
    shaurmaFriendsVisible?: boolean
    friendsBadge?: number
    networkOnline?: boolean
    pageIcons: Record<string, string>
    pageTitles: Record<string, string>
    navigate: (page: string, resetBrowser?: boolean) => void
    openCreatePack: () => void
    openImportPack: () => void
    openCreateFolderModal: () => void
    openPacksFolder: () => void
    toggleSidebar: () => void
    startSidebarResize: (e: MouseEvent) => void
    toggleAccDropdown: () => void
    switchAccount: (id: string) => void
    removeAccount: (id: string) => void
    openRenameAccount: (acc: Account) => void
    openCtxMenu: (e: MouseEvent, id: string) => void
    openFolderCtxMenu: (e: MouseEvent, id: string) => void
    packPointerDown: (e: PointerEvent, id: string, color: string, fromFolder?: boolean) => void
    packPointerMove: (e: PointerEvent) => void
    folderPointerDown: (e: PointerEvent, id: string) => void
    folderPointerMove: (e: PointerEvent) => void
    pointerDragEnd: (e?: MouseEvent) => void
    cancelGameInstall: (instanceId: string) => void
    packBtnAction: (bv: BuildView) => void
    openPackPage: (id: string) => void
    packIconSrc: (icon: string | undefined) => string
    shouldPauseIcon: (icon: string | undefined, scope: 'sidebar' | 'grid' | 'folder', isSelected: boolean) => boolean
    loaderImgFor: (loaderType?: string) => string | undefined
    glowClass: (bv: BuildView, lp?: LaunchProgress | null) => string
    syncStageIcon: (bv: BuildView) => { icon: string; cls: string } | null
    launchStageIcon: (stage?: string) => { icon: string; cls: string } | null
    spBtnClass: (bv: BuildView) => string
    spBtnTitle: (bv: BuildView) => string
    packSubText: (bv: BuildView, lp?: LaunchProgress | null) => string
    otherRunningAccounts: (bv: BuildView | null | undefined) => Account[]
    isPinned: (id: string) => boolean
    headFor: (uuid?: string) => string
    activeAccount: () => Account | undefined
    onaddaccount: () => void
  } = $props()

  // Розділ «Друзі» показуємо, коли доступна хоч одна з соціальних систем:
  // друзі Shaurma (редакція) або друзі Minecraft (ліцензійний акаунт).
  const friendsNavVisible = $derived(shaurmaFriendsVisible)

  // ── Соціальний стан для sidebar ──────────────────────────────────────
  // • Голови друзів, що ЗАРАЗ у мережі, живуть НА САМОМУ пункті навігації
  //   «Друзі» (а не окремим блоком під списком) — у згорнутому sidebar вони
  //   зникають разом з підписом пункту (правило .sidebar.collapsed .nav-item span).
  // • Мій статус («В мережі» / «Не турбувати») і стан мережі — у ВІДЖЕТІ
  //   АКАУНТА (футер), поруч з даними акаунта: саме там гравець шукає «себе».
  const HEADS_MAX = 4
  let social = $state<SocialState | null>(null)
  let socialLoaded = false
  let dnd = $state(false)
  // Ручний режим присутності для друзів: '' | 'away' | 'invisible' (settings.presenceMode).
  let presenceMode = $state('')
  let statusMenu = $state(false)
  let statusBusy = $state(false)
  let stRowEl: HTMLElement | undefined = $state()
  let stPos = $state({ left: 0, bottom: 0 })

  // ДЖЕРЕЛО ПРАВДИ ДЛЯ СТАТУСУ — реальний стан, а не припущення:
  //  • не ввійшли / немає з'єднання → «не в мережі» (або «підключення…»);
  //  • у грі → «у грі»; вікно неактивне довго → «відійшов» (рахує бекенд,
  //    приходить у meStatus від friends:update);
  //  • «Не турбувати» — локальний режим (глушить тости й звук), він і
  //    показується замість «в мережі».
  type MyStatus = 'offline' | 'connecting' | 'in_game' | 'away' | 'dnd' | 'invisible' | 'online'
  const rawStatus = $derived.by<MyStatus>(() => {
    if (!social?.loggedIn) return 'offline'
    if (social.conn === 'connecting') return 'connecting'
    if (social.conn !== 'online') return 'offline'
    if (dnd) return 'dnd'
    if (presenceMode === 'invisible' || (social.meStatus as string) === 'invisible') return 'invisible'
    if (social.meStatus === 'in_game') return 'in_game'
    if (presenceMode === 'away' || social.meStatus === 'away') return 'away'
    return 'online'
  })
  // Антимерехтіння: перемикання між «в мережі / у грі / відійшов» застосовуємо лише
  // коли стан простояв ~1.2с (під час запуску збірки події летять пачкою). Переходи
  // з/в offline, connecting, dnd, invisible — миттєві.
  const SOFT: MyStatus[] = ['online', 'in_game', 'away']
  let myStatus = $state<MyStatus>('offline')
  let profileOpen = $state(false)
  let settleTimer: ReturnType<typeof setTimeout> | undefined
  $effect(() => {
    const next = rawStatus
    clearTimeout(settleTimer)
    if (next === myStatus) return
    if (SOFT.includes(next) && SOFT.includes(myStatus)) {
      settleTimer = setTimeout(() => { myStatus = next }, 1200)
    } else {
      myStatus = next
    }
    return () => clearTimeout(settleTimer)
  })
  const myStatusText = $derived(
    myStatus === 'offline' ? t('social.me.offline')
    : myStatus === 'connecting' ? t('social.me.connecting')
    : myStatus === 'dnd' ? t('social.me.dnd')
    : myStatus === 'invisible' ? t('social.me.invisible')
    : myStatus === 'in_game' ? t('social.me.inGame')
    : myStatus === 'away' ? t('social.me.away')
    : t('social.me.online'))
  // Статус можна вибирати лише коли є зʼєднання: «в мережі» без нього — обман.
  const canPickStatus = $derived(myStatus !== 'offline' && myStatus !== 'connecting')
  // Колір кружка на аватарі: мережа → стан. Без інтернету — оранжевий «off».
  const avDot = $derived(!networkOnline ? 'off' : myStatus)
  // Підпис типу акаунта (та сама логіка, що й у таблетці) — для title.
  const accTypeLabel = $derived.by(() => {
    const a = activeAccount()
    if (!a) return t('offline')
    if (a.isLicensed) return t('account.type.' + a.type)
    if (a.isShaurmaSkinAccount) return t('shaurmaAccount')
    return t('account.type.' + a.type)
  })

  const onlineFriends = $derived(
    (social?.loggedIn && Array.isArray(social.friends) ? (social.friends as SocialFriend[]) : [])
      .filter((f) => !!f.presence?.status && f.presence!.status !== 'offline')
      .sort((a, b) => (a.presence!.status === 'in_game' ? 0 : 1) - (b.presence!.status === 'in_game' ? 0 : 1)),
  )
  const heads = $derived(onlineFriends.slice(0, HEADS_MAX))
  const headsExtra = $derived(Math.max(0, onlineFriends.length - HEADS_MAX))

  // ── Запущені сервери на пункті «Сервери» ─────────────────────────────
  // Те саме, що голови друзів на «Друзі»: іконки серверів, які ЗАРАЗ
  // працюють (status === 'running'). Іконка — через спільний serverIcon():
  // PNG лаунчера → пресет (Tabler) → протокольна → іконка ядра, колір —
  // колір сервера. Список живе тут (а не в App.svelte), бо потрібен лише
  // сайдбару: перечитується на подіях server:status (debounce) і після
  // виходу зі сторінки серверів (там іконку/колір могли змінити).
  let runningServers = $state<ServerView[]>([])
  let srvReloadTimer: ReturnType<typeof setTimeout> | null = null
  async function loadRunningServers() {
    try {
      const list = ((await App.ListServers()) ?? []) as ServerView[]
      runningServers = list.filter((sv) => sv.status === 'running')
    } catch { /* лишаємо попередній стан */ }
  }
  function scheduleRunningServersReload() {
    if (srvReloadTimer) clearTimeout(srvReloadTimer)
    srvReloadTimer = setTimeout(() => { srvReloadTimer = null; loadRunningServers() }, 300)
  }
  const srvHeads = $derived(runningServers.slice(0, HEADS_MAX))
  const srvTitle = $derived(`${t('servers.status.running')}: ${runningServers.map((sv) => sv.name).join(', ')}`)
  const srvExtra = $derived(Math.max(0, runningServers.length - HEADS_MAX))
  let prevNavPage = currentPage
  $effect(() => {
    const p = currentPage
    if (p !== prevNavPage) {
      const was = prevNavPage
      prevNavPage = p
      if (was === 'servers') scheduleRunningServersReload()
    }
  })

  async function loadDnd() {
    try {
      const st = await App.GetSettings()
      dnd = !!st?.notifyDnd
      presenceMode = (st as any)?.presenceMode ?? ''
    } catch { /* лишаємо як є */ }
  }
  // Вибір статусу (як у Discord): «В мережі» / «Відійшов» / «Не турбувати» / «Невидимий».
  // Режими взаємовиключні; «Не турбувати» глушить тости й звук, «Відійшов» і «Невидимий»
  // міняють те, що бачать друзі.
  async function pickStatus(mode: 'online' | 'away' | 'dnd' | 'invisible') {
    if (statusBusy) return
    statusBusy = true
    try {
      const saved = await (App as any).SetPresenceMode(mode)
      dnd = saved === 'dnd'
      presenceMode = saved === 'away' || saved === 'invisible' ? saved : ''
      statusMenu = false
    } catch (e: any) { toast(String(e?.message ?? e), 'error') }
    finally { statusBusy = false }
  }
  function openStatusMenu() {
    if (!canPickStatus) return
    if (stRowEl) {
      const r = stRowEl.getBoundingClientRect()
      stPos = { left: r.right + 6, bottom: Math.max(8, window.innerHeight - r.bottom - 2) }
    }
    statusMenu = true
  }
  $effect(() => { if (!accDropdownOpen) statusMenu = false })

  onMount(() => {
    loadDnd()
    loadRunningServers()
    const offs = [
      on('server:status', () => scheduleRunningServersReload()),
      on('friends:update', (s: SocialState) => { social = s ?? null }),
      // Зʼєднання з сервером друзів міняється ОКРЕМИМ кадром (friends:conn),
      // без повного знімка — без цього чип статусу лишався б «В мережі»
      // навіть коли звʼязок уже впав.
      on('friends:conn', (c: SocialConn) => { if (social) social = { ...social, conn: c } }),
      on('friends:presence', (e: SocialPresenceEvent) => {
        if (!e?.accountId || !social) return
        social = { ...social, friends: (social.friends as SocialFriend[]).map((f) => (f.accountId === e.accountId ? { ...f, presence: e.presence } : f)) }
      }),
      on('settings:saved', () => { loadDnd() }),
    ]
    const close = (ev: MouseEvent) => {
      if (statusMenu && !(ev.target as HTMLElement)?.closest?.('.acc-st-row, .acc-st-sub')) statusMenu = false
    }
    window.addEventListener('click', close)
    return () => {
      offs.forEach((o) => o()); window.removeEventListener('click', close)
      if (srvReloadTimer) clearTimeout(srvReloadTimer)
    }
  })

  // Початковий стан соціальної системи: ВАЖЛИВО ЗАВАНТАЖУВАТИ ЗАВЖДИ (один
  // виклик на старт), бо чип статусу у віджеті акаунта показує conn/meStatus і
  // працює навіть там, де розділу «Друзі» у цій редакції немає.
  $effect(() => {
    if (socialLoaded) return
    socialLoaded = true
    App.GetFriendsState().then((s) => { social = s ?? null }).catch(() => {})
  })

  // ── Клік по пустому місцю sidebar (порожня зона списку збірок) ──
  // Показує невелике меню зі швидкими діями: створити збірку, імпортувати,
  // відкрити теку зі збірками. Закривається по кліку будь-де (він самих
  // дій закриває меню через stopPropagation-логіку в пунктах).
  let emptyMenu = $state<{ x: number; y: number } | null>(null)

  function onPacksEmptyClick(e: MouseEvent) {
    // Реагуємо ЛИШЕ на ПКМ (contextmenu) по самому контейнеру (порожня
    // зона), а не по картці збірки/папці — ті мають власні обробники і не
    // повинні відкривати меню. ЛКМ по пустому місцю нічого не робить.
    if (e.target !== e.currentTarget) return
    if (sidebarCollapsed) return
    e.preventDefault()
    // Координати у viewport: меню рендериться з position:fixed (прямо над
    // прокручуваним контейнером), тож воно не «втече» при скролі списку.
    emptyMenu = { x: e.clientX, y: e.clientY }
  }

  function closeEmptyMenu() { emptyMenu = null }

  // Розгорнути/згорнути папку в sidebar. Кілька папок можуть бути
  // розгорнуті одночасно (не акордеон): просто додаємо/прибираємо ID з
  // масиву. Стан зберігається у App.svelte через localStorage.
  function toggleFolder(id: string) {
    openFolderIds = openFolderIds.includes(id)
      ? openFolderIds.filter((x) => x !== id)
      : [...openFolderIds, id]
  }

  // Карти для layoutItems: збірка за id / папка за id.
  const buildById = $derived(new Map(builds.map(b => [b.build.id, b])))
  const folderById = $derived(new Map(folderViews.map(f => [f.folder.id, f])))
</script>

<aside class="sidebar" class:collapsed={sidebarCollapsed}
       class:narrow={sidebarWidth > 0 && sidebarWidth < 260}
       style="--sb-w:{sidebarWidth}px;--sb-scale:{sidebarScale}">
  <button class="sidebar-collapse-btn" onclick={toggleSidebar} title={t('sidebar.toggle')}>
    <i class="ti ti-chevron-left"></i>
  </button>
  <!-- Ресайз-хендл: тягни правий край, щоб розширити/звузити sidebar.
       Зберігається в localStorage. Стиснутий sidebar не ресайзиться. -->
  {#if !sidebarCollapsed}
    <div class="sb-resize" class:dragging={sbResizing} onmousedown={startSidebarResize} title={t('sidebar.resize')}></div>
  {/if}
    <div class="logo-area">
      <div class="logo-icon-box logo-icon-img sb-puff-host" class:beshanga-logo={beshangaOn} bind:this={logoBoxEl} onclick={logoParty}>
        <img src={appIcon} alt="Shaurma Launcher"/>
        {#each logoPuffs as p (p.id)}
          <span class="sb-puff" style="--dx:{p.dx};--dy:{p.dy};--h:{p.hue}"
                onanimationend={() => { logoPuffs = logoPuffs.filter((x) => x.id !== p.id) }}></span>
        {/each}
      </div>
      <div class="logo-text">
        <span class="logo-title">Shaurma</span>
        <span class="logo-sub">LAUNCHER</span>
      </div>
    </div>

    <div class="nav-list" style="padding-top:6px">
      <!-- Аналітика тут НЕ в списку: вона відкривається кнопкою у розділі
           «Мої збірки» (див. page-actions), як просив користувач. -->
      {#each (wardrobeVisible ? ['instances', 'browser', 'wardrobe'] : ['instances', 'browser']).concat(friendsNavVisible ? ['friends'] : [], ['servers'], ['settings']) as page}
        <!-- resetBrowser=true лише тут: клік по пункту меню «Браузер» —
             свідомий «чистий» вхід (як Home-кнопка), на відміну від
             звичайного повернення в розділ через інші дії лаунчера. -->
        <div class="nav-item" class:active={currentPage === page} class:just-deactivated={deactivatedNavPage === page} onclick={() => navigate(page, true)}>
          <i class="ti {pageIcons[page]}"></i>
          <span class="nav-label">{t(pageTitles[page])}</span>
          {#if page === 'friends' && (heads.length > 0 || friendsBadge > 0)}
            <!-- Голови друзів онлайн — НА КНОПЦІ «Друзі», а не окремим блоком:
                 клік по голові = клік по пункту (відкрити сторінку друзів). -->
            <span class="nav-aside">
              {#if heads.length > 0}
                <span class="nav-heads" title={`${t('social.me.friendsOnline')}: ${onlineFriends.length}`} aria-hidden="true">
                  {#each heads as f (f.accountId)}
                    <FriendAvatar nickname={f.nickname} color={f.color} status={f.presence?.status} ring="status" size={22} />
                  {/each}
                  {#if headsExtra > 0}<span class="nav-heads-more">+{headsExtra}</span>{/if}
                </span>
              {/if}
              {#if friendsBadge > 0}
                {#key friendsBadge}
                  <span class="nav-badge" title={t('friends.badgeHint')}>{friendsBadge > 9 ? '9+' : friendsBadge}</span>
                {/key}
              {/if}
            </span>
          {/if}
          {#if page === 'servers' && runningServers.length > 0}
            <!-- Іконки запущених серверів — НА КНОПЦІ «Сервери» (як голови друзів):
                 клік по іконці = клік по пункту. У згорнутому sidebar замість
                 них — зелена крапка біля іконки розділу. -->
            <span class="nav-aside">
              <span class="nav-heads nav-srvs" title={srvTitle} aria-hidden="true">
                {#each srvHeads as sv (sv.id)}
                  {@const ic = serverIcon(sv)}
                  <span class="nav-srv" style="--pcol:{ic.color}">
                    {#if ic.src}<img src={ic.src} alt="" draggable="false" />{:else}<i class="ti {ic.ti || 'ti-server-2'}"></i>{/if}
                  </span>
                {/each}
                {#if srvExtra > 0}<span class="nav-heads-more">+{srvExtra}</span>{/if}
              </span>
            </span>
            <i class="nav-live-dot" title={srvTitle}></i>
          {/if}
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
  {/if}

  {#if builds.length > 0}
    <div class="sb-packs" oncontextmenu={onPacksEmptyClick}>
      {#if tabPinned.length > 0}
        <div class="sb-pin-label"><i class="ti ti-pin"></i> {t('packs.pinned')}</div>
      {/if}
      {#each tabPinned as bv, i (bv.build.id)}
        {@const pk = bv.build}
        {@const status = bv.status}
        {@const prog = bv.progress}
        {@const pcol = pk.color || 'var(--orange)'}
        {@const othersRunning = otherRunningAccounts(bv)}
        {@const icon = packAsset(pk.iconUrl)}
        {@const iconSrc = packIconSrc(icon)}
        {@const lp = launchProgress[pk.id]}
        {@const sti = lp && !lp.done ? launchStageIcon(lp.stage) : null}
        {@const dsti = !sti ? syncStageIcon(bv) : null}
        {@const limg = lp && !lp.done && lp.stage === 'loader' ? loaderImgFor(pk.loaderType) : undefined}
        {@const glow = glowClass(bv, lp)}
        {@const mig = migrateByPack[pk.id]}
        {@const isDragSrc = dragSrcId === pk.id}
        {@const isDropTarget = dropTargetId === pk.id}
        <div class="sb-pack" class:active={currentPage === 'instances' && openPackId === pk.id} class:just-deactivated={deactivatedPackId === pk.id} class:sb-pinned={isPinned(pk.id)} class:running={status === 'running'} class:glow-green={glow === 'glow-green'} class:glow-orange={glow === 'glow-orange'} class:glow-red={glow === 'glow-red'} class:glow-blue={glow === 'glow-blue'} class:glow-yellow={glow === 'glow-yellow'} class:glow-mig={!!mig && !lp} class:drag-src={isDragSrc} class:drop-before={isDropTarget && dropPos === 'before'} class:drop-after={isDropTarget && dropPos === 'after'} data-pack-id={pk.id} onpointerdown={(e) => packPointerDown(e, pk.id, pcol)} onpointermove={packPointerMove} onpointerup={pointerDragEnd} oncontextmenu={(e) => openCtxMenu(e, pk.id)} onclick={(e: any) => { if (suppressPackClick) return; if (e.button === 0) openPackPage(pk.id) }} style="--pcol:{pcol};--drop-c:{dragPackColor}" title={sidebarCollapsed ? pk.name : undefined} in:fly={{ y: 6, duration: 260, easing: cubicOut, delay: Math.min(i, 8) * 28 }} animate:flip={{ duration: 220 }}>                <div class="sp-ic" class:has-icon={!!icon} class:blink={status === 'downloading' || status === 'updating'} style="--pcol:{pcol}">
            {#if iconSrc}
              <AnimatedIcon src={iconSrc} paused={shouldPauseIcon(icon, 'sidebar', currentPage === 'instances' && openPackId === pk.id)} alt="" />
            {:else}
              <i class="ti {pk.icon || 'ti-puzzle'}"></i>
            {/if}
          </div>
          <div class="sp-info">
            <div class="sp-name">
              <span class="sp-name-txt">{pk.name}</span>
              {#if autoPackIds.has(pk.id)}
                <span class="sp-auto-badge" title={t('sidebar.autoBadgeHint')}>{t('sidebar.autoBadge')}</span>
              {/if}
            </div>
            <div class="sp-sub" class:live={!!lp && !lp.done || status === 'downloading' || status === 'updating'}>
              {#if lp && !lp.done}
                <i class="ti ti-loader spin"></i>
                <span>{launchStageLabel(lp.stage)}</span>
              {:else if mig && !lp}
                <span class="mig-chip" title={mig.currentFile}>
                  <i class="ti ti-arrows-right-left spin-slow"></i>
                  <span>{t('mods.migratingTitle')} {mig.done}/{mig.total}</span>
                </span>
              {:else if status === 'downloading' || status === 'updating'}
                <i class="ti {prog?.mode === 'paused' ? 'ti-player-pause' : 'ti-download'}"></i>
                <span>{packSubText(bv, lp)}</span>
              {:else if othersRunning.length}
                <!-- Голови акаунтів, у яких ця збірка ЗАРАЗ запущена (крім
                     поточного) — замість «версія · лоадер» у sidebar. -->
                <span class="sp-heads" title={othersRunning.map(o => o.username).join(', ')}>
                  {#each othersRunning as o (o.id)}
                    <img src={headFor(o.uuid)} alt="" title={o.username} />
                  {/each}
                </span>
              {:else}
                <span>{packSubText(bv, lp)}</span>
              {/if}
            </div>
            {#if lp && !lp.done && lp.message}
              <div class="sp-stage"><i class="ti ti-file"></i> {lp.message}</div>
            {/if}
          </div>
          {#if sti}
            <!-- Під час встановлення Java/лоадера кнопка стає «Стоп»
                 (скасувати встановлення): червона Java, іконка лоадера
                 збірки, крутіння оновлення чи лупа — клік м'яко зупиняє
                 качку компонентів гри (раніше зупинити було неможливо
                 до кінця завантаження). -->
            <button class="sp-st st-{lp.stage} sp-stop" onclick={(e: any) => { e.stopPropagation(); cancelGameInstall(pk.id) }} title={launchStageLabel(lp.stage) + ' — ' + t('pack.stop')}>
              {#if limg}
                <img src={limg} alt="" />
              {:else}
                <i class="ti {sti.icon}"></i>
              {/if}
            </button>
          {:else if dsti}
            <!-- Індикатор стадії синхронізації файлів (downloading/
                 manifest_fetched/extracting/finalizing) — той самий
                 принцип, що для sti вище, окрім паузи (там лишається
                 кнопка «продовжити»). -->
            <div class="sp-st {dsti.cls}" title={downloadStageLabel(prog?.stage)}>
              <i class="ti {dsti.icon}"></i>
            </div>
          {:else if mig && !lp}
            <button class="sp-btn sp-mig" disabled title={t('mods.migratingTitle')}>
              <i class="ti ti-arrows-right-left"></i>
            </button>
          {:else}
            <button class="sp-btn sp-{spBtnClass(bv)}" onclick={(e: any) => { e.stopPropagation(); packBtnAction(bv) }} title={spBtnTitle(bv)}>
              {#if status === 'running'}<i class="ti ti-player-stop"></i>
              {:else if status === 'ready'}<i class="ti ti-player-play"></i>
              {:else if status === 'needs-update'}<i class="ti ti-refresh"></i>
              {:else if status === 'not-installed'}<i class="ti ti-download"></i>
              {:else if prog?.mode === 'paused'}<i class="ti ti-player-pause"></i>
              {:else if status === 'downloading' || status === 'updating'}<i class="ti ti-player-pause"></i>
                  {:else}<i class="ti ti-loader spin"></i>
              {/if}
            </button>
          {/if}
          {#if lp && !lp.done}
            <div class="sp-prog"><div class="sp-prog-fill" style="width:{launchStagePct(lp)}%;background:{launchStageColor(lp.stage)}"></div></div>
          {:else if prog && (status === 'downloading' || status === 'updating')}
            <div class="sp-prog"><div class="sp-prog-fill" style="width:{prog.percent}%;background:{downloadStageColor(prog.stage)}"></div></div>
          {/if}
        </div>
      {/each}
      {#each layoutItems as item, i (item.kind === 'folder' ? 'fld:' + item.id : 'pk:' + item.id)}
        {#if item.kind === 'folder'}
          <!-- Папка в ЄДИНОМУ списку sidebar (custom-таб): акордеон-заголовок
               зі стеком іконок збірок, розкриття показує збірки всередині.
               Папка стоїть НА СВОЄМУ МІСЦІ серед збірок — її можна
               перетягнути МІЖ двома збірками (і навпаки); дроп збірки на
               заголовок додає її в папку; тягнути збірку з папки назовні —
               витягнути в головний список. -->
          {@const fv = folderById.get(item.id)}
          {#if fv}
            {@const folder = fv.folder}
            {@const fpcol = folder.color || 'var(--orange)'}
            {@const isFolderOpen = openFolderIds.includes(folder.id)}
            {@const fIsDropTarget = dragFolderTarget === folder.id}
            {@const fIcons = fv.packs.slice(0, 3).map((p: BuildView) => {
              const fic = packAsset(p.build.iconUrl)
              return { src: packIconSrc(fic), paused: shouldPauseIcon(fic, 'sidebar', false), pcol: p.build.color || fpcol, preset: p.build.icon || 'ti-puzzle' }
            })}
            {@const fEmpty = fv.packs.length === 0}
            <div class="sb-folder" class:open={isFolderOpen} class:folder-drop={fIsDropTarget} style="--pcol:{fpcol}" data-folder-drop={folder.id}>
              <div class="sf-row" class:drag-src={dragFolderId === folder.id} class:drop-before={dropTargetId === folder.id && dropPos === 'before'} class:drop-after={dropTargetId === folder.id && dropPos === 'after'} data-folder-id={folder.id} onclick={() => { if (suppressPackClick) return; toggleFolder(folder.id) }} onpointerdown={(e) => folderPointerDown(e, folder.id)} onpointermove={folderPointerMove} onpointerup={pointerDragEnd} oncontextmenu={(e) => openFolderCtxMenu(e, folder.id)} title={folder.name}>
                <div class="sf-stack">
                  {#if fEmpty}
                    <!-- Порожня папка: іконка папки замість стеку збірок -->
                    <span class="sf-stack-fallback sf-stack-empty" style="--pcol:{fpcol}"><i class="ti {safeFolderIcon(folder.icon)}"></i></span>
                  {:else}
                    {#each fIcons as fic, fii (fii)}
                      {#if fic.src}
                        <AnimatedIcon src={fic.src} paused={fic.paused} alt="" />
                      {:else}
                        <span class="sf-stack-fallback" style="--pcol:{fic.pcol}"><i class="ti {fic.preset}"></i></span>
                      {/if}
                    {/each}
                  {/if}
                </div>
                <div class="sf-meta">
                  <div class="sf-name">{folder.name}</div>
                  <div class="sf-count">{fv.packs.length} {t('packs.folderCount')}</div>
                </div>
                <i class="ti ti-chevron-down sf-arrow"></i>
              </div>
              {#if isFolderOpen}
                <div class="sf-packs">
                <div class="sf-packs-inner">
                  {#each fv.packs as bv, fi (bv.build.id)}
                    {@const pk = bv.build}
                    {@const status = bv.status}
                    {@const prog = bv.progress}
                    {@const pcol = pk.color || 'var(--orange)'}
                    {@const icon = packAsset(pk.iconUrl)}
                    {@const iconSrc = packIconSrc(icon)}
                    {@const lp = launchProgress[pk.id]}
                    {@const glow = glowClass(bv, lp)}
                    {@const mig = migrateByPack[pk.id]}
                    {@const isDragSrc = dragSrcId === pk.id}
                    {@const isDropTarget = dropTargetId === pk.id}
                    <div class="sb-pack" class:active={currentPage === 'instances' && openPackId === pk.id} class:running={status === 'running'} class:drag-src={isDragSrc} class:drop-before={isDropTarget && dropPos === 'before'} class:drop-after={isDropTarget && dropPos === 'after'} data-pack-id={pk.id} onpointerdown={(e) => packPointerDown(e, pk.id, pcol, true)} onpointermove={packPointerMove} onpointerup={pointerDragEnd} oncontextmenu={(e) => openCtxMenu(e, pk.id)} onclick={(e: any) => { if (suppressPackClick) return; if (e.button === 0) openPackPage(pk.id) }} style="--pcol:{pcol};--drop-c:{dragPackColor}" title={sidebarCollapsed ? pk.name : undefined} in:fly={{ y: 5, duration: 220, easing: cubicOut, delay: Math.min(fi, 6) * 24 }} animate:flip={{ duration: 220 }}>
                      <div class="sp-ic" class:has-icon={!!iconSrc} style="--pcol:{pcol}">
                        {#if iconSrc}
                          <AnimatedIcon src={iconSrc} paused={shouldPauseIcon(icon, 'sidebar', currentPage === 'instances' && openPackId === pk.id)} alt="" />
                        {:else}
                          <i class="ti {pk.icon || 'ti-puzzle'}"></i>
                        {/if}
                      </div>
                      <div class="sp-info">
                        <div class="sp-name">
              <span class="sp-name-txt">{pk.name}</span>
              {#if autoPackIds.has(pk.id)}
                <span class="sp-auto-badge" title={t('sidebar.autoBadgeHint')}>{t('sidebar.autoBadge')}</span>
              {/if}
            </div>
                        <div class="sp-sub" class:live={!!lp && !lp.done || status === 'downloading' || status === 'updating'}>
                          {#if lp && !lp.done}<span>{launchStageLabel(lp.stage)}</span>
                          {:else}<span>{packSubText(bv, lp)}</span>
                          {/if}
                        </div>
                      </div>
                    </div>
                  {/each}
                </div>
                </div>
              {/if}
            </div>
          {/if}
        {:else}
          {@const bv = buildById.get(item.id)}
          {#if bv}
        {@const pk = bv.build}
        {@const status = bv.status}
        {@const prog = bv.progress}
        {@const pcol = pk.color || 'var(--orange)'}
        {@const othersRunning = otherRunningAccounts(bv)}
        {@const icon = packAsset(pk.iconUrl)}
        {@const iconSrc = packIconSrc(icon)}
        {@const lp = launchProgress[pk.id]}
        {@const sti = lp && !lp.done ? launchStageIcon(lp.stage) : null}
        {@const dsti = !sti ? syncStageIcon(bv) : null}
        {@const limg = lp && !lp.done && lp.stage === 'loader' ? loaderImgFor(pk.loaderType) : undefined}
        {@const glow = glowClass(bv, lp)}
        {@const mig = migrateByPack[pk.id]}
        {@const isDragSrc = dragSrcId === pk.id}
        {@const isDropTarget = dropTargetId === pk.id}
        <div class="sb-pack" class:active={currentPage === 'instances' && openPackId === pk.id} class:just-deactivated={deactivatedPackId === pk.id} class:sb-pinned={isPinned(pk.id)} class:running={status === 'running'} class:glow-green={glow === 'glow-green'} class:glow-orange={glow === 'glow-orange'} class:glow-red={glow === 'glow-red'} class:glow-blue={glow === 'glow-blue'} class:glow-yellow={glow === 'glow-yellow'} class:glow-mig={!!mig && !lp} class:drag-src={isDragSrc} class:drop-before={isDropTarget && dropPos === 'before'} class:drop-after={isDropTarget && dropPos === 'after'} data-pack-id={pk.id} onpointerdown={(e) => packPointerDown(e, pk.id, pcol)} onpointermove={packPointerMove} onpointerup={pointerDragEnd} oncontextmenu={(e) => openCtxMenu(e, pk.id)} onclick={(e: any) => { if (suppressPackClick) return; if (e.button === 0) openPackPage(pk.id) }} style="--pcol:{pcol};--drop-c:{dragPackColor}" title={sidebarCollapsed ? pk.name : undefined} in:fly={{ y: 6, duration: 260, easing: cubicOut, delay: Math.min(i, 8) * 28 }}>                <div class="sp-ic" class:has-icon={!!icon} class:blink={status === 'downloading' || status === 'updating'} style="--pcol:{pcol}">
            {#if iconSrc}
              <AnimatedIcon src={iconSrc} paused={shouldPauseIcon(icon, 'sidebar', currentPage === 'instances' && openPackId === pk.id)} alt="" />
            {:else}
              <i class="ti {pk.icon || 'ti-puzzle'}"></i>
            {/if}
          </div>
          <div class="sp-info">
            <div class="sp-name">
              <span class="sp-name-txt">{pk.name}</span>
              {#if autoPackIds.has(pk.id)}
                <span class="sp-auto-badge" title={t('sidebar.autoBadgeHint')}>{t('sidebar.autoBadge')}</span>
              {/if}
            </div>
            <div class="sp-sub" class:live={!!lp && !lp.done || status === 'downloading' || status === 'updating'}>
              {#if lp && !lp.done}
                <i class="ti ti-loader spin"></i>
                <span>{launchStageLabel(lp.stage)}</span>
              {:else if mig && !lp}
                <span class="mig-chip" title={mig.currentFile}>
                  <i class="ti ti-arrows-right-left spin-slow"></i>
                  <span>{t('mods.migratingTitle')} {mig.done}/{mig.total}</span>
                </span>
              {:else if status === 'downloading' || status === 'updating'}
                <i class="ti {prog?.mode === 'paused' ? 'ti-player-pause' : 'ti-download'}"></i>
                <span>{packSubText(bv, lp)}</span>
              {:else if othersRunning.length}
                <!-- Голови акаунтів, у яких ця збірка ЗАРАЗ запущена (крім
                     поточного) — замість «версія · лоадер» у sidebar. -->
                <span class="sp-heads" title={othersRunning.map(o => o.username).join(', ')}>
                  {#each othersRunning as o (o.id)}
                    <img src={headFor(o.uuid)} alt="" title={o.username} />
                  {/each}
                </span>
              {:else}
                <span>{packSubText(bv, lp)}</span>
              {/if}
            </div>
            {#if lp && !lp.done && lp.message}
              <div class="sp-stage"><i class="ti ti-file"></i> {lp.message}</div>
            {/if}
          </div>
          {#if sti}
            <!-- Під час встановлення Java/лоадера кнопка стає «Стоп»
                 (скасувати встановлення): червона Java, іконка лоадера
                 збірки, крутіння оновлення чи лупа — клік м'яко зупиняє
                 качку компонентів гри (раніше зупинити було неможливо
                 до кінця завантаження). -->
            <button class="sp-st st-{lp.stage} sp-stop" onclick={(e: any) => { e.stopPropagation(); cancelGameInstall(pk.id) }} title={launchStageLabel(lp.stage) + ' — ' + t('pack.stop')}>
              {#if limg}
                <img src={limg} alt="" />
              {:else}
                <i class="ti {sti.icon}"></i>
              {/if}
            </button>
          {:else if dsti}
            <!-- Індикатор стадії синхронізації файлів (downloading/
                 manifest_fetched/extracting/finalizing) — той самий
                 принцип, що для sti вище, окрім паузи (там лишається
                 кнопка «продовжити»). -->
            <div class="sp-st {dsti.cls}" title={downloadStageLabel(prog?.stage)}>
              <i class="ti {dsti.icon}"></i>
            </div>
          {:else if mig && !lp}
            <button class="sp-btn sp-mig" disabled title={t('mods.migratingTitle')}>
              <i class="ti ti-arrows-right-left"></i>
            </button>
          {:else}
            <button class="sp-btn sp-{spBtnClass(bv)}" onclick={(e: any) => { e.stopPropagation(); packBtnAction(bv) }} title={spBtnTitle(bv)}>
              {#if status === 'running'}<i class="ti ti-player-stop"></i>
              {:else if status === 'ready'}<i class="ti ti-player-play"></i>
              {:else if status === 'needs-update'}<i class="ti ti-refresh"></i>
              {:else if status === 'not-installed'}<i class="ti ti-download"></i>
              {:else if prog?.mode === 'paused'}<i class="ti ti-player-pause"></i>
              {:else if status === 'downloading' || status === 'updating'}<i class="ti ti-player-pause"></i>
                  {:else}<i class="ti ti-loader spin"></i>
              {/if}
            </button>
          {/if}
          {#if lp && !lp.done}
            <div class="sp-prog"><div class="sp-prog-fill" style="width:{launchStagePct(lp)}%;background:{launchStageColor(lp.stage)}"></div></div>
          {:else if prog && (status === 'downloading' || status === 'updating')}
            <div class="sp-prog"><div class="sp-prog-fill" style="width:{prog.percent}%;background:{downloadStageColor(prog.stage)}"></div></div>
          {/if}
        </div>
          {/if}
        {/if}
      {/each}

      {#if sidebarOnlyPinned && tabPinned.length === 0}
        <div class="sb-empty-hint"><i class="ti ti-pin"></i> {t('sidebar.onlyPinnedEmpty')}</div>
      {:else if !(sidebarOnlyPinned && tabPinned.length > 0) && layoutItems.length === 0}
        <!-- Активна вкладка порожня (але збірки є в іншій) — раніше тут була
             порожня сіра зона без жодної дії: користувач не знав, що можна
             зробити. Компактний стан з діями під вкладку: для «Шаурма» —
             пошук у браузері + імпорт, для «Кастомних» — створити + імпорт. -->
        <div class="sb-tab-empty">
          <i class="ti {instanceTab === 'shaurma' ? 'ti-flame' : 'ti-packages'}"></i>
          <span>{t(instanceTab === 'shaurma' ? 'sidebar.emptyShaurma' : 'sidebar.emptyCustom')}</span>
          <div class="sb-tab-empty-acts">
            {#if instanceTab === 'custom'}
              <button class="btn btn-primary btn-sm" onclick={openCreatePack}>
                <i class="ti ti-plus"></i> {t('instances.create')}
              </button>
              <button class="btn btn-ghost btn-sm" onclick={openImportPack}>
                <i class="ti ti-download"></i> {t('createPack.tab.import')}
              </button>
            {:else}
              <button class="btn btn-primary btn-sm" onclick={() => { navigate('browser', true) }}>
                <i class="ti ti-world-search"></i> {t('nav.browser')}
              </button>
              <button class="btn btn-ghost btn-sm" onclick={openImportPack}>
                <i class="ti ti-download"></i> {t('createPack.tab.import')}
              </button>
            {/if}
          </div>
        </div>
      {/if}
    </div>
  {/if}

  {#if emptyMenu}
    <!-- Меню по кліку (ПКМ) на пусте місце sidebar: швидкі дії (створити /
         імпортувати збірку, створити папку, відкрити теку зі збірками).
         Fixed-позиція у viewport — не тікає при скролі списку збірок. -->
    <div class="sb-empty-backdrop" onclick={closeEmptyMenu} oncontextmenu={(e) => { e.preventDefault(); closeEmptyMenu() }}></div>
    <div class="sb-empty-menu" style="left:{emptyMenu.x}px;top:{emptyMenu.y}px" role="menu" tabindex="-1"
         oncontextmenu={(e) => e.preventDefault()}
         onclick={(e: any) => e.stopPropagation()}>
      <button role="menuitem" onclick={() => { closeEmptyMenu(); openCreatePack() }}>
        <i class="ti ti-plus"></i> <span>{t('instances.create')}</span>
      </button>
      <button role="menuitem" onclick={() => { closeEmptyMenu(); openImportPack() }}>
        <i class="ti ti-download"></i> <span>{t('createPack.tab.import')}</span>
      </button>
      <div class="sb-empty-menu-sep"></div>
      {#if instanceTab === 'custom'}
        <button role="menuitem" onclick={() => { closeEmptyMenu(); openCreateFolderModal() }}>
          <i class="ti ti-folder-plus"></i> <span>{t('packs.folderCreate')}</span>
        </button>
        <div class="sb-empty-menu-sep"></div>
      {/if}
      <button role="menuitem" onclick={() => { closeEmptyMenu(); openPacksFolder() }}>
        <i class="ti ti-folder-open"></i> <span>{t('packs.openInstancesFolder')}</span>
      </button>
    </div>
  {/if}

  {#if builds.length === 0}
    <!-- Порожній sidebar: немає жодної збірки — іконка, напис і кнопки
         «Створити збірку» / «Імпортувати», що відкривають меню створення. -->
    <div class="sb-empty">
      <i class="ti ti-packages"></i>
      <span>{t('instances.empty')}</span>
      <div class="sb-empty-acts">
        <button class="btn btn-primary btn-sm" onclick={openCreatePack}><i class="ti ti-plus"></i> {t('instances.create')}</button>
        <button class="btn btn-ghost btn-sm" onclick={openImportPack}><i class="ti ti-download"></i> {t('createPack.tab.import')}</button>
      </div>
    </div>
  {/if}

  <div class="sidebar-footer" class:open={accDropdownOpen} onclick={toggleAccDropdown}>
    {#if accDropdownOpen}
      <div
        class="acc-dropdown"
        onclick={(e: any) => e.stopPropagation()}
        in:scale={{ duration: 180, start: 0.94, opacity: 0, easing: cubicOut }}
        out:scale={{ duration: 130, start: 0.94, opacity: 0, easing: cubicOut }}
      >
        <!-- Статус — окремий пункт зверху (як у Discord): праворуч виїжджає підменю. -->
        <button class="acc-st-row" class:open={statusMenu} bind:this={stRowEl} disabled={!canPickStatus}
                onmouseenter={openStatusMenu}
                onclick={() => (statusMenu ? (statusMenu = false) : openStatusMenu())}
                aria-haspopup="menu" aria-expanded={statusMenu}
                title={canPickStatus ? myStatusText : t('social.me.offlineHint')}>
          <i class="um-dot {myStatus}"></i>
          <span class="acc-st-lbl">{myStatusText}</span>
          <i class="ti ti-chevron-right acc-st-chev"></i>
        </button>
        {#if statusMenu}
          <div class="acc-st-sub" role="menu" style="left:{stPos.left}px;bottom:{stPos.bottom}px"
               onclick={(e: any) => e.stopPropagation()}>
            <button role="menuitem" class="acc-st-it" class:sel={myStatus === 'online'} onclick={() => pickStatus('online')}>
              <i class="um-dot online"></i><span>{t('social.me.online')}</span>
            </button>
            <button role="menuitem" class="acc-st-it" class:sel={myStatus === 'away' && presenceMode === 'away'} onclick={() => pickStatus('away')}>
              <i class="um-dot away"></i><span>{t('social.me.away')}</span>
            </button>
            <button role="menuitem" class="acc-st-it" class:sel={myStatus === 'dnd'} onclick={() => pickStatus('dnd')}>
              <i class="um-dot dnd"></i><span>{t('social.me.dnd')}<small>{t('social.me.dndHint')}</small></span>
            </button>
            <button role="menuitem" class="acc-st-it" class:sel={myStatus === 'invisible'} onclick={() => pickStatus('invisible')}>
              <i class="um-dot invisible"></i><span>{t('social.me.invisible')}<small>{t('social.me.invisibleHint')}</small></span>
            </button>
          </div>
        {/if}
        <div class="acc-dd-sep"></div>
        <button class="acc-dd-profile" onclick={() => { profileOpen = true; accDropdownOpen = false }}>
          <i class="ti ti-user-circle"></i> {t('acc.modal.open')}
          <i class="ti ti-3d-cube-sphere acc-dd-profile-3d"></i>
        </button>
        <div class="acc-dd-sep"></div>
        <div class="acc-dd-label">{t('accounts')}</div>
        {#each accounts as acc}
          <div class="acc-dd-item" class:active={acc.id === activeAccountId} onclick={() => switchAccount(acc.id)}>
            <div class="adi-av"><img src={headFor(acc.uuid)} alt="" /></div>
            <div class="adi-info">
              <div class="adi-name">
                {acc.username}
                {#if !acc.isLicensed && acc.licensedNickMine}
                  <!-- Пріоритет ліцензійного акаунта: скіном під цим ніком
                       керує ліцензійний акаунт — редактор скіна закритий. -->
                  <span class="adi-lock" title={t('licensedNickBusy')}><i class="ti ti-lock"></i></span>
                {/if}
              </div>
              <div
                class="adi-sub"
                class:licensed={acc.isLicensed}
                class:shaurma={!acc.isLicensed && acc.isShaurmaSkinAccount}
                class:pirated={!acc.isLicensed && !acc.isShaurmaSkinAccount}
              >
                <i class="ti {acc.isLicensed ? 'ti-rosette-discount-check' : acc.isShaurmaSkinAccount ? 'ti-cloud-upload' : 'ti-alert-triangle'}"></i>
                {acc.isLicensed ? t('licensed') : acc.isShaurmaSkinAccount ? t('shaurmaAccount') : t('notLicensed')}
              </div>
            </div>
            <button class="acc-dd-remove" onclick={(e: any) => { e.stopPropagation(); removeAccount(acc.id) }} title={t('acc.delete')}>
              <i class="ti ti-trash"></i>
            </button>
            {#if !acc.isLicensed}
              <button class="acc-dd-rename" onclick={(e: any) => { e.stopPropagation(); openRenameAccount(acc) }} title={t('acc.rename')}>
                <i class="ti ti-edit"></i>
              </button>
            {/if}
          </div>
        {/each}
        <div class="acc-dd-sep"></div>
        <div class="acc-dd-add" onclick={onaddaccount}>
          <i class="ti ti-plus"></i> {t('addAccount')}
        </div>
      </div>
    {/if}
    <div class="user-avatar ua-v2">
      <!-- Обличчя вирізається маскою під крапку статусу (прозоре кільце, а не
           «обгризений» кут від overflow:hidden) — крапка завжди ідеально кругла. -->
      <span class="ua-face"><img src={headFor(activeAccount()?.uuid)} alt="" /></span>
      <!-- Стан мережі — у САМОМУ віджеті акаунта (а не бейджем під логотипом):
           зелена крапка = інтернет є, wifi-off = офлайн-режим. Видно і в
           згорнутому sidebar — там віджет акаунта лишається один. -->
      <!-- Без title: стан мережі при наведенні більше не пишемо (колір крапки лишається). -->
      <i class="ua-net {avDot}" aria-hidden="true"></i>
    </div>
    <div class="user-meta">
      <div class="un">{activeAccount()?.username || t('notLoggedIn')}</div>
      <!-- Підпис за ТИПОМ акаунта, а не за сирим type: офлайн-акаунт із коміркою Шаурми
           (isShaurmaSkinAccount) — це «Шаурма-акаунт», а не «Офлайн». Та сама логіка, що й
           у випадному списку акаунтів вище. -->
      <div class="ue-row">
        <!-- Одне значення, без підміни при наведенні: що саме — тип акаунта чи статус —
             вибирає користувач у «Вигляд» акаунта (accountPrefs.widgetLine). -->
        <div class="ue ue-type" title={accTypeLabel}>
          {#if accountPrefs.widgetLine === 'status'}
            <span class="ue-k ue-status {avDot}" data-testid="account-status">{myStatusText}</span>
          {:else}
            <span class="ue-type-badge">
              {#if activeAccount()}
                {@const a = activeAccount()!}
                {#if a.isLicensed}
                  <span class="ue-k licensed"><i class="ti ti-rosette-discount-check"></i>{t('account.type.' + a.type)}</span>
                {:else if a.isShaurmaSkinAccount}
                  <span class="ue-k shaurma"><i class="ti ti-cloud-check"></i>{t('shaurmaAccount')}</span>
                {:else}
                  <span class="ue-k offline"><i class="ti ti-user-off"></i>{t('account.type.' + a.type)}</span>
                {/if}
              {:else}
                <span class="ue-k offline">{t('offline')}</span>
              {/if}
            </span>
          {/if}
        </div>
      </div>
    </div>
    <button class="dot-btn" title={t('accounts')}><i class="ti ti-chevron-up"></i></button>
  </div>
</aside>

{#if profileOpen}
  <ProfileModal
    mode="self"
    account={activeAccount()}
    head={headFor(activeAccount()?.uuid)}
    {social}
    statusKind={myStatus}
    statusText={myStatusText}
    onclose={() => (profileOpen = false)}
  />
{/if}
