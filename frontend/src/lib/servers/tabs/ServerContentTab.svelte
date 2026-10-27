<script lang="ts">
  // ── Вкладка "Моди" / "Плагіни" сторінки сервера ────────────────────────
  // Менеджер контенту сервера, зроблений 1:1 за менеджером модів ЗБІРКИ
  // (PackModsSection.svelte): той самий тулбар (пошук + фільтр + сортування
  // + дії), той самий рядок-віджет (.mod-row: чекбокс вибору → іконка з
  // бейджем джерела → назва з бейджами → мета → перемикач/дії), та сама
  // панель масових дій і та сама дроп-зона для .jar. Раніше тут був
  // «тонкий» список із двома кнопками — саме через це менеджер сервера
  // виглядав і працював інакше за менеджер збірки (запит: «усі функції
  // менеджера модів збірок мають бути і в менеджері модів і плагінів
  // сервера, а дизайн віджета — 1 в 1»).
  //
  // Додатково — кнопка «Знайти онлайн»: відкриває повноцінний «Браузер
  // контенту» (ContentBrowser.svelte) з фільтром на версію MC і ядро
  // САМЕ цього сервера, встановлення — тим самим шляхом, що й для збірок
  // (сторінка проєкту → App.InstallServerBrowserVersion, бекенд:
  // ServerBrowserContext / InstallServerBrowserVersion).
  import { untrack } from 'svelte'
  import { App } from '../../wails'
  import { toast, confirmDialog } from '../../toast.svelte'
  import { t } from '../../i18n.svelte'
  import SelectField from '../../SelectField.svelte'
  import UiModal from '../../UiModal.svelte'
  import ModListExportDialog from '../../ModListExportDialog.svelte'
  import ModSourcesDialog from '../../ModSourcesDialog.svelte'
  import ServerCompareDialog from '../components/ServerCompareDialog.svelte'
  import type { ServerModInfo, ModEntry, ServerBrowserContextView, BrowserVersionView } from '../../types'
  import modrinthIcon from '../../../assets/images/Modrinth_icon.png'
  import curseforgeIcon from '../../../assets/images/curseforge_icon.png'

  // ── «Знайти онлайн» ───────────────────────────────────────────────────
  // РАНІШЕ тут був власний саморобний онлайн-пошук (окремий mode,
  // власний runSearch/results/installFromBrowser) — простий список карток
  // без сторінки проєкту, автора, категорій, сортування чи пагінації,
  // повністю відірваний від повноцінного «Браузера контенту»
  // (ContentBrowser.svelte). Тепер кнопка «Знайти онлайн» відкриває
  // ТОЙ САМИЙ браузер, що і для збірок, із serverContext — той самий
  // функціонал (сторінка проєкту з описом/галереєю/автором/версіями),
  // просто зафільтрований під конкретний сервер, замість дублювання
  // усього цього в окремій куцій вкладці.
  let {
    serverId = '',
    kind = 'mods',
    running = false,
    serverDir = '',
    onSearchOnline = null,
  }: {
    serverId?: string
    kind?: 'mods' | 'plugins'
    running?: boolean
    serverDir?: string
    // onSearchOnline — відкрити «Браузер контенту» з фільтром на цей
    // сервер (App.svelte: navigate('browser') + serverContext). Опційно —
    // якщо не задано, кнопки «Знайти онлайн» просто не показуються (краще
    // приховати дію, ніж дати кнопку, що нічого не робить).
    onSearchOnline?: ((serverId: string, kind: 'mods' | 'plugins') => void) | null
  } = $props()

  const isMods = $derived(kind === 'mods')
  const label = $derived(isMods ? t('servers.tabs.mods') : t('servers.tabs.plugins'))
  // kindWord — слово «моди»/«плагіни» з малої літери для підказок у
  // реченні («Це ядро не підтримує моди»), окремо від label (заголовок).
  const kindWord = $derived(isMods ? t('servers.content.wordMods') : t('servers.content.wordPlugins'))
  const folder = $derived(isMods ? 'mods' : 'plugins')

  // ── Єдина модель рядка для модів і плагінів ──
  // Плагіни з бекенду приходять тоншими (ServerPluginInfo: немає
  // іконки/джерела/оновлень), але UI мусить бути ІДЕНТИЧНИМ — тому
  // зводимо обидва джерела до одного типу Row.
  type Row = {
    fileName: string
    name: string
    version: string
    enabled: boolean
    iconData?: string
    iconUrl?: string
    author?: string
    sizeText: string
    source?: string
    projectId?: string
    // projectSlug — slug проєкту на Modrinth/CurseForge (з перевірки
    // оновлень): з нього будується посилання «Копіювати посилання» в меню.
    projectSlug?: string
    hasUpdate?: boolean
    latestVersion?: string
    incompatible?: boolean
    incompatibleReason?: string
    environment?: string
    installedDate?: string
    // «Єдине джерело»: файл походить із джерела (див. ModSourcesDialog).
    sourceKind?: 'file' | 'folder'
    sourcePath?: string
    sourceMissing?: boolean
  }

  let mods = $state<ModEntry[]>([])
  let plugins = $state<ServerModInfo[]>([])
  let loading = $state(true)
  let checkingUpdates = $state(false)
  let search = $state('')
  let filter = $state<'all' | 'updates' | 'enabled' | 'disabled' | 'incompatible'>('all')
  let sort = $state<'name' | 'date' | 'update'>('name')
  let selected = $state<Set<string>>(new Set())
  let bulkWorking = $state(false)
  let workingFile = $state('')
  let updatingAll = $state(false)
  let dragOver = $state(false)
  let fileInput: HTMLInputElement | undefined = $state()

  // ── «⋯»-меню на рядку ─────────────────────────────────────────────
  // Менеджер сервера був ЄДИНИМ, де на рядку контенту не було меню дій:
  // у збірці (PackModsSection) поруч зі switch/кошиком є «⋯» з «показати
  // в теці» та іншими діями, а тут лишались тільки switch і кошик. Тепер
  // набір дій такий самий.
  let menuFor = $state('')
  function toggleRowMenu(fileName: string, e?: Event) {
    e?.stopPropagation()
    menuFor = menuFor === fileName ? '' : fileName
  }
  function closeRowMenu() { menuFor = '' }

  function fmtSize(mb: number): string {
    if (mb >= 1) return `${mb.toFixed(1)} ${t('units.mb')}`
    return `${Math.max(1, Math.round(mb * 1024))} ${t('units.kb')}`
  }

  const rows = $derived<Row[]>(isMods
    ? mods.map(m => ({
        fileName: m.fileName, name: m.name, version: m.version, enabled: m.enabled,
        iconData: m.iconData, iconUrl: m.iconUrl, author: m.author,
        sizeText: fmtSize((m.fileSize ?? 0) / (1024 * 1024)),
        source: m.source, projectId: m.projectId, projectSlug: m.projectSlug, hasUpdate: m.hasUpdate,
        latestVersion: m.latestVersion, incompatible: m.incompatible,
        incompatibleReason: m.incompatibleReason, environment: m.environment,
        installedDate: m.installedDate,
        sourceKind: m.sourceKind, sourcePath: m.sourcePath, sourceMissing: m.sourceMissing,
      }))
    : plugins.map(p => ({
        fileName: p.fileName, name: p.name, version: p.version, enabled: p.enabled,
        iconData: undefined, iconUrl: undefined, author: undefined,
        sizeText: fmtSize(p.sizeMB ?? 0), hasUpdate: false,
        sourceKind: p.sourceKind, sourcePath: p.sourcePath, sourceMissing: p.sourceMissing,
      })))

  // Технічний суфікс ".disabled" (внутрішній формат вимкненого файлу) у
  // підписі не показуємо — стан видно бейджем, а з суфіксом довге ім'я
  // обрізається саме на межі «*.jar».
  // Знімаємо ВСІ хвостові ".disabled" (не один): у файлів, колись зіпсованих
  // багаторазовим toggle ("X.jar.disabled.disabled"), інакше в підписі
  // лишався б «подвійний .disable».
  function displayFileName(fileName: string): string {
    return fileName.replace(/(?:\.disabled)+$/i, '')
  }

  const disabledCount = $derived(rows.filter(r => !r.enabled).length)
  const updatesCount = $derived(rows.filter(r => r.hasUpdate).length)
  const incompatibleCount = $derived(rows.filter(r => r.incompatible).length)

  const filtered = $derived.by(() => {
    let list = rows
    if (search) {
      const q = search.toLowerCase()
      list = list.filter(r => r.name.toLowerCase().includes(q) || r.fileName.toLowerCase().includes(q))
    }
    switch (filter) {
      case 'updates': list = list.filter(r => r.hasUpdate); break
      case 'enabled': list = list.filter(r => r.enabled); break
      case 'disabled': list = list.filter(r => !r.enabled); break
      case 'incompatible': list = list.filter(r => r.incompatible); break
    }
    const arr = [...list]
    if (sort === 'name') arr.sort((a: Row, b: Row) => a.name.localeCompare(b.name))
    else if (sort === 'date') arr.sort((a: Row, b: Row) => String(b.installedDate || '').localeCompare(String(a.installedDate || '')))
    else arr.sort((a: Row, b: Row) => Number(!!b.hasUpdate) - Number(!!a.hasUpdate) || a.name.localeCompare(b.name))
    return arr
  })

  const selectedRows = $derived(rows.filter(r => selected.has(r.fileName)))

  let sourcesOpen = $state(false)

  async function load() {
    loading = true
    try {
      if (isMods) mods = (await App.ListServerMods(serverId)) ?? []
      else plugins = (await App.ListServerPlugins(serverId)) ?? []
    } catch (e: any) { toast(String(e), 'error'); mods = []; plugins = [] }
    finally { loading = false }
    // Оновлення — окремою мережевою хвилею ПІСЛЯ показу списку з диска:
    // відкриття вкладки не чекає на Modrinth.
    if (isMods) checkUpdates()
  }

  async function checkUpdates() {
    if (checkingUpdates || !isMods) return
    checkingUpdates = true
    try { mods = (await App.CheckServerModUpdates(serverId)) ?? mods }
    catch { /* мережа впала — картки просто без бейджа оновлення */ }
    finally { checkingUpdates = false }
  }

  // untrack — щоб load()/checkUpdates() не робили ефект залежним від
  // loading/checkingUpdates і не перезапускали його по колу.
  $effect(() => {
    const id = serverId
    const k = kind
    if (!id) return
    void k
    selected = new Set()
    untrack(() => { load(); loadContext() })
  })

  // ── Дії з встановленим контентом ─────────────────────────────────────
  // ── Захист від змін під час запущеного сервера ───────────────────────
  //
  // Раніше вкладка модів сервера НЕ мала жодного захисту (лише напис
  // «сервер запущено»), і видалення/оновлення увімкненого мода під час
  // роботи давало криптову помилку Windows «файл зайнятий іншим
  // процесом». Але забороняти все теж неправильно: ВИМКНЕНИЙ мод сервер не
  // завантажує зовсім, тож його файл ніким не тримається — змінювати,
  // оновлювати й навіть видаляти його на льоту безпечно (саме це й просив
  // користувач: «включити його можна навіть з запущеним сервером, бо
  // вимкнені моди не працюють»). Тому блокуємо ЛИШЕ руйнівні дії над
  // УВІМКНЕНИМ файлом, а перемикач доступний завжди.
  function rowLocked(row: Row): boolean {
    return running && row.enabled
  }
  function guardLocked(row: Row): boolean {
    if (!rowLocked(row)) return true
    toast(t('servers.content.lockEnabled'), 'info')
    return false
  }

  async function toggle(row: Row) {
    // Вимкнення УВІМКНЕНОГО мода під час роботи сервера дозволене: java уже
    // завантажила свій jar у пам'ять і файл більше не читає, а перейменування
    // в .disabled діє лише на наступний запуск.
    workingFile = row.fileName
    try {
      if (isMods) await App.ToggleServerMod(serverId, row.fileName)
      else await App.ToggleServerPlugin(serverId, row.fileName)
      await load()
      toast(row.enabled ? t('servers.content.toggledOff', { name: row.name }) : t('servers.content.toggledOn', { name: row.name }), 'success')
    } catch (e: any) { toast(String(e), 'error') }
    finally { workingFile = '' }
  }

  // ── Додаткові дії рядка (меню «⋯») ─────────────────────────────────
  // «Показати в теці» — відкриває mods/ або plugins/ цього сервера
  // (serverDir приходить зі сторінки сервера, тож шлях будується так
  // само, як у кнопці тулбара).
  function revealInFolder() {
    openFolder()
  }

  async function copyFileName(row: Row) {
    try {
      await App.CopyToClipboard(displayFileName(row.fileName))
      toast(t('servers.content.fileNameCopied'), 'success')
    } catch (e: any) { toast(String(e), 'error') }
  }

  // «Копіювати посилання» — посилання на СТОРІНКУ ПРОЄКТУ (як у менеджері
  // модів збірки). Будується зі slug, який приходить разом із перевіркою
  // оновлень (Modrinth/CurseForge); якщо проєкт не знайдено — чесно кажемо
  // це, замість копіювання порожнього рядка.
  function projectUrl(row: Row): string {
    const slug = row.projectSlug
    if (!slug) return ''
    if (row.source === 'curseforge') return `https://www.curseforge.com/minecraft/mc-mods/${slug}`
    if (row.source === 'modrinth') return `https://modrinth.com/${isMods ? 'mod' : 'plugin'}/${slug}`
    return ''
  }

  async function copyProjectLink(row: Row) {
    const url = projectUrl(row)
    if (!url) { toast(t('servers.content.noLink'), 'info'); return }
    try {
      await App.CopyToClipboard(url)
      toast(t('mods.copyLinkDone'), 'success')
    } catch (e: any) { toast(String(e), 'error') }
  }

  async function remove(row: Row) {
    if (!guardLocked(row)) return
    const proceed = await confirmDialog(
      t('servers.content.deleteTitle', { label, name: row.name }),
      t('servers.content.deleteBody', { file: displayFileName(row.fileName), folder }),
      { confirmLabel: t('servers.content.delete'), cancelLabel: t('servers.content.cancel'), danger: true },
    )
    if (!proceed) return
    workingFile = row.fileName
    try {
      if (isMods) await App.DeleteServerMod(serverId, row.fileName)
      else await App.DeleteServerPlugin(serverId, row.fileName)
      await load()
      toast(t('servers.content.removed'), 'success')
    } catch (e: any) { toast(String(e), 'error') }
    finally { workingFile = '' }
  }

  // Оновлення одного мода — те саме встановлення з Браузера, що й для
  // сторінки проєкту: source+projectId відомі після CheckServerModUpdates.
  async function updateOne(row: Row, silent = false): Promise<boolean> {
    if (!row.hasUpdate || !row.projectId || !row.source) {
      if (!silent) toast(t('servers.content.noProjectData'), 'error')
      return false
    }
    if (!guardLocked(row)) return false
    workingFile = row.fileName
    try {
      await App.InstallServerBrowserVersion(serverId, row.source, row.projectId, '', kind, true)
      if (!silent) toast(t('servers.content.updatedOne', { name: row.name }), 'success')
      return true
    } catch (e: any) {
      toast(`«${row.name}»: ${String(e)}`, 'error')
      return false
    } finally { workingFile = '' }
  }

  async function updateAll() {
    if (updatingAll) return
    const list = rows.filter(r => r.hasUpdate && r.projectId && r.source)
    if (list.length === 0) { toast(t('servers.content.noProjectsToUpdate'), 'error'); return }
    updatingAll = true
    let done = 0
    try {
      for (const r of list) {
        if (await updateOne(r, true)) done++
      }
      toast(done > 0 ? t('servers.content.updateAllDone', { n: done }) : t('servers.content.updateAllNone'), done > 0 ? 'success' : 'error')
    } finally {
      updatingAll = false
      await load()
    }
  }

  // ── Вибір і масові дії ───────────────────────────────────────────────
  function toggleSelect(fileName: string) {
    const next = new Set(selected)
    if (next.has(fileName)) next.delete(fileName)
    else next.add(fileName)
    selected = next
  }
  function selectAllVisible() {
    selected = new Set([...selected, ...filtered.map(r => r.fileName)])
  }
  function clearSelection() { selected = new Set() }

  async function bulkSetEnabled(enabled: boolean) {
    const list = selectedRows.filter(r => r.enabled !== enabled && !r.sourceKind)
    if (list.length === 0) { clearSelection(); return }
    bulkWorking = true
    let ok = 0
    try {
      for (const r of list) {
        try {
          if (isMods) await App.ToggleServerMod(serverId, r.fileName)
          else await App.ToggleServerPlugin(serverId, r.fileName)
          ok++
        } catch { /* продовжуємо — одна помилка не має зупиняти решту */ }
      }
      toast(enabled ? t('servers.content.bulkEnabled', { n: ok }) : t('servers.content.bulkDisabled', { n: ok }), ok > 0 ? 'success' : 'error')
    } finally {
      bulkWorking = false
      clearSelection()
      await load()
    }
  }

  async function bulkDelete() {
    const list = selectedRows.filter(r => !r.sourceKind)
    if (list.length === 0) return
    const proceed = await confirmDialog(
      t('servers.content.bulkDeleteTitle', { label, n: list.length }),
      list.map(r => `• ${displayFileName(r.fileName)}`).join('\n'),
      { confirmLabel: t('servers.content.delete'), cancelLabel: t('servers.content.cancel'), danger: true },
    )
    if (!proceed) return
    bulkWorking = true
    let ok = 0
    try {
      for (const r of list) {
        try {
          if (isMods) await App.DeleteServerMod(serverId, r.fileName)
          else await App.DeleteServerPlugin(serverId, r.fileName)
          ok++
        } catch { /* продовжуємо */ }
      }
      toast(t('servers.content.bulkDeleted', { n: ok }), ok > 0 ? 'success' : 'error')
    } finally {
      bulkWorking = false
      clearSelection()
      await load()
    }
  }

  // ── Додавання .jar з диска + drag&drop ───────────────────────────────
  async function uploadFiles(files: File[]) {
    let added = 0
    for (const f of files) {
      if (!f.name.toLowerCase().endsWith('.jar')) {
        toast(t('servers.content.skippedNotJar', { name: f.name }), 'error')
        continue
      }
      try {
        const buf = await f.arrayBuffer()
        const bytes = new Uint8Array(buf)
        let bin = ''
        const chunk = 0x8000
        for (let i = 0; i < bytes.length; i += chunk) {
          bin += String.fromCharCode.apply(null, Array.from(bytes.subarray(i, i + chunk)) as number[])
        }
        await App.AddServerModFile(serverId, kind, f.name, btoa(bin))
        added++
        toast(t('servers.content.added', { name: f.name }), 'success')
      } catch (err: any) { toast(t('servers.content.uploadError', { name: f.name, err: String(err) }), 'error') }
    }
    if (added > 0) { await load() }
  }

  async function onFilesPicked(e: Event) {
    const input = e.target as HTMLInputElement
    const files = input.files
    input.value = ''
    if (!files || files.length === 0) return
    await uploadFiles(Array.from(files))
  }

  async function onDrop(e: DragEvent) {
    e.preventDefault()
    dragOver = false
    const files = e.dataTransfer?.files
    if (!files || files.length === 0) return
    await uploadFiles(Array.from(files))
  }

  function openFolder() {
    const sep = serverDir.includes('\\') ? '\\' : '/'
    const path = serverDir ? `${serverDir}${serverDir.endsWith(sep) ? '' : sep}${folder}` : folder
    App.OpenFolder(path).catch((e: any) => toast(t('servers.content.openFolderError', { err: String(e) }), 'error'))
  }

  // ── «Знайти онлайн»: контекст сумісності сервера ────────────────────
  // ctx/browseLoader/browseUnsupported лишаються тут (а не переїжджають у
  // App.svelte) — потрібні одразу при рендері тулбара/порожнього стану
  // (задизейблити чи приховати кнопку «Знайти онлайн», якщо ядро сервера
  // взагалі не підтримує моди/плагіни — напр. чистий vanilla), а не лише
  // в момент кліку.
  let ctx = $state<ServerBrowserContextView | null>(null)

  async function loadContext() {
    try { ctx = await App.ServerBrowserContext(serverId) }
    catch { ctx = null }
  }

  const browseLoader = $derived(isMods ? (ctx?.modLoader || '') : (ctx?.pluginLoader || ''))
  const browseUnsupported = $derived(!!ctx && browseLoader === '')

  function searchOnline() {
    if (browseUnsupported) return
    onSearchOnline?.(serverId, kind)
  }

  // ── Зміна версії встановленого контенту ───────────────────────────
  // Те саме, що кнопка «версії» в менеджері модів ЗБІРКИ: вибір іншої
  // версії того ж проєкту і перевстановлення файлу. Раніше на сервері
  // можна було лише «оновити до останньої» — жодного способу поставити
  // конкретну версію (напр. відкотитись на попередню) не існувало.
  //
  // Версії беремо через GetBrowserProjectVersions (той самий провайдер,
  // що й сторінка проєкту в Браузері контенту) і ставимо через
  // InstallServerBrowserVersion з явним versionID — бекенд-метод уже
  // приймає його (порожній рядок = «найкраща сумісна», як в оновленні).
  let versionRow = $state<Row | null>(null)
  let versionOpts = $state<BrowserVersionView[]>([])
  let versionLoading = $state(false)
  let installingVersion = $state(false)
  let selectedVersionKey = $state('')

  // Змінити версію можна лише коли відомо проєкт-джерело (source+projectId)
  // — це дані з перевірки оновлень Modrinth. Локальні jar та плагіни
  // (бекенд сканує plugins/ тонким списком, без метаданих проєкту) такої
  // інформації не мають, тож кнопки в них не буде — краще її не показувати,
  // ніж дати клік, що веде в нікуди.
  function canChangeVersion(row: Row): boolean {
    return !!row.projectId && !!row.source && row.source !== 'local' && row.source !== 'unknown'
  }

  // Експорт списку й порівняння серверів — діалоги, що відкриваються з
  // тулбара (експорт — той самий ModListExportDialog, що для збірок, але
  // з serverId: бекенд сканує mods/ цього сервера).
  let exportOpen = $state(false)
  let compareOpen = $state(false)

  async function openVersion(row: Row) {
    if (!canChangeVersion(row)) return
    if (!guardLocked(row)) return
    versionRow = row
    versionOpts = []
    selectedVersionKey = ''
    versionLoading = true
    try {
      const projectType = isMods ? 'mod' : 'plugin'
      const list = (await App.GetBrowserProjectVersions(
        row.source!, row.projectId!, projectType, ctx?.mcVersion ?? '', browseLoader,
      )) || []
      versionOpts = list
      const current = list.find(v => v.versionNumber === row.version || v.name === row.version)
      selectedVersionKey = current?.id ?? list[0]?.id ?? ''
    } catch (e: any) {
      toast(String(e), 'error')
      versionRow = null
    } finally {
      versionLoading = false
    }
  }

  async function installVersion() {
    if (!versionRow || installingVersion) return
    const opt = versionOpts.find(v => v.id === selectedVersionKey)
    if (!opt) return
    const row = versionRow
    installingVersion = true
    try {
      await App.InstallServerBrowserVersion(serverId, row.source!, row.projectId!, opt.id, kind, true)
      toast(t('servers.content.versionInstalled', { name: row.name, v: opt.versionNumber || opt.name }), 'success')
      versionRow = null
      await load()
    } catch (e: any) {
      toast(`«${row.name}»: ${String(e)}`, 'error')
    } finally {
      installingVersion = false
    }
  }

  const selectedVersion = $derived(versionOpts.find(v => v.id === selectedVersionKey) ?? null)
  const selectedIsCurrent = $derived(
    !!selectedVersion && !!versionRow &&
    (selectedVersion.versionNumber === versionRow.version || selectedVersion.name === versionRow.version)
  )
