<script lang="ts">
  import UiModal from './UiModal.svelte'
  // ── Розділ «Моди» на сторінці збірки ───────────────────────────────────
  // Повноцінний менеджер модів:
  //   · картки модів: іконка (з jar або Modrinth), реальна назва/версія,
  //     автор, бейджі лоадера, джерела (Modrinth/CurseForge) та оновлення;
  //   · пошук, фільтр (всі / з оновленнями / увімкнені / вимкнені),
  //     сортування (дата встановлення / назва / наявність оновлення);
  //   · увімкнення/вимкнення (.jar.disabled), видалення (з підтвердженням);
  //   · зміна версії мода (модалка зі списком версій під версію гри/лоадер);
  //   · залежності: пошук відсутніх + встановлення знайдених;
  //   · «Оновити всі» з резервною копією старих версій та відновленням.
  import { onMount, untrack } from 'svelte'
  import { t } from './i18n.svelte'
  import { App, on } from './wails'
  import { modIcon } from './modIcon'
  import { VirtualWindow } from './virtualWindow.svelte'
  import { toast, confirmDialog } from './toast.svelte'
  import ModMigrationDialog from './ModMigrationDialog.svelte'
  import PackCompareDialog from './PackCompareDialog.svelte'
  import ModListExportDialog from './ModListExportDialog.svelte'
  import ModSourcesDialog from './ModSourcesDialog.svelte'
  import ModDependencyDialog from './ModDependencyDialog.svelte'
  import SelectField from './SelectField.svelte'
  import modrinthIcon from '../assets/images/Modrinth_icon.png'
  import curseforgeIcon from '../assets/images/curseforge_icon.png'
  import type { ModEntry, ModVersionOption, ModBackupEntry, SuggestedMod, TempModInfo } from './types'
  import type { PackPickerItem, PackRow, PackFolderRow } from './PackPicker.svelte'

  let {
    packId,
    packName = '',
    mcVersion = '',
    loader = '',
    // Іконка/колір поточної збірки — лише для фіксованої лівої картки в
    // PackCompareDialog (сам менеджер модів їх ніде більше не використовує).
    iconUrl = '',
    icon = '',
    color = '',
    onSearchMods = null as null | (() => void),
    onOpenModPage = null as null | ((source: string, projectId: string) => void),
    // Сигнал «розгорни віджет дублікатів» (з передзапускової модалки
    // попередження): при кожному інкременті віджет розгортається і
    // підсвічується — користувач одразу бачить копії і може прибрати
    // зайві, не шукаючи їх серед списку.
    dupFocusSignal = 0,
    // Збірка зараз запущена (на будь-якому акаунті) — змінювати моди
    // заборонено (Minecraft тримає jar-файли відкритими). Фронтенд
    // блокує мутації модалкою ДО виклику бекенду.
    running = false,
    // Наскільки нижче від верху контейнера скролу прилипати тулбару
    // (висота закріпленого тулбара сторінки збірки, який лишається на
    // місці при скролі). PackInstancePage міряє його і передає сюди —
    // інакше тулбар модів лип би ПІД/НА тулбар сторінки.
    stickyTop = 0,
    // Спільний список збірок з папками (App.svelte → ... → сюди) — для
    // пікера правої збірки в PackCompareDialog. null, поки не підключено
    // (кнопка «Порівняти» тоді просто не з'явиться — нема з чим порівнювати).
    packPicker = null as null | {
      items: PackPickerItem[]
      rowById: Map<string, PackRow>
      folderById: Map<string, PackFolderRow>
    }
  }: {
    packId: string
    packName?: string
    mcVersion?: string
    loader?: string
    iconUrl?: string
    icon?: string
    color?: string
    // Кнопка «Шукати моди» → відкриває Браузер контенту з контекстом
    // цієї збірки (версія/лоадер закріплені, встановлення йде одразу сюди).
    onSearchMods?: null | (() => void)
    // Клік по іконці/назві мода → відкриває сторінку цього мода в
    // Браузері контенту (лише якщо відомі source+projectId, тобто мод
    // встановлений з Modrinth/CurseForge, а не імпортований вручну).
    onOpenModPage?: null | ((source: string, projectId: string) => void)
    // Сигнал фокусу віджета дублікатів (App.svelte → PackInstancePage).
    dupFocusSignal?: number
    // Збірка запущена (на будь-якому акаунті) — змінювати моди заборонено.
    running?: boolean
    // Висота закріпленого тулбара сторінки збірки (для sticky-відступу).
    stickyTop?: number
    packPicker?: null | {
      items: PackPickerItem[]
      rowById: Map<string, PackRow>
      folderById: Map<string, PackFolderRow>
    }
  } = $props()

  // ── Захист від змін під час запущеної гри ──
  // Аналогічно PackContentSection: поки збірка запущена, увімкнення/
  // вимкнення/видалення/оновлення/зміна версії модів блокуються модалкою
  // ДО виклику бекенду (який теж перевіряє errPackRunning — подвійний
  // захист від "used by another process").
  let runningLocked = $state(false)
  function guardNotRunning(): boolean {
    if (!running) return true
    runningLocked = true
    return false
  }

  // ── Стан списку ──
  let mods = $state.raw<ModEntry[]>([])
  // tempMods — тимчасові мітки поточної збірки (fileName → інформація
  // про дату/дію), окремо від mods: TempModInfo приходить з іншого
  // виклику (GetTempMods, читає Pack.TempMods), не зі сканування диска.
  let tempMods = $state<Record<string, TempModInfo>>({})
  // Дефолтна дія для НОВОЇ мітки (немає ще власного вибору) — те саме
  // значення, що Налаштування → Попередження → «Видаляти тимчасові моди
  // за замовчуванням». Читається один раз при монтуванні сторінки; якщо
  // користувач змінить налаштування, поки ця сторінка вже відкрита, нове
  // значення підхопиться при наступному відкритті модалки (не критично —
  // це лише дефолт чекбокса, який завжди можна змінити вручну в діалозі).
  let settingsTempModsDeleteByDefault = $state(false)
  async function loadTempMods() {
    try {
      const list = (await App.GetTempMods(packId)) ?? []
      const map: Record<string, TempModInfo> = {}
      for (const t of list) map[t.fileName] = t
      tempMods = map
    } catch (e) { console.error(e) }
  }
  async function loadTempModsDefaultAction() {
    try {
      const s = await App.GetSettings()
      settingsTempModsDeleteByDefault = !!s?.tempModsDeleteByDefault
    } catch (e) { console.error(e) }
  }

  let scanning = $state(true)
  let err = $state('')
  let search = $state('')
  let filter = $state<'all' | 'updates' | 'enabled' | 'disabled'>('all')
  let sort = $state<'name' | 'date' | 'update'>('date')

  // Перевірка оновлень (фонова, з прогресом mods:update-progress).
  let updateChecking = $state(false)
  let checkDone = $state(0)
  let checkTotal = $state(0)

  // ── Модалки ──
  let versionMod = $state<ModEntry | null>(null)
  let versionOpts = $state<ModVersionOption[]>([])
  let versionLoading = $state(false)
  let installingVersion = $state(false)
  let selectedVersionKey = $state('')


  let updateAllOpen = $state(false)
  let updateAllBackup = $state(true)
  let updateAllRunning = $state(false)
  let updateAllProgress = $state('')

  let backupsOpen = $state(false)
  let backups = $state<ModBackupEntry[]>([])
  let restoringId = $state('')

  // ── Оновити ОДИН мод (кнопка на картці) — так само як «Оновити всі»,
  // пропонуємо створити резервну копію перед заміною файлу. ──
  let updateOneMod = $state<ModEntry | null>(null)
  let updateOneBackup = $state(true)
  let updateOneRunning = $state(false)

  let deleting = $state('')

  // ── Масові дії (вибір кількох модів чекбоксами, як на скріні) ──
  let selected = $state<Set<string>>(new Set())
  const selectMode = $derived(selected.size > 0)
  let bulkWorking = $state(false)

  function toggleSelect(fileName: string, e?: Event) {
    e?.stopPropagation()
    const next = new Set(selected)
    if (next.has(fileName)) next.delete(fileName)
    else next.add(fileName)
    selected = next
  }
  function clearSelection() { selected = new Set() }
  function selectAllVisible() { selected = new Set(visibleMods.map(m => m.fileName)) }

  // Іконки обраних модів для панелі масових дій — до 3 штук "купкою"
  // (як на скріні: перекриваються, найновіший вибір зверху стека),
  // плюс лічильник "+N", якщо обрано більше трьох.
  const selectedModsPreview = $derived.by(() => {
    if (selected.size === 0) return []
    const list: ModEntry[] = []
    for (const f of selected) {
      const m = mods.find(x => x.fileName === f)
      if (m) list.push(m)
      if (list.length >= 3) break
    }
    return list
  })

  async function bulkEnable(enable: boolean) {
    if (bulkWorking || selected.size === 0) return
    // Увімкнення/вимкнення модів дозволено і під час гри (перейменування
    // у .jar.disabled, запущену сесію не ламає — див. toggleMod).
    bulkWorking = true
    try {
      const files = [...selected]
      for (const f of files) {
        const m = mods.find(x => x.fileName === f)
        // Моди з «єдиного джерела» вимкнути/увімкнути вручну не можна.
        if (m && !m.sourceKind && m.enabled !== enable) await App.ToggleMod(packId, f, enable)
      }
      clearSelection()
      await loadMods()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      bulkWorking = false
    }
  }

  async function bulkDelete() {
    if (bulkWorking || selected.size === 0) return
    if (!guardNotRunning()) return
    const names = [...selected].filter(f => !mods.find(x => x.fileName === f)?.sourceKind)
    if (names.length === 0) { toast(t('modsrc.lockedHint'), 'info'); return }
    const ok = await confirmDialog(t('mods.bulkDeleteConfirm', { n: names.length }), names.join(', '), {
      confirmLabel: t('mods.delete'), cancelLabel: t('cancel'), danger: true
    })
    if (!ok) return
    bulkWorking = true
    try {
      for (const f of names) await App.DeleteMod(packId, f)
      toast(t('mods.deleted'), 'success')
      clearSelection()
      await loadMods()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      bulkWorking = false
    }
  }

  // ── 3-крапки меню на картці мода ── (тулбар більше не має свого
  // overflow-меню — усі дії розгорнуті прямими кнопками в рядок; лишається
  // лише меню на рядку окремого мода: «показати в теці» / «копіювати
  // посилання» / тимчасовий мод.)
  let menuFor = $state('')
  function toggleRowMenu(fileName: string, e?: Event) {
    e?.stopPropagation()
    menuFor = menuFor === fileName ? '' : fileName
  }
  function closeRowMenu() { menuFor = '' }

  // Клік по іконці/назві мода → сторінка мода в Браузері контенту.
  // Доступно лише для модів, встановлених з Modrinth/CurseForge (є
  // source+projectId); локальні jar без метаданих клікабельними не робимо.
  function openModPage(m: ModEntry) {
    if (!onOpenModPage) return
    if (!m.projectId || !m.source || m.source === 'unknown') return
    onOpenModPage(m.source, m.projectId)
  }
  function isModPageClickable(m: ModEntry): boolean {
    return !!onOpenModPage && !!m.projectId && !!m.source && m.source !== 'unknown'
  }

  // ── Імпорт jar-файлів (діалог вибору + drag&drop у список) ──
  let importing = $state(false)
  let dragOver = $state(false)

  async function pickAndImportFiles() {
    if (importing) return
    if (!guardNotRunning()) return
    try {
      const paths = await App.PickModFiles()
      if (!paths || paths.length === 0) return
      await importFiles(paths)
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  async function importFiles(paths: string[]) {
    if (!guardNotRunning()) return
    importing = true
    try {
      const res = await App.ImportModFiles(packId, paths)
      // res.* можуть прийти null (Go nil-слайс у JSON) — страхуємось
      // порожнім масивом, щоб жоден `.length` не кидав TypeError.
      const imported = res?.imported ?? []
      const skipped = res?.skipped ?? []
      const failed = res?.failed ?? []
      if (imported.length > 0) toast(t('mods.importedCount', { n: imported.length }), 'success')
      if (skipped.length > 0) toast(t('mods.importSkipped', { n: skipped.length }), 'info')
      if (failed.length > 0) toast(t('mods.importFailed', { n: failed.length }), 'error')
      await loadMods()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      importing = false
    }
  }

  function onModsDrop(e: DragEvent) {
    e.preventDefault()
    dragOver = false
    const files = [...(e.dataTransfer?.files ?? [])] as any[]
    const paths = files.map(f => f.path).filter((p): p is string => !!p && p.toLowerCase().endsWith('.jar'))
    if (paths.length > 0) importFiles(paths)
  }

  // ── Похідні ──
  const updatesCount = $derived(mods.filter(m => m.hasUpdate).length)
  const disabledCount = $derived(mods.filter(m => !m.enabled).length)
  const incompatibleCount = $derived(mods.filter(m => m.incompatible).length)

  // ── Дублікати: групуємо В ОДНОМУ ВІДЖЕТІ (а не розкидано по 1000
  // рядках списку з бейджем). Група = моди з однаковим modId (id).
  // Кожен такий блок згортається/розгортається і показує всі файли групи,
  // щоб юзер бачив «що зайве» і видаляв одним кліком.
  let dupExpanded = $state(true)
  const dupGroups = $derived.by(() => {
    const byId = new Map<string, ModEntry[]>()
    for (const m of mods) {
      if (!m.id) continue
      const arr = byId.get(m.id)
      if (arr) arr.push(m)
      else byId.set(m.id, [m])
    }
    return [...byId.values()]
      .filter(g => g.length > 1)
      .sort((a, b) => a[0].name.localeCompare(b[0].name))
  })
  const dupGroupCount = $derived(dupGroups.reduce((n, g) => n + g.length, 0))
  const dupGroupExtra = $derived(dupGroups.reduce((n, g) => n + g.length - 1, 0))

  // ── Пошук у віджеті дублікатів: фільтруємо і групи, і файли всередині
  // груп за назвою мода / іменем файлу / версією. Запит у пошуку
  // автоматично розгортає віджет, щоб було видно знайдені копії.
  let dupSearch = $state('')
  function dupFileMatches(f: ModEntry, q: string): boolean {
    if (!q) return true
    return f.name.toLowerCase().includes(q) ||
      f.fileName.toLowerCase().includes(q) ||
      (f.version || '').toLowerCase().includes(q) ||
      (f.author || '').toLowerCase().includes(q)
  }
  const filteredDupGroups = $derived.by(() => {
    const q = dupSearch.trim().toLowerCase()
    if (!q) return dupGroups
    return dupGroups.filter(g => g.some(m => dupFileMatches(m, q)))
  })
  const filteredDupCount = $derived(filteredDupGroups.reduce((n, g) => n + g.length, 0))
  // Як тільки юзер почав шукати — розгортаємо віджет (інакше знахідки
  // сховані під згорнутими групами).
  $effect(() => {
    if (dupSearch.trim()) dupExpanded = true
  })

  // Сигнал «розгорни віджет дублікатів» (передзапускове попередження):
  // розгортаємо віджет і підкручуємо до нього скрол — користувач одразу
  // бачить групи копій і може видалити зайві одним кліком (або обрати
  // «яку лишити»), а тоді повернутись і запустити гру знову.
  $effect(() => {
    if (dupFocusSignal <= 0) return
    dupExpanded = true
    dupSearch = ''
    // Скрол до віджета трохи пізніше — після того, як вкладка встигла
    // змонтуватись/оновитись (Svelte 5 ефекти, асинхронний скан модів).
    setTimeout(() => {
      const el = document.querySelector('.dup-widget') as HTMLElement | null
      el?.scrollIntoView({ behavior: 'smooth', block: 'start' })
      el?.animate(
        [
          { boxShadow: '0 0 0 0 rgba(239,68,68,0)' },
          { boxShadow: '0 0 0 6px rgba(239,68,68,.28)' },
          { boxShadow: '0 0 0 0 rgba(239,68,68,0)' }
        ],
        { duration: 900, easing: 'ease-out' }
      )
    }, 350)
  })

  // ── Вибір «яку копію лишити» (дублікати) ──
  // Користувач САМ відмічає, який файл групи зберегти (радіо-кнопка на
  // рядку), замість автозбереження найновішої. dupKeep: groupId → fileName.
  let dupKeep = $state<Record<string, string>>({})
  function setDupKeep(groupId: string, fileName: string) {
    dupKeep = { ...dupKeep, [groupId]: fileName }
  }
  function keepNameFor(g: ModEntry[]): string {
    const chosen = dupKeep[g[0].id]
    if (chosen && g.some(m => m.fileName === chosen)) return chosen
    return [...g].sort((a, b) => (b.installedDate || '').localeCompare(a.installedDate || ''))[0].fileName
  }

  // Видалити ОДИН файл з групи дублікатів (кнопка на рядку копії).
  async function deleteDupFile(group: ModEntry[], f: ModEntry) {
    const ok = await confirmDialog(
      t('mods.deleteConfirm', { name: f.name }),
      f.fileName,
      { confirmLabel: t('mods.delete'), cancelLabel: t('cancel'), danger: true }
    )
    if (!ok) return
    bulkWorking = true
    try {
      await App.DeleteMod(packId, f.fileName)
      toast(t('mods.deleted'), 'success')
      await loadMods()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      bulkWorking = false
    }
  }

  // Видалити всі файли групи, окрім обраного (keepNameFor) — лишаємо
  // один «правильний» екземпляр і прибираємо копії одним кліком.
  async function deleteDupGroup(group: ModEntry[]) {
    if (group.length < 2) return
    const keep = keepNameFor(group)
    const toDelete = group.filter(m => m.fileName !== keep)
    const ok = await confirmDialog(
      t('mods.dupGroupDeleteConfirm', { n: toDelete.length, name: keep }),
      toDelete.map(m => m.fileName).join('\n'),
      { confirmLabel: t('mods.delete'), cancelLabel: t('cancel'), danger: true }
    )
    if (!ok) return
    // Те саме, що bulkDelete: видаляємо файли послідовно через App.DeleteMod.
    bulkWorking = true
    try {
      for (const f of toDelete) await App.DeleteMod(packId, f.fileName)
      toast(t('mods.deleted'), 'success')
      await loadMods()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      bulkWorking = false
    }
  }

  // ── Стабільний порядок при оновленні ──────────────────────────────────
  // Сортування «за датою» йде за installedDate, а оновлений мод — це НОВИЙ
  // файл із новою датою, тож після оновлення він стрибав на початок списку.
  // Тепер дату мода (за стійким ключем projectId/назва, а не за іменем файла)
  // запам'ятовуємо при першій появі й далі використовуємо її: оновлення й
  // перейменування (.jar ↔ .jar.disabled) позицію не міняють, а справді
  // новий мод (ключа ще не було) лишається зверху. Звичайний Map, не $state —
  // читання в $derived не створює реактивних циклів.
  const dateMemo = new Map<string, string>()
  function modKey(m: ModEntry): string {
    return ((m.projectId || m.name || m.fileName) + '').toLowerCase()
  }
  function stableDate(m: ModEntry): string {
    const k = modKey(m)
    let d = dateMemo.get(k)
    if (d === undefined) { d = m.installedDate || ''; dateMemo.set(k, d) }
    return d
  }

  // ── Тихе оновлення списку ─────────────────────────────────────────────
  // Замість `mods = list` (усі рядки нові об'єкти → перемальовується весь
  // список) повторно використовуємо старі об'єкти для модів, що не змінились,
  // а якщо не змінилось нічого — не присвоюємо взагалі. Список не блимає,
  // скрол і виділення лишаються на місці.
  function sameMod(a: ModEntry, b: ModEntry): boolean {
    const ka = Object.keys(a) as (keyof ModEntry)[]
    if (ka.length !== Object.keys(b).length) return false
    for (const k of ka) {
      const x = a[k], y = b[k]
      if (x !== y && JSON.stringify(x) !== JSON.stringify(y)) return false
    }
    return true
  }
  function setModsQuiet(next: ModEntry[]) {
    const prev = new Map(mods.map(m => [m.fileName, m]))
    let changed = next.length !== mods.length
    const merged = next.map((m, i) => {
      const p = prev.get(m.fileName)
      if (p && sameMod(p, m)) { if (mods[i] !== p) changed = true; return p }
      changed = true
      return m
    })
    if (changed) mods = merged
  }

  // Анімація появи рядків — лише при першому показі списку; після оновлення
  // мода (новий файл = новий рядок) вона б виглядала як блимання.
  let animateRows = $state(true)
  let animateTimer: ReturnType<typeof setTimeout> | undefined
  $effect(() => {
    if (mods.length > 0 && animateRows && !animateTimer) {
      animateTimer = setTimeout(() => { animateRows = false }, 700)
    }
  })

  const visibleMods = $derived.by(() => {
    let list = mods
    if (search.trim()) {
      const q = search.trim().toLowerCase()
      list = list.filter(m =>
        m.name.toLowerCase().includes(q) ||
        (m.author || '').toLowerCase().includes(q) ||
        m.fileName.toLowerCase().includes(q)
      )
    }
    switch (filter) {
      case 'updates': list = list.filter(m => m.hasUpdate); break
      case 'enabled': list = list.filter(m => m.enabled); break
      case 'disabled': list = list.filter(m => !m.enabled); break
    }
    const arr = [...list]
    if (sort === 'name') arr.sort((a, b) => a.name.localeCompare(b.name))
    else if (sort === 'update') arr.sort((a, b) => Number(b.hasUpdate) - Number(a.hasUpdate) || a.name.localeCompare(b.name))
    else arr.sort((a, b) => stableDate(b).localeCompare(stableDate(a)))
    // Несумісні з лоадером збірки моди — завжди зверху, незалежно від
    // обраного сортування: це проблема, яку треба помітити першою.
    arr.sort((a, b) => Number(!!b.incompatible) - Number(!!a.incompatible))
    return arr
  })

  // ── Віртуалізація списку ──
  // Довгий список (60+ модів) рендерить у DOM лише видиме вікно + буфер,
  // решту заміщають відступи контейнера. Менше вузлів DOM, менше <img>,
  // менше роботи Svelte — на 1000 модів у DOM ~30 рядків замість 1000.
  const vw = new VirtualWindow({ min: 60, buffer: 6, gap: 8, rowSelector: '.mod-row', estimate: 70 })
  let listEl = $state<HTMLElement | undefined>()
  $effect(() => {
    const el = listEl
    if (!el) return
    vw.attach(el)
    return () => vw.detach()
  })
  $effect(() => { vw.setTotal(visibleMods.length) })
  const virtual = $derived(vw.active)
  const renderedMods = $derived(virtual ? visibleMods.slice(vw.start, vw.end) : visibleMods)

  // ── Завантаження ──
  // При звичайному відкритті сторінки — миттєвий скан з диска (іконки,
  // назви, версії) одразу підмішаний з ВЖЕ ВІДОМИМ (закешованим) статусом
  // оновлень, БЕЗ походу в мережу — сторінка не «висне» і не блимає
  // список щоразу. Мережевий похід (Modrinth/CurseForge) стається лише:
  //   1) один раз, коли для цієї (версія гри, лоадер) кешу ще не було —
  //      тобто перше відкриття сторінки модів для цієї збірки;
  //   2) коли з'явився НОВИЙ файл мода, якого не було в попередньому скані
  //      (реальний час: onMount-поллінг директорії нижче);
  //   3) коли користувач явно тисне «Перевірити оновлення».
  let haveCheckedOnce = $state(false)

  // ── Рекомендовані моди (hard-залежності встановлених модів, яких
  // самих ще немає в збірці) ────────────────────────────────────────────
  let suggestedMods = $state<SuggestedMod[]>([])
  let suggestedLoading = $state(false)
  let installingSuggested = $state<string | null>(null) // depId, що зараз ставиться
  let dismissedSuggestIds = $state<Set<string>>(new Set())

  async function loadSuggestedMods(scanId = packId) {
    if (!mcVersion || !loader) { suggestedMods = []; return }
    suggestedLoading = true
    try {
      const list = (await App.GetSuggestedMods(scanId, mcVersion, loader)) || []
      if (scanId === packId) suggestedMods = list
    } catch {
      // Мовчазний фейл — блок рекомендацій не критичний, не заважаємо
      // основному списку модів повідомленням про помилку.
      if (scanId === packId) suggestedMods = []
    } finally {
      if (scanId === packId) suggestedLoading = false
    }
  }

  async function installSuggested(s: SuggestedMod) {
    if (installingSuggested) return
    if (!guardNotRunning()) return
    installingSuggested = s.depId
    try {
      const res = await App.InstallSuggestedMod(packId, s.depId, mcVersion, loader)
      if (res?.success) {
        toast(t('mods.suggested.installed', { name: s.name || s.depId }), 'success')
        suggestedMods = suggestedMods.filter(x => x.depId !== s.depId)
        await loadMods()
      } else {
        toast(res?.message || t('error.generic'), 'error')
      }
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      installingSuggested = null
    }
  }

  function dismissSuggested(depId: string) {
    // Лише для поточної сесії вкладки (не зберігається) — швидкий спосіб
    // прибрати з очей пропозицію, яку користувач свідомо не хоче
    // ставити, не чіпаючи глобальне налаштування «Вимкнути рекомендації».
    dismissedSuggestIds = new Set([...dismissedSuggestIds, depId])
  }

  const visibleSuggestedMods = $derived(suggestedMods.filter(s => !dismissedSuggestIds.has(s.depId)))

  // scanId — за замовчуванням поточний packId (виклики поза
  // race-guard'ованим $effect, напр. кнопка "Оновити список",
  // просто працюють з актуальним packId як і раніше).
  async function loadMods(scanId = packId): Promise<ModEntry[]> {
    const forThisPack = scanId === packId
    // Індикатор «Сканування» — лише коли списку ще нема; при оновленні вже
    // показаного списку він би блимав у шапці без причини.
    if (forThisPack) { if (mods.length === 0) scanning = true; err = '' }
    try {
      const raw = mcVersion && loader
        ? await App.ScanModsWithCachedUpdates(scanId, mcVersion, loader)
        : await App.ScanModsFull(scanId)
      // Захист: якщо бекенд колись поверне null (порожня тека серіалізується
      // в Go як nil-слайс -> JSON null), тут гарантовано підміняємо на [].
      // Без цього mods.length нижче по коду кидає TypeError, рендер шаблону
      // вибухає, і плашка "Сканування модів..." лишається висіти назавжди.
      const list = raw ?? []
      // packId міг змінитись, поки чекали відповідь бекенду — застарілий
      // результат чужої збірки в список не пишемо (гонка при швидкому
      // перемиканні між збірками або повільному диску/антивірусі).
      if (scanId === currentScanId || scanId === packId) setModsQuiet(list)
      return list
    } catch (e: any) {
      if (scanId === packId) err = String(e?.message ?? e)
      return []
    } finally {
      if (scanId === packId) scanning = false
    }
  }

  // Друга, мережева хвиля перевірки сумісності: питає Modrinth/CurseForge
  // лише для модів, які локальний скан щойно позначив «несумісний» —
  // якщо там знайдеться сумісний лоадер (нетиповий/неповний файл
  // метаданих у самому jar, а насправді мод опубікований і під потрібний
  // лоадер) — знімає хибну позначку прямо в поточному списку, без
  // повторного повного скану сторінки. Некритичний фоновий запит: збій
  // мовчки ігнорується, позначка просто лишається як була.
  async function verifyIncompatible(scanId = packId) {
    if (!mcVersion || !loader) return
    const hasIncompatible = mods.some(m => m.incompatible)
    if (!hasIncompatible) return
    try {
      const changed = await App.VerifyIncompatibleMods(scanId, mcVersion, loader)
      if (scanId !== packId || !changed || changed.length === 0) return
      const byFile = new Map(changed.map(m => [m.fileName, m]))
      guardScroll()
      mods = mods.map(m => byFile.get(m.fileName) ?? m)
    } catch { /* мережевий збій — позначки лишаються як були */ }
  }

  // ── Захист скролу під час/після перевірки оновлень ───────────────────
  // Оновлення списку (патчі рядків, завершення перевірки, сумісність) не
  // повинні відкидати сторінку на початок. Запам'ятовуємо останню позицію
  // скролу користувача; якщо протягом перевірки (і ~1.5 с після) контейнер
  // раптом стрибнув у 0, повертаємо збережену позицію.
  let lastScrollTop = 0
  let scrollGuardUntil = 0
  function guardScroll(ms = 1500) { scrollGuardUntil = Date.now() + ms }
  $effect(() => {
    const el = document.querySelector('.content') as HTMLElement | null
    if (!el) return
    const onScr = () => {
      const guarded = updateChecking || Date.now() < scrollGuardUntil
      if (guarded && el.scrollTop < 4 && lastScrollTop > 40) {
        const want = lastScrollTop
        requestAnimationFrame(() => { if (el.scrollTop < 4) el.scrollTop = want })
        return
      }
      lastScrollTop = el.scrollTop
    }
    el.addEventListener('scroll', onScr, { passive: true })
    return () => el.removeEventListener('scroll', onScr)
  })
  $effect(() => {
    // Перехід updateChecking true→false також захищаємо.
    if (!updateChecking) untrack(() => guardScroll())
  })

  async function checkUpdates(force = false, scanId = packId) {
    if (!mcVersion || !loader || updateChecking) return
    // Без force — це ПЕРШИЙ автоматичний прохід для цієї версії/лоадера;
    // якщо кеш уже відповів (мод має Status === 'done' після loadMods),
    // повторно лізти в мережу не треба.
    if (!force && haveCheckedOnce) return
    // Захист: якщо список модів порожній, не запускаємо мережеву перевірку
    // (нема що перевіряти) — одразу встановлюємо haveCheckedOnce=true.
    if (mods.length === 0) {
      haveCheckedOnce = true
      return
    }
    updateChecking = true
    checkDone = 0
    checkTotal = 0
    try {
      const list = await App.RefreshModUpdates(scanId, mcVersion, loader)
      // Та сама гонка, що й у loadMods: якщо збірку вже перемкнули,
      // результат застарілої мережевої перевірки в UI не підмішуємо.
      if (scanId === currentScanId || scanId === packId) {
        guardScroll()
        setModsQuiet(list)
        haveCheckedOnce = true
      }
    } catch (e: any) {
      if (scanId === packId) toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      if (scanId === packId) updateChecking = false
    }
  }

  // race-guard: packId, для якого точно виконується поточний
  // цикл сканування/перевірки оновлень. Без цього застарілий
  // Promise від ПОПЕРЕДНЬОЇ збірки (напр. довге мережеве
  // сканування, яке ще виконується, поки юзер уже переключився
  // на іншу збірку) міг дорезолвитись пізніше і переписати mods/
  // scanning чужими даними — тоді список "застрягав" у стані
  // сканування або показував моди не тієї збірки.
  let currentScanId = ''

  // ВАЖЛИВО (виправлення «лаунчер зависає при завантаженні модів»): цей
  // $effect має залежати ЛИШЕ від packId/mcVersion/loader. Раніше його тіло
  // читало реактивний стан (mods.length у loadMods, mods.some у
  // verifyIncompatible тощо) і одночасно писало `mods = []`, тож ефект
  // підписувався на `mods`: щойно скан завершувався й `mods` змінювався,
  // ефект запускався знову, скидав список і стартував повний скан +
  // перевірку оновлень + сумісність + рекомендації — безкінечний цикл
  // сканів, що й виглядав як зависання. Тепер залежності зчитуємо явно, а
  // решту виконуємо в untrack.
  $effect(() => {
    const id = packId
    void mcVersion
    void loader
    untrack(() => startScanCycle(id))
  })

  function startScanCycle(id: string) {
    currentScanId = id
    mods = []
    dateMemo.clear()
    animateRows = true
    clearTimeout(animateTimer); animateTimer = undefined
    haveCheckedOnce = false
    dismissedSuggestIds = new Set()
    tempMods = {}
    loadTempMods()
    loadMods(id).then(list => {
      if (currentScanId !== id) return // packId вже змінився — ігноруємо застарілий результат
      // Якщо кеш уже дав статус оновлень для всіх модів (Status === 'done'
      // після mergeCachedUpdateInfo) — не лізем у мережу вдруге. Інакше
      // (перше відкриття для цієї версії/лоадера збірки: свіжі моди ще БЕЗ
      // кешу, Status === undefined) — один автоматичний мережевий прохід.
      // ВАЖЛИВО: undefined НЕ рахується як "відомий статус" — інакше на
      // першому відкритті перевірка оновлень ніколи б не запускалась,
      // hasUpdate лишався б false, і кнопки оновлення взагалі не з'являлись.
      const allKnown = list.length === 0 || list.every(m => m.status === 'done')
      if (mcVersion && loader && !allKnown) checkUpdates(true, id)
      // Онлайн-перевірка несумісних модів — окремий фоновий запит,
      // не блокує ні відкриття сторінки, ні перевірку оновлень вище.
      verifyIncompatible(id)
    })
    // Рекомендації — окремий, некритичний запит, не блокує основний
    // список модів (свій try/catch, свій loading-стан).
    loadSuggestedMods(id)
  }

  onMount(() => {
    loadTempModsDefaultAction()
    // Застосувати ОДНУ подію прогресу (той самий payload, що раніше йшов
    // окремим mods:update-progress) — винесено в окрему функцію, бо тепер
    // застосовується в циклі по пачці (applyProgress нижче).
    function applyProgress(p: any) {
      if (p?.packId !== packId) return
      checkDone = p.done ?? checkDone
      checkTotal = p.total ?? checkTotal
      if (p.phase === 'update') {
        updateAllProgress = `${p.current ?? 0}/${p.total ?? 0}`
      }
    }
    // mods:update-progress:batch — той самий канал, що раніше окремими
    // mods:update-progress, але пачкою (бекенд: mods_batch.go). На збірках
    // з великою кількістю модів окремі події (сотні підряд у синхронному
    // циклі CheckModUpdates) призводили до "зависання" інтерфейсу — щоб
    // не рендерити список сотні разів поспіль, застосовуємо всю пачку і
    // читаємо лише ОСТАННІЙ елемент кожного типу поля (checkDone/checkTotal
    // — проміжні значення для решти елементів пачки нікому не потрібні,
    // важливий лише фінальний стан після пачки).
    const off = on('mods:update-progress:batch', (batch: any[]) => {
      if (!Array.isArray(batch)) return
      for (const p of batch) applyProgress(p)
    })
    // Живе оновлення ОДНОГО мода під час перевірки оновлень: бекенд шле
    // mods:update-one:batch пачкою (mods_batch.go) — патчимо КОЖЕН рядок
    // із пачки за один прохід по локальному масиву й ОДНЕ присвоєння
    // mods = mods наприкінці, щоб Svelte перерендерив список ОДИН раз на
    // всю пачку, а не окремо на кожен мод (раніше — окрема подія й окреме
    // присвоєння mods[idx]=m на кожен файл, що на збірках із сотнями
    // модів давало сотні реактивних ре-рендерів списку підряд).
    const offOne = on('mods:update-one:batch', (batch: any[]) => {
      if (!Array.isArray(batch) || batch.length === 0) return
      // `mods` — $state.raw (без глибоких проксі на кожне поле кожного мода:
      // на збірці з сотнями модів це зайві мегабайти і повільні рендери),
      // тому патчимо НЕЗМІННО: копія масиву + копії лише змінених рядків,
      // одне присвоєння на всю пачку. Індекс fileName→позиція будуємо один
      // раз на пачку (раніше findIndex на кожну подію = O(n²)).
      let next: ModEntry[] | null = null
      let index: Map<string, number> | null = null
      for (const p of batch) {
        if (p?.packId !== packId || !p?.fileName || !p?.mod) continue
        const upd = p.mod as Partial<ModEntry>
        if (!index) {
          index = new Map()
          mods.forEach((x, i) => index!.set(x.fileName, i))
        }
        const idx = index.get(p.fileName)
        if (idx === undefined) continue
        if (!next) next = mods.slice()
        const m: ModEntry = { ...next[idx] }
        if (upd.status) m.status = upd.status
        if (upd.hasUpdate !== undefined) m.hasUpdate = upd.hasUpdate
        if (upd.latestVersion !== undefined) m.latestVersion = upd.latestVersion
        if (upd.latestUrl !== undefined) m.latestUrl = upd.latestUrl
        if (upd.source !== undefined) m.source = upd.source
        if (upd.projectId !== undefined) m.projectId = upd.projectId
        if (upd.projectSlug !== undefined) m.projectSlug = upd.projectSlug
        if (upd.iconUrl !== undefined) m.iconUrl = upd.iconUrl
        next[idx] = m
      }
      if (next) { guardScroll(); mods = next }
    })
    // Реальний час: тека модів може змінитись поза лаунчером (юзер кинув
    // .jar вручну) або з'явитись новий файл після встановлення залежності
    // з іншої вкладки. mods:dir-changed шле бекенд-вотчер директорії
    // (fsnotify) — при спрацюванні тихо перескановуємо диск і, якщо
    // з'явився файл, якого не було в поточному списку, перевіряємо саме
    // його на оновлення (без повного мережевого проходу по всіх модах).
    const offDir = on('mods:dir-changed', async (p: any) => {
      if (p?.packId !== packId) return
      const prevFiles = new Set(mods.map(m => m.fileName))
      const list = await loadMods()
      const added = list.filter(m => !prevFiles.has(m.fileName))
      if (added.length > 0 && mcVersion && loader) {
        // Новий(і) мод(и) з'явились — перевіряємо оновлення заново, щоб
        // побачити для них hasUpdate (кеш ще не знає про свіжий файл).
        checkUpdates(true)
      }
    })
    // Прибиральник (backend/maintenance.go → cleanupExpiredTempMods)
    // може прибрати прострочений тимчасовий мод, ПОКИ сторінка модів
    // відкрита (прохід раз/годину) — mods:changed шле той самий event,
    // що й після встановлення/оновлення контенту з браузера. Перескановуємо
    // і список файлів, і мітки, щоб рядок мода зник/змінив стан одразу,
    // а не після ручного переходу на іншу вкладку і назад.
    const offChanged = on('mods:changed', (changedPackId: string) => {
      if (changedPackId !== packId) return
      loadMods()
      loadTempMods()
    })
    return () => { off(); offOne(); offDir(); offChanged() }
  })

  // ── Дії ──
  // Toggle мода = перейменування у .jar.disabled. Minecraft вантажить моди
  // лише при старті, тож увімкнення/вимкнення під час запущеної гри БЕЗПЕЧНЕ
  // (зміниться лише набір модів наступного запуску). Заборонені лише дії,
  // що перезаписують/видаляють jar на диску (delete/update/версія).
  let sourcesOpen = $state(false)
  let graphOpen = $state(false)
  // Усі моди збірки дзеркалять теку-джерело — ручні зміни заборонені.
  const folderLocked = $derived(mods.length > 0 && mods.every(m => m.sourceKind === 'folder'))

  async function toggleMod(m: ModEntry) {
    if (m.sourceKind) { toast(t('modsrc.lockedHint'), 'info'); return }
    try {
      await App.ToggleMod(packId, m.fileName, !m.enabled)
      await loadMods()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  async function openVersion(m: ModEntry) {
    if (!guardNotRunning()) return
    versionMod = m
    versionOpts = []
    selectedVersionKey = ''
    versionLoading = true
    try {
      versionOpts = (await App.GetModVersions(packId, m.fileName, mcVersion, loader)) || []
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
      versionMod = null
    } finally {
      versionLoading = false
    }
  }

  async function installVersion(opt: ModVersionOption) {
    if (!versionMod || installingVersion) return
    if (!guardNotRunning()) return
    installingVersion = true
    try {
      await App.InstallModVersion(packId, versionMod.fileName, opt)
      toast(t('mods.versionInstalled'), 'success')
      versionMod = null
      await loadMods()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      installingVersion = false
    }
  }

  // Клік по кнопці «оновити» на картці мода — НЕ оновлює одразу, а
  // відкриває підтвердження з опцією створити резервну копію поточного
  // файлу (так само, як робить «Оновити всі»), щоб оновлення одного
  // мода можна було безпечно відкотити через ListModBackups.
  function updateOne(m: ModEntry) {
    if (!guardNotRunning()) return
    updateOneMod = m
    updateOneBackup = true
  }

  async function confirmUpdateOne() {
    if (!updateOneMod || updateOneRunning) return
    if (!guardNotRunning()) return
    const m = updateOneMod
    updateOneRunning = true
    try {
      await App.UpdateOneMod(packId, m, updateOneBackup)
      toast(t('mods.updated'), 'success')
      if (updateOneBackup) toast(t('mods.backupCreated'), 'info')
      updateOneMod = null
      await loadMods()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      updateOneRunning = false
    }
  }

  async function deleteMod(m: ModEntry) {
    if (m.sourceKind) { toast(t('modsrc.lockedHint'), 'info'); return }
    if (!guardNotRunning()) return
    const ok = await confirmDialog(t('mods.deleteConfirm', { name: m.name }), m.fileName, {
      confirmLabel: t('mods.delete'), cancelLabel: t('cancel'), danger: true
    })
    if (!ok) return
    deleting = m.fileName
    try {
      await App.DeleteMod(packId, m.fileName)
      toast(t('mods.deleted'), 'success')
      await loadMods()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      deleting = ''
    }
  }

  // ── Тимчасові моди («становлення на строк») ──────────────────────
  // Модалка: дата + дія (видалити/вимкнути) + чекбокс «разом із
  // залежностями». Прострочення обробляється бекендом у фоні
  // (maintenance.go), тут лише зберігається намір.
  let tempModDialog = $state<{ mod: ModEntry; date: string; action: 'delete' | 'disable'; includeDeps: boolean; saving: boolean } | null>(null)

  function openTempModDialog(m: ModEntry) {
    const existing = tempModFor(m.fileName)
    // Дефолт дати: якщо вже є мітка — редагуємо її дату; інакше рівно
    // через тиждень від зараз (розумний дефолт, а не сьогоднішня дата,
    // яку довелось би одразу міняти).
    const defaultDate = existing
      ? new Date(existing.expiresAt)
      : new Date(Date.now() + 7 * 86400 * 1000)
    tempModDialog = {
      mod: m,
      date: defaultDate.toISOString().slice(0, 10),
      action: (existing?.action as 'delete' | 'disable') ?? (settingsTempModsDeleteByDefault ? 'delete' : 'disable'),
      includeDeps: false,
      saving: false,
    }
  }

  async function submitTempModDialog() {
    if (!tempModDialog) return
    const { mod, date, action, includeDeps } = tempModDialog
    if (!date) return
    // Опівночі обраного дня в ЛОКАЛЬНОМУ часі користувача — інтуїтивніше,
    // ніж UTC-опівніч, для дати, введеної через <input type="date">.
    const expiresAt = new Date(date + 'T00:00:00')
    if (isNaN(expiresAt.getTime())) return
    tempModDialog = { ...tempModDialog, saving: true }
    try {
      await App.SetModExpiry(packId, mod.fileName, expiresAt.toISOString(), action, includeDeps)
      tempModDialog = null
      await loadTempMods()
      toast(t('mods.temp.saved'), 'success')
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
      tempModDialog = tempModDialog ? { ...tempModDialog, saving: false } : tempModDialog
    }
  }

  async function removeTempMod(m: ModEntry) {
    try {
      await App.RemoveModExpiry(packId, m.fileName)
      await loadTempMods()
      toast(t('mods.temp.removed'), 'success')
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  async function openModsFolder() {
    try {
      await App.OpenModsFolder(packId)
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  async function revealMod(fileName: string) {
    try {
      await App.RevealModFile(packId, fileName)
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  // Копіювання посилання на сторінку мода (Modrinth/CurseForge) — щоб
  // швидко відкрити мод у браузері. URL резолвиться на бекенді на льоту,
  // якщо slug/projectId ще не відомий з перевірки оновлень.
  let copyingLink = $state('')
  async function copyModLink(m: ModEntry) {
    if (copyingLink === m.fileName) return
    copyingLink = m.fileName
    try {
      const url = await App.GetModPageLink(packId, m.fileName, mcVersion, loader)
      if (!url) {
        toast(t('mods.copyLinkFailed'), 'error')
        return
      }
      await App.CopyToClipboard(url)
      toast(t('mods.copyLinkDone'), 'success')
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      copyingLink = ''
    }
  }

  // Оновити всі
  async function runUpdateAll() {
    if (updateAllRunning) return
    if (!guardNotRunning()) return
    updateAllRunning = true
    updateAllProgress = ''
    try {
      const res = await App.UpdateAllMods(packId, mcVersion, loader, updateAllBackup)
      const updated = res?.updated ?? []
      const failed = res?.failed ?? []
      if (updated.length) toast(t('mods.updatedCount', { n: updated.length }), 'success')
      if (failed.length) toast(t('mods.updateFailed', { n: failed.length }), 'error')
      if (res?.backedUp) toast(t('mods.backupCreated'), 'info')
      updateAllOpen = false
      await loadMods()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
      App.AppendLauncherError('runUpdateAll', String(e), '')
    } finally {
      updateAllRunning = false
    }
  }

  // Резервні копії
  async function openBackups() {
    backupsOpen = true
    try {
      backups = (await App.ListModBackups(packId)) || []
    } catch {
      backups = []
    }
  }

  // ── Міграція модів ──
  let migrateOpen = $state(false)
  // ── Порівняння модів двох збірок ──
  let compareOpen = $state(false)
  let exportOpen = $state(false)
  // Поточне виділення передається в діалог як ДЖЕРЕЛО для режиму «лише
  // вибрані». Обсяг (усі/лише вибрані) вибирається вже в самому діалозі —
  // окремої кнопки «експорт вибраних» у панелі масових дій більше нема.
  let exportSelectedFiles = $state<string[]>([])

  // Копіювання посилання на сторінку мода з запису бек-апу (якщо вона
  // збереглась на момент створення копії — див. SourceURL у бекенді).
  async function copyBackupSource(b: ModBackupEntry) {
    if (!b.sourceUrl) return
    try {
      await App.CopyToClipboard(b.sourceUrl)
      toast(t('mods.copyLinkDone'), 'success')
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  async function restoreBackup(b: ModBackupEntry) {
    if (!guardNotRunning()) return
    const ok = await confirmDialog(t('mods.restoreConfirm', { name: b.name }), t('mods.restoreHint'), {
      confirmLabel: t('mods.restore'), cancelLabel: t('cancel'), danger: true
    })
    if (!ok) return
    restoringId = b.id
    try {
      await App.RestoreModBackup(packId, b)
      toast(t('mods.restored'), 'success')
      await loadMods()
      // Відновлення ВИДАЛЯЄ запис резервної копії з manifest.json (вона
      // вже використана). Перечитуємо список бек-апів у модалці — інакше
      // відновлений рядок лишався б там, хоч файлу копії вже нема.
      backups = (await App.ListModBackups(packId)) || []
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      restoringId = ''
    }
  }

  // Ім'я файлу для показу в UI: без технічного суфікса ".disabled"
  // (див. коментар біля .mod-meta) — стан вимкнення показує окремий
  // бейдж, тож дублювати його довгим суфіксом у назві файлу не треба,
  // а ".jar" лишається видимим навіть при обрізанні довгих імен.
  function displayFileName(fileName: string): string {
    return fileName.endsWith('.disabled') ? fileName.slice(0, -'.disabled'.length) : fileName
  }

  function fmtDate(s?: string): string {
    if (!s) return ''
    const d = new Date(s)
    return isNaN(d.getTime()) ? '' : d.toLocaleDateString()
  }

  // Ключ мода тут — та сама FileName, за якою фронтенд звіряється зі
  // сканованим списком (tempMods.disabled-суфікс, якщо мод уже вимкнено,
  // враховано на бекенді - GetTempMods повертає ключі точно як
  // ModEntry.FileName у поточному стані файлу).
  function tempModFor(fileName: string): TempModInfo | undefined {
    return tempMods[fileName]
  }

  // Людський формат «часу до видалення»: днів достатньо для типового
  // випадку (дата на кшталт «1 вересня»), години/хвилини — коли строк
  // ось-ось настане. Від'ємне значення (прибиральник ще не встиг
  // обробити прострочений запис) показуємо як «незабаром», а не як
  // «-2 години», що виглядало б як помилка.
  function fmtTimeRemaining(seconds: number): string {
    if (seconds <= 0) return t('mods.temp.soon')
    const days = Math.floor(seconds / 86400)
    if (days >= 1) return t('mods.temp.inDays', { days })
    const hours = Math.floor(seconds / 3600)
    if (hours >= 1) return t('mods.temp.inHours', { hours })
    const minutes = Math.max(1, Math.floor(seconds / 60))
    return t('mods.temp.inMinutes', { minutes })
  }

  function versionTypeLabel(vt?: string): string {
    switch (vt) {
      case 'beta': return t('mods.versionBeta')
      case 'alpha': return t('mods.versionAlpha')
      default: return t('mods.versionRelease')
    }
  }
</script>

<div class="mods">
  <!-- ═══ Тулбар ═══ -->
  <div class="mods-toolbar" onclick={closeRowMenu} style={`--mt-top:${stickyTop}px`}>
    <div class="mt-group mt-group-main">
      <div class="mt-search">
        <i class="ti ti-search"></i>
        <input value={search} placeholder={t('mods.search')} oninput={(e: any) => search = e.target.value} />
      </div>
      <SelectField
        value={filter}
        options={[
          { value: 'all', label: t('mods.filterAll') },
          { value: 'updates', label: t('mods.filterUpdates') },
          { value: 'enabled', label: t('mods.filterEnabled') },
          { value: 'disabled', label: t('mods.filterDisabled') },
        ]}
        onchange={(v: string) => { filter = v as 'all' | 'updates' | 'enabled' | 'disabled' }}
        minWidth="130px"
        ariaLabel={t('mods.filter')}
      />
      <SelectField
        value={sort}
        options={[
          { value: 'date', label: t('mods.sortDate') },
          { value: 'name', label: t('mods.sortName') },
          { value: 'update', label: t('mods.sortUpdate') },
        ]}
        onchange={(v: string) => { sort = v as 'name' | 'date' | 'update' }}
        minWidth="130px"
        ariaLabel={t('mods.sort')}
      />
    </div>
    <div class="mt-group mt-group-actions">
      <!-- Усі дії тулбара — прямими кнопками в один рядок, без «⋯»-меню
           (раніше рідкісні дії ховались у overflow-меню — незручно, коли
           потрібно швидко дістатись резервних копій/міграції/порівняння/
           експорту). Перевірку оновлень з тулбара прибрано: вона й так
           запускається автоматично при відкритті збірки (див. $effect
           вище) і за кнопкою «Оновити все» — окремий ручний пункт був
           зайвим. -->
      <button class="btn btn-ghost btn-sm icon-tb" onclick={() => loadMods()} title={t('mods.refresh')}>
        <i class="ti ti-refresh"></i>
      </button>
      <button class="btn btn-ghost btn-sm icon-tb" onclick={openModsFolder} title={t('mods.openFolder')}>
        <i class="ti ti-folder"></i>
      </button>
      {#if onSearchMods}
        <button class="btn btn-ghost btn-sm" onclick={onSearchMods} title={t('mods.searchOnline')}>
          <i class="ti ti-world-search"></i> {t('mods.searchOnline')}
        </button>
      {/if}
      <button class="btn btn-ghost btn-sm" onclick={pickAndImportFiles} disabled={importing} title={t('mods.addMod')}>
        {#if importing}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-file-plus"></i>{/if} {t('mods.addMod')}
      </button>
      <button class="btn btn-ghost btn-sm" onclick={() => sourcesOpen = true} title={t('modsrc.title')}>
        <i class="ti ti-link"></i> {t('modsrc.button')}
      </button>
      <button class="btn btn-ghost btn-sm" onclick={() => graphOpen = true} title={t('modgraph.title')}>
        <i class="ti ti-hierarchy-3"></i> {t('modgraph.button')}
      </button>
      <button class="btn btn-ghost btn-sm" onclick={openBackups} title={t('mods.backups')}>
        <i class="ti ti-archive"></i> {t('mods.backups')}
      </button>
      <button class="btn btn-ghost btn-sm" onclick={() => migrateOpen = true} title={t('mods.migrate')}>
        <i class="ti ti-arrows-right-left"></i> {t('mods.migrate')}
      </button>
      {#if packPicker && packPicker.items.length > 0}
        <button class="btn btn-ghost btn-sm" onclick={() => compareOpen = true} title={t('mods.compare')}>
          <i class="ti ti-git-compare"></i> {t('mods.compare')}
        </button>
      {/if}
      <button class="btn btn-ghost btn-sm" onclick={() => { exportSelectedFiles = [...selected]; exportOpen = true }} title={t('mods.export.title')}>
        <i class="ti ti-list-details"></i> {t('mods.export.title')}
      </button>
      <button class="btn btn-primary btn-sm" onclick={() => updateAllOpen = true}
              disabled={updatesCount === 0 || updateChecking || updateAllRunning}
              title={t('mods.updateAll')}>
        <i class="ti ti-refresh"></i> {t('mods.updateAll')}
        {#if updatesCount > 0}<span class="upd-badge">{updatesCount}</span>{/if}
      </button>
    </div>
  </div>

  <!-- ═══ Статус-рядок ═══ -->
  <div class="mods-status">
    {#if scanning}
      <span class="ms-item"><i class="ti ti-loader spin"></i> {t('mods.scanning')}</span>
    {:else}
      <span class="ms-item">{t('mods.count', { n: mods.length })}</span>
      {#if updatesCount > 0}<span class="ms-item acc"><i class="ti ti-arrow-up"></i> {t('mods.withUpdates', { n: updatesCount })}</span>{/if}
      {#if disabledCount > 0}<span class="ms-item dim">{t('mods.disabledCount', { n: disabledCount })}</span>{/if}
      {#if incompatibleCount > 0}<span class="ms-item err"><i class="ti ti-alert-triangle"></i> {t('mods.incompatibleCount', { n: incompatibleCount })}</span>{/if}
    {/if}
    {#if updateChecking}
      <span class="ms-item acc"><i class="ti ti-loader spin"></i> {t('mods.checking', { done: checkDone, total: checkTotal })}</span>
    {/if}
    {#if err}<span class="ms-item err">{err}</span>{/if}
  </div>

  <!-- ═══ Список модів ═══ -->
  <div class="mods-dropzone" class:drag={dragOver}
       ondragover={(e: DragEvent) => { e.preventDefault(); dragOver = true }}
       ondragleave={() => dragOver = false}
       ondrop={onModsDrop}
       onclick={closeRowMenu}>
    {#if scanning && mods.length === 0}
      <div class="mods-empty"><i class="ti ti-loader spin"></i> {t('mods.scanning')}</div>
    {:else if mods.length === 0}
      <!-- Тека модів фізично порожня — картка у стилі лаунчера (перероблена
           з нуля): градієнтна плитка, заголовок, підказка, кнопки дій
           (додати jar / відкрити теку / шукати онлайн) і натяк на drag&drop.
           Не спирається на .mods-empty (щоб не тягнути його гігантську
           іконку 64px і великі відступи, через які старий стан «виглядав
           криво»). -->
      <div class="mods-empty-folder">
        <div class="mef-ic"><i class="ti ti-box"></i></div>
        <div class="mef-title">{t('mods.emptyFolderTitle')}</div>
        <div class="mef-sub">{t('mods.emptyFolderHint')}</div>
        <div class="mef-actions">
          <button class="btn btn-primary" onclick={pickAndImportFiles} disabled={importing}>
            {#if importing}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-file-plus"></i>{/if} {t('mods.addMod')}
          </button>
          <button class="btn btn-ghost" onclick={openModsFolder}><i class="ti ti-folder"></i> {t('mods.openFolder')}</button>
          {#if onSearchMods}
            <button class="btn btn-ghost" onclick={onSearchMods}><i class="ti ti-world-search"></i> {t('mods.searchOnline')}</button>
          {/if}
        </div>
        <div class="mef-drop"><i class="ti ti-download"></i> {t('mods.emptyDropHint')}</div>
      </div>
    {:else}
      {#if dupGroups.length > 0}
        <!-- Дублікати одним віджетом: групи за modId згортаються/розгортаються.
             Не «шукай серед 1000 рядків» — всі копії видно тут разом. -->
        <div class="dup-widget" class:expanded={dupExpanded}>
          <div class="dup-widget-head" onclick={() => dupExpanded = !dupExpanded} role="button" tabindex="0"
               onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); dupExpanded = !dupExpanded } }}>
            <div class="dup-widget-ic"><i class="ti ti-copy"></i></div>
            <span class="dup-widget-title">{t('mods.dupGroupsTitle', { n: dupGroups.length, m: dupGroupCount })}</span>
            <span class="dup-widget-sub">{t('mods.dupGroupsExtra', { n: dupGroupExtra })}</span>
            <span class="dup-widget-chev"><i class="ti ti-chevron-down"></i></span>
          </div>
          {#if dupExpanded}
            <div class="dup-search">
              <i class="ti ti-search"></i>
              <input value={dupSearch} placeholder={t('mods.dupSearch')}
                     oninput={(e: any) => dupSearch = e.target.value} />
              {#if dupSearch}<button class="dup-search-clear" onclick={() => dupSearch = ''} title={t('mods.dupSearchClear')}><i class="ti ti-x"></i></button>{/if}
            </div>
            {#if filteredDupGroups.length === 0}
              <div class="dup-empty">
                <i class="ti ti-search-off"></i>
                <span>{t('mods.dupSearchEmpty', { q: dupSearch.trim() })}</span>
              </div>
            {:else}
            <div class="dup-groups">
              {#each filteredDupGroups as g (g[0].id)}
                {@const q = dupSearch.trim().toLowerCase()}
                {@const matched = q ? g.filter(m => dupFileMatches(m, q)) : g}
                {@const sorted = [...matched].sort((a, b) => (b.installedDate || '').localeCompare(a.installedDate || ''))}
                {@const keepName = keepNameFor(g)}
                <div class="dup-group">
                  <div class="dup-group-head">
                    <div class="dup-group-ic">
                      {#if g[0].hasIcon}<img use:modIcon={{ packId, fileName: g[0].fileName, rev: g[0].fileSize }} alt="" />
                      {:else if g[0].iconUrl}<img src={g[0].iconUrl} alt="" loading="lazy" />
                      {:else}<i class="ti ti-puzzle"></i>{/if}
                    </div>
                    <div class="dup-group-meta">
                      <div class="dup-group-name" title={g[0].name}>{g[0].name}</div>
                      <div class="dup-group-sub">{t('mods.dupGroupSub', { n: g.length, version: g[0].version || '' })}</div>
                    </div>
                    <span class="dup-group-count">{matched.length}×{#if matched.length < g.length} <i class="ti ti-filter" title={t('mods.dupGroupFiltered', { n: g.length })}></i>{/if}</span>
                    <button class="icon-btn del dup-group-del" onclick={() => deleteDupGroup(g)}
                            disabled={bulkWorking} title={t('mods.dupGroupDelete')}>
                      <i class="ti ti-trash"></i>
                    </button>
                  </div>
                  <div class="dup-group-files">
                    {#each sorted as f, fi (f.fileName)}
                      {@const isKeep = f.fileName === keepName}
                      <div class="dup-file rise-in" class:keep={isKeep} style="animation-delay:{Math.min(fi,6)*25}ms">
                        <!-- Радіо-«лишити»: користувач сам обирає, яку копію
                             зберегти, замість автозбереження найновішої. -->
                        <button class="dup-file-keep" class:on={isKeep}
                                onclick={() => setDupKeep(g[0].id, f.fileName)}
                                title={t('mods.dupKeepThis')}>
                          <i class="ti {isKeep ? 'ti-circle-check' : 'ti-circle'}"></i>
                        </button>
                        <i class="ti ti-file"></i>
                        <span class="dup-file-name" title={f.fileName}>{f.fileName}</span>
                        <span class="dup-file-meta">{f.version || f.installedDate || ''}</span>
                        {#if isKeep}<span class="dup-keep-badge">{t('mods.dupKeep')}</span>{/if}
                        <!-- Видалити саме цей файл (вибір «те чи те»). -->
                        <button class="dup-file-del" onclick={() => deleteDupFile(g, f)} disabled={bulkWorking}
                                title={t('mods.delete')}><i class="ti ti-trash"></i></button>
                      </div>
                    {/each}
                  </div>
                </div>
              {/each}
            </div>
            {/if}
          {/if}
        </div>
      {/if}

      <!-- ═══ Рекомендовані моди (hard-залежності вже встановлених модів) ═══ -->
      {#if visibleSuggestedMods.length > 0}
        <div class="suggest-mods">
          <div class="suggest-mods-head">
            <i class="ti ti-sparkles"></i>
            <span>{t('mods.suggested.title')}</span>
          </div>
          <div class="suggest-mods-list">
            {#each visibleSuggestedMods as s (s.depId)}
              <div class="suggest-mod-card rise-in">
                {#if s.iconUrl}
                  <img class="suggest-mod-icon" src={s.iconUrl} alt="" />
                {:else}
                  <div class="suggest-mod-icon suggest-mod-icon-fallback"><i class="ti ti-puzzle"></i></div>
                {/if}
                <div class="suggest-mod-info">
                  <span class="suggest-mod-name">{s.name || s.depId}</span>
                  {#if s.requiredBy?.length}
                    <span class="suggest-mod-req">{t('mods.suggested.requiredBy', { names: s.requiredBy.join(', ') })}</span>
                  {/if}
                </div>
                <button class="suggest-mod-install" disabled={installingSuggested === s.depId}
                        onclick={() => installSuggested(s)} title={t('mods.suggested.install')}>
                  {#if installingSuggested === s.depId}
                    <i class="ti ti-loader spin"></i>
                  {:else}
                    <i class="ti ti-download"></i>
                  {/if}
                </button>
                <button class="suggest-mod-dismiss" onclick={() => dismissSuggested(s.depId)} title={t('mods.suggested.dismiss')}>
                  <i class="ti ti-x"></i>
                </button>
              </div>
            {/each}
          </div>
        </div>
      {/if}

      {#if visibleMods.length === 0}
        <div class="mods-empty">
          <i class="ti ti-puzzle"></i>
          {t('mods.noMatches')}
        </div>
      {:else}
      <div class="mods-list" bind:this={listEl} style={vw.style}>
        {#each renderedMods as m, i (m.fileName)}
          <div class="mod-row" class:rise-in={!virtual && animateRows} class:disabled={!m.enabled} class:upd={m.hasUpdate} class:dup={m.isDuplicate}
               class:incompat={m.incompatible} class:selected={selected.has(m.fileName)} class:menu-open={menuFor === m.fileName}
               style={virtual ? '' : `animation-delay:${Math.min(i,14)*24}ms`}>
            <button type="button" role="checkbox" aria-checked={selected.has(m.fileName)}
                    class="mod-check" class:checked={selected.has(m.fileName)}
                    onclick={(e: any) => toggleSelect(m.fileName, e)} aria-label={t('mods.select')}>
              <i class="ti ti-check"></i>
            </button>
            {#if isModPageClickable(m)}
              <button type="button" class="mod-ic clickable" title={t('mods.openPage')}
                      onclick={(e: any) => { e.stopPropagation(); openModPage(m) }}>
                {#if m.hasIcon}<img use:modIcon={{ packId, fileName: m.fileName, rev: m.fileSize }} alt="" />
                {:else if m.iconUrl}<img src={m.iconUrl} alt="" loading="lazy" />
                {:else}<i class="ti ti-puzzle"></i>{/if}
                {#if m.source === 'modrinth'}
                  <span class="mod-src" title="Modrinth"><img src={modrinthIcon} alt="Modrinth" loading="lazy" /></span>
                {:else if m.source === 'curseforge'}
                  <span class="mod-src" title="CurseForge"><img src={curseforgeIcon} alt="CurseForge" loading="lazy" /></span>
                {/if}
              </button>
            {:else}
              <div class="mod-ic">
                {#if m.hasIcon}<img use:modIcon={{ packId, fileName: m.fileName, rev: m.fileSize }} alt="" />
                {:else if m.iconUrl}<img src={m.iconUrl} alt="" loading="lazy" />
                {:else}<i class="ti ti-puzzle"></i>{/if}
                {#if m.source === 'modrinth'}
                  <span class="mod-src" title="Modrinth"><img src={modrinthIcon} alt="Modrinth" loading="lazy" /></span>
                {:else if m.source === 'curseforge'}
                  <span class="mod-src" title="CurseForge"><img src={curseforgeIcon} alt="CurseForge" loading="lazy" /></span>
                {/if}
              </div>
            {/if}

            <div class="mod-info">
              <div class="mod-name">
                {#if isModPageClickable(m)}
                  <button type="button" class="mod-name-btn" title={t('mods.openPage')}
                          onclick={(e: any) => { e.stopPropagation(); openModPage(m) }}>{m.name}</button>
                {:else}
                  <span title={m.description || m.name}>{m.name}</span>
                {/if}
                {#if m.sourceKind}
                  <span class="src-badge" class:gone={m.sourceMissing}
                        title={m.sourceMissing ? t('modsrc.sourceGone') : m.sourcePath}>
                    <i class="ti ti-link"></i> {m.sourceKind === 'folder' ? t('modsrc.fromFolder') : t('modsrc.fromFile')}
                  </span>
                {/if}
                {#if m.status === 'checking'}
                  <i class="ti ti-loader spin mod-checking" title={t('mods.checkingOne')}></i>
                {:else if m.hasUpdate}
                  <span class="badge upd" title={t('mods.updateRangeHint', { from: m.version || '?', to: m.latestVersion || '?' })}>
                    <i class="ti ti-arrow-up"></i> {m.version ? m.version + ' → ' : ''}{m.latestVersion}
                  </span>
                {/if}
                <!-- SHAURMA: бейдж залежностей прибраний -->
                {#if !m.enabled}
                  <span class="badge off">{t('mods.off')}</span>
                {/if}
                {#if m.isDuplicate}
                  <span class="badge dup" title={t('mods.duplicateHint')}>
                    <i class="ti ti-copy"></i> {t('mods.duplicate')}
                  </span>
                {/if}
                {#if m.incompatible}
                  <span class="badge incompat" title={t('mods.incompatibleHint', { loader: m.incompatibleReason ?? '' })}>
                    <i class="ti ti-plug-x"></i> {t('mods.incompatible')}
                  </span>
                {/if}
                {#if tempModFor(m.fileName)}
                  {@const tm = tempModFor(m.fileName)!}
                  <span class="badge temp" title={t(tm.action === 'delete' ? 'mods.temp.willDeleteAt' : 'mods.temp.willDisableAt', { date: fmtDate(tm.expiresAt) })}>
                    <i class="ti ti-clock-hour-4"></i> {fmtTimeRemaining(tm.secondsRemaining)}
                  </span>
                {/if}
              </div>
              <div class="mod-meta">
                <!-- Технічний суфікс ".disabled" (внутрішній формат
                     вимкненого мода) ховаємо з відображення — стан і
                     так позначено бейджем "вимкнено" вище. Інакше при
                     довгому імені файлу рядок обрізається ellipsis'ом
                     саме на межі "*.jar" перед ".disabled", і візуально
                     здається, що розширення .jar зникло. -->
                {displayFileName(m.fileName)}
                {#if m.author} · {m.author}{/if}
                {#if m.version} · {m.version}{/if}
              </div>
            </div>

            <div class="mod-acts">
              {#if m.hasUpdate && !m.sourceKind}
                <button class="icon-btn upd" onclick={() => updateOne(m)} title={t('mods.updateOne')}><i class="ti ti-refresh"></i></button>
              {/if}
              <div class="toggle" class:on={m.enabled} class:locked={!!m.sourceKind} role="switch" aria-checked={m.enabled} tabindex="0"
                   onclick={() => toggleMod(m)}
                   onkeydown={(e: any) => { if (e.key === 'Enter' || e.key === ' ') toggleMod(m) }}
                   title={m.enabled ? t('mods.disable') : t('mods.enable')}></div>
              <button class="icon-btn" onclick={() => openVersion(m)} disabled={!!m.sourceKind} title={t('mods.changeVersion')}>
                <i class="ti ti-versions"></i>
              </button>
              <button class="icon-btn del" onclick={() => deleteMod(m)} disabled={deleting === m.fileName || !!m.sourceKind} title={t('mods.delete')}>
                {#if deleting === m.fileName}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-trash"></i>{/if}
              </button>
                <div class="row-menu-wrap">
                <button class="icon-btn" onclick={(e: any) => toggleRowMenu(m.fileName, e)} title={t('mods.moreActions')}>
                  <i class="ti ti-dots-vertical"></i>
                </button>
                {#if menuFor === m.fileName}
                  <div class="row-menu" onclick={(e: any) => e.stopPropagation()}>
                    <button onclick={() => { closeRowMenu(); revealMod(m.fileName) }}>
                      <i class="ti ti-folder"></i> {t('mods.revealInFolder')}
                    </button>
                    <button onclick={() => { closeRowMenu(); copyModLink(m) }} disabled={copyingLink === m.fileName}>
                      {#if copyingLink === m.fileName}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-link"></i>{/if} {t('mods.copyLink')}
                    </button>
                    {#if tempModFor(m.fileName)}
                      <button onclick={() => { closeRowMenu(); openTempModDialog(m) }}>
                        <i class="ti ti-clock-edit"></i> {t('mods.temp.editMenu')}
                      </button>
                      <button onclick={() => { closeRowMenu(); removeTempMod(m) }}>
                        <i class="ti ti-clock-off"></i> {t('mods.temp.removeMenu')}
                      </button>
                    {:else}
                      <button onclick={() => { closeRowMenu(); openTempModDialog(m) }}>
                        <i class="ti ti-clock-plus"></i> {t('mods.temp.setMenu')}
                      </button>
                    {/if}
                  </div>
                {/if}
              </div>
            </div>
          </div>
        {/each}
      </div>
      {/if}
    {/if}
  </div>

  <!-- ═══ Панель масових дій (з'являється, коли є вибір) ═══
       Знизу списку (не над ним) і "прилипає" до нижнього краю при
       скролі — не зникає з очей, поки вибір активний. -->
  {#if selectMode}
    <div class="bulk-bar">
      <div class="bulk-icons">
        {#each selectedModsPreview as m (m.fileName)}
          <div class="bulk-ic">
            {#if m.hasIcon}<img use:modIcon={{ packId, fileName: m.fileName, rev: m.fileSize }} alt="" />
            {:else if m.iconUrl}<img src={m.iconUrl} alt="" />
            {:else}<i class="ti ti-puzzle"></i>{/if}
          </div>
        {/each}
      </div>
      <span class="bulk-count">{t('mods.selectedCount', { n: selected.size })}</span>
      <div class="mt-spacer"></div>
      <button class="btn btn-ghost btn-sm" onclick={() => bulkEnable(true)} disabled={bulkWorking}>
        <i class="ti ti-power"></i> {t('mods.enable')}
      </button>
      <button class="btn btn-ghost btn-sm" onclick={() => bulkEnable(false)} disabled={bulkWorking}>
        <i class="ti ti-power"></i> {t('mods.disable')}
      </button>
      <button class="btn btn-ghost btn-sm" onclick={selectAllVisible} disabled={bulkWorking} title={t('mods.selectAllVisible')}>
        <i class="ti ti-square-check"></i> {t('mods.selectAllVisible')}
      </button>
      <button class="btn btn-ghost btn-sm bulk-del" onclick={bulkDelete} disabled={bulkWorking}>
        <i class="ti ti-trash"></i> {t('mods.delete')}
      </button>
      <button class="bulk-clear" onclick={clearSelection} title={t('mods.clearSelection')}><i class="ti ti-x"></i></button>
    </div>
  {/if}
</div>

<!-- ═══ Модалка: збірка запущена — зміни заборонені ═══ -->
{#if sourcesOpen}
  <ModSourcesDialog {packId} onclose={() => sourcesOpen = false} onchanged={() => loadMods()} />
{/if}

{#if graphOpen}
  <ModDependencyDialog {packId} {packName} {mcVersion} {loader} onclose={() => graphOpen = false} onchanged={() => loadMods()} />
{/if}

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
    <p class="mods-empty" style="text-align:left;padding:0 4px">{t('content.lockedWhileRunningBody')}</p>
    {#snippet footer()}
      <button class="btn btn-primary" onclick={() => runningLocked = false}>{t('content.lockedOk')}</button>
    {/snippet}
  </UiModal>
{/if}

<!-- ═══ Модалка: зміна версії ═══ -->
{#if versionMod}
  {@const vm = versionMod}
  {@const activeOpt = versionOpts.find(v => (v.id || v.url) === (selectedVersionKey || versionOpts[0]?.id || versionOpts[0]?.url))}
  <UiModal open title={t('mods.versionTitle', { name: vm.name })} icon="ti-versions" color="var(--orange)" width="720px" onclose={() => { if (!installingVersion) versionMod = null }} locked={installingVersion} bodyClass="ver-modal-body">
      <div class="ver-modal-inner">
        {#if versionLoading}
          <div class="mods-empty"><i class="ti ti-loader spin"></i> {t('mods.loadingVersions')}</div>
        {:else if versionOpts.length === 0}
          <div class="mods-empty"><i class="ti ti-puzzle"></i> {t('mods.noVersions')}</div>
        {:else}
          <div class="ver-split">
            <!-- Ліва колонка: список версій -->
            <div class="ver-list">
              {#each versionOpts as v, i (v.id || v.url)}
                {@const key = v.id || v.url}
                {@const isCurrent = v.isCurrent || v.versionNumber === versionMod.version}
                <div class="ver-row rise-in" class:current={isCurrent} class:active={key === (selectedVersionKey || versionOpts[0]?.id || versionOpts[0]?.url)}
                     style="animation-delay:{Math.min(i,20)*18}ms"
                     onclick={() => selectedVersionKey = key}>
                  <span class="vr-type" class:beta={v.versionType === 'beta'} class:alpha={v.versionType === 'alpha'}>
                    {versionTypeLabel(v.versionType)}
                  </span>
                  <div class="vr-info">
                    <div class="vr-ver">{v.versionNumber}</div>
                    <div class="vr-meta">
                      {#if v.gameVersion}<span class="chip">{v.gameVersion}</span>{/if}
                      {#if v.loader}<span class="chip">{v.loader}</span>{/if}
                    </div>
                  </div>
                  {#if isCurrent}
                    <span class="vr-current">{t('mods.current')}</span>
                  {/if}
                </div>
              {/each}
            </div>

            <!-- Права колонка: деталі обраної версії + журнал змін -->
            {#if activeOpt}
              {@const isCurrent = activeOpt.isCurrent || activeOpt.versionNumber === versionMod.version}
              <div class="ver-detail">
                <div class="vd-head">
                  <div class="vd-ver-row">
                    <span class="vd-ver">{activeOpt.versionNumber}</span>
                    <span class="vr-type" class:beta={activeOpt.versionType === 'beta'} class:alpha={activeOpt.versionType === 'alpha'}>
                      {versionTypeLabel(activeOpt.versionType)}
                    </span>
                  </div>
                  {#if activeOpt.datePublished}<span class="vd-date">{fmtDate(activeOpt.datePublished)}</span>{/if}
                </div>
                <div class="vd-meta">
                  {#if activeOpt.gameVersion}<span class="chip">{activeOpt.gameVersion}</span>{/if}
                  {#if activeOpt.loader}<span class="chip">{activeOpt.loader}</span>{/if}
                </div>
                {#if activeOpt.changelog}
                  <div class="vd-changelog-label">{t('mods.changelog')}</div>
                  <div class="vd-changelog">{activeOpt.changelog}</div>
                {/if}
                {#if !isCurrent}
                  <div class="vd-warn">
                    <i class="ti ti-alert-triangle"></i> {t('mods.versionSwitchWarn')}
                  </div>
                {/if}
              </div>
            {/if}
          </div>
        {/if}
      </div>
    {#snippet footer()}
      {#if installingVersion}
        <span class="md-working"><i class="ti ti-loader spin"></i> {t('mods.installing')}</span>
      {/if}
      <button class="btn btn-ghost" onclick={() => versionMod = null} disabled={installingVersion}>{t('cancel')}</button>
      {#if activeOpt && !(activeOpt.isCurrent || activeOpt.versionNumber === vm.version)}
        <button class="btn btn-primary" onclick={() => installVersion(activeOpt)} disabled={installingVersion}>
          {#if installingVersion}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-download"></i>{/if}
          {t('mods.installVersion', { v: activeOpt.versionNumber })}
        </button>
      {/if}
    {/snippet}
  </UiModal>
{/if}

<!-- SHAURMA: модалка залежностей прибрана -->

<!-- ═══ Модалка: оновити всі ═══ -->
{#if updateAllOpen}
  {@const toUpd = mods.filter(m => m.enabled && m.hasUpdate)}
  <UiModal
    open={true}
    size="lg"
    title={t('mods.updateAllTitle', { n: toUpd.length })}
    icon="ti-refresh"
    color="var(--orange)"
    locked={updateAllRunning}
    onclose={() => { if (!updateAllRunning) updateAllOpen = false }}
  >
        {#if toUpd.length === 0}
          <div class="mods-empty"><i class="ti ti-circle-check" style="color:var(--green)"></i> {t('mods.upToDate')}</div>
        {:else}
          <div class="ua-list">
            {#each toUpd as m (m.fileName)}
              <div class="ua-row">
                <span class="ua-name" title={m.fileName}>{m.name}</span>
                <span class="ua-old">{m.version}</span>
                <i class="ti ti-arrow-right ua-arr"></i>
                <span class="ua-new">{m.latestVersion}</span>
              </div>
            {/each}
          </div>
          <button type="button" role="checkbox" aria-checked={updateAllBackup}
                  class="ua-backup" class:checked={updateAllBackup} disabled={updateAllRunning}
                  onclick={() => updateAllBackup = !updateAllBackup}>
            <span class="ua-check"><i class="ti ti-check"></i></span>
            <span>{t('mods.updateAllBackup')}</span>
          </button>
          {#if updateAllProgress}
            <div class="ua-prog"><i class="ti ti-loader spin"></i> {t('mods.updateProgress', { p: updateAllProgress })}</div>
          {/if}
        {/if}
    {#snippet footer()}
      <button class="btn btn-ghost" onclick={() => updateAllOpen = false} disabled={updateAllRunning}>{t('cancel')}</button>
      <button class="btn btn-primary" onclick={runUpdateAll} disabled={updateAllRunning || toUpd.length === 0}>
        {#if updateAllRunning}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-refresh"></i>{/if}
        {t('mods.updateAll')}
      </button>
    {/snippet}
  </UiModal>
{/if}

<!-- ═══ Модалка: оновити ОДИН мод ═══ -->
{#if updateOneMod}
  <UiModal
    open={true}
    size="sm"
    title={t('mods.updateOneTitle', { name: updateOneMod.name })}
    icon="ti-refresh"
    color="var(--orange)"
    locked={updateOneRunning}
    onclose={() => { if (!updateOneRunning) updateOneMod = null }}
  >
        <div class="ua-row">
          <span class="ua-name" title={updateOneMod.fileName}>{updateOneMod.name}</span>
          <span class="ua-old">{updateOneMod.version}</span>
          <i class="ti ti-arrow-right ua-arr"></i>
          <span class="ua-new">{updateOneMod.latestVersion}</span>
        </div>
        <button type="button" role="checkbox" aria-checked={updateOneBackup}
                class="ua-backup" class:checked={updateOneBackup} disabled={updateOneRunning}
                onclick={() => updateOneBackup = !updateOneBackup}>
          <span class="ua-check"><i class="ti ti-check"></i></span>
          <span>{t('mods.updateAllBackup')}</span>
        </button>
    {#snippet footer()}
      <button class="btn btn-ghost" onclick={() => updateOneMod = null} disabled={updateOneRunning}>{t('cancel')}</button>
      <button class="btn btn-primary" onclick={confirmUpdateOne} disabled={updateOneRunning}>
        {#if updateOneRunning}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-refresh"></i>{/if}
        {t('mods.updateOne')}
      </button>
    {/snippet}
  </UiModal>
{/if}

<!-- ═══ Модалка: тимчасовий мод («становлення на строк») ═══ -->
{#if tempModDialog}
  <UiModal
    open={true}
    size="sm"
    title={t('mods.temp.dialogTitle', { name: tempModDialog.mod.name })}
    icon="ti-clock-hour-4"
    color="var(--orange)"
    locked={tempModDialog.saving}
    onclose={() => { if (!tempModDialog?.saving) tempModDialog = null }}
  >
    <label class="tm-label" for="tm-date">{t('mods.temp.dateLabel')}</label>
    <input
      id="tm-date"
      type="date"
      class="tm-date-input"
      value={tempModDialog.date}
      min={new Date().toISOString().slice(0, 10)}
      disabled={tempModDialog.saving}
      oninput={(e: Event) => { if (tempModDialog) tempModDialog.date = (e.target as HTMLInputElement).value }}
    />

    <div class="tm-action-row">
      <span class="tm-label">{t('mods.temp.actionLabel')}</span>
      <div class="tm-action-pills">
        <button
          type="button"
          class="pill"
          class:on={tempModDialog.action === 'disable'}
          disabled={tempModDialog.saving}
          onclick={() => { if (tempModDialog) tempModDialog.action = 'disable' }}
        >
          <i class="ti ti-power"></i> {t('mods.temp.actionDisable')}
        </button>
        <button
          type="button"
          class="pill"
          class:on={tempModDialog.action === 'delete'}
          disabled={tempModDialog.saving}
          onclick={() => { if (tempModDialog) tempModDialog.action = 'delete' }}
        >
          <i class="ti ti-trash"></i> {t('mods.temp.actionDelete')}
        </button>
      </div>
    </div>

    <button type="button" role="checkbox" aria-checked={tempModDialog.includeDeps}
            class="ua-backup" class:checked={tempModDialog.includeDeps} disabled={tempModDialog.saving}
            onclick={() => { if (tempModDialog) tempModDialog.includeDeps = !tempModDialog.includeDeps }}>
      <span class="ua-check"><i class="ti ti-check"></i></span>
      <span>{t('mods.temp.includeDeps')}</span>
    </button>
    <p class="tm-hint">{t('mods.temp.hint')}</p>

    {#snippet footer()}
      <button class="btn btn-ghost" onclick={() => tempModDialog = null} disabled={tempModDialog?.saving}>{t('cancel')}</button>
      <button class="btn btn-primary" onclick={submitTempModDialog} disabled={tempModDialog?.saving || !tempModDialog?.date}>
        {#if tempModDialog?.saving}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-check"></i>{/if}
        {t('save')}
      </button>
    {/snippet}
  </UiModal>
{/if}
<!-- ═══ Модалка: резервні копії ═══ -->
{#if backupsOpen}
  <UiModal
    open={true}
    size="lg"
    title={t('mods.backupsTitle')}
    icon="ti-archive"
    color="var(--green)"
    onclose={() => backupsOpen = false}
  >
        {#if backups.length === 0}
          <div class="mods-empty"><i class="ti ti-archive-off"></i> {t('mods.backupsNone')}</div>
        {:else}
          <div class="bk-list">
            {#each backups as b (b.id)}
              <div class="bk-row">
                <div class="bk-info">
                  <div class="bk-name">{b.name}</div>
                  <div class="bk-sub">{b.version} · {fmtDate(b.date)}</div>
                </div>
                {#if b.sourceUrl}
                  <button class="btn btn-ghost btn-sm" onclick={() => copyBackupSource(b)} title={t('mods.backupSource')}>
                    <i class="ti ti-link"></i>
                  </button>
                {/if}
                <button class="btn btn-ghost btn-sm" onclick={() => restoreBackup(b)} disabled={restoringId === b.id}>
                  {#if restoringId === b.id}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-rotate-clockwise"></i>{/if}
                  {t('mods.restore')}
                </button>
              </div>
            {/each}
          </div>
        {/if}
  </UiModal>
{/if}

<!-- ═══ Модалка: міграція модів ═══ -->
{#if migrateOpen}
  <ModMigrationDialog
    {packId}
    {packName}
    {mcVersion}
    {loader}
    onClose={() => { migrateOpen = false; loadMods() }}
  />
{/if}

<!-- ═══ Модалка: порівняння модів двох збірок ═══ -->
{#if compareOpen}
  <PackCompareDialog
    {packId}
    {packName}
    {mcVersion}
    {loader}
    {iconUrl}
    {icon}
    {color}
    {packPicker}
    onClose={() => { compareOpen = false; loadMods() }}
  />
{/if}

<!-- ═══ Модалка: експорт списку модів (як Prism ExportToModList) ═══ -->
{#if exportOpen}
  <ModListExportDialog {packId} open={true} selectedFileNames={exportSelectedFiles} onClose={() => exportOpen = false} />
{/if}

<style>
  .mods { display: flex; flex-direction: column; gap: 12px; }

  /* ── Тулбар: дві групи (пошук+фільтри / дії). Перенос між групами
       акуратний: група-дій притиснута праворуч (margin-left:auto), при
       звуженні переноситься ЦІЛОЮ на новий рядок, а не рветься кнопками.
       Sticky: поки список модів довгий, тулбар лишається на очах під
       закріпленим тулбаром сторінки збірки (відступ --mt-top передає
       PackInstancePage — виміряна висота того тулбара), а не їде за екран.
       Напівпрозора «скляна» підкладка — контент не світить крізь текст. */
  /* Тулбар менеджера модів — ЗАВЖДИ два чіткі рядки, а не один flex-wrap,
     що ламався непередбачувано: раніше пошук/фільтр/сортування і кнопки
     дій жили в одному wrap-рядку, і коли не влазило все разом, кнопки
     дій самі починали переноситись по одній та стискатися — рядок дій
     виглядав розбитим і кнопки ставали дрібними. Тепер mt-group-main
     (пошук + фільтр + сортування) — рядок 1, mt-group-actions (кнопки
     дій) — рядок 2, кожен на всю ширину, з flex-wrap лише ВСЕРЕДИНІ
     групи (на дуже вузьких вікнах), а не між ними.
     mt-group-actions тепер містить УСІ дії прямими кнопками (без
     overflow-«⋯»-меню) — на всю ширину рядка; на вузьких вікнах кнопки
     переносяться по flex-wrap, зберігаючи нормальний розмір, замість
     стиснення в один рядок. */
  .mods-toolbar {
    display: flex; flex-direction: column; gap: 8px;
    position: sticky; top: var(--mt-top, 0px); z-index: 18;
    margin: -4px -4px 8px; padding: 8px 4px 10px;
    background: linear-gradient(180deg,
      color-mix(in srgb, var(--bg-app) 92%, transparent) 0%,
      color-mix(in srgb, var(--bg-app) 78%, transparent) 100%);
    backdrop-filter: blur(12px); -webkit-backdrop-filter: blur(12px);
    border-radius: 0 0 var(--r-lg) var(--r-lg);
  }
  .mt-group { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  .mt-group-main { width: 100%; min-width: 0; }
  .mt-group-actions { width: 100%; justify-content: flex-end; }
  /* Компактні іконкові кнопки тулбара (без текстового підпису) — лише
     іконка по центру, щоб рядок дій не розтягувався. Трохи більший
     розмір (36px, було 32px), щоб кнопки не виглядали дрібними поряд з
     текстовими кнопками того самого рядка. */
  .icon-tb { width: 36px; height: 36px; padding: 0; justify-content: center; }
  .mt-group-actions .btn { height: 36px; }
  .mt-search {
    display: flex; align-items: center; gap: 8px;
    background: var(--bg-input); border: 1px solid var(--border); border-radius: var(--r-sm);
    padding: 0 11px; height: 32px; flex: 1 1 220px; min-width: 150px;
  }
  .mt-search i { color: var(--text-dim); font-size: 14px; }
  .mt-search input { flex: 1; background: none; border: none; outline: none; color: var(--text); font-size: 12.5px; }
  .mt-spacer { flex: 1; }
  .upd-badge {
    display: inline-flex; align-items: center; justify-content: center;
    min-width: 18px; height: 18px; padding: 0 5px; border-radius: 999px;
    background: rgba(255,255,255,.22); font-size: 10.5px; font-weight: 800; margin-left: 2px;
  }

  /* ── Статус ── */
  .mods-status { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; font-size: 11.5px; color: var(--text-mute); min-height: 16px; }

  /* ── Віджет дублікатів: групи одним блоком ── */
  /* ── Віджет дублікатів: групи одним блоком ──
     Оновлено: картка мода з іконкою/назвою/лічильником, кнопки видалення
     у стилі лаунчера (icon-btn), плавне розкриття груп (grid-rows trick),
     анімація появи рядків, ховер-підсвітка. */
  .dup-widget {
    margin: 6px 0 12px; border: 1px solid rgba(239,68,68,.35); border-radius: var(--r-lg);
    background: linear-gradient(180deg, rgba(239,68,68,.05), rgba(239,68,68,.02));
    overflow: hidden;
    box-shadow: 0 4px 18px -8px rgba(239,68,68,.25);
  }
  .dup-widget-head {
    display: flex; align-items: center; gap: 10px; padding: 12px 14px; cursor: pointer;
    user-select: none; transition: background .18s;
  }
  .dup-widget-head:hover { background: rgba(239,68,68,.07); }
  .dup-widget-ic {
    width: 30px; height: 30px; min-width: 30px; border-radius: 8px;
    background: rgba(239,68,68,.14); display: flex; align-items: center; justify-content: center;
  }
  .dup-widget-ic .ti-copy { color: #ef4444; font-size: 15px; }
  .dup-widget-title { font-size: 12.5px; font-weight: 800; color: #ef4444; }
  .dup-widget-sub { font-size: 11px; color: var(--text-mute); }
  .dup-widget-chev { margin-left: auto; color: var(--text-mute); font-size: 15px; transition: transform .25s cubic-bezier(.34,1.56,.64,1); }
  .dup-widget.expanded .dup-widget-chev { transform: rotate(180deg); }
  .dup-search {
    display: flex; align-items: center; gap: 8px; margin: 0 14px 12px;
    background: var(--bg-input); border: 1px solid var(--border); border-radius: var(--r-sm);
    padding: 0 11px; height: 30px; transition: border-color .15s;
  }
  .dup-search:focus-within { border-color: var(--orange); }
  .dup-search i { color: var(--text-dim); font-size: 14px; flex-shrink: 0; }
  .dup-search input { flex: 1; min-width: 0; background: none; border: none; outline: none; color: var(--text); font-size: 12px; font-family: inherit; }
  .dup-search-clear { background: none; border: none; color: var(--text-dim); cursor: pointer; padding: 2px; font-size: 13px; display: flex; align-items: center; border-radius: 4px; transition: color .15s; }
  .dup-search-clear:hover { color: var(--text); background: var(--hover); }
  .dup-empty {
    display: flex; align-items: center; justify-content: center; gap: 8px;
    padding: 18px 14px; color: var(--text-mute); font-size: 12px;
  }
  .dup-empty i { font-size: 16px; color: var(--text-dim); }
  .dup-groups { display: flex; flex-direction: column; gap: 10px; padding: 0 14px 14px; }
  .dup-group {
    border: 1px solid var(--border); border-radius: var(--r-lg); background: var(--bg-card); overflow: hidden;
    transition: border-color .15s, box-shadow .2s;
  }
  .dup-group:hover { border-color: rgba(239,68,68,.4); box-shadow: 0 4px 14px -8px rgba(239,68,68,.2); }
  .dup-group-head { display: flex; align-items: center; gap: 10px; padding: 10px 12px; }
  .dup-group-ic {
    width: 32px; height: 32px; min-width: 32px; border-radius: 8px; overflow: hidden;
    background: var(--bg-panel); border: 1px solid var(--border);
    display: flex; align-items: center; justify-content: center; color: var(--text-dim); font-size: 14px;
  }
  .dup-group-ic img { width: 100%; height: 100%; object-fit: cover; }
  .dup-group-meta { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
  .dup-group-name { font-size: 12.5px; font-weight: 700; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .dup-group-sub { font-size: 10.5px; color: var(--text-mute); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .dup-group-count {
    font-size: 10.5px; font-weight: 800; color: #ef4444; font-family: var(--font-mono);
    background: rgba(239,68,68,.1); padding: 3px 9px; border-radius: 999px; flex-shrink: 0;
  }
  .dup-group-del { flex-shrink: 0; }
  .dup-group-files { display: flex; flex-direction: column; border-top: 1px solid var(--border); }
  .dup-file {
    display: flex; align-items: center; gap: 9px; padding: 7px 12px; font-size: 11.5px;
    transition: background .13s;
  }
  .dup-file:hover { background: var(--hover); }
  .dup-file + .dup-file { border-top: 1px solid var(--border-dim, var(--border)); }
  .dup-file > .ti-file { color: var(--text-dim); font-size: 12px; flex-shrink: 0; }
  .dup-file-name { flex: 1; min-width: 0; font-family: var(--font-mono); font-size: 10.5px; color: var(--text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .dup-file-meta { font-size: 10px; color: var(--text-mute); font-family: var(--font-mono); flex-shrink: 0; }
  .dup-keep-badge { font-size: 9.5px; font-weight: 800; color: var(--green); background: rgba(16,185,129,.12); padding: 2px 7px; border-radius: 999px; flex-shrink: 0; }
  .dup-file.keep { background: rgba(16,185,129,.05); }
  .dup-file:not(.keep) { color: var(--text-mute); }
  /* Радіо-«лишити цю копію» + кнопка видалення файлу — у стилі лаунчера
     (були ГОЛИМИ кнопками без CSS). */
  .dup-file-keep {
    background: none; border: none; padding: 3px; cursor: pointer; color: var(--text-dim);
    font-size: 14px; display: flex; align-items: center; border-radius: 6px; flex-shrink: 0;
    transition: color .15s, transform .15s;
  }
  .dup-file-keep:hover { color: var(--green); transform: scale(1.12); }
  .dup-file-keep.on { color: var(--green); }
  .dup-file-keep i { font-size: 14px; }
  .dup-file-del {
    background: none; border: none; padding: 4px; cursor: pointer; color: var(--text-dim);
    font-size: 13px; display: flex; align-items: center; border-radius: 6px; flex-shrink: 0;
    transition: color .15s, background .15s, transform .15s;
  }
  .dup-file-del:hover { color: var(--red); background: rgba(239,68,68,.1); transform: scale(1.1); }
  .dup-file-del:disabled { opacity: .4; cursor: not-allowed; transform: none; }

  .ms-item { display: inline-flex; align-items: center; gap: 5px; }
  .ms-item.acc { color: var(--orange); font-weight: 600; }
  .ms-item.dim { color: var(--text-dim); }
  .ms-item.err { color: var(--red); }

  /* ── Список модів (рядки, як у прикладі лаунчера) ── */
  .mods-list { display: flex; flex-direction: column; gap: 8px; }
  /* Оптимізація (розділ 5): рядки поза в'юпортом не рендеряться браузером
     (content-visibility) — при 200+ модах рендер/пам'ять набагато легші,
     а скрол сторінки лишається плавним. contain-intrinsic-size — щоб
     висота рядка була відома БЕЗ рендеру (без стрибків скролу). */
  .mod-row { content-visibility: auto; contain-intrinsic-size: auto 70px; }
  .mod-row {
    display: flex; align-items: center; gap: 14px;
    padding: 12px 16px; border-radius: var(--r-md);
    background: var(--bg-panel); border: 1px solid var(--border);
    transition: border-color .15s;
    /* position:relative + z-index власного стекінг-контексту потрібні,
       щоб рядок з відкритим меню "⋯" завжди малювався НАД сусідніми
       рядками нижче. Без цього анімація rise-in (opacity/transform) на
       кожному рядку створює власний стекінг-контекст під час програвання,
       і випадаюче меню верхнього рядка провалюється під рядки, що йдуть
       далі за ним у DOM (вони "перекривають" absolute-меню, попри його
       великий z-index — той z-index діє лише всередині СВОГО контексту). */
    position: relative; z-index: 0;
  }
  .mod-row:hover { border-color: var(--border-hi); }
  .mod-row.upd { border-color: rgba(var(--orange-rgb), .4); }
  .mod-row.disabled { opacity: .55; }
  .mod-row.dup { border-color: rgba(239,68,68,.5); animation: dup-blink 1.6s ease-in-out infinite; }
  .mod-row.menu-open {
    z-index: 50;
    /* content-visibility:auto створює paint containment — контент, що
       виходить за межі рядка (absolute-меню "⋯"), ОБРІЗАЄТЬСЯ рамками
       рядка: меню "провалювалось" під наступні картки модів. При
       відкритому меню вимикаємо containment (content-visibility:visible
       = contain:none) — меню вільно спливає над сусідніми рядками. */
    content-visibility: visible;
    contain: none;
  }
  @keyframes dup-blink {
    0%, 100% { box-shadow: 0 0 0 0 rgba(239,68,68,0); }
    50% { box-shadow: 0 0 0 3px rgba(239,68,68,.25); }
  }
  .badge.dup { background: rgba(239,68,68,.15); color: #ef4444; }

  .mod-ic {
    position: relative; width: 40px; height: 40px; min-width: 40px; border-radius: 10px;
    background: var(--bg-input); border: 1px solid var(--border);
    display: flex; align-items: center; justify-content: center;
    font-size: 18px; color: var(--violet-l); overflow: visible;
  }
  .mod-ic img { width: 100%; height: 100%; object-fit: cover; border-radius: 9px; }
  /* Клікабельна іконка мода (<button>) → сторінка мода в Браузері контенту.
     Скидаємо button-стилі за замовчуванням, лишаючи вигляд .mod-ic. */
  button.mod-ic {
    padding: 0; cursor: pointer; font-family: inherit;
    transition: border-color .15s, transform .1s;
  }
  button.mod-ic:hover { border-color: var(--violet-l); }
  button.mod-ic:active { transform: scale(.96); }
  .mod-src {
    position: absolute; right: -6px; bottom: -6px; width: 17px; height: 17px; border-radius: 50%;
    overflow: hidden;
    display: flex; align-items: center; justify-content: center;
    border: 2px solid var(--bg-panel); background: var(--bg-panel);
  }
  .mod-src img { width: 100%; height: 100%; object-fit: cover; display: block; }

  .mod-info { flex: 1; min-width: 0; }
  .mod-name {
    font-size: 13px; font-weight: 700; color: var(--text);
    display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
  }
  /* Клікабельна назва мода (<button>) → сторінка мода в Браузері контенту.
     Скидаємо button-стилі за замовчуванням, лишаючи вигляд тексту назви. */
  .mod-name-btn {
    padding: 0; margin: 0; border: none; background: transparent; cursor: pointer;
    font: inherit; font-weight: 700; color: var(--text); text-align: left;
  }
  .mod-name-btn:hover { color: var(--violet-l); text-decoration: underline; }
  .mod-checking { font-size: 12px; color: var(--text-mute); }
  .mod-meta {
    font-size: 10.5px; color: var(--text-mute); margin-top: 2px;
    font-family: var(--font-mono);
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }

  .badge { font-size: 9px; font-weight: 700; padding: 2px 7px; border-radius: 5px; font-family: var(--font-mono); letter-spacing: .3px; }
  .badge.upd { background: rgba(16,185,129,.15); color: var(--green); }
  .badge.dep { background: rgba(245,158,11,.15); color: var(--yellow); cursor: pointer; display: inline-flex; align-items: center; gap: 3px; }
  .badge.dep:hover { background: rgba(245,158,11,.25); }
  .badge.off { background: var(--bg-input); color: var(--text-mute); border: 1px solid var(--border); }

  .mod-acts { display: flex; align-items: center; gap: 4px; flex-shrink: 0; }
  .icon-btn {
    width: 30px; height: 30px; border-radius: 7px; background: transparent; border: none; cursor: pointer;
    display: flex; align-items: center; justify-content: center; color: var(--text-mute); font-size: 15px;
    transition: background .15s, color .15s;
  }
  .icon-btn:hover { background: rgba(255,255,255,.08); color: var(--text); }
  .icon-btn.upd { color: var(--green); }
  .icon-btn.upd:hover { background: rgba(16,185,129,.12); }
  /* Бейдж оновлення з діапазоном «стара → нова» версія — видно одразу
     на картці, без відкриття модалки. */
  .badge.upd { display: inline-flex; align-items: center; gap: 3px; }
  .badge.upd .ti-arrow-up { font-size: 9px; }
  .icon-btn.del:hover { background: rgba(239,68,68,.12); color: var(--red); }
  .icon-btn:disabled { opacity: .5; cursor: default; }

  .toggle {
    position: relative; width: 42px; height: 24px; min-width: 42px;
    background: var(--bg-input); border-radius: 14px; cursor: pointer;
    transition: background .2s; border: 1px solid var(--border);
  }
  .toggle::after {
    content: ''; position: absolute; top: 2px; left: 3px; width: 18px; height: 18px;
    border-radius: 50%; background: var(--text-mute); transition: transform .22s, background .22s;
    box-shadow: 0 2px 4px rgba(0,0,0,.4);
  }
  .toggle.on { background: rgba(255,138,0,.25); border-color: rgba(255,138,0,.4); }
  .toggle.on::after { transform: translateX(18px); background: var(--orange); box-shadow: 0 2px 8px rgba(255,138,0,.5); }

  .mods-empty {
    display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 14px;
    padding: 64px 10px; color: var(--text-mute); font-size: 13.5px; text-align: center;
  }
  .mods-empty i { font-size: 64px; color: var(--text-dim); }
  .mods-empty i.spin { font-size: 30px; }

  /* ── Рекомендовані моди («Налаштування → Моди → Вимкнути рекомендації») ── */
  .suggest-mods { margin: 0 0 14px; }
  .suggest-mods-head {
    display: flex; align-items: center; gap: 8px;
    font-size: 12.5px; font-weight: 700; color: var(--text-mute);
    text-transform: uppercase; letter-spacing: .3px; margin: 0 2px 8px;
  }
  .suggest-mods-head i { color: var(--accent); font-size: 15px; }
  .suggest-mods-list { display: flex; flex-direction: column; gap: 8px; }
  /* Оптимізація (розділ 5): картки рекомендованих модів поза в'юпортом не
     рендеряться браузером (content-visibility) — при десятках залежностей
     список лишається легким, як і основний список модів. */
  .suggest-mod-card { content-visibility: auto; contain-intrinsic-size: auto 56px; }
  .suggest-mod-card {
    display: flex; align-items: center; gap: 12px;
    padding: 10px 12px; border-radius: var(--r-md);
    background: var(--bg-panel); border: 1px dashed var(--border-hi);
  }
  .suggest-mod-icon {
    width: 32px; height: 32px; border-radius: var(--r-sm);
    object-fit: cover; flex-shrink: 0; background: var(--bg-input);
  }
  .suggest-mod-icon-fallback {
    display: flex; align-items: center; justify-content: center;
    color: var(--text-dim); font-size: 16px;
  }
  .suggest-mod-info { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
  .suggest-mod-name { font-size: 13.5px; font-weight: 600; color: var(--text); }
  .suggest-mod-req { font-size: 11.5px; color: var(--text-mute); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .suggest-mod-install, .suggest-mod-dismiss {
    display: flex; align-items: center; justify-content: center;
    width: 30px; height: 30px; border-radius: var(--r-sm); border: 1px solid var(--border);
    background: var(--bg-input); color: var(--text-mute); cursor: pointer; transition: .15s; flex-shrink: 0;
  }
  .suggest-mod-install:hover:not(:disabled) { border-color: var(--accent); color: var(--accent); }
  .suggest-mod-install:disabled { opacity: .6; cursor: default; }
  .suggest-mod-dismiss:hover { border-color: var(--border-hi); color: var(--text); }

  /* ── Версії ── */
  .ver-list { display: flex; flex-direction: column; gap: 6px; }
  .ver-row {
    display: flex; align-items: center; gap: 10px;
    padding: 10px 12px; background: var(--bg-input); border: 1px solid var(--border);
    border-radius: var(--r-sm); cursor: pointer; transition: .15s;
  }
  .ver-row:hover { border-color: var(--border-hi); background: var(--hover); }
  .ver-row.current { border-color: rgba(16,185,129,.4); }
  .vr-info { flex: 1; min-width: 0; }
  .vr-ver { font-size: 12.5px; font-weight: 700; color: var(--text); font-family: var(--font-mono); }
  .vr-meta { display: flex; align-items: center; gap: 6px; margin-top: 4px; flex-wrap: wrap; }
  .chip {
    font-size: 10px; font-weight: 600; color: var(--text-mute);
    background: var(--bg-panel); border: 1px solid var(--border);
    padding: 2px 7px; border-radius: 999px;
  }
  .vr-date { font-size: 10.5px; color: var(--text-dim); }
  .vr-current { font-size: 10.5px; font-weight: 700; color: var(--green); }
  .vr-icon { color: var(--orange); font-size: 15px; }
  .md-working { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; color: var(--text-mute); margin-right: auto; }
  .md-close {
    background: none; border: none; color: var(--text-dim); font-size: 16px;
    cursor: pointer; padding: 4px; border-radius: 6px; line-height: 1;
  }
  .md-close:hover { color: var(--text); background: var(--hover); }

  /* ── Оновити всі ── */
  .ua-list { display: flex; flex-direction: column; gap: 5px; max-height: 300px; overflow-y: auto; }
  .ua-row {
    display: flex; align-items: center; gap: 8px;
    padding: 7px 10px; background: var(--bg-input); border: 1px solid var(--border); border-radius: var(--r-sm);
    font-size: 12px;
  }
  .ua-name { flex: 1; min-width: 0; font-weight: 600; color: var(--text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .ua-old { font-family: var(--font-mono); font-size: 10.5px; color: var(--text-dim); text-decoration: line-through; }
  .ua-arr { color: var(--text-dim); font-size: 12px; }
  .ua-new { font-family: var(--font-mono); font-size: 10.5px; color: var(--green); }
  .ua-backup {
    display: flex; align-items: center; gap: 8px; margin-top: 12px;
    font-size: 12px; color: var(--text); cursor: pointer; user-select: none;
    border: none; background: none; padding: 0; font-family: inherit; text-align: left;
  }
  .ua-backup:disabled { opacity: .55; cursor: not-allowed; }
  /* Кастомний чекбокс у модалках оновлення (той самий стиль, що .mod-check
     на рядках модів): квадрат з рамкою, галочка з'являється тільки коли
     увімкнено. */
  .ua-check {
    width: 18px; height: 18px; flex-shrink: 0; margin: 0; padding: 0;
    border-radius: 6px; border: 1.5px solid var(--border-hi);
    background: var(--bg-input);
    display: flex; align-items: center; justify-content: center;
    color: transparent; font-size: 13px; line-height: 1;
    transition: border-color .15s, background .15s, color .12s, transform .12s;
  }
  .ua-backup:hover:not(:disabled) .ua-check { border-color: var(--orange); transform: scale(1.08); }
  .ua-backup.checked .ua-check { background: var(--orange); border-color: var(--orange); color: #fff; }
  .ua-prog { display: flex; align-items: center; gap: 7px; margin-top: 10px; font-size: 12px; color: var(--orange); }

  /* ── Резервні копії ── */
  .bk-list { display: flex; flex-direction: column; gap: 6px; }
  .bk-row {
    display: flex; align-items: center; gap: 10px;
    padding: 9px 11px; background: var(--bg-input); border: 1px solid var(--border);
    border-radius: var(--r-sm);
  }
  .bk-info { flex: 1; min-width: 0; }
  .bk-name { font-size: 12.5px; font-weight: 700; color: var(--text); }
  .bk-sub { font-size: 11px; color: var(--text-mute); font-family: var(--font-mono); margin-top: 2px; }

  /* ── Несумісний лоадер: блимання рядка (те саме, що .dup, окремий колір) ── */
  .mod-row.incompat { border-color: rgba(239,68,68,.55); animation: incompat-blink 1.4s ease-in-out infinite; }
  @keyframes incompat-blink {
    0%, 100% { box-shadow: 0 0 0 0 rgba(239,68,68,0); }
    50% { box-shadow: 0 0 0 3px rgba(239,68,68,.3); }
  }
  .badge.incompat { background: rgba(239,68,68,.15); color: #ef4444; }
  /* Тимчасовий мод: жовтий/бурштиновий (той самий колір, що зірочка
     закладок у браузері контенту) — «час іде», не помилка й не тривога. */
  .badge.temp { background: rgba(245,197,66,.15); color: #f5c542; }

  /* ── Модалка тимчасового мода ── */
  .tm-label {
    display: block; font: 600 12px/1.4 var(--font-ui); color: var(--text-dim);
    margin-bottom: 6px;
  }
  .tm-date-input {
    width: 100%; padding: 9px 12px; margin-bottom: 16px;
    background: var(--bg-input, var(--bg-card));
    border: 1px solid var(--border); border-radius: 10px;
    color: var(--text); font: 500 13px/1.4 var(--font-ui);
  }
  .tm-date-input:focus { outline: none; border-color: color-mix(in srgb, var(--orange) 50%, var(--border)); }
  .tm-action-row { margin-bottom: 14px; }
  .tm-action-pills { display: flex; gap: 8px; }
  /* Кнопки «Вимкнути»/«Видалити» — раніше class="pill" без жодного
     визначення .pill у цьому компоненті: показувались як голі,
     нестилізовані <button> браузера. Той самий дизайн пілюлі-перемикача,
     що й у ContentBrowser (браузер контенту), тут — orange-акцент
     (узгоджено з icon="ti-clock-hour-4" color="var(--orange)" модалки). */
  .tm-action-pills .pill {
    display: inline-flex; align-items: center; gap: 6px;
    padding: 8px 14px; border-radius: 999px;
    background: var(--bg-input, var(--bg-card)); border: 1px solid var(--border);
    color: var(--text-mute);
    font: 700 12px/1 var(--font-ui);
    cursor: pointer;
    transition: transform .18s ease, border-color .15s, background .15s, color .15s;
  }
  .tm-action-pills .pill:hover:not(:disabled) {
    transform: translateY(-1px);
    border-color: var(--border-hi);
    color: var(--text);
  }
  .tm-action-pills .pill.on {
    color: var(--orange);
    border-color: rgba(var(--orange-rgb), .5);
    background: rgba(var(--orange-rgb), .1);
    box-shadow: inset 0 0 0 1px rgba(var(--orange-rgb), .18);
  }
  .tm-action-pills .pill:disabled { opacity: .55; cursor: default; transform: none; }
  .tm-hint {
    margin: 10px 0 0; font: 500 11.5px/1.5 var(--font-ui); color: var(--text-mute);
  }

  /* ── Чекбокс вибору на рядку мода — кастомний (Modrinth-style) ── */
  .mod-check {
    width: 18px; height: 18px; flex-shrink: 0; margin: 0; padding: 0;
    border-radius: 6px; border: 1.5px solid var(--border-hi);
    background: var(--bg-input);
    display: flex; align-items: center; justify-content: center;
    color: transparent; cursor: pointer; font-size: 13px; line-height: 1;
    transition: border-color .15s, background .15s, color .12s, transform .12s;
  }
  .mod-check:hover { border-color: var(--orange); transform: scale(1.08); }
  .mod-check.checked { background: var(--orange); border-color: var(--orange); color: #fff; }
  .mod-check.checked:hover { background: color-mix(in srgb, var(--orange) 85%, #fff); }
  .mod-row.selected { border-color: rgba(255,138,0,.5); background: rgba(255,138,0,.05); }

  /* ── Панель масових дій ──
     Розміщена ПІСЛЯ списку модів (не над ним) і "прилипає" до низу
     видимої області при скролі (position: sticky), щоб лишатись на
     очах, поки активний вибір — а не гортатись разом зі списком і
     зникати з екрана. */
  .bulk-bar {
    position: sticky; bottom: 8px; z-index: 15;
    display: flex; align-items: center; gap: 10px; flex-wrap: wrap;
    padding: 8px 12px; background: var(--bg-panel); border: 1px solid rgba(255,138,0,.4);
    border-radius: var(--r-md); box-shadow: 0 6px 20px rgba(0,0,0,.35);
  }
  /* Іконки обраних модів — до 3, "купкою" з перекриттям зліва в панелі. */
  .bulk-icons { display: flex; align-items: center; flex-shrink: 0; }
  .bulk-ic {
    width: 26px; height: 26px; border-radius: 7px; overflow: hidden;
    background: var(--bg-input); border: 2px solid var(--bg-panel);
    display: flex; align-items: center; justify-content: center;
    font-size: 12px; color: var(--text-dim);
    margin-left: -8px;
  }
  .bulk-ic:first-child { margin-left: 0; }
  .bulk-ic img { width: 100%; height: 100%; object-fit: cover; }
  .bulk-clear {
    width: 24px; height: 24px; border-radius: 6px; background: transparent; border: none; cursor: pointer;
    display: flex; align-items: center; justify-content: center; color: var(--text-mute); flex-shrink: 0;
  }
  .bulk-clear:hover { background: rgba(255,255,255,.08); color: var(--text); }
  .bulk-count { font-size: 12.5px; font-weight: 700; color: var(--text); white-space: nowrap; }
  .bulk-del:hover { background: rgba(239,68,68,.12); color: var(--red); }

  /* ── 3-крапки меню на рядку мода ── */
  .row-menu-wrap { position: relative; }
  .row-menu {
    position: absolute; top: calc(100% + 4px); right: 0; z-index: 2147483000;
    display: flex; flex-direction: column; min-width: 190px;
    background: var(--bg-panel); border: 1px solid var(--border-hi); border-radius: var(--r-md);
    box-shadow: 0 8px 24px rgba(0,0,0,.4); padding: 4px; gap: 1px;
  }
  .row-menu button {
    display: flex; align-items: center; gap: 8px; width: 100%;
    padding: 8px 10px; background: transparent; border: none; border-radius: var(--r-sm);
    color: var(--text); font-size: 12.5px; text-align: left; cursor: pointer;
  }
  .row-menu button:hover { background: var(--hover); }
  .row-menu button.danger { color: var(--red); }
  .row-menu button.danger:hover { background: rgba(239,68,68,.12); }
  .row-menu button:disabled { opacity: .5; cursor: default; }
  .row-menu-sep { height: 1px; background: var(--border); margin: 3px 2px; }

  /* ── Дроп-зона списку модів (drag&drop jar у вікно) ── */
  .mods-dropzone { position: relative; border-radius: var(--r-md); transition: background .15s; }
  .mods-dropzone.drag {
    background: rgba(255,138,0,.06); outline: 2px dashed rgba(255,138,0,.5); outline-offset: 4px;
  }

  /* ── Порожня тека модів (окремий стан, з діями) ──
     Окрема картка у стилі лаунчера: іконка-«плитка» з градієнтом, заголовок,
     підказка та дії — як у порожнього стану серверів/картки «Створити збірку».
     Раніше був «сирий» стан з гігантською іконкою 64px і майже без відступів,
     який вибивався з решти сторінки. */
  .mods-empty-folder {
    display: flex; flex-direction: column; align-items: center; text-align: center;
    gap: 8px; padding: 44px 24px; margin: 4px 0;
    background: color-mix(in srgb, var(--bg-card) 55%, transparent);
    border: 1px dashed var(--border-hi); border-radius: var(--r-lg);
    animation: rise-in .3s ease both;
  }
  .mods-empty-folder i { color: var(--orange); }
  .mef-ic {
    width: 62px; height: 62px; border-radius: 17px; margin-bottom: 4px;
    display: flex; align-items: center; justify-content: center; font-size: 27px;
    background: linear-gradient(135deg, rgba(var(--orange-rgb), .2), rgba(var(--orange-rgb), .04));
    border: 1px solid rgba(var(--orange-rgb), .35);
    box-shadow: 0 10px 26px rgba(0,0,0,.3);
  }
  .mef-title { font-size: 15px; font-weight: 700; color: var(--text); }
  .mef-sub { font-size: 12.5px; color: var(--text-mute); max-width: 400px; line-height: 1.5; }
  .mef-actions { display: flex; gap: 8px; margin-top: 12px; }
  .mef-drop {
    display: inline-flex; align-items: center; gap: 6px; margin-top: 6px;
    font-size: 11.5px; color: var(--text-dim);
  }

  /* ── Модалка версій: двоколонковий макет (список + деталі/changelog) ──
     Раніше тут стояв селектор `.modal-ver .ver-list`, який мав давати
     списку версій власний скрол. Але UiModal НЕ приймає клас на свій
     кореневий контейнер (.ui-modal) — приймає лише bodyClass для .ui-body.
     Класу .modal-ver ніде в DOM не було, тому весь блок правил нижче
     ніколи не застосовувався: список версій не мав overflow:auto,
     і при довгому списку скролилась ВСЯ модалка (.ui-overlay/.ui-modal)
     разом з правою колонкою (журналом змін), замість того щоб скролитись
     лише список зліва у своїх межах.

     Фікс: усі правила прив'язані до bodyClass="ver-modal-body", який
     дійсно потрапляє в DOM (клас на .ui-body), тож :global(...) селектори
     нижче реально working. */
  :global(.ui-body.ver-modal-body) {
    padding: 0; overflow: hidden; display: flex; flex-direction: column; min-height: 0;
  }
  .ver-modal-inner { flex: 1; min-height: 0; display: flex; flex-direction: column; }
  .ver-split { display: grid; grid-template-columns: 260px 1fr; gap: 0; flex: 1; min-height: 0; }
  :global(.ver-modal-body .ver-list) {
    padding: 12px; overflow-y: auto; border-right: 1px solid var(--border);
    gap: 4px;
    /* min-height:0 — обов'язково для grid-елемента: без нього рядок grid
       не може стиснутись нижче контенту, і overflow-y:auto НЕ працює
       (список версій не скролився взагалі). */
    min-height: 0;
  }
  :global(.ver-modal-body .ver-row) { cursor: pointer; }
  :global(.ver-modal-body .ver-row.active) { border-color: var(--orange); background: rgba(255,138,0,.08); }
  .vr-type {
    font-size: 9px; font-weight: 800; letter-spacing: .3px; text-transform: uppercase;
    padding: 2px 6px; border-radius: 5px; background: rgba(16,185,129,.15); color: var(--green);
    flex-shrink: 0;
  }
  .vr-type.beta { background: rgba(245,158,11,.15); color: var(--yellow); }
  .vr-type.alpha { background: rgba(239,68,68,.15); color: var(--red); }

  .ver-detail { padding: 16px; overflow-y: auto; display: flex; flex-direction: column; gap: 10px; min-height: 0; }
  .vd-head { display: flex; align-items: center; justify-content: space-between; gap: 10px; }
  .vd-ver-row { display: flex; align-items: center; gap: 8px; }
  .vd-ver { font-size: 16px; font-weight: 800; color: var(--text); font-family: var(--font-mono); }
  .vd-date { font-size: 11.5px; color: var(--text-dim); }
  .vd-meta { display: flex; gap: 6px; flex-wrap: wrap; }
  .vd-changelog-label { font-size: 11px; font-weight: 700; color: var(--text-mute); text-transform: uppercase; letter-spacing: .3px; margin-top: 4px; }
  .vd-changelog {
    font-size: 12.5px; color: var(--text); line-height: 1.6; white-space: pre-wrap;
    background: var(--bg-input); border: 1px solid var(--border); border-radius: var(--r-sm);
    padding: 10px 12px; max-height: 220px; overflow-y: auto;
  }
  .vd-warn {
    display: flex; align-items: center; gap: 6px; font-size: 12px; color: var(--yellow);
    background: rgba(245,158,11,.1); border: 1px solid rgba(245,158,11,.3);
    border-radius: var(--r-sm); padding: 8px 10px; margin-top: auto;
  }
  .src-badge { display: inline-flex; align-items: center; gap: 4px; margin-left: 8px; padding: 1px 8px;
    border-radius: 999px; font-size: 11px; font-weight: 600; vertical-align: middle;
    background: color-mix(in srgb, var(--violet) 22%, transparent); color: var(--violet); }
  .src-badge.gone { background: color-mix(in srgb, var(--orange) 25%, transparent); color: var(--orange); }
  .toggle.locked { opacity: .5; cursor: not-allowed; }
</style>
