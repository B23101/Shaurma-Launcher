<script lang="ts">
  import { onMount } from 'svelte'
  import { App, on, type P2PNetworkReport } from './wails'
  import { t } from './i18n.svelte'
  import UiModal from './UiModal.svelte'

  let open = $state(false)
  let busy = $state(false)
  let report = $state<P2PNetworkReport | null>(null)
  let error = $state('')
  let repaired = $state(false)
  let notified = false

  async function diagnose() {
    busy = true
    error = ''
    try { report = await App.DiagnoseP2PNetwork() }
    catch { error = 'p2p_repair_unavailable' }
    finally { busy = false }
  }
  function show(manual = false) {
    if (open || (!manual && notified)) return
    notified = true
    open = true
    repaired = false
    report = null
    void diagnose()
  }
  async function repair() {
    busy = true
    error = ''
    try {
      report = await App.RepairP2PNetwork()
      repaired = true
    } catch (e) {
      const code = e instanceof Error ? e.message : ''
      error = /^p2p_repair_(declined|unavailable|policy|busy|failed|pending|listener)$/.test(code) ? code : 'p2p_repair_failed'
      // Refresh even on a partial failure; never leave a stale repair button
      // implying that completed rules still need administrator approval.
      try { report = await App.DiagnoseP2PNetwork() } catch { /* keep last report */ }
    } finally { busy = false }
  }
  onMount(() => {
    const failed = () => show()
    const manual = () => show(true)
    const shareFailed = (v: { error?: string }) => {
      if (v?.error && /timeout|deadline|network|connection|dial tcp|мереж|з'єднан|з’єднан/i.test(v.error)) show()
    }
    window.addEventListener('p2p-network-failure', failed)
    window.addEventListener('p2p-network-settings', manual)
    const off = [on('p2p:network_failure', failed), on('share:in', shareFailed), on('share:out', shareFailed)]
    return () => {
      window.removeEventListener('p2p-network-failure', failed)
      window.removeEventListener('p2p-network-settings', manual)
      off.forEach(f => f())
    }
  })
</script>

<UiModal {open} title={t('p2p.network.title')} icon="ti-network" locked={busy} onclose={() => open = false}>
  <div aria-live="polite">
    <p>{t('p2p.network.intro')}</p>
    {#if busy}<p>{t('p2p.network.busy')}</p>{/if}
    {#if report}
      <p>{t('p2p.network.control.' + report.control)}</p>
      {#if report.blocked || report.policyRestricted}
        <p>{t('p2p.network.restricted')}</p>
      {:else if !report.supported}
        <p>{t('p2p.network.unsupported')}</p>
      {:else if report.configured}
        <p>{t('p2p.network.configured')}</p>
      {:else}
        <p>{t('p2p.network.optional')}</p>
        <p>{t('p2p.network.consent')}</p>
      {/if}
    {/if}
    {#if repaired}<p>{t('p2p.network.repaired')}</p>{/if}
    {#if error}<p role="alert">{t('p2p.network.' + error)}</p>{/if}
  </div>
  {#snippet footer()}
    <button class="btn btn-ghost" disabled={busy} onclick={() => open = false}>{t('p2p.network.close')}</button>
    <button class="btn btn-ghost" disabled={busy} onclick={diagnose}>{t('p2p.network.check')}</button>
    {#if report?.supported && !report.configured && !report.blocked && !report.policyRestricted}
      <button class="btn btn-primary" disabled={busy} onclick={repair}>{t('p2p.network.repair')}</button>
    {/if}
  {/snippet}
</UiModal>
