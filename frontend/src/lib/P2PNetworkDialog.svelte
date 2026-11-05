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

<!-- Замість стіни тексту — перелік перевірок зі статусами (як чек-лист):
     кожен пункт має іконку стану, коротку назву й пояснення. -->
{#snippet row(tone: string, icon: string, title: string, text: string, extra: string = '')}
  <div class="pn-row {tone}">
    <span class="pn-ic"><i class="ti {icon}"></i></span>
    <div class="pn-tx">
      <b>{title}</b>
      <span>{text}</span>
      {#if extra}<span class="pn-extra">{extra}</span>{/if}
    </div>
  </div>
{/snippet}

<UiModal {open} title={t('p2p.network.title')} icon="ti-network" color="var(--orange)" locked={busy} onclose={() => open = false}>
  <div class="pn" aria-live="polite">
    <div class="pn-intro"><i class="ti ti-info-circle"></i><span>{t('p2p.network.intro')}</span></div>

    {#if busy && !report}
      <div class="pn-row busy">
        <span class="pn-ic"><i class="ti ti-loader-2 spin"></i></span>
        <div class="pn-tx"><b>{t('p2p.network.check')}</b><span>{t('p2p.network.busy')}</span></div>
      </div>
    {/if}

    {#if report}
      <div class="pn-list" class:refreshing={busy}>
        {@render row(
          report.control === 'ok' ? 'ok' : report.control === 'server' ? 'warn' : 'bad',
          report.control === 'ok' ? 'ti-circle-check' : report.control === 'dns' ? 'ti-world-off' : 'ti-server-off',
          t('p2p.network.row.control'),
          t('p2p.network.control.' + report.control),
        )}
        {#if report.blocked || report.policyRestricted}
          {@render row('bad', 'ti-shield-x', t('p2p.network.row.rules'), t('p2p.network.restricted'))}
        {:else if !report.supported}
          {@render row('muted', 'ti-shield-question', t('p2p.network.row.rules'), t('p2p.network.unsupported'))}
        {:else if report.configured}
          {@render row('ok', 'ti-shield-check', t('p2p.network.row.rules'), t('p2p.network.configured'))}
        {:else}
          {@render row('warn', 'ti-shield-half', t('p2p.network.row.rules'), t('p2p.network.optional'), t('p2p.network.consent'))}
        {/if}
      </div>
    {/if}

    {#if repaired}<div class="pn-banner ok"><i class="ti ti-circle-check"></i><span>{t('p2p.network.repaired')}</span></div>{/if}
    {#if error}<div class="pn-banner bad" role="alert"><i class="ti ti-alert-triangle"></i><span>{t('p2p.network.' + error)}</span></div>{/if}
  </div>
  {#snippet footer()}
    <button class="btn btn-ghost" disabled={busy} onclick={() => open = false}>{t('p2p.network.close')}</button>
    <button class="btn btn-ghost" disabled={busy} onclick={diagnose}>
      <i class="ti {busy ? 'ti-loader-2 spin' : 'ti-refresh'}"></i> {t('p2p.network.check')}
    </button>
    {#if report?.supported && !report.configured && !report.blocked && !report.policyRestricted}
      <button class="btn btn-primary" disabled={busy} onclick={repair}><i class="ti ti-shield-check"></i> {t('p2p.network.repair')}</button>
    {/if}
  {/snippet}
</UiModal>

<style>
  .pn { display: flex; flex-direction: column; gap: 12px; max-width: 520px; }
  .pn-intro { display: flex; gap: 10px; align-items: flex-start; font-size: 12.5px; line-height: 1.5; color: var(--text-mute); }
  .pn-intro i { flex: none; margin-top: 1px; font-size: 16px; color: var(--text-dim); }
  .pn-list { display: flex; flex-direction: column; border: 1px solid var(--border); border-radius: var(--r-lg); background: var(--bg-input); overflow: hidden; transition: opacity .2s; }
  .pn-list.refreshing { opacity: .55; }
  .pn-list > :global(.pn-row) + :global(.pn-row) { border-top: 1px solid var(--border); }
  .pn-row { --c: var(--text-mute); display: flex; gap: 12px; align-items: flex-start; padding: 13px 14px; animation: pn-in .35s cubic-bezier(.16,1,.3,1) both; }
  .pn-row + .pn-row { animation-delay: .07s; }
  .pn-row.busy { border: 1px solid var(--border); border-radius: var(--r-lg); background: var(--bg-input); --c: var(--orange); }
  .pn-row.ok { --c: var(--green); }
  .pn-row.warn { --c: var(--yellow); }
  .pn-row.bad { --c: var(--red); }
  .pn-ic { flex: none; display: grid; place-items: center; width: 34px; height: 34px; border-radius: 10px; font-size: 18px; color: var(--c); background: color-mix(in srgb, var(--c) 14%, transparent); }
  .pn-tx { min-width: 0; display: flex; flex-direction: column; gap: 3px; }
  .pn-tx b { font-size: 13px; font-weight: 700; color: var(--text); }
  .pn-tx span { font-size: 12px; line-height: 1.5; color: var(--text-mute); }
  .pn-extra { margin-top: 4px; padding: 8px 10px; border-radius: 9px; background: color-mix(in srgb, var(--c) 8%, transparent); }
  .pn-banner { display: flex; gap: 10px; align-items: flex-start; padding: 10px 12px; border-radius: var(--r-md); font-size: 12px; line-height: 1.5; color: var(--text);
    border: 1px solid color-mix(in srgb, var(--c) 35%, transparent); background: color-mix(in srgb, var(--c) 9%, transparent); }
  .pn-banner.ok { --c: var(--green); }
  .pn-banner.bad { --c: var(--red); }
  .pn-banner i { flex: none; font-size: 16px; color: var(--c); }
  @keyframes pn-in { from { opacity: 0; transform: translateY(5px); } to { opacity: 1; transform: none; } }
</style>
