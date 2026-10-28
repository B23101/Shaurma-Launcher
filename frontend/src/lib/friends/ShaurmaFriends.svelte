<script lang="ts">
  // ══════════════════════════════════════════════════════════════════════════
  // Друзі Shaurma — вкладка розділу «Друзі». ТОНКА збірка з модулів:
  //
  //   FriendsStore      — уся логіка й стан (без розмітки)
  //   FriendsToolbar    — зʼєднання + швидкі дії
  //   FriendsList       — ліва панель (чати / запити) → FriendRow, RequestRow
  //   ChatStage         — права панель (діалог або заглушка) → ShaurmaChat
  //   AddFriendDialog   — модалка «Додати друга»
  //   PrivacyDialog     — модалка «Приватність і сповіщення»
  //   ProfileModal (mode="friend") / TempInviteModal — як і раніше
  //
  // Макет: сітка «список | чат» із контейнерними запитами. Ширше 720px —
  // дві панелі поруч (список можна згорнути в рейку з іконками), вужче —
  // по одній панелі, «назад» у шапці чату. Ніщо не накладається: усі
  // елементи мають min-width:0, фіксовану висоту рядків і обрізання тексту.
  // ══════════════════════════════════════════════════════════════════════════
  import { onMount } from 'svelte'
  import { App } from '../wails'
  import { windowActive } from '../windowActive.svelte'
  import { t } from '../i18n.svelte'
  import { FriendsStore, provideFriendsStore, LIST_W_RAIL } from './FriendsStore.svelte'
  import FriendsToolbar from './FriendsToolbar.svelte'
  import FriendsList from './FriendsList.svelte'
  import ChatStage from './ChatStage.svelte'
  import AddFriendDialog from './AddFriendDialog.svelte'
  import PrivacyDialog from './PrivacyDialog.svelte'
  import ProfileModal from '../ProfileModal.svelte'
  import TempInviteModal from './TempInviteModal.svelte'
  import EmptyState from './EmptyState.svelte'
  import './ff.css'

  let { openAccountId = '', licensed = false, activeId = $bindable(''), onOpenMod, lead }: {
    // accountId, для якого віджет сповіщень у трей попросив відкрити діалог.
    openAccountId?: string
    // Активний акаунт — ліцензійний (Microsoft): підказки на екрані входу інші.
    licensed?: boolean
    // Відкритий діалог живе в батьківському компоненті, щоб переживати
    // перехід на іншу сторінку лаунчера й назад.
    activeId?: string
    onOpenMod?: (source: string, projectId: string) => void
    // Необов'язково: перемикач джерел (Шаурма / Minecraft) у лівій частині тулбара.
    lead?: import('svelte').Snippet
  } = $props()

  const s = provideFriendsStore(new FriendsStore((id) => { activeId = id }))

  let addOpen = $state(false)
  let privacyOpen = $state(false)
  let width = $state(0)

  const activePeer = $derived(activeId ? s.peerById(activeId) : null)

  onMount(() => s.start())

  // Дані, що залежать від списку друзів: голови, «колишні», незнайомці.
  $effect(() => { s.syncHeads() })
  $effect(() => { void s.st.me; s.syncFormer() })
  $effect(() => { s.syncStrangers() })

  // Бекенд має знати, що користувач бачить діалог, щоб не слати тост про те,
  // що й так на екрані, але слати в усіх інших випадках.
  // Неактивне вікно лаунчера = користувач нічого не бачить: тост НЕ ховаємо.
  $effect(() => {
    const id = activePeer ? activePeer.accountId : ''
    App.SetSocialView(windowActive.active, id).catch(() => {})
  })

  // Вихідні запити: підтягуємо при відкритті «Запитів» і коли змінився склад друзів
  // (друг прийняв запит).
  $effect(() => {
    const need = s.st.loggedIn && (s.listTab === 'requests' || s.rail)
    void s.st.friends.length
    if (need) void s.loadSent()
  })

  // Вузьке вікно → по одній панелі (див. також @container нижче).
  $effect(() => { if (width > 0) s.narrow = width <= 720 })

  // Запит із трей-віджета споживається ОДИН раз на кожну зміну цілі.
  let consumed = ''
  $effect(() => {
    if (openAccountId && openAccountId !== consumed) {
      consumed = openAccountId
      activeId = openAccountId
    }
  })
</script>

