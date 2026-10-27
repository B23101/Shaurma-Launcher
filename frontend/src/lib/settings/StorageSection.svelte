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
    {#if storageInfo}
      {@const segs = storageInfo.entries.filter((e) => e.bytes > 0)}
      {@const total = Math.max(1, storageInfo.totalBytes)}
      {@const gradient = (() => {
        let acc = 0
        const stops = segs.map((e, i) => {
          const start = (acc / total) * 100
          acc += e.bytes
          return `${SEG_COLORS[i % SEG_COLORS.length]} ${start}% ${(acc / total) * 100}%`
        })
        return stops.join(', ') || 'var(--bg-input) 0% 100%'
      })()}
      <div class="sh-top">
        <div class="sh-ring-wrap">
          <div class="sh-ring" style="--sh-gradient:{gradient}">
            <div class="sh-ring-hole">
              <div class="sh-ring-total">{fmtBytes(storageInfo.totalBytes)}</div>
              <div class="sh-ring-label">{t('st.storage.total')}</div>
            </div>
          </div>
        </div>
        <div class="sh-stats">
          <div class="sh-stat">
            <div class="ss-ic free"><i class="ti ti-device-floppy"></i></div>
            <div class="ss-body"><div class="ss-label">{t('st.storage.free')}</div><div class="ss-value">{(storageInfo.freeDiskMB / 1024).toFixed(1)} {t('st.unit.gb')}</div></div>
          </div>
          <div class="sh-stat">
            <div class="ss-ic packs"><i class="ti ti-package"></i></div>
            <div class="ss-body"><div class="ss-label">{t('st.storage.packs')}</div><div class="ss-value">{storagePacks.length}</div></div>
          </div>
          {#each segs.slice(0, 4) as e, i}
            <div class="sh-stat">
              <div class="ss-ic" style="background:{SEG_COLORS[i % SEG_COLORS.length]}22;color:{SEG_COLORS[i % SEG_COLORS.length]}"><span class="ss-dot" style="background:{SEG_COLORS[i % SEG_COLORS.length]}"></span></div>
              <div class="ss-body"><div class="ss-label">{e.name}</div><div class="ss-value">{fmtBytes(e.bytes)}</div></div>
            </div>
          {/each}
        </div>
      </div>
      <button class="btn btn-ghost btn-sm" style="margin-top:16px" onclick={clearPackLogs}><i class="ti ti-file-text"></i> {t('st.storage.clearLogs')}</button>
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
      <div class="storage-packs">
        {#each storagePacks as sp (sp.id)}
          {@const sic = packAsset(sp.iconUrl)}
          <div class="sp-row" title={sp.name} style="--pcol:{sp.color || 'var(--orange)'}">
            <span class="sp-ic" class:has-icon={!!sic} style="--pcol:{sp.color || 'var(--orange)'}">
              {#if sic}<img src={sic} alt="" />{:else}<i class="ti {sp.icon || 'ti-package'}"></i>{/if}
            </span>
            <span class="sp-name">{sp.name}</span>
            <span class="sp-size">{fmtBytes(sp.bytes)}</span>
            <button class="icon-btn danger" title={t('st.delete')} onclick={() => deletePackFromStorage(sp.id)}><i class="ti ti-trash"></i></button>
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
      <div class="ct-ic green"><i class="ti ti-box"></i></div>
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
      <div class="ct-ic orange"><i class="ti ti-coffee"></i></div>
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
      <div class="ct-ic violet"><i class="ti ti-stack-2"></i></div>
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
      <div class="ct-ic blue"><i class="ti ti-photo"></i></div>
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
      <div class="ct-ic yellow"><i class="ti ti-folder-open"></i></div>
      <div class="ct-title">{t('st.storage.cacheFolder')}</div>
      <div class="ct-status">{t('st.storage.openInExplorer')}</div>
    </button>

    <button class="cleanup-tile" onclick={() => App.OpenLogsFolder()}>
      <div class="ct-ic red"><i class="ti ti-folder-open"></i></div>
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
