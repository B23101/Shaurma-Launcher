<script lang="ts">
  // Картка «Збірку/сервер надіслано» у чаті. Одержувач бачить «Прийняти / Відмовитись», потім
  // живий прогрес із паузою; відправник — статус свого друга. Уся логіка передачі в бекенді
  // (share_shaurma.go), тут лише розмітка й виклики.
  import { onMount } from 'svelte'
  import { App } from '../wails'
  import { toast } from '../toast.svelte'
  import { tip } from './tooltip'
  import { shareIn, shareOut } from './shareStore.svelte'
  import { fmtBytes, fmtEta, type ShareCardData } from './shareMsg'

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
  const sameDevice = $derived(!own && !!card.d && card.d === devTag)

  const inc = $derived(shareIn[card.sid])
  const out = $derived(Object.values(shareOut).find(o => o.id === card.sid))
  const rec = $derived(out?.recipients?.find(r => r.id === peerAccountId))
  const isServer = $derived(card.k === 'server')
  let busy = $state(false)

  const pct = $derived(inc && inc.progress.bytesTotal > 0 ? Math.min(100, Math.floor(inc.progress.bytesDone * 100 / inc.progress.bytesTotal)) : 0)
  const eta = $derived(inc ? fmtEta(inc.progress.bytesTotal - inc.progress.bytesDone, inc.progress.speedBps) : '')

  async function accept() {
    if (busy) return
    busy = true
    try { await App.ShareAccept(encoded, peerAccountId, peerName) }
    catch (e: any) { toast(String(e?.message ?? e), 'error') }
    finally { busy = false }
  }
  async function decline() {
    try { await App.ShareDiscard(card.sid, encoded) } catch (e: any) { toast(String(e?.message ?? e), 'error') }
  }
  const pause = () => App.SharePause(card.sid).catch(() => {})
  const resume = () => App.ShareResume(card.sid).catch(() => {})
  const cancel = () => App.ShareCancelIn(card.sid).catch(() => {})
</script>