<div class="sf" class:chat-open={!!activePeer} bind:clientWidth={width}>
  {#if s.loading}
    <FriendsToolbar minimal {lead} onAdd={() => {}} onPrivacy={() => {}} />
    <div class="sf-state"><div class="sf-spin"><i class="ti ti-loader-2 ff-spin"></i></div><span>{t('friends.loading')}</span></div>

  {:else if !s.st.loggedIn}
    <FriendsToolbar minimal {lead} onAdd={() => {}} onPrivacy={() => {}} />
    <div class="sf-state">
      <EmptyState icon="ti-user-shield" title={t('social.loginTitle')} hint={t('social.loginHint')}>
        <button class="btn btn-primary btn-sm" onclick={() => window.dispatchEvent(new CustomEvent('open-settings-skin-account'))}>
          <i class="ti ti-user-plus"></i> {t('social.loginBtn')}
        </button>
      </EmptyState>
      {#if s.st.error === 'device_revoked'}
        <div class="sf-note">
          <b><i class="ti ti-device-desktop-off"></i> {t('dev.revokedNotice.title')}</b>
          <span>{t('dev.revokedNotice.text')}</span>
        </div>
      {:else if s.st.error}
        <div class="sf-note warn"><i class="ti ti-alert-triangle"></i><span>{s.codeText(s.st.error)}</span></div>
      {/if}
    </div>

  {:else}
    <!-- Макет як у налаштувань лаунчера: зверху рядок дій (FriendsToolbar),
         під ним меню-список друзів зліва і діалог у картці справа. -->
    <div class="sf-body" class:rail={s.rail} class:resizing={s.listResizing}
         style="--fl-w:{s.rail ? LIST_W_RAIL : s.listW}px">
      <FriendsList {activeId} onAdd={() => (addOpen = true)} />
      <div class="sf-tb"><FriendsToolbar {lead} onAdd={() => (addOpen = true)} onPrivacy={() => (privacyOpen = true)} /></div>
      <ChatStage peer={activePeer} onBack={() => (activeId = "")} {onOpenMod} onAdd={() => (addOpen = true)} onPrivacy={() => (privacyOpen = true)} />
    </div>
  {/if}
</div>

<AddFriendDialog open={addOpen} onclose={() => (addOpen = false)} />
<PrivacyDialog open={privacyOpen} onclose={() => (privacyOpen = false)} />

<ProfileModal mode="friend" friend={s.profilePeer} head={s.profilePeer ? (s.heads[s.profilePeer.nickname] ?? '') : ''}
  busy={!!s.profilePeer && s.busy === s.profilePeer.accountId}
  muted={!!s.profilePeer && s.isMuted(s.profilePeer.accountId)}
  blocked={!!s.profilePeer && s.blocked.some((b) => b.accountId === s.profilePeer?.accountId)}
  onmute={(f) => s.toggleMutePeer(f)}
  onblock={(f) => s.blockPeer(f)}
  onclose={() => (s.profileId = '')}
  onmessage={(f) => { s.profileId = ''; s.openChat(f) }}
  onremove={(f) => s.remove(f)} />

{#if s.tempInviteFor}
  <TempInviteModal friends={s.st.friends} preselect={s.tempInviteFor} onclose={() => (s.tempInviteFor = '')} />
{/if}

<style>
  .sf { container: sf / inline-size; display: flex; flex-direction: column; flex: 1 1 0; min-width: 0; min-height: 0; height: 100%; }

  /* Сітка: [рядок дій на всю ширину] / [список-меню | картка діалогу].
     Ширина списку — --fl-w (перетягування краю, 220–400px; рейка 68px),
     зміна ширини анімується тим самим easing, що й у головного sidebar. */
  .sf-body { flex: 1 1 0; min-height: 0; min-width: 0; display: grid; gap: 18px 24px;
    grid-template-columns: var(--fl-w, 264px) minmax(0, 1fr);
    grid-template-rows: auto minmax(0, 1fr);
    grid-template-areas: "tb tb" "list cs";
    transition: grid-template-columns .22s cubic-bezier(.4, 0, .2, 1); }
  .sf-body.resizing { transition: none; }
  .sf-body > :global(.fl) { grid-area: list; }
  .sf-body > :global(.cs) { grid-area: cs; }
  .sf-tb { grid-area: tb; min-width: 0; }
  .sf-body > :global(*) { min-width: 0; min-height: 0; }

  .sf-state { flex: 1 1 0; min-height: 0; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 12px; padding: 20px; overflow-y: auto; color: var(--text-mute); font: 500 13px/1.4 var(--font-ui); }
  .sf-spin { width: 52px; height: 52px; display: grid; place-items: center; border-radius: 17px; background: rgba(var(--orange-rgb), .1); border: 1px solid rgba(var(--orange-rgb), .24); }
  .sf-spin i { font-size: 24px; color: var(--orange); }
  .sf-note { display: flex; flex-direction: column; gap: 4px; max-width: 420px; padding: 10px 12px; border-radius: 11px; font: 400 11.5px/1.5 var(--font-ui); color: var(--text);
    border: 1px solid rgba(var(--orange-rgb), .3); background: rgba(var(--orange-rgb), .07); }
  .sf-note b { display: flex; align-items: center; gap: 6px; } .sf-note i { color: var(--orange); }
  .sf-note.warn { flex-direction: row; align-items: flex-start; gap: 8px; border-color: color-mix(in srgb, var(--ff-warn) 40%, transparent); background: color-mix(in srgb, var(--ff-warn) 9%, transparent); }
  .sf-note.warn i { color: var(--ff-warn); }

  /* ≤720px: одна панель за раз. */
  @container sf (max-width: 720px) {
    .sf-body, .sf-body.rail { grid-template-columns: minmax(0, 1fr); grid-template-areas: "tb" "main"; transition: none; gap: 10px; }
    .sf-body > :global(.fl), .sf-body > :global(.cs) { grid-area: main; }
    .sf-body > :global(.cs) { display: none; }
    .sf.chat-open .sf-body > :global(.fl) { display: none; }
    .sf.chat-open .sf-body > :global(.cs) { display: flex; }
    .sf.chat-open .sf-tb { display: none; }
  }
</style>
