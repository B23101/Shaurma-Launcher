<script lang="ts">
  // ── Доступ до P2P-кімнати: режим, списки, співхости ──────────────────
  // Працює з тією ж адресою, що й ServerP2PWidget (книга адрес бекенда, мітка = serverId).
  // ДОСТУП — ЦЕ ПАРАМЕТРИ АДРЕСИ, А НЕ «ОХОРОНЕЦЬ, ЩО ПРАЦЮЄ ЛИШЕ ПІД ЧАС РОБОТИ»:
  // його можна редагувати завжди, і коли сервер зупинено, і коли адреси ще немає.
  // Режим зберігається в самій кімнаті (App.P2PSetAccess), хостинг підніметься вже з ним.
  // Якщо адреси для цього сервера ще немає, її створюють (ClaimP2PAddress) при ПЕРШІЙ
  // зміні доступу — без запуску тунелю. Не при відкритті вкладки: на claim є ліміт і
  // cooldown на акаунт, тож простий перегляд не має витрачати адреси.
  //
  // Людей додають за ІДЕНТИФІКАТОРОМ: повним (UUID) або кастомним (@handle). Кастомний сервер зводить до
  // того самого повного id, тож у списку все одно лежить id. Нік не підходить — він не унікальний.
  // Друзів можна не вводити руками, а вибрати у спільному вікні друзів (FriendPickerModal).
  import { App, on } from '../../wails'
  import { t } from '../../i18n.svelte'
  import { toast } from '../../toast.svelte'
  import { p2pErr } from '../p2pErr'
  import { parsePlayerInput, playerLabel } from '../p2pPlayer'
  import FriendAvatar from '../../friends/FriendAvatar.svelte'
  import FriendPickerModal from '../../friends/FriendPickerModal.svelte'
  import { friendColor } from '../../friends/friendHead'
  import type {
    P2PAccess, P2PACLList, P2PCohostList, P2PHostStatus, P2PApplyResult, P2PEntry, SocialFriend,
  } from '../../types'

  let {
    port = 25565,
    serverId = '',
    refreshKey = 0,
    onclaimed = null,
  }: { port?: number; serverId?: string; refreshKey?: number; onclaimed?: (() => void) | null } = $props()

  const ownerKey = $derived(serverId || `port-${port}`)
  const MODES: { id: P2PAccess; icon: string }[] = [
    { id: 'all', icon: 'ti-world' },
    { id: 'accounts', icon: 'ti-user-circle' },
    { id: 'friends', icon: 'ti-users' },
    { id: 'list', icon: 'ti-list-details' },
  ]

  let tab = $state<'access' | 'cohosts'>('access')
  let sub = $state('')
  let host = $state<P2PHostStatus | null>(null)
  let acl = $state<P2PACLList>({ allow: [], deny: [] })
  let cohosts = $state<P2PCohostList>({ cohosts: [], host: null, max: 0 })
  let busy = $state(false)
  // Режим доступу кімнати, прочитаний З КІМНАТИ (працює і без хостингу).
  let roomAccess = $state<P2PAccess | ''>('')
  let friends = $state<SocialFriend[]>([])
  let idInput = $state('')
  let kindAcl = $state<'allow' | 'deny'>('allow')
  // Що саме вибирає вікно друзів: для списку доступу чи для співхостів.
  let picker = $state<null | 'acl' | 'cohost'>(null)

  const parsed = $derived(parsePlayerInput(idInput))
  const inputBad = $derived(idInput.trim() !== '' && !parsed)

  // Два списки доступу в одному місці, з точними типами (кортеж [kind, items] губить тип елементів).
  const lists = $derived<{ kind: 'allow' | 'deny'; items: P2PACLList['allow'] }[]>([
    { kind: 'allow', items: acl.allow },
    { kind: 'deny', items: acl.deny },
  ])

  const hosting = $derived(!!host?.active)
  // Жива сесія знає режим точно; коли хостингу немає — беремо його з кімнати
  // (roomAccess), а якщо його ще не прочитали — показуємо «усі» як дефолт.
  const current = $derived(((host?.access || roomAccess || 'all') as P2PAccess))

  const friendById = $derived(new Map(friends.map((f) => [f.accountId.toLowerCase(), f])))
  const friendOf = (id: string) => friendById.get(id.toLowerCase())

  // Для вікна друзів: хто вже є в обраному списку / серед співхостів.
  const takenAcl = $derived(new Set((kindAcl === 'allow' ? acl.allow : acl.deny).map((x) => x.id.toLowerCase())))
  const takenCo = $derived(new Set(cohosts.cohosts.map((x) => x.id.toLowerCase())))

  async function loadRoom() {
    try {
      const list = await App.ListP2PAddresses()
      sub = (list.find((a) => (a.label || '') === ownerKey)?.subdomain) ?? ''
    } catch { sub = '' }
    try {
      const hs = await App.ListP2PHosts()
      host = sub ? (hs.find((h) => h.subdomain === sub) ?? null) : null
    } catch { host = null }
    if (!sub) { roomAccess = ''; return }
    // Режим кімнати читаємо НЕЗАЛЕЖНО від хостингу: саме через це перемикач
    // раніше виглядав «мертвим», коли сервер зупинено.
    try { roomAccess = ((await App.P2PRoomAccess(sub)) || '') as P2PAccess | '' }
    catch { /* старий вузол / немає мережі — лишаємо як було */ }
    try { acl = await App.P2PACL(sub) } catch { /* кімнати може не бути на вузлі */ }
    try { cohosts = await App.P2PCohosts(sub) } catch { /* ігноруємо */ }
  }
  async function loadFriends() {
    try { friends = (await App.GetFriendsState())?.friends ?? [] } catch { friends = [] }
  }

  $effect(() => {
    void ownerKey; void refreshKey
    loadRoom()
    loadFriends()
  })
  $effect(() => {
    // Тунель зупинено не користувачем (оренду забрано / співхоста відкликано).
    return on('p2p:host_stopped', (d: { subdomain: string; reason: string }) => {
      toast(t(`servers.p2p.stopped.${d?.reason || 'lease_lost'}`), 'error')
      loadRoom()
    })
  })

  async function guard(fn: () => Promise<void>) {
    busy = true
    try { await fn() } catch (e) { toast(p2pErr(e), 'error') } finally { busy = false }
  }

  // Базова назва адреси для ЦЬОГО сервера: унікальна на сервер (інакше claim повернув би
  // чужу адресу з тією ж базою й перепризначив її). 3–24 символи, a-z 0-9 дефіс.
  const baseName = $derived('s-' + (ownerKey.toLowerCase().replace(/[^a-z0-9]/g, '').slice(-18) || 'server'))

  // ensureSub — адреса цього сервера; якщо її ще немає, створює (без запуску тунелю).
  async function ensureSub(): Promise<string> {
    if (sub) return sub
    const r = await App.ClaimP2PAddress(baseName, ownerKey)
    sub = r.subdomain
    onclaimed?.()
    await loadRoom()
    return sub
  }

  // Результат пачки: показуємо першу відмову людською мовою (не сирий код).
  function report(r: P2PApplyResult): boolean {
    const bad = r.results.find((x) => x.status !== 'ok')
    if (bad) toast(t(`servers.p2p.res.${bad.status}`, { key: bad.key }), 'error')
    return !bad
  }

  // Встановлення режиму: бекенд сам вирішує, як його застосувати — через
  // живу сесію (host-update) чи прямо в запис кімнати, коли хостингу немає.
  const setMode = (m: P2PAccess) => guard(async () => {
    const wasHosting = hosting
    await App.P2PSetAccess(await ensureSub(), m)
    await loadRoom()
    toast(t(wasHosting ? 'servers.p2p.access.saved' : 'servers.p2p.access.savedLater'), 'success')
  })

  const addAcl = (entries: P2PEntry[]) => guard(async () => {
    if (!entries.length) return
    const s = await ensureSub()
    const ok = report(await App.P2PACLApply(s, entries.map((e) => ({ ...e, kind: kindAcl })), []))
    if (ok) idInput = ''
    acl = await App.P2PACL(s)
  })
  const addFromInput = () => { if (parsed) addAcl([parsed.entry]) }
  const removeAcl = (id: string, kind: 'allow' | 'deny') => guard(async () => {
    await App.P2PACLApply(sub, [], [{ id, kind }])
    acl = await App.P2PACL(sub)
  })

  const addCo = (entries: P2PEntry[]) => guard(async () => {
    if (!entries.length) return
    const s = await ensureSub()
    const ok = report(await App.P2PCohostsApply(s, entries, []))
    if (ok) idInput = ''
    cohosts = await App.P2PCohosts(s)
  })
  const removeCo = (id: string) => guard(async () => {
    await App.P2PCohostsApply(sub, [], [{ id }])
    cohosts = await App.P2PCohosts(sub)
  })
  const takeHost = () => guard(async () => {
    const kicked = await App.P2PKick(sub)
    toast(t(kicked ? 'servers.p2p.co.kicked' : 'servers.p2p.co.nobody'), kicked ? 'success' : 'info')
    await loadRoom()
  })

  function onPicked(list: SocialFriend[]) {
    const target = picker
    picker = null
    const entries = list.map((f) => ({ id: f.accountId.toLowerCase() }))
    if (target === 'acl') addAcl(entries)
    else if (target === 'cohost') addCo(entries)
  }
