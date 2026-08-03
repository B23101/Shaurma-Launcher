<script lang="ts">
  // ── Розділ «Продуктивність» на сторінці збірки ─────────────────────────
  // Per-instance налаштування запуску: RAM (-Xmx/-Xms), JVM-аргументи,
  // окрема Java, вікно гри (повноекранний режим / розмір) та команди.
  //
  // Кожна група (RAM / вікно / команди) має ПЕРЕМИКАЧ «використовувати
  // глобальні налаштування» (за замовчуванням УВІМКНЕНИЙ = збірка бере
  // значення з налаштувань лаунчера). Вимкнений перемикач розблоковує
  // власні значення збірки. Значення 0/""/null = «успадкувати глобальні».
  import { t } from './i18n.svelte'
  import { App } from './wails'
  import type { InstanceConfig, Settings } from './types'

  let {
    cfg,
    settings,
    onUpdate,
    // Небезпечні дії обслуговування збірки (Полагодити / Переставити).
    // Живуть ТУТ, у «Продуктивності», а не в меню «…» нагорі сторінки —
    // біля кожної кнопки детально описано, що саме вона зробить, щоб
    // користувач не натискав їх наосліп.
    packName = '',
    repairing = false,
    reinstalling = false,
    onRepair = () => {},
    onReinstall = () => {}
  } = $props<{
    cfg: InstanceConfig
    settings: Settings
    onUpdate: (key: keyof InstanceConfig, value: InstanceConfig[keyof InstanceConfig]) => void
    packName?: string
    repairing?: boolean
    reinstalling?: boolean
    onRepair?: () => void
    onReinstall?: () => void
  }>()

  // ── RAM-слайдери (Max -Xmx та Min -Xms) ──
  const ramMin = 1024
  const ramMax = 16384
  const ramStep = 512
  const effectiveRAM = $derived(cfg.maxRamOverride > 0 ? cfg.maxRamOverride : settings.maxRAM)
  const ramPct = $derived(((effectiveRAM - ramMin) / (ramMax - ramMin)) * 100)
  const effectiveMinRAM = $derived(cfg.minRamOverride > 0 ? cfg.minRamOverride : Math.round(effectiveRAM / 2 / ramStep) * ramStep)
  const minRamPct = $derived(((effectiveMinRAM - ramMin) / (ramMax - ramMin)) * 100)

  function setRamFromClick(e: MouseEvent) {
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect()
    let pct = (e.clientX - r.left) / r.width
    pct = Math.max(0, Math.min(1, pct))
    const v = Math.round((ramMin + pct * (ramMax - ramMin)) / ramStep) * ramStep
    onUpdate('maxRamOverride', Math.max(ramMin, Math.min(ramMax, v)))
  }
  function setMinRamFromClick(e: MouseEvent) {
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect()
    let pct = (e.clientX - r.left) / r.width
    pct = Math.max(0, Math.min(1, pct))
    const v = Math.round((ramMin + pct * (ramMax - ramMin)) / ramStep) * ramStep
    onUpdate('minRamOverride', Math.max(ramMin, Math.min(effectiveRAM, v)))
  }

  // «Глобально» = обидва override порожні (0). Вимкнення перемикача
  // ініціалізує своє значення поточним глобальним, щоб користувач бачив
  // точку старту, а не порожній слайдер.
  const ramGlobal = $derived(cfg.maxRamOverride === 0 && cfg.minRamOverride === 0)
  function toggleRamGlobal() {
    if (ramGlobal) {
      onUpdate('maxRamOverride', Math.max(ramMin, Math.min(ramMax, settings.maxRAM || 4096)))
      onUpdate('minRamOverride', 0)
    } else {
      onUpdate('maxRamOverride', 0)
      onUpdate('minRamOverride', 0)
    }
  }

  // ── Вікно ──
  const effectiveFullscreen = $derived(cfg.fullscreenOverride ?? settings.fullscreen)
  const windowGlobal = $derived(
    cfg.windowWidthOverride === 0 && cfg.windowHeightOverride === 0 && cfg.fullscreenOverride == null
  )
  function toggleWindowGlobal() {
    if (windowGlobal) {
      onUpdate('windowWidthOverride', settings.windowWidth || 854)
      onUpdate('windowHeightOverride', settings.windowHeight || 480)
    } else {
      onUpdate('windowWidthOverride', 0)
      onUpdate('windowHeightOverride', 0)
      onUpdate('fullscreenOverride', null)
    }
  }

  // ── Команди ──
  // Стан тримає явний прапорець cfg.useCustomCommands (false = глобальні
  // команди лаунчера). Раніше ми виводили стан з ПОРОЖНЬОСТІ чотирьох
  // override-полів — але коли глобальні команди порожні (типовий випадок),
  // вмикання «своїх» писало порожні рядки, derived знову ставав true, і
  // тогл неможливо було вимкнути. Прапорець зберігається в InstanceConfig
  // і переживає перезапуск; значення override при цьому залишаються
  // «успадкованими», доки користувач не введе свої.
  const commandsGlobal = $derived(!cfg.useCustomCommands)
  function toggleCommandsGlobal() {
    if (commandsGlobal) {
      // Перехід у «свої команди»: стартуємо з глобальних значень (як
      // RAM/вікно), щоб користувач бачив точку старту. Якщо глобальні
      // порожні — поля лишаються порожніми, але прапорець уже вимкнений,
      // тож тогл чесно показує «свої» і поля відкриті для вводу.
      onUpdate('useCustomCommands', true)
      onUpdate('preLaunchCommandOverride', settings.preLaunchCommand || '')
      onUpdate('wrapperCommandOverride', settings.wrapperCommand || '')
      onUpdate('postExitCommandOverride', settings.postExitCommand || '')
      onUpdate('envVarsOverride', settings.envVars || '')
    } else {
      onUpdate('useCustomCommands', false)
      onUpdate('preLaunchCommandOverride', '')
      onUpdate('wrapperCommandOverride', '')
      onUpdate('postExitCommandOverride', '')
      onUpdate('envVarsOverride', '')
    }
  }
