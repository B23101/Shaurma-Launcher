<script lang="ts">
  // Майстер першого запуску. Велика картка: зліва — анімована векторна
  // сцена кроку (wizard/WizArt), справа — налаштування. Вхід в акаунт робить
  // App.svelte (той самий шлях, що й модалка входу: нік, ліміт пристроїв),
  // сюди приходять лише стан і колбеки.
  import { onDestroy, onMount } from 'svelte'
  import { fly, fade } from 'svelte/transition'
  import { quintOut } from 'svelte/easing'
  import { t, setLanguage } from './i18n.svelte'
  import { pirateNicknameError, sanitizeNick } from './validation'
  import { ACCENTS, applyAccent, applyTheme, applyFont, applyAnimations, applyPerformanceMode } from './theme.svelte'
  import { App, Application, Window, on } from './wails'
  import { toast, confirmDialog, setToastAnchor } from './toast.svelte'
  import type { Account, SetupProgress, SystemInfo } from './types'
  import WizArt from './wizard/WizArt.svelte'
  import PlayerHead from './wizard/PlayerHead.svelte'
  import AccountStep from './wizard/AccountStep.svelte'
  import FilesStep from './wizard/FilesStep.svelte'
  import SkinStage from './wizard/SkinStage.svelte'
  import JvmArgsEditor from './JvmArgsEditor.svelte'
  import appIcon from '../assets/images/appicon.png'

  let {
    onFinished,
    accounts = [],
    msStep = 'idle',
    googleStep = 'idle',
    skingate = false,
    onMsLogin = null,
    onMsCancel = null,
    onGoogleLogin = null,
    onGoogleCancel = null,
    onPirate = null,
    activeAccountId = '',
    nickAsk = null,
    onConfirmNick = null,
    onCancelNick = null,
  }: {
    onFinished: (o?: { createPack?: boolean }) => void
    activeAccountId?: string
    nickAsk?: null | { provider: string; noLicense: string }
    onConfirmNick?: null | ((nick: string) => Promise<void>)
    onCancelNick?: null | (() => void)
    accounts?: Account[]
    msStep?: 'idle' | 'waiting'
    googleStep?: 'idle' | 'waiting'
    skingate?: boolean
    onMsLogin?: null | (() => void)
    onMsCancel?: null | (() => void)
    onGoogleLogin?: null | (() => void)
    onGoogleCancel?: null | (() => void)
    onPirate?: null | ((nick: string) => Promise<boolean> | boolean | void)
  } = $props()

  const STEPS = [
    { id: 'welcome', icon: 'ti-hand-stop' },
    { id: 'look', icon: 'ti-palette' },
    { id: 'account', icon: 'ti-user-circle' },
    { id: 'files', icon: 'ti-folders' },
    { id: 'game', icon: 'ti-device-gamepad-2' },
    { id: 'notify', icon: 'ti-bell' },
    { id: 'behavior', icon: 'ti-app-window' },
    { id: 'done', icon: 'ti-check' },
  ] as const
  const LAST = STEPS.length

  const THEMES = [
    { value: 'dark', bg: '#16171b', card: '#22242a', text: '#e8e8ea' },
    { value: 'oled', bg: '#000000', card: '#111113', text: '#f1f1f1' },
    { value: 'light', bg: '#f3f3f5', card: '#ffffff', text: '#1d1d22' },
    { value: 'system', bg: '', card: '', text: '' },
  ]

  // Налаштування, які майстер міняє поверх settings.json. Тримаємо в
  // localStorage, щоб закритий посередині майстер повернувся з ними ж.
  const OPT_KEY = 'shaurma.wizard.opts'
  const DEFAULT_OPTS = {
    theme: 'dark', animations: true, performance: false,
    notifyEnabled: true, notifyDurationSec: 8, notifyDnd: false,
    discord: true, p2p: true, cloudTime: true,
    startupMode: 'never', closeAction: 'quit', gameLaunchAction: 'keep', gameExitAction: 'restore',
    showConsoleOnCrash: true, autoUpdate: true,
  }
  let opt = $state({ ...DEFAULT_OPTS })

  let step = $state(1)
  let dir = $state(1)
  let wiz = $state<SetupProgress>({ step: 1, instanceDir: '', javaPath: '', language: 'uk', accent: 'orange', maxRAM: 4096, javaArgs: '' })
  let appDir = $state('')
  let sysInfo = $state<SystemInfo>({ cpuCount: 0, totalRAMMB: 0, freeDiskMB: 0 })
  let heads = $state<Record<string, string>>({})
  let finishing = $state(false)
  let ready = $state(false)
  let showAdvanced = $state(false)
  let skinPreview = $state<{ texture: string; slim: boolean } | null>(null)
  let migrated = $state(0)
  let headsTick = $state(0)
  const cur = $derived(STEPS[step - 1])
  const ramGB = $derived(Math.max(1, Math.round(wiz.maxRAM / 1024)))
  const totalGB = $derived(Math.round(sysInfo.totalRAMMB / 1024))
  const recGB = $derived(totalGB <= 0 ? 4 : totalGB <= 4 ? 2 : totalGB <= 8 ? 4 : totalGB <= 16 ? 6 : 8)
  const waiting = $derived(msStep === 'waiting' || googleStep === 'waiting')
  const mainAcc = $derived(accounts[0])
  const mainHead = $derived(mainAcc ? (heads[mainAcc.uuid || mainAcc.username] ?? '') : '')

  // ── Голови акаунтів ──
  $effect(() => {
    const list = accounts
    void headsTick
    let alive = true
    ;(async () => {
      const next: Record<string, string> = {}
      await Promise.all(list.map(async (acc) => {
        try {
          const url = await App.GetAccountHead(acc.uuid, acc.username, !!acc.isLicensed)
          const key = acc.uuid || acc.username
          if (key && url) next[key] = url
        } catch { /* лишається скін Стіва */ }
      }))
      if (alive) heads = next
    })()
    return () => { alive = false }
  })

  // ── RAM-слайдер (1–16 ГБ) ──
  const RAM_MIN = 1, RAM_MAX = 16
  let ramDragging = $state(false)
  let ramTrackEl: HTMLDivElement | undefined = $state()
  function ramFromX(x: number) {
    if (!ramTrackEl) return
    const r = ramTrackEl.getBoundingClientRect()
    const pct = Math.max(0, Math.min(1, (x - r.left) / r.width))
    wiz.maxRAM = Math.round(pct * (RAM_MAX - RAM_MIN) + RAM_MIN) * 1024
  }
  const ramPct = $derived(((ramGB - RAM_MIN) / (RAM_MAX - RAM_MIN)) * 100)
  const recPct = $derived(((recGB - RAM_MIN) / (RAM_MAX - RAM_MIN)) * 100)
  function ramKey(d: number) {
    wiz.maxRAM = Math.max(RAM_MIN, Math.min(RAM_MAX, ramGB + d)) * 1024
    persist()
  }
  const ramWarn = $derived(totalGB > 0 && ramGB > Math.max(2, totalGB - 2))

  // ── Старт ──
  async function start() {
    const [saved, defaults, si, dir0, settings] = await Promise.all([
      App.GetSetupProgress(), App.GetWizardDefaults(), App.GetSystemInfo(), App.GetAppDir(), App.GetSettings(),
    ])
    // Стартові значення беремо з поточних налаштувань (бекенд уже має дефолти).
    if (settings) {
      const s = settings as any
      opt = {
        ...opt,
        theme: ['dark', 'oled', 'light', 'system'].includes(s.theme) ? s.theme : 'dark',
        animations: s.animationsEnabled !== false,
        performance: !!s.performanceMode,
        notifyEnabled: s.notifyEnabled !== false,
        notifyDurationSec: [5, 8, 15].includes(s.notifyDurationSec) ? s.notifyDurationSec : 8,
        notifyDnd: !!s.notifyDnd,
        discord: s.discordRpcEnabled !== false,
        p2p: !s.disableP2P,
        cloudTime: !s.disableCloudPlaytime,
        startupMode: s.startupMode || 'never',
        closeAction: s.closeAction || 'quit',
        gameLaunchAction: s.gameLaunchAction || 'keep',
        gameExitAction: s.gameExitAction || 'restore',
        showConsoleOnCrash: s.showConsoleOnCrash !== false,
        autoUpdate: s.autoUpdate !== false,
      }
    }
    try {
      const raw = localStorage.getItem(OPT_KEY)
      if (raw) opt = { ...opt, ...JSON.parse(raw) }
    } catch { /* приватний режим */ }

    if (saved && saved.step > 0) {
      wiz = { ...defaults, ...saved, instanceDir: saved.instanceDir || defaults.instanceDir, javaPath: saved.javaPath || defaults.javaPath }
      step = Math.max(1, Math.min(LAST, saved.step))
    } else {
      wiz = defaults
      step = 1
    }
    const fp = await App.GetFolderPaths().catch(() => null)
    const base = fp?.baseDir || shaurmBase(dir0)
    wiz.instanceDir = fp?.installations || joinDir(base, 'installations')
    wiz.javaPath = fp?.java || joinDir(base, 'java')
    sysInfo = si
    appDir = base
    if (!saved || saved.step <= 0) wiz.maxRAM = recommended(si) * 1024
    applyAccent(wiz.accent)
    setLanguage(wiz.language)
    applyLook()
    await applyFont(settings?.font || 'inter', settings?.fontPath)
    ready = true
    await persist()
  }

  function recommended(si: SystemInfo) {
    const g = Math.round(si.totalRAMMB / 1024)
    return g <= 0 ? 4 : g <= 4 ? 2 : g <= 8 ? 4 : g <= 16 ? 6 : 8
  }

  function applyLook() {
    applyTheme(opt.theme)
    applyAnimations(opt.animations)
    applyPerformanceMode(opt.performance)
  }

  function onKey(e: KeyboardEvent) {
    if (e.key !== 'Enter' || finishing) return
    const tag = (e.target as HTMLElement)?.tagName
    if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'BUTTON') return
    next()
  }

  onMount(() => {
    setToastAnchor('top')
    start()
    window.addEventListener('keydown', onKey)
    offAvatar = on('avatar:changed', () => { headsTick++ })
  })
  let offAvatar: (() => void) | null = null
  onDestroy(() => {
    setToastAnchor('bottom')
    window.removeEventListener('keydown', onKey)
    offAvatar?.()
  })

  async function persist() {
    try { localStorage.setItem(OPT_KEY, JSON.stringify(opt)) } catch { /* ignore */ }
    try { await App.SaveSetupProgress({ ...wiz, step }) } catch (e) { console.error('save setup progress', e) }
  }

  async function goto(n: number) {
    const to = Math.max(1, Math.min(LAST, n))
    if (to === step) return
    dir = to > step ? 1 : -1
    step = to
    await persist()
  }
  async function next() {
    if (step === LAST) { await finish(); return }
    await goto(step + 1)
  }
  async function prev() { if (step > 1) await goto(step - 1) }

  function set<K extends keyof typeof opt>(k: K, v: (typeof opt)[K]) {
    opt[k] = v
    if (k === 'theme' || k === 'animations' || k === 'performance') applyLook()
    persist()
  }
  function pickLang(v: string) { wiz.language = v; setLanguage(v); persist() }
  function pickAccent(v: string) { wiz.accent = v; applyAccent(v); persist() }

  // ── Папка даних ──
  async function browseAppDir() {
    const p = await App.BrowseFolder()
    if (p) await applyAppDir(p)
  }
  async function applyAppDir(p: string) {
    // Спершу бекенд переносить дані; поля майстра міняємо лише після успіху,
    // інакше settings розійдуться з реальними теками і гра не знайде Java.
    try { await App.SetAppDir(p) } catch (e: any) { toast(t('error.withMessage', { message: e }), 'error'); return }
    const fp = await App.GetFolderPaths().catch(() => null)
    const base = fp?.baseDir || shaurmBase(p)
    wiz.instanceDir = fp?.installations || joinDir(base, 'installations')
    wiz.javaPath = fp?.java || joinDir(base, 'java')
    appDir = base
    await persist()
  }
  function shaurmBase(p: string): string {
    const norm = p.replace(/[\\/]+$/, '')
    if (!norm) return p
    return norm.endsWith('.shaurm') ? norm : joinDir(norm, '.shaurm')
  }
  function joinDir(...parts: string[]) { return parts.join('\\').replace(/\\+/g, '\\') }

  // ── Завершення ──
  async function finish(createPack = false) {
    if (finishing) return
    finishing = true
    try {
      const s = (await App.GetSettings()) as any
      s.instanceDir = wiz.instanceDir
      s.javaPath = wiz.javaPath
      s.language = wiz.language
      s.accent = wiz.accent
      s.maxRAM = wiz.maxRAM
      s.javaArgs = wiz.javaArgs
      s.theme = opt.theme
      s.animationsEnabled = opt.animations
      s.performanceMode = opt.performance
      s.notifyEnabled = opt.notifyEnabled
      s.notifyDurationSec = opt.notifyDurationSec
      s.notifyDnd = opt.notifyDnd
      s.discordRpcEnabled = opt.discord
      s.disableP2P = !opt.p2p
      s.disableCloudPlaytime = !opt.cloudTime
      s.startupMode = opt.startupMode
      s.closeAction = opt.closeAction
      s.gameLaunchAction = opt.gameLaunchAction
      s.gameExitAction = opt.gameExitAction
      s.showConsoleOnCrash = opt.showConsoleOnCrash
      s.autoUpdate = opt.autoUpdate
      await App.CompleteSetup(s)
      try { localStorage.removeItem(OPT_KEY) } catch { /* ignore */ }
      onFinished({ createPack })
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
      finishing = false
    }
  }

  async function closeWizard() {
    const ok = await confirmDialog(t('wiz.closeTitle'), t('wiz.closeConfirm'), {
      confirmLabel: t('wiz.closeAndQuit'), cancelLabel: t('cancel'), danger: true,
    })
    if (ok) Application.Quit()
  }

  // ── Підсумок ──
  const onOff = (v: boolean) => t(v ? 'wz.on' : 'wz.off')
  const langName = (v: string) => t(v === 'uk' ? 'language.uk' : v === 'en' ? 'language.en' : 'language.auto')
  const summary = $derived([
    { icon: 'ti-language', label: t('wz.sum.lang'), value: langName(wiz.language), to: 1 },
    { icon: 'ti-moon', label: t('wz.sum.theme'), value: t('wz.theme.' + opt.theme), to: 2 },
    { icon: 'ti-palette', label: t('wz.sum.accent'), value: t(ACCENTS[wiz.accent]?.name ?? 'accent.orange'), to: 2, swatch: true },
    { icon: 'ti-user-circle', label: t('wz.sum.account'), value: mainAcc?.username ?? t('wz.sum.noAccount'), to: 3, head: true },
    { icon: 'ti-folder', label: t('wz.sum.folder'), value: appDir, to: 4 },
    { icon: 'ti-transfer-in', label: t('wz.sum.migrated'), value: migrated > 0 ? String(migrated) : t('wz.sum.none'), to: 4 },
    { icon: 'ti-cpu', label: t('wz.sum.ram'), value: `${ramGB} ${t('wz.unit.gb')}`, to: 5 },
    { icon: 'ti-player-play', label: t('wz.sum.inGame'), value: t('wz.launch.' + opt.gameLaunchAction), to: 5 },
    { icon: 'ti-bell', label: t('wz.sum.notify'), value: opt.notifyEnabled ? (opt.notifyDnd ? t('wz.notify.dnd') : onOff(true)) : onOff(false), to: 6 },
    { icon: 'ti-brand-discord', label: t('wz.sum.discord'), value: onOff(opt.discord), to: 6 },
    { icon: 'ti-world-share', label: t('wz.sum.p2p'), value: onOff(opt.p2p), to: 6 },
    { icon: 'ti-power', label: t('wz.sum.startup'), value: t('wz.startup.' + opt.startupMode), to: 7 },
    { icon: 'ti-x', label: t('wz.sum.close'), value: t('wz.close.' + opt.closeAction), to: 7 },
    { icon: 'ti-refresh', label: t('wz.sum.update'), value: onOff(opt.autoUpdate), to: 7 },
  ])
