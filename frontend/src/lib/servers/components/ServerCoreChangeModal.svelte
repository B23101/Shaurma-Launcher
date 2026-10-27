<script lang="ts">
  // ── ServerCoreChangeModal — зміна ядра/версії існуючого сервера ─────────
  //
  // Один екран: ядро → версія гри → версія ядра. Ті самі контроли, що в
  // майстрі створення сервера (CORE_META-іконки + SelectField), тож зміна
  // ядра виглядає й поводиться так само, як створення — жодного власного
  // «другого» стилю вибору версії.
  //
  // Світи, моди, плагіни, конфіги й server.properties НЕ чіпаються: бекенд
  // (App.ChangeServerCore) зносить лише core-footprint (server.jar,
  // libraries/, launch_config.json...) і ставить нове ядро. Сервер мусить
  // бути зупинений — інакше Java тримає файли відкритими.
  import { App, on } from '../../wails'
  import { toast } from '../../toast.svelte'
  import { t } from '../../i18n.svelte'
  import type { MCVersionEntry, ServerView, ServerInstallProgress } from '../../types'
  import { CORE_META } from '../constants'
  import SelectField from '../../SelectField.svelte'
  import UiModal from '../../UiModal.svelte'

  let {
    srv,
    open = false,
    running = false,
    onclose = () => {},
    onchanged = (_s: ServerView) => {},
  }: {
    srv: ServerView
    open?: boolean
    running?: boolean
    onclose?: () => void
    onchanged?: (s: ServerView) => void
  } = $props()

  let core = $state(srv.core)
  let mcVersion = $state(srv.mcVersion)
  let loaderVer = $state(srv.loaderVer)

  let mcVersions = $state<MCVersionEntry[]>([])
  let mcLoading = $state(false)
  let cores = $state<string[]>([])
  let versions = $state<{ version: string; stable: boolean; recommended: boolean }[]>([])
  let versionsLoading = $state(false)
  let showSnapshots = $state(false)

  let working = $state(false)
  let progress = $state<ServerInstallProgress | null>(null)

  const isProxy = $derived(core === 'velocity')
  // Версії ядра є в усіх, крім vanilla (у неї просто «версія гри»).
  const needsCoreVersion = $derived(core !== 'vanilla')

  const mcList = $derived(mcVersions.filter(v => v.type === 'release' || (v.type === 'snapshot' && showSnapshots)))
  const mcLabel = $derived(
    isProxy ? t('servers.core.noMcVersion')
      : (mcList.find(v => v.id === mcVersion)?.id ?? mcVersion ?? ''),
  )
  const coreMeta = $derived(CORE_META[core])

  const changed = $derived(
    core !== srv.core || (!isProxy && mcVersion !== srv.mcVersion) ||
    (needsCoreVersion && loaderVer !== srv.loaderVer),
  )

  // ── Завантаження списків ────────────────────────────────────────────
  async function loadMC() {
    mcLoading = true
    try {
      mcVersions = (await App.GetServerMCVersions()) ?? []
    } catch {
      mcVersions = []
    } finally {
      mcLoading = false
    }
  }

  async function loadCores(mc: string) {
    if (!mc) { cores = []; return }
    try {
      cores = (await App.GetServerAvailableCores(mc)) ?? []
    } catch {
      cores = []
    }
  }

  async function loadVersions() {
    versionsLoading = true
    try {
      const raw = await App.GetServerCoreVersions(core, isProxy ? '' : mcVersion)
      versions = (raw ?? []).map(v => ({
        version: v.version,
        stable: v.stable ?? true,
        recommended: v.recommended ?? false,
      }))
      if (needsCoreVersion && !versions.some(v => v.version === loaderVer)) {
        loaderVer = (versions.find(v => v.recommended) ?? versions[0])?.version ?? ''
      }
      if (!needsCoreVersion) loaderVer = ''
    } catch {
      versions = []
    } finally {
      versionsLoading = false
    }
  }

  // При відкритті — стартуємо з поточного ядра сервера.
  $effect(() => {
    if (!open) return
    core = srv.core
    mcVersion = srv.mcVersion
    loaderVer = srv.loaderVer
    void (async () => {
      await loadMC()
      await loadCores(srv.core === 'velocity' ? '' : srv.mcVersion)
      await loadVersions()
    })()
  })

  // Прогрес встановлення нового ядра (ті самі події, що при старті сервера).
  $effect(() => {
    if (!open) return
    const off = on('server:install-progress', (d: any) => {
      if (d.serverId !== srv.id) return
      progress = { stage: d.stage || 'core', percent: d.percent ?? -1, message: d.message || '' }
    })
    return () => off?.()
  })

  async function onCoreChange(v: string) {
    core = v
    if (v === 'velocity') {
      loaderVer = ''
      await loadVersions()
      return
    }
    loaderVer = ''
    await loadVersions()
  }

  async function onMCChange(v: string) {
    mcVersion = v
    loaderVer = ''
    await loadCores(v)
    await loadVersions()
  }

  const coreOptions = $derived(
    (cores.includes(core) ? cores : [core, ...cores]).filter(Boolean).map(id => ({
      value: id,
      label: CORE_META[id]?.label ?? id,
      icon: CORE_META[id]?.ti || undefined,
      color: CORE_META[id]?.color,
    })),
  )

  async function apply() {
    if (working || running || !changed) return
    working = true
    progress = null
    try {
      const updated = await App.ChangeServerCore(srv.id, {
        core,
        mcVersion: isProxy ? '' : mcVersion,
        loaderVer,
        javaMajor: 0,
      })
      onchanged(updated)
      toast(t('servers.core.changed'), 'success')
      onclose()
    } catch (e: any) {
      toast(String(e?.message ?? e), 'error')
    } finally {
      working = false
      progress = null
    }
  }
