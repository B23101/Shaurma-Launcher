<script lang="ts">
  // ── «Грати через P2P»: вибір збірки для P2P-адреси ─────────────────────
  // Модалка для ДВОХ дій з адресою P2P-кімнати (спільної або запрошення):
  //   1. Запустити обрану збірку з ПРЯМИМ підключенням через цю адресу
  //      (App.LaunchInstanceOnServer — гра одразу відкриває цей сервер);
  //   2. Додати адресу у СПИСОК СЕРВЕРІВ збірки ТИМЧАСОВО
  //      (App.AddTempPackServer). Лаунчер пам'ятає, що запис тимчасовий, і
  //      прибирає його сам, коли доступу немає (App.SyncTempPackServers) —
  //      саме тому такий сервер у списку позначений міткою.
  //
  // Навіщо кнопка запуску: раніше, щоб зайти в чужу P2P-кімнату, треба було
  // вручну скопіювати адресу і вставити її в «Пряме підключення» в грі.
  import { App } from '../../wails'
  import { t } from '../../i18n.svelte'
  import { toast } from '../../toast.svelte'
  import UiModal from '../../UiModal.svelte'
  import '../../settings/settings.css'
  import PackPicker, { type PackPickerItem, type PackRow, type PackFolderRow } from '../../PackPicker.svelte'
  import type { Build, PackFolder, BuildView, ServerPing } from '../../types'

  let {
    address = '',
    label = '',
    onclose,
  }: { address: string; label?: string; onclose: () => void } = $props()

  let views = $state<BuildView[]>([])
  let chosen = $state<Build | null>(null)
  let ping = $state<ServerPing | null>(null)
  let checking = $state(false)
  let allowUnverified = $state(false)
  let checkError = $state('')
  let builds = $state<Build[]>([])
  let folders = $state<PackFolder[]>([])
  let layout = $state<{ order: string[]; pinned: string[] }>({ order: [], pinned: [] })
  let loading = $state(true)
  let busy = $state('')
  let query = $state('')
  // Яку саме дію виконуємо: гра одразу чи додати у список збірки.
  let mode = $state<'play' | 'add'>('play')

  // Дані для PackPicker: той самий порядок збірок і папок, що в sidebar
  // (закріплені → порядок з layout → нові), усі збірки з id.
  const picker = $derived.by(() => {
    const rowById = new Map<string, PackRow>()
    for (const b of builds) {
      rowById.set(b.id, { name: b.name, color: b.color, iconUrl: b.iconUrl, icon: b.icon, loader: b.loaderType, mcVersion: b.mcVersion })
    }
    const folderById = new Map<string, PackFolderRow>()
    const inFolder = new Set<string>()
    for (const f of folders) {
      const packIds = f.packIds.filter((id) => rowById.has(id))
      folderById.set(f.id, { name: f.name, color: f.color, packIds })
      for (const id of packIds) inFolder.add(id)
    }
    const items: PackPickerItem[] = []
    const placed = new Set<string>()
    const addPack = (id: string) => {
      if (!id || placed.has(id) || inFolder.has(id) || !rowById.has(id)) return
      items.push({ kind: 'pack', id }); placed.add(id)
    }
    const addFolder = (id: string) => {
      if (!id || placed.has(id) || !folderById.has(id)) return
      items.push({ kind: 'folder', id }); placed.add(id)
    }
    for (const id of layout.pinned) addPack(id)
    for (const id of layout.order) { if (folderById.has(id)) addFolder(id); else addPack(id) }
    for (const f of folders) addFolder(f.id)
    for (const b of builds) addPack(b.id)
    return { items, rowById, folderById }
  })

  $effect(() => {
    Promise.all([
      App.GetBuilds().then((list) => { views = list ?? []; return views.map((v) => v.build).filter((b) => !!(b && b.id)) }).catch(() => [] as Build[]),
      App.GetPackFolders().catch(() => [] as PackFolder[]),
      App.GetPackLayout().catch(() => ({ order: [] as string[], pinned: [] as string[] })),
    ]).then(([b, f, l]) => {
      builds = b
      folders = Array.isArray(f) ? f : []
      layout = { order: l?.order ?? [], pinned: l?.pinned ?? [] }
    }).finally(() => { loading = false })
  })

  function pick(id: string) {
    const b = builds.find((x) => x.id === id)
    if (!b) return
    if (mode === 'play') check(b); else addToList(b)
  }

  const selectedView = $derived(views.find(v => v.build.id === chosen?.id))
  const serverVersions = $derived(ping?.version?.match(/\b1\.\d+(?:\.\d+)?\b/g) ?? [])
  const mismatch = $derived(!!chosen?.mcVersion && serverVersions.length === 1 && serverVersions[0] !== chosen.mcVersion)
  async function check(b: Build) {
    if (checking || busy) return
    chosen = b; checking = true; ping = null; checkError = ''; allowUnverified = false
    try {
      const [result, current] = await Promise.all([App.PingServer(address), App.GetBuilds()])
      ping = result; views = current ?? []
    } catch (e) { checkError = String(e) }
    finally { checking = false }
  }

  async function play(b: Build) {
    if (busy) return
    busy = b.id
    try {
      await App.LaunchInstanceOnServer(b.id, address)
      toast(t('servers.p2p.play.launching', { pack: b.name }), 'info')
      onclose()
    } catch (e: any) {
      if (/already running/i.test(String(e?.message ?? e))) {
        // Збірка вже грає: не лякаємо англійською помилкою, а копіюємо IP —
        // його лишається вставити в «Пряме підключення» у відкритій грі.
        try { await App.CopyToClipboard(address) } catch { /* нічого */ }
        toast(`«${b.name}» уже запущена — IP ${address} скопійовано. Вставте його в «Пряме підключення» в грі`, 'info')
        onclose()
      } else {
        toast(String(e?.message ?? e), 'error')
      }
    } finally { busy = '' }
  }

  async function addToList(b: Build) {
    if (busy) return
    busy = b.id
    try {
      const name = label ? `${label} (P2P)` : address
      await App.AddTempPackServer(b.id, name, address)
      toast(t('servers.p2p.play.added', { pack: b.name }), 'success')
      onclose()
    } catch (e: any) {
      toast(String(e?.message ?? e), 'error')
    } finally { busy = '' }
  }
