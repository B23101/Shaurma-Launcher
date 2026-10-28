<script lang="ts">
  // PackPicker — спільний рендер списку збірок з ПАПКАМИ для всіх меню
  // вибору (фільтр «Збірка» в браузері, «Копіювати в іншу збірку»,
  // вибір цільової збірки тощо). Папки розкриваються/згортаються як у
  // sidebar; збірки поза папками йдуть звичайними рядками; папка може
  // стояти МІЖ збірками (єдиний порядок — передається в `items`).
  //
  // Контракт: `items` — впорядкований список (збірки + папки), `rowById` —
  // мапа id → {name, color, iconUrl, icon, loader, mcVersion}, `folderById` —
  // мапа id → {name, color, packIds}. Клік по збірці → onselect(id), по
  // заголовку папки → розкрити/згорнути. Слот actions(packId) — кнопки
  // суто для конкретного меню; слота немає — просто вибір.
  import AnimatedIcon from './AnimatedIcon.svelte'
  import { packAsset } from './packAssets.svelte'
  import { smartImage } from './smartImage'

  export interface PackRow {
    name: string
    color?: string
    iconUrl?: string
    icon?: string
    loader?: string
    mcVersion?: string
  }
  export interface PackFolderRow {
    name: string
    color?: string
    packIds: string[]
  }
  export interface PackPickerItem { kind: 'pack' | 'folder'; id: string }

  let {
    items = [] as PackPickerItem[],
    rowById = new Map() as Map<string, PackRow>,
    folderById = new Map() as Map<string, PackFolderRow>,
    selected = '',
    onselect = (_id: string) => {},
    // Мульти-режим (вибір кількох збірок, напр. у папку): замість
    // selected/onselect використовуються selectedIds (множина) і ontoggle.
    multi = false,
    selectedIds = null as null | Set<string>,
    ontoggle = (_id: string) => {},
    // Пошук-фільтр: рядки, що не збігаються, ховаються; папка показується
    // лише якщо в ній є збіги (і авто-розкривається).
    query = '',
    // Підпис під назвою збірки (за замовчуванням loader · mcVersion).
    // Отримує рядок збірки + її id (id немає в PackRow — він живе ключем
    // у rowById), щоб зовнішні підписи могли звертатись до per-pack стану.
    subtitle = (_r: PackRow, _id: string) => (_r.loader && _r.mcVersion ? `${_r.loader} · ${_r.mcVersion}` : (_r.mcVersion || _r.loader || '')),
    // Додаткові кнопки-дії в рядку збірки (контекст меню) — Svelte-snippet,
    // передається ззовні: {#snippet rowActions(pid)}…{/snippet}.
    rowActions = null as null | ((packId: string) => any),
    // Порожній стан (немає збірок взагалі).
    emptyText = '',
    // Чи можна клікати по збірці для вибору (якщо false — лише слоти-дії).
    selectable = true,
    // Підмножина ВИДИМИХ збірок (напр. лише сумісні з конкретною версією
    // контенту). Рядки поза множиною ховаються; папка показується лише
    // якщо в ній є видимі збірки. null = всі.
    allowedIds = null as null | Set<string>,
    // CSS-клас контейнера списку (для перевикористання стилів меню).
    class: cls = '',
  }: {
    items?: PackPickerItem[]
    rowById?: Map<string, PackRow>
    folderById?: Map<string, PackFolderRow>
    selected?: string
    onselect?: (id: string) => void
    multi?: boolean
    selectedIds?: Set<string> | null
    ontoggle?: (id: string) => void
    query?: string
    subtitle?: (r: PackRow, id: string) => string
    rowActions?: ((packId: string) => any) | null
    emptyText?: string
    selectable?: boolean
    allowedIds?: Set<string> | null
    class?: string
  } = $props()

  // Розкриті папки — локальний стан цього списку (не глобальний openFolderIds).
  let openIds = $state<string[]>([])
  function toggle(id: string) {
    openIds = openIds.includes(id) ? openIds.filter(x => x !== id) : [...openIds, id]
  }

  const q = $derived(query.trim().toLowerCase())

  // Чи збігається збірка з пошуком (назва + loader/версія) і чи входить
  // у дозволену підмножину (allowedIds).
  function matches(id: string, r: PackRow | undefined): boolean {
    if (!r) return false
    if (allowedIds && !allowedIds.has(id)) return false
    if (!q) return true
    const hay = `${r.name} ${r.loader ?? ''} ${r.mcVersion ?? ''}`.toLowerCase()
    return hay.includes(q)
  }

  // Видимі рядки: папки з хоч одним збігом (авто-розкриті), збірки-збіги.
  const visible = $derived.by((): (PackPickerItem & { autoOpen?: boolean })[] => {
    if (!q && !allowedIds) return items
    const out: (PackPickerItem & { autoOpen?: boolean })[] = []
    for (const it of items) {
      if (it.kind === 'folder') {
        const f = folderById.get(it.id)
        if (!f) continue
        const has = f.packIds.some(pid => matches(pid, rowById.get(pid)))
        if (has) out.push({ kind: 'folder', id: it.id, autoOpen: true })
      } else {
        if (matches(it.id, rowById.get(it.id))) out.push(it)
      }
    }
    return out
  })

  // Для пошуку папки авто-розкриваються (видимі збірки всередині).
  const effectiveOpen = $derived.by((): Set<string> => {
    const s = new Set(openIds)
    if (q || allowedIds) {
      for (const it of visible) {
        if (it.kind === 'folder' && it.autoOpen) s.add(it.id)
      }
    }
    return s
  })

  const hasAny = $derived(
    items.some(it => it.kind === 'pack' ? matches(it.id, rowById.get(it.id)) : !!folderById.get(it.id)?.packIds.some(pid => matches(pid, rowById.get(pid))))
  )

  // Мульти-режим: чи входить збірка до вибраних (selectedIds).
  function isSel(id: string): boolean {
    return multi ? (selectedIds?.has(id) ?? false) : selected === id
  }

  // Клік по рядку: multi — toggle, інакше — звичайний onselect.
  function handleRowClick(id: string) {
    if (!selectable) return
    if (multi) ontoggle(id)
    else onselect(id)
  }

  function iconFor(r: PackRow) {
    return packAsset(r.iconUrl)
  }
