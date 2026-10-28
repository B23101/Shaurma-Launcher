<script lang="ts">
  // ── «Сервер разом зі збіркою» ──────────────────────────────────────────
  // Одна картка для обох сторін зв'язку (backend/pack_server_link.go):
  //   · на сторінці збірки (packId) — які сервери стартують разом із нею;
  //   · на сторінці сервера (serverId) — з якими збірками стартує він.
  // Для кожного зв'язку: запускати разом, підключитися після входу в гру,
  // зупиняти сервер після виходу з гри.
  import { onMount } from 'svelte'
  import SCard from '../../settings/SCard.svelte'
  import '../../settings/settings.css'
  import StSelect, { type StOption } from '../../StSelect.svelte'
  import { App } from '../../wails'
  import { t } from '../../i18n.svelte'
  import { toast } from '../../toast.svelte'
  import { serverVisual } from '../serverTargets'
  import { packAsset } from '../../packAssets.svelte'
  import type { MigrationPack, PackServerLink, ServerView } from '../../types'

  let { packId = '', serverId = '' }: { packId?: string; serverId?: string } = $props()

  const onPack = $derived(!!packId)

  let links = $state<PackServerLink[]>([])
  let servers = $state<ServerView[]>([])
  let packs = $state<MigrationPack[]>([])
  let busy = $state(false)

  async function load() {
    try {
      const [l, s, p] = await Promise.all([
        onPack ? App.GetPackServerLinks(packId) : App.GetServerPackLinks(serverId),
        App.ListServers(),
        App.GetMigrationPacks(''),
      ])
      links = l ?? []
      servers = s ?? []
      packs = (p ?? []).filter(x => x.kind !== 'server')
    } catch (e: any) { toast(String(e), 'error') }
  }
  onMount(load)

  const serverById = $derived(new Map(servers.map(s => [s.id, s])))
  const packById = $derived(new Map(packs.map(p => [p.packId, p])))

  // «Інший бік» зв'язку: на сторінці збірки — сервер, на сторінці сервера — збірка.
  function otherId(l: PackServerLink): string { return onPack ? l.serverId : l.packId }

  type Peer = { name: string; sub: string; src?: string; ti: string; color: string; mc: string; missing: boolean }
  function peer(l: PackServerLink): Peer {
    if (onPack) {
      const s = serverById.get(l.serverId)
      if (!s) return { name: l.serverName || l.serverId, sub: '', ti: 'ti-server-off', color: 'var(--text-dim)', mc: '', missing: true }
      const v = serverVisual(s)
      return { name: s.name, sub: `${s.coreLabel || s.core} · ${s.mcVersion} · 127.0.0.1:${s.port}`, src: v.src, ti: v.ti, color: v.color, mc: s.mcVersion, missing: false }
    }
    const p = packById.get(l.packId)
    if (!p) return { name: l.packName || l.packId, sub: '', ti: 'ti-package-off', color: 'var(--text-dim)', mc: '', missing: true }
    // iconUrl — шлях/URL збірки, а не готова картинка: як у сайдбарі, беремо через packAsset
    // (до завантаження — undefined, тоді показується іконка-заглушка).
    return { name: p.name, sub: [p.loader, p.mcVersion].filter(Boolean).join(' · '), src: packAsset(p.iconUrl), ti: p.icon || 'ti-package', color: p.color || 'var(--orange)', mc: p.mcVersion, missing: false }
  }

  // Версія гри другого боку — для попередження «інша версія».
  const ownMc = $derived(onPack ? (packById.get(packId)?.mcVersion ?? '') : (serverById.get(serverId)?.mcVersion ?? ''))

  const addOptions = $derived.by((): StOption[] => {
    const taken = new Set(links.map(otherId))
    return onPack
      ? servers.filter(s => !taken.has(s.id)).map(s => ({ value: s.id, label: `${s.name} · ${s.mcVersion}`, icon: serverVisual(s).ti || 'ti-server-2' }))
      : packs.filter(p => !taken.has(p.packId)).map(p => ({ value: p.packId, label: `${p.name} · ${p.mcVersion}`, icon: p.icon || 'ti-package' }))
  })

  async function save(l: PackServerLink) {
    busy = true
    try {
      await App.SavePackServerLink(l)
      await load()
    } catch (e: any) { toast(String(e), 'error') }
    finally { busy = false }
  }

  function add(id: string) {
    if (!id) return
    save({
      packId: onPack ? packId : id,
      serverId: onPack ? id : serverId,
      // За замовчуванням сервер лише стартує разом зі збіркою; автопідключення
      // вмикається окремим перемикачем.
      launchWithPack: true, joinOnLaunch: false, stopWithPack: false,
    })
  }

  function toggle(l: PackServerLink, key: 'launchWithPack' | 'joinOnLaunch' | 'stopWithPack') {
    save({ ...l, [key]: !l[key] })
  }

  async function remove(l: PackServerLink) {
    busy = true
    try {
      await App.RemovePackServerLink(l.packId, l.serverId)
      await load()
    } catch (e: any) { toast(String(e), 'error') }
    finally { busy = false }
  }

  const toggles: { key: 'launchWithPack' | 'joinOnLaunch' | 'stopWithPack'; name: string; hint: string }[] = $derived([
    { key: 'launchWithPack', name: t('packlink.launch'), hint: t('packlink.launchHint') },
    { key: 'joinOnLaunch', name: t('packlink.join'), hint: t('packlink.joinHint') },
    { key: 'stopWithPack', name: t('packlink.stop'), hint: t('packlink.stopHint') },
  ])
