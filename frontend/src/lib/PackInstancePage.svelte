<script lang="ts">
  // ── Сторінка деталей збірки Shaurma ───────────────────────────────────
  // Показує одну офіційну збірку: банер, статус завантаження/гри та вкладки
  // (Сервери / Основне / Продуктивність / Гра / Файли / Моди). Версія гри,
  // лоадер і фон НЕ редагуються тут навмисно — вони приходять з маніфесту
  // збірки (packIndex) і на бекенді для shaurma-збірок немає жодного методу,
  // який би їх міняв. Редагуються лише RAM, JVM-аргументи, вікно гри,
  // команди та список серверів — усе це зберігається per-instance через
  // App.SaveInstanceConfig і при відсутності override успадковує глобальні
  // налаштування лаунчера.
  //
  // Кожен розділ живе в окремому файлі (PackServersSection / PackBasicSection
  // / PackPerfSection / PackGameSection / PackFilesPanel / PackModsSection),
  // тут лише спільний стан (конфіг збірки cfg, налаштування, вкладка) та
  // скелет сторінки.
  import { untrack } from 'svelte'
  import PackPageDesign from './PackPageDesign.svelte'
  import { t } from './i18n.svelte'
  import { App, on } from './wails'
  import { toast, confirmDialog } from './toast.svelte'
  import UiModal from './UiModal.svelte'
  import { packAsset } from './packAssets.svelte'
  import { registerPendingConfig, clearPendingConfig } from './pendingConfigFlush'
  import { getPackUI, setPackUI, readContentScroll, restoreContentScroll } from './uiState'
  import PackServersSection from './PackServersSection.svelte'
  import PackOfficialServerWidget from './PackOfficialServerWidget.svelte'
  import { smartImage } from './smartImage'
  import PackBasicSection from './PackBasicSection.svelte'
  import PackPerfSection from './PackPerfSection.svelte'
  import PackGameSection from './PackGameSection.svelte'
  import PackFilesPanel from './PackFilesPanel.svelte'
  import PackModsSection from './PackModsSection.svelte'
  import PackContentSection from './PackContentSection.svelte'
  import PackWikiSection from './PackWikiSection.svelte'
  import ScreenshotsPanel from './ScreenshotsPanel.svelte'
  import ErrorPanel from './ErrorPanel.svelte'
  import GameConsole from './GameConsole.svelte'
  import ExportPackModal from './ExportPackModal.svelte'
  import ShareModal from './ShareModal.svelte'
  import BlockedModsWarning from './BlockedModsWarning.svelte'
  import type { PackIndexEntry, InstanceConfig, InstanceServer, Settings, BuildView, Account, BuildKind, LaunchProgress, BrowserVersionView, BlockedModInfo } from './types'
  import type { PackRow, PackFolderRow, PackPickerItem } from './PackPicker.svelte'
  import { LAUNCH_STAGES, launchStageLabel, downloadStageLabel } from './launchStages'

  let {
    pack,
    settings,
    playtimeSeconds,
    gameRunning,
    buildView = null,
    allBuilds = [],
    // Дані списку збірок з ПАПКАМИ (PackPicker) для модалки «Копіювати в
    // іншу збірку» — збірки з папок видно і можна вибрати, папки
    // розкриваються як у sidebar (App.svelte будує через customLayoutItems).
    packPicker = null,
    runningAccounts = [],
    headOf = () => '',
    launchProgress = null,
    // Помилка встановлення (з App.svelte, download:error/installer:log) —
    // згортувана панель біля прогресу качки, а не пів екрана тексту.
    installError = '',
    consoleSignal = 0,
    gallerySignal = 0,
    // Лічильник запитів «відкрити Моди» з передзапускової модалки
    // попередження про дублікати (App.svelte): перемикає вкладку на
    // «Моди» навіть якщо сторінка збірки ВЖЕ відкрита (setPackUI
    // читається лише при маунті), і передається у PackModsSection як
    // dupFocusSignal — той розгортає віджет дублікатів одразу.
    modsFocusSignal = 0,
    // Timestamp (Date.now()) старту сесії гри на ПОТОЧНОМУ акаунті. Коли
    // гра запущена на цьому акаунті — сторінка показує живий лічильник
    // «у сесії» (вмикається налаштуванням settings.showSessionTime).
    sessionStartedAt = null as number | null,
    onBack,
    onLaunch,
    // onLaunchOnServer — та сама точка входу, що onLaunch, для кнопок
    // «Приєднатись» (розділ «Сервери», віджет офіційного сервера): веде
    // через ідентичний ланцюг health-guard/mod-check/ram-check, лише з
    // адресою сервера на останньому кроці (див. коментар у App.svelte
    // joinPinnedServer). Прокидається далі в PackOfficialServerWidget і
    // PackServersSection замість того, щоб вони самі викликали
    // App.LaunchInstanceOnServer напряму.
    onLaunchOnServer,
    onStop,
    // Скасувати встановлення компонентів гри (Java/лоадер/асети) під час
    // запуску — доступно з toolbar, бо кнопка «Грати» під час качки
    // компонентів стає заблокованою і зупинити інакше не можна.
    cancelGameInstall = () => {},
    onDownload,
    onDelete,
    onPackUpdated = () => {},
    // Після зміни ID кастомної збірки (назви теки) — переключити відкриту
    // сторінку на новий ID (старий зникає зі списку збірок).
    onPackRenamed = (_oldId: string, _newId: string) => {},
    onSearchMods = null as null | (() => void),
    // Кнопка «Додати з браузера» у вкладці «Контент» → відкрити Браузер
    // контенту з контекстом збірки на конкретному типі ('mod' |
    // 'resourcepack' | 'datapack' | 'shader').
    onSearchContent = null as null | ((projectType: string) => void),
    onOpenModPage = null as null | ((source: string, projectId: string) => void),
    // Активний акаунт лаунчера (для «Скіна збірки» — чи акаунт ліцензійний,
    // та для списку пресетів Гардероба саме цього акаунта).
    activeAccount = null as Account | null,
    fmtPlaytime,
    fmtSessionTime,
    // Прогрес міграції модів (з App.svelte — mods:migrate-progress): банер
    // показує, який мод зараз переноситься, з іконкою і прогрес-баром.
    migration = null,
    // «Поділитися» збіркою з друзями (обидві редакції).
    canShare = false
  } = $props<{
    pack: PackIndexEntry
    settings: Settings
    playtimeSeconds: number
    gameRunning: boolean
    buildView: BuildView | null
    // Живий прогрес міграції модів (StartModMigration): назва/іконка
    // поточного мода + лічильники. null = міграція не йде.
    migration?: { done: number; total: number; currentFile: string; name: string; iconUrl: string; status: string } | null
    // ВСІ збірки (Шаурма + кастомні) — для модалки «Копіювати в іншу збірку»:
    // у ній мають бути і кольори, і іконки, і обидва типи збірок, і їх може
    // бути багато (пошук + скрол).
    allBuilds?: BuildView[]
    // Дані списку збірок з ПАПКАМИ для модалки копіювання сервера.
    packPicker?: {
      items: PackPickerItem[]
      rowById: Map<string, PackRow>
      folderById: Map<string, PackFolderRow>
    } | null
    // Акаунти, у яких ця збірка ЗАРАЗ запущена на ІНШИХ акаунтах (крім
    // поточного — він і так бачить кнопку «Стоп») — голови під банером.
    runningAccounts?: Account[]
    // Голова скіна акаунта (data URL) — передає батьківська App.svelte.
    headOf?: (a: Account) => string
    // Активний акаунт лаунчера (для «Скіна збірки»).
    activeAccount?: Account | null
    sessionStartedAt?: number | null
    launchProgress?: LaunchProgress | null
    // Лічильник запитів «відкрити консоль» з модалки краху (App.svelte):
    // при кожному інкременті сторінка перемикається на вкладку «Консоль».
    consoleSignal?: number
    // Лічильник запитів «відкрити галерею» (з плаваючого віджету нового
    // скріншоту, App.svelte) — той самий підхід, що consoleSignal.
    gallerySignal?: number
    // Лічильник запитів «відкрити Моди» (передзапускове попередження про
    // дублікати) — перемикає вкладку на «Моди» без ремаунта сторінки.
    modsFocusSignal?: number
    installError?: string
    onBack: () => void
    onLaunch: (id: string) => void
    onLaunchOnServer: (id: string, address: string) => void
    onStop: () => void
    cancelGameInstall?: (instanceId: string) => void
    onDownload: (id: string) => void
    onDelete: (id: string) => void
    // Після редагування кастомної збірки (назва/іконка/колір/версія) —
    // оновлення списку збірок, щоб сторінка/сайдбар показали зміни одразу.
    onPackUpdated?: () => void
    // Після зміни ID збірки (назви теки) — переключити відкриту сторінку
    // на новий ID (старий зникає зі списку збірок).
    onPackRenamed?: (oldId: string, newId: string) => void
    // Кнопка «Шукати моди» в менеджері модів → відкрити Браузер контенту
    // з контекстом цієї збірки.
    onSearchMods?: null | (() => void)
    // Кнопка «Додати з браузера» у вкладці «Контент» → відкрити Браузер
    // контенту з контекстом збірки на конкретному типі.
    onSearchContent?: null | ((projectType: string) => void)
    // Клік по іконці/назві мода в менеджері модів → відкрити сторінку
    // цього мода в Браузері контенту (лише якщо мод пов'язаний з
    // Modrinth/CurseForge — має source+projectId).
    onOpenModPage?: null | ((source: string, projectId: string) => void)
    fmtPlaytime: (s: number) => string
    // Формат mm:ss / h:mm:ss — для живого таймера сесії кнопки «Стоп»,
    // на відміну від fmtPlaytime (заокруглено до хвилин, для загального
    // накопиченого часу гри).
    fmtSessionTime: (s: number) => string
    canShare?: boolean
  }>()

  let tab = $state<'servers' | 'basic' | 'perf' | 'game' | 'files' | 'content' | 'console' | 'gallery' | 'health' | 'wiki'>('servers')
  // Модалка Pack Doctor — показується перед запуском, якщо є проблеми
  let healthModalPackId = $state('')
  let healthModalOpen   = $state(false)
  let pendingLaunchAfterHealth = $state('')  // packId для запуску після перевірки
  let actionsOpen = $state(false)
  // Модалка «Експорт збірки» (.mrpack/.zip) — відкривається кнопкою
  // «Експорт» у toolbar або пунктом меню «⋯».
  let exportOpen = $state(false)
  let shareOpen = $state(false)

  // ── «Оновити» для інтернет-збірок (Modrinth/CurseForge) ──────────────
  // У Шаурма-збірки оновлення йде одразу (поточна версія з маніфесту).
  // У кастомної Modrinth/CurseForge-збірки спершу показуємо СПИСОК ВЕРСІЙ
  // з джерела — користувач обирає, до якої оновлювати (UpdatePackFromSource
  // робить diff-імпорт: прибрані файли видаляються, свої лишаються).
  let updateDialog = $state<{
    versions: BrowserVersionView[]
    loading: boolean
    selectedId: string
    error: string
  } | null>(null)
  let updatingVersion = $state(false)
  // Модалка «збірка запущена — зміни заборонені» (оновлення версії збірки).
  let runningLocked = $state(false)

  // Загальний вхід для «Оновити»: Шаурма → одразу DownloadPack;
  // Modrinth/CurseForge → діалог зі списком версій (isSourcePack
  // оголошено нижче, поруч із kind, від якого залежить).
  function handleUpdate() {
    if (isSourcePack) {
      openUpdateDialog()
      return
    }
    onDownload(pack.id)
  }

  async function openUpdateDialog() {
    const src = buildView?.build.source
    const pid = buildView?.build.sourceProjectId
    if (!src || !pid) return
    updateDialog = { versions: [], loading: true, selectedId: '', error: '' }
    try {
      const versions = (await App.GetBrowserProjectVersions(src, pid, 'modpack', '', '')) ?? []
      if (!updateDialog) return
      updateDialog = {
        versions,
        loading: false,
        // За замовчуванням — перша (найновіша) версія.
        selectedId: versions[0]?.id ?? '',
        error: '',
      }
    } catch (e: any) {
      if (updateDialog) updateDialog = { versions: [], loading: false, selectedId: '', error: String(e) }
    }
  }

  async function doUpdatePack() {
    if (!updateDialog?.selectedId || updatingVersion) return
    // Збірка запущена (на будь-якому акаунті) — оновлювати версію
    // заборонено: бекенд все одно відхилить (errPackRunning), але
    // кажемо користувачу модалкою ДО спроби, як у розділі «Контент».
    if (isThisPackRunning || (runningAccounts?.length ?? 0) > 0) {
      updateDialog = null
      runningLocked = true
      return
    }
    updatingVersion = true
    try {
      await App.UpdatePackFromSource(pack.id, updateDialog.selectedId)
      toast(t('pack.updateDone', { name: pack.name }), 'success')
      updateDialog = null
      // Оновити картку/список збірок (нова версія гри/лоадера в банері).
      onPackUpdated()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      updatingVersion = false
    }
  }

  // ── Розділ «Галерея» ──────────────────────────────────────────────────
  // Вкладка показується ЛИШЕ якщо у збірки є хоча б один скріншот —
  // порожню галерею показувати немає смислу (ТЗ). Перевіряємо разом з
  // рештою даних сторінки; якщо після цього гравець зробить перший
  // скріншот під час гри, вкладка з'явиться сама (той самий ефект нижче
  // реагує на подію нового скріншоту з App.svelte).
  let hasScreenshots = $state(false)

  async function checkScreenshots() {
    try {
      const list = await App.ListScreenshots(pack.id)
      hasScreenshots = list.length > 0
    } catch {
      hasScreenshots = false
    }
  }

  $effect(() => { if (pack?.id) checkScreenshots() })

  function closeActionsMenu() { actionsOpen = false }

  // ── Фон банеру/героя збірки ──
  // Якщо є фон (background_url) — картинка; якщо ні — градієнт у КОЛЬОРІ
  // збірки (pack.color). Раніше суміш була 30% кольору / 70% майже
  // чорного (#1d150c→#120c06) — при будь-якому кольорі це виглядало як
  // суцільно чорний банер з ледь помітним відтінком (баг: "чому банер
  // чорний?"). Тепер суміш нахилена сильніше на бік кольору збірки і
  // другий стоп світліший — колір збірки видно одразу, а не лише при
  // придивлянні. Текст банера лишається білим (--ib-name у style.css),
  // тому темний градієнт тут і далі потрібен ЯК ФОН для контрасту з
  // білим текстом — просто вже не "чорний", а помітно кольоровий.
  function bgStyle(p: PackIndexEntry): string {
    const bg = packAsset(p.background_url)
    if (bg) return `background-image:url('${bg}')`
    const c = p.color || 'var(--orange)'
    return `background:linear-gradient(135deg, color-mix(in srgb, ${c} 55%, #241a10), color-mix(in srgb, ${c} 22%, #120c06))`
  }

  // ── Банер авто-збірок = картинка актуальної версії гри ───────────────
  // auto-vanilla/auto-snapshot власного фону не мають (background_url у них
  // завжди порожній). Банер підставляється ЗА ТИПОМ збірки:
  //   • auto-vanilla  → картинка останнього РЕЛІЗУ (GetNewsBanner, та сама
  //     що на головній);
  //   • auto-snapshot → картинка САМОГО снапшота зі статті minecraft.net
  //     (GetSnapshotBanner). Раніше тут теж ішла картинка релізу — баг.
  // Для снапшота картинки релізу НЕ підставляємо навіть як запасну: якщо
  // статтю не знайдено, лишається градієнт кольору збірки.
  let autoBanner = $state('')
  $effect(() => {
    // Тягнемо картинку ЛИШЕ коли з'ясувалось, що це авто-збірка (щоб не
    // робити зайвий виклик для сотень звичайних збірок) і лише один раз.
    if (!isAutoPack || autoBanner) return
    if (autoKind === 'snapshot') {
      App.GetSnapshotBanner()
        .then((url) => { if (url) autoBanner = url })
        .catch(() => { /* статті ще нема / offline — лишається градієнт */ })
      return
    }
    App.GetNewsBanner()
      .then((url) => { if (url) autoBanner = url })
      .catch(() => { /* offline / Mojang недоступний — лишається градієнт */ })
    // Бекенд сам перевіряє банер релізу при старті — картинка може приїхати
    // пізніше (подія news:banner стосується лише релізу).
    const off = on('news:banner', (p: { dataUrl?: string }) => {
      if (p?.dataUrl) autoBanner = p.dataUrl
    })
    return () => off()
  })

  // Фон банера: власна картинка збірки (або градієнт), а для авто-збірок —
  // картинка версії, щойно вона приїхала. Функція (а не $derived), бо
  // викликається у трьох місцях розмітки, де pack гарантовано є.
  function bannerStyle(p: PackIndexEntry): string {
    if (isAutoPack && autoBanner) return `background-image:url('${autoBanner}')`
    return bgStyle(p)
  }

  // ── Лічильник поточної сесії ──
  // Живе значення «скільки гра вже запущена на цьому акаунті»: оновлюється
  // раз на секунду лише поки гра йде (settings.showSessionTime вимикає показ).
  // Effect винесено НИЖЧЕ оголошення status/isThisPackRunning (а не одразу
  // тут) — інакше звернення до isThisPackRunning всередині залежало б від
  // порядку інціалізації змінних у скрипті компонента.
  let sessionNow = $state(0)

  // ── Відновлення/збереження «останнього вікна» збірки ──────────────────
  // Вкладка + скрол сторінки збірки персистяться на кожній зміні і при
  // виході (перехід в іншу збірку, закриття лаунчера). При відкритті збірки
  // повертаємось саме туди, де були — навіть якщо між цим відвідали іншу.
  let viewScrollTimer: ReturnType<typeof setTimeout> | undefined

  // uiReady — поки НЕ відновлено збережену вкладку на маунті, ефект
  // збереження нижче не має писати дефолтну вкладку ('servers') і затирати
  // збережену. Раніше persist-ефект (оголошений першим) спрацьовував на
  // маунті ДО відновлення й перезаписував збережений розділ на дефолтний —
  // повернення в збірку завжди відкривало «Сервери».
  let uiReady = $state(false)

  function persistViewState() {
    const saved = getPackUI(pack.id)
    const scr = readContentScroll()
    setPackUI(pack.id, { tab, scroll: scr, scrolls: { ...(saved?.scrolls ?? {}), [tab]: scr } })
  }

  // Вкладка змінилась — зберігаємо сам розділ і відновлюємо скрол НОВОГО
  // розділу: кожен розділ (вкладка) має свій збережений скрол (scrolls),
  // тож повернення в уже відвіданий розділ стає саме туди, де зупинився
  // користувач, а не на позицію минулого розділу. Скрол попереднього
  // розділу вже збережений scroll-слухачем (persistViewState).
  $effect(() => {
    if (!uiReady) return
    // ЗАХИСТ (виправлення "null is not an object (evaluating 'pack.id')"):
    // pack — проп, який App.svelte передає з $derived(packIndex.find(...)).
    // Коли packIndex оновлюється (видалення/зникнення збірки зі списку) і
    // збірки з цим id більше нема, openPack стає null РАНІШЕ, ніж Svelte
    // встигає прибрати цей компонент з DOM — { #key openPack.id } перемонтує
    // сторінку лише на НАСТУПНОМУ тіку. У проміжку ефекти цього компонента
    // (реактивні на pack) можуть виконатись ще раз із pack === null.
    if (!pack) return
    const t2 = tab
    // Перемикання вкладки НЕ чіпає скрол: раніше тут викликався
    // restoreContentScroll() зі збереженим скролом нового розділу (+ повтори
    // кожні 120мс, поки вантажиться контент) — екран стрибав і «трясся».
    // Скрол розділу відновлюється лише при відкритті збірки (ефект нижче).
    untrack(() => {
      const saved = getPackUI(pack.id)
      const scr = readContentScroll()
      setPackUI(pack.id, { tab: t2, scroll: scr, scrolls: { ...(saved?.scrolls ?? {}), [t2]: scr } })
    })
  })

  // Утримуємо висоту панелі під час заміни вкладки ({#key tab} знищує DOM
  // і висота на мить падала до нуля → скролбар блимав, сторінка смикалась).
  let panelHost = $state<HTMLElement | null>(null)
  let panelHoldTimer: ReturnType<typeof setTimeout> | undefined
  $effect.pre(() => {
    tab
    const host = untrack(() => panelHost)
    if (!host) return
    const h = host.offsetHeight
    if (h > 0) host.style.minHeight = h + 'px'
    clearTimeout(panelHoldTimer)
    panelHoldTimer = setTimeout(() => { if (panelHost) panelHost.style.minHeight = '' }, 420)
  })

  // Запит «відкрити консоль» з модалки краху (App.svelte): перемикаємось
  // на вкладку «Консоль» при кожному інкременті лічильника.
  $effect(() => {
    if (consoleSignal > 0) tab = 'console'
  })

  // Запит «відкрити галерею» з плаваючого віджету нового скріншоту.
  $effect(() => {
    if (gallerySignal > 0) { hasScreenshots = true; tab = 'gallery' }
  })

  // Запит «відкрити Моди» з передзапускової модалки попередження про
  // дублікати. Вкладка «Моди» є лише у керованих (кастомних) збірок з
  // мод-лоадером — для решти сигнал ігноруємо (модалка все одно
  // показується лише там, де є моди).
  $effect(() => {
    if (!pack) return
    if (modsFocusSignal > 0 && (pack.loader_type || '').toLowerCase() !== 'vanilla') tab = 'content'
  })

  // На маунті: відновлюємо збережену вкладку (якщо вона існує для цього
  // типу збірки) і скрол. Скрол відновлюється з повторними спробами, бо
  // контент вкладок вантажиться асинхронно.
  let restoredFor = ''
  $effect(() => {
    if (!pack) return
    const id = pack.id
    // pack-об'єкт перестворюється при кожному оновленні індексу — без цієї
    // перевірки вкладка/скрол «відновлювались» знову і сторінку смикало.
    if (restoredFor === id) return
    restoredFor = id
    const saved = getPackUI(id)
    if (saved?.tab) {
      const known = ['servers', 'basic', 'perf', 'game', 'health'].includes(saved.tab)
        || ['content', 'files', 'console', 'gallery'].includes(saved.tab)
      if (known) tab = saved.tab as typeof tab
    }
    // Скрол відновлюємо саме для відновленого розділу (нова схема scrolls),
    // інакше — fallback на старий єдиний `scroll`. Контент вкладок
    // вантажиться асинхронно, тож відновлення робиться з повторними спробами.
    const restoredTarget = saved?.scrolls
      ? (saved.tab && saved.scrolls[saved.tab] !== undefined ? saved.scrolls[saved.tab] : 0)
      : (saved?.scroll ?? 0)
    restoreContentScroll(restoredTarget)
    // Вкладку відновлено — тепер збереження розділу/скролу дозволене.
    uiReady = true

    const el = document.querySelector('.content') as HTMLElement | null
    const onScroll = () => {
      clearTimeout(viewScrollTimer)
      viewScrollTimer = setTimeout(persistViewState, 150)
    }
    if (el) el.addEventListener('scroll', onScroll, { passive: true })
    window.addEventListener('pagehide', persistViewState)
    return () => {
      clearTimeout(viewScrollTimer)
      if (el) el.removeEventListener('scroll', onScroll)
      window.removeEventListener('pagehide', persistViewState)
      persistViewState()
    }
  })

  // ── Полагодити / Переставити заново ──
  // Небезпечні дії НЕ ховаються у меню «…» нагорі сторінки — вони живуть у
  // розділі «Продуктивність» (PackPerfSection), де кожна кнопка детально
  // описує, що саме зробить. Сторінка лише прокидає хендлери нижче.
  let repairing = $state(false)
  let reinstalling = $state(false)

  async function repairPack() {
    repairing = true
    toast(t('pack.repairing'), 'info')
    try {
      await App.RepairPack(pack.id)
      toast(t('pack.repaired'), 'success')
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      repairing = false
    }
  }

  async function reinstallPack() {
    const ok = await confirmDialog(
      t('pack.reinstallConfirm', { name: pack.name }),
      t('pack.reinstallConfirmHint'),
      { confirmLabel: t('pack.reinstall'), cancelLabel: t('cancel'), danger: true }
    )
    if (!ok) return
    reinstalling = true
    try {
      await App.ReinstallPack(pack.id)
      toast(t('pack.reinstalled'), 'success')
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      reinstalling = false
    }
  }

  // Акаунти лаунчера (для селекта «прив'язати збірку до акаунта») та
  // список світів збірки (для вибору цілі автоприєднання). Обидва
  // підвантажуються лінькво — лише коли відкривається вкладка «Гра».
  let accounts = $state<Account[]>([])
  let worlds = $state<string[]>([])
  let packServers = $state<InstanceServer[]>([])
  let gameTabLoaded = $state(false)

  // Світи і сервери з server.dat збірки перечитуються ПРИ КОЖНОМУ відкритті
  // вкладки «Гра»: гра могла створити новий світ або користувач міг додати
  // сервер — список має бути актуальним (раніше вантажився лише раз і
  // «застигав»). Кнопка «Оновити» у розділі теж кличе цю функцію.
  async function refreshGameTab() {
    try { worlds = (await App.ListInstanceWorlds(pack.id)) ?? [] } catch (e) { worlds = [] }
    try { packServers = (await App.ListPackServers(pack.id)) ?? [] } catch (e) { packServers = [] }
  }

  async function loadGameTabData() {
    if (gameTabLoaded) return
    gameTabLoaded = true
    try { accounts = await App.GetAccounts() } catch (e) { accounts = [] }
  }

  $effect(() => {
    if (tab === 'game') {
      loadGameTabData()
      refreshGameTab()
    } else if (tab === 'servers') {
      refreshGameTab()
    }
  })

  // Локальна робоча копія конфігурації — редагується в полях і зберігається
  // з дебаунсом, той самий підхід, що й у глобальних налаштуваннях.
  // Стартове значення нейтральне (порожній ID); реальний конфіг
  // підтягується в $effect нижче за актуальним pack.id, щоб не захоплювати
  // застаріле значення пропси при першому рендері.
  let cfg = $state<InstanceConfig>(emptyConfig(''))
  let cfgLoaded = $state(false)

  function emptyConfig(id: string): InstanceConfig {
    return {
      id, maxRamOverride: 0, minRamOverride: 0, javaArgsOverride: '',
      fullscreenOverride: null, windowWidthOverride: 0, windowHeightOverride: 0,
      preLaunchCommandOverride: '', wrapperCommandOverride: '', postExitCommandOverride: '',
      envVarsOverride: '', useCustomCommands: false, useSeparateJava: false, javaPathOverride: '',
      accountIdOverride: '', autoJoinEnabled: false, autoJoinType: 'world',
      autoJoinWorld: '', autoJoinServer: '',
      skinPresetId: '', skinRestoreOnExit: null
    }
  }

  // Нормалізація конфіга, що прийшов з бекенду. ВАЖЛИВО: Go-структура
  // InstanceConfig має `servers,omitempty`/`pinnedServerIps,omitempty`,
  // тому для збірки без збереженого конфіга бекенд повертає об'єкт БЕЗ
  // цих полів. Список серверів розділ "Сервери" тепер читає напряму зі
  // servers.dat (App.ListPackServers), а не з cfg — тут лише заповнюємо
  // решту відсутніх полів дефолтами.
  function normalizeConfig(raw: InstanceConfig): InstanceConfig {
    const base = emptyConfig(raw?.id ?? '')
    const c = { ...base, ...(raw ?? {}) }
    // Backward compat: старі конфіги (збережені ДО появи прапорця
    // useCustomCommands) мають непусті override команд, але поле відсутнє
    // (Go розпарсував у false). Без цього UI показав би «Глобально» і сховав
    // поля, хоча бекенд (app.go) ВСЕ ОДНО використовує ці команди при
    // запуску. Виводимо прапорець з наявності реальних override.
    const hasCommandOverrides = !!(c.preLaunchCommandOverride || c.wrapperCommandOverride || c.postExitCommandOverride || c.envVarsOverride)
    return {
      ...c,
      useCustomCommands: c.useCustomCommands || hasCommandOverrides,
      fullscreenOverride: c.fullscreenOverride ?? null
    }
  }

  async function loadConfig() {
    cfgLoaded = false
    try {
      cfg = normalizeConfig(await App.GetInstanceConfig(pack.id))
    } catch (e) {
      cfg = emptyConfig(pack.id)
    }
    cfgLoaded = true
  }

  // Перезавантажуємо конфіг щоразу, коли відкривається інша збірка.
  let lastPackId = ''
  let saveTimer: ReturnType<typeof setTimeout> | undefined
  $effect(() => {
    if (!pack) return
    if (pack.id !== lastPackId) {
      lastPackId = pack.id
      // Скасовуємо ще незбережений відкладений таймер попередньої збірки:
      // сам знімок (cfg на момент update()) далі коректний навіть без
      // цього, але явне скасування прибирає зайвий пізній запис під час
      // навігації.
      // Незбережену правку ПОПЕРЕДНЬОЇ збірки дописуємо до диска, а вже
      // потім читаємо конфіг нової (раніше таймер просто скасовувався —
      // зміна, зроблена менш ніж за 350 мс до переходу, губилась).
      void commitPending().then(() => loadConfig())
    }
  })

  // ВАЖЛИВО: `cfg` — реактивна змінна сторінки, спільна для ВСІХ збірок
  // (перезаписується в loadConfig() при переході на іншу збірку). Раніше
  // setTimeout-колбек читав `cfg` у МОМЕНТ СПРАЦЮВАННЯ (через 350мс), а не
  // в момент виклику saveConfig() — якщо користувач встигав перейти на
  // іншу збірку до спрацювання таймера, loadConfig() вже підмінював `cfg`
  // на конфіг ІНШОЇ збірки, і відкладене збереження писало чужі
  // JVM-аргументи/RAM під невірним ID (виглядало так, ніби налаштування
  // "стають глобальними" — насправді потрапляли не в ту збірку). Тепер
  // знімок cfg робиться одразу при виклику, а не всередині колбека.
  // pendingSnapshot — остання ще не записана версія конфіга (одна на
  // сторінку: перед переходом на іншу збірку її дописує commitPending).
  let pendingSnapshot: InstanceConfig | null = null

  async function commitPending(): Promise<boolean> {
    clearTimeout(saveTimer)
    const snap = pendingSnapshot
    if (!snap) return true
    pendingSnapshot = null
    try {
      await App.SaveInstanceConfig(snap)
      // Запис на диску актуальний — перед запуском гри повторно його
      // писати не треба (інакше старий знімок міг би перетерти свіжіші дані).
      if (!pendingSnapshot) clearPendingConfig(snap.id)
      return true
    } catch (e: any) {
      // Не вдалось — лишаємо знімок, якщо за цей час не з'явився новіший.
      if (!pendingSnapshot) pendingSnapshot = snap
      toast(t('error.withMessage', { message: e }), 'error')
      return false
    }
  }

  function saveConfig(snapshot: InstanceConfig, showToast = false) {
    pendingSnapshot = snapshot
    clearTimeout(saveTimer)
    saveTimer = setTimeout(async () => {
      const ok = await commitPending()
      if (ok && showToast) toast(t('pack.saved'), 'success')
    }, 350)
  }

  // flushConfig — примусове негайне збереження (скасовує debounce).
  // Викликається перед діями, які потребують актуального конфігу на
  // бекенді: запуск гри (health check читає RAM з диска), діагностика.
  // Пише лише те, що справді змінювалось: без правок диск уже актуальний,
  // а примусовий запис «поточного cfg» міг би перетерти поля, які бекенд
  // оновив після відкриття сторінки (наприклад, час гри).
  async function flushConfig(): Promise<void> {
    await commitPending()
  }

  function update<K extends keyof InstanceConfig>(key: K, value: InstanceConfig[K]) {
    cfg = { ...cfg, [key]: value }
    // Register for immediate flush before launch (RAM/JVM/window changes)
    registerPendingConfig(pack.id, cfg)
    saveConfig(cfg)
  }

  // ── Сервери ──
  // Розділ "Сервери" (PackServersSection.svelte) працює напряму зі
  // servers.dat через App.* методи (AddPackServer/UpdatePackServer/...),
  // тож тут окремих addServer/updateServer/removeServer більше не треба —
  // компонент лише отримує ID збірки.


  // Відкрити теку збірки у Провіднику (кнопка на сторінці збірки). Якщо
  // теки ще нема (збірку не качали) — бекенд повертає зрозумілу помилку,
  // показуємо її тостом.
  async function openFolder() {
    try {
      await App.OpenPackFolder(pack.id)
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  // Порядок визначення важливий: Svelte 5 компілює $derived у порядку
  // оголошення, тому залежні похідні мають йти ПІСЛЯ тих, на які спираються.
  const status = $derived(buildView?.status ?? 'not-installed')
  const buildProgress = $derived(buildView?.progress ?? null)
  // Збірка "встановлена" = файли є локально (buildView.installed). Раніше тут
  // був статус 'installed', якого взагалі не існує (статуси: not-installed /
  // needs-update / ready / downloading / updating / running / error) — тому
  // після успішної качки сторінка лишалась на екрані завантаження.
  const installed = $derived(
    buildView?.installed === true || status === 'ready' || status === 'needs-update'
  )
  const isDownloading = $derived(status === 'downloading' || status === 'updating')
  // Тип збірки (shaurma | custom) — звідси залежить, чи показувати lock-бейдж
  // «Керується лаунчером» (тільки для офіційних збірок Шаурма). Кастомні
  // збірки Шаурма-бекенд не чіпає, тож lock-бейдж для них — брехня.
  const kind: BuildKind | null = $derived(buildView?.build.kind ?? null)
  // Авто-збірки (Vanilla latest/Snapshot, керовані з Налаштувань): версію
  // гри та лоадер їм задає ЛИШЕ фонова синхронізація Mojang, тож розділ
  // «Основне» їм не потрібен — ховаємо вкладку повністю.
  let isAutoPack = $state(false)
  // 'release' | 'snapshot' — яка саме авто-збірка (від цього залежить банер).
  let autoKind = $state('')
  $effect(() => {
    if (!pack) return
    // ЗАХИСТ: pack захоплюється в замиканні на момент запуску ефекту, але
    // .then() спрацьовує ПІЗНІШЕ — якщо за цей час компонент отримав
    // pack === null (збірку прибрали зі списку) або взагалі розмонтувався,
    // читання pack.id тут кидало TypeError ("null is not an object").
    // Знімок id тут же, до асинхронного виклику, і повторна перевірка
    // pack у колбеку роблять це безпечним у будь-якому порядку подій.
    const packId = pack.id
    App.GetAutoPacks()
      .then((list) => {
        if (!pack) return
        const hit = (list ?? []).find(p => p.id === packId)
        isAutoPack = !!hit
        autoKind = hit?.kind ?? ''
      })
      .catch(() => { if (pack) { isAutoPack = false; autoKind = '' } })
  })
  $effect(() => {
    if (isAutoPack && tab === 'basic') tab = 'servers'
  })
  // Заблоковані моди при імпорті — завантажуються для кастомних збірок,
  // щоб показати попередження на сторінці ( BlockedModsWarning ).
  let blockedMods = $state<BlockedModInfo[]>([])
  function refreshBlockedMods() {
    if (kind === 'custom' && pack.id) {
      App.GetCustomPack(pack.id).then(p => {
        blockedMods = p.blockedMods ?? []
      }).catch(() => { blockedMods = [] })
    } else {
      blockedMods = []
    }
  }
  $effect(() => { refreshBlockedMods() })
  // Банер на всю ширину розділу (Налаштування → Вигляд → «Банер збірки на
  // всю ширину», packBannerFull): геро-блок на всю ширину контенту
  // (край-у-край, без паддингів), сайдбар лаунчера НЕ чіпаємо. За
  // замовчуванням вимкнено — звичайний банер-картка, як раніше.
  let usePackDesign = $state(false)
  const bannerFull = $derived(!!(settings as any).packBannerFull && !usePackDesign)
  // Іконка збірки (локальний асет або URL) — потрібна у банері, який у
  // повному режимі рендериться ДО тулбара (нагорі розділу).
  const bannerIcon = $derived(pack ? packAsset(pack.icon_url) : '')
  // Чи ця збірка є «інтернет-збіркою» з джерелом, з якого можна оновитись
  // (список версій у кнопці «Оновити» замість миттєвого DownloadPack).
  const isSourcePack = $derived(
    kind === 'custom'
      && (buildView?.build.source === 'modrinth' || buildView?.build.source === 'curseforge')
      && !!buildView?.build.sourceProjectId
  )
  // Кастомні збірки повністю керуються користувачем — вони завжди мають
  // розділи «Моди» і «Файли», незалежно від лоадера (навіть ваніль-збірка
  // може мати файли/конфіги/ресурс-паки для перегляду). Офіційні збірки
  // Шаурма керуються лаунчером (lock-бейдж), тож провідник їм не показуємо.
  const canManage = $derived(kind === 'custom')
  // Розділ «Моди» має сенс лише для збірок з мод-лоадерами: у ванільних
  // збірок (vanilla) вкладки «Моди» немає (ТЗ: «у луардерів ваніла є розділ
  // моди якого немає там бути»). «Файли» лишається — провідник файлів
  // корисний і для ванільних кастомних збірок (конфіги/ресурс-паки).
  const canMods = $derived(canManage && !!pack && (pack.loader_type || '').toLowerCase() !== 'vanilla')
  // Пауза: рушій перевів сесію в ModePaused — прогрес лишається видимим,
  // а кнопки міняються на «Продовжити» (не «Пауза»).
  const paused = $derived(buildProgress?.mode === 'paused')
  const noInternet = $derived(buildProgress?.noInternet === true)

  // Прогрес ЗАПУСКУ (не якання файлів): checking → worker_update → java →
  // loader. Показуємо сегментний бар, поки launchProgress живий і без
  // помилки; помилку показуємо окремим тостом (App.svelte вже такі шле),
  // тут — лише сегмент/підпис.
  const isLaunching = $derived(!!launchProgress && !launchProgress.done)
  const launchError = $derived(launchProgress?.error ?? null)

  // Чи ЦЯ конкретна збірка (pack.id) зараз запущена на ПОТОЧНОМУ акаунті —
  // саме те, від чого має залежати кнопка "Грати"/"Стоп" на сторінці
  // збірки. buildView.status приходить із App.svelte через build:started/
  // build:exit і вже прив'язаний до конкретного pack.id (на відміну від
  // пропса gameRunning, який є ГЛОБАЛЬНИМ прапорцем "чи хоч щось запущено
  // на цьому акаунті", без прив'язки до збірки).
  //
  // РАНІШЕ кнопка нижче перевіряла {#if gameRunning}: коли гра йшла на
  // збірці A, а користувач відкривав сторінку збірки B, кнопка на B теж
  // показувала "Стоп" (і натискання на неї викликало onStop() — зупиняло
  // гру збірки A, а не запускало B). Або навпаки: після перемикання назад
  // на збірку A кнопка могла лишитись у стані "Грати", хоча гра на A вже
  // йшла — залежно від порядку подій. В обох випадках кнопка не стежила
  // за реальним станом САМЕ цієї збірки в реальному часі.
  const isThisPackRunning = $derived(status === 'running')

  // Живий лічильник поточної сесії (кнопка «Стоп»): рахує секунди від
  // sessionStartedAt поки САМЕ ця збірка запущена. Розміщено тут, ПІСЛЯ
  // isThisPackRunning — effect читає її синхронно, тож порядок
  // оголошення важливий для надійного трекінгу залежності.
  $effect(() => {
    const started = sessionStartedAt
    if (!started || started <= 0 || !isThisPackRunning) {
      sessionNow = 0
      return
    }
    sessionNow = Math.max(0, Math.floor((Date.now() - started) / 1000))
    const timer = setInterval(() => {
      sessionNow = Math.max(0, Math.floor((Date.now() - started) / 1000))
    }, 1000)
    return () => clearInterval(timer)
  })

  // Стадії та підписи — у спільному модулі launchStages.ts (однакові
  // кольори/назви в sidebar, тайлах «Мої збірки» і консолі).

  // Реальний (не сегментний) прогрес-бар запуску: ОДНА лінія, яка росте
  // крізь стадії (кожна стадія = слот бару), а колір змінюється разом зі
  // стадією (жовтий → синій → червоний → зелений). Коли бекенд не знає
  // відсотка поточної стадії — бар показує рухомі «стрічки» (indeterminate).
  const launchStageIndex = $derived(
    LAUNCH_STAGES.findIndex(x => x.key === launchProgress?.stage)
  )
  const launchStageColor = $derived(
    launchStageIndex >= 0 ? LAUNCH_STAGES[launchStageIndex].color : 'var(--orange)'
  )
  // Колір заповнення бару: при помилці — червоний, інакше колір стадії.
  const launchBarColor = $derived(launchError ? 'var(--red)' : launchStageColor)
  const lpPercent = $derived.by(() => {
    if (launchError) return 100
    if (launchStageIndex < 0) return 0
    const slot = 100 / LAUNCH_STAGES.length
    const base = launchStageIndex * slot
    const pct = launchProgress?.percent ?? -1
    const within = pct >= 0 ? Math.min(pct, 100) : 0
    return Math.min(100, Math.round(base + (within / 100) * slot))
  })
  const lpIndeterminate = $derived(
    !!launchProgress && !launchProgress.done &&
    (launchProgress.percent ?? -1) < 0 && !launchError
  )

  // Підпис стадії якання файлів — downloadStageLabel з launchStages.ts.

  // Пауза / продовжити / скасувати качку збірки (бекенд: sync.Runner).
  function pauseDownload() { App.PauseDownload(pack.id) }
  function resumeDownload() { onDownload(pack.id) }
  // Скасування — з модалкою підтвердження: часткові файли буде видалено.
  async function cancelDownload() {
    const ok = await confirmDialog(t('pack.cancelConfirm'), t('pack.cancelConfirmHint'), {
      confirmLabel: t('pack.cancelDownload'), cancelLabel: t('cancel'), danger: true
    })
    if (ok) App.CancelDownload(pack.id)
  }

  $effect(() => {
    if (!actionsOpen) return
    document.addEventListener('click', closeActionsMenu)
    return () => document.removeEventListener('click', closeActionsMenu)
  })

  // ── Ковзна «пігулка» активної вкладки ──
  // Сучасний таббар: замість миготливого перефарбовування активної вкладки —
  // плавний індикатор-пігулка, що ковзає між вкладками (як у налаштуваннях).
  let tabbarEl = $state<HTMLElement | null>(null)
  // Висота тулбара (повноширинний банер): потрібна, щоб таббар лип
  // рівно ПІД тулбаром (sticky top) і щоб негативна маржа таббара
  // ставила його на нижній край банера. Вимірюємо фактичний тулбар
  // і кладемо в CSS-змінну --tb-h (fallback 56px на перший кадр).
  let toolbarEl = $state<HTMLElement | null>(null)
  let tbH = $state(56)
  function measureToolbar() {
    if (toolbarEl) tbH = Math.max(40, Math.round(toolbarEl.getBoundingClientRect().height))
  }
  $effect(() => {
    const el = toolbarEl
    if (!el) return
    const id = requestAnimationFrame(() => requestAnimationFrame(() => {
      measureToolbar()
      // Після того як шрифти/іконки завантажились, висота може змінитись
      setTimeout(measureToolbar, 120)
    }))
    window.addEventListener('resize', measureToolbar)
    return () => {
      cancelAnimationFrame(id)
      window.removeEventListener('resize', measureToolbar)
    }
  })
  // ── Закріплений тулбар у повноширинному банері ──
  // heroStuck — тулбар прилип до верху (даємо йому «скляний» фон);
  // heroTitleGone — велика назва банера заїхала під тулбар (показуємо чип).
  let heroStuck = $state(false)
  let heroTitleGone = $state(false)
  function updateHeroStuck() {
    const bar = toolbarEl
    if (!bar) return
    const sc = bar.closest<HTMLElement>('.content')
    if (!sc) return
    const bt = bar.getBoundingClientRect()
    heroStuck = bt.top <= sc.getBoundingClientRect().top + 1
    const title = (bar.nextElementSibling as HTMLElement | null)?.querySelector('.ib-title')
    heroTitleGone = !!title && title.getBoundingClientRect().bottom <= bt.bottom + 4
  }
  $effect(() => {
    const bar = toolbarEl
    if (!bar || !bannerFull) { heroStuck = false; heroTitleGone = false; return }
    const sc = bar.closest<HTMLElement>('.content')
    let raf = 0
    const tick = () => { cancelAnimationFrame(raf); raf = requestAnimationFrame(updateHeroStuck) }
    sc?.addEventListener('scroll', tick, { passive: true })
    window.addEventListener('resize', tick)
    // висота тулбара може змінитись (перенос кнопок, таймер сесії) — тримаємо --tb-h точним
    const ro = typeof ResizeObserver !== 'undefined' ? new ResizeObserver(() => { measureToolbar(); tick() }) : null
    ro?.observe(bar)
    tick()
    return () => {
      sc?.removeEventListener('scroll', tick)
      window.removeEventListener('resize', tick)
      ro?.disconnect()
      cancelAnimationFrame(raf)
    }
  })

  let tbIndL = $state(0)
  let tbIndW = $state(0)
  function measureTabPill() {
    const bar = tabbarEl
    if (!bar) return
    const btn = bar.querySelector<HTMLElement>('.tab-btn.active')
    if (!btn) return
    const br = bar.getBoundingClientRect()
    const r = btn.getBoundingClientRect()
    // Пильця ::before позиціонується відносно PADDING-боксу таббара (всередині
    // рамки 1px), а getBoundingClientRect міряє від border-боксу — тому відстань
    // до кнопки треба ЗМЕНШИТИ на ширину рамки (clientLeft), а не збільшити.
    // + scrollLeft: пігулка лежить усередині контейнера прокрутки.
    tbIndL = Math.max(0, r.left - br.left - (bar.clientLeft || 0) + bar.scrollLeft)
    tbIndW = r.width
    // Активна вкладка завжди в зоні видимості (вузьке вікно → прокрутка).
    const want = btn.offsetLeft - (bar.clientWidth - btn.offsetWidth) / 2
    if (Math.abs(bar.scrollLeft - want) > 1 && bar.scrollWidth > bar.clientWidth) {
      bar.scrollTo({ left: Math.max(0, want), behavior: 'smooth' })
    }
  }
  $effect(() => {
    const t = tab
    const hasShots = hasScreenshots
    // Таббар перераховується і при зміні набору вкладок (галерея/моди/файли)
    // та лічильника серверів — читаємо їх, щоб пігулка змістилась одразу.
    const manage = canManage
    const mods = canMods
    const scount = packServers.length
    if (!tabbarEl) return
    const id = requestAnimationFrame(() => requestAnimationFrame(measureTabPill))
    window.addEventListener('resize', measureTabPill)
    return () => {
      cancelAnimationFrame(id)
      window.removeEventListener('resize', measureTabPill)
    }
  })

  // ── Компактна ідентичність у закріпленому тулбарі ──
  // Поки банер з великою назвою/іконкою видно — тулбар чистий (кнопки
  // + «Назад»). Щойно банер «виїхав» за верх (верхній край банера
  // дійшов до нижнього краю тулбара), з'являється компактна плашка
  // «іконка + назва збірки» — завжди зрозуміло, сторінку якої збірки
  // гортаєш. Реагує на скрол контейнера .content та resize вікна.
  let toolbarStuck = $state(false)
  let bannerEl = $state<HTMLElement | null>(null)
  function updateToolbarStuck() {
    const bar = toolbarEl
    const ban = bannerEl
    if (!bar || !ban) { toolbarStuck = false; return }
    const br = bar.getBoundingClientRect()
    const bn = ban.getBoundingClientRect()
    // Чип з'являється, коли банер (з великою назвою) почав ховатись ПІД
    // тулбаром (його верх дійшов до верху тулбара) — не коли він просто
    // поруч унизу, інакше чип висів би весь час у верху сторінки.
    toolbarStuck = bn.top <= br.top + 10
  }
  $effect(() => {
    const bar = toolbarEl
    const ban = bannerEl
    if (!bar || !ban) { toolbarStuck = false; return }
    const sc = bar.closest<HTMLElement>('.content')
    const rafTick = () => requestAnimationFrame(updateToolbarStuck)
    sc?.addEventListener('scroll', rafTick, { passive: true })
    window.addEventListener('resize', rafTick)
    const id = requestAnimationFrame(() => requestAnimationFrame(updateToolbarStuck))
    return () => {
      sc?.removeEventListener('scroll', rafTick)
      window.removeEventListener('resize', rafTick)
      cancelAnimationFrame(id)
    }
  })
</script>

<PackPageDesign packId={pack.id} official={kind === 'shaurma'} background={packAsset(pack.background_url) || ''} onLayoutChange={(active) => usePackDesign = active}>
<!-- Єдиний рядок дій: «Назад» зліва, решта кнопок СПРАВА, і «⋯» з
     «Грати» в ОДНОМУ рядку поряд одне з одним.

     ПОВНОШИРИННИЙ РЕЖИМ (bannerFull): тулбар, назва/бейджі збірки і
     таббар вкладок лежать ОДНИМ суцільним геро-блоком просто на фоновому
     арті збірки — як у розділі «Контент» (Моди/Ресурспаки/Датапаки),
     що є еталоном цього стилю в лаунчері. Ніякого sticky, ніяких
     негативних margin/order-хитрощів між тулбаром і банером: банер —
     це фон САМОГО геро-блоку (position:absolute inset:0 z-index:0),
     а тулбар/назва/таббар — звичайні блоки в normal flow зверху нього
     (z-index:1). Просто, надійно, точно як у референсі.

     ЗВИЧАЙНИЙ РЕЖИМ (не bannerFull) — без жодних змін: банер-картка як
     і раніше рендериться окремим блоком через {@render instBannerContent()}
     нижче ({#if !bannerFull}), а тулбар — це .pack-toolbar без класу
     pt-full, теж без змін. -->
{#if bannerFull && installed}
  <!-- Тулбар — ЗАКРІПЛЕНИЙ (sticky) сусід геро-блоку, а не його дитина: геро має
       overflow:hidden і sticky усередині нього не працює (кнопки «Грати» тощо
       їхали за екран, а секції під ним залипали на tbH → «пробіл»). Геро
       заїжджає під тулбар через від'ємний margin-top = виміряна висота. -->
    <div class="pack-toolbar pt-full" class:pt-stuck={heroStuck} bind:this={toolbarEl}>
      <button class="btn btn-ghost btn-sm" onclick={onBack} title={t('pack.back')}><i class="ti ti-arrow-left"></i> {t('pack.back')}</button>
      <div class="pt-id" class:show={heroTitleGone} title={pack.name}>
        {#if bannerIcon}<span class="pt-id-ic"><img src={bannerIcon} alt="" use:smartImage /></span>{:else}<span class="pt-id-ic ti-fallback" style="--pcol:{pack.color || 'var(--orange)'}"><i class="ti {pack.icon || 'ti-puzzle'}"></i></span>{/if}
        <div class="pt-id-meta">
          <div class="pt-id-name">{pack.name}</div>
          <div class="pt-id-sub"><i class="ti ti-cube"></i> {pack.mc_version} · {pack.loader_type}</div>
        </div>
      </div>
      <div class="pt-spacer"></div>
      <button class="btn btn-ghost btn-sm" onclick={openFolder} title={t('pack.openFolder')}><i class="ti ti-folder-open"></i> {t('pack.openFolder')}</button>
      <button class="btn btn-ghost btn-sm" onclick={() => App.OpenPackConsole(pack.id)} title={t('pack.openConsole')}><i class="ti ti-terminal-2"></i> {t('pack.openConsole')}</button>
      {#if kind === 'custom'}
        <button class="btn btn-ghost btn-sm" onclick={() => exportOpen = true} title={t('pack.export')}><i class="ti ti-package-export"></i> {t('pack.export')}</button>
        {#if canShare}<button class="btn btn-ghost btn-sm" onclick={() => shareOpen = true} title="Поділитися з друзями"><i class="ti ti-share-3"></i> Поділитися</button>{/if}
      {/if}
      <div class="menu-wrap">
        <button class="btn btn-ghost btn-sm" onclick={(e: any) => { e.stopPropagation(); actionsOpen = !actionsOpen }} title={t('pack.update')}><i class="ti ti-dots-vertical"></i></button>
        {#if actionsOpen}
          <div class="dropdown-menu" onclick={(e: any) => e.stopPropagation()}>
            {#if kind === 'shaurma' || isSourcePack}
              <div class="dd-item" onclick={() => { actionsOpen = false; handleUpdate() }}>
                <i class="ti ti-refresh"></i> {t('pack.update')}
              </div>
            {/if}
            {#if kind === 'custom'}
              <div class="dd-item" onclick={() => { actionsOpen = false; exportOpen = true }}>
                <i class="ti ti-package-export"></i> {t('pack.export')}
              </div>
            {/if}
            <div class="dd-sep"></div>
            <div class="dd-item danger" onclick={() => { actionsOpen = false; onDelete(pack.id) }}>
              <i class="ti ti-trash"></i> {t('pack.delete')}
            </div>
          </div>
        {/if}
      </div>
      {#if isThisPackRunning}
        <button class="btn btn-danger" onclick={onStop}>
          <i class="ti ti-player-stop"></i> {t('pack.stop')}
          {#if settings.showSessionTime && sessionStartedAt && sessionStartedAt > 0}
            <span class="btn-time">{fmtSessionTime(sessionNow)}</span>
          {/if}
        </button>
      {:else if migration}
        <button class="btn btn-info" disabled title={t('mods.migratingTitle')}>
          <i class="ti ti-arrows-right-left"></i> {t('mods.migratingTitle')}
        </button>
      {:else if isDownloading}
        <button class="btn btn-warning" disabled title={status === 'updating' ? t('build.status.updating') : t('build.status.downloading')}>
          {#if status === 'updating'}
            <i class="ti ti-refresh spin"></i> {t('build.status.updating')}
          {:else}
            <i class="ti ti-download spin"></i> {t('build.status.downloading')}
          {/if}
        </button>
      {:else if status === 'needs-update'}
        <button class="btn btn-primary" onclick={handleUpdate}><i class="ti ti-refresh"></i> {t('pack.update')}</button>
      {:else if isLaunching}
        {@const lstage = launchProgress?.stage}
        <button
          class="btn btn-danger play-launch-btn launch-stop-btn"
          onclick={() => cancelGameInstall(pack.id)}
          title={t('pack.stop')}
        >
          <i class="ti ti-square"></i>
          {#if lstage && lstage !== 'checking'}
            {t('pack.stop')} · {launchStageLabel(lstage)}
          {:else}
            {t('pack.stop')}
          {/if}
        </button>
      {:else}
        <button class="btn btn-primary play-launch-btn" onclick={async () => { await flushConfig(); onLaunch(pack.id) }}>
          <i class="ti ti-player-play"></i> {t('pack.play')}
        </button>
      {/if}
    </div>
  <div class="pack-hero-full" style="{bannerStyle(pack)};--tb-h:{tbH}px">
    <div class="phf-shine" aria-hidden="true"></div>
    <div class="phf-overlay" aria-hidden="true"></div>

    <!-- Нижній рядок геро-блоку: зліва назва, справа бейджі статусу. Раніше бейджі були
         position:absolute у правому верхньому куті й лягали на кнопки тулбара. -->
    <div class="phf-bottom">
      <div class="ib-title">
        {#if bannerIcon}<span class="ib-ic"><img src={bannerIcon} alt="" use:smartImage /></span>{:else}<i class="ti {pack.icon || 'ti-puzzle'}" style="color:{pack.color || 'var(--orange)'}"></i>{/if}
        <div class="ib-meta">
          <div class="ib-name">{pack.name}</div>
          <div class="ib-badges">
            <span class="badge-mc"><i class="ti ti-cube"></i> {pack.mc_version} · {pack.loader_type}</span>
            {#if pack.tags?.length}
              {#each pack.tags as tag}<span class="ib-tag">{tag}</span>{/each}
            {/if}
          </div>
        </div>
      </div>
      <div class="phf-side">
      {#if kind === 'shaurma'}
      <div class="ib-lock" title={t('pack.locked.hint')}>
        <i class="ti ti-lock"></i> {t('pack.locked')}
      </div>
      {/if}
      {#if settings.showGameTimeOnPage && playtimeSeconds > 0}
      <div class="ib-time" title={t('pack.totalTimeDesc')}>
        <i class="ti ti-clock"></i> {fmtPlaytime(playtimeSeconds)}
      </div>
      {/if}
      {#if runningAccounts.length}
        <!-- Той самий віджет "хто ще грає", що й у звичайному (не-bannerFull)
             режимі нижче (.pip-running) — раніше в full-режимі геро-блоку
             цей блок був відсутній ВЗАГАЛІ (і зникав лише в цьому режимі,
             не через дизайн, а тому що його розмітку сюди просто не
             додали при впровадженні pack-hero-full). Стилізовано як
             .ib-lock/.ib-time (напівпрозора плашка на арт-фоні, той самий
             namespace .pack-hero-full > .ib-running у <style> нижче), а не
             як картку .pip-running (--bg-card) — та розрахована на
             непрозорий фон звичайної сторінки, на арт-банері виглядала б
             чужою плямою. -->
        <div class="ib-running" title={t('pack.runningOnAccounts')}>
          <i class="ti ti-users"></i>
          <span class="ib-running-heads">
            {#each runningAccounts as acc (acc.id)}
              <img src={headOf(acc)} alt="" title={acc.username} />
            {/each}
          </span>
        </div>
      {/if}

    
      </div>
    </div>

<div class="tabbar tb-slide tb-full" bind:this={tabbarEl} style="--tb-ind-l:{tbIndL}px;--tb-ind-w:{tbIndW}px">
      <button class="tab-btn" data-section="servers" class:active={tab === 'servers'} onclick={() => tab = 'servers'}>
        <i class="ti ti-server-2"></i> {t('pack.tab.servers')} <span class="num">{packServers.length}</span>
      </button>
      {#if hasScreenshots}
        <button class="tab-btn" data-section="gallery" class:active={tab === 'gallery'} onclick={() => tab = 'gallery'}>
          <i class="ti ti-photo"></i> {t('pack.tab.gallery')}
        </button>
      {/if}
      {#if canManage}
        <button class="tab-btn" data-section="content" class:active={tab === 'content'} onclick={() => tab = 'content'}>
          <i class="ti ti-puzzle"></i> {t('pack.tab.content')}
        </button>
        <button class="tab-btn" data-section="files" class:active={tab === 'files'} onclick={() => tab = 'files'}>
          <i class="ti ti-files"></i> {t('pack.tab.files')}
        </button>
      {/if}
      <button class="tab-btn" data-section="basic" class:active={tab === 'basic'} onclick={() => tab = 'basic'}>
        <i class="ti ti-info-circle"></i> {t('pack.tab.basic')}
      </button>
      <button class="tab-btn" data-section="perf" class:active={tab === 'perf'} onclick={() => tab = 'perf'}>
        <i class="ti ti-cpu"></i> {t('pack.tab.perf')}
      </button>
      <button class="tab-btn" data-section="game" class:active={tab === 'game'} onclick={() => tab = 'game'}>
        <i class="ti ti-device-gamepad-2"></i> {t('pack.tab.game')}
      </button>
      <button class="tab-btn" data-section="console" class:active={tab === 'console'} onclick={() => tab = 'console'}>
        <i class="ti ti-terminal-2"></i> {t('pack.tab.console')}
      </button>
    </div>
  </div>
{:else}
<div class="pack-toolbar" bind:this={toolbarEl}>
  <button class="btn btn-ghost btn-sm" onclick={onBack} title={t('pack.back')}><i class="ti ti-arrow-left"></i> {t('pack.back')}</button>
  <!-- Компактна ідентичність збірки у ЗАКРІПЛЕНОМУ тулбарі: з'являється,
       щойно банер з великою назвою/іконкою виїхав за верх при скролі
       (class:show). Той самий прийом, що плашка активної вкладки у
       ContentBrowser: у верху сторінки прихована (display:none, не займає
       місця), при скролі плавно з'являється — користувач завжди бачить,
       на сторінці ЯКОЇ збірки він зараз. -->
  <div class="pt-id" class:show={toolbarStuck} title={pack.name}>
    {#if bannerIcon}<span class="pt-id-ic"><img src={bannerIcon} alt="" use:smartImage /></span>{:else}<span class="pt-id-ic ti-fallback" style="--pcol:{pack.color || 'var(--orange)'}"><i class="ti {pack.icon || 'ti-puzzle'}"></i></span>{/if}
    <div class="pt-id-meta">
      <div class="pt-id-name">{pack.name}</div>
      <div class="pt-id-sub"><i class="ti ti-cube"></i> {pack.mc_version} · {pack.loader_type}</div>
    </div>
  </div>
  {#if runningAccounts.length}
    <!-- Акаунти, у яких ця збірка ЗАРАЗ запущена — квадратні голови з
         закругленнями прямо в тулбарі (не окремим віджетом під банером). -->
    <span class="pt-heads" title="{t('pack.runningOnAccounts')}: {runningAccounts.map((a: Account) => a.username).join(', ')}">
      {#each runningAccounts as acc (acc.id)}
        <img class="pt-head" src={headOf(acc)} alt="" title={acc.username} />
      {/each}
    </span>
  {/if}
  <div class="pt-spacer"></div>
  <button class="btn btn-ghost btn-sm" onclick={openFolder} title={t('pack.openFolder')}><i class="ti ti-folder-open"></i> {t('pack.openFolder')}</button>
  <button class="btn btn-ghost btn-sm" onclick={() => App.OpenPackConsole(pack.id)} title={t('pack.openConsole')}><i class="ti ti-terminal-2"></i> {t('pack.openConsole')}</button>
  {#if installed && kind === 'custom'}
    <button class="btn btn-ghost btn-sm" onclick={() => exportOpen = true} title={t('pack.export')}><i class="ti ti-package-export"></i> {t('pack.export')}</button>
    {#if canShare}<button class="btn btn-ghost btn-sm" onclick={() => shareOpen = true} title="Поділитися з друзями"><i class="ti ti-share-3"></i> Поділитися</button>{/if}
  {/if}
  {#if installed}
    <div class="menu-wrap">
      <button class="btn btn-ghost btn-sm" onclick={(e: any) => { e.stopPropagation(); actionsOpen = !actionsOpen }} title={t('pack.update')}><i class="ti ti-dots-vertical"></i></button>
      {#if actionsOpen}
        <div class="dropdown-menu" onclick={(e: any) => e.stopPropagation()}>
          {#if kind === 'shaurma' || isSourcePack}
            <!-- «Оновити» — лише там, де є що оновлювати: Шаурма-збірки
                 (remote-версія) або кастомні, імпортовані з Modrinth/
                 CurseForge (для них відкривається список версій). У
                 звичайної локальної збірки оновлення немає. -->
            <div class="dd-item" onclick={() => { actionsOpen = false; handleUpdate() }}>
              <i class="ti ti-refresh"></i> {t('pack.update')}
            </div>
          {/if}
          {#if kind === 'custom'}
            <div class="dd-item" onclick={() => { actionsOpen = false; exportOpen = true }}>
              <i class="ti ti-package-export"></i> {t('pack.export')}
            </div>
          {/if}
          <div class="dd-sep"></div>
          <div class="dd-item danger" onclick={() => { actionsOpen = false; onDelete(pack.id) }}>
            <i class="ti ti-trash"></i> {t('pack.delete')}
          </div>
        </div>
      {/if}
    </div>
    {#if isThisPackRunning}
      <button class="btn btn-danger" onclick={onStop}>
        <i class="ti ti-player-stop"></i> {t('pack.stop')}
        {#if settings.showSessionTime && sessionStartedAt && sessionStartedAt > 0}
          <span class="btn-time">{fmtSessionTime(sessionNow)}</span>
        {/if}
      </button>
    {:else if migration}
      <!-- Під час міграції модів гра не запускається — кнопка «Грати»
           стає заблокованою з написом «Міграція модів». -->
      <button class="btn btn-info" disabled title={t('mods.migratingTitle')}>
        <i class="ti ti-arrows-right-left"></i> {t('mods.migratingTitle')}
      </button>
    {:else if isDownloading}
      <!-- Під час завантаження/оновлення файлів збірки кнопка «Грати»
           НЕ показується: раніше ({:else} гілка нижче) вона лишалась
           активною «Грати» під час downloading/updating — клік по ній
           запускав гру з неповними файлами, а сам факт завантаження
           ігнорувався (скарга: «коли вантажиться чи оновлюється щось,
           кнопка грати показує грати, а не оновлюється»). Тепер —
           заблокована кнопка зі стадією, як у Java/лоадера. -->
      <button class="btn btn-warning" disabled title={status === 'updating' ? t('build.status.updating') : t('build.status.downloading')}>
        {#if status === 'updating'}
          <i class="ti ti-refresh spin"></i> {t('build.status.updating')}
        {:else}
          <i class="ti ti-download spin"></i> {t('build.status.downloading')}
        {/if}
      </button>
    {:else if status === 'needs-update'}
      <button class="btn btn-primary" onclick={handleUpdate}><i class="ti ti-refresh"></i> {t('pack.update')}</button>
    {:else if isLaunching}
      <!-- Під час качки компонентів гри (Java/лоадер/асети, стадії
           checking/worker_update/java/loader) кнопка «Грати» раніше була
           просто заблокована — зупинити качку неможливо. Тепер це активна
           червона кнопка «Стоп», що м'яко скасовує встановлення
           (CancelGameInstall): .part лишається, наступний запуск докачає. -->
      {@const lstage = launchProgress?.stage}
      <button
        class="btn btn-danger play-launch-btn launch-stop-btn"
        onclick={() => cancelGameInstall(pack.id)}
        title={t('pack.stop')}
      >
        <i class="ti ti-square"></i>
        {#if lstage && lstage !== 'checking'}
          {t('pack.stop')} · {launchStageLabel(lstage)}
        {:else}
          {t('pack.stop')}
        {/if}
      </button>
    {:else}
      <button class="btn btn-primary play-launch-btn" onclick={() => onLaunch(pack.id)}>
        <i class="ti ti-player-play"></i> {t('pack.play')}
      </button>
    {/if}
  {/if}
</div>
{/if}

{#if !installed}
  {@const icon = packAsset(pack.icon_url)}
  <div class="pack-empty" style={bannerStyle(pack)}>
    <div class="pe-inner">
      <div class="pe-ic">
        {#if icon}<img src={icon} alt="" />{:else}<i class="ti {pack.icon || 'ti-puzzle'}"></i>{/if}
      </div>
      <div class="pe-title">{pack.name}</div>
      <div class="pe-meta">
        <span class="pm-chip"><i class="ti ti-cube"></i>{pack.mc_version}</span>
        <span class="pm-chip"><i class="ti ti-package"></i>{pack.loader_type}</span>
        {#if pack.tags?.length}
          {#each pack.tags as tag}<span class="pm-chip">{tag}</span>{/each}
        {/if}
      </div>
      {#if pack.description}<div class="pe-desc">{pack.description}</div>{/if}
      {#if isDownloading && buildProgress}
        <div class="inst-progress pe-prog">
          <div class="ip-head">
            <div class="ip-label"><i class="ti ti-loader spin"></i> {buildProgress.currentFile || downloadStageLabel(buildProgress.stage)}</div>
            <div class="ip-pct">{buildProgress.percent}%</div>
          </div>
          <div class="ip-track"><div class="ip-fill" style="width:{buildProgress.percent}%"></div></div>
          <div class="ip-sub">
            <span>{downloadStageLabel(buildProgress.stage)}</span>
            <span>{t('download.files', { done: buildProgress.filesDone, total: buildProgress.filesTotal })}</span>
            <span>{(buildProgress.speedBps / 1024 / 1024).toFixed(1)} {t('download.speedUnit')}</span>
          </div>
          {#if noInternet}<div class="ip-nonet"><i class="ti ti-wifi-off"></i> {t('pack.noInternet')}</div>{/if}
          <div class="ip-actions">
            {#if paused}
              <button class="btn btn-primary btn-sm" onclick={resumeDownload}><i class="ti ti-player-play"></i> {t('pack.resume')}</button>
            {:else}
              <button class="btn btn-ghost btn-sm" onclick={pauseDownload}><i class="ti ti-player-pause"></i> {t('pack.pause')}</button>
            {/if}
            <button class="btn btn-danger btn-sm" onclick={cancelDownload}><i class="ti ti-x"></i> {t('pack.cancelDownload')}</button>
          </div>
        </div>
      {:else if isDownloading}
        <div class="inst-progress pe-prog">
          <div class="ip-head"><div class="ip-label"><i class="ti ti-loader spin"></i> {buildProgress?.currentFile || t('build.status.downloading')}</div></div>
          <div class="ip-track"><div class="ip-fill" style="width:0%"></div></div>
          <div class="ip-actions">
            <button class="btn btn-ghost btn-sm" onclick={pauseDownload}><i class="ti ti-player-pause"></i> {t('pack.pause')}</button>
            <button class="btn btn-danger btn-sm" onclick={cancelDownload}><i class="ti ti-x"></i> {t('pack.cancelDownload')}</button>
          </div>
        </div>
      {:else}
        <div class="pe-actions">
          <button class="btn btn-primary btn-lg pe-btn" onclick={() => onDownload(pack.id)} disabled={!!migration}>
            <i class="ti ti-download"></i> {t('pack.download')}
          </button>
          {#if kind === 'custom'}
            <!-- Кастомна збірка без файлів (щойно встановлена з браузера
                 контенту або порожня ручна) раніше не мала способу
                 видалитись зі сторінки — «⋯» ховалось усередині
                 {#if installed}. Тепер видалення доступне і тут. -->
            <button class="btn btn-ghost btn-lg" onclick={() => onDelete(pack.id)} title={t('pack.delete')}>
              <i class="ti ti-trash"></i>
            </button>
          {/if}
        </div>
      {/if}
    </div>
  </div>
{:else}
{#if !bannerFull}
  <!-- Звичайний (не-full) режим банера — картка у контенті, як і раніше,
     без жодних змін відносно оригіналу. -->
  <div class="inst-banner" bind:this={bannerEl} style={bannerStyle(pack)}>
    <div class="ib-shine" aria-hidden="true"></div>
    <div class="ib-title">
      {#if bannerIcon}<span class="ib-ic"><img src={bannerIcon} alt="" use:smartImage /></span>{:else}<i class="ti {pack.icon || 'ti-puzzle'}" style="color:{pack.color || 'var(--orange)'}"></i>{/if}
      <div class="ib-meta">
        <div class="ib-name">{pack.name}</div>
        <div class="ib-badges">
          <span class="badge-mc"><i class="ti ti-cube"></i> {pack.mc_version} · {pack.loader_type}</span>
          {#if pack.tags?.length}
            {#each pack.tags as tag}<span class="ib-tag">{tag}</span>{/each}
          {/if}
        </div>
      </div>
    </div>
    {#if kind === 'shaurma'}
    <div class="ib-lock" title={t('pack.locked.hint')}>
      <i class="ti ti-lock"></i> {t('pack.locked')}
    </div>
    {/if}
    {#if settings.showGameTimeOnPage && playtimeSeconds > 0}
    <div class="ib-time" title={t('pack.totalTimeDesc')}>
      <i class="ti ti-clock"></i> {fmtPlaytime(playtimeSeconds)}
    </div>
    {/if}
  </div>
{/if}

{#if blockedMods.length > 0}
  <!-- pack?.id: ці два віджети рендеряться ПОЗА вкладками (завжди, щойно
       сторінка змонтована), тож читають props-гетер найраніше — на них і
       падало «Cannot read properties of null» під час перечитування
       збірок. App.svelte більше не віддає null (останній відомий стан —
       lastOpenPack), але defensive-читання лишаємо: краще порожній id,
       ніж падіння сторінки. -->
  <BlockedModsWarning mods={blockedMods} packId={pack?.id ?? ''} onchange={refreshBlockedMods} />
{/if}


<!-- disabled={isThisPackRunning}: акаунт може паралельно грати в кілька
     РІЗНИХ збірок одночасно — блокувати "Приєднатись" тут потрібно лише
     якщо САМЕ ЦЯ збірка вже запущена (дублікат запуску тієї ж збірки на
     тому самому акаунті), а не через те, що щось інше йде паралельно. -->
<PackOfficialServerWidget packId={pack?.id ?? ''} serverIp={pack?.server_ip} disabled={isThisPackRunning} activeAccountId={activeAccount?.id ?? ''} onJoin={onLaunchOnServer} />

{#if isLaunching || launchError}
  <!-- Прогрес-бар запуску як в HTML-макеті: ОДНА лінія, що росте крізь
       стадії (перевірка → файли збірки → Java → лоадер), а колір змінюється
       разом зі стадією. Зверху — стадії-кроки (галочка = пройдена, спінер =
       активна), під баром — жива назва стадії та % поточної стадії. -->
  <div class="launch-prog">
    <div class="lp-stages">
      {#each LAUNCH_STAGES as s, i}
        {@const active = i === launchStageIndex}
        {@const passed = launchStageIndex >= 0 && i < launchStageIndex}
        <div class="lp-stage" class:active class:passed style="--seg-color:{s.color}">
          {#if active && !launchError}<i class="ti ti-loader spin"></i>
          {:else if passed}<i class="ti ti-check"></i>
          {:else}<i class="ti ti-circle"></i>{/if}
          <span>{launchStageLabel(s.key)}</span>
        </div>
      {/each}
    </div>
    <div class="lp-bar">
      <div class="lp-fill" class:indet={lpIndeterminate} style="--seg-color:{launchBarColor};width:{lpPercent}%"></div>
    </div>
    {#if launchError}
      <ErrorPanel title={t('pack.launchErrorTitle')} message={launchError} />
    {:else}
      <div class="lp-label">
        <i class="ti ti-loader spin"></i> {launchProgress?.message || launchStageLabel(launchProgress?.stage)}
        {#if launchProgress && launchProgress.percent != null && launchProgress.percent >= 0}
          <span class="lp-pct">{launchProgress.percent}%</span>
        {/if}
      </div>
    {/if}
  </div>
{/if}

{#if isDownloading && buildProgress}
  <div class="inst-progress">
    <div class="ip-head">
      <div class="ip-label"><i class="ti ti-loader spin"></i> {buildProgress.currentFile || downloadStageLabel(buildProgress.stage)}</div>
      <div class="ip-pct">{buildProgress.percent}%</div>
    </div>
    <div class="ip-track"><div class="ip-fill" style="width:{buildProgress.percent}%"></div></div>
    <div class="ip-sub">
      <span>{downloadStageLabel(buildProgress.stage)}</span>
      <span>{t('download.files', { done: buildProgress.filesDone, total: buildProgress.filesTotal })}</span>
      <span>{(buildProgress.speedBps / 1024 / 1024).toFixed(1)} {t('download.speedUnit')}</span>
    </div>
    {#if noInternet}<div class="ip-nonet"><i class="ti ti-wifi-off"></i> {t('pack.noInternet')}</div>{/if}
    {#if installError}
      <ErrorPanel title={t('install.errorTitle')} message={installError} accent="yellow" />
    {/if}
    <div class="ip-actions">
      {#if paused}
        <button class="btn btn-primary btn-sm" onclick={resumeDownload}><i class="ti ti-player-play"></i> {t('pack.resume')}</button>
      {:else}
        <button class="btn btn-ghost btn-sm" onclick={pauseDownload}><i class="ti ti-player-pause"></i> {t('pack.pause')}</button>
      {/if}
      <button class="btn btn-danger btn-sm" onclick={cancelDownload}><i class="ti ti-x"></i> {t('pack.cancelDownload')}</button>
    </div>
  </div>
{/if}

  {#if migration}
    <!-- Міграція модів у цю збірку (після створення/з діалогу): живий
         банер з іконкою та назвою поточного мода — як у Modrinth App. -->
    <div class="mig-banner">
      <div class="mig-banner-head">
        <span class="mig-banner-title"><i class="ti ti-arrows-right-left spin-slow"></i> {t('mods.migratingTitle')}</span>
        <span class="mig-banner-pct mono">{migration.total ? Math.round(migration.done / migration.total * 100) : 0}%</span>
      </div>
      <div class="mig-banner-track"><div class="mig-banner-fill" style="width:{migration.total ? Math.round(migration.done / migration.total * 100) : 0}%"></div></div>
      <div class="mig-banner-current">
        {#if migration.iconUrl}<img src={migration.iconUrl} alt="" loading="lazy" use:smartImage />{:else}<span class="mig-banner-ic"><i class="ti ti-puzzle"></i></span>{/if}
        <span class="mig-banner-name">{migration.name || migration.currentFile}</span>
        <span class="mono mig-banner-file">{migration.currentFile}</span>
      </div>
      <div class="mig-banner-sub">{t('mods.migratingSub')} — {migration.done}/{migration.total}</div>
    </div>
  {/if}

{#if !bannerFull}
  <div class="tabbar tb-slide" bind:this={tabbarEl} style="--tb-ind-l:{tbIndL}px;--tb-ind-w:{tbIndW}px">
    <button class="tab-btn" data-section="servers" class:active={tab === 'servers'} onclick={() => tab = 'servers'}>
      <i class="ti ti-server-2"></i> {t('pack.tab.servers')} <span class="num">{packServers.length}</span>
    </button>
    {#if hasScreenshots}
      <button class="tab-btn" data-section="gallery" class:active={tab === 'gallery'} onclick={() => tab = 'gallery'}>
        <i class="ti ti-photo"></i> {t('pack.tab.gallery')}
      </button>
    {/if}
    {#if canManage}
      <button class="tab-btn" data-section="content" class:active={tab === 'content'} onclick={() => tab = 'content'}>
        <i class="ti ti-puzzle"></i> {t('pack.tab.content')}
      </button>
      <button class="tab-btn" data-section="files" class:active={tab === 'files'} onclick={() => tab = 'files'}>
        <i class="ti ti-files"></i> {t('pack.tab.files')}
      </button>
    {/if}
    {#if !isAutoPack}
      <button class="tab-btn" data-section="basic" class:active={tab === 'basic'} onclick={() => tab = 'basic'}>
        <i class="ti ti-info-circle"></i> {t('pack.tab.basic')}
      </button>
    {/if}
    {#if kind === 'shaurma'}
      <button class="tab-btn" data-section="wiki" class:active={tab === 'wiki'} onclick={() => tab = 'wiki'}>
        <i class="ti ti-book-2"></i> {t('pack.tab.wiki')}
      </button>
    {/if}
    <button class="tab-btn" data-section="perf" class:active={tab === 'perf'} onclick={() => tab = 'perf'}>
      <i class="ti ti-cpu"></i> {t('pack.tab.perf')}
    </button>
    <button class="tab-btn" data-section="game" class:active={tab === 'game'} onclick={() => tab = 'game'}>
      <i class="ti ti-device-gamepad-2"></i> {t('pack.tab.game')}
    </button>
    <button class="tab-btn" data-section="console" class:active={tab === 'console'} onclick={() => tab = 'console'}>
      <i class="ti ti-terminal-2"></i> {t('pack.tab.console')}
    </button>
  </div>
{/if}
{/if}
<!-- ^ закриває зовнішній {#if !installed} ... {:else} ... {/if} з рядка 984:
     весь вміст встановленої збірки (running-widget/launch-progress/
     download-progress/migration/таббар вище) належав гілці {:else}, а
     я вставив ще один {#if !bannerFull} НАВКОЛО таббару — тому додався
     цей другий {/if}, якого не було в оригіналі (баг компіляції
     «Block was left open», що я допустив у попередній правці). -->


{#if installed}
<div class="tab-panel-host" bind:this={panelHost}>
{#key tab}
<div class="tab-panel-anim">
{#if !cfgLoaded}
  <div class="card"><div style="padding:24px;text-align:center;color:var(--text-mute);font-size:12px">…</div></div>

{:else if tab === 'servers'}
  <!-- disabled={isThisPackRunning || isLaunching}: та сама логіка, що й у
       PackOfficialServerWidget вище — блокуємо лише якщо САМЕ ЦЯ збірка
       вже йде або запускається; паралельний запуск ІНШИХ збірок на тому
       самому акаунті не повинен блокувати "Приєднатись" тут. -->
  <PackServersSection packId={pack.id} packName={pack.name} disabled={isThisPackRunning || isLaunching} {allBuilds} {packPicker} onServersChanged={refreshGameTab} activeAccountId={activeAccount?.id ?? ''} stickyTop={Math.max(40, tbH)} onPlay={onLaunchOnServer} />

{:else if tab === 'gallery'}
  <ScreenshotsPanel packId={pack.id} packName={pack.name} />

{:else if tab === 'basic'}
  <PackBasicSection pack={pack} kind={kind} onUpdated={onPackUpdated} onPackRenamed={onPackRenamed} />

{:else if tab === 'wiki'}
  <PackWikiSection packId={pack.id} packName={pack.name} />

{:else if tab === 'perf'}
  <PackPerfSection
    {cfg}
    {settings}
    onUpdate={update}
    packId={pack.id}
    packName={pack.name}
    repairing={repairing}
    reinstalling={reinstalling}
    onRepair={repairPack}
    onReinstall={reinstallPack}
  />

{:else if tab === 'game'}
  <PackGameSection {cfg} {settings} accounts={accounts} activeAccount={activeAccount} worlds={worlds} packServers={packServers} packId={pack.id} mcVersion={pack.mc_version} onRefreshWorlds={refreshGameTab} onUpdate={update} />

{:else if tab === 'files'}
  {#if canManage}
    <PackFilesPanel packId={pack.id} packName={pack.name} stickyTop={Math.max(40, tbH)} />
  {/if}

{:else if tab === 'content'}
  <PackContentSection
    packId={pack.id}
    packName={pack.name}
    mcVersion={pack.mc_version}
    loader={pack.loader_type}
    canMods={canMods}
    onSearchContent={onSearchContent}
    onOpenModPage={onOpenModPage}
    dupFocusSignal={modsFocusSignal}
    {allBuilds}
    {packPicker}	    // Закріплений тулбар сторінки лишається над контентом при скролі —
	    // тулбар менеджера модів/контенту прилипає одразу ПІД ним (sticky
	    // top = висота тулбара, БЕЗ зайвого проміжку). Раніше тут було
	    // tbH+16: між двома закріпленими смугами лишалась щілина 16px,
	    // крізь яку просвічував контент, що скролиться — виглядало як
	    // «виступ» між панеллю збірки (Грати та інші кнопки) і панеллю
	    // пошуку. Самі смуги мають власний зградієнтний blur-фон, тож
	    // стик без проміжку виглядає суцільним.
	    stickyTop={Math.max(40, tbH)}
    // Збірка запущена (на поточному чи будь-якому іншому акаунті) —
    // змінювати моди/ресурспаки/шейдери/версію заборонено: фронтенд
    // блокує дії модалкою ДО виклику бекенду (не чекаючи OS-помилки
    // "used by another process").
    running={isThisPackRunning || (runningAccounts?.length ?? 0) > 0}
  />

{:else if tab === 'console'}
  <!-- Консоль у вкладці займає висоту, що лишається під закріпленим
       тулбаром сторінки (--ct-top), і скролиться ВСЕРЕДИНІ себе (.gc-term),
       як окреме вікно: пошук/фільтри/дії завжди на місці, лог не тягне за
       собою всю сторінку. Ідентичність збірки і кнопки Запустити/Зупинити
       у вкладці НЕ дублюються — вони є в тулбарі сторінки збірки. -->
  <div class="console-tab-wrap" style={`--ct-top:${Math.max(40, tbH)}px`}>
    <!-- packID: вкладка консолі ЖОРСТКО прив'язана до цієї збірки — показує
         її буфер/стан і не перемикається на чужі запуски (консоль вікна
         окремо слідує за збіркою, що запускається). -->
    <GameConsole packID={pack.id} packIconUrl={pack.iconUrl} packColor={pack.color} packIcon={pack.icon} />
  </div>
{/if}
</div>
{/key}
</div>
{/if}

{#if shareOpen}
  <ShareModal kind="build" refId={pack.id} name={pack.name} onClose={() => shareOpen = false} />
{/if}
{#if exportOpen}
  <ExportPackModal packId={pack.id} packName={pack.name} onClose={() => exportOpen = false} />
{/if}

<!-- «Оновити» інтернет-збірки (Modrinth/CurseForge): список версій з
     джерела. Поточна версія позначена; оновлення — UpdatePackFromSource
     (diff-імпорт, прогрес у sync:progress → картка показує живі %). -->
{#if updateDialog}
  {@const dlg = updateDialog}
  <UiModal
    open={true}
    title={t('pack.updateVersionTitle')}
    subtitle={t('pack.updateVersionSub')}
    icon="ti-refresh"
    color="var(--orange)"
    width="540px"
    locked={updatingVersion}
    onclose={() => { if (!updatingVersion) updateDialog = null }}
  >
    {#if updateDialog.loading}
      <div class="upd-state"><i class="ti ti-loader spin"></i> {t('browser.compatChecking')}</div>
    {:else if updateDialog.error}
      <div class="upd-state upd-err"><i class="ti ti-alert-triangle"></i> {updateDialog.error}</div>
    {:else if updateDialog.versions.length === 0}
      <div class="upd-state"><i class="ti ti-mood-sad"></i> {t('browser.update.noVersions')}</div>
    {:else}
      <div class="upd-versions">
        {#each updateDialog.versions as v (v.id)}
          <button
            type="button"
            class="upd-v-row"
            class:sel={updateDialog.selectedId === v.id}
            class:cur={v.id === buildView?.build.sourceVersionId}
            onclick={() => { if (updateDialog) updateDialog.selectedId = v.id }}
            disabled={updatingVersion}
          >
            <span class="upd-v-main">
              <b>{v.name || v.versionNumber}</b>
              {#if v.versionType === 'release'}<span class="upd-chip release">{t('browser.project.recommended')}</span>{/if}
              {#if v.id === buildView?.build.sourceVersionId}<span class="upd-chip cur">{t('pack.currentVersion')}</span>{/if}
            </span>
            <span class="upd-v-meta">
              <span>{(v.gameVersions ?? []).join(', ')}</span>
              {#if (v.loaders ?? []).length}<span>{(v.loaders ?? [])[0]}</span>{/if}
              <span>{new Date(v.datePublished).toLocaleDateString()}</span>
            </span>
          </button>
        {/each}
      </div>
    {/if}
    {#snippet footer()}
      <button class="btn btn-ghost" onclick={() => updateDialog = null} disabled={updatingVersion}>{t('browser.alreadyInstalled.cancel')}</button>
      <button class="btn btn-primary" onclick={doUpdatePack} disabled={!dlg.selectedId || updatingVersion}>
        {#if updatingVersion}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-refresh"></i>{/if}
        {t('pack.update')}
      </button>
    {/snippet}
  </UiModal>
{/if}

<!-- Модалка «збірка запущена — зміни заборонені» (оновлення версії) -->
{#if runningLocked}
  <UiModal
    open={true}
    size="sm"
    title={t('content.lockedWhileRunning')}
    subtitle={t('content.lockedWhileRunningHint')}
    icon="ti-lock"
    color="var(--orange)"
    onclose={() => runningLocked = false}
  >
    <p class="ct-modal-hint">{t('content.lockedWhileRunningBody')}</p>
    {#snippet footer()}
      <button class="btn btn-primary" onclick={() => runningLocked = false}>{t('content.lockedOk')}</button>
    {/snippet}
  </UiModal>
{/if}

</PackPageDesign>

<style>
  /* ── Компактна ідентичність у закріпленому тулбарі сторінки ──
     Прихована у верху сторінки (банер і так показує іконку+назву),
     плавно з'являється при скролі — клас .show ставить scroll-ефект
     (display:none у базі, щоб плашка не займала місця в flex-рядку
     тулбара, коли банер ще видно). */
  .pt-id {
    display: none; align-items: center; gap: 8px;
    margin-left: 4px; padding: 3px 12px 3px 4px;
    background: color-mix(in srgb, var(--bg-card) 82%, transparent);
    border: 1px solid var(--border); border-radius: 12px;
    min-width: 0; max-width: 280px;
    animation: pt-id-in .28s cubic-bezier(.22,1,.36,1);
  }
  .pt-id.show { display: flex; }
  @keyframes pt-id-in {
    from { opacity: 0; transform: translateX(-8px); }
    to   { opacity: 1; transform: none; }
  }
  .pt-id-ic {
    width: 26px; height: 26px; border-radius: 8px; overflow: hidden;
    flex-shrink: 0; display: flex; align-items: center; justify-content: center;
    background: var(--bg-input);
  }
  .pt-id-ic img { width: 100%; height: 100%; object-fit: cover; }
  .pt-id-ic.ti-fallback i { font-size: 14px; color: var(--pcol, var(--orange)); }
  .pt-id-meta { min-width: 0; display: flex; flex-direction: column; gap: 1px; }
  .pt-id-name {
    font-size: 12.5px; font-weight: 800; color: var(--text);
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .pt-id-sub {
    display: flex; align-items: center; gap: 5px;
    font-size: var(--fs-min); color: var(--text-mute); font-family: var(--font-mono);
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .pt-id-sub i { font-size: 11px; }

  /* Голови акаунтів, на яких збірка ЗАРАЗ запущена — квадратні аватарки
     із закругленням у самому тулбарі (поруч із назвою), а не окремий
     віджет під банером. 24px — компактно, але впізнавано. */
  .pt-heads {
    display: inline-flex; align-items: center; gap: 3px;
    margin-left: 2px; flex-shrink: 0;
  }
  .pt-heads .pt-head {
    width: 24px; height: 24px; border-radius: 7px;
    object-fit: cover; background: var(--bg-input);
    border: 1.5px solid color-mix(in srgb, var(--bg-window) 85%, transparent);
    box-shadow: 0 0 0 1px var(--border-hi);
  }

  /* Вигляд ковзного таббара (.tabbar.tb-slide) — спільний, у style.css. */

  /* ── Повноширинний режим (packBannerFull) — .pack-hero-full ──────────
     Один суцільний геро-блок: фон-арт збірки на весь блок, а тулбар
     кнопок / назва+бейджі / таббар вкладок — звичайні елементи в
     normal flow ЗВЕРХУ нього (z-index). Ніякого sticky, ніяких
     negative-margin/order трюків між окремими шматками — саме це
     раніше раз у раз ламало верстку (кнопки спливали в кут, банер
     обрізався, елементи лягали під банер). Структура точно як у
     референсі — розділ «Контент»: суцільна картинка на весь геро,
     кнопки й таббар просто лежать зверху. */
  :global(.pack-hero-full) {
    position: relative;
    margin: calc(var(--tb-h, 56px) * -1) -34px 18px;
    padding: calc(var(--tb-h, 56px) + 8px) 34px 22px;
    min-height: 320px;
    background-size: cover;
    background-position: center;
    overflow: hidden;
    display: flex;
    flex-direction: column;
    gap: 26px;
  }
  :global(.pack-hero-full .phf-overlay) {
    position: absolute; inset: 0; z-index: 0;
    pointer-events: none;
    background: linear-gradient(180deg,
      rgba(0,0,0,.55) 0%,
      rgba(0,0,0,.15) 32%,
      rgba(0,0,0,.35) 70%,
      rgba(0,0,0,.75) 100%);
  }
  :global(.pack-hero-full .phf-shine) {
    position: absolute; inset: 0; z-index: 0;
    pointer-events: none;
    background: linear-gradient(105deg, transparent 40%, rgba(255,255,255,.12) 50%, transparent 60%);
    background-size: 250% 100%;
    animation: pp-shine 1.1s cubic-bezier(.25,1,.4,1) .35s both;
  }
  /* Тулбар кнопок і таббар вкладок — прямі діти геро-блоку, у normal
     flow, ПОВЕРХ фону (z-index:1). Ніякого власного background —
     кнопки/пігулки читаються завдяки затемнюючому phf-overlay вище.
     Явно перекриваємо .pack-toolbar (style.css) — там базовий стиль
     робить рядок кнопок sticky/з фоном для звичайного режиму, тут це
     не потрібно: геро-блок сам не sticky і не має власного фону рядка. */
  /* Тулбар кнопок — закріплений (sticky) сусід геро-блоку. У спокої прозорий
     (читається завдяки phf-overlay), після скролу — «скло» на всю ширину. */
  :global(.pack-toolbar.pt-full) {
    position: sticky;
    top: 0;
    z-index: 30;
    margin: 0 -34px;
    padding: 12px 34px;
    flex-wrap: wrap;
    row-gap: 8px;
    background: transparent;
    backdrop-filter: none;
    border-bottom: 1px solid transparent;
    transition: background-color .22s ease, border-color .22s ease, box-shadow .22s ease;
  }
  :global(.pack-toolbar.pt-full .pt-spacer) { flex: 1 1 0; min-width: 0; }
  :global(.pack-toolbar.pt-full.pt-stuck) {
    background: color-mix(in srgb, var(--bg-app) 82%, transparent);
    backdrop-filter: blur(14px);
    border-bottom-color: var(--border);
    box-shadow: 0 6px 20px rgba(0,0,0,.22);
  }
  /* Нижній рядок: назва ліворуч, бейджі праворуч (в одну лінію, переносяться, якщо тісно). */
  :global(.pack-hero-full > .phf-bottom) {
    position: relative; z-index: 1; margin-top: auto;
    display: flex; align-items: flex-end; justify-content: space-between;
    gap: 12px 16px; flex-wrap: wrap; min-width: 0;
  }
  :global(.pack-hero-full .phf-bottom > .ib-title) {
    display: flex; align-items: center; gap: 14px; min-width: 0; color: #fff;
  }
  :global(.pack-hero-full .phf-side) {
    display: flex; align-items: center; justify-content: flex-end;
    flex-wrap: wrap; gap: 8px; margin-left: auto; min-width: 0;
  }
  :global(.pack-hero-full .phf-side > *) { position: static; margin: 0; white-space: nowrap; }
  :global(.pack-hero-full > .tabbar.tb-full) {
    position: relative;
    z-index: 1;
    margin: 0;
    width: 100%;
    background: color-mix(in srgb, var(--bg-window) 55%, transparent);
    backdrop-filter: blur(10px);
    border-color: rgba(255,255,255,.14);
    padding: 5px;
    gap: 6px;
  }
  /* Вкладки рівномірно ділять усю ширину бару, а підпис у кожній — по центру
     (раніше вони тулилися ліворуч, праворуч лишався порожній хвіст). У вузькому
     вікні flex-shrink:0 + nowrap → бар гортається, як і раніше. */
  :global(.pack-hero-full > .tabbar.tb-full .tab-btn) {
    flex: 1 0 auto;
    justify-content: center;
    padding: 10px 18px;
  }

  /* М'який «відблиск», що пробігає по банеру збірки при відкритті сторінки. */
  :global(.inst-banner .ib-shine) {
    position: absolute; inset: 0;
    pointer-events: none;
    background: linear-gradient(105deg, transparent 40%, rgba(255,255,255,.14) 50%, transparent 60%);
    background-size: 250% 100%;
    animation: pp-shine 1.1s cubic-bezier(.25,1,.4,1) .35s both;
    z-index: 1;
  }
  @keyframes pp-shine {
    from { background-position: 130% 0; opacity: 0; }
    40% { opacity: 1; }
    to { background-position: -30% 0; opacity: 0; }
  }

  /* ── Діалог «Оновити»: список версій з джерела ── */
  .upd-state {
    padding: 26px 0; display: flex; align-items: center; justify-content: center;
    gap: 10px; font-size: 12.5px; color: var(--text-mute);
  }
  .upd-state i { font-size: 18px; }
  .upd-state.upd-err { color: var(--red); }
  .upd-versions {
    display: flex; flex-direction: column; gap: 7px;
    max-height: min(340px, 55vh); overflow-y: auto;

  /* Вкладка консолі: картка на всю висоту, що лишається під закріпленим
     тулбаром сторінки (висота залежить від вікна, а не від кількості
     рядків лога — термінал скролиться всередині, а пошук/дії завжди
     видимі). Той самий підхід, що окреме вікно консолі. */
  .console-tab-wrap {
    display: flex; flex-direction: column;
    margin-bottom: 4px;
  }
  .console-tab-wrap :global(.gc) {
    height: calc(100vh - var(--ct-top, 40px) - 24px);
    min-height: 300px;
    max-height: calc(100vh - var(--ct-top, 40px) - 24px);
  }
  }
  .upd-v-row {
    display: flex; flex-direction: column; gap: 6px; text-align: left;
    padding: 11px 13px; border-radius: 12px;
    background: var(--bg-input); border: 1.5px solid var(--border);
    cursor: pointer; color: var(--text);
    transition: border-color .15s, background .15s, transform .18s cubic-bezier(.34,1.56,.64,1);
  }
  .upd-v-row:hover { border-color: var(--border-hi); transform: translateY(-1px); }
  .upd-v-row.sel {
    border-color: var(--orange);
    background: rgba(var(--orange-rgb), .07);
  }
  .upd-v-row.cur { border-left: 3px solid var(--orange); }
  .upd-v-row:disabled { opacity: .7; cursor: default; transform: none; }
  .upd-v-main { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; font-size: 13px; }
  .upd-v-meta {
    display: flex; gap: 12px; flex-wrap: wrap;
    font: 500 11px var(--font-mono); color: var(--text-mute);
  }
  .upd-chip { font: 700 var(--fs-min) var(--font-ui); padding: 2px 8px; border-radius: 999px; }
  .upd-chip.release {
    background: rgba(16,185,129,.13); border: 1px solid rgba(16,185,129,.35); color: var(--green);
  }
  .upd-chip.cur {
    background: rgba(var(--orange-rgb), .14); border: 1px solid rgba(var(--orange-rgb), .4); color: var(--orange);
  }
  /* Низьке вікно ноутбука (спільний брейкпоінт, див. кінець style.css):
     нижчий геро-блок, щоб вкладки й вміст починались вище. */
  @media (max-height: 760px) {
    :global(.pack-hero-full) { min-height: 236px; gap: 16px; padding-bottom: 16px; }
  }
</style>
