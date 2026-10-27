<script lang="ts">
  // ── MigrationPanel: віджет міграції модів у створенні збірки ────────────
  // Переписано з нуля (v2). Стара версія використовувала загальні класи
  // .mig-mini-* з style.css, які були визначені ДВІЧІ у різних місцях
  // файлу — другий блок частково перекривав перший, через що рядки не
  // мали фіксованої сітки і "стрибали"/не влазили залежно від довжини
  // назви мода, наявності бейджа джерела чи версії. Тут все локально
  // (scoped), на CSS grid з фіксованими колонками — нічого поза цим
  // компонентом більше не чіпає верстку.
  //
  // Публічний контракт (props) навмисно 1-в-1 повторює те, що раніше
  // жило прямо в CreatePackPage.svelte, — увесь стан і мережеві виклики
  // лишаються там, цей компонент лише малює.

  import { t } from './i18n.svelte'
  import PackChooser from './PackChooser.svelte'
  import modrinthIcon from '../assets/images/Modrinth_icon.png'
  import curseforgeIcon from '../assets/images/curseforge_icon.png'
  import type { MigrationPack, MigrationItem, ModMigrationReport } from './types'

  let {
    packs,
    packsLoaded,
    source = $bindable(''),
    onSourceChange,
    analyzing,
    cancelling,
    onAnalyze,
    onCancel,
    analyzeDone,
    analyzeTotal,
    live,
    report,
    selected,
    onToggleSelect,
    onToggleAll,
    expanded = $bindable(false),
    submitting = false,
    migratingAfter = false,
  } = $props<{
    packs: MigrationPack[]
    packsLoaded: boolean
    source: string
    onSourceChange: (v: string) => void
    analyzing: boolean
    cancelling: boolean
    onAnalyze: () => void
    onCancel: () => void
    analyzeDone: number
    analyzeTotal: number
    live: MigrationItem[]
    report: ModMigrationReport | null
    selected: Set<string>
    onToggleSelect: (key: string) => void
    onToggleAll: () => void
    expanded?: boolean
    submitting?: boolean
    migratingAfter?: boolean
  }>()

  const sourcePack = $derived(packs.find((p: MigrationPack) => p.packId === source))
  // Джерело: лише збірки з модами (modCount > 0). Вибір — модальним
  // вікном (PackChooser), як і в решті лаунчера, а не select-дропдауном
  // (скарга: «блок міграції в створенні збірки застарів, немає модалки
  // вибору збірки»).
  const sourcePacks = $derived(packs.filter((p: MigrationPack) => p.modCount > 0))
  const hasSources = $derived(sourcePacks.length > 0)

  function countOf(items: MigrationItem[]) {
    const c = { found: 0, notFound: 0, already: 0, update: 0 }
    for (const it of items) {
      if (it.status === 'found') c.found++
      else if (it.status === 'not-found') c.notFound++
      else if (it.status === 'already') c.already++
      else if (it.status === 'update') c.update++
    }
    return c
  }

  const liveCounts = $derived(countOf(live))
  const reportCounts = $derived(countOf(report?.items ?? []))
  const transferable = $derived((report?.items ?? []).filter((it: MigrationItem) => it.status === 'found' || it.status === 'update'))
  const visibleTransferable = $derived(expanded ? transferable : transferable.slice(0, 6))
  const analyzePct = $derived(analyzeTotal ? Math.round((analyzeDone / analyzeTotal) * 100) : 0)

  // Дедуп за sourceFileName: два jar з однаковим ім'ям у різних теках
  // mods/ дали б дублікат ключа для {#each (key)} → краш each_key_duplicate.
  // Останній запис виграє (він же використовується у liveItems/report).
  function dedupe(items: MigrationItem[]): MigrationItem[] {
    const seen = new Map<string, MigrationItem>()
    for (const it of items) seen.set(it.sourceFileName, it)
    return [...seen.values()]
  }
  const liveUnique = $derived(dedupe(live))
  const transferableUnique = $derived(dedupe(transferable))
  const visibleTransferableUnique = $derived(expanded ? transferableUnique : transferableUnique.slice(0, 6))

  function statusIcon(status: string): string {
    switch (status) {
      case 'found': return 'ti-circle-check'
      case 'update': return 'ti-refresh'
      case 'already': return 'ti-circle-check'
      default: return 'ti-circle-x'
    }
  }
</script>