</script>

<SCard icon="ti-server-bolt" tone="violet"
       title={onPack ? t('packlink.titlePack') : t('packlink.titleServer')}
       desc={onPack ? t('packlink.descPack') : t('packlink.descServer')}>
  {#each links as l (otherId(l))}
    {@const p = peer(l)}
    <div class="st-row">
      <div class="pl-peer">
        <span class="pl-ic" style="--c:{p.color}">
          {#if p.src}<img src={p.src} alt="" />{:else}<i class="ti {p.ti}"></i>{/if}
        </span>
        <div class="st-row-text">
          <div class="st-row-name pl-name">{p.name}</div>
          {#if p.missing}
            <div class="st-row-hint">{onPack ? t('packlink.serverMissing') : t('packlink.packMissing')}</div>
          {:else}
            <div class="st-row-hint pl-sub">{p.sub}</div>
          {/if}
        </div>
      </div>
      <div class="st-row-ctl">
        {#if !p.missing && ownMc && p.mc && p.mc !== ownMc}
          <span class="st-tag warn" title={t('packlink.versionMismatchHint')}><i class="ti ti-alert-triangle"></i> {t('packlink.versionMismatch')}</span>
        {/if}
        <button type="button" class="st-iconbtn danger" onclick={() => remove(l)} disabled={busy}
                title={t('packlink.remove')} aria-label={t('packlink.remove')}><i class="ti ti-unlink"></i></button>
      </div>
    </div>
    {#each toggles as tg (tg.key)}
      <div class="st-row sub">
        <div class="st-row-text">
          <div class="st-row-name">{tg.name}</div>
          <div class="st-row-hint">{tg.hint}</div>
        </div>
        <div class="st-row-ctl">
          <button type="button" class="st-switch" class:on={l[tg.key]} role="switch" aria-checked={l[tg.key]}
                  aria-label={tg.name} disabled={busy || (tg.key === 'joinOnLaunch' && !l.launchWithPack)}
                  onclick={() => toggle(l, tg.key)}></button>
        </div>
      </div>
    {/each}
  {/each}

  <div class="st-row">
    <div class="st-row-text">
      <div class="st-row-name">{onPack ? t('packlink.addServer') : t('packlink.addPack')}</div>
      {#if links.length === 0}<div class="st-row-hint">{onPack ? t('packlink.emptyPack') : t('packlink.emptyServer')}</div>{/if}
    </div>
    <div class="st-row-ctl">
      {#if addOptions.length === 0}
        <span class="st-row-hint" style="margin:0">{onPack ? t('packlink.noServers') : t('packlink.noPacks')}</span>
      {:else}
        <StSelect value="" options={addOptions} minWidth="220px" disabled={busy}
                  placeholder={onPack ? t('packlink.pickServer') : t('packlink.pickPack')}
                  ariaLabel={onPack ? t('packlink.addServer') : t('packlink.addPack')}
                  onchange={add} />
      {/if}
    </div>
  </div>
</SCard>

<style>
  .pl-peer { flex: 1; min-width: 0; display: flex; align-items: center; gap: 12px; }
  .pl-ic {
    width: 36px; height: 36px; flex: none; border-radius: 9px; overflow: hidden;
    display: flex; align-items: center; justify-content: center; font-size: 18px;
    color: var(--c); background: color-mix(in srgb, var(--c) 14%, transparent);
  }
  .pl-ic img { width: 100%; height: 100%; object-fit: cover; }
  .pl-name, .pl-sub { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
</style>
