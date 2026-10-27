<script lang="ts">
  // ── Картка друга ────────────────────────────────────────────────────────
  // Фіксована висота 64px → список не стрибає, а браузер пропускає paint рядків
  // поза екраном (content-visibility). Дії при наведенні лежать ПОВЕРХ картки
  // (position:absolute), тож нік ніколи не стискається. У грі — картка
  // підсвічується кольором збірки гравця.
  import { useFriendsStore, clean, safeColor, initialOf } from './FriendsStore.svelte'
  import { t } from '../i18n.svelte'
  import { tip } from './tooltip'
  import { lastSeenText } from './lastSeen'
  import FAvatar from './FAvatar.svelte'
  import type { SocialFriend } from '../types'

  let { f, selected = false, compact = false, animate = false, readonly = false, subtitle = '' }: {
    f: SocialFriend
    selected?: boolean
    compact?: boolean
    animate?: boolean
    /** Не друг (колишній / незнайомець): без присутності, запрошень і профілю. */
    readonly?: boolean
    subtitle?: string
  } = $props()

  const s = useFriendsStore()
  const status = $derived(readonly ? 'offline' : (f.presence?.status ?? 'offline'))
  const unread = $derived(s.unreadOf(f.accountId))
  const pack = $derived(status === 'in_game' ? clean(f.presence?.packName) : '')
  const pc = $derived(safeColor(f.presence?.packColor))
  const typing = $derived(!!s.typingNow[f.accountId])
  const muted = $derived(s.isMuted(f.accountId))
  const name = $derived(clean(f.nickname))
  const tipText = $derived(compact ? name + (pack ? ' · ' + pack : '') : '')
  const canInvite = $derived(!readonly && !compact && (status === 'online' || status === 'in_game' || status === 'away'))
</script>

