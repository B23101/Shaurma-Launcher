<script lang="ts">
  import { onDestroy, onMount } from 'svelte'
  import { t, setLanguage } from './i18n.svelte'
  import { ACCENTS, applyAccent, applyTheme, applyFont } from './theme.svelte'
  import { App, on, Application, Window } from './wails'
  import { toast, confirmDialog } from './toast.svelte'
  import type { Account, JavaStatus, SetupProgress, SystemInfo } from './types'
  import steveHead from '../assets/images/steve-head.png'

  let { onFinished } = $props<{ onFinished: () => void }>()

  const STEPS = [
    { n: 1, label: 'wiz.step.welcome' },
    { n: 2, label: 'wiz.step.lang' },
    { n: 3, label: 'wiz.step.folders' },
    { n: 4, label: 'wiz.step.perf' },
    { n: 5, label: 'wiz.step.account' },
    { n: 6, label: 'wiz.step.done' },
  ]

  const LANG_OPTS = [
    { value: 'uk', flag: '🇺🇦', label: 'language.uk' },
    { value: 'en', flag: '🇬🇧', label: 'language.en' },
    { value: 'auto', flag: '🌐', label: 'language.auto' },
  ]

  const ACCENT_NAMES: Record<string, string> = Object.fromEntries(
    Object.entries(ACCENTS).map(([k, v]) => [k, v.name])
  )

  let step = $state(1)
  let wiz = $state<SetupProgress>({
    step: 1, instanceDir: '', javaPath: '', language: 'uk', accent: 'orange', maxRAM: 4096, javaArgs: ''
  })
  let appDir = $state('')
  let sysInfo = $state<SystemInfo>({ cpuCount: 0, totalRAMMB: 0, freeDiskMB: 0 })
  let accounts = $state<Account[]>([])
  let heads = $state<Record<string, string>>({})
  let msStep = $state<'idle' | 'waiting'>('idle')
  let pirateUsername = $state('')
  let finishing = $state(false)
  // ── Стан вбудованої Java (крок 3) ──
  let javaStatus = $state<JavaStatus>({ installed: false, dir: '', exePath: '', version: '' })
  let javaInstalling = $state(false)
  let javaProgress = $state('')
  let javaUnsub: (() => void) | null = null

  const langLabel = (v: string) => t(LANG_OPTS.find(o => o.value === v)?.label ?? 'language.auto')
  const accentLabel = (v: string) => t(ACCENT_NAMES[v] ?? 'accent.orange')
  const ramGB = (mb: number) => Math.max(1, Math.round(mb / 1024))

  async function refreshHeads() {
    const next: Record<string, string> = {}
    await Promise.all(accounts.map(async (acc) => {
      try {
        const url = await App.GetAccountHead(acc.uuid, acc.username, !!acc.isLicensed)
        const key = acc.uuid || acc.username
        if (key && url) next[key] = url
      } catch (e) { /* запасна голова Стіва */ }
    }))
    heads = next
  }
  const headFor = (uuid?: string) => (uuid && heads[uuid]) || steveHead

  // ── RAM slider (1–16 ГБ, pointer-керування як у макеті) ──
  let ramDragging = $state(false)
  let ramTrackEl: HTMLDivElement | undefined = $state()
  function ramFromClientX(clientX: number) {
    if (!ramTrackEl) return
    const rect = ramTrackEl.getBoundingClientRect()
    let pct = (clientX - rect.left) / rect.width
    pct = Math.max(0, Math.min(1, pct))
    const gb = Math.max(1, Math.round(pct * 16))
    wiz.maxRAM = gb * 1024
    persist()
  }
  const ramPct = $derived(((wiz.maxRAM / 1024 - 1) / 15) * 100)

  // ── Запуск: відновити прогрес або дефолти ──
  async function start() {
    const [saved, defaults, si, dir, accs, settings] = await Promise.all([
      App.GetSetupProgress(),
      App.GetWizardDefaults(),
      App.GetSystemInfo(),
      App.GetAppDir(),
      App.GetAccounts(),
      App.GetSettings(),
    ])
    if (saved && saved.step > 0) {
      // Відновлюємо прогрес, але не даємо порожнім рядкам зі старого
      // setup-progress.json затерти дефолтні теки (instanceDir/javaPath).
      // Раніше { ...defaults, ...saved } забивало дефолт пустим рядком —
      // звідси й було "тека для збірок не вибрана, там пустий рядок".
      wiz = {
        ...defaults,
        ...saved,
        instanceDir: saved.instanceDir || defaults.instanceDir,
        javaPath: saved.javaPath || defaults.javaPath,
      }
      step = saved.step
    } else {
      wiz = defaults
      step = 1
    }
    // Якщо збережений шлях тек відрізняється від дефолту відносно поточної
    // папки даних — користувач обрав його вручну, каскад не сміє його
    // перебивати при зміні папки даних.
    manualInstances = wiz.instanceDir !== joinDir(dir, 'installations')
    manualJava = wiz.javaPath !== joinDir(dir, 'java')
    sysInfo = si
    appDir = dir
    accounts = accs ?? []
    refreshHeads()
    applyAccent(wiz.accent)
    setLanguage(wiz.language)
    applyTheme(settings?.theme || 'dark')
    await applyFont(settings?.font || 'inter', settings?.fontPath)
    await persist()
    await refreshJavaStatus()
    listenJavaProgress()
  }

  onMount(start)

  // Прибирання слухача java:status, коли майстер закривається.
  onDestroy(() => {
    if (javaUnsub) {
      try { javaUnsub() } catch (e) { /* ignore */ }
      javaUnsub = null
    }
  })

  // ── Вбудована Java: статус + встановлення ──
  async function refreshJavaStatus() {
    try {
      javaStatus = await App.GetJavaStatus()
    } catch (e) { console.error('get java status', e) }
  }

  function listenJavaProgress() {
    if (javaUnsub) return
    try {
      javaUnsub = on('java:status', (msg: string) => {
        javaProgress = msg
      })
    } catch (e) { console.error('listen java events', e) }
  }

  // javaPath вказує на вбудовану Java лаунчера? (dir !== '' — щоб не
  // вважати порожній шлях «вбудованим», поки статус ще не завантажено)
  const isBundledJava = $derived(javaStatus.dir !== '' && wiz.javaPath === javaStatus.dir)

  async function persist() {
    try {
      await App.SaveSetupProgress({ ...wiz, step })
    } catch (e) { console.error('save setup progress', e) }
  }

  async function goto(n: number) {
    step = Math.max(1, Math.min(6, n))
    await persist()
  }

  function pickLang(v: string) {
    wiz.language = v
    setLanguage(v)
    persist()
  }
  function pickAccent(v: string) {
    wiz.accent = v
    applyAccent(v)
    persist()
  }

  // ── Теки збірок і Java: ручний вибір користувача ──
  // Прапорець ставиться, коли користувач САМ обрав теку (кнопка «Огляд»
  // для збірок чи «Інша версія» для Java). Коли він потім міняє папку
  // даних лаунчера, каскад оновлює ЛИШЕ ті теки, які користувач НЕ
  // чіпав руками — ручний вибір ніколи не перебивається.
  let manualInstances = $state(false)
  let manualJava = $state(false)

  async function browseInstances() {
    const p = await App.BrowseFolder()
    if (p) { wiz.instanceDir = p; manualInstances = true; await persist() }
  }
  async function detectJava() {
    const p = await App.DetectJava()
    if (p) { wiz.javaPath = p; manualJava = true; await persist() }
  }

  // ── Папка даних лаунчера + каскад залежних тек ──
  // При зміні папки даних теки installations/ і java/ переслідують її
  // АВТОМАТИЧНО (шлях = папка даних + installations|java), але лише якщо
  // користувач не перевибрав їх вручну (manualInstances/manualJava).
  async function browseAppDir() {
    const p = await App.BrowseFolder()
    if (!p) return
    await applyAppDir(p)
  }

  async function browseAppDirInput() {
    const p = appDir.trim()
    if (!p) return
    await applyAppDir(p)
  }

  async function applyAppDir(p: string) {
    if (!manualInstances) {
      wiz.instanceDir = joinDir(p, 'installations')
    }
    if (!manualJava) {
      wiz.javaPath = joinDir(p, 'java')
    }
    appDir = p
    await App.SetAppDir(p)
    await refreshJavaStatus()
    await persist()
  }

  function joinDir(...parts: string[]) {
    return parts.join('\\').replace(/\\+/g, '\\')
  }

  // ── Акаунт ──
  async function msLogin() {
    if (msStep === 'waiting') return
    msStep = 'waiting'
    try {
      const url = await App.StartMSLogin()
      App.OpenExternal(url)
      await App.AwaitMSLogin()
      accounts = (await App.GetAccounts()) ?? []
      refreshHeads()
      msStep = 'idle'
      toast(t('wiz.account.signedIn'), 'success')
    } catch (e: any) {
      msStep = 'idle'
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }
  function msCancel() {
    App.CancelMSLogin()
    msStep = 'idle'
  }
  async function pirateLogin() {
    if (!pirateUsername.trim()) return
    try {
      await App.LoginPirate(pirateUsername)
      accounts = (await App.GetAccounts()) ?? []
      refreshHeads()
      pirateUsername = ''
      toast(t('wiz.account.signedIn'), 'success')
    } catch (e: any) { toast(t('error.withMessage', { message: e }), 'error') }
  }

  // ── Навігація ──
  async function next() {
    if (step === 3 && !wiz.instanceDir) { toast(t('wiz.toast.needFolder'), 'warn'); return }
    if (step === 3 && !wiz.javaPath) { toast(t('wiz.toast.needJava'), 'warn'); return }
    if (step === 6) { await finish(); return }
    await goto(step + 1)
  }
  async function prev() { if (step > 1) await goto(step - 1) }

  async function finish() {
    if (finishing) return
    finishing = true
    try {
      // Остаточні шляхи тек — у launcher-location.json (стабільна тека
      // Local), щоб перенесення папки даних не загубило налаштування.
      await App.SaveFolderPaths(wiz.instanceDir, wiz.javaPath)
      const s = await App.GetSettings()
      s.instanceDir = wiz.instanceDir
      s.javaPath = wiz.javaPath
      s.language = wiz.language
      s.accent = wiz.accent
      s.maxRAM = wiz.maxRAM
      s.javaArgs = wiz.javaArgs
      await App.CompleteSetup(s)
      onFinished()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
      finishing = false
    }
  }

  // Пропустити налаштування: завершити з дефолтами (за ТЗ макета).
  async function skipAll() {
    const d = await App.GetWizardDefaults()
    wiz = d
    await finish()
  }

  // Закрити майстер (X у топбарі): лише через інтерфейс-модалку, без
  // браузерних confirm(). Прогрес уже збережено на бекенді, тому
  // наступний запуск відновить цей самий крок.
  async function closeWizard() {
    const ok = await confirmDialog(t('wiz.closeTitle'), t('wiz.closeConfirm'), {
      confirmLabel: t('wiz.closeAndQuit'), cancelLabel: t('cancel'), danger: true
    })
    if (ok) Application.Quit()
  }
</script>

<div class="wizard-window">
  <!-- ══ ТОПБАР ══ -->
  <div class="wiz-topbar">
    <div class="wiz-title">
      <div class="logo-icon-box"><i class="ti ti-flame"></i></div>
      <div class="wiz-title-text">
        <div class="wt-main">{t('brand.title')}</div>
        <div class="wt-sub">{t('wiz.subtitle')}</div>
      </div>
    </div>
    <div class="wiz-ctrls">
      <button class="wiz-ctrl-btn" title={t('win.minimize')} onclick={() => Window.Minimise()}><i class="ti ti-minus"></i></button>
      <button class="wiz-ctrl-btn close" title={t('wiz.closeTitle')} onclick={closeWizard}><i class="ti ti-x"></i></button>
    </div>
  </div>

  <!-- ══ ПРОГРЕС КРОКІВ ══ -->
  <div class="wiz-progress-row">
    {#each STEPS as s, i}
      <div class="wiz-step-dot" class:active={step === s.n} class:done={step > s.n}>
        {#if i > 0}<div class="wiz-step-line"><div class="wiz-step-line-fill" style="width:{step >= s.n ? '100%' : '0%'}"></div></div>{/if}
        <div class="wsd-circle">
          {#if step > s.n}<i class="ti ti-check"></i>{:else}{s.n}{/if}
        </div>
        <div class="wsd-label">{t(s.label)}</div>
      </div>
    {/each}
  </div>

  <!-- ══ ТІЛО: КРОКИ ══ -->
  <div class="wiz-body">
    {#if step === 1}
      <!-- КРОК 1 · ВІТАННЯ -->
      <div class="welcome-hero">
        <div class="wh-logo"><i class="ti ti-flame"></i></div>
        <h1>{t('wiz.welcome.title')}</h1>
        <p>{t('wiz.welcome.desc')}</p>
        <div class="welcome-perks">
          <div class="welcome-perk"><i class="ti ti-bolt"></i> {t('wiz.welcome.perk1')}</div>
          <div class="welcome-perk"><i class="ti ti-shirt"></i> {t('wiz.welcome.perk2')}</div>
          <div class="welcome-perk"><i class="ti ti-shield-check"></i> {t('wiz.welcome.perk3')}</div>
        </div>
      </div>

    {:else if step === 2}
      <!-- КРОК 2 · МОВА ТА ВИГЛЯД -->
      <div class="wiz-head">
        <h1><i class="ti ti-language"></i> {t('wiz.lang.title')}</h1>
        <p>{t('wiz.lang.desc')}</p>
      </div>
      <div class="card">
        <div class="card-head"><div class="ci"><i class="ti ti-language"></i></div>
          <div class="ctw"><div class="ct">{t('wiz.lang.language')}</div><div class="cd">{t('wiz.lang.languageDesc')}</div></div>
        </div>
        <div class="row col" style="padding:16px 20px">
          <div class="lang-grid">
            {#each LANG_OPTS as o}
              <div class="lang-opt" class:sel={wiz.language === o.value} onclick={() => pickLang(o.value)}>
                <div class="flag">{o.flag}</div>
                <div class="ln">{t(o.label)}</div>
              </div>
            {/each}
          </div>
        </div>
      </div>
      <div class="card">
        <div class="card-head"><div class="ci violet"><i class="ti ti-palette"></i></div>
          <div class="ctw"><div class="ct">{t('wiz.lang.accent')}</div><div class="cd">{t('wiz.lang.accentDesc')}</div></div>
        </div>
        <div class="row">
          <div class="accent-grid">
            {#each Object.entries(ACCENTS) as [key, a]}
              <div class="accent-opt" class:sel={wiz.accent === key} style="background:{a.main}" onclick={() => pickAccent(key)}>
                {#if wiz.accent === key}<i class="ti ti-check"></i>{/if}
              </div>
            {/each}
          </div>
        </div>
        <div class="row">
          <div class="row-label">
            <div class="rl-name">{t('wiz.lang.darkTheme')} <span class="help" title={t('wiz.lang.darkThemeDesc')}>?</span></div>
          </div>
          <div class="toggle on" style="pointer-events:none;opacity:.6"></div>
        </div>
      </div>

    {:else if step === 3}
      <!-- КРОК 3 · ТЕКИ ТА JAVA -->
      <div class="wiz-head">
        <h1><i class="ti ti-folder"></i> {t('wiz.folders.title')}</h1>
        <p>{t('wiz.folders.desc')}</p>
      </div>
      <div class="card">
        <div class="card-head"><div class="ci"><i class="ti ti-folder"></i></div>
          <div class="ctw"><div class="ct">{t('wiz.folders.launcherDir')}</div><div class="cd">{t('wiz.folders.launcherDirDesc')}</div></div>
        </div>
        <div class="row col">
          <div class="folder-row">
            <input class="input mono" bind:value={appDir} onchange={browseAppDirInput} placeholder="C:\Users\...\.shaurm" />
            <button class="btn btn-ghost" onclick={browseAppDir}><i class="ti ti-folder"></i> {t('wiz.folders.browse')}</button>
          </div>
          <div class="wiz-hint" style="margin-top:6px"><i class="ti ti-info-circle"></i> {@html t('wiz.folders.launcherDirCascade')}</div>
        </div>
      </div>
      <div class="card">
        <div class="card-head"><div class="ci violet"><i class="ti ti-files"></i></div>
          <div class="ctw"><div class="ct">{t('wiz.folders.instancesDir')}</div></div>
        </div>
        <div class="row col">
          <div class="folder-row">
            <input class="input mono" bind:value={wiz.instanceDir} onchange={() => { manualInstances = true; persist() }} placeholder="C:\Users\...\.shaurm\installations" />
            <button class="btn btn-ghost" onclick={browseInstances}><i class="ti ti-folder"></i> {t('wiz.folders.browse')}</button>
          </div>
        </div>
      </div>
      <div class="card">
        <div class="card-head"><div class="ci green"><i class="ti ti-coffee"></i></div>
          <div class="ctw"><div class="ct">{t('wiz.folders.javaRuntime')}</div><div class="cd">{t('wiz.folders.javaRuntimeDesc')}</div></div>
        </div>
        <div class="row col">
          <div class="folder-row">
            <input class="input mono" bind:value={wiz.javaPath} onchange={() => { manualJava = true; persist() }} placeholder="C:\Users\...\.shaurm\java" />
            <button class="btn btn-ghost" onclick={detectJava}><i class="ti ti-search"></i> {t('wiz.folders.findJava')}</button>
          </div>
          {#if wiz.javaPath && wiz.javaPath !== 'java'}
            <div class="detect-banner" class:installing={javaInstalling}>
              {#if javaInstalling}
                <i class="ti ti-loader ti-spin"></i> {t('wiz.folders.javaInstalling')} {javaProgress ? `— ${javaProgress}` : ''}
              {:else if javaStatus.installed && isBundledJava}
                <i class="ti ti-circle-check"></i> {t('wiz.folders.javaReady')} · {javaStatus.version}
              {:else if javaStatus.installed}
                <i class="ti ti-circle-check"></i> {t('wiz.folders.javaFound')}
              {:else if isBundledJava}
                <i class="ti ti-info-circle"></i> {t('wiz.folders.javaNotInstalled')}
              {:else}
                <i class="ti ti-info-circle"></i> {t('wiz.folders.javaManual')}
              {/if}
            </div>
          {/if}
        </div>
      </div>

    {:else if step === 4}
      <!-- КРОК 4 · ПРОДУКТИВНІСТЬ -->
      <div class="wiz-head">
        <h1><i class="ti ti-cpu"></i> {t('wiz.perf.title')}</h1>
        <p>{t('wiz.perf.desc')}</p>
      </div>
      <div class="sys-info-strip">
        <div><span class="sis-label">{t('wiz.perf.cpu')}</span><span class="sis-val">{sysInfo.cpuCount} ядер</span></div>
        <div><span class="sis-label">{t('wiz.perf.ram')}</span><span class="sis-val">{(sysInfo.totalRAMMB / 1024).toFixed(0)} ГБ</span></div>
        <div><span class="sis-label">{t('wiz.perf.disk')}</span><span class="sis-val">{(sysInfo.freeDiskMB / 1024).toFixed(0)} ГБ</span></div>
      </div>
      <div class="card">
        <div class="card-head"><div class="ci"><i class="ti ti-player-play"></i></div>
          <div class="ctw"><div class="ct">{t('wiz.perf.ramXmx')}</div><div class="cd">{t('wiz.perf.ramXmxDesc')}</div></div>
        </div>
        <div class="row col" style="padding:18px 20px 16px">
          <div class="ram-track" bind:this={ramTrackEl}
               onpointerdown={(e) => { ramDragging = true; ramFromClientX(e.clientX) }}
               onpointermove={(e) => { if (ramDragging) ramFromClientX(e.clientX) }}
               onpointerup={() => { ramDragging = false }}
               onpointerleave={() => { ramDragging = false }}>
            <div class="ram-fill" style="width:{ramPct}%"></div>
            <div class="ram-knob" style="left:{ramPct}%"></div>
          </div>
          <div class="ram-ticks"><span>1 ГБ</span><span>4 ГБ</span><span>8 ГБ</span><span>12 ГБ</span><span>16 ГБ</span></div>
          <div class="row" style="padding:10px 0 0; border:none">
            <div class="row-label"><div class="rl-name">{t('wiz.perf.selected')}</div></div>
            <span class="chip g" style="font-family: var(--font-mono)">{ramGB(wiz.maxRAM)} ГБ</span>
          </div>
        </div>
      </div>
      <div class="card">
        <div class="card-head"><div class="ci violet"><i class="ti ti-terminal-2"></i></div>
          <div class="ctw"><div class="ct">{t('wiz.perf.jvmArgs')}</div><div class="cd">{t('wiz.perf.jvmArgsDesc')}</div></div>
        </div>
        <div class="row col">
          <input class="input mono" bind:value={wiz.javaArgs} onchange={persist} placeholder="-XX:+UseG1GC -XX:+ParallelRefProcEnabled" />
        </div>
      </div>

    {:else if step === 5}
      <!-- КРОК 5 · АКАУНТ -->
      <div class="wiz-head">
        <h1><i class="ti ti-users"></i> {t('wiz.account.title')}</h1>
        <p>{t('wiz.account.desc')}</p>
      </div>
      <div class="card">
        {#if accounts.length > 0}
          <div class="row col" style="padding:20px">
            {#each accounts as acc}
              <div class="account-signed-in">
                <div class="user-avatar"><img src={headFor(acc.uuid)} alt="" /></div>
                <div>
                  <div class="asi-name">{acc.username}</div>
                  <div class="asi-sub">{acc.type === 'microsoft' ? t('account.type.microsoft') : t('account.type.offline')} · {t('wiz.account.signedIn')}</div>
                </div>
                <i class="ti ti-circle-check" style="color:var(--green);font-size:20px;margin-left:auto"></i>
              </div>
            {/each}
            <div style="font-size:11px;color:var(--text-mute);margin-top:4px">{t('wiz.account.msHint')}</div>
          </div>
        {:else}
          <div class="account-box">
            <div class="ab-icon"><i class="ti ti-user-plus"></i></div>
            <div>
              <div style="font-size:13.5px;font-weight:700;margin-bottom:4px">{t('wiz.account.notAdded')}</div>
              <div style="font-size:11px;color:var(--text-mute)">{t('wiz.account.msHint')}</div>
            </div>
            <button class="btn btn-primary btn-lg" onclick={msLogin}><i class="ti ti-brand-windows"></i> {t('wiz.account.signInMs')}</button>
            {#if msStep === 'waiting'}
              <div class="ms-waiting" style="margin-top:12px">
                <div class="spinner"></div>
                <span>{t('wiz.waiting')}</span>
                <button class="btn btn-ghost btn-sm" onclick={msCancel}>{t('cancel')}</button>
              </div>
            {/if}
            <div class="wiz-divider">{t('orPirate')}</div>
            <div class="wiz-field-row">
              <input class="input" style="flex:1" placeholder={t('nickname')} bind:value={pirateUsername}
                     onkeydown={(e: any) => e.key === 'Enter' && pirateLogin()} />
              <button class="btn btn-ghost" onclick={pirateLogin}><i class="ti ti-user"></i> {t('login')}</button>
            </div>
            <div class="wf-skip" onclick={() => goto(6)} style="margin-top:6px">{t('wiz.account.skipStep')}</div>
          </div>
        {/if}
      </div>

    {:else if step === 6}
      <!-- КРОК 6 · ГОТОВО -->
      <div class="finish-hero">
        <div class="fh-check"><i class="ti ti-check"></i></div>
        <h1>{t('wiz.finish.title')}</h1>
        <p>{t('wiz.finish.desc')}</p>
        <div class="finish-summary">
          <div class="fs-row"><span class="fsl"><i class="ti ti-language"></i> {t('wiz.finish.lang')}</span><span class="fsv">{langLabel(wiz.language)}</span></div>
          <div class="fs-row"><span class="fsl"><i class="ti ti-palette"></i> {t('wiz.finish.accent')}</span><span class="fsv">{accentLabel(wiz.accent)}</span></div>
          <div class="fs-row"><span class="fsl"><i class="ti ti-cpu"></i> {t('wiz.finish.ram')}</span><span class="fsv">{ramGB(wiz.maxRAM)} ГБ</span></div>
          <div class="fs-row"><span class="fsl"><i class="ti ti-users"></i> {t('wiz.finish.account')}</span><span class="fsv">{accounts[0]?.username ?? t('wiz.finish.notAdded')}</span></div>
        </div>
      </div>
    {/if}
  </div>

  <!-- ══ ФУТЕР: НАВІГАЦІЯ ══ -->
  <div class="wiz-footer">
    <div class="wf-left">
      <button class="btn btn-ghost" style="visibility:{step === 1 ? 'hidden' : 'visible'}" onclick={prev}>
        <i class="ti ti-arrow-left"></i> {t('wiz.back')}
      </button>
      <span class="wf-step-count">{t('wiz.stepOf', { cur: step, total: 6 })}</span>
    </div>
    <div style="display:flex;align-items:center;gap:16px">
      {#if step < 6}
        <div class="wf-skip" onclick={skipAll}>{t('wiz.skipAll')}</div>
      {/if}
      <button class="btn btn-primary btn-lg" onclick={next} disabled={finishing}>
        {#if step === 6}
          <i class="ti ti-rocket"></i> {t('wiz.finish.start')}
        {:else}
          {t('wiz.next')} <i class="ti ti-arrow-right"></i>
        {/if}
      </button>
    </div>
  </div>
</div>

<style>
  .wizard-window {
    width: 100vw;
    height: 100vh;
    background: var(--bg-window);
    overflow: hidden;
    display: flex;
    flex-direction: column;
    position: relative;
  }

  /* ── Топбар ── */
  .wiz-topbar {
    display: flex; align-items: center; justify-content: space-between;
    padding: 14px 18px;
    border-bottom: 1px solid var(--border);
    --wails-draggable: drag;
  }
  .wiz-title { display: flex; align-items: center; gap: 10px; --wails-draggable: no-drag; }
  .wiz-title .logo-icon-box {
    width: 32px; height: 32px; border-radius: 10px;
    background: linear-gradient(135deg, var(--orange), var(--orange-2));
    display: flex; align-items: center; justify-content: center;
    box-shadow: 0 6px 14px rgba(var(--orange-rgb), .25);
    font-size: 16px; color: #fff;
  }
  .wiz-title-text { display: flex; flex-direction: column; gap: 1px; }
  .wt-main { font-size: 13.5px; font-weight: 800; }
  .wt-sub { font-size: 10.5px; color: var(--text-mute); font-weight: 600; }
  .wiz-ctrls { display: flex; align-items: center; gap: 6px; --wails-draggable: no-drag; }
  .wiz-ctrl-btn {
    width: 30px; height: 30px; border-radius: 9px;
    border: 1px solid var(--border);
    background: var(--hover);
    color: var(--text-mute); cursor: pointer;
    display: flex; align-items: center; justify-content: center; font-size: 14px;
    transition: background .15s, color .15s, border-color .15s;
    --wails-draggable: no-drag;
  }
  .wiz-ctrl-btn:hover { background: var(--hover-strong); color: var(--text); border-color: var(--border-hi); }
  .wiz-ctrl-btn.close:hover { background: var(--red); color: #fff; border-color: var(--red); }

  /* ── Прогрес кроків ── */
  .wiz-progress-row { display: flex; align-items: center; gap: 10px; padding: 16px 30px 0; }
  .wiz-step-dot {
    flex: 1; display: flex; flex-direction: column; align-items: center; gap: 6px;
    position: relative; opacity: .45; transition: opacity .2s;
  }
  .wiz-step-dot.active, .wiz-step-dot.done { opacity: 1; }
  .wsd-circle {
    width: 26px; height: 26px; border-radius: 50%;
    background: var(--bg-input); border: 1.5px solid var(--border-hi);
    display: flex; align-items: center; justify-content: center;
    font-size: 12px; font-weight: 800; color: var(--text-mute);
    transition: background .2s, border-color .2s, color .2s;
  }
  .wiz-step-dot.active .wsd-circle { background: var(--orange); border-color: var(--orange); color: #000; box-shadow: 0 0 14px rgba(var(--orange-rgb), .45); }
  .wiz-step-dot.done .wsd-circle { background: var(--green); border-color: var(--green); color: #000; }
  .wsd-label { font-size: 9.5px; font-weight: 700; color: var(--text-mute); letter-spacing: .2px; text-align: center; }
  .wiz-step-dot.active .wsd-label { color: #fff; }
  .wiz-step-line { position: absolute; top: 13px; left: calc(-50% + 13px); width: calc(100% - 26px); height: 2px; background: var(--border-hi); z-index: -1; }
  .wiz-step-line-fill { height: 100%; background: var(--green); width: 0%; transition: width .25s ease; }
  .wiz-step-dot:first-child .wiz-step-line { display: none; }

  /* ── Тіло ── */
  .wiz-body { flex: 1; overflow-y: auto; padding: 24px 34px 18px; display: flex; flex-direction: column; }
  .wiz-body::-webkit-scrollbar { width: 5px; }
  .wiz-body::-webkit-scrollbar-thumb { background: var(--hover-strong); border-radius: 3px; }
  .wiz-head { margin-bottom: 16px; }
  .wiz-head h1 { font-size: 24px; font-weight: 800; display: flex; align-items: center; gap: 10px; }
  .wiz-head h1 i { color: var(--orange); font-size: 24px; }
  .wiz-head p { font-size: 12.5px; color: var(--text-mute); margin-top: 6px; max-width: 560px; line-height: 1.5; }

  /* ── Крок 1: вітання ── */
  .welcome-hero {
    flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center;
    text-align: center; gap: 14px;
  }
  .wh-logo {
    width: 88px; height: 88px; border-radius: 24px;
    background: linear-gradient(135deg, var(--orange), var(--orange-2));
    display: flex; align-items: center; justify-content: center;
    font-size: 44px; color: #fff; box-shadow: 0 20px 40px rgba(var(--orange-rgb), .3);
    margin-bottom: 6px;
  }
  .welcome-hero h1 { font-size: 26px; font-weight: 800; }
  .welcome-hero p { font-size: 13px; color: var(--text-mute); max-width: 440px; line-height: 1.6; }
  .welcome-perks { display: flex; gap: 10px; margin-top: 14px; flex-wrap: wrap; justify-content: center; }
  .welcome-perk {
    display: flex; align-items: center; gap: 7px;
    background: var(--bg-card); border: 1px solid var(--border);
    padding: 8px 14px; border-radius: 999px; font-size: 11.5px; font-weight: 600;
  }
  .welcome-perk i { color: var(--orange); font-size: 15px; }

  /* ── Крок 3: теки та java ── */
  .detect-banner {
    display: flex; align-items: center; gap: 10px;
    background: rgba(16,185,129,.08);
    border: 1px solid rgba(16,185,129,.3); border-radius: var(--r-md);
    padding: 11px 14px; font-size: 11.5px; color: #dfffef; margin-top: 4px;
  }
  .detect-banner.installing {
    background: rgba(var(--orange-rgb),.08);
    border-color: rgba(var(--orange-rgb),.35);
    color: var(--text);
  }
  .detect-banner.installing i { color: var(--orange); }
  .detect-banner i { color: var(--green); font-size: 17px; }

  /* ── Крок 4: продуктивність ── */
  .sys-info-strip {
    display: flex; gap: 18px; padding: 11px 14px;
    background: var(--bg-input); border: 1px solid var(--border);
    border-radius: var(--r-md); font-size: 11px; margin-bottom: 14px;
  }
  .sys-info-strip div { display: flex; flex-direction: column; gap: 2px; }
  .sis-label { color: var(--text-dim); font-size: 9.5px; text-transform: uppercase; letter-spacing: .4px; }
  .sis-val { font-weight: 700; font-family: var(--font-mono); }
  /* ── Крок 5: акаунт ── */
  .account-box {
    display: flex; flex-direction: column; align-items: center; gap: 16px;
    padding: 30px 20px; text-align: center;
  }
  .ab-icon {
    width: 64px; height: 64px; border-radius: 18px;
    background: var(--bg-input); border: 1.5px dashed var(--border-hi);
    display: flex; align-items: center; justify-content: center;
    font-size: 28px; color: var(--text-mute);
  }
  .account-signed-in {
    display: flex; align-items: center; gap: 12px;
    padding: 14px 18px; background: var(--bg-card);
    border: 1px solid var(--border-hi); border-radius: var(--r-lg);
    width: 100%; max-width: 340px;
  }
  .account-signed-in .user-avatar {
    width: 38px; height: 38px; border-radius: 10px;
    background: linear-gradient(135deg, #c07040, #8a4a26);
    display: flex; align-items: center; justify-content: center;
    font-size: 14px; font-weight: 700;
    overflow: hidden; flex-shrink: 0;
  }
  .account-signed-in .user-avatar img { width: 100%; height: 100%; object-fit: cover; display: block; }
  .asi-name { font-size: 13px; font-weight: 700; text-align: left; }
  .asi-sub { font-size: 10.5px; color: var(--text-mute); text-align: left; }

  /* ── Крок 6: готово ── */
  .finish-hero {
    flex: 1; display: flex; flex-direction: column; align-items: center;
    justify-content: center; text-align: center; gap: 12px;
  }
  .fh-check {
    width: 80px; height: 80px; border-radius: 50%;
    background: rgba(16,185,129,.13); border: 2px solid var(--green);
    display: flex; align-items: center; justify-content: center;
    font-size: 38px; color: var(--green); margin-bottom: 6px;
  }
  .finish-hero h1 { font-size: 24px; font-weight: 800; }
  .finish-hero p { font-size: 12.5px; color: var(--text-mute); max-width: 420px; line-height: 1.6; }
  .finish-summary { display: flex; flex-direction: column; gap: 6px; margin-top: 16px; width: 100%; max-width: 420px; text-align: left; }
  .fs-row {
    display: flex; justify-content: space-between; align-items: center;
    padding: 9px 14px; background: var(--bg-card); border: 1px solid var(--border);
    border-radius: var(--r-md); font-size: 11.5px;
  }
  .fs-row .fsl { color: var(--text-mute); display: flex; align-items: center; gap: 8px; }
  .fs-row .fsl i { color: var(--orange); font-size: 14px; }
  .fs-row .fsv { font-weight: 700; font-family: var(--font-mono); }

  /* ── Футер ── */
  .wiz-footer {
    display: flex; align-items: center; justify-content: space-between;
    padding: 16px 30px; border-top: 1px solid var(--border);
    background: rgba(0,0,0,.15);
  }
  .wf-left { display: flex; align-items: center; gap: 10px; }
  .wf-skip { font-size: 11.5px; color: var(--text-mute); cursor: pointer; text-decoration: underline; text-underline-offset: 2px; }
  .wf-skip:hover { color: var(--text); }
  .wf-step-count { font-size: 10.5px; color: var(--text-dim); font-family: var(--font-mono); }
  .help {
    display: inline-flex; align-items: center; justify-content: center;
    width: 15px; height: 15px; border-radius: 50%;
    background: var(--bg-input-hi); color: var(--text-mute);
    font-size: 9.5px; font-weight: 700; cursor: help; margin-left: 5px;
  }
</style>
