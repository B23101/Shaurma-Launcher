<script lang="ts">
  import { tip as launcherTip } from '../friends/tooltip'

  // Налаштування групи. Адмін змінює все (назву, колір аватарки, видимість історії для новачків,
  // хто закріплює/відкріплює, дозвіл на моди й збірки), керує складом (додати, запросити, виключити,
  // передати адмінку, видалити групу). Учасник бачить склад і може вийти. Права — лише підказка UI:
  // кожну дію перевіряє сервер.
  import '../settings/settings.css'
  import UiModal from '../UiModal.svelte'
  import { t } from '../i18n.svelte'
  import { App } from '../wails'
  import { toast, confirmDialog } from '../toast.svelte'
  import { clean } from '../friends/FriendsStore.svelte'
  import { useGroupsStore } from './GroupsStore.svelte'
  import { GROUP_ICONS, GROUP_MAX_TITLE, safeGroupColor, safeGroupIcon, type Group, type GroupWho } from './groupTypes'
  import GroupAvatar from './GroupAvatar.svelte'

  let { group, onclose, onmembers }: { group: Group; onclose: () => void; onmembers: () => void } = $props()
  const g = useGroupsStore()
  const admin = $derived(g.isAdmin(group))
  const s = $derived(group.settings)
  const icon = $derived(safeGroupIcon(s.icon))
  const color = $derived(safeGroupColor(s.color))

  // Чернетка назви: застосовується кнопкою (не на кожен символ).
  let title = $state(group.title)
  const letter = $derived(((clean(title) || group.title).trim().charAt(0) || '?').toUpperCase())
  const COLORS = ['', '#ff8a32', '#fb7185', '#4ade80', '#60a5fa', '#b69aff', '#2dd4bf', '#f472b6']

  const save = (patch: Parameters<typeof g.patchSettings>[1]) => g.patchSettings(group.id, patch)

  let linkBusy = $state(false)
  async function groupLink(revoke = false) {
    if (linkBusy) return
    linkBusy = true
    try {
      const r = await App.GroupRequest('link_create', { groupId: group.id, revoke })
      if (!revoke && /^[a-f0-9]{64}$/.test(r?.token ?? '')) await navigator.clipboard.writeText('https://shaurma-launcher.org/group/join#' + r.token)
      toast(t(revoke ? 'groups.link.revoked' : 'groups.link.copied'), 'success')
    } catch (e) { toast(g.errText(e), 'error') }
    finally { linkBusy = false }
  }
  async function leave() {
    const ok = await confirmDialog(t('groups.leave.title'), t(admin && group.members.length > 1 ? 'groups.leave.textAdmin' : 'groups.leave.text'), { confirmLabel: t('groups.leave.go'), cancelLabel: t('share.cancel') })
    if (ok && (await g.leave(group.id))) onclose()
  }
  async function del() {
    const ok = await confirmDialog(t('groups.delete.title'), t('groups.delete.text'), { confirmLabel: t('groups.delete.go'), cancelLabel: t('share.cancel') })
    if (ok && (await g.deleteGroup(group.id))) onclose()
  }
</script>

