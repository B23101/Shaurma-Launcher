// nav.ts — шляхи до налаштувань, що НІКОЛИ не застарівають.
//
// Тексти на кшталт «Налаштування → Java та запуск → Пам'ять» не пишуться
// руками: шлях збирається з registry.ts (розділ → картка → параметр) і з
// поточних перекладів. Якщо параметр перенесли в інший розділ чи картку —
// достатньо змінити registry.ts, і всі підказки в лаунчері оновляться самі.
//
// Посилання (ref) бувають трьох видів:
//   'maxRAM'            — конкретний параметр (id з searchItems)
//   'card:java/memory'  — картка: розділ/картка
//   's:account'         — розділ цілком
//
// У рядках перекладів шлях вставляється маркером {path:<ref>}:
//   "Змінити можна тут: {path:maxRAM}"
// Для клікабельного тексту — компонент RichT.svelte; для звичайного (toast,
// підказка, діалог підтвердження) — tp() нижче.

import { t } from '../i18n.svelte'
import { searchItems, settingsSectionById, labelKeyOf, cardKeyOf, type SectionId } from './registry'

export interface ResolvedSetting {
  section: SectionId
  /** Значення data-setting-key, до якого треба доскролити (немає — просто відкрити розділ). */
  anchor?: string
  /** Частини шляху після слова «Налаштування», вже перекладені. */
  parts: string[]
}

const norm = (s: string) => s.toLowerCase().replace(/[’ʼ'`]/g, '').trim()

/** Розбирає ref у розділ, якір і текст шляху. null — якщо такого налаштування немає в реєстрі. */
export function resolveSettingRef(ref: string): ResolvedSetting | null {
  if (ref.startsWith('s:')) {
    const id = ref.slice(2)
    if (!settingsSectionById[id]) return null
    return { section: id as SectionId, parts: [t(`st.s.${id}`)] }
  }
  if (ref.startsWith('card:')) {
    const [sec, card] = ref.slice(5).split('/')
    if (!settingsSectionById[sec]) return null
    const first = searchItems.find((i) => i.section === sec && i.card === card)
    if (!first) return null
    return { section: sec as SectionId, anchor: first.id, parts: [t(`st.s.${sec}`), t(cardKeyOf(card))] }
  }
  const it = searchItems.find((i) => i.id === ref)
  if (!it) return null
  const parts = [t(`st.s.${it.section}`), t(cardKeyOf(it.card))]
  const label = t(labelKeyOf(it.id))
  // Назву параметра додаємо лише коли вона несе нову інформацію (не дублює картку).
  if (label !== labelKeyOf(it.id) && norm(label) !== norm(parts[1])) parts.push(label)
  return { section: it.section, anchor: it.id, parts }
}

/** Повний шлях звичайним текстом: «Налаштування → Гра → Вікно». */
export function settingPathText(ref: string): string {
  const r = resolveSettingRef(ref)
  if (!r) return t('nav.settings')
  return [t('nav.settings'), ...r.parts].join(' → ')
}

const PATH_RE = /\{path:([^}]+)\}/g

/** t() із підстановкою {path:ref} → звичайний текст шляху (для toast, підказок, діалогів). */
export function tp(key: string, params?: Record<string, string | number>): string {
  return t(key, params).replace(PATH_RE, (_, ref: string) => settingPathText(ref))
}

/** Розбиває переклад на текст і посилання (для RichT). */
export function splitPaths(text: string): Array<{ text?: string; ref?: string }> {
  const out: Array<{ text?: string; ref?: string }> = []
  let last = 0
  for (const m of text.matchAll(PATH_RE)) {
    if (m.index! > last) out.push({ text: text.slice(last, m.index) })
    out.push({ ref: m[1] })
    last = m.index! + m[0].length
  }
  if (last < text.length) out.push({ text: text.slice(last) })
  return out
}

/** Окреме вікно консолі — інший webview: головне вікно звідти не дістати, тож там шлях лише текстом. */
export const canNavigateToSettings = (): boolean => !/console\.html/i.test(window.location.pathname)

/** Відкрити налаштування за ref (слухає App.svelte). */
export function goToSetting(ref: string): void {
  window.dispatchEvent(new CustomEvent('open-setting', { detail: ref }))
}
