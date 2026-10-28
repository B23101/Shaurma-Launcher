<script lang="ts">
  // ── Приватність і сповіщення ─────────────────────────────────────────────
  // Модалка (раніше — панель, що розсувала список друзів). Три блоки:
  // «Хто що бачить», «Сповіщення», «Заблоковані».
  import { useFriendsStore, clean } from './FriendsStore.svelte'
  import { t } from '../i18n.svelte'
  import UiModal from '../UiModal.svelte'
  import SelectField from '../SelectField.svelte'
  import type { SocialShowTo, SocialFriendRequests, SocialWhoCanMessage } from '../types'

  let { open = false, onclose }: { open?: boolean; onclose: () => void } = $props()
  const s = useFriendsStore()

  $effect(() => { if (open && !s.blockedLoaded) void s.loadBlocked() })

  // Сервер показує присутність лише взаємним друзям, тож «Усі» й «Друзі» діють
  // однаково. Тому «Усі» не пропонуємо, а збережене 'everyone' показуємо як 'friends'.
  const showOptions = $derived([
    { value: 'friends', label: t('social.priv.friends') },
    { value: 'nobody', label: t('social.priv.nobody') },
  ])
  const whoOptions = $derived([
    { value: 'friends', label: t('social.priv.friends') },
    { value: 'everyone', label: t('social.priv.everyone') },
    { value: 'nobody', label: t('social.priv.nobody') },
  ])
  const requestOptions = $derived([
    { value: 'everyone', label: t('social.priv.reqEveryone') },
    { value: 'friends_of_friends', label: t('social.priv.reqFof') },
    { value: 'nobody', label: t('social.priv.reqNobody') },
  ])
</script>

