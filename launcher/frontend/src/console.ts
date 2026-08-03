import { mount } from 'svelte'
import './style.css'
import GameConsole from './lib/GameConsole.svelte'

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

const app = mount(GameConsole, {
  target: document.getElementById('app')!,
  props: { standalone: true, packID: buildID },
})

export default app
