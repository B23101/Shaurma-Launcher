<script lang="ts">
  // Вихідний запит у друзі: аватар, нік, «очікує відповіді» і кнопка «скасувати».
  import { useFriendsStore, clean, initialOf } from './FriendsStore.svelte'
  import { t } from '../i18n.svelte'
  import { tip } from './tooltip'
  import FAvatar from './FAvatar.svelte'
  import type { SocialInvite } from '../types'

  let { inv, compact = false }: { inv: SocialInvite; compact?: boolean } = $props()
  const s = useFriendsStore()
  const name = $derived(clean(inv.fromNickname))
</script>

<div class="rq" class:busy={s.busy === inv.id} class:compact
     use:tip={compact ? t('friends.outgoing') : ''}
     onclick={() => { if (compact) s.setCompact(false) }} role="presentation">
  <FAvatar size={36} head={s.heads[inv.fromNickname] ?? ''} initial={initialOf(inv.fromNickname)} />
  {#if !compact}
    <div class="rq-info">
      <div class="rq-name"><span class="rq-nick">{name}</span></div>
      <div class="rq-sub"><i class="ti ti-clock"></i> {t('friends.pending')}</div>
    </div>
    <div class="rq-acts">
      <button class="ff-ibtn bordered bad" onclick={() => s.cancelSent(inv)} use:tip={t('friends.cancelRequest')} aria-label={t('friends.cancelRequest')}><i class="ti ti-x"></i></button>
    </div>
  {/if}
</div>

<style>
  .rq { display: flex; align-items: center; gap: 14px; height: 72px; margin: 6px 8px; padding: 0 12px 0 12px; box-sizing: border-box; border-radius: 18px;
    background: var(--bg-input); border: 1px solid var(--border); transition: opacity .2s; }
  .rq.busy { opacity: .5; pointer-events: none; }
  .rq.compact { justify-content: center; padding: 0; height: 60px; margin: 4px 10px; cursor: pointer; }
  .rq-info { flex: 1 1 0; min-width: 0; display: flex; flex-direction: column; gap: 4px; }
  .rq-name { display: flex; align-items: baseline; gap: 7px; min-width: 0; font: 700 14px/1.2 var(--font-ui); color: var(--text); }
  .rq-nick { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .rq-sub { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font: 500 11.5px/1.2 var(--font-ui); color: var(--text-dim); }
  .rq-acts { display: flex; gap: 6px; flex: 0 0 auto; }
  .rq-acts :global(.ff-ibtn) { width: 36px; height: 36px; border-radius: 12px; }
  .rq-acts :global(.ff-ibtn.ok) { background: color-mix(in srgb, var(--ff-ok) 16%, transparent); border-color: color-mix(in srgb, var(--ff-ok) 45%, transparent); color: var(--ff-ok); }
</style>
