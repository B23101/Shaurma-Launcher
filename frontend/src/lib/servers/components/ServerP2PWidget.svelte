<script lang="ts">
  // ── Віджет P2P-хостингу ("LAN через інтернет") ────────────────────────
  // Власний Headscale/tsnet-тунель без потреби ставити зовнішній агент.
  // Гравець один раз застовблює адресу (напр.
  // "prlool" → "prlool-482917.play.shaurma-launcher.org") — вона
  // лишається сталою між сесіями хостингу навіть попри те, що сама
  // tsnet-нода щоразу отримує нову IP.
  //
  // Двоетапний потік:
  //   1. Немає збереженої адреси → форма "застовбити базову назву"
  //      (ClaimP2PAddress).
  //   2. Адреса вже застовблена → кнопка "Почати хостинг"
  //      (StartP2PHost) — піднімає tsnet-ноду під цю адресу.
  //
  // Застовблена адреса зберігається в localStorage віджета (не в
  // бекенді — бекенд знає лише те, що застовблено на Worker, а яку саме
  // адресу гравець хоче використовувати для ЦЬОГО сервера, вирішує
  // фронтенд).
  // Голосовий чат через P2P іде ОКРЕМИМ UDP-портом (Simple Voice Chat —
  // 24454 за замовчуванням), бо сам Minecraft спілкується TCP. UDP-порт
  // усередині P2P-мережі (Headscale) видно ВСІМ її нодам — не лише тому,
  // хто знає адресу-«PIN», як у TCP. Тому галочка вимкнена за замовчуванням,
  // а гравцю показується чесне попередження (див. backend/internal/p2p/udp.go).
  import { App } from '../../wails'
  import { t } from '../../i18n.svelte'
  import { toast } from '../../toast.svelte'
  import { p2pErr } from '../p2pErr'
  import Portal from '../../Portal.svelte'
  import P2PHealthCard from './P2PHealthCard.svelte'
  import type { P2PHostStatus, P2PConnection, P2PClaimedAddress } from '../../types'

  let {
    port = 25565,
    isRunning = false,
    refreshKey = 0,
    // serverId — СТАБІЛЬНА ідентичність сервера (id із бази лаунчера).
    // Раніше адреса зберігалась у localStorage під ключем із ПОРТОМ, а порт
    // сервера змінюється — тому кожен запуск вимагав нового claim'у. Тепер
    // адреса лежить у бекенді (внутрішня книга адрес, прив'язана до акаунта)
    // під міткою serverId.
    serverId = '',
    // accountOk/accountReason — P2P вимагає Shaurma/ліцензійний акаунт
    // (адреси прив'язуються до акаунта).
    accountOk = true,
    accountReason = '',
    // roomDomain — суфікс P2P-адрес ("play.shaurma-launcher.org"), з бекенда,
    // щоб збирати повні адреси для списку збережених.
    roomDomain = 'play.shaurma-launcher.org',
    // autoStart — перемикач «запускати P2P разом із сервером». Зберігається
    // в САМОМУ сервері (server.json), тому переживає перезапуск лаунчера.
    autoStart = false,
    // onAutoStartChange — повідомляє сторінку сервера, що перемикач
    // зберігся: саме сторінка тепер керує автозапуском P2P (він більше
    // не залежить від того, чи відкрито вкладку «Мережа»), тож їй
    // потрібне свіже значення прапорця.
    onAutoStartChange = null,
  }: {
    port?: number
    isRunning?: boolean
    refreshKey?: number
    serverId?: string
    accountOk?: boolean
    accountReason?: string
    roomDomain?: string
    autoStart?: boolean
    onAutoStartChange?: ((v: boolean) => void) | null
  } = $props()

  // ownerKey — ключ, під яким адреса закріплена за ЦИМ сервером.
  const ownerKey = $derived(serverId || `port-${port}`)
  let status = $state<P2PHostStatus | null>(null)
  let loading = $state(true)
  let busy = $state(false)
  let baseNameDraft = $state('')
  let claimOpen = $state(false)
  let savedSubdomain = $state<string>('')
  // connections — хто зараз під'єднаний до НАШОГО сервера і який у нього пінг.
  let connections = $state<P2PConnection[]>([])
  // addresses — УСІ адреси цього акаунта: щоб гравець бачив свої адреси і міг
  // взяти для цього сервера вже зайняту, а не займати нову щораза.
  let addresses = $state<P2PClaimedAddress[]>([])
  let pickerOpen = $state(false)
  // Тимчасові адреси запрошень (tmpN) — не «збережені»: бекенд їх уже не віддає, це страховка на
  // випадок старого файлу книги.
  const ownAddresses = $derived(addresses.filter((a) => a.label !== 'temp-invite' && !/^tmp[0-9]-/.test(a.subdomain)))

  function openPicker() {
    pickerOpen = true
    confirmDelSub = ''
  }

  // confirmDelSub — адреса, для якої показано «Точно видалити?» (перший клік —
  // підтвердження, другий — видалення). Без модалки поверх модалки.
  let confirmDelSub = $state('')
  let confirmDelTimer: ReturnType<typeof setTimeout> | undefined

  // deleteSaved — видаляє збережену адресу (будь-яку, не лише поточну). Бекенд
  // зупиняє хост, звільняє адресу в lobby і прибирає її з книги. Навіть якщо
  // lobby не відповів, локальний запис зникає — тому список перечитуємо завжди.
  async function deleteSaved(sub: string) {
    if (confirmDelSub !== sub) {
      confirmDelSub = sub
      clearTimeout(confirmDelTimer)
      confirmDelTimer = setTimeout(() => { if (confirmDelSub === sub) confirmDelSub = '' }, 4000)
      return
    }
    clearTimeout(confirmDelTimer)
    confirmDelSub = ''
    busy = true
    try {
      await App.ReleaseP2PAddress(sub)
      toast(t('servers.p2p.toast.released'), 'success')
    } catch (e: any) {
      toast(p2pErr(e), 'error')
    } finally {
      if (sub === savedSubdomain) { savedSubdomain = ''; connections = [] }
      await loadAddress()
      await refresh()
      busy = false
    }
  }

  // loadAddress — бере адресу ЦЬОГО сервера з бекендової книги. Це головна
  // частина виправлення «не зберігається занятий ip»: адреса переживає
  // перезапуск лаунчера й не залежить від випадкового порту гри.
  async function loadAddress() {
    try {
      addresses = (await App.ListP2PAddresses()) ?? []
      const match = ownAddresses.find((a) => (a.label || '') === ownerKey)
        ?? ownAddresses.find((a) => a.subdomain === savedSubdomain)
      if (match) savedSubdomain = match.subdomain
    } catch { /* немає адрес — це нормальний перший запуск */ }
  }

  // fullAddr — повна адреса для показу/копіювання.
  function fullAddr(sub: string): string {
    return `${sub}.${roomDomain}`
  }

  // pickAddress — гравець у списку своїх адрес вибрав ту, яку хоче
  // використовувати для ЦЬОГО сервера. Адреса закріплюється за сервером
  // на бекенді (мітка = serverId), тож переживе перезапуск лаунчера.
  async function pickAddress(sub: string) {
    busy = true
    try {
      await App.AssignP2PAddress(sub, ownerKey)
      savedSubdomain = sub
      pickerOpen = false
      await loadAddress()
      await refresh()
      toast(t('servers.p2p.toast.picked', { addr: fullAddr(sub) }), 'success')
    } catch (e: any) { toast(p2pErr(e), 'error') }
    finally { busy = false }
  }

  // UDP-налаштування й автозапуск більше НЕ живуть у цьому віджеті: їх веде бекенд, а показує
  // і змінює P2PHealthCard. Тут лише адреса й старт/стоп тунелю.
  $effect(() => {
    void ownerKey
    void refreshKey
    void loadAddress()
  })

  // refresh — перечитати стан хостингу.
  //
  // silent=true — «тихе» оновлення (фонове опитування): НЕ вмикає індикатор
  // завантаження, інакше пігулка «Перевірка…» блимала б кожні 5 секунд.
  async function refresh(silent = false) {
    if (!silent) loading = true
    try {
      status = savedSubdomain ? await App.GetP2PHostStatus(savedSubdomain) : null
    } catch { if (!silent) status = null }
    finally { if (!silent) loading = false }
  }

  $effect(() => {
    void refreshKey
    void savedSubdomain
    // Хостинг міг підняти/покласти бекенд РАЗОМ із сервером (це робить
    // ServerInstancePage) — перечитуємо статус і на зміну isRunning, щоб
    // пігулка та список гравців не «застигли».
    void isRunning
    refresh()
  })

  // ── Живе оновлення P2P-екрана ────────────────────────────────────────
  // Раніше статус перечитувався ЛИШЕ на зміну пропсів (refreshKey /
  // savedSubdomain / isRunning). Поки вкладка «Мережа» змонтована, ніщо не
  // змінювалось — щоб побачити актуальний стан (напр. що хостинг піднявся
  // сам разом із сервером, або що гість уже в мережі), доводилось
  // перемикати розділ і повертатись, бо це єдине, що перемонтовувало
  // віджет. Тепер статус опитується сам — плюс одразу при поверненні у
  // вікно лаунчера.
  $effect(() => {
    if (!savedSubdomain) return
    const tick = () => { void refresh(true) }
    const id = setInterval(tick, 5000)
    const onFocus = () => { tick() }
    const onVisible = () => { if (document.visibilityState === 'visible') tick() }
    window.addEventListener('focus', onFocus)
    document.addEventListener('visibilitychange', onVisible)
    return () => {
      clearInterval(id)
      window.removeEventListener('focus', onFocus)
      document.removeEventListener('visibilitychange', onVisible)
    }
  })

  const active = $derived(!!status?.active)
  // Адресу показуємо і коли хостинг вимкнено — вона стала, це не «стан тунелю».
  const address = $derived(status?.address || (savedSubdomain ? fullAddr(savedSubdomain) : ''))

  // Поки хостинг активний — раз на 3 с тягнемо список під'єднаних гравців,
  // щоб власник бачив «хто в мережі» й пінг живо, без перемикання розділів.
  // Разом із опитуванням статусу вище це дає повністю самоновлюваний екран.
  $effect(() => {
    if (!active || !savedSubdomain) { connections = []; return }
    const sub = savedSubdomain
    void loadConnections(sub)
    const id = setInterval(() => { void loadConnections(sub) }, 3000)
    return () => clearInterval(id)
  })

  async function loadConnections(sub: string) {
    try { connections = (await App.HostP2PConnections(sub)) ?? [] }
    catch { connections = [] }
  }

  async function claim() {
    const name = baseNameDraft.trim().toLowerCase()
    if (!name) return
    busy = true
    try {
      const result = await App.ClaimP2PAddress(name, ownerKey)
      savedSubdomain = result.subdomain
      baseNameDraft = ''
      claimOpen = false
      // reused=true — ми ПОВЕРНУЛИ вже зайняту цим акаунтом адресу, а не
      // зайняли нову. Це те, що просив користувач.
      toast(
        result.reused
          ? t('servers.p2p.toast.reused', { addr: result.address })
          : t('servers.p2p.toast.claimed', { addr: result.address }),
        'success'
      )
    } catch (e: any) { toast(p2pErr(e), 'error') }
    finally { busy = false }
  }

  async function startHost() {
    if (!savedSubdomain) return
    busy = true
    try {
      // Бекенд сам підбирає UDP-порт: зі збережених налаштувань і конфігу голосового моду.
      const result = serverId
        ? await App.P2PStartForServer(serverId)
        : await App.StartP2PHost(String(port), savedSubdomain, 0)
      status = result
      toast(t('servers.p2p.toast.started', { addr: result.address }), 'success')
      if (result.disabled) {
        toast(t('servers.p2p.toast.disabled'), 'error')
      }
      // Чому голос не піднявся (порт зайнятий тощо) — картка діагностики покаже деталі.
      if (result.udpError) toast(t('servers.p2p.toast.udpDown'), 'error')
    } catch (e: any) { toast(p2pErr(e), 'error') }
    finally { busy = false }
  }

  async function stopHost() {
    if (!savedSubdomain) return
    busy = true
    try {
      await App.StopP2PHost(savedSubdomain)
      connections = []
      await refresh()
      toast(t('servers.p2p.toast.stopped'), 'success')
    }
    catch (e: any) { toast(p2pErr(e), 'error') }
    finally { busy = false }
  }

  async function releaseAddress() {
    if (!savedSubdomain) return
    busy = true
    try {
      await App.ReleaseP2PAddress(savedSubdomain)
      toast(t('servers.p2p.toast.released'), 'success')
    } catch (e: any) { toast(p2pErr(e), 'error') }
    finally {
      // Перечитуємо книгу завжди: локальний запис видаляється навіть при збої мережі.
      savedSubdomain = ''
      connections = []
      await loadAddress()
      await refresh()
      busy = false
    }
  }

  // pingLabel — людяне форматування затримки для списку гравців.
  function pingLabel(ms: number): string {
    if (!ms) return t('servers.p2p.members.pingPending')
    return `${ms} ms`
  }

  function bytesLabel(n: number): string {
    if (n < 1024) return `${n} B`
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
    return `${(n / (1024 * 1024)).toFixed(1)} MB`
  }

  async function copy(text: string) {
    try { await navigator.clipboard.writeText(text); toast(t('servers.p2p.copied'), 'success') }
    catch { toast(t('servers.p2p.copyFailed'), 'error') }
  }

  // missing — адреси ще немає (треба застовбити); idle — адреса є, хостинг
  // не запущено; online — хостинг активний.
  const viewState: 'missing' | 'idle' | 'online' = $derived(
    active ? 'online' : savedSubdomain ? 'idle' : 'missing'
  )

  // ── Автозапуск P2P разом із сервером ──────────────────────────────────
  // Локальний стан + збереження у backend (SaveServerConfig.autoStartP2P).
  // Синхронізується з пропса ЛИШЕ при зміні сервера — інакше збережене
  // значення «відкотилось» би від застарілого пропса після старту/стопу.
  let autoStartOn = $state(autoStart)
  let autoStartSaving = $state(false)
  let autoOwner = $state('')
  $effect(() => {
    const id = serverId
    if (autoOwner !== id) { autoOwner = id; autoStartOn = autoStart }
  })

  async function toggleAutoStart() {
    if (autoStartSaving || !serverId) return
    const next = !autoStartOn
    autoStartOn = next
    autoStartSaving = true
    try {
      await App.SaveServerConfig(serverId, { autoStartP2P: next })
      onAutoStartChange?.(next)
      void App.P2PEnsureAuto(serverId).catch(() => {})
      toast(t(next ? 'servers.p2p.autoStartOn' : 'servers.p2p.autoStartOff'), 'success')
    } catch (e: any) {
      autoStartOn = !next
      toast(p2pErr(e), 'error')
    } finally { autoStartSaving = false }
  }

  // Автостарт/автостоп тунеля РАЗОМ ІЗ СЕРВЕРОМ тепер живе на сторінці
  // сервера (ServerInstancePage), а не тут: у віджеті він спрацьовував
  // ЛИШЕ коли гравець відкривав вкладку «Мережа». Віджет лишається
  // ручним керуванням (Почати/Зупинити) і перечитує статус (див. ефект
  // вище, він реагує і на isRunning).
