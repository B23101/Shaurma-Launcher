<script lang="ts">
  import Portal from './Portal.svelte'
  import UiModal from './UiModal.svelte'
  import './settings/settings.css'
  // ── Галерея скріншотів збірки (вкладка «Галерея») ─────────────────────
  // Сітка всіх скріншотів з <InstanceDir>/<packID>/.minecraft/screenshots.
  // ЛКМ по картці — відкриває фоторедактор на весь екран (ScreenshotEditor).
  // ПКМ — контекстне меню: копіювати, видалити, показати у Провіднику,
  // відкрити у типовій програмі перегляду Windows.
  //
  // Оновлюється в реальному часі: підписка на подію 'gallery:new-
  // screenshot' (бекенд емітить її, поки збірка запущена і гравець робить
  // F2-скрін) + легкий polling підпису теки (ScreenshotsSignature) кожні
  // кілька секунд, поки вкладка відкрита — той самий підхід, що
  // PackServersSection (5с тик, порівняння підпису перед повним
  // перечитуванням списку).
  import { onDestroy } from 'svelte'
  import { t } from './i18n.svelte'
  import { App, on } from './wails'
  import { toast, confirmDialog } from './toast.svelte'
  import { smartImage } from './smartImage'
  import { windowVisibility } from './visible.svelte'
  import type { ScreenshotEntry, NewScreenshotEvent } from './types'
  import ScreenshotEditor from './ScreenshotEditor.svelte'

  let { packId, packName = '' } = $props<{ packId: string; packName?: string }>()

  let entries = $state<ScreenshotEntry[]>([])
  let loading = $state(true)
  let lastSignature = $state('')
  // Асети (data URL мініатюр) кешуються за (ім'я файлу + mtime), щоб
  // сітка не сипала повторні запити на кожен ререндер — АЛЕ перезаписаний
  // скріншот (після редагування) давав СТАРУ картинку назавжди, бо ключ
  // був лише іменем файлу: polling бачив новий mtime, список оновлювався,
  // а кеш мініатюри зі старим вмістом лишався (баг «картинка не
  // оновлюється на актуальну»). mtime у ключі змушує перегенерувати
  // мініатюру після будь-якої зміни файлу.
  let thumbCache = $state<Record<string, string>>({})

  function thumbKey(entry: ScreenshotEntry): string {
    return entry.fileName + ':' + (entry.modifiedTs || 0)
  }

  async function load() {
    try {
      entries = await App.ListScreenshots(packId)
    } catch {
      entries = []
    }
    loading = false
  }

  // ── Черга завантаження мініатюр з обмеженим паралелізмом ──────────────
  // Бекенд віддає ЗМЕНШЕНУ мініатюру (GetScreenshotThumbnail, ≤320px,
  // JPEG, кешується на диск), а фронтенд тримає лише thumbConcurrency
  // запитів одночасно, решта чекає в черзі — плавне поступове заповнення
  // сітки замість одного важкого ривка.
  const thumbConcurrency = 4
  let thumbQueue: string[] = []
  let thumbActive = 0
  // Токен поколінь — інкрементується ЛИШЕ при реальній зміні списку
  // (новий load(): інша збірка або перечит теки), щоб мініатюри зі
  // СТАРОГО списку (що вже летять у черзі) не записались у кеш.
  let thumbGen = 0
  // Останній «підпис» списку (fileName:mtime, склеєні через |) — за ним
  // ефект нижче відрізняє справжню зміну списку від оновлення thumbCache.
  let lastListKey = ''

  function enqueueThumb(entry: ScreenshotEntry) {
    const key = thumbKey(entry)
    if (thumbCache[key] || thumbQueue.includes(key)) return
    thumbQueue.push(key)
    pumpThumbQueue()
  }

  function pumpThumbQueue() {
    const gen = thumbGen
    while (thumbActive < thumbConcurrency && thumbQueue.length > 0) {
      const key = thumbQueue.shift()!
      thumbActive++
      // Ключ = fileName:mtime — запит іде по імені файлу, а кеш пишеться
      // під ключ з mtime (згенерована мініатюра відповідає саме цьому
      // вмісту файлу).
      const fileName = key.slice(0, key.lastIndexOf(':'))
      App.GetScreenshotThumbnail(packId, fileName)
        .then(url => {
          if (gen === thumbGen) thumbCache = { ...thumbCache, [key]: url }
        })
        .catch(() => {
          // тихо ігноруємо — картка покаже плейсхолдер-іконку
        })
        .finally(() => {
          thumbActive--
          pumpThumbQueue()
        })
    }
  }

  // Реакція на ЗМІНУ СПИСКУ (нова збірка / перечит теки): піднімаємо
  // покоління, чистимо чергу, прунимо кеш до поточних файлів. Саме
  // завантаження мініатюр робить lazyThumb (IntersectionObserver) — лише
  // для карток, що наблизились до в'юпорту.
  //
  // ВАЖЛИВО (причина зависання галереї): раніше цей ефект БЕЗУМОВНО писав
  // thumbCache (новий об'єкт), хоча й читав його ж як залежність. Svelte 5
  // перевиконує ефект, який пише у власну залежність (у runes-режимі
  // активний ефект не пропускається при позначці реакцій) — виходив
  // нескінченний цикл: при відкритті «Галереї» ефект крутився ~1000 разів
  // (поки Svelte не кидав effect_update_depth_exceeded), на кожній ітерації
  // перезапускаючи чергу мініатюр і пускаючи нові запити на повне
  // декодування скріншотів у бекенд — UI замерзав, галерея не вантажилась.
  // Тепер: (1) ефект завершується одразу (guard за підписом списку),
  // (2) thumbCache пишеться лише коли прунінг реально щось видалив,
  // (3) оновлення кешу мініатюр більше НЕ перезапускає чергу (запити, що
  // вже летять, доїжджають і застосовуються під своїм поколінням).
  $effect(() => {
    const key = entries.map(thumbKey).join('|')
    if (key === lastListKey) return
    lastListKey = key
    thumbGen++
    thumbQueue = []
    const valid = new Set(entries.map(thumbKey))
    const pruned: Record<string, string> = {}
    for (const k of Object.keys(thumbCache)) if (valid.has(k)) pruned[k] = thumbCache[k]
    if (Object.keys(pruned).length !== Object.keys(thumbCache).length) {
      thumbCache = pruned
    }
  })

  // Лазі-завантаження мініатюри: запит іде лише коли картка наближається
  // до в'юпорту (rootMargin 400px) — при сотні скріншотів не качаються всі
  // одразу, сітка заповнюється в міру скролу. update() викликається при
  // перечиту списку (новий mtime/файл) — перевіряємо кеш заново; якщо
  // запит упав (transient), наступний перетин картки (скрол туди-сюди)
  // повторить спробу, бо спостерігач лишається активним.
  function lazyThumb(node: HTMLElement, entry: ScreenshotEntry) {
    let io: IntersectionObserver | null = null
    function ensure() {
      if (io) return
      if (thumbCache[thumbKey(entry)] || thumbQueue.includes(thumbKey(entry))) return
      io = new IntersectionObserver(es => {
        for (const e of es) {
          if (e.isIntersecting) {
            enqueueThumb(entry)
            break
          }
        }
      }, { rootMargin: '400px' })
      io.observe(node)
    }
    ensure()
    return {
      update(next: ScreenshotEntry) {
        entry = next
        io?.disconnect()
        io = null
        ensure()
      },
      destroy() {
        io?.disconnect()
      }
    }
  }

  // ── Реальний час: подія від бекенду (гра запущена, зроблено F2) ──
  const unsubNew = on('gallery:new-screenshot', (data: NewScreenshotEvent) => {
    if (data.packId !== packId) return
    if (entries.some(e => e.fileName === data.entry.fileName)) return
    entries = [data.entry, ...entries]
  })

  // ── Polling підпису теки, поки панель відкрита (ловить зовнішні зміни:
  //    видалення файлу вручну, перейменування поза лаунчером тощо) ──
  let pollTimer: ReturnType<typeof setInterval> | undefined
  async function pollTick() {
    // Оптимізація (розділ 3): вікно згорнуте/приховане — поллінг теки
    // не потрібен, пропускаємо тик (F2-скріни ловить подія
    // gallery:new-screenshot, вона прийде при поверненні у фокус).
    if (!windowVisibility.visible) return
    try {
      const sig = await App.ScreenshotsSignature(packId)
      if (sig !== lastSignature) {
        lastSignature = sig
        await load()
      }
    } catch {
      // тека могла зникнути (перевстановлення збірки) — просто ігноруємо тик
    }
  }

  $effect(() => {
    if (!packId) return
    loading = true
    load().then(() => { App.ScreenshotsSignature(packId).then(s => lastSignature = s).catch(() => {}) })
    pollTimer = setInterval(pollTick, 4000)
    return () => { if (pollTimer) clearInterval(pollTimer) }
  })

  onDestroy(() => { unsubNew() })

  function fmtDate(rfc: string): string {
    const d = new Date(rfc)
    if (isNaN(d.getTime())) return ''
    return d.toLocaleString(undefined, { day: '2-digit', month: '2-digit', year: 'numeric', hour: '2-digit', minute: '2-digit' })
  }

  // ── Контекстне меню (ПКМ) ──
  let ctxMenu = $state<{ entry: ScreenshotEntry; x: number; y: number } | null>(null)

  function openCtx(e: MouseEvent, entry: ScreenshotEntry) {
    e.preventDefault()
    const W = 220, H = 260
    ctxMenu = {
      entry,
      x: Math.max(4, Math.min(e.clientX, window.innerWidth - W - 8)),
      y: Math.max(4, Math.min(e.clientY, window.innerHeight - H - 8)),
    }
  }

  $effect(() => {
    if (!ctxMenu) return
    const close = () => { ctxMenu = null }
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') ctxMenu = null }
    window.addEventListener('click', close)
    window.addEventListener('blur', close)
    window.addEventListener('keydown', onKey)
    return () => {
      window.removeEventListener('click', close)
      window.removeEventListener('blur', close)
      window.removeEventListener('keydown', onKey)
    }
  })

  async function ctxAction(act: string, entry: ScreenshotEntry) {
    ctxMenu = null
    switch (act) {
      case 'open':
        openEditor(entry)
        break
      case 'copy':
        try {
          await App.CopyScreenshotToClipboard(packId, entry.fileName)
          toast(t('gallery.copied'), 'success')
        } catch (e: any) {
          toast(t('error.withMessage', { message: e }), 'error')
        }
        break
      case 'copyPath':
        try {
          await App.CopyScreenshotPathToClipboard(packId, entry.fileName)
          toast(t('gallery.pathCopied'), 'success')
        } catch (e: any) {
          toast(t('error.withMessage', { message: e }), 'error')
        }
        break
      case 'reveal':
        try { await App.RevealScreenshot(packId, entry.fileName) } catch (e: any) { toast(t('error.withMessage', { message: e }), 'error') }
        break
      case 'openViewer':
        try { await App.OpenScreenshotInDefaultApp(packId, entry.fileName) } catch (e: any) { toast(t('error.withMessage', { message: e }), 'error') }
        break
      case 'rename':
        openRename(entry)
        break
      case 'details':
        openDetails(entry)
        break
      case 'delete':
        await deleteEntry(entry)
        break
    }
  }

  // ── Перейменування ──
  let renamePrompt = $state<{ entry: ScreenshotEntry } | null>(null)
  let renameValue = $state('')

  function openRename(entry: ScreenshotEntry) {
    renameValue = entry.fileName
    renamePrompt = { entry }
  }

  async function submitRename() {
    const p = renamePrompt
    if (!p) return
    renamePrompt = null
    const name = renameValue.trim()
    if (!name || name === p.entry.fileName) return
    try {
      await App.RenameScreenshot(packId, p.entry.fileName, name)
      toast(t('gallery.renamed'), 'success')
      await load()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  // ── Підпис/опис ──
  let detailsPrompt = $state<{ entry: ScreenshotEntry } | null>(null)
  let detailsTitle = $state('')
  let detailsDesc = $state('')

  function openDetails(entry: ScreenshotEntry) {
    detailsTitle = entry.title ?? ''
    detailsDesc = entry.description ?? ''
    detailsPrompt = { entry }
  }

  async function submitDetails() {
    const p = detailsPrompt
    if (!p) return
    detailsPrompt = null
    try {
      await App.SetScreenshotMeta(packId, p.entry.fileName, detailsTitle.trim(), detailsDesc.trim())
      toast(t('gallery.saved'), 'success')
      await load()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  async function deleteEntry(entry: ScreenshotEntry) {
    const ok = await confirmDialog(
      t('gallery.deleteConfirm', { name: entry.title || entry.fileName }),
      t('gallery.deleteConfirmHint'),
      { confirmLabel: t('gallery.delete'), cancelLabel: t('cancel'), danger: true }
    )
    if (!ok) return
    try {
      await App.DeleteScreenshot(packId, entry.fileName)
      entries = entries.filter(e => e.fileName !== entry.fileName)
      toast(t('gallery.deleted'), 'success')
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    }
  }

  // ── Редактор (повноекранний) ──
  let editing = $state<ScreenshotEntry | null>(null)

  function openEditor(entry: ScreenshotEntry) {
    editing = entry
  }

  function closeEditor() {
    editing = null
  }

  async function onEditorSaved() {
    await load()
  }
</script>

{#if loading && entries.length === 0}
  <div class="card"><div style="padding:24px;text-align:center;color:var(--text-mute);font-size:12px"><i class="ti ti-loader spin"></i> {t('gallery.loading')}</div></div>
{:else if entries.length === 0}
  <div class="card">
    <div style="padding:36px 24px;text-align:center;color:var(--text-mute)">
      <i class="ti ti-photo-off empty-float" style="font-size:32px;opacity:.5;display:block;margin-bottom:10px"></i>
      <div style="font-size:13px;font-weight:700;color:var(--text);margin-bottom:4px">{t('gallery.empty')}</div>
      <div style="font-size:12px">{t('gallery.emptyHint')}</div>
    </div>
  </div>
{:else}
  <div class="gal-grid">
    {#each entries as entry, i (entry.fileName)}
      <div class="gal-card rise-in" use:lazyThumb={entry} style="animation-delay:{Math.min(i, 8) * 20}ms" onclick={() => openEditor(entry)} oncontextmenu={(e) => openCtx(e, entry)}>
        <div class="gal-thumb">
          {#if thumbCache[thumbKey(entry)]}
            <img src={thumbCache[thumbKey(entry)]} alt={entry.fileName} loading="lazy" use:smartImage />
          {:else}
            <i class="ti ti-photo"></i>
          {/if}
        </div>
        <div class="gal-meta">
          <div class="gal-title" title={entry.title || entry.fileName}>{entry.title || entry.fileName}</div>
          <div class="gal-date">{fmtDate(entry.modified)}</div>
        </div>
        <button class="gal-more" title={t('gallery.editDetails')} onclick={(e: any) => { e.stopPropagation(); openCtx(e, entry) }}>
          <i class="ti ti-dots-vertical"></i>
        </button>
      </div>
    {/each}
  </div>
{/if}

<!-- ── Контекстне меню ── -->
{#if ctxMenu}
  <div class="ctx-menu" style="left:{ctxMenu.x}px;top:{ctxMenu.y}px" oncontextmenu={(e) => e.preventDefault()}>
    <div class="ctx-item" onclick={() => ctxAction('open', ctxMenu!.entry)}>
      <i class="ti ti-edit"></i> {t('gallery.edit')}
    </div>
    <div class="ctx-item" onclick={() => ctxAction('copy', ctxMenu!.entry)}>
      <i class="ti ti-copy"></i> {t('gallery.copy')}
    </div>
    <div class="ctx-item" onclick={() => ctxAction('copyPath', ctxMenu!.entry)}>
      <i class="ti ti-link"></i> {t('gallery.copyPath')}
    </div>
    <div class="ctx-item" onclick={() => ctxAction('reveal', ctxMenu!.entry)}>
      <i class="ti ti-folder-open"></i> {t('gallery.reveal')}
    </div>
    <div class="ctx-item" onclick={() => ctxAction('openViewer', ctxMenu!.entry)}>
      <i class="ti ti-photo-search"></i> {t('gallery.openInViewer')}
    </div>
    <div class="ctx-item" onclick={() => ctxAction('details', ctxMenu!.entry)}>
      <i class="ti ti-notes"></i> {t('gallery.editDetails')}
    </div>
    <div class="ctx-item" onclick={() => ctxAction('rename', ctxMenu!.entry)}>
      <i class="ti ti-pencil"></i> {t('gallery.rename')}
    </div>
    <div class="ctx-item danger" onclick={() => ctxAction('delete', ctxMenu!.entry)}>
      <i class="ti ti-trash"></i> {t('gallery.delete')}
    </div>
  </div>
{/if}

<!-- ── Перейменування ── -->
{#if renamePrompt}
  <UiModal open={true} onclose={() => renamePrompt = null} size="sm" icon="ti-pencil" title={t('gallery.rename')}>
    <div class="rl-name" style="margin-bottom:8px">{t('gallery.renameTo')}</div>
    <!-- svelte-ignore a11y_autofocus -->
    <input class="st-input mono grow" value={renameValue} oninput={(e: any) => renameValue = e.target.value}
           onkeydown={(e: any) => { if (e.key === 'Enter') submitRename() }} autofocus />
    {#snippet footer()}
      <button class="btn btn-ghost" onclick={() => renamePrompt = null}>{t('gallery.cancel')}</button>
      <button class="btn btn-primary" onclick={submitRename} disabled={!renameValue.trim()}>{t('gallery.save')}</button>
    {/snippet}
  </UiModal>
{/if}

<!-- ── Підпис/опис ── -->
{#if detailsPrompt}
  <UiModal open={true} onclose={() => detailsPrompt = null} icon="ti-file-description" title={t('gallery.editDetails')}>
    <div class="rl-name" style="margin-bottom:6px">{t('gallery.title')}</div>
    <!-- svelte-ignore a11y_autofocus -->
    <input class="st-input grow" placeholder={t('gallery.titlePlaceholder')} value={detailsTitle}
           oninput={(e: any) => detailsTitle = e.target.value} style="margin-bottom:14px" autofocus />
    <div class="rl-name" style="margin-bottom:6px">{t('gallery.description')}</div>
    <textarea class="st-input grow" placeholder={t('gallery.descriptionPlaceholder')} rows="4"
              value={detailsDesc}
              oninput={(e: any) => detailsDesc = e.target.value}></textarea>
    {#snippet footer()}
      <button class="btn btn-ghost" onclick={() => detailsPrompt = null}>{t('gallery.cancel')}</button>
      <button class="btn btn-primary" onclick={submitDetails}>{t('gallery.save')}</button>
    {/snippet}
  </UiModal>
{/if}

<!-- ── Фоторедактор (повноекранний). ОБОВ'ЯЗКОВО в <Portal>: інакше
     position:fixed всередині контейнера (таб панелі) ламається — редактор
     рендерився не на весь екран, а всередині вкладки. Portal підносить
     його до body, і overlay перекриває все вікно. -->
{#if editing}
  <Portal>
    <ScreenshotEditor
      packId={packId}
      entry={editing}
      onClose={closeEditor}
      onSaved={onEditorSaved}
    />
  </Portal>
{/if}

<style>

  .gal-grid {
    display:grid; grid-template-columns:repeat(auto-fill, minmax(200px, 1fr)); gap:14px;
  }
  /* Оптимізація (розділ 5): картки поза в'юпортом не рендеряться
     браузером — легші пам'ять/рендер у довгій галереї скріншотів. */
  .gal-card { content-visibility: auto; contain-intrinsic-size: auto 180px; }
  .gal-card {
    position:relative; border-radius:var(--r-md); overflow:hidden; cursor:pointer;
    background:var(--bg-input); border:1px solid var(--border); transition:border-color .15s, transform .15s;
  }
  .gal-card:hover { border-color:var(--border-hi); transform:translateY(-2px); }
  .gal-thumb {
    aspect-ratio:16/9; display:flex; align-items:center; justify-content:center;
    background:#000; color:var(--text-dim); font-size:26px; overflow:hidden;
  }
  .gal-thumb img {
    width:100%; height:100%; object-fit:cover; display:block;
    /* Плавна поява мініатюри замість «стрибка»: картинка приходить data URL
       асинхронно і раніше просто з'являлась на місці плейсхолдера. */
    animation:gal-thumb-in .25s ease-out both;
  }
  @keyframes gal-thumb-in {
    from { opacity:0; transform:scale(.96); }
    to   { opacity:1; transform:none; }
  }
  @media (prefers-reduced-motion: reduce) {
    .gal-thumb img { animation:none; }
  }
  .gal-meta { padding:8px 10px; }
  .gal-title { font-size:12px; font-weight:700; color:var(--text); overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
  .gal-date { font-size:var(--fs-min); color:var(--text-mute); margin-top:2px; font-family:var(--font-mono); }
  .gal-more {
    position:absolute; top:6px; right:6px; width:26px; height:26px; border-radius:8px; border:none;
    background:rgba(0,0,0,.55); color:#fff; opacity:0; cursor:pointer; display:flex; align-items:center; justify-content:center;
    transition:opacity .15s, background .15s; font-size:13px;
  }
  .gal-card:hover .gal-more { opacity:1; }
  .gal-more:hover { background:rgba(0,0,0,.8); }
</style>
