<script lang="ts">
  // «Друг хоче надіслати вам збірку/сервер» — з'являється, щойно картка передачі приходить у
  // чат (shareStore → shareUi.offers). Нічого не качається без явного «Прийняти»; «Пізніше»
  // лише ховає вікно — картка лишається в чаті з тими самими кнопками.
  import UiModal from '../UiModal.svelte'
  import { App } from '../wails'
  import { toast } from '../toast.svelte'
  import { t } from '../i18n.svelte'
  import { shareUi, shareFriends, dropOffer, openTransfer, errText, loadPreview, rememberPreviewSrc } from './shareStore.svelte'
  import { fmtBytes } from './shareMsg'
  import ShareContents from './ShareContents.svelte'
  import FriendAvatar from './FriendAvatar.svelte'
  import { friendColor } from './friendHead'

  const Z = '2200000'
  const offer = $derived(shareUi.offersHidden ? undefined : shareUi.offers[0])
  // Вміст підтягуємо одразу: друг бачить, що саме отримає, ще до рішення.
  $effect(() => {
    const o = offer
    if (!o) return
    rememberPreviewSrc(o.card.sid, o.encoded, o.fromAccount)
    loadPreview(o.card.sid)
  })
  const isServer = $derived(offer?.card.k === 'server')
  const offerFriend = $derived(offer ? shareFriends[offer.fromAccount] : undefined)
  let busy = $state<'' | 'accept' | 'decline'>('')

  async function accept() {
    if (!offer || busy) return
    busy = 'accept'
    const o = offer
    try {
      const v = await App.ShareAccept(o.encoded, o.fromAccount, o.fromName)
      dropOffer(o.card.sid)
      openTransfer('in', v?.id || o.card.sid)
    } catch (e) {
      toast(errText(e), 'error', 7000)
    } finally { busy = '' }
  }
  async function decline() {
    if (!offer || busy) return
    busy = 'decline'
    const o = offer
    try {
      await App.ShareDiscard(o.card.sid, o.encoded, o.fromAccount)
      dropOffer(o.card.sid)
      toast(t('share.offer.declinedToast', { name: o.fromName }), 'info')
    } catch (e) {
      toast(errText(e), 'error')
    } finally { busy = '' }
  }
  function later() { if (offer && !busy) dropOffer(offer.card.sid) }
</script>

{#if offer}
  <UiModal
    open={true}
    zIndex={Z}
    width="min(600px, 94vw)"
    icon={isServer ? 'ti-server-2' : 'ti-package'}
    title={t(isServer ? 'share.offer.titleServer' : 'share.offer.title')}
    subtitle={t('share.offer.sub', { name: offer.fromName })}
    locked={!!busy}
    onclose={later}
  >
    <div class="of-card">
      <div class="of-ic"><i class="ti {isServer ? 'ti-server-2' : 'ti-package'}"></i></div>
      <div class="of-main">
        <div class="of-name" title={offer.card.n}>{offer.card.n}</div>
        <div class="of-meta">
          {#if offer.card.m?.mc}<span><i class="ti ti-brand-minecraft"></i> {offer.card.m.mc}</span>{/if}
          {#if offer.card.m?.loader && offer.card.m.loader !== 'vanilla'}<span>{offer.card.m.loader}</span>{/if}
          {#if offer.card.m?.core}<span>{offer.card.m.core}</span>{/if}
        </div>
      </div>
    </div>
    <div class="of-grid">
      <div class="of-from">
        <FriendAvatar nickname={offer.fromName} color={offerFriend?.color ?? ''} status={offerFriend?.presence?.status ?? 'offline'} size={30} />
        <span class="of-fromt"><span>{t('share.offer.from')}</span><b style="color:{friendColor(offerFriend?.color ?? '', 'var(--text)')}">{offer.fromName}</b></span>
      </div>
      <div><span>{t('share.offer.size')}</span><b>{fmtBytes(offer.card.b)}</b></div>
      <div><span>{t('share.stat.files')}</span><b>{offer.card.f}</b></div>
    </div>
    <div class="of-sec">{t('share.contents.title')}</div>
    <ShareContents sid={offer.card.sid} maxHeight="min(260px, 34vh)" />
    <ul class="of-safe">
      <li><i class="ti ti-shield-check"></i> {t('share.offer.safe1')}</li>
      <li><i class="ti ti-player-play-off"></i> {t('share.offer.safe2')}</li>
      <li><i class="ti ti-refresh"></i> {t('share.offer.safe3')}</li>
    </ul>
    {#if shareUi.offers.length > 1}
      <div class="of-more">{t('share.offer.more', { n: shareUi.offers.length - 1 })}</div>
    {/if}

    {#snippet footer()}
      <button class="btn btn-danger" disabled={!!busy} onclick={decline}>
        {#if busy === 'decline'}<i class="ti ti-loader-2 of-spin"></i>{:else}<i class="ti ti-x"></i>{/if} {t('share.offer.decline')}
      </button>
      <button class="btn btn-ghost" disabled={!!busy} onclick={later}>{t('share.offer.later')}</button>
      <button class="btn btn-primary" disabled={!!busy} onclick={accept}>
        {#if busy === 'accept'}<i class="ti ti-loader-2 of-spin"></i>{:else}<i class="ti ti-download"></i>{/if} {t('share.offer.accept')}
      </button>
    {/snippet}
  </UiModal>
{/if}

<style>
  .of-card { display: flex; gap: 12px; align-items: center; padding: 12px; border-radius: 14px; background: var(--bg-input); border: 1px solid var(--border-hi); }
  .of-ic { width: 46px; height: 46px; border-radius: 12px; display: grid; place-items: center; font-size: 24px; color: var(--orange);
    background: rgba(var(--orange-rgb), .14); flex: none; }
  .of-main { min-width: 0; flex: 1; }
  .of-name { font-size: 15px; font-weight: 800; color: var(--text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .of-meta { display: flex; flex-wrap: wrap; gap: 4px 12px; font-size: 11.5px; color: var(--text-mute); margin-top: 3px; }
  .of-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px; margin-top: 10px; }
  .of-grid > div { background: var(--bg-input); border: 1px solid var(--border); border-radius: 10px; padding: 8px 10px; display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .of-grid span { font-size: 10.5px; font-weight: 700; text-transform: uppercase; letter-spacing: .3px; color: var(--text-mute); }
  .of-grid b { font-size: 13px; color: var(--text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .of-sec { margin: 12px 0 6px; font-size: 10.5px; font-weight: 800; text-transform: uppercase; letter-spacing: .4px; color: var(--text-mute); }
  .of-grid > .of-from { flex-direction: row; align-items: center; gap: 8px; }
  .of-fromt { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .of-safe { list-style: none; margin: 12px 0 0; padding: 0; display: flex; flex-direction: column; gap: 6px; }
  .of-safe li { display: flex; gap: 8px; align-items: flex-start; font-size: 12px; color: var(--text-mute); line-height: 1.4; }
  .of-safe i { color: var(--green); margin-top: 1px; flex: none; }
  .of-more { margin-top: 10px; font-size: 11.5px; color: var(--text-mute); }
  .of-spin { animation: of-rot 1s linear infinite; }
  @keyframes of-rot { to { transform: rotate(360deg); } }
</style>