</script>

<div class="spw" class:on={viewState === 'online'}>
  <div class="spw-bar" class:on={viewState === 'online'}></div>

  <div class="spw-head">
    <div class="spw-tt">
      <span class="spw-label">{t('servers.p2p.addrTitle')}</span>
      {#if savedSubdomain}<span class="spw-own"><i class="ti ti-user-check"></i> {t('servers.p2p.mine')}</span>{/if}
    </div>
    <button
      class="spw-auto"
      class:on={autoStartOn}
      onclick={toggleAutoStart}
      disabled={autoStartSaving || !serverId}
      title={t('servers.p2p.autoStartHint')}
      aria-pressed={autoStartOn}
    >
      {#if autoStartSaving}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-bolt"></i>{/if}
      <span>{t('servers.p2p.autoStart')}</span>
      <span class="spw-auto-sw"><span></span></span>
    </button>
    <span class="spw-pill {viewState}">
      {#if loading}<i class="ti ti-loader spin"></i>{:else}<span class="spw-dot"></span>{/if}
      {#if loading}{t('servers.p2p.checking')}
      {:else if viewState === 'online'}{t('servers.p2p.pill.online')}
      {:else if viewState === 'missing'}{t('servers.p2p.pill.missing')}
      {:else}{t('servers.p2p.pill.off')}{/if}
    </span>
    <button class="spw-icon-btn" onclick={() => refresh()} disabled={loading} title={t('servers.p2p.refresh')} aria-label={t('servers.p2p.refresh')}>
      <i class="ti ti-refresh"></i>
    </button>
  </div>

  {#if status?.error}
    <div class="spw-err"><i class="ti ti-alert-triangle"></i> {status.error}</div>
  {/if}
  {#if !accountOk}
    <div class="spw-err">
      <i class="ti ti-user-exclamation"></i>
      {accountReason || t('servers.p2p.account.required')}
    </div>
  {/if}
  {#if active && status?.disabled}
    <div class="spw-err"><i class="ti ti-shield-off"></i> {t('servers.p2p.disabledByAdmin')}</div>
  {/if}

  {#if address}
    <div class="spw-addr">
      <code class="spw-addr-t" title={address}>{address}</code>
      <button class="spw-copy" onclick={() => copy(address)} title={t('servers.p2p.copyTitle')} aria-label={t('servers.p2p.copy')}>
        <i class="ti ti-copy"></i>
      </button>
    </div>
    <div class="spw-sub">
      {#if viewState === 'online'}{t('servers.p2p.sub.online')}{:else}{t('servers.p2p.sub.idle')}{/if}
    </div>
  {:else}
    <div class="spw-empty">
      <div class="spw-empty-t">{t('servers.p2p.sub.missing')}</div>
      <div class="spw-empty-s">{t('servers.p2p.claimHint')}</div>
    </div>
  {/if}

  <div class="spw-actions">
    {#if viewState === 'missing'}
      <button class="spw-btn primary" onclick={() => (claimOpen = !claimOpen)} aria-expanded={claimOpen} disabled={!accountOk}>
        <i class="ti ti-plus"></i> {t('servers.p2p.claim')}
      </button>
      {#if ownAddresses.length > 0}
        <button class="spw-btn" onclick={openPicker} disabled={!accountOk}>
          <i class="ti ti-list"></i> {t('servers.p2p.pick')}
        </button>
      {/if}
    {:else if viewState === 'idle'}
      <button class="spw-btn primary" onclick={startHost} disabled={busy}>
        {#if busy}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-player-play"></i>{/if} {t('servers.p2p.start')}
      </button>
      <button class="spw-btn" onclick={openPicker} disabled={busy}>
        <i class="ti ti-list"></i> {t('servers.p2p.pick')}
      </button>
      <button class="spw-btn ghost danger-text" onclick={releaseAddress} disabled={busy} title={t('servers.p2p.release')} aria-label={t('servers.p2p.release')}>
        <i class="ti ti-trash"></i>
      </button>
    {:else}
      <button class="spw-btn danger" onclick={stopHost} disabled={busy}>
        {#if busy}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-player-stop"></i>{/if} {t('servers.p2p.stop')}
      </button>
      <span class="spw-hint">{t('servers.p2p.tunnelPort', { port })}</span>
    {/if}
  </div>
  {#if viewState === 'idle'}
    <div class="spw-hint">{#if isRunning}{t('servers.p2p.hint.running')}{:else}{t('servers.p2p.hint.startFirst')}{/if}</div>
  {/if}

  {#if claimOpen}
    <div class="spw-setup">
      <div class="spw-setup-row">
        <input class="input mono" bind:value={baseNameDraft} placeholder={t('servers.p2p.basePlaceholder')} spellcheck="false" maxlength="24"
          onkeydown={(e) => { if (e.key === 'Enter') claim() }} />
        <button class="btn btn-primary btn-sm" onclick={claim} disabled={busy || !baseNameDraft.trim() || !accountOk}>
          <i class="ti ti-check"></i> {t('servers.p2p.claimShort')}
        </button>
      </div>
    </div>
  {/if}
</div>

<!-- ── Хто зараз у мережі (з'являється, коли хостинг активний) ── -->
{#if active}
  <div class="spw-card spw-members">
    <div class="spw-card-h"><i class="ti ti-users"></i> {t('servers.p2p.members.title', { count: connections.length })}</div>
    {#if connections.length === 0}
      <div class="spw-hint">{t('servers.p2p.members.empty')}</div>
    {:else}
      {#each connections as c (c.id)}
        <div class="spw-member">
          <i class="ti ti-user"></i>
          <span class="mono spw-member-ip">{c.guestIp || c.remoteAddr}</span>
          <span class="spw-member-ping">{pingLabel(c.pingMs)}</span>
          <span class="spw-member-bytes">{bytesLabel(c.bytesIn + c.bytesOut)}</span>
        </div>
      {/each}
    {/if}
  </div>
{/if}

<!-- Попередження про «тільки через Shaurma Launcher» — завжди, а не лише коли хостинг активний: його
     треба прочитати ДО того, як роздаси адресу друзям. Коротке й по суті: раніше тут був абзац про те,
     куди саме заходять друзі, — зайвий текст, який лише відлякував. -->
{#if accountOk}
  <div class="spw-note warn">
    <i class="ti ti-shield-lock"></i>
    <span><b>{t('servers.p2p.launcherOnlyBold')}</b> {t('servers.p2p.launcherOnly')}</span>
  </div>
{/if}

<!-- ── Чесний стан P2P + голосовий чат (дані й логіка — на бекенді) ── -->
{#if serverId}
  <P2PHealthCard {serverId} {refreshKey} onChanged={() => void refresh()} />
{/if}

<!-- ── Вибір зі збережених адрес ── -->
{#if pickerOpen}
  <Portal>
    <div class="spw-ov" onclick={() => (pickerOpen = false)} role="presentation">
      <div class="spw-modal" onclick={(e: MouseEvent) => e.stopPropagation()} role="dialog" aria-modal="true" aria-label={t('servers.p2p.pickTitle')}>
        <div class="spw-modal-h">
          <i class="ti ti-list"></i> {t('servers.p2p.pickTitle')}
          <button class="spw-icon-btn" onclick={() => (pickerOpen = false)} aria-label={t('cancel')}><i class="ti ti-x"></i></button>
        </div>
        <div class="spw-modal-b">
          <div class="spw-sec">{t('servers.p2p.pickMine')}</div>
          {#each ownAddresses as a (a.subdomain)}
            {@const cur = a.subdomain === savedSubdomain}
            {@const other = !!a.label && a.label !== ownerKey}
            <div class="spw-pr" class:cur>
              <i class="ti ti-world"></i>
              <code class="spw-pr-addr" title={fullAddr(a.subdomain)}>{fullAddr(a.subdomain)}</code>
              {#if cur}
                <span class="spw-tag cur"><i class="ti ti-check"></i> {t('servers.p2p.pickCurrent')}</span>
              {:else if other}
                <span class="spw-tag">{t('servers.p2p.pickUsed')}</span>
              {/if}
              {#if !cur}
                <button class="spw-btn" onclick={() => pickAddress(a.subdomain)} disabled={busy}>
                  <i class="ti ti-corner-down-left"></i> {t('servers.p2p.pickUse')}
                </button>
              {/if}
              <button class="spw-icon-btn" onclick={() => copy(fullAddr(a.subdomain))} title={t('servers.p2p.copyTitle')} aria-label={t('servers.p2p.copy')}>
                <i class="ti ti-copy"></i>
              </button>
              <button class="spw-icon-btn spw-del" class:armed={confirmDelSub === a.subdomain}
                onclick={() => deleteSaved(a.subdomain)} disabled={busy}
                title={confirmDelSub === a.subdomain ? t('servers.p2p.pickDeleteConfirm') : t('servers.p2p.pickDelete')}
                aria-label={t('servers.p2p.pickDelete')}>
                <i class="ti {confirmDelSub === a.subdomain ? 'ti-check' : 'ti-trash'}"></i>
                {#if confirmDelSub === a.subdomain}<span class="spw-del-t">{t('servers.p2p.pickDeleteConfirm')}</span>{/if}
              </button>
            </div>
          {:else}
            <div class="spw-hint pad">{t('servers.p2p.pickMineEmpty')}</div>
          {/each}
        </div>
      </div>
    </div>
  </Portal>
{/if}

<style>
  /* Герой-картка адреси */
  .spw {
    position: relative; overflow: hidden;
    display: flex; flex-direction: column; gap: 14px;
    padding: 20px 20px 18px; border-radius: var(--r-lg, 16px);
    background: var(--bg-card); border: 1px solid var(--border);
  }
  .spw-bar { position: absolute; top: 0; left: 0; right: 0; height: 3px; background: var(--hover-strong); transition: background .4s, box-shadow .4s; }
  .spw-bar.on { background: #22c55e; box-shadow: 0 0 18px rgba(34, 197, 94, .55); }
  .spw-head { display: flex; align-items: center; gap: 8px; margin-top: 4px; flex-wrap: wrap; }
  .spw-tt { flex: 1; min-width: 0; display: flex; align-items: center; gap: 8px; }
  .spw-label { font: 700 10.5px/1 var(--font-ui); letter-spacing: .12em; text-transform: uppercase; color: var(--text-mute); }
  .spw-own {
    display: inline-flex; align-items: center; gap: 4px; padding: 3px 8px; border-radius: 999px;
    font: 700 10px/1 var(--font-ui); color: var(--pcol, var(--orange));
    background: color-mix(in srgb, var(--pcol, var(--orange)) 12%, transparent);
  }
  .spw-pill {
    display: inline-flex; align-items: center; gap: 6px; flex-shrink: 0;
    font: 600 11px/1 var(--font-ui); padding: 5px 10px; border-radius: 999px;
    background: var(--bg-input); border: 1px solid var(--border); color: var(--text-mute);
  }
  .spw-dot { width: 6px; height: 6px; border-radius: 50%; background: var(--text-dim); }
  .spw-pill.online { background: rgba(34, 197, 94, .1); border-color: rgba(34, 197, 94, .3); color: #22c55e; }
  .spw-pill.online .spw-dot { background: #22c55e; box-shadow: 0 0 8px #22c55e; animation: spw-pulse 1.6s ease-in-out infinite; }
  @keyframes spw-pulse { 50% { opacity: .4; } }
  .spw-icon-btn {
    width: 28px; height: 28px; border-radius: var(--r-sm); flex-shrink: 0;
    display: flex; align-items: center; justify-content: center;
    background: var(--bg-input); border: 1px solid var(--border); color: var(--text-mute);
    cursor: pointer; transition: background .13s, color .13s;
  }
  .spw-icon-btn:hover { background: var(--hover-strong); color: var(--text); }
  .spw-icon-btn:disabled { opacity: .5; cursor: default; }

  .spw-auto {
    display: inline-flex; align-items: center; gap: 6px;
    padding: 5px 9px; border-radius: 999px; cursor: pointer;
    background: var(--bg-input); border: 1px solid var(--border);
    font: 700 10.5px/1 var(--font-ui); color: var(--text-mute);
    transition: color .14s, border-color .14s, background .14s; flex-shrink: 0;
  }
  .spw-auto:hover { color: var(--text); border-color: var(--border-hi); }
  .spw-auto.on { color: var(--pcol, var(--orange)); border-color: color-mix(in srgb, var(--pcol, var(--orange)) 55%, var(--border)); background: color-mix(in srgb, var(--pcol, var(--orange)) 9%, transparent); }
  .spw-auto:disabled { opacity: .6; cursor: default; }
  .spw-auto-sw { width: 24px; height: 13px; border-radius: 99px; background: var(--hover-strong); position: relative; transition: background .15s; flex-shrink: 0; }
  .spw-auto-sw > span { position: absolute; top: 2px; left: 2px; width: 9px; height: 9px; border-radius: 50%; background: var(--text-dim); transition: transform .15s, background .15s; }
  .spw-auto.on .spw-auto-sw { background: color-mix(in srgb, var(--pcol, var(--orange)) 45%, transparent); }
  .spw-auto.on .spw-auto-sw > span { transform: translateX(11px); background: var(--pcol, var(--orange)); }

  /* Велика адреса */
  .spw-addr { display: flex; align-items: flex-start; gap: 12px; }
  .spw-addr-t {
    flex: 1; min-width: 0; word-break: break-all;
    font: 700 clamp(17px, 2.1vw, 24px)/1.25 var(--font-mono); letter-spacing: -.01em; color: var(--text);
  }
  .spw-copy {
    flex-shrink: 0; width: 40px; height: 40px; border-radius: 12px; font-size: 18px;
    display: grid; place-items: center; cursor: pointer;
    background: var(--bg-input); border: 1px solid var(--border); color: var(--text-mute);
    transition: background .13s, color .13s, transform .1s;
  }
  .spw-copy:hover { background: var(--hover-strong); color: var(--text); }
  .spw-copy:active { transform: scale(.94); }
  .spw-sub { font: 400 11.5px/1.45 var(--font-ui); color: var(--text-mute); margin-top: -6px; }
  .spw-empty { padding: 6px 0 2px; }
  .spw-empty-t { font: 700 15px/1.3 var(--font-ui); color: var(--text); }
  .spw-empty-s { font: 400 12px/1.5 var(--font-ui); color: var(--text-mute); margin-top: 4px; }

  .spw-actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  .spw-hint { font: 400 11px/1.4 var(--font-ui); color: var(--text-mute); display: flex; align-items: center; gap: 5px; }
  .spw-hint.ok { color: #22c55e; }
  .spw-hint.pad { padding: 8px 4px; }
  .spw-btn {
    display: inline-flex; align-items: center; justify-content: center; gap: 6px; cursor: pointer;
    padding: 10px 16px; border-radius: 12px; text-decoration: none;
    background: var(--bg-input); border: 1px solid var(--border); color: var(--text-mute);
    font: 600 12px/1 var(--font-ui); transition: background .13s, color .13s, border-color .13s;
  }
  .spw-btn:hover { background: var(--hover-strong); color: var(--text); }
  .spw-btn.primary { background: var(--pcol, var(--orange)); border-color: transparent; color: #111; font-weight: 700; }
  .spw-btn.primary:hover { filter: brightness(1.08); background: var(--pcol, var(--orange)); }
  .spw-btn.danger { background: transparent; color: #ef4444; border-color: rgba(239, 68, 68, .45); }
  .spw-btn.danger:hover { background: rgba(239, 68, 68, .1); }
  .spw-btn.ghost { background: var(--bg-input); padding: 10px 13px; }
  .spw-btn.ghost.danger-text:hover { color: #ef4444; border-color: rgba(239, 68, 68, .4); background: rgba(239, 68, 68, .08); }
  .spw-btn:disabled { opacity: .55; cursor: default; }
  .spw-setup { display: flex; flex-direction: column; gap: 7px; padding-top: 12px; border-top: 1px solid var(--border); }
  .spw-setup-row { display: flex; gap: 6px; }
  .spw-setup-row .input { flex: 1; min-width: 0; font-size: 12px; padding: 8px 11px; }

  .spw-err {
    display: flex; align-items: center; gap: 6px; font: 400 11px/1.4 var(--font-mono);
    color: var(--red); background: rgba(239, 68, 68, .07); border-radius: 8px; padding: 7px 10px; word-break: break-word;
  }

  /* Окремі картки під героєм */
  .spw-card { display: flex; flex-direction: column; gap: 10px; padding: 16px 18px; border-radius: var(--r-lg, 16px); background: var(--bg-card); border: 1px solid var(--border); }
  .spw-card-h { display: flex; align-items: center; gap: 7px; font: 700 12.5px/1.2 var(--font-ui); color: var(--text); }
  .spw-card-h i { color: #22c55e; font-size: 15px; }
  .spw-member { display: flex; align-items: center; gap: 8px; font: 500 11.5px/1.3 var(--font-mono); color: var(--text-mute); }
  .spw-member i { color: #22c55e; font-size: 12px; }
  .spw-member-ip { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .spw-member-ping { color: #22c55e; }
  .spw-member-bytes { color: var(--text-dim); }

  .spw-note { display: flex; gap: 10px; align-items: flex-start; border-radius: 14px; padding: 13px 15px; font: 400 12px/1.55 var(--font-ui); }
  .spw-note i { font-size: 16px; margin-top: 1px; flex-shrink: 0; }
  .spw-note.warn { color: color-mix(in srgb, #f59e0b 80%, var(--text)); background: rgba(245, 158, 11, .06); border: 1px solid rgba(245, 158, 11, .22); }
  .spw-note.warn i, .spw-note.warn b { color: #f59e0b; }
  .spw-note.caution { padding: 10px 12px; font-size: 11.5px; color: color-mix(in srgb, #fb923c 80%, var(--text)); background: rgba(251, 146, 60, .06); border: 1px solid rgba(251, 146, 60, .18); }
  .spw-note.caution i, .spw-note.caution b { color: #fb923c; }


  /* Вікно «Вибрати зі збережених» */
  .spw-ov { position: fixed; inset: 0; z-index: 420; display: flex; align-items: center; justify-content: center; background: rgba(0, 0, 0, .62); backdrop-filter: blur(3px); }
  .spw-modal { width: min(640px, 94vw); max-height: 82vh; display: flex; flex-direction: column; background: var(--bg-panel); border: 1px solid var(--border); border-radius: 16px; overflow: hidden; box-shadow: 0 18px 60px rgba(0, 0, 0, .55); }
  .spw-modal-h { display: flex; align-items: center; gap: 9px; padding: 16px 18px; font: 700 15px/1 var(--font-ui); color: var(--text); border-bottom: 1px solid var(--border); }
  .spw-modal-h i.ti-list { color: var(--text-mute); }
  .spw-modal-h .spw-icon-btn { margin-left: auto; }
  .spw-modal-b { padding: 8px; overflow-y: auto; display: flex; flex-direction: column; gap: 2px; }
  .spw-sec { padding: 10px 10px 4px; font: 700 10px/1 var(--font-ui); letter-spacing: .1em; text-transform: uppercase; color: var(--text-dim); }
  .spw-del:hover { color: #ef4444; }
  .spw-del.armed { color: #fff; background: #ef4444; width: auto; padding: 0 10px; gap: 6px; }
  .spw-del.armed:hover { background: #dc2626; color: #fff; }
  .spw-del-t { font: 600 12px/1 var(--font-ui); }
  .spw-pr { display: flex; align-items: center; gap: 10px; padding: 10px 12px; border-radius: 12px; border: 1px solid transparent; }
  .spw-pr:hover { background: var(--hover); border-color: var(--border); }
  .spw-pr.cur { background: color-mix(in srgb, var(--pcol, var(--orange)) 7%, transparent); border-color: color-mix(in srgb, var(--pcol, var(--orange)) 28%, transparent); }
  .spw-pr > i.ti-world { color: var(--text-dim); font-size: 16px; }
  .spw-pr.cur > i.ti-world { color: var(--pcol, var(--orange)); }
  .spw-pr-addr { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font: 500 12.5px/1.3 var(--font-mono); color: var(--text); }
  .spw-tag { flex-shrink: 0; display: inline-flex; align-items: center; gap: 4px; padding: 4px 8px; border-radius: 7px; font: 700 9.5px/1 var(--font-ui); letter-spacing: .04em; text-transform: uppercase; color: var(--text-mute); background: var(--bg-input); border: 1px solid var(--border); }
  .spw-tag.cur { color: var(--pcol, var(--orange)); background: color-mix(in srgb, var(--pcol, var(--orange)) 12%, transparent); border-color: transparent; }
  .spw-pr .spw-btn { padding: 6px 11px; font-size: 11px; border-radius: 9px; }
</style>
