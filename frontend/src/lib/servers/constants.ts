// ── Мета ядер серверів ────────────────────────────────────────────────
// Спільна мапа CORE_META для всіх серверних компонентів (бібліотека,
// сторінка сервера, іконки в картках). Раніше цей же обʼєкт був
// продубльований у ServersLibraryPage і ServerInstancePage — тепер один
// джерело правди.
import vanillaIcon from '../../assets/images/vanilla_icon.png'
import fabricIcon from '../../assets/images/fabric_icon.png'
import forgeIcon from '../../assets/images/forge_icon.png'
import neoforgeIcon from '../../assets/images/neoforge_icon.png'
import quiltIcon from '../../assets/images/quilt_icon.png'
import papermcIcon from '../../assets/images/papermc.svg'
import purpurIcon from '../../assets/images/purpur.png'
import velocityIcon from '../../assets/images/velocity.webp'

export type CoreMeta = { label: string; icon?: string; ti: string; color: string }

export const CORE_META: Record<string, CoreMeta> = {
  vanilla:  { label: 'Vanilla',  icon: vanillaIcon,  ti: '', color: '#8b8b8b' },
  paper:    { label: 'Paper',    icon: papermcIcon,  ti: '', color: '#10b981' },
  purpur:   { label: 'Purpur',   icon: purpurIcon,   ti: '', color: '#ef4444' },
  fabric:   { label: 'Fabric',   icon: fabricIcon,   ti: '', color: '#ff8a00' },
  forge:    { label: 'Forge',    icon: forgeIcon,    ti: '', color: '#d08b5b' },
  neoforge: { label: 'NeoForge', icon: neoforgeIcon, ti: '', color: '#ff6b35' },
  quilt:    { label: 'Quilt',    icon: quiltIcon,    ti: '', color: '#9d7cff' },
  velocity: { label: 'Velocity', icon: velocityIcon, ti: '', color: '#3b82f6' },
}

// Ті самі списки, що використовувалися в ServerInstancePage — які ядра
// підтримують моди / плагіни / світи. Уніфіковано в одному місці.
export const MOD_CORES = new Set(['fabric', 'forge', 'neoforge', 'quilt'])
export const PLUGIN_CORES = new Set(['paper', 'purpur', 'velocity'])

// Статуси, які вважаємо "живими" (сервер онлайн або переходить у/з
// онлайну) — для фільтра "онлайн/офлайн" у бібліотеці й індикаторів.
export const ONLINE_STATUSES = new Set(['running', 'starting', 'stopping'])

// ── server.properties ──────────────────────────────────────────────────
// Групи тепер визначає БЕКЕНД (properties_schema.go, поле group) — раніше тут
// лежав окремий список ключів, який розходився зі схемою, і половина
// параметрів потрапляла в «Інше» чи малювалась текстовим полем. Тут лишився
// тільки порядок вкладок та їхні іконки; підписи — з i18n (props.group.<id>).
export const PROP_GROUP_ORDER = ['players', 'world', 'gameplay', 'status', 'network', 'performance', 'remote', 'management', 'resources', 'logging', 'other', 'legacy']
export const PROP_GROUP_ICON: Record<string, string> = {
  players: 'ti-users',
  world: 'ti-world',
  gameplay: 'ti-sword',
  status: 'ti-list-details',
  network: 'ti-network',
  performance: 'ti-gauge',
  remote: 'ti-terminal-2',
  management: 'ti-api',
  resources: 'ti-package',
  logging: 'ti-notes',
  other: 'ti-dots',
  legacy: 'ti-history',
}
export const PROP_UNGROUPED = 'other'
export const PROP_LEGACY = 'legacy'

// Ключі server.properties, які в цьому розділі не показуємо: вони вже мають
// власний контрол в іншому місці UI.
//
// server-port — єдине джерело правди для порту, але редагується він у блоці
// «Порт» на цій же сторінці (звідти бекенд пише і в store, і в
// server.properties). Показувати його ще й тут означало б ДВА поля для
// одного значення.
export const PROP_HIDDEN = new Set<string>(['server-port'])

// Старі числові значення, які сервер мовчки перетворює на назви.
export const LEGACY_ENUM_NUMBERS: Record<string, string[]> = {
  gamemode: ['survival', 'creative', 'adventure', 'spectator'],
  difficulty: ['peaceful', 'easy', 'normal', 'hard'],
}

// Голосові моди/плагіни: якщо такий файл лежить у mods/ чи plugins/
// сервера, а UDP-тунель у P2P вимкнено — голосового чату в грі не буде,
// і гравець має про це знати (див. props.voice.* в i18n).
const VOICE_MOD_MARKERS = ['voicechat', 'simplevoicechat', 'plasmovoice', 'voice-chat', 'simvoicechat']
export function isVoiceMod(fileName: string): boolean {
  const n = fileName.toLowerCase()
  return VOICE_MOD_MARKERS.some(m => n.includes(m))
}
