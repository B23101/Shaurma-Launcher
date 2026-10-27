<script lang="ts">
  // Аватар: сквіркл із головою (або літерою), кільце кольору статусу та
  // «значок» статусу в куті. У грі — кільце кольору збірки з м'яким світінням.
  // Усе статичне (box-shadow без анімації): сотня аватарів не навантажує GPU.
  let { head = '', initial = '?', size = 38, status = '' as '' | 'online' | 'in_game' | 'away' | 'offline', ring = '' }: {
    head?: string; initial?: string; size?: number; status?: '' | 'online' | 'in_game' | 'away' | 'offline'
    /** Колір кільця (збірка гравця) — діє лише для in_game. */
    ring?: string
  } = $props()
</script>

<span class="fa {status}" style="--sz:{size}px;{ring ? `--ring:${ring}` : ''}">
  <span class="fa-face">
    {#if head}<img src={head} alt="" decoding="async" draggable="false" />{:else}<b>{initial}</b>{/if}
  </span>
  {#if status && status !== 'offline'}<i class="fa-dot" aria-hidden="true"></i>{/if}
</span>

<style>
  .fa { --ring: var(--orange); position: relative; flex: 0 0 auto; width: var(--sz); height: var(--sz); display: inline-block; }
  .fa-face { display: grid; place-items: center; width: 100%; height: 100%; overflow: hidden; border-radius: calc(var(--sz) * .34);
    background: linear-gradient(145deg, var(--bg-input-hi, var(--bg-input)), var(--bg-input)); box-shadow: inset 0 0 0 1px var(--border-hi); }
  .fa-face img { width: 100%; height: 100%; object-fit: cover; image-rendering: pixelated; display: block; }
  .fa-face b { font: 800 calc(var(--sz) * .4)/1 var(--font-ui); color: var(--orange); }
  .fa.in_game .fa-face { box-shadow: 0 0 0 2px var(--bg-panel), 0 0 0 3.5px var(--ring), 0 0 12px color-mix(in srgb, var(--ring) 45%, transparent); }
  .fa.offline .fa-face img { filter: grayscale(.55) brightness(.85); }
  .fa-dot { position: absolute; right: -3px; bottom: -3px; width: 12px; height: 12px; border-radius: 50%; box-sizing: border-box; border: 2.5px solid var(--bg-panel); background: var(--text-dim); }
  .fa.online .fa-dot { background: var(--ff-ok); }
  .fa.away .fa-dot { background: var(--ff-warn); }
  .fa.in_game .fa-dot { background: var(--ring); right: -4px; bottom: -4px; }
</style>
