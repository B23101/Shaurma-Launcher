// ── FlexVer (порт Prism Launcher launcher/Version.cpp) ───────────────────
// ТОЙ САМИЙ алгоритм порівняння версій, яким Prism сортує списки версій
// (Minecraft, лоадери) та визначає рекомендовану версію. Скопійовано 1:1
// з C++-коду Prism (https://git.sleeping.town/exa/FlexVer):
//
//   - '+' та все після нього ігнорується (appendices: "1.20.1+build.5" == "1.20.1");
//   - Numeric-секції порівнюються за довжиною, потім кодопойнтами
//     (провідні нулі зрізаються: "1.007" == "1.7"; "2.0" < "10.0");
//   - пре-релізи сортуються НИЖЧЕ за релізи ("1.20 Pre-Release 1" < "1.20",
//     "0.20.0-beta.9" < "0.20.0");
//   - Null-секція (коротший рядок) < Textual, але > PreRelease.
//
// Використовується у формі створення/редагування збірки, щоб версії
// лоадерів відображались і сортувались у фронтенді так само, як у Prism.

export type SectionType = 0 | 1 | 2 | 3 // Null | Textual | Numeric | PreRelease

interface Section {
  t: SectionType
  v: string
}

const isDigit = (c: string): boolean => c >= '0' && c <= '9'

function parse(s: string): Section[] {
  const out: Section[] = []
  let i = 0
  const len = s.length
  while (i < len) {
    let t: SectionType = 1 // Textual
    const c = s[i]
    if (c === '+') break // ігноруємо appendices
    let v = ''
    if (c === '-' || c === ' ') {
      v += c
      i++
      // "-" чи " " на початку секції з нецифровим наступним символом → пре-реліз
      if (i < len && !isDigit(s[i])) t = 3
    } else if (isDigit(c)) {
      t = 2 // Numeric
    }
    for (; i < len; i++) {
      const r = s[i]
      if (
        (isDigit(r) !== (t === 2)) || // зміна типу = нова секція
        (r === ' ' && t === 2) || // числова секція + пробіл = пре-реліз
        (r === '-' && t !== 3) || // "---" валідний пре-реліз компонент
        r === '+'
      ) break
      v += r
    }
    if (v.length) {
      if (t === 2) v = v.replace(/^0+/, '') // провідні нулі
      out.push({ t, v })
    }
  }
  return out
}

const NULL_SECTION: Section = { t: 0, v: '' }

function compareSections(a: Section, b: Section): number {
  // Обидві числові: за довжиною, потім кодопойнтами
  if (a.t === 2 && b.t === 2) {
    if (a.v.length !== b.v.length) return a.v.length < b.v.length ? -1 : 1
    return a.v < b.v ? -1 : a.v > b.v ? 1 : 0
  }
  // Null-секції: null < textual, але null > prerelease
  if (a.t === 0) return b.t === 3 ? 1 : -1
  if (b.t === 0) return a.t === 3 ? -1 : 1
  // Текстове порівняння (різні типи або обидві текстові/пре-реліз)
  const minLen = Math.min(a.v.length, b.v.length)
  for (let k = 0; k < minLen; k++) {
    const ca = a.v.codePointAt(k)!
    const cb = b.v.codePointAt(k)!
    if (ca !== cb) return ca < cb ? -1 : 1
  }
  if (a.v.length !== b.v.length) return a.v.length < b.v.length ? -1 : 1
  return 0
}

/** FlexVer-порівняння: -1 якщо a < b, 0 якщо рівні, 1 якщо a > b. */
export function flexverCompare(a: string, b: string): number {
  const sa = parse(a)
  const sb = parse(b)
  const n = Math.max(sa.length, sb.length)
  for (let i = 0; i < n; i++) {
    const x = i < sa.length ? sa[i] : NULL_SECTION
    const y = i < sb.length ? sb[i] : NULL_SECTION
    const c = compareSections(x, y)
    if (c !== 0) return c
  }
  return 0
}

/**
 * Сортування версій за спаданням (найновіші перші) — те саме, що Prism
 * VersionFilterModel (SortRole, DescendingOrder). Мутує і повертає масив.
 *
 * versionOf — селектор версії для сортування об'єктів
 * (напр. `sortVersionsDesc(entries, v => v.version)`).
 */
export function sortVersionsDesc<T>(
  versions: T[],
  versionOf: (v: T) => string = (v) => String(v)
): T[] {
  return versions.sort((a, b) => flexverCompare(versionOf(b), versionOf(a)))
}
