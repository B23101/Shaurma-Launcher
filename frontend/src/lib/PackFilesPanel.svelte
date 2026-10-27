<script lang="ts">
  // ── Провідник файлів збірки: тонка обгортка над FileBrowserPanel ────────
  // Підключає API-методи збірки (App.ListPackFiles тощо) до узагальненого
  // компонента FileBrowserPanel, щоб не чіпати наявні виклики в PackInstancePage.
  import FileBrowserPanel from './FileBrowserPanel.svelte'
  import { App } from './wails'
  import type { FileBrowserAPI } from './FileBrowserPanel.svelte'

  let {
    packId,
    packName = '',
    stickyTop = 0
  }: { packId: string; packName?: string; stickyTop?: number } = $props()

  const packFilesAPI: FileBrowserAPI = {
    list: (rootId, relPath) => App.ListPackFiles(rootId, relPath),
    readText: (rootId, relPath) => App.ReadPackTextFile(rootId, relPath),
    writeText: (rootId, relPath, content) => App.WritePackTextFile(rootId, relPath, content),
    rename: (rootId, relPath, newName) => App.RenamePackEntry(rootId, relPath, newName),
    createFolder: (rootId, relPath, name) => App.CreatePackFolder(rootId, relPath, name),
    createFile: (rootId, relPath, name) => App.CreatePackFile(rootId, relPath, name),
    delete: (rootId, relPath) => App.DeletePackEntry(rootId, relPath),
    reveal: (rootId, relPath) => App.RevealPackFile(rootId, relPath),
    getAsset: (rootId, relPath) => App.GetPackFileAsset(rootId, relPath),
  }
</script>

<FileBrowserPanel rootId={packId} rootName={packName} api={packFilesAPI} {stickyTop} />
