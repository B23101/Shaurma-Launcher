// ── Спільні стадії запуску збірки (launch:progress) ──────────────────────
// checking → worker_update → java → loader → done. Цей список і підписи
// використовують ОДНОЧАСНО App.svelte (міні-прогрес у sidebar/тайлах),
// PackInstancePage.svelte (прогрес-бар на сторінці збірки) і GameConsole —
// тому вони винесені сюди, щоб кольори/назви стадій не роз'їжджались.
import { t } from './i18n.svelte'
import type { LaunchProgress } from './types'

// icon — іконка процесу (Tabler), weight — частка стадії в загальному барі:
// перевірка й джерела модів майже миттєві, тож основний «вантаж» бару —
// файли збірки, Java і лоадер.
export const LAUNCH_STAGES: { key: string; color: string; icon: string; weight: number }[] = [
  { key: 'checking',      color: 'var(--yellow)', icon: 'ti-shield-check',   weight: 5 },
  { key: 'mod_sources',   color: 'var(--violet)', icon: 'ti-puzzle',         weight: 5 },
  { key: 'worker_update', color: 'var(--blue)',   icon: 'ti-cloud-download', weight: 35 },
  { key: 'java',          color: 'var(--red)',    icon: 'ti-coffee',         weight: 20 },
  { key: 'loader',        color: 'var(--green)',  icon: 'ti-tool',           weight: 35 }
]

const LAUNCH_WEIGHT_TOTAL = LAUNCH_STAGES.reduce((n, s) => n + s.weight, 0)

export function launchStageLabel(stage?: string): string {
  switch (stage) {
    case 'checking': return t('launch.stage.checking')
    case 'mod_sources': return t('launch.stage.modSources')
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

export function launchStageIcon(stage?: string): string {
  return LAUNCH_STAGES.find(s => s.key === stage)?.icon ?? 'ti-rocket'
}

// Відрізок бару (0-100), який займає стадія: [start, end].
export function launchStageSpan(stage?: string): { start: number; end: number } {
  const idx = launchStageIndex(stage)
  if (idx < 0) return { start: 0, end: 0 }
  let before = 0
  for (let i = 0; i < idx; i++) before += LAUNCH_STAGES[i].weight
  return {
    start: (before / LAUNCH_WEIGHT_TOTAL) * 100,
    end: ((before + LAUNCH_STAGES[idx].weight) / LAUNCH_WEIGHT_TOTAL) * 100
  }
}

// Чи відомий реальний % поточної стадії (бекенд шле -1, коли невідомий).
export function launchStageKnown(lp?: LaunchProgress | null): boolean {
  return !!lp && launchStageIndex(lp.stage) >= 0 && (lp.percent ?? -1) >= 0
}

// Загальний % запуску: пройдені стадії = їхні повні ваги, поточна = її
// реальний % від своєї ваги. Коли % стадії невідомий, бар стоїть на початку
// стадії (нічого не вигадуємо) — рух «в процесі» малює сам компонент.
export function launchStagePct(lp?: LaunchProgress | null): number {
  if (!lp || launchStageIndex(lp.stage) < 0) return 0
  const { start, end } = launchStageSpan(lp.stage)
  const within = launchStageKnown(lp) ? Math.min(lp.percent as number, 100) : 0
  return Math.min(100, Math.round(start + (within / 100) * (end - start)))
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

// ── Стадії синхронізації файлів (sync:progress) ──────────────────────────
// downloading → manifest_fetched → extracting → finalizing. За аналогією
// з LAUNCH_STAGES: свій колір/іконка/клас на кожну стадію, щоб .sp-st/
// .it-st-індикатор і glow працювали так само, як для запуску (checking/
// worker_update/java/loader), а не лише текстовим підписом під прогресом.
export const DOWNLOAD_STAGES: { key: string; color: string; icon: string; cls: string; glow: string }[] = [
  { key: 'downloading',       color: 'var(--orange)', icon: 'ti-download',      cls: 'st-dl-download',  glow: 'glow-orange' },
  { key: 'manifest_fetched',  color: 'var(--blue)',   icon: 'ti-file-download', cls: 'st-dl-manifest',  glow: 'glow-blue' },
  { key: 'extracting',        color: 'var(--yellow)', icon: 'ti-archive',       cls: 'st-dl-extracting', glow: 'glow-yellow' },
  { key: 'finalizing',        color: 'var(--green)',  icon: 'ti-check',         cls: 'st-dl-finalizing', glow: 'glow-green' },
]

export function downloadStageColor(stage?: string): string {
  return DOWNLOAD_STAGES.find(s => s.key === stage)?.color ?? 'var(--orange)'
}

export function downloadStageIcon(stage?: string): { icon: string; cls: string } {
  const s = DOWNLOAD_STAGES.find(s => s.key === stage) ?? DOWNLOAD_STAGES[0]
  return { icon: s.icon, cls: s.cls }
}

export function downloadStageGlow(stage?: string): string {
  return DOWNLOAD_STAGES.find(s => s.key === stage)?.glow ?? 'glow-orange'
}
