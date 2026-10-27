<script lang="ts">
  // ══════════════════════════════════════════════════════════════════════
  // Сторінка «Друзі» — окремий розділ sidebar. Друзі Minecraft (граф Mojang)
  // прибрано: друзі, чат, запрошення й P2P працюють лише через акаунт Shaurma,
  // тож сторінка — це лише ОБОЛОНКА (картка) навколо ShaurmaFriends. Усе, що
  // всередині, — окремі модулі у ./friends (див. ShaurmaFriends.svelte).
  // ══════════════════════════════════════════════════════════════════════
  import { t } from './i18n.svelte'
  import ShaurmaFriends from './friends/ShaurmaFriends.svelte'
  import './friends/ff.css'

  let {
    shaurmaAvailable = false,
    openAccountId = '',
    activeId = $bindable(''),
    onOpenMod,
  }: {
    shaurmaAvailable?: boolean
    // accountId друга, чий тост у трей щойно натиснули (див. App.svelte).
    openAccountId?: string
    activeId?: string
    onOpenMod?: (source: string, projectId: string) => void
  } = $props()
</script>

<div class="fp">
  {#if shaurmaAvailable}
    <ShaurmaFriends {openAccountId} bind:activeId {onOpenMod} />
  {:else}
    <div class="fp-off">
      <i class="ti ti-user-shield"></i>
      <b>{t('social.loginTitle')}</b>
      <span>{t('social.loginHint')}</span>
    </div>
  {/if}
</div>

<style>
  /* Без «картки»: сторінка на всю область контенту, список друзів стикується
     з головним sidebar, як другий sidebar (див. .content-friends у style.css). */
  .fp { display: flex; flex-direction: column; flex: 1 1 0; min-height: 0; min-width: 0; overflow: visible; animation: fp-in .25s ease-out both; }
  .fp > :global(*) { flex: 1; min-height: 0; min-width: 0; }
  @keyframes fp-in { from { opacity: 0; } to { opacity: 1; } }
  .fp-off { flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 8px; padding: 40px; text-align: center; color: var(--text-dim); font: 500 12.5px/1.5 var(--font-ui); }
  .fp-off i { font-size: 34px; color: var(--orange); }
  .fp-off b { color: var(--text); font-size: 14px; }
  @media (prefers-reduced-motion: reduce) { .fp { animation: none; } }
</style>
