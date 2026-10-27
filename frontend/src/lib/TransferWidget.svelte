<script lang="ts">
  // Плаваючий віджет передач збірок (знизу по центру), видно з будь-якої сторінки. Компактний
  // рядок на кожну передачу — клік відкриває повне вікно (ShareTransferModal). Щойно завершені
  // передачі лишаються кілька секунд із підсумком, тож результат не губиться.
  // Тут же змонтовані модалки: пропозиція від друга, вікно передачі, «недокачане з минулого разу».
  import { App } from './wails'
  import { toast } from './toast.svelte'
  import { t } from './i18n.svelte'
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
  const visible = $derived(ins.length + outs.length > 0 || !!hiddenOffer)

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
</script>

{#if visible}
  <div class="tw" role="region" aria-label={t('share.widget.title')}>
    {#if hiddenOffer}
      <div class="tw-item offer">
        <button class="tw-open" onclick={() => (shareUi.offersHidden = false)} aria-label={t('share.details')}>
          <span class="tw-ic in"><i class="ti ti-gift"></i></span>
          <span class="tw-body">
            <span class="tw-top"><b title={hiddenOffer.card.n}>{hiddenOffer.card.n}</b><span class="tw-from">{t('share.widget.from', { name: hiddenOffer.fromName })}</span></span>
            <span class="tw-sub">{t('share.widget.offerWaiting')}{shareUi.offers.length > 1 ? ' · +' + (shareUi.offers.length - 1) : ''}</span>
          </span>
        </button>
      </div>
    {/if}
    {#each ins as v (v.id)}
      {@const pc = v.phase === 'done' ? 100 : pctOf(v.progress.bytesDone, v.progress.bytesTotal || v.bytes)}
      <div class="tw-item {tone(v.phase, v.reason)}">
        <button class="tw-open" onclick={() => openTransfer('in', v.id)} aria-label={t('share.details')}>
          <span class="tw-ic in"><i class="ti {v.phase === 'done' ? 'ti-circle-check' : v.phase === 'error' ? 'ti-alert-triangle' : 'ti-download'}"></i></span>
          <span class="tw-body">
            <span class="tw-top"><b title={v.name}>{v.name}</b><span class="tw-from">{t('share.widget.from', { name: v.from })}</span></span>
            {#if IN_ACTIVE.includes(v.phase) && v.phase !== 'connecting' && v.phase !== 'installing'}
              <span class="tw-bar"><span class="tw-fill" class:paused={v.phase === 'paused' || v.progress.reconnecting} style="width:{pc}%"></span></span>
            {/if}
            <span class="tw-sub">{IN_ACTIVE.includes(v.phase) && v.phase === 'downloading' ? pc + '% · ' : ''}{inLine(v)}</span>
          </span>
        </button>
        {#if v.phase === 'downloading' || v.phase === 'paused'}
          <div class="tw-acts">
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
      <div class="tw-item" class:ok={finished && o.phase !== 'error'} class:bad={o.phase === 'error'}>
        <button class="tw-open" onclick={() => openTransfer('out', o.id)} aria-label={t('share.details')}>
          <span class="tw-ic out"><i class="ti {finished ? 'ti-flag-check' : 'ti-upload'}"></i></span>
          <span class="tw-body">
            <span class="tw-top"><b title={o.name}>{o.name}</b>
              {#if (o.recipients?.length ?? 0) > 0}<span class="tw-from">{o.recipients.length === 1 ? t('share.widget.to', { name: friendName(o.recipients[0].id, o.names?.[o.recipients[0].id]) }) : t('share.widget.toN', { n: o.recipients.length })}</span>{/if}
            </span>
            {#if !finished}<span class="tw-bar"><span class="tw-fill" style="width:{outPct(o)}%"></span></span>{/if}
            <span class="tw-sub">{outLine(o)}</span>
          </span>
        </button>
      </div>
    {/each}
  </div>
{/if}

<ShareRecoveryModal />
<ShareTransferModal />
<ShareOfferModal />
<ShareContentsModal />

<style>
  .tw { position: fixed; left: 50%; bottom: 18px; transform: translateX(-50%); z-index: 2000000; display: flex; flex-direction: column;
    gap: 8px; width: min(420px, calc(100vw - 32px)); font-family: var(--font-ui); }
  .tw-item { position: relative; display: flex; align-items: stretch; background: var(--bg-panel); border: 1px solid var(--border-hi); border-radius: 14px;
    box-shadow: 0 10px 30px rgba(0,0,0,.45); overflow: hidden; animation: tw-in .22s ease; }
  .tw-item.ok { border-color: color-mix(in srgb, var(--green) 55%, var(--border-hi)); }
  .tw-item.bad { border-color: color-mix(in srgb, var(--red) 55%, var(--border-hi)); }
  .tw-item.warn { border-color: color-mix(in srgb, var(--yellow) 55%, var(--border-hi)); }
  .tw-item.offer { border-color: color-mix(in srgb, var(--blue) 55%, var(--border-hi)); }
  .tw-open { flex: 1; min-width: 0; display: flex; gap: 10px; align-items: center; padding: 10px 12px; background: transparent; border: 0; color: inherit;
    text-align: left; cursor: pointer; font: inherit; }
  .tw-open:hover { background: var(--hover); }
  .tw-ic { width: 32px; height: 32px; border-radius: 9px; display: grid; place-items: center; font-size: 17px; flex: none; }
  .tw-ic.in { color: var(--blue); background: color-mix(in srgb, var(--blue) 15%, transparent); }
  .tw-ic.out { color: var(--orange); background: rgba(var(--orange-rgb), .14); }
  .tw-item.ok .tw-ic { color: var(--green); background: color-mix(in srgb, var(--green) 15%, transparent); }
  .tw-item.bad .tw-ic { color: var(--red); background: color-mix(in srgb, var(--red) 14%, transparent); }
  .tw-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 5px; }
  .tw-top { display: flex; gap: 8px; align-items: baseline; min-width: 0; color: var(--text); font-size: 12.5px; }
  .tw-top b { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .tw-from { color: var(--text-mute); font-size: 11px; margin-left: auto; white-space: nowrap; }
  .tw-bar { display: block; height: 5px; border-radius: 99px; background: var(--bg-input); overflow: hidden; }
  .tw-fill { display: block; height: 100%; background: var(--orange); transition: width .3s; }
  .tw-fill.paused { background: var(--text-mute); }
  .tw-sub { font-size: 11px; color: var(--text-mute); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .tw-acts { display: flex; align-items: center; padding-right: 8px; }
  .tw-acts button { border: 0; background: var(--bg-input); color: var(--text); width: 28px; height: 28px; border-radius: 8px; cursor: pointer; }
  .tw-acts button:hover { background: var(--hover-strong); }
  @keyframes tw-in { from { opacity: 0; transform: translateY(8px); } to { opacity: 1; transform: none; } }
</style>
