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
  import { t } from './i18n.svelte'
  import { App } from './wails'
  import { toast } from './toast.svelte'
  import { packAsset } from './packAssets.svelte'
  import type { CustomLoader, MCVersionEntry, LoaderVersionEntry, DetectedPack, Settings } from './types'
  import vanillaIcon from '../assets/images/vanilla_icon.png'
  import fabricIcon from '../assets/images/fabric_icon.png'
  import forgeIcon from '../assets/images/forge_icon.png'
  import neoforgeIcon from '../assets/images/neoforge_icon.png'
  import quiltIcon from '../assets/images/quilt_icon.png'

  // initialTab — яку вкладку відкрити одразу ("Імпортувати" з кнопки
  // «Імпортувати» у розділі збірок, "Вручну" з кнопки «Створити збірку»).
  let { settings, initialTab = 'manual', onCancel, onCreated } = $props<{
    settings: Settings
    initialTab?: 'manual' | 'import'
    onCancel: () => void
    onCreated: (packId: string) => void
  }>()

  let tab = $state<'manual' | 'import'>(initialTab)
  let submitting = $state(false)

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

  const filteredMCVersions = $derived(
    mcVersions.filter(v => {
      if (v.type === 'release') return true
      if (v.type === 'snapshot') return showSnapshots
      return showOld // old_beta / old_alpha
    })
  )

  // ID збірки береться з назви і підтримує пробіли та кирилицю — прибираємо
  // ЛИШЕ символи, які не приймаються в іменах папок (як Prism Launcher):
  // "Моя збірка!" → "Моя збірка", а не транслітерація в pack-1/pack-2.
  const slugId = $derived(slugify(name))
  function slugify(s: string): string {
    const cleaned = s.trim()
      .replace(/[<>:"/\\|?*\u0000-\u001f]+/g, '')
      .replace(/\.+$/g, '')
      .trim()
    return cleaned || 'pack'
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
      mcVersions = await App.GetCustomPackMCVersions()
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
      loaderVersions = await App.GetCustomPackLoaderVersions(loader, mcVersion)
      const recommended = loaderVersions.find(v => v.recommended) ?? loaderVersions[0]
      loaderVersion = recommended?.version ?? ''
    } catch (e: any) {
      loaderVersions = []
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      loaderVersionsLoading = false
    }
  }

  let lastLoaderKey = ''
  $effect(() => {
    const key = `${loader}:${mcVersion}`
    if (key !== lastLoaderKey) {
      lastLoaderKey = key
      loadLoaderVersions()
    }
  })

  loadMCVersions()

  function selectLoader(id: CustomLoader) { loader = id }

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
  let color = $state('#ff8a00')
  const colorPresets = ['#ff8a00', '#7b4dff', '#10b981', '#3b82f6', '#ef4444']
  let customColor = $state('#b24592')
  const usingCustomColor = $derived(!colorPresets.includes(color))
  function applyCustomColor(c: string) { customColor = c; color = c }
  let iconSrcPath = $state('')
  let backgroundSrcPath = $state('')
  // Пресет-іконка (Tabler): за замовчуванням пазл. Якщо користувач обрав
  // PNG (iconSrcPath) — пресет ігнорується (PNG має пріоритет).
  let iconName = $state('ti-puzzle')

  // Доступні пресет-іконки (Tabler Icons). PNG з iconSrcPath — пріоритет.
  const presetIcons = [
    'ti-puzzle', 'ti-star', 'ti-heart', 'ti-diamond', 'ti-crown', 'ti-skull',
    'ti-flame', 'ti-bolt', 'ti-sword', 'ti-shield', 'ti-axe', 'ti-target',
    'ti-ghost', 'ti-alien', 'ti-robot', 'ti-brain', 'ti-cat', 'ti-dog',
    'ti-bug', 'ti-rocket', 'ti-planet', 'ti-moon', 'ti-sun', 'ti-snowflake',
  ]

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

  // ── Створення (вкладка "Вручну") ──
  async function submitManual() {
    if (!name.trim()) { toast(t('createPack.error.nameRequired'), 'error'); return }
    if (!mcVersion) { toast(t('createPack.error.mcVersionRequired'), 'error'); return }
    submitting = true
    try {
      const pack = await App.CreateCustomPack({
        name, description, iconSrcPath, backgroundSrcPath, icon: iconName, color,
        mcVersion, loader, loaderVersion,
        useCustomRam, minRamMb: useCustomRam ? ramMinValue : 0, maxRamMb: useCustomRam ? ramMaxValue : 0,
        useSeparateJava, javaPath,
      })
      toast(t('createPack.created'), 'success')
      onCreated(pack.id)
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
  let detectError = $state('')
  let importName = $state('')
  let dragOver = $state(false)
  let hasCFKey = $state(true)

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

  async function detectArchive(path: string) {
    importArchivePath = path
    detecting = true
    detectError = ''
    detected = null
    try {
      detected = await App.DetectModpackArchive(path)
      importName = detected.suggestedName
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
      default: return t('createPack.import.formatGeneric')
    }
  }

  async function submitImport() {
    if (!detected) return
    const finalName = importName.trim() || detected.suggestedName
    if (!finalName) { toast(t('createPack.error.nameRequired'), 'error'); return }
    submitting = true
    try {
      const pack = await App.ImportModpackArchive(importArchivePath, detected, finalName, color, iconSrcPath, iconName)
      toast(t('createPack.imported'), 'success')
      onCreated(pack.id)
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

  // ── Візуальний прев'ю: картка у стилі сайдбара + перемикачі станів ──
  type PreviewState = 'ready' | 'running' | 'not-installed' | 'needs-update' | 'downloading' | 'updating'
  let previewState = $state<PreviewState>('ready')
  const previewStates: { id: PreviewState; icon: string; labelKey: string }[] = [
    { id: 'ready', icon: 'ti-player-play', labelKey: 'createPack.preview.state.ready' },
    { id: 'running', icon: 'ti-player-stop', labelKey: 'createPack.preview.state.running' },
    { id: 'not-installed', icon: 'ti-download', labelKey: 'createPack.preview.state.notInstalled' },
    { id: 'needs-update', icon: 'ti-refresh', labelKey: 'createPack.preview.state.needsUpdate' },
    { id: 'downloading', icon: 'ti-download', labelKey: 'createPack.preview.state.downloading' },
    { id: 'updating', icon: 'ti-refresh', labelKey: 'createPack.preview.state.updating' },
  ]

  const previewName = $derived(tab === 'manual' ? (name || t('createPack.namePlaceholder')) : (importName || t('createPack.namePlaceholder')))
  const previewLoaderLabel = $derived(tab === 'manual' ? formatLoaderLabel(loader) : detected ? formatLoaderLabel(detected.loader) : 'Vanilla')
  const previewMc = $derived(tab === 'manual' ? mcVersion : detected?.mcVersion ?? '')
  const previewBtnClass = $derived(
    previewState === 'running' ? 'stop'
      : previewState === 'ready' ? 'play'
      : previewState === 'not-installed' || previewState === 'needs-update' ? 'download'
      : 'disabled'
  )
  const previewBtnIcon = $derived(
    previewState === 'running' ? 'ti-player-stop'
      : previewState === 'ready' ? 'ti-player-play'
      : previewState === 'needs-update' || previewState === 'updating' ? 'ti-refresh'
      : 'ti-download'
  )
  const previewSpin = $derived(previewState === 'downloading' || previewState === 'updating')
  const previewStateLabel = $derived(t(previewStates.find(s => s.id === previewState)?.labelKey ?? 'createPack.preview.state.ready'))
</script>

{#snippet previewBlock()}
  <div>
    <div class="section-label" style="margin:2px 2px 8px;display:block">{t('createPack.preview')}</div>
    <div class="pv-sb" style="--pcol:{color}">
      <div class="pv-sb-ic" class:has-icon={!!iconPreviewUrl} class:blink={previewSpin}>
        {#if iconPreviewUrl}
          <img src={iconPreviewUrl} alt="" />
        {:else}
          <i class="ti {iconName || 'ti-puzzle'}"></i>
        {/if}
      </div>
      <div class="pv-sb-info">
        <div class="pv-sb-name">{previewName}</div>
        <div class="pv-sb-sub">{previewLoaderLabel} · {previewMc || '—'}</div>
        {#if previewSpin}
          <div class="pv-sb-prog"><div class="pv-sb-prog-fill" style="width:47%"></div></div>
        {/if}
      </div>
      <button class="sp-btn sp-{previewBtnClass}" title={previewStateLabel}>
        <i class="ti {previewBtnIcon} {previewSpin ? 'spin' : ''}"></i>
      </button>
    </div>
    <div class="status-pills">
      {#each previewStates as ps (ps.id)}
        <button class="pill" class:sel={previewState === ps.id} onclick={() => previewState = ps.id} title={t(ps.labelKey)}>
          <i class="ti {ps.icon}"></i> {t(ps.labelKey)}
        </button>
      {/each}
    </div>
    <div class="hero-mini" style="margin-top:14px">
      <div class="hm-bg" style={backgroundPreviewUrl ? `background-image:url('${backgroundPreviewUrl}')` : ''}>
        <div class="hm-title">{previewName}</div>
      </div>
      <div class="hm-cap">{t('createPack.preview.heroHint')}</div>
    </div>
  </div>
{/snippet}

<div class="page-hero">
  <div>
    <h1>{t('createPack.title')}</h1>
    <p class="h-sub">{t('createPack.subtitle')}</p>
  </div>
  <div class="h-acts">
    <button class="btn btn-ghost" onclick={onCancel}><i class="ti ti-x"></i> {t('createPack.cancel')}</button>
    <button class="btn btn-primary" disabled={submitting || (tab === 'import' && !detected)} onclick={submit}>
      <i class="ti {submitting ? 'ti-loader spin' : tab === 'import' ? 'ti-download' : 'ti-check'}"></i>
      {submitting ? t('createPack.submitting') : tab === 'import' ? t('createPack.import.submit') : t('createPack.submit')}
    </button>
  </div>
</div>

<div class="tabbar">
  <button class="tab-btn" class:active={tab === 'manual'} onclick={() => tab = 'manual'}>
    <i class="ti ti-adjustments"></i> {t('createPack.tab.manual')}
  </button>
  <button class="tab-btn" class:active={tab === 'import'} onclick={() => tab = 'import'}>
    <i class="ti ti-download"></i> {t('createPack.tab.import')}
  </button>
</div>

{#if tab === 'manual'}
  <div class="create-grid">
    <div>
      <!-- ОСНОВНЕ -->
      <div class="card">
        <div class="card-head">
          <div class="ci"><i class="ti ti-id"></i></div>
          <div class="ctw"><div class="ct">{t('createPack.section.basic')}</div><div class="cd">{t('createPack.section.basic.desc')}</div></div>
        </div>
        <div class="row col">
          <div class="rl-name">{t('createPack.name')}</div>
          <input class="input" placeholder={t('createPack.namePlaceholder')} bind:value={name} />
        </div>
        <div class="row">
          <div class="row-label"><div class="rl-name">{t('createPack.id')}</div><div class="rl-sub">{t('createPack.idHint')}</div></div>
          <span class="input mono" style="width:auto;color:var(--text-mute)">{slugId}</span>
        </div>
        <div class="row col">
          <div class="rl-name">{t('createPack.description')} <span class="muted" style="font-weight:500">{t('createPack.descriptionOptional')}</span></div>
          <input class="input" placeholder={t('createPack.descriptionPlaceholder')} bind:value={description} />
        </div>
      </div>

      <!-- ВЕРСІЯ ГРИ -->
      <div class="card">
        <div class="card-head">
          <div class="ci violet"><i class="ti ti-versions"></i></div>
          <div class="ctw"><div class="ct">{t('createPack.section.version')}</div><div class="cd">{t('createPack.section.version.desc')}</div></div>
        </div>
        <div class="row">
          <div class="row-label"><div class="rl-name">{t('createPack.mcVersion')}</div></div>
          <div class="flex gap8">
            <span class="chip" class:on={showSnapshots} onclick={() => showSnapshots = !showSnapshots} role="button" tabindex="0">{t('createPack.mcVersion.snapshot')}</span>
            <span class="chip" class:on={showOld} onclick={() => showOld = !showOld} role="button" tabindex="0">{t('createPack.mcVersion.old')}</span>
            <select class="select" bind:value={mcVersion} disabled={mcVersionsLoading}>
              {#if mcVersionsLoading}
                <option value="">…</option>
              {/if}
              {#each filteredMCVersions as v (v.id)}
                <option value={v.id}>{v.id}</option>
              {/each}
            </select>
          </div>
        </div>
        <div class="row col">
          <div class="rl-name">{t('createPack.loader')}</div>
          <div class="loader-grid">
            {#each loaders as l (l.id)}
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
              <select class="select" bind:value={loaderVersion} disabled={loaderVersionsLoading || loaderVersions.length === 0}>
                {#if loaderVersionsLoading}
                  <option value="">…</option>
                {:else if loaderVersions.length === 0}
                  <option value="">{t('createPack.loaderVersion.none')}</option>
                {:else}
                  {#each loaderVersions as v (v.version)}
                    <option value={v.version}>{v.version}{v.recommended ? ` (${t('createPack.loaderVersion.recommended')})` : ''}</option>
                  {/each}
                {/if}
              </select>
            </div>
          {/if}
        </div>
      </div>

      <!-- ПРОДУКТИВНІСТЬ -->
      <div class="card">
        <div class="card-head">
          <div class="ci green"><i class="ti ti-cpu"></i></div>
          <div class="ctw"><div class="ct">{t('createPack.section.perf')}</div><div class="cd">{t('createPack.section.perf.desc')}</div></div>
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
      <div class="card" style="margin:0">
        <div class="card-head"><div class="ci"><i class="ti ti-palette"></i></div><div class="ctw"><div class="ct">{t('createPack.section.visual')}</div></div></div>
        <div class="row">
          <div class="icon-pick" class:has-icon={!!iconPreviewUrl} onclick={browseIcon} role="button" tabindex="0">
            {#if iconPreviewUrl}
              <img src={iconPreviewUrl} alt="" />
            {:else if iconName}
              <i class="ti {iconName}" style="color:{color}"></i>
            {:else}
              <i class="ti ti-photo-plus"></i><span>{t('createPack.icon').toUpperCase()}</span>
            {/if}
          </div>
          <div class="row-label"><div class="rl-name">{t('createPack.icon')}</div><div class="rl-sub">{t('createPack.icon.hint')}</div></div>
        </div>
        <div class="row col">
          <div class="rl-name">{t('createPack.icon.preset')}</div>
          <div class="icon-preset-grid">
            {#each presetIcons as ic (ic)}
              <button class="icon-preset" class:sel={iconName === ic} onclick={() => selectPresetIcon(ic)} title={ic} style="--pcol:{color}">
                <i class="ti {ic}"></i>
              </button>
            {/each}
          </div>
        </div>
        <div class="row col">
          <div class="rl-name">{t('createPack.color')}</div>
          <div class="color-swatches">
            {#each colorPresets as c (c)}
              <button class="sw" class:sel={color === c} style="background:{c}" onclick={() => color = c} aria-label={c}></button>
            {/each}
            <label class="sw sw-custom" class:sel={usingCustomColor} style="--cc:{usingCustomColor ? color : customColor}" title={t('createPack.color.custom')}>
              <input type="color" value={usingCustomColor ? color : customColor} oninput={(e) => applyCustomColor(e.currentTarget.value)} />
            </label>
          </div>
          <div class="color-hex">
            <span class="rl-sub mono">#</span>
            <input class="input mono color-hex-input" value={color.replace('#', '')} placeholder="ff8a00" spellcheck="false"
              oninput={(e) => {
                const v = e.currentTarget.value.trim()
                if (/^[0-9a-fA-F]{6}$/.test(v)) { const c = '#' + v.toLowerCase(); color = c; customColor = c }
              }} />
          </div>
        </div>
        <div class="row col">
          <div class="rl-name">{t('createPack.background')}</div>
          <button class="btn btn-ghost" style="justify-content:center" onclick={browseBackground}>
            <i class="ti ti-photo"></i> {t('createPack.background.choose')}
          </button>
        </div>
      </div>

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
              </div>
            </div>
          </div>
          <div class="row col">
            <div class="rl-name">{t('createPack.import.nameLabel')}</div>
            <input class="input" placeholder={t('createPack.import.namePlaceholder')} bind:value={importName} />
          </div>
        {/if}
      </div>

      {#if detected}
        <div class="card">
          <div class="card-head">
            <div class="ci green"><i class="ti ti-cpu"></i></div>
            <div class="ctw"><div class="ct">{t('createPack.section.perf')}</div><div class="cd">{t('createPack.section.perf.desc')}</div></div>
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

        <div class="card">
          <div class="card-head"><div class="ci"><i class="ti ti-palette"></i></div><div class="ctw"><div class="ct">{t('createPack.section.visual')}</div></div></div>
          <div class="row">
            <div class="icon-pick" class:has-icon={!!iconPreviewUrl} onclick={browseIcon} role="button" tabindex="0">
              {#if iconPreviewUrl}
                <img src={iconPreviewUrl} alt="" />
              {:else if iconName}
                <i class="ti {iconName}" style="color:{color}"></i>
              {:else}
                <i class="ti ti-photo-plus"></i><span>{t('createPack.icon').toUpperCase()}</span>
              {/if}
            </div>
            <div class="row-label"><div class="rl-name">{t('createPack.icon')}</div><div class="rl-sub">{t('createPack.icon.hint')}</div></div>
          </div>
          <div class="row col">
            <div class="rl-name">{t('createPack.icon.preset')}</div>
            <div class="icon-preset-grid">
              {#each presetIcons as ic (ic)}
                <button class="icon-preset" class:sel={iconName === ic} onclick={() => selectPresetIcon(ic)} title={ic} style="--pcol:{color}">
                  <i class="ti {ic}"></i>
                </button>
              {/each}
            </div>
          </div>
          <div class="row col">
            <div class="rl-name">{t('createPack.color')}</div>
            <div class="color-swatches">
              {#each colorPresets as c (c)}
                <button class="sw" class:sel={color === c} style="background:{c}" onclick={() => color = c} aria-label={c}></button>
              {/each}
              <label class="sw sw-custom" class:sel={usingCustomColor} style="--cc:{usingCustomColor ? color : customColor}" title={t('createPack.color.custom')}>
                <input type="color" value={usingCustomColor ? color : customColor} oninput={(e) => applyCustomColor(e.currentTarget.value)} />
              </label>
            </div>
            <div class="color-hex">
              <span class="rl-sub mono">#</span>
              <input class="input mono color-hex-input" value={color.replace('#', '')} placeholder="ff8a00" spellcheck="false"
                oninput={(e) => {
                  const v = e.currentTarget.value.trim()
                  if (/^[0-9a-fA-F]{6}$/.test(v)) { const c = '#' + v.toLowerCase(); color = c; customColor = c }
                }} />
            </div>
          </div>
        </div>
      {/if}
    </div>

    {#if detected}
      <div class="preview-col">
        {@render previewBlock()}
      </div>
    {/if}
  </div>
{/if}
