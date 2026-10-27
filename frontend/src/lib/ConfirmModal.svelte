<script lang="ts">
  import UiModal from './UiModal.svelte'
  import { confirmState, confirmAnswer } from './toast.svelte'
</script>

{#if confirmState.open}
  <!-- z-index явно вище за глобальний .modal-overlay (1000000): підтвердження
       (напр. «закрити редактор без збереження?») може відкриватись ПОВЕРХ
       повноекранного фоторедактора (.editor-overlay, z-index:2000000) —
       без цього перекриття модалка малювалась під редактором і виглядала
       так, ніби кнопка «закрити» просто не працює. -->
  <UiModal
    open={true}
    size="sm"
    title={confirmState.title}
    icon={confirmState.danger ? 'ti-alert-triangle' : 'ti-question-mark'}
    color={confirmState.danger ? 'var(--red)' : 'var(--orange)'}
    zIndex="2100000"
    onclose={() => confirmAnswer(false)}
  >
    <div style="font-size:13px;color:var(--text-mute);line-height:1.6;white-space:pre-line">{confirmState.message}</div>
    {#snippet footer()}
      <button class="btn btn-ghost" onclick={() => confirmAnswer(false)}>{confirmState.cancelLabel}</button>
      <button class="btn {confirmState.danger ? 'btn-danger' : 'btn-primary'}" onclick={() => confirmAnswer(true)}>
        {confirmState.confirmLabel}
      </button>
    {/snippet}
  </UiModal>
{/if}
