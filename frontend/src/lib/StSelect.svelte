<script lang="ts" module>
  export interface StOption {
    value: string
    label: string
    icon?: string
    // URL/dataURL квадратної голови (акаунт, пресет скіна)
    head?: string
    // Передня панель плаща (dataURL) — малюється поруч із головою
    cape?: string
  }
</script>

<script lang="ts">
  // ── StSelect: випадаючий список у стилі налаштувань лаунчера ───────────
  // Та сама розмітка й CSS (.st-select / .st-select-btn / .st-menu із
  // settings.css), що в settings/Setting.svelte, тільки options задаються
  // напряму, без SettingDef. Меню виноситься в <body>, щоб його не
  // обрізали картки. editable — комбобокс: поле вводу + підказки.
  let {
    value, options, onchange,
    placeholder = '', disabled = false, editable = false,
    emptyText = '', ariaLabel = '', minWidth = '200px',
  }: {
    value: string
    options: StOption[]
    onchange: (v: string) => void
    placeholder?: string
    disabled?: boolean
    editable?: boolean
    emptyText?: string
    ariaLabel?: string
    minWidth?: string
  } = $props()

  let open = $state(false)
  let rootEl = $state<HTMLDivElement | undefined>()
  let menuEl = $state<HTMLDivElement | undefined>()
  let typed = $state<string | null>(null)

  const current = $derived(options.find((o) => o.value === value))
  const shown = $derived.by(() => {
    if (!editable || typed === null || typed.trim() === '') return options
    const q = typed.trim().toLowerCase()
    return options.filter((o) => o.label.toLowerCase().includes(q) || o.value.toLowerCase().includes(q))
  })

  $effect(() => {
    if (!open) return
    const close = () => { open = false; typed = null }
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') close() }
    document.addEventListener('click', close)
    document.addEventListener('keydown', onKey)
    return () => { document.removeEventListener('click', close); document.removeEventListener('keydown', onKey) }
  })

  $effect(() => {
    if (!open || !menuEl || !rootEl) return
    const el = menuEl, root = rootEl
    document.body.appendChild(el)
    const place = () => {
      const r = root.getBoundingClientRect()
      const w = Math.max(r.width, 190)
      const below = window.innerHeight - r.bottom
      el.style.position = 'fixed'
      el.style.minWidth = `${w}px`
      el.style.maxHeight = '280px'
      el.style.overflowY = 'auto'
      el.style.left = `${Math.max(8, Math.min(r.left, window.innerWidth - w - 8))}px`
      if (below < 220 && r.top > below) { el.style.top = 'auto'; el.style.bottom = `${window.innerHeight - r.top + 4}px` }
      else { el.style.bottom = 'auto'; el.style.top = `${r.bottom + 4}px` }
    }
    place()
    window.addEventListener('resize', place)
    window.addEventListener('scroll', place, true)
    return () => { window.removeEventListener('resize', place); window.removeEventListener('scroll', place, true); el.remove() }
  })

  function pick(o: StOption) { open = false; typed = null; onchange(o.value) }
</script>

<div class="st-select sts" bind:this={rootEl} style="min-width:{minWidth}">
  {#if editable}
    <input class="st-input mono grow" type="text" autocomplete="off" spellcheck="false"
           value={typed ?? value} {placeholder} {disabled} aria-label={ariaLabel}
           onfocus={() => (open = true)}
           onclick={(e) => { e.stopPropagation(); open = true }}
           oninput={(e) => { typed = (e.currentTarget as HTMLInputElement).value; open = true }}
           onchange={(e) => { onchange((e.currentTarget as HTMLInputElement).value.trim()); typed = null }} />
  {:else}
    <button type="button" class="st-select-btn" class:open aria-haspopup="listbox" aria-expanded={open}
            aria-label={ariaLabel || current?.label || placeholder} {disabled}
            onclick={(e) => { e.stopPropagation(); open = !open }}>
      <span class="sts-cur">
        {#if current?.head}
          <img class="sts-head" src={current.head} alt="" />
          {#if current.cape}<img class="sts-cape" src={current.cape} alt="" />{/if}
        {:else if current?.icon}
          <i class="ti {current.icon}"></i>
        {/if}
        <span class="sts-label">{current?.label ?? placeholder}</span>
      </span>
      <i class="ti ti-chevron-down"></i>
    </button>
  {/if}

  {#if open && !disabled}
    <div class="st-menu" role="listbox" tabindex="-1" bind:this={menuEl} onclick={(e) => e.stopPropagation()} onkeydown={(e) => e.stopPropagation()}>
      {#each shown as o (o.value)}
        <button type="button" role="option" aria-selected={value === o.value} class:sel={value === o.value} onclick={() => pick(o)}>
          <span class="sts-cur">
            {#if o.head}
              <img class="sts-head" src={o.head} alt="" />
              {#if o.cape}<img class="sts-cape" src={o.cape} alt="" />{/if}
            {:else if o.icon}
              <i class="ti {o.icon}"></i>
            {/if}
            <span class="sts-label">{o.label}</span>
          </span>
          {#if value === o.value}<i class="ti ti-check"></i>{/if}
        </button>
      {:else}
        {#if emptyText}<div class="sts-empty">{emptyText}</div>{/if}
      {/each}
    </div>
  {/if}
</div>

<style>
  .sts { display: inline-block; }
  .sts :global(.st-select-btn) { width: 100%; }
  .sts-cur { display: inline-flex; align-items: center; gap: 8px; min-width: 0; }
  .sts-label { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .sts-cur > i { font-size: 15px; color: var(--text-mute); transform: none !important; }
  /* Голова — квадрат із заокругленими кутами, плащ — прямокутник. */
  .sts-head { width: 22px; height: 22px; border-radius: 6px; object-fit: cover; image-rendering: pixelated; flex: none; }
  .sts-cape { width: 18px; height: 22px; border-radius: 5px; object-fit: cover; image-rendering: pixelated; flex: none; }
  .sts-empty { padding: 10px 12px; font-size: 12px; color: var(--text-mute); }
</style>
