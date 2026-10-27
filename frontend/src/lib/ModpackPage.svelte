<script lang="ts">
  import ShareToFriend from './friends/ShareToFriend.svelte'
  import UiModal from './UiModal.svelte'
  // ── Сторінка проєкту (модпак / мод / ресурспак / шейдер / датапак) ────
  // Той самий макет, що сторінка Шаурма-збірки: банер з іконкою+назвою,
  // вкладки (Опис / Галерея / Відео / Зміни / Версії), список версій.
  // Клік по картці в браузері завжди відкриває ЦЮ сторінку; встановлення —
  // кнопкою тут, з прогресом і позначкою «вже встановлено» (як Modrinth
  // app):
  //   - модпак → «Завантажити» = створення кастомної «інтернет-збірки»;
  //   - мод/ресурспак/шейдер → «Встановити» у збірку-фільтр (або вибір
  //     збірки, якщо фільтра немає);
  //   - датапак → «Встановити» у конкретний світ збірки.
  //
  // Вибір версії — як у Modrinth app (DownloadProject.vue):
  //   - коли є збірка-контекст — версія ПІДБИРАЄТЬСЯ АВТОМАТИЧНО під її
  //     версію Minecraft і лоадер (GetBrowserRecommendedVersion), користувач
  //     може лише перемкнути на іншу сумісну (список відфільтровано);
  //   - без збірки — два селекти (Версія Minecraft / Платформа) звужують
  //     список, як у веб-апки, і рекомендована обирається сама.
  //
  // Галерея: клік по зображенню відкриває повноекранний лайтбокс (як у
  // Modrinth app): зум колесом миші, панорама перетягуванням, prev/next,
  // навігація клавіатурою (←/→/Esc/+/-), підпис і опис під зображенням.
  import { t, getLanguage } from './i18n.svelte'
  import { App, on, withCallTimeout } from './wails'
  import { toast, confirmDialog } from './toast.svelte'
  import { packAsset } from './packAssets.svelte'
  import type { BrowserProjectDetail, BrowserVersionView, BrowserCompatiblePack, BrowserInstalledItem, BrowserInstalledInPack, BrowserProgress, BrowserTranslation, BrowserReview, BrowserEntry } from './types'
  import PackPicker from './PackPicker.svelte'
  import ServerTargetList from './servers/ServerTargetList.svelte'
  import { kindForTab, serversForTab, serverVisual, coreName, effectiveServerTab, type BrowserServer } from './servers/serverTargets'
  import type { PackRow, PackFolderRow, PackPickerItem } from './PackPicker.svelte'
  import fabricIcon from '../assets/images/fabric_icon.png'
  import forgeIcon from '../assets/images/forge_icon.png'
  import neoforgeIcon from '../assets/images/neoforge_icon.png'
  import quiltIcon from '../assets/images/quilt_icon.png'
  import vanillaIcon from '../assets/images/vanilla_icon.png'
  import SelectField from './SelectField.svelte'
  import { marked } from 'marked'
  import { sanitizeHtml, normalizeYouTubeEmbed } from './sanitizeHtml'
  import { smartImage } from './smartImage'

  let {
    source,
    projectId,
    onBack,
    // Збірка-фільтр браузера: якщо задана — встановлення модів/ресурспаків/
    // шейдерів іде одразу в неї (без діалогу), а версія підбирається під її
    // mcVersion/loader. Може бути відсутня.
    packContext = null,
    // Усі кастомні збірки (для діалогу вибору, коли фільтра немає).
    packs = [],
    // Дані списку збірок з ПАПКАМИ (PackPicker) — діалог вибору збірки
    // рендериться ним: збірки з папок видно, папки розкриваються як у
    // sidebar (App.svelte будує через customLayoutItems).
    packPicker = null,
    // Перехід на сторінку вже встановленої збірки (кнопка «Перейти до
    // збірки» в діалозі «модпак уже додано»). Опційно — якщо не задано,
    // ця кнопка в діалозі не показується.
    onOpenPack = null,
    // Перехід на сторінку автора (клік по автору в сайдбарі).
    onAuthor = null,
    // Зміна/зняття фільтра збірки прямо зі сторінки контенту (клік по
    // бейджу «Х» знімає фільтр, «Змінити» відкриває вибір іншої збірки).
    // Опційно — якщо не задано, бейдж лишається лише інформаційним (як
    // було раніше).
    onChangePackContext = null,
    // Власні сервери — ще одна ЦІЛЬ встановлення поруч зі збірками. Без цього
    // на сторінці проєкту не можна було вибрати сервер взагалі (лише збірки).
    servers = [],
    // Користувач обрав сервер ціллю зі сторінки проєкту. kind — 'mods' чи
    // 'plugins' (залежить від типу проєкту). App резолвить loader ядра.
    onPickServerContext = null,
    // Повноширинний банер (Налаштування → Вигляд → «Банер збірки на всю
    // ширину», packBannerFull). За замовчуванням false — звичайний банер
    // у колонці контенту, як було завжди.
    bannerFull = false,
    // Показати кнопку «Поділитися з другом» (Shaurma-редакція) і перехід у «Друзі».
    canShare = false,
    onGoFriends,
  } = $props<{
    source: string
    projectId: string
    onBack: () => void
    // isServer=true — packContext.id насправді serverId (сторінка контенту
    // відкрита з менеджера модів/плагінів СЕРВЕРА, не збірки): встановлення
    // йде через App.InstallServerBrowserVersion замість
    // InstallBrowserContentVersion (див. installIntoPack нижче). Ім'я поля
    // лишили "packContext", а не завели окремий serverContext, — 95% цієї
    // сторінки (опис/галерея/автор/версії/сумісність за mc+loader) взагалі
    // не залежить від того, пак це чи сервер, тож дублювати всю логіку
    // заради одного прапорця сенсу не мало; розгалуження лише там, де
    // виклик бекенду реально різний (встановлення), і там, де для серверів
    // немає сенсу (вибір іншої "збірки", розділ світів для датапаків).
    packContext?: { id: string; name: string; mcVersion: string; loader: string; isServer?: boolean } | null
    packs?: { id: string; name: string; mcVersion: string; loader: string; iconUrl?: string; color?: string }[]
    packPicker?: {
      items: PackPickerItem[]
      rowById: Map<string, PackRow>
      folderById: Map<string, PackFolderRow>
    } | null
    onOpenPack?: ((packId: string) => void) | null
    onAuthor?: ((authorId: string, authorName: string) => void) | null
    onChangePackContext?: ((ctx: { id: string; name: string; mcVersion: string; loader: string } | null) => void) | null
    servers?: BrowserServer[]
    onPickServerContext?: ((id: string, kind: 'mods' | 'plugins') => void | Promise<void>) | null
    bannerFull?: boolean
    canShare?: boolean
    onGoFriends?: () => void
  }>()

  let detail = $state<BrowserProjectDetail | null>(null)
  let versions = $state<BrowserVersionView[]>([])
  let loading = $state(true)
  let loadingVersions = $state(false)
  let loadError = $state('')
  let tab = $state<'description' | 'gallery' | 'videos' | 'changelog' | 'versions'>('description')
  let selectedVersionId = $state('')
  let installing = $state(false)
  // Прогрес встановлення (browser:progress): percent 0-100.
  let installProgress = $state(0)
  // ytId відео, яке юзер запустив у вкладці «Відео» (iframe монтується
  // лише після кліку по постеру — решта карток показують прев'ю, бо
  // YouTube-embed у WebView2 масово віддає «Помилку 153»).
  let playingVideo = $state('')
  // Діалог «модпак уже додано в Моїх збірках»: показується перед
  // CreateBrowserModpack, якщо CheckBrowserModpackInstalled знайшов
  // існуючу картку з того самого проєкту — замість мовчазного дубля.
  let alreadyInstalledDialog = $state<{ packId: string; packName: string; pendingVersion: BrowserVersionView } | null>(null)
  // Аналіз «вже встановлено» у збірці-фільтрі (по sha1): знайдений проєкт.
  let installedItem = $state<BrowserInstalledItem | null>(null)
  let installedChecked = $state(false)
  // Компактний блок «Також встановлено у: ...» — усі збірки користувача,
  // де вже стоїть цей проєкт (незалежно від того, чи є packContext).
  // Вантажиться окремо й ліниво: не блокує основне завантаження сторінки,
  // бо перебирає ВСІ встановлені збірки (може бути трохи повільніше за
  // одиничний installedItem-чек однієї збірки).
  let installedInPacks = $state<BrowserInstalledInPack[]>([])
  let installedInPacksLoading = $state(false)
  let installedInPacksExpanded = $state(false)

  // ── Закладка («зірочка») на сторінці проєкту ──────────────────────────
  // Раніше кнопка збереження існувала ЛИШЕ на картках у ContentBrowser
  // (грід результатів пошуку/«Збережено») — на самій сторінці проєкту
  // (сюди веде і клік по картці, і клік по моду з менеджера модів
  // ВСЕРЕДИНІ сторінки збірки, openModPageFromPack у App.svelte) зірочки
  // не було взагалі: «зберегти» можна було лише повернувшись у грід і
  // знайшовши ту саму картку ще раз. Той самий бекенд-метод ToggleBookmark
  // (backend/browser_bookmarks_api.go), той самий візуальний стан
  // (жовтий #f5c542, коли закладено), просто інший виклик — тут не
  // потрібен bmKey-Set на купу карток, досить одного bool на сторінку.
  let bookmarked = $state(false)
  let bookmarkBusy = $state(false)

  // OPTIMISTIC UPDATE (той самий підхід, що ContentBrowser.svelte): раніше
  // `bookmarked` мінявся лише ПІСЛЯ await App.ToggleBookmark — на
  // повільнішому диску клік по зірочці міг на мить (а при затримці —
  // помітно) лишати її «невидимою»/незміненою, виглядаючи так, ніби
  // закладка не зберіглась. Тепер перемикаємо одразу на клік і
  // відкочуємо назад лише якщо бекенд справді повернув помилку.
  async function toggleBookmark() {
    if (!detail || bookmarkBusy) return
    const wasBookmarked = bookmarked
    bookmarked = !wasBookmarked
    bookmarkBusy = true
    try {
      const entry: BrowserEntry = {
        id: detail.projectId, name: detail.name, type: detail.projectType,
        version: '', loader: '', mcVersion: '', description: detail.description,
        downloads: detail.downloads, followers: detail.followers, likes: 0,
        iconUrl: detail.iconUrl, source: detail.source, projectId: detail.projectId,
        author: detail.author ?? '', categories: detail.categories ?? [], loaders: detail.loaders ?? [],
        dateCreated: detail.published, dateModified: detail.updated,
      }
      bookmarked = await App.ToggleBookmark(entry)
    } catch (e: any) {
      // Бекенд відмовив — повертаємо зірочку до стану ДО кліку замість
      // того, щоб лишати оптимістичну (можливо неправильну) позначку.
      bookmarked = wasBookmarked
      toast(String(e?.message ?? e), 'error')
    } finally {
      bookmarkBusy = false
    }
  }

  // ── Додати проєкт у колекцію ───────────────────────────────────────────
  // На відміну від картки в ContentBrowser (там версія не вибрана — йде
  // versionId=''), тут ВЖЕ є обрана версія (selectedVersionId, той самий
  // вибір, яким керується кнопка «Встановити») — колекція закріплює саме
  // її (AddToCollection: "перевірена пачка ставить саме ту версію, яку
  // склали, а не найновішу на момент майбутнього встановлення").
  let collectionsMenuOpen = $state(false)
  let collectionsForMenu = $state<{ id: string; name: string; itemCount: number }[]>([])
  let collectionsMenuLoading = $state(false)
  let collectionsMenuLoadedOnce = false

  async function openCollectionsMenu() {
    collectionsMenuOpen = !collectionsMenuOpen
    if (!collectionsMenuOpen || collectionsMenuLoadedOnce) return
    collectionsMenuLoadedOnce = true
    collectionsMenuLoading = true
    try {
      collectionsForMenu = (await App.GetCollections()) ?? []
    } catch {
      collectionsForMenu = []
    } finally {
      collectionsMenuLoading = false
    }
  }

  async function addToCollection(collectionId: string) {
    if (!detail) return
    collectionsMenuOpen = false
    const v = versions.find(x => x.id === selectedVersionId)
    const entry: BrowserEntry = {
      id: detail.projectId, name: detail.name, type: detail.projectType,
      version: '', loader: '', mcVersion: '', description: detail.description,
      downloads: detail.downloads, followers: detail.followers, likes: 0,
      iconUrl: detail.iconUrl, source: detail.source, projectId: detail.projectId,
      author: detail.author ?? '', categories: detail.categories ?? [], loaders: detail.loaders ?? [],
      dateCreated: detail.published, dateModified: detail.updated,
    }
    try {
      await App.AddToCollection(collectionId, entry, selectedVersionId, v?.versionNumber ?? '')
      toast(t('browser.saved.addedToCollection'), 'success')
    } catch (e: any) {
      toast(String(e?.message ?? e), 'error')
    }
  }

  async function createCollectionAndAdd() {
    collectionsMenuOpen = false
    try {
      const c = await App.CreateCollection(t('browser.saved.newCollection'), '', '', '')
      await addToCollection(c.id)
    } catch (e: any) {
      toast(String(e?.message ?? e), 'error')
    }
  }

  $effect(() => {
    if (!collectionsMenuOpen) return
    const close = () => { collectionsMenuOpen = false }
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') collectionsMenuOpen = false }
    window.addEventListener('click', close)
    window.addEventListener('keydown', onKey)
    return () => {
      window.removeEventListener('click', close)
      window.removeEventListener('keydown', onKey)
    }
  })

  // ── ШІ браузера: переклад сторінки + AI-відгук про проєкт ────────────
  // Переклад: працює ЛИШЕ по кнопці «Перекласти» (не автоматично),
  // кешується на бекенді (повторне відкриття — без запиту до Groq).
  // Користувач може перемикатися між перекладом і оригіналом.
  let translation = $state<BrowserTranslation | null>(null)
  let translationLoading = $state(false)
  // showTranslation — показувати переклад (true) чи оригінал (false).
  let showTranslation = $state(false)
  // AI-відгук: генерується ОДИН раз (перегенерація заборонена — бекенд
  // кешує; кнопка лишається вимкненою після успіху). Повторний запуск
  // можливий лише після помилки (reviewError=true).
  let review = $state<BrowserReview | null>(null)
  let reviewLoading = $state(false)
  let reviewError = $state(false)

  // ── Селектори версії (як DownloadProject.vue у Modrinth app) ─────────
  // Без збірки-контексту: «Версія Minecraft» + «Платформа» звужують список
  // версій; рекомендована (release → beta → alpha) вибирається автоматично
  // при зміні фільтрів.
  let mcFilter = $state('')
  let loaderFilter = $state('')
  // Коли збірка задана — версія підібрана під неї; прапорець для UI
  // («Автоматично підібрана версія для…»).
  let autoMatched = $state(false)
  // Збірка-фільтр НЕ підтримує цей контент (бекенд не знайшов жодної
  // версії під (mc, loader) збірки). Тоді замість порожнечі показуємо
  // «не підтримується» у віджеті встановлення і «немає сумісних версій»
  // у вкладці Версії — раніше список підмінявся УСІМА версіями, і юзер
  // бачив порожній список + незрозумілу поведінку кнопки.
  let packUnsupported = $state(false)
  // «Показати всі версії» (як checkbox у DownloadProject.vue Modrinth app):
  // за замовчуванням у селекторі версії Minecraft показуються лише
  // стабільні (release) версії; сніпшоти/бети ховаються, доки не
  // відмічено цей прапорець. Показуємо його лише коли в проєкту є І
  // стабільні, І нестабільні версії гри (showVersionsCheckbox).
  let showAllVersions = $state(false)

  // ── Лайтбокс галереї (повноекранний перегляд, як у Modrinth app) ─────
  let lightboxIndex = $state<number | null>(null)
  let lightboxZoom = $state(1)
  let lightboxPan = $state({ x: 0, y: 0 })
  let lightboxDragging = $state(false)
  let lightboxDragStart = $state({ x: 0, y: 0, px: 0, py: 0 })
  // Лічильник поколінь load(): старий in-flight запит після перемикання
  // проєкту перестає писати в стан (див. load()).
  let loadSeq = $state(0)

  // topbarScrolled — sticky-панель зверху отримує щільніший фон/тінь, коли
  // сторінку проскролили (панель прикріплена, але не заважає читати).
  // Слухаємо скрол найближчого скрол-контейнера (сторінка всередині .content).
  let topbarScrolled = $state(false)
  // heroVisible — коли іконка+назва проєкту (hero) ще у зоні видимості,
  // sticky-панель НЕ дублює їх (лише кнопка назад і зовнішнє посилання).
  // Іконка/назва з'являються в панелі лише після того, як hero виїхав
  // за верхній край — так закріплений бар не повторює те, що й так видно.
  // Стартове значення true: перший кадр до першого onScr() — банер
  // справді ще на екрані (сторінка щойно відкрита, скрол = 0).
  let heroVisible = $state(true)

  // Діалог вибору світів для датапака: датапаки застосовуються до
  // КОНКРЕТНИХ світів збірки (saves/<world>/datapacks) — МУЛЬТИВИБІР:
  // один датапак можна встановити одразу в кілька світів (якщо світів
  // немає — спершу потрібно запустити гру і створити світ).
  // Відкривається зі сторінки проєкту при встановленні датапака.
  let worldDialog = $state<{ packId: string; packName: string; worlds: string[]; loading: boolean; selectedWorlds: string[] } | null>(null)
  // worldGlobal — встановити датапак у ЗАГАЛЬНУ теку datapacks збірки
  // (не лише в окремі світи). Можна разом зі світами.
  let worldGlobal = $state(false)

  // Діалог вибору збірки (коли фільтра немає). Тепер несе і список версій:
  // користувач обирає ВЕРСІЮ контенту, а список збірок фільтрується під
  // саме цю версію (запит GetBrowserCompatiblePacks з versionID) — так
  // «розділ версії» живе всередині діалогу встановлення, а не на сторінці.
  let packDialog = $state<{ packs: BrowserCompatiblePack[]; versions: BrowserVersionView[]; loading: boolean; selectedVersionId: string } | null>(null)
  // Пошук по збірках у діалозі вибору (як StyledInput у ContentInstallModal
  // Modrinth app) — корисно, коли збірок багато.
  let packDialogFilter = $state('')
  // Вкладка цілі в діалозі «Встановити»: збірки чи сервери.
  let dialogTab = $state<'packs' | 'servers'>('packs')
  const filteredPackDialogPacks = $derived.by(() => {
    if (!packDialog) return []
    const q = packDialogFilter.trim().toLowerCase()
    const list = q ? packDialog.packs.filter(p => (p.name ?? '').toLowerCase().includes(q)) : packDialog.packs
    // Вже встановлені — в кінець списку (як filteredInstances у Modrinth app).
    return list.slice().sort((a, b) => Number(a.alreadyInstalled) - Number(b.alreadyInstalled))
  })

  // Для PackPicker у діалозі вибору збірки: дозволені = лише сумісні збірки
  // (packDialog.packs), мапа id → сумісна збірка (для кнопок/badge «вже
  // встановлено»). Папки показуються лише з видимими збірками.
  const packDialogAllowedIds = $derived.by((): Set<string> => {
    const s = new Set<string>()
    for (const p of packDialog?.packs ?? []) s.add(p.packId)
    return s
  })
  const packDialogCompatibleById = $derived.by((): Map<string, BrowserCompatiblePack> => {
    const m = new Map<string, BrowserCompatiblePack>()
    for (const p of packDialog?.packs ?? []) m.set(p.packId, p)
    return m
  })

  // Перемикач фільтра збірки прямо на сторінці контенту (бейдж під
  // версією): «Змінити» відкриває вибір ІНШОЇ збірки, «×» знімає фільтр
  // повністю. На відміну від packDialog (вибір збірки для встановлення
  // конкретної версії, список звужений сумісністю) — тут повний список
  // усіх збірок користувача, як у сайдбарі ContentBrowser.
  let packSwitcherOpen = $state(false)
  let packSwitcherFilter = $state('')
  // Дозволені збірки у перемикачі: лише СУМІСНІ з цим контентом
  // (GetBrowserCompatiblePacks з versionID='' = будь-яка версія/лоадер, які
  // підтримує автор). Це «віджет встановлення» сторінки — обираючи збірку
  // тут, користувач каже, КУДИ встановлювати контент, тож несумісні збірки
  // (інший лоадер/версія MC) не мають пропонуватись. null = обмеження
  // немає (ще не завантажено / запит не дав списку — тоді показуємо всі,
  // щоб не заводити користувача в глухий кут через неточні дані джерела).
  let packSwitcherAllowed = $state<Set<string> | null>(null)
  function openPackSwitcher() {
    packSwitcherOpen = true
    packSwitcherFilter = ''
    packSwitcherAllowed = null
    // Ціль зараз — сервер → одразу вкладка «Сервери».
    switcherTab = packContext?.isServer && canPickServer ? 'servers' : 'packs'
    if (!detail || detail.projectType === 'modpack') return
    const dialogPack = packDialog
    void (async () => {
      try {
        const sel = ''
        const list = (await App.GetBrowserCompatiblePacks(source, detail.projectId, detail.projectType, contentDirFor(detail.projectType), sel)) ?? []
        // Якщо за час запиту перемикач закрили або сторінку перезавантажили —
        // не чіпаємо стан. Список порожній = точних даних немає → лишаємо
        // обмеження null (усі збірки), щоб список не зник безслідно.
        if (packSwitcherOpen && list.length > 0) {
          packSwitcherAllowed = new Set(list.map((p: { packId: string }) => p.packId))
        }
      } catch { /* несумісність не можна перевірити — показуємо всі збірки */ }
    })()
  }
  const filteredPackSwitcherPacks = $derived.by(() => {
    const q = packSwitcherFilter.trim().toLowerCase()
    let list: { id: string; name: string; mcVersion: string; loader: string; iconUrl?: string; color?: string; icon?: string }[] = packs
    if (packSwitcherAllowed) list = list.filter(p => packSwitcherAllowed!.has(p.id))
    return q ? list.filter(p => p.name.toLowerCase().includes(q)) : list
  })
  // ── Сервер як ціль встановлення на сторінці проєкту ─────────────────
  // Сервери, які РЕАЛЬНО підтримують цей проєкт: для модів — мод-ядра
  // (fabric/forge/neoforge/quilt), для плагінів — плагін-ядра (paper/purpur/
  // velocity). Сервери без підтримки (vanilla тощо) тут не показуємо.
  // Тип для серверів — з урахуванням лоадерів проєкту (плагін Velocity/
  // Paper може мати project_type="mod" у Modrinth).
  const projectServerTab = $derived(effectiveServerTab(detail?.projectType ?? '', detail?.loaders))
  const projectServerKind = $derived(kindForTab(projectServerTab))
  const projectServers = $derived(serversForTab(servers, projectServerTab))
  // Вкладка перемикача цілі: 'packs' чи 'servers'. Коли поточна ціль —
  // сервер, відкриваємось одразу на серверах.
  let switcherTab = $state<'packs' | 'servers'>('packs')
  // Серверні цілі доступні лише для проєктів, які сервер тримає (моди/плагіни).
  const canPickServer = $derived(!!onPickServerContext && !!projectServerKind && projectServers.length > 0)
  async function pickServerContext(s: BrowserServer) {
    const kind = projectServerKind
    if (!kind || !onPickServerContext) return
    packSwitcherOpen = false
    packSwitcherFilter = ''
    packSwitcherAllowed = null
    await onPickServerContext(s.id, kind)
  }
  // Візуал поточного серверного контексту (реальна іконка/колір) для бейджа.
  const ctxServer = $derived(packContext?.isServer ? (servers.find((s: BrowserServer) => s.id === packContext!.id) ?? null) : null)
  const ctxServerVisual = $derived(ctxServer ? serverVisual(ctxServer) : null)

  function pickPackContext(p: { id: string; name: string; mcVersion: string; loader: string }) {
    packSwitcherOpen = false
    packSwitcherFilter = ''
    packSwitcherAllowed = null
    onChangePackContext?.({ id: p.id, name: p.name, mcVersion: p.mcVersion, loader: p.loader })
  }
  function clearPackContext(e?: MouseEvent) {
    e?.stopPropagation()
    onChangePackContext?.(null)
  }

  // Пресети іконок лоадерів (як у фільтрах браузера).
  const loaderIcons: Record<string, string> = {
    fabric: fabricIcon,
    forge: forgeIcon,
    neoforge: neoforgeIcon,
    quilt: quiltIcon,
    vanilla: vanillaIcon,
  }
  const loaderNames: Record<string, string> = {
    fabric: 'Fabric',
    forge: 'Forge',
    neoforge: 'NeoForge',
    quilt: 'Quilt',
    vanilla: 'Vanilla',
  }
  // Платформи для селекта: лише ті лоадери, для яких у джерела реально є
  // хоч одна версія (звужено ще й під обрану MC-версію, якщо вона задана) —
  // раніше тут був статичний список усіх чотирьох лоадерів, через що в
  // селекті можна було обрати платформу, для якої джерело не мало жодної
  // збірки («Немає доступних версій з джерела»).
  const platformOptions = $derived.by(() => {
    const pool = mcFilter ? versions.filter(v => (v.gameVersions ?? []).includes(mcFilter)) : versions
    const order = ['fabric', 'forge', 'neoforge', 'quilt', 'vanilla']
    const present = new Set(pool.flatMap(v => v.loaders ?? []))
    return ['', ...order.filter(l => present.has(l))]
  })

  function contentDirFor(type: string): string {
    switch (type) {
      case 'resourcepack': return 'resourcepacks'
      case 'shader': return 'shaderpacks'
      case 'datapack': return 'datapacks'
      default: return 'mods'
    }
  }

  // serverKindFor — те саме для СЕРВЕРНОГО контексту (packContext.isServer):
  // App.InstallServerBrowserVersion приймає лише "mods" | "plugins" (сервер
  // не тримає resourcepacks/shaderpacks/datapacks — контент.projectType тут
  // завжди 'mod' або 'plugin', бо ContentBrowser з serverContext показує
  // тільки ці дві вкладки, див. ContentBrowser.svelte).
  function serverKindFor(type: string): 'mods' | 'plugins' {
    return type === 'plugin' ? 'plugins' : 'mods'
  }

  // ── Ідентичність типу ресурсу ─────────────────────────────────────────
  // Кожен тип контенту (модпак/мод/ресурспак/шейдер/датапак) отримує свою
  // іконку + акцентний колір — так сторінка ОДРАЗУ читається з першого
  // погляду («це шейдер», «це датапак»), а не лише по дрібному тексту.
  // Раніше акцент банера — випадковий hash-колір (dominantColor) без жодного
  // зв'язку з типом ресурсу; тепер колір типу — головний акцент сторінки,
  // а dominantColor лишається як тонка добавка у фон банера.
  const typeAccent: Record<string, { icon: string; color: string; rgb: string }> = {
    modpack:      { icon: 'ti-box',          color: '#ff8a00', rgb: '255, 138, 0' },   // orange — фірмовий колір лаунчера
    mod:          { icon: 'ti-puzzle',       color: '#7b4dff', rgb: '123, 77, 255' },  // violet
    resourcepack: { icon: 'ti-brush',        color: '#3b82f6', rgb: '59, 130, 246' },  // blue
    shader:       { icon: 'ti-sun',          color: '#eab308', rgb: '234, 179, 8' },   // gold
    datapack:     { icon: 'ti-database',     color: '#22c55e', rgb: '34, 197, 94' },   // green
  }
  function accentFor(type: string) {
    return typeAccent[type] ?? typeAccent.mod
  }
  function typeLabel(type: string): string {
    switch (type) {
      case 'modpack': return t('browser.type.modpack')
      case 'resourcepack': return t('browser.type.resourcepack')
      case 'shader': return t('browser.type.shader')
      case 'datapack': return t('browser.type.datapack')
      case 'plugin': return t('browser.type.plugin')
      default: return t('browser.type.mod')
    }
  }
  // Дія головної кнопки — теж чутлива до типу (одна фраза замість «Завантажити/Встановити»).
  function primaryActionLabel(type: string): string {
    if (type === 'modpack') return t('browser.project.download')
    if (type === 'datapack') return t('browser.world.installBtn')
    return t('browser.install')
  }

  // Чи є у проєкта хоч одна стабільна (release) версія Minecraft — як
  // isReleaseGameVersion у Modrinth app. Формат release: "1.21.1", "1.20"
  // (чисті мажор.мінор); snapshot/бета мають суфікси ("24w14a",
  // "1.21-pre1") або префікс "snapshot".
  function isReleaseGameVersion(gv: string): boolean {
    return /^\d+\.\d+(\.\d+)?$/.test(gv)
  }

  // ── Фільтрація версій по селекторах (Modrinth-стиль) ─────────────────
  // Раніше список версій просто вантажився весь і користувач гортав усе —
  // тепер «Версія Minecraft»/«Платформа» звужують, як у DownloadProject.
  const filteredVersions = $derived.by(() => {
    // Модпаки: збірка-фільтр не застосовується (версія модпака — це версія
    // НОВОЇ збірки, а не контекст існуючої).
    const packActive = packContext && detail?.projectType !== 'modpack'
    if (packActive) {
      // Збірка задана: версії вже відфільтровані бекендом під неї — але
      // для надійності лишаємо локальний фільтр-страховку.
      const mc = packContext!.mcVersion
      const ld = packContext!.loader
      // gameVersions/loaders можуть бути null у відповіді API (деякі
      // версії CurseForge приходять без цих полів) — без ?? [] фільтр
      // падав би «Cannot read properties of null (reading 'includes')»
      // при виборі версії/лоадера.
      // Лоадер перевіряємо для модів і плагінів (лоадер плагіна —
      // paper/purpur/velocity, та сама логіка сумісності, що для модів):
      // ресурспаки/датапаки/шейдери типо-агностичні (версії часто без
      // loaders або з 'minecraft'), і інакше під fabric-збірку вони б
      // хибно відфільтровувались у порожнечу (бекенд для них теж
      // ігнорує лоадер).
      const loaderRelevant = detail?.projectType === 'mod' || detail?.projectType === 'plugin'
      return versions.filter(v =>
        (!mc || !(v.gameVersions ?? []).length || (v.gameVersions ?? []).includes(mc)) &&
        (!ld || !loaderRelevant || !(v.loaders ?? []).length || (v.loaders ?? []).includes(ld)),
      )
    }
    return versions.filter(v =>
      (!mcFilter || (v.gameVersions ?? []).includes(mcFilter)) &&
      (!loaderFilter || (v.loaders ?? []).includes(loaderFilter)),
    )
  })

  // MC-версії для селекта — з версій проєкту (як Modrinth app бере з
  // game_versions проєкту), сортовані як в API (новіші перші).
  // «Показати всі версії»: коли прапорець не відмічено, сніпшоти/бети
  // ховаються (release-only, як filteredGameVersions у DownloadProject).
  const mcFilterOptions = $derived.by(() => {
    if (packContext && detail?.projectType !== 'modpack') return []
    const set = new Set<string>()
    for (const v of versions) for (const gv of v.gameVersions ?? []) set.add(gv)
    const all = [...set]
    if (showAllVersions) return all
    return all.filter(isReleaseGameVersion)
  })

  // Показуємо checkbox «Показати всі версії» лише коли є і стабільні, і
  // нестабільні версії гри (showVersionsCheckbox у Modrinth app).
  const showVersionsCheckbox = $derived.by(() => {
    let hasRelease = false
    let hasNonRelease = false
    for (const v of versions) {
      for (const gv of v.gameVersions ?? []) {
        if (isReleaseGameVersion(gv)) hasRelease = true
        else hasNonRelease = true
        if (hasRelease && hasNonRelease) return true
      }
    }
    return false
  })

  // Якщо mcFilter (сніпшот/бета) зник з опцій після вимкнення «Показати
  // всі версії» — скидаємо його, щоб селект не показував порожній стан,
  // а фільтр не застосовував приховану версію (Modrinth так само робить
  // через filteredGameVersions: вимкнений checkbox прибирає нестабільні
  // версії з dropdown, а обрана зникає).
  $effect(() => {
    if (mcFilter && !mcFilterOptions.includes(mcFilter)) {
      mcFilter = ''
      applyRecommendedSelection()
    }
  })

  // Коли змінились фільтри (або завантажились версії) — підбираємо
  // рекомендовану версію за правилом Modrinth: остання release → beta → alpha.
  function applyRecommendedSelection() {
    const list = filteredVersions
    if (list.length === 0) return
    const rel = list.find(v => v.versionType === 'release')
    const beta = list.find(v => v.versionType === 'beta')
    const alpha = list.find(v => v.versionType === 'alpha')
    const best = rel ?? beta ?? alpha ?? list[0]
    selectedVersionId = best.id
  }

  // Страховка від «вічного спінера»: якщо виклик мосту не повертається зовсім
  // (зависла мережа, DLL не відповіла, проміс загубився в транспорті Wails),
  // показуємо помилку з кнопкою повтору замість нескінченного лоадера.
  // Таймаут навмисно більший за 30-секундний таймаут HTTP-клієнта бекенду —
  // він ловить саме «завис» випадок, а не повільну мережу (див. wails.ts,
  // withCallTimeout). Сам виклик НЕ скасовується, тому нижче після нього
  // обов'язково перевіряється loadId === loadSeq.
  const DETAIL_TIMEOUT_MS = 45000

  async function load() {
    // Захист від гонитви: якщо користувач швидко перемикається між
    // проєктами, старий in-flight load() не має перезаписувати новий.
    const loadId = ++loadSeq
    loading = true
    loadError = ''
    // Скидаємо і сам проєкт. Раніше detail не обнулявся: після невдалого
    // завантаження на екрані лишався ПОПЕРЕДНІЙ проєкт (ані помилки, ані
    // ознак того, що запит провалився) — користувач бачив чужі дані як свої.
    detail = null
    versions = []
    selectedVersionId = ''
    autoMatched = false
    installedChecked = false
    installedItem = null
    packUnsupported = false
    installedInPacks = []
    installedInPacksLoading = false
    installedInPacksExpanded = false
    // Скидаємо ШІ-стан при переході між проєктами (переклад/відгук
    // належать КОНКРЕТНОМУ проєкту).
    translation = null
    showTranslation = false
    review = null
    reviewError = false
    try {
      detail = await withCallTimeout(
        App.GetBrowserProjectDetail(source, projectId),
        DETAIL_TIMEOUT_MS,
        t('browser.timeout'),
      )
      if (loadId !== loadSeq) return
      // Головне для сторінки вже є — знімаємо спінер ЗАРАЗ. Версії та
      // статус «вже встановлено» довантажуються далі в цій же функції, але
      // сторінку вони вже не тримають: у кожної секції свій індикатор
      // (loadingVersions / installedInPacksLoading). Раніше спінер зникав
      // аж після них усіх, і на повільній мережі це виглядало як «сторінка
      // не завантажується».
      loading = false
      // Початковий стан зірочки — окремий легкий виклик (лише набір
      // ключів, не повні записи), не блокує решту завантаження сторінки.
      App.GetBookmarkedKeys().then(keys => {
        if (loadId === loadSeq) bookmarked = (keys ?? []).includes(source + '|' + projectId)
      }).catch(() => {})
      selectedVersionId = ''
      autoMatched = false
      // Версії. ВАЖЛИВО: помилка версій НЕ повинна вбивати всю сторінку
      // (опис/галерея вже завантажені) — ловимо її окремо і показуємо
      // «немає версій», як раніше робила окрема loadVersions().
      loadingVersions = true
      try {
        // ВАЖЛИВО (запит «модпаки мають вибір версії»): модпак НЕ
        // встановлюється в збірку-фільтр — він СТВОРЮЄ власну нову
        // збірку, тож версію/лоадер збірки-контексту на нього не
        // застосовуємо (інакше для модпака під іншу MC-версію список
        // версій був би порожній). Для контенту (моди/ресурспаки/
        // шейдери/датапаки) збірка-контекст = автопідбір версії під неї
        // (Modrinth app: instance context auto-picks compatible version).
        const isModpack = detail.projectType === 'modpack'
        if (packContext && !isModpack) {
          // Бекенд сам типо-агностичний: GetBrowserProjectVersions приймає
          // projectType і не фільтрує за лоадером збірки для ресурспаків/
          // датапаків/шейдерів; release→beta→alpha підбираємо тут же на
          // вже відфільтрованому списку (без зайвого повторного запиту).
          versions = (await App.GetBrowserProjectVersions(source, detail.projectId, detail.projectType, packContext.mcVersion, packContext.loader)) ?? []
          if (loadId !== loadSeq) return
          if (versions.length > 0) {
            applyRecommendedSelection()
            autoMatched = filteredVersions.length > 0
            packUnsupported = false
          } else {
            // Збірка НЕ підтримує цей контент: жодної версії під її
            // (mc, loader). Не підмінюємо список усіма версіями — чесно
            // показуємо «не підтримується» (віджет встановлення) і
            // «немає сумісних версій» (вкладка Версії).
            versions = []
            autoMatched = false
            packUnsupported = true
            selectedVersionId = ''
          }
        } else {
          versions = (await App.GetBrowserProjectVersions(source, detail.projectId, detail.projectType, '', '')) ?? []
          applyRecommendedSelection()
        }
      } catch (e) {
        console.error(e)
        versions = []
      } finally {
        loadingVersions = false
      }
      if (loadId !== loadSeq) return
      // Аналіз «вже встановлено» у збірці-фільтрі (Modrinth app-стиль).
      // Для модпаків НЕ робимо: модпак не встановлюється в збірку-фільтр,
      // він СТВОРЮЄ власну збірку — «вже встановлено у збірку X» для нього
      // безглузде (інакше поруч з «Завантажити» з'явився б некоректний
      // бейдж/кнопка оновлення).
      if (packContext && detail.projectType !== 'modpack') {
        try {
          const map = packContext.isServer
            ? (await App.GetServerBrowserInstalledMap(packContext.id)) ?? {}
            : (await App.GetBrowserInstalledMap(packContext.id)) ?? {}
          installedItem = map[detail.projectId] || null
        } catch (e) { console.error(e) }
        installedChecked = true
      }
      // «Також встановлено у: ...» — окремо від installedItem: те
      // рахується ЛИШЕ для збірки-фільтра, це ж — по ВСІХ збірках
      // користувача, незалежно від того, звідки відкрито сторінку.
      // Так само не для модпаків (модпак не «встановлюється» в збірку).
      // Запускаємо НЕ чекаючи (не await) — не затримує рендер решти
      // сторінки, бо перебір усіх збірок повільніший за один map-запит.
      // Для серверного контексту (packContext.isServer) пропускаємо: блок
      // «Також встановлено у» — про ЗБІРКИ користувача, сервери в нього
      // не входять (інша сутність, інший список на бекенді), а показувати
      // порожній/оманливий результат тут гірше, ніж не показувати нічого.
      if (detail.projectType !== 'modpack' && !packContext?.isServer) {
        const pid = detail.projectId
        installedInPacksLoading = true
        App.GetBrowserProjectInstalledPacks(pid).then(list => {
          if (loadId !== loadSeq) return
          installedInPacks = list ?? []
          installedInPacksLoading = false
        }).catch(e => {
          console.error(e)
          if (loadId === loadSeq) installedInPacksLoading = false
        })
      }
    } catch (e: any) {
      // Показуємо СПРАВЖНЮ причину, а не загальне «не вдалося завантажити»:
      // саме через узагальнений текст неможливо було зрозуміти, що сталося
      // (таймаут? немає ключа CurseForge? мережа? метод не знайдено?).
      const msg = e?.message ? String(e.message) : String(e)
      // Помилка ПІСЛЯ показу сторінки (напр. підбір рекомендованої версії) не
      // має підміняти вже завантажений проєкт екраном помилки — інакше
      // користувач втрачає сторінку через дрібний збій.
      if (!detail) loadError = t('browser.project.loadFailed', { message: msg })
      toast(t('error.withMessage', { message: msg }), 'error')
    } finally {
      if (loadId === loadSeq) loading = false
    }
  }

  function selectedVersion(): BrowserVersionView | null {
    return versions.find(v => v.id === selectedVersionId) ?? null
  }

  function loaderIconFor(v: BrowserVersionView): string | undefined {
    const l = (v.loaders ?? [])[0]
    return l ? loaderIcons[l.toLowerCase()] : undefined
  }

  function loaderLabelFor(v: BrowserVersionView): string {
    const l = (v.loaders ?? [])[0]
    return l ? (loaderNames[l.toLowerCase()] ?? l) : ''
  }

  function mcLabelFor(v: BrowserVersionView): string {
    return (v.gameVersions ?? []).join(', ')
  }

  function formatSize(n: number): string {
    if (!n) return ''
    if (n >= 1048576) return (n / 1048576).toFixed(1) + ' GB'
    if (n >= 1024) return (n / 1024).toFixed(0) + ' MB'
    return n + ' KB'
  }

  function formatDate(s: string): string {
    if (!s) return ''
    const d = new Date(s)
    return isNaN(d.getTime()) ? '' : d.toLocaleDateString()
  }

  function sideLabel(v: string): string {
    switch (v) {
      case 'required': return t('browser.project.sidebar.required')
      case 'optional': return t('browser.project.sidebar.optional')
      case 'unsupported': return t('browser.project.sidebar.unsupported')
      default: return t('browser.project.sidebar.unknown')
    }
  }

  // normalizeYouTubeEmbed імпортується з lib/sanitizeHtml: той самий хелпер
  // використовує і санітайзер опису, і вкладка «Відео», і вікі збірок.

  // Детект YouTube-посилань в описі проєкту → вкладка «Відео».
  // ВАЖЛИВО («Помилка 153»): усі формати НОРМАЛІЗУЮТЬСЯ у вбудовуваний
  // youtube-nocookie.com/embed/<id> — shorts/ раніше лишався як є
  // (youtube.com/shorts/ID вбудувати не можна, YouTube віддає помилку
  // конфігурації відеопрогравача), а звичайний youtube.com у WebView2
  // часто відхиляє embed через referrer/куки. Параметри ?/& (si=, list=,
  // feature=…) обрізаються — вони ламали embed і тригерили 153.
  function videoLinks(): string[] {
    if (!detail) return []
    const out: string[] = []
    for (const m of (detail.body ?? '').matchAll(/https?:\/\/[^\s"'<>)\]]+/gi)) {
      // Вирізаємо хвіст розмітки (markdown-посилання закривається ')' тощо).
      const u = m[0].replace(/[),.!?;:]+$/, '')
      const embed = normalizeYouTubeEmbed(u)
      if (embed && !out.includes(embed)) out.push(embed)
    }
    return out.slice(0, 6)
  }

  // Підписка на прогрес встановлення (browser:progress).
  let offProgress: (() => void) | null = null
  $effect(() => {
    if (!offProgress) {
      offProgress = on('browser:progress', (p: BrowserProgress) => {
        if (p && p.projectId === projectId) {
          installProgress = p.percent ?? 0
          if (installProgress >= 100) {
            setTimeout(() => { installProgress = 0 }, 600)
          }
        }
      })
    }
  })

  // Скрол sticky-панелі: .mp-topbar тепер sticky відносно .mp-col —
  // ЛІВА колонка є власним незалежним скрол-контейнером (див. CSS
  // .mp-col: overflow-y:auto), а не .content сторінки. .mp-side
  // (права панель) має свій ОКРЕМИЙ скрол і навмисно НЕ впливає на
  // стан топбару. Від'єднуємось при розмонтуванні сторінки.
  $effect(() => {
    const scroller = document.querySelector('.mp-col') as HTMLElement | null
    if (!scroller) return
    const onScr = () => {
      topbarScrolled = scroller.scrollTop > 40
      // Hero-блок (.mp-title) з іконкою і назвою проєкту: поки його нижній
      // край нижче верхньої межі sticky-панелі — він видимий, і панель не
      // дублює іконку/назву.
      //
      // ВАЖЛИВО (баг «топбар при скролі не показує назву й іконку»):
      // .mp-title рендериться усередині bannerBlock, а той при
      // bannerFull=true монтується НАД .mp-main (сестра .mp-col), а не
      // всередині неї — див. розмітку нижче. querySelector('.mp-title')
      // на scroller=.mp-col тоді не знаходив НІЧОГО (hero=null), і
      // fallback `hero ? ... : true` завжди тримав heroVisible=true —
      // топбар назавжди лишався «схлопнутим» (без іконки/назви),
      // незалежно від реального скролу. Шукаємо тепер по всьому
      // документу (banner з .mp-title в обох режимах — єдиний на
      // сторінці), а fallback на «не видимий» (false), а не «видимий»
      // (true) — якщо hero взагалі не знайдено, показати іконку/назву в
      // топбарі безпечніше, ніж мовчки ховати їх назавжди.
      const hero = document.querySelector('.mp-title') as HTMLElement | null
      heroVisible = hero ? hero.getBoundingClientRect().bottom > 8 : false
    }
    scroller.addEventListener('scroll', onScr, { passive: true })
    onScr()
    return () => scroller.removeEventListener('scroll', onScr)
  })

  // Клавіатура лайтбокса (як Modrinth app): ←/→ — навігація, Esc — закрити,
  // +/− — зум, 0 — скинути.
  $effect(() => {
    if (lightboxIndex === null) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') closeLightbox()
      else if (e.key === 'ArrowRight') nextLightbox()
      else if (e.key === 'ArrowLeft') prevLightbox()
      else if (e.key === '+' || e.key === '=') lightboxZoom = Math.min(4, lightboxZoom + 0.25)
      else if (e.key === '-') lightboxZoom = Math.max(0.5, lightboxZoom - 0.25)
      else if (e.key === '0') resetLightboxZoom()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })

  // Головна кнопка встановлення залежно від типу контенту.
  // Датапаки — у КОНКРЕТНИЙ світ збірки (діалог вибору світу,
  // saves/<world>/datapacks): у збірку-фільтр одразу, без фільтра — спершу
  // вибір збірки, потім вибір світу.
  //
  // ВАЖЛИВО (запит «прибрати вибір версії зі сторінки»): для НЕ-модпаків
  // вибір версії більше НЕ на сторінці — він у діалозі встановлення.
  // Тут беремо рекомендовану (автопідбір під збірку або release→beta→alpha)
  // лише як дефолт для входу в діалог/встановлення у фільтр-збірку.
  async function primaryInstall() {
    if (!detail) return
    if (detail.projectType === 'modpack') {
      const v = selectedVersion()
      if (!v) {
        toast(t('browser.project.chooseVersion'), 'info')
        return
      }
      await downloadPack(v)
    } else if (detail.projectType === 'datapack') {
      await openWorldPick(null)
    } else {
      await installIntoPack(null)
    }
  }

  // Датапак: спершу визначаємо цільову збірку (фільтр або діалог вибору),
  // потім відкриваємо діалог вибору світу цієї збірки.
  async function openWorldPick(initialVersion: BrowserVersionView | null) {
    if (packContext) {
      // У збірку-фільтр одразу (рекомендована під збірку версія).
      const v = initialVersion ?? recommendedVersionForPack()
      if (!v) {
        toast(t('browser.project.chooseVersion'), 'info')
        return
      }
      await openWorldDialog(packContext.id, packContext.name, v)
      return
    }
    // Фільтра немає — діалог вибору збірки (версія + сумісні збірки).
    await openPackDialog(initialVersion)
  }

  // Відкриває модалку вибору світів для датапака (saves/<world>/datapacks).
  // Мультивибір: жоден світ не вибраний за замовчуванням — користувач
  // сам відмічає світи (кнопка встановлення вимкнена, доки не вибрано
  // хоча б один). versionID передаємо в InstallDatapackToWorlds: для
  // світів НЕ вмикаємо «остання версія» (versionID=''), щоб користувач
  // бачив, що саме встановлюється, і міг змінити версію перед
  // встановленням.
  async function openWorldDialog(packId: string, packName: string, v: BrowserVersionView) {
    worldGlobal = false
    worldDialog = { packId, packName, worlds: [], loading: true, selectedWorlds: [] }
    try {
      const worlds = (await App.ListInstanceWorlds(packId)) ?? []
      if (worldDialog && worldDialog.packId === packId) {
        worldDialog.worlds = worlds
      }
    } catch (e: any) {
      // Модалку НЕ закриваємо (вікно вибору світів має відкритись завжди):
      // лишаємо порожній список із підказкою, а причину показуємо тостом.
      if (worldDialog && worldDialog.packId === packId) worldDialog.worlds = []
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      if (worldDialog && worldDialog.packId === packId) worldDialog.loading = false
    }
  }

  // Позначити всі світи / зняти всі позначки (мультивибір).
  function selectAllWorlds(on: boolean) {
    if (!worldDialog) return
    worldDialog.selectedWorlds = on ? [...worldDialog.worlds] : []
  }

  // Чи входить світ у вибір (для class:sel у списку).
  function worldSelected(w: string): boolean {
    return !!worldDialog?.selectedWorlds.includes(w)
  }

  // Українська множина для кнопки: 1 світ, 2-4 світи, 5+ світів.
  function installBtnLabel(n: number): string {
    if (n <= 1) return t('browser.world.installBtn')
    const mod10 = n % 10
    const mod100 = n % 100
    if (mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14)) {
      return t('browser.world.installBtnPlural2', { n })
    }
    return t('browser.world.installBtnPlural5', { n })
  }

  async function installDatapackToWorld() {
    const d = worldDialog
    const v = selectedVersion()
    // У загальні datapacks можна і без світів; у світи — лише з вибором.
    if (!d || (!d.selectedWorlds.length && !worldGlobal) || !v || !detail) return
    d.loading = true
    installing = true
    try {
      // 1) Загальна тека datapacks збірки (якщо позначено).
      if (worldGlobal) {
        await App.InstallBrowserContentVersion(d.packId, source, detail.projectId, v.id, 'datapacks', false)
      }
      // 2) Окремі світи (якщо вибрані).
      if (d.selectedWorlds.length > 0) {
        const res = await App.InstallDatapackToWorlds(d.packId, d.selectedWorlds, source, detail.projectId, v.id)
        toast(res?.message || t('browser.installed'), 'success')
      } else {
        toast(t('browser.installed'), 'success')
      }
      worldDialog = null
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      if (worldDialog) worldDialog.loading = false
      installing = false
    }
  }

  // Модпак: «Завантажити» — ДВА кроки, як Modrinth app:
  //   1) CreateBrowserModpack — картка «не завантажено» з'являється в
  //      «Моїх збірках» одразу (іконка/колір), файлів гри ще немає;
  //   2) DownloadBrowserModpack — качає архів у ТОЙ САМИЙ pack ID,
  //      прогрес іде подіями sync:progress (екран синхронізації).
  async function downloadPack(v: BrowserVersionView, force = false) {
    // Перевіряємо ДО створення картки: якщо цей модпак уже є в «Моїх
    // збірках» (той самий source+projectId) — питаємо користувача, а не
    // мовчки плодимо копію з суфіксом -2/-3 в ID.
    if (!force) {
      try {
        const check = await App.CheckBrowserModpackInstalled(source, detail!.projectId)
        if (check?.alreadyInstalled) {
          alreadyInstalledDialog = { packId: check.existingPackId || '', packName: check.existingPackName || '', pendingVersion: v }
          return
        }
      } catch { /* перевірка не критична — продовжуємо звичайним шляхом */ }
    }
    installing = true
    try {
      const created = await App.CreateBrowserModpack(source, detail!.projectId, v.id, force)
      await App.DownloadBrowserModpack(created.id)
      // Одразу на сторінку новоствореної збірки — там видно живий прогрес
      // качання (раніше повертались у браузер і доводилось шукати картку).
      onOpenPack(created.id)
      toast(t('browser.project.installedAs', { name: detail!.name }), 'success')
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      installing = false
    }
  }

  function goToExistingPack() {
    const id = alreadyInstalledDialog?.packId
    alreadyInstalledDialog = null
    if (id && onOpenPack) onOpenPack(id)
  }

  async function createCopyAnyway() {
    const pending = alreadyInstalledDialog?.pendingVersion
    alreadyInstalledDialog = null
    if (pending) await downloadPack(pending, true)
  }

  // Рекомендована версія ПІД ЗБІРКУ-ФІЛЬТР (release → beta → alpha зі
  // списку, вже відфільтрованого бекендом під mc/loader збірки).
  function recommendedVersionForPack(): BrowserVersionView | null {
    const list = filteredVersions
    if (list.length === 0) return null
    const rel = list.find(v => v.versionType === 'release')
    const beta = list.find(v => v.versionType === 'beta')
    const alpha = list.find(v => v.versionType === 'alpha')
    return rel ?? beta ?? alpha ?? list[0]
  }

  // Мод/ресурспак/шейдер: у збірку-фільтр або діалог вибору збірки.
  // initialVersion — конкретна версія (напр. клік по рядку вкладки
  // «Версії»); null = автопідбір (для фільтра-збірки) / діалог з
  // рекомендованою (без фільтра).
  async function installIntoPack(initialVersion: BrowserVersionView | null) {
    if (!detail) return
    if (packContext) {
      // Збірка-фільтр: версія підбирається автоматично під її mc/loader
      // (жодного вибору версії на сторінці — як просив ужиток).
      const v = initialVersion ?? recommendedVersionForPack()
      if (!v) {
        toast(t('browser.project.chooseVersion'), 'info')
        return
      }
      if (installedItem && installedItem.versionId === v.id) {
        toast(t('browser.installConfirm.alreadyUpToDate'), 'info')
        return
      }
      const ok = installedItem
        ? await confirmDialog(
            t('browser.installConfirm.updateTitle', { name: detail.name }),
            t('browser.installConfirm.updateMessage', { from: installedItem.versionNumber || '—', to: v.versionNumber }),
            { confirmLabel: t('browser.installConfirm.updateBtn'), cancelLabel: t('browser.installConfirm.cancel'), danger: false },
          )
        : await confirmDialog(
            t('browser.installConfirm.installTitle', { name: detail.name }), '',
            { confirmLabel: t('browser.installConfirm.installBtn'), cancelLabel: t('browser.installConfirm.cancel'), danger: false },
          )
      if (!ok) return
      installing = true
      try {
        // isServer — той самий контент (мод/плагін), інший цільовий
        // об'єкт (сервер, не збірка) і інший бекенд-метод: worker/API
        // збірок і серверів історично розійшлись (ManifestClient проти
        // srvAPI), тож на рівні Go це справді два різні виклики навіть
        // при однаковому UI-результаті.
        const res = packContext.isServer
          ? await App.InstallServerBrowserVersion(
              packContext.id, source, detail.projectId, v.id,
              serverKindFor(projectServerTab), !!installedItem,
            )
          : await App.InstallBrowserContentVersion(
              packContext.id, source, detail.projectId, v.id,
              contentDirFor(detail.projectType), !!installedItem,
            )
        // res.message несе «Встановлено» + список авто-встановлених
        // залежностей/companion-ресурспаків (порт DownloadDependencies) —
        // показуємо його, а не хардкод, щоб користувач бачив, що довантажилося.
        toast(res?.message || t('browser.installed'), 'success')
        installedItem = { ...(installedItem ?? {} as any), versionId: v.id, versionNumber: v.versionNumber }
      } catch (e: any) {
        toast(t('error.withMessage', { message: e }), 'error')
      } finally {
        installing = false
      }
      return
    }
    // Немає фільтра збірки — діалог вибору (версія + сумісні збірки).
    // Серверний контекст (packContext.isServer) СЮДИ ніколи не потрапляє:
    // сервер завжди один фіксований — на відміну від збірок, для нього
    // немає «діалогу вибору», бо ContentBrowser відкритий із serverContext
    // завжди приходить із заданим packContext (див. ContentBrowser.svelte).
    await openPackDialog(initialVersion)
  }

  // Відкриває діалог вибору збірки.
  //
  // ВАЖЛИВО (запит «загальна кнопка встановлення → усі сумісні збірки,
  // а не вибір версії»):
  //   - без initialVersion (загальна кнопка «Встановити») — selectedVersionId=''
  //     і бекенд повертає ВСІ сумісні збірки (GetBrowserCompatiblePacks з
  //     versionID='' = будь-яка версія/лоадер, які підтримує автор контенту);
  //     версію під кожну збірку підбирає ЛАУНЧЕР при встановленні
  //     (InstallBrowserContentVersion з versionID='' сам резолвить
  //     release→beta→alpha під (mc, loader) збірки);
  //   - з initialVersion (клік по рядку у вкладці «Версії») — версія фіксується
  //     і збірки фільтруються СТРОГО під неї, встановлюється саме вона.
  async function openPackDialog(initialVersion: BrowserVersionView | null) {
    if (!detail) return
    packDialogFilter = ''
    dialogTab = 'packs'
    packDialog = { packs: [], versions: [], loading: true, selectedVersionId: '' }
    const dialog = packDialog
    try {
      // Версії НЕ завантажуємо: вибір версії прибрано з модалки — завжди
      // «авто» (''), лаунчер підбирає версію під (mc, loader) обраної збірки
      // при встановленні. Раніше спершу йшов запит версій, і лише потім —
      // сумісних збірок: модалка «спочатку показувала немає збірки, а потім
      // список». Тепер одразу запит збірок.
      const sel = initialVersion?.id ?? ''
      packDialog.selectedVersionId = sel
      const packsList = (await App.GetBrowserCompatiblePacks(source, detail.projectId, detail.projectType, contentDirFor(detail.projectType), sel)) ?? []
      // Діалог міг закритись або перевідкритись за час запиту — пишемо
      // результат лише у той самий діалог, що відкривали.
      if (packDialog === dialog) packDialog.packs = packsList
    } catch (e: any) {
      if (packDialog === dialog) {
        toast(t('error.withMessage', { message: e }), 'error')
        packDialog = null
      }
    } finally {
      if (packDialog === dialog) packDialog.loading = false
    }
  }

  // «Встановити» на сервер прямо з діалогу вибору цілі (без вибору версії:
  // бекенд сам підбирає найкращу сумісну під MC-версію і ядро сервера).
  async function installToPickedServer(srv: BrowserServer) {
    const kind = projectServerKind
    if (!detail || !kind) return
    const dialog = packDialog
    installing = true
    try {
      const res = await App.InstallServerBrowserVersion(
        srv.id, source, detail.projectId, dialog?.selectedVersionId ?? '', kind, false,
      )
      toast(res?.message || t('browser.installed'), 'success')
      packDialog = null
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      installing = false
    }
  }

  async function installToPickedPack(packId: string) {
    const dialog = packDialog
    if (!dialog || !detail) return
    // Обрана в діалозі версія; '' = «авто» (лаунчер сам підбере найкращу
    // версію під (mc, loader) збірки при встановленні). Список versions у
    // діалозі більше не завантажується, тож беремо id напряму.
    const v: BrowserVersionView | null = dialog.selectedVersionId
      ? ({ id: dialog.selectedVersionId } as BrowserVersionView)
      : null
    // Датапак: збірку обрано — тепер обираємо світ цієї збірки. Для «авто»
    // (версії в діалозі не обирали) резолвимо рекомендовану під збірку.
    // БАГ «вікно вибору світів не відкривається»: помилки резолву версії
    // (збій API тощо) раніше губилися без try/catch — клік мовчки нічого
    // не показував. Тепер помилка видима тостом.
    if (detail.projectType === 'datapack') {
      packDialog = null
      const p = dialog.packs.find(x => x.packId === packId)
      try {
        if (!v) {
          const rec = await App.GetBrowserRecommendedVersion(source, detail.projectId, 'datapack', p?.mcVersion ?? '', p?.loader ?? '')
          if (!rec) {
            toast(t('browser.project.chooseVersion'), 'info')
            return
          }
          await openWorldDialog(packId, p?.name ?? packId, rec)
          return
        }
        await openWorldDialog(packId, p?.name ?? packId, v)
      } catch (e: any) {
        toast(t('error.withMessage', { message: e }), 'error')
      }
      return
    }
    dialog.loading = true
    installing = true
    try {
      const res = await App.InstallBrowserContentVersion(
        packId, source, detail.projectId, v?.id ?? '',
        contentDirFor(detail.projectType), false,
      )
      toast(res?.message || t('browser.installed'), 'success')
      packDialog = null
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      dialog.loading = false
      installing = false
    }
  }

  // Клік по кнопці встановлення в рядку вкладки «Версії»: для модпака —
  // завантажити саме цю версію; для решти контенту — у збірку-фільтр
  // одразу, або в діалог вибору збірки з цією версією наперед обраною.
  async function onVersionRowInstall(v: BrowserVersionView) {
    if (!detail) return
    if (detail.projectType === 'modpack') {
      await downloadPack(v)
    } else if (detail.projectType === 'datapack') {
      await openWorldPick(v)
    } else {
      await installIntoPack(v)
    }
  }

  // ── ШІ: переклад сторінки (Groq, кешується) ───────────────────────────
  // Працює ЛИШЕ по кнопці «Перекласти» (не автоматично). Повторне
  // відкриття сторінки показує закешований переклад без запиту до API;
  // користувач може перемикатися переклад ↔ оригінал.
  async function translatePage() {
    if (!detail || translationLoading) return
    if (translation) {
      // Вже перекладено — просто перемикаємо показ (кеш на бекенді).
      showTranslation = !showTranslation
      return
    }
    translationLoading = true
    try {
      translation = await App.TranslateBrowserProject(source, detail.projectId, getLanguage())
      showTranslation = true
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      translationLoading = false
    }
  }

  // ── ШІ: AI-відгук про проєкт ──────────────────────────────────────────
  // Генерується ОДИН раз і кешується — перегенерація заборонена (після
  // успіху кнопка вимикається). Повторний запуск можливий лише після
  // помилки (reviewError) — помилки не кешуються.
  async function loadReview() {
    if (!detail || reviewLoading || review) return
    reviewLoading = true
    reviewError = false
    try {
      review = await App.GetBrowserProjectReview(source, detail.projectId, getLanguage())
    } catch (e: any) {
      reviewError = true
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      reviewLoading = false
    }
  }

  // Чи саме ця версія встановлена у збірці-фільтрі (Modrinth app-аналіз).
  function isVersionInstalled(v: BrowserVersionView): boolean {
    if (!installedItem) return false
    return !!(installedItem.versionId && v.id === installedItem.versionId)
  }

  // Встановлена версія СТАРІША за вибрану — кнопка «Оновити».
  function hasNewerSelected(): boolean {
    if (!installedItem || !installedItem.versionId || !selectedVersion()) return false
    return selectedVersion()!.id !== installedItem.versionId
  }

  // ── Лайтбокс галереї ──────────────────────────────────────────────────
  function currentGalleryImage(): BrowserProjectDetail['gallery'][number] | null {
    if (lightboxIndex === null || !detail) return null
    return detail.gallery[lightboxIndex] ?? null
  }

  function openLightbox(i: number) {
    lightboxIndex = i
    resetLightboxZoom()
  }
  function closeLightbox() {
    lightboxIndex = null
    resetLightboxZoom()
  }
  function nextLightbox() {
    if (lightboxIndex === null || !detail || detail.gallery.length === 0) return
    lightboxIndex = (lightboxIndex + 1) % detail.gallery.length
    resetLightboxZoom()
  }
  function prevLightbox() {
    if (lightboxIndex === null || !detail || detail.gallery.length === 0) return
    lightboxIndex = (lightboxIndex - 1 + detail.gallery.length) % detail.gallery.length
    resetLightboxZoom()
  }
  function resetLightboxZoom() {
    lightboxZoom = 1
    lightboxPan = { x: 0, y: 0 }
  }
  function onLightboxWheel(e: WheelEvent) {
    e.preventDefault()
    lightboxZoom = Math.max(0.5, Math.min(4, lightboxZoom + (e.deltaY < 0 ? 0.15 : -0.15)))
    if (lightboxZoom === 1) lightboxPan = { x: 0, y: 0 }
  }
  function onLightboxDragStart(e: MouseEvent) {
    lightboxDragging = true
    lightboxDragStart = { x: e.clientX, y: e.clientY, px: lightboxPan.x, py: lightboxPan.y }
  }
  function onLightboxDragMove(e: MouseEvent) {
    if (!lightboxDragging) return
    lightboxPan = {
      x: lightboxDragStart.px + (e.clientX - lightboxDragStart.x),
      y: lightboxDragStart.py + (e.clientY - lightboxDragStart.y),
    }
  }
  function onLightboxDragEnd() {
    lightboxDragging = false
  }

  // ── Хелпери відображення ──

  const palette = ['#ff8a00', '#7b4dff', '#22c55e', '#3b82f6', '#ef4444', '#06b6d4', '#eab308', '#ec4899']
  function dominantColor(d: BrowserProjectDetail): string {
    let h = 0
    for (const ch of d.projectId + d.name) h = (h * 31 + ch.charCodeAt(0)) >>> 0
    return palette[h % palette.length]
  }

  function mcSummary(d: BrowserProjectDetail): string {
    if (!d.mcVersions?.length) return ''
    const rel = d.mcVersions.filter(v => /^\d+\.\d+(\.\d+)?$/.test(v))
    const list = rel.length >= 3 ? rel : d.mcVersions
    return list.slice(0, 3).join(', ') + (list.length > 3 ? ' +' + (list.length - 3) : '')
  }

  function fmtCount(n: number): string {
    if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M'
    if (n >= 1000) return (n / 1000).toFixed(1) + 'K'
    return String(n || 0)
  }

  // ── Рендер опису (markdown Modrinth / HTML CurseForge) ────────────────
  // CurseForge body — готовий HTML (з білого списку через sanitizeHtml).
  // Modrinth/зміни/відгук — markdown (GFM через marked), результат теж
  // санатизується: markdown-опис може містити raw HTML (Modrinth дозволяє
  // вбудовані теги в описах), і все небезпечне має вирізатись однаково.
  function renderBody(body: string, isHtml = false): string {
    if (!body) return ''
    if (isHtml) return sanitizeHtml(body)
    let html: string
    try {
      html = marked.parse(body, { gfm: true, breaks: true, async: false }) as string
    } catch {
      html = escMd(body)
    }
    return sanitizeHtml(html, { videoPosters: true, videoLabel: t('browser.project.playVideo') })
  }

  // escMd — останній захист: якщо marked упав, показуємо текст без розмітки
  // (не сирий HTML, який міг би виконатись).
  function escMd(s: string): string {
    return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
  }

  // Делегований клік по контейнеру опису: клік на постері відео
  // (вставлений у {@html} — Svelte не бачить внутрішніх хендлерів)
  // монтує справжній iframe-плеєр замість постера.
  function onBodyClick(e: MouseEvent) {
    const t = (e.target as HTMLElement).closest?.('.mp-body-video') as HTMLElement | null
    if (!t || !t.dataset.yt) return
    const id = t.dataset.yt
    t.innerHTML =
      `<iframe src="https://www.youtube-nocookie.com/embed/${id}?autoplay=1&rel=0" ` +
      `title="YouTube" ` +
      `allow="accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture; web-share" ` +
      `allowfullscreen></iframe>`
  }

  // mdToHtml видалено: самописний порядковий парсер не вмів багаторядкові
  // зображення/посилання, fenced code blocks з мовою і вкладені списки.
  // Опис рендериться через marked (GFM) у renderBody, результат проходить
  // sanitizeHtml (той самий білий список, що й для CurseForge HTML).

  // Запуск завантаження проєкту. ВАЖЛИВО: НЕ можна писати просто
  // $effect(() => { load() }) — load() синхронно робить ++loadSeq (читає І
  // пише loadSeq), тому ефект, що читає loadSeq, перезапускався б
  // нескінченно і вішав увесь UI (сторінка не вантажиться, перемикання
  // розділів мертве). Замість цього — keyed-ефект: тіло запускається лише
  // коли змінився source/projectId, а запис loadedKey всередині ефекту
  // блокує повторні запуски (другий прохід бачить той самий key).
  // ВАЖЛИВО: у ключ входить і packContext?.id — зміна фільтра збірки
  // просто на сторінці (кнопки «Змінити»/«×» біля бейджа) має перерахувати
  // версії/автопідбір/installedItem під нову збірку, а не лишити старі
  // дані від попереднього фільтра. Це трохи «дорожче» за точковий
  // перерахунок (load() тягне GetBrowserProjectDetail заново), але
  // лишає код простим і без дублювання логіки підбору версії.
  let loadedKey = $state('')
  $effect(() => {
    const key = `${source}::${projectId}::${packContext?.id ?? ''}`
    if (loadedKey === key) return
    loadedKey = key
    load()
  })
