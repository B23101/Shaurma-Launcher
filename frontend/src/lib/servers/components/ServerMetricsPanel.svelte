<script lang="ts">
  // ── Метрики CPU/RAM/гравці/uptime у банері сервера ───────────────────
  // Живі бари як на сторінці збірки. При перевантаженні (CPU > 85% або
  // RAM > 95%) бари блимають червоним (.over) — це той самий візуал, що
  // був у ServerInstancePage, тепер винесено в окремий компонент.
  import type { ServerLiveStats, ServerInstallProgress } from '../../types'
  import { formatUptime } from '../utils'
  import { t } from '../../i18n.svelte'

  let {
    running = false,
    installing = false,
    installProgress = null,
    stats = null,
    allocRAM = 0,
    uptimeSecFallback = 0,
    velocity = false,
    lastError = '',
  }: {
    running?: boolean
    installing?: boolean
    installProgress?: ServerInstallProgress | null
    stats?: ServerLiveStats | null
    allocRAM?: number
    uptimeSecFallback?: number
    velocity?: boolean
    lastError?: string
  } = $props()

  // usedRAM — скільки сервер РЕАЛЬНО використовує. Раніше тут завжди був
  // stats.ramMB (RSS процесу — скільки ОС виділила/закомітила java.exe),
  // що systематично завищувало «використання»: RSS включає резерв JVM,
  // метадані, off-heap буфери — сервер з легким навантаженням і -Xmx 4 ГБ
  // показував «3.8/4 ГБ використано» просто тому, що JVM заздалегідь
  // закомітила пам'ять під -Xms/-Xmx, а не тому, що вона зайнята об'єктами.
  // Тепер беремо heapUsedMB — реальний розмір живої купи з GC-логу самої
  // JVM (backend/internal/servers/stats.go, HeapSource="jvm"). Fallback на
  // RSS лишається ЛИШЕ для перших секунд після старту (heapSource==="rss":
  // GC-лог ще порожній, жодного збирання не було) — інакше панель була б
  // порожньою одразу після запуску сервера.
  const usingHeap = $derived(stats?.heapSource === 'jvm')
  const usedRAM = $derived(usingHeap ? (stats?.heapUsedMB ?? 0) : (stats?.ramMB ?? 0))
  // ramPct має сенс ЛИШЕ коли відома сама КУПА (heapSource==='jvm').
  // Раніше тут ділився RSS процесу на -Xmx і в стані «GC ще не було»
  // (heapSource==='rss', тобто перші хвилини після старту сервера, поки JVM
  // не зробила жодного збирання) це давало стабільні 100 %: RSS включає
  // метадані, off-heap буфери, JIT-код і стеки потоків, тож він майже
  // завжди більший за -Xmx, хоч купа могла бути зайнята на 10 %. Тепер у
  // цьому стані показуємо чесний RSS і індетерміновану смугу замість
  // фальшивих «4.0 / 4.0 ГБ, 100 %».
  const ramPct = $derived(usingHeap && allocRAM > 0 ? Math.min(100, Math.round((usedRAM / allocRAM) * 100)) : 0)
  const ramOver = $derived(usingHeap && allocRAM > 0 && usedRAM >= allocRAM * 0.95)
  const cpuPct = $derived(Math.min(100, Math.round(stats?.cpuPercent ?? 0)))
</script>

