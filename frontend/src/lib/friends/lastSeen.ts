// «Був у мережі 20 хв тому / 20 год тому / 5 днів тому» для друзів, що офлайн.
//
// Дані приходять із VPS (presence.lastSeenAt, unix мс). Якщо друг приховав
// статус, а також для старого сервера значення нема (0/undefined) — функція
// повертає порожній рядок, і UI показує звичайне «Не в мережі».
//
// Множини ("1 день / 2 дні / 5 днів") — через Intl.PluralRules поточної мови:
// у словнику лежать форми `.one/.few/.many/.other`, відсутня форма падає на `.other`.
import { t, getLanguage } from '../i18n.svelte'

const MIN = 60_000
const HOUR = 60 * MIN
const DAY = 24 * HOUR

function plural(base: string, n: number): string {
  let cat = 'other'
  try { cat = new Intl.PluralRules(getLanguage()).select(n) } catch { /* невідома мова → other */ }
  const key = `${base}.${cat}`
  const s = t(key, { n })
  // t() повертає сам ключ, якщо перекладу нема → падаємо на .other
  return s === key ? t(`${base}.other`, { n }) : s
}

/** Підпис «востаннє в мережі». `now` передається ззовні, щоб UI міг оновлювати його таймером. */
export function lastSeenText(lastSeenAt: number | undefined, now: number = Date.now()): string {
  if (!lastSeenAt || lastSeenAt <= 0) return ''
  const diff = Math.max(0, now - lastSeenAt)
  if (diff < MIN) return t('social.lastSeen.justNow')
  if (diff < HOUR) return t('social.lastSeen.min', { n: Math.floor(diff / MIN) })
  if (diff < DAY) return t('social.lastSeen.hour', { n: Math.floor(diff / HOUR) })
  const days = Math.floor(diff / DAY)
  if (days < 30) return plural('social.lastSeen.day', days)
  if (days < 365) return plural('social.lastSeen.month', Math.floor(days / 30))
  return plural('social.lastSeen.year', Math.floor(days / 365))
}
