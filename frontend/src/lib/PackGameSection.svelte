<script lang="ts">
  import { untrack, onDestroy } from 'svelte'
  import UiModal from './UiModal.svelte'
  import SCard from './settings/SCard.svelte'
  import './settings/settings.css'
  // ── Розділ «Гра» на сторінці збірки ────────────────────────────────────
  // Мінімальна RAM (-Xms), прив'язка збірки до конкретного акаунта та
  // автоприєднання (світ або сервер). Все зберігається per-instance.
  import { t } from './i18n.svelte'
  import { App, on } from './wails'
  import StSelect, { type StOption } from './StSelect.svelte'
  import PackServerLinkCard from './servers/components/PackServerLinkCard.svelte'
  import steveHead from '../assets/images/steve-head.png'
  import { toast } from './toast.svelte'
  import type { Account, BuildView, InstanceConfig, InstanceServer, KeyBindLabel, PackOptionsView, ServerPing, Settings, SkinPreset } from './types'

  let {
    cfg,
    settings,
    accounts,
    worlds,
    packServers = [],
    onRefreshWorlds = () => {},
    mcVersion = '',
    packId = '',
    activeAccount = null as Account | null,
    onUpdate
  } = $props<{
    cfg: InstanceConfig
    settings: Settings
    accounts: Account[]
    activeAccount?: Account | null
    worlds: string[]
    // Сервери, збережені у server.dat збірки (для вибору цілі автоприєднання
    // по IP замість ручного введення).
    packServers?: InstanceServer[]
    // Оновлення списку світів/серверів (батьківська сторінка перечитує їх
    // з диска — гра могла створити новий світ).
    onRefreshWorlds?: () => void
    mcVersion?: string
    // ID збірки — для розділу «Налаштування гри (options.txt)».
    packId?: string
    onUpdate: (key: keyof InstanceConfig, value: InstanceConfig[keyof InstanceConfig]) => void
  }>()

  // ── Підтримка автоприєднання до світу (quick-play) ─────────────────────
  // Офіційний прапорець --quickPlaySingleplayer з'явився у 1.20
  // (снапшот 23w14a). Для старіших версій гра його просто ігнорує, тому
  // опцію «Світ» в автоприєднанні для них не показуємо взагалі — лишаємо
  // лише «Сервер». Для версій 1.20.x сам вхід у світ працює, але довше.
  function parseMCVersion(v: string): { major: number; minor: number } | null {
    const m = /^(\d+)\.(\d+)/.exec(v.trim())
    return m ? { major: Number(m[1]), minor: Number(m[2]) } : null
  }
  const supportsWorldJoin = $derived.by((): boolean => {
    const v = parseMCVersion(mcVersion)
    if (!v) return true // невідома версія (снапшоти тощо) — вважаємо сучасною
    return v.major > 1 || (v.major === 1 && v.minor >= 20)
  })
  // 1.20.x — вхід у світ працює, але довше; 1.20.0/1.20.1 quick-play ще
  // глючний, тож для всіх 1.20 показуємо застереження.
  const isSlowWorldJoin = $derived.by((): boolean => {
    const v = parseMCVersion(mcVersion)
    return !!v && v.major === 1 && v.minor === 20
  })

  // ── Аватари акаунтів (для селекта «прив'язати збірку до акаунта») ─────
  // Ті самі data URL голів зі скінів Mojang, що й у сайдбарі; кешуються на
  // бекенді (App.GetAccountHead). Якщо не завантажились — запасна голова.
  let heads = $state<Record<string, string>>({})
  $effect(() => {
    const accs = accounts
    if (!accs?.length) return
    let cancelled = false
    ;(async () => {
      const next: Record<string, string> = {}
      await Promise.all(accs.map(async (acc: Account) => {
        try {
          const url = await App.GetAccountHead(acc.uuid, acc.username, !!acc.isLicensed)
          const key = acc.uuid || acc.username
          if (key && url) next[key] = url
        } catch (e) { /* запасна голова Стіва */ }
      }))
      if (!cancelled) heads = next
    })()
    return () => { cancelled = true }
  })
  function headFor(uuid?: string): string {
    return (uuid && heads[uuid]) || steveHead
  }

  // Опції селекта акаунтів + поточний обраний акаунт.
  const accountOptions = $derived(
    accounts.map((acc: Account) => ({
      value: acc.id, label: acc.username, head: headFor(acc.uuid)
    }))
  )
  const currentAccount = $derived(accounts.find((a: Account) => a.id === cfg.accountIdOverride))

  // ── Скін збірки ────────────────────────────────────────────────────────
  // Акаунт, який реально запускатиме збірку (прив'язаний або активний) —
  // від нього залежить, чи показувати блок «Скін збірки» (лише ліцензійні).
  const launchAccount = $derived(accounts.find((a: Account) => a.id === cfg.accountIdOverride) ?? activeAccount ?? null)
  const skinLicensed = $derived(!!launchAccount?.isLicensed)

  // Пресети Гардероба вибраного акаунта — список для випадаючого меню.
  let wardrobePresets = $state<SkinPreset[]>([])
  let presetsLoaded = $state(false)
  // Прев'ю «голова + плащ» для ВСІХ пресетів: показуються прямо в опціях
  // випадаючого списку (той самий метод кропа, що в редакторі пресета,
  // тільки мініатюри менші). Завантажуємо всі одразу, щоб дропдаун був
  // «живим» при відкритті — без дочікування при виборі.
  let presetThumbs = $state<Record<string, { head: string; cape: string | null }>>({})
  const thumbSig: Record<string, string> = {}

  const skinPresetOptions = $derived<StOption[]>(
    wardrobePresets.map((p) => {
      const t = presetThumbs[p.id]
      return {
        value: p.id,
        label: p.name,
        head: t?.head || undefined,
        cape: t?.cape || undefined,
      }
    }),
  )

  async function loadWardrobePresets() {
    if (!launchAccount) {
      wardrobePresets = []
      presetsLoaded = true
      return
    }
    try {
      wardrobePresets = (await App.GetWardrobePresets(launchAccount.id)) ?? []
    } catch {
      wardrobePresets = []
    } finally {
      presetsLoaded = true
    }
  }

  // Завантаження мініатюр голова+плащ для всіх пресетів (кеш за сигнатурою
  // skinPath|capePath — як у гардеробі: зміна пресета інвалідує мініатюру).
  $effect(() => {
    const list = wardrobePresets
    if (list.length === 0) return
    for (const p of list) {
      const sig = p.skinPath + '|' + (p.capePath ?? '')
      if (thumbSig[p.id] === sig) continue
      thumbSig[p.id] = sig
      // Скидаємо стару мініатюру, щоб опція не показувала чуже прев'ю.
      delete presetThumbs[p.id]
      ;(async () => {
        let head = ''
        let cape: string | null = null
        try {
          const skinDataUrl = await App.GetWardrobeTextureDataURL(p.skinPath)
          if (thumbSig[p.id] !== sig) return
          head = await cropSkinHead(skinDataUrl)
          if (thumbSig[p.id] !== sig) return
          if (p.capePath) {
            try {
              const capeDataUrl = await App.GetWardrobeTextureDataURL(p.capePath)
              if (thumbSig[p.id] !== sig) return
              cape = await cropCapeFront(capeDataUrl)
            } catch { cape = null }
          }
          if (thumbSig[p.id] === sig) presetThumbs[p.id] = { head, cape }
        } catch {
          if (thumbSig[p.id] === sig) presetThumbs[p.id] = { head, cape }
        }
      })()
    }
  })

  // Той самий метод відображення, що в редакторі пресета Гардероба:
  // голова — зона 8,8,8,8 у текстурі 64x64, збільшена до 52x52 без
  // згладжування; плащ — фронтальна частина 1,2,10,15 → 50x60.
  function cropSkinHead(dataUrl: string): Promise<string> {
    return new Promise((resolve, reject) => {
      const img = new Image()
      img.onload = () => {
        try {
          const c = document.createElement('canvas')
          c.width = 52
          c.height = 52
          const ctx = c.getContext('2d')
          if (!ctx) { reject(new Error('canvas')); return }
          ctx.imageSmoothingEnabled = false
          ctx.drawImage(img, 8, 8, 8, 8, 0, 0, 52, 52)
          resolve(c.toDataURL('image/png'))
        } catch (e) { reject(e) }
      }
      img.onerror = () => reject(new Error('head load failed'))
      img.src = dataUrl
    })
  }

  function cropCapeFront(dataUrl: string): Promise<string> {
    return new Promise((resolve, reject) => {
      const img = new Image()
      img.onload = () => {
        try {
          const c = document.createElement('canvas')
          c.width = 50
          c.height = 60
          const ctx = c.getContext('2d')
          if (!ctx) { reject(new Error('canvas')); return }
          ctx.imageSmoothingEnabled = false
          const srcH = Math.max(1, Math.min(15, img.height - 2))
          ctx.drawImage(img, 1, 2, 10, srcH, 0, 0, 50, 60)
          resolve(c.toDataURL('image/png'))
        } catch (e) { reject(e) }
      }
      img.onerror = () => reject(new Error('cape load failed'))
      img.src = dataUrl
    })
  }

  // Вмикання/вимикання скіна збірки. При вмиканні підвантажуємо пресети
  // Гардероба і вибираємо перший (якщо є).
  async function toggleBuildSkin() {
    if (cfg.skinPresetId) {
      onUpdate('skinPresetId', '')
      return
    }
    if (!presetsLoaded) await loadWardrobePresets()
    if (wardrobePresets.length === 0) {
      toast(t('pack.game.skinNoPresets'), 'error')
      return
    }
    onUpdate('skinPresetId', wardrobePresets[0].id)
  }

  // Мініатюра вибраного пресета для заголовка картки.
  const selectedThumb = $derived(cfg.skinPresetId ? presetThumbs[cfg.skinPresetId] : undefined)

  // При зміні акаунта (або перевідкритті сторінки) — свіжі пресети.
  $effect(() => {
    void launchAccount?.id
    presetsLoaded = false
    // Чистимо кеш мініатюр: пресети іншого акаунта можуть мати ті самі id
    // або збігатись за сигнатурою — без скидання показувались би чужі скіни.
    for (const k of Object.keys(thumbSig)) delete thumbSig[k]
    for (const k of Object.keys(presetThumbs)) delete presetThumbs[k]
    loadWardrobePresets()
  })

  // ── Статус повернення скіна (після зупинки збірки) ───────────────────
  // Бекенд шле подію skin:restore під час повернення (restoring 2/6), при
  // успіху (done) і при невдачі (failed). Панель у блоці «Скін збірки»
  // показує, що зараз відбувається зі скіном акаунта.
  let restoreStatus = $state<{ status: string; attempt?: number; maxAttempts?: number; error?: string } | null>(null)
  let restoreUnsub: (() => void) | null = null
  let restoreFetchGen = 0

  // Підписка на події повернення (для ЦІЄЇ збірки).
  $effect(() => {
    const pid = packId
    if (!pid) return
    restoreUnsub?.()
    restoreUnsub = on('skin:restore', (e: { buildId?: string; status: string; attempt?: number; maxAttempts?: number; error?: string }) => {
      if (e.buildId !== pid) return
      if (e.status === 'done') {
        // Коротко показуємо успіх, потім ховаємо (снапшот видалено).
        restoreStatus = { status: 'done' }
        setTimeout(() => { if (restoreStatus?.status === 'done') restoreStatus = null }, 4000)
      } else {
        restoreStatus = { status: e.status, attempt: e.attempt, maxAttempts: e.maxAttempts, error: e.error }
      }
    })
    return () => restoreUnsub?.()
  })

  // При відкритті сторінки — поточний стан з бекенду (якщо повернення йде
  // або не вдалось, поки сторінка була закрита).
  $effect(() => {
    const pid = packId
    const acc = launchAccount?.id
    if (!pid || !acc) return
    const gen = ++restoreFetchGen
    App.GetBuildSkinRestoreStatus(pid, acc)
      .then((s) => {
        if (gen !== restoreFetchGen) return
        if (s.status === 'restoring' || s.status === 'failed') restoreStatus = s
        else restoreStatus = null
      })
      .catch(() => {})
  })

  // Тип автоприєднання («Світ»/«Сервер») вибирається сегментним перемикачем
  // у розмітці (seg-ctl — той самий стиль, що розмір сайдбара в налаштуваннях).
  // Опція «Світ» прихована для версій без quick-play; якщо тип у cfg «world»,
  // а версія не підтримує — $effect нижче тихо перемикає на «Сервер».
  const worldOptions = $derived(
    worlds.map((w: string) => ({ value: w, label: w, icon: 'ti-map' }))
  )

  // ── Вибір сервера з server.dat збірки ──
  // Поле — «комбобокс» (editable): адресу можна ВВЕСТИ вручну, а сервери з
  // server.dat доступні як підказки. Окремий пункт «ввести вручну» не
  // потрібен — введення у поле і є ручне значення.
  const serverOptions = $derived.by((): StOption[] => {
    const opts: StOption[] = []
    const seen = new Set<string>()
    for (const s of packServers) {
      if (!s.address || seen.has(s.address)) continue
      seen.add(s.address)
      opts.push({ value: s.address, label: s.name ? `${s.name} — ${s.address}` : s.address, icon: 'ti-server-2' })
    }
    if (cfg.autoJoinServer && !seen.has(cfg.autoJoinServer)) {
      opts.push({ value: cfg.autoJoinServer, label: cfg.autoJoinServer, icon: 'ti-pencil' })
    }
    return opts
  })

  // ── Пінг сервера (чи в мережі) ──
  // Автоматично після зміни адреси (з дебаунсом) і по кнопці оновлення.
  let pings = $state<Record<string, ServerPing | null>>({})
  let pingTimer: ReturnType<typeof setTimeout> | undefined

  async function pingServer(address: string) {
    if (!address) return
    pings = { ...pings, [address]: null }
    try {
      pings = { ...pings, [address]: await App.PingServer(address) }
    } catch {
      pings = { ...pings, [address]: { online: false, error: t('pack.game.pingError') } }
    }
  }

  function pingCurrent() {
    if (cfg.autoJoinServer) pingServer(cfg.autoJoinServer)
  }

  $effect(() => {
    const addr = cfg.autoJoinServer
    if (!addr || pings[addr] !== undefined) return
    clearTimeout(pingTimer)
    pingTimer = setTimeout(() => pingServer(addr), 350)
  })

  // ── Дефолт-сервер ──
  // Автоприєднання до сервера увімкнене, а адреса порожня → підставляємо
  // ПЕРШИЙ сервер із server.dat збірки (не лишаємо поле порожнім).
  // serverEdited: щойно користувач сам змінив адресу (ввів/очистив поле) —
  // автопідстановку вимикаємо, інакше вона боролася б із ручним введенням.
  let serverEdited = $state(false)
  $effect(() => {
    if (serverEdited) return
    if (cfg.autoJoinEnabled && cfg.autoJoinType !== 'world' && !cfg.autoJoinServer && packServers.length > 0) {
      const first = packServers.find((s: InstanceServer) => s.address)
      if (first) onUpdate('autoJoinServer', first.address)
    }
  })

  // ── Автоперемикання на «Сервер», якщо версія не підтримує вхід у світ ──
  // Сегментний перемикач не має показувати «мертвий» стан: якщо тип у cfg
  // ще «world», а версія гри його не підтримує — тихо перемикаємо на сервер
  // (значення «world» усе одно не застосувалося б — прапорець ігнорується).
  $effect(() => {
    if (cfg.autoJoinType === 'world' && !supportsWorldJoin) {
      onUpdate('autoJoinType', 'server')
    }
  })

  // ── Авто-скрол до полів автоприєднання, коли вмикається ──
  // Виправляє зависання: розділ «Гра» не показує поля автоприєднання,
  // якщо їх вміст не поміщається у в'юпорт — примусово скролимо до них.
  let autoJoinRef = $state<HTMLDivElement | undefined>()
  $effect(() => {
    if (cfg.autoJoinEnabled && autoJoinRef) {
      requestAnimationFrame(() => {
        autoJoinRef?.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
      })
    }
  })

  // ── Налаштування гри (options.txt) — пряме читання/редагування ────────
  // Порт розділу «Гра» старого лаунчера: без запуску гри можна читати і
  // редагувати options.txt збірки. Редагуються ЛИШЕ опції, відомі лаунчеру
  // (співпадають з розділом «За замовчуванням» у налаштуваннях) — решту
  // файла (опції модів тощо) бекенд не чіпає ніколи.
  let optionsView = $state<PackOptionsView | null>(null)
  let opts = $state<Record<string, string>>({})
  let optionsLoading = $state(false)
  let busy = $state(false)

  // ── Автозбереження options.txt ──────────────────────────────────────
  // Кожна зміна ставить ключ у dirtyKeys і запускає відкладений запис
  // (600 мс). Пишемо ТІЛЬКИ змінені ключі — решту файла (і те, що гра
  // могла перезаписати між відкриттям сторінки та зміною) не чіпаємо.
  // optsPackId — збірка, якій належать opts: при перемиканні збірки
  // незбережене дописується у СТАРУ збірку, а не губиться і не йде в нову.
  type SaveState = 'idle' | 'pending' | 'saving' | 'saved' | 'error'
  let saveState = $state<SaveState>('idle')
  let optsPackId = ''
  const dirtyKeys = new Set<string>()
  let optTimer: ReturnType<typeof setTimeout> | undefined
  let savedTimer: ReturnType<typeof setTimeout> | undefined

  async function flushOptions(): Promise<void> {
    clearTimeout(optTimer)
    const pid = optsPackId
    if (!pid || dirtyKeys.size === 0) return
    const keys = [...dirtyKeys]
    const patch: Record<string, string> = {}
    for (const k of keys) patch[k] = opts[k]
    dirtyKeys.clear()
    saveState = 'saving'
    try {
      await App.SavePackOptions(pid, patch)
      if (dirtyKeys.size > 0) saveState = 'pending'
      else {
        saveState = 'saved'
        clearTimeout(savedTimer)
        savedTimer = setTimeout(() => { if (saveState === 'saved') saveState = 'idle' }, 2500)
      }
    } catch (e) {
      // Не вдалось — повертаємо ключі в чергу, щоб наступна зміна їх дописала.
      for (const k of keys) dirtyKeys.add(k)
      saveState = 'error'
      toast(t('pack.game.saveFailed'), 'error')
    }
  }

  function scheduleSave() {
    saveState = 'pending'
    clearTimeout(optTimer)
    optTimer = setTimeout(() => { void flushOptions() }, 600)
  }

  // Завантаження options.txt. Залежить ТІЛЬКИ від packId: усе, що
  // читається всередині (opts, dirtyKeys), обгорнуте в untrack — інакше
  // кожна правка перезапускала б завантаження і затирала введене.
  $effect(() => {
    const pid = packId
    if (!pid) return
    let cancelled = false
    ;(async () => {
      await untrack(() => flushOptions())
      if (cancelled) return
      optionsLoading = true
      try {
        const v = await App.GetPackOptions(pid)
        if (cancelled) return
        optionsView = v ?? null
        // Файла немає — редактор не показуємо (лише створення/копіювання).
        opts = v?.exists ? { ...v.options } : {}
        optsPackId = pid
        saveState = 'idle'
      } catch (e) {
        if (!cancelled) toast(String(e), 'error')
      } finally {
        if (!cancelled) optionsLoading = false
      }
    })()
    return () => { cancelled = true }
  })

  onDestroy(() => { void flushOptions() })

  function setOpt(k: string, v: string) {
    if (opts[k] === v) return
    opts = { ...opts, [k]: v }
    dirtyKeys.add(k)
    scheduleSave()
  }
  function boolOpt(k: string): boolean { return opts[k] === 'true' }
  function setBool(k: string, v: boolean) { setOpt(k, String(v)) }
  function numOpt(k: string, def: number): number {
    const n = parseFloat(opts[k] ?? '')
    return Number.isFinite(n) ? n : def
  }

  // Динамічний максимум GUI-масштабу за розміром екрана (як у старого
  // лаунчера: maxScale = min(ширина/320, висота/240)) — x5, x6, x7...
  const guiScaleMax = $derived.by(() => {
    const w = window.screen?.width || 1920
    const h = window.screen?.height || 1080
    return Math.max(1, Math.min(Math.floor(w / 320), Math.floor(h / 240)))
  })


  // Згортання довгих списків (звук, клавіші) — за замовчуванням згорнуті.
  let showSound = $state(false)
  let showKeys = $state(false)
  let showModKeys = $state(false)

  // Варіанти GUI-масштабу: «Авто», x1…xN за розміром екрана + поточне
  // значення з файлу, якщо воно більше за максимум (інакше список був би порожній).
  const guiScaleOptions = $derived.by((): StOption[] => {
    const list: StOption[] = [{ value: '0', label: t('defaults.guiscale.auto') }]
    for (let i = 1; i <= guiScaleMax; i++) list.push({ value: String(i), label: 'x' + i })
    const cur = opts.guiScale
    if (cur && !list.some((o) => o.value === cur)) list.push({ value: cur, label: 'x' + cur })
    return list
  })

  // Мови гри — компактний fallback-список, поки не завантажиться повний
  // набір з ОРИГІНАЛЬНИХ json-мов Mojang (App.GetGameLanguagesForPack:
  // asset index версії збірки → minecraft/lang/*.json, назви з самих
  // файлів мов). Якщо бекенд відповів (навіть порожнім списком) —
  // використовуємо ЙОГО список, а не цей захардкоджений.
  const OPT_LANGS: [string, string][] = [
    ['en_us', 'English (US)'], ['en_gb', 'English (UK)'], ['uk_ua', 'Українська'],
    ['ru_ru', 'Русский'], ['de_de', 'Deutsch'], ['fr_fr', 'Français'],
    ['es_es', 'Español'], ['pt_br', 'Português (BR)'], ['it_it', 'Italiano'],
    ['pl_pl', 'Polski'], ['tr_tr', 'Türkçe'], ['ja_jp', '日本語'],
    ['ko_kr', '한국어'], ['zh_cn', '简体中文'], ['zh_tw', '繁體中文'],
  ]
  // Актуальний список мов: завантажується з Mojang; fallback — OPT_LANGS.
  let gameLangs = $state<[string, string][]>(OPT_LANGS)
  $effect(() => {
    const pid = packId
    if (!pid) return
    let cancelled = false
    App.GetGameLanguagesForPack(pid)
      .then((list) => {
        if (cancelled || !Array.isArray(list)) return
        // Бекенд повертає [] (не помилку), коли версія гри ще не
        // встановлена локально (loadGameLanguages → os.IsNotExist) —
        // наприклад, збірка щойно створена і ще качається. Раніше це
        // затирало fallback-список ПОРОЖНІМ, і селект лишався майже
        // пустим. Порожню відповідь ігноруємо й лишаємо fallback —
        // оновлюємо лише коли бекенд реально прислав мови.
        if (list.length === 0) return
        const mapped: [string, string][] = list.map(l => [l.code, l.name || l.code])
        // Поточна мова у файлі має БУТИ у списку (навіть якщо її чомусь
        // немає в asset index) — інакше селект покаже порожнє значення.
        if (opts.lang && !mapped.some(([c]) => c === opts.lang)) {
          mapped.push([opts.lang, opts.lang])
        }
        gameLangs = mapped
      })
      .catch(() => { /* офлайн/немає версії — лишаємо fallback-список */ })
    return () => { cancelled = true }
  })

  const OPT_SOUND_CATS = ['master', 'music', 'record', 'weather', 'block', 'hostile', 'neutral', 'player', 'ambient', 'voice']

  // Прив'язки клавіш (у options.txt зберігаються як key_<bind>).
  const OPT_BINDS = [
    'key.attack', 'key.use', 'key.forward', 'key.back', 'key.left', 'key.right',
    'key.jump', 'key.sneak', 'key.sprint', 'key.inventory', 'key.swapOffhand',
    'key.drop', 'key.chat', 'key.command', 'key.playerlist', 'key.pickItem',
    'key.screenshot', 'key.togglePerspective', 'key.fullscreen',
    'key.spectatorOutlines', 'key.smoothCamera', 'key.advancements',
    'key.saveToolbarActivator', 'key.loadToolbarActivator',
    'key.hotbar.1', 'key.hotbar.2', 'key.hotbar.3', 'key.hotbar.4',
    'key.hotbar.5', 'key.hotbar.6', 'key.hotbar.7', 'key.hotbar.8', 'key.hotbar.9',
  ]
  const OPT_BIND_LABELS: Record<string, string> = {
    'key.attack': 'Атака', 'key.use': 'Використати', 'key.forward': 'Вперед',
    'key.back': 'Назад', 'key.left': 'Вліво', 'key.right': 'Вправо',
    'key.jump': 'Стрибок', 'key.sneak': 'Крастись', 'key.sprint': 'Біг',
    'key.inventory': 'Інвентар', 'key.swapOffhand': 'Змінити руку',
    'key.drop': 'Викинути', 'key.chat': 'Чат', 'key.command': 'Команда',
    'key.playerlist': 'Список гравців', 'key.pickItem': 'Взяти блок',
    'key.screenshot': 'Скріншот', 'key.togglePerspective': 'Камера',
    'key.fullscreen': 'Повний екран', 'key.spectatorOutlines': 'Контури',
    'key.smoothCamera': 'Плавна камера', 'key.advancements': 'Досягнення',
    'key.saveToolbarActivator': 'Зберегти панель', 'key.loadToolbarActivator': 'Завантажити панель',
    'key.hotbar.1': 'Слот 1', 'key.hotbar.2': 'Слот 2', 'key.hotbar.3': 'Слот 3',
    'key.hotbar.4': 'Слот 4', 'key.hotbar.5': 'Слот 5', 'key.hotbar.6': 'Слот 6',
    'key.hotbar.7': 'Слот 7', 'key.hotbar.8': 'Слот 8', 'key.hotbar.9': 'Слот 9',
  }

  // Підставити налаштування «За замовчуванням» у options.txt цієї збірки
  // (створює файл, якщо його ще немає). Спершу дописуємо незбережене.
  async function reloadOptions() {
    const v = await App.GetPackOptions(packId)
    optionsView = v ?? null
    opts = v?.options ? { ...v.options } : {}
    optsPackId = packId
    dirtyKeys.clear()
    saveState = 'idle'
  }

  async function applyDefaultsToPack() {
    if (!packId) return
    busy = true
    try {
      await flushOptions()
      await App.ApplyMinecraftDefaultsToPack(packId)
      await reloadOptions()
      toast(t('pack.game.optionsApplied'), 'success')
    } catch (e) {
      toast(String(e), 'error')
    } finally {
      busy = false
    }
  }

  // ── Захоплення клавіш для біндів (options.txt: key_<bind>) ────────────
  // Той самий UX, що в «За замовчуванням» у налаштуваннях лаунчера:
  // клік по бінду → «натисни клавішу…» → Esc/клавіша/кнопка миші записує
  // значення у форматі Minecraft (key.keyboard.w, key.mouse.left). Раніше
  // тут були ТЕКСТОВІ поля, де треба було вручну вписувати key.keyboard.*
  // — незручно і легко помилитись.
  let capturingKey = $state<string | null>(null)

  function startCapture(bind: string) {
    if (capturingKey) capturingKey = null
    capturingKey = bind
  }
  function cancelCapture() {
    capturingKey = null
  }

  function keyEventToMc(e: KeyboardEvent): string | null {
    const code = e.code
    if (/^Key[A-Z]$/.test(code)) return 'key.keyboard.' + code.slice(3).toLowerCase()
    if (/^Digit[0-9]$/.test(code)) return 'key.keyboard.' + code.slice(5)
    if (/^F([1-9]|1[0-2])$/.test(code)) return 'key.keyboard.' + code.toLowerCase()
    const map: Record<string, string> = {
      Space: 'key.keyboard.space', Tab: 'key.keyboard.tab', Enter: 'key.keyboard.enter',
      Backspace: 'key.keyboard.backspace', Delete: 'key.keyboard.delete',
      Insert: 'key.keyboard.insert', Home: 'key.keyboard.home', End: 'key.keyboard.end',
      PageUp: 'key.keyboard.page.up', PageDown: 'key.keyboard.page.down',
      ArrowUp: 'key.keyboard.up', ArrowDown: 'key.keyboard.down',
      ArrowLeft: 'key.keyboard.left', ArrowRight: 'key.keyboard.right',
      ShiftLeft: 'key.keyboard.left.shift', ShiftRight: 'key.keyboard.right.shift',
      ControlLeft: 'key.keyboard.left.control', ControlRight: 'key.keyboard.right.control',
      AltLeft: 'key.keyboard.left.alt', AltRight: 'key.keyboard.right.alt',
      // Символьні клавіші (раніше відсутні — «`» та сусідні клавіші
      // просто не записувались, map[code] ?? null повертав null).
      Backquote: 'key.keyboard.grave.accent',
      Minus: 'key.keyboard.minus', Equal: 'key.keyboard.equal',
      BracketLeft: 'key.keyboard.left.bracket', BracketRight: 'key.keyboard.right.bracket',
      Semicolon: 'key.keyboard.semicolon', Quote: 'key.keyboard.apostrophe',
      Comma: 'key.keyboard.comma', Period: 'key.keyboard.period', Slash: 'key.keyboard.slash',
      Backslash: 'key.keyboard.backslash',
      CapsLock: 'key.keyboard.caps.lock',
      NumpadAdd: 'key.keyboard.keypad.add', NumpadSubtract: 'key.keyboard.keypad.subtract',
      NumpadMultiply: 'key.keyboard.keypad.multiply', NumpadDivide: 'key.keyboard.keypad.divide',
      NumpadDecimal: 'key.keyboard.keypad.decimal', NumpadEnter: 'key.keyboard.keypad.enter',
      Numpad0: 'key.keyboard.keypad.0', Numpad1: 'key.keyboard.keypad.1', Numpad2: 'key.keyboard.keypad.2',
      Numpad3: 'key.keyboard.keypad.3', Numpad4: 'key.keyboard.keypad.4', Numpad5: 'key.keyboard.keypad.5',
      Numpad6: 'key.keyboard.keypad.6', Numpad7: 'key.keyboard.keypad.7', Numpad8: 'key.keyboard.keypad.8',
      Numpad9: 'key.keyboard.keypad.9',
    }
    return map[code] ?? null
  }

  function formatKeyName(mcKey?: string): string {
    if (!mcKey || mcKey === 'key.keyboard.unknown') return t('defaults.key.none')
    if (mcKey.startsWith('key.mouse.')) {
      const m: Record<string, string> = { 'key.mouse.left': 'LMB', 'key.mouse.right': 'RMB', 'key.mouse.middle': 'MMB' }
      return m[mcKey] ?? mcKey
    }
    const symbolNames: Record<string, string> = {
      'key.keyboard.grave.accent': '`', 'key.keyboard.minus': '-', 'key.keyboard.equal': '=',
      'key.keyboard.left.bracket': '[', 'key.keyboard.right.bracket': ']',
      'key.keyboard.semicolon': ';', 'key.keyboard.apostrophe': "'",
      'key.keyboard.comma': ',', 'key.keyboard.period': '.', 'key.keyboard.slash': '/',
      'key.keyboard.backslash': '\\',
      'key.keyboard.keypad.add': 'Num +', 'key.keyboard.keypad.subtract': 'Num -',
      'key.keyboard.keypad.multiply': 'Num *', 'key.keyboard.keypad.divide': 'Num /',
      'key.keyboard.keypad.decimal': 'Num .', 'key.keyboard.keypad.enter': 'Num Enter',
    }
    if (symbolNames[mcKey]) return symbolNames[mcKey]
    return mcKey.replace('key.keyboard.', '').split('.').map((w) => w.charAt(0).toUpperCase() + w.slice(1)).join(' ')
  }

  // ── Справжні (не вгадані) назви біндів модів ────────────────────────
  // options.txt зберігає бінди модів як key_key.<modid>.<name> — сам файл
  // НЕ містить перекладу. Бекенд (App.GetKeyBindLabels) знаходить jar
  // потрібного мода в mods/ і читає звідти РЕАЛЬНИЙ підпис із
  // assets/<modid>/lang/<lang>.json (мова гри збірки, з fallback на
  // en_us). Якщо мода з таким modid у mods/ немає — бекенд просто не
  // включає його бінди в результат, і ми їх також не показуємо.
  let modBindLabels = $state<Record<string, KeyBindLabel>>({})

  const modBindKeysRaw = $derived(
    Object.keys(opts)
      .filter((k) => k.startsWith('key_') && !OPT_BINDS.includes(k.slice(4)))
      .map((k) => k.slice(4))
  )

  $effect(() => {
    const pid = packId
    const keys = modBindKeysRaw
    if (!pid || keys.length === 0) {
      modBindLabels = {}
      return
    }
    let cancelled = false
    App.GetKeyBindLabels(pid, keys)
      .then((v) => { if (!cancelled) modBindLabels = v ?? {} })
      .catch(() => { if (!cancelled) modBindLabels = {} })
    return () => { cancelled = true }
  })

  // Групуємо бінди по модах — «як в Minecraft»: заголовок мода, під ним
  // лише його бінди (без назви мода в кожному рядку), групи йдуть
  // відсортовані за назвою мода, бінди всередині групи — за назвою бінда.
  type ModBindGroup = { modId: string; modName: string; binds: string[] }
  const modBindGroups = $derived.by((): ModBindGroup[] => {
    const groups = new Map<string, ModBindGroup>()
    for (const bind of modBindKeysRaw) {
      const l = modBindLabels[bind]
      if (!l) continue // мода немає в mods/ — бінд не показуємо
      let g = groups.get(l.modId)
      if (!g) {
        g = { modId: l.modId, modName: l.modName, binds: [] }
        groups.set(l.modId, g)
      }
      g.binds.push(bind)
    }
    const result = Array.from(groups.values())
    for (const g of result) {
      g.binds.sort((a, b) => modBindLabels[a].label.localeCompare(modBindLabels[b].label))
    }
    result.sort((a, b) => a.modName.localeCompare(b.modName))
    return result
  })

  function modBindLabel(bind: string): string {
    const l = modBindLabels[bind]
    if (!l) return bind
    return l.category ? `${l.category}: ${l.label}` : l.label
  }


  $effect(() => {
    if (!capturingKey) return
    const onKey = (e: KeyboardEvent) => {
      e.preventDefault()
      e.stopPropagation()
      if (e.key === 'Escape') {
        setOpt('key_' + capturingKey!, 'key.keyboard.unknown')
      } else {
        const name = keyEventToMc(e)
        if (name) setOpt('key_' + capturingKey!, name)
      }
      cancelCapture()
    }
    const onMouse = (e: MouseEvent) => {
      if (e.target instanceof Element && (e.target as HTMLElement).closest('.opt-kb-capture')) return
      const name = e.button === 0 ? 'key.mouse.left' : e.button === 1 ? 'key.mouse.middle' : e.button === 2 ? 'key.mouse.right' : null
      if (name) setOpt('key_' + capturingKey!, name)
      cancelCapture()
    }
    window.addEventListener('keydown', onKey, true)
    window.addEventListener('mousedown', onMouse, true)
    return () => {
      window.removeEventListener('keydown', onKey, true)
      window.removeEventListener('mousedown', onMouse, true)
    }
  })

  // ── Копіювання налаштувань з іншої збірки ──
  // Копіюються ЛИШЕ «видимі» лаунчеру опції (CopyVisible на бекенді),
  // решта options.txt цільової збірки не змінюється.
  let copyModal = $state(false)
  let copyPacks = $state<BuildView[]>([])
  let copying = $state(false)

  async function openCopyModal() {
    try {
      // ?? [] — захист від null від бекенду (коли список збірок порожній).
      copyPacks = ((await App.GetBuilds()) ?? []).filter((b) => b.installed && b.build.id !== packId)
    } catch (e) {
      toast(String(e), 'error')
      return
    }
    copyModal = true
  }
  async function doCopy(srcId: string) {
    if (!packId) return
    copying = true
    try {
      await flushOptions()
      await App.CopyPackOptionsFromPack(srcId, packId)
      copyModal = false
      await reloadOptions()
      toast(t('pack.game.optionsCopied'), 'success')
    } catch (e) {
      toast(String(e), 'error')
    } finally {
      copying = false
    }
  }
