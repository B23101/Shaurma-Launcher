<script lang="ts">
  // Кнопка «Поділитися» на сторінці збірки/мода в Браузері контенту.
  // Відкриває СПІЛЬНЕ вікно вибору друзів (FriendPickerModal) у режимі
  // «поділитися»: можна вибрати кількох друзів одразу, а не тицяти по
  // рядку у власному випадаючому меню. Кожному йде звичайна картка мода
  // в чат (modShare.ts), тож працює шифрування й клієнти старіших версій.
  import { App } from '../wails'
  import { toast } from '../toast.svelte'
  import { t } from '../i18n.svelte'
  import { tip } from './tooltip'
  import { buildModMessage } from './modShare'
  import FriendPickerModal from './FriendPickerModal.svelte'
  import type { BrowserProjectDetail, SocialFriend } from '../types'

  let { detail, onGoFriends }: {
    detail: BrowserProjectDetail
    // Перейти в розділ «Друзі» (якщо ще не вхід у соціальну систему).
    onGoFriends?: () => void
  } = $props()

  let pickerOpen = $state(false)
  let sending = $state(false)
  // Список беремо з того самого GetFriendsState, яким перевіряємо вхід, і
  // передаємо у пікер: він не робить другий запит при відкритті.
  let friends = $state<SocialFriend[]>([])

  // eslint-disable-next-line no-control-regex
  const HIDDEN = /[\u0000-\u001f\u007f-\u009f\u200b-\u200f\u202a-\u202e\u2066-\u2069\ufeff]/g
  const clean = (s?: string) => (s ?? '').replace(HIDDEN, '').trim()

  async function openPicker(e: MouseEvent) {
    e.stopPropagation()
    try {
      const st = await App.GetFriendsState()
      if (!st?.loggedIn) {
        toast(t('social.mod.needLogin'), 'error')
        onGoFriends?.()
        return
      }
      friends = Array.isArray(st.friends) ? st.friends : []
    } catch {
      onGoFriends?.()
      return
    }
    pickerOpen = true
  }

  async function shareWith(friends: SocialFriend[]) {
    if (sending || friends.length === 0) return
    sending = true
    const text = buildModMessage(detail)
    let ok = 0
    for (const f of friends) {
      try {
        await App.SendChatMessage(f.accountId, text)
        ok++
      } catch (err) {
        const m = /social:([A-Za-z_ ]+)/.exec(String((err as any)?.message ?? err ?? ''))
        const key = m ? `social.err.${m[1].trim().replace(/ /g, '_')}` : ''
        const txt = key ? t(key) : ''
        toast(txt && txt !== key ? txt : t('social.err.unknown'), 'error')
      }
    }
    sending = false
    pickerOpen = false
    if (ok > 0) {
      toast(ok === 1
        ? t('social.mod.sentTo', { name: clean(friends[0]?.nickname) })
        : t('social.mod.sentCount', { n: ok }), 'success')
    }
  }
</script>

<div class="sh-wrap">
  <button class="sh-btn" onclick={openPicker} disabled={sending} use:tip={t('social.pack.shareTip')}
          aria-label={t('social.pack.shareTip')} aria-haspopup="dialog">
    {#if sending}<i class="ti ti-loader-2 spin"></i>{:else}<i class="ti ti-share-3"></i>{/if}
  </button>
</div>

{#if pickerOpen}
  <FriendPickerModal
    title={t('social.pack.sharePick')}
    subtitle={detail.name || ''}
    confirmLabel={sending ? t('social.pack.sharing') : t('social.pack.shareAction')}
    {friends}
    onconfirm={shareWith}
    onclose={() => { if (!sending) pickerOpen = false }} />
{/if}

<style>
  .sh-wrap { position: relative; display: inline-flex; }
  .sh-btn { width: 34px; height: 34px; display: inline-flex; align-items: center; justify-content: center; border-radius: 10px; border: 1px solid var(--border-hi); background: var(--bg-input); color: var(--text-mute); font-size: 16px; cursor: pointer; transition: color .14s, border-color .14s, background .14s, transform .12s; }
  .sh-btn:hover:not(:disabled) { color: var(--orange); border-color: rgba(var(--orange-rgb), .55); background: rgba(var(--orange-rgb), .1); }
  .sh-btn:active:not(:disabled) { transform: scale(.92); }
  .sh-btn:disabled { opacity: .65; cursor: default; }
</style>
