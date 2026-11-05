// activityCache.ts — «коли востаннє писали в чаті» (друзі й групи) для порядку списку чатів.
//
// ЧОМУ ПОРЯДОК «ПРОГРІВАВСЯ»: сервер не віддає час останньої активності разом зі списком, тож клієнт
// питав історію КОЖНОГО чату окремо (по одному запиту, 3 паралельно) — список спершу ішов за
// алфавітом, а потім стрибав, коли відповіді сходились. Тепер останній відомий порядок
// зберігається локально (по акаунту) і застосовується ОДРАЗУ при запуску; мережеві відповіді його лише
// уточнюють (беремо максимум, нічого не обнуляємо).
//
// Сервер лишається джерелом правди: щойно він віддаватиме lastActivity у friends_snapshot /
// groups_snapshot, ці дані можна просто підмішувати через mergeActivity().

/** Секунди → мілісекунди. Сервер/бекенд віддають і ті, і ті; змішувати їх у сортуванні не можна. */
export const toMs = (ts: number | undefined | null): number => {
  const n = Number(ts)
  if (!Number.isFinite(n) || n <= 0) return 0
  return n < 1e12 ? n * 1000 : n
}

const MAX_ENTRIES = 500
const timers = new Map<string, ReturnType<typeof setTimeout>>()

export function readActivity(key: string): Record<string, number> {
  try {
    const v = JSON.parse(localStorage.getItem(key) ?? '{}')
    if (!v || typeof v !== 'object') return {}
    const out: Record<string, number> = {}
    for (const [id, ts] of Object.entries(v)) {
      const ms = toMs(ts as number)
      if (ms > 0) out[id] = ms
    }
    return out
  } catch { return {} }
}

/** Відкладений запис: активність змінюється часто, а localStorage — синхронний. */
export function saveActivity(key: string, read: () => Record<string, number>, delay = 800) {
  clearTimeout(timers.get(key))
  timers.set(key, setTimeout(() => {
    timers.delete(key)
    try {
      const entries = Object.entries(read()).filter(([, v]) => v > 0).sort((a, b) => b[1] - a[1]).slice(0, MAX_ENTRIES)
      localStorage.setItem(key, JSON.stringify(Object.fromEntries(entries)))
    } catch { /* не критично: порядок просто перерахується з мережі */ }
  }, delay))
}

/** Підмішує нові значення в map, лишаючи більше з двох. Повертає true, якщо щось змінилось. */
export function mergeActivity(into: Record<string, number>, from: Record<string, number>): boolean {
  let changed = false
  for (const [id, ts] of Object.entries(from)) {
    const ms = toMs(ts)
    if (ms > (into[id] ?? 0)) { into[id] = ms; changed = true }
  }
  return changed
}