<UiModal open={true} icon="ti-settings" title={t('groups.settings.title')} subtitle={group.title} width="min(560px, 94vw)" locked={g.busy} {onclose}>
  {#if admin}
    <div class="gs-sec">{t('groups.settings.name')}</div>
    <div class="gs-row">
      <input class="gs-in" bind:value={title} maxlength={GROUP_MAX_TITLE} aria-label={t('groups.settings.name')} spellcheck="false" />
      <button class="btn btn-primary btn-sm" disabled={g.busy || !clean(title) || clean(title) === group.title} onclick={() => save({ title: clean(title) })}>{t('groups.settings.save')}</button>
    </div>

    <div class="gs-sec">{t('groups.settings.look')}</div>
    <div class="gs-look">
      <div class="gs-prev">
        <GroupAvatar title={clean(title) || group.title} icon={icon} color={color} size={44} />
        <span class="gs-pt"><b>{clean(title) || group.title}</b><small>{t('groups.members', { n: group.members.length })}</small></span>
      </div>

      <div class="gs-lbl">{t('groups.settings.icon')}</div>
      <div class="gs-icons" role="radiogroup" aria-label={t('groups.settings.icon')}>
        {#each ['', ...GROUP_ICONS] as ic (ic)}
          <button class="gs-i" class:on={icon === ic} role="radio" aria-checked={icon === ic} disabled={g.busy}
                  onclick={() => icon !== ic && save({ icon: ic })}
                  use:launcherTip={ic ? ic : t('groups.settings.iconNone')} aria-label={ic ? ic : t('groups.settings.iconNone')}>
            {#if ic}<i class="ti ti-{ic}"></i>{:else}<span class="gs-i-l">{letter}</span>{/if}
          </button>
        {/each}
      </div>

      <div class="gs-lbl">{t('groups.settings.color')}</div>
      <div class="gs-colors" role="radiogroup" aria-label={t('groups.settings.color')}>
        {#each COLORS as c (c)}
          <button class="gs-c" class:on={color === c} role="radio" aria-checked={color === c} disabled={g.busy}
                  style="--c:{c || 'transparent'}" onclick={() => color !== c && save({ color: c })} use:launcherTip={c || t('groups.settings.colorNone')}
                  aria-label={c || t('groups.settings.colorNone')}>
            {#if !c}<i class="ti ti-ban"></i>{/if}
          </button>
        {/each}
      </div>
    </div>

    <div class="gs-sec">{t('groups.link.title')}</div>
    <p class="gs-ro">{t('groups.link.hint')}</p>
    <div class="gs-row"><button class="btn btn-secondary" disabled={linkBusy || g.busy} onclick={() => groupLink()}><i class="ti ti-link"></i>{t('groups.link.copy')}</button><button class="btn btn-ghost" disabled={linkBusy || g.busy} onclick={() => groupLink(true)}>{t('groups.link.revoke')}</button></div>
    <div class="gs-sec">{t('groups.settings.rules')}</div>
    <div class="gs-t"><span>{t('groups.settings.history')}<small>{t('groups.settings.historyHint')}</small></span>
      <button type="button" class="st-switch" class:on={s.historyForNew} role="switch" aria-checked={s.historyForNew} aria-label={t('groups.settings.history')} disabled={g.busy} onclick={() => save({ historyForNew: !s.historyForNew })}></button></div>
    <div class="gs-t"><span>{t('groups.settings.mods')}<small>{t('groups.settings.modsHint')}</small></span>
      <button type="button" class="st-switch" class:on={s.allowMods} role="switch" aria-checked={s.allowMods} aria-label={t('groups.settings.mods')} disabled={g.busy} onclick={() => save({ allowMods: !s.allowMods })}></button></div>
    <div class="gs-t"><span>{t('groups.settings.builds')}<small>{t('groups.settings.buildsHint')}</small></span>
      <button type="button" class="st-switch" class:on={s.allowBuilds} role="switch" aria-checked={s.allowBuilds} aria-label={t('groups.settings.builds')} disabled={g.busy} onclick={() => save({ allowBuilds: !s.allowBuilds })}></button></div>
    {#each [['pinBy', 'groups.settings.pinBy'], ['unpinBy', 'groups.settings.unpinBy']] as [key, label] (key)}
      <div class="gs-t"><span>{t(label)}</span>
        <div class="gs-segment" role="group" aria-label={t(label)}>
          {#each ['admin', 'all'] as value}
            <button class:active={(key === 'pinBy' ? s.pinBy : s.unpinBy) === value} aria-pressed={(key === 'pinBy' ? s.pinBy : s.unpinBy) === value} disabled={g.busy} onclick={() => save({ [key]: value as GroupWho })}>{t(value === 'admin' ? 'groups.settings.whoAdmin' : 'groups.settings.whoAll')}</button>
          {/each}
        </div></div>
    {/each}
  {:else}
    <p class="gs-ro">{t('groups.settings.readonly')}</p>
  {/if}

  <button class="gs-members" onclick={onmembers}>
    <i class="ti ti-users-group"></i><span>{t('groups.members.title')}<small>{t('groups.members', { n: group.members.length })}</small></span><i class="ti ti-chevron-right"></i>
  </button>

  <p class="gs-sec2"><i class="ti ti-lock-exclamation"></i> {t('groups.settings.notE2e')}</p>

  {#snippet footer()}
    {#if admin}<button class="btn btn-danger" disabled={g.busy} onclick={del}><i class="ti ti-trash"></i> {t('groups.delete.go')}</button>{/if}
    <button class="btn btn-ghost" disabled={g.busy} onclick={leave}><i class="ti ti-logout"></i> {t('groups.leave.go')}</button>
    <button class="btn btn-primary" onclick={onclose}>{t('groups.close')}</button>
  {/snippet}
</UiModal>

<style>
  .gs-sec { margin: 20px 0 10px; font-size: var(--fs-min, 11px); font-weight: 800; text-transform: uppercase; letter-spacing: .4px; color: var(--text-mute); }
  .gs-sec:first-child { margin-top: 0; }
  .gs-row { display: flex; gap: 8px; }
  .gs-in { flex: 1; min-width: 0; padding: 8px 12px; border-radius: 10px; border: 1px solid var(--border-hi); background: var(--bg-input); color: var(--text); font: inherit; font-size: 13px; }
  .gs-in:focus { outline: none; border-color: var(--orange); }
  .gs-look { --gc: var(--orange); padding: 12px; border-radius: 12px; border: 1px solid var(--border); background: var(--bg-input); }
  .gs-prev { display: flex; align-items: center; gap: 12px; padding-bottom: 12px; border-bottom: 1px solid var(--border); }
  .gs-pt { min-width: 0; display: flex; flex-direction: column; gap: 2px; }
  .gs-pt b { font-size: 14px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .gs-pt small { font-size: 12px; color: var(--text-mute); }
  .gs-lbl { margin: 12px 0 8px; font-size: 12px; font-weight: 700; color: var(--text-mute); }
  .gs-icons { display: grid; grid-template-columns: repeat(auto-fill, minmax(40px, 1fr)); gap: 6px; }
  .gs-i { aspect-ratio: 1; min-width: 0; display: grid; place-items: center; padding: 0; border-radius: 10px; border: 1px solid var(--border); background: var(--bg-card); color: var(--text-mute); cursor: pointer; font-size: 18px; transition: color .12s, border-color .12s, background .12s; }
  .gs-i:hover:not(:disabled) { color: var(--text); border-color: var(--border-hi); }
  .gs-i.on { color: var(--gc); border-color: var(--gc); background: color-mix(in srgb, var(--gc) 14%, var(--bg-card)); }
  .gs-i:focus-visible, .gs-c:focus-visible { outline: 2px solid var(--gc); outline-offset: 2px; }
  .gs-i:disabled, .gs-c:disabled { cursor: default; }
  .gs-i-l { font-size: 15px; font-weight: 800; line-height: 1; }
  .gs-colors { display: flex; gap: 10px; flex-wrap: wrap; padding: 2px; }
  .gs-c { width: 28px; height: 28px; padding: 0; border-radius: 50%; border: 2px solid var(--border-hi); background: var(--c); cursor: pointer; display: grid; place-items: center; color: var(--text-mute); }
  .gs-c.on { border-color: var(--text); box-shadow: 0 0 0 2px var(--bg-input), 0 0 0 4px var(--c, var(--orange)); }
  .gs-t { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 14px 16px; border: 1px solid var(--border); border-radius: 12px; background: var(--bg-card); margin-bottom: 8px; font-size: 13px; }
  .gs-t span { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .gs-t small { font-size: 11.5px; color: var(--text-mute); }
  .gs-ro { margin: 6px 0; font-size: 12px; line-height: 1.45; color: var(--text-mute); }
  .gs-sec2 { display: flex; gap: 6px; margin: 14px 0 0; font-size: 11.5px; line-height: 1.45; color: var(--text-mute); }
  .gs-sec2 i { flex: none; margin-top: 1px; }
  .gs-members { display: flex; width: 100%; align-items: center; gap: 12px; padding: 16px; margin-top: 20px; border: 1px solid var(--border); border-radius: 12px; background: var(--bg-card); color: var(--text); text-align: left; cursor: pointer; }
  .gs-members > span { flex: 1; font-weight: 600; }
  .gs-members small { display: block; margin-top: 4px; color: var(--text-mute); font-weight: 400; }
  .gs-members > i:first-child { color: var(--orange); font-size: 22px; }
  .gs-segment { display: flex; padding: 3px; gap: 3px; background: var(--bg-input); border: 1px solid var(--border); border-radius: 10px; }
  .gs-segment button { padding: 7px 10px; border: 0; border-radius: 7px; background: transparent; color: var(--text-mute); font: 600 12px var(--font-ui); cursor: pointer; }
  .gs-segment button.active { background: var(--hover-strong); color: var(--text); }
  .gs-segment button:focus-visible, .gs-members:focus-visible { outline: 2px solid var(--orange); outline-offset: 2px; }
  @media (max-width: 480px) { .gs-t:has(.gs-segment) { flex-direction: column; align-items: stretch; } .gs-segment button { flex: 1; } }
</style>
