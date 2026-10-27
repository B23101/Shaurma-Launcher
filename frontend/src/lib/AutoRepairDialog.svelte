<script lang="ts">
  // ── Авто-починка збірки ─────────────────────────────────────────────────
  // Показує проблеми, знайдені діагностикою (логи гри + crash-report + mods/),
  // і дає застосувати лагодження. Кожне лагодження описане кроками — нічого
  // не робиться «наосліп»; рекомендоване обране одразу, решта — за бажанням.
  // Відкривається: (1) подією repair:report після краху / помилки запуску,
  // (2) вручну («Діагностика» у «Продуктивності»).
  import { onMount, untrack } from 'svelte'
  import UiModal from './UiModal.svelte'
  import { t } from './i18n.svelte'
  import { App, on } from './wails'
  import { toast } from './toast.svelte'
  import type { RepairReport, RepairIssue, RepairFix, RepairFixResult, RepairProgressEvent } from './types'

  let {
    packId,
    packName = '',
    // Готовий звіт (з події). null → діалог сам запустить діагностику.
    initial = null as null | RepairReport,
    onclose = () => {},
    onconsole = null as null | (() => void),
    // Лагодження «увійти в Microsoft» — вхід живе в головному вікні.
    onlogin = null as null | (() => void)
  } = $props<{
    packId: string
    packName?: string
    initial?: null | RepairReport
    onclose?: () => void
    onconsole?: null | (() => void)
    onlogin?: null | (() => void)
  }>()

  let report = $state<RepairReport | null>(null)
  let loading = $state(false)
  let applying = $state(false)
  let err = $state('')
  let results = $state<RepairFixResult[] | null>(null)
  let selected = $state<Record<string, boolean>>({})
  let expanded = $state<Record<string, boolean>>({})
  let mutedKinds = $state<string[]>([])
  // Прогрес застосування: довгі кроки (repair_version / install_java /
  // update_loader) качають сотні мегабайт — без цього модалка виглядала
  // завислою на «Застосовую…» із заблокованою кнопкою «Закрити».
  let step = $state<RepairProgressEvent | null>(null)
  let applyLog = $state<string[]>([])
  let logBuf: string[] = []
  let logTimer: ReturnType<typeof setTimeout> | null = null
  let alive = true

  // Ключ лагодження: id проблеми + id лагодження (id унікальний лише в межах звіту).
  const fkey = (issue: RepairIssue, fix: RepairFix) => `${issue.id}|${fix.id}`

  function adopt(r: RepairReport) {
    report = { ...r, issues: r.issues ?? [], mutedIssues: r.mutedIssues ?? [] }
    const sel: Record<string, boolean> = {}
    for (const is of report.issues) {
      for (const f of is.fixes ?? []) sel[fkey(is, f)] = !!f.recommended
    }
    selected = sel
    results = null
  }

  async function diagnose() {
    loading = true
    err = ''
    try {
      adopt(await App.DiagnosePack(packId))
    } catch (e: any) {
      err = String(e?.message ?? e)
    } finally {
      loading = false
    }
  }

  // Залежності ефекту — ЛИШЕ packId та initial. Усе, що читають adopt()/
  // diagnose() (report, selected, loading…), виконується в untrack: інакше
  // ефект підписувався на `report`, і будь-яка його зміна (напр. «Не
  // показувати» → report = {...}) перезапускала adopt() → той знову писав
  // report → нескінченний цикл оновлень, UI вікна «замерзав».
  $effect(() => {
    const pid = packId
    const init = initial
    untrack(() => {
      void pid
      App.GetRepairMuted().then((m) => (mutedKinds = m ?? [])).catch(() => {})
      if (init) adopt(init)
      else diagnose()
    })
  })

  onMount(() => {
    const offs = [
      on('repair:progress', (e: RepairProgressEvent) => {
        if (e?.packId === packId) step = e
      }),
      // Під час лагодження installer:log показує, на чому саме ми стоїмо
      // (завантаження маніфесту, клієнтського jar, бібліотек, Java).
      // Подій installer:log під час качки сотні на секунду — без згладжування
      // кожна перемальовувала модалку й вона переставала реагувати на кліки.
      // Тому складаємо рядки в буфер і скидаємо в UI не частіше ~4 разів/с.
      on('installer:log', (msg: string) => {
        if (!applying) return
        logBuf.push(String(msg ?? ''))
        if (logBuf.length > 40) logBuf.splice(0, logBuf.length - 40)
        if (!logTimer) {
          logTimer = setTimeout(() => {
            logTimer = null
            if (!alive) return
            applyLog = logBuf.slice(-5)
          }, 250)
        }
      }),
    ]
    return () => {
      alive = false
      if (logTimer) clearTimeout(logTimer)
      offs.forEach((off) => off())
    }
  })

  const chosen = $derived.by(() => {
    const out: RepairFix[] = []
    for (const is of report?.issues ?? []) {
      for (const f of is.fixes ?? []) if (selected[fkey(is, f)]) out.push(f)
    }
    return out
  })

  // Одна дія — одне лагодження: вибір двох способів для однієї проблеми
  // (напр. «вимкнути мод» + «завантажити залежність») дозволений, але
  // дубль того самого action відсікається.
  function toggle(issue: RepairIssue, fix: RepairFix) {
    selected = { ...selected, [fkey(issue, fix)]: !selected[fkey(issue, fix)] }
  }

  async function apply() {
    if (!chosen.length || applying) return
    applying = true
    step = null
    applyLog = []
    logBuf = []
    try {
      const seen = new Set<string>()
      const fixes = chosen.filter((f) => {
        const k = f.action + JSON.stringify(f.params ?? {})
        if (seen.has(k)) return false
        seen.add(k)
        return true
      })
      const res = (await App.ApplyPackRepairs(packId, fixes)) ?? []
      if (alive) results = res
      if (res.some((r) => r.needsLogin)) onlogin?.()
      if (res.length && res.every((r) => r.ok)) toast(t('repair.applied'), 'success')
      else if (res.some((r) => !r.ok) && !alive) toast(res.find((r) => !r.ok)!.message, 'error')
    } catch (e: any) {
      toast(t('error.withMessage', { message: e?.message ?? e }), 'error')
    } finally {
      applying = false
      step = null
    }
  }

  async function mute(issue: RepairIssue) {
    try {
      mutedKinds = (await App.SetRepairMuted([issue.id], true)) ?? []
      if (report) report = { ...report, issues: report.issues.filter((i) => i.id !== issue.id) }
      toast(t('repair.muted'), 'info')
    } catch (e: any) {
      toast(t('error.withMessage', { message: e?.message ?? e }), 'error')
    }
  }

  async function unmuteAll() {
    try {
      mutedKinds = (await App.SetRepairMuted(mutedKinds, false)) ?? []
      await diagnose()
    } catch {}
  }

  const failedCount = $derived((results ?? []).filter((r) => !r.ok).length)
