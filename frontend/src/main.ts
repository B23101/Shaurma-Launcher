import { mount, tick } from 'svelte'
import App from './App.svelte'
import { App as Backend } from './lib/wails'
import { toast } from './lib/toast.svelte'
import { t } from './lib/i18n.svelte'

// Необроблена помилка/відхилення (зокрема реактивна помилка Svelte, що може
// «оббілити» екран — напр. порожні Налаштування після браузера контенту) —
// показуємо реальний текст помилки тостом замість тихої білої порожнечі,
// щоб було видно, що саме сталося, а не малювався пустий розділ.
window.addEventListener('error', (e) => {
  if (!e?.message) return
  // Локація помилки. Для помилок, перекинутих через Svelte runtime,
  // подія повідомляє лише місце rethrow у vendor-бандлі — це марно.
  // Тому беремо СТЕК помилки і шукаємо ПЕРШИЙ кадр НЕ з vendor (наш
  // компонент) — він покаже точний файл:рядок, де стався краш.
  let at = ''
  const st = e?.error?.stack ?? ''
  if (st) {
    const frame = st.split('\n').map((l: string) => l.trim()).find((l: string) => l.startsWith('at ') && !l.includes('vendor-') && !l.includes('svelte/internal'))
    if (frame) {
      const m = /at .*\(?((?:[a-zA-Z]:)?[^()]*\.(?:svelte|ts|js):\d+:\d+)/.exec(frame)
      if (m) at = ` [${m[1].split(/[\\/]/).pop()}]`
    }
  }
  if (!at && e.filename) at = ` [${e.filename.split(/[\\/]/).pop()}:${e.lineno}:${e.colno ?? 0}]`
  toast(t('error.withMessage', { message: e.message + at }), 'error')
})
window.addEventListener('unhandledrejection', (e) => {
  const msg = e?.reason?.message ?? String(e?.reason ?? '')
  if (!msg) return
  // Для асинхронних помилок теж показуємо перший рядок стека — там буде
  // файл:рядок виклику, де стався краш.
  const st = (e?.reason?.stack ?? '').split('\n')[1]?.trim()
  toast(t('error.withMessage', { message: msg + (st ? ` [${st}]` : '') }), 'error')
})

// Усі зовнішні посилання (<a href="https://...">, зокрема згенеровані
// linkify:Html у ModpackPage та посилання сайдбару проєкту) мають
// відкриватись у СИСТЕМНОМУ браузері користувача, а не у вбудованому
// вікні WebView2 — так само, як вхід у Microsoft-акаунт. Перехоплюємо
// глобальною делегацією кліку на document, тож правило діє для всього,
// що у лаунчері (і для майбутніх посилань теж).
document.addEventListener('click', (e) => {
  const target = e.target instanceof Element ? e.target.closest('a[href]') : null
  if (!target) return
  const href = target.getAttribute('href')
  if (!href) return
  let url: URL | null = null
  try {
    url = new URL(href, window.location.href)
  } catch {
    return
  }
  // Захист (оборона в глибину, поруч із sanitizeHtml/CSP): навіть якщо
  // санатор щось пропустив — javascript:/data:/vbscript: посилання НЕ
  // виконуються у WebView2, а блокується. Дозволяємо лише http(s).
  if (!url || (url.protocol !== 'http:' && url.protocol !== 'https:')) {
    e.preventDefault()
    return
  }
  if (url.origin === window.location.origin) return // внутрішня SPA-навігація
  // Клік по cmd/ctrl/середній кнопці на зовнішньому лінку — все одно
  // у системний браузер (серед WebView2 немає «нових вкладок»).
  e.preventDefault()
  Backend.OpenExternal(url.href)
})

const app = mount(App, {
  target: document.getElementById('app')!,
})

// Головне вікно створюється прихованим (воно прозоре заради заокруглених
// кутів і до першого малювання «просвічувало» б робочий стіл). Після mount
// даємо Svelte дорендерити перший кадр і просимо shell показати вікно.
// setTimeout, а не rAF: у прихованому вікні requestAnimationFrame може не
// спрацьовувати. Запасний таймер у shell покаже вікно, якщо сюди не дійшло.
tick().then(() => setTimeout(() => { Backend.UIReady().catch(() => {}) }, 50))

export default app