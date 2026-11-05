<script lang="ts">
  // ── Доктор крашів (авто-починка збірки) ──────────────────────────────────
  // Відповідає на одне питання: «чому впала гра і що натиснути». Тому:
  //  • зверху — головна причина простими словами;
  //  • ОДНА велика кнопка з головним (рекомендованим) лагодженням;
  //  • решта варіантів і докази з логів — згорнуті, щоб не розгубитись;
  //  • після вдалого лагодження — кнопка «Грати знову».
  // Закриття працює ЗАВЖДИ (хрестик, «Закрити», Esc, клік по тлі), навіть
  // поки лагодження триває: воно доробиться у фоні й повідомить тостом.
  // Раніше вікно могло «застрягти» й перекривати лаунчер — саме через це
  // доктора й вимикали.
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
    onlogin = null as null | (() => void),
    // «Грати знову» після вдалого лагодження.
    onrelaunch = null as null | (() => void),
  } = $props<{
    packId: string
    packName?: string
    initial?: null | RepairReport
    onclose?: () => void
    onconsole?: null | (() => void)
    onlogin?: null | (() => void)
    onrelaunch?: null | (() => void)
  }>()

  type Choice = { issue: RepairIssue; fix: RepairFix }

  let report = $state<RepairReport | null>(null)
  let loading = $state(false)
  let applying = $state('')            // ключ лагодження, що застосовується
  let err = $state('')
  let results = $state<RepairFixResult[] | null>(null)
  let showMore = $state(false)
  let showLogs = $state(false)
  let mutedKinds = $state<string[]>([])
  let step = $state<RepairProgressEvent | null>(null)
  let applyLog = $state<string[]>([])
  let logBuf: string[] = []
  let logTimer: ReturnType<typeof setTimeout> | null = null
  let alive = true

  const fkey = (issue: RepairIssue, fix: RepairFix) => `${issue.id}|${fix.id}`

  function adopt(r: RepairReport) {
    // Глибока копія: діалог працює з власними даними, не з реактивним станом
    // App (інакше зміни тут могли зачіпати батька й зациклювати оновлення).
    const plain = JSON.parse(JSON.stringify(r)) as RepairReport
    report = { ...plain, issues: plain.issues ?? [], mutedIssues: plain.mutedIssues ?? [] }
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

  $effect(() => {
    const pid = packId
    const init = initial
    untrack(() => {
      void pid
      App.GetRepairMuted().then((m) => { if (alive) mutedKinds = m ?? [] }).catch(() => {})
      if (init) adopt(init)
      else void diagnose()
    })
  })

  onMount(() => {
    const offs = [
      on('repair:progress', (e: RepairProgressEvent) => { if (alive && e?.packId === packId) step = e }),
      // installer:log під час качки — сотні подій на секунду: складаємо в
      // буфер і показуємо не частіше ~4 разів/с, щоб вікно не «замерзало».
      on('installer:log', (msg: string) => {
        if (!applying) return
        logBuf.push(String(msg ?? ''))
        if (logBuf.length > 40) logBuf.splice(0, logBuf.length - 40)
        if (!logTimer) {
          logTimer = setTimeout(() => {
            logTimer = null
            if (alive) applyLog = logBuf.slice(-3)
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

  // Головна причина — перша проблема з лагодженням (звіт уже впорядкований
  // за важливістю); головне лагодження — її рекомендоване або перше.
  const issues = $derived(report?.issues ?? [])
  const mainIssue = $derived(issues.find((i) => (i.fixes ?? []).length > 0) ?? issues[0] ?? null)
  const mainFix = $derived(mainIssue ? ((mainIssue.fixes ?? []).find((f) => f.recommended) ?? (mainIssue.fixes ?? [])[0] ?? null) : null)
  const others = $derived.by<Choice[]>(() => {
    const out: Choice[] = []
    for (const issue of issues) for (const fix of issue.fixes ?? []) {
      if (mainIssue && mainFix && issue.id === mainIssue.id && fix.id === mainFix.id) continue
      out.push({ issue, fix })
    }
    return out
  })
  const otherIssues = $derived(issues.filter((i) => i !== mainIssue))
  const allOk = $derived(!!results && results.length > 0 && results.every((r) => r.ok))
  const evidence = $derived(issues.flatMap((i) => i.evidence ?? []).slice(0, 40))

  async function apply(issue: RepairIssue, fix: RepairFix) {
    if (applying) return
    applying = fkey(issue, fix)
    step = null
    applyLog = []
    logBuf = []
    try {
      const res = (await App.ApplyPackRepairs(packId, [fix])) ?? []
      if (res.some((r) => r.needsLogin)) onlogin?.()
      if (!alive) {
        // Вікно вже закрили — повідомляємо результат тостом.
        if (res.length && res.every((r) => r.ok)) toast(t('repair.applied'), 'success')
        else if (res.some((r) => !r.ok)) toast(res.find((r) => !r.ok)!.message, 'error')
        return
      }
      results = res
      if (res.length && res.every((r) => r.ok)) toast(t('repair.applied'), 'success')
    } catch (e: any) {
      toast(t('error.withMessage', { message: e?.message ?? e }), 'error')
    } finally {
      if (alive) { applying = ''; step = null }
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
    } catch { /* ignore */ }
  }

  function close() { onclose() }
</script>

<UiModal open={true} size="lg" title={t('repair.title')} subtitle={packName} icon="ti-stethoscope" color="var(--orange)" onclose={close}>
  <div class="dr">
    {#if loading}
      <div class="dr-empty"><i class="ti ti-loader-2 spin"></i>{t('repair.analyzing')}</div>
    {:else if err}
      <div class="dr-empty bad"><i class="ti ti-alert-circle"></i>{err}</div>
    {:else if report && issues.length === 0}
      <div class="dr-empty ok">
        <i class="ti ti-circle-check"></i>
        <b>{t('repair.none')}</b>
        <span>{t('repair.noneHint')}</span>
      </div>
    {:else if mainIssue}
      <!-- Головна причина -->
      <section class="dr-cause">
        <div class="dr-cause-ic"><i class="ti ti-bug"></i></div>
        <div class="dr-cause-tx">
          <span class="dr-kicker">{t('repair.why')}</span>
          <b>{mainIssue.title}</b>
          {#if mainIssue.detail}<p>{mainIssue.detail}</p>{/if}
        </div>
      </section>

      {#if allOk}
        <div class="dr-done">
          <i class="ti ti-circle-check"></i>
          <div><b>{t('repair.doneTitle')}</b><span>{t('repair.tryLaunch')}</span></div>
          {#if onrelaunch}
            <button class="btn btn-primary" onclick={() => onrelaunch?.()}><i class="ti ti-player-play"></i> {t('repair.playAgain')}</button>
          {/if}
        </div>
      {:else if mainFix}
        <!-- Головне лагодження: одна кнопка, під нею — що саме буде зроблено -->
        <section class="dr-fix">
          <button class="btn btn-primary dr-fix-btn" disabled={!!applying} onclick={() => apply(mainIssue!, mainFix!)}>
            {#if applying === fkey(mainIssue, mainFix)}<i class="ti ti-loader-2 spin"></i>{:else}<i class="ti ti-wand"></i>{/if}
            {mainFix.label}
          </button>
          {#if (mainFix.steps ?? []).length}
            <ul class="dr-steps">{#each mainFix.steps as s}<li>{s}</li>{/each}</ul>
          {/if}
          <span class="dr-safe"><i class="ti ti-shield-check"></i>{t('repair.safe')}</span>
        </section>
      {:else}
        <div class="dr-manual"><i class="ti ti-hand-stop"></i><span>{mainIssue.manual || t('repair.manualOnly')}</span></div>
      {/if}

      {#if mainIssue.manual && mainFix}
        <div class="dr-manual"><i class="ti ti-info-circle"></i><span>{mainIssue.manual}</span></div>
      {/if}

      {#if applying}
        <div class="dr-progress" role="status" aria-live="polite">
          <div><i class="ti ti-loader-2 spin"></i>{step ? t('repair.step', { done: step.done + 1, total: step.total, label: step.label }) : t('repair.applying')}</div>
          <span>{t('repair.slowHint')}</span>
          {#if applyLog.length}<pre>{applyLog.join('\n')}</pre>{/if}
        </div>
      {/if}

      {#if results && !allOk}
        <div class="dr-results">
          {#each results as r}
            <div class:bad={!r.ok}><i class="ti {r.ok ? 'ti-check' : 'ti-x'}"></i>{r.message}</div>
          {/each}
        </div>
      {/if}

      <!-- Інші варіанти — згорнуто, щоб не губитись у списку -->
      {#if others.length > 0 || otherIssues.length > 0}
        <button class="dr-toggle" onclick={() => (showMore = !showMore)} aria-expanded={showMore}>
          <i class="ti ti-chevron-right" class:open={showMore}></i>
          {t('repair.more', { n: others.length || otherIssues.length })}
        </button>
        {#if showMore}
          <div class="dr-more">
            {#each others as c (fkey(c.issue, c.fix))}
              <div class="dr-alt">
                <div class="dr-alt-tx">
                  <b>{c.fix.label}</b>
                  <span>{c.issue.title}</span>
                </div>
                <button class="btn btn-ghost btn-sm" disabled={!!applying} onclick={() => apply(c.issue, c.fix)}>
                  {#if applying === fkey(c.issue, c.fix)}<i class="ti ti-loader-2 spin"></i>{:else}{t('repair.do')}{/if}
                </button>
              </div>
            {/each}
            {#each otherIssues.filter((i) => !(i.fixes ?? []).length) as i (i.id)}
              <div class="dr-alt">
                <div class="dr-alt-tx"><b>{i.title}</b><span>{i.manual || t('repair.manualOnly')}</span></div>
              </div>
            {/each}
          </div>
        {/if}
      {/if}

      {#if evidence.length}
        <button class="dr-toggle" onclick={() => (showLogs = !showLogs)} aria-expanded={showLogs}>
          <i class="ti ti-chevron-right" class:open={showLogs}></i>{t('repair.evidence')}
        </button>
        {#if showLogs}<pre class="dr-logs">{evidence.join('\n')}</pre>{/if}
      {/if}

      <div class="dr-meta">
        {#if report?.exitCode != null && report.exitCode !== 0}<span>{t('repair.exitCode', { code: report.exitCode })}</span>{/if}
        <button class="dr-link" onclick={() => mute(mainIssue!)} title={t('repair.muteHint')}><i class="ti ti-bell-off"></i>{t('repair.mute')}</button>
        {#if mutedKinds.length}<button class="dr-link" onclick={unmuteAll}>{t('repair.mutedCount', { n: mutedKinds.length })} · {t('repair.unmute')}</button>{/if}
      </div>
    {/if}
  </div>

  {#snippet footer()}
    {#if onconsole}
      <button class="btn btn-ghost" onclick={onconsole}><i class="ti ti-terminal-2"></i> {t('repair.console')}</button>
    {/if}
    <button class="btn btn-ghost" onclick={diagnose} disabled={loading || !!applying}><i class="ti ti-refresh"></i> {t('repair.recheck')}</button>
    <button class="btn btn-ghost" onclick={close}>{t('close')}</button>
  {/snippet}
</UiModal>

<style>
  .dr { display: flex; flex-direction: column; gap: 12px; }
  .dr-empty { display: flex; flex-direction: column; align-items: center; gap: 8px; padding: 30px 10px; color: var(--text-mute); font-size: 13px; text-align: center; }
  .dr-empty i { font-size: 30px; color: var(--orange); }
  .dr-empty.ok i { color: var(--green); }
  .dr-empty.bad, .dr-empty.bad i { color: var(--red); }
  .dr-empty b { color: var(--text); font-size: 14px; }

  .dr-cause { display: flex; gap: 14px; padding: 14px 16px; border-radius: var(--r-lg); border: 1px solid rgba(var(--orange-rgb), .3);
    background: linear-gradient(135deg, rgba(var(--orange-rgb), .1), var(--bg-panel) 65%); animation: dr-in .35s cubic-bezier(.16,1,.3,1) both; }
  .dr-cause-ic { flex: none; display: grid; place-items: center; width: 40px; height: 40px; border-radius: 12px; font-size: 20px; color: var(--orange); background: rgba(var(--orange-rgb), .15); }
  .dr-cause-tx { min-width: 0; display: flex; flex-direction: column; gap: 3px; }
  .dr-kicker { font-size: 10.5px; font-weight: 800; letter-spacing: .06em; text-transform: uppercase; color: var(--orange); }
  .dr-cause-tx b { font-size: 15px; color: var(--text); }
  .dr-cause-tx p { margin: 2px 0 0; font-size: 12.5px; line-height: 1.55; color: var(--text-mute); }

  .dr-fix { display: flex; flex-direction: column; gap: 8px; animation: dr-in .35s .06s cubic-bezier(.16,1,.3,1) both; }
  .dr-fix-btn { width: 100%; min-height: 44px; font-size: 14px; }
  .dr-steps { margin: 0; padding: 0 0 0 18px; font-size: 12px; line-height: 1.6; color: var(--text-mute); }
  .dr-safe { display: inline-flex; align-items: center; gap: 6px; font-size: 11.5px; color: var(--text-dim); }
  .dr-safe i { color: var(--green); }

  .dr-done { display: flex; align-items: center; gap: 12px; padding: 12px 14px; border-radius: var(--r-lg); border: 1px solid rgba(16,185,129,.35); background: rgba(16,185,129,.08); animation: dr-in .35s cubic-bezier(.16,1,.3,1) both; }
  .dr-done > i { font-size: 24px; color: var(--green); }
  .dr-done div { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
  .dr-done b { font-size: 13.5px; }
  .dr-done span { font-size: 12px; color: var(--text-mute); }

  .dr-manual { display: flex; gap: 8px; font-size: 12px; line-height: 1.5; color: var(--text-mute); padding: 10px 12px; border-radius: var(--r-md); background: var(--bg-input); border: 1px solid var(--border); }
  .dr-manual i { flex: none; margin-top: 1px; color: var(--text-dim); font-size: 15px; }

  .dr-progress { display: flex; flex-direction: column; gap: 5px; padding: 10px 12px; border-radius: var(--r-md); border: 1px solid rgba(var(--orange-rgb), .35); background: rgba(var(--orange-rgb), .06); font-size: 12px; }
  .dr-progress div { display: flex; align-items: center; gap: 8px; font-weight: 600; }
  .dr-progress div i { color: var(--orange); }
  .dr-progress span { color: var(--text-mute); font-size: 11.5px; }
  .dr-progress pre, .dr-logs { margin: 0; max-height: 150px; overflow: auto; padding: 8px 10px; border-radius: 8px; background: rgba(0,0,0,.25); font: 11px/1.45 var(--font-mono); white-space: pre-wrap; word-break: break-word; color: var(--text-mute); }

  .dr-results { display: flex; flex-direction: column; gap: 4px; font-size: 12.5px; }
  .dr-results div { display: flex; gap: 7px; align-items: flex-start; }
  .dr-results i { color: var(--green); margin-top: 2px; }
  .dr-results .bad i { color: var(--red); }

  .dr-toggle { all: unset; cursor: pointer; display: inline-flex; align-items: center; gap: 6px; align-self: flex-start; font-size: 12.5px; font-weight: 600; color: var(--text-mute); }
  .dr-toggle:hover { color: var(--text); }
  .dr-toggle:focus-visible { outline: 2px solid var(--orange); outline-offset: 3px; border-radius: 4px; }
  .dr-toggle i { transition: transform .2s cubic-bezier(.34,1.56,.64,1); }
  .dr-toggle i.open { transform: rotate(90deg); color: var(--orange); }
  .dr-more { display: flex; flex-direction: column; gap: 2px; padding: 4px; border-radius: var(--r-md); background: var(--bg-input); border: 1px solid var(--border); animation: dr-in .25s cubic-bezier(.16,1,.3,1) both; }
  .dr-alt { display: flex; align-items: center; gap: 10px; padding: 8px 10px; border-radius: 9px; }
  .dr-alt:hover { background: var(--hover); }
  .dr-alt-tx { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
  .dr-alt-tx b { font-size: 12.5px; }
  .dr-alt-tx span { font-size: 11.5px; color: var(--text-mute); line-height: 1.45; }

  .dr-meta { display: flex; flex-wrap: wrap; align-items: center; gap: 14px; font-size: 11.5px; color: var(--text-dim); }
  .dr-link { all: unset; cursor: pointer; display: inline-flex; align-items: center; gap: 5px; }
  .dr-link:hover { color: var(--orange); }
  @keyframes dr-in { from { opacity: 0; transform: translateY(6px); } to { opacity: 1; transform: none; } }
</style>
