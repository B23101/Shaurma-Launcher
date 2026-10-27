<script lang="ts">
  import Portal from './Portal.svelte'
  import UiModal from './UiModal.svelte'
  // ── Експорт збірки у .mrpack / .zip ────────────────────────────────────
  // Модалка (патерн ModMigrationDialog): дерево вибору папок/файлів з
  // каскадними чекбоксами, формат (.mrpack і/або .zip), тека призначення,
  // назва/версія/опис. Вибір ЗБЕРІГАЄТЬСЯ у localStorage (переживає
  // перезапуск лаунчера). Якщо цільовий файл уже існує — підтвердження
  // перезапису. Прогрес — живий оверлей з іконками модів (export:progress).
  import { onMount, onDestroy } from 'svelte'
  import { t } from './i18n.svelte'
  import { App, on } from './wails'
  import { toast, confirmDialog } from './toast.svelte'
  import ExportTreeNode, { type ExportTreeNodeData } from './ExportTreeNode.svelte'
  import type { ExportCandidate, ExportProgress, ExportPackResult, ExportShaurmpackRequest, ExportShaurmpackResult } from './types'

  let {
    packId = '',
    packName = '',
    onClose = () => {}
  }: {
    packId?: string
    packName?: string
    onClose?: () => void
  } = $props()

  const storageKey = `shaurma.export.${packId}`

  let candidates = $state<ExportCandidate[]>([])
  let loadingCandidates = $state(true)
  let included = $state<string[]>([])
  let excluded = $state<string[]>([])
  let name = $state(packName)
  let version = $state('1.0.0')
  let summary = $state('')
  let formats = $state<string[]>(['mrpack'])
  let outDir = $state('')

  let exporting = $state(false)
  let progress = $state<ExportProgress | null>(null)
  let recentFiles = $state<{ file: string; modName?: string; iconUrl?: string }[]>([])
  let progressUnsub: (() => void) | null = null

  let tree = $state<ExportTreeNodeData[]>([])

  onMount(async () => {
    try {
      candidates = (await App.ListExportCandidates(packId)) ?? []
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      loadingCandidates = false
      buildTree()
      await restoreSelection()
    }
  })

  onDestroy(() => {
    progressUnsub?.()
  })

  type SavedSelection = { included?: string[]; excluded?: string[]; formats?: string[]; outDir?: string; name?: string; version?: string; summary?: string }

  // restoreSelection — джерело правди ТЕПЕР БД (export_settings, SQLite,
  // Частина «зберігати вибір» задачі): переживає переустановлення
  // лаунчера/чистку профілю, на відміну від localStorage. localStorage
  // лишається ФОЛБЕКОМ у двох випадках: (1) міграція — параметри,
  // збережені до появи БД-шару, ще можуть лежати тільки там; (2) БД
  // недоступна (App.GetExportSettings повернув '' навіть якщо раніше
  // щось зберігалось — рідкісний edge-case, напр. свіжий профіль без
  // міграції). Обидва джерела читаються, БД має пріоритет, якщо в ній
  // щось є.
  async function restoreSelection() {
    let saved: SavedSelection | null = null
    try {
      const dbRaw = await App.GetExportSettings(packId)
      if (dbRaw) saved = JSON.parse(dbRaw)
    } catch { /* ignore — падаємо на localStorage нижче */ }
    if (!saved) {
      try {
        const raw = localStorage.getItem(storageKey)
        if (raw) saved = JSON.parse(raw)
      } catch { /* ignore */ }
    }
    if (saved?.included?.length) {
      included = [...saved.included]
    } else {
      // Перший запуск — дефолтний вибір (як Modrinth App DEFAULT_SELECTED
      // _EXPORT_PATH_PREFIXES): mods/datapacks/resourcepacks/shaderpacks/config.
      // Без цього дерево було б порожнім і «Експортувати» падав би.
      included = candidates.filter(c => c.defaultSelected).map(c => c.path)
    }
    if (saved?.excluded) excluded = [...saved.excluded]
    // Формат «звичайний zip» прибрано (лишились mrpack і curseforge) —
    // викидаємо його із збереженого вибору старих сесій, щоб після
    // оновлення лаунчера не повернулась опція, якої більше немає.
    if (saved?.formats?.length) formats = [...saved.formats].filter(f => f !== 'zip')
    if (saved?.outDir) outDir = saved.outDir
    if (saved?.name) name = saved.name
    if (saved?.version) version = saved.version
    if (saved?.summary) summary = saved.summary
    syncChecks()
  }

  // persistSelection пише в ОБИДВА місця: БД (основне джерело правди,
  // переживає переустановлення) і localStorage (миттєвий локальний кеш —
  // не чекаємо Wails IPC round-trip на кожен клік по чекбоксу дерева,
  // await App.SaveExportSettings() навмисно НЕ await-иться тут, працює
  // у фоні). Якщо збереження в БД впаде — localStorage все одно матиме
  // актуальний стан у межах цієї сесії лаунчера.
  function persistSelection() {
    const payload = JSON.stringify({ included, excluded, formats, outDir, name, version, summary })
    try {
      localStorage.setItem(storageKey, payload)
    } catch { /* ignore */ }
    App.SaveExportSettings(packId, payload).catch(() => { /* ignore — localStorage вже записано */ })
  }

  function buildTree() {
    const index = new Map<string, ExportTreeNodeData>()
    const root: ExportTreeNodeData[] = []
    for (const c of candidates) {
      const parts = c.path.split('/')
      let level = root
      let cur = ''
      for (let i = 0; i < parts.length; i++) {
        cur = cur ? cur + '/' + parts[i] : parts[i]
        let node = index.get(cur)
        if (!node) {
          const isLeaf = i === parts.length - 1
          // Усі папки, БЕЗ ВИНЯТКІВ (включно з кореневими розділами mods/,
          // config/, assets/... і будь-якою глибиною вкладеності),
          // ЗГОРНУТІ за замовчуванням. На реальних збірках трапляються
          // теки з сотнями/тисячами вкладених підпапок (asset-паки,
          // worldedit/.archive-unpack/<hash>/<lang>/..., installer/com/...) —
          // якщо розкрити хоч один рівень автоматично, дерево все одно
          // розтягується на екрани. Користувач розгортає вручну кліком по
          // шеврону саме ті папки, які йому потрібні.
          //
          // isLeaf визначає ТИП (файл/директорія) — ЗАВЖДИ виставляємо
          // collapsed=true для вузла-директорії напряму (не через `!isLeaf`),
          // щоб не залежати від правильності isLeaf і не отримати
          // "розкрито" через побічний edge-case (напр. файл і директорія
          // з однаковим ім'ям на різних рівнях кандидатів).
          const type = isLeaf ? c.type : 'directory'
          node = {
            path: cur,
            type,
            size: isLeaf ? c.size : undefined,
            defaultSelected: isLeaf ? c.defaultSelected : false,
            children: [],
            state: 'unchecked',
            collapsed: type === 'directory'
          }
          index.set(cur, node)
          level.push(node)
        } else if (node.type === 'directory') {
          // Страховка: якщо вузол-директорія вже існував (напр. створений
          // раніше як проміжний рівень іншого файлу), гарантуємо, що він
          // лишається згорнутим, а не був випадково розкритий.
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

  // syncChecks — ОДИН прохід дерева знизу вгору (post-order): стан
  // кожного вузла рахується РІВНО ОДИН РАЗ, з уже готових n.state дітей
  // (nodeState вище читає child.state, не рекурсивно перераховує його
  // через дітей дітей). Раніше nodeState була рекурсивною САМА ПО СОБІ
  // (n.children.map(c => nodeState(c)), кожен виклик наново спускався у
  // всіх нащадків), а syncChecks викликала її ЩЕ РАЗ на кожному вузлі
  // під час обходу walk(tree) — тобто для вузла на глибині d його
  // піддерево перераховувалось O(глибина_дерева) разів. На збірці з
  // тисячами файлів (мод-паки часто мають 5000+ файлів у mods/config/
  // resourcepacks) це перетворювалось на секунди лагу на КОЖЕН клік по
  // чекбоксу. Post-order (спочатку діти, потім сам вузол) прибирає
  // повторний рахунок повністю — загальна робота O(кількість_вузлів),
  // а не O(вузли × глибина).
  //
  // includedSet/excludedSet — Set замість Array.includes(): included/
  // excluded теж ростуть до тисяч шляхів на великих збірках, а
  // Array.includes() у nodeState був би O(n) на КОЖЕН вузол — разом з
  // O(вузли) викликів це давало O(вузли × елементи_included) додатково
  // до самого обходу дерева. Set.has() — O(1), будується один раз на
  // виклик syncChecks(), не на кожен вузол.
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
  }

  function selectAll() {
    included = candidates.map(c => c.path)
    excluded = []
    syncChecks()
  }

  function clearAll() {
    included = []
    excluded = candidates.map(c => c.path)
    syncChecks()
  }

  const selectedCount = $derived(included.length)
  const canExport = $derived(!exporting && name.trim() !== '' && formats.length > 0 && outDir.trim() !== '')

  async function startExport(force = false) {
    if (exporting) return
    if (name.trim() === '') { toast(t('export.needName'), 'error'); return }
    if (formats.length === 0) { toast(t('export.needFormat'), 'error'); return }
    persistSelection()

    exporting = true
    progress = null
    recentFiles = []
    subscribeProgress()

    // .shaurmpack іде окремим Wails-методом (ExportShaurmpack) — не через
    // ExportPack (той знає лише mrpack/curseforge, formats туди йде без
    // 'shaurmpack'). Обидва можуть бути обрані одночасно (звичайний
    // мультиселект чекбоксів) — тоді виконуємо ОБИДВА виклики послідовно.
    const regularFormats = formats.filter(f => f !== 'shaurmpack') as ('mrpack' | 'curseforge')[]
    const wantShaurmpack = formats.includes('shaurmpack')
    const doneFiles: string[] = []

    try {
      if (regularFormats.length > 0) {
        const res: ExportPackResult = await App.ExportPack({
          packId,
          name: name.trim(),
          version: version.trim() || '1.0.0',
          summary,
          formats: regularFormats,
          outDir,
          included,
          excluded,
          force
        })
        if (res.conflicts && res.conflicts.length > 0) {
          const files = res.conflicts.map(f => f.split(/[\\/]/).pop() ?? f).join('\n')
          const ok = await confirmDialog(t('export.conflictTitle'), t('export.conflictMsg', { files }), {
            confirmLabel: t('export.conflictDelete'),
            cancelLabel: t('export.cancelBtn'),
            danger: true
          })
          if (ok) {
            // ВАЖЛИВО: скидаємо прапорці ПЕРЕД повторним запуском. Раніше
            // рекурсивний startExport(true) блокувався рядком `if (exporting)
            // return` — exporting був ще true, тому «Видалити і експортувати»
            // просто нічого не робило (ні видаляло, ні експортувало).
            exporting = false
            progressUnsub?.()
            progressUnsub = null
            await startExport(true)
            return
          }
          exporting = false
          return
        }
        doneFiles.push(...(res.files ?? []))
      }

      if (wantShaurmpack) {
        // included/excluded ОБОВ'ЯЗКОВО передаємо й сюди: без них бекенд
        // пакував всю теку збірки, хоч у дереві вибору було позначено лише
        // частину (скарга «вибрав mods, а він пакує все»).
        const req: ExportShaurmpackRequest = {
          packId,
          name: name.trim(),
          version: version.trim() || '1.0.0',
          description: summary,
          outDir,
          included,
          excluded,
          force
        }
        const sres: ExportShaurmpackResult = await App.ExportShaurmpack(req)
        if (sres.conflict) {
          const fileLabel = sres.outPath.split(/[\\/]/).pop() ?? sres.outPath
          const ok = await confirmDialog(t('export.conflictTitle'), t('export.conflictMsg', { files: fileLabel }), {
            confirmLabel: t('export.conflictDelete'),
            cancelLabel: t('export.cancelBtn'),
            danger: true
          })
          if (ok) {
            const sres2 = await App.ExportShaurmpack({ ...req, force: true })
            doneFiles.push(sres2.outPath)
          } else if (doneFiles.length === 0) {
            exporting = false
            return
          }
        } else {
          doneFiles.push(sres.outPath)
        }
      }

      const files = doneFiles.map(f => f.split(/[\\/]/).pop() ?? f).join(', ')
      toast(t('export.done', { files }), 'success')
      onClose()
    } catch (e: any) {
      toast(t('export.error', { message: e }), 'error')
    } finally {
      exporting = false
      progressUnsub?.()
      progressUnsub = null
    }
  }

  function subscribeProgress() {
    progressUnsub?.()
    progressUnsub = on('export:progress', (p: ExportProgress) => {
      if (p.packId !== packId) return
      progress = p
      if (p.fileName) {
        recentFiles = [{ file: p.fileName, modName: p.modName, iconUrl: p.iconUrl }, ...recentFiles].slice(0, 12)
      }
    })
  }

  const stageLabel = $derived.by(() => {
    if (!progress) return ''
    switch (progress.stage) {
      case 'mod_sources': return t('export.stage.modSources')
      case 'scan': return t('export.stage.scan')
      case 'hash': return t('export.stage.hash')
      case 'resolve': return t('export.stage.resolve')
      case 'pack': return t('export.stage.pack')
      default: return ''
    }
  })

  const stagePct = $derived.by(() => {
    if (!progress) return 0
    if (progress.total <= 0) return progress.stage === 'done' ? 100 : 0
    return Math.round((progress.current / progress.total) * 100)
  })

  async function browseDir() {
    const dir = await App.BrowseFolder()
    if (dir) outDir = dir
  }
</script>

{#if exporting}
  <!-- ── Живий прогрес-оверлей ── -->
  <Portal>
<div class="modal-overlay" style="z-index:2100000">
    <div class="modal export-progress-modal" onclick={(e: any) => e.stopPropagation()}>
      <div class="modal-head">
        <h3><i class="ti ti-package-export" style="color:var(--violet)"></i> {t('export.title')}</h3>
      </div>
      <div class="modal-body">	        <div class="ep-stage">{stageLabel || (progress ? '' : '…')}</div>
	        <div class="ep-track">
	          <div class="ep-fill" style="width:{progress ? stagePct : 0}%"></div>
	        </div>
	        <div class="ep-pct">{progress ? `${stagePct}%` : '…'}</div>

        <div class="ep-stream">
          {#each recentFiles as f}
            <div class="ep-stream-item">
              {#if f.iconUrl}
                <img src={f.iconUrl} alt="" class="ep-icon" loading="lazy" />
              {:else}
                <i class="ti ti-file"></i>
              {/if}
              <span class="ep-fname" title={f.file}>{f.modName || f.file.split('/').pop()}</span>
            </div>
          {/each}
          {#if recentFiles.length === 0}
            <div class="ep-stream-empty"><i class="ti ti-loader" style="animation:spin 1s linear infinite"></i></div>
          {/if}
        </div>
      </div>
      <div class="modal-foot">
        <button class="btn btn-ghost" disabled>{t('export.cancelBtn')}</button>
      </div>
    </div>
  </div>
</Portal>
{:else}
  <!-- ── Форма експорту ── -->
  <UiModal
    open={true}
    width="min(860px, 94vw)"
    title={t('export.title')}
    subtitle={t('export.subtitle')}
    icon="ti-package-export"
    color="var(--violet)"
    onclose={onClose}
  >
        <p class="ep-subtitle">{t('export.subtitle')}</p>

        <div class="ep-grid">
          <div class="ep-tree-col">
            <div class="ep-tree-head">
              <span class="ep-label">{t('export.files')}</span>
              <span class="ep-tree-actions">
                <button class="btn btn-ghost btn-xs" onclick={selectAll}><i class="ti ti-select-all"></i> {t('export.selectAll')}</button>
                <button class="btn btn-ghost btn-xs" onclick={clearAll}><i class="ti ti-square-x"></i> {t('export.clearAll')}</button>
              </span>
            </div>
            <div class="ep-hint">{t('export.filesHint')}</div>
            {#if loadingCandidates}
              <div class="ep-empty"><i class="ti ti-loader" style="animation:spin 1s linear infinite"></i></div>
            {:else if tree.length === 0}
              <div class="ep-empty"><i class="ti ti-package-off"></i></div>
            {:else}
              <div class="ep-tree">
                {#each tree as node}
                  <ExportTreeNode {node} onToggle={toggleNode} />
                {/each}
              </div>
            {/if}
            <div class="ep-selected-count">{t('export.selected', { n: selectedCount })}</div>
          </div>

          <div class="ep-settings-col">
            <label class="ep-field">
              <span>{t('export.name')}</span>
              <input type="text" bind:value={name} placeholder={packName} />
            </label>

            <label class="ep-field">
              <span>{t('export.version')} <small>({t('export.versionHint')})</small></span>
              <input type="text" bind:value={version} placeholder="1.0.0" />
            </label>

            <label class="ep-field">
              <span>{t('export.summary')}</span>
              <textarea bind:value={summary} rows={2}></textarea>
            </label>

            <div class="ep-field">
              <span>{t('export.format')}</span>
              <div class="ep-formats">
                <label class="ep-check">
                  <input type="checkbox" checked={formats.includes('mrpack')} onclick={(e) => {
                    const on2 = (e.currentTarget as HTMLInputElement).checked
                    formats = on2 ? [...new Set([...formats, 'mrpack'])] : formats.filter(f => f !== 'mrpack')
                  }} />
                  <span class="ep-format-chip chip-mrpack"><i class="ti ti-box"></i> {t('export.mrpack')}</span>
                </label>
                <label class="ep-check">
                  <input type="checkbox" checked={formats.includes('curseforge')} onclick={(e) => {
                    const on2 = (e.currentTarget as HTMLInputElement).checked
                    formats = on2 ? [...new Set([...formats, 'curseforge'])] : formats.filter(f => f !== 'curseforge')
                  }} />
                  <span class="ep-format-chip chip-cf"><i class="ti ti-flame"></i> {t('export.curseforge')}</span>
                </label>
                <label class="ep-check">
                  <input type="checkbox" checked={formats.includes('shaurmpack')} onclick={(e) => {
                    const on2 = (e.currentTarget as HTMLInputElement).checked
                    formats = on2 ? [...new Set([...formats, 'shaurmpack'])] : formats.filter(f => f !== 'shaurmpack')
                  }} />
                  <span class="ep-format-chip chip-shaurmpack"><i class="ti ti-package"></i> {t('export.shaurmpack')}</span>
                </label>
              </div>
              <div class="ep-format-hint">{t('export.cfHint')}</div>
              {#if formats.includes('shaurmpack')}
                <div class="ep-format-hint">{t('export.shaurmpackHint')}</div>
              {/if}
            </div>

            <label class="ep-field">
              <span>{t('export.outDir')}</span>
              <div class="ep-outdir">
                <input type="text" bind:value={outDir} placeholder="C:\…" />
                <button class="btn btn-ghost btn-sm" onclick={browseDir}><i class="ti ti-folder-open"></i> {t('export.browse')}</button>
              </div>
            </label>
          </div>
        </div>
    {#snippet footer()}
      <button class="btn btn-ghost" onclick={() => onClose()}>{t('export.cancelBtn')}</button>
      <button class="btn btn-primary" onclick={() => startExport(false)} disabled={!canExport}>
        <i class="ti ti-package-export"></i> {t('export.exportBtn')}
      </button>
    {/snippet}
  </UiModal>
{/if}
