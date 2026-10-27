<script lang="ts">
  // Після краху / закриття лаунчера посеред прийому: що лишилось на диску і що з ним робити.
  // Повністю прийняте встановлюється й без відправника; недокачане продовжується з місця
  // обриву (якщо відправник ще роздає) або видаляється. Нічого не стається без кліку.
  import UiModal from '../UiModal.svelte'
  import { App } from '../wails'
  import { toast } from '../toast.svelte'
  import { t } from '../i18n.svelte'
  import { shareRecoverable, shareUi, removeRecoverable, openTransfer, errText } from './shareStore.svelte'
  import { fmtBytes, inReasonText } from './shareMsg'

  const Z = '2120000'
  let busy = $state('')
  const items = $derived(shareRecoverable.items)
  const open = $derived(shareUi.recoverOpen && items.length > 0)

  async function resume(mid: string) {
    if (busy) return
    busy = mid
    try {
      const v = await App.ShareResumeRecovered(mid)
      removeRecoverable(mid)
      if (v?.id) openTransfer('in', v.id)
    } catch (e) { toast(errText(e), 'error', 7000) } finally { busy = '' }
  }
  async function discard(mid: string) {
    if (busy) return
    busy = mid
    try { await App.ShareDiscardRecovered(mid); removeRecoverable(mid) }
    catch (e) { toast(errText(e), 'error') } finally { busy = '' }
  }
  function close() { if (!busy) shareUi.recoverOpen = false }
</script>

{#if open}
  <UiModal open={true} zIndex={Z} width="min(540px, 94vw)" icon="ti-history" color="var(--blue)"
    title={t('share.recover.title')} subtitle={t('share.recover.sub')} locked={!!busy} onclose={close}>
    <div class="rc-list">
      {#each items as r (r.mid)}
        <div class="rc-item">
          <div class="rc-ic"><i class="ti {r.kind === 'server' ? 'ti-server-2' : 'ti-package'}"></i></div>
          <div class="rc-main">
            <b title={r.name}>{r.name}</b>
            <span>{t('share.recover.from', { name: r.from || t('share.friend') })} · {fmtBytes(r.bytes)}</span>
            <span class="rc-state">
              {#if r.complete}<i class="ti ti-circle-check ok"></i> {t('share.recover.complete')}
              {:else}<i class="ti ti-progress"></i> {t('share.recover.partial', { done: r.filesDone, total: r.files || '?' })}{/if}
            </span>
            {#if r.reason && !r.complete}<span class="rc-why">{inReasonText(r.reason)}</span>{/if}
          </div>
          <div class="rc-acts">
            <button class="btn btn-primary" disabled={!!busy} onclick={() => resume(r.mid)}>
              {#if busy === r.mid}<i class="ti ti-loader-2 rc-spin"></i>{:else}<i class="ti {r.complete ? 'ti-package-import' : 'ti-player-play'}"></i>{/if}
              {r.complete ? t('share.recover.install') : t('share.recover.resume')}
            </button>
            <button class="btn btn-danger" disabled={!!busy} onclick={() => discard(r.mid)}><i class="ti ti-trash"></i> {t('share.recover.discard')}</button>
          </div>
        </div>
      {/each}
    </div>
    {#snippet footer()}
      <button class="btn btn-ghost" disabled={!!busy} onclick={close}>{t('share.recover.later')}</button>
    {/snippet}
  </UiModal>
{/if}

<style>
  .rc-list { display: flex; flex-direction: column; gap: 8px; }
  .rc-item { display: flex; gap: 10px; align-items: center; padding: 10px; border-radius: 12px; background: var(--bg-input); border: 1px solid var(--border); }
  .rc-ic { width: 38px; height: 38px; border-radius: 10px; display: grid; place-items: center; font-size: 20px; color: var(--orange); background: rgba(var(--orange-rgb), .14); flex: none; }
  .rc-main { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; font-size: 11.5px; color: var(--text-mute); }
  .rc-main b { font-size: 13px; color: var(--text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .rc-state { display: flex; gap: 5px; align-items: center; }
  .rc-state .ok { color: var(--green); }
  .rc-why { color: var(--yellow); }
  .rc-acts { display: flex; flex-direction: column; gap: 6px; flex: none; }
  .rc-acts .btn { justify-content: center; padding: 7px 12px; }
  .rc-spin { animation: rc-rot 1s linear infinite; }
  @keyframes rc-rot { to { transform: rotate(360deg); } }
</style>
