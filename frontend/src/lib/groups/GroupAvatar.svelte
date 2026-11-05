<script lang="ts">
  // Аватар групи: готова іконка з GROUP_ICONS або перша літера назви. Відтінок — колір обведення групи
  // (якщо задано), інакше помаранчевий. Іконка поза списком (старий/чужий клієнт) показується літерою.
  import { safeGroupColor, safeGroupIcon } from './groupTypes'

  let { title, icon = '', color = '', size = 34 }: { title: string; icon?: string; color?: string; size?: number } = $props()
  const ic = $derived(safeGroupIcon(icon))
  const c = $derived(safeGroupColor(color))
  const letter = $derived((title.trim().charAt(0) || '?').toUpperCase())
</script>

<span class="gav" style="--s:{size}px;{c ? `--t:${c}` : ''}" aria-hidden="true">
  {#if ic}<i class="ti ti-{ic}"></i>{:else}{letter}{/if}
</span>

<style>
  .gav {
    --t: var(--orange);
    flex: none; width: var(--s); height: var(--s); display: grid; place-items: center;
    border-radius: calc(var(--s) * .3); font-weight: 800; font-size: calc(var(--s) * .44); line-height: 1;
    color: var(--t); background: color-mix(in srgb, var(--t) 15%, transparent);
  }
  .gav i { font-size: calc(var(--s) * .56); }
</style>
