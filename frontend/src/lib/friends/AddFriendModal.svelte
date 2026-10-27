<script lang="ts">
  // ── Вікно «Додати цього друга?» ─────────────────────────────────────────
  // Відкривається, коли лаунчер побачив посилання-запрошення (у буфері
  // обміну або вставлене вручну). Показує іконку (голову скіна) та ім'я
  // акаунта, а кнопка надсилає запит у друзі через наявний SendFriendInvite.
  import Portal from '../Portal.svelte'
  import { App } from '../wails'
  import { toast } from '../toast.svelte'
  import { t } from '../i18n.svelte'
  import type { FriendLink } from './friendLink'

  let {
    link = null,
    onclose = () => {},
    onadded = () => {},
  }: {
    link?: FriendLink | null
    onclose?: () => void
    onadded?: (accountId: string) => void
  } = $props()

  let head = $state('')
  let checking = $state(true)
  let busy = $state(false)
  let already = $state(false)
  let self = $state(false)
  let loggedIn = $state(false)
  // Цей гравець уже надіслав НАМ запит (лишилось прийняти у списку друзів).
  let incoming = $state(false)
  // Показуємо, ЩО саме додаємо: handle (@bob_1) або початок UUID.
  const label = $derived(link ? (link.handle ? '@' + link.handle : link.accountId.slice(0, 8) + '…') : '')

  // Перевіряємо стан лише коли лінк відкрито (і скидаємо при закритті).
  $effect(() => {
    const l = link
    if (!l) return
    let cancelled = false
    checking = true
    already = false
    self = false
    incoming = false
    head = ''
    ;(async () => {
      try {
        // Знімок одразу після запуску лаунчера порожній (друзі ще не
        // завантажені) — тому спершу свіже оновлення, з запасним виходом на
        // кешований стан, якщо мережа повільна.
        let st: any
        try {
          st = await Promise.race([
            App.RefreshFriends(),
            new Promise<never>((_, rej) => setTimeout(() => rej(new Error('timeout')), 4000)),
          ])
        } catch {
          st = await App.GetFriendsState()
        }
        if (cancelled) return
        loggedIn = !!st?.loggedIn
        let prof: any = null
        try { prof = await App.GetShaurmaProfile() } catch { /* best-effort */ }
        const mineHandle = (prof?.handle ?? '').toLowerCase()
        const mineId = (st?.me || prof?.accountId || '').toLowerCase()
        const lh = l.handle.toLowerCase()
        const lid = l.accountId.toLowerCase()
        // «Це я»: за handle АБО за accountId — що є в посиланні.
        self = (!!lh && !!mineHandle && mineHandle === lh) || (!!lid && !!mineId && mineId === lid)
        already = !self && !!st?.friends?.some((f: any) =>
          (!!lh && (f.handle ?? '').toLowerCase() === lh) || (!!lid && (f.accountId ?? '').toLowerCase() === lid))
        incoming = !self && !already && !!st?.invites?.some((i: any) =>
          (!!lh && (i.fromHandle ?? '').toLowerCase() === lh) || (!!lid && (i.fromAccountId ?? '').toLowerCase() === lid))
      } catch {
        if (cancelled) return
        loggedIn = false
      }
      if (l.nickname) {
        try {
          const h = await App.GetAccountHead('', l.nickname, false)
          if (!cancelled) head = h || ''
        } catch { /* голова не обов'язкова */ }
      }
      if (!cancelled) checking = false
    })()
    return () => { cancelled = true }
  })

  async function add() {
    const l = link
    if (!l || busy) return
    busy = true
    try {
      if (l.handle) await App.SendFriendInviteByHandle(l.handle)
      else await App.SendFriendInvite(l.accountId)
      toast(t('friends.invite.sent'), 'success')
      onadded(l.accountId || l.handle)
      onclose()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      busy = false
    }
  }

  function copyId() {
    if (!link) return
    const id = link.handle ? '@' + link.handle : link.accountId
    App.CopyToClipboard(id).then(() => toast(t('social.idCopied'), 'success')).catch(() => {
      navigator.clipboard?.writeText(id).then(() => toast(t('social.idCopied'), 'success')).catch(() => {})
    })
  }
</script>

