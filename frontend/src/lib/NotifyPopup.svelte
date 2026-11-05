<script lang="ts">
  // ── Віджет сповіщень у трей ──────────────────────────────────────────────
  //
  // Живе в ОКРЕМОМУ вікні (frontend/notify.html), яке створює
  // backend/notify_window.go. Завдання сторінки — показати стос карток і
  // повідомити бекенд про фактичну висоту контенту (App.SetNotifyPopupSize),
  // бо саме він розміщує вікно в правому нижньому куті та ховає його, коли
  // стос порожній.
  //
  // КОЖЕН ТИП ПОДІЇ має свій вигляд: колір, іконку, підпис, мітки й кнопки
  // (див. KINDS та actionsOf). Тип приходить з бекенду в полі event.
  //
  // СТОС: нова картка зʼявляється ВНИЗУ (ближче до кута), старі зсуваються
  // вгору; максимум MAX карток, найстаріша (верхня) зникає. Повідомлення від
  // одного співрозмовника НЕ замінюють одне одного.
  //
  // ПРОДУКТИВНІСТЬ: жодного backdrop-filter/filter, анімації лише transform/
  // opacity і скінченні (без нескінченних циклів), таймер-смужка — чиста CSS-
  // анімація (пауза при наведенні), без rAF і інтервалів. Іконки збірок
  // кешує спільний packAssets, голови — одна спроба на нік.
  //
  // БЕЗПЕКА ВІДОБРАЖЕННЯ: весь текст виводиться лише інтерполяцією (без
  // {@html}); бекенд уже санітизує й обрізає поля.
  import GroupAvatar from './groups/GroupAvatar.svelte'
  import { onMount, tick } from 'svelte'
  import { App, on } from './wails'
  import { t, setLanguage } from './i18n.svelte'
  import { syncAppearance } from './theme.svelte'
  import { packAsset } from './packAssets.svelte'
  import type { SocialNotifyPayload } from './types'
  import notifySoundUrl from '../assets/sounds/chat-notify.mp3'
  import appIcon from '../assets/images/appicon.png'

  // Запасний час показу (основне — durationMs із самої події).
  let ttl = 7000
  const TTL_MIN = 3000
  const TTL_MAX = 60000
  const MAX = 3          // має збігатися з notifyMaxStack у бекенді
  const GONE = 260       // тривалість анімації зникнення (як transition у стилях)
  const POPUP_W = 360    // фіксована ширина вікна

  // ── Вигляд кожного типу: колір, іконка, колір тексту на заливці ─────────
  interface KindDef { color: string; icon: string; on: string }
  const KINDS: Record<string, KindDef> = {
    chat:           { color: '#38bdf8', icon: 'ti-message-circle', on: '#06222f' },
    group:          { color: '#818cf8', icon: 'ti-users-group',           on: '#ffffff' },
    info:           { color: '#fb923c', icon: 'ti-bell',           on: '#2a1200' },
    friend_request: { color: '#84cc16', icon: 'ti-user-plus',             on: '#1a2a00' },
    download_done:  { color: '#22c55e', icon: 'ti-circle-check-filled',   on: '#04230f' },
    install_failed: { color: '#ef4444', icon: 'ti-alert-octagon',  on: '#ffffff' },
    action_needed:  { color: '#f97316', icon: 'ti-hand-stop',             on: '#2a1000' },
    share_build:    { color: '#a855f7', icon: 'ti-package',               on: '#ffffff' },
    share_server:   { color: '#3b82f6', icon: 'ti-server-2',              on: '#ffffff' },
    share_mod:      { color: '#ec4899', icon: 'ti-puzzle',         on: '#ffffff' },
    invite_temp:    { color: '#eab308', icon: 'ti-door-enter',            on: '#2a2000' },
    invite_perm:    { color: '#06b6d4', icon: 'ti-infinity',              on: '#032329' },
    server_started: { color: '#14b8a6', icon: 'ti-player-play-filled',    on: '#032420' },
    server_stopped: { color: '#94a3b8', icon: 'ti-player-stop',    on: '#101722' },
  }
  // Події, де в заголовку — нік друга (потрібна його голова).
  const FRIEND_EVENTS = new Set(['chat', 'friend_request', 'share_build', 'share_server', 'share_mod', 'invite_temp', 'invite_perm', 'server_started', 'server_stopped'])

  interface Card {
    key: string
    kind: string
    ev: string                 // ключ у KINDS
    title: string
    body: string
    sub: string
    chips: string[]
    iconUrl: string            // data:/packAsset-джерело (мініатюра предмета)
    packIcon: string           // icon_url збірки (резолвиться через packAsset)
    iconName: string           // ti-… глиф предмета
    color: string
    packId: string
    ref: string
    groupIcon?: string
    groupColor?: string
    accountId: string
    ts: number
    leaving: boolean
    timer?: number
    left: number
    started: number
    total: number              // повний час показу (для смужки)
    busy: string               // id кнопки, що виконується
    done: string               // підсумок дії («Друга додано»)
    doneOk: boolean
    err: string
    copied: boolean
  }

  let cards = $state<Card[]>([])
  let stack: HTMLDivElement | undefined = $state()
  let hovering = $state(false)
  let lastW = -1
  let lastH = -1

  const HIDDEN = /[\u0000-\u001f\u007f-\u009f​-‏‪-‮⁦-⁩﻿]/g
  const clean = (s?: string) => (s ?? '').replace(HIDDEN, '').trim()

  const ttlOf = (p: SocialNotifyPayload) => {
    const ms = typeof p.durationMs === 'number' && isFinite(p.durationMs) && p.durationMs > 0 ? p.durationMs : ttl
    return Math.min(TTL_MAX, Math.max(TTL_MIN, ms))
  }

  // ── Голови гравців (одна спроба на нік) ─────────────────────────────────
  const heads = $state<Record<string, string>>({})
  const headSeen = new Set<string>()
  function wantHead(name: string) {
    if (!name || headSeen.has(name)) return
    headSeen.add(name)
    App.GetAccountHead('', name, false)
      .then((h: string) => { if (typeof h === 'string' && h.startsWith('data:image/')) heads[name] = h })
      .catch(() => {})
  }

  // ── Іконки/кольори збірок: один запит на життя вікна ────────────────────
  type PackLite = { name: string; icon_url?: string; color?: string; icon?: string }
  let packsP: Promise<Record<string, PackLite>> | null = null
  function packsById() {
    packsP ??= App.GetPackIndex()
      .then((list) => Object.fromEntries((list ?? []).map((p: any) => [p.id, p])))
      .catch(() => ({}))
    return packsP
  }
  async function resolvePack(c: Card) {
    const p = (await packsById())[c.packId]
    if (!p) return
    if (p.icon_url) c.packIcon = p.icon_url
    // Колір збірки НЕ застосовуємо: колір картки — це тип події (червоний = помилка тощо).
    if (p.icon && /^ti-[a-z0-9-]+$/.test(p.icon)) c.iconName = c.iconName || p.icon
    void sync()
  }

  // ── Мод: справжня назва й іконка (один запит, кеш на бекенді/в мережі) ──
  async function resolveMod(c: Card) {
    const [source, id] = c.ref.split(':')
    if (!source || !id) return
    try {
      const d: any = await App.GetBrowserProjectDetail(source, id)
      if (!d) return
      if (d.name) c.sub = clean(d.name).slice(0, 60)
      if (typeof d.iconUrl === 'string' && /^https:\/\//.test(d.iconUrl)) c.iconUrl = d.iconUrl
      const tags: string[] = [...(d.loaders ?? [])].slice(0, 2)
      if (tags.length) c.chips = tags
      void sync()
    } catch { /* без мережі лишається slug */ }
  }

  // ── Звук ────────────────────────────────────────────────────────────────
  let notifyAudio: HTMLAudioElement | null = null
  async function playSound() {
    try {
      const m = await App.GetChatMuteState()
      if (m?.sound) return
      notifyAudio ??= new Audio(notifySoundUrl)
      notifyAudio.volume = 0.7
      notifyAudio.currentTime = 0
      await notifyAudio.play()
    } catch { /* автоплей заблоковано або файл недоступний */ }
  }

  // ── Додавання/видалення карток ──────────────────────────────────────────
  function evOf(p: SocialNotifyPayload): string {
    if (p.event && KINDS[p.event]) return p.event
    if (p.kind === 'invite') return 'friend_request'
    if (p.kind === 'group') return 'group'
    if (p.kind === 'info') return 'info'
    return 'chat'
  }
  const ACTIONABLE = new Set(['friend_request', 'share_build', 'share_server', 'share_mod', 'invite_temp', 'invite_perm', 'install_failed', 'action_needed', 'download_done'])

  function add(p: SocialNotifyPayload | null | undefined) {
    if (!p) return
    const title = clean(p.title)
    const body = clean(p.body)
    if (!title && !body && !p.event) return
    const key = p.id || `${p.kind}-${p.accountId}-${p.ts}`
    if (cards.some(c => c.key === key)) return
    void playSound()

    const ev = evOf(p)
    // Картки з кнопками висять довше: їх треба встигнути натиснути.
    const base = ttlOf(p)
    const total = Math.min(TTL_MAX, Math.round(base * (ev === 'install_failed' || ev === 'action_needed' ? 2 : ACTIONABLE.has(ev) ? 1.6 : 1)))
    const color = typeof p.color === 'string' && /^#[0-9a-fA-F]{6}$/.test(p.color) ? p.color : (ev === 'group' && p.groupColor && /^#[0-9a-fA-F]{6}$/.test(p.groupColor) ? p.groupColor : '')
    const fresh: Card = {
      key, kind: p.kind || 'chat', ev, title, body,
      sub: clean(p.sub), chips: (p.chips ?? []).map(clean).filter(Boolean).slice(0, 4),
      iconUrl: typeof p.icon === 'string' && p.icon.startsWith('data:image/') ? p.icon : '',
      packIcon: '', iconName: p.iconName ?? '', color, packId: p.packId ?? '', ref: p.ref ?? '',
      groupIcon: p.groupIcon, groupColor: p.groupColor,
      accountId: p.accountId || '', ts: p.ts || Date.now(),
      leaving: false, left: total, started: 0, total,
      busy: '', done: '', doneOk: true, err: '', copied: false,
    }
    // Нова картка — ВНИЗУ стосу; старі зсуваються вгору.
    cards = [...cards, fresh]
    // Переповнення: витісняємо найстаріші активні (з початку списку).
    const active = cards.filter(c => !c.leaving)
    for (let i = 0; i < active.length - MAX; i++) dismiss(active[i].key)

    if (FRIEND_EVENTS.has(ev) && fresh.kind !== 'info') wantHead(title)
    const live = cards[cards.length - 1]
    if (live.packId) void resolvePack(live)
    if (ev === 'share_mod' && live.ref) void resolveMod(live)
    armAll()
    void sync()
  }

  function arm(c: Card) {
    if (c.timer || c.leaving || hovering) return
    c.started = Date.now()
    c.timer = window.setTimeout(() => dismiss(c.key), c.left)
  }
  function armAll() { for (const c of cards) arm(c) }
  function pauseAll() {
    hovering = true
    for (const c of cards) {
      if (!c.timer) continue
      window.clearTimeout(c.timer)
      c.timer = undefined
      c.left = Math.max(900, c.left - (Date.now() - c.started))
    }
  }
  function resumeAll() { hovering = false; armAll() }

  function dismiss(key: string) {
    const c = cards.find(x => x.key === key)
    if (!c || c.leaving) return
    if (c.timer) { window.clearTimeout(c.timer); c.timer = undefined }
    c.leaving = true
    window.setTimeout(() => {
      cards = cards.filter(x => x.key !== key)
      void sync()
    }, GONE)
  }

  // ── Розмір вікна ────────────────────────────────────────────────────────
  async function sync() {
    await tick()
    const h = cards.length === 0 ? 0 : Math.ceil(stack?.getBoundingClientRect().height || 0)
    const w = POPUP_W
    if (w === lastW && h === lastH) return
    lastW = w
    lastH = h
    App.SetNotifyPopupSize(w, h).catch(() => {})
  }

  // ── Кнопки ──────────────────────────────────────────────────────────────
  interface Act { id: string; label: string; icon: string; primary?: boolean; keep?: boolean; run: () => Promise<void> }

  function actionsOf(c: Card): Act[] {
    const nav = (action: string, ref = c.ref, acc = c.accountId) => async () => { await App.NotifyAction(action, ref, acc) }
    const copy = (text: string) => async () => {
      await App.CopyToClipboard(text)
      c.copied = true
      window.setTimeout(() => { c.copied = false }, 1600)
    }
    switch (c.ev) {
      case 'friend_request':
        return [
          { id: 'accept', label: t('social.notify.a.accept'), icon: 'ti-check', primary: true, run: async () => { await App.NotifyAction('accept-friend', c.ref, ''); finish(c, t('social.notify.a.accepted'), true) }, keep: true },
          { id: 'decline', label: t('social.notify.a.decline'), icon: 'ti-x', run: async () => { await App.NotifyAction('decline-friend', c.ref, ''); finish(c, t('social.notify.a.declined'), false) }, keep: true },
        ]
      case 'share_build':
      case 'share_server':
        return [{ id: 'dl', label: t('social.notify.a.download'), icon: 'ti-download', primary: true, run: nav('share-offer') }]
      case 'share_mod':
        return [{ id: 'view', label: t('social.notify.a.view'), icon: 'ti-eye', primary: true, run: nav('mod-view') }]
      case 'invite_temp':
      case 'invite_perm':
        return [
          { id: 'play', label: t('social.notify.a.launch'), icon: 'ti-player-play-filled', primary: true, run: nav('invite-play') },
          { id: 'copy', label: c.copied ? t('social.notify.a.copied') : t('social.notify.a.copy'), icon: c.copied ? 'ti-check' : 'ti-copy', keep: true, run: copy(c.ref) },
        ]
      case 'server_started':
        return c.ref ? [{ id: 'copy', label: c.copied ? t('social.notify.a.copied') : t('social.notify.a.copy'), icon: c.copied ? 'ti-check' : 'ti-copy', keep: true, run: copy(c.ref) }] : []
      case 'download_done':
        return c.packId ? [{ id: 'open', label: t('social.notify.a.open'), icon: 'ti-player-play-filled', primary: true, run: nav('open-pack', c.packId, '') }] : []
      case 'install_failed':
        return [
          ...(c.packId ? [{ id: 'open', label: t('social.notify.a.open'), icon: 'ti-external-link', primary: true, run: nav('open-pack', c.packId, '') } as Act] : []),
          { id: 'copy', label: c.copied ? t('social.notify.a.copied') : t('social.notify.a.copy'), icon: c.copied ? 'ti-check' : 'ti-copy', keep: true, run: copy(c.ref || c.body) },
        ]
      case 'action_needed':
        return [{ id: 'fix', label: t('social.notify.a.fix'), icon: 'ti-hand-click', primary: true, run: nav('open-pack', c.packId, '') }]
    }
    return []
  }

  async function runAct(c: Card, a: Act) {
    if (c.busy) return
    c.busy = a.id
    c.err = ''
    try {
      await a.run()
      if (!a.keep) dismiss(c.key)
    } catch (e: any) {
      c.err = clean(typeof e === 'string' ? e : e?.message) || t('social.notify.a.failed')
      void sync()
    } finally { c.busy = '' }
  }

  // Підсумок дії на самій картці, потім плавне зникнення.
  function finish(c: Card, text: string, ok: boolean) {
    c.done = text
    c.doneOk = ok
    if (c.timer) window.clearTimeout(c.timer)
    c.timer = window.setTimeout(() => dismiss(c.key), 1500)
    void sync()
  }

  function open(c: Card) {
    switch (c.ev) {
      case 'info': dismiss(c.key); return
      case 'chat': case 'group':
        App.OpenChatFromNotification(c.accountId).catch(() => {}); dismiss(c.key); return
      case 'friend_request':
        App.OpenChatFromNotification('').catch(() => {}); dismiss(c.key); return
    }
    const first = actionsOf(c).find(a => a.primary)
    if (first) void runAct(c, first)
    else if (c.accountId) { App.NotifyAction('open-friends', '', c.accountId).catch(() => {}); dismiss(c.key) }
    else dismiss(c.key)
  }

  // ── Тексти ──────────────────────────────────────────────────────────────
  const kindLabel = (c: Card) => t(`social.notify.k.${c.ev}`)
  function lineOf(c: Card): string {
    switch (c.ev) {
      case 'friend_request': return c.body || t('social.notify.friendRequest')
      case 'share_build': case 'share_server': case 'share_mod':
      case 'invite_temp': case 'invite_perm': case 'server_started': case 'server_stopped':
        return c.ev === 'server_stopped' && c.body === 'crashed' ? t('social.notify.b.crashed') : t(`social.notify.b.${c.ev}`)
    }
    return c.body
  }
  // Додатковий рядок: для запрошень — адреса моноширинним, для передач —
  // коментар друга (якщо є).
  const monoOf = (c: Card) => (c.ev === 'invite_temp' || c.ev === 'invite_perm') ? c.body : ''
  const quoteOf = (c: Card) => (c.ev === 'share_build' || c.ev === 'share_server' || c.ev === 'share_mod') ? c.body : ''

  const kd = (c: Card): KindDef => KINDS[c.ev] ?? KINDS.chat
  const colorOf = (c: Card) => c.color || kd(c).color
  // Мініатюра предмета: власна картинка → іконка збірки.
  const thumbOf = (c: Card) => c.iconUrl || packAsset(c.packIcon) || ''

  onMount(() => {
    let offAppearance: (() => void) | undefined
    syncAppearance().then(({ settings: s, off }) => {
      offAppearance = off
      if (s?.language) setLanguage(s.language)
      const sec = typeof (s as any)?.notifyDurationSec === 'number' && isFinite((s as any).notifyDurationSec)
        ? Math.round((s as any).notifyDurationSec) : 7
      ttl = Math.min(TTL_MAX, Math.max(TTL_MIN, sec * 1000))
    }).catch(() => {})
    const offs = [on('notify:push', (p: SocialNotifyPayload) => add(p))]
    // Події, що прийшли до створення вікна, лежать у черзі бекенда.
    App.TakeNotifyPopup()
      .then((list: SocialNotifyPayload[]) => { (list ?? []).forEach(add) })
      .catch(() => {})
    return () => { offs.forEach(off => off()); offAppearance?.() }
  })
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  class="np"
  class:hov={hovering}
  bind:this={stack}
  onpointerenter={pauseAll}
  onpointerleave={resumeAll}
>
  {#each cards as c (c.key)}
    {@const k = kd(c)}
    {@const thumb = thumbOf(c)}
    {@const acts = c.done ? [] : actionsOf(c)}
    {@const mono = monoOf(c)}
    {@const quote = quoteOf(c)}
    <div
      class="np-card ev-{c.ev}"
      class:leaving={c.leaving}
      class:done={!!c.done}
      style="--c:{colorOf(c)}; --on:{k.on}; --ttl:{c.total}ms"
      role="button"
      tabindex="0"
      aria-label={kindLabel(c)}
      onclick={() => open(c)}
      onkeydown={(e) => { if (e.target === e.currentTarget && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); open(c) } }}
    >
      <div class="np-row">
        <span class="np-tile" class:glyph={!thumb && !(c.kind === 'group') && !(heads[c.title] && (c.ev === 'chat' || c.ev === 'friend_request'))}>
          {#if c.kind === 'group'}
            <GroupAvatar title={c.title} icon={c.groupIcon} color={c.groupColor} size={44} />
          {:else if thumb}
            <img class="np-img" src={thumb} alt="" loading="lazy" />
          {:else if heads[c.title] && (c.ev === 'chat' || c.ev === 'friend_request')}
            <img class="np-img np-head" src={heads[c.title]} alt="" />
          {:else if c.ev === 'info'}
            <img class="np-app" src={appIcon} alt="" />
          {:else}
            <i class="ti {c.iconName || k.icon} np-glyph"></i>
          {/if}
          <span class="np-badge"><i class="ti {k.icon}"></i></span>
        </span>

        <div class="np-text">
          <div class="np-kicker"><span>{kindLabel(c)}</span></div>
          <div class="np-title">
            {#if FRIEND_EVENTS.has(c.ev) && c.ev !== 'chat' && c.ev !== 'friend_request' && heads[c.title]}<img class="np-mini" src={heads[c.title]} alt="" />{/if}<span class="np-name">{c.title}</span>
          </div>
          {#if c.done}
            <div class="np-line np-doneline" class:bad={!c.doneOk}><i class="ti {c.doneOk ? 'ti-circle-check-filled' : 'ti-circle-x'}"></i> {c.done}</div>
          {:else}
            {#if lineOf(c)}<div class="np-line" class:err={c.ev === 'install_failed'}>{lineOf(c)}</div>{/if}
            {#if c.sub}<div class="np-sub">{c.sub}</div>{/if}
            {#if quote}<div class="np-quote">“{quote}”</div>{/if}
            {#if mono}<div class="np-mono">{mono}</div>{/if}
            {#if c.chips.length}
              <div class="np-chips">{#each c.chips as ch}<span class="np-chip">{ch}</span>{/each}</div>
            {/if}
            {#if c.err}<div class="np-err"><i class="ti ti-alert-circle"></i> {c.err}</div>{/if}
          {/if}
        </div>

        <button
          class="np-x"
          aria-label={t('social.notify.dismiss')}
          title={t('social.notify.dismiss')}
          onclick={(e) => { e.stopPropagation(); dismiss(c.key) }}
        >
          <i class="ti ti-x"></i>
        </button>
      </div>

      {#if acts.length}
        <div class="np-acts">
          {#each acts as a (a.id)}
            <button
              class="np-btn"
              class:pri={a.primary}
              class:ok={a.id === 'copy' && c.copied}
              disabled={!!c.busy}
              onclick={(e) => { e.stopPropagation(); void runAct(c, a) }}
            >
              {#if c.busy === a.id}<i class="ti ti-loader-2 np-spin"></i>{:else}<i class="ti {a.icon}"></i>{/if}
              {a.label}
            </button>
          {/each}
        </div>
      {/if}

      {#if !c.done}<span class="np-bar"></span>{/if}
    </div>
  {/each}
</div>

<style>
  /* Вікно ПРОЗОРЕ: єдиного фону під стосом немає — видно лише самі картки
     (з легкою тінню). Відступ .np потрібен, щоб тінь не обрізалась краєм вікна. */
  :global(html), :global(body) { background: transparent !important; margin: 0; }
  :global(#app) { background: transparent !important; height: auto !important; display: block !important; }

  /* Контейнер — ЄДИНЕ джерело висоти вікна. */
  .np {
    box-sizing: border-box; width: 360px;
    display: flex; flex-direction: column; gap: 8px; padding: 10px 12px 12px;
    background: transparent; overflow: hidden;
  }

  .np-card {
    --c: #fb923c; --on: #2a1200;
    position: relative; display: flex; flex-direction: column; gap: 9px;
    padding: 11px 36px 12px 11px; border-radius: 14px; overflow: hidden;
    cursor: pointer; contain: layout paint;
    /* Колірна «аура» типу: заливка зліва + загальний відтінок. */
    background:
      linear-gradient(100deg, color-mix(in srgb, var(--c) 20%, transparent) 0%, transparent 62%),
      color-mix(in srgb, var(--c) 6%, var(--bg-card));
    border: 1px solid color-mix(in srgb, var(--c) 42%, var(--border-hi));
    box-shadow: 0 8px 20px rgba(0, 0, 0, .38);
    animation: np-in .5s cubic-bezier(.2, 1.15, .3, 1) both;
    transition: opacity .24s ease, transform .24s ease, border-color .15s;
  }
  /* Кольорова планка зліва. */
  .np-card::before {
    content: ''; position: absolute; left: 0; top: 0; bottom: 0; width: 4px; background: var(--c);
  }
  .np-card:hover { border-color: color-mix(in srgb, var(--c) 75%, var(--border-hi)); }
  .np-card.leaving { opacity: 0; transform: translateX(34px); pointer-events: none; }
  .np-card:focus-visible { outline: 2px solid var(--c); outline-offset: -2px; }
  @keyframes np-in {
    0% { opacity: 0; transform: translateX(56px) scale(.96); }
    35% { opacity: 1; }
    100% { opacity: 1; transform: none; }
  }

  .np-row { display: flex; align-items: flex-start; gap: 12px; padding-left: 4px; }

  /* ── Іконка-плитка ── */
  .np-tile {
    position: relative; flex: 0 0 auto; width: 46px; height: 46px; border-radius: 13px;
    background: var(--bg-input); display: grid; place-items: center;
    box-shadow: 0 4px 10px rgba(0, 0, 0, .32);
    animation: np-pop .55s cubic-bezier(.34, 1.56, .64, 1) backwards .12s;
  }
  .np-tile.glyph {
    background: linear-gradient(145deg, color-mix(in srgb, var(--c) 88%, #fff), color-mix(in srgb, var(--c) 62%, #000));
  }
  .np-tile .np-img { width: 100%; height: 100%; border-radius: 13px; object-fit: cover; }
  .np-tile .np-head { image-rendering: pixelated; }
  .np-tile .np-app { width: 30px; height: 30px; object-fit: contain; }
  .np-glyph { font-size: 25px; color: var(--on); }
  .np-badge {
    position: absolute; right: -6px; bottom: -6px; width: 20px; height: 20px; border-radius: 50%;
    display: grid; place-items: center; font-size: 11px; color: var(--on);
    background: var(--c); border: 2px solid var(--bg-card);
  }
  .np-tile.glyph .np-badge { display: none; }
  @keyframes np-pop { from { transform: scale(.4) rotate(-8deg); opacity: 0 } }

  /* ── Текст ── */
  .np-text { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
  .np-kicker {
    font: 800 9.5px/1.2 var(--font-ui); letter-spacing: .09em; text-transform: uppercase;
    color: var(--c); white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .np-title { display: flex; align-items: center; gap: 6px; min-width: 0; font: 800 13.5px/1.25 var(--font-ui); color: var(--text); }
  .np-name { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .np-mini { flex: 0 0 auto; width: 16px; height: 16px; border-radius: 5px; image-rendering: pixelated; }
  .np-line {
    font: 500 11.5px/1.35 var(--font-ui); color: var(--text-mute);
    display: -webkit-box; line-clamp: 2; -webkit-line-clamp: 2; -webkit-box-orient: vertical;
    overflow: hidden; word-break: break-word;
  }
  .np-line.err { color: color-mix(in srgb, var(--c) 55%, var(--text)); }
  .np-sub { font: 700 12px/1.3 var(--font-ui); color: var(--text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .np-quote { font: italic 500 11px/1.35 var(--font-ui); color: var(--text-dim); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .np-mono {
    font: 600 11px/1.3 var(--font-mono, ui-monospace, monospace); color: var(--c);
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .np-chips { display: flex; flex-wrap: wrap; gap: 4px; margin-top: 3px; }
  .np-chip {
    padding: 1px 7px; border-radius: 999px; font: 700 10px/1.5 var(--font-ui); color: var(--text);
    background: color-mix(in srgb, var(--c) 16%, transparent);
    border: 1px solid color-mix(in srgb, var(--c) 34%, transparent);
  }
  .np-err { margin-top: 2px; font: 600 11px/1.3 var(--font-ui); color: var(--red); }
  .np-doneline { display: flex; align-items: center; gap: 6px; font-weight: 700; color: var(--c); animation: np-pop .4s cubic-bezier(.34, 1.56, .64, 1) both; }
  .np-doneline.bad { color: var(--text-mute); }
  .np-doneline i { font-size: 16px; }

  /* ── Кнопки ── */
  .np-acts { display: flex; gap: 6px; padding-left: 4px; }
  .np-btn {
    display: inline-flex; align-items: center; justify-content: center; gap: 5px;
    height: 28px; padding: 0 12px; border-radius: 9px; cursor: pointer;
    font: 700 11.5px/1 var(--font-ui); color: var(--text);
    background: color-mix(in srgb, var(--c) 12%, transparent);
    border: 1px solid color-mix(in srgb, var(--c) 42%, transparent);
    transition: background .14s, transform .12s, border-color .14s;
  }
  .np-btn i { font-size: 14px; }
  .np-btn:hover:not(:disabled) { background: color-mix(in srgb, var(--c) 24%, transparent); border-color: var(--c); }
  .np-btn:active:not(:disabled) { transform: scale(.95); }
  .np-btn:disabled { opacity: .65; cursor: default; }
  .np-btn.pri { flex: 1 1 auto; background: var(--c); border-color: var(--c); color: var(--on); }
  .np-btn.pri:hover:not(:disabled) { background: color-mix(in srgb, var(--c) 86%, #fff); }
  .np-btn.ok { color: var(--green); border-color: var(--green); }
  .np-btn:focus-visible { outline: 2px solid var(--c); outline-offset: 2px; }
  .np-spin { animation: np-spin .8s linear infinite; }
  @keyframes np-spin { to { transform: rotate(360deg) } }

  .np-x {
    position: absolute; top: 8px; right: 8px;
    width: 22px; height: 22px; border: none; border-radius: 7px;
    background: transparent; color: var(--text-dim);
    display: grid; place-items: center; font-size: 13px; cursor: pointer;
    opacity: 0; transition: opacity .14s, background .14s, color .14s;
  }
  .np-card:hover .np-x, .np-x:focus-visible { opacity: 1; }
  .np-x:hover { background: rgba(239, 68, 68, .16); color: var(--red); }
  .np-x:focus-visible { outline: 2px solid var(--c); outline-offset: 1px; }

  /* ── Таймер-смужка: чиста CSS-анімація (transform), пауза при наведенні ── */
  .np-bar {
    position: absolute; left: 0; right: 0; bottom: 0; height: 3px; background: var(--c); opacity: .55;
    transform-origin: left center; animation: np-bar var(--ttl) linear forwards;
  }
  .np.hov .np-bar { animation-play-state: paused; }
  @keyframes np-bar { from { transform: scaleX(1) } to { transform: scaleX(0) } }

  /* ── Фірмова анімація кожного типу (скінченна, лише transform/opacity) ── */
  /* Помилка: картка тремтить, значок пульсує. */
  .ev-install_failed { animation: np-in .5s cubic-bezier(.2, 1.15, .3, 1) both, np-shake .5s ease .5s 1; }
  @keyframes np-shake { 0%,100% { transform: none } 20% { transform: translateX(-6px) } 45% { transform: translateX(5px) } 70% { transform: translateX(-3px) } }
  .ev-install_failed .np-tile::after, .ev-action_needed .np-tile::after, .ev-server_started .np-tile::after {
    content: ''; position: absolute; inset: -2px; border-radius: 15px; border: 2px solid var(--c); opacity: 0;
    animation: np-ring 1.5s ease-out .6s 3;
  }
  @keyframes np-ring { 0% { transform: scale(.9); opacity: .85 } 100% { transform: scale(1.35); opacity: 0 } }
  /* Завантажено: блиск пробігає карткою один раз, плитка підстрибує. */
  .ev-download_done::after {
    content: ''; position: absolute; top: 0; bottom: 0; left: -40%; width: 36%; pointer-events: none;
    background: linear-gradient(100deg, transparent, color-mix(in srgb, var(--c) 30%, transparent), transparent);
    transform: translateX(0) skewX(-14deg); animation: np-sheen 1.1s ease .45s 1 both;
  }
  @keyframes np-sheen { from { transform: translateX(0) skewX(-14deg); opacity: 1 } to { transform: translateX(420%) skewX(-14deg); opacity: 0 } }
  .ev-download_done .np-tile { animation: np-pop .55s cubic-bezier(.34, 1.56, .64, 1) backwards .12s, np-bob .5s ease .65s 2; }
  /* Запит у друзі: плитка «махає». */
  .ev-friend_request .np-tile { animation: np-pop .55s cubic-bezier(.34, 1.56, .64, 1) backwards .12s, np-wig .7s ease .7s 1; }
  @keyframes np-wig { 0%,100% { transform: none } 25% { transform: rotate(-9deg) } 55% { transform: rotate(8deg) } 80% { transform: rotate(-4deg) } }
  /* Передачі та запрошення: плитка двічі підстрибує. */
  .ev-share_build .np-tile, .ev-share_server .np-tile, .ev-share_mod .np-tile,
  .ev-invite_temp .np-tile, .ev-invite_perm .np-tile { animation: np-pop .55s cubic-bezier(.34, 1.56, .64, 1) backwards .12s, np-bob .5s ease .7s 2; }
  @keyframes np-bob { 0%,100% { transform: none } 45% { transform: translateY(-5px) } }
  /* Сервер зупинено: приглушений вигляд. */
  .ev-server_stopped .np-tile { opacity: .82; }

  @media (prefers-reduced-motion: reduce) {
    .np-card, .np-tile, .np-doneline, .np-bar, .np-card::after, .np-tile::after { animation: none !important; }
  }
</style>
