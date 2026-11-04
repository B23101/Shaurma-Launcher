<script lang="ts">
  // Крок «Файли»: де лежать збірки (папка даних) і перенесення збірок з
  // інших лаунчерів (Prism, Modrinth App). Сканування — лише стандартні теки
  // тих лаунчерів (бекенд MigrateScan), без обходу дисків.
  import { onDestroy, onMount } from 'svelte'
  import { slide } from 'svelte/transition'
  import { t } from '../i18n.svelte'
  import { App, on } from '../wails'
  import type { MigrationLauncher, MigrationPackItem, MigrateImportProgress } from '../types'
  import prismIcon from '../../assets/images/prism_icon.svg'
  import modrinthIcon from '../../assets/images/Modrinth_icon.png'

  let { appDir = '', freeGB = 0, onBrowse = null, onPathInput = null, imported = $bindable(0) }: {
    appDir?: string
    freeGB?: number
    onBrowse?: null | (() => void)
    onPathInput?: null | ((p: string) => void)
    imported?: number
  } = $props()

  let editPath = $state(false)
  let pathDraft = $state('')

  let scanning = $state(true)
  let launchers = $state<MigrationLauncher[]>([])
  let selected = $state<Set<string>>(new Set()) // "<kind>|<id>"
  let progress = $state<Record<string, MigrateImportProgress>>({})
  let importing = $state(false)
  let doneIds = $state<Set<string>>(new Set())
  let icons = $state<Record<string, string>>({})
  let offs: Array<() => void> = []

  const LOGO: Record<string, string> = { prism: prismIcon, modrinth: modrinthIcon }
  const total = $derived(launchers.reduce((n, l) => n + l.packs.length, 0))

  onMount(async () => {
    offs.push(on('migrate:progress', (p: MigrateImportProgress) => { if (p?.packId) progress = { ...progress, [p.packId]: p } }))
    try {
      const r = await App.MigrateScan()
      launchers = (r?.launchers ?? []).filter(l => l.found && l.packs.length > 0)
      const s = new Set<string>()
      for (const l of launchers) for (const p of l.packs) if (!p.alreadyImported) s.add(l.kind + '|' + p.id)
      selected = s
      for (const l of launchers) for (const p of l.packs) loadIcon(p)
    } catch { launchers = [] }
    finally { scanning = false }
  })
  onDestroy(() => { for (const o of offs) o() })

  async function loadIcon(p: MigrationPackItem) {
    if (!p.iconPath) return
    try { const u = await App.MigrateIconDataURL(p.iconPath); if (u) icons = { ...icons, [p.id]: u } } catch { /* без іконки */ }
  }
  function toggle(key: string) {
    const s = new Set(selected)
    if (s.has(key)) s.delete(key); else s.add(key)
    selected = s
  }
  function fmtSize(b: number) {
    if (!b) return ''
    const mb = b / 1048576
    return mb >= 1024 ? (mb / 1024).toFixed(1) + ' ' + t('wz.unit.gb') : Math.round(mb) + ' MB'
  }
  function pct(id: string) {
    const p = progress[id]
    if (!p) return 0
    if (p.stage === 'done') return 100
    return p.filesTotal > 0 ? Math.min(100, Math.round(p.filesDone / p.filesTotal * 100)) : 4
  }
  async function runImport() {
    if (importing || selected.size === 0) return
    importing = true
    try {
      for (const l of launchers) {
        const ids = l.packs.filter(p => selected.has(l.kind + '|' + p.id)).map(p => p.id)
        if (!ids.length) continue
        const r = await App.MigrateImport({ launcher: l.kind, packIds: ids })
        const ok = new Set(doneIds)
        for (const id of ids) if (!(r?.failed ?? []).includes(id)) ok.add(id)
        doneIds = ok
      }
      imported = doneIds.size
      selected = new Set()
      App.MigrateDismiss().catch(() => {})
    } catch (e) { console.error('migrate', e) }
    finally { importing = false }
  }
</script>

