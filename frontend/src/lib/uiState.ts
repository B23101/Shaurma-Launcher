// ── Персистентний стан інтерфейсу («останнє вікно») ──────────────────────
// Запам'ятовує, де зупинився користувач: сторінку, вкладку перемикача
// збірок (Шаурма/Кастомні), вкладку налаштувань, відкриту збірку (з
// вкладкою і скролом) та скрол кожної сторінки. Відновлюється при
// поверненні у збірку (навіть через іншу) і після перезапуску лаунчера.
//
// Зберігається на ДИСКУ через бекенд (App.GetUIState()/SaveUIState() →
// store.UIState → .ui-state.json, поруч із .build-layout.json) — НЕ в
// localStorage.
//
// РЕГРЕСІЯ (виправлено): раніше весь цей стан жив ВИКЛЮЧНО в localStorage
// браузерного профілю WebView2 (ключ "shaurma:uiState"), усупереч
// задокументованій архітектурі («ніяких даних лаунчера в localStorage»).
// Наслідок: стан губився при очищенні профілю WebView2 (яке саме
// content_marker.go викликає при кожному оновленні лаунчера — новий
// exe/frontend.zip = новий sha256 = профіль стирається ПОВНІСТЮ) і не
// потрапляв у жоден бекап/перенесення даних разом з рештою.
//
// Публічний API лишається СИНХРОННИМ (як і раніше — читає/пише поле
// `state` у пам'яті), щоб не переписувати десятки викликів getUIState()/
// updateUIState() у App.svelte на async. Натомість:
//   - initUIState() — ОДИН асинхронний виклик при старті (App.svelte:
//     await initUIState() ПЕРЕД першим читанням getUIState(), усередині
//     loadData()) — підвантажує реальний стан з диска в `state`;
//   - persist() шле debounced App.SaveUIState() замість
//     localStorage.setItem — кілька швидких змін поспіль (напр. скрол +
//     зміна вкладки) зливаються в один запис, а не б'ють бекенд на кожен
//     піксель скролу.
import { App } from './wails'
import type { UIState as BackendUIState } from './types'

export interface PackUI {
  tab: string
  scroll: number
  // Скрол по РОЗДІЛАХ (вкладках) сторінки збірки: scrolls[розділ] = позиція.
  // Дозволяє повертатись у конкретний розділ на те саме місце, а не на
  // позицію останнього відвіданого розділу (як було з єдиним `scroll`).
  scrolls?: Record<string, number>
}

export interface UIState {
  page: string
  instanceTab: string
  settingsTab: string
  openPackId: string | null
  lastPacks: string[]
  pages: Record<string, number>
  packs: Record<string, PackUI>
}

const defaults: UIState = {
  page: 'instances',
  // '' = користувач ще нічого не обирав (перша установка): App.svelte тоді
  // відкриває «Шаурма» у Shaurma-редакції і «Кастомні» в базовій.
  instanceTab: '',
  settingsTab: 'general',
  openPackId: null,
  lastPacks: [],
  pages: {},
  packs: {},
}

let state: UIState = { ...defaults }
let initialized = false
let initPromise: Promise<void> | null = null

function fromBackend(s: BackendUIState | null | undefined): UIState {
  if (!s) return { ...defaults }
  const packs: Record<string, PackUI> = {}
  if (s.packs) {
    for (const [id, p] of Object.entries(s.packs)) {
      packs[id] = { tab: p.tab ?? '', scroll: p.scroll ?? 0, scrolls: p.scrolls }
    }
  }
  return {
    page: s.page ?? defaults.page,
    instanceTab: s.instanceTab || defaults.instanceTab,
    settingsTab: s.settingsTab ?? defaults.settingsTab,
    openPackId: s.openPackId ?? null,
    lastPacks: Array.isArray(s.lastPacks) ? s.lastPacks : [],
    pages: s.pages ?? {},
    packs,
  }
}

