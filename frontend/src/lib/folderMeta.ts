// folderMeta.ts — спільні константи для папок збірок і серверів
// (вікно перегляду FolderViewModal, редактор FolderEditorModal).

export const FOLDER_COLORS = [
  '#ff8a00', '#f87171', '#f472b6', '#a78bfa',
  '#60a5fa', '#2dd4bf', '#34d399', '#fbbf24',
]

// Tabler-іконки, які можна призначити папці. 'ti-folder' — стандартна.
export const FOLDER_ICONS = [
  'ti-folder', 'ti-star', 'ti-heart', 'ti-flame', 'ti-bolt', 'ti-sword',
  'ti-shield', 'ti-skull', 'ti-ghost', 'ti-rocket', 'ti-planet', 'ti-leaf',
  'ti-diamond', 'ti-trophy', 'ti-flask', 'ti-hammer', 'ti-world', 'ti-moon',
  'ti-cloud', 'ti-device-gamepad-2',
]

/** Дозволяємо лише `ti-…` (захист від довільних класів з чужих даних). */
export function safeFolderIcon(icon?: string): string {
  return icon && /^ti-[a-z0-9-]{1,40}$/.test(icon) ? icon : 'ti-folder'
}
