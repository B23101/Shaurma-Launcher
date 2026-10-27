<script lang="ts">
  // Окреме вікно «Що всередині» (з картки в чаті чи з вікна передачі): та сама ShareContents.
  import UiModal from '../UiModal.svelte'
  import { t } from '../i18n.svelte'
  import { shareUi, shareIn } from './shareStore.svelte'
  import ShareContents from './ShareContents.svelte'

  const sid = $derived(shareUi.contents)
  const name = $derived(sid ? shareIn[sid]?.name ?? shareUi.offers.find(o => o.card.sid === sid)?.card.n ?? '' : '')
  function close() { shareUi.contents = null }
</script>

{#if sid}
  <UiModal open={true} zIndex="2250000" width="min(600px, 94vw)" icon="ti-list-details"
    title={t('share.contents.title')} subtitle={name} onclose={close}>
    <ShareContents {sid} maxHeight="min(420px, 55vh)" />
    {#snippet footer()}
      <button class="btn btn-primary" onclick={close}>{t('share.close')}</button>
    {/snippet}
  </UiModal>
{/if}
