<script lang="ts">
  // Вибір однієї групи — «Поділитися в групу» (мод або збірка). Показує лише групи, де цей вид
  // вмісту дозволено налаштуваннями; решта видимі, але вимкнені з поясненням.
  import { onMount } from 'svelte'
  import UiModal from '../UiModal.svelte'
  import { App, on } from '../wails'
  import { t } from '../i18n.svelte'
  import { safeGroupColor, type Group } from './groupTypes'
  import GroupAvatar from './GroupAvatar.svelte'

  let { kind, title = '', busy = false, zIndex = 1000000, onpick, onclose, onback = null }: {
    kind: 'mods' | 'builds'
    title?: string
    busy?: boolean
    zIndex?: number
    onpick: (g: Group) => void
    onclose: () => void
    onback?: (() => void) | null
  } = $props()
  // Вікно відкривається і поза розділом «Друзі» (сторінка збірки/моду), тож список груп бере сам:
  // просить знімок і чекає подію groups_snapshot.
  let list = $state<Group[]>([])
  let loading = $state(true)
  onMount(() => {
    const off = on('group:event', (e: { type?: string; data?: any }) => {
      if (e?.type === 'groups_snapshot' && Array.isArray(e.data?.groups)) { list = e.data.groups; loading = false }
    })
    const timer = setTimeout(() => (loading = false), 4000)
    App.GroupRequest('list').catch(() => { loading = false })
    return () => { off(); clearTimeout(timer) }
  })
  const allowed = (g: Group) => (kind === 'mods' ? g.settings.allowMods : g.settings.allowBuilds)
</script>

<UiModal open={true} {zIndex} icon="ti-users-group" title={title || t('groups.pick.title')}
         subtitle={t(kind === 'mods' ? 'groups.pick.subMods' : 'groups.pick.subBuilds')} width="min(460px, 94vw)" locked={busy} {onclose}>
  <ul class="gp-list">
    {#each list as g (g.id)}
      <li>
        <button class="gp-r" disabled={busy || !allowed(g)} onclick={() => onpick(g)} style="--gc:{safeGroupColor(g.settings.color) || 'var(--border-hi)'}">
          <GroupAvatar title={g.title} icon={g.settings.icon} color={g.settings.color} size={30} />
          <span class="gp-n">{g.title}</span>
          <small>{allowed(g) ? t('groups.members', { n: g.members.length }) : t(kind === 'mods' ? 'groups.pick.noMods' : 'groups.pick.noBuilds')}</small>
        </button>
      </li>
    {:else}
      <li class="gp-empty">{loading ? '…' : t('groups.pick.none')}</li>
    {/each}
  </ul>
  {#snippet footer()}
    {#if onback}<button class="btn btn-ghost" onclick={onback} disabled={busy}>{t('groups.back')}</button>{/if}
    <button class="btn btn-ghost" onclick={onclose} disabled={busy}>{t('share.cancel')}</button>
  {/snippet}
</UiModal>

<style>
  .gp-list { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 6px; max-height: 340px; overflow-y: auto; }
  .gp-r { width: 100%; display: flex; align-items: center; gap: 10px; padding: 10px 12px; border-radius: 12px; border: 1px solid var(--gc); background: var(--bg-input); color: var(--text); cursor: pointer; text-align: left; }
  .gp-r:hover:not(:disabled) { background: rgba(var(--orange-rgb), .08); }
  .gp-r:disabled { opacity: .55; cursor: not-allowed; }
  .gp-n { min-width: 0; flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-weight: 700; font-size: 13.5px; }
  .gp-r small { font-size: 11.5px; color: var(--text-mute); white-space: nowrap; }
  .gp-empty { padding: 18px; text-align: center; font-size: 12.5px; color: var(--text-mute); }
</style>
