<script lang="ts">
  // ── Профіль друга (read-only) ───────────────────────────────────────────
  // Те саме модульне вікно, що й «Мій профіль», але без налаштувань: видно
  // лише те, що власник дозволив показати (статус, збірка, біо, колір,
  // @handle, дата дружби). Приватність, «Вигляд» і редагування тут не
  // дублюються — це профіль ІНШОГО гравця, а не власний акаунт.
  import { onDestroy } from 'svelte'
  import UiModal from '../UiModal.svelte'
  import { App } from '../wails'
  import { toast } from '../toast.svelte'
  import { t } from '../i18n.svelte'
  import { lastSeenText } from './lastSeen'
  import { tip } from './tooltip'
  import { buildFriendLink } from './friendLink'
  import { safeBio, safeColor, hueOf, shortId } from './profileStyle'
  import type { SocialFriend } from '../types'

  let {
    friend = null,
    head = '',
    busy = false,
    muted = false,
    blocked = false,
    onmute = () => {},
    onblock = () => {},
    onclose = () => {},
    onmessage = () => {},
    onremove = () => {},
  }: {
    friend?: SocialFriend | null
    head?: string
    busy?: boolean
    muted?: boolean
    blocked?: boolean
    onmute?: (f: SocialFriend) => void
    onblock?: (f: SocialFriend) => void
    onclose?: () => void
    onmessage?: (f: SocialFriend) => void
    onremove?: (f: SocialFriend) => void
  } = $props()

  const HIDDEN = /[\u0000-\u001f\u007f-\u009f\u200b-\u200f\u202a-\u202e\u2066-\u2069\ufeff]/g
  const clean = (s?: string) => (s ?? '').replace(HIDDEN, '').trim()

  // Лишаємо останній непорожній стан: під час закриття/видалення друга
  // `friend` уже null, а вікно ще має домальовуватись без стрибків.
  let shown = $state<SocialFriend | null>(null)
  $effect(() => { if (friend) shown = friend })
  const f = $derived(shown)

  const status = $derived(f?.presence?.status ?? 'offline')
  const packName = $derived(clean(f?.presence?.packName))
  const packColor = $derived(safeColor(f?.presence?.packColor))
  const bio = $derived(safeBio(f?.bio))
  const color = $derived(safeColor(f?.color))
  const hue = $derived(hueOf(f?.accountId ?? ''))
  const statusText = $derived(
    status === 'in_game' ? (packName ? t('social.profile.playingPack', { pack: packName }) : t('social.playing'))
    : status === 'online' ? t('social.status.online')
    : status === 'away' ? t('social.status.away')
    : (lastSeenText(f?.presence?.lastSeenAt) || t('social.status.offline')))
  const since = $derived.by(() => {
    if (!f?.since) return ''
    try {
      return new Date(f.since).toLocaleDateString(document.documentElement.lang || 'uk', { day: 'numeric', month: 'long', year: 'numeric' })
    } catch { return '' }
  })
  const link = $derived(f ? buildFriendLink(f.accountId, clean(f.nickname), f.handle ?? '') : '')

  let copied = $state('')
  let copiedTimer: ReturnType<typeof setTimeout> | undefined
  async function copy(what: string, text: string, msgKey = 'social.idCopied') {
    if (!text) return
    try { await App.CopyToClipboard(text) } catch {
      try { await navigator.clipboard?.writeText(text) } catch { toast(t('social.profile.copyFail'), 'error'); return }
    }
    copied = what
    clearTimeout(copiedTimer)
    copiedTimer = setTimeout(() => { copied = '' }, 1400)
    toast(t(msgKey), 'success')
  }

  onDestroy(() => clearTimeout(copiedTimer))
</script>

<UiModal
  open={!!friend && !!f}
  title={f ? clean(f.nickname) : t('social.profile.title')}
  subtitle={statusText}
  icon="ti-user-circle"
  color={color || `hsl(${hue} 72% 52%)`}
  width="760px"
  bodyClass="fpm-ui"
  onclose={onclose}
