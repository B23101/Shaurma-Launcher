<script lang="ts">
  import UiModal from './UiModal.svelte'
  // ── Міграція модів між збірками ─────────────────────────────────────────
  // Модалка з тим самим сценарієм, що ModMigrationScreen у старому
  // лаунчері: «Звідки» → «Куди» → «Аналіз» (Modrinth batch + CurseForge)
  // → звіт з піктограмами та статусами → «Мігрувати» з прогресом.
  //
  // Аналіз має «живу» анімацію: радар-пульс + біжучі рядки зі статусами
  // окремих модів, щоб користувач бачив, що відбувається (не голий
  // прогресс-бар). Після завершення рядки плавно «впадають» на місце.
  //
  // mode='port' — портування модів зі збірки на сервер (AnalyzeModPort):
  // джерело — лише збірки з тією самою версією гри й лоадером, переносяться
  // ті самі jar; клієнтські моди не переносяться, серверні вибрані одразу,
  // а моди з невизначеною стороною вирішує користувач.
  import { onMount } from 'svelte'
  import { t } from './i18n.svelte'
  import { on } from './wails'
  import { toast } from './toast.svelte'
  import { ModMigrationFlow } from './modMigrationFlow.svelte'
  import PackChooser from './PackChooser.svelte'
  import modrinthIcon from '../assets/images/Modrinth_icon.png'
  import curseforgeIcon from '../assets/images/curseforge_icon.png'
  import type { MigrationPack, MigrationItem, ModMigrationResult } from './types'

  let {
    packId = '',       // поточна збірка (вкладка модів) — ціль за замовчуванням
    packName = '',
    mcVersion = '',    // версія/лоадер поточної збірки — ціль аналізу
    loader = '',
    mode = 'migrate',
    onClose = () => {}
  }: {
    packId?: string
    packName?: string
    mcVersion?: string
    loader?: string
    mode?: 'migrate' | 'port'
    onClose?: () => void
  } = $props()

  const isPort = $derived(mode === 'port')

  let targetPackId = $state(packId)

  // Аналіз, живі рядки, вибір модів — спільний стан (modMigrationFlow).
  const flow: ModMigrationFlow = new ModMigrationFlow({
    target: () => targetPackId,
    mcVersion: () => selectedTarget?.mcVersion ?? mcVersion,
    loader: () => selectedTarget?.loader ?? loader,
    mode: () => mode,
  })

  // Міграція
  let migrating = $state(false)
  let migrateDone = $state(0)
  let migrateTotal = $state(0)
  let migrateCurrent = $state('')

  // Джерело НЕ підставляємо — користувач сам обирає, «з якої» збірки
  // копіювати моди. Порожній селект = жодної випадкової міграції.
  onMount(() => { flow.loadPacks(packId) })
  onMount(() => flow.listen())

  // Поточна збірка `packId` БЕКЕНД виключає зі списку (GetMigrationPacks
  // передає її як excludePackID), тож її немає в `packs`. Але вона — ціль
  // міграції за замовчуванням (кнопка «копіювати з іншої збірки» копіює
  // СЮДИ). Формуємо її об'єкт окремо.
  const currentPackT = $derived<MigrationPack>({
    packId, name: packName, mcVersion, loader, iconUrl: '', icon: 'ti-package', color: '', modCount: 0,
  })
  const selectedTarget: MigrationPack | undefined = $derived(flow.packs.find(p => p.packId === targetPackId) ?? (packId === targetPackId ? currentPackT : undefined))

  // ── Вибір збірок — МОДАЛЬНЕ вікно (PackChooser), а не select-дропдаун.
  // Портування: джерело — лише збірки з тією самою версією гри й лоадером.
  const sourceChoices = $derived(flow.packs.filter(p => p.modCount > 0 && p.packId !== targetPackId &&
    (!isPort || (p.kind !== 'server' && p.mcVersion === mcVersion && p.loader.toLowerCase() === loader.toLowerCase()))))
  const targetChoices = $derived.by((): MigrationPack[] => {
    if (isPort) return [currentPackT] // портування — завжди на цей сервер
    const list = flow.packs.filter(p => p.packId !== flow.source)
    if (packId && !list.some(p => p.packId === packId)) list.push(currentPackT)
    return list
  })

  // ── Міграція ──
  async function runMigration() {
    if (!flow.report || migrating) return
    const toMigrate = flow.selectedItems
    if (toMigrate.length === 0) return
    migrating = true
    migrateDone = 0
    migrateTotal = toMigrate.length
    try {
      const res: ModMigrationResult = await flow.migrate(targetPackId)
      if (res.migrated.length > 0) {
        toast(t('mods.migrateDone', { n: res.migrated.length, name: selectedTarget?.name ?? '' }), 'success')
      }
      if (res.failed.length > 0) {
        toast(t('mods.migrateDoneWithErrors', { n: res.migrated.length, total: toMigrate.length }), 'error')
      }
      onClose()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      migrating = false
    }
  }

  const canMigrate = $derived(!!flow.report && flow.selected.size > 0 && !migrating)

  function statusIcon(it: MigrationItem): string {
    switch (it.status) {
      case 'found': return 'ti-circle-check'
      case 'update': return 'ti-refresh'
      case 'already': return 'ti-circle-check'
      default:
        // Проєкт знайдено (Modrinth/CurseForge), але сумісної версії під
        // цю MC/лоадер немає — показуємо інформаційну іконку, а не хрестик
        // «не знайдено взагалі».
        return it.projectId || it.projectSlug ? 'ti-alert-triangle' : 'ti-circle-x'
    }
  }

  function statusLabel(it: MigrationItem): string {
    switch (it.status) {
      case 'found': return t('mods.migrateStatusFound')
      case 'update': return t('mods.migrateStatusUpdate')
      case 'already': return t('mods.migrateStatusAlready')
      default:
        return it.projectId || it.projectSlug
          ? t('mods.migrateNoCompatVersion')
          : t('mods.migrateStatusNotFound')
    }
  }

  onMount(() => on('mods:migrate-progress', (p: any) => {
    if (p?.packId !== targetPackId) return
    migrateDone = p.current ?? migrateDone
    migrateTotal = p.total ?? migrateTotal
    migrateCurrent = p.fileName ?? ''
  }))
