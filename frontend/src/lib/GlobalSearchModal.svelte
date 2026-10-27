<script lang="ts">
  import Portal from './Portal.svelte'
  // Глобальний пошук (Ctrl+K / клік по полю пошуку в topbar). Шукає: назви
  // збірок, пресети гардероба, налаштування і контент браузера. Пошук
  // (завантаження пресетів/браузера) виконує БАТЬКО через реактивний
  // bind:query — компонент лише показує результати та віддає кліки.
  import { t } from './i18n.svelte'
  import { packAsset } from './packAssets.svelte'
  import PackPicker from './PackPicker.svelte'
  import type { PackRow, PackFolderRow, PackPickerItem } from './PackPicker.svelte'
  import type { BrowserEntry, BuildView, SettingDef, SkinPreset } from './types'

  let {
    open = false,
    query = $bindable(''),
    packs = [] as BuildView[],
    // Дані списку збірок з ПАПКАМИ (PackPicker) — пошук показує папки
    // розкривними, збірки всередині папок знаходяться і видно.
    packPicker = null,
    presetResults = [] as SkinPreset[],
    presetHeads = {} as Record<string, string>,
    presetCapes = {} as Record<string, string>,
    browser = [] as BrowserEntry[],
    browserLoading = false,
    settingsResults = [] as SettingDef[],
    grpTitleKeys = {} as Record<string, string>,
    onclose = null as null | (() => void),
    ongopack = null as null | ((id: string) => void),
    ongopreset = null as null | ((id: string) => void),
    ongosettings = null as null | ((sd: SettingDef) => void),
    ongobrowser = null as null | ((be: BrowserEntry) => void)
  }: {
    open?: boolean
    query?: string
    packs?: BuildView[]
    packPicker?: {
      items: PackPickerItem[]
      rowById: Map<string, PackRow>
      folderById: Map<string, PackFolderRow>
    } | null
    presetResults?: SkinPreset[]
    presetHeads?: Record<string, string>
    presetCapes?: Record<string, string>
    browser?: BrowserEntry[]
    browserLoading?: boolean
    settingsResults?: SettingDef[]
    grpTitleKeys?: Record<string, string>
    onclose?: null | (() => void)
    ongopack?: null | ((id: string) => void)
    ongopreset?: null | ((id: string) => void)
    ongosettings?: null | ((sd: SettingDef) => void)
    ongobrowser?: null | ((be: BrowserEntry) => void)
  } = $props()
</script>

