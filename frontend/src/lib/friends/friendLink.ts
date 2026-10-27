// ── Посилання-запрошення в друзі ────────────────────────────────────────
//
// Формат (звичайне https-посилання, яке можна кинути в Discord/Telegram):
//
//     https://shaurma-launcher.org/friend/@<handle>?n=<nickname>   (коротке, коли handle відомий)
//     https://shaurma-launcher.org/friend/<accountId>?n=<nickname> (старий формат — теж працює)
//
// handle / accountId — ГОЛОВНЕ: за ним іде SendFriendInviteByHandle / SendFriendInvite. ?n= — лише підказка
// для показу (нік у модалці одразу, без походу на сервер); сам нік не є
// ідентичністю (він не унікальний, друзів шукають за accountId).
//
// Лаунчер «бачить» посилання у двох місцях: (1) коли гравець копіює його
// (перевірка буфера обміну при поверненні фокуса вікна) і (2) коли посилання
// вставили вручну. Тоді відкривається AddFriendModal з іконкою та ім'ям.

import { normalizeHandle } from './handle'

export interface FriendLink {
  /** UUID акаунта; порожній, якщо посилання несе лише handle. */
  accountId: string
  /** @handle без «@»; порожній у старих посиланнях. */
  handle: string
  nickname: string
}

/** Ключ посилання для «вже показували»: accountId або @handle. */
export function friendLinkKey(l: FriendLink): string {
  return (l.accountId || '@' + l.handle).toLowerCase()
}

// Хости, які вважаємо «нашими». Додавай сюди всі домени лаунчера.
const LINK_HOSTS = ['shaurma-launcher.org', 'shaurma.app']

// accountId — UUID (VPS) або подібний токен: літери/цифри/дефіс/підкреслення.
const ACCOUNT_ID = /^[A-Za-z0-9_-]{6,64}$/

// Одне посилання десь у тексті (Discord додає текст довкола — шукаємо фрагмент).
const LINK_RE = new RegExp(
  String.raw`https?://(?:www\.)?(?:` +
    LINK_HOSTS.map((h) => h.replace(/\./g, '\\.')).join('|') +
    String.raw`)/friend/(?:@([A-Za-z0-9_.]{3,32})|([A-Za-z0-9_-]{6,64}))(\?[^\s<>"']*)?`,
  'i'
)

/** Формує посилання-запрошення. nickname необов'язковий (лише для показу). Є handle → коротке посилання. */
export function buildFriendLink(accountId: string, nickname = '', handle = ''): string {
  const h = normalizeHandle(handle)
  const base = h
    ? `https://${LINK_HOSTS[0]}/friend/@${h}`
    : `https://${LINK_HOSTS[0]}/friend/${encodeURIComponent(accountId)}`
  const n = nickname.trim()
  return n ? `${base}?n=${encodeURIComponent(n)}` : base
}

/** Розпізнає посилання-запрошення (за потреби — всередині довшого тексту). */
export function parseFriendLink(text: string | undefined | null): FriendLink | null {
  if (!text) return null
  const m = LINK_RE.exec(text)
  if (!m) return null
  let accountId = ''
  let handle = ''
  if (m[1]) {
    // Крапка в кінці — це кінець речення, а не частина handle.
    handle = normalizeHandle(m[1].replace(/\.+$/, ''))
    if (!handle) return null
  } else {
    accountId = m[2]
    if (!ACCOUNT_ID.test(accountId)) return null
  }
  let nickname = ''
  if (m[3]) {
    try {
      nickname = (new URLSearchParams(m[3].slice(1)).get('n') ?? '').trim()
    } catch {
      nickname = ''
    }
  }
  // Санітизація: нік не має містити керуючих/невидимих символів.
  nickname = nickname.replace(/[\u0000-\u001f\u007f\u200b-\u200f\u202a-\u202e\u2066-\u2069\ufeff]/g, '').slice(0, 32)
  return { accountId, handle, nickname }
}
