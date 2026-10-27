<script lang="ts">
  // ── Консоль сервера друга ─────────────────────────────────────────────
  // Відкривається з картки сервера друга. Стан і виклики — у remoteServers.svelte.ts.
  import UiModal from '../../UiModal.svelte'
  import { t } from '../../i18n.svelte'
  import { remote, remoteCloseConsole, remoteSend } from '../remoteServers.svelte'

  let cmd = $state('')
  let box: HTMLDivElement | undefined = $state()

  const srv = $derived(remote.consoleFor)

  $effect(() => {
    void remote.lines.length
    queueMicrotask(() => { if (box) box.scrollTop = box.scrollHeight })
  })

  async function send() {
    const c = cmd
    cmd = ''
    await remoteSend(c)
  }
</script>

<UiModal
  open={!!srv}
  title={t('servers.remote.consoleTitle', { name: srv?.name ?? '' })}
  icon="ti-terminal-2"
  color={srv?.color || 'var(--orange)'}
  size="xl"
  onclose={() => { void remoteCloseConsole() }}
>
  {#if srv}
    <div class="rcm-sec">
      {#if remote.secure === true}
        <span class="rcm-pill ok" title={t('servers.remote.encryptedTip')}><i class="ti ti-lock"></i> {t('servers.remote.encrypted')}</span>
      {:else if remote.secure === false}
        <span class="rcm-pill" title={t('servers.remote.viaVpsTip')}><i class="ti ti-lock-open"></i> {t('servers.remote.viaVps')}</span>
      {/if}
    </div>
    <div class="rcm-out" bind:this={box}>
      {#each remote.lines as l, n (n)}<div class="ln {l.l}"><span class="t">{l.t}</span>{l.x}</div>{/each}
      {#if remote.ended}<div class="rcm-end">{remote.ended}</div>{/if}
    </div>
    {#if srv.canExec && !remote.ended}
      <input class="rcm-in" placeholder={t('servers.remote.cmdPlaceholder')} bind:value={cmd} maxlength="256"
             onkeydown={(e) => e.key === 'Enter' && send()} />
    {:else if !remote.ended}
      <div class="rcm-ro">{t('servers.remote.readOnly')}</div>
    {/if}
  {/if}
</UiModal>

<style>
  .rcm-sec { min-height: 22px; margin-bottom: 8px; }
  .rcm-pill { display: inline-flex; align-items: center; gap: 5px; font: 600 11px/1 var(--font-ui); padding: 4px 9px; border-radius: 999px; border: 1px solid var(--border); color: var(--text-mute); }
  .rcm-pill.ok { color: var(--green, #30a46c); border-color: currentColor; }
  .rcm-out { height: min(52vh, 420px); overflow: auto; font-family: var(--font-mono, monospace); font-size: 12px; padding: 8px 12px; white-space: pre-wrap; word-break: break-word; border: 1px solid var(--border); border-radius: var(--r-md); background: var(--bg-input, rgba(0,0,0,.25)); }
  .ln.warn { color: var(--orange, #f59e0b); } .ln.error { color: var(--red, #e5484d); } .ln.system { opacity: .65; }
  .ln .t { opacity: .5; margin-right: 8px; }
  .rcm-end { margin-top: 6px; opacity: .7; font-style: italic; }
  .rcm-in { width: 100%; box-sizing: border-box; margin-top: 10px; padding: 9px 12px; border: 1px solid var(--border); border-radius: var(--r-md); background: var(--bg-input, transparent); color: inherit; font-family: var(--font-mono, monospace); }
  .rcm-ro { margin-top: 10px; font-size: 12px; color: var(--text-mute); }
</style>
