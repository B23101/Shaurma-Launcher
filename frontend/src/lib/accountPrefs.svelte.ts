// accountPrefs — ЛОКАЛЬНІ налаштування відображення вікна акаунта («Вигляд»):
// що показувати мені у власному профілі та чи вмикати ефекти/анімації.
// Це НЕ приватність для інших гравців (її тримає сервер: статус, збірка, чат, запити в друзі),
// а персоналізація власного вікна — зберігається лише на цьому пристрої.

const KEY = 'shaurma.accountModal.prefs.v1'

/** Що писати під ніком у віджеті акаунта в sidebar: тип акаунта або мій статус. Без підміни при наведенні. */
export type WidgetLine = 'type' | 'status'
const WIDGET_LINES: WidgetLine[] = ['type', 'status']

export interface AccountPrefs {
  showPlaytime: boolean
  showBuilds: boolean
  showFriends: boolean
  showSince: boolean
  showBio: boolean
  show3D: boolean
  autoSpin: boolean
  widgetLine: WidgetLine
}

/** Ключі-перемикачі (булеві) — для списку тумблерів у «Вигляді». */
export type AccountToggleKey = Exclude<keyof AccountPrefs, 'widgetLine'>

export const ACCOUNT_PREF_DEFAULTS: AccountPrefs = {
  showPlaytime: true,
  showBuilds: true,
  showFriends: true,
  showSince: true,
  showBio: true,
  show3D: true,
  autoSpin: true,
  widgetLine: 'type',
}

function read(): AccountPrefs {
  try {
    const raw = localStorage.getItem(KEY)
    if (!raw) return { ...ACCOUNT_PREF_DEFAULTS }
    const p = JSON.parse(raw)
    const out = { ...ACCOUNT_PREF_DEFAULTS }
    for (const k of Object.keys(out) as AccountToggleKey[]) {
      if (typeof p?.[k] === 'boolean') out[k] = p[k]
    }
    if (WIDGET_LINES.includes(p?.widgetLine)) out.widgetLine = p.widgetLine
    return out
  } catch { return { ...ACCOUNT_PREF_DEFAULTS } }
}

export const accountPrefs = $state<AccountPrefs>(read())

export function setAccountPref<K extends keyof AccountPrefs>(key: K, value: AccountPrefs[K]) {
  accountPrefs[key] = value
  try { localStorage.setItem(KEY, JSON.stringify($state.snapshot(accountPrefs))) } catch { /* ок */ }
}

export function resetAccountPrefs() {
  Object.assign(accountPrefs, ACCOUNT_PREF_DEFAULTS)
  try { localStorage.removeItem(KEY) } catch { /* ок */ }
}
