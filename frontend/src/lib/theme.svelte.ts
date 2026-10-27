// ── Акцентна система + теми + шрифти ─────────────────────────────────
//
// Лаунчер використовує CSS-змінні --orange / --orange-2 по всьому UI
// (кнопки, прогреси, активні стани). Система акцентів дає змогу
// міняти акцентний колір у РОБОЧОМУ стані: applyAccent(accent) просто
// перезаписує ці змінні на documentElement, і весь інтерфейс одразу
// перефарбовується. Жодних рестартів чи окремих тем — все через CSS.
//
// Теми: applyTheme('dark'|'light'|'oled'|'custom') встановлює
// data-theme на <html>. Базові значення змінних (--bg-app/--bg-card/
// --text тощо) описані в style.css через селектор html[data-theme=...],
// тому тут лише перемикання атрибута.
//
// Шрифти: applyFont(font, fontPath?) задає --font-ui та --font-mono.
// Для 'custom' завантажує TTF через @font-face із локального файлу
// (шлях приходить з Settings.FontPath).
//
// Щоб додати новий акцент: додати запис у ACCENTS (назва + кольори) і
// варіант у config.SettingsSchema() + i18n-ключ accent.<name>.

import { App, on } from './wails'
import type { Settings } from './types'
// Вбудовані шрифти лаунчера (папка assets/ttf): юзер може покласти сюди
// свої TTF — вони з'являться у списках шрифтів (інтерфейс і консоль) і
// зберуться в бінарник через Vite.
// Inter/JetBrains Mono — ДЕФОЛТНІ шрифти лаунчера (FONTS.inter нижче).
// Раніше для них не було жодного @font-face: --font-ui/--font-mono просто
// вказували назву сімейства ('Inter','JetBrains Mono') і покладались на
// те, що шрифт ВЖЕ встановлений в ОС. Segoe UI на Windows має дуже схожі
// метрики з Inter, тому різниця була непомітна; на Linux дефолтний
// sans-serif стільниці (Cantarell/Ubuntu/Noto Sans залежно від DE) сильно
// відрізняється — звідси «на різних ОС виглядає по-іншому». Файли ті самі,
// що вже використовує updater-frontend/index.html (там @font-face прямо
// в HTML, бо це окремий документ без Vite-модулів) — тепер шрифт
// однаковий і в вікні апдейтера, і в головному лаунчері, на всіх ОС.
import interRegularUrl from '../assets/ttf/Inter-Regular.ttf'
import interSemiBoldUrl from '../assets/ttf/Inter-SemiBold.ttf'
import interExtraBoldUrl from '../assets/ttf/Inter-ExtraBold.ttf'
import jetbrainsRegularUrl from '../assets/ttf/JetBrainsMono-Regular.ttf'
import jetbrainsSemiBoldUrl from '../assets/ttf/JetBrainsMono-SemiBold.ttf'

export interface AccentDef {
  name: string      // i18n key accent.<name>
  main: string      // основний колір (--orange)
  deep: string      // градієнт-партнер (--orange-2)
  rgb: string       // rgb-трійка для rgba(var(--orange-rgb), …)
  // gradient — «градієнтний» акцент: main і deep НАВМИСНЕ різні тони,
  // тому surface-елементи з linear-gradient(135deg, main, deep) (кнопки,
  // прогрес, лого) показують видимий двоколірний градієнт, а не темніший
  // відтінок того самого кольору. Сам swatch у пікері теж градієнтний.
  gradient?: boolean
}

