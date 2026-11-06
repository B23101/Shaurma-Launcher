<script lang="ts">
  // ── Ліва панель: «Чати» / «Запити» ───────────────────────────────────────
  // Єдиний список приватних і групових чатів за часом останнього повідомлення.
  // Вкладки — той самий ковзний таббар, що на сторінках збірки й сервера, пошук, фільтр за
  // статусом, запити, «інші чати». Режим «лише іконки» (рейка 72px) — для тих,
  // кому потрібен максимум місця під чат.
  import { useFriendsStore, LIST_W_MIN, LIST_W_MAX } from './FriendsStore.svelte'
  import { t } from '../i18n.svelte'
  import { tip } from './tooltip'
  import FriendRow from './FriendRow.svelte'
  import RequestRow from './RequestRow.svelte'
  import SentRow from './SentRow.svelte'
  import EmptyState from './EmptyState.svelte'
  import FriendsNotices from './FriendsNotices.svelte'
  import { slideTabs } from '../slideTabs'
  import GroupsPanel from '../groups/GroupsPanel.svelte'
  import GroupAvatar from '../groups/GroupAvatar.svelte'
  import type { SocialFriend } from '../types'
  import type { Group } from '../groups/groupTypes'
  import { useGroupsStore } from '../groups/GroupsStore.svelte'
  import { toMs } from './activityCache'
  import { onMount } from 'svelte'

  let { activeId = '', onAdd, onCreateGroup }: { activeId?: string; onAdd: () => void; onCreateGroup: () => void } = $props()
  const s = useFriendsStore()
  const gs = useGroupsStore()

  type ChatRow = { key: string; name: string; time: number; friend?: SocialFriend; group?: Group }
  const chats = $derived.by(() => {
    const q = s.search.trim().toLowerCase()
    const people = [...s.filtered, ...s.otherChats.filter(f => !q || f.nickname.toLowerCase().includes(q))]
    const rows: ChatRow[] = people.map(f => ({ key: 'friend:' + f.accountId, name: f.nickname, time: s.activity[f.accountId] ?? 0, friend: f }))
    if (s.statusFilter === 'all') for (const g of gs.groups) {
      if (!q || g.title.toLowerCase().includes(q)) rows.push({ key: 'group:' + g.id, name: g.title, time: gs.activity[g.id] ?? toMs(g.createdAt), group: g })
    }
    return rows.sort((a, b) => b.time - a.time || a.name.localeCompare(b.name) || a.key.localeCompare(b.key))
  })
  // Порядок «останніх активних» береться з локального кешу ОДРАЗУ (restoreActivity), мережа лише уточнює —
  // тому список більше не «прогрівається» й не стрибає після відкриття.
  $effect(() => { void s.me; void gs.me; s.restoreActivity(); gs.restoreActivity() })
  $effect(() => { s.syncActivity([...s.st.friends, ...s.otherChats].map(f => f.accountId)) })
  // Поки порядок чатів невідомий (перший запуск без кешу), замість списку в «неправильному» порядку, який потім
  // стрибне, показуємо заготовки. Із кешем (звичайний запуск) цього стану немає зовсім. Запасний таймер — щоб
  // список не лишився прихованим, якщо сервер недоступний.
  let waited = $state(false)
  onMount(() => { const id = setTimeout(() => (waited = true), 2500); return () => clearTimeout(id) })
  const orderReady = $derived(waited || (s.activityReady && (gs.loaded ? gs.activityReady || gs.groups.length === 0 : false)))

  // Історію групи вантажимо наперед (наведення з невеликою затримкою або натискання), тож до кліку вона вже тут.
  let hoverT: ReturnType<typeof setTimeout> | undefined
  const hoverGroup = (id: string) => { clearTimeout(hoverT); hoverT = setTimeout(() => gs.prefetch(id), 120) }
  const leaveGroup = () => clearTimeout(hoverT)

  // Захист від «клікнув одне — відкрилось інше»: якщо між натисканням і відпусканням
  // кнопки миші список переставився (прийшло повідомлення), клік на чужий рядок ігноруємо.
  let pressedKey = ''
  const rowKey = (e: Event) => (e.target as HTMLElement | null)?.closest<HTMLElement>('[data-chat-key]')?.dataset.chatKey ?? ''
  function rememberPress(e: PointerEvent) { pressedKey = rowKey(e) }
  function guardClick(e: MouseEvent) {
    const k = rowKey(e)
    if (pressedKey && k && k !== pressedKey && e.detail > 0) { e.stopPropagation(); e.preventDefault() }
    pressedKey = ''
  }
  const compact = $derived(s.rail)
  const showControls = $derived(s.listTab === 'chats' && !compact && (s.st.friends.length > 0 || gs.groups.length > 0))
  // У стислому списку рядків запитів у друзі немає: вони з'являлись/зникали над чатами й зсували їх
  // просто під курсором. Про вхідні нагадує бейдж на кнопці «Розгорнути».
  const showRequests = $derived(!compact && s.listTab === 'requests')
  const showChats = $derived(compact || s.listTab === 'chats')

  // Перетягування правого краю — та сама механіка, що й у головного sidebar
  // (startSidebarResize в App.svelte): 220–400px, збереження при відпусканні.
  function startResize(e: MouseEvent) {
    e.preventDefault()
    if (compact || s.narrow) return
    const startX = e.clientX
    const startW = s.listW
    s.listResizing = true
    document.body.style.userSelect = 'none'
    document.body.style.cursor = 'col-resize'
    const move = (ev: MouseEvent) => s.setListW(startW + (ev.clientX - startX))
    const up = () => {
      s.listResizing = false
      document.body.style.userSelect = ''
      document.body.style.cursor = ''
      window.removeEventListener('mousemove', move)
      window.removeEventListener('mouseup', up)
      s.setListW(s.listW, true)
    }
    window.addEventListener('mousemove', move)
    window.addEventListener('mouseup', up)
  }
  function resizeKey(e: KeyboardEvent) {
    if (e.key === 'ArrowLeft') { e.preventDefault(); s.setListW(s.listW - 16, true) }
    else if (e.key === 'ArrowRight') { e.preventDefault(); s.setListW(s.listW + 16, true) }
  }
