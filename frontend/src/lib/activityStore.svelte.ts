// Довгі операції лаунчера (бекенд: activity_api.go) — завантаження збірок, передачі друзям,
// встановлення ядра сервера, міграції, експорт… Живить індикатор активності у верхній панелі
// (TransferWidget). Бекенд шле повний список подією activity:changed.
import { App, on } from './wails'

export type ActivityState = 'running' | 'paused' | 'waiting' | 'error' | 'done'

export interface ActivityItem {
  id: string
  kind: string
  title: string
  detail?: string
  /** 0..100; -1 — невідомо (нескінченний бар). */
  percent: number
  state: ActivityState
  target?: string
  startedAt: number
  updatedAt: number
}

export const activityState = $state<{ items: ActivityItem[] }>({ items: [] })

let started = false
export function initActivity(): void {
  if (started) return
  started = true
  on('activity:changed', (list: ActivityItem[] | null) => (activityState.items = list ?? []))
  App.ListActivities().then(list => (activityState.items = list ?? [])).catch(() => {})
}

// Іконки видів операцій (Tabler).
const ICONS: Record<string, string> = {
  'download': 'ti-download',
  'share-in': 'ti-download',
  'share-out': 'ti-upload',
  'server-install': 'ti-server-bolt',
  'server-backup': 'ti-archive',
  'launch': 'ti-coffee',
  'pack-install': 'ti-package',
  'mods-migrate': 'ti-arrows-exchange',
  'migrate': 'ti-transfer-in',
  'data-migration': 'ti-folder-symlink',
  'duplicate': 'ti-copy',
  'export': 'ti-file-export',
  'repair': 'ti-first-aid-kit',
  'mod-sources': 'ti-folder-cog',
  'bundle-export': 'ti-file-export',
}
export const activityIcon = (kind: string) => ICONS[kind] ?? 'ti-loader-2'
