<script lang="ts">
  // «Поділитися» збіркою або локальним сервером: (для збірки) що передавати → вибір до 3 друзів
  // → бекенд готує маніфест і шле друзям картки в чат. Файли їдуть напряму P2P (share_shaurma.go).
  //
  // Візуально — ті самі блоки, що в «Дублюванні збірки»: UiModal + дерево ExportTreeNode (каскадні
  // чекбокси, пошук, лічильник вибраного). Вибір друзів — спільний FriendPickerModal на тому ж z-index,
  // з кнопкою «Назад», лімітом 3 і блокуванням подвійного кліку.
  import { onMount } from 'svelte'
  import UiModal from './UiModal.svelte'
  import ExportTreeNode, { type ExportTreeNodeData } from './ExportTreeNode.svelte'
  import FriendPickerModal from './friends/FriendPickerModal.svelte'
  import { App } from './wails'
  import { toast } from './toast.svelte'
  import { fmtBytes } from './friends/shareMsg'
  import type { SocialFriend, ExportCandidate } from './types'

  let { kind, refId, name, onClose = () => {} }: {
    kind: 'build' | 'server'
    refId: string
    name: string
    onClose?: () => void
  } = $props()

  const MAX_FRIENDS = 3
  // Над сторінкою збірки/сервера та її власними оверлеями.
  const Z = '2100000'

  let step = $state<'files' | 'friends'>(kind === 'build' ? 'files' : 'friends')
  let candidates = $state<ExportCandidate[]>([])
  let tree = $state<ExportTreeNodeData[]>([])
  let included = $state<string[]>([])
  let excluded = $state<string[]>([])
  let filter = $state('')
  let loading = $state(kind === 'build')
  let loadErr = $state('')
  let starting = $state(false)
  let alive = true
  let filterTimer: ReturnType<typeof setTimeout> | null = null

  onMount(() => {
    if (kind === 'build') load()
    return () => { alive = false; if (filterTimer) clearTimeout(filterTimer) }
  })

  async function load() {
    loading = true
    loadErr = ''
    try {
      const cands = (await App.GetDuplicatePackTree(refId)) ?? []
      if (!alive) return
      candidates = cands
      // Типовий вибір — як в експорті (mods, config…); якщо нічого не позначено — усе.
      included = cands.filter(c => c.defaultSelected).map(c => c.path)
      if (included.length === 0) included = cands.map(c => c.path)
      excluded = []
      buildTree()
      syncChecks()
    } catch (e: any) {
      if (alive) loadErr = String(e?.message ?? e)
    } finally {
      if (alive) loading = false
    }
  }

  // ── Дерево (той самий механізм, що в «Дублюванні збірки») ──
  function buildTree() {
    const index = new Map<string, ExportTreeNodeData>()
    const root: ExportTreeNodeData[] = []
    const q = filter.trim().toLowerCase()
    for (const c of candidates) {
      if (q && !c.path.toLowerCase().includes(q)) continue
      const parts = c.path.split('/')
      let level = root
      let cur = ''
      for (let i = 0; i < parts.length; i++) {
        cur = cur ? cur + '/' + parts[i] : parts[i]
        let node = index.get(cur)
        if (!node) {
          const isLeaf = i === parts.length - 1
          const type = isLeaf ? c.type : 'directory'
          node = {
            path: cur, type,
            size: isLeaf ? c.size : undefined,
            defaultSelected: isLeaf ? c.defaultSelected : false,
            children: [], state: 'unchecked', collapsed: type === 'directory',
          }
          index.set(cur, node)
          level.push(node)
        }
        level = node.children
      }
    }
    tree = root
  }

  function syncChecks() {
    const inc = new Set(included)
    const exc = new Set(excluded)
    const walk = (nodes: ExportTreeNodeData[]) => {
      for (const n of nodes) {
        walk(n.children)
        if (n.children.length === 0) {
          n.state = !exc.has(n.path) && inc.has(n.path) ? 'checked' : 'unchecked'
        } else if (n.children.every(c => c.state === 'checked')) n.state = 'checked'
        else if (n.children.every(c => c.state === 'unchecked')) n.state = inc.has(n.path) ? 'checked' : 'unchecked'
        else n.state = 'indeterminate'
      }
    }
    walk(tree)
  }

  function collect(n: ExportTreeNodeData, out: string[] = []): string[] {
    out.push(n.path)
    for (const c of n.children) collect(c, out)
    return out
  }

  function toggleNode(n: ExportTreeNodeData) {
    const on = n.state !== 'checked'
    const paths = collect(n)
    const set = new Set(paths)
    if (on) {
      included = [...new Set([...included, ...paths])]
      excluded = excluded.filter(p => !set.has(p))
    } else {
      excluded = [...new Set([...excluded, ...paths])]
      included = included.filter(p => !set.has(p))
    }
    syncChecks()
  }
  function selectAll() { included = candidates.map(c => c.path); excluded = []; syncChecks() }
  function clearAll() { included = []; excluded = candidates.map(c => c.path); syncChecks() }
  function onFilterInput() {
    if (filterTimer) clearTimeout(filterTimer)
    filterTimer = setTimeout(() => { buildTree(); syncChecks() }, 120)
  }

  // Лічильники: рахуємо лише файли (і порожні/термінальні папки), щоб папка не дублювала вміст.
  const summary = $derived.by(() => {
    const inc = new Set(included)
    const parents = new Set<string>()
    for (const c of candidates) {
      const i = c.path.lastIndexOf('/')
      if (i > 0) parents.add(c.path.slice(0, i))
    }
    let files = 0, bytes = 0
    for (const c of candidates) {
      if (!inc.has(c.path)) continue
      if (c.type === 'directory' && parents.has(c.path)) continue
      files++
      bytes += c.size || 0
    }
    return { files, bytes }
  })
  const canNext = $derived(!loading && !loadErr && included.length > 0)

  async function start(friends: SocialFriend[]) {
    if (starting) return
    if (friends.length === 0 || friends.length > MAX_FRIENDS) {
      toast(`Оберіть від 1 до ${MAX_FRIENDS} друзів`, 'error')
      return
    }
    starting = true
    try {
      await App.ShareStart({
        kind, refId,
        friendIds: friends.map(f => f.accountId),
        included: kind === 'build' ? included : [],
        excluded: kind === 'build' ? excluded : [],
      })
      toast('Готуємо передачу — картки надійдуть друзям у чат', 'success')
      onClose()
    } catch (e: any) {
      // Вибір друзів лишається — можна виправити причину й натиснути ще раз.
      toast(String(e?.message ?? e), 'error')
      starting = false
    }
  }
