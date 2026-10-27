<script lang="ts">
  // AboutSection.svelte — «Про лаунчер»: версія, оновлення, куди писати про помилки.
  import { t } from '../i18n.svelte'
  import { App } from '../wails'
  import SCard from './SCard.svelte'
  import appIcon from '../../assets/images/appicon.png'
  import type { Settings, SystemInfo, UpdateInfo } from '../types'

  let {
    settings, aboutSys, aboutVersion, updateResult, updatingCheck, updateStarting, exportingErrors,
    checkUpdates, updateNow, exportLauncherErrors,
  }: {
    settings: Settings
    aboutSys: SystemInfo
    aboutVersion: string
    updateResult: UpdateInfo | null
    updatingCheck: boolean
    updateStarting: boolean
    exportingErrors: boolean
    checkUpdates: () => void
    updateNow: () => void
    exportLauncherErrors: () => void
  } = $props()

  const DISCORD_URL = 'https://discord.gg/MffED5js3Z'

  function lastChecked(iso?: string): string {
    if (!iso) return '—'
    const d = new Date(iso)
    return isNaN(d.getTime()) ? '—' : d.toLocaleString(settings.language === 'en' ? 'en-US' : 'uk-UA')
  }
  const version = $derived(aboutVersion || '2.0.0')
</script>

<SCard id="x:version" icon="ti-info-circle" title={t('st.c.app')} flush>
  <div class="st-row" style="padding:20px">
    <img src={appIcon} alt="" draggable="false" style="width:56px;height:56px;border-radius:14px;flex:none" />
    <div class="st-row-text">
      <div class="st-row-name" style="font-size:17px;font-weight:800">{t('st.about.name')}</div>
      <div class="st-row-hint">{t('st.about.tagline')}</div>
    </div>
    <span class="st-tag on" style="font-family:var(--font-mono)">{version}</span>
  </div>
  <div class="st-row"><div class="st-row-text"><div class="st-row-name">{t('st.about.system')}</div></div>
    <div class="st-row-ctl"><span class="st-row-hint" style="margin:0">{aboutSys.os || t('st.about.unknown')}</span></div></div>
  <div class="st-row"><div class="st-row-text"><div class="st-row-name">{t('st.about.arch')}</div></div>
    <div class="st-row-ctl"><span class="st-row-hint" style="margin:0">{aboutSys.arch || 'x64'}</span></div></div>
  <div class="st-row"><div class="st-row-text"><div class="st-row-name">{t('st.about.channel')}</div></div>
    <div class="st-row-ctl"><span class="st-tag">{settings.updateChannel === 'beta' ? t('st.o.updateChannel.beta') : t('st.o.updateChannel.stable')}</span></div></div>
</SCard>

<SCard id="x:updatesCheck" icon="ti-refresh" tone="blue" title={t('st.c.updatesCheck')}>
  <div class="st-row">
    <div class="st-row-text">
      {#if updateResult?.available}
        <div class="st-row-name">{t('st.about.available', { version: updateResult.version })}</div>
      {:else}
        <div class="st-row-name">{t('st.about.upToDate')}</div>
      {/if}
      <div class="st-row-hint">{t('st.about.lastChecked')}: {lastChecked((settings as any).lastUpdateCheck)}</div>
    </div>
    <div class="st-row-ctl">
      {#if updateResult?.available && updateResult.changelogUrl}
        <button class="btn btn-ghost btn-sm" onclick={() => App.OpenExternal(updateResult!.changelogUrl)}><i class="ti ti-notes"></i> {t('st.about.changelog')}</button>
      {/if}
      {#if updateResult?.available}
        <button class="btn btn-primary btn-sm" onclick={updateNow} disabled={updateStarting}>
          <i class="ti {updateStarting ? 'ti-loader-2 spin' : 'ti-download'}"></i> {updateStarting ? t('st.about.updating') : t('st.about.install')}
        </button>
      {:else}
        <button class="btn btn-ghost btn-sm" onclick={checkUpdates} disabled={updatingCheck}>
          <i class="ti {updatingCheck ? 'ti-loader-2 spin' : 'ti-refresh'}"></i> {t('st.about.check')}
        </button>
      {/if}
    </div>
  </div>
</SCard>

<SCard icon="ti-lifebuoy" tone="violet" title={t('st.c.help')}>
  <div class="st-row" data-setting-key="x:bug">
    <div class="st-row-text">
      <div class="st-row-name">{t('st.x.bug')}</div>
      <div class="st-row-hint">{t('st.x.bug.d')}</div>
    </div>
    <div class="st-row-ctl">
      <button class="btn btn-ghost btn-sm" onclick={() => App.OpenExternal(DISCORD_URL)}><i class="ti ti-brand-discord"></i> {t('st.about.discord')}</button>
    </div>
  </div>
  <div class="st-row" data-setting-key="x:errorLog">
    <div class="st-row-text">
      <div class="st-row-name">{t('st.x.errorLog')}</div>
      <div class="st-row-hint">{t('st.x.errorLog.d')}</div>
    </div>
    <div class="st-row-ctl">
      <button class="btn btn-ghost btn-sm" onclick={exportLauncherErrors} disabled={exportingErrors}>
        <i class="ti {exportingErrors ? 'ti-loader-2 spin' : 'ti-share'}"></i> {t('st.about.shareLog')}
      </button>
    </div>
  </div>
</SCard>