export const ACCENTS: Record<string, AccentDef> = {
  orange: { name: 'accent.orange', main: '#ff8a00', deep: '#ff5e00', rgb: '255, 138, 0' },
  violet: { name: 'accent.violet', main: '#7b4dff', deep: '#5a2eff', rgb: '123, 77, 255' },
  green:  { name: 'accent.green',  main: '#10b981', deep: '#059669', rgb: '16, 185, 129' },
  blue:   { name: 'accent.blue',   main: '#3b82f6', deep: '#2563eb', rgb: '59, 130, 246' },
  red:    { name: 'accent.red',    main: '#ef4444', deep: '#dc2626', rgb: '239, 68, 68' },
  // Градієнтні акценти: дві РІЗНІ кольорові «точки» в одному акценті.
  sunset: { name: 'accent.sunset', main: '#ff7a3d', deep: '#ff3cac', rgb: '255, 122, 61', gradient: true },
  ocean:  { name: 'accent.ocean',  main: '#00b8ff', deep: '#7c5cff', rgb: '0, 184, 255', gradient: true },
  aurora: { name: 'accent.aurora', main: '#7f5cea', deep: '#00d4ff', rgb: '127, 92, 234', gradient: true },
}

export const DEFAULT_ACCENT = 'orange'
export const DEFAULT_THEME = 'dark'
export const DEFAULT_FONT = 'inter'

/**
 * Застосувати акцентний колір: перезаписує CSS-змінні на :root.
 * Для accent === 'custom' використовується пікер (settings.accentCustom*):
 *   mode === 'gradient' — customHex і customHex2 як дві точки градієнта,
 *   як завод-градієнтні акценти (sunset/ocean/aurora).
 *   mode === 'flat' (за замовч.) — один customHex, другий тон
 *   вираховується автозатемненням на 18% (стара поведінка, лишена як
 *   найпростіший дефолт — «просто клікнув колір і воно виглядає добре»).
 */
export function applyAccent(accent: string, customHex?: string, customMode?: string, customHex2?: string) {
  let a = ACCENTS[accent]
  if (accent === 'custom' && customHex && /^#[0-9a-fA-F]{6}$/.test(customHex)) {
    const { r, g, b } = hexToRgb(customHex)
    const useGradient = customMode === 'gradient' && customHex2 && /^#[0-9a-fA-F]{6}$/.test(customHex2)
    a = {
      name: 'accent.custom',
      main: customHex,
      deep: useGradient ? (customHex2 as string) : darkenHex(customHex, 0.82),
      rgb: `${r}, ${g}, ${b}`
    }
  }
  a = a ?? ACCENTS[DEFAULT_ACCENT]
  const root = document.documentElement
  root.style.setProperty('--orange', a.main)
  root.style.setProperty('--orange-2', a.deep)
  root.style.setProperty('--orange-rgb', a.rgb)
  // Готовий градієнтний рядок акценту (135deg) — для surface-елементів,
  // які хочуть використовувати акцент як градієнт, не дублюючи формулу.
  root.style.setProperty('--accent-grad', `linear-gradient(135deg, ${a.main}, ${a.deep})`)
}

function hexToRgb(hex: string): { r: number; g: number; b: number } {
  const n = parseInt(hex.slice(1), 16)
  return { r: (n >> 16) & 255, g: (n >> 8) & 255, b: n & 255 }
}

function darkenHex(hex: string, factor: number): string {
  const { r, g, b } = hexToRgb(hex)
  const f = (v: number) => Math.max(0, Math.round(v * factor)).toString(16).padStart(2, '0')
  return `#${f(r)}${f(g)}${f(b)}`
}

/**
 * Застосувати тему: 'dark' (за замовчуванням), 'light', 'oled' (справжній
 * чистий чорний для OLED-дисплеїв) або 'custom'.
 * Для 'custom' додатково задаємо кольори фону/карток/тексту через CSS-
 * змінні (--bg-app/--bg-card/--text тощо) — вони перекривають темну палітру
 * на documentElement, і весь інтерфейс одразу перефарбовується.
 */
let systemThemeMql: MediaQueryList | null = null
let systemThemeHandler: (() => void) | null = null

