<script lang="ts">
  import { tip as launcherTip } from './friends/tooltip'

  import Portal from './Portal.svelte'
  // Глобальний пошук (Ctrl+K / Ctrl+F / клік по полю пошуку в topbar).
  // Шукає: збірки (назва, лоадер, версія гри, папка), пресети гардероба,
  // налаштування, контент браузера та «швидкі дії» (розділи лаунчера,
  // створення/імпорт збірки…). Фільтр-чипи звужують пошук до одного
  // розділу, ↑/↓ ходять по результатах, Enter відкриває вибраний, а
  // порожній запит показує недавні пошуки й швидкий перехід.
  // Мережевий пошук (браузер) і завантаження пресетів робить БАТЬКО через
  // bind:query / bind:scope — компонент лише показує й віддає кліки.
  import { tick } from 'svelte'
  import { t } from './i18n.svelte'
  import { packAsset } from './packAssets.svelte'
  import type { PackRow, PackFolderRow, PackPickerItem } from './PackPicker.svelte'
  import type { BrowserEntry, BuildView, SettingDef, SkinPreset } from './types'

  export type SearchScope = 'all' | 'packs' | 'content' | 'settings' | 'wardrobe' | 'actions'
  export interface SearchAction { id: string; icon: string; label: string; meta?: string; keywords?: string }

  let {
    open = false,
    query = $bindable(''),
    scope = $bindable<SearchScope>('all'),
    packs = [] as BuildView[],
    packPicker = null,
    presetResults = [] as SkinPreset[],
    presetHeads = {} as Record<string, string>,
    presetCapes = {} as Record<string, string>,
    browser = [] as BrowserEntry[],
    browserLoading = false,
    settingsResults = [] as SettingDef[],
    grpTitleKeys = {} as Record<string, string>,
    actions = [] as SearchAction[],
    showWardrobe = true,
    onclose = null as null | (() => void),
    ongopack = null as null | ((id: string) => void),
    ongopreset = null as null | ((id: string) => void),
    ongosettings = null as null | ((sd: SettingDef) => void),
    ongobrowser = null as null | ((be: BrowserEntry) => void),
    onaction = null as null | ((id: string) => void),
  }: {
    open?: boolean
    query?: string
    scope?: SearchScope
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
    actions?: SearchAction[]
    showWardrobe?: boolean
    onclose?: null | (() => void)
    ongopack?: null | ((id: string) => void)
    ongopreset?: null | ((id: string) => void)
    ongosettings?: null | ((sd: SettingDef) => void)
    ongobrowser?: null | ((be: BrowserEntry) => void)
    onaction?: null | ((id: string) => void)
  } = $props()

  const q = $derived(query.trim())
  const tokens = $derived(q.toLowerCase().split(/\s+/).filter(Boolean))
  const matchAll = (hay: string) => tokens.every((tk) => hay.includes(tk))

  // Папка збірки — показуємо в підписі, щоб було видно, де вона лежить.
  const folderOf = $derived.by(() => {
    const m = new Map<string, string>()
    for (const f of packPicker?.folderById.values() ?? []) for (const id of f.packIds) m.set(id, f.name)
    return m
  })

  const actionHits = $derived(
    !q ? actions : actions.filter((a) => matchAll(`${a.label} ${a.meta ?? ''} ${a.keywords ?? ''}`.toLowerCase())).slice(0, 6),
  )

  const show = (s: SearchScope) => scope === 'all' || scope === s
  const counts = $derived({
    packs: packs.length,
    content: browser.length,
    settings: settingsResults.length,
    wardrobe: presetResults.length,
    actions: actionHits.length,
  })
  const chips = $derived([
    { id: 'all' as const, icon: 'ti-sparkles', label: t('globalSearch.scope.all'), n: -1 },
    { id: 'packs' as const, icon: 'ti-box', label: t('globalSearch.packs'), n: counts.packs },
    { id: 'content' as const, icon: 'ti-world', label: t('globalSearch.scope.content'), n: counts.content },
    { id: 'settings' as const, icon: 'ti-settings', label: t('globalSearch.settings'), n: counts.settings },
    ...(showWardrobe ? [{ id: 'wardrobe' as const, icon: 'ti-shirt', label: t('globalSearch.wardrobe'), n: counts.wardrobe }] : []),
    { id: 'actions' as const, icon: 'ti-bolt', label: t('globalSearch.scope.actions'), n: counts.actions },
  ])

  const nothing = $derived(
    !!q && !browserLoading &&
    (!show('packs') || packs.length === 0) && (!show('wardrobe') || presetResults.length === 0) &&
    (!show('settings') || settingsResults.length === 0) && (!show('content') || browser.length === 0) &&
    (!show('actions') || actionHits.length === 0),
  )

  // ── Недавні запити ────────────────────────────────────────────────────
  const RECENT_KEY = 'gs.recent'
  const readRecent = (): string[] => { try { const v = JSON.parse(localStorage.getItem(RECENT_KEY) ?? '[]'); return Array.isArray(v) ? v.filter((x) => typeof x === 'string').slice(0, 6) : [] } catch { return [] } }
  let recent = $state<string[]>([])
  $effect(() => { if (open) recent = readRecent() })
  function remember() {
    if (q.length < 2) return
    const next = [q, ...readRecent().filter((r) => r.toLowerCase() !== q.toLowerCase())].slice(0, 6)
    try { localStorage.setItem(RECENT_KEY, JSON.stringify(next)) } catch { /* не критично */ }
  }
  function clearRecent() { recent = []; try { localStorage.removeItem(RECENT_KEY) } catch { /* ignore */ } }
  const go = <A extends unknown[]>(fn: null | ((...a: A) => void), ...a: A) => () => { remember(); fn?.(...a) }

  // ── Підсвітка збігу в назві ───────────────────────────────────────────
  function parts(text: string): { s: string; hit: boolean }[] {
    if (!tokens.length || !text) return [{ s: text, hit: false }]
    const low = text.toLowerCase()
    const marks = new Array(text.length).fill(false)
    for (const tk of tokens) { let i = low.indexOf(tk); while (i >= 0) { for (let j = i; j < i + tk.length; j++) marks[j] = true; i = low.indexOf(tk, i + tk.length) } }
    const out: { s: string; hit: boolean }[] = []
    for (let i = 0; i < text.length; i++) {
      const last = out[out.length - 1]
      if (last && last.hit === marks[i]) last.s += text[i]
      else out.push({ s: text[i], hit: marks[i] })
    }
    return out
  }

  // ── Клавіатура: ↑/↓ по результатах, Enter — відкрити ──────────────────
  let resultsEl = $state<HTMLElement>()
  let active = $state(0)
  const navItems = () => [...(resultsEl?.querySelectorAll<HTMLElement>('[data-gs-nav]') ?? [])]
  $effect(() => { void q; void scope; active = 0 })
  $effect(() => {
    void active; void q; void scope; void packs; void browser; void settingsResults; void presetResults
    void tick().then(() => {
      const items = navItems()
      items.forEach((el, i) => el.classList.toggle('kb', i === active))
      items[active]?.scrollIntoView({ block: 'nearest' })
    })
  })
  function onKey(e: KeyboardEvent) {
    if (e.key === 'Escape') { onclose?.(); return }
    const items = navItems()
    if (e.key === 'ArrowDown') { e.preventDefault(); if (items.length) active = (active + 1) % items.length }
    else if (e.key === 'ArrowUp') { e.preventDefault(); if (items.length) active = (active - 1 + items.length) % items.length }
    else if (e.key === 'Enter') { e.preventDefault(); items[Math.min(active, items.length - 1)]?.click() }
    else if (e.key === 'Tab' && !e.shiftKey && chips.length) {
      e.preventDefault()
      const i = chips.findIndex((c) => c.id === scope)
      scope = chips[(i + 1) % chips.length].id
    }
  }
