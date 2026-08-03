// ── Персистентний стан інтерфейсу («останнє вікно») ──────────────────────
// Запам'ятовує у localStorage, де зупинився користувач: сторінку, вкладку
// перемикача збірок (Шаурма/Кастомні), вкладку налаштувань, відкриту збірку
// (з вкладкою і скролом) та скрол кожної сторінки. Відновлюється при
// поверненні у збірку (навіть через іншу) і після перезапуску лаунчера.
//
// Якщо збережена збірка вже не існує (видалена) — resolveOpenPackId
// підставляє найновішу з історії відкривань, а якщо і тієї нема — null
// (покажемо список «Мої збірки»).

const KEY = 'shaurma:uiState'

export interface PackUI {
  tab: string
  scroll: number
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
  instanceTab: 'custom',
  settingsTab: 'general',
  openPackId: null,
  lastPacks: [],
  pages: {},
  packs: {},
}

let state: UIState = load()

function load(): UIState {
  try {
    const raw = localStorage.getItem(KEY)
    if (!raw) return { ...defaults }
    const parsed = JSON.parse(raw) as Partial<UIState>
    return {
      ...defaults,
      ...parsed,
      page: typeof parsed.page === 'string' ? parsed.page : defaults.page,
      instanceTab: typeof parsed.instanceTab === 'string' ? parsed.instanceTab : defaults.instanceTab,
      settingsTab: typeof parsed.settingsTab === 'string' ? parsed.settingsTab : defaults.settingsTab,
      openPackId: typeof parsed.openPackId === 'string' ? parsed.openPackId : null,
      lastPacks: Array.isArray(parsed.lastPacks) ? parsed.lastPacks.filter((x): x is string => typeof x === 'string') : [],
      pages: parsed.pages && typeof parsed.pages === 'object' ? parsed.pages : {},
      packs: parsed.packs && typeof parsed.packs === 'object' ? parsed.packs : {},
    }
  } catch {
    return { ...defaults }
  }
}

function persist() {
  try {
    localStorage.setItem(KEY, JSON.stringify(state))
  } catch {
    // localStorage недоступний (WebView2 вимкнув) — просто не зберігаємо.
  }
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
