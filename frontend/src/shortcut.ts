import { mount } from 'svelte'
import './style.css'
import ShortcutLaunchWindow from './lib/ShortcutLaunchWindow.svelte'

// ── Окреме вікно запуску гри з ярлика збірки ─────────────────────────────
//
// backend/shortcut_window.go створює це вікно (id "shortcut-launch"), коли
// користувач клікнув ярлик збірки, а головний екран лаунчера не був відкритий.
// Сторінка лише показує іконку/назву збірки, прогрес запуску й кнопку
// «Скасувати»; сама гра запускається у прихованому головному вікні. Коли гра
// стартує, backend закриває це вікно сам.
document.body.style.margin = '0'
document.body.style.overflow = 'hidden'

const app = mount(ShortcutLaunchWindow, {
  target: document.getElementById('app')!,
})

export default app
