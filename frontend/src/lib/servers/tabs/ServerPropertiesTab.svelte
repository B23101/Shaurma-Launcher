<script lang="ts">
  // ── Властивості сервера (server.properties) ────────────────────────────
  //
  // Що було не так (і що виправлено):
  //   • Схема бекенду знала ~38 ключів із 69 — решта (force-gamemode, log-ips,
  //     enforce-secure-profile…) малювалась ТЕКСТОВИМ полем із «true»/«false».
  //     Тепер бекенд описує всі ключі, а для чужих (моди) тип виводиться зі
  //     значення.
  //   • Усе йшло однією нескінченною сторінкою, а назви повторювались:
  //     чіпи груп + заголовки груп, повзунок + число. Тепер групи — вкладки
  //     (видно одну за раз), а кожен параметр — ОДИН рядок.
  //   • Ліміти були вигадані (view-distance від 2, op-level 1..4, порти до
  //     65535…). Тепер межі — з офіційної вікі, і поле підсвічує вихід за них
  //     ДО збереження.
  //   • Ключі, які Mojang прибрала (pvp → /gamerule), не змішуються з
  //     робочими: вони у вкладці «Не діють у цій версії».
  //
  // Порт сервера тут НЕ показується (PROP_HIDDEN): він редагується у блоці
  // «Порт» на цій сторінці, щоб не було двох полів для одного значення.
  import { untrack } from 'svelte'
  import { App } from '../../wails'
  import { toast } from '../../toast.svelte'
  import { t } from '../../i18n.svelte'
  import SelectField from '../../SelectField.svelte'
  import type { PropertiesDoc, PropEntry } from '../../types'
  import { PROP_GROUP_ORDER, PROP_GROUP_ICON, PROP_UNGROUPED, PROP_LEGACY, PROP_HIDDEN, LEGACY_ENUM_NUMBERS, isVoiceMod } from '../constants'

  let { serverId = '', running = false }: { serverId?: string; running?: boolean } = $props()

  let propsDoc = $state<PropertiesDoc | null>(null)
  let loading = $state(false)
  let loadErr = $state('')
  let saving = $state(false)
  let search = $state('')
  let activeTab = $state('')
  let revealed = $state<Record<string, boolean>>({})   // key → показати секрет
  let raw = $state<Record<string, string>>({})         // key → значення на момент завантаження
  let voiceMods = $state<string[]>([])
  let udpOn = $state(false)
  let voicePort = $state(24454)

  async function load() {
    if (loading) return
    loading = true
    loadErr = ''
    try {
      propsDoc = await App.GetServerProperties(serverId)
      raw = Object.fromEntries((propsDoc?.entries ?? []).map(e => [e.key, e.value]))
    } catch (e: any) {
      propsDoc = null
      loadErr = String(e?.message ?? e ?? '')
    } finally {
      loading = false
    }
    untrack(() => { syncVoiceState(); void detectVoiceMods() })
  }

  // untrack ОБОВ'ЯЗКОВИЙ: load() синхронно читає loading (guard) і пише
  // loading/loadErr/propsDoc. У скоупі ефекту ці читання ставали б його
  // залежностями, і ефект перезапускався б після КОЖНОЇ відповіді бекенду
  // (вічний спінер + скидання щойно введених значень).
  $effect(() => {
    const id = serverId
    if (!id) return
    propsDoc = null
    untrack(() => load())
  })

  // ── Групування ────────────────────────────────────────────────────────
  // Дубль ключа (файл з двома однаковими рядками) раніше ламав keyed-each.
  // Бекенд уже зливає дублі, але тут лишаємо страховку.
  const entries = $derived.by(() => {
    const seen = new Map<string, PropEntry>()
    for (const e of propsDoc?.entries ?? []) {
      if (e.key.startsWith('#') || PROP_HIDDEN.has(e.key)) continue
      seen.set(e.key, e)
    }
    return [...seen.values()]
  })

  function groupOf(e: PropEntry): string {
    if (e.status === 'removed' || e.status === 'new') return PROP_LEGACY
    return e.group || PROP_UNGROUPED
  }

  const byOrder = (a: PropEntry, b: PropEntry) => {
    const oa = a.order ?? 1e9, ob = b.order ?? 1e9
    return oa !== ob ? oa - ob : a.key.localeCompare(b.key)
  }

  const groups = $derived.by(() => {
    const map = new Map<string, PropEntry[]>()
    for (const e of entries) {
      const g = groupOf(e)
      if (!map.has(g)) map.set(g, [])
      map.get(g)!.push(e)
    }
    const rank = (id: string) => { const i = PROP_GROUP_ORDER.indexOf(id); return i < 0 ? PROP_GROUP_ORDER.length : i }
    return [...map.entries()]
      .map(([id, items]) => ({ id, items: items.sort(byOrder) }))
      .sort((a, b) => rank(a.id) - rank(b.id))
  })

  // Активна вкладка: якщо поточна зникла — перша наявна.
  const tab = $derived(groups.some(g => g.id === activeTab) ? activeTab : (groups[0]?.id ?? ''))

  const searching = $derived(search.trim().length > 0)

  // Пошук іде по ВСІХ групах: і за ключем, і за перекладеною назвою/поясненням —
  // «вайтлист» знаходить white-list, а не порожній екран.
  const visible = $derived.by(() => {
    const q = search.trim().toLowerCase()
    if (!q) return groups.filter(g => g.id === tab)
    return groups
      .map(g => ({ id: g.id, items: g.items.filter(e =>
        e.key.toLowerCase().includes(q) ||
        propName(e).toLowerCase().includes(q) ||
        propHint(e).toLowerCase().includes(q)) }))
      .filter(g => g.items.length > 0)
  })
  const foundCount = $derived(visible.reduce((n, g) => n + g.items.length, 0))

  // ── Зміни ─────────────────────────────────────────────────────────────
  const isDirty = (e: PropEntry) => raw[e.key] !== e.value
  const dirtyEntries = $derived(entries.filter(isDirty))
  const dirty = $derived(dirtyEntries.length > 0)
  const dirtyByGroup = $derived.by(() => {
    const m: Record<string, number> = {}
    for (const e of dirtyEntries) m[groupOf(e)] = (m[groupOf(e)] ?? 0) + 1
    return m
  })
  // Блокують збереження лише помилки у ЗМІНЕНИХ полях: старе некоректне
  // значення з файлу (напр. view-distance=2) підсвічується, але не мусить
  // забороняти зберегти щось інше.
  const blockingErrors = $derived(dirtyEntries.filter(e => fieldError(e) !== '').length)

  const currentPort = $derived(Number(propsDoc?.entries.find(e => e.key === 'server-port')?.value) || Number(raw['server-port']) || 25565)

  // ── Тексти ────────────────────────────────────────────────────────────
  function propName(e: PropEntry): string {
    const k = `props.${e.key}.name`
    const v = t(k)
    return v === k ? e.key : v
  }
  function propHint(e: PropEntry): string {
    const k = `props.${e.key}.hint`
    const v = t(k)
    return v === k ? '' : v
  }
  const cleanEnum = (v: string) => v.replace(/^minecraft\\?:/, '')

  function enumLabel(key: string, value: string): string {
    const k = `props.val.${key}.${cleanEnum(value)}`
    const v = t(k)
    return v === k ? value : v
  }

  // Значення enum із файлу → канонічний варіант зі схеми:
  //   "minecraft:flat" ≡ "minecraft\:flat", "1" ≡ "creative" (старі числа).
  function canonEnum(e: PropEntry): string | null {
    const opts = e.enum ?? []
    if (opts.includes(e.value)) return e.value
    const hit = opts.find(o => cleanEnum(o) === cleanEnum(e.value))
    if (hit) return hit
    const legacy = LEGACY_ENUM_NUMBERS[e.key]
    if (legacy && /^[0-3]$/.test(e.value.trim())) return legacy[Number(e.value.trim())]
    return null
  }

  function enumOptions(e: PropEntry) {
    const opts = (e.enum ?? []).map(o => ({ value: o, label: enumLabel(e.key, o) }))
    // Власне значення (плагін/старий файл) не повинно «зникати» з вибору.
    if (canonEnum(e) === null && e.value !== '') opts.push({ value: e.value, label: t('servers.props.custom', { v: e.value }) })
    return opts
  }
  // Сегмент — для коротких закритих списків; решта — випадаючий список.
  const useSegments = (e: PropEntry) => !e.open && (e.enum?.length ?? 0) <= 4 && canonEnum(e) !== null

  // ── Межі й типове значення ───────────────────────────────────────────
  function rangeText(e: PropEntry): string {
    if (e.type !== 'int') return ''
    const u = e.unit ? ` ${e.unit}` : ''
    const lo = e.min, hi = e.max
    if (lo !== undefined && hi !== undefined) return t('servers.props.range', { min: fmtNum(lo), max: fmtNum(hi) }) + u
    if (lo !== undefined) return t('servers.props.rangeMin', { min: fmtNum(lo) }) + u
    if (hi !== undefined) return t('servers.props.rangeMax', { max: fmtNum(hi) }) + u
    return e.unit ? e.unit : ''
  }
  const fmtNum = (n: number) => Math.abs(n) >= 10000 ? n.toLocaleString('uk-UA').replace(/\u00a0/g, ' ') : String(n)

  function defaultText(e: PropEntry): string {
    if (e.default === undefined) return ''
    const shown = e.type === 'bool'
      ? (e.default === 'true' ? t('servers.props.on') : t('servers.props.off'))
      : e.type === 'enum' ? enumLabel(e.key, e.default)
      : e.default === '' ? t('servers.props.defaultEmpty') : e.default
    return t('servers.props.default', { v: shown })
  }
  const canResetDefault = (e: PropEntry) => e.known && e.default !== undefined && e.value !== e.default && e.status !== 'removed'

  function fieldError(e: PropEntry): string {
    if (e.type !== 'int') return ''
    const v = String(e.value ?? '').trim()
    if (!/^-?\d+$/.test(v)) return t('servers.props.errInt')
    const n = Number(v)
    if (e.min !== undefined && n < e.min) return t('servers.props.errMin', { min: fmtNum(e.min) })
    if (e.max !== undefined && n > e.max) return t('servers.props.errMax', { max: fmtNum(e.max) })
    return ''
  }

  // ── Дії ───────────────────────────────────────────────────────────────
  function setValue(e: PropEntry, value: string) { e.value = value }
  function revertOne(e: PropEntry) { if (raw[e.key] !== undefined) e.value = raw[e.key] }
  function revertAll() { for (const e of entries) revertOne(e) }

  function stepInt(e: PropEntry, dir: 1 | -1) {
    const n = Number(String(e.value).trim())
    const step = e.step && e.step > 0 ? e.step : 1
    let next = (Number.isFinite(n) ? n : (e.min ?? 0)) + dir * step
    if (e.min !== undefined) next = Math.max(e.min, next)
    if (e.max !== undefined) next = Math.min(e.max, next)
    e.value = String(next)
  }

  async function save() {
    if (!propsDoc) return
    if (blockingErrors > 0) { toast(t('servers.props.errCount', { n: blockingErrors }), 'error'); return }
    // String() ОБОВ'ЯЗКОВИЙ: бінд на input[type=number]/range віддає JS-ЧИСЛО,
    // а бекенд чекає рядок.
    const updates = dirtyEntries.map(e => ({ key: e.key, value: String(e.value ?? '') }))
    if (updates.length === 0) { toast(t('servers.props.noChanges'), 'info'); return }
    saving = true
    try {
      await App.SaveServerProperties(serverId, updates)
      for (const u of updates) raw[u.key] = u.value
      toast(t('servers.props.saved'), 'success')
    } catch (e: any) {
      toast(String(e?.message ?? e), 'error')
    } finally {
      saving = false
    }
  }

  // ── Голосовий чат (UDP) ───────────────────────────────────────────────
  // Голосові моди (Simple Voice Chat / Plasmo Voice) ходять UDP-портом
  // (типово 24454) ПОВЗ Minecraft-протокол. У P2P це окремий тунель, який
  // гравець вмикає у вкладці «Мережа». Якщо мод стоїть, а тунель вимкнено —
  // голосу не буде зовсім, і жодна помилка в грі про це не скаже.
  async function syncVoiceState() {
    // Збережені UDP-налаштування беремо з бекенду (ключ — id сервера, як у віджеті P2P).
    try {
      if (serverId) {
        const pref = await App.GetP2PUDPPref(serverId)
        if (pref?.found) {
          udpOn = !!pref.enabled
          voicePort = pref.port > 0 && pref.port < 65536 ? pref.port : (currentPort || 24454)
          return
        }
      }
    } catch { /* падаємо на localStorage */ }
    udpOn = localStorage.getItem(`shaurma.p2p.udp.${serverId || currentPort}`) === '1'
    const saved = Number(localStorage.getItem(`shaurma.p2p.voiceport.${serverId || currentPort}`))
    voicePort = saved > 0 && saved < 65536 ? saved : 24454
  }

  async function detectVoiceMods() {
    if (!serverId) return
    try {
      const [mods, plugins] = await Promise.all([
        App.ListServerMods(serverId).catch(() => []),
        App.ListServerPlugins(serverId).catch(() => []),
      ])
      const all = [...(mods ?? []), ...(plugins ?? [])] as { name?: string; fileName: string; enabled?: boolean }[]
      voiceMods = all.filter(m => isVoiceMod(m.fileName) && m.enabled !== false).map(m => m.name || m.fileName)
    } catch { voiceMods = [] }
  }

  $effect(() => {
    const p = currentPort
    untrack(() => { void p; syncVoiceState() })
  })
