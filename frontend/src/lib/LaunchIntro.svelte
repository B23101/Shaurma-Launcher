<script lang="ts">
  // ── Повноекранне кінематографічне інтро після майстра налаштування ─────
  // Хронологія (мс):
  //   0–3300   титри: світла лінія розкривається, «КОМАНДА «ШАУРМА»»
  //            проявляється з розфокусу, під нею «ПРЕДСТАВЛЯЄ» → згасають
  //   3000–3600 іконка влітає з величезного масштабу й «б'є» в екран:
  //            спалах, ударна хвиля, шейк, вибух іскор
  //   3900–4900 назва «SHAURMA LAUNCHER» виїжджає по літерах із маски,
  //            потім по ній пробігає блік
  //   6600–7300 усе плавно гасне → onDone
  // Усе в ОДНІЙ сітці з центруванням: титри та лого лежать в одній клітинці,
  // тож композиція завжди точно по центру і нічого не «пливе».
  // Клік / Esc / Enter / Space — пропустити.
  import { t } from './i18n.svelte'
  import appIcon from '../assets/images/appicon.png'

  let { onDone }: { onDone: () => void } = $props()

  const TOTAL_MS = 7300
  const IMPACT_MS = 3500
  const title = t('brand.title')
  const letters = [...title]

  let canvas: HTMLCanvasElement
  let overlay: HTMLDivElement
  let finished = false

  function finish() {
    if (finished) return
    finished = true
    onDone()
  }

  type P = {
    x: number; y: number; vx: number; vy: number
    life: number; max: number; size: number; hue: number; burst: boolean
  }

  $effect(() => {
    const ctx = canvas.getContext('2d')!
    const dpr = Math.min(window.devicePixelRatio || 1, 2)
    let w = 0, h = 0
    const resize = () => {
      w = overlay.clientWidth; h = overlay.clientHeight
      canvas.width = w * dpr; canvas.height = h * dpr
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
    }
    resize()
    window.addEventListener('resize', resize)

    const parts: P[] = []
    const rnd = (a: number, b: number) => a + Math.random() * (b - a)

    const spawnAmbient = () => {
      parts.push({
        x: rnd(0, w), y: h + 10, vx: rnd(-8, 8), vy: rnd(-40, -90),
        life: 0, max: rnd(3, 6), size: rnd(.8, 2.2), hue: rnd(22, 40), burst: false,
      })
    }
    const burst = () => {
      const cx = w / 2, cy = h / 2 - 20
      for (let i = 0; i < 140; i++) {
        const a = rnd(0, Math.PI * 2)
        const sp = rnd(120, 820) * (Math.random() < .25 ? 1.6 : 1)
        parts.push({
          x: cx, y: cy, vx: Math.cos(a) * sp, vy: Math.sin(a) * sp,
          life: 0, max: rnd(.7, 1.9), size: rnd(1, 3.4), hue: rnd(18, 46), burst: true,
        })
      }
    }

    let raf = 0, last = performance.now(), start = last, burstDone = false, acc = 0
    const tick = (now: number) => {
      const dt = Math.min((now - last) / 1000, .05); last = now
      const el = now - start
      if (!burstDone && el >= IMPACT_MS) { burstDone = true; burst() }
      // Фонові іскри: починаються після появи титрів, густішають після удару.
      if (el > 900 && el < TOTAL_MS - 500) {
        acc += dt * (burstDone ? 26 : 9)
        while (acc >= 1) { acc -= 1; spawnAmbient() }
      }
      ctx.clearRect(0, 0, w, h)
      ctx.globalCompositeOperation = 'lighter'
      for (let i = parts.length - 1; i >= 0; i--) {
        const p = parts[i]
        p.life += dt
        if (p.life >= p.max) { parts.splice(i, 1); continue }
        if (p.burst) { const d = Math.pow(.06, dt); p.vx *= d; p.vy = p.vy * d + 60 * dt }
        p.x += p.vx * dt; p.y += p.vy * dt
        const k = 1 - p.life / p.max
        const r = p.size * (p.burst ? 1 + k : 1)
        const g = ctx.createRadialGradient(p.x, p.y, 0, p.x, p.y, r * 4)
        g.addColorStop(0, `hsla(${p.hue},100%,62%,${.9 * k})`)
        g.addColorStop(1, `hsla(${p.hue},100%,50%,0)`)
        ctx.fillStyle = g
        ctx.beginPath(); ctx.arc(p.x, p.y, r * 4, 0, Math.PI * 2); ctx.fill()
      }
      raf = requestAnimationFrame(tick)
    }
    raf = requestAnimationFrame(tick)

    const timer = setTimeout(finish, TOTAL_MS)
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' || e.key === 'Enter' || e.key === ' ') finish()
    }
    window.addEventListener('keydown', onKey)

    return () => {
      cancelAnimationFrame(raf)
      clearTimeout(timer)
      window.removeEventListener('resize', resize)
      window.removeEventListener('keydown', onKey)
    }
  })
</script>

