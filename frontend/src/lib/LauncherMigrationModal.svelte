<script lang="ts">
  // ── LauncherMigrationModal: міграція збірок з інших лаунчерів ──────────
  // Показується після входу (один раз — галочка MigrateDismiss у БД) або
  // з віджета в Налаштуваннях. Сканує інші лаунчери (Prism / Modrinth App)
  // у ЇХНІХ стандартних теках даних — жодного обходу дисків (антивірусна
  // безпека). Кожен знайдений лаунчер — вкладка зі списком збірок;
  // гравець сам вибирає, що переносити («Виділити все» / «Не виділяти
  // все»). Імпорт — чисте копіювання (файли без кешів, налаштування,
  // іконка, ID/назва), прогрес кожної збірки — у цій же модалці.
  //
  // Публічний контракт: open + onClose. Сканування і стан — усередині
  // (самодостатній компонент), тож App.svelte лише відкриває/закриває.

  import UiModal from './UiModal.svelte'
  import { t } from './i18n.svelte'
  import { App, on } from './wails'
  import { packAsset } from './packAssets.svelte'
  import prismIcon from '../assets/images/prism_icon.svg'
  import modrinthIcon from '../assets/images/Modrinth_icon.png'
  import type {
    MigrationLauncher, MigrationPackItem, MigrateImportProgress, MigrateImportResult
  } from './types'

  let { open, onClose, dismissOnClose = false, initialLaunchers = [] } = $props<{
    open: boolean
    onClose: () => void
    // initialLaunchers — уже отримані App.MigrateStatus() лаунчери зі збірками:
    // тоді модалка показується одразу, без повторного (довгого) сканування.
    initialLaunchers?: MigrationLauncher[]
    // dismissOnClose — викликати App.MigrateDismiss() при закритті (галочка
    // «ми показали вікно»): для пост-логін сценарію, де вікно не має
    // спливати знову. Для віджета в Налаштуваннях — false.
    dismissOnClose?: boolean
  }>()

  let launchers = $state<MigrationLauncher[]>([])
  // Є дані зі статусу — стартуємо без «Сканування…», щоб не показувати
  // нескінченне завантаження.
  let loading = $state(initialLaunchers.length === 0)
  let scanError = $state('')
  let activeTab = $state<string>('')
  let selected = $state<Record<string, Set<string>>>({}) // launcherKind -> packId set
  // progress: packId -> MigrateImportProgress (активний імпорт).
  let progress = $state<Record<string, MigrateImportProgress>>({})
  let importing = $state(false)
  let done = $state(false)
  let unsubs: Array<() => void> = []

  $effect(() => {
    if (open) {
      if (initialLaunchers.length > 0) applyLaunchers(initialLaunchers)
      else scan()
    }
    return () => { for (const u of unsubs) u(); unsubs = [] }
  })

  function applyLaunchers(list: MigrationLauncher[]) {
    loading = false; scanError = ''; done = false
    launchers = list.filter((l: MigrationLauncher) => l.found)
    if (launchers.length > 0 && !launchers.some((l: MigrationLauncher) => l.kind === activeTab)) {
      activeTab = launchers[0].kind
    }
    for (const l of launchers) {
      if (!selected[l.kind]) selected[l.kind] = new Set()
    }
    subscribeProgress()
  }

  async function scan() {
    loading = true; scanError = ''; done = false
    try {
      const res = await App.MigrateScan()
      launchers = res.launchers.filter((l: MigrationLauncher) => l.found)
      // Перша вкладка — перший знайдений лаунчер; перевибір при повторному
      // скануванні (напр. після того, як знайшли новий лаунчер).
      if (launchers.length > 0 && !launchers.some((l: MigrationLauncher) => l.kind === activeTab)) {
        activeTab = launchers[0].kind
      }
      for (const l of launchers) {
        if (!selected[l.kind]) selected[l.kind] = new Set()
      }
      subscribeProgress()
      // Автоматичне вікно (після входу) не має показувати «нічого не знайдено»:
      // якщо збірок для переносу немає — тихо закриваємось (без галочки
      // «показано», щоб вікно спливло, коли збірки з'являться).
      if (dismissOnClose && !launchers.some((l: MigrationLauncher) => l.packs.length > 0)) onClose()
    } catch (e: any) {
      scanError = String(e?.message ?? e)
    } finally {
      loading = false
    }
  }

  // Прогрес імпорту — подіями migrate:progress; підсумок — migrate:done.
  function subscribeProgress() {
    unsubs.push(on('migrate:progress', (p: MigrateImportProgress) => {
      if (p?.packId) progress[p.packId] = p
    }))
    unsubs.push(on('migrate:done', (_r: MigrateImportResult) => {
      importing = false
      done = true
    }))
  }

  const activeLauncher = $derived(launchers.find((l: MigrationLauncher) => l.kind === activeTab) ?? null)

  function togglePack(l: string, id: string) {
    const s = selected[l] ?? new Set<string>()
    if (s.has(id)) s.delete(id); else s.add(id)
    selected = { ...selected, [l]: s }
  }

  function toggleAll(l: string) {
    const launcher = launchers.find((x: MigrationLauncher) => x.kind === l)
    if (!launcher) return
    const s = selected[l] ?? new Set<string>()
    if (s.size === launcher.packs.length) {
      selected = { ...selected, [l]: new Set() }
      return
    }
    // «Виділити все» — обирає лише ще НЕ перенесені збірки (уже завантажені
    // можна додати вручну — повторна міграція дасть копію з «(2)»).
    const ids = launcher.packs
      .filter((p: MigrationPackItem) => !p.alreadyImported)
      .map((p: MigrationPackItem) => p.id)
    selected = { ...selected, [l]: new Set(ids) }
  }

  const selectedCount = $derived((l: string) => selected[l]?.size ?? 0)
  // «Усі виділені» — коли обрано всі НЕ перенесені збірки (перенесені
  // лишаються невибраними за замовчуванням, тож повний вибір = всі нові).
  const allSelected = $derived((l: string) => {
    const launcher = launchers.find((x: MigrationLauncher) => x.kind === l)
    if (!launcher || launcher.packs.length === 0) return false
    const selectable = launcher.packs.filter((p: MigrationPackItem) => !p.alreadyImported)
    if (selectable.length === 0) return (selected[l]?.size ?? 0) === 0
    return selectable.every((p: MigrationPackItem) => selected[l]?.has(p.id))
  })

  const totalSelected = $derived(
    Object.entries(selected).reduce((n, [k, s]) => n + (launchers.some(l => l.kind === k) ? s.size : 0), 0)
  )

  function fmtSize(bytes: number): string {
    if (!bytes) return ''
    const mb = bytes / 1024 / 1024
    if (mb >= 1024) return (mb / 1024).toFixed(1) + ' GB'
    return Math.round(mb) + ' MB'
  }

  // Прев'ю PNG-іконки збірки іншого лаунчера (data URL через бекенд).
  // Завантажуємо іконки для збірок АКТИВНОЇ вкладки (та що користувач
  // бачить); при перемиканні вкладки довантажуємо нові. Результат
  // кешується в iconCache — повторні виклики (зміна стану) не б'ють по
  // бекенду.
  let iconCache = $state<Record<string, string>>({})
  async function iconFor(p: MigrationPackItem): Promise<void> {
    if (!p.iconPath || iconCache[p.id]) return
    try {
      const url = await App.MigrateIconDataURL(p.iconPath)
      if (url) iconCache = { ...iconCache, [p.id]: url }
    } catch { /* іконку не вдалось — лишаємо плейсхолдер */ }
  }

  $effect(() => {
    const launcher = launchers.find((l: MigrationLauncher) => l.kind === activeTab)
    if (!launcher) return
    for (const p of launcher.packs) iconFor(p)
  })

  async function doImport() {
    const packIds = [...(selected[activeTab] ?? [])]
    if (packIds.length === 0) return
    importing = true; done = false
    try {
      // Стан «імпортується» для кожної обраної збірки (бар «0%» чесно видно).
      for (const id of packIds) {
        if (!progress[id]) progress[id] = { launcher: activeTab, packId: id, packName: '', stage: 'copying', filesDone: 0, filesTotal: 0 }
      }
      await App.MigrateImport({ launcher: activeTab, packIds })
      // done приходить подією migrate:done (вище) — метод повертає той самий
      // підсумок, але подія — основний канал для оновлення цієї модалки.
    } catch (e: any) {
      scanError = String(e?.message ?? e)
      importing = false
    }
  }

  function close() {
    if (dismissOnClose) { App.MigrateDismiss().catch(() => {}) }
    onClose()
  }

  function pct(p: MigrateImportProgress | undefined): number {
    if (!p) return 0
    if (p.stage === 'done') return 100
    if (p.filesTotal > 0) return Math.min(100, Math.round((p.filesDone / p.filesTotal) * 100))
    return 0
  }