</script>

<UiModal
  open={true}
  size="xl"
  title={isPort ? t('mods.port.title') : t('mods.migrateTitle')}
  subtitle={isPort ? t('mods.port.subtitle', { mc: mcVersion, loader }) : ''}
  icon={isPort ? 'ti-server-bolt' : 'ti-arrows-right-left'}
  color="var(--orange)"
  locked={flow.analyzing || migrating}
  onclose={onClose}
>
    <div>
      <!-- ═══ Вибір збірок (компактні SelectField-дропдауни) ═══ -->
      <div class="mig-selectors">
        <div class="mig-sel">
          <label class="mig-sel-label">{t('mods.migrateFrom')}</label>
          <PackChooser
            packs={sourceChoices}
            value={flow.source}
            onchange={(v) => flow.setSource(v)}
            disabled={flow.analyzing || migrating || !flow.packsLoaded}
            placeholder={!flow.packsLoaded ? '…' : (isPort && sourceChoices.length === 0 ? t('mods.port.noSource') : t('mods.migrateEmptySource'))}
            ariaLabel={t('mods.migrateFrom')}
          />
        </div>
        <div class="mig-arrow"><i class="ti ti-arrow-down"></i></div>
        <div class="mig-sel">
          <label class="mig-sel-label">{t('mods.migrateTo')}</label>
          <PackChooser
            packs={targetChoices}
            value={targetPackId}
            onchange={(v) => { targetPackId = v; flow.reset() }}
            disabled={flow.analyzing || migrating || !flow.packsLoaded}
            placeholder={!flow.packsLoaded ? '…' : ''}
            ariaLabel={t('mods.migrateTo')}
            currentId={packId}
          />
        </div>
      </div>

      <!-- ═══ Кнопка аналізу ═══ -->
      <div class="mig-analyze-row">
        <button class="btn btn-primary" onclick={() => flow.analyze()} disabled={!flow.source || !targetPackId || flow.analyzing || migrating}>
          {#if flow.analyzing}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-search"></i>{/if}
          {t('mods.migrateAnalyze')}
        </button>
        {#if flow.analyzing}
          <button class="btn btn-danger" onclick={() => flow.cancel()} disabled={flow.cancelling}>
            {#if flow.cancelling}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-square"></i>{/if}
            {t('mods.migrateCancel')}
          </button>
        {/if}
        <span class="mig-hint">{isPort ? t('mods.port.hint') : t('mods.migrateAnalyzeHint')}</span>
      </div>

      <!-- ═══ Звіт / живі рядки ═══ -->
      {#if flow.analyzing && flow.live.length === 0}
        <div class="mig-radar">
          <div class="mig-radar-ring"></div>
          <div class="mig-radar-ring r2"></div>
          <div class="mig-radar-core"><i class="ti ti-radar"></i></div>
          <div class="mig-radar-text">{t('mods.migrateAnalyzing', { done: flow.analyzeDone, total: flow.analyzeTotal })}</div>
        </div>
      {:else if flow.live.length > 0 || flow.report}
        <!-- Підсумок: чипи лічаться з flow.live і оновлюються в реальному
             часі (found/update/already/notFound), а не лише по завершенню -->
        {#if flow.live.length > 0 && !isPort}
          <div class="mig-summary">
            <span class="mig-sum found"><i class="ti ti-circle-check"></i>{flow.liveCounts.found} {t('mods.migrateStatusFound').toLowerCase()}</span>
            <span class="mig-sum upd"><i class="ti ti-refresh"></i>{flow.liveCounts.update} {t('mods.migrateStatusUpdate').toLowerCase()}</span>
            <span class="mig-sum alr"><i class="ti ti-circle-check"></i>{flow.liveCounts.already} {t('mods.migrateStatusAlready').toLowerCase()}</span>
            <span class="mig-sum nf"><i class="ti ti-circle-x"></i>{flow.liveCounts.notFound} {t('mods.migrateStatusNotFound').toLowerCase()}</span>
          </div>
        {/if}
        <!-- Ціль — сервер: де працює кожен мод (клієнтські не переносяться). -->
        {#if flow.forServer && flow.report && !flow.analyzing}
          <div class="mig-summary">
            <span class="mig-sum found"><i class="ti ti-server-2"></i>{flow.sideCounts.server} {t('mods.port.sideServer')}</span>
            <span class="mig-sum upd"><i class="ti ti-help-circle"></i>{flow.sideCounts.unknown} {t('mods.port.sideUnknown')}</span>
            <span class="mig-sum nf"><i class="ti ti-device-desktop"></i>{flow.sideCounts.client} {t('mods.port.sideClient')}</span>
          </div>
          {#if flow.sideCounts.unknown > 0}
            <div class="mig-cancelled-hint"><i class="ti ti-help-circle"></i> {t('mods.port.unknownHint')}</div>
          {/if}
        {/if}
        {#if flow.report?.cancelled}
          <div class="mig-cancelled-hint"><i class="ti ti-alert-triangle"></i> {t('mods.migrateCancelledHint')}</div>
        {/if}

        {#if flow.analyzing && flow.live.length > 0}
          <div class="mig-live-head">
            <div class="mig-live-head-top">
              <i class="ti ti-radar"></i>
              <span>{t('mods.migrateAnalyzing', { done: flow.analyzeDone, total: flow.analyzeTotal })}</span>
              <span class="mig-live-remaining">{t('mods.migrateRemaining', { n: Math.max(0, flow.analyzeTotal - flow.analyzeDone) })}</span>
              <span class="mig-live-pct mono">{flow.analyzeTotal ? Math.round(flow.analyzeDone / flow.analyzeTotal * 100) : 0}%</span>
            </div>
            <div class="mig-live-bar"><div style="width:{flow.analyzeTotal ? Math.round(flow.analyzeDone / flow.analyzeTotal * 100) : 0}%"></div></div>
          </div>
        {/if}

        {#if !flow.analyzing && flow.report && flow.transferable.length > 0}
          <div class="mig-tools">
            <span class="mig-tools-hint">{t('mods.migrateSelectedHint', { n: flow.selected.size, total: flow.transferable.length })}</span>
            <button class="btn btn-ghost btn-sm" onclick={() => flow.toggleAll()} disabled={migrating}>
              <i class="ti {flow.selected.size === flow.transferable.length ? 'ti-square-off' : 'ti-square-check'}"></i>
              {flow.selected.size === flow.transferable.length ? t('mods.migrateSelectNone') : t('mods.migrateSelectAll')}
            </button>
          </div>
        {/if}

        <div class="mig-list">
          {#each flow.liveUnique as it (it.sourceFileName)}
            {@const canSelect = !flow.analyzing && flow.report && flow.isTransferable(it)}
            <div
              class={'mig-row st-' + it.status}
              class:selectable={canSelect}
              class:sel={canSelect && flow.selected.has(it.sourceFileName)}
              onclick={() => { if (canSelect) flow.toggle(it.sourceFileName) }}
              role={canSelect ? 'checkbox' : undefined}
              aria-checked={canSelect ? flow.selected.has(it.sourceFileName) : undefined}
              tabindex={canSelect ? 0 : undefined}
            >
              {#if canSelect}
                <button
                  type="button"
                  role="checkbox"
                  aria-checked={flow.selected.has(it.sourceFileName)}
                  class="mig-check"
                  class:checked={flow.selected.has(it.sourceFileName)}
                  disabled={migrating}
                  onclick={(e: any) => { e.stopPropagation(); flow.toggle(it.sourceFileName) }}
                >
                  <i class="ti ti-check"></i>
                </button>
              {/if}
              <div class="mig-ic">
                {#if it.iconUrl}<img src={it.iconUrl} alt="" loading="lazy" />
                {:else}<i class="ti ti-puzzle"></i>{/if}
                {#if it.source === 'modrinth'}
                  <span class="mig-src" title="Modrinth"><img src={modrinthIcon} alt="Modrinth" loading="lazy" /></span>
                {:else if it.source === 'curseforge'}
                  <span class="mig-src" title="CurseForge"><img src={curseforgeIcon} alt="CurseForge" loading="lazy" /></span>
                {/if}
              </div>
              <div class="mig-info">
                <div class="mig-name">{it.resolvedName || it.sourceName}</div>
                <div class="mig-meta">
                  {#if it.status === 'not-found'}
                    {#if it.projectId || it.projectSlug}
                      <!-- Проєкт знайдено на Modrinth/CurseForge, але сумісної
                           версії під цю версію гри/лоадер немає — це НЕ те саме,
                           що «мода взагалі немає». -->
                      {t('mods.migrateNoCompatVersion')} · {it.sourceName}
                    {:else}
                      {t('mods.migrateStatusNotFound')} · {it.sourceName}
                    {/if}
                  {:else}
                    <!-- Під час живого аналізу resolvedVersion ще порожній —
                         показуємо назву файла як fallback, потім версію. -->
                    <span class="mig-ver">{it.resolvedVersion || it.sourceFileName}</span>
                    {#if it.status === 'update'}<span class="mig-tag upd">{t('mods.migrateStatusUpdate')}</span>{/if}
                    {#if it.willReplace}<span class="mig-tag rep">{t('mods.migrateReplace')}</span>{/if}
                    {#if it.side}
                      {#if it.side === 'server'}<span class="mig-tag side-srv">{t('mods.port.sideServer')}</span>
                      {:else if it.side === 'client'}<span class="mig-tag side-cl" title={t('mods.port.clientHint')}>{t('mods.port.sideClient')}</span>
                      {:else}<span class="mig-tag side-unk" title={t('mods.port.unknownHint')}>{t('mods.port.sideUnknown')}</span>{/if}
                    {:else if it.localCopy}<span class="mig-tag local" title={t('mods.migrateLocalCopyHint')}>{t('mods.migrateLocalCopy')}</span>{/if}
                  {/if}
                </div>
              </div>
              <div class={'mig-status st-' + it.status}>
                <i class="ti {statusIcon(it)}"></i><span>{statusLabel(it)}</span>
              </div>
            </div>
          {/each}
        </div>
      {:else}
        <div class="mig-empty">
          <i class="ti ti-arrows-right-left"></i>
          <div>{t('mods.migrateAnalyzeHint')}</div>
        </div>
      {/if}

      {#if migrating}
        <div class="mig-progress">
          <div class="mig-progress-bar"><div class="mig-progress-fill" style="width:{migrateTotal ? Math.round(migrateDone / migrateTotal * 100) : 0}%"></div></div>
          <div class="mig-progress-text">{t('mods.migrateRunning', { done: migrateDone, total: migrateTotal })} {migrateCurrent}</div>
        </div>
      {/if}
    </div>

    <div class="modal-foot">
      <button class="btn btn-ghost" onclick={onClose} disabled={flow.analyzing || migrating}>{t('cancel')}</button>
      <button class="btn btn-primary" onclick={runMigration} disabled={!canMigrate}>
        {#if migrating}<i class="ti ti-loader spin"></i>{:else}<i class="ti {isPort ? 'ti-server-bolt' : 'ti-arrows-right-left'}"></i>{/if}
        {isPort ? t('mods.port.run') : t('mods.migrate')}{#if flow.report && flow.transferable.length > 0} ({flow.selected.size}){/if}
      </button>
    </div>
</UiModal>

<style>
  .mig-modal { max-width: 720px; width: 720px; }

  /* ── Селектори збірок ── */
  .mig-selectors { display: flex; align-items: flex-end; gap: 12px; }
  .mig-sel { flex: 1; min-width: 0; }
  .mig-sel-label { display: block; font-size: 11px; font-weight: 700; color: var(--text-mute); margin-bottom: 5px; text-transform: uppercase; letter-spacing: .4px; }
  /* Стрілка між селекторами: центрується ПО ВЕРТИКАЛІ відносно полів
     (не лейблів, не всього осередку) — translateY(-50%) + зсув на висоту
     лейбла, інакше «стрибає»/виглядає криво. */
  .mig-arrow {
    color: var(--text-dim); font-size: 16px; flex-shrink: 0;
    height: 0; display: flex; align-items: center; justify-content: center;
    transform: translateY(-9px);
  }
  /* Корінь SelectField (клас mig-pp на .sf) розтягується в flex-осередку;
     min-width:0 — щоб дропдаун НЕ випирав за рамки в парі двох селекторів. */
  .mig-pp { width: 100%; min-width: 0; }
  .mig-pp :global(.sf-trigger) { width: 100%; min-width: 0; }

  .mig-live-head { display: flex; flex-direction: column; gap: 6px; padding: 12px 4px 8px; font-size: 12px; color: var(--text-mute); }
  .mig-live-head-top { display: flex; align-items: center; gap: 10px; }
  .mig-live-head-top i { color: var(--orange); }
  .mig-live-remaining { font-size: 11px; color: var(--text-dim); }
  .mig-live-pct { margin-left: auto; font-size: 11px; font-weight: 800; color: var(--orange); }
  .mig-live-bar { height: 4px; border-radius: 2px; background: var(--bg-input-hi); overflow: hidden; }
  .mig-live-bar div { height: 100%; border-radius: 2px; background: linear-gradient(90deg, var(--orange), var(--violet)); transition: width .25s ease; }

  .md-close {
    display: flex; align-items: center; justify-content: center;
    width: 30px; height: 30px; border-radius: 8px; flex-shrink: 0;
    background: transparent; border: 1px solid transparent; color: var(--text-mute);
    cursor: pointer; transition: .15s; font-size: 16px;
  }
  .md-close:hover { color: var(--text); background: var(--bg-input-hi); border-color: var(--border-hi); }
  .md-close:disabled { opacity: .4; cursor: default; }

  .mig-analyze-row { display: flex; align-items: center; gap: 12px; margin-top: 14px; }
  .mig-hint { font-size: 11px; color: var(--text-mute); line-height: 1.45; }

  /* ── Радар аналізу ── */
  .mig-radar { position: relative; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 10px; padding: 36px 0 30px; }
  .mig-radar-ring {
    position: absolute; width: 74px; height: 74px; border-radius: 50%;
    border: 2px solid rgba(255,138,0,.35); animation: mig-ping 1.6s cubic-bezier(0,0,.2,1) infinite;
  }
  .mig-radar-ring.r2 { animation-delay: .8s; }
  .mig-radar-core {
    position: relative; width: 44px; height: 44px; border-radius: 50%;
    background: radial-gradient(circle, rgba(255,138,0,.28), rgba(255,138,0,.05));
    border: 1px solid rgba(255,138,0,.4);
    display: flex; align-items: center; justify-content: center; font-size: 20px; color: var(--orange);
    box-shadow: 0 0 24px rgba(255,138,0,.35);
  }
  /* Іконка в кружку радара стояла вище центру: гліфи tabler мають
     вертикальні метрики, зміщені вгору відносно свого рядкового бокса, а
     flex центрує саме бокс. display:block + line-height:1 прибирають зайву
     висоту рядка, translateY(1px) доводить оптичний центр до центру кола. */
  .mig-radar-core :global(.ti) { display: block; line-height: 1; transform: translateY(1px); }
  .mig-radar-text { font-size: 12px; color: var(--text-mute); margin-top: 66px; }
  @keyframes mig-ping { 0% { transform: scale(.4); opacity: 1; } 100% { transform: scale(1.5); opacity: 0; } }

  /* ── Підсумок ── */
  .mig-summary { display: flex; gap: 8px; flex-wrap: wrap; margin: 14px 0 10px; }
  .mig-sum { display: inline-flex; align-items: center; gap: 5px; font-size: 11.5px; font-weight: 700; padding: 4px 10px; border-radius: 999px; }
  .mig-sum i { font-size: 13px; }
  .mig-sum.found { background: rgba(16,185,129,.12); color: var(--green); }
  .mig-sum.upd { background: rgba(255,138,0,.12); color: var(--orange); }
  .mig-sum.alr { background: rgba(99,102,241,.12); color: var(--violet-l); }
  .mig-sum.nf { background: rgba(239,68,68,.1); color: #ef4444; }

  /* ── Список звіту ── */
  .mig-tools { display: flex; align-items: center; justify-content: space-between; gap: 8px; margin-top: 6px; }
  .mig-tools-hint { font-size: 11.5px; color: var(--text-mute); }

  .mig-list { display: flex; flex-direction: column; gap: 6px; max-height: 320px; overflow-y: auto; }
  .mig-row {
    display: flex; align-items: center; gap: 12px; padding: 8px 12px;
    background: var(--bg-input); border: 1px solid var(--border); border-radius: var(--r-sm);
    animation: mig-row-in .25s ease both;
  }
  @keyframes mig-row-in { from { opacity: 0; transform: translateY(6px); } to { opacity: 1; transform: none; } }
  .mig-row.st-found { border-color: rgba(16,185,129,.3); }
  .mig-row.st-update { border-color: rgba(255,138,0,.35); }
  .mig-row.st-already { opacity: .7; }
  .mig-row.st-not-found { opacity: .6; }
  .mig-row.selectable { cursor: pointer; }
  .mig-row.selectable:hover { background: var(--bg-input-hi); }
  .mig-row.selectable.sel { border-color: rgba(157,124,255,.45); background: rgba(157,124,255,.08); }

  .mig-check {
    width: 20px; height: 20px; margin: 0; padding: 0; flex-shrink: 0;
    border-radius: 6px; border: 1.5px solid var(--border-hi);
    background: var(--bg-input);
    display: flex; align-items: center; justify-content: center;
    color: transparent; cursor: pointer; font-size: 13px; line-height: 1;
    transition: border-color .15s, background .15s, color .12s, transform .12s;
  }
  .mig-check:hover:not(:disabled) { border-color: var(--violet); transform: scale(1.08); }
  .mig-check.checked { background: var(--violet); border-color: var(--violet); color: #fff; }
  .mig-check.checked:hover:not(:disabled) { background: color-mix(in srgb, var(--violet) 85%, #fff); }
  .mig-check:disabled { opacity: .5; cursor: default; }

  .mig-ic {
    position: relative; width: 34px; height: 34px; min-width: 34px; border-radius: 9px;
    background: var(--bg-panel); border: 1px solid var(--border);
    display: flex; align-items: center; justify-content: center; font-size: 15px; color: var(--violet-l); overflow: visible;
  }
  .mig-ic img { width: 100%; height: 100%; object-fit: cover; border-radius: 8px; }

  .mig-info { flex: 1; min-width: 0; }
  .mig-name { font-size: 12.5px; font-weight: 700; color: var(--text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .mig-meta { display: flex; align-items: center; gap: 6px; font-size: var(--fs-min); color: var(--text-mute); margin-top: 2px; flex-wrap: wrap; }
  .mig-ver { font-family: var(--font-mono); }
  .mig-tag { font-size: var(--fs-min); font-weight: 700; padding: 1px 6px; border-radius: 4px; }
  .mig-tag.upd { background: rgba(255,138,0,.15); color: var(--orange); }
  .mig-tag.rep { background: rgba(245,158,11,.15); color: var(--yellow); }
  .mig-tag.local { background: rgba(148,163,184,.15); color: var(--muted, #94a3b8); }
  .mig-tag.side-srv { background: rgba(16,185,129,.15); color: var(--green); }
  .mig-tag.side-cl { background: rgba(148,163,184,.15); color: var(--text-mute); }
  .mig-tag.side-unk { background: rgba(245,158,11,.15); color: var(--yellow); }

  .mig-status { display: flex; align-items: center; gap: 5px; font-size: var(--fs-min); font-weight: 600; flex-shrink: 0; }
  .mig-status.st-found { color: var(--green); }
  .mig-status.st-update { color: var(--orange); }
  .mig-status.st-already { color: var(--violet-l); }
  .mig-status.st-not-found { color: var(--text-dim); }

  .mig-empty { display: flex; flex-direction: column; align-items: center; gap: 12px; padding: 42px 0; color: var(--text-mute); font-size: 13px; text-align: center; }
  .mig-empty i { font-size: 52px; color: var(--text-dim); }

  /* ── Прогрес міграції ── */
  .mig-progress { margin-top: 14px; }
  .mig-progress-bar { height: 6px; background: var(--bg-input); border-radius: 999px; overflow: hidden; }
  .mig-progress-fill { height: 100%; background: linear-gradient(90deg, var(--orange), var(--yellow)); border-radius: 999px; transition: width .3s; }
  .mig-progress-text { font-size: 11px; color: var(--text-mute); margin-top: 6px; font-family: var(--font-mono); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
</style>
