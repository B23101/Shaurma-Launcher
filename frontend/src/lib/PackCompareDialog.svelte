<script lang="ts">
  // PackCompareDialog — двосторонній порівнювач модів двох збірок.
  // Обидві сторони РІВНОправні й клікабельні: клік по будь-якій стороні
  // (PackCompareChip) відкриває той самий пікер (PackPicker), лише
  // запам'ятовує, яку зі сторін зараз редагує (pickerSide). Початкова
  // ліва збірка — та, з якої відкрито порівнювач (packId проп), але
  // користувач може підмінити й її — це просто стартове значення
  // leftPackId, а не жорстко зафіксований проп.
  import UiModal from './UiModal.svelte'
  import PackPicker, { type PackPickerItem, type PackRow, type PackFolderRow } from './PackPicker.svelte'
  import PackCompareChip from './PackCompareChip.svelte'
  import { t } from './i18n.svelte'
  import { App } from './wails'
  import { toast } from './toast.svelte'
  import modrinthIcon from '../assets/images/Modrinth_icon.png'
  import curseforgeIcon from '../assets/images/curseforge_icon.png'
  import type { ModComparePack, ModCompareReport, ModCompareRow, ModCompareSide, CompareInstallRequest } from './types'

  let {
    packId,
    packName = '',
    mcVersion = '',
    loader = '',
    iconUrl = '',
    icon = '',
    color = '',
    packPicker = null as null | {
      items: PackPickerItem[]
      rowById: Map<string, PackRow>
      folderById: Map<string, PackFolderRow>
    },
    onClose = () => {},
  }: {
    packId: string
    packName?: string
    mcVersion?: string
    loader?: string
    iconUrl?: string
    icon?: string
    color?: string
    packPicker?: null | {
      items: PackPickerItem[]
      rowById: Map<string, PackRow>
      folderById: Map<string, PackFolderRow>
    }
    onClose?: () => void
  } = $props()

  // Стартові id: ліва — збірка, з якої відкрито порівнювач; права — поки
  // не обрана. Обидві надалі змінюються незалежно через пікер.
  let leftPackId = $state(packId)
  let rightPackId = $state('')
  // Яку сторону зараз редагує відкритий пікер ('left' | 'right').
  let pickerSide = $state<'left' | 'right'>('right')
  let pickerOpen = $state(false)
  let pickerFilter = $state('')

  function packFromRow(id: string): ModComparePack | null {
    if (!id || !packPicker) return null
    const r = packPicker.rowById.get(id)
    if (!r) return null
    return { packId: id, name: r.name, mcVersion: r.mcVersion ?? '', loader: r.loader ?? '', iconUrl: r.iconUrl, icon: r.icon, color: r.color }
  }

  // Ліва сторона стартує з даних, переданих пропами (щоб не блимати
  // порожньою карткою, поки packPicker.rowById довантажиться) — тільки
  // якщо leftPackId досі дорівнює вихідному packId; щойно користувач
  // підміняє ліву збірку через пікер, дані йдуть з packPicker як для
  // правої сторони.
  const leftPack = $derived<ModComparePack | null>(
    leftPackId === packId
      ? { packId, name: packName, mcVersion, loader, iconUrl, icon, color }
      : packFromRow(leftPackId)
  )

  const rightPack = $derived.by((): ModComparePack | null => packFromRow(rightPackId))

  // Пікер не повинен пропонувати збірку, що вже стоїть на ІНШІЙ стороні —
  // порівнювати збірку саму з собою немає сенсу.
  const pickerAllowedIds = $derived.by((): Set<string> | null => {
    if (!packPicker) return null
    const excludeId = pickerSide === 'left' ? rightPackId : leftPackId
    const ids = new Set<string>()
    for (const [id] of packPicker.rowById) if (id !== excludeId) ids.add(id)
    return ids
  })

  function openPicker(side: 'left' | 'right') {
    pickerSide = side
    pickerOpen = true
  }

  let loading = $state(false);
  let report = $state<ModCompareReport | null>(null)
  let err = $state('')
  // Ключі рядків, для яких зараз триває встановлення (окремо left/right,
  // щоб можна було тиснути обидві кнопки одного рядка незалежно).
  let installingKeys = $state<Set<string>>(new Set())

  async function runCompare() {
    if (!leftPackId || !rightPackId || leftPackId === rightPackId || loading) return
    loading = true
    err = ''
    report = null
    try {
      report = await App.CompareModPacks(leftPackId, rightPackId)
    } catch (e: any) {
      err = String(e)
    } finally {
      loading = false
    }
  }

  function pickSide(id: string) {
    const otherId = pickerSide === 'left' ? rightPackId : leftPackId
    if (id === otherId) {
      toast(t('mods.compareSamePack'), 'error')
      return
    }
    if (pickerSide === 'left') leftPackId = id
    else rightPackId = id
    pickerOpen = false
    pickerFilter = ''
    report = null
    err = ''
    runCompare()
  }

  function sideKey(row: ModCompareRow, which: 'left' | 'right'): string {
    return row.key + ':' + which
  }

  function targetPackFor(which: 'left' | 'right'): string {
    return which === 'left' ? leftPackId : rightPackId
  }

  async function installSide(row: ModCompareRow, which: 'left' | 'right') {
    const side: ModCompareSide = which === 'left' ? row.left : row.right
    if (!side.canInstall && !side.localCopy) return
    const key = sideKey(row, which)
    if (installingKeys.has(key)) return
    installingKeys = new Set(installingKeys).add(key)
    try {
      const req: CompareInstallRequest = {
        targetPackId: targetPackFor(which),
        side,
        sourceName: row.name,
        replace: side.status === 'outdated',
      }
      await App.InstallCompareMod(req)
      toast(t('mods.compareInstallDone', { name: row.name }), 'success')
      await runCompare()
    } catch (e: any) {
      toast(t('mods.compareInstallFailed', { name: row.name }) + ': ' + String(e), 'error')
    } finally {
      const next = new Set(installingKeys)
      next.delete(key)
      installingKeys = next
    }
  }

  function sourceIconFor(side: ModCompareSide) {
    if (side.localCopy) return null
    if (side.source === 'modrinth') return modrinthIcon
    if (side.source === 'curseforge') return curseforgeIcon
    return null
  }

  function actionLabel(side: ModCompareSide): string {
    return side.status === 'outdated' ? t('mods.compareUpdateBtn') : t('mods.compareInstallBtn')
  }

  // Колонки незалежні (кожна — свій список, свій стан), але той самий
  // мод у звіті завжди на однаковій позиції зліва й справа (report.rows —
  // спільний порядок для обох) — тому синхронізуємо ВЕРТИКАЛЬНИЙ скрол
  // між колонками, щоб рядки лишались вирівняні одне навпроти одного при
  // прокрутці. Це суто зручність відображення, не зв'язок стану/дій.
  let leftListEl = $state<HTMLDivElement | null>(null)
  let rightListEl = $state<HTMLDivElement | null>(null)
  let syncingScroll = false
  function syncScroll(from: 'left' | 'right') {
    if (syncingScroll || !leftListEl || !rightListEl) return
    syncingScroll = true
    if (from === 'left') rightListEl.scrollTop = leftListEl.scrollTop
    else leftListEl.scrollTop = rightListEl.scrollTop
    syncingScroll = false
  }
