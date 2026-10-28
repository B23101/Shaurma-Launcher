<script lang="ts">
  import UiModal from './UiModal.svelte'
  // ── Дублювання збірки (повністю переписано) ─────────────────────────────
  // Сучасна модалка копії збірки: дві панелі — дерево вибору файлів (як в
  // експорті, каскадні чекбокси + пошук + лічильник вибраного) і параметри
  // (назва, ID з живою перевіркою, іконка/колір, копіювання налаштувань).
  // Прогрес — живий оверлей (duplicate:progress).
  //
  // ВАЖЛИВО про ширину: глобальний .modal має max-width:480px — без явного
  // перевизначення двоколонний лейаут стискався у вузьку смугу («криве»
  // вікно). Тут ширину задаємо явно (див. .dup-modal у стилях).
  import { onMount, onDestroy } from 'svelte'
  import { t } from './i18n.svelte'
  import { App, on } from './wails'
  import { toast } from './toast.svelte'
  import ExportTreeNode, { type ExportTreeNodeData } from './ExportTreeNode.svelte'
  import PresetIconPicker from './PresetIconPicker.svelte'
  import { packAsset } from './packAssets.svelte'
  import type { ExportCandidate, DuplicatePackResult, DuplicateProgress } from './types'

  let {
    packId = '',
    packName = '',
    packIconUrl = '',
    packIcon = '',
    packColor = '#ff8a00',
    onClose = () => {},
    onDone = (_packId: string) => {},
  }: {
    packId?: string
    packName?: string
    // Іконка/колір джерела — для прев'ю «дублювати іконку» і як дефолти.
    packIconUrl?: string
    packIcon?: string
    packColor?: string
    onClose?: () => void
    onDone?: (newPackId: string) => void
  } = $props()

  // Пресет-іконки — у спільному компоненті PresetIconPicker.
  const colorPresets = ['#ff8a00', '#7b4dff', '#10b981', '#3b82f6', '#ef4444']

  // ── Дерево вибору ──
  let candidates = $state<ExportCandidate[]>([])
  let loadingCandidates = $state(true)
  let included = $state<string[]>([])
  let excluded = $state<string[]>([])
  let tree = $state<ExportTreeNodeData[]>([])
  // Пошук по дереву: фільтруємо ЛИШЕ ВИД (included/excluded не чіпаємо).
  let treeFilter = $state('')
  let filterTimer: ReturnType<typeof setTimeout> | null = null

  // ── Назва / ID ──
  let name = $state('')
  let id = $state('')
  let idTouched = $state(false)
  let idCheck = $state<{ id: string; exists: boolean; suggested: string } | null>(null)
  let idChecking = $state(false)
  let idTimer: ReturnType<typeof setTimeout> | null = null

  // ── Іконка / колір / налаштування ──
  let copyIcon = $state(true)
  let iconName = $state(packIcon || 'ti-puzzle')

  // Реальна іконка джерела через packAsset (як у sidebar): локальні
  // іконки (local-file://) і CDN-URL конвертуються у data-URL бекендом —
  // інакше <img> показував би биту картинку для локальних збірок.
  const effectiveIcon = $derived(packAsset(packIconUrl) ?? undefined)
  let color = $state(packColor || '#ff8a00')
  let iconSrcPath = $state('')
  let copySettings = $state(true)

  // ── Виконання ──
  let duplicating = $state(false)
  let progress = $state<DuplicateProgress | null>(null)
  let recentFiles = $state<string[]>([])
  let progressUnsub: (() => void) | null = null
  // selectionRestored — true лише ПІСЛЯ restoreSelection() у onMount:
  // guard для $effect нижче, окремий від loadingCandidates (та керує
  // UI-спінером дерева, не готовністю саме copyIcon/color/copySettings).
  let selectionRestored = $state(false)

  onMount(async () => {
    // Дефолтна назва/ID: "<Назва> (2)" — як у Modrinth App для копій.
    name = `${packName} (2)`
    id = slugify(`${packName} 2`)
    idTouched = false
    checkID()
    try {
      candidates = (await App.GetDuplicatePackTree(packId)) ?? []
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      loadingCandidates = false
      buildTree()
      await restoreSelection()
      selectionRestored = true
    }
  })

  type SavedDuplicateSelection = { included?: string[]; excluded?: string[]; copyIcon?: boolean; color?: string; copySettings?: boolean }

  // restoreSelection — параметри дублювання зберігаються ОКРЕМО для
  // КОЖНОЇ збірки-джерела (packId тут — та збірка, з чиєї картки
  // відкрили «Дублювати», не нова копія): duplicate_settings у SQLite,
  // ключ pack_id. Раніше цей вибір НІКОЛИ не запам'ятовувався — щоразу
  // дефолтний набір файлів, навіть для тієї самої збірки. name/id/
  // iconSrcPath СВІДОМО не відновлюються: "<Назва> (2)" — завжди
  // актуальний дефолт, збережений id майже напевно вже зайнятий
  // попередньою копією (checkID() однаково перевіряє наново), а
  // iconSrcPath — шлях до тимчасового файлу вибору, який міг зникнути
  // між сесіями лаунчера.
  async function restoreSelection() {
    let saved: SavedDuplicateSelection | null = null
    try {
      const raw = await App.GetDuplicateSettings(packId)
      if (raw) saved = JSON.parse(raw)
    } catch { /* ignore — падаємо на дефолтний вибір нижче */ }

    if (saved?.included?.length) {
      included = [...saved.included]
    } else {
      // Дефолтний вибір — як в експорті: mods/datapacks/resourcepacks/
      // shaderpacks/config. Для копії збірки цього достатньо, решту
      // (saves, config тощо) користувач додає вручну.
      included = candidates.filter(c => c.defaultSelected).map(c => c.path)
      if (included.length === 0) included = candidates.map(c => c.path)
    }
    if (saved?.excluded) excluded = [...saved.excluded]
    if (typeof saved?.copyIcon === 'boolean') copyIcon = saved.copyIcon
    if (saved?.color) color = saved.color
    if (typeof saved?.copySettings === 'boolean') copySettings = saved.copySettings
    syncChecks()
  }

  // persistSelection — викликається після кожної зміни вибору (той самий
  // момент, що syncChecks нижче), не лише при фактичному дублюванні.
  // App.SaveDuplicateSettings навмисно НЕ await-иться — не блокуємо клік
  // по чекбоксу очікуванням Wails IPC round-trip.
  function persistSelection() {
    const payload = JSON.stringify({ included, excluded, copyIcon, color, copySettings })
    App.SaveDuplicateSettings(packId, payload).catch(() => { /* ignore */ })
  }

  $effect(() => {
    // copyIcon/color/copySettings змінюються через bind:/onclick у
    // кількох місцях розмітки нижче — замість дублювання persistSelection()
    // у кожному обробнику, $effect реагує на будь-яку зміну одразу.
    // selectionRestored guard: без нього перший рендер (ще до
    // restoreSelection у onMount) уже спрацював би й затер щойно
    // завантажені saved-значення дефолтними ще до того, як вони встигли
    // застосуватись.
    void copyIcon; void color; void copySettings
    if (selectionRestored) persistSelection()
  })

  function onTreeFilterInput() {
    if (filterTimer) clearTimeout(filterTimer)
    filterTimer = setTimeout(() => buildTree(), 120)
  }

  onDestroy(() => {
    progressUnsub?.()
    if (idTimer) clearTimeout(idTimer)
    if (filterTimer) clearTimeout(filterTimer)
  })

  function slugify(s: string): string {
    return s.trim().toLowerCase().replace(/[^a-z0-9а-яіїєґА-ЯІЇЄҐ]+/g, '-').replace(/^-+|-+$/g, '') || 'pack'
  }

  function fmtBytes(n?: number): string {
    if (!n) return ''
    if (n >= 1024 ** 3) return (n / 1024 ** 3).toFixed(1) + ' GB'
    if (n >= 1024 ** 2) return (n / 1024 ** 2).toFixed(1) + ' MB'
    if (n >= 1024) return (n / 1024).toFixed(0) + ' KB'
    return n + ' B'
  }

  // ── Жива перевірка ID (debounce 350ms) ──
  function checkID() {
    if (idTimer) clearTimeout(idTimer)
    idTimer = setTimeout(async () => {
      const cand = id.trim()
      if (!cand) { idCheck = null; return }
      idChecking = true
      try {
        const r = await App.CheckDuplicatePackID(cand)
        idCheck = r
      } catch {
        idCheck = null
      } finally {
        idChecking = false
      }
    }, 350)
  }

  function onNameInput() {
    if (!idTouched) id = slugify(name)
    checkID()
  }

  function onIDInput() {
    idTouched = true
    checkID()
  }

  function useSuggested() {
    if (!idCheck?.suggested) return
    id = idCheck.suggested
    idTouched = true
    checkID()
  }

  // ── Дерево (той самий механізм, що в ExportPackModal) ──
  function buildTree() {
    const index = new Map<string, ExportTreeNodeData>()
    const root: ExportTreeNodeData[] = []
    // Пошук: лишаємо лише кандидатів, чий шлях містить запит. Папки
    // будуються з відфільтрованих шляхів — порожніх гілок не буде.
    const q = treeFilter.trim().toLowerCase()
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
            path: cur,
            type,
            size: isLeaf ? c.size : undefined,
            defaultSelected: isLeaf ? c.defaultSelected : false,
            children: [],
            state: 'unchecked',
            collapsed: type === 'directory',
          }
          index.set(cur, node)
          level.push(node)
        } else if (node.type === 'directory') {
          node.collapsed = true
        }
        level = node.children
      }
    }
    tree = root
  }

  function nodeState(n: ExportTreeNodeData, includedSet: Set<string>, excludedSet: Set<string>): 'checked' | 'unchecked' | 'indeterminate' {
    if (n.type === 'file' || n.children.length === 0) {
      if (excludedSet.has(n.path)) return 'unchecked'
      return includedSet.has(n.path) ? 'checked' : 'unchecked'
    }
    const childStates = n.children.map(c => c.state)
    if (childStates.length === 0) return includedSet.has(n.path) ? 'checked' : 'unchecked'
    if (childStates.every(s => s === 'checked')) return 'checked'
    if (childStates.every(s => s === 'unchecked')) {
      return includedSet.has(n.path) ? 'checked' : 'unchecked'
    }
    return 'indeterminate'
  }

  // syncChecks — ОДИН прохід дерева знизу вгору (post-order), Set замість
  // Array.includes() для included/excluded. Той самий фікс, що й у
  // ExportPackModal.svelte (ідентичний код був скопійований звідти) —
  // раніше nodeState рекурсивно перераховувала ціле піддерево на КОЖЕН
  // виклик, а syncChecks викликала її ще раз на кожному вузлі під час
  // власного обходу: O(вузли × глибина) замість O(вузли), плюс
  // Array.includes() в кожному виклику додавав O(вузли × елементи). На
  // великих збірках (тисячі файлів у mods/config/resourcepacks) це й
  // було причиною лагу на кожен клік по чекбоксу дерева дублювання.
  function syncChecks() {
    const includedSet = new Set(included)
    const excludedSet = new Set(excluded)
    const walk = (nodes: ExportTreeNodeData[]) => {
      for (const n of nodes) {
        walk(n.children)
        n.state = nodeState(n, includedSet, excludedSet)
      }
    }
    walk(tree)
  }

  function collectPaths(n: ExportTreeNodeData): string[] {
    const out: string[] = []
    const walk = (node: ExportTreeNodeData) => {
      out.push(node.path)
      for (const c of node.children) walk(c)
    }
    walk(n)
    return out
  }

  function toggleNode(n: ExportTreeNodeData) {
    const nowChecked = n.state !== 'checked'
    const paths = collectPaths(n)
    const pathSet = new Set(paths)
    if (nowChecked) {
      included = [...new Set([...included, ...paths])]
      excluded = excluded.filter(p => !pathSet.has(p))
    } else {
      excluded = [...new Set([...excluded, ...paths])]
      included = included.filter(p => !pathSet.has(p))
    }
    syncChecks()
    persistSelection()
  }

  function selectAll() {
    included = candidates.map(c => c.path)
    excluded = []
    syncChecks()
    persistSelection()
  }

  function clearAll() {
    included = []
    excluded = candidates.map(c => c.path)
    syncChecks()
    persistSelection()
  }

  const selectedCount = $derived(included.length)
  const selectedSize = $derived.by(() => {
    // Set замість candidates.filter(c => included.includes(c.path)) —
    // те саме O(n×m), тепер O(n). candidates і included обидва можуть
    // бути тисячами елементів на великих збірках.
    const includedSet = new Set(included)
    let total = 0
    for (const c of candidates) {
      if (includedSet.has(c.path)) total += c.size || 0
    }
    return total
  })
  const canDuplicate = $derived(
    !duplicating &&
    name.trim() !== '' &&
    included.length > 0 &&
    id.trim() !== '' &&
    !!idCheck && !idCheck.exists,
  )

  async function browseIcon() {
    const p = await App.BrowseImageFile()
    if (p) iconSrcPath = p
  }

  // ── Виконання ──
  async function startDuplicate() {
    if (duplicating) return
    if (name.trim() === '') { toast(t('duplicate.needName'), 'error'); return }
    if (included.length === 0) { toast(t('duplicate.needFiles'), 'error'); return }
    if (!idCheck || idCheck.exists) { toast(t('duplicate.needFreeId'), 'error'); return }

    duplicating = true
    progress = null
    recentFiles = []
    subscribeProgress()

    try {
      const res: DuplicatePackResult = await App.DuplicatePack({
        sourcePackId: packId,
        newId: id.trim(),
        newName: name.trim(),
        description: '',
        copyIcon,
        iconSrcPath,
        icon: iconName,
        useCustomColor: true,
        color,
        copySettings,
        included,
        excluded,
      })
      toast(t('duplicate.done', { name: res.pack.name }), 'success')
      onDone(res.pack.id)
      onClose()
    } catch (e: any) {
      toast(t('duplicate.error', { message: e }), 'error')
    } finally {
      duplicating = false
      progressUnsub?.()
      progressUnsub = null
    }
  }

  function subscribeProgress() {
    progressUnsub?.()
    progressUnsub = on('duplicate:progress', (p: DuplicateProgress) => {
      progress = p
      if (p.fileName) {
        recentFiles = [p.fileName, ...recentFiles].slice(0, 12)
      }
    })
  }

  const stagePct = $derived.by(() => {
    if (!progress) return 0
    if (progress.total <= 0) return progress.stage === 'done' ? 100 : 0
    return Math.round((progress.current / progress.total) * 100)
  })
