<script lang="ts">
  import { tip as launcherTip } from './tooltip'

  // ── Ліва панель (макет «месенджер»): картка 275px ─────────────────────────
  // Зверху — пошук і амброва кнопка «+» (меню: додати друга / створити групу).
  // Далі — банер «Запити в друзі», блок груп, роздільник і друзі. Знизу —
  // футер «Приватність». Запити відкриваються тут же, в тій самій колонці.
  import { useFriendsStore } from './FriendsStore.svelte'
  import { t } from '../i18n.svelte'
  import { tip } from './tooltip'
  import FriendRow from './FriendRow.svelte'
  import RequestRow from './RequestRow.svelte'
  import SentRow from './SentRow.svelte'
  import EmptyState from './EmptyState.svelte'
  import FriendsNotices from './FriendsNotices.svelte'
  import GroupsPanel from '../groups/GroupsPanel.svelte'
  import GroupAvatar from '../groups/GroupAvatar.svelte'
  import type { SocialFriend } from '../types'
  import type { Group } from '../groups/groupTypes'
  import { useGroupsStore } from '../groups/GroupsStore.svelte'
  import { toMs } from './activityCache'
  import { clean } from './FriendsStore.svelte'
  import { onMount } from 'svelte'

  let { activeId = '', onAdd, onCreateGroup, onPrivacy }: { activeId?: string; onAdd: () => void; onCreateGroup: () => void; onPrivacy: () => void } = $props()
  const s = useFriendsStore()
  const gs = useGroupsStore()

  type ChatRow = { key: string; name: string; time: number; friend?: SocialFriend; group?: Group }
  const byTime = (a: ChatRow, b: ChatRow) => b.time - a.time || a.name.localeCompare(b.name) || a.key.localeCompare(b.key)
  const q = $derived(s.search.trim().toLowerCase())
  const groupRows = $derived.by<ChatRow[]>(() =>
    gs.groups
      .filter((g) => !q || g.title.toLowerCase().includes(q))
      .map((g) => ({ key: 'group:' + g.id, name: g.title, time: gs.activity[g.id] || toMs(g.createdAt), group: g }))
      .sort(byTime))
  const friendRows = $derived.by<ChatRow[]>(() => {
    const people = [...s.filtered, ...s.otherChats.filter((f) => !q || f.nickname.toLowerCase().includes(q))]
    return people.map((f) => ({ key: 'friend:' + f.accountId, name: f.nickname, time: s.activity[f.accountId] || toMs(f.since), friend: f })).sort(byTime)
  })
  // Порядок «останніх активних» береться з локального кешу ОДРАЗУ (restoreActivity), мережа лише уточнює.
  $effect(() => { void s.me; void gs.me; s.restoreActivity(); gs.restoreActivity() })
  $effect(() => { s.syncActivity([...s.st.friends, ...s.otherChats].map(f => f.accountId)) })
  let waited = $state(false)
  onMount(() => { const id = setTimeout(() => (waited = true), 2500); return () => clearTimeout(id) })
  const orderReady = $derived(waited || (s.activityReady && (gs.loaded ? gs.activityReady || gs.groups.length === 0 : false)))

  // Історію групи вантажимо наперед (наведення з невеликою затримкою або натискання).
  let hoverT: ReturnType<typeof setTimeout> | undefined
  const hoverGroup = (id: string) => { clearTimeout(hoverT); hoverT = setTimeout(() => gs.prefetch(id), 120) }
  const leaveGroup = () => clearTimeout(hoverT)

  // Захист від «клікнув одне — відкрилось інше».
  let pressedKey = ''
  const rowKey = (e: Event) => (e.target as HTMLElement | null)?.closest<HTMLElement>('[data-chat-key]')?.dataset.chatKey ?? ''
  function rememberPress(e: PointerEvent) { pressedKey = rowKey(e) }
  function guardClick(e: MouseEvent) {
    const k = rowKey(e)
    if (pressedKey && k && k !== pressedKey && e.detail > 0) { e.stopPropagation(); e.preventDefault() }
    pressedKey = ''
  }

  // Меню «+»
  let menuOpen = $state(false)
  function onWinDown(e: PointerEvent) { if (menuOpen && !(e.target as HTMLElement | null)?.closest('.fl-add-wrap')) menuOpen = false }
  function onWinKey(e: KeyboardEvent) { if (e.key === 'Escape') menuOpen = false }

  // Запити
  const reqCount = $derived(s.st.invites.length + gs.invites.length)
  const reqPreview = $derived(s.st.invites.length ? s.st.invites.map((i) => clean(i.fromNickname)).join(', ') : t('friends.requests.empty'))
  const inRequests = $derived(s.listTab === 'requests')
  // Один спільний список (як у Telegram): групи й особисті чати разом, за часом останньої активності.
  const allRows = $derived<ChatRow[]>([...groupRows, ...friendRows].sort(byTime))
  const hasAnyChat = $derived(allRows.length > 0)
