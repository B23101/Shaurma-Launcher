<script lang="ts">
  import { confirmState, confirmAnswer } from './toast.svelte'
</script>

{#if confirmState.open}
  <div class="modal-overlay" onclick={() => confirmAnswer(false)}>
    <div class="modal" onclick={(e: any) => e.stopPropagation()}>
      <div class="modal-head">
        <h3>
          {#if confirmState.danger}<i class="ti ti-alert-triangle" style="color:var(--red)"></i>{/if}
          {confirmState.title}
        </h3>
      </div>
      <div class="modal-body">
        <div style="font-size:13px;color:var(--text-mute);line-height:1.6;white-space:pre-line">{confirmState.message}</div>
      </div>
      <div class="modal-foot">
        <button class="btn btn-ghost" onclick={() => confirmAnswer(false)}>{confirmState.cancelLabel}</button>
        <button class="btn {confirmState.danger ? 'btn-danger' : 'btn-primary'}" onclick={() => confirmAnswer(true)}>
          {confirmState.confirmLabel}
        </button>
      </div>
    </div>
  </div>
{/if}
