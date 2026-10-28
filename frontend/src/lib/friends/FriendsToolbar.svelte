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

<div class="tb" class:minimal>
  {#if lead}<div class="tb-lead">{@render lead()}</div>{/if}

  {#if !minimal}
  <div class="tb-conn" class:on={s.st.conn === 'online'} class:wait={s.st.conn === 'connecting'} role="status">
    <span class="tb-dot"></span>
    <span class="tb-ct">{s.connLabel}</span>
    {#if s.onlineCount > 0}<span class="tb-on">{s.onlineCount} {t('social.onlineCount')}</span>{/if}
  </div>

  <div class="tb-sp"></div>

  <div class="tb-tools">
    <button class="btn btn-ghost btn-sm tb-ic" onclick={() => s.load(true)} use:tip={t('friends.refresh')} aria-label={t('friends.refresh')}><i class="ti ti-refresh"></i></button>
    <button class="btn btn-ghost btn-sm tb-ic" class:on={s.muteAll} onclick={() => s.toggleMuteAll()} disabled={s.muteAllBusy} aria-pressed={s.muteAll}
            use:tip={s.muteAll ? t('social.muteAll.on') : t('social.muteAll.off')} aria-label={s.muteAll ? t('social.muteAll.on') : t('social.muteAll.off')}>
      <i class="ti {s.muteAll ? 'ti-bell-off' : 'ti-bell'}"></i>
    </button>
    <button class="btn btn-ghost btn-sm" onclick={onPrivacy} aria-label={t('social.priv.title')}><i class="ti ti-lock"></i><span class="tb-lbl">{t('social.priv.title')}</span></button>
    <button class="btn btn-primary btn-sm" onclick={onAdd} aria-label={t('friends.addFriend')}>
      <i class="ti ti-user-plus"></i><span class="tb-lbl">{t('friends.addFriend')}</span>
    </button>
  </div>
  {/if}
</div>

<style>
  /* Рядок дій розділу — як верх сторінок налаштувань і серверів: стан
     зліва (у стилі «Автозбереження»), звичайні кнопки лаунчера справа.
     Назву розділу показує topbar, тут вона не дублюється. */
  .tb { display: flex; align-items: center; gap: 10px; min-width: 0; flex: 0 0 auto; flex-wrap: wrap; row-gap: 8px; }
  .tb.minimal:not(:has(.tb-lead)) { display: none; }
  .tb-lead { flex: 0 0 auto; min-width: 0; }
  .tb-sp { flex: 1 1 0; min-width: 0; }
  .tb-conn { display: inline-flex; align-items: center; gap: 7px; min-width: 0; font-size: 12px; color: var(--text-mute); }
  .tb-dot { flex: 0 0 auto; width: 8px; height: 8px; border-radius: 50%; background: var(--text-dim); }
  .tb-conn.on .tb-dot { background: var(--ff-ok); box-shadow: 0 0 0 3px color-mix(in srgb, var(--ff-ok) 22%, transparent); }
  .tb-conn.wait .tb-dot { background: var(--ff-warn); animation: tb-p 1.2s ease-in-out infinite; }
  @keyframes tb-p { 50% { opacity: .3; } }
  .tb-ct { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .tb-on { flex: 0 0 auto; padding: 3px 8px; border-radius: 999px; background: color-mix(in srgb, var(--ff-ok) 14%, transparent); color: var(--ff-ok); font-size: 11px; font-weight: 700; white-space: nowrap; }
  .tb-tools { display: flex; align-items: center; gap: 6px; flex: 0 0 auto; }
  .tb-tools .btn { display: inline-flex; align-items: center; gap: 6px; }
  .tb-ic { width: 32px; padding-left: 0; padding-right: 0; justify-content: center; }
  .tb-ic.on { color: var(--orange); border-color: rgba(var(--orange-rgb), .45); background: rgba(var(--orange-rgb), .12); }

  /* Вузько: підписи кнопок згортаються до іконок. */
  @container sf (max-width: 640px) { .tb-lbl { display: none; } .tb-ct { display: none; } }
  @container sf (max-width: 460px) { .tb-on { display: none; } }
  @media (prefers-reduced-motion: reduce) { .tb-conn.wait .tb-dot { animation: none; } }
</style>
