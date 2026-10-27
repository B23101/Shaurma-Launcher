<script lang="ts">
  // ── Чесний стан P2P + голосовий чат ─────────────────────────────────────
  // Раніше віджет показував «онлайн», навіть коли гості не могли зайти, і не казав чому.
  // Тут усе береться з бекенду одним викликом (P2PGetVoiceState): чек-лист «що працює /
  // що ні і чому», знайдений на сервері голосовий мод із його РЕАЛЬНИМ портом, і
  // налаштування голосу, що зберігаються й застосовуються на ходу — без перезапуску тунелю.
  import { onMount } from 'svelte'
  import { App } from '../../wails'
  import { t } from '../../i18n.svelte'
  import { toast } from '../../toast.svelte'
  import { p2pErr } from '../p2pErr'
  import type { P2PVoiceState, P2PCheck } from '../../types'

  let {
    serverId = '',
    refreshKey = 0,
    // onChanged — тунель запущено/перезапущено звідси: віджет має перечитати свій стан.
    onChanged = null,
  }: {
    serverId?: string
    refreshKey?: number
    onChanged?: (() => void) | null
  } = $props()

  let st = $state<P2PVoiceState | null>(null)
  let busy = $state(false)
  let customDraft = $state('')
  let customOpen = $state(false)
  // seq — відкидає запізнілі відповіді опитування, щоб вони не затирали щойно застосоване
  // користувачем (саме така гонка раніше «забувала» налаштування UDP).
  let seq = 0

  async function load(force = false) {
    if (!serverId) return
    const my = ++seq
    try {
      const next = await App.P2PGetVoiceState(serverId, force)
      if (my !== seq) return
      st = next
      if (!customOpen) customDraft = next.customPort > 0 ? String(next.customPort) : ''
    } catch { /* бекенд тимчасово недоступний — лишаємо попередній стан */ }
  }

  onMount(() => {
    void load()
    const id = setInterval(() => { if (!document.hidden) void load() }, 4000)
    return () => clearInterval(id)
  })
  $effect(() => { void serverId; void refreshKey; void load() })

  async function setVoice(enabled: boolean, custom: number) {
    if (busy || !serverId) return
    busy = true
    seq++ // скасовуємо відповіді опитування, що вже летять
    try {
      st = await App.P2PSetVoice(serverId, enabled, custom)
      customDraft = st.customPort > 0 ? String(st.customPort) : ''
    } catch (e: any) {
      toast(p2pErr(e), 'error')
      await load(true)
    } finally { busy = false }
  }

  function toggleVoice() { if (st) void setVoice(!st.enabled, st.customPort) }
  function setAuto() { customOpen = false; void setVoice(true, 0) }
  function applyCustom() {
    const n = Number(customDraft)
    if (!Number.isInteger(n) || n < 1 || n > 65535) { toast(t('servers.p2p.health.badPort'), 'error'); return }
    void setVoice(true, n)
  }

  async function restart() {
    if (busy || !serverId) return
    busy = true
    try {
      await App.P2PStartForServer(serverId)
      onChanged?.()
    } catch (e: any) { toast(p2pErr(e), 'error') }
    finally { busy = false; await load(true) }
  }

  function runFix(c: P2PCheck) {
    switch (c.fix) {
      case 'udp_on': void setVoice(true, st?.customPort ?? 0); break
      case 'udp_auto': setAuto(); break
      case 'start_p2p':
      case 'restart_p2p': void restart(); break
      case 'recheck': void load(true); break
    }
  }
  const fixLabel = (f?: string) => t(`servers.p2p.health.fix.${f}`)

  const icon = (lv: string) =>
    lv === 'ok' ? 'ti-circle-check' : lv === 'error' ? 'ti-circle-x' : lv === 'warn' ? 'ti-alert-triangle' : 'ti-info-circle'

  const mods = $derived(st?.voice.solutions ?? [])
  const activeMods = $derived(mods.filter((m) => m.enabled))
  const showChecks = $derived((st?.checks ?? []).filter((c) => !(st?.level === 'ok' && c.level === 'info')))
