<script lang="ts">
  // ── Розділ «Основне» на сторінці збірки ────────────────────────────────
  // Офіційні збірки Shaurma: фіксована інформація (назва, версія гри,
  // лоадер, опис, дата оновлення) — значення приходять з маніфесту і на
  // бекенді не змінюються (read-only).
  //
  // Кастомні збірки: той самий опис + кнопка «Редагувати». У режимі
  // редагування можна змінити назву, опис, версію Minecraft, лоадер та
  // його версію, колір, іконку (пресет або PNG) і фон. Зміна версії
  // гри/лоадера на бекенді очищає кеш старої версії. Після збереження
  // сторінка/сайдбар оновлюються одразу (onUpdated → refreshBuilds).
  import { t } from './i18n.svelte'
  import { App } from './wails'
  import { toast } from './toast.svelte'
  import { packAsset } from './packAssets.svelte'
  import type { BuildKind, CustomLoader, CustomPack, MCVersionEntry, LoaderVersionEntry, PackIndexEntry } from './types'
  import vanillaIcon from '../assets/images/vanilla_icon.png'
  import fabricIcon from '../assets/images/fabric_icon.png'
  import forgeIcon from '../assets/images/forge_icon.png'
  import neoforgeIcon from '../assets/images/neoforge_icon.png'
  import quiltIcon from '../assets/images/quilt_icon.png'

  let {
    pack,
    kind = null,
    onUpdated = () => {}
  } = $props<{
    pack: PackIndexEntry
    kind: BuildKind | null
    onUpdated?: () => void
  }>()

  // Іконки лоадерів (ті самі, що у формі створення збірки).
  const loaders: { id: CustomLoader; icon: string; color: string }[] = [
    { id: 'vanilla', icon: vanillaIcon, color: '#8b8b8b' },
    { id: 'fabric', icon: fabricIcon, color: '#ff8a00' },
    { id: 'forge', icon: forgeIcon, color: '#d08b5b' },
    { id: 'neoforge', icon: neoforgeIcon, color: '#ff6b35' },
    { id: 'quilt', icon: quiltIcon, color: '#9d7cff' },
  ]
  const presetIcons = [
    'ti-puzzle', 'ti-star', 'ti-heart', 'ti-diamond', 'ti-crown', 'ti-skull',
    'ti-flame', 'ti-bolt', 'ti-sword', 'ti-shield', 'ti-axe', 'ti-target',
    'ti-ghost', 'ti-alien', 'ti-robot', 'ti-brain', 'ti-cat', 'ti-dog',
    'ti-bug', 'ti-rocket', 'ti-planet', 'ti-moon', 'ti-sun', 'ti-snowflake',
  ]
  const colorPresets = ['#ff8a00', '#7b4dff', '#10b981', '#3b82f6', '#ef4444']

  // ── Форма редагування ──
  let saving = $state(false)
  let loadingCfg = $state(false)
  // Повний опис кастомної збірки (iconPath/backgroundPath/RAM/Java), які
  // не потрапляють у PackIndexEntry сторінки — підтягується при відкритті
  // редагування і зберігається без змін при збереженні форми.
  let cp = $state<CustomPack | null>(null)

  let name = $state('')
  let description = $state('')
  let mcVersion = $state('')
  let loader = $state<CustomLoader>('vanilla')
  let loaderVersion = $state('')
  let color = $state('#ff8a00')
  let customColor = $state('#b24592')
  let iconName = $state('')
  let iconSrcPath = $state('')
  let backgroundSrcPath = $state('')
  let showSnapshots = $state(false)
  let showOld = $state(false)

  let mcVersions = $state<MCVersionEntry[]>([])
  let loaderVersions = $state<LoaderVersionEntry[]>([])
  let mcVersionsLoading = $state(false)
  let loaderVersionsLoading = $state(false)

  const filteredMCVersions = $derived(
    mcVersions.filter(v => {
      if (v.type === 'release') return true
      if (v.type === 'snapshot') return showSnapshots
      return showOld
    })
  )
  const usingCustomColor = $derived(!colorPresets.includes(color))
  function applyCustomColor(c: string) { customColor = c; color = c }

  async function loadMCVersions() {
    if (mcVersions.length) return
    mcVersionsLoading = true
    try { mcVersions = await App.GetCustomPackMCVersions() }
    catch (e: any) { toast(t('error.withMessage', { message: e }), 'error') }
    finally { mcVersionsLoading = false }
  }

  async function loadLoaderVersions() {
    if (loader === 'vanilla' || !mcVersion) { loaderVersions = []; loaderVersion = ''; return }
    loaderVersionsLoading = true
    try {
      loaderVersions = await App.GetCustomPackLoaderVersions(loader, mcVersion)
      if (!loaderVersion || !loaderVersions.some(v => v.version === loaderVersion)) {
        const recommended = loaderVersions.find(v => v.recommended) ?? loaderVersions[0]
        loaderVersion = recommended?.version ?? ''
      }
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

  // Відкриття редагування: підтягуємо повний опис збірки і заповнюємо
  // форму поточними значеннями (список версій/лоадерів качається лінькво).
  async function openEdit() {
    loadingCfg = true
    try {
      const full = await App.GetCustomPack(pack.id)
      cp = full
      name = full.name
      description = full.description ?? ''
      mcVersion = full.mcVersion
      loader = (full.loader as CustomLoader) || 'vanilla'
      loaderVersion = full.loaderVersion ?? ''
      color = full.color || '#ff8a00'
      customColor = color
      iconName = full.icon ?? ''
      iconSrcPath = ''
      backgroundSrcPath = ''
      await loadMCVersions()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
      loadFailed = true
    } finally {
      loadingCfg = false
    }
  }

  // Кастомні збірки: «Основне» ОДРАЗУ в режимі редагування — візуал збірки
  // (назва, іконка, фон, колір) видно й можна міняти без кнопки
  // «Редагувати». Форма підтягує повний опис один раз (або після помилки).
  let loadFailed = $state(false)
  $effect(() => {
    if (kind === 'custom' && !cp && !loadingCfg && !loadFailed) openEdit()
  })

  function selectPresetIcon(n: string) { iconName = n; iconSrcPath = '' }
  async function browseIcon() {
    try { const p = await App.BrowseImageFile(); if (p) { iconSrcPath = p; iconName = '' } } catch { /* скасовано */ }
  }
  async function browseBackground() {
    try { const p = await App.BrowseImageFile(); if (p) backgroundSrcPath = p } catch { /* скасовано */ }
  }

  const iconPreviewUrl = $derived(
    iconSrcPath
      ? packAsset('local-file://' + iconSrcPath)
      : (!iconName && cp?.iconPath ? packAsset('local-file://' + cp.iconPath) : '')
  )
  const backgroundPreviewUrl = $derived(
    backgroundSrcPath
      ? packAsset('local-file://' + backgroundSrcPath)
      : (cp?.backgroundPath ? packAsset('local-file://' + cp.backgroundPath) : '')
  )

  async function save() {
    if (!name.trim()) { toast(t('createPack.error.nameRequired'), 'error'); return }
    if (!mcVersion) { toast(t('createPack.error.mcVersionRequired'), 'error'); return }
    saving = true
    let saved = false
    try {
      await App.UpdateCustomPack(pack.id, {
        name, description, iconSrcPath, backgroundSrcPath, icon: iconName, color,
        mcVersion, loader, loaderVersion,
        useCustomRam: cp?.useCustomRam ?? false,
        minRamMb: cp?.minRamMb ?? 0,
        maxRamMb: cp?.maxRamMb ?? 0,
        useSeparateJava: cp?.useSeparateJava ?? false,
        javaPath: cp?.javaPath ?? '',
      })
      toast(t('pack.basic.saved'), 'success')
      saved = true
      onUpdated()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
      saving = false
      return
    }
    // Інлайн-редагування: лишаємось у формі, перечитавши збережені значення
    // з бекенду (нова назва/іконка/фон одразу у полях). Помилка перечитування
    // НЕ має маскувати успішне збереження, тож try/catch тут окремий.
    try {
      if (saved) await openEdit()
    } catch { /* релоад уже показав свою помилку */ }
    saving = false
  }
</script>

{#if kind === 'custom'}
  <div class="card">
    <div class="card-head">
      <div class="ci"><i class="ti ti-pencil"></i></div>
      <div class="ctw"><div class="ct">{t('pack.basic.editTitle')}</div><div class="cd">{t('pack.basic.editDesc')}</div></div>
    </div>

    {#if loadingCfg}
      <div class="row"><div class="rl-sub"><i class="ti ti-loader spin"></i> {t('pack.basic.loading')}</div></div>
    {:else}
      <div class="row col">
        <div class="rl-name">{t('createPack.name')}</div>
        <input class="input" bind:value={name} />
      </div>
      <div class="row col">
        <div class="rl-name">{t('createPack.description')} <span class="muted" style="font-weight:500">{t('createPack.descriptionOptional')}</span></div>
        <input class="input" bind:value={description} />
      </div>

      <div class="row">
        <div class="row-label"><div class="rl-name">{t('createPack.mcVersion')}</div></div>
        <div class="flex gap8">
          <span class="chip" class:on={showSnapshots} onclick={() => showSnapshots = !showSnapshots} role="button" tabindex="0">{t('createPack.mcVersion.snapshot')}</span>
          <span class="chip" class:on={showOld} onclick={() => showOld = !showOld} role="button" tabindex="0">{t('createPack.mcVersion.old')}</span>
          <select class="select" bind:value={mcVersion} disabled={mcVersionsLoading}>
            {#if mcVersionsLoading}<option value="">…</option>{/if}
            {#each filteredMCVersions as v (v.id)}
              <option value={v.id}>{v.id}</option>
            {/each}
          </select>
        </div>
      </div>
      {#if mcVersion && mcVersion !== pack.mc_version}
        <div class="field-hint" style="padding:0 4px;color:var(--yellow)"><i class="ti ti-alert-triangle"></i> {t('pack.basic.versionChangeHint')}</div>
      {/if}

      <div class="row col">
        <div class="rl-name">{t('createPack.loader')}</div>
        <div class="loader-grid">
          {#each loaders as l (l.id)}
            <div class="loader-opt" class:sel={loader === l.id} onclick={() => loader = l.id} role="button" tabindex="0">
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

      <div class="row col">
        <div class="rl-name">{t('createPack.icon')}</div>
        <div class="flex gap8">
          <div class="icon-pick" class:has-icon={!!iconPreviewUrl || !!iconName} onclick={browseIcon} role="button" tabindex="0">
            {#if iconPreviewUrl}
              <img src={iconPreviewUrl} alt="" />
            {:else if iconName}
              <i class="ti {iconName}" style="color:{color}"></i>
            {:else}
              <i class="ti ti-photo-plus"></i><span>{t('createPack.icon').toUpperCase()}</span>
            {/if}
          </div>
          <div class="rl-sub" style="flex:1">{t('createPack.icon.hint')}</div>
        </div>
        <div class="rl-name" style="margin-top:10px">{t('createPack.icon.preset')}</div>
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
        <div class="flex gap8">
          <button class="btn btn-ghost" style="justify-content:center" onclick={browseBackground}>
            <i class="ti ti-photo"></i> {t('createPack.background.choose')}
          </button>
          {#if backgroundPreviewUrl}
            <div class="bg-preview" style="background-image:url('{backgroundPreviewUrl}')"></div>
          {/if}
        </div>
      </div>

      <div class="row" style="border-bottom:none;padding-top:6px">
        <div style="flex:1"></div>
        <button class="btn btn-ghost" onclick={openEdit} disabled={saving} title={t('pack.basic.reset')}><i class="ti ti-refresh"></i> {t('pack.basic.reset')}</button>
        <button class="btn btn-primary" onclick={save} disabled={saving}>
          <i class="ti {saving ? 'ti-loader spin' : 'ti-device-floppy'}"></i>
          {saving ? t('pack.basic.saving') : t('pack.basic.save')}
        </button>
      </div>
    {/if}
  </div>
{:else if kind === 'shaurma'}
  <div class="card">
    <div class="card-head">
      <div class="ci"><i class="ti ti-info-circle"></i></div>
      <div class="ctw"><div class="ct">{t('pack.tab.basic')}</div></div>
    </div>
    <div class="row">
      <div class="row-label"><div class="rl-name">{t('pack.basic.name')}</div></div>
      <div class="rl-name">{pack.name}</div>
    </div>
    <div class="row">
      <div class="row-label"><div class="rl-name">{t('pack.basic.version')}</div></div>
      <div class="select field-locked"><i class="ti ti-versions lf"></i>{pack.mc_version}</div>
    </div>
    <div class="row">
      <div class="row-label"><div class="rl-name">{t('pack.basic.loader')}</div></div>
      <div class="select field-locked"><i class="ti ti-package lf"></i>{pack.loader_type} {pack.loader_version}</div>
    </div>
    {#if pack.description}
      <div class="row col">
        <div class="rl-name">{t('pack.basic.description')}</div>
        <div style="font-size:12.5px;color:var(--text-mute);line-height:1.55">{pack.description}</div>
      </div>
    {/if}
    <div class="row">
      <div class="row-label"><div class="rl-name">{t('pack.basic.updated')}</div></div>
      <div class="mono" style="font-size:11.5px;color:var(--text-mute)">{pack.updated_at}</div>
    </div>
  </div>
  {#if kind === 'shaurma'}
    <div class="field-hint" style="padding:0 4px"><i class="ti ti-lock"></i> {t('pack.locked.hint')}</div>
  {/if}
{/if}

<style>
  .bg-preview {
    width:120px; height:52px; border-radius:var(--r-md); background-size:cover; background-position:center;
    border:1px solid var(--border); flex-shrink:0;
  }
</style>