</script>

{#if step === 'files'}
  <UiModal
    open={true}
    zIndex={Z}
    width="min(560px, 94vw)"
    maxHeight="calc(100vh - 48px)"
    icon="ti-share-3"
    title="Поділитися збіркою"
    subtitle={`«${name}» — що передати другові. Логи й крашдампи не передаються ніколи.`}
    onclose={onClose}
  >
    <div class="sm-wrap">
      <div class="sm-bar">
        <div class="sm-search">
          <i class="ti ti-search"></i>
          <input type="text" bind:value={filter} oninput={onFilterInput} placeholder="Пошук файлів…" spellcheck="false" />
          {#if filter}
            <button class="sm-clear" aria-label="Очистити" onclick={() => { filter = ''; buildTree(); syncChecks() }}><i class="ti ti-x"></i></button>
          {/if}
        </div>
        <button class="sm-mini" onclick={selectAll} disabled={loading || !!loadErr}><i class="ti ti-select-all"></i> Усі</button>
        <button class="sm-mini" onclick={clearAll} disabled={loading || !!loadErr}><i class="ti ti-square-x"></i> Жодного</button>
      </div>

      <div class="sm-tree">
        {#if loading}
          <div class="sm-state"><i class="ti ti-loader-2 spin"></i><span>Читаю вміст збірки…</span></div>
        {:else if loadErr}
          <div class="sm-state bad">
            <i class="ti ti-alert-triangle"></i><span>{loadErr}</span>
            <button class="sm-mini" onclick={load}><i class="ti ti-refresh"></i> Спробувати знову</button>
          </div>
        {:else if tree.length === 0}
          <div class="sm-state">
            {#if filter}<i class="ti ti-search-off"></i><span>Нічого не знайдено за запитом</span>
            {:else}<i class="ti ti-package-off"></i><span>У збірці немає файлів</span>{/if}
          </div>
        {:else}
          {#each tree as node (node.path)}
            <ExportTreeNode {node} onToggle={toggleNode} />
          {/each}
        {/if}
      </div>

      <div class="sm-sum">
        <span><i class="ti ti-file-check"></i> Вибрано файлів: {summary.files}</span>
        <span class="sm-size">{summary.bytes > 0 ? fmtBytes(summary.bytes) : ''}</span>
      </div>
    </div>

    {#snippet footer()}
      <button class="btn btn-ghost" onclick={onClose}>Скасувати</button>
      <button class="sm-cta" disabled={!canNext} onclick={() => step = 'friends'}>
        Далі <i class="ti ti-arrow-right"></i>
      </button>
    {/snippet}
  </UiModal>
{:else}
  <FriendPickerModal
    title={kind === 'build' ? `Надіслати збірку «${name}»` : `Надіслати сервер «${name}»`}
    subtitle={`До ${MAX_FRIENDS} друзів одночасно. Друг сам прийме передачу в чаті.`}
    multi={true}
    max={MAX_FRIENDS}
    busy={starting}
    zIndex={Number(Z)}
    confirmLabel="Надіслати"
    onback={kind === 'build' ? () => { if (!starting) step = 'files' } : null}
    onconfirm={start}
    onclose={() => { if (!starting) onClose() }}
  />
{/if}

<style>
  .sm-wrap { display: flex; flex-direction: column; gap: 10px; min-height: 0; }
  .sm-bar { display: flex; align-items: center; gap: 6px; }
  .sm-search { position: relative; flex: 1; min-width: 0; display: flex; align-items: center; }
  .sm-search > .ti-search { position: absolute; left: 10px; font-size: 13px; color: var(--text-mute); pointer-events: none; }
  .sm-search input {
    width: 100%; background: var(--bg-input); border: 1px solid var(--border); border-radius: 9px;
    padding: 7px 30px 7px 31px; color: var(--text); font-family: inherit; font-size: 12.5px;
    transition: border-color .15s, box-shadow .15s;
  }
  .sm-search input:focus { border-color: var(--orange); outline: none; box-shadow: 0 0 0 3px color-mix(in srgb, var(--orange) 16%, transparent); }
  .sm-search input::placeholder { color: var(--text-mute); }
  .sm-clear {
    position: absolute; right: 4px; width: 22px; height: 22px; border: 0; border-radius: 6px; background: transparent;
    color: var(--text-dim); cursor: pointer; display: grid; place-items: center; font-size: 12px;
  }
  .sm-clear:hover { background: var(--bg-input-hi); color: var(--text); }
  .sm-mini {
    display: inline-flex; align-items: center; gap: 4px; flex: none;
    border: 1px solid var(--border); background: var(--bg-input); color: var(--text-dim);
    font: 600 11px var(--font-ui); border-radius: 8px; padding: 7px 9px; cursor: pointer; transition: all .14s;
  }
  .sm-mini:hover:not(:disabled) { color: var(--orange); border-color: var(--orange); }
  .sm-mini:disabled { opacity: .5; cursor: default; }

  .sm-tree {
    height: min(360px, 50vh); overflow-y: auto; overscroll-behavior: contain;
    background: var(--bg-input); border: 1px solid var(--border); border-radius: 12px; padding: 6px;
  }
  .sm-tree::-webkit-scrollbar { width: 6px; }
  .sm-tree::-webkit-scrollbar-thumb { background: var(--hover-strong); border-radius: 3px; }
  .sm-state {
    height: 100%; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 8px;
    padding: 16px; text-align: center; color: var(--text-mute); font-size: 12.5px;
  }
  .sm-state > .ti { font-size: 24px; }
  .sm-state.bad { color: var(--red); }

  .sm-sum { display: flex; align-items: center; justify-content: space-between; gap: 8px; font-size: 11.5px; font-weight: 700; color: var(--text-dim); }
  .sm-sum .ti { color: var(--green); margin-right: 4px; }
  .sm-size { font-weight: 600; color: var(--text-mute); }

  .sm-cta {
    display: inline-flex; align-items: center; gap: 7px; border: 0; border-radius: 10px; padding: 9px 18px;
    font: 800 13px var(--font-ui); color: #fff; cursor: pointer;
    background: linear-gradient(135deg, var(--orange), color-mix(in srgb, var(--orange) 55%, #ff4d6d));
    box-shadow: 0 4px 16px color-mix(in srgb, var(--orange) 35%, transparent);
    transition: transform .14s, filter .14s, box-shadow .14s;
  }
  .sm-cta:hover:not(:disabled) { transform: translateY(-1px); filter: brightness(1.06); }
  .sm-cta:disabled { opacity: .45; cursor: not-allowed; box-shadow: none; }
</style>
