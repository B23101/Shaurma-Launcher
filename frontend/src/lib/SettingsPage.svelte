<script lang="ts">
  // SettingsPage.svelte — сторінка «Налаштування»: пошук, меню розділів
  // (згруповане, як у Prism) і вміст розділу. Уся логіка лишається в App.svelte —
  // сюди приходять значення й колбеки. Структура меню та індекс пошуку — у
  // settings/registry.ts; кожен параметр має якір data-setting-key, тому
  // пошук доскролює саме до рядка.
  import { untrack } from 'svelte'
  import { t, setLanguage, removeCustomLanguage, getCustomLanguages } from './i18n.svelte'
  import { consoleFontStack } from './theme.svelte'
  import { App } from './wails'
  import LauncherAppearance from './LauncherAppearance.svelte'
  import MinecraftDefaultsTab from './MinecraftDefaultsTab.svelte'
  import MigrationSettingsWidget from './MigrationSettingsWidget.svelte'
  import AccountProfilePanel from './AccountProfilePanel.svelte'
  import SCard from './settings/SCard.svelte'
  import Setting from './settings/Setting.svelte'
  import AccountsCard from './settings/AccountsCard.svelte'
  import AutoPacksCard from './settings/AutoPacksCard.svelte'
  import FoldersSection from './settings/FoldersSection.svelte'
  import StorageSection from './settings/StorageSection.svelte'
  import AboutSection from './settings/AboutSection.svelte'
  import { navGroups, settingsSectionById, normalizeSectionId, searchSettings, labelKeyOf, cardKeyOf, revealSetting } from './settings/registry'
  import './settings/settings.css'
  import type { Account, Settings, SettingDef, StorageInfo, StoragePackEntry, StorageVersionsInfo, StorageSuggestion, StorageLibrariesInfo, StorageAssetsInfo, UnusedJavaInfo, UpdateInfo, FolderPaths, SystemInfo, AutoPackView } from './types'

  let {
    settings, settingsSchema,
    settingsTab = $bindable('general'),
    versionsModalOpen = $bindable(false),
    javaModalOpen = $bindable(false),
    orphanLibsModalOpen = $bindable(false),
    orphanAssetsModalOpen = $bindable(false),
    dragAcc = $bindable<number | null>(null),
    dragOverRow = $bindable<number | null>(null),
    accounts, activeAccountId, skinAuthNick = '', skinsEnabled = false,
    storageInfo, storageScan, storageLoading, storageVersions, storagePacks, storageSuggestions,
    orphanLibs, orphanLibsBusy, orphanAssets, orphanAssetsBusy,
    aboutSys, aboutVersion, updateResult, updatingCheck, updateStarting,
    javaBusy, javaProgress, unusedJava, versionsBusy,
    folderPaths, folderRows, fontsDir,
    sidebarOnlyPinned, sidebarScale, sidebarWidth,
    headFor, fmtPlaytime,
    updateSetting, loadStorageInfo, clearCache, clearPackLogs, deletePackFromStorage,
    openVersionsModal, analyzeStorageVersions, cleanupUnusedVersions,
    openJavaModal, analyzeUnusedJava, cleanupUnusedJava,
    openOrphanLibsModal, analyzeOrphanLibs, cleanupOrphanLibs,
    openOrphanAssetsModal, analyzeOrphanAssets, cleanupOrphanAssets,
    loadFolderPaths, setFolder, resetBaseDir, loadFontsDir,
    checkUpdates, updateNow, toggleMinecraftDrawer,
    moveAccountTo, reloginAccount, switchAccount, removeAccount, openRenameAccount,
    toggleSidebarOnlyPinned, setSidebarScale, saveSidebarWidth,
    importLanguageFile, exportLauncherErrors, exportingErrors,
    onaddaccount, onaccountschanged = () => {},
    autoPacks = [] as AutoPackView[], autoPacksBusy = false,
    onAutoPacksToggle, onAutoPacksDelete, onAutoPacksOpen,
  }: {
    settings: Settings
    settingsSchema: SettingDef[]
    settingsTab?: string
    versionsModalOpen?: boolean
    javaModalOpen?: boolean
    orphanLibsModalOpen?: boolean
    orphanAssetsModalOpen?: boolean
    dragAcc?: number | null
    dragOverRow?: number | null
    accounts: Account[]
    activeAccountId: string
    skinAuthNick?: string
    skinsEnabled?: boolean
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
    aboutSys: SystemInfo
    aboutVersion: string
    updateResult: UpdateInfo | null
    updatingCheck: boolean
    updateStarting: boolean
    javaBusy: boolean
    javaProgress: { done: number; total: number; major: number } | null
    unusedJava: UnusedJavaInfo | null
    versionsBusy: boolean
    folderPaths: FolderPaths | null
    folderRows: { kind: string; name: string; desc: string; path: string; custom: string; reset: boolean; fixed: boolean }[]
    fontsDir: string
    sidebarOnlyPinned: boolean
    sidebarScale: number | null
    sidebarWidth: number
    headFor: (uuid?: string) => string
    fmtPlaytime: (seconds: number) => string
    updateSetting: (key: string, v: any) => void
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
    loadFolderPaths: (force?: boolean) => void
    setFolder: (kind: string) => void
    resetBaseDir: () => void
    loadFontsDir: (force?: boolean) => void
    checkUpdates: () => void
    updateNow: () => void
    toggleMinecraftDrawer: () => void
    moveAccountTo: (from: number, to: number) => void
    reloginAccount: (acc: Account) => void
    switchAccount: (id: string) => void
    removeAccount: (id: string) => void
    openRenameAccount: (acc: Account) => void
    toggleSidebarOnlyPinned: () => void
    setSidebarScale: (v: number) => void
    saveSidebarWidth: () => void
    importLanguageFile: () => void
    exportLauncherErrors: () => void
    exportingErrors: boolean
    onaddaccount: () => void
    onaccountschanged?: () => void
    autoPacks?: AutoPackView[]
    autoPacksBusy?: boolean
    onAutoPacksToggle: (kind: string, enabled: boolean) => Promise<void> | void
    onAutoPacksDelete: (view: AutoPackView) => void
    onAutoPacksOpen: (id: string) => void
  } = $props()

  // ── Поточний розділ ────────────────────────────────────────────────────
  // Старі ID (збережені в uiState) нормалізуємо до нових.
  $effect(() => {
    const n = normalizeSectionId(settingsTab)
    if (n !== settingsTab) settingsTab = n
  })
  const section = $derived(settingsSectionById[settingsTab] ?? settingsSectionById.general)

  // Ледачі завантаження при відкритті розділу (у тому числі через пошук).
  $effect(() => {
    const tab = settingsTab
    untrack(() => {
      if (tab === 'folders') loadFolderPaths()
      if (tab === 'appearance') loadFontsDir()
    })
  })

  let rootEl = $state<HTMLElement | null>(null)
  let mainEl = $state<HTMLElement | null>(null)
  // Скролиться лише права частина — після зміни розділу повертаємо її нагору.
  $effect(() => { settingsTab; mainEl?.scrollTo({ top: 0 }) })
  function openSection(id: string) {
    if (settingsTab === id) return
    settingsTab = id
    // нова вкладка завжди з початку (інакше лишається зсув від попередньої)
    let el: HTMLElement | null = rootEl?.parentElement ?? null
    while (el) {
      const oy = getComputedStyle(el).overflowY
      if ((oy === 'auto' || oy === 'scroll') && el.scrollHeight > el.clientHeight) { el.scrollTop = 0; break }
      el = el.parentElement
    }
  }

  // ── Допоміжне для розмітки ─────────────────────────────────────────────
  const defOf = (key: string) => settingsSchema.find((d) => d.key === key)
  const val = (key: string) => (settings as any)[key]
  // Умови showIf зі схеми: рядок видно, лише коли залежний параметр має потрібне значення.
  function condsMet(d: SettingDef): boolean {
    return (d.showIf ?? []).every((c) => {
      const hit = c.in.includes(String(val(c.key) ?? ''))
      return c.not ? !hit : hit
    })
  }
  type RowOpts = { invert?: boolean; unit?: string; sub?: boolean; disabled?: boolean; label?: string; hint?: string }

  // ── Пошук ──────────────────────────────────────────────────────────────
  let query = $state('')
  let activeHit = $state(0)
  let searchFocused = $state(false)
  const hits = $derived(searchSettings(query, t, 10))
  $effect(() => { query; activeHit = 0 })

  function goHit(i: number) {
    const h = hits[i]
    if (!h) return
    query = ''
    searchFocused = false
    if (settingsTab !== h.section) settingsTab = h.section
    revealSetting(h.id)
  }
  function onSearchKey(e: KeyboardEvent) {
    if (e.key === 'Escape') { query = ''; (e.currentTarget as HTMLElement).blur() }
    else if (e.key === 'ArrowDown') { e.preventDefault(); activeHit = Math.min(activeHit + 1, hits.length - 1) }
    else if (e.key === 'ArrowUp') { e.preventDefault(); activeHit = Math.max(activeHit - 1, 0) }
    else if (e.key === 'Enter') { e.preventDefault(); goHit(activeHit) }
  }
  // Підсвітка збігу в назві.
  function highlight(text: string, q: string): { pre: string; mid: string; post: string } {
    const tok = q.trim().toLowerCase().split(/\s+/)[0] ?? ''
    const i = tok ? text.toLowerCase().indexOf(tok) : -1
    return i < 0 ? { pre: text, mid: '', post: '' } : { pre: text.slice(0, i), mid: text.slice(i, i + tok.length), post: text.slice(i + tok.length) }
  }

  // ── Мова: імпортовані переклади ────────────────────────────────────────
  const customLangs = $derived(Object.values(getCustomLanguages()))

  // ── Вигляд ─────────────────────────────────────────────────────────────
  const THEME_PRESETS = [
    { bg: '#0f0f0f', card: '#161616', text: '#ffffff' },
    { bg: '#0b1020', card: '#151c36', text: '#e9edff' },
    { bg: '#12101f', card: '#1d1933', text: '#f1eaff' },
    { bg: '#0d1510', card: '#16251b', text: '#e9f5ec' },
    { bg: '#16120d', card: '#241b11', text: '#f6ecd9' },
    { bg: '#101418', card: '#1a212a', text: '#e6edf3' },
    { bg: '#f5f5f2', card: '#ffffff', text: '#1d1d1d' },
    { bg: '#f1eef7', card: '#ffffff', text: '#2b2536' },
  ]
  const activePreset = $derived(THEME_PRESETS.findIndex((p) =>
    p.bg === settings.themeCustomBg && p.card === settings.themeCustomCard && p.text === settings.themeCustomText))
  function applyPreset(p: { bg: string; card: string; text: string }) {
    updateSetting('themeCustomBg', p.bg); updateSetting('themeCustomCard', p.card); updateSetting('themeCustomText', p.text)
  }

  // Вигляд карток у каталозі модів — той самий перемикач, що в самому каталозі.
  type DisplayMode = 'detailed' | 'grid' | 'compact'
  const DISPLAY_KEY = 'shaurma.cbDisplayMode.v1'
  let displayMode = $state<DisplayMode>('detailed')
  try {
    const dm = localStorage.getItem(DISPLAY_KEY)
    if (dm === 'detailed' || dm === 'grid' || dm === 'compact') displayMode = dm
  } catch { /* ignore */ }
  $effect(() => {
    const on = (e: Event) => {
      const dm = (e as CustomEvent).detail
      if (dm === 'detailed' || dm === 'grid' || dm === 'compact') displayMode = dm
    }
    window.addEventListener('cb-display-mode:changed', on)
    return () => window.removeEventListener('cb-display-mode:changed', on)
  })
  function setDisplayMode(m: DisplayMode) {
    displayMode = m
    try { localStorage.setItem(DISPLAY_KEY, m) } catch { /* ignore */ }
    try { window.dispatchEvent(new CustomEvent('cb-display-mode:changed', { detail: m })) } catch { /* ignore */ }
  }
  const DISPLAY_MODES: { id: DisplayMode; icon: string; label: string }[] = [
    { id: 'detailed', icon: 'ti-layout-list', label: 'browser.view.detailed' },
    { id: 'grid', icon: 'ti-layout-grid', label: 'browser.view.grid' },
    { id: 'compact', icon: 'ti-list', label: 'browser.view.compact' },
  ]

  // ── Консоль ────────────────────────────────────────────────────────────
  function consoleFontFor(lvl: 'info' | 'warn' | 'error'): string {
    if (settings.consoleFontMode === 'perType') {
      const f = lvl === 'error' ? settings.consoleErrorFont : lvl === 'warn' ? settings.consoleWarnFont : settings.consoleInfoFont
      return consoleFontStack(f || 'mono')
    }
    return consoleFontStack(settings.consoleFontFamily || 'mono')
  }
  const consolePx = $derived(Math.max(9, Math.min(24, settings.consoleFontSize || 12)))
  const consoleNeedsFile = $derived(
    settings.consoleFontFamily === 'custom' ||
    (settings.consoleFontMode === 'perType' && [settings.consoleInfoFont, settings.consoleWarnFont, settings.consoleErrorFont].includes('custom')))
