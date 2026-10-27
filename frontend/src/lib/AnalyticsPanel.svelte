<script lang="ts">
  // ══════════════════════════════════════════════════════════════════════
  // Аналітика ігрового часу (розділ «Аналітика» у «Моїх збірках»).
  //
  //   • Великий підсумок «Твій ігровий час» + стовпчики за останні 7 днів
  //     (день тижня під кожним стовпчиком — як у макеті: Thu..Wed).
  //   • Перемикач масштабу діаграми: 7 днів / Тижні / Місяці / Роки.
  //   • Плитки підсумків: збірки, середня сесія, зіграні та модовані збірки.
  //   • Топ збірок «де ти грав найбільше» (іконка + назва + час + бар).
  //
  // Дані — один виклик App.GetAnalytics() (бекенд рахує з таблиці сесій
  // playtime_sessions). Назви/іконки/кольори збірок домішуються тут з
  // builds (BuildView[]), щоб бекенду не треба було знати про реєстр.
  // ══════════════════════════════════════════════════════════════════════

  import { t } from './i18n.svelte'
  import { packAsset } from './packAssets.svelte'
  import { App } from './wails'
  import { fly } from 'svelte/transition'
  import { cubicOut } from 'svelte/easing'
  import SelectField from './SelectField.svelte'
  import type { Account, AnalyticsPoint, AnalyticsSummary, BuildView } from './types'

  let {
    summary = null,
    builds = [],
    accounts = [],
    activeAccountName = '',
    lang = 'uk',
    heads = {},
  }: {
    summary?: AnalyticsSummary | null
    builds?: BuildView[]
    accounts?: Account[]
    // Активний гравець — за НІКОМ (не ID акаунта): нік переживає вихід з
    // акаунта, тож аналітика не «губиться» після перевходу.
    activeAccountName?: string
    lang?: string
    // 2D-голови акаунтів (data URL зі скінів) — для селектора гравця в
    // стилі лаунчера, де поряд з іменем показується голова скіна акаунта.
    heads?: Record<string, string>
  } = $props()

  // ── Аналітика окремо по кожному гравцю (за НІКОМ) ──────────────────────
  // Вибір гравця вгорі панелі: "Всі акаунти" (лише коли їх більше одного)
  // або конкретний нік. Дані перекачуються з бекенда (GetAnalytics з
  // accountName) і показуються замість переданого summary. Порожній
  // activeAccountName (немає акаунтів) = сумарно по всіх. Нік — стабільний
  // ключ: навіть якщо гравець вийшов з акаунта і зайшов заново (або інший
  // акаунт на той самий нік), аналітика лишається прив'язаною.
  let selAccountName = $state(activeAccountName || '')
  let liveSummary = $state<AnalyticsSummary | null>(null)
  let liveLoading = $state(false)
  // seq — захист від гонки при швидкому перемиканні гравців: старий,
  // повільніший запит не перезапише свіжіший.
  let seq = 0

  const accountOptions = $derived([
    ...(accounts.length > 1 ? [{ value: '', label: t('analytics.allAccounts') }] : []),
    ...accounts.map((a) => ({
      value: a.username,
      label: a.username,
      // Голова скіна акаунта (та сама, що в sidebar): 2D з обох слоїв.
      head: (a.uuid && heads[a.uuid]) || heads[a.username] || undefined,
    })),
  ])

  async function loadForAccount(name: string) {
    const mySeq = ++seq
    liveLoading = true
    try {
      const res = (await App.GetAnalytics(name)) ?? null
      if (mySeq !== seq) return
      liveSummary = res
    } catch {
      if (mySeq !== seq) return
      liveSummary = null
    } finally {
      if (mySeq === seq) liveLoading = false
    }
  }

  // При зміні перекинутого summary (напр. після завершеної ігрової сесії
  // App перекачав свіжі дані) або вибору гравця — оновлюємо live-дані.
  $effect(() => {
    void summary
    loadForAccount(selAccountName)
  })

  // Ефективний знімок: live-дані (якщо завантажились) або переданий summary.
  const eff = $derived(liveSummary ?? summary)
  const effLoading = $derived(liveLoading && !liveSummary)

  // ── Масштаб діаграми ──
  type ChartMode = 'days' | 'weeks' | 'months' | 'years'
  let mode = $state<ChartMode>('days')
  const modes: { id: ChartMode; label: string }[] = [
    { id: 'days', label: t('analytics.tab.days') },
    { id: 'weeks', label: t('analytics.tab.weeks') },
    { id: 'months', label: t('analytics.tab.months') },
    { id: 'years', label: t('analytics.tab.years') },
  ]

  // ── Серія поточного масштабу ──
  const series = $derived<AnalyticsPoint[]>(
    eff
      ? mode === 'days' ? eff.last7Days
        : mode === 'weeks' ? eff.weeks
        : mode === 'months' ? eff.months
        : eff.years
      : []
  )

  const maxSeconds = $derived(series.reduce((m, p) => Math.max(m, p.seconds), 0))

  // ── Форматування ──
  function hoursLabel(seconds: number): string {
    const h = seconds / 3600
    return `${h.toFixed(1)} ${t('analytics.hoursShort')}`
  }

  // «1 год 25 хв» — той самий стиль, що на картках збірок (fmtPlaytime).
  function durationLabel(seconds: number): string {
    if (!seconds || seconds <= 0) return '—'
    const h = Math.floor(seconds / 3600)
    const m = Math.floor((seconds % 3600) / 60)
    if (h === 0) {
      const s = Math.floor(seconds % 60)
      return m === 0 ? `${s} ${t('analytics.secShort')}` : `${m} ${t('analytics.minShort')}`
    }
    return `${h} ${t('analytics.hourShort')} ${m} ${t('analytics.minShort')}`
  }

  // Локалізований підпис бакета: «Чт» для днів, «20.07» для тижнів,
  // «Сер» для місяців (з роком на першому стовпчику/у січні), «2026» — роки.
  let dateFmt: Intl.DateTimeFormat | null = null
  let monthFmt: Intl.DateTimeFormat | null = null
  let dayFmt: Intl.DateTimeFormat | null = null
  try {
    dateFmt = new Intl.DateTimeFormat(lang, { day: '2-digit', month: '2-digit' })
    monthFmt = new Intl.DateTimeFormat(lang, { month: 'short' })
    dayFmt = new Intl.DateTimeFormat(lang, { weekday: 'short' })
  } catch {
    dateFmt = new Intl.DateTimeFormat('uk', { day: '2-digit', month: '2-digit' })
    monthFmt = new Intl.DateTimeFormat('uk', { month: 'short' })
    dayFmt = new Intl.DateTimeFormat('uk', { weekday: 'short' })
  }

  function parseStart(start: string): Date {
    // Бакет дня/тижня/місяця — "2006-01-02" (локальний початок доби);
    // рік — "2006". Суфікс T00:00:00 без Z = локальний час, без зсуву.
    return new Date(start.length === 4 ? `${start}-01-01` : `${start}T00:00:00`)
  }

  function bucketLabel(p: AnalyticsPoint, i: number): string {
    const d = parseStart(p.start)
    if (mode === 'days') {
      const label = dayFmt?.format(d) ?? ''
      return label.charAt(0).toUpperCase() + label.slice(1)
    }
    if (mode === 'weeks') return dateFmt?.format(d) ?? p.start
    if (mode === 'years') return p.start
    // Місяці: коротка назва; рік додаємо на першому стовпчику та в січні
    // (там починається новий рік — інакше підписи незрозумілі).
    const month = monthFmt?.format(d) ?? ''
    const short = month.charAt(0).toUpperCase() + month.slice(1)
    if (i === 0 || d.getMonth() === 0) return `${short} ${String(d.getFullYear()).slice(2)}`
    return short
  }

  // ── Топ збірок: склейка з реєстром для назв/іконок/кольорів ──
  const topWithBuild = $derived((eff?.topInstances ?? []).map((ai) => {
    const bv = builds.find((b) => b.build.id === ai.instanceId)
    return {
      ai,
      name: bv?.build.name ?? ai.instanceId,
      icon: packAsset(bv?.build.iconUrl ?? ''),
      preset: bv?.build.icon,
      color: bv?.build.color || 'var(--orange)',
      loader: bv?.build.loaderType || '',
      mc: bv?.build.mcVersion || '',
    }
  }))
  const topMax = $derived(topWithBuild.reduce((m, r) => Math.max(m, r.ai.seconds), 0))

  // ── Плитки підсумків ──
  const totalInstances = $derived(builds.length)
  const moddedInstances = $derived(
    builds.filter((b) => {
      const l = (b.build.loaderType || '').toLowerCase()
      return l !== '' && l !== 'vanilla'
    }).length
  )
  const avgSession = $derived(eff?.avgSessionSeconds ?? 0)
  const playedInstances = $derived(eff?.playedInstances ?? 0)
  const totalSeconds = $derived(eff?.totalSeconds ?? 0)
  const hasAny = $derived(totalSeconds > 0)

  const tiles = $derived([
    { key: 'instances', icon: 'ti-layout-grid', value: String(totalInstances), label: t('analytics.instances'), accent: 'orange' },
    { key: 'avg', icon: 'ti-hourglass-high', value: durationLabel(avgSession), label: t('analytics.avgSession'), accent: 'violet' },
    { key: 'played', icon: 'ti-player-play', value: String(playedInstances), label: t('analytics.playedInstances'), accent: 'green' },
    { key: 'modded', icon: 'ti-puzzle', value: String(moddedInstances), label: t('analytics.moddedInstances'), accent: 'blue' },
  ])

  // Дні тижня у підказці стовпчика: повна назва + дата.
  function fullLabel(p: AnalyticsPoint): string {
    const d = parseStart(p.start)
    return `${dayFmt?.format(d) ?? ''}, ${dateFmt?.format(d) ?? p.start}`
  }