</script>

<div class="mp-wrap">
  {#if loading}
    <div class="mp-loading"><i class="ti ti-loader spin"></i> {t('browser.project.loading')}</div>
  {:else if !detail}
    <!-- Помилка завантаження: показуємо ПРИЧИНУ (її формує load()) і даємо
         повторити запит — раніше був лише текст «Не вдалося завантажити
         проєкт» без пояснень і без жодної дії, крім «Назад». -->
    <div class="mp-empty">
      <i class="ti ti-alert-triangle"></i>
      <div>{loadError || t('browser.project.notFound')}</div>
      <div class="mp-empty-acts">
        <button class="btn btn-primary btn-sm" onclick={() => load()}><i class="ti ti-refresh"></i> {t('browser.retry')}</button>
        <button class="btn btn-ghost btn-sm" onclick={onBack}><i class="ti ti-arrow-left"></i> {t('browser.project.back')}</button>
      </div>
    </div>
  {:else}
    {@const acc = accentFor(detail.projectType)}
    <!-- Кнопка назад + компактна sticky-панель: прикріплена зверху під час
         гортання вниз (не заважає читати контент). Містить повернення до
         збірки, іконку контенту з назвою та міні-інфо і кнопку «Відкрити
         в Modrinth/CurseForge». -->
    <div class="mp-topbar" class:scrolled={topbarScrolled} class:collapsed={heroVisible} style="--tacc:{acc.color}; --tacc-rgb:{acc.rgb}">
      <div class="mp-tb-left">
        <button class="mp-tb-back" onclick={onBack} title={t('browser.project.back')}>
          <i class="ti ti-arrow-left"></i>
        </button>
        {#if detail.iconUrl}
          <span class="mp-tb-ic"><img src={detail.iconUrl} alt="" loading="lazy" use:smartImage /></span>
        {:else}
          <span class="mp-tb-ic mp-ic-fallback"><i class="ti {acc.icon}"></i></span>
        {/if}
        <div class="mp-tb-meta">
          <div class="mp-tb-name">{detail.name}</div>
          <div class="mp-tb-sub">{mcSummary(detail)} · {fmtCount(detail.downloads)} {t('browser.project.sidebar.downloads')}</div>
        </div>
      </div>
      <div class="mp-tb-right">
        <button
          class="mp-bookmark-btn"
          class:on={bookmarked}
          disabled={bookmarkBusy}
          onclick={toggleBookmark}
          title={bookmarked ? t('browser.saved.removeBookmark') : t('browser.saved.addBookmark')}
        >
          <i class="ti {bookmarked ? 'ti-star-filled' : 'ti-star'}"></i>
        </button>
        <div class="mp-col-menu-wrap">
          <button
            class="mp-bookmark-btn"
            onclick={(e) => { e.stopPropagation(); openCollectionsMenu() }}
            title={t('browser.saved.addToCollection')}
          >
            <i class="ti ti-folder-plus"></i>
          </button>
          {#if collectionsMenuOpen}
            <div class="ctx-menu mp-col-menu" onclick={(e) => e.stopPropagation()}>
              {#if collectionsMenuLoading}
                <div class="ctx-hint"><i class="ti ti-loader spin"></i> {t('loading')}</div>
              {:else if collectionsForMenu.length === 0}
                <div class="ctx-hint">{t('browser.saved.emptyCollections')}</div>
              {:else}
                {#each collectionsForMenu as c (c.id)}
                  <button class="ctx-item" onclick={() => addToCollection(c.id)}>
                    <i class="ti ti-folder"></i> <span style="flex:1">{c.name}</span>
                    <span class="mp-col-menu-count">{c.itemCount}</span>
                  </button>
                {/each}
                <div class="ctx-sep"></div>
              {/if}
              <button class="ctx-item" onclick={createCollectionAndAdd}>
                <i class="ti ti-folder-plus"></i> <span>{t('browser.saved.newCollection')}</span>
              </button>
            </div>
          {/if}
        </div>
        {#if canShare && detail.projectUrl}
          <ShareToFriend {detail} {onGoFriends} />
        {/if}
        <a class="mp-external" href={detail.projectUrl} target="_blank" rel="noreferrer" title={t('browser.project.openSource')}>
          <i class="ti ti-external-link"></i> <span class="mp-external-txt">{detail.source === 'curseforge' ? 'CurseForge' : 'Modrinth'}</span>
        </a>
      </div>
    </div>

    <!-- ═══ HERO: банер проєкту, ідентичність типу ресурсу зверху ═══
         ДВА режими (Налаштування → Вигляд → «Банер збірки на всю
         ширину», packBannerFull):
           - звичайний (за замовч.): банер у колонці контенту;
           - на всю ширину РОЗДІЛУ: окремий блок над .mp-main (див. markup нижче). -->
    {#snippet bannerBlock(d: BrowserProjectDetail)}
      <div class="mp-banner" class:mp-banner-full={bannerFull} style="--pcol:{dominantColor(d)}; --tacc:{acc.color}; --tacc-rgb:{acc.rgb}">
        <div class="mp-banner-bg">
          {#if (d.gallery ?? []).length > 0}
            <img class="mp-banner-art" src={d.gallery[0].url} alt="" loading="lazy" use:smartImage />
          {/if}
        </div>

        <!-- Тип ресурсу — головний ідентифікатор сторінки, завжди перше,
             що бачить користувач: іконка + підпис у фірмовому кольорі типу. -->
        <div class="mp-type-badge">
          <i class="ti {acc.icon}"></i> {typeLabel(d.projectType)}
        </div>

        <div class="mp-title">
          {#if d.iconUrl}
            <span class="mp-ic"><img src={d.iconUrl} alt="" use:smartImage /></span>
          {:else}
            <span class="mp-ic mp-ic-fallback"><i class="ti {acc.icon}"></i></span>
          {/if}
          <div class="mp-meta">
            <div class="mp-name">{d.name}</div>
            {#if d.description}<div class="mp-tagline">{d.description}</div>{/if}
            <div class="mp-badges">
              <span class="badge-mc"><i class="ti ti-cube"></i> {mcSummary(d)}</span>
              {#if d.author}
                <span class="badge-author"><i class="ti ti-user"></i> {d.author}</span>
              {/if}
              {#if installedItem && installedChecked}
                <span class="chip g"><i class="ti ti-check"></i> {t('browser.installed.short')}</span>
              {/if}
            </div>
            {#if (d.categories ?? []).length > 0}
              <div class="mp-cat-row">
                {#each (d.categories ?? []).slice(0, 6) as cat}
                  <span class="ib-tag">{cat}</span>
                {/each}
              </div>
            {/if}
          </div>
        </div>
        <div class="mp-stats">
          <span title={t('browser.sort.downloads')}><i class="ti ti-download"></i> {fmtCount(d.downloads)}</span>
          <span title={t('browser.sort.follows')}><i class="ti ti-heart"></i> {fmtCount(d.followers)}</span>
          <button
            class="mp-bookmark-btn mp-bookmark-btn-hero"
            class:on={bookmarked}
            disabled={bookmarkBusy}
            onclick={toggleBookmark}
            title={bookmarked ? t('browser.saved.removeBookmark') : t('browser.saved.addBookmark')}
          >
            <i class="ti {bookmarked ? 'ti-star-filled' : 'ti-star'}"></i>
            <span>{bookmarked ? t('browser.saved.removeBookmark') : t('browser.saved.addBookmark')}</span>
          </button>
        </div>
      </div>
    {/snippet}

    <!-- banner-full живе НАД .mp-main (не всередині нього): .mp-main —
         це тепер flex-ряд фіксованої висоти з двома незалежно скрольованими
         колонками (.mp-col/.mp-side), а не CSS grid, тож "на всю ширину
         розділу" досягається звичайним розташуванням блоку одним рівнем
         вище, а не grid-column: 1/-1 усередині ряду колонок. -->
    {#if bannerFull}
      {@render bannerBlock(detail)}
    {/if}

    <div class="mp-main">
      <!-- ═══ КОЛОНКА КОНТЕНТУ ═══ -->
      <div class="mp-col">
        {#if !bannerFull}
          {@render bannerBlock(detail)}
        {/if}

        <!-- Вкладки (як у Modrinth app: Опис/Галерея/Відео/Зміни/Версії) -->
        <div class="mp-tabs">
          <button class="mp-tab" class:active={tab === 'description'} onclick={() => tab = 'description'}><i class="ti ti-file-text"></i> {t('browser.project.tab.description')}</button>
          <button class="mp-tab" class:active={tab === 'gallery'} onclick={() => tab = 'gallery'}><i class="ti ti-photo"></i> {t('browser.project.tab.gallery')} {#if (detail.gallery ?? []).length}<span class="num">{(detail.gallery ?? []).length}</span>{/if}</button>
          {#if videoLinks().length > 0}
            <button class="mp-tab" class:active={tab === 'videos'} onclick={() => tab = 'videos'}><i class="ti ti-brand-youtube"></i> {t('browser.project.tab.videos')}</button>
          {/if}
          {#if selectedVersion()?.changelog}
            <button class="mp-tab" class:active={tab === 'changelog'} onclick={() => tab = 'changelog'}><i class="ti ti-history"></i> {t('browser.project.tab.changelog')}</button>
          {/if}
          <button class="mp-tab" class:active={tab === 'versions'} onclick={() => tab = 'versions'}><i class="ti ti-versions"></i> {t('browser.project.tab.versions')} <span class="num">{versions.length}</span></button>
        </div>

        {#key tab}
        <div class="mp-tab-content">
          {#if tab === 'description'}
            <!-- Панель ШІ: переклад сторінки (кешується, перемикач
                 переклад ↔ оригінал) + AI-відгук про проєкт (генерується
                 ОДИН раз, перегенерація заборонена). -->
            <div class="mp-ai-bar">
              <button class="btn btn-ghost btn-sm" onclick={translatePage} disabled={translationLoading || !detail.body}>
                {#if translationLoading}<i class="ti ti-loader spin"></i>
                {:else}<i class="ti ti-language"></i>{/if}
                {translation
                  ? (showTranslation ? t('browser.ai.showOriginal') : t('browser.ai.showTranslation'))
                  : t('browser.ai.translate')}
              </button>
              {#if translation?.fromCache}
                <span class="mp-ai-cache"><i class="ti ti-database"></i> {t('browser.ai.cached')}</span>
              {/if}
              <button class="btn btn-ghost btn-sm" onclick={loadReview} disabled={reviewLoading || !!review}>
                {#if reviewLoading}<i class="ti ti-loader spin"></i>
                {:else}<i class="ti ti-sparkles"></i>{/if}
                {review
                  ? t('browser.ai.reviewReady')
                  : (reviewError ? t('browser.ai.retry') : t('browser.ai.review'))}
              </button>
              {#if review?.fromCache}
                <span class="mp-ai-cache"><i class="ti ti-database"></i> {t('browser.ai.cached')}</span>
              {/if}
            </div>
            <!-- AI-відгук ОДРАЗУ під панеллю ШІ (там, де кнопка), а не
                 внизу опису: щоб користувач бачив відгук одразу після
                 генерації, а не прокручував увесь текст. -->
            {#if review}
              <div class="mp-review">
                <div class="mp-review-head">
                  <i class="ti ti-sparkles"></i> {t('browser.ai.reviewTitle')}
                  {#if review.fromCache}
                    <span class="mp-ai-cache"><i class="ti ti-database"></i> {t('browser.ai.cached')}</span>
                  {/if}
                  <span class="mp-review-date">{formatDate(review.created)}</span>
                </div>
                <div class="mp-review-body">{@html renderBody(review.review)}</div>
                {#if (review.sources ?? []).length > 0}
                  <div class="mp-review-sources">
                    <div class="mp-review-src-title"><i class="ti ti-world"></i> {t('browser.ai.sources')}</div>
                    {#each review.sources as s}
                      <a class="mp-review-src" href={s} target="_blank" rel="noreferrer">{s}</a>
                    {/each}
                  </div>
                {/if}
              </div>
            {/if}
            <!-- Фолбек: якщо переклад виглядає як сирий JSON ({"... — зі
                 старого кешу або глюка моделі) — показуємо оригінал. -->
            {@const shownBody = (translation && showTranslation && translation.body) ? translation.body : detail.body}
            <div class="mp-body" onclick={onBodyClick}>{@html renderBody(shownBody && !shownBody.trim().startsWith('{"') ? shownBody : detail.body, detail.source === 'curseforge')}</div>
          {:else if tab === 'gallery'}
            {#if (detail.gallery ?? []).length === 0}
              <div class="mp-empty sm"><i class="ti ti-photo-off empty-float"></i><div>{t('browser.project.noGallery')}</div></div>
            {:else}
              <div class="mp-gallery">
                {#each detail.gallery ?? [] as g, i (g.url)}
                  <div class="mp-g-item" onclick={() => openLightbox(i)} role="button" tabindex="0">
                    <img src={g.url} alt={g.title || ''} loading="lazy" use:smartImage />
                    {#if g.title}<div class="mp-g-cap">{g.title}</div>{/if}
                    <div class="mp-g-zoom"><i class="ti ti-zoom-in"></i></div>
                  </div>
                {/each}
              </div>
            {/if}
          {:else if tab === 'videos'}
            {#if videoLinks().length === 0}
              <div class="mp-empty sm"><i class="ti ti-video-off"></i><div>{t('browser.project.noVideos')}</div></div>
            {:else}
              <!-- Відео: ПОСТЕР + кнопка ▶, iframe підвантажується ЛИШЕ за
                   кліком. Причина — «Помилка 153»: YouTube у WebView2
                   майже завжди відхиляє embed, завантажений одразу
                   (конфігурація відеопрогравача / куки), і замість відео
                   юзер бачив чорний квадрат з помилкою. Тепер у вкладці
                   завжди гарний постер (i.ytimg.com працює в WebView2 без
                   обмежень); плеєр з'являється лише коли юзер САМ натиснув
                   ▶, а поряд завжди є кнопка «Дивитись на YouTube». -->
              <div class="mp-videos">
                {#each videoLinks() as url, i (url)}
                  {@const ytId = /embed\/([A-Za-z0-9_-]{6,})/.exec(url)?.[1] ?? ''}
                  {@const thumb = ytId ? `https://i.ytimg.com/vi/${ytId}/hqdefault.jpg` : ''}
                  <div class="mp-video">
                    {#if ytId && playingVideo === ytId}
                      <iframe
                        src={`https://www.youtube-nocookie.com/embed/${ytId}?autoplay=1&rel=0`}
                        title="YouTube"
                        allow="accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture; web-share"
                        allowfullscreen
                      ></iframe>
                    {:else if ytId}
                      <button
                        class="mp-video-poster"
                        onclick={() => playingVideo = ytId}
                        aria-label={t('browser.project.playVideo')}
                        title={t('browser.project.playVideo')}
                      >
                        <img src={thumb} alt="" loading="lazy"
                             use:smartImage
                             onerror={(e) => { const el = e.currentTarget as HTMLImageElement; if (el.src.includes('/hqdefault.jpg')) el.src = el.src.replace('/hqdefault.jpg', '/mqdefault.jpg') }} />
                        <span class="mp-video-play"><i class="ti ti-player-play-filled"></i></span>
                      </button>
                    {/if}
                    {#if ytId}
                      <a
                        class="mp-video-fallback"
                        href="https://www.youtube.com/watch?v={ytId}"
                        target="_blank"
                        rel="noreferrer"
                      >
                        <i class="ti ti-brand-youtube"></i> {t('browser.project.watchOnYoutube')}
                      </a>
                    {/if}
                  </div>
                {/each}
              </div>
            {/if}
          {:else if tab === 'changelog'}
            {#if selectedVersion()?.changelog}
              <div class="mp-body mp-changelog">{@html renderBody(selectedVersion()!.changelog, detail.source === 'curseforge')}</div>
            {:else}
              <div class="mp-empty sm"><i class="ti ti-history"></i><div>{t('browser.project.tab.changelogEmpty')}</div></div>
            {/if}
          {:else}
            <div class="mp-versions">
              {#if packContext && packUnsupported && detail?.projectType !== 'modpack'}
                <!-- Збірка не підтримує контент: явне «немає сумісних версій»
                     замість порожнього списку/загального «немає версій». -->
                <div class="mp-empty sm">
                  <i class="ti ti-plug-x"></i>
                  <div>{t('browser.update.noCompatibleForPack', { pack: packContext.name })}</div>
                  {#if onChangePackContext}
                    <div class="mp-empty-acts">
                      <button class="btn btn-ghost btn-sm" onclick={openPackSwitcher}><i class="ti ti-replace"></i> {t('browser.project.changePack')}</button>
                      <button class="btn btn-ghost btn-sm" onclick={clearPackContext}><i class="ti ti-x"></i> {t('browser.project.clearPack')}</button>
                    </div>
                  {/if}
                </div>
              {:else if versions.length === 0}
                <div class="mp-empty sm"><i class="ti ti-versions"></i><div>{t('browser.update.noVersions')}</div></div>
              {:else if filteredVersions.length === 0}
                <div class="mp-empty sm"><i class="ti ti-search-off"></i><div>{t('browser.update.noVersions')}</div></div>
              {:else}
                {#each filteredVersions as v (v.id)}
                  <div class="mp-v-row" class:sel={v.id === selectedVersionId} onclick={() => { selectedVersionId = v.id; autoMatched = false }}>
                    <div class="mp-v-row-main">
                      <div class="mp-v-row-name">
                        {#if loaderIconFor(v)}<img class="mp-v-row-ic" src={loaderIconFor(v)} alt="" />{/if}
                        <b>{v.name || v.versionNumber}</b>
                        {#if v.versionType === 'release'}<span class="chip g">{t('browser.project.recommended')}</span>{/if}
                        {#if isVersionInstalled(v)}<span class="chip g"><i class="ti ti-check"></i> {t('browser.installed.short')}</span>{/if}
                      </div>
                      <div class="mp-v-row-meta">
                        <span><i class="ti ti-cube"></i> {mcLabelFor(v)}</span>
                        {#if loaderLabelFor(v)}<span><i class="ti ti-plug"></i> {loaderLabelFor(v)}</span>{/if}
                        <span><i class="ti ti-calendar"></i> {formatDate(v.datePublished)}</span>
                        {#if v.fileSize}<span><i class="ti ti-file"></i> {formatSize(v.fileSize)}</span>{/if}
                      </div>
                    </div>
                    <div class="mp-v-row-actions">
                      <button class="btn btn-primary btn-sm" onclick={(e) => { e.stopPropagation(); selectedVersionId = v.id; onVersionRowInstall(v) }}>
                        <i class="ti ti-download"></i> {t('browser.install')}
                      </button>
                    </div>
                  </div>
                {/each}
              {/if}
            </div>
          {/if}
        </div>
        {/key}
      </div>

      <!-- ═══ ПРАВА КОЛОНКА: install-картка + сайдбар, sticky-блок ═══
           Одна колонка, яка не розсипається на дрібні шматки: кнопка
           встановлення завжди під рукою поруч з тим, що читає користувач,
           решта інформації — під нею, впорядкованими секціями. -->
      <aside class="mp-side">
        <!-- Install rail: статус + версія + головна кнопка. Панель вибору
             версії показується ЛИШЕ для модпаків (модпак встановлюється як
             ОКРЕМА збірка — версія тут необхідна одразу). Для контенту
             (моди/ресурспаки/шейдери/датапаки) вибір версії живе всередині
             діалогу встановлення — тут лише статус автопідбору. -->
        <div class="mp-install-card" style="--tacc:{acc.color}; --tacc-rgb:{acc.rgb}">
          <div class="mp-install-head">
            <i class="ti {acc.icon}"></i>
            <span>{typeLabel(detail.projectType)}</span>
          </div>

          {#if detail.projectType === 'modpack'}
            <div class="mp-version-pick">
              {#if loadingVersions}
                <span class="mp-v-loading"><i class="ti ti-loader spin"></i> {t('browser.project.loading')}</span>
              {:else if versions.length === 0}
                <span class="mp-v-empty">{t('browser.update.noVersions')}</span>
              {:else}
                {#if mcFilterOptions.length > 1}
                  <div class="mp-v-filters">
                    <SelectField
                      value={mcFilter}
                      options={[{ value: '', label: t('browser.project.gameVersion') }, ...mcFilterOptions.map(mcv => ({ value: mcv, label: mcv }))]}
                      onchange={(v) => { mcFilter = v; autoMatched = false; applyRecommendedSelection() }}
                      minWidth="100%"
                      ariaLabel={t('browser.project.gameVersion')}
                    />
                    <SelectField
                      value={loaderFilter}
                      options={[{ value: '', label: t('browser.project.platform') }, ...platformOptions.slice(1).map(ld => ({ value: ld, label: loaderNames[ld] ?? ld }))]}
                      onchange={(v) => { loaderFilter = v; autoMatched = false; applyRecommendedSelection() }}
                      minWidth="100%"
                      ariaLabel={t('browser.project.platform')}
                    />
                  </div>
                  {#if showVersionsCheckbox}
                    <label class="mp-v-all" title={t('browser.project.showAllVersions')}>
                      <input type="checkbox" bind:checked={showAllVersions} />
                      <span>{t('browser.project.showAllVersions')}</span>
                    </label>
                  {/if}
                {/if}
                {#if filteredVersions.length === 0}
                  <span class="mp-v-empty">{t('browser.update.noVersions')}</span>
                {:else}
                  <SelectField
                    value={selectedVersionId}
                    options={filteredVersions.map(v => ({
                      value: v.id,
                      label: `${v.name || v.versionNumber} · ${mcLabelFor(v)}${loaderLabelFor(v) ? ' · ' + loaderLabelFor(v) : ''}`
                    }))}
                    onchange={(v) => { selectedVersionId = v; autoMatched = false }}
                    minWidth="100%"
                    ariaLabel={t('browser.project.tab.versions')}
                  />
                {/if}
              {/if}
            </div>

            {#if selectedVersion()}
              <div class="mp-version-detail">
                {#if loaderIconFor(selectedVersion()!)}
                  <img class="mp-vd-ic" src={loaderIconFor(selectedVersion()!)} alt="" />
                {/if}
                <span><i class="ti ti-cube"></i> {mcLabelFor(selectedVersion()!)}</span>
                {#if loaderLabelFor(selectedVersion()!)}
                  <span><i class="ti ti-plug"></i> {loaderLabelFor(selectedVersion()!)}</span>
                {/if}
                {#if selectedVersion()!.fileSize}
                  <span class="mp-vd-size"><i class="ti ti-file"></i> {formatSize(selectedVersion()!.fileSize)}</span>
                {/if}
                {#if selectedVersion()!.versionType === 'release'}
                  <span class="chip g">{t('browser.project.recommended')}</span>
                {/if}
              </div>
            {/if}
          {:else if packContext && packUnsupported}
            <!-- Збірка-фільтр НЕ підтримує цей контент — явний стан замість
                 порожнечі: попередження + дії (інша збірка / прибрати
                 фільтр). Кнопка встановлення нижче заблокована. -->
            <div class="mp-unsup">
              <i class="ti ti-alert-triangle"></i>
              <div class="mp-unsup-main">
                <div class="mp-unsup-t">{t('browser.project.notSupportedForPack', { pack: packContext.name })}</div>
                <div class="mp-unsup-s">{packContext.mcVersion} · {loaderNames[packContext.loader] ?? packContext.loader}</div>
                {#if onChangePackContext}
                  <div class="mp-unsup-acts">
                    <button class="btn btn-ghost btn-sm" onclick={() => packSwitcherOpen = true}><i class="ti ti-replace"></i> {t('browser.project.changePack')}</button>
                    <button class="btn btn-ghost btn-sm" onclick={clearPackContext}><i class="ti ti-x"></i> {t('browser.project.clearPack')}</button>
                  </div>
                {/if}
              </div>
            </div>
          {:else if packContext && autoMatched && selectedVersion()}
            <div class="mp-auto-badge">
              {#if ctxServerVisual}
                {@render targetIcon(ctxServerVisual)}
              {:else}
                <i class="ti ti-sparkles"></i>
              {/if}
              <span class="mp-auto-badge-txt" title={t('browser.project.autoMatched', { mc: packContext.mcVersion, loader: packContext.loader })}>{t('browser.project.matchForPack', { pack: packContext.name })}</span>
              {#if onChangePackContext}
                <button class="mp-badge-action" onclick={openPackSwitcher} title={t('browser.project.changePack')}><i class="ti ti-replace"></i></button>
                <button class="mp-badge-action" onclick={clearPackContext} title={t('browser.project.clearPack')}><i class="ti ti-x"></i></button>
              {/if}
            </div>
          {:else if packContext}
            <div class="mp-target-badge" style={ctxServerVisual ? `--pcol:${ctxServerVisual.color}` : ''}>
              {#if ctxServerVisual}
                {@render targetIcon(ctxServerVisual)}
              {:else}
                <i class="ti ti-target-arrow"></i>
              {/if}
              <span>{packContext.name}</span>
              <span class="mp-target-sub">{packContext.mcVersion} · {loaderNames[packContext.loader] ?? packContext.loader}</span>
              {#if onChangePackContext}
                <button class="mp-badge-action" onclick={openPackSwitcher} title={t('browser.project.changePack')}><i class="ti ti-replace"></i></button>
                <button class="mp-badge-action" onclick={clearPackContext} title={t('browser.project.clearPack')}><i class="ti ti-x"></i></button>
              {/if}
            </div>
          {:else if onChangePackContext && (packs.length > 0 || canPickServer) && detail.projectType !== 'modpack'}
            <!-- Без фільтра, але є збірки — даємо швидко ОБРАТИ його прямо
                 тут, а не лише при кліку «Встановити» (те саме, що радіо
                 «Без фільтра» / вибір збірки в сайдбарі ContentBrowser,
                 перенесене на сторінку контенту). -->
            <button class="mp-target-badge mp-target-badge-empty" onclick={openPackSwitcher}>
              <i class="ti ti-target-arrow"></i>
              <span>{t('browser.project.pickPackFilter')}</span>
            </button>
          {/if}

          <button class="btn btn-primary btn-lg mp-dl-btn" onclick={primaryInstall} disabled={installing || (detail.projectType !== 'modpack' ? false : !selectedVersion()) || (!!packContext && packUnsupported && detail.projectType !== 'modpack')}>
            {#if installing}
              <i class="ti ti-loader spin"></i> {t('browser.project.downloading')}
            {:else if packContext && packUnsupported && detail.projectType !== 'modpack'}
              <i class="ti ti-alert-triangle"></i> {t('browser.project.notSupported')}
            {:else if installedItem && hasNewerSelected()}
              <i class="ti ti-refresh"></i> {t('browser.update.install', { v: selectedVersion()?.versionNumber || '' })}
            {:else if installedItem}
              <i class="ti ti-check"></i> {t('browser.installed.short')}
            {:else}
              <i class="ti ti-download"></i> {primaryActionLabel(detail.projectType)}
            {/if}
          </button>
          {#if installProgress > 0 && installProgress < 100}
            <div class="mp-progress">
              <div class="mp-progress-bar"><i style={`width:${installProgress}%`}></i></div>
              <span>{installProgress}%</span>
            </div>
          {/if}
          {#if !packContext && detail.projectType !== 'modpack'}
            <div class="mp-install-hint"><i class="ti ti-info-circle"></i> {t('browser.choosePack')}</div>
          {/if}
        </div>

        <!-- «Також встановлено у: ...» — компактний список збірок, де вже
             стоїть цей контент. Згорнутий за замовчуванням (лише перші 3 +
             лічильник решти), щоб не займати багато місця в колонці —
             розгортається по кліку. Не показуємо для модпаків (нема сенсу)
             і не показуємо порожній «завантажується» стан (лоадер сюди не
             вставляємо навмисно: перший рендер сторінки не має блимати). -->
        {#if detail.projectType !== 'modpack' && installedInPacks.length > 0}
          <div class="mp-side-sec mp-installed-packs">
            <div class="mp-side-title"><i class="ti ti-folder-check"></i> {t('browser.project.sidebar.installedIn')}</div>
            <div class="mp-installed-list">
              {#each (installedInPacksExpanded ? installedInPacks : installedInPacks.slice(0, 3)) as ip (ip.packId)}
                {@const ipPack = packs.find((p: { id: string; name: string; mcVersion: string; loader: string; iconUrl?: string; color?: string }) => p.id === ip.packId)}
                {@const ipPcol = ipPack?.color || 'var(--orange)'}
                {@const ipIcon = packAsset(ipPack?.iconUrl)}
                <button class="mp-installed-row" style="--pcol:{ipPcol}" onclick={() => onOpenPack?.(ip.packId)} title={ip.packName}>
                  <span class="mp-installed-ic" class:has-icon={!!ipIcon}>
                    {#if ipIcon}<img src={ipIcon} alt="" loading="lazy" />{:else}<i class="ti ti-puzzle"></i>{/if}
                  </span>
                  <div class="mp-installed-info">
                    <span class="mp-installed-name">{ip.packName}</span>
                    {#if ip.versionNumber}<span class="mp-installed-ver">{ip.versionNumber}</span>{/if}
                  </div>
                </button>
              {/each}
            </div>
            {#if !installedInPacksExpanded && installedInPacks.length > 3}
              <button class="mp-installed-more" onclick={() => installedInPacksExpanded = true}>
                {t('browser.project.sidebar.installedInMore', { count: installedInPacks.length - 3 })}
              </button>
            {/if}
          </div>
        {/if}

        {#if detail.clientSide || detail.serverSide}
          <div class="mp-side-sec">
            <div class="mp-side-title"><i class="ti ti-shield-check"></i> {t('browser.project.sidebar.compatibility')}</div>
            {#if detail.clientSide}
              <div class="mp-side-row">
                <span>{t('browser.project.sidebar.clientSide')}</span>
                <span class="mp-side-val" class:on={detail.clientSide === 'required'}>{sideLabel(detail.clientSide)}</span>
              </div>
            {/if}
            {#if detail.serverSide}
              <div class="mp-side-row">
                <span>{t('browser.project.sidebar.serverSide')}</span>
                <span class="mp-side-val" class:on={detail.serverSide === 'required'}>{sideLabel(detail.serverSide)}</span>
              </div>
            {/if}
          </div>
        {/if}

        {#if detail.published || detail.updated || detail.licenseName}
          <div class="mp-side-sec">
            <div class="mp-side-title"><i class="ti ti-info-circle"></i> {t('browser.project.sidebar.details')}</div>
            {#if detail.published}
              <div class="mp-side-row">
                <span>{t('browser.project.sidebar.published')}</span>
                <span class="mp-side-val">{formatDate(detail.published)}</span>
              </div>
            {/if}
            {#if detail.updated}
              <div class="mp-side-row">
                <span>{t('browser.project.sidebar.updated')}</span>
                <span class="mp-side-val">{formatDate(detail.updated)}</span>
              </div>
            {/if}
            {#if detail.licenseName}
              <div class="mp-side-row">
                <span>{t('browser.project.sidebar.license')}</span>
                {#if detail.licenseUrl}
                  <a class="mp-side-val mp-side-val-link" href={detail.licenseUrl} target="_blank" rel="noreferrer">{detail.licenseName}</a>
                {:else}
                  <span class="mp-side-val">{detail.licenseName}</span>
                {/if}
              </div>
            {/if}
          </div>
        {/if}

        {#if detail.author}
          <div class="mp-side-sec">
            <div class="mp-side-title"><i class="ti ti-user"></i> {t('browser.project.sidebar.author')}</div>
            {#if detail.authorId && onAuthor}
              <button class="mp-author-row" onclick={() => onAuthor!(detail!.authorId!, detail!.author || detail!.name)}>
                {#if detail.authorAvatar}
                  <img class="mp-author-avatar" src={detail.authorAvatar} alt="" onerror={(e) => ((e.currentTarget as HTMLElement).style.display = 'none')} />
                {:else}
                  <span class="mp-author-avatar mp-author-avatar-fallback"><i class="ti ti-user"></i></span>
                {/if}
                <span class="mp-author-name">{detail.author}</span>
                <i class="ti ti-chevron-right"></i>
              </button>
            {:else}
              <div class="mp-side-row"><span class="mp-side-val">{detail.author}</span></div>
            {/if}
          </div>
        {/if}

        {#if detail.issuesUrl || detail.sourceUrl || detail.discordUrl || detail.wikiUrl}
          <div class="mp-side-sec">
            <div class="mp-side-title"><i class="ti ti-link"></i> {t('browser.project.sidebar.links')}</div>
            <div class="mp-side-links">
              {#if detail.issuesUrl}
                <a class="mp-side-link" href={detail.issuesUrl} target="_blank" rel="noreferrer"><i class="ti ti-bug"></i> {t('browser.project.sidebar.issues')}</a>
              {/if}
              {#if detail.sourceUrl}
                <a class="mp-side-link" href={detail.sourceUrl} target="_blank" rel="noreferrer"><i class="ti ti-code"></i> {t('browser.project.sidebar.source')}</a>
              {/if}
              {#if detail.discordUrl}
                <a class="mp-side-link" href={detail.discordUrl} target="_blank" rel="noreferrer"><i class="ti ti-brand-discord"></i> {t('browser.project.sidebar.discord')}</a>
              {/if}
              {#if detail.wikiUrl}
                <a class="mp-side-link" href={detail.wikiUrl} target="_blank" rel="noreferrer"><i class="ti ti-book"></i> {t('browser.project.sidebar.wiki')}</a>
              {/if}
            </div>
          </div>
        {/if}

        {#if (detail.categories ?? []).length > 0}
          <div class="mp-side-sec">
            <div class="mp-side-title"><i class="ti ti-tags"></i> {t('browser.project.sidebar.tags')}</div>
            <div class="mp-side-tags">
              {#each detail.categories as cat}
                <span class="ib-tag">{cat}</span>
              {/each}
            </div>
          </div>
        {/if}
      </aside>
    </div>
  {/if}
</div>

<!-- Лайтбокс галереї (повноекранний, як у Modrinth app): зум колесом,
     панорама перетягуванням, prev/next стрілками на екрані і клавіатурою. -->
{#if lightboxIndex !== null && detail && currentGalleryImage()}
  <div
    class="lb-overlay"
    oncontextmenu={(e) => e.preventDefault()}
    onclick={(e) => { if (e.target === e.currentTarget) closeLightbox() }}
  >
    <div class="lb-toolbar">
      <span class="lb-count">{t('browser.gallery.of', { current: lightboxIndex! + 1, total: detail.gallery.length })}</span>
      <div class="lb-tools">
        <button class="lb-btn" title={t('browser.gallery.zoomOut')} onclick={() => lightboxZoom = Math.max(0.5, lightboxZoom - 0.25)}><i class="ti ti-zoom-out"></i></button>
        <button class="lb-btn" title={t('browser.gallery.resetZoom')} onclick={resetLightboxZoom}><i class="ti ti-zoom-reset"></i></button>
        <button class="lb-btn" title={t('browser.gallery.zoomIn')} onclick={() => lightboxZoom = Math.min(4, lightboxZoom + 0.25)}><i class="ti ti-zoom-in"></i></button>
        <button class="lb-btn" title={t('browser.gallery.close')} onclick={closeLightbox}><i class="ti ti-x"></i></button>
      </div>
    </div>

    <button class="lb-nav lb-prev" title={t('browser.gallery.prev')} onclick={prevLightbox}><i class="ti ti-chevron-left"></i></button>

    <div class="lb-stage" onwheel={onLightboxWheel}>
      <img
        class="lb-img"
        src={currentGalleryImage()!.rawUrl || currentGalleryImage()!.url}
        alt={currentGalleryImage()!.title || ''}
        style={`transform: translate(${lightboxPan.x}px, ${lightboxPan.y}px) scale(${lightboxZoom}); cursor:${lightboxZoom > 1 ? 'grab' : 'default'}`}
        onmousedown={onLightboxDragStart}
        onmousemove={onLightboxDragMove}
        onmouseup={onLightboxDragEnd}
        onmouseleave={onLightboxDragEnd}
        ondblclick={(e) => { e.preventDefault(); lightboxZoom = lightboxZoom > 1 ? 1 : 2.5; if (lightboxZoom === 1) lightboxPan = { x: 0, y: 0 } }}
      />
    </div>

    <button class="lb-nav lb-next" title={t('browser.gallery.next')} onclick={nextLightbox}><i class="ti ti-chevron-right"></i></button>

    {#if currentGalleryImage()!.title || lightboxIndex !== null}
      <div class="lb-caption">
        {#if currentGalleryImage()!.title}<b>{currentGalleryImage()!.title}</b>{/if}
        <span class="lb-hint"><i class="ti ti-hand-move"></i> {t('browser.gallery.dragHint')}</span>
      </div>
    {/if}
  </div>
{/if}

<!-- Діалог вибору збірки (мод/ресурспак/шейдер без фільтра) -->
{#snippet packDialogRowActions(pid: string)}
  {@const p = packDialogCompatibleById.get(pid)}
  {#if p}
    {#if p.alreadyInstalled}
      <button class="btn btn-ghost btn-sm" disabled><i class="ti ti-check"></i> {t('browser.installed.short')}</button>
    {:else}
      <button class="btn btn-primary btn-sm" onclick={() => installToPickedPack(pid)} disabled={installing}>
        <i class="ti ti-download"></i> {t('browser.install')}
      </button>
    {/if}
  {/if}
{/snippet}

{#snippet targetIcon(v: { src?: string; ti: string; color: string })}
  <span class="mp-tgt-ic" class:has-img={!!v.src} style="--pcol:{v.color}">
    {#if v.src}<img src={v.src} alt="" />{:else}<i class="ti {v.ti || 'ti-server-2'}"></i>{/if}
  </span>
{/snippet}

{#if packDialog}
  <UiModal
    open={true}
    title={detail?.name ?? ''}
    subtitle={t('browser.choosePack')}
    icon="ti-package"
    color="var(--tb-c, var(--orange))"
    width="520px"
    onclose={() => (packDialog = null)}
  >
      {#if canPickServer}
        <!-- Ціль встановлення: і збірки, і сервери (лише ті, що підтримують
             цей вид контенту). -->
        <div class="mp-target-tabs" role="tablist">
          <button type="button" role="tab" aria-selected={dialogTab === 'packs'} class="mp-target-tab" class:on={dialogTab === 'packs'} onclick={() => (dialogTab = 'packs')}>
            <i class="ti ti-box"></i> {t('browser.target.packs')}
          </button>
          <button type="button" role="tab" aria-selected={dialogTab === 'servers'} class="mp-target-tab" class:on={dialogTab === 'servers'} onclick={() => (dialogTab = 'servers')}>
            <i class="ti ti-server"></i> {t('browser.target.servers')}
          </button>
        </div>
      {/if}
      {#if dialogTab === 'servers' && canPickServer}
        <ServerTargetList
          servers={servers}
          tab={detail?.projectType ?? 'mod'}
          selectedId=""
          onpick={installToPickedServer}
          variant="row"
          name="mp-dlg-target"
        />
      {:else}
      {#if !packDialog.loading && packDialog.packs.length > 0}
        <div class="cb-modal-search">
          <i class="ti ti-search"></i>
          <input placeholder={t('browser.searchPack')} bind:value={packDialogFilter} />
        </div>
      {/if}
        {#if packDialog.loading}
          <div class="cb-loading sm"><i class="ti ti-loader spin"></i> {t('browser.compatChecking')}</div>
        {:else}
          {#if packDialog.packs.length === 0}
            <div class="cb-empty sm"><i class="ti ti-mood-sad"></i><div>{t('browser.noCompatiblePacks')}</div></div>
          {:else if filteredPackDialogPacks.length === 0}
            <div class="cb-empty sm"><i class="ti ti-search-off"></i><div>{t('browser.noPackMatch')}</div></div>
          {:else}
            {#if packPicker && packPicker.items.length > 0}
              <PackPicker
                items={packPicker.items}
                rowById={packPicker.rowById}
                folderById={packPicker.folderById}
                query={packDialogFilter}
                allowedIds={packDialogAllowedIds}
                selectable={false}
                subtitle={(r) => `${r.loader ?? ''} · ${r.mcVersion ?? ''}`}
                rowActions={packDialogRowActions}
                class="mp-pp"
              />
            {:else}
              <div class="cb-pack-list">
                {#each filteredPackDialogPacks as p}
                  {@const picon = packAsset(p.iconUrl)}
                  <div class="cb-pack-row" class:installed={p.alreadyInstalled} style="--pcol:{p.color || 'var(--orange)'}">
                    <div class="cb-pack-info">
                      <span class="cb-pack-ic" class:has-icon={!!picon}>
                        {#if picon}<img src={picon} alt="" loading="lazy" />{:else}<i class="ti {p.icon || 'ti-puzzle'}"></i>{/if}
                      </span>
                      <div>
                        <div class="cb-pack-name">{p.name}</div>
                        <div class="cb-pack-meta">{p.loader} · {p.mcVersion}</div>
                        {#if p.alreadyInstalled}
                          <div class="cb-warn"><i class="ti ti-alert-triangle"></i> {t('browser.alreadyInstalled', { file: p.installedFileName ?? p.name })}</div>
                        {/if}
                      </div>
                    </div>
                    {#if p.alreadyInstalled}
                      <button class="btn btn-ghost btn-sm" disabled><i class="ti ti-check"></i> {t('browser.installed.short')}</button>
                    {:else}
                      <button class="btn btn-primary btn-sm" onclick={() => installToPickedPack(p.packId)} disabled={installing}>
                        <i class="ti ti-download"></i> {t('browser.install')}
                      </button>
                    {/if}
                  </div>
                {/each}
              </div>
            {/if}
          {/if}
        {/if}
      {/if}
  </UiModal>
{/if}

{#if packSwitcherOpen}
  <!-- Перемикач цілі встановлення: Збірки ↔ Сервери. Збірки — ПОВНИЙ список
       (без обмеження сумісністю, на відміну від packDialog вище); сервери —
       лише ті, що підтримують цей вид контенту (моди/плагіни), з реальними
       іконками/кольорами і фільтром за типом та версією сервера. -->
  <UiModal
    open={true}
    title={t('browser.installTarget')}
    icon="ti-target-arrow"
    color="var(--tb-c, var(--orange))"
    width="480px"
    onclose={() => { packSwitcherOpen = false; packSwitcherFilter = '' }}
  >
    {#if canPickServer}
      <div class="mp-target-tabs" role="tablist">
        <button type="button" role="tab" aria-selected={switcherTab === 'packs'} class="mp-target-tab" class:on={switcherTab === 'packs'} onclick={() => (switcherTab = 'packs')}>
          <i class="ti ti-box"></i> {t('browser.target.packs')}
        </button>
        <button type="button" role="tab" aria-selected={switcherTab === 'servers'} class="mp-target-tab" class:on={switcherTab === 'servers'} onclick={() => (switcherTab = 'servers')}>
          <i class="ti ti-server"></i> {t('browser.target.servers')}
        </button>
      </div>
    {/if}

    {#if switcherTab === 'servers' && canPickServer}
      <ServerTargetList
        servers={servers}
        tab={detail?.projectType ?? 'mod'}
        selectedId={packContext?.isServer ? packContext.id : ''}
        onpick={pickServerContext}
        variant="row"
        name="mp-target"
      />
    {:else}
      {#if packs.length > 4}
        <div class="cb-modal-search">
          <i class="ti ti-search"></i>
          <input placeholder={t('browser.searchPack')} bind:value={packSwitcherFilter} />
        </div>
      {/if}
      {#if packPicker && packPicker.items.length > 0}
        <PackPicker
          items={packPicker.items}
          rowById={packPicker.rowById}
          folderById={packPicker.folderById}
          query={packSwitcherFilter}
          allowedIds={packSwitcherAllowed}
          selected={packContext && !packContext.isServer ? packContext.id : ''}
          onselect={(id) => {
            const p = packs.find((x: { id: string; name: string; mcVersion: string; loader: string; iconUrl?: string; color?: string }) => x.id === id)
            if (p) pickPackContext(p)
          }}
          subtitle={(r) => `${r.loader ?? ''} · ${r.mcVersion ?? ''}`}
          class="mp-pp"
        />
      {:else if filteredPackSwitcherPacks.length === 0}
        <div class="cb-empty sm"><i class="ti ti-search-off"></i><div>{t('browser.noPackMatch')}</div></div>
      {:else}
        <div class="cb-pack-list">
          {#each filteredPackSwitcherPacks as p (p.id)}
            {@const picon = packAsset(p.iconUrl)}
            <div class="cb-pack-row" class:installed={!packContext?.isServer && p.id === packContext?.id} style="--pcol:{p.color || 'var(--orange)'}" onclick={() => pickPackContext(p)} role="button" tabindex="0">
              <div class="cb-pack-info">
                <span class="cb-pack-ic" class:has-icon={!!picon}>
                  {#if picon}<img src={picon} alt="" loading="lazy" />{:else}<i class="ti {p.icon || 'ti-puzzle'}"></i>{/if}
                </span>
                <div>
                  <div class="cb-pack-name">{p.name}</div>
                  <div class="cb-pack-meta">{p.loader} · {p.mcVersion}</div>
                </div>
              </div>
              {#if !packContext?.isServer && p.id === packContext?.id}
                <i class="ti ti-check" style="color:var(--tacc, var(--orange))"></i>
              {/if}
            </div>
          {/each}
        </div>
      {/if}
    {/if}
  </UiModal>
{/if}

{#if worldDialog}
  {@const dlg = worldDialog}
  <UiModal
    open={true}
    title={detail?.name ?? ''}
    subtitle={t('browser.world.chooseFor', { pack: dlg.packName })}
    icon="ti-world"
    color="var(--tb-c, var(--orange))"
    width="480px"
    onclose={() => (worldDialog = null)}
  >
    {#if worldDialog.loading}
      <div class="cb-loading sm"><i class="ti ti-loader spin"></i> {t('browser.compatChecking')}</div>
    {:else if worldDialog.worlds.length === 0}
      <div class="cb-empty sm"><i class="ti ti-mood-sad"></i><div>{t('browser.world.noWorlds')}</div></div>
      <div class="cb-hint"><i class="ti ti-info-circle"></i><span>{t('browser.world.noWorldsHint')}</span></div>
    {:else}
      <div class="cb-world-toolbar">
        <span class="cb-world-count">{t('browser.world.selected', { n: worldDialog.selectedWorlds.length, total: worldDialog.worlds.length })}</span>
        <div class="cb-world-quick">
          <button class="btn btn-ghost btn-sm" onclick={() => selectAllWorlds(true)} disabled={worldDialog.selectedWorlds.length === worldDialog.worlds.length}>
            {t('browser.world.selectAll')}
          </button>
          <button class="btn btn-ghost btn-sm" onclick={() => selectAllWorlds(false)} disabled={worldDialog.selectedWorlds.length === 0}>
            {t('browser.world.selectNone')}
          </button>
        </div>
      </div>
      <div class="cb-world-list">
        {#each worldDialog.worlds as w}
          <label class="cb-world-row" class:sel={worldSelected(w)}>
            <input type="checkbox" bind:group={worldDialog.selectedWorlds} value={w} />
            <i class="ti ti-world"></i>
            <span class="cb-world-name">{w}</span>
          </label>
        {/each}
      </div>
    {/if}
    <label class="cb-world-global">
      <input type="checkbox" bind:checked={worldGlobal} />
      <i class="ti ti-folder-data"></i>
      <span>{t('browser.world.installGlobal')}</span>
    </label>
    {#snippet footer()}
      <button class="btn btn-ghost" onclick={() => worldDialog = null}>{t('browser.alreadyInstalled.cancel')}</button>
      <button class="btn btn-primary" onclick={installDatapackToWorld} disabled={(!dlg.selectedWorlds.length && !worldGlobal) || installing || dlg.loading}>
        {#if installing}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-download"></i>{/if}
        {worldGlobal ? (dlg.selectedWorlds.length ? t('browser.world.installGlobalAndWorlds') : t('browser.world.installGlobalBtn')) : installBtnLabel(dlg.selectedWorlds.length)}
      </button>
    {/snippet}
  </UiModal>
{/if}

{#if alreadyInstalledDialog}
  <UiModal
    open={true}
    title={t('browser.alreadyInstalled.title')}
    icon="ti-alert-triangle"
    color="var(--orange)"
    width="420px"
    onclose={() => (alreadyInstalledDialog = null)}
  >
    <div class="cb-hint">
      <i class="ti ti-info-circle"></i>
      <span>{t('browser.alreadyInstalled.message', { name: alreadyInstalledDialog.packName })}</span>
    </div>
    {#snippet footer()}
      <button class="btn btn-ghost" onclick={() => alreadyInstalledDialog = null}>
        <i class="ti ti-x"></i> {t('browser.alreadyInstalled.cancel')}
      </button>
      {#if onOpenPack}
        <button class="btn btn-secondary" onclick={goToExistingPack}>
          <i class="ti ti-arrow-right"></i> {t('browser.alreadyInstalled.goToPack')}
        </button>
      {/if}
      <button class="btn btn-primary" onclick={createCopyAnyway} disabled={installing}>
        {#if installing}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-plus"></i>{/if}
        {t('browser.alreadyInstalled.createCopy')}
      </button>
    {/snippet}
  </UiModal>
{/if}

<style>
  /* ═════════════════════════════════════════════════════════════════════
     СТОРІНКА ПРОЄКТУ (ModpackPage) — переписано з нуля.
     Принципи макету:
       • тип ресурсу (модпак/мод/ресурспак/шейдер/датапак) — головний
         візуальний ідентифікатор: власна іконка + колір у бейджі банера,
         topbar, install-картці — сторінка читається з першого погляду;
       • права колонка — ОДНА нерозривна стрічка (install-картка + всі
         секції сайдбару), а не розкидані панелі — головна дія завжди
         під рукою, sticky;
       • сітка 8px, консистентні радіуси/тіні/переходи через локальні
         токени, без магічних чисел по всьому файлу;
       • акуратні мікроанімації при появі — не відволікають, підказують
         ієрархію (банер → інсталяція → вкладки → контент).
     ═════════════════════════════════════════════════════════════════════ */

  /* ── Локальні токени ── */
  .modal {
    border-radius: var(--mp-radius-lg, 18px);
  }
  /* .mp-wrap тепер займає ВСЮ висоту .page-view (див. style.css:
     .content.content-no-scroll > .page-view > .mp-wrap { flex:1;
     min-height:0 }) — сторінка проєкту більше НЕ живе в скрол-контейнері
     .content разом з рештою UI. Це і є передумова для справді окремого,
     незалежного правого блоку: .mp-main ділить цю фіксовану висоту на
     дві колонки, кожна зі своїм overflow-y:auto (.mp-col зліва,
     .mp-side справа) — вони скроляться абсолютно незалежно одна від
     одної, без sticky-хаків і без спільного зовнішнього скролбару. */
  .mp-wrap {
    --mp-gap: 16px;
    --mp-radius-lg: 18px;
    --mp-radius-md: 12px;
    --mp-radius-sm: 8px;
    --mp-shadow-card: 0 8px 22px -14px rgba(0, 0, 0, .55);
    --mp-shadow-strong: 0 20px 50px -20px rgba(0, 0, 0, .7);
    --mp-ease: cubic-bezier(.22, 1, .36, 1);
    --mp-ease-spring: cubic-bezier(.34, 1.56, .64, 1);
    --mp-accent: var(--pcol, var(--orange));

    display: flex; flex-direction: column;
    height: 100%; min-height: 0;
    animation: mp-page-in-opacity .38s var(--mp-ease) both;
  }
  @keyframes mp-page-in-opacity {
    from { opacity: 0; }
    to   { opacity: 1; }
  }

  /* Два незалежні блоки на всю висоту .mp-wrap, БЕЗ grid/sticky-звязку:
     flex-ряд фіксованої висоти, кожна колонка сама вирішує свій скрол.
     .mp-main більше не є контейнером, що визначає spring для sticky —
     він лише ділить ширину/висоту між .mp-col і .mp-side. */
  .mp-main {
    display: flex; align-items: stretch;
    gap: 20px;
    flex: 1; min-height: 0;
  }
  /* Ліва колонка (банер/вкладки/опис) — окремий скрол-контейнер: увесь
     контент, що раніше скролився разом з .content, тепер скролиться
     тут. mp-topbar лишається sticky відносно ЦЬОГО контейнера. */
  .mp-col {
    display: flex; flex-direction: column; gap: var(--mp-gap); min-width: 0;
    flex: 1; min-height: 0;
    overflow-y: auto; overflow-x: hidden;
    padding-right: 2px;
    overscroll-behavior: contain;
  }
  .mp-col::-webkit-scrollbar { width: 6px; }
  .mp-col::-webkit-scrollbar-thumb { background: var(--hover-strong); border-radius: 3px; }
  .mp-col::-webkit-scrollbar-thumb:hover { background: var(--border-hi); }
  .mp-col::-webkit-scrollbar-track { background: transparent; }

  /* Права панель (install-картка/сумісність/деталі/автор/посилання/теги)
     — СПРАВДІ окремий незалежний блок: фіксована ширина, висота 100%
     батька (.mp-main), власний overflow-y:auto. Ніякого position:sticky
     і ніякого align-items:stretch-звязку з висотою лівої колонки —
     .mp-side завжди займає рівно висоту сторінки і скролиться сам,
     незалежно від того, скільки контенту в описі зліва чи наскільки
     він проскрольований. */
  .mp-side {
    display: flex; flex-direction: column; gap: 12px;
    flex: 0 0 280px;
    align-self: stretch;
    overflow-y: auto; overflow-x: hidden;
    padding-right: 2px; /* місце під скролбар, щоб не наїжджав на контент */
    /* overscroll-behavior:contain — щоб скрол колесом усередині .mp-side,
       докрутивши до верху/низу, не «протікав» і не крутив ліву колонку
       чи сторінку — кожен блок крутиться суворо сам по собі. */
    overscroll-behavior: contain;
  }
  .mp-side::-webkit-scrollbar { width: 6px; }
  .mp-side::-webkit-scrollbar-thumb { background: var(--hover-strong); border-radius: 3px; }
  .mp-side::-webkit-scrollbar-thumb:hover { background: var(--border-hi); }
  .mp-side::-webkit-scrollbar-track { background: transparent; }

  /* ═════════════════════════════════════════════════════════════════════
     ВЕРХНЯ НАВІГАЦІЯ (sticky topbar)
     ═════════════════════════════════════════════════════════════════════ */
  .mp-topbar {
    position: sticky; top: -6px; z-index: 30;
    display: flex; align-items: center; justify-content: space-between;
    gap: 10px;
    padding: 9px 10px;
    margin: -4px -8px 4px;
    border-radius: 12px;
    background: color-mix(in srgb, var(--bg-panel) 50%, transparent);
    border: 1px solid transparent;
    backdrop-filter: blur(10px);
    -webkit-backdrop-filter: blur(10px);
    transition: background .2s, border-color .2s, box-shadow .2s;
  }
  .mp-topbar.scrolled {
    background: color-mix(in srgb, var(--bg-panel) 92%, transparent);
    border-color: var(--border);
    box-shadow: 0 12px 28px -18px rgba(0, 0, 0, .7);
  }
  .mp-topbar.collapsed .mp-tb-ic,
  .mp-topbar.collapsed .mp-tb-meta {
    display: none;
  }
  .mp-tb-left {
    display: flex; align-items: center; gap: 10px;
    min-width: 0;
  }
  .mp-tb-back {
    width: 32px; height: 32px; flex-shrink: 0;
    display: flex; align-items: center; justify-content: center;
    border-radius: 9px;
    background: var(--hover);
    border: 1px solid var(--border);
    color: var(--text);
    font-size: 15px; cursor: pointer;
    transition: background .15s, border-color .15s, transform .18s var(--mp-ease-spring);
  }
  .mp-tb-back:hover {
    background: var(--hover-strong);
    border-color: var(--border-hi);
    transform: translateX(-2px);
  }
  .mp-tb-ic {
    width: 30px; height: 30px; border-radius: 8px; overflow: hidden; flex-shrink: 0;
    background: var(--bg-card); border: 1px solid var(--border);
    display: flex; align-items: center; justify-content: center;
  }
  .mp-tb-ic img { width: 100%; height: 100%; object-fit: cover; }
  .mp-tb-ic.mp-ic-fallback { color: var(--tacc, var(--orange)); font-size: 15px; }
  .mp-tb-meta { min-width: 0; }
  .mp-tb-name {
    font-size: 12.5px; font-weight: 700; color: var(--text);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .mp-tb-sub {
    font-size: 10.5px; color: var(--text-mute);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .mp-tb-right { display: flex; align-items: center; gap: 8px; flex-shrink: 0; }
  .mp-external {
    display: inline-flex; align-items: center; gap: 7px;
    padding: 6px 12px; border-radius: 999px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    color: var(--text-mute);
    font: 700 11.5px/1 var(--font-ui);
    text-decoration: none; white-space: nowrap;
    transition:
      border-color .18s, color .18s, background .18s,
      transform .2s var(--mp-ease-spring);
  }
  .mp-external:hover {
    color: var(--orange);
    border-color: rgba(var(--orange-rgb), .45);
    background: rgba(var(--orange-rgb), .08);
    transform: translateY(-1px);
  }
  /* Меню «Додати в колекцію» в topbar: .ctx-menu (style.css) — глобально
     position:fixed з left/top у пікселях viewport, тут простіше
     прив'язати відносно кнопки через обгортку. */
  .mp-col-menu-wrap { position: relative; }
  .mp-col-menu { position: absolute !important; top: calc(100% + 6px); right: 0; left: auto !important; min-width: 220px; }
  .mp-col-menu-count {
    font-size: 10.5px; color: var(--text-mute);
    background: var(--bg); padding: 1px 7px; border-radius: 999px;
  }

  /* ── Стани завантаження / порожньо ── */
  .mp-loading, .mp-empty {
    display: flex; flex-direction: column;
    align-items: center; justify-content: center; gap: 12px;
    padding: 80px 0;
    color: var(--text-mute); font-size: 13px;
  }
  .mp-loading i { font-size: 32px; color: var(--orange); }
  .mp-empty i {
    font-size: 40px; color: var(--text-dim);
    filter: drop-shadow(0 8px 20px rgba(0, 0, 0, .35));
    animation: mp-float 4s ease-in-out infinite;
  }
  .mp-empty.sm { padding: 32px 0; font-size: 12px; }
  .mp-empty.sm i { font-size: 26px; animation: none; }
  @keyframes mp-float {
    0%, 100% { transform: translateY(0); }
    50%      { transform: translateY(-6px); }
  }

  /* ═════════════════════════════════════════════════════════════════════
     БАНЕР ПРОЄКТУ — головний геро-блок з ідентичністю типу ресурсу
     ═════════════════════════════════════════════════════════════════════ */
  .mp-banner {
    --pcol: var(--orange);
    position: relative; overflow: hidden;
    /* flex-shrink:0 — банер НІКОЛИ не стискається нижче природної висоти
       свого контенту. .mp-col — flex-колонка фіксованої висоти зі
       скролом, і без цього (flex-shrink:1 за замовчуванням), коли контент
       нижче (вкладки/опис) високий, flex-механіка стискала банер під
       його мінімальну висоту; overflow:hidden тоді відрізав нижній ряд
       (.mp-stats з лічильниками завантажень/підписників і кнопкою
       «Зберегти») — вони були змонтовані, але невидимі (відтворено
       вимірюванням геометрії: .mp-stats на 40+px нижче нижнього краю
       банера). Тепер банер займає свою природну висоту завжди, а зайве
       просто докручується скролом .mp-col. */
    flex-shrink: 0;
    /* Мінімальна висота НЕЗАЛЕЖНА від контенту (title/badges/stats):
       раніше банер мав лише padding, а фактичну висоту йому давав
       flex-вміст .mp-title. Поки картинка галереї (.mp-banner-art,
       завантажується через use:smartImage — прогресивно, без наперед
       відомих інтринзик-розмірів) домальовувалась чи поки .mp-title ще
       не встиг розкластись на першому кадрі анімації mp-hero-in, банер
       міг мати проміжну/нижчу за фінальну висоту — overflow:hidden тоді
       обрізав верх картинки посередині, а бейдж типу ресурсу (перший
       елемент у потоці) лишався притиснутим до нижнього краю цієї
       обрізаної області, з’являючись «поверх» банера без назви й
       статистики під ним (усе це технічно ВСЕРЕДИНІ банера, просто
       обрізане). min-height, що не залежить від того, скільки контенту
       вже змонтовано, — той самий підхід, що й фіксована висота
       hero-блоку сторінки аддона в GDLauncher-Carbon.
       Компактний і повноширинний банер мають різну «природну» висоту
       (менший ic 84px проти 104px, без/з статистикою внизу) — окремі
       min-height нижче (.mp-banner-full) точніші за одне спільне число. */
    min-height: 236px;
    border: 1px solid var(--border);
    border-radius: var(--mp-radius-lg);
    padding: 26px 28px 24px;
    background:
      radial-gradient(ellipse at top left,
        color-mix(in srgb, var(--tacc, var(--orange)) 20%, transparent), transparent 55%),
      radial-gradient(ellipse at bottom right,
        color-mix(in srgb, var(--pcol) 16%, transparent), transparent 55%),
      var(--bg-card);
    box-shadow: var(--mp-shadow-card);
    animation: mp-hero-in .55s var(--mp-ease) both;
  }
  @keyframes mp-hero-in {
    from { opacity: 0; transform: translateY(-6px); filter: blur(3px); }
    to   { opacity: 1; transform: none; filter: blur(0); }
  }
  .mp-banner-bg {
    position: absolute; inset: 0; overflow: hidden;
    /* z-index:0, ПІД усім контентом банера (.mp-type-badge/.mp-title/
       .mp-stats мають z-index:1) — але це рятує лише поки .mp-banner сам
       має коректну висоту. Без min-height вище цей шар міг «сплюснутись»
       разом із батьком до незавершеного проміжного розміру. */
    z-index: 0;
  }
  .mp-banner-bg::after {
    content: ''; position: absolute; inset: 0;
    background: linear-gradient(90deg,
      rgba(15, 15, 15, .92) 0%,
      rgba(15, 15, 15, .68) 45%,
      rgba(15, 15, 15, .32) 100%);
  }
  :global(html[data-theme="light"]) .mp-banner-bg::after {
    background: linear-gradient(90deg,
      rgba(255, 255, 255, .94) 0%,
      rgba(255, 255, 255, .72) 45%,
      rgba(255, 255, 255, .38) 100%);
  }
  .mp-banner-art {
    width: 100%; height: 100%; object-fit: cover;
    filter: blur(2px) saturate(1.05);
    transform: scale(1.08);
  }

  /* Бейдж типу ресурсу — перше, що читає око: іконка + назва типу у
     фірмовому кольорі. Один і той самий компонент, колір і текст
     змінюються з projectType (accentFor/typeLabel у скрипті). */
  .mp-type-badge {
    position: relative; z-index: 1;
    display: inline-flex; align-items: center; gap: 7px;
    padding: 5px 12px 5px 10px; margin-bottom: 14px;
    border-radius: 999px;
    background: color-mix(in srgb, var(--tacc, var(--orange)) 16%, var(--bg-card));
    border: 1px solid color-mix(in srgb, var(--tacc, var(--orange)) 42%, transparent);
    color: var(--tacc, var(--orange));
    font: 800 11px/1 var(--font-ui);
    text-transform: uppercase; letter-spacing: .5px;
    box-shadow: 0 4px 14px -6px color-mix(in srgb, var(--tacc, var(--orange)) 45%, transparent);
    animation: mp-slide-in .4s var(--mp-ease) both;
  }
  .mp-type-badge i { font-size: 13px; }

  .mp-title {
    position: relative; z-index: 1;
    display: flex; gap: 18px; align-items: flex-start;
  }
  .mp-ic {
    width: 84px; height: 84px; min-width: 84px;
    border-radius: var(--mp-radius-lg);
    overflow: hidden;
    box-shadow:
      0 10px 28px -10px rgba(0, 0, 0, .55),
      0 0 0 1px color-mix(in srgb, var(--tacc, var(--orange)) 35%, rgba(255,255,255,.06)),
      inset 0 0 0 1px rgba(255, 255, 255, .08);
    background: var(--bg-input);
  }
  .mp-ic img { width: 100%; height: 100%; object-fit: cover; }
  .mp-ic-fallback {
    display: flex; align-items: center; justify-content: center;
    font-size: 34px; color: #fff;
    background: linear-gradient(135deg, var(--tacc, var(--violet)), var(--pcol, var(--orange)));
  }
  .mp-meta { display: flex; flex-direction: column; gap: 8px; min-width: 0; flex: 1; }
  .mp-name {
    font: 900 25px/1.15 var(--font-ui);
    color: var(--text); letter-spacing: -.2px;
    text-shadow: 0 2px 12px rgba(0, 0, 0, .3);
    user-select: text; /* назва проєкту — текст, який хочуть копіювати */
  }
  .mp-tagline {
    font: 500 13px/1.5 var(--font-ui);
    color: var(--text-mute);
    max-width: 62ch;
    display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical;
    overflow: hidden;
    user-select: text; /* короткий опис — так само копіюється */
  }
  .mp-badges {
    display: flex; gap: 6px; flex-wrap: wrap; align-items: center;
    margin-top: 2px;
  }
  .mp-cat-row { display: flex; gap: 6px; flex-wrap: wrap; margin-top: 2px; }
  .badge-mc {
    display: inline-flex; align-items: center; gap: 5px;
    padding: 4px 10px; border-radius: 999px;
    background: color-mix(in srgb, var(--tacc, var(--orange)) 15%, transparent);
    border: 1px solid color-mix(in srgb, var(--tacc, var(--orange)) 40%, transparent);
    color: var(--tacc, var(--orange));
    font: 700 11px/1 var(--font-ui);
  }
  .badge-author {
    display: inline-flex; align-items: center; gap: 5px;
    padding: 4px 10px; border-radius: 999px;
    background: rgba(255, 255, 255, .06);
    border: 1px solid var(--border);
    color: var(--text-mute);
    font: 600 11px/1 var(--font-ui);
  }
  :global(html[data-theme="light"]) .badge-author { background: rgba(0, 0, 0, .04); }
  .ib-tag {
    padding: 4px 10px; border-radius: 999px;
    background: rgba(255, 255, 255, .06);
    border: 1px solid var(--border);
    color: var(--text-mute);
    font: 600 11px/1 var(--font-ui);
    transition: border-color .18s, color .18s;
  }
  :global(html[data-theme="light"]) .ib-tag { background: rgba(0, 0, 0, .04); }
  .ib-tag:hover { color: var(--text); border-color: var(--border-hi); }

  .mp-stats {
    position: relative; z-index: 1;
    display: flex; gap: 18px;
    margin-top: 18px; padding-top: 16px;
    border-top: 1px solid var(--border);
    font: 700 12.5px/1 var(--font-ui);
    color: var(--text-mute);
  }
  .mp-stats span { display: inline-flex; align-items: center; gap: 6px; }
  .mp-stats i { color: var(--tacc, var(--orange)); font-size: 14px; }

  /* ── Зірочка закладки: компактна кругла кнопка в topbar (sticky, завжди
     видима), повна кнопка з підписом у hero-банері. Той самий жовтий
     акцент (#f5c542), що й на картках ContentBrowser — впізнаваний стан
     «збережено» незалежно від того, де саме на екрані користувач бачить
     зірочку. ── */
  .mp-bookmark-btn {
    display: inline-flex; align-items: center; justify-content: center;
    width: 32px; height: 32px; flex-shrink: 0;
    border-radius: 999px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    color: var(--text-mute);
    cursor: pointer;
    font-size: 15px;
    transition: color .18s, border-color .18s, background .18s, transform .2s var(--mp-ease-spring);
  }
  .mp-bookmark-btn:hover {
    color: #f5c542;
    border-color: color-mix(in srgb, #f5c542 45%, var(--border));
    transform: translateY(-1px);
  }
  .mp-bookmark-btn.on {
    color: #f5c542;
    border-color: color-mix(in srgb, #f5c542 55%, var(--border));
    background: color-mix(in srgb, #f5c542 12%, var(--bg-card));
  }
  .mp-bookmark-btn:disabled { opacity: .6; cursor: default; transform: none; }
  .mp-bookmark-btn-hero {
    position: relative; z-index: 1;
    width: auto; height: auto; margin-left: auto;
    padding: 8px 14px; gap: 7px;
    font: 700 12px/1 var(--font-ui);
  }
  .mp-banner-full .mp-bookmark-btn-hero { margin-left: 0; }

  /* ── Банер на всю ширину (Налаштування → Вигляд) ── */
  .mp-banner-full {
    /* Тепер сестра .mp-main (обидва — прямі flex-column діти .mp-wrap),
       а не елемент усередині grid-ряду колонок — на всю ширину виходить
       природно, без grid-column: 1/-1. flex-shrink:0, щоб .mp-main
       (flex:1) не стискав банер, забираючи в нього висоту. */
    flex-shrink: 0;
    margin-bottom: var(--mp-gap);
    padding: 40px 34px 30px;
    min-height: 220px;
    display: flex; flex-direction: column; justify-content: flex-end;
  }
  .mp-banner-full::before {
    content: ''; position: absolute; inset: 0;
    background: radial-gradient(ellipse at 50% -30%, rgba(255,255,255,.14), transparent 60%);
    opacity: .5; pointer-events: none;
    animation: mp-banner-breathe 7s ease-in-out infinite;
  }
  @keyframes mp-banner-breathe {
    0%, 100% { opacity: .35; }
    50%      { opacity: .7; }
  }
  .mp-banner-full .mp-title { width: 100%; align-items: flex-end; }
  .mp-banner-full .mp-ic {
    width: 104px; height: 104px; min-width: 104px;
    border-radius: var(--mp-radius-lg);
    box-shadow:
      0 16px 40px -12px rgba(0, 0, 0, .65),
      0 0 0 1px color-mix(in srgb, var(--tacc, var(--orange)) 40%, rgba(255,255,255,.09)),
      inset 0 0 0 1px rgba(255, 255, 255, .12);
  }
  .mp-banner-full .mp-name {
    font-size: 32px; font-weight: 900;
    text-shadow: 0 4px 20px rgba(0, 0, 0, .5);
  }
  .mp-banner-full .mp-stats { border-top: none; margin-top: 0; padding-top: 0; }

  /* ═════════════════════════════════════════════════════════════════════
     ВКЛАДКИ (Опис / Галерея / Відео / Зміни / Версії)
     ═════════════════════════════════════════════════════════════════════ */
  .mp-tabs {
    position: relative; z-index: 2;
    /* Непрозора підложка (той самий принцип, що bg-darkSlate-800 на
       вкладках у GDLauncher-Carbon): вкладки йдуть ОДРАЗУ під банером у
       звичайному потоці документа, тож у нормальному стані вони й так не
       перекриваються з .mp-banner (overflow:hidden, власна min-height —
       див. коментар вище). Фон і z-index тут — друга, незалежна лінія
       захисту: якщо банер із будь-якої причини вийде за межі своєї
       min-height (напр. дуже довгий tagline/багато категорій розтягнуть
       flex-контент понад заданий мінімум), вкладки лишаються повністю
       читабельними й не «просвічуються» картинкою банера, замість того
       щоб мовчки покладатись на те, що сусідні блоки ніколи не
       перетнуться по висоті.
       Той самий колір фону, що й у батьківської колонки (.mp-col не
       задає власний background — успадковує сторінку), щоб перехід між
       банером і вкладками лишався візуально безшовним. */
    background: var(--bg);
    display: flex; gap: 2px; flex-wrap: wrap;
    border-bottom: 1px solid var(--border);
    padding: 0;
    animation: mp-slide-in .4s var(--mp-ease) .08s both;
  }
  @keyframes mp-slide-in {
    from { opacity: 0; transform: translateY(8px); }
    to   { opacity: 1; transform: none; }
  }
  .mp-tab {
    position: relative;
    display: inline-flex; align-items: center; gap: 8px;
    padding: 12px 16px;
    background: transparent; border: none;
    color: var(--text-mute);
    font: 700 12.5px/1 var(--font-ui);
    cursor: pointer;
    transition: color .18s ease;
  }
  .mp-tab::after {
    content: ''; position: absolute;
    left: 12px; right: 12px; bottom: -1px;
    height: 2px; border-radius: 2px 2px 0 0;
    background: var(--orange);
    transform: scaleX(0); transform-origin: center;
    transition: transform .3s var(--mp-ease);
  }
  .mp-tab:hover { color: var(--text); }
  .mp-tab:hover::after { transform: scaleX(.35); background: var(--text-mute); }
  .mp-tab.active {
    color: var(--orange);
  }
  .mp-tab.active::after {
    transform: scaleX(1);
    background: linear-gradient(90deg, var(--orange), var(--orange-2));
    box-shadow: 0 0 10px rgba(var(--orange-rgb), .5);
  }
  .mp-tab .num {
    font: 800 10px/1 var(--font-mono);
    color: var(--text-dim);
    background: var(--bg-input);
    border-radius: 999px;
    padding: 2px 7px;
  }
  .mp-tab.active .num {
    color: var(--orange);
    background: rgba(var(--orange-rgb), .12);
  }

  .mp-tab-content {
    padding: 6px 2px;
    animation: mp-tab-content-in .28s var(--mp-ease) both;
  }
  @keyframes mp-tab-content-in {
    from { opacity: 0; transform: translateY(8px); }
    to   { opacity: 1; transform: none; }
  }

  /* ═════════════════════════════════════════════════════════════════════
     ТІЛО ОПИСУ (Markdown renderer)
     ═════════════════════════════════════════════════════════════════════ */
  .mp-body {
    font: 500 13.5px/1.75 var(--font-ui);
    color: var(--text);
    overflow-wrap: break-word;
    padding: 20px 22px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--mp-radius-lg);
    /* Глобально user-select:none (щоб UI не виділявся випадково при
       кліках/драгах) — але опис проєкту це ЗМІСТ, який люди хочуть
       копіювати (команди, конфіги, посилання). Повертаємо виділення. */
    user-select: text;
  }
  .mp-body :global(h1),
  .mp-body :global(h2),
  .mp-body :global(h3),
  .mp-body :global(h4),
  .mp-body :global(h5),
  .mp-body :global(h6) {
    color: var(--text);
    margin: 22px 0 10px;
    font-weight: 700; letter-spacing: -.1px;
  }
  .mp-body :global(h1:first-child),
  .mp-body :global(h2:first-child),
  .mp-body :global(h3:first-child) { margin-top: 0; }
  .mp-body :global(h1) { font-size: 22px; padding-bottom: 8px; border-bottom: 1px solid var(--border); }
  .mp-body :global(h2) { font-size: 18px; padding-bottom: 6px; border-bottom: 1px solid var(--border); }
  .mp-body :global(h3) { font-size: 16px; }
  .mp-body :global(h4) { font-size: 14px; }
  .mp-body :global(p) { margin: 10px 0; }
  .mp-body :global(ul),
  .mp-body :global(ol) { margin: 8px 0 14px; padding-left: 26px; }
  .mp-body :global(li) { margin: 5px 0; }
  .mp-body :global(li > ul),
  .mp-body :global(li > ol) { margin: 3px 0 6px; }
  .mp-body :global(input[type='checkbox']) {
    margin-right: 7px; vertical-align: middle; accent-color: var(--orange);
  }
  .mp-body :global(hr) {
    border: none; border-top: 1px solid var(--border); margin: 20px 0;
  }
  .mp-body :global(a) {
    color: var(--orange); text-decoration: none;
    border-bottom: 1px dashed transparent;
    transition: border-color .15s;
  }
  .mp-body :global(a:hover) { border-bottom-color: var(--orange); }
  .mp-body :global(img) {
    max-width: 100%; border-radius: var(--mp-radius-md);
    margin: 10px 0;
    box-shadow: 0 6px 20px -8px rgba(0, 0, 0, .5);
  }
  .mp-body :global(.mp-body-video) {
    position: relative; display: block;
    aspect-ratio: 16/9; max-width: 100%;
    margin: 14px 0; overflow: hidden;
    border-radius: var(--mp-radius-md);
    border: 1px solid var(--border);
    background: #000; cursor: pointer;
    box-shadow: 0 8px 24px -10px rgba(0, 0, 0, .55);
  }
  .mp-body :global(.mp-body-video img) {
    position: absolute; inset: 0; width: 100%; height: 100%;
    object-fit: cover; margin: 0; box-shadow: none; border-radius: 0;
    filter: brightness(.85);
    transition: filter .3s ease, transform .4s var(--mp-ease);
  }
  .mp-body :global(.mp-body-video:hover img) { filter: brightness(1); transform: scale(1.03); }
  .mp-body :global(.mp-body-video-play) {
    position: absolute; left: 50%; top: 50%; transform: translate(-50%, -50%);
    display: flex; align-items: center; justify-content: center;
    width: 52px; height: 52px; border-radius: 50%;
    background: rgba(15, 15, 18, .6);
    border: 2px solid rgba(255, 255, 255, .85);
    color: #fff; font-size: 22px;
    backdrop-filter: blur(6px); -webkit-backdrop-filter: blur(6px);
    transition: transform .25s var(--mp-ease-spring), background .2s ease;
  }
  .mp-body :global(.mp-body-video-play i) { margin-left: 3px; }
  .mp-body :global(.mp-body-video:hover .mp-body-video-play) {
    transform: translate(-50%, -50%) scale(1.1);
    background: rgba(var(--orange-rgb), .85);
  }
  .mp-body :global(.mp-body-video iframe) {
    position: absolute; inset: 0; width: 100%; height: 100%; border: none;
  }
  .mp-body :global(code) {
    background: var(--bg-input); padding: 2px 7px; border-radius: 6px;
    font-family: var(--font-mono); font-size: 12px;
    border: 1px solid var(--border);
  }
  .mp-body :global(pre) {
    background: var(--bg-input); padding: 16px; border-radius: var(--mp-radius-md);
    overflow-x: auto; border: 1px solid var(--border);
    margin: 12px 0;
  }
  .mp-body :global(pre code) {
    background: none; padding: 0; border: none;
  }
  .mp-body :global(blockquote) {
    border-left: 3px solid var(--orange);
    margin: 14px 0; padding: 8px 16px;
    color: var(--text-mute);
    background: rgba(var(--orange-rgb), .06);
    border-radius: 0 var(--mp-radius-md) var(--mp-radius-md) 0;
  }
  .mp-body :global(table) {
    border-collapse: collapse; width: 100%;
    margin: 12px 0; font-size: 12.5px;
    border-radius: var(--mp-radius-sm); overflow: hidden;
  }
  .mp-body :global(th) {
    background: var(--bg-input); text-align: left; font-weight: 700;
  }
  .mp-body :global(td),
  .mp-body :global(th) {
    border: 1px solid var(--border); padding: 8px 12px;
  }
  .mp-body :global(tr:nth-child(even) td) { background: rgba(127, 127, 127, .04); }
  .mp-body :global(strong),
  .mp-body :global(b) { font-weight: 700; color: var(--text); }
  .mp-body :global(iframe) {
    display: block; width: 100%; max-width: 560px;
    aspect-ratio: 16 / 9; border: none;
    border-radius: var(--mp-radius-md);
    margin: 12px 0; background: #000;
    box-shadow: 0 6px 24px -8px rgba(0, 0, 0, .5);
  }
  .mp-changelog { padding: 20px 22px; }

  /* ═════════════════════════════════════════════════════════════════════
     ГАЛЕРЕЯ
     ═════════════════════════════════════════════════════════════════════ */
  .mp-gallery {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
    gap: 12px;
  }
  .mp-g-item {
    position: relative; overflow: hidden;
    border: 1px solid var(--border);
    border-radius: var(--mp-radius-md);
    background: var(--bg-card);
    cursor: zoom-in;
    transition:
      border-color .18s ease,
      transform .22s var(--mp-ease),
      box-shadow .25s ease;
    animation: mp-g-item-in .4s var(--mp-ease) both;
  }
  .mp-g-item:nth-child(1)  { animation-delay: 10ms;  }
  .mp-g-item:nth-child(2)  { animation-delay: 40ms;  }
  .mp-g-item:nth-child(3)  { animation-delay: 70ms;  }
  .mp-g-item:nth-child(4)  { animation-delay: 100ms; }
  .mp-g-item:nth-child(5)  { animation-delay: 130ms; }
  .mp-g-item:nth-child(6)  { animation-delay: 160ms; }
  .mp-g-item:nth-child(7)  { animation-delay: 190ms; }
  .mp-g-item:nth-child(8)  { animation-delay: 220ms; }
  .mp-g-item:nth-child(9)  { animation-delay: 250ms; }
  .mp-g-item:nth-child(10) { animation-delay: 280ms; }
  @keyframes mp-g-item-in {
    from { opacity: 0; transform: translateY(10px) scale(.985); }
    to   { opacity: 1; transform: none; }
  }
  .mp-g-item:hover {
    border-color: var(--tacc, var(--orange));
    transform: translateY(-3px);
    box-shadow: 0 14px 30px -12px rgba(0, 0, 0, .5);
  }
  .mp-g-item img {
    width: 100%; aspect-ratio: 16/9;
    object-fit: cover; display: block;
    transition: transform .5s var(--mp-ease);
  }
  .mp-g-item:hover img { transform: scale(1.05); }
  .mp-g-cap {
    padding: 8px 12px;
    font-size: 11.5px; color: var(--text-mute);
  }
  .mp-g-zoom {
    position: absolute; top: 10px; right: 10px;
    width: 32px; height: 32px; border-radius: 50%;
    background: rgba(0, 0, 0, .6);
    backdrop-filter: blur(6px);
    color: #fff; font-size: 14px;
    display: flex; align-items: center; justify-content: center;
    opacity: 0; transform: scale(.8);
    transition: opacity .2s, transform .25s var(--mp-ease-spring);
  }
  .mp-g-item:hover .mp-g-zoom {
    opacity: 1; transform: scale(1);
  }

  /* ── Відео ── */
  .mp-videos {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(340px, 1fr));
    gap: 14px;
  }
  .mp-video {
    position: relative; aspect-ratio: 16/9;
    border: 1px solid var(--border);
    border-radius: var(--mp-radius-md);
    background: #000; overflow: hidden;
    box-shadow: var(--mp-shadow-card);
  }
  .mp-video iframe {
    position: absolute; inset: 0;
    width: 100%; height: 100%; border: none;
  }
  .mp-video-poster {
    position: absolute; inset: 0;
    width: 100%; height: 100%; padding: 0;
    border: none; background: #000; cursor: pointer;
    display: flex; align-items: center; justify-content: center;
    transition: transform .3s var(--mp-ease);
  }
  .mp-video-poster img {
    position: absolute; inset: 0;
    width: 100%; height: 100%; object-fit: cover;
    filter: brightness(.82) saturate(.95);
    transition: filter .3s ease, transform .4s var(--mp-ease);
  }
  .mp-video-poster:hover img { filter: brightness(1) saturate(1.05); transform: scale(1.04); }
  .mp-video-play {
    position: relative; z-index: 1;
    display: flex; align-items: center; justify-content: center;
    width: 58px; height: 58px; border-radius: 50%;
    background: rgba(15, 15, 18, .62);
    border: 2px solid rgba(255, 255, 255, .85);
    color: #fff; font-size: 24px;
    backdrop-filter: blur(6px); -webkit-backdrop-filter: blur(6px);
    box-shadow: 0 10px 30px -8px rgba(0, 0, 0, .6);
    transition: transform .25s var(--mp-ease-spring), background .2s ease;
  }
  .mp-video-poster:hover .mp-video-play {
    transform: scale(1.12);
    background: rgba(var(--orange-rgb), .85);
  }
  .mp-video-play i { margin-left: 3px; }
  .mp-video-fallback {
    position: absolute; left: 8px; bottom: 8px; z-index: 2;
    display: inline-flex; align-items: center; gap: 5px;
    padding: 4px 9px; border-radius: 999px;
    background: rgba(15, 15, 18, .75); border: 1px solid rgba(255, 255, 255, .16);
    color: #fff; font: 700 10.5px/1 var(--font-ui); text-decoration: none;
    opacity: .9; transition: opacity .15s, background .15s;
  }
  .mp-video-fallback:hover { opacity: 1; background: rgba(30, 30, 36, .92); }
  .mp-video-fallback i { color: #ff0033; font-size: 13px; }

  /* ═════════════════════════════════════════════════════════════════════
     СПИСОК ВЕРСІЙ
     ═════════════════════════════════════════════════════════════════════ */
  .mp-versions { display: flex; flex-direction: column; gap: 8px; }
  .mp-v-row {
    position: relative; overflow: hidden;
    display: flex; align-items: center; justify-content: space-between;
    gap: 12px; padding: 12px 16px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--mp-radius-md);
    cursor: pointer;
    transition:
      border-color .18s ease,
      background .18s ease,
      transform .2s var(--mp-ease);
  }
  .mp-v-row::before {
    content: ''; position: absolute; left: 0; top: 0; bottom: 0;
    width: 3px; background: var(--orange);
    transform: scaleY(0); transform-origin: center;
    transition: transform .25s var(--mp-ease);
  }
  .mp-v-row:hover {
    border-color: var(--border-hi);
    transform: translateX(2px);
  }
  .mp-v-row.sel {
    border-color: rgba(var(--orange-rgb), .5);
    background: rgba(var(--orange-rgb), .05);
  }
  .mp-v-row.sel::before { transform: scaleY(1); }
  .mp-v-row-main { min-width: 0; flex: 1; }
  .mp-v-row-name {
    display: flex; align-items: center; gap: 8px;
    font-size: 13px; flex-wrap: wrap;
  }
  .mp-v-row-name b { color: var(--text); font-weight: 700; }
  .mp-v-row-ic { width: 18px; height: 18px; border-radius: 4px; object-fit: contain; }
  .mp-v-row-meta {
    display: flex; gap: 12px; margin-top: 6px;
    font-size: 11.5px; color: var(--text-mute);
    flex-wrap: wrap;
  }
  .mp-v-row-meta span { display: inline-flex; align-items: center; gap: 5px; }
  .mp-v-row-meta i { color: var(--text-dim); }
  .mp-v-row-actions { white-space: nowrap; }

  /* ═════════════════════════════════════════════════════════════════════
     ПРАВА КОЛОНКА: install-картка + сайдбар-секції
     ═════════════════════════════════════════════════════════════════════ */

  /* Install-картка — головна дія сторінки: контрастна рамка кольором типу
     ресурсу, статус (автопідбір / цільова збірка), кнопка, прогрес. */
  .mp-install-card {
    display: flex; flex-direction: column; gap: 12px;
    padding: 16px;
    background:
      linear-gradient(160deg,
        color-mix(in srgb, var(--tacc, var(--orange)) 10%, transparent),
        transparent 60%),
      var(--bg-card);
    border: 1px solid color-mix(in srgb, var(--tacc, var(--orange)) 30%, var(--border));
    border-radius: var(--mp-radius-lg);
    box-shadow: var(--mp-shadow-card);
    animation: mp-slide-side .45s var(--mp-ease) both;
  }
  .mp-install-head {
    display: flex; align-items: center; gap: 8px;
    font: 800 11px/1 var(--font-ui);
    color: var(--tacc, var(--orange));
    text-transform: uppercase; letter-spacing: .5px;
  }
  .mp-install-head i {
    width: 22px; height: 22px;
    display: inline-flex; align-items: center; justify-content: center;
    border-radius: 6px; font-size: 12px;
    background: color-mix(in srgb, var(--tacc, var(--orange)) 16%, transparent);
  }

  .mp-version-pick { display: flex; flex-direction: column; gap: 8px; }
  .mp-v-filters { display: flex; flex-direction: column; gap: 8px; }
  .mp-v-all {
    display: inline-flex; align-items: center; gap: 6px;
    padding: 6px 10px; border-radius: 999px;
    background: color-mix(in srgb, var(--bg-input) 70%, transparent);
    border: 1px solid var(--border);
    font-size: 11px; color: var(--text-mute);
    cursor: pointer; user-select: none; align-self: flex-start;
    transition: color .15s, border-color .15s, background .15s;
  }
  .mp-v-all:hover {
    color: var(--text);
    border-color: var(--border-hi);
    background: var(--bg-input);
  }
  .mp-v-all input { accent-color: var(--orange); cursor: pointer; }
  .mp-v-loading,
  .mp-v-empty {
    display: flex; align-items: center; gap: 8px;
    font-size: 12px; color: var(--text-mute);
    padding: 4px 0;
  }

  .mp-auto-badge {
    display: flex; align-items: center; gap: 7px;
    padding: 8px 10px; border-radius: var(--mp-radius-sm);
    background: rgba(16, 185, 129, .1);
    border: 1px solid rgba(16, 185, 129, .32);
    color: var(--green);
    font: 700 11px/1.35 var(--font-ui);
  }
  .mp-auto-badge i { color: var(--green); flex-shrink: 0; }
  .mp-auto-badge-txt { flex: 1; min-width: 0; }

  /* Збірка не підтримує контент — попередження у віджеті встановлення
     (замість порожнечі/незрозумілого бейджа) з діями «інша збірка»/
     «прибрати фільтр». */
  .mp-unsup {
    display: flex; align-items: flex-start; gap: 8px;
    padding: 9px 11px; border-radius: var(--mp-radius-sm);
    background: color-mix(in srgb, #ff6b6b 9%, var(--bg-card));
    border: 1px solid color-mix(in srgb, #ff6b6b 34%, transparent);
  }
  .mp-unsup > i { color: #ff6b6b; font-size: 14px; margin-top: 1px; flex-shrink: 0; }
  .mp-unsup-main { display: flex; flex-direction: column; gap: 2px; min-width: 0; flex: 1; }
  .mp-unsup-t { font: 800 12px/1.35 var(--font-ui); color: var(--text); }
  .mp-unsup-s { font: 600 11px/1.4 var(--font-ui); color: #ff6b6b; }
  .mp-unsup-acts { display: flex; gap: 6px; margin-top: 6px; flex-wrap: wrap; }
  .mp-empty-acts { display: flex; gap: 6px; flex-wrap: wrap; justify-content: center; }

  .mp-target-badge {
    display: flex; align-items: center; flex-wrap: wrap; gap: 2px 7px;
    padding: 8px 10px; border-radius: var(--mp-radius-sm);
    background: var(--bg-input);
    border: 1px solid var(--border);
  }
  .mp-target-badge > i:first-child { color: var(--tacc, var(--orange)); flex-shrink: 0; }
  .mp-target-badge > span:not(.mp-target-sub):not(.mp-tgt-ic) {
    font: 700 12px/1.2 var(--font-ui); color: var(--text);
    flex: 1; min-width: 0;
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .mp-target-sub {
    width: 100%; /* завжди власний рядок під іконкою+назвою+кнопками */
    font: 600 10.5px/1.2 var(--font-mono);
    color: var(--text-mute);
    padding-left: 21px;
  }
  /* Реальна іконка сервера (власна або іконка його ядра) у бейджі цілі. */
  .mp-tgt-ic {
    width: 20px; height: 20px; min-width: 20px; flex-shrink: 0;
    border-radius: 6px;
    display: inline-flex; align-items: center; justify-content: center;
    background: color-mix(in srgb, var(--pcol, var(--orange)) 18%, var(--bg-input-hi));
    border: 1px solid color-mix(in srgb, var(--pcol, var(--orange)) 45%, var(--border));
    color: var(--pcol, var(--orange));
    font-size: 11px; overflow: hidden;
  }
  .mp-tgt-ic img { width: 100%; height: 100%; object-fit: contain; }
  .mp-auto-badge .mp-tgt-ic i { color: var(--pcol, var(--orange)); }

  /* Вкладки «Збірки ↔ Сервери» у модалках вибору цілі. */
  .mp-target-tabs {
    display: flex; gap: 4px; padding: 3px; margin-bottom: 10px;
    background: color-mix(in srgb, var(--bg-card) 60%, var(--bg-input));
    border: 1px solid var(--border);
    border-radius: 10px;
  }
  .mp-target-tab {
    flex: 1; min-width: 0;
    display: inline-flex; align-items: center; justify-content: center; gap: 6px;
    padding: 8px 10px;
    font: 700 12px/1 var(--font-ui);
    color: var(--text-mute);
    background: transparent; border: none; border-radius: 8px;
    cursor: pointer;
    transition: color .15s, background .15s;
  }
  .mp-target-tab:hover { color: var(--text); background: color-mix(in srgb, var(--hover) 70%, transparent); }
  .mp-target-tab.on { color: var(--orange); background: rgba(var(--orange-rgb), .14); }

  /* Порожній стан («немає фільтра, але є збірки») — весь бейдж клікабельний,
     виглядає як запрошення обрати збірку, а не як активний фільтр. */
  .mp-target-badge-empty {
    width: 100%; cursor: pointer; text-align: left; font-family: inherit;
    transition: border-color .15s, background .15s;
  }
  .mp-target-badge-empty > span { color: var(--text-mute); font-weight: 600; }
  .mp-target-badge-empty:hover { border-color: var(--border-hi); background: var(--hover-strong); }
  .mp-target-badge-empty i { color: var(--text-mute); }

  /* Кнопки «Змінити»/«×» у бейджі фільтра — компактні, з'являються лише
     коли onChangePackContext заданий (сторінка керує фільтром). */
  .mp-badge-action {
    flex-shrink: 0; width: 22px; height: 22px;
    display: inline-flex; align-items: center; justify-content: center;
    background: transparent; border: 1px solid transparent; border-radius: 6px;
    color: var(--text-mute); font-size: 12px; cursor: pointer;
    transition: background .15s, border-color .15s, color .15s;
  }
  .mp-badge-action:hover { background: var(--hover-strong); border-color: var(--border-hi); color: var(--text); }

  .mp-dl-btn {
    width: 100%; justify-content: center;
  }
  .mp-dl-btn:disabled {
    filter: grayscale(.5) brightness(.85);
    cursor: not-allowed;
    box-shadow: none;
  }
  .mp-progress {
    display: flex; align-items: center; gap: 10px;
    width: 100%;
  }
  .mp-progress-bar {
    flex: 1; height: 6px; border-radius: 999px;
    background: var(--bg-input); overflow: hidden;
  }
  .mp-progress-bar i {
    display: block; height: 100%;
    background: linear-gradient(90deg, var(--tacc, var(--orange)), var(--orange-2));
    box-shadow: 0 0 8px color-mix(in srgb, var(--tacc, var(--orange)) 55%, transparent);
    border-radius: 999px;
    transition: width .25s ease;
  }
  .mp-progress span {
    font: 700 11px/1 var(--font-mono);
    color: var(--text-mute);
    min-width: 34px; text-align: right;
  }
  .mp-install-hint {
    display: flex; align-items: center; gap: 7px;
    font-size: 11px; color: var(--text-mute);
    padding-top: 2px;
  }
  .mp-install-hint i { color: var(--text-dim); }

  /* ── Деталі поточної версії (модпак) ── */
  .mp-version-detail {
    display: flex; align-items: center; gap: 10px;
    flex-wrap: wrap;
    padding: 8px 10px;
    background: var(--bg-input);
    border-radius: var(--mp-radius-sm);
    font-size: 11.5px; color: var(--text-mute);
  }
  .mp-vd-ic { width: 16px; height: 16px; border-radius: 4px; object-fit: contain; }
  .mp-version-detail span { display: inline-flex; align-items: center; gap: 5px; }
  .mp-version-detail i { color: var(--text-dim); }
  .mp-vd-size { font-family: var(--font-mono); font-size: 10.5px; color: var(--text-dim); }
  .chip.g {
    background: rgba(16, 185, 129, .13);
    border: 1px solid rgba(16, 185, 129, .35);
    color: var(--green);
    padding: 2px 9px; border-radius: 999px;
    font: 700 10.5px/1.2 var(--font-ui);
  }

  /* ── Секції сайдбару (сумісність/деталі/автор/посилання/теги) ── */
  .mp-side-sec {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--mp-radius-md);
    padding: 14px 15px;
    transition: border-color .2s ease;
    animation: mp-slide-side .45s var(--mp-ease) both;
  }
  @keyframes mp-slide-side {
    from { opacity: 0; transform: translateX(10px); }
    to   { opacity: 1; transform: none; }
  }
  .mp-side-sec:nth-child(2) { animation-delay: 60ms; }
  .mp-side-sec:nth-child(3) { animation-delay: 100ms; }
  .mp-side-sec:nth-child(4) { animation-delay: 140ms; }
  .mp-side-sec:nth-child(5) { animation-delay: 180ms; }
  .mp-side-sec:nth-child(6) { animation-delay: 220ms; }
  .mp-side-sec:hover { border-color: var(--border-hi); }
  .mp-side-title {
    display: flex; align-items: center; gap: 8px;
    margin-bottom: 11px;
    font: 800 10.5px/1 var(--font-ui);
    color: var(--text-mute);
    text-transform: uppercase; letter-spacing: .6px;
  }
  .mp-side-title i {
    color: var(--tacc, var(--orange)); font-size: 13px;
    width: 22px; height: 22px;
    display: inline-flex; align-items: center; justify-content: center;
    border-radius: 6px;
    background: color-mix(in srgb, var(--tacc, var(--orange)) 12%, transparent);
  }
  .mp-side-row {
    display: flex; align-items: center; justify-content: space-between;
    gap: 8px; padding: 4px 0;
    font-size: 11.5px; color: var(--text-mute);
  }
  .mp-side-val {
    color: var(--text); font-weight: 600;
    text-align: right; font-size: 12px;
  }
  .mp-side-val.on { color: var(--tacc, var(--orange)); }
  .mp-side-val-link { color: var(--orange); text-decoration: none; }
  .mp-side-val-link:hover { text-decoration: underline; }
  .mp-side-links { display: flex; flex-direction: column; }
  .mp-side-link {
    display: flex; align-items: center; gap: 9px;
    padding: 7px 0;
    font-size: 12px; color: var(--text);
    text-decoration: none;
    transition: color .15s, padding-left .18s var(--mp-ease);
  }
  .mp-side-link i {
    color: var(--text-mute);
    transition: color .15s, transform .2s var(--mp-ease-spring);
  }
  .mp-side-link:hover {
    color: var(--tacc, var(--orange));
    padding-left: 4px;
  }
  .mp-side-link:hover i {
    color: var(--tacc, var(--orange));
    transform: scale(1.15);
  }
  .mp-side-tags { display: flex; gap: 5px; flex-wrap: wrap; }

  /* «Також встановлено у: ...» — компактні клікабельні рядки збірок.
     Навмисно вужчі за .mp-side-link (менше вертикального повітря): це
     потенційно довгий список, не має «роздувати» колонку. */
  .mp-installed-list { display: flex; flex-direction: column; gap: 2px; }
  .mp-installed-row {
    display: flex; align-items: center; gap: 9px;
    width: 100%; padding: 6px 8px;
    background: transparent; border: 1px solid transparent; border-radius: 8px;
    color: var(--text); font: 600 12px/1.3 var(--font-ui);
    text-align: left; cursor: pointer;
    transition: background .15s, border-color .15s, box-shadow .15s;
  }
  .mp-installed-row:hover { background: var(--hover); }
  .mp-installed-ic {
    width: 28px; height: 28px; min-width: 28px; border-radius: 7px;
    display: flex; align-items: center; justify-content: center;
    background: color-mix(in srgb, var(--pcol, var(--orange)) 18%, var(--bg-input));
    overflow: hidden; flex-shrink: 0;
  }
  .mp-installed-ic:not(.has-icon) { color: var(--pcol, var(--orange)); }
  .mp-installed-ic img { width: 100%; height: 100%; object-fit: cover; display: block; }
  .mp-installed-ic i { font-size: 14px; }
  .mp-installed-info { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
  .mp-installed-name {
    font: 700 12.5px/1.2 var(--font-ui); color: var(--text);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .mp-installed-ver {
    font: 500 10.5px/1 var(--font-mono);
    color: var(--text-mute); white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .mp-installed-more {
    width: 100%; margin-top: 2px; padding: 6px 7px;
    background: transparent; border: none;
    color: var(--tacc, var(--orange)); font: 700 11px/1 var(--font-ui);
    text-align: left; cursor: pointer; border-radius: 8px;
    transition: background .15s;
  }
  .mp-installed-more:hover { background: var(--hover-strong); }

  /* Автор */
  .mp-author-row {
    display: flex; align-items: center; gap: 10px;
    width: 100%; padding: 8px 10px;
    background: transparent;
    border: 1px solid transparent;
    border-radius: var(--mp-radius-md);
    color: var(--text); cursor: pointer;
    text-align: left;
    transition: background .15s, border-color .15s, transform .2s var(--mp-ease);
  }
  .mp-author-row:hover {
    background: color-mix(in srgb, var(--hover) 60%, transparent);
    border-color: var(--border);
    transform: translateX(2px);
  }
  .mp-author-avatar {
    width: 32px; height: 32px; min-width: 32px;
    border-radius: 50%; object-fit: cover;
    background: linear-gradient(135deg, var(--violet), var(--tacc, var(--orange)));
    box-shadow: 0 2px 8px rgba(0, 0, 0, .3);
  }
  .mp-author-avatar-fallback {
    display: flex; align-items: center; justify-content: center;
    color: #fff; font-size: 15px;
  }
  .mp-author-name {
    flex: 1; min-width: 0;
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
    font: 700 13px/1 var(--font-ui);
  }
  .mp-author-row > i { color: var(--text-mute); font-size: 15px; }
  .mp-author-row:hover > i { color: var(--tacc, var(--orange)); transform: translateX(3px); }

  /* ═════════════════════════════════════════════════════════════════════
     ШІ-панель + AI-відгук
     ═════════════════════════════════════════════════════════════════════ */
  .mp-ai-bar {
    display: flex; align-items: center; gap: 8px;
    flex-wrap: wrap; padding: 12px 16px;
    background: linear-gradient(135deg,
      color-mix(in srgb, var(--bg-card) 92%, transparent),
      color-mix(in srgb, var(--bg-panel) 92%, transparent));
    border: 1px solid var(--border);
    border-radius: var(--mp-radius-md);
    margin-bottom: 12px;
    animation: mp-slide-in .4s var(--mp-ease) .1s both;
  }
  .mp-ai-bar .btn { gap: 7px; }
  .mp-ai-bar .btn i { color: var(--orange); }
  .mp-ai-cache {
    display: inline-flex; align-items: center; gap: 6px;
    padding: 4px 10px; border-radius: 999px;
    background: rgba(255, 255, 255, .05);
    border: 1px solid var(--border);
    font-size: 11px; color: var(--text-mute);
  }
  :global(html[data-theme="light"]) .mp-ai-cache { background: rgba(0, 0, 0, .04); }
  .mp-ai-cache i { color: var(--green); }

  .mp-review {
    position: relative; overflow: hidden;
    margin-bottom: 14px; padding: 18px 20px;
    background:
      radial-gradient(ellipse at top left, rgba(var(--orange-rgb), .12), transparent 60%),
      radial-gradient(ellipse at bottom right, rgba(123, 77, 255, .12), transparent 60%),
      var(--bg-card);
    border: 1px solid rgba(var(--orange-rgb), .32);
    border-radius: var(--mp-radius-lg);
    box-shadow: 0 10px 30px -14px rgba(var(--orange-rgb), .35);
    animation: mp-review-in .45s var(--mp-ease) both;
  }
  @keyframes mp-review-in {
    from { opacity: 0; transform: translateY(8px); }
    to   { opacity: 1; transform: none; }
  }
  .mp-review-head {
    display: flex; align-items: center; gap: 8px;
    font: 800 13px/1 var(--font-ui);
    color: var(--text);
    margin-bottom: 12px;
  }
  .mp-review-head > i {
    color: var(--orange);
    filter: drop-shadow(0 0 8px rgba(var(--orange-rgb), .55));
    animation: mp-sparkle 2.4s ease-in-out infinite;
  }
  @keyframes mp-sparkle {
    0%, 100% { transform: scale(1) rotate(0); opacity: 1; }
    50%      { transform: scale(1.15) rotate(15deg); opacity: .85; }
  }
  .mp-review-date {
    margin-left: auto;
    font: 600 11px/1 var(--font-mono);
    color: var(--text-mute);
  }
  .mp-review-body {
    font: 500 13px/1.65 var(--font-ui);
    color: var(--text);
    user-select: text;
  }
  .mp-review-body :global(p) { margin: 0 0 8px; }
  .mp-review-body :global(h1),
  .mp-review-body :global(h2),
  .mp-review-body :global(h3),
  .mp-review-body :global(h4) {
    margin: 12px 0 6px; font-size: 15px; font-weight: 700;
  }
  .mp-review-body :global(ul),
  .mp-review-body :global(ol) { margin: 5px 0 10px; padding-left: 22px; }
  .mp-review-body :global(li) { margin: 3px 0; }
  .mp-review-body :global(a) { color: var(--orange); }
  .mp-review-body :global(code) {
    background: rgba(255, 255, 255, .07);
    padding: 1px 6px; border-radius: 4px;
    font-family: var(--font-mono); font-size: 12px;
  }
  :global(html[data-theme="light"]) .mp-review-body :global(code) { background: rgba(0, 0, 0, .05); }
  .mp-review-sources {
    margin-top: 14px; border-top: 1px solid var(--border);
    padding-top: 12px;
    display: flex; flex-direction: column; gap: 5px;
  }
  .mp-review-src-title {
    display: inline-flex; align-items: center; gap: 6px;
    font: 700 11px/1 var(--font-ui);
    color: var(--text-mute);
  }
  .mp-review-src {
    display: inline-flex; align-items: center; gap: 6px;
    font-size: 12px; color: var(--orange);
    text-decoration: none; word-break: break-all;
    transition: opacity .15s;
  }
  .mp-review-src::before { content: '🔗'; font-size: 10px; }
  .mp-review-src:hover { text-decoration: underline; }

  /* ═════════════════════════════════════════════════════════════════════
     МОДАЛКИ (вміст усередині UiModal — шапка/оверлей/анімація появи вже
     дає сам UiModal; тут лишається лише внутрішнє наповнення тіла)
     ═════════════════════════════════════════════════════════════════════ */
  @keyframes mp-modal-overlay-in {
    from { opacity: 0; }
    to   { opacity: 1; }
  }
  .cb-modal-search {
    display: flex; align-items: center; gap: 9px;
    margin: -16px -20px 14px;
    padding: 10px 20px;
    border-bottom: 1px solid var(--border);
    background: color-mix(in srgb, var(--bg-input) 40%, transparent);
  }
  .cb-modal-search > i { color: var(--text-mute); font-size: 15px; }
  .cb-modal-search input {
    flex: 1; background: transparent; border: none; outline: none;
    color: var(--text); font: 500 12.5px var(--font-ui);
  }
  .cb-modal-search input::placeholder { color: var(--text-dim); }

  .cb-pack-list { display: flex; flex-direction: column; gap: 8px; }
  .cb-pack-row {
    display: flex; align-items: center; justify-content: space-between;
    gap: 10px; padding: 10px 12px;
    background: var(--bg-input);
    border: 1px solid var(--border);
    border-radius: var(--mp-radius-md);
    transition: border-color .18s, background .18s, transform .18s var(--mp-ease);
  }
  .cb-pack-row:hover {
    border-color: color-mix(in srgb, var(--pcol, var(--orange)) 50%, var(--border));
    background: color-mix(in srgb, var(--pcol, var(--orange)) 5%, var(--bg-input));
    transform: translateY(-1px);
  }
  .cb-pack-row.installed { opacity: .55; }
  .cb-pack-info { display: flex; gap: 11px; align-items: center; min-width: 0; }
  .cb-pack-ic {
    width: 32px; height: 32px; min-width: 32px;
    border-radius: 8px; overflow: hidden; position: relative;
    display: flex; align-items: center; justify-content: center;
    background: color-mix(in srgb, var(--pcol, var(--orange)) 18%, var(--bg-input-hi));
    color: var(--pcol, var(--orange)); font-size: 14px;
    flex-shrink: 0;
  }
  .cb-pack-ic.has-icon { background: transparent; }
  .cb-pack-ic img { width: 100%; height: 100%; object-fit: cover; border-radius: 8px; }
  .cb-pack-name { font: 700 13px/1.2 var(--font-ui); color: var(--text); }
  .cb-pack-meta {
    font: 500 11px/1.2 var(--font-mono);
    color: var(--text-mute); margin-top: 3px;
  }
  .cb-warn {
    display: flex; align-items: center; gap: 6px;
    font-size: 10.5px; color: var(--orange);
    margin-top: 4px;
  }
  .cb-hint {
    display: flex; gap: 8px; align-items: flex-start;
    padding: 10px 12px;
    background: color-mix(in srgb, var(--bg-input) 70%, transparent);
    border: 1px solid var(--border);
    border-radius: var(--mp-radius-md);
    font-size: 12px; line-height: 1.5; color: var(--text-mute);
    margin-bottom: 10px;
  }
  .cb-hint > i { color: var(--orange); margin-top: 1px; flex-shrink: 0; }
  .cb-loading.sm {
    padding: 22px 0; font-size: 12px;
    display: flex; align-items: center; justify-content: center;
    gap: 10px; color: var(--text-mute);
  }
  .cb-empty.sm {
    padding: 26px 0; font-size: 12px;
    display: flex; flex-direction: column; align-items: center; gap: 10px;
    color: var(--text-mute);
  }
  .cb-empty.sm i { font-size: 26px; color: var(--border-hi); }

  /* ── Вибір світів (мультивибір) ── */
  .cb-world-toolbar {
    display: flex; align-items: center; justify-content: space-between;
    gap: 8px; margin-bottom: 10px;
  }
  .cb-world-count {
    font: 700 11px/1 var(--font-mono);
    color: var(--text-mute);
    padding: 4px 10px; border-radius: 999px;
    background: var(--bg-input); border: 1px solid var(--border);
  }
  .cb-world-quick { display: flex; gap: 6px; }
  .cb-world-list {
    display: flex; flex-direction: column; gap: 7px;
    max-height: 280px; overflow-y: auto;
  }
  .cb-world-row {
    display: flex; align-items: center; gap: 10px;
    padding: 10px 12px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--mp-radius-md);
    cursor: pointer;
    transition: border-color .15s, background .15s, transform .18s var(--mp-ease);
  }
  .cb-world-row:hover {
    border-color: var(--border-hi);
    transform: translateX(2px);
  }
  .cb-world-row.sel {
    border-color: rgba(var(--orange-rgb), .5);
    background: rgba(var(--orange-rgb), .05);
  }
  .cb-world-row input { accent-color: var(--orange); cursor: pointer; }
  .cb-world-row i { color: var(--text-mute); font-size: 15px; }
  .cb-world-global {
    display: flex; align-items: center; gap: 9px;
    margin-top: 12px; padding: 10px 12px;
    background: color-mix(in srgb, var(--bg-input) 55%, transparent);
    border: 1px dashed var(--border-hi);
    border-radius: var(--mp-radius-md);
    cursor: pointer;
    font: 700 12px/1.3 var(--font-ui); color: var(--text-mute);
    transition: border-color .15s, background .15s;
  }
  .cb-world-global:hover { border-color: rgba(var(--orange-rgb), .5); background: rgba(var(--orange-rgb), .04); }
  .cb-world-global input { accent-color: var(--orange); cursor: pointer; margin: 0; }
  .cb-world-global i { color: var(--orange); font-size: 14px; }
  .cb-world-name {
    font: 700 12.5px/1.2 var(--font-ui);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }

  /* ═════════════════════════════════════════════════════════════════════
     ЛАЙТБОКС ГАЛЕРЕЇ
     ═════════════════════════════════════════════════════════════════════ */
  .lb-overlay {
    position: fixed; inset: 0; z-index: 1000010;
    background: rgba(0, 0, 0, .94);
    backdrop-filter: blur(12px); -webkit-backdrop-filter: blur(12px);
    display: flex; align-items: center; justify-content: center;
    user-select: none;
    animation: mp-modal-overlay-in .25s ease-out both;
  }
  .lb-toolbar {
    position: absolute; top: 0; left: 0; right: 0; z-index: 2;
    display: flex; align-items: center; justify-content: space-between;
    padding: 14px 18px;
    background: linear-gradient(180deg, rgba(0, 0, 0, .5), transparent);
  }
  .lb-count {
    font: 700 12px/1 var(--font-mono);
    color: rgba(255, 255, 255, .75);
    padding: 5px 12px; border-radius: 999px;
    background: rgba(255, 255, 255, .08);
    border: 1px solid rgba(255, 255, 255, .12);
  }
  .lb-tools { display: flex; gap: 6px; }
  .lb-btn {
    width: 36px; height: 36px;
    border-radius: 10px;
    border: 1px solid rgba(255, 255, 255, .14);
    background: rgba(255, 255, 255, .06);
    color: #fff; font-size: 16px; cursor: pointer;
    display: flex; align-items: center; justify-content: center;
    backdrop-filter: blur(6px);
    transition: background .15s, transform .2s var(--mp-ease-spring);
  }
  .lb-btn:hover {
    background: rgba(255, 255, 255, .18);
    transform: scale(1.08);
  }
  .lb-stage {
    width: 100%; height: 100%; overflow: hidden;
    padding: 70px 90px 80px;
    display: flex; align-items: center; justify-content: center;
  }
  .lb-img {
    max-width: 100%; max-height: 100%;
    object-fit: contain; border-radius: 8px;
    box-shadow: 0 20px 60px rgba(0, 0, 0, .7);
    transition: transform .06s linear;
  }
  .lb-nav {
    position: absolute; top: 50%; z-index: 2;
    transform: translateY(-50%);
    width: 48px; height: 64px; border-radius: 12px;
    border: 1px solid rgba(255, 255, 255, .14);
    background: rgba(255, 255, 255, .06);
    color: #fff; font-size: 24px; cursor: pointer;
    display: flex; align-items: center; justify-content: center;
    backdrop-filter: blur(6px);
    transition: background .15s, transform .2s var(--mp-ease-spring);
  }
  .lb-nav:hover {
    background: rgba(255, 255, 255, .18);
    transform: translateY(-50%) scale(1.06);
  }
  .lb-prev { left: 16px; }
  .lb-next { right: 16px; }
  .lb-caption {
    position: absolute; bottom: 0; left: 0; right: 0;
    padding: 18px 20px 20px;
    background: linear-gradient(transparent, rgba(0, 0, 0, .8));
    color: #fff;
    display: flex; flex-direction: column; gap: 6px;
    align-items: center; text-align: center;
    font-size: 13px;
  }
  .lb-hint {
    font-size: 11px; color: rgba(255, 255, 255, .55);
    display: inline-flex; align-items: center; gap: 6px;
  }

  /* ═════════════════════════════════════════════════════════════════════
     АДАПТИВНІСТЬ
     ═════════════════════════════════════════════════════════════════════ */
  @media (max-width: 1100px) {
    /* Вузький екран: колонки складаються одна під одну в загальному
       потоці — .mp-main більше не ділить фіксовану висоту, тож обидва
       блоки повертаються до звичайного document-скролу разом з .mp-wrap
       (не мають власного overflow, інакше на телефоні/вузькому вікні
       з'явився б зайвий внутрішній скрол на весь блок). */
    .mp-main { flex-direction: column; height: auto; }
    .mp-col { overflow: visible; flex: 0 0 auto; padding-right: 0; }
    .mp-side { flex: 0 0 auto; overflow: visible; padding-right: 0; }
    .mp-wrap { overflow-y: auto; }
    .mp-install-card { order: -1; }
  }
  @media (max-width: 720px) {
    .mp-banner { padding: 22px 20px 20px; min-height: 214px; }
    .mp-name { font-size: 20px; }
    .mp-ic { width: 68px; height: 68px; min-width: 68px; }
    .mp-tagline { -webkit-line-clamp: 3; }
    .mp-body, .mp-changelog { padding: 16px 14px; }
    .lb-stage { padding: 60px 20px 70px; }
    .lb-prev { left: 6px; }
    .lb-next { right: 6px; }
    .mp-external-txt { display: none; }
  }

  /* ── Знижена анімація ── */
  @media (prefers-reduced-motion: reduce) {
    .mp-empty i,
    .mp-review-head > i { animation: none !important; }
    .mp-banner, .mp-tabs, .mp-tab-content,
    .mp-side-sec, .mp-review, .mp-ai-bar, .mp-install-card,
    .mp-type-badge { animation: none !important; }
  }
</style>