</script>

<svelte:window onpointerdown={onWinDown} onkeydown={onWinKey} />


<aside class="fl" aria-label={t('friends.title')}>
  <div class="fl-top">
    <div class="fl-search">
      <i class="ti ti-search"></i>
      <input bind:value={s.search} placeholder={t('social.search')} aria-label={t('social.search')} spellcheck="false" autocomplete="off" />
      {#if s.search}<button class="fl-clear" onclick={() => (s.search = '')} aria-label={t('friends.close')}><i class="ti ti-x"></i></button>{/if}
    </div>
    <div class="fl-add-wrap">
      <button class="fl-add" onclick={() => (menuOpen = !menuOpen)} aria-haspopup="menu" aria-expanded={menuOpen}
              use:launcherTip={t('friends.addFriend')} aria-label={t('friends.addFriend')}>
        <i class="ti ti-plus"></i>
      </button>
      {#if menuOpen}
        <div class="fl-menu" role="menu">
          <button class="fl-mi" role="menuitem" onclick={() => { menuOpen = false; onAdd() }}><i class="ti ti-user-plus"></i><span>{t('friends.addFriend')}</span></button>
          {#if gs.enabled}
            <button class="fl-mi" role="menuitem" onclick={() => { menuOpen = false; onCreateGroup() }}><i class="ti ti-users-plus"></i><span>{t('groups.new')}</span></button>
          {/if}
        </div>
      {/if}
    </div>
  </div>

  <FriendsNotices />

  <div class="fl-scroll" onpointerdowncapture={rememberPress} onclickcapture={guardClick}>
    {#if inRequests}
      <button class="fl-back" onclick={() => (s.listTab = 'chats')}>
        <i class="ti ti-arrow-left"></i><span>{t('friends.tab.requests')}</span>
        {#if reqCount > 0}<span class="fl-count">{reqCount}</span>{/if}
      </button>

      {#if s.st.invites.length > 0}
        <section class="fl-sec">
          <h3 class="fl-sh"><i class="ti ti-user-down"></i>{t('friends.incoming')}<span>{s.st.invites.length}</span></h3>
          {#each s.st.invites as inv (inv.id)}<RequestRow {inv} />{/each}
        </section>
      {/if}
      {#if s.sent.length > 0}
        <section class="fl-sec">
          <h3 class="fl-sh"><i class="ti ti-user-up"></i>{t('friends.outgoing')}<span>{s.sent.length}</span></h3>
          {#each s.sent as inv (inv.id)}<SentRow {inv} />{/each}
        </section>
      {/if}
      {#if gs.invites.length > 0}
        <GroupsPanel invitesOnly oncreate={onCreateGroup} />
      {/if}
      {#if s.st.invites.length === 0 && s.sent.length === 0 && gs.invites.length === 0}
        <EmptyState icon="ti-user-heart" title={t('friends.requests.empty')} hint={t('friends.requests.emptyHint')}>
          <button class="btn btn-primary btn-sm" onclick={onAdd}><i class="ti ti-user-plus"></i> {t('friends.addFriend')}</button>
        </EmptyState>
      {/if}
    {:else}
      {#if !q}
        <button class="fl-req" class:has={reqCount > 0} onclick={() => (s.listTab = 'requests')}>
          <span class="fl-req-ic"><i class="ti ti-user-plus"></i></span>
          <span class="fl-req-tx"><b>{t('friends.tab.requests')}</b><small>{reqPreview}</small></span>
          {#if reqCount > 0}<span class="fl-count">{reqCount}</span>{/if}
        </button>
      {/if}

      {#if !orderReady}
        {#each [0, 1, 2, 3, 4, 5] as i (i)}<div class="fl-skel" style="--i:{i}" aria-hidden="true"></div>{/each}
      {:else}
        {#each allRows as row (row.key)}
          {#if row.group}
            {@const gr = row.group}
            <div class="fl-row" data-chat-key={row.key}>
              <button class="fl-group" class:active={gs.activeId === gr.id} onclick={() => gs.open(gr.id)}
                      onpointerenter={() => hoverGroup(gr.id)} onpointerleave={leaveGroup} onfocus={() => hoverGroup(gr.id)} onpointerdown={() => gs.prefetch(gr.id)}>
                <span class="fl-gav"><GroupAvatar title={gr.title} icon={gr.settings.icon} color={gr.settings.color} size={34} /></span>
                <span class="fl-gtx"><b>{gr.title}{#if s.isMuted(gr.id)} <i class="ti ti-bell-off" aria-label={t('groups.mute')}></i>{/if}</b><small>{t('groups.members', { n: gr.members.length })}</small></span>
                {#if gs.unread[gr.id]}<span class="ff-badge">{gs.unread[gr.id]}</span>{/if}
              </button>
            </div>
          {:else if row.friend}
            <div class="fl-row" data-chat-key={row.key}>
              <FriendRow f={row.friend} readonly={!s.isFriend(row.friend.accountId)} selected={activeId === row.friend.accountId} animate={false} />
            </div>
          {/if}
        {/each}
        {#if !hasAnyChat}
          <EmptyState icon="ti-message-circle-2" title={s.search ? t('social.searchEmpty') : t('social.empty')} />
        {/if}
      {/if}
    {/if}
  </div>

  <div class="fl-foot">
    <button class="fl-priv" onclick={onPrivacy}>
      <span class="fl-priv-l"><span class="fl-priv-ic"><i class="ti ti-lock"></i></span><span class="fl-priv-t">{t('social.priv.title')}</span></span>
      <i class="ti ti-chevron-right fl-priv-ch"></i>
    </button>
    <button class="fl-fb" onclick={() => s.load(true)} use:tip={s.connLabel} aria-label={t('friends.refresh')}>
      <i class="ti ti-refresh"></i><i class="fl-conn" class:on={s.st.conn === 'online'} class:wait={s.st.conn === 'connecting'}></i>
    </button>
    <button class="fl-fb" class:on={s.muteAll} onclick={() => s.toggleMuteAll()} disabled={s.muteAllBusy} aria-pressed={s.muteAll}
            use:tip={s.muteAll ? t('social.muteAll.on') : t('social.muteAll.off')} aria-label={s.muteAll ? t('social.muteAll.on') : t('social.muteAll.off')}>
      <i class="ti {s.muteAll ? 'ti-bell-off' : 'ti-bell'}"></i>
    </button>
  </div>
</aside>

<style>
  /* Ліва панель — самостійна картка 275px (як у макеті): рамка, радіус 16,
     пошук + «+» зверху, список, футер. Без рейки, табів, фільтрів і ресайзу. */
  .fl { --fl-bg: var(--bg-panel); position: relative; display: flex; flex-direction: column; min-width: 0; min-height: 0;
    background: var(--bg-panel); border: 1px solid var(--border); border-radius: var(--r-lg, 16px); overflow: visible; }
  .fl-top { position: relative; z-index: 3; display: flex; align-items: center; gap: 8px; padding: 12px; border-bottom: 1px solid var(--border); flex: 0 0 auto; }
  .fl-search { position: relative; flex: 1; min-width: 0; display: flex; align-items: center; height: 34px; padding: 0 8px 0 31px; box-sizing: border-box;
    border-radius: 11px; background: var(--bg-input, var(--bg-card)); border: 1px solid var(--border); transition: border-color .15s; }
  .fl-search:focus-within { border-color: rgba(var(--orange-rgb), .55); }
  .fl-search > i.ti { position: absolute; left: 10px; top: 50%; transform: translateY(-50%); font-size: 14px; color: var(--text-dim); pointer-events: none; }
  .fl-search input { flex: 1; min-width: 0; background: none; border: 0; outline: none; color: var(--text); font: 500 13px var(--font-ui); }
  .fl-search input::placeholder { color: var(--text-dim); }
  .fl-clear { display: grid; place-items: center; width: 22px; height: 22px; border: 0; border-radius: 7px; background: none; color: var(--text-mute); cursor: pointer; }
  .fl-clear:hover { color: var(--text); background: var(--hover-strong); }

  .fl-add-wrap { position: relative; flex: 0 0 auto; }
  .fl-add { width: 34px; height: 34px; display: grid; place-items: center; border: 0; border-radius: 11px; cursor: pointer; font-size: 16px;
    background: var(--orange); color: var(--accent-ink, #0c0d11); transition: filter .15s, transform .1s; }
  .fl-add:hover { filter: brightness(1.1); }
  .fl-add:active { transform: scale(.95); }
  .fl-menu { position: absolute; right: 0; top: calc(100% + 6px); width: 196px; display: flex; flex-direction: column; gap: 2px; padding: 6px; z-index: 40;
    background: var(--bg-card); border: 1px solid var(--border-hi); border-radius: 14px; box-shadow: 0 16px 36px rgba(0, 0, 0, .5); animation: fl-pop .14s ease-out; }
  .fl-mi { display: flex; align-items: center; gap: 10px; padding: 9px 10px; border: 0; border-radius: 10px; background: transparent; color: var(--text); text-align: left; cursor: pointer; font: 500 12.5px var(--font-ui); }
  .fl-mi:hover { background: var(--hover); }
  .fl-mi i { font-size: 16px; color: var(--orange); flex: 0 0 auto; }
  @keyframes fl-pop { from { opacity: 0; transform: scale(.97) translateY(4px); } to { opacity: 1; transform: none; } }

  .fl-scroll { flex: 1 1 0; min-height: 0; overflow-y: auto; overflow-x: hidden; overscroll-behavior: contain; padding: 8px; display: flex; flex-direction: column; gap: 3px; }
  .fl-row { display: contents; }
  .fl-sec { display: flex; flex-direction: column; gap: 3px; }
  .fl-sec + .fl-sec { margin-top: 8px; }
  .fl-sh { display: flex; align-items: center; gap: 7px; width: 100%; margin: 0; padding: 6px 8px; box-sizing: border-box; font-size: var(--fs-min); font-weight: 700; letter-spacing: .9px; text-transform: uppercase; color: var(--text-dim); }
  .fl-sh i { font-size: 13px; }
  .fl-sh span { margin-left: auto; font: 700 var(--fs-min)/1 var(--font-mono); letter-spacing: 0; }

  /* Банер «Запити в друзі» — перший елемент списку. */
  .fl-req { display: flex; align-items: center; gap: 10px; width: 100%; flex: 0 0 auto; padding: 10px; margin-bottom: 3px; border-radius: 12px; cursor: pointer; text-align: left; color: var(--text);
    background: rgba(var(--orange-rgb), .08); border: 1px solid rgba(var(--orange-rgb), .25); transition: background .15s, border-color .15s; }
  .fl-req:hover { background: rgba(var(--orange-rgb), .14); border-color: rgba(var(--orange-rgb), .4); }
  .fl-req-ic { width: 34px; height: 34px; flex: 0 0 auto; display: grid; place-items: center; border-radius: 10px; font-size: 16px; color: var(--orange);
    background: rgba(var(--orange-rgb), .18); border: 1px solid rgba(var(--orange-rgb), .35); }
  .fl-req-tx { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 1px; }
  .fl-req-tx b { font: 700 12.5px/1.25 var(--font-ui); color: var(--orange); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .fl-req-tx small { font: 500 11px/1.25 var(--font-ui); color: var(--text-mute); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .fl-count { flex: 0 0 auto; min-width: 21px; height: 21px; padding: 0 6px; display: inline-flex; align-items: center; justify-content: center; border-radius: 999px;
    background: var(--orange); color: var(--accent-ink, #0c0d11); font: 700 11px/1 var(--font-ui); }
  .fl-back { display: flex; align-items: center; gap: 8px; width: 100%; flex: 0 0 auto; padding: 8px 10px; margin-bottom: 4px; border: 0; border-radius: 11px; background: transparent; cursor: pointer; color: var(--text); font: 700 12.5px var(--font-ui); }
  .fl-back:hover { background: var(--hover); }
  .fl-back i { font-size: 15px; color: var(--orange); }
  .fl-back span:not(.fl-count) { flex: 1; text-align: left; }

  /* Рядок групи — така ж геометрія, як і в друга. */
  .fl-group { width: 100%; display: flex; align-items: center; gap: 10px; padding: 8px 10px; border: 1px solid transparent; border-radius: 12px; background: transparent; color: var(--text); text-align: left; cursor: pointer; transition: background .12s, border-color .12s; }
  .fl-group:hover { background: var(--hover); }
  .fl-group.active { background: rgba(var(--orange-rgb), .12); border-color: rgba(var(--orange-rgb), .3); }
  .fl-group.active b { color: var(--orange); }
  .fl-gav { flex: 0 0 auto; display: inline-flex; }
  .fl-gtx { display: flex; flex: 1; min-width: 0; flex-direction: column; gap: 1px; }
  .fl-gtx b { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font: 600 13px/1.3 var(--font-ui); }
  .fl-gtx small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-mute); font: 500 11.5px/1.3 var(--font-ui); }
  .fl-group:focus-visible { outline: 2px solid var(--orange); outline-offset: -2px; }

  .fl-skel { height: 50px; flex: 0 0 auto; border-radius: 12px; background: var(--bg-card); opacity: .6; animation: fl-skel 1.2s ease-in-out infinite; animation-delay: calc(var(--i) * 90ms); }
  @keyframes fl-skel { 50% { opacity: .25; } }

  /* Футер: «Приватність» + дві іконки (оновити зі станом зʼєднання, сповіщення). */
  .fl-foot { flex: 0 0 auto; display: flex; align-items: center; gap: 4px; padding: 8px; border-top: 1px solid var(--border); background: rgba(0, 0, 0, .12); border-radius: 0 0 var(--r-lg, 16px) var(--r-lg, 16px); }
  .fl-priv { flex: 1; min-width: 0; display: flex; align-items: center; justify-content: space-between; gap: 8px; padding: 8px 10px; border: 1px solid transparent; border-radius: 11px; background: transparent; cursor: pointer; color: var(--text-mute); font: 500 12.5px var(--font-ui); transition: background .15s, color .15s, border-color .15s; }
  .fl-priv:hover { background: var(--hover); border-color: var(--border); color: var(--orange); }
  .fl-priv-l { display: flex; align-items: center; gap: 10px; min-width: 0; }
  .fl-priv-ic { width: 26px; height: 26px; flex: 0 0 auto; display: grid; place-items: center; border-radius: 8px; font-size: 14px; color: var(--orange); background: rgba(var(--orange-rgb), .12); border: 1px solid rgba(var(--orange-rgb), .25); }
  .fl-priv-t { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .fl-priv-ch { font-size: 14px; color: var(--text-dim); flex: 0 0 auto; }
  .fl-fb { position: relative; flex: 0 0 auto; width: 32px; height: 32px; display: grid; place-items: center; border: 1px solid transparent; border-radius: 10px; background: transparent; color: var(--text-mute); cursor: pointer; font-size: 15px; transition: background .15s, color .15s; }
  .fl-fb:hover:not(:disabled) { background: var(--hover); color: var(--text); }
  .fl-fb.on { color: var(--orange); background: rgba(var(--orange-rgb), .12); }
  .fl-fb:disabled { opacity: .5; cursor: default; }
  .fl-conn { position: absolute; right: 4px; bottom: 4px; width: 7px; height: 7px; border-radius: 50%; background: var(--text-dim); border: 1.5px solid var(--bg-panel); box-sizing: content-box; }
  .fl-conn.on { background: var(--ff-ok); } .fl-conn.wait { background: var(--ff-warn); }

  @media (prefers-reduced-motion: reduce) { .fl-skel, .fl-menu { animation: none; } }
</style>
