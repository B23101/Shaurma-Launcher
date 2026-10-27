<script lang="ts">
  // ── Вкладка "Моди" ─────────────────────────────────────────────────────
  // Тонка обгортка: фіксує kind='mods' на спільній реалізації
  // ServerContentTab (моди й плагіни — той самий список, різні API).
  import ServerContentTab from './ServerContentTab.svelte'

  let {
    serverId = '', running = false, serverDir = '', onSearchOnline = null,
  }: {
    serverId?: string
    running?: boolean
    serverDir?: string
    onSearchOnline?: ((serverId: string, kind: 'mods' | 'plugins') => void) | null
  } = $props()
</script>

<ServerContentTab {serverId} kind="mods" {running} {serverDir} {onSearchOnline} />
