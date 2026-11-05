<script lang="ts">
  // StorageSection.svelte — розділ «Сховище»: скільки місця займає лаунчер,
  // які збірки найважчі і що можна безпечно прибрати. Логіка (аналіз,
  // очищення) лишається в App.svelte — тут лише відображення й колбеки.
  import { t } from '../i18n.svelte'
  import { App } from '../wails'
  import { packAsset } from '../packAssets.svelte'
  import UiModal from '../UiModal.svelte'
  import SCard from './SCard.svelte'
  import Setting from './Setting.svelte'
  import type { Settings, SettingDef, StorageInfo, StoragePackEntry, StorageVersionsInfo, StorageSuggestion, StorageLibrariesInfo, StorageAssetsInfo, UnusedJavaInfo } from '../types'

  let {
    settings, settingsSchema, updateSetting,
    storageInfo, storageScan, storageLoading, storageVersions, storagePacks, storageSuggestions,
    orphanLibs, orphanLibsBusy, orphanAssets, orphanAssetsBusy,
    javaBusy, javaProgress, unusedJava, versionsBusy,
    versionsModalOpen = $bindable(false), javaModalOpen = $bindable(false),
    orphanLibsModalOpen = $bindable(false), orphanAssetsModalOpen = $bindable(false),
    loadStorageInfo, clearCache, clearPackLogs, deletePackFromStorage,
    openVersionsModal, analyzeStorageVersions, cleanupUnusedVersions,
    openJavaModal, analyzeUnusedJava, cleanupUnusedJava,
    openOrphanLibsModal, analyzeOrphanLibs, cleanupOrphanLibs,
    openOrphanAssetsModal, analyzeOrphanAssets, cleanupOrphanAssets,
    fmtPlaytime,
  }: {
    settings: Settings
    settingsSchema: SettingDef[]
    updateSetting: (key: string, v: any) => void
    storageInfo: StorageInfo | null
    storageScan: { done: number; total: number; label: string } | null
    storageLoading: boolean
    storageVersions: StorageVersionsInfo | null
    storagePacks: StoragePackEntry[]
    storageSuggestions: StorageSuggestion[]
    orphanLibs: StorageLibrariesInfo | null
    orphanLibsBusy: boolean
    orphanAssets: StorageAssetsInfo | null
    orphanAssetsBusy: boolean
    javaBusy: boolean
    javaProgress: { done: number; total: number; major: number } | null
    unusedJava: UnusedJavaInfo | null
    versionsBusy: boolean
    versionsModalOpen?: boolean
    javaModalOpen?: boolean
    orphanLibsModalOpen?: boolean
    orphanAssetsModalOpen?: boolean
    loadStorageInfo: (force?: boolean) => void
    clearCache: () => void
    clearPackLogs: () => void
    deletePackFromStorage: (id: string) => void
    openVersionsModal: () => void
    analyzeStorageVersions: () => void
    cleanupUnusedVersions: () => void
    openJavaModal: () => void
    analyzeUnusedJava: () => void
    cleanupUnusedJava: () => void
    openOrphanLibsModal: () => void
    analyzeOrphanLibs: () => void
    cleanupOrphanLibs: () => void
    openOrphanAssetsModal: () => void
    analyzeOrphanAssets: () => void
    cleanupOrphanAssets: () => void
    fmtPlaytime: (seconds: number) => string
  } = $props()

  function fmtBytes(b: number): string {
    if (b < 1024) return `${b} ${t('st.unit.b')}`
    if (b < 1024 ** 2) return `${(b / 1024).toFixed(1)} ${t('st.unit.kb')}`
    if (b < 1024 ** 3) return `${(b / 1024 ** 2).toFixed(1)} ${t('st.unit.mb')}`
    return `${(b / 1024 ** 3).toFixed(2)} ${t('st.unit.gb')}`
  }
  const SEG_COLORS = ['#f97316', '#7b4dff', '#10b981', '#3b82f6', '#ef4444', '#eab308']
  const SEG_ICONS: Record<string, string> = {
    installations: 'ti-packages', java: 'ti-coffee', minecraft: 'ti-cube', cache: 'ti-bolt', logs: 'ti-file-text', avatars: 'ti-user-circle',
  }
  // Людські назви тек замість «installations / minecraft / cache».
  function segName(name: string): string {
    const k = `st.storage.seg.${name}`
    const v = t(k)
    return v === k ? name : v
  }
  // Кільце на SVG: кожен сегмент — дуга з stroke-dasharray, що плавно
  // «намальовується» при появі (лише stroke-dashoffset — дешево для GPU).
  const RING_R = 62
  const RING_C = 2 * Math.PI * RING_R
  let hoverSeg = $state(-1)
  let drawn = $state(false)
  $effect(() => { if (storageInfo) { drawn = false; const id = requestAnimationFrame(() => (drawn = true)); return () => cancelAnimationFrame(id) } })
  const usage = $derived.by(() => {
    if (!storageInfo) return null
    const total = Math.max(1, storageInfo.totalBytes)
    const segs = storageInfo.entries.filter((e) => e.bytes > 0).sort((a, b) => b.bytes - a.bytes)
    let acc = 0
    const arcs = segs.map((e, i) => {
      const frac = e.bytes / total
      const arc = { e, i, color: SEG_COLORS[i % SEG_COLORS.length], frac, offset: acc }
      acc += frac
      return arc
    })
    const freeBytes = storageInfo.freeDiskMB * 1024 * 1024
    const diskShare = storageInfo.totalBytes / Math.max(1, storageInfo.totalBytes + freeBytes)
    return { arcs, total, freeBytes, diskShare }
  })
  const maxPackBytes = $derived(Math.max(1, ...storagePacks.map((p) => p.bytes)))
  const sortedPacks = $derived([...storagePacks].sort((a, b) => b.bytes - a.bytes))
  const packsTotal = $derived(storagePacks.reduce((a, p) => a + p.bytes, 0))
  const cleanupDef = $derived(settingsSchema.find((d) => d.key === 'cleanupAfterPackDelete'))
