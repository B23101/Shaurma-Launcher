<script lang="ts">
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
  import { onMount } from 'svelte'
  import { t } from './i18n.svelte'
  import { App, on } from './wails'
  import { toast, confirmDialog } from './toast.svelte'
  import type { ModEntry, ModVersionOption, ModDependency, ModBackupEntry } from './types'

  let {
    packId,
    packName = '',
    mcVersion = '',
    loader = ''
  }: {
    packId: string
    packName?: string
    mcVersion?: string
    loader?: string
  } = $props()

  // ── Стан списку ──
  let mods = $state<ModEntry[]>([])
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

  let depsModal = $state(false)
  let deps = $state<ModDependency[]>([])
  let depsLoading = $state(false)
  let installingDeps = $state(false)

  let updateAllOpen = $state(false)
  let updateAllBackup = $state(true)
  let updateAllRunning = $state(false)
  let updateAllProgress = $state('')

  let backupsOpen = $state(false)
  let backups = $state<ModBackupEntry[]>([])
  let restoringId = $state('')

  let deleting = $state('')

  // ── Похідні ──
  const updatesCount = $derived(mods.filter(m => m.hasUpdate).length)
  const disabledCount = $derived(mods.filter(m => !m.enabled).length)

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
    else arr.sort((a, b) => (b.installedDate || '').localeCompare(a.installedDate || ''))
    return arr
  })

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

  async function loadMods(): Promise<ModEntry[]> {
    scanning = true
    err = ''
    try {
      const list = mcVersion && loader
        ? await App.ScanModsWithCachedUpdates(packId, mcVersion, loader)
        : await App.ScanModsFull(packId)
      mods = list
      return list
    } catch (e: any) {
      err = String(e?.message ?? e)
      return []
    } finally {
      scanning = false
    }
  }

  async function checkUpdates(force = false) {
    if (!mcVersion || !loader || updateChecking) return
    // Без force — це ПЕРШИЙ автоматичний прохід для цієї версії/лоадера;
    // якщо кеш уже відповів (мод має Status === 'done' після loadMods),
    // повторно лізти в мережу не треба.
    if (!force && haveCheckedOnce) return
    updateChecking = true
    checkDone = 0
    checkTotal = 0
    try {
      mods = await App.RefreshModUpdates(packId, mcVersion, loader)
      haveCheckedOnce = true
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      updateChecking = false
    }
  }

  $effect(() => {
    const id = packId
    mods = []
    haveCheckedOnce = false
    loadMods().then(list => {
      // Якщо кеш уже дав статус оновлень для всіх модів — не лізем у
      // мережу вдруге. Інакше (перше відкриття для цієї версії/лоадера
      // збірки) — один автоматичний мережевий прохід.
      const allKnown = list.length === 0 || list.every(m => m.status === 'done' || m.status === undefined)
      if (mcVersion && loader && !allKnown) checkUpdates(true)
    })
  })

  onMount(() => {
    const off = on('mods:update-progress', (p: any) => {
      if (p?.packId !== packId) return
      checkDone = p.done ?? checkDone
      checkTotal = p.total ?? checkTotal
      if (p.phase === 'update') {
        updateAllProgress = `${p.current ?? 0}/${p.total ?? 0}`
      }
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
    return () => { off(); offDir() }
  })

  // ── Дії ──
  async function toggleMod(m: ModEntry) {
    try {
      await App.ToggleMod(packId, m.fileName, !m.enabled)
      await loadMods()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  async function openVersion(m: ModEntry) {
    versionMod = m
    versionOpts = []
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

  async function updateOne(m: ModEntry) {
    const url = m.latestUrl || ''
    const opt: ModVersionOption = {
      id: '', name: m.name, versionNumber: m.latestVersion || '', gameVersion: mcVersion, loader,
      url, filename: url.split('/').pop() || (m.projectSlug || m.name) + '.jar', sha1: '', datePublished: ''
    }
    try {
      await App.InstallModVersion(packId, m.fileName, opt)
      toast(t('mods.updated'), 'success')
      await loadMods()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  async function deleteMod(m: ModEntry) {
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

  // Залежності. fileName відсутній (undefined) — залежності всієї
  // збірки (кнопка в тулбарі); переданий — лише залежності конкретного
  // мода (клік на бейдж «залежність» на картці). Раніше обидва виклики
  // йшли в одну й ту саму функцію без параметра, і бейдж на картці
  // завжди показував список усіх нестанов­лених залежностей збірки.
  let depsForMod = $state('')
  async function openDeps(fileName = '') {
    depsModal = true
    depsLoading = true
    depsForMod = fileName
    deps = []
    try {
      deps = (await App.ResolveModDependencies(packId, mcVersion, loader, fileName)) || []
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      depsLoading = false
    }
  }

  async function installDeps() {
    if (installingDeps) return
    installingDeps = true
    try {
      await App.InstallModDependencies(packId, deps)
      toast(t('mods.depsInstalled'), 'success')
      depsModal = false
      await loadMods()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      installingDeps = false
    }
  }

  // Оновити всі
  async function runUpdateAll() {
    if (updateAllRunning) return
    updateAllRunning = true
    updateAllProgress = ''
    try {
      const res = await App.UpdateAllMods(packId, mcVersion, loader, updateAllBackup)
      if (res.updated.length) toast(t('mods.updatedCount', { n: res.updated.length }), 'success')
      if (res.failed.length) toast(t('mods.updateFailed', { n: res.failed.length }), 'error')
      if (res.backedUp) toast(t('mods.backupCreated'), 'info')
      updateAllOpen = false
      await loadMods()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
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

  async function restoreBackup(b: ModBackupEntry) {
    const ok = await confirmDialog(t('mods.restoreConfirm', { name: b.name }), t('mods.restoreHint'), {
      confirmLabel: t('mods.restore'), cancelLabel: t('cancel'), danger: true
    })
    if (!ok) return
    restoringId = b.id
    try {
      await App.RestoreModBackup(packId, b)
      toast(t('mods.restored'), 'success')
      await loadMods()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      restoringId = ''
    }
  }

  function fmtDate(s?: string): string {
    if (!s) return ''
    const d = new Date(s)
    return isNaN(d.getTime()) ? '' : d.toLocaleDateString()
  }
</script>

<div class="mods">
  <!-- ═══ Тулбар ═══ -->
  <div class="mods-toolbar">
    <div class="mt-search">
      <i class="ti ti-search"></i>
      <input value={search} placeholder={t('mods.search')} oninput={(e: any) => search = e.target.value} />
    </div>
    <select class="mt-select" bind:value={filter} aria-label={t('mods.filter')}>
      <option value="all">{t('mods.filterAll')}</option>
      <option value="updates">{t('mods.filterUpdates')}</option>
      <option value="enabled">{t('mods.filterEnabled')}</option>
      <option value="disabled">{t('mods.filterDisabled')}</option>
    </select>
    <select class="mt-select" bind:value={sort} aria-label={t('mods.sort')}>
      <option value="date">{t('mods.sortDate')}</option>
      <option value="name">{t('mods.sortName')}</option>
      <option value="update">{t('mods.sortUpdate')}</option>
    </select>
    <button class="btn btn-ghost btn-sm" onclick={loadMods} title={t('mods.refresh')}><i class="ti ti-refresh"></i></button>
    <div class="mt-spacer"></div>
    <button class="btn btn-ghost btn-sm" onclick={openModsFolder} title={t('mods.openFolder')}><i class="ti ti-folder"></i></button>
    <button class="btn btn-ghost btn-sm" onclick={openBackups} title={t('mods.backups')}><i class="ti ti-archive"></i> {t('mods.backups')}</button>
    <button class="btn btn-ghost btn-sm" onclick={openDeps} title={t('mods.deps')}><i class="ti ti-link"></i> {t('mods.deps')}</button>
    <button class="btn btn-primary btn-sm" onclick={() => updateAllOpen = true}
            disabled={updatesCount === 0 || updateChecking || updateAllRunning}>
      <i class="ti ti-refresh"></i> {t('mods.updateAll')}
      {#if updatesCount > 0}<span class="upd-badge">{updatesCount}</span>{/if}
    </button>
  </div>

  <!-- ═══ Статус-рядок ═══ -->
  <div class="mods-status">
    {#if scanning}
      <span class="ms-item"><i class="ti ti-loader spin"></i> {t('mods.scanning')}</span>
    {:else}
      <span class="ms-item">{t('mods.count', { n: mods.length })}</span>
      {#if updatesCount > 0}<span class="ms-item acc"><i class="ti ti-arrow-up"></i> {t('mods.withUpdates', { n: updatesCount })}</span>{/if}
      {#if disabledCount > 0}<span class="ms-item dim">{t('mods.disabledCount', { n: disabledCount })}</span>{/if}
    {/if}
    {#if updateChecking}
      <span class="ms-item acc"><i class="ti ti-loader spin"></i> {t('mods.checking', { done: checkDone, total: checkTotal })}</span>
    {/if}
    {#if err}<span class="ms-item err">{err}</span>{/if}
  </div>

  <!-- ═══ Список модів ═══ -->
  {#if scanning && mods.length === 0}
    <div class="mods-empty"><i class="ti ti-loader spin"></i> {t('mods.scanning')}</div>
  {:else if visibleMods.length === 0}
    <div class="mods-empty">
      <i class="ti ti-puzzle"></i>
      {#if mods.length === 0}{t('mods.none')}{:else}{t('mods.noMatches')}{/if}
    </div>
  {:else}
    <div class="mods-list">
      {#each visibleMods as m (m.fileName)}
        <div class="mod-row" class:disabled={!m.enabled} class:upd={m.hasUpdate} class:dup={m.isDuplicate}>
          <div class="mod-ic">
            {#if m.iconData}<img src={m.iconData} alt="" loading="lazy" />
            {:else if m.iconUrl}<img src={m.iconUrl} alt="" loading="lazy" />
            {:else}<i class="ti ti-puzzle"></i>{/if}
            {#if m.source === 'modrinth'}
              <span class="mod-src mr" title="Modrinth"><i class="ti ti-brand-figma"></i></span>
            {:else if m.source === 'curseforge'}
              <span class="mod-src cf" title="CurseForge"><i class="ti ti-flame"></i></span>
            {/if}
          </div>

          <div class="mod-info">
            <div class="mod-name" title={m.description || m.name}>
              {m.name}
              {#if m.status === 'checking'}
                <i class="ti ti-loader spin mod-checking" title={t('mods.checkingOne')}></i>
              {:else if m.hasUpdate}
                <span class="badge upd">{m.latestVersion}</span>
              {/if}
              {#if m.hasMissingDeps}
                <span class="badge dep" role="button" tabindex="0" onclick={() => openDeps(m.fileName)}
                      title={t('mods.missingDeps', { n: m.missingDeps?.length ?? 0 })}>
                  <i class="ti ti-link"></i> {t('mods.deps')}
                </span>
              {/if}
              {#if !m.enabled}
                <span class="badge off">{t('mods.off')}</span>
              {/if}
              {#if m.isDuplicate}
                <span class="badge dup" title={t('mods.duplicateHint')}>
                  <i class="ti ti-copy"></i> {t('mods.duplicate')}
                </span>
              {/if}
            </div>
            <div class="mod-meta">
              {m.fileName}
              {#if m.author} · {m.author}{/if}
              {#if m.version} · {m.version}{/if}
            </div>
          </div>

          <div class="mod-acts">
            {#if m.hasUpdate}
              <button class="icon-btn upd" onclick={() => updateOne(m)} title={t('mods.updateOne')}><i class="ti ti-refresh"></i></button>
            {/if}
            <button class="icon-btn" onclick={() => openVersion(m)} title={t('mods.changeVersion')}><i class="ti ti-versions"></i></button>
            <button class="icon-btn" onclick={() => revealMod(m.fileName)} title={t('mods.revealInFolder')}><i class="ti ti-folder"></i></button>
            <button class="icon-btn del" onclick={() => deleteMod(m)} disabled={deleting === m.fileName} title={t('delete')}>
              {#if deleting === m.fileName}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-trash"></i>{/if}
            </button>
            <div class="toggle" class:on={m.enabled} role="switch" aria-checked={m.enabled} tabindex="0"
                 onclick={() => toggleMod(m)}
                 onkeydown={(e: any) => { if (e.key === 'Enter' || e.key === ' ') toggleMod(m) }}
                 title={m.enabled ? t('mods.disable') : t('mods.enable')}></div>
          </div>
        </div>
      {/each}
    </div>
  {/if}
</div>

<!-- ═══ Модалка: зміна версії ═══ -->
{#if versionMod}
  <div class="modal-overlay" onclick={() => { if (!installingVersion) versionMod = null }}>
    <div class="modal" style="max-width:540px" onclick={(e: any) => e.stopPropagation()}>
      <div class="modal-head">
        <h3><i class="ti ti-versions" style="color:var(--orange);margin-right:8px"></i>{t('mods.versionTitle', { name: versionMod.name })}</h3>
        <button class="md-close" onclick={() => versionMod = null} title={t('win.close')}><i class="ti ti-x"></i></button>
      </div>
      <div class="modal-body">
        {#if versionLoading}
          <div class="mods-empty"><i class="ti ti-loader spin"></i> {t('mods.loadingVersions')}</div>
        {:else if versionOpts.length === 0}
          <div class="mods-empty"><i class="ti ti-puzzle"></i> {t('mods.noVersions')}</div>
        {:else}
          <div class="ver-list">
            {#each versionOpts as v (v.id || v.url)}
              <div class="ver-row" class:current={v.versionNumber === versionMod.version}
                   onclick={() => !installingVersion && installVersion(v)}>
                <div class="vr-info">
                  <div class="vr-ver">{v.versionNumber}</div>
                  <div class="vr-meta">
                    {#if v.gameVersion}<span class="chip">{v.gameVersion}</span>{/if}
                    {#if v.loader}<span class="chip">{v.loader}</span>{/if}
                    {#if v.datePublished}<span class="vr-date">{fmtDate(v.datePublished)}</span>{/if}
                  </div>
                </div>
                {#if v.versionNumber === versionMod.version}
                  <span class="vr-current">{t('mods.current')}</span>
                {:else}
                  <i class="ti ti-download vr-icon"></i>
                {/if}
              </div>
            {/each}
          </div>
        {/if}
      </div>
      <div class="modal-foot">
        {#if installingVersion}
          <span class="md-working"><i class="ti ti-loader spin"></i> {t('mods.installing')}</span>
        {/if}
        <button class="btn btn-ghost" onclick={() => versionMod = null} disabled={installingVersion}>{t('cancel')}</button>
      </div>
    </div>
  </div>
{/if}

<!-- ═══ Модалка: залежності ═══ -->
{#if depsModal}
  <div class="modal-overlay">
    <div class="modal" style="max-width:560px" onclick={(e: any) => e.stopPropagation()}>
      <div class="modal-head">
        <h3><i class="ti ti-link" style="color:var(--violet-l);margin-right:8px"></i>{depsForMod ? t('mods.depsTitleFor', { name: mods.find(x => x.fileName === depsForMod)?.name ?? depsForMod }) : t('mods.depsTitle')}</h3>
        <button class="md-close" onclick={() => depsModal = false} title={t('win.close')}><i class="ti ti-x"></i></button>
      </div>
      <div class="modal-body">
        {#if depsLoading}
          <div class="mods-empty"><i class="ti ti-loader spin"></i> {t('mods.depsSearching')}</div>
        {:else if deps.length === 0}
          <div class="mods-empty"><i class="ti ti-circle-check" style="color:var(--green)"></i> {t('mods.depsNone')}</div>
        {:else}
          <div class="dep-list">
            {#each deps as d (d.modId)}
              <div class="dep-row" class:unresolved={!d.resolved}>
                <div class="dep-ic">
                  {#if d.iconUrl}<img src={d.iconUrl} alt="" loading="lazy" />{:else}<i class="ti ti-puzzle"></i>{/if}
                </div>
                <div class="dep-info">
                  <div class="dep-name">{d.name}</div>
                  <div class="dep-sub">{d.resolved ? d.version : (d.error || '')}</div>
                </div>
                {#if d.resolved}<span class="dep-ok"><i class="ti ti-circle-check"></i></span>
                {:else}<span class="dep-no"><i class="ti ti-alert-triangle"></i></span>{/if}
              </div>
            {/each}
          </div>
          <div class="dep-note"><i class="ti ti-info-circle"></i> {t('mods.depsNote', { n: deps.filter(d => d.resolved).length })}</div>
        {/if}
      </div>
      <div class="modal-foot">
        <button class="btn btn-ghost" onclick={() => depsModal = false} disabled={installingDeps}>{t('cancel')}</button>
        {#if deps.some(d => d.resolved)}
          <button class="btn btn-primary" onclick={installDeps} disabled={installingDeps || depsLoading}>
            {#if installingDeps}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-download"></i>{/if}
            {t('mods.depsInstall', { n: deps.filter(d => d.resolved).length })}
          </button>
        {/if}
      </div>
    </div>
  </div>
{/if}

<!-- ═══ Модалка: оновити всі ═══ -->
{#if updateAllOpen}
  {@const toUpd = mods.filter(m => m.enabled && m.hasUpdate)}
  <div class="modal-overlay" onclick={() => { if (!updateAllRunning) updateAllOpen = false }}>
    <div class="modal" style="max-width:600px" onclick={(e: any) => e.stopPropagation()}>
      <div class="modal-head">
        <h3><i class="ti ti-refresh" style="color:var(--orange);margin-right:8px"></i>{t('mods.updateAllTitle', { n: toUpd.length })}</h3>
        <button class="md-close" onclick={() => updateAllOpen = false} title={t('win.close')}><i class="ti ti-x"></i></button>
      </div>
      <div class="modal-body">
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
          <label class="ua-backup">
            <input type="checkbox" bind:checked={updateAllBackup} disabled={updateAllRunning} style="accent-color:var(--orange)" />
            <span>{t('mods.updateAllBackup')}</span>
          </label>
          {#if updateAllProgress}
            <div class="ua-prog"><i class="ti ti-loader spin"></i> {t('mods.updateProgress', { p: updateAllProgress })}</div>
          {/if}
        {/if}
      </div>
      <div class="modal-foot">
        <button class="btn btn-ghost" onclick={() => updateAllOpen = false} disabled={updateAllRunning}>{t('cancel')}</button>
        <button class="btn btn-primary" onclick={runUpdateAll} disabled={updateAllRunning || toUpd.length === 0}>
          {#if updateAllRunning}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-refresh"></i>{/if}
          {t('mods.updateAll')}
        </button>
      </div>
    </div>
  </div>
{/if}

<!-- ═══ Модалка: резервні копії ═══ -->
{#if backupsOpen}
  <div class="modal-overlay">
    <div class="modal" style="max-width:560px" onclick={(e: any) => e.stopPropagation()}>
      <div class="modal-head">
        <h3><i class="ti ti-archive" style="color:var(--green);margin-right:8px"></i>{t('mods.backupsTitle')}</h3>
        <button class="md-close" onclick={() => backupsOpen = false} title={t('win.close')}><i class="ti ti-x"></i></button>
      </div>
      <div class="modal-body">
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
                <button class="btn btn-ghost btn-sm" onclick={() => restoreBackup(b)} disabled={restoringId === b.id}>
                  {#if restoringId === b.id}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-rotate-clockwise"></i>{/if}
                  {t('mods.restore')}
                </button>
              </div>
            {/each}
          </div>
        {/if}
      </div>
    </div>
  </div>
{/if}

<style>
  .mods { display: flex; flex-direction: column; gap: 12px; }

  /* ── Тулбар ── */
  .mods-toolbar { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  .mt-search {
    display: flex; align-items: center; gap: 8px;
    background: var(--bg-input); border: 1px solid var(--border); border-radius: var(--r-sm);
    padding: 0 11px; height: 32px; flex: 0 1 260px; min-width: 160px;
  }
  .mt-search i { color: var(--text-dim); font-size: 14px; }
  .mt-search input { flex: 1; background: none; border: none; outline: none; color: var(--text); font-size: 12.5px; }
  .mt-select {
    height: 32px; background: var(--bg-input); border: 1px solid var(--border);
    border-radius: var(--r-sm); color: var(--text); font-size: 12px; padding: 0 8px; outline: none;
  }
  .mt-select:hover { border-color: var(--border-hi); }
  .mt-spacer { flex: 1; }
  .upd-badge {
    display: inline-flex; align-items: center; justify-content: center;
    min-width: 18px; height: 18px; padding: 0 5px; border-radius: 999px;
    background: rgba(255,255,255,.22); font-size: 10.5px; font-weight: 800; margin-left: 2px;
  }

  /* ── Статус ── */
  .mods-status { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; font-size: 11.5px; color: var(--text-mute); min-height: 16px; }
  .ms-item { display: inline-flex; align-items: center; gap: 5px; }
  .ms-item.acc { color: var(--orange); font-weight: 600; }
  .ms-item.dim { color: var(--text-dim); }
  .ms-item.err { color: var(--red); }

  /* ── Список модів (рядки, як у прикладі лаунчера) ── */
  .mods-list { display: flex; flex-direction: column; gap: 8px; }
  .mod-row {
    display: flex; align-items: center; gap: 14px;
    padding: 12px 16px; border-radius: var(--r-md);
    background: var(--bg-panel); border: 1px solid var(--border);
    transition: border-color .15s;
  }
  .mod-row:hover { border-color: var(--border-hi); }
  .mod-row.upd { border-color: rgba(var(--orange-rgb), .4); }
  .mod-row.disabled { opacity: .55; }
  .mod-row.dup { border-color: rgba(239,68,68,.5); animation: dup-blink 1.6s ease-in-out infinite; }
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
  .mod-src {
    position: absolute; right: -6px; bottom: -6px; width: 17px; height: 17px; border-radius: 50%;
    display: flex; align-items: center; justify-content: center; font-size: 10px;
    border: 2px solid var(--bg-panel);
  }
  .mod-src.mr { background: #1bd96a; color: #0b2d1a; }
  .mod-src.cf { background: #f16436; color: #fff; }

  .mod-info { flex: 1; min-width: 0; }
  .mod-name {
    font-size: 13px; font-weight: 700; color: var(--text);
    display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
  }
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

  /* ── Залежності ── */
  .dep-list { display: flex; flex-direction: column; gap: 6px; }
  .dep-row {
    display: flex; align-items: center; gap: 10px;
    padding: 9px 11px; background: var(--bg-input); border: 1px solid var(--border);
    border-radius: var(--r-sm);
  }
  .dep-row.unresolved { border-color: rgba(239,68,68,.35); }
  .dep-ic {
    width: 32px; height: 32px; border-radius: 8px; flex-shrink: 0;
    background: var(--bg-panel); border: 1px solid var(--border);
    display: flex; align-items: center; justify-content: center; overflow: hidden;
    font-size: 14px; color: var(--text-dim);
  }
  .dep-ic img { width: 100%; height: 100%; object-fit: cover; }
  .dep-info { flex: 1; min-width: 0; }
  .dep-name { font-size: 12.5px; font-weight: 700; color: var(--text); }
  .dep-sub { font-size: 11px; color: var(--text-mute); font-family: var(--font-mono); }
  .dep-ok { color: var(--green); font-size: 15px; }
  .dep-no { color: var(--red); font-size: 15px; }
  .dep-note {
    display: flex; align-items: center; gap: 6px; margin-top: 10px;
    font-size: 11.5px; color: var(--text-mute); line-height: 1.5;
  }

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
  }
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
</style>
