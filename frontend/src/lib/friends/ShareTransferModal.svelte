<script lang="ts">
  // Вікно однієї передачі збірки/сервера — для відправника (стан кожного друга) і для
  // одержувача (прогрес, швидкість, ETA, пауза/зупинка, підсумок). Відкривається з віджета
  // передач, картки в чаті, тосту або одразу після «Надіслати». Уся логіка — у бекенді
  // (share_shaurma.go); тут лише відображення стану зі shareStore та виклики App.Share*.
  import UiModal from '../UiModal.svelte'
  import FriendPickerModal from './FriendPickerModal.svelte'
  import FriendAvatar from './FriendAvatar.svelte'
  import { friendColor } from './friendHead'
  import { App } from '../wails'
  import { toast } from '../toast.svelte'
  import { t } from '../i18n.svelte'
  import { shareIn, shareOut, shareUi, shareFriends, closeTransfer, friendName, errText, canPreview, openContents } from './shareStore.svelte'
  import {
    fmtBytes, fmtEta, pctOf, inReasonText, outReasonText, IN_ACTIVE, REC_FINAL,
    type ShareRecipientState,
  } from './shareMsg'
  import type { SocialFriend } from '../types'

  const Z = '2150000'

  const inc = $derived(shareUi.view?.dir === 'in' ? shareIn[shareUi.view.id] : undefined)
  const out = $derived(shareUi.view?.dir === 'out' ? shareOut[shareUi.view.id] : undefined)
  const open = $derived(!!(inc || out))
  let busy = $state(false)

  const err = (e: any) => toast(errText(e), 'error')
  async function act(fn: () => Promise<unknown>) {
    if (busy) return
    busy = true
    try { await fn() } catch (e) { err(e) } finally { busy = false }
  }

  // ── Одержувач ──
  const inPct = $derived(inc ? pctOf(inc.progress.bytesDone, inc.progress.bytesTotal || inc.bytes) : 0)
  const inTotal = $derived(inc ? (inc.progress.bytesTotal || inc.bytes) : 0)
  const inEta = $derived(inc && inc.phase === 'downloading' && !inc.progress.reconnecting
    ? fmtEta(inTotal - inc.progress.bytesDone, inc.progress.speedBps) : '')
  const inActive = $derived(!!inc && IN_ACTIVE.includes(inc.phase))
  const inRetryable = $derived(!!inc && (inc.phase === 'error' || inc.phase === 'cancelled') &&
    !['cancelled_sender', 'timeout', 'changed', 'access_closed', 'declined'].includes(inc.reason ?? ''))

  function inStatus(): { icon: string; tone: string; text: string } {
    const v = inc!
    switch (v.phase) {
      case 'connecting': return { icon: 'ti-plug-connected', tone: 'busy', text: t('share.in.connecting', { name: v.from }) }
      case 'verifying': return { icon: 'ti-shield-check', tone: 'busy', text: t('share.in.verifying') }
      case 'installing': return { icon: 'ti-package-import', tone: 'busy', text: t('share.in.installing') }
      case 'paused': return { icon: 'ti-player-pause', tone: 'pause', text: v.progress.pausedBy === 'sender' ? t('share.in.pausedSender', { name: v.from }) : t('share.in.paused') }
      case 'downloading':
        if (v.progress.reconnecting) return { icon: 'ti-wifi-off', tone: 'warn', text: t('share.in.reconnecting', { name: v.from }) }
        return { icon: 'ti-download', tone: 'busy', text: t('share.in.downloading') }
      case 'done': return { icon: 'ti-circle-check', tone: 'ok', text: t(v.kind === 'server' ? 'share.in.doneServer' : 'share.in.done') }
      case 'declined': return { icon: 'ti-circle-x', tone: 'mute', text: t('share.in.declined') }
      case 'cancelled': return { icon: 'ti-player-stop', tone: v.reason === 'cancelled_receiver' ? 'mute' : 'warn', text: inReasonText(v.reason || 'cancelled_receiver') }
      default: return { icon: 'ti-alert-triangle', tone: 'bad', text: inReasonText(v.reason, v.error) }
    }
  }

  // ── Відправник ──
  function recStatus(r: ShareRecipientState): { icon: string; tone: string; text: string } {
    if (!REC_FINAL.includes(r.state) && r.offline) return { icon: 'ti-wifi-off', tone: 'warn', text: t('share.out.offline') }
    switch (r.state) {
      case 'waiting': return { icon: 'ti-hourglass', tone: 'mute', text: t('share.out.waiting') }
      case 'transferring': return { icon: 'ti-arrows-transfer-down', tone: 'busy', text: t('share.out.transferring') }
      case 'paused': return { icon: 'ti-player-pause', tone: 'pause', text: r.pausedBy === 'sender' ? t('share.out.pausedByMe') : t('share.out.pausedByFriend') }
      case 'done': return { icon: 'ti-circle-check', tone: 'ok', text: t('share.out.done') }
      case 'declined': return { icon: 'ti-circle-x', tone: 'warn', text: outReasonText('declined') }
      case 'cancelled': return { icon: 'ti-player-stop', tone: 'warn', text: outReasonText('cancelled_receiver') }
      case 'revoked': return { icon: 'ti-ban', tone: 'mute', text: outReasonText(r.reason || 'cancelled_sender') }
      case 'timeout': return { icon: 'ti-clock-x', tone: 'warn', text: outReasonText(r.reason || 'timeout') }
      default: return { icon: 'ti-alert-triangle', tone: 'bad', text: outReasonText(r.reason, r.error) }
    }
  }
  const outActive = $derived(!!out && (out.phase === 'preparing' || out.phase === 'ready'))
  const outTotals = $derived.by(() => {
    if (!out) return { done: 0, ok: 0, n: 0, speed: 0 }
    const rs = out.recipients ?? []
    return {
      n: rs.length,
      ok: rs.filter(r => r.state === 'done').length,
      done: rs.filter(r => REC_FINAL.includes(r.state)).length,
      speed: rs.reduce((s, r) => s + (r.state === 'transferring' ? r.speedBps || 0 : 0), 0),
    }
  })
  const hashPct = $derived(out ? pctOf(out.hashed, out.hashTotal) : 0)

  // Скасування/зупинка: вікно одразу закривається, тост каже, що сталось (бекенд ще кілька
  // секунд закриває доступ — це видно у віджеті, а не у «завислому» вікні).
  async function closeOut() {
    if (!out || busy) return
    const name = out.name
    const many = (out.recipients ?? []).some(r => !REC_FINAL.includes(r.state))
    busy = true
    try {
      await App.ShareStop(out.id)
      closeTransfer()
      toast(t(many ? 'share.toast.outCancelled' : 'share.toast.outClosed', { name }), 'info')
    } catch (e) { err(e) } finally { busy = false }
  }
  async function stopIn() {
    if (!inc || busy) return
    const name = inc.name
    busy = true
    try {
      await App.ShareCancelIn(inc.id)
      closeTransfer()
      toast(t('share.toast.inStoppedByMe', { name }), 'info', 6000)
    } catch (e) { err(e) } finally { busy = false }
  }

  // ── «Додати друга» в уже відкриту передачу (бекенд: ShareInviteMore, ліміт — 3 разом) ──
  const MAX_RECIPIENTS = 3
  let addOpen = $state(false)
  let adding = $state(false)
  const slotsLeft = $derived(out ? MAX_RECIPIENTS - (out.recipients?.length ?? 0) : 0)
  const canAdd = $derived(!!out && out.phase === 'ready' && slotsLeft > 0)
  // Ті, кому вже надіслано (будь-який стан), — у списку приглушені й не вибираються.
  const already = $derived(new Set((out?.recipients ?? []).map(r => r.id.toLowerCase())))

  async function addFriends(friends: SocialFriend[]) {
    if (!out || adding || friends.length === 0) return
    if (friends.length > slotsLeft) {
      toast(t('share.out.addLimit', { n: slotsLeft }), 'error')
      return
    }
    adding = true
    try {
      await App.ShareInviteMore(out.id, friends.map(f => f.accountId))
      toast(friends.length === 1
        ? t('share.out.addedOne', { name: friends[0].nickname })
        : t('share.out.addedMany', { n: friends.length }), 'success')
      addOpen = false
    } catch (e) {
      err(e) // вибір лишається — можна виправити й спробувати ще раз
    } finally { adding = false }
  }
