<script lang="ts">
  // ── NewsBanner ────────────────────────────────────────────────────────
  // Динамічний банер головного екрана «Мої збірки»: картинка останнього
  // офіційного оновлення Minecraft. Бекенд (backend/news_banner.go) сам
  // питає Mojang-маніфест (latest.release), знаходить у news.json новину
  // з цим номером версії, качає playPageImage в локальний кеш
  // (<CacheDir>/banners/) і повертає data URL — тут він стає
  // background-image головного div.
  //
  // Джерела подій:
  //  1) onMount → App.GetNewsBanner() — явний запит (виклик дешевий:
  //     якщо фоновий стартовий виклик уже приніс картинку, бекенд віддасть
  //     її з кешу без мережі, а якщо ще йде — ми просто почекаємо в черзі);
  //  2) подія news:banner — бекенд шле її сам при старті лаунчера
  //     (ensureNewsBannerAtStartup), тож банер з'являється навіть тоді,
  //     коли onMount-виклик не встиг дочекатись мережі.
  //
  // Поява — плавна: фон переходить від прозорого до банера через
  // background-image transition (0.5s) + легкий fade/slide (той самий
  // cubic-bezier, що в .inst-banner сторінок збірок).

  import { onMount } from 'svelte'
  import { App, on } from './wails'
  import { t } from './i18n.svelte'

  // data URL банера (порожній — банер не показуємо взагалі).
  let banner = $state('')
  // Контент уже підтверджено браузером (onload/завершений transition) —
  // від нього залежить фінальна прозорість шару з фоном.
  let ready = $state(false)
  // Банер цього сеансу вже показано — не повторюємо appearance-анімацію
  // при поверненні на «Мої збірки» (компонент монтується щоразу заново).
  const seen = /* @__PURE__ */ new Set<string>()

  function applyBanner(dataUrl: string) {
    if (!dataUrl) return
    banner = dataUrl
    ready = seen.has(dataUrl)
    // Декодуємо фонову картинку ДО ввімкнення transition: інакше
    // background-image змінився б «стрибком» посеред анімації.
    const probe = new Image()
    probe.onload = () => {
      ready = true
      seen.add(dataUrl)
    }
    probe.onerror = () => { ready = false }
    probe.src = dataUrl
  }

  onMount(() => {
    let disposed = false
    App.GetNewsBanner()
      .then((dataUrl) => { if (!disposed) applyBanner(dataUrl) })
      .catch(() => { /* offline / Mojang недоступний — банер просто ховаємо */ })
    // Бекенд сам перевіряє банер при старті — банер може приїхати пізніше.
    const off = on('news:banner', (p: { dataUrl?: string }) => {
      if (!disposed && p?.dataUrl) applyBanner(p.dataUrl)
    })
    return () => { disposed = true; off() }
  })
</script>

{#if banner}
  <!-- Головний div банера: фон = знайдена бекендом картинка оновлення.
       background-image у батьківського .news-banner міняється з ''
       на url(...) — CSS transition нижче робить цю заміну плавною
       (плюс окремий шар з opacity для гарантованого fade). -->
  <div
    class="news-banner rise-in"
    class:ready
    style:background-image={`url('${banner}')`}
    role="img"
    aria-label={t('newsBanner.aria')}
  >
    <div class="nb-shade"></div>
    <div class="nb-content">
      <span class="nb-chip"><i class="ti ti-sparkles"></i> {t('newsBanner.chip')}</span>
    </div>
  </div>
{/if}

<style>
  /* ── Банер останнього оновлення на головному екрані ────────────────── */
  .news-banner {
    position: relative;
    width: 100%;
    height: 150px;
    border-radius: var(--r-lg);
    overflow: hidden;
    margin-bottom: 16px;
    background-size: cover;
    background-position: center;
    background-color: var(--bg-card);
    /* Плавна зміна фону (background-image анімується в WebView2 як
       cross-fade) + м'яка поява самого блоку. */
    transition: background-image 0.5s ease, opacity 0.5s ease;
    border: 1px solid var(--border);
  }
  /* Шар затінення: текст-чіп читається на будь-якому скріншоті Mojang. */
  .nb-shade {
    position: absolute;
    inset: 0;
    background:
      linear-gradient(180deg, rgba(0, 0, 0, 0.05) 0%, rgba(0, 0, 0, 0.42) 100%);
    pointer-events: none;
  }
  /* Фінальний fade: шар з фоном набирає прозорість, коли картинка
       реально декодувалась — разом з transition виходить плавна поява. */
  .news-banner:not(.ready) {
    opacity: 0;
  }
  .news-banner.ready {
    opacity: 1;
  }
  .nb-content {
    position: absolute;
    z-index: 1;
    left: 16px;
    bottom: 12px;
    display: flex;
    align-items: center;
  }
  .nb-chip {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    padding: 5px 12px;
    border-radius: 999px;
    font-size: 12px;
    font-weight: 700;
    color: var(--text);
    background: color-mix(in srgb, var(--bg-window) 72%, transparent);
    border: 1px solid var(--border-hi);
    backdrop-filter: blur(6px);
    transition: background 0.5s ease;
  }
  .nb-chip i {
    font-size: 13px;
    color: var(--orange);
  }
  /* Пауза «живих» шарів, коли вікно неактивне — консистентно з рештою UI
     (windowActive.svelte.ts ставить data-window-active на <html>). */
  :global(html[data-window-active='off']) .news-banner {
    transition-duration: 0s;
  }
</style>
