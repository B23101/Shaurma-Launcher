// Голови друзів для будь-якого місця лаунчера (вибір друзів, списки доступу, «від кого адреса»).
// Одна голова — один запит: результат кешується за ніком, а паралельні запити зливаються в один.
import { App } from '../wails'

const cache = new Map<string, { value: string; used: number }>()
const IDLE_MS = 5 * 60_000
let cleanupTimer: ReturnType<typeof setTimeout> | undefined

function scheduleCleanup() {
  if (cleanupTimer !== undefined || cache.size === 0) return
  cleanupTimer = setTimeout(() => {
    cleanupTimer = undefined
    const cutoff = Date.now() - IDLE_MS
    for (const [key, entry] of cache) if (entry.used <= cutoff) cache.delete(key)
    scheduleCleanup()
  }, IDLE_MS)
}
const pending = new Map<string, Promise<string>>()

/** Голова акаунта як data URL або '' (скіна немає / не вдалося). Не кидає помилок. */
export function friendHead(nickname: string | undefined | null): Promise<string> {
  const nick = String(nickname ?? '').trim()
  if (!nick) return Promise.resolve('')
  const key = nick.toLowerCase()
  const hit = cache.get(key)
  if (hit !== undefined) { hit.used = Date.now(); return Promise.resolve(hit.value) }
  let p = pending.get(key)
  if (!p) {
    p = App.GetAccountHead('', nick, false)
      .then((h) => { const v = h || ''; cache.set(key, { value: v, used: Date.now() }); scheduleCleanup(); return v })
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