</script>

{#if open}
  <UiModal
    open={true}
    zIndex={Z}
    width="min(560px, 94vw)"
    icon={(inc?.kind ?? out?.kind) === 'server' ? 'ti-server-2' : 'ti-package'}
    color={inc ? 'var(--blue)' : 'var(--orange)'}
    title={inc ? t('share.modal.inTitle', { name: inc.name }) : t('share.modal.outTitle', { name: out!.name })}
    subtitle={inc ? t('share.modal.inSub', { name: inc.from }) : t((out!.kind === 'server') ? 'share.modal.outSubServer' : 'share.modal.outSub')}
    onclose={closeTransfer}
  >
    {#if inc}
      {@const st = inStatus()}
      <div class="st-wrap">
        <div class="st-status {st.tone}">
          <i class="ti {st.icon}" class:st-spin={st.tone === 'busy' && inc.phase !== 'downloading'}></i>
          <span>{st.text}</span>
        </div>
        {#if inc.phase !== 'declined' && (inActive || inc.progress.bytesDone > 0 || inc.phase === 'done')}
          <div class="st-bar"><div class="st-fill {st.tone}" style="width:{inc.phase === 'done' ? 100 : inPct}%"></div></div>
          <div class="st-grid">
            <div><span>{t('share.stat.received')}</span><b>{fmtBytes(inc.phase === 'done' ? inTotal : inc.progress.bytesDone)} / {fmtBytes(inTotal)}</b></div>
            <div><span>{t('share.stat.files')}</span><b>{inc.phase === 'done' ? (inc.progress.filesTotal || inc.files) : inc.progress.filesDone} / {inc.progress.filesTotal || inc.files}</b></div>
            {#if inc.phase === 'downloading' && !inc.progress.reconnecting}
              <div><span>{t('share.stat.speed')}</span><b>{fmtBytes(inc.progress.speedBps)}/{t('share.unit.s')}</b></div>
              <div><span>{t('share.stat.eta')}</span><b>{inEta || '—'}</b></div>
            {/if}
            {#if (inc.progress.cdnBytes ?? 0) > 0}
              <div title={t('share.contents.cdnTip')}><span>{t('share.stat.cdn')}</span><b class="st-cdn"><i class="ti ti-bolt"></i> {fmtBytes(inc.progress.cdnBytes ?? 0)}</b></div>
            {/if}
          </div>
          {#if inc.progress.current && inc.phase === 'downloading'}
            <div class="st-cur" title={inc.progress.current}>{inc.progress.current}</div>
          {/if}
        {/if}
        {#if inc.phase === 'error' && inc.error && inc.reason !== 'error'}
          <div class="st-detail">{inc.error}</div>
        {/if}
        {#if inc.phase === 'cancelled' && inc.reason !== 'declined'}
          <div class="st-hint"><i class="ti ti-info-circle"></i> {t('share.in.keptHint')}</div>
        {:else if inc.phase === 'error' && inc.reason === 'disk_full'}
          <div class="st-hint"><i class="ti ti-info-circle"></i> {t('share.in.diskHint')}</div>
        {/if}
      </div>
    {:else if out}
      <div class="st-wrap">
        {#if out.phase === 'preparing'}
          <div class="st-status busy"><i class="ti ti-loader-2 st-spin"></i><span>{out.resolving ? t('share.out.resolving') : t('share.out.preparing', { done: out.hashed, total: out.hashTotal || '…' })}</span></div>
          <div class="st-bar"><div class="st-fill busy" style="width:{hashPct}%"></div></div>
        {:else}
          <div class="st-sum">
            <span><i class="ti ti-files"></i> {t('share.out.summary', { files: out.files, size: fmtBytes(out.totalBytes) })}</span>
            {#if outActive && outTotals.speed > 0}<span><i class="ti ti-gauge"></i> {fmtBytes(outTotals.speed)}/{t('share.unit.s')}</span>{/if}
            {#if outTotals.n > 1}<span><i class="ti ti-users"></i> {t('share.out.groupDone', { ok: outTotals.ok, n: outTotals.n })}</span>{/if}
          </div>
          {#if (out.cdnFiles ?? 0) > 0}
            <div class="st-cdnchip" title={t('share.contents.cdnTip')}>
              <i class="ti ti-bolt"></i>
              <b>{t('share.out.cdn', { n: out.cdnFiles ?? 0, size: fmtBytes(out.cdnBytes ?? 0) })}</b>
              <span>{t('share.out.cdnHint')}</span>
            </div>
          {/if}
          <div class="st-recs">
            {#each out.recipients ?? [] as r (r.id)}
              {@const rs = recStatus(r)}
              {@const p = r.state === 'done' ? 100 : pctOf(r.bytesDone, out.totalBytes)}
              {@const who = friendName(r.id, out.names?.[r.id])}
              {@const fr = shareFriends[r.id]}
              {@const pst = fr?.presence?.status ?? 'offline'}
              <div class="st-rec">
                <FriendAvatar nickname={who} color={fr?.color ?? ''} status={pst} size={38} />
                <div class="st-rmain">
                  <div class="st-rtop">
                    <span class="st-who">
                      <b title={who} style="color:{friendColor(fr?.color ?? '', 'var(--text)')}">{who}</b>
                      <span class="st-pres {pst}">{t('friendPicker.status.' + pst)}{#if pst === 'in_game' && fr?.presence?.packName} · {fr.presence.packName}{/if}</span>
                    </span>
                    <span class="st-badge {rs.tone}"><i class="ti {rs.icon}"></i> {rs.text}</span>
                  </div>
                  {#if r.state !== 'waiting' && r.state !== 'declined' && !(r.state === 'timeout' && r.reason === 'timeout')}
                    <div class="st-bar sm"><div class="st-fill {rs.tone}" style="width:{p}%"></div></div>
                    <div class="st-rsub">
                      <span>{p}% · {fmtBytes(r.state === 'done' ? out.totalBytes : r.bytesDone)} / {fmtBytes(out.totalBytes)}</span>
                      {#if r.state === 'transferring' && !r.offline && r.speedBps > 0}
                        <span>{fmtBytes(r.speedBps)}/{t('share.unit.s')} · {fmtEta(out.totalBytes - r.bytesDone, r.speedBps) || '—'}</span>
                      {/if}
                    </div>
                  {/if}
                  {#if r.state === 'failed' && r.error && r.reason !== 'error'}<div class="st-detail">{r.error}</div>{/if}
                </div>
                {#if outActive && !REC_FINAL.includes(r.state)}
                  <div class="st-racts">
                    {#if r.state === 'transferring' || (r.state === 'paused' && r.pausedBy === 'sender')}
                      <button class="st-ic" disabled={busy} aria-label={r.state === 'transferring' ? t('share.pause') : t('share.resume')}
                        title={r.state === 'transferring' ? t('share.pause') : t('share.resume')}
                        onclick={() => act(() => App.ShareSenderPause(out.id, r.id, r.state === 'transferring'))}>
                        <i class="ti {r.state === 'transferring' ? 'ti-player-pause' : 'ti-player-play'}"></i>
                      </button>
                    {/if}
                    <button class="st-ic bad" disabled={busy} aria-label={t('share.out.cancelFor')} title={t('share.out.cancelFor')}
                      onclick={() => act(() => App.ShareRevokeRecipient(out.id, r.id))}><i class="ti ti-x"></i></button>
                  </div>
                {/if}
              </div>
            {:else}
              <div class="st-hint"><i class="ti ti-hourglass"></i> {t('share.out.noRecipients')}</div>
            {/each}
          </div>
          {#if out.skipped?.length}
            <div class="st-hint"><i class="ti ti-shield-x"></i> {t('share.out.skipped', { n: out.skipped.length })}</div>
          {/if}
          {#if out.error}<div class="st-detail">{out.error}</div>{/if}
          {#if out.phase === 'closed' && out.reason}
            <div class="st-hint"><i class="ti ti-info-circle"></i> {out.reason === 'cancelled_sender' ? t('share.out.closedByMe') : t('share.out.closedWhy', { why: outReasonText(out.reason) })}</div>
          {/if}
        {/if}
      </div>
    {/if}

    {#snippet footer()}
      {#if inc}
        {#if inActive && inc.phase !== 'installing'}
          {#if canPreview(inc.id)}
            <button class="btn btn-ghost" onclick={() => openContents(inc.id)}><i class="ti ti-list-details"></i> {t('share.contents.title')}</button>
          {/if}
          {#if inc.phase === 'paused' && inc.progress.pausedBy !== 'sender'}
            <button class="btn btn-ghost" disabled={busy} onclick={() => act(() => App.ShareResume(inc.id))}><i class="ti ti-player-play"></i> {t('share.resume')}</button>
          {:else if inc.phase === 'downloading'}
            <button class="btn btn-ghost" disabled={busy} onclick={() => act(() => App.SharePause(inc.id))}><i class="ti ti-player-pause"></i> {t('share.pause')}</button>
          {/if}
          <button class="btn btn-danger" disabled={busy} onclick={stopIn}><i class="ti ti-player-stop"></i> {t('share.in.stop')}</button>
          <button class="btn btn-primary" onclick={closeTransfer}>{t('share.hide')}</button>
        {:else}
          {#if inRetryable}
            <button class="btn btn-ghost" disabled={busy} onclick={() => act(() => App.ShareResumeRecovered(inc.mid))}><i class="ti ti-refresh"></i> {t('share.in.retry')}</button>
          {/if}
          {#if canPreview(inc.id) && inc.phase !== 'declined'}
            <button class="btn btn-ghost" onclick={() => openContents(inc.id)}><i class="ti ti-list-details"></i> {t('share.contents.title')}</button>
          {/if}
          <button class="btn btn-primary" onclick={closeTransfer}>{t('share.close')}</button>
        {/if}
      {:else if out}
        {#if outActive}
          {#if out.phase === 'ready'}
            <button class="btn btn-ghost" disabled={busy || !canAdd} onclick={() => (addOpen = true)}
              title={canAdd ? t('share.out.addFriendTip', { n: slotsLeft }) : t('share.out.addFull', { max: MAX_RECIPIENTS })}>
              <i class="ti ti-user-plus"></i> {t('share.out.addFriend')}
            </button>
          {/if}
          <button class="btn btn-danger" disabled={busy} onclick={closeOut}><i class="ti ti-player-stop"></i> {t(outTotals.n > 1 ? 'share.out.stopAll' : 'share.out.stop')}</button>
          <button class="btn btn-primary" onclick={closeTransfer}>{t('share.hide')}</button>
        {:else}
          <button class="btn btn-primary" onclick={closeTransfer}>{t('share.close')}</button>
        {/if}
      {/if}
    {/snippet}
  </UiModal>
{/if}

{#if addOpen && out && canAdd}
  <FriendPickerModal
    title={t('share.out.addTitle', { name: out.name })}
    subtitle={t('share.out.addSub', { n: slotsLeft })}
    multi={true}
    max={slotsLeft}
    exclude={already}
    excludedLabel={t('share.out.addAlready')}
    busy={adding}
    zIndex={Number(Z) + 10}
    confirmLabel={t('share.send.send')}
    onconfirm={addFriends}
    onclose={() => { if (!adding) addOpen = false }}
  />
{/if}

<style>
  .st-wrap { display: flex; flex-direction: column; gap: 12px; }
  .st-status { display: flex; align-items: center; gap: 10px; padding: 11px 13px; border-radius: 12px; font-size: 13px; font-weight: 700;
    background: var(--bg-input); border: 1px solid var(--border); color: var(--text); }
  .st-status > i { font-size: 18px; flex: none; }
  .st-status.busy > i { color: var(--blue); }
  .st-status.ok { border-color: color-mix(in srgb, var(--green) 45%, transparent); background: color-mix(in srgb, var(--green) 10%, var(--bg-input)); }
  .st-status.ok > i { color: var(--green); }
  .st-status.bad { border-color: color-mix(in srgb, var(--red) 45%, transparent); background: color-mix(in srgb, var(--red) 9%, var(--bg-input)); }
  .st-status.bad > i { color: var(--red); }
  .st-status.warn > i, .st-status.pause > i { color: var(--yellow); }
  .st-status.mute > i { color: var(--text-mute); }

  .st-bar { height: 8px; border-radius: 99px; background: var(--bg-input); overflow: hidden; border: 1px solid var(--border); }
  .st-bar.sm { height: 6px; }
  .st-fill { height: 100%; background: linear-gradient(90deg, var(--orange), color-mix(in srgb, var(--orange) 60%, #ffd166)); transition: width .35s ease; }
  .st-fill.ok { background: var(--green); }
  .st-fill.pause, .st-fill.warn, .st-fill.mute { background: var(--text-mute); }
  .st-fill.bad { background: var(--red); }

  .st-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(110px, 1fr)); gap: 8px; }
  .st-grid > div { background: var(--bg-input); border: 1px solid var(--border); border-radius: 10px; padding: 8px 10px; display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .st-grid span { font-size: 10.5px; font-weight: 700; text-transform: uppercase; letter-spacing: .3px; color: var(--text-mute); }
  .st-grid b { font-size: 13px; color: var(--text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .st-cdn { color: var(--green) !important; }
  .st-cur { font-size: 11px; color: var(--text-mute); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; font-family: var(--font-mono, monospace); }
  .st-detail { font-size: 11.5px; color: var(--text-mute); overflow-wrap: anywhere; }
  .st-hint { display: flex; gap: 7px; align-items: flex-start; font-size: 12.5px; color: var(--text); line-height: 1.45; }
  .st-hint > i { margin-top: 2px; color: var(--text-mute); flex: none; }

  .st-sum { display: flex; flex-wrap: wrap; gap: 6px 14px; font-size: 12.5px; font-weight: 700; color: var(--text); }
  .st-sum i { color: var(--orange); margin-right: 3px; }
  .st-recs { display: flex; flex-direction: column; gap: 8px; max-height: min(340px, 50vh); overflow-y: auto; }
  .st-rec { display: flex; gap: 10px; align-items: center; padding: 10px; border-radius: 12px; background: var(--bg-input); border: 1px solid var(--border); }
  .st-who { display: flex; flex-direction: column; min-width: 0; }
  .st-pres { font-size: 11px; font-weight: 600; color: var(--text-mute); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .st-pres.online { color: #22c55e; }
  .st-pres.in_game { color: var(--orange); }
  .st-pres.away { color: var(--yellow); }
  .st-cdnchip { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; padding: 8px 11px; border-radius: 10px;
    background: color-mix(in srgb, var(--green) 12%, var(--bg-input)); border: 1px solid color-mix(in srgb, var(--green) 40%, transparent); }
  .st-cdnchip > i { color: var(--green); font-size: 15px; }
  .st-cdnchip b { font-size: 12.5px; color: var(--text); }
  .st-cdnchip span { font-size: 11.5px; color: var(--text-mute); }
  .st-rmain { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 5px; }
  .st-rtop { display: flex; align-items: center; justify-content: space-between; gap: 8px; min-width: 0; }
  .st-rtop b { font-size: 13px; color: var(--text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .st-rsub { display: flex; justify-content: space-between; gap: 8px; font-size: 11px; color: var(--text-mute); }
  .st-badge { display: inline-flex; align-items: center; gap: 4px; font-size: 11px; font-weight: 700; border-radius: 99px; padding: 3px 8px; white-space: nowrap;
    background: var(--hover); color: var(--text-mute); flex: none; max-width: 60%; overflow: hidden; text-overflow: ellipsis; }
  .st-badge.ok { color: var(--green); background: color-mix(in srgb, var(--green) 14%, transparent); }
  .st-badge.bad { color: var(--red); background: color-mix(in srgb, var(--red) 13%, transparent); }
  .st-badge.warn, .st-badge.pause { color: var(--yellow); background: color-mix(in srgb, var(--yellow) 14%, transparent); }
  .st-badge.busy { color: var(--blue); background: color-mix(in srgb, var(--blue) 14%, transparent); }
  .st-racts { display: flex; flex-direction: column; gap: 4px; flex: none; }
  .st-ic { width: 28px; height: 28px; border-radius: 8px; border: 1px solid var(--border); background: var(--bg-panel); color: var(--text-mute); cursor: pointer;
    display: grid; place-items: center; font-size: 14px; transition: all .14s; }
  .st-ic:hover:not(:disabled) { color: var(--orange); border-color: var(--orange); }
  .st-ic.bad:hover:not(:disabled) { color: var(--red); border-color: var(--red); }
  .st-ic:disabled { opacity: .5; cursor: default; }
  .st-spin { animation: st-rot 1s linear infinite; }
  @keyframes st-rot { to { transform: rotate(360deg); } }
</style>
