<script lang="ts">
  // Жива 3D-модель гравця в лівій панелі майстра (той самий переглядач, що в
  // гардеробі). three.js вантажиться лише тут, динамічно.
  import { onDestroy, onMount } from 'svelte'
  import type { WardrobeViewerHandle } from '../wardrobe3d'

  let { texture = '', slim = false, name = '' }: { texture?: string; slim?: boolean; name?: string } = $props()

  let box: HTMLDivElement | undefined = $state()
  let viewer: WardrobeViewerHandle | null = null
  let ready = $state(false)
  let alive = true

  onMount(async () => {
    try {
      const mod = await import('../wardrobe3d')
      if (!alive || !box) return
      viewer = await mod.createWardrobeViewer(box)
      if (!alive) { viewer.dispose(); viewer = null; return }
      viewer.setSlim(slim)
      if (texture) viewer.setTexture(texture)
      viewer.playAnimation('idle')
      ready = true
    } catch (e) { console.error('skin stage', e) }
  })
  onDestroy(() => { alive = false; viewer?.dispose(); viewer = null })

  let lastSlim: boolean | null = null
  $effect(() => {
    const s = slim, tx = texture
    if (!ready || !viewer) return
    if (lastSlim !== s) { viewer.setSlim(s); lastSlim = s }
    if (tx) viewer.setTexture(tx)
    viewer.playAnimation('look')
  })
</script>

<div class="ss">
  <div class="ss-floor"></div>
  <div class="ss-view" class:on={ready} bind:this={box}></div>
  {#if !ready}<div class="ss-load"><span></span></div>{/if}
  {#if name}<div class="ss-name">{name}</div>{/if}
</div>

<style>
  .ss { position: relative; width: 100%; height: 100%; display: grid; place-items: center; }
  .ss-view { position: absolute; inset: 6% 8% 12%; opacity: 0; transform: translateY(16px) scale(.96); transition: opacity .6s, transform .7s cubic-bezier(.22, 1, .36, 1); }
  .ss-view.on { opacity: 1; transform: none; }
  .ss-view :global(canvas) { width: 100% !important; height: 100% !important; display: block; cursor: grab; }
  .ss-view :global(canvas:active) { cursor: grabbing; }
  .ss-floor {
    position: absolute; bottom: 13%; width: 56%; height: 9%; border-radius: 50%;
    background: radial-gradient(closest-side, rgba(var(--orange-rgb), .45), transparent);
    animation: ss-pulse 3s ease-in-out infinite;
  }
  @keyframes ss-pulse { 0%, 100% { transform: scaleX(1); opacity: .8 } 50% { transform: scaleX(1.12); opacity: 1 } }
  .ss-load span { display: block; width: 34px; height: 34px; border-radius: 50%; border: 3px solid rgba(var(--orange-rgb), .25); border-top-color: var(--orange); animation: ss-spin .8s linear infinite; }
  @keyframes ss-spin { to { transform: rotate(360deg) } }
  .ss-name {
    position: absolute; bottom: 6%; padding: 4px 12px; border-radius: 999px; font-size: 12.5px; font-weight: 800;
    background: var(--bg-card); border: 1px solid var(--border-hi); animation: ss-in .5s cubic-bezier(.34, 1.56, .64, 1) backwards .3s;
  }
  @keyframes ss-in { from { opacity: 0; transform: translateY(8px) scale(.9) } }
</style>