</script>

{#snippet row(key: string, o: RowOpts = {})}
  {@const def = defOf(key)}
  {#if def && condsMet(def)}
    <Setting {def} value={val(key)} onchange={(v) => updateSetting(key, v)} {...o} />
  {/if}
{/snippet}

<div class="st-root" bind:this={rootEl}>
  <header class="st-top">
    <span class="st-autosave"><i class="ti ti-circle-check"></i>{t('st.autosave')}</span>
    <div class="st-search">
      <i class="ti ti-search st-search-ic"></i>
      <input type="text" placeholder={t('st.search.ph')} bind:value={query} autocomplete="off" spellcheck="false"
             aria-label={t('st.search.ph')} onkeydown={onSearchKey}
             onfocus={() => (searchFocused = true)} onblur={() => setTimeout(() => (searchFocused = false), 150)} />
      {#if query}
        <button class="st-search-clear" onmousedown={(e) => e.preventDefault()} onclick={() => (query = '')} aria-label={t('st.search.clear')}><i class="ti ti-x"></i></button>
      {/if}
      {#if query.trim() && searchFocused}
        <div class="st-search-menu" role="listbox">
          {#each hits as h, i (h.id)}
            {@const name = highlight(t(labelKeyOf(h.id)), query)}
            <button class="st-hit" class:active={i === activeHit} role="option" aria-selected={i === activeHit}
                    onmousedown={(e) => e.preventDefault()} onmouseenter={() => (activeHit = i)} onclick={() => goHit(i)}>
              <i class="ti {settingsSectionById[h.section].icon}"></i>
              <div>
                <div class="st-hit-name">{name.pre}{#if name.mid}<mark>{name.mid}</mark>{/if}{name.post}</div>
                <div class="st-hit-path">{t(settingsSectionById[h.section].titleKey)} › {t(cardKeyOf(h.card))}</div>
              </div>
            </button>
          {:else}
            <div class="st-search-empty">{t('st.search.none')}</div>
          {/each}
        </div>
      {/if}
    </div>
  </header>

  <div class="st-layout">
    <nav class="st-nav" aria-label={t('st.title')}>
      {#each navGroups as g (g.id)}
        <div class="st-nav-group">
          <div class="st-nav-title">{t(g.titleKey)}</div>
          {#each g.sections as s (s.id)}
            <button class="st-nav-item" class:active={settingsTab === s.id} aria-current={settingsTab === s.id ? 'page' : undefined}
                    onclick={() => openSection(s.id)}>
              <i class="ti {s.icon}"></i><span>{t(s.titleKey)}</span>
            </button>
          {/each}
        </div>
      {/each}
    </nav>

    <main class="st-main" bind:this={mainEl}>
      {#key settingsTab}
      <div class="st-panel">
        <div class="st-sec-head">
          <h2>{t(section.titleKey)}</h2>
          <p>{t(section.descKey)}</p>
        </div>

        <!-- ═══════════════ ЗАГАЛЬНІ ═══════════════ -->
        {#if settingsTab === 'general'}
          <SCard icon="ti-language" tone="blue" title={t('st.c.language')}>
            {@render row('language')}
            <div class="st-row" data-setting-key="x:importLang">
              <div class="st-row-text">
                <div class="st-row-name">{t('st.x.importLang')}</div>
                <div class="st-row-hint">{t('st.x.importLang.d')}</div>
              </div>
              <div class="st-row-ctl"><button class="btn btn-ghost btn-sm" onclick={importLanguageFile}><i class="ti ti-upload"></i> {t('st.importLang.btn')}</button></div>
            </div>
            {#if customLangs.length > 0}
              <div class="st-row stack">
                <div class="st-pills">
                  {#each customLangs as cl (cl.code)}
                    <span class="st-pill sel">{cl.name}
                      <button class="st-pill-x" aria-label={t('st.importLang.remove')} title={t('st.importLang.remove')}
                              onclick={() => { removeCustomLanguage(cl.code); setLanguage(settings.language) }}><i class="ti ti-x"></i></button>
                    </span>
                  {/each}
                </div>
              </div>
            {/if}
          </SCard>

          <SCard icon="ti-refresh" tone="green" title={t('st.c.updates')}>
            {@render row('autoUpdate')}
            {@render row('updateChannel')}
          </SCard>

          <SCard icon="ti-power" title={t('st.c.startup')}>
            {@render row('startupMode')}
          </SCard>

          <SCard icon="ti-app-window" tone="violet" title={t('st.c.window')} desc={t('st.c.window.d')}>
            {@render row('closeAction')}
            {@render row('minimizeAction')}
          </SCard>

        <!-- ═══════════════ ВИГЛЯД ═══════════════ -->
        {:else if settingsTab === 'appearance'}
          <SCard id="x:theme" icon="ti-palette" tone="violet" title={t('st.c.theme')}>
            <div class="st-pad" data-setting-key="x:accent">
              <LauncherAppearance
                theme={settings.theme} accent={settings.accent}
                accentCustom={settings.accentCustom} accentCustom2={(settings as any).accentCustom2}
                accentCustomMode={(settings as any).accentCustomMode}
                themeCustomBg={settings.themeCustomBg} themeCustomCard={settings.themeCustomCard} themeCustomText={settings.themeCustomText}
                onchange={(k, v) => updateSetting(k, v)} />
            </div>
          </SCard>

          {#if settings.theme === 'custom'}
            <SCard icon="ti-color-swatch" tone="violet" title={t('st.c.customTheme')} desc={t('st.c.customTheme.d')}>
              <div class="st-pad">
                <div class="st-presets">
                  {#each THEME_PRESETS as p, i}
                    <button class="st-preset" class:sel={activePreset === i} onclick={() => applyPreset(p)}>
                      <span class="st-preset-sw"><span style="background:{p.bg}"></span><span style="background:{p.card}"></span><span style="background:{p.text}"></span></span>
                      {t('st.preset')} {i + 1}
                    </button>
                  {/each}
                </div>
              </div>
              {@render row('themeCustomBg')}
              {@render row('themeCustomCard')}
              {@render row('themeCustomText')}
            </SCard>
          {/if}

          <SCard icon="ti-typography" tone="green" title={t('st.c.font')}>
            {@render row('font')}
            {#if settings.font === 'custom'}{@render row('fontPath', { sub: true })}{/if}
            <div class="st-row" data-setting-key="x:fontsFolder">
              <div class="st-row-text">
                <div class="st-row-name">{t('st.x.fontsFolder')}</div>
                <div class="st-row-hint">{t('st.x.fontsFolder.d')}</div>
                {#if fontsDir}<div class="st-path">{fontsDir}</div>{/if}
              </div>
              <div class="st-row-ctl"><button class="btn btn-ghost btn-sm" onclick={() => App.OpenFontsFolder()}><i class="ti ti-folder-open"></i> {t('st.open')}</button></div>
            </div>
          </SCard>

          <SCard icon="ti-layout-sidebar" title={t('st.c.sidebar')}>
            <div class="st-row" data-setting-key="sidebarOnlyPinned">
              <div class="st-row-text"><div class="st-row-name">{t('st.k.sidebarOnlyPinned')}</div><div class="st-row-hint">{t('st.k.sidebarOnlyPinned.d')}</div></div>
              <div class="st-row-ctl"><button type="button" class="st-switch" class:on={sidebarOnlyPinned} role="switch" aria-checked={sidebarOnlyPinned}
                aria-label={t('st.k.sidebarOnlyPinned')} onclick={toggleSidebarOnlyPinned}></button></div>
            </div>
            <div class="st-row" data-setting-key="x:sidebarScale">
              <div class="st-row-text"><div class="st-row-name">{t('st.x.sidebarScale')}</div></div>
              <div class="st-row-ctl" style="min-width:260px">
                <div class="st-slider">
                  <input type="range" min="80" max="125" step="5" value={Math.round((sidebarScale ?? 1) * 100)}
                         aria-label={t('st.x.sidebarScale')} oninput={(e) => setSidebarScale(Number((e.currentTarget as HTMLInputElement).value) / 100)} />
                  <span class="st-slider-val">{Math.round((sidebarScale ?? 1) * 100)}%</span>
                </div>
              </div>
            </div>
            <div class="st-row" data-setting-key="x:sidebarWidth">
              <div class="st-row-text"><div class="st-row-name">{t('st.x.sidebarWidth')}</div></div>
              <div class="st-row-ctl" style="min-width:260px">
                <div class="st-slider">
                  <input type="range" min="220" max="400" step="10" value={sidebarWidth} aria-label={t('st.x.sidebarWidth')}
                         oninput={(e) => updateSetting('sidebarWidth', Number((e.currentTarget as HTMLInputElement).value))} onchange={saveSidebarWidth} />
                  <span class="st-slider-val">{sidebarWidth}px</span>
                </div>
              </div>
            </div>
            {@render row('sidebarAnimAlways')}
            {@render row('pinnedServersCompact')}
          </SCard>

          <SCard icon="ti-layout-grid" tone="blue" title={t('st.c.pages')}>
            {@render row('packBannerFull')}
            <div class="st-row" data-setting-key="x:contentView">
              <div class="st-row-text"><div class="st-row-name">{t('st.x.contentView')}</div><div class="st-row-hint">{t('st.x.contentView.d')}</div></div>
              <div class="st-row-ctl">
                <div class="st-seg" role="radiogroup" aria-label={t('st.x.contentView')}>
                  {#each DISPLAY_MODES as m (m.id)}
                    <button type="button" class:on={displayMode === m.id} role="radio" aria-checked={displayMode === m.id}
                            title={t(m.label)} aria-label={t(m.label)} onclick={() => setDisplayMode(m.id)}><i class="ti {m.icon}"></i></button>
                  {/each}
                </div>
              </div>
            </div>
          </SCard>

          <SCard icon="ti-bolt" tone="yellow" title={t('st.c.motion')} desc={t('st.c.motion.d')}>
            {@render row('animationsEnabled')}
            {@render row('performanceMode')}
            {@render row('lowMemoryMode')}
            {@render row('hardwareAcceleration')}
          </SCard>

        <!-- ═══════════════ СПОВІЩЕННЯ ═══════════════ -->
        {:else if settingsTab === 'notifications'}
          <SCard icon="ti-bell" tone="blue" title={t('st.c.notify')} desc={t('st.c.notify.d')}>
            {@render row('notifyEnabled')}
            {@render row('notifyDurationSec', { unit: t('st.unit.sec'), disabled: !settings.notifyEnabled })}
            {@render row('notifyDnd')}
            {@render row('chatMuteAll')}
          </SCard>

        <!-- ═══════════════ АКАУНТИ ═══════════════ -->
        {:else if settingsTab === 'account'}
          <AccountsCard {accounts} {activeAccountId} {skinAuthNick} {headFor} bind:dragAcc bind:dragOverRow
            {moveAccountTo} {reloginAccount} {switchAccount} {removeAccount} {openRenameAccount} {onaddaccount} />
          <div id="settings-account" data-setting-key="x:profile">
            <AccountProfilePanel
              accountName={accounts.find((a) => a.id === activeAccountId)?.username || ''}
              head={headFor(accounts.find((a) => a.id === activeAccountId)?.uuid)} />
          </div>

        <!-- ═══════════════ ГРА ═══════════════ -->
        {:else if settingsTab === 'game'}
          <SCard icon="ti-window-maximize" tone="orange" title={t('st.c.window')} desc={t('st.c.gameWindow.d')}>
            {@render row('fullscreen')}
            {@render row('windowWidth', { unit: 'px', disabled: !!settings.fullscreen })}
            {@render row('windowHeight', { unit: 'px', disabled: !!settings.fullscreen })}
          </SCard>

          <SCard icon="ti-app-window" tone="violet" title={t('st.c.whilePlaying')} desc={t('st.c.whilePlaying.d')}>
            {@render row('gameLaunchAction')}
            {@render row('gameLaunchDelaySec', { unit: t('st.unit.sec'), sub: true })}
            {@render row('gameExitAction')}
            {@render row('restoreOnCrash', { sub: true })}
          </SCard>

          <SCard icon="ti-shield-check" tone="yellow" title={t('st.c.confirm')}>
            {@render row('confirmOnStop')}
            {@render row('showRamWarnings')}
            {@render row('tempModsDeleteByDefault')}
          </SCard>

          <SCard icon="ti-server" tone="green" title={t('st.c.serverChecks')} desc={t('st.c.serverChecks.d')}>
            {@render row('showServerWarnings')}
            {#if settings.showServerWarnings}
              {@render row('serverWarnIgnoreRam', { invert: true, sub: true })}
              {@render row('serverWarnIgnoreJava', { invert: true, sub: true })}
              {@render row('serverWarnIgnoreDisk', { invert: true, sub: true })}
              {@render row('serverWarnIgnoreCrash', { invert: true, sub: true })}
            {/if}
          </SCard>

          <SCard icon="ti-clock" tone="blue" title={t('st.c.playtime')}>
            {@render row('recordGameTime')}
            {@render row('showGameTime')}
            {@render row('showGameTimeOnPage')}
            {@render row('showSessionTime')}
            {@render row('showTotalGameTime')}
            {@render row('gameTimeInHours')}
          </SCard>

        <!-- ═══════════════ JAVA ТА ЗАПУСК ═══════════════ -->
        {:else if settingsTab === 'java'}
          <SCard icon="ti-cpu" tone="green" title={t('st.c.memory')}>
            {@render row('maxRAM')}
          </SCard>
          <SCard icon="ti-coffee" tone="orange" title={t('st.c.jvm')}>
            {@render row('javaArgs')}
          </SCard>
          <SCard icon="ti-device-desktop-bolt" tone="violet" title={t('st.c.gpu')}>
            {@render row('useDedicatedGPU')}
          </SCard>
          <SCard icon="ti-terminal" title={t('st.c.commands')} desc={t('st.c.commands.d')}>
            {@render row('preLaunchCommand')}
            {@render row('wrapperCommand')}
            {@render row('postExitCommand')}
            {@render row('envVars')}
          </SCard>

        <!-- ═══════════════ НОВІ ЗБІРКИ ═══════════════ -->
        {:else if settingsTab === 'defaults'}
          <SCard icon="ti-package" title={t('st.c.newPacks')}>
            {@render row('autoDownloadAfterCreate')}
          </SCard>
          <div data-setting-key="x:mcOptions"><MinecraftDefaultsTab /></div>
          <AutoPacksCard {autoPacks} busy={autoPacksBusy} onToggle={onAutoPacksToggle} onDelete={onAutoPacksDelete} onOpen={onAutoPacksOpen} />
          <div data-setting-key="x:import"><MigrationSettingsWidget /></div>

        <!-- ═══════════════ КОНСОЛЬ ═══════════════ -->
        {:else if settingsTab === 'console'}
          <SCard icon="ti-terminal-2" title={t('st.c.show')}>
            {@render row('showConsoleOnLaunch')}
            {@render row('showConsoleOnCrash')}
            {@render row('showConsoleOnClose')}
          </SCard>

          <SCard icon="ti-typography" tone="violet" title={t('st.c.look')}>
            {@render row('consoleFontFamily')}
            {@render row('consoleFontMode')}
            {#if settings.consoleFontMode === 'perType'}
              {@render row('consoleInfoFont', { sub: true })}
              {@render row('consoleWarnFont', { sub: true })}
              {@render row('consoleErrorFont', { sub: true })}
            {/if}
            {#if consoleNeedsFile}{@render row('consoleFontPath', { sub: true })}{/if}
            {@render row('consoleFontSize', { unit: 'px' })}
            {@render row('consoleWrap')}
            {@render row('consoleMaxLines')}
            {@render row('consoleInfoColor')}
            {@render row('consoleWarnColor')}
            {@render row('consoleErrorColor')}
            <div class="st-row stack">
              <div class="st-row-text"><div class="st-row-name">{t('st.console.preview')}</div></div>
              <div class="st-row-ctl">
                <div class="st-console" style="font-size:{consolePx}px;--cp-h:{consolePx + 10}px">
                  <div style="color:{settings.consoleInfoColor || '#8fd3ff'};font-family:{consoleFontFor('info')}"><span class="ts">12:00:01</span><span class="lv">INFO</span><span>{t('st.console.sampleInfo')}</span></div>
                  <div style="color:{settings.consoleWarnColor || '#ffd166'};font-family:{consoleFontFor('warn')}"><span class="ts">12:00:05</span><span class="lv">WARN</span><span>Can't keep up! Is the server overloaded?</span></div>
                  <div style="color:{settings.consoleErrorColor || '#ff6b6b'};font-family:{consoleFontFor('error')}"><span class="ts">12:00:09</span><span class="lv">ERROR</span><span>java.lang.OutOfMemoryError: Java heap space</span></div>
                </div>
              </div>
            </div>
          </SCard>

          <SCard icon="ti-file-text" tone="blue" title={t('st.c.logs')}>
            {@render row('saveLogs')}
          </SCard>

          <SCard icon="ti-bug" tone="red" title={t('st.c.crash')}>
            {@render row('disableAutoRepair', { invert: true })}
            {@render row('consoleAI')}
            {#if settings.consoleAI}{@render row('consoleAIKey', { sub: true })}{/if}
          </SCard>

        <!-- ═══════════════ ЗАВАНТАЖЕННЯ ТА МОДИ ═══════════════ -->
        {:else if settingsTab === 'downloads'}
          <SCard icon="ti-gauge" tone="green" title={t('st.c.speed')}>
            {@render row('maxConcurrentDownloads')}
            {@render row('maxRetries')}
            {@render row('httpTimeoutSec', { unit: t('st.unit.sec') })}
          </SCard>
          <SCard icon="ti-puzzle" tone="orange" title={t('st.c.mods')}>
            {@render row('allowAlphaBetaModUpdates')}
            {@render row('useModrinthForBlocked')}
            {@render row('disableModSuggestions', { invert: true })}
          </SCard>
          <SCard icon="ti-flame" tone="orange" title={t('st.c.curseforge')}>
            {@render row('curseForgeApiKey')}
          </SCard>

        <!-- ═══════════════ ПРИВАТНІСТЬ ТА СЕРВІСИ ═══════════════ -->
        {:else if settingsTab === 'privacy'}
          <SCard icon="ti-hanger" tone="blue" title={t('st.c.skins')}>
            {@render row('disableSkins', { invert: true })}
            {#if !settings.disableSkins}{@render row('disableMojangSkinFallback', { invert: true, sub: true })}{/if}
          </SCard>
          <SCard icon="ti-network" tone="green" title={t('st.c.p2p')}>
            {@render row('disableP2P', { invert: true })}
          </SCard>
          <SCard icon="ti-brand-discord" tone="violet" title={t('st.c.discord')} desc={settings.discordRpcEnabled ? t('st.c.discord.d') : ''}>
            {@render row('discordRpcEnabled')}
            {#if settings.discordRpcEnabled}
              {@render row('discordRPCDetails', { sub: true })}
              {@render row('discordRPCState', { sub: true })}
            {/if}
          </SCard>
          <SCard icon="ti-cloud" tone="blue" title={t('st.c.cloud')}>
            {@render row('disableCloudPlaytime', { invert: true })}
          </SCard>

        <!-- ═══════════════ ТЕКИ ТА ЯРЛИКИ ═══════════════ -->
        {:else if settingsTab === 'folders'}
          <FoldersSection {folderPaths} {folderRows} {accounts} {loadFolderPaths} {setFolder} {resetBaseDir} />

        <!-- ═══════════════ СХОВИЩЕ ═══════════════ -->
        {:else if settingsTab === 'storage'}
          <StorageSection {settings} {settingsSchema} {updateSetting}
            {storageInfo} {storageScan} {storageLoading} {storageVersions} {storagePacks} {storageSuggestions}
            {orphanLibs} {orphanLibsBusy} {orphanAssets} {orphanAssetsBusy}
            {javaBusy} {javaProgress} {unusedJava} {versionsBusy}
            bind:versionsModalOpen bind:javaModalOpen bind:orphanLibsModalOpen bind:orphanAssetsModalOpen
            {loadStorageInfo} {clearCache} {clearPackLogs} {deletePackFromStorage}
            {openVersionsModal} {analyzeStorageVersions} {cleanupUnusedVersions}
            {openJavaModal} {analyzeUnusedJava} {cleanupUnusedJava}
            {openOrphanLibsModal} {analyzeOrphanLibs} {cleanupOrphanLibs}
            {openOrphanAssetsModal} {analyzeOrphanAssets} {cleanupOrphanAssets} {fmtPlaytime} />

        <!-- ═══════════════ ПРО ЛАУНЧЕР ═══════════════ -->
        {:else if settingsTab === 'about'}
          <AboutSection {settings} {aboutSys} {aboutVersion} {updateResult} {updatingCheck} {updateStarting} {exportingErrors}
            {checkUpdates} {updateNow} {exportLauncherErrors} />
        {/if}
      </div>
      {/key}
    </main>
  </div>
</div>
