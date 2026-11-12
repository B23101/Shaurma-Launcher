<script lang="ts">
  // ProfileComments — коментарі під профілем (за мотивами Steam, але короткі й з жорсткими
  // лімітами): до 40 символів, до 3 коментарів від мене в одному профілі, під кожним — реакції.
  // ВСІ ЛІМІТИ ТРИМАЄ СЕРВЕР; тут вони лише підказки в інтерфейсі (лічильник, блокування кнопки).
  // Список вантажиться один раз при відкритті профілю й після власної відправки; реакції
  // оновлюються локально без повторного запиту, щоб не навантажувати слабкий сервер.
  import { fly } from 'svelte/transition'
  import { t } from '../i18n.svelte'
  import { App } from '../wails'
  import { toast } from '../toast.svelte'
  import { tip } from './tooltip'
  import { friendHead } from './friendHead'
  import FAvatar from './FAvatar.svelte'
  import type { SocialProfileComments } from '../types'

  let { profileId, isOwner = false, onopen = null, onopenself = null }: {
    profileId: string
    isOwner?: boolean
    /** Клік по моєму власному коментарю → відкрити «Мій акаунт» (свій профіль сервер як «чужий» не видає). */
    onopenself?: (() => void) | null
    /** Клік по автору коментаря → відкрити його профіль. null — автори не клікабельні. */
    onopen?: ((accountId: string, nickname: string) => void) | null
  } = $props()

  const MAX_LEN = 40
  const PER_AUTHOR = 3
  // Той самий набір, що й на сервері (символ-у-символ, включно з U+FE0F у серця).
  const EMOJI = ['👍', '❤️', '😂', '😮', '😢', '🔥']

  let data = $state<SocialProfileComments | null>(null)
  let status = $state<'loading' | 'ok' | 'error'>('loading')
  let errText = $state('')
  let draft = $state('')
  let sending = $state(false)
  let pickerFor = $state(0)
  let reloadTick = $state(0)

  function codeText(e: any): string {
    const m = String(e?.message ?? e ?? '')
    const key = `social.err.${m.replace(/^social:/, '')}`
    const txt = t(key)
    return txt !== key ? txt : t('social.err.unknown')
  }

  $effect(() => {
    const id = profileId
    void reloadTick
    if (!id) { data = null; status = 'loading'; return }
    let dead = false
    status = 'loading'
    App.GetProfileComments(id)
      .then((r) => { if (!dead) { data = r; status = 'ok' } })
      .catch((e: any) => { if (!dead) { errText = codeText(e); status = 'error' } })
    return () => { dead = true }
  })

  const used = $derived([...draft.trim().replace(/\s+/g, ' ')].length)
  const tooLong = $derived(used > MAX_LEN)
  const canSend = $derived(!isOwner && !!data?.canPost && used > 0 && !tooLong && !sending)

  async function send() {
    if (!canSend) return
    sending = true
    try {
      await App.PostProfileComment(profileId, draft)
      draft = ''
      reloadTick++
    } catch (e: any) {
      toast(codeText(e), 'error')
    } finally { sending = false }
  }

  async function remove(id: number) {
    try {
      await App.DeleteProfileComment(id)
      reloadTick++
    } catch (e: any) {
      toast(codeText(e), 'error')
      reloadTick++
    }
  }

  // Локальне оновлення реакції: прибираємо мою стару, додаємо нову (emoji порожній — лише знімаємо).
  function applyLocal(id: number, emoji: string) {
    if (!data) return
    data = {
      ...data,
      comments: data.comments.map((c) => {
        if (c.id !== id) return c
        const r = { ...(c.reactions ?? {}) }
        if (c.mine) {
          r[c.mine] = (r[c.mine] ?? 1) - 1
          if (r[c.mine] <= 0) delete r[c.mine]
        }
        if (emoji) r[emoji] = (r[emoji] ?? 0) + 1
        return { ...c, reactions: r, mine: emoji }
      }),
    }
  }

  async function react(id: number, emoji: string) {
    pickerFor = 0
    const cur = data?.comments.find((c) => c.id === id)
    if (!cur) return
    const next = cur.mine === emoji ? '' : emoji
    applyLocal(id, next)
    try {
      await App.ReactProfileComment(id, next)
    } catch (e: any) {
      toast(codeText(e), 'error')
      reloadTick++
    }
  }

  // Голови авторів: беремо з загального кешу friendHead (один запит на нік).
  let heads = $state<Record<string, string>>({})
  $effect(() => {
    for (const c of data?.comments ?? []) {
      const key = c.nickname.toLowerCase()
      if (!c.nickname || key in heads) continue
      heads[key] = ''
      friendHead(c.nickname).then((h) => { if (h) heads[key] = h })
    }
  })
  const headOf = (nick: string) => heads[nick.toLowerCase()] ?? ''

  // Автор клікабельний, якщо це не власник цього профілю і не я сам:
  // у чужому профілі canDelete=true лише для мого коментаря, у своєму — «автор = власник».
  const isMine = (c: { canDelete: boolean }) => !isOwner && c.canDelete
  const canOpen = (c: { authorId: string; canDelete: boolean }) =>
    !!c.authorId && c.authorId !== profileId && (isMine(c) ? !!onopenself : !!onopen)
  function openAuthor(c: { authorId: string; nickname: string; canDelete: boolean }) {
    if (isMine(c)) onopenself?.()
    else onopen?.(c.authorId, c.nickname)
  }

  function when(ts: number): string {
    try { return new Date(ts).toLocaleDateString(document.documentElement.lang || 'uk', { day: 'numeric', month: 'short' }) }
    catch { return '' }
  }