<div class="card">
  <div class="card-head">
    <div class="ci orange"><i class="ti ti-arrows-right-left"></i></div>
    <div class="ctw">
      <div class="ct">{t('mods.migrateTitle')}</div>
      <div class="cd">{t('mods.migrateAnalyzeHint')}</div>
    </div>
  </div>

  <div class="mp-body">
    <!-- Рядок вибору джерела: кнопка + модальне вікно (PackChooser),
         як і решта вибору збірок у лаунчері. -->
    <div class="mp-row-src">
      <div class="mp-label">
        <span class="mp-label-name">{t('mods.migrateFrom')}</span>
      </div>
      {#if !packsLoaded}
        <div class="mp-src-loading"><i class="ti ti-loader spin"></i> …</div>
      {:else if !hasSources}
        <div class="mp-src-empty">{t('mods.migrateEmptySource')}</div>
      {:else}
        <PackChooser
          packs={sourcePacks}
          value={source}
          onchange={onSourceChange}
          disabled={analyzing || submitting}
          placeholder={t('mods.migrateFrom')}
          ariaLabel={t('mods.migrateFrom')}
        />
      {/if}
    </div>

    <!-- Рядок дій -->
    <div class="mp-actions">
      <button class="btn btn-primary" onclick={onAnalyze} disabled={!source || analyzing || submitting}>
        {#if analyzing}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-search"></i>{/if}
        {t('mods.migrateAnalyze')}
      </button>
      {#if analyzing}
        <button class="btn btn-ghost btn-danger" onclick={onCancel} disabled={cancelling}>
          {#if cancelling}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-square"></i>{/if}
          {t('mods.migrateCancel')}
        </button>
      {/if}
    </div>

    {#if analyzing && !report}
      <!-- ── Живий прогрес аналізу ── -->
      <div class="mp-live">
        <div class="mp-live-top">
          <i class="ti ti-radar"></i>
          <span class="mp-live-status">{t('mods.migrateAnalyzing', { done: analyzeDone, total: analyzeTotal || (sourcePack?.modCount ?? '?') })}</span>
          <span class="mp-live-remaining">{t('mods.migrateRemaining', { n: Math.max(0, analyzeTotal - analyzeDone) })}</span>
          <span class="mp-live-pct mono">{analyzePct}%</span>
        </div>
        <div class="mp-bar"><div class="mp-bar-fill" style="width:{analyzePct}%"></div></div>

        {#if live.length > 0}
          <div class="mp-summary">
            <span class="mp-chip found"><i class="ti ti-circle-check"></i>{liveCounts.found}</span>
            <span class="mp-chip upd"><i class="ti ti-refresh"></i>{liveCounts.update}</span>
            <span class="mp-chip alr"><i class="ti ti-circle-check"></i>{liveCounts.already}</span>
            <span class="mp-chip nf"><i class="ti ti-circle-x"></i>{liveCounts.notFound}</span>
          </div>

          <div class="mp-list">
            {#each liveUnique as it (it.sourceFileName)}
              <div class="mp-item">
                <div class="mp-item-icon">
                  {#if it.iconUrl}<img src={it.iconUrl} alt="" loading="lazy" />{:else}<i class="ti ti-puzzle"></i>{/if}
                  {#if it.source === 'modrinth'}
                    <span class="mp-badge" title="Modrinth"><img src={modrinthIcon} alt="Modrinth" loading="lazy" /></span>
                  {:else if it.source === 'curseforge'}
                    <span class="mp-badge" title="CurseForge"><img src={curseforgeIcon} alt="CurseForge" loading="lazy" /></span>
                  {/if}
                </div>
                <span class="mp-item-name" title={it.resolvedName || it.sourceName}>{it.resolvedName || it.sourceName}</span>
                <span class="mp-item-ver"></span>
                <span class="mp-item-status st-{it.status}">
                  <i class="ti {statusIcon(it.status)}"></i>
                </span>
              </div>
            {/each}
          </div>
        {/if}
      </div>
    {:else if report}
      <!-- ── Готовий звіт ── -->
      <div class="mp-summary">
        <span class="mp-chip found"><i class="ti ti-circle-check"></i>{reportCounts.found}</span>
        <span class="mp-chip upd"><i class="ti ti-refresh"></i>{reportCounts.update}</span>
        <span class="mp-chip alr"><i class="ti ti-circle-check"></i>{reportCounts.already}</span>
        <span class="mp-chip nf"><i class="ti ti-circle-x"></i>{reportCounts.notFound}</span>
      </div>

      {#if report.cancelled}
        <div class="mp-hint warn"><i class="ti ti-alert-triangle"></i> {t('mods.migrateCancelledHint')}</div>
      {/if}

      {#if transferable.length > 0}
        <div class="mp-tools">
          <span class="mp-tools-hint">{t('mods.migrateSelectedHint', { n: selected.size, total: transferable.length })}</span>
          <button class="btn btn-ghost btn-sm" onclick={onToggleAll} disabled={submitting || migratingAfter}>
            <i class="ti {selected.size === transferable.length ? 'ti-square-off' : 'ti-square-check'}"></i>
            {selected.size === transferable.length ? t('mods.migrateSelectNone') : t('mods.migrateSelectAll')}
          </button>
        </div>

        <div class="mp-list">
          {#each visibleTransferableUnique as it (it.sourceFileName)}
            <div class="mp-item selectable" class:sel={selected.has(it.sourceFileName)} onclick={() => onToggleSelect(it.sourceFileName)}>
              <button
                type="button"
                role="checkbox"
                aria-checked={selected.has(it.sourceFileName)}
                class="mp-check"
                class:checked={selected.has(it.sourceFileName)}
                disabled={submitting || migratingAfter}
                onclick={(e: any) => { e.stopPropagation(); onToggleSelect(it.sourceFileName) }}
              >
                <i class="ti ti-check"></i>
              </button>
              <div class="mp-item-icon">
                {#if it.iconUrl}<img src={it.iconUrl} alt="" loading="lazy" />{:else}<i class="ti ti-puzzle"></i>{/if}
                {#if it.source === 'modrinth'}
                  <span class="mp-badge" title="Modrinth"><img src={modrinthIcon} alt="Modrinth" loading="lazy" /></span>
                {:else if it.source === 'curseforge'}
                  <span class="mp-badge" title="CurseForge"><img src={curseforgeIcon} alt="CurseForge" loading="lazy" /></span>
                {/if}
              </div>
              <span class="mp-item-name" title={it.resolvedName || it.sourceName}>{it.resolvedName || it.sourceName}</span>
              <span class="mp-item-ver mono">{it.resolvedVersion}</span>
              <span class="mp-item-status st-{it.status}">
                <i class="ti {it.status === 'found' ? 'ti-circle-check' : 'ti-refresh'}"></i>
              </span>
            </div>
          {/each}

          {#if transferable.length > 6}
            <button class="mp-more" onclick={() => expanded = !expanded}>
              <i class="ti {expanded ? 'ti-chevron-up' : 'ti-chevron-down'}"></i>
              {expanded ? t('mods.migrateCollapse') : t('mods.migrateExpandAll', { n: transferable.length })}
            </button>
          {/if}
        </div>

        <!-- ГАЛОЧКИ «Перенести вибрані» БІЛЬШЕ НЕМА: вибір рядків у списку
             ВЖЕ є підтвердженням (галочка біля мода + «Обрано N із M»), а
             окремий чекбокс зверху лише дублював його і збивав з пантелику
             («навіщо ще раз підтверджувати те, що я вже обрав»).
             Переносимо рівно те, що позначено; не треба нічого переносити —
             знімаєш галочки в списку (або тиснеш «Зняти всі»). -->
      {/if}
    {/if}
  </div>
</div>

<style>
  /* Внутрішній layout картки — незалежний від style.css .card-head тощо. */
  .mp-body { padding: 4px 22px 18px; display: flex; flex-direction: column; gap: 12px; }

  .mp-row-src { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
  .mp-label { display: flex; flex-direction: column; gap: 2px; }
  .mp-label-name { font-size: 12.5px; font-weight: 600; color: var(--text); }
  /* Дропдаун джерела: розтягується на вільний простір рядка (не довгий
     список збірок, що змушував сторінку ставати дуже довгою). */
  /* Кнопка вибору збірки (PackChooser) розтягується на вільний простір. */
  .mp-row-src :global(.pc-trigger) { flex: 1 1 0; min-width: 0; }

  .mp-actions { display: flex; align-items: center; gap: 8px; }

  /* ── Прогрес-бар аналізу ── */
  .mp-live { display: flex; flex-direction: column; gap: 8px; }
  .mp-live-top { display: grid; grid-template-columns: auto 1fr auto auto; align-items: center; column-gap: 8px; font-size: 12px; color: var(--text-mute); }
  .mp-live-top > i { color: var(--orange); font-size: 14px; }
  .mp-live-status { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .mp-live-remaining { font-size: 10.5px; color: var(--text-dim); white-space: nowrap; }
  .mp-live-pct { font-size: 10.5px; font-weight: 800; color: var(--orange); white-space: nowrap; }

  /* Прогрес-бар: fill гарантовано не виходить за межі треку (fix бага "виходить за лінію"). */
  .mp-bar { position: relative; height: 5px; border-radius: 3px; background: var(--bg-input-hi); overflow: hidden; width: 100%; }
  .mp-bar-fill { position: absolute; inset: 0 auto 0 0; height: 100%; max-width: 100%; border-radius: 3px; background: linear-gradient(90deg, var(--orange), var(--violet)); transition: width .25s ease; }

  /* ── Чипи-лічильники ── */
  .mp-summary { display: flex; gap: 6px; flex-wrap: wrap; }
  .mp-chip { display: inline-flex; align-items: center; gap: 4px; font-size: 11px; font-weight: 700; padding: 3px 9px; border-radius: 999px; white-space: nowrap; }
  .mp-chip i { font-size: 12px; }
  .mp-chip.found { background: rgba(16,185,129,.12); color: var(--green); }
  .mp-chip.upd   { background: rgba(255,138,0,.12); color: var(--orange); }
  .mp-chip.alr   { background: rgba(99,102,241,.12); color: var(--violet-l); }
  .mp-chip.nf    { background: rgba(239,68,68,.1);  color: #ef4444; }

  .mp-hint.warn {
    display: flex; align-items: center; gap: 7px; padding: 8px 12px;
    font-size: 11.5px; color: var(--yellow, #eab308); background: rgba(234,179,8,.08);
    border: 1px solid rgba(234,179,8,.25); border-radius: var(--r-sm);
  }

  .mp-tools { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
  .mp-tools-hint { font-size: 11.5px; color: var(--text-mute); }

  /* ── Список модів: фіксована grid-сітка колонок для КОЖНОГО рядка,
     незалежно від того, є чекбокс, версія чи бейдж джерела — нічого
     не з'їжджає і не переповнюється. ── */
  .mp-list { display: flex; flex-direction: column; gap: 5px; max-height: 220px; overflow-y: auto; padding-right: 2px; }

  .mp-item {
    display: grid;
    grid-template-columns: 26px 1fr auto 20px;
    align-items: center;
    column-gap: 9px;
    padding: 6px 9px;
    background: var(--bg-input);
    border: 1px solid var(--border);
    border-radius: var(--r-sm);
    min-width: 0;
    transition: background .15s, border-color .15s;
  }
  .mp-item.selectable {
    grid-template-columns: 18px 26px 1fr auto 20px;
    cursor: pointer;
  }
  .mp-item.selectable:hover { background: var(--bg-input-hi); }
  .mp-item.selectable.sel { border-color: rgba(157,124,255,.4); background: rgba(157,124,255,.08); }

  .mp-check {
    width: 18px; height: 18px; margin: 0; padding: 0;
    border-radius: 6px; border: 1.5px solid var(--border-hi);
    background: var(--bg-input);
    display: flex; align-items: center; justify-content: center;
    color: transparent; cursor: pointer; font-size: 13px; line-height: 1;
    transition: border-color .15s, background .15s, color .12s, transform .12s;
  }
  .mp-check:hover:not(:disabled) { border-color: var(--violet); transform: scale(1.08); }
  .mp-check.checked {
    background: var(--violet); border-color: var(--violet); color: #fff;
  }
  .mp-check.checked:hover:not(:disabled) { background: color-mix(in srgb, var(--violet) 85%, #fff); }
  .mp-check:disabled { opacity: .5; cursor: default; }

  .mp-item-icon {
    position: relative; width: 24px; height: 24px;
    border-radius: 6px; background: var(--bg-panel);
    display: flex; align-items: center; justify-content: center;
    font-size: 12px; color: var(--text-dim);
    overflow: visible;
  }
  .mp-item-icon img { width: 100%; height: 100%; object-fit: cover; border-radius: 6px; }

  .mp-badge {
    position: absolute; right: -5px; bottom: -5px; width: 15px; height: 15px;
    border-radius: 50%; overflow: hidden;
    display: flex; align-items: center; justify-content: center;
    border: 1.5px solid var(--bg-input);
    background: var(--bg-panel);
  }
  .mp-badge img { width: 100%; height: 100%; object-fit: cover; display: block; }

  .mp-item-name {
    min-width: 0; font-size: 11.5px; font-weight: 600; color: var(--text);
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .mp-item-ver {
    font-size: 10px; color: var(--text-mute);
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 90px;
  }
  .mp-item-status { display: flex; align-items: center; justify-content: center; font-size: 13px; }
  .mp-item-status.st-found     { color: var(--green); }
  .mp-item-status.st-update    { color: var(--orange); }
  .mp-item-status.st-already   { color: var(--violet-l); }
  .mp-item-status.st-not-found { color: #ef4444; }

  .mp-more {
    display: flex; align-items: center; justify-content: center; gap: 6px;
    padding: 7px 8px; margin-top: 2px; border: none; background: transparent;
    color: var(--violet); font-size: 11.5px; font-weight: 700; font-family: inherit; cursor: pointer;
    border-radius: var(--r-sm);
  }
  .mp-more:hover { background: var(--bg-input-hi); }

  .mp-after-create {
    display: flex; align-items: center; gap: 8px; margin-top: 4px;
    font-size: 12px; color: var(--text); cursor: pointer; user-select: none;
  }
  .mp-after-create input { accent-color: var(--orange); cursor: pointer; }

  .mono { font-family: var(--font-mono); }
</style>
