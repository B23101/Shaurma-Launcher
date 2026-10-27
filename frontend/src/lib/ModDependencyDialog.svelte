<script lang="ts">
  // ── Дерево залежностей модів збірки ─────────────────────────────────────
  // Читає маніфести всіх jar у mods/ (Fabric/Quilt/Forge/NeoForge, вкладені
  // jar-in-jar, provides) і малює ДЕРЕВО (шарами): угорі — бібліотеки, від яких
  // залежать інші (Fabric API, Sodium…), нижче — те, що на них спирається.
  // Вузол — лише іконка мода. Наведення показує назву, версію, опис і смужки
  // кольорів модів, від яких він залежить (колір береться з іконки — його
  // рахує бекенд). Клік по моду — що зламається, якщо його прибрати.
  // Відсутні обов'язкові залежності показані «привидами» (пунктирне коло) —
  // їх можна одразу встановити/увімкнути в блоці нижче.
  import UiModal from './UiModal.svelte'
  import Portal from './Portal.svelte'
  import { t } from './i18n.svelte'
  import { App } from './wails'
  import { toast } from './toast.svelte'
  import { modIcon } from './modIcon'
  import type { ModDependencyGraph, ModGraphNode, ModGraphMissing, ModRemovalImpact } from './types'

  let {
    packId,
    packName = '',
    mcVersion = '',
    loader = '',
    onclose = () => {},
    // Викликається, коли набір модів змінився (встановили/увімкнули) — батько перечитує список.
    onchanged = () => {}
  } = $props<{
    packId: string
    packName?: string
    mcVersion?: string
    loader?: string
    onclose?: () => void
    onchanged?: () => void
  }>()

  let graph = $state<ModDependencyGraph | null>(null)
  let loading = $state(true)
  let err = $state('')
  let query = $state('')
  let showOptional = $state(true)
  let showLoose = $state(false)
  let busy = $state<Record<string, boolean>>({})
  let impact = $state<ModRemovalImpact | null>(null)

  async function load() {
    loading = true
    err = ''
    try {
      graph = await App.GetModDependencyGraph(packId)
    } catch (e: any) {
      err = String(e?.message ?? e)
    } finally {
      loading = false
    }
  }
  $effect(() => { load() })

  const nodes = $derived(graph?.nodes ?? [])
  const missing = $derived(graph?.missing ?? [])
  const cycles = $derived(graph?.cycles ?? [])
  const required = $derived(missing.filter((m: ModGraphMissing) => !m.optional))
  const optional = $derived(missing.filter((m: ModGraphMissing) => m.optional))
  const byId = $derived(new Map(nodes.map((n: ModGraphNode) => [n.id, n])))
  const nameOf = (id: string) => byId.get(id)?.displayName || id

  // ── Модель дерева ──────────────────────────────────────────────────────
  type TNode = {
    key: string
    id: string
    name: string
    version: string
    file: string
    desc: string
    loader: string
    enabled: boolean
    corrupted: boolean
    ghost: boolean            // залежності немає в збірці взагалі
    color: string
    dependents: number
    level: number
    x: number
    y: number
  }
  type TEdge = { from: string; to: string; optional: boolean; missing: boolean; constraint: string }

  const R = 24            // радіус іконки
  const PITCH = 74        // крок по горизонталі
  const ROW_H = 78        // висота рядка
  const LAYER_GAP = 52    // додатковий проміжок між шарами
  const COLS = 12         // макс. іконок у рядку (довгі шари переносяться)
  const PAD = 30

  const norm = (id: string) => id.toLowerCase().replace(/-/g, '_')

  // Колір-запасний: стабільний відтінок від ID, якщо в моду нема іконки.
  function fallbackColor(id: string): string {
    let h = 0
    for (let i = 0; i < id.length; i++) h = (h * 31 + id.charCodeAt(i)) >>> 0
    return `hsl(${h % 360} 55% 60%)`
  }

  const model = $derived.by(() => {
    const tn = new Map<string, TNode>()
    const byNorm = new Map<string, string>() // norm(id) → key
    for (const n of nodes as ModGraphNode[]) {
      const key = norm(n.id)
      if (tn.has(key)) continue
      byNorm.set(key, key)
      tn.set(key, {
        key, id: n.id, name: n.displayName || n.id, version: n.version, file: n.file,
        desc: n.description ?? '', loader: n.loader, enabled: n.enabled, corrupted: n.corrupted,
        ghost: false, color: n.color || fallbackColor(n.id), dependents: n.dependents,
        level: 0, x: 0, y: 0,
      })
    }

    const edges: TEdge[] = []
    const seen = new Set<string>()
    const push = (e: TEdge) => {
      const k = `${e.from}>${e.to}`
      if (e.from === e.to || seen.has(k)) return
      seen.add(k)
      edges.push(e)
    }
    for (const e of graph?.edges ?? []) {
      const from = byNorm.get(norm(e.from)), to = byNorm.get(norm(e.to))
      if (from && to) push({ from, to, optional: e.optional, missing: false, constraint: e.constraint })
    }
    // Бракує обов'язкової залежності: або вона є, але вимкнена (вузол існує),
    // або її нема зовсім — тоді малюємо «привида».
    for (const m of required as ModGraphMissing[]) {
      let target = byNorm.get(norm(m.id))
      if (!target) {
        target = 'ghost:' + norm(m.id)
        if (!tn.has(target)) {
          tn.set(target, {
            key: target, id: m.id, name: m.id, version: '', file: '', desc: '', loader: '',
            enabled: false, corrupted: false, ghost: true, color: fallbackColor(m.id),
            dependents: m.requiredByAll?.length ?? 1, level: 0, x: 0, y: 0,
          })
        }
      }
      for (const rb of (m.requiredByAll?.length ? m.requiredByAll : [m.requiredBy])) {
        const from = byNorm.get(norm(rb))
        if (from) push({ from, to: target, optional: false, missing: true, constraint: '' })
      }
    }
    const shownEdges = showOptional ? edges : edges.filter((e) => !e.optional)

    const deps = new Map<string, string[]>()      // key → від кого залежить
    const users = new Map<string, string[]>()     // key → хто від нього залежить
    for (const e of shownEdges) {
      ;(deps.get(e.from) ?? deps.set(e.from, []).get(e.from)!).push(e.to)
      ;(users.get(e.to) ?? users.set(e.to, []).get(e.to)!).push(e.from)
    }
    const inTree = new Set<string>()
    for (const e of shownEdges) { inTree.add(e.from); inTree.add(e.to) }
    const treeNodes = [...inTree].map((k) => tn.get(k)!).filter(Boolean)
    const loose = [...tn.values()].filter((n) => !inTree.has(n.key))

    // Рівні: 0 — ні від чого не залежить (бібліотеки), далі — 1 + max(рівень залежності).
    const memo = new Map<string, number>()
    const visiting = new Set<string>()
    const levelOf = (k: string): number => {
      const hit = memo.get(k)
      if (hit !== undefined) return hit
      if (visiting.has(k)) return 0          // цикл — рубаємо зворотне ребро
      visiting.add(k)
      let lv = 0
      for (const d of deps.get(k) ?? []) lv = Math.max(lv, levelOf(d) + 1)
      visiting.delete(k)
      memo.set(k, lv)
      return lv
    }
    for (const n of treeNodes) n.level = levelOf(n.key)

    const maxLevel = treeNodes.reduce((m, n) => Math.max(m, n.level), 0)
    const layers: TNode[][] = Array.from({ length: maxLevel + 1 }, () => [])
    for (const n of treeNodes) layers[n.level].push(n)
    for (const l of layers) l.sort((a, b) => b.dependents - a.dependents || a.name.localeCompare(b.name))

    // Мінімізуємо перетини: кілька проходів «барицентр» вниз/вгору.
    const idx = new Map<string, number>()
    const reindex = () => layers.forEach((l) => l.forEach((n, i) => idx.set(n.key, i / Math.max(1, l.length))))
    reindex()
    const bary = (n: TNode, nb: string[] | undefined) =>
      nb && nb.length ? nb.reduce((s, k) => s + (idx.get(k) ?? 0), 0) / nb.length : idx.get(n.key) ?? 0
    for (let pass = 0; pass < 4; pass++) {
      for (let i = 1; i < layers.length; i++) {
        layers[i].sort((a, b) => bary(a, deps.get(a.key)) - bary(b, deps.get(b.key)))
        reindex()
      }
      for (let i = layers.length - 2; i >= 0; i--) {
        layers[i].sort((a, b) => bary(a, users.get(a.key)) - bary(b, users.get(b.key)))
        reindex()
      }
    }

    // Координати: шар → один або кілька рядків (перенос по COLS), по центру.
    let y = PAD + R
    let maxCols = 1
    for (const l of layers) maxCols = Math.max(maxCols, Math.min(COLS, l.length))
    const W = maxCols * PITCH + PAD * 2
    layers.forEach((l, li) => {
      if (li > 0) y += LAYER_GAP
      for (let i = 0; i < l.length; i += COLS) {
        const row = l.slice(i, i + COLS)
        const rowW = row.length * PITCH
        const x0 = (W - rowW) / 2 + PITCH / 2
        row.forEach((n, j) => { n.x = x0 + j * PITCH; n.y = y })
        y += ROW_H
      }
    })
    const H = treeNodes.length ? y - ROW_H + R + PAD : 0

    // Фон шарів (тонкі підписи ліворуч): ключ → верхня межа шару.
    return { tn, edges: shownEdges, deps, users, treeNodes, loose, W, H, layers }
  })

  // Криві-лінії: від верху залежного вузла до низу вузла-залежності.
  function pathOf(e: TEdge): string {
    const a = model.tn.get(e.from)!, b = model.tn.get(e.to)!
    const x1 = a.x, y1 = a.y - R, x2 = b.x, y2 = b.y + R
    const dy = Math.max(24, Math.abs(y1 - y2) * 0.5)
    return `M ${x1} ${y1} C ${x1} ${y1 - dy}, ${x2} ${y2 + dy}, ${x2} ${y2}`
  }

  // ── Наведення / пошук ─────────────────────────────────────────────────
  let hoverKey = $state('')
  let tipPos = $state({ x: 0, y: 0 })
  let tipH = $state(0)
  const TIP_W = 310

  function enter(n: TNode, ev: Event) {
    hoverKey = n.key
    const r = (ev.currentTarget as HTMLElement).getBoundingClientRect()
    let x = r.right + 12
    if (x + TIP_W > window.innerWidth - 8) x = Math.max(8, r.left - TIP_W - 12)
    tipPos = { x, y: r.top + r.height / 2 }
  }
  const leave = () => { hoverKey = '' }

  const hovered = $derived(hoverKey ? model.tn.get(hoverKey) ?? null : null)
  const related = $derived.by(() => {
    const s = new Set<string>()
    if (!hoverKey) return s
    s.add(hoverKey)
    for (const k of model.deps.get(hoverKey) ?? []) s.add(k)
    for (const k of model.users.get(hoverKey) ?? []) s.add(k)
    return s
  })
  const q = $derived(query.trim().toLowerCase())
  const matches = (n: TNode) => !q || n.id.toLowerCase().includes(q) || n.name.toLowerCase().includes(q) || n.file.toLowerCase().includes(q)
  const dimmed = (n: TNode) => (hoverKey ? !related.has(n.key) : !matches(n))
  const edgeOn = (e: TEdge) => !!hoverKey && (e.from === hoverKey || e.to === hoverKey)

  type Rel = { node: TNode; optional: boolean; missing: boolean; constraint: string }
  const tipDeps = $derived.by<Rel[]>(() => !hoverKey ? [] : model.edges
    .filter((e) => e.from === hoverKey)
    .map((e) => ({ node: model.tn.get(e.to)!, optional: e.optional, missing: e.missing, constraint: e.constraint }))
    .filter((r) => r.node))
  const tipUsers = $derived.by<Rel[]>(() => !hoverKey ? [] : model.edges
    .filter((e) => e.to === hoverKey)
    .map((e) => ({ node: model.tn.get(e.from)!, optional: e.optional, missing: e.missing, constraint: e.constraint }))
    .filter((r) => r.node))
  const tipTop = $derived(Math.max(8, Math.min(tipPos.y - tipH / 2, (typeof window !== 'undefined' ? window.innerHeight : 800) - tipH - 8)))

  // ── Дії ───────────────────────────────────────────────────────────────
  async function installMissing(m: ModGraphMissing) {
    busy = { ...busy, [m.id]: true }
    try {
      if (m.disabledFile) {
        await App.ToggleMod(packId, m.disabledFile, true)
        toast(t('modgraph.enabled', { name: m.id }), 'success')
      } else {
        const r = await App.InstallSuggestedMod(packId, m.id, mcVersion, loader)
        if (!r.success) throw new Error(r.message || t('modgraph.installFailed'))
        toast(t('modgraph.installed', { name: m.id }), 'success')
      }
      onchanged()
      await load()
    } catch (e: any) {
      toast(String(e?.message ?? e), 'error')
    } finally {
      busy = { ...busy, [m.id]: false }
    }
  }

  async function showImpact(n: TNode) {
    if (n.ghost) return
    hoverKey = ''
    try {
      impact = await App.GetModRemovalImpact(packId, n.id)
    } catch (e: any) {
      toast(String(e?.message ?? e), 'error')
    }
  }
