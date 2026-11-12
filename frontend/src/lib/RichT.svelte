<script lang="ts">
  // RichT.svelte — переклад із клікабельними шляхами до налаштувань.
  // Маркер {path:ref} у рядку перекладу стає посиланням (див. settings/nav.ts).
  import { t } from './i18n.svelte'
  import { splitPaths } from './settings/nav'
  import SettingPath from './settings/SettingPath.svelte'

  let { key, params }: { key: string; params?: Record<string, string | number> } = $props()
  const parts = $derived(splitPaths(t(key, params)))
</script>

{#each parts as p}{#if p.ref}<SettingPath ref={p.ref} />{:else}{p.text}{/if}{/each}
