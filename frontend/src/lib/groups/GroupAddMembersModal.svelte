<script lang="ts">
  // Окреме вікно «Додати учасників»: мультивибір друзів, яких ще немає в групі.
  //  • «Додати» — сервер сам додасть тих, хто дозволив, а решті надішле запрошення (як у Telegram).
  //  • «Запросити» — завжди запрошення-картка в приватний чат: людина сама вирішує, приєднуватись.
  //  • «Копіювати» — текст запрошення, щоб надіслати де завгодно.
  import FriendAvatar from '../friends/FriendAvatar.svelte'
  import UiModal from '../UiModal.svelte'
  import { App } from '../wails'
  import { t } from '../i18n.svelte'
  import { toast } from '../toast.svelte'
  import { clean } from '../friends/FriendsStore.svelte'
  import { useGroupsStore } from './GroupsStore.svelte'
  import { GROUP_MAX_MEMBERS, type Group } from './groupTypes'
  import type { SocialFriend } from '../types'

  let { group, friends, onclose }: { group: Group; friends: SocialFriend[]; onclose: () => void } = $props()
  const g = useGroupsStore()

  let query = $state('')
  let picked = $state<Set<string>>(new Set())

  const inGroup = $derived(new Set(group.members.map((m) => m.accountId)))
  const pending = $derived(new Set((g.invites ?? []).filter((i) => i.groupId === group.id).map((i) => i.to)))
  const candidates = $derived(
    friends.filter((f) => !inGroup.has(f.accountId)
      && (!query.trim() || clean(f.nickname).toLowerCase().includes(query.trim().toLowerCase()))),
  )
  const room = $derived(Math.max(0, GROUP_MAX_MEMBERS - group.members.length))
  const count = $derived(picked.size)
  const statusOf = (f: SocialFriend) => f.presence?.status || undefined

  function toggle(id: string) {
    const next = new Set(picked)
    if (next.has(id)) next.delete(id)
    else if (next.size < room) next.add(id)
    picked = next
  }

  async function addSelected() {
    if (!count || g.busy) return
    const r = await g.add(group.id, [...picked])
    if (r) onclose()
  }
  async function inviteSelected() {
    if (!count || g.busy) return
    let ok = 0
    for (const id of picked) { if (await g.invite(group.id, id)) ok++ }
    if (ok) onclose()
  }
  const inviteText = $derived(t('groups.add.copyText', { group: group.title }))
  async function copyText() {
    try { await App.CopyToClipboard(inviteText) }
    catch { try { await navigator.clipboard?.writeText(inviteText) } catch { toast(t('social.profile.copyFail'), 'error'); return } }
    toast(t('groups.add.copied'), 'success')
  }
</script>

<UiModal open={true} icon="ti-user-plus" title={t('groups.add.open')} subtitle={group.title}
         width="min(520px, 94vw)" zIndex={1000010} locked={g.busy} {onclose}>
  <input class="ga-q" bind:value={query} placeholder={t('social.search')} aria-label={t('social.search')} spellcheck="false" autocomplete="off" />
  <ul class="ga-list">
    {#each candidates as f (f.accountId)}
      {@const on = picked.has(f.accountId)}
      <li>
        <button class="ga-row" class:on aria-pressed={on} disabled={!on && count >= room} onclick={() => toggle(f.accountId)}>
          <FriendAvatar nickname={f.nickname} status={statusOf(f)} size={32} />
          <span class="ga-n">{clean(f.nickname)}{#if pending.has(f.accountId)}<small>{t('groups.add.pending')}</small>{/if}</span>
          <i class="ti {on ? 'ti-circle-check-filled' : 'ti-circle'}" aria-hidden="true"></i>
        </button>
      </li>
    {:else}
      <li class="ga-empty">{t('groups.add.none')}</li>
    {/each}
  </ul>
  <p class="ga-hint">{t('groups.add.hint')}</p>

  {#snippet footer()}
    <button class="btn btn-ghost" onclick={copyText}><i class="ti ti-copy"></i> {t('groups.add.copy')}</button>
    <button class="btn btn-ghost" disabled={!count || g.busy} onclick={inviteSelected}><i class="ti ti-send"></i> {t('groups.add.invite')}{count ? ` (${count})` : ''}</button>
    <button class="btn btn-primary" disabled={!count || g.busy} onclick={addSelected}><i class="ti ti-user-plus"></i> {t('groups.add.go')}{count ? ` (${count})` : ''}</button>
  {/snippet}
</UiModal>

<style>
  .ga-q { width: 100%; box-sizing: border-box; margin-bottom: 8px; padding: 8px 12px; border-radius: 10px; border: 1px solid var(--border-hi); background: var(--bg-input); color: var(--text); font: inherit; font-size: 13px; }
  .ga-q:focus { outline: none; border-color: var(--orange); }
  .ga-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 4px; max-height: 320px; overflow-y: auto; }
  .ga-row { width: 100%; display: flex; align-items: center; gap: 10px; padding: 8px 12px; border-radius: 10px; border: 1px solid var(--border); background: var(--bg-input); color: var(--text); cursor: pointer; text-align: left; font: inherit; }
  .ga-row:hover:not(:disabled) { border-color: var(--orange); }
  .ga-row.on { border-color: var(--orange); background: rgba(var(--orange-rgb), .08); }
  .ga-row:disabled { opacity: .5; cursor: not-allowed; }
  .ga-n { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 13px; font-weight: 600; }
  .ga-n small { display: block; margin-top: 2px; font-size: 11.5px; font-weight: 400; color: var(--text-mute); }
  .ga-row .ti { font-size: 18px; color: var(--text-dim); }
  .ga-row.on .ti { color: var(--orange); }
  .ga-empty { padding: 18px; text-align: center; font-size: 12.5px; color: var(--text-mute); }
  .ga-hint { margin: 8px 0 0; font-size: 12px; line-height: 1.45; color: var(--text-mute); }
</style>