</script>

{#if st}
  <div class="phc phc-lv-{st.level}">
    <!-- ── Вирок ── -->
    <div class="phc-head">
      <span class="phc-dot"></span>
      <div class="phc-headtxt">
        <div class="phc-title">{st.headline}</div>
        {#if st.host.active}
          <div class="phc-sub">
            {t('servers.p2p.health.uptime', { min: Math.max(1, Math.round(st.host.uptimeSec / 60)) })}
            · {t('servers.p2p.health.players', { n: st.host.guests })}
          </div>
        {/if}
      </div>
      <button class="phc-btn" type="button" disabled={busy} onclick={() => load(true)} title={t('servers.p2p.health.recheck')}>
        <i class="ti ti-refresh"></i> {t('servers.p2p.health.recheck')}
      </button>
    </div>

    <!-- ── Чек-лист ── -->
    {#if showChecks.length}
      <ul class="phc-list">
        {#each showChecks as c (c.id)}
          <li class="phc-item lv-{c.level}">
            <i class="ti {icon(c.level)}"></i>
            <div class="phc-txt">
              <div class="phc-t">{c.title}</div>
              {#if c.detail}<div class="phc-d">{c.detail}</div>{/if}
            </div>
            {#if c.fix}
              <button class="phc-fix" type="button" disabled={busy} onclick={() => runFix(c)}>{fixLabel(c.fix)}</button>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}

    <!-- ── Голосовий чат ── -->
    <div class="phc-voice">
      <div class="phc-vrow">
        <div class="phc-vtxt">
          <div class="phc-vt">{t('servers.p2p.udp.label')}</div>
          <div class="phc-vd">
            {#if activeMods.length}
              {#each activeMods as m, i (m.file)}
                {i ? ', ' : ''}<b>{m.name}</b> ({m.where === 'mod' ? t('servers.p2p.health.mod') : t('servers.p2p.health.plugin')}) —
                {t('servers.p2p.health.udpPort')} {m.port}{m.sameAsServer ? ` (${t('servers.p2p.health.sameAsServer')})` : ''}
              {/each}
            {:else}
              {t('servers.p2p.health.noVoiceMod')}
            {/if}
          </div>
        </div>
        <div
          class="toggle" class:on={st.enabled}
          onclick={toggleVoice} role="switch" aria-checked={st.enabled} tabindex="0"
          onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); toggleVoice() } }}
        ></div>
      </div>

      {#if st.enabled}
        <div class="phc-port">
          <span class="phc-vd">{t('servers.p2p.health.tunnelPort')}:</span>
          <button class="phc-chip" class:on={st.customPort === 0} type="button" disabled={busy} onclick={setAuto}>
            {t('servers.p2p.health.auto')} · {st.voice.autoPort}
          </button>
          <button class="phc-chip" class:on={st.customPort > 0} type="button" disabled={busy} onclick={() => (customOpen = true)}>
            {t('servers.p2p.health.custom')}
          </button>
          {#if customOpen || st.customPort > 0}
            <input class="input mono phc-input" type="number" min="1" max="65535" bind:value={customDraft}
              onfocus={() => (customOpen = true)} onchange={applyCustom} aria-label={t('servers.p2p.udp.portLabel')} />
          {/if}
        </div>
        <div class="phc-vd">{t('servers.p2p.health.appliedLive')}</div>
        <div class="phc-exposure"><i class="ti ti-alert-triangle"></i><span>{t('servers.p2p.health.exposure')}</span></div>
      {/if}
    </div>
  </div>
{/if}

<style>
  .phc { display: flex; flex-direction: column; gap: 12px; padding: 16px 18px; border-radius: var(--r-lg, 16px); background: var(--bg-card); border: 1px solid var(--border); --lv: var(--text-mute); }
  .phc-lv-ok { --lv: #22c55e; }
  .phc-lv-warn { --lv: #f59e0b; }
  .phc-lv-error { --lv: #ef4444; }
  .phc-lv-off { --lv: var(--text-dim, #888); }
  .phc-lv-ok, .phc-lv-warn, .phc-lv-error { border-color: color-mix(in srgb, var(--lv) 35%, var(--border)); }
  .phc-head { display: flex; align-items: center; gap: 11px; }
  .phc-dot { width: 11px; height: 11px; border-radius: 50%; background: var(--lv); box-shadow: 0 0 0 4px color-mix(in srgb, var(--lv) 22%, transparent); flex-shrink: 0; }
  .phc-headtxt { flex: 1; min-width: 0; }
  .phc-title { font: 700 14px/1.3 var(--font-ui); color: var(--text); }
  .phc-sub { font: 400 11px/1.4 var(--font-ui); color: var(--text-mute); margin-top: 2px; }
  .phc-btn { display: inline-flex; align-items: center; gap: 6px; padding: 7px 11px; border-radius: 10px; cursor: pointer; background: var(--bg-input); border: 1px solid var(--border); color: var(--text-mute); font: 600 11.5px/1 var(--font-ui); }
  .phc-btn:hover { background: var(--hover-strong); color: var(--text); }
  .phc-btn:disabled { opacity: .55; cursor: default; }

  .phc-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 6px; }
  .phc-item { display: flex; align-items: flex-start; gap: 9px; padding: 9px 11px; border-radius: 11px; background: var(--bg-input); }
  .phc-item > i { font-size: 16px; margin-top: 1px; flex-shrink: 0; }
  .lv-ok > i { color: #22c55e; } .lv-info > i { color: var(--text-mute); }
  .lv-warn { background: rgba(245, 158, 11, .07); } .lv-warn > i { color: #f59e0b; }
  .lv-error { background: rgba(239, 68, 68, .08); } .lv-error > i { color: #ef4444; }
  .phc-txt { flex: 1; min-width: 0; }
  .phc-t { font: 600 12.5px/1.4 var(--font-ui); color: var(--text); }
  .phc-d { font: 400 11.5px/1.5 var(--font-ui); color: var(--text-mute); margin-top: 2px; word-break: break-word; }
  .phc-fix { flex-shrink: 0; padding: 6px 11px; border-radius: 9px; cursor: pointer; border: none; background: var(--pcol, var(--orange)); color: #111; font: 700 11.5px/1 var(--font-ui); }
  .phc-fix:hover { filter: brightness(1.08); }
  .phc-fix:disabled { opacity: .55; cursor: default; }

  .phc-voice { display: flex; flex-direction: column; gap: 8px; padding-top: 12px; border-top: 1px solid var(--border); }
  .phc-vrow { display: flex; align-items: center; gap: 12px; }
  .phc-vtxt { flex: 1; min-width: 0; }
  .phc-vt { font: 700 13.5px/1.25 var(--font-ui); color: var(--text); }
  .phc-vd { font: 400 11px/1.5 var(--font-ui); color: var(--text-mute); }
  .phc-vd b { color: var(--text); font-weight: 600; }
  .phc-port { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
  .phc-chip { padding: 6px 11px; border-radius: 999px; cursor: pointer; background: var(--bg-input); border: 1px solid var(--border); color: var(--text-mute); font: 600 11.5px/1 var(--font-ui); }
  .phc-chip.on { background: color-mix(in srgb, var(--pcol, var(--orange)) 18%, transparent); border-color: var(--pcol, var(--orange)); color: var(--text); }
  .phc-input { width: 88px; min-width: 0; font-size: 12px; padding: 6px 9px; text-align: center; }
  .phc-exposure { display: flex; gap: 8px; align-items: flex-start; padding: 9px 11px; border-radius: 10px; font: 400 11px/1.5 var(--font-ui); color: color-mix(in srgb, #fb923c 80%, var(--text)); background: rgba(251, 146, 60, .06); border: 1px solid rgba(251, 146, 60, .18); }
  .phc-exposure i { color: #fb923c; font-size: 14px; flex-shrink: 0; margin-top: 1px; }
</style>
