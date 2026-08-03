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
  import { t } from './i18n.svelte'
  import { App } from './wails'
  import { toast, confirmDialog } from './toast.svelte'
  import { packAsset } from './packAssets.svelte'
  import { getPackUI, setPackUI, readContentScroll, restoreContentScroll } from './uiState'
  import PackServersSection from './PackServersSection.svelte'
  import PackBasicSection from './PackBasicSection.svelte'
  import PackPerfSection from './PackPerfSection.svelte'
  import PackGameSection from './PackGameSection.svelte'
  import PackFilesPanel from './PackFilesPanel.svelte'
  import PackModsSection from './PackModsSection.svelte'
  import GameConsole from './GameConsole.svelte'
  import type { PackIndexEntry, InstanceConfig, InstanceServer, Settings, BuildView, Account, BuildKind, LaunchProgress } from './types'
  import { LAUNCH_STAGES, launchStageLabel, downloadStageLabel } from './launchStages'

  let {
    pack,
    settings,
    playtimeSeconds,
    gameRunning,
    buildView = null,
    launchProgress = null,
    consoleSignal = 0,
    onBack,
    onLaunch,
    onStop,
    onDownload,
    onDelete,
    onPackUpdated = () => {},
    fmtPlaytime
  } = $props<{
    pack: PackIndexEntry
    settings: Settings
    playtimeSeconds: number
    gameRunning: boolean
    buildView: BuildView | null
    launchProgress?: LaunchProgress | null
    // Лічильник запитів «відкрити консоль» з модалки краху (App.svelte):
    // при кожному інкременті сторінка перемикається на вкладку «Консоль».
    consoleSignal?: number
    onBack: () => void
    onLaunch: (id: string) => void
    onStop: () => void
    onDownload: (id: string) => void
    onDelete: (id: string) => void
    // Після редагування кастомної збірки (назва/іконка/колір/версія) —
    // оновлення списку збірок, щоб сторінка/сайдбар показали зміни одразу.
    onPackUpdated?: () => void
    fmtPlaytime: (s: number) => string
  }>()

  let tab = $state<'servers' | 'basic' | 'perf' | 'game' | 'files' | 'mods' | 'console'>('servers')
  let actionsOpen = $state(false)

  function closeActionsMenu() { actionsOpen = false }

  // ── Відновлення/збереження «останнього вікна» збірки ──────────────────
  // Вкладка + скрол сторінки збірки персистяться на кожній зміні і при
  // виході (перехід в іншу збірку, закриття лаунчера). При відкритті збірки
  // повертаємось саме туди, де були — навіть якщо між цим відвідали іншу.
  let viewScrollTimer: ReturnType<typeof setTimeout> | undefined

  function persistViewState() {
    setPackUI(pack.id, { tab, scroll: readContentScroll() })
  }

  // Вкладка змінилась — зберігаємо одразу.
  $effect(() => {
    const t = tab
    setPackUI(pack.id, { tab: t, scroll: readContentScroll() })
  })

  // Запит «відкрити консоль» з модалки краху (App.svelte): перемикаємось
  // на вкладку «Консоль» при кожному інкременті лічильника.
  $effect(() => {
    if (consoleSignal > 0) tab = 'console'
  })

  // На маунті: відновлюємо збережену вкладку (якщо вона існує для цього
  // типу збірки) і скрол. Скрол відновлюється з повторними спробами, бо
  // контент вкладок вантажиться асинхронно.
  $effect(() => {
    const id = pack.id
    const saved = getPackUI(id)
    if (saved?.tab) {
      const known = ['servers', 'basic', 'perf', 'game'].includes(saved.tab)
        || ['mods', 'files', 'console'].includes(saved.tab)
      if (known) tab = saved.tab as typeof tab
    }
    restoreContentScroll(saved?.scroll ?? 0)

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
    try { worlds = await App.ListInstanceWorlds(pack.id) } catch (e) { worlds = [] }
    try { packServers = await App.ListPackServers(pack.id) } catch (e) { packServers = [] }
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
      autoJoinWorld: '', autoJoinServer: '', servers: []
    }
  }

  // Нормалізація конфіга, що прийшов з бекенду. ВАЖЛИВО: Go-структура
  // InstanceConfig має `servers,omitempty`, тому для збірки без збереженого
  // конфіга бекенд повертає об'єкт БЕЗ поля `servers` — і `cfg.servers.length`
  // у таббарі/секції кидало TypeError при КОЖНОМУ рендері, через що вкладки
  // не перемикались і контент лишався порожнім. Заповнюємо всі відсутні
  // поля дефолтами, щоб cfg завжди був повноцінним.
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
      fullscreenOverride: c.fullscreenOverride ?? null,
      servers: Array.isArray(c.servers) ? c.servers : []
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
  $effect(() => {
    if (pack.id !== lastPackId) {
      lastPackId = pack.id
      loadConfig()
    }
  })

  let saveTimer: ReturnType<typeof setTimeout> | undefined
  function saveConfig(showToast = false) {
    clearTimeout(saveTimer)
    saveTimer = setTimeout(async () => {
      try {
        await App.SaveInstanceConfig(cfg)
        if (showToast) toast(t('pack.saved'), 'success')
      } catch (e: any) {
        toast(t('error.withMessage', { message: e }), 'error')
      }
    }, 350)
  }

  function update<K extends keyof InstanceConfig>(key: K, value: InstanceConfig[K]) {
    cfg = { ...cfg, [key]: value }
    saveConfig()
  }

  // ── Сервери ──
  function addServer() {
    const server: InstanceServer = { id: crypto.randomUUID(), name: '', address: '', pinned: false, official: false }
    cfg = { ...cfg, servers: [...cfg.servers, server] }
    saveConfig()
  }

  function updateServer(id: string, patch: Partial<InstanceServer>) {
    cfg = { ...cfg, servers: cfg.servers.map(s => s.id === id ? { ...s, ...patch } : s) }
    saveConfig()
  }

  async function removeServer(id: string) {
    const server = cfg.servers.find(s => s.id === id)
    const ok = await confirmDialog(t('pack.servers.remove'), server?.name || server?.address || '', {
      confirmLabel: t('pack.servers.remove'), cancelLabel: t('cancel'), danger: true
    })
    if (!ok) return
    cfg = { ...cfg, servers: cfg.servers.filter(s => s.id !== id) }
    saveConfig(true)
  }

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
  // Кастомні збірки повністю керуються користувачем — вони завжди мають
  // розділи «Моди» і «Файли», незалежно від лоадера (навіть ваніль-збірка
  // може мати файли/конфіги/ресурс-паки для перегляду). Офіційні збірки
  // Шаурма керуються лаунчером (lock-бейдж), тож провідник їм не показуємо.
  const canManage = $derived(kind === 'custom')
  // Розділ «Моди» має сенс лише для збірок з мод-лоадерами: у ванільних
  // збірок (vanilla) вкладки «Моди» немає (ТЗ: «у луардерів ваніла є розділ
  // моди якого немає там бути»). «Файли» лишається — провідник файлів
  // корисний і для ванільних кастомних збірок (конфіги/ресурс-паки).
  const canMods = $derived(canManage && (pack.loader_type || '').toLowerCase() !== 'vanilla')
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
</script>