</script>

<UiModal open={true} {onclose} icon="ti-device-gamepad-2" title={t('servers.p2p.play.title')} subtitle={address}
         width="min(560px, 94vw)" maxHeight="84vh" bodyClass="pjm-body">
  <div class="pjm-sub">{mode === 'play' ? t('servers.p2p.play.sub') : t('servers.p2p.play.subAdd')}</div>

  <div class="pjm-modes">
    <button class:on={mode === 'play'} onclick={() => (mode = 'play')}>
      <i class="ti ti-player-play"></i> {t('servers.p2p.play.modePlay')}
    </button>
    <button class:on={mode === 'add'} onclick={() => (mode = 'add')}>
      <i class="ti ti-playlist-add"></i> {t('servers.p2p.play.modeAdd')}
    </button>
  </div>

  <div class="st-search pjm-search">
    <i class="ti ti-search st-search-ic"></i>
    <input bind:value={query} placeholder={t('servers.p2p.play.search')} spellcheck="false" />
  </div>

  {#if chosen && mode === 'play'}
    <section class="readiness" aria-live="polite" aria-busy={checking}>
      <strong>Готовність: {chosen.name}</strong>
      {#if checking}<p>Перевіряємо сервер і збірку…</p>
      {:else}
        <p>{ping?.online ? '✓ Сервер відповідає' : 'Сервер не відповів. Перевірте, чи друг увімкнув його та надав доступ.'}</p>
        <p>{selectedView?.installed ? '✓ Збірка встановлена' : 'Файли збірки потрібно підготувати під час запуску'}</p>
        {#if selectedView?.status === 'needs-update'}<p>Перед запуском потрібне оновлення збірки.</p>{/if}
        <p>Minecraft збірки: {chosen.mcVersion || 'невідомо'} · сервер: {ping?.version || 'невідомо'}</p>
        {#if mismatch}<p>Версії Minecraft відрізняються. Оберіть відповідну збірку нижче або змініть її версію на сторінці збірки.</p>{/if}
        <p>Склад модів сервер не повідомляє: переконайтеся, що обрана та сама збірка, що й у друга.</p>
        {#if checkError}<p role="alert">{checkError}</p>{/if}
        {#if !ping?.online || mismatch}
          <label class="pjm-sub"><input type="checkbox" bind:checked={allowUnverified} disabled={!!busy} /> Спробувати вхід попри непідтверджену сумісність або недоступний status ping</label>
        {/if}
        <div class="pjm-modes">
          <button disabled={!!busy} onclick={() => check(chosen!)}>Перевірити знову</button>
          <button class="on" disabled={!!busy || ((!ping?.online || mismatch) && !allowUnverified) || !selectedView || ['running', 'downloading', 'updating'].includes(selectedView.status)} onclick={() => play(chosen!)}>{busy ? 'Готуємо запуск…' : 'Підготувати й грати'}</button>
        </div>
      {/if}
    </section>
  {/if}

  <div class="pjm-list">
    {#if loading}
      <div class="pjm-empty"><i class="ti ti-loader spin"></i></div>
    {:else}
      <PackPicker
        items={picker.items}
        rowById={picker.rowById}
        folderById={picker.folderById}
        {query}
        selectable={!busy && !checking}
        onselect={pick}
        emptyText={t('servers.p2p.play.noPacks')}
      />
    {/if}
  </div>
</UiModal>

<style>
  .readiness { padding:14px; border:1px solid var(--border-hi); border-radius:12px; background:var(--bg-card); color:var(--text); }
  .readiness p { font-size:12px; line-height:1.5; color:var(--text-mute); overflow-wrap:anywhere; }
  button:focus-visible { outline:2px solid var(--orange); outline-offset:2px; }
  .pjm-sub { font: 400 12px/1.45 var(--font-ui); color: var(--text-mute); }
  .pjm-modes { display: flex; gap: 3px; margin-top: 12px; padding: 3px; border-radius: 12px; background: var(--bg-input); border: 1px solid var(--border-hi); }
  .pjm-modes button { flex: 1; display: inline-flex; align-items: center; justify-content: center; gap: 7px; padding: 9px 8px; border: 1px solid transparent; border-radius: 9px; cursor: pointer; background: transparent; color: var(--text-mute); font: 600 12.5px/1 var(--font-ui); }
  .pjm-modes button:hover { color: var(--text); }
  .pjm-modes button.on { background: var(--bg-card); color: var(--orange); border-color: rgba(var(--orange-rgb), .5); }
  .pjm-search { max-width: none; margin: 12px 0; }
  .pjm-list { min-height: 120px; display: flex; flex-direction: column; gap: 3px; }
  .pjm-empty { display: grid; place-items: center; padding: 26px; font: 500 12px/1.3 var(--font-ui); color: var(--text-dim); }
</style>