</script>

<div class="pp-list {cls}">
  {#if !hasAny}
    {#if emptyText}
      <div class="pp-empty"><i class="ti ti-search-off"></i> {emptyText}</div>
    {/if}
  {:else}
    {#each visible as item (item.kind === 'folder' ? 'fld:' + item.id : 'pk:' + item.id)}
      {#if item.kind === 'folder'}
        {@const f = folderById.get(item.id)}
        {#if f}
          {@const fpcol = f.color || 'var(--orange)'}
          {@const isOpen = effectiveOpen.has(item.id)}
          {@const visiblePacks = (q || allowedIds)
            ? f.packIds.filter(pid => matches(pid, rowById.get(pid)))
            : f.packIds}
          {@const fIcons = f.packIds.slice(0, 3).map((pid) => {
            const r = rowById.get(pid)
            const ic = r ? iconFor(r) : undefined
            return { src: ic, paused: false, pcol: r?.color || fpcol, preset: r?.icon || 'ti-puzzle' }
          })}
          <div class="pp-folder" class:open={isOpen} style="--pcol:{fpcol}">
            <div class="pp-folder-head" onclick={() => toggle(item.id)} title={f.name}>
              <div class="pp-stack">
                {#if f.packIds.length === 0}
                  <span class="pp-stack-fallback pp-stack-empty" style="--pcol:{fpcol}"><i class="ti ti-folder"></i></span>
                {:else}
                  {#each fIcons as fic, fii (fii)}
                    {#if fic.src}
                      <AnimatedIcon src={fic.src} paused={fic.paused} alt="" />
                    {:else}
                      <span class="pp-stack-fallback" style="--pcol:{fic.pcol}"><i class="ti {fic.preset}"></i></span>
                    {/if}
                  {/each}
                  {#if f.packIds.length > 3}
                    <span class="pp-more" style="--pcol:{fpcol}">+{f.packIds.length - 3}</span>
                  {/if}
                {/if}
              </div>
              <div class="pp-folder-meta">
                <div class="pp-folder-name">{f.name}</div>
                <div class="pp-folder-count">{visiblePacks.length}</div>
              </div>
              <i class="ti ti-chevron-down pp-folder-arrow"></i>
            </div>
            {#if isOpen}
              <div class="pp-packs">
                <div class="pp-packs-inner">
                {#each visiblePacks as pid (pid)}
                  {@const r = rowById.get(pid)}
                  {#if r}
                    {@const pcol = r.color || 'var(--orange)'}
                    {@const ic = iconFor(r)}
                    {@const sel = isSel(pid)}
                    <div
                      class="pp-row pp-in-folder"
                      class:sel={sel}
                      style="--pcol:{pcol}"
                      role={selectable ? 'button' : undefined}
                      onclick={() => handleRowClick(pid)}
                      tabindex={selectable ? 0 : -1}
                      onkeydown={(e) => { if (selectable && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); handleRowClick(pid) } }}
                    >
                      <span class="pp-ic" class:has-icon={!!ic}>
                        {#if ic}
                          <img src={ic} alt="" loading="lazy" use:smartImage />
                        {:else}
                          <i class="ti {r.icon || 'ti-puzzle'}"></i>
                        {/if}
                      </span>
                      <span class="pp-info">
                        <span class="pp-name">{r.name}</span>
                        <span class="pp-sub">{subtitle(r, pid)}</span>
                      </span>
                      {#if rowActions}
                        <span class="pp-actions" onclick={(e: any) => e.stopPropagation()}>
                          {@render rowActions(pid)}
                        </span>
                      {/if}
                      {#if selectable && sel}
                        <i class="ti ti-check pp-check"></i>
                      {/if}
                    </div>
                  {/if}
                {/each}
                </div>
              </div>
            {/if}
          </div>
        {/if}
      {:else}
        {@const r = rowById.get(item.id)}
        {#if r}
          {@const pcol = r.color || 'var(--orange)'}
          {@const ic = iconFor(r)}
          {@const sel = isSel(item.id)}
          <div
            class="pp-row"
            class:sel={sel}
            style="--pcol:{pcol}"
            role={selectable ? 'button' : undefined}
            onclick={() => handleRowClick(item.id)}
            tabindex={selectable ? 0 : -1}
            onkeydown={(e) => { if (selectable && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); handleRowClick(item.id) } }}
          >
            <span class="pp-ic" class:has-icon={!!ic}>
              {#if ic}
                <img src={ic} alt="" loading="lazy" use:smartImage />
              {:else}
                <i class="ti {r.icon || 'ti-puzzle'}"></i>
              {/if}
            </span>
            <span class="pp-info">
              <span class="pp-name">{r.name}</span>
              <span class="pp-sub">{subtitle(r, item.id)}</span>
            </span>
            {#if rowActions}
              <span class="pp-actions" onclick={(e: any) => e.stopPropagation()}>
                {@render rowActions(item.id)}
              </span>
            {/if}
            {#if selectable && sel}
              <i class="ti ti-check pp-check"></i>
            {/if}
          </div>
        {/if}
      {/if}
    {/each}
  {/if}
</div>

<style>
  .pp-list { display: flex; flex-direction: column; gap: 2px; }
  .pp-empty {
    display: flex; align-items: center; justify-content: center; gap: 8px;
    padding: 22px 0; color: var(--text-mute); font-size: 12px;
  }
  .pp-empty i { font-size: 16px; }

  /* ── Папка (акордеон, як sidebar) ── */
  .pp-folder {
    border: 1px solid transparent; border-radius: var(--r-md);
    margin: 1px 0; transition: border-color .18s, background .18s;
  }
  .pp-folder:hover { background: var(--hover); }
  .pp-folder.open { background: color-mix(in srgb, var(--pcol, var(--orange)) 6%, var(--hover)); }
  .pp-folder-head {
    display: flex; align-items: center; gap: 8px;
    padding: 6px 7px; cursor: pointer; border-radius: var(--r-md);
    user-select: none;
  }
  .pp-stack { position: relative; width: 30px; height: 30px; flex: 0 0 30px; }
  /* :global(img) — іконки в шапці папки рендеряться дочірнім AnimatedIcon,
     тому звичайний скоупований селектор .pp-stack img до них не доходить
     (у них scope-клас AnimatedIcon, не PackPicker). Без global кап розміру
     не спрацьовує і 512+ px PNG розтягує рядок. */
  .pp-stack :global(img), .pp-stack-fallback {
    position: absolute; width: 22px; height: 22px; border-radius: 6px;
    object-fit: cover; border: 1px solid var(--border-hi); background: var(--bg-card);
    left: 0; top: 0;
    /* Жорсткий кап розміру PNG-іконки: картинка з величезним внутрішнім
       розміром (512+ px) ніколи не виходить за межі іконки. */
    max-width: 22px; max-height: 22px;
  }
  .pp-stack :global(img:nth-child(2)), .pp-stack-fallback:nth-child(2) { left: 6px; top: 4px; }
  .pp-stack :global(img:nth-child(3)), .pp-stack-fallback:nth-child(3) { left: 12px; top: 8px; }
  .pp-stack-fallback {
    display: flex; align-items: center; justify-content: center;
    font-size: 13px; color: var(--pcol, var(--orange));
  }
  .pp-stack-fallback i { font-size: 13px; }
  .pp-stack .pp-stack-empty {
    left: 4px; top: 4px; width: 22px; height: 22px;
    background: color-mix(in srgb, var(--pcol, var(--orange)) 14%, var(--bg-card));
    border-color: color-mix(in srgb, var(--pcol, var(--orange)) 35%, var(--border-hi));
  }
  .pp-more {
    position: absolute; right: -2px; bottom: -2px; min-width: 15px; height: 15px;
    border-radius: 8px; background: var(--pcol, var(--orange)); color: #fff;
    font-size: var(--fs-min); font-weight: 800; display: flex; align-items: center;
    justify-content: center; padding: 0 3px; box-shadow: 0 2px 5px rgba(0,0,0,.4);
  }
  .pp-folder-meta { flex: 1; min-width: 0; }
  .pp-folder-name {
    font-size: 12px; font-weight: 700; color: var(--pcol, var(--orange));
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .pp-folder-count { font-size: var(--fs-min); color: var(--text-mute); }
  .pp-folder-arrow {
    font-size: 14px; color: var(--text-mute); flex-shrink: 0;
    transition: transform .22s cubic-bezier(.34,1.56,.64,1);
  }
  .pp-folder.open .pp-folder-arrow { transform: rotate(180deg); }

  /* ── Збірки всередині папки. Плавне розкриття: grid-template-rows
       0fr → 1fr анімує висоту без JS (висота контенту невідома). */
  .pp-packs {
    display: grid; grid-template-rows: 0fr;
    transition: grid-template-rows .28s cubic-bezier(.22,1,.36,1);
  }
  .pp-folder.open .pp-packs { grid-template-rows: 1fr; }
  .pp-packs-inner {
    min-height: 0; overflow: hidden;
    display: flex; flex-direction: column; gap: 2px;
    padding: 0 4px 4px 12px;
    border-left: 2px solid color-mix(in srgb, var(--pcol, var(--orange)) 35%, transparent);
    margin-left: 14px;
  }
  @media (prefers-reduced-motion: reduce) {
    .pp-packs { transition: none; }
  }

  /* ── Рядок збірки ── */
  .pp-row {
    display: flex; align-items: center; gap: 9px;
    padding: 6px 8px; border-radius: var(--r-md);
    cursor: pointer; user-select: none;
    transition: background .15s, box-shadow .15s;
  }
  .pp-row:hover { background: var(--hover); }
  .pp-row.sel {
    background: color-mix(in srgb, var(--pcol, var(--orange)) 12%, var(--hover));
    box-shadow: inset 2px 0 0 0 var(--pcol, var(--orange));
  }
  .pp-ic {
    width: 28px; height: 28px; min-width: 28px; border-radius: 7px;
    display: flex; align-items: center; justify-content: center;
    background: var(--bg-input); overflow: hidden;
  }
  .pp-ic img {
    width: 100%; height: 100%;
    max-width: 100%; max-height: 100%;
    object-fit: cover; display: block;
  }
  .pp-ic:not(.has-icon) { color: var(--pcol, var(--orange)); }
  .pp-ic i { font-size: 14px; }
  .pp-info { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
  .pp-name {
    font-size: 12.5px; font-weight: 700; color: var(--text);
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .pp-sub { font-size: var(--fs-min); color: var(--text-mute); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .pp-actions { display: flex; align-items: center; gap: 4px; flex-shrink: 0; }
  .pp-check { color: var(--pcol, var(--orange)); font-size: 15px; flex-shrink: 0; }
</style>
