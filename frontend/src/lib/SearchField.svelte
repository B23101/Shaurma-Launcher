<script lang="ts">
  // Єдине поле пошуку лаунчера: іконка-лупа + input.
  //   variant 'toolbar' — рядок у панелі інструментів (рамка, фокус-кільце);
  //   variant 'strip'   — смуга зверху модалки зі списком (на всю ширину
  //                       тіла, нижня межа; рахує від падінгу UiModal).
  let {
    value = $bindable(''),
    placeholder = '',
    ariaLabel = undefined as string | undefined,
    variant = 'toolbar' as 'toolbar' | 'strip',
  }: {
    value?: string
    placeholder?: string
    ariaLabel?: string
    variant?: 'toolbar' | 'strip'
  } = $props()
</script>

<div class="sf" class:strip={variant === 'strip'}>
  <i class="ti ti-search"></i>
  <input bind:value {placeholder} aria-label={ariaLabel} />
</div>

<style>
  .sf {
    display: flex; align-items: center; gap: 8px;
    background: var(--bg-input); border: 1px solid var(--border); border-radius: 12px;
    padding: 0 12px; height: 36px; flex: 1 1 200px; min-width: 140px;
    transition: border-color .15s, box-shadow .15s;
  }
  .sf:focus-within { border-color: rgba(var(--orange-rgb), .6); box-shadow: 0 0 0 3px rgba(var(--orange-rgb), .12); }
  .sf i { color: var(--text-dim); font-size: 15px; }
  .sf:focus-within i { color: var(--orange); }
  .sf input { flex: 1; min-width: 0; background: none; border: none; outline: none; color: var(--text); font-size: 13px; }

  .sf.strip {
    gap: 9px; height: auto; min-width: 0; flex: none; border-radius: 0;
    margin: -16px -20px 14px; padding: 10px 20px;
    border: none; border-bottom: 1px solid var(--border);
    background: color-mix(in srgb, var(--bg-input) 40%, transparent);
  }
  .sf.strip:focus-within { box-shadow: none; border-bottom-color: rgba(var(--orange-rgb), .6); }
  .sf.strip i { color: var(--text-mute); }
  .sf.strip:focus-within i { color: var(--orange); }
  .sf.strip input { background: transparent; color: var(--text); font: 500 12.5px var(--font-ui); }
  .sf.strip input::placeholder { color: var(--text-dim); }
</style>
