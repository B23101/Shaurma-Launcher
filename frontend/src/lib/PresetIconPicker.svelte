<script lang="ts">
  // PresetIconPicker — спільний вибір пресет-іконки збірки.
  // Один компонент замість трьох дублікатів сітки іконок (PackBasicSection,
  // CreatePackPage ×2, DuplicatePackModal). Категорії перемикаються
  // ВКЛАДКАМИ (як лаунчери в LauncherMigrationModal) — активна лише ОДНА
  // категорія за раз, а не стос розгорнутих акордеонів одне під одним.
  // Є пошук (перемикає в режим «плаский список збігів по всіх категоріях»,
  // бо збіги можуть бути розкидані по кількох вкладках одразу). Усі іконки
  // перевірені на існування у tabler-icons.min.css (жодної «примарної» —
  // ti-castle видалено).
  import { t } from './i18n.svelte'

  interface Props {
    value?: string
    color?: string
    onSelect: (name: string) => void
  }
  let { value = '', color = '#ff8a00', onSelect }: Props = $props()

  // ── Категорії ──
  const categories: { id: string; label: string; icon: string; icons: string[] }[] = [
    {
      id: 'basic',
      label: t('iconPicker.cat.basic'),
      icon: 'ti-star',
      icons: ['ti-puzzle', 'ti-star', 'ti-heart', 'ti-diamond', 'ti-crown', 'ti-skull', 'ti-flame', 'ti-bolt', 'ti-sword', 'ti-shield', 'ti-axe', 'ti-target', 'ti-ghost', 'ti-alien', 'ti-robot', 'ti-brain', 'ti-cat', 'ti-dog', 'ti-bug', 'ti-rocket', 'ti-planet', 'ti-moon', 'ti-sun', 'ti-snowflake'],
    },
    {
      id: 'creatures',
      label: t('iconPicker.cat.creatures'),
      icon: 'ti-dragon',
      icons: ['ti-dragon', 'ti-fish', 'ti-spider', 'ti-paw', 'ti-bone', 'ti-feather'],
    },
    {
      id: 'nature',
      label: t('iconPicker.cat.nature'),
      icon: 'ti-leaf',
      icons: ['ti-leaf', 'ti-tree', 'ti-mushroom', 'ti-flower', 'ti-cactus', 'ti-clover'],
    },
    {
      id: 'food',
      label: t('iconPicker.cat.food'),
      icon: 'ti-pizza',
      icons: ['ti-apple', 'ti-cherry', 'ti-pizza', 'ti-coffee', 'ti-beer', 'ti-cake', 'ti-candy', 'ti-cookie', 'ti-ice-cream', 'ti-egg', 'ti-cheese', 'ti-salad'],
    },
    {
      id: 'battle',
      label: t('iconPicker.cat.battle'),
      icon: 'ti-swords',
      icons: ['ti-bow', 'ti-bomb', 'ti-droplet', 'ti-helmet', 'ti-gavel', 'ti-swords'],
    },
    {
      id: 'transport',
      label: t('iconPicker.cat.transport'),
      icon: 'ti-plane',
      icons: ['ti-plane', 'ti-car', 'ti-bike', 'ti-truck', 'ti-bus', 'ti-train', 'ti-ship', 'ti-anchor'],
    },
    {
      id: 'world',
      label: t('iconPicker.cat.world'),
      icon: 'ti-map',
      icons: ['ti-map', 'ti-compass', 'ti-key', 'ti-lock', 'ti-tower', 'ti-trophy', 'ti-medal', 'ti-award', 'ti-flag', 'ti-chess', 'ti-dice', 'ti-pokeball', 'ti-wand', 'ti-meteor', 'ti-comet', 'ti-galaxy', 'ti-space'],
    },
    {
      id: 'weather',
      label: t('iconPicker.cat.weather'),
      icon: 'ti-cloud',
      icons: ['ti-cloud', 'ti-cloud-rain', 'ti-cloud-snow', 'ti-umbrella', 'ti-sunrise', 'ti-sunset', 'ti-moon-stars'],
    },
    {
      id: 'home',
      label: t('iconPicker.cat.home'),
      icon: 'ti-home',
      icons: ['ti-home', 'ti-building', 'ti-fence', 'ti-palette', 'ti-brush', 'ti-paint', 'ti-pencil', 'ti-ruler', 'ti-calculator', 'ti-cpu', 'ti-wallet', 'ti-coins'],
    },
    {
      id: 'tech',
      label: t('iconPicker.cat.tech'),
      icon: 'ti-device-gamepad',
      icons: ['ti-device-gamepad', 'ti-device-desktop', 'ti-device-laptop', 'ti-device-mobile'],
    },
  ]

  let query = $state('')
  const lowered = $derived(query.trim().toLowerCase())
  const searching = $derived(lowered.length > 0)

  // Активна вкладка-категорія (перемикання розділів замість акордеону).
  let activeTab = $state(categories[0].id)
  const activeCategory = $derived(categories.find((c) => c.id === activeTab) ?? categories[0])

  // Пошук: збіги можуть бути розкидані по кількох категоріях одразу, тож
  // під час пошуку вкладки ховаються і показується плаский список збігів
  // (з підписом категорії на кожній іконці — title атрибут).
  const searchResults = $derived.by(() => {
    if (!searching) return []
    const out: { id: string; catLabel: string }[] = []
    for (const c of categories) {
      for (const ic of c.icons) {
        if (ic.replace('ti-', '').includes(lowered)) out.push({ id: ic, catLabel: c.label })
      }
    }
    return out
  })
