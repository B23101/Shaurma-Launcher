<script lang="ts">
  // Налаштування групи. Адмін змінює все (назву, колір обведення, видимість історії для новачків,
  // хто закріплює/відкріплює, дозвіл на моди й збірки), керує складом (додати, запросити, виключити,
  // передати адмінку, видалити групу). Учасник бачить склад і може вийти. Права — лише підказка UI:
  // кожну дію перевіряє сервер.
  import UiModal from '../UiModal.svelte'
  import { t } from '../i18n.svelte'
  import { confirmDialog } from '../toast.svelte'
  import { clean } from '../friends/FriendsStore.svelte'
  import { useGroupsStore } from './GroupsStore.svelte'
  import { GROUP_ICONS, GROUP_MAX_MEMBERS, GROUP_MAX_TITLE, safeGroupColor, safeGroupIcon, type Group, type GroupWho } from './groupTypes'
  import GroupAvatar from './GroupAvatar.svelte'
  import type { SocialFriend } from '../types'

  let { group, friends, onclose }: { group: Group; friends: SocialFriend[]; onclose: () => void } = $props()
  const g = useGroupsStore()
  const admin = $derived(g.isAdmin(group))
  const s = $derived(group.settings)
  const icon = $derived(safeGroupIcon(s.icon))
  const color = $derived(safeGroupColor(s.color))

  // Чернетка назви: застосовується кнопкою (не на кожен символ).
  let title = $state(group.title)
  const letter = $derived(((clean(title) || group.title).trim().charAt(0) || '?').toUpperCase())
  let addOpen = $state(false)
  let query = $state('')
  const COLORS = ['', '#ff7a00', '#e5484d', '#30a46c', '#3e63dd', '#8e4ec6', '#12a594', '#e93d82']

  const inGroup = $derived(new Set(group.members.map((m) => m.accountId)))
  const candidates = $derived(
    friends.filter((f) => !inGroup.has(f.accountId) && (!query.trim() || clean(f.nickname).toLowerCase().includes(query.trim().toLowerCase()))),
  )
  const full = $derived(group.members.length >= GROUP_MAX_MEMBERS)

  const save = (patch: Parameters<typeof g.patchSettings>[1]) => g.patchSettings(group.id, patch)

  async function kick(id: string) {
    const ok = await confirmDialog(t('groups.kick.title'), t('groups.kick.text', { name: g.nameOf(id, group) }), { confirmLabel: t('groups.kick.go'), cancelLabel: t('share.cancel') })
    if (ok) await g.kick(group.id, id)
  }
  async function transfer(id: string) {
    const ok = await confirmDialog(t('groups.transfer.title'), t('groups.transfer.text', { name: g.nameOf(id, group) }), { confirmLabel: t('groups.transfer.go'), cancelLabel: t('share.cancel'), danger: false })
    if (ok) await g.transfer(group.id, id)
  }
  async function leave() {
    const ok = await confirmDialog(t('groups.leave.title'), t(admin && group.members.length > 1 ? 'groups.leave.textAdmin' : 'groups.leave.text'), { confirmLabel: t('groups.leave.go'), cancelLabel: t('share.cancel') })
    if (ok && (await g.leave(group.id))) onclose()
  }
  async function del() {
    const ok = await confirmDialog(t('groups.delete.title'), t('groups.delete.text'), { confirmLabel: t('groups.delete.go'), cancelLabel: t('share.cancel') })
    if (ok && (await g.deleteGroup(group.id))) onclose()
  }
  async function addFriend(f: SocialFriend) { await g.add(group.id, [f.accountId]) }
</script>

