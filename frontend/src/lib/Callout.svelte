<script lang="ts">
  // Єдина плашка-підказка лаунчера: іконка + текст у закругленій рамці.
  // tone: 'info' (помаранчева, за замовчуванням) | 'warn' (жовта).
  import type { Snippet } from 'svelte'

  let { icon = 'ti-info-circle', tone = 'info', children }: {
    icon?: string
    tone?: 'info' | 'warn'
    children?: Snippet
  } = $props()
</script>

<div class="callout" class:warn={tone === 'warn'}>
  <i class="ti {icon}"></i>
  <span>{@render children?.()}</span>
</div>

<style>
  .callout {
    display: flex; align-items: flex-start; gap: 8px;
    font-size: 12.5px; color: var(--text-mute); line-height: 1.5;
    background: rgba(var(--orange-rgb), .07); border: 1px solid rgba(var(--orange-rgb), .2);
    border-radius: 10px; padding: 10px 14px;
  }
  .callout.warn { background: rgba(255,170,0,.08); border-color: rgba(255,170,0,.3); }
  .callout i { color: var(--orange); font-size: 16px; flex-shrink: 0; margin-top: 1px; }
</style>