</script>

{#snippet ico(n: TNode, size: number)}
  <span class="mg-ico" class:ghost={n.ghost} style="--c:{n.color};--s:{size}px">
    <span class="mg-letter">{(n.name || '?').charAt(0).toUpperCase()}</span>
    {#if !n.ghost && n.file}
      <img use:modIcon={{ packId, fileName: n.file, rev: n.version }} alt="" draggable="false" />
    {/if}
  </span>
{/snippet}

<UiModal open={true} width="min(1120px, 96vw)" title={t('modgraph.title')} subtitle={packName} icon="ti-hierarchy-3" color="var(--violet-l)" {onclose}>
  <div class="mg-body">
    {#if loading && !graph}
      <div class="mg-empty"><i class="ti ti-loader spin"></i> {t('modgraph.loading')}</div>
    {:else if err}
      <div class="mg-empty mg-bad"><i class="ti ti-alert-circle"></i> {err}</div>
    {:else if nodes.length === 0}
      <div class="mg-empty"><i class="ti ti-puzzle-off"></i> {t('modgraph.noMods')}</div>
    {:else}
      <div class="mg-stats">
        <div><b>{nodes.length}</b><span>{t('modgraph.mods')}</span></div>
        <div class:warn={required.length > 0}><b>{required.length}</b><span>{t('modgraph.missingReq')}</span></div>
        <div><b>{optional.length}</b><span>{t('modgraph.missingOpt')}</span></div>
        <div class:warn={cycles.length > 0}><b>{cycles.length}</b><span>{t('modgraph.cycles')}</span></div>
      </div>

      {#if required.length === 0 && cycles.length === 0}
        <div class="mg-ok"><i class="ti ti-circle-check"></i> {t('modgraph.allGood')}</div>
      {/if}

      {#if required.length}
        <section class="mg-sec">
          <h4><i class="ti ti-alert-triangle"></i> {t('modgraph.missingTitle')}</h4>
          {#each required as m (m.id)}
            <div class="mg-miss">
              <div class="mg-miss-main">
                <code>{m.id}</code>
                <span class="mg-sub">
                  {m.disabledFile ? t('modgraph.disabledDep', { file: m.disabledFile }) : t('modgraph.neededBy')}
                  {(m.requiredByAll?.length ? m.requiredByAll : [m.requiredBy]).slice(0, 4).map(nameOf).join(', ')}
                </span>
              </div>
              <button class="btn btn-primary btn-sm" onclick={() => installMissing(m)} disabled={busy[m.id]}>
                <i class="ti {busy[m.id] ? 'ti-loader spin' : m.disabledFile ? 'ti-player-play' : 'ti-download'}"></i>
                {m.disabledFile ? t('modgraph.enable') : t('modgraph.install')}
              </button>
            </div>
          {/each}
        </section>
      {/if}

      {#if cycles.length}
        <section class="mg-sec">
          <h4><i class="ti ti-refresh-alert"></i> {t('modgraph.cyclesTitle')}</h4>
          {#each cycles as c}<div class="mg-sub">{c.map(nameOf).join(' → ')} → {nameOf(c[0])}</div>{/each}
        </section>
      {/if}

      <section class="mg-sec">
        <div class="mg-bar">
          <h4><i class="ti ti-hierarchy-3"></i> {t('modgraph.treeTitle')}</h4>
          <div class="mg-bar-r">
            <label class="mg-tgl" title={t('modgraph.showOptionalHint')}>
              <input type="checkbox" bind:checked={showOptional} /> {t('modgraph.showOptional')}
            </label>
            <div class="mg-search">
              <i class="ti ti-search"></i>
              <input placeholder={t('modgraph.search')} bind:value={query} />
            </div>
          </div>
        </div>

        {#if model.treeNodes.length === 0}
          <div class="mg-empty mg-empty-sm"><i class="ti ti-unlink"></i> {t('modgraph.noLinks')}</div>
        {:else}
          <div class="mg-canvas">
            <div class="mg-stage" style="width:{model.W}px;height:{model.H}px">
              <svg class="mg-edges" width={model.W} height={model.H} viewBox="0 0 {model.W} {model.H}" aria-hidden="true">
                <defs>
                  {#each model.edges as e, i (e.from + '>' + e.to)}
                    {@const a = model.tn.get(e.from)!}
                    {@const b = model.tn.get(e.to)!}
                    <linearGradient id="mgg{i}" gradientUnits="userSpaceOnUse" x1={a.x} y1={a.y - R} x2={b.x} y2={b.y + R}>
                      <stop offset="0" stop-color={a.color} />
                      <stop offset="1" stop-color={b.color} />
                    </linearGradient>
                  {/each}
                </defs>
                {#each model.edges as e, i (e.from + '>' + e.to)}
                  <path d={pathOf(e)} fill="none" stroke={e.missing ? 'var(--red)' : `url(#mgg${i})`}
                        class="mg-edge" class:opt={e.optional} class:miss={e.missing} class:on={edgeOn(e)}
                        class:dim={hoverKey ? !edgeOn(e) : false} />
                {/each}
              </svg>

              {#each model.treeNodes as n (n.key)}
                <button class="mg-node" class:off={!n.enabled && !n.ghost} class:ghost={n.ghost} class:bad={n.corrupted}
                        class:dim={dimmed(n)} class:hit={!!q && matches(n)} class:hot={hoverKey === n.key}
                        style="left:{n.x - R}px;top:{n.y - R}px;--c:{n.color}"
                        onmouseenter={(ev) => enter(n, ev)} onmouseleave={leave}
                        onfocus={(ev) => enter(n, ev)} onblur={leave}
                        onclick={() => showImpact(n)} aria-label={n.name}>
                  {@render ico(n, R * 2)}
                  {#if n.ghost}<i class="mg-badge ti ti-question-mark"></i>
                  {:else if !n.enabled}<i class="mg-badge off ti ti-player-pause"></i>
                  {:else if n.corrupted}<i class="mg-badge bad ti ti-alert-triangle"></i>{/if}
                </button>
              {/each}
            </div>
          </div>

          <div class="mg-legend">
            <span><i class="mg-ln"></i> {t('modgraph.legend.req')}</span>
            <span><i class="mg-ln dash"></i> {t('modgraph.legend.opt')}</span>
            <span><i class="mg-ln red"></i> {t('modgraph.legend.miss')}</span>
            <span class="mg-legend-hint"><i class="ti ti-pointer"></i> {t('modgraph.legend.hint')}</span>
          </div>
        {/if}

        {#if model.loose.length}
          <button class="mg-loose-h" onclick={() => (showLoose = !showLoose)}>
            <i class="ti {showLoose ? 'ti-chevron-down' : 'ti-chevron-right'}"></i>
            {t('modgraph.loose')} <b>{model.loose.length}</b>
          </button>
          {#if showLoose}
            <div class="mg-loose">
              {#each model.loose as n (n.key)}
                <button class="mg-node sm" class:off={!n.enabled} class:bad={n.corrupted} class:dim={dimmed(n)} class:hit={!!q && matches(n)}
                        style="--c:{n.color}"
                        onmouseenter={(ev) => enter(n, ev)} onmouseleave={leave} onfocus={(ev) => enter(n, ev)} onblur={leave}
                        onclick={() => showImpact(n)} aria-label={n.name}>
                  {@render ico(n, 36)}
                  {#if !n.enabled}<i class="mg-badge off ti ti-player-pause"></i>{/if}
                </button>
              {/each}
            </div>
          {/if}
        {/if}

        {#if optional.length}
          <div class="mg-opt-row">
            <span class="mg-lbl">{t('modgraph.optionalTitle')}</span>
            <div class="mg-chips">
              {#each optional as m (m.id)}
                <span class="mg-chip" title={(m.requiredByAll ?? [m.requiredBy]).join(', ')}>{m.id}</span>
              {/each}
            </div>
          </div>
        {/if}
      </section>
    {/if}
  </div>

  {#snippet footer()}
    <button class="btn btn-ghost" onclick={load} disabled={loading}><i class="ti ti-refresh"></i> {t('modgraph.refresh')}</button>
    <button class="btn btn-primary" onclick={() => onclose()}>{t('close')}</button>
  {/snippet}
</UiModal>

<!-- Підказка при наведенні: назва, версія, опис + смужки кольорів залежностей. -->
{#if hovered}
  <Portal>
    <div class="mgt" style="left:{tipPos.x}px;top:{tipTop}px;width:{TIP_W}px;--c:{hovered.color}" bind:clientHeight={tipH} role="tooltip">
      <div class="mgt-strip">
        {#if tipDeps.length}
          {#each tipDeps as r (r.node.key)}<i style="background:{r.node.color}" class:dash={r.optional || r.missing}></i>{/each}
        {:else}
          <i style="background:{hovered.color}"></i>
        {/if}
      </div>
      <div class="mgt-head">
        {@render ico(hovered, 40)}
        <div class="mgt-title">
          <div class="mgt-name">{hovered.name}</div>
          <div class="mgt-sub">
            {#if hovered.version}<span class="mono">{hovered.version}</span>{/if}
            {#if hovered.loader}<span class="mgt-tag">{hovered.loader}</span>{/if}
            {#if hovered.ghost}<span class="mgt-tag bad">{t('modgraph.notInstalled')}</span>
            {:else if !hovered.enabled}<span class="mgt-tag warn">{t('modgraph.off')}</span>{/if}
            {#if hovered.corrupted}<span class="mgt-tag bad">{t('modgraph.corrupted')}</span>{/if}
          </div>
        </div>
      </div>
      {#if hovered.desc}<div class="mgt-desc">{hovered.desc}</div>{/if}
      {#if hovered.file}<div class="mgt-file mono">{hovered.file}</div>{/if}

      {#if tipDeps.length}
        <div class="mgt-lbl">{t('modgraph.requires')} <b>{tipDeps.length}</b></div>
        {#each tipDeps.slice(0, 8) as r (r.node.key)}
          <div class="mgt-row" class:dim={r.optional}>
            <i class="mgt-bar" class:dash={r.optional || r.missing} style="--c:{r.node.color}"></i>
            {@render ico(r.node, 20)}
            <span class="mgt-rn">{r.node.name}</span>
            {#if r.missing}<span class="mgt-tag bad">{r.node.ghost ? t('modgraph.notInstalled') : t('modgraph.off')}</span>
            {:else if r.optional}<span class="mgt-tag">{t('modgraph.optional')}</span>
            {:else if r.constraint && r.constraint !== '*' && r.constraint !== 'будь-яка'}<span class="mgt-cs mono">{r.constraint}</span>{/if}
          </div>
        {/each}
        {#if tipDeps.length > 8}<div class="mgt-more">+{tipDeps.length - 8}</div>{/if}
      {/if}

      {#if tipUsers.length}
        <div class="mgt-lbl">{t('modgraph.requiredBy')} <b>{tipUsers.length}</b></div>
        {#each tipUsers.slice(0, 8) as r (r.node.key)}
          <div class="mgt-row" class:dim={r.optional}>
            <i class="mgt-bar" class:dash={r.optional} style="--c:{r.node.color}"></i>
            {@render ico(r.node, 20)}
            <span class="mgt-rn">{r.node.name}</span>
            {#if r.optional}<span class="mgt-tag">{t('modgraph.optional')}</span>{/if}
          </div>
        {/each}
        {#if tipUsers.length > 8}<div class="mgt-more">+{tipUsers.length - 8}</div>{/if}
      {/if}

      {#if !tipDeps.length && !tipUsers.length}
        <div class="mgt-more">{t('modgraph.noLinks')}</div>
      {/if}
    </div>
  </Portal>
{/if}

{#if impact}
  <UiModal open={true} size="sm" title={t('modgraph.impactTitle', { name: nameOf(impact.modId) })} icon="ti-radar" color="var(--orange)" onclose={() => (impact = null)}>
    <div class="mg-body">
      {#if (impact.breaks ?? []).length === 0 && (impact.degrades ?? []).length === 0}
        <div class="mg-ok"><i class="ti ti-circle-check"></i> {t('modgraph.impactNone')}</div>
      {/if}
      {#if (impact.breaks ?? []).length}
        <div class="mg-lbl bad">{t('modgraph.impactBreaks')}</div>
        <div class="mg-chips">{#each impact.breaks ?? [] as id}<span class="mg-chip bad">{nameOf(id)}</span>{/each}</div>
      {/if}
      {#if (impact.degrades ?? []).length}
        <div class="mg-lbl">{t('modgraph.impactDegrades')}</div>
        <div class="mg-chips">{#each impact.degrades ?? [] as id}<span class="mg-chip">{nameOf(id)}</span>{/each}</div>
      {/if}
    </div>
    {#snippet footer()}
      <button class="btn btn-primary" onclick={() => (impact = null)}>{t('close')}</button>
    {/snippet}
  </UiModal>
{/if}

<style>
  .mg-body { display: flex; flex-direction: column; gap: 14px; min-width: 0; }
  .mg-empty { display: flex; flex-direction: column; align-items: center; gap: 8px; padding: 30px; color: var(--text-mute); font-size: 13px; }
  .mg-empty-sm { padding: 18px; }
  .mg-empty i { font-size: 28px; }
  .mg-bad { color: var(--red); }
  .mg-ok { display: flex; gap: 8px; align-items: center; font-size: 13px; color: var(--green); }
  .mg-stats { display: grid; grid-template-columns: repeat(4, 1fr); gap: 10px; }
  .mg-stats div { border: 1px solid var(--border); border-radius: 10px; padding: 9px 12px; display: flex; flex-direction: column; }
  .mg-stats b { font-size: 20px; font-family: var(--font-mono); }
  .mg-stats span { font-size: 11px; color: var(--text-mute); }
  .mg-stats .warn { border-color: var(--red); }
  .mg-stats .warn b { color: var(--red); }
  .mg-sec { display: flex; flex-direction: column; gap: 10px; min-width: 0; }
  .mg-sec h4 { margin: 0; font-size: 13px; display: flex; gap: 7px; align-items: center; }
  .mg-miss { display: flex; justify-content: space-between; align-items: center; gap: 10px; border: 1px solid var(--border); border-radius: 9px; padding: 8px 12px; }
  .mg-miss-main { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .mg-sub { font-size: 11.5px; color: var(--text-mute); }
  .mg-chips { display: flex; flex-wrap: wrap; gap: 6px; }
  .mg-chip { font-size: 11.5px; padding: 3px 9px; border-radius: 7px; background: rgba(255,255,255,.06); border: 1px solid var(--border); }
  .mg-chip.bad { border-color: var(--red); color: var(--red); }
  .mg-lbl { font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .04em; color: var(--text-mute); }
  .mg-lbl.bad { color: var(--red); }
  .mono { font-family: var(--font-mono); }
  code { font-family: var(--font-mono); font-size: 12px; }

  /* ── панель керування деревом ── */
  .mg-bar { display: flex; align-items: center; justify-content: space-between; gap: 12px; flex-wrap: wrap; }
  .mg-bar-r { display: flex; align-items: center; gap: 12px; }
  .mg-tgl { display: inline-flex; gap: 6px; align-items: center; font-size: 12px; color: var(--text-mute); cursor: pointer; user-select: none; }
  .mg-search { position: relative; display: flex; align-items: center; }
  .mg-search i { position: absolute; left: 9px; font-size: 14px; color: var(--text-dim); pointer-events: none; }
  .mg-search input {
    width: 200px; padding: 6px 10px 6px 29px; border-radius: 9px; font-size: 12.5px;
    background: var(--bg); border: 1px solid var(--border); color: var(--text); outline: none;
    transition: border-color .15s, box-shadow .15s;
  }
  .mg-search input:focus { border-color: var(--violet-l); box-shadow: 0 0 0 3px color-mix(in srgb, var(--violet-l) 18%, transparent); }

  /* ── полотно дерева ── */
  .mg-canvas {
    overflow: auto; max-height: min(60vh, 640px); border-radius: 14px; border: 1px solid var(--border);
    background:
      radial-gradient(circle at 1px 1px, color-mix(in srgb, var(--text-mute) 22%, transparent) 1px, transparent 0) 0 0 / 22px 22px,
      color-mix(in srgb, var(--bg) 70%, transparent);
  }
  .mg-stage { position: relative; margin: 0 auto; min-width: 100%; }
  .mg-edges { position: absolute; inset: 0; pointer-events: none; overflow: visible; }
  .mg-edge { stroke-width: 2; stroke-linecap: round; opacity: .42; transition: opacity .18s, stroke-width .18s; }
  .mg-edge.opt { stroke-dasharray: 5 6; opacity: .3; }
  .mg-edge.miss { stroke-dasharray: 3 5; opacity: .75; }
  .mg-edge.on { opacity: 1; stroke-width: 3; }
  .mg-edge.dim { opacity: .07; }

  /* ── вузол-іконка ── */
  .mg-node {
    all: unset; box-sizing: border-box; position: absolute; width: 48px; height: 48px; border-radius: 14px; cursor: pointer;
    transition: transform .16s cubic-bezier(.34,1.56,.64,1), opacity .18s, filter .18s, box-shadow .18s;
    box-shadow: 0 0 0 2px color-mix(in srgb, var(--c) 55%, transparent), 0 6px 16px rgba(0,0,0,.35);
    animation: mg-pop .35s cubic-bezier(.34,1.56,.64,1) both;
  }
  .mg-node:hover, .mg-node:focus-visible, .mg-node.hot {
    transform: scale(1.14); z-index: 5;
    box-shadow: 0 0 0 3px var(--c), 0 0 22px color-mix(in srgb, var(--c) 55%, transparent), 0 8px 20px rgba(0,0,0,.45);
  }
  .mg-node.dim { opacity: .22; filter: grayscale(.6); }
  .mg-node.hit { box-shadow: 0 0 0 3px #fff, 0 0 18px var(--c); }
  .mg-node.off { filter: grayscale(.85); opacity: .6; }
  .mg-node.bad { box-shadow: 0 0 0 2px var(--red); }
  .mg-node.ghost { box-shadow: none; outline: 2px dashed var(--red); outline-offset: 2px; }
  .mg-node.sm { position: relative; width: 36px; height: 36px; border-radius: 11px; }
  @keyframes mg-pop { from { transform: scale(.6); opacity: 0; } to { transform: scale(1); opacity: 1; } }

  .mg-ico {
    position: relative; display: inline-flex; align-items: center; justify-content: center; overflow: hidden;
    width: var(--s); height: var(--s); border-radius: calc(var(--s) * .29); flex: none;
    background: linear-gradient(145deg, color-mix(in srgb, var(--c) 38%, #14141a), color-mix(in srgb, var(--c) 14%, #14141a));
  }
  .mg-ico.ghost { background: transparent; }
  .mg-letter { font-weight: 800; font-size: calc(var(--s) * .44); color: color-mix(in srgb, var(--c) 85%, #fff); }
  .mg-ico img { position: absolute; inset: 0; width: 100%; height: 100%; object-fit: cover; image-rendering: pixelated; background: #14141a; }
  .mg-badge {
    position: absolute; right: -5px; bottom: -5px; width: 18px; height: 18px; border-radius: 50%; display: grid; place-items: center;
    font-size: 11px; background: var(--red); color: #fff; border: 2px solid var(--bg);
  }
  .mg-badge.off { background: var(--orange); }
  .mg-badge.bad { background: var(--red); }

  .mg-legend { display: flex; flex-wrap: wrap; gap: 16px; align-items: center; font-size: 11.5px; color: var(--text-mute); }
  .mg-legend > span { display: inline-flex; align-items: center; gap: 7px; }
  .mg-legend-hint { margin-left: auto; }
  .mg-ln { display: inline-block; width: 24px; height: 0; border-top: 2px solid var(--violet-l); }
  .mg-ln.dash { border-top-style: dashed; }
  .mg-ln.red { border-top: 2px dashed var(--red); }

  .mg-loose-h { all: unset; cursor: pointer; display: inline-flex; gap: 6px; align-items: center; font-size: 12.5px; color: var(--text-mute); padding: 4px 2px; }
  .mg-loose-h:hover { color: var(--text); }
  .mg-loose-h b { font-family: var(--font-mono); color: var(--text); }
  .mg-loose { display: flex; flex-wrap: wrap; gap: 10px; padding: 12px; border: 1px solid var(--border); border-radius: 12px; max-height: 240px; overflow: auto; }
  .mg-opt-row { display: flex; flex-direction: column; gap: 6px; }

  /* ── підказка (в Portal — стилі глобальні через :global) ── */
  :global(.mgt) {
    position: fixed; z-index: 2000000; pointer-events: none; padding: 0 0 10px; border-radius: 14px; overflow: hidden;
    background: color-mix(in srgb, var(--bg-card, #1a1a22) 94%, #000); border: 1px solid color-mix(in srgb, var(--c) 45%, var(--border));
    box-shadow: 0 14px 40px rgba(0,0,0,.55), 0 0 0 1px rgba(255,255,255,.03), 0 0 28px color-mix(in srgb, var(--c) 22%, transparent);
    font-size: 12px; color: var(--text); animation: mgt-in .14s ease-out both;
  }
  @keyframes mgt-in { from { opacity: 0; transform: translateY(4px) scale(.98); } to { opacity: 1; transform: none; } }
  :global(.mgt-strip) { display: flex; height: 5px; gap: 2px; }
  :global(.mgt-strip i) { flex: 1; min-width: 6px; }
  :global(.mgt-strip i.dash) { background-image: repeating-linear-gradient(90deg, transparent 0 4px, rgba(0,0,0,.55) 4px 7px) !important; }
  :global(.mgt-head) { display: flex; gap: 11px; align-items: center; padding: 12px 14px 4px; }
  :global(.mgt-title) { min-width: 0; }
  :global(.mgt-name) { font-size: 14px; font-weight: 700; line-height: 1.2; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  :global(.mgt-sub) { display: flex; flex-wrap: wrap; gap: 5px; align-items: center; margin-top: 3px; color: var(--text-mute); font-size: 11px; }
  :global(.mgt-tag) { font-size: 10px; padding: 1px 6px; border-radius: 5px; background: rgba(255,255,255,.08); color: var(--text-mute); }
  :global(.mgt-tag.bad) { background: rgba(239,68,68,.16); color: var(--red); }
  :global(.mgt-tag.warn) { background: rgba(255,138,0,.16); color: var(--orange); }
  :global(.mgt-desc) { padding: 6px 14px 0; color: var(--text-mute); line-height: 1.45; display: -webkit-box; -webkit-line-clamp: 4; line-clamp: 4; -webkit-box-orient: vertical; overflow: hidden; }
  :global(.mgt-file) { padding: 6px 14px 0; color: var(--text-dim); font-size: 10.5px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  :global(.mgt-lbl) { margin: 10px 14px 4px; font-size: 10px; font-weight: 700; letter-spacing: .05em; text-transform: uppercase; color: var(--text-mute); }
  :global(.mgt-lbl b) { color: var(--text); font-family: var(--font-mono); margin-left: 3px; }
  :global(.mgt-row) { display: flex; align-items: center; gap: 8px; padding: 3px 14px; }
  :global(.mgt-row.dim) { opacity: .65; }
  :global(.mgt-bar) { width: 4px; align-self: stretch; min-height: 20px; border-radius: 3px; background: var(--c); flex: none; }
  :global(.mgt-bar.dash) { background: repeating-linear-gradient(180deg, var(--c) 0 4px, transparent 4px 7px); }
  :global(.mgt-rn) { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  :global(.mgt-cs) { font-size: 10px; color: var(--text-dim); max-width: 90px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  :global(.mgt-more) { padding: 3px 14px; color: var(--text-dim); font-size: 11px; }
  :global(.mgt .mono) { font-family: var(--font-mono); }
  :global(.mgt .mg-ico) { background: linear-gradient(145deg, color-mix(in srgb, var(--c) 38%, #14141a), color-mix(in srgb, var(--c) 14%, #14141a)); }
  :global(.mgt .mg-ico.ghost) { background: transparent; outline: 1.5px dashed var(--red); }
  :global(.mgt .mg-letter) { font-weight: 800; font-size: calc(var(--s) * .44); color: color-mix(in srgb, var(--c) 85%, #fff); }
  :global(.mgt .mg-ico img) { position: absolute; inset: 0; width: 100%; height: 100%; object-fit: cover; image-rendering: pixelated; background: #14141a; }
  :global(.mgt .mg-ico) { position: relative; display: inline-flex; align-items: center; justify-content: center; overflow: hidden; width: var(--s); height: var(--s); border-radius: calc(var(--s) * .29); flex: none; }

  @media (prefers-reduced-motion: reduce) {
    .mg-node, :global(.mgt) { animation: none; }
    .mg-node, .mg-edge { transition: none; }
  }
</style>