</script>

<!-- Розділ «Гра» збірки. Розмітка й стилі 1 в 1 як у налаштуваннях
     лаунчера (SCard + .st-row + .st-switch + .st-select із settings.css).
     Усе зберігається автоматично: конфіг збірки — через debounce у
     PackInstancePage, options.txt — flushOptions() вище. -->

<div class="st-root">
  <div class="gs-top">
    {#if saveState === 'pending' || saveState === 'saving'}
      <span class="st-autosave"><i class="ti ti-loader spin" style="color:var(--text-mute)"></i>{t('pack.game.saving')}</span>
    {:else if saveState === 'error'}
      <span class="st-autosave"><i class="ti ti-alert-circle" style="color:var(--red)"></i>{t('pack.game.notSaved')}</span>
    {:else}
      <span class="st-autosave"><i class="ti ti-circle-check"></i>{t('st.autosave')}</span>
    {/if}
    <span class="st-note"><i class="ti ti-info-circle"></i><span>{t('pack.game.note')}</span></span>
  </div>

  <!-- ═══ Акаунт ═══ -->
  <SCard icon="ti-user" tone="orange" title={t('pack.game.account')}>
    <div class="st-row">
      <div class="st-row-text">
        <div class="st-row-name">{t('pack.game.accountOverride')}</div>
        <div class="st-row-hint">{t('pack.game.accountOverrideHint')}</div>
      </div>
      <div class="st-row-ctl">
        <button type="button" class="st-switch" class:on={!!cfg.accountIdOverride} role="switch"
                aria-checked={!!cfg.accountIdOverride} aria-label={t('pack.game.accountOverride')}
                onclick={() => onUpdate('accountIdOverride', cfg.accountIdOverride ? '' : (accounts[0]?.id ?? ''))}></button>
      </div>
    </div>
    {#if cfg.accountIdOverride}
      <div class="st-row sub">
        <div class="st-row-text"><div class="st-row-name">{t('pack.game.account')}</div></div>
        <div class="st-row-ctl">
          <StSelect value={cfg.accountIdOverride} options={accountOptions}
                    placeholder={t('pack.game.accountPick')} ariaLabel={t('pack.game.account')}
                    onchange={(v) => onUpdate('accountIdOverride', v)} />
        </div>
      </div>
    {/if}
  </SCard>

  <!-- ═══ Скін збірки ═══ -->
  <SCard icon="ti-hanger" tone="violet" title={t('pack.game.skinBuild')}>
    {#if !skinLicensed}
      <div class="st-pad">
        <div class="st-note"><i class="ti ti-lock"></i><span>{t('pack.game.skinLicensedOnly')}</span></div>
      </div>
    {:else}
      <div class="st-row">
        <div class="st-row-text">
          <div class="st-row-name">{t('pack.game.skinEnable')}</div>
          <div class="st-row-hint">{t('pack.game.skinBuildHint')}</div>
        </div>
        <div class="st-row-ctl">
          <button type="button" class="st-switch" class:on={!!cfg.skinPresetId} role="switch"
                  aria-checked={!!cfg.skinPresetId} aria-label={t('pack.game.skinEnable')}
                  onclick={() => toggleBuildSkin()}></button>
        </div>
      </div>

      {#if cfg.skinPresetId}
        {#if restoreStatus?.status === 'restoring'}
          <div class="st-pad">
            <div class="sk-status"><i class="ti ti-loader spin"></i>
              <span>{t('pack.game.skinRestoring', { attempt: restoreStatus.attempt ?? 1, max: restoreStatus.maxAttempts ?? 6 })}</span></div>
          </div>
        {:else if restoreStatus?.status === 'failed'}
          <div class="st-pad">
            <div class="sk-status bad"><i class="ti ti-alert-triangle"></i>
              <span>{t('pack.game.skinRestoreFailed')}{#if restoreStatus.error}<span class="sk-err">{restoreStatus.error}</span>{/if}</span></div>
          </div>
        {:else if restoreStatus?.status === 'done'}
          <div class="st-pad">
            <div class="sk-status ok"><i class="ti ti-check"></i><span>{t('pack.game.skinRestoreDone')}</span></div>
          </div>
        {/if}

        <div class="st-row sub">
          <div class="st-row-text"><div class="st-row-name">{t('pack.game.skinPreset')}</div></div>
          <div class="st-row-ctl">
            <!-- Прев'ю пресета: квадратна голова + прямокутник з передньою
                 панеллю плаща (ті самі кропи, що в Гардеробі) -->
            {#if selectedThumb?.head}
              <img class="sk-head" src={selectedThumb.head} alt="" />
              {#if selectedThumb.cape}
                <img class="sk-cape" src={selectedThumb.cape} alt="" />
              {:else}
                <div class="sk-cape empty" title={t('pack.game.skinNoCape')}><i class="ti ti-minus"></i></div>
              {/if}
            {/if}
            <StSelect value={cfg.skinPresetId} options={skinPresetOptions}
                      placeholder={t('pack.game.skinNoPresets')} ariaLabel={t('pack.game.skinPreset')}
                      onchange={(v) => onUpdate('skinPresetId', v)} />
          </div>
        </div>

        <div class="st-row sub">
          <div class="st-row-text">
            <div class="st-row-name">{t('pack.game.skinRestore')}</div>
            <div class="st-row-hint">{t('pack.game.skinRestoreHint')}</div>
          </div>
          <div class="st-row-ctl">
            <button type="button" class="st-switch" class:on={cfg.skinRestoreOnExit !== false} role="switch"
                    aria-checked={cfg.skinRestoreOnExit !== false} aria-label={t('pack.game.skinRestore')}
                    onclick={() => onUpdate('skinRestoreOnExit', cfg.skinRestoreOnExit === false)}></button>
          </div>
        </div>
      {/if}
    {/if}
  </SCard>

  <!-- ═══ Автоприєднання ═══ -->
  <SCard icon="ti-plug-connected" tone="blue" title={t('pack.game.autoJoin')}>
    <div class="st-row">
      <div class="st-row-text">
        <div class="st-row-name">{t('pack.game.autoJoin')}</div>
        <div class="st-row-hint">{t('pack.game.autoJoinHint')}</div>
      </div>
      <div class="st-row-ctl">
        <button type="button" class="st-switch" class:on={cfg.autoJoinEnabled} role="switch"
                aria-checked={cfg.autoJoinEnabled} aria-label={t('pack.game.autoJoin')}
                onclick={() => onUpdate('autoJoinEnabled', !cfg.autoJoinEnabled)}></button>
      </div>
    </div>

    {#if cfg.autoJoinEnabled}
      <div class="st-row sub" bind:this={autoJoinRef}>
        <div class="st-row-text"><div class="st-row-name">{t('pack.game.autoJoinType')}</div></div>
        <div class="st-row-ctl">
          <div class="st-seg labeled" role="radiogroup" aria-label={t('pack.game.autoJoinType')}>
            {#if supportsWorldJoin}
              <button type="button" role="radio" aria-checked={cfg.autoJoinType === 'world'} class:on={cfg.autoJoinType === 'world'} onclick={() => onUpdate('autoJoinType', 'world')}>
                <i class="ti ti-world"></i> {t('pack.game.autoJoinTypeWorld')}
              </button>
            {/if}
            <button type="button" role="radio" aria-checked={cfg.autoJoinType === 'server'} class:on={cfg.autoJoinType === 'server'} onclick={() => onUpdate('autoJoinType', 'server')}>
              <i class="ti ti-server-2"></i> {t('pack.game.autoJoinTypeServer')}
            </button>
          </div>
        </div>
      </div>

      {#if cfg.autoJoinType === 'world'}
        {#if !supportsWorldJoin}
          <div class="st-pad"><div class="st-note warn"><i class="ti ti-alert-triangle"></i><span>{t('pack.game.autoJoinWorldUnsupported')}</span></div></div>
        {:else if isSlowWorldJoin}
          <div class="st-pad"><div class="st-note warn"><i class="ti ti-clock-hour-4"></i><span>{t('pack.game.autoJoinWorldSlow')}</span></div></div>
        {/if}
        <div class="st-row sub">
          <div class="st-row-text"><div class="st-row-name">{t('pack.game.autoJoinWorld')}</div></div>
          <div class="st-row-ctl">
            {#if worlds.length === 0}
              <span class="st-row-hint" style="margin:0">{t('pack.game.autoJoinWorldEmpty')}</span>
            {:else}
              <StSelect value={cfg.autoJoinWorld}
                        options={[{ value: '', label: t('pack.game.autoJoinWorldPlaceholder'), icon: 'ti-map' }, ...worldOptions]}
                        ariaLabel={t('pack.game.autoJoinWorld')}
                        onchange={(v) => onUpdate('autoJoinWorld', v)} />
            {/if}
            <button type="button" class="st-iconbtn" onclick={onRefreshWorlds}
                    title={t('pack.game.refreshWorlds')} aria-label={t('pack.game.refreshWorlds')}><i class="ti ti-refresh"></i></button>
          </div>
        </div>
      {:else}
        <div class="st-row sub">
          <div class="st-row-text"><div class="st-row-name">{t('pack.game.autoJoinServer')}</div></div>
          <div class="st-row-ctl wrap">
            <StSelect editable minWidth="220px" value={cfg.autoJoinServer} options={serverOptions}
                      placeholder={t('pack.game.autoJoinServerPlaceholder')}
                      emptyText={t('pack.game.autoJoinServerNoMatch')}
                      ariaLabel={t('pack.game.autoJoinServer')}
                      onchange={(v) => { serverEdited = true; onUpdate('autoJoinServer', v) }} />
            {#if cfg.autoJoinServer}
              {@const pr = pings[cfg.autoJoinServer]}
              {#if pr}
                <span class="st-tag" class:ok={pr.online} class:bad={!pr.online} title={pr.motd || pr.error || ''}>
                  <i class="ti {pr.online ? 'ti-wifi' : 'ti-wifi-off'}"></i>
                  {#if pr.online}{pr.players ?? 0}/{pr.maxPlayers ?? 0}{:else}{t('pack.game.serverOffline')}{/if}
                </span>
              {:else if pings[cfg.autoJoinServer] === null}
                <span class="st-tag"><i class="ti ti-loader spin"></i></span>
              {/if}
              <button type="button" class="st-iconbtn" onclick={pingCurrent}
                      title={t('pack.game.pingServer')} aria-label={t('pack.game.pingServer')}><i class="ti ti-refresh"></i></button>
            {/if}
          </div>
        </div>
      {/if}

      <div class="st-pad">
        <div class="st-note">
          <i class="ti ti-rocket"></i>
          <span>
            {t('pack.game.autoJoinAfter')}
            {#if cfg.autoJoinType === 'world'}
              {#if cfg.autoJoinWorld}<b>{t('pack.game.autoJoinSummaryWorld', { name: cfg.autoJoinWorld })}</b>
              {:else}<b class="dim">{t('pack.game.autoJoinSummaryWorldNone')}</b>{/if}
            {:else}
              {#if cfg.autoJoinServer}<b>{t('pack.game.autoJoinSummaryServer', { addr: cfg.autoJoinServer })}</b>
              {:else}<b class="dim">{t('pack.game.autoJoinSummaryServerNone')}</b>{/if}
            {/if}
          </span>
        </div>
      </div>
    {/if}
  </SCard>

  <!-- ═══ Сервер разом зі збіркою (та сама картка, що й на сторінці сервера) ═══ -->
  {#if packId}<PackServerLinkCard {packId} />{/if}

  <!-- ═══ options.txt ═══ -->
  <SCard icon="ti-settings" tone="green" title={t('pack.game.options')} desc={t('pack.game.optionsHint')}>
    {#if optionsLoading && !optionsView}
      <div class="st-empty"><i class="ti ti-loader spin"></i></div>
    {:else if optionsView}
      <div class="st-pad">
        <div class="st-path" style="margin:0">{t('pack.game.optionsPath', { path: optionsView.path })}</div>
        {#if !optionsView.exists}
          <div class="st-note" style="margin-top:10px"><i class="ti ti-info-circle"></i><span>{t('pack.game.optionsMissing')}</span></div>
        {:else if Object.keys(opts).length === 0}
          <div class="st-note" style="margin-top:10px"><i class="ti ti-info-circle"></i><span>{t('pack.game.optionsEmptyVisible')}</span></div>
        {/if}
      </div>
      <div class="st-row">
        <div class="st-row-text">
          <div class="st-row-name">{optionsView.exists ? t('pack.game.optionsApply') : t('pack.game.optionsCreate')}</div>
          <div class="st-row-hint">{t('pack.game.optionsApplyHint')}</div>
        </div>
        <div class="st-row-ctl">
          <button type="button" class="btn btn-ghost btn-sm" disabled={busy} onclick={applyDefaultsToPack}>
            <i class="ti ti-wand"></i> {optionsView.exists ? t('pack.game.optionsApplyBtn') : t('pack.game.optionsCreateBtn')}
          </button>
        </div>
      </div>
      <div class="st-row">
        <div class="st-row-text">
          <div class="st-row-name">{t('pack.game.optionsCopy')}</div>
          <div class="st-row-hint">{t('pack.game.optionsCopyRowHint')}</div>
        </div>
        <div class="st-row-ctl">
          <button type="button" class="btn btn-ghost btn-sm" disabled={copying || busy} onclick={openCopyModal}>
            <i class="ti ti-copy"></i> {t('pack.game.optionsCopyBtn')}
          </button>
        </div>
      </div>
    {/if}
  </SCard>

  {#if optionsView?.exists && Object.keys(opts).length > 0}
    <!-- Мова та GUI -->
    {#if 'lang' in opts || 'guiScale' in opts}
      <SCard icon="ti-language" tone="orange" title={t('defaults.section.lang')}>
        {#if 'lang' in opts}
          <div class="st-row">
            <div class="st-row-text"><div class="st-row-name">{t('defaults.language')}</div></div>
            <div class="st-row-ctl">
              <StSelect value={opts.lang} options={gameLangs.map(([value, label]) => ({ value, label }))}
                        ariaLabel={t('defaults.language')} onchange={(v) => setOpt('lang', v)} />
            </div>
          </div>
        {/if}
        {#if 'guiScale' in opts}
          <div class="st-row">
            <div class="st-row-text">
              <div class="st-row-name">{t('defaults.guiscale')}</div>
              <div class="st-row-hint">{t('defaults.guiscaleHint')}</div>
            </div>
            <div class="st-row-ctl">
              <StSelect value={opts.guiScale} options={guiScaleOptions}
                        ariaLabel={t('defaults.guiscale')} onchange={(v) => setOpt('guiScale', v)} />
            </div>
          </div>
        {/if}
      </SCard>
    {/if}

    <!-- Екран -->
    {#if 'fullscreen' in opts || 'enableVsync' in opts || 'maxFps' in opts}
      <SCard icon="ti-device-desktop" tone="blue" title={t('defaults.section.display')}>
        {#if 'fullscreen' in opts}
          <div class="st-row">
            <div class="st-row-text"><div class="st-row-name">{t('defaults.fullscreen')}</div></div>
            <div class="st-row-ctl">
              <button type="button" class="st-switch" class:on={boolOpt('fullscreen')} role="switch" aria-checked={boolOpt('fullscreen')}
                      aria-label={t('defaults.fullscreen')} onclick={() => setBool('fullscreen', !boolOpt('fullscreen'))}></button>
            </div>
          </div>
        {/if}
        {#if 'enableVsync' in opts}
          <div class="st-row">
            <div class="st-row-text"><div class="st-row-name">{t('defaults.vsync')}</div></div>
            <div class="st-row-ctl">
              <button type="button" class="st-switch" class:on={boolOpt('enableVsync')} role="switch" aria-checked={boolOpt('enableVsync')}
                      aria-label={t('defaults.vsync')} onclick={() => setBool('enableVsync', !boolOpt('enableVsync'))}></button>
            </div>
          </div>
        {/if}
        {#if 'maxFps' in opts}
          <div class="st-row">
            <div class="st-row-text"><div class="st-row-name">{t('defaults.maxfps')}</div></div>
            <div class="st-row-ctl">
              <div class="st-slider gs-slider">
                <input type="range" min="10" max="260" step="10" value={numOpt('maxFps', 120)} aria-label={t('defaults.maxfps')}
                       oninput={(e) => setOpt('maxFps', String(Number(e.currentTarget.value)))} />
                <span class="st-slider-val">{numOpt('maxFps', 120) >= 260 ? t('defaults.maxfps.unlimited') : numOpt('maxFps', 120) + ' FPS'}</span>
              </div>
            </div>
          </div>
        {/if}
      </SCard>
    {/if}

    <!-- Геймплей -->
    {#if 'autoJump' in opts || 'invertYMouse' in opts || 'mouseSensitivity' in opts || 'fov' in opts || 'darkMojangStudiosBackground' in opts}
      <SCard icon="ti-device-gamepad-2" tone="violet" title={t('defaults.section.gameplay')}>
        {#if 'autoJump' in opts}
          <div class="st-row">
            <div class="st-row-text"><div class="st-row-name">{t('defaults.autojump')}</div></div>
            <div class="st-row-ctl">
              <button type="button" class="st-switch" class:on={boolOpt('autoJump')} role="switch" aria-checked={boolOpt('autoJump')}
                      aria-label={t('defaults.autojump')} onclick={() => setBool('autoJump', !boolOpt('autoJump'))}></button>
            </div>
          </div>
        {/if}
        {#if 'invertYMouse' in opts}
          <div class="st-row">
            <div class="st-row-text"><div class="st-row-name">{t('defaults.invertyMouse')}</div></div>
            <div class="st-row-ctl">
              <button type="button" class="st-switch" class:on={boolOpt('invertYMouse')} role="switch" aria-checked={boolOpt('invertYMouse')}
                      aria-label={t('defaults.invertyMouse')} onclick={() => setBool('invertYMouse', !boolOpt('invertYMouse'))}></button>
            </div>
          </div>
        {/if}
        {#if 'mouseSensitivity' in opts}
          <div class="st-row">
            <div class="st-row-text"><div class="st-row-name">{t('defaults.sensitivity')}</div></div>
            <div class="st-row-ctl">
              <div class="st-slider gs-slider">
                <input type="range" min="0" max="200" step="1" value={Math.round(numOpt('mouseSensitivity', 0.5) * 200)}
                       aria-label={t('defaults.sensitivity')}
                       oninput={(e) => setOpt('mouseSensitivity', String(Number(e.currentTarget.value) / 200))} />
                <span class="st-slider-val">{Math.round(numOpt('mouseSensitivity', 0.5) * 200)}%</span>
              </div>
            </div>
          </div>
        {/if}
        {#if 'fov' in opts}
          <div class="st-row">
            <div class="st-row-text"><div class="st-row-name">{t('defaults.fov')}</div></div>
            <div class="st-row-ctl">
              <div class="st-slider gs-slider">
                <input type="range" min="0" max="100" step="1" value={Math.round(numOpt('fov', 0) * 100)}
                       aria-label={t('defaults.fov')}
                       oninput={(e) => setOpt('fov', String(Number(e.currentTarget.value) / 100))} />
                <span class="st-slider-val">{Math.round(70 + numOpt('fov', 0) * 40)}°</span>
              </div>
            </div>
          </div>
        {/if}
        {#if 'darkMojangStudiosBackground' in opts}
          <div class="st-row">
            <div class="st-row-text"><div class="st-row-name">{t('defaults.darkMojang')}</div></div>
            <div class="st-row-ctl">
              <button type="button" class="st-switch" class:on={boolOpt('darkMojangStudiosBackground')} role="switch"
                      aria-checked={boolOpt('darkMojangStudiosBackground')} aria-label={t('defaults.darkMojang')}
                      onclick={() => setBool('darkMojangStudiosBackground', !boolOpt('darkMojangStudiosBackground'))}></button>
            </div>
          </div>
        {/if}
      </SCard>
    {/if}

    <!-- Звук -->
    {#if OPT_SOUND_CATS.some((c) => ('soundCategory_' + c) in opts)}
      {#snippet soundAct()}
        <button type="button" class="st-iconbtn" onclick={() => (showSound = !showSound)}
                aria-label={showSound ? t('pack.game.hide') : t('pack.game.show')} aria-expanded={showSound}>
          <i class="ti {showSound ? 'ti-chevron-up' : 'ti-chevron-down'}"></i>
        </button>
      {/snippet}
      {#if showSound}
        <SCard icon="ti-volume" tone="green" title={t('defaults.section.sound')} actions={soundAct}>
          {#each OPT_SOUND_CATS as cat}
            {#if ('soundCategory_' + cat) in opts}
              <div class="st-row">
                <div class="st-row-text"><div class="st-row-name">{t('defaults.sound.' + cat)}</div></div>
                <div class="st-row-ctl">
                  <div class="st-slider gs-slider">
                    <input type="range" min="0" max="100" step="1" value={Math.round(numOpt('soundCategory_' + cat, 1) * 100)}
                           aria-label={t('defaults.sound.' + cat)}
                           oninput={(e) => setOpt('soundCategory_' + cat, String(Number(e.currentTarget.value) / 100))} />
                    <span class="st-slider-val">{Math.round(numOpt('soundCategory_' + cat, 1) * 100)}%</span>
                  </div>
                </div>
              </div>
            {/if}
          {/each}
        </SCard>
      {:else}
        <SCard icon="ti-volume" tone="green" title={t('defaults.section.sound')} actions={soundAct} />
      {/if}
    {/if}

    <!-- Клавіші -->
    {#if OPT_BINDS.some((b) => ('key_' + b) in opts)}
      {#snippet keysAct()}
        <button type="button" class="st-iconbtn" onclick={() => (showKeys = !showKeys)}
                aria-label={showKeys ? t('pack.game.hide') : t('pack.game.show')} aria-expanded={showKeys}>
          <i class="ti {showKeys ? 'ti-chevron-up' : 'ti-chevron-down'}"></i>
        </button>
      {/snippet}
      {#if showKeys}
        <SCard icon="ti-keyboard" tone="orange" title={t('defaults.section.keybinds')} actions={keysAct}>
          <div class="st-pad">
            <div class="st-note" style="margin-bottom:12px"><i class="ti ti-info-circle"></i><span>{t('defaults.key.pressHint')}</span></div>
            <div class="kb-grid">
              {#each OPT_BINDS as bind}
                {#if ('key_' + bind) in opts}
                  <button type="button" class="kb-row" class:capturing={capturingKey === bind} onclick={() => startCapture(bind)}>
                    <span class="kb-label">{OPT_BIND_LABELS[bind] ?? bind}</span>
                    <span class="kb-key mono">{capturingKey === bind ? t('defaults.key.waiting') : formatKeyName(opts['key_' + bind])}</span>
                  </button>
                {/if}
              {/each}
            </div>
          </div>
        </SCard>
      {:else}
        <SCard icon="ti-keyboard" tone="orange" title={t('defaults.section.keybinds')} actions={keysAct} />
      {/if}
    {/if}

    <!-- Клавіші модів: назви беруться з lang-файлів модів (GetKeyBindLabels) -->
    {#if modBindGroups.length > 0}
      {#snippet modKeysAct()}
        <button type="button" class="st-iconbtn" onclick={() => (showModKeys = !showModKeys)}
                aria-label={showModKeys ? t('pack.game.hide') : t('pack.game.show')} aria-expanded={showModKeys}>
          <i class="ti {showModKeys ? 'ti-chevron-up' : 'ti-chevron-down'}"></i>
        </button>
      {/snippet}
      {#if showModKeys}
        <SCard icon="ti-puzzle" tone="orange" title={t('defaults.section.modKeybinds')} actions={modKeysAct}>
          <div class="st-pad">
            <div class="st-note" style="margin-bottom:12px"><i class="ti ti-info-circle"></i><span>{t('defaults.key.pressHint')}</span></div>
            {#each modBindGroups as group (group.modId)}
              <div class="kb-group">
                <div class="kb-group-title">{group.modName}</div>
                <div class="kb-grid">
                  {#each group.binds as bind}
                    <button type="button" class="kb-row" class:capturing={capturingKey === bind} onclick={() => startCapture(bind)} title={bind}>
                      <span class="kb-label">{modBindLabel(bind)}</span>
                      <span class="kb-key mono">{capturingKey === bind ? t('defaults.key.waiting') : formatKeyName(opts['key_' + bind])}</span>
                    </button>
                  {/each}
                </div>
              </div>
            {/each}
          </div>
        </SCard>
      {:else}
        <SCard icon="ti-puzzle" tone="orange" title={t('defaults.section.modKeybinds')} actions={modKeysAct} />
      {/if}
    {/if}
  {/if}
</div>

<!-- Копіювання налаштувань з іншої збірки -->
{#if copyModal}
  <UiModal
    open={true}
    size="md"
    title={t('pack.game.optionsCopyTitle')}
    icon="ti-copy"
    color="var(--violet)"
    zIndex="2100000"
    locked={copying}
    onclose={() => { if (!copying) copyModal = false }}
  >
    <div class="st-note" style="margin-bottom:12px"><i class="ti ti-info-circle"></i><span>{t('pack.game.optionsCopyHint')}</span></div>
    {#if copyPacks.length === 0}
      <div class="st-note">{t('defaults.import.noPacks')}</div>
    {:else}
      <div class="cp-list">
        {#each copyPacks as cp (cp.build.id)}
          <button type="button" class="cp-item" style="--pcol:{cp.build.color}" disabled={copying} onclick={() => doCopy(cp.build.id)}>
            <div class="cp-ic"><i class="ti {cp.build.icon || 'ti-puzzle'}"></i></div>
            <div class="cp-name">{cp.build.name}</div>
            <div class="cp-meta mono">{cp.build.mcVersion} · {cp.build.loaderType}</div>
          </button>
        {/each}
      </div>
    {/if}
  </UiModal>
{/if}

<style>
  .gs-top { display: flex; align-items: center; gap: 16px; flex-wrap: wrap; }
  .gs-top .st-note { flex: 1 1 320px; min-width: 0; }

  /* Повзунки options.txt: фіксована ширина, щоб підпис значення не стрибав */
  .gs-slider { width: 300px; max-width: 100%; }
  .gs-slider :global(.st-slider-val) { min-width: 84px; }

  /* Прев'ю пресета: квадрат-голова + прямокутник-плащ із заокругленими кутами */
  .sk-head, .sk-cape {
    flex: none; object-fit: cover; image-rendering: pixelated;
    border: 1px solid var(--border-hi); box-sizing: border-box;
  }
  .sk-head { width: 40px; height: 40px; border-radius: 10px; }
  .sk-cape { width: 34px; height: 40px; border-radius: 9px; }
  .sk-cape.empty {
    display: flex; align-items: center; justify-content: center;
    background: var(--bg-input-hi); color: var(--text-dim); font-size: 13px; border-style: dashed;
  }

  /* Статус повернення скіна */
  .sk-status {
    display: flex; align-items: flex-start; gap: 8px; padding: 10px 12px; font-size: 12.5px; line-height: 1.45;
    border-radius: var(--r-md); border: 1px solid rgba(59, 130, 246, .3); background: rgba(59, 130, 246, .08);
  }
  .sk-status i { font-size: 15px; margin-top: 1px; flex: none; }
  .sk-status.ok { border-color: rgba(16, 185, 129, .3); background: rgba(16, 185, 129, .08); }
  .sk-status.ok i { color: var(--green); }
  .sk-status.bad { border-color: rgba(239, 68, 68, .3); background: rgba(239, 68, 68, .08); }
  .sk-status.bad i { color: var(--red); }
  .sk-err { display: block; margin-top: 2px; font: 11px var(--font-mono); color: var(--text-mute); word-break: break-word; }

  .st-note.warn { color: var(--yellow); }
  .st-note b { color: var(--text); font-weight: 700; }
  .st-note b.dim { color: var(--text-dim); font-weight: 600; }

  /* Бінди клавіш — той самий вигляд, що в «Гра за умовч.» налаштувань */
  .kb-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 6px; }
  @media (max-width: 900px) { .kb-grid { grid-template-columns: 1fr; } }
  .kb-row {
    display: flex; align-items: center; justify-content: space-between; gap: 10px;
    padding: 8px 12px; border-radius: var(--r-sm); border: 1px solid var(--border);
    background: var(--bg-input); color: var(--text); cursor: pointer; transition: .15s; font-family: inherit;
  }
  .kb-row:hover { border-color: var(--border-hi); }
  .kb-row.capturing { border-color: var(--orange); box-shadow: 0 0 0 2px rgba(var(--orange-rgb), .25); }
  .kb-label { font-size: 12px; font-weight: 600; }
  .kb-key {
    font-size: 11.5px; font-weight: 700; color: var(--orange);
    background: rgba(var(--orange-rgb), .1); border: 1px solid rgba(var(--orange-rgb), .25);
    padding: 3px 8px; border-radius: 6px; white-space: nowrap;
  }
  .kb-row.capturing .kb-key { color: #fff; background: var(--orange); }
  .kb-group + .kb-group { margin-top: 16px; padding-top: 16px; border-top: 1px solid var(--border); }
  .kb-group-title { text-align: center; font-size: 12.5px; font-weight: 700; margin-bottom: 8px; }

  /* Копіювання з іншої збірки (модалка) */
  .cp-list { display: flex; flex-direction: column; gap: 6px; max-height: 320px; overflow-y: auto; }
  .cp-item {
    display: flex; align-items: center; gap: 10px; width: 100%;
    padding: 9px 12px; border-radius: var(--r-sm); border: 1px solid var(--border);
    background: var(--bg-input); cursor: pointer; transition: .15s; color: var(--text); font-family: inherit;
  }
  .cp-item:hover { border-color: var(--border-hi); }
  .cp-ic {
    width: 28px; height: 28px; border-radius: 8px; flex-shrink: 0;
    background: color-mix(in srgb, var(--pcol, var(--orange)) 18%, var(--bg-input-hi));
    color: var(--pcol, var(--orange)); display: flex; align-items: center; justify-content: center; font-size: 15px;
  }
  .cp-name { font-size: 12.5px; font-weight: 700; flex: 1; text-align: left; }
  .cp-meta { font-size: var(--fs-min); color: var(--text-mute); }
</style>
