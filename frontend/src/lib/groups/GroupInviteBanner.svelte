<script lang="ts">
  // Запрошення в групу — картка у ПРИВАТНОМУ чаті з тим, хто запросив. Саме запрошення живе на
  // сервері (не як текст повідомлення), тож його не можна підробити чи переслати.
  import { t } from '../i18n.svelte'
  import { useGroupsStore } from './GroupsStore.svelte'

  let { peerId }: { peerId: string } = $props()
  const g = useGroupsStore()
  const mine = $derived(g.invites.filter((i) => i.from === peerId))
</script>

{#each mine as inv (inv.id)}
  <div class="gb" role="group" aria-label={t('groups.invite.card')}>
    <i class="ti ti-users-group gb-i"></i>
    <div class="gb-t">
      <b>{inv.groupTitle}</b>
      <small>{t('groups.invite.cardHint')}</small>
    </div>
    <button class="btn btn-primary btn-sm" disabled={g.busy} onclick={() => g.respondInvite(inv, true)}>{t('groups.invite.accept')}</button>
    <button class="btn btn-ghost btn-sm" disabled={g.busy} onclick={() => g.respondInvite(inv, false)}>{t('groups.invite.decline')}</button>
  </div>
{/each}

<style>
  .gb { display: flex; align-items: center; gap: 10px; margin: 8px 14px 0; padding: 10px 12px; border-radius: 12px; border: 1px solid rgba(var(--orange-rgb), .4); background: rgba(var(--orange-rgb), .08); flex-wrap: wrap; }
  .gb-i { font-size: 22px; color: var(--orange); }
  .gb-t { min-width: 0; flex: 1 1 160px; display: flex; flex-direction: column; gap: 2px; }
  .gb-t b { font-size: 13.5px; overflow-wrap: anywhere; }
  .gb-t small { font-size: 11.5px; color: var(--text-mute); }
</style>
