<script lang="ts">
  // ── Мережа Velocity: проксі + backend-сервери під одним дахом ─────────
  // Velocity-сервер є «центром» мережі: тут видно його самого + всі інші
  // сервери бібліотеки, можна вмикати їх у мережу (inNetwork), задавати
  // порядок запуску (order) і паузу перед стартом (delaySec), зберігати
  // склад (network.json біля проксі) і запускати/зупиняти ВСЮ мережу
  // однією кнопкою (кожен сервер стартує зі своєю Java, ядро доставляється
  // ліниво — як при звичайному старті).
  import { App, on } from './wails'
  import { toast, confirmDialog } from './toast.svelte'
  import type { NetworkView, NetworkMemberView } from './types'
  import papermcIcon from '../assets/images/papermc.svg'
  import purpurIcon from '../assets/images/purpur.png'
  import velocityIcon from '../assets/images/velocity.webp'

  let { proxyServerId = '' }: { proxyServerId?: string } = $props()

  let view = $state<NetworkView | null>(null)
  let loading = $state(true)
  let saving = $state(false)
  let networkBusy = $state(false) // старт/зупинка мережі в процесі
  let dirty = $state(false)

  const coreLabel: Record<string, string> = {
    vanilla: 'Vanilla', paper: 'Paper', purpur: 'Purpur', fabric: 'Fabric',
    forge: 'Forge', neoforge: 'NeoForge', quilt: 'Quilt', velocity: 'Velocity',
  }
  const coreColor: Record<string, string> = {
    vanilla: '#3aa675', paper: '#f7b731', purpur: '#ef4444', fabric: '#ff8a00',
    forge: '#d08b5b', neoforge: '#ff6b35', quilt: '#9d7cff', velocity: '#3b82f6',
  }
  const coreIcon: Record<string, string> = {
    vanilla: 'ti-cube', paper: 'ti-stack-2', purpur: 'ti-crown', fabric: 'ti-needle',
    forge: 'ti-anvil', neoforge: 'ti-anvil', quilt: 'ti-quotes', velocity: 'ti-router',
  }
  // Реальні іконки ядер (щойно додані в assets/images) — пріоритет над шрифт-іконкою.
  const coreImg: Record<string, string | undefined> = {
    paper: papermcIcon, purpur: purpurIcon, velocity: velocityIcon,
  }

  // ── Завантаження ──
  async function load() {
    loading = true
    try { view = await App.NetworkGetView(proxyServerId) } catch (e: any) { toast(String(e), 'error') }
    finally { loading = false }
  }

  const enabled = $derived((view?.members ?? []).filter(m => m.inNetwork))
  const enabledCount = $derived(enabled.length)
  const runningCount = $derived((view?.members ?? []).filter(m => m.inNetwork && m.running).length)
  const anyBusy = $derived(!!view?.proxyRunning || enabled.some(m => m.running) || networkBusy)
  const allUp = $derived(view?.proxyRunning && enabledCount > 0 && runningCount === enabledCount)
  // «Запустити» дозволено, поки є що запускати: проксі або хоч один
  // backend не працює (NetworkStart сам пропустить уже запущені).
  const canStart = $derived(!view?.proxyRunning || runningCount < enabledCount)

  // Живий статус: події server:status оновлюють прапорці running усіх
  // серверів мережі без повного перечитання списку (не застигає).
  $effect(() => {
    if (!proxyServerId) return
    load()
    const off = on('server:status', (data: any) => {
      if (!view) return
      const sid: string = data?.serverId
      const running = data?.status === 'running'
      if (sid === view.proxyServerId) {
        view.proxyRunning = running
        return
      }
      for (const m of view.members) {
        if (m.serverId === sid) m.running = running
      }
    })
    return () => { off() }
  })

  // ── Редагування ──
  function toggleIn(m: NetworkMemberView) {
    m.inNetwork = !m.inNetwork
    dirty = true
    if (m.inNetwork) {
      // порядок = наступне вільне місце після вже включених
      const used = new Set(enabled.map(x => x.order))
      let o = 1
      while (used.has(o)) o++
      m.order = o
      if (m.delaySec < 0) m.delaySec = 0
    }
  }

  function move(m: NetworkMemberView, dir: -1 | 1) {
    const list = enabled
    const idx = list.indexOf(m)
    const other = list[idx + dir]
    if (!other) return
    const tmp = m.order
    m.order = other.order
    other.order = tmp
    dirty = true
  }

  function normalizeDelays() {
    for (const m of view?.members ?? []) {
      if (!Number.isFinite(m.delaySec) || m.delaySec < 0) m.delaySec = 0
      if (!Number.isInteger(m.order) || m.order < 1) m.order = 1
    }
  }

  async function save(): Promise<boolean> {
    if (!view) return false
    normalizeDelays()
    saving = true
    try {
      await App.NetworkSave(view.proxyServerId, view.members.map(m => ({ ...m })))
      dirty = false
      toast('Склад мережі збережено', 'success')
      return true
    } catch (e: any) {
      toast(String(e), 'error')
      return false
    } finally { saving = false }
  }

  async function saveAndStart() {
    if (!view) return
    if (dirty || !view.networkSaved) {
      const ok = await save()
      if (!ok) return
    }
    await startNetwork()
  }

  async function startNetwork() {
    if (!view) return
    if (enabledCount === 0) { toast('Додай хоча б один сервер у мережу', 'error'); return }
    // Проксі вже працює (напр., запущений з консолі) — NetworkStart сам
    // це вміє: піднімає тільки backend-сервери, що ще не запущені.
    const starting = enabledCount - runningCount
    networkBusy = true
    toast(
      view.proxyRunning
        ? `Запуск ${starting > 0 ? starting + ' серверів мережі' : 'мережі'}…`
        : `Запуск мережі: проксі + ${enabledCount} серверів…`,
      'success',
    )
    try {
      await App.NetworkStart(view.proxyServerId)
      await load()
      toast('Мережу запущено', 'success')
    } catch (e: any) {
      toast(String(e), 'error')
    } finally { networkBusy = false }
  }

  async function stopNetwork() {
    if (!view) return
    const proceed = await confirmDialog(
      'Зупинити мережу?',
      `Будуть зупинені проксі та всі запущені сервери мережі (спочатку backend, проксі — останнім).`,
      { confirmLabel: 'Зупинити мережу', cancelLabel: 'Скасувати', danger: true },
    )
    if (!proceed) return
    networkBusy = true
    try {
      await App.NetworkStop(view.proxyServerId)
      await load()
      toast('Мережу зупинено', 'success')
    } catch (e: any) {
      toast(String(e), 'error')
    } finally { networkBusy = false }
  }

  function fmtVers(core: string, mc: string): string {
    if (core === 'velocity') return 'Проксі'
    return mc || '—'
  }
