<script lang="ts">
  // ── Верхня панель вкладки «Шаурма» ───────────────────────────────────────
  // Ліворуч: (необов'язкові) перемикач джерел + стан зʼєднання. Праворуч:
  // оновити, сповіщення, приватність, «Додати друга». Усі кнопки мають
  // фіксований розмір, а підписи ховаються контейнерним запитом — тож у вузькому
  // вікні нічого не накладається й не переноситься.
  import { useFriendsStore } from './FriendsStore.svelte'
  import { t } from '../i18n.svelte'
  import { tip } from './tooltip'

  let { lead, onAdd, onPrivacy, minimal = false }: { lead?: import('svelte').Snippet; onAdd: () => void; onPrivacy: () => void; minimal?: boolean } = $props()
  const s = useFriendsStore()
</script>

<div class="tb">
  {#if lead}<div class="tb-lead">{@render lead()}</div>{/if}

  {#if !minimal}
  <div class="tb-conn" class:on={s.st.conn === 'online'} class:wait={s.st.conn === 'connecting'} role="status">
    <span class="tb-dot"></span>
    <span class="tb-ct">{s.connLabel}</span>
    {#if s.onlineCount > 0}<span class="tb-on">{s.onlineCount} {t('social.onlineCount')}</span>{/if}
  </div>

  <div class="tb-sp"></div>

  <div class="tb-tools">
    <button class="ff-ibtn" onclick={() => s.load(true)} use:tip={t('friends.refresh')} aria-label={t('friends.refresh')}><i class="ti ti-refresh"></i></button>
    <button class="ff-ibtn" class:on={s.muteAll} onclick={() => s.toggleMuteAll()} disabled={s.muteAllBusy} aria-pressed={s.muteAll}
            use:tip={s.muteAll ? t('social.muteAll.on') : t('social.muteAll.off')} aria-label={s.muteAll ? t('social.muteAll.on') : t('social.muteAll.off')}>
      <i class="ti {s.muteAll ? 'ti-bell-off' : 'ti-bell'}"></i>
    </button>
    <button class="ff-ibtn" onclick={onPrivacy} use:tip={t('social.priv.title')} aria-label={t('social.priv.title')}><i class="ti ti-lock"></i></button>
    <button class="btn btn-primary btn-sm tb-add" onclick={onAdd} aria-label={t('friends.addFriend')}>
      <i class="ti ti-user-plus"></i><span>{t('friends.addFriend')}</span>
    </button>
  </div>
  {/if}
</div>

<style>
  .tb { display: flex; align-items: center; gap: 8px; min-width: 0; min-height: 52px; box-sizing: border-box; padding: 8px 14px 8px 24px; flex: 0 0 auto; border-bottom: 1px solid var(--border);
    background: var(--bg-window); }
  .tb-lead { flex: 0 0 auto; min-width: 0; }
  .tb-sp { flex: 1 1 0; min-width: 0; }
  .tb-conn { display: inline-flex; align-items: center; gap: 7px; min-width: 0; height: 26px; padding: 0 6px 0 10px; border-radius: 13px; background: var(--bg-input); box-shadow: inset 0 0 0 1px var(--border); font: 600 11px/1 var(--font-ui); color: var(--text-dim); }
  .tb-dot { flex: 0 0 auto; width: 8px; height: 8px; border-radius: 50%; background: var(--text-dim); }
  .tb-conn.on { color: var(--text-mute); }
  .tb-conn.on .tb-dot { background: var(--ff-ok); box-shadow: 0 0 0 3px color-mix(in srgb, var(--ff-ok) 24%, transparent); }
  .tb-conn.wait .tb-dot { background: var(--ff-warn); animation: tb-p 1.2s ease-in-out infinite; }
  @keyframes tb-p { 50% { opacity: .3; } }
  .tb-conn:not(:has(.tb-on)) { padding-right: 13px; }
  .tb-ct { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .tb-on { flex: 0 0 auto; padding: 4px 8px; border-radius: 10px; background: color-mix(in srgb, var(--ff-ok) 16%, transparent); color: var(--ff-ok); font: 750 10.5px/1 var(--font-ui); white-space: nowrap; }
  .tb-tools { display: flex; align-items: center; gap: 3px; flex: 0 0 auto; }
  .tb-tools :global(.ff-ibtn) { width: 28px; height: 28px; border-radius: 8px; background: var(--bg-input); box-shadow: inset 0 0 0 1px var(--border); }
  .tb-tools :global(.ff-ibtn:hover:not(:disabled)) { box-shadow: inset 0 0 0 1px var(--border-hi); }
  .tb-tools :global(.ff-ibtn.on) { background: rgba(var(--orange-rgb), .14); box-shadow: inset 0 0 0 1px rgba(var(--orange-rgb), .45); }
  .tb-add { display: inline-flex; align-items: center; gap: 5px; height: 28px; margin-left: 2px; padding: 0 10px; white-space: nowrap; border-radius: 8px; font-size: 11.5px; font-weight: 750;
    background: linear-gradient(135deg, var(--orange), var(--orange-2, var(--orange))); box-shadow: 0 5px 12px rgba(var(--orange-rgb), .24); }
  .tb-add .ti { font-size: 14px; }

  /* Вузько: підпис кнопки й текст стану згортаються до іконок / крапки. */
  @container sf (max-width: 640px) {
    .tb-add span { display: none; } .tb-add { width: 28px; padding: 0; justify-content: center; }
    .tb-ct { display: none; } .tb-conn { padding: 0 8px 0 10px; }
  }
  @container sf (max-width: 460px) { .tb { gap: 6px; padding: 8px; } .tb-on { display: none; } .tb-conn { padding: 0 10px; } }
  @media (prefers-reduced-motion: reduce) { .tb-conn.wait .tb-dot { animation: none; } }
</style>
