<script lang="ts">
  import { tip as launcherTip } from '../friends/tooltip'

  // Крок «Акаунт» майстра.
  //   • вибір: ліцензія → Microsoft, без ліцензії → Google (радимо), або просто нік;
  //   • новий акаунт Шаурми → нік прямо тут (без окремого вікна);
  //   • без ліцензії → скін (живу 3D-модель показує ліва панель) і профіль;
  //   • ліцензія → лише профіль: нік і скін беруться з Mojang.
  // Сам вхід виконує App.svelte, сюди приходять стан і колбеки.
  import { untrack } from 'svelte'
  import { fly, fade, slide } from 'svelte/transition'
  import { t } from '../i18n.svelte'
  import { App } from '../wails'
  import { toast } from '../toast.svelte'
  import { pirateNicknameError, sanitizeNick } from '../validation'
  import { generateNick } from '../nickgen'
  import { normalizeHandle } from '../friends/handle'
  import { BIO_MAX, PROFILE_COLORS, normalizeBio, bioLength } from '../friends/profileStyle'
  import type { Account, ShaurmaProfile } from '../types'
  import PlayerHead from './PlayerHead.svelte'
  import steve from '../../assets/skins/steve.png'
  import alex from '../../assets/skins/alex.png'
  import ari from '../../assets/skins/ari.png'
  import efe from '../../assets/skins/efe.png'
  import kai from '../../assets/skins/kai.png'
  import makena from '../../assets/skins/makena.png'
  import noor from '../../assets/skins/noor.png'
  import sunny from '../../assets/skins/sunny.png'
  import zuri from '../../assets/skins/zuri.png'

  type Preview = { texture: string; slim: boolean } | null

  let {
    accounts = [],
    activeAccountId = '',
    heads = {},
    msStep = 'idle',
    googleStep = 'idle',
    skingate = false,
    nickAsk = null,
    onMsLogin = null,
    onMsCancel = null,
    onGoogleLogin = null,
    onGoogleCancel = null,
    onPirate = null,
    onConfirmNick = null,
    onCancelNick = null,
    preview = $bindable(null),
  }: {
    accounts?: Account[]
    activeAccountId?: string
    heads?: Record<string, string>
    msStep?: 'idle' | 'waiting'
    googleStep?: 'idle' | 'waiting'
    skingate?: boolean
    nickAsk?: null | { provider: string; noLicense: string }
    onMsLogin?: null | (() => void)
    onMsCancel?: null | (() => void)
    onGoogleLogin?: null | (() => void)
    onGoogleCancel?: null | (() => void)
    onPirate?: null | ((nick: string) => Promise<boolean> | boolean | void)
    onConfirmNick?: null | ((nick: string) => Promise<void>)
    onCancelNick?: null | (() => void)
    preview?: Preview
  } = $props()

  const SKINS = [
    { id: 'steve', url: steve, slim: false }, { id: 'alex', url: alex, slim: true }, { id: 'ari', url: ari, slim: false },
    { id: 'efe', url: efe, slim: false }, { id: 'kai', url: kai, slim: false }, { id: 'makena', url: makena, slim: false },
    { id: 'noor', url: noor, slim: false }, { id: 'sunny', url: sunny, slim: false }, { id: 'zuri', url: zuri, slim: false },
  ]

  const waiting = $derived(msStep === 'waiting' || googleStep === 'waiting')
  const acc = $derived(accounts.find(a => a.id === activeAccountId) ?? accounts[accounts.length - 1])
  const accHead = $derived(acc ? (heads[acc.uuid || acc.username] ?? '') : '')
  let adding = $state(false)
  let showOffline = $state(false)

  // ── Профіль Шаурми активного акаунта ──
  let profile = $state<ShaurmaProfile | null>(null)
  let profileFor = ''
  $effect(() => {
    const id = acc?.id ?? ''
    if (!id || !skingate || id === profileFor) return
    profileFor = id
    untrack(() => loadProfile())
  })
  async function loadProfile() {
    try {
      profile = await App.GetShaurmaProfile()
      handle = profile?.handle ?? ''
      bio = profile?.bio ?? ''
      color = profile?.color || PROFILE_COLORS[0]
    } catch { profile = null }
  }
  const hasShaurma = $derived(!!profile?.hasAccount)
  const licensed = $derived(!!acc?.isLicensed)
  let tab = $state<'skin' | 'profile'>('skin')
  const showSkin = $derived(skingate && hasShaurma && !licensed)
  // Щойно з'ясувалось, що акаунту доступний скін, — відкриваємо саме його (раз).
  let skinTabShown = false
  $effect(() => { if (showSkin && !skinTabShown) { skinTabShown = true; tab = 'skin' } })

  // ── Офлайн-нік ──
  let pirateNick = $state('')
  let pirateTouched = $state(false)
  let pirateBusy = $state(false)
  const pirateErr = $derived(pirateNicknameError(pirateNick.trim()))
  async function pirate() {
    if (pirateErr || !pirateNick.trim() || pirateBusy) return
    pirateBusy = true
    try {
      const ok = await onPirate?.(pirateNick.trim())
      if (ok !== false) { pirateNick = ''; pirateTouched = false; showOffline = false; adding = false }
    } finally { pirateBusy = false }
  }

  // ── Нік нового акаунта Шаурми ──
  let nick = $state('')
  let nickSt = $state<'idle' | 'checking' | 'ok' | 'bad'>('idle')
  let nickMsg = $state('')
  let nickBusy = $state(false)
  let seq = 0
  let timer: ReturnType<typeof setTimeout> | null = null
  async function checkNick(v: string): Promise<boolean> {
    const my = ++seq
    const local = pirateNicknameError(v)
    if (!v || local) { nickSt = v ? 'bad' : 'idle'; nickMsg = local; return false }
    nickSt = 'checking'; nickMsg = ''
    try {
      const r = await App.CheckShaurmaNick(v)
      if (my !== seq) return false
      nickSt = r.available ? 'ok' : 'bad'
      nickMsg = r.available ? '' : (r.message || t('nick.taken'))
      return r.available
    } catch (e: any) {
      if (my !== seq) return false
      nickSt = 'idle'; nickMsg = String(e?.message ?? e)
      return false
    }
  }
  function nickInput(v: string) {
    nick = sanitizeNick(v)
    if (timer) clearTimeout(timer)
    nickSt = nick ? 'checking' : 'idle'
    timer = setTimeout(() => checkNick(nick), 350)
  }
  async function rollNick() {
    for (let i = 0; i < 6; i++) {
      nick = generateNick(i >= 2)
      if (await checkNick(nick)) return
    }
  }
  async function confirmNick() {
    if (nickBusy || !(await checkNick(nick.trim()))) return
    nickBusy = true
    try { await onConfirmNick?.(nick.trim()); adding = false }
    catch (e: any) { nickSt = 'bad'; nickMsg = String(e?.message ?? e) }
    finally { nickBusy = false }
  }
  let nickOpenedFor = ''
  $effect(() => {
    const key = nickAsk ? nickAsk.provider : ''
    if (key && key !== nickOpenedFor) { nickOpenedFor = key; untrack(() => { nick = ''; rollNick() }) }
    if (!key) nickOpenedFor = ''
  })

  // ── Скін ──
  let skinSel = $state('steve')
  let slim = $state(false)
  let custom = $state('') // dataURL власного PNG або скопійованого
  let customName = $state('')
  let byNick = $state('')
  let byNickBusy = $state(false)
  let applying = $state(false)
  let applied = $state('')
  let fileEl: HTMLInputElement | undefined = $state()

  const curTexture = $derived(skinSel === 'custom' ? custom : (SKINS.find(s => s.id === skinSel)?.url ?? steve))
  $effect(() => {
    const show = showSkin && tab === 'skin' && !nickAsk && !waiting && !adding
    preview = show ? { texture: curTexture, slim } : null
  })

  function pickSkin(id: string) {
    skinSel = id
    const s = SKINS.find(x => x.id === id)
    if (s) slim = s.slim
  }

  function readFile(f: File): Promise<string> {
    return new Promise((res, rej) => { const r = new FileReader(); r.onload = () => res(String(r.result)); r.onerror = rej; r.readAsDataURL(f) })
  }
  function imgSize(url: string): Promise<{ w: number; h: number }> {
    return new Promise((res, rej) => { const i = new Image(); i.onload = () => res({ w: i.naturalWidth, h: i.naturalHeight }); i.onerror = rej; i.src = url })
  }
  async function onFile(e: Event) {
    const f = (e.target as HTMLInputElement).files?.[0]
    if (!f) return
    try {
      const url = await readFile(f)
      const { w, h } = await imgSize(url)
      if (w !== 64 || (h !== 64 && h !== 32)) { toast(t('wz.skin.badSize'), 'warn'); return }
      custom = url; customName = f.name.replace(/\.png$/i, '').slice(0, 32); skinSel = 'custom'
    } catch { toast(t('wz.skin.badSize'), 'warn') }
    finally { if (fileEl) fileEl.value = '' }
  }
  async function copyByNick() {
    const n = byNick.trim()
    if (!n || byNickBusy) return
    byNickBusy = true
    try {
      const p = await App.LookupWardrobePlayer(n)
      if (!p?.skinUrl) throw new Error(t('wz.skin.notFound'))
      custom = await App.GetWardrobeRemoteSkinDataURL(p.skinUrl)
      customName = p.nickname || n
      slim = !!p.slimArms
      skinSel = 'custom'
    } catch (e: any) { toast(String(e?.message ?? e) || t('wz.skin.notFound'), 'warn') }
    finally { byNickBusy = false }
  }
  async function toBase64(url: string): Promise<string> {
    if (url.startsWith('data:')) return url.split(',')[1] ?? ''
    const blob = await (await fetch(url)).blob()
    const d = await new Promise<string>((res, rej) => { const r = new FileReader(); r.onload = () => res(String(r.result)); r.onerror = rej; r.readAsDataURL(blob) })
    return d.split(',')[1] ?? ''
  }
  async function applySkin() {
    if (!acc || applying) return
    applying = true
    try {
      const b64 = await toBase64(curTexture)
      const name = skinSel === 'custom' ? (customName || t('wz.skin.mine')) : skinSel
      const preset = await App.SaveWardrobeLocalSkin(acc.id, name, b64, slim)
      await App.ApplyWardrobePreset(acc.id, preset.id)
      applied = skinSel + (slim ? ':s' : '')
      toast(t('wz.skin.done'), 'success')
    } catch (e: any) { toast(t('error.withMessage', { message: e }), 'error') }
    finally { applying = false }
  }

  // ── Профіль ──
  let handle = $state('')
  let bio = $state('')
  let color = $state<string>(PROFILE_COLORS[0])
  let savingProfile = $state(false)
  let profileSaved = $state(false)
  const handleNorm = $derived(normalizeHandle(handle))
  const handleBad = $derived(handle.trim() !== '' && !handleNorm)
  const bioNorm = $derived(normalizeBio(bio))
  async function saveProfile() {
    if (savingProfile || handleBad || !bioNorm.ok) return
    savingProfile = true
    try {
      if (handleNorm && handleNorm !== (profile?.handle ?? '')) await App.SetShaurmaHandle(handleNorm)
      if (bioNorm.text !== (profile?.bio ?? '') || color !== (profile?.color ?? '')) await App.SetShaurmaProfileStyle(bioNorm.text, color)
      profileSaved = true
      toast(t('wz.prof.saved'), 'success')
      await loadProfile()
    } catch (e: any) { toast(t('error.withMessage', { message: e }), 'error') }
    finally { savingProfile = false }
  }
  $effect(() => { void handle; void bio; void color; profileSaved = false })

  function another() { adding = true; showOffline = false }
