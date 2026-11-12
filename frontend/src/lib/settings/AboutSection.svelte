<script lang="ts">
  // AboutSection.svelte — «Про лаунчер»: версія, оновлення, куди писати про помилки.
  //
  // Раніше це були три сірі списки «назва — значення» без жодного акценту: ні
  // обличчя програми, ні зрозумілого стану оновлення. Тепер: герой-картка (іконка
  // зі свічінням, назва, версія, стан оновлення з головною дією), плитки
  // «система / архітектура / канал» і плитки-посилання для допомоги.
  // data-setting-key збережені — пошук налаштувань і далі підсвічує потрібний блок.
  import { t } from '../i18n.svelte'
  import { App } from '../wails'
  import { toast } from '../toast.svelte'
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

  const ISSUES_URL = 'https://github.com/B23101/Shaurma-Launcher/issues/'
  const DONATE_URL = 'https://donatello.to/shaurm_donate'
  const DISCORD_URL = 'https://discord.gg/MffED5js3Z'

  function lastChecked(iso?: string): string {
    if (!iso) return '—'
    const d = new Date(iso)
    return isNaN(d.getTime()) ? '—' : d.toLocaleString(settings.language === 'en' ? 'en-US' : 'uk-UA')
  }
  const version = $derived(aboutVersion || '2.0.0')
  const hasUpdate = $derived(!!updateResult?.available)
  const isBeta = $derived(settings.updateChannel === 'beta')

  // Іконка ОС за назвою (aboutSys.os — «Windows 11», «Ubuntu 24.04», «macOS 15»…).
  const osIcon = $derived.by(() => {
    const o = (aboutSys.os || '').toLowerCase()
    if (o.includes('win')) return 'ti-brand-windows'
    if (o.includes('mac') || o.includes('darwin')) return 'ti-brand-apple'
    if (o.includes('ubuntu')) return 'ti-brand-ubuntu'
    if (o.includes('debian')) return 'ti-brand-debian'
    if (o.includes('linux') || o.includes('arch') || o.includes('fedora')) return 'ti-brand-linux'
    return 'ti-device-desktop'
  })

  async function openComponents() {
    try { await App.OpenComponentsFolder() } catch (e) { toast(String(e), 'error') }
  }

  const links = $derived([
    { key: 'x:bug', icon: 'ti-brand-github', tone: 'gray', title: t('st.x.bug'), desc: t('st.x.bug.d'), cta: t('st.about.issues'), run: () => App.OpenExternal(ISSUES_URL), busy: false },
    { key: 'x:community', icon: 'ti-brand-discord', tone: 'violet', title: t('st.x.community'), desc: t('st.x.community.d'), cta: t('st.about.discord'), run: () => App.OpenExternal(DISCORD_URL), busy: false },
    { key: 'x:errorLog', icon: 'ti-share', tone: 'blue', title: t('st.x.errorLog'), desc: t('st.x.errorLog.d'), cta: t('st.about.shareLog'), run: exportLauncherErrors, busy: exportingErrors },
    { key: 'x:donate', icon: 'ti-heart', tone: 'red', title: t('st.x.donate'), desc: t('st.x.donate.d'), cta: t('st.about.donate'), run: () => App.OpenExternal(DONATE_URL), busy: false },
  ])
</script>