</script>

<!-- ══ Скільки місця займає лаунчер ══ -->
<SCard id="x:storageUsage" icon="ti-database" tone="green" title={t('st.c.usage')} desc={t('st.c.usage.d')}>
  {#snippet actions()}
    <button class="st-iconbtn" onclick={() => loadStorageInfo(true)} disabled={storageLoading}
            title={t('st.refresh')} aria-label={t('st.refresh')}>
      <i class="ti {storageLoading ? 'ti-loader-2 spin' : 'ti-refresh'}"></i>
    </button>
    <button class="btn btn-danger btn-sm" onclick={clearCache}><i class="ti ti-eraser"></i> {t('st.storage.clearCache')}</button>
  {/snippet}
  <div class="st-pad">
    {#if storageInfo && usage}
      <div class="sv-hero">
        <div class="sv-ring" role="img" aria-label="{t('st.storage.total')}: {fmtBytes(storageInfo.totalBytes)}">
          <svg viewBox="0 0 160 160" aria-hidden="true">
            <circle class="sv-ring-track" cx="80" cy="80" r={RING_R} />
            {#each usage.arcs as arc (arc.e.name)}
              <circle class="sv-ring-seg" class:dim={hoverSeg >= 0 && hoverSeg !== arc.i} class:hot={hoverSeg === arc.i}
                      cx="80" cy="80" r={RING_R} stroke={arc.color}
                      stroke-dasharray="{Math.max(0, arc.frac * RING_C - 2.5)} {RING_C}"
                      stroke-dashoffset={drawn ? -arc.offset * RING_C : -arc.offset * RING_C + arc.frac * RING_C}
                      style="opacity:{drawn ? 1 : 0};transition-delay:{arc.i * 70}ms" />
            {/each}
          </svg>
          <div class="sv-ring-center">
            {#if hoverSeg >= 0 && usage.arcs[hoverSeg]}
              {@const h = usage.arcs[hoverSeg]}
              <b style="color:{h.color}">{Math.round(h.frac * 100)}%</b>
              <span>{segName(h.e.name)}</span>
            {:else}
              <b>{fmtBytes(storageInfo.totalBytes)}</b>
              <span>{t('st.storage.total')}</span>
            {/if}
          </div>
        </div>

        <div class="sv-side">
          <div class="sv-chips">
            <div class="sv-chip green"><i class="ti ti-device-floppy"></i><div><b>{(storageInfo.freeDiskMB / 1024).toFixed(1)} {t('st.unit.gb')}</b><span>{t('st.storage.free')}</span></div></div>
            <div class="sv-chip orange"><i class="ti ti-package"></i><div><b>{storagePacks.length}</b><span>{t('st.storage.packs')}</span></div></div>
          </div>
          <div class="sv-disk" title={t('st.storage.diskShare', { pct: (usage.diskShare * 100).toFixed(1) })}>
            <div class="sv-disk-bar"><div style="width:{drawn ? Math.max(1.5, usage.diskShare * 100) : 0}%"></div></div>
            <div class="sv-disk-tx">{t('st.storage.diskShare', { pct: (usage.diskShare * 100).toFixed(1) })}</div>
          </div>
          <div class="sv-legend" role="list" onmouseleave={() => (hoverSeg = -1)}>
            {#each usage.arcs as arc (arc.e.name)}
              <div class="sv-leg" role="listitem" class:hot={hoverSeg === arc.i} style="--c:{arc.color}" onmouseenter={() => (hoverSeg = arc.i)} title={arc.e.path}>
                <span class="sv-leg-ic"><i class="ti {SEG_ICONS[arc.e.name] ?? 'ti-folder'}"></i></span>
                <span class="sv-leg-name">{segName(arc.e.name)}</span>
                <span class="sv-leg-bar"><i style="width:{drawn ? arc.frac * 100 : 0}%"></i></span>
                <span class="sv-leg-size">{fmtBytes(arc.e.bytes)}</span>
              </div>
            {/each}
          </div>
        </div>
      </div>
      <div class="sv-foot">
        <button class="btn btn-ghost btn-sm" onclick={clearPackLogs}><i class="ti ti-file-text"></i> {t('st.storage.clearLogs')}</button>
      </div>
    {:else if storageLoading}
      <div class="storage-loading">
        <div class="sl-title"><i class="ti ti-loader-2 spin"></i> {t('st.storage.scanning')}</div>
        {#if storageScan}
          <div class="sl-bar"><div style="width:{storageScan.total > 0 ? Math.round(((storageScan.done + 1) / storageScan.total) * 100) : 8}%"></div></div>
          <div class="sl-sub">{storageScan.label} · {storageScan.done + 1}/{storageScan.total}</div>
        {/if}
      </div>
    {:else}
      <button class="btn btn-ghost btn-sm" onclick={() => loadStorageInfo(true)}><i class="ti ti-refresh"></i> {t('st.storage.scan')}</button>
    {/if}
  </div>
</SCard>

<!-- ══ Збірки, в які майже не грають ══ -->
{#if storageSuggestions.some((s) => s.recommended)}
  <SCard icon="ti-trending-down" tone="violet" title={t('st.c.suggestions')} desc={t('st.c.suggestions.d')}>
    <div class="st-pad">
      <div class="storage-packs">
        {#each storageSuggestions.filter((s) => s.recommended) as sp (sp.id)}
          {@const sic = packAsset(sp.iconUrl)}
          <div class="sp-row" title={sp.name} style="--pcol:{sp.color || 'var(--orange)'}">
            <span class="sp-ic" class:has-icon={!!sic} style="--pcol:{sp.color || 'var(--orange)'}">
              {#if sic}<img src={sic} alt="" />{:else}<i class="ti {sp.icon || 'ti-package'}"></i>{/if}
            </span>
            <span class="sp-name">{sp.name}</span>
            <span class="sp-size">{fmtBytes(sp.bytes)}</span>
            <span class="sp-badge">{t('st.storage.played', { t: fmtPlaytime(sp.playSeconds) })}</span>
            <button class="icon-btn danger" title={t('st.delete')} onclick={() => deletePackFromStorage(sp.id)}><i class="ti ti-trash"></i></button>
          </div>
        {/each}
      </div>
    </div>
  </SCard>
{/if}

<!-- ══ Встановлені збірки ══ -->
<SCard id="x:storagePacks" icon="ti-package" title={t('st.c.storagePacks')} desc={t('st.c.storagePacks.d')}>
  <div class="st-pad">
    {#if storagePacks.length === 0}
      <div class="st-note"><i class="ti ti-info-circle"></i> {t('st.storage.noPacks')}</div>
    {:else}
      <div class="sv-packs-head">
        <span>{t('st.storage.packsTotal', { n: storagePacks.length, size: fmtBytes(packsTotal) })}</span>
      </div>
      <div class="storage-packs sv-packs">
        {#each sortedPacks as sp, i (sp.id)}
          {@const sic = packAsset(sp.iconUrl)}
          <div class="sp-row" title={sp.name} style="--pcol:{sp.color || 'var(--orange)'};--ri:{Math.min(i, 12)}">
            <span class="sp-ic" class:has-icon={!!sic} style="--pcol:{sp.color || 'var(--orange)'}">
              {#if sic}<img src={sic} alt="" />{:else}<i class="ti {sp.icon || 'ti-package'}"></i>{/if}
            </span>
            <span class="sv-pack-main">
              <span class="sp-name">{sp.name}</span>
              <span class="sv-pack-bar"><i style="width:{(sp.bytes / maxPackBytes) * 100}%"></i></span>
            </span>
            <span class="sp-size">{fmtBytes(sp.bytes)}</span>
            <button class="icon-btn danger" title={t('st.delete')} aria-label={t('st.delete')} onclick={() => deletePackFromStorage(sp.id)}><i class="ti ti-trash"></i></button>
          </div>
        {/each}
      </div>
    {/if}
  </div>
</SCard>

<!-- ══ Очищення ══ -->
<SCard id="x:cleanup" icon="ti-sparkles" tone="blue" title={t('st.c.cleanup')} desc={t('st.c.cleanup.d')}>
  <div class="cleanup-grid">
    <button class="cleanup-tile" onclick={openVersionsModal}>
      <div class="ct-ic green"><i class="ti ti-box"></i></div><i class="ti ti-chevron-right ct-go" aria-hidden="true"></i>
      <div class="ct-title">{t('st.storage.mcVersions')}</div>
      {#if storageVersions}
        {#if (storageVersions.unused ?? []).length > 0}
          <div class="ct-status warn">{t('st.storage.unusedCount', { n: storageVersions.unused.length })}</div>
          <div class="ct-sub">{t('st.storage.canFree', { size: fmtBytes(storageVersions.totalBytes) })}</div>
        {:else}
          <div class="ct-status ok"><i class="ti ti-circle-check"></i> {t('st.storage.allUsed')}</div>
        {/if}
      {:else}
        <div class="ct-status">{t('st.storage.check')}</div>
      {/if}
    </button>

    <button class="cleanup-tile" onclick={openJavaModal}>
      <div class="ct-ic orange"><i class="ti ti-coffee"></i></div><i class="ti ti-chevron-right ct-go" aria-hidden="true"></i>
      <div class="ct-title">Java</div>
      {#if javaBusy && !unusedJava}
        <div class="ct-status"><i class="ti ti-loader-2 spin"></i> {t('st.storage.checking')}</div>
      {:else if unusedJava && unusedJava.installed.length === 0}
        <div class="ct-status">{t('st.storage.javaEmpty')}</div>
      {:else if unusedJava && unusedJava.unused.length === 0}
        <div class="ct-status ok"><i class="ti ti-circle-check"></i> {t('st.storage.allUsed')}</div>
      {:else if unusedJava}
        <div class="ct-status warn">{t('st.storage.unusedCount', { n: unusedJava.unused.length })}</div>
        <div class="ct-sub">{t('st.storage.canFree', { size: fmtBytes(unusedJava.totalBytes) })}</div>
      {:else}
        <div class="ct-status">{t('st.storage.check')}</div>
      {/if}
    </button>

    <button class="cleanup-tile" onclick={openOrphanLibsModal}>
      <div class="ct-ic violet"><i class="ti ti-stack-2"></i></div><i class="ti ti-chevron-right ct-go" aria-hidden="true"></i>
      <div class="ct-title">{t('st.storage.libs')}</div>
      {#if orphanLibsBusy && !orphanLibs}
        <div class="ct-status"><i class="ti ti-loader-2 spin"></i> {t('st.storage.checking')}</div>
      {:else if orphanLibs && orphanLibs.orphanFiles > 0}
        <div class="ct-status warn">{t('st.storage.unusedFiles', { n: orphanLibs.orphanFiles })}</div>
        <div class="ct-sub">{t('st.storage.canFree', { size: fmtBytes(orphanLibs.orphanBytes) })}</div>
      {:else if orphanLibs}
        <div class="ct-status ok"><i class="ti ti-circle-check"></i> {t('st.storage.nothingExtra')}</div>
      {:else}
        <div class="ct-status">{t('st.storage.check')}</div>
      {/if}
    </button>

    <button class="cleanup-tile" onclick={openOrphanAssetsModal}>
      <div class="ct-ic blue"><i class="ti ti-photo"></i></div><i class="ti ti-chevron-right ct-go" aria-hidden="true"></i>
      <div class="ct-title">{t('st.storage.assets')}</div>
      {#if orphanAssetsBusy && !orphanAssets}
        <div class="ct-status"><i class="ti ti-loader-2 spin"></i> {t('st.storage.checking')}</div>
      {:else if orphanAssets && orphanAssets.orphanObjects > 0}
        <div class="ct-status warn">{t('st.storage.unusedFiles', { n: orphanAssets.orphanObjects })}</div>
        <div class="ct-sub">{t('st.storage.canFree', { size: fmtBytes(orphanAssets.orphanBytes) })}</div>
      {:else if orphanAssets}
        <div class="ct-status ok"><i class="ti ti-circle-check"></i> {t('st.storage.nothingExtra')}</div>
      {:else}
        <div class="ct-status">{t('st.storage.check')}</div>
      {/if}
    </button>

    <button class="cleanup-tile" onclick={() => App.OpenCacheFolder()}>
      <div class="ct-ic yellow"><i class="ti ti-folder-open"></i></div><i class="ti ti-chevron-right ct-go" aria-hidden="true"></i>
      <div class="ct-title">{t('st.storage.cacheFolder')}</div>
      <div class="ct-status">{t('st.storage.openInExplorer')}</div>
    </button>

    <button class="cleanup-tile" onclick={() => App.OpenLogsFolder()}>
      <div class="ct-ic red"><i class="ti ti-folder-open"></i></div><i class="ti ti-chevron-right ct-go" aria-hidden="true"></i>
      <div class="ct-title">{t('st.storage.logsFolder')}</div>
      <div class="ct-status">{t('st.storage.openInExplorer')}</div>
    </button>
  </div>
  {#if cleanupDef}
    <Setting def={cleanupDef} value={(settings as any)[cleanupDef.key]} onchange={(v) => updateSetting(cleanupDef.key, v)} />
  {/if}
</SCard>

              <!-- ══ Модалка: Версії Minecraft ══ -->
              {#if versionsModalOpen}
                <UiModal
                  open={true}
                  size="md"
                  title={t('settings.storage.mcModalTitle')}
                  icon="ti-box"
                  color="var(--green)"
                  onclose={() => (versionsModalOpen = false)}
                >
                      {#if versionsBusy && !storageVersions}
                        <div class="java-modal-analyzing">
                          <div class="jma-ring"></div>
                          <div class="jma-title"><i class="ti ti-box"></i> {t('settings.storage.mcAnalyzing')}</div>
                          <div class="jma-sub">{t('settings.storage.mcAnalyzingHint')}</div>
                        </div>
                      {:else if storageVersions}
                        {#if (storageVersions.installed ?? []).length === 0}
                          <div class="java-none"><i class="ti ti-box"></i> {t('settings.storage.mcEmpty')}</div>
                        {:else}
                          <div class="java-list" style="max-height:340px;overflow-y:auto">
                            {#each storageVersions.installed ?? [] as v (v.id)}
                              <div class="java-row" class:unused={!v.used}>
                                <i class="ti {v.inherit ? 'ti-stack-2' : 'ti-cube'} ji" style="color:var(--green)"></i>
                                <span class="j-name">{v.id}</span>
                                <span class="j-size">{fmtBytes(v.bytes)}</span>
                                {#if v.used}
                                  <span class="j-badge used"><i class="ti ti-check"></i> {t('settings.storage.javaUsed')}</span>
                                {:else}
                                  <span class="j-badge unused"><i class="ti ti-alert-triangle"></i> {t('settings.storage.javaUnused')}</span>
                                {/if}
                              </div>
                            {/each}
                          </div>
                          <div class="java-total">
                            <span>{t('settings.storage.mcVersionsTotal')} ({(storageVersions.installed ?? []).length})</span>
                            <b style="color:var(--text-mute)">{fmtBytes((storageVersions.installed ?? []).reduce((s, v) => s + v.bytes, 0))}</b>
                          </div>
                          {#if (storageVersions.unused ?? []).length > 0}
                            <button class="btn btn-danger" style="width:100%; justify-content:center; margin-top:14px" disabled={versionsBusy} onclick={cleanupUnusedVersions}>
                              <i class="ti {versionsBusy ? 'ti-loader-2 spin' : 'ti-trash'}"></i>
                              {t('settings.storage.cleanVersionsModalBtn', { n: storageVersions.unused.length, size: fmtBytes(storageVersions.totalBytes) })}
                            </button>
                          {:else}
                            <div class="java-none"><i class="ti ti-circle-check" style="color:var(--green)"></i> {t('settings.storage.mcAllUsed')}</div>
                          {/if}
                          <button class="btn btn-ghost btn-sm" style="margin-top:12px" onclick={analyzeStorageVersions}>
                            <i class="ti {versionsBusy ? 'ti-loader-2 spin' : 'ti-refresh'}"></i> {t('settings.storage.javaReanalyze')}
                          </button>
                        {/if}
                      {:else}
                        <div class="java-none"><i class="ti ti-alert-circle"></i> {t('settings.storage.mcFailed')}</div>
                      {/if}
                </UiModal>
              {/if}

              {#if javaModalOpen}
                <UiModal
                  open={true}
                  size="md"
                  title={t('settings.storage.javaModalTitle')}
                  icon="ti-coffee"
                  color="var(--orange)"
                  onclose={() => (javaModalOpen = false)}
                >
                      {#if javaBusy && !unusedJava}
                        <!-- Процес аналізу: спінер + ЖИВИЙ прогрес (який мажор
                             сканується, скільки лишилось) поки бекенд рахує. -->
                        <div class="java-modal-analyzing">
                          <div class="jma-ring"></div>
                          <div class="jma-title"><i class="ti ti-coffee"></i> {t('settings.storage.javaAnalyzing')}</div>
                          <div class="jma-sub">{t('settings.storage.javaAnalyzingHint')}</div>
                          {#if javaProgress && javaProgress.total > 0}
                            <div class="ja-bar" style="width:100%;margin-top:16px"><div style="width:{Math.round(((javaProgress.done + 1) / javaProgress.total) * 100)}%"></div></div>
                            <div class="ja-sub" style="text-align:center">{t('settings.storage.javaScanItem', { major: javaProgress.major, n: javaProgress.done + 1, total: javaProgress.total })}</div>
                          {/if}
                        </div>
                      {:else if unusedJava}
                        {#if (unusedJava.installed ?? []).length === 0}
                          <div class="java-none"><i class="ti ti-coffee-off"></i> {t('settings.storage.javaEmpty')}</div>
                        {:else}
                          <div class="java-list">
                            {#each unusedJava.installed ?? [] as j, i (j.major + '-' + j.bytes + '-' + i)}
                              <div class="java-row" class:unused={!j.used}>
                                <i class="ti ti-coffee ji"></i>
                                <span class="j-name">Java {j.major}</span>
                                <span class="j-size">{fmtBytes(j.bytes)}</span>
                                {#if j.used}
                                  <span class="j-badge used"><i class="ti ti-check"></i> {t('settings.storage.javaUsed')}</span>
                                {:else}
                                  <span class="j-badge unused"><i class="ti ti-alert-triangle"></i> {t('settings.storage.javaUnused')}</span>
                                {/if}
                              </div>
                            {/each}
                          </div>
                          {#if (unusedJava.unused ?? []).length > 0}
                            <div class="java-total">
                              <span>{t('settings.storage.javaCanFreeTotal')}</span>
                              <b style="color:var(--red)">{fmtBytes(unusedJava.totalBytes)}</b>
                            </div>
                            <button class="btn btn-danger" style="width:100%; justify-content:center; margin-top:14px" disabled={javaBusy} onclick={cleanupUnusedJava}>
                              <i class="ti {javaBusy ? 'ti-loader-2 spin' : 'ti-trash'}"></i>
                              {t('settings.storage.cleanJavaModalBtn', { n: unusedJava.unused.length })}
                            </button>
                          {:else}
                            <div class="java-none"><i class="ti ti-circle-check" style="color:var(--green)"></i> {t('settings.storage.cleanJavaNone')}</div>
                          {/if}
                          <button class="btn btn-ghost btn-sm" style="margin-top:12px" onclick={analyzeUnusedJava}>
                            <i class="ti {javaBusy ? 'ti-loader-2 spin' : 'ti-refresh'}"></i> {t('settings.storage.javaReanalyze')}
                          </button>
                        {/if}
                      {:else}
                        <div class="java-none"><i class="ti ti-alert-circle"></i> {t('settings.storage.javaFailed')}</div>
                      {/if}
                </UiModal>
              {/if}

              <!-- ══ Модалка: Бібліотеки-сироти (нове) ══ -->
              {#if orphanLibsModalOpen}
                <UiModal
                  open={true}
                  size="md"
                  title={t('settings.storage.orphanLibsModalTitle')}
                  icon="ti-stack-2"
                  color="var(--violet)"
                  onclose={() => (orphanLibsModalOpen = false)}
                >
                      {#if orphanLibsBusy && !orphanLibs}
                        <div class="java-modal-analyzing">
                          <div class="jma-ring"></div>
                          <div class="jma-title"><i class="ti ti-stack-2"></i> {t('settings.storage.orphanScanning')}</div>
                          <div class="jma-sub">{t('settings.storage.orphanLibsHint')}</div>
                        </div>
                      {:else if orphanLibs}
                        {#if orphanLibs.error}
                          <div class="java-none"><i class="ti ti-alert-circle"></i> {orphanLibs.error}</div>
                        {:else if orphanLibs.orphanFiles === 0}
                          <div class="java-none"><i class="ti ti-circle-check" style="color:var(--green)"></i> {t('settings.storage.orphanLibsNone')}</div>
                        {:else}
                          <div class="java-total" style="margin-bottom:0">
                            <span>{t('settings.storage.orphanLibsCount', { n: orphanLibs.orphanFiles })}</span>
                            <b style="color:var(--red)">{fmtBytes(orphanLibs.orphanBytes)}</b>
                          </div>
                          <div class="rl-sub" style="font-size:11px;margin-top:8px">{t('settings.storage.orphanLibsHint')}</div>
                          <button class="btn btn-danger" style="width:100%; justify-content:center; margin-top:14px" disabled={orphanLibsBusy} onclick={cleanupOrphanLibs}>
                            <i class="ti {orphanLibsBusy ? 'ti-loader-2 spin' : 'ti-trash'}"></i>
                            {t('settings.storage.cleanLibs')} ({orphanLibs.orphanFiles}) · {fmtBytes(orphanLibs.orphanBytes)}
                          </button>
                        {/if}
                        <button class="btn btn-ghost btn-sm" style="margin-top:12px" onclick={analyzeOrphanLibs}>
                          <i class="ti {orphanLibsBusy ? 'ti-loader-2 spin' : 'ti-refresh'}"></i> {t('settings.storage.javaReanalyze')}
                        </button>
                      {:else}
                        <div class="java-none"><i class="ti ti-alert-circle"></i> {t('settings.storage.orphanFailed')}</div>
                      {/if}
                </UiModal>
              {/if}

              <!-- ══ Модалка: Асети-сироти (нове) ══ -->
              {#if orphanAssetsModalOpen}
                <UiModal
                  open={true}
                  size="md"
                  title={t('settings.storage.orphanAssetsModalTitle')}
                  icon="ti-photo"
                  color="var(--blue)"
                  onclose={() => (orphanAssetsModalOpen = false)}
                >
                      {#if orphanAssetsBusy && !orphanAssets}
                        <div class="java-modal-analyzing">
                          <div class="jma-ring"></div>
                          <div class="jma-title"><i class="ti ti-photo"></i> {t('settings.storage.orphanScanning')}</div>
                          <div class="jma-sub">{t('settings.storage.orphanAssetsHint')}</div>
                        </div>
                      {:else if orphanAssets}
                        {#if orphanAssets.error}
                          <div class="java-none"><i class="ti ti-alert-circle"></i> {orphanAssets.error}</div>
                        {:else if orphanAssets.orphanObjects === 0}
                          <div class="java-none"><i class="ti ti-circle-check" style="color:var(--green)"></i> {t('settings.storage.orphanAssetsNone')}</div>
                        {:else}
                          <div class="java-total" style="margin-bottom:0">
                            <span>{t('settings.storage.orphanAssetsCount', { n: orphanAssets.orphanObjects })}</span>
                            <b style="color:var(--red)">{fmtBytes(orphanAssets.orphanBytes)}</b>
                          </div>
                          <div class="rl-sub" style="font-size:11px;margin-top:8px">{t('settings.storage.orphanAssetsHint')}</div>
                          <button class="btn btn-danger" style="width:100%; justify-content:center; margin-top:14px" disabled={orphanAssetsBusy} onclick={cleanupOrphanAssets}>
                            <i class="ti {orphanAssetsBusy ? 'ti-loader-2 spin' : 'ti-trash'}"></i>
                            {t('settings.storage.cleanAssets')} ({orphanAssets.orphanObjects}) · {fmtBytes(orphanAssets.orphanBytes)}
                          </button>
                        {/if}
                        <button class="btn btn-ghost btn-sm" style="margin-top:12px" onclick={analyzeOrphanAssets}>
                          <i class="ti {orphanAssetsBusy ? 'ti-loader-2 spin' : 'ti-refresh'}"></i> {t('settings.storage.javaReanalyze')}
                        </button>
                      {:else}
                        <div class="java-none"><i class="ti ti-alert-circle"></i> {t('settings.storage.orphanFailed')}</div>
                      {/if}
                </UiModal>
              {/if}

<style>
  /* ── «Використання диска»: кільце + легенда (редизайн) ── */
  .sv-hero { display: grid; grid-template-columns: 172px 1fr; gap: 26px; align-items: center; }
  .sv-ring { position: relative; width: 172px; height: 172px; }
  .sv-ring svg { width: 100%; height: 100%; transform: rotate(-90deg); overflow: visible; }
  .sv-ring-track { fill: none; stroke: var(--hover-strong); stroke-width: 16; }
  .sv-ring-seg {
    fill: none; stroke-width: 16; stroke-linecap: butt;
    transition: stroke-dashoffset .9s cubic-bezier(.16,1,.3,1), opacity .4s ease, stroke-width .2s ease, filter .2s ease;
  }
  .sv-ring-seg.hot { stroke-width: 20; filter: drop-shadow(0 0 6px currentColor); }
  .sv-ring-seg.dim { opacity: .3 !important; }
  .sv-ring-center {
    position: absolute; inset: 30px; border-radius: 50%; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 3px;
    background: radial-gradient(circle at 50% 35%, color-mix(in srgb, var(--orange) 10%, var(--bg-card)), var(--bg-card) 70%);
    box-shadow: inset 0 0 0 1px var(--border); text-align: center;
  }
  .sv-ring-center b { font: 800 19px/1.1 var(--font-mono); color: var(--text); font-variant-numeric: tabular-nums; }
  .sv-ring-center span { max-width: 92px; font-size: 10.5px; font-weight: 700; color: var(--text-mute); text-transform: uppercase; letter-spacing: .05em; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

  .sv-side { min-width: 0; display: flex; flex-direction: column; gap: 12px; }
  .sv-chips { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; }
  .sv-chip { display: flex; align-items: center; gap: 10px; padding: 10px 12px; border-radius: var(--r-md); background: var(--bg-input); border: 1px solid var(--border); }
  .sv-chip > i { display: grid; place-items: center; width: 32px; height: 32px; border-radius: 10px; font-size: 16px; flex: none; }
  .sv-chip.green > i { background: rgba(16,185,129,.14); color: var(--green); }
  .sv-chip.orange > i { background: rgba(var(--orange-rgb), .14); color: var(--orange); }
  .sv-chip div { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .sv-chip b { font: 800 14px/1.1 var(--font-mono); color: var(--text); }
  .sv-chip span { font-size: 11px; color: var(--text-mute); }

  .sv-disk { display: flex; flex-direction: column; gap: 5px; }
  .sv-disk-bar { height: 6px; border-radius: 999px; background: var(--hover-strong); overflow: hidden; }
  .sv-disk-bar div { height: 100%; border-radius: inherit; background: linear-gradient(90deg, var(--orange), color-mix(in srgb, var(--orange) 55%, #fff)); transition: width 1s cubic-bezier(.16,1,.3,1); }
  .sv-disk-tx { font-size: 11px; color: var(--text-dim); }

  .sv-legend { display: flex; flex-direction: column; gap: 2px; padding: 4px; border-radius: var(--r-md); background: var(--bg-input); border: 1px solid var(--border); }
  .sv-leg { display: grid; grid-template-columns: 26px minmax(80px, 1fr) minmax(60px, 1.2fr) auto; align-items: center; gap: 10px; padding: 6px 8px; border-radius: 9px; cursor: default; transition: background .15s; }
  .sv-leg:hover, .sv-leg.hot { background: color-mix(in srgb, var(--c) 9%, transparent); }
  .sv-leg-ic { display: grid; place-items: center; width: 26px; height: 26px; border-radius: 8px; font-size: 14px; color: var(--c); background: color-mix(in srgb, var(--c) 15%, transparent); }
  .sv-leg-name { min-width: 0; font-size: 12.5px; font-weight: 650; color: var(--text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .sv-leg-bar { height: 5px; border-radius: 999px; background: var(--hover-strong); overflow: hidden; }
  .sv-leg-bar i { display: block; height: 100%; border-radius: inherit; background: var(--c); transition: width .9s cubic-bezier(.16,1,.3,1); }
  .sv-leg-size { font: 700 12px/1 var(--font-mono); color: var(--text); text-align: right; min-width: 64px; font-variant-numeric: tabular-nums; }
  .sv-foot { display: flex; justify-content: flex-end; margin-top: 14px; }

  /* ── Встановлені збірки: від найважчої, зі смужкою відносного розміру ── */
  .sv-packs-head { margin: -2px 0 10px; font-size: 12px; color: var(--text-mute); }
  .sv-packs { gap: 2px; padding: 4px; max-height: 340px; background: var(--bg-input); border: 1px solid var(--border); border-radius: var(--r-md); }
  .sv-packs :global(.sp-row) { background: transparent; border-color: transparent; border-radius: 10px; animation: sv-row-in .35s cubic-bezier(.16,1,.3,1) both; animation-delay: calc(var(--ri, 0) * 25ms); }
  .sv-packs :global(.sp-row:hover) { background: var(--hover); border-color: transparent; }
  .sv-pack-main { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 5px; }
  .sv-pack-bar { height: 4px; border-radius: 999px; background: var(--hover-strong); overflow: hidden; }
  .sv-pack-bar i { display: block; height: 100%; border-radius: inherit; background: var(--pcol, var(--orange)); opacity: .85; }
  @keyframes sv-row-in { from { opacity: 0; transform: translateY(4px); } to { opacity: 1; transform: none; } }

  /* ── Плитки очищення: стрілка-підказка й кольоровий ободок при наведенні ── */
  :global(.cleanup-tile) { position: relative; }
  .ct-go { position: absolute; top: 14px; right: 12px; color: var(--text-dim); font-size: 15px; transition: transform .2s cubic-bezier(.34,1.56,.64,1), color .15s; }
  :global(.cleanup-tile:hover) .ct-go { transform: translateX(3px); color: var(--orange); }

  @container (max-width: 560px) { .sv-hero { grid-template-columns: 1fr; justify-items: center; } .sv-side { width: 100%; } }
  @media (max-width: 720px) { .sv-hero { grid-template-columns: 1fr; justify-items: center; } .sv-side { width: 100%; } }
</style>
