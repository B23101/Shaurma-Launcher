// ── Активність вікна (правило «вікно неактивне → анімації на паузу») ─────
// Спільний модульний стан: чи WebView-вікно зараз АКТИВНЕ (має фокус).
// Один ланцюг на весь додаток:
//
//   shell/window_exec.go (WM_ACTIVATE) → backend → подія "window:active"
//   (для головного вікна) або "window:active:<id>" (консоль/splash) →
//   initWindowActive(eventName) → windowActive + атрибут data-window-active
//   на <html>.
//
// CSS (style.css) під html[data-window-active="off"] паузить усі keyframes;
// AnimatedIcon заморожує GIF/WebP-іконки; цей модуль додатково заморожує
// РЕШТУ анімованих <img> (data:image/gif|webp) та інлайн background-image
// (банери збірок) через canvas — поточний кадр стає статичним src, без
// стрибка на перший кадр.
//
// Примітка: як і visible.svelte.ts — об'єкт (не гола $state-змінна), бо
// Svelte 5 забороняє ПЕРЕприсвоєння експортованого $state-значення з
// іншого модуля; мутація властивості .active — дозволена.
import { on } from './wails'

export const windowActive = $state({ active: true })

let nativeWindowActive: boolean | null = null
let inited = false
// Підписка (для cleanup) + зберігання оригінальних src заморожених картинок.
let unsubs: (() => void)[] = []
const frozenImgs = new WeakMap<HTMLImageElement, string>()
const frozenBgs = new WeakMap<HTMLElement, string>()
let pauseObserver: MutationObserver | null = null

function apply() {
  const active = nativeWindowActive ?? (typeof document === 'undefined' ? true : document.hasFocus())
  windowActive.active = active
  if (typeof document !== 'undefined') {
    document.documentElement.setAttribute('data-window-active', active ? 'on' : 'off')
  }
  if (active) {
    restoreAnimatedContent()
  } else {
    freezeAnimatedContent()
  }
}

/**
 * initWindowActive — єдина точка входу для підписки на активність вікна.
 * eventName: "window:active" (головне вікно лаунчера) або скоповане
 * "window:active:<id>" для окремих вікон (консоль: console-<build>,
 * splash: updater-splash). Викликається один раз при старті сторінки.
 */
export function initWindowActive(eventName = 'window:active') {
  if (inited || typeof window === 'undefined') return
  inited = true
  // Порожнє eventName = немає скопованої нативної події (напр. консоль
  // відкрита в dev без ?win=) — підписку НЕ створюємо, лишаємось на
  // fallback focus/blur/visibilitychange. Критично: fallback не має
  // підписуватись на generic "window:active" чужого вікна (головного) —
  // інакше анімації консолі паузились би при деактивації ГОЛОВНОГО
  // вікна, а не свого.
  if (eventName) {
    try {
      const unsub = on(eventName, (p: any) => {
        if (p && typeof p.active === 'boolean') {
          nativeWindowActive = p.active
          apply()
        }
      })
      unsubs.push(unsub)
    } catch {
      /* dev/SSR без нативного runtime — працюємо на fallback focus/blur */
    }
  }
  apply()
  window.addEventListener('focus', apply)
  window.addEventListener('blur', apply)
  document.addEventListener('visibilitychange', apply)
}

// ── Глобальна заморозка анімованих зображень ─────────────────────────────
// Коли вікно неактивне, усі <img> з анімованим вмістом (data:image/gif,
// data:image/webp або .gif/.webp) заморожуються на ПОТОЧНОМУ кадрі через
// canvas (як AnimatedIcon). Зображення, що приходять пізніше (lazy-load у
// браузері контенту), підхоплюються MutationObserver'ом, поки вікно
// неактивне. Remote-зображення (cross-origin) canvas не може захопити —
// вони лишаються як є (це іконки CDN, зазвичай статичні).

function isAnimatedImgSrc(src: string): boolean {
  if (!src) return false
  return src.startsWith('data:image/gif') || src.startsWith('data:image/webp') || /\.(gif|webp)(\?|$)/i.test(src)
}

function freezeImg(img: HTMLImageElement) {
  if (frozenImgs.has(img)) return
  // AnimatedIcon керує своїми іконками сам — не чіпаємо.
  if (img.dataset.animManaged === '1') return
  const src = img.currentSrc || img.src
  if (!isAnimatedImgSrc(src)) return
  // Lazy-load: картинка ще не завантажена — не пропускаємо назавжди.
  // Вішаємо ОДНОРАЗОВИЙ load-слухач, який заморозить її, коли вона
  // довантажиться (поки вікно лишається неактивним).
  if (!img.complete || !img.naturalWidth || !img.naturalHeight) {
    // Одноразовий load-слухач: заморозити, лише якщо вікно ДОСІ неактивне
    // (за час завантаження користувач міг повернутись у фокус).
    img.addEventListener('load', () => {
      if (!windowActive.active) freezeImg(img)
    }, { once: true })
    return
  }
  try {
    const c = document.createElement('canvas')
    c.width = img.naturalWidth
    c.height = img.naturalHeight
    const ctx = c.getContext('2d')
    if (!ctx) return
    ctx.drawImage(img, 0, 0)
    const frozen = c.toDataURL('image/png')
    // Canvas не зміг прочитати пікселі (cross-origin taint) — пропускаємо.
    if (!frozen.startsWith('data:image/png')) return
    frozenImgs.set(img, src)
    img.src = frozen
  } catch {
    /* cross-origin/недоступний canvas — лишаємо як є */
  }
}

