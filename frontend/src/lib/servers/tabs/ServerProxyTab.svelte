<script lang="ts">
  // ── Вкладка «Мережа серверів» проксі Velocity ──────────────────────────
  // Проксі — не «ще один сервер», а система серверів: учасники з іменами в
  // мережі, вхід гравців, ланцюги запуску (проксі ↔ сервери), налаштування
  // самого Velocity і діагностика всієї мережі. Конфіги (velocity.toml,
  // forwarding.secret, paper-global.yml / FabricProxy-Lite / PCF,
  // server.properties) лаунчер пише сам (backend/internal/servers/network*.go).
  //
  // remoteServer — проксі друга: та сама вкладка, але лише сервери його мережі,
  // до яких власник дав доступ (статус, запуск і зупинка за правами).
  import { onMount } from 'svelte'
  import SCard from '../../settings/SCard.svelte'
  import '../../settings/settings.css'
  import StSelect, { type StOption } from '../../StSelect.svelte'
  import UiModal from '../../UiModal.svelte'
  import { App, on } from '../../wails'
  import { t } from '../../i18n.svelte'
  import { toast } from '../../toast.svelte'
  import { serverVisual, toBrowserServer } from '../serverTargets'
  import type { NetworkView, NetworkMemberView, NetworkSettings, NetworkIssue, ServerView, ExternalServer, RemoteServer } from '../../types'
  import { remote, remoteKey, remoteAct } from '../remoteServers.svelte'

  let {
    srv,
    onOpenServer = null,
    remoteServer = null,
  }: {
    srv: ServerView
    onOpenServer?: ((id: string) => void) | null
    remoteServer?: RemoteServer | null
  } = $props()

  // Сервери мережі проксі друга (з його списку доступних серверів).
  const remoteMembers = $derived(remoteServer
    ? remote.servers.filter(r => r.ownerId === remoteServer.ownerId && r.network === remoteServer.id)
    : [])

  const proxyId = $derived(srv.id)

  let view = $state<NetworkView | null>(null)
  let busy = $state(false)        // збереження складу
  let netBusy = $state(false)     // старт/зупинка мережі
  let settings = $state<NetworkSettings | null>(null)
  let settingsDirty = $state(false)
  let settingsSaving = $state(false)
  let showAdvanced = $state(false)
  let issues = $state<NetworkIssue[] | null>(null)
  let diagnosing = $state(false)
  let repairing = $state(false)
  let configText = $state<string | null>(null)
  let pickerOpen = $state(false)

  // Справжні іконки/кольори серверів (а не загальна іконка ядра): NetworkMemberView
  // їх не несе, тож беремо зі списку серверів — так само, як у бібліотеці й вибірі цілей.
  let srvById = $state<Record<string, any>>({})
  async function loadVisuals() {
    try {
      const list = ((await App.ListServers()) ?? []) as ServerView[]
      srvById = Object.fromEntries(list.map(x => [x.id, toBrowserServer(x)]))
    } catch { /* лишаємо іконки ядер */ }
  }
  function visualOf(m: { serverId: string; core: string }) {
    return serverVisual(srvById[m.serverId] ?? { core: m.core })
  }

  async function load() {
    try {
      const v = await App.NetworkGetView(proxyId)
      view = v
      if (!settingsDirty) settings = structuredClone(v.settings)
    } catch (e: any) { toast(String(e), 'error') }
  }

  $effect(() => {
    void proxyId
    if (remoteServer) return
    settingsDirty = false
    issues = null
    load()
  })

  onMount(() => { void loadVisuals() })

  // Живі статуси серверів без повного перечитання.
  onMount(() => on('server:status', (d: any) => {
    if (!view) return
    const running = d?.status === 'running'
    if (d?.serverId === view.proxyServerId) { view.proxyRunning = running; view.proxyStatus = d.status; return }
    const m = view.members.find(x => x.serverId === d?.serverId)
    if (m) { m.running = running; m.status = d.status }
  }))

  const members = $derived((view?.members ?? []).filter(m => m.inNetwork).sort((a, b) => a.order - b.order))
  const candidates = $derived((view?.members ?? []).filter(m => !m.inNetwork))
  const runningCount = $derived(members.filter(m => m.running).length)

  // ── Склад мережі: кожна зміна одразу зберігається ──
  async function saveMembers() {
    if (!view) return
    busy = true
    try {
      await App.NetworkSave(proxyId, view.members.map(m => ({ ...m })))
    } catch (e: any) {
      toast(String(e), 'error')
    } finally {
      busy = false
      await load()
      if (issues) diagnose()
    }
  }

  function add(id: string) {
    const m = view?.members.find(x => x.serverId === id)
    if (!m) return
    pickerOpen = false
    m.inNetwork = true
    m.order = members.length + 1
    saveMembers()
  }

  function remove(m: NetworkMemberView) {
    m.inNetwork = false
    saveMembers()
  }

  function move(m: NetworkMemberView, dir: -1 | 1) {
    const list = members
    const i = list.indexOf(m)
    const other = list[i + dir]
    if (!other) return
    ;[m.order, other.order] = [other.order, m.order]
    saveMembers()
  }

  function setFlag(m: NetworkMemberView, key: 'autoStart' | 'startsNetwork' | 'inTry') {
    m[key] = !m[key]
    saveMembers()
  }

  function setText(m: NetworkMemberView, key: 'alias' | 'forcedHost', value: string) {
    const v = value.trim()
    if (v === m[key]) return
    m[key] = v
    saveMembers()
  }

  // ── Сервери поза лаунчером (інша машина, запущений окремо) ──
  let extAlias = $state('')
  let extAddr = $state('')
  async function saveExternal(list: ExternalServer[]) {
    busy = true
    try { await App.NetworkSaveExternal(proxyId, list); return true }
    catch (e: any) { toast(String(e), 'error'); return false }
    finally { busy = false; await load() }
  }
  async function addExternal() {
    if (!view || !extAlias.trim() || !extAddr.trim()) return
    if (await saveExternal([...view.external, { alias: extAlias.trim(), address: extAddr.trim(), inTry: false }])) {
      extAlias = ''; extAddr = ''
    }
  }
  function removeExternal(i: number) {
    if (view) saveExternal(view.external.filter((_, j) => j !== i))
  }
  function toggleExternalTry(i: number) {
    if (view) saveExternal(view.external.map((x, j) => (j === i ? { ...x, inTry: !x.inTry } : x)))
  }

  function setDelay(m: NetworkMemberView, value: string) {
    const v = Math.max(0, Math.min(600, Math.round(Number(value) || 0)))
    if (v === m.delaySec) return
    m.delaySec = v
    saveMembers()
  }

  // Сервер може бути лише в одній мережі (у кожного проксі свій секрет).
  const addCandidates = $derived(candidates.filter(m => !m.otherNetwork))

  // ── Запуск/зупинка всієї мережі ──
  async function startNetwork() {
    netBusy = true
    try { await App.NetworkStart(proxyId); toast(t('proxy.started'), 'success') }
    catch (e: any) { toast(String(e), 'error') }
    finally { netBusy = false; load() }
  }
  async function stopNetwork() {
    netBusy = true
    try { await App.NetworkStop(proxyId); toast(t('proxy.stopping'), 'info') }
    catch (e: any) { toast(String(e), 'error') }
    finally { netBusy = false; load() }
  }

  // ── Налаштування проксі ──
  function touch() { settingsDirty = true }
  async function saveSettings() {
    if (!settings) return
    settingsSaving = true
    try {
      await App.NetworkSaveSettings(proxyId, $state.snapshot(settings))
      settingsDirty = false
      toast(view?.proxyRunning ? t('proxy.settingsSavedLive') : t('proxy.settingsSaved'), 'success')
      await load()
    } catch (e: any) { toast(String(e), 'error') }
    finally { settingsSaving = false }
  }
  function resetSettings() {
    if (view) settings = structuredClone(view.settings)
    settingsDirty = false
  }

  // ── Діагностика ──
  async function diagnose() {
    diagnosing = true
    try { issues = (await App.NetworkDiagnose(proxyId)) ?? [] }
    catch (e: any) { toast(String(e), 'error') }
    finally { diagnosing = false }
  }
  async function repair() {
    repairing = true
    try { await App.NetworkRepair(proxyId); toast(t('proxy.repaired'), 'success'); await load(); await diagnose() }
    catch (e: any) { toast(String(e), 'error') }
    finally { repairing = false }
  }
  async function showConfig() {
    try { configText = await App.NetworkConfigText(proxyId) }
    catch (e: any) { toast(String(e), 'error') }
  }
  const fixable = $derived((issues ?? []).some(i => i.fixable))
  const levelIcon: Record<string, string> = { error: 'ti-circle-x', warn: 'ti-alert-triangle', info: 'ti-info-circle', ok: 'ti-circle-check' }

  const CORE_NAMES: Record<string, string> = {
    vanilla: 'Vanilla', paper: 'Paper', purpur: 'Purpur', fabric: 'Fabric',
    forge: 'Forge', neoforge: 'NeoForge', quilt: 'Quilt',
  }
  function coreName(c: string) { return CORE_NAMES[c] ?? c }
  function fwdLabel(f: string) {
    return f === 'paper' ? t('proxy.fwdPaper') : f === 'fabric-proxy' ? 'FabricProxy-Lite' : f === 'pcf' ? 'Proxy Compatible Forge' : t('proxy.fwdNone')
  }

  const forwardingOptions = $derived<StOption[]>([
    { value: 'modern', label: t('proxy.fwd.modern') },
    { value: 'legacy', label: t('proxy.fwd.legacy') },
    { value: 'bungeeguard', label: t('proxy.fwd.bungeeguard') },
    { value: 'none', label: t('proxy.fwd.none') },
  ])
  const passthroughOptions = $derived<StOption[]>([
    { value: 'disabled', label: t('proxy.ping.disabled') },
    { value: 'mods', label: t('proxy.ping.mods') },
    { value: 'description', label: t('proxy.ping.description') },
    { value: 'all', label: t('proxy.ping.all') },
  ])

  type BoolKey = 'onlineMode' | 'forceKeyAuthentication' | 'preventClientProxyConnections' | 'announceForge' | 'kickExistingPlayers' | 'failoverOnUnexpectedDisconnect' | 'haproxyProtocol' | 'tcpFastOpen' | 'logCommandExecutions' | 'logPlayerConnections' | 'queryEnabled'
  type NumKey = 'showMaxPlayers' | 'compressionThreshold' | 'compressionLevel' | 'loginRatelimit' | 'connectionTimeout' | 'readTimeout' | 'queryPort'
  const mainSwitches: BoolKey[] = ['onlineMode', 'kickExistingPlayers', 'failoverOnUnexpectedDisconnect', 'announceForge']
  const advSwitches: BoolKey[] = ['forceKeyAuthentication', 'preventClientProxyConnections', 'haproxyProtocol', 'tcpFastOpen', 'logCommandExecutions', 'logPlayerConnections', 'queryEnabled']
  const advNumbers: NumKey[] = ['compressionThreshold', 'compressionLevel', 'loginRatelimit', 'connectionTimeout', 'readTimeout', 'queryPort']
