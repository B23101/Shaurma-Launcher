<script lang="ts">
  // ── Вкладка "Мережа" ──────────────────────────────────────────────────
  // Тут лише МЕРЕЖА — тобто доступ до сервера:
  //   • локальна адреса (ServerIpBar);
  //   • публічний доступ через власну P2P-систему (ServerP2PWidget) і
  //     P2P-кімната (ServerP2PAccess: режим доступу, списки, співхости);
  //   (мережа серверів самого проксі — окрема вкладка ServerProxyTab).
  //
  // НАВАНТАЖЕННЯ (CPU/RAM/гравці/uptime) тут БІЛЬШЕ НЕ ПОКАЗУЄМО: воно
  // переїхало в банер сервера (ServerBanner → ServerMetricsPanel). «Продуктивність
  // сервера» — це не мережа, і ховати її у мережеву вкладку було безглуздо:
  // щоб побачити, що сервер задихається, доводилось відкривати інший розділ.
  //
  // Вкладка доступна ДЛЯ ВСІХ ядер (не лише Velocity).
  //
  // remoteServer — сервер друга: лише його публічна P2P-адреса, яку друг
  // вмикає з правом запуску й вимикає з правом зупинки (решта — у власника).
  import type { ServerView, P2PStatus, RemoteServer } from '../../types'
  import { remote, remoteKey, remoteP2P } from '../remoteServers.svelte'
  import { toast } from '../../toast.svelte'
  import { App } from '../../wails'
  import ServerIpBar from '../components/ServerIpBar.svelte'
  import ServerP2PWidget from '../components/ServerP2PWidget.svelte'
  import ServerP2PAccess from '../components/ServerP2PAccess.svelte'
  import { t } from '../../i18n.svelte'

  let {
    srv,
    isRunning = false,
    refreshKey = 0,
    velocityProxyRunning = false,
    onAutoStartChange = null,
    remoteServer = null,
  }: {
    srv: ServerView
    isRunning?: boolean
    refreshKey?: number
    velocityProxyRunning?: boolean
    onAutoStartChange?: ((v: boolean) => void) | null
    remoteServer?: RemoteServer | null
  } = $props()

  // ── Сервер друга: P2P-адреса ──
  const rs = $derived(remoteServer)
  const p2pBusy = $derived(!!rs && !!remote.p2pPending[remoteKey(rs)])
  // Увімкнути — з правом запуску на запущеному сервері, вимкнути — з правом зупинки.
  const canToggleP2P = $derived(!!rs && rs.ownerOnline && (rs.p2p ? rs.canStop : rs.canStart && rs.state === 'running'))
  async function copyAddr() {
    if (!rs?.p2pAddr) return
    try { await App.CopyToClipboard(rs.p2pAddr); toast(t('servers.remote.addrCopied'), 'success') } catch { /* ignore */ }
  }

  const isVelocity = $derived(srv.core === 'velocity')
  const port = $derived(srv.port ?? 25565)

  // ── Чи можна взагалі показувати P2P ──────────────────────────────────
  // P2P працює лише коли на worker'і налаштований Headscale. Якщо ні —
  // показувати кнопки «Застовбити адресу» було б обманом: кожне натискання
  // повертало б помилку. Тому беремо стан у бекенда й показуємо пояснення.
  // Помилка запиту статусу ≠ вимкнено: тоді лишаємо віджет (тимчасовий
  // збій мережі не має ховати робочу фічу).
  let p2pStatus = $state<P2PStatus | null>(null)
  let p2pBlocked = $state(false)
  // Лічильник «адресу створено»: ServerP2PAccess створює адресу при першій зміні доступу,
  // і віджет адреси має перечитати книгу (перемонтовується через {#key}).
  let addrTick = $state(0)
  $effect(() => {
    const rk = refreshKey
    void rk
    if (remoteServer) return
    App.GetP2PStatus()
      .then((st) => { p2pStatus = st; p2pBlocked = !st.configured })
      .catch(() => { p2pStatus = null; p2pBlocked = false })
  })
</script>