</script>

<div class="pc">
  <div class="pc-lbl"><i class="ti ti-message-2"></i> {t('profile.comments.title')}{#if data && data.comments.length} <span class="pc-n">{data.comments.length}</span>{/if}</div>

  {#if !isOwner}
    <div class="pc-form" class:off={!data?.canPost}>
      <div class="pc-field">
        <input class="pc-in" type="text" maxlength={MAX_LEN} placeholder={t('profile.comments.placeholder')} bind:value={draft}
               disabled={!data?.canPost || sending} onkeydown={(e) => { if (e.key === 'Enter') { e.preventDefault(); send() } }} />
        <span class="pc-cnt" class:bad={tooLong}>{used}/{MAX_LEN}</span>
      </div>
      <button class="pc-send" disabled={!canSend} onclick={send} aria-label={t('profile.comments.send')} use:tip={t('profile.comments.send')}>
        {#if sending}<i class="ti ti-loader-2 spin"></i>{:else}<i class="ti ti-send"></i>{/if}
      </button>
    </div>
    {#if status === 'ok' && data && !data.canPost}
      <small class="pc-hint">{t('profile.comments.limit', { n: data.myCount, max: PER_AUTHOR })}</small>
    {/if}
  {:else}
    <small class="pc-hint">{t('profile.comments.ownerHint')}</small>
  {/if}

  {#if status === 'loading' && !data}
    <div class="pc-skel"></div>
  {:else if status === 'error'}
    <div class="pc-empty">
      <span>{errText || t('profile.comments.loadFail')}</span>
      <button class="btn btn-ghost btn-sm" onclick={() => reloadTick++}><i class="ti ti-refresh"></i> {t('acc.modal.friend.retry')}</button>
    </div>
  {:else if data && data.comments.length === 0}
    <div class="pc-empty"><i class="ti ti-message-off"></i><span>{t('profile.comments.empty')}</span></div>
  {:else if data}
    <ul class="pc-list">
      {#each data.comments as c (c.id)}
        {@const open = canOpen(c)}
        <li class="pc-item" in:fly={{ y: 6, duration: 180 }}>
          {#if open}
            <button class="pc-ava as-btn" onclick={() => openAuthor(c)} use:tip={t('ff.profile.open')} aria-label={`${t('ff.profile.open')}: ${c.nickname}`}>
              <FAvatar head={headOf(c.nickname)} initial={(c.nickname.charAt(0) || '?').toUpperCase()} size={34} />
            </button>
          {:else}
            <span class="pc-ava"><FAvatar head={headOf(c.nickname)} initial={(c.nickname.charAt(0) || '?').toUpperCase()} size={34} /></span>
          {/if}

          <div class="pc-body">
            <div class="pc-head">
              {#if open}
                <button class="pc-nick as-btn" onclick={() => openAuthor(c)}>{c.nickname}</button>
              {:else}
                <b class="pc-nick">{c.nickname}</b>
              {/if}
              <span class="pc-date">{when(c.createdAt)}</span>
              {#if c.canDelete}
                <button class="pc-del" onclick={() => remove(c.id)} use:tip={t('profile.comments.delete')} aria-label={t('profile.comments.delete')}><i class="ti ti-trash"></i></button>
              {/if}
            </div>
            <div class="pc-text">{c.text}</div>
            <div class="pc-react">
              {#each Object.entries(c.reactions ?? {}) as [e, n] (e)}
                <button class="pc-chip" class:mine={c.mine === e} onclick={() => react(c.id, e)}>{e} <span>{n}</span></button>
              {/each}
              <button class="pc-chip plus" onclick={() => (pickerFor = pickerFor === c.id ? 0 : c.id)} use:tip={t('profile.comments.react')} aria-label={t('profile.comments.react')}><i class="ti ti-mood-plus"></i></button>
              {#if pickerFor === c.id}
                {#each EMOJI as e (e)}
                  <button class="pc-chip pick" class:mine={c.mine === e} onclick={() => react(c.id, e)} in:fly={{ x: -4, duration: 120 }}>{e}</button>
                {/each}
              {/if}
            </div>
          </div>
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  .pc { display: grid; gap: 10px; }
  /* Той самий підпис, що й «Про себе» у профілі: без капсу. */
  .pc-lbl { display: inline-flex; align-items: center; gap: 6px; font: 700 12px/1 var(--font-ui); color: var(--text-dim); }
  .pc-n { padding: 2px 7px; border-radius: 999px; background: var(--hover); color: var(--text-dim); font: 650 11px/1 var(--font-ui); }

  .pc-form { display: flex; align-items: stretch; gap: 8px; }
  .pc-field { position: relative; flex: 1; min-width: 0; }
  .pc-in { width: 100%; box-sizing: border-box; height: 40px; padding: 0 58px 0 13px; border-radius: 12px; border: 1px solid var(--border); background: var(--bg-input); color: var(--text); font: 500 13.5px/1.2 var(--font-ui); transition: border-color .15s, box-shadow .15s; }
  .pc-in:focus { outline: none; border-color: var(--aura, var(--border-hi)); box-shadow: 0 0 0 3px color-mix(in srgb, var(--aura, #ff7a1a) 20%, transparent); }
  .pc-in:disabled { opacity: .55; }
  .pc-cnt { position: absolute; right: 12px; top: 50%; transform: translateY(-50%); pointer-events: none; font: 600 var(--fs-min)/1 var(--font-ui); color: var(--text-mute); font-variant-numeric: tabular-nums; }
  .pc-cnt.bad { color: var(--danger, #ef4444); }
  .pc-send { flex: none; width: 40px; height: 40px; display: grid; place-items: center; border: 0; border-radius: 12px; cursor: pointer; color: #111; background: var(--aura, var(--orange)); font-size: 17px; transition: filter .14s, transform .12s, opacity .14s; }
  .pc-send:hover:not(:disabled) { filter: brightness(1.1); }
  .pc-send:active:not(:disabled) { transform: scale(.94); }
  .pc-send:disabled { opacity: .35; cursor: default; }
  .pc-send:focus-visible { outline: 2px solid var(--aura, var(--orange)); outline-offset: 2px; }
  .pc-hint { color: var(--text-mute); font: 500 var(--fs-min)/1.35 var(--font-ui); }
  .pc-skel { height: 56px; border-radius: 12px; background: var(--hover); opacity: .6; }
  .pc-empty { display: flex; align-items: center; justify-content: center; gap: 8px; padding: 16px 12px; border-radius: 12px; border: 1px dashed var(--border); color: var(--text-mute); font: 500 13px/1.3 var(--font-ui); }
  .pc-empty i { font-size: 18px; }

  .pc-list { list-style: none; margin: 0; padding: 0 2px 0 0; display: grid; gap: 8px; max-height: 340px; overflow-y: auto; }
  .pc-item { display: grid; grid-template-columns: 34px minmax(0, 1fr); gap: 11px; align-items: start; padding: 11px 12px; border-radius: 14px; border: 1px solid var(--border); background: var(--bg-input); transition: border-color .15s; }
  .pc-item:hover { border-color: var(--border-hi); }
  .pc-ava { display: block; width: 34px; height: 34px; }
  .as-btn { border: 0; background: none; padding: 0; cursor: pointer; color: inherit; font: inherit; text-align: left; }
  .pc-ava.as-btn { border-radius: 12px; transition: transform .14s; }
  .pc-ava.as-btn:hover { transform: scale(1.06); }
  .pc-ava.as-btn:focus-visible, .pc-nick.as-btn:focus-visible { outline: 2px solid var(--aura, var(--orange)); outline-offset: 2px; border-radius: 8px; }
  .pc-body { display: grid; gap: 4px; min-width: 0; }
  .pc-head { display: flex; align-items: center; gap: 8px; min-width: 0; min-height: 20px; }
  .pc-nick { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font: 750 13.5px/1.2 var(--font-ui); color: var(--text); }
  .pc-nick.as-btn:hover { color: var(--aura, var(--orange)); text-decoration: underline; text-underline-offset: 3px; }
  .pc-date { flex: none; margin-left: auto; color: var(--text-mute); font: 500 var(--fs-min)/1 var(--font-ui); }
  .pc-del { flex: none; border: 0; background: none; color: var(--text-mute); cursor: pointer; padding: 3px; border-radius: 7px; display: grid; place-items: center; }
  .pc-del:hover { color: var(--danger, #ef4444); background: var(--hover); }
  .pc-text { font: 500 13.5px/1.4 var(--font-ui); color: var(--text); overflow-wrap: anywhere; }
  .pc-react { display: flex; flex-wrap: wrap; align-items: center; gap: 5px; margin-top: 3px; }
  .pc-chip { display: inline-flex; align-items: center; gap: 4px; padding: 3px 8px; border-radius: 999px; border: 1px solid var(--border); background: transparent; color: var(--text-dim); cursor: pointer; font: 600 12px/1 var(--font-ui); transition: background .14s, border-color .14s; }
  .pc-chip:hover { background: var(--hover); }
  .pc-chip.mine { border-color: var(--aura, var(--border-hi)); background: color-mix(in srgb, var(--aura, #ff7a1a) 16%, transparent); color: var(--text); }
  .pc-chip.plus { padding: 3px 7px; color: var(--text-mute); }
</style>
