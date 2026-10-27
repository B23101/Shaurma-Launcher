<script lang="ts">
  // Порожній стан: велика «планета» з іконкою, заголовок, підказка, дія.
  // Лише одна повільна анімація (transform) — на екрані такий блок завжди один.
  let { icon = 'ti-users', title = '', hint = '', compact = false, children }: {
    icon?: string; title?: string; hint?: string; compact?: boolean; children?: import('svelte').Snippet
  } = $props()
</script>

<div class="es" class:compact>
  <div class="es-orb"><i class="ti {icon}"></i></div>
  {#if title}<div class="es-t">{title}</div>{/if}
  {#if hint}<div class="es-h">{hint}</div>{/if}
  {#if children}<div class="es-a">{@render children()}</div>{/if}
</div>

<style>
  .es { display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 8px; padding: 36px 22px; text-align: center; animation: es-in .36s var(--ff-ease) both; }
  .es-orb { width: 72px; height: 72px; border-radius: 24px; display: grid; place-items: center; margin-bottom: 4px;
    background: linear-gradient(145deg, rgba(var(--orange-rgb), .16), rgba(var(--orange-rgb), .05));
    border: 1px solid rgba(var(--orange-rgb), .26); animation: es-float 4s ease-in-out infinite; }
  .es-orb i { font-size: 34px; color: var(--orange); }
  .es-t { font: 750 14px/1.3 var(--font-ui); color: var(--text); }
  .es-h { max-width: 300px; font: 400 12px/1.55 var(--font-ui); color: var(--text-dim); }
  .es-a { display: flex; flex-wrap: wrap; gap: 8px; justify-content: center; margin-top: 8px; }
  .es.compact { padding: 14px 0; }
  .es.compact .es-orb { width: 44px; height: 44px; border-radius: 14px; margin: 0; animation: none; }
  .es.compact .es-orb i { font-size: 22px; }
  .es.compact .es-t, .es.compact .es-h, .es.compact .es-a { display: none; }
  @keyframes es-in { from { opacity: 0; transform: translateY(8px); } to { opacity: 1; transform: none; } }
  @keyframes es-float { 50% { transform: translateY(-5px); } }
  @media (prefers-reduced-motion: reduce) { .es, .es-orb { animation: none; } }
</style>
