// ── @handle: публічний ідентифікатор для пошуку друзів ───────────────────
// Дзеркало VPS accounts.NormalizeHandle / Go social.NormalizeHandle / worker normalizeHandle.
// ТІ САМІ тестові вектори (див. handle.vectors.ts): змінюєш правило — міняй у всіх чотирьох місцях.

const HANDLE_RE = /^[a-z0-9_.]{3,32}$/

/** Нормалізований handle (без «@», нижній регістр) або '' — якщо формат невалідний. */
export function normalizeHandle(raw: string | undefined | null): string {
  const h = String(raw ?? '').trim().toLowerCase().replace(/^@/, '')
  if (!HANDLE_RE.test(h) || h.includes('..') || h.includes('__')) return ''
  if (/^[._]|[._]$/.test(h)) return ''
  return h
}

// accountId — UUID із дефісами (VPS видає саме такі). Рядок без дефісів — це handle:
// «deadbeef» валідний handle, а не обрізаний id.
const ACCOUNT_ID_RE = /^[0-9a-f]{8}-[0-9a-f-]{4,}$/i

export type FriendTarget =
  | { kind: 'id'; id: string }
  | { kind: 'handle'; handle: string }

/** Розпізнає, що ввів гравець у полі «Додати друга»: ID акаунта або @handle. */
export function parseFriendInput(raw: string | undefined | null): FriendTarget | null {
  const s = String(raw ?? '').trim()
  if (!s) return null
  if (ACCOUNT_ID_RE.test(s) && s.length <= 64) return { kind: 'id', id: s.toLowerCase() }
  const h = normalizeHandle(s)
  return h ? { kind: 'handle', handle: h } : null
}