</script>

<div class="spa">
  <div class="spa-tabs" role="tablist">
    <button role="tab" aria-selected={tab === 'access'} class:on={tab === 'access'} onclick={() => (tab = 'access')}>{t('servers.p2p.tab.access')}</button>
    <button role="tab" aria-selected={tab === 'cohosts'} class:on={tab === 'cohosts'} onclick={() => (tab = 'cohosts')}>{t('servers.p2p.tab.cohosts')}</button>
  </div>

  {#if tab === 'access'}
    <!-- Режим можна міняти ЗАВЖДИ (і до запуску сервера, і до появи адреси): він
         зберігається в кімнаті й застосується, коли хостинг підніметься. -->
      <div class="spa-hint">
        <i class="ti ti-info-circle"></i>
        {#if hosting}{t('servers.p2p.access.liveHint')}{:else if !sub}{t('servers.p2p.access.noAddrHint')}{:else}{t('servers.p2p.access.offlineHint')}{/if}
      </div>

      <div class="spa-modes">
        {#each MODES as m}
          <button class="spa-mode" class:on={current === m.id} disabled={busy} onclick={() => setMode(m.id)}>
            <span class="spa-mode-top">
              <i class="ti {m.icon}"></i>
              {#if current === m.id}<i class="ti ti-circle-check-filled spa-mode-ok"></i>{/if}
            </span>
            <b>{t(`servers.p2p.access.${m.id}`)}</b>
            <span>{t(`servers.p2p.access.${m.id}.desc`)}</span>
          </button>
        {/each}
      </div>

      <div class="spa-lists">
        {#each lists as { kind, items } (kind)}
          <div class="spa-list">
            <div class="spa-list-h">
              <b>{t(`servers.p2p.acl.${kind}`)}</b>
              <small>{t(`servers.p2p.acl.${kind}.hint`)}</small>
            </div>
            <div class="spa-list-b">
              {#each items as it (it.id)}
                {@const fr = friendOf(it.id)}
                <div class="spa-row">
                  <FriendAvatar nickname={it.nick || fr?.nickname} color={it.color || fr?.color} status={fr?.presence?.status} size={26} />
                  <span class="spa-who" title={it.id}>
                    <span class="spa-nick" style="color:{friendColor(it.color || fr?.color, 'var(--text)')}">{it.nick || fr?.nickname || playerLabel(it)}</span>
                    {#if it.handle}<small>@{it.handle}</small>{:else}<small>{it.id.slice(0, 8)}…</small>{/if}
                  </span>
                  <button class="spa-x" disabled={busy} title={t('servers.p2p.remove')} aria-label={t('servers.p2p.remove')} onclick={() => removeAcl(it.id, kind)}><i class="ti ti-x"></i></button>
                </div>
              {:else}
                <div class="spa-empty">{t('servers.p2p.acl.empty')}</div>
              {/each}
            </div>
          </div>
        {/each}
      </div>

      <form class="spa-add" class:bad={inputBad} onsubmit={(e) => { e.preventDefault(); addFromInput() }}>
        <i class="ti ti-user spa-add-ic"></i>
        <input bind:value={idInput} placeholder={t('servers.p2p.idPlaceholder')} maxlength="64" spellcheck="false" autocomplete="off" aria-label={t('servers.p2p.idPlaceholder')} />
        <span class="spa-sep"></span>
        <select bind:value={kindAcl} aria-label={t('servers.p2p.acl.which')}>
          <option value="allow">{t('servers.p2p.acl.allow')}</option>
          <option value="deny">{t('servers.p2p.acl.deny')}</option>
        </select>
        <button class="spa-btn primary" type="submit" disabled={busy || !parsed}>{t('servers.p2p.add')}</button>
      </form>
      <div class="spa-under">
        {#if inputBad}
          <span class="spa-bad"><i class="ti ti-alert-circle"></i> {t('servers.p2p.idBad')}</span>
        {:else if parsed?.kind === 'handle'}
          <span class="spa-hint"><i class="ti ti-arrow-right"></i> {t('servers.p2p.idHandleHint', { handle: parsed.value })}</span>
        {:else}
          <span class="spa-hint">{t('servers.p2p.idHelp')}</span>
        {/if}
        <button class="spa-link" type="button" disabled={busy} onclick={() => (picker = 'acl')}><i class="ti ti-users"></i> {t('servers.p2p.fromFriends')}</button>
      </div>
  {:else}
      <div class="spa-hint"><i class="ti ti-info-circle"></i> {t('servers.p2p.co.desc', { n: cohosts.max || 0 })}</div>
      {#if cohosts.host}
        <div class="spa-host">
          <i class="ti ti-server"></i>
          <span>{t('servers.p2p.co.hostingNow', { nick: cohosts.host.nick })}</span>
          <button class="spa-btn danger" disabled={busy} onclick={takeHost}>{t('servers.p2p.co.take')}</button>
        </div>
      {/if}
      <div class="spa-list">
        <div class="spa-list-b">
          {#each cohosts.cohosts as c (c.id)}
            {@const fr = friendOf(c.id)}
            <div class="spa-row">
              <FriendAvatar nickname={c.nick || fr?.nickname} color={c.color || fr?.color} status={fr?.presence?.status} size={26} />
              <span class="spa-who" title={c.id}>
                <span class="spa-nick" style="color:{friendColor(c.color || fr?.color, 'var(--text)')}">{c.nick || fr?.nickname || playerLabel(c)}</span>
                {#if c.handle}<small>@{c.handle}</small>{:else}<small>{c.id.slice(0, 8)}…</small>{/if}
              </span>
              <button class="spa-x" disabled={busy} title={t('servers.p2p.remove')} aria-label={t('servers.p2p.remove')} onclick={() => removeCo(c.id)}><i class="ti ti-x"></i></button>
            </div>
          {:else}
            <div class="spa-empty">{t('servers.p2p.co.empty')}</div>
          {/each}
        </div>
      </div>
      <form class="spa-add" class:bad={inputBad} onsubmit={(e) => { e.preventDefault(); if (parsed) addCo([parsed.entry]) }}>
        <i class="ti ti-user spa-add-ic"></i>
        <input bind:value={idInput} placeholder={t('servers.p2p.idPlaceholder')} maxlength="64" spellcheck="false" autocomplete="off" aria-label={t('servers.p2p.idPlaceholder')} />
        <button class="spa-btn primary" type="submit" disabled={busy || !parsed}>{t('servers.p2p.add')}</button>
      </form>
      <div class="spa-under">
        {#if inputBad}
          <span class="spa-bad"><i class="ti ti-alert-circle"></i> {t('servers.p2p.idBad')}</span>
        {:else}
          <span class="spa-hint">{t('servers.p2p.co.idHelp')}</span>
        {/if}
        <button class="spa-link" type="button" disabled={busy} onclick={() => (picker = 'cohost')}><i class="ti ti-users"></i> {t('servers.p2p.fromFriends')}</button>
      </div>
  {/if}
</div>

{#if picker}
  <FriendPickerModal
    title={picker === 'acl' ? t('servers.p2p.pick.aclTitle', { list: t(`servers.p2p.acl.${kindAcl}`) }) : t('servers.p2p.pick.coTitle')}
    subtitle={picker === 'acl' ? t(`servers.p2p.acl.${kindAcl}.hint`) : t('servers.p2p.pick.coSub')}
    {friends}
    exclude={picker === 'acl' ? takenAcl : takenCo}
    excludedLabel={t('servers.p2p.pick.already')}
    onconfirm={onPicked}
    onclose={() => (picker = null)}
  />
{/if}

<style>
  .spa { display: flex; flex-direction: column; gap: 14px; }
  .spa-tabs { display: flex; padding: 5px; gap: 4px; border-radius: 13px; background: var(--bg-card); border: 1px solid var(--border); }
  .spa-tabs button {
    flex: 1; display: inline-flex; align-items: center; justify-content: center; gap: 6px; padding: 9px 8px; border-radius: 9px; cursor: pointer;
    background: transparent; border: 0; color: var(--text-mute); font: 600 12.5px/1 var(--font-ui); transition: background .14s, color .14s;
  }
  .spa-tabs button:hover { color: var(--text); }
  .spa-tabs button.on { background: var(--hover-strong); color: var(--text); font-weight: 700; }

  .spa-modes { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
  .spa-mode {
    display: flex; flex-direction: column; align-items: flex-start; gap: 4px; padding: 15px 16px; border-radius: 14px; cursor: pointer; text-align: left;
    background: var(--bg-card); border: 1px solid var(--border); color: var(--text-mute); transition: border-color .14s, background .14s;
  }
  .spa-mode:hover:not(:disabled) { border-color: var(--border-hi); }
  .spa-mode-top { width: 100%; display: flex; align-items: center; justify-content: space-between; margin-bottom: 6px; }
  .spa-mode-top > i { font-size: 22px; color: var(--text-dim); }
  .spa-mode b { font: 700 13.5px/1.2 var(--font-ui); color: var(--text); }
  .spa-mode > span:last-child { font: 400 11.5px/1.4 var(--font-ui); }
  .spa-mode.on { border-color: color-mix(in srgb, var(--pcol, var(--orange)) 50%, transparent); background: color-mix(in srgb, var(--pcol, var(--orange)) 8%, var(--bg-card)); }
  .spa-mode.on .spa-mode-top > i:first-child { color: var(--pcol, var(--orange)); }
  .spa-mode-ok { color: var(--pcol, var(--orange)); font-size: 18px !important; }
  .spa-mode:disabled { opacity: .55; cursor: default; }

  .spa-lists { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
  .spa-list { display: flex; flex-direction: column; gap: 10px; padding: 14px; border-radius: 13px; background: var(--bg-card); border: 1px solid var(--border); min-height: 130px; }
  .spa-list-h { display: flex; flex-direction: column; gap: 3px; font: 700 13px/1.2 var(--font-ui); color: var(--text); }
  .spa-list-h small { font: 400 10.5px/1.3 var(--font-ui); color: var(--text-dim); }
  .spa-list-b { flex: 1; display: flex; flex-direction: column; gap: 4px; }
  .spa-row { display: flex; align-items: center; gap: 9px; padding: 5px 6px; border-radius: 9px; }
  .spa-row:hover { background: var(--hover); }
  .spa-who { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
  .spa-nick { font: 700 12px/1.2 var(--font-ui); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .spa-who small { font: 400 10.5px/1.2 var(--font-mono); color: var(--text-dim); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .spa-x { width: 24px; height: 24px; border-radius: 7px; cursor: pointer; background: transparent; border: 1px solid transparent; color: var(--text-dim); }
  .spa-x:hover { color: #ef4444; border-color: rgba(239, 68, 68, .4); background: rgba(239, 68, 68, .08); }
  .spa-empty { flex: 1; display: grid; place-items: center; min-height: 56px; border: 1px dashed var(--border); border-radius: 9px; font: 500 12px/1 var(--font-mono); color: var(--text-dim); }
  .spa-empty.box { padding: 22px; }
  .spa-hint { display: flex; align-items: center; gap: 6px; font: 400 11.5px/1.4 var(--font-ui); color: var(--text-mute); }

  .spa-add { display: flex; align-items: center; gap: 10px; padding: 6px 6px 6px 14px; border-radius: 13px; background: var(--bg-card); border: 1px solid var(--border); transition: border-color .14s; }
  .spa-add:focus-within { border-color: var(--border-hi); }
  .spa-add.bad { border-color: rgba(239, 68, 68, .5); }
  .spa-add-ic { color: var(--text-dim); font-size: 16px; }
  .spa-add input { flex: 1; min-width: 0; background: transparent; border: 0; outline: 0; color: var(--text); font: 500 12.5px/1 var(--font-mono); padding: 9px 0; }
  .spa-add input::placeholder { color: var(--text-dim); font-family: var(--font-ui); }
  .spa-sep { width: 1px; height: 24px; background: var(--border); }
  .spa-add select { background: transparent; border: 0; outline: 0; color: var(--text-mute); font: 600 11.5px/1 var(--font-ui); cursor: pointer; padding: 0 4px; }
  .spa-add select option { background: var(--bg-panel); color: var(--text); }
  .spa-under { display: flex; align-items: center; justify-content: space-between; gap: 10px; margin-top: -6px; flex-wrap: wrap; }
  .spa-bad { display: flex; align-items: center; gap: 6px; font: 500 11.5px/1.4 var(--font-ui); color: #ef4444; }
  .spa-link { display: inline-flex; align-items: center; gap: 6px; padding: 4px 2px; background: none; border: 0; cursor: pointer; font: 600 11.5px/1 var(--font-ui); color: var(--pcol, var(--orange)); }
  .spa-link:hover:not(:disabled) { text-decoration: underline; }
  .spa-link:disabled { opacity: .5; cursor: default; }

  .spa-btn { display: inline-flex; align-items: center; justify-content: center; gap: 5px; cursor: pointer; padding: 9px 15px; border-radius: 10px; background: var(--bg-input); border: 1px solid var(--border); color: var(--text); font: 700 12px/1 var(--font-ui); transition: background .13s, filter .13s; }
  .spa-btn:hover:not(:disabled) { background: var(--hover-strong); }
  .spa-btn.primary { background: var(--pcol, var(--orange)); border-color: transparent; color: #111; }
  .spa-btn.primary:hover:not(:disabled) { filter: brightness(1.08); background: var(--pcol, var(--orange)); }
  .spa-btn.danger { background: transparent; color: #ef4444; border-color: rgba(239, 68, 68, .4); }
  .spa-btn:disabled { opacity: .5; cursor: default; }

  .spa-host { display: flex; align-items: center; gap: 9px; padding: 10px 12px; border-radius: 11px; background: rgba(34, 197, 94, .07); border: 1px solid rgba(34, 197, 94, .25); font: 500 12px/1.3 var(--font-ui); color: var(--text); }
  .spa-host span { flex: 1; }
  .spa-host i { color: #22c55e; }

  @media (max-width: 560px) { .spa-modes, .spa-lists { grid-template-columns: minmax(0, 1fr); } }
</style>
