// ── «Поділитися збіркою / сервером»: повідомлення-картка в чаті ─────────────
// Картка — ЗВИЧАЙНЕ текстове (тож зашифроване, як усе інше) повідомлення, чий ОСТАННІЙ рядок
//
//     shaurma-share:v1:<base64url(json)>
//
// Старі версії лаунчера бачать читабельний текст над рядком; нові малюють картку з кнопками.
// Ключ доступу всередині картки їде лише в зашифрованому чаті. Повну перевірку полів робить бекенд.
//
// Службові повідомлення (відмова, скасування, збій) — так само, з рядком
//     shaurma-share-ctl:v1:<base64url(json)>
// (internal/share/control.go). Бекенд застосовує їх сам, чат лише показує короткий статус.
import { t } from '../i18n.svelte'

export const SHARE_PREFIX = 'shaurma-share:v1:'
export const SHARE_CTL_PREFIX = 'shaurma-share-ctl:v1:'

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

/** Коди причин завершення (share.Reason* у бекенді). */
export type ShareReason =
  | 'declined' | 'cancelled_sender' | 'cancelled_receiver' | 'sender_offline' | 'receiver_offline'
  | 'timeout' | 'integrity' | 'disk_full' | 'changed' | 'access_closed' | 'error' | 'interrupted'

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
  reason?: ShareReason
  /** Зв'язок із відправником обірвався — пробуємо знову. */
  reconnecting?: boolean
  /** Скільки вже взято напряму з Modrinth CDN (а не від друга). */
  cdnFiles?: number
  cdnBytes?: number
}

export type ShareInPhase =
  | 'connecting' | 'verifying' | 'downloading' | 'paused' | 'installing'
  | 'done' | 'error' | 'declined' | 'cancelled'

export interface ShareInView {
  id: string
  mid: string
  kind: 'build' | 'server'
  name: string
  from: string
  fromAccount?: string
  files: number
  bytes: number
  phase: ShareInPhase
  progress: ShareProgress
  error?: string
  reason?: ShareReason
  resultId?: string
  createdAt?: number
  updatedAt?: number
}

export type ShareRecipientStatus =
  'waiting' | 'transferring' | 'paused' | 'done' | 'declined' | 'failed' | 'revoked' | 'cancelled' | 'timeout'

export interface ShareRecipientState {
  id: string
  state: ShareRecipientStatus
  pausedBy?: 'receiver' | 'sender'
  bytesSent: number
  bytesDone: number
  filesDone: number
  filesTotal: number
  lastSeenMs: number
  error?: string
  reason?: ShareReason
  speedBps: number
  /** Друг давно не виходив на зв'язок (ще чекаємо). */
  offline?: boolean
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
  /** Шукаємо файли на Modrinth (частина підготовки). */
  resolving?: boolean
  /** Знайдено на Modrinth — друзі візьмуть це з CDN, а не з вашого каналу. */
  cdnFiles?: number
  cdnBytes?: number
  names?: Record<string, string>
  error?: string
  reason?: ShareReason
  createdAt?: number
  updatedAt?: number
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
  files: number
  bytes: number
  kind: 'build' | 'server'
  complete: boolean
  reason?: ShareReason
  card: string
}

/** Один мод / ресурспак / шейдер / плагін у передачі (SharePreview). */
export interface SharePreviewItem {
  path: string
  name: string
  size: number
  category: 'mods' | 'resourcepacks' | 'shaderpacks' | 'plugins' | 'datapacks'
  disabled?: boolean
  projectId?: string
  title?: string
  iconUrl?: string
  slug?: string
}

export interface SharePreviewView {
  sid: string
  kind: 'build' | 'server'
  files: number
  bytes: number
  groups: { key: string; files: number; bytes: number }[]
  items: SharePreviewItem[]
  cdnFiles: number
  cdnBytes: number
  truncated?: boolean
  meta?: Record<string, string>
}

