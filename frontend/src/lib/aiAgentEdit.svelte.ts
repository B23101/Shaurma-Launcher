// Стан сесій редагування збірок ШІ-агентом (backend/ai_agent_edit.go).
// Бекенд шле подію aiagent:edit зі знімком однієї сесії; при старті питаємо всі наявні.

import { App, on } from './wails'

export type AIEditChange = { time: number; kind: string; summary: string; file?: string; ok: boolean }
export type AIEditSession = {
  packId: string
  name: string
  client: string
  started: number
  last: number
  active: boolean
  current: string
  changes: AIEditChange[]
}

let sessions = $state<Record<string, AIEditSession>>({})
let inited = false

export function initAIEdit() {
  if (inited) return
  inited = true
  App.GetAIAgentEditSessions()
    .then((list: AIEditSession[]) => {
      const next: Record<string, AIEditSession> = {}
      for (const s of list ?? []) next[s.packId] = s
      sessions = next
    })
    .catch(() => {})
  on('aiagent:edit', (s: AIEditSession) => {
    if (s?.packId) sessions = { ...sessions, [s.packId]: s }
  })
}

/** Короткий підпис, якщо збіркою зараз керує ШІ (для сайдбару, «Моїх збірок» і кнопки «Грати»); інакше null. */
export function aiBusyLabel(packId: string): string | null {
  const s = sessions[packId]
  if (!s || !s.active) return null
  return s.current || s.changes[0]?.summary || 'ШІ керує збіркою'
}

export const aiEdit = {
  get(packId: string): AIEditSession | undefined {
    return sessions[packId]
  },
  /** Закрити вікно завершеної сесії. */
  dismiss(packId: string) {
    const { [packId]: _removed, ...rest } = sessions
    sessions = rest
    App.DismissAIAgentEdit(packId).catch(() => {})
  },
}
