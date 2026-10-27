<script lang="ts">
  // ── Розділ «Контент» на сторінці збірки ─────────────────────────────────
  // Під-вкладки: Моди / Ресурспаки / Датапаки / Шейдери
  // Ресурспаки/датапаки/шейдери: картки як у менеджері модів, drag&drop,
  // вибір файлів, мульти-вибір, копіювання в інші збірки (з перевіркою
  // сумісності через Modrinth), перехід на сторінку проєкту.
  import { t } from './i18n.svelte'
  import { App } from './wails'
  import { toast, confirmDialog } from './toast.svelte'
  import UiModal from './UiModal.svelte'
  import PackPicker from './PackPicker.svelte'
  import PackModsSection from './PackModsSection.svelte'
  import SelectField from './SelectField.svelte'
  import modrinthIcon from '../assets/images/Modrinth_icon.png'
  import curseforgeIcon from '../assets/images/curseforge_icon.png'
  import { getPackUI, setPackUI, readContentScroll, restoreContentScroll } from './uiState'
  import type { PackPickerItem, PackRow, PackFolderRow } from './PackPicker.svelte'
  import type { ContentFileEntry, ModVersionOption, BuildView } from './types'

  let {
    packId,
    packName = '',
    mcVersion = '',
    loader = '',
    canMods = true,
    onSearchContent = null as null | ((projectType: string) => void),
    onOpenModPage = null as null | ((source: string, projectId: string) => void),
    dupFocusSignal = 0,
    allBuilds = [] as BuildView[],
    // Збірка зараз запущена (на будь-якому акаунті) — змінювати контент
    // заборонено. Фронтенд блокує мутації модалкою ДО виклику бекенду.
    running = false,
    packPicker = null as null | {
      items: PackPickerItem[]
      rowById: Map<string, PackRow>
      folderById: Map<string, PackFolderRow>
    },
    // Висота закріпленого тулбара сторінки збірки — передається в
    // менеджер модів (sticky-відступ його тулбара).
    stickyTop = 0
  } = $props<{
    packId: string
    packName?: string
    mcVersion?: string
    loader?: string
    canMods?: boolean
    onSearchContent?: null | ((projectType: string) => void)
    onOpenModPage?: null | ((source: string, projectId: string) => void)
    dupFocusSignal?: number
    allBuilds?: BuildView[]
    running?: boolean
    packPicker?: {
      items: PackPickerItem[]
      rowById: Map<string, PackRow>
      folderById: Map<string, PackFolderRow>
    } | null
    stickyTop?: number
  }>()

  // ── Під-вкладки ──
  const subTabs = [
    { id: 'mods',         contentDir: 'mods',         projectType: 'mod',         label: () => t('content.tab.mods'),         icon: 'ti-puzzle',   color: 'var(--green)',   mods: true },
    { id: 'resourcepacks',contentDir: 'resourcepacks', projectType: 'resourcepack',label: () => t('content.tab.resourcepacks'),icon: 'ti-photo',    color: 'var(--violet)',  mods: false },
    { id: 'datapacks',    contentDir: 'datapacks',     projectType: 'datapack',    label: () => t('content.tab.datapacks'),    icon: 'ti-database', color: 'var(--blue)',    mods: false },
    { id: 'shaderpacks',  contentDir: 'shaderpacks',   projectType: 'shader',      label: () => t('content.tab.shaders'),      icon: 'ti-brush',    color: '#ff6b9d',        mods: false },
  ]
  let sub = $state<string>(canMods ? 'mods' : 'resourcepacks')

  // ── Стан списку контенту ──
  let entries    = $state<ContentFileEntry[]>([])
  let scanning   = $state(false)
  let importing  = $state(false)
  let dragOver   = $state(false)
  let err        = $state('')
  let search     = $state('')
  let filter     = $state<'all' | 'enabled' | 'disabled'>('all')

  // ── Мульти-вибір (як у менеджері модів) ──
  let selected    = $state<Set<string>>(new Set())
  let bulkWorking = $state(false)
  const selectMode = $derived(selected.size > 0)

  function toggleSelect(fileName: string, e: MouseEvent) {
    e.stopPropagation()
    const next = new Set(selected)
    if (next.has(fileName)) next.delete(fileName)
    else next.add(fileName)
    selected = next
  }
  function clearSelection() { selected = new Set() }
  function selectAllVisible() { selected = new Set(visibleEntries.map(e => e.fileName)) }

  const selectedPreview = $derived.by(() => {
    if (selected.size === 0) return []
    const res: ContentFileEntry[] = []
    for (const f of selected) {
      const e = entries.find(en => en.fileName === f)
      if (e) res.push(e)
      if (res.length >= 3) break
    }
    return res
  })

  const visibleEntries = $derived(
    entries.filter(e => {
      if (filter === 'enabled'  && !e.enabled) return false
      if (filter === 'disabled' &&  e.enabled) return false
      const q = search.trim().toLowerCase()
      return !q || e.name.toLowerCase().includes(q) || e.fileName.toLowerCase().includes(q)
    })
  )

  const activeTab = $derived(subTabs.find(s => s.id === sub) ?? subTabs[0])

  // Іконка/колір ПОТОЧНОЇ збірки — лише для фіксованої лівої картки в
  // PackCompareDialog (менеджер модів сам по собі їх не потребує).
  // iconUrl (PNG) має пріоритет над пресетом icon — PackCompareChip сам
  // вирішує, що показати, як і всі інші картки збірок у застосунку.
  const currentBuild = $derived(allBuilds.find((bv: BuildView) => bv.build.id === packId))
  const currentIconUrl = $derived(currentBuild?.build.iconUrl ?? '')
  const currentIcon = $derived(currentBuild?.build.icon ?? '')
  const currentColor = $derived(currentBuild?.build.color ?? '')

  // ── Захист від змін під час запущеної гри ──
  // Збірка запущена (status='running' або runningOn інших акаунтів) —
  // змінювати моди/ресурспаки/шейдери/версію заборонено. Замість того,
  // щоб чекати OS-помилку "used by another process" від бекенду, блокуємо
  // дію ОДРАЗУ модалкою з поясненням (ТЗ: «казати користувачу у
  // модульному вікні, а не чекати помилки»).
  let runningLocked = $state(false)
  function guardNotRunning(): boolean {
    if (!running) return true
    runningLocked = true
    return false
  }

  // ── Скрол ──
  let lastSub = ''
  function currentSubScrollKey() { return 'content:' + sub }

  $effect(() => {
    if (sub !== lastSub) {
      if (lastSub) {
        const prev = getPackUI(packId)
        const prevKeys = prev?.scrolls ?? {}
        prevKeys['content:' + lastSub] = readContentScroll()
        setPackUI(packId, { tab: prev?.tab ?? 'content', scroll: prevKeys['content:' + lastSub], scrolls: { ...prevKeys } })
      }
      lastSub = sub
      search = ''
      selected = new Set()
      loadEntries()
      const cur = getPackUI(packId)
      restoreContentScroll(cur?.scrolls?.['content:' + sub] ?? 0)
    }
  })

  $effect(() => {
    const el = document.querySelector('.content') as HTMLElement | null
    if (!el) return
    const onScr = () => {
      const cur = getPackUI(packId)
      const keys = { ...(cur?.scrolls ?? {}) }
      keys[currentSubScrollKey()] = readContentScroll()
      setPackUI(packId, { tab: cur?.tab ?? 'content', scroll: readContentScroll(), scrolls: keys })
    }
    el.addEventListener('scroll', onScr, { passive: true })
    return () => el.removeEventListener('scroll', onScr)
  })

  $effect(() => {
    if (dupFocusSignal > 0 && sub !== 'mods' && canMods) sub = 'mods'
  })

  // ── Завантаження списку ──
  async function loadEntries() {
    if (activeTab.mods) return
    scanning = true
    err = ''
    try {
      entries = (await App.RefreshContentUpdates(packId, activeTab.contentDir, mcVersion)) ?? []
    } catch (e: any) {
      err = String(e)
      entries = []
    } finally {
      scanning = false
    }
  }	  async function checkUpdates() {
	    if (activeTab.mods) return
	    await loadEntries()
	    toast(t('content.updatesChecked'), 'success')
	  }

	  // ── «Оновити всі» / оновити один (паритет з менеджером модів) ──
	  // Контент оновлюється через той самий механізм, що модалка «Змінити
	  // версію»: GetContentVersions (Modrinth) → InstallContentVersion.
	  // Авто-вибір «найкращої» версії — останній стабільний release
	  // (пріоритет, як у браузері контенту), інакше перша не-поточна.
	  const updatableCount = $derived(entries.filter(e => !e.isDir && e.hasUpdate).length)
	  let updatingAll = $state(false)
	  let updateAllDone = $state(0)

	  function pickBestUpdateOption(opts: ModVersionOption[]): ModVersionOption | null {
	    const candidates = opts.filter(o => !o.isCurrent && o.url && o.filename)
	    if (candidates.length === 0) return null
	    // release (або невідомий тип) — найсвіжіший перший у списку API
	    for (const o of candidates) if (!o.versionType || o.versionType === 'release') return o
	    return candidates[0]
	  }

	  // Оновити ОДИН файл одразу до найкращої версії (кнопка-стрілка на рядку
	  // з бейджем оновлення). Повертає встановлену версію або null, якщо
	  // оновлення недоступне.
	  async function updateEntry(e: ContentFileEntry): Promise<string | null> {
	    if (!guardNotRunning()) return null
	    const opts = (await App.GetContentVersions(packId, activeTab.contentDir, e.fileName, mcVersion)) ?? []
	    const best = pickBestUpdateOption(opts)
	    if (!best) return null
	    await App.InstallContentVersion(packId, activeTab.contentDir, e.fileName, best)
	    return best.versionNumber
	  }

	  async function updateOne(e: ContentFileEntry) {
	    if (updatingAll) return
	    try {
	      const v = await updateEntry(e)
	      if (v) {
	        await loadEntries()
	        toast(t('content.versionInstalled', { name: e.name, version: v }), 'success')
	      }
	    } catch (ex: any) {
	      toast(t('content.updateFail', { name: e.name, error: ex }), 'error')
	    }
	  }

	  // Послідовно оновлює всі файли з бейджем «оновлення» (як «Оновити всі»
	  // у модах): стабільні спершу, лічильник — у статус-рядку тулбара.
	  async function updateAllNow() {
	    if (updatingAll || updatableCount === 0) return
	    if (!guardNotRunning()) return
	    const targets = entries.filter(e => !e.isDir && e.hasUpdate)
	    updatingAll = true
	    updateAllDone = 0
	    let failed = 0
	    try {
	      for (const e of targets) {
	        try {
	          await updateEntry(e)
	        } catch {
	          failed++
	        }
	        updateAllDone++
	      }
	      await loadEntries()
	      const ok = targets.length - failed
	      if (failed === 0) {
	        toast(t('content.updateDone', { n: ok }), 'success')
	      } else {
	        toast(t('content.updatePartial', { ok, failed }), ok > 0 ? 'warn' : 'error')
	      }
	    } finally {
	      updatingAll = false
	    }
	  }


  // ── Дії з одним записом ──
  // Увімкнення/вимкнення (toggle) дозволено НАВІТЬ під час запущеної гри:
  // це лише перейменування файла у .disabled (Minecraft ігнорує такі файли
  // при наступному старті, а запущену сесію не ламає). Заборонені лише
  // мутації, що замінюють/видаляють файли на диску (delete/update/версія).
  async function toggleEntry(e: ContentFileEntry) {
    try {
      await App.ToggleContentFile(packId, activeTab.contentDir, e.fileName, !e.enabled)
      await loadEntries()
    } catch (ex: any) {
      toast(t('error.withMessage', { message: ex }), 'error')
    }
  }

  async function deleteEntry(e: ContentFileEntry) {
    if (!guardNotRunning()) return
    const ok = await confirmDialog(
      t('content.deleteConfirm', { name: e.name }),
      t('content.deleteConfirmHint'),
      { confirmLabel: t('delete'), cancelLabel: t('cancel'), danger: true }
    )
    if (!ok) return
    try {
      await App.DeleteContentFile(packId, activeTab.contentDir, e.fileName)
      selected.delete(e.fileName)
      selected = new Set(selected)
      await loadEntries()
    } catch (ex: any) {
      toast(t('error.withMessage', { message: ex }), 'error')
    }
  }

  async function openFolder() {
    try { await App.OpenContentFolder(packId, activeTab.contentDir) }
    catch (ex: any) { toast(t('error.withMessage', { message: ex }), 'error') }
  }

  // ── Відкрити сторінку проєкту (якщо є projectId та source) ──
  function isPageClickable(e: ContentFileEntry) {
    return !!onOpenModPage && !!e.projectId && !!e.source && e.source !== 'unknown'
  }
  function openProjectPage(e: ContentFileEntry) {
    if (!onOpenModPage || !e.projectId || !e.source) return
    onOpenModPage(e.source, e.projectId)
  }

  // ── Імпорт файлів (діалог + drag&drop) ──
  async function pickAndImportFiles() {
    if (importing) return
    if (!guardNotRunning()) return
    try {
      const paths = await App.PickContentFiles(activeTab.contentDir)
      if (!paths || paths.length === 0) return
      await importFiles(paths)
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  async function importFiles(paths: string[]) {
    if (!guardNotRunning()) return
    importing = true
    try {
      const res = await App.ImportContentFiles(packId, activeTab.contentDir, paths)
      const imported = res?.imported ?? []
      const skipped  = res?.skipped  ?? []
      const failed   = res?.failed   ?? []
      if (imported.length > 0) toast(t('content.importedCount', { n: imported.length }), 'success')
      if (skipped.length  > 0) toast(t('content.importSkipped',  { n: skipped.length  }), 'info')
      if (failed.length   > 0) toast(t('content.importFailed',   { n: failed.length   }), 'error')
      await loadEntries()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      importing = false
    }
  }

  function onDrop(e: DragEvent) {
    e.preventDefault()
    dragOver = false
    const files = [...(e.dataTransfer?.files ?? [])] as any[]
    const paths = files.map(f => f.path).filter((p): p is string => !!p)
    if (paths.length > 0) importFiles(paths)
  }

  // ── Масові дії ──
  async function bulkToggle(enable: boolean) {
    if (bulkWorking || selected.size === 0) return
    // Toggle дозволений і під час гри (див. toggleEntry).
    bulkWorking = true
    try {
      const files = [...selected]
      for (const f of files) {
        const e = entries.find(en => en.fileName === f)
        if (e && e.enabled !== enable)
          await App.ToggleContentFile(packId, activeTab.contentDir, f, enable)
      }
      await loadEntries()
    } catch (ex: any) {
      toast(t('error.withMessage', { message: ex }), 'error')
    } finally {
      bulkWorking = false
    }
  }

  async function bulkDelete() {
    if (bulkWorking || selected.size === 0) return
    if (!guardNotRunning()) return
    const ok = await confirmDialog(
      t('content.bulkDeleteConfirm', { n: selected.size }),
      t('content.bulkDeleteHint'),
      { confirmLabel: t('delete'), cancelLabel: t('cancel'), danger: true }
    )
    if (!ok) return
    bulkWorking = true
    try {
      for (const f of [...selected])
        await App.DeleteContentFile(packId, activeTab.contentDir, f)
      clearSelection()
      await loadEntries()
    } catch (ex: any) {
      toast(t('error.withMessage', { message: ex }), 'error')
    } finally {
      bulkWorking = false
    }
  }

  // ── Копіювання в іншу збірку (Smart Copy) ──
  let copyEntry       = $state<ContentFileEntry | null>(null)
  let copyOpen        = $state(false)
  let copyQuery       = $state('')
  let copyTarget      = $state('')
  let copying         = $state(false)
  let copyPresence    = $state<Record<string, boolean>>({})
  let copyChecking    = $state(false)

  const copyCandidates = $derived<BuildView[]>(
    allBuilds.filter((b: BuildView) => b.build.id !== packId)
  )
  const copyAllowedIds = $derived<Set<string>>(new Set(copyCandidates.map(c => c.build.id)))

  async function openCopy(e: ContentFileEntry) {
    if (!guardNotRunning()) return
    copyEntry     = e
    copyQuery     = ''
    copyTarget    = ''
    copyPresence  = {}
    copyOpen      = true
    copyChecking  = true
    try {
      copyPresence = await App.CheckContentInBuilds(e.fileName, activeTab.contentDir) ?? {}
    } catch { /* ignore */ }
    finally { copyChecking = false }
  }

  async function doCopy() {
    const entry = copyEntry
    if (!entry || !copyTarget || copying) return
    const tgt = copyCandidates.find(c => c.build.id === copyTarget)
    copying = true
    try {
      await App.SmartCopyContentFile(packId, activeTab.contentDir, entry.fileName, copyTarget, tgt?.build.mcVersion ?? mcVersion)
      toast(t('content.copyDone', { name: entry.name, pack: tgt?.build.name ?? copyTarget }), 'success')
      copyOpen = false
      copyEntry = null
    } catch (e: any) {
      // Файл вже є — питаємо про перезапис через TransferContentFile з overwrite
      const conflict = String(e).includes('уже є')
      if (conflict) {
        const ok = await confirmDialog(
          t('content.transferOverwrite', { name: entry.name, pack: tgt?.build.name ?? copyTarget }),
          t('content.transferOverwriteHint'),
          { confirmLabel: t('replace'), cancelLabel: t('cancel'), danger: true }
        )
        if (ok) {
          try {
            await App.TransferContentFile(packId, activeTab.contentDir, entry.fileName, copyTarget, true)
            toast(t('content.copyDone', { name: entry.name, pack: tgt?.build.name ?? copyTarget }), 'success')
            copyOpen = false
            copyEntry = null
          } catch (ex: any) {
            toast(t('error.withMessage', { message: ex }), 'error')
          }
        }
      } else {
        toast(t('error.withMessage', { message: e }), 'error')
      }
    } finally {
      copying = false
    }
  }

  // ── Зміна версії ──
  let verEntry      = $state<ContentFileEntry | null>(null)
  let verOpen       = $state(false)
  let verLoading    = $state(false)
  let verVersions   = $state<ModVersionOption[]>([])
  let verErr        = $state('')
  let verInstalling = $state(false)

  async function openVersions(e: ContentFileEntry) {
    if (!guardNotRunning()) return
    verEntry   = e
    verOpen    = true
    verVersions = []
    verErr     = ''
    verLoading  = true
    try {
      verVersions = (await App.GetContentVersions(packId, activeTab.contentDir, e.fileName, mcVersion)) ?? []
    } catch (ex: any) {
      verErr = String(ex)
    } finally {
      verLoading = false
    }
  }

  async function installVersion(opt: ModVersionOption) {
    const e = verEntry
    if (!e || verInstalling) return
    if (!guardNotRunning()) return
    verInstalling = true
    try {
      await App.InstallContentVersion(packId, activeTab.contentDir, e.fileName, opt)
      await loadEntries()
      verOpen  = false
      verEntry = null
      toast(t('content.versionInstalled', { name: e.name, version: opt.versionNumber }), 'success')
    } catch (ex: any) {
      toast(t('error.withMessage', { message: ex }), 'error')
    } finally {
      verInstalling = false
    }
  }

  async function copyLink(e: ContentFileEntry) {
    try {
      const link = await App.GetContentFileLink(packId, activeTab.contentDir, e.fileName, mcVersion)
      if (!link) { toast(t('content.noLink'), 'info'); return }
      await navigator.clipboard.writeText(link)
      toast(t('content.linkCopied'), 'success')
    } catch (ex: any) {
      toast(t('error.withMessage', { message: ex }), 'error')
    }
  }
</script>

<div class="content-section">
  <!-- ── Під-вкладки ── -->
  <div class="ct-subtabs">
    {#each subTabs.filter(s => !s.mods || canMods) as st (st.id)}
      <button
        class="ct-subtab"
        class:active={sub === st.id}
        style="--ct-c:{st.color}"
        onclick={() => sub = st.id}
      >
        <i class="ti {st.icon}"></i>{st.label()}
      </button>
    {/each}
  </div>

  {#if activeTab.mods}
    <!-- Моди — той самий повноцінний менеджер модів -->
    <PackModsSection
      {packId}
      {packName}
      {mcVersion}
      {loader}
      iconUrl={currentIconUrl}
      icon={currentIcon}
      color={currentColor}
      onSearchMods={onSearchContent ? () => onSearchContent('mod') : null}
      {onOpenModPage}
      {dupFocusSignal}
      {running}
      {stickyTop}
      {packPicker}
    />
  {:else}
    <!-- ── Ресурспаки / Датапаки / Шейдери ── -->	    <!-- Тулбар — та сама структура й стиль, що в менеджера модів
	         (два рядки, sticky з тим самим відступом під закріпленим
	         тулбаром сторінки; кнопки: оновити/тека/додати файл/додати з
	         браузера/«Оновити всі» з лічильником). Раніше тут були лише
	         іконки без тексту й БЕЗ «Оновити всі» — розділи виглядали по-
	         різному, а кнопок бракувало. -->
	    <div class="ct-toolbar" style={`--mt-top:${stickyTop}px`}>
	      <div class="ct-toolbar-main">
	        <div class="ct-search">
	          <i class="ti ti-search"></i>
	          <input value={search} placeholder={t('content.search')} oninput={(e: any) => search = e.target.value} />
	        </div>
	        <SelectField
	          value={filter}
	          options={[
	            { value: 'all',      label: t('content.filterAll')      },
	            { value: 'enabled',  label: t('content.filterEnabled')  },
	            { value: 'disabled', label: t('content.filterDisabled') },
	          ]}
	          onchange={(v: string) => filter = v as 'all' | 'enabled' | 'disabled'}
	          minWidth="130px"
	          ariaLabel={t('content.search')}
	        />
	      </div>
	      <div class="ct-toolbar-actions">
	        <button class="btn btn-ghost btn-sm icon-tb" onclick={checkUpdates} title={t('content.checkUpdates')} disabled={scanning}>
	          <i class="ti {scanning ? 'ti-loader spin' : 'ti-refresh'}"></i>
	        </button>
	        <button class="btn btn-ghost btn-sm icon-tb" onclick={openFolder} title={t('content.openFolder')}>
	          <i class="ti ti-folder-open"></i>
	        </button>
	        {#if onSearchContent}
	          <button class="btn btn-ghost btn-sm" onclick={() => onSearchContent(activeTab.projectType)} title={t('content.addFromBrowser')}>
	            <i class="ti ti-world-search"></i> {t('content.addFromBrowser')}
	          </button>
	        {/if}
	        <button class="btn btn-ghost btn-sm" onclick={pickAndImportFiles} disabled={importing} title={t('content.addFile')}>
	          {#if importing}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-file-plus"></i>{/if} {t('content.addFile')}
	        </button>
	        <button class="btn btn-primary btn-sm" onclick={updateAllNow}
	                disabled={updatingAll || updatableCount === 0 || scanning}
	                title={updatableCount > 0 ? t('content.updateAll') : t('content.updateAllNone')}>
	          {#if updatingAll}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-refresh"></i>{/if} {t('content.updateAll')}
	          {#if updatableCount > 0}<span class="upd-badge">{updatableCount}</span>{/if}
	        </button>
	      </div>
	    </div>

	    <!-- Статус-рядок — як у менеджера модів (кількість, оновлення) -->
	    <div class="ct-status">
	      {#if scanning}
	        <span class="cs-item"><i class="ti ti-loader spin"></i> {t('content.loading')}</span>
	      {:else}
	        <span class="cs-item">{t('content.filesCount', { n: entries.length })}</span>
	        {#if updatableCount > 0}
	          <span class="cs-item acc"><i class="ti ti-arrow-up"></i> {t('content.updatesFound', { n: updatableCount })}</span>
	        {/if}
	        {#if err}<span class="cs-item err">{err}</span>{/if}
	      {/if}
	      {#if updatingAll}
	        <span class="cs-item acc"><i class="ti ti-loader spin"></i> {t('content.updateProgress', { done: updateAllDone, total: updatableCount })}</span>
	      {/if}
	    </div>


    <!-- Список з drag&drop -->
    <div
      class="ct-dropzone"
      class:drag={dragOver}
      style="--ct-c:{activeTab.color}"
      ondragover={(e: DragEvent) => { e.preventDefault(); dragOver = true }}
      ondragleave={() => dragOver = false}
      ondrop={onDrop}
    >
      {#if scanning && entries.length === 0}
        <div class="ct-scanning">
          <i class="ti ti-loader spin"></i> {t('content.loading')}
        </div>

      {:else if err}
        <div class="ct-empty-folder">
          <div class="ct-ef-ic" style="--ct-c:var(--red)"><i class="ti ti-alert-triangle"></i></div>
          <div class="ct-ef-title" style="color:var(--red)">{err}</div>
        </div>

      {:else if entries.length === 0}
        <!-- Порожня тека — стиль як у менеджері модів -->
        <div class="ct-empty-folder">
          <div class="ct-ef-ic">
            <i class="ti {activeTab.icon}"></i>
          </div>
          <div class="ct-ef-title">{t('content.emptyFolderTitle')}</div>
          <div class="ct-ef-sub">{t('content.emptyFolderHint')}</div>
          <div class="ct-ef-actions">
            <button class="btn btn-primary" onclick={pickAndImportFiles} disabled={importing}>
              {#if importing}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-file-plus"></i>{/if}
              {t('content.addFile')}
            </button>
            <button class="btn btn-ghost" onclick={openFolder}>
              <i class="ti ti-folder"></i> {t('content.openFolder')}
            </button>
            {#if onSearchContent}
              <button class="btn btn-ghost" onclick={() => onSearchContent(activeTab.projectType)}>
                <i class="ti ti-world-search"></i> {t('content.addFromBrowser')}
              </button>
            {/if}
          </div>
          <div class="ct-ef-drop"><i class="ti ti-download"></i> {t('content.emptyDropHint')}</div>
        </div>

      {:else if visibleEntries.length === 0}
        <!-- Є файли але фільтр/пошук нічого не показує -->
        <div class="ct-empty-folder">
          <div class="ct-ef-ic"><i class="ti ti-search-off"></i></div>
          <div class="ct-ef-title">{t('content.empty')}</div>
          <div class="ct-ef-sub">{t('content.emptyHint', { dir: activeTab.contentDir })}</div>
        </div>

      {:else}
        <!-- Список карток ресурсів — точно як у менеджері модів -->
        <div class="ct-list">
          {#each visibleEntries as e, i (e.fileName)}
            {@const clickable = isPageClickable(e)}
            <div
              class="ct-row rise-in"
              class:off={!e.enabled}
              class:upd={e.hasUpdate}
              class:selected={selected.has(e.fileName)}
              style="animation-delay:{Math.min(i,14)*24}ms; --ct-c:{activeTab.color}"
            >
              <!-- Чекбокс вибору (як у менеджері модів) -->
              <button
                type="button"
                role="checkbox"
                aria-checked={selected.has(e.fileName)}
                class="ct-check"
                class:checked={selected.has(e.fileName)}
                onclick={(ev: MouseEvent) => toggleSelect(e.fileName, ev)}
                aria-label={t('content.select')}
              >
                <i class="ti ti-check"></i>
              </button>

              <!-- Іконка з переходом на сторінку проєкту при наведенні -->
              {#if clickable}
                <button
                  type="button"
                  class="ct-ic clickable"
                  title={t('content.openPage')}
                  onclick={(ev: MouseEvent) => { ev.stopPropagation(); openProjectPage(e) }}
                >
                  {#if e.isDir}
                    <i class="ti ti-folder"></i>
                  {:else if e.iconUrl}
                    <img src={e.iconUrl} alt="" loading="lazy" />
                  {:else}
                    <i class="ti {activeTab.icon}"></i>
                  {/if}
                  <!-- Значок джерела (Modrinth/CurseForge) -->
                  {#if e.source === 'modrinth'}
                    <span class="ct-src" title="Modrinth"><img src={modrinthIcon} alt="Modrinth" loading="lazy" /></span>
                  {:else if e.source === 'curseforge'}
                    <span class="ct-src" title="CurseForge"><img src={curseforgeIcon} alt="CurseForge" loading="lazy" /></span>
                  {/if}
                </button>
              {:else}
                <div class="ct-ic">
                  {#if e.isDir}
                    <i class="ti ti-folder"></i>
                  {:else if e.iconUrl}
                    <img src={e.iconUrl} alt="" loading="lazy" />
                  {:else}
                    <i class="ti {activeTab.icon}"></i>
                  {/if}
                  {#if e.source === 'modrinth'}
                    <span class="ct-src" title="Modrinth"><img src={modrinthIcon} alt="Modrinth" loading="lazy" /></span>
                  {:else if e.source === 'curseforge'}
                    <span class="ct-src" title="CurseForge"><img src={curseforgeIcon} alt="CurseForge" loading="lazy" /></span>
                  {/if}
                </div>
              {/if}

              <!-- Інформація -->
              <div class="ct-info">
                <div class="ct-name">
                  {#if clickable}
                    <button
                      type="button"
                      class="ct-name-btn"
                      title={t('content.openPage')}
                      onclick={(ev: MouseEvent) => { ev.stopPropagation(); openProjectPage(e) }}
                    >{e.name}</button>
                  {:else}
                    <span>{e.name}</span>
                  {/if}
                  {#if e.hasUpdate}
                    <span class="badge upd" title={t('content.hasUpdate', { ver: e.latestVersion ?? '' })}>
                      <i class="ti ti-arrow-up"></i>{e.version ? e.version + ' → ' : ''}{e.latestVersion}
                    </span>
                  {/if}
                  {#if !e.enabled}
                    <span class="badge off">{t('content.off')}</span>
                  {/if}
                </div>
                <div class="ct-meta">
                  {#if e.version}{e.version}{/if}
                  {#if e.author} · {e.author}{/if}
                  {#if e.fileName} · {e.fileName}{/if}
                </div>
              </div>	              <!-- Дії -->
	              <div class="ct-acts">
	                {#if !e.isDir}
	                  <!-- Оновити до останньої версії — лише коли є оновлення
	                       (як кнопка оновлення у менеджері модів) -->
	                  {#if e.hasUpdate}
	                    <button class="icon-btn upd" onclick={() => updateOne(e)} title={t('content.updateOne')} disabled={updatingAll}>
	                      <i class="ti ti-arrow-up"></i>
	                    </button>
	                  {/if}
	                  <!-- Зміна версії — іконка міграції ресурса (ti-replace) -->
	                  <button class="icon-btn" onclick={() => openVersions(e)} title={t('content.changeVersion')}>
	                    <i class="ti ti-replace"></i>
	                  </button>
                  <!-- Скопіювати посилання -->
                  <button class="icon-btn" onclick={() => copyLink(e)} title={t('content.copyLink')}>
                    <i class="ti ti-link"></i>
                  </button>
                {/if}
                <!-- Копіювати в іншу збірку -->
                <button class="icon-btn" onclick={() => openCopy(e)} title={t('content.copyToPack')}>
                  <i class="ti ti-copy"></i>
                </button>
                <!-- Видалити -->
                <button class="icon-btn del" onclick={() => deleteEntry(e)} title={t('content.delete')}>
                  <i class="ti ti-trash"></i>
                </button>
                <!-- Перемикач увімкнення -->
                <button
                  class="ct-toggle"
                  class:on={e.enabled}
                  onclick={() => toggleEntry(e)}
                  title={e.enabled ? t('content.disable') : t('content.enable')}
                  aria-label={e.enabled ? t('content.disable') : t('content.enable')}
                >
                  <span class="ct-knob"></span>
                </button>
              </div>
            </div>
          {/each}
        </div>
      {/if}
    </div>

    <!-- ── Панель масових дій (sticky, знизу — як у менеджері модів) ── -->
    {#if selectMode}
      <div class="ct-bulk-bar">
        <div class="ct-bulk-icons">
          {#each selectedPreview as e (e.fileName)}
            <div class="ct-bulk-ic">
              {#if e.iconUrl}<img src={e.iconUrl} alt="" />{:else}<i class="ti {activeTab.icon}"></i>{/if}
            </div>
          {/each}
        </div>
        <span class="ct-bulk-count">{t('content.selectedCount', { n: selected.size })}</span>
        <div class="ct-bulk-spacer"></div>
        <button class="btn btn-ghost btn-sm" onclick={() => bulkToggle(true)} disabled={bulkWorking}>
          <i class="ti ti-power"></i> {t('content.bulkEnable')}
        </button>
        <button class="btn btn-ghost btn-sm" onclick={() => bulkToggle(false)} disabled={bulkWorking}>
          <i class="ti ti-power-off"></i> {t('content.bulkDisable')}
        </button>
        <button class="btn btn-ghost btn-sm" onclick={selectAllVisible} disabled={bulkWorking} title={t('content.selectAll')}>
          <i class="ti ti-square-check"></i> {t('content.selectAll')}
        </button>
        <button class="btn btn-ghost btn-sm ct-bulk-del" onclick={bulkDelete} disabled={bulkWorking}>
          <i class="ti ti-trash"></i> {t('content.bulkDelete')}
        </button>
        <button class="ct-bulk-clear" onclick={clearSelection} title={t('content.clearSelection')}>
          <i class="ti ti-x"></i>
        </button>
      </div>
    {/if}
  {/if}
</div>

<!-- ══════════════════════════════════════════════════════════════════════
     Модалка «Копіювати в іншу збірку» (Smart Copy)
     Дефолтний стиль UiModal — без кастомного кольору.
     Показує для кожної збірки: «Вже є» (зелений бейдж) або «Можна встановити».
     ══════════════════════════════════════════════════════════════════════ -->
{#if copyOpen && copyEntry}
  {@const entry = copyEntry}
  <UiModal
    open={true}
    size="lg"
    title={t('content.copyToPackTitle', { name: entry.name })}
    icon="ti-copy"
    onclose={() => { copyOpen = false; copyEntry = null }}
  >
    <p class="ct-modal-hint">{t('content.copyToPackHint', { name: entry.name })}</p>
    <p class="ct-modal-compat">{t('content.copyCompatNote')}</p>

    <div class="ct-modal-search">
      <i class="ti ti-search"></i>
      <input bind:value={copyQuery} placeholder={t('content.search')} />
    </div>

    {#if copyChecking}
      <div class="ct-modal-checking">
        <i class="ti ti-loader spin"></i> {t('content.copyChecking')}
      </div>
    {:else}
      <div class="ct-modal-list">
        {#if packPicker && packPicker.items.length > 0}
          <PackPicker
            items={packPicker.items}
            rowById={packPicker.rowById}
            folderById={packPicker.folderById}
            selected={copyTarget}
            onselect={(id: string) => copyTarget = id}
            query={copyQuery}
            allowedIds={copyAllowedIds}
            subtitle={(r, id) => {
              const already = copyPresence[id]
              return (already ? '✓ ' + t('content.alreadyIn') + '  ·  ' : '') + (r.loader ?? '') + ' · ' + (r.mcVersion ?? '')
            }}
          />
        {:else}
          {#each copyCandidates.filter((c: BuildView) => !copyQuery || c.build.name.toLowerCase().includes(copyQuery.toLowerCase())) as cp (cp.build.id)}
            <button
              class="ct-pack-item"
              class:sel={copyTarget === cp.build.id}
              onclick={() => copyTarget = cp.build.id}
            >
              <div class="ct-pack-ic" style="--pcol:{cp.build.color}">
                <i class="ti {cp.build.icon || 'ti-puzzle'}"></i>
              </div>
              <div class="ct-pack-info">
                <div class="ct-pack-name">{cp.build.name}</div>
                <div class="ct-pack-sub">{cp.build.loaderType} · {cp.build.mcVersion}</div>
              </div>
              {#if copyPresence[cp.build.id]}
                <span class="ct-presence-badge already">{t('content.alreadyIn')}</span>
              {:else}
                <span class="ct-presence-badge can">{t('content.canInstall')}</span>
              {/if}
              <i class="ti ti-check ct-pack-check"></i>
            </button>
          {:else}
            <div class="ct-modal-empty"><i class="ti ti-search-off"></i> —</div>
          {/each}
        {/if}
      </div>
    {/if}

    {#snippet footer()}
      <button class="btn btn-ghost" onclick={() => { copyOpen = false; copyEntry = null }}>{t('cancel')}</button>
      <button class="btn btn-primary" disabled={!copyTarget || copying} onclick={doCopy}>
        {#if copying}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-copy"></i>{/if}
        {t('content.copyBtn')}
      </button>
    {/snippet}
  </UiModal>
{/if}

<!-- ══════════════════════════════════════════════════════════════════════
     Модалка «Змінити версію» — іконка міграції ресурса (ti-replace)
     ══════════════════════════════════════════════════════════════════════ -->
{#if verOpen && verEntry}
  <UiModal
    open={true}
    size="lg"
    title={t('content.changeVersionTitle', { name: verEntry.name })}
    icon="ti-replace"
    color="var(--violet)"
    onclose={() => { verOpen = false; verEntry = null }}
  >
    <div class="ct-modal-hint" style="margin-bottom:10px">{t('content.changeVersionHint', { name: verEntry.name })}</div>
    <div class="ct-verlist">
      {#if verLoading}
        <div class="ct-ver-note"><i class="ti ti-loader spin"></i> {t('content.loadingVersions')}</div>
      {:else if verErr}
        <div class="ct-ver-note" style="color:var(--red)">{verErr}</div>
      {:else if verVersions.length === 0}
        <div class="ct-ver-note">{t('content.noVersions')}</div>
      {:else}
        {#each verVersions as v (v.id)}
          <button
            class="ct-ver-row"
            class:cur={v.isCurrent}
            disabled={v.isCurrent || verInstalling}
            onclick={() => installVersion(v)}
          >
            <i class="ti {v.isCurrent ? 'ti-check' : 'ti-download'}"></i>
            <span class="ct-ver-name">{v.versionNumber}</span>
            <span class="ct-ver-type">{v.versionType ?? v.gameVersion ?? ''}</span>
            <span class="ct-ver-date">{v.datePublished ? new Date(v.datePublished).toLocaleDateString() : ''}</span>
          </button>
        {/each}
      {/if}
    </div>
    {#snippet footer()}
      <button class="btn btn-ghost" onclick={() => { verOpen = false; verEntry = null }}>{t('close')}</button>
    {/snippet}
  </UiModal>
{/if}

<!-- ══════════════════════════════════════════════════════════════════════
     Модалка «Збірка запущена — зміни заборонені»
     Показується ПЕРЕД спробою змінити контент, поки гра йде (замість
     того щоб чекати OS-помилку "used by another process" від бекенду).
     ══════════════════════════════════════════════════════════════════════ -->
{#if runningLocked}
  <UiModal
    open={true}
    size="sm"
    title={t('content.lockedWhileRunning')}
    subtitle={t('content.lockedWhileRunningHint')}
    icon="ti-lock"
    color="var(--orange)"
    onclose={() => runningLocked = false}
  >
    <p class="ct-modal-hint">{t('content.lockedWhileRunningBody')}</p>
    {#snippet footer()}
      <button class="btn btn-primary" onclick={() => runningLocked = false}>{t('content.lockedOk')}</button>
    {/snippet}
  </UiModal>
{/if}

<style>
  .content-section { display: flex; flex-direction: column; gap: 12px; }

  /* ── Під-вкладки ── */
  .ct-subtabs {
    display: flex; gap: 6px; flex-wrap: wrap;
    padding: 4px; border-radius: 10px;
    background: var(--bg-input);
  }
  .ct-subtab {
    display: inline-flex; align-items: center; gap: 6px;
    padding: 6px 14px; border-radius: 8px;
    border: 1px solid transparent;
    background: transparent; color: var(--text-mute, #999);
    font-size: 12.5px; font-weight: 600; cursor: pointer;
    transition: all .15s ease;
  }
  .ct-subtab:hover { color: var(--text, #eee); background: rgba(255,255,255,.06); }
  .ct-subtab.active {
    color: #fff;
    background: color-mix(in srgb, var(--ct-c) 22%, transparent);
    border-color: color-mix(in srgb, var(--ct-c) 55%, transparent);
    box-shadow: 0 0 14px -4px var(--ct-c);
  }
  .ct-subtab.active i { color: var(--ct-c); }	  /* ── Тулбар ──
	     Той самий вигляд і поведінка, що .mods-toolbar у менеджері модів:
	     два чіткі рядки, sticky ПІД закріпленим тулбаром сторінки збірки
	     (той самий відступ stickyTop, що й у модів — без «виступу» між
	     закріпленими смугами), зградієнтний blur-фон і заокруглення знизу.
	     Рядок 1 — пошук + фільтр; рядок 2 — кнопки дій (перевірити
	     оновлення, тека, додати з браузера, додати файл, «Оновити всі»). */
	  .ct-toolbar {
	    display: flex; flex-direction: column; gap: 8px;
	    position: sticky; top: var(--mt-top, 0px); z-index: 18;
	    margin: -4px -4px 4px; padding: 8px 4px 10px;
	    background: linear-gradient(180deg,
	      color-mix(in srgb, var(--bg-app) 92%, transparent) 0%,
	      color-mix(in srgb, var(--bg-app) 78%, transparent) 100%);
	    backdrop-filter: blur(12px); -webkit-backdrop-filter: blur(12px);
	    border-radius: 0 0 var(--r-lg) var(--r-lg);
	  }
	  .ct-toolbar-main    { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; width: 100%; min-width: 0; }
	  .ct-toolbar-actions { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; width: 100%; justify-content: flex-end; }
	  .ct-toolbar-actions .btn { height: 36px; }
	  .ct-toolbar-actions .upd-badge {
	    display: inline-flex; align-items: center; justify-content: center;
	    min-width: 18px; height: 18px; padding: 0 5px; border-radius: 999px;
	    background: rgba(255,255,255,.22); font-size: 10.5px; font-weight: 800; margin-left: 2px;
	  }
	  .ct-search {
	    display: flex; align-items: center; gap: 8px;
	    background: var(--bg-input); border: 1px solid var(--border); border-radius: var(--r-sm);
	    padding: 0 11px; height: 32px; flex: 1 1 220px; min-width: 150px;
	  }
	  .ct-search i { color: var(--text-dim, #777); font-size: 14px; }
	  .ct-search input { flex: 1; background: none; border: none; outline: none; color: var(--text, #eee); font-size: 12.5px; }
	  .ct-addbtn { white-space: nowrap; }

	  /* Компактні іконкові кнопки тулбара (як .icon-tb у модів) */
	  .icon-tb { width: 36px; height: 36px; padding: 0; justify-content: center; }

	  /* ── Статус-рядок (як .mods-status у менеджері модів) ── */
	  .ct-status { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; font-size: 11.5px; color: var(--text-mute); min-height: 16px; margin: 0 2px 6px; }
	  .cs-item { display: inline-flex; align-items: center; gap: 5px; }
	  .cs-item.acc { color: var(--orange); font-weight: 600; }
	  .cs-item.err { color: var(--red); }


  /* ── Дроп-зона ── */
  .ct-dropzone { position: relative; border-radius: var(--r-md); transition: background .15s; }
  .ct-dropzone.drag {
    background: color-mix(in srgb, var(--ct-c) 6%, transparent);
    outline: 2px dashed color-mix(in srgb, var(--ct-c) 50%, transparent);
    outline-offset: 4px;
  }

  /* ── Сканування ── */
  .ct-scanning {
    padding: 28px; text-align: center; color: var(--text-mute);
    font-size: 12px; display: flex; align-items: center; justify-content: center; gap: 8px;
  }

  /* ── Порожня тека — стиль як mods-empty-folder ── */
  .ct-empty-folder {
    display: flex; flex-direction: column; align-items: center; text-align: center;
    gap: 8px; padding: 44px 24px; margin: 4px 0;
    background: color-mix(in srgb, var(--bg-card) 55%, transparent);
    border: 1px dashed var(--border-hi); border-radius: var(--r-lg);
    animation: rise-in .3s ease both;
  }
  .ct-ef-ic {
    width: 62px; height: 62px; border-radius: 17px; margin-bottom: 4px;
    display: flex; align-items: center; justify-content: center; font-size: 27px;
    background: linear-gradient(135deg, color-mix(in srgb, var(--ct-c) 20%, transparent), color-mix(in srgb, var(--ct-c) 4%, transparent));
    border: 1px solid color-mix(in srgb, var(--ct-c) 35%, transparent);
    box-shadow: 0 10px 26px rgba(0,0,0,.3);
    color: var(--ct-c);
  }
  .ct-ef-title { font-size: 15px; font-weight: 700; color: var(--text); }
  .ct-ef-sub { font-size: 12.5px; color: var(--text-mute); max-width: 400px; line-height: 1.5; }
  .ct-ef-actions { display: flex; gap: 8px; margin-top: 12px; flex-wrap: wrap; justify-content: center; }
  .ct-ef-drop {
    display: inline-flex; align-items: center; gap: 6px; margin-top: 6px;
    font-size: 11.5px; color: var(--text-dim);
  }

  /* ── Список ── */
  .ct-list { display: flex; flex-direction: column; gap: 8px; }

  /* ── Рядок — точно як mod-row у менеджері модів ── */
  /* content-visibility: браузер пропускає рендер/анімації рядків за
     межами в'юпорту — на великих теках ресурспаків/шейдерів скрол
     лишається плавним (те саме, що mod-row у модах). */
  .ct-row {
    display: flex; align-items: center; gap: 14px;
    padding: 12px 16px; border-radius: var(--r-md);
    background: var(--bg-panel); border: 1px solid var(--border);
    transition: border-color .15s;
    position: relative; z-index: 0;
    content-visibility: auto; contain-intrinsic-size: auto 64px;
  }
  .ct-row:hover { border-color: var(--border-hi, rgba(255,255,255,.16)); }
  .ct-row.upd  { border-color: rgba(var(--orange-rgb, 255,160,0), .4); }
  .ct-row.off  { opacity: .55; }
  .ct-row.selected { border-color: rgba(255,138,0,.5); background: rgba(255,138,0,.05); }

  /* ── Чекбокс вибору — точно як mod-check ── */
  .ct-check {
    width: 18px; height: 18px; flex-shrink: 0; margin: 0; padding: 0;
    border-radius: 6px; border: 1.5px solid var(--border-hi);
    background: var(--bg-input);
    display: flex; align-items: center; justify-content: center;
    color: transparent; cursor: pointer; font-size: 13px; line-height: 1;
    transition: border-color .15s, background .15s, color .12s, transform .12s;
  }
  .ct-check:hover { border-color: var(--orange); transform: scale(1.08); }
  .ct-check.checked { background: var(--orange); border-color: var(--orange); color: #fff; }
  .ct-check.checked:hover { background: color-mix(in srgb, var(--orange) 85%, #fff); }

  /* ── Іконка ресурсу ── */
  .ct-ic {
    position: relative; width: 40px; height: 40px; min-width: 40px; border-radius: 10px;
    background: color-mix(in srgb, var(--ct-c) 14%, var(--bg-input));
    border: 1px solid color-mix(in srgb, var(--ct-c) 28%, var(--border));
    display: flex; align-items: center; justify-content: center;
    color: var(--ct-c); font-size: 18px; overflow: visible;
  }
  .ct-ic img { width: 100%; height: 100%; object-fit: cover; border-radius: 9px; }
  button.ct-ic {
    padding: 0; cursor: pointer; font-family: inherit;
    transition: border-color .15s, transform .1s;
  }
  button.ct-ic:hover { border-color: var(--ct-c); }
  button.ct-ic:active { transform: scale(.96); }

  /* Значок джерела (Modrinth/CurseForge) — як mod-src */
  .ct-src {
    position: absolute; right: -6px; bottom: -6px; width: 17px; height: 17px; border-radius: 50%;
    overflow: hidden; display: flex; align-items: center; justify-content: center;
    border: 2px solid var(--bg-panel); background: var(--bg-panel);
  }
  .ct-src img { width: 100%; height: 100%; object-fit: cover; display: block; }

  /* ── Інформаційна зона ── */
  .ct-info { flex: 1; min-width: 0; }
  .ct-name {
    font-size: 13px; font-weight: 700; color: var(--text, #eee);
    display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
  }
  .ct-name-btn {
    padding: 0; margin: 0; border: none; background: transparent; cursor: pointer;
    font: inherit; font-weight: 700; color: var(--text); text-align: left;
  }
  .ct-name-btn:hover { color: var(--violet-l, #c4b5fd); text-decoration: underline; }
  .ct-meta {
    font-size: 10.5px; color: var(--text-mute, #999); margin-top: 2px;
    font-family: var(--font-mono);
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }

  /* ── Бейджі — точно як у менеджері модів ── */
  .badge { font-size: 9px; font-weight: 700; padding: 2px 7px; border-radius: 5px; font-family: var(--font-mono); letter-spacing: .3px; }
  .badge.upd { background: rgba(16,185,129,.15); color: var(--green, #3ddc84); display: inline-flex; align-items: center; gap: 3px; }
  .badge.upd .ti-arrow-up { font-size: 9px; }
  .badge.off { background: var(--bg-input); color: var(--text-mute, #999); border: 1px solid var(--border); }

  /* ── Дії на рядку ── */
  .ct-acts { display: flex; align-items: center; gap: 2px; flex-shrink: 0; }
  .icon-btn {
    width: 30px; height: 30px; border-radius: 7px; background: transparent; border: none; cursor: pointer;
    display: flex; align-items: center; justify-content: center; color: var(--text-mute); font-size: 15px;
    transition: background .15s, color .15s;
  }	  .icon-btn:hover { background: rgba(255,255,255,.08); color: var(--text); }
	  .icon-btn.del:hover { background: rgba(239,68,68,.12); color: var(--red, #ff5c5c); }
	  /* Кнопка «оновити до останньої» на рядку з бейджем оновлення */
	  .icon-btn.upd { color: var(--green, #3ddc84); }
	  .icon-btn.upd:hover { background: rgba(16,185,129,.14); color: var(--green, #3ddc84); }
	  .icon-btn:disabled { opacity: .5; cursor: default; }

  /* ── Перемикач — точно як у менеджері модів (.toggle) ── */
  .ct-toggle {
    position: relative; width: 42px; height: 24px; min-width: 42px;
    background: var(--bg-input); border-radius: 14px; cursor: pointer;
    transition: background .2s; border: 1px solid var(--border);
    padding: 0; flex-shrink: 0;
  }
  .ct-toggle::after {
    content: ''; position: absolute; top: 2px; left: 3px; width: 18px; height: 18px;
    border-radius: 50%; background: var(--text-mute); transition: transform .22s, background .22s;
    box-shadow: 0 2px 4px rgba(0,0,0,.4);
  }
  .ct-toggle.on { background: rgba(255,138,0,.25); border-color: rgba(255,138,0,.4); }
  .ct-toggle.on::after { transform: translateX(18px); background: var(--orange); box-shadow: 0 2px 8px rgba(255,138,0,.5); }
  .ct-knob { display: none; } /* kept for markup compat */

  /* ── Панель масових дій — точно як bulk-bar у менеджері модів ── */
  .ct-bulk-bar {
    position: sticky; bottom: 8px; z-index: 15;
    display: flex; align-items: center; gap: 10px; flex-wrap: wrap;
    padding: 8px 12px; background: var(--bg-panel);
    border: 1px solid rgba(255,138,0,.4); border-radius: var(--r-md);
    box-shadow: 0 6px 20px rgba(0,0,0,.35);
  }
  .ct-bulk-icons { display: flex; align-items: center; flex-shrink: 0; }
  .ct-bulk-ic {
    width: 26px; height: 26px; border-radius: 7px; overflow: hidden;
    background: var(--bg-input); border: 2px solid var(--bg-panel);
    display: flex; align-items: center; justify-content: center;
    font-size: 12px; color: var(--text-dim);
    margin-left: -8px;
  }
  .ct-bulk-ic:first-child { margin-left: 0; }
  .ct-bulk-ic img { width: 100%; height: 100%; object-fit: cover; }
  .ct-bulk-count { font-size: 12.5px; font-weight: 700; color: var(--text); white-space: nowrap; }
  .ct-bulk-spacer { flex: 1; }
  .ct-bulk-del:hover { background: rgba(239,68,68,.12); color: var(--red); }
  .ct-bulk-clear {
    width: 24px; height: 24px; border-radius: 6px; background: transparent; border: none; cursor: pointer;
    display: flex; align-items: center; justify-content: center; color: var(--text-mute); flex-shrink: 0;
  }
  .ct-bulk-clear:hover { background: rgba(255,255,255,.08); color: var(--text); }

  /* ── Модалка копіювання ── */
  .ct-modal-hint {
    font-size: 12.5px; color: var(--text-mute); margin: 0 0 4px;
  }
  .ct-modal-compat {
    font-size: 11px; color: var(--text-dim); margin: 0 0 12px;
    padding: 6px 10px; background: rgba(255,255,255,.04); border-radius: 7px;
    border: 1px solid var(--border);
  }
  .ct-modal-search {
    display: flex; align-items: center; gap: 8px;
    background: var(--bg-input); border: 1px solid var(--border); border-radius: var(--r-sm);
    padding: 0 11px; height: 32px; margin-bottom: 10px;
  }
  .ct-modal-search i { color: var(--text-dim); font-size: 14px; }
  .ct-modal-search input { flex: 1; background: none; border: none; outline: none; color: var(--text); font-size: 12.5px; }
  .ct-modal-checking {
    padding: 20px; text-align: center; color: var(--text-mute); font-size: 12px;
    display: flex; align-items: center; justify-content: center; gap: 8px;
  }
  .ct-modal-list { display: flex; flex-direction: column; gap: 6px; max-height: 45vh; overflow-y: auto; }
  .ct-modal-empty { padding: 20px; text-align: center; color: var(--text-mute); font-size: 12px; }

  /* Картка збірки у модалці копіювання */
  .ct-pack-item {
    display: flex; align-items: center; gap: 12px;
    padding: 10px 14px; border-radius: var(--r-md);
    background: var(--bg-input); border: 1px solid var(--border);
    color: var(--text); cursor: pointer; text-align: left; width: 100%;
    transition: border-color .15s, background .15s;
  }
  .ct-pack-item:hover { border-color: var(--border-hi); background: rgba(255,255,255,.04); }
  .ct-pack-item.sel { border-color: var(--violet, #a78bfa); background: rgba(167,139,250,.08); }
  .ct-pack-ic {
    width: 36px; height: 36px; min-width: 36px; border-radius: 9px;
    display: flex; align-items: center; justify-content: center; font-size: 16px;
    background: color-mix(in srgb, var(--pcol) 16%, transparent);
    border: 1px solid color-mix(in srgb, var(--pcol) 28%, transparent);
    color: var(--pcol);
  }
  .ct-pack-info { flex: 1; min-width: 0; }
  .ct-pack-name { font-size: 13px; font-weight: 600; color: var(--text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .ct-pack-sub  { font-size: 10.5px; color: var(--text-mute); font-family: var(--font-mono); }
  .ct-pack-check { color: var(--violet, #a78bfa); opacity: 0; transition: opacity .12s; }
  .ct-pack-item.sel .ct-pack-check { opacity: 1; }

  /* Бейдж наявності у збірці */
  .ct-presence-badge {
    font-size: 9px; font-weight: 700; padding: 2px 7px; border-radius: 5px;
    font-family: var(--font-mono); letter-spacing: .3px; white-space: nowrap; flex-shrink: 0;
  }
  .ct-presence-badge.already { background: rgba(16,185,129,.15); color: var(--green, #3ddc84); }
  .ct-presence-badge.can     { background: rgba(167,139,250,.12); color: var(--violet, #a78bfa); }

  /* ── Модалка «Змінити версію» ── */
  .ct-verlist { display: flex; flex-direction: column; gap: 4px; max-height: 45vh; overflow: auto; }
  .ct-ver-note {
    padding: 18px; text-align: center; color: var(--text-mute); font-size: 12px;
    display: flex; align-items: center; justify-content: center; gap: 6px;
  }
  .ct-ver-row {
    display: flex; align-items: center; gap: 8px; width: 100%;
    padding: 8px 12px; border-radius: 8px; text-align: left;
    background: var(--bg-input); border: 1px solid var(--border);
    color: var(--text); font-size: 12.5px; cursor: pointer;
    transition: all .15s ease;
  }
  .ct-ver-row:hover:not(:disabled) { border-color: var(--violet, #a78bfa); }
  .ct-ver-row:disabled { opacity: .7; cursor: default; }
  .ct-ver-row.cur { border-color: var(--green, #3ddc84); }
  .ct-ver-row.cur i { color: var(--green, #3ddc84); }
  .ct-ver-row i { color: var(--violet, #a78bfa); }
  .ct-ver-name { flex: 1; font-weight: 600; }
  .ct-ver-type { font-size: 11px; color: var(--text-mute); }
  .ct-ver-date { font-size: 11px; color: var(--text-mute); }
</style>
