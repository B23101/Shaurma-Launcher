<script lang="ts">
  // ── BirthdayMode — святковий режим 22.10 ────────────────────────────────
  // Щороку 22 жовтня (за ЛОКАЛЬНОЮ датою користувача) лаунчер вмикає
  // «день народження»: гірлянда з прапорців зверху, повітряні кульки, що
  // летять знизу вгору та конфеті. Жодних написів.
  //
  // Фон (гірлянда, конфеті) — pointer-events:none і кліків не блокує. Кульки
  // клікабельні: клац → вони лопаються (спалах-уламки + тихий «поп»), а через
  // кілька секунд на їхньому місці знову з'являється нова кулька. Дата перевіряється раз на хвилину: якщо лаунчер відкритий
  // опівночі, режим сам увімкнеться/вимкнеться.
  //
  // Performance Mode / «Анімації: вимк.» / prefers-reduced-motion → лишається
  // статична гірлянда й кульки по кутах, без руху й конфеті.
  //
  // Перевірка поза 22.10: у консолі WebView
  //   localStorage.setItem('shaurma.birthday.preview','1')  → перезапуск
  //   localStorage.removeItem('shaurma.birthday.preview')   → вимкнути
  import { onMount, onDestroy } from 'svelte'

  const MONTH = 9 // жовтень (0-індекс)
  const DAY = 22

  function isBirthday(): boolean {
    try {
      if (localStorage.getItem('shaurma.birthday.preview') === '1') return true
    } catch {}
    const d = new Date()
    return d.getMonth() === MONTH && d.getDate() === DAY
  }

  let active = $state(isBirthday())

  onMount(() => {
    const id = setInterval(() => { active = isBirthday() }, 60_000)
    return () => clearInterval(id)
  })

  // ── лускання кульок ──
  const BALLOON_COUNT = 6
  const RESPAWN_MS = 8000
  const SHARDS = 10
  let rootEl = $state<HTMLDivElement | null>(null)
  let popped = $state<boolean[]>(Array(BALLOON_COUNT).fill(false))
  let bursts = $state<{ id: number; x: number; y: number; color: string }[]>([])
  let burstId = 0
  const timers = new Set<ReturnType<typeof setTimeout>>()
  function later(fn: () => void, ms: number) {
    const h = setTimeout(() => { timers.delete(h); fn() }, ms)
    timers.add(h)
  }
  onDestroy(() => { timers.forEach(clearTimeout); timers.clear() })

  let audio: AudioContext | null = null
  function popSound() {
    try {
      audio ??= new AudioContext()
      const ctx = audio
      const len = Math.floor(ctx.sampleRate * 0.09)
      const buf = ctx.createBuffer(1, len, ctx.sampleRate)
      const d = buf.getChannelData(0)
      for (let i = 0; i < len; i++) d[i] = (Math.random() * 2 - 1) * Math.pow(1 - i / len, 3)
      const src = ctx.createBufferSource()
      const g = ctx.createGain()
      g.gain.value = 0.12
      src.buffer = buf
      src.connect(g).connect(ctx.destination)
      src.start()
    } catch {}
  }

  function pop(e: MouseEvent, i: number) {
    if (popped[i] || !rootEl) return
    const b = (e.currentTarget as HTMLElement).getBoundingClientRect()
    const root = rootEl.getBoundingClientRect()
    const id = ++burstId
    bursts.push({ id, x: b.left - root.left + b.width / 2, y: b.top - root.top + b.height / 2, color: balloons[i].color })
    popped[i] = true
    popSound()
    later(() => { bursts = bursts.filter((x) => x.id !== id) }, 700)
    later(() => { popped[i] = false }, RESPAWN_MS)
  }

  const COLORS = ['#ff5d73', '#ffb627', '#4cd964', '#3fa9f5', '#b46bff', '#ff8a00', '#2ee6c5']

  // Псевдовипадковість з фіксованим зерном: розкладка стабільна між
  // перемальовуваннями, і не потрібні Math.random у розмітці.
  function rng(seed: number) {
    let s = seed
    return () => ((s = (s * 1664525 + 1013904223) % 4294967296) / 4294967296)
  }
  const r = rng(2210)

  const balloons = Array.from({ length: BALLOON_COUNT }, (_, i) => ({
    left: 6 + (i * 88) / (BALLOON_COUNT - 1) + (r() - 0.5) * 6, // % — рівномірно вздовж ширини
    size: 38 + Math.round(r() * 22), // px
    dur: 14 + r() * 12, // c — повний підйом
    delay: -r() * 24, // від'ємний: частина кульок уже в польоті на старті
    sway: 10 + r() * 22, // px — амплітуда погойдування
    color: COLORS[i % COLORS.length],
  }))

  const confetti = Array.from({ length: 16 }, (_, i) => ({
    left: r() * 100,
    w: 5 + Math.round(r() * 5),
    h: 8 + Math.round(r() * 7),
    dur: 7 + r() * 8,
    delay: -r() * 14,
    drift: (r() - 0.5) * 140,
    spin: 360 + Math.round(r() * 720),
    color: COLORS[(i * 3) % COLORS.length],
  }))

  // Прапорці гірлянди: два провисання по 12 прапорців, обчислюємо позиції.
  const FLAGS = 24
  const flags = Array.from({ length: FLAGS }, (_, i) => {
    const t = (i + 0.5) / FLAGS // 0..1 вздовж ширини
    return { x: t * 100, color: COLORS[i % COLORS.length] }
  })
