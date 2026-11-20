<script lang="ts">
  import GroupInviteBanner from '../groups/GroupInviteBanner.svelte'
  // ── Права панель: діалог або «домашня» сцена ─────────────────────────────
  // Чат перемонтується при зміні співрозмовника ({#key}) — потрібно, щоб історія
  // й скрол не змішувались. Поява — лише fade (CSS).
  //
  // Візуальний шар чату (шапка, бульбашки, поле вводу) перестилізований тут через
  // :global під .cs — сам ShaurmaChat (логіка, шифрування, стани) не чіпали.
  import { useFriendsStore } from './FriendsStore.svelte'
  import { App } from '../wails'
  import { t } from '../i18n.svelte'
  import ShaurmaChat from './ShaurmaChat.svelte'
  import type { SocialFriend } from '../types'
  import type { GroupChatInfo } from './chatApi'
  import { useGroupsStore } from '../groups/GroupsStore.svelte'
  import { groupChatApi } from '../groups/groupChatApi'
  import { GROUP_MAX_BODY, parseSystem, type Group } from '../groups/groupTypes'
  import GroupMembersModal from '../groups/GroupMembersModal.svelte'
  import GroupSettingsModal from '../groups/GroupSettingsModal.svelte'

  let { peer, group = null, onBack, onOpenMod, onAdd, onPrivacy }: {
    peer: SocialFriend | null
    // Груповий чат: той самий ShaurmaChat, лише з іншим джерелом даних (groups/groupChatApi.ts).
    group?: Group | null
    onBack: () => void
    onOpenMod?: (source: string, projectId: string) => void
    onAdd: () => void
    onPrivacy: () => void
  } = $props()
  const s = useFriendsStore()
  const g = useGroupsStore()

  let settingsOpen = $state(false)
  let membersOpen = $state(false)

  // Текст системної події групи — переклад за кодом, імена з довідника учасників.
  function sysText(gr: Group, body: string): string {
    const e = parseSystem(body)
    if (!e) return ''
    const key = `groups.sys.${e.e}`
    const txt = t(key)
    return txt === key ? '' : t(key, { a: e.a ? g.nameOf(e.a, gr) : '', by: e.by ? g.nameOf(e.by, gr) : '' })
  }
  function headOf(gr: Group, id: string): string {
    const nick = gr.members.find((m) => m.accountId === id)?.nickname ?? s.st.friends.find((f) => f.accountId === id)?.nickname ?? ''
    return nick ? s.heads[nick] ?? '' : ''
  }
  const info = $derived.by<GroupChatInfo | undefined>(() => {
    const gr = group
    if (!gr) return undefined
    return {
      id: gr.id, title: gr.title, icon: gr.settings.icon ?? '', color: gr.settings.color,
      memberIds: gr.members.map(m => m.accountId), members: gr.members.length, adminName: g.nameOf(gr.admin, gr), isAdmin: g.isAdmin(gr),
      canPin: g.canPin(gr, true), canUnpin: g.canPin(gr, false), typing: g.typingNames(gr.id), maxLen: GROUP_MAX_BODY,
      nameOf: (id) => g.nameOf(id, gr), headOf: (id) => headOf(gr, id), sysText: (b) => sysText(gr, b),
      onSettings: () => (settingsOpen = true),
      onMembers: () => (membersOpen = true),
      muted: s.isMuted(gr.id),
      toggleMute: () => { void s.toggleMutePeer({ accountId: gr.id, nickname: gr.title } as SocialFriend) },
      // Курсори «прочитано до» інших учасників групи (сервер часу прочитання не віддає).
      readCursors: async () => {
        const result = await App.GroupRequest('history', { groupId: gr.id, limit: 1 })
        return (result?.history?.reads ?? [])
          .filter((r: { by: string; upTo: number }) => r?.by && r.by !== s.me && gr.members.some(m => m.accountId === r.by))
          .map((r: { by: string; upTo: number }) => ({ by: r.by, upTo: Number(r.upTo) || 0 }))
      },
      // id учасників, які прочитали повідомлення (і всі новіші за нього).
      readers: async (messageId) => {
        const result = await App.GroupRequest('history', { groupId: gr.id, limit: 1 })
        return (result?.history?.reads ?? [])
          .filter((r: { by: string; upTo: number }) => r?.by && r.by !== s.me && Number(r.upTo) >= messageId && gr.members.some(m => m.accountId === r.by))
          .map((r: { by: string }) => r.by)
      },
      claim: (messageId, from) => { void g.claimBuild({ groupId: gr.id, id: messageId, from } as any) },
    }
  })
  // Джерело даних створюється раз на групу (залежить лише від id): інакше кожне оновлення групи
  // давало б новий об'єкт, і підтвердження надсилання (chat:ack) губились би.
  const gid = $derived(group?.id ?? '')
  const chatApi = $derived(gid ? groupChatApi(g, gid) : null)
  // Синтетичний «співрозмовник» для групового чату: ShaurmaChat передає його accountId у джерело даних.
  const groupPeer = $derived(group ? ({ accountId: group.id, nickname: group.title } as SocialFriend) : null)