</script>

<div class="snp">
  {#if loading}
    <div class="snp-state"><i class="ti ti-loader spin"></i> Завантаження мережі…</div>

  {:else if !view}
    <div class="snp-state"><i class="ti ti-router"></i><div>Не вдалося завантажити мережу</div></div>

  {:else}
    <!-- ═══ Банер стану мережі ═══ -->
    <div class="snp-banner" class:up={allUp} class:partial={view.proxyRunning && !allUp}>
      <div class="snp-banner-ic"><i class="ti ti-router"></i></div>
      <div class="snp-banner-info">
        <div class="snp-banner-name">{view.proxyName} — Velocity мережа</div>
        <div class="snp-banner-sub">
          {#if allUp}
            <span class="dot ok"></span> вся мережа працює
          {:else if view.proxyRunning}
            <span class="dot warn"></span> проксі працює · сервери запускаються
          {:else}
            <span class="dot"></span> зупинено
          {/if}
          · {enabledCount} серверів у мережі · {runningCount} запущено
          {#if view.proxyPort}
            <span class="snp-port">порт проксі <b>:{view.proxyPort}</b></span>
          {/if}
        </div>
      </div>
      <div class="snp-banner-actions">
        <button
          class="btn btn-primary btn-sm"
          onclick={saveAndStart}
          disabled={networkBusy || saving || !canStart || enabledCount === 0}
          title={enabledCount === 0 ? 'Додай сервери в мережу' : allUp ? 'Усе вже запущено' : ''}
        >
          <i class="ti {networkBusy ? 'ti-loader spin' : 'ti-player-play'}"></i>
          {dirty ? 'Зберегти і запустити мережу' : allUp ? 'Мережу запущено' : 'Запустити мережу'}
        </button>
        <button class="btn btn-danger btn-sm" onclick={stopNetwork} disabled={networkBusy || !anyBusy || !view.proxyRunning && runningCount === 0}>
          <i class="ti ti-player-stop"></i> Зупинити мережу
        </button>
      </div>
    </div>

    <!-- ═══ Проксі (центр) ═══ -->
    <div class="snp-row proxy">
      <span class="snp-role-chip">проксі</span>
      <span class="snp-ic" style="--cc:{coreColor['velocity']}">{#if coreImg['velocity']}<img class="snp-ic-img" src={coreImg['velocity']} alt="" />{:else}<i class="ti ti-router"></i>{/if}</span>
      <div class="snp-info">
        <div class="snp-name">{view.proxyName}</div>
        <div class="snp-meta">Velocity · порт у server.properties</div>
      </div>
      <span class="snp-pill" class:on={view.proxyRunning}>
        <span class="dot {view.proxyRunning ? 'ok' : ''}"></span>{view.proxyRunning ? 'працює' : 'зупинено'}
      </span>
    </div>

    <!-- ═══ Backend-сервери ═══ -->
    <div class="snp-members-head">
      <div class="snp-members-title"><i class="ti ti-server-2"></i> Сервери мережі</div>
      <div class="snp-members-hint">увімкни сервери, задай порядок і паузи — потім «Запустити мережу»</div>
      <button class="btn btn-ghost btn-sm" onclick={save} disabled={saving || !dirty}>
        <i class="ti {saving ? 'ti-loader spin' : 'ti-device-floppy'}"></i> Зберегти
        {#if dirty}<span class="snp-dirty-dot"></span>{/if}
      </button>
    </div>

    {#if view.members.length === 0}
      <div class="snp-state small"><i class="ti ti-server-off"></i><div>Інших серверів немає — створи сервер Paper/Fabric тощо, щоб додати його до мережі.</div></div>
    {:else}
      <div class="snp-list">
        {#each [...enabled].sort((a, b) => a.order - b.order) as m}
          <div class="snp-row" class:off={!m.inNetwork}>
            <span class="snp-order">{m.order}</span>
            <span class="snp-ic" style="--cc:{coreColor[m.core] || '#999'}">
              {#if m.inNetwork && coreImg[m.core]}
                <img class="snp-ic-img" src={coreImg[m.core]} alt="" />
              {:else}
                <i class="ti {m.inNetwork ? (coreIcon[m.core] || 'ti-server') : 'ti-ban'}"></i>
              {/if}
            </span>
            <div class="snp-info">
              <div class="snp-name">{m.name}</div>
              <div class="snp-meta">{coreLabel[m.core] || m.core} · MC {fmtVers(m.core, m.mcVersion)} · порт {m.port}</div>
            </div>
            <span class="snp-pill" class:on={m.running}>
              <span class="dot {m.running ? 'ok' : ''}"></span>{m.running ? 'працює' : 'зупинено'}
            </span>
            {#if m.inNetwork}
              <div class="snp-arr">
                <button class="snp-mini" onclick={() => move(m, -1)} disabled={m.order <= 1} title="Вище у порядку запуску" aria-label="Вище"><i class="ti ti-chevron-up"></i></button>
                <button class="snp-mini" onclick={() => move(m, 1)} disabled={m.order >= enabledCount} title="Нижче у порядку запуску" aria-label="Нижче"><i class="ti ti-chevron-down"></i></button>
              </div>
              <div class="snp-delay">
                <label for="delay-{m.serverId}">пауза</label>
                <input id="delay-{m.serverId}" class="snp-num" type="number" min="0" max="600" bind:value={m.delaySec}
                  onchange={() => { if (m.delaySec < 0) m.delaySec = 0; dirty = true }} />
                <span>с</span>
              </div>
            {/if}
            <button class="snp-switch" class:on={m.inNetwork} onclick={() => toggleIn(m)}
              title={m.inNetwork ? 'Прибрати з мережі' : 'Додати в мережу'} role="switch" aria-checked={m.inNetwork}>
              <span class="snp-knob"></span>
            </button>
          </div>
        {/each}
      </div>
    {/if}

    <div class="snp-note">
      <i class="ti ti-info-circle"></i>
      Запуск мережі: спершу стартує проксі, потім backend-сервери у порядку черги (з паузами).
      Кожен сервер стартує зі своєю Java; якщо ядро ще не встановлене — воно доставиться автоматично.
      Склад мережі зберігається у <span class="mono">.shaurma/network.json</span> біля проксі.
    </div>
  {/if}
</div>

<style>
  .snp { display: flex; flex-direction: column; gap: 10px; height: 100%; overflow-y: auto; padding-right: 4px; }
  .snp-state { display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 10px; padding: 60px 20px; color: var(--text-mute); font-size: 13px; text-align: center; }
  .snp-state i { font-size: 34px; color: var(--text-dim); }
  .snp-state.small { padding: 26px 16px; font-size: 12px; }

  /* ── Банер стану мережі ── */
  .snp-banner {
    display: flex; align-items: center; gap: 12px;
    padding: 12px 14px; border-radius: 12px;
    background: linear-gradient(135deg, rgba(59,130,246,.14), rgba(59,130,246,.03));
    border: 1px solid rgba(59,130,246,.28);
  }
  .snp-banner.up { background: linear-gradient(135deg, rgba(34,197,94,.14), rgba(34,197,94,.03)); border-color: rgba(34,197,94,.3); }
  .snp-banner.partial { background: linear-gradient(135deg, rgba(234,179,8,.12), rgba(234,179,8,.02)); border-color: rgba(234,179,8,.26); }
  .snp-banner-ic {
    width: 40px; height: 40px; border-radius: 11px; flex-shrink: 0;
    display: flex; align-items: center; justify-content: center;
    background: rgba(59,130,246,.16); color: #60a5fa; font-size: 20px;
  }
  .snp-banner.up .snp-banner-ic { background: rgba(34,197,94,.14); color: #4ade80; }
  .snp-banner-info { flex: 1; min-width: 0; }
  .snp-banner-name { font: 700 13px/1.3 var(--font-ui); color: var(--text); }  .snp-port { font-family: var(--font-mono); font-size: 10.5px; color: var(--text-dim); }
  .snp-port b { color: var(--text-mute); }
  .snp-banner-sub {
    display: flex;
    align-items: center; gap: 6px; font: 400 11px/1.5 var(--font-ui); color: var(--text-mute); margin-top: 2px; }
  .snp-banner-actions { flex-shrink: 0; }
  .dot { width: 7px; height: 7px; border-radius: 50%; background: #6b7280; display: inline-block; flex-shrink: 0; }
  .dot.ok { background: #22c55e; box-shadow: 0 0 6px rgba(34,197,94,.6); }
  .dot.warn { background: #eab308; box-shadow: 0 0 6px rgba(234,179,8,.6); animation: snp-blink 1.1s ease-in-out infinite; }
  @keyframes snp-blink { 0%,100% { opacity: 1 } 50% { opacity: .35 } }

  /* ── Рядки ── */
  .snp-members-head { display: flex; align-items: center; gap: 8px; margin-top: 4px; flex-wrap: wrap; }
  .snp-members-title { display: flex; align-items: center; gap: 6px; font: 700 12.5px/1 var(--font-ui); color: var(--text); }
  .snp-members-title i { color: var(--pcol, #3b82f6); font-size: 15px; }
  .snp-members-hint { font: 400 10.5px/1.4 var(--font-ui); color: var(--text-dim); flex: 1; min-width: 140px; }
  .snp-dirty-dot { width: 7px; height: 7px; border-radius: 50%; background: #eab308; display: inline-block; margin-left: 2px; }

  .snp-list { display: flex; flex-direction: column; gap: 5px; }
  .snp-row {
    display: flex; align-items: center; gap: 10px;
    padding: 8px 10px; border-radius: 10px;
    background: var(--bg-card); border: 1px solid var(--border);
    transition: border-color .14s, opacity .14s, background .14s;
  }
  .snp-row:hover { border-color: var(--border-hi); }
  .snp-row.off { opacity: .62; }
  .snp-row.proxy { border-color: rgba(59,130,246,.35); background: color-mix(in srgb, #3b82f6 6%, var(--bg-card)); }
  .snp-order {
    min-width: 22px; height: 22px; border-radius: 7px; flex-shrink: 0;
    display: flex; align-items: center; justify-content: center;
    font: 700 11px var(--font-mono); color: var(--text);
    background: color-mix(in srgb, var(--pcol, #3b82f6) 16%, transparent);
  }
  .snp-row.off .snp-order { display: none; }
  .snp-role-chip {
    font: 700 8.5px/1 var(--font-ui); text-transform: uppercase; letter-spacing: .5px;
    color: #93c5fd; background: rgba(59,130,246,.16);
    padding: 3px 6px; border-radius: 5px; flex-shrink: 0;
  }
  .snp-ic {
    width: 32px; height: 32px; border-radius: 9px; flex-shrink: 0;
    display: flex; align-items: center; justify-content: center;
    background: color-mix(in srgb, var(--cc) 14%, transparent);
    color: var(--cc); font-size: 15px;
    overflow: hidden;
  }
  .snp-ic .snp-ic-img { width: 100%; height: 100%; object-fit: contain; }
  .snp-info { flex: 1; min-width: 0; }
  .snp-name { font: 600 12.5px/1.3 var(--font-ui); color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .snp-meta { font: 400 10.5px/1.3 var(--font-mono); color: var(--text-mute); margin-top: 1px; }

  .snp-pill {
    display: inline-flex; align-items: center; gap: 5px;
    font: 500 10px/1 var(--font-ui); color: var(--text-mute);
    background: var(--hover); padding: 4px 9px; border-radius: 999px; flex-shrink: 0;
  }
  .snp-pill.on { color: #4ade80; background: rgba(34,197,94,.1); }

  .snp-arr { display: flex; gap: 2px; flex-shrink: 0; }
  .snp-mini {
    width: 22px; height: 22px; border-radius: 6px; border: 1px solid var(--border);
    background: var(--bg-input); color: var(--text-mute); font-size: 12px;
    display: flex; align-items: center; justify-content: center; cursor: pointer;
  }
  .snp-mini:hover:not(:disabled) { background: var(--hover-strong); color: var(--text); }
  .snp-mini:disabled { opacity: .35; cursor: default; }

  .snp-delay { display: flex; align-items: center; gap: 4px; flex-shrink: 0; font: 400 10px/1 var(--font-ui); color: var(--text-dim); }
  .snp-delay label { cursor: default; }
  .snp-num {
    width: 46px; padding: 3px 4px; border-radius: 6px;
    background: var(--bg-input); border: 1px solid var(--border);
    color: var(--text); font: 500 11px var(--font-mono); text-align: center;
  }
  .snp-num:focus { outline: none; border-color: var(--pcol, #3b82f6); }

  /* ── Перемикач ── */
  .snp-switch {
    width: 34px; height: 19px; border-radius: 999px; border: 1px solid var(--border);
    background: var(--hover-strong); position: relative; cursor: pointer;
    transition: background .15s, border-color .15s; flex-shrink: 0; padding: 0;
  }
  .snp-switch.on { background: #3b82f6; border-color: #3b82f6; }
  .snp-knob {
    position: absolute; top: 2px; left: 2px; width: 13px; height: 13px;
    border-radius: 50%; background: #cbd5e1; transition: left .15s, background .15s;
  }
  .snp-switch.on .snp-knob { left: 17px; background: #fff; }

  .snp-note {
    display: flex; gap: 7px; align-items: flex-start;
    font: 400 10.5px/1.55 var(--font-ui); color: var(--text-dim);
    padding: 8px 10px; border-radius: 9px; background: var(--hover);
    border: 1px dashed var(--border);
  }
  .snp-note i { color: var(--pcol, #3b82f6); margin-top: 1px; }
  .mono { font-family: var(--font-mono); }
</style>
