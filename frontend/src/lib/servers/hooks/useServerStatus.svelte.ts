// ── useServerStatus — живий статус сервера (runes-хук) ──────────────────
// Підписка на server:status + server:install-progress. Повертає реактивний
// об'єкт { status, progress, pid, uptimeSec, lastError }, який сам
// оновлюється з подій бекенду — сторінка ніколи не застигає на моменті
// GetServer(). Використовується і бібліотекою (для списку), і сторінкою
// сервера (для банера/вкладок).
//
// Використання:
//   const live = useServerStatus(() => serverId)
//   // live.status, live.progress, live.pid, live.uptimeSec, live.lastError
import { App, on } from '../../wails'
import type { ServerInstallProgress } from '../../types'

export interface LiveServerStatus {
  readonly status: string
  readonly progress: ServerInstallProgress | null
  readonly pid: number
  readonly uptimeSec: number
  readonly exitCode: number
  readonly lastError: string
}

/**
 * getId — функція, що повертає поточний serverId (не сам рядок), щоб хук
 * міг реагувати на зміну ID у батьківському $effect так само, як на
 * будь-який інший $derived/$state вираз.
 */
export function useServerStatus(getId: () => string): LiveServerStatus {
  const state = $state<{
    status: string
    progress: ServerInstallProgress | null
    pid: number
    uptimeSec: number
    exitCode: number
    lastError: string
  }>({ status: '', progress: null, pid: 0, uptimeSec: 0, exitCode: 0, lastError: '' })

  $effect(() => {
    const serverId = getId()
    if (!serverId) return

    const offStatus = on('server:status', (data: any) => {
      if (data.serverId !== serverId) return
      state.status = data.status || ''
      state.pid = data.pid || 0
      state.uptimeSec = data.uptimeSec || 0
      state.exitCode = data.exitCode || 0
      state.lastError = data.lastError || ''
      if (data.status !== 'installing') state.progress = null
    })
    const offProgress = on('server:install-progress', (data: any) => {
      if (data.serverId !== serverId) return
      state.progress = { stage: data.stage || 'core', percent: data.percent ?? -1, message: data.message || '' }
    })

    // Знімок поточного статусу при монтуванні/зміні serverId — інакше до
    // першої події банер показував би "" замість реального стану.
    App.GetServerStatus(serverId).then((s: any) => {
      state.status = s?.status || 'stopped'
      state.pid = s?.pid || 0
      state.uptimeSec = s?.uptimeSec || 0
      state.exitCode = s?.exitCode || 0
      state.lastError = s?.lastError || ''
    }).catch(() => {})

    return () => { offStatus?.(); offProgress?.() }
  })

  return state
}
