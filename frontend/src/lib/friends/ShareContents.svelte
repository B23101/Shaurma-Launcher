<script lang="ts">
  // «Що всередині» передачі: підсумок за категоріями (моди, ресурспаки, конфіги, світи…),
  // скільки прийде напряму з Modrinth, і список модів/паків з іконками. Мод із Modrinth
  // відкривається в браузері контенту. Дані — SharePreview (маніфест відправника через P2P,
  // назви й іконки — напряму з Modrinth); кеш і завантаження — у shareStore.
  import { t } from '../i18n.svelte'
  import { sharePreview, loadPreview, openModFromShare } from './shareStore.svelte'
  import { fmtBytes, type SharePreviewItem } from './shareMsg'

  let { sid, maxHeight = '300px' }: { sid: string; maxHeight?: string } = $props()

  const st = $derived(sharePreview[sid])
  const data = $derived(st?.data)

  const CATS = ['mods', 'resourcepacks', 'shaderpacks', 'plugins', 'datapacks'] as const
  const CAT_ICON: Record<string, string> = {
    mods: 'ti-puzzle', resourcepacks: 'ti-photo', shaderpacks: 'ti-sun', plugins: 'ti-plug', datapacks: 'ti-database',
    config: 'ti-settings', worlds: 'ti-world', other: 'ti-files',
  }
  const tabs = $derived(CATS.filter(c => data?.items.some(i => i.category === c)))
  let tab = $state<string>('')
  const curTab = $derived(tabs.includes(tab as any) ? tab : (tabs[0] ?? ''))
  let q = $state('')
  const list = $derived.by(() => {
    if (!data) return [] as SharePreviewItem[]
    const s = q.trim().toLowerCase()
    return data.items
      .filter(i => i.category === curTab && (!s || (i.title ?? '').toLowerCase().includes(s) || i.name.toLowerCase().includes(s)))
      .sort((a, b) => (a.title || a.name).localeCompare(b.title || b.name))
  })
  const failedIcons = $state<Record<string, boolean>>({})
</script>