{#if rs}
<div class="snt">
  <div class="snt-block">
    <div class="snt-title"><i class="ti ti-world"></i> {t('servers.remote.p2pTitle')}</div>
    <div class="st-card">
      <div class="st-row">
        <div class="st-row-text">
          <div class="st-row-name">{t('servers.remote.p2pTitle')}</div>
          <div class="st-row-hint">{rs.p2p ? t('servers.remote.p2pOnHint') : t('servers.remote.p2pOffHint')}</div>
        </div>
        <div class="st-row-ctl wrap">
          {#if rs.p2p && rs.p2pAddr}
            <span class="st-tag ok mono">{rs.p2pAddr}</span>
            <button type="button" class="st-iconbtn" onclick={copyAddr} title={t('servers.remote.copyAddr')} aria-label={t('servers.remote.copyAddr')}><i class="ti ti-copy"></i></button>
          {/if}
          <button type="button" class="st-switch" class:on={rs.p2p} role="switch" aria-checked={!!rs.p2p}
                  aria-label={t('servers.remote.p2pTitle')} disabled={!canToggleP2P || p2pBusy}
                  title={!rs.p2p && rs.state !== 'running' ? t('servers.remote.p2pNeedRunning') : ''}
                  onclick={() => remoteP2P(rs, !rs.p2p)}></button>
        </div>
      </div>
      {#if !rs.p2p && !rs.canStart}
        <div class="st-pad"><div class="st-note"><i class="ti ti-lock"></i><span>{t('servers.remote.p2pNoStart')}</span></div></div>
      {:else if rs.p2p && !rs.canStop}
        <div class="st-pad"><div class="st-note"><i class="ti ti-lock"></i><span>{t('servers.remote.p2pNoStop')}</span></div></div>
      {/if}
    </div>
  </div>
</div>
{:else}
<div class="snt">
  <div class="snt-block">
    <div class="snt-title"><i class="ti ti-world"></i> {t('servers.net.access')}</div>
    <ServerIpBar {port} {isRunning} {isVelocity} {velocityProxyRunning} />
    {#if !isRunning}
      <div class="snt-hint"><i class="ti ti-info-circle"></i> {t('servers.net.addrHint')}</div>
    {/if}
  </div>

  {#key refreshKey}
    {#if p2pBlocked}
      <!-- Фіча вимкнена в налаштуваннях або не налаштована на сервері:
           показуємо причину замість мертвих кнопок. УВІМКНУТИ можна у
           Налаштування → Основне. -->
      <div class="snt-block">
        <div class="snt-title"><i class="ti ti-network-off"></i> {t('servers.net.p2pOff')}</div>
        <div class="snt-hint">
          <i class="ti ti-info-circle"></i>
          {p2pStatus?.reason || t('servers.net.p2pUnavailable')}
        </div>
      </div>
    {:else}
      <!-- Одна колонка вниз: спочатку адреса й тунель, нижче — хто може заходити. -->
      <div class="snt-p2p">
        {#key addrTick}
          <ServerP2PWidget
            {port}
            {isRunning}
            serverId={srv.id}
            accountOk={p2pStatus?.accountOk ?? false}
            accountReason={p2pStatus?.accountReason ?? ''}
            roomDomain={p2pStatus?.roomDomain ?? 'play.shaurma-launcher.org'}
            autoStart={srv.autoStartP2P ?? false}
            {onAutoStartChange}
          />
        {/key}
        {#if p2pStatus?.accountOk}
          <ServerP2PAccess {port} serverId={srv.id} {refreshKey} onclaimed={() => addrTick++} />
        {/if}
      </div>
    {/if}
  {/key}
</div>
{/if}

<style>
  .snt { display: flex; flex-direction: column; gap: 22px; padding: 2px 2px 24px; }
  .snt-p2p { display: flex; flex-direction: column; gap: 22px; min-width: 0; }
  .snt-block { display: flex; flex-direction: column; gap: 8px; }
  .snt-title {
    display: flex; align-items: center; gap: 7px;
    font: 700 12px/1 var(--font-ui); color: var(--text);
  }
  .snt-title i { color: var(--pcol, var(--orange)); font-size: 14px; }
  .snt-hint { font: 400 var(--fs-min)/1.4 var(--font-ui); color: var(--text-dim); display: flex; align-items: center; gap: 5px; }
</style>
