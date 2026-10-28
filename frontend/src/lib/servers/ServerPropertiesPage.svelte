<script lang="ts">
  // ── Сторінка «Властивості сервера» (server.properties) ─────────────────
  //
  // ОКРЕМИЙ РОЗДІЛ, а не картка в кінці «Налаштувань» (так просив
  // користувач): відкривається кнопкою з банера сервера й зібраний з тих самих
  // деталей, що налаштування лаунчера (settings.css: .st-top, .st-nav, SCard,
  // .st-row, .st-switch, StSelect) — шапка з пошуком, зліва групи, справа параметри.
  //
  // Що тут є понад старий список:
  //   • «Огляд» — зведення (скільки параметрів/змінено/помилок/не діють у
  //     цій версії), ЖИВИЙ аналіз значень (insights) і швидкі налаштування.
  //     Аналіз перераховується під час редагування, ще до збереження:
  //     «online-mode=false без білого списку», «RCON без пароля»,
  //     «view-distance 20» тощо — клік по зауваженню веде до параметра.
  //   • «Типові для групи» — скидання всієї групи до значень за замовчуванням.
  //   • Ctrl+S — зберегти; вихід із незбереженими змінами питає підтвердження.
  //   • Порт тут не показується (PROP_HIDDEN) — він редагується в
  //     «Налаштуваннях» сервера, там же і пишеться в server.properties.
  import { untrack, tick } from 'svelte'
  import { App } from '../wails'
  import { toast, confirmDialog } from '../toast.svelte'
  import { t } from '../i18n.svelte'
  import StSelect from '../StSelect.svelte'
  import SCard from '../settings/SCard.svelte'
  import type { PropertiesDoc, PropEntry } from '../types'
  import { PROP_GROUP_ORDER, PROP_GROUP_ICON, PROP_UNGROUPED, PROP_LEGACY, PROP_HIDDEN, LEGACY_ENUM_NUMBERS, isVoiceMod } from './constants'

  let {
    serverId = '',
    serverName = '',
    running = false,
    color = '',
    onBack = () => {},
    onOpenSettings = null,
    focusKey = '',
  }: {
    serverId?: string
    serverName?: string
    running?: boolean
    color?: string
    onBack?: () => void
    // Перейти до «Налаштувань» сервера (де редагується порт). null — без кнопки.
    onOpenSettings?: (() => void) | null
    // Параметр, обраний у пошуку сторінки сервера: після завантаження одразу переходимо до нього.
    focusKey?: string
  } = $props()

  const OVERVIEW = '__overview'
  // Ключі швидких налаштувань на «Огляді» (показуємо лише ті, що є у файлі
  // і не прибрані у версії сервера).
  const QUICK_KEYS = ['motd', 'max-players', 'gamemode', 'difficulty', 'white-list', 'online-mode', 'view-distance', 'simulation-distance', 'pvp', 'allow-flight']

  let propsDoc = $state<PropertiesDoc | null>(null)
  let loading = $state(false)
  let loadErr = $state('')
  let saving = $state(false)
  let search = $state('')
  let activeTab = $state(OVERVIEW)
  let revealed = $state<Record<string, boolean>>({})   // key → показати секрет
  let raw = $state<Record<string, string>>({})         // key → значення на момент завантаження
  let flashKey = $state('')                            // рядок, до якого щойно перейшли
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

  // Активна вкладка: якщо поточна зникла — «Огляд».
  const tab = $derived(activeTab === OVERVIEW || groups.some(g => g.id === activeTab) ? activeTab : OVERVIEW)
  const searching = $derived(search.trim().length > 0)

  // Пошук іде по ВСІХ групах: і за ключем, і за перекладеною назвою/поясненням —
  // «вайтлист» знаходить white-list, а не порожній екран.
  const visible = $derived.by(() => {
    const q = search.trim().toLowerCase()
    if (q) {
      return groups
        .map(g => ({ id: g.id, items: g.items.filter(e =>
          e.key.toLowerCase().includes(q) ||
          propName(e).toLowerCase().includes(q) ||
          propHint(e).toLowerCase().includes(q)) }))
        .filter(g => g.items.length > 0)
    }
    if (tab === OVERVIEW) return []
    return groups.filter(g => g.id === tab)
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
  const invalidAll = $derived(entries.filter(e => fieldError(e) !== ''))
  const legacyCount = $derived(groups.find(g => g.id === PROP_LEGACY)?.items.length ?? 0)

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
  const fmtNum = (n: number) => Math.abs(n) >= 10000 ? n.toLocaleString('uk-UA').replace(/\u00a0/g, ' ') : String(n)
  function rangeText(e: PropEntry): string {
    if (e.type !== 'int') return ''
    const u = e.unit ? ` ${e.unit}` : ''
    const lo = e.min, hi = e.max
    if (lo !== undefined && hi !== undefined) return t('servers.props.range', { min: fmtNum(lo), max: fmtNum(hi) }) + u
    if (lo !== undefined) return t('servers.props.rangeMin', { min: fmtNum(lo) }) + u
    if (hi !== undefined) return t('servers.props.rangeMax', { max: fmtNum(hi) }) + u
    return e.unit ? e.unit : ''
  }

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

  // Скинути ВСЮ групу до типових значень (лише для відомих ключів, у яких
  // є default і які діють у цій версії).
  function resetGroupDefaults(id: string) {
    const g = groups.find(x => x.id === id)
    if (!g) return
    let n = 0
    for (const e of g.items) if (canResetDefault(e)) { e.value = e.default ?? ''; n++ }
    if (n > 0) toast(t('servers.props.groupReset', { n }), 'info')
  }
  const groupResettable = (id: string) => (groups.find(g => g.id === id)?.items ?? []).filter(canResetDefault).length

  function stepInt(e: PropEntry, dir: 1 | -1) {
    const n = Number(String(e.value).trim())
    const step = e.step && e.step > 0 ? e.step : 1
    let next = (Number.isFinite(n) ? n : (e.min ?? 0)) + dir * step
    if (e.min !== undefined) next = Math.max(e.min, next)
    if (e.max !== undefined) next = Math.min(e.max, next)
    e.value = String(next)
  }

  async function save() {
    if (!propsDoc || saving) return
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

  // Вихід із сторінки: якщо є незбережені зміни — спершу питаємо.
  async function leave() {
    if (dirty) {
      const discard = await confirmDialog(
        t('servers.props.leaveTitle'),
        t('servers.props.leaveBody', { n: dirtyEntries.length }),
        { confirmLabel: t('servers.props.leaveDiscard'), cancelLabel: t('servers.props.leaveStay'), danger: true },
      )
      if (!discard) return
    }
    onBack()
  }

  // Ctrl/Cmd+S — зберегти. Слухач живе лише поки сторінка змонтована.
  $effect(() => {
    const onKey = (ev: KeyboardEvent) => {
      if ((ev.ctrlKey || ev.metaKey) && ev.key.toLowerCase() === 's') {
        ev.preventDefault()
        if (dirty) void save()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })

  // ── Живий аналіз значень ──────────────────────────────────────────────
  // Правила дивляться на ПОТОЧНІ (ще не збережені) значення — гравець бачить
  // наслідки своєї зміни до натискання «Зберегти».
  type Insight = { id: string; level: 'error' | 'warn' | 'info'; key: string; text: string }
  const vmap = $derived(new Map(entries.map(e => [e.key, String(e.value ?? '').trim()])))
  const insights = $derived.by<Insight[]>(() => {
    const v = (k: string) => vmap.get(k)
    const n = (k: string) => { const x = Number(v(k)); return Number.isFinite(x) ? x : NaN }
    const out: Insight[] = []

    if (v('online-mode') === 'false') {
      if (v('white-list') === 'true') out.push({ id: 'offline', level: 'warn', key: 'online-mode', text: t('servers.props.insight.offline') })
      else out.push({ id: 'offlineOpen', level: 'error', key: 'online-mode', text: t('servers.props.insight.offlineOpen') })
      if (v('enforce-secure-profile') === 'true') out.push({ id: 'secureProfile', level: 'warn', key: 'enforce-secure-profile', text: t('servers.props.insight.secureProfile') })
    }
    if (v('enable-rcon') === 'true' && (v('rcon.password') ?? '') === '') {
      out.push({ id: 'rconNoPass', level: 'error', key: 'rcon.password', text: t('servers.props.insight.rconNoPass') })
    }
    if (v('enable-query') === 'true') out.push({ id: 'query', level: 'info', key: 'enable-query', text: t('servers.props.insight.query') })
    if (n('view-distance') > 12) out.push({ id: 'viewDist', level: 'warn', key: 'view-distance', text: t('servers.props.insight.viewDist', { n: n('view-distance') }) })
    if (n('simulation-distance') > 10) out.push({ id: 'simDist', level: 'warn', key: 'simulation-distance', text: t('servers.props.insight.simDist', { n: n('simulation-distance') }) })
    if ((v('server-ip') ?? '') !== '') out.push({ id: 'serverIp', level: 'warn', key: 'server-ip', text: t('servers.props.insight.serverIp', { ip: v('server-ip') ?? '' }) })
    if (n('max-players') > 200) out.push({ id: 'maxPlayers', level: 'info', key: 'max-players', text: t('servers.props.insight.maxPlayers', { n: n('max-players') }) })
    const motd = entries.find(e => e.key === 'motd')
    if (motd && motd.value.length > 59) out.push({ id: 'motd', level: 'info', key: 'motd', text: t('servers.props.insight.motd', { n: motd.value.length }) })
    if (invalidAll.length > 0) out.push({ id: 'invalid', level: 'error', key: invalidAll[0].key, text: t('servers.props.insight.invalid', { n: invalidAll.length }) })
    if (legacyCount > 0) out.push({ id: 'legacy', level: 'info', key: groups.find(g => g.id === PROP_LEGACY)?.items[0]?.key ?? '', text: t('servers.props.insight.legacy', { n: legacyCount, v: propsDoc?.mcVersion || '—' }) })

    const rank = { error: 0, warn: 1, info: 2 } as const
    return out.sort((a, b) => rank[a.level] - rank[b.level])
  })
  const insightIcon = { error: 'ti-circle-x', warn: 'ti-alert-triangle', info: 'ti-info-circle' } as const

  // Перехід до параметра: відкриваємо його групу, прокручуємо й підсвічуємо.
  async function jumpTo(key: string) {
    const e = entries.find(x => x.key === key)
    if (!e) return
    search = ''
    activeTab = groupOf(e)
    await tick()
    flashKey = key
    document.querySelector<HTMLElement>(`[data-pkey="${CSS.escape(key)}"]`)?.scrollIntoView({ block: 'center', behavior: 'smooth' })
    setTimeout(() => { if (flashKey === key) flashKey = '' }, 1800)
  }

  let focusedFor = ''
  $effect(() => {
    const k = focusKey
    if (!k || !propsDoc || focusedFor === k) return
    focusedFor = k
    untrack(() => { void jumpTo(k) })
  })

  const quickEntries = $derived(QUICK_KEYS.map(k => entries.find(e => e.key === k)).filter((e): e is PropEntry => !!e && e.status !== 'removed'))

  // ── Голосовий чат (UDP) ───────────────────────────────────────────────
  // Голосові моди (Simple Voice Chat / Plasmo Voice) ходять UDP-портом
  // (типово 24454) ПОВЗ Minecraft-протокол. У P2P це окремий тунель, який
  // гравець вмикає у вкладці «Мережа». Якщо мод стоїть, а тунель вимкнено —
  // голосу не буде зовсім, і жодна помилка в грі про це не скаже.
  const currentPort = $derived(Number(propsDoc?.entries.find(e => e.key === 'server-port')?.value) || Number(raw['server-port']) || 25565)

  async function syncVoiceState() {
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

<!-- ═══ Один рядок параметра (спільний для груп, пошуку й «Швидких») ═══
     Та сама розмітка, що й рядки налаштувань лаунчера (.st-row з settings.css):
     назва, коротка підказка, під нею лише ключ файлу і (якщо змінено) «типове». -->
{#snippet propRow(entry: PropEntry, inactive: boolean)}
  {@const err = fieldError(entry)}
  {@const hint = propHint(entry)}
  {@const range = rangeText(entry)}
  {@const defText = defaultText(entry)}
  {@const canon = entry.type === 'enum' ? canonEnum(entry) : null}
  <div class="st-row spp-row" data-pkey={entry.key} class:stack={entry.type === 'string'} class:dirty={isDirty(entry)}
       class:invalid={err !== ''} class:inactive class:st-flash={flashKey === entry.key}>
    <div class="st-row-text">
      <div class="st-row-name spp-name">
        <span>{propName(entry)}</span>
        {#if entry.newWorldOnly}<span class="st-tag" title={t('servers.props.newWorldOnlyHint')}><i class="ti ti-world-plus"></i> {t('servers.props.newWorldOnly')}</span>{/if}
        {#if !entry.known}<span class="st-tag">{t('servers.props.unknownKey')}</span>{/if}
      </div>
      {#if hint}<div class="st-row-hint">{hint}</div>{/if}
      <div class="spp-sub">
        <code>{entry.key}</code>
        {#if defText && canResetDefault(entry)}
          <span aria-hidden="true">·</span>
          <button class="spp-def" title={t('servers.props.useDefault')} onclick={() => setValue(entry, entry.default ?? '')}>{defText}</button>
        {/if}
      </div>
      {#if err}<div class="spp-err"><i class="ti ti-alert-circle"></i> {err}</div>{/if}
    </div>

    <div class="st-row-ctl">
      {#if entry.type === 'bool'}
        <button class="st-switch" class:on={entry.value === 'true'} role="switch" aria-checked={entry.value === 'true'}
                aria-label={propName(entry)}
                onclick={() => setValue(entry, entry.value === 'true' ? 'false' : 'true')}></button>

      {:else if entry.type === 'enum' && useSegments(entry)}
        <div class="spp-seg" role="radiogroup" aria-label={propName(entry)}>
          {#each entry.enum ?? [] as opt (opt)}
            <button role="radio" aria-checked={canon === opt} class:on={canon === opt}
                    onclick={() => setValue(entry, opt)}>{enumLabel(entry.key, opt)}</button>
          {/each}
        </div>

      {:else if entry.type === 'enum'}
        <StSelect
          value={canon ?? entry.value}
          options={enumOptions(entry)}
          onchange={(v) => setValue(entry, v)}
          ariaLabel={propName(entry)}
          minWidth="190px"
        />

      {:else if entry.type === 'int'}
        {#if entry.slider && entry.min !== undefined && entry.max !== undefined}
          <input class="spp-range" type="range" min={entry.min} max={entry.max} step={entry.step ?? 1}
                 value={entry.value} aria-label={propName(entry)}
                 oninput={(ev) => setValue(entry, (ev.currentTarget as HTMLInputElement).value)} />
        {/if}
        <input class="st-input num mono" class:bad={err !== ''} type="text" inputmode="numeric" value={entry.value}
               title={range} aria-label={propName(entry)} aria-invalid={err !== ''}
               onkeydown={(ev) => { if (ev.key === 'ArrowUp' || ev.key === 'ArrowDown') { ev.preventDefault(); stepInt(entry, ev.key === 'ArrowUp' ? 1 : -1) } }}
               oninput={(ev) => setValue(entry, (ev.currentTarget as HTMLInputElement).value.trim())} />
        {#if entry.unit}<span class="st-unit">{entry.unit}</span>{/if}

      {:else}
        <input class="st-input mono grow" class:bad={err !== ''}
               type={entry.secret && !revealed[entry.key] ? 'password' : 'text'}
               autocomplete="off" spellcheck="false"
               value={entry.value} aria-label={propName(entry)}
               oninput={(ev) => setValue(entry, (ev.currentTarget as HTMLInputElement).value)} />
        {#if entry.secret}
          <button class="st-iconbtn" onclick={() => revealed[entry.key] = !revealed[entry.key]}
                  title={revealed[entry.key] ? t('servers.props.hide') : t('servers.props.show')}
                  aria-label={revealed[entry.key] ? t('servers.props.hide') : t('servers.props.show')}>
            <i class="ti {revealed[entry.key] ? 'ti-eye-off' : 'ti-eye'}"></i>
          </button>
        {/if}
        {#if entry.key === 'motd'}
          <span class="spp-count" class:over={entry.value.length > 59}>{t('servers.props.motdLen', { n: entry.value.length })}</span>
        {/if}
      {/if}

      {#if isDirty(entry)}
        <button class="st-iconbtn" title={t('servers.props.revertOne')} aria-label={t('servers.props.revertOne')} onclick={() => revertOne(entry)}>
          <i class="ti ti-arrow-back-up"></i>
        </button>
      {/if}
    </div>
  </div>
{/snippet}

<div class="st-root spp" style="--pcol:{color || 'var(--orange)'}" data-testid="server-properties">
  <header class="st-top">
    <button class="btn btn-ghost btn-sm" onclick={leave} aria-label={t('servers.props.back')}>
      <i class="ti ti-arrow-left"></i> {t('servers.props.back')}
    </button>
    <div class="spp-title">
      <b>{t('servers.props.title')}</b>
      <span><span class="mono">server.properties</span>{serverName ? ` · ${serverName}` : ''}{propsDoc?.mcVersion ? ` · ${propsDoc.mcVersion}` : ''}</span>
    </div>
    {#if propsDoc}
      <div class="st-search">
        <i class="ti ti-search st-search-ic"></i>
        <input bind:value={search} placeholder={t('servers.props.search')} spellcheck="false"
               onkeydown={(e) => { if (e.key === 'Escape') search = '' }} />
        {#if search}
          <button class="st-search-clear" onclick={() => search = ''} aria-label={t('servers.props.clearSearch')}><i class="ti ti-x"></i></button>
        {/if}
      </div>
    {/if}
  </header>

  {#if loading}
    <div class="st-empty"><i class="ti ti-loader spin"></i></div>
  {:else if !propsDoc}
    <section class="st-card">
      <div class="st-empty">
        <i class="ti ti-file-settings"></i>
        <b>{t('servers.props.loadFailed')}</b>
        <span>{loadErr || t('servers.props.loadFailedSub')}</span>
        <button class="btn btn-ghost btn-sm" style="margin-top:8px" onclick={load}><i class="ti ti-refresh"></i> {t('servers.props.retry')}</button>
      </div>
    </section>
  {:else}
    <div class="st-layout">
      <nav class="st-nav" class:spp-dim={searching} aria-label={t('servers.props.sections')}>
        <div class="st-nav-group">
          <div class="st-nav-title">{t('servers.props.sections')}</div>
          <button class="st-nav-item" class:active={!searching && tab === OVERVIEW} onclick={() => { search = ''; activeTab = OVERVIEW }}>
            <i class="ti ti-layout-dashboard"></i><span>{t('servers.props.overview')}</span>
            {#if insights.some(i => i.level !== 'info')}<em class="spp-mark warn" title={t('servers.props.insights')}></em>{/if}
          </button>
          {#each groups as g (g.id)}
            <button class="st-nav-item" class:active={!searching && tab === g.id} onclick={() => { search = ''; activeTab = g.id }}>
              <i class="ti {PROP_GROUP_ICON[g.id] ?? 'ti-dots'}"></i><span>{t(`props.group.${g.id}`)}</span>
              {#if dirtyByGroup[g.id]}<em class="spp-mark" title={t('servers.props.dirty', { n: dirtyByGroup[g.id] })}></em>{/if}
            </button>
          {/each}
        </div>
      </nav>

      <main class="st-main">
        {#if running}
          <div class="spp-banner"><i class="ti ti-player-play"></i><span>{t('servers.props.runningWarn')}</span></div>
        {/if}
        {#if voiceMods.length > 0 && !udpOn}
          <div class="spp-banner"><i class="ti ti-microphone-off"></i><span>{t('props.voice.body', { mods: voiceMods.join(', '), port: voicePort })}</span></div>
        {/if}

        {#key searching ? `s:${search}` : tab}
          <div class="st-panel">
            {#if searching}
              <div class="st-sec-head">
                <h2>{t('servers.props.results', { n: foundCount })}</h2>
              </div>
              {#if visible.length === 0}
                <section class="st-card"><div class="st-empty"><i class="ti ti-search-off"></i><span>{t('servers.props.nothingFound', { q: search })}</span></div></section>
              {/if}
              {#each visible as group (group.id)}
                <SCard icon={PROP_GROUP_ICON[group.id] ?? 'ti-dots'} title={t(`props.group.${group.id}`)}>
                  {#each group.items as entry (entry.key)}
                    {@render propRow(entry, group.id === PROP_LEGACY)}
                  {/each}
                </SCard>
              {/each}

            {:else if tab === OVERVIEW}
              <div class="st-sec-head">
                <h2>{t('servers.props.overview')}</h2>
                <p>{t('servers.props.insightsDesc')}</p>
              </div>

              <div class="spp-stats">
                <div class="spp-stat"><b>{entries.length}</b><span>{t('servers.props.stat.total')}</span></div>
                <div class="spp-stat" class:hot={dirty}><b>{dirtyEntries.length}</b><span>{t('servers.props.stat.changed')}</span></div>
                <div class="spp-stat" class:bad={invalidAll.length > 0}><b>{invalidAll.length}</b><span>{t('servers.props.stat.errors')}</span></div>
                <div class="spp-stat"><b>{legacyCount}</b><span>{t('servers.props.stat.inactive')}</span></div>
              </div>

              <SCard icon="ti-stethoscope" tone={insights.some(i => i.level === 'error') ? 'red' : insights.some(i => i.level === 'warn') ? 'yellow' : 'green'}
                     title={t('servers.props.insights')}>
                {#if insights.length === 0}
                  <div class="st-pad spp-ok"><i class="ti ti-circle-check"></i> {t('servers.props.insightsNone')}</div>
                {:else}
                  {#each insights as ins (ins.id)}
                    <button class="spp-ins {ins.level}" onclick={() => ins.key && jumpTo(ins.key)} disabled={!ins.key}>
                      <i class="ti {insightIcon[ins.level]}"></i>
                      <span>{ins.text}</span>
                      {#if ins.key}<i class="ti ti-chevron-right spp-ins-go"></i>{/if}
                    </button>
                  {/each}
                {/if}
              </SCard>

              {#if quickEntries.length > 0}
                <SCard icon="ti-bolt" title={t('servers.props.quick')} desc={t('servers.props.quickDesc')}>
                  {#each quickEntries as entry (entry.key)}
                    {@render propRow(entry, false)}
                  {/each}
                </SCard>
              {/if}

              <SCard icon="ti-plug" tone="gray" title={t('servers.props.portMovedTitle')} desc={t('servers.props.portMoved', { port: currentPort })}>
                {#snippet actions()}
                  {#if onOpenSettings}
                    <button class="btn btn-ghost btn-sm" onclick={onOpenSettings}><i class="ti ti-settings"></i> {t('servers.props.openSettings')}</button>
                  {/if}
                {/snippet}
              </SCard>

            {:else}
              {#each visible as group (group.id)}
                <div class="st-sec-head spp-sec-head">
                  <div>
                    <h2>{t(`props.group.${group.id}`)}</h2>
                    <p>{group.id === PROP_LEGACY ? t('servers.props.legacyNote', { v: propsDoc.mcVersion || '—' }) : t(`props.groupDesc.${group.id}`)}</p>
                  </div>
                  {#if groupResettable(group.id) > 0}
                    <button class="btn btn-ghost btn-sm" onclick={() => resetGroupDefaults(group.id)} title={t('servers.props.resetGroupHint')}>
                      <i class="ti ti-restore"></i> {t('servers.props.resetGroup')}
                    </button>
                  {/if}
                </div>
                <section class="st-card spp-rows">
                  {#each group.items as entry (entry.key)}
                    {@render propRow(entry, group.id === PROP_LEGACY)}
                  {/each}
                </section>
              {/each}
            {/if}
          </div>
        {/key}
      </main>
    </div>

    <!-- Панель збереження: з'являється лише коли є що зберегти. -->
    {#if dirty}
      <div class="spp-save">
        <span class="spp-dirty"><i class="ti ti-pencil"></i> {t('servers.props.dirty', { n: dirtyEntries.length })}</span>
        {#if blockingErrors > 0}<span class="spp-errtext"><i class="ti ti-alert-circle"></i> {t('servers.props.errCount', { n: blockingErrors })}</span>{/if}
        <span class="spp-hint">{t('servers.props.restartHint')}</span>
        <button class="btn btn-ghost btn-sm" onclick={revertAll}><i class="ti ti-arrow-back-up"></i> {t('servers.props.revertAll')}</button>
        <button class="btn btn-primary btn-sm" onclick={save} disabled={saving || blockingErrors > 0}>
          {#if saving}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-check"></i>{/if}
          {t('servers.props.save')}
          <kbd class="spp-kbd">Ctrl S</kbd>
        </button>
      </div>
    {/if}
  {/if}
</div>

<style>
  /* Макет, картки, рядки й контроли — спільні з налаштуваннями лаунчера (settings.css, .st-*).
     Тут лише те, чого там немає: маркери меню, зведення, аналіз, сегменти зі словами, панель збереження. */
  .spp { height: 100%; min-height: 0; }
  .spp .st-top { flex-wrap: nowrap; }
  .spp-title { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .spp-title b { font-size: 17px; font-weight: 800; }
  .spp-title span { font-size: 12px; color: var(--text-mute); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

  /* Меню: крапка «є зміни» / «є зауваження» праворуч від назви. */
  .spp-dim .st-nav-item { opacity: .55; }
  .spp-mark { margin-left: auto; width: 7px; height: 7px; flex: none; border-radius: 50%; background: var(--pcol, var(--orange)); }
  .spp-mark.warn { background: var(--yellow); }

  .spp-banner {
    display: flex; gap: 10px; align-items: flex-start; margin: 0 0 16px; padding: 11px 14px;
    border-radius: var(--r-md); border: 1px solid rgba(245, 158, 11, .32); background: rgba(245, 158, 11, .08);
    font-size: 12.5px; line-height: 1.5; color: var(--text);
  }
  .spp-banner i { font-size: 16px; margin-top: 1px; flex: none; color: var(--yellow); }

  .spp-sec-head { display: flex; align-items: flex-end; justify-content: space-between; gap: 16px; }
  .spp-rows > * + * { border-top: 1px solid var(--border); }

  /* ── Зведення ── */
  .spp-stats { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 10px; margin: 0 0 16px; }
  .spp-stat { display: flex; flex-direction: column; gap: 4px; padding: 14px 16px; border-radius: var(--r-lg); background: var(--bg-card); border: 1px solid var(--border); }
  .spp-stat b { font-size: 22px; font-weight: 800; line-height: 1.1; }
  .spp-stat span { font-size: 12px; color: var(--text-mute); }
  .spp-stat.hot b { color: var(--pcol, var(--orange)); }
  .spp-stat.bad b { color: var(--red); }
  @media (max-width: 760px) { .spp-stats { grid-template-columns: repeat(2, minmax(0, 1fr)); } }

  /* ── Аналіз ── */
  .spp-ok { display: flex; align-items: center; gap: 9px; font-size: 13px; color: var(--green); }
  .spp-ok i { font-size: 18px; }
  .spp-ins {
    display: flex; align-items: center; gap: 12px; width: 100%; padding: 13px 20px; text-align: left; cursor: pointer;
    border: 0; background: transparent; color: var(--text); font: 500 13px/1.45 var(--font-ui); transition: background .15s;
  }
  .spp-ins:hover:not(:disabled) { background: var(--hover); }
  .spp-ins:disabled { cursor: default; }
  .spp-ins > span { flex: 1; min-width: 0; }
  .spp-ins > i:first-child { font-size: 18px; flex: none; }
  .spp-ins.error > i:first-child { color: var(--red); }
  .spp-ins.warn > i:first-child { color: var(--yellow); }
  .spp-ins.info > i:first-child { color: var(--blue); }
  .spp-ins-go { font-size: 15px; color: var(--text-dim); flex: none; }

  /* ── Рядок параметра: змінений — тонка смужка зліва, недійсний — червона. ── */
  .spp-row { position: relative; }
  .spp-row::before { content: ''; position: absolute; left: 0; top: 12px; bottom: 12px; width: 3px; border-radius: 0 3px 3px 0; background: transparent; }
  .spp-row.dirty::before { background: var(--pcol, var(--orange)); }
  .spp-row.invalid::before { background: var(--red); }
  .spp-row.inactive { opacity: .55; }
  .spp-name { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; }
  .spp-sub { display: flex; align-items: center; gap: 6px; margin-top: 4px; font-size: 11.5px; color: var(--text-dim); }
  .spp-sub code { font: 11px var(--font-mono); }
  .spp-def { padding: 0; border: 0; background: none; cursor: pointer; color: var(--text-mute); font: inherit; text-decoration: underline dotted; text-underline-offset: 3px; }
  .spp-def:hover { color: var(--pcol, var(--orange)); }
  .spp-err { display: flex; align-items: center; gap: 5px; margin-top: 5px; font-size: 12px; font-weight: 600; color: var(--red); }
  .spp .st-input.bad { border-color: var(--red); }
  .spp-range { width: 150px; accent-color: var(--pcol, var(--orange)); cursor: pointer; }
  .spp-count { flex: none; font: 600 11px var(--font-mono); color: var(--text-dim); }
  .spp-count.over { color: var(--yellow); }

  /* Сегменти зі словами — як .st-seg, але кнопки за шириною тексту. */
  .spp-seg { display: inline-flex; flex-wrap: wrap; gap: 3px; padding: 3px; background: var(--bg-input); border: 1px solid var(--border-hi); border-radius: 12px; }
  .spp-seg button {
    height: 30px; padding: 0 12px; border: 1px solid transparent; border-radius: 9px; background: transparent;
    color: var(--text-mute); font: 600 12.5px var(--font-ui); cursor: pointer; white-space: nowrap; transition: color .15s;
  }
  .spp-seg button:hover { color: var(--text); }
  .spp-seg button.on { background: var(--bg-card); color: var(--orange); border-color: rgba(var(--orange-rgb), .5); }

  /* ── Панель збереження ── */
  .spp-save {
    flex: none; display: flex; align-items: center; gap: 12px; flex-wrap: wrap;
    padding: 10px 14px 10px 18px; border-radius: var(--r-lg);
    background: var(--bg-card); border: 1px solid rgba(var(--orange-rgb), .45);
    box-shadow: 0 10px 30px rgba(0, 0, 0, .3);
    animation: st-in .2s ease both;
  }
  .spp-dirty { display: inline-flex; align-items: center; gap: 6px; font-size: 13px; font-weight: 600; color: var(--pcol, var(--orange)); }
  .spp-hint { flex: 1; min-width: 160px; font-size: 12px; color: var(--text-mute); }
  .spp-errtext { display: inline-flex; align-items: center; gap: 5px; font-size: 12px; font-weight: 600; color: var(--red); }
  .spp-kbd { margin-left: 4px; padding: 2px 5px; border-radius: 5px; font: 600 var(--fs-min)/1 var(--font-mono); background: rgba(0, 0, 0, .22); opacity: .8; }
</style>
