// ── Рендер MOTD сервера (§-коди Minecraft → HTML) ──────────────────────
// Бекенд (internal/minecraft/ping.go, parseMotd) тепер конвертує JSON
// chat-компоненти опису сервера (color/bold/italic/...) у стандартні
// §-коди Minecraft і віддає їх у ServerPing.motd БЕЗ вирізання. Це той
// самий формат, що гра сама показує в меню Multiplayer. Тут ми парсимо
// ці §-коди назад у стилізовані <span>, щоб MOTD виглядав так само
// кольоровим у віджеті сервера, як і в самій грі — а не сірим
// одноколірним текстом, як було раніше (§-коди просто відрізались на
// бекенді ще до того, як рядок доходив до UI).

// Офіційна палітра кольорів Minecraft (§0–§f), hex-значення — ті самі,
// що використовує сама гра для рендеру чату/MOTD.
const MOTD_COLORS: Record<string, string> = {
  '0': '#000000', '1': '#0000AA', '2': '#00AA00', '3': '#00AAAA',
  '4': '#AA0000', '5': '#AA00AA', '6': '#FFAA00', '7': '#AAAAAA',
  '8': '#555555', '9': '#5555FF', a: '#55FF55', b: '#55FFFF',
  c: '#FF5555', d: '#FF55FF', e: '#FFFF55', f: '#FFFFFF',
}

export interface MotdSegment {
  text: string
  color?: string
  bold?: boolean
  italic?: boolean
  underline?: boolean
  strikethrough?: boolean
  // obfuscated (§k) навмисно не анімуємо тут — на MOTD-прев'ю це зайвий
  // рух в інтерфейсі лаунчера; символи просто показуємо як є.
}

/** Розбирає рядок з §-кодами (класичні §0-f/§k-o/§r та §#RRGGBB з 1.16+)
 *  на масив стилізованих сегментів для рендеру. */
export function parseMotdSegments(raw: string): MotdSegment[] {
  if (!raw) return []
  const segments: MotdSegment[] = []
  let cur: MotdSegment = { text: '' }
  let i = 0
  const flush = () => {
    if (cur.text) segments.push(cur)
  }
  while (i < raw.length) {
    const ch = raw[i]
    if (ch === '§' && i + 1 < raw.length) {
      // §#RRGGBB — hex-колір (сервери 1.16+, градієнти тощо).
      if (raw[i + 1] === '#' && i + 8 <= raw.length && /^#[0-9a-fA-F]{6}$/.test(raw.slice(i + 1, i + 8))) {
        flush()
        cur = { ...cur, text: '', color: raw.slice(i + 1, i + 8) }
        i += 8
        continue
      }
      const code = raw[i + 1].toLowerCase()
      if (MOTD_COLORS[code]) {
        flush()
        cur = { text: '', color: MOTD_COLORS[code] }
        i += 2
        continue
      }
      switch (code) {
        case 'l': flush(); cur = { ...cur, text: '', bold: true }; i += 2; continue
        case 'o': flush(); cur = { ...cur, text: '', italic: true }; i += 2; continue
        case 'n': flush(); cur = { ...cur, text: '', underline: true }; i += 2; continue
        case 'm': flush(); cur = { ...cur, text: '', strikethrough: true }; i += 2; continue
        case 'k': flush(); cur = { ...cur, text: '' }; i += 2; continue // obfuscated — без анімації
        case 'r': flush(); cur = { text: '' }; i += 2; continue // reset
        default: break // невідомий код — показуємо символи як є нижче
      }
    }
    cur.text += ch
    i += 1
  }
  flush()
  return segments
}

/** Плаский текст без §-кодів — для title/alt атрибутів, де HTML не рендериться. */
export function stripMotdCodes(raw: string): string {
  if (!raw) return ''
  return raw.replace(/§#[0-9a-fA-F]{6}|§./g, '')
}