</script>

{#if duplicating}
  <!-- ── Живий прогрес-оверлей ── -->
  <UiModal open={true} dismissible={false} zIndex="2100000" size="sm" icon="ti-copy" color="var(--violet)"
           title={t('duplicate.title')}
           subtitle={progress?.stage === 'done' ? t('duplicate.stage.done') : t('duplicate.stage.copy')}>
    <div class="dup-prog-meta">
      <span>{progress?.current ?? 0} / {progress?.total ?? 0}</span>
      <span class="dup-prog-pct">{stagePct}%</span>
    </div>
    <div class="dup-prog-track"><div class="dup-prog-fill" style="width:{stagePct}%"></div></div>
    <div class="dup-prog-list">
      {#each recentFiles as f}
        <div class="dup-prog-file">
          <i class="ti ti-file"></i>
          <span title={f}>{f.split('/').pop()}</span>
        </div>
      {/each}
      {#if recentFiles.length === 0}
        <div class="dup-prog-wait"><i class="ti ti-loader-2 spin"></i></div>
      {/if}
    </div>
  </UiModal>
{:else}
  <UiModal
    open={true}
    width="min(960px, 94vw)"
    maxHeight="calc(100vh - 48px)"
    title={t('duplicate.title')}
    subtitle={t('duplicate.subtitle', { name: packName })}
    icon="ti-copy"
    color="var(--violet)"
    onclose={onClose}
  >
      <!-- Тіло: дві панелі -->
      <div class="dup-body">
        <!-- Ліва: дерево файлів -->
        <section class="dup-panel dup-files-panel">
          <div class="dup-panel-head">
            <span class="dup-panel-title"><i class="ti ti-folder-open"></i> {t('export.files')}</span>
            <span class="dup-panel-actions">
              <button class="dup-mini" onclick={selectAll}><i class="ti ti-select-all"></i> {t('export.selectAll')}</button>
              <button class="dup-mini" onclick={clearAll}><i class="ti ti-square-x"></i> {t('export.clearAll')}</button>
            </span>
          </div>

          <div class="dup-search">
            <i class="ti ti-search"></i>
            <input
              type="text"
              bind:value={treeFilter}
              oninput={onTreeFilterInput}
              placeholder={t('duplicate.searchFiles')}
              spellcheck="false"
            />
            {#if treeFilter}
              <button class="dup-search-clear" onclick={() => (treeFilter = '')} title={t('export.clearAll')}>
                <i class="ti ti-x"></i>
              </button>
            {/if}
          </div>

          <div class="dup-treewrap">
            {#if loadingCandidates}
              <div class="dup-empty"><i class="ti ti-loader-2 spin"></i></div>
            {:else if tree.length === 0}
              <div class="dup-empty">
                {#if treeFilter}
                  <i class="ti ti-search-off"></i><span>{t('duplicate.noMatches')}</span>
                {:else}
                  <i class="ti ti-package-off"></i>
                {/if}
              </div>
            {:else}
              <div class="dup-tree">
                {#each tree as node}
                  <ExportTreeNode {node} onToggle={toggleNode} />
                {/each}
              </div>
            {/if}
          </div>

          <div class="dup-panel-foot">
            <span class="dup-count"><i class="ti ti-file-check"></i> {t('export.selected', { n: selectedCount })}</span>
            <span class="dup-size">{fmtBytes(selectedSize)}</span>
          </div>
        </section>

        <!-- Права: параметри -->
        <section class="dup-panel dup-settings-panel">
          <div class="dup-field">
            <label class="dup-label" for="dup-name">{t('duplicate.name')}</label>
            <input id="dup-name" type="text" bind:value={name} oninput={onNameInput} placeholder={packName}
              onkeydown={(e) => { if (e.key === 'Enter') startDuplicate() }} />
          </div>

          <div class="dup-field">
            <label class="dup-label" for="dup-id">{t('duplicate.id')}</label>
            <div class="dup-idbox">
              <input
                id="dup-id"
                type="text"
                bind:value={id}
                oninput={onIDInput}
                class:bad={!!idCheck && idCheck.exists}
                class:good={!!idCheck && !idCheck.exists && id.trim() !== ''}
                placeholder="pack-id"
                spellcheck="false"
              />
              <span class="dup-idstatus">
                {#if idChecking}
                  <i class="ti ti-loader-2 spin"></i>
                {:else if idCheck && idCheck.exists}
                  <i class="ti ti-alert-circle"></i>
                {:else if idCheck && id.trim() !== ''}
                  <i class="ti ti-circle-check"></i>
                {/if}
              </span>
            </div>
            {#if idCheck && idCheck.exists}
              <div class="dup-idwarn">
                <span>{t('duplicate.idTaken', { id: idCheck.id })}</span>
                <button class="dup-idfix" onclick={useSuggested}>
                  <i class="ti ti-sparkles"></i> {t('duplicate.useSuggested', { id: idCheck.suggested })}
                </button>
              </div>
            {/if}
          </div>

          <div class="dup-field">
            <span class="dup-label">{t('duplicate.icon')}</span>
            <div class="dup-iconseg">
              <button class:sel={copyIcon} onclick={() => { copyIcon = true; iconSrcPath = '' }}>
                <span class="dup-iconseg-ic">
                  {#if effectiveIcon}
                    <img src={effectiveIcon} alt="" />
                  {:else}
                    <i class="ti {iconName}" style="color:{packColor}"></i>
                  {/if}
                </span>
                <span class="dup-iconseg-txt">{t('duplicate.copySourceIcon')}</span>
              </button>
              <button class:sel={!copyIcon} onclick={() => copyIcon = false}>
                <span class="dup-iconseg-ic" style="--pcol:{color}">
                  {#if iconSrcPath}
                    <img src={'local-file://' + iconSrcPath} alt="" />
                  {:else}
                    <i class="ti {iconName || 'ti-puzzle'}"></i>
                  {/if}
                </span>
                <span class="dup-iconseg-txt">{t('duplicate.customIcon')}</span>
              </button>
            </div>

            {#if !copyIcon}
              <div class="dup-iconpick">
                <PresetIconPicker value={iconName} color={color} onSelect={(ic) => iconName = ic} />
              </div>
              <div class="dup-colorbar">
                {#each colorPresets as c (c)}
                  <button class="dup-cdot" class:sel={color === c} style="background:{c}" onclick={() => color = c} title={c}></button>
                {/each}
                <label class="dup-ccustom" title={t('duplicate.customColor')}>
                  <input type="color" bind:value={color} />
                  <i class="ti ti-color-picker"></i>
                </label>
                <span class="dup-color-sep"></span>
                <button class="dup-filebtn" onclick={browseIcon}>
                  <i class="ti ti-photo-plus"></i> {t('duplicate.chooseFile')}
                </button>
              </div>
            {/if}
          </div>

          <label class="dup-settings-row" class:on={copySettings}>
            <span class="dup-settings-ic"><i class="ti ti-settings"></i></span>
            <span class="dup-settings-txt">
              <span class="dup-settings-title">{t('duplicate.copySettings')}</span>
              <span class="dup-settings-desc">{t('duplicate.copySettingsHint')}</span>
            </span>
            <span class="dup-switch">
              <input type="checkbox" bind:checked={copySettings} />
              <span class="dup-switch-slider"></span>
            </span>
          </label>
        </section>
      </div>

    {#snippet footer()}
      <button class="btn btn-ghost" onclick={() => onClose()}>{t('export.cancelBtn')}</button>
      <button class="dup-cta" onclick={startDuplicate} disabled={!canDuplicate}>
        <i class="ti ti-copy"></i> {t('duplicate.duplicateBtn')}
      </button>
    {/snippet}
  </UiModal>
{/if}

<style>
  /* ── Тіло: дві панелі однакової висоти, кожна скролиться сама ── */
  .dup-body {
    flex: 1; min-height: 0; display: grid;
    grid-template-columns: 1.2fr 1fr; grid-template-rows: minmax(0, 1fr);
    gap: 14px; padding: 16px 20px;
  }
  .dup-panel {
    display: flex; flex-direction: column; min-height: 0; min-width: 0;
    background: var(--bg-input); border: 1px solid var(--border);
    border-radius: 12px; padding: 12px; gap: 8px;
  }

  /* Панель файлів */
  .dup-panel-head { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
  .dup-panel-title {
    display: inline-flex; align-items: center; gap: 7px;
    font-size: 12px; font-weight: 800; text-transform: uppercase; letter-spacing: .4px; color: var(--text-dim);
  }
  .dup-panel-title .ti { color: var(--orange); font-size: 14px; }
  .dup-panel-actions { display: flex; gap: 4px; }
  .dup-mini {
    display: inline-flex; align-items: center; gap: 4px;
    border: 1px solid var(--border); background: var(--card, transparent);
    color: var(--text-dim); font-size: 11px; font-weight: 600;
    border-radius: 7px; padding: 4px 8px; cursor: pointer;
    transition: all .14s;
  }
  .dup-mini:hover { color: var(--violet); border-color: var(--violet); }

  .dup-search { position: relative; display: flex; align-items: center; }
  .dup-search > .ti-search {
    position: absolute; left: 9px; font-size: 13px; color: var(--text-mute); pointer-events: none;
  }
  .dup-search input {
    width: 100%; background: var(--bg-panel); border: 1px solid var(--border);
    border-radius: 8px; padding: 6px 28px 6px 30px; color: var(--text);
    font-family: inherit; font-size: 12px; transition: border-color .15s, box-shadow .15s;
  }
  .dup-search input:focus { border-color: var(--violet); outline: none; box-shadow: 0 0 0 3px color-mix(in srgb, var(--violet) 18%, transparent); }
  .dup-search input::placeholder { color: var(--text-mute); }
  .dup-search-clear {
    position: absolute; right: 4px; width: 22px; height: 22px; border: none; border-radius: 6px;
    background: transparent; color: var(--text-dim); cursor: pointer;
    display: flex; align-items: center; justify-content: center; font-size: 12px;
    transition: background .14s, color .14s;
  }
  .dup-search-clear:hover { background: var(--bg-input-hi); color: var(--text); }

  .dup-treewrap { flex: 1; min-height: 0; overflow: hidden; display: flex; flex-direction: column; }
  .dup-tree {
    flex: 1; min-height: 0; overflow-y: auto;
    background: var(--bg-panel); border: 1px solid var(--border); border-radius: 10px;
    padding: 6px;
  }
  .dup-tree::-webkit-scrollbar, .dup-prog-list::-webkit-scrollbar { width: 6px; }
  .dup-tree::-webkit-scrollbar-thumb, .dup-prog-list::-webkit-scrollbar-thumb { background: var(--hover-strong); border-radius: 3px; }
  .dup-empty {
    flex: 1; display: flex; align-items: center; justify-content: center; gap: 8px;
    color: var(--text-mute); font-size: 12.5px;
  }
  .dup-empty .ti { font-size: 20px; }

  .dup-panel-foot {
    display: flex; align-items: center; justify-content: space-between; gap: 8px;
    border-top: 1px solid var(--border); padding-top: 8px;
  }
  .dup-count { display: inline-flex; align-items: center; gap: 6px; font-size: 11.5px; font-weight: 700; color: var(--text-dim); }
  .dup-count .ti { color: var(--green); }
  .dup-size { font-size: 11px; color: var(--text-mute); font-weight: 600; }

  /* Панель параметрів */
  .dup-settings-panel { gap: 13px; overflow-y: auto; }
  .dup-settings-panel::-webkit-scrollbar { width: 6px; }
  .dup-settings-panel::-webkit-scrollbar-thumb { background: var(--hover-strong); border-radius: 3px; }
  .dup-field { display: flex; flex-direction: column; gap: 6px; }
  .dup-label { font-size: 11px; font-weight: 800; text-transform: uppercase; letter-spacing: .35px; color: var(--text-dim); }
  .dup-field input[type='text'] {
    width: 100%; background: var(--bg-panel); border: 1px solid var(--border);
    border-radius: 9px; padding: 8px 11px; color: var(--text);
    font-family: inherit; font-size: 12.5px; transition: border-color .15s, box-shadow .15s;
  }
  .dup-field input[type='text']:focus { border-color: var(--violet); outline: none; box-shadow: 0 0 0 3px color-mix(in srgb, var(--violet) 18%, transparent); }

  /* ID */
  .dup-idbox { position: relative; }
  .dup-idbox input { padding-right: 34px; }
  .dup-idbox input.bad { border-color: var(--red, #ef4444); }
  .dup-idbox input.good { border-color: var(--green, #10b981); }
  .dup-idstatus {
    position: absolute; right: 10px; top: 50%; transform: translateY(-50%);
    font-size: 14px; pointer-events: none;
  }
  .dup-idstatus .ti-loader-2 { color: var(--text-dim); }
  .dup-idstatus .ti-alert-circle { color: var(--red, #ef4444); }
  .dup-idstatus .ti-circle-check { color: var(--green, #10b981); }
  .dup-idwarn {
    display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
    background: color-mix(in srgb, var(--red, #ef4444) 9%, transparent);
    border: 1px solid color-mix(in srgb, var(--red, #ef4444) 30%, transparent);
    border-radius: 9px; padding: 7px 9px; font-size: 11.5px; color: var(--red, #ef4444);
    animation: dup-warn-in .18s ease;
  }
  @keyframes dup-warn-in { from { opacity: 0; transform: translateY(-3px); } to { opacity: 1; transform: none; } }
  .dup-idfix {
    display: inline-flex; align-items: center; gap: 5px;
    background: var(--card, transparent); border: 1px solid var(--border-hi);
    color: var(--text); font-size: 11px; font-weight: 700;
    border-radius: 7px; padding: 4px 8px; cursor: pointer;
    transition: all .14s; margin-left: auto;
  }
  .dup-idfix:hover { border-color: var(--orange); color: var(--orange); }

  /* Сегментований вибір іконки */
  .dup-iconseg { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; }
  .dup-iconseg button {
    display: flex; align-items: center; gap: 10px;
    background: var(--bg-panel); border: 1.5px solid var(--border);
    border-radius: 10px; padding: 8px 10px; cursor: pointer;
    transition: border-color .15s, background .15s, box-shadow .15s;
    color: var(--text);
  }
  .dup-iconseg button:hover { border-color: var(--text-dim); }
  .dup-iconseg button.sel {
    border-color: var(--orange);
    background: color-mix(in srgb, var(--orange) 8%, transparent);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--orange) 14%, transparent);
  }
  .dup-iconseg-ic {
    flex: none; width: 34px; height: 34px; border-radius: 9px;
    display: flex; align-items: center; justify-content: center;
    background: color-mix(in srgb, var(--orange) 12%, transparent);
    color: var(--orange); font-size: 16px; overflow: hidden;
  }
  .dup-iconseg-ic img { width: 100%; height: 100%; object-fit: cover; }
  .dup-iconseg-txt { font-size: 11.5px; font-weight: 600; line-height: 1.3; text-align: left; }

  .dup-iconpick {
    max-height: 220px; overflow-y: auto;
    background: var(--bg-panel); border: 1px solid var(--border); border-radius: 10px; padding: 8px;
  }

  .dup-colorbar { display: flex; align-items: center; gap: 7px; flex-wrap: wrap; }
  .dup-cdot {
    width: 22px; height: 22px; border-radius: 50%;
    border: 2px solid transparent; cursor: pointer;
    transition: transform .12s, border-color .12s, box-shadow .12s;
  }
  .dup-cdot:hover { transform: scale(1.15); }
  .dup-cdot.sel { border-color: var(--text); box-shadow: 0 0 0 2px var(--bg) inset; }
  .dup-ccustom {
    position: relative; width: 22px; height: 22px; border-radius: 50%; overflow: hidden;
    border: 1.5px dashed var(--text-dim); cursor: pointer;
    display: flex; align-items: center; justify-content: center;
    color: var(--text-dim); font-size: var(--fs-min); transition: color .12s;
  }
  .dup-ccustom:hover { color: var(--text); }
  .dup-ccustom input[type='color'] { position: absolute; inset: -6px; opacity: 0; cursor: pointer; width: 40px; height: 40px; }
  .dup-color-sep { width: 1px; height: 18px; background: var(--border); }
  .dup-filebtn {
    display: inline-flex; align-items: center; gap: 5px;
    background: var(--bg-panel); border: 1px solid var(--border);
    color: var(--text-dim); font-size: 11px; font-weight: 600;
    border-radius: 7px; padding: 5px 9px; cursor: pointer; transition: all .14s;
  }
  .dup-filebtn:hover { color: var(--violet); border-color: var(--violet); }

  /* Копіювання налаштувань */
  .dup-settings-row {
    display: flex; align-items: center; gap: 10px;
    background: var(--bg-panel); border: 1.5px solid var(--border);
    border-radius: 11px; padding: 10px 12px; cursor: pointer;
    transition: border-color .15s, background .15s;
  }
  .dup-settings-row:hover { border-color: var(--text-dim); }
  .dup-settings-row.on { border-color: var(--orange); background: color-mix(in srgb, var(--orange) 6%, transparent); }
  .dup-settings-ic {
    flex: none; width: 30px; height: 30px; border-radius: 8px;
    background: color-mix(in srgb, var(--orange) 14%, transparent);
    color: var(--orange); display: flex; align-items: center; justify-content: center; font-size: 15px;
  }
  .dup-settings-txt { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 1px; }
  .dup-settings-title { font-size: 12px; font-weight: 700; color: var(--text); }
  .dup-settings-desc { font-size: var(--fs-min); color: var(--text-mute); line-height: 1.4; }

  /* Кастомний перемикач */
  .dup-switch { flex: none; position: relative; width: 36px; height: 20px; }
  .dup-switch input { position: absolute; opacity: 0; width: 100%; height: 100%; margin: 0; cursor: pointer; z-index: 2; }
  .dup-switch-slider {
    position: absolute; inset: 0; border-radius: 99px;
    background: var(--bg-input-hi); transition: background .18s;
  }
  .dup-switch-slider::after {
    content: ''; position: absolute; top: 2px; left: 2px; width: 16px; height: 16px;
    border-radius: 50%; background: #fff; transition: transform .18s;
    box-shadow: 0 1px 3px rgba(0,0,0,.35);
  }
  .dup-switch input:checked + .dup-switch-slider { background: var(--orange); }
  .dup-switch input:checked + .dup-switch-slider::after { transform: translateX(16px); }

  /* ── Футер ── */
  .dup-foot {
    display: flex; align-items: center; justify-content: flex-end; gap: 10px;
    padding: 14px 20px; border-top: 1px solid var(--border);
  }
  .dup-cta {
    display: inline-flex; align-items: center; gap: 7px;
    border: none; border-radius: 10px; padding: 9px 18px;
    font-family: inherit; font-size: 13px; font-weight: 800; color: #fff; cursor: pointer;
    background: linear-gradient(135deg, var(--orange), color-mix(in srgb, var(--orange) 55%, #ff4d6d));
    box-shadow: 0 4px 16px color-mix(in srgb, var(--orange) 35%, transparent);
    transition: transform .14s, box-shadow .14s, filter .14s;
  }
  .dup-cta:hover:not(:disabled) { transform: translateY(-1px); filter: brightness(1.06); box-shadow: 0 6px 20px color-mix(in srgb, var(--orange) 45%, transparent); }
  .dup-cta:active:not(:disabled) { transform: translateY(0); }
  .dup-cta:disabled { opacity: .45; cursor: not-allowed; box-shadow: none; }

  /* ── Прогрес ── */
  .dup-prog-pct { font-size: 13px; font-weight: 800; color: var(--orange); }
  .dup-prog-track {
    height: 8px; margin-top: 8px; border-radius: 99px;
    background: var(--bg-input); overflow: hidden;
  }
  .dup-prog-fill {
    height: 100%; border-radius: 99px;
    background: linear-gradient(90deg, var(--violet), var(--orange));
    transition: width .25s ease;
  }
  .dup-prog-meta { display: flex; justify-content: space-between; align-items: baseline; font-size: 11px; color: var(--text-dim); font-weight: 600; }
  .dup-prog-list {
    margin-top: 12px; max-height: 180px; overflow-y: auto;
    display: flex; flex-direction: column; gap: 4px;
  }
  .dup-prog-file {
    display: flex; align-items: center; gap: 8px;
    background: var(--bg-input); border: 1px solid var(--border);
    border-radius: 8px; padding: 5px 9px; font-size: 11.5px; color: var(--text);
    animation: dup-file-in .22s ease;
  }
  @keyframes dup-file-in { from { opacity: 0; transform: translateY(-4px); } to { opacity: 1; transform: none; } }
  .dup-prog-file .ti-file { color: var(--text-mute); font-size: 13px; flex: none; }
  .dup-prog-file span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .dup-prog-wait { display: flex; align-items: center; justify-content: center; min-height: 60px; color: var(--text-mute); }
</style>
