// Типи групових чатів — дзеркало JSON сервера (vps/internal/groups, hub/protocol_groups.go).
// Усе, що приходить із сервера, UI показує лише як ТЕКСТ (жодного {@html}); колір і число
// перевіряються тут, а не береться на віру.

export type GroupPolicy = 'friends' | 'everyone' | 'nobody'
export type GroupWho = 'admin' | 'all'

export interface GroupSettings {
  color: string
  icon?: string
  historyForNew: boolean
  pinBy: GroupWho
  unpinBy: GroupWho
  allowMods: boolean
  allowBuilds: boolean
}

export interface GroupMember {
  accountId: string
  nickname?: string
  handle?: string
  joinedAt: number
  admin?: boolean
}

export interface Group {
  id: string
  title: string
  admin: string
  createdAt: number
  settings: GroupSettings
  members: GroupMember[]
  unread?: number
  lastId?: number
}

export interface GroupReaction { by: string; emoji: string }

export interface GroupMessage {
  id: number
  groupId: string
  from: string
  kind: 'text' | 'system' | 'build_offer'
  body: string
  ts: number
  editedAt?: number
  replyToId?: number
  clientMessageId?: string
  reactions?: GroupReaction[]
}

export interface GroupInvite {
  id: string
  groupId: string
  groupTitle: string
  from: string
  fromNickname?: string
  to: string
  createdAt: number
  expiresAt: number
}

/** Вміст повідомлення kind=build_offer (сервер уже очистив поля). */
export interface GroupBuildOffer {
  k: 'build' | 'server'
  n: string
  f: number
  b: number
  sid: string
  m?: Record<string, string>
}

/** Вміст повідомлення kind=system. */
export interface GroupSystemEvent {
  e: 'created' | 'join' | 'leave' | 'kick' | 'admin' | 'invite_accepted' | string
  a?: string
  by?: string
}

export type GroupSettingsPatch = Partial<{
  title: string
  color: string
  icon: string
  historyForNew: boolean
  pinBy: GroupWho
  unpinBy: GroupWho
  allowMods: boolean
  allowBuilds: boolean
}>

export const GROUP_MAX_MEMBERS = 20
export const GROUP_MAX_BODY = 1500
export const GROUP_MAX_TITLE = 60
export const GROUP_EMOJI = ['👍', '❤️', '😂', '🔥', '😮', '🎉']

const COLOR_RE = /^#[0-9a-fA-F]{6}$/
export const safeGroupColor = (c?: string) => (c && COLOR_RE.test(c) ? c : '')

/** Готові іконки групи (Tabler без «ti-»). Дзеркало groups.GroupIcons на сервері — міняти разом. */
export const GROUP_ICONS = [
  'users-group', 'swords', 'sword', 'pick', 'axe', 'shield', 'crown', 'trophy',
  'diamond', 'cube', 'tree', 'leaf', 'mushroom', 'flame', 'campfire', 'bolt',
  'star', 'moon', 'sun', 'snowflake', 'planet', 'rocket', 'map', 'compass',
  'skull', 'ghost-2', 'cat', 'paw', 'fish', 'heart', 'music', 'device-gamepad-2',
] as const
const ICON_SET: ReadonlySet<string> = new Set(GROUP_ICONS)
/** Іконка лише зі списку: значення з сервера йде в class, тож чуже не пропускаємо. */
export const safeGroupIcon = (i?: string) => (i && ICON_SET.has(i) ? i : '')

export function parseOffer(body: string): GroupBuildOffer | null {
  try {
    const o = JSON.parse(body)
    if (!o || (o.k !== 'build' && o.k !== 'server') || typeof o.n !== 'string' || !/^[a-f0-9]{32}$/.test(o.sid ?? '')) return null
    return { k: o.k, n: o.n, f: Number(o.f) || 0, b: Number(o.b) || 0, sid: o.sid, m: o.m && typeof o.m === 'object' ? o.m : {} }
  } catch { return null }
}

export function parseSystem(body: string): GroupSystemEvent | null {
  try {
    const o = JSON.parse(body)
    return o && typeof o.e === 'string' ? { e: o.e, a: typeof o.a === 'string' ? o.a : undefined, by: typeof o.by === 'string' ? o.by : undefined } : null
  } catch { return null }
}
