<script lang="ts">
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
  import { App } from './wails'
  import { toast } from './toast.svelte'
  import { t } from './i18n.svelte'
  import VersionPickerModal from './VersionPickerModal.svelte'
  import AppearanceEditor from './AppearanceEditor.svelte'
  import { packAsset } from './packAssets.svelte'
  import { flexverCompare } from './flexver'
  import type { MCVersionEntry, NewServerRequest, CoreDetectResult, ServerView, ServerFolderCheck } from './types'
  import vanillaIcon from '../assets/images/vanilla_icon.png'
  import fabricIcon from '../assets/images/fabric_icon.png'
  import forgeIcon from '../assets/images/forge_icon.png'
  import neoforgeIcon from '../assets/images/neoforge_icon.png'
  import quiltIcon from '../assets/images/quilt_icon.png'
  import papermcIcon from '../assets/images/papermc.svg'
  import purpurIcon from '../assets/images/purpur.png'
  import velocityIcon from '../assets/images/velocity.webp'

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
      // Раніше вибрані сервери, яких більше нема в кандидатах — прибрати.
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
  type CoreDef = { id: string; label: string; sub: string; icon?: string; ti?: string; color: string; group: 'vanilla' | 'mod' | 'plugin' }
  const cores: CoreDef[] = [
    { id: 'vanilla', label: 'Vanilla', sub: 'Офіційний сервер Mojang', icon: vanillaIcon, color: '#8b8b8b', group: 'vanilla' },
    { id: 'paper', label: 'Paper', sub: 'Плагіни, висока продуктивність', icon: papermcIcon, color: '#10b981', group: 'plugin' },
    { id: 'purpur', label: 'Purpur', sub: 'Форк Paper з фічами Purpur', icon: purpurIcon, color: '#ef4444', group: 'plugin' },
    { id: 'fabric', label: 'Fabric', sub: 'Легкий модлоадер', icon: fabricIcon, color: '#ff8a00', group: 'mod' },
    { id: 'forge', label: 'Forge', sub: 'Класичний модлоадер', icon: forgeIcon, color: '#d08b5b', group: 'mod' },
    { id: 'neoforge', label: 'NeoForge', sub: 'Форк Forge', icon: neoforgeIcon, color: '#ff6b35', group: 'mod' },
    { id: 'quilt', label: 'Quilt', sub: 'Форк Fabric', icon: quiltIcon, color: '#9d7cff', group: 'mod' },
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
  let folderSeq = 0
  $effect(() => {
    const probe = (folderInput.trim() || effectiveName).trim()
    // Порожньо і назви немає — питати нічого (бекенд теж віддасть дефолт).
    if (!probe) { folderCheck = null; return }
    const seq = ++folderSeq
    folderChecking = true
    // Невелика затримка: перевірка на кожну літеру — це зайвий виклик
    // бекенду (сканування теки серверів) на кожне натискання клавіші.
    const timer = setTimeout(() => {
      App.CheckServerFolder(folderInput.trim())
        .then(res => { if (seq === folderSeq) { folderCheck = res; folderChecking = false } })
        .catch(() => { if (seq === folderSeq) { folderCheck = null; folderChecking = false } })
    }, 220)
    return () => { clearTimeout(timer); folderChecking = false }
  })

  // Порт НЕ питаємо в майстрі: це властивість server.properties, і єдине
  // місце, де його міняють, — вкладка «Властивості» (server-port). Тут лише
  // підказуємо бекенду бажане значення: 0 = «обери вільний» (звичайні
  // сервери), 25577 — звичний порт Velocity (якщо зайнятий, бекенд візьме
  // наступний вільний і ми покажемо це в «Властивості»).
  let port = $state(serverType === 'proxy' ? 25577 : 0)

  const ramMin = 1024
  const ramMax = 16384
  const ramStep = 256
  let ramMB = $state(2048)
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
        id: folderInput.trim(),
        dir: '',
        ramMB,
        port,
        acceptEula: eulaNeeded ? eulaAccepted : true,
        javaMajor: 0,
        jvmArgs: '',
        programArgs: '',
      }
      const srv = await App.CreateServer(req)
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
            return {
              serverId: m.serverId, name: m.name, core: m.core, mcVersion: m.mcVersion,
              port: m.port, running: m.running,
              inNetwork: on, order: on ? order : m.order, delaySec: on ? 0 : m.delaySec,
            }
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
  // Раніше праворуч у вкладці «Додати з папки» був ЛИШЕ блок превʼю —
  // віджет редагування стилю (іконка лаунчера + колір), який є у
  // «Створити», просто відсутній. Тепер він тут теж: колір/іконка
  // зберігаються ПІСЛЯ реєстрації сервера (SaveServerConfig/SetLauncherIcon).
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

  async function pickFolder() {
    try {
      const p = await App.BrowseFolder()
      if (!p) return
      folderPath = p
      if (!folderName) folderName = p.split(/[\\/]/).pop() ?? ''
      await runDetect()
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
    if (detected.confidence === 'low') { toast('Не вдалося впевнено визначити ядро цієї папки', 'error'); return }
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
  function previewIconMeta() {
    if (tab === 'folder') return { icon: undefined as string | undefined, ti: 'ti-server-2', color: folderColor || '#8b8b8b' }
    if (serverType === 'proxy') return { icon: velocityIcon as string | undefined, ti: '', color: '#3b82f6' }
    const c = selectedCore
    return { icon: c.icon, ti: c.ti, color: c.color }
  }
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
</script>

{#snippet previewBlock()}
  {@const pv = previewIconMeta()}
  <div>
    <div class="section-label" style="margin:2px 2px 8px;display:block">ПРЕВʼЮ</div>
    <div class="inst-tile pv-tile" style="--pcol:{pv.color}">
      <div class="it-ic has-icon" style="--pcol:{pv.color}">
        {#if pv.icon}
          <img src={pv.icon} alt="" />
        {:else}
          <i class="ti {pv.ti}" style="color:{pv.color}"></i>
        {/if}
      </div>
      <div class="it-info">
        <div class="it-name">{previewLabel()}</div>
        <div class="it-sub"><span>{previewSub()}</span></div>
      </div>
      <div class="it-st st-check"><i class="ti ti-download"></i></div>
    </div>
    <div class="rl-sub" style="margin-top:10px">
      <i class="ti ti-info-circle"></i>
      {#if tab === 'folder'}
        Ядро вже стоїть у цій папці — install не знадобиться, сервер стартує одразу.
      {:else if serverType === 'proxy'}
        Проксі Velocity працює з будь-якою версією Minecraft — версію гри обирати не потрібно.
        {#if launchAfterCreate} Після створення сервер одразу почне встановлення й стартує сам.{/if}
      {:else}
        Ядро довантажиться при запуску — так само, як компоненти збірки якаються лише при старті гри.
        {#if launchAfterCreate} Після створення сервер одразу почне встановлення й стартує сам.{/if}
      {/if}
    </div>
    <div
      class="inst-banner"
      style="margin-top:14px;height:110px"
      style:background={`linear-gradient(135deg, color-mix(in srgb, ${pv.color} 55%, #241a10), color-mix(in srgb, ${pv.color} 22%, #120c06))`}
    >
      <div class="ib-title">
        <span class="ib-ic">
          {#if pv.icon}
            <img src={pv.icon} alt="" />
          {:else}
            <i class="ti {pv.ti}" style="color:#fff"></i>
          {/if}
        </span>
        <div class="ib-meta">
          <div class="ib-name">{previewLabel()}</div>
        </div>
      </div>
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
          <div class="ctw"><div class="ct">Тип сервера</div><div class="cd">Minecraft-сервер або проксі для мережі</div></div>
          <span class="step-num">1</span>
        </div>
        <div class="row col">
          <div class="type-seg">
            <button class="type-seg-btn" class:sel={serverType === 'mc'} onclick={() => switchType('mc')} type="button">
              <i class="ti ti-package" style="color:#ff8a00"></i>
              <span class="ts-name">Minecraft-сервер</span>
              <span class="ts-sub">Vanilla, Paper, Fabric, Forge…</span>
            </button>
            <button class="type-seg-btn" class:sel={serverType === 'proxy'} onclick={() => switchType('proxy')} type="button">
              <i class="ti ti-router" style="color:#3b82f6"></i>
              <span class="ts-name">Проксі — Velocity</span>
              <span class="ts-sub">Обʼєднує кілька серверів в одну мережу</span>
            </button>
          </div>
          {#if serverType === 'proxy'}
            <div class="field-hint" style="margin-top:8px;color:var(--blue)"><i class="ti ti-info-circle"></i> Velocity — проксі: йому байдуже, яка версія Minecraft у тебе на серверах. Вкажи лише версію самого проксі.</div>
          {/if}
        </div>
      </div>

      {#if serverType === 'proxy'}
        <!-- ПРОКСІ: версія Velocity -->
        <div class="card">
          <div class="card-head">
            <div class="ci"><i class="ti ti-versions"></i></div>
            <div class="ctw"><div class="ct">Версія проксі</div><div class="cd">Версія Velocity (не залежить від версії гри)</div></div>
            <span class="step-num">2</span>
          </div>
          <div class="row">
            <div class="row-label"><div class="rl-name">Версія Velocity</div><div class="rl-sub">Найновіша рекомендована за замовчуванням</div></div>
            <VersionPickerModal
              value={coreVersion}
              onselect={(v) => coreVersion = v}
              disabled={coreVersionsLoading || coreVersions.length === 0}
              minWidth="190px"
              placeholder={coreVersionsLoading ? '…' : coreVersions.length === 0 ? 'немає версій' : ''}
              title="Версія Velocity"
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
            <div class="ctw"><div class="ct">Версія Minecraft</div><div class="cd">Обери версію гри — під неї покажуться сумісні ядра</div></div>
            <span class="step-num">2</span>
          </div>
          <div class="row">
            <div class="row-label"><div class="rl-name">Версія гри</div><div class="rl-sub">Стабільна за замовчуванням; снапшоти — за бажанням</div></div>
            <div class="flex gap8">
              <span class="chip" class:on={showSnapshots} onclick={() => showSnapshots = !showSnapshots} role="button" tabindex="0">Снапшоти</span>
              <VersionPickerModal
                value={mcVersion}
                onselect={(v) => mcVersion = v}
                disabled={mcVersionsLoading}
                minWidth="170px"
                placeholder={mcVersionsLoading ? '…' : ''}
                title="Версія Minecraft"
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
            <div class="ctw"><div class="ct">Ядро сервера</div><div class="cd">Оберіть тип ядра — визначає доступні плагіни/моди</div></div>
            <span class="step-num">3</span>
          </div>
          <div class="row col">
            {#if availableCores.length < cores.length}
              <div class="field-hint" style="margin-bottom:6px;color:var(--yellow)"><i class="ti ti-alert-triangle"></i> Деякі ядра приховані — немає версій під обрану Minecraft</div>
            {/if}
            <div class="loader-grid">
              {#each cores.filter(c => availableCores.includes(c.id)) as c (c.id)}
                <div class="loader-opt" class:sel={core === c.id} onclick={() => selectCore(c.id)} role="button" tabindex="0">
                  {#if c.icon}
                    <img class="loader-ic" src={c.icon} alt="" style="--lcol:{c.color}" />
                  {:else}
                    <i class="ti {c.ti}" style="font-size:26px;color:{c.color}"></i>
                  {/if}
                  <span class="ln">{c.label}</span>
                  <span class="lt">{c.sub}</span>
                </div>
              {/each}
            </div>
          </div>
        </div>

        <!-- ВЕРСІЯ ЯДРА (крок 4) -->
        {#if needsCoreVersion}
        <div class="card">
          <div class="card-head">
            <div class="ci"><i class="ti ti-versions"></i></div>
            <div class="ctw"><div class="ct">Версія {selectedCore.label}</div><div class="cd">Рекомендована позначена зірочкою</div></div>
            <span class="step-num">4</span>
          </div>
          <div class="row">
            <div class="row-label"><div class="rl-name">Версія {selectedCore.label}</div><div class="rl-sub">{selectedCore.id === 'paper' || selectedCore.id === 'purpur' ? 'Номер білда' : 'Версія лоадера'}</div></div>
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
      {/if}

      <!-- ПРИЄДНАННЯ СЕРВЕРІВ (тільки проксі): Velocity-сторінка — це
           платформа мережі, тож сервери можна «вписати» одразу при
           створенні. Решта серверів (уже в іншій мережі або запущені)
           тут не показуються. -->
      {#if serverType === 'proxy'}
      <div class="card">
        <div class="card-head">
          <div class="ci"><i class="ti ti-network"></i></div>
          <div class="ctw"><div class="ct">Сервери мережі</div><div class="cd">Приєднай наявні Paper/Purpur-сервери — вони «склеяться» з проксі (спільний forwarding secret)</div></div>
          <span class="step-num">3</span>
        </div>
        <div class="row col" style="gap:6px">
          {#if attachLoading}
            <div class="rl-sub"><i class="ti ti-loader spin"></i> Завантаження серверів…</div>
          {:else if attachCandidates.length === 0}
            <div class="rl-sub">Зупинених серверів без мережі немає — створи Paper/Purpur-сервер, і він зʼявиться тут.</div>
          {:else}
            <div class="rl-sub" style="margin-bottom:2px">Обрані сервери стартуватимуть разом з проксі (можна змінити пізніше на сторінці «Мережа»).</div>
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
          <div class="ctw"><div class="ct">Назва та порт</div></div>
          <span class="step-num">{serverType === 'proxy' ? 4 : needsCoreVersion ? 5 : 4}</span>
        </div>
        <div class="row col">
          <div class="rl-name">Назва сервера</div>
          <input class="input" placeholder={suggestedName || 'Назва сервера'} bind:value={name} />
        </div>
        <!-- ID сервера (тека на диску) — як ID збірки: можна лишити
             порожнім (виведеться з назви) або задати свій. Жива перевірка
             зайнятості: два сервери в одній теці перетерли б світи. -->
        <div class="row col">
          <div class="rl-name">ID сервера (тека)</div>
          <input
            class="input"
            placeholder={suggestedName || 'id-сервера'}
            bind:value={folderInput}
          />
          <div class="field-hint" style="margin-top:6px">
            {#if folderChecking}
              <i class="ti ti-loader spin"></i> Перевірка…
            {:else if folderCheck}
              {#if folderCheck.free}
                <i class="ti ti-circle-check" style="color:#10b981"></i>
                {t('servers.wizard.folderFree', { folder: folderCheck.folder })}
              {:else}
                <i class="ti ti-circle-x" style="color:#ef4444"></i>
                {folderCheck.reason || t('servers.wizard.folderTaken')}
              {/if}
            {:else}
              <i class="ti ti-info-circle"></i>
              {t('servers.wizard.folderHint')}
            {/if}
          </div>
        </div>
        <div class="field-hint" style="margin-top:6px">
          <i class="ti ti-info-circle"></i>
          {t('servers.wizard.portHint')}
        </div>
        {#if serverType === 'proxy'}
          <div class="field-hint" style="margin-top:6px"><i class="ti ti-info-circle"></i> {t('servers.wizard.velocityPortHint')}</div>
        {/if}
      </div>

      <!-- ПРОДУКТИВНІСТЬ -->
      <div class="card">
        <div class="card-head">
          <div class="ci green"><i class="ti ti-cpu"></i></div>
          <div class="ctw"><div class="ct">Продуктивність</div><div class="cd">Оперативна памʼять для сервера</div></div>
          <span class="step-num">{serverType === 'proxy' ? 5 : needsCoreVersion ? 6 : 5}</span>
        </div>
        <div class="row col">
          <div class="flex" style="justify-content:space-between">
            <div class="rl-sub">RAM: <b class="mono" style="color:var(--orange)">{ramMB} MB</b></div>
            {#if ramWarn}<div class="rl-sub" style="color:var(--yellow)"><i class="ti ti-alert-triangle"></i> {ramWarn}</div>{/if}
          </div>
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
          <div class="ctw"><div class="ct">Запуск</div><div class="cd">{eulaNeeded ? 'Перед створенням буде запропоновано прийняти EULA Minecraft' : 'Проксі не потребує EULA — лише виріши, чи стартувати одразу'}</div></div>
          <span class="step-num">{serverType === 'proxy' ? 6 : needsCoreVersion ? 7 : 6}</span>
        </div>
        <div class="row">
          <div class="row-label"><div class="rl-name">Створити й одразу запустити</div><div class="rl-sub">Install ядра та старт java підуть одним потоком у консолі сервера</div></div>
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
        <div><h4>Наявний сервер</h4><p>Вкажіть теку вже готового сервера — лаунчер сам визначить ядро, версію Minecraft і порт.</p></div>
      </div>

      <div class="card">
        <div class="card-head"><div class="ci"><i class="ti ti-folder"></i></div><div class="ctw"><div class="ct">ПАПКА СЕРВЕРА</div></div></div>
        <div class="row col">
          <div class="folder-row">
            <input class="input" placeholder="Шлях до папки сервера" bind:value={folderPath} />
            <button class="btn btn-ghost" onclick={pickFolder}><i class="ti ti-folder"></i></button>
          </div>
        </div>

        {#if detecting}
          <div class="row"><div class="rl-sub"><i class="ti ti-loader spin"></i> Визначення ядра…</div></div>
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
                    · проксі (версія гри не застосовується)
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
            <div class="rl-name">Назва сервера</div>
            <input class="input" placeholder="Назва" bind:value={folderName} />
          </div>
          <div class="row col">
            <div class="flex" style="justify-content:space-between">
              <div class="rl-sub">RAM: <b class="mono" style="color:var(--orange)">{folderRamMB} MB</b></div>
            </div>
            <input class="input mono" type="number" step="256" bind:value={folderRamMB} style="width:140px" />
          </div>
          <div class="row">
            <div class="row-label"><div class="rl-name">Одразу запустити</div></div>
            <div class="toggle" class:on={folderLaunchAfterCreate} onclick={() => folderLaunchAfterCreate = !folderLaunchAfterCreate} role="button" tabindex="0"></div>
          </div>
          {#if folderNeedsEula}
            <div class="row">
              <div class="row-label"><div class="rl-name">Угода Minecraft EULA</div><div class="rl-sub">Прийняти треба при натисканні «Додати сервер» — модалка зʼявиться сама</div></div>
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
          <div class="section-label" style="margin:2px 2px 8px;display:block">ВИГЛЯД СЕРВЕРА</div>
          <AppearanceEditor
            title="Іконка та колір"
            desc="Зберігається в картці сервера — як у збірках"
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
  <button class="btn btn-ghost" onclick={onclose}><i class="ti ti-x"></i> Скасувати</button>
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
  <div
    class="modal-overlay"
    role="presentation"
    onclick={(e) => { if (e.target === e.currentTarget) { eulaModal = false; folderEulaModal = false } }}
    onkeydown={(e) => { if (e.key === 'Escape') { eulaModal = false; folderEulaModal = false } }}
  >
    <div class="modal" style="max-width:460px" role="dialog" aria-modal="true" aria-label="Угода Minecraft EULA" tabindex="-1" onclick={(e) => e.stopPropagation()}>
      <div class="modal-head">
        <h3><i class="ti ti-file-text" style="color:var(--yellow);margin-right:8px"></i>Угода Minecraft EULA</h3>
        <button class="md-close" onclick={() => { eulaModal = false; folderEulaModal = false }} aria-label="Закрити"><i class="ti ti-x"></i></button>
      </div>
      <div class="modal-body">
        <div style="font-size:12.5px;line-height:1.6;color:var(--text)">
          Щоб запускати сервер Minecraft, ти маєш прийняти
          <a href="https://www.minecraft.net/eula" target="_blank" rel="noopener" style="color:var(--blue)">End User License Agreement</a>
          від Mojang. Без прийняття сервер не запуститься.
        </div>
      </div>
      <div class="modal-foot">
        <button class="btn btn-ghost" onclick={() => { eulaModal = false; folderEulaModal = false }}>Скасувати</button>
        <button class="btn btn-primary" onclick={eulaConfirm}><i class="ti ti-check"></i> Я приймаю EULA</button>
      </div>
    </div>
  </div>
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
  .ts-sub { font: 400 10.5px/1.25 var(--font-ui); color: var(--text-mute); }

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
  .attach-sub { font: 400 10px/1.2 var(--font-mono); color: var(--text-mute); }
  .attach-row input[type=checkbox] { accent-color: var(--acol); width: 15px; height: 15px; flex-shrink: 0; }
</style>
