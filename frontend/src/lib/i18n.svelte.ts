// ── i18n: база перекладів лаунчера ──────────────────────────────────────
//
// Усі тексти інтерфейсу (кнопки, заголовки, підказки, toast-повідомлення)
// виносяться в словники і підставляються через t('key').
//
// СЛОВНИКИ лежать у JSON-файлах lib/langs/*.json (uk.json, en.json) — не
// інлайн у коді. Це дозволяє СПІЛЬНОТІ створювати власні мови лаунчера:
//
//   1. Скопіювати lib/langs/en.json (базова мова) у будь-який файл, напр.
//      polish.json.
//   2. Перекласти значення (ключі НЕ чіпати).
//   3. У лаунчері: Налаштування → Вигляд → «Імпортувати мову…» → обрати файл.
//
// ПРИНЦИП FALLBACK: англійська — БАЗОВА мова. Будь-який переклад (вбудований
// чи імпортований) лягає ПОВЕРХ англійської: ключі, які не переклали в новій
// мові, НЕ зникають — для них показується англійський текст (а якщо ключа
// немає навіть у en.json — сам ключ).
//
// Імпортовані мови зберігаються в localStorage ('shaurma-custom-langs') і
// переживають перезапуск лаунчера. Їх можна видалити в тих самих
// налаштуваннях.

import ukDict from './langs/uk.json'
import enDict from './langs/en.json'

export type Lang = string

/** Базова мова (fallback для відсутніх ключів) — англійська. */
export const baseLanguage: Lang = 'en'

/** Мова за замовчуванням, коли в налаштуваннях не задано інше. */
export const defaultLanguage: Lang = 'uk'

/** Коди вбудованих мов. */
export const languages: Lang[] = ['uk', 'en']

/** Людські назви вбудованих мов для перемикача (значення 'auto' — автовизначення). */
export const languageNames: Record<string, string> = {
  uk: 'Українська',
  en: 'English'
}

// Вбудовані словники: uk + en (en — база fallback).
const builtinDictionaries: Record<string, Record<string, string>> = {
  uk: ukDict,
  en: enDict
}

// ── Імпортовані (спільнотні) мови ───────────────────────────────────────

export interface CustomLanguage {
  code: string      // унікальний код, напр. "custom-pl"
  name: string      // людська назва, напр. "Polski"
  dict: Record<string, string>
}

const CUSTOM_LANGS_KEY = 'shaurma-custom-langs'

function loadCustomLanguages(): Record<string, CustomLanguage> {
  try {
    const raw = localStorage.getItem(CUSTOM_LANGS_KEY)
    if (!raw) return {}
    const parsed = JSON.parse(raw)
    if (parsed && typeof parsed === 'object') return parsed
  } catch { /* пошкоджений localStorage — починаємо з чистого */ }
  return {}
}

function saveCustomLanguages(langs: Record<string, CustomLanguage>) {
  try {
    localStorage.setItem(CUSTOM_LANGS_KEY, JSON.stringify(langs))
  } catch { /* quota/безпека — ігноруємо, мова просто не переживе рестарт */ }
}

let customLanguages = $state<Record<string, CustomLanguage>>(loadCustomLanguages())

/** Усі імпортовані мови (код → {code, name, dict}). */
export function getCustomLanguages(): Record<string, CustomLanguage> {
  return customLanguages
}

/** Список кодів усіх доступних мов: вбудовані + імпортовані. */
export function getAllLanguageCodes(): string[] {
  return [...languages, ...Object.keys(customLanguages)]
}

/** Людська назва мови за кодом (вбудована або імпортована). */
export function languageNameFor(code: string): string {
  if (code in languageNames) return languageNames[code]
  const cl = customLanguages[code]
  return cl ? cl.name : code
}

/**
 * Зареєструвати імпортовану мову. Перевіряє валідність словника (об'єкт
 * з рядковими значеннями) і генерує унікальний код, якщо name порожнє.
 * Повертає код зареєстрованої мови або '' при невалідному словнику.
 */