</script>

{#if nickAsk}
  <!-- ═ Нік нового акаунта Шаурми ═ -->
  <div class="blk" in:fly={{ y: 10, duration: 300 }}>
    <h1>{t('wz.nick.title')}</h1>
    <p class="sub">{nickAsk.noLicense ? t('wz.nick.subNoLicense') : t('wz.nick.sub')}</p>
    <div class="nick-big" class:ok={nickSt === 'ok'} class:bad={nickSt === 'bad'}>
      <PlayerHead skin="steve" size={40} radius={9} />
      <input value={nick} maxlength="16" placeholder={t('nickname')} oninput={(e: any) => nickInput(e.target.value)}
             onkeydown={(e: any) => e.key === 'Enter' && confirmNick()} aria-label={t('nickname')} />
      <span class="nick-st">
        {#if nickSt === 'checking'}<span class="mini-spin"></span>
        {:else if nickSt === 'ok'}<i class="ti ti-circle-check-filled" style="color:var(--green)"></i>
        {:else if nickSt === 'bad'}<i class="ti ti-alert-circle-filled" style="color:var(--red)"></i>{/if}
      </span>
      <button class="dice" onclick={rollNick} use:launcherTip={t('wz.nick.roll')} aria-label={t('wz.nick.roll')}><i class="ti ti-dice-5"></i></button>
    </div>
    <div class="nick-msg">{#if nickMsg}<span class="err">{nickMsg}</span>{:else}{t('wz.nick.rules')}{/if}</div>
    <div class="row-btns">
      <button class="btn btn-primary" disabled={nickSt !== 'ok' || nickBusy} onclick={confirmNick}>
        {#if nickBusy}<span class="mini-spin light"></span>{:else}<i class="ti ti-check"></i>{/if} {t('wz.nick.take')}
      </button>
      <button class="btn btn-ghost" onclick={() => onCancelNick?.()}>{t('cancel')}</button>
    </div>
  </div>

{:else if waiting}
  <div class="blk">
    <h1>{t('wz.acc.title')}</h1>
    <div class="waiting" in:fade>
      <span class="spin"></span>
      <div><b>{t('wz.acc.waiting')}</b><small>{msStep === 'waiting' ? 'Microsoft' : 'Google'}</small></div>
      <button class="btn btn-ghost btn-sm" onclick={() => (msStep === 'waiting' ? onMsCancel?.() : onGoogleCancel?.())}>{t('cancel')}</button>
    </div>
  </div>

{:else if !acc || adding}
  <!-- ═ Вибір способу входу ═ -->
  <div class="blk">
    <h1>{t('wz.acc.title')}</h1>
    <div class="choices" class:one={!skingate}>
      <div class="choice lic" style="--i:0">
        <span class="ch-badge lic">{t('wz.acc.licBadge')}</span>
        <div class="ch-top">
          <span class="ch-ico ms"><svg width="20" height="20" viewBox="0 0 21 21" aria-hidden="true"><rect x="1" y="1" width="9" height="9" fill="#f25022"/><rect x="11" y="1" width="9" height="9" fill="#7fba00"/><rect x="1" y="11" width="9" height="9" fill="#00a4ef"/><rect x="11" y="11" width="9" height="9" fill="#ffb900"/></svg></span>
          <b>{t('wz.acc.licTitle')}</b>
        </div>
        <p>{t('wz.acc.licText')}</p>
        <ul class="ch-list">
          <li><i class="ti ti-rosette-discount-check"></i>{t('wz.acc.p.licNick')}</li>
          <li><i class="ti ti-users"></i>{t('wz.acc.p.friends')}</li>
          <li><i class="ti ti-world-share"></i>{t('wz.acc.p.p2p')}</li>
        </ul>
        <button class="btn btn-primary ch-btn" onclick={() => onMsLogin?.()}>{t('wz.acc.licBtn')} <i class="ti ti-arrow-right"></i></button>
      </div>
      {#if skingate}
        <div class="choice rec" style="--i:1">
          <span class="ch-badge">{t('wz.acc.recBadge')}</span>
          <div class="ch-top">
            <span class="ch-ico"><svg width="20" height="20" viewBox="0 0 48 48" aria-hidden="true"><path fill="#EA4335" d="M24 9.5c3.5 0 6.6 1.2 9.1 3.6l6.8-6.8C35.8 2.4 30.3 0 24 0 14.6 0 6.5 5.4 2.6 13.2l7.9 6.1C12.4 13.6 17.7 9.5 24 9.5z"/><path fill="#4285F4" d="M46.5 24.5c0-1.6-.1-3.1-.4-4.5H24v9h12.7c-.6 3-2.3 5.5-4.8 7.2l7.6 5.9c4.4-4.1 7-10.1 7-17.6z"/><path fill="#FBBC05" d="M10.5 28.7c-.5-1.5-.8-3.1-.8-4.7s.3-3.2.8-4.7l-7.9-6.1C.9 16.4 0 20.1 0 24s.9 7.6 2.6 10.8l7.9-6.1z"/><path fill="#34A853" d="M24 48c6.5 0 11.9-2.1 15.9-5.8l-7.6-5.9c-2.1 1.4-4.9 2.3-8.3 2.3-6.3 0-11.6-4.1-13.5-9.8l-7.9 6.1C6.5 42.6 14.6 48 24 48z"/></svg></span>
            <b>{t('wz.acc.noLicTitle')}</b>
          </div>
          <p>{t('wz.acc.noLicText')}</p>
          <ul class="ch-list">
            <li><i class="ti ti-shirt"></i>{t('wz.acc.p.skin')}</li>
            <li><i class="ti ti-users"></i>{t('wz.acc.p.friends')}</li>
            <li><i class="ti ti-world-share"></i>{t('wz.acc.p.p2p')}</li>
          </ul>
          <button class="btn btn-primary ch-btn" onclick={() => onGoogleLogin?.()}>{t('wz.acc.noLicBtn')} <i class="ti ti-arrow-right"></i></button>
        </div>
      {/if}
    </div>
    <button class="link-btn muted" onclick={() => (showOffline = !showOffline)}>
      <i class="ti ti-chevron-right rot" class:open={showOffline}></i> {t('wz.acc.offlineLink')}
    </button>
    {#if showOffline}
      <div class="offline" transition:slide={{ duration: 220 }}>
        <div class="nick-row">
          <input class="input {pirateErr && pirateTouched ? 'input-err' : ''}" placeholder={t('nickname')} value={pirateNick} maxlength="16"
                 oninput={(e: any) => { pirateNick = sanitizeNick(e.target.value); pirateTouched = true }}
                 onkeydown={(e: any) => e.key === 'Enter' && pirate()} aria-invalid={!!(pirateErr && pirateTouched)} />
          <button class="btn btn-ghost" disabled={!!pirateErr || !pirateNick.trim() || pirateBusy} onclick={pirate}>{t('login')}</button>
        </div>
        {#if pirateErr && pirateTouched}
          <div class="field-err"><i class="ti ti-alert-triangle"></i> {pirateErr}</div>
        {:else}
          <div class="note warn"><i class="ti ti-alert-triangle"></i> {t('wz.acc.offlineNote')}</div>
        {/if}
      </div>
    {/if}
    {#if adding}
      <button class="link-btn" onclick={() => (adding = false)}><i class="ti ti-arrow-left"></i> {t('wiz.back')}</button>
    {/if}
  </div>

{:else}
  <!-- ═ Акаунт є ═ -->
  <div class="blk">
    <div class="acc-card">
      <PlayerHead src={accHead} size={46} radius={11} />
      <div class="ac-t">
        <b>{acc.username}</b>
        <small>
          {#if licensed}<i class="ti ti-rosette-discount-check"></i> {t('wz.acc.typeLic')}
          {:else if hasShaurma}<i class="ti ti-flame"></i> {t('wz.acc.typeShaurma')}
          {:else}<i class="ti ti-user"></i> {t('wz.acc.typeOffline')}{/if}
        </small>
      </div>
      <button class="link-btn" onclick={another}>{t('wz.acc.other')}</button>
    </div>

    {#if skingate && !licensed && !hasShaurma}
      <!-- Офлайн-нік без Шаурми: м'яко ведемо до Google -->
      <div class="nudge" in:fly={{ y: 8, duration: 300, delay: 150 }}>
        <div class="nudge-heads"><PlayerHead skin="alex" size={34} /><PlayerHead skin="kai" size={34} /><PlayerHead skin="zuri" size={34} /></div>
        <div class="nudge-t"><b>{t('wz.nudge.title')}</b><small>{t('wz.nudge.text')}</small></div>
        <button class="btn btn-primary btn-sm" onclick={() => onGoogleLogin?.()}>Google</button>
      </div>
    {:else if skingate && hasShaurma}
      {#if showSkin}
        <div class="tabs" style="--i:{tab === 'skin' ? 0 : 1}">
          <span class="tab-pill"></span>
          <button class:sel={tab === 'skin'} onclick={() => (tab = 'skin')}><i class="ti ti-shirt"></i> {t('wz.tab.skin')}</button>
          <button class:sel={tab === 'profile'} onclick={() => (tab = 'profile')}><i class="ti ti-id-badge-2"></i> {t('wz.tab.profile')}</button>
        </div>
      {:else}
        <p class="sub">{t('wz.prof.licSub')}</p>
      {/if}

      {#if tab === 'skin' && showSkin}
        <div class="skin" in:fly={{ x: -14, duration: 260 }}>
          <div class="skin-grid">
            {#each SKINS as s, i}
              <button class="skin-it" class:sel={skinSel === s.id} style="--i:{i}" onclick={() => pickSkin(s.id)} use:launcherTip={s.id} aria-label={s.id}>
                <PlayerHead skin={s.id} size={38} radius={8} />
              </button>
            {/each}
            {#if custom}
              <button class="skin-it" class:sel={skinSel === 'custom'} onclick={() => (skinSel = 'custom')} use:launcherTip={customName} aria-label={customName}>
                <span class="cust-face" style="background-image:url({custom})"></span>
              </button>
            {/if}
            <button class="skin-it up" onclick={() => fileEl?.click()} use:launcherTip={t('wz.skin.upload')} aria-label={t('wz.skin.upload')}><i class="ti ti-upload"></i></button>
            <input type="file" accept="image/png" hidden bind:this={fileEl} onchange={onFile} />
          </div>
          <div class="nick-row">
            <input class="input" placeholder={t('wz.skin.byNick')} bind:value={byNick} maxlength="16" onkeydown={(e: any) => e.key === 'Enter' && copyByNick()} />
            <button class="btn btn-ghost" disabled={!byNick.trim() || byNickBusy} onclick={copyByNick}>
              {#if byNickBusy}<span class="mini-spin"></span>{:else}<i class="ti ti-copy"></i>{/if} {t('wz.skin.copy')}
            </button>
          </div>
          <div class="skin-foot">
            <label class="chk"><input type="checkbox" bind:checked={slim} /> <span>{t('wz.skin.slim')}</span></label>
            <button class="btn btn-primary" disabled={applying || applied === skinSel + (slim ? ':s' : '')} onclick={applySkin}>
              {#if applying}<span class="mini-spin light"></span>{:else if applied === skinSel + (slim ? ':s' : '')}<i class="ti ti-check"></i>{:else}<i class="ti ti-shirt"></i>{/if}
              {applied === skinSel + (slim ? ':s' : '') ? t('wz.skin.on') : t('wz.skin.apply')}
            </button>
          </div>
          <div class="note"><i class="ti ti-rotate-360"></i> {t('wz.skin.hint')}</div>
        </div>
      {:else}
        <div class="prof" in:fly={{ x: 14, duration: 260 }}>
          <div class="pcard" style="--pc:{color}">
            <div class="pc-cover"></div>
            <div class="pc-body">
              <PlayerHead src={accHead} size={52} radius={12} />
              <div class="pc-t">
                <b>{profile?.nickname || acc.username}</b>
                <small>@{handleNorm || profile?.handle || 'handle'}</small>
              </div>
            </div>
            {#if bioNorm.text}<p class="pc-bio">{bioNorm.text}</p>{/if}
          </div>
          <div class="label">{t('wz.prof.handle')}</div>
          <div class="handle-in" class:bad={handleBad}>
            <span>@</span><input bind:value={handle} maxlength="32" placeholder="nickname" aria-label={t('wz.prof.handle')} />
          </div>
          {#if handleBad}<div class="field-err"><i class="ti ti-alert-triangle"></i> {t('wz.prof.handleBad')}</div>{/if}
          <div class="label">{t('wz.prof.bio')} <span class="cnt" class:over={!bioNorm.ok}>{bioLength(bio)}/{BIO_MAX}</span></div>
          <textarea class="input bio" rows="2" bind:value={bio} placeholder={t('wz.prof.bioPh')}></textarea>
          <div class="label">{t('wz.prof.color')}</div>
          <div class="pcolors">
            {#each PROFILE_COLORS as c, i}
              <button class="pcol" class:sel={color === c} style="background:{c};--i:{i}" onclick={() => (color = c)} aria-label={c}><i class="ti ti-check"></i></button>
            {/each}
          </div>
          <button class="btn btn-primary save" disabled={savingProfile || handleBad || !bioNorm.ok || profileSaved} onclick={saveProfile}>
            {#if savingProfile}<span class="mini-spin light"></span>{:else}<i class="ti ti-check"></i>{/if}
            {profileSaved ? t('wz.prof.savedBtn') : t('wz.prof.save')}
          </button>
        </div>
      {/if}
    {:else if licensed}
      <p class="sub">{t('wz.acc.licDone')}</p>
    {/if}
  </div>
{/if}

<style>
  .blk > * { animation: rise .5s cubic-bezier(.22, 1, .36, 1) backwards; }
  .blk > :nth-child(2) { animation-delay: .06s } .blk > :nth-child(3) { animation-delay: .12s }
  .blk > :nth-child(4) { animation-delay: .18s } .blk > :nth-child(n+5) { animation-delay: .24s }
  @keyframes rise { from { opacity: 0; transform: translateY(12px) } }
  @keyframes pop { from { opacity: 0; transform: scale(.8) } }
  h1 { font-size: 28px; font-weight: 800; line-height: 1.15; letter-spacing: -.3px; margin: 0 0 16px; }
  .sub { font-size: 13.5px; color: var(--text-mute); line-height: 1.55; margin: -8px 0 16px; max-width: 520px; }
  .blk > .sub:not(:first-child) { margin-top: 14px; }
  .label { font-size: 11.5px; font-weight: 700; color: var(--text-mute); margin: 14px 0 7px; display: flex; justify-content: space-between; }
  .note { display: flex; gap: 7px; align-items: flex-start; font-size: 11.5px; color: var(--text-mute); margin-top: 8px; line-height: 1.45; }
  .note i { font-size: 14px; flex-shrink: 0; margin-top: 1px; }
  .note.warn i { color: var(--yellow, #eab308); }
  .link-btn { border: none; background: none; color: var(--orange); font: inherit; font-size: 12.5px; font-weight: 700; cursor: pointer; padding: 6px 0; display: inline-flex; align-items: center; gap: 5px; margin-top: 10px; }
  .link-btn.muted { color: var(--text-mute); }
  .link-btn:hover { color: var(--text); }
  .rot { transition: transform .25s cubic-bezier(.34, 1.56, .64, 1); }
  .rot.open { transform: rotate(90deg); }
  .nick-row { display: flex; gap: 8px; }
  .nick-row .input { flex: 1; min-width: 0; }
  .offline { max-width: 460px; margin-top: 4px; }
  .mini-spin { display: inline-block; width: 14px; height: 14px; border-radius: 50%; border: 2px solid rgba(var(--orange-rgb), .3); border-top-color: var(--orange); animation: sp .7s linear infinite; vertical-align: middle; }
  .mini-spin.light { border-color: rgba(255, 255, 255, .35); border-top-color: #fff; }
  @keyframes sp { to { transform: rotate(360deg) } }

  /* ── вибір ── */
  .choices { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
  .choices.one { grid-template-columns: minmax(0, 360px); }
  .choice {
    position: relative; display: flex; flex-direction: column; gap: 8px; padding: 16px; border-radius: 16px;
    background: var(--bg-card); border: 1.5px solid var(--border);
    transition: transform .2s cubic-bezier(.34, 1.56, .64, 1), border-color .2s, box-shadow .2s;
    animation: pop .5s cubic-bezier(.34, 1.56, .64, 1) backwards; animation-delay: calc(.2s + var(--i) * .1s);
  }
  .choice:hover { transform: translateY(-3px); border-color: var(--border-hi); box-shadow: 0 14px 30px rgba(0, 0, 0, .25); }
  .choice.lic { border-color: rgba(91, 141, 239, .45); background: linear-gradient(180deg, rgba(91, 141, 239, .09), transparent 70%), var(--bg-card); }
  .choice.rec { border-color: rgba(var(--orange-rgb), .55); background: linear-gradient(180deg, rgba(var(--orange-rgb), .1), transparent 70%), var(--bg-card); }
  .ch-badge { position: absolute; top: -10px; right: 14px; padding: 2px 9px; border-radius: 999px; font-size: 10.5px; font-weight: 800; color: #fff; background: var(--orange); box-shadow: 0 4px 12px rgba(var(--orange-rgb), .45); }
  .ch-badge.lic { background: #3b6fd9; box-shadow: 0 4px 12px rgba(59, 111, 217, .45); }
  .ch-top { display: flex; align-items: center; gap: 10px; }
  .ch-top b { font-size: 14.5px; }
  .ch-ico { width: 36px; height: 36px; border-radius: 10px; display: grid; place-items: center; background: var(--hover-strong); flex-shrink: 0; }
  .choice p { margin: 0; font-size: 12.5px; color: var(--text-mute); line-height: 1.5; }
  .ch-list { list-style: none; margin: 2px 0 0; padding: 0; display: flex; flex-direction: column; gap: 5px; }
  .ch-list li { display: flex; align-items: center; gap: 8px; font-size: 12px; font-weight: 600; }
  .ch-list i { color: var(--orange); font-size: 15px; }
  .ch-btn { margin-top: auto; justify-content: center; }
  .ch-btn i { transition: transform .2s; }
  .ch-btn:hover i { transform: translateX(3px); }

  .waiting { display: flex; align-items: center; gap: 12px; padding: 14px 16px; border-radius: 14px; max-width: 460px; background: rgba(var(--orange-rgb), .08); border: 1px solid rgba(var(--orange-rgb), .35); }
  .waiting div { flex: 1; display: flex; flex-direction: column; }
  .waiting b { font-size: 13px; } .waiting small { font-size: 11.5px; color: var(--text-mute); }
  .spin { width: 22px; height: 22px; border-radius: 50%; border: 3px solid rgba(var(--orange-rgb), .25); border-top-color: var(--orange); animation: sp .8s linear infinite; }

  /* ── нік ── */
  .nick-big {
    display: flex; align-items: center; gap: 12px; padding: 10px 10px 10px 12px; border-radius: 16px; max-width: 460px;
    background: var(--bg-card); border: 1.5px solid var(--border-hi); transition: border-color .2s, box-shadow .2s;
  }
  .nick-big:focus-within { border-color: var(--orange); box-shadow: 0 0 0 4px rgba(var(--orange-rgb), .15); }
  .nick-big.ok { border-color: var(--green); }
  .nick-big.bad { border-color: var(--red); }
  .nick-big input { flex: 1; min-width: 0; border: none; background: none; outline: none; color: var(--text); font: inherit; font-size: 20px; font-weight: 800; font-family: var(--font-mono); }
  .nick-st { width: 20px; display: grid; place-items: center; font-size: 18px; }
  .dice { width: 40px; height: 40px; border-radius: 11px; border: 1px solid var(--border-hi); background: var(--hover); color: var(--text); font-size: 20px; cursor: pointer; transition: transform .3s cubic-bezier(.34, 1.56, .64, 1), background .15s; }
  .dice:hover { background: var(--hover-strong); transform: rotate(-20deg) scale(1.06); }
  .dice:active { transform: rotate(160deg) scale(.95); }
  .nick-msg { font-size: 11.5px; color: var(--text-mute); margin: 8px 2px 14px; min-height: 16px; }
  .nick-msg .err { color: var(--red); }
  .row-btns { display: flex; gap: 8px; }

  /* ── акаунт ── */
  .acc-card { display: flex; align-items: center; gap: 12px; padding: 12px 14px; border-radius: 16px; background: var(--bg-card); border: 1px solid var(--border-hi); }
  .acc-card .link-btn { margin: 0 0 0 auto; }
  .ac-t { display: flex; flex-direction: column; min-width: 0; }
  .ac-t b { font-size: 15px; }
  .ac-t small { font-size: 11.5px; color: var(--text-mute); display: inline-flex; align-items: center; gap: 4px; }
  .ac-t small i { color: var(--orange); font-size: 13px; }
  .nudge {
    display: flex; align-items: center; gap: 14px; margin-top: 12px; padding: 14px; border-radius: 16px;
    background: linear-gradient(120deg, rgba(var(--orange-rgb), .16), rgba(var(--orange-rgb), .04)); border: 1px solid rgba(var(--orange-rgb), .4);
  }
  .nudge-heads { display: flex; }
  .nudge-heads :global(.ph + .ph) { margin-left: -10px; }
  .nudge-t { flex: 1; display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .nudge-t b { font-size: 13px; } .nudge-t small { font-size: 11.5px; color: var(--text-mute); line-height: 1.4; }

  .tabs { position: relative; display: grid; grid-template-columns: 1fr 1fr; padding: 4px; margin: 14px 0 4px; border-radius: 13px; background: var(--bg-card); border: 1px solid var(--border); max-width: 320px; }
  .tab-pill { position: absolute; top: 4px; bottom: 4px; left: 4px; width: calc(50% - 4px); border-radius: 10px; background: var(--orange); transform: translateX(calc(100% * var(--i))); transition: transform .4s cubic-bezier(.34, 1.3, .64, 1); box-shadow: 0 6px 16px rgba(var(--orange-rgb), .35); }
  .tabs button { position: relative; z-index: 1; border: none; background: none; color: var(--text-mute); font: inherit; font-size: 12.5px; font-weight: 700; padding: 8px; cursor: pointer; display: flex; align-items: center; justify-content: center; gap: 6px; transition: color .25s; }
  .tabs button.sel { color: #fff; }

  /* ── скін ── */
  .skin { margin-top: 10px; }
  .skin-grid { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 10px; }
  .skin-it {
    width: 54px; height: 54px; border-radius: 13px; border: 2px solid var(--border); background: var(--bg-card); cursor: pointer;
    display: grid; place-items: center; color: var(--text-mute); font-size: 20px;
    transition: transform .2s cubic-bezier(.34, 1.56, .64, 1), border-color .2s, box-shadow .2s;
    animation: pop .4s cubic-bezier(.34, 1.56, .64, 1) backwards; animation-delay: calc(.1s + var(--i, 9) * .035s);
  }
  .skin-it:hover { transform: translateY(-3px); border-color: var(--border-hi); }
  .skin-it.sel { border-color: var(--orange); box-shadow: 0 0 0 4px rgba(var(--orange-rgb), .18); transform: translateY(-2px); }
  .skin-it.up { border-style: dashed; }
  .skin-it.up:hover { color: var(--orange); }
  .cust-face { width: 38px; height: 38px; border-radius: 8px; image-rendering: pixelated; background-size: 304px 304px; background-position: -38px -38px; }
  .skin-foot { display: flex; align-items: center; justify-content: space-between; gap: 10px; margin-top: 12px; }
  .chk { display: inline-flex; align-items: center; gap: 7px; font-size: 12.5px; font-weight: 600; cursor: pointer; }
  .chk input { accent-color: var(--orange); width: 16px; height: 16px; }

  /* ── профіль ── */
  .prof { margin-top: 10px; max-width: 460px; }
  .pcard { border-radius: 16px; overflow: hidden; background: color-mix(in srgb, var(--pc) 8%, var(--bg-card)); border: 1px solid color-mix(in srgb, var(--pc) 40%, var(--border)); transition: background .3s, border-color .3s; }
  .pc-cover { height: 46px; background: linear-gradient(120deg, var(--pc), color-mix(in srgb, var(--pc) 45%, #000)); transition: background .3s; }
  .pc-body { display: flex; align-items: flex-end; gap: 12px; padding: 0 14px; margin-top: -22px; }
  .pc-body :global(.ph) { box-shadow: 0 0 0 4px var(--bg-card), 0 8px 20px rgba(0, 0, 0, .35); }
  .pc-t { display: flex; flex-direction: column; padding-bottom: 2px; min-width: 0; }
  .pc-t b { font-size: 15px; } .pc-t small { font-size: 12px; color: var(--text-mute); font-family: var(--font-mono); }
  .pcard > :last-child { margin-bottom: 12px; }
  .pc-bio { margin: 10px 14px 0; font-size: 12.5px; line-height: 1.45; color: var(--text); overflow-wrap: anywhere; }
  .handle-in { display: flex; align-items: center; border-radius: 10px; border: 1px solid var(--border-hi); background: var(--bg-input, var(--bg-card)); padding: 0 10px; }
  .handle-in.bad { border-color: var(--red); }
  .handle-in:focus-within { border-color: var(--orange); }
  .handle-in span { color: var(--text-dim); font-weight: 700; }
  .handle-in input { flex: 1; border: none; outline: none; background: none; color: var(--text); font: inherit; font-family: var(--font-mono); padding: 9px 4px; }
  .bio { width: 100%; resize: none; font: inherit; }
  .cnt { font-weight: 600; color: var(--text-dim); font-family: var(--font-mono); }
  .cnt.over { color: var(--red); }
  .pcolors { display: flex; flex-wrap: wrap; gap: 7px; }
  .pcol { width: 28px; height: 28px; border-radius: 9px; border: none; cursor: pointer; color: #fff; display: grid; place-items: center; transition: transform .2s cubic-bezier(.34, 1.56, .64, 1), box-shadow .2s; animation: pop .35s cubic-bezier(.34, 1.56, .64, 1) backwards; animation-delay: calc(.2s + var(--i) * .03s); }
  .pcol i { font-size: 15px; transform: scale(0); transition: transform .2s cubic-bezier(.34, 1.56, .64, 1); }
  .pcol:hover { transform: scale(1.12); }
  .pcol.sel { box-shadow: 0 0 0 2px var(--bg-window), 0 0 0 4px var(--text); }
  .pcol.sel i { transform: scale(1); }
  .save { margin-top: 14px; }

  @media (max-width: 1000px) { .choices { grid-template-columns: 1fr; } }
</style>
