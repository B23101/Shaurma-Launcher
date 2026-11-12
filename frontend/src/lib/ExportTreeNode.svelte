<script lang="ts">
  import { tip as launcherTip } from './friends/tooltip'

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
    // Необов'язково: вузол завжди вибраний і не знімається (напр. файли, без яких сервер
    // не запуститься); lockedTip — підказка, чому.
    locked?: boolean
    lockedTip?: string
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

  // Іконка й колір за типом теки/файла — дерево читається з першого погляду.
  const name = $derived(node.path.split('/').pop() ?? '')
  const DIRS: Record<string, [string, string]> = {
    mods: ['ti-puzzle', 'var(--green)'], config: ['ti-settings', 'var(--blue)'], defaultconfigs: ['ti-settings', 'var(--blue)'],
    resourcepacks: ['ti-photo', 'var(--violet-l)'], texturepacks: ['ti-photo', 'var(--violet-l)'], shaderpacks: ['ti-sun', '#ff6b9d'],
    saves: ['ti-world', 'var(--green)'], screenshots: ['ti-camera', 'var(--yellow)'], datapacks: ['ti-database', 'var(--blue)'],
    kubejs: ['ti-code', 'var(--yellow)'], scripts: ['ti-code', 'var(--yellow)'],
  }
  const FILES: [RegExp, string][] = [
    [/\.(png|jpe?g|gif|webp)$/i, 'ti-photo'], [/\.(jar|zip)$/i, 'ti-file-zip'], [/\.(json|toml|ya?ml|cfg|properties|txt|ini)$/i, 'ti-file-text'],
    [/\.(dat|dat_old|nbt)$/i, 'ti-file-database'], [/\.(log)$/i, 'ti-file-description'],
  ]
  const look = $derived.by<[string, string]>(() => {
    if (isDir) {
      const d = DIRS[name.toLowerCase()]
      return d ? [d[0], d[1]] : [isCollapsed ? 'ti-folder' : 'ti-folder-open', 'var(--orange)']
    }
    return [FILES.find(([re]) => re.test(name))?.[1] ?? 'ti-file', 'var(--text-dim)']
  })

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
  <div class="ep-row" class:ep-row-collapsed={isDir && isCollapsed} class:sel={node.state !== 'unchecked'}>
    {#if isDir}
      <button
        class="ep-caret"
        class:open={!isCollapsed}
        aria-label={name}
        aria-expanded={!isCollapsed}
        onclick={(e) => { e.stopPropagation(); toggleCollapsed() }}
      >
        <i class="ti ti-chevron-right"></i>
      </button>
    {:else}
      <span class="ep-caret-spacer"></span>
    {/if}
    <!-- Чекбокс — окрема зона: лише він міняє вибір. Справжній input лишається
         для клавіатури й читачів екрана, малюється власна «галочка». -->
    <label class="ep-box-wrap" class:locked={node.locked}>
      <input
        class="ep-native"
        type="checkbox"
        checked={node.state === 'checked'}
        indeterminate={node.state === 'indeterminate'}
        disabled={node.locked}
        onclick={(e) => {
          e.stopPropagation()
          onToggle(node)
          // Після кліку мишею прибираємо фокус, щоб не лишалось помаранчевої
          // рамки (detail === 0 — це клавіатура, там фокус потрібен).
          if ((e as MouseEvent).detail > 0) (e.currentTarget as HTMLInputElement).blur()
        }}
      />
      <span class="ep-box" class:on={node.state === 'checked'} class:mid={node.state === 'indeterminate'} aria-hidden="true">
        <i class="ti {node.state === 'indeterminate' ? 'ti-minus' : 'ti-check'}"></i>
      </span>
    </label>
    <!-- Решта рядка: для папки — лише розгортає/згортає (вибір НЕ чіпає),
         для файлу — перемикає його вибір. -->
    <div
      class="ep-node-main"
      class:ep-label-dir={isDir}
      class:locked={node.locked && !isDir}
      role="presentation"
      onclick={() => { if (isDir) toggleCollapsed(); else if (!node.locked) onToggle(node) }}
    >
      <span class="ep-node-icon" style="--ic:{look[1]}"><i class="ti {look[0]}"></i></span>
      <span class="ep-node-name" use:launcherTip={node.path}>{name}</span>
      {#if isDir && node.children.length > 0}<span class="ep-node-count">{node.children.length}</span>{/if}
      {#if node.locked}<span class="ep-node-lock" use:launcherTip={node.lockedTip ?? ''}><i class="ti ti-lock"></i></span>{/if}
      {#if node.size}<span class="ep-node-size">{fmtSize(node.size)}</span>{/if}
    </div>
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
  /* Рядок дерева: власний чекбокс, кольорові іконки за типом, м'яка
     підсвітка вибраного. Усе локально — глобальні .ep-* лише для модалки. */
  .ep-row { display: flex; align-items: center; gap: 2px; border-radius: 9px; transition: background .14s; }
  .ep-row:hover { background: var(--hover); }
  .ep-row.sel { background: color-mix(in srgb, var(--orange) 5%, transparent); }
  .ep-row.sel:hover { background: color-mix(in srgb, var(--orange) 9%, transparent); }
  .ep-caret {
    width: 22px; height: 30px; flex-shrink: 0;
    background: none; border: none; padding: 0;
    display: flex; align-items: center; justify-content: center;
    color: var(--text-dim); cursor: pointer; border-radius: 6px;
    transition: color .12s;
  }
  .ep-caret:hover { color: var(--text); }
  .ep-caret i { font-size: 13px; transition: transform .22s cubic-bezier(.34,1.56,.64,1); }
  .ep-caret.open i { transform: rotate(90deg); color: var(--orange); }
  .ep-caret-spacer { width: 22px; flex-shrink: 0; }
  .ep-box-wrap { position: relative; flex: none; display: flex; align-items: center; justify-content: center; width: 26px; height: 30px; cursor: pointer; }
  .ep-box-wrap.locked { cursor: default; }
  .ep-node-main {
    display: flex; align-items: center; gap: 9px;
    flex: 1; min-width: 0; min-height: 30px; padding: 0 8px 0 2px;
    border-radius: 9px; cursor: pointer; font-size: 12.5px; color: var(--text);
  }
  .ep-node-main.locked { cursor: default; }
  .ep-label-dir .ep-node-name { font-weight: 650; }
  .ep-native { position: absolute; opacity: 0; inset: 0; width: 100%; height: 100%; margin: 0; cursor: pointer; z-index: 1; }
  .ep-native:disabled { cursor: default; }
  .ep-box {
    flex: none; width: 18px; height: 18px; border-radius: 6px; display: grid; place-items: center;
    border: 1.5px solid var(--border-hi); background: var(--bg-panel); color: transparent; font-size: 12px;
    transition: background .15s, border-color .15s, color .15s, transform .2s cubic-bezier(.34,1.56,.64,1);
  }
  .ep-box.on, .ep-box.mid { background: var(--orange); border-color: var(--orange); color: #fff; box-shadow: 0 2px 7px rgba(var(--orange-rgb), .35); }
  .ep-box.mid { background: color-mix(in srgb, var(--orange) 55%, var(--bg-panel)); }
  .ep-box-wrap:hover .ep-box:not(.on):not(.mid) { border-color: var(--orange); }
  .ep-native:active + .ep-box { transform: scale(.82); }
  .ep-native:focus-visible + .ep-box { outline: 2px solid var(--orange); outline-offset: 2px; }
  .locked .ep-box { opacity: .55; }
  .ep-node-icon {
    flex: none; width: 24px; height: 24px; border-radius: 7px; display: grid; place-items: center;
    font-size: 14px; color: var(--ic); background: color-mix(in srgb, var(--ic) 13%, transparent);
  }
  .ep-node-name { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .ep-node-count { flex: none; padding: 2px 6px; border-radius: 999px; background: var(--hover-strong); color: var(--text-mute); font: 700 10px/1 var(--font-mono); }
  .ep-node-lock { display: flex; align-items: center; color: var(--text-mute); font-size: 12px; flex-shrink: 0; }
  .ep-node-size { flex: none; font: 600 var(--fs-min)/1 var(--font-mono); color: var(--text-dim); }
  .ep-node-children {
    margin-left: 22px; padding-left: 8px; border-left: 1px dashed var(--border-hi);
    animation: ep-open .22s cubic-bezier(.16,1,.3,1);
  }
  @keyframes ep-open { from { opacity: 0; transform: translateY(-4px); } to { opacity: 1; transform: none; } }
</style>
