<script lang="ts">
  // ── Сторінка сервера друга ────────────────────────────────────────────
  // Відкривається кліком по картці сервера друга. Показує лише те, що дозволив власник:
  //   • Старт / Стоп — якщо є відповідне право;
  //   • консоль — якщо є право «Консоль» (вводити команди — лише з правом «Команди»).
  // Нічого з власного (файли, моди, налаштування) тут немає: у друга цього доступу нема.
  // Стан і виклики — у remoteServers.svelte.ts; сторінка бере сервер зі спільного списку,
  // тож права, статус і «офлайн» оновлюються наживо.
  import { untrack, onDestroy } from 'svelte'
  import { t } from '../i18n.svelte'
  import ServerStatusBadge from './components/ServerStatusBadge.svelte'
  import { remote, remoteKey, remoteAct, remoteOpenConsole, remoteCloseConsole, remoteSend, startRemote } from './remoteServers.svelte'

  let {
    ownerId = '',
    serverId = '',
    onBack = () => {},
  }: { ownerId?: string; serverId?: string; onBack?: () => void } = $props()

  $effect(() => startRemote())

  const srv = $derived(remote.servers.find(r => r.ownerId === ownerId && r.id === serverId) ?? null)
  const nick = $derived(remote.nicks[ownerId] ?? t('servers.remote.friend'))
  const online = $derived(!!srv?.ownerOnline)
  const st = $derived(srv?.state || 'stopped')
  const running = $derived(st === 'running')
  const transient = $derived(st === 'starting' || st === 'stopping')
  const busy = $derived(!!srv && !!remote.pending[remoteKey(srv)])
  const canConsole = $derived(!!srv?.canConsole)
  const canView = $derived(online && canConsole) // консоль можна відкрити

  let cmd = $state('')
  let box: HTMLDivElement | undefined = $state()
  const mine = $derived(remote.consoleFor?.ownerId === ownerId && remote.consoleFor?.id === serverId)

  async function connect() {
    const s = srv
    if (!s) return
    await remoteOpenConsole(s)
  }

  // Консоль відкривається сама, щойно її можна показати (власник онлайн + є право), і закривається,
  // коли право зникло або власник вийшов.
  $effect(() => {
    if (canView) {
      untrack(() => { if (!remote.sid && !mine) void connect() })
    } else {
      untrack(() => { if (mine) void remoteCloseConsole() })
    }
  })

  onDestroy(() => { void remoteCloseConsole() })

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

