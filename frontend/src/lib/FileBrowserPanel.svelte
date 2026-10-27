<script lang="ts">
  import Portal from './Portal.svelte'
  // ── Провідник файлів: узагальнений компонент ───────────────────────────
  // Параметризується через пропси rootId (packId/serverId) та api-об'єкт
  // з методами: list, readText, writeText, rename, createFolder, createFile,
  // delete, reveal, getAsset. Використовується і для збірок, і для серверів.
  import { t } from './i18n.svelte'
  import { App } from './wails'
  import { toast, confirmDialog } from './toast.svelte'
  import UiModal from './UiModal.svelte'
  import SelectField from './SelectField.svelte'
  import { highlightCode, highlightCodeLines, langLabel, validateSyntax } from './highlight'
  import type { PackDirListing, PackFileEntry, PackFileContent } from './types'

  // ── FileBrowserAPI — набір методів для доступу до файлів ────────────────
  export type FileBrowserAPI = {
    list: (rootId: string, relPath: string) => Promise<PackDirListing>
    readText: (rootId: string, relPath: string) => Promise<PackFileContent>
    writeText: (rootId: string, relPath: string, content: string) => Promise<void>
    rename: (rootId: string, relPath: string, newName: string) => Promise<void>
    createFolder: (rootId: string, relPath: string, name: string) => Promise<void>
    createFile: (rootId: string, relPath: string, name: string) => Promise<void>
    delete: (rootId: string, relPath: string) => Promise<void>
    reveal: (rootId: string, relPath: string) => Promise<void>
    getAsset: (rootId: string, relPath: string) => Promise<string>
  }

  let {
    rootId,
    rootName = '',
    api,
    serverRunning = false,
    // Відступ sticky-панелі інструментів під закріпленим тулбаром сторінки
    // збірки (як у менеджері контенту). 0 — коли панель у контексті без
    // зовнішнього sticky-тулбара (консоль сервера тощо).
    stickyTop = 0,
  }: {
    rootId: string
    rootName?: string
    api: FileBrowserAPI
    serverRunning?: boolean
    stickyTop?: number
  } = $props()

  let curPath = $state('')
  let listing = $state<PackDirListing | null>(null)
  let loading = $state(false)
  let loadError = $state<string | null>(null)

  // ── Перегляд файлу ──
  type Viewer =
    | { kind: 'text'; entry: PackFileEntry; content: PackFileContent; editing: boolean; saving: boolean }
    | { kind: 'image'; entry: PackFileEntry; url: string }
    | { kind: 'binary'; entry: PackFileEntry }
  let viewer = $state<Viewer | null>(null)

  // ── Поле вводу назви (створення теки/файлу / перейменування) ──
  let prompt = $state<{ mode: 'create-folder' | 'create-file' | 'rename'; title: string; label: string; ok: string; target?: PackFileEntry } | null>(null)
  let promptValue = $state('')

  // ── Фільтр/сортування списку (як у менеджері модів) ─────────────────
  // «Все/Папки/Файли» + сортування за назвою/розміром/датою зміни.
  // Панель інструментів sticky: у великій теці фільтри й дії завжди під
  // рукою, а не їдуть разом зі списком за край екрана.
  let fileFilter = $state<'all' | 'dirs' | 'files'>('all')
  let fileSort = $state<'name' | 'size' | 'modified'>('name')
  // Меню «⋯» на рядку файлу — той самий патерн, що в менеджері модів
  // (row-menu): замість 5 окремих іконок поспіль (наочення/накладання на
  // вузьких вікнах) лишаємо 2 головні дії + решту під «⋯».
  let menuFor = $state('')
  function toggleRowMenu(path: string, e: MouseEvent) { e.stopPropagation(); menuFor = menuFor === path ? '' : path }
  function closeRowMenu() { menuFor = '' }
  const visibleEntries = $derived.by(() => {
    let list = (listing?.entries ?? []).slice()
    if (fileFilter === 'dirs') list = list.filter(e => e.isDir)
    else if (fileFilter === 'files') list = list.filter(e => !e.isDir)
    const dirFirst = (a: PackFileEntry, b: PackFileEntry) => Number(b.isDir) - Number(a.isDir)
    const byName = (a: PackFileEntry, b: PackFileEntry) => a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: 'base' })
    if (fileSort === 'size') {
      return list.sort((a, b) => dirFirst(a, b) || (a.size ?? 0) - (b.size ?? 0) || byName(a, b))
    }
    if (fileSort === 'modified') {
      const mt = (e: PackFileEntry) => Date.parse(String(e.modified ?? '')) || 0
      return list.sort((a, b) => dirFirst(a, b) || mt(b) - mt(a) || byName(a, b))
    }
    return list.sort((a, b) => dirFirst(a, b) || byName(a, b))
  })

  function fmtSize(n: number): string {
    if (n < 1024) return t('files.sizeBytes', { size: n })
    if (n < 1024 * 1024) return t('files.sizeKB', { size: (n / 1024).toFixed(1) })
    return t('files.sizeMB', { size: (n / 1024 / 1024).toFixed(1) })
  }

  function fmtModified(rfc: string): string {
    const d = new Date(rfc)
    if (isNaN(d.getTime())) return ''
    return d.toLocaleDateString(undefined, { day: '2-digit', month: '2-digit', year: 'numeric' })
  }

  async function load() {
    loading = true
    loadError = null
    try {
      listing = await api.list(rootId, curPath)
    } catch (e: any) {
      listing = null
      loadError = String(e)
    }
    loading = false
  }

  // Стартове завантаження + перечит після операцій, що змінюють дерево.
  $effect(() => { if (rootId) load() })

  async function reload() { await load() }

  function navigate(path: string) { curPath = path; load() }

  function goUp() {
    if (!curPath) return
    const i = curPath.lastIndexOf('/')
    navigate(i < 0 ? '' : curPath.slice(0, i))
  }

  const crumbs = $derived(
    curPath ? ['', ...curPath.split('/')] : ['']
  )

  function crumbPath(i: number): string {
    if (i === 0) return ''
    return crumbs.slice(1, i + 1).join('/')
  }

  function isImage(entry: PackFileEntry): boolean {
    return /\.(png|jpe?g|webp|gif|bmp|svg|ico)$/i.test(entry.name)
  }

  async function openEntry(entry: PackFileEntry) {
    if (entry.isDir) { navigate(entry.path); return }
    if (isImage(entry)) {
      try {
        const url = await api.getAsset(rootId, entry.path)
        viewer = { kind: 'image', entry, url }
      } catch (e: any) {
        toast(t('files.assetError'), 'error')
      }
      return
    }
    try {
      const content = await api.readText(rootId, entry.path)
      if (content.binary) {
        viewer = { kind: 'binary', entry }
      } else {
        viewer = { kind: 'text', entry, content, editing: false, saving: false }
      }
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  function closeViewer() { viewer = null }

  // Підсвічений HTML поточного текстового файлу (для режиму перегляду).
  const highlighted = $derived.by(() => {
    if (!viewer || viewer.kind !== 'text') return ''
    return highlightCode(viewer.entry.name, viewer.content.content)
  })

  const viewerLang = $derived(viewer && viewer.kind === 'text' ? langLabel(viewer.entry.name) : '')

  // Результат перевірки синтаксису (JSON/TOML): банер у в'ювері у режимах і
  // перегляду, і редагування — помилка видно одразу при друці.
  const syntaxResult = $derived(
    viewer && viewer.kind === 'text' ? validateSyntax(viewer.entry.name, viewer.content.content) : null
  )

  // ── Overlay-редактор ──
  const ceLines = $derived(
    viewer && viewer.kind === 'text' ? highlightCodeLines(viewer.entry.name, viewer.content.content) : []
  )
  const ceErrLine = $derived(
    viewer && viewer.kind === 'text' && syntaxResult && !syntaxResult.ok ? (syntaxResult.line ?? 0) : 0
  )
  let ceTaEl = $state<HTMLTextAreaElement | null>(null)
  let cePreEl = $state<HTMLDivElement | null>(null)

  function syncCeScroll() {
    if (ceTaEl && cePreEl) {
      cePreEl.scrollTop = ceTaEl.scrollTop
      cePreEl.scrollLeft = ceTaEl.scrollLeft
    }
  }

  function toggleEdit() {
    if (!viewer || viewer.kind !== 'text') return
    viewer = { ...viewer, editing: !viewer.editing }
  }

  function onTextEdit(value: string) {
    const v = viewer
    if (v && v.kind === 'text') {
      viewer = { ...v, content: { ...v.content, content: value } }
    }
  }

  function onTextKeydown(e: KeyboardEvent) {
    if ((e.ctrlKey || e.metaKey) && e.key === 's') { e.preventDefault(); saveText() }
    if (e.key === 'Tab') {
      e.preventDefault()
      const ta = ceTaEl
      const v = viewer
      if (!ta || !v || v.kind !== 'text') return
      const start = ta.selectionStart ?? v.content.content.length
      const end = ta.selectionEnd ?? start
      const indent = '  '
      const next = v.content.content.slice(0, start) + indent + v.content.content.slice(end)
      onTextEdit(next)
      requestAnimationFrame(() => {
        ta.selectionStart = ta.selectionEnd = start + indent.length
        syncCeScroll()
      })
    }
  }

  async function saveText() {
    if (!viewer || viewer.kind !== 'text') return
    viewer = { ...viewer, saving: true }
    try {
      await api.writeText(rootId, viewer.entry.path, viewer.content.content)
      viewer = { ...viewer, saving: false, editing: false }
      toast(t('files.saved'), 'success')
      load()
    } catch (e: any) {
      viewer = { ...viewer, saving: false }
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  // ── Створення теки ──
  function openCreateFolder() {
    promptValue = ''
    prompt = { mode: 'create-folder', title: t('files.createFolder'), label: t('files.newFolderName'), ok: t('files.createFolder') }
  }

  // ── Створення файлу ──
  function openCreateFile() {
    promptValue = ''
    prompt = { mode: 'create-file', title: t('files.createFile'), label: t('files.newFileName'), ok: t('files.createFile') }
  }

  // ── Перейменування ──
  function openRename(entry: PackFileEntry) {
    promptValue = entry.name
    prompt = { mode: 'rename', title: t('files.rename'), label: t('files.renameTo'), ok: t('files.save'), target: entry }
  }

  async function submitPrompt() {
    const p = prompt
    if (!p) return
    prompt = null
    const name = promptValue.trim()
    if (!name) return
    try {
      if (p.mode === 'create-folder') {
        await api.createFolder(rootId, curPath, name)
      } else if (p.mode === 'create-file') {
        await api.createFile(rootId, curPath, name)
        const path = curPath ? `${curPath}/${name}` : name
        const entry: PackFileEntry = { name, path, isDir: false, size: 0, modified: new Date().toISOString() }
        viewer = { kind: 'text', entry, content: { content: '', size: 0, binary: false }, editing: true, saving: false }
      } else if (p.target) {
        await api.rename(rootId, p.target.path, name)
      }
      reload()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  async function deleteEntry(entry: PackFileEntry) {
    const ok = await confirmDialog(
      t('files.deleteConfirm', { name: entry.name }),
      t('files.deleteConfirmHint'),
      { confirmLabel: t('files.delete'), cancelLabel: t('cancel'), danger: true }
    )
    if (!ok) return
    try {
      await api.delete(rootId, entry.path)
      reload()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  async function revealEntry(entry: PackFileEntry) {
    try {
      await api.reveal(rootId, entry.path)
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  async function copyPath(entry: PackFileEntry) {
    const text = entry.path
    try {
      await App.CopyToClipboard(text)
    } catch { /* буфер обміну недоступний */ }
    toast(t('files.copied'), 'success')
  }
</script>

<!-- ── Інлайн-перегляд файлу: ЗАМІСТЬ списку (окремий розділ) ── -->
<svelte:window onclick={closeRowMenu} />
{#if viewer}
  <div class="fp-viewer">
    <div class="fp-viewer-head">
      <button class="btn btn-ghost btn-sm" onclick={closeViewer} title={t('files.back')}>
        <i class="ti ti-arrow-left"></i> {t('files.back')}
      </button>
      <div class="fp-viewer-title">
        <i class="ti {viewer.kind === 'image' ? 'ti-photo' : viewer.kind === 'binary' ? 'ti-file-unknown' : 'ti-file-text'}" style="color:var(--orange)"></i>
        <span class="fp-viewer-name" title={viewer.entry.path}>{viewer.entry.name}</span>
        {#if viewerLang}<span class="fp-viewer-lang">{viewerLang}</span>{/if}
        <span class="fp-viewer-path">{viewer.entry.path}</span>
      </div>
      <div class="fp-viewer-actions">
        {#if viewer.kind === 'text'}
          <button class="btn btn-ghost btn-sm" onclick={toggleEdit} disabled={viewer.saving}>
            <i class="ti {viewer.editing ? 'ti-eye' : 'ti-pencil'}"></i>
            {viewer.editing ? t('files.preview') : t('files.edit')}
          </button>
          {#if viewer.editing}
            <button class="btn btn-primary btn-sm" onclick={saveText} disabled={viewer.saving}>
              <i class="ti {viewer.saving ? 'ti-loader spin' : 'ti-device-floppy'}"></i>
              {viewer.saving ? t('files.saving') : t('files.save')}
            </button>
          {/if}
        {/if}
      </div>
    </div>
    <div class="fp-viewer-body">
      {#if viewer.kind === 'text'}
        {#if syntaxResult && !syntaxResult.ok}
          <div class="fp-syntax-err">
            <i class="ti ti-alert-triangle"></i>
            <div>
              <div class="fse-title">{t('files.syntaxError')}</div>
              <div class="fse-msg">
                {syntaxResult.message}
                {#if syntaxResult.line}{t('files.syntaxLine', { line: syntaxResult.line, col: syntaxResult.col ?? 0 })}{/if}
              </div>
            </div>
          </div>
        {:else if syntaxResult && syntaxResult.checked}
          <div class="fp-syntax-ok"><i class="ti ti-circle-check"></i> {t('files.syntaxOk')}</div>
        {/if}
        {#if viewer.editing}
          <div class="ce-shell" class:ce-err={!syntaxResult?.ok}>
            <div class="ce-pre mono" bind:this={cePreEl} aria-hidden="true">
              {#each ceLines as lineHtml, i}
                <div class="ce-line" class:err={ceErrLine === i + 1}>{@html lineHtml}</div>
              {/each}
            </div>
            <textarea
              class="ce-ta mono"
              bind:this={ceTaEl}
              value={viewer.content.content}
              oninput={(e: any) => onTextEdit(e.target.value)}
              onscroll={syncCeScroll}
              onkeydown={onTextKeydown}
              readonly={viewer.saving}
              spellcheck="false"
              autofocus></textarea>
          </div>
        {:else}
          <pre class="hl-view">{@html highlighted}</pre>
        {/if}
      {:else if viewer.kind === 'image'}
        <div class="fp-img-wrap">
          <img class="fp-img" src={viewer.url} alt={viewer.entry.name} />
        </div>
      {:else}
        <div style="text-align:center;padding:24px 0;color:var(--text-mute);font-size:12.5px;line-height:1.8">
          <i class="ti ti-file-unknown" style="font-size:38px;display:block;margin-bottom:10px;color:var(--text-dim)"></i>
          {t('files.binaryView')}
        </div>
      {/if}
    </div>
  </div>
{:else}
<div class="files-panel">
  {#if rootName}
    <div class="fp-name"><i class="ti ti-files"></i> {rootName}</div>
  {/if}

  <!-- Тулбар: один рядок — навігація зліва, фільтр/сортування/дії справа. -->
  <div class="fp-sticky" style={`--mt-top:${stickyTop}px`}>
    <div class="toolbar">
      <div class="breadcrumb">
        <button class="crumb-btn" class:active={curPath === ''} onclick={() => navigate('')} title={t('files.navigateUp')}>
          <i class="ti ti-home"></i>
        </button>
        {#each crumbs as crumb, i (crumb)}
          {#if i > 0}<span class="crumb-sep">/</span>{/if}
          <button class="crumb-btn" class:active={i === crumbs.length - 1} onclick={() => navigate(crumbPath(i))}>
            {crumb || rootName || rootId}
          </button>
        {/each}
      </div>
      <div style="flex:1"></div>
      <SelectField
        value={fileFilter}
        options={[
          { value: 'all', label: t('files.filterAll') },
          { value: 'dirs', label: t('files.filterFolders') },
          { value: 'files', label: t('files.filterFiles') },
        ]}
        onchange={(v: string) => fileFilter = v as 'all' | 'dirs' | 'files'}
        minWidth="122px"
        ariaLabel={t('files.filterAll')}
      />
      <SelectField
        value={fileSort}
        options={[
          { value: 'name',     label: t('files.sortName') },
          { value: 'size',     label: t('files.sortSize') },
          { value: 'modified', label: t('files.sortModified') },
        ]}
        onchange={(v: string) => fileSort = v as 'name' | 'size' | 'modified'}
        minWidth="132px"
        ariaLabel={t('files.sortName')}
      />
      <span class="fp-count">{visibleEntries.length}</span>
      <button class="btn btn-ghost btn-sm icon-tb" onclick={goUp} disabled={!curPath} title={t('files.navigateUp')}><i class="ti ti-arrow-up"></i></button>
      <button class="btn btn-ghost btn-sm icon-tb" onclick={reload} title={t('files.refresh')}><i class="ti ti-refresh"></i></button>
      <button class="btn btn-ghost btn-sm icon-tb" onclick={openCreateFile} title={t('files.createFile')}><i class="ti ti-file-plus"></i></button>
      <button class="btn btn-primary btn-sm" onclick={openCreateFolder}><i class="ti ti-folder-plus"></i> {t('files.createFolder')}</button>
    </div>
  </div>

  {#if loading && !listing}
    <div class="card"><div style="padding:24px;text-align:center;color:var(--text-mute);font-size:12px"><i class="ti ti-loader spin"></i> {t('files.loading')}</div></div>
  {:else if loadError}
    <div class="card"><div style="padding:24px;text-align:center;color:var(--text-mute);font-size:12px;line-height:1.6">{t('files.notFound')}<br><span style="font-family:var(--font-mono);font-size:11px;opacity:.7">{loadError}</span></div></div>
  {:else if listing && listing.entries.length > 0 && visibleEntries.length === 0}
    <div class="card"><div style="padding:24px;text-align:center;color:var(--text-mute);font-size:12px"><i class="ti ti-search-off"></i> {t('files.noMatch')}</div></div>
  {:else if listing && listing.entries.length === 0}
    <div class="card"><div style="padding:24px;text-align:center;color:var(--text-mute);font-size:12px">{t('files.empty')}</div></div>
  {:else if listing}
    <div class="fp-list-scroll">
      <div class="fp-list">
        {#each visibleEntries as entry (entry.path)}
          <div class="fp-row" onclick={() => openEntry(entry)}>
            <div class="fp-ic">
              {#if entry.isDir}
                <i class="ti ti-folder"></i>
              {:else if isImage(entry)}
                <i class="ti ti-photo"></i>
              {:else}
                <i class="ti ti-file"></i>
              {/if}
            </div>
            <div class="fp-name-cell" title={entry.name}>
              <div class="fp-ename">{entry.name}</div>
              <div class="fp-emeta">{entry.isDir ? t('files.open') : fmtSize(entry.size)} · {fmtModified(entry.modified)}</div>
            </div>
            <div class="fp-actions" onclick={(e: any) => e.stopPropagation()}>
              {#if !entry.isDir}
                <button class="icon-btn" title={t('files.open')} onclick={() => openEntry(entry)}><i class="ti ti-eye"></i></button>
              {/if}
              <div class="row-menu-wrap">
                <button class="icon-btn" onclick={(e: MouseEvent) => toggleRowMenu(entry.path, e)} title={t('files.moreActions')} aria-label={t('files.moreActions')} aria-expanded={menuFor === entry.path}>
                  <i class="ti ti-dots-vertical"></i>
                </button>
                {#if menuFor === entry.path}
                  <div class="row-menu" onclick={(e: MouseEvent) => e.stopPropagation()}>
                    <button onclick={() => { closeRowMenu(); revealEntry(entry) }}><i class="ti ti-folder-open"></i> {t('files.reveal')}</button>
                    <button onclick={() => { closeRowMenu(); copyPath(entry) }}><i class="ti ti-link"></i> {t('files.copyPath')}</button>
                    <button onclick={() => { closeRowMenu(); openRename(entry) }}><i class="ti ti-pencil"></i> {t('files.rename')}</button>
                    <div class="row-menu-sep"></div>
                    <button class="danger" onclick={() => { closeRowMenu(); deleteEntry(entry) }}><i class="ti ti-trash"></i> {t('files.delete')}</button>
                  </div>
                {/if}
              </div>
            </div>
          </div>
        {/each}
      </div>
    </div>
  {/if}
</div>
{/if}

<!-- ── Поле вводу назви ── -->
{#if prompt}
  {@const pr = prompt}
  <UiModal
    open={true}
    size="sm"
    title={pr.title}
    icon="ti-pencil"
    color="var(--orange)"
    onclose={() => prompt = null}
  >
      <div class="rl-name" style="margin-bottom:8px">{pr.label}</div>
      <input class="input mono" value={promptValue} oninput={(e: any) => promptValue = e.target.value}
             onkeydown={(e: any) => { if (e.key === 'Enter') submitPrompt() }}
             autofocus />
    {#snippet footer()}
      <button class="btn btn-ghost" onclick={() => prompt = null}>{t('cancel')}</button>
      <button class="btn btn-primary" onclick={submitPrompt} disabled={!promptValue.trim()}>{pr.ok}</button>
    {/snippet}
  </UiModal>
{/if}

<style>
  /* height:100%/min-height:0 — щоб панель могла зайняти обмежену висоту
     предка (сторінка сервера: .sv-panel{overflow:hidden}) і віддати скрол
     ВНУТРІШНЬОМУ .fp-list-scroll, а не обрізатись/розтягувати предка.
     На сторінці збірки (де скролиться сама .content, предок без
     обмеженої висоти) height:100% просто резолвиться в природну висоту
     вмісту — поведінка там не змінюється. */
  .files-panel { display:flex; flex-direction:column; height:100%; min-height:0; }
  .fp-name { display:flex; align-items:center; gap:8px; font-size:14px; font-weight:800; margin-bottom:12px; }
  /* Sticky-панель інструментів (два ряди), як .ct-toolbar менеджера модів. */
  .fp-sticky {
    display: flex; flex-direction: column; gap: 8px;
    position: sticky; top: var(--mt-top, 0px); z-index: 18;
    margin: -4px -4px 12px; padding: 6px 4px 8px;
    background: linear-gradient(180deg,
      color-mix(in srgb, var(--bg-app) 94%, transparent) 0%,
      color-mix(in srgb, var(--bg-app) 80%, transparent) 100%);
    backdrop-filter: blur(12px); -webkit-backdrop-filter: blur(12px);
    border-radius: 0 0 var(--r-lg, 14px) var(--r-lg, 14px);
  }
  .icon-tb { width: 32px; height: 32px; padding: 0; justify-content: center; flex-shrink: 0; }
  .fp-count {
    font: 800 11px/1 var(--font-mono); color: var(--text-dim);
    background: var(--bg-input); border: 1px solid var(--border);
    border-radius: 999px; padding: 4px 9px; flex-shrink: 0;
  }
  /* ── «⋯»-меню на рядку файлу — той самий патерн, що в менеджері модів ── */
  .row-menu-wrap { position: relative; }
  .row-menu {
    position: absolute; top: calc(100% + 4px); right: 0; z-index: 2147483000;
    display: flex; flex-direction: column; min-width: 190px;
    background: var(--bg-panel); border: 1px solid var(--border-hi); border-radius: var(--r-md, 10px);
    box-shadow: 0 8px 24px rgba(0, 0, 0, .4); padding: 4px; gap: 1px;
  }
  .row-menu button {
    display: flex; align-items: center; gap: 8px; width: 100%;
    padding: 8px 10px; background: transparent; border: none; border-radius: var(--r-sm, 6px);
    color: var(--text); font-size: 12.5px; text-align: left; cursor: pointer;
  }
  .row-menu button:hover { background: var(--hover); }
  .row-menu button.danger { color: var(--red); }
  .row-menu button.danger:hover { background: rgba(239, 68, 68, .12); }
  .row-menu-sep { height: 1px; background: var(--border); margin: 3px 2px; }
  .fp-name i { color:var(--orange); }
  .breadcrumb { display:flex; align-items:center; gap:4px; flex-wrap:wrap; min-height:32px; }
  .crumb-btn {
    background:transparent; border:none; cursor:pointer; color:var(--text-mute);
    font-size:12px; font-weight:600; padding:4px 7px; border-radius:7px; transition:background .15s,color .15s;
    max-width:180px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; font-family:inherit;
  }
  .crumb-btn:hover { background:var(--hover); color:var(--text); }
  .crumb-btn.active { color:var(--text); }
  .crumb-sep { color:var(--text-dim); font-size:12px; }

  /* fp-list-scroll — власний скрол-контейнер списку. flex:1/min-height:0
     дають йому "взяти" решту висоти .files-panel лише коли та обмежена
     (сторінка сервера); інакше (сторінка збірки, .files-panel без явної
     висоти) просто не активується, і скролиться сторінка, як раніше. */
  .fp-list-scroll { flex:1; min-height:0; overflow-y:auto; }
  .fp-list { display:flex; flex-direction:column; gap:4px; }
  .fp-row {
    display:flex; align-items:center; gap:12px; padding:8px 10px; border-radius:var(--r-md);
    background:var(--bg-input); border:1px solid var(--border); cursor:pointer;
    transition:border-color .15s, background .15s;
  }
  .fp-row:hover { border-color:var(--border-hi); background:var(--hover); }
  .fp-ic { width:32px; height:32px; border-radius:9px; display:flex; align-items:center; justify-content:center; background:var(--hover); color:var(--text-mute); font-size:17px; flex-shrink:0; }
  .fp-row:hover .fp-ic { color:var(--orange); }
  .fp-name-cell { flex:1; min-width:0; }
  .fp-ename { font-size:13px; font-weight:600; color:var(--text); overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
  .fp-emeta { font-size:11px; color:var(--text-mute); margin-top:2px; font-family:var(--font-mono); }
  .fp-actions { display:flex; gap:6px; opacity:.7; transition:opacity .15s; flex-shrink:0; }
  .fp-row:hover .fp-actions { opacity:1; }

  /* ── Overlay-редактор з підсвіткою синтаксису ── */
  .ce-shell {
    position: relative;
    border: 1px solid var(--border);
    border-radius: var(--r-md);
    background: var(--bg-input);
    overflow: hidden;
  }
  .ce-shell:focus-within { border-color: color-mix(in srgb, var(--orange) 45%, var(--border)); }
  .ce-pre, .ce-ta {
    margin: 0; padding: 12px 14px;
    font-family: var(--font-mono); font-size: 12px; line-height: 1.55;
    tab-size: 4; white-space: pre; word-wrap: normal; overflow-wrap: normal;
    letter-spacing: 0;
  }
  .ce-pre {
    position: absolute; inset: 0;
    color: var(--text);
    overflow: hidden;
    pointer-events: none;
  }
  .ce-line { white-space: pre; min-height: 1.55em; }
  .ce-line.err {
    background: rgba(239, 68, 68, .16);
    box-shadow: inset 3px 0 0 var(--red);
    border-radius: 3px;
  }
  .ce-ta {
    position: relative; display: block;
    width: 100%; min-height: 300px; max-height: 62vh;
    resize: vertical;
    background: transparent;
    color: transparent;
    caret-color: var(--text);
    -webkit-text-fill-color: transparent;
    border: none; outline: none;
    overflow: auto;
  }
  .ce-ta::selection { background: rgba(var(--orange-rgb), .25); }
  .ce-err .ce-ta { caret-color: var(--red); }
  .ce-err .ce-ta::selection { background: rgba(239, 68, 68, .25); }
  .fp-img-wrap { display:flex; justify-content:center; align-items:center; min-height:200px; }
  .fp-img { max-width:100%; max-height:60vh; border-radius:var(--r-md); object-fit:contain; }

  .fp-viewer {
    border:1px solid var(--border); border-radius:var(--r-md); overflow:hidden;
    background:var(--bg-input); margin-bottom:12px;
  }
  .fp-viewer-head {
    display:flex; align-items:center; gap:10px; padding:10px 14px;
    background:var(--hover); border-bottom:1px solid var(--border);
  }
  .fp-viewer-title { display:flex; align-items:center; gap:8px; min-width:0; flex:1; }
  .fp-viewer-name { font-size:13px; font-weight:700; color:var(--text); overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
  .fp-viewer-lang {
    font-size:10px; font-weight:700; letter-spacing:.06em; padding:2px 7px; border-radius:6px;
    background:var(--orange); color:#fff; flex-shrink:0; font-family:var(--font-mono);
  }
  .fp-viewer-path { font-size:11px; color:var(--text-mute); overflow:hidden; text-overflow:ellipsis; white-space:nowrap; font-family:var(--font-mono); }
  .fp-viewer-actions { display:flex; align-items:center; gap:6px; flex-shrink:0; }
  .fp-viewer-body { padding:12px 14px; }

  .fp-syntax-err {
    display:flex; align-items:flex-start; gap:10px; padding:10px 12px; margin-bottom:12px;
    background:rgba(239,68,68,.1); border:1px solid rgba(239,68,68,.4); border-radius:var(--r-md);
    color:var(--red); font-size:12px;
  }
  .fp-syntax-err > i { font-size:16px; margin-top:1px; }
  .fse-title { font-weight:700; margin-bottom:3px; }
  .fse-msg { font-family:var(--font-mono); font-size:11px; opacity:.9; word-break:break-word; }
  .fp-syntax-ok {
    display:flex; align-items:center; gap:8px; padding:8px 12px; margin-bottom:12px;
    background:rgba(16,185,129,.1); border:1px solid rgba(16,185,129,.35); border-radius:var(--r-md);
    color:var(--green); font-size:12px; font-weight:600;
  }

  /* ── Підсвітка синтаксису: контейнер прев'ю. Кольори токенів (.hl-k,
     .hl-s, ...) — глобальні в style.css, бо {@html}-вміст не отримує
     Svelte-скоуп-атрибут (див. коментар там). ── */
  .hl-view {
    margin:0; padding:12px 14px; font-family:var(--font-mono); font-size:12px; line-height:1.55;
    background:var(--bg-input); border:1px solid var(--border); border-radius:var(--r-md);
    overflow:auto; max-height:62vh; white-space:pre; tab-size:4; color:var(--text);
  }

  .icon-btn {
    width:30px; height:30px; border-radius:9px; border:none; background:var(--hover);
    color:var(--text-mute); cursor:pointer; display:flex; align-items:center; justify-content:center;
    transition:background .15s, color .15s; flex-shrink:0; font-size:14px;
  }
  .icon-btn:hover { background:var(--hover-strong); color:var(--text); }
  .icon-btn.del:hover { background:rgba(239,68,68,.15); color:var(--red); }
</style>
