<script lang="ts">
  // ── Права панель: діалог або «домашня» сцена ─────────────────────────────
  // Чат перемонтується при зміні співрозмовника ({#key}) — потрібно, щоб історія
  // й скрол не змішувались. Поява — лише fade (CSS).
  //
  // Візуальний шар чату (шапка, бульбашки, поле вводу) перестилізований тут через
  // :global під .cs — сам ShaurmaChat (логіка, шифрування, стани) не чіпали.
  import { useFriendsStore } from './FriendsStore.svelte'
  import { t } from '../i18n.svelte'
  import ShaurmaChat from './ShaurmaChat.svelte'
  import type { SocialFriend } from '../types'

  let { peer, onBack, onOpenMod, onAdd, onPrivacy }: {
    peer: SocialFriend | null
    onBack: () => void
    onOpenMod?: (source: string, projectId: string) => void
    onAdd: () => void
    onPrivacy: () => void
  } = $props()
  const s = useFriendsStore()
</script>

<section class="cs" aria-live="polite">
  {#if peer}
    {#key peer.accountId}
      <div class="cs-in">
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

<style>
  /* Діалог — картка як .st-card у налаштуваннях: фон картки, рамка,
     заокруглення; шапка як .st-card-head, роздільники — var(--border). */
  .cs { position: relative; display: flex; flex-direction: column; min-width: 0; min-height: 0; overflow: hidden;
    background: var(--bg-card); border: 1px solid var(--border); border-radius: var(--r-lg, 16px); }
  .cs-in { flex: 1 1 0; min-height: 0; display: flex; flex-direction: column; animation: cs-in .22s ease both; }
  @keyframes cs-in { from { opacity: 0; transform: translateY(6px); } to { opacity: 1; transform: none; } }
  /* «Назад» потрібна лише коли панелі показуються по одній. */
  .cs :global(.sc-back) { display: none; }
  @container sf (max-width: 720px) { .cs :global(.sc-back) { display: flex; } }

  /* ── Домашня сцена: заголовок розділу + картки дій (як .st-card-head) ── */
  .cs-empty { flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 6px; padding: 28px 24px; text-align: center; overflow-y: auto; animation: cs-in .22s ease both; }
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

  /* ── Чат: шапка як .st-card-head ── */
  .cs.cs :global(.sc-head) { gap: 12px; padding: 13px 18px; border-bottom: 1px solid var(--border); background: var(--bg-card); }
  .cs.cs :global(.sc-av.lg) { width: 38px; height: 38px; border-radius: 11px; }
  .cs.cs :global(.sc-av.lg img) { border-radius: 11px; }
  .cs.cs :global(.sc-head .sc-st) { border-color: var(--bg-card); }
  .cs.cs :global(.sc-name) { font-size: 14.5px; font-weight: 700; }
  .cs.cs :global(.sc-sub) { font-size: 12px; color: var(--text-mute); }
  .cs.cs :global(.sc-hbtn) { width: 32px; height: 32px; border-radius: var(--r-sm, 8px); font-size: 15px; }
  /* «Запросити» — як .btn-primary лаунчера: суцільний помаранчевий, без власного градієнта. */
  .cs.cs :global(.sc-hbtn-txt) { width: auto; padding: 0 12px; font: 600 12px/1 var(--font-ui); color: #fff; border-color: transparent;
    background: var(--orange); box-shadow: 0 6px 16px rgba(var(--orange-rgb), .3); }
  .cs.cs :global(.sc-hbtn-txt:hover:not(:disabled)) { color: #fff; border-color: transparent; background: var(--orange-2, var(--orange)); }
  .cs.cs :global(.sc-back) { width: 32px; height: 32px; border-radius: var(--r-sm, 8px); }
  .cs.cs :global(.sc-pinbar) { background: color-mix(in srgb, var(--bg-card) 92%, var(--orange) 8%); }

  /* ── Чат: стрічка повідомлень ── */
  .cs.cs :global(.sc-msgs) { padding: 18px 20px 10px; background: var(--bg-card); }
  .cs.cs :global(.sc-day) { padding: 4px 13px; font-size: 11px; background: var(--bg-input); }
  .cs.cs :global(.sc-bub) { padding: 9px 13px 7px; border-radius: 16px; font-size: 13.5px; line-height: 1.45; background: var(--bg-input); border-color: var(--border); }
  .cs.cs :global(.sc-row:not(.own).gend .sc-bub) { border-bottom-left-radius: 5px; }
  .cs.cs :global(.sc-row.own .sc-bub) { background: rgba(var(--orange-rgb), .14); border-color: rgba(var(--orange-rgb), .4); }
  .cs.cs :global(.sc-row.own.gend .sc-bub) { border-bottom-right-radius: 5px; }

  /* ── Чат: поле вводу як поле налаштувань ── */
  .cs.cs :global(.sc-foot) { gap: 8px; padding: 12px 18px 14px; border-top: 1px solid var(--border); background: var(--bg-card); }
  .cs.cs :global(.sc-input) { padding: 10px 14px; border-radius: 12px; border: 1px solid var(--border-hi); background: var(--bg-input); font-size: 13.5px; }
  .cs.cs :global(.sc-input:focus) { border-color: rgba(var(--orange-rgb), .7); box-shadow: 0 0 0 3px rgba(var(--orange-rgb), .15); }
  .cs.cs :global(.sc-send) { width: 40px; height: 40px; border-radius: 12px; font-size: 17px; }
  .cs.cs :global(.sc-send.ready) { color: #fff; background: linear-gradient(135deg, var(--orange), var(--orange-2, var(--orange))); box-shadow: 0 8px 16px rgba(var(--orange-rgb), .25); }

  @media (prefers-reduced-motion: reduce) { .cs-in, .cs-empty { animation: none; } .cs-card { transition: none; } }
  :global([data-animations="off"]) .cs-in, :global([data-animations="off"]) .cs-empty { animation: none; }
</style>
