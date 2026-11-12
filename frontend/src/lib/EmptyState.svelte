<script lang="ts">
  // Єдиний порожній стан лаунчера (дизайн «Тека порожня»): пунктирна картка,
  // іконка-плитка з градієнтом, заголовок, підказка, кнопки дій і натяк
  // внизу. Єдине, що відрізняється між екранами, — колір (`color`).
  //   • compact — малий варіант для вузьких блоків (модалки, бічні панелі):
  //     без рамки, менша плитка.
  //   • actions — кнопки дій (.btn), footnote — рядок-натяк під ними.
  import type { Snippet } from 'svelte'

  let {
    icon = 'ti-inbox',
    color = 'var(--orange)',
    title = '',
    hint = '',
    compact = false,
    actions,
    footnote,
  }: {
    icon?: string
    color?: string
    title?: string
    hint?: string
    compact?: boolean
    actions?: Snippet
    footnote?: Snippet
  } = $props()
</script>

<div class="es" class:compact style="--es-c:{color}">
  <div class="es-ic"><i class="ti {icon}"></i></div>
  {#if title}<div class="es-title">{title}</div>{/if}
  {#if hint}<div class="es-sub">{hint}</div>{/if}
  {#if actions}<div class="es-actions">{@render actions()}</div>{/if}
  {#if footnote}<div class="es-foot">{@render footnote()}</div>{/if}
</div>

<style>
  .es {
    display: flex; flex-direction: column; align-items: center; text-align: center;
    gap: 8px; padding: 44px 24px; margin: 4px 0;
    background: color-mix(in srgb, var(--bg-card) 55%, transparent);
    border: 1px dashed var(--border-hi); border-radius: var(--r-lg);
    animation: rise-in .3s ease both;
  }
  .es-ic {
    width: 62px; height: 62px; border-radius: 17px; margin-bottom: 4px;
    display: flex; align-items: center; justify-content: center; font-size: 27px;
    background: linear-gradient(135deg, color-mix(in srgb, var(--es-c) 20%, transparent), color-mix(in srgb, var(--es-c) 4%, transparent));
    border: 1px solid color-mix(in srgb, var(--es-c) 35%, transparent);
    box-shadow: 0 10px 26px rgba(0,0,0,.3);
    color: var(--es-c);
  }
  .es-title { font-size: 15px; font-weight: 700; color: var(--text); }
  .es-sub { font-size: 12.5px; color: var(--text-mute); max-width: 400px; line-height: 1.5; }
  .es-actions { display: flex; gap: 8px; margin-top: 12px; flex-wrap: wrap; justify-content: center; }
  .es-foot {
    display: inline-flex; align-items: center; gap: 6px; margin-top: 6px;
    font-size: 11.5px; color: var(--text-dim);
  }

  .es.compact { padding: 18px 12px; margin: 0; gap: 6px; background: none; border: none; }
  .es.compact .es-ic { width: 44px; height: 44px; border-radius: 13px; font-size: 20px; margin-bottom: 2px; box-shadow: none; }
  .es.compact .es-title { font-size: 13px; }
  .es.compact .es-sub { font-size: 12px; }
  .es.compact .es-actions { margin-top: 8px; }

  @media (prefers-reduced-motion: reduce) { .es { animation: none; } }
</style>
