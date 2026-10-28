<script lang="ts">
  // ── Вкладки "Моди" / "Плагіни" сторінки сервера ────────────────────────
  // Не окрема реалізація, а той самий менеджер модів, що й у збірки
  // (PackModsSection): сервер для нього — таргет "srv:<id>" / "srvp:<id>"
  // (modTarget.ts, backend/mod_target.go). Тому всі модалки й функції
  // (оновлення з резервною копією, версії, дерево залежностей, джерела,
  // експорт, порівняння, міграція, портування зі збірки) у збірок і
  // серверів спільні: виправлення в одному місці діє для обох.
  import { App } from '../../wails'
  import PackModsSection from '../../PackModsSection.svelte'
  import { serverModTarget, type ServerContentKind } from '../../modTarget'
  import { serverVisual } from '../serverTargets'
  import type { ServerView } from '../../types'

  let {
    srv,
    kind = 'mods',
    running = false,
    onSearchOnline = null,
  }: {
    srv: ServerView
    kind?: ServerContentKind
    running?: boolean
    // Відкрити «Браузер контенту» з фільтром на цей сервер (App.svelte).
    onSearchOnline?: ((serverId: string, kind: ServerContentKind) => void) | null
  } = $props()

  // Лоадер, під який шукаються версії (paper-сумісні плагіни мають інший
  // лоадер, ніж id ядра) — з бекенду, той самий мапінг, що й «Знайти онлайн».
  // null — ще не відомо: менеджер монтується одразу з лоадером, без
  // зайвого другого сканування.
  // Залежність лише від ID і виду: об'єкт srv сторінка оновлює при кожній
  // зміні статусу, і менеджер не має від цього перемонтовуватись.
  const serverId = $derived(srv.id)
  let loader = $state<string | null>(null)
  $effect(() => {
    const id = serverId
    const k = kind
    loader = null
    App.ServerBrowserContext(id)
      .then(ctx => { if (id === serverId && k === kind) loader = (k === 'plugins' ? ctx?.pluginLoader : ctx?.modLoader) || '' })
      .catch(() => { if (id === serverId && k === kind) loader = '' })
  })

  const visual = $derived(serverVisual(srv))
</script>

{#key `${serverId}:${kind}`}
  {#if loader !== null}
  <PackModsSection
    packId={serverModTarget(serverId, kind)}
    packName={srv.name}
    mcVersion={srv.mcVersion}
    {loader}
    iconUrl={visual.src ?? ''}
    icon={visual.ti}
    color={visual.color}
    {running}
    onSearchMods={onSearchOnline ? () => onSearchOnline?.(serverId, kind) : null}
  />
  {/if}
{/key}
