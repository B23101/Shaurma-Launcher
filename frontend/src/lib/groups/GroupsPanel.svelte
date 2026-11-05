<script lang="ts">
  // Вкладка «Групи» в лівій панелі: запрошення, список груп, кнопка «Нова група».
  import { t } from '../i18n.svelte'
  import { useGroupsStore } from './GroupsStore.svelte'
  import { safeGroupColor } from './groupTypes'
  import GroupAvatar from './GroupAvatar.svelte'

  let { compact = false, invitesOnly = false, oncreate }: { compact?: boolean; invitesOnly?: boolean; oncreate: () => void } = $props()
  const g = useGroupsStore()
</script>

{#if !g.enabled && g.loaded}
  <p class="gl-off">{t('groups.disabled')}</p>
{:else}
  {#if !compact && !invitesOnly}
    <button class="btn btn-primary btn-sm gl-new" onclick={oncreate}><i class="ti ti-plus"></i> {t('groups.new')}</button>
  {/if}

  {#if g.invites.length > 0 && !compact}
    <h3 class="gl-sh"><i class="ti ti-mail"></i>{t('groups.invites')}<span>{g.invites.length}</span></h3>
    {#each g.invites as inv (inv.id)}
      <div class="gl-inv">
        <div class="gl-it"><b>{inv.groupTitle}</b><small>{t('groups.invite.from', { name: inv.fromNickname || g.nameOf(inv.from) })}</small></div>
        <div class="gl-ia">
          <button class="btn btn-primary btn-sm" disabled={g.busy} onclick={() => g.respondInvite(inv, true)}>{t('groups.invite.accept')}</button>
          <button class="btn btn-ghost btn-sm" disabled={g.busy} onclick={() => g.respondInvite(inv, false)}>{t('groups.invite.decline')}</button>
        </div>
      </div>
    {/each}
  {/if}

  {#if !invitesOnly}
  {#each g.groups as gr (gr.id)}
    <button class="gl-row" class:active={g.activeId === gr.id} class:compact onclick={() => g.open(gr.id)}
            style="--gc:{safeGroupColor(gr.settings.color) || 'var(--border-hi)'}" title={gr.title}>
      <GroupAvatar title={gr.title} icon={gr.settings.icon} color={gr.settings.color} />
      {#if !compact}
        <span class="gl-t"><b>{gr.title}</b><small>{t('groups.members', { n: gr.members.length })}</small></span>
      {/if}
      {#if (g.unread[gr.id] ?? 0) > 0}<span class="ff-badge gl-bd">{g.unread[gr.id]}</span>{/if}
    </button>
  {:else}
    {#if !compact}<p class="gl-empty">{t('groups.empty')}</p>{/if}
  {/each}
  {/if}
{/if}

<style>
  .gl-new { width: 100%; margin-bottom: 8px; }
  .gl-off, .gl-empty { margin: 12px 8px; font-size: 12.5px; line-height: 1.5; color: var(--text-mute); text-align: center; }
  .gl-sh { display: flex; align-items: center; gap: 6px; margin: 10px 4px 6px; font-size: var(--fs-min, 11px); font-weight: 800; text-transform: uppercase; letter-spacing: .4px; color: var(--text-mute); }
  .gl-sh span { margin-left: auto; }
  .gl-inv { display: flex; flex-direction: column; gap: 8px; padding: 10px; margin-bottom: 6px; border-radius: 12px; border: 1px solid rgba(var(--orange-rgb), .35); background: rgba(var(--orange-rgb), .07); }
  .gl-it { min-width: 0; display: flex; flex-direction: column; gap: 2px; }
  .gl-it b { font-size: 13px; overflow-wrap: anywhere; }
  .gl-it small { font-size: 11.5px; color: var(--text-mute); }
  .gl-ia { display: flex; gap: 6px; }
  .gl-row { width: 100%; display: flex; align-items: center; gap: 10px; padding: 8px 10px; margin-bottom: 4px; border-radius: 12px; border: 1px solid transparent; background: transparent; color: var(--text); cursor: pointer; text-align: left; position: relative; }
  .gl-row:hover { background: var(--hover); }
  .gl-row.active { background: rgba(var(--orange-rgb), .1); }
  .gl-row.compact { justify-content: center; padding: 6px; }
  .gl-t { min-width: 0; flex: 1; display: flex; flex-direction: column; gap: 2px; }
  .gl-t b { font-size: 13px; font-weight: 700; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .gl-t small { font-size: 11.5px; color: var(--text-mute); }
  .gl-bd { margin-left: auto; }
  .gl-row.compact .gl-bd { position: absolute; top: 2px; right: 2px; }
</style>