</script>

<UiModal
  open={true}
  size="xl"
  title={t('repair.title')}
  subtitle={packName}
  icon="ti-first-aid-kit"
  color="var(--orange)"
  {onclose}
>
  <div class="ar-body">
    {#if applying}
      <div class="ar-progress" role="status" aria-live="polite">
        <div class="ar-progress-top">
          <i class="ti ti-loader spin"></i>
          {#if step}
            <span>{t('repair.step', { done: step.done + 1, total: step.total, label: step.label })}</span>
          {:else}
            <span>{t('repair.applying')}</span>
          {/if}
        </div>
        <div class="ar-progress-hint">{t('repair.slowHint')}</div>
        {#if applyLog.length}
          <pre class="ar-evidence ar-progress-log">{applyLog.slice(-5).join('\n')}</pre>
        {/if}
      </div>
    {/if}
    {#if loading}
      <div class="ar-empty"><i class="ti ti-loader spin"></i> {t('repair.analyzing')}</div>
    {:else if err}
      <div class="ar-empty ar-err"><i class="ti ti-alert-circle"></i> {err}</div>
    {:else if report && report.issues.length === 0}
      <div class="ar-empty ar-ok">
        <i class="ti ti-circle-check"></i>
        <div>{t('repair.none')}</div>
        <div class="ar-sub">{t('repair.noneHint')}</div>
      </div>
    {:else if report}
      {#if report.exitCode != null && report.exitCode !== 0}
        <div class="ar-exit"><i class="ti ti-bug"></i> {t('repair.exitCode', { code: report.exitCode })}</div>
      {/if}
      {#each report.issues as issue (issue.id)}
        <section class="ar-issue">
          <header>
            <div class="ar-title"><i class="ti ti-alert-triangle"></i> {issue.title}</div>
            <button class="ar-link" onclick={() => mute(issue)} title={t('repair.muteHint')}>
              <i class="ti ti-bell-off"></i> {t('repair.mute')}
            </button>
          </header>
          {#if issue.detail}<div class="ar-detail">{issue.detail}</div>{/if}

          {#each issue.fixes ?? [] as fix (fix.id)}
            {@const k = fkey(issue, fix)}
            <label class="ar-fix" class:on={selected[k]}>
              <input type="checkbox" checked={!!selected[k]} onchange={() => toggle(issue, fix)} disabled={applying} />
              <div class="ar-fix-main">
                <div class="ar-fix-label">
                  {fix.label}
                  {#if fix.recommended}<span class="ar-rec">{t('repair.recommended')}</span>{/if}
                </div>
                <ul class="ar-steps">
                  {#each fix.steps ?? [] as s}<li>{s}</li>{/each}
                </ul>
              </div>
            </label>
          {/each}

          {#if (issue.fixes ?? []).length === 0}
            <div class="ar-detail"><i class="ti ti-hand-stop"></i> {t('repair.manualOnly')}</div>
          {/if}
          {#if issue.manual}
            <div class="ar-manual"><i class="ti ti-info-circle"></i> {issue.manual}</div>
          {/if}
          {#if (issue.evidence ?? []).length}
            <button class="ar-link" onclick={() => (expanded = { ...expanded, [issue.id]: !expanded[issue.id] })}>
              <i class="ti {expanded[issue.id] ? 'ti-chevron-up' : 'ti-chevron-down'}"></i> {t('repair.evidence')}
            </button>
            {#if expanded[issue.id]}
              <pre class="ar-evidence">{(issue.evidence ?? []).join('\n')}</pre>
            {/if}
          {/if}
        </section>
      {/each}

      {#if results}
        <section class="ar-results">
          <div class="ar-title">{t('repair.results')}</div>
          {#each results as r}
            <div class="ar-res" class:bad={!r.ok}>
              <i class="ti {r.ok ? 'ti-check' : 'ti-x'}"></i> {r.message}
            </div>
          {/each}
          {#if failedCount === 0}<div class="ar-sub">{t('repair.tryLaunch')}</div>{/if}
        </section>
      {/if}
    {/if}

    {#if mutedKinds.length}
      <div class="ar-muted">
        <i class="ti ti-bell-off"></i> {t('repair.mutedCount', { n: mutedKinds.length })}
        <button class="ar-link" onclick={unmuteAll}>{t('repair.unmute')}</button>
      </div>
    {/if}
  </div>

  {#snippet footer()}
    {#if onconsole}
      <button class="btn btn-ghost" onclick={onconsole}><i class="ti ti-terminal-2"></i> {t('repair.console')}</button>
    {/if}
    <button class="btn btn-ghost" onclick={diagnose} disabled={loading || applying}>
      <i class="ti ti-refresh"></i> {t('repair.recheck')}
    </button>
    <button class="btn btn-ghost" onclick={() => onclose()}>{t('close')}</button>
    <button class="btn btn-primary" onclick={apply} disabled={applying || chosen.length === 0}>
      <i class="ti {applying ? 'ti-loader spin' : 'ti-tool'}"></i>
      {applying ? t('repair.applying') : t('repair.apply', { n: chosen.length })}
    </button>
  {/snippet}
</UiModal>

<style>
  .ar-body { display: flex; flex-direction: column; gap: 14px; }
  .ar-progress { border: 1px solid var(--orange); border-radius: 10px; padding: 10px 12px; display: flex; flex-direction: column; gap: 6px; background: rgba(var(--orange-rgb), .06); }
  .ar-progress-top { display: flex; gap: 8px; align-items: center; font-size: 12.5px; font-weight: 600; }
  .ar-progress-top i { color: var(--orange); }
  .ar-progress-hint { font-size: 11.5px; color: var(--text-mute); }
  .ar-progress-log { max-height: 110px; }
  .ar-empty { display: flex; flex-direction: column; align-items: center; gap: 8px; padding: 34px 10px; color: var(--text-mute); font-size: 13px; text-align: center; }
  .ar-empty i { font-size: 30px; }
  .ar-ok i { color: var(--green); }
  .ar-err { color: var(--red); }
  .ar-sub { font-size: 12px; color: var(--text-mute); }
  .ar-exit { font-size: 12px; color: var(--text-mute); display: flex; gap: 6px; align-items: center; }
  .ar-issue { border: 1px solid var(--border); border-radius: var(--r-md, 10px); padding: 12px 14px; display: flex; flex-direction: column; gap: 9px; background: color-mix(in srgb, var(--bg-card) 70%, transparent); }
  .ar-issue header { display: flex; justify-content: space-between; align-items: center; gap: 10px; }
  .ar-title { font-weight: 700; font-size: 13.5px; display: flex; gap: 7px; align-items: center; }
  .ar-title i { color: var(--orange); }
  .ar-detail { font-size: 12.5px; color: var(--text-mute); line-height: 1.55; }
  .ar-fix { display: flex; gap: 10px; padding: 9px 10px; border-radius: 9px; border: 1px solid var(--border); cursor: pointer; align-items: flex-start; }
  .ar-fix.on { border-color: var(--orange); background: rgba(var(--orange-rgb), .07); }
  .ar-fix input { margin-top: 3px; accent-color: var(--orange); }
  .ar-fix-label { font-size: 13px; font-weight: 600; display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
  .ar-rec { font-size: 10.5px; font-weight: 700; padding: 2px 7px; border-radius: 6px; background: rgba(16,185,129,.15); color: var(--green); }
  .ar-steps { margin: 5px 0 0; padding-left: 17px; font-size: 12px; color: var(--text-mute); line-height: 1.55; }
  .ar-manual { font-size: 12px; color: var(--text-mute); display: flex; gap: 6px; }
  .ar-evidence { margin: 0; max-height: 170px; overflow: auto; font-size: 11px; padding: 9px 10px; border-radius: 8px; background: rgba(0,0,0,.25); white-space: pre-wrap; word-break: break-word; font-family: var(--font-mono); }
  .ar-link { all: unset; cursor: pointer; font-size: 11.5px; color: var(--text-mute); display: inline-flex; gap: 5px; align-items: center; align-self: flex-start; }
  .ar-link:hover { color: var(--orange); }
  .ar-results { border: 1px solid var(--border); border-radius: 10px; padding: 12px 14px; display: flex; flex-direction: column; gap: 6px; }
  .ar-res { font-size: 12.5px; display: flex; gap: 7px; align-items: flex-start; }
  .ar-res i { color: var(--green); margin-top: 2px; }
  .ar-res.bad i { color: var(--red); }
  .ar-muted { font-size: 11.5px; color: var(--text-mute); display: flex; gap: 8px; align-items: center; }
</style>