// 'system' слідує за темою ОС і оновлюється наживо при її зміні.
function stopSystemThemeWatch() {
  if (systemThemeMql && systemThemeHandler) systemThemeMql.removeEventListener('change', systemThemeHandler)
  systemThemeMql = null
  systemThemeHandler = null
}

export function applyTheme(theme: string, custom?: { bg?: string; card?: string; text?: string }) {
  const root = document.documentElement
  stopSystemThemeWatch()
  let resolved = theme
  if (theme === 'system' && typeof window !== 'undefined' && window.matchMedia) {
    const mql = window.matchMedia('(prefers-color-scheme: light)')
    resolved = mql.matches ? 'light' : 'dark'
    systemThemeMql = mql
    systemThemeHandler = () => root.setAttribute('data-theme', mql.matches ? 'light' : 'dark')
    mql.addEventListener('change', systemThemeHandler)
  }
  const t = resolved === 'light' ? 'light' : resolved === 'oled' ? 'oled' : 'dark'
  root.setAttribute('data-theme', t)

  // Кастомна тема: прибираємо старі inline-змінні та ставимо кольори юзера.
  if (theme === 'custom' && custom?.bg && custom?.card && custom?.text) {
    const bg = custom.bg
    const card = custom.card
    const text = custom.text
    root.style.setProperty('--bg-app', bg)
    root.style.setProperty('--bg-window', card)
    root.style.setProperty('--bg-card', card)
    root.style.setProperty('--bg-panel', card)
    root.style.setProperty('--bg-input', card)
    root.style.setProperty('--text', text)
    root.style.setProperty('--text-mute', text + 'b3') // 70% прозорості
    root.style.setProperty('--text-dim', text + '80')  // 50% прозорості
    root.style.setProperty('--border', text + '1c')    // ~11% прозорості
    root.style.setProperty('--border-hi', text + '33')
    root.style.setProperty('--hover', text + '14')
    root.style.setProperty('--hover-strong', text + '24')
  } else {
    // Повертаємось на дефолтну палітру: inline-змінні більше не потрібні.
    for (const v of ['--bg-app', '--bg-window', '--bg-card', '--bg-panel', '--bg-input', '--text', '--text-mute', '--text-dim', '--border', '--border-hi', '--hover', '--hover-strong']) {
      root.style.removeProperty(v)
    }
  }
}

// Вмикач/вимикач декоративних UI-анімацій (Налаштування → Вигляд).
// data-animations="off" на <html> — той самий підхід, що data-theme,
// а CSS (style.css) під цим атрибутом обнуляє тривалість/затримку всіх
// декоративних keyframes (rise-in, sb-pack*, nav-*, sni-sweep тощо) —
// той самий трюк, що вже є для prefers-reduced-motion, просто керований
// явним перемикачем користувача, а не системною налаштуванням ОС.
export function applyAnimations(enabled: boolean) {
  document.documentElement.setAttribute('data-animations', enabled ? 'on' : 'off')
}

// Performance Mode (Налаштування → Вигляд) — окремо від animationsEnabled:
// той лише обнуляє тривалість переходів/keyframes, а Performance Mode ще
// й знімає backdrop-filter: blur() з модалок/топбарів/карток (найдорожча
// GPU-композиція в Chromium/WebView2) та важкі фонові ефекти — для
// справді слабких машин, де самих переходів замало. data-performance-mode
// на <html>, той самий підхід, що data-animations/data-theme.
export function applyPerformanceMode(enabled: boolean) {
  document.documentElement.setAttribute('data-performance-mode', enabled ? 'on' : 'off')
}

