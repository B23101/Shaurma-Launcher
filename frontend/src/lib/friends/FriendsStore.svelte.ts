// ══════════════════════════════════════════════════════════════════════════
// FriendsStore — уся логіка розділу «Друзі Shaurma» без жодної розмітки.
//
// Раніше все це жило в одному компоненті на 1000+ рядків разом зі стилями.
// Тепер компоненти (список, рядок, модалки, чат) лише читають цей store й
// викликають його методи — тож кожен можна міняти окремо.
//
// БЕЗПЕКА ВІДОБРАЖЕННЯ (як і було): усе, що показується (нік, назва збірки,
// колір), бекенд уже санітизував, але UI не довіряє йому наосліп — жодного
// {@html}, колір перевіряється регуляркою, помилки показуються ПЕРЕКЛАДОМ
// за кодом, а не сирим текстом сервера.
// ══════════════════════════════════════════════════════════════════════════
import { getContext, setContext } from 'svelte'
import { App, on } from '../wails'
import { toast, confirmDialog } from '../toast.svelte'
import { t } from '../i18n.svelte'
import { buildFriendLink, parseFriendLink } from './friendLink'
import { parseFriendInput } from './handle'
import type { ChatKeyStatus } from '../wails'
import type {
  SocialState, SocialFriend, SocialPresenceEvent, SocialConn, SocialPrivacy,
  SocialBlockedAccount, SocialPeerProfile, SocialUserHit, SocialInvite,
} from '../types'