{#if link}
  <Portal>
    <div class="af-overlay" onclick={onclose} role="presentation">
      <div class="af-modal" onclick={(e: any) => e.stopPropagation()} role="dialog" aria-modal="true">
        <div class="af-head">
          <div class="af-ic"><i class="ti ti-user-plus"></i></div>
          <div class="af-title">{t('friends.invite.title')}</div>
        </div>

        <div class="af-body">
          <div class="af-ava">
            {#if head}
              <img src={head} alt="" />
            {:else}
              <span>{(link.nickname || '?').charAt(0).toUpperCase()}</span>
            {/if}
          </div>
          <div class="af-meta">
            <div class="af-nick">{link.nickname || t('friends.invite.unknown')}</div>
            {#if self}
              <span class="af-badge me"><i class="ti ti-user-check"></i> {t('friends.invite.badgeSelf')}</span>
            {:else if already}
              <span class="af-badge ok"><i class="ti ti-users"></i> {t('friends.invite.badgeFriend')}</span>
            {/if}
            <button class="af-id" onclick={copyId} title={t('social.copyId')}>
              <i class="ti ti-copy"></i> <code>{label}</code>
            </button>
          </div>
        </div>

        {#if self}
          <div class="af-note warn"><i class="ti ti-info-circle"></i> {t('friends.invite.self')}</div>
        {:else if already}
          <div class="af-note ok"><i class="ti ti-users"></i> {t('friends.invite.already')}</div>
        {:else if incoming}
          <div class="af-note warn"><i class="ti ti-mail"></i> {t('friends.invite.incoming')}</div>
        {:else if !loggedIn && !checking}
          <div class="af-note warn"><i class="ti ti-user-question"></i> {t('friends.invite.loginNeeded')}</div>
        {/if}
      </div>

      <div class="af-actions">
        <button class="btn btn-ghost" onclick={onclose}>{t('cancel')}</button>
        <button class="btn btn-primary" onclick={add}
                disabled={busy || checking || self || already || incoming || !loggedIn}>
          {#if busy}<i class="ti ti-loader-2 spin"></i>{:else}<i class="ti ti-user-plus"></i>{/if}
          {t('friends.invite.add')}
        </button>
      </div>
    </div>
  </Portal>
{/if}

<style>
  .af-overlay {
    position: fixed; inset: 0; z-index: 400;
    display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 14px;
    background: rgba(0, 0, 0, .55);
  }
  .af-modal {
    width: min(380px, 92vw);
    background: var(--bg-panel); border: 1px solid var(--border); border-radius: 16px;
    padding: 20px; box-shadow: 0 18px 60px rgba(0, 0, 0, .5);
  }
  .af-head { display: flex; align-items: center; gap: 10px; margin-bottom: 16px; }
  .af-ic {
    width: 34px; height: 34px; border-radius: 10px; display: grid; place-items: center;
    background: rgba(var(--orange-rgb), .12); color: var(--orange); font-size: 18px;
  }
  .af-title { font-size: 16px; font-weight: 700; color: var(--text); }
  .af-body { display: flex; align-items: center; gap: 14px; }
  .af-ava {
    width: 72px; height: 72px; border-radius: 14px; overflow: hidden; flex-shrink: 0;
    border: 2px solid rgba(var(--orange-rgb), .5); background: var(--bg);
    display: grid; place-items: center; font-size: 28px; font-weight: 700; color: var(--text-mute);
  }
  .af-ava img { width: 100%; height: 100%; object-fit: cover; image-rendering: pixelated; display: block; }
  .af-meta { min-width: 0; }
  .af-nick { font-size: 18px; font-weight: 700; color: var(--text); word-break: break-word; }
  .af-id {
    margin-top: 6px; display: inline-flex; align-items: center; gap: 6px;
    background: none; border: 1px solid var(--border); border-radius: 8px; padding: 3px 8px;
    color: var(--text-mute); font: inherit; font-size: 11px; cursor: pointer;
  }
  .af-id code { font-family: var(--font-mono); }
  .af-id:hover { border-color: rgba(var(--orange-rgb), .5); color: var(--text); }
  .af-badge {
    display: inline-flex; align-items: center; gap: 5px; margin-top: 4px; margin-right: 6px;
    font-size: 11px; font-weight: 600; border-radius: 999px; padding: 2px 9px;
  }
  .af-badge.ok { color: #2ecc71; background: rgba(46, 204, 113, .1); border: 1px solid rgba(46, 204, 113, .3); }
  .af-badge.me { color: var(--orange); background: rgba(var(--orange-rgb), .1); border: 1px solid rgba(var(--orange-rgb), .3); }
  .af-note {
    margin-top: 14px; display: flex; align-items: center; gap: 8px;
    font-size: 12px; line-height: 1.45; border-radius: 10px; padding: 9px 12px;
  }
  .af-note.warn { color: var(--text-mute); background: rgba(var(--orange-rgb), .08); border: 1px solid rgba(var(--orange-rgb), .22); }
  .af-note.warn i { color: var(--orange); }
  .af-note.ok { color: #2ecc71; background: rgba(46, 204, 113, .08); border: 1px solid rgba(46, 204, 113, .25); }
  .af-actions { width: min(380px, 92vw); display: flex; justify-content: flex-end; gap: 10px; }
</style>
