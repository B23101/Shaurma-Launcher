<script lang="ts">
  // ── Розділ «Сервери» на сторінці збірки ────────────────────────────────
  // Відображення у стилі гардероба: картки з gradient-background, halo-glow
  // при hover, floating action-buttons. Drag&drop з візуальним ghost-
  // індикатором вставки (рядок між картками, що «розсовує» місце куди
  // потрапить сервер) — так само, як у Wardrobe.svelte для пресетів.
  import { t } from './i18n.svelte'
  import { flip } from 'svelte/animate'
  import { toast, confirmDialog } from './toast.svelte'
  import { App } from './wails'
  import { packAsset } from './packAssets.svelte'
  import { parseMotdSegments, stripMotdCodes } from './motd'
  import { windowVisibility } from './visible.svelte'
  import type { InstanceServer, ServerPing, BuildView, BuildKind } from './types'
  import UiModal from './UiModal.svelte'
  import PackPicker from './PackPicker.svelte'
  import SelectField from './SelectField.svelte'
  import type { PackRow, PackFolderRow, PackPickerItem } from './PackPicker.svelte'

  let { packId, packName = '', disabled = false, allBuilds, packPicker = null, onServersChanged = () => {}, activeAccountId = '', stickyTop = 0, onPlay } = $props<{
    packId: string
    packName?: string
    disabled?: boolean
    allBuilds?: BuildView[]
    packPicker?: {
      items: PackPickerItem[]
      rowById: Map<string, PackRow>
      folderById: Map<string, PackFolderRow>
    } | null
    onServersChanged?: () => void
    // Відступ sticky-панелі інструментів під закріпленим тулбаром сторінки
    // збірки (як у менеджері контенту).
    stickyTop?: number
    // activeAccountId лишається пропом для сумісності; фіксація акаунта
    // (SetLaunchClickAccount) тепер відбувається в App.svelte до всього
    // ланцюга запуску (launchWithHealthGuard/doLaunch), а не тут.
    activeAccountId?: string
    // onPlay — та сама точка входу, що onLaunch на сторінці збірки
    // (App.svelte joinPinnedServer → launchWithHealthGuard): раніше
    // playOn() нижче викликав App.LaunchInstanceOnServer НАПРЯМУ, в обхід
    // health-guard/mod-check/ram-check і без видимого прогресу запуску —
    // кнопка «Грати» біля сервера в цьому списку працювала «тихіше», ніж
    // головна кнопка «Грати» на тій самій сторінці.
    onPlay: (packId: string, address: string) => void
  }>()

  let servers = $state<InstanceServer[]>([])
  let loading = $state(true)
  let query = $state('')
  let pings = $state<Record<string, ServerPing | null>>({})
  let pingingAll = $state(false)
  let selected = $state<Set<string>>(new Set())

  function dedupeServers(list: InstanceServer[]): InstanceServer[] {
    const seen = new Set<string>()
    const out: InstanceServer[] = []
    for (const s of list) {
      if (!s.address || seen.has(s.address)) continue
      seen.add(s.address)
      out.push(s)
    }
    return out
  }

  async function load() {
    loading = true
    try {
      servers = dedupeServers((await App.ListPackServers(packId)) ?? [])
    } catch {
      servers = []
    }
    selected = new Set([...selected].filter(a => servers.some(s => s.address === a)))
    loading = false
    pingVisible()
  }

  let lastPackId = ''
  let refreshTimer: ReturnType<typeof setInterval> | undefined
  async function refreshFromDisk() {
    if (!windowVisibility.visible) return
    if (!packId) return
    try {
      const fresh = dedupeServers(await App.ListPackServers(packId))
      const cur = JSON.stringify(servers.map(s => ({ a: s.address, n: s.name })))
      const next = JSON.stringify(fresh.map(s => ({ a: s.address, n: s.name })))
      if (cur !== next) {
        servers = fresh
        selected = new Set([...selected].filter(a => fresh.some(s => s.address === a)))
        pingVisible()
      }
    } catch { /* ігнор */ }
  }
  $effect(() => {
    if (packId && packId !== lastPackId) {
      lastPackId = packId
      load()
      clearInterval(refreshTimer)
      refreshTimer = setInterval(refreshFromDisk, 5000)
    }
  })
  $effect(() => () => clearInterval(refreshTimer))

  // Фільтр (Все/Онлайн/Офлайн) і сортування (порядок/назва/статус/гравці) —
  // як у менеджері модів: панель інструментів sticky, фільтри під нею.
  let statusFilter = $state<'all' | 'online' | 'offline'>('all')
  let sortBy = $state<'order' | 'name' | 'status' | 'players'>('order')
  const filtered = $derived.by(() => {
    const q = query.trim().toLowerCase()
    let list = q
      ? servers.filter(s => (s.name || '').toLowerCase().includes(q) || (s.address || '').toLowerCase().includes(q))
      : servers.slice()
    list = dedupeServers(list)
    if (statusFilter === 'online') list = list.filter(s => pings[s.address]?.online === true)
    else if (statusFilter === 'offline') list = list.filter(s => !(pings[s.address]?.online === true))
    if (sortBy === 'name') {
      return list.sort((a, b) => (a.name || a.address).localeCompare(b.name || b.address, undefined, { sensitivity: 'base' }))
    }
    if (sortBy === 'status') {
      return list.sort((a, b) => Number(pings[b.address]?.online === true) - Number(pings[a.address]?.online === true) || (a.name || a.address).localeCompare(b.name || b.address))
    }
    if (sortBy === 'players') {
      return list.sort((a, b) => (pings[b.address]?.players ?? 0) - (pings[a.address]?.players ?? 0))
    }
    return list
  })

  async function pingVisible() {
    if (!servers.length) return
    const addrs = servers.map(s => s.address)
    for (const a of addrs) pings = { ...pings, [a]: null }
    try {
      const result = await App.PingServers(addrs)
      pings = { ...pings, ...result }
    } catch {
      const fallback: Record<string, ServerPing> = {}
      for (const a of addrs) fallback[a] = { online: false, error: t('pack.game.pingError') }
      pings = { ...pings, ...fallback }
    }
  }

  async function pingAll() {
    if (!servers.length || pingingAll) return
    pingingAll = true
    await pingVisible()
    toast(t('pack.servers.pingedAll'), 'success')
    pingingAll = false
  }

  // ── Drag & drop з ghost-індикатором вставки (wardrobe/Modrinth-style) ──
  // dragOverAddress + dragOverPos визначають, куди саме (before/after) впаде
  // сервер — та сама механіка, що в гардеробі (Wardrobe.svelte) та всюди:
  // тонка кольорова лінія з потрібного боку цільової картки + картки
  // «розсуваються» через margin + animate:flip.
  let dragAddress = $state<string | null>(null)
  let dragOverAddress = $state<string | null>(null)
  let dragOverPos = $state<'before' | 'after' | null>(null)

  function onDragStart(e: DragEvent, address: string) {
    dragAddress = address
    if (e.dataTransfer) {
      e.dataTransfer.effectAllowed = 'move'
      e.dataTransfer.setData('text/plain', address)
    }
  }

  function onDragOverCard(e: DragEvent, address: string) {
    if (!dragAddress || dragAddress === address) return
    e.preventDefault()
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
    const pos = (e.clientY - rect.top) / (rect.height || 1) < 0.5 ? 'before' : 'after'
    dragOverAddress = address
    dragOverPos = pos
  }

  async function onDrop(e: DragEvent) {
    e.preventDefault()
    const from = dragAddress
    const to = dragOverAddress
    const pos = dragOverPos
    dragAddress = null
    dragOverAddress = null
    dragOverPos = null
    if (!from || !to || from === to) return

    const order = servers.map(s => s.address)
    const fromIdx = order.indexOf(from)
    const toIdx = order.indexOf(to)
    if (fromIdx < 0 || toIdx < 0) return

    order.splice(fromIdx, 1)
    const newToIdx = order.indexOf(to)
    order.splice(pos === 'before' ? newToIdx : newToIdx + 1, 0, from)

    const byAddr = new Map(servers.map(s => [s.address, s]))
    servers = order.map(a => byAddr.get(a)!).filter(Boolean)

    try {
      await App.ReorderPackServers(packId, order)
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
      load()
    }
  }

  function onDragEnd() {
    dragAddress = null
    dragOverAddress = null
    dragOverPos = null
  }

  // ── Закріплення ──
  async function togglePin(s: InstanceServer) {
    servers = servers.map(x => x.address === s.address ? { ...x, pinned: !x.pinned } : x)
    try {
      await App.TogglePinPackServer(packId, s.address)
      onServersChanged()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
      load()
    }
  }

  // ── Видалення ──
  async function removeServer(s: InstanceServer) {
    const ok = await confirmDialog(t('pack.servers.remove'), s.name || s.address, {
      confirmLabel: t('pack.servers.remove'), cancelLabel: t('cancel'), danger: true
    })
    if (!ok) return
    try {
      await App.RemovePackServer(packId, s.address)
      servers = servers.filter(x => x.address !== s.address)
      toast(t('pack.servers.removed'), 'success')
      onServersChanged()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  async function playOn(s: InstanceServer) {
    // onPlay (App.svelte joinPinnedServer → launchWithHealthGuard) сам
    // ловить і показує помилки запуску — той самий шлях, що й головна
    // кнопка «Грати», дублювати try/catch з окремим тостом тут не треба.
    await onPlay(packId, s.address)
  }

  // ── Модалка додавання/редагування ──
  let editorOpen = $state(false)
  let editingAddress = $state<string | null>(null)
  let formName = $state('')
  let formAddress = $state('')
  let formResourcePolicy = $state<'ask' | 'accept' | 'reject'>('ask')

  function openAddModal() {
    editingAddress = null; formName = ''; formAddress = ''; formResourcePolicy = 'ask'; editorOpen = true
  }
  function openEditModal(s: InstanceServer) {
    editingAddress = s.address; formName = s.name; formAddress = s.address; formResourcePolicy = 'ask'; editorOpen = true
  }
  function closeEditor() { editorOpen = false }

  async function submitEditor() {
    const address = formAddress.trim()
    if (!address) { toast(t('pack.servers.nameRequired'), 'error'); return }
    try {
      if (editingAddress) {
        await App.UpdatePackServer(packId, editingAddress, formName.trim(), address)
        toast(t('pack.servers.updated'), 'success')
      } else {
        await App.AddPackServer(packId, formName.trim(), address)
        toast(t('pack.servers.added'), 'success')
      }
      editorOpen = false; await load(); onServersChanged()
    } catch (e: any) { toast(t('error.withMessage', { message: e }), 'error') }
  }

  // ── Модалка «Копіювати в іншу збірку» ──
  let copyModalOpen = $state(false)
  let copySources = $state<InstanceServer[]>([])
  let copyTargetId = $state('')
  let copyQuery = $state('')
  let copyExisting = $state<Set<string>>(new Set())
  let copyChecking = $state(false)

  interface CopyTarget {
    id: string; name: string; color: string; icon?: string; iconUrl?: string
    kind: BuildKind; loaderType: string; mcVersion: string
  }

  const copyTargets = $derived.by((): CopyTarget[] =>
    (allBuilds ?? [])
      .filter((b: BuildView) => b.build.id !== packId)
      .map((b: BuildView) => ({
        id: b.build.id, name: b.build.name, color: b.build.color || 'var(--orange)',
        icon: b.build.icon, iconUrl: b.build.iconUrl, kind: b.build.kind,
        loaderType: b.build.loaderType, mcVersion: b.build.mcVersion,
      }))
  )

  const filteredCopyTargets = $derived.by((): CopyTarget[] => {
    const q = copyQuery.trim().toLowerCase()
    return q ? copyTargets.filter(c => c.name.toLowerCase().includes(q)) : copyTargets
  })

  const copyAllowedIds = $derived.by(() => {
    const s = new Set<string>()
    for (const b of allBuilds ?? []) { if (b.build.id !== packId) s.add(b.build.id) }
    return s
  })

  function toggleSelected(address: string) {
    const next = new Set(selected)
    if (next.has(address)) next.delete(address); else next.add(address)
    selected = next
  }

  function openBatchCopy() {
    const list = servers.filter(s => selected.has(s.address))
    if (!list.length) { toast(t('pack.servers.copySelectFirst'), 'error'); return }
    openCopyModal(list)
  }

  function openCopyModal(list: InstanceServer[]) {
    copySources = list; copyTargetId = ''; copyQuery = ''; copyExisting = new Set(); copyModalOpen = true
  }

  async function selectCopyTarget(id: string) {
    copyTargetId = id; copyChecking = true
    try {
      const list = await App.ListPackServers(id)
      copyExisting = new Set(list.map(s => s.address))
    } catch { copyExisting = new Set() }
    copyChecking = false
  }

  const copyCount = $derived(copySources.length)

  async function submitCopy() {
    const addrs = copySources.map(s => s.address)
    if (!addrs.length || !copyTargetId) return
    const target = copyTargets.find(p => p.id === copyTargetId)
    const targetName = target?.name ?? copyTargetId
    try {
      const results = await App.CopyServersToPack(packId, addrs, copyTargetId)
      const copied = results.filter(r => r.copied)
      const errs = results.filter(r => !r.copied)
      const fresh = copied.filter(r => !r.exists).length
      const ex = copied.length - fresh
      copyModalOpen = false; selected = new Set(); await load()
      if (errs.length) {
        toast(t('pack.servers.copyDoneMany', { name: targetName, ok: fresh, ex }) + ' — ' + errs[0].error, 'error')
      } else {
        toast(t('pack.servers.copyDoneMany', { name: targetName, ok: fresh, ex }), 'success')
      }
    } catch (e: any) { toast(t('error.withMessage', { message: e }), 'error') }
  }
</script>

<!-- Sticky-панель інструментів — ТОЙ САМИЙ патерн, що .mods-toolbar у
     менеджері модів: рядок пошуку + фільтр-чіпи + select сортування
     зверху, окремий рядок дій (ping/copy/add) знизу. Раніше тут був
     власний стиль із двома довільними .toolbar-рядами. -->
<div class="mods-toolbar" style={`--mt-top:${stickyTop}px`}>
  <div class="mt-group mt-group-main">
    <div class="mt-search">
      <i class="ti ti-search"></i>
      <input bind:value={query} placeholder={t('pack.servers.search')} />
    </div>
    <div class="pss-chips">
      <button class="pss-chip" class:active={statusFilter === 'all'} onclick={() => statusFilter = 'all'}><i class="ti ti-server-2"></i> {t('pack.servers.filterAll')}</button>
      <button class="pss-chip" class:active={statusFilter === 'online'} onclick={() => statusFilter = 'online'}><span class="dot on"></span> {t('pack.servers.filterOnline')}</button>
      <button class="pss-chip" class:active={statusFilter === 'offline'} onclick={() => statusFilter = 'offline'}><span class="dot off"></span> {t('pack.servers.filterOffline')}</button>
    </div>
    <div class="mt-spacer"></div>
    <span class="pss-count">{filtered.length} / {servers.length}</span>
    <SelectField
      value={sortBy}
      options={[
        { value: 'order',   label: t('pack.servers.sortOrder') },
        { value: 'name',    label: t('pack.servers.sortName') },
        { value: 'status',  label: t('pack.servers.sortStatus') },
        { value: 'players', label: t('pack.servers.sortPlayers') },
      ]}
      onchange={(v: string) => sortBy = v as 'order' | 'name' | 'status' | 'players'}
      minWidth="150px"
      ariaLabel={t('pack.servers.sortOrder')}
    />
  </div>
  <div class="mt-group mt-group-actions">
    <button class="btn btn-ghost btn-sm" onclick={pingAll} disabled={pingingAll || !servers.length}>
      <i class="ti {pingingAll ? 'ti-loader spin' : 'ti-refresh'}"></i> {pingingAll ? t('pack.servers.pinging') : t('pack.servers.pingAll')}
    </button>
    <button class="btn btn-ghost btn-sm" onclick={openBatchCopy} disabled={!servers.length} title={t('pack.servers.copyTo')}>
      <i class="ti ti-copy"></i> {t('pack.servers.copyTo')}
      {#if selected.size}<span class="srv-sel-count">{selected.size}</span>{/if}
    </button>
    <button class="btn btn-primary btn-sm" onclick={openAddModal}><i class="ti ti-plus"></i> {t('pack.servers.add')}</button>
  </div>
</div>

{#if loading}
  <div class="srv-empty"><i class="ti ti-loader spin"></i></div>
{:else if !servers.length}
  <div class="srv-empty">
    <i class="ti ti-server-2"></i>
    <div>{t('pack.servers.empty')}</div>
  </div>
{:else if !filtered.length}
  <div class="srv-empty">
    <i class="ti ti-search-off"></i>
    <div>{t('pack.servers.noResultsFor', { query })}</div>
  </div>
{:else}
  {#each filtered as s, i (s.address)}
    {@const pr = pings[s.address]}
    {@const isDragging = dragAddress === s.address}
    {@const isDropTarget = dragOverAddress === s.address && dragAddress !== s.address}
    <div
      class="srv-card rise-in"
      class:dragging={isDragging}
      class:drop-before={isDropTarget && dragOverPos === 'before'}
      class:drop-after={isDropTarget && dragOverPos === 'after'}
      class:sel={selected.has(s.address)}
      style="--drop-c:var(--orange);animation-delay:{Math.min(i,10)*22}ms"
      draggable="true"
      ondragstart={(e) => onDragStart(e, s.address)}
      ondragover={(e) => onDragOverCard(e, s.address)}
      ondrop={onDrop}
      ondragend={onDragEnd}
      animate:flip={{ duration: 220 }}
    >
      <!-- Левий акцент-бар (як glow у гардеробі) -->
      <div class="srv-card-accent"></div>

      <!-- Чекбокс вибору (батч-копія) — кастомний, зліва на картці. -->
      <button
        type="button"
        role="checkbox"
        aria-checked={selected.has(s.address)}
        class="srv-check"
        class:checked={selected.has(s.address)}
        onclick={(e: any) => { e.stopPropagation(); toggleSelected(s.address) }}
        title={t('pack.servers.selectForCopy')}
      >
        <i class="ti ti-check"></i>
      </button>

      <!-- Favicon / іконка -->
      <div class="srv-fav">
        {#if s.icon}
          <img src={s.icon} alt="" />
        {:else if pr?.favicon}
          <img src={pr.favicon} alt="" />
        {:else}
          <i class="ti ti-server-2"></i>
        {/if}
      </div>

      <!-- Основна інфа -->
      <div class="srv-info">
        <div class="srv-name">
          <span class="srv-name-text">{s.name || s.address}</span>
          {#if s.official}<span class="chip on">{t('pack.servers.official')}</span>{/if}
          <!-- Тимчасовий сервер (P2P-кімната): лаунчер прибере його сам,
               щойно доступу не стане — про це треба чесно сказати тут. -->
          {#if s.temp}
            <span class="chip temp" title={t('servers.tempServer.hint')}>
              <i class="ti ti-clock-play"></i> {t('servers.tempServer.badge')}
            </span>
          {/if}
          {#if s.pinned}<span class="chip pin"><i class="ti ti-pin"></i></span>{/if}
        </div>
        <div class="srv-addr">{s.address}</div>
        {#if pr?.online && pr.motd}
          <div class="srv-motd" title={stripMotdCodes(pr.motd)}>{#each parseMotdSegments(pr.motd) as seg}<span style="color:{seg.color ?? 'inherit'}; font-weight:{seg.bold ? 700 : 'inherit'}; font-style:{seg.italic ? 'italic' : 'normal'}; text-decoration:{[seg.underline && 'underline', seg.strikethrough && 'line-through'].filter(Boolean).join(' ') || 'none'}">{seg.text}</span>{/each}</div>
        {/if}
      </div>

      <!-- Статус онлайн -->
      <div class="srv-ping">
        {#if pr === null}
          <i class="ti ti-loader spin srv-ping-spin"></i>
        {:else if pr?.online}
          <div class="srv-ping-on">
            <span class="dot on"></span>
            <span class="srv-ping-count">{pr.players ?? 0}<span class="srv-ping-max">/{pr.maxPlayers ?? 0}</span></span>
          </div>
        {:else if pr}
          <div class="srv-ping-off"><span class="dot off"></span><span>{t('pack.servers.offline')}</span></div>
        {/if}
      </div>

      <!-- Action-кнопки: завжди видимі справа -->
      <div class="srv-actions">
        <button class="srv-action-btn primary" onclick={() => playOn(s)} disabled={disabled} title={t('pack.servers.join')}>
          <i class="ti ti-player-play"></i>
        </button>
        <button class="srv-action-btn" onclick={() => openEditModal(s)} title={t('pack.servers.edit')}>
          <i class="ti ti-pencil"></i>
        </button>
        <button class="srv-action-btn" onclick={() => openCopyModal([s])} title={t('pack.servers.copyTo')}>
          <i class="ti ti-copy"></i>
        </button>
        <button class="srv-action-btn" class:pin-on={s.pinned} onclick={() => togglePin(s)} title={s.pinned ? t('pack.servers.unpin') : t('pack.servers.pin')}>
          <i class="ti ti-pin"></i>
        </button>
        <button class="srv-action-btn danger" onclick={() => removeServer(s)} title={t('pack.servers.remove')}>
          <i class="ti ti-trash"></i>
        </button>
      </div>

      <!-- Ручка перетягування -->
      <i class="ti ti-grip-vertical srv-grip"></i>
    </div>
  {/each}
{/if}

<!-- Додати/редагувати сервер -->
{#if editorOpen}
  <UiModal
    open={true}
    size="sm"
    title={editingAddress ? t('pack.servers.editTitle') : t('pack.servers.addTitle')}
    icon="ti-server-2"
    color="var(--orange)"
    onclose={closeEditor}
  >
        <div class="field">
          <label for="srv-name">{t('pack.servers.name')}</label>
          <input id="srv-name" class="input" bind:value={formName} placeholder={t('pack.servers.namePlaceholder')} />
        </div>
        <div class="field" style="margin-top:12px">
          <label for="srv-addr">{t('pack.servers.address')}</label>
          <input id="srv-addr" class="input" bind:value={formAddress} placeholder={t('pack.servers.addressPlaceholder')} />
        </div>

        <div class="divider"></div>
        <div class="field">
          <label>{t('pack.servers.resourcePacks')}</label>
          <div class="rl-sub" style="margin-bottom:8px">{t('pack.servers.resourcePacksHint')}</div>
          <div class="seg">
            <button class:active={formResourcePolicy === 'ask'} onclick={() => formResourcePolicy = 'ask'}>{t('pack.servers.resourceAsk')}</button>
            <button class:active={formResourcePolicy === 'accept'} onclick={() => formResourcePolicy = 'accept'}>{t('pack.servers.resourceAccept')}</button>
            <button class:active={formResourcePolicy === 'reject'} onclick={() => formResourcePolicy = 'reject'}>{t('pack.servers.resourceReject')}</button>
          </div>
        </div>
    {#snippet footer()}
      <button class="btn btn-ghost" onclick={closeEditor}>{t('cancel')}</button>
      <button class="btn btn-primary" onclick={submitEditor}><i class="ti ti-device-floppy"></i> {t('pack.servers.save')}</button>
    {/snippet}
  </UiModal>
{/if}

<!-- Копіювати в іншу збірку -->
{#if copyModalOpen}
  <UiModal
    open={true}
    size="lg"
    title={t('pack.servers.copyToTitle')}
    icon="ti-copy"
    color="var(--violet)"
    onclose={() => copyModalOpen = false}
  >
        <div class="cp-src-list">
          {#each copySources as s (s.address)}
            <div class="cp-src-row">
              <div class="cp-src-ic">
                {#if s.icon}<img src={s.icon} alt="" />{:else}<i class="ti ti-server-2"></i>{/if}
              </div>
              <div class="cp-src-name">{s.name || s.address}</div>
              <div class="cp-src-addr mono muted" title={s.address}>{s.address}</div>
              {#if copyChecking}
                <i class="ti ti-loader spin" style="flex-shrink:0"></i>
              {:else if copyExisting.has(s.address)}
                <span class="chip ex">{t('pack.servers.copyExists')}</span>
              {/if}
            </div>
          {/each}
        </div>
        <div class="rl-sub" style="margin-bottom:10px">{t('pack.servers.copyToHintMany', { n: copyCount })}</div>
        <div class="cp-search"><i class="ti ti-search"></i><input bind:value={copyQuery} placeholder={t('pack.servers.search')} /></div>
        <div class="cp-list">
          {#if packPicker && packPicker.items.length > 0}
            <PackPicker
              items={packPicker.items}
              rowById={packPicker.rowById}
              folderById={packPicker.folderById}
              selected={copyTargetId}
              onselect={(id) => selectCopyTarget(id)}
              query={copyQuery}
              allowedIds={copyAllowedIds}
              subtitle={(r) => `${r.loader ?? ''} · ${r.mcVersion ?? ''}`}
              class="cp-pp"
            />
          {:else}
            {#each filteredCopyTargets as cp (cp.id)}
              {@const icon = packAsset(cp.iconUrl)}
              <button class="cp-item" class:sel={copyTargetId === cp.id} onclick={() => selectCopyTarget(cp.id)}>
                <div class="cp-ic" style="--pcol:{cp.color}">
                  {#if icon}
                    <img src={icon} alt="" loading="lazy" />
                  {:else}
                    <i class="ti {cp.icon || 'ti-puzzle'}"></i>
                  {/if}
                </div>
                <div class="cp-info">
                  <div class="cp-name">
                    <span>{cp.name}</span>
                    <span class="chip {cp.kind === 'shaurma' ? 'on' : 'g'}">{cp.kind === 'shaurma' ? t('shaurma') : t('custom')}</span>
                  </div>
                  <div class="cp-sub">{cp.loaderType} · {cp.mcVersion}</div>
                </div>
                <i class="ti ti-check cp-check"></i>
              </button>
            {:else}
              <div class="srv-empty"><i class="ti ti-search-off"></i><div>{t('pack.servers.noResultsFor', { query: copyQuery })}</div></div>
            {/each}
          {/if}
        </div>
    {#snippet footer()}
      <button class="btn btn-ghost" onclick={() => copyModalOpen = false}>{t('cancel')}</button>
      <button class="btn btn-primary" disabled={!copyTargetId || copyChecking} onclick={submitCopy}><i class="ti ti-copy"></i> {t('pack.servers.copyToConfirmMany', { n: copyCount })}</button>
    {/snippet}
  </UiModal>
{/if}

<style>
  /* ── Тулбар: рівно як .mods-toolbar у менеджері модів (дві групи в
     стовпець: пошук+фільтр+сортування, потім рядок дій праворуч). ── */
  .mods-toolbar {
    display: flex; flex-direction: column; gap: 8px;
    position: sticky; top: var(--mt-top, 0px); z-index: 18;
    margin: -4px -4px 8px; padding: 8px 4px 10px;
    background: linear-gradient(180deg,
      color-mix(in srgb, var(--bg-app) 92%, transparent) 0%,
      color-mix(in srgb, var(--bg-app) 78%, transparent) 100%);
    backdrop-filter: blur(12px); -webkit-backdrop-filter: blur(12px);
    border-radius: 0 0 var(--r-lg) var(--r-lg);
  }
  .mt-group { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  .mt-group-main { width: 100%; min-width: 0; }
  .mt-group-actions { width: 100%; justify-content: flex-end; }
  .mt-group-actions .btn { height: 36px; }
  .mt-search {
    display: flex; align-items: center; gap: 8px;
    background: var(--bg-input); border: 1px solid var(--border); border-radius: var(--r-sm);
    padding: 0 11px; height: 32px; flex: 1 1 220px; min-width: 150px;
  }
  .mt-search i { color: var(--text-dim); font-size: 14px; }
  .mt-search input { flex: 1; background: none; border: none; outline: none; color: var(--text); font-size: 12.5px; }
  .mt-spacer { flex: 1; }

  /* ── Напрямок вставки: лінія В ЗАЗОРІ між картками (зверху для before,
     знизу для after) кольором --drop-c — раніше тут була лише підсвітка
     картки (box-shadow), і було незрозуміло, куди саме встане сервер
     (скарга: «у розділі сервера немає лінії, що показує куди переміститься
     сервер»). Ободок лишаємо як підсвітку цілі, але напрямок тепер
     читається з лінії. Після дропу позицію анімує animate:flip. ── */
  .srv-card.drop-before::before,
  .srv-card.drop-after::after {
    content: '';
    position: absolute;
    left: 6px; right: 6px;
    height: 3px;
    border-radius: 3px;
    background: var(--drop-c, var(--orange));
    box-shadow: 0 0 10px color-mix(in srgb, var(--drop-c, var(--orange)) 65%, transparent);
    z-index: 6;
    pointer-events: none;
    animation: srv-drop-line-in .13s ease both;
  }
  .srv-card.drop-before::before { top: -5px; }
  .srv-card.drop-after::after { bottom: -5px; }
  @keyframes srv-drop-line-in {
    from { opacity: 0; transform: scaleX(.6); }
    to   { opacity: 1; transform: scaleX(1); }
  }
  .srv-card.drop-before, .srv-card.drop-after {
    box-shadow: 0 0 0 2px var(--drop-c, var(--orange)), 0 8px 24px color-mix(in srgb, var(--drop-c, var(--orange)) 26%, transparent);
    transform: translateY(-1px);
  }

  /* ── Картка сервера — wardrobe-style ── */
  .srv-card {
    position: relative;
    display: flex; align-items: center; gap: 12px;
    padding: 11px 14px 11px 18px;
    border-radius: var(--r-md);
    background: linear-gradient(
      135deg,
      color-mix(in srgb, var(--bg-card) 95%, transparent),
      color-mix(in srgb, var(--bg-card) 85%, transparent)
    );
    border: 1px solid var(--border);
    overflow: visible;
    transition:
      transform .2s cubic-bezier(.22,1.2,.36,1),
      border-color .15s,
      box-shadow .2s,
      background .15s,
      opacity .15s,
      margin .14s ease;
    cursor: grab;
    animation: srv-rise .22s cubic-bezier(.34,1.4,.64,1) both;
  }
  @keyframes srv-rise {
    from { opacity: 0; transform: translateY(6px) scale(.99); }
    to   { opacity: 1; transform: translateY(0) scale(1); }
  }
  .srv-card:hover {
    transform: translateY(-2px);
    border-color: var(--border-hi);
    box-shadow: 0 8px 28px rgba(0,0,0,.25);
  }
  .srv-card:hover .srv-card-accent { opacity: 1; }
  .srv-card.dragging {
    opacity: .35;
    cursor: grabbing;
    border-style: dashed;
    transform: scale(.98);
  }
  /* Картка вибрана (для батч-копії) */
  .srv-card.sel {
    border-color: rgba(var(--orange-rgb), .55);
    background: linear-gradient(135deg,
      color-mix(in srgb, var(--orange) 6%, var(--bg-card)),
      var(--bg-card)
    );
  }

  /* Лівий акцент-бар (як glow у wr2-card) */
  .srv-card-accent {
    position: absolute;
    left: 0; top: 12%; bottom: 12%;
    width: 3px;
    border-radius: 0 3px 3px 0;
    background: var(--orange);
    opacity: 0;
    transition: opacity .2s;
    box-shadow: 0 0 8px rgba(var(--orange-rgb), .6);
  }

  .srv-fav {
    width: 38px; height: 38px; min-width: 38px; border-radius: 10px;
    background: var(--bg-input-hi); display: flex; align-items: center; justify-content: center;
    font-size: 16px; overflow: hidden; color: var(--text-mute);
    border: 1px solid var(--border); flex-shrink: 0;
    transition: border-color .15s, box-shadow .2s;
  }
  .srv-card:hover .srv-fav {
    border-color: var(--border-hi);
    box-shadow: 0 2px 10px rgba(0,0,0,.2);
  }
  .srv-fav img { width: 100%; height: 100%; object-fit: cover; image-rendering: pixelated; }

  .srv-info { flex: 1; min-width: 0; }
  .srv-name { display: flex; align-items: center; gap: 7px; margin-bottom: 2px; }
  .srv-name-text { font-size: 12.5px; font-weight: 700; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .srv-addr { font-size: var(--fs-min); color: var(--text-mute); font-family: 'JetBrains Mono', monospace; }
  .srv-motd {
    margin-top: 3px; font-size: var(--fs-min); color: var(--text-mute); line-height: 1.3;
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 420px;
    opacity: .85;
  }

  /* Статус онлайн / офлайн */
  .srv-ping { display: flex; align-items: center; flex-shrink: 0; font-size: 11px; font-weight: 600; color: var(--text-mute); }
  .srv-ping-spin { color: var(--text-dim); font-size: 14px; }
  .srv-ping-on, .srv-ping-off { display: flex; align-items: center; gap: 6px; }
  .srv-ping-count { font-size: 11.5px; font-weight: 700; color: var(--text); }
  .srv-ping-max { font-weight: 400; color: var(--text-mute); }
  .dot { width: 7px; height: 7px; border-radius: 50%; flex-shrink: 0; }
  .dot.on { background: var(--green); box-shadow: 0 0 8px var(--green); }
  .dot.off { background: var(--red); }

  /* Чекбокс відбору — кастомна кнопка (не нативний input), у стилі
     action-кнопок картки: квадрат з рамкою, галочка з'являється тільки
     коли вибрано. Завжди видимий (не ховається поза hover). */
  .srv-check {
    width: 20px; height: 20px; flex-shrink: 0; margin: 0; padding: 0;
    border-radius: 6px; border: 1.5px solid var(--border-hi);
    background: var(--bg-input);
    display: flex; align-items: center; justify-content: center;
    color: transparent; cursor: pointer;
    transition: border-color .15s, background .15s, color .12s, transform .12s;
  }
  .srv-check i { font-size: 13px; line-height: 1; }
  .srv-check:hover { border-color: var(--orange); transform: scale(1.08); }
  .srv-check.checked {
    background: var(--orange); border-color: var(--orange); color: #fff;
  }
  .srv-check.checked:hover { background: color-mix(in srgb, var(--orange) 85%, #fff); }

  /* Action-кнопки — завжди видимі (Modrinth-style) */
  .srv-actions {
    display: flex; align-items: center; gap: 4px; flex-shrink: 0;
    margin-left: auto;
  }

  .srv-action-btn {
    width: 28px; height: 28px; border-radius: 8px; border: 1px solid var(--border);
    background: var(--bg-input); color: var(--text-mute); cursor: pointer;
    display: flex; align-items: center; justify-content: center; font-size: 13px;
    transition: background .12s, color .12s, border-color .12s, transform .12s;
    flex-shrink: 0;
  }
  .srv-action-btn:hover { background: var(--bg-input-hi); color: var(--text); border-color: var(--border-hi); transform: scale(1.08); }
  .srv-action-btn.primary { background: var(--orange); color: #fff; border-color: transparent; }
  .srv-action-btn.primary:hover { background: color-mix(in srgb, var(--orange) 85%, #fff); box-shadow: 0 4px 14px rgba(var(--orange-rgb),.45); }
  .srv-action-btn.primary:disabled { opacity: .5; cursor: not-allowed; transform: none; }
  .srv-action-btn.danger:hover { background: rgba(239,68,68,.15); color: var(--red); border-color: rgba(239,68,68,.4); }
  .srv-action-btn.pin-on { color: var(--orange); border-color: rgba(var(--orange-rgb),.4); }
  .srv-action-btn.pin-on i.ti-pin { transform: rotate(45deg); }

  /* Ручка перетягування */
  .srv-grip {
    color: var(--text-dim); font-size: 14px; opacity: 0; cursor: grab;
    transition: opacity .15s; flex-shrink: 0; margin-left: -4px;
  }
  .srv-card:hover .srv-grip { opacity: .6; }
  .srv-card:active .srv-grip { cursor: grabbing; }

  /* Chip-бейджі */
  .chip.pin { padding: 2px 6px; font-size: var(--fs-min); color: var(--orange); background: rgba(var(--orange-rgb),.12); border-color: rgba(var(--orange-rgb),.3); }
  .chip.temp { display: inline-flex; align-items: center; gap: 4px; padding: 2px 7px; font-size: var(--fs-min); color: #60a5fa; background: rgba(59, 130, 246, .13); border-color: rgba(59, 130, 246, .35); }

  .pss-chips { display: flex; align-items: center; gap: 4px; flex-wrap: wrap; }
  .pss-chip {
    display: inline-flex; align-items: center; gap: 6px;
    padding: 5px 11px; border-radius: 999px;
    background: var(--bg-input); border: 1px solid var(--border);
    color: var(--text-mute); font: 700 11px/1 var(--font-ui); cursor: pointer;
    transition: color .15s, border-color .15s, background .15s;
  }
  .pss-chip:hover { color: var(--text); border-color: var(--border-hi); }
  .pss-chip.active {
    color: var(--orange); border-color: rgba(var(--orange-rgb), .5);
    background: rgba(var(--orange-rgb), .12);
  }
  .pss-chip .dot { width: 7px; height: 7px; border-radius: 999px; display: inline-block; }
  .pss-count {
    font: 800 11px/1 var(--font-mono); color: var(--text-dim);
    background: var(--bg-input); border: 1px solid var(--border);
    border-radius: 999px; padding: 4px 9px; margin-right: 2px;
  }

  .srv-empty { display: flex; flex-direction: column; align-items: center; gap: 10px; padding: 50px 20px; color: var(--text-mute); font-size: 12.5px; text-align: center; }
  .srv-empty i { font-size: 28px; opacity: .5; }

  /* Лічильник вибраних */
  .srv-sel-count {
    background: var(--orange); color: #fff; border-radius: 999px;
    font-size: var(--fs-min); font-weight: 700; padding: 1px 7px; line-height: 1.5;
  }

  .field label { font-size: 12px; font-weight: 700; display: block; margin-bottom: 6px; }
  .rl-sub { font-size: 11px; color: var(--text-mute); line-height: 1.5; }
  .seg { display: flex; gap: 6px; }
  .seg button {
    flex: 1; padding: 8px 10px; border-radius: var(--r-sm); border: 1px solid var(--border);
    background: var(--bg-panel); color: var(--text-mute); font-family: inherit; font-size: 11.5px;
    font-weight: 700; cursor: pointer; transition: .15s;
  }
  .seg button.active { background: var(--orange); color: #fff; border-color: var(--orange); }
  .divider { height: 1px; background: var(--border); margin: 14px 0; }

  /* ── Модалка «Копіювати» ── */
  .cp-src-list {
    display: flex; flex-direction: column; gap: 6px; margin-bottom: 12px;
    max-height: 132px; overflow-y: auto; padding: 2px;
  }
  .cp-src-row {
    display: flex; align-items: center; gap: 10px; padding: 8px 12px;
    border-radius: var(--r-md); background: var(--bg-input); border: 1px solid var(--border);
    flex-shrink: 0; min-width: 0;
  }
  .cp-src-ic {
    width: 30px; height: 30px; min-width: 30px; border-radius: 8px; overflow: hidden;
    background: var(--bg-panel); display: flex; align-items: center; justify-content: center;
    font-size: 14px; color: var(--text-mute);
  }
  .cp-src-ic img { width: 100%; height: 100%; object-fit: cover; image-rendering: pixelated; }
  .cp-src-name { flex: 1; min-width: 0; font-size: 12.5px; font-weight: 700; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .cp-src-addr { font-size: 11px; flex-shrink: 1; min-width: 0; max-width: 40%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .chip.ex {
    padding: 2px 8px; font-size: var(--fs-min); font-weight: 700; border-radius: 6px;
    background: rgba(251,191,36,.12); border: 1px solid rgba(251,191,36,.35); color: rgb(251,191,36);
    flex-shrink: 0;
  }
  .cp-search {
    display: flex; align-items: center; gap: 8px;
    background: var(--bg-input); border: 1px solid var(--border); border-radius: var(--r-md);
    padding: 8px 12px; color: var(--text-mute); font-size: 12.5px; margin-bottom: 10px; flex-shrink: 0;
  }
  .cp-search input { flex: 1; background: transparent; border: none; color: var(--text); font-family: inherit; font-size: 12.5px; outline: none; }
  .cp-list { flex: 1; min-height: 0; overflow-y: auto; display: flex; flex-direction: column; gap: 6px; padding: 2px; }
  .cp-item {
    display: flex; align-items: center; gap: 12px; width: 100%;
    padding: 8px 10px; border: 1px solid var(--border); border-radius: var(--r-md);
    background: var(--bg-card); color: var(--text); font-family: inherit; text-align: left;
    cursor: pointer; transition: border-color .15s, background .15s;
  }
  .cp-item:hover { border-color: var(--border-hi); background: var(--hover); }
  .cp-item.sel { border-color: var(--orange); background: rgba(var(--orange-rgb),.08); }
  .cp-ic {
    width: 36px; height: 36px; min-width: 36px; border-radius: 10px;
    background: color-mix(in srgb, var(--pcol) 16%, var(--bg-input-hi));
    display: flex; align-items: center; justify-content: center; font-size: 17px;
    overflow: hidden; color: var(--pcol);
  }
  .cp-ic img { width: 100%; height: 100%; object-fit: cover; }
  .cp-info { flex: 1; min-width: 0; }
  .cp-name { display: flex; align-items: center; gap: 8px; font-size: 12.5px; font-weight: 700; white-space: nowrap; overflow: hidden; }
  .cp-name > span:first-child { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .cp-name .chip { padding: 2px 8px; font-size: var(--fs-min); flex-shrink: 0; }
  .cp-sub { font-size: var(--fs-min); color: var(--text-mute); font-family: var(--font-mono); margin-top: 2px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .cp-check { font-size: 16px; color: var(--text-dim); opacity: 0; flex-shrink: 0; transition: opacity .15s, color .15s; }
  .cp-item.sel .cp-check { opacity: 1; color: var(--orange); }
</style>
