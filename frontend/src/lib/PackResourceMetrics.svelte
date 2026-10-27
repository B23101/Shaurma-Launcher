<script lang="ts">
  // ── Метрика ресурсів гри ────────────────────────────────────────────────
  // CPU і RAM усього дерева процесів гри (Java + дочірні), піки, середні,
  // розмір теки збірки. Поки гра йде — оновлюється живцем; після виходу
  // лишається знімок останньої сесії.
  import { onMount } from 'svelte'
  import { t } from './i18n.svelte'
  import { App } from './wails'
  import type { PackMetrics, PackMetricSample } from './types'

  let { packId } = $props<{ packId: string }>()

  let m = $state<PackMetrics | null>(null)
  let loaded = $state(false)

  async function poll() {
    try {
      m = await App.GetPackMetrics(packId)
    } catch {
      /* метрика — допоміжна, помилку мовчки ігноруємо */
    } finally {
      loaded = true
    }
  }

  onMount(() => {
    poll()
    const timer = setInterval(poll, 2000)
    return () => clearInterval(timer)
  })

  const samples = $derived<PackMetricSample[]>(m?.samples ?? [])
  const has = $derived(samples.length > 0)

  // Полілінія для SVG 100×32: значення нормуються на [0, max].
  function line(values: number[], max: number): string {
    if (values.length < 2 || max <= 0) return ''
    const step = 100 / (values.length - 1)
    return values.map((v, i) => `${(i * step).toFixed(2)},${(32 - Math.min(1, v / max) * 30 - 1).toFixed(2)}`).join(' ')
  }
  const cpuLine = $derived(line(samples.map((s) => s.cpu), 100))
  const ramMax = $derived(Math.max(1, ...samples.map((s) => s.ramMb)) * 1.1)
  const ramLine = $derived(line(samples.map((s) => s.ramMb), ramMax))

  const fmtMb = (v: number) => (v >= 1024 ? `${(v / 1024).toFixed(2)} ГБ` : `${Math.round(v)} МБ`)
  const fmtDur = (ms: number) => {
    const s = Math.max(0, Math.round(ms / 1000))
    const h = Math.floor(s / 3600), mi = Math.floor((s % 3600) / 60)
    return h ? `${h} ${t('metrics.h')} ${mi} ${t('metrics.min')}` : `${mi} ${t('metrics.min')} ${s % 60} ${t('metrics.sec')}`
  }
  const duration = $derived(m && m.startedAt ? fmtDur((m.running ? Date.now() : m.endedAt || Date.now()) - m.startedAt) : '')
</script>

<div class="card">
  <div class="card-head">
    <div class="ci green"><i class="ti ti-activity"></i></div>
    <div class="ctw">
      <div class="ct">{t('metrics.title')}</div>
      <div class="cd">{m?.running ? t('metrics.live') : has ? t('metrics.last') : t('metrics.hint')}</div>
    </div>
  </div>

  {#if !loaded}
    <div class="row"><span class="rl-sub"><i class="ti ti-loader spin"></i></span></div>
  {:else if !has}
    <div class="row col"><div class="rl-sub">{t('metrics.empty')}</div></div>
  {:else if m}
    <div class="pm-grid">
      <div class="pm-cell">
        <div class="pm-head"><span>{t('metrics.cpu')}</span><b class="mono text-orange">{m.curCpu.toFixed(1)}%</b></div>
        <svg viewBox="0 0 100 32" preserveAspectRatio="none" class="pm-spark" aria-label={t('metrics.cpu')}>
          <polyline points={cpuLine} fill="none" stroke="var(--orange)" stroke-width="1.5" vector-effect="non-scaling-stroke" />
        </svg>
        <div class="pm-sub">{t('metrics.peak')}: {m.peakCpu.toFixed(1)}% · {t('metrics.avg')}: {m.avgCpu.toFixed(1)}%</div>
      </div>
      <div class="pm-cell">
        <div class="pm-head"><span>{t('metrics.ram')}</span><b class="mono text-orange">{fmtMb(m.curRamMb)}</b></div>
        <svg viewBox="0 0 100 32" preserveAspectRatio="none" class="pm-spark" aria-label={t('metrics.ram')}>
          <polyline points={ramLine} fill="none" stroke="var(--violet-l)" stroke-width="1.5" vector-effect="non-scaling-stroke" />
        </svg>
        <div class="pm-sub">{t('metrics.peak')}: {fmtMb(m.peakRamMb)} · {t('metrics.avg')}: {fmtMb(m.avgRamMb)}</div>
      </div>
    </div>
    <div class="row">
      <div class="row-label"><div class="rl-name">{t('metrics.disk')}</div><div class="rl-sub">{t('metrics.diskHint')}</div></div>
      <span class="mono">{fmtMb(m.diskMb)}</span>
    </div>
    <div class="row" style="border-bottom:none">
      <div class="row-label"><div class="rl-name">{t('metrics.duration')}</div></div>
      <span class="mono">{duration}</span>
    </div>
  {/if}
</div>

<style>
  .pm-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 14px; padding: 14px 22px 4px; }
  .pm-cell { display: flex; flex-direction: column; gap: 6px; min-width: 0; }
  .pm-head { display: flex; justify-content: space-between; font-size: 12.5px; }
  .pm-spark { width: 100%; height: 56px; border-radius: 8px; background: rgba(255,255,255,.04); border: 1px solid var(--border); }
  .pm-sub { font-size: 11px; color: var(--text-mute); }
  @media (max-width: 620px) { .pm-grid { grid-template-columns: 1fr; } }
</style>