</script>

<UiModal
  open={true}
  size="xl"
  title={t('mods.compareTitle')}
  subtitle={leftPack && rightPack ? t('mods.compareSubtitle', { left: leftPack.name, right: rightPack.name }) : ''}
  icon="ti-git-compare"
  color="var(--violet-l)"
  onclose={onClose}
>
  <div class="pcd-body">
    <!-- ═══ Шапка вибору збірок: ОБИДВІ сторони клікабельні й
         рівноправні — клік по будь-якій відкриває той самий пікер
         (openPicker запам'ятовує, яку саме сторону редагувати). ─────── -->
    <div class="pcd-heads">
      <PackCompareChip
        pack={leftPack}
        label={t('mods.compareLeft')}
        clickable={true}
        onclick={() => openPicker('left')}
      />
      <div class="pcd-vs"><i class="ti ti-git-compare"></i></div>
      <PackCompareChip
        pack={rightPack}
        label={t('mods.compareRight')}
        clickable={true}
        onclick={() => openPicker('right')}
      />
    </div>

    {#if report?.sameLoaderVersion}
      <div class="pcd-banner">
        <i class="ti ti-bolt"></i> {t('mods.compareSameLoaderVersion')}
      </div>
    {/if}

    <!-- ═══ Стан: одна зі сторін ще не обрана ═══ -->
    {#if !leftPackId || !rightPackId}
      <div class="pcd-empty">
        <i class="ti ti-git-compare"></i>
        <div>{t('mods.comparePickPack')}</div>
      </div>
    {:else if loading}
      <div class="pcd-loading">
        <i class="ti ti-loader spin"></i>
        <div>{t('mods.compareLoading')}</div>
      </div>
    {:else if err}
      <div class="pcd-empty">
        <i class="ti ti-alert-triangle"></i>
        <div>{t('error.withMessage', { message: err })}</div>
      </div>
    {:else if report}
      {#if report.rows.length === 0}
        <div class="pcd-empty">
          <i class="ti ti-mood-empty"></i>
          <div>{t('mods.compareEmpty')}</div>
        </div>
      {:else}
        <!-- ═══ Підсумкові чипи ═══ -->
        <div class="pcd-summary">
          <span class="pcd-sum same"><i class="ti ti-circle-check"></i>{t('mods.compareSummaryBothSame', { n: report.bothSame })}</span>
          <span class="pcd-sum diff"><i class="ti ti-refresh"></i>{t('mods.compareSummaryDiff', { n: report.diff })}</span>
          <span class="pcd-sum onlyleft"><i class="ti ti-arrow-left"></i>{t('mods.compareSummaryOnlyLeft', { n: report.onlyLeft })}</span>
          <span class="pcd-sum onlyright"><i class="ti ti-arrow-right"></i>{t('mods.compareSummaryOnlyRight', { n: report.onlyRight })}</span>
        </div>

        <!-- ═══ Дві незалежні колонки: кожен мод — окремий віджет у своїй
             колонці зі своїм станом. Ряди синхронізовано по вертикалі за
             тим самим порядком report.rows (спільний indexed scroll), але
             кожна колонка — окремий список карток, а не спільний грід-рядок.
             Перемикання збірки для будь-якої колонки — тільки через чипи
             в шапці (pcd-heads), самі картки списку не клікабельні. ──── -->
        <div class="pcd-cols">
          <div class="pcd-col">
            <div class="pcd-col-head"><i class="ti ti-arrow-left"></i>{leftPack?.name ?? ''}</div>
            <div class="pcd-col-list" bind:this={leftListEl} onscroll={() => syncScroll('left')}>
              {#each report.rows as row (row.key)}
                {@render sideWidget(row, 'left', row.left)}
              {/each}
            </div>
          </div>
          <div class="pcd-col">
            <div class="pcd-col-head"><i class="ti ti-arrow-right"></i>{rightPack?.name ?? ''}</div>
            <div class="pcd-col-list" bind:this={rightListEl} onscroll={() => syncScroll('right')}>
              {#each report.rows as row (row.key)}
                {@render sideWidget(row, 'right', row.right)}
              {/each}
            </div>
          </div>
        </div>
      {/if}
    {/if}
  </div>
</UiModal>

{#snippet sideWidget(row: ModCompareRow, which: 'left' | 'right', side: ModCompareSide)}
  {@const busy = installingKeys.has(sideKey(row, which))}
  <div class="pcd-mcard" class:same={row.sameVersion} class:missing={side.status === 'missing'}>
    <!-- ═══ Рівень 1 (верхній): статус/версія — завжди перше, що видно,
         НЕ заховане в один рядок з назвою мода. ═══ -->
    <div class="pcd-status-row">
      {#if side.status === 'present'}
        <span class="pcd-badge present"><i class="ti ti-check"></i>{side.version || t('mods.compareStatusPresent')}</span>
      {:else if side.status === 'outdated'}
        <span class="pcd-badge outdated"><i class="ti ti-refresh"></i>{side.version}</span>
        <i class="ti ti-arrow-right pcd-arrow"></i>
        <span class="pcd-badge target"><i class="ti ti-tag"></i>{side.resolvedVersion}</span>
      {:else if side.canInstall}
        <span class="pcd-badge missing"><i class="ti ti-minus"></i>{t('mods.compareStatusMissing')}</span>
      {:else if side.compatNote === 'noCompatVersion'}
        <span class="pcd-badge warn" title={t('mods.compareNoCompatVersion')}><i class="ti ti-alert-triangle"></i>{t('mods.compareNoCompatVersion')}</span>
      {:else}
        <span class="pcd-badge dim" title={t('mods.compareNotFound')}><i class="ti ti-circle-x"></i>{t('mods.compareStatusMissing')}</span>
      {/if}
      {#if side.localCopy}<span class="pcd-tag local">{t('mods.compareLocalCopy')}</span>{/if}
    </div>

    <!-- ═══ Рівень 2: іконка + назва мода, і, якщо дію можна виконати,
         кругла іконка-кнопка по центру праворуч (без тексту — статус
         вище вже сказав усе словами). ═══ -->
    <div class="pcd-mod-row">
      <div class="pcd-mod-ic">
        {#if row.iconUrl}<img src={row.iconUrl} alt="" loading="lazy" onerror={(e) => ((e.currentTarget as HTMLElement).style.display = 'none')} />
        {:else}<i class="ti ti-puzzle"></i>{/if}
      </div>
      <div class="pcd-mod-name">
        <div class="pcd-name">{row.name}</div>
        {#if row.author}<div class="pcd-author">{row.author}</div>{/if}
      </div>

      {#if side.status === 'outdated' || (side.status === 'missing' && side.canInstall)}
        <button
          type="button"
          class="pcd-action-btn"
          class:update={side.status === 'outdated'}
          disabled={busy}
          title={actionLabel(side)}
          aria-label={actionLabel(side)}
          onclick={() => installSide(row, which)}
        >
          <!-- Кнопка дії завжди показує ОДНОЗНАЧНУ іконку дії (завантажити/
               оновити), НІКОЛИ значок джерела (Modrinth/CurseForge) — лого
               на самій кнопці читалось як «сплутана» дія, не як «скачати».
               Джерело показуємо окремо, дрібним бейджем біля кнопки. -->
          {#if busy}
            <i class="ti ti-loader spin"></i>
          {:else if side.status === 'outdated'}
            <i class="ti ti-refresh"></i>
          {:else}
            <i class="ti ti-download"></i>
          {/if}
        </button>
        {#if !busy && side.status !== 'outdated' && sourceIconFor(side)}
          <img class="pcd-src-badge" src={sourceIconFor(side)} alt="" title={side.source === 'modrinth' ? 'Modrinth' : 'CurseForge'} />
        {/if}
      {/if}
    </div>
  </div>
{/snippet}

<!-- ═══ Модалка: пікер збірки (ліва чи права — залежно від pickerSide) ═══ -->
{#if pickerOpen}
  <UiModal
    open={true}
    title={t('mods.comparePickPack')}
    icon="ti-target-arrow"
    color="var(--violet-l)"
    width="480px"
    onclose={() => { pickerOpen = false; pickerFilter = '' }}
  >
    {#if packPicker && packPicker.items.length > 4}
      <div class="pcd-modal-search">
        <i class="ti ti-search"></i>
        <input placeholder={t('browser.searchPack')} bind:value={pickerFilter} />
      </div>
    {/if}
    {#if packPicker && packPicker.items.length > 0}
      <PackPicker
        items={packPicker.items}
        rowById={packPicker.rowById}
        folderById={packPicker.folderById}
        query={pickerFilter}
        allowedIds={pickerAllowedIds}
        selected={pickerSide === 'left' ? leftPackId : rightPackId}
        onselect={pickSide}
        subtitle={(r) => `${r.loader ?? ''} · ${r.mcVersion ?? ''}`}
        class="pcd-pp"
      />
    {:else}
      <div class="cb-empty sm"><i class="ti ti-search-off"></i><div>{t('browser.noPackMatch')}</div></div>
    {/if}
  </UiModal>
{/if}

<style>
  .pcd-body { display: flex; flex-direction: column; gap: 14px; min-width: 620px; }

  /* ── Шапка ── */
  .pcd-heads { display: flex; align-items: flex-end; gap: 12px; }
  .pcd-vs {
    color: var(--text-dim); font-size: 16px; flex-shrink: 0;
    height: 32px; display: flex; align-items: center; justify-content: center;
  }

  .pcd-banner {
    display: flex; align-items: center; gap: 8px;
    padding: 8px 12px; border-radius: var(--r-sm);
    background: rgba(99,102,241,.1); color: var(--violet-l);
    font-size: 11.5px; font-weight: 600;
  }

  /* ── Порожні/завантаження стани ── */
  .pcd-empty, .pcd-loading {
    display: flex; flex-direction: column; align-items: center; gap: 12px;
    padding: 42px 0; color: var(--text-mute); font-size: 13px; text-align: center;
  }
  .pcd-empty i, .pcd-loading i { font-size: 44px; color: var(--text-dim); }
  .pcd-loading i { font-size: 32px; color: var(--violet-l); }

  /* ── Підсумок ── */
  .pcd-summary { display: flex; gap: 8px; flex-wrap: wrap; }
  .pcd-sum { display: inline-flex; align-items: center; gap: 5px; font-size: 11.5px; font-weight: 700; padding: 4px 10px; border-radius: 999px; }
  .pcd-sum i { font-size: 13px; }
  .pcd-sum.same { background: rgba(16,185,129,.12); color: var(--green); }
  .pcd-sum.diff { background: rgba(255,138,0,.12); color: var(--orange); }
  .pcd-sum.onlyleft, .pcd-sum.onlyright { background: rgba(99,102,241,.12); color: var(--violet-l); }

  /* ── Дві незалежні колонки: кожна — свій список карток модів ── */
  .pcd-cols { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; min-height: 0; }
  .pcd-col { display: flex; flex-direction: column; gap: 8px; min-width: 0; }
  .pcd-col-head {
    display: flex; align-items: center; gap: 6px;
    font-size: 11px; font-weight: 700; color: var(--text-mute); text-transform: uppercase; letter-spacing: .4px;
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .pcd-col-head i { font-size: 12px; color: var(--text-dim); flex-shrink: 0; }
  .pcd-col-list { display: flex; flex-direction: column; gap: 6px; max-height: 420px; overflow-y: auto; }

  /* ── Картка одного мода в одній колонці: 2 рівні (статус зверху,
       іконка+назва+дія знизу), свій стан, своя кнопка ── */
  .pcd-mcard {
    display: flex; flex-direction: column; gap: 6px;
    padding: 9px 10px; background: var(--bg-input); border: 1px solid var(--border); border-radius: var(--r-sm);
    box-sizing: border-box; transition: border-color .15s, background .15s;
  }
  .pcd-mcard.same { opacity: .75; }
  /* Відсутній мод: підсвічується червоним і пунктиром — одразу видно,
     де є розбіжність, навіть побіжним поглядом по колонці. */
  .pcd-mcard.missing {
    background: color-mix(in srgb, var(--red) 5%, var(--bg-input));
    border: 1px dashed color-mix(in srgb, var(--red) 45%, var(--border));
  }

  .pcd-status-row { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; min-height: 18px; }

  .pcd-mod-row { display: flex; align-items: center; gap: 10px; }

  .pcd-mod-ic {
    width: 34px; height: 34px; min-width: 34px; border-radius: 9px;
    background: var(--bg-panel); border: 1px solid var(--border);
    display: flex; align-items: center; justify-content: center; font-size: 15px; color: var(--violet-l);
    overflow: hidden;
  }
  .pcd-mod-ic img { width: 100%; height: 100%; object-fit: cover; border-radius: 8px; }

  .pcd-mod-name { min-width: 0; flex: 1 1 90px; }
  .pcd-name { font-size: 12.5px; font-weight: 700; color: var(--text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .pcd-author { font-size: var(--fs-min); color: var(--text-mute); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

  .pcd-badge { display: inline-flex; align-items: center; gap: 4px; font-size: var(--fs-min); font-weight: 700; padding: 2px 8px; border-radius: 999px; white-space: nowrap; }
  .pcd-badge.present { background: rgba(16,185,129,.12); color: var(--green); }
  .pcd-badge.outdated { background: rgba(255,138,0,.12); color: var(--orange); }
  .pcd-badge.target { background: rgba(157,124,255,.14); color: var(--violet-l); }
  .pcd-badge.missing { background: color-mix(in srgb, var(--red) 14%, transparent); color: var(--red); }
  .pcd-badge.warn { background: rgba(245,158,11,.12); color: var(--yellow); }
  .pcd-badge.dim { background: color-mix(in srgb, var(--red) 10%, transparent); color: var(--red); opacity: .85; }
  .pcd-arrow { font-size: 11px; color: var(--text-dim); }

  .pcd-tag.local { font-size: var(--fs-min); font-weight: 700; padding: 1px 6px; border-radius: 4px; background: rgba(148,163,184,.15); color: var(--muted, #94a3b8); }

  /* ── Кнопка дії: кругла іконка-кнопка по центру, без тексту — статус/
       версія вже сказані рядком вище (pcd-status-row). Іконка на самій
       кнопці — ЗАВЖДИ однозначна дія (завантажити/оновити), ніколи лого
       джерела — лого сервісу на кнопці читалось як «незрозуміла дія»,
       не як «скачати». ── */
  .pcd-action-btn {
    width: 32px; height: 32px; min-width: 32px; border-radius: 50%;
    display: flex; align-items: center; justify-content: center; flex-shrink: 0;
    border: 1px solid var(--border); background: var(--bg-input-hi); color: var(--text);
    cursor: pointer; font-size: 14px; transition: background .15s, border-color .15s, transform .15s;
  }
  .pcd-action-btn:hover:not(:disabled) { background: var(--violet); border-color: var(--violet); color: #fff; transform: scale(1.06); }
  .pcd-action-btn:active:not(:disabled) { transform: scale(.94); }
  .pcd-action-btn:disabled { opacity: .6; cursor: default; }
  .pcd-action-btn.update:hover:not(:disabled) { background: var(--orange); border-color: var(--orange); }

  /* ── Бейдж джерела: окремо ПІД/ПОРУЧ кнопки дії, дрібний, не заважає
       читати кнопку як «скачати» — лише підказка звідки саме прийде мод. ── */
  .pcd-src-badge {
    width: 15px; height: 15px; border-radius: 4px; object-fit: cover; flex-shrink: 0;
    opacity: .8;
  }

  /* ── Пікер збірки: пошук ── */
  .pcd-modal-search {
    display: flex; align-items: center; gap: 9px;
    margin: -16px -20px 14px; padding: 10px 20px;
    border-bottom: 1px solid var(--border);
    background: color-mix(in srgb, var(--bg-input) 40%, transparent);
  }
  .pcd-modal-search > i { color: var(--text-mute); font-size: 15px; }
  .pcd-modal-search input {
    flex: 1; background: transparent; border: none; outline: none;
    color: var(--text); font: 500 12.5px var(--font-ui);
  }
  .pcd-modal-search input::placeholder { color: var(--text-dim); }

  :global(.pcd-pp) { max-height: 360px; overflow-y: auto; }
</style>