</script>

<div class="card">
  <div class="card-head">
    <div class="ci green"><i class="ti ti-cpu"></i></div>
    <div class="ctw"><div class="ct">{t('pack.perf.ram')}</div><div class="cd">{t('pack.perf.globalHint')}</div></div>
  </div>
  <div class="row">
    <div class="row-label"><div class="rl-name">{t('pack.perf.useGlobal')}</div><div class="rl-sub">{t('pack.perf.useGlobalDesc')}</div></div>
    <div class="toggle" class:on={ramGlobal} onclick={toggleRamGlobal}></div>
  </div>
  {#if ramGlobal}
    <div class="row" style="border-bottom:none">
      <div class="row-label"><div class="rl-name">{t('pack.perf.inheritedRam')}</div></div>
      <span class="mono text-orange">{settings.maxRAM} МБ</span>
    </div>
  {:else}
    <div class="row col">
      <div class="rl-name flex" style="justify-content:space-between">
        <span>{t('pack.perf.ram')}</span><span class="mono text-orange">{effectiveRAM} МБ</span>
      </div>
      <div class="ram-track" onclick={setRamFromClick}>
        <div class="ram-fill" style="width:{ramPct}%"></div>
        <div class="ram-knob" style="left:{ramPct}%"></div>
      </div>
    </div>
    <div class="row col">
      <div class="rl-name flex" style="justify-content:space-between">
        <span>{t('pack.game.ramMin')}</span><span class="mono text-orange">{effectiveMinRAM} МБ</span>
      </div>
      <div class="ram-track" onclick={setMinRamFromClick}>
        <div class="ram-fill" style="width:{minRamPct}%"></div>
        <div class="ram-knob" style="left:{minRamPct}%"></div>
      </div>
      <div class="rl-sub">{t('pack.game.ramMinHint')}</div>
    </div>
  {/if}
</div>

<div class="card">
  <div class="card-head">
    <div class="ci"><i class="ti ti-code"></i></div>
    <div class="ctw"><div class="ct">{t('pack.perf.jvm')}</div></div>
  </div>
  <div class="row col">
    <div class="rl-name">{t('pack.perf.jvmArgs')}</div>
    <input class="input mono" value={cfg.javaArgsOverride} placeholder={t('pack.perf.jvmArgsPlaceholder')}
           onchange={(e: any) => onUpdate('javaArgsOverride', e.target.value)} />
  </div>
  <div class="row">
    <div class="row-label"><div class="rl-name">{t('pack.perf.separateJava')}</div></div>
    <div class="toggle" class:on={cfg.useSeparateJava} onclick={() => onUpdate('useSeparateJava', !cfg.useSeparateJava)}></div>
  </div>
  {#if cfg.useSeparateJava}
    <div class="row col">
      <div class="rl-name">{t('pack.perf.javaPath')}</div>
      <div class="folder-row">
        <input class="input mono" style="flex:1" value={cfg.javaPathOverride}
               onchange={(e: any) => onUpdate('javaPathOverride', e.target.value)} />
        <button class="btn btn-ghost" onclick={async () => { const p = await App.BrowseFolder(); if (p) onUpdate('javaPathOverride', p) }}>
          <i class="ti ti-folder"></i>
        </button>
      </div>
    </div>
  {/if}
</div>

<div class="card">
  <div class="card-head">
    <div class="ci violet"><i class="ti ti-window-maximize"></i></div>
    <div class="ctw"><div class="ct">{t('pack.perf.window')}</div><div class="cd">{t('pack.perf.globalHint')}</div></div>
  </div>
  <div class="row">
    <div class="row-label"><div class="rl-name">{t('pack.perf.useGlobal')}</div><div class="rl-sub">{t('pack.perf.useGlobalDesc')}</div></div>
    <div class="toggle" class:on={windowGlobal} onclick={toggleWindowGlobal}></div>
  </div>
  {#if windowGlobal}
    <div class="row" style="border-bottom:none">
      <div class="row-label"><div class="rl-name">{t('pack.perf.inheritedWindow')}</div></div>
      <span class="mono muted">{effectiveFullscreen ? t('pack.perf.fullscreen') : `${settings.windowWidth}×${settings.windowHeight}`}</span>
    </div>
  {:else}
    <div class="row">
      <div class="row-label"><div class="rl-name">{t('pack.perf.fullscreen')}</div></div>
      <div class="toggle" class:on={effectiveFullscreen} onclick={() => onUpdate('fullscreenOverride', !effectiveFullscreen)}></div>
    </div>
    {#if !effectiveFullscreen}
      <div class="row">
        <div class="row-label"><div class="rl-name">{t('pack.perf.windowWidth')}</div></div>
        <input class="input mono" style="max-width:110px" type="number" value={cfg.windowWidthOverride || settings.windowWidth}
               onchange={(e: any) => onUpdate('windowWidthOverride', Number(e.target.value) || 0)} />
      </div>
      <div class="row">
        <div class="row-label"><div class="rl-name">{t('pack.perf.windowHeight')}</div></div>
        <input class="input mono" style="max-width:110px" type="number" value={cfg.windowHeightOverride || settings.windowHeight}
               onchange={(e: any) => onUpdate('windowHeightOverride', Number(e.target.value) || 0)} />
      </div>
    {/if}
  {/if}
</div>

<div class="card">
  <div class="card-head">
    <div class="ci"><i class="ti ti-terminal"></i></div>
    <div class="ctw"><div class="ct">{t('pack.perf.commands')}</div><div class="cd">{t('pack.perf.globalHint')}</div></div>
  </div>
  <div class="row">
    <div class="row-label"><div class="rl-name">{t('pack.perf.useGlobal')}</div><div class="rl-sub">{t('pack.perf.useGlobalDesc')}</div></div>
    <div class="toggle" class:on={commandsGlobal} onclick={toggleCommandsGlobal}></div>
  </div>
  {#if !commandsGlobal}
    <div class="row col">
      <div class="rl-name">{t('pack.perf.preLaunch')}</div>
      <input class="input mono" value={cfg.preLaunchCommandOverride} placeholder={t('pack.perf.jvmArgsPlaceholder')}
             onchange={(e: any) => onUpdate('preLaunchCommandOverride', e.target.value)} />
    </div>
    <div class="row col">
      <div class="rl-name">{t('pack.perf.wrapper')}</div>
      <input class="input mono" value={cfg.wrapperCommandOverride} placeholder={t('pack.perf.jvmArgsPlaceholder')}
             onchange={(e: any) => onUpdate('wrapperCommandOverride', e.target.value)} />
    </div>
    <div class="row col">
      <div class="rl-name">{t('pack.perf.postExit')}</div>
      <input class="input mono" value={cfg.postExitCommandOverride} placeholder={t('pack.perf.jvmArgsPlaceholder')}
             onchange={(e: any) => onUpdate('postExitCommandOverride', e.target.value)} />
    </div>
    <div class="row col">
      <div class="rl-name">{t('pack.perf.envVars')}</div>
      <textarea class="input mono" rows="3" style="resize:vertical;font-size:11px" value={cfg.envVarsOverride}
                placeholder={t('pack.perf.jvmArgsPlaceholder')}
                onchange={(e: any) => onUpdate('envVarsOverride', e.target.value)}></textarea>
    </div>
  {/if}
</div>

<!-- ── Обслуговування збірки ──
     Небезпечні дії винесені сюди (розділ «Продуктивність»), а не в меню
     «…» нагорі: кожна кнопка супроводжується детальним описом того, що
     саме вона зробить. «Полагодити» перевіряє/перезаписує пошкоджені
     файли гри, «Переставити» видаляє все і качає з нуля. -->
<div class="card">
  <div class="card-head">
    <div class="ci" style="background:rgba(239,68,68,.13);color:var(--red)"><i class="ti ti-tool"></i></div>
    <div class="ctw"><div class="ct">{t('pack.perf.maintenance')}</div><div class="cd">{t('pack.perf.maintenanceDesc')}</div></div>
  </div>
  <div class="row col">
    <div class="rl-name">{t('pack.perf.repairName')}</div>
    <div class="rl-sub">{t('pack.perf.repairDesc')}</div>
    <button class="btn btn-ghost btn-sm" onclick={onRepair} disabled={repairing} style="align-self:flex-start">
      <i class="ti {repairing ? 'ti-loader spin' : 'ti-tools'}"></i>
      {repairing ? t('pack.repairing') : t('pack.repair')}
    </button>
  </div>
  <div class="row col">
    <div class="rl-name">{t('pack.perf.reinstallName')}</div>
    <div class="rl-sub">{t('pack.perf.reinstallDesc', { name: packName })}</div>
    <button class="btn btn-danger btn-sm" onclick={onReinstall} disabled={reinstalling} style="align-self:flex-start">
      <i class="ti {reinstalling ? 'ti-loader spin' : 'ti-recycle'}"></i>
      {reinstalling ? t('pack.reinstalling') : t('pack.reinstall')}
    </button>
  </div>
</div>
