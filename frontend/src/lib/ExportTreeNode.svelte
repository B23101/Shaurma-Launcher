<script lang="ts">
  // ── Вузол дерева вибору експорту (рекурсивний) ────────────────────────
  // Self-import для рекурсії (Svelte 5 рекомендує замість <svelte:self>).
  // Папки ЗГОРНУТІ за замовчуванням, БЕЗ ВИНЯТКІВ і на будь-якій глибині
  // (див. buildTree у ExportPackModal.svelte) — на великих збірках повністю
  // розгорнуте дерево розтягується на сотні/тисячі рядків і в ньому
  // неможливо орієнтуватись. Клік по шеврону або по назві папки розгортає/
  // згортає її (стан зберігається локально у вузлі, не глобально — кожна
  // папка пам'ятає своє згортання). Чекбокс НЕ розгортає папку: він живе
  // окремо від згортання, щоб можна було обрати всю папку, не розкриваючи
  // її (каскадний вибір працює незалежно від видимості дітей).
  import ExportTreeNode from './ExportTreeNode.svelte'

  export interface ExportTreeNodeData {
    path: string
    type: 'directory' | 'file'
    size?: number
    defaultSelected: boolean
    children: ExportTreeNodeData[]
    state: 'checked' | 'unchecked' | 'indeterminate'
    // Локальний стан згортання папки. Створюється в buildTree як false
    // (згорнуто) для директорій і ніколи не змінюється для файлів.
    collapsed?: boolean
  }

  let {
    node,
    onToggle = (n: ExportTreeNodeData) => {}
  }: {
    node: ExportTreeNodeData
    onToggle?: (n: ExportTreeNodeData) => void
  } = $props()

  // Усі папки, БЕЗ винятків (включно з кореневими mods/resourcepacks/
  // config тощо) і на будь-якій глибині, стартують ЗГОРНУТИМИ — так їх
  // виставляє buildTree(). Для файлів collapsed завжди false — вузол
  // просто не має дітей, тож нічого не впливає.
  const isDir = node.type === 'directory'

  // isCollapsed — похідне ЯВНЕ boolean-значення для рендеру. НЕ читаємо
  // node.collapsed напряму в розмітці: якщо джерело даних колись віддасть
  // undefined замість true (напр. старий кеш/інша точка створення вузла),
  // `!node.collapsed` у шаблоні трактує undefined як falsy → папка
  // виглядає розкритою всупереч дефолту "згорнуто". Тут для директорій
  // undefined явно нормалізуємо в true (згорнуто).
  const isCollapsed = $derived(isDir ? node.collapsed !== false : false)

  function toggleCollapsed() {
    if (!isDir) return
    node.collapsed = !isCollapsed
  }

  function fmtSize(size?: number): string {
    if (!size) return ''
    // Латинські одиниці — компонент працює в обох мовах (uk/en).
    if (size >= 1024 * 1024 * 1024) return (size / (1024 * 1024 * 1024)).toFixed(1) + ' GB'
    if (size >= 1024 * 1024) return (size / (1024 * 1024)).toFixed(1) + ' MB'
    if (size >= 1024) return (size / 1024).toFixed(0) + ' KB'
    return size + ' B'
  }
</script>

<div class="ep-node" class:ep-dir={isDir}>
  <div class="ep-row" class:ep-row-collapsed={isDir && isCollapsed}>
    {#if isDir}
      <button
        class="ep-caret"
        class:open={!isCollapsed}
        aria-label="toggle folder"
        onclick={(e) => { e.stopPropagation(); toggleCollapsed() }}
      >
        <i class="ti ti-chevron-right"></i>
      </button>
    {:else}
      <span class="ep-caret-spacer"></span>
    {/if}
    <label
      class="ep-node-label"
      class:indeterminate={node.state === 'indeterminate'}
      class:ep-label-dir={isDir}
      onclick={(e) => { if (isDir) { e.stopPropagation(); toggleCollapsed() } }}
    >
      <input
        type="checkbox"
        checked={node.state === 'checked'}
        indeterminate={node.state === 'indeterminate'}
        onclick={(e) => {
          // Клік по чекбоксу НЕ має розгортати/згортати папку — чекбокс
          // окрема зона від згортання.
          e.stopPropagation()
          onToggle(node)
        }}
      />
      <span class="ep-node-icon">
        {#if isDir}
          <i class="ti ti-folder"></i>
        {:else}
          <i class="ti ti-file"></i>
        {/if}
      </span>
      <span class="ep-node-name" title={node.path}>{node.path.split('/').pop()}</span>
      {#if node.size}<span class="ep-node-size">{fmtSize(node.size)}</span>{/if}
    </label>
  </div>
  {#if isDir && !isCollapsed && node.children.length > 0}
    <div class="ep-node-children">
      {#each node.children as child}
        <ExportTreeNode node={child} onToggle={onToggle} />
      {/each}
    </div>
  {/if}
</div>

<style>
  .ep-row { display: flex; align-items: center; gap: 2px; }
  .ep-caret {
    width: 20px; height: 24px; flex-shrink: 0;
    background: none; border: none; padding: 0;
    display: flex; align-items: center; justify-content: center;
    color: var(--text-dim); cursor: pointer; border-radius: 5px;
    transition: color .12s, background .12s;
  }
  .ep-caret:hover { color: var(--text); background: var(--hover); }
  .ep-caret i { font-size: 12px; transition: transform .15s; }
  .ep-caret.open i { transform: rotate(90deg); }
  .ep-caret-spacer { width: 20px; flex-shrink: 0; }
  .ep-node-label {
    display: flex; align-items: center; gap: 7px;
    flex: 1; min-width: 0; padding: 3px 6px 3px 2px;
    border-radius: 6px; cursor: pointer;
    transition: background .12s;
  }
  .ep-node-label:hover { background: var(--hover); }
  .ep-label-dir { font-weight: 600; }
  .ep-node-label.indeterminate { color: var(--text); }
  .ep-node-icon { display: flex; align-items: center; color: var(--text-mute); font-size: 14px; flex-shrink: 0; }
  .ep-dir > .ep-row .ep-node-icon { color: var(--orange); }
  .ep-node-name {
    font-size: 12px; color: var(--text); overflow: hidden;
    text-overflow: ellipsis; white-space: nowrap; flex: 1; min-width: 0;
  }
  .ep-node-size { font-size: 10.5px; color: var(--text-dim); font-family: var(--font-mono); flex-shrink: 0; }
  .ep-node-children { margin-left: 16px; border-left: 1px solid var(--hover); padding-left: 6px; }
  input[type='checkbox'] { accent-color: var(--orange); cursor: pointer; flex-shrink: 0; }
</style>
