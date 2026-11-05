<script lang="ts">
  // Картка збірки/сервера в груповому чаті (повідомлення build_offer) — всередині звичайної
  // бульбашки ShaurmaChat. Ключів у картці немає: «Отримати» просить відправника видати
  // окремий ключ саме мені (groups/GroupsStore.claimBuild).
  import { t } from '../i18n.svelte'
  import { fmtBytes } from '../friends/shareMsg'
  import type { GroupBuildOffer } from './groupTypes'

  let { offer, own, onClaim }: { offer: GroupBuildOffer; own: boolean; onClaim: () => void } = $props()
  const meta = $derived([offer.m?.mc, offer.m?.loader && offer.m.loader !== 'vanilla' ? offer.m.loader : '', offer.m?.core].filter(Boolean).join(' · '))
</script>

<div class="go">
  <span class="go-ic"><i class="ti {offer.k === 'server' ? 'ti-server-2' : 'ti-package'}"></i></span>
  <div class="go-t">
    <b>{offer.n}</b>
    {#if meta}<small>{meta}</small>{/if}
    <small>{fmtBytes(offer.b)} · {offer.f} {t('share.stat.files').toLowerCase()}</small>
  </div>
  {#if own}
    <small class="go-own">{t('groups.share.yours')}</small>
  {:else}
    <button class="btn btn-primary btn-sm" onclick={onClaim}><i class="ti ti-download"></i> {t('groups.share.get')}</button>
  {/if}
</div>

<style>
  /* Іконка + опис в один рядок, кнопка — на всю ширину під ними: не ламається у вузькому вікні. */
  .go { display: grid; grid-template-columns: auto minmax(0, 1fr); align-items: center; gap: 8px 10px; width: min(260px, 100%); padding: 2px 0 4px; }
  .go-ic { width: 38px; height: 38px; display: grid; place-items: center; border-radius: 11px; font-size: 20px; color: var(--orange); background: rgba(var(--orange-rgb), .13); }
  .go-t { min-width: 0; display: flex; flex-direction: column; gap: 2px; }
  .go-t b { font-size: 13.5px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .go-t small { font-size: 11.5px; color: var(--text-mute); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .go > .btn, .go-own { grid-column: 1 / -1; }
  .go > .btn { justify-content: center; }
  .go-own { font-size: 11.5px; color: var(--text-mute); }
</style>
