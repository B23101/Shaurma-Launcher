<script lang="ts">
  // ЄДИНЕ місце приватності акаунта. Використовується скрізь однаково:
  //  • «Мій профіль» → вкладка «Приватність»
  //  • Налаштування → «Приватність та сервіси»
  //  • Друзі → замочок
  // Компонент самодостатній (читає/пише через App), тож не залежить від того, де його показано.
  import { onMount } from 'svelte'
  import { App, on } from './wails'
  import { t } from './i18n.svelte'
  import { toast } from './toast.svelte'
  import SelectField from './SelectField.svelte'
  import './settings/settings.css'
  import type { SocialPrivacy, SocialBlockedAccount, SocialProfileVisibility } from './types'

  type Who = 'friends' | 'everyone' | 'nobody'
  const HIDDEN = /[\u0000-\u001f\u007f-\u009f\u200b-\u200f\u202a-\u202e\u2066-\u2069\ufeff]/g
  const clean = (s?: string) => (s ?? '').replace(HIDDEN, '').trim()

  let loggedIn = $state(false)
  let loaded = $state(false)
  let priv = $state<SocialPrivacy | null>(null)
  let profVis = $state<Who | ''>('')
  let whoAdd = $state<Who | ''>('')
  let mutualVis = $state<Who | ''>('')
  let muteAll = $state(false)
  let muteSound = $state(false)
  let blocked = $state<SocialBlockedAccount[]>([])
  let busy = $state('')

  function errText(e: any): string {
    const m = String(e?.message ?? e ?? '')
    const code = m.replace(/^social:/, '')
    const key = `social.err.${code}`
    const txt = t(key)
    return txt !== key ? txt : (m || t('social.err.unknown'))
  }

  async function load() {
    try {
      const st = await App.GetFriendsState()
      loggedIn = !!st?.loggedIn
      priv = st?.privacy ?? null
    } catch { loggedIn = false }
    loaded = true
    if (!loggedIn) return
    App.GetProfileVisibility().then((v) => { profVis = v as Who }).catch(() => {})
    App.GetMutualVisibility().then((v) => { mutualVis = v as Who }).catch(() => {})
    App.GetChatMuteState().then((m) => { muteAll = !!m?.all; muteSound = !!m?.sound }).catch(() => {})
    App.GetBlockedAccounts().then((b) => { blocked = b ?? [] }).catch(() => {})
    App.GroupRequest('list').catch(() => {})
  }

  onMount(() => {
    void load()
    // «Хто може додавати в групи» приходить у знімку груп.
    const off = on('group:event', (e: { type?: string; data?: any }) => {
      const v = e?.data?.whoCanAdd
      if (e?.type === 'groups_snapshot' && (v === 'friends' || v === 'everyone' || v === 'nobody')) whoAdd = v
    })
    return off
  })

  // «Усі» — сервер показує статус/гру і не-друзям у профілі (лише коли власник так вирішив).
  const showOpts = $derived([
    { value: 'everyone', label: t('social.priv.everyone') },
    { value: 'friends', label: t('social.priv.friends') },
    { value: 'nobody', label: t('social.priv.nobody') },
  ])
  const whoOpts = $derived([
    { value: 'friends', label: t('social.priv.friends') },
    { value: 'everyone', label: t('social.priv.everyone') },
    { value: 'nobody', label: t('social.priv.nobody') },
  ])
  const reqOpts = $derived([
    { value: 'everyone', label: t('social.priv.reqEveryone') },
    { value: 'friends_of_friends', label: t('social.priv.reqFof') },
    { value: 'nobody', label: t('social.priv.reqNobody') },
  ])

  async function savePriv(patch: Partial<SocialPrivacy>) {
    if (!priv || busy) return
    const prev = priv
    const next = { ...priv, ...patch } as SocialPrivacy
    priv = next; busy = 'priv'
    try { await App.SetFriendsPrivacy(next); toast(t('acc.modal.priv.saved'), 'success') }
    catch (e) { priv = prev; toast(errText(e), 'error') }
    finally { busy = '' }
  }
  async function saveProfile(v: string) {
    if (busy) return
    const prev = profVis
    profVis = v as Who; busy = 'prof'
    try { await App.SetProfileVisibility(v as SocialProfileVisibility); toast(t('acc.modal.priv.saved'), 'success') }
    catch (e) { profVis = prev; toast(errText(e), 'error') }
    finally { busy = '' }
  }
  async function saveMutual(v: string) {
    if (busy) return
    const prev = mutualVis
    mutualVis = v as Who; busy = 'mutual'
    try { await App.SetMutualVisibility(v as SocialProfileVisibility); toast(t('acc.modal.priv.saved'), 'success') }
    catch (e) { mutualVis = prev; toast(errText(e), 'error') }
    finally { busy = '' }
  }
  async function saveWhoAdd(v: string) {
    if (busy) return
    const prev = whoAdd
    whoAdd = v as Who; busy = 'add'
    try { await App.SetWhoCanAddToGroup(v as Who); toast(t('acc.modal.priv.saved'), 'success') }
    catch (e) { whoAdd = prev; toast(errText(e), 'error') }
    finally { busy = '' }
  }
  async function toggleMuteAll() {
    if (busy) return
    busy = 'mute'
    try { muteAll = !!(await App.SetChatMuteAll(!muteAll))?.all } catch (e) { toast(errText(e), 'error') }
    finally { busy = '' }
  }
  async function toggleSound() {
    if (busy) return
    busy = 'sound'
    try { muteSound = !!(await App.SetChatMuteSound(!muteSound))?.sound } catch (e) { toast(errText(e), 'error') }
    finally { busy = '' }
  }
  async function unblock(b: SocialBlockedAccount) {
    try {
      await App.UnblockAccount(b.accountId)
      blocked = blocked.filter((x) => x.accountId !== b.accountId)
      toast(t('social.block.undone', { name: clean(b.nickname) }), 'success')
    } catch (e) { toast(errText(e), 'error') }
  }

  const off = $derived(!loggedIn || !priv)
  const norm = (v?: string) => v ?? 'friends'
