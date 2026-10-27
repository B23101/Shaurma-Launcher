<script lang="ts">
  // ── Порівняння модів двох серверів ─────────────────────────────────────
  //
  // СВІДОМО той самий дизайн, що в порівнянні ЗБІРОК (PackCompareDialog):
  // та сама модалка (UiModal size=xl), той самий акцентний колір
  // (--violet-l), та сама шапка з ДВОМА клікабельними чипами (спільний
  // CompareChip), ті самі пігулки-підсумки та картки рядків. Раніше тут
  // була власна верстка з SelectField замість чипів — через це порівняння
  // серверів виглядало «чужим» поруч із збірками, хоча робить те саме.
  //
  // Дані бекенду (ServerModsCompareResult) природно групові (тільки A /
  // тільки B / в обох), а не по-модні (як у збірок), — тому структура
  // (три групи) лишається серверною, а ОФОРМЛЕННЯ уніфіковане зі збірками.
  //
  // Свідомо БЕЗ встановлення модів з цього вікна (як у порівнянні збірок):
  // для сервера кожен мод ще треба узгодити з ядром/версією, і робити це
  // «однією кнопкою» небезпечно. Тут — діагностика, а встановлення
  // лишається в «Браузері контенту» цього сервера.
  import UiModal from '../../UiModal.svelte'
  import CompareChip from '../../CompareChip.svelte'
  import ServerTargetList from '../ServerTargetList.svelte'
  import { App } from '../../wails'
  import { toast } from '../../toast.svelte'
  import { t } from '../../i18n.svelte'
  import { serverVisual, toBrowserServer, type BrowserServer } from '../serverTargets'
  import type { ServerView, ServerModsCompareResult, ServerModsCompareEntry } from '../../types'

  // Проп називається onclose (а не onClose), бо саме таку назву читає
  // UiModal — інакше модалка не закривалась ні хрестиком, ні кліком по
  // фону (UiModal підставляв свій дефолтний noop), а svelte-check валився
  // на невідомому пропі в <UiModal {onClose}>.
  let {
    serverId = '',
    serverName = '',
    onclose = () => {},
  }: {
    serverId: string
    serverName?: string
    onclose?: () => void
  } = $props()

  let leftId = $state(serverId)
  let rightId = $state('')
  let servers = $state<ServerView[]>([])
  let loadingServers = $state(true)
  let comparing = $state(false)
  let result = $state<ServerModsCompareResult | null>(null)
  let err = $state('')

  // Яку сторону зараз редагує відкритий пікер ('left' | 'right') — той самий
  // патерн, що в PackCompareDialog: обидві сторони рівноправні.
  let pickerSide = $state<'left' | 'right'>('right')
  let pickerOpen = $state(false)

  $effect(() => {
    // Список серверів потрібен лише для вибору сторін; сам список
    // невеликий (десятки записів), тож окремий запит тут дешевий і не
    // потребує протягування даних зі сторінки бібліотеки.
    App.ListServers()
      .then((list: ServerView[]) => { servers = list ?? [] })
      .catch(() => { servers = [] })
      .finally(() => { loadingServers = false })
  })

  const leftServer = $derived(servers.find(s => s.id === leftId) ?? null)
  const rightServer = $derived(servers.find(s => s.id === rightId) ?? null)
  const leftName = $derived(leftServer?.name || serverName || '—')

  const browserServers = $derived(servers.map(toBrowserServer))
  // Пікер не повинен пропонувати сервер, що вже стоїть на ІНШІЙ стороні.
  const pickerServers = $derived(
    browserServers.filter((s: BrowserServer) => s.id !== (pickerSide === 'left' ? rightId : leftId)),
  )

  function chipProps(s: ServerView | null, fallbackName = '') {
    if (!s) return { name: '', meta: '' }
    const vis = serverVisual({
      core: s.core, color: s.color,
      launcherIconData: s.launcherIconData, icon: s.icon, iconData: s.iconData,
    })
    const meta = [s.coreLabel || s.core, s.mcVersion].filter(Boolean).join(' · ')
    return { name: s.name || fallbackName, meta, src: vis.src, ti: vis.ti, color: vis.color }
  }

  const leftChip = $derived(chipProps(leftServer, serverName))
  const rightChip = $derived(chipProps(rightServer))

  async function run() {
    if (!leftId || !rightId || leftId === rightId) return
    comparing = true
    err = ''
    result = null
    try {
      result = await App.CompareServerMods(leftId, rightId)
    } catch (e: any) {
      err = String(e?.message ?? e)
    } finally {
      comparing = false
    }
  }

  function openPicker(side: 'left' | 'right') {
    pickerSide = side
    pickerOpen = true
  }

  function pickSide(id: string) {
    const otherId = pickerSide === 'left' ? rightId : leftId
    if (id === otherId) {
      toast(t('servers.compare.same'), 'error')
      return
    }
    if (pickerSide === 'left') leftId = id
    else rightId = id
    pickerOpen = false
    result = null
    err = ''
    void run()
  }

  // Знімок розбіжностей у текст — щоб можна було кинути в чат/нотатку або
  // скопіювати в звіт (той самий формат, що в експорті списку модів).
  function diffText(): string {
    if (!result) return ''
    const lines: string[] = []
    lines.push(`${result.a.name} ↔ ${result.b.name}`)
    lines.push('')
    lines.push(`${t('servers.compare.onlyA')} (${result.onlyA.length}):`)
    for (const e of result.onlyA) lines.push(`  - ${e.name}${e.version ? ' ' + e.version : ''}${e.enabled ? '' : ' [' + t('servers.content.disabled') + ']'}`)
    lines.push('')
    lines.push(`${t('servers.compare.onlyB')} (${result.onlyB.length}):`)
    for (const e of result.onlyB) lines.push(`  - ${e.name}${e.version ? ' ' + e.version : ''}${e.enabled ? '' : ' [' + t('servers.content.disabled') + ']'}`)
    lines.push('')
    lines.push(`${t('servers.compare.mismatch')} (${result.mismatchCount}):`)
    for (const e of result.both.filter(x => x.versionMismatch)) {
      lines.push(`  - ${e.name}: ${result.a.name} ${e.version || '?'} → ${e.otherVersion || '?'}`)
    }
    return lines.join('\n')
  }

  async function copyDiff() {
    try {
      await App.CopyToClipboard(diffText())
      toast(t('servers.compare.copied'), 'success')
    } catch (e: any) { toast(String(e?.message ?? e), 'error') }
  }

  function entryKey(e: ServerModsCompareEntry, i: number): string {
    return `${e.fileName || e.name}:${i}`
  }
