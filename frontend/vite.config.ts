import {resolve} from 'node:path'
import {defineConfig} from 'vite'
import {svelte} from '@sveltejs/vite-plugin-svelte'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [svelte(), {
    // tabler-icons постачає той самий шрифт у woff2+woff+ttf. WebView2 (Windows)
    // і WebKitGTK (Linux) завжди беруть woff2, тож .woff/.ttf того самого
    // шрифта — кілька МБ мертвого вантажу в бандлі та інсталяторі. Прибираємо
    // їх із виходу і точково чистимо лише їхні посилання в CSS (інші шрифти —
    // Inter, JetBrains Mono — не чіпаємо; у них свій ttf і woff2-альтернативи нема).
    name: 'drop-redundant-tabler-font-formats',
    generateBundle(_options, bundle) {
      const dropped: string[] = []
      for (const name of Object.keys(bundle)) {
        if (/tabler-icons[^/\\]*\.(ttf|woff)$/i.test(name)) { // .woff2 закінчується на «2» — не підпадає
          delete bundle[name]
          dropped.push(name.split(/[/\\]/).pop()!)
        }
      }
      if (dropped.length === 0) return
      const esc = (s: string) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
      const refRe = new RegExp(`,?\\s*url\\([^)]*(?:${dropped.map(esc).join('|')})[^)]*\\)\\s*format\\([^)]*\\)`, 'gi')
      for (const output of Object.values(bundle)) {
        if (output.type !== 'asset' || !output.fileName.endsWith('.css')) continue
        const css = typeof output.source === 'string' ? output.source : Buffer.from(output.source).toString('utf8')
        output.source = css.replace(refRe, '')
      }
    }
  }, {
    name: 'bundled-license-inventory',
    generateBundle(_options, bundle) {
      const packages = new Set<string>()
      for (const output of Object.values(bundle)) {
        if (output.type !== 'chunk') continue
        for (const id of Object.keys(output.modules)) {
          const normalized = id.replace(/\\/g, '/')
          if (!normalized.includes('/node_modules/')) continue
          const relative = normalized.split('/node_modules/').pop()!
          const segments = relative.split('/')
          packages.add(segments.slice(0, relative.startsWith('@') ? 2 : 1).join('/'))
        }
      }
      this.emitFile({type: 'asset', fileName: 'licenses/bundled-packages.json', source: JSON.stringify([...packages].sort(), null, 2)})
    }
  }],
  // .glb-моделі Гардероба імпортуються як URL-ассети (three.js GLTFLoader).
  assetsInclude: ['**/*.glb'],
  build: {
    // three.js (Гардероб, MiniSkin3D/wardrobe3d) — важка бібліотека, яка
    // й тригерить дефолтний warning про розмір чанку (500 KB). Виносимо
    // її в окремий vendor-чанк: вона рідко міняється між релізами, тож
    // браузер (у Wails — вбудований WebView2/WebKit) кешує її окремо від
    // коду застосунку, і сам код лаунчера лишається в маленьких чанках.
    rollupOptions: {
      // Три вхідні точки: головне вікно лаунчера (index.html), окреме
      // вікно консолі гри (console.html — GameConsole.svelte на весь розмір
      // вікна) та віджет сповіщень у трей (notify.html — NotifyPopup.svelte).
      // Усі збираються у dist/, звідки Wails роздає їх як /index.html,
      // /console.html і /notify.html.
      input: {
        main: resolve(__dirname, 'index.html'),
        console: resolve(__dirname, 'console.html'),
        // Віджет сповіщень у трей: окремий frameless-шелл (notify_window.go),
        // тому і своя точка входу — так само, як у консолі гри.
        notify: resolve(__dirname, 'notify.html'),
        // Вікно запуску гри з ярлика збірки (shortcut_window.go): мала
        // картка з прогресом замість повного головного вікна.
        shortcut: resolve(__dirname, 'shortcut.html')
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
