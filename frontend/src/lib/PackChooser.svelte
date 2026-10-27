<script lang="ts">
  // ── PackChooser: кнопка + модальне вікно вибору збірки ─────────────────
  // Замінює голий SelectField у міграції модів: замість випадаючого
  // select-списку (який ламав сучасний вигляд лаунчера) — повноцінне
  // модальне вікно з іконками/кольорами збірок, лічильником модів і
  // пошуком. Той самий «мовний» патерн, що й вибір збірки в інших місцях.
  import UiModal from './UiModal.svelte'
  import { t } from './i18n.svelte'
  import { packAsset } from './packAssets.svelte'
  import { smartImage } from './smartImage'
  import type { MigrationPack } from './types'

  let {
    packs = [] as MigrationPack[],
    value = '',
    onchange = (_v: string) => {},
    placeholder = '',
    disabled = false,
    ariaLabel = '',
    // packId, який у списку позначається як «поточна» збірка.
    currentId = '',
    searchPlaceholder = '',
  }: {
    packs?: MigrationPack[]
    value?: string
    onchange?: (id: string) => void
    placeholder?: string
    disabled?: boolean
    ariaLabel?: string
    currentId?: string
    searchPlaceholder?: string
  } = $props()

  let open = $state(false)
  let query = $state('')

  const selected = $derived(packs.find((p) => p.packId === value))
  const filtered = $derived.by(() => {
    const q = query.trim().toLowerCase()
    if (!q) return packs
    return packs.filter((p) =>
      `${p.name} ${p.mcVersion ?? ''} ${p.loader ?? ''}`.toLowerCase().includes(q)
    )
  })

  function openPicker() {
    if (disabled) return
    query = ''
    open = true
  }
  function pick(id: string) {
    onchange(id)
    open = false
  }
</script>

<button
  type="button"
  class="pc-trigger"
  class:placeholder={!selected}
  {disabled}
  onclick={openPicker}
  aria-label={ariaLabel || placeholder}