{#if open}
  <Portal>
<div class="modal-overlay search-overlay" onclick={onclose}>
    <div class="global-search" onclick={(e: any) => e.stopPropagation()} role="dialog" aria-label={t('topbar.search')}>
      <div class="gs-input-row">
        <i class="ti ti-search"></i>
        <input autofocus placeholder={t('globalSearch.placeholder')} bind:value={query}
               onkeydown={(e: any) => { if (e.key === 'Escape') onclose?.() }} />
        <kbd>Esc</kbd>
        <button class="gs-close" title={t('win.close')} aria-label={t('win.close')} onclick={onclose}>
          <i class="ti ti-x"></i>
        </button>
      </div>
      <div class="gs-results">
      {#if query.trim().length === 0}
        <div class="gs-empty"><i class="ti ti-bulb"></i> {t('globalSearch.hint')}</div>
      {:else}
        {#if packs.length > 0}
          <div class="gs-group">
            <div class="gs-group-title"><i class="ti ti-box"></i> {t('globalSearch.packs')}<span class="gs-count">{packs.length}</span></div>
            {#if packPicker && packPicker.items.length > 0}
              <PackPicker
                items={packPicker.items}
                rowById={packPicker.rowById}
                folderById={packPicker.folderById}
                query={query}
                onselect={(id) => ongopack?.(id)}
                selectable={!!ongopack}
                class="gs-pp"
              />
            {:else}
              {#each packs as bv}
                {@const sic = packAsset(bv.build.iconUrl)}
                <div class="gs-item" onclick={() => ongopack?.(bv.build.id)}>
                  <div class="gs-ic" class:has-icon={!!sic} style="--pcol:{bv.build.color || 'var(--orange)'}">
                    {#if sic}<img src={sic} alt="" loading="eager"/>{:else}<i class="ti {bv.build.icon || 'ti-puzzle'}"></i>{/if}
                  </div>
                  <div class="gs-name">{bv.build.name}</div>
                  <div class="gs-meta">{bv.build.mcVersion}{bv.build.loaderType ? ' · ' + bv.build.loaderType : ''}</div>
                </div>
              {/each}
            {/if}
          </div>
        {/if}
        {#if presetResults.length > 0}
          <div class="gs-group">
            <div class="gs-group-title"><i class="ti ti-shirt"></i> {t('globalSearch.wardrobe')}<span class="gs-count">{presetResults.length}</span></div>
            {#each presetResults as p}
              <div class="gs-item" onclick={() => ongopreset?.(p.id)}>
                <div class="gs-wardrobe">
                  {#if presetHeads[p.id]}<span class="gs-head" style="background-image:url('{presetHeads[p.id]}')"></span>{/if}
                  {#if presetCapes[p.id]}<span class="gs-cape" style="background-image:url('{presetCapes[p.id]}')"></span>{/if}
                </div>
                <div class="gs-name">{p.name}</div>
                <div class="gs-meta">{p.slimArms ? t('wardrobe.slim') : t('wardrobe.classic')}{p.capePath ? ' · ' + t('wardrobe.withCape') : ''}</div>
              </div>
            {/each}
          </div>
        {/if}
        {#if settingsResults.length > 0}
          <div class="gs-group">
            <div class="gs-group-title"><i class="ti ti-settings"></i> {t('globalSearch.settings')}<span class="gs-count">{settingsResults.length}</span></div>
            {#each settingsResults as sd}
              <div class="gs-item" onclick={() => ongosettings?.(sd)}>
                <div class="gs-ic" style="--pcol:var(--orange)"><i class="ti ti-adjustments"></i></div>
                <div class="gs-name">{t(sd.label)}</div>
                <div class="gs-meta">{t(grpTitleKeys[sd.group] ?? 'settings.tab.general')}</div>
              </div>
            {/each}
          </div>
        {/if}
        {#if query.trim().length >= 2}
          <div class="gs-group">
            <div class="gs-group-title"><i class="ti ti-world"></i> {t('globalSearch.browser')}
              {#if browserLoading}<span class="gs-spinner"></span>{/if}
              {#if browser.length > 0}<span class="gs-count">{browser.length}</span>{/if}
            </div>
            {#if browser.length === 0 && !browserLoading}
              <div class="gs-empty small">{t('globalSearch.browserEmpty')}</div>
            {/if}
            {#each browser as be}
              <div class="gs-item" onclick={() => ongobrowser?.(be)}>
                <div class="gs-ic" class:has-icon={!!be.iconUrl} style="--pcol:var(--orange)">
                  {#if be.iconUrl}<img src={be.iconUrl} alt="" loading="lazy"/>{:else}<i class="ti ti-package"></i>{/if}
                </div>
                <div class="gs-name">{be.name}</div>
                <div class="gs-meta">{be.type}{be.mcVersion ? ' · ' + be.mcVersion : ''}</div>
              </div>
            {/each}
          </div>
        {/if}
        {#if packs.length === 0 && presetResults.length === 0 && settingsResults.length === 0 && browser.length === 0 && !browserLoading}
          <div class="gs-empty"><i class="ti ti-mood-empty"></i> {t('globalSearch.noResults')}</div>
        {/if}
      {/if}
      </div>
    </div>
  </div>
</Portal>
{/if}

<style>
  /* Глобальний пошук (Ctrl+K): стилі перенесені сюди з App.svelte разом
     з розміткою. .search-overlay піднятий над звичайними модалками. */
  .search-overlay { z-index:1000001; }
  .global-search {
    width:min(640px,94vw); max-height:74vh;
    display:flex; flex-direction:column; overflow:hidden;
    background:var(--bg-panel); border:1px solid var(--border-hi);
    border-radius:var(--r-lg); box-shadow:0 24px 60px rgba(0,0,0,.45);
  }
  .gs-input-row {
    display:flex; align-items:center; gap:10px;
    padding:14px 18px; border-bottom:1px solid var(--border); flex:0 0 auto;
  }
  .gs-input-row .ti-search { font-size:19px; color:var(--text-dim); flex:none; }
  .gs-input-row input {
    flex:1; border:none; background:transparent; font-size:15px;
    color:var(--text); outline:none; padding:0;
  }
  .gs-input-row input::placeholder { color:var(--text-mute); }
  .gs-input-row kbd {
    flex:none; padding:3px 9px; font-size:11px; font-family:var(--font-mono);
    color:var(--text-dim); background:var(--bg-input);
    border:1px solid var(--border); border-radius:var(--r-sm);
  }
  .gs-close {
    flex:none; width:30px; height:30px; border:none; border-radius:var(--r-sm);
    background:transparent; color:var(--text-mute); cursor:pointer;
    display:flex; align-items:center; justify-content:center; font-size:16px;
    transition:background .15s ease, color .15s ease;
  }
  .gs-close:hover { background:var(--bg-input-hi); color:var(--text); }
  /* Контейнер результатів: flex-розтяжка + власний скрол у межах панелі
     (без min-height:0 контент випирає за max-height панелі і обрізається
     overflow:hidden — пошук виглядав «порожнім»). */
  .gs-results { overflow-y:auto; flex:1 1 auto; min-height:0; padding:6px 0 10px; }
  .gs-group { padding:2px 0; }
  .gs-group-title {
    display:flex; align-items:center; gap:8px; padding:10px 18px 5px;
    font-size:12px; font-weight:700; text-transform:uppercase;
    letter-spacing:.05em; color:var(--text-dim);
  }
  .gs-group-title .ti { font-size:14px; }
  .gs-item {
    display:flex; align-items:center; gap:12px; padding:8px 18px;
    cursor:pointer; transition:background .12s ease;
  }
  .gs-item:hover, .gs-item:focus-visible { background:var(--hover); outline:none; }
  .gs-ic {
    width:34px; height:34px; border-radius:var(--r-md); flex:none; overflow:hidden;
    display:flex; align-items:center; justify-content:center; font-size:18px;
    color:var(--pcol,var(--orange)); background:color-mix(in srgb, var(--pcol,var(--orange)) 16%, var(--bg-input-hi));
  }
  .gs-ic.has-icon { background:transparent; }
  .gs-ic img { width:100%; height:100%; object-fit:cover; }
  .gs-name { flex:1 1 auto; min-width:0; font-size:14px; color:var(--text);
    white-space:nowrap; overflow:hidden; text-overflow:ellipsis; }
  .gs-meta { flex:none; font-size:12px; color:var(--text-dim); white-space:nowrap; }
  .gs-empty {
    padding:28px 18px; text-align:center; color:var(--text-dim);
    font-size:13px; display:flex; flex-direction:column; align-items:center; gap:8px;
  }
  .gs-empty .ti { font-size:26px; color:var(--text-mute); }
  .gs-empty.small { padding:14px 18px; font-size:12px; }
  .gs-spinner {
    width:13px; height:13px; margin-left:auto; border-radius:50%; flex:none;
    border:2px solid var(--border-hi); border-top-color:var(--orange);
    animation:gs-spin .7s linear infinite;
  }
  @keyframes gs-spin { to { transform:rotate(360deg); } }
</style>