</script>

<UiModal
  {open}
  title={t('servers.core.title')}
  subtitle={t('servers.core.subtitle')}
  icon="ti-cpu-2"
  color={coreMeta?.color || 'var(--orange)'}
  size="md"
  locked={working}
  {onclose}
>
  <div class="scm">
    {#if running}
      <div class="scm-note warn"><i class="ti ti-alert-triangle"></i> {t('servers.core.running')}</div>
    {/if}

    <div class="scm-field">
      <div class="scm-label">{t('servers.core.core')}</div>
      <SelectField
        value={core}
        options={coreOptions}
        onchange={onCoreChange}
        disabled={working}
        ariaLabel={t('servers.core.core')}
        minWidth="100%"
      />
    </div>

    {#if !isProxy}
      <div class="scm-field">
        <div class="scm-label">
          {t('servers.core.mcVersion')}
          <label class="scm-snap">
            <input type="checkbox" bind:checked={showSnapshots} disabled={working} />
            {t('servers.core.snapshots')}
          </label>
        </div>
        <SelectField
          value={mcVersion}
          options={mcList.map(v => ({ value: v.id, label: v.id }))}
          onchange={onMCChange}
          disabled={working || mcLoading}
          placeholder={mcLoading ? '…' : ''}
          ariaLabel={t('servers.core.mcVersion')}
          minWidth="100%"
        />
      </div>
    {/if}

    {#if needsCoreVersion}
      <div class="scm-field">
        <div class="scm-label">{t('servers.core.coreVersion')}</div>
        <SelectField
          value={loaderVer}
          options={versions.map(v => ({
            value: v.version,
            label: v.version,
            badgeIcon: v.recommended ? 'ti-star-filled' : undefined,
            badgeTitle: v.recommended ? t('servers.core.recommended') : undefined,
          }))}
          onchange={(v) => { loaderVer = v }}
          disabled={working || versionsLoading || versions.length === 0}
          placeholder={versionsLoading ? '…' : (versions.length === 0 ? t('servers.core.noVersions') : '')}
          ariaLabel={t('servers.core.coreVersion')}
          minWidth="100%"
        />
      </div>
    {/if}

    <div class="scm-note"><i class="ti ti-info-circle"></i> {t('servers.core.warning')}</div>

    {#if mcLabel && !isProxy}
      <div class="scm-current">
        <span>{t('servers.core.current')}:</span>
        <b>{CORE_META[srv.core]?.label ?? srv.core}</b> · {srv.mcVersion || '—'}{srv.loaderVer ? ` · ${srv.loaderVer}` : ''}
      </div>
    {/if}

    {#if working}
      <div class="scm-progress">
        <div class="scm-progress-head">
          <i class="ti ti-loader spin"></i>
          <span>{progress?.message || t('servers.core.changing')}</span>
        </div>
        {#if progress && progress.percent >= 0}
          <div class="scm-bar"><div class="scm-bar-fill" style="width:{progress.percent}%"></div></div>
        {/if}
      </div>
    {/if}
  </div>

  {#snippet footer()}
    <button class="btn btn-ghost" onclick={onclose} disabled={working}>{t('cancel')}</button>
    <button class="btn btn-primary" onclick={apply} disabled={working || running || !changed || (needsCoreVersion && versions.length === 0)}>
      {#if working}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-replace"></i>{/if}
      {t('servers.core.apply')}
    </button>
  {/snippet}
</UiModal>

<style>
  .scm { display: flex; flex-direction: column; gap: 14px; }
  .scm-field { display: flex; flex-direction: column; gap: 6px; }
  .scm-label {
    display: flex; align-items: center; justify-content: space-between; gap: 8px;
    font: 700 11px/1 var(--font-ui); color: var(--text-mute);
    text-transform: uppercase; letter-spacing: .04em;
  }
  .scm-snap {
    display: inline-flex; align-items: center; gap: 5px; text-transform: none;
    letter-spacing: 0; font: 500 11px/1 var(--font-ui); color: var(--text-dim);
    cursor: pointer;
  }
  .scm-note {
    display: flex; gap: 9px; align-items: flex-start;
    padding: 10px 12px; border-radius: 9px; border: 1px solid var(--border);
    background: var(--hover, rgba(255,255,255,.03));
    font: 400 11.5px/1.5 var(--font-ui); color: var(--text-mute);
  }
  .scm-note i { font-size: 15px; flex: 0 0 auto; margin-top: 1px; color: var(--text-dim); }
  .scm-note.warn { border-color: rgba(234,179,8,.4); background: rgba(234,179,8,.08); color: var(--text); }
  .scm-note.warn i { color: #eab308; }
  .scm-current { font: 500 11.5px/1.4 var(--font-ui); color: var(--text-dim); }
  .scm-current b { color: var(--text); }
  .scm-progress { display: flex; flex-direction: column; gap: 8px; }
  .scm-progress-head { display: flex; align-items: center; gap: 8px; font: 600 12px/1.3 var(--font-ui); color: var(--text); }
  .scm-progress-head i { color: var(--orange); }
  .scm-bar { height: 6px; border-radius: 4px; background: var(--bg-input); overflow: hidden; }
  .scm-bar-fill { height: 100%; background: var(--orange); transition: width .2s; }
</style>
