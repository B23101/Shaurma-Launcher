// Постійний кеш статичних превʼю скінів (IndexedDB) — ідея з skin-preview-storage Modrinth.
// Готові картинки переживають перезапуск лаунчера: при відкритті Гардероба картки
// показуються одразу з диска, а three.js для них навіть не вантажиться.
// Ключ — короткий хеш вмісту текстур (а не сам base64-рядок), тож зміна скіна під
// тим самим пресетом дає інший ключ, а старі записи самі витісняються за часом.

const DB_NAME = 'shaurma-skin-thumbs'
const STORE = 'thumbs'
const DB_VERSION = 1
/** Збільшити, якщо змінився вигляд превʼю (камера, світло, розмір) — старі записи ігноруються. */
export const THUMB_RENDER_VERSION = 'v1'
const MAX_ENTRIES = 400

interface StoredThumb { blob: Blob; ts: number }

let dbPromise: Promise<IDBDatabase | null> | null = null

function openDb(): Promise<IDBDatabase | null> {
  if (dbPromise) return dbPromise
  dbPromise = new Promise((resolve) => {
    try {
      if (typeof indexedDB === 'undefined') { resolve(null); return }
      const req = indexedDB.open(DB_NAME, DB_VERSION)
      req.onupgradeneeded = () => {
        const db = req.result
        if (!db.objectStoreNames.contains(STORE)) {
          const store = db.createObjectStore(STORE)
          store.createIndex('ts', 'ts')
        }
      }
      req.onsuccess = () => resolve(req.result)
      req.onerror = () => resolve(null)
      req.onblocked = () => resolve(null)
    } catch { resolve(null) }
  })
  return dbPromise
}

/** Швидкий нехриптографічний хеш рядка (cyrb53) — лише для ключів кешу. */
export function hashString(s: string): string {
  let h1 = 0xdeadbeef, h2 = 0x41c6ce57
  for (let i = 0; i < s.length; i++) {
    const ch = s.charCodeAt(i)
    h1 = Math.imul(h1 ^ ch, 2654435761)
    h2 = Math.imul(h2 ^ ch, 1597334677)
  }
  h1 = Math.imul(h1 ^ (h1 >>> 16), 2246822507) ^ Math.imul(h2 ^ (h2 >>> 13), 3266489909)
  h2 = Math.imul(h2 ^ (h2 >>> 16), 2246822507) ^ Math.imul(h1 ^ (h1 >>> 13), 3266489909)
  return (4294967296 * (2097151 & h2) + (h1 >>> 0)).toString(36)
}

export function thumbKey(slim: boolean, textureUrl: string, capeUrl: string): string {
  return `${THUMB_RENDER_VERSION}|${slim ? 's' : 'c'}|${hashString(textureUrl)}|${capeUrl ? hashString(capeUrl) : '-'}`
}

// Усі картки одного кадру збирають ключі в одну транзакцію (а не 40 окремих).
const pending = new Map<string, ((b: Blob | null) => void)[]>()
let flushTimer: ReturnType<typeof setTimeout> | undefined

async function flush() {
  flushTimer = undefined
  const batch = new Map(pending)
  pending.clear()
  const finish = (key: string, blob: Blob | null) => { for (const cb of batch.get(key) ?? []) cb(blob) }
  const db = await openDb()
  if (!db) { for (const key of batch.keys()) finish(key, null); return }
  try {
    const tx = db.transaction(STORE, 'readonly')
    const store = tx.objectStore(STORE)
    for (const key of batch.keys()) {
      const req = store.get(key)
      req.onsuccess = () => finish(key, (req.result as StoredThumb | undefined)?.blob ?? null)
      req.onerror = () => finish(key, null)
    }
    tx.onabort = () => { for (const key of batch.keys()) finish(key, null) }
  } catch { for (const key of batch.keys()) finish(key, null) }
}

/** Достати превʼю з диска (null — немає). Виклики в межах одного тіку об'єднуються. */
export function loadStoredThumb(key: string): Promise<Blob | null> {
  return new Promise((resolve) => {
    const list = pending.get(key)
    if (list) list.push(resolve)
    else pending.set(key, [resolve])
    if (flushTimer === undefined) flushTimer = setTimeout(() => { void flush() }, 0)
  })
}

let puts = 0

/** Зберегти превʼю на диск (без очікування; помилки ігноруються). */
export function storeThumb(key: string, blob: Blob): void {
  void openDb().then((db) => {
    if (!db) return
    try {
      const tx = db.transaction(STORE, 'readwrite')
      tx.objectStore(STORE).put({ blob, ts: Date.now() } satisfies StoredThumb, key)
      if (++puts % 25 === 0) tx.oncomplete = () => prune(db)
    } catch { /* ок: кеш — лише прискорення */ }
  })
}

/** Лишаємо найновіші MAX_ENTRIES записів. */
function prune(db: IDBDatabase) {
  try {
    const tx = db.transaction(STORE, 'readwrite')
    const store = tx.objectStore(STORE)
    const countReq = store.count()
    countReq.onsuccess = () => {
      let extra = countReq.result - MAX_ENTRIES
      if (extra <= 0) return
      const cur = store.index('ts').openCursor()
      cur.onsuccess = () => {
        const c = cur.result
        if (!c || extra <= 0) return
        c.delete()
        extra--
        c.continue()
      }
    }
  } catch { /* ок */ }
}
