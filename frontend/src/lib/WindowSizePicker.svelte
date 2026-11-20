<script lang="ts">
  // Розмір вікна гри: готові шаблони або свій розмір (ширина × висота).
  import NumberInput from './NumberInput.svelte'
  import { t } from './i18n.svelte'

  let {
    width,
    height,
    disabled = false,
    onchange,
  }: {
    width: number
    height: number
    disabled?: boolean
    onchange: (w: number, h: number) => void
  } = $props()

  const PRESETS: [number, number][] = [[854, 480], [1280, 720], [1600, 900], [1920, 1080]]
  const isPreset = $derived(PRESETS.some(([w, h]) => w === width && h === height))
  // «Свій» відкривається кліком і лишається відкритим, поки розмір не збігся з шаблоном.
  let customOpen = $state(false)
  const custom = $derived(customOpen || !isPreset)

  const clampW = (n: number) => Math.max(480, Math.min(7680, Math.round(n) || 854))
  const clampH = (n: number) => Math.max(360, Math.min(4320, Math.round(n) || 480))
</script>

<div class="wsp" class:off={disabled}>
  <div class="st-seg" role="radiogroup" aria-label={t('window.size')}>
    {#each PRESETS as [w, h] (w)}
      <button type="button" role="radio" aria-checked={!custom && width === w && height === h}
              class:on={!custom && width === w && height === h} {disabled}
              onclick={() => { customOpen = false; onchange(w, h) }}>{w}×{h}</button>
    {/each}
    <button type="button" role="radio" aria-checked={custom} class:on={custom} {disabled}
            onclick={() => { customOpen = true }}>{t('window.custom')}</button>
  </div>
  {#if custom}
    <div class="wsp-custom">
      <NumberInput class="st-input num mono" style="width:86px" value={width} min={480} max={7680} step={10} {disabled}
                   aria-label={t('window.width')}
                   onchange={(e) => onchange(clampW(Number((e.currentTarget as HTMLInputElement).value)), height)} />
      <span class="wsp-x">×</span>
      <NumberInput class="st-input num mono" style="width:86px" value={height} min={360} max={4320} step={10} {disabled}
                   aria-label={t('window.height')}
                   onchange={(e) => onchange(width, clampH(Number((e.currentTarget as HTMLInputElement).value)))} />
      <span class="st-unit">px</span>
    </div>
  {/if}
</div>

<style>
  .wsp { display: flex; flex-direction: column; align-items: flex-end; gap: 8px; min-width: 0; }
  .wsp .st-seg { flex-wrap: wrap; justify-content: flex-end; max-width: 100%; }
  .wsp .st-seg button { font-family: var(--font-mono); font-size: 12px; }
  .wsp.off { opacity: .5; pointer-events: none; }
  .wsp-custom { display: flex; align-items: center; gap: 6px; }
  .wsp-x { color: var(--text-dim); font-weight: 700; }
</style>