// data-platform на <html> — з App.GetPlatform() (runtime.GOOS), викликається
// один раз при старті. НАВІЩО: на Linux рушій вікна — WebKitGTK (не
// Chromium/WebView2, як на Windows), і backdrop-filter:blur() у style.css
// (модалки, sticky-топбари, dropdown-меню — використовується масово) там
// рендериться без апаратної композиції так само гладко — кожен кадр із
// blur перераховується програмно, звідси "криві й дьоргані анімації" саме
// на Linux: будь-який transition/анімація поруч із blur-елементом рветься
// по кадрах. На відміну від Performance Mode (ручний перемикач користувача,
// той самий CSS-ефект знімає і на Windows теж) — це автоматичне, лише для
// platform === 'linux', без участі користувача: слабкість WebKitGTK у
// цьому — властивість платформи, а не щось, що варто змушувати кожного
// Linux-користувача вмикати вручну.
export function applyPlatform(platform: string) {
  document.documentElement.setAttribute('data-platform', platform)
}

// ── Вигляд у ДОДАТКОВИХ вікнах (консоль гри / консоль сервера) ─────────
// Кожне вікно — окремий документ зі своїм <html>: inline-змінні кастомної
// теми (--bg-app/--text/...) і data-* атрибути, які головне вікно ставить
// на свій documentElement, сюди НЕ переходять. Раніше консоль викликала
// applyTheme(s.theme) БЕЗ кольорів користувача → гілка 'custom' не
// спрацьовувала, а else-гілка ще й ЗНІМАЛА змінні → власний стиль
// скидався до дефолтної темної; так само applyAccent без режиму градієнта
// і другого кольору. Тепер усе застосовується ТИМИ САМИМИ викликами, що й
// у App.svelte, з одного місця.
export function applyAppearance(s: Settings) {
  const x = s as any
  applyAccent(s.accent, s.accentCustom, x.accentCustomMode, x.accentCustom2)
  applyTheme(s.theme, { bg: s.themeCustomBg, card: s.themeCustomCard, text: s.themeCustomText })
  applyAnimations(x.animationsEnabled !== false)
  applyPerformanceMode(!!x.performanceMode)
  applyFont(s.font, s.fontPath).catch(() => {})
}

/**
 * Синхронізує вигляд вікна з налаштуваннями лаунчера: застосовує зараз і
 * підписується на "settings:saved" (шле бекенд після SaveSettings), щоб
 * відкрита консоль перефарбовувалась наживо, коли тему міняють у
 * головному вікні. Повертає налаштування (для мови/шрифтів консолі) і
 * функцію відписки.
 */
export async function syncAppearance(): Promise<{ settings: Settings | null; off: () => void }> {
  let settings: Settings | null = null
  try {
    const [s, platform] = await Promise.all([
      App.GetSettings(),
      App.GetPlatform().catch(() => ''),
    ])
    settings = s
    applyPlatform(platform)
    applyAppearance(s)
  } catch { /* лишаються дефолти з style.css */ }
  const off = on('settings:saved', () => {
    App.GetSettings().then((s: Settings) => applyAppearance(s)).catch(() => {})
  })
  return { settings, off }
}

// Відомі шрифти: font-family значення для UI та моноширинного тексту.
// 'inter' — вбудований TTF лаунчера; 'system' — системні шрифти ОС;
// 'jetbrains' — моноширинний JetBrains Mono (як у логах); 'custom' —
// кастомний TTF, вказаний у Settings.FontPath. Додаткові системні шрифти
// (segoe/roboto/consolas/verdana/georgia) — готові шрифти Windows, не
// потребують файлів, дають користувачу більше вибору в «Вигляд».
export const FONTS: Record<string, { ui: string; mono: string }> = {
  inter:     { ui: "'Inter','Segoe UI',sans-serif", mono: "'JetBrains Mono','Consolas',monospace" },
  system:    { ui: "'Segoe UI',system-ui,sans-serif", mono: "'Consolas','Courier New',monospace" },
  jetbrains: { ui: "'JetBrains Mono','Inter',monospace", mono: "'JetBrains Mono','Consolas',monospace" },
  segoe:     { ui: "'Segoe UI',system-ui,sans-serif", mono: "'Consolas','Courier New',monospace" },
  roboto:    { ui: "'Roboto','Segoe UI',system-ui,sans-serif", mono: "'Roboto Mono','Consolas',monospace" },
  consolas:  { ui: "'Consolas','Segoe UI',monospace", mono: "'Consolas','Courier New',monospace" },
  verdana:   { ui: "'Verdana','Segoe UI',sans-serif", mono: "'Consolas','Courier New',monospace" },
  georgia:   { ui: "'Georgia','Times New Roman',serif", mono: "'Consolas','Courier New',monospace" },
  // Вбудовані шрифти з assets/ttf — імена сімейств з назв файлів.
  custom:    { ui: "'ShaurmaCustom','Inter','Segoe UI',sans-serif", mono: "'JetBrains Mono','Consolas',monospace" },
}