</script>

<div class="ap" data-testid="account-privacy">
  {#if loaded && !loggedIn}
    <div class="ap-warn"><i class="ti ti-plug-off"></i> {t('acc.modal.priv.need')}</div>
  {:else}
    <p class="ap-note"><i class="ti ti-shield-check"></i> {t('acc.modal.priv.note')}</p>
  {/if}

  <section class="st-card ap-card" class:ap-off={off}>
    <div class="st-card-head"><div class="st-card-ic"><i class="ti ti-eye"></i></div><div class="st-card-tx"><h3>{t('acc.set.vis')}</h3></div></div>
    <div class="st-card-body">
      <div class="st-row"><div class="st-row-text"><div class="st-row-name">{t('social.priv.status')}</div><div class="st-row-hint">{t('social.priv.statusHint')}</div></div>
        <SelectField value={norm(priv?.showStatusTo)} options={showOpts} ariaLabel={t('social.priv.status')} minWidth="124px" disabled={off} onchange={(v: string) => savePriv({ showStatusTo: v as any })} /></div>
      <div class="st-row"><div class="st-row-text"><div class="st-row-name">{t('social.priv.server')}</div><div class="st-row-hint">{t('social.priv.serverHint')}</div></div>
        <SelectField value={norm(priv?.showServerTo)} options={showOpts} ariaLabel={t('social.priv.server')} minWidth="124px" disabled={off} onchange={(v: string) => savePriv({ showServerTo: v as any })} /></div>
      <div class="st-row"><div class="st-row-text"><div class="st-row-name">{t('acc.modal.priv.profile')}</div><div class="st-row-hint">{t('acc.modal.priv.profileHint')}</div></div>
        <SelectField value={profVis || 'friends'} options={whoOpts} ariaLabel={t('acc.modal.priv.profile')} minWidth="124px" disabled={off || profVis === ''} onchange={saveProfile} /></div>
      <div class="st-row"><div class="st-row-text"><div class="st-row-name">{t('acc.set.mutual')}</div><div class="st-row-hint">{t('acc.set.mutualHint')}</div></div>
        <SelectField value={mutualVis || 'friends'} options={whoOpts} ariaLabel={t('acc.set.mutual')} minWidth="124px" disabled={off || mutualVis === ''} onchange={saveMutual} /></div>
    </div>
  </section>

  <section class="st-card ap-card" class:ap-off={off}>
    <div class="st-card-head"><div class="st-card-ic"><i class="ti ti-message-circle"></i></div><div class="st-card-tx"><h3>{t('acc.set.contact')}</h3></div></div>
    <div class="st-card-body">
      <div class="st-row"><div class="st-row-text"><div class="st-row-name">{t('social.priv.whoMsg')}</div><div class="st-row-hint">{t('social.priv.whoMsgHint')}</div></div>
        <SelectField value={priv?.whoCanMessage ?? 'friends'} options={whoOpts} ariaLabel={t('social.priv.whoMsg')} minWidth="124px" disabled={off} onchange={(v: string) => savePriv({ whoCanMessage: v as any })} /></div>
      <div class="st-row"><div class="st-row-text"><div class="st-row-name">{t('groups.priv.whoAdd')}</div><div class="st-row-hint">{t('groups.priv.whoAddHint')}</div></div>
        <SelectField value={whoAdd || 'friends'} options={whoOpts} ariaLabel={t('groups.priv.whoAdd')} minWidth="124px" disabled={off || whoAdd === ''} onchange={saveWhoAdd} /></div>
      <div class="st-row"><div class="st-row-text"><div class="st-row-name">{t('social.priv.requests')}</div><div class="st-row-hint">{t('social.priv.requestsHint')}</div></div>
        <SelectField value={priv?.friendRequests ?? 'everyone'} options={reqOpts} ariaLabel={t('social.priv.requests')} minWidth="124px" disabled={off} onchange={(v: string) => savePriv({ friendRequests: v as any })} /></div>
    </div>
  </section>

  <section class="st-card ap-card">
    <div class="st-card-head"><div class="st-card-ic violet"><i class="ti ti-bell"></i></div><div class="st-card-tx"><h3>{t('ff.priv.notifs')}</h3></div></div>
    <div class="st-card-body">
      <div class="st-row"><div class="st-row-text"><div class="st-row-name">{t('ff.priv.muteAll')}</div><div class="st-row-hint">{t('ff.priv.muteAllHint')}</div></div>
        <button class="st-switch" class:on={!muteAll} role="switch" aria-checked={!muteAll} aria-label={t('ff.priv.muteAll')} disabled={busy === 'mute'} onclick={toggleMuteAll}></button></div>
      <div class="st-row"><div class="st-row-text"><div class="st-row-name">{t('ff.priv.sound')}</div><div class="st-row-hint">{t('ff.priv.soundHint')}</div></div>
        <button class="st-switch" class:on={!muteSound} role="switch" aria-checked={!muteSound} aria-label={t('ff.priv.sound')} disabled={busy === 'sound'} onclick={toggleSound}></button></div>
    </div>
  </section>

  <section class="st-card ap-card">
    <div class="st-card-head"><div class="st-card-ic red"><i class="ti ti-ban"></i></div><div class="st-card-tx"><h3>{t('social.block.list')}</h3></div></div>
    <div class="st-card-body">
      {#if blocked.length === 0}
        <div class="st-row st-row-hint ap-empty">{t('social.block.empty')}</div>
      {:else}
        {#each blocked as b (b.accountId)}
          <div class="st-row">
            <div class="st-row-text"><div class="st-row-name">{clean(b.nickname)}</div></div>
            <button class="btn btn-ghost btn-sm" onclick={() => unblock(b)}>{t('social.block.unblock')}</button>
          </div>
        {/each}
      {/if}
    </div>
  </section>
</div>

<style>
  .ap { display: flex; flex-direction: column; gap: 12px; }
  .ap-card .st-row { padding: 12px 18px; min-height: 54px; }
  .ap-off { opacity: .6; pointer-events: none; }
  .ap-note { display: flex; gap: 8px; margin: 0; font-size: 12px; line-height: 1.5; color: var(--text-mute); }
  .ap-warn { display: flex; gap: 10px; align-items: center; padding: 10px 12px; border-radius: var(--r-md); font-size: 12px; border: 1px solid rgba(245,158,11,.4); background: rgba(245,158,11,.09); }
  .ap-empty { color: var(--text-dim); min-height: 0; }
  @media (max-width: 520px) { .ap-card .st-row { flex-wrap: wrap; } }
</style>
