// Біо та колір фону профілю — клієнтська частина. Дзеркало Go: social.NormalizeBio і VPS
// accounts.NormalizeBio / NormalizeColor; спільні вектори — profileStyle.vectors.ts.
// Сервер залишається авторитетом: тут лише щоб форма не пускала сміття й не палила ліміт змін.

export const BIO_MAX = 120

// Готові кольори фону (насичені, але такі, що не «вбивають» текст: у вікні профілю колір лише
// підфарбовує обкладинку й трохи тонує фон, текст завжди на звичайній панелі).
export const PROFILE_COLORS = [
  '#ff7a1a', '#ef4444', '#ec4899', '#a855f7', '#6366f1',
  '#3b82f6', '#06b6d4', '#14b8a6', '#22c55e', '#eab308',
] as const

const SPACES = new Set([0x85, 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000])
function isSpace(cp: number): boolean {
  return (cp >= 9 && cp <= 13) || cp === 32 || SPACES.has(cp) || (cp >= 0x2000 && cp <= 0x200a)
}
function isHidden(cp: number): boolean {
  if (cp === 0x200d) return false // ZWJ — частина складених емодзі
  return cp <= 0x1f || (cp >= 0x7f && cp <= 0x9f) || (cp >= 0x200b && cp <= 0x200f)
    || (cp >= 0x202a && cp <= 0x202e) || (cp >= 0x2066 && cp <= 0x2069)
    || cp === 0xfeff || cp === 0x061c || cp === 0x180e
}
const MARK = /^[\p{Mn}\p{Me}\p{Mc}]$/u
const MAX_MARKS = 2

/** Очищене біо; ok=false — задовге (> 120 символів) або зіпсований текст. Порожнє — дозволено. */
export function normalizeBio(raw: string): { text: string; ok: boolean } {
  let out = ''
  let n = 0, marks = 0
  let lastSpace = true, visible = false
  for (const ch of raw) {
    const cp = ch.codePointAt(0)!
    if (cp >= 0xd800 && cp <= 0xdfff) return { text: '', ok: false } // одинокий сурогат
    if (isSpace(cp)) {
      if (!lastSpace) { out += ' '; n++; lastSpace = true }
      marks = 0
      continue
    }
    if (isHidden(cp) || cp === 0xfffd) continue
    if (MARK.test(ch)) {
      if (++marks > MAX_MARKS) continue
    } else {
      marks = 0
      if (cp !== 0x200d) visible = true
    }
    out += ch
    n++
    lastSpace = false
  }
  if (!visible) return { text: '', ok: true }
  out = out.replace(/ +$/, '')
  return [...out].length > BIO_MAX ? { text: '', ok: false } : { text: out, ok: true }
}

/** Скільки символів (code points) займає текст — те, що рахує і сервер. */
export function bioLength(s: string): number { return [...s].length }

/** Біо ДРУГА для показу: очищене або '' (недійсне не показуємо). */
export function safeBio(raw?: string): string { return raw ? (normalizeBio(raw).ok ? normalizeBio(raw).text : '') : '' }

/** '#RRGGBB' у нижньому регістрі або '' (стандартний / недійсний). */
export function safeColor(raw?: string): string {
  const c = (raw ?? '').trim()
  return /^#[0-9a-fA-F]{6}$/.test(c) ? c.toLowerCase() : ''
}

/** Відтінок 0–359 зі стабільного рядка (ID) — запасний колір обкладинки, коли свій не вибрано. */
export function hueOf(id: string): number {
  let h = 0
  for (let i = 0; i < id.length; i++) h = (h * 31 + id.charCodeAt(i)) % 360
  return h
}

/** ID коротко для показу: перші 8 і останні 4 символи (повний — у буфер по кліку). */
export function shortId(id: string): string {
  return id.length > 16 ? `${id.slice(0, 8)}…${id.slice(-4)}` : id
}
