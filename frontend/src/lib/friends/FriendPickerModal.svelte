<script lang="ts">
  // FriendPickerModal — спільне вікно вибору друзів. Те саме, що PackPicker для збірок і
  // ServerTargetList для серверів, але для людей: голова, колір профілю, нік, @handle і статус
  // «в мережі / у грі / відійшов / не в мережі». Використовується скрізь, де треба вибрати друзів
  // (списки доступу P2P, співхости), без власних копій списку в кожному екрані.
  //
  // Контракт: onconfirm(friends) — вибрані друзі (в одиночному режимі — один). Друзі, чиї accountId
  // є в `exclude`, показуються приглушеними з позначкою `excludedLabel` і не вибираються.
  import { onMount } from 'svelte'
  import Portal from '../Portal.svelte'
  import FriendAvatar from './FriendAvatar.svelte'
  import { App } from '../wails'
  import { t } from '../i18n.svelte'
  import { friendColor } from './friendHead'
  import type { SocialFriend, SocialStatus } from '../types'

  let {
    title = '',
    subtitle = '',
    multi = true,
    exclude = new Set<string>(),
    excludedLabel = '',
    confirmLabel = '',
    friends: given = null as SocialFriend[] | null,
    // Додатково: z-index (щоб вікно було над модалкою-власником), ліміт вибору (0 = без ліміту),
    // busy (триває дія — блокує повторне підтвердження й закриття), onback — кнопка «Назад».
    zIndex = 420,
    max = 0,
    busy = false,
    onback = null as (() => void) | null,
    onconfirm = (_f: SocialFriend[]) => {},
    onclose = () => {},
  }: {
    title?: string
    subtitle?: string
    multi?: boolean
    exclude?: Set<string>
    excludedLabel?: string
    confirmLabel?: string
    friends?: SocialFriend[] | null
    zIndex?: number
    max?: number
    busy?: boolean
    onback?: (() => void) | null
    onconfirm?: (f: SocialFriend[]) => void
    onclose?: () => void
  } = $props()

  let friends = $state<SocialFriend[]>(given ?? [])
  let loading = $state(!given)
  let loggedIn = $state(true)
  let query = $state('')
  let chosen = $state<Set<string>>(new Set())

  onMount(async () => {
    if (given) return
    try {
      const st = await App.GetFriendsState()
      loggedIn = !!st?.loggedIn
      friends = Array.isArray(st?.friends) ? st.friends : []
    } catch { loggedIn = false; friends = [] }
    finally { loading = false }
  })

  const RANK: Record<SocialStatus, number> = { in_game: 0, online: 1, away: 2, offline: 3 }
  const statusOf = (f: SocialFriend): SocialStatus => f.presence?.status ?? 'offline'
  const taken = (f: SocialFriend) => exclude.has(f.accountId.toLowerCase())

  const list = $derived.by(() => {
    const q = query.trim().toLowerCase().replace(/^@/, '')
    return friends
      .filter((f) => !q || f.nickname.toLowerCase().includes(q) || (f.handle ?? '').toLowerCase().includes(q))
      .sort((a, b) => Number(taken(a)) - Number(taken(b)) || RANK[statusOf(a)] - RANK[statusOf(b)] || a.nickname.localeCompare(b.nickname))
  })
  const onlineCount = $derived(friends.filter((f) => statusOf(f) !== 'offline').length)

  function pick(f: SocialFriend) {
    if (taken(f)) return
    if (!multi) { onconfirm([f]); return }
    if (busy) return
    const s = new Set(chosen)
    if (s.has(f.accountId)) s.delete(f.accountId)
    else if (max > 0 && s.size >= max) return
    else s.add(f.accountId)
    chosen = s
  }
  const full = $derived(max > 0 && chosen.size >= max)
  function confirm() {
    if (busy || chosen.size === 0) return
    onconfirm(friends.filter((f) => chosen.has(f.accountId)))
  }
  function close() { if (!busy) onclose() }
  function onKey(e: KeyboardEvent) { if (e.key === 'Escape') close() }

  const statusText = (s: SocialStatus) => t(`friendPicker.status.${s}`)
</script>

<svelte:window onkeydown={onKey} />

