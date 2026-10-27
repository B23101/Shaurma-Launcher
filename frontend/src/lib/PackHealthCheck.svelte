<script lang="ts">
  // ── Pack Doctor: перевірка здоров'я збірки ──────────────────────────────
  // Показує список перевірок (Java / диск / дублікати / RAM) з кнопками
  // авто-виправлення. Використовується:
  //  • як вкладка «Діагностика» на сторінці збірки (standalone);
  //  • як модалка перед запуском, якщо overall !== 'ok'.
  import { t } from './i18n.svelte'
  import { App } from './wails'
  import { toast } from './toast.svelte'
  import type { HealthCheckReport, HealthCheckItem, HealthCheckSeverity } from './types'

  let {
    packId,
    packName = '',
    // modal=true → компонент показується всередині UiModal, кнопка «Запустити все одно»
    modal = false,
    onLaunchAnyway = null as null | (() => void),
    onClose        = null as null | (() => void),
  } = $props<{
    packId: string
    packName?: string
    modal?: boolean
    onLaunchAnyway?: null | (() => void)
    onClose?:        null | (() => void)
  }>()

  let report   = $state<HealthCheckReport | null>(null)
  let loading  = $state(false)
  let fixing   = $state<Record<string, boolean>>({})   // fixActionId → true під час фіксу
  let err      = $state('')

  // ── Запустити перевірку ─────────────────────────────────────────────────
  async function runCheck() {
    loading = true
    err     = ''
    try {
      report = await App.RunPackHealthCheck(packId)
    } catch (e: any) {
      err = String(e)
    } finally {
      loading = false
    }
  }

  // ── Автофікс ────────────────────────────────────────────────────────────
  async function runFix(item: HealthCheckItem) {
    if (!item.fixActionId || fixing[item.fixActionId]) return
    fixing = { ...fixing, [item.fixActionId]: true }
    try {
      await App.RunHealthCheckFix(packId, item.fixActionId)
      toast(t('health.fixDone'), 'success')
      await runCheck()          // перечитуємо звіт після фіксу
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      fixing = { ...fixing, [item.fixActionId]: false }
    }
  }

  // ── Кнопка «Запустити все одно» ─────────────────────────────────────────
  // Завжди дозволена — навіть при помилках (RAM, тощо). Блокується
  // ТІЛЬКИ поки звіт не завантажено (report === null). Користувач
  // має право вирішувати: краш через RAM = його ризик, не наш бар'єр.
  const canLaunchAnyway = $derived(report !== null)

  // ── Іконка / клас по severity ───────────────────────────────────────────
  function sevIcon(s: HealthCheckSeverity) {
    if (s === 'ok')      return 'ti-circle-check'
    if (s === 'warning') return 'ti-alert-triangle'
    return 'ti-circle-x'
  }
  function sevClass(s: HealthCheckSeverity) {
    if (s === 'ok')      return 'sev-ok'
    if (s === 'warning') return 'sev-warn'
    return 'sev-err'
  }

  // Заголовок кнопки фіксу за actionId
  function fixLabel(id: string) {
    const map: Record<string, string> = {
      install_java:            t('health.fix.installJava'),
      cleanup_storage:         t('health.fix.cleanupStorage'),
      remove_duplicate_mods:   t('health.fix.removeDuplicates'),
    }
    return map[id] ?? t('health.fix.auto')
  }

  // ── Автозапуск при першому рендері ──────────────────────────────────────
  $effect(() => {
    if (packId) runCheck()
  })

  // ── Колір загального статусу ─────────────────────────────────────────────
  const overallColor = $derived(
    !report ? 'var(--text-mute)'
    : report.overall === 'ok'      ? 'var(--green)'
    : report.overall === 'warning' ? 'var(--orange)'
    : 'var(--red)'
  )
  const overallIcon = $derived(
    !report ? 'ti-loader spin'
    : report.overall === 'ok'      ? 'ti-shield-check'
    : report.overall === 'warning' ? 'ti-shield-exclamation'
    : 'ti-shield-x'
  )
</script>