</script>

<div class="pip">
  <div class="pip-search">
    <i class="ti ti-search"></i>
    <input class="input" placeholder={t('iconPicker.search')} value={query} oninput={(e) => (query = e.currentTarget.value)} />
    {#if query}
      <button class="icon-btn" title={t('win.close')} onclick={() => (query = '')}><i class="ti ti-x"></i></button>
    {/if}
  </div>

  {#if !searching}
    <!-- Перемикачі розділів (вкладки категорій) — активна лише ОДНА. -->
    <div class="pip-tabs" role="tablist">
      {#each categories as cat (cat.id)}
        <button
          class="pip-tab"
          class:active={activeTab === cat.id}
          onclick={() => activeTab = cat.id}
          type="button"
          role="tab"
          aria-selected={activeTab === cat.id}
          title={cat.label}
        >
          <i class="ti {cat.icon}"></i>
          <span class="pip-tab-name">{cat.label}</span>
        </button>
      {/each}
    </div>

    <div class="pip-grid">
      {#each activeCategory.icons as ic (ic)}
        <button class="icon-preset" class:sel={value === ic} onclick={() => onSelect(ic)} title={ic} style="--pcol:{color}" type="button">
          <i class="ti {ic}"></i>
        </button>
      {/each}
    </div>
  {:else if searchResults.length > 0}
    <div class="pip-grid">
      {#each searchResults as r (r.id)}
        <button class="icon-preset" class:sel={value === r.id} onclick={() => onSelect(r.id)} title="{r.id} · {r.catLabel}" style="--pcol:{color}" type="button">
          <i class="ti {r.id}"></i>
        </button>
      {/each}
    </div>
  {:else}
    <div class="pip-empty">
      <i class="ti ti-mood-empty"></i>
      <span>{t('iconPicker.empty')}</span>
    </div>
  {/if}
</div>

<style>
  .pip {
    display: flex;
    flex-direction: column;
    gap: 10px;
  }
  .pip-search {
    position: relative;
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .pip-search > .ti-search {
    position: absolute;
    left: 10px;
    color: var(--text-dim);
    font-size: 15px;
    pointer-events: none;
  }
  .pip-search .input {
    padding-left: 32px;
    padding-right: 34px;
  }
  .pip-search .icon-btn {
    position: absolute;
    right: 4px;
    width: 26px;
    height: 26px;
  }

  /* ── Вкладки категорій (перемикання розділів) ── */
  .pip-tabs {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
    padding-bottom: 2px;
  }
  .pip-tab {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 6px 10px;
    border: 1px solid var(--border);
    border-radius: 999px;
    background: var(--bg-elev);
    color: var(--text-mute);
    font: inherit;
    font-size: 12px;
    font-weight: 600;
    cursor: pointer;
    transition: border-color 0.15s ease, background 0.15s ease, color 0.15s ease;
  }
  .pip-tab:hover {
    border-color: var(--border-strong);
    color: var(--text);
  }
  .pip-tab i {
    font-size: 14px;
  }
  .pip-tab.active {
    background: rgba(var(--orange-rgb), .14);
    border-color: rgba(var(--orange-rgb), .45);
    color: var(--orange);
  }

  .pip-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(44px, 1fr));
    gap: 6px;
    padding: 10px 12px 12px;
    border: 1px solid var(--border);
    border-radius: 12px;
    background: var(--bg-elev);
  }
  .pip-empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 6px;
    padding: 24px 0;
    color: var(--text-dim);
    font-size: 13px;
  }
  .pip-empty .ti {
    font-size: 28px;
    opacity: 0.5;
  }
</style>