</script>

{#if remoteServer}
<div class="px">
  <SCard icon="ti-server-2" title={t('proxy.members')} desc={t('proxy.remoteDesc')}>
    {#each remoteMembers as m (m.id)}
      {@const v = serverVisual({ core: (m.core ?? '').toLowerCase(), color: m.color, icon: m.icon, launcherIconData: m.img })}
      {@const ms = m.ownerOnline ? m.state || 'stopped' : 'stopped'}
      {@const mbusy = !!remote.pending[remoteKey(m)]}
      <div class="st-row">
        <span class="px-dot" class:on={ms === 'running'} class:mid={ms === 'starting' || ms === 'stopping'}></span>
        <span class="px-ic" style="--c:{v.color}">{#if v.src}<img src={v.src} alt="" />{:else}<i class="ti {v.ti}"></i>{/if}</span>
        <div class="st-row-text">
          <div class="st-row-name">{m.name}</div>
          <div class="st-row-hint">{m.core}{#if m.p2p && m.p2pAddr} · {m.p2pAddr}{/if}</div>
        </div>
        <div class="st-row-ctl">
          <span class="st-tag" class:ok={ms === 'running'}>{t('servers.status.' + ms)}</span>
          {#if ms === 'running' && m.canStop}
            <button class="btn btn-ghost btn-sm" disabled={mbusy || !m.ownerOnline} onclick={() => remoteAct(m, 'stop')}><i class="ti ti-player-stop"></i> {t('servers.actions.stop')}</button>
          {:else if (ms === 'stopped' || ms === 'crashed' || ms === 'error') && m.canStart}
            <button class="btn btn-primary btn-sm" disabled={mbusy || !m.ownerOnline} onclick={() => remoteAct(m, 'start')}><i class="ti ti-player-play"></i> {t('servers.actions.start')}</button>
          {/if}
        </div>
      </div>
    {:else}
      <div class="st-pad"><div class="st-note"><i class="ti ti-info-circle"></i><span>{t('proxy.remoteEmpty')}</span></div></div>
    {/each}
  </SCard>
</div>
{:else}
<div class="px">
  <!-- ═══ Стан мережі ═══ -->
  <SCard icon="ti-topology-star-3" tone="blue" title={t('proxy.title')} desc={t('proxy.desc')}>
    {#snippet actions()}
      {#if view?.proxyRunning || runningCount > 0}
        <button class="btn btn-ghost btn-sm" onclick={stopNetwork} disabled={netBusy}>
          {#if netBusy}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-player-stop"></i>{/if} {t('proxy.stop')}
        </button>
      {/if}
      <button class="btn btn-primary btn-sm" onclick={startNetwork} disabled={netBusy || members.length === 0}>
        {#if netBusy}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-player-play"></i>{/if} {t('proxy.start')}
      </button>
    {/snippet}
    {#if view}
      <div class="st-row">
        <div class="st-row-text">
          <div class="st-row-name">{t('proxy.address')}</div>
          <div class="st-row-hint">{t('proxy.addressHint')}</div>
        </div>
        <div class="st-row-ctl wrap">
          <span class="st-tag mono">127.0.0.1:{view.proxyPort}</span>
          <span class="st-tag" class:ok={view.proxyRunning}><i class="ti ti-router"></i> {view.proxyRunning ? t('proxy.proxyUp') : t('proxy.proxyDown')}</span>
          <span class="st-tag" class:ok={members.length > 0 && runningCount === members.length}>
            <i class="ti ti-server-2"></i> {t('proxy.membersRunning', { n: runningCount, total: members.length })}
          </span>
        </div>
      </div>
    {/if}
  </SCard>

  <!-- ═══ Сервери мережі ═══ -->
  <SCard icon="ti-server-2" title={t('proxy.members')} desc={t('proxy.membersDesc')}>
    {#each members as m, i (m.serverId)}
      {@const v = visualOf(m)}
      <div class="px-member" class:busy>
        <div class="px-head">
          <span class="px-dot" class:on={m.running} class:mid={m.status === 'starting'} title={m.status}></span>
          <span class="px-ic" style="--c:{v.color}">{#if v.src}<img src={v.src} alt="" />{:else}<i class="ti {v.ti}"></i>{/if}</span>
          <div class="px-name">
            <div class="px-title">{m.name}</div>
            <div class="px-sub mono">{coreName(m.core)} {m.mcVersion} · :{m.port} · {fwdLabel(m.forwarding)}</div>
          </div>
          <label class="px-alias" title={t('proxy.aliasHint')}>
            <span>/server</span>
            <input class="st-input mono" value={m.alias} maxlength="32" spellcheck="false"
                   onchange={(e) => setText(m, 'alias', (e.target as HTMLInputElement).value)} aria-label={t('proxy.alias')} />
          </label>
          <div class="px-tools">
            <button type="button" class="st-iconbtn" onclick={() => move(m, -1)} disabled={busy || i === 0} title={t('proxy.up')} aria-label={t('proxy.up')}><i class="ti ti-arrow-up"></i></button>
            <button type="button" class="st-iconbtn" onclick={() => move(m, 1)} disabled={busy || i === members.length - 1} title={t('proxy.down')} aria-label={t('proxy.down')}><i class="ti ti-arrow-down"></i></button>
            {#if onOpenServer}
              <button type="button" class="st-iconbtn" onclick={() => onOpenServer?.(m.serverId)} title={t('proxy.open')} aria-label={t('proxy.open')}><i class="ti ti-external-link"></i></button>
            {/if}
            <button type="button" class="st-iconbtn danger" onclick={() => remove(m)} disabled={busy} title={t('proxy.remove')} aria-label={t('proxy.remove')}><i class="ti ti-unlink"></i></button>
          </div>
        </div>
        <div class="px-opts">
          <button type="button" class="st-pill" class:sel={m.inTry} onclick={() => setFlag(m, 'inTry')} title={t('proxy.inTryHint')} aria-pressed={m.inTry}>
            <i class="ti ti-door-enter"></i> {t('proxy.inTry')}
          </button>
          <button type="button" class="st-pill" class:sel={m.autoStart} onclick={() => setFlag(m, 'autoStart')} title={t('proxy.autoStartHint')} aria-pressed={m.autoStart}>
            <i class="ti ti-player-play"></i> {t('proxy.autoStart')}
          </button>
          <button type="button" class="st-pill" class:sel={m.startsNetwork} onclick={() => setFlag(m, 'startsNetwork')} title={t('proxy.startsNetworkHint')} aria-pressed={m.startsNetwork}>
            <i class="ti ti-bolt"></i> {t('proxy.startsNetwork')}
          </button>
          <label class="px-field" title={t('proxy.delayHint')}>
            <i class="ti ti-clock"></i>
            <input class="st-input num mono" type="number" min="0" max="600" value={m.delaySec}
                   onchange={(e) => setDelay(m, (e.target as HTMLInputElement).value)} aria-label={t('proxy.delay')} />
            <span>{t('proxy.sec')}</span>
          </label>
          <label class="px-field grow" title={t('proxy.forcedHostHint')}>
            <i class="ti ti-world-www"></i>
            <input class="st-input mono" value={m.forcedHost} placeholder={t('proxy.forcedHostPlaceholder')} spellcheck="false"
                   onchange={(e) => setText(m, 'forcedHost', (e.target as HTMLInputElement).value)} aria-label={t('proxy.forcedHost')} />
          </label>
        </div>
      </div>
    {/each}
    {#each view?.external ?? [] as ex, i (ex.alias)}
      <div class="px-member">
        <div class="px-head">
          <span class="px-ic" style="--c:var(--text-mute)"><i class="ti ti-world"></i></span>
          <div class="px-name">
            <div class="px-title">{ex.alias}</div>
            <div class="px-sub mono">{ex.address} · {t('proxy.external')}</div>
          </div>
          <button type="button" class="st-pill" class:sel={ex.inTry} onclick={() => toggleExternalTry(i)} disabled={busy} aria-pressed={ex.inTry} title={t('proxy.inTryHint')}>
            <i class="ti ti-door-enter"></i> {t('proxy.inTry')}
          </button>
          <div class="px-tools">
            <button type="button" class="st-iconbtn danger" onclick={() => removeExternal(i)} disabled={busy} title={t('proxy.remove')} aria-label={t('proxy.remove')}><i class="ti ti-unlink"></i></button>
          </div>
        </div>
      </div>
    {/each}
    <div class="st-row">
      <div class="st-row-text">
        <div class="st-row-name">{t('proxy.addExternal')}</div>
        <div class="st-row-hint">{t('proxy.addExternalHint')}</div>
      </div>
      <div class="st-row-ctl wrap">
        <input class="st-input mono" style="width:130px" placeholder={t('proxy.alias')} bind:value={extAlias} maxlength="32" spellcheck="false" aria-label={t('proxy.alias')} />
        <input class="st-input mono" style="width:170px" placeholder="192.168.1.10:25565" bind:value={extAddr} spellcheck="false" aria-label={t('proxy.externalAddr')} />
        <button type="button" class="st-iconbtn" onclick={addExternal} disabled={busy || !extAlias.trim() || !extAddr.trim()} title={t('proxy.addExternal')} aria-label={t('proxy.addExternal')}><i class="ti ti-plus"></i></button>
      </div>
    </div>
    <div class="st-row">
      <div class="st-row-text">
        <div class="st-row-name">{t('proxy.add')}</div>
        {#if members.length === 0}<div class="st-row-hint">{t('proxy.empty')}</div>{/if}
      </div>
      <div class="st-row-ctl">
        <button type="button" class="btn btn-primary btn-sm" onclick={() => (pickerOpen = true)}
                disabled={busy || addCandidates.length === 0}>
          <i class="ti ti-plus"></i> {t('proxy.add')}
        </button>
        {#if addCandidates.length === 0}
          <span class="st-row-hint" style="margin:0">{t('proxy.noCandidates')}</span>
        {/if}
      </div>
    </div>
  </SCard>

  <!-- ═══ Діагностика ═══ -->
  <SCard icon="ti-stethoscope" tone="green" title={t('proxy.diag')} desc={t('proxy.diagDesc')}>
    {#snippet actions()}
      <button class="btn btn-ghost btn-sm" onclick={showConfig}><i class="ti ti-file-code"></i> velocity.toml</button>
      {#if fixable}
        <button class="btn btn-ghost btn-sm" onclick={repair} disabled={repairing}>
          {#if repairing}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-tool"></i>{/if} {t('proxy.repair')}
        </button>
      {/if}
      <button class="btn btn-primary btn-sm" onclick={diagnose} disabled={diagnosing}>
        {#if diagnosing}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-player-play"></i>{/if} {t('proxy.check')}
      </button>
    {/snippet}
    {#if issues}
      {#each issues as it, i (i)}
        <div class="st-row px-issue {it.level}">
          <i class="ti {levelIcon[it.level]} px-lv"></i>
          <div class="st-row-text">
            <div class="st-row-name">{it.title}</div>
            {#if it.detail}<div class="st-row-hint">{it.detail}</div>{/if}
          </div>
          {#if it.fixable}<span class="st-tag on">{t('proxy.fixable')}</span>{/if}
        </div>
      {/each}
    {:else}
      <div class="st-pad"><div class="st-note"><i class="ti ti-info-circle"></i><span>{t('proxy.diagHint')}</span></div></div>
    {/if}
  </SCard>

  <!-- ═══ Налаштування проксі ═══ -->
  {#if settings}
    <SCard icon="ti-adjustments-horizontal" title={t('proxy.settings')} desc={t('proxy.settingsDesc')}>
      {#snippet actions()}
        {#if settingsDirty}
          <button class="btn btn-ghost btn-sm" onclick={resetSettings} disabled={settingsSaving}>{t('proxy.reset')}</button>
          <button class="btn btn-primary btn-sm" onclick={saveSettings} disabled={settingsSaving}>
            {#if settingsSaving}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-device-floppy"></i>{/if} {t('proxy.save')}
          </button>
        {/if}
      {/snippet}
      <div class="st-row">
        <div class="st-row-text"><div class="st-row-name">{t('proxy.port')}</div><div class="st-row-hint">{t('proxy.portHint')}</div></div>
        <div class="st-row-ctl"><input class="st-input num mono" type="number" min="1" max="65535" bind:value={settings.proxyPort} oninput={touch} aria-label={t('proxy.port')} /></div>
      </div>
      <div class="st-row">
        <div class="st-row-text"><div class="st-row-name">MOTD</div><div class="st-row-hint">{t('proxy.motdHint')}</div></div>
        <div class="st-row-ctl"><input class="st-input mono px-wide" bind:value={settings.proxy.motd} oninput={touch} aria-label="MOTD" /></div>
      </div>
      <div class="st-row">
        <div class="st-row-text"><div class="st-row-name">{t('proxy.k.showMaxPlayers')}</div></div>
        <div class="st-row-ctl"><input class="st-input num mono" type="number" min="0" bind:value={settings.proxy.showMaxPlayers} oninput={touch} aria-label={t('proxy.k.showMaxPlayers')} /></div>
      </div>
      <div class="st-row">
        <div class="st-row-text"><div class="st-row-name">{t('proxy.forwarding')}</div><div class="st-row-hint">{t('proxy.forwardingHint')}</div></div>
        <div class="st-row-ctl"><StSelect value={settings.proxy.forwardingMode} options={forwardingOptions} minWidth="210px" ariaLabel={t('proxy.forwarding')}
          onchange={(v) => { if (settings) { settings.proxy.forwardingMode = v as any; touch() } }} /></div>
      </div>
      <div class="st-row">
        <div class="st-row-text"><div class="st-row-name">{t('proxy.pingPassthrough')}</div><div class="st-row-hint">{t('proxy.pingPassthroughHint')}</div></div>
        <div class="st-row-ctl"><StSelect value={settings.proxy.pingPassthrough} options={passthroughOptions} minWidth="210px" ariaLabel={t('proxy.pingPassthrough')}
          onchange={(v) => { if (settings) { settings.proxy.pingPassthrough = v as any; touch() } }} /></div>
      </div>
      {#each mainSwitches as k (k)}
        <div class="st-row">
          <div class="st-row-text"><div class="st-row-name">{t('proxy.k.' + k)}</div><div class="st-row-hint">{t('proxy.h.' + k)}</div></div>
          <div class="st-row-ctl"><button type="button" class="st-switch" class:on={settings.proxy[k]} role="switch" aria-checked={settings.proxy[k]} aria-label={t('proxy.k.' + k)}
            onclick={() => { if (settings) { settings.proxy[k] = !settings.proxy[k]; touch() } }}></button></div>
        </div>
      {/each}
      <div class="st-row">
        <div class="st-row-text"><div class="st-row-name">{t('proxy.stopMembers')}</div><div class="st-row-hint">{t('proxy.stopMembersHint')}</div></div>
        <div class="st-row-ctl"><button type="button" class="st-switch" class:on={settings.stopMembersWithProxy} role="switch" aria-checked={settings.stopMembersWithProxy} aria-label={t('proxy.stopMembers')}
          onclick={() => { if (settings) { settings.stopMembersWithProxy = !settings.stopMembersWithProxy; touch() } }}></button></div>
      </div>
      <div class="st-row">
        <div class="st-row-text"><div class="st-row-name">{t('proxy.lock')}</div><div class="st-row-hint">{t('proxy.lockHint')}</div></div>
        <div class="st-row-ctl"><button type="button" class="st-switch" class:on={settings.lockBackends} role="switch" aria-checked={settings.lockBackends} aria-label={t('proxy.lock')}
          onclick={() => { if (settings) { settings.lockBackends = !settings.lockBackends; touch() } }}></button></div>
      </div>

      <button type="button" class="px-adv-toggle" onclick={() => showAdvanced = !showAdvanced} aria-expanded={showAdvanced}>
        <i class="ti {showAdvanced ? 'ti-chevron-down' : 'ti-chevron-right'}"></i> {t('proxy.advanced')}
      </button>
      {#if showAdvanced}
        {#each advNumbers as k (k)}
          <div class="st-row sub">
            <div class="st-row-text"><div class="st-row-name">{t('proxy.k.' + k)}</div><div class="st-row-hint">{t('proxy.h.' + k)}</div></div>
            <div class="st-row-ctl"><input class="st-input num mono" type="number" bind:value={settings.proxy[k]} oninput={touch} aria-label={t('proxy.k.' + k)} /></div>
          </div>
        {/each}
        {#each advSwitches as k (k)}
          <div class="st-row sub">
            <div class="st-row-text"><div class="st-row-name">{t('proxy.k.' + k)}</div><div class="st-row-hint">{t('proxy.h.' + k)}</div></div>
            <div class="st-row-ctl"><button type="button" class="st-switch" class:on={settings.proxy[k]} role="switch" aria-checked={settings.proxy[k]} aria-label={t('proxy.k.' + k)}
              onclick={() => { if (settings) { settings.proxy[k] = !settings.proxy[k]; touch() } }}></button></div>
          </div>
        {/each}
      {/if}
    </SCard>
  {/if}
</div>

{/if}

{#if pickerOpen}
  <UiModal open title={t('proxy.add')} subtitle={t('proxy.pick')} icon="ti-server-2" size="md" onclose={() => (pickerOpen = false)}>
    <div class="px-pick">
      {#each addCandidates as c (c.serverId)}
        {@const cv = visualOf(c)}
        <button type="button" class="px-pickrow" style="--c:{cv.color}" disabled={busy} onclick={() => add(c.serverId)}>
          <span class="px-ic" style="--c:{cv.color}">{#if cv.src}<img src={cv.src} alt="" />{:else}<i class="ti {cv.ti}"></i>{/if}</span>
          <span class="px-pickinfo">
            <span class="px-title">{c.name}</span>
            <span class="px-sub">{coreName(c.core)} {c.mcVersion}{c.forwarding ? '' : ' · ' + t('proxy.noForwardingShort')}</span>
          </span>
          <i class="ti ti-plus px-pickplus"></i>
        </button>
      {/each}
    </div>
  </UiModal>
{/if}

{#if configText !== null}
  <UiModal open title="velocity.toml" subtitle={t('proxy.configHint')} icon="ti-file-code" width="720px" onclose={() => configText = null}>
    <pre class="px-config">{configText}</pre>
  </UiModal>
{/if}

<style>
  .px { display: flex; flex-direction: column; gap: 14px; padding: 2px 2px 24px; }
  .px-member { padding: 12px 20px; border-top: 1px solid var(--border); display: flex; flex-direction: column; gap: 10px; }
  .px-member:first-child { border-top: 0; }
  .px-member.busy { opacity: .7; }
  .px-head { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
  .px-dot { width: 8px; height: 8px; border-radius: 50%; background: var(--text-dim); flex: none; }
  .px-dot.on { background: var(--green); box-shadow: 0 0 0 3px rgba(16, 185, 129, .18); }
  .px-dot.mid { background: var(--yellow); }
  .px-ic {
    width: 34px; height: 34px; flex: none; border-radius: 9px; overflow: hidden;
    display: flex; align-items: center; justify-content: center; font-size: 17px;
    color: var(--c); background: color-mix(in srgb, var(--c) 14%, transparent);
  }
  .px-ic img { width: 100%; height: 100%; object-fit: cover; }
  .px-name { flex: 1; min-width: 140px; }
  .px-title { font-size: 13.5px; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .px-sub { font-size: 11.5px; color: var(--text-mute); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .px-alias { display: inline-flex; align-items: center; gap: 6px; font: 600 12px var(--font-mono); color: var(--text-mute); }
  .px-alias input { width: 150px; }
  .px-tools { display: flex; gap: 4px; }
  .px-pick { display: flex; flex-direction: column; gap: 8px; }
  .px-pickrow {
    display: flex; align-items: center; gap: 12px; min-width: 0; padding: 10px 14px; text-align: left; cursor: pointer;
    border-radius: 12px; border: 1px solid var(--border); background: var(--bg-input); color: inherit; font: inherit;
    transition: background .15s, border-color .15s;
  }
  .px-pickrow:hover:not(:disabled) { background: color-mix(in srgb, var(--c) 10%, var(--hover)); border-color: color-mix(in srgb, var(--c) 45%, var(--border)); }
  .px-pickrow:disabled { opacity: .6; cursor: default; }
  .px-pickinfo { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
  .px-pickplus { color: var(--text-dim); font-size: 16px; }
  .px-pickrow:hover:not(:disabled) .px-pickplus { color: var(--c); }
  .px-opts { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; padding-left: 20px; }
  .px-opts .st-pill { padding: 6px 11px; font-size: 12px; }
  .px-field { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; color: var(--text-mute); }
  .px-field input.num { width: 70px; }
  .px-field.grow { flex: 1; min-width: 200px; }
  .px-field.grow input { flex: 1; min-width: 0; }
  .px-wide { width: 280px; max-width: 100%; }
  .px-issue { gap: 12px; }
  .px-lv { font-size: 18px; flex: none; }
  .px-issue.error .px-lv { color: var(--red); }
  .px-issue.warn .px-lv { color: var(--yellow); }
  .px-issue.info .px-lv { color: var(--text-mute); }
  .px-issue.ok .px-lv { color: var(--green); }
  .px-adv-toggle {
    display: flex; align-items: center; gap: 6px; width: 100%; padding: 12px 20px;
    border: 0; border-top: 1px solid var(--border); background: transparent;
    color: var(--text-mute); font: 600 12.5px var(--font-ui); cursor: pointer; text-align: left;
  }
  .px-adv-toggle:hover { color: var(--text); }
  .px-config {
    margin: 0; max-height: 60vh; overflow: auto; padding: 14px; border-radius: var(--r-md, 10px);
    background: var(--bg-input); border: 1px solid var(--border);
    font: 12px/1.55 var(--font-mono); color: var(--text); white-space: pre;
  }
</style>
