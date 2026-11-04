<script lang="ts">
  // Банер над кнопками входу (LoginModal): живі голови гравців зі справжніх
  // скінів, бульбашки чату і короткий перелік того, що дає акаунт Шаурми.
  // Тексту мінімум: людина і так розуміє, що таке вхід.
  import { t } from './i18n.svelte'
  import PlayerHead from './wizard/PlayerHead.svelte'

  let { compact = false }: { compact?: boolean } = $props()

  const heads = [
    { skin: 'alex', size: 40, x: 14, y: 58, online: true },
    { skin: 'kai', size: 34, x: 31, y: 36, online: false },
    { skin: 'steve', size: 52, x: 50, y: 50, online: true },
    { skin: 'zuri', size: 36, x: 69, y: 34, online: true },
    { skin: 'noor', size: 40, x: 86, y: 58, online: false },
  ]
  const perks = [
    { icon: 'ti-messages', key: 'perks.friends.title' },
    { icon: 'ti-world-share', key: 'perks.p2p.title' },
    { icon: 'ti-hanger', key: 'perks.skin.title' },
    { icon: 'ti-cloud-check', key: 'perks.time.title' },
  ]
</script>

<section class="sp" class:compact aria-label={t('perks.title')}>
  <div class="sp-art" aria-hidden="true">
    <svg class="sp-lines" viewBox="0 0 100 100" preserveAspectRatio="none">
      <path d="M14 58 Q22 30 31 36 T50 50 T69 34 T86 58" />
    </svg>
    {#each heads as h, i}
      <div class="sp-h" style="left:{h.x}%;top:{h.y}%;--i:{i}">
        <PlayerHead skin={h.skin} size={compact ? Math.round(h.size * .85) : h.size} radius={8} online={h.online} />
      </div>
    {/each}
    <div class="sp-bub b1"><span></span><span></span><span></span></div>
    <div class="sp-bub b2"><i class="ti ti-heart-filled"></i></div>
  </div>

  <div class="sp-body">
    <h2 class="sp-title">{t('perks.title')}</h2>
    <ul class="sp-list">
      {#each perks as p, i}
        <li style="--i:{i}"><i class="ti {p.icon}"></i>{t(p.key)}</li>
      {/each}
    </ul>
  </div>
</section>

<style>
  .sp {
    border: 1px solid rgba(var(--orange-rgb), .25); border-radius: 14px; overflow: hidden;
    background: linear-gradient(180deg, rgba(var(--orange-rgb), .06), transparent 60%), var(--bg-panel);
  }
  .sp-art {
    position: relative; height: 128px; overflow: hidden;
    border-bottom: 1px solid rgba(var(--orange-rgb), .18);
    background:
      radial-gradient(60% 90% at 50% 40%, rgba(var(--orange-rgb), .35), transparent 70%),
      linear-gradient(180deg, rgba(var(--orange-rgb), .22), rgba(var(--orange-rgb), .03));
  }
  .sp-art::before {
    content: ''; position: absolute; inset: 0; opacity: .35;
    background-image: linear-gradient(rgba(255, 255, 255, .06) 1px, transparent 1px), linear-gradient(90deg, rgba(255, 255, 255, .06) 1px, transparent 1px);
    background-size: 16px 16px;
  }
  .sp-lines { position: absolute; inset: 0; width: 100%; height: 100%; }
  .sp-lines path {
    fill: none; stroke: rgba(var(--orange-rgb), .75); stroke-width: 1.4; stroke-dasharray: 2 4;
    vector-effect: non-scaling-stroke; animation: sp-dash 1.8s linear infinite;
  }
  .sp-h {
    position: absolute; transform: translate(-50%, -50%); z-index: 1;
    animation: sp-pop .55s cubic-bezier(.34, 1.56, .64, 1) backwards; animation-delay: calc(.08s + var(--i) * .07s);
  }
  .sp-h :global(.ph) { animation: sp-bob 4s ease-in-out infinite; animation-delay: calc(var(--i) * -.8s); }
  .sp-bub {
    position: absolute; z-index: 2; display: flex; align-items: center; justify-content: center; gap: 3px;
    height: 22px; padding: 0 9px; border-radius: 9px 9px 9px 3px;
    animation: sp-pop .45s cubic-bezier(.34, 1.56, .64, 1) backwards .6s, sp-bob 3.6s ease-in-out infinite 1s;
  }
  .b1 { left: 36%; top: 10%; background: var(--bg-card); border: 1px solid var(--border-hi); }
  .b1 span { width: 4px; height: 4px; border-radius: 50%; background: var(--text-mute); animation: sp-typing 1.2s ease-in-out infinite; }
  .b1 span:nth-child(2) { animation-delay: .15s } .b1 span:nth-child(3) { animation-delay: .3s }
  .b2 { left: 74%; top: 6%; background: var(--orange); color: #fff; font-size: 12px; animation-delay: .8s, 1.6s; }
  .sp-body { padding: 12px 14px 14px; }
  .sp-title { margin: 0 0 10px; font-size: 15px; font-weight: 800; line-height: 1.25; color: var(--text); }
  .sp-list { list-style: none; margin: 0; padding: 0; display: flex; flex-wrap: wrap; gap: 6px; }
  .sp-list li {
    display: inline-flex; align-items: center; gap: 6px; padding: 6px 10px; border-radius: 999px;
    font-size: 11.5px; font-weight: 600; color: var(--text);
    background: rgba(255, 255, 255, .04); border: 1px solid var(--border);
    animation: sp-rise .4s cubic-bezier(.22, 1, .36, 1) backwards; animation-delay: calc(.35s + var(--i) * .06s);
    transition: border-color .15s, transform .15s;
  }
  .sp-list li:hover { border-color: rgba(var(--orange-rgb), .5); transform: translateY(-1px); }
  .sp-list li i { color: var(--orange); font-size: 14px; }

  .compact .sp-art { height: 104px; }
  .compact .sp-body { padding: 10px 12px 12px; }

  @keyframes sp-pop { from { opacity: 0; scale: .3 } }
  @keyframes sp-rise { from { opacity: 0; transform: translateY(6px) } }
  @keyframes sp-bob { 0%, 100% { translate: 0 0 } 50% { translate: 0 -4px } }
  @keyframes sp-dash { to { stroke-dashoffset: -12 } }
  @keyframes sp-typing { 0%, 100% { opacity: .3; transform: translateY(0) } 50% { opacity: 1; transform: translateY(-2px) } }
  @media (prefers-reduced-motion: reduce) {
    .sp :global(*) { animation: none !important; }
  }
</style>
