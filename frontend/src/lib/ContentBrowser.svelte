<script lang="ts">
  import UiModal from './UiModal.svelte'
  // ── Браузер контенту ──────────────────────────────────────────────────
  // Портує UX веб-апки Modrinth (modrinth/code) у стиль лаунчера:
  //   - таби типів контенту (Модпаки/Моди/Ресурспаки/Шейдери/Датапаки);
  //   - пошук + сорт (dropdown, «Рекомендовані» за замовчуванням);
  //   - правий сайдбар з фільтрами-акардіонами: ФІЛЬТР ЗБІРКИ (можна
  //     додати/прибрати), Джерело, Категорії з іконками (Modrinth+CF),
  //     Версія гри, Лоадер — як SearchSidebarFilter у веб-апки;
  //   - клік по картці ЗАВЖДИ відкриває сторінку проєкту (для всіх типів
  //     контенту), встановлення — зі сторінки (з прогресом і позначкою
  //     «вже встановлено», як у Modrinth app).
  import { t } from './i18n.svelte'
  import { tick } from 'svelte'
  import { fly } from 'svelte/transition'
  import { cubicOut } from 'svelte/easing'
  import { App, on, withCallTimeout } from './wails'
  import { toast, confirmDialog } from './toast.svelte'
  import { packAsset } from './packAssets.svelte'
  import type { BrowserEntry, BrowserSearchResult, BrowserCategory, BrowserInstalledItem, BrowserProgress, BrowserCompatEvent, BrowserCompatiblePack, BrowserVersionView, BrowserModpackInstallCheck, BrowserGameVersion, BrowserLoaderTag, BrowserCollection, CollectionItemVersion, CollectionInstallPlanItem, BrowserInstallResult, SearchHistoryItem } from './types'
  import fabricIcon from '../assets/images/fabric_icon.png'
  import forgeIcon from '../assets/images/forge_icon.png'
  import neoforgeIcon from '../assets/images/neoforge_icon.png'
  import quiltIcon from '../assets/images/quilt_icon.png'
  import modrinthIcon from '../assets/images/Modrinth_icon.png'
  import curseforgeIcon from '../assets/images/curseforge_icon.png'
  import SelectField from './SelectField.svelte'
  import { smartImage } from './smartImage'
  import PackPicker from './PackPicker.svelte'
  import PresetIconPicker from './PresetIconPicker.svelte'
  import ServerTargetList from './servers/ServerTargetList.svelte'
  import { kindForTab, serversForTab, serverVisual, coreName, type BrowserServer } from './servers/serverTargets'
  import { CORE_META } from './servers/constants'
  import type { PackPickerItem, PackRow, PackFolderRow } from './PackPicker.svelte'

  // Палітра кольорів колекцій (та сама, що в редакторі папок/збірок).
  const cbColorPresets = ['#7b4dff', '#ff8a00', '#10b981', '#3b82f6', '#ef4444', '#f59e0b', '#8fd3ff', '#ff6b9d']

  // ── Пропси ──
  // packContext — контекст збірки (кнопка «Шукати моди» в менеджері модів):
  // фільтр збірки закріплено, встановлення йде одразу в неї. У браузері
  // цей фільтр МОЖНА прибрати або вибрати іншу збірку (dropdown).
  // packs — кастомні збірки для фільтра (id/назва/версія/лоадер/іконка).
  // packPicker — дані для спільного списку з ПАПКАМИ (порядок збірок і
  // папок + рядки + папки) — фільтр «Збірка» рендериться ним: збірки з
  // папок видно і можна вибрати, папки розкриваються як у sidebar.
  //
  // serverContext — те саме, що packContext, але для СЕРВЕРА (кнопка
  // «Знайти онлайн» у менеджері модів/плагінів сторінки сервера —
  // ServerContentTab.svelte). На відміну від packContext:
  //   - фіксований назавжди: немає dropdown вибору «іншого сервера» (сервер
  //     один, конкретний — так само, як не можна «змінити збірку» для
  //     world-датапака);
  //   - обмежує видимі вкладки до «Моди»/«Плагіни» (сервер фізично не
  //     тримає модпаки/ресурспаки/шейдери/датапаки як встановлюваний
  //     контент цього менеджера);
  //   - kind визначає, яка вкладка активна при вході: 'mods' → таб 'mod',
  //     'plugins' → таб 'plugin' (залежно від того, ServerModsTab чи
  //     ServerPluginsTab відкрив браузер).
  let {
    packContext = null,
    serverContext = null,
    servers = [],
    packs = [],
    packPicker = null,
    openProject,
    onOpenPack,
    onPickServer,
    onClearServerContext,
    visible = true,
  } = $props<{
    // projectType — тип контенту, з якого відкрили браузер (кнопка
    // «Додати з браузера» у вкладці «Контент»): моди/ресурспаки/шейдери/
    // датапаки. Порожній = «Моди» (як раніше).
    packContext?: { id: string; name: string; mcVersion: string; loader: string; projectType?: string } | null
    serverContext?: { id: string; name: string; mcVersion: string; loader: string; kind: 'mods' | 'plugins' } | null
    // servers — власні сервери для перемикача цілі встановлення «Збірки ↔
    // Сервери» (заповнює App.svelte). Сервер — така сама ціль, як збірка,
    // але його loader залежить від вкладки (modLoader vs pluginLoader),
    // тому його визначає App у onPickServer, а не сам браузер.
    servers?: BrowserServer[]
    // onPickServer — користувач обрав сервер ціллю встановлення. Саме
    // callback, а не локальний стан: сервер — сутність App.svelte, і
    // контекст має бути ОДИН на застосунок (той самий serverContext, що й
    // від кнопки «Знайти онлайн»), інакше «Моди сервера» показували б
    // різні цілі залежно від того, як саме відкрили браузер.
    onPickServer?: (id: string, kind: 'mods' | 'plugins') => void
    // onClearServerContext — ціль повернулась до «Збірок» (або обрано
    // вкладку, яку сервер фізично не тримає: шейдери/ресурспаки/датапаки).
    onClearServerContext?: () => void
    packs?: { id: string; name: string; mcVersion: string; loader: string; iconUrl?: string; icon?: string; color?: string }[]
    packPicker?: {
      items: PackPickerItem[]
      rowById: Map<string, PackRow>
      folderById: Map<string, PackFolderRow>
    } | null
    openProject?: (source: string, projectId: string, packCtx?: { id: string; name: string; mcVersion: string; loader: string; isServer?: boolean } | null) => void
    // Після встановлення модпака з картки — перейти на сторінку нової
    // збірки (App.svelte: закриває браузер і відкриває сторінку).
    onOpenPack?: (packId: string) => void
    // Браузер змонтований завжди (стан зберігається), але ховається.
    // Коли він невидимий — закриваємо модалки, щоб вони не «спливали»
    // назад при поверненні на сторінку пошуку.
    visible?: boolean
  }>()

  // ── Таби типів контенту (як NavTabs у Modrinth discover) ──────────────
  // Кожен таб має СВІЙ акцентний колір (--tb-c): збірки — помаранчевий,
  // моди — зелений, плагіни — бірюзовий, ресурспаки — фіолетовий, шейдери —
  // рожевий, датапаки — синій. Використовується в градієнті активного
  // таба/підсвітках карток.
  const allContentTypes = [
    { id: 'modpack', label: t('browser.tab.modpacks'), icon: 'ti-package', color: 'var(--orange)' },
    { id: 'mod', label: t('browser.tab.mods'), icon: 'ti-puzzle', color: 'var(--green)' },
    { id: 'plugin', label: t('browser.tab.plugins'), icon: 'ti-plug', color: '#14b8a6' },
    { id: 'resourcepack', label: t('browser.tab.resourcepacks'), icon: 'ti-photo', color: 'var(--violet)' },
    { id: 'shader', label: t('browser.tab.shaders'), icon: 'ti-brush', color: '#ff6b9d' },
    { id: 'datapack', label: t('browser.tab.datapacks'), icon: 'ti-database', color: 'var(--blue)' },
  ]
  // serverContext обмежує видимі вкладки до ОДНОГО типу — того, що вміє
  // цей конкретний сервер (kind: 'mods' | 'plugins' уже визначено при
  // відкритті браузера з картки сервера, див. activeType нижче). Раніше
  // тут лишались ОБИДВІ вкладки «Моди»/«Плагіни» одразу для будь-якого
  // серверного контексту — мод-сервер (Fabric/Forge/NeoForge/Quilt) і
  // плагін-сервер (Paper/Purpur/Velocity) не можуть встановити контент
  // іншого типу, тож друга вкладка була мертвим вибором, що вів у
  // порожню видачу.
  const contentTypes = $derived(
    serverContext ? allContentTypes.filter(c => c.id === (serverContext.kind === 'plugins' ? 'plugin' : 'mod')) : allContentTypes
  )
  const initialProjectType = packContext?.projectType
  let activeType = $state(
    serverContext
      ? (serverContext.kind === 'plugins' ? 'plugin' : 'mod')
      : initialProjectType && ['mod', 'resourcepack', 'shader', 'datapack'].includes(initialProjectType)
      ? initialProjectType
      : (packContext ? 'mod' : 'modpack')
  )

  // ── Розділ «Збережено» (закладки + колекції) ──
  // viewMode='search' — звичайний пошук/рекомендовані (як завжди).
  // viewMode='saved' — окремий розділ: замінює грід результатів пошуку
  // списком закладених проєктів або відкритою колекцією, не займаючи
  // жодне зі значень sourceFilter (те про "звідки контент", це — про
  // "що показати замість пошуку").
  let viewMode = $state<'search' | 'saved'>('search')
  // savedTab — усередині розділу «Збережено»: 'bookmarks' (плоский
  // список зірочок) чи 'collections' (групи для одноклакового встановлення).
  let savedTab = $state<'bookmarks' | 'collections'>('bookmarks')
  let bookmarkedEntries = $state<BrowserEntry[]>([])
  let bookmarkedKeys = $state<Set<string>>(new Set())
  let loadingBookmarks = $state(false)
  // savedTypeFilter — перемикач типу контенту ВСЕРЕДИНІ «Закладок»
  // ('' = усі типи разом). Раніше bookmarkedEntries завжди показував усі
  // збережені моди/ресурспаки/шейдери/датапаки одним суцільним грідом
  // без можливості відфільтрувати — на великому списку закладок важко
  // знайти саме шейдер серед сотні модів. Фільтрація суто клієнтська
  // (bookmarkedEntries вже завантажені цілком), нового запиту не треба.
  let savedTypeFilter = $state('')
  const savedFilteredEntries = $derived(
    savedTypeFilter ? bookmarkedEntries.filter(e => e.type === savedTypeFilter) : bookmarkedEntries
  )
  // Типи, які реально є серед закладок (щоб не показувати пілюлю
  // «Шейдери», якщо жодного шейдера не закладено) — рахуємо один раз на
  // кожну зміну bookmarkedEntries, не на кожен рендер картки.
  const savedTypesPresent = $derived(new Set(bookmarkedEntries.map(e => e.type)))
  let collectionsList = $state<BrowserCollection[]>([])
  let loadingCollections = $state(false)
  // openCollectionId — непорожньо, коли розділ «Збережено» → «Колекції»
  // показує ВМІСТ конкретної колекції (не список самих колекцій).
  let openCollectionId = $state('')
  let openCollectionItems = $state<BrowserEntry[]>([])
  let openCollectionVersions = $state<Record<string, CollectionItemVersion>>({})
  let loadingCollectionItems = $state(false)

  // ── Історія пошукових запитів («останні, що дали результат») ──
  let searchHistory = $state<SearchHistoryItem[]>([])
  let showSearchHistory = $state(false)
  // Вузьке вікно (≤1100px): сайдбар фільтрів ховається й відкривається поверх
  // списку кнопкою в toolbar (замість того, щоб стискати список).
  let filtersOpen = $state(false)
  let searchHistoryLoaded = false
  async function loadSearchHistory() {
    try {
      searchHistory = (await App.GetSearchHistory()) ?? []
    } catch (e) { console.error(e) }
  }
  // Список для поточної вкладки типу контенту зверху («меблі» шукане у
  // модах не змішується з тим самим текстом, шуканим серед ресурспаків) —
  // фільтруємо на фронтенді з уже завантаженого повного списку, а не
  // робимо окремий запит на кожну зміну вкладки.
  const searchHistoryForType = $derived(searchHistory.filter(h => h.projectType === activeType))
  function openSearchHistoryDropdown() {
    showSearchHistory = true
    if (!searchHistoryLoaded) {
      searchHistoryLoaded = true
      loadSearchHistory()
    }
  }
  function useSearchHistoryItem(item: SearchHistoryItem) {
    query = item.query
    showSearchHistory = false
    doSearch(1)
  }
  async function removeSearchHistoryItem(item: SearchHistoryItem, e: Event) {
    e.stopPropagation()
    searchHistory = searchHistory.filter(h => !(h.query === item.query && h.projectType === item.projectType))
    try {
      await App.RemoveSearchHistoryEntry(item.query, item.projectType)
    } catch (err) { console.error(err) }
  }
  async function clearSearchHistoryAll() {
    searchHistory = []
    try {
      await App.ClearSearchHistory()
    } catch (err) { console.error(err) }
  }

  // Множина ключів закладок завантажується РАЗ при відкритті браузера
  // (і оновлюється локально на кожен toggle) — картки звичайного пошуку
  // звіряються з нею синхронно (без IPC на кожну картку), щоб зірочка
  // одразу показувала правильний стан навіть у звичайних результатах
  // пошуку, не лише в розділі «Збережено».
  async function loadBookmarkedKeys() {
    try {
      const keys = (await App.GetBookmarkedKeys()) ?? []
      bookmarkedKeys = new Set(keys)
    } catch (e) { console.error(e) }
  }

  async function loadBookmarkedEntries() {
    loadingBookmarks = true
    try {
      bookmarkedEntries = (await App.GetBookmarkedEntries()) ?? []
    } catch (e) {
      console.error(e)
      bookmarkedEntries = []
    } finally {
      loadingBookmarks = false
    }
  }

  async function loadCollectionsList() {
    loadingCollections = true
    try {
      collectionsList = (await App.GetCollections()) ?? []
    } catch (e) {
      console.error(e)
      collectionsList = []
    } finally {
      loadingCollections = false
    }
  }

  async function openCollection(id: string) {
    openCollectionId = id
    loadingCollectionItems = true
    try {
      const [items, versions] = await Promise.all([
        App.GetCollectionItems(id),
        App.GetCollectionVersionInfo(id),
      ])
      openCollectionItems = items ?? []
      openCollectionVersions = versions ?? {}
    } catch (e) {
      console.error(e)
      openCollectionItems = []
      openCollectionVersions = {}
    } finally {
      loadingCollectionItems = false
    }
  }

  function closeCollection() {
    openCollectionId = ''
    openCollectionItems = []
    openCollectionVersions = {}
  }

  // Перемикання на розділ «Збережено» — ліниво довантажує потрібний
  // список ЛИШЕ якщо його ще не було завантажено в цій сесії компонента
  // (повторний перехід туди-сюди між "Пошук" і "Збережено" не бʼє мережу
  // заново щоразу).
  let bookmarksLoadedOnce = false
  let collectionsLoadedOnce = false
  function openSavedView() {
    viewMode = 'saved'
    closeCollection()
    if (savedTab === 'bookmarks' && !bookmarksLoadedOnce) {
      bookmarksLoadedOnce = true
      loadBookmarkedEntries()
    } else if (savedTab === 'collections' && !collectionsLoadedOnce) {
      collectionsLoadedOnce = true
      loadCollectionsList()
    }
  }
  function switchSavedTab(tab: 'bookmarks' | 'collections') {
    savedTab = tab
    closeCollection()
    if (tab === 'bookmarks' && !bookmarksLoadedOnce) {
      bookmarksLoadedOnce = true
      loadBookmarkedEntries()
    } else if (tab === 'collections' && !collectionsLoadedOnce) {
      collectionsLoadedOnce = true
      loadCollectionsList()
    }
  }
  function backToSearch() {
    viewMode = 'search'
    closeCollection()
  }

  // Тумблер зірочки на картці — доступний і зі звичайного пошуку, і з
  // розділу «Збережено» (там натискання прибирає закладку одразу зі
  // списку, що показано, без повторного відкриття розділу).
  //
  // OPTIMISTIC UPDATE: раніше клас .on (жовта «запалена» зірочка)
  // з'являвся лише ПІСЛЯ того, як await App.ToggleBookmark повернувся —
  // на повільнішому диску/першому виклику після запуску (сховище ще не
  // прогріте) це відчувалось як «клікнув — зірочка лишилась
  // невидимою/сірою», хоча закладка насправді ЗБЕРЕГЛАСЬ (просто UI ще
  // не встиг оновитись, а виглядало це так, ніби бекенд «не бачить»
  // такого проєкту). Тепер bookmarkedKeys перемикається ОДРАЗУ на клік,
  // синхронно, ще до відповіді бекенду — зірочка запалюється миттєво.
  // Якщо бекенд усе ж повернув відмінний результат або помилку (напр.
  // порожній projectId — «некоректний проєкт для закладки»), локальний
  // стан ВІДКОЧУЄТЬСЯ назад і показується тост з причиною — так реальна
  // відмова більше не виглядає як мовчазний глюк.
  async function toggleBookmark(entry: BrowserEntry) {
    const key = entry.source + '|' + (entry.projectId || entry.id)
    const wasBookmarked = bookmarkedKeys.has(key)
    // Миттєве перемикання (optimistic) — до відповіді бекенду.
    const optimistic = new Set(bookmarkedKeys)
    if (wasBookmarked) optimistic.delete(key); else optimistic.add(key)
    bookmarkedKeys = optimistic
    try {
      const nowBookmarked = await App.ToggleBookmark(entry)
      // Бекенд підтвердив ІНШИЙ стан, ніж очікували оптимістично (рідко,
      // напр. паралельний клік чи розсинхрон стану) — коригуємо до
      // фактичного результату, а не лишаємо здогадку з кліку.
      if (nowBookmarked !== !wasBookmarked) {
        const corrected = new Set(bookmarkedKeys)
        if (nowBookmarked) corrected.add(key); else corrected.delete(key)
        bookmarkedKeys = corrected
      }
      if (viewMode === 'saved' && savedTab === 'bookmarks' && !nowBookmarked) {
        // У самому розділі «Збережено» зняття зірочки прибирає картку
        // зі списку одразу (інакше юзер бачив би картку без зірочки,
        // що виглядає як «закладка лишилась, зірочка просто зникла»).
        bookmarkedEntries = bookmarkedEntries.filter(e => (e.source + '|' + (e.projectId || e.id)) !== key)
      }
    } catch (e: any) {
      // Бекенд відмовив (напр. порожній projectId для цього запису) —
      // відкочуємо оптимістичне перемикання назад до стану ДО кліку,
      // інакше зірочка лишилась би «запаленою» в UI, хоча закладка
      // фактично не зберіглась.
      const reverted = new Set(bookmarkedKeys)
      if (wasBookmarked) reverted.add(key); else reverted.delete(key)
      bookmarkedKeys = reverted
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  // ── Додавання картки в колекцію («+» на картці, поруч із зірочкою) ────
  // AddToCollection на бекенді існував і працював — просто ніде у
  // фронтенді не викликався: RemoveFromCollection (кнопка «х» у вже
  // відкритій колекції) був, а самого способу ДОДАТИ мод у колекцію не
  // було ніде. Клік по «+» відкриває маленьке спливне меню зі списком
  // колекцій прямо під карткою (без переходу в розділ «Збережено») —
  // клік по назві додає entry в неї з versionId='' (найновіша сумісна;
  // те саме "авто", що бере встановлення без явного вибору версії).
  // «Нова колекція…» в меню створює колекцію і одразу додає entry в неї.
  let addToCollectionMenu = $state<{ entry: BrowserEntry; x: number; y: number } | null>(null)
  // entryCollectionIds — ID колекцій, де ВІДКРИТИЙ ЗАРАЗ entry вже є
  // (GetEntryCollectionIDs). Порожній Set, поки запит не завершився —
  // меню тоді не показує позначок, але й не дозволяє хибно «додати
  // знову» вважати новою дією (див. addEntryToCollection).
  let entryCollectionIds = $state<Set<string>>(new Set())

  function openAddToCollectionMenu(e: MouseEvent, entry: BrowserEntry) {
    e.stopPropagation()
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
    addToCollectionMenu = { entry, x: rect.left, y: rect.bottom + 6 }
    entryCollectionIds = new Set()
    // Список колекцій потрібен для меню незалежно від того, чи юзер уже
    // заходив у вкладку «Збережено» цього сеансу компонента.
    if (!collectionsLoadedOnce) {
      collectionsLoadedOnce = true
      loadCollectionsList()
    }
    const pid = entry.projectId || entry.id
    App.GetEntryCollectionIDs(entry.source, pid).then((ids: string[]) => {
      // Меню могло закритись або відкритись для іншого entry, поки йшов
      // запит — застосовуємо результат лише якщо це все ще той самий entry.
      if (addToCollectionMenu && addToCollectionMenu.entry === entry) {
        entryCollectionIds = new Set(ids ?? [])
      }
    }).catch(() => {})
  }

  async function addEntryToCollection(collectionId: string) {
    if (!addToCollectionMenu) return
    // Елемент уже в цій колекції — AddToCollection мовчки перезаписав би
    // той самий запис (не дублікат у даних, але й не видима дія для
    // користувача). Замість повторного виклику одразу повідомляємо, що
    // додавати нічого — і не закриваємо меню, щоб можна було обрати іншу
    // колекцію без повторного відкриття.
    if (entryCollectionIds.has(collectionId)) {
      toast(t('browser.saved.alreadyInCollection'), 'info')
      return
    }
    const entry = addToCollectionMenu.entry
    addToCollectionMenu = null
    try {
      await App.AddToCollection(collectionId, entry, '', '')
      collectionsList = collectionsList.map(c => c.id === collectionId ? { ...c, itemCount: c.itemCount + 1 } : c)
      toast(t('browser.saved.addedToCollection'), 'success')
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  async function createCollectionAndAdd() {
    if (!addToCollectionMenu) return
    const entry = addToCollectionMenu.entry
    addToCollectionMenu = null
    const name = t('browser.saved.newCollection')
    try {
      const c = await App.CreateCollection(name, '', '', '')
      collectionsList = [...collectionsList, c]
      await App.AddToCollection(c.id, entry, '', '')
      collectionsList = collectionsList.map(x => x.id === c.id ? { ...x, itemCount: 1 } : x)
      toast(t('browser.saved.addedToCollection'), 'success')
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  $effect(() => {
    if (!addToCollectionMenu) return
    const close = () => { addToCollectionMenu = null }
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') addToCollectionMenu = null }
    window.addEventListener('click', close)
    window.addEventListener('blur', close)
    window.addEventListener('keydown', onKey)
    return () => {
      window.removeEventListener('click', close)
      window.removeEventListener('blur', close)
      window.removeEventListener('keydown', onKey)
    }
  })


  // ── Створення/перейменування колекції ──
  // Модалка тепер несе ІКОНКУ і КОЛІР, як у збірок (бекенд BrowserCollection
  // давно зберігає Icon/Color — їх просто не було звідки виставити в UI).
  // Без іконки лишається дефолтна папка; колір тонує іконку/картку.
  let collectionEditDialog = $state<{ id: string; name: string; icon: string; color: string } | null>(null)
  function createCollectionPrompt() {
    collectionEditDialog = { id: '', name: '', icon: 'ti-folder', color: '#7b4dff' }
  }
  function renameCollectionPrompt(c: BrowserCollection) {
    collectionEditDialog = { id: c.id, name: c.name, icon: c.icon || 'ti-folder', color: c.color || '#7b4dff' }
  }
  async function submitCollectionEdit() {
    if (!collectionEditDialog) return
    const name = collectionEditDialog.name.trim()
    if (!name) return
    const icon = collectionEditDialog.icon === 'ti-folder' ? '' : collectionEditDialog.icon
    try {
      if (collectionEditDialog.id) {
        await App.RenameCollection(collectionEditDialog.id, name, '', icon, collectionEditDialog.color)
      } else {
        await App.CreateCollection(name, '', icon, collectionEditDialog.color)
      }
      collectionEditDialog = null
      collectionsLoadedOnce = true
      await loadCollectionsList()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  // ── Видалення колекції (підтвердження — деструктивна дія) ──
  async function deleteCollectionPrompt(c: BrowserCollection) {
    const proceed = await confirmDialog(
      t('browser.saved.deleteCollectionTitle', { name: c.name }),
      t('browser.saved.deleteCollectionMessage'),
      { confirmLabel: t('browser.saved.deleteCollectionBtn'), cancelLabel: t('browser.installConfirm.cancel'), danger: true },
    )
    if (!proceed) return
    try {
      await App.DeleteCollection(c.id)
      collectionsList = collectionsList.filter(x => x.id !== c.id)
      if (openCollectionId === c.id) closeCollection()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  // ── Прибрати один запис з відкритої колекції ──
  async function removeFromOpenCollection(item: BrowserEntry) {
    if (!openCollectionId) return
    try {
      await App.RemoveFromCollection(openCollectionId, item.source, item.projectId || item.id)
      const key = item.source + '|' + (item.projectId || item.id)
      openCollectionItems = openCollectionItems.filter(e => (e.source + '|' + (e.projectId || e.id)) !== key)
      collectionsList = collectionsList.map(c => c.id === openCollectionId ? { ...c, itemCount: Math.max(0, c.itemCount - 1) } : c)
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  // ── Групове встановлення колекції: вибір збірки → план → підтвердження ──
  // Той самий принцип, що просив ужиток: «луанчер буде писати навіть не
  // підтримується такий-то мод у цій збірці — він може скасувати чи
  // продовжити». Три кроки: 1) обрати збірку, 2) показати план
  // (install/update/already/incompatible на кожен запис), 3) підтвердити
  // (або скасувати НЕсумісні записи й продовжити з рештою).
  let collectionInstallDialog = $state<{
    collectionId: string
    step: 'pick-pack' | 'plan' | 'installing' | 'done'
    // target — куди ставимо колекцію: збірка чи сервер (packId тоді = serverId).
    target: 'pack' | 'server'
    packId: string
    plan: CollectionInstallPlanItem[]
    loading: boolean
    results: BrowserInstallResult[]
  } | null>(null)

  function openCollectionInstallDialog(collectionId: string) {
    collectionInstallDialog = {
      collectionId, step: 'pick-pack', target: serverContext ? 'server' : 'pack',
      packId: serverContext ? serverContext.id : (packFilterId || (packs[0]?.id ?? '')),
      plan: [], loading: false, results: [],
    }
  }

  async function collectionInstallPickPack() {
    if (!collectionInstallDialog || !collectionInstallDialog.packId) return
    collectionInstallDialog = { ...collectionInstallDialog, loading: true }
    try {
      const plan = (collectionInstallDialog.target === 'server'
        ? await App.PlanCollectionInstallToServer(collectionInstallDialog.collectionId, collectionInstallDialog.packId)
        : await App.PlanCollectionInstall(collectionInstallDialog.collectionId, collectionInstallDialog.packId)) ?? []
      collectionInstallDialog = { ...collectionInstallDialog, step: 'plan', plan, loading: false }
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
      collectionInstallDialog = { ...collectionInstallDialog, loading: false }
    }
  }

  async function collectionInstallConfirm() {
    if (!collectionInstallDialog) return
    const dlg = collectionInstallDialog
    // Несумісні записи пропускаються автоматично («продовжити без них»,
    // а не «скасувати все») — гравець уже бачив план і натиснув
    // «Встановити»: якщо він хотів скасувати повністю, для цього є
    // окрема кнопка «Скасувати» в діалозі.
    const skip = dlg.plan.filter(p => p.status === 'incompatible').map(p => p.projectId)
    collectionInstallDialog = { ...dlg, step: 'installing', loading: true }
    try {
      const results = (dlg.target === 'server'
        ? await App.InstallCollectionToServer(dlg.collectionId, dlg.packId, skip)
        : await App.InstallCollectionToPack(dlg.collectionId, dlg.packId, skip)) ?? []
      collectionInstallDialog = { ...dlg, step: 'done', loading: false, results }
      loadInstalledMap()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
      collectionInstallDialog = { ...dlg, step: 'plan', loading: false }
    }
  }

  // ── Пошук / фільтри ──
  let query = $state('')
  // Приймає запит з ГЛОБАЛЬНОГО пошуку (Ctrl+K у шапці): браузер переходить
  // на звичайний пошук з введеним текстом (без фільтра збірки).
  $effect(() => {
    const onGS = (e: Event) => {
      const q = (e as CustomEvent<string>).detail
      if (!q) return
      query = q
      packFilterId = ''
      if (activeType !== 'mod') activeType = 'mod'
      sort = 'relevance'
      doSearch(1)
    }
    window.addEventListener('global-search-query', onGS)
    return () => window.removeEventListener('global-search-query', onGS)
  })
  // «Рекомендовані» — сорт за замовчуванням (Modrinth index=featured).
  let sort = $state('featured')

  // ── Режим відображення результатів ──
  //  detailed — поточна картка (іконка, теги, чіпи версії/лоадера, кнопки);
  //  grid     — сітка карток із банером зверху (як у Modrinth app);
  //  compact  — компактний список: один рядок на проєкт.
  // Запам'ятовується між запусками (localStorage). Ключ СПІЛЬНИЙ з
  // перемикачем у Налаштування → Вигляд (SettingsPage.svelte) — обидва
  // місця читають/пишуть той самий DISPLAY_MODE_KEY, а подія
  // 'cb-display-mode:changed' тримає їх у синхроні, поки ContentBrowser
  // лишається змонтованим (App.svelte монтує його один раз і просто
  // ховає — див. коментар там), тож самого localStorage.setItem з
  // іншого компонента замало: цей інстанс і так живий, і без слухача
  // просто не перечитав би нове значення, доки не перемонтується.
  type DisplayMode = 'detailed' | 'grid' | 'compact'
  const DISPLAY_MODE_KEY = 'shaurma.cbDisplayMode.v1'
  let displayMode = $state<DisplayMode>('detailed')
  try {
    const dm = localStorage.getItem(DISPLAY_MODE_KEY)
    if (dm === 'detailed' || dm === 'grid' || dm === 'compact') displayMode = dm
  } catch (e) { /* localStorage недоступний — режим просто не запам'ятається */ }
  $effect(() => {
    try { localStorage.setItem(DISPLAY_MODE_KEY, displayMode) } catch (e) { /* ignore */ }
    try { window.dispatchEvent(new CustomEvent('cb-display-mode:changed', { detail: displayMode })) } catch (e) { /* ignore */ }
  })
  $effect(() => {
    function onExternalChange(e: Event) {
      const dm = (e as CustomEvent).detail
      if ((dm === 'detailed' || dm === 'grid' || dm === 'compact') && dm !== displayMode) displayMode = dm
    }
    window.addEventListener('cb-display-mode:changed', onExternalChange)
    return () => window.removeEventListener('cb-display-mode:changed', onExternalChange)
  })
  const displayModes: { id: DisplayMode; icon: string; label: string }[] = [
    { id: 'detailed', icon: 'ti-layout-list', label: t('browser.view.detailed') },
    { id: 'grid', icon: 'ti-layout-grid', label: t('browser.view.grid') },
    { id: 'compact', icon: 'ti-list', label: t('browser.view.compact') },
  ]
  let sourceFilter = $state('all') // all | modrinth | curseforge
  let selectedGameVersion = $state('')
  let selectedLoader = $state('')
  // Modrinth-стиль фільтри: середовище (client/server) та «Open source».
  // Застосовуються лише до Modrinth-результатів (див. бекенд), тому групи
  // ховаємо, коли джерело — виключно CurseForge.
  let envClient = $state(false)
  let envServer = $state(false)
  let openSourceOnly = $state(false)

  // ── Схлопування груп фільтрів (акордеон) ──
  // Клік по заголовку блока ховає/показує його вміст — при багатьох
  // фільтрах непотрібні групи згортаються, щоб не тягнути сайдбар вниз.
  // Стан персистентний (localStorage), бо браузер контенту живе всю сесію
  // лаунчера і колапс «не забувається» між відкриттями сторінки.
  const COLLAPSE_STORAGE_KEY = 'shaurma.cbCollapsedFilters.v1'
  let collapsedFilters = $state<Record<string, boolean>>({})
  try {
    const raw = localStorage.getItem(COLLAPSE_STORAGE_KEY)
    if (raw) collapsedFilters = JSON.parse(raw) || {}
  } catch (e) { /* localStorage недоступний — акордеони просто не запам'ятаються */ }
  $effect(() => {
    try { localStorage.setItem(COLLAPSE_STORAGE_KEY, JSON.stringify(collapsedFilters)) } catch (e) { /* ignore */ }
  })
  function toggleFilterGroup(key: string) {
    collapsedFilters = { ...collapsedFilters, [key]: !collapsedFilters[key] }
  }
  function isCollapsed(key: string): boolean {
    return !!collapsedFilters[key]
  }
  // Вибрана збірка-фільтр: '' = без фільтра (вільний пошук, встановлення
  // через діалог вибору збірки). Не порожня = версія/лоадер закріплені
  // за збіркою, сумісний лише контент, встановлення одразу в неї.
  let packFilterId = $state(packContext?.id ?? '')
  // Ціль встановлення у браузері: збірка чи сервер. Може бути 'servers' і
  // без вибраного сервера (користувач лише розкрив список), але коли
  // serverContext заданий — режим завжди 'servers'.
  let pickMode = $state<'packs' | 'servers'>(serverContext ? 'servers' : 'packs')
  // Перемикач «Збірки ↔ Сервери» має сенс лише коли є сервери, що тримають
  // поточний вид контенту (моди → мод-ядра, плагіни → плагін-ядра). Інакше
  // вкладка «Сервери» була б порожньою — її взагалі не малюємо.
  const hasTargetServers = $derived(serversForTab(servers, activeType).length > 0)
  // Вибрана категорії-теги (з іконками) — OR всередині фасета. Зберігаємо
  // ПОВНИЙ обраний тег (не лише назву): Modrinth і CurseForge фільтрують
  // категорію РІЗНИМИ ключами — Modrinth за назвою/slug (searchVersion API
  // так влаштований офіційно), CurseForge — ВИКЛЮЧНО за числовим
  // categoryId. Раніше обидва джерела отримували той самий спільний рядок
  // назв (selectedCategories.join(',')) — для CurseForge це ніколи не
  // працювало як справжній фільтр API (назва — не ідентифікатор), і
  // фільтрація там відбувалась постфактум на вже обрізаній лімітом видачі
  // (баг: обраний тег часто давав порожній результат, хоча сумісних
  // проєктів під тегом було багато).
  let selectedCategories = $state<BrowserCategory[]>([])
  let categories = $state<BrowserCategory[]>([])
  // Рядок НАЗВ вибраних категорій — те, що йде у BrowserSearchModrinth
  // (Modrinth API фільтрує facet-ом за назвою/slug).
  const selectedCategoryNames = $derived(selectedCategories.map(c => c.name).join(','))
  // Рядок ЧИСЛОВИХ ID вибраних CurseForge-категорій — те, що йде у
  // BrowserSearchCurseForge (CF /mods/search фільтрує лише за categoryId).
  // Modrinth-категорії (cfId=0) тут не беруть участі: вибір тегу Modrinth
  // не повинен звужувати паралельний пошук по CurseForge.
  const selectedCFCategoryIds = $derived(
    selectedCategories.filter(c => c.source === 'curseforge' && c.cfId > 0).map(c => c.cfId).join(',')
  )

  let results = $state<BrowserSearchResult>({ modrinth: [], curseforge: [], cfDisabled: false, totalHits: 0, hasMore: false })
  // searching — «жодне з двох джерел ще не відповіло» (показуємо скелетон
  // на весь грід). searchingModrinth/searchingCurseForge — точний стан
  // КОЖНОГО джерела окремо: Modrinth і CurseForge пошук іде паралельно
  // і НЕ чекає одне одного (App.BrowserSearchModrinth/CurseForge — два
  // незалежні виклики), тож секція CurseForge може ще показувати «шукає…»
  // (наприклад, б'ється в rate limit CurseForge API), поки Modrinth уже
  // відрендерив свої картки — раніше єдиний прапорець searching тримав
  // ВЕСЬ грід на скелетоні, доки не відповість НАЙПОВІЛЬНІШЕ з двох джерел.
  // searchingModrinth/searchingCurseForge — точний стан КОЖНОГО джерела
  // окремо: Modrinth і CurseForge пошук іде паралельно і НЕ чекає одне
  // одного (App.BrowserSearchModrinth/CurseForge — два незалежні виклики).
  // resultsReadyModrinth/resultsReadyCurseForge — «це джерело вже
  // відповіло хоч раз для поточного пошуку» (на відміну від searching*,
  // не скидається на false між сторінками одного джерела — потрібне лише
  // для першого показу гріда замість скелетона).
  // true лише поки джерело бере участь у поточному пошуку (не вимкнене
  // фільтром джерела/відсутнім ключем) і ще не відповіло на ПОТОЧНИЙ
  // (не застарілий) запит.
  let searchingModrinth = $state(false)
  let searchingCurseForge = $state(false)
  // hasAnyResult — чи є вже хоч щось для показу (з будь-якого з двох
  // джерел) — щойно з'явився хоч один результат, загальний скелетон
  // ховається назавжди для цього пошуку, навіть якщо друге джерело ще
  // вантажиться (воно показує власний невеликий індикатор у своїй секції,
  // див. розмітку нижче).
  const hasAnyResult = $derived((results.modrinth?.length ?? 0) > 0 || (results.curseforge?.length ?? 0) > 0)
  // searching — показати загальний скелетон на весь грід. Умова: хоч
  // одне джерело ще шукає, І результатів для показу поки що нуль. Якщо
  // в пошуку бере участь лише одне джерело (фільтр звужений до
  // "modrinth" чи "curseforge") — друге ніколи не стає true, тож на
  // загальний прапорець не впливає.
  const searching = $derived((searchingModrinth || searchingCurseForge) && !hasAnyResult)
  let searched = $state(false)
  // Пагінація сторінками (замість «Показати ще»): offset = (page-1)*PAGE_SIZE.
  // currentPage скидається на 1 при будь-якій зміні запиту/фільтрів (нова
  // видача — новий пошук).
  const PAGE_SIZE = 30
  // Скільки чекати відповідь пошуку, перш ніж визнати, що вона не прийде
  // (див. withCallTimeout у wails.ts). Більше за 30-секундний таймаут
  // HTTP-клієнта бекенду — ловимо саме «завис», а не повільну мережу.
  const SEARCH_TIMEOUT_MS = 45000
  let offset = $state(0)
  let currentPage = $state(1)
  // Загальна кількість сторінок з бекенду (totalHits) — якщо бекенд не
  // дав точний totalHits, вважаємо що є ще хоча б одна сторінка, поки
  // hasMore === true.
  const totalPages = $derived.by(() => {
    if (results.totalHits > 0) return Math.max(1, Math.ceil(results.totalHits / PAGE_SIZE))
    return results.hasMore ? currentPage + 1 : currentPage
  })

  // Аналіз «вже встановлено» у збірці-фільтрі (по sha1, як Modrinth app):
  // мапа projectId → встановлений файл. Заповнюється коли packFilterId != ''.
  let installedMap = $state<Record<string, BrowserInstalledItem>>({})
  // installedMapLoading — поки true, картки показують скелетон бейджа
  // замість "немає"/"встановлено": раніше під час довантаження мапи
  // картки на короткий час показували неправильний ("не встановлено")
  // стан, який потім різко змінювався — вигладало як миготіння.
  let installedMapLoading = $state(false)
  // Прогрес встановлення (browser:progress): projectId → percent.
  let installProgress = $state<Record<string, number>>({})
  // Швидке встановлення з картки (без переходу на сторінку): projectId
  // встановлюваного контенту зараз (для дизейбла кнопки/показу спінера).
  let quickInstalling = $state<Record<string, boolean>>({})
  // Модалка вибору збірки, коли фільтра збірки немає («у яку збірку
  // встановити?») — та сама механіка, що packDialog на сторінці проєкту.
  // ВАЖЛИВО (запит «прибрати вибір версії зі сторінки»): вибір версії
  // живе ВСЕРЕДИНІ цього діалогу — юзер обирає версію контенту, а список
  // збірок фільтрується під саме цю версію (GetBrowserCompatiblePacks з
  // versionID). Без фільтра збірки — клік «Встановити» на картці відкриває
  // саме це; з фільтром збірки — встановлення йде одразу (без вікна).
  let packPickDialog = $state<{ entry: BrowserEntry; packs: BrowserCompatiblePack[]; versions: BrowserVersionView[]; loading: boolean; selectedVersionId: string } | null>(null)
  let packPickFilter = $state('')
  // Ціль у діалозі «Встановити»: збірки чи сервери. Сервери пропонуємо лише
  // для видів контенту, які сервер реально тримає (моди/плагіни), і лише ті
  // сервери, що їх підтримують (див. serversForTab).
  let packPickTab = $state<'packs' | 'servers'>('packs')
  // Для колекцій (можуть містити і моди, і плагіни) — сервери з підтримкою
  // хоч чогось: мод-ядра АБО плагін-ядра. Vanilla не підходить нікому.
  const collectionServers = $derived(
    servers.filter((s: BrowserServer) => serversForTab([s], 'mod').length > 0 || serversForTab([s], 'plugin').length > 0)
  )
  // Ефективний тип запису в діалозі: вкладка «Плагіни» → 'plugin',
  // «Моди» → 'mod' (навіть якщо API віддало для плагіна project_type
  // "mod"), інакше — власний тип запису. Від нього залежать і список
  // серверів, і вкладка «Збірки», яку для плагінів не показуємо.
  const packPickType = $derived(
    activeType === 'plugin' || activeType === 'mod' ? activeType : (packPickDialog?.entry.type ?? '')
  )
  const packPickServerKind = $derived(kindForTab(packPickType))
  const packPickServers = $derived(serversForTab(servers, packPickType))
  // Порядок id→індекс зі списку `packs` (уже прийшов у порядку sidebar —
  // закріплені спочатку, далі ручний порядок) — щоб діалог вибору збірки
  // сортував картки так само, як sidebar/«Мої збірки», а не як їх віддав
  // бекенд GetBrowserCompatiblePacks.
  const packOrderIndex = $derived.by(() => {
    const m = new Map<string, number>()
    packs.forEach((p: { id: string; name: string; mcVersion: string; loader: string; iconUrl?: string; icon?: string; color?: string }, i: number) => m.set(p.id, i))
    return m
  })
  const filteredPackPickPacks = $derived.by(() => {
    if (!packPickDialog) return []
    const q = packPickFilter.trim().toLowerCase()
    const list = packPickDialog.packs.filter(p => !p.whitelisted && (!q || p.name.toLowerCase().includes(q)))
    return list.slice().sort((a, b) => {
      const inst = Number(a.alreadyInstalled) - Number(b.alreadyInstalled)
      if (inst !== 0) return inst
      const ia = packOrderIndex.get(a.packId) ?? Number.MAX_SAFE_INTEGER
      const ib = packOrderIndex.get(b.packId) ?? Number.MAX_SAFE_INTEGER
      return ia - ib
    })
  })

  // Шаурма-збірки, де ЦЕЙ мод явно дозволений автором через Pack Manager
  // → Білий список (BrowserCompatiblePack.whitelisted, з бекенду —
  // GetBrowserCompatiblePacks/whitelistedShaurmaPacks). ВАЖЛИВО: звичайний
  // PackPicker-шлях нижче будується з packPicker (App.svelte), який
  // містить ЛИШЕ kind==='custom' збірки — шаурма-id туди фізично не
  // потрапляють, тож без цього окремого списку вони губились би з UI
  // повністю щойно в «Моїх збірках» була б хоч одна кастомна збірка
  // (фолбек-плаский-список показується лише коли filteredPackPicker
  // порожній). Показуємо окремою секцією — «як окремий перемикач типу
  // sidebar» (запит користувача), не змішуючи зі списком кастомних.
  const packPickWhitelistedPacks = $derived.by(() => {
    if (!packPickDialog) return []
    const q = packPickFilter.trim().toLowerCase()
    const list = packPickDialog.packs.filter(p => p.whitelisted && (!q || p.name.toLowerCase().includes(q)))
    return list.slice().sort((a, b) => Number(a.alreadyInstalled) - Number(b.alreadyInstalled))
  })

  // Модалка встановлення МОДПАКА з картки (клік по кнопці на картці):
  // вибір версії + перевірка «вже встановлено» (оновлення або нова копія).
  // Раніше кнопка на модпаку вела на сторінку проєкту — тепер показує
  // модульне вікно, як DownloadProject modal у Modrinth app.
  let modpackDialog = $state<{
    entry: BrowserEntry
    versions: BrowserVersionView[]
    loading: boolean
    selectedVersionId: string
    check: BrowserModpackInstallCheck | null
  } | null>(null)

  // Модалка вибору СВІТІВ для датапака (InstallDatapackToWorlds): коли
  // встановлюємо датапак, треба вказати, у які світи збірки його класти
  // (saves/<world>/datapacks). Мультивибір — юзер сам відмічає світи.
  // ВАЖЛИВО: працює І з фільтром збірки (activePack — світи саме цієї
  // збірки), І без нього (спершу вибір збірки в packPickDialog, потім —   // світи цієї збірки), як у ModpackPage. Раніше датапак з фільтром
   // збірки встановлювався ОДРАЗУ в datapacks/ збірки без вибору світів.
   let worldDialog = $state<{
     entry: BrowserEntry
     packId: string
     packName: string
     worlds: string[]
     loading: boolean
     selectedWorlds: string[]
     // versionId — обрана в діалозі версія датапака (передається в
     // InstallDatapackToWorlds, щоб встановлювалась саме вона).
     versionId: string
   } | null>(null)
   let worldInstalling = $state(false)
   // worldGlobal — встановити датапак у ЗАГАЛЬНУ теку datapacks збірки
   // (можна разом зі світами).
   let worldGlobal = $state(false)

  // Чи проскролені результати вниз: у верху сторінки sticky-панель і
  // topbar НЕ дублюють іконку/назву активної вкладки (вони й так видно в
  // табах), а при прокрутці вниз компактна плашка з іконкою+назвою
  // з'являється (як у ModpackPage: mp-topbar.collapsed).
  let cbScrolled = $state(false)

  // Список версій Minecraft для фільтра. Джерело — теги Modrinth
  // /tag/game_version (лише версії, для яких є контент), fallback — повний
  // Mojang-маніфест. Кожен елемент несе versionType (release/snapshot/...)
  // і дату — щоб групувати/сортувати як у Modrinth app.
  let mcVersionEntries = $state<BrowserGameVersion[]>([])
  let versionsLoaded = $state(false)
  // «Показати всі версії» — toggle, що розкриває снапшоти/старі версії
  // (за замовчуванням показуємо лише release, як у Modrinth app).
  let showAllVersions = $state(false)

  // Видимі версії для радіо-списку: при showAllVersions=false показуємо лише
  // стабільні release (найновіші спочатку), інакше — всі типи. Обмежуємо,
  // щоб список не розтягувався (Modrinth теж згортає старі версії).
  const visibleVersions = $derived(
    (showAllVersions
      ? mcVersionEntries
      : mcVersionEntries.filter(v => v.versionType === 'release')
    ).slice(0, 40)
  )

  // Сортування — той самий список, що dropdown у Modrinth (featured =
  // «Рекомендовані», за замовчуванням у Modrinth discover без запиту).
  const sortOptions = [
    { id: 'featured', label: t('browser.sort.featured') },
    { id: 'relevance', label: t('browser.sort.relevance') },
    { id: 'downloads', label: t('browser.sort.downloads') },
    { id: 'follows', label: t('browser.sort.follows') },
    { id: 'newest', label: t('browser.sort.newest') },
    { id: 'updated', label: t('browser.sort.updated') },
  ]

  // ── Лоадери (для фільтра — з /tag/loader Modrinth, з іконками) ────────
  // Динамічний список з бекенда; кожен тег має SupportedProjectTypes (для
  // яких типів контенту лоадер підходить). Fallback — статичний набір тих,
  // що лаунчер гарантовано знає. Відфільтрований по активному типу контенту
  // (як у Modrinth app: для модів — fabric/forge/neoforge/quilt, для шейдерів
  // — iris/optifine, тощо), з пріоритизацією дефолтних у верх.
  let loaderTags = $state<BrowserLoaderTag[]>([])
  let loadersLoaded = $state(false)

  const fallbackLoaderTags: BrowserLoaderTag[] = [
    { name: 'fabric', icon: '', types: ['mod'] },
    { name: 'forge', icon: '', types: ['mod'] },
    { name: 'neoforge', icon: '', types: ['mod'] },
    { name: 'quilt', icon: '', types: ['mod'] },
    { name: 'iris', icon: '', types: ['shader'] },
    { name: 'optifine', icon: '', types: ['shader'] },
    { name: 'vanilla', icon: '', types: ['mod', 'shader', 'datapack'] },
    // Плагіни: лише ядра, які лаунчер РЕАЛЬНО вміє ставити для сервера
    // (той самий перелік, що PLUGIN_CORES у servers/constants.ts — spigot/
    // bukkit прибрано звідси: лаунчер не встановлює їх як ядро сервера,
    // тож показувати їх у фільтрі «Лоадер» лише збивало б з пантелику).
    { name: 'paper', icon: '', types: ['plugin'] },
    { name: 'purpur', icon: '', types: ['plugin'] },
    { name: 'velocity', icon: '', types: ['plugin'] },
  ]

  // Локальна іконка за ім'ям лоадера; невідомі — загальний плуг-стиль.
  function loaderIcon(name: string) {
    switch (name) {
      case 'fabric': return fabricIcon
      case 'forge': return forgeIcon
      case 'neoforge': return neoforgeIcon
      case 'quilt': return quiltIcon
      // Серверні ядра (плагіни) — ті самі іконки, що в бібліотеці серверів.
      case 'paper': case 'purpur': case 'velocity': return CORE_META[name]?.icon ?? null
      default: return null
    }
  }

  function capitalizeLoader(name: string) {
    return name.charAt(0).toUpperCase() + name.slice(1)
  }

  const loaders = $derived.by(() => {
    const tags = loaderTags.length ? loaderTags : fallbackLoaderTags
    const priority = ['fabric', 'forge', 'neoforge', 'quilt', 'paper', 'purpur', 'velocity', 'spigot', 'bukkit', 'iris', 'optifine']
    const relevant = tags
      .filter(l => !l.types.length || l.types.includes(activeType))
      .map(l => ({
        id: l.name,
        label: capitalizeLoader(l.name),
        icon: loaderIcon(l.name),
      }))
      .sort((a, b) => {
        const pa = priority.indexOf(a.id)
        const pb = priority.indexOf(b.id)
        return (pa === -1 ? 99 : pa) - (pb === -1 ? 99 : pb) || a.label.localeCompare(b.label)
      })
    return [{ id: '', label: t('browser.loader.all'), icon: null }, ...relevant]
  })
  // Зміна вкладки типу контенту: вибраний лоадер міг стати недійсним
  // (fabric на «Плагінах», paper на «Модах») — скидаємо, інакше пошук
  // мовчки повертав би порожньо.
  $effect(() => {
    void activeType
    if (selectedLoader && !loaders.some(l => l.id === selectedLoader)) selectedLoader = ''
  })

  // Збірка-фільтр, що зараз активна (для бейджа і встановлення).
  // serverContext (сервер, не збірка) синтезує сумісний об'єкт напряму:
  // сервер не входить у список packs (це геть інша сутність на бекенді —
  // srvAPI, не customPacks), тож шукати serverContext.id серед packs
  // завжди дало б null. Коли serverContext заданий, він ЗАВЖДИ головний
  // (сторінка сервера відкриває браузер лише в цьому режимі — вибір іншої
  // "збірки" для серверного контексту не має сенсу, тому і фільтр по
  // packFilterId тут ігнорується).
  const activePack = $derived(
    serverContext
      ? { id: serverContext.id, name: serverContext.name, mcVersion: serverContext.mcVersion, loader: serverContext.loader, isServer: true }
      : packFilterId ? (packs.find((p: { id: string }) => p.id === packFilterId) ?? null) : null
  )
  // Мета активної вкладки (іконка/назва/колір) — для компактної плашки
  // у sticky-панелі інструментів при прокрутці вниз.
  const activeTypeMeta = $derived(contentTypes.find(c => c.id === activeType))

  // Фільтрований packPicker: для модів виключаємо ванільні збірки (vanilla/datapack/порожній
  // лоадер) — вони фізично не можуть запускати моди без мод-лоадера. Для
  // шейдерів/ресурспаків/датапаків ванільні лишаються (лоадер-агностичні).
  const filteredPackPicker = $derived.by(() => {
    if (!packPicker) return null
    if (activeType !== 'mod') return packPicker
    const excludedLoaders = new Set(['', 'vanilla', 'datapack'])
    const filteredRowById = new Map<string, any>()
    const filteredFolderById = new Map<string, any>()
    const allowedPackIds = new Set<string>()
    for (const [id, row] of packPicker.rowById) {
      const loader = (row.loader ?? '').toLowerCase()
      if (!excludedLoaders.has(loader)) {
        filteredRowById.set(id, row)
        allowedPackIds.add(id)
      }
    }
    for (const [id, folder] of packPicker.folderById) {
      const packIds = folder.packIds.filter((pid: string) => allowedPackIds.has(pid))
      if (packIds.length > 0) {
        filteredFolderById.set(id, { ...folder, packIds })
      }
    }
    const filteredItems = packPicker.items.filter((it: PackPickerItem) =>
      it.kind === 'folder' ? filteredFolderById.has(it.id) : filteredRowById.has(it.id)
    )
    return { items: filteredItems, rowById: filteredRowById, folderById: filteredFolderById }
  })

  // Ефективні версія/лоадер для ПОШУКУ. Коли активний фільтр збірки (а
  // вкладка — НЕ «Модпаки»), версія/лоадер жорстко беруться ЗІ ЗБІРКИ:
  // фільтри версії/лоадера в сайдбарі не працюють (вони приховані, див.
  // шаблон), бо встановлюємо контент у конкретну збірку.
  // Для «Модпаків» фільтр збірки взагалі не застосовується (модпак
  // встановлюється як ОКРЕМА нова збірка), тож там пошук вільний — за
  // користувацькими selectedGameVersion/selectedLoader.
  const searchVersion = $derived(
    activePack && activeType !== 'modpack' ? activePack.mcVersion : selectedGameVersion
  )
  const searchLoader = $derived(
    activePack && activeType !== 'modpack' ? activePack.loader : selectedLoader
  )
  // ID активної збірки-фільтра для пошуку — передається на бекенд, щоб
  // BrowserSearchModrinth/CurseForge могли перевірити, чи в цій збірці
  // встановлено Sinytra Connector, і, якщо так, розширити пошук на
  // Fabric-контент поряд з "рідним" лоадером збірки (Forge/NeoForge).
  // Порожньо для «Модпаків» (там фільтр збірки не застосовується взагалі).
  const searchPackID = $derived(
    activePack && activeType !== 'modpack' ? activePack.id : ''
  )

  // Середовище для ПОШУКУ. Коли ціль — сервер, а вкладка «Моди», сервер
  // не запустить чисто клієнтські моди (Sodium, Iris, мінімапи…) — вони
  // на Modrinth мають server_side:unsupported. Тому в цьому режимі
  // примусово вмикаємо серверний фасет (server_side optional/required),
  // незалежно від чекбокса «Середовище». Клієнтський чекбокс у цьому
  // режимі ігноруємо: він суперечив би цілі. Для плагінів фасет не
  // потрібен (плагін за визначенням серверний, а на Modrinth у них
  // client_side зазвичай unsupported — фасет міг би нічого не залишити).
  const serverTargetMods = $derived(!!serverContext && activeType === 'mod')
  const searchEnvClient = $derived(serverTargetMods ? false : envClient)
  const searchEnvServer = $derived(serverTargetMods ? true : envServer)

  // Синхронізація з контекстом збірки з App.svelte (кнопка «Шукати моди»
  // може спрацювати, поки браузер уже відкритий): оновлюємо фільтр і
  // перемикаємось на вкладку «Моди» (менеджер модів відкриває браузер саме
  // там, а не в «Модпаках»).
  //
  // ВАЖЛИВО: синк має відбуватись ЛИШЕ коли контекст РЕАЛЬНО змінився
  // (lastCtxId). Стара версія читала activeType/packFilterId прямо в тілі
  // ефекту — тому будь-який клік по табу («Модпаки»/«Шейдери»/…) одразу
  // повертав вкладку на «Моди», а зняття фільтра збірки (кнопка «×»/радіо
  // «Без фільтра») миттєво відновлювало його назад. Тепер вкладки
  // перемикаються вільно, а фільтр можна прибрати; контекст застосовується
  // лише при вході з іншої збірки.
  let lastCtxId = ''
  $effect(() => {
    const ctxId = packContext?.id ?? ''
    if (ctxId && ctxId !== lastCtxId) {
      lastCtxId = ctxId
      // Тип контенту з контексту (кнопка «Додати з браузера» у вкладці
      // «Контент»); порожній = «Моди», як раніше для «Шукати моди».
      const pt = packContext?.projectType ?? ''
      const want = ['mod', 'resourcepack', 'shader', 'datapack'].includes(pt) ? pt : 'mod'
      if (activeType !== want) activeType = want
      setPackFilter(ctxId)
    }
  })

  // Те саме для serverContext (кнопка «Знайти онлайн» на сторінці
  // сервера може спрацювати, поки браузер уже відкритий — наприклад,
  // користувач був у «Моди сервера А», перейшов на сторінку сервера Б і
  // знову натиснув «Знайти онлайн»). На відміну від packContext,
  // setPackFilter тут НЕ викликається: packFilterId лишається порожнім,
  // activePack для serverContext синтезується напряму (див. вище) —
  // достатньо виставити activeType і форсувати пошук/installedMap так
  // само, як setPackFilter робить для збірок.
  // Ціль встановлення: щойно з'явився serverContext (кнопка «Знайти
  // онлайн» або вибір сервера у фільтрі) — панель показує «Сервери».
  $effect(() => {
    if (serverContext) pickMode = 'servers'
  })
  // Плагіни збірки не тримають — на вкладці «Плагіни» ціль лише сервер.
  $effect(() => {
    if (activeType === 'plugin' && pickMode !== 'servers') pickMode = 'servers'
  })
  // Якщо для поточного типу контенту немає жодного сумісного сервера
  // (hasTargetServers === false) — автоматично повертаємось на «Збірки» й
  // ФІЛЬТР ЦІЛІ ВСТАНОВЛЕННЯ взагалі не згадує сервери (умова показу
  // вкладок «Збірки/Сервери» нижче — просто hasTargetServers, без
  // фолбеку на поточний pickMode). Раніше без цього ефекту людина, що
  // раніше вибирала «Сервери» на іншій вкладці, потрапляла на порожній
  // стан «Немає серверів з підтримкою…» замість того, щоб про сервери
  // тут узагалі не йшлось. Не чіпає «Плагіни» — там сервер ЄДИНА ціль.
  $effect(() => {
    if (activeType !== 'plugin' && !hasTargetServers && pickMode === 'servers') pickMode = 'packs'
  })

  let lastServerCtxId = ''
  $effect(() => {
    const ctxId = serverContext?.id ?? ''
    if (ctxId && ctxId !== lastServerCtxId) {
      lastServerCtxId = ctxId
      const want = serverContext?.kind === 'plugins' ? 'plugin' : 'mod'
      if (activeType !== want) activeType = want
      offset = 0
      currentPage = 1
      installedMap = {}
      searched = false
      doSearch(1)
      loadInstalledMap()
    }
  })

  // Коли браузер захований (інша сторінка/відкритий проєкт) — закриваємо
  // модалки вибору збірки/версії: вони інакше лишались би «відкритими» у
  // стані компонента і з'являлись назад при поверненні на сторінку пошуку.
  $effect(() => {
    if (!visible) {
      packPickDialog = null
      modpackDialog = null
      worldDialog = null
    }
  })

  // Коли змінилась збірка-фільтр: завантажуємо аналіз «вже встановлено»
  // (іконки на картках). Версію/лоадер і сам пошук уже скидає й
  // перезапускає setPackFilter — тут лишається тільки installedMap,
  // щоб не викликати doSearch() двічі підряд.
  $effect(() => {
    if (activePack) {
      loadInstalledMap()
    }
  })

  async function loadInstalledMap() {
    // serverContext: activePack.id — це serverId (синтезований вище), а
    // не packFilterId (той лишається порожнім — фільтру «збірка» для
    // серверного контексту не існує, сервер завжди один і фіксований).
    const filterId = serverContext ? (activePack?.id ?? '') : packFilterId
    if (!filterId) { installedMap = {}; installedMapLoading = false; return }
    installedMapLoading = true
    try {
      installedMap = serverContext
        ? (await App.GetServerBrowserInstalledMap(filterId)) ?? {}
        : (await App.GetBrowserInstalledMap(filterId)) ?? {}
    } catch (e) { console.error(e) } finally {
      installedMapLoading = false
    }
  }

  // Завантаження категорій для активного типу (з іконками).
  async function loadCategories() {
    try {
      categories = (await App.GetBrowserCategories(activeType)) ?? []
    } catch (e) { console.error(e) }
  }

  async function loadMCVersions() {
    if (versionsLoaded) return
    versionsLoaded = true
    try {
      const list = await App.GetBrowserGameVersions()
      mcVersionEntries = (list ?? []).filter(v => v && v.id)
    } catch (e) { console.error(e) }
  }

  async function loadLoaders() {
    if (loadersLoaded) return
    loadersLoaded = true
    try {
      loaderTags = (await App.GetBrowserLoaders()) ?? []
    } catch (e) { console.error(e) }
  }

  // Підписка на прогрес встановлення контенту (browser:progress).
  let offProgress: (() => void) | null = null
  $effect(() => {
    if (!offProgress) {
      offProgress = on('browser:progress', (p: BrowserProgress) => {
        if (p && p.projectId) {
          installProgress[p.projectId] = p.percent ?? 0
          installProgress = { ...installProgress }
        }
      })
    }
  })

  // Підписка на підтвердження сумісності (browser:compat): бекенд
  // повертає результат пошуку ОДРАЗУ з усіма хітами позначеними
  // compatibilityUnconfirmed=true (коли активний фільтр збірки), а
  // перевірку кожного окремого хіта шле в цю подію по мірі готовності —
  // без цього список довелось би тримати на скелетоні, поки НЕ ПЕРЕВІРИТЬСЯ
  // КОЖЕН хіт (те саме блокування, яке ми щойно прибрали для CurseForge).
  // Знімаємо позначку "перевіряється" для сумісних і ПРИБИРАЄМО з видачі
  // підтверджено несумісні — без перезапуску всього пошуку.
  let offCompat: (() => void) | null = null
  $effect(() => {
    if (!offCompat) {
      offCompat = on('browser:compat', (ev: BrowserCompatEvent) => {
        if (!ev || !ev.projectId) return
        const applyTo = (list: BrowserEntry[]): { changed: boolean; next: BrowserEntry[] } => {
          // ВАЖЛИВО: бекенд шле projectId — числовий/hex ID проєкту
          // (entry.ProjectID), а НЕ slug (entry.ID). Для CurseForge вони
          // майже завжди різні (ID = slug, напр. "jei", ProjectID =
          // "238222"), тож порівняння з e.id тут ніколи не збігалось —
          // подія browser:compat для CurseForge мовчки ігнорувалась:
          // бейдж "перевіряється" не знімався і підтверджено несумісні
          // картки не приховувались. Порівнюємо тим самим ключем, що й
          // усюди в цьому файлі (entryKey: projectId || id).
          const idx = list.findIndex(e => e.source === ev.source && entryKey(e) === ev.projectId)
          if (idx === -1) return { changed: false, next: list }
          if (!ev.compatible) {
            // Підтверджено несумісний — прибираємо картку зі списку
            // (той самий результат, що й старий синхронний фільтр давав
            // одразу, лише тепер не блокуючи показ решти).
            const next = list.slice()
            next.splice(idx, 1)
            return { changed: true, next }
          }
          // Сумісний (або перевірка не змогла підтвердити через мережеву
          // помилку — fail-open, лишаємо видимим) — просто знімаємо
          // позначку "перевіряється"/ставимо unconfirmed за потреби.
          const next = list.slice()
          next[idx] = { ...next[idx], compatibilityChecking: false, compatibilityUnconfirmed: false }
          return { changed: true, next }
        }
        const mr = applyTo(results.modrinth ?? [])
        const cf = applyTo(results.curseforge ?? [])
        if (mr.changed || cf.changed) {
          results = { ...results, modrinth: mr.next, curseforge: cf.next }
        }
      })
    }
  })

  // Множина ключів закладок — завантажується РАЗ при монтуванні браузера
  // (не залежить від activeType/пошуку), щоб зірочка на картках звичайних
  // результатів пошуку одразу знала свій стан без окремого запиту на
  // кожну картку (див. коментар на bookmarkedKeys вище).
  let bookmarksInited = false
  $effect(() => {
    if (!bookmarksInited) {
      bookmarksInited = true
      loadBookmarkedKeys()
    }
  })

  // Пошук конкретної сторінки (page — 1-based). Пустий запит +
  // «Рекомендовані» = featured-контент (як Modrinth discover без запиту),
  // тож не блокуємо пошук пустим query.
  //
  // РАСЕ-ЗАХИСТ (searchSeq): пошуки можуть перекриватись — користувач
  // ввів новий запит і натиснув Enter, поки попередній запит ще летить
  // (мережа повільна), або клікнув фільтр/сторінку. Раніше відповідь
  // СТАРІШОГО запиту могла прийти ПІСЛЯ новішого і затерти його
  // результати + передчасно зняти searching (спінер зникав, а запит ще
  // не завершився). Тепер кожен запит бере свій номер; відповідь зі
  // застарілим номером відкидається повністю (не чіпає results і не
  // знімає searching) — на екрані завжди результати ОСТАННЬОГО пошуку.
  //
  // НЕЗАЛЕЖНІСТЬ ДЖЕРЕЛ: Modrinth і CurseForge викликаються ДВОМА окремими
  // запитами (App.BrowserSearchModrinth / App.BrowserSearchCurseForge), не
  // через один await App.BrowserSearch(...). Раніше єдиний виклик чекав
  // ОБИДВА джерела разом — коли CurseForge гальмував (rate limit 4
  // запити/с, повільна мережа), спінер висів навіть на давно готових
  // результатах Modrinth. Тепер кожне джерело оновлює СВОЮ частину
  // results щойно відповість, незалежно від того, як довго відповідає
  // інше — жодного очікування CurseForge для показу Modrinth, і навпаки.
  let searchSeq = 0
  // Тип контенту останнього запущеного пошуку (не $state — лише для порівняння).
  let lastSearchType = ''
  async function doSearch(page: number = 1) {
    const seq = ++searchSeq
    currentPage = page
    offset = (page - 1) * PAGE_SIZE
    searched = true
    // Зміна ТИПУ контенту: старі картки іншого типу не можна лишати на
    // екрані «поки вантажиться нове» (stale-while-revalidate доречний лише
    // в межах того ж типу). Інакше на CurseForge (повільний rate limit)
    // під новою вкладкою висіли ті самі проєкти, що й під попередньою.
    const typeChanged = lastSearchType !== '' && lastSearchType !== activeType
    lastSearchType = activeType
    const q = query.trim()
    // Записуємо в історію лише СПРАВЖНІЙ новий пошук (сторінка 1, є
    // текст запиту) — не пагінацію (goToPage передає конкретний номер
    // сторінки, тут завжди був би page>1) і не «Рекомендовані» без тексту.
    const isFreshQuerySearch = page === 1 && q !== ''

    const wantModrinth = sourceFilter === '' || sourceFilter === 'all' || sourceFilter === 'modrinth'
    const wantCurseForge = sourceFilter === '' || sourceFilter === 'all' || sourceFilter === 'curseforge'

    searchingModrinth = wantModrinth
    searchingCurseForge = wantCurseForge	    // Скидаємо ОБИДВА джерела незалежно від того, яке бере участь:
	    // раніше при виключному фільтрі (curseforge/modrinth) невибране
	    // джерело НЕ очищалось, і на екрані лишались СТАРІ результати
	    // іншого джерела — «вибрав CurseForge, а бачу Modrinth».
	    // ШВИДКІСТЬ (stale-while-revalidate): НЕ обнуляємо списки на старті
	    // пошуку — старі картки лишаються на екрані, поки не прийдуть нові
	    // (як у GDLauncher). Раніше `modrinth: []` показувало порожній
	    // екран/скелетон на кожен клік по фільтру/сторінці. Обнуляємо лише
	    // джерело, яке в цьому пошуку НЕ бере участі (щоб не лишались
	    // результати вимкненого джерела), і помилки.
	    results = {
	      ...results,
	      modrinth: wantModrinth && !typeChanged ? results.modrinth : [],
	      modrinthError: undefined, smartSearchTerms: undefined,
	      curseforge: wantCurseForge && !typeChanged ? results.curseforge : [],
	      cfError: undefined,
	    }

    let modrinthCount = 0
    let curseforgeCount = 0

    // Сторожовий таймаут (див. wails.ts, withCallTimeout): якщо відповідь
    // не прийде ЗОВСІМ, список лишається в стані завантаження назавжди — без
    // помилки й без можливості повторити. Тепер через SEARCH_TIMEOUT_MS
    // показуємо причину (results.modrinthError) і кнопку «Спробувати ще»;
    // сам запит дозавершиться у фоні, а seq-перевірки відкинуть застарілу
    // відповідь.
    const modrinthPromise = !wantModrinth ? null :
      withCallTimeout(App.BrowserSearchModrinth(
        query.trim(), activeType,
        searchVersion, searchLoader,
        selectedCategoryNames, sort,
        searchEnvClient, searchEnvServer, openSourceOnly,
        PAGE_SIZE, offset,
        searchPackID,
      ), SEARCH_TIMEOUT_MS, t('browser.timeout')).then((res) => {
        if (seq !== searchSeq) return // застарілий запит — ігноруємо
        modrinthCount = res?.modrinth?.length ?? 0
        results = {
          ...results,
          modrinth: res?.modrinth ?? [],
          modrinthError: res?.modrinthError,
          totalHits: res?.totalHits ?? results.totalHits,
          hasMore: res?.hasMore ?? results.hasMore,
          // Modrinth і CurseForge можуть незалежно розпізнати той самий
          // запит як тему — беремо перше непорожнє (обидва однакові за
          // словником, дублювання підказки на екрані не потрібне).
          smartSearchTerms: res?.smartSearchTerms?.length ? res.smartSearchTerms : results.smartSearchTerms,
        }
      }).catch((e: any) => {
        if (seq !== searchSeq) return
        // Раніше тут стояло `modrinth: []` — одна невдала спроба (офлайн,
        // тимчасовий 429 від Modrinth, обрив TLS) ПОРОЖНИЛА вже показаний
        // список, і браузер виглядав так, ніби «проєкти перестали
        // завантажуватись», хоча впав лише один запит. Стабільність:
        // лишаємо попередні результати на екрані, а причину показуємо
        // інлайн через results.modrinthError (порожній список + помилка →
        // блок «немає з'єднання» з кнопкою повтору).
        results = { ...results, modrinthError: String(e) }
        toast(t('error.withMessage', { message: e }), 'error')
      }).finally(() => {
        if (seq === searchSeq) searchingModrinth = false
      })

    // Серверна ціль + «Моди»: CurseForge не має фасета середовища, тож
    // відсіюємо суто клієнтські моди на бекенді за тегами файлів.
    const cfSearch = serverTargetMods ? App.BrowserSearchCurseForgeForServer : App.BrowserSearchCurseForge
    const curseforgePromise = !wantCurseForge ? null :
      withCallTimeout(cfSearch(
        query.trim(), activeType,
        searchVersion, searchLoader,
        selectedCFCategoryIds, sort,
        PAGE_SIZE,
        searchPackID,
      ), SEARCH_TIMEOUT_MS, t('browser.timeout')).then((res) => {
        if (seq !== searchSeq) return
        curseforgeCount = res?.curseforge?.length ?? 0
        results = {
          ...results,
          curseforge: res?.curseforge ?? [],
          cfDisabled: res?.cfDisabled ?? false,
          cfError: res?.cfError,
          smartSearchTerms: res?.smartSearchTerms?.length ? res.smartSearchTerms : results.smartSearchTerms,
        }
      }).catch((e: any) => {
        if (seq !== searchSeq) return
        // CurseForge — другорядне джерело: мережева помилка саме тут не
        // повинна показувати toast поверх уже готових результатів
        // Modrinth (виглядало б як помилка всього пошуку). Повідомлення
        // видно у секції CurseForge через results.cfError.
        results = { ...results, curseforge: [], cfError: String(e) }
      }).finally(() => {
        if (seq === searchSeq) searchingCurseForge = false
      })

    // Історія («останні пошукові запити, які дали результат»): пишемо
    // ЛИШЕ коли ЦЕ справжній новий пошук (не пагінація/фільтр) і коли
    // ЦЕЙ запит (seq) усе ще актуальний на момент завершення обох джерел
    // — застарілий, перекритий новішим пошуком запит не повинен
    // потрапити в історію заднім числом.
    if (isFreshQuerySearch) {
      Promise.allSettled([modrinthPromise, curseforgePromise]).then(() => {
        if (seq !== searchSeq) return
        const total = modrinthCount + curseforgeCount
        if (total > 0) {
          App.AddSearchHistory(q, activeType, total).catch((e: any) => console.error(e))
          // Оновлюємо локальний список одразу, без повторного запиту —
          // той самий дедуп за (query, projectType), що на бекенді
          // (store.AddSearchHistory), щоб список у випадаючому меню не
          // розходився з тим, що щойно записано.
          const qLower = q.toLowerCase()
          searchHistory = [
            { query: q, projectType: activeType, resultCount: total, searchedAt: new Date().toISOString() },
            ...searchHistory.filter(h => !(h.query.toLowerCase() === qLower && h.projectType === activeType)),
          ]
          searchHistoryLoaded = true
        }
      })
    }
  }

  // Перехід між сторінками (кнопки зверху і знизу списку) замість
  // «Показати ще» — кожна сторінка замінює попередню видачу, а не
  // додається до неї.
  function goToPage(page: number) {
    if (page < 1 || page === currentPage || searching) return
    doSearch(page)
  }

  // ── Слайдер активного розділу (таби зверху) ───────────────────────────
  // Активна «пілюля» не міняється миттєво, а їде до нового таба (позиція і
  // ширина міряються з DOM активної кнопки). Перший показ — без анімації.
  let tabsEl = $state<HTMLElement | null>(null)
  let ind = $state({ x: 0, w: 0, c: '', saved: false, anim: false })
  function measureTabInd() {
    const el = tabsEl?.querySelector<HTMLElement>('.cb-tab.active')
    if (!el) { ind = { ...ind, w: 0 }; return }
    const c = getComputedStyle(el).getPropertyValue('--tb-c').trim()
    const first = ind.w === 0 && !ind.anim
    ind = { x: el.offsetLeft, w: el.offsetWidth, c, saved: el.classList.contains('cb-tab-saved'), anim: ind.anim }
    if (first) requestAnimationFrame(() => { ind = { ...ind, anim: true } })
  }
  $effect(() => {
    void viewMode; void activeType; void contentTypes.length
    tick().then(measureTabInd)
  })
  $effect(() => {
    if (!tabsEl) return
    const ro = new ResizeObserver(() => measureTabInd())
    ro.observe(tabsEl)
    return () => ro.disconnect()
  })

  // Перемикання типу контенту (таби зверху і чіпи в сайдбарі — одна логіка).
  function selectContentType(id: string) {
    backToSearch()
    // Теги/категорії належать конкретному типу контенту (CurseForge —
    // числові categoryId свого classId). Перенесені на інший тип, вони
    // ламали видачу CurseForge (чужий категорійний фільтр), тож скидаємо.
    if (activeType !== id) selectedCategories = []
    activeType = id
    // Якщо переключились на "Моди" і обрана збірка — ванільна, скидаємо фільтр
    if (id === 'mod' && packFilterId) {
      const loader = (packs.find((p: { id: string; loader?: string }) => p.id === packFilterId)?.loader ?? '').toLowerCase()
      if (['', 'vanilla', 'datapack'].includes(loader)) {
        packFilterId = ''
      }
    }
    // Ціль — сервер: вкладка визначає ЛОАДЕР сервера (modLoader чи pluginLoader),
    // тож просимо App перерахувати контекст. Вкладки, яких сервер не тримає
    // (шейдери/ресурспаки/датапаки/модпаки), знімають серверний контекст.
    if (serverContext) {
      const want = kindForTab(id)
      if (!want) {
        onClearServerContext?.()
        pickMode = 'packs'
      } else if (serverContext.kind !== want) {
        // Сервер, що не підтримує нову вкладку, НЕ лишаємо ціллю — інакше
        // пошук і встановлення далі йшли б під його ядро.
        const srv = servers.find((x: BrowserServer) => x.id === serverContext.id)
        if (srv && serversForTab([srv], id).length > 0) {
          onPickServer?.(serverContext.id, want)
        } else {
          onClearServerContext?.()
        }
      }
    }
  }

  // ── Обробники нового сайдбара (кожна зміна фільтра одразу оновлює пошук) ──
  function pickGameVersion(v: string) { selectedGameVersion = v; doSearch(1) }
  function pickLoader(id: string) { selectedLoader = selectedLoader === id ? '' : id; doSearch(1) }
  function pickSource(v: string) { sourceFilter = v; doSearch(1) }
  function toggleEnv(which: 'client' | 'server') {
    if (which === 'client') envClient = !envClient; else envServer = !envServer
    doSearch(1)
  }
  function toggleOpenSource() { openSourceOnly = !openSourceOnly; doSearch(1) }
  // Опції селекта версій: «Будь-яка» + видимі; поточна версія завжди присутня.
  const gameVersionOptions = $derived.by(() => {
    const opts = [{ value: '', label: t('browser.anyVersion') }, ...visibleVersions.map(v => ({ value: v.id, label: v.id }))]
    if (selectedGameVersion && !opts.some(o => o.value === selectedGameVersion)) {
      opts.splice(1, 0, { value: selectedGameVersion, label: selectedGameVersion })
    }
    return opts
  })

  function resetFilters() {
    query = ''
    selectedGameVersion = ''
    selectedLoader = ''
    sort = 'featured'
    selectedCategories = []
    envClient = false
    envServer = false
    openSourceOnly = false
    // «Без фільтра» теж означає «без цілі-сервера».
    if (serverContext) onClearServerContext?.()
    pickMode = 'packs'
    packFilterId = ''
    installedMap = {}
    offset = 0
    currentPage = 1
    results = { modrinth: [], curseforge: [], cfDisabled: false, totalHits: 0, hasMore: false }
    searched = false
    // «Скинути фільтри» розгортає й згорнуті групи акордеона — інакше
    // фільтри скидаються, а сайдбар лишається схлопнутим, і здається,
    // що нічого не сталося (рев'ю: асиметрія resetFilters vs акордеони).
    collapsedFilters = {}
  }

  // setPackFilter — єдина точка зміни фільтра збірки: скидає все, що
  // успадковане/залежне від попередньої збірки (сторінку пошуку,
  // installedMap), і перезапускає пошук під новий контекст. Версію/лоадер
  // для пошуку НЕ чіпаємо тут — їх підставляють derived searchVersion/
  // searchLoader з активної збірки (коли вкладка не «Модпаки»), тож вони
  // ніколи не «протікають» у вільний пошук модпаків. Раніше зміна
  // packFilterId (перевибір збірки у фільтрі, або вхід через packContext з
  // менеджера модів) не скидала ці поля — фільтри/результати лишались від
  // попередньої збірки, поки користувач не натискав "Пошук" вручну.
  function setPackFilter(id: string) {
    // Вибір збірки завжди знімає серверний контекст — інакше браузер
    // продовжив би встановлювати в сервер, хоч у фільтрі стоїть збірка
    // (serverContext має пріоритет над packFilterId у activePack).
    if (serverContext) onClearServerContext?.()
    packFilterId = id
    offset = 0
    currentPage = 1
    installedMap = {}
    searched = false
    doSearch(1)
    loadInstalledMap()
  }

  function contentDirFor(type: string): string {
    switch (type) {
      case 'resourcepack': return 'resourcepacks'
      case 'shader': return 'shaderpacks'
      case 'datapack': return 'datapacks'
      default: return 'mods'
    }
  }

  function entryKey(entry: BrowserEntry): string {
    return entry.projectId || entry.id
  }

  // Стан кнопки на картці, коли обрано фільтр збірки:
  //   none   → нічого не встановлено: «Встановити»
  //   update → у збірці вже є цей мод: «Оновити/Перестановити» (завжди
  //     тягне найновішу сумісну версію; беквзгляду точну версію не
  //     порівнюємо тут — це робить сторінка проєкту).
  function packButtonState(entry: BrowserEntry): 'none' | 'update' {
    if (!activePack) return 'none'
    return installedMap[entryKey(entry)] ? 'update' : 'none'
  }

  // Клік по кнопці на картці (не по картці загалом):
  //   - є фільтр збірки → підтвердити (install/update) і встановити одразу
  //     в неї (найновіша сумісна версія — versionID='' в
  //     InstallBrowserContentVersion);
  //   - фільтра немає → відкрити модалку вибору збірки (клік по кнопці НЕ
  //     відкриває сторінку проєкту — сторінка відкривається лише кліком по
  //     самій картці, поза кнопкою).
  async function quickInstallClick(e: MouseEvent, entry: BrowserEntry) {
    e.stopPropagation()
    // Сумісність цього елемента з активною збіркою НЕ підтверджена (мережева
    // помилка фільтра пошуку, fail-open — елемент лишили у видачі, але без
    // гарантії). Попереджаємо ОКРЕМИМ діалогом перед звичайним install/
    // update-підтвердженням: раніше юзер дізнавався про (не)сумісність лише
    // після кліку, з тексту помилки InstallBrowserContentVersion — тепер
    // видно заздалегідь, з можливістю все одно спробувати (реальну перевірку
    // все одно робить бекенд при встановленні).
    // Діалог «Сумісність не підтверджена» ПРИБРАНО: він змушував щоразу
    // натискати «Спробувати», а бекенд (InstallBrowserContentVersion /
    // InstallServerBrowserVersion) і так перевіряє сумісність перед
    // завантаженням та повертає зрозумілу помилку, якщо версії немає.
    // Значок «Не підтверджено» на кнопці лишається як тиха підказка.
    if (entry.type === 'modpack') {
      // Модпак: модульне вікно вибору версії + «вже встановлено»
      // (оновити або створити копію) — як DownloadProject modal у Modrinth
      // app, а не перехід на сторінку.
      await openModpackDialog(entry)
      return
    }
    if (entry.type === 'datapack') {
      // Датапак ЗАВЖДИ йде через вибір світів: з фільтром збірки — світи
      // саме цієї збірки, без фільтра — спершу вибір збірки (packPickDialog),
      // а після вибору — світи (worldDialog). Раніше з фільтром збірки
      // датапак встановлювався одразу без вибору світів, і юзер не міг
      // вказати, у які світи його класти.
      if (activePack) {
        await openWorldDialog(activePack.id, activePack.name, entry)
        return
      }
      await openPackPickDialog(entry)
      return
    }
    if (activePack) {
      const key = entryKey(entry)
      const installed = installedMap[key]
      const ok = await confirmInstallOrUpdate(entry.name, entry.type, activePack.id, entry.source, key, installed)
      if (!ok) return
      await installQuick(activePack.id, entry, !!installed, '', !!serverContext)
      return
    }
    await openPackPickDialog(entry)
  }

  // Відкриває модалку вибору світів для датапака (світи цієї збірки).
  async function openWorldDialog(packId: string, packName: string, entry: BrowserEntry, versionId = '') {
    worldGlobal = false
    worldDialog = { entry, packId, packName, worlds: [], loading: true, selectedWorlds: [], versionId }
    try {
      const worlds = (await App.ListInstanceWorlds(packId)) ?? []
      if (worldDialog && worldDialog.packId === packId) {
        worldDialog.worlds = worlds
      }
    } catch (e: any) {
      // Помилка списку світів — модалку НЕ закриваємо (вікно вибору світів
      // має відкриватись завжди): лишаємо порожній список із підказкою, а
      // причину показуємо тостом, щоб користувач бачив, що сталося.
      if (worldDialog && worldDialog.packId === packId) worldDialog.worlds = []
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      if (worldDialog && worldDialog.packId === packId) worldDialog.loading = false
    }
  }

  function toggleWorld(w: string) {
    if (!worldDialog) return
    worldDialog.selectedWorlds = worldDialog.selectedWorlds.includes(w)
      ? worldDialog.selectedWorlds.filter(x => x !== w)
      : [...worldDialog.selectedWorlds, w]
  }

  function selectAllWorlds(on: boolean) {
    if (!worldDialog) return
    worldDialog.selectedWorlds = on ? [...worldDialog.worlds] : []
  }

  async function installDatapackToWorlds() {
    const d = worldDialog
    if (!d || (!d.selectedWorlds.length && !worldGlobal) || worldInstalling) return
    worldInstalling = true
    try {
      // 1) Загальна тека datapacks збірки (якщо позначено).
      if (worldGlobal) {
        await App.InstallBrowserContentVersion(d.packId, d.entry.source, entryKey(d.entry), d.versionId, 'datapacks', false)
      }
      // 2) Окремі світи (якщо вибрані).
      if (d.selectedWorlds.length > 0) {
        const res = await App.InstallDatapackToWorlds(d.packId, d.selectedWorlds, d.entry.source, entryKey(d.entry), d.versionId)
        toast(res?.message || t('browser.installed'), 'success')
      } else {
        toast(t('browser.installed'), 'success')
      }
      worldDialog = null
      if (d.packId === packFilterId) await loadInstalledMap()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      worldInstalling = false
    }
  }

  // Кнопка «Встановити» у packPickDialog: для датапака після вибору
  // збірки одразу відкриваємо вибір світів ЦІЄЇ збірки (замість прямого
  // встановлення в datapacks/), для решти контенту — звичайне встановлення.
  // Встановлюється САМЕ обрана в діалозі версія (dialog.selectedVersionId),
  // а не «остання сумісна».
  // «Встановити» на сервер з діалогу вибору цілі. Версію підбирає бекенд
  // (InstallServerBrowserVersion) під MC-версію і ядро сервера.
  async function installToPickedServer(srv: BrowserServer) {
    const dialog = packPickDialog
    if (!dialog) return
    dialog.loading = true
    try {
      await installQuick(srv.id, dialog.entry, false, dialog.selectedVersionId, true)
      packPickDialog = null
    } finally {
      if (packPickDialog) packPickDialog.loading = false
    }
  }

  async function installToPickedPack(packId: string) {
    const dialog = packPickDialog
    if (!dialog) return
    // '' = «Авто» — InstallBrowserContentVersion сам підбере найкращу версію
    // під (mc, loader) збірки; конкретна версія встановлюється саме вона.
    const versionId = dialog.selectedVersionId
    if (dialog.entry.type === 'datapack') {
      const p = dialog.packs.find(x => x.packId === packId)
      packPickDialog = null
      // Для датапака версія обов'язкова (InstallDatapackToWorlds), тож при
      // «Авто» резолвимо рекомендовану під обрану збірку перед вибором світів.
      // БАГ «вікно вибору світів не відкривається»: раніше помилка
      // резолву версії (напр. збій API) йшла без try/catch — клік мовчки
      // губився, і жодне вікно не показувалось. Тепер помилка видима.
      try {
        let vv = versionId
        if (!vv) {
          const rec = await App.GetBrowserRecommendedVersion(dialog.entry.source, entryKey(dialog.entry), 'datapack', p?.mcVersion ?? '', p?.loader ?? '')
          vv = rec?.id ?? ''
        }
        await openWorldDialog(packId, p?.name ?? packId, dialog.entry, vv)
      } catch (e: any) {
        toast(t('error.withMessage', { message: e }), 'error')
      }
      return
    }
    dialog.loading = true
    try {
      await installQuick(packId, dialog.entry, false, versionId)
      packPickDialog = null
    } finally {
      if (packPickDialog) packPickDialog.loading = false
    }
  }

  // Модалка встановлення модпака (клік по кнопці на картці модпака):
  // завантажує версії + перевірку «уже в Моїх збірках», показує вибір
  // версії і кнопку встановлення; якщо модпак уже встановлений —
  // пропонує оновити наявну збірку або створити нову копію.
  async function openModpackDialog(entry: BrowserEntry) {
    modpackDialog = { entry, versions: [], loading: true, selectedVersionId: '', check: null }
    try {
      const [versions, check] = await Promise.all([
        App.GetBrowserProjectVersions(entry.source, entryKey(entry), 'modpack', '', ''),
        App.CheckBrowserModpackInstalled(entry.source, entryKey(entry)),
      ])
      if (!modpackDialog || modpackDialog.entry !== entry) return
      modpackDialog.versions = versions ?? []
      modpackDialog.check = check || null
      // Рекомендована версія: release → beta → alpha (як Modrinth).
      const rel = modpackDialog.versions.find(v => v.versionType === 'release')
      const beta = modpackDialog.versions.find(v => v.versionType === 'beta')
      const alpha = modpackDialog.versions.find(v => v.versionType === 'alpha')
      modpackDialog.selectedVersionId = (rel ?? beta ?? alpha ?? modpackDialog.versions[0])?.id ?? ''
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
      modpackDialog = null
    } finally {
      if (modpackDialog) modpackDialog.loading = false
    }
  }

  function modpackSelectedVersion(): BrowserVersionView | null {
    if (!modpackDialog) return null
    return modpackDialog.versions.find(v => v.id === modpackDialog!.selectedVersionId) ?? null
  }

  function modpackVersionLabel(v: BrowserVersionView): string {
    const mc = (v.gameVersions ?? []).join(', ')
    const ld = (v.loaders ?? [])[0] ?? ''
    return `${v.name || v.versionNumber}${mc ? ' · ' + mc : ''}${ld ? ' · ' + ld : ''}`
  }

  // «Встановити» модпак: створює картку «не завантажено» і качає архів.
  // updateExisting — оновити вже встановлену збірку (UpdatePackFromSource).
  async function installModpack(entry: BrowserEntry, updateExisting: boolean) {
    const d = modpackDialog
    if (!d || !d.selectedVersionId) return
    d.loading = true
    try {
      const key = entryKey(entry)
      if (updateExisting && d.check?.existingPackId) {
        // Оновлення НАЯВНОЇ збірки до обраної версії (diff-імпорт, як
        // «Оновити з джерела» через 3 крапки на картці збірки).
        await App.UpdatePackFromSource(d.check.existingPackId, d.selectedVersionId)
        toast(t('browser.modpackDialog.updated', { name: d.check.existingPackName || entry.name }), 'success')
        modpackDialog = null
      } else {
        // Нова встановлення/копія: CreateBrowserModpack (force=false для
        // нового, force=true для свідомої копії, коли користувач обрав
        // «Створити копію» попри те, що модпак уже є).
        const force = !!d.check?.alreadyInstalled
        const created = await App.CreateBrowserModpack(entry.source, key, d.selectedVersionId, force)
        await App.DownloadBrowserModpack(created.id)
        toast(t('browser.project.installedAs', { name: entry.name }), 'success')
        modpackDialog = null
        // Одразу на сторінку новоствореної збірки — там видно живий
        // прогрес качання (раніше лишались у браузері з тостом).
        onOpenPack?.(created.id)
        return
      }
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      if (modpackDialog) modpackDialog.loading = false
    }
  }

  // confirmInstallOrUpdate — модалка підтвердження перед встановленням/
  // оновленням мода з картки/сторінки. Раніше клік одразу тягнув
  // найновішу версію без жодного підтвердження — незручно, якщо
  // клікнули випадково або не помітили, що це оновлення, а не перше
  // встановлення. Три випадки: не встановлено → «Встановити?»;
  // встановлена стара версія → «Оновити з X до Y?»; уже актуальна →
  // повідомляємо і не встановлюємо повторно без явного другого кліку.
  async function confirmInstallOrUpdate(
    name: string, projectType: string, packId: string, source: string, projectId: string,
    installed: BrowserInstalledItem | undefined,
  ): Promise<boolean> {
    if (!installed) {
      return confirmDialog(
        t('browser.installConfirm.installTitle', { name }), '',
        { confirmLabel: t('browser.installConfirm.installBtn'), cancelLabel: t('browser.installConfirm.cancel'), danger: false },
      )
    }
    // Дізнаємось найновішу сумісну версію (та сама, яку встановить
    // versionID='' у InstallBrowserContentVersion), щоб показати, з якої
    // саме версії на яку йде оновлення — або сказати, що вже актуально.
    // projectType важливий: GetBrowserProjectVersions не фільтрує за
    // лоадером збірки для ресурспаків/датапаків/шейдерів.
    let latestVersionNumber = ''
    try {
      const versions = await App.GetBrowserProjectVersions(source, projectId, projectType, activePack?.mcVersion ?? '', activePack?.loader ?? '')
      latestVersionNumber = versions?.[0]?.versionNumber ?? ''
    } catch (e) { console.error(e) }
    if (latestVersionNumber && latestVersionNumber === installed.versionNumber) {
      toast(t('browser.installConfirm.alreadyUpToDate'), 'info')
      return false
    }
    return confirmDialog(
      t('browser.installConfirm.updateTitle', { name }),
      // versionNumber може бути порожнім, коли стара версія знайдена за
      // slug з імені файлу (sha1 не резолвився) — показуємо «—» замість
      // порожнього рядка в «Поточна версія:».
      t('browser.installConfirm.updateMessage', { from: installed.versionNumber || '—', to: latestVersionNumber || '?' }),
      { confirmLabel: t('browser.installConfirm.updateBtn'), cancelLabel: t('browser.installConfirm.cancel'), danger: false },
    )
  }

  // isServer — див. serverContext вище: та сама картка, інший бекенд-метод
  // (App.InstallServerBrowserVersion замість InstallBrowserContentVersion),
  // бо на Go-стороні збірки і сервери — різні сутності з різним API.
  async function installQuick(packId: string, entry: BrowserEntry, replace: boolean, versionId = '', isServer = false) {
    const key = entryKey(entry)
    quickInstalling = { ...quickInstalling, [key]: true }
    try {
      const res = isServer
        ? await App.InstallServerBrowserVersion(
            // kind — з ВКЛАДКИ браузера (де користувач знайшов запис), а
            // не з entry.type: Modrinth для Paper/Velocity-плагінів нерідко
            // віддає project_type="mod", і тоді плагін ставився як «мод»
            // → помилка «ядро velocity не підтримує моди».
            packId, entry.source, key, versionId, kindForTab(activeType) ?? (entry.type === 'plugin' ? 'plugins' : 'mods'), replace,
          )
        : await App.InstallBrowserContentVersion(
            packId, entry.source, key, versionId, contentDirFor(entry.type), replace,
          )
      // res.message — «Встановлено» + авто-встановлені залежності.
      toast(res?.message || t('browser.installed'), 'success')
      if (serverContext ? packId === serverContext.id : packId === packFilterId) await loadInstalledMap()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      const next = { ...quickInstalling }
      delete next[key]
      quickInstalling = next
    }
  }

  // Відкриває модалку вибору збірки (без фільтра збірки).
  //
  // ВАЖЛИВО (запит «загальна кнопка встановлення → усі сумісні збірки»):
  // за замовчуванням selectedVersionId='' — бекенд повертає ВСІ сумісні
  // збірки (будь-яка версія/лоадер, які підтримує автор контенту), а
  // версію під обрану збірку підбирає сам лаунчер при встановленні.
  // Якщо користувач вибере конкретну версію — список звужується під неї.
  async function openPackPickDialog(entry: BrowserEntry) {
    packPickFilter = ''
    // Плагін збірки не тримають — одразу вкладка «Сервери»; решта — «Збірки».
    packPickTab = (activeType === 'plugin' || entry.type === 'plugin') ? 'servers' : 'packs'
    packPickDialog = { entry, packs: [], versions: [], loading: true, selectedVersionId: '' }
    const dialog = packPickDialog
    try {
      // Версії НЕ завантажуємо: вибір версії прибрано з UI — завжди «авто»
      // (selectedVersionId=''), лаунчер підбирає версію під (mc, loader)
      // обраної збірки при встановленні. Раніше спершу йшов запит версій,
      // і лише потім — сумісних збірок: модалка «спочатку показувала
      // немає збірки, а потім список». Тепер одразу запит збірок.
      const list = (await App.GetBrowserCompatiblePacks(entry.source, entryKey(entry), entry.type, contentDirFor(entry.type), '')) ?? []
      // Модалка могла закритись (packPickDialog=null) або відкритись інший
      // діалог, поки йшов запит сумісних збірок — застосовуємо результат
      // лише до того самого діалога, який відкривали.
      if (packPickDialog === dialog) packPickDialog.packs = list
    } catch (e: any) {
      if (packPickDialog === dialog) {
        toast(t('error.withMessage', { message: e }), 'error')
        packPickDialog = null
      }
    } finally {
      if (packPickDialog === dialog) packPickDialog.loading = false
    }
  }

  function typeIcon(type: string): string {
    return contentTypes.find(c => c.id === type)?.icon || 'ti-cube'
  }

  function formatDownloads(n: number): string {
    if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M'
    if (n >= 1000) return (n / 1000).toFixed(1) + 'K'
    return String(n)
  }

  function isInstalled(entry: BrowserEntry): boolean {
    // serverContext: filter-прапорцем тут виступає сам activePack (packFilterId
    // лишається порожнім для серверного контексту — див. коментар біля
    // loadInstalledMap), тож перевіряємо activePack, а не лише packFilterId.
    const hasFilter = serverContext ? !!activePack : packFilterId !== ''
    return hasFilter && !!installedMap[entry.projectId || entry.id]
  }

  function progressFor(entry: BrowserEntry): number | null {
    const p = installProgress[entry.projectId || entry.id]
    return p === undefined || p >= 100 ? null : p
  }

  // Об'єднаний список карток (Modrinth + CurseForge в ОДНОМУ гріді, як
  // просив ужиток: «контентфорж і модрінг мають бути в одному списку, але
  // з іконками і написами звідки воно»). Кожна картка несе власний бейдж
  // джерела (лого + підпис MR/CF), сортування — по downloads спадаюче
  // (окрім sort='featured', де лишаємо порядок API кожного джерела:
  // Modrinth featured спершу, CurseForge featured — одразу після).
  const mergedResults = $derived.by(() => {
    const merged: BrowserEntry[] = [...(results.modrinth ?? []), ...(results.curseforge ?? [])]
    // Дедуп за (source, id): той самий мод може прийти двічі з одного API,
    // і {#each (key)} Svelte 5 впав би з each_key_duplicate на дублікаті.
    const seen = new Set<string>()
    // Крос-сервісний дедуп за назвою з ПРІОРИТЕТОМ Modrinth: той самий
    // проєкт, що є і в Modrinth, і в CurseForge, показуємо лише ОДИН раз
    // (з Modrinth). Modrinth-записи йдуть першими в merged, тож CurseForge-
    // дублікат просто пропускається. Назви нормалізуємо (нижній регістр,
    // без зайвих пробілів), щоб «A. E.» і «a.e.» не розмножували картки.
    const seenName = new Set<string>()
    const uniq: BrowserEntry[] = []
    for (const e of merged) {
      const k = (e.source || '') + '|' + (e.id || '')
      if (seen.has(k)) continue
      seen.add(k)
      const nameK = (e.name || '').toLowerCase().replace(/\s+/g, ' ').trim()
      if (nameK && e.source === 'curseforge' && seenName.has(nameK)) continue
      if (nameK && e.source === 'modrinth') seenName.add(nameK)
      uniq.push(e)
    }
    if (sort !== 'featured' && sort !== 'relevance') {
      uniq.sort((a, b) => (b.downloads ?? 0) - (a.downloads ?? 0))
    }
    return uniq
  })

  function toggleCategory(cat: BrowserCategory) {
    const idx = selectedCategories.findIndex(c => c.source === cat.source && c.name === cat.name)
    if (idx !== -1) {
      selectedCategories = selectedCategories.filter((_, i) => i !== idx)
    } else {
      selectedCategories = [...selectedCategories, cat]
    }
  }

  function isCategorySelected(cat: BrowserCategory): boolean {
    return selectedCategories.some(c => c.source === cat.source && c.name === cat.name)
  }

  function categoryIcon(c: BrowserCategory): string {
    return c.icon || ''
  }

  // Порядок і підписи груп категорій (як заголовки акордіонів у
  // SearchSidebarFilter Modrinth). «Оптимізація» з Modrinth і CurseForge
  // потрапляють в одну групу performance-impact завдяки Header з бекенду.
  const categoryHeaderOrder = ['category', 'feature', 'performance-impact', 'resolutions']
  function categoryHeaderLabel(header: string): string {
    return t('browser.categoryHeader.' + header) || header
  }

  // Групуємо категорії по header і сортуємо назви всередині групи —
  // так фільтр не «стрибає» і однакові за змістом теги з різних джерел
  // видно поруч (MR/CF бейджі різняться, назва та сама).
  const groupedCategories = $derived.by(() => {
    const groups = new Map<string, BrowserCategory[]>()
    for (const c of categories) {
      const h = c.header || 'category'
      if (!groups.has(h)) groups.set(h, [])
      groups.get(h)!.push(c)
    }
    for (const list of groups.values()) {
      list.sort((a, b) => a.name.localeCompare(b.name) || (a.source < b.source ? -1 : 1))
    }
    const orderedKeys = [
      ...categoryHeaderOrder.filter(h => groups.has(h)),
      ...[...groups.keys()].filter(h => !categoryHeaderOrder.includes(h)),
    ]
    return orderedKeys.map(h => ({ header: h, items: groups.get(h)! }))
  })

  async function doSearchWithEnter(e: any) {
    if (e.key === 'Enter') doSearch()
  }

  // Відкрити сторінку проєкту — для ВСІХ типів контенту (клік по картці
  // ніколи не встановлює одразу; встановлення — зі сторінки проєкту).
  //
  // ВАЖЛИВО (фікс «фільтр збірки не застосовується на сторінці проєкту»):
  // якщо користувач на цій сторінці сам обрав фільтр збірки (activePack —
  // радіо-кнопки «Показати для збірки X»), це передається на сторінку
  // проєкту ЯВНО. Раніше тут ішов лише (source, projectId) — сторінка
  // проєкту отримувала packContext ЛИШЕ якщо його задали ЗОВНІ (кнопка
  // «Шукати моди» з конкретної збірки), а власний вибір користувача в
  // фільтрі губився при переході на картку → сторінка проєкту не знала,
  // під яку збірку підбирати версію, і не могла показати «вже
  // встановлено», навіть коли фільтр збірки був явно обраний.
  function openEntry(entry: BrowserEntry) {
    const ctx = serverContext
      ? { id: serverContext.id, name: serverContext.name, mcVersion: serverContext.mcVersion, loader: serverContext.loader, isServer: true }
      : packContext ?? (activePack
        ? { id: activePack.id, name: activePack.name, mcVersion: activePack.mcVersion, loader: activePack.loader }
        : null)
    openProject?.(entry.source, entry.projectId || entry.id, ctx)
  }

  // Первинне завантаження: «Рекомендовані» одразу (як Modrinth discover).
  // Ефект залежить від activeType (читається через loadCategories) — тому
  // виконується один раз при монтуванні і повторно при зміні таба типу.
  // ВАЖЛИВО: жоден $state тут не пишеться (самопис у $effect викликає
  // перезапуск ефекту, а його cleanup скасовує запланований пошук).
  $effect(() => {
    loadMCVersions()
    loadLoaders()
    loadCategories()
    searched = false
    currentPage = 1
    const id = setTimeout(() => doSearch(1), 0)
    return () => clearTimeout(id)
  })

  // Скрол результатів (результати скроляться у .cb-main): при прокрутці
  // вниз показуємо в sticky-панелі іконку+назву активної вкладки (а у
  // верху — ні) і сповіщаємо App.svelte, щоб topbar поводився так само.
  // visible у залежностях — щоб при поверненні на сторінку стан перечитався
  // (скрол .cb-main зберігається, бо компонент не перемонтовується).
  // Скрол результатів (результати скроляться у .cb-main): при прокрутці
  // вниз показуємо в sticky-панелі іконку+назву активної вкладки (а у
  // верху — ні) і сповіщаємо App.svelte, щоб topbar поводився так само.
  // visible у залежностях — щоб при поверненні на сторінку стан перечитався
  // (скрол .cb-main зберігається, бо компонент не перемонтовується).
  $effect(() => {
    const v = visible
    const main = document.querySelector('.cb-wrap .cb-main') as HTMLElement | null
    if (!main) return
    const onScr = () => {
      const s = main.scrollTop > 24
      if (s !== cbScrolled) {
        cbScrolled = s
        window.dispatchEvent(new CustomEvent('browser:scroll', { detail: { scrolled: s } }))
      }
    }
    main.addEventListener('scroll', onScr, { passive: true })
    onScr()
    return () => main.removeEventListener('scroll', onScr)
  })
</script>

{#snippet cbCard(item: BrowserEntry, opts?: { removable?: boolean; versionBadge?: string })}
  {@const state = packButtonState(item)}
  {@const busy = !!quickInstalling[entryKey(item)]}
  {@const bmKey = item.source + '|' + (item.projectId || item.id)}
  {@const mode = displayMode}
  {@const srcName = item.source === 'curseforge' ? 'CurseForge' : 'Modrinth'}
  {@const srcIcon = item.source === 'curseforge' ? curseforgeIcon : modrinthIcon}
  <div class="cb-card" onclick={() => openEntry(item)} role="button" tabindex="0">
    {#if mode === 'grid'}
      <!-- Банер (Modrinth featured_gallery / CurseForge screenshot); без нього — градієнт -->
      <div class="cb-card-banner">
        {#if item.bannerUrl}
          <img class="cb-card-banner-img" src={item.bannerUrl} alt="" loading="lazy" decoding="async" use:smartImage />
        {/if}
        <span class="cb-srcbadge" title={srcName}><img src={srcIcon} alt={srcName} loading="lazy" /></span>
      </div>
    {/if}
    {#if item.iconUrl}
      <img class="cb-card-ic" src={item.iconUrl} alt="" loading="lazy" use:smartImage />
    {:else}
      <div class="cb-card-ic cb-card-ic-fallback"><i class="ti {typeIcon(item.type)}"></i></div>
    {/if}
    <div class="cb-card-body">
      <div class="cb-card-name">
        {#if mode === 'detailed'}
          <img class="cb-src-logo" src={srcIcon} alt={srcName} title={srcName} loading="lazy" />
        {/if}
        <span class="cb-card-title">{item.name}</span>
        {#if mode !== 'detailed' && state === 'update'}
          <i class="ti ti-circle-check-filled cb-card-installed" title={t('browser.installed.short')}></i>
        {/if}
        {#if mode === 'compact'}
          <span class="cb-srcbadge" title={srcName}><img src={srcIcon} alt={srcName} loading="lazy" /></span>
        {:else if mode === 'grid'}
          <span class="cb-card-dl"><i class="ti ti-download"></i> {formatDownloads(item.downloads)}</span>
        {/if}
      </div>
      {#if mode === 'detailed'}
        <div class="cb-card-tags">
          <span class="cb-card-type">{item.type}</span>
          <span class="cb-card-src {item.source === 'curseforge' ? 'cf' : 'mr'}">
            <img src={srcIcon} alt="" loading="lazy" />
            {srcName}
          </span>
          {#if opts?.versionBadge}
            <span class="chip"><i class="ti ti-tag"></i> {opts.versionBadge}</span>
          {/if}
        </div>
      {/if}
      <div class="cb-card-desc">{item.description}</div>
      {#if mode === 'detailed'}
        <div class="cb-card-meta">
          {#if item.loader}<span class="chip chip-l"><i class="ti ti-brand-git"></i> {item.loader}</span>{/if}
          {#if item.mcVersion}<span class="chip"><i class="ti ti-cube"></i> {item.mcVersion}</span>{/if}
          {#if activePack && installedMapLoading}
            <span class="chip cb-chip-skeleton"><i class="ti ti-loader spin"></i> …</span>
          {:else if state === 'update'}
            <span class="chip g"><i class="ti ti-check"></i> {t('browser.installed.short')}</span>
          {/if}
        </div>
      {:else if mode === 'compact'}
        <div class="cb-card-sub">
          {#if item.author}<span><i class="ti ti-user"></i> {item.author}</span>{/if}
          <span><i class="ti ti-download"></i> {formatDownloads(item.downloads)}</span>
          {#if opts?.versionBadge}<span><i class="ti ti-tag"></i> {opts.versionBadge}</span>{/if}
        </div>
      {/if}
    </div>
    <div class="cb-card-side">
      {#if mode === 'grid'}
        <span class="cb-card-author">
          {#if item.author}<i class="ti ti-user"></i> {item.author}{/if}
          {#if opts?.versionBadge}<i class="ti ti-tag"></i> {opts.versionBadge}{/if}
        </span>
      {/if}
      <!-- Ряд дій-міток (зірочка + колекція + видалення): один горизонтальний
           ряд фіксованої висоти — стовпчик не росте з кожною новою кнопкою.
           У режимі «Сітка» ряд лежить поверх банера й з'являється при наведенні. -->
      <div class="cb-card-actions">
        <!-- Зірочка закладки: клік НЕ відкриває сторінку проєкту і НЕ запускає встановлення. -->
        <button
          class="cb-bookmark-btn"
          class:on={bookmarkedKeys.has(bmKey)}
          title={bookmarkedKeys.has(bmKey) ? t('browser.saved.removeBookmark') : t('browser.saved.addBookmark')}
          onclick={(e) => { e.stopPropagation(); toggleBookmark(item) }}
        >
          <i class="ti {bookmarkedKeys.has(bmKey) ? 'ti-star-filled' : 'ti-star'}"></i>
        </button>
        {#if item.type !== 'modpack'}
          <!-- Модпаки НЕ додаються в колекції: модпак — сама окрема збірка.
               Бекенд теж відхиляє modpack в AddToCollection. -->
          <button
            class="cb-bookmark-btn"
            title={t('browser.saved.addToCollection')}
            onclick={(e) => openAddToCollectionMenu(e, item)}
          >
            <i class="ti ti-folder-plus"></i>
          </button>
        {/if}
        {#if opts?.removable}
          <button
            class="cb-bookmark-btn"
            title={t('browser.saved.removeFromCollection')}
            onclick={(e) => { e.stopPropagation(); removeFromOpenCollection(item) }}
          >
            <i class="ti ti-x"></i>
          </button>
        {/if}
      </div>
      {#if mode === 'detailed'}
        <div class="cb-downloads"><i class="ti ti-download"></i> {formatDownloads(item.downloads)}</div>
      {/if}
      {#if progressFor(item) !== null}
        <div class="cb-progress-mini"><div class="cb-progress-bar"><i style={`width:${progressFor(item)}%`}></i></div></div>
      {/if}
      <button
        class="btn btn-primary btn-sm cb-install-btn"
        disabled={busy || (!!activePack && installedMapLoading)}
        onclick={(e) => quickInstallClick(e, item)}
      >
        {#if busy}
          <i class="ti ti-loader spin"></i> {t('browser.install.installing')}
        {:else if activePack && installedMapLoading}
          <i class="ti ti-loader spin"></i>
        {:else if state === 'update'}
          <i class="ti ti-refresh"></i> {t('browser.install.reinstall')}
        {:else}
          <i class="ti ti-download"></i> {t('browser.install')}
        {/if}
      </button>
    </div>
  </div>
{/snippet}


<div class="cb-wrap" data-view={displayMode}>
  <!-- Контекст збірки (кнопка «Шукати моди» в менеджері модів) -->
  {#if packContext}
    <div class="cb-context-bar">
      <i class="ti ti-box"></i>
      <span>{t('browser.contextFor', { pack: packContext.name })}</span>
      <span class="cb-ctx-meta">{packContext.loader} {packContext.mcVersion}</span>
    </div>
  {:else if serverContext}
    <!-- Контекст сервера (кнопка «Знайти онлайн» у менеджері модів/плагінів
         сторінки сервера) — той самий бейдж, що для збірки, з іконкою
         сервера замість пакунка; без кнопки «Змінити»/«×», бо сервер тут
         єдиний і фіксований (на відміну від packContext, де можна вибрати
         іншу збірку або зняти фільтр). -->
    <div class="cb-context-bar">
      <i class="ti ti-server-2"></i>
      <span>{t('browser.contextForServer', { server: serverContext.name })}</span>
      <span class="cb-ctx-meta">{serverContext.loader} {serverContext.mcVersion}</span>
    </div>
  {/if}

  <!-- Таби типів контенту (як NavTabs у Modrinth discover) -->
  <div class="cb-tabs" class:has-ind={ind.w > 0} bind:this={tabsEl}>
    <span class="cb-tab-ind" class:anim={ind.anim} class:saved={ind.saved} aria-hidden="true"
          style="transform:translateX({ind.x}px);width:{ind.w}px;--ind-c:{ind.c || 'var(--orange)'};opacity:{ind.w > 0 ? 1 : 0}"></span>
    <!-- «Збережено» — окремий розділ (закладки + колекції), не тип
         контенту. Раніше стояв ОСТАННІМ у ряду з margin-left:auto (візуально
         відірваний праворуч) і був стилізований як звичайна pill-кнопка —
         користувачі не знаходили розділ. Тепер: перший зліва (найпомітніша
         позиція) і з постійним (не лише на active/hover) кольоровим фоном,
         щоб виділятись як самостійний розділ навігації, а не губитись серед
         однакових табів типів контенту. -->
    <button
      class="cb-tab cb-tab-saved"
      class:active={viewMode === 'saved'}
      onclick={openSavedView}
    >
      <i class="ti ti-star"></i>
      <span>{t('browser.saved.tab')}</span>
    </button>
    {#if contentTypes.length > 1}
      <div class="cb-tabs-sep" aria-hidden="true"></div>
      {#each contentTypes as ct}
        <button
          class="cb-tab"
          class:active={viewMode === 'search' && activeType === ct.id}
          style="--tb-c:{ct.color}"
          onclick={() => selectContentType(ct.id)}
        >
          <i class="ti {ct.icon}"></i>
          <span>{ct.label}</span>
        </button>
      {/each}
    {/if}
  </div>

  {#if viewMode === 'saved'}
    <div class="cb-saved" in:fly={{ y: 12, duration: 260, easing: cubicOut }}>
      <div class="cb-saved-subtabs">
        <button class="pill" class:on={savedTab === 'bookmarks'} onclick={() => switchSavedTab('bookmarks')}>
          <i class="ti ti-star"></i> {t('browser.saved.bookmarks')}
        </button>
        <button class="pill" class:on={savedTab === 'collections'} onclick={() => switchSavedTab('collections')}>
          <i class="ti ti-folder"></i> {t('browser.saved.collections')}
        </button>
        <button class="btn btn-ghost btn-sm cb-saved-back" onclick={backToSearch}>
          <i class="ti ti-arrow-left"></i> {t('browser.saved.backToSearch')}
        </button>
      </div>

      {#if savedTab === 'bookmarks'}
        {#if loadingBookmarks}
          <div class="cb-saved-loading"><i class="ti ti-loader spin"></i> {t('loading')}</div>
        {:else if bookmarkedEntries.length === 0}
          <div class="cb-empty">
            <i class="ti ti-star"></i>
            <p>{t('browser.saved.emptyBookmarks')}</p>
          </div>
        {:else}
          <!-- Перемикач типу контенту всередині закладок: пілюля для
               кожного типу, що РЕАЛЬНО є серед закладок (savedTypesPresent),
               + «Усі». Раніше такого перемикача не було взагалі — довгий
               список закладок показувався одним суцільним грідом без
               можливості відфільтрувати саме шейдери чи саме моди. -->
          {#if savedTypesPresent.size > 1}
            <div class="cb-saved-type-pills">
              <button class="pill" class:on={savedTypeFilter === ''} onclick={() => savedTypeFilter = ''}>
                {t('browser.saved.allTypes')}
              </button>
              {#each contentTypes as ct (ct.id)}
                {#if savedTypesPresent.has(ct.id)}
                  <button class="pill" class:on={savedTypeFilter === ct.id} onclick={() => savedTypeFilter = ct.id}>
                    <i class="ti {ct.icon}"></i> {ct.label}
                  </button>
                {/if}
              {/each}
            </div>
          {/if}
          {#if savedFilteredEntries.length === 0}
            <div class="cb-empty sm"><i class="ti ti-search-off"></i><div>{t('browser.noPackMatch')}</div></div>
          {:else}
            <div class="cb-grid">
              {#each savedFilteredEntries as item (item.source + '|' + item.id)}
                {@render cbCard(item)}
              {/each}
            </div>
          {/if}
        {/if}
      {:else if openCollectionId}
        <!-- Вміст відкритої колекції -->
        <div class="cb-collection-header">
          <button class="btn btn-ghost btn-sm" onclick={closeCollection}><i class="ti ti-arrow-left"></i> {t('browser.saved.collections')}</button>
          <h3>{collectionsList.find(c => c.id === openCollectionId)?.name ?? ''}</h3>
          <button class="btn btn-primary btn-sm" onclick={() => openCollectionInstallDialog(openCollectionId)} disabled={openCollectionItems.length === 0}>
            <i class="ti ti-download"></i> {t('browser.saved.installCollection')}
          </button>
        </div>
        {#if loadingCollectionItems}
          <div class="cb-saved-loading"><i class="ti ti-loader spin"></i> {t('loading')}</div>
        {:else if openCollectionItems.length === 0}
          <div class="cb-empty">
            <i class="ti ti-folder"></i>
            <p>{t('browser.saved.emptyCollection')}</p>
          </div>
        {:else}
          <div class="cb-grid">
            {#each openCollectionItems as item (item.source + '|' + item.id)}
              {@render cbCard(item, { removable: true, versionBadge: openCollectionVersions[item.source + '|' + (item.projectId || item.id)]?.versionNumber })}
            {/each}
          </div>
        {/if}
      {:else}
        <!-- Список колекцій -->
        <div class="cb-collections-toolbar">
          <button class="btn btn-primary btn-sm" onclick={createCollectionPrompt}>
            <i class="ti ti-plus"></i> {t('browser.saved.newCollection')}
          </button>
        </div>
        {#if loadingCollections}
          <div class="cb-saved-loading"><i class="ti ti-loader spin"></i> {t('loading')}</div>
        {:else if collectionsList.length === 0}
          <div class="cb-empty">
            <i class="ti ti-folder"></i>
            <p>{t('browser.saved.emptyCollections')}</p>
          </div>
        {:else}
          <div class="cb-collections-grid">
            {#each collectionsList as c (c.id)}
              {@const cIcon = c.icon || 'ti-folder'}
              <div class="cb-collection-card" style="--pcol:{c.color || '#7b4dff'}" onclick={() => openCollection(c.id)} role="button" tabindex="0">
                <div class="cb-collection-icon">
                  <i class="ti {cIcon}"></i>
                </div>
                <div class="cb-collection-name">{c.name}</div>
                <div class="cb-collection-count">{t('browser.saved.itemCount', { count: c.itemCount })}</div>
                <div class="cb-collection-actions">
                  <button class="btn btn-ghost btn-xs" onclick={(e: Event) => { e.stopPropagation(); renameCollectionPrompt(c) }} title={t('browser.saved.renameCollection')}>
                    <i class="ti ti-pencil"></i>
                  </button>
                  <button class="btn btn-ghost btn-xs cb-collection-delete" onclick={(e: Event) => { e.stopPropagation(); deleteCollectionPrompt(c) }} title={t('browser.saved.deleteCollectionBtn')}>
                    <i class="ti ti-trash"></i>
                  </button>
                </div>
              </div>
            {/each}
          </div>
        {/if}
      {/if}
    </div>
  {:else}

  <div class="cb-layout" class:filters-open={filtersOpen} in:fly={{ y: 12, duration: 260, easing: cubicOut }}>
    <!-- Ліва колонка: результати -->
    <div class="cb-main">
      <!-- Рядок пошуку + сорт (як у layout.vue Modrinth: StyledInput + Combobox) -->
      <div class="cb-toolbar">
        <!-- Компактна плашка активної вкладки (іконка + назва): у верху
             сторінки прихована (іконки/назви й так видно в табах),
             з'являється лише коли результати проскролені вниз. -->
        <div class="cb-toolbar-type {cbScrolled ? 'show' : ''}" style="--tb-c:{activeTypeMeta?.color || 'var(--orange)'}">
          <i class="ti {activeTypeMeta?.icon || 'ti-cube'}"></i>
          <span>{activeTypeMeta?.label ?? ''}</span>
        </div>
        <div class="cb-search">
          <i class="ti ti-search"></i>
          <input
            placeholder={t('browser.searchPlaceholderType', { type: contentTypes.find(c => c.id === activeType)?.label ?? '' })}
            bind:value={query}
            onkeydown={doSearchWithEnter}
            onfocus={openSearchHistoryDropdown}
            onblur={() => setTimeout(() => { showSearchHistory = false }, 160)}
          />
          {#if query}
            <button class="cb-search-clear" title={t('browser.clearSearch')} onclick={() => { query = ''; doSearch(1) }}><i class="ti ti-x"></i></button>
          {/if}
          {#if showSearchHistory && !query && searchHistoryForType.length > 0}
            <!-- Історія пошуку («останні запити, що дали результат»):
                 показується лише коли поле порожнє й у фокусі — той
                 самий UX, що адресний рядок браузера. onmousedown
                 preventDefault на рядках — інакше клік спершу викликав
                 би onblur (закриття списку), а не сам клік по рядку. -->
            <div class="cb-search-history">
              <div class="cb-search-history-head">
                <span>{t('browser.history.title')}</span>
                <button class="cb-search-history-clear" onmousedown={(e: Event) => e.preventDefault()} onclick={clearSearchHistoryAll}>
                  {t('browser.history.clear')}
                </button>
              </div>
              {#each searchHistoryForType as item (item.query + '|' + item.projectType)}
                <div
                  class="cb-search-history-item"
                  onmousedown={(e: Event) => e.preventDefault()}
                  onclick={() => useSearchHistoryItem(item)}
                  role="button"
                  tabindex="0"
                >
                  <i class="ti ti-history"></i>
                  <span class="cb-search-history-q">{item.query}</span>
                  <span class="cb-search-history-count">{item.resultCount}</span>
                  <button class="cb-search-history-remove" onmousedown={(e: Event) => e.preventDefault()} onclick={(e: Event) => removeSearchHistoryItem(item, e)}>
                    <i class="ti ti-x"></i>
                  </button>
                </div>
              {/each}
            </div>
          {/if}
        </div>
        <button class="btn btn-primary" onclick={() => doSearch(1)}>
          {#if searching || searchingModrinth || searchingCurseForge}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-search"></i>{/if}
          {t('search')}
        </button>
        <div class="cb-sort">
          <i class="ti ti-arrows-sort"></i>
          <SelectField
            value={sort}
            options={sortOptions.map(so => ({ value: so.id, label: so.label }))}
            onchange={(v) => { sort = v; doSearch(1) }}
            minWidth="150px"
            ariaLabel={t('browser.sort')}
          />
        </div>
        <div class="cb-view-toggle" role="radiogroup" aria-label={t('browser.view.title')}>
          {#each displayModes as m (m.id)}
            <button type="button" class="cb-view-btn" class:on={displayMode === m.id}
                    role="radio" aria-checked={displayMode === m.id}
                    title={m.label} aria-label={m.label} onclick={() => (displayMode = m.id)}>
              <i class="ti {m.icon}"></i>
            </button>
          {/each}
        </div>
        <button type="button" class="cb-filters-toggle" class:on={filtersOpen}
                aria-expanded={filtersOpen} aria-label={t('browser.filters')} title={t('browser.filters')}
                onclick={() => (filtersOpen = !filtersOpen)}>
          <i class="ti ti-filter"></i><span>{t('browser.filters')}</span>
        </button>
      </div>

      {#if activePack && activeType !== 'modpack'}
        <div class="cb-active-pack">
          {#if packAsset(activePack.iconUrl)}
            <img class="cb-pack-mini" src={packAsset(activePack.iconUrl)} alt="" />
          {:else}
            <i class="ti {activePack.icon || 'ti-puzzle'} cb-pack-mini-fallback" style="--pcol:{activePack.color || 'var(--orange)'}"></i>
          {/if}
          <span>{t('browser.filterByPack', { pack: activePack.name })}</span>
          <span class="cb-ctx-meta">{activePack.loader} {activePack.mcVersion}</span>
          <button class="btn btn-ghost btn-sm" onclick={() => { resetFilters(); doSearch() }}>
            <i class="ti ti-x"></i> {t('browser.removePackFilter')}
          </button>
        </div>
      {/if}

      <!-- Результати: ОДИН спільний грід для Modrinth+CurseForge (кожна
           картка несе власний бейдж джерела — крапка + MR/CF), як просив
           ужиток: «контентфорж і модрінг мають бути в одному списку з
           іконками і написами звідки воно». -->
      {#if searching}
        <div class="cb-grid cb-grid-skeleton">
          {#each Array(8) as _, i}
            <div class="cb-card cb-card-skel" style="animation-delay:{i * 40}ms">
              <div class="cb-skel cb-skel-ic"></div>
              <div class="cb-card-body">
                <div class="cb-skel cb-skel-line" style="width:60%"></div>
                <div class="cb-skel cb-skel-line" style="width:35%;margin-top:8px"></div>
                <div class="cb-skel cb-skel-line" style="width:90%;margin-top:10px"></div>
                <div class="cb-skel cb-skel-line" style="width:75%;margin-top:5px"></div>
              </div>
            </div>
          {/each}
        </div>
      {:else if mergedResults.length === 0 && !results.cfDisabled}
        {#if results.modrinthError}
          <!-- Офлайн / мережева помилка Modrinth: показуємо причину людською
               мовою замість порожнього «Нічого не знайдено» (немає сенсу
               казати «нічого немає», коли просто немає інтернету). -->
          <div class="cb-empty cb-empty-error">
            <i class="ti ti-wifi-off"></i>
            <div>{results.modrinthError}</div>
            <button class="btn btn-ghost btn-sm" onclick={() => doSearch(currentPage)}>
              <i class="ti ti-refresh"></i> {t('browser.retry')}
            </button>
          </div>
        {:else}
          <div class="cb-empty">
            <i class="ti ti-mood-sad"></i>
            <div>{t('browser.noResults')}</div>
          </div>
        {/if}
      {:else}
        {#if searchingCurseForge}
          <!-- Modrinth уже відповів (інакше ми б лишались у гілці searching
               вище) — CurseForge ще шукає окремо. Невеликий інлайн-індикатор
               замість блокування вже готового Modrinth-гріда: показуємо, що
               CurseForge «дотягується», а не мовчимо і не морозимо екран. -->
          <div class="cb-cf-note cb-cf-loading"><i class="ti ti-loader spin"></i> {t('browser.cfSearching')}</div>
        {:else if results.cfError}
          <!-- Ключ є, але CurseForge відмовив (403/401/rate limit) — показуємо
               причину, а не порожню секцію без пояснень. -->
          <div class="cb-cf-note cb-cf-error"><i class="ti ti-alert-octagon"></i> {t('browser.cfError', { message: results.cfError })}</div>
        {:else if results.cfDisabled}
          <div class="cb-cf-note"><i class="ti ti-alert-triangle"></i> {t('browser.cfDisabled')}</div>
        {/if}

        {#if mergedResults.length > 0}
          <div class="cb-section">
            <div class="cb-section-head">
              <span class="cb-count">{t('browser.resultsCount', { count: mergedResults.length, total: results.totalHits || mergedResults.length })}</span>
              {@render pager()}
            </div>
            {#if results.smartSearchTerms?.length}
              <!-- Розумний пошук («меблі» → furniture/decoration): гравець
                   бачить, ЧОМУ видача не збігається дослівно з тим, що він
                   ввів, замість мовчазної підміни запиту. -->
              <div class="cb-smart-hint">
                <i class="ti ti-bulb"></i>
                {t('browser.smartSearch.hint', { terms: results.smartSearchTerms.join(', ') })}
              </div>
            {/if}
            <div class="cb-grid" class:cb-refreshing={searchingModrinth || searchingCurseForge}>
              {#each mergedResults as item (item.source + '|' + item.id)}
                {@render cbCard(item)}
              {/each}
            </div>
          </div>
        {/if}

        <!-- Сторінки (замість «Показати ще») — та сама панель зверху і знизу списку. -->
        {@render pager()}

        <!-- Атрибуція CurseForge (вимога CurseForge ToS: показувати назву/лого
             і посилання на curseforge.com, коли показуємо їхній контент).
             Показується лише коли в результатах є хоч один CurseForge-проєкт. -->
        {#if mergedResults.some((r) => r.source === 'curseforge')}
          <div class="cb-cf-attr">
            <img class="cb-cf-logo" src={curseforgeIcon} alt="" width="18" height="18" loading="lazy" />
            <span>{t('browser.attribution.curseforge')}</span>
            <a href="https://www.curseforge.com/minecraft" target="_blank" rel="noopener noreferrer">curseforge.com</a>
          </div>
        {/if}
      {/if}
    </div>

    <!-- Правий сайдбар фільтрів: одна панель із секціями-розділювачами,
         вибір — чіпами (як у Modrinth app). Секції складаються за заголовком. -->
    {#if filtersOpen}
      <button type="button" class="cb-sidebar-backdrop" aria-label={t('close')} onclick={() => (filtersOpen = false)}></button>
    {/if}
    <aside class="cb-sidebar">
      <div class="cbs-panel">
        <div class="cbs-title"><i class="ti ti-filter"></i> {t('browser.filters')}</div>

        <!-- Джерело: сегментований перемикач -->
        <section class="cbs-sec" class:collapsed={isCollapsed('source')}>
          <button type="button" class="cbs-head" onclick={() => toggleFilterGroup('source')} aria-expanded={!isCollapsed('source')}>
            <span>{t('browser.source.title')}</span>
            <i class="ti ti-chevron-down cbs-caret"></i>
          </button>
          <div class="cbs-fold"><div class="cbs-body">
            <div class="cbs-seg" role="radiogroup" aria-label={t('browser.source.title')}>
              <button type="button" role="radio" aria-checked={sourceFilter === 'all'} class:on={sourceFilter === 'all'} onclick={() => pickSource('all')}>{t('browser.source.all')}</button>
              <button type="button" role="radio" aria-checked={sourceFilter === 'modrinth'} class:on={sourceFilter === 'modrinth'} onclick={() => pickSource('modrinth')}><img src={modrinthIcon} alt="" />Modrinth</button>
              <button type="button" role="radio" aria-checked={sourceFilter === 'curseforge'} class:on={sourceFilter === 'curseforge'} onclick={() => pickSource('curseforge')}><img src={curseforgeIcon} alt="" />CurseForge</button>
            </div>
          </div></div>
        </section>

      {#if activeType !== 'modpack'}
      <section class="cbs-sec" class:collapsed={isCollapsed('pack')}>
        <button type="button" class="cbs-head" onclick={() => toggleFilterGroup('pack')} aria-expanded={!isCollapsed('pack')}>
          <span>{t('browser.installTarget')}</span>
          <i class="ti ti-chevron-down cbs-caret"></i>
        </button>
        <div class="cbs-fold"><div class="cbs-body cbs-body-list">
          <!-- Перемикач цілі встановлення «Збірки ↔ Сервери» — той самий
               принцип, що й кастомні/шаурма у sidebar збірок: браузер
               однаково вміє ставити і в збірку, і НА СЕРВЕР. -->
          {#if hasTargetServers}
          <div class="cb-target-tabs" role="tablist">
            {#if activeType !== 'plugin'}
            <button
              type="button"
              role="tab"
              aria-selected={pickMode === 'packs'}
              class="cb-target-tab"
              class:on={pickMode === 'packs'}
              onclick={() => { pickMode = 'packs'; onClearServerContext?.() }}
            >
              <i class="ti ti-box"></i> {t('browser.target.packs')}
            </button>
            {/if}
            <button
              type="button"
              role="tab"
              aria-selected={pickMode === 'servers'}
              class="cb-target-tab"
              class:on={pickMode === 'servers'}
              onclick={() => {
                pickMode = 'servers'
                // Сервер тримає лише моди/плагіни.
                if (activeType !== 'mod' && activeType !== 'plugin') activeType = 'mod'
              }}
            >
              <i class="ti ti-server"></i> {t('browser.target.servers')}
            </button>
          </div>
          {/if}

          {#if pickMode === 'servers'}
            <!-- Сервери: лише ті, що підтримують поточний вид контенту (моди →
                 мод-ядра, плагіни → плагін-ядра), з реальними іконками/
                 кольорами. -->
            <ServerTargetList
              servers={servers}
              tab={activeType}
              selectedId={serverContext?.id ?? ''}
              onpick={(srv) => onPickServer?.(srv.id, kindForTab(activeType) ?? 'mods')}
              variant="radio"
              name="cb-target"
            />
          {:else}
          <label class="cb-opt">
            <input type="radio" name="cb-pack" checked={packFilterId === ''} onchange={() => { resetFilters(); doSearch() }} />
            <span>{t('browser.noPackFilter')}</span>
          </label>
          {#if filteredPackPicker && filteredPackPicker.items.length > 0}
            <PackPicker
              items={filteredPackPicker.items}
              rowById={filteredPackPicker.rowById}
              folderById={filteredPackPicker.folderById}
              selected={packFilterId}
              onselect={(id) => setPackFilter(id)}
              class="cb-pp"
            />
          {:else}
            {#each packs.filter((p: { id: string; name: string; mcVersion: string; loader: string; iconUrl?: string; icon?: string; color?: string }) => activeType !== 'mod' || !['', 'vanilla', 'datapack'].includes((p.loader ?? '').toLowerCase())) as p (p.id)}
              <label class="cb-opt cb-opt-pack" style="--pcol:{p.color || 'var(--orange)'}">
                <input type="radio" name="cb-pack" checked={packFilterId === p.id} onchange={() => setPackFilter(p.id)} />
                {#if packAsset(p.iconUrl)}
                  <img class="cb-pack-mini" src={packAsset(p.iconUrl)} alt="" />
                {:else}
                  <i class="ti {p.icon || 'ti-puzzle'} cb-pack-mini-fallback" style="--pcol:{p.color || 'var(--orange)'}"></i>
                {/if}
                <span class="cb-pack-opt-name">{p.name}</span>
                <span class="cb-ctx-meta">{p.loader} {p.mcVersion}</span>
              </label>
            {/each}
          {/if}
          {/if}
        </div></div>
      </section>
      {/if}

        <!-- Версія гри: випадаючий список (доступний, коли версія не задана збіркою) -->
        {#if !activePack || activeType === 'modpack'}
          <section class="cbs-sec" class:collapsed={isCollapsed('version')}>
            <button type="button" class="cbs-head" onclick={() => toggleFilterGroup('version')} aria-expanded={!isCollapsed('version')}>
              <span>{t('browser.gameVersion')}</span>
              <i class="ti ti-chevron-down cbs-caret"></i>
            </button>
            <div class="cbs-fold"><div class="cbs-body">
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <div class="cbs-select" onpointerdown={loadMCVersions} onfocusin={loadMCVersions}>
                <SelectField
                  value={selectedGameVersion}
                  options={gameVersionOptions}
                  onchange={pickGameVersion}
                  minWidth="100%"
                  ariaLabel={t('browser.gameVersion')}
                />
              </div>
              {#if mcVersionEntries.some(e => e.versionType !== 'release')}
                <button type="button" class="cbs-link" onclick={() => (showAllVersions = !showAllVersions)}>
                  <i class="ti {showAllVersions ? 'ti-chevron-up' : 'ti-chevron-down'}"></i>
                  {showAllVersions ? t('browser.versionsOnlyReleases') : t('browser.versionsShowAll')}
                </button>
              {/if}
            </div></div>
          </section>
        {/if}

        <!-- Лоадери: чіпи з іконками (клік по вибраному знімає вибір).
             Ховаємо, коли лоадер уже заданий збіркою. -->
        {#if loaders.length > 1 && !activePack && !packContext}
          <section class="cbs-sec" class:collapsed={isCollapsed('loader')}>
            <button type="button" class="cbs-head" onclick={() => toggleFilterGroup('loader')} aria-expanded={!isCollapsed('loader')}>
              <span>{t('browser.loader.title')}</span>
              <i class="ti ti-chevron-down cbs-caret"></i>
            </button>
            <div class="cbs-fold"><div class="cbs-body">
              <div class="cbs-chips">
                {#each loaders.filter(l => l.id !== '') as l (l.id)}
                  <button type="button" class="cbs-chip" class:on={selectedLoader === l.id} onclick={() => pickLoader(l.id)}>
                    {#if l.icon}<img class="cbs-chip-ic" src={l.icon} alt="" />{:else}<i class="ti ti-apps"></i>{/if}
                    {l.label}
                  </button>
                {/each}
              </div>
            </div></div>
          </section>
        {/if}

        <!-- Середовище + ліцензія (Modrinth-фасети; для «лише CurseForge» не мають сенсу) -->
        {#if sourceFilter !== 'curseforge'}
          <section class="cbs-sec" class:collapsed={isCollapsed('environment')}>
            <button type="button" class="cbs-head" onclick={() => toggleFilterGroup('environment')} aria-expanded={!isCollapsed('environment')}>
              <span>{t('browser.environment.title')}</span>
              <i class="ti ti-chevron-down cbs-caret"></i>
            </button>
            <div class="cbs-fold"><div class="cbs-body">
              {#if serverTargetMods}
                <div class="cb-env-locked">
                  <i class="ti ti-server"></i>
                  <span>{t('browser.environment.serverTargetLocked')}</span>
                </div>
              {:else}
                <div class="cbs-chips">
                  <button type="button" class="cbs-chip" class:on={envClient} aria-pressed={envClient} onclick={() => toggleEnv('client')}>
                    <i class="ti ti-device-desktop"></i>{t('browser.environment.client')}
                  </button>
                  <button type="button" class="cbs-chip" class:on={envServer} aria-pressed={envServer} onclick={() => toggleEnv('server')}>
                    <i class="ti ti-server"></i>{t('browser.environment.server')}
                  </button>
                  <button type="button" class="cbs-chip" class:on={openSourceOnly} aria-pressed={openSourceOnly} onclick={toggleOpenSource}>
                    <i class="ti ti-code"></i>{t('browser.license.openSource')}
                  </button>
                </div>
              {/if}
            </div></div>
          </section>
        {/if}

        <!-- Категорії: чіпи, згруповані по Header (як у Modrinth) -->
        {#each groupedCategories as grp (grp.header)}
          <section class="cbs-sec" class:collapsed={isCollapsed('cat:' + grp.header)}>
            <button type="button" class="cbs-head" onclick={() => toggleFilterGroup('cat:' + grp.header)} aria-expanded={!isCollapsed('cat:' + grp.header)}>
              <span>{categoryHeaderLabel(grp.header)}</span>
              <i class="ti ti-chevron-down cbs-caret"></i>
            </button>
            <div class="cbs-fold"><div class="cbs-body">
              <div class="cbs-chips">
                {#each grp.items as cat (cat.source + cat.name)}
                  <button type="button" class="cbs-chip" class:on={isCategorySelected(cat)} aria-pressed={isCategorySelected(cat)}
                          onclick={() => { toggleCategory(cat); doSearch(1) }}>
                    {cat.name}
                    {#if cat.source === 'curseforge'}<span class="cbs-src" title="CurseForge">CF</span>{/if}
                  </button>
                {/each}
              </div>
            </div></div>
          </section>
        {/each}

        <div class="cbs-foot">
          <button class="cbs-reset" onclick={() => { resetFilters(); doSearch() }}><i class="ti ti-refresh"></i> {t('browser.reset')}</button>
        </div>
      </div>
    </aside>
  </div>
  {/if}
</div>

<!-- Панель сторінок — рендериться зверху (у заголовку секції результатів)
     і знизу списку через той самий снипет, замість «Показати ще». -->
{#snippet pager()}
  {#if totalPages > 1}
    <div class="cb-pager">
      <button class="btn btn-ghost btn-sm" onclick={() => goToPage(currentPage - 1)} disabled={currentPage <= 1 || searching}>
        <i class="ti ti-chevron-left"></i> {t('browser.page.prev')}
      </button>
      <span class="cb-pager-count">{t('browser.page', { page: currentPage, total: totalPages })}</span>
      <button class="btn btn-ghost btn-sm" onclick={() => goToPage(currentPage + 1)} disabled={currentPage >= totalPages || searching}>
        {t('browser.page.next')} <i class="ti ti-chevron-right"></i>
      </button>
    </div>
  {/if}
{/snippet}

<!-- Модалка вибору збірки для швидкого встановлення з картки (коли немає
     активного фільтра збірки). Клік по кнопці на картці відкриває саме
     це, а НЕ сторінку проєкту. -->
{#if packPickDialog}
  {@const dlg = packPickDialog}
  <UiModal
    open={true}
    title={dlg.entry.name}
    subtitle={t('browser.install.pickPackTitle')}
    icon="ti-package"
    color="var(--tb-c, var(--orange))"
    width="520px"
    onclose={() => (packPickDialog = null)}
  >
      {#if packPickServers.length > 0 || packPickType === 'plugin'}
        <!-- Ціль встановлення: і збірки, і сервери (лише ті, що підтримують
             цей вид контенту). Для плагінів вкладка «Збірки» не має сенсу —
             збірка плагінів не тримає — тож її ховаємо. -->
        <div class="cb-target-tabs cb-dlg-target-tabs" role="tablist">
          {#if packPickType !== 'plugin'}
            <button type="button" role="tab" aria-selected={packPickTab === 'packs'} class="cb-target-tab" class:on={packPickTab === 'packs'} onclick={() => (packPickTab = 'packs')}>
              <i class="ti ti-box"></i> {t('browser.target.packs')}
            </button>
          {/if}
          <button type="button" role="tab" aria-selected={packPickTab === 'servers'} class="cb-target-tab" class:on={packPickTab === 'servers'} onclick={() => (packPickTab = 'servers')}>
            <i class="ti ti-server"></i> {t('browser.target.servers')}
          </button>
        </div>
      {/if}
      {#if packPickTab === 'servers' && (packPickServers.length > 0 || packPickType === 'plugin')}
        <ServerTargetList
          servers={servers}
          tab={packPickType}
          selectedId=""
          onpick={installToPickedServer}
          variant="row"
          name="cb-dlg-target"
        />
      {:else}
      {#if !packPickDialog.loading && packPickDialog.packs.length > 0}
        <div class="cb-modal-search">
          <i class="ti ti-search"></i>
          <input placeholder={t('browser.searchPack')} bind:value={packPickFilter} />
        </div>
      {/if}
        {#if packPickDialog.loading}
          <!-- «Аналізуємо…» ПОКИ ЙДЕ ЗАПИТ (раніше умова залежала від
               versions.length===0 — якщо версії завантажувались швидше за
               список збірок, користувач спершу бачив «немає сумісних
               збірок» і лише потім живий список). -->
          <div class="cb-loading sm"><i class="ti ti-loader spin"></i> {t('browser.compatChecking')}</div>
        {:else}
          <!-- Вибір версії прибрано з UI (запит «чого тут є вибір версії
               а не авто, прибери це»): раніше тут був SelectField з
               «Авто»/конкретними версіями, але користувачу для
               встановлення в наявну збірку не потрібно самому підбирати
               версію контенту — лаунчер завжди бере ту, що підходить під
               (mcVersion, loader) обраної збірки. selectedVersionId
               лишається '' (авто) назавжди — саме цей режим і показує
               ВСІ сумісні збірки одразу, без додаткового кроку. -->
          {#if packPickWhitelistedPacks.length > 0}
            <!-- Шаурма-збірки, де мод дозволений білим списком — ОКРЕМА
                 секція з іншим акцентним кольором (violet, не orange
                 кастомних), щоб не сплутати з «звичайним» встановленням:
                 це виняток понад маніфест, а не типовий кастомний шлях. -->
            <div class="cb-pp-shaurma-head">
              <i class="ti ti-shield-check"></i>
              <span>{t('browser.install.shaurmaAllowed')}</span>
            </div>
            <div class="cb-pack-list cb-pack-list-shaurma">
              {#each packPickWhitelistedPacks as p}
                {@const picon = packAsset(p.iconUrl)}
                <div class="cb-pack-row cb-pack-row-shaurma" class:installed={p.alreadyInstalled} style="--pcol:{p.color || 'var(--violet)'}">
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
                  <button class="btn btn-primary btn-sm" onclick={() => installToPickedPack(p.packId)} disabled={packPickDialog.loading}>
                    {#if p.alreadyInstalled}
                      <i class="ti ti-refresh"></i> {t('browser.install.reinstall')}
                    {:else}
                      <i class="ti ti-download"></i> {t('browser.install')}
                    {/if}
                  </button>
                </div>
              {/each}
            </div>
            {#if filteredPackPickPacks.length > 0 || (filteredPackPicker && filteredPackPicker.items.length > 0)}
              <div class="cb-pp-shaurma-sep"></div>
            {/if}
          {/if}
          {#if packPickDialog.packs.length === 0}
            <div class="cb-empty sm"><i class="ti ti-mood-sad"></i><div>{t('browser.noCompatiblePacks')}</div></div>
          {:else if !filteredPackPicker || filteredPackPicker.items.length === 0}
            <!-- Фолбек: якщо мапа папок недоступна — плаский список, як раніше. -->
            {#if filteredPackPickPacks.length === 0}
              {#if packPickWhitelistedPacks.length === 0}
                <div class="cb-empty sm"><i class="ti ti-search-off"></i><div>{t('browser.noPackMatch')}</div></div>
              {/if}
            {:else}
              <div class="cb-pack-list">
                {#each filteredPackPickPacks as p}
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
                    <button class="btn btn-primary btn-sm" onclick={() => installToPickedPack(p.packId)} disabled={packPickDialog.loading}>
                      {#if p.alreadyInstalled}
                        <i class="ti ti-refresh"></i> {t('browser.install.reinstall')}
                      {:else}
                        <i class="ti ti-download"></i> {t('browser.install')}
                      {/if}
                    </button>
                  </div>
                {/each}
              </div>
            {/if}
          {:else}
            {@const allowedIds = new Set(filteredPackPickPacks.map(p => p.packId))}
            {@const infoById = new Map(filteredPackPickPacks.map(p => [p.packId, p]))}
            <PackPicker
              items={filteredPackPicker.items}
              rowById={filteredPackPicker.rowById}
              folderById={filteredPackPicker.folderById}
              allowedIds={allowedIds}
              query={packPickFilter}
              selectable={false}
              emptyText={t('browser.noPackMatch')}
              class="cb-pp cb-pp-install"
            >
              {#snippet rowActions(packId)}
                {@const p = infoById.get(packId)}
                {#if p}
                  {#if p.alreadyInstalled}
                    <span class="cb-pp-installed-badge" title={t('browser.alreadyInstalled', { file: p.installedFileName ?? p.name })}>
                      <i class="ti ti-circle-check"></i>
                    </span>
                  {/if}
                  <button class="btn btn-primary btn-sm" onclick={() => installToPickedPack(packId)} disabled={dlg.loading}>
                    {#if p.alreadyInstalled}
                      <i class="ti ti-refresh"></i> {t('browser.install.reinstall')}
                    {:else}
                      <i class="ti ti-download"></i> {t('browser.install')}
                    {/if}
                  </button>
                {/if}
              {/snippet}
            </PackPicker>
          {/if}
        {/if}
      {/if}
  </UiModal>
{/if}

<!-- Модалка встановлення модпака (клік по кнопці на картці модпака):
     вибір версії + перевірка «уже в Моїх збірках» → оновити наявну або
     створити копію (як DownloadProject modal у Modrinth app). -->
{#if modpackDialog}
  {@const dlg = modpackDialog}
  <UiModal
    open={true}
    title={dlg.entry.name}
    subtitle={t('browser.modpackDialog.title')}
    icon="ti-package"
    color="var(--tb-c, var(--orange))"
    width="480px"
    onclose={() => (modpackDialog = null)}
  >
    {#if modpackDialog.loading}
      <div class="cb-loading sm"><i class="ti ti-loader spin"></i> {t('browser.compatChecking')}</div>
    {:else if modpackDialog.versions.length === 0}
      <div class="cb-empty sm"><i class="ti ti-mood-sad"></i><div>{t('browser.update.noVersions')}</div></div>
    {:else}
      <label class="cb-dlg-label">{t('browser.project.tab.versions')}</label>
      <SelectField
        value={modpackDialog.selectedVersionId}
        options={modpackDialog.versions.map(v => ({ value: v.id, label: modpackVersionLabel(v) }))}
        onchange={(v) => { if (modpackDialog) modpackDialog.selectedVersionId = v }}
        minWidth="100%"
        ariaLabel={t('browser.project.tab.versions')}
      />
      {#if modpackDialog.check?.alreadyInstalled}
        <div class="cb-hint cb-dlg-hint">
          <i class="ti ti-alert-triangle"></i>
          <span>{t('browser.modpackDialog.alreadyInstalled', { name: modpackDialog.check.existingPackName ?? '' })}</span>
        </div>
      {/if}
    {/if}
    {#snippet footer()}
      {#if !dlg.loading && dlg.versions.length > 0}
        {#if dlg.check?.alreadyInstalled}
          <button class="btn btn-primary" onclick={() => installModpack(dlg.entry, true)} disabled={dlg.loading}>
            <i class="ti ti-refresh"></i> {t('browser.modpackDialog.update')}
          </button>
          <button class="btn btn-secondary" onclick={() => installModpack(dlg.entry, false)} disabled={dlg.loading}>
            <i class="ti ti-plus"></i> {t('browser.modpackDialog.copy')}
          </button>
          <button class="btn btn-ghost" onclick={() => modpackDialog = null}>
            <i class="ti ti-x"></i> {t('browser.alreadyInstalled.cancel')}
          </button>
        {:else}
          <button class="btn btn-ghost" onclick={() => modpackDialog = null}>{t('browser.alreadyInstalled.cancel')}</button>
          <button class="btn btn-primary" onclick={() => installModpack(dlg.entry, false)} disabled={!dlg.selectedVersionId || dlg.loading}>
            <i class="ti ti-download"></i> {t('browser.install')}
          </button>
        {/if}
      {/if}
    {/snippet}
  </UiModal>
{/if}

<!-- Модалка вибору світів для датапака (світи саме тієї збірки, куди
     встановлюємо — як instance context у Modrinth app). Показується коли:
     - фільтр збірки активний і тип контенту = datapack (світи збірки-фільтра),
     - або після вибору збірки в packPickDialog для датапака. -->
{#if worldDialog}
  {@const dlg = worldDialog}
  <UiModal
    open={true}
    title={dlg.packName}
    subtitle={t('browser.world.chooseFor', { pack: dlg.entry.name })}
    icon="ti-world"
    color="var(--tb-c, var(--orange))"
    width="480px"
    onclose={() => (worldDialog = null)}
  >
    {#if worldDialog.loading}
      <div class="cb-loading sm"><i class="ti ti-loader spin"></i> {t('browser.compatChecking')}</div>
    {:else if worldDialog.worlds.length === 0}
      <div class="cb-empty sm"><i class="ti ti-mood-sad"></i><div>{t('browser.world.noWorlds')}</div></div>
      <div class="cb-hint" style="margin-top:8px"><i class="ti ti-info-circle"></i><span>{t('browser.world.noWorldsHint')}</span></div>
    {:else}
      <div class="cb-world-tools">
        <button class="btn btn-ghost btn-sm" onclick={() => selectAllWorlds(true)}><i class="ti ti-checkbox"></i> {t('browser.world.selectAll')}</button>
        <button class="btn btn-ghost btn-sm" onclick={() => selectAllWorlds(false)}><i class="ti ti-square"></i> {t('browser.world.selectNone')}</button>
      </div>
      <div class="cb-world-list">
        {#each worldDialog.worlds as w (w)}
          <label class="cb-world-row">
            <input type="checkbox" checked={worldDialog.selectedWorlds.includes(w)} onchange={() => toggleWorld(w)} />
            <i class="ti ti-world"></i>
            <span>{w}</span>
          </label>
        {/each}
      </div>
    {/if}
    <!-- Встановити у ЗАГАЛЬНІ datapacks збірки — доступно і без світів. -->
    <label class="cb-world-global">
      <input type="checkbox" checked={worldGlobal} onchange={() => (worldGlobal = !worldGlobal)} />
      <i class="ti ti-folder-data"></i>
      <span>{t('browser.world.installGlobal')}</span>
    </label>
    {#snippet footer()}
      <div style="display:flex; align-items:center; justify-content:space-between; gap:10px; width:100%">
      <span class="cb-world-count">{t('browser.world.selected', { n: dlg.selectedWorlds.length, total: dlg.worlds.length })}</span>
      <button class="btn btn-primary" onclick={installDatapackToWorlds} disabled={(!dlg.selectedWorlds.length && !worldGlobal) || worldInstalling}>
        <i class="ti ti-download"></i>
        {worldGlobal
          ? (dlg.selectedWorlds.length ? t('browser.world.installGlobalAndWorlds') : t('browser.world.installGlobalBtn'))
          : dlg.selectedWorlds.length === 0
            ? t('browser.world.installBtn')
            : dlg.selectedWorlds.length < 5
              ? t('browser.world.installBtnPlural2', { n: dlg.selectedWorlds.length })
              : t('browser.world.installBtnPlural5', { n: dlg.selectedWorlds.length })}
      </button>
      </div>
    {/snippet}
  </UiModal>
{/if}

<!-- Створення/перейменування колекції — просте текстове поле, той самий
     принцип, що діалог перейменування збірки. -->
{#if collectionEditDialog}
  <UiModal
    open={true}
    title={collectionEditDialog.id ? t('browser.saved.renameCollection') : t('browser.saved.newCollection')}
    icon={collectionEditDialog.icon}
    color={collectionEditDialog.color}
    width="480px"
    onclose={() => (collectionEditDialog = null)}
  >
    <label class="cb-col-label">{t('browser.saved.collectionNamePlaceholder')}</label>
    <input
      class="cb-collection-name-input"
      type="text"
      value={collectionEditDialog.name}
      placeholder={t('browser.saved.collectionNamePlaceholder')}
      oninput={(e: Event) => { if (collectionEditDialog) collectionEditDialog.name = (e.target as HTMLInputElement).value }}
      onkeydown={(e: KeyboardEvent) => { if (e.key === 'Enter') submitCollectionEdit() }}
    />

    <!-- Іконка + колір колекції (як у збірок): без цього колекції були
         всі однаковими сірими папками, на відміну від збірок, де колір і
         іконку можна обрати. -->
    <label class="cb-col-label">{t('browser.saved.color')}</label>
    <div class="cb-col-colors">
      {#each cbColorPresets as col (col)}
        <button
          type="button"
          class="cb-col-dot"
          class:sel={collectionEditDialog.color === col}
          style="background:{col}"
          aria-label={col}
          onclick={() => { if (collectionEditDialog) collectionEditDialog.color = col }}
        ></button>
      {/each}
      <label class="cb-col-custom" title={t('browser.saved.customColor')} style="--pcol:{collectionEditDialog.color}">
        <i class="ti ti-color-picker"></i>
        <input type="color" value={collectionEditDialog.color} oninput={(e: Event) => { if (collectionEditDialog) collectionEditDialog.color = (e.target as HTMLInputElement).value }} />
      </label>
    </div>

    <label class="cb-col-label">{t('browser.saved.icon')}</label>
    <div class="cb-col-picker">
      <PresetIconPicker
        value={collectionEditDialog.icon}
        color={collectionEditDialog.color}
        onSelect={(name: string) => { if (collectionEditDialog) collectionEditDialog.icon = name }}
      />
    </div>

    {#snippet footer()}
      <button class="btn btn-ghost" onclick={() => (collectionEditDialog = null)}>{t('browser.installConfirm.cancel')}</button>
      <button class="btn btn-primary" onclick={submitCollectionEdit} disabled={!collectionEditDialog?.name.trim()}>
        <i class="ti ti-check"></i> {t('save')}
      </button>
    {/snippet}
  </UiModal>
{/if}

<!-- Спливне меню «Додати в колекцію»: список наявних колекцій під
     карткою (клік по назві додає туди), + «Нова колекція» знизу. Раніше
     цього меню не існувало взагалі — AddToCollection на бекенді був
     готовий, а спосіб його викликати з UI був відсутній. -->
{#if addToCollectionMenu}
  <div class="ctx-menu" style="left:{addToCollectionMenu.x}px;top:{addToCollectionMenu.y}px" onclick={(e) => e.stopPropagation()}>
    {#if loadingCollections}
      <div class="ctx-hint"><i class="ti ti-loader spin"></i> {t('loading')}</div>
    {:else if collectionsList.length === 0}
      <div class="ctx-hint">{t('browser.saved.emptyCollections')}</div>
    {:else}
      {#each collectionsList as c (c.id)}
        <button class="ctx-item" class:ctx-item-in={entryCollectionIds.has(c.id)} onclick={() => addEntryToCollection(c.id)}>
          <i class="ti {entryCollectionIds.has(c.id) ? 'ti-folder-check' : 'ti-folder'}"></i> <span style="flex:1">{c.name}</span>
          {#if entryCollectionIds.has(c.id)}
            <span class="cb-add-col-in">{t('browser.saved.alreadyIn')}</span>
          {:else}
            <span class="cb-add-col-count">{c.itemCount}</span>
          {/if}
        </button>
      {/each}
      <div class="ctx-sep"></div>
    {/if}
    <button class="ctx-item" onclick={createCollectionAndAdd}>
      <i class="ti ti-folder-plus"></i> <span>{t('browser.saved.newCollection')}</span>
    </button>
  </div>
{/if}

<!-- Групове встановлення колекції: 1) обрати збірку, 2) переглянути
     план (install/update/already/incompatible на кожен запис) і
     скасувати або продовжити, 3) результат. -->
{#if collectionInstallDialog}
  {@const dlg = collectionInstallDialog}
  <UiModal
    open={true}
    title={t('browser.saved.installCollection')}
    icon="ti-download"
    width="520px"
    onclose={() => (collectionInstallDialog = null)}
  >	    {#if dlg.step === 'pick-pack'}
	      {#if servers.length > 0}
	        <!-- Ціль колекції: збірка чи сервер (сервер — лише моди/плагіни;
	             решту записів колекції план позначить несумісними). -->
	        <div class="cb-target-tabs cb-dlg-target-tabs" role="tablist">
	          <button type="button" role="tab" aria-selected={dlg.target === 'pack'} class="cb-target-tab" class:on={dlg.target === 'pack'}
	            onclick={() => { if (collectionInstallDialog) collectionInstallDialog = { ...collectionInstallDialog, target: 'pack', packId: packFilterId || (packs[0]?.id ?? '') } }}>
	            <i class="ti ti-box"></i> {t('browser.target.packs')}
	          </button>
	          <button type="button" role="tab" aria-selected={dlg.target === 'server'} class="cb-target-tab" class:on={dlg.target === 'server'}
	            onclick={() => { if (collectionInstallDialog) collectionInstallDialog = { ...collectionInstallDialog, target: 'server', packId: '' } }}>
	            <i class="ti ti-server"></i> {t('browser.target.servers')}
	          </button>
	        </div>
	      {/if}
	      {#if dlg.target === 'server'}
	        <label class="cb-dlg-label">{t('browser.target.pickServerForCollection')}</label>
	        <!-- Колекція може містити і моди, і плагіни — показуємо всі сервери,
	             що тримають хоч щось (мод-ядра та плагін-ядра); vanilla — ні. -->
	        <ServerTargetList
	          servers={collectionServers}
	          tab="mod"
	          allKinds={true}
	          selectedId={dlg.packId}
	          onpick={(srv) => { if (collectionInstallDialog) collectionInstallDialog = { ...collectionInstallDialog, packId: srv.id } }}
	          variant="row"
	          name="cb-coll-target"
	        />
	      {:else}

	      <label class="cb-dlg-label">{t('browser.saved.pickPackForCollection')}</label>
	      <!-- Вибір збірки — тим самим PackPicker, що й решта лаунчера:
	           з папками «Моїх збірок», іконками та loader·mc підписом, а не
	           пласким SelectField (скарга: «вибір збірки не використовує
	           packpicker», дублікати-назви з різними лоадерами не розрізнити
	           у спадному списку). Сумісність перевіряється на кроці «план»
	           бекендом, тож тут дозволені ВСІ кастомні збірки.
	           Фолбек на SelectField — лише коли даних папок немає зовсім. -->
	      {#if packPicker && packPicker.items.length > 0}
	        <div class="cb-collection-picker">
	          <PackPicker
	            items={packPicker.items}
	            rowById={packPicker.rowById}
	            folderById={packPicker.folderById}
	            selected={dlg.packId}
	            onselect={(id: string) => { if (collectionInstallDialog) collectionInstallDialog.packId = id }}
	            query={''}
	            emptyText={t('browser.noCompatiblePacks')}
	          />
	        </div>
	      {:else}
	        <SelectField
	          value={dlg.packId}
	          options={packs.map((p: { id: string; name: string; mcVersion: string; loader: string; iconUrl?: string; icon?: string; color?: string }) => ({ value: p.id, label: `${p.name} (${p.mcVersion} ${p.loader})` }))}
	          onchange={(v) => { if (collectionInstallDialog) collectionInstallDialog.packId = v }}
	          minWidth="100%"
	          ariaLabel={t('browser.saved.pickPackForCollection')}
	        />
	      {/if}
	      {/if}
    {:else if dlg.step === 'plan'}
      {#if dlg.plan.length === 0}
        <div class="cb-empty sm"><i class="ti ti-mood-sad"></i><div>{t('browser.saved.emptyCollection')}</div></div>
      {:else}
        <div class="cb-collection-plan">
          {#each dlg.plan as p (p.source + '|' + p.projectId)}
            <div class="cb-plan-row cb-plan-{p.status}">
              <i class="ti {p.status === 'incompatible' ? 'ti-alert-triangle' : p.status === 'update' ? 'ti-refresh' : p.status === 'already' ? 'ti-check' : 'ti-download'}"></i>
              <span class="cb-plan-name">{p.name}</span>
              <span class="cb-plan-status">
                {#if p.status === 'incompatible'}{t('browser.saved.planIncompatible')}
                {:else if p.status === 'update'}{t('browser.saved.planUpdate')}
                {:else if p.status === 'already'}{t('browser.saved.planAlready')}
                {:else}{t('browser.saved.planInstall')}{/if}
              </span>
            </div>
          {/each}
        </div>
        {#if dlg.plan.some(p => p.status === 'incompatible')}
          <div class="cb-hint cb-dlg-hint">
            <i class="ti ti-alert-triangle"></i>
            <span>{t('browser.saved.planIncompatibleHint')}</span>
          </div>
        {/if}
      {/if}
    {:else if dlg.step === 'installing'}
      <div class="cb-loading sm"><i class="ti ti-loader spin"></i> {t('browser.saved.installingCollection')}</div>
    {:else if dlg.step === 'done'}
      <div class="cb-collection-plan">
        {#each dlg.results as r}
          <div class="cb-plan-row {r.message && !r.installedTo ? 'cb-plan-incompatible' : 'cb-plan-already'}">
            <i class="ti {r.installedTo ? 'ti-check' : 'ti-alert-triangle'}"></i>
            <span class="cb-plan-name">{r.fileName || r.message}</span>
          </div>
        {/each}
      </div>
    {/if}
    {#snippet footer()}
      {#if dlg.step === 'pick-pack'}
        <button class="btn btn-ghost" onclick={() => (collectionInstallDialog = null)}>{t('browser.installConfirm.cancel')}</button>
        <button class="btn btn-primary" onclick={collectionInstallPickPack} disabled={!dlg.packId || dlg.loading}>
          {#if dlg.loading}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-arrow-right"></i>{/if} {t('browser.saved.viewPlan')}
        </button>
      {:else if dlg.step === 'plan'}
        <button class="btn btn-ghost" onclick={() => (collectionInstallDialog = null)}>{t('browser.installConfirm.cancel')}</button>
        <button class="btn btn-primary" onclick={collectionInstallConfirm} disabled={dlg.loading || dlg.plan.every(p => p.status === 'incompatible' || p.status === 'already')}>
          <i class="ti ti-download"></i> {t('browser.saved.installCollection')}
        </button>
      {:else if dlg.step === 'done'}
        <button class="btn btn-primary" onclick={() => (collectionInstallDialog = null)}>{t('close')}</button>
      {/if}
    {/snippet}
  </UiModal>
{/if}

<style>
  /* ═════════════════════════════════════════════════════════════════════
     БРАУЗЕР КОНТЕНТУ — узгоджено з дизайн-мовою сторінки проєкту (mp-*):
     ті самі локальні токени радіусів/тіней/easing, скляний sticky-topbar,
     спокійні переходи без aurora-фону, каскадних nth-child анімацій та
     glow/shine-ефектів на hover.
     • ліва колонка результатів + правий сайдбар фільтрів (окремий скрол)
     • акцентний колір типу контенту (--tb-c) підхоплюється в табах/чіпах
     ═════════════════════════════════════════════════════════════════════ */

  /* ── Локальні токени: ті самі імена й значення, що в ModpackPage ── */
  .cb-wrap {
    --cb-gap: 16px;
    --cb-radius-lg: 18px;
    --cb-radius-md: 12px;
    --cb-radius-sm: 8px;
    --cb-shadow-card: 0 8px 22px -14px rgba(0, 0, 0, .55);
    --cb-shadow-strong: 0 20px 50px -20px rgba(0, 0, 0, .7);
    --cb-ease: cubic-bezier(.22, 1, .36, 1);
    --cb-ease-spring: cubic-bezier(.34, 1.56, .64, 1);
    --cb-accent: var(--tb-c, var(--orange));

    display: flex; flex-direction: column; gap: 14px;
    flex: 1; min-height: 0;
    position: relative; overflow: hidden;
    animation: cb-page-in .35s var(--cb-ease) both;
  }
  @keyframes cb-page-in { from { opacity: 0; } to { opacity: 1; } }

  /* ── Смужка контексту (пошук у конкретну збірку) ── */
  .cb-context-bar {
    display: flex; align-items: center; gap: 10px;
    padding: 11px 14px 11px 15px;
    background: linear-gradient(90deg, rgba(var(--orange-rgb), .12), transparent 80%);
    border: 1px solid rgba(var(--orange-rgb), .28);
    border-left: 3px solid var(--orange);
    border-radius: var(--cb-radius-md);
    color: var(--text); font-size: 13px; font-weight: 600;
  }
  .cb-context-bar > i:first-child { color: var(--orange); font-size: 16px; }
  .cb-context-bar .cb-ctx-meta { color: var(--orange); font-size: 11px; font-family: var(--font-mono); }
  .cb-context-bar :global(.btn) { margin-left: auto; }

  /* ── Таби типів контенту: sticky панель у стилі mp-tabs ── */
  .cb-tabs {
    position: sticky; top: 0; z-index: 20;
    display: flex; align-items: center; gap: 4px; flex-wrap: nowrap;
    overflow-x: auto; overflow-y: hidden;
    padding: 4px;
    scrollbar-width: none;
    background: color-mix(in srgb, var(--bg-panel) 88%, transparent);
    backdrop-filter: blur(14px);
    -webkit-backdrop-filter: blur(14px);
    border: 1px solid var(--border);
    border-radius: 13px;
  }
  .cb-tabs::-webkit-scrollbar { display: none; }
  .cb-tab {
    --tb-c: var(--orange);
    flex: 0 0 auto;
    display: inline-flex; align-items: center; gap: 7px;
    height: 36px; padding: 0 14px;
    background: transparent;
    border: 1px solid transparent;
    border-radius: 9px;
    color: var(--text-mute); cursor: pointer;
    font: 650 12.5px/1 var(--font-ui);
    white-space: nowrap;
    transition: color .15s ease, background-color .15s ease, border-color .15s ease;
    position: relative; z-index: 1;
  }
  .cb-tab i { font-size: 15px; transition: color .15s ease; }
  .cb-tab:hover { color: var(--text); background: var(--hover); }
  .cb-tab:active { transform: scale(.97); }
  .cb-tab.active {
    color: var(--text);
    background: var(--bg-card);
    border-color: color-mix(in srgb, var(--tb-c) 40%, transparent);
    box-shadow: 0 4px 12px -6px rgba(0, 0, 0, .6);
  }
  .cb-tab.active i { color: var(--tb-c); }
  .cb-tab-saved {
    --tb-c: #f5c542;
    color: color-mix(in srgb, var(--tb-c) 75%, var(--text));
  }
  .cb-tab-saved.active { background: rgba(245, 197, 66, .14); border-color: rgba(245, 197, 66, .4); }
  .cb-tab-saved i { color: var(--tb-c); }
  /* Слайдер: пілюля активного розділу їде між табами (замість миттєвої зміни фону). */
  .cb-tab-ind {
    position: absolute; top: 4px; left: 0; height: 36px; z-index: 0; pointer-events: none;
    border-radius: 9px; box-sizing: border-box;
    background: var(--bg-card);
    border: 1px solid color-mix(in srgb, var(--ind-c, var(--orange)) 40%, transparent);
    box-shadow: 0 4px 12px -6px rgba(0, 0, 0, .6);
  }
  .cb-tab-ind.anim {
    transition: transform .34s var(--cb-ease), width .34s var(--cb-ease), border-color .2s ease, background-color .2s ease, opacity .15s ease;
  }
  .cb-tab-ind.saved { background: rgba(245, 197, 66, .14); border-color: rgba(245, 197, 66, .4); }
  .cb-tabs.has-ind .cb-tab.active,
  .cb-tabs.has-ind .cb-tab-saved.active { background: transparent; border-color: transparent; box-shadow: none; }
  .cb-tabs-sep { width: 1px; align-self: stretch; margin: 4px 2px; background: var(--border-hi); }

  /* ── Лейаут: результати + сайдбар з окремим скролом ── */
  .cb-layout {
    flex: 1; min-height: 0;
    display: grid; grid-template-columns: 1fr 288px;
    /* minmax(0,1fr) — інакше grid-рядок auto росте на весь контент
       сайдбара і власний скрол .cb-sidebar не спрацьовує. */
    grid-template-rows: minmax(0, 1fr);
    gap: var(--cb-gap);
  }
  .cb-main {
    min-height: 0; overflow-y: auto; overflow-x: hidden;
    padding-right: 8px;
    scroll-behavior: smooth;
  }
  .cb-main::-webkit-scrollbar { width: 6px; }
  .cb-main::-webkit-scrollbar-thumb { background: var(--hover-strong); border-radius: 3px; }
  .cb-main::-webkit-scrollbar-thumb:hover { background: var(--border-hi); }
  .cb-main::-webkit-scrollbar-track { background: transparent; }

  /* ── Панель інструментів: sticky, той самий скляний topbar, що на сторінці проєкту ── */
  .cb-toolbar {
    position: sticky; top: 0; z-index: 20;
    display: flex; gap: 8px; align-items: center; flex-wrap: wrap;
    padding: 6px 8px; margin-bottom: 8px;
    min-height: 52px;
    background: color-mix(in srgb, var(--bg-panel) 85%, transparent);
    backdrop-filter: blur(16px) saturate(1.2);
    -webkit-backdrop-filter: blur(16px) saturate(1.2);
    border: 1px solid var(--border);
    border-radius: 14px;
    transition: box-shadow .2s ease, border-color .2s ease;
  }
  /* Компактна плашка активного типу в toolbar — з'являється лише коли
     сторінка проскролена (іконку/назву й так видно в табах вище). */
  .cb-toolbar-type {
    --tb-c: var(--orange);
    display: none; align-items: center; gap: 7px;
    height: 28px; padding: 0 12px;
    border-radius: 999px;
    background: rgba(var(--tacc-rgb, var(--orange-rgb)), .16);
    border: 1px solid rgba(var(--tacc-rgb, var(--orange-rgb)), .4);
    color: var(--tb-c);
    font: 750 11px/1 var(--font-ui); letter-spacing: .02em;
    flex-shrink: 0;
  }
  .cb-toolbar-type.show { display: inline-flex; }
  .cb-toolbar-type i { font-size: 13px; }
  .cb-search {
    flex: 1 1 220px; min-width: 220px;
    display: flex; align-items: center; gap: 8px;
    height: 38px;
    background: var(--bg-input);
    border: 1px solid var(--border-hi);
    border-radius: 10px;
    padding: 0 12px;
    position: relative;
    transition: border-color .18s ease, box-shadow .18s ease;
  }
  .cb-search > i { color: var(--text-mute); font-size: 16px; }
  .cb-search input {
    flex: 1; min-width: 0; background: transparent; border: none; outline: none;
    color: var(--text); font: 500 13px/1.3 var(--font-ui);
  }
  .cb-search input::placeholder { color: var(--text-dim); }
  .cb-search:focus-within {
    border-color: rgba(var(--orange-rgb), .6);
    box-shadow: 0 0 0 3px rgba(var(--orange-rgb), .14);
  }
  .cb-search:focus-within > i { color: var(--orange); }
  .cb-search-clear {
    width: 22px; height: 22px; flex-shrink: 0;
    border: none; border-radius: 50%; cursor: pointer;
    background: var(--hover-strong); color: var(--text-mute);
    display: flex; align-items: center; justify-content: center;
    font-size: 11px;
    transition: background-color .15s ease, color .15s ease;
  }
  .cb-search-clear:hover { background: var(--orange); color: #fff; }

  /* ── Історія пошукових запитів ── */
  .cb-search-history {
    position: absolute; left: 0; right: 0; top: calc(100% + 8px);
    background: var(--bg-window);
    border: 1px solid var(--border-hi);
    border-radius: var(--cb-radius-md);
    box-shadow: var(--cb-shadow-strong);
    padding: 6px;
    z-index: 40;
    max-height: 280px; overflow-y: auto;
    animation: cb-pop .18s var(--cb-ease) both;
  }
  @keyframes cb-pop { from { opacity: 0; transform: translateY(-4px); } to { opacity: 1; transform: none; } }
  .cb-search-history-head {
    display: flex; align-items: center; justify-content: space-between;
    padding: 6px 8px 8px;
    font: 700 10.5px/1 var(--font-ui); letter-spacing: .06em; text-transform: uppercase;
    color: var(--text-dim);
  }
  .cb-search-history-clear {
    background: none; border: none; cursor: pointer;
    font: 600 11px/1 var(--font-ui); color: var(--text-mute); padding: 2px 4px;
  }
  .cb-search-history-clear:hover { color: var(--orange); }
  .cb-search-history-item {
    display: flex; align-items: center; gap: 10px;
    padding: 8px; border-radius: 8px; cursor: pointer;
    transition: background-color .15s ease;
  }
  .cb-search-history-item:hover { background: var(--hover); }
  .cb-search-history-item > i.ti-history { color: var(--text-mute); font-size: 14px; flex-shrink: 0; }
  .cb-search-history-q {
    flex: 1; min-width: 0; font: 500 12.5px/1.3 var(--font-ui); color: var(--text);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .cb-search-history-count { font: 600 11px/1 var(--font-mono); color: var(--text-dim); flex-shrink: 0; }
  .cb-search-history-remove {
    width: 20px; height: 20px; flex-shrink: 0;
    border: none; border-radius: 50%; cursor: pointer;
    background: transparent; color: var(--text-mute);
    display: flex; align-items: center; justify-content: center; font-size: 10px;
    transition: background-color .15s ease, color .15s ease;
  }
  .cb-search-history-remove:hover { background: var(--orange); color: #fff; }

  .cb-sort { display: flex; align-items: center; gap: 8px; flex-shrink: 0; }
  .cb-sort > i { color: var(--text-mute); font-size: 15px; }

  /* ── Активний контекст збірки (зелений акцент) ── */
  .cb-active-pack {
    display: flex; align-items: center; gap: 9px;
    padding: 10px 13px; margin-bottom: 12px;
    background: color-mix(in srgb, var(--green) 9%, var(--bg-card));
    border: 1px solid color-mix(in srgb, var(--green) 32%, transparent);
    border-radius: var(--cb-radius-md);
    color: var(--text); font-size: 12.5px; font-weight: 600;
  }
  .cb-active-pack .btn { margin-left: auto; }
  .cb-active-pack .cb-ctx-meta { color: var(--green); font-size: 10.5px; font-family: var(--font-mono); white-space: nowrap; }

  /* ── Секція результатів ── */
  .cb-section { margin-bottom: 22px; }
  .cb-section-head {
    display: flex; align-items: center; justify-content: space-between;
    gap: 10px; margin-bottom: 12px;
  }
  .cb-count {
    padding: 4px 12px; border-radius: 999px;
    background: var(--bg-input); border: 1px solid var(--border);
    font: 700 10.5px/1 var(--font-mono);
    color: var(--text-mute); white-space: nowrap;
  }

  /* ── Грід карток ── */
  .cb-grid.cb-refreshing { opacity: .55; transition: opacity .15s ease; pointer-events: none; }
  .cb-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(340px, 1fr));
    gap: 12px;
  }

  .cb-card {
    position: relative; overflow: hidden;
    display: flex; gap: 12px; padding: 14px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--cb-radius-md);
    cursor: pointer;
    box-shadow: var(--cb-shadow-card);
    transition: border-color .15s ease, background-color .15s ease, box-shadow .2s ease;
    animation: cb-card-in .3s var(--cb-ease) both;
  }
  @keyframes cb-card-in { from { opacity: 0; } to { opacity: 1; } }
  .cb-card:hover {
    border-color: var(--border-hi);
    background: color-mix(in srgb, var(--bg-card) 100%, var(--hover) 20%);
    box-shadow: var(--cb-shadow-strong);
  }
  .cb-card:focus-visible { outline: 2px solid var(--orange); outline-offset: 2px; }

  .cb-card-ic {
    width: 52px; height: 52px; min-width: 52px;
    border-radius: var(--cb-radius-sm);
    object-fit: cover;
    background: var(--bg-input);
  }
  .cb-card-ic-fallback {
    display: flex; align-items: center; justify-content: center;
    font-size: 22px; color: var(--cb-accent);
    background: color-mix(in srgb, var(--cb-accent) 14%, var(--bg-input));
  }
  .cb-card-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 4px; }
  .cb-card-name { display: flex; align-items: center; gap: 7px; min-width: 0; }
  .cb-card-title {
    flex: 1; min-width: 0;
    font: 700 13.5px/1.2 var(--font-ui); color: var(--text);
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  /* Лого джерела (Modrinth/CurseForge PNG) */
  .cb-src-logo { width: 14px; height: 14px; min-width: 14px; border-radius: 4px; object-fit: contain; }

  .cb-card-tags { display: flex; align-items: center; gap: 6px; margin-top: 2px; flex-wrap: wrap; }
  .cb-card-type {
    font: 700 9.5px/1 var(--font-ui);
    text-transform: uppercase; letter-spacing: .5px;
    color: var(--text-mute);
    border: 1px solid var(--border);
    border-radius: 4px; padding: 3px 6px;
    white-space: nowrap;
  }
  .cb-card-src {
    display: inline-flex; align-items: center; gap: 5px;
    font: 700 9.5px/1 var(--font-ui);
    padding: 3px 7px; border-radius: 999px;
    white-space: nowrap;
  }
  .cb-card-src img { width: 11px; height: 11px; border-radius: 3px; object-fit: contain; }
  .cb-card-src.mr { color: var(--green); background: rgba(16, 185, 129, .12); }
  .cb-card-src.cf { color: var(--orange); background: rgba(var(--orange-rgb), .12); }
  .cb-card-desc {
    font-size: 11.5px; line-height: 1.5;
    color: var(--text-mute);
    margin-top: 4px;
    display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .cb-card-meta { display: flex; gap: 5px; margin-top: 7px; flex-wrap: wrap; }
  .cb-card-meta .chip { padding: 2px 8px; font-size: 10.5px; }
  .cb-chip-skeleton { opacity: .6; }

  .cb-card-side {
    display: flex; flex-direction: column;
    align-items: stretch; justify-content: flex-start;
    gap: 8px; min-width: 122px; flex-shrink: 0;
  }
  .cb-downloads {
    font: 600 11px/1 var(--font-ui); color: var(--text-mute);
    display: inline-flex; align-items: center; gap: 5px;
    align-self: flex-end;
  }
  .cb-install-btn {
    white-space: nowrap; width: 100%;
    display: inline-flex; align-items: center; justify-content: center; gap: 5px;
  }
  .cb-progress-mini { width: 90px; }
  .cb-progress-bar {
    height: 5px; border-radius: 999px;
    background: var(--hover-strong); overflow: hidden;
  }
  .cb-progress-bar i {
    display: block; height: 100%;
    background: var(--accent-grad);
    border-radius: 999px;
    transition: width .25s ease;
  }

  /* ── Скелетон ── */
  .cb-card-skel { cursor: default; pointer-events: none; }
  .cb-card-skel:hover { border-color: var(--border); box-shadow: var(--cb-shadow-card); background: var(--bg-card); }
  .cb-skel {
    border-radius: 6px;
    background: linear-gradient(90deg, var(--hover) 25%, var(--hover-strong) 50%, var(--hover) 75%);
    background-size: 200% 100%;
    animation: cb-shimmer 1.3s linear infinite;
  }
  .cb-skel-ic { width: 52px; height: 52px; min-width: 52px; border-radius: var(--cb-radius-sm); }
  .cb-skel-line { height: 10px; }
  @keyframes cb-shimmer { from { background-position: 200% 0; } to { background-position: -200% 0; } }

  /* ── Порожній / нотатки ── */
  .cb-empty {
    display: flex; flex-direction: column; align-items: center; gap: 10px;
    padding: 56px 20px; color: var(--text-mute); font-size: 13px; text-align: center;
    border: 1px dashed var(--border-hi); border-radius: var(--cb-radius-lg);
  }
  .cb-empty i { font-size: 34px; opacity: .6; }
  .cb-empty-error i { color: var(--red); opacity: .9; }
  .cb-empty-error { max-width: 460px; margin: 0 auto; padding: 40px 16px; line-height: 1.5; }
  .cb-loading {
    display: flex; align-items: center; justify-content: center; gap: 10px;
    padding: 56px 0; color: var(--text-mute); font-size: 13.5px;
  }
  .cb-loading i { font-size: 18px; color: var(--orange); }
  .cb-cf-note {
    display: flex; align-items: center; gap: 8px;
    padding: 8px 14px; margin-bottom: 12px;
    background: var(--hover); border: 1px solid var(--border);
    border-radius: var(--cb-radius-md);
    color: var(--text-mute); font-size: 12px;
  }
  .cb-cf-note i { color: var(--yellow); flex-shrink: 0; }
  .cb-cf-note.cb-cf-error { background: rgba(239, 68, 68, .08); border-color: rgba(239, 68, 68, .3); color: var(--text); }
  .cb-cf-note.cb-cf-error i { color: var(--red); }
  .cb-cf-note.cb-cf-loading i { color: var(--text-mute); }

  /* Розумний пошук — інформаційна підказка над гродом результатів. */
  .cb-smart-hint {
    display: flex; align-items: center; gap: 8px;
    padding: 8px 14px; margin-bottom: 10px;
    background: rgba(88, 166, 255, .08); border: 1px solid rgba(88, 166, 255, .25);
    border-radius: var(--cb-radius-md);
    color: var(--text-mute); font-size: 12px;
  }
  .cb-smart-hint i { color: #58a6ff; flex-shrink: 0; }
  .cb-cf-attr {
    display: flex; align-items: center; justify-content: center; gap: 8px;
    margin-top: 18px; padding: 10px 14px;
    border-top: 1px solid var(--border);
    color: var(--text-dim); font-size: 11px;
  }
  .cb-cf-logo { flex: none; border-radius: 4px; object-fit: contain; }
  .cb-cf-attr a { color: var(--orange); text-decoration: none; }
  .cb-cf-attr a:hover { text-decoration: underline; }

  /* ── Пагінатор ── */
  .cb-pager { display: flex; align-items: center; gap: 12px; }
  .cb-pager:last-of-type { justify-content: center; padding: 18px 0 6px; }
  .cb-pager-count {
    font: 700 11px/1 var(--font-mono); color: var(--text-dim); white-space: nowrap;
    padding: 4px 12px; border-radius: 999px;
    background: var(--bg-input); border: 1px solid var(--border);
  }

  /* ── Правий сайдбар: фільтри ── */
  .cb-sidebar {
    min-height: 0; overflow-y: auto;
    overscroll-behavior: contain;
    padding: 0 4px 16px 0;
  }
  .cb-sidebar::-webkit-scrollbar { width: 6px; }
  .cb-sidebar::-webkit-scrollbar-thumb { background: var(--hover-strong); border-radius: 3px; }
  .cb-sidebar::-webkit-scrollbar-thumb:hover { background: var(--border-hi); }
  .cb-sidebar::-webkit-scrollbar-track { background: transparent; }

  .cbs-panel {
    background: var(--bg-card); border: 1px solid var(--border);
    border-radius: var(--cb-radius-lg); overflow: hidden;
    box-shadow: var(--cb-shadow-card);
  }
  .cbs-title {
    display: flex; align-items: center; gap: 9px;
    padding: 14px 16px; font: 700 14px/1 var(--font-ui); color: var(--text);
    border-bottom: 1px solid var(--border);
  }
  .cbs-title i { font-size: 16px; color: var(--orange); }
  .cbs-sec { border-top: 1px solid var(--border); }
  .cbs-sec:first-of-type { border-top: none; }
  .cbs-head {
    display: flex; align-items: center; justify-content: space-between; width: 100%;
    padding: 14px 16px 10px; background: transparent; border: none; cursor: pointer;
    font: 700 11px/1 var(--font-ui); letter-spacing: .08em; text-transform: uppercase;
    color: var(--text-mute); text-align: left; transition: color .15s ease;
  }
  .cbs-head:hover { color: var(--text); }
  .cbs-head:focus-visible { outline: 2px solid var(--orange); outline-offset: -2px; }
  .cbs-caret { font-size: 14px; transition: transform .2s var(--cb-ease); }
  .cbs-sec.collapsed .cbs-caret { transform: rotate(-90deg); }
  .cbs-fold { display: grid; grid-template-rows: 1fr; overflow: hidden; transition: grid-template-rows .22s var(--cb-ease); }
  .cbs-sec.collapsed .cbs-fold { grid-template-rows: 0fr; }
  .cbs-fold > .cbs-body { overflow: hidden; min-height: 0; }
  .cbs-body { padding: 0 16px 16px; display: flex; flex-direction: column; gap: 10px; }
  .cbs-fold > .cbs-body.cbs-body-list { gap: 2px; padding: 0 8px 12px; max-height: 300px; overflow-y: auto; }
  /* Згорнута секція: нижній відступ тіла теж схлопується (раніше падінг 12–16px не
     стискався разом із рядком 0fr, і верх чіпів «Fabric/Forge» стирчав під заголовком). */
  .cbs-body { transition: padding-bottom .22s var(--cb-ease); }
  .cbs-sec.collapsed .cbs-fold > .cbs-body { padding-bottom: 0; }

  /* Чіпи */
  .cbs-chips { display: flex; flex-wrap: wrap; gap: 6px; }
  .cbs-chip {
    display: inline-flex; align-items: center; gap: 6px;
    padding: 5px 11px; border-radius: 999px; cursor: pointer;
    background: var(--hover); border: 1px solid var(--border);
    font: 600 12px/1.1 var(--font-ui); color: var(--text-mute);
    transition: background-color .15s ease, color .15s ease, border-color .15s ease;
  }
  .cbs-chip:hover { color: var(--text); border-color: var(--border-hi); background: var(--hover-strong); }
  .cbs-chip:active { transform: scale(.96); }
  .cbs-chip i { font-size: 13px; color: var(--chip-c, currentColor); }
  .cbs-chip-ic { width: 14px; height: 14px; object-fit: contain; flex-shrink: 0; }
  .cbs-chip.on { color: var(--orange); border-color: rgba(var(--orange-rgb), .45); background: rgba(var(--orange-rgb), .15); }
  .cbs-chip.on i { color: var(--orange); }
  .cbs-src {
    font: 800 9px/1 var(--font-mono); padding: 2px 4px; border-radius: 4px; letter-spacing: .3px;
    color: var(--orange); background: rgba(var(--orange-rgb), .14);
  }

  /* Джерело: сегментований перемикач */
  .cbs-seg {
    display: flex; gap: 3px; padding: 3px; border-radius: 11px;
    background: var(--bg-input); border: 1px solid var(--border);
  }
  .cbs-seg button {
    flex: 1; min-width: 0; display: inline-flex; align-items: center; justify-content: center; gap: 5px;
    padding: 0 6px; min-height: 32px; border-radius: 8px; border: 1px solid transparent; cursor: pointer;
    background: transparent; color: var(--text-mute);
    font: 650 11.5px/1 var(--font-ui); white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
    transition: background-color .15s ease, color .15s ease, border-color .15s ease;
  }
  .cbs-seg button img { width: 14px; height: 14px; object-fit: contain; flex-shrink: 0; }
  .cbs-seg button:hover { color: var(--text); }
  .cbs-seg button.on {
    color: var(--text); background: var(--bg-card);
    border-color: rgba(var(--orange-rgb), .4); box-shadow: 0 4px 12px -6px rgba(0, 0, 0, .6);
  }

  .cbs-select { width: 100%; }
  .cbs-link {
    display: inline-flex; align-items: center; gap: 5px; align-self: flex-start; margin-top: 8px;
    background: none; border: none; cursor: pointer; padding: 4px 0;
    font: 600 11.5px/1 var(--font-ui); color: var(--text-mute); transition: color .15s ease;
  }
  .cbs-link:hover { color: var(--text); }

  .cbs-foot { border-top: 1px solid var(--border); padding: 12px 16px; }
  .cbs-reset {
    width: 100%; display: inline-flex; align-items: center; justify-content: center; gap: 6px;
    height: 36px; border-radius: 10px; cursor: pointer;
    background: var(--hover); border: 1px solid var(--border);
    font: 600 12px/1 var(--font-ui); color: var(--text-mute);
    transition: background-color .15s ease, color .15s ease;
  }
  .cbs-reset:hover { color: var(--text); border-color: var(--border-hi); background: var(--hover-strong); }

  /* Перемикач цілі встановлення «Збірки ↔ Сервери» */
  .cb-target-tabs {
    display: flex; gap: 4px; padding: 3px; margin-bottom: 8px;
    background: var(--bg-input); border: 1px solid var(--border);
    border-radius: 11px; flex-shrink: 0;
  }
  .cb-target-tab {
    flex: 1; min-width: 0;
    display: inline-flex; align-items: center; justify-content: center; gap: 6px;
    min-height: 32px; padding: 0 8px;
    font: 650 11.5px/1 var(--font-ui); color: var(--text-mute);
    background: transparent; border: 1px solid transparent; border-radius: 8px;
    cursor: pointer;
    transition: color .15s ease, background-color .15s ease;
  }
  .cb-target-tab:hover { color: var(--text); }
  .cb-target-tab.on { background: var(--bg-card); color: var(--text); border-color: rgba(var(--orange-rgb), .4); }
  .cb-target-tab i { font-size: 13px; }
  .cb-target-tab span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .cb-dlg-target-tabs { margin-bottom: 10px; }
  .cb-target-empty { padding: 10px 8px; font: 500 11.5px/1.45 var(--font-ui); color: var(--text-dim); text-align: center; }

  .cb-opt {
    display: flex; align-items: center; gap: 9px;
    min-height: 38px; padding: 6px 8px; border-radius: 9px; cursor: pointer;
    color: var(--text); font-size: 12.5px; border: 1px solid transparent;
    transition: border-color .15s ease, background-color .15s ease;
  }
  .cb-opt:hover { background: var(--hover); }
  .cb-opt:has(input:checked) { border-color: rgba(var(--orange-rgb), .5); background: color-mix(in srgb, var(--orange) 6%, transparent); }
  .cb-opt input { accent-color: var(--orange); flex-shrink: 0; }
  .cb-opt > i { color: var(--text-mute); flex-shrink: 0; }
  .cb-env-locked {
    display: flex; align-items: center; gap: 8px; padding: 10px 12px;
    background: var(--hover); border-radius: 10px; color: var(--text-mute); font-size: 12px;
  }
  .cb-env-locked i { color: var(--orange); font-size: 14px; flex-shrink: 0; }
  .cb-pack-mini { width: 26px; height: 26px; min-width: 26px; border-radius: 7px; object-fit: cover; }
  .cb-pack-mini-fallback {
    width: 26px; height: 26px; min-width: 26px; border-radius: 7px;
    display: flex; align-items: center; justify-content: center; font-size: 14px;
    color: var(--pcol); background: color-mix(in srgb, var(--pcol) 18%, transparent);
  }
  .cb-pack-opt-name { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 600; }
  .cb-sidebar :global(.cb-pp .pp-row) { padding: 6px 10px; font-size: 12px; border-radius: 9px; }
  .cb-sidebar :global(.cb-pp .pp-row.sel) { background: rgba(var(--orange-rgb), .12); }
  :global(.cb-pp-install .pp-row) { cursor: default; padding: 8px 10px; }
  :global(.cb-pp-install .pp-ic) { width: 34px; height: 34px; }
  .cb-pp-installed-badge { display: inline-flex; align-items: center; justify-content: center; color: var(--green); font-size: 15px; }

  /* Пілюлі-джерела «Збережено» (Закладки/Колекції) */
  .cb-saved-subtabs .pill,
  .cb-saved-type-pills .pill {
    display: inline-flex; align-items: center; gap: 6px;
    height: 34px; padding: 0 14px; border-radius: 999px;
    background: var(--hover); border: 1px solid var(--border);
    color: var(--text-mute); font: 650 12.5px/1 var(--font-ui); cursor: pointer;
    transition: background-color .15s ease, color .15s ease, border-color .15s ease;
  }
  .cb-saved-subtabs .pill:hover,
  .cb-saved-type-pills .pill:hover { color: var(--text); border-color: var(--border-hi); }
  .cb-saved-subtabs .pill.on,
  .cb-saved-type-pills .pill.on { color: var(--orange); border-color: rgba(var(--orange-rgb), .45); background: rgba(var(--orange-rgb), .14); }
  .cb-saved-type-pills { display: flex; gap: 6px; flex-wrap: wrap; margin-top: -4px; }

  /* ═══════════════════════════════════════════════════════════════════
     МОДАЛКИ БРАУЗЕРА (спільні з іншими вікнами лаунчера)
     ═══════════════════════════════════════════════════════════════════ */
  .modal-overlay {
    position: fixed; inset: 0;
    background: rgba(0, 0, 0, .68);
    backdrop-filter: blur(8px); -webkit-backdrop-filter: blur(8px);
    display: flex; align-items: center; justify-content: center;
    padding: 32px; z-index: 1000000;
    animation: cb-modal-overlay-in .2s ease-out both;
  }
  .modal {
    background: var(--bg-panel);
    border: 1px solid var(--border-hi);
    border-radius: var(--cb-radius-lg, 18px);
    box-shadow: var(--cb-shadow-strong);
  }
  .cb-modal {
    width: 500px; max-width: 100%; max-height: 82vh;
    display: flex; flex-direction: column;
    animation: cb-modal-in .28s var(--cb-ease) both;
  }
  @keyframes cb-modal-overlay-in { from { opacity: 0; } to { opacity: 1; } }
  @keyframes cb-modal-in { from { opacity: 0; transform: translateY(10px); } to { opacity: 1; transform: none; } }
  .modal-head {
    display: flex; align-items: center; justify-content: space-between;
    padding: 15px 18px; border-bottom: 1px solid var(--border);
  }
  .cb-modal-title { display: flex; gap: 12px; align-items: center; min-width: 0; }
  .cb-modal-name { font: 750 14px/1.2 var(--font-ui); color: var(--text); }
  .cb-modal-sub { font-size: 11px; color: var(--text-mute); text-transform: capitalize; margin-top: 2px; }
  .modal-x {
    width: 30px; height: 30px;
    background: transparent; border: 1px solid transparent;
    color: var(--text-mute); cursor: pointer;
    display: flex; align-items: center; justify-content: center;
    border-radius: 8px; font-size: 16px;
    transition: color .15s ease, background-color .15s ease;
  }
  .modal-x:hover { color: var(--text); background: var(--hover); }
  .cb-modal-body { padding: 16px 18px; overflow-y: auto; }
  .cb-modal-search {
    display: flex; align-items: center; gap: 9px;
    margin: -16px -20px 14px; padding: 10px 20px;
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
    background: var(--bg-input); border: 1px solid var(--border);
    border-radius: var(--cb-radius-md);
    transition: border-color .15s ease, background-color .15s ease;
  }
  .cb-pack-row:hover {
    border-color: color-mix(in srgb, var(--pcol, var(--orange)) 50%, var(--border));
    background: color-mix(in srgb, var(--pcol, var(--orange)) 5%, var(--bg-input));
  }
  .cb-pack-row.installed { opacity: .55; }
  .cb-pp-shaurma-head {
    display: flex; align-items: center; gap: 7px;
    padding: 2px 2px 8px;
    font-size: 11.5px; font-weight: 700; text-transform: uppercase; letter-spacing: .02em;
    color: var(--violet-l, #b9a4ff);
  }
  .cb-pp-shaurma-head i { font-size: 13px; }
  .cb-pack-list-shaurma { margin-bottom: 4px; }
  .cb-pack-row-shaurma {
    background: color-mix(in srgb, var(--pcol, var(--violet)) 6%, var(--bg-input));
    border-color: color-mix(in srgb, var(--pcol, var(--violet)) 22%, var(--border));
  }
  .cb-pp-shaurma-sep { height: 1px; background: var(--border); margin: 12px 0; }
  .cb-pack-info { display: flex; gap: 11px; align-items: center; min-width: 0; }
  .cb-pack-ic {
    width: 32px; height: 32px; min-width: 32px; border-radius: 8px;
    display: flex; align-items: center; justify-content: center; flex-shrink: 0;
    background: color-mix(in srgb, var(--pcol, var(--orange)) 18%, var(--bg-input));
    color: var(--pcol, var(--orange)); font-size: 14px;
    overflow: hidden; position: relative;
  }
  .cb-pack-ic.has-icon { background: transparent; }
  .cb-pack-ic img { width: 100%; height: 100%; object-fit: cover; border-radius: 8px; }
  .cb-pack-name { font: 700 13px/1.2 var(--font-ui); color: var(--text); }
  .cb-pack-meta { font: 500 11px/1.2 var(--font-mono); color: var(--text-mute); margin-top: 3px; }
  .cb-warn { display: flex; align-items: center; gap: 6px; font-size: 10.5px; color: var(--orange); margin-top: 4px; }
  .cb-hint {
    display: flex; gap: 8px; align-items: flex-start;
    padding: 10px 12px;
    background: var(--hover); border: 1px solid var(--border);
    border-radius: var(--cb-radius-md);
    font-size: 12px; line-height: 1.5; color: var(--text-mute);
  }
  .cb-hint > i { color: var(--orange); margin-top: 1px; flex-shrink: 0; }
  .cb-dlg-label {
    display: block; font: 700 10.5px/1 var(--font-ui); color: var(--text-mute);
    text-transform: uppercase; letter-spacing: .05em; margin-bottom: 8px;
  }
  .cb-loading.sm, .cb-empty.sm { padding: 28px 16px; }
  .cb-empty.sm i { font-size: 26px; }

  .cb-dlg-hint { margin-top: 10px; }
  .cb-dlg-actions {
    display: flex; justify-content: flex-end; align-items: center;
    gap: 8px; margin-top: 14px;
  }
  .cb-dlg-actions-stacked { flex-direction: column; align-items: stretch; justify-content: flex-start; margin-top: 10px; }
  .cb-dlg-actions-stacked :global(.btn) { width: 100%; justify-content: center; }

  /* ── Модалка вибору світів (датапаки) ── */
  .cb-world-tools { display: flex; gap: 6px; margin-bottom: 10px; }
  .cb-world-list {
    display: flex; flex-direction: column; gap: 6px;
    max-height: 300px; overflow-y: auto;
    border: 1px solid var(--border); border-radius: var(--cb-radius-md); padding: 6px;
  }
  .cb-world-row {
    display: flex; align-items: center; gap: 9px;
    padding: 9px 11px;
    background: var(--bg-input); border: 1px solid transparent;
    border-radius: var(--cb-radius-sm); cursor: pointer;
    font-size: 12.5px;
    transition: border-color .15s ease, background-color .15s ease;
  }
  .cb-world-row:hover { border-color: var(--border-hi); }
  .cb-world-row:has(input:checked) { border-color: rgba(var(--orange-rgb), .5); background: rgba(var(--orange-rgb), .07); }
  .cb-world-row input { accent-color: var(--orange); cursor: pointer; }
  .cb-world-row i { color: var(--text-mute); font-size: 15px; }
  .cb-world-row span { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .cb-world-global {
    display: flex; align-items: center; gap: 9px;
    margin-top: 12px; padding: 10px 12px;
    background: var(--hover); border: 1px dashed var(--border-hi);
    border-radius: var(--cb-radius-md); cursor: pointer;
    font: 700 12px/1.3 var(--font-ui); color: var(--text-mute);
    transition: border-color .15s ease, background-color .15s ease;
  }
  .cb-world-global:hover { border-color: rgba(var(--orange-rgb), .5); background: rgba(var(--orange-rgb), .04); }
  .cb-world-global input { accent-color: var(--orange); cursor: pointer; margin: 0; }
  .cb-world-global i { color: var(--orange); font-size: 14px; }
  .cb-world-foot { display: flex; align-items: center; justify-content: space-between; gap: 10px; margin-top: 12px; }
  .cb-world-count { font: 600 11.5px/1 var(--font-mono); color: var(--text-mute); }
  .btn-sm { padding: 6px 12px; font-size: 11.5px; }

  /* ── Розділ «Збережено» (закладки + колекції) ── */
  .cb-add-col-count { font-size: 10.5px; color: var(--text-mute); background: var(--bg); padding: 1px 7px; border-radius: 999px; }
  .ctx-item-in { opacity: .6; }
  .ctx-item-in i { color: var(--green); }
  .cb-add-col-in { font-size: 10px; font-weight: 700; color: var(--green); text-transform: uppercase; letter-spacing: .03em; }
  .cb-saved {
    display: flex; flex-direction: column; gap: 14px;
    padding: 4px 2px 28px;
    min-height: 0; flex: 1; overflow-y: auto; overflow-x: hidden;
    padding-right: 8px;
    scroll-behavior: smooth;
  }
  .cb-saved::-webkit-scrollbar { width: 6px; }
  .cb-saved::-webkit-scrollbar-thumb { background: var(--hover-strong); border-radius: 3px; }
  .cb-saved::-webkit-scrollbar-thumb:hover { background: var(--border-hi); }
  .cb-saved::-webkit-scrollbar-track { background: transparent; }
  .cb-saved-subtabs { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  .cb-saved-back { margin-left: auto; }
  .cb-saved-loading {
    display: flex; align-items: center; justify-content: center; gap: 10px;
    padding: 50px 0; color: var(--text-mute); font-size: 13px;
  }

  .cb-card-actions { display: flex; align-items: center; justify-content: flex-end; gap: 6px; width: 100%; flex-shrink: 0; }
  .cb-bookmark-btn {
    display: inline-flex; align-items: center; justify-content: center;
    width: 26px; height: 26px; flex-shrink: 0;
    border: 1px solid var(--border); border-radius: 999px;
    background: var(--hover);
    color: var(--text-mute); cursor: pointer; font-size: 13px; padding: 0;
    transition: color .15s ease, border-color .15s ease, background-color .15s ease;
  }
  .cb-bookmark-btn:hover { color: var(--text); background: var(--hover-strong); border-color: var(--border-hi); }
  .cb-bookmark-btn.on { color: #f5c542; border-color: color-mix(in srgb, #f5c542 55%, var(--border)); background: rgba(245, 197, 66, .12); }

  /* ═══════════════ РЕЖИМИ ВІДОБРАЖЕННЯ: перемикач ═══════════════ */
  .cb-view-toggle {
    display: flex; gap: 2px; padding: 3px; flex-shrink: 0; height: 38px;
    background: var(--hover); border: 1px solid var(--border); border-radius: 10px;
  }
  .cb-view-btn {
    width: 32px; display: inline-flex; align-items: center; justify-content: center;
    border-radius: 8px; border: none; background: transparent;
    color: var(--text-mute); cursor: pointer; font-size: 16px; padding: 0;
    transition: color .15s ease, background-color .15s ease;
  }
  .cb-view-btn:hover { color: var(--text); background: var(--hover); }
  .cb-view-btn.on { color: var(--orange); background: rgba(var(--orange-rgb), .16); }

  /* Лого джерела у темній плашці (кут банера / поруч із назвою) */
  .cb-srcbadge {
    display: inline-flex; align-items: center; justify-content: center; flex-shrink: 0;
    width: 24px; height: 24px; border-radius: 7px; background: rgba(0, 0, 0, .55);
    backdrop-filter: blur(6px);
  }
  .cb-srcbadge img { width: 14px; height: 14px; object-fit: contain; }
  .cb-card-installed { color: var(--green); font-size: 15px; flex-shrink: 0; }

  /* ═══════════════ РЕЖИМ «СІТКА» (банер зверху) ═══════════════ */
  .cb-wrap[data-view='grid'] .cb-grid { grid-template-columns: repeat(auto-fill, minmax(250px, 1fr)); gap: 12px; }
  .cb-wrap[data-view='grid'] .cb-card { flex-direction: column; gap: 0; padding: 0; }
  .cb-card-banner {
    position: relative; height: 96px; flex-shrink: 0; overflow: hidden;
    background: color-mix(in srgb, var(--orange) 10%, var(--bg-card));
  }
  .cb-card-banner::after {
    content: ''; position: absolute; inset: 0;
    background:
      linear-gradient(180deg, rgba(0, 0, 0, .1) 0%, color-mix(in srgb, var(--bg-card) 88%, transparent) 70%, var(--bg-card) 100%),
      radial-gradient(120% 90% at 0% 0%, rgba(var(--orange-rgb), .18), transparent 60%);
  }
  .cb-card-banner-img { position: relative; display: block; width: 100%; height: 100%; object-fit: cover; }
  .cb-card-banner .cb-srcbadge { position: absolute; top: 10px; right: 10px; z-index: 1; }
  .cb-wrap[data-view='grid'] .cb-card-ic {
    position: absolute; top: 68px; left: 14px; z-index: 2;
    width: 56px; height: 56px; min-width: 56px; box-sizing: border-box;
    border: 3px solid var(--bg-card); border-radius: var(--cb-radius-sm);
  }
  .cb-wrap[data-view='grid'] .cb-card-body { padding: 34px 14px 0; gap: 6px; }
  .cb-wrap[data-view='grid'] .cb-card-title { font-size: 14px; }
  .cb-card-dl {
    display: inline-flex; align-items: center; gap: 4px; flex-shrink: 0;
    font: 600 11.5px/1 var(--font-ui); color: var(--text-mute); white-space: nowrap;
  }
  .cb-wrap[data-view='grid'] .cb-card-desc { margin-top: 0; min-height: 3em; }
  .cb-wrap[data-view='grid'] .cb-card-side {
    flex-direction: row; flex-wrap: wrap; align-items: center; justify-content: space-between;
    gap: 8px; min-width: 0; padding: 12px 14px 14px;
  }
  .cb-card-author {
    flex: 1 1 0; min-width: 0; display: inline-flex; align-items: center; gap: 5px;
    font: 500 12px/1 var(--font-ui); color: var(--text-mute);
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .cb-wrap[data-view='grid'] .cb-install-btn { width: auto; flex-shrink: 0; }
  .cb-wrap[data-view='grid'] .cb-progress-mini { order: 3; width: 100%; }
  /* Кнопки-закладки лежать поверх банера й з'являються при наведенні */
  .cb-wrap[data-view='grid'] .cb-card-actions { position: absolute; top: 10px; left: 10px; width: auto; z-index: 3; }
  .cb-wrap[data-view='grid'] .cb-card-actions .cb-bookmark-btn {
    background: rgba(0, 0, 0, .58); color: #fff; border-color: transparent; opacity: 0;
  }
  .cb-wrap[data-view='grid'] .cb-card:hover .cb-card-actions .cb-bookmark-btn,
  .cb-wrap[data-view='grid'] .cb-card:focus-within .cb-card-actions .cb-bookmark-btn,
  .cb-wrap[data-view='grid'] .cb-card-actions .cb-bookmark-btn.on { opacity: 1; }

  /* ═══════════════ РЕЖИМ «КОМПАКТНИЙ СПИСОК» ═══════════════ */
  .cb-wrap[data-view='compact'] .cb-grid { grid-template-columns: 1fr; gap: 6px; }
  .cb-wrap[data-view='compact'] .cb-card {
    align-items: center; gap: 12px; padding: 10px 14px; border-radius: var(--cb-radius-sm);
  }
  .cb-wrap[data-view='compact'] .cb-card-ic { width: 44px; height: 44px; min-width: 44px; }
  .cb-wrap[data-view='compact'] .cb-card-name { gap: 8px; }
  .cb-wrap[data-view='compact'] .cb-card-title { flex: 0 1 auto; font-size: 13.5px; }
  .cb-wrap[data-view='compact'] .cb-card-desc { margin-top: 0; font-size: 12px; -webkit-line-clamp: 1; }
  .cb-card-sub {
    display: flex; align-items: center; gap: 12px; margin-top: 2px;
    font: 500 11.5px/1 var(--font-ui); color: var(--text-dim);
  }
  .cb-card-sub span { display: inline-flex; align-items: center; gap: 5px; }
  .cb-wrap[data-view='compact'] .cb-card-side { flex-direction: row; align-items: center; justify-content: flex-end; gap: 10px; min-width: 0; }
  .cb-wrap[data-view='compact'] .cb-card-actions { width: auto; }
  .cb-wrap[data-view='compact'] .cb-install-btn { width: auto; min-width: 100px; }
  .cb-wrap[data-view='compact'] .cb-progress-mini { width: 70px; }

  /* Колекції: список + картка */
  .cb-collections-toolbar { display: flex; justify-content: flex-end; }
  .cb-collections-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(180px, 1fr)); gap: 10px; }
  .cb-collection-card {
    position: relative;
    display: flex; flex-direction: column; align-items: center; gap: 6px;
    padding: 22px 14px 16px;
    background: var(--bg-card); border: 1px solid var(--border); border-radius: var(--cb-radius-md);
    box-shadow: var(--cb-shadow-card);
    cursor: pointer; text-align: center;
    transition: border-color .15s ease, background-color .15s ease;
  }
  .cb-collection-card:hover { border-color: color-mix(in srgb, var(--pcol) 50%, var(--border)); background: color-mix(in srgb, var(--bg-card) 100%, var(--hover) 20%); }
  .cb-collection-icon {
    width: 48px; height: 48px; border-radius: var(--cb-radius-sm);
    display: flex; align-items: center; justify-content: center; font-size: 24px;
    background: color-mix(in srgb, var(--pcol, var(--violet)) 16%, var(--bg-input));
    color: var(--pcol, var(--violet));
  }
  .cb-collection-name {
    font: 700 13px/1.3 var(--font-ui); color: var(--text);
    max-width: 100%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .cb-collection-count { font: 600 11px/1 var(--font-ui); color: var(--text-dim); }
  .cb-collection-actions { position: absolute; top: 8px; right: 8px; display: flex; gap: 4px; opacity: 0; transition: opacity .15s ease; }
  .cb-collection-card:hover .cb-collection-actions { opacity: 1; }
  .cb-collection-delete:hover { color: var(--red); }

  .cb-collection-header {
    display: flex; align-items: center; gap: 12px; margin-bottom: 4px;
    /* Той самий sticky-патерн, що .cb-toolbar, аби кнопки «Назад»/«Встановити
       колекцію» лишались під рукою під час скролу довгого списку. */
    position: sticky; top: 0; z-index: 5;
    padding: 4px 4px 10px; margin: -4px -4px 0;
    background: color-mix(in srgb, var(--bg-panel) 85%, transparent);
    backdrop-filter: blur(14px); -webkit-backdrop-filter: blur(14px);
  }
  .cb-collection-header h3 {
    flex: 1; margin: 0; font: 700 15px/1.3 var(--font-ui); color: var(--text);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }

  .cb-collection-name-input {
    width: 100%; padding: 10px 12px;
    background: var(--bg-input); border: 1px solid var(--border); border-radius: 10px;
    color: var(--text); font: 500 13px/1.4 var(--font-ui);
  }
  .cb-collection-name-input:focus { outline: none; border-color: rgba(var(--orange-rgb), .5); }

  .cb-col-label {
    display: block; margin: 12px 0 6px;
    font: 700 10.5px/1 var(--font-ui); color: var(--text-mute);
    text-transform: uppercase; letter-spacing: .05em;
  }
  .cb-col-colors { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  .cb-col-dot {
    width: 26px; height: 26px; border-radius: 50%;
    border: 2px solid transparent; cursor: pointer;
    transition: transform .12s ease, border-color .12s ease;
  }
  .cb-col-dot:hover { transform: scale(1.1); }
  .cb-col-dot.sel { border-color: var(--text); box-shadow: 0 0 0 2px color-mix(in srgb, var(--text) 30%, transparent); }
  .cb-col-custom {
    position: relative; width: 30px; height: 30px; border-radius: 9px;
    display: flex; align-items: center; justify-content: center;
    border: 1.5px dashed color-mix(in srgb, var(--pcol, var(--violet)) 60%, var(--border));
    color: var(--pcol, var(--violet)); cursor: pointer; overflow: hidden;
  }
  .cb-col-custom input[type='color'] { position: absolute; inset: -6px; width: 44px; height: 44px; opacity: 0; cursor: pointer; }
  .cb-col-picker { margin-top: 2px; }
  .cb-collection-picker { max-height: 46vh; overflow-y: auto; margin-top: 2px; }

  .cb-collection-plan { display: flex; flex-direction: column; gap: 6px; max-height: 340px; overflow-y: auto; }
  .cb-plan-row {
    display: flex; align-items: center; gap: 10px;
    padding: 8px 10px; border-radius: 8px;
    background: var(--hover); border: 1px solid var(--border);
    font-size: 12.5px;
  }
  .cb-plan-row i { font-size: 15px; flex-shrink: 0; }
  .cb-plan-name { flex: 1; font-weight: 600; color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .cb-plan-status { font: 600 11px/1 var(--font-ui); color: var(--text-dim); flex-shrink: 0; }
  .cb-plan-install i { color: var(--green); }
  .cb-plan-update i { color: var(--blue, #3498db); }
  .cb-plan-already { opacity: .6; }
  .cb-plan-already i { color: var(--text-dim); }
  .cb-plan-incompatible { border-color: color-mix(in srgb, var(--orange) 40%, var(--border)); }
  .cb-plan-incompatible i { color: var(--orange); }

  /* ── Адаптивність ── */
  /* Кнопка фільтрів видима лише у вузькому режимі. */
  .cb-filters-toggle { display: none; }
  .cb-sidebar-backdrop { display: none; }

  @media (max-width: 1100px) {
    /* ВУЗЬКЕ ВІКНО. Раніше сітка ставала 1 колонкою, а сайдбар падав у
       неявний рядок висотою 300px і стискав список до смужки, а sticky-
       toolbar накладався на картки. Тепер: список займає ВСЮ висоту, а
       фільтри — висувна панель поверх (відкривається кнопкою в toolbar). */
    .cb-layout { grid-template-columns: minmax(0, 1fr); position: relative; }
    .cb-main { padding-right: 4px; }

    .cb-filters-toggle {
      display: inline-flex; align-items: center; gap: 7px; flex-shrink: 0;
      height: 38px; padding: 0 12px;
      background: var(--bg-input); border: 1px solid var(--border-hi); border-radius: 10px;
      color: var(--text-mute); cursor: pointer;
      font: 650 12.5px/1 var(--font-ui);
      transition: color .15s ease, border-color .15s ease, background-color .15s ease;
    }
    .cb-filters-toggle i { font-size: 15px; }
    .cb-filters-toggle:hover { color: var(--text); background: var(--hover); }
    .cb-filters-toggle.on {
      color: var(--orange); border-color: rgba(var(--orange-rgb), .55);
      background: rgba(var(--orange-rgb), .12);
    }

    .cb-sidebar {
      display: none;
      position: absolute; top: 0; right: 0; bottom: 0; z-index: 30;
      width: min(340px, 100%);
      max-height: none;
      padding: 0 4px 0 0;
      box-shadow: var(--cb-shadow-strong);
      border-radius: var(--cb-radius-lg);
      background: var(--bg-window);
    }
    .cb-layout.filters-open .cb-sidebar { display: block; animation: cb-pop .18s var(--cb-ease) both; }
    .cb-sidebar-backdrop {
      display: block; position: absolute; inset: 0; z-index: 29;
      border: none; padding: 0; cursor: default;
      background: rgba(0, 0, 0, .35);
    }

    /* Toolbar: пошук не розпирає рядок, елементи переносяться акуратно. */
    .cb-toolbar { row-gap: 6px; }
    .cb-search { flex: 1 1 160px; min-width: 0; }
  }
  @media (max-width: 640px) {
    .cb-grid,
    .cb-wrap[data-view='grid'] .cb-grid { grid-template-columns: minmax(0, 1fr); }
    .cb-card { flex-wrap: wrap; }
    .cb-card-side { min-width: 100%; flex-direction: row; align-items: center; justify-content: space-between; }
    .cb-card-actions { width: auto; }
    .cb-install-btn { width: auto; min-width: 140px; }
    /* Дуже вузько: кнопка фільтрів лишається іконкою без підпису. */
    .cb-filters-toggle span { display: none; }
    .cb-filters-toggle { padding: 0 11px; }
  }

  @media (prefers-reduced-motion: reduce) {
    .cb-wrap, .cb-card, .cb-search-history { animation: none !important; transition: none !important; }
  }
</style>