</script>

<aside class="fl" class:compact aria-label={t('friends.title')}>
  <!-- Кнопка згортання на краю та ресайз-хендл — ті самі глобальні класи, що
       й у головного sidebar (.sidebar-collapse-btn / .sb-resize). -->
  {#if !s.narrow}
    <button class="sidebar-collapse-btn" onclick={() => s.setCompact(!s.compact)} aria-pressed={compact}
            use:tip={compact ? t('social.list.expand') : t('social.list.collapse')}
            aria-label={compact ? t('social.list.expand') : t('social.list.collapse')}>
      <i class="ti ti-chevron-left"></i>
      {#if compact && s.st.invites.length > 0}<span class="ff-badge fl-collapse-badge">{s.st.invites.length}</span>{/if}
    </button>
  {/if}
  {#if !compact && !s.narrow}
    <div class="sb-resize" class:dragging={s.listResizing} onmousedown={startResize} onkeydown={resizeKey}
         role="separator" aria-orientation="vertical" aria-valuemin={LIST_W_MIN} aria-valuemax={LIST_W_MAX} aria-valuenow={s.listW}
         tabindex="0" title={t('sidebar.resize')}></div>
  {/if}
  <div class="fl-top">
    {#if !compact}
      <div class="tabbar tb-slide fl-tabs" role="tablist" use:slideTabs>
        <button class="tab-btn fl-tab" role="tab" aria-selected={s.listTab === 'chats'} class:active={s.listTab === 'chats'} onclick={() => (s.listTab = 'chats')}>
          <i class="ti ti-message-circle-2"></i><span>{t('friends.tab.chats')}</span>
          {#if s.hasUnread || gs.totalUnread > 0}<i class="fl-dot"></i>{/if}
        </button>
        <button class="tab-btn fl-tab" role="tab" aria-selected={s.listTab === 'requests'} class:active={s.listTab === 'requests'} onclick={() => (s.listTab = 'requests')}>
          <i class="ti ti-user-plus"></i><span>{t('friends.tab.requests')}</span>
          {#if s.st.invites.length + gs.invites.length > 0}<span class="ff-badge">{s.st.invites.length + gs.invites.length}</span>{/if}
        </button>
      </div>
    {/if}
  </div>

  {#if !compact}<FriendsNotices />{/if}

  {#if showControls}
    <div class="fl-search-row">
      <div class="fl-search">
        <i class="ti ti-search"></i>
        <input bind:value={s.search} placeholder={t('social.search')} aria-label={t('social.search')} spellcheck="false" autocomplete="off" />
        {#if s.search}<button class="fl-clear" onclick={() => (s.search = '')} aria-label={t('friends.close')}><i class="ti ti-x"></i></button>{/if}
      </div>
    </div>
    <div class="fl-chips" role="group">
      <button class="fl-chip" class:on={s.statusFilter === 'all'} aria-pressed={s.statusFilter === 'all'} onclick={() => (s.statusFilter = 'all')}><span class="fl-chip-t">{t('ff.filter.all')}</span><b>{s.st.friends.length + gs.groups.length + s.otherChats.length}</b></button>
      <button class="fl-chip online" class:on={s.statusFilter === 'online'} aria-pressed={s.statusFilter === 'online'} onclick={() => (s.statusFilter = 'online')}><i></i><span class="fl-chip-t">{t('social.group.online')}</span><b>{s.onlineCount}</b></button>
      <button class="fl-chip game" class:on={s.statusFilter === 'in_game'} aria-pressed={s.statusFilter === 'in_game'} onclick={() => (s.statusFilter = 'in_game')}><i></i><span class="fl-chip-t">{t('social.group.in_game')}</span><b>{s.inGameCount}</b></button>
    </div>
  {/if}

  <div class="fl-scroll" onpointerdowncapture={rememberPress} onclickcapture={guardClick}>
    {#if showRequests && s.st.invites.length > 0}
      <section class="fl-sec">
        {#if !compact}<h3 class="fl-sh"><i class="ti ti-user-down"></i>{t('friends.incoming')}<span>{s.st.invites.length}</span></h3>{/if}
        {#each s.st.invites as inv (inv.id)}<RequestRow {inv} {compact} />{/each}
      </section>
    {/if}

    {#if !compact && s.listTab === 'requests' && s.sent.length > 0}
      <section class="fl-sec">
        {#if !compact}<h3 class="fl-sh"><i class="ti ti-user-up"></i>{t('friends.outgoing')}<span>{s.sent.length}</span></h3>{/if}
        {#each s.sent as inv (inv.id)}<SentRow {inv} {compact} />{/each}
      </section>
    {/if}

    {#if s.listTab === 'requests' && !compact && s.st.invites.length === 0 && s.sent.length === 0 && gs.invites.length === 0}
      <EmptyState icon="ti-user-heart" title={t('friends.requests.empty')} hint={t('friends.requests.emptyHint')}>
        <button class="btn btn-primary btn-sm" onclick={onAdd}><i class="ti ti-user-plus"></i> {t('friends.addFriend')}</button>
      </EmptyState>
    {/if}

    {#if showRequests && gs.invites.length > 0}
      <GroupsPanel invitesOnly oncreate={onCreateGroup} />
    {/if}
    {#if showChats}
      {#if !compact && gs.enabled}
        <button class="btn btn-ghost btn-sm fl-new" onclick={onCreateGroup}><i class="ti ti-users-plus"></i>{t('groups.new')}</button>
      {/if}
      {#if !orderReady}
        {#each [0, 1, 2, 3, 4, 5] as i (i)}<div class="fl-skel" class:compact style="--i:{i}" aria-hidden="true"></div>{/each}
      {:else}
      {#each chats as row (row.key)}
        <div class="fl-row" data-chat-key={row.key}>
        {#if row.group}
          {@const gr = row.group}
          <button class="fl-group" class:active={gs.activeId === gr.id} class:compact onclick={() => gs.open(gr.id)} title={gr.title}
                  onpointerenter={() => hoverGroup(gr.id)} onpointerleave={leaveGroup} onfocus={() => hoverGroup(gr.id)} onpointerdown={() => gs.prefetch(gr.id)}>
            <GroupAvatar title={gr.title} icon={gr.settings.icon} color={gr.settings.color} />
            {#if !compact}<span><b>{gr.title}{#if s.isMuted(gr.id)} <i class="ti ti-bell-off" aria-label={t('groups.mute')}></i>{/if}</b><small>{t('groups.members', { n: gr.members.length })}</small></span>{/if}
            {#if gs.unread[gr.id]}<span class="ff-badge">{gs.unread[gr.id]}</span>{/if}
          </button>
        {:else if row.friend}
          <FriendRow f={row.friend} readonly={!s.isFriend(row.friend.accountId)} selected={activeId === row.friend.accountId} {compact} animate={false} />
        {/if}
        </div>
      {:else}
        <EmptyState icon="ti-message-circle-2" title={s.search ? t('social.searchEmpty') : t('social.empty')} {compact} />
      {/each}
      {/if}
    {/if}
  </div>
</aside>

<style>
  .fl-row { display: contents; }
  .fl-skel { height: 58px; margin-bottom: 6px; border-radius: 12px; background: var(--bg-card); opacity: .6; animation: fl-skel 1.2s ease-in-out infinite; animation-delay: calc(var(--i) * 90ms); }
  .fl-skel.compact { width: 44px; height: 44px; margin: 0 auto 8px; border-radius: 50%; }
  @keyframes fl-skel { 50% { opacity: .25; } }
  @media (prefers-reduced-motion: reduce) { .fl-skel { animation: none; } }
  .fl-new { width: 100%; justify-content: center; margin-bottom: 8px; }
  .fl-group { width: 100%; min-height: 58px; display: flex; align-items: center; gap: 10px; padding: 9px 10px; border: 1px solid transparent; border-radius: 12px; background: transparent; color: var(--text); text-align: left; cursor: pointer; }
  .fl-group:hover { background: var(--hover); }
  .fl-group.active { background: rgba(var(--orange-rgb), .12); }
  .fl-group > span:not(.ff-badge) { display: flex; flex: 1; min-width: 0; flex-direction: column; gap: 4px; }
  .fl-group b { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 13px; }
  .fl-group small { color: var(--text-mute); font-size: 12px; }
  .fl-group.compact { justify-content: center; position: relative; }
  .fl-group.compact .ff-badge { position: absolute; top: 2px; right: 2px; }
  .fl-group:focus-visible { outline: 2px solid var(--orange); outline-offset: -2px; }

  /* Список — як бокове меню налаштувань (.st-nav): без власної картки, на
     фоні сторінки; заголовки груп — дрібні великі літери, активний чат —
     помаранчевий відтінок. position:relative — для кнопки згортання й
     ресайз-хендла на правому краю (у проміжку між списком і діалогом). */
  .fl { --fl-bg: var(--bg-app); position: relative; display: flex; flex-direction: column; min-width: 0; min-height: 0; }
  .fl :global(.sb-resize) { right: -14px; border-radius: 3px; }
  .fl:hover :global(.sb-resize) { opacity: 1; }
  .fl :global(.sidebar-collapse-btn) { top: 8px; right: -23px; }
  .fl.compact :global(.sidebar-collapse-btn) { transform: rotate(180deg); }
  .fl-top { display: flex; align-items: center; gap: 5px; padding: 0 0 10px; flex: 0 0 auto; }
  .fl.compact .fl-top { display: none; }
  .fl.compact .fl-search-row, .fl.compact .fl-chips { display: none; }
  .fl-collapse-badge { position: absolute; top: -9px; right: -9px; min-width: 16px; height: 16px; padding: 0 4px; font-size: var(--fs-min); pointer-events: none; transform: rotate(0); }
  .fl.compact .fl-collapse-badge { transform: rotate(180deg); }

  /* Таббар «Чати / Запити» — дві рівні половини на всю ширину картки. */
  .fl-tabs.tabbar { flex: 1; min-width: 0; width: auto; margin: 0; padding: 4px; border-radius: 12px; }
  .fl-tabs.tabbar::before { top: 4px; bottom: 4px; border-radius: 10px; }
  .fl-tabs.tabbar::after { bottom: 4px; }
  .fl-tabs .fl-tab.tab-btn { flex: 1 1 0; min-width: 0; justify-content: center; gap: 6px; padding: 8px 6px; font-size: 11.5px; }
  .fl-tab span:not(.ff-badge) { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .fl-tab i.ti { font-size: 14px; flex: 0 0 auto; }
  .fl-dot { width: 7px; height: 7px; border-radius: 50%; background: var(--orange); flex: 0 0 auto; box-shadow: 0 0 0 3px rgba(var(--orange-rgb), .22); }

  .fl-search-row { display: flex; align-items: center; gap: 5px; padding: 0 0 8px; flex: 0 0 auto; }
  /* Поле пошуку — як пошук налаштувань (.st-search). */
  .fl-search { position: relative; flex: 1; min-width: 0; display: flex; align-items: center; gap: 8px; height: 36px; padding: 0 8px 0 12px; box-sizing: border-box;
    border-radius: 12px; background: var(--bg-card); border: 1px solid var(--border-hi); transition: border-color .15s, box-shadow .15s; }
  .fl-search:focus-within { border-color: rgba(var(--orange-rgb), .7); box-shadow: 0 0 0 3px rgba(var(--orange-rgb), .15); }
  .fl-search i { color: var(--text-mute); font-size: 15px; }
  .fl-search input { flex: 1; min-width: 0; background: none; border: 0; outline: none; color: var(--text); font: 500 13px var(--font-ui); }
  .fl-search input::placeholder { color: var(--text-dim); }
  .fl-clear { display: grid; place-items: center; width: 24px; height: 24px; border: 0; border-radius: 8px; background: none; color: var(--text-mute); cursor: pointer; }
  .fl-clear:hover { color: var(--text); background: var(--hover-strong); }

  /* Фільтри — пігулки, як .st-pill у налаштуваннях. */
  .fl-chips { display: flex; gap: 5px; padding: 0 0 6px; flex: 0 0 auto; min-width: 0; }
  .fl-chip { display: inline-flex; align-items: center; gap: 6px; height: 28px; padding: 0 9px; flex: 1 1 auto; justify-content: center; min-width: 0; border-radius: 999px;
    border: 1px solid var(--border); background: var(--bg-card); color: var(--text-mute); cursor: pointer; white-space: nowrap;
    font: 600 11.5px/1 var(--font-ui); transition: background-color .15s, border-color .15s, color .15s; }
  .fl-chip-t { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .fl-chip:hover { color: var(--text); border-color: var(--border-hi); }
  .fl-chip b { flex: 0 0 auto; font: 700 var(--fs-min)/1 var(--font-mono); color: var(--text-dim); }
  .fl-chip i { width: 7px; height: 7px; border-radius: 50%; flex: 0 0 auto; }
  .fl-chip.online i { background: var(--ff-ok); }
  .fl-chip.game i { background: var(--orange); }
  .fl-chip.on { color: var(--orange); background: rgba(var(--orange-rgb), .14); border-color: rgba(var(--orange-rgb), .45); }
  .fl-chip.on b { color: var(--orange); }
  .fl-chip:focus-visible { outline: 2px solid var(--orange); outline-offset: -2px; }

  .fl-scroll { flex: 1 1 0; min-height: 0; overflow-y: auto; overflow-x: hidden; overscroll-behavior: contain; padding: 0 4px 16px 0; scrollbar-width: thin; }
  .fl.compact .fl-scroll { padding-top: 4px; scrollbar-width: none; }
  .fl.compact .fl-scroll::-webkit-scrollbar { display: none; }
  .fl-sec + .fl-sec { margin-top: 12px; }
  .fl.compact .fl-sec + .fl-sec { margin-top: 8px; padding-top: 8px; border-top: 1px solid var(--border); }
  /* Заголовок групи — як .st-nav-title. */
  .fl-sh { display: flex; align-items: center; gap: 7px; width: 100%; margin: 0; padding: 6px 12px 6px; box-sizing: border-box;
    font-size: var(--fs-min); font-weight: 700; letter-spacing: .9px; text-transform: uppercase; color: var(--text-dim); }
  .fl-sh i { font-size: 13px; }
  .fl-sh span { margin-left: auto; font: 700 var(--fs-min)/1 var(--font-mono); letter-spacing: 0; }
  .fl-toggle { background: none; border: 0; cursor: pointer; text-align: left; }
  .fl-toggle:hover { color: var(--text); }

</style>
