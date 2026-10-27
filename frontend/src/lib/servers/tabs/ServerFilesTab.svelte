<script lang="ts">
  // ── Вкладка "Файли" ────────────────────────────────────────────────────
  // FileBrowserPanel — спільний провідник файлів (той самий, що на
  // сторінці збірки), параметризований через FileBrowserAPI. Тут лише
  // збираємо API-обʼєкт з методів App.*ServerFile*/*ServerEntry* —
  // раніше цей самий обʼєкт будувався прямо в ServerInstancePage.
  import { App } from '../../wails'
  import FileBrowserPanel from '../../FileBrowserPanel.svelte'
  import type { FileBrowserAPI } from '../../FileBrowserPanel.svelte'

  let {
    serverId = '',
    serverName = '',
    running = false,
  }: {
    serverId?: string
    serverName?: string
    running?: boolean
  } = $props()

  const api: FileBrowserAPI = {
    list: (rootId, relPath) => App.ListServerFiles(rootId, relPath),
    readText: (rootId, relPath) => App.ReadServerTextFile(rootId, relPath),
    writeText: (rootId, relPath, content) => App.WriteServerTextFile(rootId, relPath, content),
    rename: (rootId, relPath, newName) => App.RenameServerEntry(rootId, relPath, newName),
    createFolder: (rootId, relPath, name) => App.CreateServerFolder(rootId, relPath, name),
    createFile: (rootId, relPath, name) => App.CreateServerFile(rootId, relPath, name),
    delete: (rootId, relPath) => App.DeleteServerEntry(rootId, relPath),
    reveal: (rootId, relPath) => App.RevealServerFile(rootId, relPath),
    getAsset: (rootId, relPath) => App.GetServerFileAsset(rootId, relPath),
  }
</script>

<FileBrowserPanel rootId={serverId} rootName={serverName} {api} serverRunning={running} />
