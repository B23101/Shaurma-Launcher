// modIcon.ts — ЛІНИВЕ завантаження іконок модів для списку модів збірки.
//
// ПРОБЛЕМА: раніше бекенд віддавав base64-іконку КОЖНОГО мода всередині
// списку (ModEntry.iconData), причому відкриття сторінки модів робить кілька
// повних сканів (скан → перевірка оновлень → перевірка сумісності), і кожен
// раз іконки їхали в JS знову. На збірці з сотнями модів це десятки мегабайт
// рядків у купі WebView2 (+ реактивні проксі Svelte поверх них), хоча на
// екрані одночасно видно 10–15 рядків.
//
// ТЕПЕРЬ: у списку лише `hasIcon`. Цей Svelte-action:
//   1) чекає, поки <img> наблизиться до в'юпорту (IntersectionObserver);
//   2) ставить запит у чергу; черга раз на ~25 мс скидається ПАЧКАМИ в
//      App.GetModIcons (бекенд бере іконки з кешу метаданих jar);
//   3) кладе результат в обмежений LRU-кеш (не росте безкінечно) і одразу
//      пише в node.src — БЕЗ реактивного стану Svelte.
//
// ПРЯМІ URL (основний шлях): якщо бекенд підняв локальний loopback-сервер
// іконок (App.GetModIconBase), у <img> ставиться короткий URL
// `<base>/<packId>/<fileName>?v=<rev>` — картинку тягне й кешує сам WebView,
// у JS-купі нема ні рядка base64, ні LRU-кешу, ні роботи моста на кожну
// іконку. Пакетний GetModIcons (data URL) лишається ЛИШЕ запасним шляхом,
// якщо сервер не стартував.
//
// Використання: <img use:modIcon={{ packId, fileName: m.fileName, rev: m.fileSize }} alt="" />
import { App } from './wails'

export interface ModIconParams {
  packId: string
  fileName: string
  /** Ревізія файлу (напр. розмір): якщо jar замінили під тим самим іменем — іконку беремо наново. */
  rev?: string | number
}

// ~300 іконок × (2–4 КБ після масштабування на бекенді) ≈ 1 МБ — жорстка стеля.
const MAX_ENTRIES = 300
const BATCH = 48
const FLUSH_MS = 25

// key -> data URL ('' = у моду іконки нема / не вдалося отримати).
const cache = new Map<string, string>()
const waiters = new Map<string, Set<(url: string) => void>>()
// packId -> (fileName -> key), що чекає на відправку.
let queue = new Map<string, Map<string, string>>()
const inflight = new Set<string>()
let timer: ReturnType<typeof setTimeout> | undefined

// undefined = ще не питали; '' = сервера нема (запасний шлях); інакше базовий URL.
let iconBase: string | undefined
let basePromise: Promise<void> | undefined

function loadBase(): Promise<void> {
  if (!basePromise) {
    basePromise = App.GetModIconBase()
      .then((b) => { iconBase = typeof b === 'string' ? b : '' })
      .catch(() => { iconBase = '' })
  }
  return basePromise
}

function directUrl(p: ModIconParams): string {
  return `${iconBase}/${encodeURIComponent(p.packId)}/${encodeURIComponent(p.fileName)}?v=${encodeURIComponent(String(p.rev ?? ''))}`
}

function keyOf(p: ModIconParams): string {
  return `${p.packId}\u0000${p.fileName}\u0000${p.rev ?? ''}`
}

function cacheGet(k: string): string | undefined {
  const v = cache.get(k)
  if (v === undefined) return undefined
  // LRU: щойно використаний запис — «найсвіжіший».
  cache.delete(k)
  cache.set(k, v)
  return v
}

function cacheSet(k: string, v: string) {
  cache.delete(k)
  cache.set(k, v)
  while (cache.size > MAX_ENTRIES) {
    const oldest = cache.keys().next().value
    if (oldest === undefined) break
    cache.delete(oldest)
  }
}

function notify(k: string, url: string) {
  const set = waiters.get(k)
  if (!set) return
  waiters.delete(k)
  for (const cb of set) cb(url)
}

async function flush() {
  timer = undefined
  const batches = queue
  queue = new Map()
  for (const [packId, files] of batches) {
    const entries = [...files.entries()] // [fileName, key]
    for (let i = 0; i < entries.length; i += BATCH) {
      const chunk = entries.slice(i, i + BATCH)
      try {
        const res = (await App.GetModIcons(packId, chunk.map(([fn]) => fn))) ?? {}
        for (const [fn, k] of chunk) {
          const url = res[fn] ?? ''
          cacheSet(k, url)
          inflight.delete(k)
          notify(k, url)
        }
      } catch {
        // Мережі тут нема, але бекенд міг відмовити — не кешуємо помилку,
        // дозволяємо спробувати ще раз при наступному показі рядка.
        for (const [, k] of chunk) {
          inflight.delete(k)
          waiters.delete(k)
        }
      }
    }
  }
}

function enqueue(p: ModIconParams, k: string) {
  if (inflight.has(k)) return
  inflight.add(k)
  let files = queue.get(p.packId)
  if (!files) { files = new Map(); queue.set(p.packId, files) }
  files.set(p.fileName, k)
  if (timer === undefined) timer = setTimeout(flush, FLUSH_MS)
}

function subscribe(p: ModIconParams, cb: (url: string) => void): () => void {
  const k = keyOf(p)
  let set = waiters.get(k)
  if (!set) { set = new Set(); waiters.set(k, set) }
  set.add(cb)
  enqueue(p, k)
  return () => { waiters.get(k)?.delete(cb) }
}

export function modIcon(node: HTMLImageElement, initial: ModIconParams) {
  let params = initial
  let io: IntersectionObserver | null = null
  let unsub: (() => void) | null = null
  let visible = false
  let destroyed = false

  node.style.visibility = 'hidden' // без src не показуємо «биту» картинку
  node.onerror = () => { node.style.visibility = 'hidden' } // 404 (нема іконки) — без «битої» картинки

  const apply = (url: string) => {
    if (destroyed || !url) return
    node.src = url
    node.style.visibility = ''
  }

  const request = () => {
    unsub?.(); unsub = null
    if (iconBase === undefined) {
      // Базу ще не знаємо — питаємо один раз на всіх, потім повторюємо.
      loadBase().then(() => { if (!destroyed && visible) request() })
      return
    }
    if (iconBase) {
      apply(directUrl(params))
      return
    }
    const hit = cacheGet(keyOf(params))
    if (hit !== undefined) { apply(hit); return }
    unsub = subscribe(params, apply)
  }

  const stopObserving = () => { io?.disconnect(); io = null }

  if (typeof IntersectionObserver === 'undefined') {
    visible = true
    request()
  } else {
    io = new IntersectionObserver((entries) => {
      if (entries.some(e => e.isIntersecting)) {
        visible = true
        stopObserving()
        request()
      }
    }, { rootMargin: '300px' })
    io.observe(node)
  }

  return {
    update(next: ModIconParams) {
      if (keyOf(next) === keyOf(params)) { params = next; return }
      params = next
      node.removeAttribute('src')
      node.style.visibility = 'hidden'
      if (visible) request()
    },
    destroy() {
      destroyed = true
      stopObserving()
      unsub?.()
      node.onerror = null
    },
  }
}
