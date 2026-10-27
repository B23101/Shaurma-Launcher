import { mount } from 'svelte'
import './style.css'
import NotifyPopup from './lib/NotifyPopup.svelte'

// ── Окреме вікно віджета сповіщень у трей ─────────────────────────────────
//
// backend/notify_window.go створює це вікно: frameless, прозорий фон,
// AlwaysOnTop, без панелі завдань, WS_EX_NOACTIVATE (показ не викрадає фокус).
// Сторінка показує ЛИШЕ стос карток у правому нижньому куті; висоту контенту
// вона міряє сама і віддає бекенду через App.SetNotifyPopupSize — той
// встановлює розмір вікна і прив'язує його до кута (window_op
// "anchor-bottom-right"), тому вікно НЕ займає прозору область екрана, коли
// сповіщень немає (стос порожній → висота 0 → вікно ховається).
//
// ПРИМІТКА: вікно тепер суцільне (стос карток без прозорих відступів), тож
// справжня прозорість не потрібна. Раніше: style.css + примусовий прозорий фон: глобальні стили лаунчера дають
// змінні теми (--bg-*, шрифти), але body вони фарбують під застосунок. Тут
// це треба прибрати — інакше поверх прозорого вікна була б велика темна
// плашка. Інлайн-стилі перемагають таблиці стилів, тож це надійніше, ніж
// боротись зі специфічністю селекторів у CSS.
document.documentElement.style.background = 'var(--bg-panel)'
document.body.style.background = 'var(--bg-panel)'
document.body.style.margin = '0'
document.body.style.overflow = 'hidden'
document.body.style.width = 'fit-content'

const app = mount(NotifyPopup, {
  target: document.getElementById('app')!,
})

export default app