</script>

{#if active}
  <div class="bd-root" aria-hidden="true" bind:this={rootEl}>
    <!-- Гірлянда -->
    <svg class="bd-garland" viewBox="0 0 100 6" preserveAspectRatio="none">
      <path d="M0 0.4 Q 25 5 50 0.4 T 100 0.4" fill="none" stroke="rgba(255,255,255,.55)" stroke-width="0.25" vector-effect="non-scaling-stroke" />
    </svg>
    <div class="bd-flags">
      {#each flags as f, i}
        <span
          class="bd-flag"
          style="left:{f.x}%; --c:{f.color}; --sag:{Math.sin(((f.x % 50) / 50) * Math.PI) * 22}px; --d:{(i % 5) * 0.35}s"
        ></span>
      {/each}
    </div>

    <!-- Кульки -->
    {#each balloons as b, i}
      <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
      <span
        class="bd-balloon"
        class:bd-static={i % 3 !== 0}
        class:bd-popped={popped[i]}
        onclick={(e) => pop(e, i)}
        style="left:{b.left}%; --s:{b.size}px; --c:{b.color}; --dur:{b.dur}s; --delay:{b.delay}s; --sway:{b.sway}px"
      ></span>
    {/each}

    <!-- Уламки лопнутих кульок -->
    {#each bursts as x (x.id)}
      <span class="bd-burst" style="left:{x.x}px; top:{x.y}px; --c:{x.color}">
        <i class="bd-ring"></i>
        {#each Array(SHARDS) as _, k}
          <i class="bd-shard" style="--a:{(k * 360) / SHARDS}deg"></i>
        {/each}
      </span>
    {/each}

    <!-- Конфеті -->
    {#each confetti as c}
      <span
        class="bd-confetti"
        style="left:{c.left}%; width:{c.w}px; height:{c.h}px; background:{c.color}; --dur:{c.dur}s; --delay:{c.delay}s; --drift:{c.drift}px; --spin:{c.spin}deg"
      ></span>
    {/each}

  </div>
{/if}

<style>
  .bd-root {
    position: absolute;
    inset: 0;
    overflow: hidden;
    pointer-events: none;
    z-index: 5000; /* над сторінками, під модалками/wizard (99999) */
    border-radius: inherit;
  }

  /* ── гірлянда ── */
  .bd-garland {
    position: absolute;
    left: 0; top: 0;
    width: 100%; height: 30px;
    opacity: .7;
  }
  .bd-flags { position: absolute; left: 0; top: 0; width: 100%; height: 40px; }
  .bd-flag {
    position: absolute;
    top: var(--sag);
    width: 0; height: 0;
    margin-left: -7px;
    border-left: 7px solid transparent;
    border-right: 7px solid transparent;
    border-top: 16px solid var(--c);
    filter: drop-shadow(0 2px 2px rgba(0, 0, 0, .35));
    transform-origin: 50% 0;
    animation: bd-wave 3.2s ease-in-out var(--d) infinite;
  }
  @keyframes bd-wave {
    0%, 100% { transform: rotate(-5deg); }
    50% { transform: rotate(5deg); }
  }

  /* ── кульки ── */
  .bd-balloon {
    position: absolute;
    bottom: calc(var(--s) * -2);
    width: var(--s);
    height: calc(var(--s) * 1.2);
    border-radius: 50% 50% 48% 48% / 56% 56% 44% 44%;
    background:
      radial-gradient(circle at 32% 28%, rgba(255, 255, 255, .75) 0 9%, transparent 24%),
      radial-gradient(circle at 50% 40%, var(--c), color-mix(in srgb, var(--c) 62%, #000) 100%);
    box-shadow: inset -4px -6px 10px rgba(0, 0, 0, .18);
    opacity: .92;
    pointer-events: auto; /* єдине клікабельне в оверлеї — самі кульки */
    cursor: pointer;
    animation:
      bd-rise var(--dur) linear var(--delay) infinite,
      bd-sway 4.5s ease-in-out var(--delay) infinite;
  }
  /* вузлик */
  .bd-balloon::before {
    content: '';
    position: absolute;
    left: 50%; bottom: -5px;
    width: 8px; height: 7px;
    margin-left: -4px;
    background: var(--c);
    clip-path: polygon(50% 0, 100% 100%, 0 100%);
    filter: brightness(.8);
  }
  /* нитка */
  .bd-balloon::after {
    content: '';
    position: absolute;
    left: 50%; top: 100%;
    width: 1.5px; height: calc(var(--s) * 1.1);
    margin-top: 2px;
    background: linear-gradient(rgba(255, 255, 255, .6), transparent);
  }
  @keyframes bd-rise {
    from { translate: 0 0; }
    to { translate: 0 calc(-100vh - var(--s) * 4); }
  }
  @keyframes bd-sway {
    0%, 100% { transform: translateX(calc(var(--sway) * -1)) rotate(-4deg); }
    50% { transform: translateX(var(--sway)) rotate(4deg); }
  }

  .bd-popped { visibility: hidden; pointer-events: none; }

  /* ── лусання ── */
  .bd-burst { position: absolute; width: 0; height: 0; }
  .bd-ring {
    position: absolute;
    left: -18px; top: -18px;
    width: 36px; height: 36px;
    border-radius: 50%;
    border: 2px solid var(--c);
    animation: bd-ring .45s ease-out forwards;
  }
  @keyframes bd-ring {
    from { transform: scale(.3); opacity: .9; }
    to { transform: scale(1.7); opacity: 0; }
  }
  .bd-shard {
    position: absolute;
    left: -3px; top: -5px;
    width: 6px; height: 10px;
    border-radius: 2px;
    background: var(--c);
    animation: bd-shard .6s cubic-bezier(.15, .7, .3, 1) forwards;
  }
  @keyframes bd-shard {
    from { transform: rotate(var(--a)) translateY(0) scale(1); opacity: 1; }
    to { transform: rotate(var(--a)) translateY(-54px) scale(.3); opacity: 0; }
  }

  /* ── конфеті ── */
  .bd-confetti {
    position: absolute;
    top: -20px;
    border-radius: 2px;
    opacity: .9;
    animation: bd-fall var(--dur) linear var(--delay) infinite;
  }
  @keyframes bd-fall {
    from { transform: translate3d(0, 0, 0) rotate(0deg); }
    to { transform: translate3d(var(--drift), calc(100vh + 40px), 0) rotate(var(--spin)); }
  }

  /* ── економний режим: статична гірлянда + кульки по кутах, без конфеті ── */
  :global(html[data-performance-mode='on']) .bd-confetti,
  :global(html[data-animations='off']) .bd-confetti { display: none; }

  :global(html[data-performance-mode='on']) .bd-balloon,
  :global(html[data-animations='off']) .bd-balloon {
    animation: none;
    bottom: 8px;
  }
  :global(html[data-performance-mode='on']) .bd-balloon.bd-static,
  :global(html[data-animations='off']) .bd-balloon.bd-static { display: none; }

  :global(html[data-performance-mode='on']) .bd-flag,
  :global(html[data-animations='off']) .bd-flag { animation: none; }

  @media (prefers-reduced-motion: reduce) {
    .bd-confetti { display: none; }
    .bd-ring, .bd-shard { animation-duration: .01s; }
    .bd-balloon, .bd-flag { animation: none; }
    .bd-balloon { bottom: 8px; }
    .bd-balloon.bd-static { display: none; }
  }
</style>