// Заморозка інлайн background-image (банер збірки, фон сторінки), якщо це
// data:image/gif|webp (packAsset). Remote-фони не чіпаємо (canvas taint).
function freezeBgs() {
  if (typeof document === 'undefined') return
  const els = document.querySelectorAll<HTMLElement>('[style*="data:image/gif"], [style*="data:image/webp"]')
  for (const el of els) {
    if (frozenBgs.has(el)) continue
    const m = el.style.backgroundImage.match(/url\((['"]?)(data:image\/(?:gif|webp);[^'")]+)\1\)/i)
    if (!m) continue
    const url = m[2]
    const img = new Image()
    img.onload = () => {
      try {
        const c = document.createElement('canvas')
        c.width = img.naturalWidth
        c.height = img.naturalHeight
        const ctx = c.getContext('2d')
        if (!ctx) return
        ctx.drawImage(img, 0, 0)
        const frozen = c.toDataURL('image/png')
        if (!frozen.startsWith('data:image/png')) return
        frozenBgs.set(el, el.style.backgroundImage)
        el.style.backgroundImage = `url('${frozen}')`
      } catch { /* пропускаємо */ }
    }
    img.src = url
  }
}

function freezeAnimatedContent() {
  if (typeof document === 'undefined') return
  const imgs = document.querySelectorAll<HTMLImageElement>('img')
  for (const img of imgs) freezeImg(img)
  freezeBgs()
  // Нові <img> (lazy-load, динамічні списки) — заморожуємо одразу.
  if (!pauseObserver) {
    pauseObserver = new MutationObserver((muts) => {
      for (const m of muts) {
        for (const node of m.addedNodes) {
          if (node instanceof HTMLImageElement) freezeImg(node)
          else if (node.nodeType === Node.ELEMENT_NODE) {
            const imgs = (node as Element).querySelectorAll?.('img')
            if (imgs) for (const img of imgs) freezeImg(img)
          }
        }
      }
    })
    pauseObserver.observe(document.body, { childList: true, subtree: true })
  }
}

// restartAnimatedSrc — WebView2/Chromium НЕ перезапускає анімацію data-URL
// GIF, коли src повертається на ТОЙ САМИЙ рядок, що вже був завантажений
// (показує останній кадр з кешу, анімація «грає один раз і завмирає»).
// Примусовий перезапуск: на один кадр ставимо прозорий 1×1 GIF (а не '' —
// інакше блимає бите зображення), і лише наступним кадром — оригінал.
const BLANK_GIF =
  'data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw=='

function restartAnimatedImg(img: HTMLImageElement, orig: string) {
  img.src = BLANK_GIF
  requestAnimationFrame(() => {
    // Відновлюємо лише якщо вікно ДОСІ активне (за кадр користувач міг
    // знову втратити фокус — тоді анімація має лишатись замороженою).
    if (img.isConnected && windowActive.active) img.src = orig
  })
}

function restoreAnimatedContent() {
  if (typeof document === 'undefined') return
  if (pauseObserver) {
    pauseObserver.disconnect()
    pauseObserver = null
  }
  const imgs = document.querySelectorAll<HTMLImageElement>('img')
  for (const img of imgs) {
    const orig = frozenImgs.get(img)
    if (orig) {
      frozenImgs.delete(img)
      // Анімовані (GIF/WebP) перезапускаємо примусово — WebView2 інакше
      // лишає останній кадр (той самий баг, що в AnimatedIcon).
      if (isAnimatedImgSrc(orig)) restartAnimatedImg(img, orig)
      else img.src = orig
    }
  }
  const els = document.querySelectorAll<HTMLElement>('[style]')
  for (const el of els) {
    const orig = frozenBgs.get(el)
    if (orig) {
      el.style.backgroundImage = orig
      frozenBgs.delete(el)
    }
  }
}

// Для тестів/dev: скинути стан і відписатись (повторний init).
export function resetWindowActive() {
  restoreAnimatedContent()
  for (const u of unsubs) {
    try { u() } catch { /* ок */ }
  }
  unsubs = []
  inited = false
  nativeWindowActive = null
  windowActive.active = true
}
