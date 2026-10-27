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
  .cs { position: relative; display: flex; flex-direction: column; min-width: 0; min-height: 0; background: var(--bg-app); }
  .cs-in { flex: 1 1 0; min-height: 0; display: flex; flex-direction: column; animation: cs-in .2s ease-out both; }
  @keyframes cs-in { from { opacity: 0; } to { opacity: 1; } }
  /* «Назад» потрібна лише коли панелі показуються по одній. */
  .cs :global(.sc-back) { display: none; }
  @container sf (max-width: 720px) { .cs :global(.sc-back) { display: flex; } }

  /* ── Домашня сцена ── */
  .cs-empty { flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 6px; padding: 28px 24px; text-align: center; overflow-y: auto; animation: cs-in .3s ease-out both; }
  .cs-orb { margin-bottom: 12px; flex: 0 0 auto; }
  .cs-main { display: grid; place-items: center; width: 56px; height: 56px; border-radius: 17px; font-size: 28px; color: #fff;
    background: linear-gradient(135deg, var(--orange), var(--orange-2, var(--orange))); box-shadow: 0 8px 18px rgba(var(--orange-rgb), .25); }
  .cs-t { margin: 0; font: 800 17px/1.25 var(--font-ui); color: var(--text); letter-spacing: -.2px; }
  .cs-h { margin: 0; max-width: 380px; font: 400 12.5px/1.65 var(--font-ui); color: var(--text-dim); }
  .cs-cards { display: grid; grid-template-columns: repeat(auto-fit, minmax(190px, 1fr)); gap: 10px; width: 100%; max-width: 640px; margin-top: 22px; }
  .cs-card { display: flex; align-items: center; gap: 12px; padding: 12px 14px; border-radius: var(--r-md, 12px); text-align: left; cursor: pointer; color: var(--text);
    background: var(--bg-card, var(--bg-input)); border: 1px solid var(--border); transition: border-color .16s, background-color .16s; }
  .cs-card:hover { border-color: var(--border-hi); background: var(--hover); }
  .cs-card:active { transform: scale(.985); }
  .cs-card:focus-visible { outline: 2px solid var(--orange); outline-offset: 2px; }
  .cs-ci { flex: 0 0 auto; width: 38px; height: 38px; display: grid; place-items: center; border-radius: 12px; font-size: 19px; color: var(--orange); background: rgba(var(--orange-rgb), .13); }
  .cs-ct { min-width: 0; display: flex; flex-direction: column; gap: 3px; }
  .cs-ct b { font: 700 12.5px/1.3 var(--font-ui); overflow-wrap: anywhere; }
  .cs-ct small { font: 400 11px/1.35 var(--font-ui); color: var(--text-dim); }

  /* ── Чат: шапка ── */
  .cs.cs :global(.sc-head) { gap: 10px; padding: 9px 14px; background: var(--bg-window); }
  .cs.cs :global(.sc-av.lg) { width: 36px; height: 36px; border-radius: 12px; box-shadow: inset 0 0 0 1px var(--border-hi); }
  .cs.cs :global(.sc-av.lg img) { border-radius: 12px; }
  .cs.cs :global(.sc-name) { font-size: 14px; font-weight: 800; letter-spacing: -.1px; }
  .cs.cs :global(.sc-sub) { font-size: 11.5px; }
  .cs.cs :global(.sc-hbtn) { width: 30px; height: 30px; border-radius: 10px; font-size: 15px; }
  .cs.cs :global(.sc-hbtn-txt) { width: auto; padding: 0 11px; font-size: 12px; }
  .cs.cs :global(.sc-back) { width: 30px; height: 30px; border-radius: 10px; }

  /* ── Чат: повідомлення ── */
  .cs.cs :global(.sc-msgs) { padding: 18px 22px 10px; }
  .cs.cs :global(.sc-day) { padding: 4px 14px; font-size: 11px; background: var(--bg-input); }
  .cs.cs :global(.sc-bub) { padding: 9px 13px 7px; border-radius: 19px; border-color: transparent; font-size: 13.5px; line-height: 1.45; background: var(--bg-input); box-shadow: inset 0 0 0 1px var(--border); }
  .cs.cs :global(.sc-row:not(.own).gend .sc-bub) { border-bottom-left-radius: 6px; }
  .cs.cs :global(.sc-row.own .sc-bub) { background: linear-gradient(135deg, rgba(var(--orange-rgb), .34), rgba(var(--orange-rgb), .16)); box-shadow: inset 0 0 0 1px rgba(var(--orange-rgb), .45); }
  .cs.cs :global(.sc-row.own.gend .sc-bub) { border-bottom-right-radius: 6px; }

  /* ── Чат: поле вводу ── */
  .cs.cs :global(.sc-foot) { gap: 8px; padding: 8px 14px 12px; border-top: 0; background: linear-gradient(180deg, transparent, color-mix(in srgb, var(--bg-input) 40%, transparent)); }
  .cs.cs :global(.sc-input) { padding: 9px 14px; border-radius: 14px; border-color: transparent; background: var(--bg-input); box-shadow: inset 0 0 0 1px var(--border-hi); font-size: 13.5px; }
  .cs.cs :global(.sc-input:focus) { box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--orange) 65%, transparent), 0 0 0 4px rgba(var(--orange-rgb), .1); }
  .cs.cs :global(.sc-send) { width: 36px; height: 36px; border-radius: 12px; font-size: 16px; }
  .cs.cs :global(.sc-send.ready) { color: #fff; background: linear-gradient(135deg, var(--orange), var(--orange-2, var(--orange))); box-shadow: 0 8px 18px rgba(var(--orange-rgb), .35); }

  @media (prefers-reduced-motion: reduce) { .cs-in, .cs-empty { animation: none; } .cs-card { transition: none; } }
</style>
