<script lang="ts">
  // ── Сторінка «Властивості сервера» (server.properties) ─────────────────
  //
  // ОКРЕМИЙ РОЗДІЛ, а не картка в кінці «Налаштувань» (так просив
  // користувач): відкривається кнопкою з банера сервера й має той самий
  // макет, що налаштування лаунчера — шапка з пошуком, зліва меню груп
  // (.sub-nav з ковзною пігулкою), справа картки параметрів обраної групи.
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
  import SelectField from '../SelectField.svelte'
  import type { PropertiesDoc, PropEntry } from '../types'
  import { PROP_GROUP_ORDER, PROP_GROUP_ICON, PROP_UNGROUPED, PROP_LEGACY, PROP_HIDDEN, LEGACY_ENUM_NUMBERS, isVoiceMod } from './constants'

  let {
    serverId = '',
    serverName = '',
    running = false,
    color = '',
    onBack = () => {},
    onOpenSettings = null,
  }: {
    serverId?: string
    serverName?: string
    running?: boolean
    color?: string
    onBack?: () => void
    // Перейти до «Налаштувань» сервера (де редагується порт). null — без кнопки.
    onOpenSettings?: (() => void) | null
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

  // ── Ковзна пігулка активного пункту меню (як у налаштуваннях лаунчера) ──
  let navEl = $state<HTMLElement | null>(null)
  $effect(() => {
    void tab; void groups.length
    const measure = () => {
      const nav = navEl
      const active = nav?.querySelector<HTMLElement>('.sni.active')
      if (!nav || !active) return
      nav.style.setProperty('--ind-top', `${active.offsetTop}px`)
      nav.style.setProperty('--ind-h', `${active.offsetHeight}px`)
    }
    const raf = requestAnimationFrame(() => requestAnimationFrame(measure))
    window.addEventListener('resize', measure)
    return () => { cancelAnimationFrame(raf); window.removeEventListener('resize', measure) }
  })
</script>

<!-- ═══ Один рядок параметра (спільний для груп, пошуку й «Швидких») ═══ -->
{#snippet propRow(entry: PropEntry, inactive: boolean)}
  {@const err = fieldError(entry)}
  {@const hint = propHint(entry)}
  {@const range = rangeText(entry)}
  {@const defText = defaultText(entry)}
  {@const canon = entry.type === 'enum' ? canonEnum(entry) : null}
  <div class="spp-row" data-pkey={entry.key} class:dirty={isDirty(entry)} class:invalid={err !== ''} class:wide={entry.type === 'string'} class:inactive class:flash={flashKey === entry.key}>
    <div class="spp-info">
      <div class="spp-name">
        <span>{propName(entry)}</span>
        {#if entry.newWorldOnly}<span class="spp-badge" title={t('servers.props.newWorldOnlyHint')}><i class="ti ti-world-plus"></i> {t('servers.props.newWorldOnly')}</span>{/if}
        {#if !entry.known}<span class="spp-badge mute">{t('servers.props.unknownKey')}</span>{/if}
      </div>
      {#if hint}<div class="spp-hint">{hint}</div>{/if}
      <div class="spp-meta">
        <code class="spp-key">{entry.key}</code>
        {#if range}<span class="spp-chipm">{range}</span>{/if}
        {#if defText}
          {#if canResetDefault(entry)}
            <button class="spp-chipm link" title={t('servers.props.useDefault')} onclick={() => setValue(entry, entry.default ?? '')}>{defText}</button>
          {:else}
            <span class="spp-chipm">{defText}</span>
          {/if}
        {/if}
      </div>
      {#if err}<div class="spp-err"><i class="ti ti-alert-circle"></i> {err}</div>{/if}
    </div>

    <div class="spp-ctl">
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
        <div class="spp-seg" role="radiogroup" aria-label={propName(entry)}>
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
        <div class="spp-int">
          {#if entry.slider && entry.min !== undefined && entry.max !== undefined}
            <input class="spp-range" type="range" min={entry.min} max={entry.max} step={entry.step ?? 1}
                   value={entry.value} aria-label={propName(entry)}
                   oninput={(ev) => setValue(entry, (ev.currentTarget as HTMLInputElement).value)} />
          {/if}
          <div class="spp-stepper" class:bad={err !== ''}>
            <button tabindex="-1" aria-label="−" onclick={() => stepInt(entry, -1)}><i class="ti ti-minus"></i></button>
            <input class="mono" type="text" inputmode="numeric" value={entry.value}
                   aria-label={propName(entry)} aria-invalid={err !== ''}
                   oninput={(ev) => setValue(entry, (ev.currentTarget as HTMLInputElement).value.trim())} />
            <button tabindex="-1" aria-label="+" onclick={() => stepInt(entry, 1)}><i class="ti ti-plus"></i></button>
          </div>
        </div>

      {:else}
        <div class="spp-text">
          <input class="input mono" class:bad={err !== ''}
                 type={entry.secret && !revealed[entry.key] ? 'password' : 'text'}
                 autocomplete="off" spellcheck="false"
                 value={entry.value} aria-label={propName(entry)}
                 oninput={(ev) => setValue(entry, (ev.currentTarget as HTMLInputElement).value)} />
          {#if entry.secret}
            <button class="spp-eye" onclick={() => revealed[entry.key] = !revealed[entry.key]}
                    title={revealed[entry.key] ? t('servers.props.hide') : t('servers.props.show')}
                    aria-label={revealed[entry.key] ? t('servers.props.hide') : t('servers.props.show')}>
              <i class="ti {revealed[entry.key] ? 'ti-eye-off' : 'ti-eye'}"></i>
            </button>
          {/if}
          {#if entry.key === 'motd'}
            <span class="spp-count" class:over={entry.value.length > 59}>{t('servers.props.motdLen', { n: entry.value.length })}</span>
          {/if}
        </div>
      {/if}

      {#if isDirty(entry)}
        <button class="spp-revert" title={t('servers.props.revertOne')} aria-label={t('servers.props.revertOne')} onclick={() => revertOne(entry)}>
          <i class="ti ti-arrow-back-up"></i>
        </button>
      {/if}
    </div>
  </div>
{/snippet}

<div class="spp settings-page" style="--pcol:{color || 'var(--orange)'}" data-testid="server-properties">
  <header class="settings-header">
    <button class="btn btn-ghost btn-sm spp-back" onclick={leave} aria-label={t('servers.props.back')}>
      <i class="ti ti-arrow-left"></i> {t('servers.props.back')}
    </button>
    <div class="settings-title">
      <div class="settings-title-icon"><i class="ti ti-adjustments-horizontal"></i></div>
      <div class="settings-title-text">
        <div class="settings-title-main">{t('servers.props.title')}</div>
        <div class="settings-title-sub"><span class="mono">server.properties</span>{serverName ? ` · ${serverName}` : ''}{propsDoc?.mcVersion ? ` · ${propsDoc.mcVersion}` : ''}</div>
      </div>
    </div>
    {#if propsDoc}
      <div class="settings-search spp-search">
        <i class="ti ti-search"></i>
        <input bind:value={search} placeholder={t('servers.props.search')} spellcheck="false"
               onkeydown={(e) => { if (e.key === 'Escape') search = '' }} />
        {#if search}
          <button class="spp-x" onclick={() => search = ''} title={t('servers.props.clearSearch')} aria-label={t('servers.props.clearSearch')}><i class="ti ti-x"></i></button>
        {/if}
      </div>
    {/if}
  </header>

  {#if loading}
    <div class="spp-empty"><i class="ti ti-loader spin"></i></div>
  {:else if !propsDoc}
    <div class="card spp-empty">
      <i class="ti ti-file-settings"></i>
      <div>{t('servers.props.loadFailed')}</div>
      {#if loadErr}
        <div class="spp-errtext">{loadErr}</div>
      {:else}
        <div class="spp-dim">{t('servers.props.loadFailedSub')}</div>
      {/if}
      <button class="btn btn-ghost btn-sm" style="margin-top:10px" onclick={load}><i class="ti ti-refresh"></i> {t('servers.props.retry')}</button>
    </div>
  {:else}
    {#if running}
      <div class="spp-note warn"><i class="ti ti-player-play"></i><div>{t('servers.props.runningWarn')}</div></div>
    {/if}
    {#if voiceMods.length > 0 && !udpOn}
      <div class="spp-note warn">
        <i class="ti ti-microphone-off"></i>
        <div>{t('props.voice.body', { mods: voiceMods.join(', '), port: voicePort })}</div>
      </div>
    {/if}

    <div class="settings-grid">
      <nav class="sub-nav" bind:this={navEl} class:dim={searching}>
        <div class="sub-nav-label">{t('servers.props.sections')}</div>
        <div class="sni" class:active={!searching && tab === OVERVIEW} onclick={() => { search = ''; activeTab = OVERVIEW }} role="tab" tabindex="0" aria-selected={!searching && tab === OVERVIEW} onkeydown={(e) => { if (e.key === 'Enter') { search = ''; activeTab = OVERVIEW } }}>
          <i class="ti ti-layout-dashboard"></i>
          <span class="sni-label">{t('servers.props.overview')}</span>
          {#if insights.some(i => i.level !== 'info')}<span class="spp-nav-warn" title={t('servers.props.insights')}></span>{/if}
        </div>
        {#each groups as g (g.id)}
          <div class="sni" class:active={!searching && tab === g.id} onclick={() => { search = ''; activeTab = g.id }} role="tab" tabindex="0" aria-selected={!searching && tab === g.id} onkeydown={(e) => { if (e.key === 'Enter') { search = ''; activeTab = g.id } }}>
            <i class="ti {PROP_GROUP_ICON[g.id] ?? 'ti-dots'}"></i>
            <span class="sni-label">{t(`props.group.${g.id}`)}</span>
            {#if dirtyByGroup[g.id]}<span class="spp-nav-dot" title={t('servers.props.dirty', { n: dirtyByGroup[g.id] })}></span>{/if}
            <span class="spp-nav-n">{g.items.length}</span>
          </div>
        {/each}
      </nav>

      <div class="sub-content">
        {#key searching ? `s:${search}` : tab}
          <div class="settings-tab-panel">
            {#if searching}
              <div class="spp-found"><i class="ti ti-search"></i> {t('servers.props.results', { n: foundCount })}</div>
              {#if visible.length === 0}
                <div class="card spp-empty small"><i class="ti ti-search-off"></i><div>{t('servers.props.nothingFound', { q: search })}</div></div>
              {/if}
              {#each visible as group (group.id)}
                <div class="card">
                  <div class="card-head">
                    <div class="ci"><i class="ti {PROP_GROUP_ICON[group.id] ?? 'ti-dots'}"></i></div>
                    <div class="ctw"><div class="ct">{t(`props.group.${group.id}`)}</div></div>
                  </div>
                  {#each group.items as entry (entry.key)}
                    {@render propRow(entry, group.id === PROP_LEGACY)}
                  {/each}
                </div>
              {/each}

            {:else if tab === OVERVIEW}
              <!-- Зведення -->
              <div class="spp-stats">
                <div class="spp-stat"><i class="ti ti-list-details"></i><b>{entries.length}</b><span>{t('servers.props.stat.total')}</span></div>
                <div class="spp-stat" class:hot={dirty}><i class="ti ti-pencil"></i><b>{dirtyEntries.length}</b><span>{t('servers.props.stat.changed')}</span></div>
                <div class="spp-stat" class:bad={invalidAll.length > 0}><i class="ti ti-alert-circle"></i><b>{invalidAll.length}</b><span>{t('servers.props.stat.errors')}</span></div>
                <div class="spp-stat" class:muted={legacyCount === 0}><i class="ti ti-history"></i><b>{legacyCount}</b><span>{t('servers.props.stat.inactive')}</span></div>
              </div>

              <!-- Живий аналіз -->
              <div class="card">
                <div class="card-head">
                  <div class="ci {insights.some(i => i.level === 'error') ? 'red' : insights.some(i => i.level === 'warn') ? 'yellow' : 'green'}"><i class="ti ti-stethoscope"></i></div>
                  <div class="ctw">
                    <div class="ct">{t('servers.props.insights')}</div>
                    <div class="cd">{t('servers.props.insightsDesc')}</div>
                  </div>
                </div>
                {#if insights.length === 0}
                  <div class="spp-ok"><i class="ti ti-circle-check"></i> {t('servers.props.insightsNone')}</div>
                {:else}
                  <div class="spp-insights">
                    {#each insights as ins (ins.id)}
                      <button class="spp-ins {ins.level}" onclick={() => ins.key && jumpTo(ins.key)} disabled={!ins.key}>
                        <i class="ti {insightIcon[ins.level]}"></i>
                        <span class="spp-ins-t">{ins.text}</span>
                        {#if ins.key}<code class="spp-key">{ins.key}</code><i class="ti ti-chevron-right spp-ins-go"></i>{/if}
                      </button>
                    {/each}
                  </div>
                {/if}
              </div>

              <!-- Швидкі налаштування -->
              {#if quickEntries.length > 0}
                <div class="card">
                  <div class="card-head">
                    <div class="ci orange"><i class="ti ti-bolt"></i></div>
                    <div class="ctw">
                      <div class="ct">{t('servers.props.quick')}</div>
                      <div class="cd">{t('servers.props.quickDesc')}</div>
                    </div>
                  </div>
                  {#each quickEntries as entry (entry.key)}
                    {@render propRow(entry, false)}
                  {/each}
                </div>
              {/if}

              <!-- Порт живе в «Налаштуваннях» -->
              <div class="spp-portcard">
                <i class="ti ti-plug"></i>
                <div class="spp-portcard-t">
                  <b>{t('servers.props.portMovedTitle')}</b>
                  <span>{t('servers.props.portMoved', { port: currentPort })}</span>
                </div>
                {#if onOpenSettings}
                  <button class="btn btn-ghost btn-sm" onclick={onOpenSettings}><i class="ti ti-settings"></i> {t('servers.props.openSettings')}</button>
                {/if}
              </div>

            {:else}
              {#each visible as group (group.id)}
                <div class="card">
                  <div class="card-head">
                    <div class="ci"><i class="ti {PROP_GROUP_ICON[group.id] ?? 'ti-dots'}"></i></div>
                    <div class="ctw">
                      <div class="ct">{t(`props.group.${group.id}`)}</div>
                      <div class="cd">{group.id === PROP_LEGACY ? t('servers.props.legacyNote', { v: propsDoc.mcVersion || '—' }) : t(`props.groupDesc.${group.id}`)}</div>
                    </div>
                    {#if groupResettable(group.id) > 0}
                      <button class="btn btn-ghost btn-sm" onclick={() => resetGroupDefaults(group.id)} title={t('servers.props.resetGroupHint')}>
                        <i class="ti ti-restore"></i> {t('servers.props.resetGroup')}
                      </button>
                    {/if}
                  </div>
                  {#each group.items as entry (entry.key)}
                    {@render propRow(entry, group.id === PROP_LEGACY)}
                  {/each}
                </div>
              {/each}
            {/if}
          </div>
        {/key}
      </div>
    </div>

    <!-- Липка панель збереження -->
    <div class="spp-save" class:active={dirty}>
      <button class="btn btn-primary" onclick={save} disabled={saving || !dirty || blockingErrors > 0}>
        {#if saving}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-check"></i>{/if}
        {t('servers.props.save')}
        <kbd class="spp-kbd">Ctrl S</kbd>
      </button>
      {#if dirty}
        <span class="spp-dirty">{t('servers.props.dirty', { n: dirtyEntries.length })}</span>
        {#if blockingErrors > 0}<span class="spp-errtext"><i class="ti ti-alert-circle"></i> {t('servers.props.errCount', { n: blockingErrors })}</span>{/if}
        <button class="btn btn-ghost btn-sm" onclick={revertAll}><i class="ti ti-arrow-back-up"></i> {t('servers.props.revertAll')}</button>
      {:else}
        <span class="spp-dim"><i class="ti ti-info-circle"></i> {t('servers.props.restartHint')}</span>
      {/if}
    </div>
  {/if}
</div>

<style>
  .spp { display: flex; flex-direction: column; min-width: 0; height: 100%; overflow-y: auto; padding: 2px 2px 0; }
  .spp > :global(.settings-grid) { flex: 1; }
  .spp-back { flex-shrink: 0; }
  .spp-search { margin-left: auto; min-width: 260px; max-width: 380px; flex: 1; }
  .spp-x { background: none; border: 0; color: var(--text-dim); cursor: pointer; padding: 2px; display: flex; }
  .spp-x:hover { color: var(--text); }

  /* Меню груп: кількість параметрів і маркери праворуч. */
  .spp :global(.sub-nav.dim .sni) { opacity: .55; }
  .spp-nav-n { margin-left: auto; font: 700 9.5px/1 var(--font-mono); color: var(--text-dim); }
  .spp-nav-dot { width: 7px; height: 7px; border-radius: 50%; background: var(--orange); flex-shrink: 0; box-shadow: 0 0 6px rgba(var(--orange-rgb), .6); }
  .spp-nav-warn { margin-left: auto; width: 8px; height: 8px; border-radius: 50%; background: #eab308; flex-shrink: 0; }
  .spp :global(.sni) { gap: 10px; }

  .spp-found { display: flex; align-items: center; gap: 7px; margin: 0 2px 12px; font: 600 12px/1 var(--font-ui); color: var(--text-mute); }

  /* ── Зведення ── */
  .spp-stats { display: grid; grid-template-columns: repeat(auto-fit, minmax(130px, 1fr)); gap: 10px; margin-bottom: 18px; }
  .spp-stat {
    display: grid; grid-template-columns: 26px 1fr; grid-template-rows: auto auto; column-gap: 9px; align-items: center;
    padding: 12px 14px; border-radius: var(--r-md); background: var(--bg-card); border: 1px solid var(--border);
  }
  .spp-stat > i { grid-row: 1 / span 2; font-size: 20px; color: var(--pcol, var(--orange)); }
  .spp-stat b { font: 800 20px/1.1 var(--font-mono); color: var(--text); }
  .spp-stat span { font: 600 10px/1.2 var(--font-ui); letter-spacing: .4px; text-transform: uppercase; color: var(--text-dim); }
  .spp-stat.hot { border-color: color-mix(in srgb, var(--pcol, var(--orange)) 50%, var(--border)); }
  .spp-stat.bad { border-color: rgba(239, 68, 68, .5); }
  .spp-stat.bad > i, .spp-stat.bad b { color: var(--red); }
  .spp-stat.muted { opacity: .6; }

  /* ── Аналіз ── */
  .spp-ok { display: flex; align-items: center; gap: 9px; padding: 16px 22px; font: 600 12.5px/1.4 var(--font-ui); color: var(--green); }
  .spp-ok i { font-size: 18px; }
  .spp-insights { display: flex; flex-direction: column; }
  .spp-ins {
    display: flex; align-items: center; gap: 11px; width: 100%; text-align: left; cursor: pointer;
    padding: 12px 22px; border: 0; border-bottom: 1px solid var(--hover); background: transparent; color: var(--text);
    font: 500 12.5px/1.45 var(--font-ui); transition: background .14s;
  }
  .spp-ins:last-child { border-bottom: 0; }
  .spp-ins:hover:not(:disabled) { background: var(--hover); }
  .spp-ins:disabled { cursor: default; }
  .spp-ins > i:first-child { font-size: 18px; flex-shrink: 0; }
  .spp-ins.error > i:first-child { color: var(--red); }
  .spp-ins.warn > i:first-child { color: #eab308; }
  .spp-ins.info > i:first-child { color: var(--blue, #3b82f6); }
  .spp-ins-t { flex: 1; min-width: 0; }
  .spp-ins-go { font-size: 14px; color: var(--text-dim); flex-shrink: 0; }

  .spp-portcard {
    display: flex; align-items: center; gap: 13px; flex-wrap: wrap; margin-bottom: 18px;
    padding: 13px 18px; border-radius: var(--r-lg); border: 1px dashed var(--border); background: transparent;
  }
  .spp-portcard > i { font-size: 20px; color: var(--text-dim); }
  .spp-portcard-t { flex: 1; min-width: 200px; display: flex; flex-direction: column; gap: 3px; }
  .spp-portcard-t b { font: 700 12.5px/1.2 var(--font-ui); color: var(--text); }
  .spp-portcard-t span { font: 400 11.5px/1.4 var(--font-ui); color: var(--text-mute); }

  /* ── Рядки параметрів ── */
  .spp-row {
    position: relative; display: grid; grid-template-columns: minmax(0, 1fr) auto; align-items: center; gap: 8px 22px;
    padding: 14px 22px; border-bottom: 1px solid var(--hover); transition: background .25s;
  }
  .spp-row:last-child { border-bottom: none; }
  .spp-row::before { content: ''; position: absolute; left: 0; top: 10px; bottom: 10px; width: 3px; border-radius: 0 3px 3px 0; background: transparent; transition: background .15s; }
  .spp-row.dirty { background: color-mix(in srgb, var(--pcol, var(--orange)) 6%, transparent); }
  .spp-row.dirty::before { background: var(--pcol, var(--orange)); }
  .spp-row.invalid::before { background: var(--red); }
  .spp-row.inactive { opacity: .62; }
  .spp-row:hover { background: var(--hover); }
  .spp-row.flash { animation: spp-flash 1.8s ease-out; }
  @keyframes spp-flash { 0%, 55% { background: color-mix(in srgb, var(--pcol, var(--orange)) 22%, transparent); } 100% { background: transparent; } }
  /* Текстові поля — на всю ширину під описом, а не стиснуті праворуч. */
  .spp-row.wide { grid-template-columns: minmax(0, 1fr); }
  .spp-row.wide .spp-ctl { justify-content: stretch; }
  @media (max-width: 640px) {
    .spp-row { grid-template-columns: minmax(0, 1fr); }
    .spp-ctl { justify-content: flex-start; }
  }
  @media (max-width: 760px) { .spp :global(.settings-grid) { grid-template-columns: 1fr; } .spp :global(.settings-grid .sub-nav) { position: static; max-height: none; flex-direction: row; overflow-x: auto; } }

  .spp-info { min-width: 0; display: flex; flex-direction: column; gap: 4px; }
  .spp-name { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; font: 650 13px/1.3 var(--font-ui); color: var(--text); }
  .spp-badge { display: inline-flex; align-items: center; gap: 4px; font: 600 9.5px/1 var(--font-ui); padding: 3px 7px; border-radius: 20px; color: var(--blue, #3b82f6); background: color-mix(in srgb, var(--blue, #3b82f6) 13%, transparent); border: 1px solid color-mix(in srgb, var(--blue, #3b82f6) 30%, transparent); }
  .spp-badge.mute { color: var(--text-mute); background: var(--hover); border-color: var(--border); }
  .spp-hint { font: 400 11.5px/1.5 var(--font-ui); color: var(--text-mute); max-width: 620px; }
  .spp-meta { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; margin-top: 1px; }
  .spp-key { font: 500 10px/1 var(--font-mono); color: var(--text-dim); padding: 3px 6px; border-radius: 5px; background: var(--bg-input); border: 1px solid var(--border); }
  .spp-chipm { font: 600 10px/1 var(--font-ui); color: var(--text-dim); padding: 3px 0; background: none; border: 0; }
  .spp-chipm.link { cursor: pointer; padding: 3px 7px; border-radius: 5px; border: 1px dashed var(--border); color: var(--text-mute); }
  .spp-chipm.link:hover { color: var(--pcol, var(--orange)); border-color: var(--pcol, var(--orange)); }
  .spp-err { display: inline-flex; align-items: center; gap: 5px; font: 600 11px/1.3 var(--font-ui); color: var(--red); }

  /* ── Контроли ── */
  .spp-ctl { display: flex; align-items: center; justify-content: flex-end; gap: 8px; min-width: 0; }
  .spp-revert { flex: 0 0 auto; background: none; border: 1px solid var(--border); border-radius: 7px; color: var(--text-dim); cursor: pointer; padding: 5px 7px; font-size: 13px; }
  .spp-revert:hover { color: var(--text); border-color: var(--border-hi, var(--border)); }

  .spp-seg { display: inline-flex; padding: 3px; gap: 2px; border-radius: 10px; background: var(--bg-input); border: 1px solid var(--border); }
  .spp-seg button { border: 0; background: none; cursor: pointer; color: var(--text-mute); font: 600 11.5px/1 var(--font-ui); padding: 7px 11px; border-radius: 7px; white-space: nowrap; transition: background .14s, color .14s; }
  .spp-seg button:hover { color: var(--text); }
  .spp-seg button.on { background: color-mix(in srgb, var(--pcol, var(--orange)) 22%, transparent); color: var(--text); box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--pcol, var(--orange)) 55%, transparent); }

  .spp-int { display: inline-flex; align-items: center; gap: 12px; }
  .spp-range { width: 160px; accent-color: var(--pcol, var(--orange)); }
  .spp-stepper { display: inline-flex; align-items: stretch; border: 1px solid var(--border); border-radius: var(--r-md, 10px); background: var(--bg-input); overflow: hidden; }
  .spp-stepper:focus-within { border-color: var(--pcol, var(--orange)); }
  .spp-stepper.bad { border-color: var(--red); }
  .spp-stepper button { border: 0; background: none; cursor: pointer; color: var(--text-dim); padding: 0 9px; font-size: 12px; }
  .spp-stepper button:hover { color: var(--text); background: var(--hover); }
  .spp-stepper input { width: 92px; border: 0; background: none; color: var(--text); text-align: center; font: 600 12px/1 var(--font-mono); padding: 8px 2px; outline: none; }

  .spp-text { position: relative; display: flex; align-items: center; gap: 6px; width: 100%; }
  .spp-text .input { padding: 8px 10px; font-size: 12px; }
  .spp-text .input.bad { border-color: var(--red); }
  .spp-eye { flex: 0 0 auto; background: none; border: 1px solid var(--border); border-radius: 7px; color: var(--text-dim); cursor: pointer; padding: 6px 8px; }
  .spp-eye:hover { color: var(--text); }
  .spp-count { flex: 0 0 auto; font: 600 10.5px/1 var(--font-mono); color: var(--text-dim); }
  .spp-count.over { color: var(--yellow); }

  /* ── Нижня панель збереження (липка) ── */
  .spp-save {
    position: sticky; bottom: 0; z-index: 5; display: flex; align-items: center; gap: 12px; flex-wrap: wrap;
    margin-top: 6px; padding: 12px 18px; border: 1px solid var(--border); border-radius: var(--r-lg);
    background: color-mix(in srgb, var(--bg-card, #151515) 92%, #000); backdrop-filter: blur(12px);
    transition: border-color .2s;
  }
  .spp-save.active { border-color: color-mix(in srgb, var(--pcol, var(--orange)) 55%, var(--border)); }
  .spp-kbd { margin-left: 4px; padding: 2px 5px; border-radius: 5px; font: 600 9.5px/1 var(--font-mono); background: rgba(0, 0, 0, .22); color: inherit; opacity: .75; }
  .spp-dirty { font: 650 11.5px/1 var(--font-ui); color: var(--pcol, var(--orange)); }
  .spp-dim { font: 400 11px/1.4 var(--font-ui); color: var(--text-dim); display: inline-flex; align-items: center; gap: 5px; }
  .spp-errtext { font: 600 11px/1.4 var(--font-ui); color: var(--red); display: inline-flex; align-items: center; gap: 5px; max-width: 460px; word-break: break-word; }

  .spp-empty { display: flex; flex-direction: column; align-items: center; gap: 10px; padding: 50px 20px; color: var(--text-mute); font-size: 13px; text-align: center; }
  .spp-empty i { font-size: 28px; color: var(--text-dim); }
  .spp-empty.small { padding: 30px; }

  .spp-note { display: flex; gap: 10px; align-items: flex-start; margin: 0 0 14px; padding: 10px 14px; border-radius: 11px; border: 1px solid var(--border); font: 400 12px/1.5 var(--font-ui); color: var(--text-mute); }
  .spp-note i { font-size: 16px; flex: 0 0 auto; margin-top: 1px; }
  .spp-note.warn { border-color: rgba(234, 179, 8, .4); background: rgba(234, 179, 8, .08); color: var(--text); }
  .spp-note.warn i { color: #eab308; }

  @media (prefers-reduced-motion: reduce) { .spp-row.flash { animation: none; } }
</style>