// eslint-disable-next-line no-control-regex
const HIDDEN = /[\u0000-\u001f\u007f-\u009f​-‏‪-‮⁦-⁩﻿]/g
export const clean = (s?: string) => (s ?? '').replace(HIDDEN, '').trim()
export const safeColor = (c?: string) => (c && /^#[0-9a-fA-F]{6}$/.test(c) ? c : '')
export const initialOf = (s?: string) => (clean(s).charAt(0) || '?').toUpperCase()

const EMPTY: SocialState = {
  loggedIn: false, conn: 'off', friends: [], invites: [],
  privacy: { showStatusTo: 'friends', showServerTo: 'friends', friendRequests: 'everyone', whoCanMessage: 'friends' },
  privacyLoaded: false,
}

// Бекенд міг віддати null-слайси або неповний об'єкт — не ламаємо UI.
function normalize(s: SocialState | null | undefined): SocialState {
  const x = s ?? EMPTY
  return {
    ...EMPTY, ...x,
    friends: Array.isArray(x.friends) ? x.friends : [],
    invites: Array.isArray(x.invites) ? x.invites : [],
    privacy: { ...EMPTY.privacy, ...(x.privacy ?? {}) },
  }
}

const listToSet = (l: unknown) => new Set((Array.isArray(l) ? l : []).map((x) => String(x).toLowerCase()))

export type GroupKey = 'in_game' | 'online' | 'offline'
export interface FriendGroup { key: GroupKey; icon: string; list: SocialFriend[] }

const COMPACT_KEY = 'shaurma.friends.compact'
const WIDTH_KEY = 'shaurma.friends.listWidth'
/** Межі ширини списку — як у головного sidebar (220–400px). */
export const LIST_W_MIN = 220
export const LIST_W_MAX = 400
/** Ширина згорнутого списку (рейка) — рівно як у головного sidebar. */
export const LIST_W_RAIL = 68

export class FriendsStore {
  // ── Стан ──────────────────────────────────────────────────────────────
  st = $state<SocialState>(structuredClone(EMPTY))
  loading = $state(true)
  /** Перший знімок уже показано — далі рядки, що з'являються, анімуються. */
  ready = $state(false)
  busy = $state('')
  /** Мої ВИХІДНІ запити в друзі: видно, кому надіслано, і можна скасувати. */
  sent = $state<SocialInvite[]>([])
  heads = $state<Record<string, string>>({})
  search = $state('')
  /** Швидкий фільтр за статусом (чіпи над списком). */
  statusFilter = $state<'all' | 'online' | 'in_game'>('all')
  listTab = $state<'chats' | 'requests'>('chats')
  compact = $state((() => { try { return localStorage.getItem(COMPACT_KEY) === '1' } catch { return false } })())
  /** Ширина розгорнутого списку (перетягується правим краєм, зберігається). */
  listW = $state((() => { try { const v = Number(localStorage.getItem(WIDTH_KEY)); return v >= LIST_W_MIN && v <= LIST_W_MAX ? v : 264 } catch { return 264 } })())
  listResizing = $state(false)
  nowTick = $state(Date.now())
  /** Вікно вузьке (<=720px): панелі показуються по одній, режим «рейка» вимкнено. */
  narrow = $state(false)

  keys = $state<ChatKeyStatus>({ state: 'ready', managed: false, versions: 0, offline: false })
  keysBusy = $state(false)

  muteAll = $state(false)
  muteAllBusy = $state(false)
  muteSound = $state(false)
  mutedIds = $state<Set<string>>(new Set())

  blocked = $state<SocialBlockedAccount[]>([])
  blockedLoaded = $state(false)
  savingPrivacy = $state(false)

  former = $state<SocialFriend[]>([])
  formerOpen = $state(false)
  peerProfiles = $state<Record<string, SocialPeerProfile>>({})
  typingNow = $state<Record<string, boolean>>({})

  myHandle = $state('')

  /** Хто натиснув «Профіль» / «Запросити» — модалки живуть у шеллі. */
  profileId = $state('')
  tempInviteFor = $state('')

  #setActive: (id: string) => void
  #typingTimers: Record<string, ReturnType<typeof setTimeout>> = {}
  #headQueue: string[] = []
  #headSeen = new Set<string>()
  #headActive = 0
  #readyTimer: ReturnType<typeof setTimeout> | undefined

  constructor(setActive: (id: string) => void) {
    this.#setActive = setActive
  }

  // ── Похідні ───────────────────────────────────────────────────────────
  get me() { return this.st.me ?? '' }
  /** Рейка з іконками справді активна: збережена перевага + достатньо місця. */
  get rail() { return this.compact && !this.narrow }
  unreadOf = (id: string) => this.st.unread?.[id] ?? 0
  isMuted = (id: string) => this.mutedIds.has(id.toLowerCase())
  isFriend = (id: string) => this.st.friends.some((f) => f.accountId === id)

  hasUnread = $derived(Object.values(this.st.unread ?? {}).some((n) => n > 0))
  unreadTotal = $derived(Object.values(this.st.unread ?? {}).reduce((a, n) => a + (n > 0 ? n : 0), 0))

  inGameCount = $derived(this.st.friends.filter((f) => f.presence?.status === 'in_game').length)

  filtered = $derived.by(() => {
    const q = this.search.trim().toLowerCase()
    const sf = this.statusFilter
    return this.st.friends.filter((f) => {
      const st = f.presence?.status
      if (sf === 'in_game' && st !== 'in_game') return false
      if (sf === 'online' && st !== 'in_game' && st !== 'online' && st !== 'away') return false
      return !q || clean(f.nickname).toLowerCase().includes(q) || (f.handle ?? '').toLowerCase().includes(q)
    })
  })

  // Сортування стабільне: спершу з непрочитаним, далі за ніком. Друзі НЕ
  // «стрибають» між позиціями від кожного оновлення присутності.
  #sorted(list: SocialFriend[]): SocialFriend[] {
    const u = this.st.unread ?? {}
    return [...list].sort((a, b) => {
      const ua = (u[a.accountId] ?? 0) > 0 ? 1 : 0
      const ub = (u[b.accountId] ?? 0) > 0 ? 1 : 0
      if (ua !== ub) return ub - ua
      return clean(a.nickname).localeCompare(clean(b.nickname), undefined, { sensitivity: 'base' })
    })
  }

  groups = $derived.by<FriendGroup[]>(() => {
    const f = this.filtered
    const g = (s?: string) => (s === 'in_game' ? 'in_game' : s === 'online' || s === 'away' ? 'online' : 'offline')
    const by: Record<GroupKey, SocialFriend[]> = { in_game: [], online: [], offline: [] }
    for (const x of f) by[g(x.presence?.status)].push(x)
    return [
      { key: 'in_game' as const, icon: 'ti-device-gamepad-2', list: this.#sorted(by.in_game) },
      { key: 'online' as const, icon: 'ti-circle-check', list: this.#sorted(by.online) },
      { key: 'offline' as const, icon: 'ti-moon', list: this.#sorted(by.offline) },
    ].filter((x) => x.list.length > 0)
  })
  onlineCount = $derived(this.st.friends.filter((f) => ['in_game', 'online', 'away'].includes(f.presence?.status ?? '')).length)

  formerShown = $derived(this.former.filter((x) => !this.st.friends.some((f) => f.accountId === x.accountId)))
  strangerIds = $derived(Object.keys(this.st.unread ?? {}).filter((id) =>
    (this.st.unread?.[id] ?? 0) > 0 && !this.st.friends.some((f) => f.accountId === id) && !this.former.some((x) => x.accountId === id)))
  strangerRows = $derived(this.strangerIds.map((id) => this.peerProfiles[id]).filter(Boolean).map((p) =>
    ({ accountId: p.accountId, nickname: p.nickname, handle: p.handle, since: 0, presence: { status: 'offline' } }) as SocialFriend))
  otherChats = $derived([...this.strangerRows, ...this.formerShown])

  connLabel = $derived(
    !this.st.loggedIn ? '' :
    this.st.conn === 'online' ? t('social.conn.online') :
    this.st.conn === 'connecting' ? t('social.conn.connecting') : t('social.conn.off'))

  profilePeer = $derived(this.st.friends.find((x) => x.accountId === this.profileId) ?? null)

  peerById(id: string): SocialFriend | null {
    return this.st.friends.find((f) => f.accountId === id) ?? this.otherChats.find((f) => f.accountId === id) ?? null
  }

  // ── Життєвий цикл ─────────────────────────────────────────────────────
  /** Підписки на події бекенда. Викликати з onMount; повертає відписку. */
  start(): () => void {
    void this.load()
    App.GetChatMuteState().then((m) => {
      this.muteAll = !!m?.all; this.muteSound = !!m?.sound
      this.mutedIds = listToSet(m?.muted)
    }).catch(() => {})
    App.GetChatKeyStatus().then((k) => { if (k) this.keys = k }).catch(() => {})

    // «Був у мережі N хв тому» мусить «тікати» без нових подій від сервера.
    const tick = setInterval(() => { this.nowTick = Date.now() }, 30_000)

    const offs = [
      on('friends:update', (s: SocialState) => { this.st = normalize(s); this.loading = false; this.#markReady() }),
      on('friends:conn', (c: SocialConn) => { this.st.conn = c }),
      on('chat:keys', (k: ChatKeyStatus) => { if (k) this.keys = k }),
      on('friends:presence', (e: SocialPresenceEvent) => {
        const f = this.st.friends.find((x) => x.accountId === e?.accountId)
        if (f && e.presence) f.presence = e.presence
      }),
      on('friends:unread', (m: Record<string, number>) => { this.st.unread = m && typeof m === 'object' ? m : {} }),
      on('chat:typing', (e: { accountId?: string }) => {
        const id = e?.accountId
        if (!id) return
        this.typingNow[id] = true
        clearTimeout(this.#typingTimers[id])
        this.#typingTimers[id] = setTimeout(() => { this.typingNow[id] = false }, 4000)
      }),
      on('chat:message', (e: { from?: string }) => { if (e?.from) this.typingNow[e.from] = false }),
    ]
    return () => {
      clearInterval(tick)
      clearTimeout(this.#readyTimer)
      offs.forEach((off) => off())
      Object.values(this.#typingTimers).forEach(clearTimeout)
      App.SetSocialView(false, '').catch(() => {})
    }
  }

  // Рядки, що з'являються ПІСЛЯ першого показу, анімуються; початковий
  // список — ні (інакше 100 друзів одночасно «в'їжджають» і лагають).
  #markReady() {
    if (this.ready || this.#readyTimer) return
    this.#readyTimer = setTimeout(() => { this.ready = true }, 450)
  }

  async load(force = false) {
    try {
      this.st = normalize(force ? await App.RefreshFriends() : await App.GetFriendsState())
    } catch (e) {
      toast(this.errText(e), 'error')
    } finally {
      this.loading = false
      this.#markReady()
    }
  }

  // ── Помилки: КОД → переклад ───────────────────────────────────────────
  errText(e: unknown): string {
    const msg = String((e as any)?.message ?? e ?? '')
    const m = /social:([A-Za-z_ ]+)/.exec(msg)
    const code = m ? m[1].trim().replace(/ /g, '_') : ''
    return code ? this.codeText(code) : t('social.err.unknown')
  }
  codeText(code: string): string {
    const key = `social.err.${code}`
    const txt = t(key)
    return txt !== key ? txt : t('social.err.unknown')
  }

  // ── Ефекти, які шелл запускає у своєму контексті ──────────────────────
  /** Читає список «колишніх» для поточного акаунта. */
  syncFormer() {
    this.former = this.#readFormer()
  }
  #formerKey = () => `sf.former.${this.st.me ?? ''}`
  #readFormer(): SocialFriend[] {
    try {
      const v = JSON.parse(localStorage.getItem(this.#formerKey()) ?? '[]')
      return Array.isArray(v) ? v.filter((x) => x && typeof x.accountId === 'string') : []
    } catch { return [] }
  }
  rememberFormer(f: SocialFriend) {
    const next = [{ accountId: f.accountId, nickname: f.nickname, handle: f.handle, color: f.color, since: f.since, presence: { status: 'offline' } } as SocialFriend,
      ...this.former.filter((x) => x.accountId !== f.accountId)].slice(0, 50)
    this.former = next
    try { localStorage.setItem(this.#formerKey(), JSON.stringify(next)) } catch { /* не критично */ }
  }

  /** Нік не-друга, який написав першим, — підтягуємо з сервера й кешуємо. */
  syncStrangers() {
    for (const id of this.strangerIds) {
      if (this.peerProfiles[id]) continue
      this.peerProfiles[id] = { accountId: id, nickname: id.slice(0, 8) + '…' }
      App.GetChatPeerProfile(id).then((p) => { if (p?.accountId) this.peerProfiles[id] = p }).catch(() => {})
    }
  }

  // ── Голови: черга з обмеженою паралельністю (3), щоб список зі 100 друзів
  // не відкрив 100 одночасних запитів до Mojang.
  wantHead(name: string) {
    if (!name || this.#headSeen.has(name)) return
    this.#headSeen.add(name)
    this.#headQueue.push(name)
    this.#pumpHeads()
  }
  #pumpHeads() {
    while (this.#headActive < 3 && this.#headQueue.length) {
      const name = this.#headQueue.shift()!
      this.#headActive++
      App.GetAccountHead('', name, false)
        .then((h) => { if (typeof h === 'string' && h.startsWith('data:image/')) this.heads[name] = h })
        .catch(() => {})
        .finally(() => { this.#headActive--; this.#pumpHeads() })
    }
  }
  syncHeads() {
    for (const f of this.st.friends) this.wantHead(f.nickname)
    for (const i of this.st.invites) this.wantHead(i.fromNickname)
  }

  // ── UI-перемикачі ─────────────────────────────────────────────────────
  setCompact(v: boolean) {
    this.compact = v
    if (v) this.search = ''
    try { localStorage.setItem(COMPACT_KEY, v ? '1' : '0') } catch { /* не критично */ }
  }
  setListW(w: number, persist = false) {
    this.listW = Math.min(LIST_W_MAX, Math.max(LIST_W_MIN, Math.round(w)))
    if (persist) { try { localStorage.setItem(WIDTH_KEY, String(this.listW)) } catch { /* не критично */ } }
  }
  openChat(f: SocialFriend) {
    if (!f?.accountId) return
    // Не друг: запам'ятовуємо діалог, щоб він лишився в списку після прочитання.
    if (!this.isFriend(f.accountId)) this.rememberFormer(f)
    this.#setActive(f.accountId)
  }
  openProfile(f: SocialFriend) { if (f?.accountId) this.profileId = f.accountId }

  // ── Додавання друзів ──────────────────────────────────────────────────
  /** Що саме ввів користувач: посилання-запрошення, UUID або @handle. */
  parseTarget(input: string) {
    const l = parseFriendLink(input)
    if (l) return l.handle ? ({ kind: 'handle', handle: l.handle } as const) : ({ kind: 'id', id: l.accountId.toLowerCase() } as const)
    return parseFriendInput(input)
  }
  async inviteTarget(tg: NonNullable<ReturnType<FriendsStore['parseTarget']>>) {
    if (tg.kind === 'handle') await App.SendFriendInviteByHandle(tg.handle)
    else await App.SendFriendInvite(tg.id)
    toast(t('social.inviteSent', { name: tg.kind === 'handle' ? '@' + tg.handle : tg.id.slice(0, 8) + '…' }), 'success')
    void this.loadSent()
  }
  async inviteHit(h: SocialUserHit) {
    await App.SendFriendInvite(h.accountId)
    toast(t('social.inviteSent', { name: clean(h.nickname) }), 'success')
    void this.loadSent()
  }
  async loadSent() {
    try { this.sent = (await App.ListSentFriendInvites()) ?? [] } catch { /* старий сервер — просто без списку */ }
    for (const i of this.sent) this.wantHead(i.fromNickname)
  }
  async cancelSent(inv: SocialInvite) {
    if (this.busy) return
    this.busy = inv.id
    try {
      await App.RespondFriendInvite(inv.id, false)   // відправник «відхиляє» = скасовує
      this.sent = this.sent.filter((x) => x.id !== inv.id)
      toast(t('social.sentCanceled', { name: clean(inv.fromNickname) }), 'info')
    } catch (e) {
      toast(this.errText(e), 'error')
      void this.loadSent()
    } finally { this.busy = '' }
  }
  async loadMyHandle() {
    if (this.myHandle) return
    try { this.myHandle = (await App.GetShaurmaProfile())?.handle ?? '' } catch { /* без handle лишається UUID */ }
  }
  get myIdLabel() { return this.myHandle ? '@' + this.myHandle : this.me }

  async #copy(text: string, okMsg: string, failMsg: string) {
    try { await App.CopyToClipboard(text); toast(okMsg, 'success'); return }
    catch { /* пробуємо браузерний буфер */ }
    try { await navigator.clipboard.writeText(text); toast(okMsg, 'success') }
    catch { toast(failMsg, 'error') }
  }
  copyMyId() { return this.#copy(this.myIdLabel, t('social.idCopied'), t('social.idCopyFail')) }
  async copyInviteLink() {
    if (!this.me) return
    let nickname = ''
    try { nickname = (await App.GetShaurmaAccount())?.nickname ?? '' } catch { /* без ніка — теж ок */ }
    let handle = this.myHandle
    if (!handle) { try { handle = (await App.GetShaurmaProfile())?.handle ?? '' } catch { /* старе посилання з UUID теж працює */ } }
    return this.#copy(buildFriendLink(this.me, nickname, handle), t('friends.linkCopied'), t('friends.linkCopyFail'))
  }

  // ── Дії над запитами / друзями ────────────────────────────────────────
  async respond(id: string, accept: boolean) {
    if (this.busy) return
    this.busy = id
    try {
      await App.RespondFriendInvite(id, accept)
      if (accept) toast(t('social.accepted'), 'success')
      await this.load()   // не покладаємось лише на подію friends:update
    } catch (e) {
      toast(this.errText(e), 'error')
      void this.load(true)
    } finally { this.busy = '' }
  }

  async remove(f: SocialFriend) {
    if (this.busy) return
    const ok = await confirmDialog(
      t('social.removeTitle'), t('social.removeDesc', { name: f.nickname }),
      { confirmLabel: t('social.remove'), cancelLabel: t('cancel'), danger: true },
    )
    if (!ok) return
    this.busy = f.accountId
    try {
      await App.RemoveFriend(f.accountId)
      this.rememberFormer(f)
      if (this.profileId === f.accountId) this.profileId = ''
      toast(t('social.removed', { name: clean(f.nickname) }), 'info')
      await this.load()
    } catch (e) { toast(this.errText(e), 'error') }
    finally { this.busy = '' }
  }

  // ── Блокування ────────────────────────────────────────────────────────
  async loadBlocked() {
    try { this.blocked = (await App.GetBlockedAccounts()) ?? []; this.blockedLoaded = true }
    catch (e) { toast(this.errText(e), 'error') }
  }
  async blockPeer(p: SocialFriend | null, afterReload?: () => void) {
    if (!p || this.busy) return
    const ok = await confirmDialog(
      t('social.block.title'), t('social.block.desc', { name: clean(p.nickname) }),
      { confirmLabel: t('social.block.confirm'), cancelLabel: t('cancel'), danger: true },
    )
    if (!ok) return
    this.busy = p.accountId
    try {
      await App.BlockAccount(p.accountId)
      this.rememberFormer(p)  // історія лишається доступною для читання
      if (this.profileId === p.accountId) this.profileId = ''
      this.#setActive('')
      toast(t('social.block.done', { name: clean(p.nickname) }), 'info')
      await this.load()
      if (this.blockedLoaded) await this.loadBlocked()
      afterReload?.()
    } catch (e) { toast(this.errText(e), 'error') }
    finally { this.busy = '' }
  }
  async unblock(b: SocialBlockedAccount) {
    try {
      await App.UnblockAccount(b.accountId)
      this.blocked = this.blocked.filter((x) => x.accountId !== b.accountId)
      toast(t('social.block.undone', { name: clean(b.nickname) }), 'success')
    } catch (e) { toast(this.errText(e), 'error') }
  }

  // ── Мут ───────────────────────────────────────────────────────────────
  async toggleMutePeer(f: SocialFriend) {
    const was = this.isMuted(f.accountId)
    try {
      const s = await App.SetChatMuted(f.accountId, !was)
      this.mutedIds = listToSet(s?.muted)
      toast(was ? t('social.chat.mutedOff', { name: clean(f.nickname) }) : t('social.chat.mutedOn', { name: clean(f.nickname) }), 'success')
    } catch (e) { toast(this.errText(e), 'error') }
  }
  async toggleMuteSound() {
    try { this.muteSound = !!(await App.SetChatMuteSound(!this.muteSound))?.sound }
    catch (e) { toast(this.errText(e), 'error') }
  }
  async toggleMuteAll() {
    if (this.muteAllBusy) return
    this.muteAllBusy = true
    try {
      this.muteAll = !!(await App.SetChatMuteAll(!this.muteAll))?.all
      toast(t(this.muteAll ? 'social.muteAll.toastOn' : 'social.muteAll.toastOff'), 'success')
    } catch (e) { toast(this.errText(e), 'error') }
    finally { this.muteAllBusy = false }
  }

  // ── Приватність ───────────────────────────────────────────────────────
  async savePrivacy(patch: Partial<SocialPrivacy>) {
    if (this.savingPrivacy) return
    const next: SocialPrivacy = { ...this.st.privacy, ...patch }
    this.savingPrivacy = true
    try {
      await App.SetFriendsPrivacy(next)
      this.st.privacy = next
      this.st.privacyLoaded = true
    } catch (e) {
      toast(this.errText(e), 'error')   // значення в UI лишається попереднім
    } finally { this.savingPrivacy = false }
  }

  // ── Ключі чату ────────────────────────────────────────────────────────
  async resetKeys() {
    if (this.keysBusy) return
    if (!(await confirmDialog(t('social.keys.resetConfirmTitle'), t('social.keys.resetConfirm'),
      { confirmLabel: t('social.keys.reset'), cancelLabel: t('cancel') }))) return
    this.keysBusy = true
    try { await App.ResetChatKeys(); toast(t('social.keys.resetDone'), 'success') }
    catch (e: any) { toast(this.errText(e), 'error') }
    finally { this.keysBusy = false; App.GetChatKeyStatus().then((k) => (this.keys = k)).catch(() => {}) }
  }
}

// ── Контекст: щоб дочірні компоненти не тягнули store пропсами крізь усе ──
const KEY = Symbol('friends-store')
export function provideFriendsStore(s: FriendsStore) { setContext(KEY, s); return s }
export function useFriendsStore(): FriendsStore { return getContext<FriendsStore>(KEY) }
