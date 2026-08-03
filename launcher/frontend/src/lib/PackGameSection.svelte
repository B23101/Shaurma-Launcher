<script lang="ts">
  // ── Розділ «Гра» на сторінці збірки ────────────────────────────────────
  // Мінімальна RAM (-Xms), прив'язка збірки до конкретного акаунта та
  // автоприєднання (світ або сервер). Все зберігається per-instance.
  import { t } from './i18n.svelte'
  import { App } from './wails'
  import SelectField, { type SelectOption } from './SelectField.svelte'
  import steveHead from '../assets/images/steve-head.png'
  import type { Account, InstanceConfig, InstanceServer, ServerPing, Settings } from './types'

  let {
    cfg,
    settings,
    accounts,
    worlds,
    packServers = [],
    onRefreshWorlds = () => {},
    mcVersion = '',
    onUpdate
  } = $props<{
    cfg: InstanceConfig
    settings: Settings
    accounts: Account[]
    worlds: string[]
    // Сервери, збережені у server.dat збірки (для вибору цілі автоприєднання
    // по IP замість ручного введення).
    packServers?: InstanceServer[]
    // Оновлення списку світів/серверів (батьківська сторінка перечитує їх
    // з диска — гра могла створити новий світ).
    onRefreshWorlds?: () => void
    mcVersion?: string
    onUpdate: (key: keyof InstanceConfig, value: InstanceConfig[keyof InstanceConfig]) => void
  }>()

  // ── Підтримка автоприєднання до світу (quick-play) ─────────────────────
  // Офіційний прапорець --quickPlaySingleplayer з'явився у 1.20
  // (снапшот 23w14a). Для старіших версій гра його просто ігнорує, тому
  // опцію «Світ» в автоприєднанні для них не показуємо взагалі — лишаємо
  // лише «Сервер». Для версій 1.20.x сам вхід у світ працює, але довше.
  function parseMCVersion(v: string): { major: number; minor: number } | null {
    const m = /^(\d+)\.(\d+)/.exec(v.trim())
    return m ? { major: Number(m[1]), minor: Number(m[2]) } : null
  }
  const supportsWorldJoin = $derived.by((): boolean => {
    const v = parseMCVersion(mcVersion)
    if (!v) return true // невідома версія (снапшоти тощо) — вважаємо сучасною
    return v.major > 1 || (v.major === 1 && v.minor >= 20)
  })
  // 1.20.x — вхід у світ працює, але довше; 1.20.0/1.20.1 quick-play ще
  // глючний, тож для всіх 1.20 показуємо застереження.
  const isSlowWorldJoin = $derived.by((): boolean => {
    const v = parseMCVersion(mcVersion)
    return !!v && v.major === 1 && v.minor === 20
  })

  // ── Аватари акаунтів (для селекта «прив'язати збірку до акаунта») ─────
  // Ті самі data URL голів зі скінів Mojang, що й у сайдбарі; кешуються на
  // бекенді (App.GetAccountHead). Якщо не завантажились — запасна голова.
  let heads = $state<Record<string, string>>({})
  $effect(() => {
    const accs = accounts
    if (!accs?.length) return
    let cancelled = false
    ;(async () => {
      const next: Record<string, string> = {}
      await Promise.all(accs.map(async (acc) => {
        try {
          const url = await App.GetAccountHead(acc.uuid, acc.username, !!acc.isLicensed)
          const key = acc.uuid || acc.username
          if (key && url) next[key] = url
        } catch (e) { /* запасна голова Стіва */ }
      }))
      if (!cancelled) heads = next
    })()
    return () => { cancelled = true }
  })
  function headFor(uuid?: string): string {
    return (uuid && heads[uuid]) || steveHead
  }

  // Опції селекта акаунтів + поточний обраний акаунт.
  const accountOptions = $derived(
    accounts.map(acc => ({
      value: acc.id, label: acc.username, head: headFor(acc.uuid)
    }))
  )
  const currentAccount = $derived(accounts.find(a => a.id === cfg.accountIdOverride))

  // Опції типу автоприєднання: «Світ» показуємо лише якщо версія його
  // підтримує. Якщо поточний тип «world», а версія не підтримує — під час
  // відображення користувач бачить лише «Сервер»; саме значення в cfg
  // залишаємо, але реально воно не застосується (прапорець ігнорується).
  const autoJoinTypeOptions = $derived.by((): SelectOption[] => {
    const opts: SelectOption[] = [{ value: 'server', label: t('pack.game.autoJoinTypeServer'), icon: 'ti-server-2' }]
    if (supportsWorldJoin) {
      opts.unshift({ value: 'world', label: t('pack.game.autoJoinTypeWorld'), icon: 'ti-world' })
    }
    return opts
  })
  const worldOptions = $derived(
    worlds.map(w => ({ value: w, label: w, icon: 'ti-map' }))
  )

  // ── Вибір сервера з server.dat збірки ──
  // Опції: «ввести вручну» + сервери, збережені грою (по IP). Поточне
  // значення, введене вручну, теж потрапляє у список, інакше селект
  // показував би порожній рядок.
  const serverOptions = $derived.by((): SelectOption[] => {
    const opts: SelectOption[] = [{ value: '', label: t('pack.game.autoJoinServerManual'), icon: 'ti-pencil' }]
    const seen = new Set<string>()
    for (const s of packServers) {
      if (!s.address || seen.has(s.address)) continue
      seen.add(s.address)
      opts.push({ value: s.address, label: s.name ? `${s.name} — ${s.address}` : s.address, icon: 'ti-server-2' })
    }
    if (cfg.autoJoinServer && !seen.has(cfg.autoJoinServer)) {
      opts.push({ value: cfg.autoJoinServer, label: cfg.autoJoinServer, icon: 'ti-pencil' })
    }
    return opts
  })

  // ── Пінг сервера (чи в мережі) ──
  // Автоматично після зміни адреси (з дебаунсом) і по кнопці оновлення.
  let pings = $state<Record<string, ServerPing | null>>({})
  let pingTimer: ReturnType<typeof setTimeout> | undefined

  async function pingServer(address: string) {
    if (!address) return
    pings = { ...pings, [address]: null }
    try {
      pings = { ...pings, [address]: await App.PingServer(address) }
    } catch {
      pings = { ...pings, [address]: { online: false, error: t('pack.game.pingError') } }
    }
  }

  function pingCurrent() {
    if (cfg.autoJoinServer) pingServer(cfg.autoJoinServer)
  }

  $effect(() => {
    const addr = cfg.autoJoinServer
    if (!addr || pings[addr] !== undefined) return
    clearTimeout(pingTimer)
    pingTimer = setTimeout(() => pingServer(addr), 350)
  })

  // ── Дефолт-сервер ──
  // Автоприєднання до сервера увімкнене, а адреса порожня → підставляємо
  // ПЕРШИЙ сервер із server.dat збірки (не лишаємо поле порожнім).
  $effect(() => {
    if (cfg.autoJoinEnabled && cfg.autoJoinType !== 'world' && !cfg.autoJoinServer && packServers.length > 0) {
      const first = packServers.find(s => s.address)
      if (first) onUpdate('autoJoinServer', first.address)
    }
  })

  // ── Авто-скрол до полів автоприєднання, коли вмикається ──
  // Виправляє зависання: розділ «Гра» не показує поля автоприєднання,
  // якщо їх вміст не поміщається у в'юпорт — примусово скролимо до них.
  let autoJoinRef = $state<HTMLDivElement | undefined>()
  $effect(() => {
    if (cfg.autoJoinEnabled && autoJoinRef) {
      requestAnimationFrame(() => {
        autoJoinRef?.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
      })
    }
  })