</script>

<div class="card spt" data-testid="server-properties">
  <div class="card-head">
    <div class="ci violet"><i class="ti ti-file-settings"></i></div>
    <div class="ctw">
      <div class="ct">{t('servers.props.title')} <span class="spt-file mono">server.properties</span></div>
      <div class="cd">{t('servers.props.subtitle')}</div>
    </div>
  </div>

  {#if loading}
    <div class="spt-empty small"><i class="ti ti-loader spin"></i></div>
  {:else if !propsDoc}
    <div class="spt-empty">
      <i class="ti ti-file-settings"></i>
      <div>{t('servers.props.loadFailed')}</div>
      {#if loadErr}
        <div class="spt-errtext">{loadErr}</div>
      {:else}
        <div class="spt-dim">{t('servers.props.loadFailedSub')}</div>
      {/if}
      <button class="btn btn-ghost btn-sm" style="margin-top:10px" onclick={load}><i class="ti ti-refresh"></i> {t('servers.props.retry')}</button>
    </div>
  {:else}
    {#if running}
      <div class="spt-note warn"><i class="ti ti-player-play"></i><div>{t('servers.props.runningWarn')}</div></div>
    {/if}
    {#if voiceMods.length > 0 && !udpOn}
      <div class="spt-note warn">
        <i class="ti ti-microphone-off"></i>
        <div>{t('props.voice.body', { mods: voiceMods.join(', '), port: voicePort })}</div>
      </div>
    {/if}

    <div class="spt-bar">
      <div class="spt-search">
        <i class="ti ti-search"></i>
        <input class="input" bind:value={search} placeholder={t('servers.props.search')} spellcheck="false" />
        {#if search}
          <button class="spt-x" onclick={() => search = ''} title={t('servers.props.clearSearch')} aria-label={t('servers.props.clearSearch')}><i class="ti ti-x"></i></button>
        {/if}
      </div>
      {#if searching}
        <span class="spt-found">{t('servers.props.results', { n: foundCount })}</span>
      {/if}
    </div>

    <div class="spt-tabs" role="tablist" class:dim={searching}>
      {#each groups as g (g.id)}
        <button role="tab" aria-selected={!searching && tab === g.id} class="spt-tab" class:on={!searching && tab === g.id}
                onclick={() => { search = ''; activeTab = g.id }}>
          <i class="ti {PROP_GROUP_ICON[g.id] ?? 'ti-dots'}"></i>
          <span>{t(`props.group.${g.id}`)}</span>
          <span class="spt-tab-n">{g.items.length}</span>
          {#if dirtyByGroup[g.id]}<span class="spt-tab-dot" title={t('servers.props.dirty', { n: dirtyByGroup[g.id] })}></span>{/if}
        </button>
      {/each}
    </div>

    <div class="spt-body">
      {#if visible.length === 0}
        <div class="spt-empty small"><i class="ti ti-search-off"></i><div>{t('servers.props.nothingFound', { q: search })}</div></div>
      {/if}

      {#each visible as group (group.id)}
        <div class="spt-section">
          {#if searching}
            <div class="spt-gtitle"><i class="ti {PROP_GROUP_ICON[group.id] ?? 'ti-dots'}"></i> {t(`props.group.${group.id}`)}</div>
          {:else}
            <div class="spt-gdesc">
              {group.id === PROP_LEGACY ? t('servers.props.legacyNote', { v: propsDoc.mcVersion || '—' }) : t(`props.groupDesc.${group.id}`)}
            </div>
          {/if}

          {#each group.items as entry (entry.key)}
            {@const err = fieldError(entry)}
            {@const hint = propHint(entry)}
            {@const range = rangeText(entry)}
            {@const defText = defaultText(entry)}
            {@const canon = entry.type === 'enum' ? canonEnum(entry) : null}
            <div class="spt-row" class:dirty={isDirty(entry)} class:invalid={err !== ''} class:wide={entry.type === 'string'} class:inactive={group.id === PROP_LEGACY}>
              <div class="spt-info">
                <div class="spt-name">
                  <span>{propName(entry)}</span>
                  {#if entry.newWorldOnly}<span class="spt-badge" title={t('servers.props.newWorldOnlyHint')}><i class="ti ti-world-plus"></i> {t('servers.props.newWorldOnly')}</span>{/if}
                  {#if !entry.known}<span class="spt-badge mute">{t('servers.props.unknownKey')}</span>{/if}
                </div>
                {#if hint}<div class="spt-hint">{hint}</div>{/if}
                <div class="spt-meta">
                  <code class="spt-key">{entry.key}</code>
                  {#if range}<span class="spt-chipm">{range}</span>{/if}
                  {#if defText}
                    {#if canResetDefault(entry)}
                      <button class="spt-chipm link" title={t('servers.props.useDefault')} onclick={() => setValue(entry, entry.default ?? '')}>{defText}</button>
                    {:else}
                      <span class="spt-chipm">{defText}</span>
                    {/if}
                  {/if}
                </div>
                {#if err}<div class="spt-err"><i class="ti ti-alert-circle"></i> {err}</div>{/if}
              </div>

              <div class="spt-ctl">
                {#if entry.type === 'bool'}
                  <div
                    class="toggle" class:on={entry.value === 'true'}
                    role="switch" aria-checked={entry.value === 'true'}
                    aria-label={propName(entry)}
                    tabindex="0"
                    onclick={() => setValue(entry, entry.value === 'true' ? 'false' : 'true')}
                    onkeydown={(ev) => { if (ev.key === 'Enter' || ev.key === ' ') { ev.preventDefault(); setValue(entry, entry.value === 'true' ? 'false' : 'true') } }}
                  ></div>

                {:else if entry.type === 'enum' && useSegments(entry)}
                  <div class="spt-seg" role="radiogroup" aria-label={propName(entry)}>
                    {#each entry.enum ?? [] as opt (opt)}
                      <button role="radio" aria-checked={canon === opt} class:on={canon === opt}
                              onclick={() => setValue(entry, opt)}>{enumLabel(entry.key, opt)}</button>
                    {/each}
                  </div>

                {:else if entry.type === 'enum'}
                  <SelectField
                    value={canon ?? entry.value}
                    options={enumOptions(entry)}
                    onchange={(v) => setValue(entry, v)}
                    ariaLabel={propName(entry)}
                    minWidth="190px"
                  />

                {:else if entry.type === 'int'}
                  <div class="spt-int">
                    {#if entry.slider && entry.min !== undefined && entry.max !== undefined}
                      <input class="spt-range" type="range" min={entry.min} max={entry.max} step={entry.step ?? 1}
                             value={entry.value} aria-label={propName(entry)}
                             oninput={(ev) => setValue(entry, (ev.currentTarget as HTMLInputElement).value)} />
                    {/if}
                    <div class="spt-stepper" class:bad={err !== ''}>
                      <button tabindex="-1" aria-label="−" onclick={() => stepInt(entry, -1)}><i class="ti ti-minus"></i></button>
                      <input class="mono" type="text" inputmode="numeric" value={entry.value}
                             aria-label={propName(entry)} aria-invalid={err !== ''}
                             oninput={(ev) => setValue(entry, (ev.currentTarget as HTMLInputElement).value.trim())} />
                      <button tabindex="-1" aria-label="+" onclick={() => stepInt(entry, 1)}><i class="ti ti-plus"></i></button>
                    </div>
                  </div>

                {:else}
                  <div class="spt-text">
                    <input class="input mono" class:bad={err !== ''}
                           type={entry.secret && !revealed[entry.key] ? 'password' : 'text'}
                           autocomplete="off" spellcheck="false"
                           value={entry.value} aria-label={propName(entry)}
                           oninput={(ev) => setValue(entry, (ev.currentTarget as HTMLInputElement).value)} />
                    {#if entry.secret}
                      <button class="spt-eye" onclick={() => revealed[entry.key] = !revealed[entry.key]}
                              title={revealed[entry.key] ? t('servers.props.hide') : t('servers.props.show')}
                              aria-label={revealed[entry.key] ? t('servers.props.hide') : t('servers.props.show')}>
                        <i class="ti {revealed[entry.key] ? 'ti-eye-off' : 'ti-eye'}"></i>
                      </button>
                    {/if}
                    {#if entry.key === 'motd'}
                      <span class="spt-count" class:over={entry.value.length > 59}>{t('servers.props.motdLen', { n: entry.value.length })}</span>
                    {/if}
                  </div>
                {/if}

                {#if isDirty(entry)}
                  <button class="spt-revert" title={t('servers.props.revertOne')} aria-label={t('servers.props.revertOne')} onclick={() => revertOne(entry)}>
                    <i class="ti ti-arrow-back-up"></i>
                  </button>
                {/if}
              </div>
            </div>
          {/each}
        </div>
      {/each}
    </div>

    <div class="spt-save" class:active={dirty}>
      <button class="btn btn-primary" onclick={save} disabled={saving || !dirty || blockingErrors > 0}>
        {#if saving}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-check"></i>{/if}
        {t('servers.props.save')}
      </button>
      {#if dirty}
        <span class="spt-dirty">{t('servers.props.dirty', { n: dirtyEntries.length })}</span>
        {#if blockingErrors > 0}<span class="spt-errtext"><i class="ti ti-alert-circle"></i> {t('servers.props.errCount', { n: blockingErrors })}</span>{/if}
        <button class="btn btn-ghost btn-sm" onclick={revertAll}><i class="ti ti-arrow-back-up"></i> {t('servers.props.revertAll')}</button>
      {:else}
        <span class="spt-dim"><i class="ti ti-info-circle"></i> {t('servers.props.restartHint')}</span>
      {/if}
    </div>
  {/if}
</div>

<style>
  /* min-width:0 — без нього ряд вкладок (nowrap) розтягує flex-батька і картка
     вилазить за межі екрана замість внутрішнього горизонтального скролу. */
  .spt { padding-bottom: 0; overflow: visible; min-width: 0; max-width: 100%; }
  .spt-file { font: 500 var(--fs-min)/1 var(--font-mono); color: var(--text-dim); margin-left: 6px; padding: 2px 6px; border: 1px solid var(--border); border-radius: 5px; vertical-align: 1px; }

  /* ── Пошук і вкладки груп ── */
  .spt-bar { display: flex; align-items: center; gap: 12px; padding: 12px 22px 0; }
  .spt-search { position: relative; display: flex; align-items: center; flex: 1; max-width: 420px; }
  .spt-search i { position: absolute; left: 10px; color: var(--text-dim); font-size: 14px; pointer-events: none; }
  .spt-search input { padding: 8px 30px 8px 31px; font-size: 12px; }
  .spt-x { position: absolute; right: 6px; background: none; border: 0; color: var(--text-dim); cursor: pointer; padding: 3px; }
  .spt-x:hover { color: var(--text); }
  .spt-found { font: 600 11px/1 var(--font-ui); color: var(--text-mute); }

  .spt-tabs { min-width: 0; display: flex; gap: 6px; padding: 12px 22px 10px; overflow-x: auto; scrollbar-width: thin; border-bottom: 1px solid var(--border); }
  .spt-tabs.dim .spt-tab { opacity: .55; }
  .spt-tab {
    position: relative; flex: 0 0 auto; display: inline-flex; align-items: center; gap: 6px;
    padding: 7px 11px; border-radius: 9px; border: 1px solid var(--border); background: var(--hover);
    color: var(--text-mute); font: 600 11.5px/1 var(--font-ui); cursor: pointer;
    transition: color .14s, border-color .14s, background .14s;
  }
  .spt-tab i { font-size: 14px; }
  .spt-tab:hover { color: var(--text); border-color: var(--border-hi, var(--border)); }
  .spt-tab.on { color: var(--text); border-color: color-mix(in srgb, var(--pcol, var(--orange)) 60%, transparent); background: color-mix(in srgb, var(--pcol, var(--orange)) 13%, transparent); }
  .spt-tab.on i { color: var(--pcol, var(--orange)); }
  .spt-tab-n { font: 700 var(--fs-min)/1 var(--font-mono); color: var(--text-dim); }
  .spt-tab-dot { position: absolute; top: -3px; right: -3px; width: 8px; height: 8px; border-radius: 50%; background: var(--pcol, var(--orange)); box-shadow: 0 0 0 2px var(--bg-card, #151515); }

  /* ── Рядки параметрів ── */
  .spt-body { display: flex; flex-direction: column; }
  .spt-section { display: flex; flex-direction: column; }
  .spt-gdesc { padding: 12px 22px 4px; font: 400 11.5px/1.5 var(--font-ui); color: var(--text-mute); }
  .spt-gtitle { display: flex; align-items: center; gap: 7px; padding: 14px 22px 4px; font: 700 11px/1 var(--font-ui); text-transform: uppercase; letter-spacing: .6px; color: var(--pcol, var(--orange)); }

  .spt-row {
    position: relative; display: grid; grid-template-columns: minmax(0, 1fr) auto; align-items: center; gap: 8px 22px;
    padding: 13px 22px; border-bottom: 1px solid var(--hover);
  }
  .spt-row:last-child { border-bottom: none; }
  .spt-row::before { content: ''; position: absolute; left: 0; top: 10px; bottom: 10px; width: 3px; border-radius: 0 3px 3px 0; background: transparent; transition: background .15s; }
  .spt-row.dirty { background: color-mix(in srgb, var(--pcol, var(--orange)) 6%, transparent); }
  .spt-row.dirty::before { background: var(--pcol, var(--orange)); }
  .spt-row.invalid::before { background: var(--red); }
  .spt-row.inactive { opacity: .62; }
  .spt-row:hover { background: var(--hover); }
  .spt-row.dirty:hover { background: color-mix(in srgb, var(--pcol, var(--orange)) 9%, transparent); }
  /* Текстові поля — на всю ширину під описом, а не стиснуті праворуч. */
  .spt-row.wide { grid-template-columns: minmax(0, 1fr); }
  .spt-row.wide .spt-ctl { justify-content: stretch; }
  @media (max-width: 640px) {
    .spt-row { grid-template-columns: minmax(0, 1fr); }
    .spt-ctl { justify-content: flex-start; }
  }

  .spt-info { min-width: 0; display: flex; flex-direction: column; gap: 4px; }
  .spt-name { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; font: 650 13px/1.3 var(--font-ui); color: var(--text); }
  .spt-badge { display: inline-flex; align-items: center; gap: 4px; font: 600 var(--fs-min)/1 var(--font-ui); padding: 3px 7px; border-radius: 20px; color: var(--blue, #3b82f6); background: color-mix(in srgb, var(--blue, #3b82f6) 13%, transparent); border: 1px solid color-mix(in srgb, var(--blue, #3b82f6) 30%, transparent); }
  .spt-badge.mute { color: var(--text-mute); background: var(--hover); border-color: var(--border); }
  .spt-hint { font: 400 11.5px/1.5 var(--font-ui); color: var(--text-mute); max-width: 620px; }
  .spt-meta { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; margin-top: 1px; }
  .spt-key { font: 500 var(--fs-min)/1 var(--font-mono); color: var(--text-dim); padding: 3px 6px; border-radius: 5px; background: var(--bg-input); border: 1px solid var(--border); }
  .spt-chipm { font: 600 var(--fs-min)/1 var(--font-ui); color: var(--text-dim); padding: 3px 0; background: none; border: 0; }
  .spt-chipm.link { cursor: pointer; padding: 3px 7px; border-radius: 5px; border: 1px dashed var(--border); color: var(--text-mute); }
  .spt-chipm.link:hover { color: var(--pcol, var(--orange)); border-color: var(--pcol, var(--orange)); }
  .spt-err { display: inline-flex; align-items: center; gap: 5px; font: 600 11px/1.3 var(--font-ui); color: var(--red); }

  /* ── Контроли ── */
  .spt-ctl { display: flex; align-items: center; justify-content: flex-end; gap: 8px; min-width: 0; }
  .spt-revert { flex: 0 0 auto; background: none; border: 1px solid var(--border); border-radius: 7px; color: var(--text-dim); cursor: pointer; padding: 5px 7px; font-size: 13px; }
  .spt-revert:hover { color: var(--text); border-color: var(--border-hi, var(--border)); }

  .spt-seg { display: inline-flex; padding: 3px; gap: 2px; border-radius: 10px; background: var(--bg-input); border: 1px solid var(--border); }
  .spt-seg button { border: 0; background: none; cursor: pointer; color: var(--text-mute); font: 600 11.5px/1 var(--font-ui); padding: 7px 11px; border-radius: 7px; white-space: nowrap; transition: background .14s, color .14s; }
  .spt-seg button:hover { color: var(--text); }
  .spt-seg button.on { background: color-mix(in srgb, var(--pcol, var(--orange)) 22%, transparent); color: var(--text); box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--pcol, var(--orange)) 55%, transparent); }

  .spt-int { display: inline-flex; align-items: center; gap: 12px; }
  .spt-range { width: 160px; accent-color: var(--pcol, var(--orange)); }
  .spt-stepper { display: inline-flex; align-items: stretch; border: 1px solid var(--border); border-radius: var(--r-md, 10px); background: var(--bg-input); overflow: hidden; }
  .spt-stepper:focus-within { border-color: var(--pcol, var(--orange)); }
  .spt-stepper.bad { border-color: var(--red); }
  .spt-stepper button { border: 0; background: none; cursor: pointer; color: var(--text-dim); padding: 0 9px; font-size: 12px; }
  .spt-stepper button:hover { color: var(--text); background: var(--hover); }
  .spt-stepper input { width: 92px; border: 0; background: none; color: var(--text); text-align: center; font: 600 12px/1 var(--font-mono); padding: 8px 2px; outline: none; }

  .spt-text { position: relative; display: flex; align-items: center; gap: 6px; width: 100%; }
  .spt-text .input { padding: 8px 10px; font-size: 12px; }
  .spt-text .input.bad { border-color: var(--red); }
  .spt-eye { flex: 0 0 auto; background: none; border: 1px solid var(--border); border-radius: 7px; color: var(--text-dim); cursor: pointer; padding: 6px 8px; }
  .spt-eye:hover { color: var(--text); }
  .spt-count { flex: 0 0 auto; font: 600 var(--fs-min)/1 var(--font-mono); color: var(--text-dim); }
  .spt-count.over { color: var(--yellow); }

  /* ── Нижня панель збереження (липка) ── */
  .spt-save {
    position: sticky; bottom: 0; z-index: 5; display: flex; align-items: center; gap: 12px; flex-wrap: wrap;
    padding: 12px 22px; border-top: 1px solid var(--border); border-radius: 0 0 var(--r-lg) var(--r-lg);
    background: color-mix(in srgb, var(--bg-card, #151515) 94%, #000); backdrop-filter: blur(10px);
  }
  .spt-save.active { border-top-color: color-mix(in srgb, var(--pcol, var(--orange)) 50%, var(--border)); }
  .spt-dirty { font: 650 11.5px/1 var(--font-ui); color: var(--pcol, var(--orange)); }
  .spt-dim { font: 400 11px/1.4 var(--font-ui); color: var(--text-dim); display: inline-flex; align-items: center; gap: 5px; }
  .spt-errtext { font: 600 11px/1.4 var(--font-ui); color: var(--red); display: inline-flex; align-items: center; gap: 5px; max-width: 460px; word-break: break-word; }

  .spt-empty { display: flex; flex-direction: column; align-items: center; gap: 10px; padding: 50px 20px; color: var(--text-mute); font-size: 13px; text-align: center; }
  .spt-empty i { font-size: 28px; color: var(--text-dim); }
  .spt-empty.small { padding: 30px; }

  .spt-note { display: flex; gap: 10px; align-items: flex-start; margin: 12px 22px 0; padding: 10px 12px; border-radius: 9px; border: 1px solid var(--border); font: 400 11.5px/1.5 var(--font-ui); color: var(--text-mute); }
  .spt-note i { font-size: 16px; flex: 0 0 auto; margin-top: 1px; }
  .spt-note.warn { border-color: rgba(234, 179, 8, .4); background: rgba(234, 179, 8, .08); color: var(--text); }
  .spt-note.warn i { color: #eab308; }
</style>
