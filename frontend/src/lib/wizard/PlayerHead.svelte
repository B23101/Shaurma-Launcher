<script lang="ts">
  // 2D-голова гравця, вирізана прямо зі скіна 64×64 (обличчя 8×8 + шар
  // «капелюха»), або готова картинка голови (src). Пікселі без розмиття.
  import steve from '../../assets/skins/steve.png'
  import alex from '../../assets/skins/alex.png'
  import ari from '../../assets/skins/ari.png'
  import efe from '../../assets/skins/efe.png'
  import kai from '../../assets/skins/kai.png'
  import makena from '../../assets/skins/makena.png'
  import noor from '../../assets/skins/noor.png'
  import sunny from '../../assets/skins/sunny.png'
  import zuri from '../../assets/skins/zuri.png'

  const SKINS: Record<string, string> = { steve, alex, ari, efe, kai, makena, noor, sunny, zuri }

  let { skin = 'steve', src = '', size = 40, radius = 8, online = false }: {
    skin?: string; src?: string; size?: number; radius?: number; online?: boolean
  } = $props()

  const url = $derived(SKINS[skin] ?? steve)
</script>

<span class="ph" style="--s:{size}px;--r:{radius}px">
  {#if src}
    <img class="ph-img" src={src} alt="" draggable="false" />
  {:else}
    <span class="ph-face" style="background-image:url({url})"></span>
    <span class="ph-hat" style="background-image:url({url})"></span>
  {/if}
  {#if online}<span class="ph-dot"></span>{/if}
</span>

<style>
  .ph {
    position: relative; display: inline-block; flex-shrink: 0;
    width: var(--s); height: var(--s); border-radius: var(--r);
    box-shadow: 0 6px 16px rgba(0, 0, 0, .35), inset 0 0 0 1px rgba(255, 255, 255, .06);
  }
  .ph-face, .ph-hat, .ph-img {
    position: absolute; inset: 0; border-radius: var(--r); overflow: hidden;
    image-rendering: pixelated; image-rendering: crisp-edges;
  }
  .ph-img { width: 100%; height: 100%; object-fit: cover; }
  /* Скін 64×64: обличчя на (8,8), шар капелюха на (40,8). Масштаб ×(s/8). */
  .ph-face, .ph-hat {
    background-repeat: no-repeat;
    background-size: calc(var(--s) * 8) calc(var(--s) * 8);
  }
  .ph-face { background-position: calc(var(--s) * -1) calc(var(--s) * -1); }
  .ph-hat {
    inset: calc(var(--s) * -.06);
    background-size: calc(var(--s) * 1.12 * 8) calc(var(--s) * 1.12 * 8);
    background-position: calc(var(--s) * 1.12 * -5) calc(var(--s) * 1.12 * -1);
  }
  .ph-dot {
    position: absolute; right: -3px; bottom: -3px;
    width: calc(var(--s) * .28); height: calc(var(--s) * .28); min-width: 9px; min-height: 9px;
    border-radius: 50%; background: var(--green); border: 2px solid var(--bg-panel, #111);
    animation: ph-pulse 2.4s ease-in-out infinite;
  }
  @keyframes ph-pulse {
    0%, 100% { box-shadow: 0 0 0 0 rgba(16, 185, 129, .55); }
    50% { box-shadow: 0 0 0 5px rgba(16, 185, 129, 0); }
  }
  @media (prefers-reduced-motion: reduce) { .ph-dot { animation: none; } }
</style>