// Вбудовані шрифти, що збираються в бінарник (assets/ttf): value — ключ у
// FONTS, label — i18n-ключ назви (bundledFont.<value>). App.svelte додає їх
// у списки шрифтів (інтерфейс + консоль) після завантаження схеми.
export const BUNDLED_FONTS: { value: string; label: string }[] = [
]

// Реєструє @font-face для вбудованих шрифтів (assets/ttf) у поточному
// документі. Викликається при старті (App.svelte) і у вікні консолі.
// Ідемпотентно: faces перезаписуються тим самим вмістом.
function registerBundledFaces() {
  const styleId = 'shaurma-bundled-fonts'
  let style = document.getElementById(styleId) as HTMLStyleElement | null
  if (!style) {
    style = document.createElement('style')
    style.id = styleId
    document.head.appendChild(style)
  }
  style.textContent =
    `@font-face{font-family:'Inter';src:url(${interRegularUrl}) format('truetype');font-weight:400;font-display:swap;}` +
    `@font-face{font-family:'Inter';src:url(${interSemiBoldUrl}) format('truetype');font-weight:600;font-display:swap;}` +
    `@font-face{font-family:'Inter';src:url(${interExtraBoldUrl}) format('truetype');font-weight:800;font-display:swap;}` +
    `@font-face{font-family:'JetBrains Mono';src:url(${jetbrainsRegularUrl}) format('truetype');font-weight:400;font-display:swap;}` +
    `@font-face{font-family:'JetBrains Mono';src:url(${jetbrainsSemiBoldUrl}) format('truetype');font-weight:700;font-display:swap;}`
}

export function registerBundledFonts() {
  registerBundledFaces()
}

/**
 * fontFamilyFromPath — CSS-безпечне ім'я сімейства зі шляху до файлу
 * шрифту: "C:\\fonts\\Minecraft.ttf" → "Minecraft". Для шрифтів із теки
 * fonts (ListFonts) ім'я виводиться з назви файлу.
 */
export function fontFamilyFromPath(path: string): string {
  const base = (path.split(/[\\/]/).pop() ?? 'CustomFont').replace(/\.[^.]+$/, '')
  const clean = base.replace(/[^a-zA-Z0-9-]/g, '-').replace(/-+/g, '-').replace(/^-|-$/g, '')
  return clean || 'CustomFont'
}

// Реєструє @font-face у ПОТОЧНОМУ документі (вікно консолі або головний
// UI — кожне має свій документ). Файл передається бекендом як data: URL.
async function registerFontFace(styleId: string, family: string, path: string): Promise<boolean> {
  try {
    const dataUrl = await App.LoadFontFile(path)
    if (!dataUrl) return false
    let style = document.getElementById(styleId) as HTMLStyleElement | null
    if (!style) {
      style = document.createElement('style')
      style.id = styleId
      document.head.appendChild(style)
    }
    style.textContent = `@font-face{font-family:'${family}';src:url(${dataUrl}) format('truetype');font-display:swap;}`
    return true
  } catch (e) {
    console.error('font load failed', e)
    return false
  }
}

