<script lang="ts">
  // ── Розділ «Сервери» на сторінці збірки ────────────────────────────────
  // Список серверів збірки: додавання, редагування (назва/адреса),
  // закріплення, видалення. Дані зберігаються per-instance через
  // InstanceConfig.servers (бекенд App.SaveInstanceConfig).
  import { t } from './i18n.svelte'
  import { confirmDialog } from './toast.svelte'
  import type { InstanceServer } from './types'

  let {
    servers,
    onAdd,
    onUpdate,
    onRemove
  } = $props<{
    servers: InstanceServer[]
    onAdd: () => void
    onUpdate: (id: string, patch: Partial<InstanceServer>) => void
    onRemove: (id: string) => void
  }>()
</script>

<div class="toolbar" style="margin-bottom:14px">
  <button class="btn btn-primary btn-sm" onclick={onAdd}><i class="ti ti-plus"></i> {t('pack.servers.add')}</button>
</div>

{#if servers.length === 0}
  <div class="card"><div style="padding:20px 22px;font-size:12px;color:var(--text-mute);line-height:1.6">{t('pack.servers.empty')}</div></div>
{:else}
  {#each servers as server (server.id)}
    <div class="srv-row">
      <div class="srv-fav"><i class="ti {server.pinned ? 'ti-pinned text-orange' : 'ti-pin'}"></i></div>
      <div class="srv-info">
        <input class="input" style="margin-bottom:6px" placeholder={t('pack.servers.name')} value={server.name}
               onchange={(e: any) => onUpdate(server.id, { name: e.target.value })} />
        <input class="input mono" placeholder={t('pack.servers.address')} value={server.address}
               onchange={(e: any) => onUpdate(server.id, { address: e.target.value })} />
      </div>
      {#if server.official}<span class="chip">{t('pack.servers.official')}</span>{/if}
      <button class="icon-btn" title={server.pinned ? t('pack.servers.unpin') : t('pack.servers.pin')}
              onclick={() => onUpdate(server.id, { pinned: !server.pinned })}>
        <i class="ti {server.pinned ? 'ti-pinned text-orange' : 'ti-pin'}"></i>
      </button>
      <button class="icon-btn del" title={t('pack.servers.remove')} onclick={() => onRemove(server.id)}>
        <i class="ti ti-trash"></i>
      </button>
    </div>
  {/each}
{/if}

<style>
  .toolbar { display:flex; gap:8px; align-items:center; }
  .icon-btn {
    width:32px; height:32px; border-radius:9px; border:none; background:var(--hover);
    color:var(--text-mute); cursor:pointer; display:flex; align-items:center; justify-content:center;
    transition:background .15s, color .15s; flex-shrink:0;
  }
  .icon-btn:hover { background:var(--hover-strong); color:var(--text); }
  .icon-btn.del:hover { background:rgba(239,68,68,.15); color:var(--red); }
</style>
