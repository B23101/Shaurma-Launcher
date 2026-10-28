<script lang="ts">
  // ── Вкладка «Вигляд» на сторінці сервера ────────────────────────────
  //
  // Сюди переїхало все, що описує сервер ЯК ОБʼЄКТ У ЛАУНЧЕРІ, а не як
  // процес: назва, опис (нотатки), ID (тека) і, власне, вигляд — іконка
  // лаунчера + колір + протокольна іконка гри (server-icon.png).
  //
  // Раніше це лежало в «Налаштуваннях» поряд із портом/JVM-аргументами —
  // різні за змістом речі в одному екрані. Тепер «Налаштування» = запуск,
  // «Вигляд» = як сервер виглядає й називається.
  import { App } from '../../wails'
  import { toast } from '../../toast.svelte'
  import { t } from '../../i18n.svelte'
  import type { ServerView, ServerConfigPatch } from '../../types'
  import AppearanceEditor from '../../AppearanceEditor.svelte'
  import { packAsset } from '../../packAssets.svelte'

  let {
    srv,
    isRunning = false,
    onUpdated = (_s: ServerView) => {},
  }: {
    srv: ServerView
    isRunning?: boolean
    onUpdated?: (s: ServerView) => void
  } = $props()

  let editName = $state(srv.name)
  let editNotes = $state(srv.notes || '')
  let editColor = $state(srv.color || '#ff8a00')
  let savingKey = $state('')

  // ── ID (тека) сервера ──
  const currentFolder = $derived(srv.dir.split(/[\\/]/).filter(Boolean).pop() ?? '')
  let editFolder = $state(currentFolder)
  let folderCheck = $state<{ free: boolean; folder: string } | null>(null)
  let folderBusy = $state(false)
  const folderChanged = $derived(editFolder.trim() !== '' && editFolder.trim() !== currentFolder)

  // ── Вигляд ──
  let iconName = $state(srv.icon ?? '')
  let launcherSrcPath = $state('')
  let savingIcon = $state(false)
  let appearanceSaving = $state(false)
  let appearanceSavedTick = $state(0)

  let loadedId = ''
  $effect(() => {
    if (srv && loadedId !== srv.id) {
      loadedId = srv.id
      editName = srv.name; editNotes = srv.notes || ''
      editColor = srv.color || '#ff8a00'
      iconName = srv.icon ?? ''
      editFolder = srv.dir.split(/[\\/]/).filter(Boolean).pop() ?? ''
    }
  })

  const dirtyName = $derived(editName !== srv.name)
  const dirtyNotes = $derived(editNotes !== (srv.notes || ''))

  async function saveOne(field: 'name' | 'notes') {
    const patch: Partial<ServerConfigPatch> = {}
    if (field === 'name') {
      if (!dirtyName) return
      if (!editName.trim()) { toast(t('servers.appearance.nameRequired'), 'error'); return }
      patch.name = editName.trim()
    } else {
      if (!dirtyNotes) return
      patch.notes = editNotes
    }
    savingKey = field
    try {
      const updated = await App.SaveServerConfig(srv.id, patch)
      onUpdated(updated)
      toast(t('servers.settings.savedOne'), 'success')
    } catch (e: any) { toast(String(e), 'error') }
    finally { savingKey = '' }
  }

  // Жива перевірка зайнятості нового ID.
  let folderSeq = 0
  $effect(() => {
    const val = editFolder.trim()
    if (!folderChanged) { folderCheck = null; return }
    const seq = ++folderSeq
    const timer = setTimeout(() => {
      App.CheckServerFolder(val)
        .then(r => { if (seq === folderSeq) folderCheck = r })
        .catch(() => { if (seq === folderSeq) folderCheck = null })
    }, 220)
    return () => clearTimeout(timer)
  })

  async function renameFolder() {
    if (!folderChanged || folderBusy) return
    folderBusy = true
    try {
      const updated = await App.RenameServerFolder(srv.id, editFolder.trim())
      onUpdated(updated)
      editFolder = updated.dir.split(/[\\/]/).filter(Boolean).pop() ?? ''
      folderCheck = null
      toast(t('servers.appearance.idChanged'), 'success')
    } catch (e: any) { toast(String(e), 'error') }
    finally { folderBusy = false }
  }

  // ── Іконка лаунчера + колір — АВТО-збереження (як у збірках) ──
  const launcherIconPreview = $derived(
    launcherSrcPath ? (packAsset('local-file://' + launcherSrcPath) ?? '') : (srv.launcherIconData || '')
  )

  function selectPresetIcon(n: string) {
    iconName = n
    launcherSrcPath = ''
  }

  async function browseLauncherIcon() {
    try {
      const p = await App.BrowseImageFile()
      if (p) { launcherSrcPath = p; iconName = '' }
    } catch { /* скасовано */ }
  }

  async function removeLauncherIcon() {
    if (launcherSrcPath) { launcherSrcPath = ''; return }
    try {
      const updated = await App.ResetLauncherIcon(srv.id)
      onUpdated(updated)
      toast(t('servers.appearance.iconRemoved'), 'success')
    } catch (e: any) { toast(String(e), 'error') }
  }

  let appearanceTimer: ReturnType<typeof setTimeout> | undefined
  let lastSavedAppearance = { color: srv.color || '#ff8a00', icon: srv.icon ?? '' }

  async function saveAppearance() {
    if (appearanceSaving) return
    appearanceSaving = true
    try {
      let updated: ServerView | null = null
      if (launcherSrcPath) {
        updated = await App.SetLauncherIcon(srv.id, launcherSrcPath)
        launcherSrcPath = ''
        iconName = ''
      } else if (iconName !== lastSavedAppearance.icon) {
        updated = await App.SaveServerConfig(srv.id, { icon: iconName })
      }
      if (editColor !== lastSavedAppearance.color) {
        updated = await App.SaveServerConfig(srv.id, { color: editColor })
      }
      lastSavedAppearance = { color: editColor, icon: updated?.icon ?? iconName }
      if (updated) onUpdated(updated)
      appearanceSavedTick++
    } catch (e: any) {
      toast(String(e), 'error')
    } finally {
      appearanceSaving = false
    }
  }

  $effect(() => {
    void editColor; void iconName; void launcherSrcPath
    if (savingIcon) return
    const changed = editColor !== lastSavedAppearance.color ||
      iconName !== lastSavedAppearance.icon || !!launcherSrcPath
    if (!changed) { clearTimeout(appearanceTimer); return }
    clearTimeout(appearanceTimer)
    appearanceTimer = setTimeout(() => { void saveAppearance() }, 700)
    return () => clearTimeout(appearanceTimer)
  })

  // ── Протокольна іконка гри (server-icon.png 64×64) ──
  async function pickIcon(e: Event) {
    const input = e.target as HTMLInputElement
    const file = input.files?.[0]
    if (!file) return
    savingIcon = true
    try {
      const dataUrl = await new Promise<string>((resolve, reject) => {
        const fr = new FileReader()
        fr.onload = () => resolve(String(fr.result))
        fr.onerror = () => reject(new Error(t('servers.appearance.iconReadError')))
        fr.readAsDataURL(file)
      })
      await App.SetServerIcon(srv.id, dataUrl)
      const iconData = await App.GetServerIconData(srv.id)
      onUpdated({ ...srv, iconData })
      toast(t('servers.appearance.iconUpdated'), 'success')
    } catch (e: any) { toast(String(e), 'error') }
    finally { savingIcon = false; input.value = '' }
  }

  async function resetIcon() {
    try {
      await App.ResetServerIcon(srv.id)
      onUpdated({ ...srv, iconData: '' })
      toast(t('servers.appearance.iconReset'), 'success')
    } catch (e: any) { toast(String(e), 'error') }
  }
