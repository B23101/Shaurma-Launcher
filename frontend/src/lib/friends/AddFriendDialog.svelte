<script lang="ts">
  // ── «Додати друга» ──────────────────────────────────────────────────────
  // Модалка замість панелі, що розсувала список. Три способи: підказка за
  // ніком / @handle, точний @handle або ID, посилання-запрошення (вставлене).
  // Нік не унікальний — додати «наосліп» за ним не можна, лише вибрати зі
  // списку підказок. Область результатів має сталу висоту: вікно не стрибає.
  import { useFriendsStore, clean, initialOf } from './FriendsStore.svelte'
  import { App } from '../wails'
  import { toast } from '../toast.svelte'
  import { t } from '../i18n.svelte'
  import { parseFriendLink } from './friendLink'
  import FModal from './FModal.svelte'
  import FAvatar from './FAvatar.svelte'
  import type { SocialUserHit } from '../types'

  let { open = false, onclose }: { open?: boolean; onclose: () => void } = $props()
  const s = useFriendsStore()

  let input = $state('')
  let adding = $state(false)
  let hits = $state<SocialUserHit[]>([])
  let searching = $state(false)
  let searched = $state('')
  let inputEl = $state<HTMLInputElement | null>(null)

  const target = $derived(s.parseTarget(input))
  const query = $derived(input.trim().replace(/^@/, ''))

  $effect(() => { if (open) void s.loadMyHandle() })
  $effect(() => {
    if (open) setTimeout(() => inputEl?.focus({ preventScroll: true }), 60)
    else { input = ''; hits = []; searched = ''; searching = false }
  })

  // Підказки (префіксний пошук) з debounce; застарілі відповіді відкидаємо.
  $effect(() => {
    const q = query
    if (!open || q.length < 2 || parseFriendLink(input) || target?.kind === 'id') { hits = []; searched = ''; searching = false; return }
    searching = true
    let dead = false
    const timer = setTimeout(async () => {
      try {
        const r = (await App.SearchFriendCandidates(q)) ?? []
        if (!dead) { hits = r; searched = q }
      } catch { if (!dead) { hits = []; searched = q } }
      finally { if (!dead) searching = false }
    }, 280)
    return () => { dead = true; clearTimeout(timer) }
  })

  async function submit() {
    const tg = target
    if (!tg || adding) return
    adding = true
    try { await s.inviteTarget(tg); onclose() }
    catch (e) { toast(s.errText(e), 'error') }
    finally { adding = false }
  }
  async function pick(h: SocialUserHit) {
    if (adding) return
    adding = true
    try { await s.inviteHit(h); onclose() }
    catch (e) { toast(s.errText(e), 'error') }
    finally { adding = false }
  }
</script>

