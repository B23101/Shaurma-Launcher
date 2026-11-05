// ══════════════════════════════════════════════════════════════════════════
// GroupsStore — уся логіка групових чатів без розмітки (за зразком FriendsStore).
//
// Сервер — єдине джерело правди про права: UI лише ховає кнопки, а кожну дію ще раз перевіряє
// сервер (forbidden / not_found). Тому тут немає «оптимістичних» змін складу чи прав: стан
// оновлюється відповіддю або живою подією group_*. Повідомлення групи йдуть відкритим текстом по
// WSS і зберігаються на сервері зашифрованими ключем сервера — це НЕ наскрізне шифрування, і UI
// про це чесно каже в налаштуваннях групи.
// ══════════════════════════════════════════════════════════════════════════
import { getContext, setContext, untrack } from 'svelte'
import { App, on } from '../wails'
import { toast } from '../toast.svelte'
import { t } from '../i18n.svelte'
import { toMs, readActivity, saveActivity, mergeActivity } from '../friends/activityCache'
import type {
  Group, GroupInvite, GroupMessage, GroupPolicy, GroupSettingsPatch,
} from './groupTypes'

const CTX = Symbol('groups-store')

/** Скільки секунд індикатор «друкує…» живе без оновлення. */
const TYPING_MS = 4000
/** Скільки повідомлень тримаємо в пам'яті на групу (старі підвантажуються знову). */
const KEEP_MSGS = 400

type Fields = Record<string, unknown>

export class GroupsStore {
  /** На сервері групи ввімкнені (є ключ шифрування). false — розділ ховається. */
  enabled = $state(false)
  loaded = $state(false)
  groups = $state<Group[]>([])
  activity = $state<Record<string, number>>({})
  invites = $state<GroupInvite[]>([])
  whoCanAdd = $state<GroupPolicy>('friends')
  activeId = $state('')
  msgs = $state<Record<string, GroupMessage[]>>({})
  pinned = $state<Record<string, GroupMessage[]>>({})
  hasMore = $state<Record<string, boolean>>({})
  reads = $state<Record<string, Record<string, number>>>({})
  unread = $state<Record<string, number>>({})
  typing = $state<Record<string, Record<string, number>>>({})
  histLoading = $state<Record<string, boolean>>({})
  busy = $state(false)
  /** Непрочитане на момент відкриття групи (роздільник «Нові» у чаті). */
  openedUnread: Record<string, number> = {}
  #listeners = new Set<(type: string, d: any) => void>()

  /** Мій accountId і довідник нікнеймів друзів (дає батьківський компонент). */
  me = $state('')
  #friendName: (id: string) => string
  #typingTimers: Record<string, ReturnType<typeof setTimeout>> = {}
  #lastTypingSent = 0
  #windowActive: () => boolean
  #onOpen: () => void

  constructor(friendName: (id: string) => string, windowActive: () => boolean = () => true, onOpen: () => void = () => {}) {
    this.#friendName = friendName
    this.#windowActive = windowActive
    this.#onOpen = onOpen
  }

  // ── Життєвий цикл ──────────────────────────────────────────────────────

