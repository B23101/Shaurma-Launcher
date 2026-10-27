// ── Поширення модів у чаті ────────────────────────────────────────────────
// Повідомлення з модом — ЗВИЧАЙНЕ текстове (тож зашифроване, як усе інше,
// і не потребує змін ні у Worker, ні у VPS):
//
//     <необов'язковий коментар>
//     https://modrinth.com/mod/sodium#shaurma=modrinth:AANobbMI
//
// Посилання клікабельне й зрозуміле навіть у старих версіях лаунчера (фрагмент
// після # сайт ігнорує). Нові версії розпізнають рядок і малюють картку мода:
// іконка, назва, автор, лоадери та кнопки «Відкрити в лаунчері / на сайті».
// Звичайне посилання на Modrinth без фрагмента теж стає карткою (за slug).

import { App } from '../wails'
import type { BrowserProjectDetail } from '../types'

export type ModSource = 'modrinth' | 'curseforge'

export interface ModRef {
  source: ModSource
  /** id проєкту (або slug для Modrinth, якщо фрагмента shaurma= немає) */
  projectId: string
  /** чисте посилання без фрагмента */
  url: string
}

const FRAG = /#shaurma=(modrinth|curseforge):([A-Za-z0-9_\-.]{1,40})\s*$/
const MR_URL = /^https:\/\/(?:www\.)?modrinth\.com\/(mod|modpack|resourcepack|shader|datapack|plugin)\/([A-Za-z0-9_\-.]{1,64})\/?(?:[?#].*)?$/
const CF_URL = /^https:\/\/(?:www\.)?curseforge\.com\/minecraft\/[a-z\-]+\/[A-Za-z0-9_\-.]{1,80}\/?(?:[?#].*)?$/

/** Формує текст повідомлення для поширення мода. */
export function buildModMessage(d: Pick<BrowserProjectDetail, 'source' | 'projectId' | 'projectUrl'>, comment = ''): string {
  const url = `${d.projectUrl}#shaurma=${d.source}:${d.projectId}`
  const c = comment.trim()
  return c ? `${c}\n${url}` : url
}

/**
 * Розпізнає повідомлення про мод. Повертає коментар (текст до посилання) і
 * саме посилання. Лише коли посилання — ОСТАННІЙ рядок: довільний текст зі
 * згадкою URL посеред речення лишається звичайним текстом.
 */
export function parseModMessage(text: string | undefined): { comment: string; ref: ModRef } | null {
  if (!text || text.length > 2200) return null
  const lines = text.trim().split('\n')
  const last = lines[lines.length - 1].trim()
  if (!last.startsWith('https://')) return null
  const comment = lines.slice(0, -1).join('\n').trim()

  const frag = FRAG.exec(last)
  const bare = last.replace(FRAG, '')
  if (frag && (MR_URL.test(bare) || CF_URL.test(bare))) {
    return { comment, ref: { source: frag[1] as ModSource, projectId: frag[2], url: bare } }
  }
  const mr = MR_URL.exec(last)
  if (mr) return { comment, ref: { source: 'modrinth', projectId: mr[2], url: bare } }
  return null
}

// Кеш деталей: той самий мод у стрічці не вантажимо двічі.
const detailCache = new Map<string, Promise<BrowserProjectDetail>>()
export function loadModDetail(ref: ModRef): Promise<BrowserProjectDetail> {
  const key = `${ref.source}:${ref.projectId}`
  let p = detailCache.get(key)
  if (!p) {
    p = App.GetBrowserProjectDetail(ref.source, ref.projectId)
    p.catch(() => detailCache.delete(key))
    detailCache.set(key, p)
  }
  return p
}

// ── Локальне приховування повідомлень («Видалити у мене») ────────────────
// Повідомлення лежать зашифрованими на сервері обох учасників; лаунчер вміє
// лише приховати їх на ЦЬОМУ пристрої. Ідентифікатори тримаємо в localStorage
// окремо для кожної особистості (me) і діалогу (chatId).
const HIDE_PREFIX = 'shaurma.chat.hidden.'

export function loadHidden(me: string, chatId: string): Set<number> {
  if (!me || !chatId) return new Set()
  try {
    const raw = localStorage.getItem(HIDE_PREFIX + me)
    const all = raw ? JSON.parse(raw) : {}
    const arr = all?.[chatId]
    return new Set(Array.isArray(arr) ? arr.filter((n: unknown) => typeof n === 'number') : [])
  } catch { return new Set() }
}

export function saveHidden(me: string, chatId: string, ids: Set<number>) {
  if (!me || !chatId) return
  try {
    const raw = localStorage.getItem(HIDE_PREFIX + me)
    const all = raw ? JSON.parse(raw) : {}
    // Ліміт, щоб сховище не росло безкінечно: найновіші 2000 на діалог.
    all[chatId] = [...ids].sort((a, b) => a - b).slice(-2000)
    localStorage.setItem(HIDE_PREFIX + me, JSON.stringify(all))
  } catch { /* приватний режим / квота — ховаємо лише до перезапуску */ }
}
