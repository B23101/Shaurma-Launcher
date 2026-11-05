<script lang="ts">
  // Створення групи: назва + одразу додати друзів. Прямо додаються лише ті, чия приватність це
  // дозволяє (за замовчуванням — друзі); решті сервер пропонує запрошення. Причини, чому когось
  // не додано, не розкриваються — це чужі налаштування приватності.
  import UiModal from '../UiModal.svelte'
  import FriendAvatar from '../friends/FriendAvatar.svelte'
  import { t } from '../i18n.svelte'
  import { clean } from '../friends/FriendsStore.svelte'
  import { useGroupsStore } from './GroupsStore.svelte'
  import { GROUP_MAX_MEMBERS, GROUP_MAX_TITLE } from './groupTypes'
  import type { SocialFriend } from '../types'

  let { friends, onclose, oncreated }: {
    friends: SocialFriend[]
    onclose: () => void
    oncreated?: (groupId: string) => void
  } = $props()
  const g = useGroupsStore()

  let title = $state('')
  let query = $state('')
  let chosen = $state<Set<string>>(new Set())

  const list = $derived(
    friends.filter((f) => !query.trim() || clean(f.nickname).toLowerCase().includes(query.trim().toLowerCase())),
  )
  const titleOk = $derived(clean(title).length > 0)

  function toggle(id: string) {
    const next = new Set(chosen)
    if (next.has(id)) next.delete(id)
    else if (next.size < GROUP_MAX_MEMBERS - 1) next.add(id)
    chosen = next
  }

  async function create() {
    if (!titleOk || g.busy) return
    const grp = await g.create(clean(title), [...chosen])
    if (grp) { onclose(); oncreated?.(grp.id) }
  }
</script>

<UiModal open={true} icon="ti-users-group" title={t('groups.create.title')} subtitle={t('groups.create.sub')}
         width="min(520px, 94vw)" locked={g.busy} {onclose}>
  <label class="gc-l" for="gc-title">{t('groups.create.name')}</label>
  <input id="gc-title" class="gc-in" bind:value={title} maxlength={GROUP_MAX_TITLE} placeholder={t('groups.create.namePh')}
         spellcheck="false" autocomplete="off" />

  <div class="gc-l gc-row">
    <span>{t('groups.create.addFriends')}</span>
    <span class="gc-cnt">{chosen.size}/{GROUP_MAX_MEMBERS - 1}</span>
  </div>
  <input class="gc-in" bind:value={query} placeholder={t('social.search')} aria-label={t('social.search')} spellcheck="false" autocomplete="off" />
  <ul class="gc-list">
    {#each list as f (f.accountId)}
      <li>
        <button class="gc-f" class:on={chosen.has(f.accountId)} onclick={() => toggle(f.accountId)} aria-pressed={chosen.has(f.accountId)}>
          <FriendAvatar nickname={clean(f.nickname)} color={f.color ?? ''} status={f.presence?.status ?? 'offline'} size={28} />
          <span class="gc-n">{clean(f.nickname)}</span>
          <i class="ti {chosen.has(f.accountId) ? 'ti-square-check-filled' : 'ti-square'}"></i>
        </button>
      </li>
    {:else}
      <li class="gc-empty">{t('groups.create.noFriends')}</li>
    {/each}
  </ul>
  <p class="gc-note"><i class="ti ti-info-circle"></i> {t('groups.create.note')}</p>

  {#snippet footer()}
    <button class="btn btn-ghost" onclick={onclose} disabled={g.busy}>{t('share.cancel')}</button>
    <button class="btn btn-primary" onclick={create} disabled={!titleOk || g.busy}>
      {#if g.busy}<i class="ti ti-loader-2 gc-spin"></i>{:else}<i class="ti ti-plus"></i>{/if} {t('groups.create.go')}
    </button>
  {/snippet}
</UiModal>

<style>
  .gc-l { display: block; margin: 12px 0 6px; font-size: var(--fs-min, 11px); font-weight: 800; text-transform: uppercase; letter-spacing: .4px; color: var(--text-mute); }
  .gc-l:first-child { margin-top: 0; }
  .gc-row { display: flex; justify-content: space-between; }
  .gc-cnt { font-weight: 700; }
  .gc-in { width: 100%; padding: 9px 12px; border-radius: 10px; border: 1px solid var(--border-hi); background: var(--bg-input); color: var(--text); font: inherit; font-size: 13px; }
  .gc-in:focus { outline: none; border-color: var(--orange); box-shadow: 0 0 0 3px color-mix(in srgb, var(--orange) 16%, transparent); }
  .gc-list { list-style: none; margin: 8px 0 0; padding: 0; max-height: 260px; overflow-y: auto; display: flex; flex-direction: column; gap: 4px; }
  .gc-f { width: 100%; display: flex; align-items: center; gap: 10px; padding: 6px 10px; border-radius: 10px; border: 1px solid var(--border); background: var(--bg-input); color: var(--text); cursor: pointer; text-align: left; }
  .gc-f:hover { border-color: rgba(var(--orange-rgb), .45); }
  .gc-f.on { border-color: var(--orange); background: rgba(var(--orange-rgb), .1); }
  .gc-f i { margin-left: auto; font-size: 18px; color: var(--text-mute); }
  .gc-f.on i { color: var(--orange); }
  .gc-n { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 13px; font-weight: 600; }
  .gc-empty { padding: 14px; text-align: center; font-size: 12.5px; color: var(--text-mute); }
  .gc-note { display: flex; gap: 6px; margin: 10px 0 0; font-size: 11.5px; line-height: 1.45; color: var(--text-mute); }
  .gc-note i { flex: none; margin-top: 1px; }
  .gc-spin { animation: gc-rot 1s linear infinite; }
  @keyframes gc-rot { to { transform: rotate(360deg); } }
</style>