</script>

<div class="field-hint" style="padding:0 4px;margin-bottom:12px"><i class="ti ti-info-circle"></i> {t('pack.game.note')}</div>


<div class="card">
  <div class="card-head">
    <div class="ci"><i class="ti ti-user"></i></div>
    <div class="ctw"><div class="ct">{t('pack.game.account')}</div></div>
  </div>
  <div class="row">
    <div class="row-label"><div class="rl-name">{t('pack.game.accountOverride')}</div><div class="rl-sub">{t('pack.game.accountOverrideHint')}</div></div>
    <div class="toggle" class:on={!!cfg.accountIdOverride} onclick={() => onUpdate('accountIdOverride', cfg.accountIdOverride ? '' : (accounts[0]?.id ?? ''))}></div>
  </div>
  {#if cfg.accountIdOverride}
    <div class="row">
      <div class="row-label"><div class="rl-name">{t('pack.game.account')}</div></div>
      <SelectField
        value={cfg.accountIdOverride}
        options={accountOptions}
        placeholder={t('pack.game.autoJoinWorldPlaceholder')}
        minWidth="220px"
        ariaLabel={t('pack.game.account')}
        onchange={(v) => onUpdate('accountIdOverride', v)}
      />
    </div>
    {#if currentAccount}
      <div class="row" style="border-bottom:none">
        <div class="row-label"><div class="rl-name">{t('pack.game.accountSelected')}</div></div>
        <div class="acc-sel">
          <img class="acc-sel-head" src={headFor(currentAccount.uuid)} alt="" />
          <span>{currentAccount.username}</span>
        </div>
      </div>
    {/if}
  {/if}
</div>

<div class="card">
  <div class="card-head">
    <div class="ci violet"><i class="ti ti-plug-connected"></i></div>
    <div class="ctw"><div class="ct">{t('pack.game.autoJoin')}</div><div class="cd">{t('pack.game.autoJoinHint')}</div></div>
  </div>
  <div class="row">
    <div class="row-label"><div class="rl-name">{t('pack.game.autoJoin')}</div></div>
    <div class="toggle" class:on={cfg.autoJoinEnabled} onclick={() => onUpdate('autoJoinEnabled', !cfg.autoJoinEnabled)}></div>
  </div>
  {#if cfg.autoJoinEnabled}
    <div bind:this={autoJoinRef}>
    <div class="row">
      <div class="row-label"><div class="rl-name">{t('pack.game.autoJoinType')}</div></div>
      <SelectField
        value={cfg.autoJoinType}
        options={autoJoinTypeOptions}
        minWidth="220px"
        ariaLabel={t('pack.game.autoJoinType')}
        onchange={(v) => onUpdate('autoJoinType', v)}
      />
    </div>
    {#if !supportsWorldJoin && cfg.autoJoinType === 'world'}
      <div class="field-hint" style="padding:0 22px;color:var(--yellow)"><i class="ti ti-alert-triangle"></i> {t('pack.game.autoJoinWorldUnsupported')}</div>
    {/if}
    {#if isSlowWorldJoin && cfg.autoJoinType === 'world'}
      <div class="field-hint" style="padding:0 22px;color:var(--yellow)"><i class="ti ti-clock-hour-4"></i> {t('pack.game.autoJoinWorldSlow')}</div>
    {/if}
    {#if cfg.autoJoinType === 'world'}
      <div class="row">
        <div class="row-label"><div class="rl-name">{t('pack.game.autoJoinWorld')}</div></div>
        <div class="srv-join">
          {#if worlds.length === 0}
            <div class="rl-sub">{t('pack.game.autoJoinWorldEmpty')}</div>
          {:else}
            <SelectField
              value={cfg.autoJoinWorld}
              options={[{ value: '', label: t('pack.game.autoJoinWorldPlaceholder'), icon: 'ti-map' }, ...worldOptions]}
              minWidth="220px"
              ariaLabel={t('pack.game.autoJoinWorld')}
              onchange={(v) => onUpdate('autoJoinWorld', v)}
            />
          {/if}
          <button class="gbtn-sm" onclick={onRefreshWorlds} title={t('pack.game.refreshWorlds')}><i class="ti ti-refresh"></i></button>
        </div>
      </div>
    {:else}
      <div class="row">
        <div class="row-label"><div class="rl-name">{t('pack.game.autoJoinServer')}</div></div>
        <div class="srv-join">
          <SelectField
            value={cfg.autoJoinServer}
            options={serverOptions}
            minWidth="240px"
            ariaLabel={t('pack.game.autoJoinServer')}
            onchange={(v) => onUpdate('autoJoinServer', v)}
          />
          {#if cfg.autoJoinServer}
            {@const pr = pings[cfg.autoJoinServer]}
            {#if pr}
              <span class="ping" class:online={pr.online} title={pr.motd || pr.error || ''}>
                <i class="ti {pr.online ? 'ti-wifi' : 'ti-wifi-off'}"></i>
                {#if pr.online}{pr.players ?? 0}/{pr.maxPlayers ?? 0}{:else}{t('pack.game.serverOffline')}{/if}
              </span>
            {:else if pings[cfg.autoJoinServer] === null}
              <span class="ping ping-wait"><i class="ti ti-loader spin"></i></span>
            {/if}
            <button class="gbtn-sm" onclick={pingCurrent} title={t('pack.game.pingServer')}><i class="ti ti-refresh"></i></button>
          {/if}
        </div>
      </div>
    {/if}
    </div>
  {/if}
</div>

<style>
  .acc-sel { display: flex; align-items: center; gap: 10px; font-size: 12.5px; font-weight: 600; color: var(--text); }
  .acc-sel-head { width: 26px; height: 26px; border-radius: 8px; object-fit: cover; box-shadow: 0 0 0 1px var(--border-hi); }

  .srv-join { display: flex; align-items: center; gap: 8px; }
  .gbtn-sm {
    display: inline-flex; align-items: center; justify-content: center;
    width: 30px; height: 30px; border-radius: 8px; flex-shrink: 0;
    background: var(--bg-panel); border: 1px solid var(--border); color: var(--text-mute);
    cursor: pointer; transition: .15s;
  }
  .gbtn-sm:hover { color: var(--text); border-color: var(--border-hi); }
  .ping {
    display: inline-flex; align-items: center; gap: 5px;
    font-size: 11.5px; font-weight: 600; color: var(--text-mute);
    padding: 5px 9px; border-radius: 20px; background: var(--bg-panel);
    border: 1px solid var(--border); white-space: nowrap;
  }
  .ping.online { color: var(--green); border-color: rgba(16,185,129,.35); background: rgba(16,185,129,.08); }
  .ping.ping-wait { color: var(--text-dim); }
</style>