  start(): () => void {
    const offs = [
      on('group:event', (e: { type?: string; data?: any }) => this.#handle(e?.type ?? '', e?.data)),
      // Після (пере)підключення сервер сам шле знімок, але повторимо запит: пуші йдуть лише на
      // цей вузол, тож історію відкритої групи теж підтягуємо знову.
      on('friends:conn', (c: string) => { if (c === 'online') { this.#activityVerified.clear(); void this.refresh() } }),
    ]
    void this.refresh()
    return () => {
      offs.forEach((off) => off())
      Object.values(this.#typingTimers).forEach(clearTimeout)
    }
  }

  // Порядок чатів: останній відомий час активності зберігається локально (як у FriendsStore), тож
  // список груп одразу впорядкований, а мережа лише уточнює.
  #actKey = () => `sg.activity.${this.me}`
  #activityFor = ''
  #activityVerified = new Set<string>()
  restoreActivity() {
    untrack(() => {
      if (!this.me || this.#activityFor === this.me) return
      this.#activityFor = this.me
      this.#activityVerified.clear()
      mergeActivity(this.activity, readActivity(this.#actKey()))
    })
  }
  #bump(id: string, ts: number) {
    const ms = toMs(ts)
    if (!id || ms <= (this.activity[id] ?? 0)) return
    this.activity[id] = ms
    if (this.me) saveActivity(this.#actKey(), () => ({ ...this.activity }))
  }

  async refreshActivity() {
    this.restoreActivity()
    // Звіряємо лише групи, яких ще не звіряли в цій сесії: живі group_message вже оновлюють activity.
    const ids = this.groups.map(g => g.id).filter(id => !this.#activityVerified.has(id))
    // Результати застосовуємо разом (один перерахунок порядку замість стрибка на кожну відповідь).
    const found: Record<string, number> = {}
    const worker = async () => {
      for (let id = ids.shift(); id; id = ids.shift()) {
        try {
          const r = await this.#op('history', { groupId: id, before: 0, limit: 1 })
          found[id] = Math.max(0, ...(r?.history?.messages ?? []).map((m: GroupMessage) => m.ts))
        } catch { /* Keep known activity while offline. */ }
      }
    }
    await Promise.all(Array.from({ length: 6 }, worker))
    for (const [id, ts] of Object.entries(found)) {
      if (!this.group(id)) continue
      this.#bump(id, ts)
      this.#activityVerified.add(id)
    }
  }

  async refresh() {
    try { await App.GroupRequest('list') } catch { /* офлайн — знімок прийде після підключення */ }
    if (this.activeId) void this.loadHistory(this.activeId, true)
  }

  // ── Допоміжне ──────────────────────────────────────────────────────────

  get active(): Group | undefined { return this.groups.find((g) => g.id === this.activeId) }
  group(id: string): Group | undefined { return this.groups.find((g) => g.id === id) }
  isAdmin(g: Group | undefined): boolean { return !!g && !!this.me && g.admin === this.me }
  get totalUnread(): number { return Object.values(this.unread).reduce((a, b) => a + b, 0) }

  nameOf(id: string, g?: Group): string {
    if (id === this.me) return t('groups.you')
    const m = (g ?? this.active)?.members.find((x) => x.accountId === id)
    return (m?.nickname || this.#friendName(id) || t('groups.someone')).slice(0, 40)
  }

  canPin(g: Group, pin: boolean): boolean {
    return this.isAdmin(g) || (pin ? g.settings.pinBy === 'all' : g.settings.unpinBy === 'all')
  }
  canDelete(g: Group, m: GroupMessage): boolean {
    return m.kind !== 'system' && (this.isAdmin(g) || m.from === this.me)
  }
  canEdit(m: GroupMessage): boolean { return m.kind === 'text' && m.from === this.me }

  /** Код помилки сервера → переведений текст (а не сирий рядок сервера). */
  errText(e: unknown): string {
    const m = /social:([A-Za-z_ ]+)/.exec(String((e as any)?.message ?? e ?? ''))
    const code = m ? m[1].trim().replace(/ /g, '_') : ''
    if (code) {
      for (const key of [`groups.err.${code}`, `social.err.${code}`]) {
        const txt = t(key)
        if (txt !== key) return txt
      }
    }
    return t('groups.err.unknown')
  }

  async #op(op: string, fields?: Fields): Promise<any> {
    return App.GroupRequest(op, fields)
  }

  // ── Живі події ─────────────────────────────────────────────────────────

  /** Живі події груп для чату (groupChatApi): викликається ПІСЛЯ оновлення стану стору. */
  listen(fn: (type: string, d: any) => void): () => void {
    this.#listeners.add(fn)
    return () => { this.#listeners.delete(fn) }
  }

  #handle(type: string, d: any) {
    if (!d || typeof d !== 'object') return
    this.#apply(type, d)
    for (const fn of this.#listeners) { try { fn(type, d) } catch { /* слухач не ламає стор */ } }
  }

  #apply(type: string, d: any) {
    switch (type) {
      case 'groups_snapshot': {
        this.enabled = !!d.enabled
        this.groups = Array.isArray(d.groups) ? d.groups : []
        this.invites = Array.isArray(d.invites) ? d.invites : []
        if (d.whoCanAdd === 'friends' || d.whoCanAdd === 'everyone' || d.whoCanAdd === 'nobody') this.whoCanAdd = d.whoCanAdd
        const un: Record<string, number> = {}
        for (const g of this.groups) if ((g.unread ?? 0) > 0 && g.id !== this.activeId) un[g.id] = g.unread as number
        this.unread = un
        if (this.activeId && !this.group(this.activeId)) this.activeId = ''
        void this.refreshActivity()
        this.loaded = true
        break
      }
      case 'group_changed': this.#upsertGroup(d.group); break
      case 'group_removed': this.#removed(String(d.groupId ?? ''), String(d.reason ?? '')); break
      case 'group_message': this.#incoming(d.message); break
      case 'group_message_edited': this.#replaceMsg(d.message); break
      case 'group_message_deleted': this.#dropMsg(String(d.groupId ?? ''), Number(d.messageId)); break
      case 'group_pins': if (typeof d.groupId === 'string') this.pinned[d.groupId] = Array.isArray(d.pinned) ? d.pinned : []; break
      case 'group_reactions': this.#setReactions(String(d.groupId ?? ''), Number(d.messageId), d.reactions); break
      case 'group_read_elsewhere': if (typeof d.groupId === 'string') this.unread[d.groupId] = 0; break
      case 'group_typing': this.#typed(String(d.groupId ?? ''), String(d.accountId ?? '')); break
      case 'group_invite_in': {
        const inv = d.invite as GroupInvite
        if (inv?.id && !this.invites.some((i) => i.id === inv.id)) {
          this.invites = [...this.invites, inv]
          toast(t('groups.invite.toast', { name: inv.fromNickname || this.#friendName(inv.from), group: inv.groupTitle }), 'info')
        }
        break
      }
      case 'group_invite_gone': this.invites = this.invites.filter((i) => i.id !== d.inviteId); break
      case 'group_share_result': {
        const key = `groups.share.${d.code}`
        const txt = t(key)
        toast(txt !== key ? txt : t('groups.share.unavailable'), 'error')
        break
      }
    }
  }

  #upsertGroup(g: Group | undefined) {
    if (!g?.id) return
    const i = this.groups.findIndex((x) => x.id === g.id)
    if (i >= 0) this.groups[i] = { ...this.groups[i], ...g, unread: this.groups[i].unread }
    else this.groups = [...this.groups, g]
  }

  #removed(id: string, reason: string) {
    const g = this.group(id)
    this.groups = this.groups.filter((x) => x.id !== id)
    delete this.msgs[id]; delete this.pinned[id]; delete this.unread[id]
    if (this.activeId === id) this.activeId = ''
    if (g && (reason === 'kicked' || reason === 'deleted')) {
      toast(t(reason === 'kicked' ? 'groups.removed.kicked' : 'groups.removed.deleted', { group: g.title }), 'info')
    }
  }

  #incoming(m: GroupMessage | undefined) {
    if (!m?.groupId || !m.id) return
    this.#addMsg(m)
    const seen = this.activeId === m.groupId && this.#windowActive()
    if (!seen && m.from !== this.me && m.kind !== 'system') this.unread[m.groupId] = (this.unread[m.groupId] ?? 0) + 1
    if (m.from !== this.me) this.#clearTyping(m.groupId, m.from)
  }

  /** Повідомлення, яке щойно надіслав цей пристрій (сервер не шле його нам назад). */
  addOwn(m: GroupMessage) { this.#addMsg(m) }

  #addMsg(m: GroupMessage) {
    this.#bump(m.groupId, m.ts)
    const list = this.msgs[m.groupId] ?? []
    if (list.some((x) => x.id === m.id)) return
    const next = [...list, m].sort((a, b) => a.id - b.id)
    this.msgs[m.groupId] = next.length > KEEP_MSGS ? next.slice(next.length - KEEP_MSGS) : next
  }

  #replaceMsg(m: GroupMessage | undefined) {
    if (!m?.groupId) return
    const list = this.msgs[m.groupId]
    const i = list?.findIndex((x) => x.id === m.id) ?? -1
    if (list && i >= 0) list[i] = { ...list[i], ...m, reactions: list[i].reactions }
    const p = this.pinned[m.groupId]
    const j = p?.findIndex((x) => x.id === m.id) ?? -1
    if (p && j >= 0) p[j] = { ...p[j], ...m }
  }

  #dropMsg(gid: string, id: number) {
    if (this.msgs[gid]) this.msgs[gid] = this.msgs[gid].filter((m) => m.id !== id)
    if (this.pinned[gid]) this.pinned[gid] = this.pinned[gid].filter((m) => m.id !== id)
  }

  #setReactions(gid: string, id: number, r: unknown) {
    const m = this.msgs[gid]?.find((x) => x.id === id)
    if (m) m.reactions = Array.isArray(r) ? (r as any) : []
  }

  #typed(gid: string, who: string) {
    if (!gid || !who || who === this.me) return
    this.typing[gid] = { ...(this.typing[gid] ?? {}), [who]: Date.now() }
    const key = gid + who
    clearTimeout(this.#typingTimers[key])
    this.#typingTimers[key] = setTimeout(() => this.#clearTyping(gid, who), TYPING_MS)
  }
  #clearTyping(gid: string, who: string) {
    const cur = this.typing[gid]
    if (!cur || !(who in cur)) return
    const { [who]: _drop, ...rest } = cur
    this.typing[gid] = rest
  }
  typingNames(gid: string): string[] {
    return Object.keys(this.typing[gid] ?? {}).map((id) => this.nameOf(id, this.group(gid)))
  }

  // ── Відкриття групи й історія ──────────────────────────────────────────

  async open(id: string) {
    this.#onOpen()
    this.activeId = id
    this.openedUnread[id] = this.unread[id] ?? 0
    this.unread[id] = 0
    await this.loadHistory(id, true)
  }
  close() { this.activeId = '' }

  /** fresh=true — найновіша сторінка (замінює локальну); інакше — старіші за найстаріше відоме. */
  async loadHistory(gid: string, fresh = false) {
    if (this.histLoading[gid]) return
    this.histLoading[gid] = true
    try {
      const before = fresh ? 0 : (this.msgs[gid]?.[0]?.id ?? 0)
      const r = await this.#op('history', { groupId: gid, before, limit: 50 })
      const h = r?.history
      if (!h) return
      const got: GroupMessage[] = Array.isArray(h.messages) ? h.messages : []
      const merged = new Map<number, GroupMessage>()
      for (const m of fresh ? got.concat(this.msgs[gid] ?? []) : (this.msgs[gid] ?? []).concat(got)) merged.set(m.id, m)
      this.msgs[gid] = [...merged.values()].sort((a, b) => a.id - b.id).slice(-KEEP_MSGS)
      if (fresh || !before) {
        this.hasMore[gid] = !!h.hasMore
        this.pinned[gid] = Array.isArray(h.pinned) ? h.pinned : []
        const rd: Record<string, number> = {}
        for (const x of h.reads ?? []) if (x?.by) rd[x.by] = Number(x.upTo) || 0
        this.reads[gid] = rd
      } else this.hasMore[gid] = !!h.hasMore
    } catch (e) {
      toast(this.errText(e), 'error')
    } finally {
      this.histLoading[gid] = false
    }
  }

  async markRead(gid: string, upTo: number) {
    this.unread[gid] = 0
    try { await this.#op('read', { groupId: gid, messageId: upTo }) } catch { /* не критично */ }
  }

  // ── Дії ────────────────────────────────────────────────────────────────

  /** Обгортка: показує переклад помилки й повертає null. */
  async #do<T>(fn: () => Promise<T>): Promise<T | null> {
    if (this.busy) return null
    this.busy = true
    try { return await fn() } catch (e) { toast(this.errText(e), 'error'); return null } finally { this.busy = false }
  }

  async create(title: string, members: string[]): Promise<Group | null> {
    const r = await this.#do(() => this.#op('create', { title, members }))
    if (!r?.group) return null
    this.#upsertGroup(r.group)
    this.#reportAdd(r)
    return r.group
  }

  async add(gid: string, members: string[]) {
    const r = await this.#do(() => this.#op('add', { groupId: gid, members }))
    if (r) { if (r.group) this.#upsertGroup(r.group); this.#reportAdd(r) }
    return r
  }

  async invite(gid: string, account: string) {
    const r = await this.#do(() => this.#op('invite', { groupId: gid, account }))
    if (r) this.#reportAdd(r)
    return r
  }

  /** Підсумок додавання: причини пропуску НЕ розкриваються (так само, як на сервері). */
  #reportAdd(r: any) {
    const added = Array.isArray(r.added) ? r.added.length : 0
    const invited = Array.isArray(r.invited) ? r.invited.length : 0
    const skipped = Array.isArray(r.skipped) ? r.skipped.length : 0
    if (added) toast(t('groups.added', { n: added }), 'success')
    if (invited) toast(t('groups.invitedN', { n: invited }), 'success')
    if (skipped) toast(t('groups.skipped', { n: skipped }), 'info')
  }

  async respondInvite(inv: GroupInvite, accept: boolean) {
    const r = await this.#do(() => this.#op('invite_respond', { inviteId: inv.id, accept }))
    this.invites = this.invites.filter((i) => i.id !== inv.id)
    if (r?.group) { this.#upsertGroup(r.group); if (accept) await this.open(r.group.id) }
    return !!r
  }

  async leave(gid: string) {
    const r = await this.#do(() => this.#op('leave', { groupId: gid }))
    if (r) this.#removed(gid, 'left')
    return !!r
  }
  async deleteGroup(gid: string) {
    const r = await this.#do(() => this.#op('delete', { groupId: gid }))
    if (r) this.#removed(gid, 'left')
    return !!r
  }
  async kick(gid: string, account: string) {
    const r = await this.#do(() => this.#op('kick', { groupId: gid, account }))
    if (r?.group) this.#upsertGroup(r.group)
    return !!r
  }
  async transfer(gid: string, account: string) {
    const r = await this.#do(() => this.#op('transfer', { groupId: gid, account }))
    if (r?.group) this.#upsertGroup(r.group)
    return !!r
  }
  async patchSettings(gid: string, patch: GroupSettingsPatch) {
    const r = await this.#do(() => this.#op('settings', { groupId: gid, settings: patch }))
    if (r?.group) this.#upsertGroup(r.group)
    return !!r
  }

  async send(gid: string, body: string, replyToId = 0): Promise<boolean> {
    const text = body.trim()
    if (!text) return false
    this.#bump(gid, Date.now()) // група піднімається нагору одразу, а не після відповіді сервера
    try {
      const r = await this.#op('send', { groupId: gid, body: text, replyToId: replyToId || undefined,
        clientMessageId: crypto.randomUUID().replace(/-/g, '') })
      if (r?.message) this.#addMsg(r.message)
      return true
    } catch (e) {
      toast(this.errText(e), 'error')
      return false
    }
  }

  async edit(gid: string, id: number, body: string): Promise<boolean> {
    try { await this.#op('edit', { groupId: gid, messageId: id, body: body.trim() }); return true }
    catch (e) { toast(this.errText(e), 'error'); return false }
  }
  async deleteMessage(gid: string, id: number) {
    try { await this.#op('delete_message', { groupId: gid, messageId: id }) } catch (e) { toast(this.errText(e), 'error') }
  }
  async pin(gid: string, id: number, pin: boolean) {
    try { await this.#op('pin', { groupId: gid, messageId: id, pin }) } catch (e) { toast(this.errText(e), 'error') }
  }
  async react(gid: string, id: number, emoji: string) {
    try { await this.#op('react', { groupId: gid, messageId: id, emoji }) } catch (e) { toast(this.errText(e), 'error') }
  }

  sendTyping(gid: string) {
    const now = Date.now()
    if (now - this.#lastTypingSent < 3000) return
    this.#lastTypingSent = now
    App.GroupSendTyping(gid).catch(() => {})
  }

  /** «Отримати збірку» з картки: сервер → відправник → окрема P2P-передача саме мені. */
  async claimBuild(m: GroupMessage): Promise<boolean> {
    try {
      await App.GroupShareClaim(m.groupId, m.id, m.from)
      toast(t('groups.share.requested'), 'info')
      return true
    } catch (e) {
      toast(this.errText(e), 'error')
      return false
    }
  }

  async setWhoCanAdd(v: GroupPolicy) {
    const prev = this.whoCanAdd
    this.whoCanAdd = v
    try { await App.SetWhoCanAddToGroup(v) } catch (e) { this.whoCanAdd = prev; toast(this.errText(e), 'error') }
  }
}

export function provideGroupsStore(s: GroupsStore): GroupsStore { setContext(CTX, s); return s }
export function useGroupsStore(): GroupsStore { return getContext<GroupsStore>(CTX) }
