<script lang="ts">
  // ── Приватність і сповіщення ─────────────────────────────────────────────
  // Модалка (раніше — панель, що розсувала список друзів). Три блоки:
  // «Хто що бачить», «Сповіщення», «Заблоковані».
  import { useFriendsStore, clean } from './FriendsStore.svelte'
  import { t } from '../i18n.svelte'
  import { useGroupsStore } from '../groups/GroupsStore.svelte'
  import UiModal from '../UiModal.svelte'
  import SelectField from '../SelectField.svelte'
  import '../settings/settings.css'
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
  const gs = useGroupsStore()
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

  <section class="st-card pv-card">
    <div class="st-card-head">
      <div class="st-card-ic"><i class="ti ti-eye" aria-hidden="true"></i></div>
      <div class="st-card-tx"><h3>{t('ff.priv.who')}</h3></div>
    </div>
    <div class="st-card-body">
      <div class="st-row">
        <div class="st-row-text"><div class="st-row-name">{t('social.priv.status')}</div><div class="st-row-hint">{t('social.priv.statusHint')}</div></div>
        <SelectField value={s.st.privacy.showStatusTo === 'everyone' ? 'friends' : s.st.privacy.showStatusTo} options={showOptions} ariaLabel={t('social.priv.status')} minWidth="124px"
                     onchange={(v: string) => s.savePrivacy({ showStatusTo: v as SocialShowTo })} />
      </div>
      <div class="st-row">
        <div class="st-row-text"><div class="st-row-name">{t('social.priv.server')}</div><div class="st-row-hint">{t('social.priv.serverHint')}</div></div>
        <SelectField value={s.st.privacy.showServerTo === 'everyone' ? 'friends' : s.st.privacy.showServerTo} options={showOptions} ariaLabel={t('social.priv.server')} minWidth="124px"
                     onchange={(v: string) => s.savePrivacy({ showServerTo: v as SocialShowTo })} />
      </div>
      <div class="st-row">
        <div class="st-row-text"><div class="st-row-name">{t('social.priv.whoMsg')}</div><div class="st-row-hint">{t('social.priv.whoMsgHint')}</div></div>
        <SelectField value={s.st.privacy.whoCanMessage ?? 'friends'} options={whoOptions} ariaLabel={t('social.priv.whoMsg')} minWidth="124px"
                     onchange={(v: string) => s.savePrivacy({ whoCanMessage: v as SocialWhoCanMessage })} />
      </div>
      <div class="st-row">
        <div class="st-row-text"><div class="st-row-name">{t('groups.priv.whoAdd')}</div><div class="st-row-hint">{t('groups.priv.whoAddHint')}</div></div>
        <SelectField value={gs.whoCanAdd} options={whoOptions} ariaLabel={t('groups.priv.whoAdd')} minWidth="124px"
                     onchange={(v: string) => gs.setWhoCanAdd(v as 'friends' | 'everyone' | 'nobody')} />
      </div>
      <div class="st-row">
        <div class="st-row-text"><div class="st-row-name">{t('social.priv.requests')}</div><div class="st-row-hint">{t('social.priv.requestsHint')}</div></div>
        <SelectField value={s.st.privacy.friendRequests} options={requestOptions} ariaLabel={t('social.priv.requests')} minWidth="124px"
                     onchange={(v: string) => s.savePrivacy({ friendRequests: v as SocialFriendRequests })} />
      </div>
    </div>
  </section>

  <section class="st-card pv-card">
    <div class="st-card-head">
      <div class="st-card-ic violet"><i class="ti ti-bell" aria-hidden="true"></i></div>
      <div class="st-card-tx"><h3>{t('ff.priv.notifs')}</h3></div>
    </div>
    <div class="st-card-body">
      <div class="st-row">
        <div class="st-row-text"><div class="st-row-name">{t('ff.priv.muteAll')}</div><div class="st-row-hint">{t('ff.priv.muteAllHint')}</div></div>
        <button class="st-switch" class:on={!s.muteAll} role="switch" aria-checked={!s.muteAll} aria-label={t('ff.priv.muteAll')} disabled={s.muteAllBusy} onclick={() => s.toggleMuteAll()}></button>
      </div>
      <div class="st-row">
        <div class="st-row-text"><div class="st-row-name">{t('ff.priv.sound')}</div><div class="st-row-hint">{t('ff.priv.soundHint')}</div></div>
        <button class="st-switch" class:on={!s.muteSound} role="switch" aria-checked={!s.muteSound} aria-label={t('ff.priv.sound')} onclick={() => s.toggleMuteSound()}></button>
      </div>
    </div>
  </section>

  <section class="st-card pv-card">
    <div class="st-card-head">
      <div class="st-card-ic red"><i class="ti ti-ban" aria-hidden="true"></i></div>
      <div class="st-card-tx"><h3>{t('social.block.list')}</h3></div>
    </div>
    <div class="st-card-body">
      {#if s.blocked.length === 0}
        <div class="st-row st-row-hint pv-empty">{t('social.block.empty')}</div>
      {:else}
        {#each s.blocked as b (b.accountId)}
          <div class="st-row">
            <div class="st-row-text"><div class="st-row-name">{clean(b.nickname)}</div></div>
            <button class="btn btn-ghost btn-sm" onclick={() => s.unblock(b)}>{t('social.block.unblock')}</button>
          </div>
        {/each}
      {/if}
    </div>
  </section>

  {#snippet footer()}
    <button class="btn btn-primary btn-sm" onclick={onclose}>{t('ff.done')}</button>
  {/snippet}
</UiModal>

<style>
  /* Рядки, картки й перемикачі — спільні з налаштуваннями лаунчера
     (settings.css: .st-card/.st-row/.st-switch). Тут лише відступи модалки. */
  .pv-warn { display: flex; gap: 10px; align-items: flex-start; margin-bottom: 14px; padding: 10px 12px; border-radius: var(--r-md); font-size: 12px; line-height: 1.5; color: var(--text);
    border: 1px solid rgba(245, 158, 11, .4); background: rgba(245, 158, 11, .09); }
  .pv-warn i { color: var(--yellow); font-size: 16px; flex: 0 0 auto; }
  .pv-card { margin-bottom: 12px; }
  .pv-card:last-child { margin-bottom: 0; }
  .pv-card .st-row { padding: 12px 18px; min-height: 54px; }
  .pv-empty { color: var(--text-dim); min-height: 0; }
  @media (max-width: 520px) { .pv-card .st-row { flex-wrap: wrap; } }
</style>