<div class="fs">
  <div class="label">{t('wz.files.where')}</div>
  <div class="folder">
    <span class="f-ico"><i class="ti ti-folder"></i></span>
    <div class="f-t">
      {#if editPath}
        <input class="input mono" bind:value={pathDraft} onkeydown={(e: any) => { if (e.key === 'Enter') { onPathInput?.(pathDraft.trim()); editPath = false } if (e.key === 'Escape') editPath = false }} aria-label={t('wz.files.where')} />
      {:else}
        <b class="mono" title={appDir}>{appDir || '…'}</b>
        <small>{freeGB > 0 ? t('wz.files.free', { n: freeGB }) : t('wz.files.whereD')}</small>
      {/if}
    </div>
    <div class="f-btns">
      {#if editPath}
        <button class="btn btn-primary btn-sm" onclick={() => { onPathInput?.(pathDraft.trim()); editPath = false }}><i class="ti ti-check"></i></button>
      {:else}
        <button class="btn btn-ghost btn-sm" onclick={() => { pathDraft = appDir; editPath = true }} title={t('wz.files.type')} aria-label={t('wz.files.type')}><i class="ti ti-pencil"></i></button>
        <button class="btn btn-ghost btn-sm" onclick={() => onBrowse?.()}><i class="ti ti-folder-open"></i> {t('wz.game.change')}</button>
      {/if}
    </div>
  </div>
  <div class="note"><i class="ti ti-info-circle"></i> {t('wz.files.note')}</div>

  <div class="label mig-head">
    <span>{t('wz.files.migrate')}</span>
    {#if !scanning && total > 0}<span class="cnt">{selected.size}/{total}</span>{/if}
  </div>
  {#if scanning}
    <div class="scan"><span class="mini-spin"></span> {t('wz.files.scanning')}</div>
  {:else if launchers.length === 0}
    <div class="empty"><i class="ti ti-mood-smile"></i><div><b>{t('wz.files.none')}</b><small>{t('wz.files.noneD')}</small></div></div>
  {:else}
    {#each launchers as l, li}
      <div class="lch" style="--i:{li}">
        <div class="lch-head">
          {#if LOGO[l.kind]}<img src={LOGO[l.kind]} alt="" />{:else}<i class="ti ti-box"></i>{/if}
          <b>{l.name}</b><small>{t('wz.files.packs', { n: l.packs.length })}</small>
        </div>
        <div class="packs">
          {#each l.packs as p, i}
            {@const key = l.kind + '|' + p.id}
            {@const done = doneIds.has(p.id) || p.alreadyImported}
            <button class="pk" class:sel={selected.has(key)} class:done disabled={importing || done} style="--i:{i}" onclick={() => toggle(key)}>
              <span class="pk-ico">{#if icons[p.id]}<img src={icons[p.id]} alt="" />{:else}<i class="ti ti-box"></i>{/if}</span>
              <span class="pk-t"><b>{p.name}</b><small>{p.mcVersion}{p.loader && p.loader !== 'vanilla' ? ' · ' + p.loader : ''}{p.sizeBytes ? ' · ' + fmtSize(p.sizeBytes) : ''}</small></span>
              {#if done}<i class="ti ti-circle-check-filled pk-ok"></i>
              {:else if importing && progress[p.id]}<span class="pk-bar"><span style="width:{pct(p.id)}%"></span></span>
              {:else}<span class="pk-chk"><i class="ti ti-check"></i></span>{/if}
            </button>
          {/each}
        </div>
      </div>
    {/each}
    {#if selected.size > 0 || importing}
      <button class="btn btn-primary mig-go" transition:slide={{ duration: 200 }} disabled={importing} onclick={runImport}>
        {#if importing}<span class="mini-spin light"></span> {t('wz.files.importing')}{:else}<i class="ti ti-transfer-in"></i> {t('wz.files.import', { n: selected.size })}{/if}
      </button>
    {/if}
  {/if}
</div>

<style>
  .label { font-size: 11.5px; font-weight: 700; color: var(--text-mute); margin: 16px 0 8px; display: flex; justify-content: space-between; }
  .fs { margin-top: 18px; }
  .fs > .label:first-child { margin-top: 0; }
  .cnt { font-family: var(--font-mono); color: var(--text-dim); }
  .note { display: flex; gap: 7px; align-items: flex-start; font-size: 11.5px; color: var(--text-mute); margin-top: 8px; line-height: 1.45; }
  .note i { font-size: 14px; flex-shrink: 0; margin-top: 1px; }
  .mono { font-family: var(--font-mono); }
  .mini-spin { display: inline-block; width: 14px; height: 14px; border-radius: 50%; border: 2px solid rgba(var(--orange-rgb), .3); border-top-color: var(--orange); animation: sp .7s linear infinite; vertical-align: middle; }
  .mini-spin.light { border-color: rgba(255, 255, 255, .35); border-top-color: #fff; }
  @keyframes sp { to { transform: rotate(360deg) } }
  @keyframes pop { from { opacity: 0; transform: translateY(8px) scale(.96) } }

  .folder {
    display: flex; align-items: center; gap: 12px; padding: 12px 14px; border-radius: 16px;
    background: linear-gradient(120deg, rgba(var(--orange-rgb), .1), transparent 60%), var(--bg-card); border: 1.5px solid rgba(var(--orange-rgb), .35);
    animation: pop .45s cubic-bezier(.34, 1.4, .64, 1) backwards .15s;
  }
  .f-ico { width: 42px; height: 42px; border-radius: 12px; display: grid; place-items: center; font-size: 22px; color: var(--orange); background: rgba(var(--orange-rgb), .15); flex-shrink: 0; }
  .f-t { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
  .f-t b { font-size: 13px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; direction: rtl; text-align: left; }
  .f-t small { font-size: 11.5px; color: var(--text-mute); }
  .f-btns { display: flex; gap: 6px; flex-shrink: 0; }

  .scan, .empty { display: flex; align-items: center; gap: 10px; padding: 14px; border-radius: 14px; background: var(--bg-card); border: 1px dashed var(--border-hi); font-size: 12.5px; color: var(--text-mute); }
  .empty i { font-size: 24px; color: var(--orange); }
  .empty div { display: flex; flex-direction: column; } .empty b { color: var(--text); font-size: 13px; } .empty small { font-size: 11.5px; }

  .lch { margin-bottom: 10px; animation: pop .45s cubic-bezier(.22, 1, .36, 1) backwards; animation-delay: calc(.2s + var(--i) * .08s); }
  .lch-head { display: flex; align-items: center; gap: 8px; margin-bottom: 7px; }
  .lch-head img { width: 20px; height: 20px; object-fit: contain; }
  .lch-head b { font-size: 13px; } .lch-head small { font-size: 11.5px; color: var(--text-dim); }
  .packs { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 7px; }
  .pk {
    display: flex; align-items: center; gap: 10px; padding: 8px 10px; border-radius: 12px; text-align: left; min-width: 0;
    background: var(--bg-card); border: 1.5px solid var(--border); color: var(--text); font: inherit; cursor: pointer;
    transition: border-color .2s, transform .2s cubic-bezier(.34, 1.56, .64, 1), background .2s;
    animation: pop .4s cubic-bezier(.34, 1.4, .64, 1) backwards; animation-delay: calc(.25s + var(--i) * .04s);
  }
  .pk:hover:not(:disabled) { transform: translateY(-2px); border-color: var(--border-hi); }
  .pk.sel { border-color: var(--orange); background: rgba(var(--orange-rgb), .07); }
  .pk.done { opacity: .75; cursor: default; }
  .pk-ico { width: 32px; height: 32px; border-radius: 8px; display: grid; place-items: center; overflow: hidden; background: var(--hover-strong); color: var(--text-dim); flex-shrink: 0; }
  .pk-ico img { width: 100%; height: 100%; object-fit: cover; image-rendering: pixelated; }
  .pk-t { flex: 1; min-width: 0; display: flex; flex-direction: column; }
  .pk-t b, .pk-t small { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .pk-t b { font-size: 12.5px; } .pk-t small { font-size: 11px; color: var(--text-mute); }
  .pk-chk { width: 20px; height: 20px; border-radius: 6px; border: 1.5px solid var(--border-hi); display: grid; place-items: center; font-size: 13px; color: transparent; flex-shrink: 0; transition: background .2s, border-color .2s, color .2s; }
  .pk.sel .pk-chk { background: var(--orange); border-color: var(--orange); color: #fff; }
  .pk-ok { color: var(--green); font-size: 20px; flex-shrink: 0; }
  .pk-bar { width: 44px; height: 6px; border-radius: 3px; background: var(--hover-strong); overflow: hidden; flex-shrink: 0; }
  .pk-bar span { display: block; height: 100%; background: var(--orange); transition: width .3s; }
  .mig-go { margin-top: 4px; }
</style>
