// ── Акцентна система + теми + шрифти ─────────────────────────────────
//
// Лаунчер використовує CSS-змінні --orange / --orange-2 по всьому UI
// (кнопки, прогреси, активні стани). Система акцентів дає змогу
// міняти акцентний колір у РОБОЧОМУ стані: applyAccent(accent) просто
// перезаписує ці змінні на documentElement, і весь інтерфейс одразу
// перефарбовується. Жодних рестартів чи окремих тем — все через CSS.
//
// Теми: applyTheme('dark'|'light') встановлює data-theme на <html>.
// Базові значення змінних (--bg-app/--bg-card/--text тощо) описані в
// style.css через селектор html[data-theme=...], тому тут лише
// перемикання атрибута.
//
// Шрифти: applyFont(font, fontPath?) задає --font-ui та --font-mono.
// Для 'custom' завантажує TTF через @font-face із локального файлу
// (шлях приходить з Settings.FontPath).
//
// Щоб додати новий акцент: додати запис у ACCENTS (назва + кольори) і
// варіант у config.SettingsSchema() + i18n-ключ accent.<name>.

import { App } from './wails'

export interface AccentDef {
  name: string      // i18n key accent.<name>
  main: string      // основний колір (--orange)
  deep: string      // градієнт-партнер (--orange-2)
  rgb: string       // rgb-трійка для rgba(var(--orange-rgb), …)
}

export const ACCENTS: Record<string, AccentDef> = {
  orange: { name: 'accent.orange', main: '#ff8a00', deep: '#ff5e00', rgb: '255, 138, 0' },
  violet: { name: 'accent.violet', main: '#7b4dff', deep: '#5a2eff', rgb: '123, 77, 255' },
  green:  { name: 'accent.green',  main: '#10b981', deep: '#059669', rgb: '16, 185, 129' },
  blue:   { name: 'accent.blue',   main: '#3b82f6', deep: '#2563eb', rgb: '59, 130, 246' },
  red:    { name: 'accent.red',    main: '#ef4444', deep: '#dc2626', rgb: '239, 68, 68' },
}

export const DEFAULT_ACCENT = 'orange'
export const DEFAULT_THEME = 'dark'
export const DEFAULT_FONT = 'inter'

/**
 * Застосувати акцентний колір: перезаписує CSS-змінні на :root.
 * Для accent === 'custom' використовується hex з пікера (settings.accentCustom):
 * основний колір = сам hex, глибший = затемнення на 18%, rgb = парс hex.
 */
export function applyAccent(accent: string, customHex?: string) {
  let a = ACCENTS[accent]
  if (accent === 'custom' && customHex && /^#[0-9a-fA-F]{6}$/.test(customHex)) {
    const { r, g, b } = hexToRgb(customHex)
    a = {
      name: 'accent.custom',
      main: customHex,
      deep: darkenHex(customHex, 0.82),
      rgb: `${r}, ${g}, ${b}`
    }
  }
  a = a ?? ACCENTS[DEFAULT_ACCENT]
  const root = document.documentElement
  root.style.setProperty('--orange', a.main)
  root.style.setProperty('--orange-2', a.deep)
  root.style.setProperty('--orange-rgb', a.rgb)
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

/** Застосувати тему: 'dark' (за замовчуванням) або 'light'. */
export function applyTheme(theme: string) {
  const t = theme === 'light' ? 'light' : 'dark'
  document.documentElement.setAttribute('data-theme', t)
}

// Відомі шрифти: font-family значення для UI та моноширинного тексту.
// 'inter' — вбудований TTF лаунчера; 'system' — системні шрифти ОС;
// 'jetbrains' — моноширинний JetBrains Mono (як у логах); 'custom' —
// кастомний TTF, вказаний у Settings.FontPath.
export const FONTS: Record<string, { ui: string; mono: string }> = {
  inter:     { ui: "'Inter','Segoe UI',sans-serif", mono: "'JetBrains Mono','Consolas',monospace" },
  system:    { ui: "'Segoe UI',system-ui,sans-serif", mono: "'Consolas','Courier New',monospace" },
  jetbrains: { ui: "'JetBrains Mono','Inter',monospace", mono: "'JetBrains Mono','Consolas',monospace" },
  custom:    { ui: "'ShaurmaCustom','Inter','Segoe UI',sans-serif", mono: "'JetBrains Mono','Consolas',monospace" },
}

/** Застосувати шрифт інтерфейсу. Для 'custom' — підвантажує TTF. */
export async function applyFont(font: string, fontPath?: string) {
  const key = font || DEFAULT_FONT
  const spec = FONTS[key] ?? FONTS[DEFAULT_FONT]

  // Кастомний TTF: створюємо @font-face з локального файлу. Оскільки
  // WebView не має доступу до довільних шляхів ФС, файл передається
  // бекендом як data: URL (див. App.LoadFontFile → base64).
  if (key === 'custom' && fontPath) {
    try {
      const dataUrl = await App.LoadFontFile(fontPath)
      if (dataUrl) {
        const styleId = 'shaurma-custom-font'
        let style = document.getElementById(styleId) as HTMLStyleElement | null
        if (!style) {
          style = document.createElement('style')
          style.id = styleId
          document.head.appendChild(style)
        }
        style.textContent = `@font-face{font-family:'ShaurmaCustom';src:url(${dataUrl}) format('truetype');font-display:swap;}`
      }
    } catch (e) {
      console.error('font load failed', e)
    }
  }

  const root = document.documentElement
  root.style.setProperty('--font-ui', spec.ui)
  root.style.setProperty('--font-mono', spec.mono)
}
