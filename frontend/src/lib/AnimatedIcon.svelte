<script lang="ts">
  // AnimatedIcon — <img>, що може анімуватись (GIF/WebP), з підтримкою
  // паузи «на поточному кадрі». Правило «вікно неактивне → анімації на
  // паузу»: замість підміни src на ПЕРШИЙ кадр (статичний PNG, який
  // смикав картинку і створював «дригання») ЗАМОРОЖУЄМО кадр, що зараз
  // на екрані — canvas.drawImage(<img>) малює поточний кадр анімованого
  // зображення, і саме він стає статичним src до відновлення. При
  // відновленні src повертається на анімований і гра продовжується.
  //
  // Для не-анімованих зображень paused завжди false — компонент просто
  // рендерить <img> без жодних накладних витрат (ефект не захоплює кадр).
  let {
    src,
    paused = false,
    class: cls = '',
    alt = '',
    title = '',
  }: {
    src: string
    paused?: boolean
    class?: string
    alt?: string
    title?: string
  } = $props()

  let imgEl = $state<HTMLImageElement | null>(null)
  let frozen = $state<string | null>(null)

  // restartKey — лічильник ПРИМУСОВОГО перемонтування <img> при розморозці.
  // WebView2/Chromium, коли src повертається на РАНІШЕ завантажений той самий
  // data-URL GIF (заморожений кадр → анімований), показує останній кадр з
  // кешу і НЕ перезапускає анімацію (відоме обмеження: анімація «грає один
  // раз і зупиняється»; «перемикання між збірками оживляло» лише тому, що
  // Svelte створював НОВИЙ <img> з іншим URL). Перемонтовуємо <img> через
  // {#key} — браузер гарантовано завантажує GIF заново і анімація стартує
  // з нуля.
  let restartKey = $state(0)
  let wasPaused = false

  // capture — малює поточний кадр <img> у canvas і зберігає його як
  // статичний data URL. Викликається реактивно при paused (ефект нижче)
  // і при завантаженні зображення (onload) — на випадок, коли пауза
  // настала до того, як картинка встигла прогрузитись.
  function capture() {
    if (frozen !== null || !imgEl || !imgEl.complete || !imgEl.naturalWidth) return
    // Захоплюємо кадр лише для потенційно анімованих (GIF/WebP): для
    // статичних зображень paused завжди false, але страхуємось від
    // майбутніх сайтів, що передадуть paused помилково, — без цього
    // гейта марно копіювали б картинку в canvas.
    if (!src.startsWith('data:image/gif') && !src.startsWith('data:image/webp')) return
    try {
      // Зображення рендериться в іконці (22–34 px), тож заморожений кадр
      // НЕ повинен бути PNG у ПОВНІЙ природній роздільності (512/1024+ px =
      // data URL на мегабайти — пам'ять, повільний рендер). Масштабуємо до
      // фактично показаного розміру (з запасом до 256 px).
      let w = imgEl.naturalWidth
      let h = imgEl.naturalHeight
      const maxSide = 256
      if (Math.max(w, h) > maxSide) {
        const s = maxSide / Math.max(w, h)
        w = Math.max(1, Math.round(w * s))
        h = Math.max(1, Math.round(h * s))
      }
      const c = document.createElement('canvas')
      c.width = w
      c.height = h
      const ctx = c.getContext('2d')
      if (ctx) {
        ctx.drawImage(imgEl, 0, 0, w, h)
        frozen = c.toDataURL('image/png')
        return
      }
    } catch {
      /* canvas недоступний — лишаємо анімований src (не ламаємо іконку) */
    }
    frozen = ''
  }

  // $effect.pre (не $effect) — виконується ДО оновлення DOM, тому при
  // заморозці показ frozen застосовується тим самим рендером, що й
  // capture() (без кадру «анімований блимнув перед PNG»), а при розморозці
  // restartKey встигає змінитись до того, як Svelte перемонтує <img>.
  $effect.pre(() => {
    if (paused) {
      wasPaused = true
      capture()
    } else {
      // Розморозка: попередній стан був paused → перемонтовуємо <img>,
      // щоб браузер завантажив GIF заново (WebView2 не перезапускає
      // анімацію при повторному призначенні того самого data URL).
      if (wasPaused) restartKey++
      wasPaused = false
      frozen = null
    }
  })
</script>

{#key `${src}|${restartKey}`}
<img
  bind:this={imgEl}
  src={paused && frozen ? frozen : src}
  class={cls}
  alt={alt}
  title={title}
  loading="eager"
  draggable="false"
  data-anim-managed="1"
  onload={() => {
    if (paused) capture()
  }}
/>
{/key}
