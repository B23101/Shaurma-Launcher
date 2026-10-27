// smartImage.ts — Svelte action для <img>, що СТИСКУЄ зображення під
// фактичний розмір показу.
//
// Проблема, яку вирішує: картинка 4000×3000 (скриншот збірки), показана
// в галереї на 340px ширини, декодується WebView2 у ПОВНУ бітову карту
// (~48 МБ) у пам'ять GPU/процесу — навіть якщо на екрані вона «маленька».
// Десяток таких карток у галереї/браузері — і лаунчер легко тримає сотні
// мегабайтів заради картинок, які візуально займають копійки.
//
// Як працює: після завантаження зображення малюємо його в офскрін-canvas
// розміром ДО display size × devicePixelRatio (з капом ×2 — більше для
// екрана все одно не потрібно) і підміняємо src на стиснутий WebP/JPEG.
// Для юзера зміни непомітні (пікселів на екрані стільки ж), а пам'яті
// йде в рази менше: 4K-скриншот 340px → ~340×190×4 ≈ 0.26 МБ замість
// 48 МБ.
//
// Обмеження:
//   - GIF-и пропускаємо (анімовані — canvas зловив би лише перший кадр);
//   - data:URL пропускаємо (це вже оброблені/генеровані картинки);
//   - якщо зображення вже не більше ніж у 1.6 рази за розмір показу —
//     не чіпаємо (оверхед стиснення не вартий пари кілобайт);
//   - маленькі іконки (< 96px у природному розмірі) теж не чіпаємо.
//
// Використання: <img src={url} use:smartImage alt="" loading="lazy" />

export function smartImage(node: HTMLImageElement): { destroy(): void } {
  // Дозволяємо примусово вимкнути для окремого зображення
  // (напр. <img data-smart-ignore>).
  if (node.dataset.smartIgnore !== undefined) {
    return { destroy() {} }
  }

  let cancelled = false
  // Поточний blob-URL, створений цим action. РАНІШЕ createObjectURL ніколи
  // не звільнявся (revoke був лише у гілці «вузол вже відключено»): кожна
  // стиснута картка браузера/галереї лишала blob у пам'яті до закриття
  // вікна — на кількох сторінках пошуку це десятки мегабайт, що ніколи не
  // повертались. Тепер revoke і при заміні, і при знищенні вузла.
  let ownBlobUrl: string | null = null
  const releaseOwnBlob = () => {
    if (ownBlobUrl) {
      URL.revokeObjectURL(ownBlobUrl)
      ownBlobUrl = null
    }
  }

  const compress = () => {
    if (cancelled) return
    const src = node.currentSrc || node.src
    if (!src || src.startsWith('data:') || src.toLowerCase().endsWith('.gif')) return

    const nw = node.naturalWidth
    const nh = node.naturalHeight
    if (!nw || !nh) return
    // Дрібні картинки (іконки тощо) і так дешеві — не чіпаємо.
    if (Math.max(nw, nh) < 96) return

    const cw = node.clientWidth
    const ch = node.clientHeight
    if (!cw || !ch) return
    // Коефіцієнт «наскільки картинка більша за показ». Менше 1.6 —
    // економія не варта перекодування.
    if (nw < cw * 1.6 && nh < ch * 1.6) return

    const dpr = Math.min(window.devicePixelRatio || 1, 2)
    const tw = Math.max(1, Math.round(cw * dpr))
    const th = Math.max(1, Math.round(ch * dpr))
    // Запобігання збільшенню: якщо природний розмір менший за цільовий —
    // малюємо в природний (не роздуваємо).
    const w = Math.min(nw, tw)
    const h = Math.min(nh, th)
    if (w >= nw && h >= nh) return

    try {
      const canvas = document.createElement('canvas')
      canvas.width = w
      canvas.height = h
      const ctx = canvas.getContext('2d')
      if (!ctx) return
      // Білий фон під JPEG/WebP без альфи (обкладинки/скриншоти майже
      // завжди непрозорі; чорний фон з'їв би темні картинки).
      ctx.fillStyle = '#ffffff'
      ctx.fillRect(0, 0, w, h)
      // Smoothening — якість ресайзу (bicubic за замовчуванням).
      ctx.imageSmoothingEnabled = true
      ctx.imageSmoothingQuality = 'high'
      ctx.drawImage(node, 0, 0, w, h)

      const replace = (blob: Blob | null) => {
        if (!blob || cancelled) return
        const url = URL.createObjectURL(blob)
        // Міняємо src ТІЛЬКИ якщо картинка не зникла з DOM (переключення
        // вкладок могли розмонтувати компонент).
        if (node.isConnected) {
          const prev = ownBlobUrl
          ownBlobUrl = url
          node.src = url
          if (prev) URL.revokeObjectURL(prev)
        } else {
          URL.revokeObjectURL(url)
        }
      }

      const done = (blob: Blob | null) => {
        replace(blob)
        // Backing store canvas-а (w×h×4 байт) віддаємо одразу, а не чекаємо GC.
        canvas.width = 0
        canvas.height = 0
      }
      if (canvas.toBlob) {
        canvas.toBlob(done, 'image/webp', 0.85)
      } else {
        // Старі WebView2 без webp — JPEG 0.85 (альфу втрачаємо, але такі
        // рідко зустрічаються в картинках контенту).
        canvas.toBlob(done, 'image/jpeg', 0.85)
      }
    } catch {
      // Жодна помилка стиснення не має ламати показ картинки.
    }
  }

  const onLoad = () => compress()
  node.addEventListener('load', onLoad)
  // Картинка могла вже завантажитись до підключення action (кеш браузера).
  if (node.complete && node.naturalWidth > 0) {
    // Відкладаємо, щоб layout встиг визначити клієнтський розмір.
    setTimeout(compress, 0)
  }

  return {
    destroy() {
      cancelled = true
      node.removeEventListener('load', onLoad)
      releaseOwnBlob()
    },
  }
}
