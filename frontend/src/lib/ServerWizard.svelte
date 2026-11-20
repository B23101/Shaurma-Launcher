<script lang="ts">
  import NumberInput from './NumberInput.svelte'
  // ── Сторінка "Новий сервер" ──────────────────────────────────────────
  // Побудована у стилі CreatePackPage.svelte ("Нова збірка") та "Моїх
  // збірок": ті самі класи card/card-head/row/row-label/step-num, той самий
  // VersionPickerModal, те саме прев'ю inst-tile/inst-banner праворуч.
  //
  // Порядок кроків — ЯК У ЗВИЧАЙНИХ ЗБІРКАХ: спочатку версія Minecraft,
  // потім ядро/лоадер (список відфільтрований під обрану версію), потім
  // версія лоадера. Це гарантує, що для обраної версії гри показуються
  // лише ядра/лоадери, які реально існують, і що install не падає на
  // несумісній парі (головна скарга: "не все становлюється").
  //
  // Velocity — це ПРОКСІ: йому байдуже, яка версія Minecraft. Тому він
  // винесений в окремий шлях "Проксі (Velocity)": вибирається лише
  // ВЕРСІЯ САМОГО ПРОКСІ (з api.papermc.io), жодного вибору версії гри.
  //
  // Дві вкладки:
  //  · «Створити» — тип (Minecraft-сервер / проксі) → версія Minecraft →
  //    ядро → версія ядра → назва/порт/RAM → EULA. Сервер одразу
  //    "створений" (тільки конфіг записано) — ядро довантажується ЛІНИВО
  //    при першому натисканні "Старт", так само як збірка не якає файли
  //    Minecraft при створенні, а лише при запуску гри.
  //  · «Додати з папки» — вказати наявну теку сервера, лаунчер сам
  //    визначає ядро/версію/порт (DetectServerCore) і реєструє її.
  import { onMount } from 'svelte'
  import { App } from './wails'
  import { toast } from './toast.svelte'
  import { t } from './i18n.svelte'
  import VersionPickerModal from './VersionPickerModal.svelte'
  import UiModal from './UiModal.svelte'
  import AppearanceEditor from './AppearanceEditor.svelte'
  import { packAsset } from './packAssets.svelte'
  import { flexverCompare } from './flexver'
  import MigrationPanel from './MigrationPanel.svelte'
  import ServerCard from './servers/components/ServerCard.svelte'
  import ServerBanner from './servers/components/ServerBanner.svelte'
  import { ModMigrationFlow } from './modMigrationFlow.svelte'
  import { serverModTarget } from './modTarget'
  import type { MCVersionEntry, NewServerRequest, CoreDetectResult, ServerView, ServerFolderCheck, NetworkFolderInfo } from './types'
  import vanillaIcon from '../assets/images/vanilla_icon.png'
  import fabricIcon from '../assets/images/fabric_icon.png'
  import forgeIcon from '../assets/images/forge_icon.png'
  import neoforgeIcon from '../assets/images/neoforge_icon.png'
  import quiltIcon from '../assets/images/quilt_icon.png'
  import papermcIcon from '../assets/images/papermc.svg'
  import purpurIcon from '../assets/images/purpur.png'

  // onCreated отримує ID сервера і launchNow: сторінка сервера, побачивши
  // launchNow=true, одразу викликає StartServer сама (щоб install-прогрес
  // почав литись у консоль без додаткового кліку користувача).
  let { onclose, onCreated } = $props<{
    onclose: () => void
    onCreated: (id: string, launchNow: boolean) => void
  }>()

  let tab = $state<'create' | 'folder'>('create')
  let submitting = $state(false)

  // ── Тип сервера ──
  // mc    — звичайний Minecraft-сервер (vanilla/paper/purpur/fabric/…):
  //          версія гри → ядро → версія ядра.
  // proxy — Velocity: проксі не прив'язаний до версії гри, вибирається
  //          лише версія самого проксі.
  let serverType = $state<'mc' | 'proxy'>('mc')

  // ── Приєднання існуючих серверів при створенні проксі ──
  // Velocity — це «платформа»: одразу при створенні можна вписати в мережу
  // наявні сервери (Paper/Purpur тощо), які ще не належать жодному проксі.
  // NetworkSave потім «склеює» їх (forwarding.secret + paper-global.yml).
  let attachCandidates = $state<ServerView[]>([])
  let attachSelected = $state<Set<string>>(new Set())
  let attachLoading = $state(false)
  async function loadAttachCandidates() {
    if (attachLoading) return
    attachLoading = true
    try {
      const list = (await App.ListServers()) ?? []
      // Сервери, що вже належать якійсь мережі (включені в network.json
      // іншого проксі), не пропонуємо — один backend не може бути у двох.
      const taken = new Set<string>()
      for (const s of list) {
        if (s.core !== 'velocity') continue
        try {
          const v = await App.NetworkGetView(s.id)
          for (const m of v.members ?? []) if (m.inNetwork) taken.add(m.serverId)
        } catch { /* ігнор */ }
      }
      // Приєднувати можна лише зупинені сервери: конфіги «клеяться» на
      // диску, а запущений процес може перезаписати їх при збереженні.
      attachCandidates = list.filter(s =>
        s.core !== 'velocity' && !taken.has(s.id) &&
        !['running', 'starting', 'stopping', 'installing'].includes(s.status),
      )
      for (const id of attachSelected) if (!attachCandidates.some(c => c.id === id)) attachSelected.delete(id)
    } finally {
      attachLoading = false
    }
  }
  $effect(() => { if (serverType === 'proxy' && tab === 'create') loadAttachCandidates() })

  // ── Ядро (для mc-типу) ──
  // Paper/Purpur/Velocity не мають готового PNG у наборі (як vanilla/
  // fabric/forge/neoforge/quilt) — використовуємо Tabler-іконку з
  // кольоровим квадратом, той самий патерн, що .ci у card-head.
  // Velocity у цьому списку НЕМАЄ — це окремий шлях serverType='proxy'.
  type CoreDef = { id: string; label: string; icon?: string; ti?: string; color: string; group: 'vanilla' | 'mod' | 'plugin' }
  const cores: CoreDef[] = [
    { id: 'vanilla', label: 'Vanilla', icon: vanillaIcon, color: '#8b8b8b', group: 'vanilla' },
    { id: 'paper', label: 'Paper', icon: papermcIcon, color: '#10b981', group: 'plugin' },
    { id: 'purpur', label: 'Purpur', icon: purpurIcon, color: '#ef4444', group: 'plugin' },
    { id: 'fabric', label: 'Fabric', icon: fabricIcon, color: '#ff8a00', group: 'mod' },
    { id: 'forge', label: 'Forge', icon: forgeIcon, color: '#d08b5b', group: 'mod' },
    { id: 'neoforge', label: 'NeoForge', icon: neoforgeIcon, color: '#ff6b35', group: 'mod' },
    { id: 'quilt', label: 'Quilt', icon: quiltIcon, color: '#9d7cff', group: 'mod' },
  ]
  let core = $state('vanilla')
  const selectedCore = $derived(cores.find(c => c.id === core) ?? cores[0])
  // Ядро має версію (Paper/Purpur білди, версія лоадера Fabric/Forge/…) —
  // Vanilla не має (офіційний server.jar без вибору версії лоадера).
  const needsCoreVersion = $derived(serverType === 'mc' && core !== 'vanilla')

  // ── Версія Minecraft (тільки mc-тип) ──
  let mcVersion = $state('')
  let mcVersions = $state<MCVersionEntry[]>([])
  let mcVersionsLoading = $state(true)
  let showSnapshots = $state(false)
  const filteredMCVersions = $derived(mcVersions.filter(v => v.type === 'release' || (v.type === 'snapshot' && showSnapshots)))

  // minCoreMCVersionOK — статична, offline межа сумісності лоадера з
  // версією Minecraft (дзеркалить minCoreMCVersionOK у backend/internal/
  // servers/versions.go — той самий факт про лоадер з обох боків). Ховає
  // явно несумісні пари (Fabric під 1.8 тощо) МИТТЄВО, ще до відповіді
  // бекенду — мережевий GetServerAvailableCores лише додатково звужує.
  function minCoreMCVersionOK(coreId: string, mc: string): boolean {
    let min = ''
    if (coreId === 'fabric' || coreId === 'quilt') min = '1.14'
    else if (coreId === 'neoforge') min = '1.20.1'
    else if (coreId === 'forge') min = '1.1'
    else return true
    return flexverCompare(mc, min) >= 0
  }
  const staticAvailableCores = $derived(
    cores.map(c => c.id).filter(id => !mcVersion || minCoreMCVersionOK(id, mcVersion))
  )

  // Ядра, доступні для обраної версії Minecraft за даними бекенду (мережа).
  // Фінальний список у розмітці — це ПЕРЕТИН зі staticAvailableCores: обидві
  // перевірки мають пропустити лоадер, щоб він показався. Ініціалізується
  // всіма ядрами (до першої відповіді бекенду).
  let networkAvailableCores = $state<string[]>(cores.map(c => c.id))
  const availableCores = $derived(staticAvailableCores.filter(id => networkAvailableCores.includes(id)))

  async function loadMCVersions() {
    mcVersionsLoading = true
    try {
      mcVersions = (await App.GetServerMCVersions()) ?? []
      if (!mcVersion) {
        try { mcVersion = await App.GetServerDefaultMCVersion() }
        catch { mcVersion = mcVersions.find(v => v.type === 'release')?.id ?? '' }
      }
    } catch (e: any) {
      toast(`Помилка завантаження версій: ${e}`, 'error')
    } finally {
      mcVersionsLoading = false
    }
  }

  async function loadAvailableCores() {
    if (!mcVersion) return
    try {
      const list = await App.GetServerAvailableCores(mcVersion)
      networkAvailableCores = list?.length ? list : cores.map(c => c.id)
    } catch {
      // Мережева помилка — не звужуємо мережевим боком; статичний фільтр
      // (staticAvailableCores) лишається єдиним, але надійним захистом.
      networkAvailableCores = cores.map(c => c.id)
    }
  }

  // ── Версія ядра (Paper build, Forge версія, версія Velocity тощо) ──
  let coreVersion = $state('')
  let coreVersions = $state<{ version: string; stable: boolean; recommended: boolean }[]>([])
  let coreVersionsLoading = $state(false)

  // Для Velocity версії беруться без прив'язки до MC: "проксі, яка версія".
  function coreVersionSource(): { coreId: string; mc: string } {
    if (serverType === 'proxy') return { coreId: 'velocity', mc: '' }
    return { coreId: core, mc: needsCoreVersion ? mcVersion : '' }
  }

  async function loadCoreVersions() {
    const { coreId, mc } = coreVersionSource()
    if (serverType === 'mc' && !needsCoreVersion) { coreVersions = []; coreVersion = ''; return }
    if (serverType === 'mc' && !mc) { coreVersions = []; coreVersion = ''; return }
    coreVersionsLoading = true
    try {
      const raw = await App.GetServerCoreVersions(coreId, mc)
      coreVersions = (raw ?? []).map(v => ({ version: v.version, stable: v.stable ?? true, recommended: v.recommended ?? false }))
      const recommended = coreVersions.find(v => v.recommended) ?? coreVersions[0]
      coreVersion = recommended?.version ?? ''
    } catch (e: any) {
      coreVersions = []
      toast(`Помилка завантаження версій ядра: ${e}`, 'error')
    } finally {
      coreVersionsLoading = false
    }
  }

  loadMCVersions()

  // Перемикання типу сервера: скидаємо неактуальний стан.
  function switchType(t: 'mc' | 'proxy') {
    if (t === serverType) return
    serverType = t
    core = t === 'proxy' ? 'velocity' : 'vanilla'
    coreVersion = ''
    coreVersions = []
  }

  let lastCoreKey = ''
  $effect(() => {
    const key = serverType === 'proxy' ? 'proxy:velocity' : `${core}:${mcVersion}`
    if (key !== lastCoreKey) {
      lastCoreKey = key
      loadCoreVersions()
    }
  })

  let lastAvailKey = ''
  $effect(() => {
    if (serverType === 'mc' && mcVersion && lastAvailKey !== mcVersion) {
      lastAvailKey = mcVersion
      loadAvailableCores()
    }
  })

  // Якщо поточний core випав зі списку доступних (через зміну mcVersion,
  // статичний фільтр чи мережеву відповідь) — одразу скидаємо на vanilla.
  $effect(() => {
    if (serverType !== 'mc') return
    if (core !== 'vanilla' && !availableCores.includes(core)) {
      core = 'vanilla'
    }
  })

  function selectCore(id: string) {
    if (availableCores.includes(id)) { core = id; coreVersion = '' }
  }

  // ── Назва / порт / RAM ──
  let name = $state('')
  const suggestedName = $derived(serverType === 'proxy' ? 'Velocity' : `${selectedCore.label} ${mcVersion || ''}`.trim())
  const effectiveName = $derived(name.trim() || suggestedName)

  // ── ID сервера (ім'я теки на диску) ──────────────────────────────────
  // Як ID збірки: гравець може написати його сам, кирилицею і з пробілами
  // («Мій Сервер з Друзями») — backend зберігає і це саме ім'я теки, а не
  // безлике «server». Порожнє поле = ID виведеться з назви. Перевіряємо
  // ЗАЗДАЛЕГІДЬ (як у діалозі дублювання збірки), щоб не отримати помилку
  // аж після натискання «Створити».
  let folderInput = $state('')
  let folderCheck = $state<ServerFolderCheck | null>(null)
  let folderChecking = $state(false)
  // autoFolder — вільний ID, виведений з назви (коли поле ID порожнє). Якщо ID з
  // назви зайнятий, додаємо « 2», « 3»… — створення не впирається в помилку.
  let autoFolder = $state('')
  let folderSeq = 0
  $effect(() => {
    const typed = folderInput.trim()
    const probe = (typed || effectiveName).trim()
    // Порожньо і назви немає — питати нічого (бекенд теж віддасть дефолт).
    if (!probe) { folderCheck = null; autoFolder = ''; return }
    const seq = ++folderSeq
    folderChecking = true
    // Невелика затримка: перевірка на кожну літеру — це зайвий виклик
    // бекенду (сканування теки серверів) на кожне натискання клавіші.
    const timer = setTimeout(async () => {
      try {
        let res = await App.CheckServerFolder(probe)
        if (!typed) {
          const base = res.folder || probe
          for (let n = 2; !res.free && n <= 99; n++) {
            if (seq !== folderSeq) return
            res = await App.CheckServerFolder(`${base} ${n}`)
          }
          if (seq !== folderSeq) return
          autoFolder = res.free ? res.folder : ''
        } else if (seq === folderSeq) {
          autoFolder = ''
        }
        if (seq === folderSeq) { folderCheck = res; folderChecking = false }
      } catch {
        if (seq === folderSeq) { folderCheck = null; autoFolder = ''; folderChecking = false }
      }
    }, 220)
    return () => { clearTimeout(timer); folderChecking = false }
  })

  // Порт НЕ питаємо в майстрі: це властивість server.properties, і єдине
  // місце, де його міняють, — вкладка «Властивості» (server-port). Тут лише
  // підказуємо бекенду бажане значення: 0 = «обери вільний» (звичайні
  // сервери), 25577 — звичний порт Velocity (якщо зайнятий, бекенд візьме
  // наступний вільний і ми покажемо це в «Властивості»).
  // svelte-ignore state_referenced_locally
  let port = $state(serverType === 'proxy' ? 25577 : 0)

  // Проксі не тримає світів: свої межі RAM і 512 МБ за замовчуванням
  // (рекомендація Velocity), а не 1–16 ГБ ігрового сервера.
  const ramMin = $derived(serverType === 'proxy' ? 256 : 1024)
  const ramMax = $derived(serverType === 'proxy' ? 4096 : 16384)
  const ramStep = $derived(serverType === 'proxy' ? 128 : 256)
  let ramMB = $state(2048)
  $effect(() => { ramMB = serverType === 'proxy' ? 512 : 2048 })
  let ramDragging = $state(false)
  const ramPct = $derived(((ramMB - ramMin) / (ramMax - ramMin)) * 100)
  let ramWarn = $state('')

  function ramValueFromPoint(e: PointerEvent, el: HTMLElement): number {
    const r = el.getBoundingClientRect()
    let pct = (e.clientX - r.left) / r.width
    pct = Math.max(0, Math.min(1, pct))
    return Math.round((ramMin + pct * (ramMax - ramMin)) / ramStep) * ramStep
  }
  function startRamDrag(e: PointerEvent) {
    const el = e.currentTarget as HTMLElement
    el.setPointerCapture(e.pointerId)
    ramDragging = true
    ramMB = Math.max(ramMin, Math.min(ramMax, ramValueFromPoint(e, el)))
  }
  function onRamDragMove(e: PointerEvent) {
    if (!ramDragging) return
    ramMB = Math.max(ramMin, Math.min(ramMax, ramValueFromPoint(e, e.currentTarget as HTMLElement)))
  }
  function stopRamDrag() { ramDragging = false }

  async function checkRAM() {
    try {
      const res = await App.CheckServerRAM(ramMB)
      ramWarn = res.ok ? '' : `Доступно лише ${(res.available / 1024).toFixed(1)} ГБ вільної памʼяті`
    } catch { /* не блокуємо форму */ }
  }
  let ramCheckTimer: ReturnType<typeof setTimeout> | undefined
  $effect(() => {
    ramMB // залежність
    clearTimeout(ramCheckTimer)
    ramCheckTimer = setTimeout(checkRAM, 300)
  })

  // ── EULA ──
  // EULA потрібна лише Minecraft-серверам (Mojang). Velocity — проксі,
  // вона не запускає server.jar і не потребує прийняття EULA Mojang.
  const eulaNeeded = $derived(serverType === 'mc')
  let eulaAccepted = $state(false)
  // EULA приймається МОДАЛКОЮ при натисканні «Створити сервер», а не
  // окремою карткою-кроком налаштування.
  let eulaModal = $state(false)
  let folderEulaModal = $state(false)
  function eulaConfirm() {
    if (folderEulaModal) {
      folderEulaModal = false
      folderEulaAccepted = true
      submitFolder()
      return
    }
    eulaModal = false
    eulaAccepted = true
    submitCreate()
  }

  // launchAfterCreate — «Створити й одразу запустити»: після CreateServer
  // одразу StartServer, і сторінка сервера відкривається вже в режимі
  // запуску — install-прогрес і старт java йдуть суцільним потоком.
  let launchAfterCreate = $state(true)

  // ── Моди з іншої збірки чи сервера (як у створенні збірки) ──
  // Той самий ModMigrationFlow і та сама панель, що в CreatePackPage. Ціль
  // "srv:" — сервер, що створюється: бекенд позначає сторону кожного мода,
  // клієнтські не переносяться, невизначені обирає користувач.
  const modCore = $derived(serverType === 'mc' && selectedCore.group === 'mod')
  const migration = new ModMigrationFlow({ target: () => serverModTarget(''), mcVersion: () => mcVersion, loader: () => core })
  let migrationExpanded = $state(false)
  migration.loadPacks('')
  onMount(() => migration.listen())

  // ── Створення ──
  async function submitCreate() {
    if (serverType === 'mc') {
      if (!mcVersion) { toast('Оберіть версію Minecraft', 'error'); return }
      if (!availableCores.includes(core)) { toast(`${selectedCore.label} не підтримує обрану версію Minecraft`, 'error'); return }
    }
    if (needsCoreVersion || serverType === 'proxy') {
      if (coreVersions.length > 0 && !coreVersion) { toast('Оберіть версію ядра', 'error'); return }
    }
    if (eulaNeeded && !eulaAccepted) { eulaModal = true; return }

    submitting = true
    try {
      const req: NewServerRequest = {
        core: serverType === 'proxy' ? 'velocity' : core,
        // Velocity: версія гри не потрібна взагалі — проксі.
        mcVersion: serverType === 'proxy' ? '' : mcVersion,
        // Velocity: версія самого проксі; vanilla: пусто (install сам
        // визначить); решта: обрана версія лоадера/білда.
        loaderVer: serverType === 'proxy' ? coreVersion : (needsCoreVersion ? coreVersion : ''),
        name: effectiveName,
        // Порожній ID = бекенд виведе теку з назви (SanitizeServerFolder).
        id: folderInput.trim() || autoFolder,
        dir: '',
        ramMB,
        port,
        acceptEula: eulaNeeded ? eulaAccepted : true,
        javaMajor: 0,
        jvmArgs: '',
        programArgs: '',
      }
      const srv = await App.CreateServer(req)
      // Обрані моди копіюємо ДО першого запуску (синхронно): інакше сервер
      // стартував би з порожньою mods/, поки йде копіювання.
      if (modCore && migration.selectedItems.length > 0) {
        try {
          const res = await App.MigrateMods(serverModTarget(srv.id), migration.selectedItems)
          if (res.failed.length > 0) toast(t('mods.migrateDoneWithErrors', { n: res.migrated.length, total: migration.selectedItems.length }), 'error')
        } catch (e: any) {
          toast(t('error.withMessage', { message: e }), 'error')
        }
      }
      // Проксі — «платформа»: одразу приєднуємо обрані сервери в мережу
      // (NetworkSave пише network.json + velocity.toml і «клеїть» forwarding).
      let attached = 0
      if (serverType === 'proxy' && attachSelected.size > 0) {
        try {
          const view = await App.NetworkGetView(srv.id)
          let order = 0
          const members = (view.members ?? []).map((m: any) => {
            const on = attachSelected.has(m.serverId)
            if (on) order += 1
            // Решта ролей (ім'я в мережі, вхід, автозапуск) — з типових
            // значень, які вже підставив NetworkGetView.
            return { ...m, inNetwork: on, order: on ? order : m.order }
          })
          await App.NetworkSave(srv.id, members)
          attached = order
        } catch (e: any) {
          toast(`Проксі створено, але приєднати сервери не вдалося: ${e}`, 'error')
        }
      }
      if (attached > 0 && launchAfterCreate) {
        // «Створити й одразу запустити» з серверами в мережі = запуск
        // УСІЄЇ мережі одним потоком (проксі перший, потім backend-и).
        App.NetworkStart(srv.id).then(() => {}).catch((e: any) => toast(`Запуск мережі: ${e}`, 'error'))
        onCreated(srv.id, false)
      } else {
        onCreated(srv.id, launchAfterCreate)
      }
    } catch (e: any) {
      toast(`Не вдалося створити сервер: ${e}`, 'error')
    } finally {
      submitting = false
    }
  }

  // ── Вкладка "Додати з папки" ──
  let folderPath = $state('')
  let folderName = $state('')
  let detecting = $state(false)
  let detectError = $state('')
  let detected = $state<CoreDetectResult | null>(null)
  let folderEulaAccepted = $state(false)
  // EULA потрібна лише Minecraft-ядрам — Velocity-проксі не запускає
  // server.jar Mojang, тому для неї EULA не питаємо взагалі.
  const folderNeedsEula = $derived(!!detected && detected.core !== 'velocity')
  let folderRamMB = $state(2048)
  let folderLaunchAfterCreate = $state(true)

  // ── Вигляд сервера з папки ─────────────────────────────────────────
  let folderIcon = $state('')
  let folderColor = $state('#8b8b8b')
  let folderIconSrc = $state('')
  const folderIconPreview = $derived(
    folderIconSrc ? (packAsset('local-file://' + folderIconSrc) ?? '') : ''
  )
  async function folderBrowseIcon() {
    try {
      const p = await App.BrowseImageFile()
      if (p) { folderIconSrc = p; folderIcon = '' }
    } catch { /* скасовано */ }
  }
  function folderSelectPreset(n: string) { folderIcon = n; folderIconSrc = '' }

  // Застосовуємо вигляд ЩОЙНО створеному серверу (помилки не блокують:
  // сервер уже доданий, поганий колір — не привід вважати все провалом).
  async function applyFolderAppearance(serverId: string) {
    try {
      // БАГ: SetLauncherIcon зберігає лише саму іконку (немає параметра
      // кольору) — тому за гілки "свій файл іконки" колір ніколи не
      // писався в SaveServerConfig і губився. Тепер зберігаємо його
      // окремим викликом незалежно від того, яку іконку обрали.
      if (folderIconSrc) {
        await App.SetLauncherIcon(serverId, folderIconSrc)
        if (folderColor) await App.SaveServerConfig(serverId, { color: folderColor })
      } else if (folderIcon) {
        await App.SaveServerConfig(serverId, { icon: folderIcon, color: folderColor })
      } else if (folderColor) {
        await App.SaveServerConfig(serverId, { color: folderColor })
      }
    } catch { /* вигляд застосується наступного разу в налаштуваннях */ }
  }

  // ── Тека мережі: проксі й сервери в підтеках (CYBERCLUB/velocity, /hub…) ──
  // Додаються всі разом, мережа береться з velocity.toml проксі.
  let netFolder = $state<NetworkFolderInfo | null>(null)
  let importingNet = $state(false)
  const isNetworkFolder = $derived(!!netFolder && netFolder.hasProxy && netFolder.servers.length >= 2)
  async function detectNetworkFolder() {
    netFolder = null
    if (!folderPath) return
    try { netFolder = await App.DetectNetworkFolder(folderPath) } catch { netFolder = null }
  }
  async function importNetwork() {
    if (!netFolder || importingNet) return
    importingNet = true
    try {
      const res = await App.ImportNetworkFolder(netFolder.dir)
      for (const e of res.errors ?? []) toast(e, 'error')
      toast(t('servers.wizard.netImported', { n: res.added }), 'success')
      onCreated(res.proxyId, false)
    } catch (e: any) {
      toast(String(e), 'error')
    } finally {
      importingNet = false
    }
  }

  async function pickFolder() {
    try {
      const p = await App.BrowseFolder()
      if (!p) return
      folderPath = p
      if (!folderName) folderName = p.split(/[\\/]/).pop() ?? ''
      await Promise.all([runDetect(), detectNetworkFolder()])
    } catch { /* діалог скасовано */ }
  }

  async function runDetect() {
    if (!folderPath) return
    detecting = true
    detectError = ''
    detected = null
    try {
      detected = await App.DetectServerCore(folderPath)
    } catch (e: any) {
      detectError = String(e)
    } finally {
      detecting = false
    }
  }

  async function submitFolder() {
    if (!folderPath) { toast('Оберіть папку сервера', 'error'); return }
    if (!detected) { toast('Спочатку дочекайтесь визначення ядра', 'error'); return }
    if (detected.confidence === 'low') { toast('Не вдалося визначити ядро сервера', 'error'); return }
    if (folderNeedsEula && !folderEulaAccepted) { folderEulaModal = true; return }

    submitting = true
    try {
      const srv = await App.AddServerFromFolder(folderPath, folderName, folderRamMB, folderNeedsEula ? folderEulaAccepted : true)
      // Вигляд (іконка/колір), обраний праворуч — після реєстрації.
      await applyFolderAppearance(srv.id)
      // Ядро вже стоїть на диску (це наявна папка) — install-крок при
      // старті нічого не якатиме, просто одразу запуститься java.
      onCreated(srv.id, folderLaunchAfterCreate)
    } catch (e: any) {
      toast(`Не вдалося додати сервер: ${e}`, 'error')
    } finally {
      submitting = false
    }
  }

  function confidenceLabel(c: string): string {
    if (c === 'high') return 'Впевнено визначено'
    if (c === 'medium') return 'Визначено приблизно'
    return 'Не вдалося впевнено визначити'
  }
  function confidenceColor(c: string): string {
    if (c === 'high') return 'var(--green)'
    if (c === 'medium') return 'var(--yellow)'
    return 'var(--red)'
  }

  // ── Прев'ю ──
  function previewLabel(): string {
    if (tab === 'folder') return folderName || 'Сервер із папки'
    if (serverType === 'proxy') return effectiveName || 'Velocity'
    return effectiveName || 'Новий сервер'
  }
  function previewSub(): string {
    if (tab === 'folder') return detected ? `Визначено: ${detected.core || '—'}` : 'ще не визначено'
    if (serverType === 'proxy') return 'Velocity · проксі'
    return `${selectedCore.label}${mcVersion ? ' · ' + mcVersion : ''}`
  }

  // Синтетичний ServerView для превʼю: ті самі компоненти, що показують готовий сервер.
  const previewSrv = $derived<ServerView>({
    id: 'preview',
    name: previewLabel(),
    dir: '',
    core: tab === 'folder' ? (detected?.core || '') : serverType === 'proxy' ? 'velocity' : core,
    coreLabel: '',
    mcVersion: tab === 'folder' || serverType === 'proxy' ? '' : mcVersion,
    loaderVer: tab !== 'folder' && (serverType === 'proxy' || needsCoreVersion) ? coreVersion : '',
    javaMajor: 0, // підбирається бекендом при створенні — до того не показуємо
    javaInstalled: false,
    coreInstalled: tab === 'folder',
    ramMB: tab === 'folder' ? folderRamMB : ramMB,
    port: tab !== 'folder' && serverType === 'proxy' ? port : 0,
    status: 'stopped',
    hasWorlds: false, hasMods: false, hasPlugins: false,
    createdAt: 0,
    color: tab === 'folder' ? folderColor : undefined,
    icon: tab === 'folder' && folderIcon ? folderIcon : undefined,
    launcherIconData: tab === 'folder' && folderIconPreview ? folderIconPreview : undefined,
    autoBackup: { enabled: false, intervalMin: 60, keepCount: 10, keepDays: 7, maxPerDay: 0, worldDir: '' },
  } as ServerView)
