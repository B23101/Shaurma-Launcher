import { parseFriendInput } from '../friends/handle'
import type { P2PEntry } from '../types'

// У списки доступу гравця вводять ПОВНИМ ідентифікатором акаунта або кастомним @handle.
// Кастомний на VPS зводиться до того самого повного id, тож у списку все одно лежить id.
// Нік тут не підходить: він не унікальний і змінюється.

export type P2PPlayerRef = { kind: 'id' | 'handle'; value: string; entry: P2PEntry }

/** Розбирає введене: UUID → {id}, @handle (або просто handle) → {handle}. null — не схоже ні на те, ні на інше. */
export function parsePlayerInput(raw: string | undefined | null): P2PPlayerRef | null {
  const t = parseFriendInput(raw)
  if (!t) return null
  return t.kind === 'id'
    ? { kind: 'id', value: t.id, entry: { id: t.id } }
    : { kind: 'handle', value: t.handle, entry: { handle: t.handle } }
}

/** Короткий підпис для рядка списку: нік, якщо є, інакше @handle, інакше початок id. */
export function playerLabel(p: { nick?: string; handle?: string; id: string }): string {
  return p.nick || (p.handle ? '@' + p.handle : p.id.slice(0, 8) + '…')
}