export interface ShareCtl {
  v: number
  sid: string
  e: 'declined' | 'cancelled' | 'failed'
  r?: ShareReason
  n?: string
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

/** Розпізнає службове повідомлення передачі (відмова / скасування / збій). */
export function parseShareCtl(text: string | undefined): ShareCtl | null {
  if (!text || text.length > 2000) return null
  const s = text.trim()
  const idx = s.lastIndexOf(SHARE_CTL_PREFIX)
  if (idx < 0) return null
  const enc = s.slice(idx + SHARE_CTL_PREFIX.length)
  if (!enc || /\s/.test(enc)) return null
  const json = decodeB64Url(enc)
  if (!json) return null
  try {
    const c = JSON.parse(json) as ShareCtl
    if (c?.v !== 1 || typeof c.sid !== 'string' || !['declined', 'cancelled', 'failed'].includes(c.e)) return null
    return c
  } catch { return null }
}

const KNOWN_REASONS = new Set<string>(['declined', 'cancelled_sender', 'cancelled_receiver', 'sender_offline', 'receiver_offline',
  'timeout', 'integrity', 'disk_full', 'changed', 'access_closed', 'error', 'interrupted'])

/** Текст причини для ОДЕРЖУВАЧА (моя вхідна передача). */
export function inReasonText(reason?: string, fallback?: string): string {
  const r = reason && KNOWN_REASONS.has(reason) ? reason : 'error'
  if (r === 'error' && fallback) return fallback
  return t('share.in.reason.' + r)
}

/** Текст причини для ВІДПРАВНИКА (стан одного друга). */
export function outReasonText(reason?: string, fallback?: string): string {
  const r = reason && KNOWN_REASONS.has(reason) ? reason : 'error'
  if (r === 'error' && fallback) return fallback
  return t('share.out.reason.' + r)
}

/** Короткий текст службового повідомлення в чаті. own — я його надіслав. */
export function ctlText(c: ShareCtl, own: boolean, peer: string): string {
  const name = c.n || '…'
  if (c.e === 'declined') return own ? t('share.ctl.declinedMine', { name }) : t('share.ctl.declined', { peer, name })
  if (c.e === 'failed') {
    const why = inReasonText(c.r)
    return own ? t('share.ctl.failedMine', { name, why }) : t('share.ctl.failed', { peer, name, why })
  }
  if (c.r === 'cancelled_receiver') return own ? t('share.ctl.stoppedMine', { name }) : t('share.ctl.stopped', { peer, name })
  if (c.r === 'timeout') return own ? t('share.ctl.expiredMine', { name }) : t('share.ctl.expired', { name })
  if (c.r === 'sender_offline') return own ? t('share.ctl.interruptedMine', { name }) : t('share.ctl.interrupted', { peer, name })
  return own ? t('share.ctl.cancelledMine', { name }) : t('share.ctl.cancelled', { peer, name })
}

export function fmtBytes(n: number): string {
  if (!isFinite(n) || n <= 0) return '0 ' + t('share.unit.b')
  const u = ['b', 'kb', 'mb', 'gb', 'tb']
  let i = 0
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++ }
  return `${n >= 100 || i === 0 ? Math.round(n) : n.toFixed(1)} ${t('share.unit.' + u[i])}`
}

export function fmtEta(remaining: number, speed: number): string {
  if (speed <= 0 || remaining <= 0) return ''
  const s = Math.round(remaining / speed)
  if (s < 60) return t('share.eta.s', { s })
  if (s < 3600) return t('share.eta.m', { m: Math.floor(s / 60), s: s % 60 })
  return t('share.eta.h', { h: Math.floor(s / 3600), m: Math.floor((s % 3600) / 60) })
}

export function pctOf(done: number, total: number): number {
  return total > 0 ? Math.min(100, Math.floor(done * 100 / total)) : 0
}

/** Остаточні фази вхідної передачі. */
export const IN_FINAL: ShareInPhase[] = ['done', 'error', 'declined', 'cancelled']
export const IN_ACTIVE: ShareInPhase[] = ['connecting', 'verifying', 'downloading', 'paused', 'installing']
export const REC_FINAL: ShareRecipientStatus[] = ['done', 'declined', 'failed', 'revoked', 'cancelled', 'timeout']