function toBackend(s: UIState): BackendUIState {
  const packs: Record<string, { tab?: string; scroll?: number; scrolls?: Record<string, number> }> = {}
  for (const [id, p] of Object.entries(s.packs)) {
    packs[id] = { tab: p.tab, scroll: p.scroll, scrolls: p.scrolls }
  }
  return {
    page: s.page,
    instanceTab: s.instanceTab,
    settingsTab: s.settingsTab,
    openPackId: s.openPackId ?? undefined,
    lastPacks: s.lastPacks,
    pages: s.pages,
    packs,
  }
}

/** Підвантажує реальний стан з диска. Викликати ОДИН раз при старті,
 *  до першого читання getUIState()/resolveOpenPackId() — інакше
 *  відновлення «останнього вікна» спрацює на дефолтах. Повторні виклики
 *  повертають той самий проміс (ідемпотентно). */
export function initUIState(): Promise<void> {
  if (initPromise) return initPromise
  initPromise = App.GetUIState()
    .then((s) => { state = fromBackend(s); initialized = true })
    .catch((e) => { console.error('initUIState:', e); initialized = true })
  return initPromise
}

// Debounce запису на бекенд: скрол і швидкі перемикання вкладок можуть
// оновлювати state по кілька разів на секунду — зайве мережеве/дискове
// навантаження без користі (лише останній стан переживає перезапуск).
let saveTimer: ReturnType<typeof setTimeout> | undefined
function persist() {
  if (!initialized) return // не затираємо диск дефолтами до завершення initUIState()
  clearTimeout(saveTimer)
  saveTimer = setTimeout(() => {
    App.SaveUIState(toBackend(state)).catch((e) => console.error('SaveUIState:', e))
  }, 250)
}

/** Форсує негайний запис (без дебаунсу) — викликати перед закриттям
 *  вікна/застосунку, де відкладений setTimeout може не встигнути. */
export function flushUIState(): void {
  if (!initialized) return
  clearTimeout(saveTimer)
  App.SaveUIState(toBackend(state)).catch((e) => console.error('SaveUIState:', e))
}

export function getUIState(): UIState {
  return state
}

export function updateUIState(patch: Partial<UIState>): void {
  state = { ...state, ...patch }
  persist()
}

export function getPackUI(id: string): PackUI | undefined {
  return state.packs[id]
}

export function setPackUI(id: string, ui: PackUI): void {
  if (!id) return
  state = { ...state, packs: { ...state.packs, [id]: ui } }
  persist()
}

// Найновіші збірки попереду (для fallback при зниклій збірці).
export function pushPackHistory(id: string): void {
  if (!id) return
  const last = state.lastPacks.filter((p) => p !== id)
  state = { ...state, lastPacks: [id, ...last].slice(0, 20) }
  persist()
}

export function removePackState(id: string): void {
  const packs = { ...state.packs }
  const lastPacks = state.lastPacks.filter((p) => p !== id)
  if (packs[id] || lastPacks.length !== state.lastPacks.length) {
    delete packs[id]
    state = { ...state, packs, lastPacks }
    persist()
  }
}

/** Знаходить збірку для відкриття при старті: збережену, інакше найновішу
 *  з історії, інакше null (список збірок). validIds — існуючі ID. */
export function resolveOpenPackId(validIds: Set<string>): string | null {
  if (state.openPackId && validIds.has(state.openPackId)) return state.openPackId
  for (const id of state.lastPacks) if (validIds.has(id)) return id
  return null
}

// ── Скрол контенту (`.content` — єдиний скролящий контейнер) ──

function contentEl(): HTMLElement | null {
  return document.querySelector('.content') as HTMLElement | null
}

export function readContentScroll(): number {
  return contentEl()?.scrollTop ?? 0
}

// Встановлює скрол з повторними спробами: контент вкладок вантажиться
// асинхронно (конфіг збірки, сервери), і scrollTop схлопується, якщо
// висота ще не сформувалась.
export function restoreContentScroll(top: number): void {
  const el = contentEl()
  if (!el) return
  if (top <= 0) {
    el.scrollTop = 0
    return
  }
  let tries = 0
  const attempt = () => {
    const c = contentEl()
    if (!c) return
    c.scrollTop = top
    if (++tries < 15 && Math.abs(c.scrollTop - top) > 4) {
      setTimeout(attempt, 120)
    }
  }
  requestAnimationFrame(attempt)
}
