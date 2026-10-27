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
  import { fade, scale } from 'svelte/transition'
  import { App } from '../../wails'
  import { t } from '../../i18n.svelte'
  import { toast } from '../../toast.svelte'
  import Portal from '../../Portal.svelte'
  import PackPicker, { type PackPickerItem, type PackRow, type PackFolderRow } from '../../PackPicker.svelte'
  import type { Build, PackFolder } from '../../types'

  let {
    address = '',
    label = '',
    onclose,
  }: { address: string; label?: string; onclose: () => void } = $props()

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
      App.GetBuilds().then((views) => (views ?? []).map((v) => v.build).filter((b) => !!(b && b.id))).catch(() => [] as Build[]),
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
    if (mode === 'play') play(b); else addToList(b)
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

<Portal>
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
  <div class="pjm-back" transition:fade={{ duration: 140 }} onclick={onclose}>
    <div class="pjm" transition:scale={{ duration: 170, start: 0.96 }} onclick={(e) => e.stopPropagation()} role="dialog" aria-modal="true" aria-label={t('servers.p2p.play.title')}>
      <div class="pjm-h">
        <i class="ti ti-device-gamepad-2"></i>
        <div class="pjm-ht">
          <b>{t('servers.p2p.play.title')}</b>
          <code title={address}>{address}</code>
        </div>
        <button class="pjm-x" onclick={onclose} aria-label={t('cancel')}><i class="ti ti-x"></i></button>
      </div>

      <div class="pjm-sub">{mode === 'play' ? t('servers.p2p.play.sub') : t('servers.p2p.play.subAdd')}</div>

      <div class="pjm-modes">
        <button class:on={mode === 'play'} onclick={() => (mode = 'play')}>
          <i class="ti ti-player-play"></i> {t('servers.p2p.play.modePlay')}
        </button>
        <button class:on={mode === 'add'} onclick={() => (mode = 'add')}>
          <i class="ti ti-playlist-add"></i> {t('servers.p2p.play.modeAdd')}
        </button>
      </div>

      <div class="pjm-search">
        <i class="ti ti-search"></i>
        <input bind:value={query} placeholder={t('servers.p2p.play.search')} spellcheck="false" />
      </div>

      <div class="pjm-list">
        {#if loading}
          <div class="pjm-empty"><i class="ti ti-loader spin"></i></div>
        {:else}
          <PackPicker
            items={picker.items}
            rowById={picker.rowById}
            folderById={picker.folderById}
            {query}
            selectable={!busy}
            onselect={pick}
            emptyText={t('servers.p2p.play.noPacks')}
          />
        {/if}
      </div>
    </div>
  </div>
</Portal>

<style>
  .pjm-back { position: fixed; inset: 0; z-index: 460; display: grid; place-items: center; background: rgba(0, 0, 0, .62); backdrop-filter: blur(3px); }
  .pjm { width: min(560px, 94vw); max-height: 84vh; display: flex; flex-direction: column; gap: 12px; padding: 18px; border-radius: 16px; background: var(--bg-panel); border: 1px solid var(--border); box-shadow: 0 20px 60px rgba(0, 0, 0, .55); }
  .pjm-h { display: flex; align-items: center; gap: 10px; }
  .pjm-h > i { font-size: 20px; color: var(--pcol, var(--orange)); }
  .pjm-ht { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 3px; }
  .pjm-ht b { font: 700 14.5px/1.2 var(--font-ui); color: var(--text); }
  .pjm-ht code { font: 500 11.5px/1.2 var(--font-mono); color: var(--text-mute); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .pjm-x { width: 28px; height: 28px; border-radius: 8px; cursor: pointer; background: var(--bg-input); border: 1px solid var(--border); color: var(--text-mute); }
  .pjm-x:hover { color: var(--text); background: var(--hover-strong); }
  .pjm-sub { font: 400 11.5px/1.45 var(--font-ui); color: var(--text-mute); }
  .pjm-modes { display: flex; gap: 4px; padding: 4px; border-radius: 12px; background: var(--bg-card); border: 1px solid var(--border); }
  .pjm-modes button { flex: 1; display: inline-flex; align-items: center; justify-content: center; gap: 7px; padding: 9px 8px; border: 0; border-radius: 9px; cursor: pointer; background: transparent; color: var(--text-mute); font: 600 12.5px/1 var(--font-ui); }
  .pjm-modes button:hover { color: var(--text); }
  .pjm-modes button.on { background: color-mix(in srgb, var(--pcol, var(--orange)) 14%, transparent); color: var(--text); font-weight: 700; }
  .pjm-search { position: relative; display: flex; align-items: center; }
  .pjm-search > i { position: absolute; left: 11px; color: var(--text-dim); font-size: 14px; }
  .pjm-search input { width: 100%; padding: 9px 12px 9px 32px; border-radius: 11px; background: var(--bg-input); border: 1px solid var(--border); color: var(--text); font: 500 12.5px/1 var(--font-ui); outline: none; }
  .pjm-search input:focus { border-color: var(--border-hi); }
  .pjm-list { flex: 1; min-height: 120px; overflow-y: auto; display: flex; flex-direction: column; gap: 3px; }
  .pjm-empty { display: grid; place-items: center; padding: 26px; font: 500 12px/1.3 var(--font-ui); color: var(--text-dim); }
</style>
