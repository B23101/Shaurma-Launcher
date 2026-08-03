<script lang="ts">
  // ── Провідник файлів кастомної збірки (вкладка «Файли») ──────────────
  // Перегляд теки .minecraft збірки: навігація по теках, breadcrumb,
  // інлайн-перегляд текстових файлів (з підсвіткою синтаксису) та
  // зображень, створення тек, перейменування, видалення, «Показати у
  // Провіднику» і копіювання шляху. Усі операції йдуть через бекенд з
  // патою відносною .minecraft (бекенд сам не дає вийти за межі теки).
  import { t } from './i18n.svelte'
  import { App } from './wails'
  import { toast, confirmDialog } from './toast.svelte'
  import { highlightCode, langLabel, validateSyntax } from './highlight'
  import type { PackDirListing, PackFileEntry, PackFileContent } from './types'

  let {
    packId,
    packName = ''
  } = $props<{ packId: string; packName?: string }>()

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

  // ── Поле вводу назви (створення теки / перейменування) ──
  let prompt = $state<{ mode: 'create-folder' | 'rename'; title: string; label: string; ok: string; target?: PackFileEntry } | null>(null)
  let promptValue = $state('')

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
      listing = await App.ListPackFiles(packId, curPath)
    } catch (e: any) {
      listing = null
      loadError = String(e)
    }
    loading = false
  }

  // Стартове завантаження + перечит після операцій, що змінюють дерево.
  $effect(() => { if (packId) load() })

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
        const url = await App.GetPackFileAsset(packId, entry.path)
        viewer = { kind: 'image', entry, url }
      } catch (e: any) {
        toast(t('files.assetError'), 'error')
      }
      return
    }
    try {
      const content = await App.ReadPackTextFile(packId, entry.path)
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

  // Результат перевірки синтаксису (JSON): банер у в'ювері у режимах і
  // перегляду, і редагування — помилка видно одразу при друці.
  const syntaxResult = $derived(
    viewer && viewer.kind === 'text' ? validateSyntax(viewer.entry.name, viewer.content.content) : null
  )

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
  }

  async function saveText() {
    if (!viewer || viewer.kind !== 'text') return
    viewer = { ...viewer, saving: true }
    try {
      await App.WritePackTextFile(packId, viewer.entry.path, viewer.content.content)
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
        await App.CreatePackFolder(packId, curPath, name)
      } else if (p.target) {
        await App.RenamePackEntry(packId, p.target.path, name)
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
      await App.DeletePackEntry(packId, entry.path)
      reload()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  async function revealEntry(entry: PackFileEntry) {
    try {
      await App.RevealPackFile(packId, entry.path)
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  async function copyPath(entry: PackFileEntry) {
    const text = entry.path
    try {
      await navigator.clipboard.writeText(text)
    } catch {
      // Fallback для WebView2 без дозволу на clipboard API.
      const ta = document.createElement('textarea')
      ta.value = text
      document.body.appendChild(ta)
      ta.select()
      try { document.execCommand('copy') } catch { /* ignore */ }
      ta.remove()
    }
    toast(t('files.copied'), 'success')
  }
</script>

<!-- ── Інлайн-перегляд файлу: ЗАМІСТЬ списку (окремий розділ) ── -->
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
          <textarea class="input mono editor-ta" rows="22"
                    value={viewer.content.content}
                    oninput={(e: any) => onTextEdit(e.target.value)}
                    onkeydown={onTextKeydown}
                    readonly={viewer.saving}></textarea>
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
  {#if packName}
    <div class="fp-name"><i class="ti ti-files"></i> {packName}</div>
  {/if}

  <!-- Toolbar -->
  <div class="toolbar" style="margin-bottom:12px">
    <div class="breadcrumb">
      <button class="crumb-btn" class:active={curPath === ''} onclick={() => navigate('')} title={t('files.navigateUp')}>
        <i class="ti ti-home"></i>
      </button>
      {#each crumbs as crumb, i (crumb)}
        {#if i > 0}<span class="crumb-sep">/</span>{/if}
        <button class="crumb-btn" class:active={i === crumbs.length - 1} onclick={() => navigate(crumbPath(i))}>
          {crumb || packName || packId}
        </button>
      {/each}
    </div>
    <div style="flex:1"></div>
    <button class="btn btn-ghost btn-sm" onclick={goUp} disabled={!curPath} title={t('files.navigateUp')}><i class="ti ti-arrow-up"></i></button>
    <button class="btn btn-ghost btn-sm" onclick={reload} title={t('files.refresh')}><i class="ti ti-refresh"></i></button>
    <button class="btn btn-primary btn-sm" onclick={openCreateFolder}><i class="ti ti-folder-plus"></i> {t('files.createFolder')}</button>
  </div>

  {#if loading && !listing}
    <div class="card"><div style="padding:24px;text-align:center;color:var(--text-mute);font-size:12px"><i class="ti ti-loader spin"></i> {t('files.loading')}</div></div>
  {:else if loadError}
    <div class="card"><div style="padding:24px;text-align:center;color:var(--text-mute);font-size:12px;line-height:1.6">{t('files.notFound')}<br><span style="font-family:var(--font-mono);font-size:11px;opacity:.7">{loadError}</span></div></div>
  {:else if listing && listing.entries.length === 0}
    <div class="card"><div style="padding:24px;text-align:center;color:var(--text-mute);font-size:12px">{t('files.empty')}</div></div>
  {:else if listing}
    <div class="fp-list">
      {#each listing.entries as entry (entry.path)}
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
            <button class="icon-btn" title={t('files.reveal')} onclick={() => revealEntry(entry)}><i class="ti ti-folder-open"></i></button>
            <button class="icon-btn" title={t('files.copyPath')} onclick={() => copyPath(entry)}><i class="ti ti-link"></i></button>
            <button class="icon-btn" title={t('files.rename')} onclick={() => openRename(entry)}><i class="ti ti-pencil"></i></button>
            <button class="icon-btn del" title={t('files.delete')} onclick={() => deleteEntry(entry)}><i class="ti ti-trash"></i></button>
          </div>
        </div>
      {/each}
    </div>
  {/if}
</div>
{/if}

<!-- ── Поле вводу назви ── -->
{#if prompt}
  <div class="modal-overlay" onclick={() => prompt = null}>
    <div class="modal" onclick={(e: any) => e.stopPropagation()}>
      <div class="modal-head"><h3>{prompt.title}</h3></div>
      <div class="modal-body">
        <div class="rl-name" style="margin-bottom:8px">{prompt.label}</div>
        <input class="input mono" value={promptValue} oninput={(e: any) => promptValue = e.target.value}
               onkeydown={(e: any) => { if (e.key === 'Enter') submitPrompt() }}
               autofocus />
      </div>
      <div class="modal-foot">
        <button class="btn btn-ghost" onclick={() => prompt = null}>{t('cancel')}</button>
        <button class="btn btn-primary" onclick={submitPrompt} disabled={!promptValue.trim()}>{prompt.ok}</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .files-panel { display:flex; flex-direction:column; }
  .fp-name { display:flex; align-items:center; gap:8px; font-size:14px; font-weight:800; margin-bottom:12px; }
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
  .fp-actions { display:flex; gap:6px; opacity:.35; transition:opacity .15s; flex-shrink:0; }
  .fp-row:hover .fp-actions { opacity:1; }

  .editor-ta { width:100%; resize:vertical; font-size:12px; line-height:1.5; tab-size:4; }
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

  /* ── Підсвітка синтаксису ── */
  .hl-view {
    margin:0; padding:12px 14px; font-family:var(--font-mono); font-size:12px; line-height:1.55;
    background:var(--bg-input); border:1px solid var(--border); border-radius:var(--r-md);
    overflow:auto; max-height:62vh; white-space:pre; tab-size:4; color:var(--text);
  }
  .hl-k { color:#c792ea; }          /* keyword / тег */
  .hl-s { color:#9ece6a; }          /* рядок */
  .hl-n { color:#ff9e64; }          /* число */
  .hl-p { color:#7aa2f7; }          /* властивість/ключ */
  .hl-c { color:#8b93a7; font-style:italic; }  /* коментар */
  .hl-a { color:#e0af68; }          /* атрибут/аннотація */
  .hl-f { color:#ffb8b8; font-style:italic; }  /* функція */
  .hl-b { color:#ff9e64; font-weight:600; }    /* булеве/логи */
  .hl-t { color:#c792ea; }

  @media (prefers-color-scheme: light) {
    .hl-k, .hl-t { color:#8b3fbf; }
    .hl-s { color:#3d7a1f; }
    .hl-n, .hl-b { color:#b45309; }
    .hl-p { color:#1d4ed8; }
    .hl-c { color:#7a7f8f; }
    .hl-a { color:#b4660f; }
    .hl-f { color:#b91c1c; }
  }

  .icon-btn {
    width:30px; height:30px; border-radius:9px; border:none; background:var(--hover);
    color:var(--text-mute); cursor:pointer; display:flex; align-items:center; justify-content:center;
    transition:background .15s, color .15s; flex-shrink:0; font-size:14px;
  }
  .icon-btn:hover { background:var(--hover-strong); color:var(--text); }
  .icon-btn.del:hover { background:rgba(239,68,68,.15); color:var(--red); }
</style>
