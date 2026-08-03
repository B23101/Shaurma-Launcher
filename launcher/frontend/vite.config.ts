import {resolve} from 'node:path'
import {defineConfig} from 'vite'
import {svelte} from '@sveltejs/vite-plugin-svelte'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [svelte()],
  // .glb-моделі Гардероба імпортуються як URL-ассети (three.js GLTFLoader).
  assetsInclude: ['**/*.glb'],
  build: {
    // three.js (Гардероб, MiniSkin3D/wardrobe3d) — важка бібліотека, яка
    // й тригерить дефолтний warning про розмір чанку (500 KB). Виносимо
    // її в окремий vendor-чанк: вона рідко міняється між релізами, тож
    // браузер (у Wails — вбудований WebView2/WebKit) кешує її окремо від
    // коду застосунку, і сам код лаунчера лишається в маленьких чанках.
    rollupOptions: {
      // Дві вхідні точки: головне вікно лаунчера (index.html) та окреме
      // вікно консолі гри (console.html — GameConsole.svelte на весь розмір
      // вікна). Обидві збираються у dist/, звідки Wails роздає їх як
      // /index.html та /console.html.
      input: {
        main: resolve(__dirname, 'index.html'),
        console: resolve(__dirname, 'console.html')
      },
      // /wails/runtime.js роздає assetserver додатка Wails v3 — це не
      // модуль, який треба бандлити. Лишаємо імпорт як є, щоб браузер
      // підвантажував його з ассет-сервера при запуску.
      external: ['/wails/runtime.js'],
      output: {
        manualChunks(id) {
          if (id.includes('node_modules')) {
            if (id.includes('three')) return 'vendor-three'
            return 'vendor'
          }
        }
      }
    },
    // Піднімаємо поріг попередження: three.js-чанк все одно буде понад
    // дефолтні 500 KB навіть після виносу — це очікувано для 3D-бібліотеки,
    // а не ознака проблеми з рештою бандлу.
    chunkSizeWarningLimit: 1000
  }
})
