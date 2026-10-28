<script lang="ts">
  // Індикатор активності у верхній панелі: одна кнопка з кільцем загального прогресу, клік —
  // список усього, що зараз триває (завантаження збірок, передачі друзям, встановлення ядра
  // сервера, міграції, експорт…). Передачі збірок показуються тут же розгорнутими рядками —
  // клік відкриває повне вікно (ShareTransferModal); щойно завершені лишаються кілька секунд із
  // підсумком. Тут же змонтовані модалки передач: пропозиція від друга, вікно передачі,
  // «недокачане з минулого разу» (через Portal — поза областю перетягування вікна).
  import { App } from './wails'
  import { toast } from './toast.svelte'
  import { t } from './i18n.svelte'
  import Portal from './Portal.svelte'
  import './settings/settings.css'
  import { activityState, initActivity, activityIcon, type ActivityItem } from './activityStore.svelte'
  import { shareIn, shareOut, shareUi, openTransfer, friendName, errText, setOpenMod } from './friends/shareStore.svelte'
  import { fmtBytes, fmtEta, pctOf, inReasonText, IN_ACTIVE, IN_FINAL, REC_FINAL } from './friends/shareMsg'
  import ShareTransferModal from './friends/ShareTransferModal.svelte'
  import ShareOfferModal from './friends/ShareOfferModal.svelte'
  import ShareRecoveryModal from './friends/ShareRecoveryModal.svelte'
  import ShareContentsModal from './friends/ShareContentsModal.svelte'

  // Перехід із вмісту передачі на сторінку мода в браузері контенту (дає App).
  let { onOpenMod = null }: { onOpenMod?: ((source: string, projectId: string) => void) | null } = $props()
  $effect(() => {
    setOpenMod(onOpenMod)
    return () => setOpenMod(null)
  })
  $effect(() => initActivity())

  // Підсумок щойно завершеної передачі тримаємо LINGER мс (оновлюємо «зараз» раз на секунду).
  const LINGER = 8000
  let now = $state(Date.now())
  $effect(() => {
    const id = setInterval(() => (now = Date.now()), 1000)
    return () => clearInterval(id)
  })
  const fresh = (at?: number) => !!at && now - at < LINGER

  const ins = $derived(Object.values(shareIn).filter(v => IN_ACTIVE.includes(v.phase) || (IN_FINAL.includes(v.phase) && v.phase !== 'declined' && fresh(v.updatedAt))))
  const outs = $derived(Object.values(shareOut).filter(v => v.phase === 'preparing' || v.phase === 'ready' || fresh(v.updatedAt)))
  // Пропозицію згорнули (перейшли в браузер подивитись мод) — рядок, щоб повернутись до неї.
  const hiddenOffer = $derived(shareUi.offersHidden ? shareUi.offers[0] : undefined)
  // Передачі друзям уже є розгорнутими рядками вище — з реєстру беремо решту.
  const others = $derived(activityState.items.filter(a => a.kind !== 'share-in' && a.kind !== 'share-out'))
  const count = $derived(ins.length + outs.length + others.length + (hiddenOffer ? 1 : 0))

  // Кільце на кнопці — як прогрес у панелі завдань: найбільший % серед активних.
  const summary = $derived.by(() => {
    const items = activityState.items
    const run = items.filter(a => a.state === 'running')
    if (run.length) return { tone: 'run', pct: Math.max(...run.map(a => a.percent)) }
    const paused = items.filter(a => a.state === 'paused')
    if (paused.length) return { tone: 'pause', pct: Math.max(...paused.map(a => a.percent)) }
    if (items.some(a => a.state === 'error')) return { tone: 'bad', pct: 100 }
    if (hiddenOffer || items.some(a => a.state === 'waiting')) return { tone: 'wait', pct: -1 }
    return { tone: 'ok', pct: 100 }
  })

  let open = $state(false)
  let btn = $state<HTMLButtonElement | null>(null)
  let pop = $state<HTMLDivElement | null>(null)
  let pos = $state({ top: 0, right: 0 })
  function place() {
    if (!btn) return
    const r = btn.getBoundingClientRect()
    pos = { top: r.bottom + 8, right: Math.max(8, window.innerWidth - r.right) }
  }
  function toggle() {
    open = !open
    if (open) place()
  }
  // Нова передача від/для друга відкриває список сама (раніше вона одразу була видна в
  // плаваючому віджеті); решта операцій запускається зі своїх сторінок і там же видна.
  const seen = new Set<string>()
  $effect(() => {
    const ids = [...ins.map(v => 'in:' + v.id), ...outs.map(o => 'out:' + o.id), ...(hiddenOffer ? ['offer:' + hiddenOffer.card.sid] : [])]
    let added = false
    for (const id of ids) if (!seen.has(id)) { seen.add(id); added = true }
    if (added && !open) { open = true; queueMicrotask(place) }
  })
  $effect(() => { if (count === 0) open = false })
  $effect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      const el = e.target as Node
      if (!pop?.contains(el) && !btn?.contains(el)) open = false
    }
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') open = false }
    window.addEventListener('mousedown', onDown, true)
    window.addEventListener('keydown', onKey)
    window.addEventListener('resize', place)
    return () => {
      window.removeEventListener('mousedown', onDown, true)
      window.removeEventListener('keydown', onKey)
      window.removeEventListener('resize', place)
    }
  })

  const err = (e: any) => toast(errText(e), 'error')

  function outLine(o: typeof outs[number]): string {
    if (o.phase === 'preparing') return t('share.out.preparing', { done: o.hashed, total: o.hashTotal || '…' })
    const rs = o.recipients ?? []
    if (o.phase === 'error') return o.error || t('share.out.reason.error')
    if (rs.length === 0) return t('share.out.noRecipients')
    const ok = rs.filter(r => r.state === 'done').length
    const act = rs.filter(r => r.state === 'transferring').length
    const off = rs.filter(r => r.offline && !REC_FINAL.includes(r.state)).length
    const fin = rs.filter(r => REC_FINAL.includes(r.state)).length
    if (fin === rs.length) return t('share.widget.outFinished', { ok, n: rs.length })
    const parts = [t('share.widget.outProgress', { ok, n: rs.length })]
    if (act) parts.push(t('share.widget.outActive', { n: act }))
    if (off) parts.push(t('share.widget.outOffline', { n: off }))
    return parts.join(' · ')
  }
  function outPct(o: typeof outs[number]): number {
    if (o.phase === 'preparing') return pctOf(o.hashed, o.hashTotal)
    const rs = (o.recipients ?? []).filter(r => r.state !== 'declined' && r.state !== 'revoked')
    if (!rs.length || !o.totalBytes) return 0
    return Math.floor(rs.reduce((s, r) => s + (r.state === 'done' ? 100 : pctOf(r.bytesDone, o.totalBytes)), 0) / rs.length)
  }
  function inLine(v: typeof ins[number]): string {
    const p = v.progress
    switch (v.phase) {
      case 'connecting': return t('share.in.connecting', { name: v.from })
      case 'verifying': return t('share.in.verifying')
      case 'installing': return t('share.in.installing')
      case 'paused': return p.pausedBy === 'sender' ? t('share.in.pausedSender', { name: v.from }) : t('share.in.paused')
      case 'done': return t(v.kind === 'server' ? 'share.in.doneServer' : 'share.in.done')
      case 'downloading': {
        if (p.reconnecting) return t('share.in.reconnecting', { name: v.from })
        const total = p.bytesTotal || v.bytes
        const eta = fmtEta(total - p.bytesDone, p.speedBps)
        return `${fmtBytes(p.bytesDone)} / ${fmtBytes(total)} · ${fmtBytes(p.speedBps)}/${t('share.unit.s')}${eta ? ' · ' + eta : ''}`
      }
      default: return inReasonText(v.reason || (v.phase === 'cancelled' ? 'cancelled_receiver' : 'error'), v.error)
    }
  }
  const tone = (phase: string, reason?: string) =>
    phase === 'done' ? 'ok' : phase === 'error' ? 'bad' : phase === 'cancelled' ? (reason === 'cancelled_receiver' ? 'mute' : 'warn') : phase === 'paused' ? 'pause' : ''

  // Рядок операції з реєстру: чип стану (.st-tag) і підпис «вид · деталь».
  const actTone = (a: ActivityItem) => a.state === 'done' ? 'ok' : a.state === 'error' ? 'bad' : a.state === 'paused' ? 'pause' : ''
  const actTagCls = (a: ActivityItem) => a.state === 'done' ? 'ok' : a.state === 'error' ? 'bad' : a.state === 'paused' ? 'warn' : a.state === 'running' ? 'on' : ''
  const actTag = (a: ActivityItem) => a.state === 'running' && a.percent >= 0 ? a.percent + '%' : t('activity.state.' + a.state)
  const kindLabel = (a: ActivityItem) => t('activity.kind.' + a.kind)
  const actSub = (a: ActivityItem) => {
    // Стадія завантаження — внутрішній код, підпис виду вже все каже.
    const d = a.kind === 'download' ? '' : (a.detail ?? '')
    return d && d !== a.title ? `${kindLabel(a)} · ${d}` : kindLabel(a)
  }

  // Кільце: довжина кола r=9 → 56.55.
  const C = 2 * Math.PI * 9
