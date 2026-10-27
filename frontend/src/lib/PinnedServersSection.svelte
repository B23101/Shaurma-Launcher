<script lang="ts">
  // ── Закріплені сервери (над картками на «Моїх збірках») ──────────────────
  // Wardrobe-style картки в адаптивній сітці; drag&drop з тим самим
  // «розсувним» ghost-slide індикатором, що в sidebar/wardrobe: цільова
  // картка отримує лінію-підказку (before/after) прямо на собі, а сусідні
  // картки плавно з'їжджають на нові місця через animate:flip.
  import { flip } from 'svelte/animate'
  import { t } from './i18n.svelte'
  import { packAsset } from './packAssets.svelte'
  import { parseMotdSegments, stripMotdCodes } from './motd'
  import { launchStageLabel } from './launchStages'
  import Portal from './Portal.svelte'
  import type { PinnedServerView, ServerPing, BuildView, LaunchProgress } from './types'

  let {
    servers = [] as PinnedServerView[],
    pings = {} as Record<string, ServerPing | null>,
    pinging = false,
    onCopy,
    onOpenPack,
    onPing,
    onUnpin,
    onPingAll,
    onJoin,
    onReorder = () => {},
    // Компактний вигляд (лише іконки в один ряд) — налаштування вигляду
    // з Налаштування → Вигляд (pinnedServersCompact), а НЕ перемикач у
    // шапці секції (скарга користувача: вигляд має керуватись у
    // налаштуваннях, а не в «Моїх збірках»).
    compact = false,
    builds = [] as BuildView[],
    launchProgress = {} as Record<string, LaunchProgress | null>,
  } = $props<{
    servers: PinnedServerView[]
    pings: Record<string, ServerPing | null>
    pinging: boolean
    onCopy: (address: string) => void
    onOpenPack: (packId: string) => void
    onPing: (address: string) => void
    onUnpin: (packId: string, address: string) => void
    onPingAll: () => void
    onJoin: (packId: string, address: string) => void
    onReorder?: (keys: string[]) => void
    compact?: boolean
    builds?: BuildView[]
    launchProgress?: Record<string, LaunchProgress | null>
  }>()

  const buildById = $derived<Map<string, BuildView>>(new Map(builds.map((b: BuildView) => [b.build.id, b])))

  // ── Компактний вигляд (лише іконки) ──────────────────────────────────
  // За замовчуванням — повні картки (як і було). Налаштування
  // pinnedServersCompact (Налаштування → Вигляд) ховає весь текст/MOTD/
  // бейдж і лишає лише коло іконки сервера з маленькою іконкою збірки в
  // кутку та статус-крапкою — займає значно менше місця по вертикалі,
  // зручно коли закріплено багато серверів. Раніше перемикач жив у шапці
  // секції (localStorage) — вигляд тепер керується з налаштувань.

  let openMenu = $state<string | null>(null)
  // Позиція меню при рендері через Portal (position:fixed, за координатами
  // кнопки-тригера) — без цього dropdown-menu позиціонувався б відносно
  // .pss-menu-wrap (position:relative) і, у сітці карток, ВИЇЖДЖАВ би вниз
  // ПОВЕРХ сусіднього рядка (top:calc(100% + 6px) від кнопки, а не від
  // вільного місця у вікні). Portal + fixed-координати гарантують, що меню
  // завжди в межах viewport і не перекриває сусідні картки: якщо знизу не
  // вистачає висоти — відкривається ВГОРУ від кнопки.
  let menuPos = $state<{ left: number; top: number; openUp: boolean } | null>(null)

  // Приблизна висота меню (4 пункти + роздільник) — досить точно, щоб
  // вирішити «вгору чи вниз» ДО рендеру (реальна висота відома лише
  // постфактум, а миготіння небажане).
  const MENU_EST_HEIGHT = 190
  const MENU_WIDTH = 190

  function toggleMenu(e: MouseEvent, address: string) {
    e.stopPropagation()
    if (openMenu === address) { openMenu = null; menuPos = null; return }
    const btn = e.currentTarget as HTMLElement
    const r = btn.getBoundingClientRect()
    const spaceBelow = window.innerHeight - r.bottom
    const openUp = spaceBelow < MENU_EST_HEIGHT && r.top > spaceBelow
    menuPos = {
      // Праві краї меню й кнопки збігаються (той самий вигляд, що раніше
      // давав right:0 у .pss-menu-wrap), лише координати тепер абсолютні
      // (viewport), а не відносні контейнера картки.
      left: Math.max(8, Math.min(r.right - MENU_WIDTH, window.innerWidth - MENU_WIDTH - 8)),
      top: openUp ? r.top - 6 : r.bottom + 6,
      openUp,
    }
    openMenu = address
  }

  $effect(() => {
    if (!openMenu) return
    const close = () => { openMenu = null; menuPos = null }
    document.addEventListener('click', close)
    // Меню зафіксоване координатами кнопки на момент відкриття — при
    // скролі/резайзі списку координати застаріють, тож найпростіше й
    // найнадійніше просто закрити меню (а не перераховувати позицію щокадру).
    window.addEventListener('resize', close)
    document.addEventListener('scroll', close, true)
    return () => {
      document.removeEventListener('click', close)
      window.removeEventListener('resize', close)
      document.removeEventListener('scroll', close, true)
    }
  })

  function menuAction(fn: () => void) {
    openMenu = null
    menuPos = null
    fn()
  }

  const serverKey = (ps: PinnedServerView) => ps.packId + '|' + ps.address

  // ── Drag&drop з ghost-slide (лінія-підказка НА картці, before/after) ──
  // dragOverKey + dragOverPos визначають, куди саме впаде картка — той
  // самий принцип, що dropTargetId/dropPos у sidebar.
  let dragKey = $state<string | null>(null)
  let dragOverKey = $state<string | null>(null)
  let dragOverPos = $state<'before' | 'after' | null>(null)

  function pssDragStart(e: DragEvent, key: string) {
    if (servers.length < 2) return
    dragKey = key
    if (e.dataTransfer) {
      e.dataTransfer.effectAllowed = 'move'
      e.dataTransfer.setData('text/plain', key)
    }
  }

  function pssDragOverCard(e: DragEvent, key: string) {
    if (!dragKey || dragKey === key) return
    e.preventDefault()
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
    dragOverKey = key
    dragOverPos = 'before' // картка обробляє; контейнер перевизначить у зазорі
    // Позиція відносно розміру картки (не абсолютних координат viewport) —
    // коректно за будь-якого масштабу/адаптивної сітки.
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
    const ratio = (e.clientY - rect.top) / (rect.height || 1)
    dragOverPos = ratio < 0.5 ? 'before' : 'after'
  }

  // ── Drop у ПРОМІЖКУ між картками сітки ────────────────────────────────
  // Контейнер .pss-list ловить dragover коли курсор над зазором (а не над
  // карткою): слот обчислюється за Y — рядок, за X у рядку — позиція.
  function pssGridDragOver(e: DragEvent) {
    if (!dragKey) return
    const t = e.target as HTMLElement
    if (t.closest?.('.pss-row')) return // картка сама обробляє
    e.preventDefault()
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
    const grid = e.currentTarget as HTMLElement
    const rows = Array.from(grid.querySelectorAll<HTMLElement>('.pss-row'))
    if (!rows.length) return
    let row = rows.filter(r => {
      const rect = r.getBoundingClientRect()
      return e.clientY >= rect.top && e.clientY <= rect.bottom
    })
    if (!row.length) {
      row = [...rows].sort((a, b) => {
        const ra = a.getBoundingClientRect(), rb = b.getBoundingClientRect()
        const da = Math.min(Math.abs(e.clientY - ra.top), Math.abs(e.clientY - ra.bottom))
        const db = Math.min(Math.abs(e.clientY - rb.top), Math.abs(e.clientY - rb.bottom))
        return da - db
      }).slice(0, 1)
    }
    row.sort((a, b) => a.getBoundingClientRect().left - b.getBoundingClientRect().left)
    const target = row.find(r => e.clientX < r.getBoundingClientRect().left + r.getBoundingClientRect().width / 2)
    const keyOfRow = (r: HTMLElement) => r.dataset.key ?? null
    if (target) {
      const k = keyOfRow(target)
      if (k && k !== dragKey) { dragOverKey = k; dragOverPos = 'before' }
    } else {
      const last = row[row.length - 1]
      const k = keyOfRow(last)
      if (k && k !== dragKey) { dragOverKey = k; dragOverPos = 'after' }
    }
  }

  function pssGridDrop(e: DragEvent) {
    e.preventDefault()
    const from = dragKey
    const toKey = dragOverKey
    const pos = dragOverPos
    dragKey = null
    dragOverKey = null
    dragOverPos = null
    if (!from || !toKey || from === toKey) return

    const keys = servers.map(serverKey)
    const fromIdx = keys.indexOf(from)
    const toIdx = keys.indexOf(toKey)
    if (fromIdx < 0 || toIdx < 0) return

    keys.splice(fromIdx, 1)
    const targetIdx = keys.indexOf(toKey)
    const insertAt = pos === 'before' ? targetIdx : targetIdx + 1
    keys.splice(insertAt, 0, from)

    onReorder(keys)
  }

  function pssDrop(e: DragEvent) {
    pssGridDrop(e)
  }

  function pssDragEnd() {
    dragKey = null
    dragOverKey = null
    dragOverPos = null
  }