>
  {#if f}
    <div class="fpm" style={color ? `--cv:${color}` : `--h:${hue}`}>
      <aside class="fpm-hero">
        <div class="fpm-cover" aria-hidden="true"></div>
        <div class="fpm-ava" class:ing={status === 'in_game'}>
          {#if head}<img src={head} alt="" />{:else}<span>{clean(f.nickname).charAt(0).toUpperCase()}</span>{/if}
          <i class="fpm-dot {status}" aria-hidden="true"></i>
        </div>
        <div class="fpm-name">{clean(f.nickname)}</div>
        <div class="fpm-status {status}">{statusText}</div>

        <div class="fpm-badges">
          {#if f.handle}
            <button class="fpm-badge" onclick={() => copy('handle', '@' + f!.handle)} use:tip={t('social.profile.handle')}>
              <i class="ti ti-at"></i> <b>@{f.handle}</b>
              <i class="ti {copied === 'handle' ? 'ti-check ok' : 'ti-copy'}"></i>
            </button>
          {/if}
          {#if since}
            <span class="fpm-badge plain"><i class="ti ti-heart-handshake"></i> <b>{since}</b></span>
          {/if}
        </div>

        {#if muted || blocked}
          <div class="fpm-flags">
            {#if muted}<span class="fpm-flag"><i class="ti ti-bell-off"></i> {t('social.profile.muted')}</span>{/if}
            {#if blocked}<span class="fpm-flag ban"><i class="ti ti-ban"></i> {t('social.profile.blocked')}</span>{/if}
          </div>
        {/if}

        <div class="fpm-acts">
          <button class="fpm-btn pri" onclick={() => onmessage(f!)}>
            <i class="ti ti-message-circle-2"></i> {t('social.profile.message')}
          </button>
          <button class="fpm-btn ghost" class:on={muted} data-testid="profile-mute-btn"
                  onclick={() => onmute(f!)}
                  use:tip={muted ? t('social.profile.muteOff') : t('social.profile.muteOn')}
                  aria-label={muted ? t('social.profile.muteOff') : t('social.profile.muteOn')} aria-pressed={muted}>
            <i class="ti {muted ? 'ti-bell-off' : 'ti-bell'}"></i>
          </button>
          <button class="fpm-btn ghost" onclick={() => copy('link', link, 'social.profile.linkCopied')}
                  use:tip={t('social.profile.copyLink')} aria-label={t('social.profile.copyLink')}>
            <i class="ti {copied === 'link' ? 'ti-check' : 'ti-link'}"></i>
          </button>
        </div>
      </aside>

      <section class="fpm-main">
        <div class="fpm-tiles">
          <div class="fpm-tile">
            <span class="fpm-tile-ic"><i class="ti ti-circle-dot"></i></span>
            <b class="fpm-tile-v">{status === 'in_game' ? t('friendPicker.status.in_game') : status === 'online' ? t('friendPicker.status.online') : status === 'away' ? t('friendPicker.status.away') : t('friendPicker.status.offline')}</b>
            <small>{t('acc.modal.tab.profile')}</small>
          </div>
          <div class="fpm-tile">
            <span class="fpm-tile-ic"><i class="ti ti-device-gamepad-2"></i></span>
            <b class="fpm-tile-v sm">{packName || '—'}</b>
            <small>{t('social.playing')}</small>
          </div>
          <div class="fpm-tile">
            <span class="fpm-tile-ic"><i class="ti ti-calendar-heart"></i></span>
            <b class="fpm-tile-v sm">{since || '—'}</b>
            <small>{t('social.profile.since')}</small>
          </div>
        </div>

        {#if bio}
          <div class="fpm-card">
            <div class="fpm-card-t"><i class="ti ti-quote"></i> {t('acc.modal.bio')}</div>
            <p class="fpm-bio">{bio}</p>
          </div>
        {/if}

        <div class="fpm-card">
          <div class="fpm-card-t"><i class="ti ti-id-badge-2"></i> {t('acc.modal.tab.profile')}</div>
          <div class="fpm-list">
            {#if f.handle}
              <button class="fpm-row" onclick={() => copy('handle', '@' + f!.handle)} aria-label={t('social.profile.handle') + ': @' + f.handle}>
                <span class="fpm-row-ic"><i class="ti ti-at"></i></span>
                <span class="fpm-row-t"><b>@{f.handle}</b><small>{t('social.profile.handle')}</small></span>
                <i class="ti {copied === 'handle' ? 'ti-check ok' : 'ti-copy'} fpm-cp"></i>
              </button>
            {/if}
            <button class="fpm-row" onclick={() => copy('id', f!.accountId)} aria-label={t('social.profile.id') + ': ' + f.accountId} use:tip={f.accountId}>
              <span class="fpm-row-ic"><i class="ti ti-fingerprint"></i></span>
              <span class="fpm-row-t"><b class="mono">{shortId(f.accountId)}</b><small>{t('social.profile.id')}</small></span>
              <i class="ti {copied === 'id' ? 'ti-check ok' : 'ti-copy'} fpm-cp"></i>
            </button>
            {#if since}
              <div class="fpm-row static">
                <span class="fpm-row-ic"><i class="ti ti-heart-handshake"></i></span>
                <span class="fpm-row-t"><b>{since}</b><small>{t('social.profile.since')}</small></span>
              </div>
            {/if}
          </div>
        </div>

        <div class="fpm-card danger">
          <div class="fpm-card-t"><i class="ti ti-shield-lock"></i> {t('social.priv.title')}</div>
          <div class="fpm-danger">
            <button class="fpm-danger-btn" disabled={busy} data-testid="profile-block-btn" onclick={() => onblock(f!)}>
              <i class="ti ti-ban"></i> {blocked ? t('social.profile.unblock') : t('social.profile.block')}
            </button>
            <button class="fpm-danger-btn" disabled={busy} onclick={() => onremove(f!)}>
              {#if busy}<i class="ti ti-loader-2 spin"></i>{:else}<i class="ti ti-user-minus"></i>{/if}
              {t('social.profile.remove')}
            </button>
          </div>
        </div>
      </section>
    </div>
  {/if}
</UiModal>

<style>
  .fpm { --cv: hsl(var(--h, 24) 72% 52%); display: grid; grid-template-columns: 252px minmax(0, 1fr); gap: 16px; align-items: start; }

  /* ── Ліва колонка ── */
  .fpm-hero {
    position: relative; display: flex; flex-direction: column; align-items: center; gap: 8px;
    padding: 0 14px 16px; overflow: hidden; border-radius: 18px;
    background: linear-gradient(180deg, color-mix(in srgb, var(--cv) 16%, var(--bg-panel)), var(--bg-panel) 68%);
    border: 1px solid color-mix(in srgb, var(--cv) 32%, var(--border));
  }
  .fpm-cover {
    align-self: stretch; height: 74px; margin: 0 -14px 0;
    background:
      radial-gradient(120% 140% at 15% 0%, color-mix(in srgb, var(--cv) 90%, #fff 10%), transparent 62%),
      radial-gradient(110% 130% at 100% 100%, color-mix(in srgb, var(--cv) 55%, #000), transparent 62%),
      color-mix(in srgb, var(--cv) 38%, var(--bg-input));
  }
  .fpm-ava {
    position: relative; width: 82px; height: 82px; margin-top: -42px; border-radius: 22px;
    display: grid; place-items: center; overflow: visible; color: var(--text-mute);
    background: var(--bg-input); border: 3px solid var(--bg-panel);
    box-shadow: 0 10px 26px rgba(0,0,0,.35);
  }
  .fpm-ava img { width: 100%; height: 100%; object-fit: cover; image-rendering: pixelated; border-radius: 19px; display: block; }
  .fpm-ava > span { font: 800 30px/1 var(--font-ui); }
  .fpm-ava.ing { box-shadow: 0 10px 26px rgba(0,0,0,.35), 0 0 0 2px rgba(var(--orange-rgb), .55); }
  .fpm-dot { position: absolute; right: -4px; bottom: -4px; width: 18px; height: 18px; border-radius: 50%; border: 3px solid var(--bg-panel); background: var(--text-dim); }
  .fpm-dot.online { background: var(--ff-ok, #22c55e); } .fpm-dot.away { background: var(--ff-warn, #eab308); } .fpm-dot.in_game { background: var(--orange); }

  .fpm-name { margin-top: 2px; max-width: 100%; font: 800 18px/1.15 var(--font-ui); color: var(--text); overflow-wrap: anywhere; text-align: center; }
  .fpm-status { max-width: 100%; font: 600 11.5px/1.3 var(--font-ui); color: var(--text-dim); text-align: center; overflow-wrap: anywhere; }
  .fpm-status.online { color: var(--ff-ok, #22c55e); } .fpm-status.away { color: var(--ff-warn, #eab308); }
  .fpm-status.in_game { color: var(--pc, var(--orange)); }

  .fpm-badges { display: flex; flex-wrap: wrap; justify-content: center; gap: 6px; margin-top: 2px; }
  .fpm-badge {
    display: inline-flex; align-items: center; gap: 5px; max-width: 100%; padding: 4px 9px; border-radius: 999px;
    border: 1px solid var(--border); background: var(--hover); color: var(--text-mute); font: 600 10.5px/1.2 var(--font-ui);
    cursor: pointer; transition: border-color .14s, color .14s, background .14s;
  }
  .fpm-badge.plain { cursor: default; }
  .fpm-badge b { font-weight: 700; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .fpm-badge:hover:not(.plain) { color: var(--text); border-color: color-mix(in srgb, var(--cv) 50%, var(--border)); }
  .fpm-badge .ti { font-size: 12px; }
  .fpm-badge .ok, .fpm-cp.ok { color: var(--ff-ok, #22c55e); }

  .fpm-flags { display: flex; flex-wrap: wrap; justify-content: center; gap: 5px; }
  .fpm-flag { display: inline-flex; align-items: center; gap: 5px; padding: 3px 8px; border-radius: 999px; font: 650 10px/1 var(--font-ui);
    color: var(--red); background: color-mix(in srgb, var(--red) 12%, transparent); border: 1px solid color-mix(in srgb, var(--red) 32%, transparent); }
  .fpm-flag.ban { color: var(--red); }
  .fpm-flag i { font-size: 11px; }

  .fpm-acts { display: flex; gap: 6px; width: 100%; margin-top: 4px; }
  .fpm-btn {
    display: inline-flex; align-items: center; justify-content: center; gap: 6px; height: 32px; padding: 0 11px;
    border-radius: 9px; border: 1px solid transparent; cursor: pointer; font: 750 11.5px/1 var(--font-ui);
    transition: filter .14s, transform .12s, border-color .14s, color .14s, background .14s;
  }
  .fpm-btn:active { transform: scale(.96); }
  .fpm-btn i { font-size: 15px; }
  .fpm-btn.pri { flex: 1; min-width: 0; color: #111; background: var(--orange); }
  .fpm-btn.pri:hover { filter: brightness(1.08); }
  .fpm-btn.ghost { width: 32px; padding: 0; color: var(--text-mute); background: var(--bg-input); border-color: var(--border-hi); }
  .fpm-btn.ghost:hover { color: var(--text); border-color: color-mix(in srgb, var(--orange) 55%, transparent); }
  .fpm-btn.ghost.on { color: var(--red); border-color: color-mix(in srgb, var(--red) 45%, transparent); background: color-mix(in srgb, var(--red) 10%, transparent); }

  /* ── Права колонка ── */
  .fpm-main { display: grid; gap: 10px; min-width: 0; }
  .fpm-tiles { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px; }
  .fpm-tile {
    position: relative; display: grid; gap: 3px; padding: 11px 12px; border-radius: 12px;
    background: var(--bg-input); border: 1px solid var(--border); overflow: hidden;
  }
  .fpm-tile-ic { position: absolute; right: 9px; top: 9px; font-size: 17px; color: color-mix(in srgb, var(--cv) 75%, var(--text-dim)); opacity: .55; }
  .fpm-tile-ic i { font-size: inherit; }
  .fpm-tile-v { font: 800 17px/1.15 var(--font-ui); color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; padding-right: 22px; }
  .fpm-tile-v.sm { font-size: 13px; padding-top: 3px; }
  .fpm-tile small { font: 500 10.5px/1.2 var(--font-ui); color: var(--text-mute); }

  .fpm-card { display: grid; gap: 8px; padding: 12px; border-radius: 14px; background: var(--bg-input); border: 1px solid var(--border); }
  .fpm-card-t { display: flex; align-items: center; gap: 6px; font: 700 11.5px/1 var(--font-ui); color: var(--text-dim); }
  .fpm-card-t i { color: var(--orange); font-size: 14px; }
  .fpm-bio { margin: 0; font: 450 12.5px/1.5 var(--font-ui); color: var(--text); overflow-wrap: anywhere; white-space: pre-wrap; }

  .fpm-list { display: flex; flex-direction: column; gap: 2px; }
  .fpm-row {
    display: flex; align-items: center; gap: 10px; width: 100%; padding: 8px 9px; border: 0; border-radius: 9px;
    background: transparent; color: var(--text); text-align: left; cursor: pointer; font: inherit; transition: background .12s;
  }
  .fpm-row.static { cursor: default; }
  .fpm-row:not(.static):hover { background: var(--hover); }
  .fpm-row-ic { flex: 0 0 auto; width: 28px; height: 28px; display: grid; place-items: center; border-radius: 8px; font-size: 14px;
    color: color-mix(in srgb, var(--cv) 80%, var(--text)); background: color-mix(in srgb, var(--cv) 14%, transparent); }
  .fpm-row-t { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 1px; }
  .fpm-row-t b { font: 650 12.5px/1.25 var(--font-ui); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .fpm-row-t b.mono { font: 500 11.5px/1.25 var(--font-mono); color: var(--text-mute); }
  .fpm-row-t small { font: 500 10px/1.2 var(--font-ui); color: var(--text-dim); }
  .fpm-cp { font-size: 14px; color: var(--text-dim); opacity: .6; transition: opacity .12s, color .12s; }
  .fpm-row:hover .fpm-cp, .fpm-row:focus-visible .fpm-cp { opacity: 1; }

  .fpm-card.danger { border-color: color-mix(in srgb, var(--red) 24%, var(--border)); }
  .fpm-card.danger .fpm-card-t i { color: var(--red); }
  .fpm-danger { display: flex; flex-wrap: wrap; gap: 6px; }
  .fpm-danger-btn {
    display: inline-flex; align-items: center; justify-content: center; gap: 6px; height: 30px; padding: 0 11px;
    border-radius: 8px; border: 1px solid color-mix(in srgb, var(--red) 32%, transparent);
    background: color-mix(in srgb, var(--red) 8%, transparent); color: var(--red);
    font: 700 11px/1 var(--font-ui); cursor: pointer; transition: background .14s, color .14s;
  }
  .fpm-danger-btn:hover:not(:disabled) { color: #fff; background: var(--red); }
  .fpm-danger-btn:disabled { opacity: .55; cursor: default; }
  .fpm-danger-btn i { font-size: 14px; }

  @media (max-width: 680px) {
    .fpm { grid-template-columns: minmax(0, 1fr); }
    .fpm-tiles { grid-template-columns: minmax(0, 1fr); }
  }
  @media (prefers-reduced-motion: reduce) { .fpm-badge, .fpm-btn, .fpm-cp { transition: none; } }
</style>