</script>

<div class="sat" style="--pcol:{editColor}">
  <!-- ═══ ІДЕНТИЧНІСТЬ: назва · опис · ID ═══ -->
  <div class="card" class:qdirty={dirtyName || dirtyNotes} data-skey="name">
    <div class="card-head">
      <div class="ci"><i class="ti ti-id"></i></div>
      <div class="ctw"><div class="ct">{t('servers.appearance.identity')}</div><div class="cd">{t('servers.appearance.identityDesc')}</div></div>
    </div>

    <div class="row col">
      <div class="rl-name flex" style="justify-content:space-between">
        <span>{t('servers.settings.name')}</span>
        <button class="sat-save sm" class:busy={savingKey === 'name'} disabled={savingKey === 'name' || !dirtyName}
                onclick={() => saveOne('name')} title={t('servers.settings.save')} aria-label={t('servers.settings.save')}>
          {#if savingKey === 'name'}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-check"></i>{/if}
        </button>
      </div>
      <input class="input" bind:value={editName} placeholder={t('servers.settings.name')} data-testid="server-name-input" />
    </div>

    <div class="row col" data-skey="id">
      <div class="rl-name">{t('servers.settings.folderId')} <span class="muted">{isRunning ? t('servers.appearance.stopFirst') : ''}</span></div>
      <div class="sat-inline">
        <input class="input mono" style="flex:1" bind:value={editFolder} disabled={isRunning} placeholder="id-сервера" />
        <button class="btn btn-ghost btn-sm" disabled={isRunning || !folderChanged || folderBusy || (!!folderCheck && !folderCheck.free)} onclick={renameFolder}>
          {folderBusy ? '…' : t('servers.appearance.idChange')}
        </button>
      </div>
      {#if folderChanged && folderCheck}
        <div class="field-hint" style="color:{folderCheck.free ? 'var(--green)' : 'var(--red)'}">
          <i class="ti {folderCheck.free ? 'ti-check' : 'ti-x'}"></i> {folderCheck.free ? t('servers.appearance.idFree') : t('servers.appearance.idBusy')} · {folderCheck.folder}
        </div>
      {/if}
    </div>

    <div class="row col last" data-skey="notes">
      <div class="rl-name flex" style="justify-content:space-between">
        <span>{t('servers.settings.notes')}</span>
        <button class="sat-save sm" class:busy={savingKey === 'notes'} disabled={savingKey === 'notes' || !dirtyNotes}
                onclick={() => saveOne('notes')} title={t('servers.settings.save')} aria-label={t('servers.settings.save')}>
          {#if savingKey === 'notes'}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-check"></i>{/if}
        </button>
      </div>
      <textarea class="input" bind:value={editNotes} rows="3" placeholder={t('servers.settings.notes')}></textarea>
    </div>
  </div>

  <!-- ═══ ВИГЛЯД: іконка лаунчера + колір ═══ -->
  <AppearanceEditor
    title={t('servers.settings.appearanceTitle')}
    desc={t('servers.settings.appearanceDesc')}
    ciClass="violet"
    iconPreviewUrl={launcherIconPreview}
    {iconName}
    color={editColor}
    onBrowseIcon={browseLauncherIcon}
    onSelectPreset={selectPresetIcon}
    onColorChange={(c: string) => editColor = c}
    onRemoveBackground={null}
    autoSaveState={appearanceSaving ? 'saving' : (appearanceSavedTick > 0 ? 'saved' : 'idle')}
  />
  {#if srv.launcherIconData || launcherSrcPath}
    <div class="sat-reset">
      <button class="btn btn-ghost btn-sm" onclick={removeLauncherIcon}>
        <i class="ti ti-trash"></i> {t('servers.settings.appearanceReset')}
      </button>
    </div>
  {/if}

  <!-- ═══ ІКОНКА В ГРІ (server-icon.png) ═══ -->
  <div class="card" data-skey="gameicon">
    <div class="card-head">
      <div class="ci"><i class="ti ti-photo"></i></div>
      <div class="ctw">
        <div class="ct">{t('servers.settings.gameIcon')}</div>
        <div class="cd">{t('servers.settings.gameIconDesc')}</div>
      </div>
    </div>
    <div class="row last">
      <span class="sat-icon-preview">
        {#if srv.iconData}<img src={srv.iconData} alt="" />{:else}<i class="ti ti-server"></i>{/if}
      </span>
      <div class="sat-icon-actions">
        <label class="btn btn-ghost btn-sm" for="srv-icon-file" style="cursor:pointer">
          {#if savingIcon}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-upload"></i>{/if} {t('servers.appearance.choose')}
        </label>
        <input id="srv-icon-file" type="file" accept="image/*" hidden onchange={pickIcon} />
        {#if srv.iconData}
          <button class="btn btn-ghost btn-sm" onclick={resetIcon} title={t('servers.settings.appearanceReset')}><i class="ti ti-trash"></i></button>
        {/if}
      </div>
    </div>
  </div>
</div>

<style>
  .sat { display: flex; flex-direction: column; gap: 14px; padding-bottom: 24px; }
  .sat .card { margin-bottom: 0; }
  .row.last { border-bottom: none; }
  .sat .card.qdirty { border-color: color-mix(in srgb, var(--pcol, var(--orange)) 45%, var(--border)); }

  .sat-inline { display: flex; gap: 8px; align-items: center; }
  .field-hint { display: inline-flex; align-items: center; gap: 5px; font: 600 11px/1 var(--font-ui); margin-top: 4px; }
  .muted { font-weight: 500; color: var(--text-dim); font-size: 11px; }

  .sat-save {
    width: 24px; height: 24px; flex-shrink: 0;
    display: inline-flex; align-items: center; justify-content: center;
    border-radius: 6px; cursor: pointer; font-size: 12px;
    background: color-mix(in srgb, #22c55e 14%, transparent);
    color: #22c55e; border: 1px solid color-mix(in srgb, #22c55e 34%, transparent);
    transition: background .14s, color .14s, opacity .14s, transform .12s;
  }
  .sat-save:hover:not(:disabled) { background: #22c55e; color: #06230f; transform: scale(1.05); }
  .sat-save:disabled { opacity: .3; cursor: default; }

  .sat-reset { display: flex; justify-content: flex-end; margin-top: -6px; }
  .sat-icon-preview {
    width: 48px; height: 48px; min-width: 48px; border-radius: 8px;
    display: flex; align-items: center; justify-content: center;
    background: var(--bg-input); border: 1px solid var(--border);
    color: var(--text-dim); font-size: 18px; overflow: hidden;
  }
  .sat-icon-preview img { width: 100%; height: 100%; object-fit: cover; border-radius: 7px; }
  .sat-icon-actions { display: flex; align-items: center; gap: 6px; }
</style>
