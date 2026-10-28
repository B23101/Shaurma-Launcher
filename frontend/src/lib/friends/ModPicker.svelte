<script lang="ts">
  // Вибір мода для поширення прямо з чату: пошук у браузері контенту лаунчера
  // (Modrinth), клік по результату — і мод летить другові карткою. Те саме
  // можна зробити зі сторінки мода в «Браузері контенту» (кнопка «Поділитися»).
  import { onMount } from 'svelte'
  import { fly, fade } from 'svelte/transition'
  import { cubicOut } from 'svelte/easing'
  import { App } from '../wails'
  import { t } from '../i18n.svelte'
  import { tip } from './tooltip'
  import type { BrowserEntry } from '../types'

  let { onPick, onClose, busy = false }: {
    onPick: (e: BrowserEntry) => void
    onClose: () => void
    busy?: boolean
  } = $props()

  const TYPES = ['mod', 'modpack', 'shader', 'resourcepack'] as const
  let type = $state<(typeof TYPES)[number]>('mod')
  let query = $state('')
  let results = $state<BrowserEntry[]>([])
  let loading = $state(true)
  let error = $state(false)
  let seq = 0
  let timer: ReturnType<typeof setTimeout> | undefined
  let inputEl: HTMLInputElement | undefined = $state()

  async function run() {
    const my = ++seq
    loading = true
    error = false
    try {
      const q = query.trim()
      const res = await App.BrowserSearchModrinth(q, type, '', '', '', q ? 'relevance' : 'downloads', false, false, false, 12, 0, '')
      if (my !== seq) return
      results = res?.modrinth ?? []
      error = !!res?.modrinthError && results.length === 0
    } catch {
      if (my !== seq) return
      results = []
      error = true
    } finally {
      if (my === seq) loading = false
    }
  }

  function schedule() { clearTimeout(timer); timer = setTimeout(run, 280) }
  function pickType(x: (typeof TYPES)[number]) { if (type !== x) { type = x; run() } }
  function fmt(n: number): string {
    if (n >= 1e6) return (n / 1e6).toFixed(1).replace(/\.0$/, '') + 'M'
    if (n >= 1e3) return Math.round(n / 1e3) + 'K'
    return String(n)
  }

  onMount(() => { run(); inputEl?.focus(); return () => clearTimeout(timer) })
</script>

<svelte:window onkeydown={(e) => { if (e.key === 'Escape') onClose() }} />

