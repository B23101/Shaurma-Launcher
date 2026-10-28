// ── Таргет менеджера модів: збірка або сервер ───────────────────────────
// Один і той самий менеджер модів (PackModsSection і всі його модалки)
// працює і зі збірками, і з серверами: бекенд приймає ID таргету, де
// звичайний ID — збірка, "srv:<id>" — mods/ сервера, "srvp:<id>" — його
// plugins/ (backend/mod_target.go).
import type { MigrationPack } from './types'
import type { PackPickerItem, PackRow, PackFolderRow } from './PackPicker.svelte'

export type ServerContentKind = 'mods' | 'plugins'

export function serverModTarget(serverId: string, kind: ServerContentKind = 'mods'): string {
  return (kind === 'plugins' ? 'srvp:' : 'srv:') + serverId
}

export function isServerModTarget(id: string): boolean {
  return id.startsWith('srv:') || id.startsWith('srvp:')
}

export function isPluginModTarget(id: string): boolean {
  return id.startsWith('srvp:')
}

export interface ModTargetPicker {
  items: PackPickerItem[]
  rowById: Map<string, PackRow>
  folderById: Map<string, PackFolderRow>
}

// withMigrationTargets — список для вибору «з чим порівняти»: збірки з
// папками (base, якщо є) плюс таргети з GetMigrationPacks, яких у base
// немає (сервери, а на сторінці сервера — і збірки).
export function withMigrationTargets(base: ModTargetPicker | null, targets: MigrationPack[]): ModTargetPicker {
  const items = [...(base?.items ?? [])]
  const rowById = new Map(base?.rowById ?? [])
  for (const p of targets) {
    if (rowById.has(p.packId)) continue
    rowById.set(p.packId, {
      name: p.name, color: p.color, iconUrl: p.iconUrl, icon: p.icon,
      loader: p.loader, mcVersion: p.mcVersion,
    })
    items.push({ kind: 'pack', id: p.packId })
  }
  return { items, rowById, folderById: base?.folderById ?? new Map() }
}
