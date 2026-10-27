// Голови друзів для будь-якого місця лаунчера (вибір друзів, списки доступу, «від кого адреса»).
// Одна голова — один запит: результат кешується за ніком, а паралельні запити зливаються в один.
import { App } from '../wails'

const cache = new Map<string, string>()
const pending = new Map<string, Promise<string>>()

/** Голова акаунта як data URL або '' (скіна немає / не вдалося). Не кидає помилок. */
export function friendHead(nickname: string | undefined | null): Promise<string> {
  const nick = String(nickname ?? '').trim()
  if (!nick) return Promise.resolve('')
  const key = nick.toLowerCase()
  const hit = cache.get(key)
  if (hit !== undefined) return Promise.resolve(hit)
  let p = pending.get(key)
  if (!p) {
    p = App.GetAccountHead('', nick, false)
      .then((h) => { const v = h || ''; cache.set(key, v); return v })
      .catch(() => '')
      .finally(() => { pending.delete(key) })
    pending.set(key, p)
  }
  return p
}

/** Колір друга (#rrggbb) із запасним значенням, якщо профіль кольору не задав. */
export function friendColor(color: string | undefined | null, fallback = 'var(--text-dim)'): string {
  return /^#[0-9a-f]{6}$/i.test(String(color ?? '')) ? String(color) : fallback
}
