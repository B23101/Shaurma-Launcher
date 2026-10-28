<script lang="ts">
  // ══════════════════════════════════════════════════════════════════════
  // «Пристрої» — де відкрито акаунт Шаурми (ЛИШЕ Shaurma-редакція).
  //
  // Показує для кожного пристрою: назву, ОС, тип клієнта (лаунчер/браузер) і версію, IP, країну,
  // коли додано й остання активність, онлайн. Дії: перейменувати, вилучити, «Вийти на всіх, крім
  // цього» (10 хв діє «Повернути»), «Це не я», екстрена ротація, журнал подій.
  // Вихід/вилучення НІЧОГО не видаляє: усе повертається новим входом через Google/Microsoft.
  // Бекенд: shaurma_devices_shaurma.go → VPS /api/account/devices*.
  // ══════════════════════════════════════════════════════════════════════
  import { onMount } from 'svelte'
  import { App, on } from './wails'
  import type { ShaurmaDevice, ShaurmaDeviceList, ShaurmaAuditEvent } from './wails'
  import { toast, confirmDialog } from './toast.svelte'
  import { t } from './i18n.svelte'

  let list = $state<ShaurmaDeviceList | null>(null)
  let loading = $state(true)
  let failed = $state('')
  let busy = $state('')
  let editing = $state('')
  let nameDraft = $state('')
  let showAudit = $state(false)
  let showRemoved = $state(false)
  let audit = $state<ShaurmaAuditEvent[] | null>(null)
  let now = $state(Date.now())

  const active = $derived((list?.devices ?? []).filter((d) => d.status === 'active'))
  const removed = $derived((list?.devices ?? []).filter((d) => d.status !== 'active'))
  const canRestore = $derived(!!list && list.restoreUntil > now)

  async function load(silent = false) {
    if (!silent) loading = true
    failed = ''
    try {
      list = await App.GetShaurmaDevices()
      if (showAudit) audit = await App.GetShaurmaAudit(50)
    } catch (e: any) {
      failed = String(e?.message ?? e)
    } finally { loading = false }
  }

  function fmtTime(ms: number): string {
    if (!ms) return t('dev.never')
    const diff = Date.now() - ms
    if (diff < 60_000) return t('dev.justNow')
    return new Date(ms).toLocaleString([], { day: '2-digit', month: '2-digit', year: 'numeric', hour: '2-digit', minute: '2-digit' })
  }
  function kindLabel(d: ShaurmaDevice): string {
    const k = d.clientKind === 'launcher' || d.clientKind === 'browser' ? d.clientKind : 'unknown'
    const base = d.clientName || t(`dev.kind.${k}`)
    return d.clientKind === 'launcher' && d.clientVersion ? `${base} ${d.clientVersion}` : base
  }
  function icon(d: ShaurmaDevice): string {
    if (d.clientKind === 'browser') return 'ti-world'
    const os = (d.os || '').toLowerCase()
    if (os.includes('mac')) return 'ti-brand-apple'
    if (os.includes('linux')) return 'ti-brand-ubuntu'
    return 'ti-device-desktop'
  }
  function reasonLabel(r: string): string {
    const k = `dev.reason.${r}`
    const s = t(k)
    return s === k ? '' : s
  }

  async function run(key: string, fn: () => Promise<void>) {
    if (busy) return
    busy = key
    try { await fn(); await load(true) }
    catch (e: any) { toast(String(e?.message ?? e), 'error') }
    finally { busy = '' }
  }

  function startRename(d: ShaurmaDevice) { editing = d.id; nameDraft = d.name }
  async function saveRename(d: ShaurmaDevice) {
    const n = nameDraft.trim()
    if (!n) return
    await run('rename:' + d.id, async () => { await App.RenameShaurmaDevice(d.id, n); editing = '' })
  }

  async function revoke(d: ShaurmaDevice) {
    const msg = d.current ? t('dev.revokeThisConfirm') : t('dev.revokeConfirm', { name: d.name })
    if (!(await confirmDialog(t('dev.revokeConfirmTitle'), msg, { confirmLabel: t('dev.revoke'), cancelLabel: t('cancel') }))) return
    await run('revoke:' + d.id, () => App.RevokeShaurmaDevice(d.id, false))
  }
  async function notMe(d: ShaurmaDevice) {
    if (!(await confirmDialog(t('dev.notMeConfirmTitle'), t('dev.notMeConfirm', { name: d.name }), { confirmLabel: t('dev.notMe'), cancelLabel: t('cancel') }))) return
    await run('notme:' + d.id, () => App.RevokeShaurmaDevice(d.id, true))
  }
  async function revokeOthers() {
    if (!(await confirmDialog(t('dev.revokeOthersConfirmTitle'), t('dev.revokeOthersConfirm'), { confirmLabel: t('dev.revokeOthers'), cancelLabel: t('cancel') }))) return
    // Подвійне підтвердження: випадковий «Вийти скрізь» не має проходити з одного кліку.
    if (!(await confirmDialog(t('dev.revokeOthersConfirmTitle'), t('dev.revokeOthersConfirm'), { confirmLabel: t('dev.revokeOthers'), cancelLabel: t('cancel') }))) return
    await run('others', async () => {
      const r = await App.RevokeOtherShaurmaDevices()
      toast(t('dev.revokedN', { n: r.revoked }), 'info')
    })
  }
  async function restore() {
    await run('restore', async () => { toast(t('dev.restoredN', { n: await App.RestoreShaurmaDevices() }), 'success') })
  }
  async function emergency() {
    if (!(await confirmDialog(t('dev.emergencyConfirmTitle'), t('dev.emergencyConfirm'), { confirmLabel: t('dev.emergency'), cancelLabel: t('cancel') }))) return
    await run('emergency', async () => { await App.ShaurmaEmergencyReset(); toast(t('dev.emergencyDone'), 'success') })
  }
  async function toggleAudit() {
    showAudit = !showAudit
    if (showAudit && !audit) {
      try { audit = await App.GetShaurmaAudit(50) } catch (e: any) { audit = []; toast(String(e?.message ?? e), 'error') }
    }
  }
  function evLabel(e: ShaurmaAuditEvent): string {
    const k = `dev.ev.${e.event}`
    const s = t(k)
    return s === k ? e.event : s
  }
  function devName(id: string): string {
    return list?.devices.find((d) => d.id === id)?.name ?? ''
  }

  onMount(() => {
    load()
    const tick = setInterval(() => { now = Date.now() }, 15_000)
    // Сервер повідомляє, що склад пристроїв змінився (інший пристрій додано/вилучено).
    const offs = [
      on('account:devices_changed', () => load(true)),
      on('account:device_added', () => load(true)),
      on('shaurma-account:devices-changed', () => load(true)),
    ]
    return () => { clearInterval(tick); offs.forEach((o) => o && o()) }
  })
