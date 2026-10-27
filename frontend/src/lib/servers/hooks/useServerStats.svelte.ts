// ── useServerStats — жива статистика CPU/RAM/гравці (runes-хук) ─────────
// Поки сервер запущено — раз/секунду тягне GetServerStats(); зупиняється
// й скидає значення, щойно сервер не running. Раніше цей таймер жив
// прямо в ServerInstancePage — тепер одна реалізація для банера.
import { App } from '../../wails'
import type { ServerLiveStats } from '../../types'

export interface LiveServerStats {
  readonly stats: ServerLiveStats | null
}

/**
 * getId — поточний serverId, isRunning — чи варто зараз опитувати
 * бекенд (обидва як функції, щоб хук реагував на їхню зміну реактивно).
 */
export function useServerStats(getId: () => string, isRunning: () => boolean): LiveServerStats {
  const state = $state<{ stats: ServerLiveStats | null }>({ stats: null })

  $effect(() => {
    const serverId = getId()
    const running = isRunning()
    if (!serverId || !running) {
      state.stats = null
      return
    }
    let alive = true
    const poll = () => {
      App.GetServerStats(serverId).then((s: any) => { if (alive) state.stats = s }).catch(() => {})
    }
    poll()
    const timer = setInterval(poll, 1000)
    return () => { alive = false; clearInterval(timer) }
  })

  return state
}
