<script lang="ts">
  import { recentInvitesStore } from './recentInvites'
  import { onMount } from 'svelte'
  import { App } from '../wails'
  import { toast } from '../toast.svelte'
  import { t } from '../i18n.svelte'
  import UiModal from '../UiModal.svelte'
  import FriendAvatar from './FriendAvatar.svelte'
  import { friendColor } from './friendHead'
  import type { LocalServer, TempInvite, SocialFriend } from '../types'

  // Модальне вікно «Запросити у мій світ»: LAN-світ або локальний сервер → тимчасовий P2P-доступ
  // лише для обраних друзів. Доступ живе, поки відкритий світ.
  //
  // Шапка, хрестик, анімація появи/зникнення і футер — СПІЛЬНІ з рештою лаунчера
  // (UiModal): раніше це вікно малювало власну шапку з власним хрестиком, який
  // відрізнявся розміром і позицією від усіх інших модалок.
  //
  // ЧОТИРИ ДЖЕРЕЛА, у порядку зверху вниз (спершу те, що вже відкрито):
  //   1. уже відкриті тимчасові доступи (ListTempInvites) — найтиповіший
  //      випадок: світ відкрито, треба лише докликати друга;
  //   2. P2P-СЕРВЕР, ЯКИЙ УЖЕ ПРАЦЮЄ (ListP2PHosts + ListP2PAddresses) — не
  //      тимчасовий, а постійна кімната сервера: можна поділитися нею, не
  //      піднімаючи жодного нового тунелю; резервованою
  //      SendChatServerInvite-карткою у чаті;
  //   3. LAN-світ на цьому ПК (ScanLocalServers);
  //   4. порт вручну.
  let {
    friends = [] as SocialFriend[],
    preselect = '',
    onclose,
  }: { friends?: SocialFriend[]; preselect?: string; onclose: () => void } = $props()

  // Антиспам повторних запрошень: «друг|адреса» → час останньої відправки (живе, поки відкрито застосунок).
  const recentInvites = recentInvitesStore
  let scanning = $state(false)
  let found = $state<LocalServer[]>([])
  let scanned = $state(false)
  let manualPort = $state('')
  let picked = $state<LocalServer | null>(null)
  let existing = $state<TempInvite[]>([])
  let useExisting = $state('')
  // Серверні P2P-кімнати, які ЗАРАЗ працюють (не тимчасові).
  let serverRooms = $state<{ sub: string; address: string; name: string; players: number; online: boolean; version: string }[]>([])
  let useServer = $state('')
  let chosen = $state<Set<string>>(new Set(preselect ? [preselect] : []))
  let busy = $state(false)
  let err = $state('')
  let confirmClose = $state('')
  let confirmTimer: ReturnType<typeof setTimeout> | undefined

  const invitedSet = $derived(new Set(existing.find((x) => x.id === useExisting)?.guests ?? []))
  const freshFound = $derived(found.filter((s) => !existing.some((e) => e.meta.port === s.port)))
  const newGuests = $derived([...chosen].filter((id) => !invitedSet.has(id)))
  const shareGuests = $derived([...chosen])
  // Скільком друзям реально піде запрошення (залежить від вибраного джерела).
  const willInvite = $derived(useServer ? shareGuests.length : useExisting ? newGuests.length : chosen.size)
  const pendingFriends = $derived(friends.filter((f) => !invitedSet.has(f.accountId)))
  const allSelected = $derived(pendingFriends.length > 0 && pendingFriends.every((f) => chosen.has(f.accountId)))

  // Пошук і фільтр друзів + сортування «спершу ті, хто в мережі».
  let q = $state('')
  let onlyOnline = $state(false)
  const rank = (f: SocialFriend) => {
    const st = f.presence?.status
    return st === 'in_game' ? 0 : st === 'online' ? 1 : st === 'away' ? 2 : 3
  }
  const visibleFriends = $derived(
    friends
      .filter((f) => (!q.trim() || f.nickname.toLowerCase().includes(q.trim().toLowerCase())))
      .filter((f) => !onlyOnline || rank(f) < 3)
      .slice()
      .sort((a, b) => rank(a) - rank(b) || a.nickname.localeCompare(b.nickname)),
  )
  const chosenFriends = $derived(friends.filter((f) => chosen.has(f.accountId) && !invitedSet.has(f.accountId)))
  const sourceChosen = $derived(!!(picked || useExisting || useServer))

  const kindLabel = (k: string) => (k === 'lan' ? 'LAN-світ' : k === 'modded' ? 'Сервер з модами' : 'Сервер')
  const kindIcon = (k: string) => (k === 'lan' ? 'ti-world' : k === 'modded' ? 'ti-puzzle' : 'ti-server-2')
  const statusLabel = (f: SocialFriend) => {
    const s = f.presence?.status
    if (s === 'in_game') return f.presence?.packName ? t('social.profile.playingPack', { pack: f.presence.packName }) : t('social.playing')
    if (s === 'away') return t('social.status.away')
    if (s && s !== 'offline') return t('social.status.online')
    return t('social.status.offline')
  }

  // loadServerRooms — ПОСТІЙНІ P2P-кімнати серверів цього акаунта (не тимчасові).
  // Беремо всі збережені адреси з книги, а не лише активні: картка-запрошення в чаті
  // сама показує онлайн/офлайн, тож запросити друга на вимкнений сервер теж можна.
  // Назва — людська (за id сервера з мітки адреси); активні — зверху.
  async function loadServerRooms() {
    try {
      const [hosts, addrs, servers] = await Promise.all([
        App.ListP2PHosts().catch(() => []),
        App.ListP2PAddresses().catch(() => []),
        App.ListServers().catch(() => []),
      ])
      const byId = new Map<string, any>()
      for (const s of (servers ?? []) as any[]) byId.set(s.id, s)
      const live = new Map<string, any>()
      for (const h of (hosts ?? []) as any[]) if (h?.active && h.subdomain) live.set(String(h.subdomain), h)
      const out: typeof serverRooms = []
      for (const a of (addrs ?? []) as any[]) {
        const sub = String(a?.subdomain || '')
        if (!sub || /^tmp[0-9]-/.test(sub) || a.label === 'temp-invite' || !a.label) continue
        const srv = byId.get(a.label)
        // Мітка вказує на сервер, якого вже немає (видалено) — запрошувати нікуди.
        if (!srv && !/^port-/.test(String(a.label))) continue
        const h = live.get(sub)
        out.push({
          sub,
          address: String(h?.address || '') || `${sub}.play.shaurma-launcher.org`,
          name: srv?.name || `Сервер · порт ${String(a.label).replace('port-', '')}`,
          players: h?.playerCount ?? 0,
          online: !!h,
          version: srv?.mcVersion || '',
        })
      }
      out.sort((a, b) => Number(b.online) - Number(a.online) || a.name.localeCompare(b.name))
      serverRooms = out
      // Нічого іншого не вибрано — одразу підсвічуємо перший постійний сервер.
      if (!useServer && !useExisting && !picked && out.length) useServer = out[0].sub
    } catch { serverRooms = [] }
  }

  onMount(() => {
    App.ListTempInvites().then((l) => {
      existing = l ?? []
      if (existing.length && !useServer) useExisting = existing[0].id
    }).catch(() => {})
    loadServerRooms()
    scan()
  })

  async function scan() {
    scanning = true
    err = ''
    try {
      found = (await App.ScanLocalServers()) ?? []
      const fresh = found.filter((s) => !existing.some((e) => e.meta.port === s.port))
      if (fresh.length === 1 && !picked && !useExisting && !useServer) picked = fresh[0]
    } catch (e) { err = String(e) } finally { scanning = false; scanned = true }
  }

  async function probe() {
    const p = Number(manualPort)
    if (!Number.isInteger(p) || p < 1 || p > 65535) { err = 'Порт — число від 1 до 65535'; return }
    busy = true
    err = ''
    const ex = existing.find((e) => e.meta.port === p)
    if (ex) { useExisting = ex.id; picked = null; busy = false; toast('Для цього порту доступ уже відкрито — вибрано його', 'info'); return }
    try { picked = await App.ProbeLocalServer(p); useExisting = '' } catch (e) { err = String(e) } finally { busy = false }
  }

  function toggle(id: string) {
    if (invitedSet.has(id)) return
    const s = new Set(chosen)
    s.has(id) ? s.delete(id) : s.add(id)
    chosen = s
  }
  function selectAll() {
    chosen = allSelected ? new Set() : new Set(pendingFriends.map((f) => f.accountId))
  }

  async function send() {
    if (chosen.size === 0) { err = 'Оберіть хоча б одного друга'; return }
    if (useExisting && newGuests.length === 0) { err = 'Ці друзі вже мають доступ'; return }
    busy = true
    err = ''
    try {
      if (useServer) {
        // Кімната сервера вже працює — жодного нового тунелю не піднімаємо:
        // шлемо звичайну картку-запрошення з її адресою. Гість зайде прямо,
        // бо доступ у кімнати вже налаштований.
        const room = serverRooms.find((r) => r.sub === useServer)
        if (!room) { err = 'Кімната більше не працює'; return }
        // ttl -1 = постійне запрошення: картка в чаті показує стан сервера (онлайн/офлайн/видалено).
        // Антиспам: те саме запрошення цьому другу не шлемо знову 10 хвилин.
        let sent = 0
        for (const id of chosen) {
          const key = id + '|' + room.address
          if (Date.now() - (recentInvites.get(key) ?? 0) < 10 * 60_000) continue
          await App.SendChatServerInvite(id, room.address, '', room.name, -1)
          recentInvites.set(key, Date.now())
          sent++
        }
        toast(sent > 0 ? `Запрошення надіслано — «${room.name}».` : 'Вже запрошено — повторно не надсилаю.', sent > 0 ? 'success' : 'info')
      } else if (useExisting) {
        await App.InviteToTempInvite(useExisting, newGuests)
        toast('Запрошення надіслано. Доступ працює, поки відкритий світ.', 'success')
      } else if (picked) {
        await App.CreateTempInvite(picked.port, [...chosen], '', '')
        toast('Запрошення надіслано. Доступ працює, поки відкритий світ.', 'success')
      } else { err = 'Оберіть світ або сервер'; return }
      onclose()
    } catch (e) { err = String(e) } finally { busy = false }
  }

  async function closeExisting(e: MouseEvent, id: string) {
    e.preventDefault()
    e.stopPropagation()
    clearTimeout(confirmTimer)
    if (confirmClose !== id) {
      confirmClose = id
      confirmTimer = setTimeout(() => (confirmClose = ''), 3000)
      return
    }
    confirmClose = ''
    await App.CloseTempInvite(id).catch(() => {})
    existing = existing.filter((x) => x.id !== id)
    if (useExisting === id) useExisting = existing[0]?.id ?? ''
    toast('Доступ закрито', 'info')
  }
