<script lang="ts">
  // ══════════════════════════════════════════════════════════════════════
  // Сторінка «Акаунт» у налаштуваннях (ЛИШЕ Shaurma-редакція).
  //
  // Кожен акаунт лаунчера має СВІЙ акаунт Шаурми (окремі друзі, чат, P2P,
  // скін). Тут — профіль і статистика: тип (ліцензійний/неліцензійний),
  // UUID, ID, нік, дата створення, ігровий час, кількість збірок і друзів.
  //
  // СКІНА ТУТ НЕМАЄ НАВМИСНО: скіном керує гардероб (Wardrobe) — окрема
  // сторінка вибору й застосування скінів. Тут — лише акаунт.
  // Бекенд: shaurma_account_shaurma.go → VPS. Worker акаунтів не веде.
  // ══════════════════════════════════════════════════════════════════════
  import { untrack } from 'svelte'
  import { App } from './wails'
  import { toast, confirmDialog } from './toast.svelte'
  import { pirateNicknameError } from './validation'
  import type { ShaurmaProfile } from './types'
  import DevicesPanel from './DevicesPanel.svelte'
  import { normalizeHandle } from './friends/handle'
  import { buildFriendLink } from './friends/friendLink'
  import { BIO_MAX, PROFILE_COLORS, normalizeBio, bioLength, safeColor, hueOf } from './friends/profileStyle'

  let { accountName = '', head = '' }: { accountName?: string; head?: string } = $props()

  let profile = $state<ShaurmaProfile | null>(null)
  let loading = $state(true)
  let waiting = $state(false)
  let busy = $state('')
  let nickDraft = $state('')
  let newNick = $state('')
  let handleDraft = $state('')
  let copied = $state('') // 'id' | 'uuid' | 'handle' | 'link' — коротка підсвітка кнопки після копіювання

  // ── Візитка: біо + колір фону ─────────────────────────────────────────
  let bioDraft = $state('')
  let colorDraft = $state('')
  let styleOwner = '' // для якого акаунта заповнені чернетки: при зміні акаунта скидаємо їх без питань
  let styleCooldownUntil = $state(0)
  const bioNorm = $derived(normalizeBio(bioDraft))
  const bioCount = $derived(bioLength(bioDraft))
  const styleDirty = $derived(!!profile?.hasAccount &&
    (bioNorm.text !== (profile.bio ?? '') || safeColor(colorDraft) !== (profile.color ?? '')))
  const styleValid = $derived(bioNorm.ok && (colorDraft === '' || safeColor(colorDraft) !== ''))
  const styleCooling = $derived(styleCooldownUntil > Date.now())
  const previewHue = $derived(hueOf(profile?.accountId ?? ''))

  // Ліміт — у СИМВОЛАХ (як на сервері), а не в байтах/UTF-16: емодзі = 1 символ. Вставка довгого тексту
  // обрізається по межі символу, а не відхиляється; Enter заблоковано — біо однорядкове.
  function onBioInput(e: Event) {
    const el = e.currentTarget as HTMLTextAreaElement
    const chars = [...el.value.replace(/[\r\n]+/g, ' ')]
    bioDraft = chars.length > BIO_MAX ? chars.slice(0, BIO_MAX).join('') : chars.join('')
    if (el.value !== bioDraft) el.value = bioDraft
  }
  function revertStyle() { bioDraft = profile?.bio ?? ''; colorDraft = profile?.color ?? '' }

  async function saveStyle() {
    if (!styleDirty || !styleValid || busy === 'style' || styleCooling) return
    busy = 'style'
    try {
      await App.SetShaurmaProfileStyle(bioNorm.text, safeColor(colorDraft))
      styleCooldownUntil = Date.now() + 3000
      setTimeout(() => { styleCooldownUntil = 0 }, 3000)
      bioDraft = bioNorm.text
      busy = ''
      await load()
      toast('Профіль збережено', 'success')
    } catch (e: any) { toast(String(e), 'error') }
    finally { busy = '' }
  }

  // Скелет показуємо лише при ПЕРШОМУ завантаженні. Фонові оновлення (після збереження біо/кольору/ніка,
  // подія «акаунт змінився») тихо підміняють дані: інакше вся сторінка блимала скелетом і втрачала скрол.
  async function load() {
    if (!profile) loading = true
    try {
      const wasDirty = styleDirty
      profile = await App.GetShaurmaProfile()
      // Незбережені зміни не затираємо фоновим оновленням (подія «акаунт змінився»).
      if (!wasDirty || styleOwner !== (profile?.accountId ?? '')) {
        bioDraft = profile?.bio ?? ''
        colorDraft = profile?.color ?? ''
        styleOwner = profile?.accountId ?? ''
      }
      nickDraft = profile?.nickname ?? ''
      handleDraft = profile?.handle ?? ''
      newNick = (profile?.launcherNick ?? accountName ?? '').trim()
    } catch { profile = null }
    loading = false
  }
  // load() синхронно читає styleDirty (а отже bioDraft/colorDraft): без untrack ефект підписувався на них
  // і перезавантажував усю сторінку на КОЖНУ зміну кольору чи символ біо. Залежність — лише accountName.
  $effect(() => { void accountName; untrack(() => { void load() }) })
  $effect(() => {
    const h = () => { load() }
    window.addEventListener('shaurma-account:changed', h)
    return () => window.removeEventListener('shaurma-account:changed', h)
  })

  function notifyChanged() { window.dispatchEvent(new CustomEvent('skin-auth:changed')) }

  async function create(provider: 'google' | 'microsoft') {
    const nick = newNick.trim()
    const err = pirateNicknameError(nick)
    if (err) { toast(err, 'error'); return }
    waiting = true
    try {
      await App.CreateShaurmaAccount(nick, provider)
      await load()
      notifyChanged()
      toast('Акаунт Шаурми створено', 'success')
    } catch (e: any) { toast(String(e), 'error') }
    finally { waiting = false }
  }
  function cancelLogin() { App.CancelShaurmaLogin(); waiting = false }

  async function saveNickname() {
    const nick = nickDraft.trim()
    const err = pirateNicknameError(nick)
    if (err) { toast(err, 'error'); return }
    busy = 'nick'
    try {
      await App.SetShaurmaNickname(nick)
      await load()
      notifyChanged()
      toast('Нік збережено', 'success')
    } catch (e: any) { toast(String(e), 'error') }
    finally { busy = '' }
  }

  async function syncNickname() {
    busy = 'sync'
    try {
      const nick = await App.SyncShaurmaNickname()
      await load()
      notifyChanged()
      toast(nick ? `Нік оновлено з Mojang: ${nick}` : 'Нік оновлено з Mojang', 'success')
    } catch (e: any) { toast(String(e), 'error') }
    finally { busy = '' }
  }

  // Handle — публічний ідентифікатор для пошуку друзів. Формат перевіряє і сервер; тут — миттєва підказка.
  const handleNorm = $derived(normalizeHandle(handleDraft))
  const handleWait = $derived(!!profile?.handleNextChangeAt && profile.handleNextChangeAt > Date.now())
  async function saveHandle() {
    if (!handleNorm || handleNorm === profile?.handle || handleWait) return
    busy = 'handle'
    try {
      await App.SetShaurmaHandle(handleNorm)
      await load()
      notifyChanged()
      toast('Ідентифікатор збережено', 'success')
    } catch (e: any) { toast(String(e), 'error') }
    finally { busy = '' }
  }

  async function copyText(what: 'id' | 'uuid' | 'handle' | 'link', value?: string) {
    if (!value) return
    try {
      await navigator.clipboard.writeText(value)
      copied = what
      setTimeout(() => { if (copied === what) copied = '' }, 1400)
      toast(what === 'id' ? 'ID скопійовано' : what === 'handle' ? 'Ідентифікатор скопійовано' : what === 'link' ? 'Посилання на дружбу скопійовано' : 'UUID скопійовано', 'success')
    } catch { toast('Не вдалося скопіювати', 'error') }
  }

  // Посилання-запрошення в друзі: коротке (/friend/@handle), якщо handle є, інакше — з UUID.
  async function copyInviteLink() {
    if (!profile?.accountId) return
    const url = buildFriendLink(profile.accountId, profile.nickname ?? '', profile.handle ?? '')
    try { await App.CopyToClipboard(url) }
    catch {
      try { await navigator.clipboard.writeText(url) }
      catch { toast('Не вдалося скопіювати', 'error'); return }
    }
    copied = 'link'
    setTimeout(() => { if (copied === 'link') copied = '' }, 1400)
    toast('Посилання на дружбу скопійовано', 'success')
  }

  async function remove() {
    const proceed = await confirmDialog(
      'Видалити акаунт Шаурми?',
      'Акаунт, його друзі, чат і скін буде видалено на сервері та тут. Це незворотно: ID не відновити. Сам акаунт лаунчера й гра не зачіпаються.',
      { confirmLabel: 'Видалити', cancelLabel: 'Скасувати', danger: true },
    )
    if (!proceed) return
    busy = 'delete'
    try {
      await App.DeleteShaurmaAccount()
      await load()
      notifyChanged()
      toast('Акаунт Шаурми видалено', 'success')
    } catch (e: any) { toast(String(e), 'error') }
    finally { busy = '' }
  }

  function fmtDate(ms?: number): string {
    if (!ms) return '—'
    try { return new Date(ms).toLocaleDateString() } catch { return '—' }
  }
  function fmtDuration(secs: number): string {
    if (!secs || secs <= 0) return '0 хв'
    const h = Math.floor(secs / 3600)
    const m = Math.floor((secs % 3600) / 60)
    if (h <= 0) return `${m} хв`
    return m > 0 ? `${h} год ${m} хв` : `${h} год`
  }
