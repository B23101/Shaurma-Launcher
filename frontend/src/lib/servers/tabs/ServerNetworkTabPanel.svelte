<script lang="ts">
  // ── Вкладка "Мережа" ──────────────────────────────────────────────────
  // Тут лише МЕРЕЖА — тобто доступ до сервера:
  //   • локальна адреса (ServerIpBar);
  //   • публічний доступ через власну P2P-систему (ServerP2PWidget) і
  //     P2P-кімната (ServerP2PAccess: режим доступу, списки, співхости);
  //   • для Velocity-проксі — керування проксі та backend-серверами
  //     (ServerNetworkPanel).
  //
  // НАВАНТАЖЕННЯ (CPU/RAM/гравці/uptime) тут БІЛЬШЕ НЕ ПОКАЗУЄМО: воно
  // переїхало в банер сервера (ServerBanner → ServerMetricsPanel). «Продуктивність
  // сервера» — це не мережа, і ховати її у мережеву вкладку було безглуздо:
  // щоб побачити, що сервер задихається, доводилось відкривати інший розділ.
  //
  // Вкладка доступна ДЛЯ ВСІХ ядер (не лише Velocity).
  import type { ServerView, P2PStatus } from '../../types'
  import { App } from '../../wails'
  import ServerIpBar from '../components/ServerIpBar.svelte'
  import ServerP2PWidget from '../components/ServerP2PWidget.svelte'
  import ServerP2PAccess from '../components/ServerP2PAccess.svelte'
  import ServerNetworkPanel from '../../ServerNetworkPanel.svelte'
  import { t } from '../../i18n.svelte'

  let {
    srv,
    isRunning = false,
    refreshKey = 0,
    velocityProxyRunning = false,
    onAutoStartChange = null,
  }: {
    srv: ServerView
    isRunning?: boolean
    refreshKey?: number
    velocityProxyRunning?: boolean
    onAutoStartChange?: ((v: boolean) => void) | null
  } = $props()

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
    App.GetP2PStatus()
      .then((st) => { p2pStatus = st; p2pBlocked = !st.configured })
      .catch(() => { p2pStatus = null; p2pBlocked = false })
  })
</script>

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

  {#if isVelocity}
    <div class="snt-block">
      <div class="snt-title"><i class="ti ti-network"></i> {t('servers.net.proxyNet')}</div>
      <ServerNetworkPanel proxyServerId={srv.id} />
    </div>
  {/if}
</div>

<style>
  .snt { display: flex; flex-direction: column; gap: 22px; padding: 2px 2px 24px; }
  .snt-p2p { display: flex; flex-direction: column; gap: 22px; min-width: 0; }
  .snt-block { display: flex; flex-direction: column; gap: 8px; }
  .snt-title {
    display: flex; align-items: center; gap: 7px;
    font: 700 12px/1 var(--font-ui); color: var(--text);
  }
  .snt-title i { color: var(--pcol, var(--orange)); font-size: 14px; }
  .snt-hint { font: 400 10.5px/1.4 var(--font-ui); color: var(--text-dim); display: flex; align-items: center; gap: 5px; }
</style>
