<script lang="ts" module>
  // Спільне для ВСІХ карток: модуль three.js, черга рендеру і кеші готових прев'ю.
  // Схема як у Modrinth (batch-skin-renderer + skin-preview-storage):
  //  1) памʼять (blob-URL) → 2) IndexedDB (переживає перезапуск) → 3) рендер у черзі.
  // Якщо превʼю є на диску, three.js для картки не вантажиться взагалі, а картки
  // з'являються одразу, без «завислого» відкриття Гардероба.
  // Динамічний імпорт wardrobe3d — завантажується один раз на перший справжній рендер.
  import { loadStoredThumb, storeThumb, thumbKey } from './thumbStore'

  let thumbRenderer: ((opts: { slim: boolean; textureUrl?: string; capeTextureUrl?: string }) => Promise<Blob>) | null = null

  // key → blob-URL. Blob-URL мізерні (кілька КБ у памʼяті браузера), на відміну від
  // base64-рядків у DOM/стані. Найстаріші відкликаємо, щоб не текло.
  const thumbCache = new Map<string, string>()
  const THUMB_CACHE_MAX = 300
  function rememberThumb(key: string, blob: Blob): string {
    const old = thumbCache.get(key)
    if (old) return old
    const url = URL.createObjectURL(blob)
    thumbCache.set(key, url)
    if (thumbCache.size > THUMB_CACHE_MAX) {
      const oldest = thumbCache.keys().next().value as string
      const oldUrl = thumbCache.get(oldest)
      thumbCache.delete(oldest)
      if (oldUrl) URL.revokeObjectURL(oldUrl)
    }
    return url
  }

  // Черга на один важкий рендер: 20+ карток не стартують одночасно
  // (інакше перший кадр гардероба з'їдає секунду на компіляцію шейдерів).
  let thumbQueue: Promise<void> = Promise.resolve()
  function nextIdle(): Promise<void> {
    return new Promise((resolve) => {
      if (typeof window.requestIdleCallback === 'function') window.requestIdleCallback(() => resolve(), { timeout: 180 })
      else setTimeout(resolve, 16)
    })
  }
  function enqueueThumb<T>(fn: () => Promise<T>): Promise<T> {
    const run = thumbQueue.then(fn, fn)
    thumbQueue = run.then(() => {}, () => {})
    return run
  }

  // Один спільний promise завантаження модуля (всі картки Гардероба
  // чекають на той самий чанк, а не качають його паралельно).
  let thumbModulePromise: Promise<void> | null = null
  function ensureThumbModule(): Promise<void> {
    if (!thumbModulePromise) {
      thumbModulePromise = import('./wardrobe3d').then((mod) => {
        thumbRenderer = mod.renderSkinThumbnail
      })
    }
    return thumbModulePromise
  }
</script>

<script lang="ts">
  // Міні-3D превʼю картки Гардероба. Рендерить СПРАВЖНЮ модель
  // (skin-model-launcher.glb / -slim.glb) з текстурою скіна через
  // renderSkinThumbnail (wardrobe3d.ts) — результат один статичний кадр
  // як Blob, без живого WebGL-контексту на кожній картці.

  // ОПТИМІЗАЦІЯ (розділ 2 плану): three.js (vendor-three чанк, ~619 КБ)
  // імпортується ДИНАМІЧНО — тільки коли ця картка реально починає
  // рендеритись. Стартовий завантаження лаунчера і сторінка «Мої збірки»
  // більше не парсять/не виконують three.js, якщо Гардероб ніхто не
  // відкриває.

  let { slim, textureUrl, capeTextureUrl }: { slim: boolean; textureUrl?: string; capeTextureUrl?: string } = $props()

  let imgSrc = $state<string | null>(null)
  let host: HTMLSpanElement | undefined = $state()
  // Рендеримо лише картки, що в зоні видимості (з запасом): у гардеробі з десятками
  // пресетів решта чекає, поки до неї прокрутять, і не заважає відкриттю.
  let visible = $state(false)

  $effect(() => {
    const el = host
    if (!el) return
    if (typeof IntersectionObserver === 'undefined') { visible = true; return }
    const io = new IntersectionObserver((entries) => {
      for (const e of entries) visible = e.isIntersecting
    }, { rootMargin: '240px 0px' })
    io.observe(el)
    return () => io.disconnect()
  })

  $effect(() => {
    const url = textureUrl
    const cape = capeTextureUrl
    const isVisible = visible
    // undefined = ще не запитували текстуру (запит у польоті) — чекаємо.
    // '' = запит завершився помилкою — все одно рендеримо картку:
    // renderSkinThumbnail сам підставить фолбек-текстуру Steve замість
    // порожньої білої моделі (баг з фото 1).
    if (url === undefined) {
      imgSrc = null
      return
    }
    const key = thumbKey(slim, url, cape ?? '')
    const cached = thumbCache.get(key)
    if (cached) {
      imgSrc = cached
      return
    }
    imgSrc = null
    if (!isVisible) return
    let alive = true
    loadStoredThumb(key)
      .then(async (stored) => {
        if (stored) {
          const u = rememberThumb(key, stored)
          if (alive) imgSrc = u
          return
        }
        if (!alive) return
        // Немає на диску — рендеримо в черзі й одразу кладемо в обидва кеші.
        const blob = await enqueueThumb(async () => {
          await nextIdle()
          if (!alive) return null
          await ensureThumbModule()
          if (!alive || !thumbRenderer) return null
          return thumbRenderer({ slim, textureUrl: url || undefined, capeTextureUrl: cape || undefined })
        })
        if (!blob) return
        storeThumb(key, blob)
        const u = rememberThumb(key, blob)
        if (alive) imgSrc = u
      })
      .catch(() => {
        if (alive) imgSrc = null
      })
    return () => {
      alive = false
    }
  })
</script>

<span class="wr-mini-3d-host" bind:this={host}>
  {#if imgSrc}
    <img class="wr-mini-3d" src={imgSrc} alt="" draggable="false" />
  {/if}
</span>

<style>
  .wr-mini-3d-host { display:block; width:100%; height:100%; }
  .wr-mini-3d { width:100%; height:100%; object-fit:contain; display:block; user-select:none; -webkit-user-drag:none; }
</style>
