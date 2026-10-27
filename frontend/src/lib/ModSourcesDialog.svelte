<script lang="ts">
  // «Єдине джерело» модів: діалог керування джерелами збірки або сервера.
  // Джерело — окремий .jar або ціла тека модів; лаунчер сам звіряє хеші й
  // копіює змінене в mods перед запуском гри/сервера та пакуванням.
  import { onMount, onDestroy } from 'svelte'
  import UiModal from './UiModal.svelte'
  import { t } from './i18n.svelte'
  import { App, on } from './wails'
  import { toast, confirmDialog } from './toast.svelte'
  import type { ModSourcesView, ModSourcesProgress } from './types'

  let {
    packId = '',
    serverId = '',
    kind = 'mods',
    onclose,
    onchanged,
  }: {
    packId?: string
    serverId?: string
    // Для серверів: яку теку обслуговуємо — mods або plugins.
    kind?: 'mods' | 'plugins'
    onclose: () => void
    // Викликається після зміни джерел — батько перезавантажує список.
    onchanged?: () => void
  } = $props()

  const isServer = $derived(!!serverId)

  let view = $state<ModSourcesView>({ sources: [], locked: false })
  let loading = $state(true)
  let busy = $state(false)
  let progress = $state<{ percent: number; file: string; action: string; done: number; total: number } | null>(null)
  let offs: Array<() => void> = []

  async function load() {
    loading = true
    try {
      view = (isServer
        ? await App.GetServerModSources(serverId, kind)
        : await App.GetModSources(packId)) ?? { sources: [], locked: false }
    } catch (e: any) { toast(String(e), 'error') }
    finally { loading = false }
  }

  async function add(k: 'file' | 'folder') {
    let path = ''
    try { path = k === 'file' ? await App.PickModSourceFile() : await App.PickModSourceFolder() }
    catch (e: any) { toast(String(e), 'error'); return }
    if (!path) return
    busy = true
    progress = { percent: 0, file: '', action: 'check', done: 0, total: 0 }
    try {
      view = isServer
        ? await App.AddServerModSource(serverId, kind, k, path)
        : await App.AddModSource(packId, k, path)
      toast(t('modsrc.added'), 'success')
      onchanged?.()
    } catch (e: any) { toast(String(e), 'error') }
    finally { busy = false; progress = null }
  }

  async function remove(id: string) {
    const ok = await confirmDialog(t('modsrc.detachTitle'), t('modsrc.detachBody'), { danger: false })
    if (!ok) return
    busy = true
    try {
      view = isServer
        ? await App.RemoveServerModSource(serverId, kind, id)
        : await App.RemoveModSource(packId, id)
      onchanged?.()
    } catch (e: any) { toast(String(e), 'error') }
    finally { busy = false }
  }

  async function syncNow() {
    busy = true
    progress = { percent: 0, file: '', action: 'check', done: 0, total: 0 }
    try {
      const r = await App.SyncModSources(packId)
      const changed = r.copied + r.restored + r.removed + r.moved
      toast(changed > 0 ? t('modsrc.syncedN', { n: changed }) : t('modsrc.upToDate'), 'success')
      if (r.errors?.length) toast(r.errors[0], 'error')
      onchanged?.()
      await load()
    } catch (e: any) { toast(String(e), 'error') }
    finally { busy = false; progress = null }
  }

  function actionText(a: string): string {
    if (a === 'copy') return t('modsrc.copying')
    if (a === 'remove') return t('modsrc.removing')
    return t('modsrc.checking')
  }

  onMount(() => {
    load()
    if (isServer) {
      offs.push(on('server:install-progress', (d: any) => {
        if (d?.serverId !== serverId || d?.stage !== 'mod_sources') return
        progress = { percent: d.percent ?? 0, file: '', action: 'copy', done: 0, total: 0 }
        if (d.message) progress.file = d.message
      }))
    } else {
      offs.push(on('mods:sources:progress', (d: ModSourcesProgress) => {
        if (d?.packId !== packId) return
        progress = { percent: d.percent, file: d.file, action: d.action, done: d.done, total: d.total }
      }))
    }
  })
  onDestroy(() => { offs.forEach(f => f()); offs = [] })

  const hasFolder = $derived(view.locked)
  const hasFiles = $derived(view.sources.some(s => s.kind === 'file'))
</script>

<UiModal
  open={true}
  title={t('modsrc.title')}
  subtitle={t('modsrc.subtitle')}
  icon="ti-link"
  color="var(--violet)"
  width="640px"
  locked={busy}
  onclose={() => { if (!busy) onclose() }}