/**
 * Застосувати шрифт інтерфейсу.
 *  - вбудовані/системні ключі (inter/system/…): готовий стек з FONTS;
 *  - 'custom': TTF з Settings.FontPath (@font-face 'ShaurmaCustom');
 *  - 'file:<path>': шрифт із теки fonts (App.ListFonts) — ім'я сімейства
 *    виводиться з назви файлу (fontFamilyFromPath).
 */
export async function applyFont(font: string, fontPath?: string) {
  const key = font || DEFAULT_FONT
  const root = document.documentElement
  let spec = FONTS[key]

  // Шрифт із теки <dataDir>/fonts — значення "file:<абсолютний шлях>".
  if (key.startsWith('file:')) {
    const path = key.slice(5)
    const family = fontFamilyFromPath(path)
    const ok = await registerFontFace('shaurma-custom-font', family, path)
    if (ok) {
      root.style.setProperty('--font-ui', `'${family}','Inter','Segoe UI',sans-serif`)
      root.style.setProperty('--font-mono', "'JetBrains Mono','Consolas',monospace")
      return
    }
    // Файл не прочитався — тихий fallback на дефолтний стек.
    spec = FONTS[DEFAULT_FONT]
  } else if (key === 'custom' && fontPath) {
    await registerFontFace('shaurma-custom-font', 'ShaurmaCustom', fontPath)
  }

  spec = spec ?? FONTS[DEFAULT_FONT]
  root.style.setProperty('--font-ui', spec.ui)
  root.style.setProperty('--font-mono', spec.mono)
}

// ── Шрифти консолі ──────────────────────────────────────────────────────
// Значення шрифту консолі може бути:
//   'mono' / 'sans' / 'custom' (TTF з consoleFontPath) або 'file:<path>' —
// шрифт із теки fonts. Реєстрація @font-face йде у поточному документі
// (вікно консолі має СВІЙ документ — там теж треба faces).

// Активні значення шрифтів консолі з урахуванням режиму perType — спільний
// хелпер для реєстрації faces і переглядача.
export function consoleFontValues(s: Settings): string[] {
  return s.consoleFontMode === 'perType'
    ? [s.consoleInfoFont, s.consoleWarnFont, s.consoleErrorFont]
    : [s.consoleFontFamily]
}

// CSS font-family для значення шрифту консолі (inline-стилі рядків і
// переглядача). Для file:-значень повертає зареєстроване сімейство.
export function consoleFontStack(f: string): string {
  if (f === 'sans') return 'var(--font-sans)'
  if (f === 'custom') return 'var(--font-console-custom)'
  if (f.startsWith('file:')) return `'${fontFamilyFromPath(f.slice(5))}',var(--font-mono)`
  // Вбудовані/системні ключі (jetbrains/consolas…).
  if (FONTS[f]) return FONTS[f].mono
  return 'var(--font-mono)'
}

/**
 * Зареєструвати @font-face для ВСІХ обраних шрифтів консолі у поточному
 * документі. values — значення шрифтів ('mono'|'sans'|'custom'|'file:…').
 * Для 'custom' — класичний шлях через consoleFontPath (--font-console-custom);
 * для file: — сімейство з назви файлу.
 */
export async function loadConsoleFonts(values: string[], customPath?: string): Promise<void> {
  const root = document.documentElement
  const seen = new Set<string>()
  for (const v of values) {
    if (v.startsWith('file:')) {
      const path = v.slice(5)
      const family = fontFamilyFromPath(path)
      if (seen.has(family)) continue
      seen.add(family)
      await registerFontFace('shaurma-console-font-' + family, family, path)
    }
  }
  if (values.includes('custom') && customPath) {
    const ok = await registerFontFace('shaurma-console-font', 'ShaurmaConsoleCustom', customPath)
    if (ok) {
      root.style.setProperty('--font-console-custom', "'ShaurmaConsoleCustom','JetBrains Mono','Consolas',monospace")
      return
    }
  }
  root.style.setProperty('--font-console-custom', 'var(--font-mono)')
}