</script>

{#snippet hl(text: string)}{#each parts(text) as p}{#if p.hit}<mark>{p.s}</mark>{:else}{p.s}{/if}{/each}{/snippet}

{#if open}
  <Portal>
<div class="modal-overlay search-overlay" onclick={onclose}>
    <div class="global-search" onclick={(e: any) => e.stopPropagation()} role="dialog" aria-label={t('topbar.search')}>
      <div class="gs-input-row">
        <i class="ti ti-search"></i>
        <input autofocus placeholder={t('globalSearch.placeholder')} bind:value={query} onkeydown={onKey}
               role="combobox" aria-expanded="true" aria-controls="gs-results" aria-autocomplete="list" />
        {#if query}
          <button class="gs-close" use:launcherTip={t('cancel')} aria-label={t('cancel')} onclick={() => (query = '')}><i class="ti ti-backspace"></i></button>
        {/if}
        <kbd>Esc</kbd>
        <button class="gs-close" use:launcherTip={t('win.close')} aria-label={t('win.close')} onclick={onclose}>
          <i class="ti ti-x"></i>
        </button>
      </div>

      <div class="gs-chips" role="tablist" aria-label={t('globalSearch.scope.label')}>
        {#each chips as c (c.id)}
          <button class="gs-chip" role="tab" aria-selected={scope === c.id} class:on={scope === c.id} onclick={() => (scope = c.id)}>
            <i class="ti {c.icon}"></i>{c.label}{#if q && c.n > 0}<b>{c.n}</b>{/if}
          </button>
        {/each}
      </div>

      <div class="gs-results" id="gs-results" bind:this={resultsEl}>
      {#if !q}
        {#if recent.length > 0}
          <div class="gs-group">
            <div class="gs-group-title"><i class="ti ti-history"></i> {t('globalSearch.recent')}
              <button class="gs-link" onclick={clearRecent}>{t('globalSearch.clearRecent')}</button>
            </div>
            <div class="gs-recent">
              {#each recent as r (r)}
                <button class="gs-recent-chip" data-gs-nav onclick={() => (query = r)}><i class="ti ti-search"></i>{r}</button>
              {/each}
            </div>
          </div>
        {/if}
        <div class="gs-group">
          <div class="gs-group-title"><i class="ti ti-bolt"></i> {t('globalSearch.quick')}</div>
          <div class="gs-quick">
            {#each actions as a (a.id)}
              <button class="gs-quick-item" data-gs-nav onclick={() => onaction?.(a.id)}>
                <span class="gs-ic sm"><i class="ti {a.icon}"></i></span><span>{a.label}</span>
              </button>
            {/each}
          </div>
        </div>
      {:else}
        {#if show('actions') && actionHits.length > 0}
          <div class="gs-group">
            <div class="gs-group-title"><i class="ti ti-bolt"></i> {t('globalSearch.scope.actions')}<span class="gs-count">{actionHits.length}</span></div>
            {#each actionHits as a (a.id)}
              <div class="gs-item" data-gs-nav role="option" aria-selected="false" tabindex="-1" onclick={go(onaction, a.id)}>
                <div class="gs-ic"><i class="ti {a.icon}"></i></div>
                <div class="gs-name">{@render hl(a.label)}</div>
                {#if a.meta}<div class="gs-meta">{a.meta}</div>{/if}
                <i class="ti ti-corner-down-left gs-enter"></i>
              </div>
            {/each}
          </div>
        {/if}
        {#if show('packs') && packs.length > 0}
          <div class="gs-group">
            <div class="gs-group-title"><i class="ti ti-box"></i> {t('globalSearch.packs')}<span class="gs-count">{packs.length}</span></div>
            {#each packs as bv (bv.build.id)}
              {@const sic = packAsset(bv.build.iconUrl)}
              {@const folder = folderOf.get(bv.build.id)}
              <div class="gs-item" data-gs-nav role="option" aria-selected="false" tabindex="-1" onclick={go(ongopack, bv.build.id)}>
                <div class="gs-ic" class:has-icon={!!sic} style="--pcol:{bv.build.color || 'var(--orange)'}">
                  {#if sic}<img src={sic} alt="" loading="eager"/>{:else}<i class="ti {bv.build.icon || 'ti-puzzle'}"></i>{/if}
                </div>
                <div class="gs-text">
                  <div class="gs-name">{@render hl(bv.build.name)}</div>
                  <div class="gs-sub">{@render hl(`${bv.build.loaderType || 'vanilla'} · ${bv.build.mcVersion}`)}{#if folder} · <i class="ti ti-folder"></i> {folder}{/if}</div>
                </div>
                <i class="ti ti-corner-down-left gs-enter"></i>
              </div>
            {/each}
          </div>
        {/if}
        {#if show('wardrobe') && presetResults.length > 0}
          <div class="gs-group">
            <div class="gs-group-title"><i class="ti ti-shirt"></i> {t('globalSearch.wardrobe')}<span class="gs-count">{presetResults.length}</span></div>
            {#each presetResults as p (p.id)}
              <div class="gs-item" data-gs-nav role="option" aria-selected="false" tabindex="-1" onclick={go(ongopreset, p.id)}>
                <div class="gs-wardrobe">
                  {#if presetHeads[p.id]}<span class="gs-head" style="background-image:url('{presetHeads[p.id]}')"></span>{/if}
                  {#if presetCapes[p.id]}<span class="gs-cape" style="background-image:url('{presetCapes[p.id]}')"></span>{/if}
                </div>
                <div class="gs-text">
                  <div class="gs-name">{@render hl(p.name)}</div>
                  <div class="gs-sub">{p.slimArms ? t('wardrobe.slim') : t('wardrobe.classic')}{p.capePath ? ' · ' + t('wardrobe.withCape') : ''}</div>
                </div>
                <i class="ti ti-corner-down-left gs-enter"></i>
              </div>
            {/each}
          </div>
        {/if}
        {#if show('settings') && settingsResults.length > 0}
          <div class="gs-group">
            <div class="gs-group-title"><i class="ti ti-settings"></i> {t('globalSearch.settings')}<span class="gs-count">{settingsResults.length}</span></div>
            {#each settingsResults as sd (sd.key)}
              <div class="gs-item" data-gs-nav role="option" aria-selected="false" tabindex="-1" onclick={go(ongosettings, sd)}>
                <div class="gs-ic"><i class="ti ti-adjustments"></i></div>
                <div class="gs-text">
                  <div class="gs-name">{@render hl(t(sd.label))}</div>
                  <div class="gs-sub">{t('nav.settings')} › {t(grpTitleKeys[sd.group] ?? 'settings.tab.general')}</div>
                </div>
                <i class="ti ti-corner-down-left gs-enter"></i>
              </div>
            {/each}
          </div>
        {/if}
        {#if show('content') && q.length >= 2}
          <div class="gs-group">
            <div class="gs-group-title"><i class="ti ti-world"></i> {t('globalSearch.browser')}
              {#if browserLoading}<span class="gs-spinner"></span>{/if}
              {#if browser.length > 0}<span class="gs-count">{browser.length}</span>{/if}
            </div>
            {#if browserLoading && browser.length === 0}
              {#each [0, 1, 2] as i (i)}<div class="gs-item gs-skel" aria-hidden="true"><div class="gs-ic"></div><div class="gs-text"><span></span><span></span></div></div>{/each}
            {:else if browser.length === 0}
              <div class="gs-empty small">{t('globalSearch.browserEmpty')}</div>
            {/if}
            {#each browser as be (be.source + be.id)}
              <div class="gs-item" data-gs-nav role="option" aria-selected="false" tabindex="-1" onclick={go(ongobrowser, be)}>
                <div class="gs-ic" class:has-icon={!!be.iconUrl}>
                  {#if be.iconUrl}<img src={be.iconUrl} alt="" loading="lazy"/>{:else}<i class="ti ti-package"></i>{/if}
                </div>
                <div class="gs-text">
                  <div class="gs-name">{@render hl(be.name)}</div>
                  <div class="gs-sub">{be.type}{be.mcVersion ? ' · ' + be.mcVersion : ''}{be.source ? ' · ' + be.source : ''}</div>
                </div>
                <i class="ti ti-corner-down-left gs-enter"></i>
              </div>
            {/each}
          </div>
        {/if}
        {#if nothing}
          <div class="gs-empty"><i class="ti ti-mood-empty"></i> {t('globalSearch.noResults')}
            {#if scope !== 'all'}<button class="btn btn-ghost btn-sm" onclick={() => (scope = 'all')}>{t('globalSearch.searchEverywhere')}</button>{/if}
          </div>
        {/if}
      {/if}
      </div>

      <div class="gs-tips">
        <span><kbd>↑</kbd><kbd>↓</kbd> {t('globalSearch.tip.move')}</span>
        <span><kbd>Enter</kbd> {t('globalSearch.tip.open')}</span>
        <span><kbd>Tab</kbd> {t('globalSearch.tip.scope')}</span>
      </div>
    </div>
  </div>
</Portal>
{/if}

<style>
  /* Глобальний пошук: над звичайними модалками, притиснутий до верху. */
  .search-overlay { z-index:1000001; align-items:flex-start; padding-top:min(12vh, 110px); }
  .global-search {
    position:relative;
    width:min(720px, 94vw); height:min(560px, 76vh);
    display:flex; flex-direction:column; overflow:hidden;
    background:
      radial-gradient(120% 70px at 50% 0, rgba(var(--orange-rgb), .10), transparent 70%),
      var(--bg-panel);
    border:1px solid var(--border-hi);
    border-radius:20px;
    box-shadow:0 30px 90px rgba(0,0,0,.55), 0 0 0 1px rgba(255,255,255,.03) inset;
    animation:gs-in .26s cubic-bezier(.16,1,.3,1) both;
  }
  @keyframes gs-in { from { opacity:0; transform:translateY(-10px) scale(.985); } to { opacity:1; transform:none; } }

  /* Рядок вводу */
  .gs-input-row { display:flex; align-items:center; gap:12px; padding:16px 18px 12px 22px; flex:0 0 auto; }
  .gs-input-row .ti-search { font-size:20px; color:var(--orange); flex:none; filter:drop-shadow(0 0 8px rgba(var(--orange-rgb), .45)); }
  .gs-input-row input {
    flex:1; min-width:0; border:none; background:transparent; outline:none; padding:4px 0;
    font:600 17px/1.3 var(--font-ui); color:var(--text);
  }
  .gs-input-row input::placeholder { color:var(--text-mute); font-weight:500; }
  kbd {
    flex:none; display:inline-grid; place-items:center; min-width:22px; height:22px; padding:0 7px;
    font:700 11px/1 var(--font-mono); color:var(--text-dim);
    background:var(--bg-input); border:1px solid var(--border); border-bottom-width:2px; border-radius:6px;
  }
  .gs-close {
    flex:none; width:30px; height:30px; border:none; border-radius:9px;
    background:transparent; color:var(--text-mute); cursor:pointer;
    display:flex; align-items:center; justify-content:center; font-size:16px;
    transition:background .15s ease, color .15s ease;
  }
  .gs-close:hover { background:var(--bg-input-hi); color:var(--text); }

  /* Фільтр-чипи: єдина роздільна лінія під блоком шапки. */
  .gs-chips {
    display:flex; gap:8px; flex:0 0 auto;
    padding:4px 22px 14px; border-bottom:1px solid var(--border);
    overflow-x:auto; overflow-y:hidden; scrollbar-width:none;
  }
  .gs-chips::-webkit-scrollbar { display:none; }
  .gs-chip {
    flex:none; display:inline-flex; align-items:center; gap:7px; height:32px; padding:0 14px; border-radius:999px;
    border:1px solid var(--border); background:transparent; color:var(--text-dim);
    font:700 12.5px/1 var(--font-ui); cursor:pointer; white-space:nowrap;
    transition:color .15s, border-color .15s, background .15s, transform .12s;
  }
  .gs-chip i { font-size:14px; }
  .gs-chip b { min-width:18px; padding:3px 6px; border-radius:999px; background:var(--hover-strong); color:var(--text); font:800 10px/1 var(--font-mono); text-align:center; }
  .gs-chip:hover { color:var(--text); border-color:var(--border-hi); background:var(--bg-input); }
  .gs-chip:active { transform:scale(.97); }
  .gs-chip.on { color:var(--orange); border-color:rgba(var(--orange-rgb), .55); background:rgba(var(--orange-rgb), .12); }
  .gs-chip.on b { background:rgba(var(--orange-rgb), .22); color:var(--orange); }

  /* Результати */
  .gs-results { overflow-y:auto; flex:1 1 auto; min-height:0; padding:8px 12px 12px; }
  .gs-group { padding:2px 0 6px; }
  .gs-group-title {
    display:flex; align-items:center; gap:8px; padding:12px 10px 6px;
    font-size:11px; font-weight:800; text-transform:uppercase; letter-spacing:.08em; color:var(--text-mute);
  }
  .gs-group-title .ti { font-size:14px; color:var(--orange); }
  .gs-count { margin-left:auto; padding:2px 7px; border-radius:999px; background:var(--hover); font:800 10px/1.2 var(--font-mono); color:var(--text-mute); letter-spacing:0; }
  .gs-link { margin-left:auto; border:0; background:none; color:var(--text-mute); font:600 11px var(--font-ui); text-transform:none; letter-spacing:0; cursor:pointer; }
  .gs-link:hover { color:var(--orange); }
  .gs-spinner { width:13px; height:13px; border-radius:50%; flex:none; border:2px solid var(--border-hi); border-top-color:var(--orange); animation:gs-spin .7s linear infinite; }
  @keyframes gs-spin { to { transform:rotate(360deg); } }

  .gs-item {
    position:relative; display:flex; align-items:center; gap:13px; padding:9px 12px; border-radius:12px;
    border:1px solid transparent; cursor:pointer; transition:background .12s ease, border-color .12s ease;
  }
  .gs-item:hover { background:var(--hover); }
  .gs-item:global(.kb) { background:rgba(var(--orange-rgb), .10); border-color:rgba(var(--orange-rgb), .35); }
  .gs-enter { margin-left:auto; color:var(--orange); font-size:15px; opacity:0; transform:translateX(-4px); transition:opacity .15s, transform .15s; }
  .gs-item:global(.kb) .gs-enter { opacity:1; transform:none; }

  /* Іконка результату: м'яка плитка без обведення. */
  .gs-ic {
    width:38px; height:38px; border-radius:11px; flex:none; overflow:hidden;
    display:flex; align-items:center; justify-content:center; font-size:19px;
    color:var(--pcol, var(--orange));
    background:color-mix(in srgb, var(--pcol, var(--orange)) 16%, var(--bg-input));
  }
  .gs-ic.sm { width:32px; height:32px; border-radius:10px; font-size:17px; }
  .gs-ic.has-icon { background:var(--bg-input); }
  .gs-ic img { width:100%; height:100%; object-fit:cover; display:block; }
  .gs-text { flex:1 1 auto; min-width:0; display:flex; flex-direction:column; gap:3px; }
  .gs-name { flex:1 1 auto; min-width:0; font-size:14px; font-weight:650; color:var(--text); white-space:nowrap; overflow:hidden; text-overflow:ellipsis; }
  .gs-text .gs-name { flex:none; }
  .gs-sub { min-width:0; font-size:12px; color:var(--text-dim); white-space:nowrap; overflow:hidden; text-overflow:ellipsis; }
  .gs-sub i { font-size:12px; vertical-align:-1px; }
  .gs-meta { flex:none; font-size:12px; color:var(--text-dim); white-space:nowrap; }
  mark { background:rgba(var(--orange-rgb), .24); color:var(--text); border-radius:3px; padding:0 1px; }

  /* Порожній запит: недавні + швидкий перехід */
  .gs-recent { display:flex; flex-wrap:wrap; gap:8px; padding:2px 10px 8px; }
  .gs-recent-chip { display:inline-flex; align-items:center; gap:7px; height:30px; padding:0 13px; border-radius:999px; border:1px solid var(--border); background:var(--bg-input); color:var(--text); font:600 12.5px var(--font-ui); cursor:pointer; transition:border-color .15s, color .15s; }
  .gs-recent-chip i { font-size:13px; color:var(--text-dim); }
  .gs-recent-chip:hover, .gs-recent-chip:global(.kb) { border-color:rgba(var(--orange-rgb), .5); color:var(--orange); }
  .gs-quick { display:grid; grid-template-columns:repeat(3, minmax(0, 1fr)); gap:8px; padding:2px 4px 6px; }
  @media (max-width:620px) { .gs-quick { grid-template-columns:repeat(2, minmax(0, 1fr)); } }
  .gs-quick-item {
    display:flex; align-items:center; gap:11px; min-width:0; padding:10px 12px;
    border:1px solid var(--border); border-radius:13px; background:var(--bg-input);
    color:var(--text); font:650 13px var(--font-ui); text-align:left; cursor:pointer;
    transition:background .14s, border-color .14s, transform .12s;
  }
  .gs-quick-item > span:last-child { min-width:0; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
  .gs-quick-item:hover { background:var(--bg-input-hi); border-color:var(--border-hi); }
  .gs-quick-item:active { transform:scale(.98); }
  .gs-quick-item:global(.kb) { background:rgba(var(--orange-rgb), .10); border-color:rgba(var(--orange-rgb), .45); }

  /* Підказки клавіатури: завжди внизу, не прокручуються. */
  .gs-tips {
    flex:0 0 auto; display:flex; flex-wrap:wrap; align-items:center; gap:18px; padding:11px 22px;
    border-top:1px solid var(--border); background:color-mix(in srgb, var(--bg-input) 45%, transparent);
    font-size:12px; color:var(--text-dim);
  }
  .gs-tips span { display:inline-flex; align-items:center; gap:6px; }
  .gs-tips kbd { min-width:20px; height:20px; font-size:10.5px; }

  .gs-empty { padding:40px 18px; text-align:center; color:var(--text-dim); font-size:13.5px; display:flex; flex-direction:column; align-items:center; gap:12px; }
  .gs-empty .ti { font-size:30px; color:var(--text-mute); }
  .gs-empty.small { padding:12px 10px; font-size:12px; }

  /* Скелетон, поки йде мережевий пошук */
  .gs-skel { cursor:default; }
  .gs-skel:hover { background:transparent; }
  .gs-skel .gs-ic { background:var(--hover-strong); }
  .gs-skel .gs-text span { display:block; height:9px; border-radius:5px; background:var(--hover-strong); animation:gs-pulse 1.1s ease-in-out infinite alternate; }
  .gs-skel .gs-text span:first-child { width:46%; }
  .gs-skel .gs-text span:last-child { width:28%; opacity:.7; }
  @keyframes gs-pulse { from { opacity:.45; } to { opacity:1; } }

  @media (prefers-reduced-motion: reduce) { .global-search { animation-duration:.01s; } }

  /* .gs-wardrobe/.gs-head/.gs-cape — глобальні, у style.css */
</style>
