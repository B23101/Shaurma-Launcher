<script lang="ts">
  // ── Окреме вікно консолі сервера (standalone) ────────────────────────
  // Монтується з console.ts, коли URL містить ?server=<id> (backend:
  // server_console_window.go → ShowServerConsoleWindowFor). Показує ТУ САМУ
  // вкладку консолі, що й сторінка сервера (ServerConsoleTab у
  // standalone-режимі: там кнопки Старт/Стоп/Kill, бо банера тут немає), а
  // зверху — власна «шапка» вікна з назвою сервера та кнопкою закриття.
  //
  // Закриття вікна (X) не вбиває процес і не втрачає лог: shell шле
  // "window:closing:<id>", а backend (onShellWindowClosing) відповідає
  // "hide" — рівно як у консолей збірок.
  import { App, on, Window } from '../wails'
  import { t } from '../i18n.svelte'
  import { syncAppearance } from '../theme.svelte'
  import { setLanguage } from '../i18n.svelte'
  import type { ServerView } from '../types'
  import { serverVisual } from './serverTargets'
  import ServerConsoleTab from './tabs/ServerConsoleTab.svelte'
  import ServerStatusBadge from './components/ServerStatusBadge.svelte'

  let { serverID = '' }: { serverID?: string } = $props()

  let srv = $state<ServerView | null>(null)
  let status = $state('stopped')

  // ЄДИНИЙ візуал сервера (той самий порядок, що в сторінці сервера та в
  // списках): PNG лаунчера → пресет → протокольна іконка → іконка ядра.
  const vis = $derived(serverVisual({
    core: srv?.core || '',
    color: srv?.color,
    launcherIconData: srv?.launcherIconData,
    icon: srv?.icon,
    iconData: srv?.iconData,
  }))
  const pcol = $derived(vis.color)

  $effect(() => {
    if (!serverID) return
    App.GetServer(serverID).then((s: any) => { srv = s }).catch(() => {})
    App.GetServerStatus(serverID).then((s: any) => { status = s?.status || 'stopped' }).catch(() => {})
    const off = on('server:status', (data: any) => {
      if (data?.serverId !== serverID) return
      status = data.status || 'stopped'
    })
    return () => { off?.() }
  })

  // Вигляд вікна = вигляд лаунчера. Це окремий документ (свій <html>), тож
  // тема/акцент/шрифт з головного вікна сюди не переходять — без цього
  // консоль сервера завжди була дефолтною темною, а власний стиль
  // користувача «скидався». syncAppearance ще й слухає зміни наживо.
  $effect(() => {
    let off: (() => void) | undefined
    let dead = false
    syncAppearance().then(({ settings, off: o }) => {
      if (dead) { o(); return }
      off = o
      if (settings?.language) setLanguage(settings.language)
    })
    return () => { dead = true; off?.() }
  })

  // ── Контроли вікна (вікно безрамкове — власної рамки ОС немає) ──
  // Ті самі виклики, що в окремому вікні консолі збірки (GameConsole).
  let maximized = $state(false)
  function winMin() { Window.Minimise() }
  function winMax() {
    if (maximized) { Window.UnMaximise(); maximized = false }
    else { Window.Maximise(); maximized = true }
  }

  function hideWindow() {
    App.HideServerConsoleWindowFor(serverID).catch(() => {})
  }
</script>

