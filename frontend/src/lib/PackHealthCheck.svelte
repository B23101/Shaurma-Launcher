<script lang="ts">
  import { tip as launcherTip } from './friends/tooltip'

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

  // ── Пам'ять прямо у вікні: повзунок + «Застосувати» ────────────────────
  let ramDraft = $state(0)
  let ramSaving = $state(false)
  const ramItem = $derived(report?.items.find((i: HealthCheckItem) => i.id === 'ram' && i.fixActionId === 'set_ram') ?? null)
  $effect(() => { if (ramItem) ramDraft = ramItem.suggestedMb || ramItem.allocatedMb || 4096 })
  const ramMax = $derived(Math.max(4096, Math.floor(((ramItem?.totalMb || 16384) - 1024) / 512) * 512))
  const ramZone = $derived(!ramItem ? 'ok'
    : ramItem.availableMb && ramDraft > ramItem.availableMb ? 'bad'
    : ramItem.recommendedMb && ramDraft < ramItem.recommendedMb ? 'warn' : 'ok')
  const gb = (mb: number) => (mb / 1024).toFixed(mb % 1024 ? 1 : 0)
  async function applyRam() {
    if (ramSaving) return
    ramSaving = true
    try {
      await App.SetPackRAM(packId, ramDraft)
      toast(t('health.ram.saved', { v: gb(ramDraft) }), 'success')
      await runCheck()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally { ramSaving = false }
  }

  // Пункти «усе гаразд» не займають півекрана: показуємо їх компактним рядком.
  const problems = $derived(report ? report.items.filter((i: HealthCheckItem) => i.severity !== 'ok') : [])
  const okItems = $derived(report ? report.items.filter((i: HealthCheckItem) => i.severity === 'ok') : [])

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
    : 'var(--yellow)'
  )
  const overallIcon = $derived(
    !report ? 'ti-loader spin'
    : report.overall === 'ok'      ? 'ti-shield-check'
    : report.overall === 'warning' ? 'ti-shield-half'
    : 'ti-shield-exclamation'
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
    <button class="icon-btn hc-refresh" onclick={runCheck} disabled={loading} use:launcherTip={t('health.recheck')}>
      <i class="ti {loading ? 'ti-loader spin' : 'ti-refresh'}"></i>
    </button>
  </div>

  <!-- ── Помилка завантаження ── -->
  {#if err}
    <div class="hc-err">
      <i class="ti ti-alert-triangle"></i> {err}
    </div>
  {/if}

  <!-- ── Що варто поправити ── -->
  {#if report}
    {#if problems.length > 0}
      <div class="hc-list">
        {#each problems as item, i (item.id)}
          <div class="hc-item {sevClass(item.severity)}" style="--i:{i}">
            <div class="hc-sev-ic"><i class="ti {item.id === 'ram' ? 'ti-cpu' : item.id === 'disk' ? 'ti-database' : item.id === 'duplicates' ? 'ti-copy' : sevIcon(item.severity)}"></i></div>
            <div class="hc-item-body">
              <div class="hc-item-title">{item.title}</div>
              {#if item.detail}<div class="hc-item-detail">{item.detail}</div>{/if}

              {#if item.fixActionId === 'set_ram' && ramItem}
                <!-- Повзунок пам'яті: зелена зона — безпечно, жовта — нижче
                     рекомендованої, червона — більше, ніж зараз вільно. -->
                <div class="hc-ram {ramZone}">
                  <div class="hc-ram-top">
                    <b>{gb(ramDraft)} ГБ</b>
                    <span class="hc-ram-meta">
                      {#if ramItem.availableMb}<span><i class="ti ti-circle-filled"></i>{t('health.ram.free', { v: gb(ramItem.availableMb) })}</span>{/if}
                      {#if ramItem.recommendedMb}<span><i class="ti ti-star"></i>{t('health.ram.rec', { v: gb(ramItem.recommendedMb) })}</span>{/if}
                    </span>
                  </div>
                  <input type="range" min="2048" max={ramMax} step="512" bind:value={ramDraft} aria-label={t('health.ram.label')} />
                  <div class="hc-ram-acts">
                    {#if ramItem.suggestedMb && ramItem.suggestedMb !== ramDraft}
                      <button class="hc-chip" onclick={() => (ramDraft = ramItem!.suggestedMb!)}><i class="ti ti-wand"></i>{t('health.ram.suggest', { v: gb(ramItem.suggestedMb) })}</button>
                    {/if}
                    <button class="btn btn-primary btn-sm hc-ram-apply" disabled={ramSaving || ramDraft === ramItem.allocatedMb} onclick={applyRam}>
                      {#if ramSaving}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-check"></i>{/if} {t('health.ram.apply')}
                    </button>
                  </div>
                </div>
              {:else if item.fixable && item.fixActionId}
                <button class="btn btn-ghost btn-sm hc-fix-btn" disabled={!!fixing[item.fixActionId]} onclick={() => runFix(item)}>
                  {#if fixing[item.fixActionId]}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-wand"></i>{/if}
                  {fixLabel(item.fixActionId)}
                </button>
              {/if}
            </div>
          </div>
        {/each}
      </div>
    {/if}
    {#if okItems.length > 0}
      <div class="hc-oks">
        {#each okItems as item (item.id)}
          <span class="hc-ok" use:launcherTip={item.detail || item.title}><i class="ti ti-circle-check"></i>{item.title}</span>
        {/each}
      </div>
    {/if}
  {:else if loading}
    <div class="hc-list">
      {#each [1,2,3] as n (n)}
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
    <div class="hc-footer">
      {#if report && report.overall !== 'ok'}
        <span class="hc-foot-hint"><i class="ti ti-info-circle"></i>{t('health.footHint')}</span>
      {/if}
      <div class="hc-footer-right">
        {#if onClose}<button class="btn btn-ghost" onclick={onClose}>{t('close')}</button>{/if}
        {#if onLaunchAnyway}
          <button class="btn btn-primary" disabled={loading} onclick={onLaunchAnyway}>
            <i class="ti ti-player-play"></i>
            {report && report.overall !== 'ok' ? t('health.launchAnyway') : t('health.launch')}
          </button>
        {/if}
      </div>
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

  /* ── Пункти, які варто поправити ── */
  .hc-list { display: flex; flex-direction: column; gap: 8px; }
  .hc-item {
    --c: var(--orange);
    display: flex; align-items: flex-start; gap: 14px;
    padding: 14px 16px; border-radius: var(--r-lg);
    border: 1px solid color-mix(in srgb, var(--c) 28%, var(--border));
    background: linear-gradient(135deg, color-mix(in srgb, var(--c) 7%, var(--bg-panel)), var(--bg-panel) 60%);
    animation: hc-in .35s cubic-bezier(.16,1,.3,1) both; animation-delay: calc(var(--i, 0) * 60ms);
  }
  .hc-item.sev-err { --c: var(--yellow); }
  @keyframes hc-in { from { opacity: 0; transform: translateY(6px); } to { opacity: 1; transform: none; } }
  .hc-sev-ic {
    width: 36px; height: 36px; min-width: 36px; border-radius: 11px;
    display: flex; align-items: center; justify-content: center; font-size: 18px;
    color: var(--c); background: color-mix(in srgb, var(--c) 14%, transparent);
  }
  .hc-item-body  { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 4px; }
  .hc-item-title { font-size: 13.5px; font-weight: 700; color: var(--text); }
  .hc-item-detail { font-size: 12px; line-height: 1.5; color: var(--text-mute); }
  .hc-fix-btn { align-self: flex-start; margin-top: 6px; }

  /* Повзунок пам'яті */
  .hc-ram { --z: var(--green); margin-top: 8px; padding: 12px; border-radius: var(--r-md); background: var(--bg-input); border: 1px solid var(--border); display: flex; flex-direction: column; gap: 8px; }
  .hc-ram.warn { --z: var(--yellow); }
  .hc-ram.bad { --z: var(--red); }
  .hc-ram-top { display: flex; align-items: baseline; justify-content: space-between; gap: 10px; flex-wrap: wrap; }
  .hc-ram-top b { font: 800 20px/1 var(--font-mono); color: var(--z); transition: color .2s; font-variant-numeric: tabular-nums; }
  .hc-ram-meta { display: flex; gap: 12px; flex-wrap: wrap; font-size: 11.5px; color: var(--text-mute); }
  .hc-ram-meta span { display: inline-flex; align-items: center; gap: 5px; }
  .hc-ram-meta i { font-size: 9px; color: var(--green); }
  .hc-ram-meta .ti-star { font-size: 12px; color: var(--yellow); }
  .hc-ram input[type=range] { width: 100%; }
  .hc-ram-acts { display: flex; align-items: center; justify-content: flex-end; gap: 8px; flex-wrap: wrap; }
  .hc-chip { display: inline-flex; align-items: center; gap: 6px; height: 30px; padding: 0 11px; border-radius: 999px; border: 1px dashed rgba(var(--orange-rgb), .5); background: rgba(var(--orange-rgb), .08); color: var(--orange); font: 700 12px var(--font-ui); cursor: pointer; margin-right: auto; }
  .hc-chip:hover { background: rgba(var(--orange-rgb), .15); }

  /* Перевірено й гаразд — компактні мітки */
  .hc-oks { display: flex; flex-wrap: wrap; gap: 6px; }
  .hc-ok { display: inline-flex; align-items: center; gap: 6px; max-width: 100%; padding: 5px 10px; border-radius: 999px; font-size: 11.5px; color: var(--text-mute); background: rgba(16,185,129,.08); border: 1px solid rgba(16,185,129,.22); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .hc-ok i { color: var(--green); font-size: 13px; flex: none; }

  /* ── Скелетон ── */
  .hc-skeleton { opacity: .45; pointer-events: none; border-color: var(--border); background: var(--bg-panel); }
  .skeleton-ic { width: 36px; height: 36px; border-radius: 11px; background: var(--bg-input); animation: pulse 1.4s ease-in-out infinite; }
  .skeleton-line { height: 10px; border-radius: 5px; background: var(--bg-input); margin-bottom: 6px; animation: pulse 1.4s ease-in-out infinite; }
  .skeleton-line:last-child { margin-bottom: 0; }
  .w70 { width: 70%; }
  .w45 { width: 45%; animation-delay: .15s; }
  @keyframes pulse { 0%,100% { opacity: .6; } 50% { opacity: 1; } }

  /* ── Футер (модальний режим) ── */
  .hc-footer { display: flex; align-items: center; justify-content: space-between; gap: 12px; flex-wrap: wrap; padding-top: 4px; }
  .hc-foot-hint { display: flex; align-items: center; gap: 6px; font-size: 11.5px; color: var(--text-mute); flex: 1 1 200px; }
  .hc-foot-hint i { color: var(--text-dim); font-size: 14px; }
  .hc-footer-right { display: flex; align-items: center; gap: 8px; margin-left: auto; }
</style>