>
  {#if selected}
    <span class="pc-ic">
      {#if packAsset(selected.iconUrl)}
        <img src={packAsset(selected.iconUrl)} alt="" loading="lazy" use:smartImage />
      {:else}
        <i class="ti {selected.icon || 'ti-package'}" style="color:{selected.color || 'var(--orange)'}"></i>
      {/if}
    </span>
    <span class="pc-text">
      <span class="pc-name">{selected.name}</span>
      {#if selected.mcVersion || selected.loader}
        <span class="pc-sub">{selected.loader}{selected.loader && selected.mcVersion ? ' · ' : ''}{selected.mcVersion}</span>
      {/if}
    </span>
  {:else}
    <span class="pc-ic pc-ic-empty"><i class="ti ti-package"></i></span>
    <span class="pc-text"><span class="pc-name">{placeholder}</span></span>
  {/if}
  <i class="ti ti-chevron-down pc-caret"></i>
</button>

<UiModal
  {open}
  size="sm"
  title={t('browser.choosePack')}
  icon="ti-package"
  color="var(--orange)"
  onclose={() => (open = false)}
>
  <div class="pc-body">
    <div class="pc-search">
      <i class="ti ti-search"></i>
      <input bind:value={query} placeholder={searchPlaceholder || t('mods.migrateChooseSearch')} />
      {#if query}
        <button class="pc-search-x" onclick={() => (query = '')} aria-label={t('cancel')}><i class="ti ti-x"></i></button>
      {/if}
    </div>

    <div class="pc-list">
      {#each filtered as p (p.packId)}
        <button
          type="button"
          class="pc-row"
          class:sel={p.packId === value}
          onclick={() => pick(p.packId)}
        >
          <span class="pc-ic">
            {#if packAsset(p.iconUrl)}
              <img src={packAsset(p.iconUrl)} alt="" loading="lazy" use:smartImage />
            {:else}
              <i class="ti {p.icon || 'ti-package'}" style="color:{p.color || 'var(--orange)'}"></i>
            {/if}
          </span>
          <span class="pc-info">
            <span class="pc-rname">
              {p.name}
              {#if p.packId === currentId}<span class="pc-tag">{t('mods.migrateCurrentPack')}</span>{/if}
            </span>
            <span class="pc-rsub">
              {#if p.loader || p.mcVersion}{p.loader}{p.loader && p.mcVersion ? ' · ' : ''}{p.mcVersion}{:else}—{/if}
              {#if p.modCount > 0}<span class="pc-mods"><i class="ti ti-puzzle"></i>{p.modCount}</span>{/if}
            </span>
          </span>
          {#if p.packId === value}<i class="ti ti-check pc-check"></i>{/if}
        </button>
      {/each}
      {#if filtered.length === 0}
        <div class="pc-empty"><i class="ti ti-search-off"></i> {t('mods.migrateChooseEmpty')}</div>
      {/if}
    </div>
  </div>
</UiModal>

<style>
  .pc-trigger {
    width: 100%; min-width: 0;
    display: flex; align-items: center; gap: 10px;
    padding: 8px 12px; text-align: left;
    background: var(--bg-input); border: 1px solid var(--border);
    border-radius: var(--r-md); color: var(--text);
    font-family: inherit; cursor: pointer;
    transition: border-color .15s, background .15s;
  }
  .pc-trigger:hover:not(:disabled) { border-color: var(--border-hi); background: var(--bg-input-hi); }
  .pc-trigger:disabled { opacity: .55; cursor: default; }
  .pc-trigger.placeholder .pc-name { color: var(--text-dim); font-weight: 500; }

  .pc-ic {
    width: 30px; height: 30px; min-width: 30px; border-radius: 8px;
    display: flex; align-items: center; justify-content: center;
    background: var(--bg-panel); overflow: hidden; flex-shrink: 0;
  }
  .pc-ic img { width: 100%; height: 100%; object-fit: cover; display: block; }
  .pc-ic i { font-size: 15px; }
  .pc-ic-empty { color: var(--text-dim); }

  .pc-text { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 1px; }
  .pc-name {
    font-size: 12.5px; font-weight: 700; color: var(--text);
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .pc-sub { font-size: 10.5px; color: var(--text-mute); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .pc-caret { color: var(--text-dim); font-size: 14px; flex-shrink: 0; }

  /* ── Вміст модалки ── */
  .pc-body { display: flex; flex-direction: column; gap: 10px; }
  .pc-search {
    display: flex; align-items: center; gap: 8px;
    background: var(--bg-input); border: 1px solid var(--border);
    border-radius: var(--r-sm); padding: 0 11px; height: 34px;
  }
  .pc-search i { color: var(--text-dim); font-size: 14px; }
  .pc-search input { flex: 1; background: none; border: none; outline: none; color: var(--text); font-size: 12.5px; font-family: inherit; }
  .pc-search-x {
    border: none; background: none; color: var(--text-mute); cursor: pointer;
    display: flex; align-items: center; font-size: 14px; padding: 2px;
  }
  .pc-search-x:hover { color: var(--text); }

  .pc-list { display: flex; flex-direction: column; gap: 3px; max-height: 360px; overflow-y: auto; }
  .pc-row {
    display: flex; align-items: center; gap: 10px; width: 100%;
    padding: 7px 9px; text-align: left;
    background: transparent; border: 1px solid transparent; border-radius: var(--r-sm);
    color: var(--text); font-family: inherit; cursor: pointer;
    transition: background .14s, border-color .14s;
  }
  .pc-row:hover { background: var(--bg-input); border-color: var(--border); }
  .pc-row.sel { background: rgba(157,124,255,.1); border-color: rgba(157,124,255,.4); }

  .pc-info { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
  .pc-rname { display: flex; align-items: center; gap: 7px; font-size: 12.5px; font-weight: 700; color: var(--text); }
  .pc-tag {
    font-size: 9px; font-weight: 800; padding: 1px 6px; border-radius: 999px;
    background: rgba(var(--orange-rgb), .14); color: var(--orange); white-space: nowrap;
  }
  .pc-rsub { display: flex; align-items: center; gap: 8px; font-size: 10.5px; color: var(--text-mute); }
  .pc-mods { display: inline-flex; align-items: center; gap: 3px; }
  .pc-check { color: var(--violet-l); font-size: 15px; flex-shrink: 0; }
  .pc-empty {
    display: flex; align-items: center; justify-content: center; gap: 8px;
    padding: 24px 0; color: var(--text-mute); font-size: 12px;
  }
</style>
