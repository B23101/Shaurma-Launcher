<script lang="ts">
  // ── Чат групи (права панель) ─────────────────────────────────────────────
  // Те саме місце, що й діалог з другом (клас .cs — сітка ShaurmaFriends). Колір обведення — з
  // налаштувань групи (перевіряється регуляркою). Увесь текст показується як ТЕКСТ (жодного
  // {@html}); картки збірок/модів будуються з очищених сервером полів. Права кнопок (видалити,
  // закріпити…) — лише підказка UI: кожну дію ще раз перевіряє сервер.
  import { tick } from 'svelte'
  import { t } from '../i18n.svelte'
  import { confirmDialog } from '../toast.svelte'
  import { useGroupsStore } from './GroupsStore.svelte'
  import { parseModMessage } from '../friends/modShare'
  import { fmtBytes } from '../friends/shareMsg'
  import ModCard from '../friends/ModCard.svelte'
  import GroupSettingsModal from './GroupSettingsModal.svelte'
  import GroupAvatar from './GroupAvatar.svelte'
  import {
    GROUP_EMOJI, GROUP_MAX_BODY, parseOffer, parseSystem, safeGroupColor,
    type Group, type GroupMessage,
  } from './groupTypes'

  let { group, onBack, onOpenMod, allFriends = [] }: {
    group: Group
    onBack: () => void
    onOpenMod?: (source: string, projectId: string) => void
    allFriends?: import('../types').SocialFriend[]
  } = $props()
  const g = useGroupsStore()

  const msgs = $derived(g.msgs[group.id] ?? [])
  const pins = $derived(g.pinned[group.id] ?? [])
  const color = $derived(safeGroupColor(group.settings.color))
  const typingNames = $derived(g.typingNames(group.id))
  const byId = $derived(new Map(msgs.map((m) => [m.id, m])))

  let draft = $state('')
  let replyTo = $state<GroupMessage | null>(null)
  let editing = $state<GroupMessage | null>(null)
  let settingsOpen = $state(false)
  let emojiFor = $state(0)
  let box: HTMLDivElement | undefined = $state()
  let stick = true

  // Автоскрол донизу, поки користувач не відкрутив угору.
  $effect(() => {
    void msgs.length
    if (stick) tick().then(() => { if (box) box.scrollTop = box.scrollHeight })
  })
  function onScroll() {
    if (!box) return
    stick = box.scrollHeight - box.scrollTop - box.clientHeight < 80
    if (box.scrollTop < 40 && g.hasMore[group.id] && !g.histLoading[group.id]) {
      const prev = box.scrollHeight
      g.loadHistory(group.id).then(() => tick().then(() => { if (box) box.scrollTop = box.scrollHeight - prev }))
    }
  }

  const time = (ts: number) => new Date(ts).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  const day = (ts: number) => new Date(ts).toLocaleDateString()
  const newDay = (i: number) => i === 0 || day(msgs[i].ts) !== day(msgs[i - 1].ts)

  async function submit() {
    const text = draft.trim()
    if (!text) return
    const ok = editing ? await g.edit(group.id, editing.id, text) : await g.send(group.id, text, replyTo?.id ?? 0)
    if (ok) { draft = ''; replyTo = null; editing = null; stick = true }
  }
  function onKey(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) { e.preventDefault(); void submit() }
    else if (e.key === 'Escape') cancelCtx()
  }
  function cancelCtx() { if (editing) draft = ''; replyTo = null; editing = null }
  function onInput() { if (draft) g.sendTyping(group.id) }
  function startEdit(m: GroupMessage) { editing = m; replyTo = null; draft = m.body }

  async function remove(m: GroupMessage) {
    const ok = await confirmDialog(t('groups.msg.deleteTitle'), t('groups.msg.deleteText'), { confirmLabel: t('groups.msg.delete'), cancelLabel: t('share.cancel') })
    if (ok) void g.deleteMessage(group.id, m.id)
  }

  /** Реакції згруповано за емодзі; «моя» підсвічена. */
  function reactionGroups(m: GroupMessage) {
    const map = new Map<string, { n: number; mine: boolean }>()
    for (const r of m.reactions ?? []) {
      const e = map.get(r.emoji) ?? { n: 0, mine: false }
      e.n++; if (r.by === g.me) e.mine = true
      map.set(r.emoji, e)
    }
    return [...map.entries()]
  }
  function react(m: GroupMessage, emoji: string) {
    emojiFor = 0
    const mine = (m.reactions ?? []).some((r) => r.by === g.me && r.emoji === emoji)
    void g.react(group.id, m.id, mine ? '' : emoji)
  }

  function sysText(m: GroupMessage): string {
    const e = parseSystem(m.body)
    if (!e) return ''
    const a = e.a ? g.nameOf(e.a, group) : ''
    const by = e.by ? g.nameOf(e.by, group) : ''
    const key = `groups.sys.${e.e}`
    const txt = t(key)
    return txt === key ? '' : t(key, { a, by })
  }

  const meta = (o: { m?: Record<string, string> }) => [o.m?.mc, o.m?.loader && o.m.loader !== 'vanilla' ? o.m.loader : '', o.m?.core].filter(Boolean).join(' · ')
  const preview = (m?: GroupMessage) => (!m ? t('groups.msg.gone') : m.kind === 'build_offer' ? (parseOffer(m.body)?.n ?? '') : m.body).slice(0, 90)
  const isMine = (m: GroupMessage) => m.from === g.me
