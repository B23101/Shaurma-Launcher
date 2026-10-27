<script lang="ts">
  // ProfileModal — ЄДИНЕ вікно профілю гравця. Той самий компонент і для «Мій акаунт»
  // (mode="self": редагування біо/кольору, приватність, вигляд), і для профілю друга
  // (mode="friend": рівно такий самий вигляд, але лише перегляд + дії над другом).
  //
  // ПРИВАТНІСТЬ — НА СЕРВЕРІ. У режимі friend клієнт НІЧОГО не фільтрує: він просить
  // профіль у сервера (GetFriendProfile). Якщо друг закрив профіль, сервер повертає hidden=true
  // без біо/кольору/статистики — тут показується лише заглушка. Статус і збірка в грі приходять
  // з хаба вже відфільтровані сервером за show_status_to / show_server_to.
  import { untrack, onDestroy } from 'svelte'
  import { fly, scale, fade } from 'svelte/transition'
  import { cubicOut } from 'svelte/easing'
  import { Tween } from 'svelte/motion'
  import UiModal from './UiModal.svelte'
  import { t } from './i18n.svelte'
  import { App } from './wails'
  import { toast } from './toast.svelte'
  import { createWardrobeViewer, type WardrobeViewerHandle } from './wardrobe3d'
  import { PROFILE_COLORS, normalizeBio, bioLength, safeColor, hueOf, shortId } from './friends/profileStyle'
  import { lastSeenText } from './friends/lastSeen'
  import { tip } from './friends/tooltip'
  import { buildFriendLink } from './friends/friendLink'
  import { accountPrefs, setAccountPref, resetAccountPrefs, type AccountToggleKey, type WidgetLine } from './accountPrefs.svelte'
  import type {
    Account, ShaurmaProfile, SocialPrivacy, SocialState, SocialFriend, SocialFriendProfile, SocialProfileVisibility,
  } from './types'

  let {
    mode = 'self',
    // ── mode="self" ──
    account,
    social = null,
    statusKind = 'offline',
    statusText = '',
    // ── mode="friend" ──
    friend = null,
    busy = false,
    muted = false,
    blocked = false,
    onmute = () => {},
    onblock = () => {},
    onmessage = () => {},
    onremove = () => {},
    // ── спільне ──
    head = '',
    onclose = () => {},
  }: {
    mode?: 'self' | 'friend'
    account?: Account
    social?: SocialState | null
    statusKind?: string
    statusText?: string
    friend?: SocialFriend | null
    busy?: boolean
    muted?: boolean
    blocked?: boolean
    onmute?: (f: SocialFriend) => void
    onblock?: (f: SocialFriend) => void
    onmessage?: (f: SocialFriend) => void
    onremove?: (f: SocialFriend) => void
    head?: string
    onclose?: () => void
  } = $props()

  const isSelf = $derived(mode === 'self')

  const HIDDEN = /[\u0000-\u001f\u007f-\u009f\u200b-\u200f\u202a-\u202e\u2066-\u2069\ufeff]/g
  const clean = (s?: string) => (s ?? '').replace(HIDDEN, '').trim()

  // Лишаємо останнього друга: під час закриття/видалення `friend` уже null, а вікно
  // має домалюватись без стрибків.
  let shown = $state<SocialFriend | null>(null)
  $effect(() => { if (friend) shown = friend })
  const f = $derived(shown)
  const open = $derived(isSelf ? true : !!friend && !!f)
  const friendId = $derived(isSelf ? '' : (f?.accountId ?? ''))
  const friendNick = $derived(isSelf ? '' : clean(f?.nickname))

  type Tab = 'profile' | 'privacy' | 'look'
  const TABS: { id: Tab; icon: string; key: string }[] = [
    { id: 'profile', icon: 'ti-user-circle', key: 'acc.modal.tab.profile' },
    { id: 'privacy', icon: 'ti-shield-lock', key: 'acc.modal.tab.privacy' },
    { id: 'look', icon: 'ti-adjustments-horizontal', key: 'acc.modal.tab.look' },
  ]
  let tab = $state<Tab>('profile')
  const tabIdx = $derived(TABS.findIndex((x) => x.id === tab))

  const dur = (ms: number) => ms

  // ── Дані: свій профіль (локально + сервер) ────────────────────────────
  let profile = $state<ShaurmaProfile | null>(null)
  let loading = $state(true)
  let bioDraft = $state('')
  let colorDraft = $state('')
  let saving = $state(false)
  let copied = $state('')
  let copiedTimer: ReturnType<typeof setTimeout> | undefined

  const bioNorm = $derived(normalizeBio(bioDraft))
  const styleDirty = $derived(
    isSelf && !!profile?.hasAccount && (bioNorm.text !== (profile?.bio ?? '') || safeColor(colorDraft) !== safeColor(profile?.color ?? '')),
  )
  const styleValid = $derived(bioNorm.ok && (colorDraft === '' || safeColor(colorDraft) !== ''))

  async function load() {
    try {
      profile = await App.GetShaurmaProfile()
      bioDraft = profile?.bio ?? ''
      colorDraft = profile?.color ?? ''
    } catch { profile = null }
    loading = false
  }
  $effect(() => { if (isSelf) untrack(() => { void load() }) })

  async function saveStyle() {
    if (!styleDirty || !styleValid || saving) return
    saving = true
    try {
      await App.SetShaurmaProfileStyle(bioNorm.text, safeColor(colorDraft))
      bioDraft = bioNorm.text
      await load()
      toast(t('acc.modal.saved'), 'success')
    } catch (e: any) { toast(String(e?.message ?? e), 'error') }
    finally { saving = false }
  }
  function revert() { bioDraft = profile?.bio ?? ''; colorDraft = profile?.color ?? '' }

  // ── Дані: профіль друга — ТІЛЬКИ з сервера ────────────────────────────
  let fp = $state<SocialFriendProfile | null>(null)
  let fpState = $state<'idle' | 'loading' | 'ok' | 'error'>('idle')
  let fpErr = $state('')
  let fpTick = $state(0)
  $effect(() => {
    const id = friendId
    void fpTick
    if (isSelf || !id) { fp = null; fpState = 'idle'; return }
    let dead = false
    fp = null; fpState = 'loading'; fpErr = ''
    App.GetFriendProfile(id)
      .then((r) => { if (!dead) { fp = r; fpState = 'ok' } })
      .catch((e: any) => { if (!dead) { fpErr = String(e?.message ?? e); fpState = 'error' } })
    return () => { dead = true }
  })
  const fpHidden = $derived(!isSelf && fpState === 'ok' && !!fp?.hidden)
  const fpErrText = $derived(/not_friends/.test(fpErr) ? t('acc.modal.friend.notFriend') : t('acc.modal.friend.loadFail'))

  async function copy(kind: string, text: string) {
    if (!text) return
    try { await App.CopyToClipboard(text) }
    catch {
      try { await navigator.clipboard?.writeText(text) } catch { toast(t('acc.modal.copyFail'), 'error'); return }
    }
    copied = kind
    clearTimeout(copiedTimer)
    copiedTimer = setTimeout(() => { if (copied === kind) copied = '' }, 1400)
  }
  onDestroy(() => clearTimeout(copiedTimer))

  // ── Єдина «модель перегляду»: звідки що брати залежить лише від режиму ─
  const nickname = $derived(isSelf ? (profile?.nickname || account?.username || '—') : (friendNick || '—'))
  const handle = $derived(isSelf ? (profile?.handle ?? '') : clean(fp?.handle || f?.handle))
  const accountId = $derived(isSelf ? (profile?.accountId ?? '') : friendId)

  const hue = $derived(hueOf(friendId))
  const aura = $derived(
    isSelf
      ? (safeColor(colorDraft) || safeColor(profile?.color ?? '') || '#ff7a1a')
      : (safeColor(fp?.color) || `hsl(${hue} 72% 52%)`),
  )

  // Статус: свій приходить пропсами, друга — з хаба (там його вже відфільтрував сервер).
  const fStatus = $derived(f?.presence?.status ?? 'offline')
  const packName = $derived(clean(f?.presence?.packName))
  const fStatusText = $derived(
    fStatus === 'in_game' ? (packName ? t('social.profile.playingPack', { pack: packName }) : t('social.playing'))
    : fStatus === 'online' ? t('social.status.online')
    : fStatus === 'away' ? t('social.status.away')
    : (lastSeenText(f?.presence?.lastSeenAt) || t('social.status.offline')))
  const stKind = $derived(isSelf ? statusKind : fStatus)
  const stText = $derived(isSelf ? statusText : fStatusText)

  const friendsSince = $derived.by(() => {
    if (isSelf || !f?.since) return ''
    try { return new Date(f.since).toLocaleDateString(document.documentElement.lang || 'uk', { day: 'numeric', month: 'long', year: 'numeric' }) }
    catch { return '' }
  })
  const friendLink = $derived(!isSelf && f ? buildFriendLink(f.accountId, friendNick, handle) : '')

  // Числа для плиток; null = невідомо/приховано → «—».
  const vSecs = $derived<number | null>(isSelf ? (profile?.gameSeconds ?? 0) : (fp?.gameSeconds ?? null))
  const vBuilds = $derived<number | null>(isSelf ? (profile?.buildsCount ?? 0) : (fp?.buildsCount ?? null))
  const vFriends = $derived<number | null>(isSelf ? (profile?.friends ?? social?.friends?.length ?? 0) : (fp?.friendsCount ?? null))
  const vCreated = $derived<number | null>(isSelf ? (profile?.createdAt ?? null) : (fp?.createdAt ?? null))

  // Власні перемикачі «Вигляд» стосуються лише МОГО вікна; у профілі друга показуємо все.
  const tileOn = (k: 'showPlaytime' | 'showBuilds' | 'showFriends' | 'showSince') => (isSelf ? accountPrefs[k] : true)
  const showStats = $derived(
    isSelf
      ? accountPrefs.showPlaytime || accountPrefs.showBuilds || accountPrefs.showFriends || accountPrefs.showSince
      : fpState === 'ok' && !fpHidden,
  )
  const show3D = $derived(accountPrefs.show3D)

  // ── Статистика з анімацією (count-up) ─────────────────────────────────
  const secs = new Tween(0, { duration: 900, easing: cubicOut })
  const builds = new Tween(0, { duration: 700, easing: cubicOut })
  const friendsN = new Tween(0, { duration: 700, easing: cubicOut })
  $effect(() => {
    const d = dur(900)
    secs.set(vSecs ?? 0, { duration: d })
    builds.set(vBuilds ?? 0, { duration: d })
    friendsN.set(vFriends ?? 0, { duration: d })
  })
  function fmtTime(total: number): string {
    const h = Math.floor(total / 3600)
    const m = Math.floor((total % 3600) / 60)
    if (h <= 0) return t('acc.modal.minutes', { m })
    return t('acc.modal.hoursMin', { h, m })
  }
  const sinceText = $derived.by(() => {
    const c = vCreated
    if (!c) return '—'
    const ms = c < 1e12 ? c * 1000 : c
    // «жовт. 2026» — коротко й в один рядок (повний «жовтень 2026 р.» переносився
    // на другий рядок і ламав вирівнювання картки). Без «р.» — через parts.
    try {
      const parts = new Intl.DateTimeFormat(undefined, { year: 'numeric', month: 'short' }).formatToParts(new Date(ms))
      const m = parts.find(x => x.type === 'month')?.value ?? ''
      const y = parts.find(x => x.type === 'year')?.value ?? ''
      return `${m.replace(/\.$/, '')} ${y}`.trim() || '—'
    } catch { return '—' }
  })

  // ── 3D-модель ─────────────────────────────────────────────────────────
  let stage = $state<HTMLDivElement>()
  let viewerReady = $state(false)
  let viewerFailed = $state(false)
  let spinTimer: ReturnType<typeof setInterval> | undefined

  // Ключ моделі: свій акаунт або нік друга. Саме примітив, а не об'єкт друга, — інакше кожне
  // оновлення presence перестворювало б WebGL-сцену.
  const lookKey = $derived(isSelf ? `self:${account?.id ?? ''}` : `friend:${friendNick}`)

  type Look = { skin: string; cape: string | null; slim: boolean }
  async function fetchLook(): Promise<Look | null> {
    if (isSelf) {
      const look = account ? await App.GetWardrobeActiveLook(account.id).catch(() => null) : null
      return look?.skinDataUrl ? { skin: look.skinDataUrl, cape: look.capeDataUrl ?? null, slim: !!look.slimArms } : null
    }
    if (!friendNick) return null
    // 1) Сервер скінів Shaurma (Shaurma/офлайн-друзі — Mojang їх не знає).
    const sv = await App.GetFriendLook(friendNick).catch(() => null)
    if (sv?.skinDataUrl) return { skin: sv.skinDataUrl, cape: sv.capeDataUrl ?? null, slim: !!sv.slimArms }
    // 2) Mojang (ліцензійний друг зі скіном там).
    const p = await App.LookupWardrobePlayer(friendNick).catch(() => null)
    if (!p?.skinUrl) return null
    const skin = await App.GetWardrobeRemoteSkinDataURL(p.skinUrl).catch(() => '')
    if (!skin) return null
    const cape = p.capeUrl ? await App.GetWardrobeCapeDataURL(p.capeUrl).catch(() => null) : null
    return { skin, cape, slim: !!p.slimArms }
  }

  $effect(() => {
    const el = stage
    void lookKey
    if (!el || !show3D || !open) return
    const spin = untrack(() => accountPrefs.autoSpin)
    let dead = false
    let viewer: WardrobeViewerHandle | null = null
    viewerFailed = false
    viewerReady = false
    let idleId: number | undefined
    let timer: ReturnType<typeof setTimeout> | undefined
    const boot = async () => {
      try {
        const v = await createWardrobeViewer(el)
        if (dead) { v.dispose(); return }
        viewer = v
        const look = await untrack(() => fetchLook())
        if (dead) return
        if (!look) { viewerFailed = true; return }
        v.setSlim(look.slim)
        v.setTexture(look.skin)
        v.setCapeTexture(look.cape)
        v.playAnimation('idle')
        viewerReady = true
        if (spin) {
          // Лёгка «жива» поведінка: час від часу модель повертає голову.
          spinTimer = setInterval(() => { try { v.playAnimation('look') } catch { /* ок */ } }, 7000)
        }
      } catch { if (!dead) viewerFailed = true }
    }
    // Модалка відкривається зі scale/fade — WebGL піднімаємо після першого
    // кадру, інакше компіляція шейдерів збігається з анімацією вікна і
    // лаунчер «залипає» на секунду.
    if (typeof window.requestIdleCallback === 'function') {
      idleId = window.requestIdleCallback(() => { void boot() }, { timeout: 260 })
    } else {
      timer = setTimeout(() => { void boot() }, 80)
    }
    return () => {
      dead = true
      if (idleId !== undefined && typeof window.cancelIdleCallback === 'function') window.cancelIdleCallback(idleId)
      if (timer) clearTimeout(timer)
      clearInterval(spinTimer)
      try { viewer?.dispose() } catch { /* ок */ }
      viewer = null
      viewerReady = false
    }
  })

  // ── Приватність (діє на сервері; лише mode="self") ────────────────────
  let privOverride = $state<SocialPrivacy | null>(null)
  let privBusy = $state('')
  $effect(() => { void social?.privacy; privOverride = null })
  const priv = $derived<SocialPrivacy | undefined>(privOverride ?? social?.privacy)
  const privOk = $derived(!!social?.loggedIn && !!priv)

  // Приватність профілю лежить окремо від хабового знімка — читаємо з сервера напряму.
  let profVis = $state<SocialProfileVisibility | ''>('')
  $effect(() => {
    if (!isSelf) return
    let dead = false
    App.GetProfileVisibility().then((v) => { if (!dead) profVis = v }).catch(() => { /* лишається вимкненим */ })
    return () => { dead = true }
  })

  type PrivKey = 'showStatusTo' | 'showServerTo' | 'showProfileTo' | 'whoCanMessage' | 'friendRequests'
  const PRIV_ROWS: { k: PrivKey; icon: string; title: string; hint: string; opts: [string, string][] }[] = [
    { k: 'showStatusTo', icon: 'ti-circle-dot', title: 'acc.modal.priv.status', hint: 'acc.modal.priv.statusHint',
      opts: [['everyone', 'acc.modal.opt.everyone'], ['friends', 'acc.modal.opt.friends'], ['nobody', 'acc.modal.opt.nobody']] },
    { k: 'showServerTo', icon: 'ti-device-gamepad-2', title: 'acc.modal.priv.server', hint: 'acc.modal.priv.serverHint',
      opts: [['everyone', 'acc.modal.opt.everyone'], ['friends', 'acc.modal.opt.friends'], ['nobody', 'acc.modal.opt.nobody']] },
    { k: 'showProfileTo', icon: 'ti-id-badge-2', title: 'acc.modal.priv.profile', hint: 'acc.modal.priv.profileHint',
      opts: [['friends', 'acc.modal.opt.friends'], ['nobody', 'acc.modal.opt.nobody']] },
    { k: 'whoCanMessage', icon: 'ti-message-circle', title: 'acc.modal.priv.msg', hint: 'acc.modal.priv.msgHint',
      opts: [['everyone', 'acc.modal.opt.everyone'], ['friends', 'acc.modal.opt.friends'], ['nobody', 'acc.modal.opt.nobody']] },
    { k: 'friendRequests', icon: 'ti-user-plus', title: 'acc.modal.priv.req', hint: 'acc.modal.priv.reqHint',
      opts: [['everyone', 'acc.modal.opt.everyone'], ['friends_of_friends', 'acc.modal.opt.fof'], ['nobody', 'acc.modal.opt.nobody']] },
  ]
  function privValue(k: PrivKey): string {
    if (k === 'showProfileTo') return profVis
    const v = (priv as any)?.[k]
    return v ?? (k === 'whoCanMessage' ? 'friends' : '')
  }
  const rowOk = (k: PrivKey) => privOk && (k !== 'showProfileTo' || profVis !== '')

  async function setPriv(k: PrivKey, v: string) {
    if (privBusy || privValue(k) === v) return
    privBusy = k
    try {
      if (k === 'showProfileTo') {
        const prev = profVis
        profVis = v as SocialProfileVisibility
        try { await App.SetProfileVisibility(v as SocialProfileVisibility) }
        catch (e) { profVis = prev; throw e }
      } else {
        if (!priv) return
        const next = { ...priv, [k]: v } as SocialPrivacy
        privOverride = next
        try { await App.SetFriendsPrivacy(next) }
        catch (e) { privOverride = null; throw e }
      }
      toast(t('acc.modal.priv.saved'), 'success')
    } catch (e: any) {
      toast(String(e?.message ?? e), 'error')
    } finally { privBusy = '' }
  }

  const WLINES: [WidgetLine, string][] = [['type', 'acc.modal.look.widget.type'], ['status', 'acc.modal.look.widget.status']]
  // ── Вигляд (локально) ─────────────────────────────────────────────────
  const LOOK_ROWS: { k: AccountToggleKey; icon: string; key: string }[] = [
    { k: 'showPlaytime', icon: 'ti-clock-hour-4', key: 'acc.modal.look.playtime' },
    { k: 'showBuilds', icon: 'ti-package', key: 'acc.modal.look.builds' },
    { k: 'showFriends', icon: 'ti-users', key: 'acc.modal.look.friends' },
    { k: 'showSince', icon: 'ti-calendar-heart', key: 'acc.modal.look.since' },
    { k: 'showBio', icon: 'ti-quote', key: 'acc.modal.look.bio' },
    { k: 'show3D', icon: 'ti-3d-cube-sphere', key: 'acc.modal.look.model' },
    { k: 'autoSpin', icon: 'ti-rotate-360', key: 'acc.modal.look.spin' },
  ]

  const STATUS_COLOR: Record<string, string> = {
    online: '#22c55e', in_game: '#ff8a00', away: '#f59e0b', dnd: '#ef4444',
    invisible: '#9ca3af', offline: '#6b7280', connecting: '#eab308',
  }
  const accKind = $derived(
    account?.isLicensed ? t('licensed') : account?.isShaurmaSkinAccount ? t('shaurmaAccount') : t('notLicensed'),
  )
  const accKindIcon = $derived(
    account?.isLicensed ? 'ti-rosette-discount-check' : account?.isShaurmaSkinAccount ? 'ti-cloud-upload' : 'ti-alert-triangle',
  )