<div class="rsp">
  <header class="rsp-head">
    <button class="btn btn-ghost btn-sm" onclick={onBack} title={t('servers.toolbar.back')}>
      <i class="ti ti-arrow-left"></i> <span>{t('servers.toolbar.back')}</span>
    </button>
    {#if srv}
      <div class="rsp-id">
        <div class="rsp-name" title={srv.name}>{srv.name}</div>
        <div class="rsp-sub">{nick}{srv.core ? ' · ' + srv.core : ''}</div>
      </div>
      {#if !online}
        <span class="rsp-off"><i class="ti ti-plug-off"></i> {t('servers.remote.offlinePill')}</span>
      {:else}
        <ServerStatusBadge status={st} />
      {/if}
      <span class="rsp-spacer"></span>
      {#if online}
        {#if transient}
          <button class="rsp-btn" disabled><i class="ti ti-loader-2 spin"></i> {t('servers.actions.working')}</button>
        {:else if running && srv.canStop}
          <button class="rsp-btn rsp-stop" disabled={busy} onclick={() => remoteAct(srv, 'stop')}><i class="ti ti-player-stop"></i> {t('servers.card.stop')}</button>
        {:else if !running && srv.canStart}
          <button class="rsp-btn rsp-play" disabled={busy} onclick={() => remoteAct(srv, 'start')}><i class="ti ti-player-play"></i> {t('servers.card.start')}</button>
        {/if}
      {/if}
    {/if}
  </header>

  {#if !srv}
    <div class="rsp-msg"><i class="ti ti-lock-off"></i> {t('servers.remote.gone')}</div>
  {:else if !online}
    <div class="rsp-msg"><i class="ti ti-plug-off"></i> {t('servers.remote.offline')}</div>
  {:else if !canConsole}
    <div class="rsp-msg"><i class="ti ti-terminal-2-off"></i> {t('servers.remote.noConsole')}</div>
  {:else}
    <div class="rsp-sec">
      {#if remote.secure === true}
        <span class="rsp-pill ok" title={t('servers.remote.encryptedTip')}><i class="ti ti-lock"></i> {t('servers.remote.encrypted')}</span>
      {:else if remote.secure === false}
        <span class="rsp-pill" title={t('servers.remote.viaVpsTip')}><i class="ti ti-lock-open"></i> {t('servers.remote.viaVps')}</span>
      {/if}
    </div>
    <div class="rsp-out" bind:this={box}>
      {#if mine}
        {#each remote.lines as l, n (n)}<div class="ln {l.l}"><span class="t">{l.t}</span>{l.x}</div>{/each}
      {/if}
      {#if remote.ended}
        <div class="rsp-end">
          {remote.ended}
          <button class="btn btn-ghost btn-sm" onclick={() => void connect()}><i class="ti ti-refresh"></i> {t('servers.remote.reconnect')}</button>
        </div>
      {/if}
    </div>
    {#if srv.canExec && !remote.ended}
      <input class="rsp-in" placeholder={t('servers.remote.cmdPlaceholder')} bind:value={cmd} maxlength="256"
             onkeydown={(e) => e.key === 'Enter' && send()} />
    {:else if !remote.ended}
      <div class="rsp-ro">{t('servers.remote.readOnly')}</div>
    {/if}
  {/if}
</div>

<style>
  .rsp { flex: 1; min-height: 0; min-width: 0; display: flex; flex-direction: column; gap: 12px; padding: 14px 18px 18px; }
  .rsp-head { display: flex; align-items: center; gap: 12px; min-width: 0; }
  .rsp-id { min-width: 0; }
  .rsp-name { font: 750 16px/1.2 var(--font-ui); color: var(--text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .rsp-sub { margin-top: 3px; font: 500 11px/1.2 var(--font-mono); color: var(--text-mute); }
  .rsp-spacer { flex: 1; }
  .rsp-off { display: inline-flex; align-items: center; gap: 4px; padding: 3px 8px; border-radius: 999px; font: 700 11px/1 var(--font-ui); color: var(--text-dim); border: 1px solid var(--border); }
  .rsp-btn {
    height: 34px; padding: 0 16px; border-radius: 9px; cursor: pointer; border: 1px solid transparent;
    display: inline-flex; align-items: center; gap: 6px; font: 700 12.5px/1 var(--font-ui); color: var(--text); background: var(--hover);
  }
  .rsp-btn:disabled { opacity: .7; cursor: default; }
  .rsp-play { background: #22c55e; color: #06230f; }
  .rsp-stop { background: #eab308; color: #241a02; }
  .rsp-msg { display: flex; align-items: center; gap: 10px; padding: 18px; border-radius: var(--r-lg, 16px); background: var(--bg-card); border: 1px solid var(--border); color: var(--text-mute); font-size: 13px; }
  .rsp-msg i { font-size: 20px; }
  .rsp-sec { min-height: 22px; }
  .rsp-pill { display: inline-flex; align-items: center; gap: 5px; font: 600 11px/1 var(--font-ui); padding: 4px 9px; border-radius: 999px; border: 1px solid var(--border); color: var(--text-mute); }
  .rsp-pill.ok { color: var(--green, #30a46c); border-color: currentColor; }
  .rsp-out { flex: 1; min-height: 200px; overflow: auto; font-family: var(--font-mono, monospace); font-size: 12px; padding: 8px 12px; white-space: pre-wrap; word-break: break-word; border: 1px solid var(--border); border-radius: var(--r-md); background: var(--bg-input, rgba(0,0,0,.25)); }
  .ln.warn { color: var(--orange, #f59e0b); } .ln.error { color: var(--red, #e5484d); } .ln.system { opacity: .65; }
  .ln .t { opacity: .5; margin-right: 8px; }
  .rsp-end { margin-top: 6px; display: flex; align-items: center; gap: 10px; opacity: .85; font-style: italic; }
  .rsp-in { width: 100%; box-sizing: border-box; padding: 9px 12px; border: 1px solid var(--border); border-radius: var(--r-md); background: var(--bg-input, transparent); color: inherit; font-family: var(--font-mono, monospace); }
  .rsp-ro { font-size: 12px; color: var(--text-mute); }
</style>
