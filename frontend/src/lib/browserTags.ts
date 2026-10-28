// Теги (категорії) проєктів Modrinth/CurseForge для карток і фільтрів
// браузера контенту — як Categories у GDLauncher. Modrinth віддає теги
// slug-ами («adventure», «game-mechanics») і змішує їх з лоадерами в
// categories пошуку; CurseForge — людськими назвами («Adventure and RPG»).
import { t } from './i18n.svelte'

// Лоадери/платформи, які Modrinth кладе в categories поряд із тегами.
const LOADER_TAGS = new Set([
  'fabric', 'forge', 'neoforge', 'quilt', 'liteloader', 'modloader', 'rift', 'babric', 'bta-babric',
  'java-agent', 'legacy-fabric', 'nilloader', 'ornithe', 'risugamis-modloader', 'minecraft', 'vanilla',
  'datapack', 'bukkit', 'spigot', 'paper', 'purpur', 'folia', 'sponge', 'velocity', 'bungeecord',
  'waterfall', 'iris', 'optifine', 'canvas', 'geyser',
])

// Лише справжні теги проєкту: без лоадерів, без дублікатів.
export function projectTags(categories: string[] | null | undefined): string[] {
  const out: string[] = []
  for (const c of categories ?? []) {
    if (!c || LOADER_TAGS.has(c.toLowerCase()) || out.includes(c)) continue
    out.push(c)
  }
  return out
}

// Людська назва тегу: переклад browser.tag.<slug> для Modrinth-slug-ів,
// інакше сама назва з великої літери (CurseForge вже віддає назви).
export function tagLabel(name: string): string {
  const key = 'browser.tag.' + name.toLowerCase()
  const v = t(key)
  if (v !== key) return v
  const pretty = name.replace(/[-_]+/g, ' ')
  return pretty.charAt(0).toUpperCase() + pretty.slice(1)
}