</script>

<section class="cs" aria-live="polite">
  {#if group && groupPeer && info && chatApi}
    {#key group.id}
      <div class="cs-in">
        <ShaurmaChat peer={groupPeer} me={s.me} conn={s.st.conn} isFriend={false} allFriends={s.st.friends}
                     api={chatApi} group={info} {onBack} {onOpenMod} />
      </div>
    {/key}
  {:else if peer}
    {#key peer.accountId}
      <div class="cs-in">
        <GroupInviteBanner peerId={peer.accountId} />
        <ShaurmaChat {peer} isFriend={s.isFriend(peer.accountId)} allFriends={s.st.friends} me={s.me}
                     head={s.heads[peer.nickname] ?? ''} conn={s.st.conn}
                     {onBack} {onOpenMod} onOpenProfile={() => s.openProfile(peer)} />
      </div>
    {/key}
  {:else}
    <div class="cs-empty">
      <div class="cs-orb" aria-hidden="true">
        <i class="ti ti-message-circle-2 cs-main"></i>
      </div>
      <h2 class="cs-t">{t('social.placeholder.title')}</h2>
      <p class="cs-h">{t('social.placeholder.hint')}</p>
      <div class="cs-cards">
        <button class="cs-card" onclick={onAdd}>
          <span class="cs-ci"><i class="ti ti-user-plus"></i></span>
          <span class="cs-ct"><b>{t('friends.addFriend')}</b><small>{t('ff.card.addHint')}</small></span>
        </button>
        <button class="cs-card" onclick={() => s.copyInviteLink()}>
          <span class="cs-ci"><i class="ti ti-link"></i></span>
          <span class="cs-ct"><b>{t('ff.card.link')}</b><small>{t('ff.card.linkHint')}</small></span>
        </button>
        <button class="cs-card" onclick={onPrivacy}>
          <span class="cs-ci"><i class="ti ti-lock"></i></span>
          <span class="cs-ct"><b>{t('social.priv.title')}</b><small>{t('ff.card.privHint')}</small></span>
        </button>
      </div>
    </div>
  {/if}
</section>

{#if settingsOpen && group}
  <GroupSettingsModal {group} onmembers={() => { settingsOpen = false; membersOpen = true }} onclose={() => (settingsOpen = false)} />
{/if}

{#if membersOpen && group}
  <GroupMembersModal {group} friends={s.st.friends} onclose={() => (membersOpen = false)} />
{/if}

<style>
  /* Діалог — картка як .st-card у налаштуваннях: фон картки, рамка,
     заокруглення; шапка як .st-card-head, роздільники — var(--border). */
  .cs { position: relative; display: flex; flex-direction: column; min-width: 0; min-height: 0; overflow: hidden;
    background: var(--bg-panel); border: 1px solid var(--border); border-radius: var(--r-lg, 16px); }
  .cs-in { flex: 1 1 0; min-height: 0; display: flex; flex-direction: column; animation: cs-in .16s ease-out both; }
  /* Лише opacity: зсув по Y давав «стрибок» при кожному перемиканні чату. */
  @keyframes cs-in { from { opacity: .35; } to { opacity: 1; } }
  /* «Назад» потрібна лише коли панелі показуються по одній. */
  .cs :global(.sc-back) { display: none; }
  @container sf (max-width: 720px) { .cs :global(.sc-back) { display: flex; } }

  /* ── Домашня сцена: заголовок розділу + картки дій (як .st-card-head) ── */
  .cs-empty { flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 6px; padding: 28px 24px; text-align: center; overflow-y: auto; animation: cs-in .16s ease-out both; }
  .cs-orb { margin-bottom: 10px; flex: 0 0 auto; }
  .cs-main { display: grid; place-items: center; width: 52px; height: 52px; border-radius: 16px; font-size: 26px; color: var(--orange); background: rgba(var(--orange-rgb), .13); }
  .cs-t { margin: 0; font-size: 18px; font-weight: 800; color: var(--text); }
  .cs-h { margin: 0; max-width: 62ch; font-size: 13px; line-height: 1.5; color: var(--text-mute); }
  .cs-cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 10px; width: 100%; max-width: 660px; margin-top: 20px; }
  .cs-card { display: flex; align-items: center; gap: 12px; padding: 13px 15px; border-radius: var(--r-md, 12px); text-align: left; cursor: pointer; color: var(--text);
    background: var(--bg-card); border: 1px solid var(--border-hi); transition: border-color .15s, background-color .15s; }
  .cs-card:hover { border-color: rgba(var(--orange-rgb), .5); background: var(--hover); }
  .cs-card:active { transform: scale(.985); }
  .cs-card:focus-visible { outline: 2px solid var(--orange); outline-offset: 2px; }
  .cs-ci { flex: 0 0 auto; width: 34px; height: 34px; display: grid; place-items: center; border-radius: 10px; font-size: 17px; color: var(--orange); background: rgba(var(--orange-rgb), .13); }
  .cs-ct { min-width: 0; display: flex; flex-direction: column; gap: 2px; }
  .cs-ct b { font-size: 13.5px; font-weight: 700; overflow-wrap: anywhere; }
  .cs-ct small { font-size: 12px; line-height: 1.45; color: var(--text-mute); }

  /* ── Чат: шапка 58px (аватар + нік/@тег/статус — зліва, «Запросити» і «⋯» — справа) ── */
  .cs.cs :global(.sc-head) { box-sizing: border-box; min-height: 58px; gap: 11px; padding: 0 18px; border-bottom: 1px solid var(--border); background: var(--bg-panel); }
  .cs.cs :global(.sc-av.lg) { width: 36px; height: 36px; border-radius: 11px; }
  .cs.cs :global(.sc-av.lg img) { border-radius: 11px; }
  .cs.cs :global(.sc-head .sc-st) { border-color: var(--bg-panel); }
  .cs.cs :global(.sc-who) { flex: 1 1 0; min-width: 0; }
  .cs.cs :global(.sc-name) { display: flex; align-items: baseline; gap: 8px; font-size: 14px; font-weight: 700; white-space: nowrap; }
  .cs.cs :global(.sc-handle) { font: 500 12px var(--font-mono, ui-monospace, Consolas, monospace); color: var(--text-dim); overflow: hidden; text-overflow: ellipsis; }
  .cs.cs :global(.sc-sub) { font-size: 11.5px; color: var(--text-mute); }
  .cs.cs :global(.sc-hbtn) { width: 34px; height: 34px; border-radius: 11px; font-size: 16px; background: var(--bg-input, var(--bg-card)); border: 1px solid var(--border); }
  .cs.cs :global(.sc-hbtn:hover:not(:disabled)) { color: var(--orange); background: var(--hover); border-color: var(--border-hi); }
  /* «Запросити» — суцільна амброва кнопка. */
  .cs.cs :global(.sc-hbtn-txt) { width: auto; padding: 0 14px; font: 700 12.5px/1 var(--font-ui); color: var(--accent-ink, #0c0d11); border-color: transparent; background: var(--orange); box-shadow: none; }
  .cs.cs :global(.sc-hbtn-txt:hover:not(:disabled)) { color: var(--accent-ink, #0c0d11); border-color: transparent; background: var(--orange); filter: brightness(1.1); }
  .cs.cs :global(.sc-back) { width: 34px; height: 34px; border-radius: 11px; }
  .cs.cs :global(.sc-pinbar) { background: color-mix(in srgb, var(--bg-panel) 92%, var(--orange) 8%); }

  /* ── Чат: стрічка повідомлень ── */
  .cs.cs :global(.sc-msgs) { padding: 16px 20px 8px; background: var(--bg-panel); }
  .cs.cs :global(.sc-day) { padding: 3px 12px; font-size: 11px; background: var(--bg-main, var(--bg-input)); }
  .cs.cs :global(.sc-bub) { padding: 8px 13px 7px; border-radius: 18px; font-size: 13.5px; line-height: 1.44; background: var(--bg-card); border-color: var(--border); }
  .cs.cs :global(.sc-row:not(.own).gend .sc-bub) { border-bottom-left-radius: 4px; }
  .cs.cs :global(.sc-row.own .sc-bub) { background: linear-gradient(135deg, rgba(var(--orange-rgb), .17), rgba(var(--orange-rgb), .09)); border-color: rgba(var(--orange-rgb), .28); }
  .cs.cs :global(.sc-row.own.gend .sc-bub) { border-bottom-right-radius: 4px; }

  /* ── Чат: поле вводу — одна «пілюля» (поле + кнопка надсилання всередині) ── */
  .cs.cs :global(.sc-foot) { display: block; padding: 8px 18px 14px; border-top: 0; background: var(--bg-panel); }
  .cs.cs :global(.sc-form) { display: flex; align-items: center; gap: 8px; padding: 6px 8px; border-radius: 16px; background: var(--bg-main, var(--bg-input)); border: 1px solid var(--border); transition: border-color .15s; }
  .cs.cs :global(.sc-form:focus-within) { border-color: rgba(var(--orange-rgb), .55); }
  .cs.cs :global(.sc-input) { flex: 1; min-width: 0; padding: 7px 8px; border: 0; border-radius: 0; background: transparent; box-shadow: none; font-size: 13.5px; }
  .cs.cs :global(.sc-input:focus) { border: 0; box-shadow: none; }
  .cs.cs :global(.sc-send) { width: 36px; height: 36px; border-radius: 11px; font-size: 16px; }
  .cs.cs :global(.sc-send.ready) { color: var(--accent-ink, #0c0d11); background: var(--orange); box-shadow: none; transform: none; }

  @media (prefers-reduced-motion: reduce) { .cs-in, .cs-empty { animation: none; } .cs-card { transition: none; } }
  :global([data-animations="off"]) .cs-in, :global([data-animations="off"]) .cs-empty { animation: none; }
</style>
