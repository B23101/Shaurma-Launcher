<script lang="ts">
  import { tip as launcherTip } from './friends/tooltip'

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

  import { untrack } from 'svelte'
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
  let loading = $state((initialLaunchers ?? []).length === 0)
  let scanError = $state('')
  let activeTab = $state<string>('')
  let selected = $state<Record<string, Set<string>>>({}) // launcherKind -> packId set
  // progress: packId -> MigrateImportProgress (активний імпорт).
  let progress = $state<Record<string, MigrateImportProgress>>({})
  let importing = $state(false)
  let done = $state(false)
  let unsubs: Array<() => void> = []

  // Тіло в untrack: applyLaunchers/scan читають і перезаписують launchers,
  // selected, activeTab — без untrack effect підписується на них і
  // перезапускається безкінечно (нескінченне «Сканування…»).
  $effect(() => {
    if (open) {
      untrack(() => {
        if ((initialLaunchers ?? []).length > 0) applyLaunchers(initialLaunchers)
        else scan()
      })
    }
    return () => { for (const u of unsubs) u(); unsubs = [] }
  })

  function applyLaunchers(list: MigrationLauncher[]) {
    loading = false; scanError = ''; done = false
    launchers = normalizeLaunchers(list)
    if (launchers.length > 0 && !launchers.some((l: MigrationLauncher) => l.kind === activeTab)) {
      activeTab = launchers[0].kind
    }
    for (const l of launchers) {
      if (!selected[l.kind]) selected[l.kind] = new Set()
    }
    subscribeProgress()
  }

  // Go віддає порожній слайс як null: без нормалізації `l.packs.length`
  // падає з «Cannot read properties of null (reading 'length')».
  function normalizeLaunchers(list: MigrationLauncher[] | null | undefined): MigrationLauncher[] {
    return (list ?? [])
      .filter((l: MigrationLauncher) => l && l.found)
      .map((l: MigrationLauncher) => ({ ...l, packs: l.packs ?? [] }))
  }

  async function scan() {
    loading = true; scanError = ''; done = false
    try {
      const res = await App.MigrateScan()
      launchers = normalizeLaunchers(res?.launchers)
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
    if (mb < 1) return '<1 MB'
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
    const packs = launcher.packs ?? []
    untrack(() => { for (const p of packs) iconFor(p) })
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
         width="min(620px, 94vw)" maxHeight="86vh" bodyClass="no-pad"
         footer={!loading && !scanError && activeLauncher ? foot : null}>

    {#if loading}
      <div class="lm-empty"><i class="ti ti-loader spin"></i> {t('migrate.scanning')}</div>
    {:else if scanError}
      <div class="lm-empty lm-err"><i class="ti ti-alert-triangle"></i> {scanError}</div>
    {:else if launchers.length === 0}
      <div class="lm-empty"><i class="ti ti-search-off"></i> {t('migrate.noneFound')}</div>
    {:else}
      <!-- Липка шапка: вкладки лаунчерів + лічильник вибору -->
      <div class="lm-head">
        <div class="lm-tabs" role="tablist">
          {#each launchers as l (l.kind)}
            <button
              role="tab"
              aria-selected={activeTab === l.kind}
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
            <span class="lm-hint" class:has={selectedCount(activeTab) > 0}>
              {t('migrate.selectedHint', { n: selectedCount(activeTab), total: activeLauncher.packs.length })}
            </span>
            <button class="lm-all" onclick={() => toggleAll(activeTab)} disabled={importing}>
              <i class="ti {allSelected(activeTab) ? 'ti-square-off' : 'ti-square-check'}"></i>
              {allSelected(activeTab) ? t('migrate.selectNone') : t('migrate.selectAll')}
            </button>
          </div>
        {/if}
      </div>

      {#if activeLauncher}
        <div class="lm-list">
          {#each activeLauncher.packs as p (p.id)}
            {@const isSel = selected[activeTab]?.has(p.id) ?? false}
            {@const pr = progress[p.id]}
            <div class="lm-row" class:sel={isSel} class:imported={p.alreadyImported} class:busy={importing} role="presentation"
                 onclick={() => { if (!importing) togglePack(activeTab, p.id) }}>
              <button
                type="button"
                role="checkbox"
                aria-checked={isSel}
                aria-label={p.name}
                class="lm-check"
                class:checked={isSel}
                disabled={importing}
                onclick={(e: any) => { e.stopPropagation(); togglePack(activeTab, p.id) }}
              >
                <i class="ti ti-check"></i>
              </button>

              <span class="lm-ic" style="--pcol:{['#ff8a00','#9d7cff','#10b981','#3b82f6','#f43f5e'][p.id.split('').reduce((a,c)=>a+c.charCodeAt(0),0)%5]}">
                {#if iconCache[p.id]}
                  <img src={iconCache[p.id]} alt="" loading="lazy" />
                {:else}
                  <i class="ti ti-package"></i>
                {/if}
              </span>

              <span class="lm-main">
                <span class="lm-name" use:launcherTip={p.name}>{p.name}</span>
                <span class="lm-meta">
                  {#if p.mcVersion}<span class="lm-m"><i class="ti ti-cube"></i>{p.mcVersion}</span>{/if}
                  {#if p.loader && p.loader !== 'vanilla'}
                    <span class="lm-loader">{p.loader}{p.loaderVersion ? ' ' + p.loaderVersion : ''}</span>
                  {/if}
                  {#if p.sizeBytes}<span class="lm-m"><i class="ti ti-database"></i>{fmtSize(p.sizeBytes)}</span>{/if}
                  {#if p.hasConfig}<span class="lm-m ok"><i class="ti ti-settings"></i>{t('migrate.withSettings')}</span>{/if}
                </span>
              </span>

              <span class="lm-side">
                {#if importing && pr && pr.stage !== 'done' && !pr.error}
                  <span class="lm-pct mono">{pct(pr)}%</span>
                {:else if pr?.stage === 'done'}
                  <i class="ti ti-circle-check lm-ok-ic" aria-hidden="true"></i>
                {:else if pr?.error}
                  <i class="ti ti-circle-x lm-err-ic" use:launcherTip={pr.error}></i>
                {:else if p.alreadyImported}
                  <span class="lm-imported-badge"><i class="ti ti-circle-check"></i>{t('migrate.alreadyImported')}</span>
                {/if}
              </span>

              {#if importing && pr && pr.filesTotal > 0}
                <span class="lm-bar"><span class="lm-bar-fill" style="width:{pct(pr)}%"></span></span>
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
  /* ── Шапка (липка): вкладки + лічильник ── */
  .lm-head {
    position: sticky; top: 0; z-index: 3;
    padding: 14px 20px 12px; background: var(--bg-panel);
    border-bottom: 1px solid var(--border);
  }

  /* Сегментований перемикач лаунчерів */
  .lm-tabs {
    display: flex; gap: 4px; padding: 4px;
    background: var(--bg-input); border: 1px solid var(--border); border-radius: 14px;
  }
  .lm-tab {
    flex: 1 1 0; min-width: 0;
    display: inline-flex; align-items: center; justify-content: center; gap: 8px;
    padding: 8px 12px; border-radius: 10px; border: 0;
    background: transparent; color: var(--text-mute);
    font-size: 12.5px; font-weight: 700; line-height: 1; font-family: inherit; cursor: pointer;
    transition: background .16s, color .16s, box-shadow .16s;
  }
  .lm-tab:hover:not(:disabled):not(.active) { color: var(--text); background: color-mix(in srgb, var(--text) 5%, transparent); }
  .lm-tab.active {
    color: var(--text);
    background: color-mix(in srgb, var(--orange) 13%, var(--bg-card));
    box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--orange) 45%, transparent);
  }
  .lm-tab:disabled { opacity: .55; cursor: default; }
  .lm-tab:focus-visible { outline: 2px solid var(--orange); outline-offset: 1px; }
  .lm-tab-ic { width: 18px; height: 18px; flex: none; display: flex; align-items: center; justify-content: center; }
  .lm-tab-ic img { width: 100%; height: 100%; object-fit: contain; }
  .lm-tab-ic i { font-size: 15px; color: var(--text-mute); }
  .lm-tab-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .lm-tab-count {
    flex: none; min-width: 20px; padding: 2px 7px; border-radius: 999px; text-align: center;
    font-size: var(--fs-min); font-weight: 800; background: var(--bg-input-hi); color: var(--text-dim);
  }
  .lm-tab.active .lm-tab-count { background: var(--orange); color: #111; }

  .lm-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 10px; margin-top: 12px; }
  .lm-hint { font-size: 12px; font-weight: 600; color: var(--text-dim); transition: color .15s; }
  .lm-hint.has { color: var(--orange); }
  .lm-all {
    display: inline-flex; align-items: center; gap: 6px; padding: 6px 11px; border-radius: 9px;
    border: 1px solid var(--border-hi); background: transparent; color: var(--text-mute);
    font-size: 12px; font-weight: 700; line-height: 1; font-family: inherit; cursor: pointer; transition: color .15s, border-color .15s, background .15s;
  }
  .lm-all i { font-size: 14px; }
  .lm-all:hover:not(:disabled) { color: var(--orange); border-color: color-mix(in srgb, var(--orange) 55%, transparent); background: color-mix(in srgb, var(--orange) 8%, transparent); }
  .lm-all:disabled { opacity: .5; cursor: default; }
  .lm-all:focus-visible { outline: 2px solid var(--orange); outline-offset: 2px; }

  /* ── Список: один рядок = одна збірка ── */
  .lm-list { display: flex; flex-direction: column; gap: 8px; padding: 14px 20px 18px; min-height: 160px; }

  .lm-row {
    position: relative; overflow: hidden;
    display: grid; grid-template-columns: 22px 48px minmax(0, 1fr) auto; align-items: center; column-gap: 14px;
    padding: 12px 16px 12px 14px; border-radius: 14px;
    background: var(--bg-card); border: 1px solid var(--border);
    cursor: pointer; transition: border-color .16s, background .16s, box-shadow .16s;
  }
  .lm-row::before {
    content: ''; position: absolute; left: 0; top: 10px; bottom: 10px; width: 3px; border-radius: 0 3px 3px 0;
    background: var(--orange); opacity: 0; transform: scaleY(.4); transition: opacity .16s, transform .2s;
  }
  .lm-row:hover:not(.busy) { border-color: var(--border-hi); background: color-mix(in srgb, var(--text) 3%, var(--bg-card)); }
  .lm-row.sel {
    border-color: color-mix(in srgb, var(--orange) 50%, transparent);
    background: color-mix(in srgb, var(--orange) 8%, var(--bg-card));
  }
  .lm-row.sel::before { opacity: 1; transform: none; }
  .lm-row.imported:not(.sel) { opacity: .72; }
  .lm-row.imported:not(.sel):hover { opacity: 1; }
  .lm-row.busy { cursor: default; }

  .lm-check {
    width: 22px; height: 22px; margin: 0; padding: 0; border-radius: 7px;
    border: 1.5px solid var(--border-hi); background: var(--bg-input); color: transparent;
    display: flex; align-items: center; justify-content: center; cursor: pointer;
    transition: border-color .15s, background .15s, color .12s, transform .15s;
  }
  .lm-check i { font-size: 14px; line-height: 1; transform: scale(.5); transition: transform .18s cubic-bezier(.34, 1.56, .64, 1); }
  .lm-check:hover:not(:disabled) { border-color: var(--orange); }
  .lm-check.checked { background: var(--orange); border-color: var(--orange); color: #111; }
  .lm-check.checked i { transform: none; }
  .lm-check:disabled { opacity: .55; cursor: default; }
  .lm-check:focus-visible { outline: 2px solid var(--orange); outline-offset: 2px; }

  .lm-ic {
    width: 48px; height: 48px; border-radius: 12px; overflow: hidden; flex: none;
    display: flex; align-items: center; justify-content: center; font-size: 22px;
    color: var(--pcol, var(--text-dim));
    background: color-mix(in srgb, var(--pcol, var(--text-dim)) 14%, var(--bg-panel));
    border: 1px solid color-mix(in srgb, var(--pcol, var(--text-dim)) 25%, var(--border));
  }
  .lm-ic:has(img) { color: transparent; background: var(--bg-panel); border-color: var(--border); }
  .lm-ic img { width: 100%; height: 100%; object-fit: cover; image-rendering: auto; }

  .lm-main { display: flex; flex-direction: column; gap: 5px; min-width: 0; }
  .lm-name { font-size: 14px; font-weight: 700; color: var(--text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .lm-meta { display: flex; flex-wrap: wrap; align-items: center; gap: 4px 12px; font-size: 11.5px; color: var(--text-mute); }
  .lm-m { display: inline-flex; align-items: center; gap: 5px; white-space: nowrap; }
  .lm-m i { font-size: 13px; color: var(--text-dim); }
  .lm-m.ok, .lm-m.ok i { color: var(--green); }
  .lm-loader {
    padding: 1px 8px; border-radius: 999px; white-space: nowrap; font-weight: 700;
    color: var(--violet-l); background: rgba(157,124,255,.12); border: 1px solid rgba(157,124,255,.24);
  }

  .lm-side { display: flex; align-items: center; justify-content: flex-end; min-width: 0; }
  .lm-pct { font-size: 12px; font-weight: 800; color: var(--orange); }
  .lm-ok-ic { font-size: 22px; color: var(--green); }
  .lm-err-ic { font-size: 22px; color: #ef4444; }
  .lm-imported-badge {
    display: inline-flex; align-items: center; gap: 4px; padding: 3px 9px; border-radius: 999px; white-space: nowrap;
    font-size: var(--fs-min); font-weight: 800; letter-spacing: .2px;
    color: var(--green); background: rgba(16,185,129,.12); border: 1px solid rgba(16,185,129,.3);
  }
  .lm-imported-badge i { font-size: 12px; }

  .lm-bar { position: absolute; left: 0; right: 0; bottom: 0; height: 3px; background: var(--bg-input-hi); }
  .lm-bar-fill { display: block; height: 100%; background: linear-gradient(90deg, var(--orange), var(--violet-l)); transition: width .25s ease; }

  /* Вузьке вікно */
  @media (max-width: 520px) {
    .lm-row { grid-template-columns: 22px 42px minmax(0, 1fr) auto; column-gap: 10px; padding: 10px 12px 10px 10px; }
    .lm-ic { width: 42px; height: 42px; }
    .lm-tab-name { display: none; }
  }
  @media (prefers-reduced-motion: reduce) {
    .lm-row, .lm-row::before, .lm-check, .lm-check i, .lm-bar-fill { transition: none; }
  }

  /* ── Футер ── */
  .lm-foot-hint { flex: 1; font-size: 11px; color: var(--text-dim); display: flex; align-items: center; gap: 6px; }
  .lm-foot-hint i { color: var(--green); flex: none; }
  .lm-done { flex: 1; display: flex; align-items: center; gap: 7px; font-size: 12.5px; font-weight: 700; color: var(--green); }

  .lm-empty {
    display: flex; align-items: center; justify-content: center; gap: 9px;
    padding: 48px 20px; font-size: 13px; color: var(--text-mute);
  }
  .lm-empty i { font-size: 22px; color: var(--text-dim); }
  .lm-empty.lm-err { color: #ef4444; }

  .mono { font-family: var(--font-mono); }
</style>
