// ── Живий статус сервера ──────────────────────────────────────────────
// Підписка на server:status + server:install-progress. Раніше цей код
// був продубльований у бібліотеці та в сторінці сервера — тут одна
// реалізація, яку викликають обидві сторінки.
import { App, on } from '../../wails'
import type { ServerView, ServerInstallProgress } from '../../types'

export type ServerStatusHandle = {
  status: string
  progress: ServerInstallProgress | null
}

export function subscribeServerStatus(
  serverId: string,
  onStatus: (s: ServerView | null, status: string) => void,
  onProgress: (p: ServerInstallProgress | null) => void,
): () => void {
  const offStatus = on('server:status', (data: any) => {
    if (data.serverId !== serverId) return
    onStatus(null, data.status || '')
    if (data.status !== 'installing') onProgress(null)
  })
  const offProgress = on('server:install-progress', (data: any) => {
    if (data.serverId !== serverId) return
    onProgress({ stage: data.stage || 'core', percent: data.percent ?? -1, message: data.message || '' })
  })
  App.GetServerStatus(serverId).then((s: any) => onStatus(null, s?.status || '')).catch(() => {})
  return () => { offStatus?.(); offProgress?.() }
}
