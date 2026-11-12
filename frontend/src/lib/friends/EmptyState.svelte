<script lang="ts">
  // Порожній стан друзів/чатів — тонка обгортка над спільним EmptyState
  // лаунчера (../EmptyState.svelte), щоб увесь лаунчер мав один дизайн.
  // compact тут — вузька бічна панель: показуємо лише іконку-плитку.
  import SharedEmptyState from '../EmptyState.svelte'

  let { icon = 'ti-users', title = '', hint = '', compact = false, color = 'var(--orange)', children }: {
    icon?: string; title?: string; hint?: string; compact?: boolean; color?: string; children?: import('svelte').Snippet
  } = $props()
</script>

{#if compact}
  <SharedEmptyState {icon} {color} compact />
{:else}
  <SharedEmptyState {icon} {color} {title} {hint} actions={children} />
{/if}
