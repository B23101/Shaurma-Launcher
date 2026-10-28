// ── Асети збірок (іконки/фони) через бекенд ─────────────────────────────
// Worker віддає картинки збірок лише з токеном у заголовках, які браузер
// не може додати до <img>/background-image, тому асети качає бекенд
// (App.GetPackAsset) і повертає data URL. Цей модуль кешує результат на
// рівні модуля, щоб усі компоненти (sidebar, тайли, сторінка збірки)
// ділили один кеш і не качали той самий файл повторно.
//
// Значення '' у кеші = асет не завантажився (помилка/404) — більше не
// пробуємо; компонент показує fallback-іконку замість порожнього квадрата.
import { App } from './wails'

export const packAssets = $state<Record<string, string>>({})

const inflight = new Set<string>()

// ── localStorage-кеш (оптимізація розділ 4A) ────────────────────────────
// base64 data URL залишаються в JS heap на всю сесію (це не змінилось),
// але тепер вони ПЕРЕЖИВАЮТЬ перезапуск: при старті лаунчер не качає
// іконки заново через бекенд, а бере їх з localStorage — менше мережі,
// швидший старт, менше роботи для слабких ПК.
//
// Обмеження: зберігаємо лише ІКОНКИ (URL з /icon або data: — невеликі
// файли), не фони. Сумарний розмір обрізається за LRU (~2 МБ), щоб не
// впертись у ліміт localStorage (5 МБ) і не роздувати його на фонових
// зображеннях.
const CACHE_KEY = 'shaurma.packAssets.v1'
const CACHE_MAX_BYTES = 2 * 1024 * 1024

interface PersistEntry {
  dataUrl: string
  at: number // останній доступ (для LRU)
}

let persistCache: Record<string, PersistEntry> | null = null

function loadPersistCache(): Record<string, PersistEntry> {
  if (persistCache) return persistCache
  persistCache = {}
  try {
    const raw = localStorage.getItem(CACHE_KEY)
    if (!raw) return persistCache
    const parsed = JSON.parse(raw)
    if (parsed && typeof parsed === 'object') {
      for (const [k, v] of Object.entries(parsed)) {
        if (v && typeof (v as any).dataUrl === 'string') {
          persistCache[k] = { dataUrl: (v as any).dataUrl, at: (v as any).at || 0 }
        }
      }
    }
  } catch { /* битий/переповнений кеш — починаємо з нуля */ }
  // Одразу ж викидаємо стиглі записи (> 30 днів) — інакше вони вічно
  // сиділи б у localStorage і рахувались у LRU-бюджет (розділ 4A).
  pruneStalePersist()
  return persistCache
}

// pruneStalePersist видаляє записи старші за 30 днів (за at) і переписує
// кеш, якщо щось прибрано.
function pruneStalePersist() {
  const now = Date.now()
  const cutoff = 30 * 24 * 3600 * 1000
  let removed = false
  for (const [k, v] of Object.entries(persistCache!)) {
    if (now - v.at > cutoff) {
      delete persistCache![k]
      removed = true
    }
  }
  if (removed) {
    try { localStorage.removeItem(CACHE_KEY) } catch { /* ок */ }
    try { localStorage.setItem(CACHE_KEY, JSON.stringify(persistCache)) } catch { /* ок */ }
  }
}

function persistNow() {
  try {
    localStorage.setItem(CACHE_KEY, JSON.stringify(persistCache))
  } catch {
    // Переповнення квоти (localStorage ~5 МБ) — викидаємо половину
    // найстаріших записів і пробуємо ще раз.
    try {
      const entries = Object.entries(persistCache ?? {}).sort((a, b) => a[1].at - b[1].at)
      const drop = Math.ceil(entries.length / 2)
      for (const [k] of entries.slice(0, drop)) delete persistCache![k]
      localStorage.setItem(CACHE_KEY, JSON.stringify(persistCache))
    } catch { /* кеш недоступний — працюємо без персисту */ }
  }
}

// Чи варто персистити цей асет: лише іконки (невеликі за розміром),
// не фони. /icon/* — іконки збірок; все інше (background, старі URL)
// залишаємо поза localStorage.
function shouldPersist(url: string): boolean {
  return /\/icon\//.test(url) || url.startsWith('data:image/')
}

function bytesOf(dataUrl: string): number {
  return dataUrl.length
}

// Модульний кеш стартує ПІДМІШЕНИМ з localStorage: усі компоненти, що
// читають packAssets[url] під час першого рендеру, одразу отримають
// закешовану іконку (якщо вона там є) — без мережевого round-trip.
// (Стиглі записи вже відсіяні у loadPersistCache → pruneStalePersist.)
function hydrateFromPersist() {
  const pc = loadPersistCache()
  for (const [k, v] of Object.entries(pc)) {
    packAssets[k] = v.dataUrl
  }
}
hydrateFromPersist()

/**
 * Повертає data URL асета, якщо він уже завантажений; інакше ініціює
 * завантаження через бекенд і повертає undefined (компонент показує
 * fallback). Коли кеш оновиться, Svelte 5 сам перерендерить компоненти,
 * що читали packAssets[url] під час рендеру.
 */
// isGifAsset — чи це потенційно анімоване зображення: GIF або WebP (за
// MIME data URL або розширенням у URL). Анімований WebP тепер зберігається
// як є (див. Баг 7 у backend), тож його треба розпізнавати так само, як
// GIF — щоб правило «вікно неактивне → пауза анімації» (AnimatedIcon)
// працювало і для нього. Статичний WebP теж безпечно проходить: пауза
// покаже його як є, що візуально ідентично.
export function isGifAsset(url: string | undefined | null): boolean {
  if (!url) return false
  return url.startsWith('data:image/gif') || url.startsWith('data:image/webp') || /[.?&_]?\.(gif|webp)($|\?)/i.test(url)
}

export function packAsset(url: string | undefined | null): string | undefined {
  if (!url) return undefined
  // Уже готова картинка (іконка сервера в списках міграції/порівняння) —
  // бекенд для неї не потрібен.
  if (url.startsWith('data:')) return url
  const hit = packAssets[url]
  if (hit !== undefined) return hit || undefined
  if (!inflight.has(url)) {
    inflight.add(url)
    App.GetPackAsset(url)
      .then((d) => {
        const dataUrl = d ?? ''
        packAssets[url] = dataUrl
        if (dataUrl && shouldPersist(url)) {
          const pc = loadPersistCache()
          pc[url] = { dataUrl, at: Date.now() }
          // LRU-обрізання: поки сумарний розмір не влізе в ліміт,
          // викидаємо найстаріші записи.
          let total = Object.values(pc).reduce((n, e) => n + bytesOf(e.dataUrl), 0)
          while (total > CACHE_MAX_BYTES && Object.keys(pc).length > 1) {
            let oldest: string | null = null
            let oldestAt = Infinity
            for (const [k, e] of Object.entries(pc)) {
              if (e.at < oldestAt) { oldestAt = e.at; oldest = k }
            }
            if (!oldest) break
            total -= bytesOf(pc[oldest].dataUrl)
            delete pc[oldest]
          }
          persistNow()
        }
      })
      .catch(() => { packAssets[url] = '' })
  }
  return undefined
}