<div class="hc" class:hc-modal={modal}>

  <!-- ── Шапка з загальним статусом ── -->
  <div class="hc-header">
    <div class="hc-shield" style="--sc:{overallColor}">
      <i class="ti {overallIcon}"></i>
    </div>
    <div class="hc-header-text">
      <div class="hc-title">
        {#if loading}
          {t('health.checking')}
        {:else if !report || report.overall === 'ok'}
          {t('health.allGood')}
        {:else if report.overall === 'warning'}
          {t('health.hasWarnings')}
        {:else}
          {t('health.hasErrors')}
        {/if}
      </div>
      {#if packName}
        <div class="hc-subtitle">{packName}</div>
      {/if}
    </div>
    <button class="icon-btn hc-refresh" onclick={runCheck} disabled={loading} title={t('health.recheck')}>
      <i class="ti {loading ? 'ti-loader spin' : 'ti-refresh'}"></i>
    </button>
  </div>

  <!-- ── Помилка завантаження ── -->
  {#if err}
    <div class="hc-err">
      <i class="ti ti-alert-triangle"></i> {err}
    </div>
  {/if}

  <!-- ── Список перевірок ── -->
  {#if report}
    <div class="hc-list">
      {#each report.items as item (item.id)}
        <div class="hc-item {sevClass(item.severity)} rise-in">
          <!-- Іконка статусу -->
          <div class="hc-sev-ic">
            <i class="ti {sevIcon(item.severity)}"></i>
          </div>

          <!-- Текст -->
          <div class="hc-item-body">
            <div class="hc-item-title">{item.title}</div>
            {#if item.detail}
              <div class="hc-item-detail">{item.detail}</div>
            {/if}
          </div>

          <!-- Кнопка авто-виправлення -->
          {#if item.fixable && item.fixActionId}
            <button
              class="btn btn-sm hc-fix-btn {item.severity === 'error' ? 'btn-danger-outline' : 'btn-ghost'}"
              disabled={!!fixing[item.fixActionId]}
              onclick={() => runFix(item)}
            >
              {#if fixing[item.fixActionId]}
                <i class="ti ti-loader spin"></i>
              {:else}
                <i class="ti ti-tool"></i>
              {/if}
              {fixLabel(item.fixActionId)}
            </button>
          {/if}
        </div>
      {/each}
    </div>
  {:else if loading}
    <!-- Скелетон під час завантаження -->
    <div class="hc-list">
      {#each [1,2,3,4,5] as n (n)}
        <div class="hc-item hc-skeleton">
          <div class="hc-sev-ic skeleton-ic"></div>
          <div class="hc-item-body">
            <div class="skeleton-line w70"></div>
            <div class="skeleton-line w45"></div>
          </div>
        </div>
      {/each}
    </div>
  {/if}

  <!-- ── Кнопки внизу (тільки в модальному режимі) ── -->
  {#if modal}
    <div class="hc-footer" class:hc-footer-warn={report && report.overall !== 'ok'}>
      {#if onClose}
        <button class="btn btn-ghost" onclick={onClose}>{t('close')}</button>
      {/if}
      {#if onLaunchAnyway}
        <div class="hc-footer-right">
          {#if report && report.overall === 'error'}
            <span class="hc-warn-hint"><i class="ti ti-alert-triangle"></i> Є критичні помилки — можливий краш</span>
          {/if}
          <button
            class="btn {report && report.overall === 'error' ? 'btn-danger' : 'btn-primary'}"
            disabled={loading}
            onclick={onLaunchAnyway}
          >
            <i class="ti ti-player-play"></i>
            {t('health.launchAnyway')}
          </button>
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .hc { display: flex; flex-direction: column; gap: 16px; }
  .hc-modal { /* нічого особливого — UiModal дає padding */ }

  /* ── Шапка ── */
  .hc-header {
    display: flex; align-items: center; gap: 14px;
    padding: 16px 18px; border-radius: var(--r-md);
    background: var(--bg-panel); border: 1px solid var(--border);
  }
  .hc-shield {
    width: 48px; height: 48px; min-width: 48px; border-radius: 14px;
    display: flex; align-items: center; justify-content: center; font-size: 22px;
    color: var(--sc);
    background: color-mix(in srgb, var(--sc) 14%, transparent);
    border: 1px solid color-mix(in srgb, var(--sc) 30%, transparent);
    box-shadow: 0 6px 18px -4px color-mix(in srgb, var(--sc) 35%, transparent);
    transition: all .25s ease;
  }
  .hc-header-text { flex: 1; min-width: 0; }
  .hc-title    { font-size: 15px; font-weight: 700; color: var(--text); }
  .hc-subtitle { font-size: 11.5px; color: var(--text-mute); margin-top: 2px; }
  .hc-refresh  {
    width: 32px; height: 32px; border-radius: 8px; background: transparent; border: none; cursor: pointer;
    display: flex; align-items: center; justify-content: center; color: var(--text-mute); font-size: 16px;
    transition: background .15s, color .15s;
  }
  .hc-refresh:hover  { background: rgba(255,255,255,.08); color: var(--text); }
  .hc-refresh:disabled { opacity: .5; cursor: default; }

  /* ── Помилка ── */
  .hc-err {
    padding: 10px 14px; border-radius: var(--r-sm);
    background: rgba(239,68,68,.08); border: 1px solid rgba(239,68,68,.25);
    color: var(--red); font-size: 12.5px; display: flex; align-items: center; gap: 8px;
  }

  /* ── Список перевірок ── */
  .hc-list { display: flex; flex-direction: column; gap: 8px; }

  .hc-item {
    display: flex; align-items: flex-start; gap: 14px;
    padding: 13px 16px; border-radius: var(--r-md);
    border: 1px solid var(--border); background: var(--bg-panel);
    transition: border-color .15s;
  }

  /* Кольорові акценти по severity */
  .hc-item.sev-ok   { border-left: 3px solid var(--green, #3ddc84); }
  .hc-item.sev-warn { border-left: 3px solid var(--orange, #ff8a00); }
  .hc-item.sev-err  {
    border-left: 3px solid var(--red, #ff5c5c);
    background: color-mix(in srgb, var(--red) 4%, var(--bg-panel));
  }

  /* Іконка статусу */
  .hc-sev-ic {
    width: 34px; height: 34px; min-width: 34px; border-radius: 9px;
    display: flex; align-items: center; justify-content: center; font-size: 17px;
    margin-top: 1px;
  }
  .sev-ok   .hc-sev-ic { background: rgba(61,220,132,.12); color: var(--green,  #3ddc84); }
  .sev-warn .hc-sev-ic { background: rgba(255,138, 0,.12); color: var(--orange, #ff8a00); }
  .sev-err  .hc-sev-ic { background: rgba(239, 68,68,.12); color: var(--red,    #ff5c5c); }

  /* Текст */
  .hc-item-body  { flex: 1; min-width: 0; }
  .hc-item-title { font-size: 13px; font-weight: 600; color: var(--text); }
  .hc-item-detail {
    font-size: 11.5px; color: var(--text-mute); margin-top: 3px;
    font-family: var(--font-mono); word-break: break-word;
  }

  /* Кнопка фіксу */
  .hc-fix-btn {
    flex-shrink: 0; align-self: center; display: inline-flex; align-items: center; gap: 5px;
    padding: 5px 11px; border-radius: 7px; font-size: 11.5px; font-weight: 600; cursor: pointer;
    transition: all .15s ease; border: 1px solid transparent; white-space: nowrap;
  }
  .hc-fix-btn.btn-ghost {
    background: rgba(255,255,255,.06); border-color: var(--border-hi);
    color: var(--text);
  }
  .hc-fix-btn.btn-ghost:hover { background: rgba(255,255,255,.12); }
  .hc-fix-btn.btn-danger-outline {
    background: rgba(239,68,68,.08); border-color: rgba(239,68,68,.35);
    color: var(--red, #ff5c5c);
  }
  .hc-fix-btn.btn-danger-outline:hover { background: rgba(239,68,68,.16); }
  .hc-fix-btn:disabled { opacity: .5; cursor: default; }

  /* ── Скелетон ── */
  .hc-skeleton { opacity: .45; pointer-events: none; }
  .skeleton-ic {
    width: 34px; height: 34px; border-radius: 9px;
    background: var(--bg-input); animation: pulse 1.4s ease-in-out infinite;
  }
  .skeleton-line {
    height: 10px; border-radius: 5px; background: var(--bg-input);
    margin-bottom: 6px; animation: pulse 1.4s ease-in-out infinite;
  }
  .skeleton-line:last-child { margin-bottom: 0; }
  .w70 { width: 70%; }
  .w45 { width: 45%; animation-delay: .15s; }

  @keyframes pulse {
    0%,100% { opacity: .6; }
    50%      { opacity: 1;  }
  }

  /* ── Футер (модальний режим) ── */
  .hc-footer {
    display: flex; align-items: center; justify-content: space-between; gap: 10px;
    padding: 12px 16px; border-radius: var(--r-md);
    border: 1px solid var(--border); background: var(--bg-panel);
    flex-wrap: wrap;
  }
  .hc-footer-warn {
    border-color: rgba(239,68,68,.3);
    background: color-mix(in srgb, var(--red) 5%, var(--bg-panel));
  }
  .hc-footer-right { display: flex; align-items: center; gap: 10px; margin-left: auto; }
  .hc-warn-hint {
    font-size: 11px; color: var(--red); display: flex; align-items: center; gap: 5px;
    font-weight: 600;
  }
  .btn-danger { background: var(--red); color: #fff; border: none; }
  .btn-danger:hover { background: color-mix(in srgb, var(--red) 85%, #fff); }
</style>