</script>

<div class="acc">
  {#if loading}
    <div class="skel hero-skel"></div>
    <div class="stats"><div class="skel tile-skel"></div><div class="skel tile-skel"></div><div class="skel tile-skel"></div></div>
  {:else if profile?.reason === 'no_launcher_account'}
    <div class="empty">
      <div class="empty-ic warn"><i class="ti ti-user-exclamation"></i></div>
      <div class="empty-t">Немає акаунта лаунчера</div>
      <div class="empty-s">Спершу додайте акаунт у лаунчері — акаунт Шаурми прив'язується до нього.</div>
    </div>
  {:else if profile?.reason === 'no_hwid'}
    <div class="empty">
      <div class="empty-ic warn"><i class="ti ti-device-desktop-off"></i></div>
      <div class="empty-t">Комп'ютер не визначено</div>
      <div class="empty-s">Не вдалося визначити цей комп'ютер — акаунт Шаурми тут недоступний.</div>
    </div>
  {:else if !profile?.hasAccount}
    <!-- ── Створення акаунта ───────────────────────────────────────── -->
    <div class="hero create">
      <div class="hero-glow"></div>
      <div class="hero-row">
        <div class="avatar">
          {#if head}<img src={head} alt="" />{:else}<i class="ti ti-user"></i>{/if}
          <span class="avatar-dot off"><i class="ti ti-plus"></i></span>
        </div>
        <div class="hero-txt">
          <div class="hero-name">Створити акаунт Шаурми</div>
          <div class="hero-sub">для акаунта лаунчера «{profile?.launcherNick || accountName}»</div>
        </div>
      </div>

      <div class="perks">
        <div class="perk"><i class="ti ti-users"></i><span>Друзі</span></div>
        <div class="perk"><i class="ti ti-message-circle"></i><span>Чат</span></div>
        <div class="perk"><i class="ti ti-affiliate"></i><span>P2P-ігри</span></div>
        <div class="perk"><i class="ti ti-hanger"></i><span>Скіни</span></div>
      </div>

      <div class="field">
        <label class="field-l" for="acc-new-nick"><i class="ti ti-user"></i> Ваш нік</label>
        <input id="acc-new-nick" class="inp mono" bind:value={newNick} placeholder="Нік" maxlength="16" disabled={waiting} />
        <div class="field-h">Так вас бачать друзі й гра. Унікальний — ID, який ви отримаєте. Для ліцензійного акаунта нік і скін беруться з Mojang.</div>
      </div>

      {#if waiting}
        <div class="wait">
          <i class="ti ti-loader spin"></i>
          <span>Очікування входу в браузері… завершіть вхід.</span>
          <button class="btn btn-ghost btn-sm" onclick={cancelLogin}>Скасувати</button>
        </div>
      {:else}
        <div class="prov">
          <button class="prov-btn google" onclick={() => create('google')}>
            <span class="prov-ic"><i class="ti ti-brand-google"></i></span>
            <span class="prov-t"><b>Продовжити з Google</b><small>Швидко, без пароля</small></span>
            <i class="ti ti-arrow-right prov-go"></i>
          </button>
          <button class="prov-btn ms" onclick={() => create('microsoft')}>
            <span class="prov-ic"><i class="ti ti-brand-windows"></i></span>
            <span class="prov-t"><b>Продовжити з Microsoft</b><small>Для ліцензійних акаунтів</small></span>
            <i class="ti ti-arrow-right prov-go"></i>
          </button>
        </div>
        <div class="privacy"><i class="ti ti-shield-lock"></i>
          <span>Вхід лише підтверджує, що ви — людина. Пароль, пошта й доступ до вашого акаунта серверу не передаються.</span>
        </div>
      {/if}
    </div>
  {:else}
    {#if profile.canUpgrade}
      <div class="upgrade">
        <div class="upgrade-ic"><i class="ti ti-arrow-up-circle"></i></div>
        <div>
          <b>Доступне оновлення до ліцензійного типу</b>
          <span>У цього акаунта лаунчера з'явилася ліцензія. Щоб нік і скін бралися з Mojang, видаліть акаунт Шаурми нижче й створіть його знову, підтвердивши вхід Microsoft.</span>
        </div>
      </div>
    {/if}

    <!-- ── Профіль ─────────────────────────────────────────────────── -->
    <div class="hero" class:lic={profile.licensed}>
      <div class="hero-glow"></div>
      <div class="hero-row">
        <div class="avatar">
          {#if head}<img src={head} alt="" />{:else}<i class="ti ti-user"></i>{/if}
          <span class="avatar-dot" title="Акаунт Шаурми активний"><i class="ti ti-check"></i></span>
        </div>
        <div class="hero-txt">
          <div class="hero-name">{profile.nickname || '—'}</div>
          <div class="badges">
            {#if profile.licensed}
              <span class="badge green"><i class="ti ti-rosette-discount-check"></i> Ліцензійний · Mojang</span>
            {:else}
              <span class="badge orange"><i class="ti ti-user-off"></i> Неліцензійний</span>
            {/if}
            <span class="badge blue"><i class="ti ti-cloud-check"></i> Акаунт Шаурми</span>
          </div>
          <div class="hero-sub"><i class="ti ti-calendar-event"></i> З нами з {fmtDate(profile.createdAt)}</div>
        </div>
      </div>

      <div class="idbar">
        <div class="idbar-ic"><i class="ti {profile.handle ? 'ti-at' : 'ti-fingerprint'}"></i></div>
        <div class="idbar-body">
          {#if profile.handle}
            <small>Ідентифікатор · за ним вас додають у друзі</small>
            <input class="idbar-val" value={'@' + profile.handle} readonly onfocus={(e) => (e.currentTarget as HTMLInputElement).select()} />
          {:else}
            <small>ID акаунта · задайте короткий ідентифікатор нижче</small>
            <input class="idbar-val" value={profile.accountId} readonly onfocus={(e) => (e.currentTarget as HTMLInputElement).select()} />
          {/if}
        </div>
        {#if profile.handle}
          <button class="copy" class:ok={copied === 'handle'} onclick={() => copyText('handle', '@' + profile?.handle)} title="Копіювати ідентифікатор">
            <i class="ti {copied === 'handle' ? 'ti-check' : 'ti-copy'}"></i>
          </button>
        {:else}
          <button class="copy" class:ok={copied === 'id'} onclick={() => copyText('id', profile?.accountId)} title="Копіювати ID">
            <i class="ti {copied === 'id' ? 'ti-check' : 'ti-copy'}"></i>
          </button>
        {/if}
        <button class="copy" class:ok={copied === 'link'} onclick={copyInviteLink} title="Копіювати посилання на дружбу">
          <i class="ti {copied === 'link' ? 'ti-check' : 'ti-link'}"></i>
        </button>
      </div>
    </div>

    <!-- ── Статистика ──────────────────────────────────────────────── -->
    <div class="stats">
      <div class="tile t-orange">
        <div class="tile-ic"><i class="ti ti-clock-play"></i></div>
        <div class="tile-v">{fmtDuration(profile.gameSeconds)}</div>
        <div class="tile-l">Ігровий час</div>
      </div>
      <div class="tile t-violet">
        <div class="tile-ic"><i class="ti ti-packages"></i></div>
        <div class="tile-v">{profile.buildsCount}</div>
        <div class="tile-l">Збірок зіграно</div>
      </div>
      <div class="tile t-blue">
        <div class="tile-ic"><i class="ti ti-users"></i></div>
        <div class="tile-v">{profile.friends}</div>
        <div class="tile-l">Друзів</div>
      </div>
    </div>
    <div class="note"><i class="ti ti-info-circle"></i> Статистика лаунчера; загальний час — з історії запусків на цьому комп'ютері.</div>

    <!-- ── Дані акаунта ────────────────────────────────────────────── -->
    <div class="acard">
      <div class="acard-t"><i class="ti ti-id-badge-2"></i> Дані акаунта</div>
      <div class="list">
        <div class="li">
          <span class="li-ic"><i class="ti ti-shield-check"></i></span>
          <span class="li-k">Тип</span>
          <span class="li-v">{profile.licensed ? 'Ліцензійний (Mojang)' : 'Неліцензійний'}</span>
        </div>
        <div class="li">
          <span class="li-ic"><i class="ti ti-hash"></i></span>
          <span class="li-k">UUID</span>
          <span class="li-v mono">{profile.uuid || '—'}</span>
          {#if profile.uuid}
            <button class="copy sm" class:ok={copied === 'uuid'} onclick={() => copyText('uuid', profile?.uuid)} title="Копіювати UUID">
              <i class="ti {copied === 'uuid' ? 'ti-check' : 'ti-copy'}"></i>
            </button>
          {/if}
        </div>
        <div class="li">
          <span class="li-ic"><i class="ti ti-calendar-plus"></i></span>
          <span class="li-k">Створено</span>
          <span class="li-v">{fmtDate(profile.createdAt)}</span>
        </div>
        <div class="li">
          <span class="li-ic"><i class="ti ti-device-gamepad-2"></i></span>
          <span class="li-k">Акаунт лаунчера</span>
          <span class="li-v">{profile.launcherNick || accountName || '—'}</span>
        </div>
      </div>
    </div>

    <!-- ── Нік ─────────────────────────────────────────────────────── -->
    <div class="acard">
      <div class="acard-t"><i class="ti ti-signature"></i> Нік
        {#if !profile.nickLocked && typeof profile.nickChangesLeft === 'number'}
          <span class="nchip" class:zero={profile.nickChangesLeft <= 0}><i class="ti ti-refresh"></i> змін лишилось: {profile.nickChangesLeft}</span>
        {/if}
      </div>
      {#if profile.nickLocked}
        <div class="locked">
          <i class="ti ti-lock"></i>
          <span>Нік береться з Mojang і тут не редагується. Змініть його на сайті Mojang, а потім підтягніть сюди.</span>
        </div>
        <div>
          <button class="btn btn-ghost btn-sm" onclick={syncNickname} disabled={busy === 'sync'}>
            {#if busy === 'sync'}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-refresh"></i>{/if} Підтягнути нік з Mojang
          </button>
        </div>
      {:else}
        <div class="field-h">Його бачать друзі й гра. Змінювати можна не частіше двох разів на тиждень.</div>
        <div class="nickrow">
          <div class="inp-wrap">
            <i class="ti ti-user"></i>
            <input class="inp mono" bind:value={nickDraft} placeholder="Ваш нік" maxlength="16"
                   onkeydown={(e: KeyboardEvent) => { if (e.key === 'Enter') saveNickname() }} />
          </div>
          <button class="btn btn-primary btn-sm" onclick={saveNickname} disabled={busy === 'nick' || !nickDraft.trim() || nickDraft.trim() === profile.nickname}>
            {#if busy === 'nick'}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-check"></i>{/if} Зберегти
          </button>
        </div>
      {/if}
    </div>

    <!-- ── Мій ідентифікатор (@handle) ─────────────────────────────── -->
    <div class="acard">
      <div class="acard-t"><i class="ti ti-at"></i> Мій ідентифікатор
        {#if profile.handle}
          <button class="copy sm" class:ok={copied === 'handle'} onclick={() => copyText('handle', '@' + profile?.handle)} title="Копіювати">
            <i class="ti {copied === 'handle' ? 'ti-check' : 'ti-copy'}"></i>
          </button>
        {/if}
      </div>
      <div class="field-h">За ним друзі знаходять тебе без довгого ID: 3–32 символи, a–z, 0–9, «_» і крапка. Змінювати можна раз на 7 днів; старий звільняється не одразу (14 днів).</div>
      <div class="nickrow">
        <div class="inp-wrap">
          <i class="ti ti-at"></i>
          <input class="inp mono" bind:value={handleDraft} placeholder="bob_4821" maxlength="33" spellcheck="false" autocomplete="off"
                 onkeydown={(e: KeyboardEvent) => { if (e.key === 'Enter') saveHandle() }} />
        </div>
        <button class="btn btn-primary btn-sm" onclick={saveHandle}
                disabled={busy === 'handle' || !handleNorm || handleNorm === profile.handle || handleWait}>
          {#if busy === 'handle'}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-check"></i>{/if} Зберегти
        </button>
      </div>
      {#if handleDraft.trim() && !handleNorm}
        <div class="field-h" style="color: var(--red)">Невалідний формат: без «..», «__» та без «.» чи «_» на краях.</div>
      {:else if handleWait}
        <div class="field-h">Змінити знову можна {fmtDate(profile.handleNextChangeAt)}.</div>
      {/if}
    </div>

    <!-- ── Про себе: біо + колір фону профілю ───────────────────────── -->
    <div class="acard">
      <div class="acard-t"><i class="ti ti-palette"></i> Про себе та колір профілю
        {#if styleDirty}<span class="nchip"><i class="ti ti-pencil"></i> не збережено</span>{/if}
      </div>
      <div class="field-h">Це бачать лише ваші друзі — у вікні вашого профілю. До {BIO_MAX} символів, один рядок, лише текст (посилання не клікабельні).</div>

      <div class="bio-wrap" class:bad={!bioNorm.ok}>
        <textarea class="inp bio" rows="3" placeholder="Розкажіть про себе: що граєте, у які збірки…"
                  value={bioDraft} oninput={onBioInput} spellcheck="false" autocomplete="off"
                  aria-label="Про себе" aria-describedby="bio-count"
                  onkeydown={(e: KeyboardEvent) => { if (e.key === 'Enter') { e.preventDefault(); if (e.ctrlKey || e.metaKey) saveStyle() } }}></textarea>
        <span id="bio-count" class="bio-count" class:warn={bioCount >= BIO_MAX - 10} class:full={bioCount >= BIO_MAX}>{bioCount}/{BIO_MAX}</span>
      </div>
      {#if !bioNorm.ok}<div class="field-h" style="color: var(--red)">Занадто довге або містить недійсні символи.</div>{/if}

      <div class="field-l"><i class="ti ti-droplet"></i> Колір фону</div>
      <div class="swatches" role="group" aria-label="Колір фону профілю">
        <button class="sw sw-none" class:on={colorDraft === ''} onclick={() => (colorDraft = '')} title="Стандартний (за акаунтом)" aria-pressed={colorDraft === ''}>
          <i class="ti ti-x"></i>
        </button>
        {#each PROFILE_COLORS as c}
          <button class="sw" class:on={safeColor(colorDraft) === c} style="--c:{c}" onclick={() => (colorDraft = c)} title={c} aria-label={c} aria-pressed={safeColor(colorDraft) === c}></button>
        {/each}
        <label class="sw sw-custom" class:on={colorDraft !== '' && !(PROFILE_COLORS as readonly string[]).includes(safeColor(colorDraft))} title="Свій колір">
          <i class="ti ti-color-picker"></i>
          <input type="color" value={safeColor(colorDraft) || '#ff7a1a'} oninput={(e) => (colorDraft = safeColor((e.currentTarget as HTMLInputElement).value))} aria-label="Свій колір" />
        </label>
      </div>

      <div class="pv" style={safeColor(colorDraft) ? `--cv:${safeColor(colorDraft)}` : `--h:${previewHue}`} class:custom={!!safeColor(colorDraft)} aria-hidden="true">
        <div class="pv-cover"></div>
        <div class="pv-body">
          <div class="pv-ava">{#if head}<img src={head} alt="" />{:else}<i class="ti ti-user"></i>{/if}</div>
          <div class="pv-txt">
            <b>{profile.nickname || '—'}</b>
            <span>{bioNorm.ok && bioNorm.text ? bioNorm.text : 'Так друзі побачать ваше біо'}</span>
          </div>
        </div>
      </div>

      <div class="style-acts">
        <button class="btn btn-primary btn-sm" onclick={saveStyle} disabled={busy === 'style' || !styleDirty || !styleValid || styleCooling}>
          {#if busy === 'style'}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-check"></i>{/if} Зберегти
        </button>
        {#if styleDirty}
          <button class="btn btn-ghost btn-sm" onclick={revertStyle} disabled={busy === 'style'}><i class="ti ti-arrow-back-up"></i> Скасувати зміни</button>
        {/if}
      </div>
    </div>

    <!-- ── Пристрої: де відкрито акаунт (IP, клієнт, активність), вилучення, «Це не я» ── -->
    <DevicesPanel />

    <!-- ── Небезпечна зона ─────────────────────────────────────────── -->
    <div class="danger">
      <div class="danger-ic"><i class="ti ti-alert-triangle"></i></div>
      <div class="danger-txt">
        <b>Видалити акаунт Шаурми</b>
        <span>Друзі, чат і скін буде видалено на сервері. ID не відновити. Акаунт лаунчера й гра не зачіпаються.</span>
      </div>
      <button class="btn-danger" onclick={remove} disabled={busy === 'delete'}>
        {#if busy === 'delete'}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-trash"></i>{/if} Видалити
      </button>
    </div>
  {/if}
</div>

<style>
  .acc { display: flex; flex-direction: column; gap: 14px; animation: acc-in .28s ease both; }
  @keyframes acc-in { from { opacity: 0; transform: translateY(6px); } to { opacity: 1; transform: none; } }
  .mono { font-family: var(--font-mono); }

  /* ── Hero ── */
  .hero {
    position: relative; overflow: hidden; display: flex; flex-direction: column; gap: 16px;
    padding: 20px; border-radius: 18px;
    background: linear-gradient(160deg, color-mix(in srgb, var(--blue) 10%, var(--bg-card)), var(--bg-card) 62%);
    border: 1px solid color-mix(in srgb, var(--blue) 22%, var(--border));
  }
  .hero.lic { background: linear-gradient(160deg, color-mix(in srgb, var(--green) 11%, var(--bg-card)), var(--bg-card) 62%); border-color: color-mix(in srgb, var(--green) 24%, var(--border)); }
  .hero.create { background: linear-gradient(160deg, color-mix(in srgb, var(--orange) 11%, var(--bg-card)), var(--bg-card) 62%); border-color: color-mix(in srgb, var(--orange) 24%, var(--border)); }
  .hero-glow {
    position: absolute; right: -60px; top: -70px; width: 220px; height: 220px; border-radius: 50%; pointer-events: none;
    background: radial-gradient(circle, color-mix(in srgb, var(--blue) 28%, transparent), transparent 70%); opacity: .55;
  }
  .hero.lic .hero-glow { background: radial-gradient(circle, color-mix(in srgb, var(--green) 28%, transparent), transparent 70%); }
  .hero.create .hero-glow { background: radial-gradient(circle, color-mix(in srgb, var(--orange) 30%, transparent), transparent 70%); }
  .hero-row { position: relative; display: flex; align-items: center; gap: 16px; }
  .hero-txt { min-width: 0; display: flex; flex-direction: column; gap: 7px; }
  .hero-name { font: 800 22px/1.1 var(--font-ui); color: var(--text); letter-spacing: -.01em; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .hero-sub { display: flex; align-items: center; gap: 6px; font: 500 11.5px/1.3 var(--font-ui); color: var(--text-mute); }
  .hero-sub i { font-size: 14px; color: var(--text-dim); }

  .avatar {
    position: relative; flex-shrink: 0; width: 76px; height: 76px; border-radius: 20px;
    background: linear-gradient(135deg, #c07040, #8a4a26);
    display: flex; align-items: center; justify-content: center; font-size: 30px; color: #fff;
    box-shadow: 0 0 0 3px var(--bg-card), 0 0 0 5px color-mix(in srgb, var(--blue) 45%, transparent), 0 10px 24px rgba(0,0,0,.35);
  }
  .hero.lic .avatar { box-shadow: 0 0 0 3px var(--bg-card), 0 0 0 5px color-mix(in srgb, var(--green) 50%, transparent), 0 10px 24px rgba(0,0,0,.35); }
  .hero.create .avatar { box-shadow: 0 0 0 3px var(--bg-card), 0 0 0 5px color-mix(in srgb, var(--orange) 50%, transparent), 0 10px 24px rgba(0,0,0,.35); }
  .avatar img { width: 100%; height: 100%; object-fit: cover; border-radius: 20px; image-rendering: pixelated; display: block; }
  .avatar-dot {
    position: absolute; right: -6px; bottom: -6px; width: 24px; height: 24px; border-radius: 50%;
    display: flex; align-items: center; justify-content: center; font-size: 14px; color: #fff;
    background: var(--green); border: 3px solid var(--bg-card);
  }
  .avatar-dot.off { background: var(--orange); }

  .badges { display: flex; flex-wrap: wrap; gap: 6px; }
  .badge {
    display: inline-flex; align-items: center; gap: 5px; padding: 4px 9px; border-radius: 999px;
    font: 600 var(--fs-min)/1 var(--font-ui); border: 1px solid transparent;
  }
  .badge i { font-size: 13px; }
  .badge.green  { color: var(--green);  background: color-mix(in srgb, var(--green) 13%, transparent);  border-color: color-mix(in srgb, var(--green) 30%, transparent); }
  .badge.orange { color: var(--orange); background: color-mix(in srgb, var(--orange) 13%, transparent); border-color: color-mix(in srgb, var(--orange) 30%, transparent); }
  .badge.blue   { color: var(--blue);   background: color-mix(in srgb, var(--blue) 13%, transparent);   border-color: color-mix(in srgb, var(--blue) 30%, transparent); }

  /* ID bar */
  .idbar {
    position: relative; display: flex; align-items: center; gap: 12px; padding: 10px 10px 10px 12px; border-radius: 13px;
    background: color-mix(in srgb, var(--bg-input) 80%, transparent); border: 1px solid var(--border);
  }
  .idbar-ic { width: 34px; height: 34px; border-radius: 10px; flex-shrink: 0; display: flex; align-items: center; justify-content: center; font-size: 18px; color: var(--blue); background: color-mix(in srgb, var(--blue) 14%, transparent); }
  .idbar-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 3px; }
  .idbar-body small { font: 500 var(--fs-min)/1 var(--font-ui); color: var(--text-dim); }
  .idbar-val { width: 100%; background: transparent; border: none; outline: none; padding: 0; color: var(--text); font: 600 13px/1.2 var(--font-mono); letter-spacing: .02em; }

  .copy {
    width: 34px; height: 34px; flex-shrink: 0; border-radius: 10px; cursor: pointer;
    display: flex; align-items: center; justify-content: center; font-size: 16px;
    color: var(--text-mute); background: var(--hover); border: 1px solid var(--border);
    transition: background .15s, color .15s, border-color .15s, transform .1s;
  }
  .copy:hover { background: var(--hover-strong); color: var(--text); }
  .copy:active { transform: scale(.94); }
  .copy.ok { color: var(--green); border-color: color-mix(in srgb, var(--green) 40%, transparent); background: color-mix(in srgb, var(--green) 12%, transparent); }
  .copy.sm { width: 28px; height: 28px; border-radius: 8px; font-size: 14px; }

  /* ── Статистика ── */
  .stats { display: grid; grid-template-columns: repeat(3, 1fr); gap: 10px; }
  .tile {
    --c: var(--orange);
    display: flex; flex-direction: column; gap: 4px; padding: 14px; border-radius: 14px;
    background: var(--bg-card); border: 1px solid var(--border);
    transition: transform .15s, border-color .15s;
  }
  .tile:hover { transform: translateY(-2px); border-color: color-mix(in srgb, var(--c) 40%, var(--border)); }
  .t-orange { --c: var(--orange); } .t-violet { --c: var(--violet-l); } .t-blue { --c: var(--blue); }
  .tile-ic { width: 34px; height: 34px; border-radius: 10px; display: flex; align-items: center; justify-content: center; font-size: 18px; color: var(--c); background: color-mix(in srgb, var(--c) 14%, transparent); margin-bottom: 6px; }
  .tile-v { font: 800 20px/1.1 var(--font-ui); color: var(--text); letter-spacing: -.01em; }
  .tile-l { font: 500 11px/1.2 var(--font-ui); color: var(--text-mute); }
  .note { display: flex; align-items: center; gap: 6px; margin-top: -4px; font: 400 var(--fs-min)/1.4 var(--font-ui); color: var(--text-dim); padding: 0 2px; }

  /* ── Картки ── */
  .acard { display: flex; flex-direction: column; gap: 12px; padding: 16px; border-radius: 16px; background: var(--bg-card); border: 1px solid var(--border); }
  .acard-t { display: flex; align-items: center; gap: 8px; font: 700 13px/1 var(--font-ui); color: var(--text); }
  .acard-t > i { color: var(--orange); font-size: 17px; }
  .nchip {
    margin-left: auto; display: inline-flex; align-items: center; gap: 5px; padding: 4px 9px; border-radius: 999px;
    font: 600 var(--fs-min)/1 var(--font-ui); color: var(--text-mute); background: var(--hover); border: 1px solid var(--border);
  }
  .nchip.zero { color: var(--red); background: color-mix(in srgb, var(--red) 11%, transparent); border-color: color-mix(in srgb, var(--red) 30%, transparent); }

  .list { display: flex; flex-direction: column; }
  .li { display: flex; align-items: center; gap: 12px; padding: 10px 2px; border-top: 1px solid var(--border); min-height: 46px; }
  .li:first-child { border-top: none; padding-top: 2px; }
  .li-ic { width: 30px; height: 30px; border-radius: 9px; flex-shrink: 0; display: flex; align-items: center; justify-content: center; font-size: 16px; color: var(--text-mute); background: var(--hover); }
  .li-k { font: 500 12px/1.2 var(--font-ui); color: var(--text-mute); min-width: 112px; }
  .li-v { flex: 1; min-width: 0; font: 600 12.5px/1.3 var(--font-ui); color: var(--text); word-break: break-all; text-align: right; }
  .li-v.mono { font: 600 11.5px/1.3 var(--font-mono); }

  /* ── Нік ── */
  .field-h { font: 400 11px/1.5 var(--font-ui); color: var(--text-mute); }
  .nickrow { display: flex; gap: 10px; align-items: stretch; flex-wrap: wrap; }
  .inp-wrap { position: relative; flex: 1 1 200px; min-width: 0; }
  .inp-wrap > i { position: absolute; left: 12px; top: 50%; transform: translateY(-50%); color: var(--text-dim); font-size: 16px; pointer-events: none; }
  .inp-wrap .inp { padding-left: 36px; }
  .inp { width: 100%; box-sizing: border-box; height: 40px; padding: 0 12px; border-radius: 11px; background: var(--bg-input); border: 1px solid var(--border); color: var(--text); font-size: 13px; outline: none; transition: border-color .15s, box-shadow .15s; }
  .inp:focus { border-color: color-mix(in srgb, var(--orange) 60%, transparent); box-shadow: 0 0 0 3px color-mix(in srgb, var(--orange) 14%, transparent); }
  .inp:disabled { opacity: .55; }
  .locked { display: flex; align-items: flex-start; gap: 10px; padding: 11px 12px; border-radius: 12px; font: 400 11.5px/1.5 var(--font-ui); color: var(--text-mute); background: color-mix(in srgb, var(--green) 8%, transparent); border: 1px solid color-mix(in srgb, var(--green) 24%, transparent); }
  .locked i { color: var(--green); font-size: 17px; margin-top: 1px; }

  /* ── Створення акаунта ── */
  .perks { position: relative; display: grid; grid-template-columns: repeat(4, 1fr); gap: 8px; }
  .perk { display: flex; flex-direction: column; align-items: center; gap: 7px; padding: 12px 6px; border-radius: 13px; background: color-mix(in srgb, var(--bg-input) 75%, transparent); border: 1px solid var(--border); font: 600 11px/1 var(--font-ui); color: var(--text-mute); }
  .perk i { font-size: 20px; color: var(--orange); }
  .field { position: relative; display: flex; flex-direction: column; gap: 7px; }
  .field-l { display: flex; align-items: center; gap: 6px; font: 600 11.5px/1 var(--font-ui); color: var(--text); }
  .field-l i { color: var(--text-dim); font-size: 14px; }
  .field .inp { height: 44px; font-size: 14px; }
  .prov { position: relative; display: flex; flex-direction: column; gap: 9px; }
  .prov-btn {
    display: flex; align-items: center; gap: 12px; padding: 11px 14px 11px 11px; border-radius: 14px; cursor: pointer; text-align: left;
    color: var(--text); background: var(--bg-input); border: 1px solid var(--border-hi); transition: transform .12s, border-color .15s, background .15s;
  }
  .prov-btn:hover { transform: translateY(-1px); background: var(--bg-input-hi); }
  .prov-btn:active { transform: scale(.99); }
  .prov-btn.google:hover { border-color: color-mix(in srgb, var(--red) 55%, transparent); }
  .prov-btn.ms:hover { border-color: color-mix(in srgb, var(--blue) 55%, transparent); }
  .prov-ic { width: 38px; height: 38px; border-radius: 11px; flex-shrink: 0; display: flex; align-items: center; justify-content: center; font-size: 20px; color: #fff; }
  .google .prov-ic { background: linear-gradient(135deg, #ea4335, #c5221f); }
  .ms .prov-ic { background: linear-gradient(135deg, #3b82f6, #1d4ed8); }
  .prov-t { flex: 1; display: flex; flex-direction: column; gap: 3px; min-width: 0; }
  .prov-t b { font: 700 13px/1.1 var(--font-ui); }
  .prov-t small { font: 400 var(--fs-min)/1.2 var(--font-ui); color: var(--text-mute); }
  .prov-go { color: var(--text-dim); font-size: 17px; transition: transform .15s, color .15s; }
  .prov-btn:hover .prov-go { transform: translateX(3px); color: var(--text); }
  .wait { position: relative; display: flex; align-items: center; gap: 10px; flex-wrap: wrap; padding: 12px 14px; border-radius: 13px; font: 500 12px/1.4 var(--font-ui); color: var(--orange); background: color-mix(in srgb, var(--orange) 9%, transparent); border: 1px solid color-mix(in srgb, var(--orange) 28%, transparent); }
  .wait span { flex: 1; min-width: 160px; }
  .privacy { position: relative; display: flex; align-items: flex-start; gap: 8px; font: 400 var(--fs-min)/1.5 var(--font-ui); color: var(--text-dim); }
  .privacy i { font-size: 15px; color: var(--green); margin-top: 1px; }

  /* ── Оновлення / порожні стани / небезпечна зона ── */
  .upgrade { display: flex; align-items: flex-start; gap: 12px; padding: 13px 14px; border-radius: 14px; background: color-mix(in srgb, var(--yellow) 10%, transparent); border: 1px solid color-mix(in srgb, var(--yellow) 34%, transparent); }
  .upgrade-ic { width: 34px; height: 34px; border-radius: 10px; flex-shrink: 0; display: flex; align-items: center; justify-content: center; font-size: 19px; color: var(--yellow); background: color-mix(in srgb, var(--yellow) 16%, transparent); }
  .upgrade b { display: block; font: 700 12.5px/1.2 var(--font-ui); color: var(--text); margin-bottom: 4px; }
  .upgrade span { font: 400 11px/1.5 var(--font-ui); color: var(--text-mute); }

  .empty { display: flex; flex-direction: column; align-items: center; gap: 8px; text-align: center; padding: 34px 20px; border-radius: 16px; background: var(--bg-card); border: 1px dashed var(--border-hi); }
  .empty-ic { width: 52px; height: 52px; border-radius: 16px; display: flex; align-items: center; justify-content: center; font-size: 26px; color: var(--yellow); background: color-mix(in srgb, var(--yellow) 12%, transparent); }
  .empty-t { font: 700 14px/1.2 var(--font-ui); color: var(--text); }
  .empty-s { font: 400 11.5px/1.5 var(--font-ui); color: var(--text-mute); max-width: 340px; }

  .danger { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; padding: 13px 14px; border-radius: 14px; background: color-mix(in srgb, var(--red) 6%, transparent); border: 1px solid color-mix(in srgb, var(--red) 24%, transparent); }
  .danger-ic { width: 34px; height: 34px; border-radius: 10px; flex-shrink: 0; display: flex; align-items: center; justify-content: center; font-size: 18px; color: var(--red); background: color-mix(in srgb, var(--red) 14%, transparent); }
  .danger-txt { flex: 1; min-width: 200px; display: flex; flex-direction: column; gap: 3px; }
  .danger-txt b { font: 700 12.5px/1.2 var(--font-ui); color: var(--text); }
  .danger-txt span { font: 400 11px/1.5 var(--font-ui); color: var(--text-mute); }
  .btn-danger { display: inline-flex; align-items: center; gap: 6px; height: 34px; padding: 0 14px; border-radius: 10px; cursor: pointer; font: 600 12px/1 var(--font-ui); color: var(--red); background: color-mix(in srgb, var(--red) 10%, transparent); border: 1px solid color-mix(in srgb, var(--red) 35%, transparent); transition: background .15s, color .15s; }
  .btn-danger:hover:not(:disabled) { background: var(--red); color: #fff; }
  .btn-danger:disabled { opacity: .55; cursor: default; }

  /* ── Скелетон завантаження ── */
  .skel { border-radius: 16px; background: linear-gradient(90deg, var(--bg-card) 25%, var(--hover-strong) 50%, var(--bg-card) 75%); background-size: 200% 100%; animation: skel 1.2s linear infinite; border: 1px solid var(--border); }
  .hero-skel { height: 190px; } .tile-skel { height: 110px; }
  @keyframes skel { to { background-position: -200% 0; } }

  @media (max-width: 560px) {
    .stats { grid-template-columns: 1fr; }
    .perks { grid-template-columns: repeat(2, 1fr); }
    .hero-name { font-size: 19px; }
    .li-k { min-width: 0; }
  }
  /* ── Візитка (біо + колір) ── */
  .bio-wrap { position: relative; }
  .bio { height: auto; min-height: 76px; padding: 10px 12px 26px; resize: none; line-height: 1.45; font-family: var(--font-ui); display: block; }
  .bio-wrap.bad .bio { border-color: var(--red); }
  .bio-count { position: absolute; right: 10px; bottom: 7px; font: 600 11px/1 var(--font-mono); color: var(--text-dim); pointer-events: none; }
  .bio-count.warn { color: #eab308; } .bio-count.full { color: var(--red); }
  .swatches { display: flex; flex-wrap: wrap; gap: 8px; }
  .sw { position: relative; width: 28px; height: 28px; border-radius: 50%; padding: 0; cursor: pointer; background: var(--c, var(--bg-input));
    border: 2px solid transparent; outline: 1px solid var(--border-hi); outline-offset: 1px; display: grid; place-items: center; color: var(--text-dim);
    transition: transform .12s, outline-color .12s, box-shadow .12s; }
  .sw:hover { transform: scale(1.12); }
  .sw:focus-visible { outline: 2px solid var(--orange); outline-offset: 2px; }
  .sw.on { outline: 2px solid var(--text); outline-offset: 2px; }
  .sw i { font-size: 14px; }
  .sw-custom { background: conic-gradient(#ef4444, #eab308, #22c55e, #06b6d4, #6366f1, #ec4899, #ef4444); color: #fff; }
  .sw-custom i { filter: drop-shadow(0 1px 2px rgba(0,0,0,.6)); }
  .sw-custom input { position: absolute; inset: 0; opacity: 0; width: 100%; height: 100%; cursor: pointer; }
  .pv { --cv: hsl(var(--h, 24) 70% 52%); border-radius: 14px; overflow: hidden; border: 1px solid var(--border);
    background: color-mix(in srgb, var(--cv) 9%, var(--bg-panel)); }
  .pv-cover { height: 44px; background: linear-gradient(135deg, var(--cv), color-mix(in srgb, var(--cv) 50%, #000)); }
  .pv-body { display: flex; align-items: center; gap: 12px; padding: 0 14px 12px; margin-top: -18px; }
  .pv-ava { flex: 0 0 auto; width: 48px; height: 48px; border-radius: 14px; background: var(--bg-input); border: 3px solid var(--bg-panel); display: grid; place-items: center; overflow: hidden; color: var(--text-mute); }
  .pv-ava img { width: 100%; height: 100%; object-fit: cover; image-rendering: pixelated; }
  .pv-txt { min-width: 0; padding-top: 18px; display: flex; flex-direction: column; gap: 2px; }
  .pv-txt b { font: 700 13px/1.2 var(--font-ui); color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .pv-txt span { font: 400 12px/1.4 var(--font-ui); color: var(--text-mute); overflow-wrap: anywhere; display: -webkit-box; -webkit-line-clamp: 3; line-clamp: 3; -webkit-box-orient: vertical; overflow: hidden; }
  .style-acts { display: flex; flex-wrap: wrap; gap: 8px; }
  @media (prefers-reduced-motion: reduce) { .sw { transition: none; } }
</style>