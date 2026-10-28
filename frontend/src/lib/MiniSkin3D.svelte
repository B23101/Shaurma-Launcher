<script lang="ts" module>
  // Спільне для ВСІХ карток: модуль three.js, черга рендеру і кеш готових прев'ю.
  // Раніше це жило в кожному екземплярі — кеш зникав разом із карткою, і кожне
  // відкриття гардероба рендерило всі скіни заново.
  // Динамичний імпорт wardrobe3d — завантажується один раз на перший рендер.
  let thumbRenderer: ((opts: { slim: boolean; textureUrl?: string; capeTextureUrl?: string }) => Promise<string>) | null = null

  // Готові прев'ю переживають закриття гардероба (рівень модуля — спільні для всіх карток): при повторному відкритті
  // картки показуються одразу, без повторного рендера кожного скіна.
  const thumbCache = new Map<string, string>()
  const THUMB_CACHE_MAX = 80
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
  // як data URL, без живого WebGL-контексту на кожній картці.

  // ОПТИМІЗАЦІЯ (розділ 2 плану): three.js (vendor-three чанк, ~619 КБ)
  // імпортується ДИНАМІЧНО — тільки коли ця картка реально починає
  // рендеритись. Стартовий завантаження лаунчера і сторінка «Мої збірки»
  // більше не парсять/не виконують three.js, якщо Гардероб ніхто не
  // відкриває.

  let { slim, textureUrl, capeTextureUrl }: { slim: boolean; textureUrl?: string; capeTextureUrl?: string } = $props()

  let imgSrc = $state<string | null>(null)

  $effect(() => {
    const url = textureUrl
    const cape = capeTextureUrl
    // undefined = ще не запитували текстуру (запит у польоті) — чекаємо.
    // '' = запит завершився помилкою — все одно рендеримо картку:
    // renderSkinThumbnail сам підставить фолбек-текстуру Steve замість
    // порожньої білої моделі (баг з фото 1).
    if (url === undefined) {
      imgSrc = null
      return
    }
    const key = `${slim ? 'slim' : 'classic'}|${url}|${cape ?? ''}`
    const cached = thumbCache.get(key)
    if (cached) {
      imgSrc = cached
      return
    }
    imgSrc = null
    let alive = true
    enqueueThumb(async () => {
      await nextIdle()
      if (!alive) return null
      await ensureThumbModule()
      if (!alive || !thumbRenderer) return null
      const d = await thumbRenderer({ slim, textureUrl: url || undefined, capeTextureUrl: cape || undefined })
      if (d) {
        thumbCache.set(key, d)
        if (thumbCache.size > THUMB_CACHE_MAX) thumbCache.delete(thumbCache.keys().next().value as string)
      }
      return d
    })
      .then((d) => {
        if (alive && d) imgSrc = d
      })
      .catch(() => {
        if (alive) imgSrc = null
      })
    return () => {
      alive = false
    }
  })
</script>

{#if imgSrc}
  <img class="wr-mini-3d" src={imgSrc} alt="" draggable="false" />
{/if}

<style>
  .wr-mini-3d { width:100%; height:100%; object-fit:contain; display:block; user-select:none; -webkit-user-drag:none; }
</style>
