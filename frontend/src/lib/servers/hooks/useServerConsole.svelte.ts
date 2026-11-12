// Local status and command feedback only. History is fetched by PagedConsole.
import { onDestroy } from 'svelte'
import { App, on } from '../../wails'
import type { ServerInstallProgress } from '../../types'
import type { LogLine } from '../components/ConsoleView.svelte'

// Only local command feedback lives here; history is paged from the backend.
const CONSOLE_MAX_LINES = 20

export interface ServerConsoleHandle {
  readonly lines: LogLine[]
  readonly status: string
  // statusLoaded — false, поки status — лише дефолтне "stopped" хука, а
  // НЕ підтверджене реальне значення з бекенду (GetServerStatus ще не
  // відповів). ServerConsoleTab чекає true перед автостартом — інакше
  // ефект бачить дефолтний "stopped" за одну мить ДО реальної відповіді
  // і стартує сервер, який насправді вже запущений (звідси помилка
  // "сервер вже запущено" при швидкому переході на вкладку консолі).
  readonly statusLoaded: boolean
  readonly installProgress: ServerInstallProgress | null
  addLine: (level: string, text: string) => void
  clear: () => void
}

export function useServerConsole(getId: () => string): ServerConsoleHandle {
  const state = $state<{
    status: string
    statusLoaded: boolean
    installProgress: ServerInstallProgress | null
  }>({ status: 'stopped', statusLoaded: false, installProgress: null })
  // Оновлюємо заміною масиву.
  let lines = $state.raw<LogLine[]>([])

  // seq — ЄДИНЕ джерело id для КОЖНОГО рядка (і зі знімка, і живого),
  // а не два незалежні лічильники (бекендовий line.id + локальний
  // nextId). Той старий підхід і спричиняв
  // each_key_duplicate у ConsoleView (#each lines as line (line.id)):
  // при перемиканні вкладок/гонці між асинхронним GetServerConsoleSnapshot
  // і вже прилетілими live-рядками (server:line) локальний nextId міг
  // видати те саме число, що вже використане в іншому місці, а
  // wholesale-заміна state.lines у знімку не чистила pending чергу.
  // Тепер id — це номер по порядку ВСТАВКИ в state.lines цього
  // конкретного інстансу хука, тож дублікат ключа структурно неможливий.
  let seq = 0
  function nextSeq() { return seq++ }

  let pending: LogLine[] = []
  let flushScheduled = false
  let flushRaf = 0
  let disposed = false

  function flush() {
    flushScheduled = false
    if (pending.length === 0) return
    for (const l of pending) l.id = nextSeq()
    const merged = lines.concat(pending)
    pending = []
    lines = merged.length > CONSOLE_MAX_LINES ? merged.slice(merged.length - CONSOLE_MAX_LINES) : merged
  }

  function addLine(level: string, text: string) {
    // id проставляється у flush() (в порядку реальної вставки) — тут 0
    // як тимчасове значення, ще не показане користувачу.
    if (disposed) return
    pending.push({ id: 0, time: new Date().toTimeString().slice(0, 8), level, text })
    if (pending.length >= CONSOLE_MAX_LINES * 2) pending = pending.slice(-CONSOLE_MAX_LINES)
    if (!flushScheduled) { flushScheduled = true; flushRaf = requestAnimationFrame(flush) }
  }

  let prevId = ''
  // token — захист від застарілих відповідей: якщо серверId (або сам
  // хук) змінюється, поки GetServerConsoleSnapshot ще в польоті,
  // прострочена відповідь НЕ повинна перезаписати вже свіжіший стан.
  let snapshotToken = 0
  onDestroy(() => {
    disposed = true
    snapshotToken++
    cancelAnimationFrame(flushRaf)
    pending = []
  })
  $effect(() => {
    const serverId = getId()
    if (!serverId) return

    if (serverId !== prevId) {
      prevId = serverId
      state.installProgress = null
      state.status = 'stopped'
      state.statusLoaded = false
      // Скидаємо чергу live-рядків попереднього серверу — інакше вони
      // могли б домішатись у flush() ПІСЛЯ знімка нового сервера.
      pending = []
    }
    const offLine = () => {}

    const offStatus = on('server:status', (data: any) => {
      if (data.serverId !== serverId) return
      state.status = data.status || 'stopped'
      state.statusLoaded = true
      if (state.status !== 'installing') state.installProgress = null
      if (data.lastError) addLine('error', `[SYSTEM] ${data.lastError}`)
    })
    const offProgress = on('server:install-progress', (data: any) => {
      if (data.serverId !== serverId) return
      state.installProgress = { stage: data.stage || 'core', percent: data.percent ?? -1, message: data.message || '' }
    })

    let active = true
    App.GetServerStatus(serverId).then((s: any) => {
      if (!active) return
      state.status = s?.status || 'stopped'
      state.statusLoaded = true
    }).catch(() => { if (active) state.statusLoaded = true })

    return () => { active = false; snapshotToken++; offLine?.(); offStatus?.(); offProgress?.() }
  })

  // Очищення ЛИШЕ локального буфера перегляду (кнопка «Кошик» у тулбарі,
  // як у консолі збірок): бекендовий буфер сервера не чіпаємо, тож
  // наступний вхід на вкладку поверне повний лог зі знімка.
  function clear() {
    pending = []
    lines = []
  }

  return {
    get lines() { return lines },
    get status() { return state.status },
    get statusLoaded() { return state.statusLoaded },
    get installProgress() { return state.installProgress },
    addLine,
    clear,
  }
}