<div class="abt">
  <!-- ══ Герой ══ -->
  <section class="abt-hero" class:upd={hasUpdate} data-setting-key="x:version">
    <span class="abt-glow" aria-hidden="true"></span>
    <span class="abt-grid" aria-hidden="true"></span>

    <div class="abt-id">
      <span class="abt-icon">
        <img src={appIcon} alt="" draggable="false" />
      </span>
      <div class="abt-title">
        <h2>{t('st.about.name')}</h2>
        <p>{t('st.about.tagline')}</p>
        <div class="abt-chips">
          <span class="abt-ver">v{version.replace(/^v/i, '')}</span>
          {#if isBeta}<span class="abt-chip beta"><i class="ti ti-flask"></i> {t('st.o.updateChannel.beta')}</span>{/if}
        </div>
      </div>
    </div>

    <!-- Стан оновлення: головна дія героя -->
    <div class="abt-update" data-setting-key="x:updatesCheck">
      <div class="abt-upd-ic" class:ok={!hasUpdate}>
        <i class="ti {hasUpdate ? 'ti-arrow-big-up-lines' : 'ti-circle-check'}"></i>
      </div>
      <div class="abt-upd-tx">
        <b>{hasUpdate ? t('st.about.available', { version: updateResult!.version }) : t('st.about.upToDate')}</b>
        <small>{t('st.about.lastChecked')}: {lastChecked((settings as any).lastUpdateCheck)}</small>
      </div>
      <div class="abt-upd-act">
        {#if hasUpdate && updateResult?.changelogUrl}
          <button class="btn btn-ghost btn-sm" onclick={() => App.OpenExternal(updateResult!.changelogUrl)}>
            <i class="ti ti-notes"></i> {t('st.about.changelog')}
          </button>
        {/if}
        {#if hasUpdate}
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
  </section>

  <!-- ══ Середовище ══ -->
  <div class="abt-facts">
    <div class="abt-fact">
      <span class="abt-fact-ic"><i class="ti {osIcon}"></i></span>
      <div><small>{t('st.about.system')}</small><b>{aboutSys.os || t('st.about.unknown')}</b></div>
    </div>
    <div class="abt-fact">
      <span class="abt-fact-ic"><i class="ti ti-cpu"></i></span>
      <div><small>{t('st.about.arch')}</small><b>{aboutSys.arch || 'x64'}</b></div>
    </div>
    <div class="abt-fact">
      <span class="abt-fact-ic" class:beta={isBeta}><i class="ti {isBeta ? 'ti-flask' : 'ti-shield-check'}"></i></span>
      <div><small>{t('st.about.channel')}</small><b>{isBeta ? t('st.o.updateChannel.beta') : t('st.o.updateChannel.stable')}</b></div>
    </div>
    <button class="abt-fact abt-fact-btn" onclick={openComponents}>
      <span class="abt-fact-ic"><i class="ti ti-folder-open"></i></span>
      <div><small>{t('st.about.componentsFolder')}</small><b>{t('st.open')}<i class="ti ti-arrow-up-right"></i></b></div>
    </button>
  </div>

  <!-- ══ Допомога ══ -->
  <div class="abt-sec">{t('st.c.help')}</div>
  <div class="abt-links">
    {#each links as l (l.key)}
      <button class="abt-link {l.tone}" data-setting-key={l.key} onclick={() => l.run()} disabled={l.busy}>
        <span class="abt-link-ic"><i class="ti {l.busy ? 'ti-loader-2 spin' : l.icon}"></i></span>
        <span class="abt-link-tx">
          <b>{l.title}</b>
          <small>{l.desc}</small>
        </span>
        <span class="abt-link-cta">{l.cta}<i class="ti ti-arrow-up-right"></i></span>
      </button>
    {/each}
  </div>
</div>

<style>
  .abt { display: flex; flex-direction: column; gap: 14px; }
  .abt > * { flex-shrink: 0; } /* hero має overflow:hidden → інакше flex стискає його й обрізає низ */
  .abt i.spin { animation: abt-spin 1s linear infinite; }
  @keyframes abt-spin { to { transform: rotate(360deg); } }

  /* ── Герой ── */
  .abt-hero {
    position: relative; overflow: hidden; border-radius: 20px; padding: 26px 26px 22px;
    border: 1px solid color-mix(in srgb, var(--orange) 26%, var(--border));
    background: linear-gradient(160deg, color-mix(in srgb, var(--orange) 10%, var(--bg-card)) 0%, var(--bg-card) 62%);
    display: flex; flex-direction: column; gap: 22px; isolation: isolate;
  }
  .abt-glow {
    position: absolute; z-index: -1; width: 380px; height: 380px; right: -110px; top: -190px; border-radius: 50%;
    background: radial-gradient(circle, rgba(var(--orange-rgb), .32), transparent 68%);
    animation: abt-breathe 6s ease-in-out infinite;
  }
  @keyframes abt-breathe { 0%, 100% { opacity: .75; transform: scale(1); } 50% { opacity: 1; transform: scale(1.08); } }
  /* Тонка «зошитна» сітка — додає фактури, не відволікаючи; згасає до низу. */
  .abt-grid {
    position: absolute; inset: 0; z-index: -1; opacity: .5; pointer-events: none;
    background-image: linear-gradient(var(--border) 1px, transparent 1px), linear-gradient(90deg, var(--border) 1px, transparent 1px);
    background-size: 28px 28px;
    -webkit-mask-image: linear-gradient(180deg, #000, transparent 75%); mask-image: linear-gradient(180deg, #000, transparent 75%);
  }

  .abt-id { display: flex; align-items: center; gap: 20px; min-width: 0; }
  .abt-icon {
    position: relative; flex: none; width: 88px; height: 88px; border-radius: 24px; display: grid; place-items: center;
    background: var(--bg-input); border: 1px solid var(--border-hi);
    box-shadow: 0 14px 34px -12px rgba(var(--orange-rgb), .55), var(--elev-2);
  }
  .abt-icon img { width: 66px; height: 66px; object-fit: contain; filter: drop-shadow(0 4px 8px rgba(0,0,0,.35)); }
  .abt-title { min-width: 0; display: flex; flex-direction: column; gap: 5px; }
  .abt-title h2 { margin: 0; font: 800 28px/1.1 var(--font-ui); letter-spacing: -.4px; color: var(--text); }
  .abt-title p { margin: 0; font: 500 13.5px/1.4 var(--font-ui); color: var(--text-mute); }
  .abt-chips { display: flex; align-items: center; gap: 8px; margin-top: 6px; flex-wrap: wrap; }
  .abt-ver {
    padding: 4px 10px; border-radius: 999px; font: 700 12px/1 var(--font-mono);
    color: var(--orange); background: rgba(var(--orange-rgb), .14); border: 1px solid rgba(var(--orange-rgb), .3);
  }
  .abt-chip { display: inline-flex; align-items: center; gap: 5px; padding: 4px 10px; border-radius: 999px; font: 700 11.5px/1 var(--font-ui); }
  .abt-chip.beta { color: var(--yellow); background: rgba(245, 158, 11, .14); border: 1px solid rgba(245, 158, 11, .3); }

  /* ── Стан оновлення ── */
  .abt-update {
    display: flex; align-items: center; gap: 14px; padding: 12px 14px; border-radius: 14px;
    background: color-mix(in srgb, var(--bg-input) 70%, transparent); border: 1px solid var(--border);
    -webkit-backdrop-filter: blur(6px); backdrop-filter: blur(6px);
  }
  .abt-hero.upd .abt-update { border-color: rgba(var(--orange-rgb), .45); background: rgba(var(--orange-rgb), .09); }
  .abt-upd-ic {
    width: 38px; height: 38px; flex: none; border-radius: 12px; display: grid; place-items: center; font-size: 20px;
    color: var(--orange); background: rgba(var(--orange-rgb), .16);
  }
  .abt-upd-ic.ok { color: var(--green); background: rgba(16, 185, 129, .14); }
  .abt-upd-tx { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 3px; }
  .abt-upd-tx b { font: 700 14px/1.2 var(--font-ui); color: var(--text); }
  .abt-upd-tx small { font: 500 12px/1.3 var(--font-ui); color: var(--text-mute); }
  .abt-upd-act { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; justify-content: flex-end; }

  /* ── Плитки середовища ── */
  .abt-facts { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 10px; }
  .abt-fact {
    display: flex; align-items: center; gap: 12px; padding: 13px 14px; border-radius: 16px; text-align: left;
    background: var(--bg-card); border: 1px solid var(--border); color: inherit; font: inherit;
  }
  .abt-fact > div { min-width: 0; display: flex; flex-direction: column; gap: 4px; }
  .abt-fact small { font: 600 11px/1 var(--font-ui); letter-spacing: .4px; text-transform: uppercase; color: var(--text-dim); }
  .abt-fact b { display: inline-flex; align-items: center; gap: 4px; font: 700 14px/1.2 var(--font-ui); color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .abt-fact b i { font-size: 14px; color: var(--text-mute); }
  .abt-fact-ic {
    width: 36px; height: 36px; flex: none; border-radius: 11px; display: grid; place-items: center; font-size: 18px;
    color: var(--text-mute); background: var(--hover);
  }
  .abt-fact-ic.beta { color: var(--yellow); background: rgba(245, 158, 11, .14); }
  .abt-fact-btn { cursor: pointer; transition: border-color .15s, background .15s, transform .15s; }
  .abt-fact-btn:hover { border-color: rgba(var(--orange-rgb), .45); background: color-mix(in srgb, var(--orange) 5%, var(--bg-card)); transform: translateY(-1px); }
  .abt-fact-btn:hover .abt-fact-ic { color: var(--orange); background: rgba(var(--orange-rgb), .14); }

  /* ── Допомога ── */
  .abt-sec { margin: 8px 2px -2px; font: 700 11px/1 var(--font-ui); letter-spacing: .6px; text-transform: uppercase; color: var(--text-dim); }
  .abt-links { display: grid; grid-template-columns: repeat(auto-fit, minmax(300px, 1fr)); gap: 10px; }
  .abt-link {
    position: relative; display: flex; align-items: center; gap: 14px; padding: 14px 16px; border-radius: 16px; text-align: left; cursor: pointer;
    background: var(--bg-card); border: 1px solid var(--border); color: inherit; font: inherit;
    transition: border-color .15s, transform .15s, background .15s;
    --tc: var(--text-mute); --tb: var(--hover);
  }
  .abt-link.violet { --tc: var(--violet-l); --tb: rgba(123, 77, 255, .15); }
  .abt-link.blue   { --tc: var(--blue);     --tb: rgba(59, 130, 246, .15); }
  .abt-link.red    { --tc: var(--red);      --tb: rgba(239, 68, 68, .15); }
  .abt-link:hover:not(:disabled) { border-color: color-mix(in srgb, var(--tc) 55%, var(--border)); transform: translateY(-2px); background: color-mix(in srgb, var(--tc) 5%, var(--bg-card)); }
  .abt-link:active:not(:disabled) { transform: translateY(0) scale(.99); }
  .abt-link:disabled { opacity: .7; cursor: progress; }
  .abt-link-ic { width: 42px; height: 42px; flex: none; border-radius: 13px; display: grid; place-items: center; font-size: 21px; color: var(--tc); background: var(--tb); }
  .abt-link-tx { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 4px; }
  .abt-link-tx b { font: 700 13.5px/1.2 var(--font-ui); color: var(--text); }
  .abt-link-tx small { font: 400 12px/1.4 var(--font-ui); color: var(--text-mute); }
  .abt-link-cta { display: inline-flex; align-items: center; gap: 3px; flex: none; font: 600 12px var(--font-ui); color: var(--tc); opacity: .85; white-space: nowrap; }
  .abt-link:hover:not(:disabled) .abt-link-cta { opacity: 1; }
  .abt-link-cta i { font-size: 14px; transition: transform .15s; }
  .abt-link:hover:not(:disabled) .abt-link-cta i { transform: translate(2px, -2px); }

  @media (max-width: 720px) {
    .abt-id { flex-direction: column; align-items: flex-start; }
    .abt-update { flex-wrap: wrap; }
    .abt-link { flex-wrap: wrap; }
    .abt-link-cta { margin-left: 56px; }
  }
  :global(html[data-animations="off"]) .abt-glow,
  :global(html[data-performance-mode="on"]) .abt-glow { animation: none; }
  :global(html[data-performance-mode="on"]) .abt-update { -webkit-backdrop-filter: none; backdrop-filter: none; }
  @media (prefers-reduced-motion: reduce) { .abt-glow { animation: none; } .abt-link, .abt-fact-btn { transition: none; } }
</style>
