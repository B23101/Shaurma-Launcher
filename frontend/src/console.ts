import { mount } from 'svelte'
import './style.css'
import GameConsole from './lib/GameConsole.svelte'
import ServerConsoleWindow from './lib/servers/ServerConsoleWindow.svelte'
import { initWindowActive } from './lib/windowActive.svelte'

// Окреме вікно консолі гри: повноекранна сторінка (на весь розмір вікна,
// без «чорного фону» — тема лаунчера). Монтує той самий компонент, що й
// віджет у головному вікні, але в повноекранному режимі.
//
// КОЖНА збірка має ВЛАСНЕ вікно консолі: бекенд (console_window.go →
// ShowConsoleWindowFor) відкриває вікно з ?build=<id> у URL, і компонент
// ЖОРСТКО прив'язується до цієї збірки через packID — вікно завжди показує
// лог і стан СВОЄЇ збірки, незалежно від того, скільки інших вікон/збірок
// відкрито (можна запустити 2–5 версій гри — кожна зі своєю консоллю).
const params = new URLSearchParams(window.location.search)
const buildID = params.get('build') || ''
// ?server=<id> — ТЕ САМЕ вікно, але для консолі СЕРВЕРА (backend:
// server_console_window.go). Одна HTML-сторінка console.html обслуговує
// обидва випадки: у збірки є багато специфічного (акаунти, RAM, ШІ-фікси),
// у сервера — свій набір вкладок/дій, тому для сервера монтується окремий
// компонент, а не гілка всередині GameConsole.
const serverID = params.get('server') || ''

// Правило «вікно неактивне → анімації на паузу»: консоль — ОКРЕМЕ вікно
// з власним станом активності. Shell шле скоповану подію
// "window:active:<win>" (window_exec.go: WM_ACTIVATE для не-головних
// вікон). Точний id вікна передається бекендом у ?win= (console_window.go
// шле consoleWindowID(buildID) явно) — фронтенд НЕ перекалькулює його сам,
// бо JS-репліка sanitizeWindowName не збігалась би для не-ASCII id.
// Ініціалізуємо слухач, щоб спінери/анімації консолі ставали на паузу,
// коли вікно поза фокусом (та сама схема, що в App.svelte з
// "window:active" для головного вікна).
const winParam = params.get('win') || ''
// Без ?win= (dev/ручне відкриття) — порожнє eventName: initWindowActive
// не підписується на generic "window:active" (то подія ГОЛОВНОГО вікна),
// лишаються лише fallback focus/blur/visibilitychange.
initWindowActive(winParam ? 'window:active:' + winParam : '')

const app = serverID
  ? mount(ServerConsoleWindow, {
      target: document.getElementById('app')!,
      props: { serverID },
    })
  : mount(GameConsole, {
      target: document.getElementById('app')!,
      props: { standalone: true, packID: buildID },
    })

export default app
