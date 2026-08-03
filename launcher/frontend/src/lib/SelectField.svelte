<script lang="ts">
  // ── SelectField: випадаючий список у стилі лаунчера ─────────────────────
  // Замінює нативний <select> у сторінках збірок, щоб меню виглядало як
  // решта UI лаунчера (без ванільних стрілок ОС). Підтримує опції з
  // іконкою (ti-*) або аватаром (head — URL голови акаунта).

  export interface SelectOption {
    value: string
    label: string
    icon?: string
    head?: string
  }

  let {
    value,
    options,
    onchange,
    placeholder = '',
    disabled = false,
    minWidth = '160px',
    ariaLabel = ''
  } = $props<{
    value: string
    options: SelectOption[]
    onchange: (v: string) => void
    placeholder?: string
    disabled?: boolean
    minWidth?: string
    ariaLabel?: string
  }>()

  let open = $state(false)

  const current = $derived(options.find(o => o.value === value))

  $effect(() => {
    if (!open) return
    const close = () => { open = false }
    document.addEventListener('click', close)
    return () => document.removeEventListener('click', close)
  })

  function pick(o: SelectOption) {
    open = false
    onchange(o.value)
  }
</script>

<div class="sf" class:open class:disabled={disabled} style="min-width:{minWidth}">
  <div class="sf-trigger" role="button" tabindex={disabled ? -1 : 0}
       aria-label={ariaLabel || current?.label || placeholder}
       onclick={(e) => { if (disabled) return; e.stopPropagation(); open = !open }}>
    {#if current?.head}
      <img class="sf-head" src={current.head} alt="" />
    {:else if current?.icon}
      <i class="ti {current.icon} lf"></i>
    {/if}
    <span class="sf-label">{current?.label ?? placeholder}</span>
    <i class="ti ti-chevron-down tr"></i>
  </div>
  {#if open && !disabled}
    <div class="sf-menu" onclick={(e) => e.stopPropagation()}>
      {#each options as opt}
        <button class="sf-item" class:sel={value === opt.value} onclick={() => pick(opt)}>
          {#if opt.head}
            <img class="sf-head" src={opt.head} alt="" />
          {:else if opt.icon}
            <i class="ti {opt.icon}"></i>
          {/if}
          <span>{opt.label}</span>
        </button>
      {/each}
    </div>
  {/if}
</div>

<style>
  .sf { position: relative; display: inline-flex; }
  .sf-trigger {
    display: flex; align-items: center; gap: 8px; width: 100%;
    background: var(--bg-input); border: 1px solid var(--border);
    border-radius: var(--r-md); padding: 9px 32px 9px 12px;
    font-size: 12px; font-weight: 600; color: var(--text);
    cursor: pointer; font-family: inherit;
    transition: border-color .15s;
  }
  .sf-trigger:hover { border-color: var(--border-hi); }
  .sf.open .sf-trigger { border-color: var(--violet); box-shadow: 0 0 0 2px rgba(123,77,255,.2); }
  .sf.disabled .sf-trigger { opacity: .5; cursor: not-allowed; }
  .sf-trigger .lf { color: var(--orange); font-size: 14px; }
  .sf-trigger .tr { position: absolute; right: 10px; color: var(--text-mute); font-size: 14px; pointer-events: none; }
  .sf-label { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .sf-head { width: 20px; height: 20px; border-radius: 6px; object-fit: cover; flex-shrink: 0; }
  .sf-menu {
    position: absolute; top: calc(100% + 4px); left: 0; min-width: 100%; z-index: 60;
    background: var(--surface-2, #1d1d1d); border: 1px solid var(--border);
    border-radius: var(--r-md); box-shadow: 0 8px 24px rgba(0,0,0,.4);
    padding: 4px; display: flex; flex-direction: column; gap: 2px;
    max-height: 260px; overflow-y: auto;
  }
  .sf-item {
    display: flex; align-items: center; gap: 8px; width: 100%;
    padding: 8px 10px; border: none; border-radius: var(--r-sm);
    background: transparent; color: var(--text); font-family: inherit;
    font-size: 12px; font-weight: 600; text-align: left; cursor: pointer;
    white-space: nowrap;
  }
  .sf-item:hover { background: var(--hover, rgba(255,255,255,.07)); }
  .sf-item.sel { color: var(--violet); }
  .sf-item i { color: var(--orange); }
  .sf-item .sf-head { width: 20px; height: 20px; }
  .sf-menu::-webkit-scrollbar { width: 6px; }
  .sf-menu::-webkit-scrollbar-thumb { background: var(--hover-strong); border-radius: 3px; }
  .sf-menu::-webkit-scrollbar-track { background: transparent; }
</style>