<div class="sc">
  {#if !st || st.loading}
    <div class="sc-state"><i class="ti ti-loader-2 sc-spin"></i> {t('share.contents.loading')}</div>
  {:else if st.error}
    <div class="sc-state bad">
      <i class="ti ti-alert-triangle"></i><span>{st.error}</span>
      <button class="sc-mini" onclick={() => loadPreview(sid, true)}><i class="ti ti-refresh"></i> {t('share.in.retry')}</button>
    </div>
  {:else if data}
    <div class="sc-groups">
      {#each data.groups as g (g.key)}
        <span class="sc-chip" title={fmtBytes(g.bytes)}><i class="ti {CAT_ICON[g.key] ?? 'ti-files'}"></i> {t('share.contents.cat.' + g.key)} · {g.files}</span>
      {/each}
    </div>
    {#if data.cdnFiles > 0}
      <div class="sc-cdn"><i class="ti ti-bolt"></i> {t('share.contents.cdn', { n: data.cdnFiles, size: fmtBytes(data.cdnBytes) })}</div>
    {/if}
    {#if tabs.length > 0}
      <div class="sc-bar">
        <div class="sc-tabs" role="tablist">
          {#each tabs as c (c)}
            <button role="tab" aria-selected={curTab === c} class:on={curTab === c} onclick={() => (tab = c)}>
              {t('share.contents.cat.' + c)} <b>{data.items.filter(i => i.category === c).length}</b>
            </button>
          {/each}
        </div>
        <input class="sc-q" type="text" bind:value={q} placeholder={t('share.contents.search')} spellcheck="false" />
      </div>
      <div class="sc-list" style="max-height:{maxHeight}">
        {#each list as it (it.path)}
          {@const canOpen = !!it.projectId}
          <div class="sc-row" class:dis={it.disabled}>
            <div class="sc-ic">
              {#if it.iconUrl && !failedIcons[it.path]}
                <img src={it.iconUrl} alt="" loading="lazy" referrerpolicy="no-referrer" onerror={() => (failedIcons[it.path] = true)} />
              {:else}<i class="ti {CAT_ICON[it.category]}"></i>{/if}
            </div>
            <div class="sc-main">
              <b title={it.title || it.name}>{it.title || it.name}</b>
              <span title={it.name}>{it.title ? it.name : ''}{it.title ? ' · ' : ''}{fmtBytes(it.size)}{it.disabled ? ' · ' + t('share.contents.disabled') : ''}</span>
            </div>
            {#if canOpen}
              <span class="sc-badge" title={t('share.contents.cdnTip')}><i class="ti ti-bolt"></i> Modrinth</span>
              <button class="sc-open" onclick={() => openModFromShare(it.projectId!)} title={t('share.contents.open')} aria-label={t('share.contents.open')}>
                <i class="ti ti-external-link"></i>
              </button>
            {/if}
          </div>
        {:else}
          <div class="sc-state">{t('share.send.notFound')}</div>
        {/each}
      </div>
      {#if data.truncated}<div class="sc-note">{t('share.contents.truncated')}</div>{/if}
    {:else}
      <div class="sc-note">{t('share.contents.noMods')}</div>
    {/if}
  {/if}
</div>

<style>
  .sc { display: flex; flex-direction: column; gap: 8px; min-width: 0; }
  .sc-state { display: flex; align-items: center; justify-content: center; gap: 8px; padding: 14px; font-size: 12px; color: var(--text-mute); text-align: center; flex-wrap: wrap; }
  .sc-state.bad { color: var(--red); }
  .sc-groups { display: flex; flex-wrap: wrap; gap: 6px; }
  .sc-chip { display: inline-flex; align-items: center; gap: 5px; font-size: 11px; font-weight: 700; color: var(--text-mute); background: var(--bg-input);
    border: 1px solid var(--border); border-radius: 99px; padding: 3px 9px; }
  .sc-chip i { color: var(--orange); }
  .sc-cdn { display: flex; gap: 6px; align-items: center; font-size: 12px; color: var(--green); }
  .sc-bar { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
  .sc-tabs { display: flex; gap: 4px; flex-wrap: wrap; flex: 1; min-width: 0; }
  .sc-tabs button { border: 1px solid var(--border); background: var(--bg-input); color: var(--text-mute); border-radius: 8px; padding: 5px 9px;
    font: 600 11.5px var(--font-ui); cursor: pointer; }
  .sc-tabs button b { color: var(--text-mute); margin-left: 3px; }
  .sc-tabs button.on { color: var(--orange); border-color: var(--orange); background: rgba(var(--orange-rgb), .1); }
  .sc-q { width: 150px; background: var(--bg-input); border: 1px solid var(--border); border-radius: 8px; padding: 5px 9px; color: var(--text); font: 12px var(--font-ui); }
  .sc-q:focus { outline: none; border-color: var(--orange); }
  .sc-list { overflow-y: auto; overscroll-behavior: contain; display: flex; flex-direction: column; gap: 4px; background: var(--bg-input);
    border: 1px solid var(--border); border-radius: 12px; padding: 5px; }
  .sc-row { display: flex; align-items: center; gap: 9px; padding: 5px 6px; border-radius: 9px; }
  .sc-row:hover { background: var(--hover); }
  .sc-row.dis { opacity: .55; }
  .sc-ic { width: 32px; height: 32px; border-radius: 8px; flex: none; display: grid; place-items: center; overflow: hidden; background: var(--bg-panel);
    color: var(--text-mute); font-size: 16px; }
  .sc-ic img { width: 100%; height: 100%; object-fit: cover; }
  .sc-main { flex: 1; min-width: 0; display: flex; flex-direction: column; }
  .sc-main b { font-size: 12.5px; color: var(--text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .sc-main span { font-size: var(--fs-min); color: var(--text-mute); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .sc-badge { display: inline-flex; align-items: center; gap: 3px; font-size: var(--fs-min); font-weight: 700; color: var(--green); flex: none;
    background: color-mix(in srgb, var(--green) 12%, transparent); border-radius: 99px; padding: 2px 7px; }
  .sc-open { width: 26px; height: 26px; flex: none; border-radius: 7px; border: 1px solid var(--border); background: var(--bg-panel); color: var(--text-mute);
    cursor: pointer; display: grid; place-items: center; }
  .sc-open:hover { color: var(--orange); border-color: var(--orange); }
  .sc-note { font-size: 11.5px; color: var(--text-mute); }
  .sc-mini { display: inline-flex; gap: 4px; align-items: center; border: 1px solid var(--border); background: var(--bg-input); color: var(--text-mute);
    border-radius: 8px; padding: 5px 9px; font: 600 11px var(--font-ui); cursor: pointer; }
  .sc-spin { animation: sc-rot 1s linear infinite; }
  @keyframes sc-rot { to { transform: rotate(360deg); } }
</style>
