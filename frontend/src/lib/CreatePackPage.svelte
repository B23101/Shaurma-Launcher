<script lang="ts">
  // ── Сторінка "Нова збірка" ────────────────────────────────────────────
  // Дві вкладки:
  //  · «Вручну» — вибір версії Minecraft + лоадера (як у старому лаунчері),
  //    RAM-повзунок, візуал картки. Збірка одразу "створена і завантажена":
  //    вікно прогресу з'являється лише коли при запуску качаються компоненти
  //    Minecraft, а не при самому створенні.
  //  · «Імпортувати» — .mrpack (Modrinth) чи .zip (CurseForge/Prism):
  //    після вибору файлу показуємо розпізнані версію/лоадер/кількість модів,
  //    тоді ті самі RAM/візуал поля, і кнопка "Імпортувати" переносить на
  //    сторінку збірки, де прогрес довантаження виглядає 1:1 як у Shaurma.
  import { onMount } from 'svelte'
  import { t } from './i18n.svelte'
  import { App, on } from './wails'
  import { toast, confirmDialog } from './toast.svelte'
  import UiModal from './UiModal.svelte'
  import { packAsset } from './packAssets.svelte'
  import { sortVersionsDesc } from './flexver'
  import SelectField from './SelectField.svelte'
  import VersionPickerModal from './VersionPickerModal.svelte'
  import MigrationPanel from './MigrationPanel.svelte'
  import AppearanceEditor from './AppearanceEditor.svelte'
  import type { CustomLoader, MCVersionEntry, LoaderVersionEntry, DetectedPack, Settings, LaunchProgress, ModMigrationReport, MigrationPack, MigrationItem, ShaurmbundlePreview } from './types'
  import { launchStageLabel, launchStagePct, launchStageColor, downloadStageLabel, downloadStageColor, downloadStageIcon, downloadStageGlow } from './launchStages'
  import vanillaIcon from '../assets/images/vanilla_icon.png'
  import fabricIcon from '../assets/images/fabric_icon.png'
  import forgeIcon from '../assets/images/forge_icon.png'
  import neoforgeIcon from '../assets/images/neoforge_icon.png'
  import quiltIcon from '../assets/images/quilt_icon.png'

  // Іконка-IMG конкретного лоадера для стадії "loader" — те саме, що
  // loaderImgFor у App.svelte (показуємо значок САМЕ обраного лоадера).
  function loaderImgFor(loaderType?: string): string | undefined {
    switch (loaderType) {
      case 'fabric': return fabricIcon
      case 'forge': return forgeIcon
      case 'neoforge': return neoforgeIcon
      case 'quilt': return quiltIcon
      default: return undefined
    }
  }

  // initialTab — яку вкладку відкрити одразу ("Імпортувати" з кнопки
  // «Імпортувати» у розділі збірок, "Вручну" з кнопки «Створити збірку»).
  let { settings, initialTab = 'manual', onCancel, onCreated, folders = [] } = $props<{
    settings: Settings
    initialTab?: 'manual' | 'import'
    onCancel: () => void
    onCreated: (packId: string, folderId?: string) => void
    // Папки «Моїх збірок»: нова збірка може одразу потрапити в обрану папку
    // (або лишитись без папки, коли обрано «Без папки»).
    folders?: { id: string; name: string; color?: string }[]
  }>()

  let tab = $state<'manual' | 'import'>(initialTab)
  let submitting = $state(false)
  // Обрана папка для нової збірки ('' = без папки). Дефолт: остання папка,
  // в якій користувач уже створював збірку (зручно при серії збірок).
  let targetFolderId = $state('')

  // ── Вкладка "Вручну" ──
  let name = $state('')
  let description = $state('')
  let mcVersion = $state('')
  let loader = $state<CustomLoader>('vanilla')
  let loaderVersion = $state('')
  let showSnapshots = $state(false)
  let showOld = $state(false)

  let mcVersions = $state<MCVersionEntry[]>([])
  let loaderVersions = $state<LoaderVersionEntry[]>([])
  let mcVersionsLoading = $state(true)
  let loaderVersionsLoading = $state(false)

  // Доступні лоадери для обраної версії Minecraft (з бекенду). Лоадери БЕЗ
  // версій під цю MC ховаються з сітки — як Prism (CustomPage показує для
  // них порожній список "No versions are currently available for Minecraft
  // X"). Поки не перевірено (або мережева помилка) — показуємо всі.
  let availableLoaders = $state<CustomLoader[]>(['vanilla', 'fabric', 'forge', 'neoforge', 'quilt'])
  let availabilityChecked = $state(false)

  const filteredMCVersions = $derived(
    mcVersions.filter(v => {
      if (v.type === 'release') return true
      if (v.type === 'snapshot') return showSnapshots
      return showOld // old_beta / old_alpha
    })
  )

  // slugify — генератор ID збірки. Має точно відповідати бекендовому
  // SlugifyName (custompack/store.go). ВАЖЛИВО: приймає null/undefined і
  // не падає — раніше на undefined у .trim() ламалася вся сторінка.
  function slugify(s: string | null | undefined): string {
    let r = (s ?? "").trim()
    r = r.replace(/[<>:"/\\|?*\u0000-\u001f]+/g, "")
    r = r.replace(/[ .!~`@#$%^\&()+=\[\]{}'';,]+$/g, "")
    r = r.replace(/^[ .]+/, "")
    r = r.replace(/\s+/g, " ").trim()
    return r || "pack"
  }
  // Іконки лоадерів — ті самі, що були в старого лаунчера для кожного
  // лоадера (перенесені у assets/images), з оригінальними кольорами.
  const loaders: { id: CustomLoader; icon: string; color: string }[] = [
    { id: 'vanilla', icon: vanillaIcon, color: '#8b8b8b' },
    { id: 'fabric', icon: fabricIcon, color: '#ff8a00' },
    { id: 'forge', icon: forgeIcon, color: '#d08b5b' },
    { id: 'neoforge', icon: neoforgeIcon, color: '#ff6b35' },
    { id: 'quilt', icon: quiltIcon, color: '#9d7cff' },
  ]

  async function loadMCVersions() {
    mcVersionsLoading = true
    try {
      mcVersions = (await App.GetCustomPackMCVersions()) ?? []
      if (!mcVersion) {
        try { mcVersion = await App.GetCustomPackDefaultMCVersion() }
        catch { mcVersion = mcVersions.find(v => v.type === 'release')?.id ?? '' }
      }
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      mcVersionsLoading = false
    }
  }

  async function loadLoaderVersions() {
    if (loader === 'vanilla' || !mcVersion) { loaderVersions = []; loaderVersion = ''; return }
    loaderVersionsLoading = true
    try {
      const raw = await App.GetCustomPackLoaderVersions(loader, mcVersion)
      // Сортування у фронтенді — те саме, що Prism VersionFilterModel
      // (SortRole, DescendingOrder): найновіші перші за FlexVer.
      loaderVersions = sortVersionsDesc([...(raw ?? [])], v => v.version)
      const recommended = loaderVersions.find(v => v.recommended) ?? loaderVersions[0]
      loaderVersion = recommended?.version ?? ''
    } catch (e: any) {
      loaderVersions = []
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      loaderVersionsLoading = false
    }
  }

  // Які лоадери мають версії під обрану версію Minecraft. Якщо поточний
  // лоадер зник (недоступний для цієї MC) — автоматично перемикаємось на
  // вільний (Vanilla доступна завжди, як дефолтний radio «None» у Prism
  // CustomPage).
  async function loadLoaderAvailability() {
    if (!mcVersion) return
    try {
      const list = (await App.GetCustomPackLoaderAvailability(mcVersion)) as CustomLoader[]
      availabilityChecked = true
      const next: CustomLoader[] = list.length ? list : ['vanilla']
      availableLoaders = next.includes('vanilla') ? next : ['vanilla', ...next]
      if (!availableLoaders.includes(loader)) {
        loader = 'vanilla'
        loaderVersion = ''
        loaderVersions = []
      }
    } catch { /* мережева помилка — лишаємо всі лоадери видимими */ }
  }

  let lastLoaderKey = ''
  $effect(() => {
    const key = `${loader}:${mcVersion}`
    if (key !== lastLoaderKey) {
      lastLoaderKey = key
      loadLoaderVersions()
    }
  })

  let lastAvailKey = ''
  $effect(() => {
    if (mcVersion && lastAvailKey !== mcVersion) {
      lastAvailKey = mcVersion
      loadLoaderAvailability()
    }
  })

  loadMCVersions()

  function selectLoader(id: CustomLoader) {
    if (availableLoaders.includes(id)) loader = id
  }

  // ── RAM (повзунок оновлює значення ПІД ЧАС тягнення, а не після) ──
  const ramMin = 1024
  const ramMax = 16384
  const ramStep = 512
  let useCustomRam = $state(false)
  let ramMinValue = $state(2048)
  let ramMaxValue = $state(4096)
  let ramDragging = $state(false)
  const ramMinPct = $derived(((ramMinValue - ramMin) / (ramMax - ramMin)) * 100)
  const ramMaxPct = $derived(((ramMaxValue - ramMin) / (ramMax - ramMin)) * 100)

  function ramValueFromPoint(e: PointerEvent | MouseEvent, el: HTMLElement): number {
    const r = el.getBoundingClientRect()
    let pct = (e.clientX - r.left) / r.width
    pct = Math.max(0, Math.min(1, pct))
    return Math.round((ramMin + pct * (ramMax - ramMin)) / ramStep) * ramStep
  }

  function startRamDrag(e: PointerEvent) {
    const el = e.currentTarget as HTMLElement
    el.setPointerCapture(e.pointerId)
    ramDragging = true
    ramMaxValue = Math.max(ramMinValue + ramStep, Math.min(ramMax, ramValueFromPoint(e, el)))
  }
  function onRamDragMove(e: PointerEvent) {
    if (!ramDragging) return
    const el = e.currentTarget as HTMLElement
    ramMaxValue = Math.max(ramMinValue + ramStep, Math.min(ramMax, ramValueFromPoint(e, el)))
  }
  function stopRamDrag() { ramDragging = false }

  // ── Java ──
  let useSeparateJava = $state(false)
  let javaPath = $state('')

  async function browseJava() {
    try {
      const p = await App.BrowseFolder()
      if (p) javaPath = p
    } catch { /* діалог скасовано */ }
  }

  // ── Візуал ──
  // Палітра/«свій колір»/пресети живуть у СПІЛЬНОМУ AppearanceEditor — тут
  // лишається тільки сам стан (color/iconSrcPath/iconName), щоб форма й
  // сторінка сервера використовували один і той самий віджет.
  let color = $state('#ff8a00')
  let iconSrcPath = $state('')
  let backgroundSrcPath = $state('')
  // Пресет-іконка (Tabler): за замовчуванням пазл. Якщо користувач обрав
  // PNG (iconSrcPath) — пресет ігнорується (PNG має пріоритет).
  let iconName = $state('ti-puzzle')

  // Пресет-іконки — у спільному компоненті PresetIconPicker (категорії,
  // пошук, згортання; ti-castle прибрано — іконки не існує у шрифті).

  function selectPresetIcon(name: string) {
    iconName = name
    iconSrcPath = '' // пресет скасовує обраний PNG
  }

  async function browseIcon() {
    try {
      const p = await App.BrowseImageFile()
      if (p) { iconSrcPath = p; iconName = '' }
    } catch { /* скасовано */ }
  }
  async function browseBackground() {
    try {
      const p = await App.BrowseImageFile()
      if (p) backgroundSrcPath = p
    } catch { /* скасовано */ }
  }

  // ── Міграція модів (лише для модових лоадерів) ──
  // Як у старому лаунчері: вибираєш збірку-джерело, тиснеш «Аналіз» —
  // лаунчер шукає сумісні версії для ОБРАНОЇ версії/лоадера (не
  // автоматично — лише по кнопці) і робить вирок: які моди можна
  // перенести, які вже є, які треба оновити. Після створення збірки
  // підтверджені моди качаються в неї.
  let migrationPacks = $state<MigrationPack[]>([])
  let migrationLoaded = $state(false)
  let migrationSource = $state('')
  let migrationAnalyzing = $state(false)
  let migrationCancelling = $state(false)
  let migrationReport = $state<ModMigrationReport | null>(null)
  // Переносимо вибрані моди ЗАВЖДИ, коли є хоч один позначений рядок:
  // окремий чекбокс «перенести вибрані» прибраний як дублювання — сам
  // вибір у списку є підтвердженням (не треба — зніми галочки).
  let migratingAfter = $state(false)
  // Живий прогрес аналізу (події mods:migrate-analyze) — рядки звіту
  // з'являються по одному, поки аналіз ще йде.
  let migrationAnalyzeDone = $state(0)
  let migrationAnalyzeTotal = $state(0)
  let migrationLive = $state<MigrationItem[]>([])
  // Розгорнути повний список звіту міграції (за замовчуванням — перші 6).
  let migrationExpanded = $state(false)
  // Вибрані моди для перенесення (ключ — sourceFileName). Можна перенести
  // ЛИШЕ обрані, а не все підряд: по замовчуванню після аналізу вибрані всі
  // «можна перенести» (found/update), користувач знімає зайві галочки.
  let migrationSelected = $state<Set<string>>(new Set())
  // Конфлікт ID збірки (така вже існує): модалка пропонує переназвати або
  // підставити цифру (slug-2, slug-3…) — як у Prism (InstanceName collision).
  let idConflict = $state<{ id: string; suggested: string } | null>(null)

  // Підвантажуємо список збірок-джерел одразу (для селектора міграції).
  App.GetMigrationPacks('').then(list => {
    migrationPacks = list ?? []
    migrationLoaded = true
    const withMods = (list ?? []).find(p => p.modCount > 0)
    if (withMods) migrationSource = withMods.packId
  }).catch(() => { migrationLoaded = true })

  async function runMigrationAnalysis() {
    if (!migrationSource || migrationAnalyzing) return
    migrationAnalyzing = true
    migrationReport = null
    migrationAnalyzeDone = 0
    migrationAnalyzeTotal = 0
    migrationLive = []
    migrationExpanded = false
    migrationSelected = new Set()
    try {
      migrationReport = await App.AnalyzeModMigration(migrationSource, '', mcVersion, loader)
      // Після аналізу за замовчуванням обираємо всі «можна перенести»
      // (found/update) — користувач знімає галочки з тих, що не треба.
      migrationSelected = new Set(
        (migrationReport?.items ?? [])
          .filter(it => it.status === 'found' || it.status === 'update')
          .map(it => it.sourceFileName)
      )
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      migrationAnalyzing = false
    }
  }

  // ── Скасування аналізу в міні-картці ──
  // Аналіз тут іде з цільовою збіркою '' — скасовуємо саме його.
  async function cancelMiniAnalysis() {
    if (!migrationAnalyzing) return
    migrationCancelling = true
    try {
      await App.CancelModMigrationAnalysis('')
    } catch {
      // Аналіз міг завершитись між кліком і запитом — це не помилка.
    } finally {
      migrationCancelling = false
    }
  }

  // ── Живий прогрес аналізу міграції ──
  // Міні-картка аналізує з цільовою збіркою '' (вона ще не створена) —
  // бекенд шле події mods:migrate-analyze саме з packId ''. Фільтруємо по
  // ньому, щоб не перехопити аналіз повноцінного діалогу міграції.
  onMount(() => {
    const off = on('mods:migrate-analyze', (p: any) => {
      if (p?.packId !== '') return
      if (p.total) migrationAnalyzeTotal = p.total
      if (p.phase !== 'item' || !p.sourceFileName) return
      // Події йдуть з 6 паралельних горутин — «current» може прийти не по
      // порядку. Оновлюємо лічильник лише монотонно.
      migrationAnalyzeDone = Math.max(p.current ?? 0, migrationAnalyzeDone + 1)
      const item: MigrationItem = {
        sourceName: p.sourceName ?? '',
        sourceFileName: p.sourceFileName,
        sourceSha1: '',
        status: p.status ?? 'not-found',
        projectId: '',
        projectSlug: '',
        // Іконка/джерело/назва приходять з batch-проєктів прямо в події.
        resolvedName: p.resolvedName ?? '',
        resolvedVersion: '',
        resolvedFilename: '',
        resolvedUrl: '',
        resolvedSha1: '',
        source: p.source ?? '',
        iconUrl: p.iconUrl ?? '',
        willReplace: false,
      }
      const idx = migrationLive.findIndex(it => it.sourceFileName === p.sourceFileName)
      if (idx >= 0) {
        migrationLive = migrationLive.map((it, i) => (i === idx ? item : it))
      } else {
        migrationLive = [...migrationLive, item]
      }
    })
    return off
  })

  const migrationSourcePack = $derived(migrationPacks.find(p => p.packId === migrationSource))
  // Живі чипи під час аналізу (з migrationLive, що оновлюється по подіях).
  const migrationLiveCounts = $derived.by(() => {
    const c = { found: 0, notFound: 0, already: 0, update: 0 }
    for (const it of migrationLive) {
      if (it.status === 'found') c.found++
      else if (it.status === 'not-found') c.notFound++
      else if (it.status === 'already') c.already++
      else if (it.status === 'update') c.update++
    }
    return c
  })

  const migrationCounts = $derived.by(() => {
    const c = { found: 0, notFound: 0, already: 0, update: 0 }
    for (const it of migrationReport?.items ?? []) {
      if (it.status === 'found') c.found++
      else if (it.status === 'not-found') c.notFound++
      else if (it.status === 'already') c.already++
      else if (it.status === 'update') c.update++
    }
    return c
  })

  // Моди, які реально можна перенести (found/update) — для списку з галочками.
  const migrationTransferable = $derived(
    (migrationReport?.items ?? []).filter(it => it.status === 'found' || it.status === 'update')
  )

  function toggleMigrationSelect(key: string) {
    const next = new Set(migrationSelected)
    if (next.has(key)) next.delete(key)
    else next.add(key)
    migrationSelected = next
  }
  function toggleAllMigrationSelect() {
    if (migrationSelected.size === migrationTransferable.length) migrationSelected = new Set()
    else migrationSelected = new Set(migrationTransferable.map(it => it.sourceFileName))
  }

  // ── Створення (вкладка "Вручну") ──
  async function submitManual() {
    if (!mcVersion) { toast(t('createPack.error.mcVersionRequired'), 'error'); return }
    if (!effectiveName) { toast(t('createPack.error.nameRequired'), 'error'); return }
    // Захист від створення збірки з обраним лоадером, для якого немає
    // ЖОДНОЇ версії під поточну MC (loaderVersions порожній і loaderVersion
    // порожній) — раніше форма це дозволяла: збірка створювалась з
    // loaderVersion:"" і не могла коректно запуститись. Vanilla версії
    // не потребує (loaderVersion завжди не використовується).
    if (loader !== 'vanilla' && !loaderVersion) {
      toast(t('createPack.error.loaderVersionRequired'), 'error')
      return
    }
    const finalName = effectiveName
    // Перевірка ID ПЕРЕД створенням: якщо збірка з таким ID уже існує —
    // показуємо модалку (переназвати або автоматично підставити цифру).
    // Бекенд і так зробить унікальний ID (slug-2…), але користувач має
    // знати про конфлікт заздалегідь — як Prism показує попередження про
    // співпадіння імені інстансу.
    if (!idConflict) {
      try {
        const check = await App.CheckCustomPackID(finalName)
        if (check.exists) {
          idConflict = { id: check.id, suggested: check.suggested }
          return
        }
      } catch { /* бекенд/мережа недоступні — Create сам зробить унікальний ID */ }
    }
    await doCreatePack(finalName)
  }

  // doCreatePack — реальне створення збірки (викликається з submitManual
  // або з модалки конфлікту ID). Після створення асинхронно запускає
  // міграцію ОБРАНИХ модів (StartModMigration — без блокування) і одразу
  // переходить на сторінку збірки, де видно живий прогрес міграції.
  async function doCreatePack(finalName: string) {
    submitting = true
    try {
      const pack = await App.CreateCustomPack({
        name: finalName, description, iconSrcPath, backgroundSrcPath, icon: iconName, color,
        mcVersion, loader, loaderVersion,
        useCustomRam, minRamMb: useCustomRam ? ramMinValue : 0, maxRamMb: useCustomRam ? ramMaxValue : 0,
        useSeparateJava, javaPath,
      })
      // Після створення асинхронно качаємо ОБРАНІ моди в нову збірку
      // (found/update; already/not-found пропускаються). StartModMigration
      // не блокує: сторінка збірки відкривається одразу, прогрес міграції
      // іде подіями mods:migrate-progress (назва + іконка поточного мода).
      if (migrationReport && migrationSelected.size > 0) {
        const todo = migrationTransferable.filter(it => migrationSelected.has(it.sourceFileName))
        if (todo.length > 0) {
          migratingAfter = true
          try {
            App.StartModMigration(pack.id, todo).catch((e: any) => {
              toast(t('error.withMessage', { message: e }), 'error')
            })
          } finally {
            migratingAfter = false
          }
        }
      }
      toast(t('createPack.created'), 'success')
      onCreated(pack.id, targetFolderId)
    } catch (e: any) {
      toast(t('createPack.error.generic', { message: e }), 'error')
    } finally {
      submitting = false
    }
  }

  // ── Вкладка "Імпортувати" ──
  let importArchivePath = $state('')
  let detecting = $state(false)
  let detected = $state<DetectedPack | null>(null)
  // .shaurmbundle — НАБІР збірок в одному файлі: детектиться окремо від
  // DetectedPack (там поля однієї збірки), прев'ю показує всі збірки
  // файлу, імпорт розпаковує їх усі (RAM/візуал/назва для набору не
  // застосовуються — кожна збірка зберігає своє).
  let bundlePreview = $state<ShaurmbundlePreview | null>(null)
  let detectError = $state('')
  let importName = $state('')
  let dragOver = $state(false)
  let hasCFKey = $state(true)

  // Поточна "ціль" імпорту: або одна розпізнана збірка, або набір (.shaurmbundle).
  const importReady = $derived(!!detected || !!bundlePreview)

  App.HasCurseForgeAPIKey().then(v => hasCFKey = v).catch(() => {})

  // Прев'ю локально вибраного файлу йде через бекенд (GetPackAsset →
  // data URL), бо WebView блокує прямі file:// у <img> — це і було
  // "невідоме зображення". Той самий контракт local-file://, що для
  // карток збірок. У вкладці імпорту, поки користувач не обрав власний
  // PNG, показуємо ВБУДОВАНУ іконку модпаку (IconDataURL від бекенду).
  // Оголошено ПІСЛЯ detected (Svelte 5 компілює $derived у порядку
  // оголошення, а ця похідна залежить від detected).
  const iconPreviewUrl = $derived(
    iconSrcPath
      ? packAsset('local-file://' + iconSrcPath)
      : (tab === 'import' && detected?.iconDataUrl ? detected.iconDataUrl : '')
  )
  const backgroundPreviewUrl = $derived(backgroundSrcPath ? packAsset('local-file://' + backgroundSrcPath) : '')

  async function pickArchive() {
    try {
      const p = await App.BrowseModpackArchive()
      if (p) await detectArchive(p)
    } catch { /* скасовано */ }
  }

  function isShaurmpackPath(path: string): boolean {
    return /\.shaurmpack$/i.test(path)
  }
  function isShaurmbundlePath(path: string): boolean {
    return /\.shaurmbundle$/i.test(path)
  }

  async function detectArchive(path: string) {
    importArchivePath = path
    detecting = true
    detectError = ''
    detected = null
    bundlePreview = null
    try {
      if (isShaurmbundlePath(path)) {
        // .shaurmbundle — окремий Wails-виклик (PreviewShaurmbundle):
        // читає bundle.json з архіву БЕЗ розпаковування і повертає список
        // усіх збірок набору. Тримаємо його окремо від detected — поля
        // DetectedPack описують ОДНУ збірку, а тут їх кілька.
        const p = await App.PreviewShaurmbundle(path)
        bundlePreview = p
        if (!p.packs || p.packs.length === 0) {
          detectError = t('createPack.import.bundleEmpty')
        }
      } else if (isShaurmpackPath(path)) {
        // .shaurmpack — окремий Wails-виклик (PreviewShaurmpack), не
        // DetectModpackArchive (той парсить лише mrpack/CurseForge/generic
        // zip). Результат переводимо у той самий DetectedPack-вигляд, щоб
        // решта форми (RAM/іконка/колір/прев'ю-панель нижче) працювала без
        // змін — вони всі реагують на detected, а не на конкретний формат.
        const p = await App.PreviewShaurmpack(path)
        detected = {
          format: 'shaurmpack',
          suggestedName: p.name || p.packId,
          mcVersion: p.gameVersion,
          loader: p.loader,
          loaderVersion: p.loaderVersion ?? '',
          modCount: p.fileCount,
          hasOverrides: true,
          unresolvedCFCount: 0,
          // Вбудована іконка збірки — прев'ю показує її у формі (той
          // самий контракт iconDataUrl, що для mrpack/zip), а сам імпорт
          // відновлює її в теці збірки, якщо користувач не обрав свою.
          iconDataUrl: p.iconDataUrl ?? ''
        }
        // Персоналізація збірки (запит «при імпорті оригінальна
        // персоналізація не зберігається»): коли у .shaurmpack збережено
        // рідний колір — показуємо ЙОГО у формі за замовчуванням, а не
        // незмінний дефолт лаунчера (#ff8a00). Користувач усе одно може
        // змінити колір/іконку вручну до імпорту — тоді його вибір іде у
        // ImportShaurmpackFile як завжди.
        if (p.color) color = p.color
      } else {
        detected = await App.DetectModpackArchive(path)
      }
      // ?? '' — бекенд може повернути DetectedPack без suggestedName;
      // undefined у полі імпорту ламав би .trim() у submitImport.
      if (detected) importName = detected.suggestedName ?? ''
      // Коли в архіві є ВБУДОВАНА іконка, а користувач ще НЕ чіпав іконку
      // (стоїть дефолт ti-puzzle) — прибираємо пресет за замовчуванням.
      // Інакше він їхав у ImportShaurmpackFile/ImportModpackArchive як
      // icon='ti-puzzle' і перебивав оригінальну іконку збірки — саме
      // «лаунчер сам ставить пресет-іконку, персоналізація губиться».
      // Явний вибір користувача (клік пресета чи PNG) має пріоритет над
      // вбудованою іконкою, як завжди.
      if (detected?.iconDataUrl && !iconSrcPath && iconName === 'ti-puzzle') iconName = ''
    } catch (e: any) {
      detectError = t('createPack.import.error', { message: String(e) })
    } finally {
      detecting = false
    }
  }

  function onDrop(e: DragEvent) {
    e.preventDefault()
    dragOver = false
    const file = e.dataTransfer?.files?.[0] as any
    const path = file?.path
    if (path) detectArchive(path)
  }

  function formatLoaderLabel(l: string): string {
    switch (l) {
      case 'fabric': return 'Fabric'
      case 'forge': return 'Forge'
      case 'neoforge': return 'NeoForge'
      case 'quilt': return 'Quilt'
      default: return 'Vanilla'
    }
  }

  function formatSourceLabel(f: string): string {
    switch (f) {
      case 'mrpack': return t('createPack.import.formatMrpack')
      case 'curseforge': return t('createPack.import.formatCurseforge')
      case 'shaurmpack': return t('createPack.import.formatShaurmpack')
      default: return t('createPack.import.formatGeneric')
    }
  }

  async function submitImport() {
    if (!detected && !bundlePreview) return
    // .shaurmbundle: усі збірки набору імпортуються разом (кожна зі своїми
    // назвою/іконкою/налаштуваннями з архіву) — форми RAM/візуалу тут немає.
    if (bundlePreview) {
      submitting = true
      try {
        const res = await App.ImportShaurmbundleFile(importArchivePath, targetFolderId)
        const count = res?.packs?.length ?? bundlePreview.packCount
        toast(t('createPack.import.bundleDone', { count }), 'success')
        // Порожній packId = "лишитись у списку збірок" (не відкривати
        // сторінку однієї збірки) — папки/список оновляться у App.
        onCreated('', targetFolderId)
      } catch (e: any) {
        toast(t('createPack.error.generic', { message: e }), 'error')
      } finally {
        submitting = false
      }
      return
    }
    if (!detected) return
    // Звичайний zip (без manifest.json / modrinth.index.json) НЕ є модпаком:
    // перед імпортом обов'язково попереджаємо і питаємо підтвердження,
    // щоб користувач не переплутав «просто архів» з готовим модпаком.
    if (detected.format === 'generic') {
      const ok = await confirmDialog(
        t('createPack.import.genericConfirmTitle'),
        t('createPack.import.genericConfirmBody'),
        { confirmLabel: t('createPack.import.genericConfirmOk'), cancelLabel: t('cancel'), danger: true }
      )
      if (!ok) return
    }
    const finalName = (importName ?? '').trim() || detected.suggestedName || ''
    if (!finalName) { toast(t('createPack.error.nameRequired'), 'error'); return }
    submitting = true
    try {
      // .shaurmpack — окремий Wails-виклик (ImportShaurmpackFile), не
      // ImportModpackArchive (той конвеєр заточений під mrpack/CurseForge/
      // generic zip і не знає про internal/shaurmpack).
      const pack = detected.format === 'shaurmpack'
        ? await App.ImportShaurmpackFile(importArchivePath, finalName, color, iconSrcPath, iconName)
        : await App.ImportModpackArchive(importArchivePath, detected, finalName, color, iconSrcPath, iconName)
      toast(t('createPack.imported'), 'success')
      onCreated(pack.id, targetFolderId)
    } catch (e: any) {
      toast(t('createPack.error.generic', { message: e }), 'error')
    } finally {
      submitting = false
    }
  }

  function submit() {
    if (tab === 'manual') submitManual()
    else submitImport()
  }

  // ── Візуальний прев'ю: РЕАЛЬНА картка .inst-tile + перемикачі станів ──
  // Окрім "простих" статусів (ready/running/not-installed/needs-update),
  // реальна картка в App.svelte вміє показувати ЩЕ й стадії самого
  // запуску (lp.stage: checking → worker_update → java → loader) і
  // підстадії синхронізації файлів (prog.stage: manifest_fetched →
  // extracting → finalizing, інакше — просто "downloading"). Раніше тут
  // був один статичний прогрес-бар на 47% — тепер кожен перемикач нижче
  // відповідає РЕАЛЬНОМУ стану, який реально трапляється в бекенді.
  type PreviewState =
    | 'ready' | 'running' | 'not-installed' | 'needs-update'
    | 'downloading' | 'updating' | 'paused'
    | 'launch-checking' | 'launch-worker_update' | 'launch-java' | 'launch-loader'
    | 'sync-manifest_fetched' | 'sync-extracting' | 'sync-finalizing'

  let previewState = $state<PreviewState>('ready')
  const previewStates: { id: PreviewState; icon: string; labelKey: string }[] = [
    { id: 'ready', icon: 'ti-player-play', labelKey: 'createPack.preview.state.ready' },
    { id: 'running', icon: 'ti-player-stop', labelKey: 'createPack.preview.state.running' },
    { id: 'not-installed', icon: 'ti-download', labelKey: 'createPack.preview.state.notInstalled' },
    { id: 'needs-update', icon: 'ti-refresh', labelKey: 'createPack.preview.state.needsUpdate' },
    { id: 'downloading', icon: 'ti-download', labelKey: 'createPack.preview.state.downloading' },
    { id: 'updating', icon: 'ti-refresh', labelKey: 'createPack.preview.state.updating' },
    { id: 'paused', icon: 'ti-player-pause', labelKey: 'createPack.preview.state.paused' },
    { id: 'launch-checking', icon: 'ti-search', labelKey: 'launch.stage.checking' },
    { id: 'launch-worker_update', icon: 'ti-refresh', labelKey: 'launch.stage.workerUpdate' },
    { id: 'launch-java', icon: 'ti-coffee', labelKey: 'launch.stage.java' },
    { id: 'launch-loader', icon: 'ti-package', labelKey: 'launch.stage.loader' },
    { id: 'sync-manifest_fetched', icon: 'ti-file-download', labelKey: 'sync.stage.manifest' },
    { id: 'sync-extracting', icon: 'ti-archive', labelKey: 'sync.stage.extracting' },
    { id: 'sync-finalizing', icon: 'ti-check', labelKey: 'sync.stage.finalizing' },
  ]

  // Чи поточний прев'ю-стан — один з "lp"-стадій запуску (checking/
  // worker_update/java/loader) — саме вони керують lp/launchStage*.
  const previewLaunchStage = $derived(
    previewState.startsWith('launch-') ? previewState.slice('launch-'.length) : undefined
  )
  const previewIsLaunching = $derived(!!previewLaunchStage)
  // Синтетичний LaunchProgress — щоб реально викликати launchStageLabel/
  // launchStagePct/launchStageColor так само, як реальна картка (а не
  // дублювати їхню логіку вручну).
  const previewLp = $derived<LaunchProgress | null>(
    previewIsLaunching
      ? { instanceId: 'preview', stage: previewLaunchStage as LaunchProgress['stage'], percent: -1, message: '', done: false }
      : null
  )
  // Підстадія синхронізації файлів (manifest_fetched/extracting/
  // finalizing) — інакше downloadStageLabel впаде на дефолтний
  // "downloading" підпис, як і в реальній картці.
  const previewSyncStage = $derived(
    previewState.startsWith('sync-') ? previewState.slice('sync-'.length) : undefined
  )
  const previewIsSyncing = $derived(previewState === 'downloading' || previewState === 'updating' || !!previewSyncStage)
  const previewPaused = $derived(previewState === 'paused')
  const previewLimg = $derived(previewLaunchStage === 'loader' ? loaderImgFor(tab === 'manual' ? loader : detected?.loader) : undefined)

  // Дефолтна назва збірки: коли поле назви порожнє — формується
  // "<Лоадер> <Версія ГРИ>" (напр. "Fabric 1.20.1"), для Vanilla — сама
  // версія Minecraft. Версія лоадера (напр. "0.16.5") користувачу ні про
  // що не каже, а ось версія гри + лоадер — так.
  const suggestedName = $derived(
    tab === 'manual'
      ? (loader === 'vanilla'
          ? (mcVersion ?? '')
          : `${formatLoaderLabel(loader)} ${mcVersion ?? ''}`.trim())
      : ''
  )
  // Ефективна назва — те, що реально збережеться (введене або дефолт).
  // Завжди рядок: `|| ''` страхує від undefined у suggestedName — інакше
  // slugify нижче отримав би undefined і впав би на .trim()
  // ("Cannot read properties of undefined").
  const effectiveName = $derived((name ?? '').trim() || suggestedName || '')
  // ID збірки рахується з ЕФЕКТИВНОЇ назви (як Prism: ID генерується з
  // фактичного імені інстансу), тож при порожній назві прев'ю не показує
  // "pack", а реальний ID майбутньої збірки.
  const slugId = $derived(slugify(effectiveName))
  const previewName = $derived(tab === 'manual' ? (effectiveName || t('createPack.namePlaceholder')) : (importName || t('createPack.namePlaceholder')))
  const previewLoaderLabel = $derived(tab === 'manual' ? formatLoaderLabel(loader) : detected ? formatLoaderLabel(detected.loader) : 'Vanilla')
  const previewMc = $derived(tab === 'manual' ? mcVersion : detected?.mcVersion ?? '')
  // Точне дзеркало гілок кнопки .it-btn з App.svelte (той самий порядок
  // умов: limg → java → worker_update (spin) → checking → lp-loader
  // (spin) → running → ready → needs-update → not-installed → paused →
  // downloading/updating → фолбек spin).
  const previewBtnIcon = $derived(
    previewLimg ? undefined
      : previewLaunchStage === 'java' ? 'ti-coffee'
      : previewLaunchStage === 'worker_update' ? 'ti-refresh'
      : previewLaunchStage === 'checking' ? 'ti-search'
      : previewIsLaunching ? 'ti-loader'
      : previewState === 'running' ? 'ti-player-stop'
      : previewState === 'ready' ? 'ti-player-play'
      : previewState === 'needs-update' ? 'ti-refresh'
      : previewState === 'not-installed' ? 'ti-download'
      : previewPaused ? 'ti-player-pause'
      : previewIsSyncing ? 'ti-player-pause'
      : 'ti-loader'
  )
  // spin лише в тих самих гілках, що в реальній картці: worker_update та
  // "просто lp, без відомої стадії" (ti-loader) — НЕ java/checking, у
  // яких іконка статична.
  const previewBtnSpin = $derived(
    !previewLimg && (previewLaunchStage === 'worker_update' || (previewIsLaunching && previewLaunchStage !== 'java' && previewLaunchStage !== 'checking'))
  )
  const previewStateLabel = $derived(t(previewStates.find(s => s.id === previewState)?.labelKey ?? 'createPack.preview.state.ready'))

  // Кольоровий клас кнопки .it-btn.it-* — точне дзеркало spBtnClass()
  // з App.svelte (play/stop/download/pause/disabled), рахуємо тут
  // безпосередньо на "простих" статусах прев'ю (без lp-стадій — тоді
  // кнопку заступає it-st індикатор, і клас кольору їй не потрібен).
  const previewBtnClass = $derived(
    previewState === 'running' ? 'stop'
      : previewState === 'ready' ? 'play'
      : previewState === 'needs-update' ? 'download'
      : previewState === 'not-installed' ? 'download'
      : previewPaused ? 'play'
      : previewIsSyncing ? 'pause'
      : 'disabled'
  )
  // Нормалізована sync-стадія для кольору/іконки: явна підстадія
  // (manifest_fetched/extracting/finalizing) або дефолтна "downloading"
  // для простих станів downloading/updating без підстадії.
  const previewSyncStageResolved = $derived(previewIsSyncing ? (previewSyncStage ?? 'downloading') : undefined)
  // Індикатор стадії запуску (checking/worker_update/java/loader), що
  // заступає кнопку — 1:1 дзеркало launchStageIcon() з App.svelte.
  const previewSti = $derived(
    previewLaunchStage === 'checking' ? { icon: 'ti-search', cls: 'st-check' }
      : previewLaunchStage === 'worker_update' ? { icon: 'ti-refresh', cls: 'st-update' }
      : previewLaunchStage === 'java' ? { icon: 'ti-coffee', cls: 'st-java' }
      : previewLaunchStage === 'loader' ? { icon: 'ti-package', cls: 'st-loader' }
      : null
  )
  // Індикатор стадії СИНХРОНІЗАЦІЇ ФАЙЛІВ (downloading/manifest_fetched/
  // extracting/finalizing) — те саме, що syncStageIcon() з App.svelte,
  // окрім паузи (там лишається кнопка «продовжити»).
  const previewDsti = $derived(
    !previewSti && previewIsSyncing && !previewPaused ? downloadStageIcon(previewSyncStageResolved) : null
  )
  // «Сяйво» іконки — 1:1 дзеркало glowClass() з App.svelte, включно з
  // кольором по конкретній sync-стадії (downloadStageGlow), а не лише
  // фіксованим "качає"/"оновлює".
  const previewGlow = $derived(
    previewIsLaunching
      ? (previewLaunchStage === 'java' || previewLaunchStage === 'loader' ? 'glow-red' : previewLaunchStage === 'worker_update' ? 'glow-blue' : '')
      : previewState === 'running' ? 'glow-green'
      : previewIsSyncing ? downloadStageGlow(previewSyncStageResolved)
      : ''
  )
</script>

{#snippet previewBlock()}
  <div>
    <div class="section-label" style="margin:2px 2px 8px;display:block">{t('createPack.preview')}</div>
    <!-- ВАЖЛИВО: прев'ю нижче — це буквально розмітка й класи РЕАЛЬНОЇ
         картки "Моїх збірок" (.inst-tile — та сама, що в App.svelte у
         сітці збірок) і РЕАЛЬНОГО банера сторінки збірки (.inst-banner),
         скопійовані 1:1, а не намальовані вручну заново. Включно з
         кольоровою кнопкою дії (.it-btn.it-play/it-stop/it-download/
         it-pause), індикатором стадії запуску/синхронізації (.it-st.
         st-*, заступає кнопку під час checking/worker_update/java/
         loader ТА під час downloading/manifest_fetched/extracting/
         finalizing) і «сяйвом» іконки (.glow-green/orange/red/blue/
         yellow на .it-ic) — усе додано в App.svelte й сюди ОДНОЧАСНО,
         тими самими класами/кольорами/анімаціями, що раніше існували
         лише в рядку sidebar (.sp-btn, .sp-st, .sb-pack.glow-*). -->
    {#key previewState}
    <div class="inst-tile pv-tile" class:running={previewState === 'running'} class:glow-green={previewGlow === 'glow-green'} class:glow-orange={previewGlow === 'glow-orange'} class:glow-red={previewGlow === 'glow-red'} class:glow-blue={previewGlow === 'glow-blue'} class:glow-yellow={previewGlow === 'glow-yellow'} style="--pcol:{color}">
      <div class="it-ic" class:has-icon={!!iconPreviewUrl} class:blink={previewIsSyncing} style="--pcol:{color}">
        {#if iconPreviewUrl}
          <img src={iconPreviewUrl} alt="" />
        {:else}
          <i class="ti {iconName || 'ti-puzzle'}" style="color:var(--pcol)"></i>
        {/if}
      </div>
      <div class="it-info">
        <div class="it-name">{previewName}</div>
        <div class="it-sub" class:live={previewIsLaunching || previewIsSyncing}>
          {#if previewIsLaunching}
            <i class="ti ti-loader spin"></i>
            <span>{launchStageLabel(previewLaunchStage)}</span>
          {:else if previewIsSyncing}
            <i class="ti {previewPaused ? 'ti-player-pause' : 'ti-download'}"></i>
            <span>{downloadStageLabel(previewSyncStage)}</span>
          {:else}
            <span>{previewLoaderLabel} · {previewMc || '—'}</span>
          {/if}
        </div>
        {#if previewIsLaunching}
          <div class="it-stage"><i class="ti ti-file"></i> {t('createPack.preview.stageFileHint')}</div>
        {/if}
      </div>
      {#if previewSti}
        <div class="it-st {previewSti.cls}" title={launchStageLabel(previewLaunchStage)}>
          {#if previewLimg}
            <img src={previewLimg} alt="" />
          {:else}
            <i class="ti {previewSti.icon}"></i>
          {/if}
        </div>
      {:else if previewDsti}
        <div class="it-st {previewDsti.cls}" title={downloadStageLabel(previewSyncStageResolved)}>
          <i class="ti {previewDsti.icon}"></i>
        </div>
      {:else}
        <button class="it-btn it-{previewBtnClass}" title={previewStateLabel}>
          <i class="ti {previewBtnIcon} {previewBtnSpin ? 'spin' : ''}"></i>
        </button>
      {/if}
      {#if previewIsLaunching}
        <div class="it-prog"><div class="it-prog-fill" style="width:{launchStagePct(previewLp)}%;background:{launchStageColor(previewLaunchStage)}"></div></div>
      {:else if previewIsSyncing}
        <div class="it-prog"><div class="it-prog-fill" style="width:47%;background:{downloadStageColor(previewSyncStageResolved)}"></div></div>
      {/if}
    </div>
    {/key}
    <div class="status-pills">
      {#each previewStates as ps (ps.id)}
        <button class="pill" class:sel={previewState === ps.id} onclick={() => previewState = ps.id} title={t(ps.labelKey)}>
          <i class="ti {ps.icon}"></i> {t(ps.labelKey)}
        </button>
      {/each}
    </div>
    {#key `${color}|${iconName}|${iconSrcPath}|${backgroundSrcPath}`}
    <div class="inst-banner" style="margin-top:14px;height:110px" style:background={backgroundPreviewUrl ? undefined : `linear-gradient(135deg, color-mix(in srgb, ${color} 55%, #241a10), color-mix(in srgb, ${color} 22%, #120c06))`} style:background-image={backgroundPreviewUrl ? `url('${backgroundPreviewUrl}')` : undefined}>
      <div class="ib-title">
        <span class="ib-ic">
          {#if iconPreviewUrl}<img src={iconPreviewUrl} alt="" />{:else}<i class="ti {iconName || 'ti-puzzle'}" style="color:{color || 'var(--orange)'}"></i>{/if}
        </span>
        <div class="ib-meta">
          <div class="ib-name">{previewName}</div>
        </div>
      </div>
    </div>
    {/key}
  </div>
{/snippet}

<div class="create-page">
  <div class="tabbar">
  <button class="tab-btn" class:active={tab === 'manual'} onclick={() => tab = 'manual'}>
    <i class="ti ti-adjustments"></i> {t('createPack.tab.manual')}
  </button>
  <button class="tab-btn" class:active={tab === 'import'} onclick={() => tab = 'import'}>
    <i class="ti ti-download"></i> {t('createPack.tab.import')}
  </button>
  </div>

  {#key tab}
  <div class="create-form">
  {#if tab === 'manual'}
  <div class="create-grid">
    <div>
      <!-- ОСНОВНЕ -->
      <div class="card">
        <div class="card-head">
          <div class="ci"><i class="ti ti-id"></i></div>
          <div class="ctw"><div class="ct">{t('createPack.section.basic')}</div><div class="cd">{t('createPack.section.basic.desc')}</div></div>
          <span class="step-num">1</span>
        </div>
        <div class="row col">
          <div class="rl-name">{t('createPack.name')}</div>
          <input class="input" placeholder={tab === 'manual' ? (suggestedName || t('createPack.namePlaceholder')) : t('createPack.namePlaceholder')} bind:value={name} />
          {#if tab === 'manual' && suggestedName}
            <div class="rl-sub" style="margin-top:2px;color:var(--text-mute)">
              <i class="ti ti-info-circle"></i> {t('createPack.nameEmptyHint', { name: suggestedName })}
            </div>
          {/if}
        </div>
        <div class="row">
          <div class="row-label"><div class="rl-name">{t('createPack.id')}</div><div class="rl-sub">{t('createPack.idHint')}</div></div>
          <span class="input mono" style="width:auto;color:var(--text-mute)">{slugId}</span>
        </div>
        <div class="row col">
          <div class="rl-name">{t('createPack.description')} <span class="muted" style="font-weight:500">{t('createPack.descriptionOptional')}</span></div>
          <input class="input" placeholder={t('createPack.descriptionPlaceholder')} bind:value={description} />
        </div>
        {#if folders.length > 0}
          <div class="row">
            <div class="row-label"><div class="rl-name">{t('createPack.folder')}</div><div class="rl-sub">{t('createPack.folderHint')}</div></div>
            <SelectField
              value={targetFolderId}
              onchange={(v: string) => targetFolderId = v}
              minWidth="220px"
              options={[
                { value: '', label: t('packs.folderNone') },
                ...folders.map((f: { id: string; name: string; color?: string }) => ({ value: f.id, label: f.name, icon: 'ti-folder' as string }))
              ]}
            />
          </div>
        {/if}
      </div>

      <!-- ВЕРСІЯ ГРИ -->
      <div class="card">
        <div class="card-head">
          <div class="ci violet"><i class="ti ti-versions"></i></div>
          <div class="ctw"><div class="ct">{t('createPack.section.version')}</div><div class="cd">{t('createPack.section.version.desc')}</div></div>
          <span class="step-num">2</span>
        </div>
        <div class="row">
          <div class="row-label"><div class="rl-name">{t('createPack.mcVersion')}</div></div>
          <div class="flex gap8">
            <span class="chip" class:on={showSnapshots} onclick={() => showSnapshots = !showSnapshots} role="button" tabindex="0">{t('createPack.mcVersion.snapshot')}</span>
            <span class="chip" class:on={showOld} onclick={() => showOld = !showOld} role="button" tabindex="0">{t('createPack.mcVersion.old')}</span>
            <VersionPickerModal
              value={mcVersion}
              onselect={(v) => mcVersion = v}
              disabled={mcVersionsLoading}
              minWidth="170px"
              placeholder={mcVersionsLoading ? '…' : ''}
              title={t('createPack.mcVersion')}
              icon="ti-versions"
              ariaLabel={t('createPack.mcVersion')}
              options={filteredMCVersions.map(v => ({ value: v.id, label: v.id, sub: v.type !== 'release' ? v.type : undefined }))}
            />
          </div>
        </div>
        <div class="row col">
          <div class="rl-name">{t('createPack.loader')}</div>
          {#if availabilityChecked && availableLoaders.length < loaders.length}
            <div class="field-hint" style="margin-bottom:6px;color:var(--yellow)"><i class="ti ti-alert-triangle"></i> {t('createPack.loader.unavailableHint')}</div>
          {/if}
          <div class="loader-grid">
            {#each loaders.filter(l => availableLoaders.includes(l.id)) as l (l.id)}
              <div class="loader-opt" class:sel={loader === l.id} onclick={() => selectLoader(l.id)} role="button" tabindex="0">
                <img class="loader-ic" src={l.icon} alt="" style="--lcol:{l.color}" />
                <span class="ln">{t(`createPack.loader.${l.id}`)}</span>
                <span class="lt">{t(`createPack.loader.${l.id}.sub`)}</span>
              </div>
            {/each}
          </div>
          {#if loader !== 'vanilla'}
            <div class="flex" style="justify-content:space-between;margin-top:4px">
              <div class="rl-sub">{t('createPack.loaderVersion')}</div>
              <VersionPickerModal
                value={loaderVersion}
                onselect={(v) => loaderVersion = v}
                disabled={loaderVersionsLoading || loaderVersions.length === 0}
                minWidth="170px"
                placeholder={loaderVersionsLoading ? '…' : loaderVersions.length === 0 ? t('createPack.loaderVersion.none') : ''}
                title={t('createPack.loaderVersion')}
                icon="ti-versions"
                ariaLabel={t('createPack.loaderVersion')}
                emptyText={t('createPack.loaderVersion.none')}
                options={loaderVersions.map(v => ({
                  value: v.version,
                  label: v.version,
                  badgeIcon: v.recommended ? 'ti-star' : undefined,
                  badgeTitle: v.recommended ? t('createPack.loaderVersion.recommended') : undefined
                }))}
              />
            </div>
          {/if}
        </div>
      </div>

      <!-- МІГРАЦІЯ МОДІВ (лише для модових лоадерів) — переписаний з нуля MigrationPanel -->
      {#if loader !== 'vanilla'}
        <MigrationPanel
          packs={migrationPacks}
          packsLoaded={migrationLoaded}
          source={migrationSource}
          onSourceChange={(v: string) => {
            migrationSource = v
            migrationReport = null; migrationLive = []; migrationAnalyzeDone = 0; migrationAnalyzeTotal = 0
          }}
          analyzing={migrationAnalyzing}
          cancelling={migrationCancelling}
          onAnalyze={runMigrationAnalysis}
          onCancel={cancelMiniAnalysis}
          analyzeDone={migrationAnalyzeDone}
          analyzeTotal={migrationAnalyzeTotal}
          live={migrationLive}
          report={migrationReport}
          selected={migrationSelected}
          onToggleSelect={toggleMigrationSelect}
          onToggleAll={toggleAllMigrationSelect}
          bind:expanded={migrationExpanded}
          {submitting}
          {migratingAfter}
        />
      {/if}

      <!-- ПРОДУКТИВНІСТЬ -->
      <div class="card">
        <div class="card-head">
          <div class="ci green"><i class="ti ti-cpu"></i></div>
          <div class="ctw"><div class="ct">{t('createPack.section.perf')}</div><div class="cd">{t('createPack.section.perf.desc')}</div></div>
          <span class="step-num">3</span>
        </div>
        <div class="row">
          <div class="row-label"><div class="rl-name">{t('createPack.customRam')}</div><div class="rl-sub">{useCustomRam ? t('createPack.customRam.on') : t('createPack.customRam.off')}</div></div>
          <div class="toggle" class:on={useCustomRam} onclick={() => useCustomRam = !useCustomRam} role="button" tabindex="0"></div>
        </div>
        {#if useCustomRam}
          <div class="row col">
            <div class="flex" style="justify-content:space-between">
              <div class="rl-sub">{t('createPack.ramMin')}: <b class="mono" style="color:#fff">{ramMinValue} MB</b></div>
              <div class="rl-sub">{t('createPack.ramMax')}: <b class="mono" style="color:var(--orange)">{ramMaxValue} MB</b></div>
            </div>
            <div class="ram-track" onpointerdown={startRamDrag} onpointermove={onRamDragMove} onpointerup={stopRamDrag} onpointercancel={stopRamDrag}>
              <div class="ram-fill" style="left:{ramMinPct}%;width:{ramMaxPct - ramMinPct}%"></div>
              <div class="ram-knob" style="left:{ramMaxPct}%"></div>
            </div>
            <div class="rl-sub mono">{t('createPack.ramGlobal', { mb: settings.maxRAM, gb: (ramMax / 1024).toFixed(0) })}</div>
          </div>
        {/if}
      </div>

      <!-- JAVA -->
      <div class="card">
        <div class="card-head">
          <div class="ci"><i class="ti ti-coffee"></i></div>
          <div class="ctw"><div class="ct">{t('createPack.section.java')}</div><div class="cd">{t('createPack.section.java.desc')}</div></div>
          <span class="step-num">4</span>
        </div>
        <div class="row">
          <div class="row-label"><div class="rl-name">{t('createPack.separateJava')}</div></div>
          <div class="toggle" class:on={useSeparateJava} onclick={() => useSeparateJava = !useSeparateJava} role="button" tabindex="0"></div>
        </div>
        {#if useSeparateJava}
          <div class="row col">
            <div class="rl-name">{t('createPack.separateJavaPath')}</div>
            <div class="folder-row">
              <input class="input" bind:value={javaPath} />
              <button class="btn btn-ghost" onclick={browseJava}><i class="ti ti-folder"></i></button>
            </div>
          </div>
        {/if}
      </div>
    </div>

    <!-- ПРАВА КОЛОНКА -->
    <div class="preview-col">
      <!-- Вигляд — СПІЛЬНИЙ віджет із збіркою та сторінкою сервера. -->
      <AppearanceEditor
        title={t('createPack.section.visual')}
        step={5}
        {iconPreviewUrl}
        {iconName}
        {color}
        onBrowseIcon={browseIcon}
        onSelectPreset={selectPresetIcon}
        onColorChange={(c: string) => color = c}
        {backgroundPreviewUrl}
        onBrowseBackground={browseBackground}
        onRemoveBackground={() => backgroundSrcPath = ''}
      />

      {@render previewBlock()}
    </div>
  </div>
{:else}
  <div class="create-grid">
    <div>
      <div class="idea">
        <div class="ico"><i class="ti ti-download"></i></div>
        <div><h4>{t('createPack.import.hintTitle')}</h4><p>{t('createPack.import.hintBody')}</p></div>
      </div>

      <div class="card">
        <div class="card-head"><div class="ci"><i class="ti ti-file-zip"></i></div><div class="ctw"><div class="ct">{t('createPack.tab.import').toUpperCase()}</div></div></div>
        <div class="row col">
          <div class="dropzone" class:drag-over={dragOver}
            role="button" tabindex="0"
            onclick={pickArchive}
            ondragover={(e) => { e.preventDefault(); dragOver = true }}
            ondragleave={() => dragOver = false}
            ondrop={onDrop}>
            <i class="ti ti-cloud-upload"></i>
            <div class="rl-name">{importArchivePath ? importArchivePath.split(/[\\/]/).pop() : t('createPack.import.dropTitle')}</div>
            <div class="rl-sub">{t('createPack.import.dropSub')}</div>
          </div>
        </div>

        {#if detecting}
          <div class="row"><div class="rl-sub"><i class="ti ti-loader spin"></i> {t('createPack.import.detecting')}</div></div>
        {:else if detectError}
          <div class="row"><div class="rl-sub" style="color:var(--red)">{detectError}</div></div>
        {:else if detected}
          <div class="row col">
            <div class="detected-pack" class:warn={detected.format === 'curseforge' && !hasCFKey}>
              {#if detected.iconDataUrl}
                <img class="dp-ic" src={detected.iconDataUrl} alt="" />
              {/if}
              <i class="ti {detected.format === 'curseforge' && !hasCFKey ? 'ti-alert-triangle' : 'ti-circle-check'} ok"></i>
              <div>
                <div class="dp-name">{t('createPack.import.detected')}: {detected.suggestedName}</div>
                <div class="dp-meta">{formatSourceLabel(detected.format)} · {formatLoaderLabel(detected.loader)} {detected.mcVersion} · {t('createPack.import.modCount', { count: detected.modCount })}</div>
                {#if detected.format === 'curseforge' && !hasCFKey && detected.unresolvedCFCount > 0}
                  <div class="rl-sub" style="margin-top:6px;color:var(--yellow)">{t('createPack.import.noCfKey', { count: detected.unresolvedCFCount })}</div>
                {/if}
                {#if detected.format === 'generic'}
                  <div class="rl-sub" style="margin-top:6px;color:var(--yellow)">
                    <i class="ti ti-alert-triangle"></i> {t('createPack.import.genericWarn')}
                  </div>
                {/if}
              </div>
            </div>
          </div>
          <div class="row col">
            <div class="rl-name">{t('createPack.import.nameLabel')}</div>
            <input class="input" placeholder={t('createPack.import.namePlaceholder')} bind:value={importName} />
          </div>
        {:else if bundlePreview}
          <div class="row col">
            <div class="detected-pack">
              <i class="ti ti-package ok"></i>
              <div>
                <div class="dp-name">{t('createPack.import.bundleDetected')}</div>
                <div class="dp-meta">{t('createPack.import.bundlePacks', { count: bundlePreview.packCount })} · {t('createPack.import.bundleFiles', { count: bundlePreview.totalFiles })}</div>
                <div class="rl-sub" style="margin-top:6px">{t('createPack.import.bundleHint')}</div>
              </div>
            </div>
          </div>
          <div class="row col">
            <div class="bundle-pack-list">
              {#each bundlePreview.packs as bp (bp.packId)}
                <div class="bundle-pack-row">
                  <span class="bpi" style="--pcol:{bp.color || 'var(--orange)'}"><i class="ti ti-package"></i></span>
                  <div class="bpi-info">
                    <div class="bpi-name">{bp.name || bp.packId}</div>
                    <div class="rl-sub">{formatLoaderLabel(bp.loader)} · {bp.mcVersion || '—'} · {t('createPack.import.bundlePackFiles', { count: bp.fileCount })}</div>
                  </div>
                </div>
              {/each}
            </div>
          </div>
        {/if}
      </div>

      {#if detected}
        <div class="card">
          <div class="card-head">
            <div class="ci green"><i class="ti ti-cpu"></i></div>
            <div class="ctw"><div class="ct">{t('createPack.section.perf')}</div><div class="cd">{t('createPack.section.perf.desc')}</div></div>
          <span class="step-num">3</span>
          </div>
          <div class="row">
            <div class="row-label"><div class="rl-name">{t('createPack.customRam')}</div><div class="rl-sub">{useCustomRam ? t('createPack.customRam.on') : t('createPack.customRam.off')}</div></div>
            <div class="toggle" class:on={useCustomRam} onclick={() => useCustomRam = !useCustomRam} role="button" tabindex="0"></div>
          </div>
          {#if useCustomRam}
            <div class="row col">
              <div class="flex" style="justify-content:space-between">
                <div class="rl-sub">{t('createPack.ramMin')}: <b class="mono" style="color:#fff">{ramMinValue} MB</b></div>
                <div class="rl-sub">{t('createPack.ramMax')}: <b class="mono" style="color:var(--orange)">{ramMaxValue} MB</b></div>
              </div>
              <div class="ram-track" onpointerdown={startRamDrag} onpointermove={onRamDragMove} onpointerup={stopRamDrag} onpointercancel={stopRamDrag}>
                <div class="ram-fill" style="left:{ramMinPct}%;width:{ramMaxPct - ramMinPct}%"></div>
                <div class="ram-knob" style="left:{ramMaxPct}%"></div>
              </div>
            </div>
          {/if}
        </div>

        <AppearanceEditor
          title={t('createPack.section.visual')}
          step={5}
          {iconPreviewUrl}
          {iconName}
          {color}
          onBrowseIcon={browseIcon}
          onSelectPreset={selectPresetIcon}
          onColorChange={(c: string) => color = c}
        />
      {/if}
    </div>

    {#if detected}
      <div class="preview-col">
        {@render previewBlock()}
      </div>
    {/if}
  </div>
  {/if}
  </div>
  {/key}

<!-- Конфлікт ID збірки: така збірка вже існує → запропонувати переназвати
     або автоматично підставити цифру (slug-2, slug-3…) — як Prism. -->
{#if idConflict}
  {@const ic = idConflict}
  <UiModal
    open={true}
    size="sm"
    title={t('createPack.idConflictTitle')}
    icon="ti-alert-triangle"
    color="var(--yellow)"
    zIndex="2100000"
    locked={submitting}
    onclose={() => { if (!submitting) idConflict = null }}
  >
        <div class="rl-sub" style="margin-bottom:12px">{t('createPack.idConflictDesc', { id: ic.id })}</div>
        <div class="id-conflict-preview">
          <span>{t('createPack.idConflictWillBe')}</span>
          <b class="mono">{ic.suggested}</b>
        </div>
    {#snippet footer()}
      <button class="btn btn-ghost" onclick={() => idConflict = null}>
        <i class="ti ti-pencil"></i> {t('createPack.idConflictRename')}
      </button>
      <button class="btn btn-primary" onclick={() => { idConflict = null; doCreatePack(effectiveName) }}>
        <i class="ti ti-check"></i> {t('createPack.idConflictAuto', { id: ic.suggested })}
      </button>
    {/snippet}
  </UiModal>
{/if}

<!-- Прилипла панель дій знизу екрана — форма створення збірки довга
     (особливо вкладка «Імпортувати» з міграцією модів), і кнопки
     «Скасувати»/«Створити» вгорі доводилось шукати гортанням назад.
     Тепер вони завжди на видноті, під активною вкладкою. -->
<div class="create-footer">
  <button class="btn btn-ghost" onclick={onCancel}><i class="ti ti-x"></i> {t('createPack.cancel')}</button>
  <button class="btn btn-primary" disabled={submitting || (tab === 'import' && !importReady) || (tab === 'manual' && loader !== 'vanilla' && !loaderVersion)} onclick={submit}>
    <i class="ti {submitting ? 'ti-loader spin' : tab === 'import' ? 'ti-download' : 'ti-check'}"></i>
    {submitting ? t('createPack.submitting')
      : tab === 'import'
        ? (bundlePreview ? t('createPack.import.bundleSubmit', { count: bundlePreview.packCount }) : t('createPack.import.submit'))
        : t('createPack.submit')}
  </button>
</div>
</div>

