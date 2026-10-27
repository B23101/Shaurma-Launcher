<script lang="ts">
  // FoldersSection.svelte — «Теки та ярлики»: де лаунчер зберігає файли і які
  // ярлики збірок створено на робочому столі / в меню «Пуск».
  import { untrack } from 'svelte'
  import { t } from '../i18n.svelte'
  import { App } from '../wails'
  import { toast } from '../toast.svelte'
  import ShortcutDialog from '../ShortcutDialog.svelte'
  import SCard from './SCard.svelte'
  import type { Account, DesktopShortcut, FolderPaths } from '../types'

  let {
    folderPaths, folderRows, accounts, loadFolderPaths, setFolder, resetBaseDir,
  }: {
    folderPaths: FolderPaths | null
    folderRows: { kind: string; name: string; desc: string; path: string; custom: string; reset: boolean; fixed: boolean }[]
    accounts: Account[]
    loadFolderPaths: (force?: boolean) => void
    setFolder: (kind: string) => void
    resetBaseDir: () => void
  } = $props()

  let shortcuts = $state<DesktopShortcut[] | null>(null)
  let loading = $state(false)
  let editing = $state<DesktopShortcut | null>(null)

  async function load() {
    if (loading) return
    loading = true
    try { shortcuts = (await App.GetAllShortcuts()) ?? [] }
    catch (e: any) { shortcuts = null; toast(t('error.withMessage', { message: e }), 'error') }
    finally { loading = false }
  }
  // untrack: load() читає і пише власний стан — без цього ефект зациклюється
  $effect(() => { untrack(load) })

  async function remove(sc: DesktopShortcut) {
    try {
      await App.RemoveDesktopShortcut(sc.packId)
      shortcuts = (shortcuts ?? []).filter((s) => s.packId !== sc.packId)
      toast(t('pack.shortcutRemoved'), 'success')
    } catch (e: any) {
      toast(t('pack.shortcutError', { message: e }), 'error')
    }
  }
  function openFolder(path: string) {
    App.OpenFolder(path.replace(/[\\/][^\\/]+$/, '')).catch((e: any) => toast(t('error.withMessage', { message: e }), 'error'))
  }
</script>

<SCard id="x:folders" icon="ti-folder" title={t('st.c.folders')} desc={t('st.c.folders.d')}>
  {#if folderPaths}
    {#each folderRows as f (f.kind)}
      <div class="st-row">
        <div class="st-row-text">
          <div class="st-row-name">{f.name} {#if f.custom}<span class="st-tag on" style="margin-left:6px">{t('st.folders.custom')}</span>{/if}</div>
          <div class="st-row-hint">{f.desc}</div>
          <div class="st-path" title={f.path}>{f.path}</div>
        </div>
        <div class="st-row-ctl">
          {#if !f.fixed}
            {#if f.reset}
              <button class="st-iconbtn" disabled={folderPaths.baseDir === folderPaths.defaultBaseDir} onclick={resetBaseDir}
                      title={t('st.folders.reset')} aria-label={t('st.folders.reset')}><i class="ti ti-rotate"></i></button>
            {/if}
            <button class="btn btn-ghost btn-sm" onclick={() => setFolder(f.kind)}><i class="ti ti-folder"></i> {t('st.folders.change')}</button>
          {:else}
            <span class="st-tag">{t('st.folders.fixed')}</span>
          {/if}
        </div>
      </div>
    {/each}
  {:else}
    <div class="st-pad"><button class="btn btn-ghost btn-sm" onclick={() => loadFolderPaths(true)}><i class="ti ti-refresh"></i> {t('st.folders.load')}</button></div>
  {/if}
</SCard>

<SCard id="x:shortcuts" icon="ti-external-link" tone="violet" title={t('st.c.shortcuts')} desc={t('st.c.shortcuts.d')}>
  {#snippet actions()}
    <button class="st-iconbtn" onclick={load} disabled={loading} title={t('st.refresh')} aria-label={t('st.refresh')}>
      <i class="ti {loading ? 'ti-loader-2 spin' : 'ti-refresh'}"></i>
    </button>
  {/snippet}
  {#if loading && shortcuts === null}
    <div class="st-empty"><i class="ti ti-loader-2 spin"></i><span>{t('loading')}</span></div>
  {:else if shortcuts === null}
    <div class="st-empty"><i class="ti ti-alert-circle"></i><span>{t('error.unknown')}</span></div>
  {:else if shortcuts.length === 0}
    <div class="st-empty"><i class="ti ti-external-link-off"></i><b>{t('st.shortcuts.empty')}</b><span>{t('st.shortcuts.emptyHint')}</span></div>
  {:else}
    {#each shortcuts as sc (sc.packId)}
      <div class="st-item">
        <div class="st-item-ic" style="--pcol:var(--violet-l)"><i class="ti ti-link"></i></div>
        <div class="st-item-body">
          <div class="st-item-name">{sc.name}
            {#if sc.autoJoinServer}<span class="st-tag"><i class="ti ti-server"></i> {sc.autoJoinServer}</span>{/if}
            {#if !sc.exists}<span class="st-tag bad">{t('st.shortcuts.missing')}</span>{/if}
          </div>
          <div class="st-item-sub" title={sc.path}>{sc.path}</div>
        </div>
        <div class="st-item-act">
          <button class="st-iconbtn" onclick={() => openFolder(sc.path)} title={t('st.shortcuts.openFolder')} aria-label={t('st.shortcuts.openFolder')}><i class="ti ti-folder-open"></i></button>
          <button class="st-iconbtn" onclick={() => (editing = sc)} title={t('st.shortcuts.edit')} aria-label={t('st.shortcuts.edit')}><i class="ti ti-pencil"></i></button>
          <button class="st-iconbtn danger" onclick={() => remove(sc)} title={t('st.delete')} aria-label={t('st.delete')}><i class="ti ti-trash"></i></button>
        </div>
      </div>
    {/each}
  {/if}
</SCard>

{#if editing}
  <ShortcutDialog packId={editing.packId} packName={editing.name} open={true} accounts={accounts}
                  onClose={() => (editing = null)} onCreate={async () => { editing = null; await load() }} />
{/if}
