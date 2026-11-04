<script lang="ts">
  // Великі векторні сцени для лівої панелі майстра. Кожна сцена — власний
  // SVG з одноразовою появою і тихим «живим» рухом. Кольори беруться з
  // теми (--orange = поточний акцент), тому сцена одразу підхоплює
  // обраний акцент і тему. Голови гравців — справжні PNG-скіни.
  import { t } from '../i18n.svelte'
  import PlayerHead from './PlayerHead.svelte'
  import appIcon from '../../assets/images/appicon.png'

  let { scene = 'welcome', ramGB = 4, notifyOn = true, accountHead = '', accountName = '' }: {
    scene?: string; ramGB?: number; notifyOn?: boolean; accountHead?: string; accountName?: string
  } = $props()

  const chips = $derived(Math.max(1, Math.min(8, Math.round(ramGB / 2))))
  const gauge = $derived(Math.max(.06, Math.min(1, ramGB / 16)))
  const ARC = 2 * Math.PI * 70 * .75
</script>





<div class="art" data-scene={scene}>
  <div class="glow"></div>

  {#if scene === 'welcome'}
    <svg viewBox="0 0 400 400" class="svg">
      <g class="ring r1"><circle cx="200" cy="200" r="150" /></g>
      <g class="ring r2"><circle cx="200" cy="200" r="112" /></g>
      <g class="ring r3"><circle cx="200" cy="200" r="184" /></g>
      <g class="logo">
        <image href={appIcon} x="128" y="128" width="144" height="144" class="logo-img" />
      </g>
      <g class="sparks">
        {#each [[110, 70], [320, 190], [86, 210], [250, 336], [160, 330], [330, 60]] as [x, y], i}
          <rect x={x} y={y} width="6" height="6" style="--d:{i * .35}s" />
        {/each}
      </g>
    </svg>
    <div class="h h-w1"><PlayerHead skin="alex" size={46} online /></div>
    <div class="h h-w2"><PlayerHead skin="steve" size={40} /></div>
    <div class="h h-w3"><PlayerHead skin="kai" size={36} online /></div>

  {:else if scene === 'look'}
    <svg viewBox="0 0 400 400" class="svg">
      <g class="win">
        <rect x="46" y="78" width="308" height="226" rx="16" class="w-frame" />
        <rect x="46" y="78" width="308" height="30" rx="16" class="w-top" />
        <rect x="46" y="96" width="308" height="12" class="w-top" />
        <circle cx="330" cy="93" r="4" class="w-dot" /><circle cx="316" cy="93" r="4" class="w-dot" />
        <rect x="46" y="108" width="62" height="196" class="w-side" />
        {#each [0, 1, 2, 3] as i}
          <rect x="62" y={124 + i * 38} width="30" height="30" rx="8" class="w-ico" class:acc={i === 0} style="--d:{.3 + i * .08}s" />
        {/each}
        <rect x="124" y="122" width="214" height="70" rx="12" class="w-hero" />
        <rect x="140" y="138" width="90" height="10" rx="5" class="w-line hi" />
        <rect x="140" y="156" width="130" height="7" rx="3.5" class="w-line" />
        <rect x="268" y="160" width="56" height="22" rx="8" class="w-btn" />
        {#each [0, 1, 2] as i}
          <g class="w-card" style="--d:{.45 + i * .1}s">
            <rect x={124 + i * 74} y="206" width="66" height="82" rx="10" class="w-c" />
            <rect x={132 + i * 74} y="214" width="50" height="34" rx="6" class="w-thumb" />
            <rect x={132 + i * 74} y="256" width="38" height="6" rx="3" class="w-line" />
            <rect x={132 + i * 74} y="268" width="24" height="6" rx="3" class="w-line acc" />
          </g>
        {/each}
      </g>
      <g class="swatches">
        {#each ['#c24b00', '#7536e8', '#008052', '#1664d9', '#d62438'] as c, i}
          <circle cx={92 + i * 54} cy="344" r="13" fill={c} style="--d:{.6 + i * .07}s" />
          <circle cx={92 + i * 54} cy="344" r="13" class="ripple" style="--rd:{1.75 + i * 2}s;stroke:{c}" />
        {/each}
      </g>
      <g class="cursor"><g class="cur-press">
        <path d="M0 0 L0 24 L6.5 18 L11 28 L15.5 26 L11 16.5 L19.5 16.5 Z" class="cur" />
      </g></g>
    </svg>

  {:else if scene === 'account'}
    <svg viewBox="0 0 400 400" class="svg">
      <g class="links">
        <path d="M200 200 L100 110" /><path d="M200 200 L310 116" /><path d="M200 200 L92 292" />
        <path d="M200 200 L314 290" /><path d="M200 200 L200 70" />
      </g>
      <circle cx="200" cy="200" r="64" class="halo" />
      <circle cx="200" cy="200" r="92" class="halo h2" />
      <g class="bubble bb1">
        <rect x="262" y="160" width="74" height="30" rx="12" class="bub" />
        <circle cx="284" cy="175" r="3.5" class="bub-dot" style="--d:0s" /><circle cx="299" cy="175" r="3.5" class="bub-dot" style="--d:.15s" /><circle cx="314" cy="175" r="3.5" class="bub-dot" style="--d:.3s" />
      </g>
      <g class="bubble bb2">
        <rect x="62" y="196" width="60" height="28" rx="11" class="bub acc" />
        <path d="M82 207q4-6 8 0q4-6 8 0q0 6-8 10q-8-4-8-10z" fill="#fff" />
      </g>
    </svg>
    <div class="h h-center">
      {#if accountHead}<PlayerHead src={accountHead} size={92} radius={18} />{:else}<PlayerHead skin="steve" size={92} radius={18} />{/if}
      {#if accountName}<span class="h-name">{accountName}</span>{/if}
    </div>
    <div class="h h-a1"><PlayerHead skin="alex" size={44} online /></div>
    <div class="h h-a2"><PlayerHead skin="zuri" size={44} online /></div>
    <div class="h h-a3"><PlayerHead skin="sunny" size={40} /></div>
    <div class="h h-a4"><PlayerHead skin="noor" size={42} online /></div>
    <div class="h h-a5"><PlayerHead skin="efe" size={36} /></div>

  {:else if scene === 'files'}
    <svg viewBox="0 0 400 400" class="svg">
      <g class="fold">
        <path d="M86 150h82l18 20h128a14 14 0 0 1 14 14v124a14 14 0 0 1-14 14H86a14 14 0 0 1-14-14V164a14 14 0 0 1 14-14z" class="f-back" />
        <g class="f-packs">
          <g class="pk p1"><rect x="128" y="150" width="56" height="64" rx="10" class="pk-c" /><rect x="138" y="160" width="36" height="26" rx="6" class="pk-th a" /><rect x="138" y="194" width="26" height="6" rx="3" class="pk-l" /></g>
          <g class="pk p2"><rect x="216" y="146" width="56" height="64" rx="10" class="pk-c" /><rect x="226" y="156" width="36" height="26" rx="6" class="pk-th b" /><rect x="226" y="190" width="30" height="6" rx="3" class="pk-l" /></g>
          <g class="pk p3"><rect x="172" y="160" width="56" height="64" rx="10" class="pk-c" /><rect x="182" y="170" width="36" height="26" rx="6" class="pk-th c" /><rect x="182" y="204" width="22" height="6" rx="3" class="pk-l" /></g>
        </g>
        <path d="M72 200h256a12 12 0 0 1 12 14l-10 86a16 16 0 0 1-16 14H86a16 16 0 0 1-16-14l-10-86a12 12 0 0 1 12-14z" class="f-front" />
        <rect x="170" y="246" width="60" height="8" rx="4" class="f-line" />
      </g>
      <g class="src s1"><rect x="34" y="52" width="58" height="58" rx="16" class="src-bg" /><path d="M63 66l14 8v16l-14 8l-14-8v-16z" class="src-ico" /></g>
      <g class="src s2"><rect x="308" y="56" width="58" height="58" rx="16" class="src-bg" /><circle cx="337" cy="85" r="13" class="src-ico" /><circle cx="337" cy="85" r="5" class="src-dot" /></g>
      <path class="arc a1" d="M92 96 Q150 70 186 160" /><path class="arc a2" d="M308 98 Q250 70 214 160" />
    </svg>

  {:else if scene === 'game'}
    <svg viewBox="0 0 400 400" class="svg">
      <g transform="translate(200 168)">
        <circle r="70" class="g-track" stroke-dasharray="{ARC} 999" transform="rotate(135)" />
        <circle r="70" class="g-fill" stroke-dasharray="{ARC * gauge} 999" transform="rotate(135)" />
        <text y="10" class="g-num">{ramGB}</text>
        <text y="34" class="g-unit">{t('wz.unit.gb')}</text>
      </g>
      <g class="stick"><g transform="translate(70 276)">
        <rect width="260" height="62" rx="8" class="s-pcb" />
        {#each Array(8) as _, i}
          <rect x={14 + i * 30} y="12" width="22" height="30" rx="3" class="s-chip" class:on={i < chips} style="--d:{i * .06}s" />
        {/each}
        {#each Array(26) as _, i}
          <rect x={8 + i * 9.4} y="52" width="5" height="10" class="s-pin" />
        {/each}
        <rect x="120" y="52" width="10" height="10" class="s-notch" />
      </g></g>
    </svg>

  {:else if scene === 'notify'}
    <svg viewBox="0 0 400 400" class="svg">
      <g class="bell" class:mute={!notifyOn}><g transform="translate(200 120)">
        <g class="bell-body">
          <path d="M-34 22c6-8 8-18 8-34a26 26 0 0 1 52 0c0 16 2 26 8 34z" class="bell-fill" />
          <rect x="-40" y="20" width="80" height="8" rx="4" class="bell-fill" />
          <circle cy="36" r="8" class="bell-fill" />
        </g>
        <g class="waves">
          <path d="M-56 -26a46 46 0 0 0 0 40" /><path d="M56 -26a46 46 0 0 1 0 40" />
          <path d="M-70 -36a62 62 0 0 0 0 60" /><path d="M70 -36a62 62 0 0 1 0 60" />
        </g>
        <path class="slash" d="M-50 -46L50 50" />
      </g></g>
    </svg>
    <div class="toasts" class:muted={!notifyOn}>
      <div class="toast-c tc1"><PlayerHead skin="alex" size={30} radius={7} online /><div><b>alex_builds</b><span>{t('wz.art.online')}</span></div></div>
      <div class="toast-c tc2"><PlayerHead skin="kai" size={30} radius={7} /><div><b>Kai</b><span>{t('wz.art.msg')}</span></div></div>
      <div class="toast-c tc3"><span class="tc-ico"><i class="ti ti-download"></i></span><div><b>{t('wz.art.pack')}</b><span>{t('wz.art.packDone')}</span></div></div>
    </div>

  {:else if scene === 'behavior'}
    <svg viewBox="0 0 400 400" class="svg">
      <g class="bh-chip c1"><rect x="44" y="34" width="96" height="40" rx="20" class="chip-bg" />
        <circle cx="66" cy="54" r="11" class="chip-ico-bg" /><path d="M66 47v6M61 50.5a7 7 0 1 0 10 0" class="chip-ico" />
        <rect x="88" y="45" width="40" height="18" rx="9" class="tgl" /><circle cx="119" cy="54" r="7" class="tgl-k" /></g>
      <g class="bh-chip c2"><rect x="260" y="34" width="96" height="40" rx="20" class="chip-bg" />
        <circle cx="282" cy="54" r="11" class="chip-ico-bg" />
        <g class="u-spin"><path d="M276 52a6.5 6.5 0 0 1 11-3.5M288 56a6.5 6.5 0 0 1-11 3.5" class="chip-ico" /><path d="M287.5 45v4h-4M276.5 63v-4h4" class="chip-ico" /></g>
        <rect x="302" y="48" width="42" height="7" rx="3.5" class="bar" /><rect x="302" y="48" width="42" height="7" rx="3.5" class="bar-fill" /></g>
      <g class="bh-win">
        <rect x="92" y="98" width="216" height="158" rx="18" class="bw-frame" />
        <rect x="92" y="98" width="216" height="32" rx="18" class="bw-top" /><rect x="92" y="114" width="216" height="16" class="bw-top" />
        <circle cx="286" cy="114" r="4.5" class="bw-dot x" /><circle cx="270" cy="114" r="4.5" class="bw-dot" />
        <image href={appIcon} x="172" y="140" width="56" height="56" class="bw-icon" />
        <rect x="150" y="208" width="100" height="9" rx="4.5" class="bw-line" /><rect x="168" y="226" width="64" height="9" rx="4.5" class="bw-line dim" />
      </g>
      <g class="bh-bar">
        <rect x="40" y="300" width="320" height="44" rx="14" class="tb" />
        <rect x="56" y="313" width="18" height="18" rx="5" class="tb-app" /><rect x="84" y="313" width="18" height="18" rx="5" class="tb-app" /><rect x="112" y="313" width="18" height="18" rx="5" class="tb-app" />
        <rect x="262" y="317" width="34" height="5" rx="2.5" class="tb-clock" /><rect x="268" y="326" width="22" height="4" rx="2" class="tb-clock dim" />
        <g class="tray"><image href={appIcon} x="314" y="310" width="24" height="24" /></g>
      </g>
    </svg>

  {:else if scene === 'done'}
    <svg viewBox="0 0 400 400" class="svg">
      <g class="burst">
        {#each Array(16) as _, i}
          {@const a = (i / 16) * Math.PI * 2}
          <rect x={200 + Math.cos(a) * 30 - 4} y={190 + Math.sin(a) * 30 - 4} width="8" height="8"
                style="--tx:{Math.cos(a) * 130}px;--ty:{Math.sin(a) * 130}px;--d:{.35 + (i % 4) * .05}s" class:alt={i % 3 === 0} />
        {/each}
      </g>
      <circle cx="200" cy="190" r="74" class="done-ring" />
      <circle cx="200" cy="190" r="74" class="done-ring2" />
      <path d="M166 192l24 24l46 -50" class="done-check" />
    </svg>
  {/if}
</div>

<style>
  .art { position: relative; width: 100%; height: 100%; display: grid; place-items: center; overflow: hidden; }
  .svg { width: min(100%, 420px); height: auto; aspect-ratio: 1; overflow: visible; position: relative; z-index: 1; }
  .glow {
    position: absolute; width: 70%; aspect-ratio: 1; border-radius: 50%;
    background: radial-gradient(circle, rgba(var(--orange-rgb), .32), transparent 65%);
    filter: blur(10px); animation: glow 6s ease-in-out infinite;
  }
  @keyframes glow { 0%, 100% { transform: scale(1); opacity: .8 } 50% { transform: scale(1.12); opacity: 1 } }

  /* Голови поверх SVG: координати у % від квадратної сцени. */
  .h { position: absolute; z-index: 2; transform: translate(-50%, -50%); animation: pop .6s cubic-bezier(.34, 1.56, .64, 1) backwards; }
  .h :global(.ph) { animation: bob 4s ease-in-out infinite; }
  @keyframes pop { from { opacity: 0; transform: translate(-50%, -50%) scale(.3) } }
  @keyframes bob { 0%, 100% { translate: 0 0 } 50% { translate: 0 -6px } }
  .h-w1 { left: 17%; top: 52%; animation-delay: .5s } .h-w1 :global(.ph) { animation-delay: -1s }
  .h-w2 { left: 84%; top: 54%; animation-delay: .62s } .h-w2 :global(.ph) { animation-delay: -2s }
  .h-w3 { left: 50%; top: 86%; animation-delay: .74s } .h-w3 :global(.ph) { animation-delay: -3s }

  .h-center { left: 50%; top: 50%; display: flex; flex-direction: column; align-items: center; gap: 8px; animation-delay: .1s }
  .h-center :global(.ph) { animation: none; box-shadow: 0 0 0 4px rgba(var(--orange-rgb), .5), 0 18px 40px rgba(0, 0, 0, .45); }
  .h-name {
    position: absolute; top: calc(100% + 8px); white-space: nowrap;
    font-size: 12px; font-weight: 800; padding: 3px 10px; border-radius: 999px;
    background: var(--bg-card); border: 1px solid var(--border-hi);
  }
  .h-a1 { left: 25%; top: 27.5%; animation-delay: .35s } .h-a1 :global(.ph) { animation-delay: -.5s }
  .h-a2 { left: 77.5%; top: 29%; animation-delay: .45s } .h-a2 :global(.ph) { animation-delay: -1.5s }
  .h-a3 { left: 23%; top: 73%; animation-delay: .55s } .h-a3 :global(.ph) { animation-delay: -2.5s }
  .h-a4 { left: 78.5%; top: 72.5%; animation-delay: .65s } .h-a4 :global(.ph) { animation-delay: -3s }
  .h-a5 { left: 50%; top: 17.5%; animation-delay: .75s } .h-a5 :global(.ph) { animation-delay: -2s }

  /* ── загальне ── */
  @keyframes float { 0%, 100% { translate: 0 0 } 50% { translate: 0 -10px } }

  /* ── welcome ── */
  .ring circle { fill: none; stroke: rgba(var(--orange-rgb), .35); stroke-width: 1.5; stroke-dasharray: 2 10; stroke-linecap: round; }
  .ring { transform-origin: 200px 200px; animation: spin 40s linear infinite, fade-in 1s ease backwards; }
  .r2 { animation-duration: 28s, 1s; animation-direction: reverse, normal; }
  .r2 circle { stroke: rgba(var(--orange-rgb), .55); stroke-dasharray: 18 14; }
  .r3 circle { stroke: rgba(255, 255, 255, .08); stroke-dasharray: 1 7; }
  @keyframes spin { to { transform: rotate(360deg) } }
  @keyframes fade-in { from { opacity: 0 } }
  .logo { transform-origin: 200px 200px; animation: logo-in .9s cubic-bezier(.34, 1.56, .64, 1) backwards .1s; }
  @keyframes logo-in { from { transform: scale(.2) rotate(-25deg); opacity: 0 } }
  .logo-img { filter: drop-shadow(0 16px 30px rgba(var(--orange-rgb), .45)); transform-origin: 200px 200px; animation: logo-bob 4.5s ease-in-out infinite 1s; }
  @keyframes logo-bob { 0%, 100% { transform: translateY(0) rotate(0) } 50% { transform: translateY(-8px) rotate(-3deg) } }
  .sparks rect { fill: rgba(var(--orange-rgb), .9); animation: twinkle 3s ease-in-out infinite var(--d); transform-box: fill-box; transform-origin: center; }
  @keyframes twinkle { 0%, 100% { opacity: .15; transform: scale(.6) rotate(0) } 50% { opacity: 1; transform: scale(1.2) rotate(45deg) } }

  /* ── look ── */
  .win { animation: win-in .8s cubic-bezier(.22, 1, .36, 1) backwards; transform-origin: 200px 190px; }
  @keyframes win-in { from { opacity: 0; transform: translateY(24px) scale(.94) } }
  .w-frame { fill: var(--bg-window); stroke: var(--border-hi); stroke-width: 1.5; filter: drop-shadow(0 24px 40px rgba(0, 0, 0, .45)); transition: fill .4s; }
  .w-top { fill: var(--bg-card); transition: fill .4s; }
  .w-dot { fill: var(--text-dim); }
  .w-side { fill: var(--bg-panel); transition: fill .4s; }
  .w-ico { fill: var(--hover-strong); transition: fill .4s; }
  .w-ico.acc { fill: var(--orange); }
  .w-hero { fill: var(--hover); stroke: var(--border-hi); }
  .w-line { fill: var(--hover-strong); } .w-line.hi { fill: var(--text); opacity: .85 } .w-line.acc { fill: var(--orange); }
  .w-btn { fill: var(--orange); transition: fill .4s; }
  .w-card { animation: fade-up .55s cubic-bezier(.22, 1, .36, 1) backwards var(--d); }
  .w-c { fill: var(--bg-card); stroke: var(--border); transition: fill .4s; }
  .w-thumb { fill: var(--hover-strong); }
  @keyframes fade-up { from { opacity: 0; transform: translateY(12px) } }
  .swatches circle { animation: pop-c .5s cubic-bezier(.34, 1.56, .64, 1) backwards var(--d); transform-box: fill-box; transform-origin: center; stroke: rgba(255, 255, 255, .25); stroke-width: 2; }
  @keyframes pop-c { from { transform: scale(0) } }

  .cursor { animation: cur-move 10s cubic-bezier(.65, 0, .35, 1) infinite .8s, fade-in .5s backwards .8s; }
  @keyframes cur-move {
    0% { transform: translate(307px, 343px) }
    6% { transform: translate(91px, 343px) }
    18% { transform: translate(91px, 343px) }
    26% { transform: translate(145px, 343px) }
    38% { transform: translate(145px, 343px) }
    46% { transform: translate(199px, 343px) }
    58% { transform: translate(199px, 343px) }
    66% { transform: translate(253px, 343px) }
    78% { transform: translate(253px, 343px) }
    86% { transform: translate(307px, 343px) }
    98% { transform: translate(307px, 343px) }
    100% { transform: translate(307px, 343px) }
  }
  .cur-press { animation: cur-press 2s ease-in-out infinite .8s; transform-box: fill-box; transform-origin: 0 0; }
  @keyframes cur-press { 0%, 38% { transform: scale(1) } 45% { transform: scale(.8) } 55%, 100% { transform: scale(1) } }
  .cur { fill: #fff; stroke: #111; stroke-width: 1.6; stroke-linejoin: round; filter: drop-shadow(0 4px 6px rgba(0, 0, 0, .45)); }
  .ripple { fill: none; stroke-width: 3; opacity: 0; transform-box: fill-box; transform-origin: center; animation: ripple 10s ease-out infinite var(--rd); }
  @keyframes ripple { 0% { opacity: 0; transform: scale(1) } 0.5% { opacity: .9; transform: scale(1) } 7% { opacity: 0; transform: scale(2.3) } 100% { opacity: 0 } }
  .win .acc, .win .w-btn { animation: acc-cycle 10s linear infinite .8s; }
  @keyframes acc-cycle {
    0% { fill: #d62438 }
    9% { fill: #d62438 }
    11% { fill: #c24b00 }
    29% { fill: #c24b00 }
    31% { fill: #7536e8 }
    49% { fill: #7536e8 }
    51% { fill: #008052 }
    69% { fill: #008052 }
    71% { fill: #1664d9 }
    89% { fill: #1664d9 }
    91% { fill: #d62438 }
    100% { fill: #d62438 }
  }

  /* ── account ── */
  .links path { stroke: rgba(var(--orange-rgb), .6); stroke-width: 2; stroke-dasharray: 4 7; stroke-linecap: round; fill: none; animation: dash 1.6s linear infinite, fade-in .8s ease backwards .3s; }
  @keyframes dash { to { stroke-dashoffset: -22 } }
  .halo { fill: none; stroke: rgba(var(--orange-rgb), .5); stroke-width: 2; transform-origin: 200px 200px; animation: halo 2.8s ease-out infinite; }
  .halo.h2 { animation-delay: 1.4s; }
  @keyframes halo { from { transform: scale(.7); opacity: .9 } to { transform: scale(1.5); opacity: 0 } }
  .bubble { animation: bub-in .5s cubic-bezier(.34, 1.56, .64, 1) backwards, float 4s ease-in-out infinite; transform-box: fill-box; transform-origin: bottom left; }
  .bb1 { animation-delay: .9s, 1.4s } .bb2 { animation-delay: 1.2s, 2s }
  @keyframes bub-in { from { transform: scale(0); opacity: 0 } }
  .bub { fill: var(--bg-card); stroke: var(--border-hi); } .bub.acc { fill: var(--orange); stroke: none; }
  .bub-dot { fill: var(--text-mute); animation: typing 1.2s ease-in-out infinite var(--d); }
  @keyframes typing { 0%, 100% { opacity: .3; transform: translateY(0) } 50% { opacity: 1; transform: translateY(-3px) } }

  /* ── files ── */
  .fold { animation: fade-up .7s cubic-bezier(.22, 1, .36, 1) backwards .1s; }
  .f-back { fill: color-mix(in srgb, var(--orange) 55%, #000); }
  .f-front { fill: var(--orange); filter: drop-shadow(0 -6px 18px rgba(0, 0, 0, .35)); }
  .f-line { fill: rgba(255, 255, 255, .45); }
  .pk-c { fill: var(--bg-card); stroke: var(--border-hi); stroke-width: 1.5; }
  .pk-th { fill: rgba(var(--orange-rgb), .45); } .pk-th.b { fill: rgba(91, 141, 239, .6); } .pk-th.c { fill: rgba(16, 185, 129, .55); }
  .pk-l { fill: var(--hover-strong); }
  .pk { animation: drop 3.6s cubic-bezier(.5, 0, .3, 1) infinite; }
  .p1 { animation-delay: .4s } .p2 { animation-delay: 1.6s } .p3 { animation-delay: 2.8s }
  @keyframes drop { 0% { translate: 0 -150px; opacity: 0 } 15% { opacity: 1 } 45%, 80% { translate: 0 0; opacity: 1 } 100% { translate: 0 10px; opacity: 0 } }
  .src { animation: pop-c .55s cubic-bezier(.34, 1.56, .64, 1) backwards; transform-box: fill-box; transform-origin: center; }
  .s1 { animation-delay: .3s } .s2 { animation-delay: .45s }
  .src-bg { fill: var(--bg-card); stroke: var(--border-hi); stroke-width: 1.5; filter: drop-shadow(0 10px 20px rgba(0, 0, 0, .35)); }
  .src-ico { fill: none; stroke: #8fd14f; stroke-width: 4; stroke-linejoin: round; }
  .s2 .src-ico { stroke: #1bd96a; } .src-dot { fill: #1bd96a; }
  .arc { fill: none; stroke: rgba(var(--orange-rgb), .7); stroke-width: 2.5; stroke-dasharray: 4 8; stroke-linecap: round; animation: dash 1.2s linear infinite reverse, fade-in .8s backwards .6s; }

  /* ── game ── */
  .g-track { fill: none; stroke: var(--hover-strong); stroke-width: 14; stroke-linecap: round; }
  .g-fill { fill: none; stroke: var(--orange); stroke-width: 14; stroke-linecap: round; transition: stroke-dasharray .45s cubic-bezier(.22, 1, .36, 1); filter: drop-shadow(0 0 10px rgba(var(--orange-rgb), .6)); animation: fade-in .6s backwards .2s; }
  .g-num { text-anchor: middle; font-size: 46px; font-weight: 800; fill: var(--text); font-family: var(--font-mono); }
  .g-unit { text-anchor: middle; font-size: 13px; font-weight: 700; fill: var(--text-mute); letter-spacing: 2px; }
  .stick { animation: fade-up .7s cubic-bezier(.22, 1, .36, 1) backwards .2s; }
  .s-pcb { fill: #1f5b3a; stroke: #2f7a50; stroke-width: 2; }
  .s-chip { fill: #142a1e; transition: fill .3s, filter .3s; transition-delay: var(--d); }
  .s-chip.on { fill: var(--orange); filter: drop-shadow(0 0 6px rgba(var(--orange-rgb), .8)); }
  .s-pin { fill: #d8b25a; } .s-notch { fill: var(--bg-window); }

  /* ── notify ── */
  .bell { animation: fade-in .6s backwards; }
  .bell-body { transform-origin: 0 -38px; animation: ring 2.6s ease-in-out infinite .6s; }
  .bell-fill { fill: var(--orange); filter: drop-shadow(0 10px 24px rgba(var(--orange-rgb), .5)); transition: fill .3s; }
  @keyframes ring { 0%, 50%, 100% { transform: rotate(0) } 8% { transform: rotate(16deg) } 16% { transform: rotate(-14deg) } 24% { transform: rotate(10deg) } 32% { transform: rotate(-6deg) } 40% { transform: rotate(2deg) } }
  .waves path { fill: none; stroke: rgba(var(--orange-rgb), .7); stroke-width: 4; stroke-linecap: round; animation: wave 2.6s ease-out infinite .6s; }
  .waves path:nth-child(n+3) { animation-delay: .75s; stroke-width: 3; }
  @keyframes wave { 0%, 45%, 100% { opacity: 0 } 10% { opacity: 1 } }
  .slash { stroke: var(--red); stroke-width: 8; stroke-linecap: round; stroke-dasharray: 150; stroke-dashoffset: 150; transition: stroke-dashoffset .4s ease; }
  .bell.mute .slash { stroke-dashoffset: 0; }
  .bell.mute .bell-body { animation: none; }
  .bell.mute .waves path { animation: none; opacity: 0; }
  .bell.mute .bell-fill { fill: var(--text-dim); filter: none; }
  .toasts { position: absolute; z-index: 2; left: 50%; top: 58%; transform: translateX(-50%); width: min(78%, 300px); display: flex; flex-direction: column; gap: 8px; transition: opacity .3s, filter .3s; }
  .toasts.muted { opacity: .3; filter: grayscale(1); }
  .toast-c {
    display: flex; align-items: center; gap: 10px; padding: 9px 12px; border-radius: 12px;
    background: var(--bg-card); border: 1px solid var(--border-hi); box-shadow: 0 14px 30px rgba(0, 0, 0, .35);
    animation: toast-in .55s cubic-bezier(.22, 1, .36, 1) backwards;
  }
  .toast-c div { display: flex; flex-direction: column; min-width: 0; }
  .toast-c b { font-size: 12px; } .toast-c span { font-size: 11px; color: var(--text-mute); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .tc1 { animation-delay: .5s } .tc2 { animation-delay: .9s; margin-left: 14px } .tc3 { animation-delay: 1.3s; margin-left: 28px }
  .tc-ico { width: 30px; height: 30px; border-radius: 7px; display: grid; place-items: center; background: rgba(16, 185, 129, .16); color: var(--green); font-size: 16px; flex-shrink: 0; }
  @keyframes toast-in { from { opacity: 0; transform: translateX(40px) } }

  /* ── behavior ── */
  .bh-chip { animation: pop-c .55s cubic-bezier(.34, 1.56, .64, 1) backwards; transform-box: fill-box; transform-origin: center; }
  .c1 { animation-delay: .35s } .c2 { animation-delay: .5s }
  .chip-bg { fill: var(--bg-card); stroke: var(--border-hi); stroke-width: 1.5; filter: drop-shadow(0 10px 18px rgba(0, 0, 0, .35)); }
  .chip-ico-bg { fill: rgba(var(--orange-rgb), .18); }
  .chip-ico { fill: none; stroke: var(--orange); stroke-width: 2.4; stroke-linecap: round; stroke-linejoin: round; }
  .tgl { fill: var(--orange); }
  .tgl-k { fill: #fff; }
  .u-spin { transform-box: fill-box; transform-origin: center; animation: spin 2.4s linear infinite; }
  .bar { fill: var(--hover-strong); }
  .bar-fill { fill: var(--green); transform-box: fill-box; transform-origin: left center; animation: bar 3s ease-in-out infinite 1s; }
  @keyframes bar { 0% { transform: scaleX(.05) } 70%, 100% { transform: scaleX(1) } }
  .bh-win { transform-box: view-box; transform-origin: 326px 322px; animation: win-in .8s cubic-bezier(.22, 1, .36, 1) backwards .1s, to-tray 7s cubic-bezier(.65, 0, .35, 1) infinite 1.6s; }
  @keyframes to-tray {
    0%, 30% { transform: none; opacity: 1 }
    46%, 66% { transform: translate(0, 0) scale(.08); opacity: 0 }
    84%, 100% { transform: none; opacity: 1 }
  }
  .bw-frame { fill: var(--bg-card); stroke: var(--border-hi); stroke-width: 1.5; filter: drop-shadow(0 24px 40px rgba(0, 0, 0, .45)); }
  .bw-top { fill: var(--hover-strong); }
  .bw-dot { fill: var(--text-dim); } .bw-dot.x { fill: var(--red); }
  .bw-icon { filter: drop-shadow(0 8px 16px rgba(var(--orange-rgb), .4)); }
  .bw-line { fill: var(--hover-strong); } .bw-line.dim { opacity: .6; }
  .bh-bar { animation: fade-up .6s cubic-bezier(.22, 1, .36, 1) backwards .25s; }
  .tb { fill: var(--bg-panel); stroke: var(--border-hi); stroke-width: 1.5; }
  .tb-app { fill: var(--hover-strong); }
  .tb-clock { fill: var(--text-dim); } .tb-clock.dim { opacity: .6; }
  .tray { transform-box: fill-box; transform-origin: center; animation: tray 7s ease-in-out infinite 1.6s; }
  @keyframes tray { 0%, 40% { transform: scale(1) } 48% { transform: scale(1.35) } 56%, 100% { transform: scale(1) } }

  /* ── done ── */
  .done-ring { fill: rgba(16, 185, 129, .12); stroke: var(--green); stroke-width: 6; stroke-dasharray: 466; stroke-dashoffset: 466; transform: rotate(-90deg); transform-origin: 200px 190px; animation: draw .8s cubic-bezier(.65, 0, .35, 1) forwards .1s; }
  .done-ring2 { fill: none; stroke: var(--green); stroke-width: 2; transform-origin: 200px 190px; animation: halo 2.4s ease-out infinite 1s; opacity: 0; }
  .done-check { fill: none; stroke: var(--green); stroke-width: 12; stroke-linecap: round; stroke-linejoin: round; stroke-dasharray: 110; stroke-dashoffset: 110; animation: draw .5s cubic-bezier(.65, 0, .35, 1) forwards .75s; }
  @keyframes draw { to { stroke-dashoffset: 0 } }
  .burst rect { fill: var(--orange); opacity: 0; animation: burst 1.1s cubic-bezier(.22, 1, .36, 1) forwards var(--d); transform-box: fill-box; transform-origin: center; }
  .burst rect.alt { fill: var(--green); }
  @keyframes burst { 0% { opacity: 1; transform: translate(0, 0) scale(1) rotate(0) } 100% { opacity: 0; transform: translate(var(--tx), var(--ty)) scale(.4) rotate(180deg) } }

  @media (prefers-reduced-motion: reduce) {
    .art *, .art :global(*) { animation-duration: .01ms !important; animation-iteration-count: 1 !important; animation-delay: 0s !important; }
  }
</style>