</script>

<section class="dv">
  <header class="dv-head">
    <h4><i class="ti ti-devices"></i> {t('dev.title')}</h4>
    {#if list}<span class="dv-count">{t('dev.count', { n: active.length, max: list.max })}</span>{/if}
  </header>
  <p class="dv-intro">{t('dev.intro')}</p>

  {#if loading}
    <div class="dv-dim"><div class="spinner" style="width:14px;height:14px"></div></div>
  {:else if failed}
    <div class="dv-err">
      <i class="ti ti-alert-triangle"></i> {t('dev.loadFailed')}: {failed}
      <button class="btn btn-ghost btn-sm" onclick={() => load()}>{t('dev.retry')}</button>
    </div>
  {:else if list}
    <ul class="dv-list">
      {#each active as d (d.id)}
        <li class="dv-item" class:cur={d.current}>
          <div class="dv-ico"><i class="ti {icon(d)}"></i></div>

          <div class="dv-main">
            {#if editing === d.id}
              <div class="dv-edit">
                <input class="input" maxlength="40" placeholder={t('dev.renamePh')} bind:value={nameDraft}
                  onkeydown={(e: any) => e.key === 'Enter' ? saveRename(d) : e.key === 'Escape' && (editing = '')} />
                <button class="btn btn-primary btn-sm" disabled={!nameDraft.trim() || !!busy} onclick={() => saveRename(d)}>{t('dev.save')}</button>
                <button class="btn btn-ghost btn-sm" onclick={() => (editing = '')}>{t('cancel')}</button>
              </div>
            {:else}
              <div class="dv-name">
                <b>{d.name}</b>
                {#if d.current}<span class="dv-badge cur">{t('dev.thisDevice')}</span>{/if}
                <span class="dv-badge {d.online ? 'on' : 'off'}"><i class="dv-dot"></i>{d.online ? t('dev.online') : t('dev.offline')}</span>
              </div>
            {/if}

            <div class="dv-chips">
              <span class="dv-chip"><i class="ti ti-app-window"></i>{kindLabel(d)}</span>
              {#if d.os}<span class="dv-chip"><i class="ti ti-brand-windows"></i>{d.os}</span>{/if}
              {#if d.lastIp}<span class="dv-chip mono" title={t('dev.ip')}><i class="ti ti-network"></i>{d.lastIp}</span>{/if}
              {#if d.lastCountry}<span class="dv-chip" title={t('dev.country')}><i class="ti ti-map-pin"></i>{d.lastCountry}</span>{/if}
            </div>

            <div class="dv-foot">
              <span><i class="ti ti-clock"></i> {t('dev.lastSeen')}: <b>{d.online ? t('dev.justNow') : fmtTime(d.lastSeenAt)}</b></span>
              <span><i class="ti ti-calendar-plus"></i> {t('dev.added')}: <b>{fmtTime(d.createdAt)}</b>{#if d.createdIp && d.createdIp !== d.lastIp}<span class="mono"> · {d.createdIp}{d.createdCountry ? ` (${d.createdCountry})` : ''}</span>{/if}</span>
            </div>
          </div>

          <div class="dv-acts">
            <button class="dv-ib" disabled={!!busy} onclick={() => startRename(d)} title={t('dev.rename')} aria-label={t('dev.rename')}><i class="ti ti-pencil"></i></button>
            {#if !d.current}
              <button class="btn btn-ghost btn-sm" disabled={!!busy} onclick={() => notMe(d)}><i class="ti ti-alert-octagon"></i> {t('dev.notMe')}</button>
            {/if}
            <button class="btn btn-danger btn-sm" disabled={!!busy} onclick={() => revoke(d)}>
              <i class="ti ti-logout"></i> {d.current ? t('dev.revokeThis') : t('dev.revoke')}
            </button>
          </div>
        </li>
      {/each}
    </ul>

    {#if active.length > 1 || canRestore}
      <div class="dv-bar">
        {#if active.length > 1}
          <button class="btn btn-ghost btn-sm" disabled={!!busy} onclick={revokeOthers}><i class="ti ti-logout-2"></i> {t('dev.revokeOthers')}</button>
        {/if}
        {#if canRestore}
          <button class="btn btn-primary btn-sm" disabled={!!busy} onclick={restore}><i class="ti ti-arrow-back-up"></i> {t('dev.restore')}</button>
          <span class="dv-dim">{t('dev.restoreUntil', { time: new Date(list.restoreUntil).toLocaleTimeString() })}</span>
        {/if}
      </div>
    {/if}

    {#if removed.length}
      <button class="dv-toggle" onclick={() => (showRemoved = !showRemoved)} aria-expanded={showRemoved}>
        <i class="ti {showRemoved ? 'ti-chevron-down' : 'ti-chevron-right'}"></i> {t('dev.removedTitle')} ({removed.length})
      </button>
      {#if showRemoved}
        <ul class="dv-list removed">
          {#each removed as d (d.id)}
            <li class="dv-item gone">
              <div class="dv-ico"><i class="ti {icon(d)}"></i></div>
              <div class="dv-main">
                <div class="dv-name"><b>{d.name}</b> <span class="dv-badge off">{t('dev.revokedBadge')}</span></div>
                <div class="dv-foot">
                  <span>{fmtTime(d.revokedAt)}</span>
                  {#if reasonLabel(d.revokeReason)}<span>{reasonLabel(d.revokeReason)}</span>{/if}
                  {#if d.lastIp}<span class="mono"><i class="ti ti-network"></i> {d.lastIp}</span>{/if}
                </div>
              </div>
            </li>
          {/each}
        </ul>
      {/if}
    {/if}

    <div class="dv-sec">
      <div class="dv-sec-ic"><i class="ti ti-shield-exclamation"></i></div>
      <div class="dv-sec-txt">
        <b>{t('dev.securityTitle')}</b>
        <span>{t('dev.securityText')}</span>
      </div>
      <button class="btn btn-danger btn-sm" disabled={!!busy} onclick={emergency}>{t('dev.emergency')}</button>
    </div>

    <div class="dv-end">
      <p class="dv-privacy"><i class="ti ti-eye-off"></i> {t('dev.privacy')}</p>
      <button class="btn btn-ghost btn-sm" onclick={toggleAudit}>
        <i class="ti ti-history"></i> {showAudit ? t('dev.auditHide') : t('dev.auditShow')}
      </button>
    </div>

    {#if showAudit}
      {#if !audit}<div class="dv-dim"><div class="spinner" style="width:14px;height:14px"></div></div>
      {:else if !audit.length}<div class="dv-dim">{t('dev.auditEmpty')}</div>
      {:else}
        <ul class="dv-audit">
          {#each audit as e (e.id)}
            <li>
              <time>{fmtTime(e.at)}</time>
              <b>{evLabel(e)}</b>
              {#if devName(e.deviceId)}<span>«{devName(e.deviceId)}»</span>{/if}
              {#if reasonLabel(e.detail)}<span class="dim">{reasonLabel(e.detail)}</span>{/if}
              {#if e.ip}<span class="dim">{e.ip}{e.country ? ' · ' + e.country : ''}</span>{/if}
            </li>
          {/each}
        </ul>
      {/if}
    {/if}
  {/if}
</section>

<style>
  /* Власна картка (раніше обгортка .card у батька давала інший вигляд, ніж решта блоків «Акаунта»). */
  .dv { display:flex; flex-direction:column; gap:12px; padding:16px; border-radius:16px; background:var(--bg-card); border:1px solid var(--border); }
  .dv-head { display:flex; align-items:center; justify-content:space-between; gap:8px; }
  .dv-head h4 { margin:0; font:700 13px/1 var(--font-ui); display:flex; align-items:center; gap:8px; color:var(--text); }
  .dv-head h4 i { color:var(--orange); font-size:17px; }
  .dv-count { font:600 11px/1 var(--font-mono); color:var(--text-dim); background:var(--bg-input); border-radius:999px; padding:4px 9px; }
  .dv-intro { margin:0; font-size:12px; line-height:1.5; color:var(--text-mute); }
  .dv-dim { font-size:12px; color:var(--text-dim); display:flex; align-items:center; gap:8px; }
  .dv-err { display:flex; align-items:center; gap:8px; flex-wrap:wrap; font-size:12.5px; color:var(--red); }
  .mono { font-family:var(--font-mono); }

  .dv-list { list-style:none; margin:0; padding:0; display:flex; flex-direction:column; gap:8px; }
  .dv-item { display:grid; grid-template-columns:40px minmax(0,1fr) auto; gap:6px 14px; align-items:center; padding:14px; border-radius:14px; background:var(--bg-input); border:1px solid var(--border); }
  .dv-item.cur { border-color:rgba(var(--orange-rgb),.45); }
  .dv-item.gone { opacity:.7; grid-template-columns:40px minmax(0,1fr); }
  .dv-ico { width:40px; height:40px; border-radius:11px; display:grid; place-items:center; background:var(--bg-panel); color:var(--orange); font-size:20px; }
  .dv-main { min-width:0; display:flex; flex-direction:column; gap:7px; }
  .dv-name { display:flex; align-items:center; gap:7px; flex-wrap:wrap; }
  .dv-name b { font:700 14px/1.2 var(--font-ui); color:var(--text); overflow-wrap:anywhere; }
  .dv-badge { display:inline-flex; align-items:center; gap:5px; font:600 var(--fs-min)/1 var(--font-ui); padding:4px 8px; border-radius:999px; background:var(--bg-panel); color:var(--text-mute); }
  .dv-badge.cur { background:rgba(var(--orange-rgb),.15); color:var(--orange); }
  .dv-badge.on { background:rgba(46,204,113,.14); color:#2ecc71; }
  .dv-badge.off { color:var(--text-dim); }
  .dv-dot { width:6px; height:6px; border-radius:50%; background:currentColor; }
  .dv-badge.off .dv-dot { display:none; }

  .dv-chips { display:flex; flex-wrap:wrap; gap:6px; }
  .dv-chip { display:inline-flex; align-items:center; gap:5px; font-size:11.5px; line-height:1; padding:5px 9px; border-radius:8px; background:var(--bg-panel); color:var(--text-mute); border:1px solid var(--border); }
  .dv-chip i { color:var(--text-dim); font-size:13px; }
  .dv-foot { display:flex; flex-wrap:wrap; gap:3px 16px; font-size:11.5px; color:var(--text-dim); }
  .dv-foot b { font-weight:600; color:var(--text-mute); }
  .dv-foot i { margin-right:2px; }

  .dv-acts { display:flex; align-items:center; gap:6px; flex-wrap:wrap; justify-content:flex-end; }
  .dv-ib { width:30px; height:30px; display:grid; place-items:center; border-radius:8px; background:transparent; border:1px solid var(--border); color:var(--text-mute); cursor:pointer; }
  .dv-ib:hover:not(:disabled) { color:var(--text); border-color:var(--text-dim); }
  .dv-ib:disabled { opacity:.5; cursor:default; }
  .dv-edit { display:flex; gap:6px; flex-wrap:wrap; }
  .dv-edit .input { flex:1; min-width:160px; }

  .dv-bar { display:flex; flex-wrap:wrap; gap:8px; align-items:center; }
  .dv-toggle { align-self:flex-start; display:inline-flex; align-items:center; gap:6px; background:none; border:none; padding:2px 0; font:600 12px/1 var(--font-ui); color:var(--text-mute); cursor:pointer; }
  .dv-toggle:hover { color:var(--text); }

  /* «Мій акаунт скомпрометовано» — окрема зона безпеки з поясненням, а не одинока кнопка серед списку. */
  .dv-sec { display:flex; align-items:center; gap:12px; padding:12px 14px; border-radius:12px; background:rgba(255,80,80,.06); border:1px solid rgba(255,80,80,.25); }
  .dv-sec-ic { font-size:20px; color:var(--red); }
  .dv-sec-txt { flex:1; min-width:0; display:flex; flex-direction:column; gap:3px; }
  .dv-sec-txt b { font:700 12.5px/1.2 var(--font-ui); color:var(--text); }
  .dv-sec-txt span { font-size:11.5px; line-height:1.4; color:var(--text-mute); }

  .dv-end { display:flex; align-items:center; justify-content:space-between; gap:10px; flex-wrap:wrap; padding-top:10px; border-top:1px solid var(--border); }
  .dv-privacy { margin:0; font-size:11.5px; color:var(--text-dim); display:flex; gap:6px; align-items:center; }
  .dv-audit { list-style:none; margin:0; padding:0; display:flex; flex-direction:column; gap:6px; font-size:12px; }
  .dv-audit li { display:flex; flex-wrap:wrap; gap:4px 10px; padding:6px 0; border-bottom:1px solid var(--border); color:var(--text-mute); }
  .dv-audit time { font-family:var(--font-mono); color:var(--text-dim); }
  .dv-audit b { color:var(--text); }
  .dim { color:var(--text-dim); }

  @media (max-width: 720px) {
    .dv-item { grid-template-columns:40px minmax(0,1fr); }
    .dv-acts { grid-column:1 / -1; justify-content:flex-start; }
    .dv-sec { flex-wrap:wrap; }
  }
</style>
