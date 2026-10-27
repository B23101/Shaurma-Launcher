// Спільний стан передач (відправник + одержувач) для картки в чаті, віджета, модалок і тостів.
// Живе на рівні модуля: бекенд шле події share:in / share:out, а ListShare* дає стан при старті
// (разом із підсумками минулих передач з історії бекенду).
import { App, on } from '../wails'
import { toast } from '../toast.svelte'
import { t } from '../i18n.svelte'
import type { SocialFriend } from '../types'
import {
  parseShareMessage, inReasonText, outReasonText, IN_FINAL, REC_FINAL,
  type ShareCardData, type ShareInView, type ShareOutView, type ShareRecoverable, type SharePreviewView,
} from './shareMsg'

export const shareIn = $state<Record<string, ShareInView>>({})
export const shareOut = $state<Record<string, ShareOutView>>({})
export const shareRecoverable = $state<{ items: ShareRecoverable[] }>({ items: [] })

/** Вхідна пропозиція (картка щойно прийшла в чат) — для модалки «Прийняти?». */
export interface ShareOffer {
  card: ShareCardData
  encoded: string
  fromAccount: string
  fromName: string
}

/** Що відкрито: вікно передачі, черга пропозицій, вікно «недокачане з минулого разу»,
 *  вікно «Що всередині» (sid). offersHidden — пропозицію згорнуто (перейшли в браузер контенту);
 *  віджет показує рядок, щоб повернутись до неї. */
export const shareUi = $state<{
  view: { dir: 'in' | 'out'; id: string } | null
  offers: ShareOffer[]
  offersHidden: boolean
  recoverOpen: boolean
  contents: string | null
}>({ view: null, offers: [], offersHidden: false, recoverOpen: false, contents: null })

// ── Вміст передачі (моди з іконками) — SharePreview, кеш за sid ──
export const sharePreview = $state<Record<string, { loading: boolean; data?: SharePreviewView; error?: string }>>({})
// Звідки брати вміст: картка + відправник (з чату чи пропозиції).
const previewSrc: Record<string, { encoded: string; from: string }> = {}

export function rememberPreviewSrc(sid: string, encoded: string, from: string) {
  if (sid && encoded && from) previewSrc[sid] = { encoded, from }
}
export function canPreview(sid: string): boolean { return !!previewSrc[sid] || !!sharePreview[sid]?.data }

export async function loadPreview(sid: string, force = false): Promise<void> {
  const cur = sharePreview[sid]
  if (cur?.loading || (cur?.data && !force)) return
  const src = previewSrc[sid]
  if (!src) return
  sharePreview[sid] = { loading: true }
  try {
    sharePreview[sid] = { loading: false, data: await App.SharePreview(src.encoded, src.from) }
  } catch (e) {
    sharePreview[sid] = { loading: false, error: errText(e) }
  }
}
export function openContents(sid: string) { shareUi.contents = sid; loadPreview(sid) }

// Перехід на сторінку мода в браузері контенту (обробник дає App через TransferWidget).
let openModFn: ((source: string, projectId: string) => void) | null = null
export function setOpenMod(fn: ((source: string, projectId: string) => void) | null) { openModFn = fn }
export function openModFromShare(projectId: string) {
  if (!openModFn || !projectId) return
  if (shareUi.offers.length) shareUi.offersHidden = true
  shareUi.view = null
  shareUi.contents = null
  openModFn('modrinth', projectId)
}

export function openTransfer(dir: 'in' | 'out', id: string) { shareUi.view = { dir, id } }
export function closeTransfer() { shareUi.view = null }
export function dropOffer(sid: string) { shareUi.offers = shareUi.offers.filter(o => o.card.sid !== sid) }

let inited = false
// Помилки підготовки передачі приходять лише подією (ShareStart повертається одразу) —
// без тосту відправник їх би не побачив. Кожну показуємо один раз.
const errShown = new Set<string>()
// Друзі (нік, колір профілю, присутність) — для аватарок і статусу мережі у вікнах передачі.
export const shareFriends = $state<Record<string, SocialFriend>>({})
let devTag = ''

export function friendName(id: string, fallback = ''): string {
  return shareFriends[id]?.nickname || fallback || t('share.friend')
}

function setFriends(list: SocialFriend[] | undefined) {
  if (!Array.isArray(list)) return
  const keep = new Set(list.map(f => f.accountId))
  for (const id of Object.keys(shareFriends)) if (!keep.has(id)) delete shareFriends[id]
  for (const f of list) shareFriends[f.accountId] = f
}

async function refreshFriends() {
  try { setFriends((await App.GetFriendsState())?.friends) } catch {}
}

function notifyOutError(v: ShareOutView) {
  if (!v.error) return
  const key = v.id + '|' + v.error
  if (errShown.has(key)) return
  errShown.add(key)
  toast(v.phase === 'error' ? t('share.toast.startFailed', { name: v.name, error: v.error }) : v.error, 'error', 6000)
}

