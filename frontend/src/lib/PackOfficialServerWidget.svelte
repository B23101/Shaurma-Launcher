<script lang="ts">
  // ── Віджет офіційного сервера збірки ────────────────────────────────────
  // Shaurma-збірки можуть мати "офіційний" сервер (pack.server_ip з
  // packs-index.json — worker/manifest.go, ServerIP), який НЕ живе у
  // servers.dat і не показується в розділі "Сервери" (щоб не плутати з
  // серверами, які додав сам користувач). Натомість він показується тут:
  // окремим компактним віджетом біля назви/кнопки "Грати" на сторінці
  // збірки, з живим статусом (онлайн/офлайн, гравці) і кнопкою "Приєднатись"
  // (= запустити гру і одразу зайти, як у розділі "Сервери"). Якщо такої
  // адреси ще немає у списку серверів гравця — пропонуємо додати її туди
  // одним кліком, для зручності (щоб вона з'явилась і в грі, і в лаунчері).
  import { t } from './i18n.svelte'
  import { toast } from './toast.svelte'
  import { App } from './wails'
  import { parseMotdSegments, stripMotdCodes } from './motd'
  import type { InstanceServer, ServerPing } from './types'

  let { packId, serverIp, disabled = false, activeAccountId = '', onJoin } = $props<{
    packId: string
    // IP офіційного сервера збірки (pack.server_ip); компонент нічого не
    // рендерить, якщо порожній — виклик з батька безумовний.
    serverIp?: string
    // Гра вже запущена на акаунті — «Приєднатись» деактивується (лише 1
    // збірка на 1 акаунт, ще одну сесію не запустити).
    disabled?: boolean
    // activeAccountId лишається пропом для сумісності, але сам захист
    // від гонитви акаунта тепер робить App.svelte у launchWithHealthGuard/
    // doLaunch (SetLaunchClickAccount фіксується там, ПЕРЕД усім ланцюгом
    // health-guard/mod-check/ram-check) — тут його дублювати не треба.
    activeAccountId?: string
    // onJoin — та сама точка входу, що onLaunch на сторінці збірки
    // (App.svelte joinPinnedServer → launchWithHealthGuard): раніше цей
    // компонент викликав App.LaunchInstanceOnServer НАПРЯМУ, в обхід
    // health-guard/mod-check/ram-check і без жодного видимого індикатора
    // прогресу — клік по «Приєднатись» виглядав так, ніби нічого не
    // відбувається, поки на бекенді тихо йшло оновлення файлів збірки.
    onJoin: (packId: string, address: string) => void
  }>()

  let ping = $state<ServerPing | null>(null)
  let pinging = $state(true)
  let inList = $state(false)
  let checkingList = $state(false)
  let joining = $state(false)
  let addingToList = $state(false)

  async function refresh() {
    if (!serverIp) return
    pinging = true
    try {
      ping = await App.PingServer(serverIp)
    } catch {
      ping = { online: false, error: t('pack.game.pingError') }
    }
    pinging = false
  }

  async function checkInList() {
    if (!serverIp) return
    checkingList = true
    try {
      const list: InstanceServer[] = await App.ListPackServers(packId)
      inList = list.some(s => s.address === serverIp)
    } catch {
      inList = false
    }
    checkingList = false
  }

  let lastKey = ''
  $effect(() => {
    const key = `${packId}::${serverIp ?? ''}`
    if (serverIp && key !== lastKey) {
      lastKey = key
      refresh()
      checkInList()
    }
  })

  async function addToList() {
    if (!serverIp || addingToList) return
    addingToList = true
    try {
      await App.AddPackServer(packId, t('pack.servers.official.widgetTitle'), serverIp)
      inList = true
      toast(t('pack.servers.official.addedToList'), 'success')
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    }
    addingToList = false
  }

  async function join() {
    if (!serverIp || joining) return
    joining = true
    // onJoin (App.svelte joinPinnedServer → launchWithHealthGuard) сам
    // ловить і показує помилки запуску через toast (той самий шлях, що й
    // головна кнопка «Грати») — дублювати try/catch з власним тостом тут
    // не потрібно, це раніше давало б два різних повідомлення про той
    // самий збій.
    await onJoin(packId, serverIp)
    joining = false
  }
