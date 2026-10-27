// ── Спільні хелпери для серверних компонентів ─────────────────────────
// Статус → колір/підпис/іконка. Раніше ці ж функції жили копіпастом у
// ServersLibraryPage.svelte, ServerInstancePage.svelte та старому
// ServerConsolePanel.svelte (видалений — його замінює
// servers/tabs/ServerConsoleTab.svelte) — тепер одна реалізація.
import type { ServerView } from '../types'
import { CORE_META } from './constants'
import { serverVisual } from './serverTargets'
import { t } from '../i18n.svelte'

export function statusColor(s: string): string {
  switch (s) {
    case 'running': return '#22c55e'
    case 'installing': return 'var(--orange)'
    case 'starting': case 'stopping': return '#eab308'
    case 'crashed': case 'error': return '#ef4444'
    default: return 'var(--text-dim)'
  }
}

// statusLabel — єдине місце, де статус сервера перетворюється на текст.
// Раніше рядки були тут хардкодом українською, через що картки серверів
// лишались українськими навіть при англійській мові лаунчера. Тепер ключі
// t(), а реактивність зберігається: t() читає currentLang під час рендеру.
export function statusLabel(s: string): string {
  switch (s) {
    case 'running': return t('servers.status.running')
    case 'installing': return t('servers.status.installing')
    case 'starting': return t('servers.status.starting')
    case 'stopping': return t('servers.status.stopping')
    case 'crashed': return t('servers.status.crashed')
    case 'error': return t('servers.status.error')
    default: return t('servers.status.stopped')
  }
}

export function formatUptime(sec: number): string {
  if (!sec || sec <= 0) return ''
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  const s = sec % 60
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${String(s).padStart(2, '0')}` : `${m}:${String(s).padStart(2, '0')}`
}

export function formatUptimeShort(sec: number): string {
  if (!sec || sec <= 0) return ''
  const h = Math.floor(sec / 3600)
  const m = Math.floor((sec % 3600) / 60)
  return h > 0 ? t('servers.common.uptimeHM', { h, m }) : t('servers.common.uptimeM', { m })
}

// serverIcon — тонка обгортка над ЄДИНИМ serverVisual() (serverTargets.ts),
// щоб бібліотека/картки/консоль і список цілей встановлення не могли
// розійтись у пріоритеті PNG/пресет/протокольна іконка/іконка ядра.
export function serverIcon(srv: ServerView): { src?: string; ti: string; color: string } {
  return serverVisual({
    core: srv.core,
    color: srv.color,
    launcherIconData: srv.launcherIconData,
    icon: srv.icon,
    iconData: srv.iconData,
  })
}

export function coreLabel(srv: ServerView): string {
  return srv.coreLabel || CORE_META[srv.core]?.label || srv.core
}

// Текст під назвою в картці: ядро · версія · порт (+ loaderVer)
export function metaLine(srv: ServerView): string {
  const parts: string[] = []
  if (srv.core === 'velocity') {
    parts.push(t('servers.common.velocityProxy'))
    if (srv.loaderVer) parts.push(srv.loaderVer)
  } else {
    parts.push(coreLabel(srv))
    if (srv.mcVersion) parts.push(srv.mcVersion)
    if (srv.loaderVer) parts.push(srv.loaderVer)
  }
  if (srv.port) parts.push(`:${srv.port}`)
  return parts.join(' · ')
}

// Detect log-line level з тексту (те саме, що було в консолі).
export function detectLevel(text: string): 'error' | 'warn' | 'info' {
  if (/error|exception|fatal|crash|failed|unable to|outofmemory|severe|\[stderr\]|\[error\]|throwable/i.test(text)) return 'error'
  if (/warn|warning|can't keep up/i.test(text)) return 'warn'
  return 'info'
}

// Формує градієнт-фон банера в стилі збірки — на основі кольору сервера.
export function bannerBg(color: string): string {
  return `linear-gradient(135deg, color-mix(in srgb, ${color} 52%, #22160b), color-mix(in srgb, ${color} 20%, #0e0905) 65%, #0a0604)`
}