<Portal>
  <div class="fp-overlay" style="z-index:{zIndex}" onclick={close} role="presentation">
    <div class="fp-modal" onclick={(e: MouseEvent) => e.stopPropagation()} role="dialog" aria-modal="true" aria-label={title || t('friendPicker.title')}>
      <div class="fp-head">
        <div class="fp-ic"><i class="ti ti-users"></i></div>
        <div class="fp-tt">
          <div class="fp-title">{title || t('friendPicker.title')}</div>
          {#if subtitle}<div class="fp-sub">{subtitle}</div>{/if}
        </div>
        <button class="fp-x" onclick={close} disabled={busy} aria-label={t('cancel')}><i class="ti ti-x"></i></button>
      </div>

      {#if friends.length > 6}
        <div class="fp-search">
          <i class="ti ti-search"></i>
          <input bind:value={query} placeholder={t('friendPicker.search')} spellcheck="false" autocomplete="off" />
        </div>
      {/if}

      <div class="fp-list">
        {#if loading}
          <div class="fp-state"><i class="ti ti-loader-2 spin"></i></div>
        {:else if !loggedIn}
          <div class="fp-state">{t('friendPicker.loginNeeded')}</div>
        {:else if friends.length === 0}
          <div class="fp-state">{t('friendPicker.empty')}</div>
        {:else if list.length === 0}
          <div class="fp-state">{t('friendPicker.noMatch')}</div>
        {:else}
          {#each list as f (f.accountId)}
            {@const st = statusOf(f)}
            {@const off = taken(f)}
            <button class="fp-row" class:on={chosen.has(f.accountId)} class:off class:full={full && !chosen.has(f.accountId) && !off} disabled={off || busy} onclick={() => pick(f)}>
              <FriendAvatar nickname={f.nickname} color={f.color} status={st} size={34} />
              <span class="fp-main">
                <span class="fp-nick" style="color:{friendColor(f.color, 'var(--text)')}">{f.nickname}</span>
                <span class="fp-meta">
                  {off && excludedLabel ? excludedLabel : statusText(st)}{#if st === 'in_game' && f.presence?.packName} · {f.presence.packName}{/if}
                </span>
              </span>
              {#if multi && !off}
                <i class="ti {chosen.has(f.accountId) ? 'ti-circle-check-filled' : 'ti-circle'} fp-check"></i>
              {/if}
            </button>
          {/each}
        {/if}
      </div>

      {#if multi}
        <div class="fp-foot">
          <span class="fp-count">{max > 0 ? `Вибрано ${chosen.size} з ${max}` : ''}{max > 0 ? ' · ' : ''}{t('friendPicker.count', { n: chosen.size, online: onlineCount })}</span>
          {#if onback}<button class="btn btn-ghost" onclick={onback} disabled={busy}><i class="ti ti-arrow-left"></i> Назад</button>{/if}
          <button class="btn btn-ghost" onclick={close} disabled={busy}>{t('cancel')}</button>
          <button class="btn btn-primary" onclick={confirm} disabled={chosen.size === 0 || busy}>
            {#if busy}<i class="ti ti-loader-2 spin"></i>{:else}<i class="ti ti-check"></i>{/if} {confirmLabel || t('friendPicker.add')}
          </button>
        </div>
      {/if}
    </div>
  </div>
</Portal>

<style>
  .fp-overlay { position: fixed; inset: 0; z-index: 420; display: flex; align-items: center; justify-content: center; padding: 24px; background: rgba(0, 0, 0, .65); backdrop-filter: blur(6px); }
  .fp-modal {
    width: min(480px, 100%); max-height: min(640px, calc(100vh - 48px)); display: flex; flex-direction: column;
    background: var(--bg-panel); border: 1px solid var(--border-hi); border-radius: var(--r-lg, 16px); overflow: hidden;
    box-shadow: 0 30px 60px rgba(0, 0, 0, .7);
  }
  .fp-head { display: flex; align-items: center; gap: 11px; padding: 18px 20px 12px; border-bottom: 1px solid var(--border); margin-bottom: 8px; }
  .fp-ic { width: 38px; height: 38px; flex: none; border-radius: 10px; display: grid; place-items: center; font-size: 19px; background: rgba(var(--orange-rgb), .16); color: var(--orange); }
  .fp-ic :global(.ti) { display: block; line-height: 1; transform: translateY(1px); }
  .fp-tt { flex: 1; min-width: 0; }
  .fp-title { font: 800 15px/1.25 var(--font-ui); color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .fp-sub { font: 400 11.5px/1.35 var(--font-ui); color: var(--text-mute); margin-top: 2px; }
  .fp-x { width: 28px; height: 28px; border-radius: 8px; border: 0; background: transparent; color: var(--text-mute); cursor: pointer; }
  .fp-x:hover { background: var(--hover-strong); color: var(--text); }
  .fp-search { display: flex; align-items: center; gap: 8px; margin: 0 16px 8px; padding: 0 11px; border-radius: 10px; background: var(--bg-input); border: 1px solid var(--border); }
  .fp-search i { color: var(--text-dim); font-size: 14px; }
  .fp-search input { flex: 1; min-width: 0; padding: 9px 0; background: transparent; border: 0; outline: 0; color: var(--text); font: 500 12.5px/1 var(--font-ui); }
  .fp-list { flex: 1; min-height: 120px; overflow-y: auto; padding: 4px 8px 8px; display: flex; flex-direction: column; gap: 2px; }
  .fp-state { padding: 28px 12px; text-align: center; color: var(--text-mute); font: 500 12.5px/1.5 var(--font-ui); }
  .fp-row {
    display: flex; align-items: center; gap: 11px; padding: 8px 10px; border-radius: 11px; cursor: pointer; text-align: left;
    background: transparent; border: 1px solid transparent; color: var(--text); transition: background .12s, border-color .12s;
  }
  .fp-row:hover:not(:disabled) { background: var(--hover); }
  .fp-row.on { background: rgba(var(--orange-rgb), .09); border-color: rgba(var(--orange-rgb), .4); }
  .fp-row.off { opacity: .5; cursor: default; }
  .fp-row.full { opacity: .45; cursor: not-allowed; }
  .fp-x:disabled { opacity: .4; cursor: default; }
  .fp-main { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
  .fp-nick { font: 700 13px/1.2 var(--font-ui); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .fp-meta { font: 400 11px/1.3 var(--font-ui); color: var(--text-mute); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .fp-check { font-size: 18px; color: var(--text-dim); }
  .fp-row.on .fp-check { color: var(--orange); }
  .fp-foot { display: flex; align-items: center; gap: 8px; padding: 13px 20px; border-top: 1px solid var(--border); }
  .fp-count { flex: 1; font: 500 11.5px/1.3 var(--font-ui); color: var(--text-mute); }
</style>
