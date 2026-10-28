<script lang="ts">
  // Картка «Збірку/сервер надіслано» у чаті. Одержувач бачить «Прийняти / Відмовитись», потім
  // живий прогрес із паузою і підсумок (з причиною); відправник — статус свого друга. Повний
  // вигляд — у ShareTransferModal («Деталі»). Уся логіка передачі в бекенді (share_shaurma.go).
  import { onMount } from 'svelte'
  import { App } from '../wails'
  import { toast } from '../toast.svelte'
  import { t } from '../i18n.svelte'
  import { tip } from './tooltip'
  import { shareIn, shareOut, openTransfer, dropOffer, errText, rememberPreviewSrc, openContents } from './shareStore.svelte'
  import { fmtBytes, fmtEta, pctOf, inReasonText, outReasonText, IN_ACTIVE, type ShareCardData } from './shareMsg'

  let { card, encoded, own, peerAccountId, peerName }: {
    card: ShareCardData
    encoded: string
    own: boolean
    peerAccountId: string
    peerName: string
  } = $props()

  // Той самий пристрій (інший акаунт у цьому ж лаунчері): прийняти власну передачу не можна.
  let devTag = $state('')
  onMount(() => { App.ShareDeviceTag().then((v) => (devTag = v || '')).catch(() => {}) })
  // Звідки брати «Що всередині» для цієї картки (одержувач).
  $effect(() => { if (!own) rememberPreviewSrc(card.sid, encoded, peerAccountId) })
  const sameDevice = $derived(!own && !!card.d && card.d === devTag)

  const inc = $derived(shareIn[card.sid])
  const out = $derived(shareOut[card.sid])
  const rec = $derived(out?.recipients?.find(r => r.id === peerAccountId))
  const isServer = $derived(card.k === 'server')
  let busy = $state(false)

  const pct = $derived(inc ? pctOf(inc.progress.bytesDone, inc.progress.bytesTotal || card.b) : 0)
  const eta = $derived(inc ? fmtEta((inc.progress.bytesTotal || card.b) - inc.progress.bytesDone, inc.progress.speedBps) : '')
  // Відправник скасував / минув термін — приймати вже нічого.
  const senderClosed = $derived(!!inc && inc.phase === 'cancelled' && ['cancelled_sender', 'timeout', 'sender_offline'].includes(inc.reason ?? ''))

  async function accept() {
    if (busy) return
    busy = true
    try {
      await App.ShareAccept(encoded, peerAccountId, peerName)
      dropOffer(card.sid)
    } catch (e) { toast(errText(e), 'error', 7000) }
    finally { busy = false }
  }
  async function decline() {
    if (busy) return
    busy = true
    try { await App.ShareDiscard(card.sid, encoded, peerAccountId); dropOffer(card.sid) }
    catch (e) { toast(errText(e), 'error') }
    finally { busy = false }
  }
  const pause = () => App.SharePause(card.sid).catch(() => {})
  const resume = () => App.ShareResume(card.sid).catch(() => {})
  const cancel = () => App.ShareCancelIn(card.sid).catch(() => {})
</script>

