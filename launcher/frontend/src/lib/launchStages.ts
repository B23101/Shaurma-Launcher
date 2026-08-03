// ── Спільні стадії запуску збірки (launch:progress) ──────────────────────
// checking → worker_update → java → loader → done. Цей список і підписи
// використовують ОДНОЧАСНО App.svelte (міні-прогрес у sidebar/тайлах),
// PackInstancePage.svelte (прогрес-бар на сторінці збірки) і GameConsole —
// тому вони винесені сюди, щоб кольори/назви стадій не роз'їжджались.
import { t } from './i18n.svelte'
import type { LaunchProgress } from './types'

export const LAUNCH_STAGES: { key: string; color: string }[] = [
  { key: 'checking', color: 'var(--yellow)' },
  { key: 'worker_update', color: 'var(--blue)' },
  { key: 'java', color: 'var(--red)' },
  { key: 'loader', color: 'var(--green)' }
]

export function launchStageLabel(stage?: string): string {
  switch (stage) {
    case 'checking': return t('launch.stage.checking')
    case 'worker_update': return t('launch.stage.workerUpdate')
    case 'java': return t('launch.stage.java')
    case 'loader': return t('launch.stage.loader')
    default: return t('launch.stage.checking')
  }
}

export function launchStageColor(stage?: string): string {
  return LAUNCH_STAGES.find(s => s.key === stage)?.color ?? 'var(--orange)'
}

export function launchStageIndex(stage?: string): number {
  return LAUNCH_STAGES.findIndex(x => x.key === stage)
}

// Загальний % барів запуску: кожна стадія = слот (25%), пройдені = повні
// слоти, поточна = її % від слота (без % — пів слота, щоб бар «жив»).
export function launchStagePct(lp?: LaunchProgress | null): number {
  const idx = lp ? launchStageIndex(lp.stage) : -1
  if (idx < 0) return 0
  const slot = 100 / LAUNCH_STAGES.length
  const pct = lp.percent ?? -1
  const within = pct >= 0 ? Math.min(pct, 100) : 50
  return Math.min(100, Math.round(idx * slot + (within / 100) * slot))
}

// Підпис стадії якання файлів збірки (sync:progress): «Завантаження…»,
// «Розпаковка…», «Завершення…» — щоб процес не виглядав завислим.
export function downloadStageLabel(stage?: string): string {
  switch (stage) {
    case 'manifest_fetched': return t('sync.stage.manifest')
    case 'extracting': return t('sync.stage.extracting')
    case 'finalizing': return t('sync.stage.finalizing')
    default: return t('sync.stage.downloading')
  }
}
