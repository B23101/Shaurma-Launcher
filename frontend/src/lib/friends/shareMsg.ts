// ── «Поділитися збіркою / сервером»: повідомлення-картка в чаті ─────────────
// Картка — ЗВИЧАЙНЕ текстове (тож зашифроване, як усе інше) повідомлення, чий ОСТАННІЙ рядок
//
//     shaurma-share:v1:<base64url(json)>
//
// Старі версії лаунчера бачать читабельний текст над рядком; нові малюють картку з кнопками.
// Ключ доступу всередині картки їде лише в зашифрованому чаті. Повну перевірку полів робить бекенд.

export const SHARE_PREFIX = 'shaurma-share:v1:'

export interface ShareCardData {
  v: number
  k: 'build' | 'server'
  n: string
  a: string
  key: string
  mid: string
  sid: string
  f: number
  b: number
  m?: Record<string, string>
  /** Мітка пристрою відправника (див. ShareDeviceTag у бекенді). */
  d?: string
}

export interface ShareProgress {
  state: string
  pausedBy?: 'receiver' | 'sender'
  bytesDone: number
  bytesTotal: number
  filesDone: number
  filesTotal: number
  speedBps: number
  current?: string
  error?: string
}

export interface ShareInView {
  id: string
  mid: string
  kind: 'build' | 'server'
  name: string
  from: string
  phase: 'connecting' | 'downloading' | 'paused' | 'installing' | 'done' | 'error' | 'declined' | 'cancelled'
  progress: ShareProgress
  error?: string
  resultId?: string
}

export interface ShareRecipientState {
  id: string
  state: 'waiting' | 'transferring' | 'paused' | 'done' | 'declined' | 'failed' | 'revoked'
  pausedBy?: 'receiver' | 'sender'
  bytesSent: number
  bytesDone: number
  filesDone: number
  filesTotal: number
  lastSeenMs: number
  error?: string
}

export interface ShareOutView {
  id: string
  kind: 'build' | 'server'
  refId: string
  name: string
  phase: 'preparing' | 'ready' | 'closed' | 'error'
  hashed: number
  hashTotal: number
  files: number
  totalBytes: number
  skipped?: string[]
  recipients: ShareRecipientState[]
  error?: string
}

export interface ShareStartRequest {
  kind: 'build' | 'server'
  refId: string
  friendIds: string[]
  included: string[]
  excluded: string[]
}

export interface ShareRecoverable {
  mid: string
  name: string
  from: string
  filesDone: number
  card: string
}

function decodeB64Url(s: string): string | null {
  try {
    let b = s.replace(/-/g, '+').replace(/_/g, '/')
    while (b.length % 4) b += '='
    const bin = atob(b)
    const bytes = Uint8Array.from(bin, c => c.charCodeAt(0))
    return new TextDecoder('utf-8', { fatal: true }).decode(bytes)
  } catch { return null }
}

/** Розпізнає повідомлення-картку передачі. Маркер — у кінці повідомлення; зазвичай окремим
 *  рядком, але старі повідомлення (надіслані, коли бекенд вирізав усі переноси рядків)
 *  мають його в одному рядку з текстом — тому шукаємо останнє входження префікса. */
export function parseShareMessage(text: string | undefined): { comment: string; encoded: string; card: ShareCardData } | null {
  if (!text || text.length > 6000) return null
  const t = text.trim()
  const idx = t.lastIndexOf(SHARE_PREFIX)
  if (idx < 0) return null
  const last = t.slice(idx).trim()
  if (/\s/.test(last)) return null
  const json = decodeB64Url(last.slice(SHARE_PREFIX.length))
  if (!json) return null
  try {
    const c = JSON.parse(json) as ShareCardData
    if (c?.v !== 1 || (c.k !== 'build' && c.k !== 'server') || typeof c.n !== 'string' || typeof c.sid !== 'string') return null
    return { comment: t.slice(0, idx).trim(), encoded: last, card: c }
  } catch { return null }
}

export function fmtBytes(n: number): string {
  if (!isFinite(n) || n <= 0) return '0 Б'
  const u = ['Б', 'КБ', 'МБ', 'ГБ', 'ТБ']
  let i = 0
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++ }
  return `${n >= 100 || i === 0 ? Math.round(n) : n.toFixed(1)} ${u[i]}`
}

export function fmtEta(remaining: number, speed: number): string {
  if (speed <= 0 || remaining <= 0) return ''
  const s = Math.round(remaining / speed)
  if (s < 60) return `${s} с`
  if (s < 3600) return `${Math.floor(s / 60)} хв ${s % 60} с`
  return `${Math.floor(s / 3600)} год ${Math.floor((s % 3600) / 60)} хв`
}