</script>

<!-- Клік будь-де поза меню закриває «⋯»-меню рядка (сама кнопка меню і
     саме меню зупиняють спливання, тому вони його не закривають). -->
{#if sourcesOpen}
  <ModSourcesDialog {serverId} {kind} onclose={() => sourcesOpen = false} onchanged={() => load()} />
{/if}

<svelte:window onclick={closeRowMenu} />

<div class="mods" style="--pcol:var(--pcol, var(--orange))">
  <!-- ═══ Тулбар: один компактний рядок — пошук + фільтри зліва,
       дії праворуч як іконки з tooltip (менше рядків = більше місця
       списку, критично коли модів/плагінів сотні). ═══ -->
  <div class="mods-toolbar">
    <div class="mt-search">
      <i class="ti ti-search"></i>
      <input placeholder={isMods ? t('mods.search') : t('servers.content.searchPlugins')} bind:value={search} />
    </div>
    <SelectField
      value={filter}
      options={[
        { value: 'all', label: t('servers.content.filterAll') },
        { value: 'updates', label: t('servers.content.filterUpdates') },
        { value: 'enabled', label: t('servers.content.filterEnabled') },
        { value: 'disabled', label: t('servers.content.filterDisabled') },
        { value: 'incompatible', label: t('servers.content.filterIncompatible') },
      ]}
      onchange={(v: string) => { filter = v as typeof filter }}
      minWidth="132px"
      ariaLabel={t('servers.content.filter')}
    />
    <SelectField
      value={sort}
      options={[
        { value: 'name', label: t('mods.sortName') },
        { value: 'date', label: t('mods.sortDate') },
        { value: 'update', label: t('mods.sortUpdate') },
      ]}
      onchange={(v: string) => { sort = v as typeof sort }}
      minWidth="132px"
      ariaLabel={t('servers.content.sort')}
    />
    <div class="mt-actions">
      {#if isMods}
        <button class="btn btn-primary btn-sm" onclick={updateAll} disabled={updatesCount === 0 || updatingAll} title={t('mods.updateAllTitle', { n: updatesCount })}>
          {#if updatingAll}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-refresh"></i>{/if} {t('mods.updateAll')}
          {#if updatesCount > 0}<span class="upd-badge">{updatesCount}</span>{/if}
        </button>
      {/if}
      {#if onSearchOnline}
        <button class="btn btn-ghost btn-sm icon-tb" onclick={searchOnline} disabled={browseUnsupported} title={browseUnsupported ? t('servers.content.noSupport', { kind: kindWord }) : t('mods.searchOnline') + ' · ' + kindWord}>
          <i class="ti ti-world-search"></i>
        </button>
      {/if}
      <input bind:this={fileInput} type="file" accept=".jar" multiple style="display:none" onchange={onFilesPicked} />
      <button class="btn btn-ghost btn-sm" onclick={() => sourcesOpen = true} title={t('modsrc.title')}>
        <i class="ti ti-link"></i> {t('modsrc.button')}
      </button>
      <button class="btn btn-ghost btn-sm icon-tb" onclick={() => fileInput?.click()} title={t('servers.content.addFile')}>
        <i class="ti ti-file-plus"></i>
      </button>
      {#if isMods}
        <!-- Кнопка з ПІДПИСОМ (не сама лише іконка) — так само, як «Порівняти
             збірки» в менеджері модів збірки: функція має бути помітною,
             а не ховатись в icon-only тулбарі. -->
        <button class="btn btn-ghost btn-sm" onclick={() => compareOpen = true} title={t('servers.content.compareServers')}>
          <i class="ti ti-git-compare"></i> {t('servers.content.compareServers')}
        </button>
      {/if}
      <button class="btn btn-ghost btn-sm icon-tb" onclick={() => exportOpen = true} title={t('servers.content.exportList')}>
        <i class="ti ti-file-export"></i>
      </button>
      <button class="btn btn-ghost btn-sm icon-tb" onclick={openFolder} title={t('mods.openFolder')}>
        <i class="ti ti-folder"></i>
      </button>
      <button class="btn btn-ghost btn-sm icon-tb" onclick={() => load()} title={t('mods.refresh')}>
        <i class="ti ti-refresh"></i>
      </button>
    </div>
  </div>

  <!-- ═══ Статус-рядок ═══ -->
  <div class="mods-status">
    {#if loading}
      <span class="ms-item"><i class="ti ti-loader spin"></i> {t('mods.scanning')}</span>
    {:else}
      <span class="ms-item">{isMods ? t('servers.content.countMods', { n: rows.length }) : t('servers.content.countPlugins', { n: rows.length })}</span>
        {#if updatesCount > 0}<span class="ms-item acc"><i class="ti ti-arrow-up"></i> {t('mods.withUpdates', { n: updatesCount })}</span>{/if}
        {#if disabledCount > 0}<span class="ms-item dim">{t('mods.disabledCount', { n: disabledCount })}</span>{/if}
        {#if incompatibleCount > 0}<span class="ms-item err"><i class="ti ti-alert-triangle"></i> {incompatibleCount} · {t('servers.content.incompatible')}</span>{/if}
      {/if}
      {#if checkingUpdates}<span class="ms-item acc"><i class="ti ti-loader spin"></i> {t('mods.checkingOne')}</span>{/if}
      {#if running}<span class="ms-item err"><i class="ti ti-alert-triangle"></i> {t('servers.content.runningHint')}</span>{/if}
    </div>

    <div
      class="mods-dropzone"
      class:drag={dragOver}
      role="list"
      ondragover={(e: DragEvent) => { e.preventDefault(); dragOver = true }}
      ondragleave={() => { dragOver = false }}
      ondrop={onDrop}
    >
      {#if loading && rows.length === 0}
        <div class="mods-empty"><i class="ti ti-loader spin"></i> {t('mods.scanning')}</div>
      {:else if rows.length === 0}
        <div class="mods-empty-folder">
          <div class="mef-ic"><i class="ti {isMods ? 'ti-puzzle' : 'ti-plug'}"></i></div>
          <div class="mef-title">{t('servers.content.emptyFolder', { folder })}</div>
          <div class="mef-sub">{t('servers.content.emptyHint')}</div>
          <div class="mef-actions">
            <button class="btn btn-primary" onclick={() => fileInput?.click()}><i class="ti ti-file-plus"></i> {t('servers.content.addFile')}</button>
            {#if onSearchOnline}
              <button class="btn btn-ghost" onclick={searchOnline} disabled={browseUnsupported}><i class="ti ti-world-search"></i> {t('mods.searchOnline')}</button>
            {/if}
            <button class="btn btn-ghost" onclick={openFolder}><i class="ti ti-folder"></i> {t('mods.openFolder')}</button>
          </div>
        </div>
      {:else if filtered.length === 0}
        <div class="mods-empty"><i class="ti ti-search"></i> {t('servers.content.nothingFound')}</div>
      {:else}
        <div class="mods-list">
          {#each filtered as r (r.fileName)}
            <div class="mod-row" class:disabled={!r.enabled} class:upd={r.hasUpdate} class:incompat={r.incompatible} class:selected={selected.has(r.fileName)} class:menu-open={menuFor === r.fileName}>
              <button
                type="button"
                role="checkbox"
                aria-checked={selected.has(r.fileName)}
                class="mod-check"
                class:checked={selected.has(r.fileName)}
                onclick={() => toggleSelect(r.fileName)}
                aria-label={t('mods.select')}
              ><i class="ti ti-check"></i></button>

              <div class="mod-ic">
                {#if r.iconData}<img src={r.iconData} alt="" loading="lazy" />
                {:else if r.iconUrl}<img src={r.iconUrl} alt="" loading="lazy" />
                {:else}<i class="ti {isMods ? 'ti-puzzle' : 'ti-plug'}"></i>{/if}
                {#if r.source === 'modrinth'}
                  <span class="mod-src" title="Modrinth"><img src={modrinthIcon} alt="Modrinth" loading="lazy" /></span>
                {:else if r.source === 'curseforge'}
                  <span class="mod-src" title="CurseForge"><img src={curseforgeIcon} alt="CurseForge" loading="lazy" /></span>
                {/if}
              </div>

              <div class="mod-info">
                <div class="mod-name">
                  <span title={r.fileName}>{r.name}</span>
                  {#if workingFile === r.fileName}
                    <i class="ti ti-loader spin mod-checking" title={t('servers.content.working')}></i>
                  {:else if r.hasUpdate}
                    <span class="badge upd" title={t('servers.content.updateAvailable', { v: r.latestVersion ?? '' })}>
                      <i class="ti ti-arrow-up"></i> {r.version ? r.version + ' → ' : ''}{r.latestVersion}
                    </span>
                  {/if}
                  {#if !r.enabled}<span class="badge off">{t('mods.off')}</span>{/if}
                  {#if r.incompatible}
                    <span class="badge incompat" title={r.incompatibleReason || t('servers.content.incompatibleHint')}>
                      <i class="ti ti-plug-x"></i> {t('servers.content.incompatible')}
                    </span>
                  {/if}
                  {#if r.environment === 'client'}
                    <span class="badge client" title={t('servers.content.clientOnlyHint')}>
                      <i class="ti ti-device-desktop"></i> {t('servers.content.clientOnly')}
                    </span>
                  {/if}
                </div>
                {#if r.sourceKind}
                  <span class="src-badge" class:gone={r.sourceMissing} title={r.sourceMissing ? t('modsrc.sourceGone') : r.sourcePath}>
                    <i class="ti ti-link"></i> {r.sourceKind === 'folder' ? t('modsrc.fromFolder') : t('modsrc.fromFile')}
                  </span>
                {/if}
                <div class="mod-meta">
                  {displayFileName(r.fileName)}{#if r.author} · {r.author}{/if}{#if r.version} · {r.version}{/if} · {r.sizeText}
                </div>
              </div>

              <div class="mod-acts">
                {#if r.hasUpdate && !r.sourceKind}
                  <button class="icon-btn upd" onclick={() => updateOne(r)} disabled={!!workingFile} title={t('mods.updateOne')}>
                    <i class="ti ti-refresh"></i>
                  </button>
                {/if}
                <div
                  class="toggle"
                  class:on={r.enabled}
                  role="switch"
                  aria-checked={r.enabled}
                  tabindex="0"
                  class:locked={!!r.sourceKind}
                  onclick={() => r.sourceKind ? toast(t('modsrc.lockedHint'), 'info') : toggle(r)}
                  onkeydown={(e: KeyboardEvent) => { if (e.key === 'Enter' || e.key === ' ') { if (r.sourceKind) toast(t('modsrc.lockedHint'), 'info'); else toggle(r) } }}
                  title={r.enabled ? t('mods.disable') : t('mods.enable')}
                ></div>
                {#if canChangeVersion(r)}
                  <button class="icon-btn" onclick={() => openVersion(r)} disabled={!!workingFile} title={t('mods.changeVersion')}>
                    <i class="ti ti-versions"></i>
                  </button>
                {/if}
                <button class="icon-btn del" onclick={() => remove(r)} disabled={!!r.sourceKind} title={t('mods.delete')}><i class="ti ti-trash"></i></button>
                <div class="row-menu-wrap">
                  <button class="icon-btn" onclick={(e: MouseEvent) => toggleRowMenu(r.fileName, e)} title={t('mods.moreActions')} aria-label={t('mods.moreActions')} aria-expanded={menuFor === r.fileName}>
                    <i class="ti ti-dots-vertical"></i>
                  </button>
                  {#if menuFor === r.fileName}
                    <div class="row-menu" onclick={(e: MouseEvent) => e.stopPropagation()}>
                      {#if canChangeVersion(r)}
                        <button onclick={() => { closeRowMenu(); openVersion(r) }}>
                          <i class="ti ti-versions"></i> {t('mods.changeVersion')}
                        </button>
                      {/if}
                      <button onclick={() => { closeRowMenu(); revealInFolder() }}>
                        <i class="ti ti-folder"></i> {t('mods.revealInFolder')}
                      </button>
                      <button onclick={() => { closeRowMenu(); copyProjectLink(r) }}>
                        <i class="ti ti-link"></i> {t('servers.content.copyLink')}
                      </button>
                      <button onclick={() => { closeRowMenu(); copyFileName(r) }}>
                        <i class="ti ti-copy"></i> {t('servers.content.copyFileNameMenu')}
                      </button>
                      {#if r.hasUpdate}
                        <button onclick={() => { closeRowMenu(); updateOne(r) }} disabled={!!workingFile}>
                          <i class="ti ti-refresh"></i> {t('mods.updateOne')}
                        </button>
                      {/if}
                      <div class="row-menu-sep"></div>
                      <button class="danger" onclick={() => { closeRowMenu(); remove(r) }}>
                        <i class="ti ti-trash"></i> {t('mods.delete')}
                      </button>
                    </div>
                  {/if}
                </div>
              </div>
            </div>
          {/each}
        </div>
      {/if}
    </div>

    <!-- ═══ Панель масових дій ═══ -->
    {#if selected.size > 0}
      <div class="bulk-bar">
        <div class="bulk-icons">
          {#each selectedRows.slice(0, 3) as r (r.fileName)}
            <div class="bulk-ic">
              {#if r.iconData}<img src={r.iconData} alt="" />
              {:else if r.iconUrl}<img src={r.iconUrl} alt="" />
              {:else}<i class="ti {isMods ? 'ti-puzzle' : 'ti-plug'}"></i>{/if}
            </div>
          {/each}
        </div>
        <span class="bulk-count">{t('mods.selectedCount', { n: selected.size })}</span>
        <div class="mt-spacer"></div>
        <button class="btn btn-ghost btn-sm" onclick={() => bulkSetEnabled(true)} disabled={bulkWorking} title={t('mods.enable')}><i class="ti ti-player-play"></i> {t('mods.enable')}</button>
        <button class="btn btn-ghost btn-sm" onclick={() => bulkSetEnabled(false)} disabled={bulkWorking} title={t('mods.disable')}><i class="ti ti-player-pause"></i> {t('mods.disable')}</button>
        <button class="btn btn-ghost btn-sm icon-tb" onclick={selectAllVisible} disabled={bulkWorking} title={t('mods.selectAllVisible')}><i class="ti ti-square-check"></i></button>
        <button class="btn btn-ghost btn-sm bulk-del" onclick={bulkDelete} disabled={bulkWorking}><i class="ti ti-trash"></i> {t('mods.delete')}</button>
        <button class="bulk-clear" onclick={clearSelection} title={t('mods.clearSelection')} aria-label={t('mods.clearSelection')}><i class="ti ti-x"></i></button>
      </div>
    {/if}
</div>

<!-- ═══ Модалка «Змінити версію» ═══
     Той самий контракт, що й у менеджері модів збірки: список версій,
     поточна позначена, встановлення обраної з перезаписом файлу. -->
{#if versionRow}
  {@const vr = versionRow}
  <UiModal
    open
    title={t('mods.versionTitle', { name: vr.name })}
    icon="ti-versions"
    color="var(--orange)"
    width="620px"
    onclose={() => { if (!installingVersion) versionRow = null }}
    locked={installingVersion}
  >
    <div class="ver-inner">
      <div class="ver-hint">
        {t('servers.content.versionHint', {
          mc: ctx?.mcVersion || '—',
          loader: browseLoader ? ' · ' + browseLoader : '',
          cur: vr.version || '—',
        })}
      </div>
      <!-- Вимкнений мод: версію міняти можна навіть під час роботи сервера,
           і після зміни він лишається вимкненим (ServerContentTab зберігає
           стан сам через суфікс .disabled). -->
      {#if !vr.enabled}
        <div class="ver-note"><i class="ti ti-power"></i> {t('servers.content.versionDisabledNote')}</div>
      {/if}
      {#if versionLoading}
        <div class="mods-empty"><i class="ti ti-loader spin"></i> {t('servers.content.versionLoading')}</div>
      {:else if versionOpts.length === 0}
        <div class="mods-empty"><i class="ti ti-versions"></i> {t('servers.content.versionNone')}</div>
      {:else}
        <div class="ver-list">
          {#each versionOpts as v (v.id)}
            {@const isCurrent = v.versionNumber === vr.version || v.name === vr.version}
            <button
              type="button"
              class="ver-row"
              class:active={v.id === selectedVersionKey}
              class:current={isCurrent}
              onclick={() => selectedVersionKey = v.id}
            >
              <div class="ver-info">
                <div class="ver-num">{v.versionNumber || v.name}</div>
                <div class="ver-meta">
                  {#if v.versionType}<span class="ver-chip">{v.versionType}</span>{/if}
                  {#if v.datePublished}<span class="ver-chip">{String(v.datePublished).slice(0, 10)}</span>{/if}
                </div>
              </div>
              {#if isCurrent}<span class="ver-cur">{t('servers.content.versionCurrent')}</span>{/if}
            </button>
          {/each}
        </div>
      {/if}
    </div>
    {#snippet footer()}
      {#if installingVersion}<span class="ver-working"><i class="ti ti-loader spin"></i> {t('servers.content.versionInstalling')}</span>{/if}
      <button class="btn btn-ghost" onclick={() => versionRow = null} disabled={installingVersion}>{t('servers.content.cancel')}</button>
      <button
        class="btn btn-primary"
        onclick={installVersion}
        disabled={installingVersion || versionLoading || !selectedVersion || selectedIsCurrent}
      >
        {#if installingVersion}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-download"></i>{/if}
        {t('servers.content.versionInstall', { v: selectedVersion?.versionNumber ?? '' })}
      </button>
    {/snippet}
  </UiModal>
{/if}

<!-- Експорт списку модів сервера — той самий діалог, що для збірок -->
{#if exportOpen}
  <ModListExportDialog
    {serverId}
    open={exportOpen}
    selectedFileNames={[...selected]}
    onClose={() => exportOpen = false}
  />
{/if}

<!-- Порівняння модів з іншим сервером -->
{#if compareOpen}
  <!-- serverName не передаємо: діалог сам читає список серверів і показує
       актуальну назву (вона може бути змінена на вкладці «Налаштування»). -->
  <ServerCompareDialog {serverId} onclose={() => compareOpen = false} />
{/if}

<style>
  .mods { display: flex; flex-direction: column; height: 100%; min-height: 0; }

  /* ── Тулбар: один рядок, переноситься лише на дуже вузьких вікнах ── */
  .mods-toolbar {
    display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
    margin: -4px -4px 8px; padding: 8px 4px 10px;
  }
  .mt-actions { display: flex; align-items: center; gap: 6px; margin-left: auto; flex-shrink: 0; }
  .icon-tb { width: 32px; height: 32px; padding: 0; justify-content: center; flex-shrink: 0; }
  .mt-search {
    display: flex; align-items: center; gap: 8px;
    background: var(--bg-input); border: 1px solid var(--border); border-radius: var(--r-sm, 7px);
    padding: 0 11px; height: 32px; flex: 1 1 200px; min-width: 140px;
  }
  .mt-search i { color: var(--text-dim); font-size: 14px; }
  .mt-search input { flex: 1; background: none; border: none; outline: none; color: var(--text); font-size: 12.5px; font-family: inherit; }
  .mt-spacer { flex: 1; }
  .mt-chip {
    display: inline-flex; align-items: center; gap: 5px;
    font: 600 10.5px/1 var(--font-mono); color: var(--text-mute);
    background: var(--hover); padding: 4px 9px; border-radius: 999px;
  }
  .upd-badge {
    display: inline-flex; align-items: center; justify-content: center;
    min-width: 18px; height: 18px; padding: 0 5px; border-radius: 999px;
    background: rgba(255, 255, 255, .22); font-size: 10.5px; font-weight: 800; margin-left: 2px;
  }

  /* ── Статус ── */
  .mods-status { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; font-size: 11.5px; color: var(--text-mute); min-height: 16px; margin-bottom: 8px; }
  .ms-item { display: inline-flex; align-items: center; gap: 5px; }
  .ms-item.acc { color: var(--orange); font-weight: 600; }
  .ms-item.dim { color: var(--text-dim); }
  .ms-item.err { color: var(--red); }

  .mods-dropzone { position: relative; border-radius: var(--r-md, 10px); transition: background .15s; flex: 1; min-height: 0; overflow-y: auto; padding-right: 4px; }
  .mods-dropzone.drag { background: rgba(var(--orange-rgb), .06); outline: 2px dashed rgba(var(--orange-rgb), .5); outline-offset: 4px; }

  .mods-list { display: flex; flex-direction: column; gap: 8px; }
  .mod-row {
    content-visibility: auto; contain-intrinsic-size: auto 70px;
    display: flex; align-items: center; gap: 14px;
    padding: 12px 16px; border-radius: var(--r-md, 10px);
    background: var(--bg-panel); border: 1px solid var(--border);
    transition: border-color .15s; position: relative; z-index: 0;
  }
  .mod-row:hover { border-color: var(--border-hi); }
  .mod-row.upd { border-color: rgba(var(--orange-rgb), .4); }
  .mod-row.disabled { opacity: .55; }
  .mod-row.selected { border-color: rgba(var(--orange-rgb), .5); background: rgba(var(--orange-rgb), .05); }
  .mod-row.incompat { border-color: rgba(239, 68, 68, .55); }

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

  .mod-ic {
    position: relative; width: 40px; height: 40px; min-width: 40px; border-radius: 10px;
    background: var(--bg-input); border: 1px solid var(--border);
    display: flex; align-items: center; justify-content: center;
    font-size: 18px; color: var(--violet-l, var(--pcol)); overflow: visible;
  }
  .mod-ic img { width: 100%; height: 100%; object-fit: cover; border-radius: 9px; }
  .mod-src {
    position: absolute; right: -6px; bottom: -6px; width: 17px; height: 17px; border-radius: 50%;
    overflow: hidden; display: flex; align-items: center; justify-content: center;
    border: 2px solid var(--bg-panel); background: var(--bg-panel);
  }
  .mod-src img { width: 100%; height: 100%; object-fit: cover; display: block; }

  .mod-info { flex: 1; min-width: 0; }
  .mod-name {
    font-size: 13px; font-weight: 700; color: var(--text);
    display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
  }
  .mod-checking { font-size: 12px; color: var(--text-mute); }
  .mod-meta {
    font-size: 10.5px; color: var(--text-mute); margin-top: 2px; font-family: var(--font-mono);
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .mod-desc {
    font-size: 11px; color: var(--text-mute); margin-top: 3px;
    display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden;
  }

  .badge { font-size: 9px; font-weight: 700; padding: 2px 7px; border-radius: 5px; font-family: var(--font-mono); letter-spacing: .3px; display: inline-flex; align-items: center; gap: 3px; }
  .badge.upd { background: rgba(16, 185, 129, .15); color: var(--green); }
  .badge.off { background: var(--bg-input); color: var(--text-mute); border: 1px solid var(--border); }
  .badge.incompat { background: rgba(239, 68, 68, .15); color: #ef4444; }
  .badge.client { background: rgba(245, 158, 11, .15); color: var(--yellow); }
  .badge.ok { background: rgba(34, 197, 94, .15); color: #22c55e; }

  .mod-acts { display: flex; align-items: center; gap: 4px; flex-shrink: 0; }
  .icon-btn {
    width: 30px; height: 30px; border-radius: 7px; background: transparent; border: none; cursor: pointer;
    display: flex; align-items: center; justify-content: center; color: var(--text-mute); font-size: 15px;
    transition: background .15s, color .15s;
  }
  .icon-btn:hover { background: rgba(255, 255, 255, .08); color: var(--text); }
  .icon-btn.upd { color: var(--green); }
  .icon-btn.upd:hover { background: rgba(16, 185, 129, .12); }
  .icon-btn.del:hover { background: rgba(239, 68, 68, .12); color: var(--red); }
  .icon-btn:disabled { opacity: .5; cursor: default; }

  .toggle {
    position: relative; width: 42px; height: 24px; min-width: 42px;
    background: var(--bg-input); border-radius: 14px; cursor: pointer;
    transition: background .2s; border: 1px solid var(--border);
  }
  .toggle::after {
    content: ''; position: absolute; top: 2px; left: 3px; width: 18px; height: 18px;
    border-radius: 50%; background: var(--text-mute); transition: transform .22s, background .22s;
    box-shadow: 0 2px 4px rgba(0, 0, 0, .4);
  }
  .toggle.on { background: rgba(var(--orange-rgb), .25); border-color: rgba(var(--orange-rgb), .4); }
  .toggle.on::after { transform: translateX(18px); background: var(--orange); box-shadow: 0 2px 8px rgba(var(--orange-rgb), .5); }

  .mods-empty {
    display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 14px;
    padding: 64px 10px; color: var(--text-mute); font-size: 13.5px; text-align: center;
  }
  .mods-empty i { font-size: 48px; color: var(--text-dim); }
  .mods-empty i.spin { font-size: 30px; }

  .mods-empty-folder {
    display: flex; flex-direction: column; align-items: center; text-align: center;
    gap: 8px; padding: 44px 24px; margin: 4px 0;
    background: color-mix(in srgb, var(--bg-card) 55%, transparent);
    border: 1px dashed var(--border-hi); border-radius: var(--r-lg, 14px);
  }
  .mef-ic {
    width: 52px; height: 52px; border-radius: 14px; display: flex; align-items: center; justify-content: center;
    font-size: 24px; color: var(--orange);
    background: color-mix(in srgb, var(--orange) 12%, transparent);
  }
  .mef-title { font-size: 14px; font-weight: 700; color: var(--text); }
  .mef-sub { font-size: 11.5px; color: var(--text-mute); max-width: 420px; line-height: 1.5; }
  .mef-actions { display: flex; gap: 8px; flex-wrap: wrap; justify-content: center; margin-top: 4px; }

  .bulk-bar {
    position: sticky; bottom: 8px; z-index: 15;
    display: flex; align-items: center; gap: 10px; flex-wrap: wrap;
    margin-top: 10px;
    padding: 8px 12px; background: var(--bg-panel); border: 1px solid rgba(var(--orange-rgb), .4);
    border-radius: var(--r-md, 10px); box-shadow: 0 6px 20px rgba(0, 0, 0, .35);
  }
  .bulk-icons { display: flex; align-items: center; flex-shrink: 0; }
  .bulk-ic {
    width: 26px; height: 26px; border-radius: 7px; overflow: hidden;
    background: var(--bg-input); border: 2px solid var(--bg-panel);
    display: flex; align-items: center; justify-content: center;
    font-size: 12px; color: var(--text-dim); margin-left: -8px;
  }
  .bulk-ic:first-child { margin-left: 0; }
  .bulk-ic img { width: 100%; height: 100%; object-fit: cover; }
  .bulk-clear {
    width: 24px; height: 24px; border-radius: 6px; background: transparent; border: none; cursor: pointer;
    display: flex; align-items: center; justify-content: center; color: var(--text-mute); flex-shrink: 0;
  }
  .bulk-clear:hover { background: rgba(255, 255, 255, .08); color: var(--text); }
  .bulk-count { font-size: 12.5px; font-weight: 700; color: var(--text); white-space: nowrap; }
  .bulk-del:hover { background: rgba(239, 68, 68, .12); color: var(--red); }

  /* ── «⋯»-меню на рядку ── */
  .row-menu-wrap { position: relative; }
  .row-menu {
    position: absolute; top: calc(100% + 4px); right: 0; z-index: 2147483000;
    display: flex; flex-direction: column; min-width: 210px;
    background: var(--bg-panel); border: 1px solid var(--border-hi); border-radius: var(--r-md, 10px);
    box-shadow: 0 8px 24px rgba(0, 0, 0, .4); padding: 4px; gap: 1px;
  }
  .row-menu button {
    display: flex; align-items: center; gap: 8px; width: 100%;
    padding: 8px 10px; background: transparent; border: none; border-radius: var(--r-sm, 6px);
    color: var(--text); font-size: 12.5px; text-align: left; cursor: pointer;
  }
  .row-menu button:hover { background: var(--hover); }
  .row-menu button.danger { color: var(--red); }
  .row-menu button.danger:hover { background: rgba(239, 68, 68, .12); }
  .row-menu button:disabled { opacity: .5; cursor: default; }
  .row-menu-sep { height: 1px; background: var(--border); margin: 3px 2px; }

  /* content-visibility:auto у .mod-row дає paint containment — absolute-меню
     обрізалося б рамками рядка. Для рядка з відкритим меню вимикаємо
     containment і підіймаємо його над сусідніми (той самий прийом, що в
     PackModsSection). */
  .mod-row.menu-open {
    z-index: 50;
    content-visibility: visible;
    contain: none;
  }

  /* ── Модалка версій ── */
  .ver-inner { display: flex; flex-direction: column; gap: 10px; }
  .ver-note {
    display: flex; align-items: center; gap: 6px; margin: 6px 0 2px;
    padding: 7px 10px; border-radius: 8px;
    background: rgba(234, 179, 8, .08); border: 1px solid rgba(234, 179, 8, .28);
    color: var(--text-mute); font: 500 11px/1.4 var(--font-ui);
  }
  .ver-note i { color: #eab308; }
  .ver-hint { font-size: 12px; color: var(--text-mute); line-height: 1.5; }
  .ver-hint b { color: var(--text); }
  .ver-list { display: flex; flex-direction: column; gap: 6px; max-height: 46vh; overflow-y: auto; padding-right: 4px; }
  .ver-row {
    display: flex; align-items: center; gap: 12px; cursor: pointer;
    width: 100%; font: inherit; text-align: left;
    padding: 10px 12px; border-radius: var(--r-md, 10px);
    background: var(--bg-input); border: 1px solid var(--border);
    transition: border-color .15s, background .15s;
  }
  .ver-row:hover { border-color: var(--border-hi); }
  .ver-row.active { border-color: rgba(var(--orange-rgb), .55); background: rgba(var(--orange-rgb), .07); }
  .ver-row.current { border-color: rgba(34, 197, 94, .45); }
  .ver-info { flex: 1; min-width: 0; }
  .ver-num { font-size: 13px; font-weight: 700; color: var(--text); font-family: var(--font-mono); }
  .ver-meta { display: flex; gap: 5px; flex-wrap: wrap; margin-top: 3px; }
  .ver-chip {
    font: 600 9.5px/1 var(--font-mono); color: var(--text-mute);
    background: var(--hover); padding: 3px 7px; border-radius: 999px;
  }
  .ver-cur { font-size: 10px; font-weight: 700; color: var(--green); flex-shrink: 0; }
  .ver-working { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; color: var(--text-mute); margin-right: auto; }
  .src-badge { display: inline-flex; align-items: center; gap: 4px; margin: 2px 0; padding: 1px 8px;
    border-radius: 999px; font-size: 11px; font-weight: 600;
    background: color-mix(in srgb, var(--violet) 22%, transparent); color: var(--violet); }
  .src-badge.gone { background: color-mix(in srgb, var(--orange) 25%, transparent); color: var(--orange); }
  .toggle.locked { opacity: .5; cursor: not-allowed; }
</style>