<UiModal {open} {onclose} icon="ti-lock" title={t('social.priv.title')} subtitle={t('ff.priv.sub')} width="540px" zIndex={1000001}>
  {#if !s.st.privacyLoaded}
    <div class="pv-warn"><i class="ti ti-shield-exclamation"></i>{t('social.priv.notLoaded')}</div>
  {/if}

  <h4 class="pv-h"><i class="ti ti-eye"></i>{t('ff.priv.who')}</h4>
  <div class="pv-card">
    <div class="pv-row">
      <div class="pv-tx"><div class="pv-t">{t('social.priv.status')}</div><div class="pv-d">{t('social.priv.statusHint')}</div></div>
      <SelectField value={s.st.privacy.showStatusTo === 'everyone' ? 'friends' : s.st.privacy.showStatusTo} options={showOptions} ariaLabel={t('social.priv.status')} minWidth="124px"
                   onchange={(v: string) => s.savePrivacy({ showStatusTo: v as SocialShowTo })} />
    </div>
    <div class="pv-row">
      <div class="pv-tx"><div class="pv-t">{t('social.priv.server')}</div><div class="pv-d">{t('social.priv.serverHint')}</div></div>
      <SelectField value={s.st.privacy.showServerTo === 'everyone' ? 'friends' : s.st.privacy.showServerTo} options={showOptions} ariaLabel={t('social.priv.server')} minWidth="124px"
                   onchange={(v: string) => s.savePrivacy({ showServerTo: v as SocialShowTo })} />
    </div>
    <div class="pv-row">
      <div class="pv-tx"><div class="pv-t">{t('social.priv.whoMsg')}</div><div class="pv-d">{t('social.priv.whoMsgHint')}</div></div>
      <SelectField value={s.st.privacy.whoCanMessage ?? 'friends'} options={whoOptions} ariaLabel={t('social.priv.whoMsg')} minWidth="124px"
                   onchange={(v: string) => s.savePrivacy({ whoCanMessage: v as SocialWhoCanMessage })} />
    </div>
    <div class="pv-row">
      <div class="pv-tx"><div class="pv-t">{t('social.priv.requests')}</div><div class="pv-d">{t('social.priv.requestsHint')}</div></div>
      <SelectField value={s.st.privacy.friendRequests} options={requestOptions} ariaLabel={t('social.priv.requests')} minWidth="124px"
                   onchange={(v: string) => s.savePrivacy({ friendRequests: v as SocialFriendRequests })} />
    </div>
  </div>

  <h4 class="pv-h"><i class="ti ti-bell"></i>{t('ff.priv.notifs')}</h4>
  <div class="pv-card">
    <div class="pv-row">
      <div class="pv-tx"><div class="pv-t">{t('ff.priv.muteAll')}</div><div class="pv-d">{t('ff.priv.muteAllHint')}</div></div>
      <button class="pv-sw" class:on={!s.muteAll} role="switch" aria-checked={!s.muteAll} aria-label={t('ff.priv.muteAll')} disabled={s.muteAllBusy} onclick={() => s.toggleMuteAll()}><i></i></button>
    </div>
    <div class="pv-row">
      <div class="pv-tx"><div class="pv-t">{t('ff.priv.sound')}</div><div class="pv-d">{t('ff.priv.soundHint')}</div></div>
      <button class="pv-sw" class:on={!s.muteSound} role="switch" aria-checked={!s.muteSound} aria-label={t('ff.priv.sound')} onclick={() => s.toggleMuteSound()}><i></i></button>
    </div>
  </div>

  <h4 class="pv-h"><i class="ti ti-ban"></i>{t('social.block.list')}</h4>
  <div class="pv-card">
    {#if s.blocked.length === 0}
      <div class="pv-empty">{t('social.block.empty')}</div>
    {:else}
      {#each s.blocked as b (b.accountId)}
        <div class="pv-row">
          <div class="pv-tx"><div class="pv-t pv-nick">{clean(b.nickname)}</div></div>
          <button class="pv-btn" onclick={() => s.unblock(b)}>{t('social.block.unblock')}</button>
        </div>
      {/each}
    {/if}
  </div>

  {#snippet footer()}
    <button class="btn btn-primary btn-sm" onclick={onclose}>{t('ff.done')}</button>
  {/snippet}
</UiModal>

<style>
  .pv-warn { display: flex; gap: 10px; align-items: flex-start; margin-bottom: 14px; padding: 10px 12px; border-radius: 11px; font: 400 12px/1.5 var(--font-ui); color: var(--text);
    border: 1px solid color-mix(in srgb, var(--ff-warn) 40%, transparent); background: color-mix(in srgb, var(--ff-warn) 9%, transparent); }
  .pv-warn i { color: var(--ff-warn); font-size: 16px; flex: 0 0 auto; }
  .pv-h { display: flex; align-items: center; gap: 8px; margin: 18px 0 8px; font: 750 11.5px/1 var(--font-ui); letter-spacing: .3px; color: var(--text-mute); }
  .pv-h:first-child { margin-top: 0; }
  .pv-h i { color: var(--orange); font-size: 14px; }
  .pv-card { border-radius: 13px; background: var(--bg-input); border: 1px solid var(--border); }
  .pv-row { display: flex; align-items: center; justify-content: space-between; gap: 14px; padding: 12px 14px; }
  .pv-row + .pv-row { border-top: 1px solid var(--border); }
  .pv-tx { flex: 1; min-width: 0; }
  .pv-t { font: 650 12.5px/1.3 var(--font-ui); color: var(--text); }
  .pv-d { margin-top: 3px; font: 400 11px/1.45 var(--font-ui); color: var(--text-dim); }
  .pv-nick { display: flex; align-items: baseline; gap: 6px; }
  .pv-btn { flex: 0 0 auto; height: 32px; padding: 0 14px; border-radius: 10px; cursor: pointer; font: 700 12px/1 var(--font-ui); color: var(--text); background: var(--bg-panel); border: 1px solid var(--border-hi); transition: border-color .14s, color .14s, transform .12s; }
  .pv-btn:hover { border-color: var(--orange); color: var(--orange); }
  .pv-btn:active { transform: scale(.95); }
  .pv-empty { padding: 14px; font: 400 12px/1.4 var(--font-ui); color: var(--text-dim); }

  /* Перемикач: рухається лише transform. */
  .pv-sw { position: relative; flex: 0 0 auto; width: 40px; height: 23px; padding: 0; border-radius: 12px; border: 1px solid var(--border-hi); background: var(--bg-input-hi, var(--hover-strong)); cursor: pointer; transition: background-color .18s, border-color .18s; }
  .pv-sw i { position: absolute; top: 2px; left: 2px; width: 17px; height: 17px; border-radius: 50%; background: var(--text-mute); transition: transform .2s var(--ff-spring), background-color .18s; }
  .pv-sw.on { background: color-mix(in srgb, var(--orange) 85%, transparent); border-color: var(--orange); }
  .pv-sw.on i { transform: translateX(17px); background: #fff; }
  .pv-sw:disabled { opacity: .5; cursor: default; }
  .pv-sw:focus-visible { outline: 2px solid color-mix(in srgb, var(--orange) 70%, transparent); outline-offset: 2px; }
  @media (max-width: 520px) { .pv-row { flex-wrap: wrap; } }
  @media (prefers-reduced-motion: reduce) { .pv-sw, .pv-sw i { transition: none; } }
</style>