</script>

<UiModal {open} onclose={close} locked={importing} icon="ti-arrows-shuffle" title={t('migrate.title')}
         width="min(560px, 94vw)" maxHeight="84vh" bodyClass="no-pad"
         footer={!loading && !scanError && activeLauncher ? foot : null}>

    {#if loading}
      <div class="lm-empty"><i class="ti ti-loader spin"></i> {t('migrate.scanning')}</div>
    {:else if scanError}
      <div class="lm-empty lm-err"><i class="ti ti-alert-triangle"></i> {scanError}</div>
    {:else if launchers.length === 0}
      <div class="lm-empty"><i class="ti ti-search-off"></i> {t('migrate.noneFound')}</div>
    {:else}
      <!-- Перемикачі між знайденими лаунчерами -->
      <div class="lm-tabs">
        {#each launchers as l (l.kind)}
          <button
            class="lm-tab"
            class:active={activeTab === l.kind}
            onclick={() => activeTab = l.kind}
            disabled={importing}
          >
            <span class="lm-tab-ic">
              {#if l.kind === 'prism'}
                <img src={prismIcon} alt="" />
              {:else if l.kind === 'modrinth'}
                <img src={modrinthIcon} alt="" />
              {:else}
                <i class="ti ti-box"></i>
              {/if}
            </span>
            <span class="lm-tab-name">{l.name}</span>
            <span class="lm-tab-count">{l.packs.length}</span>
          </button>
        {/each}
      </div>

      {#if activeLauncher}
        <div class="lm-toolbar">
          <span class="lm-toolbar-hint">
            {t('migrate.selectedHint', { n: selectedCount(activeTab), total: activeLauncher.packs.length })}
          </span>
          <button class="btn btn-ghost btn-sm" onclick={() => toggleAll(activeTab)} disabled={importing}>
            <i class="ti {allSelected(activeTab) ? 'ti-square-off' : 'ti-square-check'}"></i>
            {allSelected(activeTab) ? t('migrate.selectNone') : t('migrate.selectAll')}
          </button>
        </div>

        <div class="lm-grid">
          {#each activeLauncher.packs as p (p.id)}
            <div class="lm-card" class:sel={selected[activeTab]?.has(p.id)} class:imported={p.alreadyImported} onclick={() => togglePack(activeTab, p.id)}>
              <button
                type="button"
                role="checkbox"
                aria-checked={selected[activeTab]?.has(p.id) ?? false}
                class="lm-check"
                class:checked={selected[activeTab]?.has(p.id) ?? false}
                disabled={importing}
                onclick={(e: any) => { e.stopPropagation(); togglePack(activeTab, p.id) }}
              >
                <i class="ti ti-check"></i>
              </button>
              <span class="lm-card-ic" style="--pcol:{['#ff8a00','#9d7cff','#10b981','#3b82f6','#f43f5e'][p.id.split('').reduce((a,c)=>a+c.charCodeAt(0),0)%5]}">
                {#if iconCache[p.id]}
                  <img src={iconCache[p.id]} alt="" loading="lazy" />
                {:else}
                  <i class="ti ti-package"></i>
                {/if}
              </span>
              <span class="lm-card-body">
                <span class="lm-card-name" title={p.name}>{p.name}</span>
                <span class="lm-card-meta">
                  {#if p.alreadyImported}
                    <span class="lm-imported-badge"><i class="ti ti-circle-check"></i> {t('migrate.alreadyImported')}</span>
                  {/if}
                  {#if p.mcVersion}<span class="chip"><i class="ti ti-cube"></i> {p.mcVersion}</span>{/if}
                  {#if p.loader && p.loader !== 'vanilla'}
                    <span class="chip chip-l"><i class="ti ti-brand-git"></i> {p.loader}{p.loaderVersion ? ' ' + p.loaderVersion : ''}</span>
                  {/if}
                  {#if p.sizeBytes}<span class="chip"><i class="ti ti-database"></i> {fmtSize(p.sizeBytes)}</span>{/if}
                  {#if p.hasConfig}<span class="chip chip-v"><i class="ti ti-settings"></i>{t('migrate.withSettings')}</span>{/if}
                </span>
              </span>
              <span class="lm-card-side">
                {#if importing && progress[p.id]}
                  <span class="lm-pct mono">{pct(progress[p.id])}%</span>
                {:else if progress[p.id]?.stage === 'done'}
                  <i class="ti ti-circle-check" style="color:var(--green)"></i>
                {:else if progress[p.id]?.error}
                  <i class="ti ti-circle-x" style="color:#ef4444" title={progress[p.id].error}></i>
                {:else}
                  <span class="chip chip-dim"><i class="ti ti-arrow-down"></i> {t('migrate.importShort')}</span>
                {/if}
              </span>
              {#if importing && progress[p.id] && progress[p.id].filesTotal > 0}
                <div class="lm-bar"><div class="lm-bar-fill" style="width:{pct(progress[p.id])}%"></div></div>
              {/if}
            </div>
          {/each}
        </div>

      {/if}
    {/if}
</UiModal>

{#snippet foot()}
  {#if done}
    <span class="lm-done"><i class="ti ti-circle-check"></i> {t('migrate.done')}</span>
    <button class="btn btn-primary" onclick={close}><i class="ti ti-check"></i> {t('migrate.finish')}</button>
  {:else}
    <span class="lm-foot-hint"><i class="ti ti-shield-check"></i> {t('migrate.copyHint')}</span>
    <button class="btn btn-ghost" onclick={close} disabled={importing}>{t('migrate.cancel')}</button>
    <button class="btn btn-primary" onclick={doImport} disabled={importing || selectedCount(activeTab) === 0}>
      {#if importing}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-download"></i>{/if}
      {importing ? t('migrate.importing') : t('migrate.import', { n: selectedCount(activeTab) })}
    </button>
  {/if}
{/snippet}

<style>

  .lm-tabs { display: flex; gap: 7px; padding: 12px 20px 10px; border-bottom: 1px solid var(--border); }
  .lm-tab {
    display: inline-flex; align-items: center; gap: 7px;
    padding: 7px 13px; border-radius: 999px; border: 1px solid var(--border);
    background: var(--bg-input); color: var(--text-mute);
    font-size: 12px; font-weight: 700; font-family: inherit; cursor: pointer;
    transition: all .15s;
  }
  .lm-tab.active { background: rgba(157,124,255,.12); border-color: rgba(157,124,255,.45); color: var(--violet-l); }
  .lm-tab:disabled { opacity: .55; cursor: default; }
  .lm-tab-count {
    font-size: var(--fs-min); font-weight: 800; padding: 1px 7px; border-radius: 999px;
    background: var(--bg-panel); color: var(--text-dim);
  }
  .lm-tab-ic {
    width: 16px; height: 16px; flex-shrink: 0;
    display: flex; align-items: center; justify-content: center;
  }
  .lm-tab-ic img { width: 100%; height: 100%; object-fit: contain; }
  .lm-tab-ic i { font-size: 14px; color: var(--text-mute); }

  .lm-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 8px; padding: 10px 20px 6px; }
  .lm-toolbar-hint { font-size: 11.5px; color: var(--text-mute); }

  .lm-grid {
    padding: 4px 20px 14px;
    display: grid; grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
    gap: 10px; min-height: 160px; align-content: start;
  }

  /* ── Картка збірки — стиль браузера контенту (cb-card) ── */
  .lm-card {
    position: relative; display: flex; gap: 10px; padding: 12px;
    background: var(--bg-card); border: 1px solid var(--border);
    border-radius: 12px; cursor: pointer; min-width: 0;
    box-shadow: 0 1px 3px rgba(0,0,0,.12);
    transition: transform .18s, border-color .18s, box-shadow .22s, background .18s;
  }
  .lm-card:hover {
    border-color: rgba(157,124,255,.35);
    box-shadow: 0 4px 14px rgba(0,0,0,.2);
    transform: translateY(-1px);
  }
  .lm-card.sel {
    border-color: rgba(157,124,255,.55);
    background: rgba(157,124,255,.07);
    box-shadow: 0 0 0 1px rgba(157,124,255,.25), 0 4px 14px rgba(0,0,0,.2);
  }
  .lm-card.imported { opacity: .85; }

  .lm-check {
    position: absolute; top: 8px; right: 8px; z-index: 2;
    width: 20px; height: 20px; margin: 0; padding: 0;
    border-radius: 6px; border: 1.5px solid var(--border-hi);
    background: var(--bg-input);
    display: flex; align-items: center; justify-content: center;
    color: transparent; cursor: pointer;
    transition: border-color .15s, background .15s, color .12s, transform .12s;
  }
  .lm-check i { font-size: 13px; line-height: 1; }
  .lm-check:hover:not(:disabled) { border-color: var(--violet); transform: scale(1.08); }
  .lm-check.checked {
    background: var(--violet); border-color: var(--violet); color: #fff;
  }
  .lm-check.checked:hover:not(:disabled) { background: color-mix(in srgb, var(--violet) 85%, #fff); }
  .lm-check:disabled { opacity: .5; cursor: default; }

  .lm-card-ic {
    width: 52px; height: 52px; border-radius: 10px; background: var(--bg-panel);
    display: flex; align-items: center; justify-content: center;
    font-size: 22px; color: var(--pcol, var(--text-dim)); overflow: hidden; flex-shrink: 0;
    border: 1px solid var(--border);
  }
  .lm-card-ic:has(img) { color: transparent; }
  .lm-card-ic img { width: 100%; height: 100%; object-fit: cover; }

  .lm-card-body { display: flex; flex-direction: column; gap: 6px; min-width: 0; flex: 1; }
  .lm-card-name {
    font-size: 13px; font-weight: 700; color: var(--text);
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
    padding-right: 18px; /* місце для чекбокса */
  }
  .lm-card-meta { display: flex; align-items: center; gap: 5px; flex-wrap: wrap; }
  .chip {
    display: inline-flex; align-items: center; gap: 4px;
    padding: 2px 8px; border-radius: 999px; font-size: var(--fs-min); font-weight: 700;
    background: var(--bg-input); color: var(--text-mute); border: 1px solid var(--border);
    white-space: nowrap;
  }
  .chip i { font-size: 11px; }
  .chip-l { color: var(--violet-l); background: rgba(157,124,255,.1); border-color: rgba(157,124,255,.22); }
  .chip-v { color: var(--green); background: rgba(16,185,129,.1); border-color: rgba(16,185,129,.22); }
  .chip-dim { color: var(--text-dim); background: var(--bg-panel); border-color: transparent; }

  .lm-card-side { display: flex; align-items: center; justify-content: flex-end; min-width: 34px; }
  .lm-pct { font-size: 11px; font-weight: 800; color: var(--orange); }
  .lm-imported-badge {
    display: inline-flex; align-items: center; gap: 4px;
    padding: 2px 9px; border-radius: 999px;
    font-size: var(--fs-min); font-weight: 800; letter-spacing: .2px;
    color: var(--green); background: rgba(16,185,129,.12); border: 1px solid rgba(16,185,129,.32);
    white-space: nowrap;
  }
  .lm-imported-badge i { font-size: 12px; }

  .lm-bar { height: 3px; border-radius: 2px; background: var(--bg-input-hi); overflow: hidden; position: absolute; left: 12px; right: 12px; bottom: 6px; }
  .lm-bar-fill { height: 100%; border-radius: 2px; background: linear-gradient(90deg, var(--orange), var(--violet)); transition: width .25s ease; }

  .lm-foot-hint { flex: 1; font-size: 11px; color: var(--text-dim); display: flex; align-items: center; gap: 6px; }
  .lm-foot-hint i { color: var(--green); }
  .lm-done { flex: 1; display: flex; align-items: center; gap: 7px; font-size: 12.5px; font-weight: 700; color: var(--green); }

  .lm-empty {
    display: flex; align-items: center; justify-content: center; gap: 9px;
    padding: 48px 20px; font-size: 13px; color: var(--text-mute);
  }
  .lm-empty i { font-size: 22px; color: var(--text-dim); }
  .lm-empty.lm-err { color: #ef4444; }

  .mono { font-family: var(--font-mono); }
</style>
