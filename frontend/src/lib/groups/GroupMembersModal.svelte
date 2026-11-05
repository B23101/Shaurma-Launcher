<script lang="ts">
  // Membership actions and server-filtered presence live in their own dialog.
  import FriendAvatar from '../friends/FriendAvatar.svelte'
  import UiModal from '../UiModal.svelte'
  import { t } from '../i18n.svelte'
  import { confirmDialog } from '../toast.svelte'
  import { useFriendsStore, clean } from '../friends/FriendsStore.svelte'
  import { useGroupsStore } from './GroupsStore.svelte'
  import { GROUP_MAX_MEMBERS, type Group } from './groupTypes'
  import type { SocialFriend } from '../types'

  let { group, friends, onclose }: { group: Group; friends: SocialFriend[]; onclose: () => void } = $props()
  const g = useGroupsStore()
  const fs = useFriendsStore()
  const statusOf = (id: string) => {
    if (id === g.me) return fs.st.meStatus
    const presence = friends.find(f => f.accountId === id)?.presence
    return presence?.status || undefined
  }
  const statusText = (id: string) => statusOf(id) ? t(`friendPicker.status.${statusOf(id)}`) : t('groups.members.statusHidden')
  const admin = $derived(g.isAdmin(group))
  let addOpen = $state(false)
  let query = $state('')

  const inGroup = $derived(new Set(group.members.map((m) => m.accountId)))
  const candidates = $derived(
    friends.filter((f) => !inGroup.has(f.accountId) && (!query.trim() || clean(f.nickname).toLowerCase().includes(query.trim().toLowerCase()))),
  )
  const full = $derived(group.members.length >= GROUP_MAX_MEMBERS)

  async function kick(id: string) {
    const ok = await confirmDialog(t('groups.kick.title'), t('groups.kick.text', { name: g.nameOf(id, group) }), { confirmLabel: t('groups.kick.go'), cancelLabel: t('share.cancel') })
    if (ok) await g.kick(group.id, id)
  }
  async function transfer(id: string) {
    const ok = await confirmDialog(t('groups.transfer.title'), t('groups.transfer.text', { name: g.nameOf(id, group) }), { confirmLabel: t('groups.transfer.go'), cancelLabel: t('share.cancel'), danger: false })
    if (ok) await g.transfer(group.id, id)
  }
  async function addFriend(f: SocialFriend) { await g.add(group.id, [f.accountId]) }
</script>
<UiModal open={true} icon="ti-users-group" title={t('groups.members.title')} subtitle={group.title} width="min(520px, 94vw)" locked={g.busy} {onclose}>
  <div class="gs-sec">{t('groups.settings.members', { n: group.members.length, max: GROUP_MAX_MEMBERS })}</div>
  <ul class="gs-m">
    {#each group.members as m (m.accountId)}
      <li>
        <FriendAvatar nickname={m.nickname || friends.find(f => f.accountId === m.accountId)?.nickname || ''} status={statusOf(m.accountId)} size={36} />
        <span class="gs-mn"><span>{g.nameOf(m.accountId, group)}<small>{statusText(m.accountId)}</small></span>{#if m.accountId === group.admin}<i class="ti ti-crown" title={t('groups.admin.badge')}></i>{/if}</span>
        {#if admin && m.accountId !== g.me}
          <button class="gs-ib" disabled={g.busy} onclick={() => transfer(m.accountId)} title={t('groups.transfer.title')} aria-label={t('groups.transfer.title')}><i class="ti ti-crown"></i></button>
          <button class="gs-ib danger" disabled={g.busy} onclick={() => kick(m.accountId)} title={t('groups.kick.title')} aria-label={t('groups.kick.title')}><i class="ti ti-user-x"></i></button>
        {/if}
      </li>
    {/each}
  </ul>

  {#if admin}
    <button class="btn btn-ghost btn-sm" disabled={full} onclick={() => (addOpen = !addOpen)}><i class="ti ti-user-plus"></i> {t('groups.add.open')}</button>
    {#if addOpen}
      <input class="gs-in gs-q" bind:value={query} placeholder={t('social.search')} aria-label={t('social.search')} spellcheck="false" autocomplete="off" />
      <ul class="gs-m">
        {#each candidates as f (f.accountId)}
          <li><span class="gs-mn">{clean(f.nickname)}</span>
            <button class="btn btn-primary btn-sm" disabled={g.busy || full} onclick={() => addFriend(f)}>{t('groups.add.go')}</button></li>
        {:else}<li class="gs-ro">{t('groups.add.none')}</li>{/each}
      </ul>
      <p class="gs-ro">{t('groups.add.hint')}</p>
    {/if}
  {/if}

  {#snippet footer()}<button class="btn btn-primary" onclick={onclose}>{t('groups.close')}</button>{/snippet}
</UiModal>
<style>
  .gs-sec { margin: 14px 0 6px; font-size: var(--fs-min, 11px); font-weight: 800; text-transform: uppercase; letter-spacing: .4px; color: var(--text-mute); }
  .gs-sec:first-child { margin-top: 0; }
  .gs-in { flex: 1; min-width: 0; padding: 8px 12px; border-radius: 10px; border: 1px solid var(--border-hi); background: var(--bg-input); color: var(--text); font: inherit; font-size: 13px; }
  .gs-in:focus { outline: none; border-color: var(--orange); }
  .gs-q { width: 100%; margin: 8px 0; }
  .gs-m { list-style: none; margin: 0 0 10px; padding: 0; display: flex; flex-direction: column; gap: 4px; max-height: none; overflow-y: auto; }
  .gs-m li { display: flex; align-items: center; gap: 6px; padding: 12px; border-radius: 10px; background: var(--bg-input); border: 1px solid var(--border); }
  .gs-mn { flex: 1; min-width: 0; display: flex; align-items: center; gap: 6px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 13px; font-weight: 600; }
  .gs-mn i { color: var(--orange); }
  .gs-ib { width: 28px; height: 28px; display: grid; place-items: center; border-radius: 8px; border: 1px solid var(--border-hi); background: transparent; color: var(--text-mute); cursor: pointer; }
  .gs-ib:hover:not(:disabled) { color: var(--orange); border-color: var(--orange); }
  .gs-ib.danger:hover:not(:disabled) { color: var(--red, #e5484d); border-color: var(--red, #e5484d); }
  .gs-ro { margin: 6px 0; font-size: 12px; line-height: 1.45; color: var(--text-mute); }
  .gs-sec2 { display: flex; gap: 6px; margin: 14px 0 0; font-size: 11.5px; line-height: 1.45; color: var(--text-mute); }
  .gs-sec2 i { flex: none; margin-top: 1px; }
  .gs-mn small { display: block; margin-top: 4px; color: var(--text-mute); font-size: 12px; font-weight: 400; }
</style>
