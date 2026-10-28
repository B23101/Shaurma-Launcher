<script lang="ts">
  // PackCompareChip — обгортка над СПІЛЬНИМ CompareChip для порівняння
  // ЗБІРОК. Історично цей компонент і був шаблоном; тепер сама верстка/
  // стилі живуть у CompareChip.svelte, а тут лишилась лише типізація
  // ModComparePack → (src/ti/color/name/meta).
  import CompareChip from './CompareChip.svelte'
  import { packAsset } from './packAssets.svelte'
  import type { ModComparePack } from './types'

  let {
    pack = null as null | ModComparePack,
    label = '',
    clickable = true,
    onclick = () => {},
  }: {
    pack?: null | ModComparePack
    label?: string
    clickable?: boolean
    onclick?: () => void
  } = $props()

  // PNG збірки має пріоритет над пресетом — той самий принцип, що скрізь.
  const src = $derived(pack ? packAsset(pack.iconUrl) : undefined)
</script>

<CompareChip
  {label}
  {clickable}
  {onclick}
  name={pack?.name ?? ''}
  meta={pack ? `${pack.loader} · ${pack.mcVersion}` : ''}
  src={src}
  ti={pack?.icon ?? ''}
  color={pack?.color ?? ''}
  empty={!pack}
/>