<UiModal open={true} icon="ti-settings" title={t('groups.settings.title')} subtitle={group.title} width="min(560px, 94vw)" locked={g.busy} {onclose}>
  {#if admin}
    <div class="gs-sec">{t('groups.settings.name')}</div>
    <div class="gs-row">
      <input class="gs-in" bind:value={title} maxlength={GROUP_MAX_TITLE} aria-label={t('groups.settings.name')} spellcheck="false" />
      <button class="btn btn-primary btn-sm" disabled={g.busy || !clean(title) || clean(title) === group.title} onclick={() => save({ title: clean(title) })}>{t('groups.settings.save')}</button>
    </div>

    <div class="gs-sec">{t('groups.settings.look')}</div>
    <div class="gs-look" style={color ? `--gc:${color}` : ''}>
      <div class="gs-prev">
        <GroupAvatar title={clean(title) || group.title} icon={icon} color={color} size={44} />
        <span class="gs-pt"><b>{clean(title) || group.title}</b><small>{t('groups.members', { n: group.members.length })}</small></span>
      </div>

      <div class="gs-lbl">{t('groups.settings.icon')}</div>
      <div class="gs-icons" role="radiogroup" aria-label={t('groups.settings.icon')}>
        {#each ['', ...GROUP_ICONS] as ic (ic)}
          <button class="gs-i" class:on={icon === ic} role="radio" aria-checked={icon === ic} disabled={g.busy}
                  onclick={() => icon !== ic && save({ icon: ic })}
                  title={ic ? ic : t('groups.settings.iconNone')} aria-label={ic ? ic : t('groups.settings.iconNone')}>
            {#if ic}<i class="ti ti-{ic}"></i>{:else}<span class="gs-i-l">{letter}</span>{/if}
          </button>
        {/each}
      </div>

      <div class="gs-lbl">{t('groups.settings.color')}</div>
      <div class="gs-colors" role="radiogroup" aria-label={t('groups.settings.color')}>
        {#each COLORS as c (c)}
          <button class="gs-c" class:on={color === c} role="radio" aria-checked={color === c} disabled={g.busy}
                  style="--c:{c || 'transparent'}" onclick={() => color !== c && save({ color: c })} title={c || t('groups.settings.colorNone')}
                  aria-label={c || t('groups.settings.colorNone')}>
            {#if !c}<i class="ti ti-ban"></i>{/if}
          </button>
        {/each}
      </div>
    </div>

    <div class="gs-sec">{t('groups.settings.rules')}</div>
    <label class="gs-t"><span>{t('groups.settings.history')}<small>{t('groups.settings.historyHint')}</small></span>
      <input type="checkbox" checked={s.historyForNew} disabled={g.busy} onchange={(e) => save({ historyForNew: e.currentTarget.checked })} /></label>
    <label class="gs-t"><span>{t('groups.settings.mods')}<small>{t('groups.settings.modsHint')}</small></span>
      <input type="checkbox" checked={s.allowMods} disabled={g.busy} onchange={(e) => save({ allowMods: e.currentTarget.checked })} /></label>
    <label class="gs-t"><span>{t('groups.settings.builds')}<small>{t('groups.settings.buildsHint')}</small></span>
      <input type="checkbox" checked={s.allowBuilds} disabled={g.busy} onchange={(e) => save({ allowBuilds: e.currentTarget.checked })} /></label>
    {#each [['pinBy', 'groups.settings.pinBy'], ['unpinBy', 'groups.settings.unpinBy']] as [key, label] (key)}
      <div class="gs-t"><span>{t(label)}</span>
        <select value={key === 'pinBy' ? s.pinBy : s.unpinBy} disabled={g.busy}
                onchange={(e) => save({ [key]: e.currentTarget.value as GroupWho })}>
          <option value="admin">{t('groups.settings.whoAdmin')}</option>
          <option value="all">{t('groups.settings.whoAll')}</option>
        </select></div>
    {/each}
  {:else}
    <p class="gs-ro">{t('groups.settings.readonly')}</p>
  {/if}

  <div class="gs-sec">{t('groups.settings.members', { n: group.members.length, max: GROUP_MAX_MEMBERS })}</div>
  <ul class="gs-m">
    {#each group.members as m (m.accountId)}
      <li>
        <span class="gs-mn">{g.nameOf(m.accountId, group)}{#if m.accountId === group.admin}<i class="ti ti-crown" title={t('groups.admin.badge')}></i>{/if}</span>
        {#if admin && m.accountId !== g.me}
          <button class="gs-ib" disabled={g.busy} onclick={() => transfer(m.accountId)} title={t('groups.transfer.title')} aria-label={t('groups.transfer.title')}><i class="ti ti-crown"></i></button>
          <button class="gs-ib danger" disabled={g.busy} onclick={() => kick(m.accountId)} title={t('groups.kick.title')} aria-label={t('groups.kick.title')}><i class="ti ti-user-x"></i></button>
        {/if}
      </li>
    {/each}
  </ul>

  {#if admin}
    <button class="btn btn-ghost btn-sm" disabled={full} onclick={() => (addOpen = !addOpen)}><i class="ti ti-user-plus"></i> {t('groups.add.open')}</button>
    {#if addOpen}
      <input class="gs-in gs-q" bind:value={query} placeholder={t('social.search')} aria-label={t('social.search')} spellcheck="false" autocomplete="off" />
      <ul class="gs-m">
        {#each candidates as f (f.accountId)}
          <li><span class="gs-mn">{clean(f.nickname)}</span>
            <button class="btn btn-primary btn-sm" disabled={g.busy || full} onclick={() => addFriend(f)}>{t('groups.add.go')}</button></li>
        {:else}<li class="gs-ro">{t('groups.add.none')}</li>{/each}
      </ul>
      <p class="gs-ro">{t('groups.add.hint')}</p>
    {/if}
  {/if}

  <p class="gs-sec2"><i class="ti ti-lock-exclamation"></i> {t('groups.settings.notE2e')}</p>

  {#snippet footer()}
    {#if admin}<button class="btn btn-danger" disabled={g.busy} onclick={del}><i class="ti ti-trash"></i> {t('groups.delete.go')}</button>{/if}
    <button class="btn btn-ghost" disabled={g.busy} onclick={leave}><i class="ti ti-logout"></i> {t('groups.leave.go')}</button>
    <button class="btn btn-primary" onclick={onclose}>{t('groups.close')}</button>
  {/snippet}
</UiModal>

<style>
  .gs-sec { margin: 14px 0 6px; font-size: var(--fs-min, 11px); font-weight: 800; text-transform: uppercase; letter-spacing: .4px; color: var(--text-mute); }
  .gs-sec:first-child { margin-top: 0; }
  .gs-row { display: flex; gap: 8px; }
  .gs-in { flex: 1; min-width: 0; padding: 8px 12px; border-radius: 10px; border: 1px solid var(--border-hi); background: var(--bg-input); color: var(--text); font: inherit; font-size: 13px; }
  .gs-in:focus { outline: none; border-color: var(--orange); }
  .gs-q { width: 100%; margin: 8px 0; }
  .gs-look { --gc: var(--orange); padding: 12px; border-radius: 12px; border: 1px solid var(--border); background: var(--bg-input); }
  .gs-prev { display: flex; align-items: center; gap: 12px; padding-bottom: 12px; border-bottom: 1px solid var(--border); }
  .gs-pt { min-width: 0; display: flex; flex-direction: column; gap: 2px; }
  .gs-pt b { font-size: 14px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .gs-pt small { font-size: 12px; color: var(--text-mute); }
  .gs-lbl { margin: 12px 0 8px; font-size: 12px; font-weight: 700; color: var(--text-mute); }
  .gs-icons { display: grid; grid-template-columns: repeat(auto-fill, minmax(36px, 1fr)); gap: 6px; }
  .gs-i { aspect-ratio: 1; min-width: 0; display: grid; place-items: center; padding: 0; border-radius: 10px; border: 1px solid var(--border); background: var(--bg-card); color: var(--text-mute); cursor: pointer; font-size: 18px; transition: color .12s, border-color .12s, background .12s; }
  .gs-i:hover:not(:disabled) { color: var(--text); border-color: var(--border-hi); }
  .gs-i.on { color: var(--gc); border-color: var(--gc); background: color-mix(in srgb, var(--gc) 14%, var(--bg-card)); }
  .gs-i:focus-visible, .gs-c:focus-visible { outline: 2px solid var(--gc); outline-offset: 2px; }
  .gs-i:disabled, .gs-c:disabled { cursor: default; }
  .gs-i-l { font-size: 15px; font-weight: 800; line-height: 1; }
  .gs-colors { display: flex; gap: 10px; flex-wrap: wrap; padding: 2px; }
  .gs-c { width: 28px; height: 28px; padding: 0; border-radius: 50%; border: 2px solid var(--border-hi); background: var(--c); cursor: pointer; display: grid; place-items: center; color: var(--text-mute); }
  .gs-c.on { border-color: var(--text); box-shadow: 0 0 0 2px var(--bg-input), 0 0 0 4px var(--c, var(--orange)); }
  .gs-t { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 8px 0; border-bottom: 1px solid var(--border); font-size: 13px; }
  .gs-t span { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .gs-t small { font-size: 11.5px; color: var(--text-mute); }
  .gs-t select { padding: 6px 8px; border-radius: 8px; border: 1px solid var(--border-hi); background: var(--bg-input); color: var(--text); }
  .gs-m { list-style: none; margin: 0 0 10px; padding: 0; display: flex; flex-direction: column; gap: 4px; max-height: 220px; overflow-y: auto; }
  .gs-m li { display: flex; align-items: center; gap: 6px; padding: 6px 10px; border-radius: 10px; background: var(--bg-input); border: 1px solid var(--border); }
  .gs-mn { flex: 1; min-width: 0; display: flex; align-items: center; gap: 6px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 13px; font-weight: 600; }
  .gs-mn i { color: var(--orange); }
  .gs-ib { width: 28px; height: 28px; display: grid; place-items: center; border-radius: 8px; border: 1px solid var(--border-hi); background: transparent; color: var(--text-mute); cursor: pointer; }
  .gs-ib:hover:not(:disabled) { color: var(--orange); border-color: var(--orange); }
  .gs-ib.danger:hover:not(:disabled) { color: var(--red, #e5484d); border-color: var(--red, #e5484d); }
  .gs-ro { margin: 6px 0; font-size: 12px; line-height: 1.45; color: var(--text-mute); }
  .gs-sec2 { display: flex; gap: 6px; margin: 14px 0 0; font-size: 11.5px; line-height: 1.45; color: var(--text-mute); }
  .gs-sec2 i { flex: none; margin-top: 1px; }
</style>