</script>

{#snippet toggle(on: boolean, onclick: () => void, label: string, disabled = false)}
  <button class="tg" class:on role="switch" aria-checked={on} aria-label={label} {disabled} {onclick}><span class="tg-k"></span></button>
{/snippet}

{#snippet row(icon: string, title: string, desc: string, on: boolean, onclick: () => void, disabled = false)}
  <div class="opt-row" class:dis={disabled}>
    <span class="or-ico"><i class="ti {icon}"></i></span>
    <span class="or-txt"><b>{title}</b>{#if desc}<small>{desc}</small>{/if}</span>
    {@render toggle(on, onclick, title, disabled)}
  </div>
{/snippet}

{#snippet seg(options: { value: any; label: string; icon?: string }[], value: any, pick: (v: any) => void, label: string)}
  <div class="seg" role="radiogroup" aria-label={label} style="--n:{options.length};--i:{Math.max(0, options.findIndex(o => o.value === value))}">
    <span class="seg-pill"></span>
    {#each options as o}
      <button role="radio" aria-checked={o.value === value} class:sel={o.value === value} onclick={() => pick(o.value)}>
        {#if o.icon}<i class="ti {o.icon}"></i>{/if}<span>{o.label}</span>
      </button>
    {/each}
  </div>
{/snippet}

<div class="wz">
  <div class="bg" aria-hidden="true">
    <span class="blob b1"></span><span class="blob b2"></span><span class="blob b3"></span>
    <span class="grid"></span>
  </div>

  <div class="topbar">
    <div class="brand"><img class="brand-ico" src={appIcon} alt="" />{t('brand.title')}</div>
    <div class="ctrls">
      <button class="ctrl" title={t('win.minimize')} onclick={() => Window.Minimise()}><i class="ti ti-minus"></i></button>
      <button class="ctrl close" title={t('wiz.closeTitle')} onclick={closeWizard}><i class="ti ti-x"></i></button>
    </div>
  </div>

  <div class="shell" class:ready>
    <!-- ══ Ліва сцена ══ -->
    <aside class="stage" aria-hidden="true">
      {#key cur.id + (cur.id === 'account' && skinPreview ? ':skin' : '')}
        <div class="stage-in" in:fade={{ duration: 380, delay: 120 }} out:fade={{ duration: 160 }}>
          {#if cur.id === 'account' && skinPreview}
            <SkinStage texture={skinPreview.texture} slim={skinPreview.slim} name={mainAcc?.username ?? ''} />
          {:else}
            <WizArt scene={cur.id} {ramGB} notifyOn={opt.notifyEnabled && !opt.notifyDnd} accountHead={mainHead} accountName={mainAcc?.username ?? ''} />
          {/if}
        </div>
      {/key}
      <div class="stage-steps">
        {#each STEPS as s, i}
          <span class="dot" class:act={i + 1 === step} class:done={i + 1 < step}></span>
        {/each}
      </div>
    </aside>

    <!-- ══ Права частина ══ -->
    <section class="main">
      <div class="progress" style="--p:{((step - 1) / (LAST - 1)) * 100}%">
        <div class="pr-fill"></div>
        {#each STEPS as s, i}
          <button class="pr-step" class:act={i + 1 === step} class:done={i + 1 < step}
                  disabled={i + 1 > step && step !== LAST} title={t('wz.step.' + s.id)} onclick={() => goto(i + 1)}>
            <i class="ti {i + 1 < step ? 'ti-check' : s.icon}"></i>
          </button>
        {/each}
      </div>

      <div class="panes">
        {#key step}
          <div class="pane" in:fly={{ x: 48 * dir, duration: 520, delay: 140, easing: quintOut, opacity: 0 }} out:fly={{ x: -32 * dir, duration: 200, opacity: 0 }}>
            <div class="eyebrow">{t('wiz.stepOf', { cur: step, total: LAST })} · {t('wz.step.' + cur.id)}</div>

            {#if cur.id === 'welcome'}
              <h1>{t('wz.welcome.title')}</h1>
              <p class="sub">{t('wz.welcome.sub')}</p>
              <div class="lang-list">
                {#each [{ v: 'uk', l: 'language.uk' }, { v: 'en', l: 'language.en' }, { v: 'auto', l: 'language.auto' }] as o, i}
                  <button class="lang" class:sel={wiz.language === o.v} style="--i:{i}" onclick={() => pickLang(o.v)}>
                    <span class="flag">
                      {#if o.v === 'uk'}
                        <svg viewBox="0 0 30 20"><rect width="30" height="10" fill="#0057b7" /><rect y="10" width="30" height="10" fill="#ffd700" /></svg>
                      {:else if o.v === 'en'}
                        <svg viewBox="0 0 60 40"><rect width="60" height="40" fill="#012169" /><path d="M0 0L60 40M60 0L0 40" stroke="#fff" stroke-width="8" /><path d="M0 0L60 40M60 0L0 40" stroke="#c8102e" stroke-width="3" /><path d="M30 0v40M0 20h60" stroke="#fff" stroke-width="12" /><path d="M30 0v40M0 20h60" stroke="#c8102e" stroke-width="7" /></svg>
                      {:else}
                        <i class="ti ti-world"></i>
                      {/if}
                    </span>
                    <span class="lang-t">{t(o.l)}</span>
                    <span class="check"><i class="ti ti-check"></i></span>
                  </button>
                {/each}
              </div>

            {:else if cur.id === 'look'}
              <h1>{t('wz.look.title')}</h1>
              <p class="sub">{t('wz.look.sub')}</p>
              <div class="label">{t('wz.look.theme')}</div>
              <div class="theme-grid">
                {#each THEMES as th, i}
                  <button class="theme" class:sel={opt.theme === th.value} style="--i:{i}" onclick={() => set('theme', th.value)}>
                    <svg viewBox="0 0 120 72" class="th-prev">
                      {#if th.value === 'system'}
                        <clipPath id="th-half"><rect width="60" height="72" /></clipPath>
                        <rect width="120" height="72" rx="8" fill="#f3f3f5" />
                        <rect width="120" height="72" rx="8" fill="#16171b" clip-path="url(#th-half)" />
                        <rect x="8" y="10" width="44" height="8" rx="4" fill="#2a2c33" /><rect x="68" y="10" width="44" height="8" rx="4" fill="#e0e0e4" />
                        <rect x="8" y="26" width="44" height="36" rx="6" fill="#22242a" /><rect x="68" y="26" width="44" height="36" rx="6" fill="#fff" />
                      {:else}
                        <rect width="120" height="72" rx="8" fill={th.bg} />
                        <rect x="0" y="0" width="26" height="72" rx="8" fill={th.card} />
                        <rect x="7" y="10" width="12" height="12" rx="3" style="fill:var(--orange)" />
                        <rect x="7" y="28" width="12" height="12" rx="3" fill={th.text} opacity=".15" />
                        <rect x="34" y="10" width="78" height="22" rx="5" fill={th.card} />
                        <rect x="40" y="16" width="34" height="5" rx="2.5" fill={th.text} opacity=".8" />
                        <rect x="34" y="38" width="36" height="26" rx="5" fill={th.card} /><rect x="76" y="38" width="36" height="26" rx="5" fill={th.card} />
                        <rect x="40" y="56" width="16" height="4" rx="2" style="fill:var(--orange)" />
                      {/if}
                    </svg>
                    <span class="th-name">{t('wz.theme.' + th.value)}</span>
                    <span class="check"><i class="ti ti-check"></i></span>
                  </button>
                {/each}
              </div>
              <div class="label">{t('wz.look.accent')}</div>
              <div class="accents">
                {#each Object.entries(ACCENTS) as [key, a], i}
                  {@const sw = a.gradient ? `linear-gradient(135deg, ${a.main}, ${a.deep})` : a.main}
                  <button class="acc" class:sel={wiz.accent === key} style="background:{sw};--i:{i}" title={t(a.name)} aria-label={t(a.name)} onclick={() => pickAccent(key)}>
                    <i class="ti ti-check"></i>
                  </button>
                {/each}
              </div>
              <div class="rows">
                {@render row('ti-sparkles', t('wz.look.anim'), t('wz.look.animD'), opt.animations, () => set('animations', !opt.animations))}
                {@render row('ti-feather', t('wz.look.perf'), t('wz.look.perfD'), opt.performance, () => set('performance', !opt.performance))}
              </div>

            {:else if cur.id === 'account'}
              <AccountStep {accounts} {activeAccountId} {heads} {msStep} {googleStep} {skingate} {nickAsk}
                           {onMsLogin} {onMsCancel} {onGoogleLogin} {onGoogleCancel} {onPirate} {onConfirmNick} {onCancelNick}
                           bind:preview={skinPreview} />

            {:else if cur.id === 'files'}
              <h1>{t('wz.files.title')}</h1>
              <FilesStep {appDir} freeGB={Math.round(sysInfo.freeDiskMB / 1024)} onBrowse={browseAppDir}
                         onPathInput={(p) => p && applyAppDir(p)} bind:imported={migrated} />

            {:else if cur.id === 'game'}
              <h1>{t('wz.game.title')}</h1>
              <p class="sub">{t('wz.game.sub')}</p>
              <div class="sys">
                <div style="--i:0"><i class="ti ti-cpu"></i><span>{t('wz.game.cpu')}</span><b>{sysInfo.cpuCount || '—'}</b></div>
                <div style="--i:1"><i class="ti ti-database"></i><span>{t('wz.game.ram')}</span><b>{totalGB || '—'} {t('wz.unit.gb')}</b></div>
                <div style="--i:2"><i class="ti ti-device-sd-card"></i><span>{t('wz.game.disk')}</span><b>{Math.round(sysInfo.freeDiskMB / 1024) || '—'} {t('wz.unit.gb')}</b></div>
              </div>
              <div class="ram-head">
                <span class="label" style="margin:0">{t('wz.game.memory')}</span>
                <span class="ram-val">{ramGB} {t('wz.unit.gb')}</span>
              </div>
              <div class="ram" bind:this={ramTrackEl} tabindex="0" role="slider" aria-label={t('wz.game.memory')}
                   aria-valuemin={RAM_MIN} aria-valuemax={RAM_MAX} aria-valuenow={ramGB}
                   onpointerdown={(e) => { ramDragging = true; ramTrackEl?.setPointerCapture(e.pointerId); ramFromX(e.clientX) }}
                   onpointermove={(e) => { if (ramDragging) ramFromX(e.clientX) }}
                   onpointerup={(e) => { if (!ramDragging) return; ramDragging = false; try { ramTrackEl?.releasePointerCapture(e.pointerId) } catch {} persist() }}
                   onpointercancel={() => { if (ramDragging) { ramDragging = false; persist() } }}
                   onkeydown={(e: any) => {
                     if (e.key === 'ArrowLeft' || e.key === 'ArrowDown') { ramKey(-1); e.preventDefault() }
                     else if (e.key === 'ArrowRight' || e.key === 'ArrowUp') { ramKey(1); e.preventDefault() }
                   }}>
                <div class="ram-rail"><div class="ram-fill" class:warn={ramWarn} style="width:{ramPct}%"></div></div>
                <div class="ram-rec" style="left:{recPct}%" title={t('wz.game.rec', { n: recGB })}></div>
                <div class="ram-knob" class:drag={ramDragging} style="left:{ramPct}%"></div>
              </div>
              <div class="ram-foot">
                {#if ramWarn}
                  <span class="warn-t"><i class="ti ti-alert-triangle"></i> {t('wz.game.tooMuch')}</span>
                {:else}
                  <span>{t('wz.game.rec', { n: recGB })}</span>
                {/if}
                {#if ramGB !== recGB}
                  <button class="link-btn" onclick={() => { wiz.maxRAM = recGB * 1024; persist() }}>{t('wz.game.useRec')}</button>
                {/if}
              </div>
              <div class="label">{t('wz.game.whileRunning')}</div>
              {@render seg([
                { value: 'keep', label: t('wz.launch.keep'), icon: 'ti-app-window' },
                { value: 'minimize', label: t('wz.launch.minimize'), icon: 'ti-minimize' },
                { value: 'tray', label: t('wz.launch.tray'), icon: 'ti-layout-bottombar' },
              ], opt.gameLaunchAction, (v) => set('gameLaunchAction', v), t('wz.game.whileRunning'))}
              <div class="label">{t('wz.game.afterExit')}</div>
              {@render seg([
                { value: 'restore', label: t('wz.exit.restore') },
                { value: 'stay', label: t('wz.exit.stay') },
                { value: 'quit', label: t('wz.exit.quit') },
              ], opt.gameExitAction, (v) => set('gameExitAction', v), t('wz.game.afterExit'))}
              <div class="rows">
                {@render row('ti-terminal-2', t('wz.game.crash'), t('wz.game.crashD'), opt.showConsoleOnCrash, () => set('showConsoleOnCrash', !opt.showConsoleOnCrash))}
              </div>
              <button class="disclose" class:open={showAdvanced} onclick={() => (showAdvanced = !showAdvanced)}>
                <i class="ti ti-chevron-right"></i> {t('wz.game.jvm')}
              </button>
              {#if showAdvanced}
                <div class="adv" transition:fly={{ y: -6, duration: 220 }}>
                  <JvmArgsEditor value={wiz.javaArgs} onchange={(v: string) => { wiz.javaArgs = v; persist() }} />
                </div>
              {/if}

            {:else if cur.id === 'notify'}
              <h1>{t('wz.notify.title')}</h1>
              <p class="sub">{t('wz.notify.sub')}</p>
              <div class="rows">
                {@render row('ti-bell', t('wz.notify.on'), t('wz.notify.onD'), opt.notifyEnabled, () => set('notifyEnabled', !opt.notifyEnabled))}
                {@render row('ti-moon-stars', t('wz.notify.dnd'), t('wz.notify.dndD'), opt.notifyDnd, () => set('notifyDnd', !opt.notifyDnd), !opt.notifyEnabled)}
              </div>
              <div class="label" class:dim={!opt.notifyEnabled}>{t('wz.notify.dur')}</div>
              <div class:dim={!opt.notifyEnabled}>
                {@render seg([
                  { value: 5, label: t('wz.notify.sec', { n: 5 }) },
                  { value: 8, label: t('wz.notify.sec', { n: 8 }) },
                  { value: 15, label: t('wz.notify.sec', { n: 15 }) },
                ], opt.notifyDurationSec, (v) => set('notifyDurationSec', v), t('wz.notify.dur'))}
              </div>
              <div class="label">{t('wz.notify.friends')}</div>
              <div class="rows">
                {@render row('ti-brand-discord', t('wz.notify.discord'), t('wz.notify.discordD'), opt.discord, () => set('discord', !opt.discord))}
                {@render row('ti-world-share', t('wz.notify.p2p'), t('wz.notify.p2pD'), opt.p2p, () => set('p2p', !opt.p2p))}
                {#if !opt.p2p}
                  <div class="warn-box" transition:fly={{ y: -6, duration: 240 }}>
                    <i class="ti ti-alert-triangle"></i>
                    <div><b>{t('wz.p2pOff.title')}</b><small>{t('wz.p2pOff.text')}</small></div>
                  </div>
                {/if}
                {@render row('ti-cloud-check', t('wz.notify.cloud'), t('wz.notify.cloudD'), opt.cloudTime, () => set('cloudTime', !opt.cloudTime))}
              </div>

            {:else if cur.id === 'behavior'}
              <h1>{t('wz.beh.title')}</h1>
              <p class="sub">{t('wz.beh.sub')}</p>
              <div class="label">{t('wz.beh.startup')}</div>
              {@render seg([
                { value: 'never', label: t('wz.startup.never') },
                { value: 'window', label: t('wz.startup.window') },
                { value: 'background', label: t('wz.startup.background') },
              ], opt.startupMode, (v) => set('startupMode', v), t('wz.beh.startup'))}
              <div class="label">{t('wz.beh.close')}</div>
              {@render seg([
                { value: 'quit', label: t('wz.close.quit'), icon: 'ti-power' },
                { value: 'tray', label: t('wz.close.tray'), icon: 'ti-layout-bottombar' },
              ], opt.closeAction, (v) => set('closeAction', v), t('wz.beh.close'))}
              <div class="rows">
                {@render row('ti-refresh', t('wz.beh.update'), t('wz.beh.updateD'), opt.autoUpdate, () => set('autoUpdate', !opt.autoUpdate))}
              </div>

            {:else if cur.id === 'done'}
              <h1>{t('wz.done.title')}</h1>
              <p class="sub">{t('wz.done.sub')}</p>
              <div class="first-pack">
                <div class="fp-art" aria-hidden="true"><i class="ti ti-package"></i><span class="fp-plus"><i class="ti ti-plus"></i></span></div>
                <div class="fp-t"><b>{t('wz.first.title')}</b><small>{t('wz.first.text')}</small></div>
                <button class="btn btn-primary" disabled={finishing} onclick={() => finish(true)}><i class="ti ti-wand"></i> {t('wz.first.btn')}</button>
              </div>
              <div class="sum">
                {#each summary as it, i}
                  <button class="sum-it" style="--i:{i}" onclick={() => goto(it.to)}>
                    <span class="si-ico">
                      {#if it.head && mainAcc}<PlayerHead src={mainHead} size={26} radius={6} />
                      {:else if it.swatch}<span class="si-sw"></span>
                      {:else}<i class="ti {it.icon}"></i>{/if}
                    </span>
                    <span class="si-t"><small>{it.label}</small><b>{it.value}</b></span>
                    <i class="ti ti-pencil si-edit"></i>
                  </button>
                {/each}
              </div>
            {/if}
          </div>
        {/key}
      </div>

      <footer class="foot">
        <button class="btn btn-ghost back" class:hide={step === 1} onclick={prev}><i class="ti ti-arrow-left"></i> {t('wiz.back')}</button>
        <div class="foot-r">
          {#if step < LAST - 1}
            <button class="skip" onclick={() => goto(LAST)}>{t('wz.skip')}</button>
          {/if}
          <button class="btn btn-lg next" class:btn-primary={step !== LAST} class:btn-ghost={step === LAST} onclick={next} disabled={finishing || !ready}>
            {#key step}
              <span class="next-t" in:fly={{ y: 8, duration: 220 }}>
                {#if step === 1}{t('wz.start')} <i class="ti ti-arrow-right"></i>
                {:else if step === LAST}{t('wz.done.go')} <i class="ti ti-arrow-right"></i>
                {:else if cur.id === 'account' && (accounts.length === 0 || nickAsk)}{t('wz.later')} <i class="ti ti-arrow-right"></i>
                {:else}{t('wiz.next')} <i class="ti ti-arrow-right"></i>{/if}
              </span>
            {/key}
          </button>
        </div>
      </footer>
    </section>
  </div>
</div>

<style>
  .wz {
    position: relative; width: 100vw; height: 100vh; overflow: hidden;
    display: flex; flex-direction: column; background: var(--bg-app);
  }

  /* ── Фон: повільні кольорові плями + піксельна сітка ── */
  .bg { position: absolute; inset: 0; pointer-events: none; overflow: hidden; }
  .blob { position: absolute; border-radius: 50%; filter: blur(80px); opacity: .55; }
  .b1 { width: 520px; height: 520px; left: -120px; top: -160px; background: rgba(var(--orange-rgb), .45); animation: drift 22s ease-in-out infinite; }
  .b2 { width: 460px; height: 460px; right: -140px; bottom: -160px; background: rgba(123, 77, 255, .3); animation: drift 26s ease-in-out infinite reverse; }
  .b3 { width: 300px; height: 300px; left: 45%; top: 55%; background: rgba(var(--orange-rgb), .2); animation: drift 30s ease-in-out infinite -8s; }
  @keyframes drift { 0%, 100% { transform: translate(0, 0) scale(1) } 33% { transform: translate(60px, 40px) scale(1.1) } 66% { transform: translate(-40px, 30px) scale(.95) } }
  .grid {
    position: absolute; inset: 0; opacity: .5;
    background-image: linear-gradient(var(--border) 1px, transparent 1px), linear-gradient(90deg, var(--border) 1px, transparent 1px);
    background-size: 32px 32px;
    mask-image: radial-gradient(ellipse at center, #000 10%, transparent 70%);
  }
  :global([data-performance-mode='on']) .blob { display: none; }

  /* ── Топбар ── */
  .topbar {
    position: relative; z-index: 2; display: flex; align-items: center; justify-content: space-between;
    padding: 12px 16px; --wails-draggable: drag;
  }
  .brand { display: flex; align-items: center; gap: 9px; font-size: 13px; font-weight: 800; animation: rise .6s cubic-bezier(.22, 1, .36, 1) backwards; }
  .brand-ico { width: 28px; height: 28px; object-fit: contain; filter: drop-shadow(0 4px 10px rgba(var(--orange-rgb), .35)); }
  .ctrls { display: flex; gap: 6px; --wails-draggable: no-drag; }
  .ctrl {
    width: 30px; height: 30px; border-radius: 9px; border: 1px solid var(--border); background: var(--hover);
    color: var(--text-mute); display: grid; place-items: center; font-size: 14px; cursor: pointer;
    transition: background .15s, color .15s, transform .15s;
  }
  .ctrl:hover { background: var(--hover-strong); color: var(--text); transform: scale(1.06); }
  .ctrl.close:hover { background: var(--red); color: #fff; border-color: var(--red); }

  /* ── Картка ── */
  .shell {
    position: relative; z-index: 1; margin: 0 auto 22px; flex: 1; min-height: 0;
    width: min(1120px, calc(100vw - 40px)); max-height: 720px;
    display: grid; grid-template-columns: minmax(0, 44fr) minmax(0, 56fr);
    border-radius: 24px; overflow: hidden;
    background: color-mix(in srgb, var(--bg-window) 88%, transparent);
    border: 1px solid var(--border-hi);
    box-shadow: 0 40px 100px rgba(0, 0, 0, .45), inset 0 1px 0 rgba(255, 255, 255, .04);
    backdrop-filter: blur(18px);
    opacity: 0; transform: translateY(20px) scale(.98);
    transition: opacity .7s cubic-bezier(.22, 1, .36, 1), transform .7s cubic-bezier(.22, 1, .36, 1);
  }
  .shell.ready { opacity: 1; transform: none; }

  .stage {
    position: relative; display: grid; place-items: center; padding: 28px;
    background:
      radial-gradient(120% 80% at 30% 10%, rgba(var(--orange-rgb), .22), transparent 60%),
      linear-gradient(160deg, color-mix(in srgb, var(--bg-panel) 92%, var(--orange) 8%), var(--bg-panel));
    border-right: 1px solid var(--border);
  }
  .stage-in { grid-area: 1 / 1; width: 100%; height: 100%; display: grid; place-items: center; }
  .stage-steps { position: absolute; bottom: 22px; left: 50%; transform: translateX(-50%); display: flex; gap: 7px; }
  .dot { width: 7px; height: 7px; border-radius: 4px; background: var(--hover-strong); transition: width .35s cubic-bezier(.22, 1, .36, 1), background .3s; }
  .dot.done { background: rgba(var(--orange-rgb), .5); }
  .dot.act { width: 26px; background: var(--orange); }

  .main { display: flex; flex-direction: column; min-width: 0; min-height: 0; }

  /* ── Прогрес-ряд ── */
  .progress { position: relative; display: flex; justify-content: space-between; margin: 26px 40px 0; }
  .progress::before, .pr-fill { content: ''; position: absolute; left: 15px; right: 15px; top: 50%; height: 2px; margin-top: -1px; background: var(--border-hi); border-radius: 2px; }
  .pr-fill { right: auto; width: calc((100% - 30px) * var(--p) / 100); background: var(--orange); transition: width .5s cubic-bezier(.22, 1, .36, 1); }
  .pr-step {
    position: relative; z-index: 1; width: 30px; height: 30px; border-radius: 50%; border: 1.5px solid var(--border-hi);
    background: var(--bg-window); color: var(--text-dim); display: grid; place-items: center; font-size: 14px; cursor: pointer;
    transition: background .3s, color .3s, border-color .3s, transform .25s cubic-bezier(.34, 1.56, .64, 1), box-shadow .3s;
  }
  .pr-step:disabled { cursor: default; }
  .pr-step:not(:disabled):hover { transform: scale(1.12); color: var(--text); }
  .pr-step.done { background: rgba(var(--orange-rgb), .16); border-color: rgba(var(--orange-rgb), .6); color: var(--orange); }
  .pr-step.act { background: var(--orange); border-color: var(--orange); color: #fff; transform: scale(1.15); box-shadow: 0 0 0 5px rgba(var(--orange-rgb), .18); }

  /* ── Панелі кроків ── */
  .panes { flex: 1; min-height: 0; display: grid; overflow: hidden; }
  .pane { grid-area: 1 / 1; overflow-y: auto; padding: 26px 40px 20px; min-width: 0; }
  .pane::-webkit-scrollbar { width: 5px; }
  .pane::-webkit-scrollbar-thumb { background: var(--hover-strong); border-radius: 3px; }
  /* Кожен елемент кроку з'являється по черзі. */
  .pane > * { animation: rise .55s cubic-bezier(.22, 1, .36, 1) backwards; }
  .pane > :nth-child(1) { animation-delay: .16s } .pane > :nth-child(2) { animation-delay: .22s }
  .pane > :nth-child(3) { animation-delay: .28s } .pane > :nth-child(4) { animation-delay: .34s }
  .pane > :nth-child(5) { animation-delay: .4s } .pane > :nth-child(6) { animation-delay: .46s }
  .pane > :nth-child(7) { animation-delay: .52s } .pane > :nth-child(8) { animation-delay: .58s }
  .pane > :nth-child(n+9) { animation-delay: .64s }
  @keyframes rise { from { opacity: 0; transform: translateY(14px) } }
  @keyframes pop { from { opacity: 0; transform: scale(.85) } }

  .eyebrow { font-size: 11px; font-weight: 700; letter-spacing: .6px; text-transform: uppercase; color: var(--orange); margin-bottom: 8px; }
  h1 { font-size: 28px; font-weight: 800; line-height: 1.15; letter-spacing: -.3px; margin: 0; }
  .sub { font-size: 13.5px; color: var(--text-mute); line-height: 1.55; margin: 8px 0 18px; max-width: 520px; }
  .label { font-size: 11.5px; font-weight: 700; color: var(--text-mute); margin: 18px 0 8px; }
  .dim { opacity: .45; pointer-events: none; transition: opacity .2s; }

  .check {
    position: absolute; top: 8px; right: 8px; width: 20px; height: 20px; border-radius: 50%;
    display: grid; place-items: center; font-size: 12px; color: #fff; background: var(--orange);
    transform: scale(0); transition: transform .3s cubic-bezier(.34, 1.56, .64, 1);
  }
  .sel > .check { transform: scale(1); }

  /* ── Мова ── */
  .lang-list { display: flex; flex-direction: column; gap: 10px; max-width: 440px; }
  .lang {
    position: relative; display: flex; align-items: center; gap: 14px; padding: 14px 16px; border-radius: 14px;
    background: var(--bg-card); border: 1.5px solid var(--border); color: var(--text); font: inherit; cursor: pointer; text-align: left;
    transition: border-color .2s, transform .2s cubic-bezier(.34, 1.56, .64, 1), background .2s, box-shadow .2s;
    animation: rise .5s cubic-bezier(.22, 1, .36, 1) backwards; animation-delay: calc(.3s + var(--i) * .07s);
  }
  .lang:hover { transform: translateX(4px); border-color: var(--border-hi); }
  .lang:active { transform: scale(.98); }
  .lang.sel { border-color: var(--orange); background: rgba(var(--orange-rgb), .08); box-shadow: 0 8px 24px rgba(var(--orange-rgb), .15); }
  .lang .check { top: 50%; margin-top: -10px; right: 14px; }
  .flag { width: 36px; height: 24px; border-radius: 5px; overflow: hidden; display: grid; place-items: center; background: var(--hover-strong); box-shadow: 0 0 0 1px rgba(255, 255, 255, .1); flex-shrink: 0; }
  .flag svg { width: 100%; height: 100%; display: block; }
  .flag i { font-size: 17px; color: var(--text-mute); }
  .lang-t { font-size: 14px; font-weight: 700; }

  /* ── Тема ── */
  .theme-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 10px; }
  .theme {
    position: relative; padding: 8px 8px 10px; border-radius: 14px; background: var(--bg-card); border: 1.5px solid var(--border);
    color: var(--text); font: inherit; cursor: pointer; display: flex; flex-direction: column; gap: 8px; align-items: stretch;
    transition: border-color .2s, transform .2s cubic-bezier(.34, 1.56, .64, 1), box-shadow .2s;
    animation: pop .45s cubic-bezier(.34, 1.56, .64, 1) backwards; animation-delay: calc(.3s + var(--i) * .06s);
  }
  .theme:hover { transform: translateY(-3px); border-color: var(--border-hi); }
  .theme.sel { border-color: var(--orange); box-shadow: 0 10px 26px rgba(var(--orange-rgb), .2); }
  .th-prev { width: 100%; height: auto; border-radius: 8px; display: block; box-shadow: 0 0 0 1px rgba(255, 255, 255, .06); }
  .th-name { font-size: 12px; font-weight: 700; text-align: center; }

  .accents { display: flex; gap: 10px; flex-wrap: wrap; }
  .acc {
    width: 38px; height: 38px; border-radius: 12px; border: 2px solid transparent; cursor: pointer; color: #fff; display: grid; place-items: center;
    box-shadow: inset 0 0 0 1px rgba(255, 255, 255, .15);
    transition: transform .2s cubic-bezier(.34, 1.56, .64, 1), box-shadow .2s;
    animation: pop .4s cubic-bezier(.34, 1.56, .64, 1) backwards; animation-delay: calc(.35s + var(--i) * .04s);
  }
  .acc i { font-size: 18px; transform: scale(0); transition: transform .25s cubic-bezier(.34, 1.56, .64, 1); }
  .acc:hover { transform: translateY(-3px) scale(1.06); }
  .acc.sel { box-shadow: 0 0 0 3px var(--bg-window), 0 0 0 5px var(--orange); }
  .acc.sel i { transform: scale(1); }

  /* ── Рядки-перемикачі ── */
  .rows { display: flex; flex-direction: column; gap: 8px; margin-top: 16px; }
  .label + .rows { margin-top: 0; }
  .opt-row {
    display: flex; align-items: center; gap: 12px; padding: 11px 14px; border-radius: 13px;
    background: var(--bg-card); border: 1px solid var(--border); transition: border-color .2s, opacity .2s, transform .2s;
  }
  .opt-row:hover { border-color: var(--border-hi); }
  .opt-row.dis { opacity: .45; }
  .or-ico { width: 34px; height: 34px; border-radius: 10px; display: grid; place-items: center; flex-shrink: 0; font-size: 18px; color: var(--orange); background: rgba(var(--orange-rgb), .12); }
  .or-txt { display: flex; flex-direction: column; gap: 2px; flex: 1; min-width: 0; }
  .or-txt b { font-size: 13px; font-weight: 700; }
  .or-txt small { font-size: 11.5px; color: var(--text-mute); line-height: 1.4; }

  .tg {
    position: relative; width: 42px; height: 24px; border-radius: 12px; border: none; cursor: pointer; flex-shrink: 0;
    background: var(--hover-strong); transition: background .25s;
  }
  .tg:disabled { cursor: default; }
  .tg-k {
    position: absolute; top: 3px; left: 3px; width: 18px; height: 18px; border-radius: 50%; background: #fff;
    box-shadow: 0 2px 6px rgba(0, 0, 0, .3); transition: transform .3s cubic-bezier(.34, 1.56, .64, 1), width .2s;
  }
  .tg:active:not(:disabled) .tg-k { width: 22px; }
  .tg.on { background: var(--orange); }
  .tg.on .tg-k { transform: translateX(18px); }
  .tg.on:active:not(:disabled) .tg-k { transform: translateX(14px); }

  /* ── Сегменти ── */
  .seg {
    position: relative; display: grid; grid-template-columns: repeat(var(--n), minmax(0, 1fr)); padding: 4px; gap: 0;
    border-radius: 13px; background: var(--bg-input, var(--bg-card)); border: 1px solid var(--border);
  }
  .seg-pill {
    position: absolute; top: 4px; bottom: 4px; left: 4px; width: calc((100% - 8px) / var(--n));
    transform: translateX(calc(100% * var(--i))); border-radius: 10px; background: var(--orange);
    box-shadow: 0 6px 18px rgba(var(--orange-rgb), .35); transition: transform .4s cubic-bezier(.34, 1.3, .64, 1);
  }
  .seg button {
    position: relative; z-index: 1; border: none; background: none; font: inherit; color: var(--text-mute); cursor: pointer;
    padding: 9px 8px; font-size: 12.5px; font-weight: 700; display: flex; align-items: center; justify-content: center; gap: 6px;
    transition: color .25s; min-width: 0;
  }
  .seg button span { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .seg button i { font-size: 15px; }
  .seg button:hover { color: var(--text); }
  .seg button.sel { color: #fff; }

  .link-btn { border: none; background: none; color: var(--orange); font: inherit; font-size: 12.5px; font-weight: 700; cursor: pointer; padding: 6px 0; display: inline-flex; align-items: center; gap: 5px; }
  .link-btn:hover { text-decoration: underline; text-underline-offset: 3px; }

  /* ── Гра ── */
  .sys { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px; }
  .sys div {
    display: grid; grid-template-columns: auto 1fr; grid-template-rows: auto auto; column-gap: 10px; align-items: center;
    padding: 10px 12px; border-radius: 12px; background: var(--bg-card); border: 1px solid var(--border);
    animation: pop .45s cubic-bezier(.34, 1.56, .64, 1) backwards; animation-delay: calc(.3s + var(--i) * .06s);
  }
  .sys i { grid-row: span 2; font-size: 20px; color: var(--orange); }
  .sys span { font-size: 10.5px; color: var(--text-dim); text-transform: uppercase; letter-spacing: .4px; }
  .sys b { font-size: 14px; font-family: var(--font-mono); }
  .ram-head { display: flex; justify-content: space-between; align-items: baseline; margin: 20px 0 10px; }
  .ram-val { font-size: 22px; font-weight: 800; font-family: var(--font-mono); color: var(--orange); }
  .ram { position: relative; height: 26px; cursor: grab; touch-action: none; }
  .ram:active { cursor: grabbing; }
  .ram:focus-visible { outline: 2px solid var(--orange); outline-offset: 4px; border-radius: 8px; }
  .ram-rail { position: absolute; left: 0; right: 0; top: 50%; height: 8px; margin-top: -4px; border-radius: 4px; background: var(--hover-strong); overflow: hidden; }
  .ram-fill { height: 100%; border-radius: 4px; background: linear-gradient(90deg, var(--orange-2, var(--orange)), var(--orange)); transition: width .15s; }
  .ram-fill.warn { background: linear-gradient(90deg, var(--orange), var(--red)); }
  .ram-rec { position: absolute; top: 3px; width: 2px; height: 20px; margin-left: -1px; background: var(--green); border-radius: 1px; opacity: .8; }
  .ram-knob {
    position: absolute; top: 50%; width: 22px; height: 22px; margin: -11px 0 0 -11px; border-radius: 50%;
    background: #fff; box-shadow: 0 0 0 4px rgba(var(--orange-rgb), .35), 0 4px 12px rgba(0, 0, 0, .35);
    transition: left .15s, transform .2s cubic-bezier(.34, 1.56, .64, 1);
  }
  .ram-knob.drag { transform: scale(1.18); transition: transform .2s; }
  .ram-foot { display: flex; justify-content: space-between; align-items: center; margin-top: 6px; font-size: 11.5px; color: var(--text-mute); min-height: 26px; }
  .warn-t { color: var(--red); display: inline-flex; gap: 6px; align-items: center; }
  .disclose { margin-top: 14px; border: none; background: none; color: var(--text-mute); font: inherit; font-size: 12.5px; font-weight: 700; cursor: pointer; display: inline-flex; align-items: center; gap: 6px; padding: 4px 0; }
  .disclose:hover { color: var(--text); }
  .disclose i { transition: transform .25s cubic-bezier(.34, 1.56, .64, 1); }
  .disclose.open i { transform: rotate(90deg); }

  .warn-box {
    display: flex; gap: 10px; align-items: flex-start; padding: 11px 14px; border-radius: 13px;
    background: rgba(234, 179, 8, .08); border: 1px solid rgba(234, 179, 8, .4);
  }
  .warn-box i { color: #eab308; font-size: 18px; flex-shrink: 0; margin-top: 1px; }
  .warn-box div { display: flex; flex-direction: column; gap: 2px; }
  .warn-box b { font-size: 12.5px; } .warn-box small { font-size: 11.5px; color: var(--text-mute); line-height: 1.45; }

  .first-pack {
    display: flex; align-items: center; gap: 14px; margin-bottom: 14px; padding: 14px 16px; border-radius: 16px;
    background: linear-gradient(120deg, rgba(var(--orange-rgb), .2), rgba(var(--orange-rgb), .04)); border: 1.5px solid rgba(var(--orange-rgb), .5);
    animation: sum-in .6s cubic-bezier(.34, 1.4, .64, 1) backwards 1.4s;
  }
  .fp-art { position: relative; width: 48px; height: 48px; flex-shrink: 0; border-radius: 14px; display: grid; place-items: center; font-size: 24px; color: var(--orange); background: rgba(var(--orange-rgb), .15); }
  .fp-plus { position: absolute; right: -5px; bottom: -5px; width: 20px; height: 20px; border-radius: 50%; display: grid; place-items: center; font-size: 12px; color: #fff; background: var(--orange); box-shadow: 0 0 0 3px var(--bg-window); animation: fp-pulse 2s ease-in-out infinite 2s; }
  @keyframes fp-pulse { 0%, 100% { transform: scale(1) } 50% { transform: scale(1.15) } }
  .fp-t { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
  .fp-t b { font-size: 14px; } .fp-t small { font-size: 12px; color: var(--text-mute); line-height: 1.4; }

  /* ── Підсумок ── */
  .sum { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; }
  .sum-it {
    position: relative; display: flex; align-items: center; gap: 11px; padding: 10px 12px; border-radius: 13px; text-align: left;
    background: var(--bg-card); border: 1px solid var(--border); color: var(--text); font: inherit; cursor: pointer; min-width: 0;
    transition: border-color .2s, transform .2s cubic-bezier(.34, 1.56, .64, 1);
    animation: sum-in .55s cubic-bezier(.34, 1.4, .64, 1) backwards; animation-delay: calc(.45s + var(--i) * .07s);
  }
  @keyframes sum-in { from { opacity: 0; transform: translateY(16px) scale(.92) } }
  .sum-it:hover { border-color: rgba(var(--orange-rgb), .6); transform: translateY(-2px); }
  .si-ico { width: 32px; height: 32px; border-radius: 9px; display: grid; place-items: center; flex-shrink: 0; font-size: 17px; color: var(--orange); background: rgba(var(--orange-rgb), .12); }
  .si-sw { width: 18px; height: 18px; border-radius: 6px; background: linear-gradient(135deg, var(--orange), var(--orange-2, var(--orange))); }
  .si-t { display: flex; flex-direction: column; min-width: 0; flex: 1; }
  .si-t small { font-size: 10.5px; color: var(--text-dim); text-transform: uppercase; letter-spacing: .4px; }
  .si-t b { font-size: 13px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .si-edit { font-size: 14px; color: var(--text-dim); opacity: 0; transform: translateX(-4px); transition: opacity .2s, transform .2s; }
  .sum-it:hover .si-edit { opacity: 1; transform: none; }

  /* ── Футер ── */
  .foot { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 16px 40px 22px; border-top: 1px solid var(--border); }
  .back { transition: opacity .25s, transform .25s; }
  .back.hide { opacity: 0; pointer-events: none; transform: translateX(-8px); }
  .foot-r { display: flex; align-items: center; gap: 16px; }
  .skip { border: none; background: none; color: var(--text-mute); font: inherit; font-size: 12px; cursor: pointer; }
  .skip:hover { color: var(--text); text-decoration: underline; text-underline-offset: 3px; }
  .next { min-width: 150px; justify-content: center; overflow: hidden; transition: transform .2s cubic-bezier(.34, 1.56, .64, 1), box-shadow .2s; }
  .next:hover:not(:disabled) { transform: translateY(-2px); box-shadow: 0 12px 28px rgba(var(--orange-rgb), .4); }
  .next:active:not(:disabled) { transform: scale(.97); }
  .next-t { display: inline-flex; align-items: center; gap: 8px; }
  .next-t i { transition: transform .2s; }
  .next:hover .next-t .ti-arrow-right { transform: translateX(3px); }
  .next.go { animation: glow-btn 2.2s ease-in-out infinite 1.6s; }
  @keyframes glow-btn { 0%, 100% { box-shadow: 0 0 0 0 rgba(var(--orange-rgb), .5) } 50% { box-shadow: 0 0 0 10px rgba(var(--orange-rgb), 0) } }

  @media (max-width: 880px) {
    .shell { grid-template-columns: 1fr; }
    .stage { display: none; }
    .pane, .foot { padding-left: 24px; padding-right: 24px; }
    .progress { margin: 22px 24px 0; }
    .theme-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  }
  @media (max-height: 640px) { .shell { margin-bottom: 12px; } h1 { font-size: 23px; } }

  :global([data-animations='off']) .wz :global(*) { animation-duration: .01ms !important; animation-delay: 0s !important; animation-iteration-count: 1 !important; }
  @media (prefers-reduced-motion: reduce) {
    .wz :global(*) { animation-duration: .01ms !important; animation-delay: 0s !important; animation-iteration-count: 1 !important; transition-duration: .01ms !important; }
  }
</style>