<div class="scw" style="--pcol:{pcol}">
  <div class="scw-head drag">
    <span class="scw-ic">
      {#if vis.src}
        <img src={vis.src} alt="" />
      {:else}
        <i class="ti {vis.ti || 'ti-server-2'}" style="color:var(--pcol)"></i>
      {/if}
    </span>
    <div class="scw-meta">
      <div class="scw-name">{srv?.name ?? 'Консоль сервера'}</div>
      <div class="scw-sub">
        {#if srv?.mcVersion}<span><i class="ti ti-cube"></i> {srv.mcVersion}</span>{/if}
        {#if srv?.port}<span><i class="ti ti-world"></i> :{srv.port}</span>{/if}
        <span><i class="ti ti-folder"></i> {srv?.dir ?? ''}</span>
      </div>
    </div>
    <ServerStatusBadge {status} size="sm" />
    <div class="scw-win">
      <button class="scw-b" onclick={winMin} title={t('win.minimize')} aria-label={t('win.minimize')}><i class="ti ti-minus"></i></button>
      <button class="scw-b" onclick={winMax} title={t('win.maximize')} aria-label={t('win.maximize')}><i class="ti {maximized ? 'ti-copy' : 'ti-square'}"></i></button>
      <button class="scw-b scw-x" onclick={hideWindow} title="Закрити вікно (лог продовжить писатись)" aria-label={t('win.close')}><i class="ti ti-x"></i></button>
    </div>
  </div>

  <div class="scw-body">
    {#if serverID}
      <ServerConsoleTab serverId={serverID} serverColor={pcol} standalone />
    {:else}
      <div class="scw-empty"><i class="ti ti-alert-triangle"></i> Сервер не вказано</div>
    {/if}
  </div>
</div>

<style>
  /* flex:1 + width:100% обов'язкові: #app у console.html — сам flex-контейнер
     (display:flex, рядок за замовчуванням), і без flex:1 .scw як flex-дитина
     стискається до ширини вмісту замість розтягування на все вікно — вікно
     консолі сервера виглядало заповненим лише наполовину. */
  .scw { display: flex; flex-direction: column; flex: 1; width: 100%; height: 100vh; overflow: hidden; background: var(--bg-app); }
  .scw-head {
    display: flex; align-items: center; gap: 10px; flex-shrink: 0;
    padding: 9px 12px; border-bottom: 1px solid var(--border);
    background: color-mix(in srgb, var(--bg-window) 82%, transparent);
    backdrop-filter: blur(10px);
    --wails-draggable: drag;
  }
  .scw-head button { --wails-draggable: no-drag; }
  .scw-head { position: relative; z-index: 10; }
  .scw-head::after {
    content: ''; position: absolute; left: 0; right: 0; bottom: -1px; height: 1px; pointer-events: none;
    background: linear-gradient(90deg, transparent, color-mix(in srgb, var(--pcol) 60%, transparent), transparent);
  }
  .scw-ic {
    width: 28px; height: 28px; min-width: 28px; border-radius: 7px; flex-shrink: 0;
    background: color-mix(in srgb, var(--pcol) 16%, var(--bg-input-hi));
    display: flex; align-items: center; justify-content: center; overflow: hidden; font-size: 14px;
  }
  .scw-ic img { width: 100%; height: 100%; object-fit: contain; }
  .scw-meta { flex: 1; min-width: 0; }
  .scw-name { font: 700 12.5px/1.2 var(--font-ui); color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .scw-sub { display: flex; gap: 8px; margin-top: 2px; flex-wrap: wrap; }
  .scw-sub span { display: inline-flex; align-items: center; gap: 3px; font: 400 9.5px/1 var(--font-mono); color: var(--text-mute); max-width: 260px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .scw-sub i { font-size: 11px; color: var(--pcol); }
  .scw-win { display: flex; gap: 6px; flex-shrink: 0; }
  .scw-b {
    width: 30px; height: 30px; flex-shrink: 0; border-radius: var(--r-sm, 7px); cursor: pointer;
    background: var(--bg-panel); border: 1px solid var(--border); color: var(--text-mute);
    display: flex; align-items: center; justify-content: center; font-size: 14px;
    transition: .15s;
  }
  .scw-b:hover { color: #fff; border-color: var(--border-hi); background: var(--bg-input-hi); }
  .scw-b:active { transform: scale(.94); }
  .scw-x:hover { background: rgba(239, 68, 68, .16); color: var(--red, #ef4444); border-color: rgba(239, 68, 68, .4); }
  .scw-body { flex: 1; min-height: 0; display: flex; flex-direction: column; }
  .scw-empty { display: flex; align-items: center; justify-content: center; gap: 8px; height: 100%; color: var(--text-mute); font-size: 13px; }
</style>