<div class="smp">
  {#if installing && installProgress}
    <div class="sm sm-wide">
      <div class="sm-head">
        <span class="sm-label"><i class="ti ti-download"></i> {installProgress.stage === 'java' ? 'Java' : t('servers.metrics.core')}</span>
        <span class="sm-val">{installProgress.message}</span>
      </div>
      <div class="sm-bar">
        {#if installProgress.percent >= 0}
          <div class="sm-bar-fill" style="width:{installProgress.percent}%"></div>
        {:else}
          <div class="sm-bar-fill sm-bar-indet"></div>
        {/if}
      </div>
    </div>
  {:else if running && stats}
    <div class="sm" class:over={cpuPct > 85}>
      <div class="sm-head">
        <span class="sm-label"><i class="ti ti-cpu"></i> CPU</span>
        <span class="sm-val">{cpuPct}%</span>
      </div>
      <div class="sm-bar"><div class="sm-bar-fill" style="width:{cpuPct}%"></div></div>
      {#if cpuPct > 85}<div class="sm-warn"><i class="ti ti-alert-triangle"></i> {t('servers.metrics.overloaded')}</div>{/if}
    </div>

    <div class="sm" class:over={ramOver}>
      <div class="sm-head">
        <span class="sm-label"><i class="ti ti-device-desktop"></i> RAM{#if !usingHeap} · RSS{/if}</span>
        <span class="sm-val">
          {#if usingHeap}
            {t('servers.metrics.ramHeap', { used: (usedRAM / 1024).toFixed(1), total: (allocRAM / 1024).toFixed(1) })}
          {:else}
            {t('servers.metrics.ramAlloc', { used: (usedRAM / 1024).toFixed(1), total: (allocRAM / 1024).toFixed(1) })}
          {/if}
        </span>
      </div>
      <div class="sm-bar">
        {#if usingHeap}
          <div class="sm-bar-fill" style="width:{ramPct}%"></div>
        {:else}
          <div class="sm-bar-fill sm-bar-indet"></div>
        {/if}
      </div>
      {#if ramOver}<div class="sm-warn"><i class="ti ti-alert-triangle"></i> {t('servers.metrics.heapAlmostFull')}</div>{/if}
      {#if !usingHeap}<div class="sm-warn dim"><i class="ti ti-info-circle"></i> {t('servers.metrics.heapPending')}</div>{/if}
    </div>

    <div class="sm">
      <div class="sm-head">
        <span class="sm-label"><i class="ti ti-users"></i> {t('servers.metrics.players')}</span>
        <span class="sm-val">{stats.playerCount ?? 0}/{stats.maxPlayers ?? 20}</span>
      </div>
      <div class="sm-bar">
        <div class="sm-bar-fill sm-bar-players" style="width:{Math.min(100, Math.round(((stats.playerCount ?? 0) / Math.max(1, stats.maxPlayers || 20)) * 100))}%"></div>
      </div>
    </div>

    <div class="sm sm-uptime">
      <div class="sm-head">
        <span class="sm-label"><i class="ti ti-clock"></i> Uptime</span>
        <span class="sm-val sm-uptime-val">{formatUptime(stats?.uptimeSec || uptimeSecFallback || 0)}</span>
      </div>
    </div>
  {:else}
    <div class="sm sm-idle">
      <i class="ti {velocity ? 'ti-router' : 'ti-info-circle'}"></i>
      {#if lastError}
        <span>{lastError}</span>
      {:else}
        <span>{velocity ? t('servers.metrics.idleVelocity') : t('servers.metrics.idle')}</span>
      {/if}
    </div>
  {/if}
</div>

<style>
  .smp {
    position: relative; z-index: 1;
    display: flex; gap: 8px; flex-wrap: wrap; align-items: stretch; margin-top: 10px;
  }
  .sm {
    display: flex; flex-direction: column; justify-content: center; gap: 5px;
    min-width: 150px; padding: 7px 11px;
    background: rgba(0, 0, 0, .38); border: 1px solid rgba(255, 255, 255, .13);
    border-radius: 10px; backdrop-filter: blur(4px);
    transition: border-color .2s, box-shadow .2s;
  }
  .sm-wide { flex: 1; min-width: 260px; }
  .sm-head { display: flex; align-items: center; gap: 7px; }
  .sm-label {
    display: inline-flex; align-items: center; gap: 5px;
    font: 700 10px/1 var(--font-ui); letter-spacing: .4px; text-transform: uppercase;
    color: rgba(255, 255, 255, .85);
  }
  .sm-label i { font-size: 12px; color: var(--pcol, var(--orange)); }
  .sm-val {
    margin-left: auto; font: 600 11px/1 var(--font-mono); color: #fff;
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .sm-bar { width: 100%; height: 5px; border-radius: 3px; background: rgba(255, 255, 255, .14); overflow: hidden; }
  .sm-bar-fill {
    height: 100%; border-radius: 3px;
    background: linear-gradient(90deg, var(--pcol, var(--orange)), color-mix(in srgb, var(--pcol, var(--orange)) 60%, #fff));
    transition: width .4s cubic-bezier(.22, 1, .36, 1);
  }
  .sm-bar-players { background: linear-gradient(90deg, #22c55e, #86efac); }
  .sm-bar-indet { width: 40%; animation: sm-slide 1.6s ease-in-out infinite; }
  @keyframes sm-slide { 0% { transform: translateX(-110%); } 100% { transform: translateX(370%); } }
  .sm-uptime { justify-content: center; min-width: 118px; }
  .sm-uptime-val { font-size: 14px; color: var(--pcol, var(--orange)); font-weight: 700; }
  .sm-idle {
    flex: 1; flex-direction: row; align-items: center; gap: 8px;
    min-width: 260px; color: rgba(255, 255, 255, .8); font: 400 11px/1.35 var(--font-ui);
  }
  .sm-idle i { font-size: 15px; color: var(--pcol, var(--orange)); }
  .sm-warn {
    display: flex; align-items: center; gap: 4px;
    font: 700 9px/1 var(--font-ui); color: #ff8080;
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .sm-warn.dim { color: rgba(255, 255, 255, .55); font-weight: 600; }
  .sm.over {
    border-color: rgba(255, 70, 70, .75);
    box-shadow: 0 0 0 1px rgba(255, 70, 70, .35), 0 0 16px rgba(255, 60, 60, .35);
    animation: sm-over-pulse 1.1s ease-in-out infinite;
  }
  .sm.over .sm-bar-fill {
    background: linear-gradient(90deg, #ff4444, #ff6b3d);
    animation: sm-over-stripes .8s linear infinite;
  }
  @keyframes sm-over-pulse {
    0%, 100% { border-color: rgba(255, 70, 70, .45); box-shadow: 0 0 0 1px rgba(255, 70, 70, .15); }
    50% { border-color: rgba(255, 70, 70, .95); box-shadow: 0 0 0 1px rgba(255, 70, 70, .5), 0 0 20px rgba(255, 60, 60, .55); }
  }
  @keyframes sm-over-stripes {
    0% { filter: brightness(1); }
    50% { filter: brightness(1.45); }
    100% { filter: brightness(1); }
  }
  @media (prefers-reduced-motion: reduce) {
    .sm.over, .sm.over .sm-bar-fill, .sm-bar-indet { animation: none !important; }
  }
</style>