</script>

<UiModal open title="Запросити друга"
         subtitle={useServer ? 'Друг отримає в чаті картку-запрошення на ваш сервер — вона працює постійно.' : 'Тимчасовий доступ: зайти зможуть лише запрошені друзі, поки світ відкритий.'}
         icon="ti-door-enter" color="var(--orange)" size="xl" bodyClass="inv-ui" onclose={onclose}>
  <div class="inv-body" data-testid="temp-invite-modal">

    <!-- ── Крок 1: що відкриваємо (ліворуч) ── -->
    <section class="inv-step inv-left">
      <header class="inv-h">
        <span class="inv-num" class:done={sourceChosen}>{#if sourceChosen}<i class="ti ti-check"></i>{:else}1{/if}</span>
        <span class="inv-ht">Що відкрити</span>
        <button class="inv-mini inv-h-act" data-testid="temp-invite-scan" onclick={scan} disabled={scanning}>
          <i class="ti {scanning ? 'ti-loader-2 spin' : 'ti-radar-2'}"></i> {scanning ? 'Шукаю…' : 'Оновити'}
        </button>
      </header>

      <div class="inv-src inv-scroll">
        {#if serverRooms.length}
          <div class="inv-group"><i class="ti ti-server-2"></i> Мої сервери <small>постійний P2P</small></div>
          {#each serverRooms as r (r.sub)}
            <button class="inv-card" class:sel={useServer === r.sub} data-testid="temp-invite-server-{r.sub}"
                    onclick={() => { useServer = r.sub; useExisting = ''; picked = null }}>
              <span class="inv-ico srv"><i class="ti ti-server-2"></i></span>
              <span class="inv-main">
                <b>{r.name}</b>
                <small>
                  <span class="inv-dot" class:on={r.online}></span>
                  {r.online ? `Онлайн · гравців ${r.players}` : 'Зупинено'}{r.version ? ' · ' + r.version : ''}
                </small>
              </span>
              <span class="inv-pick" aria-hidden="true"><i class="ti ti-check"></i></span>
            </button>
          {/each}
        {/if}

        {#if existing.length || freshFound.length || (picked && !found.some((f) => f.port === picked?.port))}
          <div class="inv-group"><i class="ti ti-world"></i> Світ на цьому ПК <small>тимчасовий доступ</small></div>
        {/if}

        {#each existing as ex (ex.id)}
          <div class="inv-src-row">
            <button class="inv-card" class:sel={useExisting === ex.id} data-testid="temp-invite-existing-{ex.id}"
                    onclick={() => { useExisting = ex.id; picked = null; useServer = '' }}>
              <span class="inv-ico"><i class="ti {kindIcon(ex.meta.kind)}"></i></span>
              <span class="inv-main">
                <b>{ex.meta.motd || kindLabel(ex.meta.kind)}</b>
                <small>Уже відкрито · порт {ex.meta.port} · запрошено {ex.guests.length}</small>
              </span>
              <span class="inv-pick" aria-hidden="true"><i class="ti ti-check"></i></span>
            </button>
            <button class="inv-mini" class:danger={confirmClose === ex.id} data-testid="temp-invite-close-{ex.id}" onclick={(e) => closeExisting(e, ex.id)}>
              {confirmClose === ex.id ? 'Точно?' : 'Закрити'}
            </button>
          </div>
        {/each}

        {#each freshFound as s (s.port)}
          {@const on = !useExisting && !useServer && picked?.port === s.port}
          <button class="inv-card" class:sel={on} onclick={() => { picked = s; useExisting = ''; useServer = '' }}>
            <span class="inv-ico"><i class="ti {kindIcon(s.kind)}"></i></span>
            <span class="inv-main">
              <b>{s.motd || kindLabel(s.kind)}</b>
              <small>{kindLabel(s.kind)} · порт {s.port}{s.version ? ' · ' + s.version : ''}{s.max ? ` · ${s.players}/${s.max}` : ''}</small>
            </span>
            <span class="inv-pick" aria-hidden="true"><i class="ti ti-check"></i></span>
          </button>
        {/each}

        {#if picked && !found.some((f) => f.port === picked?.port)}
          <div class="inv-card sel static">
            <span class="inv-ico"><i class="ti {kindIcon(picked.kind)}"></i></span>
            <span class="inv-main"><b>{picked.motd || kindLabel(picked.kind)}</b><small>порт {picked.port}{picked.version ? ' · ' + picked.version : ''}</small></span>
            <span class="inv-pick" aria-hidden="true"><i class="ti ti-check"></i></span>
          </div>
        {/if}

        {#if scanning && !found.length && !existing.length && !serverRooms.length}
          <div class="inv-skel"><i></i><i></i></div>
        {:else if scanned && !scanning && freshFound.length === 0 && existing.length === 0 && serverRooms.length === 0}
          <div class="inv-empty">
            <i class="ti ti-world-search"></i>
            <span>Нічого не знайдено. Увімкніть P2P у налаштуваннях сервера, або в Minecraft натисніть Esc → «Відкрити для мережі», або введіть порт нижче.</span>
          </div>
        {/if}

        <div class="inv-port">
          <i class="ti ti-plug-connected"></i>
          <input data-testid="temp-invite-port" inputmode="numeric" placeholder="Порт вручну, напр. 51234" bind:value={manualPort}
                 onkeydown={(e) => { if (e.key === 'Enter') probe() }} />
          <button class="inv-mini" onclick={probe} disabled={busy || !manualPort}>Перевірити</button>
        </div>
      </div>
    </section>

    <!-- ── Крок 2: кого запрошуємо (праворуч, список зі скролом) ── -->
    <section class="inv-step inv-right">
      <header class="inv-h">
        <span class="inv-num" class:done={willInvite > 0}>{#if willInvite > 0}<i class="ti ti-check"></i>{:else}2{/if}</span>
        <span class="inv-ht">Кого запросити <em>{willInvite}</em></span>
        <button class="inv-mini inv-h-act" onclick={selectAll} disabled={pendingFriends.length === 0}>
          <i class="ti {allSelected ? 'ti-square-off' : 'ti-checks'}"></i> {allSelected ? 'Зняти все' : 'Обрати всіх'}
        </button>
      </header>

      {#if friends.length === 0}
        <div class="inv-empty"><i class="ti ti-users"></i><span>Друзів ще немає — спершу додайте їх на сторінці «Друзі».</span></div>
      {:else}
        <div class="inv-tools">
          <label class="inv-search">
            <i class="ti ti-search"></i>
            <input placeholder="Знайти друга" bind:value={q} />
            {#if q}<button class="inv-x" onclick={() => (q = '')} aria-label="Очистити"><i class="ti ti-x"></i></button>{/if}
          </label>
          <button class="inv-chip" class:on={onlyOnline} onclick={() => (onlyOnline = !onlyOnline)}>
            <i class="ti ti-point-filled"></i> У мережі
          </button>
        </div>

        <div class="inv-friends inv-scroll">
          {#each visibleFriends as f (f.accountId)}
            {@const inv = invitedSet.has(f.accountId)}
            {@const on = !inv && chosen.has(f.accountId)}
            <button class="inv-fr" class:on class:invited={inv} class:off={rank(f) === 3} disabled={inv}
                    data-testid="temp-invite-friend-{f.accountId}"
                    title={inv ? 'Уже має доступ' : f.nickname}
                    onclick={() => toggle(f.accountId)}>
              <FriendAvatar nickname={f.nickname} color={f.color} status={f.presence?.status} size={32} />
              <span class="inv-fr-txt">
                <span class="inv-nick" style="color:{friendColor(f.color, 'var(--text)')}">{f.nickname}</span>
                <small>{inv ? 'уже має доступ' : statusLabel(f)}</small>
              </span>
              <span class="inv-fr-ck" aria-hidden="true"><i class="ti {inv ? 'ti-lock' : 'ti-check'}"></i></span>
            </button>
          {:else}
            <div class="inv-empty span"><i class="ti ti-mood-empty"></i><span>Нікого не знайдено</span></div>
          {/each}
        </div>
      {/if}
    </section>

    {#if err}<div class="inv-err" data-testid="temp-invite-error"><i class="ti ti-alert-circle"></i> {err}</div>{/if}
  </div>

  {#snippet footer()}
    <div class="inv-foot">
      <div class="inv-picked" aria-live="polite">
        {#if chosenFriends.length}
          <span class="inv-stack">
            {#each chosenFriends.slice(0, 4) as f (f.accountId)}
              <span class="inv-st"><FriendAvatar nickname={f.nickname} color={f.color} size={24} /></span>
            {/each}
            {#if chosenFriends.length > 4}<span class="inv-st more">+{chosenFriends.length - 4}</span>{/if}
          </span>
        {/if}
        <span class="inv-picked-t">{willInvite ? `Обрано: ${willInvite}` : 'Нікого не обрано'}</span>
      </div>
      <button class="btn btn-ghost" onclick={onclose}>Скасувати</button>
      <button class="btn btn-primary" data-testid="temp-invite-send" onclick={send} disabled={busy || !sourceChosen || willInvite === 0}>
        {#if busy}<i class="ti ti-loader-2 spin"></i>{:else}<i class="ti ti-send"></i>{/if} Запросити
      </button>
    </div>
  {/snippet}
</UiModal>

<style>
  /* Дві колонки: ліворуч «Що відкрити», праворуч «Кого запросити» зі своїм скролом.
     Усі класи тут — з префіксом inv-, а НЕ ti-: «ti-*» належить шрифту іконок Tabler
     (.ti-search, .ti-radio, .ti-tools, .ti-chip, .ti-stack, .ti-x мають власні гліфи
     через ::before — саме вони малювали дивні «прямокутники» й кружок у картці).
     Скролбар: нативний у WebView має білі стрілки, тому в списках він приховано,
     а про прокрутку натякає м'яке затухання країв (.inv-scroll). */
  :global(.inv-ui) { overflow-y: auto; scrollbar-width: none; }
  :global(.inv-ui)::-webkit-scrollbar { display: none; width: 0; height: 0; }
  .inv-scroll {
    scrollbar-width: none;
    -webkit-mask-image: linear-gradient(to bottom, transparent 0, #000 10px, #000 calc(100% - 14px), transparent 100%);
            mask-image: linear-gradient(to bottom, transparent 0, #000 10px, #000 calc(100% - 14px), transparent 100%);
  }
  .inv-scroll::-webkit-scrollbar { display: none; width: 0; height: 0; }
  .inv-body { display: grid; grid-template-columns: minmax(0, 1fr) 290px; grid-template-rows: minmax(0, 1fr) auto; column-gap: 20px; row-gap: 12px; height: min(460px, calc(100vh - 260px)); min-height: 320px; }
  .inv-step { display: flex; flex-direction: column; gap: 10px; min-height: 0; min-width: 0; }
  .inv-right { padding-left: 20px; border-left: 1px solid var(--border); }
  .inv-h { display: flex; align-items: center; gap: 10px; }
  .inv-num {
    flex: 0 0 auto; width: 22px; height: 22px; border-radius: 50%; display: grid; place-items: center;
    font: 800 11px/1 var(--font-ui); color: var(--text-mute);
    background: var(--hover); border: 1px solid var(--border-hi);
    transition: background .2s, color .2s, border-color .2s, transform .25s cubic-bezier(.34,1.56,.64,1);
  }
  .inv-num.done { background: var(--orange); border-color: var(--orange); color: #111; transform: scale(1.06); }
  .inv-num i { font-size: 13px; }
  .inv-ht { font: 700 13px/1 var(--font-ui); color: var(--text); }
  .inv-ht em { font-style: normal; margin-left: 4px; padding: 2px 7px; border-radius: 999px; background: rgba(var(--orange-rgb), .14); color: var(--orange); font: 800 10.5px/1.2 var(--font-ui); }
  .inv-h-act { margin-left: auto; }

  .inv-src { display: grid; gap: 7px; align-content: start; flex: 1; min-height: 0; overflow-y: auto; padding: 8px 2px 10px 0; }
  .inv-src-row { display: flex; gap: 8px; align-items: stretch; }
  .inv-src-row .inv-card { flex: 1; min-width: 0; }
  .inv-card {
    display: flex; align-items: center; gap: 12px; width: 100%; text-align: left;
    padding: 10px 12px; border-radius: 13px; cursor: pointer; color: var(--text);
    border: 1px solid var(--border); background: color-mix(in srgb, var(--bg-input) 55%, transparent);
    transition: background .14s, border-color .14s, transform .14s;
  }
  .inv-card:hover { background: var(--hover); border-color: var(--border-hi); }
  .inv-card:active { transform: scale(.992); }
  .inv-card.sel { border-color: rgba(var(--orange-rgb), .55); background: rgba(var(--orange-rgb), .09); }
  .inv-card.static { cursor: default; }
  .inv-ico { flex: 0 0 auto; width: 34px; height: 34px; border-radius: 10px; display: grid; place-items: center; background: var(--hover); color: var(--text-mute); font-size: 18px; transition: background .14s, color .14s; }
  .inv-ico.srv { color: var(--blue); }
  .inv-card.sel .inv-ico { background: rgba(var(--orange-rgb), .18); color: var(--orange); }
  .inv-main { display: grid; gap: 3px; flex: 1; min-width: 0; }
  .inv-main b { font: 650 13px/1.2 var(--font-ui); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .inv-main small { color: var(--text-mute); font: 400 11px/1.3 var(--font-ui); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .inv-pick { flex: 0 0 auto; width: 20px; height: 20px; border-radius: 50%; display: grid; place-items: center; border: 1.5px solid var(--border-hi); color: transparent; font-size: 12px; transition: all .18s cubic-bezier(.34,1.56,.64,1); }
  .inv-card.sel .inv-pick { background: var(--orange); border-color: var(--orange); color: #111; transform: scale(1.08); }
  .inv-group { display: flex; align-items: center; gap: 6px; padding: 6px 2px 0; font: 800 10px/1 var(--font-ui); letter-spacing: .06em; text-transform: uppercase; color: var(--text-dim); }
  .inv-group:first-child { padding-top: 0; }
  .inv-group i { font-size: 13px; }
  .inv-group small { margin-left: auto; font: 600 10px/1 var(--font-ui); letter-spacing: 0; text-transform: none; color: var(--text-dim); }
  .inv-dot { display: inline-block; width: 7px; height: 7px; margin-right: 5px; border-radius: 50%; background: var(--text-dim); vertical-align: 0; }
  .inv-dot.on { background: #22c55e; box-shadow: 0 0 0 3px rgba(34,197,94,.18); }

  .inv-port { display: flex; align-items: center; gap: 8px; padding: 4px 4px 4px 12px; border-radius: 12px; border: 1px dashed var(--border-hi); }
  .inv-port > i { color: var(--text-mute); font-size: 16px; }
  .inv-port input { flex: 1; min-width: 0; background: transparent; border: 0; outline: 0; color: var(--text); font: 500 12.5px/1 var(--font-ui); padding: 8px 0; }
  .inv-port:focus-within { border-color: rgba(var(--orange-rgb), .5); border-style: solid; }

  .inv-empty { display: flex; align-items: center; gap: 10px; padding: 12px 14px; border-radius: 12px; background: var(--hover); color: var(--text-mute); font: 400 12px/1.4 var(--font-ui); }
  .inv-empty i { font-size: 20px; color: var(--text-dim); flex: 0 0 auto; }
  .inv-empty.span { justify-content: center; }
  .inv-skel { display: grid; gap: 7px; }
  .inv-skel i { height: 54px; border-radius: 13px; background: linear-gradient(90deg, var(--hover), var(--hover-strong), var(--hover)); background-size: 200% 100%; animation: inv-sh 1.1s linear infinite; }
  @keyframes inv-sh { to { background-position: -200% 0; } }

  .inv-tools { display: flex; gap: 8px; flex: 0 0 auto; }
  .inv-search { flex: 1; min-width: 0; display: flex; align-items: center; gap: 8px; padding: 0 10px; border-radius: 11px; background: var(--bg-input); border: 1px solid var(--border); }
  .inv-search:focus-within { border-color: rgba(var(--orange-rgb), .5); }
  .inv-search i { color: var(--text-mute); font-size: 15px; }
  .inv-search input { flex: 1; min-width: 0; background: transparent; border: 0; outline: 0; color: var(--text); font: 500 12.5px/1 var(--font-ui); padding: 9px 0; }
  .inv-x { background: transparent; border: 0; color: var(--text-mute); cursor: pointer; display: grid; place-items: center; padding: 2px; border-radius: 6px; }
  .inv-x:hover { color: var(--text); background: var(--hover); }
  .inv-chip { display: inline-flex; align-items: center; gap: 4px; padding: 0 12px; border-radius: 11px; cursor: pointer; border: 1px solid var(--border); background: transparent; color: var(--text-mute); font: 600 11.5px/1 var(--font-ui); transition: all .14s; }
  .inv-chip i { font-size: 14px; color: var(--text-dim); transition: color .14s; }
  .inv-chip:hover { background: var(--hover); color: var(--text); }
  .inv-chip.on { border-color: rgba(34,197,94,.5); background: rgba(34,197,94,.1); color: var(--text); }
  .inv-chip.on i { color: #22c55e; }

  /* Друзі: ОДНАКОВІ плитки (фіксована висота), 2 колонки, статус-крапка на голові, галочка — праворуч. */
  .inv-friends { display: flex; flex-direction: column; gap: 7px; flex: 1; min-height: 0; overflow-y: auto; padding: 8px 2px 10px 0; }
  .inv-fr {
    display: flex; align-items: center; gap: 10px; min-width: 0; flex: 0 0 52px; height: 52px; box-sizing: border-box; text-align: left;
    padding: 0 12px 0 10px; border-radius: 13px; border: 1px solid var(--border);
    background: color-mix(in srgb, var(--bg-input) 55%, transparent); color: var(--text); cursor: pointer;
    transition: background .14s, border-color .14s, transform .14s;
  }
  .inv-fr:hover:not(:disabled) { background: var(--hover); border-color: var(--border-hi); }
  .inv-fr:active:not(:disabled) { transform: scale(.985); }
  .inv-fr.on { background: rgba(var(--orange-rgb), .1); border-color: rgba(var(--orange-rgb), .55); }
  .inv-fr.off:not(.on) { opacity: .72; }
  .inv-fr.invited { opacity: .5; cursor: default; border-style: dashed; }
  .inv-fr-txt { display: grid; gap: 3px; min-width: 0; flex: 1; }
  .inv-nick { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font: 650 12.5px/1.2 var(--font-ui); }
  .inv-fr-txt small { color: var(--text-mute); font: 400 10.5px/1.2 var(--font-ui); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .inv-fr-ck { flex: 0 0 auto; width: 20px; height: 20px; border-radius: 50%; display: grid; place-items: center; border: 1.5px solid var(--border-hi); color: transparent; font-size: 12px; transition: all .18s cubic-bezier(.34,1.56,.64,1); }
  .inv-fr.on .inv-fr-ck { background: var(--orange); border-color: var(--orange); color: #111; transform: scale(1.08); }
  .inv-fr.invited .inv-fr-ck { color: var(--text-mute); border-style: dashed; }

  .inv-mini { display: inline-flex; align-items: center; gap: 5px; font: 600 11px/1 var(--font-ui); padding: 7px 11px; border-radius: 9px; border: 1px solid var(--border); background: var(--hover); color: var(--text); cursor: pointer; transition: background .12s, border-color .12s; white-space: nowrap; }
  .inv-mini:hover:not(:disabled) { background: var(--hover-strong); border-color: var(--border-hi); }
  .inv-mini:disabled { opacity: .5; cursor: default; }
  .inv-mini.danger { border-color: rgba(239,68,68,.5); color: var(--red); background: transparent; }

  .inv-err { grid-column: 1 / -1; display: flex; align-items: center; gap: 8px; padding: 10px 12px; border-radius: 11px; background: rgba(239,68,68,.1); border: 1px solid rgba(239,68,68,.3); color: var(--red); font: 500 12.5px/1.4 var(--font-ui); }

  .inv-foot { display: flex; align-items: center; gap: 8px; width: 100%; }
  .inv-picked { display: flex; align-items: center; gap: 10px; margin-right: auto; min-width: 0; }
  .inv-picked-t { font: 600 12px/1 var(--font-ui); color: var(--text-mute); white-space: nowrap; }
  .inv-stack { display: inline-flex; }
  .inv-st { margin-left: -8px; border-radius: 9px; box-shadow: 0 0 0 2px var(--bg-window); display: inline-flex; animation: inv-pop .24s cubic-bezier(.34,1.56,.64,1); }
  .inv-st:first-child { margin-left: 0; }
  .inv-st.more { width: 24px; height: 24px; border-radius: 9px; background: var(--hover-strong); color: var(--text); font: 800 9.5px/24px var(--font-ui); justify-content: center; }
  @keyframes inv-pop { from { transform: scale(.4); opacity: 0; } to { transform: none; opacity: 1; } }
  @media (max-width: 700px) {
    .inv-body { grid-template-columns: minmax(0, 1fr); grid-template-rows: none; height: auto; }
    .inv-right { padding-left: 0; border-left: 0; }
    .inv-src { overflow: visible; }
    .inv-friends { flex: 0 0 auto; max-height: 260px; }
  }
</style>
