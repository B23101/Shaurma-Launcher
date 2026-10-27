// ── Сервер як ЦІЛЬ встановлення контенту з браузера ────────────────────
// Єдине джерело правди для всіх місць, де в розділі «Браузер контенту»
// можна вибрати куди ставити (сайдбар браузера, сторінка проєкту,
// діалог «Встановити»). Раніше кожне місце мало свою версію (або не мало
// серверів взагалі — сторінка проєкту вміла вибирати лише збірки).
//
// Правила (дзеркалять backend/server_browser_api.go → serverBrowserTarget):
//   • «Моди»   → лише сервери з мод-ядром (fabric/forge/neoforge/quilt);
//   • «Плагіни» → лише сервери з плагін-ядром (paper/purpur/velocity);
//   • сервер, що не підтримує вибраний тип контенту, у списку НЕ показуємо
//     (vanilla не тримає ні модів, ні плагінів).
import type { ServerView } from '../types'
import { CORE_META, MOD_CORES, PLUGIN_CORES } from './constants'

// Мінімальний опис сервера для браузера. Збагачує старий
// {id,name,mcVersion,coreLabel} справжньою іконкою і кольором.
export interface BrowserServer {
  id: string
  name: string
  mcVersion: string
  // core — id ядра (fabric/forge/neoforge/quilt/paper/purpur/velocity/vanilla).
  // Це і є «тип сервера» для фільтра.
  core: string
  coreLabel?: string
  // launcherIconData — іконка ЛАУНЧЕРА (окреме зображення без ресайзу) —
  // найвищий пріоритет у UI (дзеркало PackRow.iconData для збірок).
  launcherIconData?: string
  // icon — пресет-іконка лаунчера (Tabler, напр. "ti-server-2") — дзеркало
  // PackRow.icon. Показується, коли PNG лаунчера немає.
  icon?: string
  // iconData — ПРОТОКОЛЬНА іконка сервера (server-icon.png 64×64), якщо її
  // задано. Це fallback-джерело картинки для UI лаунчера (іконка, яку
  // гравець задав для списку серверів у грі, теж має сенс показувати).
  iconData?: string
  // color — власний колір сервера, якщо заданий.
  color?: string
}

// Що саме шукаємо: вкладка браузера → тип контенту сервера.
export type ServerContentKind = 'mods' | 'plugins'

// Вкладка браузера → чи має вона серверний аналог. 'mod' → mods,
// 'plugin' → plugins, решта (модпаки/ресурспаки/шейдери/датапаки) —
// сервер їх не тримає, тож null.
export function kindForTab(tab: string): ServerContentKind | null {
  if (tab === 'mod') return 'mods'
  if (tab === 'plugin') return 'plugins'
  return null
}

// Лоадери Modrinth, які означають ПЛАГІН (а не мод). Публічний API
// Modrinth для Paper/Velocity-плагінів нерідко віддає project_type="mod",
// тож «плагін це чи мод» надійніше визначати за лоадерами проєкту.
const PLUGIN_LOADER_IDS = new Set(['paper', 'purpur', 'velocity', 'bukkit', 'spigot', 'folia', 'sponge', 'waterfall', 'bungeecord'])
const MOD_LOADER_IDS = new Set(['fabric', 'forge', 'neoforge', 'quilt', 'liteloader', 'rift'])

// Ефективна вкладка проєкту для серверів: 'plugin' | 'mod' | інше як є.
// Якщо проєкт заявлено як 'mod', але ВСІ його лоадери — плагінні
// (velocity/paper/…), це плагін. І навпаки — не чіпаємо, якщо є мод-лоадер.
export function effectiveServerTab(projectType: string, loaders: string[] | undefined | null): string {
  if (projectType !== 'mod' && projectType !== 'plugin') return projectType
  const ls = (loaders ?? []).map(l => (l ?? '').toLowerCase())
  const hasPlugin = ls.some(l => PLUGIN_LOADER_IDS.has(l))
  const hasMod = ls.some(l => MOD_LOADER_IDS.has(l))
  if (hasPlugin && !hasMod) return 'plugin'
  if (hasMod && !hasPlugin) return 'mod'
  return projectType
}

// Чи підтримує ядро цей вид контенту.
export function coreSupports(core: string, kind: ServerContentKind): boolean {
  const c = (core ?? '').toLowerCase()
  return kind === 'mods' ? MOD_CORES.has(c) : PLUGIN_CORES.has(c)
}

// Лоадер для пошуку контенту цього виду на цьому ядрі (те саме, що робить
// бекенд у serverBrowserTarget). '' = ядро цей вид контенту не підтримує.
export function loaderForServer(core: string, kind: ServerContentKind): string {
  const c = (core ?? '').toLowerCase()
  if (!coreSupports(c, kind)) return ''
  return c
}

// Фільтр списку серверів для вкладки. Для вкладок без серверного аналога
// (шейдери/ресурспаки/датапаки/модпаки) повертає порожній список — сервер
// їх не тримає, тож і пропонувати його ціллю немає сенсу.
export function serversForTab(list: BrowserServer[], tab: string): BrowserServer[] {
  const kind = kindForTab(tab)
  if (!kind) return []
  return list.filter(s => coreSupports(s.core, kind))
}

// ── ЄДИНЕ джерело правди для візуалу сервера ────────────────────────────
// Той самий принцип пріоритету, що у збірки (PackPicker: PNG → пресет →
// дефолт), плюс два серверні fallback'и (протокольна іконка гри → іконка
// ядра). Використовується ВСІМА місцями, де малюється сервер, щоб не
// лишалось острівців старої поведінки (прямі CORE_META[core].icon).
//   1. launcherIconData — PNG лаунчера (найвищий пріоритет, як у збірок)
//   2. icon             — пресет Tabler
//   3. iconData         — протокольна server-icon.png
//   4. іконка ядра      — CORE_META (Fabric/Paper/…)
//   5. ti-server-2      — нейтральна заглушка
// Колір: власний колір сервера → колір ядра → --orange.
export type ServerVisualInput = Pick<BrowserServer, 'core' | 'color'> &
  Partial<Pick<BrowserServer, 'launcherIconData' | 'icon' | 'iconData'>>

export function serverVisual(s: ServerVisualInput): { src?: string; ti: string; color: string } {
  const meta = CORE_META[(s.core ?? '').toLowerCase()]
  const color = s.color || meta?.color || 'var(--orange)'
  if (s.launcherIconData) return { src: s.launcherIconData, ti: '', color }
  if (s.icon) return { src: undefined, ti: s.icon, color }
  if (s.iconData) return { src: s.iconData, ti: '', color }
  if (meta?.icon) return { src: meta.icon, ti: meta.ti, color }
  return { src: undefined, ti: 'ti-server-2', color }
}

// Підпис ядра для відображення («Fabric», «Paper»…).
export function coreName(s: Pick<BrowserServer, 'core' | 'coreLabel'>): string {
  return s.coreLabel || CORE_META[(s.core ?? '').toLowerCase()]?.label || s.core
}

// Конвертація ServerView (бекенд) → BrowserServer (браузер).
export function toBrowserServer(s: ServerView | any): BrowserServer {
  return {
    id: s.id,
    name: s.name,
    mcVersion: s.mcVersion,
    core: s.core ?? '',
    coreLabel: s.coreLabel,
    launcherIconData: s.launcherIconData,
    icon: s.icon,
    iconData: s.iconData,
    color: s.color,
  }
}