</script>

<UiModal
  {open}
  title={isSelf ? t('acc.modal.title') : (friendNick || t('social.profile.title'))}
  subtitle={isSelf ? '' : stText}
  icon="ti-user-circle"
  color={aura}
  width="920px"
  bodyClass="am-ui"
  onclose={onclose}
>
  <div class="am" style="--aura:{aura}" data-testid={isSelf ? 'account-modal' : 'friend-profile-modal'}>
    <!-- ── Ліва колонка: 3D / аватар ── -->
    <aside class="am-hero stg" style="--i:0">
      <div class="am-aura" aria-hidden="true"></div>
      {#each [0, 1, 2, 3, 4, 5, 6] as n}
        <i class="am-spark" style="--n:{n}" aria-hidden="true"></i>
      {/each}

      <div class="am-stage-wrap">
        {#if show3D && !viewerFailed}
          <div class="am-stage" class:ready={viewerReady} bind:this={stage}></div>
          {#if !viewerReady}
            <div class="am-skel" out:fade={{ duration: dur(200) }}><i class="ti ti-loader-2 spin"></i></div>
          {/if}
        {:else}
          <div class="am-flat" in:scale={{ duration: dur(300), start: 0.8 }}>
            {#if head}<img src={head} alt="" />{:else}<span class="am-letter">{nickname.charAt(0).toUpperCase()}</span>{/if}
          </div>
        {/if}
        <div class="am-floor" aria-hidden="true"></div>
      </div>

      {#if show3D && viewerReady}
        <div class="am-hint" in:fade={{ duration: dur(300), delay: 600 }}><i class="ti ti-hand-move"></i> {t('acc.modal.rotateHint')}</div>
      {:else if viewerFailed}
        <div class="am-hint"><i class="ti ti-info-circle"></i> {t('acc.modal.noModel')}</div>
      {/if}

      <div class="am-id">
        <div class="am-name">{nickname}</div>
        <div class="am-badges">
          {#if isSelf}
            <span class="am-badge"><i class="ti {accKindIcon}"></i> {accKind}</span>
          {/if}
          <span class="am-badge st" style="--c:{STATUS_COLOR[stKind] ?? '#6b7280'}"><i class="am-dot"></i> {stText}</span>
          {#if !isSelf && friendsSince}
            <span class="am-badge"><i class="ti ti-heart-handshake"></i> {t('acc.modal.friend.since', { date: friendsSince })}</span>
          {/if}
          {#if !isSelf && muted}<span class="am-badge bad"><i class="ti ti-bell-off"></i> {t('social.profile.muted')}</span>{/if}
          {#if !isSelf && blocked}<span class="am-badge bad"><i class="ti ti-ban"></i> {t('social.profile.blocked')}</span>{/if}
        </div>
        {#if handle}
          <button class="am-handle" onclick={() => copy('handle', '@' + handle)} title={t('acc.modal.copy')}>
            <span>@{handle}</span>
            <i class="ti {copied === 'handle' ? 'ti-check' : 'ti-copy'}"></i>
          </button>
        {/if}

        {#if !isSelf && f}
          <div class="am-acts">
            <button class="am-btn pri" onclick={() => onmessage(f!)}>
              <i class="ti ti-message-circle-2"></i> {t('social.profile.message')}
            </button>
            <button class="am-btn ghost" class:on={muted} data-testid="profile-mute-btn"
                    onclick={() => onmute(f!)}
                    use:tip={muted ? t('social.profile.muteOff') : t('social.profile.muteOn')}
                    aria-label={muted ? t('social.profile.muteOff') : t('social.profile.muteOn')} aria-pressed={muted}>
              <i class="ti {muted ? 'ti-bell-off' : 'ti-bell'}"></i>
            </button>
            <button class="am-btn ghost" onclick={() => copy('link', friendLink)}
                    use:tip={t('social.profile.copyLink')} aria-label={t('social.profile.copyLink')}>
              <i class="ti {copied === 'link' ? 'ti-check' : 'ti-link'}"></i>
            </button>
            <button class="am-btn ghost" onclick={() => copy('id', f!.accountId)}
                    use:tip={`${t('acc.modal.friend.copyId')} · ${shortId(f.accountId)}`} aria-label={t('acc.modal.friend.copyId')}>
              <i class="ti {copied === 'id' ? 'ti-check' : 'ti-fingerprint'}"></i>
            </button>
          </div>
        {/if}
      </div>
    </aside>

    <!-- ── Права колонка ── -->
    <section class="am-main">
      {#if isSelf}
        <div class="am-tabs stg" style="--i:1" role="tablist">
          <span class="am-pill" style="transform:translateX({tabIdx * 100}%)" aria-hidden="true"></span>
          {#each TABS as x (x.id)}
            <button class="am-tab" class:on={tab === x.id} role="tab" aria-selected={tab === x.id} onclick={() => (tab = x.id)}>
              <i class="ti {x.icon}"></i> {t(x.key)}
            </button>
          {/each}
        </div>
      {/if}

      {#key tab}
        <div class="am-pane" in:fly={{ y: 10, duration: dur(260), easing: cubicOut }}>
          {#if tab === 'profile'}
            {#if showStats}
              <div class="am-stats">
                {#if tileOn('showPlaytime')}
                  <div class="am-stat stg" style="--i:2" transition:scale={{ duration: dur(220), start: 0.9 }}>
                    <i class="ti ti-clock-hour-4"></i><b>{vSecs === null ? '—' : fmtTime(Math.round(secs.current))}</b><small>{t('acc.modal.stat.time')}</small>
                  </div>
                {/if}
                {#if tileOn('showBuilds')}
                  <div class="am-stat stg" style="--i:3" transition:scale={{ duration: dur(220), start: 0.9 }}>
                    <i class="ti ti-package"></i><b>{vBuilds === null ? '—' : Math.round(builds.current)}</b><small>{t('acc.modal.stat.builds')}</small>
                  </div>
                {/if}
                {#if tileOn('showFriends')}
                  <div class="am-stat stg" style="--i:4" transition:scale={{ duration: dur(220), start: 0.9 }}>
                    <i class="ti ti-users"></i><b>{vFriends === null ? '—' : Math.round(friendsN.current)}</b><small>{t('acc.modal.stat.friends')}</small>
                  </div>
                {/if}
                {#if tileOn('showSince')}
                  <div class="am-stat stg" style="--i:5" transition:scale={{ duration: dur(220), start: 0.9 }}>
                    <i class="ti ti-calendar-heart"></i><b class="sm">{sinceText}</b><small>{t('acc.modal.stat.since')}</small>
                  </div>
                {/if}
              </div>
            {/if}

            {#if isSelf}
              <!-- ═══ Мій профіль: редагування ═══ -->
              {#if loading}
                <div class="am-skel-line"></div><div class="am-skel-line short"></div>
              {:else if !profile?.hasAccount}
                <div class="am-empty">
                  <i class="ti ti-user-question"></i>
                  <div><b>{t('acc.modal.noProfile')}</b><small>{t('acc.modal.noProfileHint')}</small></div>
                </div>
              {:else}
                {#if accountPrefs.showBio}
                  <div class="am-block" transition:fly={{ y: 8, duration: dur(220) }}>
                    <label class="am-lbl" for="am-bio"><i class="ti ti-quote"></i> {t('acc.modal.bio')}</label>
                    <textarea id="am-bio" class="am-bio" rows="3" maxlength="240" placeholder={t('acc.modal.bioPh')} bind:value={bioDraft}></textarea>
                    <div class="am-bio-foot">
                      <span class:bad={!bioNorm.ok}>{bioLength(bioDraft)}/120</span>
                    </div>
                  </div>
                {/if}

                <div class="am-block">
                  <div class="am-lbl"><i class="ti ti-palette"></i> {t('acc.modal.color')}</div>
                  <div class="am-colors">
                    <button class="am-sw none" class:sel={colorDraft === ''} onclick={() => (colorDraft = '')} title={t('acc.modal.colorNone')} aria-label={t('acc.modal.colorNone')}>
                      <i class="ti ti-ban"></i>
                    </button>
                    {#each PROFILE_COLORS as c (c)}
                      <button class="am-sw" class:sel={safeColor(colorDraft) === c} style="--c:{c}" onclick={() => (colorDraft = c)} aria-label={c}></button>
                    {/each}
                  </div>
                </div>

                {#if styleDirty}
                  <div class="am-save" transition:fly={{ y: 10, duration: dur(200) }}>
                    <button class="btn btn-ghost btn-sm" onclick={revert} disabled={saving}>{t('acc.modal.revert')}</button>
                    <button class="btn btn-primary btn-sm" onclick={saveStyle} disabled={!styleValid || saving}>
                      {#if saving}<i class="ti ti-loader-2 spin"></i>{:else}<i class="ti ti-device-floppy"></i>{/if} {t('acc.modal.save')}
                    </button>
                  </div>
                {/if}
              {/if}

            {:else}
              <!-- ═══ Профіль друга: лише перегляд, дані — з сервера ═══ -->
              {#if fpState === 'loading' || fpState === 'idle'}
                <div class="am-skel-line"></div><div class="am-skel-line short"></div>
              {:else if fpState === 'error'}
                <div class="am-empty">
                  <i class="ti ti-plug-off"></i>
                  <div>
                    <b>{fpErrText}</b>
                    {#if !/not_friends/.test(fpErr)}
                      <button class="btn btn-ghost btn-sm am-retry" onclick={() => fpTick++}><i class="ti ti-refresh"></i> {t('acc.modal.friend.retry')}</button>
                    {/if}
                  </div>
                </div>
              {:else if fpHidden}
                <div class="am-empty lock">
                  <i class="ti ti-lock"></i>
                  <div><b>{t('acc.modal.friend.hidden')}</b><small>{t('acc.modal.friend.hiddenHint')}</small></div>
                </div>
              {:else}
                <div class="am-block" in:fly={{ y: 8, duration: dur(220) }}>
                  <div class="am-lbl"><i class="ti ti-quote"></i> {t('acc.modal.bio')}</div>
                  <div class="am-bio ro" class:empty={!fp?.bio}>{fp?.bio || t('acc.modal.friend.noBio')}</div>
                </div>
              {/if}

              {#if f}
                <div class="am-danger">
                  <div class="am-lbl"><i class="ti ti-shield-lock"></i> {t('social.priv.title')}</div>
                  <div class="am-danger-row">
                    <button class="am-dbtn" disabled={busy} data-testid="profile-block-btn" onclick={() => onblock(f!)}>
                      <i class="ti ti-ban"></i> {blocked ? t('social.profile.unblock') : t('social.profile.block')}
                    </button>
                    <button class="am-dbtn" disabled={busy} onclick={() => onremove(f!)}>
                      {#if busy}<i class="ti ti-loader-2 spin"></i>{:else}<i class="ti ti-user-minus"></i>{/if}
                      {t('social.profile.remove')}
                    </button>
                  </div>
                </div>
              {/if}
            {/if}

          {:else if tab === 'privacy'}
            <div class="am-note"><i class="ti ti-shield-check"></i> {t('acc.modal.priv.note')}</div>
            {#if !privOk}
              <div class="am-empty"><i class="ti ti-plug-off"></i><div><b>{t('acc.modal.priv.need')}</b></div></div>
            {/if}
            <div class="am-priv" class:off={!privOk}>
              {#each PRIV_ROWS as r, i (r.k)}
                {@const cur = privValue(r.k)}
                {@const ci = Math.max(0, r.opts.findIndex((o) => o[0] === cur))}
                <div class="am-prow stg" style="--i:{i + 2}">
                  <span class="am-pico"><i class="ti {r.icon}"></i></span>
                  <span class="am-ptxt"><b>{t(r.title)}</b><small>{t(r.hint)}</small></span>
                  <div class="am-seg" style="--n:{r.opts.length}" role="radiogroup" aria-label={t(r.title)}>
                    <span class="am-seg-pill" style="transform:translateX({ci * 100}%)" aria-hidden="true"></span>
                    {#each r.opts as o (o[0])}
                      <button role="radio" aria-checked={cur === o[0]} class:on={cur === o[0]} disabled={!rowOk(r.k) || !!privBusy} onclick={() => setPriv(r.k, o[0])}>{t(o[1])}</button>
                    {/each}
                  </div>
                </div>
              {/each}
            </div>

          {:else}
            <div class="am-note local"><i class="ti ti-device-desktop"></i> {t('acc.modal.look.note')}</div>
            <div class="am-looks">
              {#each LOOK_ROWS as r, i (r.k)}
                <button class="am-tg stg" style="--i:{i + 2}" role="switch" aria-checked={accountPrefs[r.k]} class:on={accountPrefs[r.k]}
                        onclick={() => setAccountPref(r.k, !accountPrefs[r.k])}>
                  <span class="am-pico"><i class="ti {r.icon}"></i></span>
                  <span class="am-ttxt">{t(r.key)}</span>
                  <span class="am-sw-track"><span class="am-sw-knob"></span></span>
                </button>
              {/each}
            </div>
            <!-- Що писати під ніком у віджеті акаунта (sidebar): лише тип АБО лише статус.
                 Підміни при наведенні немає — показується те, що вибрано тут. -->
            <div class="am-prow stg am-wline" style="--i:{LOOK_ROWS.length + 2}">
              <span class="am-pico"><i class="ti ti-id-badge-2"></i></span>
              <span class="am-ptxt"><b>{t('acc.modal.look.widget')}</b><small>{t('acc.modal.look.widget.hint')}</small></span>
              <div class="am-seg" style="--n:2" role="radiogroup" aria-label={t('acc.modal.look.widget')}>
                <span class="am-seg-pill" style="transform:translateX({accountPrefs.widgetLine === 'status' ? 100 : 0}%)" aria-hidden="true"></span>
                {#each WLINES as o (o[0])}
                  <button role="radio" aria-checked={accountPrefs.widgetLine === o[0]} class:on={accountPrefs.widgetLine === o[0]} onclick={() => setAccountPref('widgetLine', o[0])}>{t(o[1])}</button>
                {/each}
              </div>
            </div>
            <div class="am-save">
              <button class="btn btn-ghost btn-sm" onclick={resetAccountPrefs}><i class="ti ti-restore"></i> {t('acc.modal.look.reset')}</button>
            </div>
          {/if}
        </div>
      {/key}
    </section>
  </div>
</UiModal>

<style>
  .am { display: grid; grid-template-columns: 290px minmax(0, 1fr); gap: 20px; min-height: 480px; }
  .stg { animation: am-rise .5s cubic-bezier(.22, 1, .36, 1) both; animation-delay: calc(var(--i, 0) * 55ms); }
  @keyframes am-rise { from { opacity: 0; transform: translateY(12px) scale(.98); } to { opacity: 1; transform: none; } }
  @media (prefers-reduced-motion: reduce) { .am, .am :global(*) { animation: none !important; } }

  /* ── Hero ── */
  .am-hero { position: relative; display: flex; flex-direction: column; align-items: center; gap: 10px; padding: 14px 12px 16px; border-radius: 20px; overflow: hidden; isolation: isolate;
    background: linear-gradient(180deg, color-mix(in srgb, var(--aura) 14%, var(--bg-panel)) 0%, var(--bg-panel) 70%); border: 1px solid color-mix(in srgb, var(--aura) 30%, var(--border)); transition: background .4s, border-color .4s; }
  .am-aura { position: absolute; inset: -30% -20% 20% -20%; z-index: -1; background: radial-gradient(closest-side, color-mix(in srgb, var(--aura) 45%, transparent), transparent 75%); filter: blur(14px); animation: am-breathe 5s ease-in-out infinite; }
  @keyframes am-breathe { 0%, 100% { opacity: .65; transform: scale(1); } 50% { opacity: 1; transform: scale(1.08); } }
  .am-spark { position: absolute; z-index: -1; width: 4px; height: 4px; border-radius: 50%; background: var(--aura); opacity: 0; left: calc(10% + var(--n) * 12%); bottom: 22%; animation: am-float calc(4s + var(--n) * .6s) ease-in infinite; animation-delay: calc(var(--n) * -.9s); }
  @keyframes am-float { 0% { opacity: 0; transform: translateY(0) scale(.6); } 20% { opacity: .9; } 100% { opacity: 0; transform: translateY(-190px) scale(1.2) translateX(calc((var(--n) - 3) * 6px)); } }

  .am-stage-wrap { position: relative; width: 100%; height: 330px; }
  .am-stage { position: absolute; inset: 0; opacity: 0; transition: opacity .5s; cursor: grab; }
  .am-stage:active { cursor: grabbing; }
  .am-stage.ready { opacity: 1; }
  .am-stage :global(canvas) { width: 100% !important; height: 100% !important; display: block; }
  .am-skel { position: absolute; inset: 0; display: grid; place-items: center; color: var(--text-mute); font-size: 26px; }
  .am-flat { position: absolute; inset: 0; display: grid; place-items: center; }
  .am-flat img { width: 148px; height: 148px; border-radius: 28px; image-rendering: pixelated; box-shadow: 0 14px 40px color-mix(in srgb, var(--aura) 40%, transparent); animation: am-bob 4s ease-in-out infinite; }
  @keyframes am-bob { 0%, 100% { transform: translateY(0); } 50% { transform: translateY(-8px); } }
  .am-floor { position: absolute; left: 50%; bottom: 6px; width: 70%; height: 22px; transform: translateX(-50%); border-radius: 50%; z-index: -1;
    background: radial-gradient(closest-side, color-mix(in srgb, var(--aura) 55%, transparent), transparent); animation: am-pulse 3.2s ease-in-out infinite; }
  @keyframes am-pulse { 0%, 100% { opacity: .6; transform: translateX(-50%) scaleX(.9); } 50% { opacity: 1; transform: translateX(-50%) scaleX(1.05); } }
  .am-hint { font: 500 10.5px/1 var(--font-ui); color: var(--text-mute); display: inline-flex; gap: 5px; align-items: center; }

  .am-id { display: grid; gap: 8px; justify-items: center; width: 100%; margin-top: 2px; }
  .am-name { font: 800 19px/1.1 var(--font-ui); color: var(--text); max-width: 100%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .am-badges { display: flex; flex-wrap: wrap; justify-content: center; gap: 6px; }
  .am-badge { display: inline-flex; align-items: center; gap: 5px; padding: 4px 9px; border-radius: 999px; font: 650 10.5px/1 var(--font-ui); color: var(--text-dim); background: var(--hover); border: 1px solid var(--border); }
  .am-badge i { font-size: 13px; }
  .am-badge.st { color: var(--text); border-color: color-mix(in srgb, var(--c) 45%, var(--border)); background: color-mix(in srgb, var(--c) 12%, transparent); }
  .am-dot { width: 7px; height: 7px; border-radius: 50%; background: var(--c); box-shadow: 0 0 0 0 var(--c); animation: am-ping 2.2s ease-out infinite; }
  @keyframes am-ping { 0% { box-shadow: 0 0 0 0 color-mix(in srgb, var(--c) 60%, transparent); } 70%, 100% { box-shadow: 0 0 0 6px transparent; } }
  .am-handle { display: inline-flex; align-items: center; gap: 6px; padding: 5px 11px; border-radius: 10px; cursor: pointer; border: 1px dashed var(--border-hi); background: transparent; color: var(--text-dim); font: 600 12px/1 var(--font-ui); transition: all .15s; position: relative; overflow: hidden; }
  .am-handle::after { content: ''; position: absolute; inset: 0; background: linear-gradient(110deg, transparent 30%, color-mix(in srgb, var(--aura) 35%, transparent) 50%, transparent 70%); transform: translateX(-120%); animation: am-shine 4.5s ease-in-out infinite; }
  @keyframes am-shine { 0%, 60% { transform: translateX(-120%); } 100% { transform: translateX(120%); } }
  .am-handle:hover { color: var(--text); border-style: solid; border-color: var(--aura); }

  /* ── Праворуч ── */
  .am-main { display: grid; align-content: start; gap: 14px; min-width: 0; }
  .am-tabs { position: relative; display: grid; grid-template-columns: repeat(3, 1fr); padding: 3px; border-radius: 13px; background: var(--bg-input); border: 1px solid var(--border); }
  .am-pill { position: absolute; top: 3px; bottom: 3px; left: 3px; width: calc((100% - 6px) / var(--n, 3)); border-radius: 10px; pointer-events: none;
    background: color-mix(in srgb, var(--aura) 18%, var(--bg-panel)); box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--aura) 45%, transparent); transition: transform .3s cubic-bezier(.34, 1.3, .64, 1), background .4s; }
  .am-tab { position: relative; z-index: 1; display: inline-flex; align-items: center; justify-content: center; gap: 7px; padding: 10px 6px; border: 0; background: transparent; cursor: pointer; color: var(--text-mute); font: 700 12.5px/1 var(--font-ui); border-radius: 10px; transition: color .18s; }
  .am-tab i { font-size: 16px; }
  .am-tab:hover, .am-tab.on { color: var(--text); }
  .am-pane { display: grid; gap: 14px; align-content: start; }

  .am-stats { display: grid; grid-template-columns: repeat(auto-fit, minmax(118px, 1fr)); gap: 10px; }
  .am-stat { position: relative; display: grid; gap: 4px; padding: 13px 14px; border-radius: 15px; background: var(--bg-input); border: 1px solid var(--border); overflow: hidden; transition: transform .18s, border-color .18s; }
  .am-stat:hover { transform: translateY(-2px); border-color: color-mix(in srgb, var(--aura) 55%, var(--border)); }
  .am-stat > i { position: absolute; right: 10px; top: 10px; font-size: 20px; color: color-mix(in srgb, var(--aura) 70%, var(--text-dim)); opacity: .55; }
  .am-stat b { font: 800 20px/1.15 var(--font-ui); color: var(--text); font-variant-numeric: tabular-nums; }
  .am-stat b.sm { font-size: 17px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; padding-right: 26px; }
  .am-stat small { color: var(--text-mute); font: 500 11px/1.2 var(--font-ui); }

  .am-block { display: grid; gap: 8px; }
  .am-lbl { display: inline-flex; align-items: center; gap: 6px; font: 700 12px/1 var(--font-ui); color: var(--text-dim); }
  .am-bio { width: 100%; box-sizing: border-box; resize: none; padding: 10px 12px; border-radius: 12px; background: var(--bg-input); border: 1px solid var(--border); color: var(--text); font: 450 13px/1.45 var(--font-ui); outline: 0; transition: border-color .15s, box-shadow .15s; }
  .am-bio:focus { border-color: var(--aura); box-shadow: 0 0 0 3px color-mix(in srgb, var(--aura) 22%, transparent); }
  .am-bio-foot { display: flex; justify-content: flex-end; font: 600 10.5px/1 var(--font-ui); color: var(--text-mute); }
  .am-bio-foot .bad { color: var(--red); }
  .am-colors { display: flex; flex-wrap: wrap; gap: 8px; }
  .am-sw { width: 28px; height: 28px; border-radius: 50%; cursor: pointer; border: 2px solid transparent; background: var(--c); outline: 2px solid transparent; outline-offset: 2px; transition: transform .16s cubic-bezier(.34, 1.56, .64, 1), outline-color .16s; }
  .am-sw:hover { transform: scale(1.14); }
  .am-sw.sel { outline-color: var(--c); transform: scale(1.1); }
  .am-sw.none { background: var(--hover); color: var(--text-mute); display: grid; place-items: center; border-color: var(--border-hi); }
  .am-sw.none.sel { outline-color: var(--text-mute); }
  .am-save { display: flex; justify-content: flex-end; gap: 8px; }

  .am-note { display: flex; align-items: center; gap: 9px; padding: 10px 13px; border-radius: 12px; font: 450 12px/1.4 var(--font-ui); color: var(--text-dim); background: rgba(34, 197, 94, .08); border: 1px solid rgba(34, 197, 94, .25); }
  .am-note i { font-size: 18px; color: #22c55e; flex: 0 0 auto; }
  .am-note.local { background: var(--hover); border-color: var(--border); }
  .am-note.local i { color: var(--text-mute); }

  .am-priv { display: grid; gap: 8px; transition: opacity .2s; }
  .am-priv.off { opacity: .45; pointer-events: none; }
  .am-prow, .am-tg { display: flex; align-items: center; gap: 12px; padding: 11px 13px; border-radius: 14px; background: var(--bg-input); border: 1px solid var(--border); transition: border-color .15s, background .15s; }
  .am-prow:hover, .am-tg:hover { border-color: var(--border-hi); }
  .am-pico { flex: 0 0 auto; width: 34px; height: 34px; border-radius: 10px; display: grid; place-items: center; font-size: 18px; background: color-mix(in srgb, var(--aura) 14%, transparent); color: color-mix(in srgb, var(--aura) 80%, var(--text)); }
  .am-ptxt { display: grid; gap: 3px; flex: 1; min-width: 0; }
  .am-ptxt b { font: 650 13px/1.2 var(--font-ui); color: var(--text); }
  .am-ptxt small { font: 400 11px/1.3 var(--font-ui); color: var(--text-mute); }
  .am-seg { position: relative; display: grid; grid-auto-flow: column; grid-auto-columns: 1fr; padding: 3px; border-radius: 10px; background: var(--bg-panel); border: 1px solid var(--border); flex: 0 0 auto; }
  .am-seg-pill { position: absolute; top: 3px; bottom: 3px; left: 3px; width: calc((100% - 6px) / var(--n, 3)); border-radius: 7px; background: var(--aura); opacity: .92; transition: transform .26s cubic-bezier(.34, 1.3, .64, 1); pointer-events: none; }
  .am-seg button { position: relative; z-index: 1; border: 0; background: transparent; cursor: pointer; padding: 6px 10px; font: 700 11px/1 var(--font-ui); color: var(--text-mute); border-radius: 7px; white-space: nowrap; transition: color .18s; }
  .am-seg button.on { color: #111; }
  .am-seg button:disabled { cursor: default; }

  .am-wline { margin-top: 8px; }
  .am-looks { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; }
  .am-tg { cursor: pointer; text-align: left; color: var(--text); width: 100%; }
  .am-tg.on { border-color: color-mix(in srgb, var(--aura) 45%, var(--border)); }
  .am-ttxt { flex: 1; min-width: 0; font: 600 12.5px/1.25 var(--font-ui); }
  .am-sw-track { flex: 0 0 auto; position: relative; width: 36px; height: 20px; border-radius: 999px; background: var(--hover-strong); transition: background .2s; }
  .am-sw-knob { position: absolute; top: 2px; left: 2px; width: 16px; height: 16px; border-radius: 50%; background: #fff; transition: transform .25s cubic-bezier(.34, 1.56, .64, 1); box-shadow: 0 1px 3px rgba(0, 0, 0, .4); }
  .am-tg.on .am-sw-track { background: var(--aura); }
  .am-tg.on .am-sw-knob { transform: translateX(16px); }

  .am-empty { display: flex; align-items: center; gap: 12px; padding: 14px; border-radius: 14px; background: var(--hover); color: var(--text-mute); }
  .am-empty > i { font-size: 26px; flex: 0 0 auto; }
  .am-empty b { display: block; color: var(--text); font: 650 13px/1.3 var(--font-ui); }
  .am-empty small { display: block; font: 400 11.5px/1.4 var(--font-ui); margin-top: 3px; }
  .am-skel-line { height: 44px; border-radius: 12px; background: linear-gradient(90deg, var(--hover), var(--hover-strong), var(--hover)); background-size: 200% 100%; animation: am-sh 1.1s linear infinite; }
  .am-skel-line.short { width: 60%; height: 28px; }
  @keyframes am-sh { to { background-position: -200% 0; } }

  /* ── Режим перегляду (профіль друга): той самий вигляд, без редагування ── */
  .am-letter { font: 800 56px/1 var(--font-ui); color: var(--text-mute); }
  .am-badge.bad { color: var(--red); border-color: color-mix(in srgb, var(--red) 32%, transparent); background: color-mix(in srgb, var(--red) 10%, transparent); }

  .am-acts { display: flex; gap: 6px; width: 100%; margin-top: 4px; }
  .am-btn { display: inline-flex; align-items: center; justify-content: center; gap: 6px; height: 34px; padding: 0 12px; border-radius: 10px; border: 1px solid transparent; cursor: pointer; font: 750 12px/1 var(--font-ui);
    transition: filter .14s, transform .12s, border-color .14s, color .14s, background .14s; }
  .am-btn:active { transform: scale(.96); }
  .am-btn i { font-size: 15px; }
  .am-btn.pri { flex: 1; min-width: 0; color: #111; background: var(--aura); }
  .am-btn.pri:hover { filter: brightness(1.08); }
  .am-btn.ghost { width: 34px; padding: 0; color: var(--text-mute); background: var(--bg-input); border-color: var(--border-hi); }
  .am-btn.ghost:hover { color: var(--text); border-color: color-mix(in srgb, var(--aura) 55%, transparent); }
  .am-btn.ghost.on { color: var(--red); border-color: color-mix(in srgb, var(--red) 45%, transparent); background: color-mix(in srgb, var(--red) 10%, transparent); }

  /* Біо друга — той самий блок, що й поле редагування, але без курсора й рамки фокуса. */
  .am-bio.ro { min-height: 76px; white-space: pre-wrap; overflow-wrap: anywhere; cursor: default; user-select: text; }
  .am-bio.ro.empty { color: var(--text-mute); font-style: italic; }
  .am-empty.lock > i { color: var(--text-dim); }
  .am-retry { margin-top: 8px; }

  .am-danger { display: grid; gap: 8px; padding: 12px; border-radius: 14px; background: var(--bg-input); border: 1px solid color-mix(in srgb, var(--red) 24%, var(--border)); }
  .am-danger .am-lbl i { color: var(--red); }
  .am-danger-row { display: flex; flex-wrap: wrap; gap: 6px; }
  .am-dbtn { display: inline-flex; align-items: center; justify-content: center; gap: 6px; height: 32px; padding: 0 12px; border-radius: 9px; cursor: pointer;
    border: 1px solid color-mix(in srgb, var(--red) 32%, transparent); background: color-mix(in srgb, var(--red) 8%, transparent); color: var(--red); font: 700 11.5px/1 var(--font-ui); transition: background .14s, color .14s; }
  .am-dbtn:hover:not(:disabled) { color: #fff; background: var(--red); }
  .am-dbtn:disabled { opacity: .55; cursor: default; }
  .am-dbtn i { font-size: 14px; }

  @media (max-width: 860px) { .am { grid-template-columns: minmax(0, 1fr); } .am-looks { grid-template-columns: minmax(0, 1fr); } }
</style>