<div class="fr {status}" class:sel={selected} class:busy={s.busy === f.accountId} class:enter={animate}
     style={pc ? `--pc:${pc}` : ''}
     role="button" tabindex="0" aria-label={name} aria-current={selected ? 'true' : undefined}
     use:tip={tipText ? { text: tipText, side: 'top' } : ''}
     onclick={() => s.openChat(f)}
     oncontextmenu={(e) => { e.preventDefault(); if (!readonly) s.openProfile(f) }}
     onkeydown={(e) => { if (e.target === e.currentTarget && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); s.openChat(f) } }}>
  <button class="fr-av" tabindex={readonly || compact ? -1 : 0} disabled={readonly || compact}
          aria-label={t('ff.profile.open')}
          onclick={(e) => { e.stopPropagation(); s.openProfile(f) }}>
    <FAvatar size={compact ? 34 : 30} head={s.heads[f.nickname] ?? ''} initial={initialOf(f.nickname)} status={readonly ? '' : (status as any)} ring={pc} />
    {#if compact && unread > 0}<span class="ff-badge fr-ub">{unread > 99 ? '99+' : unread}</span>{/if}
  </button>

  {#if !compact}
    <div class="fr-info">
      <div class="fr-name" class:unread={unread > 0}>
        <span class="fr-nick">{name}</span>
      </div>
      {#if typing}
        <div class="fr-sub typing"><span class="dots"><i></i><i></i><i></i></span>{t('social.friend.typing')}</div>
      {:else if subtitle}
        <div class="fr-sub">{subtitle}</div>
      {:else if pack}
        <div class="fr-sub game"><i class="ti ti-device-gamepad-2"></i><span>{pack}</span></div>
      {:else if status === 'in_game'}
        <div class="fr-sub game"><i class="ti ti-device-gamepad-2"></i><span>{t('social.playing')}</span></div>
      {:else if status === 'away'}
        <div class="fr-sub away">{t('social.status.away')}</div>
      {:else if status === 'online'}
        <div class="fr-sub online">{t('social.status.online')}</div>
      {:else}
        <div class="fr-sub">{lastSeenText(f.presence?.lastSeenAt, s.nowTick) || t('social.status.offline')}</div>
      {/if}
    </div>

    <div class="fr-tail">
      {#if muted}<i class="ti ti-bell-off fr-muted" title={t('social.profile.muted')}></i>{/if}
      {#if unread > 0}<span class="ff-badge fr-unread">{unread > 99 ? '99+' : unread}</span>{/if}
    </div>

    {#if canInvite}
      <div class="fr-act">
        <button class="fr-act-b" data-testid="friend-temp-invite-btn" title={t('social.chat.inviteTip')} aria-label={t('social.chat.inviteTip')}
                onclick={(e) => { e.stopPropagation(); s.tempInviteFor = f.accountId }}>
          <i class="ti ti-plus"></i><span>{t('social.chat.inviteBtn')}</span>
        </button>
      </div>
    {/if}
  {/if}
</div>

<style>
  .fr { --pc: var(--orange); position: relative; display: flex; align-items: center; gap: 8px; height: 44px; padding: 0 8px 0 7px; margin: 1px 5px; border-radius: 10px;
    cursor: pointer; box-sizing: border-box; user-select: none; border: 1px solid transparent;
    content-visibility: auto; contain-intrinsic-size: auto 44px;
    transition: background-color .16s, border-color .16s; }
  .fr.enter { animation: fr-in .3s var(--ff-ease) both; }
  @keyframes fr-in { from { opacity: 0; transform: translateY(8px) scale(.985); } to { opacity: 1; transform: none; } }
  .fr:hover { background: var(--hover); }
  /* Гравець у грі: картка тоновано кольором його збірки. */
  .fr.in_game { background: linear-gradient(100deg, color-mix(in srgb, var(--pc) 15%, transparent), color-mix(in srgb, var(--pc) 3%, transparent) 70%); }
  .fr.in_game:hover { background: linear-gradient(100deg, color-mix(in srgb, var(--pc) 22%, transparent), color-mix(in srgb, var(--pc) 6%, transparent) 70%); }
  .fr.sel { background: linear-gradient(100deg, rgba(var(--orange-rgb), .2), rgba(var(--orange-rgb), .06)); border-color: rgba(var(--orange-rgb), .38); }
  .fr.offline:not(.sel):not(:hover) .fr-info { opacity: .7; }
  .fr.busy { opacity: .5; pointer-events: none; }
  .fr:focus-visible { outline: 2px solid color-mix(in srgb, var(--orange) 70%, transparent); outline-offset: -2px; }

  .fr-av { position: relative; flex: 0 0 auto; padding: 0; border: 0; background: none; border-radius: 12px; cursor: pointer; transition: transform .18s var(--ff-spring); }
  .fr-av:not(:disabled):hover { transform: scale(1.06); }
  /* disabled-кнопка не віддає клік батьківському рядку: у згорнутому списку аватар — майже вся клікабельна
     площа, тож чат відкривався лише при кліку повз нього («не з першого разу»). pointer-events:none
     пропускає клік до .fr, який і відкриває чат. */
  .fr-av:disabled { cursor: inherit; pointer-events: none; }
  .fr-av:focus-visible { outline: 2px solid var(--orange); outline-offset: 3px; }
  .fr-ub { position: absolute; top: -6px; right: -8px; min-width: 16px; height: 16px; padding: 0 4px; font-size: 9.5px; border: 2px solid var(--bg-panel); box-sizing: content-box; }

  .fr-info { flex: 1 1 0; min-width: 0; display: flex; flex-direction: column; gap: 2px; transition: opacity .2s; }
  .fr-name { display: flex; align-items: baseline; gap: 5px; min-width: 0; font: 650 12.5px/1.2 var(--font-ui); color: var(--text); }
  .fr-name.unread { font-weight: 800; }
  .fr-nick { flex: 0 1 auto; min-width: 3ch; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .fr-sub { display: flex; align-items: center; gap: 5px; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font: 500 10.5px/1.2 var(--font-ui); color: var(--text-dim); }
  .fr-sub.online { color: var(--ff-ok); }
  .fr-sub.away { color: var(--ff-warn); }
  .fr-sub.typing { color: var(--orange); }
  .fr-sub.game { color: color-mix(in srgb, var(--pc) 80%, var(--text)); font-weight: 650; }
  .fr-sub.game i { flex: 0 0 auto; font-size: 13px; color: var(--pc); }
  .fr-sub.game span { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

  .fr-tail { flex: 0 0 auto; display: flex; align-items: center; gap: 8px; }
  .fr-muted { font-size: 14px; color: var(--text-dim); }
  .fr-unread { min-width: 18px; height: 18px; border-radius: 9px; font-size: 10px; box-shadow: 0 4px 12px rgba(var(--orange-rgb), .35); }

  /* Швидка дія: накладається на правий край, а не штовхає вміст. */
  .fr-act { position: absolute; right: 5px; top: 0; bottom: 0; display: flex; align-items: center; padding-left: 26px; opacity: 0; pointer-events: none; transition: opacity .14s;
    background: linear-gradient(90deg, transparent, var(--bg-panel) 24px); border-radius: 0 10px 10px 0; }
  .fr:hover .fr-act, .fr:focus-within .fr-act { opacity: 1; pointer-events: auto; }
  .fr-act-b { display: inline-flex; align-items: center; gap: 4px; height: 24px; padding: 0 8px; border-radius: 7px; cursor: pointer; white-space: nowrap;
    font: 700 10.5px/1 var(--font-ui); color: #fff; background: linear-gradient(135deg, var(--orange), var(--orange-2, var(--orange))); border: 0;
    box-shadow: 0 3px 8px rgba(var(--orange-rgb), .24); transition: transform .12s, filter .14s; }
  .fr-act-b:hover { filter: brightness(1.08); }
  .fr-act-b:active { transform: scale(.94); }
  .fr-act-b:focus-visible { outline: 2px solid var(--text); outline-offset: 2px; }

  .dots { display: inline-flex; gap: 3px; align-items: center; }
  .dots i { width: 4px; height: 4px; border-radius: 50%; background: currentColor; animation: dot-b 1.1s ease-in-out infinite; }
  .dots i:nth-child(2) { animation-delay: .15s; } .dots i:nth-child(3) { animation-delay: .3s; }
  @keyframes dot-b { 0%, 60%, 100% { transform: translateY(0); opacity: .45; } 30% { transform: translateY(-3px); opacity: 1; } }

  /* Згорнутий список-рейка: лише аватари по центру. */
  :global(.fl.compact) .fr { justify-content: center; padding: 0; margin: 2px 5px; height: 44px; contain-intrinsic-size: auto 44px; }

  @media (prefers-reduced-motion: reduce) { .fr.enter, .dots i { animation: none; } .fr, .fr-act, .fr-av { transition: none; } }
</style>
