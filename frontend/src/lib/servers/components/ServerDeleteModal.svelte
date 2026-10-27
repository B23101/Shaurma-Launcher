<script lang="ts">
  // ── Модалка видалення сервера ─────────────────────────────────────────
  // confirmDialog() (toast.svelte.ts) не підтримує чекбокс, тож для опції
  // «видалити й файли на диску» потрібна власна модалка — але зроблена
  // на спільному UiModal, а не власний .modal-overlay/.modal з нуля (як
  // було раніше в ServersLibraryPage).
  import UiModal from '../../UiModal.svelte'
  import { t } from '../../i18n.svelte'

  let {
    open = false,
    name = '',
    onclose = () => {},
    onconfirm = (_deleteFiles: boolean) => {},
  }: {
    open?: boolean
    name?: string
    onclose?: () => void
    onconfirm?: (deleteFiles: boolean) => void
  } = $props()

  let deleteFiles = $state(false)

  // Скидаємо чекбокс щоразу, як модалка відкривається для нового сервера.
  $effect(() => { if (open) deleteFiles = false })

  function confirm() {
    onconfirm(deleteFiles)
  }
</script>

<UiModal
  {open}
  size="sm"
  title={t('servers.modal.deleteTitle')}
  subtitle={name}
  icon="ti-alert-triangle"
  color="var(--red)"
  {onclose}
>
  <div class="sdm-desc">{t('servers.modal.deleteDesc')}</div>
  <label class="sdm-check">
    <input type="checkbox" bind:checked={deleteFiles} />
    <span>{t('servers.modal.deleteFiles')}</span>
  </label>
  {#if deleteFiles}
    <div class="sdm-warn"><i class="ti ti-alert-triangle"></i> {t('servers.modal.filesWarn')}</div>
  {/if}
  {#snippet footer()}
    <button class="btn btn-ghost" onclick={onclose}>{t('servers.modal.cancel')}</button>
    <button class="btn btn-danger" onclick={confirm}><i class="ti ti-trash"></i> {t('servers.modal.delete')}</button>
  {/snippet}
</UiModal>

<style>
  .sdm-desc { font: 400 12px/1.4 var(--font-ui); color: var(--text); margin-bottom: 12px; }
  .sdm-check { display: flex; align-items: center; gap: 8px; font: 500 12px/1 var(--font-ui); color: var(--text); cursor: pointer; padding: 8px 10px; border-radius: 8px; background: var(--hover); }
  .sdm-check input[type=checkbox] { accent-color: #ef4444; width: 16px; height: 16px; }
  .sdm-warn { display: flex; align-items: center; gap: 6px; font: 500 11px/1.3 var(--font-ui); color: #ef4444; margin-top: 10px; padding: 8px 10px; border-radius: 8px; background: rgba(239, 68, 68, .08); border: 1px solid rgba(239, 68, 68, .2); }
</style>