</script>

<div class="pss">
  <div class="pss-head">
    <span class="pss-title"><i class="ti ti-pin"></i> {t('pinnedServers.title')} <span class="pss-count">{servers.length}</span></span>
    <button class="pss-refresh" onclick={() => onPingAll()} title={t('pack.servers.pingAll')}>
      <i class="ti {pinging ? 'ti-loader spin' : 'ti-refresh'}"></i>
    </button>
  </div>

  <div class="pss-list" class:pss-list-compact={compact} ondragover={pssGridDragOver} ondrop={pssGridDrop} ondragend={pssDragEnd}>
    {#each servers as ps, psi (ps.packId + '|' + ps.address)}
      {@const pr = pings[ps.address]}
      {@const packIconSrc = packAsset(ps.packIconUrl)}
      {@const st = buildById.get(ps.packId)?.status ?? 'not-installed'}
      {@const prog = buildById.get(ps.packId)?.progress?.percent ?? null}
      {@const lp = launchProgress[ps.packId] ?? null}
      {@const key = serverKey(ps)}
      {@const isDragging = dragKey === key}
      {@const isDropTarget = dragOverKey === key && dragKey !== key}

      <div
        class="pss-row"
        class:pss-compact={compact}
        class:dragging={isDragging}
        class:drop-before={isDropTarget && dragOverPos === 'before'}
        class:drop-after={isDropTarget && dragOverPos === 'after'}
        data-key={key}
        style="--pcol:{ps.packColor || 'var(--orange)'};animation-delay:{Math.min(psi, 8) * 30}ms"
        draggable={servers.length > 1}
        ondragstart={(e) => pssDragStart(e, key)}
        ondragover={(e) => pssDragOverCard(e, key)}
        ondrop={pssDrop}
        ondragend={pssDragEnd}
        title={compact ? (ps.name || ps.address) + (servers.length > 1 ? '\n' + t('pinnedServers.dragHint') : '') : (servers.length > 1 ? t('pinnedServers.dragHint') : undefined)}
        animate:flip={{ duration: 220 }}
      >
        {#if compact}
          <!-- ── Компактний вигляд: лише іконка сервера + маленька іконка
               збірки в кутку + статус-крапка. Клік відкриває те саме меню
               «⋯», що й у повній картці (усі дії доступні, просто без
               постійно видимого тексту). ── -->
          <div class="pss-c-ic">
            {#if ps.icon}
              <img src={ps.icon} alt="" />
            {:else if pr?.favicon}
              <img src={pr.favicon} alt="" />
            {:else}
              <i class="ti ti-server-2"></i>
            {/if}
            {#if pr === null}
              <span class="pss-c-dot dot-load"></span>
            {:else if pr?.online}
              <span class="pss-c-dot dot-on"></span>
            {:else if pr}
              <span class="pss-c-dot dot-off"></span>
            {/if}
            <button class="pss-c-pack" title={ps.packName} onclick={(e: any) => { e.stopPropagation(); onOpenPack(ps.packId) }}>
              {#if packIconSrc}
                <img src={packIconSrc} alt="" />
              {:else}
                <i class="ti {ps.packIcon || 'ti-puzzle'}"></i>
              {/if}
            </button>
            {#if lp && !lp.done && !lp.error}
              <span class="pss-c-state"><i class="ti ti-loader spin"></i></span>
            {:else if st === 'running'}
              <span class="pss-c-state pss-c-state-running"><i class="ti ti-player-play"></i></span>
            {:else if st === 'downloading' || st === 'updating'}
              <span class="pss-c-state"><i class="ti ti-loader spin"></i></span>
            {/if}
          </div>
          <button class="pss-c-menu-btn" onclick={(e: any) => toggleMenu(e, ps.address)} title={t('pinnedServers.menu')}>
            <i class="ti ti-dots-vertical"></i>
          </button>
          {#if openMenu === ps.address && menuPos}
            <Portal>
              <div
                class="dropdown-menu pss-dropdown pss-dropdown-portal"
                class:open-up={menuPos.openUp}
                style="left:{menuPos.left}px; top:{menuPos.top}px"
                onclick={(e: any) => e.stopPropagation()}
              >
                <div class="dd-item" onclick={() => menuAction(() => onJoin(ps.packId, ps.address))}>
                  <i class="ti ti-player-play"></i> {t('pack.servers.join')}
                </div>
                <div class="dd-item" onclick={() => menuAction(() => onCopy(ps.address))}>
                  <i class="ti ti-copy"></i> {t('pinnedServers.copyAddress')}
                </div>
                <div class="dd-item" onclick={() => menuAction(() => onOpenPack(ps.packId))}>
                  <i class="ti ti-puzzle"></i> {t('pinnedServers.openPack')}
                </div>
                <div class="dd-item" onclick={() => menuAction(() => onPing(ps.address))}>
                  <i class="ti ti-wifi"></i> {t('pinnedServers.ping')}
                </div>
                <div class="dd-sep"></div>
                <div class="dd-item danger" onclick={() => menuAction(() => onUnpin(ps.packId, ps.address))}>
                  <i class="ti ti-pin"></i> {t('pinnedServers.unpin')}
                </div>
              </div>
            </Portal>
          {/if}
        {:else}
        <!-- Лівий акцент-бар (колір пак) -->
        <div class="pss-accent"></div>

        <!-- Іконка сервера -->
        <div class="pss-ic">
          {#if ps.icon}
            <img src={ps.icon} alt="" />
          {:else if pr?.favicon}
            <img src={pr.favicon} alt="" />
          {:else}
            <i class="ti ti-server-2"></i>
          {/if}
        </div>

        <!-- Інфо -->
        <div class="pss-info">
          <div class="pss-name">
            {ps.name || ps.address}
            {#if pr === null}
              <span class="pss-dot dot-load"></span>
            {:else if pr?.online}
              <span class="pss-dot dot-on" title="{pr.players ?? 0}/{pr.maxPlayers ?? 0}"></span>
            {:else if pr}
              <span class="pss-dot dot-off"></span>
            {/if}
          </div>
          <div class="pss-addr">{ps.address}</div>
          {#if pr?.online && pr.motd}
            <div class="pss-motd" title={stripMotdCodes(pr.motd)}>{#each parseMotdSegments(pr.motd) as seg}<span style="color:{seg.color ?? 'inherit'}; font-weight:{seg.bold ? 700 : 'inherit'}; font-style:{seg.italic ? 'italic' : 'normal'}; text-decoration:{[seg.underline && 'underline', seg.strikethrough && 'line-through'].filter(Boolean).join(' ') || 'none'}">{seg.text}</span>{/each}</div>
          {/if}
        </div>

        <!-- Бейдж збірки -->
        <div class="pss-pack" title={t('pinnedServers.fromPack')} onclick={() => onOpenPack(ps.packId)}>
          {#if packIconSrc}
            <img src={packIconSrc} alt="" />
          {:else}
            <i class="ti {ps.packIcon || 'ti-puzzle'}"></i>
          {/if}
          <span>{ps.packName}</span>
        </div>

        <!-- Стан (запуск / гра / оновлення / кнопка) -->
        {#if lp && !lp.done && !lp.error}
          <span class="pss-state pss-state-launching" title={t('pinnedServers.launching')}>
            <i class="ti ti-loader spin"></i>
            {t('pinnedServers.launching')}
            <span class="pss-state-stage">{launchStageLabel(lp.stage)}</span>
          </span>
        {:else if st === 'running'}
          <span class="pss-state pss-state-running" title={t('pinnedServers.inGame')}>
            <i class="ti ti-player-play"></i> {t('pinnedServers.inGame')}
          </span>
        {:else if st === 'downloading' || st === 'updating'}
          <span class="pss-state pss-state-updating" title={t('pinnedServers.updating')}>
            <i class="ti ti-loader spin"></i>
            {t('pinnedServers.updating')}
            {#if prog != null}<span class="pss-state-pct">{Math.round(prog)}%</span>{/if}
          </span>
        {:else if st === 'needs-update'}
          <button class="btn btn-secondary btn-sm" onclick={(e: any) => { e.stopPropagation(); onOpenPack(ps.packId) }} title={t('pinnedServers.needsUpdate')}>
            <i class="ti ti-refresh"></i> {t('pinnedServers.needsUpdate')}
          </button>
        {:else}
          <button class="btn btn-primary btn-sm pss-join-btn" onclick={(e: any) => { e.stopPropagation(); onJoin(ps.packId, ps.address) }} title={t('pack.servers.join')}>
            <i class="ti ti-player-play"></i> {t('pack.servers.join')}
          </button>
        {/if}

        <!-- Меню ⋯ -->
        <div class="pss-menu-wrap">
          <button class="pss-btn" onclick={(e: any) => toggleMenu(e, ps.address)} title={t('pinnedServers.menu')}>
            <i class="ti ti-dots-vertical"></i>
          </button>
          {#if openMenu === ps.address && menuPos}
            <Portal>
              <div
                class="dropdown-menu pss-dropdown pss-dropdown-portal"
                class:open-up={menuPos.openUp}
                style="left:{menuPos.left}px; top:{menuPos.top}px"
                onclick={(e: any) => e.stopPropagation()}
              >
                <div class="dd-item" onclick={() => menuAction(() => onCopy(ps.address))}>
                  <i class="ti ti-copy"></i> {t('pinnedServers.copyAddress')}
                </div>
                <div class="dd-item" onclick={() => menuAction(() => onOpenPack(ps.packId))}>
                  <i class="ti ti-puzzle"></i> {t('pinnedServers.openPack')}
                </div>
                <div class="dd-item" onclick={() => menuAction(() => onPing(ps.address))}>
                  <i class="ti ti-wifi"></i> {t('pinnedServers.ping')}
                </div>
                <div class="dd-sep"></div>
                <div class="dd-item danger" onclick={() => menuAction(() => onUnpin(ps.packId, ps.address))}>
                  <i class="ti ti-pin"></i> {t('pinnedServers.unpin')}
                </div>
              </div>
            </Portal>
          {/if}
        </div>
        {/if}
      </div>
    {/each}
  </div>
</div>

<style>
  .pss { margin-bottom: 16px; }
  .pss-head { display: flex; align-items: center; gap: 10px; margin-bottom: 10px; }
  .pss-title {
    display: flex; align-items: center; gap: 7px;
    font-size: 12px; font-weight: 700; color: var(--text-mute); letter-spacing: .2px;
  }
  .pss-title i { color: var(--orange); font-size: 14px; }
  .pss-count {
    font-family: var(--font-mono); font-size: 10px; background: var(--bg-input-hi);
    color: var(--text-mute); border-radius: 999px; padding: 1px 7px;
  }
  .pss-refresh {
    border: none; background: transparent; color: var(--text-dim); cursor: pointer;
    width: 24px; height: 24px; border-radius: 6px; display: flex; align-items: center;
    justify-content: center; font-size: 14px; transition: color .15s, background .15s;
  }
  .pss-refresh:hover { color: var(--orange); background: rgba(var(--orange-rgb), .08); }

  /* Сітка карток: адаптивна, 1→2→3 колонки (як у тайлах збірок) */
  .pss-list {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(320px, 100%), 1fr));
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 10px;
    align-items: start;
  }
  /* Компактний вигляд: маленькі квадратні плитки замість 2-колонкових
     карток — auto-fill стільки, скільки влізе, замість фіксованих 2. */
  .pss-list.pss-list-compact {
    grid-template-columns: repeat(auto-fill, 56px);
    gap: 8px;
  }

  /* ── Картка сервера (wardrobe-style) ── */
  .pss-row {
    position: relative;
    display: flex; align-items: center; gap: 10px;
    padding: 10px 12px 10px 16px;
    border-radius: var(--r-md);
    background: linear-gradient(
      140deg,
      color-mix(in srgb, var(--bg-card) 92%, var(--pcol)),
      color-mix(in srgb, var(--bg-card) 98%, transparent)
    );
    border: 1px solid var(--border);
    overflow: visible;
    transition:
      transform .2s cubic-bezier(.22,1.2,.36,1),
      border-color .15s,
      box-shadow .2s,
      opacity .15s,
      margin .14s ease;
    animation: pss-rise .22s cubic-bezier(.34,1.4,.64,1) both;
  }
  @keyframes pss-rise {
    from { opacity: 0; transform: translateY(5px); }
    to   { opacity: 1; transform: translateY(0); }
  }
  .pss-row:hover {
    transform: translateY(-2px);
    border-color: var(--border-hi);
    box-shadow: 0 8px 24px rgba(0,0,0,.22);
  }
  .pss-row:hover .pss-accent { opacity: 1; }
  .pss-row:hover .pss-join-btn { opacity: 1; }
  .pss-row[draggable="true"] { cursor: grab; }
  .pss-row[draggable="true"]:active { cursor: grabbing; }
  .pss-row.dragging {
    opacity: .35;
    border-style: dashed;
    transform: scale(.98);
  }

  /* ── Напрямок вставки: лінія В ЗАЗОРІ між картками ────────────────────
     (вертикальна лінія зверху для before, знизу для after) кольором збірки
     --pcol — раніше ободок був однаковий для before/after, і було
     незрозуміло, куди саме встане сервер. Після дропу позицію анімує
     animate:flip. ── */
  .pss-row.drop-before::before,
  .pss-row.drop-after::after {
    content: '';
    position: absolute;
    left: 4px; right: 4px;
    height: 3px;
    border-radius: 3px;
    background: var(--pcol, var(--orange));
    box-shadow: 0 0 10px color-mix(in srgb, var(--pcol, var(--orange)) 65%, transparent);
    z-index: 6;
    pointer-events: none;
    animation: pss-drop-line-in .13s ease both;
  }
  .pss-row.drop-before::before { top: -6px; }
  .pss-row.drop-after::after { bottom: -6px; }
  /* Компактний вигляд — плитки стоять в ОДИН ГОРИЗОНТАЛЬНИЙ ряд: лінія
     вставки там має бути вертикальною (ліворуч для before, праворуч для
     after), а не горизонтальною зверху/знизу (скарга: «лінія показує верх
     вниз, хоча коли лише іконка — все в 1 ряд»). */
  .pss-list.pss-list-compact .pss-row.drop-before::before,
  .pss-list.pss-list-compact .pss-row.drop-after::after {
    left: auto; right: auto;
    top: 4px; bottom: 4px;
    width: 3px; height: auto;
  }
  .pss-list.pss-list-compact .pss-row.drop-before::before { left: -6px; }
  .pss-list.pss-list-compact .pss-row.drop-after::after { right: -6px; }
  /* Вертикальна лінія анімується по вертикалі (scaleY), а не горизонтально. */
  .pss-list.pss-list-compact .pss-row.drop-before::before,
  .pss-list.pss-list-compact .pss-row.drop-after::after {
    animation-name: pss-drop-line-in-v;
  }
  @keyframes pss-drop-line-in-v {
    from { opacity: 0; transform: scaleY(.6); }
    to   { opacity: 1; transform: scaleY(1); }
  }
  @keyframes pss-drop-line-in {
    from { opacity: 0; transform: scaleX(.6); }
    to   { opacity: 1; transform: scaleX(1); }
  }

  /* Лівий акцент-бар у кольорі збірки */
  .pss-accent {
    position: absolute;
    left: 0; top: 15%; bottom: 15%;
    width: 3px; border-radius: 0 3px 3px 0;
    background: var(--pcol);
    opacity: 0;
    transition: opacity .18s;
    box-shadow: 0 0 8px var(--pcol);
  }

  .pss-ic {
    width: 34px; height: 34px; min-width: 34px; border-radius: 9px; overflow: hidden;
    background: color-mix(in srgb, var(--pcol) 16%, var(--bg-input-hi));
    display: flex; align-items: center; justify-content: center;
    font-size: 16px; color: var(--pcol); border: 1px solid var(--border);
    transition: border-color .15s, box-shadow .2s; flex-shrink: 0;
  }
  .pss-row:hover .pss-ic { border-color: color-mix(in srgb, var(--pcol) 50%, transparent); box-shadow: 0 2px 8px rgba(0,0,0,.18); }
  .pss-ic img { width: 100%; height: 100%; object-fit: cover; image-rendering: pixelated; }

  /* ── Компактна плитка: квадрат 56×56, лише іконка сервера + маленька
     іконка збірки в правому нижньому кутку + статус-крапка у верхньому
     лівому. Меню «⋯» з'являється тільки при hover (щоб не займати
     місце постійно на такій маленькій плитці). ── */
  .pss-row.pss-compact {
    width: 56px; height: 56px; padding: 0; gap: 0;
    align-items: stretch; justify-content: stretch;
    overflow: visible;
  }
  .pss-row.pss-compact:hover { transform: translateY(-1px); }
  .pss-c-ic {
    position: relative; width: 100%; height: 100%; border-radius: 12px; overflow: hidden;
    background: color-mix(in srgb, var(--pcol) 16%, var(--bg-input-hi));
    display: flex; align-items: center; justify-content: center;
    font-size: 20px; color: var(--pcol); border: 1px solid var(--border);
    transition: border-color .15s, box-shadow .2s;
  }
  .pss-row.pss-compact:hover .pss-c-ic { border-color: color-mix(in srgb, var(--pcol) 55%, transparent); box-shadow: 0 4px 14px rgba(0,0,0,.25); }
  .pss-c-ic img { width: 100%; height: 100%; object-fit: cover; image-rendering: pixelated; }

  /* Статус-крапка (онлайн/офлайн/завантаження) — верхній лівий кут */
  .pss-c-dot {
    position: absolute; top: 4px; left: 4px; width: 8px; height: 8px; border-radius: 50%;
    border: 1.5px solid var(--bg-card); z-index: 2;
  }

  /* Маленька іконка збірки — нижній правий кут, поверх іконки сервера */
  .pss-c-pack {
    position: absolute; right: -3px; bottom: -3px; width: 20px; height: 20px;
    border-radius: 6px; overflow: hidden; padding: 0;
    background: color-mix(in srgb, var(--pcol) 25%, var(--bg-card));
    border: 1.5px solid var(--bg-card); cursor: pointer; z-index: 3;
    display: flex; align-items: center; justify-content: center;
    font-size: 10px; color: var(--pcol);
    box-shadow: 0 2px 6px rgba(0,0,0,.3);
    transition: transform .15s;
  }
  .pss-c-pack:hover { transform: scale(1.15); }
  .pss-c-pack img { width: 100%; height: 100%; object-fit: cover; }

  /* Індикатор запуску/гри/оновлення — поверх іконки, напівпрозорий фон */
  .pss-c-state {
    position: absolute; inset: 0; border-radius: 12px;
    display: flex; align-items: center; justify-content: center;
    background: rgba(0,0,0,.45); color: #fff; font-size: 16px; z-index: 4;
  }
  .pss-c-state-running { color: var(--green, #4caf50); }

  /* Кнопка меню «⋯» — з'являється лише при hover плитки, у верхньому
     правому куті поверх іконки. */
  .pss-c-menu-btn {
    position: absolute; top: 3px; right: 3px; width: 18px; height: 18px;
    border: none; border-radius: 5px; cursor: pointer;
    background: rgba(0,0,0,.55); color: #fff;
    display: flex; align-items: center; justify-content: center;
    font-size: 11px; opacity: 0; transition: opacity .15s, background .15s;
    z-index: 5;
  }
  .pss-row.pss-compact:hover .pss-c-menu-btn { opacity: 1; }
  .pss-c-menu-btn:hover { background: var(--orange); }

  .pss-info { flex: 1; min-width: 0; }
  .pss-name { display: flex; align-items: center; gap: 7px; font-size: 12.5px; font-weight: 700; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .pss-addr { font-size: 10.5px; color: var(--text-mute); font-family: var(--font-mono); margin-top: 1px; }
  .pss-motd {
    margin-top: 3px; font-size: 11px; color: var(--text-mute); line-height: 1.35;
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 420px;
    opacity: .85;
  }

  .pss-dot { width: 7px; height: 7px; border-radius: 50%; flex-shrink: 0; }
  .dot-on  { background: var(--green); box-shadow: 0 0 6px var(--green); }
  .dot-off { background: var(--red); }
  .dot-load { background: var(--text-dim); animation: pss-blink 1s ease-in-out infinite; }
  @keyframes pss-blink { 0%,100% { opacity: 1; } 50% { opacity: .3; } }

  /* Бейдж збірки */
  .pss-pack {
    display: flex; align-items: center; gap: 6px;
    padding: 4px 9px; border-radius: 999px; cursor: pointer;
    background: color-mix(in srgb, var(--pcol) 12%, var(--bg-input));
    border: 1px solid color-mix(in srgb, var(--pcol) 30%, transparent);
    font-size: 10.5px; font-weight: 700; color: var(--text); flex-shrink: 0;
    max-width: 160px; transition: border-color .15s, background .15s;
  }
  .pss-pack:hover { background: color-mix(in srgb, var(--pcol) 22%, var(--bg-input)); }
  .pss-pack img { width: 14px; height: 14px; border-radius: 4px; object-fit: cover; flex-shrink: 0; }
  .pss-pack i { font-size: 13px; color: var(--pcol); flex-shrink: 0; }
  .pss-pack span { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

  /* Кнопка «Приєднатись» (з'являється при hover) */
  .pss-join-btn { opacity: .75; transition: opacity .15s, box-shadow .15s !important; flex-shrink: 0; }
  .pss-row:hover .pss-join-btn { opacity: 1; }

  /* Стани */
  .pss-state {
    display: flex; align-items: center; gap: 6px; flex-shrink: 0;
    font-size: 12px; padding: 6px 10px; border-radius: 6px; white-space: nowrap;
  }
  .pss-state-running  { color: var(--green, #4caf50); background: color-mix(in srgb, var(--green, #4caf50) 12%, transparent); }
  .pss-state-updating { color: var(--text-mute); background: var(--hover); }
  .pss-state-launching { color: var(--orange); background: rgba(var(--orange-rgb), .1); }
  .pss-state-stage {
    font-family: var(--font-mono); font-size: 10px; opacity: .75;
    background: rgba(255,255,255,.07); border-radius: 4px; padding: 1px 5px;
  }
  .pss-state-pct { opacity: .7; }

  /* Кнопка меню */
  .pss-menu-wrap { position: relative; flex-shrink: 0; }
  .pss-btn {
    border: none; background: transparent; color: var(--text-dim); cursor: pointer;
    width: 26px; height: 26px; border-radius: 7px; display: flex; align-items: center;
    justify-content: center; font-size: 15px; transition: background .15s, color .15s;
    opacity: .5;
  }
  .pss-row:hover .pss-btn { opacity: 1; }
  .pss-btn:hover { background: var(--hover); color: var(--text); }
  .pss-dropdown { min-width: 190px; }
  /* Меню рендериться через Portal у document.body з position:fixed —
     координати (left/top) обчислені в toggleMenu() відносно кнопки, тому
     тут ПОВНІСТЮ перекриваємо position:absolute/top/right глобального
     .dropdown-menu (інакше воно позиціонувалось би відносно .pss-menu-wrap
     і виїжджало б поверх сусідньої картки в сітці — саме той баг, що це
     виправлення усуває). z-index високий — меню завжди поверх усього
     списку закріплених серверів і карток збірок під ним. */
  .pss-dropdown-portal {
    position: fixed !important;
    right: auto !important;
    z-index: 9000;
  }
  /* Розкриття вгору: top у toggleMenu() — це координата НИЗУ меню (r.top),
     тож зсуваємо весь блок вгору на власну висоту через translateY. */
  .pss-dropdown-portal.open-up {
    transform: translateY(-100%);
  }
</style>