</script>

<section class="cs gchat" style={color ? `--gc:${color}` : ''} class:colored={!!color} aria-live="polite">
  <header class="gh">
    <button class="gh-b gh-back" onclick={onBack} aria-label={t('groups.back')}><i class="ti ti-chevron-left"></i></button>
    <GroupAvatar title={group.title} icon={group.settings.icon} color={group.settings.color} size={38} />
    <div class="gh-t">
      <b>{group.title}</b>
      <small>{t('groups.members', { n: group.members.length })} · {t('groups.admin', { name: g.nameOf(group.admin, group) })}</small>
    </div>
    <button class="gh-b" onclick={() => (settingsOpen = true)} aria-label={t('groups.settings.open')} title={t('groups.settings.open')}><i class="ti ti-settings"></i></button>
  </header>

  {#if pins.length > 0}
    <div class="gp" role="region" aria-label={t('groups.pinned')}>
      <i class="ti ti-pin"></i>
      <div class="gp-l">
        {#each pins as p (p.id)}
          <div class="gp-i">
            <span>{preview(p)}</span>
            {#if g.canPin(group, false)}<button class="gp-x" onclick={() => g.pin(group.id, p.id, false)} aria-label={t('groups.msg.unpin')}><i class="ti ti-pinned-off"></i></button>{/if}
          </div>
        {/each}
      </div>
    </div>
  {/if}

  <div class="gm" bind:this={box} onscroll={onScroll}>
    {#if g.hasMore[group.id]}<div class="gm-more">{g.histLoading[group.id] ? '…' : t('groups.history.more')}</div>{/if}
    {#each msgs as m, i (m.id)}
      {#if newDay(i)}<div class="gm-day">{day(m.ts)}</div>{/if}
      {#if m.kind === 'system'}
        <div class="gm-sys">{sysText(m)}</div>
      {:else}
        {@const mod = m.kind === 'text' ? parseModMessage(m.body) : null}
        {@const offer = m.kind === 'build_offer' ? parseOffer(m.body) : null}
        <div class="gm-row" class:own={isMine(m)}>
          <div class="gm-bub">
            {#if !isMine(m)}<div class="gm-nm">{g.nameOf(m.from, group)}</div>{/if}
            {#if m.replyToId}
              <div class="gm-rep"><i class="ti ti-corner-up-left"></i> {preview(byId.get(m.replyToId))}</div>
            {/if}
            {#if offer}
              <div class="gm-off">
                <i class="ti {offer.k === 'server' ? 'ti-server-2' : 'ti-package'} gm-oi"></i>
                <div class="gm-ot">
                  <b>{offer.n}</b>
                  <small>{meta(offer)}</small>
                  <small>{fmtBytes(offer.b)} · {offer.f} {t('share.stat.files').toLowerCase()}</small>
                </div>
                {#if !isMine(m)}
                  <button class="btn btn-primary btn-sm" onclick={() => g.claimBuild(m)}><i class="ti ti-download"></i> {t('groups.share.get')}</button>
                {:else}
                  <small class="gm-own">{t('groups.share.yours')}</small>
                {/if}
              </div>
            {:else if mod}
              {#if mod.comment}<div class="gm-tx">{mod.comment}</div>{/if}
              <ModCard ref={mod.ref} onOpenInLauncher={onOpenMod} />
            {:else}
              <div class="gm-tx">{m.body}</div>
            {/if}
            <div class="gm-ft">
              <span>{time(m.ts)}{m.editedAt ? ' · ' + t('groups.msg.edited') : ''}</span>
            </div>
            {#if (m.reactions?.length ?? 0) > 0}
              <div class="gm-rx">
                {#each reactionGroups(m) as [emoji, r] (emoji)}
                  <button class="gm-rb" class:mine={r.mine} onclick={() => react(m, emoji)}>{emoji} {r.n}</button>
                {/each}
              </div>
            {/if}
          </div>
          <div class="gm-act">
            <button onclick={() => (emojiFor = emojiFor === m.id ? 0 : m.id)} aria-label={t('groups.msg.react')}><i class="ti ti-mood-smile"></i></button>
            <button onclick={() => { replyTo = m; editing = null }} aria-label={t('groups.msg.reply')}><i class="ti ti-corner-up-left"></i></button>
            {#if g.canEdit(m)}<button onclick={() => startEdit(m)} aria-label={t('groups.msg.edit')}><i class="ti ti-pencil"></i></button>{/if}
            {#if g.canPin(group, true) && !pins.some((p) => p.id === m.id)}<button onclick={() => g.pin(group.id, m.id, true)} aria-label={t('groups.msg.pin')}><i class="ti ti-pin"></i></button>{/if}
            {#if g.canDelete(group, m)}<button class="danger" onclick={() => remove(m)} aria-label={t('groups.msg.delete')}><i class="ti ti-trash"></i></button>{/if}
            {#if emojiFor === m.id}
              <div class="gm-em">{#each GROUP_EMOJI as e (e)}<button onclick={() => react(m, e)}>{e}</button>{/each}</div>
            {/if}
          </div>
        </div>
      {/if}
    {:else}
      <div class="gm-empty">{t('groups.chat.empty')}</div>
    {/each}
  </div>

  <div class="gt">{#if typingNames.length > 0}{t('groups.typing', { names: typingNames.join(', ') })}{/if}</div>

  <footer class="gf">
    {#if replyTo || editing}
      <div class="gf-ctx">
        <i class="ti {editing ? 'ti-pencil' : 'ti-corner-up-left'}"></i>
        <span>{editing ? t('groups.msg.editing') : t('groups.msg.replying', { name: g.nameOf(replyTo?.from ?? '', group) })}</span>
        <button onclick={cancelCtx} aria-label={t('share.cancel')}><i class="ti ti-x"></i></button>
      </div>
    {/if}
    <div class="gf-row">
      <textarea bind:value={draft} onkeydown={onKey} oninput={onInput} rows="1" maxlength={GROUP_MAX_BODY}
                placeholder={t('groups.chat.placeholder')} aria-label={t('groups.chat.placeholder')}></textarea>
      <button class="gf-send" class:ready={draft.trim().length > 0} disabled={!draft.trim() || g.busy} onclick={submit} aria-label={t('groups.chat.send')}>
        <i class="ti ti-send-2"></i>
      </button>
    </div>
  </footer>
</section>

{#if settingsOpen}
  <GroupSettingsModal {group} friends={allFriends} onclose={() => (settingsOpen = false)} />
{/if}

<style>
  .cs { position: relative; display: flex; flex-direction: column; min-width: 0; min-height: 0; overflow: hidden;
    background: var(--bg-card); border: 1px solid var(--border); border-radius: var(--r-lg, 16px); }
  /* Колір обведення групи — з налаштувань (лише перевірений #RRGGBB). */
  .cs.colored { border-color: var(--gc); box-shadow: 0 0 0 1px var(--gc), 0 0 22px -6px var(--gc); }

  .gh { display: flex; align-items: center; gap: 12px; padding: 12px 16px; border-bottom: 1px solid var(--border); }
  .gh-t { min-width: 0; flex: 1; display: flex; flex-direction: column; gap: 2px; }
  .gh-t b { font-size: 14.5px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .gh-t small { font-size: 12px; color: var(--text-mute); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .gh-b { width: 32px; height: 32px; display: grid; place-items: center; border-radius: var(--r-sm, 8px); border: 1px solid var(--border-hi); background: var(--bg-input); color: var(--text-mute); cursor: pointer; font-size: 15px; }
  .gh-b:hover { color: var(--orange); border-color: rgba(var(--orange-rgb), .5); }
  .gh-back { display: none; }
  @container sf (max-width: 720px) { .gh-back { display: grid; } }

  .gp { display: flex; gap: 8px; align-items: flex-start; padding: 6px 16px; border-bottom: 1px solid var(--border); background: color-mix(in srgb, var(--bg-card) 92%, var(--orange) 8%); font-size: 12.5px; }
  .gp > i { color: var(--orange); margin-top: 2px; }
  .gp-l { min-width: 0; flex: 1; display: flex; flex-direction: column; gap: 2px; max-height: 64px; overflow-y: auto; }
  .gp-i { display: flex; gap: 6px; align-items: center; }
  .gp-i span { min-width: 0; flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .gp-x { border: none; background: none; color: var(--text-mute); cursor: pointer; }

  .gm { flex: 1 1 0; min-height: 0; overflow-y: auto; padding: 16px 18px 8px; display: flex; flex-direction: column; gap: 6px; }
  .gm-more, .gm-empty { text-align: center; font-size: 12px; color: var(--text-mute); padding: 6px; }
  .gm-empty { margin: auto; }
  .gm-day { align-self: center; padding: 3px 12px; font-size: 11px; border-radius: 999px; background: var(--bg-input); color: var(--text-mute); }
  .gm-sys { align-self: center; max-width: 90%; text-align: center; font-size: 11.5px; color: var(--text-mute); }
  .gm-row { display: flex; align-items: flex-start; gap: 6px; max-width: 100%; position: relative; }
  .gm-row.own { flex-direction: row-reverse; }
  .gm-bub { min-width: 0; max-width: min(78%, 560px); padding: 8px 12px 6px; border-radius: 16px; background: var(--bg-input); border: 1px solid var(--border); }
  .gm-row.own .gm-bub { background: rgba(var(--orange-rgb), .14); border-color: rgba(var(--orange-rgb), .4); }
  .gm-nm { font-size: 11.5px; font-weight: 700; color: var(--orange); margin-bottom: 2px; }
  .gm-tx { font-size: 13.5px; line-height: 1.45; white-space: pre-wrap; overflow-wrap: anywhere; }
  .gm-rep { font-size: 11.5px; color: var(--text-mute); border-left: 2px solid var(--orange); padding-left: 6px; margin-bottom: 4px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .gm-ft { margin-top: 3px; font-size: 10.5px; color: var(--text-mute); text-align: right; }
  .gm-rx { display: flex; flex-wrap: wrap; gap: 4px; margin-top: 4px; }
  .gm-rb { padding: 1px 8px; font-size: 12px; border-radius: 999px; border: 1px solid var(--border-hi); background: var(--bg-card); color: var(--text); cursor: pointer; }
  .gm-rb.mine { border-color: var(--orange); background: rgba(var(--orange-rgb), .12); }
  .gm-off { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
  .gm-oi { font-size: 26px; color: var(--orange); }
  .gm-ot { min-width: 0; flex: 1 1 140px; display: flex; flex-direction: column; gap: 1px; }
  .gm-ot b { font-size: 13.5px; overflow-wrap: anywhere; }
  .gm-ot small, .gm-own { font-size: 11.5px; color: var(--text-mute); }
  .gm-act { display: none; gap: 2px; align-self: center; position: relative; }
  .gm-row:hover .gm-act, .gm-row:focus-within .gm-act { display: flex; }
  .gm-act button { width: 26px; height: 26px; display: grid; place-items: center; border-radius: 7px; border: none; background: transparent; color: var(--text-mute); cursor: pointer; }
  .gm-act button:hover { color: var(--orange); background: var(--hover); }
  .gm-act button.danger:hover { color: var(--red, #e5484d); }
  .gm-em { position: absolute; top: 28px; z-index: 5; display: flex; gap: 2px; padding: 4px; border-radius: 10px; background: var(--bg-card); border: 1px solid var(--border-hi); box-shadow: 0 6px 18px rgba(0, 0, 0, .3); }
  .gm-row.own .gm-em { right: 0; }
  .gm-em button { width: 28px; height: 28px; font-size: 16px; }

  .gt { min-height: 18px; padding: 0 20px; font-size: 11.5px; color: var(--text-mute); }
  .gf { padding: 8px 16px 14px; border-top: 1px solid var(--border); display: flex; flex-direction: column; gap: 6px; }
  .gf-ctx { display: flex; align-items: center; gap: 6px; font-size: 12px; color: var(--text-mute); }
  .gf-ctx span { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .gf-ctx button { border: none; background: none; color: var(--text-mute); cursor: pointer; }
  .gf-row { display: flex; gap: 8px; align-items: flex-end; }
  .gf-row textarea { flex: 1; min-width: 0; resize: none; max-height: 120px; padding: 10px 14px; border-radius: 12px; border: 1px solid var(--border-hi); background: var(--bg-input); color: var(--text); font: inherit; font-size: 13.5px; }
  .gf-row textarea:focus { outline: none; border-color: rgba(var(--orange-rgb), .7); box-shadow: 0 0 0 3px rgba(var(--orange-rgb), .15); }
  .gf-send { width: 40px; height: 40px; border-radius: 12px; border: 1px solid var(--border-hi); background: var(--bg-input); color: var(--text-mute); font-size: 17px; cursor: pointer; }
  .gf-send.ready { color: #fff; border-color: transparent; background: linear-gradient(135deg, var(--orange), var(--orange-2, var(--orange))); }
  .gf-send:disabled { opacity: .6; cursor: default; }
</style>
