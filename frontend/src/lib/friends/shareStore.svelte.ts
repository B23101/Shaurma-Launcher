// Спільний стан передач (відправник + одержувач) для картки в чаті, віджета й попереджень.
// Живе на рівні модуля: бекенд шле події share:in / share:out, а ListShare* дає стан при старті.
import { App, on } from '../wails'
import { toast } from '../toast.svelte'
import type { ShareInView, ShareOutView, ShareRecoverable } from './shareMsg'

export const shareIn = $state<Record<string, ShareInView>>({})
export const shareOut = $state<Record<string, ShareOutView>>({})
export const shareRecoverable = $state<{ items: ShareRecoverable[] }>({ items: [] })

let inited = false
// Помилки підготовки передачі приходять лише подією (ShareStart повертається одразу) —
// без тосту відправник їх би не побачив. Кожну показуємо один раз.
const errShown = new Set<string>()

function notifyOut(v: ShareOutView) {
  if (!v.error) return
  const key = v.id + '|' + v.error
  if (errShown.has(key)) return
  errShown.add(key)
  toast(v.phase === 'error' ? `Передачу «${v.name}» не розпочато: ${v.error}` : v.error, 'error')
}

export async function initShareStore(): Promise<void> {
  if (inited) return
  inited = true
  on('share:in', (v: ShareInView) => { if (v?.id) shareIn[v.id] = v })
  on('share:out', (v: ShareOutView) => { if (v?.id) { shareOut[v.id] = v; notifyOut(v) } })
  on('share:out-closed', (id: string) => {
    // Закриту передачу лишаємо на мить, щоб віджет показав підсумок.
    setTimeout(() => { delete shareOut[id] }, 4000)
    for (const k of errShown) if (k.startsWith(id + '|')) errShown.delete(k)
  })
  try { for (const v of (await App.ListShareIn()) ?? []) shareIn[v.id] = v } catch {}
  try { for (const v of (await App.ListShareOut()) ?? []) shareOut[v.id] = v } catch {}
  try { shareRecoverable.items = (await App.ListShareRecoverable()) ?? [] } catch {}
}

export function removeRecoverable(mid: string) {
  shareRecoverable.items = shareRecoverable.items.filter(r => r.mid !== mid)
}

/** Активні (ще не завершені) вхідні передачі. */
export function activeIn(): ShareInView[] {
  return Object.values(shareIn).filter(v => ['connecting', 'downloading', 'paused', 'installing'].includes(v.phase))
}