</script>

{#snippet previewBlock()}
  <div>
    <div class="section-label" style="margin:2px 2px 8px;display:block">{t('swz.preview')}</div>
    <!-- Справжні картка бібліотеки й банер сторінки сервера (а не окрема «імітація»):
         превʼю завжди збігається з тим, що з'явиться після створення. Лише для перегляду — без кліків. -->
    <div class="pv-real" aria-hidden="true" inert>
      <ServerCard srv={previewSrv} />
    </div>
    <div class="pv-real" style="margin-top:14px" aria-hidden="true" inert>
      <ServerBanner srv={previewSrv} hideToolbar />
    </div>
  </div>
{/snippet}

<div class="create-page">
  <div class="tabbar">
    <button class="tab-btn" class:active={tab === 'create'} onclick={() => tab = 'create'}>
      <i class="ti ti-server-2"></i> Створити
    </button>
    <button class="tab-btn" class:active={tab === 'folder'} onclick={() => tab = 'folder'}>
      <i class="ti ti-folder-open"></i> Додати з папки
    </button>
  </div>

  {#key tab}
  <div class="create-form">
  {#if tab === 'create'}
  <div class="create-grid">
    <div>
      <!-- ТИП СЕРВЕРА -->
      <div class="card">
        <div class="card-head">
          <div class="ci violet"><i class="ti ti-stack-2"></i></div>
          <div class="ctw"><div class="ct">{t('swz.type')}</div></div>
          <span class="step-num">1</span>
        </div>
        <div class="row col">
          <div class="type-seg">
            <button class="type-seg-btn" class:sel={serverType === 'mc'} onclick={() => switchType('mc')} type="button">
              <i class="ti ti-package" style="color:#ff8a00"></i>
              <span class="ts-name">{t('swz.mcServer')}</span>
              <span class="ts-sub">Vanilla, Paper, Fabric, Forge…</span>
            </button>
            <button class="type-seg-btn" class:sel={serverType === 'proxy'} onclick={() => switchType('proxy')} type="button">
              <i class="ti ti-router" style="color:#3b82f6"></i>
              <span class="ts-name">{t('swz.proxy')}</span>
              <span class="ts-sub">{t('swz.proxyHint')}</span>
            </button>
          </div>
          
        </div>
      </div>

      {#if serverType === 'proxy'}
        <!-- ПРОКСІ: версія Velocity -->
        <div class="card">
          <div class="card-head">
            <div class="ci"><i class="ti ti-versions"></i></div>
            <div class="ctw"><div class="ct">{t('swz.proxyVer')}</div></div>
            <span class="step-num">2</span>
          </div>
          <div class="row">
            <div class="row-label"><div class="rl-name">{t('swz.velocityVer')}</div></div>
            <VersionPickerModal
              value={coreVersion}
              onselect={(v) => coreVersion = v}
              disabled={coreVersionsLoading || coreVersions.length === 0}
              minWidth="190px"
              placeholder={coreVersionsLoading ? '…' : coreVersions.length === 0 ? 'немає версій' : ''}
              title={t('swz.velocityVer')}
              icon="ti-versions"
              ariaLabel="Версія Velocity"
              emptyText="немає версій"
              options={coreVersions.map(v => ({
                value: v.version,
                label: v.version,
                badgeIcon: v.recommended ? 'ti-star' : undefined,
                badgeTitle: v.recommended ? 'Рекомендована' : undefined
              }))}
            />
          </div>
        </div>
      {:else}
        <!-- ВЕРСІЯ MINECRAFT (крок 2 — ЯК У ЗБІРКАХ: версія першою) -->
        <div class="card">
          <div class="card-head">
            <div class="ci"><i class="ti ti-cube"></i></div>
            <div class="ctw"><div class="ct">{t('swz.mcVer')}</div></div>
            <span class="step-num">2</span>
          </div>
          <div class="row">
            <div class="row-label"><div class="rl-name">{t('swz.gameVer')}</div></div>
            <div class="flex gap8">
              <button type="button" aria-pressed={showSnapshots} class="chip" class:on={showSnapshots} onclick={() => showSnapshots = !showSnapshots}  >{t('swz.snapshots')}</button>
              <VersionPickerModal
                value={mcVersion}
                onselect={(v) => mcVersion = v}
                disabled={mcVersionsLoading}
                minWidth="170px"
                placeholder={mcVersionsLoading ? '…' : ''}
                title={t('swz.mcVer')}
                icon="ti-versions"
                ariaLabel="Версія Minecraft"
                options={filteredMCVersions.map(v => ({ value: v.id, label: v.id, sub: v.type !== 'release' ? v.type : undefined }))}
              />
            </div>
          </div>
        </div>

        <!-- ЯДРО (крок 3) -->
        <div class="card">
          <div class="card-head">
            <div class="ci"><i class="ti ti-server-2"></i></div>
            <div class="ctw"><div class="ct">{t('swz.core')}</div></div>
            <span class="step-num">3</span>
          </div>
          <div class="row col">
            {#if availableCores.length < cores.length}
              <div class="field-hint" style="margin-bottom:6px;color:var(--yellow)"><i class="ti ti-alert-triangle"></i> {t('swz.coreLimited')}</div>
            {/if}
            <div class="loader-grid">
              {#each cores.filter(c => availableCores.includes(c.id)) as c (c.id)}
                <button type="button" aria-pressed={core === c.id} class="loader-opt" class:sel={core === c.id} onclick={() => selectCore(c.id)}  >
                  {#if c.icon}
                    <img class="loader-ic" src={c.icon} alt="" style="--lcol:{c.color}" />
                  {:else}
                    <i class="ti {c.ti}" style="font-size:26px;color:{c.color}"></i>
                  {/if}
                  <span class="ln">{c.label}</span>
                </button>
              {/each}
            </div>
          </div>
        </div>

        <!-- ВЕРСІЯ ЯДРА (крок 4) -->
        {#if needsCoreVersion}
        <div class="card">
          <div class="card-head">
            <div class="ci"><i class="ti ti-versions"></i></div>
            <div class="ctw"><div class="ct">Версія {selectedCore.label}</div></div>
            <span class="step-num">4</span>
          </div>
          <div class="row">
            <div class="row-label"><div class="rl-name">Версія {selectedCore.label}</div></div>
            <VersionPickerModal
              value={coreVersion}
              onselect={(v) => coreVersion = v}
              disabled={coreVersionsLoading || coreVersions.length === 0}
              minWidth="170px"
              placeholder={coreVersionsLoading ? '…' : coreVersions.length === 0 ? 'немає версій' : ''}
              title={`Версія ${selectedCore.label}`}
              icon="ti-versions"
              ariaLabel={`Версія ${selectedCore.label}`}
              emptyText="немає версій"
              options={coreVersions.map(v => ({
                value: v.version,
                label: v.version,
                badgeIcon: v.recommended ? 'ti-star' : undefined,
                badgeTitle: v.recommended ? 'Рекомендована' : undefined
              }))}
            />
          </div>
        </div>
        {/if}

        <!-- МОДИ З ІНШОЇ ЗБІРКИ ЧИ СЕРВЕРА (лише модові ядра) — та сама
             панель, що в створенні збірки -->
        {#if modCore && mcVersion}
          <MigrationPanel flow={migration} bind:expanded={migrationExpanded} {submitting} />
        {/if}
      {/if}

      <!-- ПРИЄДНАННЯ СЕРВЕРІВ (тільки проксі): Velocity-сторінка — це
           платформа мережі, тож сервери можна «вписати» одразу при
           створенні. Решта серверів (уже в іншій мережі або запущені)
           тут не показуються. -->
      {#if serverType === 'proxy'}
      <div class="card">
        <div class="card-head">
          <div class="ci"><i class="ti ti-network"></i></div>
          <div class="ctw"><div class="ct">{t('swz.netServers')}</div></div>
          <span class="step-num">3</span>
        </div>
        <div class="row col" style="gap:6px">
          {#if attachLoading}
            <div class="rl-sub"><i class="ti ti-loader spin"></i> {t('swz.loadingServers')}</div>
          {:else if attachCandidates.length === 0}
            <div class="rl-sub">{t('swz.noFree')}</div>
          {:else}
            <div class="attach-list">
              {#each attachCandidates as c}
                <label class="attach-row" style="--acol:{cores.find(x => x.id === c.core)?.color || 'var(--orange)'}">
                  <span class="attach-ic">
                    {#if cores.find(x => x.id === c.core)?.icon}
                      <img src={cores.find(x => x.id === c.core)!.icon} alt="" />
                    {:else}
                      <i class="ti {cores.find(x => x.id === c.core)?.ti || 'ti-server-2'}"></i>
                    {/if}
                  </span>
                  <span class="attach-info">
                    <span class="attach-name">{c.name}</span>
                    <span class="attach-sub">{cores.find(x => x.id === c.core)?.label || c.core} · {c.mcVersion || '—'} · порт {c.port}</span>
                  </span>
                  <input type="checkbox" checked={attachSelected.has(c.id)} onchange={(e) => {
                    if ((e.target as HTMLInputElement).checked) attachSelected.add(c.id)
                    else attachSelected.delete(c.id)
                  }} />
                </label>
              {/each}
            </div>
          {/if}
        </div>
      </div>
      {/if}

      <!-- НАЗВА + ПОРТ -->
      <div class="card">
        <div class="card-head">
          <div class="ci"><i class="ti ti-id"></i></div>
          <div class="ctw"><div class="ct">{t('swz.nameAndPort')}</div></div>
          <span class="step-num">{serverType === 'proxy' ? 4 : needsCoreVersion ? 5 : 4}</span>
        </div>
        <div class="row col">
          <div class="rl-name">{t('swz.name')}</div>
          <input class="input" placeholder={suggestedName || t('swz.name')} bind:value={name} />
        </div>
        <!-- ID сервера (тека на диску) — як ID збірки: можна лишити
             порожнім (виведеться з назви) або задати свій. Жива перевірка
             зайнятості: два сервери в одній теці перетерли б світи. -->
        <div class="row col">
          <div class="rl-name">{t('swz.id')}</div>
          <input
            class="input"
            placeholder={autoFolder || suggestedName || 'id-сервера'}
            bind:value={folderInput}
          />
          <div class="field-hint" style="margin-top:6px">
            {#if folderChecking}
              <i class="ti ti-loader spin"></i> Перевірка…
            {:else if folderCheck}
              {#if folderCheck.free}
                <i class="ti ti-circle-check" style="color:#10b981"></i>
              {:else}
                <i class="ti ti-circle-x" style="color:#ef4444"></i>
                {folderCheck.reason || t('servers.wizard.folderTaken')}
              {/if}
            {/if}
          </div>
        </div>
      </div>

      <!-- ПРОДУКТИВНІСТЬ -->
      <div class="card">
        <div class="card-head">
          <div class="ci green"><i class="ti ti-cpu"></i></div>
          <div class="ctw"><div class="ct">{t('swz.perf')}</div></div>
          <span class="step-num">{serverType === 'proxy' ? 5 : needsCoreVersion ? 6 : 5}</span>
        </div>
        <div class="row col">
          <div class="flex" style="justify-content:space-between">
            <div class="rl-sub">RAM: <b class="mono" style="color:var(--orange)">{ramMB} MB</b></div>
            {#if ramWarn}<div class="rl-sub" style="color:var(--yellow)"><i class="ti ti-alert-triangle"></i> {ramWarn}</div>{/if}
          </div>
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="ram-track" onpointerdown={startRamDrag} onpointermove={onRamDragMove} onpointerup={stopRamDrag} onpointercancel={stopRamDrag}>
            <div class="ram-fill" style="left:0%;width:{ramPct}%"></div>
            <div class="ram-knob" style="left:{ramPct}%"></div>
          </div>
        </div>
      </div>

      <!-- ЗАПУСК (останній крок). EULA Minecraft більше НЕ окрема картка:
           при натисканні «Створити сервер» зʼявляється модалка прийняття
           (як у звичайних збірках-лаунчерах). -->
      <div class="card">
        <div class="card-head">
          <div class="ci {eulaNeeded ? 'yellow' : 'green'}"><i class="ti {eulaNeeded ? 'ti-file-text' : 'ti-rocket'}"></i></div>
          <div class="ctw"><div class="ct">{t('swz.launch')}</div></div>
          <span class="step-num">{serverType === 'proxy' ? 6 : needsCoreVersion ? 7 : 6}</span>
        </div>
        <div class="row">
          <div class="row-label"><div class="rl-name">{t('swz.createStart')}</div></div>
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <div class="toggle" class:on={launchAfterCreate} onclick={() => launchAfterCreate = !launchAfterCreate} role="button" tabindex="0"></div>
        </div>
      </div>
    </div>

    <!-- ПРАВА КОЛОНКА -->
    <div class="preview-col">
      {@render previewBlock()}
    </div>
  </div>
  {:else}
  <!-- ── ДОДАТИ З ПАПКИ ── -->
  <div class="create-grid">
    <div>
      <div class="idea">
        <div class="ico"><i class="ti ti-folder-open"></i></div>
        <div><h4>{t('swz.existing')}</h4><p>{t('swz.existingHint')}</p></div>
      </div>

      <div class="card">
        <div class="card-head"><div class="ci"><i class="ti ti-folder"></i></div><div class="ctw"><div class="ct">{t('swz.folder')}</div></div></div>
        <div class="row col">
          <div class="folder-row">
            <input class="input" placeholder={t('swz.pathPh')} bind:value={folderPath} />
            <button class="btn btn-ghost" onclick={pickFolder} aria-label={t('swz.pickFolder')}><i class="ti ti-folder"></i></button>
          </div>
        </div>

        {#if isNetworkFolder && netFolder}
          <!-- Тека мережі: проксі + сервери в підтеках — одним кроком -->
          <div class="row col">
            <div class="detected-pack">
              <i class="ti ti-topology-star-3 ok" style="color:var(--green)"></i>
              <div>
                <div class="dp-name">{t('servers.wizard.netFound', { n: netFolder.servers.length })}</div>
                {#each netFolder.servers as s (s.dir)}
                  <div class="rl-sub" style="margin-top:4px">
                    <i class="ti {s.core === 'velocity' ? 'ti-router' : 'ti-server-2'}"></i>
                    <b>{s.name}</b> · {s.core}{#if s.mcVersion} {s.mcVersion}{/if} · :{s.port}
                    {#if s.known} · {t('servers.wizard.netKnown')}{/if}
                    {#if s.skip}<span style="color:var(--yellow)"> · {s.skip}</span>{/if}
                  </div>
                {/each}
              </div>
            </div>
          </div>
          <div class="row">
            <div class="row-label"><div class="rl-name">{t('servers.wizard.netImport')}</div></div>
            <button class="btn btn-primary" onclick={importNetwork} disabled={importingNet}>
              {#if importingNet}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-topology-star-3"></i>{/if} {t('servers.wizard.netImportBtn')}
            </button>
          </div>
        {:else if detecting}
          <div class="row"><div class="rl-sub"><i class="ti ti-loader spin"></i> {t('swz.detecting')}</div></div>
        {:else if detectError}
          <div class="row"><div class="rl-sub" style="color:var(--red)">{detectError}</div></div>
        {:else if detected}
          <div class="row col">
            <div class="detected-pack" class:warn={detected.confidence === 'low'}>
              <i class="ti {detected.confidence === 'low' ? 'ti-alert-triangle' : 'ti-circle-check'} ok" style="color:{confidenceColor(detected.confidence)}"></i>
              <div>
                <div class="dp-name">{confidenceLabel(detected.confidence)}</div>
                <div class="dp-meta">
                  {detected.core || '—'}
                  {#if detected.core === 'velocity' && !detected.mcVersion}
                    · проксі
                  {:else if detected.mcVersion}
                    · {detected.mcVersion}
                  {/if}
                  · порт {detected.port || '—'}
                </div>
                {#each detected.messages ?? [] as msg}
                  <div class="rl-sub" style="margin-top:4px">{msg}</div>
                {/each}
              </div>
            </div>
          </div>
          <div class="row col">
            <div class="rl-name">{t('swz.name')}</div>
            <input class="input" placeholder={t('swz.namePh')} bind:value={folderName} />
          </div>
          <div class="row col">
            <div class="flex" style="justify-content:space-between">
              <div class="rl-sub">RAM: <b class="mono" style="color:var(--orange)">{folderRamMB} MB</b></div>
            </div>
            <NumberInput class="input mono" step="256" bind:value={folderRamMB} style="width:140px" />
          </div>
          <div class="row">
            <div class="row-label"><div class="rl-name">{t('swz.startNow')}</div></div>
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <div class="toggle" class:on={folderLaunchAfterCreate} onclick={() => folderLaunchAfterCreate = !folderLaunchAfterCreate} role="button" tabindex="0"></div>
          </div>
          {#if folderNeedsEula}
            <div class="row">
              <div class="row-label"><div class="rl-name">{t('swz.eula')}</div></div>
            </div>
          {/if}
        {/if}
      </div>
    </div>

    {#if detected}
      <div class="preview-col">
        {@render previewBlock()}

        <!-- ВИГЛЯД: той самий віджет, що у «Створити»/налаштуваннях
             сервера (іконка лаунчера + колір), авто/після-створення. -->
        <div style="margin-top:14px">
          <div class="section-label" style="margin:2px 2px 8px;display:block">{t('swz.appearance')}</div>
          <AppearanceEditor
            title={t('swz.iconColor')}
            desc=""
            ciClass="violet"
            iconPreviewUrl={folderIconPreview}
            iconName={folderIcon}
            color={folderColor}
            onBrowseIcon={folderBrowseIcon}
            onSelectPreset={folderSelectPreset}
            onColorChange={(c: string) => folderColor = c}
            onRemoveBackground={null}
            autoSaveState="idle"
          />
        </div>
      </div>
    {/if}
  </div>
  {/if}
  </div>
  {/key}
</div>

<div class="create-footer">
  <button class="btn btn-ghost" onclick={onclose}><i class="ti ti-x"></i> {t('cancel')}</button>
  <button
    class="btn btn-primary"
    disabled={submitting || (tab === 'folder' && !detected) || (tab === 'create' && !!folderCheck && !folderCheck.free)}
    onclick={tab === 'create' ? submitCreate : submitFolder}
  >
    <i class="ti {submitting ? 'ti-loader spin' : 'ti-check'}"></i>
    {submitting ? 'Створення…' : tab === 'folder' ? 'Додати сервер' : serverType === 'proxy' ? 'Створити проксі' : 'Створити сервер'}
  </button>
</div>

<!-- EULA-модалка: зʼявляється при натисканні «Створити сервер» / «Додати
     сервер», якщо EULA ще не прийнята — не окремий крок-картка. -->
{#if eulaModal || folderEulaModal}
  <UiModal open={true} onclose={() => { eulaModal = false; folderEulaModal = false }} icon="ti-file-text" color="var(--yellow)"
           width="460px" title={t('swz.eula')}>
    <div style="font-size:12.5px;line-height:1.6;color:var(--text)">
      Для запуску сервера прийми
      <a href="https://www.minecraft.net/eula" target="_blank" rel="noopener" style="color:var(--blue)">End User License Agreement</a>
      від Mojang.
    </div>
    {#snippet footer()}
      <button class="btn btn-ghost" onclick={() => { eulaModal = false; folderEulaModal = false }}>{t('cancel')}</button>
      <button class="btn btn-primary" onclick={eulaConfirm}><i class="ti ti-check"></i> {t('swz.eulaAccept')}</button>
    {/snippet}
  </UiModal>
{/if}

<style>
  /* Перемикач типу сервера: Minecraft-сервер / проксі Velocity */
  .type-seg { display: flex; gap: 8px; flex-wrap: wrap; }
  .type-seg-btn {
    display: flex; flex-direction: column; align-items: flex-start; gap: 3px;
    flex: 1; min-width: 220px; text-align: left;
    padding: 10px 12px; border-radius: 10px; cursor: pointer;
    background: var(--bg-input); border: 1px solid var(--border);
    color: var(--text-mute); transition: border-color .15s, background .15s, transform .12s;
  }
  .type-seg-btn:hover { border-color: var(--border-hi); transform: translateY(-1px); }
  .type-seg-btn.sel {
    border-color: color-mix(in srgb, var(--pcol, var(--orange)) 55%, var(--border-hi));
    background: color-mix(in srgb, var(--pcol, var(--orange)) 7%, var(--bg-card));
    box-shadow: 0 0 0 1px color-mix(in srgb, var(--pcol, var(--orange)) 22%, transparent);
  }
  .type-seg-btn.sel:has(i.ti-router) { border-color: rgba(59,130,246,.55); background: color-mix(in srgb, #3b82f6 9%, var(--bg-card)); }
  .type-seg-btn > i { font-size: 22px; }
  .ts-name { font: 700 12.5px/1 var(--font-ui); color: var(--text); }
  .ts-sub { font: 400 var(--fs-min)/1.25 var(--font-ui); color: var(--text-mute); }

  /* Приєднання серверів до мережі при створенні проксі */
  .attach-list { display: flex; flex-direction: column; gap: 4px; max-height: 200px; overflow-y: auto; }
  .attach-row {
    display: flex; align-items: center; gap: 9px; padding: 6px 8px;
    border-radius: 8px; border: 1px solid var(--border); background: var(--bg-input);
    cursor: pointer; transition: border-color .14s, background .14s;
  }
  .attach-row:hover { border-color: var(--border-hi); }
  .attach-row:has(input:checked) { border-color: color-mix(in srgb, var(--acol) 55%, var(--border-hi)); background: color-mix(in srgb, var(--acol) 7%, var(--bg-card)); }
  .attach-ic {
    width: 26px; height: 26px; border-radius: 7px; flex-shrink: 0;
    display: flex; align-items: center; justify-content: center;
    background: color-mix(in srgb, var(--acol) 14%, transparent);
    font-size: 14px; color: var(--acol); overflow: hidden;
  }
  .attach-ic img { width: 100%; height: 100%; object-fit: contain; }
  .attach-info { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 1px; }
  .attach-name { font: 600 11.5px/1.2 var(--font-ui); color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .attach-sub { font: 400 var(--fs-min)/1.2 var(--font-mono); color: var(--text-mute); }
  .attach-row input[type=checkbox] { accent-color: var(--acol); width: 15px; height: 15px; flex-shrink: 0; }
</style>