// ── Тости результатів: кожен фінал видно обом сторонам ──
function onInChange(prev: ShareInView | undefined, v: ShareInView) {
  if (!IN_FINAL.includes(v.phase) || prev?.phase === v.phase) return
  if (!prev && v.phase !== 'cancelled') return // підсумок з історії при старті — не шумимо
  const name = v.name
  if (v.phase === 'done') {
    toast(t(v.kind === 'server' ? 'share.toast.inDoneServer' : 'share.toast.inDone', { name }), 'success', 6000,
      { label: t('share.details'), onClick: () => openTransfer('in', v.id) })
  } else if (v.phase === 'error') {
    toast(t('share.toast.inFailed', { name, why: inReasonText(v.reason, v.error) }), 'error', 8000,
      { label: t('share.details'), onClick: () => openTransfer('in', v.id) })
  } else if (v.phase === 'cancelled' && v.reason && v.reason !== 'cancelled_receiver' && v.reason !== 'interrupted') {
    toast(t('share.toast.inStopped', { name, why: inReasonText(v.reason) }), 'warn', 8000)
    dropOffer(v.id)
  }
}

function onOutChange(prev: ShareOutView | undefined, v: ShareOutView) {
  if (!prev) return
  for (const r of v.recipients ?? []) {
    const was = prev.recipients?.find(x => x.id === r.id)
    if (!REC_FINAL.includes(r.state) || was?.state === r.state) continue
    if (r.state === 'revoked' && r.reason === 'cancelled_sender') continue // сам натиснув
    const who = friendName(r.id, v.names?.[r.id])
    if (r.state === 'done') toast(t('share.toast.outDone', { who, name: v.name }), 'success', 6000)
    else toast(t('share.toast.outEnded', { who, name: v.name, why: outReasonText(r.reason, r.error) }),
      r.state === 'declined' || r.state === 'cancelled' ? 'warn' : 'error', 8000,
      { label: t('share.details'), onClick: () => openTransfer('out', v.id) })
  }
  if (v.phase === 'closed' && prev.phase !== 'closed' && v.reason === 'timeout') {
    toast(t('share.toast.outExpired', { name: v.name }), 'warn', 8000)
  }
}

// ── Нова картка в чаті → модалка «Прийняти?» ──
async function onChatMessage(ev: any) {
  if (!ev || ev.mine || ev.decryptFailed || (ev.bodyType && ev.bodyType !== 'text')) return
  const shr = parseShareMessage(ev.plaintext)
  if (!shr) return
  const c = shr.card
  if (shareIn[c.sid] || shareUi.offers.some(o => o.card.sid === c.sid)) return
  if (ev.ts && Date.now() - ev.ts > 12 * 3600_000) return // стара картка (догнали історію)
  if (!shareFriends[ev.from]) await refreshFriends()
  if (!shareFriends[ev.from]) return // лише від друзів
  if (!devTag) { try { devTag = (await App.ShareDeviceTag()) || '' } catch {} }
  if (c.d && devTag && c.d === devTag) return // з цього ж лаунчера
  rememberPreviewSrc(c.sid, shr.encoded, ev.from)
  shareUi.offers = [...shareUi.offers, { card: c, encoded: shr.encoded, fromAccount: ev.from, fromName: shareFriends[ev.from].nickname }]
  shareUi.offersHidden = false
}

export async function initShareStore(): Promise<void> {
  if (inited) return
  inited = true
  on('share:in', (v: ShareInView) => {
    if (!v?.id) return
    const prev = shareIn[v.id]
    shareIn[v.id] = v
    onInChange(prev, v)
    if (IN_FINAL.includes(v.phase) || v.phase === 'connecting') dropOffer(v.id)
  })
  on('share:out', (v: ShareOutView) => {
    if (!v?.id) return
    const prev = shareOut[v.id]
    shareOut[v.id] = v
    notifyOutError(v)
    onOutChange(prev, v)
  })
  on('share:out-closed', (id: string) => {
    for (const k of errShown) if (k.startsWith(id + '|')) errShown.delete(k)
  })
  on('chat:message', (ev: any) => { onChatMessage(ev) })
  on('friends:update', (st: any) => { if (Array.isArray(st?.friends)) setFriends(st.friends); else refreshFriends() })
  await refreshFriends()
  try { for (const v of (await App.ListShareIn()) ?? []) shareIn[v.id] = v } catch {}
  try { for (const v of (await App.ListShareOut()) ?? []) shareOut[v.id] = v } catch {}
  await refreshRecoverable()
}

export async function refreshRecoverable() {
  try { shareRecoverable.items = (await App.ListShareRecoverable()) ?? [] } catch {}
}

export function removeRecoverable(mid: string) {
  shareRecoverable.items = shareRecoverable.items.filter(r => r.mid !== mid)
}

/** Активні (ще не завершені) вхідні передачі. */
export function activeIn(): ShareInView[] {
  return Object.values(shareIn).filter(v => !IN_FINAL.includes(v.phase))
}

/** Активні вихідні передачі. */
export function activeOut(): ShareOutView[] {
  return Object.values(shareOut).filter(v => v.phase === 'preparing' || v.phase === 'ready')
}

/** Повідомлення помилки з бекенду (Error / рядок). */
export function errText(e: any): string {
  return String(e?.message ?? e ?? '')
}
