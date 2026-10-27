<script lang="ts">
  // Картка запрошення на сервер / тимчасовий доступ у чаті. Та сама мова оформлення, що й
  // ModCard (обкладинка з пігулками, іконка, заголовок, статистика, кнопки) — щоб обидві
  // «поширені» картки виглядали як одна родина. Уся логіка (вхід, запуск, копіювання)
  // лишається в ShaurmaChat: тут лише розмітка й колбеки.
  import { t } from '../i18n.svelte'
  import { tip } from './tooltip'

  let {
    title, kind = '', temp = false, dead = false, deadText = '', addr,
    version = '', players = 0, max = 0,
    host = false, own = false, canJoin = false, joining = false,
    onJoin, onPlay, onCopy,
  }: {
    title: string
    kind?: string
    temp?: boolean
    dead?: boolean
    deadText?: string
    addr: string
    version?: string
    players?: number
    max?: number
    host?: boolean
    own?: boolean
    canJoin?: boolean
    joining?: boolean
    onJoin: () => void
    onPlay: () => void
    onCopy: () => void
  } = $props()
</script>

<article class="ic" class:dead>
  <div class="ic-cover">
    <i class="ti ti-server-2 ic-deco" aria-hidden="true"></i>
    <div class="ic-cover-fade"></div>
    <span class="ic-pill type"><i class="ti {temp ? 'ti-door-enter' : 'ti-device-gamepad-2'}"></i> {temp ? 'Тимчасовий доступ' : t('social.chat.invite.title')}</span>
    <span class="ic-pill st" class:off={dead}>
      <i class="ti {dead ? 'ti-circle-x' : 'ti-circle-check-filled'}"></i> {dead ? 'закрито' : 'працює'}
    </span>
  </div>

  <div class="ic-main">
    <div class="ic-ic"><i class="ti {temp ? 'ti-door-enter' : 'ti-server-2'}"></i></div>
    <div class="ic-titles">
      <div class="ic-name" title={title}>{title}</div>
      {#if kind}<div class="ic-by"><i class="ti ti-broadcast"></i><span>{kind}</span></div>{/if}
    </div>
  </div>

  <div class="ic-pad">
    {#if temp}
      <div class="ic-stats">
        <span class="ic-stat"><i class="ti ti-cube"></i> {version || '—'}</span>
        <span class="ic-stat"><i class="ti ti-users"></i> {max ? `${players}/${max}` : '—'}</span>
        <span class="ic-stat"><i class="ti ti-lock"></i> лише для запрошених</span>
      </div>
    {/if}

    <button class="ic-addr" data-testid="chat-invite-copy" onclick={onCopy} use:tip={t('social.chat.invite.copy')} aria-label={t('social.chat.invite.copy')}>
      <i class="ti ti-server-2"></i>
      <span class="ic-addr-t" title={addr}>{addr}</span>
      <i class="ti ti-copy ic-addr-cp"></i>
    </button>

    {#if dead}
      <div class="ic-note bad" data-testid="chat-invite-dead"><i class="ti ti-circle-x"></i> {deadText}</div>
    {:else if host}
      <div class="ic-note"><i class="ti ti-broadcast"></i> Ваш світ відкритий — друзі бачать цю картку</div>
    {:else}
      <div class="ic-acts">
        {#if canJoin}
          <button class="ic-btn pri" data-testid="chat-invite-join" onclick={onJoin} disabled={joining}>
            {#if joining}<i class="ti ti-loader-2 ic-spin"></i>{:else}<i class="ti ti-player-play"></i>{/if}
            {t('social.chat.invite.join')}
          </button>
        {/if}
        {#if !own}
          <button class="ic-btn {canJoin ? 'ghost grow' : 'pri'}" data-testid="chat-invite-play" onclick={onPlay}>
            <i class="ti ti-device-gamepad-2"></i> {t('servers.p2p.shared.play')}
          </button>
        {:else}
          <span class="ic-own"><i class="ti ti-user-check"></i> це ваша кімната</span>
        {/if}
      </div>
    {/if}
  </div>
</article>

<style>
  .ic { position: relative; width: 100%; min-width: 0; border-radius: 14px; overflow: hidden; background: var(--bg-input);
    border: 1px solid var(--border-hi); animation: ic-in .34s cubic-bezier(.2,1.1,.3,1) both;
    transition: border-color .18s, box-shadow .18s, transform .18s, opacity .18s; }
  .ic:not(.dead):hover { border-color: rgba(var(--orange-rgb), .45); box-shadow: 0 8px 26px rgba(0,0,0,.35); transform: translateY(-1px); }
  .ic.dead { opacity: .62; }
  @keyframes ic-in { from { opacity: 0; transform: translateY(6px) scale(.97); } to { opacity: 1; transform: none; } }

  .ic-cover { position: relative; height: 64px; overflow: hidden;
    background: linear-gradient(135deg, rgba(var(--orange-rgb), .35), rgba(var(--orange-rgb), .06) 70%), var(--bg-panel); }
  .ic-deco { position: absolute; right: 16px; top: 50%; transform: translateY(-50%) rotate(-8deg); font-size: 64px; color: rgba(255, 255, 255, .09); pointer-events: none; }
  .ic-cover-fade { position: absolute; inset: 0; background: linear-gradient(180deg, rgba(0,0,0,.28) 0%, rgba(0,0,0,0) 40%, var(--bg-input) 100%); }
  .ic-pill { position: absolute; top: 8px; display: inline-flex; align-items: center; gap: 5px; padding: 3px 9px; border-radius: 999px;
    font: 700 10px/1.4 var(--font-ui); letter-spacing: .2px; color: #fff; background: rgba(0,0,0,.55);
    border: 1px solid rgba(255,255,255,.14); }
  .ic-pill.type { left: 8px; }
  .ic-pill.st { right: 8px; background: rgba(27, 217, 106, .22); border-color: rgba(27, 217, 106, .5); }
  .ic-pill.st.off { background: rgba(239, 68, 68, .24); border-color: rgba(239, 68, 68, .5); }

  .ic-main { display: flex; align-items: flex-end; gap: 12px; padding: 0 12px; margin-top: -26px; position: relative; }
  .ic-ic { flex: 0 0 auto; width: 58px; height: 58px; border-radius: 16px; overflow: hidden; background: var(--bg-panel);
    border: 2px solid var(--bg-input); box-shadow: 0 6px 18px rgba(0,0,0,.45), 0 0 0 1px var(--border-hi);
    display: flex; align-items: center; justify-content: center; color: var(--orange); font-size: 26px; }
  .ic-titles { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 3px; padding-bottom: 2px; }
  .ic-name { font: 800 15px/1.2 var(--font-ui); color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; letter-spacing: -.1px; }
  .ic-by { display: flex; align-items: center; gap: 6px; min-width: 0; font: 600 11.5px/1.2 var(--font-ui); color: var(--text-mute); }
  .ic-by i { font-size: 13px; color: var(--orange); }
  .ic-by span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

  .ic-pad { padding: 9px 12px 12px; display: flex; flex-direction: column; gap: 8px; }
  .ic-stats { display: flex; align-items: center; flex-wrap: wrap; gap: 6px 14px; font: 650 11.5px/1 var(--font-ui); color: var(--text-mute); }
  .ic-stat { display: inline-flex; align-items: center; gap: 5px; cursor: default; }
  .ic-stat i { font-size: 13px; color: var(--orange); }

  .ic-addr { display: flex; align-items: center; gap: 8px; width: 100%; min-width: 0; padding: 7px 10px; border-radius: 10px; cursor: pointer; text-align: left;
    background: var(--bg-panel); border: 1px solid var(--border-hi); color: var(--text-mute);
    font: 600 11.5px/1.3 var(--font-mono, monospace); transition: border-color .14s, color .14s; }
  .ic-addr:hover { color: var(--text); border-color: rgba(var(--orange-rgb), .5); }
  .ic-addr > i { flex: 0 0 auto; font-size: 14px; }
  .ic-addr-t { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .ic-addr-cp { opacity: .7; }

  .ic-note { display: flex; align-items: center; gap: 6px; font: 650 11px/1.3 var(--font-ui); color: var(--text-mute); }
  .ic-note i { font-size: 14px; color: var(--orange); }
  .ic-note.bad { color: var(--red); }
  .ic-note.bad i { color: var(--red); }

  .ic-acts { display: flex; gap: 6px; margin-top: 2px; align-items: center; }
  .ic-btn { display: inline-flex; align-items: center; justify-content: center; gap: 7px; padding: 8px 12px; border-radius: 10px; border: 1px solid transparent; cursor: pointer;
    font: 750 12px/1 var(--font-ui); transition: filter .14s, transform .12s, background .14s, border-color .14s, color .14s; }
  .ic-btn:active:not(:disabled) { transform: scale(.96); }
  .ic-btn:disabled { opacity: .6; cursor: default; }
  .ic-btn.pri { flex: 1; background: linear-gradient(135deg, var(--orange), var(--orange-2, var(--orange))); color: #fff; box-shadow: 0 6px 14px rgba(var(--orange-rgb), .28); }
  .ic-btn.pri:hover:not(:disabled) { filter: brightness(1.1); }
  .ic-btn.ghost { background: var(--bg-panel); color: var(--text-mute); border-color: var(--border-hi); }
  .ic-btn.ghost:hover { color: var(--text); border-color: rgba(var(--orange-rgb), .5); }
  .ic-btn.grow { flex: 1; }
  .ic-btn i { font-size: 15px; }
  .ic-own { display: inline-flex; align-items: center; gap: 5px; font: 650 11px/1 var(--font-ui); color: var(--text-dim); }
  .ic-spin { animation: ic-rot .8s linear infinite; }
  @keyframes ic-rot { to { transform: rotate(360deg); } }
  @media (prefers-reduced-motion: reduce) { .ic, .ic-spin { animation: none; } .ic:hover { transform: none; } }
</style>