</script>

{#if count > 0}
  <button bind:this={btn} class="act-btn {summary.tone}" class:open onclick={toggle}
          title={t('activity.title')} aria-label={t('activity.title')} aria-expanded={open}>
    <svg class="act-ring" class:spin={summary.tone === 'run' && summary.pct < 0} viewBox="0 0 24 24" aria-hidden="true">
      <circle class="trk" cx="12" cy="12" r="9" />
      <circle class="val" cx="12" cy="12" r="9" stroke-dasharray={C}
              stroke-dashoffset={summary.pct < 0 ? C * 0.72 : C * (1 - summary.pct / 100)} />
    </svg>
    {#if summary.tone === 'run' && summary.pct >= 0}<span class="act-pct">{summary.pct}%</span>{/if}
    {#if count > 1}<span class="st-tag">{count}</span>{/if}
  </button>
{/if}

<Portal>
  {#if open && count > 0}
    <div bind:this={pop} class="st-menu act-pop" role="region" aria-label={t('activity.title')}
         style="top:{pos.top}px;right:{pos.right}px">
      <div class="act-head">{t('activity.title')}</div>
      {#if hiddenOffer}
        <div class="act-row offer">
          <button class="act-open" onclick={() => { shareUi.offersHidden = false; open = false }} aria-label={t('share.details')}>
            <span class="act-ic in"><i class="ti ti-gift"></i></span>
            <span class="act-body">
              <span class="act-top"><b title={hiddenOffer.card.n}>{hiddenOffer.card.n}</b><span class="act-from">{t('share.widget.from', { name: hiddenOffer.fromName })}</span></span>
              <span class="act-sub">{t('share.widget.offerWaiting')}{shareUi.offers.length > 1 ? ' · +' + (shareUi.offers.length - 1) : ''}</span>
            </span>
          </button>
        </div>
      {/if}
      {#each ins as v (v.id)}
        {@const pc = v.phase === 'done' ? 100 : pctOf(v.progress.bytesDone, v.progress.bytesTotal || v.bytes)}
        <div class="act-row {tone(v.phase, v.reason)}">
          <button class="act-open" onclick={() => { openTransfer('in', v.id); open = false }} aria-label={t('share.details')}>
            <span class="act-ic in"><i class="ti {v.phase === 'done' ? 'ti-circle-check' : v.phase === 'error' ? 'ti-alert-triangle' : 'ti-download'}"></i></span>
            <span class="act-body">
              <span class="act-top"><b title={v.name}>{v.name}</b><span class="act-from">{t('share.widget.from', { name: v.from })}</span></span>
              {#if IN_ACTIVE.includes(v.phase) && v.phase !== 'connecting' && v.phase !== 'installing'}
                <span class="act-bar"><span class="act-fill" class:paused={v.phase === 'paused' || v.progress.reconnecting} style="width:{pc}%"></span></span>
              {/if}
              <span class="act-sub">{IN_ACTIVE.includes(v.phase) && v.phase === 'downloading' ? pc + '% · ' : ''}{inLine(v)}</span>
            </span>
          </button>
          {#if v.phase === 'downloading' || v.phase === 'paused'}
            <div class="act-acts">
              {#if v.phase === 'paused' && v.progress.pausedBy !== 'sender'}
                <button aria-label={t('share.resume')} title={t('share.resume')} onclick={() => App.ShareResume(v.id).catch(err)}><i class="ti ti-player-play"></i></button>
              {:else if v.phase === 'downloading'}
                <button aria-label={t('share.pause')} title={t('share.pause')} onclick={() => App.SharePause(v.id).catch(err)}><i class="ti ti-player-pause"></i></button>
              {/if}
            </div>
          {/if}
        </div>
      {/each}

      {#each outs as o (o.id)}
        {@const finished = o.phase === 'closed' || o.phase === 'error'}
        <div class="act-row" class:ok={finished && o.phase !== 'error'} class:bad={o.phase === 'error'}>
          <button class="act-open" onclick={() => { openTransfer('out', o.id); open = false }} aria-label={t('share.details')}>
            <span class="act-ic out"><i class="ti {finished ? 'ti-flag-check' : 'ti-upload'}"></i></span>
            <span class="act-body">
              <span class="act-top"><b title={o.name}>{o.name}</b>
                {#if (o.recipients?.length ?? 0) > 0}<span class="act-from">{o.recipients.length === 1 ? t('share.widget.to', { name: friendName(o.recipients[0].id, o.names?.[o.recipients[0].id]) }) : t('share.widget.toN', { n: o.recipients.length })}</span>{/if}
              </span>
              {#if !finished}<span class="act-bar"><span class="act-fill" style="width:{outPct(o)}%"></span></span>{/if}
              <span class="act-sub">{outLine(o)}</span>
            </span>
          </button>
        </div>
      {/each}

      {#each others as a (a.id)}
        <div class="act-row {actTone(a)}">
          <div class="act-open static">
            <span class="act-ic out"><i class="ti {a.state === 'done' ? 'ti-circle-check' : a.state === 'error' ? 'ti-alert-triangle' : activityIcon(a.kind)}"></i></span>
            <span class="act-body">
              <span class="act-top"><b title={a.title || kindLabel(a)}>{a.title || kindLabel(a)}</b><span class="st-tag {actTagCls(a)}">{actTag(a)}</span></span>
              {#if a.state === 'running' || a.state === 'paused'}
                <span class="act-bar"><span class="act-fill" class:paused={a.state === 'paused'} class:ind={a.percent < 0}
                      style="width:{a.percent < 0 ? 30 : a.percent}%"></span></span>
              {/if}
              <span class="act-sub" title={actSub(a)}>{actSub(a)}</span>
            </span>
          </div>
        </div>
      {/each}
    </div>
  {/if}

  <ShareRecoveryModal />
  <ShareTransferModal />
  <ShareOfferModal />
  <ShareContentsModal />
</Portal>

<style>
  /* Кнопка в рядку верхньої панелі — та сама «плашка», що й пошук і віконні кнопки. */
  .act-btn { display: flex; align-items: center; gap: 7px; height: 40px; padding: 0 10px; border-radius: 12px;
    background: var(--hover); border: 1px solid var(--border); color: var(--text); cursor: pointer; font: 600 12px var(--font-ui);
    transition: border-color .15s, background .15s; flex: none; --wails-draggable: no-drag; }
  .act-btn:hover, .act-btn.open { border-color: rgba(var(--orange-rgb), .6); background: var(--hover-strong); }
  .act-ring { width: 22px; height: 22px; transform: rotate(-90deg); flex: none; }
  .act-ring.spin { animation: act-spin 1s linear infinite; }
  .act-ring circle { fill: none; stroke-width: 3; }
  .act-ring .trk { stroke: var(--bg-input); }
  .act-ring .val { stroke: var(--orange); stroke-linecap: round; transition: stroke-dashoffset .3s; }
  .act-btn.pause .val, .act-btn.wait .val { stroke: var(--text-mute); }
  .act-btn.bad .val { stroke: var(--red); }
  .act-btn.ok .val { stroke: var(--green); }
  .act-pct { color: var(--orange); font-variant-numeric: tabular-nums; }
  @keyframes act-spin { from { transform: rotate(-90deg); } to { transform: rotate(270deg); } }

  /* Список — .st-menu з налаштувань лаунчера; тут лише розкладка рядків. */
  .act-pop { position: fixed; width: min(400px, calc(100vw - 32px)); max-height: min(480px, calc(100vh - 120px)); padding: 6px; gap: 2px; }
  .act-head { padding: 6px 10px 8px; font: 700 12px var(--font-ui); color: var(--text-mute); text-transform: uppercase; letter-spacing: .04em; }
  .act-row { display: flex; align-items: center; border-radius: 8px; flex: none; }
  .act-pop .act-open { flex: 1; min-width: 0; justify-content: flex-start; gap: 10px; padding: 8px 10px; font-weight: 500; }
  .act-open.static { display: flex; align-items: center; }
  .act-ic { width: 30px; height: 30px; border-radius: 8px; display: grid; place-items: center; font-size: 16px; flex: none; }
  .act-ic.in { color: var(--blue); background: color-mix(in srgb, var(--blue) 15%, transparent); }
  .act-ic.out { color: var(--orange); background: rgba(var(--orange-rgb), .14); }
  .act-row.ok .act-ic { color: var(--green); background: color-mix(in srgb, var(--green) 15%, transparent); }
  .act-row.bad .act-ic { color: var(--red); background: color-mix(in srgb, var(--red) 14%, transparent); }
  .act-row.pause .act-ic { color: var(--text-mute); background: var(--hover-strong); }
  .act-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 5px; }
  .act-top { display: flex; gap: 8px; align-items: center; min-width: 0; color: var(--text); font-size: 12.5px; }
  .act-top b { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 600; }
  .act-top .st-tag { margin-left: auto; flex: none; }
  .act-from { color: var(--text-mute); font-size: 11px; margin-left: auto; white-space: nowrap; }
  .act-bar { display: block; height: 4px; border-radius: 99px; background: var(--bg-input); overflow: hidden; }
  .act-fill { display: block; height: 100%; background: var(--orange); transition: width .3s; }
  .act-fill.paused { background: var(--text-mute); }
  .act-fill.ind { animation: act-ind 1.2s ease-in-out infinite; }
  @keyframes act-ind { from { transform: translateX(-100%); } to { transform: translateX(340%); } }
  .act-sub { font-size: 11px; color: var(--text-mute); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .act-acts { display: flex; align-items: center; padding: 0 6px; }
  .act-pop .act-acts button { width: 28px; height: 28px; padding: 0; justify-content: center; background: var(--bg-input); }
  .act-pop .act-acts button:hover { background: var(--hover-strong); }
</style>