<div class="mp" transition:fly={{ y: 14, duration: 220, easing: cubicOut }} role="dialog" aria-label={t('social.mod.pickTitle')}>
  <div class="mp-head">
    <div class="mp-title"><i class="ti ti-package"></i> {t('social.mod.pickTitle')}</div>
    <button class="mp-x" onclick={onClose} use:tip={t('friends.close')} aria-label={t('friends.close')}><i class="ti ti-x"></i></button>
  </div>
  <div class="mp-search">
    <i class="ti ti-search"></i>
    <input bind:this={inputEl} bind:value={query} oninput={schedule} placeholder={t('social.mod.searchPlaceholder')} spellcheck="false" autocomplete="off" />
    {#if loading}<i class="ti ti-loader-2 spin mp-spin"></i>{/if}
  </div>
  <div class="mp-types" role="tablist">
    {#each TYPES as x (x)}
      <button role="tab" aria-selected={type === x} class:on={type === x} onclick={() => pickType(x)}>{t(`browser.type.${x}`)}</button>
    {/each}
  </div>
  <div class="mp-list" class:busy>
    {#if error}
      <div class="mp-empty"><i class="ti ti-wifi-off"></i> {t('social.mod.searchFailed')}</div>
    {:else if !loading && results.length === 0}
      <div class="mp-empty"><i class="ti ti-mood-empty"></i> {t('social.mod.noResults')}</div>
    {:else}
      {#each results as r, i (r.source + r.projectId)}
        <button class="mp-row" onclick={() => onPick(r)} disabled={busy} in:fly={{ y: 8, duration: 200, delay: Math.min(i, 8) * 22, easing: cubicOut }}>
          <span class="mp-ic">{#if r.iconUrl}<img src={r.iconUrl} alt="" loading="lazy" />{:else}<i class="ti ti-puzzle"></i>{/if}</span>
          <span class="mp-txt">
            <span class="mp-name">{r.name}</span>
            <span class="mp-sub">{r.author}{r.author ? ' · ' : ''}<i class="ti ti-download"></i> {fmt(r.downloads)}</span>
          </span>
          <span class="mp-send"><i class="ti ti-send"></i></span>
        </button>
      {/each}
    {/if}
  </div>
</div>

<style>
  .mp { position: absolute; left: 10px; right: 10px; bottom: 10px; z-index: 30; display: flex; flex-direction: column; max-height: min(430px, 78%);
    background: var(--bg-panel); border: 1px solid var(--border-hi); border-radius: 14px; box-shadow: 0 -10px 40px rgba(0,0,0,.45), 0 0 0 1px rgba(var(--orange-rgb), .06); overflow: hidden; }
  .mp-head { display: flex; align-items: center; justify-content: space-between; padding: 11px 12px 6px; }
  .mp-title { display: flex; align-items: center; gap: 8px; font: 750 12.5px/1 var(--font-ui); color: var(--text); }
  .mp-title i { color: var(--orange); font-size: 15px; }
  .mp-x { width: 26px; height: 26px; border: 0; border-radius: 8px; background: var(--hover); color: var(--text-mute); cursor: pointer; display: flex; align-items: center; justify-content: center; transition: background .14s, color .14s, transform .12s; }
  .mp-x:hover { background: var(--hover-strong); color: var(--text); } .mp-x:active { transform: scale(.9); }
  .mp-search { display: flex; align-items: center; gap: 8px; margin: 4px 12px 8px; padding: 0 11px; height: 36px; border-radius: 10px; background: var(--bg-input); border: 1px solid var(--border-hi); transition: border-color .15s, box-shadow .15s; }
  .mp-search:focus-within { border-color: color-mix(in srgb, var(--orange) 60%, transparent); box-shadow: 0 0 0 3px rgba(var(--orange-rgb), .12); }
  .mp-search i { color: var(--text-dim); font-size: 14px; }
  .mp-search input { flex: 1; min-width: 0; background: none; border: 0; outline: none; color: var(--text); font: 500 12.5px/1 var(--font-ui); }
  .mp-spin { color: var(--orange) !important; }
  .mp-types { display: flex; gap: 6px; padding: 0 12px 8px; overflow-x: auto; }
  .mp-types button { flex: 0 0 auto; padding: 5px 11px; border-radius: 20px; border: 1px solid var(--border); background: transparent; color: var(--text-mute); font: 650 11px/1 var(--font-ui); cursor: pointer; transition: color .14s, background .14s, border-color .14s, transform .12s; }
  .mp-types button:hover { color: var(--text); } .mp-types button:active { transform: scale(.95); }
  .mp-types button.on { color: var(--text); background: rgba(var(--orange-rgb), .14); border-color: rgba(var(--orange-rgb), .5); }
  .mp-list { flex: 1; min-height: 120px; overflow-y: auto; padding: 0 8px 10px; display: flex; flex-direction: column; gap: 2px; transition: opacity .15s; }
  .mp-list.busy { opacity: .55; pointer-events: none; }
  .mp-row { display: flex; align-items: center; gap: 10px; padding: 7px 8px; border-radius: 10px; border: 0; background: transparent; cursor: pointer; text-align: left; transition: background .13s, transform .13s; }
  .mp-row:hover { background: var(--hover); transform: translateX(2px); }
  .mp-row:active { transform: scale(.98); }
  .mp-ic { flex: 0 0 auto; width: 36px; height: 36px; border-radius: 9px; overflow: hidden; background: var(--bg-input); border: 1px solid var(--border); display: flex; align-items: center; justify-content: center; color: var(--text-dim); }
  .mp-ic img { width: 100%; height: 100%; object-fit: cover; }
  .mp-txt { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
  .mp-name { font: 700 12.5px/1.25 var(--font-ui); color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .mp-sub { font: 500 var(--fs-min)/1.2 var(--font-ui); color: var(--text-dim); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .mp-sub i { font-size: 10px; }
  .mp-send { flex: 0 0 auto; color: var(--orange); opacity: 0; transform: translateX(-6px); transition: opacity .14s, transform .18s cubic-bezier(.2,1.2,.4,1); }
  .mp-row:hover .mp-send { opacity: 1; transform: none; }
  .mp-empty { display: flex; align-items: center; justify-content: center; gap: 8px; flex: 1; padding: 30px 12px; color: var(--text-dim); font: 600 12px/1.4 var(--font-ui); }
</style>