<!-- Єдиний рядок дій: «Назад» зліва, решта кнопок СПРАВА, і «⋯» з
     «Грати» в ОДНОМУ рядку поряд одне з одним. Раніше кнопки теки/консолі
     були зліва, а «Грати» — на окремому рядку від «⋯»; тепер усе разом. -->
<div class="pack-toolbar">
  <button class="btn btn-ghost btn-sm" onclick={onBack} title={t('pack.back')}><i class="ti ti-arrow-left"></i> {t('pack.back')}</button>
  <div class="pt-spacer"></div>
  <button class="btn btn-ghost btn-sm" onclick={openFolder} title={t('pack.openFolder')}><i class="ti ti-folder-open"></i> {t('pack.openFolder')}</button>
  <button class="btn btn-ghost btn-sm" onclick={() => App.OpenPackConsole(pack.id)} title={t('pack.openConsole')}><i class="ti ti-terminal-2"></i> {t('pack.openConsole')}</button>
  {#if installed}
    <div class="menu-wrap">
      <button class="btn btn-ghost btn-sm" onclick={(e: any) => { e.stopPropagation(); actionsOpen = !actionsOpen }} title={t('pack.update')}><i class="ti ti-dots-vertical"></i></button>
      {#if actionsOpen}
        <div class="dropdown-menu" onclick={(e: any) => e.stopPropagation()}>
          <div class="dd-item" onclick={() => { actionsOpen = false; onDownload(pack.id) }}>
            <i class="ti ti-refresh"></i> {t('pack.update')}
          </div>
          <div class="dd-sep"></div>
          <div class="dd-item danger" onclick={() => { actionsOpen = false; onDelete(pack.id) }}>
            <i class="ti ti-trash"></i> {t('pack.delete')}
          </div>
        </div>
      {/if}
    </div>
    {#if gameRunning}
      <button class="btn btn-danger" onclick={onStop}><i class="ti ti-player-stop"></i> {t('pack.stop')}</button>
    {:else if status === 'needs-update'}
      <button class="btn btn-primary" onclick={() => onDownload(pack.id)}><i class="ti ti-refresh"></i> {t('pack.update')}</button>
    {:else}
      <button class="btn btn-primary" disabled={isLaunching} onclick={() => onLaunch(pack.id)}>
        {#if isLaunching}<i class="ti ti-loader spin"></i> {launchStageLabel(launchProgress?.stage)}
        {:else}<i class="ti ti-player-play"></i> {t('pack.play')}{/if}
      </button>
    {/if}
  {/if}
</div>

{#if !installed}
  {@const bg = packAsset(pack.background_url)}
  {@const icon = packAsset(pack.icon_url)}
  <div class="pack-empty" style={bg ? `background-image:url('${bg}')` : 'background:linear-gradient(135deg,#3a2818,#1a0f08)'}>
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
        <button class="btn btn-primary btn-lg pe-btn" onclick={() => onDownload(pack.id)}>
          <i class="ti ti-download"></i> {t('pack.download')}
        </button>
      {/if}
    </div>
  </div>
{:else}
{@const bg = packAsset(pack.background_url)}
{@const icon = packAsset(pack.icon_url)}
<div class="inst-banner" style={bg ? `background-image:url('${bg}')` : 'background:linear-gradient(135deg,#3a2818,#1a0f08)'}>
  <!-- Назва збірки живе ТУТ, на фоні (банері), а не у topbar: іконка +
       назва + версія/лоадер + теги поверх фонового арту збірки. -->
  <div class="ib-title">
    {#if icon}<span class="ib-ic"><img src={icon} alt="" /></span>{:else}<i class="ti {pack.icon || 'ti-puzzle'}" style="color:var(--orange)"></i>{/if}
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
</div>

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
      <div class="lp-label lp-error"><i class="ti ti-alert-triangle"></i> {t('error.withMessage', { message: launchError })}</div>
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

  <div class="tabbar">
    <button class="tab-btn" class:active={tab === 'servers'} onclick={() => tab = 'servers'}>
      <i class="ti ti-server-2"></i> {t('pack.tab.servers')} <span class="num">{cfg.servers.length}</span>
    </button>
    {#if canManage}
      {#if canMods}
        <button class="tab-btn" class:active={tab === 'mods'} onclick={() => tab = 'mods'}>
          <i class="ti ti-puzzle"></i> {t('pack.tab.mods')}
        </button>
      {/if}
      <button class="tab-btn" class:active={tab === 'files'} onclick={() => tab = 'files'}>
        <i class="ti ti-files"></i> {t('pack.tab.files')}
      </button>
    {/if}
    <button class="tab-btn" class:active={tab === 'basic'} onclick={() => tab = 'basic'}>
      <i class="ti ti-info-circle"></i> {t('pack.tab.basic')}
    </button>
    <button class="tab-btn" class:active={tab === 'perf'} onclick={() => tab = 'perf'}>
      <i class="ti ti-cpu"></i> {t('pack.tab.perf')}
    </button>
    <button class="tab-btn" class:active={tab === 'game'} onclick={() => tab = 'game'}>
      <i class="ti ti-device-gamepad-2"></i> {t('pack.tab.game')}
    </button>
    <button class="tab-btn" class:active={tab === 'console'} onclick={() => tab = 'console'}>
      <i class="ti ti-terminal-2"></i> {t('pack.tab.console')}
    </button>
  </div>
{/if}

{#if installed}
{#if !cfgLoaded}
  <div class="card"><div style="padding:24px;text-align:center;color:var(--text-mute);font-size:12px">…</div></div>

{:else if tab === 'servers'}
  <PackServersSection
    servers={cfg.servers}
    onAdd={addServer}
    onUpdate={updateServer}
    onRemove={removeServer}
  />

{:else if tab === 'basic'}
  <PackBasicSection pack={pack} kind={kind} onUpdated={onPackUpdated} />

{:else if tab === 'perf'}
  <PackPerfSection
    {cfg}
    {settings}
    onUpdate={update}
    packName={pack.name}
    repairing={repairing}
    reinstalling={reinstalling}
    onRepair={repairPack}
    onReinstall={reinstallPack}
  />

{:else if tab === 'game'}
  <PackGameSection {cfg} {settings} accounts={accounts} worlds={worlds} packServers={packServers} onRefreshWorlds={refreshGameTab} onUpdate={update} />

{:else if tab === 'files'}
  {#if canManage}
    <PackFilesPanel packId={pack.id} packName={pack.name} />
  {/if}

{:else if tab === 'mods'}
  <PackModsSection packId={pack.id} packName={pack.name} mcVersion={pack.mc_version} loader={pack.loader_type} />

{:else if tab === 'console'}
  <div class="console-tab-wrap">
    <!-- packID: вкладка консолі ЖОРСТКО прив'язана до цієї збірки — показує
         її буфер/стан і не перемикається на чужі запуски (консоль вікна
         окремо слідує за збіркою, що запускається). -->
    <GameConsole packID={pack.id} />
  </div>
{/if}
{/if}