export function registerCustomLanguage(name: string, dict: unknown): string {
  if (!dict || typeof dict !== 'object' || Array.isArray(dict)) return ''
  const d = dict as Record<string, unknown>
  const clean: Record<string, string> = {}
  for (const [k, v] of Object.entries(d)) {
    if (typeof v === 'string') clean[k] = v
  }
  if (Object.keys(clean).length < 5) return '' // занадто мало ключів — не мова

  const base = 'custom-' + (name || 'lang').toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '') || 'custom-lang'
  let code = base
  let i = 2
  while (code in builtinDictionaries || code in customLanguages) {
    code = base + '-' + i++
  }

  customLanguages = { ...customLanguages, [code]: { code, name: name || code, dict: clean } }
  saveCustomLanguages(customLanguages)
  return code
}

/** Видалити імпортовану мову за кодом. */
export function removeCustomLanguage(code: string) {
  if (!(code in customLanguages)) return
  const next = { ...customLanguages }
  delete next[code]
  customLanguages = next
  saveCustomLanguages(customLanguages)
  if (currentLang === code) setLanguage(defaultLanguage)
}

let currentLang = $state<Lang>(defaultLanguage)

/** Викликати при завантаженні налаштувань: setLanguage(settings.language). */
export function setLanguage(lang: string) {
  if (lang === 'auto') {
    const nav = (navigator.language || '').toLowerCase()
    lang = nav.startsWith('uk') ? 'uk' : 'en'
  }
  currentLang = lang
  for (const fn of langHooks.values()) {
    try { fn() } catch { /* підписник не має ламати зміну мови */ }
  }
}

// Підписники на зміну мови (ключ робить реєстрацію ідемпотентною).
const langHooks = new Map<string, () => void>()
export function onLanguageChange(id: string, fn: () => void) {
  langHooks.set(id, fn)
}

/**
 * Ключі повідомлень, які лаунчер показує гравцеві В ГРІ, коли його не пустили на
 * P2P-сервер (хост офлайн, «лише друзі», немає доступу…). Збігаються з
 * p2p.RejectKeys() у бекенді; тексти — p2p.rej.<ключ> у словниках мов.
 */
export const P2P_REJECT_KEYS = [
  'hostOffline', 'hostOfflineAnon', 'notAllowed', 'onlyFriends', 'onlyList', 'onlyAccounts',
  'accountRequired', 'tunnelDisabled', 'rateLimited', 'lobbyUnreachable', 'networkDown', 'noSlot',
  'hostNoResponse', 'gameNotRunning', 'ticketRejected', 'checkUnavailable', 'badAddress', 'internal',
] as const

/** Переклади повідомлень про відмову поточною мовою (для бекенду). */
export function p2pRejectTexts(): Record<string, string> {
  const out: Record<string, string> = {}
  for (const k of P2P_REJECT_KEYS) out[k] = t('p2p.rej.' + k)
  return out
}

/** Поточна мова (read-only; змінюється лише через setLanguage). */
export function getLanguage(): Lang {
  return currentLang
}

// Словник поточної мови: вбудований або імпортований (порожній, якщо нічого).
function dictFor(code: string): Record<string, string> {
  if (code in builtinDictionaries) return builtinDictionaries[code]
  const cl = customLanguages[code]
  if (cl) return cl.dict
  return {}
}

/**
 * Переклад за ключем. Ланцюжок fallback:
 *   поточна мова → англійська (база) → сам ключ.
 * Тож ключі, які не переклали в обраній мові (або в імпортованій),
 * НЕ зникають з UI — показується англійський варіант.
 * params підставляються у {placeholders}.
 */
export function t(key: string, params?: Record<string, string | number>): string {
  let s = dictFor(currentLang)[key]
  if (s === undefined) s = builtinDictionaries[baseLanguage][key]
  if (s === undefined) s = key
  if (params) {
    for (const [k, v] of Object.entries(params)) {
      s = s.split(`{${k}}`).join(String(v))
    }
  }
  return s
}