<article class="sh">
  <div class="sh-head">
    <div class="sh-ic"><i class="ti {isServer ? 'ti-server-2' : 'ti-package'}"></i></div>
    <div class="sh-titles">
      <div class="sh-kind">{isServer ? 'Сервер' : 'Збірка'} · передача</div>
      <div class="sh-name" title={card.n}>{card.n}</div>
      <div class="sh-meta">
        {#if card.m?.mc}<span>{card.m.mc}</span>{/if}
        {#if card.m?.loader && card.m.loader !== 'vanilla'}<span>{card.m.loader}</span>{/if}
        {#if card.m?.core}<span>{card.m.core}</span>{/if}
        <span>{card.f} файлів</span><span>{fmtBytes(card.b)}</span>
      </div>
    </div>
  </div>

  {#if own}
    <div class="sh-note">
      {#if !rec}<i class="ti ti-send"></i> Надіслано {peerName}
      {:else if rec.state === 'waiting'}<i class="ti ti-hourglass"></i> {peerName} ще не прийняв(ла)
      {:else if rec.state === 'transferring'}<i class="ti ti-arrows-transfer-down"></i> {peerName} качає · {fmtBytes(rec.bytesDone)} / {fmtBytes(card.b)}
      {:else if rec.state === 'paused'}<i class="ti ti-player-pause"></i> Пауза ({rec.pausedBy === 'sender' ? 'вами' : peerName})
      {:else if rec.state === 'done'}<i class="ti ti-circle-check"></i> {peerName} отримав(ла) повністю
      {:else if rec.state === 'declined'}<i class="ti ti-circle-x"></i> {peerName} відмовився(лась)
      {:else if rec.state === 'revoked'}<i class="ti ti-ban"></i> Доступ закрито
      {:else}<i class="ti ti-alert-triangle"></i> Помилка{rec.error ? `: ${rec.error}` : ''}{/if}
    </div>
  {:else if sameDevice}
    <div class="sh-note bad"><i class="ti ti-ban"></i> Це передача з цього самого лаунчера (інший акаунт на цьому ПК) — прийняти тут не можна</div>
  {:else if !inc || inc.phase === 'declined'}
    <div class="sh-acts">
      <button class="sh-btn pri" onclick={accept} disabled={busy || inc?.phase === 'declined'}>
        {#if busy}<i class="ti ti-loader-2 sh-spin"></i>{:else}<i class="ti ti-download"></i>{/if} Прийняти
      </button>
      <button class="sh-btn" onclick={decline} disabled={inc?.phase === 'declined'}><i class="ti ti-x"></i> Відмовитись</button>
    </div>
    {#if inc?.phase === 'declined'}<div class="sh-note"><i class="ti ti-circle-x"></i> Ви відмовились</div>{/if}
  {:else if inc.phase === 'connecting'}
    <div class="sh-note"><i class="ti ti-loader-2 sh-spin"></i> Підключення до {peerName}…</div>
    <div class="sh-acts"><button class="sh-btn" onclick={cancel}><i class="ti ti-x"></i> Скасувати</button></div>
  {:else if inc.phase === 'downloading' || inc.phase === 'paused'}
    <div class="sh-bar"><div class="sh-fill" class:paused={inc.phase === 'paused'} style="width:{pct}%"></div></div>
    <div class="sh-row">
      <span>{pct}% · {fmtBytes(inc.progress.bytesDone)} / {fmtBytes(inc.progress.bytesTotal)}</span>
      <span>
        {#if inc.phase === 'paused'}пауза{inc.progress.pausedBy === 'sender' ? ' (відправник)' : ''}
        {:else}{fmtBytes(inc.progress.speedBps)}/с{eta ? ` · ${eta}` : ''}{/if}
      </span>
    </div>
    <div class="sh-acts">
      {#if inc.phase === 'paused' && inc.progress.pausedBy !== 'sender'}
        <button class="sh-btn pri" onclick={resume}><i class="ti ti-player-play"></i> Продовжити</button>
      {:else if inc.phase === 'downloading'}
        <button class="sh-btn" onclick={pause} use:tip={'Пауза'}><i class="ti ti-player-pause"></i> Пауза</button>
      {/if}
      <button class="sh-btn" onclick={cancel}><i class="ti ti-x"></i> Зупинити</button>
    </div>
  {:else if inc.phase === 'installing'}
    <div class="sh-note"><i class="ti ti-loader-2 sh-spin"></i> Встановлення…</div>
  {:else if inc.phase === 'done'}
    <div class="sh-note ok"><i class="ti ti-circle-check"></i> {isServer ? 'Сервер додано до вашого списку' : 'Збірку додано до вашого списку'}</div>
  {:else}
    <div class="sh-note bad"><i class="ti ti-alert-triangle"></i> {inc.error || (inc.phase === 'cancelled' ? 'Зупинено — прийняте збережено' : 'Помилка')}</div>
    <div class="sh-acts">
      <button class="sh-btn pri" onclick={accept} disabled={busy}><i class="ti ti-refresh"></i> Спробувати знову</button>
    </div>
  {/if}
</article>

<style>
  .sh { width: 100%; min-width: 0; border-radius: 14px; background: var(--bg-input); border: 1px solid var(--border-hi);
    padding: 12px; display: flex; flex-direction: column; gap: 10px; font-family: var(--font-ui); }
  .sh-head { display: flex; gap: 10px; align-items: center; }
  .sh-ic { width: 40px; height: 40px; border-radius: 10px; display: grid; place-items: center; font-size: 22px; color: var(--orange);
    background: rgba(var(--orange-rgb), .14); flex: none; }
  .sh-titles { min-width: 0; flex: 1; }
  .sh-kind { font-size: 10px; font-weight: 700; letter-spacing: .3px; color: var(--text-mute); text-transform: uppercase; }
  .sh-name { font-weight: 700; color: var(--text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .sh-meta { display: flex; flex-wrap: wrap; gap: 4px 10px; font-size: 11px; color: var(--text-dim); }
  .sh-acts { display: flex; gap: 8px; flex-wrap: wrap; }
  .sh-btn { border: 1px solid var(--border-hi); background: var(--bg-panel); color: var(--text); border-radius: 9px; padding: 6px 12px;
    font: 600 12px var(--font-ui); cursor: pointer; display: inline-flex; gap: 6px; align-items: center; }
  .sh-btn:hover:not(:disabled) { background: var(--hover-strong); }
  .sh-btn:disabled { opacity: .5; cursor: default; }
  .sh-btn.pri { background: var(--orange); border-color: var(--orange); color: #fff; }
  .sh-bar { height: 8px; border-radius: 99px; background: var(--bg-panel); overflow: hidden; }
  .sh-fill { height: 100%; background: var(--orange); transition: width .3s; }
  .sh-fill.paused { background: var(--text-mute); }
  .sh-row { display: flex; justify-content: space-between; font-size: 11px; color: var(--text-dim); gap: 8px; }
  .sh-note { font-size: 12px; line-height: 1.4; color: var(--text-dim); display: flex; gap: 6px; align-items: flex-start; min-width: 0; overflow-wrap: anywhere; }
  .sh-note > i { margin-top: 1px; flex: none; }
  .sh-note.ok { color: #1bd96a; } .sh-note.bad { color: var(--red); }
  .sh-spin { animation: sh-rot 1s linear infinite; }
  @keyframes sh-rot { to { transform: rotate(360deg); } }
</style>
