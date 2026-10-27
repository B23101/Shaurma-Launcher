<script lang="ts">
  // Вхідний запит у друзі: аватар, нік, кнопки «прийняти / відхилити».
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
     use:tip={compact ? t('social.list.requestsN', { n: s.st.invites.length }) : ''}
     onclick={() => { if (compact) s.setCompact(false) }} role="presentation">
  <FAvatar size={36} head={s.heads[inv.fromNickname] ?? ''} initial={initialOf(inv.fromNickname)} />
  {#if !compact}
    <div class="rq-info">
      <div class="rq-name"><span class="rq-nick">{name}</span></div>
      <div class="rq-sub">{t('friends.wantsToBeFriend')}</div>
    </div>
    <div class="rq-acts">
      <button class="ff-ibtn bordered ok" onclick={() => s.respond(inv.id, true)} use:tip={t('friends.accept')} aria-label={t('friends.accept')}><i class="ti ti-check"></i></button>
      <button class="ff-ibtn bordered bad" onclick={() => s.respond(inv.id, false)} use:tip={t('friends.deny')} aria-label={t('friends.deny')}><i class="ti ti-x"></i></button>
    </div>
  {/if}
</div>

<style>
  .rq { display: flex; align-items: center; gap: 10px; height: 54px; margin: 3px 6px; padding: 0 10px; box-sizing: border-box; border-radius: 13px;
    background: linear-gradient(100deg, rgba(var(--orange-rgb), .13), rgba(var(--orange-rgb), .03)); border: 1px solid rgba(var(--orange-rgb), .28); transition: opacity .2s; }
  .rq.busy { opacity: .5; pointer-events: none; }
  .rq.compact { justify-content: center; padding: 0; height: 52px; margin: 2px 6px; cursor: pointer; }
  .rq-info { flex: 1 1 0; min-width: 0; display: flex; flex-direction: column; gap: 3px; }
  .rq-name { display: flex; align-items: baseline; gap: 6px; min-width: 0; font: 700 13px/1.2 var(--font-ui); color: var(--text); }
  .rq-nick { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .rq-sub { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font: 500 11px/1.2 var(--font-ui); color: var(--text-dim); }
  .rq-acts { display: flex; gap: 6px; flex: 0 0 auto; }
  .rq-acts :global(.ff-ibtn) { width: 28px; height: 28px; border-radius: 9px; }
  .rq-acts :global(.ff-ibtn.ok) { background: color-mix(in srgb, var(--ff-ok) 16%, transparent); border-color: color-mix(in srgb, var(--ff-ok) 45%, transparent); color: var(--ff-ok); }
</style>