<FModal {open} {onclose} icon="ti-user-plus" title={t('friends.addFriend')} subtitle={t('ff.add.sub')} width={480}>
  {#if s.me}
    <div class="ad-me">
      <div class="ad-me-l">
        <span class="ad-lab">{t('social.myId')}</span>
        <code class="ad-code">{s.myIdLabel}</code>
      </div>
      <button class="ad-btn" onclick={() => s.copyMyId()}><i class="ti ti-copy"></i><span>{t('ff.add.copy')}</span></button>
      <button class="ad-btn" onclick={() => s.copyInviteLink()}><i class="ti ti-link"></i><span>{t('ff.add.link')}</span></button>
    </div>
  {/if}

  <label class="ad-lab ad-lab-in" for="ad-input">{t('ff.add.label')}</label>
  <div class="ad-row">
    <div class="ad-field">
      <i class="ti ti-at"></i>
      <input id="ad-input" bind:this={inputEl} bind:value={input} maxlength="160" spellcheck="false" autocomplete="off"
             placeholder={t('social.addPlaceholder')} onkeydown={(e) => { if (e.key === 'Enter') submit() }} />
      {#if searching}<i class="ti ti-loader-2 ff-spin ad-ld"></i>{/if}
    </div>
    <button class="btn btn-primary btn-sm ad-send" onclick={submit} disabled={!target || adding}>
      {#if adding}<i class="ti ti-loader-2 ff-spin"></i>{:else}<i class="ti ti-send"></i>{/if}
      {t('social.add')}
    </button>
  </div>

  <div class="ad-res" aria-live="polite">
    {#if hits.length > 0}
      {#each hits as h (h.accountId)}
        <button class="ad-hit" onclick={() => pick(h)} disabled={adding}>
          <FAvatar head={s.heads[h.nickname] ?? ''} initial={initialOf(h.nickname)} size={34} />
          <span class="ad-hit-m">
            <span class="ad-hit-n">{clean(h.nickname)}</span>
            {#if h.handle}<span class="ad-hit-h">@{h.handle}</span>{/if}
          </span>
          <span class="ad-hit-go"><i class="ti ti-user-plus"></i></span>
        </button>
      {/each}
    {:else if searched && searched === query && !searching}
      <div class="ad-note"><i class="ti ti-user-search"></i>{t('social.suggest.none')}</div>
    {:else}
      <div class="ad-note"><i class="ti ti-bulb"></i>{t('ff.add.hint')}</div>
    {/if}
  </div>
</FModal>

<style>
  .ad-me { display: flex; align-items: center; gap: 8px; padding: 12px 12px 12px 16px; margin-bottom: 20px; border-radius: 16px; background: var(--bg-input); border: 1px solid var(--border); }
  .ad-me-l { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 4px; }
  .ad-lab { font: 700 10.5px/1 var(--font-ui); letter-spacing: .3px; color: var(--text-dim); }
  .ad-lab-in { display: block; margin-bottom: 7px; }
  .ad-code { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font: 600 13px/1.3 var(--font-mono); color: var(--text); user-select: all; }
  .ad-btn { flex: 0 0 auto; display: inline-flex; align-items: center; gap: 7px; height: 34px; padding: 0 12px; border-radius: 11px; cursor: pointer; font: 700 12px/1 var(--font-ui); color: var(--text); background: var(--bg-panel); border: 1px solid var(--border-hi); transition: border-color .14s, color .14s, transform .12s; }
  .ad-btn:hover { border-color: var(--orange); color: var(--orange); }
  .ad-btn:active { transform: scale(.95); }
  .ad-btn:focus-visible { outline: 2px solid var(--orange); outline-offset: 2px; }

  .ad-row { display: flex; gap: 8px; }
  .ad-field { flex: 1; min-width: 0; display: flex; align-items: center; gap: 8px; height: 42px; padding: 0 14px; box-sizing: border-box; border-radius: 14px; background: var(--bg-input); border: 1px solid var(--border); transition: border-color .15s, box-shadow .15s; }
  .ad-field:focus-within { border-color: color-mix(in srgb, var(--orange) 60%, transparent); box-shadow: 0 0 0 3px rgba(var(--orange-rgb), .12); }
  .ad-field > i { color: var(--text-dim); font-size: 15px; flex: 0 0 auto; }
  .ad-field input { flex: 1; min-width: 0; height: 100%; background: none; border: 0; outline: none; color: var(--text); font: 500 13px/1 var(--font-ui); }
  .ad-ld { color: var(--orange) !important; }
  .ad-send { flex: 0 0 auto; display: inline-flex; align-items: center; gap: 6px; height: 42px; padding: 0 18px; border-radius: 14px; }

  /* Стала висота: результати з'являються без зсуву вікна. */
  .ad-res { display: flex; flex-direction: column; gap: 2px; margin-top: 12px; min-height: 176px; max-height: 232px; overflow-y: auto; }
  .ad-hit { display: flex; align-items: center; gap: 12px; flex: 0 0 auto; height: 50px; padding: 0 10px; box-sizing: border-box; border: 0; border-radius: 12px; background: transparent; color: var(--text); text-align: left; cursor: pointer; transition: background-color .12s; animation: ad-in .2s var(--ff-ease) both; }
  .ad-hit:hover:not(:disabled), .ad-hit:focus-visible { background: var(--hover); outline: none; }
  .ad-hit:disabled { opacity: .55; cursor: default; }
  .ad-hit-m { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 3px; }
  .ad-hit-n { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font: 700 13px/1.2 var(--font-ui); }
  .ad-hit-h { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font: 500 11px/1.2 var(--font-mono); color: var(--text-mute); }
  .ad-hit-go { flex: 0 0 auto; width: 30px; height: 30px; display: grid; place-items: center; border-radius: 9px; color: var(--orange); font-size: 16px; background: rgba(var(--orange-rgb), .12); transition: transform .14s; }
  .ad-hit:hover:not(:disabled) .ad-hit-go { transform: scale(1.08); }
  .ad-note { display: flex; align-items: flex-start; gap: 10px; margin: auto 0; padding: 14px; border-radius: 12px; font: 400 12px/1.5 var(--font-ui); color: var(--text-dim); }
  .ad-note i { flex: 0 0 auto; font-size: 18px; color: var(--orange); }
  @keyframes ad-in { from { opacity: 0; transform: translateY(4px); } to { opacity: 1; transform: none; } }
  @media (max-width: 520px) { .ad-btn span { display: none; } .ad-btn { padding: 0 9px; } }
  @media (prefers-reduced-motion: reduce) { .ad-hit { animation: none; } }
</style>
