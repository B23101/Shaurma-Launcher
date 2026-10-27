<script lang="ts">
  // ── Вкладка "Світи" ────────────────────────────────────────────────────
  // Список світів + бекапи вже живе в окремому, самодостатньому
  // ServerWorldsTab.svelte (поза папкою servers/ — спільний з рештою
  // серверного коду файл, який не потребував переписування). Ця обгортка
  // існує лише щоб структура вкладок сторінки сервера була однакова:
  // один файл у tabs/ = одна вкладка в таббарі.
  import ServerWorldsTab from '../../ServerWorldsTab.svelte'

  let { serverId = '', running = false }: { serverId?: string; running?: boolean } = $props()
</script>

<ServerWorldsTab {serverId} {running} />