</script>

<div class="an-panel" in:fly={{ y: 12, duration: 320, easing: cubicOut }}>
  <!-- Шапка розділу -->
  <div class="an-head">
    <div class="an-title">
      <span class="an-title-ic"><i class="ti ti-chart-bar"></i></span>
      <div>
        <div class="an-name">{t('analytics.title')}</div>
        <div class="an-sub">{t('analytics.subtitle')}</div>
      </div>
    </div>
    <!-- Аналітика окремо по кожному акаунту: селектор гравця (лише коли
         акаунтів більше одного). "Всі акаунти" — сумарний знімок. -->
    {#if accountOptions.length > 1}
      <label class="an-acc">
        <i class="ti ti-user"></i>
        <SelectField
          value={selAccountName}
          options={accountOptions}
          onchange={(v) => { selAccountName = v; loadForAccount(v) }}
          ariaLabel={t('analytics.accountFilter')}
          minWidth="150px"
        />
        {#if effLoading}<span class="an-acc-spin"><i class="ti ti-loader spin"></i></span>{/if}
      </label>
    {/if}
  </div>

  <div class="an-body">
    <!-- Ліва колонка: підсумок + діаграма -->
    <div class="an-main">
      <!-- Hero: «Твій ігровий час» -->
      <div class="an-hero">
        <div class="an-hero-glow"></div>
        <div class="an-hero-label">
          <i class="ti ti-clock-hour-4"></i>
          {t('analytics.yourPlaytime')}
        </div>
        <div class="an-hero-value">{hoursLabel(totalSeconds)}</div>
        <div class="an-hero-caption">{t('analytics.hoursPlayed')}</div>
        {#if !hasAny}
          <div class="an-hero-empty">
            <i class="ti ti-mood-empty"></i>
            {t('analytics.noPlaytime')}
          </div>
        {/if}
      </div>

      <!-- Діаграма -->
      <div class="an-chart-card">
        <div class="an-chart-head">
          <div class="an-chart-tabs" role="tablist">
            {#each modes as m}
              <button
                class="an-chart-tab {mode === m.id ? 'active' : ''}"
                onclick={() => (mode = m.id)}
                role="tab"
                aria-selected={mode === m.id}
              >
                {m.label}
              </button>
            {/each}
          </div>
        </div>

        <div class="an-chart">
          {#if series.length === 0}
            <div class="an-chart-empty"><i class="ti ti-chart-dots"></i></div>
          {:else}
            {#each series as p, i (mode + p.start)}
              <div class="an-col" title={`${fullLabel(p)} · ${durationLabel(p.seconds)}`}>
                <div class="an-col-track">
                  {#if p.seconds > 0}
                    <div
                      class="an-col-bar"
                      style="height:{Math.max(4, Math.round(p.seconds / Math.max(1, maxSeconds) * 100))}%;animation-delay:{i * 45}ms;"
                    ></div>
                  {/if}
                  {#if p.seconds > 0 && p.seconds === maxSeconds}
                    <div class="an-col-max"><i class="ti ti-trophy"></i></div>
                  {/if}
                </div>
                <div class="an-col-label" class:dim={!hasAny}>{bucketLabel(p, i)}</div>
              </div>
            {/each}
          {/if}
        </div>

        {#if !hasAny}
          <div class="an-chart-note">{t('analytics.chartEmptyHint')}</div>
        {/if}
      </div>

      <!-- Топ збірок -->
      <div class="an-top">
        <div class="an-top-head">
          <div class="an-top-title"><i class="ti ti-trophy"></i> {t('analytics.topInstances')}</div>
          <div class="an-top-sub">{t('analytics.topInstancesHint')}</div>
        </div>

        {#if topWithBuild.length === 0}
          <div class="an-top-empty">
            <i class="ti ti-trophy-off"></i>
            <span>{t('analytics.topEmpty')}</span>
          </div>
        {:else}
          <div class="an-top-list">
            {#each topWithBuild as row, i (row.ai.instanceId)}
              <div
                class="an-top-row"
                style="--pcol:{row.color}"
                in:fly={{ y: 8, duration: 260, delay: i * 40, easing: cubicOut }}
              >
                <div class="an-top-rank">{i + 1}</div>
                <div class="an-top-ic" style="--pcol:{row.color}">
                  {#if row.icon}
                    <img src={row.icon} alt="" />
                  {:else}
                    <i class="ti {row.preset || 'ti-puzzle'}" style="color:var(--pcol)"></i>
                  {/if}
                </div>
                <div class="an-top-info">
                  <div class="an-top-name">{row.name}</div>
                  <div class="an-top-meta">
                    {#if row.loader}{row.loader} {row.mc}{:else}{row.mc}{/if}
                    <span class="an-top-sessions">{row.ai.sessions} {t('analytics.sessionsCount')}</span>
                  </div>
                  <div class="an-top-track">
                    <div class="an-top-fill" style="width:{topMax ? Math.max(4, Math.round(row.ai.seconds / topMax * 100)) : 0}%"></div>
                  </div>
                </div>
                <div class="an-top-time">{durationLabel(row.ai.seconds)}</div>
              </div>
            {/each}
          </div>
        {/if}
      </div>
    </div>

    <!-- Права колонка: плитки підсумків -->
    <div class="an-side">
      <div class="an-tiles">
        {#each tiles as tile, i (tile.key)}
          <div
            class="an-tile an-tile-{tile.accent}"
            in:fly={{ y: 10, duration: 280, delay: 80 + i * 60, easing: cubicOut }}
          >
            <div class="an-tile-ic"><i class="ti {tile.icon}"></i></div>
            <div class="an-tile-value">{tile.value}</div>
            <div class="an-tile-label">{tile.label}</div>
          </div>
        {/each}
      </div>

      {#if hasAny}
        <div class="an-fact">
          <i class="ti ti-info-circle"></i>
          <span>{t('analytics.fact', { sessions: eff?.totalSessions ?? 0 })}</span>
        </div>
      {/if}
    </div>
  </div>
</div>

<style>
  /* ═══════════ PANEL ═══════════ */
  .an-panel {
    position: relative;
    display: flex; flex-direction: column;
    gap: 14px;
    margin: 4px 0 18px;
    padding: 18px 18px 20px;
    border-radius: 18px;
    background:
      linear-gradient(180deg, color-mix(in srgb, var(--bg-panel) 88%, transparent), color-mix(in srgb, var(--bg-card) 82%, transparent));
    border: 1px solid var(--border);
    backdrop-filter: blur(14px);
    -webkit-backdrop-filter: blur(14px);
    box-shadow: 0 14px 44px rgba(0, 0, 0, 0.22);
    overflow: hidden;
  }
  .an-panel::before {
    content: '';
    position: absolute; inset: 0;
    background: radial-gradient(60% 45% at 12% 0%, rgba(var(--orange-rgb), 0.07), transparent 60%);
    pointer-events: none;
  }

  .an-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; position: relative; z-index: 1; }
  .an-acc {
    display: inline-flex; align-items: center; gap: 8px;
    padding: 4px 8px 4px 10px;
    background: var(--bg-input); border: 1px solid var(--border-hi);
    border-radius: 11px; font-size: 11.5px; font-weight: 700; color: var(--text-mute);
    transition: border-color .15s, box-shadow .15s;
  }
  .an-acc:focus-within { border-color: color-mix(in srgb, var(--orange) 60%, transparent); box-shadow: 0 0 0 3px rgba(var(--orange-rgb), .12); }
  .an-acc .ti-user { color: var(--orange); font-size: 13px; }
  .an-acc-spin { display: inline-flex; color: var(--orange); font-size: 12px; }
  .an-title { display: flex; align-items: center; gap: 12px; }
  .an-title-ic {
    width: 40px; height: 40px;
    display: flex; align-items: center; justify-content: center;
    border-radius: 12px;
    background: linear-gradient(135deg, var(--orange), var(--orange-2));
    color: #fff; font-size: 18px;
    box-shadow: 0 8px 22px rgba(var(--orange-rgb), 0.35);
  }
  .an-name { font-size: 15px; font-weight: 800; letter-spacing: -0.2px; color: var(--text); }
  .an-sub { font-size: 10.5px; color: var(--text-mute); margin-top: 1px; letter-spacing: 0.4px; }

  .an-body {
    position: relative; z-index: 1;
    display: grid;
    grid-template-columns: minmax(0, 1.9fr) minmax(200px, 1fr);
    gap: 14px;
    align-items: start;
  }

  .an-main { display: flex; flex-direction: column; gap: 14px; min-width: 0; }
  .an-side { display: flex; flex-direction: column; gap: 12px; }

  /* ═══════════ HERO ═══════════ */
  .an-hero {
    position: relative;
    display: flex; flex-direction: column; align-items: flex-start;
    gap: 2px;
    padding: 18px 20px 16px;
    border-radius: 16px;
    background: linear-gradient(160deg, color-mix(in srgb, var(--bg-card) 92%, transparent), color-mix(in srgb, var(--bg-app) 60%, transparent));
    border: 1px solid var(--border);
    overflow: hidden;
  }
  .an-hero-glow {
    position: absolute; top: -50%; right: -8%;
    width: 220px; height: 220px; border-radius: 50%;
    background: radial-gradient(circle, rgba(var(--orange-rgb), 0.16), transparent 65%);
    pointer-events: none;
  }
  .an-hero-label {
    display: inline-flex; align-items: center; gap: 7px;
    font-size: 10.5px; font-weight: 800; letter-spacing: 2.5px; text-transform: uppercase;
    color: var(--orange);
  }
  .an-hero-value {
    font-family: var(--font-mono);
    font-size: 40px; font-weight: 800; letter-spacing: -1.5px;
    color: var(--text); line-height: 1.1;
    background: linear-gradient(90deg, var(--text), color-mix(in srgb, var(--text) 72%, var(--orange)));
    -webkit-background-clip: text; background-clip: text;
    -webkit-text-fill-color: transparent;
  }
  .an-hero-caption {
    font-size: 10px; font-weight: 700; letter-spacing: 2px; text-transform: uppercase;
    color: var(--text-dim);
  }
  .an-hero-empty {
    display: inline-flex; align-items: center; gap: 7px;
    margin-top: 10px;
    padding: 7px 12px;
    border-radius: 999px;
    font-size: 11.5px; color: var(--text-mute);
    background: color-mix(in srgb, var(--text) 4%, transparent);
    border: 1px solid var(--border);
  }
  .an-hero-empty i { color: var(--text-dim); font-size: 13px; }

  /* ═══════════ CHART ═══════════ */
  .an-chart-card {
    padding: 14px 16px 12px;
    border-radius: 16px;
    background: linear-gradient(180deg, color-mix(in srgb, var(--bg-card) 88%, transparent), color-mix(in srgb, var(--bg-app) 55%, transparent));
    border: 1px solid var(--border);
  }
  .an-chart-head { display: flex; align-items: center; justify-content: flex-start; margin-bottom: 12px; }
  .an-chart-tabs {
    display: inline-flex; gap: 4px; padding: 4px;
    background: color-mix(in srgb, var(--text) 4%, transparent);
    border: 1px solid var(--border);
    border-radius: 11px;
  }
  .an-chart-tab {
    padding: 6px 14px;
    border: none; border-radius: 8px;
    background: transparent;
    color: var(--text-mute);
    font-family: inherit; font-size: 11.5px; font-weight: 700; letter-spacing: 0.2px;
    cursor: pointer;
    transition: background 0.2s, color 0.2s, box-shadow 0.2s;
  }
  .an-chart-tab:hover { color: var(--text); }
  .an-chart-tab.active {
    background: linear-gradient(135deg, var(--orange), var(--orange-2));
    color: #fff;
    box-shadow: 0 5px 16px rgba(var(--orange-rgb), 0.35);
  }

  .an-chart {
    display: flex; align-items: flex-end; justify-content: space-between;
    gap: clamp(4px, 1vw, 12px);
    height: 128px;
    padding: 6px 2px 0;
  }
  .an-chart-empty {
    flex: 1; height: 100%;
    display: flex; align-items: center; justify-content: center;
    color: var(--text-dim); font-size: 26px;
  }
  .an-col {
    flex: 1; min-width: 0;
    display: flex; flex-direction: column; align-items: center; gap: 6px;
    height: 100%;
  }
  .an-col-track {
    position: relative;
    flex: 1; width: 100%;
    display: flex; align-items: flex-end; justify-content: center;
    border-radius: 10px;
    background:
      linear-gradient(180deg, color-mix(in srgb, var(--text) 2.5%, transparent), color-mix(in srgb, var(--text) 1.2%, transparent));
  }
  .an-col-bar {
    width: min(22px, 62%);
    min-height: 4px;
    border-radius: 7px 7px 4px 4px;
    background: linear-gradient(180deg, var(--orange-2), var(--orange));
    box-shadow: 0 -4px 14px rgba(var(--orange-rgb), 0.35);
    animation: an-rise 0.5s cubic-bezier(.22, 1.2, .36, 1) both;
    transition: filter 0.2s, box-shadow 0.2s;
  }
  .an-col:hover .an-col-bar {
    filter: brightness(1.15);
    box-shadow: 0 -6px 20px rgba(var(--orange-rgb), 0.5);
  }
  @keyframes an-rise { from { height: 0 !important; opacity: 0.4; } }
  .an-col-max {
    position: absolute; top: 2px;
    font-size: 11px; color: var(--yellow);
    filter: drop-shadow(0 0 6px rgba(255, 209, 102, 0.5));
  }
  .an-col-label {
    font-size: 9.5px; font-weight: 700; letter-spacing: 0.3px;
    color: var(--text-mute);
    white-space: nowrap;
  }
  .an-col-label.dim { color: var(--text-dim); }
  .an-chart-note {
    margin-top: 8px;
    text-align: center;
    font-size: 10.5px; color: var(--text-dim);
  }

  /* ═══════════ TOP INSTANCES ═══════════ */
  .an-top {
    padding: 14px 16px 12px;
    border-radius: 16px;
    background: linear-gradient(180deg, color-mix(in srgb, var(--bg-card) 88%, transparent), color-mix(in srgb, var(--bg-app) 55%, transparent));
    border: 1px solid var(--border);
  }
  .an-top-head { display: flex; align-items: baseline; gap: 10px; margin-bottom: 12px; }
  .an-top-title {
    display: inline-flex; align-items: center; gap: 8px;
    font-size: 12.5px; font-weight: 800; color: var(--text);
  }
  .an-top-title i { color: var(--yellow); font-size: 14px; }
  .an-top-sub { font-size: 10.5px; color: var(--text-dim); }

  .an-top-list { display: flex; flex-direction: column; gap: 8px; }
  .an-top-row {
    display: flex; align-items: center; gap: 12px;
    padding: 10px 12px;
    border-radius: 13px;
    background: color-mix(in srgb, var(--text) 2.5%, transparent);
    border: 1px solid var(--border);
    transition: transform 0.18s cubic-bezier(.22, 1.2, .36, 1), border-color 0.18s, background 0.18s;
  }
  .an-top-row:hover {
    transform: translateY(-1px);
    border-color: color-mix(in srgb, var(--pcol) 45%, var(--border));
    background: color-mix(in srgb, var(--pcol) 6%, transparent);
  }
  .an-top-rank {
    width: 22px; height: 22px; flex-shrink: 0;
    display: flex; align-items: center; justify-content: center;
    border-radius: 7px;
    font-family: var(--font-mono); font-size: 11px; font-weight: 800;
    color: var(--text-mute);
    background: color-mix(in srgb, var(--text) 5%, transparent);
  }
  .an-top-row:nth-child(1) .an-top-rank {
    color: #b8860b;
    background: rgba(255, 209, 102, 0.12);
  }
  .an-top-row:nth-child(2) .an-top-rank { color: #9aa3b2; background: color-mix(in srgb, var(--text) 7%, transparent); }
  .an-top-row:nth-child(3) .an-top-rank { color: #c98a4b; background: rgba(201, 138, 75, 0.12); }

  .an-top-ic {
    width: 38px; height: 38px; flex-shrink: 0;
    display: flex; align-items: center; justify-content: center;
    border-radius: 10px;
    background: color-mix(in srgb, var(--pcol) 12%, transparent);
    border: 1px solid color-mix(in srgb, var(--pcol) 28%, transparent);
    overflow: hidden;
  }
  .an-top-ic img { width: 100%; height: 100%; object-fit: cover; }
  .an-top-ic i { font-size: 17px; }

  .an-top-info { flex: 1; min-width: 0; }
  .an-top-name {
    font-size: 12.5px; font-weight: 700; color: var(--text);
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .an-top-meta {
    display: flex; align-items: center; gap: 8px;
    font-size: 10px; color: var(--text-dim); margin-top: 2px;
  }
  .an-top-sessions {
    padding: 1px 7px;
    border-radius: 999px;
    background: color-mix(in srgb, var(--text) 5%, transparent);
    color: var(--text-mute);
    font-size: 9px; font-weight: 700;
  }
  .an-top-track {
    height: 3px; margin-top: 6px;
    border-radius: 2px;
    background: color-mix(in srgb, var(--text) 6%, transparent);
    overflow: hidden;
  }
  .an-top-fill {
    height: 100%;
    border-radius: 2px;
    background: linear-gradient(90deg, var(--pcol), color-mix(in srgb, var(--pcol) 55%, var(--orange)));
    box-shadow: 0 0 8px color-mix(in srgb, var(--pcol) 50%, transparent);
    transition: width 0.5s cubic-bezier(.22, 1.2, .36, 1);
  }
  .an-top-time {
    flex-shrink: 0;
    font-family: var(--font-mono);
    font-size: 11px; font-weight: 700; color: var(--text-mute);
  }
  .an-top-empty {
    display: flex; align-items: center; gap: 10px;
    justify-content: center;
    padding: 16px;
    font-size: 11.5px; color: var(--text-dim);
  }
  .an-top-empty i { font-size: 18px; opacity: 0.6; }

  /* ═══════════ TILES ═══════════ */
  .an-tiles {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 10px;
  }
  .an-tile {
    display: flex; flex-direction: column; gap: 3px;
    padding: 14px;
    border-radius: 14px;
    background: linear-gradient(170deg, color-mix(in srgb, var(--bg-card) 90%, transparent), color-mix(in srgb, var(--bg-app) 55%, transparent));
    border: 1px solid var(--border);
    transition: transform 0.18s cubic-bezier(.22, 1.2, .36, 1), border-color 0.18s, box-shadow 0.18s;
  }
  .an-tile:hover { transform: translateY(-2px); border-color: var(--border-hi); }
  .an-tile-ic {
    width: 30px; height: 30px;
    display: flex; align-items: center; justify-content: center;
    border-radius: 9px;
    font-size: 14px; color: var(--tile-accent);
    background: color-mix(in srgb, var(--tile-accent) 12%, transparent);
    margin-bottom: 4px;
  }
  .an-tile-orange { --tile-accent: var(--orange); }
  .an-tile-violet { --tile-accent: #9d7cff; }
  .an-tile-green  { --tile-accent: var(--green); }
  .an-tile-blue   { --tile-accent: var(--blue); }
  .an-tile-value {
    font-family: var(--font-mono);
    font-size: 19px; font-weight: 800; letter-spacing: -0.5px;
    color: var(--text);
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .an-tile-label {
    font-size: 9.5px; font-weight: 700; letter-spacing: 1px; text-transform: uppercase;
    color: var(--text-dim);
  }

  .an-fact {
    display: flex; align-items: center; gap: 9px;
    padding: 12px 14px;
    border-radius: 13px;
    font-size: 11px; line-height: 1.5; color: var(--text-mute);
    background: color-mix(in srgb, var(--text) 3%, transparent);
    border: 1px solid var(--border);
  }
  .an-fact i { color: var(--blue); font-size: 14px; flex-shrink: 0; }

  /* ═══════════ Responsive ═══════════ */
  @media (max-width: 1180px) {
    .an-body { grid-template-columns: 1fr; }
  }
  @media (max-width: 760px) {
    .an-tiles { grid-template-columns: 1fr; }
  }
</style>