>
  <div class="ms-body">
    {#if progress}
      <div class="ms-progress" role="progressbar" aria-valuenow={progress.percent} aria-valuemin="0" aria-valuemax="100">
        <div class="ms-progress-top">
          <span>{actionText(progress.action)}{#if progress.file}: <b>{progress.file}</b>{/if}</span>
          <span>{progress.percent}%</span>
        </div>
        <div class="ms-bar"><div class="ms-bar-fill" style={`width:${progress.percent}%`}></div></div>
      </div>
    {/if}

    {#if hasFolder}
      <div class="ms-note"><i class="ti ti-lock"></i> {t('modsrc.folderLocked')}</div>
    {/if}

    {#if loading}
      <p class="ms-empty"><i class="ti ti-loader spin"></i></p>
    {:else if view.sources.length === 0}
      <p class="ms-empty">{t('modsrc.empty')}</p>
    {:else}
      <div class="ms-list">
        {#each view.sources as s (s.id)}
          <div class="ms-row" class:missing={s.missing}>
            <i class={`ti ${s.kind === 'folder' ? 'ti-folder' : 'ti-file-zip'} ms-ico`}></i>
            <div class="ms-info">
              <div class="ms-name">
                {s.kind === 'folder' ? t('modsrc.kindFolder') : (s.fileName || s.path)}
                {#if s.missing}<span class="ms-badge">{t('modsrc.missing')}</span>{/if}
              </div>
              <div class="ms-path" title={s.path}>{s.path}</div>
            </div>
            <button class="icon-btn del" disabled={busy} onclick={() => remove(s.id)} title={t('modsrc.detach')}>
              <i class="ti ti-unlink"></i>
            </button>
          </div>
        {/each}
      </div>
    {/if}
  </div>

  {#snippet footer()}
    <button class="btn btn-ghost" onclick={() => add('file')} disabled={busy || hasFolder} title={hasFolder ? t('modsrc.folderExclusive') : ''}>
      <i class="ti ti-file-plus"></i> {t('modsrc.addFile')}
    </button>
    <button class="btn btn-ghost" onclick={() => add('folder')} disabled={busy || hasFolder || hasFiles} title={hasFiles ? t('modsrc.detachFirst') : ''}>
      <i class="ti ti-folder-plus"></i> {t('modsrc.addFolder')}
    </button>
    {#if !isServer && view.sources.length > 0}
      <button class="btn btn-ghost" onclick={syncNow} disabled={busy}><i class="ti ti-refresh"></i> {t('modsrc.syncNow')}</button>
    {/if}
    <button class="btn btn-primary" onclick={onclose} disabled={busy}>{t('modsrc.close')}</button>
  {/snippet}
</UiModal>

<style>
  .ms-body { display: flex; flex-direction: column; gap: 12px; }
  .ms-empty { text-align: center; opacity: .7; padding: 18px 8px; margin: 0; }
  .ms-note { display: flex; gap: 8px; align-items: center; padding: 10px 12px; border-radius: 10px;
    background: color-mix(in srgb, var(--violet) 14%, transparent); font-size: 13px; }
  .ms-list { display: flex; flex-direction: column; gap: 8px; }
  .ms-row { display: flex; align-items: center; gap: 10px; padding: 10px 12px; border-radius: 10px;
    background: var(--bg-elevated, rgba(255,255,255,.04)); }
  .ms-row.missing { outline: 1px solid color-mix(in srgb, var(--orange) 60%, transparent); }
  .ms-ico { font-size: 20px; color: var(--violet); }
  .ms-info { flex: 1; min-width: 0; }
  .ms-name { font-weight: 600; display: flex; gap: 8px; align-items: center; }
  .ms-path { font-size: 12px; opacity: .65; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .ms-badge { font-size: 11px; font-weight: 600; padding: 1px 7px; border-radius: 999px;
    background: color-mix(in srgb, var(--orange) 25%, transparent); color: var(--orange); }
  .ms-progress { padding: 10px 12px; border-radius: 10px; background: color-mix(in srgb, var(--violet) 12%, transparent); }
  .ms-progress-top { display: flex; justify-content: space-between; gap: 8px; font-size: 13px; margin-bottom: 6px; }
  .ms-progress-top span:first-child { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .ms-bar { height: 6px; border-radius: 999px; background: rgba(255,255,255,.1); overflow: hidden; }
  .ms-bar-fill { height: 100%; background: var(--violet); transition: width .15s ease; }
</style>
