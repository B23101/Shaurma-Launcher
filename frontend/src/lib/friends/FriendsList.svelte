<script lang="ts">
  // ── Ліва панель: «Чати» / «Запити» ───────────────────────────────────────
  // Вкладки — той самий ковзний таббар, що на сторінках збірки й сервера, пошук, групи друзів за
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

  let { activeId = '', onAdd }: { activeId?: string; onAdd: () => void } = $props()
  const s = useFriendsStore()

  const compact = $derived(s.rail)
  const showControls = $derived(s.listTab === 'chats' && !compact && s.st.friends.length > 0)
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
          {#if s.hasUnread}<i class="fl-dot"></i>{/if}
        </button>
        <button class="tab-btn fl-tab" role="tab" aria-selected={s.listTab === 'requests'} class:active={s.listTab === 'requests'} onclick={() => (s.listTab = 'requests')}>
          <i class="ti ti-user-plus"></i><span>{t('friends.tab.requests')}</span>
          {#if s.st.invites.length > 0}<span class="ff-badge">{s.st.invites.length}</span>{/if}
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
      <button class="fl-chip" class:on={s.statusFilter === 'all'} aria-pressed={s.statusFilter === 'all'} onclick={() => (s.statusFilter = 'all')}><span class="fl-chip-t">{t('ff.filter.all')}</span><b>{s.st.friends.length}</b></button>
      <button class="fl-chip online" class:on={s.statusFilter === 'online'} aria-pressed={s.statusFilter === 'online'} onclick={() => (s.statusFilter = 'online')}><i></i><span class="fl-chip-t">{t('social.group.online')}</span><b>{s.onlineCount}</b></button>
      <button class="fl-chip game" class:on={s.statusFilter === 'in_game'} aria-pressed={s.statusFilter === 'in_game'} onclick={() => (s.statusFilter = 'in_game')}><i></i><span class="fl-chip-t">{t('social.group.in_game')}</span><b>{s.inGameCount}</b></button>
    </div>
  {/if}

  <div class="fl-scroll">
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

    {#if s.listTab === 'requests' && !compact && s.st.invites.length === 0 && s.sent.length === 0}
      <EmptyState icon="ti-user-heart" title={t('friends.requests.empty')} hint={t('friends.requests.emptyHint')}>
        <button class="btn btn-primary btn-sm" onclick={onAdd}><i class="ti ti-user-plus"></i> {t('friends.addFriend')}</button>
      </EmptyState>
    {/if}

    {#if showChats}
      {#if s.st.friends.length === 0 && s.otherChats.length === 0}
        <EmptyState icon="ti-users" title={t('social.empty')} hint={t('ff.empty.hint')} {compact}>
          <button class="btn btn-primary btn-sm" onclick={onAdd}><i class="ti ti-user-plus"></i> {t('friends.addFriend')}</button>
        </EmptyState>
      {:else if s.st.friends.length > 0 && s.groups.length === 0}
        <EmptyState icon="ti-search-off" title={t('social.searchEmpty')} {compact} />
      {:else}
        {#each s.groups as grp (grp.key)}
          <section class="fl-sec">
            {#if !compact}<h3 class="fl-sh"><i class="ti {grp.icon}"></i>{t(`social.group.${grp.key}`)}<span>{grp.list.length}</span></h3>{/if}
            {#each grp.list as f (f.accountId)}
              <FriendRow {f} selected={activeId === f.accountId} {compact} animate={s.ready} />
            {/each}
          </section>
        {/each}
      {/if}

      {#if s.otherChats.length > 0}
        <section class="fl-sec">
          {#if !compact}
            <button class="fl-sh fl-toggle" onclick={() => (s.formerOpen = !s.formerOpen)} aria-expanded={s.formerOpen}>
              <i class="ti {s.formerOpen ? 'ti-chevron-down' : 'ti-chevron-right'}"></i>{t('social.former.title')}<span>{s.otherChats.length}</span>
            </button>
          {/if}
          {#if s.formerOpen || s.strangerRows.length > 0 || compact}
            {#each s.otherChats as f (f.accountId)}
              <FriendRow {f} readonly selected={activeId === f.accountId} {compact} animate={s.ready} subtitle={t('social.former.sub')} />
            {/each}
          {/if}
        </section>
      {/if}
    {/if}
  </div>
</aside>

<style>
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