</script>

{#if serverIp}
  <div class="osw">
    <div class="osw-ic">
      {#if ping?.favicon}
        <img src={ping.favicon} alt="" />
      {:else}
        <i class="ti ti-flame"></i>
      {/if}
    </div>
    <div class="osw-info">
      <div class="osw-name">
        {t('pack.servers.official.widgetTitle')}
        <span class="chip on">{t('pack.servers.official')}</span>
      </div>
      <div class="osw-addr mono">{serverIp}</div>
      {#if ping?.online && ping.motd}
        <div class="osw-motd" title={stripMotdCodes(ping.motd)}>{#each parseMotdSegments(ping.motd) as seg}<span style="color:{seg.color ?? 'inherit'}; font-weight:{seg.bold ? 700 : 'inherit'}; font-style:{seg.italic ? 'italic' : 'normal'}; text-decoration:{[seg.underline && 'underline', seg.strikethrough && 'line-through'].filter(Boolean).join(' ') || 'none'}">{seg.text}</span>{/each}</div>
      {/if}
    </div>

    {#if pinging}
      <div class="osw-stat"><i class="ti ti-loader spin"></i></div>
    {:else if ping?.online}
      <div class="osw-stat" title={stripMotdCodes(ping.motd || '')}><div class="dot on"></div>{ping.players ?? 0}/{ping.maxPlayers ?? 0}</div>
    {:else}
      <div class="osw-stat"><div class="dot off"></div>{t('pack.servers.offline')}</div>
    {/if}

    {#if !checkingList && !inList}
      <button class="btn btn-ghost btn-sm" onclick={addToList} disabled={addingToList} title={t('pack.servers.official.addToList')}>
        <i class="ti {addingToList ? 'ti-loader spin' : 'ti-plus'}"></i> {t('pack.servers.official.addToList')}
      </button>
    {/if}
    <button class="btn btn-primary btn-sm" onclick={join} disabled={disabled || joining}>
      <i class="ti {joining ? 'ti-loader spin' : 'ti-player-play'}"></i> {t('pack.servers.official.join')}
    </button>
  </div>
{/if}

<style>
  .osw {
    display: flex; align-items: center; gap: 12px; padding: 10px 14px; margin-bottom: 12px;
    background: linear-gradient(135deg, rgba(255,138,0,.10), rgba(255,138,0,.03));
    border: 1px solid rgba(255,138,0,.25); border-radius: var(--r-md);
  }
  .osw-ic {
    width: 34px; height: 34px; min-width: 34px; border-radius: 9px; overflow: hidden;
    background: var(--bg-input-hi); display: flex; align-items: center; justify-content: center;
    color: var(--orange); font-size: 16px;
  }
  .osw-ic img { width: 100%; height: 100%; object-fit: cover; image-rendering: pixelated; }
  .osw-info { flex: 1; min-width: 0; }
  .osw-name { font-size: 12.5px; font-weight: 700; display: flex; align-items: center; gap: 8px; }
  .osw-addr { font-size: 10.5px; color: var(--text-mute); margin-top: 1px; }
  .osw-motd {
    margin-top: 3px; font-size: 11px; color: var(--text-mute); line-height: 1.35;
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 360px;
    opacity: .85;
  }
  .osw-stat { display: flex; align-items: center; gap: 6px; font-size: 11px; font-weight: 600; color: var(--text-mute); white-space: nowrap; }
  .dot { width: 8px; height: 8px; border-radius: 50%; }
  .dot.on { background: var(--green); box-shadow: 0 0 8px var(--green); }
  .dot.off { background: var(--red); }
</style>