<article class="sh">
  <div class="sh-head">
    <div class="sh-ic"><i class="ti {isServer ? 'ti-server-2' : 'ti-package'}"></i></div>
    <div class="sh-titles">
      <div class="sh-kind">{t(isServer ? 'share.card.kindServer' : 'share.card.kindBuild')}</div>
      <div class="sh-name" title={card.n}>{card.n}</div>
      <div class="sh-meta">
        {#if card.m?.mc}<span>{card.m.mc}</span>{/if}
        {#if card.m?.loader && card.m.loader !== 'vanilla'}<span>{card.m.loader}</span>{/if}
        {#if card.m?.core}<span>{card.m.core}</span>{/if}
        <span>{t('share.card.files', { n: card.f })}</span><span>{fmtBytes(card.b)}</span>
      </div>
    </div>
    {#if !own && !sameDevice && inc?.phase !== 'declined'}
      <button class="sh-more" onclick={() => openContents(card.sid)} use:tip={t('share.contents.title')} aria-label={t('share.contents.title')}>
        <i class="ti ti-list-details"></i>
      </button>
    {/if}
    {#if (own && out) || (inc && inc.phase !== 'declined')}
      <button class="sh-more" onclick={() => openTransfer(own ? 'out' : 'in', card.sid)} use:tip={t('share.details')} aria-label={t('share.details')}>
        <i class="ti ti-arrows-maximize"></i>
      </button>
    {/if}
  </div>

  {#if own}
    <div class="sh-note">
      {#if !rec}<i class="ti ti-send"></i> {out?.phase === 'preparing' ? t('share.card.preparing') : t('share.card.sentTo', { name: peerName })}
      {:else if rec.offline && ['waiting', 'transferring', 'paused'].includes(rec.state)}<i class="ti ti-wifi-off"></i> {t('share.card.peerOffline', { name: peerName })}
      {:else if rec.state === 'waiting'}<i class="ti ti-hourglass"></i> {t('share.card.peerWaiting', { name: peerName })}
      {:else if rec.state === 'transferring'}<i class="ti ti-arrows-transfer-down"></i> {t('share.card.peerDownloading', { name: peerName, done: fmtBytes(rec.bytesDone), total: fmtBytes(card.b) })}
      {:else if rec.state === 'paused'}<i class="ti ti-player-pause"></i> {rec.pausedBy === 'sender' ? t('share.out.pausedByMe') : t('share.card.peerPaused', { name: peerName })}
      {:else if rec.state === 'done'}<span class="ok"><i class="ti ti-circle-check"></i> {t('share.card.peerDone', { name: peerName })}</span>
      {:else if rec.state === 'declined'}<span class="warn"><i class="ti ti-circle-x"></i> {t('share.card.peerDeclined', { name: peerName })}</span>
      {:else if rec.state === 'cancelled'}<span class="warn"><i class="ti ti-player-stop"></i> {t('share.card.peerStopped', { name: peerName })}</span>
      {:else if rec.state === 'revoked'}<i class="ti ti-ban"></i> {outReasonText(rec.reason || 'cancelled_sender')}
      {:else if rec.state === 'timeout'}<span class="warn"><i class="ti ti-clock-x"></i> {outReasonText(rec.reason || 'timeout')}</span>
      {:else}<span class="bad"><i class="ti ti-alert-triangle"></i> {outReasonText(rec.reason, rec.error)}</span>{/if}
    </div>
  {:else if sameDevice}
    <div class="sh-note bad"><i class="ti ti-ban"></i> {t('share.card.sameDevice')}</div>
  {:else if inc?.phase === 'declined'}
    <div class="sh-note"><i class="ti ti-circle-x"></i> {t('share.in.declined')}</div>
  {:else if senderClosed}
    <div class="sh-note warn"><i class="ti ti-ban"></i> {inReasonText(inc!.reason)}</div>
  {:else if !inc}
    <div class="sh-acts">
      <button class="sh-btn pri" onclick={accept} disabled={busy}>
        {#if busy}<i class="ti ti-loader-2 sh-spin"></i>{:else}<i class="ti ti-download"></i>{/if} {t('share.offer.accept')}
      </button>
      <button class="sh-btn" onclick={decline} disabled={busy}><i class="ti ti-x"></i> {t('share.offer.decline')}</button>
    </div>
  {:else if inc.phase === 'connecting' || inc.phase === 'verifying'}
    <div class="sh-note"><i class="ti ti-loader-2 sh-spin"></i> {inc.phase === 'verifying' ? t('share.in.verifying') : t('share.in.connecting', { name: peerName })}</div>
    <div class="sh-acts"><button class="sh-btn" onclick={cancel}><i class="ti ti-x"></i> {t('share.cancel')}</button></div>
  {:else if inc.phase === 'downloading' || inc.phase === 'paused'}
    <div class="sh-bar"><div class="sh-fill" class:paused={inc.phase === 'paused' || inc.progress.reconnecting} style="width:{pct}%"></div></div>
    <div class="sh-row">
      <span>{pct}% · {fmtBytes(inc.progress.bytesDone)} / {fmtBytes(inc.progress.bytesTotal || card.b)}</span>
      <span>
        {#if inc.phase === 'paused'}{inc.progress.pausedBy === 'sender' ? t('share.in.pausedSender', { name: peerName }) : t('share.in.paused')}
        {:else if inc.progress.reconnecting}{t('share.in.reconnectingShort')}
        {:else}{fmtBytes(inc.progress.speedBps)}/{t('share.unit.s')}{eta ? ` · ${eta}` : ''}{/if}
      </span>
    </div>
    <div class="sh-acts">
      {#if inc.phase === 'paused' && inc.progress.pausedBy !== 'sender'}
        <button class="sh-btn pri" onclick={resume}><i class="ti ti-player-play"></i> {t('share.resume')}</button>
      {:else if inc.phase === 'downloading'}
        <button class="sh-btn" onclick={pause}><i class="ti ti-player-pause"></i> {t('share.pause')}</button>
      {/if}
      <button class="sh-btn" onclick={cancel}><i class="ti ti-player-stop"></i> {t('share.in.stop')}</button>
    </div>
  {:else if inc.phase === 'installing'}
    <div class="sh-note"><i class="ti ti-loader-2 sh-spin"></i> {t('share.in.installing')}</div>
  {:else if inc.phase === 'done'}
    <div class="sh-note ok"><i class="ti ti-circle-check"></i> {t(isServer ? 'share.in.doneServer' : 'share.in.done')}</div>
  {:else}
    <div class="sh-note" class:bad={inc.phase === 'error'}>
      <i class="ti {inc.phase === 'error' ? 'ti-alert-triangle' : 'ti-player-stop'}"></i>
      {inReasonText(inc.reason || (inc.phase === 'cancelled' ? 'cancelled_receiver' : 'error'), inc.error)}
    </div>
    {#if !IN_ACTIVE.includes(inc.phase) && !['changed', 'access_closed'].includes(inc.reason ?? '')}
      <div class="sh-acts">
        <button class="sh-btn pri" onclick={accept} disabled={busy}><i class="ti ti-refresh"></i> {t(inc.phase === 'cancelled' ? 'share.in.continue' : 'share.in.retry')}</button>
        <button class="sh-btn" onclick={decline} disabled={busy}><i class="ti ti-trash"></i> {t('share.in.discard')}</button>
      </div>
    {/if}
  {/if}
</article>

<style>
  .sh { width: 100%; min-width: 0; border-radius: 14px; background: var(--bg-input); border: 1px solid var(--border-hi);
    padding: 12px; display: flex; flex-direction: column; gap: 10px; font-family: var(--font-ui); }
  .sh-head { display: flex; gap: 10px; align-items: center; }
  .sh-ic { width: 40px; height: 40px; border-radius: 10px; display: grid; place-items: center; font-size: 22px; color: var(--orange);
    background: rgba(var(--orange-rgb), .14); flex: none; }
  .sh-titles { min-width: 0; flex: 1; }
  .sh-kind { font-size: var(--fs-min); font-weight: 700; letter-spacing: .3px; color: var(--text-mute); text-transform: uppercase; }
  .sh-name { font-weight: 700; color: var(--text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .sh-meta { display: flex; flex-wrap: wrap; gap: 4px 10px; font-size: 11px; color: var(--text-mute); }
  .sh-more { width: 28px; height: 28px; border-radius: 8px; border: 1px solid var(--border-hi); background: var(--bg-panel); color: var(--text-mute);
    cursor: pointer; display: grid; place-items: center; flex: none; align-self: flex-start; }
  .sh-more:hover { color: var(--orange); border-color: var(--orange); }
  .sh-acts { display: flex; gap: 8px; flex-wrap: wrap; }
  .sh-btn { border: 1px solid var(--border-hi); background: var(--bg-panel); color: var(--text); border-radius: 9px; padding: 6px 12px;
    font: 600 12px var(--font-ui); cursor: pointer; display: inline-flex; gap: 6px; align-items: center; }
  .sh-btn:hover:not(:disabled) { background: var(--hover-strong); }
  .sh-btn:disabled { opacity: .5; cursor: default; }
  .sh-btn.pri { background: var(--orange); border-color: var(--orange); color: #fff; }
  .sh-bar { height: 8px; border-radius: 99px; background: var(--bg-panel); overflow: hidden; }
  .sh-fill { height: 100%; background: var(--orange); transition: width .3s; }
  .sh-fill.paused { background: var(--text-mute); }
  .sh-row { display: flex; justify-content: space-between; font-size: 11px; color: var(--text-mute); gap: 8px; }
  .sh-note { font-size: 12px; line-height: 1.4; color: var(--text-mute); display: flex; gap: 6px; align-items: flex-start; min-width: 0; overflow-wrap: anywhere; }
  .sh-note i { margin-top: 1px; flex: none; }
  .sh-note.ok, .sh-note .ok { color: var(--green); }
  .sh-note.bad, .sh-note .bad { color: var(--red); }
  .sh-note.warn, .sh-note .warn { color: var(--yellow); }
  .sh-spin { animation: sh-rot 1s linear infinite; }
  @keyframes sh-rot { to { transform: rotate(360deg); } }
</style>