</script>

<UiModal
  open
  size="xl"
  title={t('servers.compare.title')}
  subtitle={rightId ? `${leftName} ↔ ${rightServer?.name || '—'}` : ''}
  icon="ti-git-compare"
  color="var(--violet-l)"
  {onclose}
>
  <div class="scd">
    <!-- Шапка: ДВІ рівноправні клікабельні сторони — той самий CompareChip,
         що в порівнянні збірок. -->
    <div class="scd-heads">
      <CompareChip
        label={t('servers.compare.left')}
        name={leftChip.name}
        meta={leftChip.meta}
        src={leftChip.src}
        ti={leftChip.ti}
        color={leftChip.color}
        empty={!leftId}
        onclick={() => openPicker('left')}
      />
      <div class="scd-vs"><i class="ti ti-git-compare"></i></div>
      <CompareChip
        label={t('servers.compare.right')}
        name={rightChip.name}
        meta={rightChip.meta}
        src={rightChip.src}
        ti={rightChip.ti}
        color={rightChip.color}
        empty={!rightId}
        onclick={() => openPicker('right')}
      />
    </div>

    {#if loadingServers}
      <div class="scd-loading"><i class="ti ti-loader spin"></i><div>{t('servers.compare.loading')}</div></div>
    {:else if !rightId}
      <div class="scd-empty">
        <i class="ti ti-git-compare"></i>
        <div>{t('servers.compare.hint')}</div>
      </div>
    {:else if err}
      <div class="scd-empty">
        <i class="ti ti-alert-triangle"></i>
        <div>{t('error.withMessage', { message: err })}</div>
      </div>
    {:else if comparing}
      <div class="scd-loading"><i class="ti ti-loader spin"></i><div>{t('servers.compare.loading')}</div></div>
    {:else if result}
      <div class="scd-summary">
        <span class="scd-sum dim">{t('servers.compare.total', { a: result.a.total, b: result.b.total })}</span>
        <span class="scd-sum same"><i class="ti ti-circle-check"></i>{t('servers.compare.sameCount', { n: result.both.length })}</span>
        {#if result.mismatchCount > 0}
          <span class="scd-sum diff"><i class="ti ti-alert-triangle"></i>{t('servers.compare.mismatchCount', { n: result.mismatchCount })}</span>
        {:else}
          <span class="scd-sum same"><i class="ti ti-versions"></i>{t('servers.compare.versionsMatch')}</span>
        {/if}
        <div class="scd-spacer"></div>
        <button class="btn btn-ghost btn-sm" onclick={copyDiff}><i class="ti ti-copy"></i> {t('servers.compare.copyDiff')}</button>
      </div>

      <div class="scd-groups">
        <section class="scd-group">
          <div class="scd-ghead only-a"><i class="ti ti-arrow-left"></i> {t('servers.compare.onlyA')} <b>{result.onlyA.length}</b></div>
          {#if result.onlyA.length === 0}
            <div class="scd-none">{t('servers.compare.none')}</div>
          {:else}
            {#each result.onlyA as e, i (entryKey(e, i))}
              <div class="scd-row" class:off={!e.enabled}>
                <span class="scd-rname" title={e.fileName}>{e.name}</span>
                {#if e.version}<span class="scd-ver">{e.version}</span>{/if}
                {#if !e.enabled}<span class="scd-badge">{t('servers.content.disabled')}</span>{/if}
              </div>
            {/each}
          {/if}
        </section>

        <section class="scd-group">
          <div class="scd-ghead only-b"><i class="ti ti-arrow-right"></i> {t('servers.compare.onlyB')} <b>{result.onlyB.length}</b></div>
          {#if result.onlyB.length === 0}
            <div class="scd-none">{t('servers.compare.none')}</div>
          {:else}
            {#each result.onlyB as e, i (entryKey(e, i))}
              <div class="scd-row" class:off={!e.enabled}>
                <span class="scd-rname" title={e.fileName}>{e.name}</span>
                {#if e.version}<span class="scd-ver">{e.version}</span>{/if}
                {#if !e.enabled}<span class="scd-badge">{t('servers.content.disabled')}</span>{/if}
              </div>
            {/each}
          {/if}
        </section>
      </div>

      <section class="scd-group">
        <div class="scd-ghead both"><i class="ti ti-equal"></i> {t('servers.compare.both')} <b>{result.both.length}</b></div>
        {#if result.both.length === 0}
          <div class="scd-none">{t('servers.compare.none')}</div>
        {:else}
          <div class="scd-grid">
            {#each result.both as e, i (entryKey(e, i))}
              <div class="scd-row" class:mismatch={e.versionMismatch} class:off={!e.enabled}>
                <span class="scd-rname" title={e.fileName}>{e.name}</span>
                {#if e.versionMismatch}
                  <span class="scd-ver diff" title={t('servers.compare.versionDiffHint')}>{e.version || '?'} → {e.otherVersion || '?'}</span>
                {:else if e.version}
                  <span class="scd-ver">{e.version}</span>
                {/if}
              </div>
            {/each}
          </div>
        {/if}
      </section>
    {/if}
  </div>

  {#snippet footer()}
    <button class="btn btn-ghost" onclick={onclose}>{t('servers.compare.close')}</button>
  {/snippet}
</UiModal>

<!-- Пікер сервера для обраної сторони: той самий рядковий вигляд
     (variant="row"), що й у списку цілей встановлення / пікері збірок. -->
{#if pickerOpen}
  <UiModal
    open={true}
    title={t('servers.compare.pick')}
    icon="ti-target-arrow"
    color="var(--violet-l)"
    width="480px"
    onclose={() => { pickerOpen = false }}
  >
    <ServerTargetList
      servers={pickerServers}
      tab="mod"
      selectedId={pickerSide === 'left' ? leftId : rightId}
      onpick={(s) => pickSide(s.id)}
      variant="row"
      name="scd-pick"
    />
  </UiModal>
{/if}

<style>
  .scd { display: flex; flex-direction: column; gap: 14px; min-width: 620px; }

  /* ── Шапка (той самий патерн .pcd-heads) ── */
  .scd-heads { display: flex; align-items: flex-end; gap: 12px; }
  .scd-vs {
    color: var(--text-dim); font-size: 16px; flex-shrink: 0;
    height: 32px; display: flex; align-items: center; justify-content: center;
  }

  /* ── Порожні/завантаження стани ── */
  .scd-empty, .scd-loading {
    display: flex; flex-direction: column; align-items: center; gap: 12px;
    padding: 42px 0; color: var(--text-mute); font-size: 13px; text-align: center;
  }
  .scd-empty i, .scd-loading i { font-size: 44px; color: var(--text-dim); }
  .scd-loading i { font-size: 32px; color: var(--violet-l); }

  /* ── Підсумок ── */
  .scd-summary { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  .scd-sum { display: inline-flex; align-items: center; gap: 5px; font-size: 11.5px; font-weight: 700; padding: 4px 10px; border-radius: 999px; }
  .scd-sum i { font-size: 13px; }
  .scd-sum.same { background: rgba(16,185,129,.12); color: var(--green); }
  .scd-sum.diff { background: rgba(255,138,0,.12); color: var(--orange); }
  .scd-sum.dim { background: color-mix(in srgb, var(--text-dim) 12%, transparent); color: var(--text-mute); }
  .scd-spacer { flex: 1; }

  /* ── Групи рядків ── */
  .scd-groups { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
  .scd-group { display: flex; flex-direction: column; gap: 6px; min-width: 0; }
  .scd-ghead {
    display: flex; align-items: center; gap: 6px;
    font-size: 11px; font-weight: 700; color: var(--text-mute);
    text-transform: uppercase; letter-spacing: .4px;
    padding-bottom: 5px; border-bottom: 1px solid var(--border);
  }
  .scd-ghead b { margin-left: auto; font: 700 10px/1 var(--font-mono); color: var(--text-dim); }
  .scd-ghead.only-a { color: var(--violet-l); }
  .scd-ghead.only-b { color: var(--orange); }
  .scd-ghead.both { color: var(--green); margin-top: 4px; }
  .scd-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 6px 12px; }

  /* Картка рядка — та сама мова, що .pcd-mcard: bg-input + рамка + radius. */
  .scd-row {
    display: flex; align-items: center; gap: 6px; min-width: 0;
    padding: 7px 9px; background: var(--bg-input);
    border: 1px solid var(--border); border-radius: var(--r-sm);
    transition: border-color .15s, background .15s;
  }
  .scd-row:hover { background: var(--bg-input-hi); }
  .scd-row.mismatch {
    background: rgba(255,138,0,.07);
    border-color: color-mix(in srgb, var(--orange) 45%, var(--border));
  }
  .scd-row.off .scd-rname { color: var(--text-dim); text-decoration: line-through; text-decoration-color: color-mix(in srgb, var(--text-dim) 60%, transparent); }
  .scd-rname { flex: 1; min-width: 0; font-size: 11.5px; font-weight: 600; color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .scd-ver { font: 500 10px/1 var(--font-mono); color: var(--text-dim); flex: 0 0 auto; }
  .scd-ver.diff { color: var(--orange); }
  .scd-badge { font-size: 9.5px; font-weight: 700; color: var(--text-mute); background: var(--hover); border: 1px solid var(--border); padding: 2px 6px; border-radius: 999px; }
  .scd-none { font-size: 11px; color: var(--text-dim); padding: 6px 2px; }

  @media (max-width: 720px) {
    .scd, .scd-heads { min-width: 0; }
    .scd-heads { flex-direction: column; align-items: stretch; }
    .scd-groups, .scd-grid { grid-template-columns: 1fr; }
  }
</style>