<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
<div class="li-overlay" bind:this={overlay} onclick={finish}>
  <div class="li-bg"></div>
  <div class="li-vignette"></div>
  <canvas class="li-canvas" bind:this={canvas}></canvas>
  <div class="li-flash"></div>

  <div class="li-stage">
    <!-- Титри -->
    <div class="li-credits">
      <div class="li-line"></div>
      <div class="li-t1">{t('intro.team')}</div>
      <div class="li-t2">{t('intro.presents')}</div>
    </div>

    <!-- Лого + назва -->
    <div class="li-hero">
      <div class="li-logo-wrap">
        <div class="li-ring r1"></div>
        <div class="li-ring r2"></div>
        <div class="li-halo"></div>
        <img class="li-logo" src={appIcon} alt="Shaurma Launcher" draggable="false" />
      </div>
      <div class="li-title" aria-label={title}>
        {#each letters as ch, i}
          <span class="li-ch-mask" aria-hidden="true">
            <span class="li-ch" style="animation-delay:{3900 + i * 45}ms">{ch === ' ' ? ' ' : ch}</span>
          </span>
        {/each}
        <span class="li-shine"></span>
      </div>
      <div class="li-underline"></div>
    </div>
  </div>
</div>

<style>
  .li-overlay {
    position: fixed; inset: 0; z-index: 99999;
    background: #000; overflow: hidden; cursor: pointer;
    --wails-draggable: no-drag;
    animation: liShake .5s linear 3.5s both, liOut .7s ease 6.6s both;
    color: #fff;
  }
  .li-bg {
    position: absolute; inset: 0;
    background:
      radial-gradient(ellipse 60% 50% at 50% 50%, rgba(255, 110, 0, .22), transparent 70%),
      radial-gradient(ellipse 90% 70% at 50% 120%, rgba(255, 138, 0, .16), transparent 70%);
    opacity: 0;
    animation: liFadeIn 1.6s ease 3.3s forwards;
  }
  .li-vignette {
    position: absolute; inset: 0; pointer-events: none;
    background: radial-gradient(ellipse at center, transparent 45%, rgba(0, 0, 0, .75) 100%);
  }
  .li-canvas { position: absolute; inset: 0; width: 100%; height: 100%; pointer-events: none; }
  .li-flash {
    position: absolute; inset: 0; pointer-events: none;
    background: radial-gradient(circle at 50% 46%, #fff 0%, rgba(255, 190, 90, .85) 25%, transparent 65%);
    opacity: 0; animation: liFlash .9s ease-out 3.5s both;
  }

  /* Єдина клітинка: титри й лого накладаються у центрі */
  .li-stage { position: absolute; inset: 0; display: grid; place-items: center; }
  .li-credits, .li-hero { grid-area: 1 / 1; }

  /* ── Титри ── */
  .li-credits {
    display: flex; flex-direction: column; align-items: center; gap: 18px;
    animation: liCreditsOut .6s ease 3s both;
  }
  .li-line {
    height: 2px; width: min(46vw, 460px);
    background: linear-gradient(90deg, transparent, #ff8a00, #fff, #ff8a00, transparent);
    box-shadow: 0 0 18px rgba(255, 138, 0, .9);
    transform: scaleX(0); opacity: 0;
    animation: liLine 1.1s cubic-bezier(.2, .8, .2, 1) .3s forwards;
  }
  .li-t1 {
    font-size: clamp(22px, 3.6vw, 40px); font-weight: 800; white-space: nowrap;
    letter-spacing: .04em; text-transform: uppercase;
    background: linear-gradient(180deg, #fff 30%, #ffc27a);
    -webkit-background-clip: text; background-clip: text; color: transparent;
    animation: liCredIn 1.2s cubic-bezier(.16, 1, .3, 1) .7s both;
  }
  .li-t2 {
    font-size: clamp(11px, 1.3vw, 15px); font-weight: 600; color: #ff9a2e;
    letter-spacing: .55em; text-transform: uppercase; padding-left: .55em;
    animation: liCredIn 1.2s cubic-bezier(.16, 1, .3, 1) 1.5s both;
  }

  /* ── Герой ── */
  .li-hero {
    display: flex; flex-direction: column; align-items: center;
    gap: clamp(18px, 3vh, 30px); position: relative;
  }
  .li-logo-wrap {
    position: relative; width: clamp(104px, 15vmin, 156px); aspect-ratio: 1;
    display: grid; place-items: center;
  }
  .li-logo {
    position: relative; width: 100%; height: 100%; border-radius: 22%;
    object-fit: cover; display: block; opacity: 0;
    animation: liSlam .55s cubic-bezier(.5, 0, .9, .4) 3s forwards, liFloat 3.2s ease-in-out 3.7s infinite;
    filter: drop-shadow(0 0 30px rgba(255, 130, 0, .55));
  }
  .li-halo {
    position: absolute; inset: -60%; border-radius: 50%;
    background: radial-gradient(circle, rgba(255, 138, 0, .5), rgba(255, 94, 0, .12) 45%, transparent 70%);
    opacity: 0; animation: liHalo 3s ease-out 3.5s forwards;
  }
  .li-ring {
    position: absolute; inset: 0; border-radius: 50%;
    border: 2px solid rgba(255, 170, 70, .9);
    box-shadow: 0 0 24px rgba(255, 138, 0, .7), inset 0 0 24px rgba(255, 138, 0, .4);
    opacity: 0; transform: scale(.4);
    animation: liRing 1.4s cubic-bezier(.1, .7, .2, 1) 3.5s forwards;
  }
  .li-ring.r2 { animation-delay: 3.68s; border-width: 1px; }

  .li-title {
    position: relative; display: flex; white-space: nowrap; overflow: hidden;
    font-size: clamp(30px, 6.2vmin, 62px); font-weight: 800; letter-spacing: .09em;
    text-transform: uppercase; padding: .08em .1em;
  }
  .li-ch-mask { display: inline-block; overflow: hidden; line-height: 1.15; }
  .li-ch {
    display: inline-block; transform: translateY(110%) rotate(8deg); opacity: 0;
    background: linear-gradient(180deg, #fff 35%, #ffb35c 100%);
    -webkit-background-clip: text; background-clip: text; color: transparent;
    animation: liChar .75s cubic-bezier(.16, 1, .3, 1) both;
  }
  .li-shine {
    position: absolute; top: 0; bottom: 0; left: 0; width: 22%;
    background: linear-gradient(100deg, transparent, rgba(255, 255, 255, .85), transparent);
    mix-blend-mode: overlay; transform: translateX(-160%) skewX(-20deg); opacity: 0;
    animation: liShine 1s ease-in-out 5s forwards;
  }
  .li-underline {
    height: 3px; width: clamp(120px, 22vmin, 260px); border-radius: 3px;
    background: linear-gradient(90deg, transparent, #ff8a00, #ff5e00, transparent);
    box-shadow: 0 0 16px rgba(255, 110, 0, .8);
    transform: scaleX(0); opacity: 0;
    animation: liLine .9s cubic-bezier(.2, .8, .2, 1) 4.6s forwards;
  }

  /* ── Keyframes ── */
  @keyframes liFadeIn { to { opacity: 1; } }
  @keyframes liLine {
    0% { transform: scaleX(0); opacity: 0; }
    30% { opacity: 1; }
    100% { transform: scaleX(1); opacity: 1; }
  }
  @keyframes liCredIn {
    from { opacity: 0; filter: blur(14px); letter-spacing: .5em; transform: scale(1.08); }
    to   { opacity: 1; filter: blur(0); }
  }
  @keyframes liCreditsOut {
    to { opacity: 0; transform: scale(.94); filter: blur(10px); }
  }
  @keyframes liSlam {
    0%   { opacity: 0; transform: scale(3.2) rotate(-14deg); filter: blur(18px) drop-shadow(0 0 30px rgba(255,130,0,.55)); }
    35%  { opacity: 1; }
    80%  { opacity: 1; transform: scale(1) rotate(0); filter: blur(0) drop-shadow(0 0 30px rgba(255,130,0,.55)); }
    90%  { transform: scale(.9); }
    100% { opacity: 1; transform: scale(1); filter: blur(0) drop-shadow(0 0 30px rgba(255,130,0,.55)); }
  }
  @keyframes liFloat {
    0%, 100% { transform: translateY(0) scale(1); }
    50% { transform: translateY(-6px) scale(1.025); }
  }
  @keyframes liHalo {
    0% { opacity: 0; transform: scale(.5); }
    15% { opacity: 1; transform: scale(1.1); }
    100% { opacity: .55; transform: scale(1); }
  }
  @keyframes liRing {
    0% { opacity: 0; transform: scale(.4); }
    10% { opacity: 1; }
    100% { opacity: 0; transform: scale(5.5); }
  }
  @keyframes liFlash {
    0% { opacity: 0; }
    8% { opacity: 1; }
    100% { opacity: 0; }
  }
  @keyframes liChar {
    0% { transform: translateY(110%) rotate(8deg); opacity: 0; }
    100% { transform: none; opacity: 1; }
  }
  @keyframes liShine {
    0% { transform: translateX(-160%) skewX(-20deg); opacity: 0; }
    15% { opacity: 1; }
    85% { opacity: 1; }
    100% { transform: translateX(560%) skewX(-20deg); opacity: 0; }
  }
  @keyframes liShake {
    0%   { transform: translate(0, 0); }
    12%  { transform: translate(-9px, 5px); }
    25%  { transform: translate(8px, -6px); }
    38%  { transform: translate(-6px, -4px); }
    52%  { transform: translate(5px, 4px); }
    68%  { transform: translate(-3px, 2px); }
    84%  { transform: translate(2px, -1px); }
    100% { transform: translate(0, 0); }
  }
  @keyframes liOut {
    to { opacity: 0; transform: scale(1.06); filter: blur(6px); }
  }

  @media (prefers-reduced-motion: reduce) {
    .li-overlay { animation: liOut .7s ease 6.6s both; }
    .li-logo { animation: liSlam .55s ease 3s forwards; }
  }
</style>
