<script lang="ts">
  // Людський текст помилки: без префікса "Error: " і без технічного хвоста
  // "[429 Too Many Requests: {...}]", який бекенд дописує для логів.
  function friendlyErr(e: unknown): string {
    const raw = e instanceof Error ? e.message : String(e)
    return raw.replace(/^Error:\s*/, '').replace(/\s*\[[^\]]*\]\s*$/, '')
  }
  // ══════════════════════════════════════════════════════════════════════
  // Гардероб — новий UI/UX (2026 redesign).
  //
  //   • Логіка рендеру 3D-моделі, поворот моделі й UV-обрізка плаща
  //     (cropCapeFront) НЕ ЗМІНЕНІ — імпортуються as-is з wardrobe3d.ts
  //     і MiniSkin3D.svelte. Тільки обгортка, layout, анімації переписані.
  //
  //   • Лаяут:
  //       ┌───────────────────────────┬──────────────────────────┐
  //       │  3D-сцена (зліва) +       │  Сітка предметів          │
  //       │  apply + copy-кнопка      │  (Скіни / Плащі)          │
  //       └───────────────────────────┴──────────────────────────┘
  //
  //   • Модальні вікна — через <WardrobeModal> (portal, backdrop blur,
  //     spring scale-in, ESC-close, draggable header).
  //
  //   • Анімації: fade + scale для карток (staggered), pill-indicator для
  //     активного табу, glow при hover, м'які transitions на кнопках,
  //     skeleton-loading для картки, поки MiniSkin3D рендериться.
  //
  // ══════════════════════════════════════════════════════════════════════

  import { onMount, onDestroy } from 'svelte'
  import { fade, fly, scale } from 'svelte/transition'
  import { cubicOut } from 'svelte/easing'
  import { flip } from 'svelte/animate'
  import { t } from './i18n.svelte'
  import { App } from './wails'
  import { toast, confirmDialog } from './toast.svelte'
  import type { WardrobeViewerHandle } from './wardrobe3d'
  import MiniSkin3D from './MiniSkin3D.svelte'
  import WardrobeModal from './WardrobeModal.svelte'
  import type { SkinPreset, CapeInfo, PlayerProfile, Account } from './types'
  import steveSkin from '../assets/skins/steve.png'
  import alexSkin from '../assets/skins/alex.png'
  import noorSkin from '../assets/skins/noor.png'
  import sunnySkin from '../assets/skins/sunny.png'
  import ariSkin from '../assets/skins/ari.png'
  import zuriSkin from '../assets/skins/zuri.png'
  import makenaSkin from '../assets/skins/makena.png'
  import kaiSkin from '../assets/skins/kai.png'
  import efeSkin from '../assets/skins/efe.png'

  // shaurma — чи показувати кнопку «Скін-акаунт»: скін-система доступна лише
  // у Shaurma-редакції І лише коли її не вимкнено в налаштуваннях
  // (Налаштування → Основне → «Вимкнути скіни офлайн-акаунтів»). Батько
  // (App.svelte) передає вже обʼєднаний прапорець — сама панель не знає ні
  // про редакцію білда, ні про налаштування.
  // (Google-вхід / нік / власний скін для офлайн-акаунтів).
  let {
    activeAccount,
    shaurma = false,
    nickMismatch = false,
    skinAuthNick = '',
  }: {
    activeAccount: Account | undefined
    shaurma?: boolean
    // nickMismatch — є активна Google-сесія скін-системи (skinAuthNick
    // непорожній), але її нік не збігається з ніком поточного Шаурма-
    // акаунта лаунчера: сервер роздає скін за ніком, тому пресет із
    // гардероба цьому конкретному акаунту в грі НЕ застосується, доки
    // ніки не зійдуться. Раніше в цьому випадку сторінку просто не
    // показували — тепер показуємо банер з поясненням замість сітки.
    nickMismatch?: boolean
    skinAuthNick?: string
  } = $props()

  // Модалка скін-акаунта прибрана з гардеробу (перенесено в Налаштування).

  // ── Стан ──────────────────────────────────────────────────────────────
  let presets = $state<SkinPreset[]>([])
  let selectedPresetId = $state<string | null>(null)
  let activePresetId = $state<string | null>(null) // пресет, що зараз стоїть на акаунті
  let loading = $state(true)
  let applying = $state(false)

  let defaultPresetIds = $state<Record<string, string>>({})
  let defaultBusy = $state('')

  let anim = $state<'idle' | 'sit' | 'look'>('idle')

  // ── Drag&drop переставляння пресетів з ghost-slide ───────────────────
  // dragOverPresetId + dragOverPos визначають куди саме (before/after) впаде
  // картка. CSS ghost-slide: карта-ціль отримує margin + псевдо-лінію
  // з боку вставки; інші картки «розсовуються» через gap.
  let dragPresetId = $state<string | null>(null)
  let dragOverPresetId = $state<string | null>(null)
  let dragOverPos = $state<'before' | 'after' | null>(null)

  function onPresetDragStart(id: string) {
    if (applying || defaultBusy) return
    dragPresetId = id
  }

  // Спільний розрахунок позиції вставки: ліва половина цільової картки =
  // before, права = after (горизонтальний потік сітки).
  function presetDropPos(e: DragEvent, el: HTMLElement): 'before' | 'after' {
    const rect = el.getBoundingClientRect()
    return (e.clientX - rect.left) / (rect.width || 1) < 0.5 ? 'before' : 'after'
  }

  function onPresetDragOver(e: DragEvent, id: string) {
    if (!dragPresetId || dragPresetId === id) return
    e.preventDefault()
    e.dataTransfer!.dropEffect = 'move'
    dragOverPresetId = id
    dragOverPos = presetDropPos(e, e.currentTarget as HTMLElement)
  }

  // ── Drop у ПРОМІЖКУ між картками сітки ────────────────────────────────
  // Раніше dragover ловився лише на самій картці: наведення в зазор між
  // картками нічого не показувало і пресет неможливо було поставити точно
  // між двома. Тепер контейнер .wr2-grid теж ловить dragover/drop: шукаємо
  // рядок, у якому курсор (за Y), і всередині рядка — слот за X. Так видно
  // КУДИ саме впаде картка (лінія в зазорі), а не лише підсвітка цілі.
  function onGridDragOver(e: DragEvent) {
    if (!dragPresetId) return
    const t = e.target as HTMLElement
    if (t.closest?.('.wr2-card')) return // картка сама обробляє свій dragover
    e.preventDefault()
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
    const grid = e.currentTarget as HTMLElement
    const cards = Array.from(grid.querySelectorAll<HTMLElement>('.wr2-card'))
    if (!cards.length) return
    // Рядок під курсором (по Y); між рядками — найближчий за центром.
    let row = cards.filter(c => {
      const r = c.getBoundingClientRect()
      return e.clientY >= r.top && e.clientY <= r.bottom
    })
    if (!row.length) {
      row = [...cards].sort((a, b) => {
        const ra = a.getBoundingClientRect(), rb = b.getBoundingClientRect()
        const da = Math.min(Math.abs(e.clientY - ra.top), Math.abs(e.clientY - ra.bottom))
        const db = Math.min(Math.abs(e.clientY - rb.top), Math.abs(e.clientY - rb.bottom))
        return da - db
      }).slice(0, 1)
    }
    row.sort((a, b) => a.getBoundingClientRect().left - b.getBoundingClientRect().left)
    const target = row.find(c => e.clientX < c.getBoundingClientRect().left + c.getBoundingClientRect().width / 2)
    if (target) {
      const id = presetIdOfCard(target)
      if (id && id !== dragPresetId) {
        dragOverPresetId = id
        dragOverPos = 'before'
      }
    } else {
      const last = row[row.length - 1]
      const id = presetIdOfCard(last)
      if (id && id !== dragPresetId) {
        dragOverPresetId = id
        dragOverPos = 'after'
      }
    }
  }

  function presetIdOfCard(card: HTMLElement): string | null {
    return card.dataset.presetId ?? null
  }

  async function commitPresetReorder(from: string, toId: string, pos: 'before' | 'after' | null) {
    if (!from || !toId || from === toId) return
    const ids = presets.map((p) => p.id)
    const fromIdx = ids.indexOf(from)
    const toIdx = ids.indexOf(toId)
    if (fromIdx === -1 || toIdx === -1) return
    // Вставляємо перед або після цільової картки
    ids.splice(fromIdx, 1)
    const newToIdx = ids.indexOf(toId)
    ids.splice(pos === 'before' ? newToIdx : newToIdx + 1, 0, from)
    // Оптимістичне оновлення
    presets = ids.map((pid) => presets.find((p) => p.id === pid)!).filter(Boolean)
    if (!activeAccount?.id) return
    try {
      presets = await App.ReorderWardrobePresets(activeAccount.id, ids)
    } catch (err) {
      toast(t('error.withMessage', { message: String(err) }), 'error')
      if (activeAccount?.id) await loadPresets(activeAccount.id)
    }
  }

  async function onPresetDrop(e: DragEvent, id: string) {
    e.preventDefault()
    const from = dragPresetId
    const pos = dragOverPos
    dragPresetId = null
    dragOverPresetId = null
    dragOverPos = null
    await commitPresetReorder(from ?? '', id, pos)
  }

  async function onGridDrop(e: DragEvent) {
    e.preventDefault()
    const from = dragPresetId
    const toId = dragOverPresetId
    const pos = dragOverPos
    dragPresetId = null
    dragOverPresetId = null
    dragOverPos = null
    await commitPresetReorder(from ?? '', toId ?? '', pos)
  }

  function onPresetDragEnd() {
    dragPresetId = null
    dragOverPresetId = null
    dragOverPos = null
  }

  // ── 3D-переглядач (не чіпаємо, той самий wardrobe3d.ts) ──
  let viewerContainer: HTMLDivElement | undefined = $state()
  let viewer: WardrobeViewerHandle | null = $state(null)

  let viewerCtor: ((container: HTMLElement) => Promise<WardrobeViewerHandle>) | null = null
  let viewerCtorPromise: Promise<void> | null = null
  function ensureViewerModule(): Promise<void> {
    if (!viewerCtorPromise) {
      viewerCtorPromise = import('./wardrobe3d').then((mod) => {
        viewerCtor = mod.createWardrobeViewer
      })
    }
    return viewerCtorPromise
  }

  // Важкий 3D-переглядач (WebGL-контекст, PMREM-env, glTF-модель) створюємо
  // ПІСЛЯ першого кадру сторінки. Раніше onMount запускав його одразу:
  // браузер ще малював сітку пресетів, а в тому ж кадрі стартувала
  // компіляція шейдерів — звідси секундний «залип» при відкритті гардероба.
  onMount(() => {
    let idleId: number | undefined
    let timer: ReturnType<typeof setTimeout> | undefined
    const startViewer = () => {
      if (!viewerContainer) return
      ensureViewerModule().then(() => {
        if (viewerCtor && viewerContainer) {
          viewerCtor(viewerContainer).then((h) => { viewer = h })
        }
      })
    }
    if (typeof window.requestIdleCallback === 'function') {
      idleId = window.requestIdleCallback(startViewer, { timeout: 300 })
    } else {
      timer = setTimeout(startViewer, 90)
    }
    // Глобальний пошук (Ctrl+K): клік по пресету в результатах → перехід
    // на гардероб з вибором цього пресета (подія wardrobe:select).
    const onSelect = (e: Event) => {
      const id = (e as CustomEvent<string>).detail
      if (id) pendingSelectId = id
    }
    window.addEventListener('wardrobe:select', onSelect)
    return () => {
      if (idleId !== undefined && typeof window.cancelIdleCallback === 'function') window.cancelIdleCallback(idleId)
      if (timer) clearTimeout(timer)
      window.removeEventListener('wardrobe:select', onSelect)
    }
  })
  // Пресет, вибраний з глобального пошуку. Може прийти ДО завантаження
  // списку пресетів — застосовується в loadPresets після підвантаження.
  let pendingSelectId = ''
  onDestroy(() => {
    viewer?.dispose()
    viewer = null
    editorViewer?.dispose()
    editorViewer = null
    copyViewer?.dispose()
    copyViewer = null
  })

  // Плавний фейд контейнера 3D при зміні вибраного пресету.
  let sceneFade = $state(false)
  function pulseSceneFade() {
    sceneFade = true
    setTimeout(() => (sceneFade = false), 320)
  }

  $effect(() => {
    const p = selectedPreset()
    const v = viewer
    if (!v) return
    pulseSceneFade()
    if (p) {
      v.setSlim(p.slimArms)
      let alive = true
      App.GetWardrobeTextureDataURL(p.skinPath).then((d) => alive && v.setTexture(d)).catch(() => {})
      if (p.capePath) {
        App.GetWardrobeTextureDataURL(p.capePath).then((d) => alive && v.setCapeTexture(d)).catch(() => alive && v.setCapeTexture(null))
      } else {
        v.setCapeTexture(null)
      }
      return () => { alive = false }
    }
    const acc = activeAccount
    if (!acc?.id) return
    let alive = true
    App.GetWardrobeActiveLook(acc.id).then((look) => {
      if (!alive || !look) return
      v.setSlim(look.slimArms)
      v.setTexture(look.skinDataUrl)
      v.setCapeTexture(look.capeDataUrl ?? null)
    }).catch(() => {})
    return () => { alive = false }
  })

  $effect(() => { viewer?.playAnimation(anim) })

  // ── Міні-3D превʼю карток пресетів ──
  let presetTextures = $state<Record<string, string>>({})
  let presetCapeTextures = $state<Record<string, string>>({})
  const presetTextureSig: Record<string, string> = {}

  $effect(() => {
    const list = presets
    if (list.length === 0) return
    for (const p of list) {
      const sig = p.skinPath + '|' + (p.capePath ?? '')
      if (presetTextureSig[p.id] === sig) continue
      presetTextureSig[p.id] = sig
      delete presetTextures[p.id]
      delete presetCapeTextures[p.id]
      App.GetWardrobeTextureDataURL(p.skinPath)
        .then((d) => { presetTextures[p.id] = d })
        .catch(() => { presetTextures[p.id] = '' })
      if (p.capePath) {
        App.GetWardrobeTextureDataURL(p.capePath)
          .then((d) => { presetCapeTextures[p.id] = d })
          .catch(() => { presetCapeTextures[p.id] = '' })
      }
    }
  })

  // Обрізка передньої панелі плаща — ТА САМА логіка, що й у старому коді
  // (обов'язково НЕ чіпаємо; це «картка плаща» за ТЗ).
  // Примітка: окрема вкладка «Плащі» прибрана (за ТЗ лишаються пресети);
  // плащі вибираються в редакторі пресету через wm-cape-grid.
  function cropCapeFront(dataUrl: string): Promise<string> {
    return new Promise((resolve, reject) => {
      const img = new Image()
      img.onload = () => {
        try {
          const srcX = 1, srcY = 2, srcW = 10, srcH = 15
          const ch = Math.max(1, Math.min(srcH, img.height - srcY))
          const c = document.createElement('canvas')
          c.width = 50
          c.height = 60
          const ctx = c.getContext('2d')
          if (!ctx) { reject(new Error('canvas unavailable')); return }
          ctx.imageSmoothingEnabled = false
          ctx.drawImage(img, srcX, srcY, srcW, ch, 0, 0, c.width, c.height)
          resolve(c.toDataURL('image/png'))
        } catch (e) { reject(e) }
      }
      img.onerror = () => reject(new Error('cape load failed'))
      img.src = dataUrl
    })
  }

  // ── 3D у редакторі ──
  let editorViewerContainer: HTMLDivElement | undefined = $state()
  let editorViewer: WardrobeViewerHandle | null = $state(null)

  $effect(() => {
    if (!editorOpen) {
      editorViewer?.dispose()
      editorViewer = null
      return
    }
    const el = editorViewerContainer
    if (el && !editorViewer) {
      ensureViewerModule().then(() => {
        if (viewerCtor && el) {
          viewerCtor(el).then((h) => {
            if (editorOpen) editorViewer = h
            else h.dispose()
          })
        }
      })
    }
  })

  $effect(() => {
    const v = editorViewer
    if (!v) return
    // Обраний, але ще не збережений файл має приоритет у прев'ю.
    if (editorPendingSkin) { v.setTexture(editorPendingSkin); return }
    const p = editorPreset
    if (p) {
      let alive = true
      App.GetWardrobeTextureDataURL(p.skinPath).then((d) => alive && v.setTexture(d)).catch(() => {})
      return () => { alive = false }
    } else {
      v.setTexture(steveSkin)
    }
  })

  $effect(() => {
    const v = editorViewer
    if (!v || !editorOpen) return
    const url = editorPreviewCapeUrl
    if (!url) { v.setCapeTexture(null); return }
    let alive = true
    App.GetWardrobeCapeDataURL(url).then((d) => alive && v.setCapeTexture(d)).catch(() => alive && v.setCapeTexture(null))
    return () => { alive = false }
  })

  $effect(() => { editorViewer?.setSlim(editorSlim) })

  // ── Модалка "Скопіювати скін гравця" ──
  let copyModalOpen = $state(false)
  let copyNick = $state('')
  let copyStatus = $state<'idle' | 'searching' | 'found' | 'notfound' | 'nosk' | 'error'>('idle')
  // copyTexLoading — поки реальна текстура скіна летить з Mojang (окремий
  // CDN-запит ПІСЛЯ LookupWardrobePlayer), 3D-модель уже є, але порожня.
  // Без цього прапорця користувач бачив «пусту» модель і не розумів, що
  // лаунчер ще тягне скін.
  let copyTexLoading = $state(false)
  let copyResult = $state<PlayerProfile | null>(null)
  let copyIncludeCape = $state(true)
  let copyBusy = $state(false)
  let copyDebounceTimer: ReturnType<typeof setTimeout> | undefined

  let copyViewerContainer: HTMLDivElement | undefined = $state()
  let copyViewer: WardrobeViewerHandle | null = $state(null)
  let copyHeadUrl = $state('')

  async function loadCopyHead(p: PlayerProfile) {
    copyHeadUrl = ''
    try {
      const skin = await App.GetWardrobeRemoteSkinDataURL(p.skinUrl)
      const img = new Image()
      img.onload = () => {
        try {
          const c = document.createElement('canvas')
          c.width = 52; c.height = 52
          const ctx = c.getContext('2d')
          if (!ctx) return
          ctx.imageSmoothingEnabled = false
          // Малюємо ОБИДВА слої голови скіна: базовий (8,8) + прозорий
          // верхній шар (40,8) поверх — інакше аватар показував лише
          // перший шар шкіри (без деталей/detailed-шарів).
          ctx.drawImage(img, 8, 8, 8, 8, 0, 0, 52, 52)
          ctx.drawImage(img, 40, 8, 8, 8, 0, 0, 52, 52)
          copyHeadUrl = c.toDataURL('image/png')
        } catch {}
      }
      img.src = skin
    } catch {}
  }

  $effect(() => {
    if (!copyModalOpen || !copyResult) {
      copyViewer?.dispose()
      copyViewer = null
      return
    }
    const el = copyViewerContainer
    if (el && !copyViewer) {
      ensureViewerModule().then(() => {
        if (viewerCtor && el) {
          viewerCtor(el).then((h) => {
            if (copyModalOpen && copyResult) copyViewer = h
            else h.dispose()
          })
        }
      })
    }
  })

  $effect(() => {
    const v = copyViewer
    const p = copyResult
    if (!v || !p) return
    v.setSlim(p.slimArms)
    let alive = true
    App.GetWardrobeRemoteSkinDataURL(p.skinUrl)
      .then((d) => { if (alive) v.setTexture(d) })
      .catch(() => {})
      .finally(() => { copyTexLoading = false })
    if (copyIncludeCape && p.capeUrl) {
      App.GetWardrobeCapeDataURL(p.capeUrl).then((d) => alive && v.setCapeTexture(d)).catch(() => alive && v.setCapeTexture(null))
    } else {
      v.setCapeTexture(null)
    }
    return () => { alive = false }
  })

  // ── Модалка редактора пресету ──
  let editorOpen = $state(false)
  let editorPreset = $state<SkinPreset | null>(null)
  let editorName = $state('')
  let editorSlim = $state(false)
  let editorCapeGrid = $state<CapeInfo[]>([])
  let editorSelectedCapeId = $state<string>('')
  let editorPreviewCapeUrl = $state<string | null>(null)
  let editorCapeTouched = $state(false)
  let editorFileInput: HTMLInputElement | undefined = $state()
  let editorCapeThumbs = $state<Record<string, string>>({})
  // Обраний у редакторі файл скіна ще НЕ збережено: тримаємо data-URL лише
  // для прев'ю в 3D, а на бекенд віддаємо тільки по кліку «Зберегти».
  let editorPendingSkin = $state<string | null>(null)
  let editorPendingSkinName = $state('')

  async function loadEditorCapeThumbs(grid: CapeInfo[]) {
    editorCapeThumbs = {}
    const next: Record<string, string> = {}
    await Promise.all(grid.map(async (cape) => {
      try {
        const full = await App.GetWardrobeCapeDataURL(cape.url)
        next[cape.id] = await cropCapeFront(full)
      } catch {}
    }))
    editorCapeThumbs = next
  }

  const DEFAULT_SKINS = [
    { name: 'Steve', slim: false, tex: steveSkin },
    { name: 'Alex', slim: true, tex: alexSkin },
    { name: 'Noor', slim: true, tex: noorSkin },
    { name: 'Sunny', slim: false, tex: sunnySkin },
    { name: 'Ari', slim: false, tex: ariSkin },
    { name: 'Zuri', slim: false, tex: zuriSkin },
    { name: 'Makena', slim: true, tex: makenaSkin },
    { name: 'Kai', slim: false, tex: kaiSkin },
    { name: 'Efe', slim: true, tex: efeSkin },
  ]

  async function loadPresets(accountID: string) {
    loading = true
    try {
      presets = await App.GetWardrobePresets(accountID)
      const dmap: Record<string, string> = {}
      for (const d of DEFAULT_SKINS) {
        const m = presets.find((p) => p.name === d.name && p.slimArms === d.slim)
        if (m) dmap[d.name] = m.id
      }
      defaultPresetIds = dmap
      let matchedId = ''
      if (accountID) {
        try { matchedId = await App.GetWardrobeActivePresetID(accountID) } catch {}
      }
      selectedPresetId = matchedId && presets.some((p) => p.id === matchedId) ? matchedId : null
      activePresetId = matchedId || null
      // Пресет з глобального пошуку: вибираємо після завантаження списку.
      if (pendingSelectId && presets.some((p) => p.id === pendingSelectId)) {
        selectedPresetId = pendingSelectId
        pendingSelectId = ''
      }
    } catch (e) {
      toast(String(e), 'error')
    } finally {
      loading = false
    }
  }

  $effect(() => {
    selectedPresetId = null
    loadPresets(activeAccount?.id ?? '')
  })

  function selectedPreset(): SkinPreset | undefined {
    return presets.find((p) => p.id === selectedPresetId)
  }

  // Клік по картці пресета = лише ВИБІР (як у Modrinth App): виділяємо
  // пресет у 3D-сцені й показуємо кнопку «Застосувати», якщо він
  // відрізняється від активного. Реальне застосування на акаунт (мережевий
  // запит до Mojang) відбувається тільки за явним підтвердженням —
  // кнопкою «Застосувати» (applyToAccount) або з редактора.
  // Раніше клік одразу викликав applyToAccount(): вибір пресету миттєво
  // «комітив» скін на Mojang ще до того, як його встигали відредагувати
  // (баг: «вибрав скін — і вже не можна редагувати»).
  function selectPreset(id: string) {
    if (applying) return
    selectedPresetId = id
  }

  async function deletePreset(p: SkinPreset, ev: Event) {
    ev.stopPropagation()
    const ok = await confirmDialog(t('wardrobe.deleteConfirmTitle'), t('wardrobe.deleteConfirmMsg'), {
      confirmLabel: t('wardrobe.editor.delete'),
      cancelLabel: t('wardrobe.editor.cancel'),
      danger: true,
    })
    if (!ok) return
    try {
      await App.DeleteWardrobePreset(activeAccount?.id ?? '', p.id)
      presets = presets.filter((x) => x.id !== p.id)
      if (selectedPresetId === p.id) selectedPresetId = null
      toast(t('wardrobe.deleted'), 'success')
    } catch (e) {
      toast(String(e), 'error')
    }
  }

  async function applyToAccount() {
    const p = selectedPreset()
    if (!p || applying) return
    applying = true
    try {
      await App.ApplyWardrobePreset(activeAccount?.id ?? '', p.id)
      activePresetId = p.id
      toast(t('wardrobe.applied'), 'success')
    } catch (e) {
      toast(t('wardrobe.applyError') + ': ' + friendlyErr(e), 'error')
    } finally {
      applying = false
    }
  }

  async function assetToBase64(url: string): Promise<string> {
    if (url.startsWith('data:')) return url.split(',')[1] ?? ''
    const res = await fetch(url)
    if (!res.ok) throw new Error('skin asset fetch failed')
    const blob = await res.blob()
    return await new Promise((resolve, reject) => {
      const r = new FileReader()
      r.onload = () => {
        const dataUrl = r.result as string
        resolve(dataUrl.split(',')[1] ?? '')
      }
      r.onerror = () => reject(new Error('skin asset read failed'))
      r.readAsDataURL(blob)
    })
  }

  async function pickDefaultSkin(d: (typeof DEFAULT_SKINS)[number]) {
    if (defaultBusy || !activeAccount?.id) return
    defaultBusy = d.name
    try {
      const base64 = await assetToBase64(d.tex)
      // Авто-назва: якщо «Steve»/«Alex» уже є — «Steve (2)» тощо.
      const preset = await App.SaveWardrobeLocalSkin(activeAccount.id, uniquePresetName(d.name), base64, d.slim)
      presets = [preset, ...presets.filter((p) => p.id !== preset.id)]
      defaultPresetIds[d.name] = preset.id
      selectedPresetId = preset.id
      await App.ApplyWardrobePreset(activeAccount.id, preset.id)
      activePresetId = preset.id
      toast(t('wardrobe.applied'), 'success')
    } catch (e) {
      toast(t('wardrobe.applyError') + ': ' + friendlyErr(e), 'error')
    } finally {
      defaultBusy = ''
    }
  }

  function openCopyModal() {
    copyModalOpen = true
    copyNick = ''
    copyStatus = 'idle'
    copyResult = null
    copyHeadUrl = ''
    copyIncludeCape = true
  }

  // Унікальна назва пресета для АВТО-назв (дефолтні скіни, імпорт файлу без
  // ручного імені): якщо пресет з такою назвою вже існує — додаємо
  // " (2)", " (3)"… Як у бекенді (uniqueName у wardrobe service), щоб
  // дефолтні скіни теж не дублювались. excludeId — поточний пресет (при
  // перейменуванні себе враховувати не треба).
  function uniquePresetName(base: string, excludeId?: string): string {
    const taken = new Set(
      presets.filter((p) => p.id !== excludeId).map((p) => p.name)
    )
    if (!taken.has(base)) return base
    for (let i = 2; ; i++) {
      const cand = `${base} (${i})`
      if (!taken.has(cand)) return cand
    }
  }

  function onCopyNickInput() {
    clearTimeout(copyDebounceTimer)
    copyResult = null
    copyHeadUrl = ''
    const nick = copyNick.trim()
    if (nick.length < 3) { copyStatus = 'idle'; return }
    copyStatus = 'searching'
    copyDebounceTimer = setTimeout(runCopyLookup, 500)
  }

  async function runCopyLookup() {
    const nick = copyNick.trim()
    if (nick.length < 3) return
    copyStatus = 'searching'
    try {
      const profile = await App.LookupWardrobePlayer(nick)
      copyResult = profile
      copyTexLoading = true
      copyStatus = 'found'
      loadCopyHead(profile)
    } catch (e) {
      const msg = String(e)
      if (msg.includes('player not found')) copyStatus = 'notfound'
      else if (msg.includes('no custom skin')) copyStatus = 'nosk'
      else copyStatus = 'error'
      copyResult = null
    }
  }

  async function createPresetFromCopy() {
    if (!copyResult || copyBusy) return
    copyBusy = true
    try {
      const preset = await App.CopyWardrobePlayerSkin(activeAccount?.id ?? '', copyResult, copyIncludeCape)
      presets = [preset, ...presets.filter((p) => p.id !== preset.id)]
      selectedPresetId = preset.id
      copyModalOpen = false
      toast(t('wardrobe.copy.created') + ' «' + preset.name + '»', 'success')
    } catch (e) {
      toast(String(e), 'error')
    } finally {
      copyBusy = false
    }
  }

  async function openEditor(p: SkinPreset | null) {
    editorPreset = p
    editorName = p?.name ?? ''
    editorSlim = p?.slimArms ?? false
    editorSelectedCapeId = ''
    editorPreviewCapeUrl = p?.capeUrl ?? null
    editorCapeTouched = false
    editorPendingSkin = null
    editorPendingSkinName = ''
    editorOpen = true
    try {
      editorCapeGrid = await App.GetWardrobeOwnedCapes(activeAccount?.id ?? '')
    } catch {
      editorCapeGrid = []
    }
    loadEditorCapeThumbs(editorCapeGrid)
    if (p?.capeUrl) {
      const match = editorCapeGrid.find((c) => c.url === p.capeUrl)
      if (match) editorSelectedCapeId = match.id
    }
  }

  function pickEditorFile() { editorFileInput?.click() }

  // Вибір файлу у редакторі — це ЛИШЕ прев'ю. Ні оновлення наявного
  // пресету, ні створення нового тут не відбувається: усе фіксується
  // тільки кнопкою «Зберегти й застосувати» (баг: «вибрав файл скіна —
  // пресет створився сам, не дочекавшись кнопки Зберегти»).
  function onEditorFileChange(ev: Event) {
    const input = ev.target as HTMLInputElement
    const file = input.files?.[0]
    if (!file) return
    const reader = new FileReader()
    reader.onload = () => {
      editorPendingSkin = reader.result as string
      editorPendingSkinName = file.name.replace(/\.png$/i, '')
      // Порожнє поле назви — підставляємо ім'я файлу (унікальне), щоб
      // користувачу було що зберегти; ручну назву не чіпаємо.
      if (!editorName.trim()) editorName = uniquePresetName(editorPendingSkinName)
      // Дозволяємо повторно вибрати той самий файл.
      input.value = ''
    }
    reader.readAsDataURL(file)
  }

  async function onEditSkinClick() {
    const p = selectedPreset()
    if (p) { openEditor(p); return }
    if (!activeAccount?.isLicensed) { openEditor(null); return }
    const ok = await confirmDialog(
      t('wardrobe.createFromActiveTitle'),
      t('wardrobe.createFromActiveMsg'),
      { confirmLabel: t('wardrobe.createFromActiveBtn'), cancelLabel: t('wardrobe.editor.cancel') },
    )
    if (!ok) return
    try {
      const preset = await App.SaveWardrobeActiveLookPreset(activeAccount.id, activeAccount.username || t('wardrobe.activeLookDefaultName'))
      presets = [preset, ...presets.filter((x) => x.id !== preset.id)]
      selectedPresetId = preset.id
      toast(t('wardrobe.copy.created') + ' «' + preset.name + '»', 'success')
    } catch (e) {
      toast(String(e), 'error')
    }
  }

  async function saveEditorCape() {
    if (!editorCapeTouched) return
    const accId = activeAccount?.id ?? ''
    try {
      if (editorSelectedCapeId === '') {
        await App.SetWardrobeActiveCape(accId, '')
      } else {
        const cape = editorCapeGrid.find((c) => c.id === editorSelectedCapeId)
        if (cape) await App.SetWardrobeActiveCape(accId, cape.id)
      }
    } catch (e) {
      toast(String(e), 'error')
    }
  }

  async function saveEditor() {
    const accId = activeAccount?.id ?? ''
    const pending = editorPendingSkin
    const origin = editorPreset
    // Створити новий пресет без обраного файлу неможливо — просимо файл.
    if (!origin?.id && !pending) {
      toast(t('wardrobe.editor.noSkin'), 'error')
      return
    }
    await saveEditorCape()
    const capeUrl = editorCapeTouched
      ? (editorSelectedCapeId === '' ? '' : (editorCapeGrid.find((c) => c.id === editorSelectedCapeId)?.url ?? ''))
      : (origin?.capeUrl ?? '')
    try {
      let base = origin
      // 1) Зафіксовуємо новий скін, обраний у цій сесії редактора:
      //    для наявного пресету — оновлюємо текстуру на місці, для нового —
      //    створюємо пресет із введеною назвою.
      if (pending) {
        const b64 = pending.split(',')[1] ?? ''
        if (base?.id) {
          base = await App.UpdateWardrobePresetSkin(accId, base.id, b64, editorSlim)
        } else {
          const name = editorName.trim() ? editorName : uniquePresetName(editorPendingSkinName || 'skin')
          base = await App.SaveWardrobeLocalSkin(accId, name, b64, editorSlim)
        }
      }
      if (!base?.id) { editorOpen = false; return }
      const savedBase = base
      // 2) Назва / тонкі руки / плащ.
      const updated = await App.UpdateWardrobePreset(accId, savedBase.id, editorName || savedBase.name, editorSlim, capeUrl)
      presets = [updated, ...presets.filter((x) => x.id !== savedBase.id && x.id !== updated.id)]
      selectedPresetId = updated.id
      // Застосовуємо на акаунт лише коли справді змінився вигляд наявного
      // пресету (як і раніше); створення нового пресету — без мережі.
      const lookChanged = !!origin?.id && (pending != null || editorSlim !== origin!.slimArms || capeUrl !== (origin!.capeUrl ?? ''))
      if (lookChanged) {
        try { await App.ApplyWardrobePreset(accId, updated.id); activePresetId = updated.id }
        catch (e) { toast(String(e), 'error'); editorOpen = false; return }
      }
    } catch (e) {
      toast(String(e), 'error'); return
    }
    editorPendingSkin = null
    editorPendingSkinName = ''
    editorOpen = false
    toast(t('wardrobe.editor.saved'), 'success')
  }

  async function deleteFromEditor() {
    if (!editorPreset) return
    const p = editorPreset
    editorOpen = false
    await deletePreset(p, new Event('click'))
  }

  const hasChanges = $derived(!!selectedPreset() && selectedPresetId !== activePresetId)

  // Кількість пресетів (для лічильника у шапці панелі пресетів).
  const skinsCount = $derived(presets.length + DEFAULT_SKINS.length)

  // Показуємо банер розбіжності лише для Шаурма-акаунтів (не ліцензійних) —
  // для Microsoft-акаунтів nickMismatch завжди false (App.svelte).
  const showNickMismatchBanner = $derived(shaurma && nickMismatch && activeAccount?.isLicensed !== true)
</script>

<!-- ══════════════ ОСНОВНИЙ LAYOUT: зліва 3D, праворуч пресети ══════════════ -->

{#if showNickMismatchBanner}
  <!-- Розбіжність ніків: Google-сесія скін-системи (skinAuthNick) активна,
       але прив'язана до ІНШОГО ніка, ніж поточний Шаурма-акаунт лаунчера
       (activeAccount.username). Скін роздається сервером за ніком — тож
       пресет із гардероба цьому акаунту в грі не застосується, доки ніки
       не зійдуться. Пояснюємо це прямо тут (раніше сторінка просто не
       показувалась — гравець бачив «нічого не працює» без причини). -->
  <div class="wr2-mismatch-banner">
    <i class="ti ti-alert-triangle"></i>
    <div class="wr2-mismatch-text">
      <b>Скін-акаунт увійшов під ніком «{skinAuthNick}»</b>, а поточний акаунт лаунчера — «{activeAccount?.username ?? ''}».
      Скін застосовується за ніком скін-акаунта, тож обраний тут пресет не з'явиться в грі, поки ніки не збігатимуться.
      Перейменуйте акаунт лаунчера в налаштуваннях або увійдіть у «Скін-акаунт» під потрібним ніком (Налаштування → Основне → Скін-акаунт).
    </div>
    <button class="wr2-btn wr2-btn-ghost" onclick={() => window.dispatchEvent(new CustomEvent('open-settings-skin-account'))}>
      <i class="ti ti-settings"></i> Відкрити в налаштуваннях
    </button>
  </div>
{/if}

<div class="wr2-shell">

  <!-- ── Ліворуч: 3D-сцена ── -->
  <section class="wr2-scene">
    <div class="wr2-scene-card">
      <!-- Динамічний акцентний градієнт-фон з подихом. -->
      <div class="wr2-scene-bg"></div>
      <div class="wr2-scene-grid"></div>

      <!-- КОНТЕЙНЕР 3D — те саме монтування, як і раніше -->
      <div class="wr2-3d-wrap {sceneFade ? 'fading' : ''}">
        <div class="wr2-3d-canvas" bind:this={viewerContainer}></div>
      </div>

      <div class="wr2-scene-hint">
        <i class="ti ti-hand-move"></i> {t('wardrobe.dragHint')}
      </div>

      <!-- Anim toggle overlay -->
      <div class="wr2-anim">
        {#each [
          { id: 'idle', label: t('wardrobe.animIdle'), icon: 'ti-user' },
          { id: 'sit',  label: t('wardrobe.animSit'),  icon: 'ti-armchair' },
          { id: 'look', label: t('wardrobe.animLook'), icon: 'ti-eye' },
        ] as a}
          <button
            class="wr2-anim-btn {anim === a.id ? 'active' : ''}"
            onclick={() => (anim = a.id as 'idle' | 'sit' | 'look')}
            title={a.label}
          >
            <i class="ti {a.icon}"></i>
            <span>{a.label}</span>
          </button>
        {/each}
      </div>
    </div>

    <!-- Actions під сценою -->
    <div class="wr2-scene-actions">
      {#if hasChanges}
        <button
          class="wr2-btn wr2-btn-primary"
          disabled={applying}
          onclick={applyToAccount}
          in:fly={{ y: 8, duration: 240, easing: cubicOut }}
        >
          <i class="ti {applying ? 'ti-loader-2 spin' : 'ti-device-floppy'}"></i>
          {applying ? t('wardrobe.applying') : t('wardrobe.applyBtn')}
        </button>
      {/if}
      <button class="wr2-btn wr2-btn-ghost" onclick={onEditSkinClick}>
        <i class="ti ti-edit"></i> {t('wardrobe.editSkin')}
      </button>
    </div>
  </section>

  <!-- ── Праворуч: вибір пресету ── -->
  <section class="wr2-grid-wrap">
    <!-- Кнопка «Скопіювати скін гравця» переїхала сюди (на картку пресетів,
         на місце колишнього перемикача Скіни/Плащі — вкладку «Плащі» прибрано). -->
    <div class="wr2-grid-head">
      <button class="wr2-copy-btn wr2-copy-in-head" onclick={openCopyModal} title={t('wardrobe.copySkin')}>
        <i class="ti ti-user-search"></i>
        <span class="wr2-copy-label">{t('wardrobe.copySkin')}</span>
      </button>
      <!-- Кнопка «Скін-акаунт» (Google-вхід/нік) прибрана звідси навмисно:
           гардероб має відповідати лише за ВИБІР/ЗАСТОСУВАННЯ скінів.
           Керування Google-сесією скін-системи та ніком офлайн-акаунта
           перенесено в Налаштування → Основне → «Скін-акаунт»
           (SkinAccountPanel рендериться там же, у SettingsPage.svelte). -->
      <div class="wr2-grid-head-count">{skinsCount} {t('wardrobe.skins').toLowerCase()}</div>
    </div>

    <div class="wr2-grid-scroll" in:fade={{ duration: 220 }}>
        <!-- Секція: збережені -->
        <div class="wr2-section-head">
          <div class="wr2-section-title">
            <i class="ti ti-sparkles"></i>
            {t('wardrobe.savedSkins')}
          </div>
          <div class="wr2-section-count">{presets.length}</div>
        </div>

        <div class="wr2-grid" ondragover={onGridDragOver} ondrop={onGridDrop} ondragend={onPresetDragEnd}>
          <button
            class="wr2-add-card"
            onclick={() => openEditor(null)}
            in:scale={{ duration: 260, start: 0.94, easing: cubicOut }}
          >
            <div class="wr2-add-icon"><i class="ti ti-plus"></i></div>
            <div class="wr2-add-title">{t('wardrobe.addSkin')}</div>
            <div class="wr2-add-sub">{t('wardrobe.addSkinHint')}</div>
          </button>

          {#each presets as p, i (p.id)}
            <div
              class="wr2-card {selectedPresetId === p.id ? 'active' : ''} {activePresetId === p.id ? 'is-applied' : ''} {dragPresetId === p.id ? 'dragging' : ''} {dragOverPresetId === p.id && dragOverPos === 'before' ? 'drop-before' : ''} {dragOverPresetId === p.id && dragOverPos === 'after' ? 'drop-after' : ''}"
              data-preset-id={p.id}
              onclick={() => selectPreset(p.id)}
              role="button"
              tabindex="0"
              draggable={!applying && !defaultBusy}
              ondragstart={(e) => onPresetDragStart(p.id)}
              ondragover={(e) => onPresetDragOver(e, p.id)}
              ondrop={(e) => onPresetDrop(e, p.id)}
              onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') selectPreset(p.id) }}
              animate:flip={{ duration: 260 }}
              in:fly={{ y: 10, duration: 260, delay: Math.min(i, 12) * 24, easing: cubicOut }}
            >
              {#if activePresetId === p.id}
                <div class="wr2-badge-applied"><i class="ti ti-check"></i></div>
              {/if}
              <div class="wr2-card-preview">
                <div class="wr2-card-halo"></div>
                {#if presetTextures[p.id] === undefined}
                  <div class="wr2-skeleton"></div>
                {/if}
                <MiniSkin3D slim={p.slimArms} textureUrl={presetTextures[p.id]} capeTextureUrl={presetCapeTextures[p.id]} />
              </div>

              <div class="wr2-card-info">
                <div class="wr2-card-name">{p.name}</div>
                <div class="wr2-card-meta">
                  <span class="wr2-chip">{p.slimArms ? t('wardrobe.slim') : t('wardrobe.classic')}</span>
                  {#if p.capePath}
                    <span class="wr2-chip wr2-chip-violet"><i class="ti ti-wind"></i> {t('wardrobe.withCape')}</span>
                  {/if}
                </div>
              </div>

              <div class="wr2-card-actions">
                <button class="wr2-icon-btn" onclick={(e) => { e.stopPropagation(); openEditor(p) }} title={t('wardrobe.editor.editShort')}>
                  <i class="ti ti-edit"></i>
                </button>
                <button class="wr2-icon-btn wr2-icon-danger" onclick={(e) => deletePreset(p, e)} title={t('wardrobe.editor.delete')}>
                  <i class="ti ti-trash"></i>
                </button>
              </div>
            </div>
          {/each}

          {#if !loading && presets.length === 0}
            <div class="wr2-empty">{t('wardrobe.emptyState')}</div>
          {/if}
        </div>

        <!-- Секція: усталені -->
        <div class="wr2-section-head" style="margin-top: 26px;">
          <div class="wr2-section-title">
            <i class="ti ti-crown"></i>
            {t('wardrobe.defaultSkins')}
          </div>
          <div class="wr2-section-count">{DEFAULT_SKINS.length}</div>
        </div>

        <div class="wr2-grid wr2-grid-compact">
          {#each DEFAULT_SKINS as d, i (d.name)}
            <div
              class="wr2-card wr2-card-compact
                {selectedPresetId === defaultPresetIds[d.name] ? 'active' : ''}
                {defaultBusy === d.name ? 'busy' : ''}"
              onclick={() => pickDefaultSkin(d)}
              role="button"
              tabindex="0"
              in:fly={{ y: 8, duration: 240, delay: 60 + i * 22, easing: cubicOut }}
            >
              <div class="wr2-card-preview">
                <div class="wr2-card-halo"></div>
                <MiniSkin3D slim={d.slim} textureUrl={d.tex} />
                {#if defaultBusy === d.name}
                  <div class="wr2-busy-overlay"><i class="ti ti-loader-2 spin"></i></div>
                {/if}
              </div>
              <div class="wr2-card-info">
                <div class="wr2-card-name">{d.name}</div>
                <div class="wr2-card-meta">
                  <span class="wr2-chip">{d.slim ? t('wardrobe.slim') : t('wardrobe.classic')}</span>
                </div>
              </div>
            </div>
          {/each}
        </div>
      </div>
  </section>
</div>

<!-- Модалка «Скін-акаунт» прибрана з гардеробу — тепер живе в
     Налаштування → Основне (SettingsPage.svelte), як звичайний розділ,
     а не спливне вікно поверх сітки скінів. -->

<!-- ══════════════ МОДАЛКА: копіювання гравця ══════════════ -->
<WardrobeModal
  open={copyModalOpen}
  onClose={() => (copyModalOpen = false)}
  title={t('wardrobe.copy.title')}
  icon="ti-user-search"
  accent="orange"
  size="lg"
>
  {#snippet body()}
    <p class="wm-desc">{t('wardrobe.copy.desc')}</p>

    <div class="wm-input-row">
      <i class="ti ti-user"></i>
      <input
        type="text"
        bind:value={copyNick}
        placeholder={t('wardrobe.copy.placeholder')}
        maxlength="16"
        oninput={onCopyNickInput}
        onkeydown={(e) => { if (e.key === 'Enter') runCopyLookup() }}
      />
    </div>

    <div class="wm-status
      {copyStatus === 'notfound' || copyStatus === 'nosk' || copyStatus === 'error' ? 'err' : ''}
      {copyStatus === 'found' ? 'ok' : ''}">
      {#if copyStatus === 'searching'}
        <i class="ti ti-loader-2 spin"></i> {t('wardrobe.copy.searching')}
      {:else if copyStatus === 'notfound'}
        <i class="ti ti-alert-circle"></i> {t('wardrobe.copy.notFound')}
      {:else if copyStatus === 'nosk'}
        <i class="ti ti-alert-circle"></i> {t('wardrobe.copy.noSkin')}
      {:else if copyStatus === 'error'}
        <i class="ti ti-alert-circle"></i> {t('wardrobe.copy.networkError')}
      {:else if copyStatus === 'found' && copyResult}
        <i class="ti ti-circle-check"></i> {copyResult.capeUrl ? t('wardrobe.copy.found') : t('wardrobe.copy.foundNoCape')}
      {/if}
    </div>

    <!-- Сцена моделі показується ВЖЕ ПІД ЧАС ПОШУКУ: замість порожнього
         місця користувач бачить плейсхолдер «завантаження скіна з
         серверів Mojang» (і той самий плейсхолдер на час, поки летить
         текстура вже знайденого гравця). -->
    {#if copyResult || copyStatus === 'searching' || copyTexLoading}
      <div class="wm-copy-result" in:fly={{ y: 8, duration: 260, easing: cubicOut }}>
        <div class="wm-copy-3d" bind:this={copyViewerContainer}>
          {#if copyStatus === 'searching' || (!copyResult && copyTexLoading) || (copyResult && copyTexLoading)}
            <div class="wm-copy-loading">
              <i class="ti ti-loader-2 spin"></i>
              <span>{t('wardrobe.copy.loadingSkin')}</span>
            </div>
          {/if}
        </div>
        {#if copyResult}
        <div class="wm-copy-info">
          <div class="wm-copy-player">
            <div class="wm-copy-avatar" style={copyHeadUrl ? `background-image:url('${copyHeadUrl}')` : ''}>
              {#if !copyHeadUrl}<i class="ti ti-user"></i>{/if}
            </div>
            <div class="wm-copy-name-wrap">
              <div class="wm-copy-name">{copyResult.nickname}</div>
              <div class="wm-copy-sub">{copyResult.slimArms ? t('wardrobe.slim') : t('wardrobe.classic')}</div>
            </div>
          </div>

          <div class="wm-toggle-row">
            <div>
              <div class="wm-toggle-label">{t('wardrobe.copy.withCapeLabel')}</div>
              <div class="wm-toggle-hint">{t('wardrobe.copy.withCapeHint')}</div>
            </div>
            <button
              class="wm-toggle {copyIncludeCape ? 'on' : ''} {!copyResult.capeUrl ? 'disabled' : ''}"
              onclick={() => { if (copyResult?.capeUrl) copyIncludeCape = !copyIncludeCape }}
              disabled={!copyResult.capeUrl}
              aria-pressed={copyIncludeCape}
            ></button>
          </div>
        </div>
        {/if}
      </div>
    {/if}
  {/snippet}

  {#snippet foot()}
    <button class="wr2-btn wr2-btn-ghost" onclick={() => (copyModalOpen = false)}>
      {t('wardrobe.editor.cancel')}
    </button>
    <button class="wr2-btn wr2-btn-primary" disabled={!copyResult || copyBusy} onclick={createPresetFromCopy}>
      <i class="ti {copyBusy ? 'ti-loader-2 spin' : 'ti-plus'}"></i>
      {t('wardrobe.copy.createBtn')}
    </button>
  {/snippet}
</WardrobeModal>

<!-- ══════════════ МОДАЛКА: редактор пресету ══════════════ -->
<WardrobeModal
  open={editorOpen}
  onClose={() => (editorOpen = false)}
  title={t('wardrobe.editor.title')}
  icon="ti-edit"
  accent="orange"
  size="xl"
  bodyNoScroll
>
  {#snippet body()}
    <div class="wm-editor">
      <div class="wm-editor-left">
        <div class="wm-editor-3d-card">
          <div class="wm-editor-3d" bind:this={editorViewerContainer}></div>
        </div>
      </div>

      <div class="wm-editor-right">
        <div>
          <label class="wm-label">{t('wardrobe.editor.nameLabel')}</label>
          <div class="wm-input-row">
            <i class="ti ti-tag"></i>
            <input type="text" bind:value={editorName} />
          </div>
          {#if editorName.trim() && editorName !== editorPreset?.name && presets.some((p) => p.id !== editorPreset?.id && p.name === editorName.trim())}
            <div class="wm-name-warn"><i class="ti ti-alert-triangle"></i> {t('wardrobe.editor.nameDuplicate')}</div>
          {/if}
        </div>

        <div>
          <label class="wm-label">{t('wardrobe.editor.armLabel')}</label>
          <div class="wm-seg">
            <div class="wm-seg-thumb" style="transform: translateX({editorSlim ? 100 : 0}%);"></div>
            <button class="wm-seg-btn {!editorSlim ? 'active' : ''}" onclick={() => (editorSlim = false)}>
              {t('wardrobe.editor.armClassic')}
            </button>
            <button class="wm-seg-btn {editorSlim ? 'active' : ''}" onclick={() => (editorSlim = true)}>
              {t('wardrobe.editor.armSlim')}
            </button>
          </div>
        </div>

        <div>
          <label class="wm-label">{t('wardrobe.editor.fileLabel')}</label>
          <button class="wm-drop" onclick={pickEditorFile}>
            <div class="wm-drop-icon"><i class="ti ti-upload"></i></div>
            <div>
              <div class="wm-drop-title">{editorPendingSkin ? t('wardrobe.editor.selectedFile', { name: editorPendingSkinName }) : t('wardrobe.editor.dropHint')}</div>
              <div class="wm-drop-sub">{t('wardrobe.editor.dropSpec')}</div>
            </div>
            <input type="file" bind:this={editorFileInput} accept=".png" style="display:none;" onchange={onEditorFileChange} />
          </button>
        </div>

        <div class="wm-editor-capes">
          <label class="wm-label">{t('wardrobe.editor.capeLabel')}</label>
          <div class="wm-cape-grid">
            <button
              class="wm-cape wm-cape-none {editorSelectedCapeId === '' ? 'sel' : ''}"
              onclick={() => { editorSelectedCapeId = ''; editorPreviewCapeUrl = null; editorCapeTouched = true }}
              title={t('wardrobe.editor.noCape')}
            >
              <div class="wm-cape-swatch wm-cape-swatch-none"><i class="ti ti-ban"></i></div>
              <div class="wm-cape-check"><i class="ti ti-check"></i></div>
            </button>
            {#each editorCapeGrid as cape (cape.id)}
              <button
                class="wm-cape {editorSelectedCapeId === cape.id ? 'sel' : ''}"
                onclick={() => { editorSelectedCapeId = cape.id; editorPreviewCapeUrl = cape.url; editorCapeTouched = true }}
                title={cape.alias || cape.id}
              >
                <div class="wm-cape-swatch" style="background-image:url('{editorCapeThumbs[cape.id] ?? ''}');"></div>
                <div class="wm-cape-check"><i class="ti ti-check"></i></div>
              </button>
            {/each}
            {#if editorCapeGrid.length === 0}
              <div class="wm-cape-empty">{t('wardrobe.editor.capeLocked')}</div>
            {/if}
          </div>
        </div>
      </div>
    </div>
  {/snippet}

  {#snippet foot()}
    {#if editorPreset}
      <button class="wr2-btn wr2-btn-danger wm-foot-left" onclick={deleteFromEditor}>
        <i class="ti ti-trash"></i> {t('wardrobe.editor.delete')}
      </button>
    {/if}
    <button class="wr2-btn wr2-btn-ghost" onclick={() => (editorOpen = false)}>
      {t('wardrobe.editor.cancel')}
    </button>
    <button class="wr2-btn wr2-btn-primary" onclick={saveEditor}>
      <i class="ti ti-device-floppy"></i> {t('wardrobe.editor.save')}
    </button>
  {/snippet}
</WardrobeModal>

<style>
  /* ══════════════ ЗМІННІ ══════════════ */
  /* Всі --wr2-* токени НАСЛІДУЮТЬ активну тему (dark / light / OLED /
     custom): виводяться з глобальних кольорових токенів інтерфейсу, тож
     гардероб автоматично перефарбовується разом з рештою лаунчера.
     --wr2-white — «білий» слід: у темних темах це білий, у світлій —
     колір тексту (темний), тому підсвітки/бордери/скіммери лишаються
     помітними на будь-якому фоні. */
  /* Токени на :root, а не на .wardrobe-root: сторінка гардероба і його
      модальні вікна (WardrobeModal — portal до <body>) мають мати доступ до
      --wr2-* незалежно від того, де опиняється DOM. Усі значення — похідні
      від глобальних токенів теми. Імена --wr2-* унікальні, тож глобально
      вони ні з ким не конфліктують. */
  :global(:root) {
    --wr2-radius: 18px;
    --wr2-radius-sm: 12px;
    --wr2-accent: var(--orange);
    --wr2-accent-2: var(--orange-2);
    --wr2-violet: #7b4dff;
    --wr2-violet-l: #9d7cff;
    --wr2-surface: color-mix(in srgb, var(--bg-panel) 78%, transparent);
    --wr2-surface-hi: color-mix(in srgb, var(--bg-card) 85%, transparent);
    --wr2-border: var(--border);
    --wr2-border-hi: var(--border-hi);
    --wr2-text-mute: var(--text-mute);
    --wr2-text-dim: var(--text-dim);
    /* --wr2-white — «білий» слід: у темних темах білий, у світлій — колір
       тексту, тож підсвітки/бордери/скіммери лишаються помітними скрізь. */
    --wr2-white: var(--text);
    /* Лаштунки (studio-фон 3D-сцен/превʼю) СВІДОМО лишаються темними в усіх
       темах: скіну/плащу потрібен контрастний «задник», як у редакторах. */
    --wr2-stage-a: #171719;
    --wr2-stage-b: #0d0d0f;
    --wr2-stage-prev-a: #1c1c20;
    --wr2-stage-prev-b: #131316;
    --wr2-stage-cape-a: #242428;
    --wr2-stage-cape-b: #16161a;
  }

  /* ══════════════ КОПІЮВАННЯ СКІНА (компактна кнопка в сцені) ══════════════ */
  .wr2-copy-btn {
    position: absolute; top: 12px; right: 12px; z-index: 5;
    overflow: hidden;
    display: inline-flex; align-items: center; gap: 8px;
    padding: 9px 16px;
    border: 1px solid rgba(var(--orange-rgb), 0.55);
    border-radius: 11px;
    background: linear-gradient(135deg, color-mix(in srgb, var(--orange) 88%, #000), color-mix(in srgb, var(--orange-2) 88%, #000));
    color: #fff; font-size: 11.5px; font-weight: 800; letter-spacing: 0.4px;
    font-family: inherit;
    cursor: pointer;
    box-shadow: 0 8px 26px rgba(var(--orange-rgb), 0.4);
    transition: transform 0.18s cubic-bezier(.22,1.2,.36,1), box-shadow 0.2s, filter 0.2s;
  }
  .wr2-copy-btn:hover {
    transform: translateY(-2px);
    box-shadow: 0 12px 34px rgba(var(--orange-rgb), 0.55);
    filter: brightness(1.08);
  }
  .wr2-copy-btn:active { transform: translateY(0) scale(0.97); }
  .wr2-copy-btn i { font-size: 14px; }
  /* У шапці панелі пресетів (замість перемикача Скіни/Плащі) — кнопка
     статична, не накладена на сцену. */
  .wr2-copy-in-head { position: static; flex: 0 0 auto; }

  /* ══════════════ БАНЕР РОЗБІЖНОСТІ НІКІВ ══════════════ */
  .wr2-mismatch-banner {
    display: flex;
    align-items: flex-start;
    gap: 12px;
    padding: 12px 16px;
    margin-bottom: 12px;
    border-radius: var(--wr2-radius-sm);
    background: color-mix(in srgb, #f59e0b 12%, transparent);
    border: 1px solid color-mix(in srgb, #f59e0b 35%, transparent);
    color: var(--text);
    font-size: 13px;
    line-height: 1.5;
  }
  .wr2-mismatch-banner > .ti-alert-triangle {
    color: #f59e0b;
    font-size: 20px;
    flex-shrink: 0;
    margin-top: 1px;
  }
  .wr2-mismatch-text { flex: 1; min-width: 0; }
  .wr2-mismatch-text b { color: var(--text); }
  .wr2-mismatch-banner .wr2-btn { flex-shrink: 0; }

  /* ══════════════ SHELL: зліва 3D, праворуч пресети ══════════════ */
  .wr2-shell {
    display: grid;
    grid-template-columns: minmax(320px, 1fr) minmax(0, 1.5fr);
    gap: 16px;
    flex: 1; min-height: 0;
  }

  /* ══════════════ ШАПКА ПРАВОЇ ПАНЕЛІ (перемикач Скіни/Плащі) ══════════════ */
  .wr2-grid-head {
    display: flex; align-items: center; justify-content: space-between; gap: 12px;
    padding: 12px 14px;
    border-bottom: 1px solid var(--wr2-border);
    flex-shrink: 0;
  }
  .wr2-seg {
    position: relative;
    display: grid; grid-template-columns: 1fr 1fr; gap: 4px;
    padding: 4px;
    background: var(--bg-input);
    border: 1px solid var(--wr2-border);
    border-radius: 12px;
  }
  .wr2-seg-thumb {
    position: absolute; top: 4px; bottom: 4px; left: 4px;
    width: calc(50% - 4px);
    border-radius: 9px;
    background: linear-gradient(135deg, var(--orange), var(--orange-2));
    box-shadow: 0 6px 18px rgba(var(--orange-rgb), 0.35);
    transition: transform 320ms cubic-bezier(.22,1.2,.36,1);
    pointer-events: none;
  }
  .wr2-seg-btn {
    position: relative; z-index: 1;
    display: inline-flex; align-items: center; justify-content: center; gap: 7px;
    padding: 8px 14px;
    background: transparent; border: none;
    color: var(--wr2-text-mute);
    font-family: inherit; font-size: 11.5px; font-weight: 700; letter-spacing: 0.3px;
    border-radius: 9px; cursor: pointer;
    transition: color 0.2s;
  }
  .wr2-seg-btn i { font-size: 14px; }
  .wr2-seg-btn.active { color: #fff; }
  .wr2-seg-btn:hover:not(.active) { color: color-mix(in srgb, var(--wr2-white) 85%, transparent); }
  .wr2-grid-head-count {
    font-size: 10.5px; color: var(--wr2-text-dim); letter-spacing: 0.4px; white-space: nowrap;
  }

  /* ══════════════ SCENE (центр) ══════════════ */
  .wr2-scene {
    display: flex; flex-direction: column; gap: 14px;
    min-height: 0;
  }
  .wr2-scene-card {
    position: relative; flex: 1; min-height: 320px;
    border-radius: var(--wr2-radius);
    overflow: hidden;
    background: linear-gradient(180deg, var(--wr2-stage-a) 0%, var(--wr2-stage-b) 100%);
    border: 1px solid var(--wr2-border);
    cursor: grab;
    box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.03);
  }
  .wr2-scene-card:active { cursor: grabbing; }

  /* Дихаючий градієнт */
  .wr2-scene-bg {
    position: absolute; inset: 0;
    background:
      radial-gradient(70% 55% at 50% 25%, rgba(var(--orange-rgb), 0.18), transparent 60%),
      radial-gradient(60% 50% at 15% 90%, rgba(123, 77, 255, 0.10), transparent 60%),
      radial-gradient(50% 40% at 85% 90%, rgba(59, 130, 246, 0.08), transparent 60%);
    pointer-events: none;
    animation: wr2-breathe 9s ease-in-out infinite;
  }
  @keyframes wr2-breathe {
    0%, 100% { opacity: 0.85; transform: scale(1); }
    50% { opacity: 1; transform: scale(1.05); }
  }
  /* Grid overlay */
  .wr2-scene-grid {
    position: absolute; inset: 0;
    background-image:
      linear-gradient(rgba(255, 255, 255, 0.025) 1px, transparent 1px),
      linear-gradient(90deg, rgba(255, 255, 255, 0.025) 1px, transparent 1px);
    background-size: 32px 32px;
    mask-image: radial-gradient(ellipse at center, black 30%, transparent 80%);
    -webkit-mask-image: radial-gradient(ellipse at center, black 30%, transparent 80%);
    pointer-events: none;
  }

  .wr2-3d-wrap { position: absolute; inset: 0; transition: opacity 0.28s ease; }
  .wr2-3d-wrap.fading { opacity: 0.35; }
  .wr2-3d-canvas { position: absolute; inset: 0; cursor: grab; }
  .wr2-3d-canvas:active { cursor: grabbing; }

  .wr2-scene-hint {
    position: absolute; bottom: 12px; left: 0; right: 0;
    display: flex; align-items: center; justify-content: center; gap: 6px;
    font-size: 10.5px; color: rgba(255, 255, 255, 0.4); pointer-events: none;
  }

  /* Anim toggle glass pill */
  .wr2-anim {
    position: absolute; top: 12px; left: 50%; transform: translateX(-50%);
    display: flex; gap: 4px; padding: 4px;
    background: rgba(20, 20, 24, 0.66);
    border: 1px solid rgba(255, 255, 255, 0.08);
    border-radius: 12px;
    backdrop-filter: blur(14px);
    -webkit-backdrop-filter: blur(14px);
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.35);
  }
  .wr2-anim-btn {
    display: inline-flex; align-items: center; gap: 6px;
    padding: 7px 11px;
    background: transparent; border: none;
    color: rgba(255, 255, 255, 0.65);
    font-size: 10.5px; font-weight: 700; letter-spacing: 0.3px;
    border-radius: 8px; cursor: pointer;
    transition: background 0.2s, color 0.2s, transform 0.2s;
    font-family: inherit;
  }
  .wr2-anim-btn i { font-size: 12px; }
  .wr2-anim-btn:hover { color: #fff; background: rgba(255, 255, 255, 0.05); }
  .wr2-anim-btn.active {
    background: linear-gradient(135deg, var(--orange), var(--orange-2));
    color: #fff;
    box-shadow: 0 4px 12px rgba(var(--orange-rgb), 0.35);
  }

  .wr2-scene-actions { display: flex; gap: 10px; }

  /* ══════════════ КНОПКИ ══════════════ */
  .wr2-btn {
    display: inline-flex; align-items: center; justify-content: center; gap: 8px;
    padding: 12px 20px;
    border: 1px solid transparent;
    border-radius: 12px;
    font-family: inherit; font-size: 12.5px; font-weight: 700; letter-spacing: 0.2px;
    cursor: pointer;
    transition: transform 0.18s cubic-bezier(.22,1.2,.36,1),
                background 0.18s, border-color 0.18s, box-shadow 0.2s;
    flex: 1;
  }
  .wr2-btn i { font-size: 15px; }
  .wr2-btn:disabled { opacity: 0.55; cursor: not-allowed; transform: none !important; }
  .wr2-btn:not(:disabled):hover { transform: translateY(-2px); }
  .wr2-btn-primary {
    background: linear-gradient(135deg, var(--orange) 0%, var(--orange-2) 100%);
    color: #fff;
    box-shadow: 0 8px 22px rgba(var(--orange-rgb), 0.35);
  }
  .wr2-btn-primary:hover { box-shadow: 0 12px 30px rgba(var(--orange-rgb), 0.5); }
  .wr2-btn-ghost {
    background: color-mix(in srgb, var(--wr2-white) 4%, transparent);
    color: color-mix(in srgb, var(--wr2-white) 85%, transparent);
    border-color: var(--wr2-border-hi);
  }
  .wr2-btn-ghost:hover { background: color-mix(in srgb, var(--wr2-white) 8%, transparent); border-color: var(--wr2-border-hi); }
  .wr2-btn-danger {
    background: rgba(239, 68, 68, 0.10);
    color: #ef4444;
    border-color: rgba(239, 68, 68, 0.32);
  }
  .wr2-btn-danger:hover { background: rgba(239, 68, 68, 0.20); border-color: rgba(239, 68, 68, 0.6); }

  /* ══════════════ ПРАВА ПАНЕЛЬ (grid wrap) ══════════════ */
  .wr2-grid-wrap {
    display: flex; flex-direction: column; min-height: 0;
    background: var(--bg-panel);
    border: 1px solid var(--wr2-border);
    border-radius: var(--wr2-radius);
    overflow: hidden;
  }
  .wr2-grid-scroll {
    flex: 1; min-height: 0;
    overflow-y: auto;
    padding: 18px 20px 24px;
    scrollbar-width: thin;
    scrollbar-color: color-mix(in srgb, var(--wr2-white) 10%, transparent) transparent;
  }
  .wr2-grid-scroll::-webkit-scrollbar { width: 8px; }
  .wr2-grid-scroll::-webkit-scrollbar-thumb { background: color-mix(in srgb, var(--wr2-white) 8%, transparent); border-radius: 4px; }

  .wr2-section-head {
    display: flex; align-items: center; gap: 10px;
    padding: 4px 2px 12px;
  }
  .wr2-section-title {
    flex: 1;
    display: inline-flex; align-items: center; gap: 8px;
    font-size: 13px; font-weight: 800;
    color: var(--text);
  }
  .wr2-section-title i { color: var(--orange); font-size: 15px; }
  .wr2-section-count {
    font-size: 10.5px; font-weight: 700; letter-spacing: 0.5px;
    padding: 3px 9px;
    border-radius: 999px;
    background: color-mix(in srgb, var(--wr2-white) 5%, transparent);
    color: var(--wr2-text-mute);
  }

  .wr2-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
    gap: 12px;
  }
  .wr2-grid-compact { grid-template-columns: repeat(auto-fill, minmax(130px, 1fr)); }

  /* ── Card ── */
  .wr2-card {
    position: relative;
    display: flex; flex-direction: column;
    /* Пресет-картки ЗАВЖДИ портретні (3:4), незалежно від ширини колонки. */
    aspect-ratio: 3 / 4;
    border-radius: 14px;
    background: linear-gradient(170deg, color-mix(in srgb, var(--wr2-white) 4%, transparent), color-mix(in srgb, var(--wr2-white) 1%, transparent));
    border: 1px solid var(--wr2-border);
    overflow: hidden;
    cursor: pointer;
    transition: transform 0.22s cubic-bezier(.22,1.2,.36,1),
                border-color 0.2s, box-shadow 0.25s, background 0.2s;
  }
  .wr2-card:hover {
    transform: translateY(-4px);
    border-color: var(--wr2-border-hi);
    box-shadow: 0 18px 40px rgba(0, 0, 0, 0.35);
  }
  .wr2-card:hover .wr2-card-halo { opacity: 1; }
  .wr2-card:hover .wr2-card-actions { opacity: 1; transform: translateY(0); }
  .wr2-card.active {
    border-color: rgba(var(--orange-rgb), 0.7);
    box-shadow: 0 0 0 1px rgba(var(--orange-rgb), 0.35), 0 14px 34px rgba(var(--orange-rgb), 0.22);
  }
  .wr2-card.busy { opacity: 0.55; pointer-events: none; }

  /* ── Drag&drop для grid пресетів гардероба (Modrinth-style) ─────────
     Картка-джерело стає напівпрозорою. Напрямок вставки показується
     ЛІНІЄЮ В ЗАЗОРІ МІЖ картками (вертикальна лінія зліва для before,
     справа для after), а не однаковим ободком на цілі — раніше було
     незрозуміло, КУДИ саме встане пресет (перед чи після). Після дропу
     позиції анімує animate:flip на .wr2-card. */
  .wr2-card { overflow: visible; }
  .wr2-card.dragging { opacity: 0.32; cursor: grabbing; filter: saturate(0.4); }

  .wr2-card.drop-before::before,
  .wr2-card.drop-after::after {
    content: '';
    position: absolute;
    top: 4px; bottom: 4px;
    width: 3px;
    border-radius: 3px;
    background: var(--orange);
    box-shadow: 0 0 10px color-mix(in srgb, var(--orange) 65%, transparent);
    z-index: 6;
    pointer-events: none;
    animation: wr2-drop-line-in .13s ease both;
  }
  .wr2-card.drop-before::before { left: -7px; }
  .wr2-card.drop-after::after { right: -7px; }
  @keyframes wr2-drop-line-in {
    from { opacity: 0; transform: scaleY(.6); }
    to   { opacity: 1; transform: scaleY(1); }
  }

  .wr2-card-preview {
    position: relative;
    flex: 1; min-height: 0;
    display: flex; align-items: center; justify-content: center;
    overflow: hidden;
    /* Картка має overflow:visible (для drop-підсвітки), тож її фон більше
       не обрізається нею — щоб градієнт-стадія тут не «вилазив» за округлення
       картки, округлюємо верхні кути прев'ю сам/себе (і overflow:hidden тримає
       halo/скелетон всередині своєї округленої області). */
    border-radius: 14px 14px 0 0;
    background:
      radial-gradient(60% 50% at 50% 40%, rgba(var(--orange-rgb), 0.10), transparent 70%),
      linear-gradient(180deg, var(--wr2-stage-prev-a) 0%, var(--wr2-stage-prev-b) 100%);
  }
  .wr2-card-halo {
    position: absolute; inset: -30% -30% -30% -30%;
    background: radial-gradient(circle, rgba(var(--orange-rgb), 0.14), transparent 60%);
    opacity: 0; transition: opacity 0.35s; pointer-events: none;
  }
  .wr2-card.active .wr2-card-halo { opacity: 1; }

  .wr2-badge-applied {
    position: absolute; top: 8px; left: 8px;
    width: 22px; height: 22px;
    border-radius: 50%;
    background: linear-gradient(135deg, var(--orange), var(--orange-2));
    color: #fff;
    display: flex; align-items: center; justify-content: center;
    font-size: 12px;
    box-shadow: 0 4px 10px rgba(var(--orange-rgb), 0.5);
    z-index: 3;
    animation: wr2-pop-in 0.35s cubic-bezier(.22,1.6,.36,1);
  }
  @keyframes wr2-pop-in { from { transform: scale(0); } to { transform: scale(1); } }

  .wr2-card-info {
    padding: 10px 12px 12px;
  }
  .wr2-card-name {
    font-size: 12.5px; font-weight: 700; color: var(--text);
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .wr2-card-meta {
    display: flex; flex-wrap: wrap; gap: 5px;
    margin-top: 6px;
  }
  .wr2-chip {
    display: inline-flex; align-items: center; gap: 4px;
    padding: 3px 8px;
    font-size: 9.5px; font-weight: 700; letter-spacing: 0.4px; text-transform: uppercase;
    color: var(--wr2-text-mute);
    background: color-mix(in srgb, var(--wr2-white) 4%, transparent);
    border: 1px solid var(--wr2-border);
    border-radius: 999px;
  }
  .wr2-chip i { font-size: 10px; }
  .wr2-chip-violet {
    color: var(--wr2-violet-l);
    background: rgba(123, 77, 255, 0.12);
    border-color: rgba(123, 77, 255, 0.3);
  }

  .wr2-card-actions {
    position: absolute; top: 8px; right: 8px;
    display: flex; gap: 6px;
    opacity: 0; transform: translateY(-4px);
    transition: opacity 0.2s, transform 0.2s;
    z-index: 3;
  }
  .wr2-icon-btn {
    width: 30px; height: 30px;
    display: flex; align-items: center; justify-content: center;
    border-radius: 9px;
    background: rgba(20, 20, 24, 0.75);
    backdrop-filter: blur(8px);
    border: 1px solid rgba(255, 255, 255, 0.1);
    color: #fff;
    font-size: 13px;
    cursor: pointer;
    transition: background 0.15s, transform 0.15s, border-color 0.15s;
  }
  .wr2-icon-btn:hover { background: rgba(255, 255, 255, 0.14); transform: scale(1.08); }
  .wr2-icon-danger { color: #ff8a8a; }
  .wr2-icon-danger:hover { background: rgba(239, 68, 68, 0.28); border-color: rgba(239, 68, 68, 0.5); }

  /* ── Add card ── */
  .wr2-add-card {
    display: flex; flex-direction: column; align-items: center; justify-content: center;
    gap: 10px;
    aspect-ratio: 3 / 4; min-height: 0;
    border: 1.5px dashed var(--wr2-border-hi);
    border-radius: 14px;
    background: color-mix(in srgb, var(--wr2-white) 1.5%, transparent);
    color: var(--wr2-text-mute);
    cursor: pointer;
    transition: background 0.2s, border-color 0.2s, color 0.2s, transform 0.2s;
    font-family: inherit;
    padding: 12px;
  }
  .wr2-add-card:hover {
    border-color: rgba(var(--orange-rgb), 0.6);
    color: var(--orange);
    background: rgba(var(--orange-rgb), 0.05);
    transform: translateY(-3px);
  }
  .wr2-add-icon {
    width: 44px; height: 44px;
    display: flex; align-items: center; justify-content: center;
    border-radius: 50%;
    background: color-mix(in srgb, var(--wr2-white) 5%, transparent);
    font-size: 20px;
    transition: background 0.2s, transform 0.35s cubic-bezier(.22,1.6,.36,1);
  }
  .wr2-add-card:hover .wr2-add-icon {
    background: rgba(var(--orange-rgb), 0.18);
    transform: rotate(90deg);
  }
  .wr2-add-title { font-size: 12px; font-weight: 700; }
  .wr2-add-sub { font-size: 10px; color: var(--wr2-text-dim); text-align: center; }

  .wr2-empty {
    grid-column: 1 / -1;
    text-align: center;
    padding: 40px 20px;
    color: var(--wr2-text-mute);
    font-size: 12px;
  }

  /* ── Skeleton ── */
  .wr2-skeleton {
    position: absolute; inset: 0;
    background:
      linear-gradient(110deg,
        color-mix(in srgb, var(--wr2-white) 4%, transparent) 8%,
        color-mix(in srgb, var(--wr2-white) 10%, transparent) 18%,
        color-mix(in srgb, var(--wr2-white) 4%, transparent) 28%);
    background-size: 200% 100%;
    animation: wr2-skeleton 1.4s linear infinite;
  }
  @keyframes wr2-skeleton {
    from { background-position: 200% 0; }
    to   { background-position: -200% 0; }
  }
  .wr2-busy-overlay {
    position: absolute; inset: 0;
    display: flex; align-items: center; justify-content: center;
    background: rgba(0, 0, 0, 0.5);
    color: #fff; font-size: 22px;
  }
  .spin { animation: wr2-spin 1s linear infinite; }
  @keyframes wr2-spin { to { transform: rotate(360deg); } }

  /* ── Capes grid ── */
  .wr2-capes-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(110px, 1fr));
    gap: 12px;
  }
  .wr2-cape-card {
    position: relative;
    display: flex; flex-direction: column; gap: 6px;
    padding: 8px 8px 10px;
    border-radius: 14px;
    background: linear-gradient(170deg, color-mix(in srgb, var(--wr2-white) 4%, transparent), color-mix(in srgb, var(--wr2-white) 1%, transparent));
    border: 1.5px solid var(--wr2-border);
    cursor: pointer;
    transition: transform 0.22s cubic-bezier(.22,1.2,.36,1),
                border-color 0.2s, box-shadow 0.25s;
    font-family: inherit;
    color: inherit;
    text-align: center;
  }
  .wr2-cape-card:disabled { cursor: default; }
  .wr2-cape-card:not(:disabled):hover {
    transform: translateY(-3px);
    border-color: rgba(123, 77, 255, 0.45);
    box-shadow: 0 14px 32px rgba(123, 77, 255, 0.18);
  }
  .wr2-cape-card.active {
    border-color: var(--wr2-violet);
    box-shadow: 0 0 0 1px rgba(123, 77, 255, 0.35), 0 12px 30px rgba(123, 77, 255, 0.25);
  }
  .wr2-cape-swatch {
    position: relative;
    padding-bottom: 160%; /* 10:16 */
    border-radius: 10px;
    background:
      linear-gradient(160deg, var(--wr2-stage-cape-a), var(--wr2-stage-cape-b));
    background-repeat: no-repeat;
    background-size: 100% 100%;
    background-position: center;
    image-rendering: pixelated;
    overflow: hidden;
  }
  .wr2-cape-swatch-none {
    display: flex; align-items: center; justify-content: center;
    padding: 0;
    height: 130px;
    background: color-mix(in srgb, var(--wr2-white) 2%, transparent);
    border: 1.5px dashed var(--wr2-border-hi);
    color: color-mix(in srgb, var(--wr2-white) 35%, transparent);
    font-size: 20px;
  }
  .wr2-cape-name {
    font-size: 10.5px; font-weight: 700;
    color: color-mix(in srgb, var(--wr2-white) 75%, transparent);
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .wr2-cape-skeleton {
    padding: 0;
    height: 190px;
    background: color-mix(in srgb, var(--wr2-white) 3%, transparent);
    animation: wr2-skeleton-fade 1.4s ease-in-out infinite;
    border-color: transparent;
  }
  @keyframes wr2-skeleton-fade {
    0%, 100% { opacity: 0.4; }
    50%      { opacity: 0.85; }
  }

  /* ══════════════ MODAL CONTENT STYLES ══════════════ */
  .wm-desc { color: var(--wr2-text-mute); font-size: 12px; line-height: 1.55; margin: 0 0 14px; }
  .wm-input-row { position: relative; }
  .wm-input-row i {
    position: absolute; left: 14px; top: 50%; transform: translateY(-50%);
    color: var(--wr2-text-mute); font-size: 15px; pointer-events: none;
  }
  .wm-input-row input {
    width: 100%; padding: 11px 14px 11px 40px;
    background: var(--bg-input);
    border: 1px solid var(--wr2-border-hi);
    border-radius: 12px;
    color: var(--text); font-size: 13px; font-family: inherit;
    transition: border-color 0.15s, background 0.15s;
  }
  .wm-input-row input:focus {
    outline: none;
    border-color: var(--orange);
    background: rgba(var(--orange-rgb), 0.08);
  }

  .wm-status {
    font-size: 11.5px; color: var(--wr2-text-mute);
    margin-top: 10px;
    display: flex; align-items: center; gap: 6px;
    min-height: 16px;
    transition: color 0.2s;
  }
  .wm-status.err { color: #ff6b6b; }
  .wm-status.ok  { color: #10b981; }
  .wm-status i { font-size: 14px; }

  .wm-copy-result {
    display: flex; gap: 16px; margin-top: 16px;
    padding: 14px;
    background: var(--bg-input);
    border: 1px solid var(--wr2-border);
    border-radius: 14px;
  }
  .wm-copy-3d {
    width: 150px; height: 200px; flex-shrink: 0;
    border-radius: 12px;
    overflow: hidden;
    cursor: grab;
    position: relative;
    background:
      radial-gradient(60% 50% at 50% 30%, rgba(var(--orange-rgb), 0.14), transparent 65%),
      linear-gradient(180deg, #1a1a1e, #0f0f12);
    border: 1px solid var(--wr2-border);
  }
  /* Плейсхолдер «тягнемо скін з Mojang» — поверх сцени моделі (канва
     лишається під ним і активується сама, щойно прилетіла текстура). */
  .wm-copy-loading {
    position: absolute; inset: 0; z-index: 3;
    display: flex; flex-direction: column; align-items: center; justify-content: center;
    gap: 8px; padding: 10px; text-align: center;
    background: color-mix(in srgb, #0f0f12 78%, transparent);
    backdrop-filter: blur(2px);
    font: 500 10px/1.35 var(--font-ui); color: color-mix(in srgb, var(--wr2-white) 78%, transparent);
    pointer-events: none;
  }
  .wm-copy-loading i { font-size: 22px; color: var(--orange); }
  .wm-copy-3d:active { cursor: grabbing; }
  .wm-copy-info { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 14px; justify-content: center; }
  .wm-copy-player {
    display: flex; align-items: center; gap: 14px;
    padding: 12px;
    background: color-mix(in srgb, var(--wr2-white) 3%, transparent);
    border: 1px solid var(--wr2-border);
    border-radius: 12px;
  }
  .wm-copy-avatar {
    width: 52px; height: 52px; flex-shrink: 0;
    border-radius: 10px;
    background: linear-gradient(160deg, #3a3a44, #1a1a20);
    background-size: cover; background-position: center;
    image-rendering: pixelated;
    display: flex; align-items: center; justify-content: center;
    color: #fff; font-size: 22px;
  }
  .wm-copy-name { font-size: 13.5px; font-weight: 800; color: var(--text); }
  .wm-copy-sub { font-size: 10.5px; color: var(--wr2-text-mute); margin-top: 2px; text-transform: uppercase; letter-spacing: 0.4px; }

  .wm-toggle-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
  .wm-toggle-label { font-size: 12px; font-weight: 700; color: var(--text); }
  .wm-toggle-hint { font-size: 10.5px; color: var(--wr2-text-mute); margin-top: 2px; }
  .wm-toggle {
    position: relative;
    width: 46px; height: 26px;
    border-radius: 999px;
    background: color-mix(in srgb, var(--wr2-white) 8%, transparent);
    border: 1px solid var(--wr2-border-hi);
    cursor: pointer;
    transition: background 0.22s, border-color 0.22s;
    flex-shrink: 0;
  }
  .wm-toggle::after {
    content: ''; position: absolute; top: 2px; left: 3px;
    width: 18px; height: 18px; border-radius: 50%;
    background: #888; box-shadow: 0 2px 4px rgba(0, 0, 0, 0.35);
    transition: transform 0.24s cubic-bezier(.22,1.4,.36,1), background 0.22s;
  }
  .wm-toggle.on { background: rgba(var(--orange-rgb), 0.28); border-color: rgba(var(--orange-rgb), 0.55); }
  .wm-toggle.on::after { transform: translateX(20px); background: var(--orange); box-shadow: 0 2px 8px rgba(var(--orange-rgb), 0.5); }
  .wm-toggle.disabled { opacity: 0.4; cursor: not-allowed; }

  /* Editor: тіло модалки (bodyNoScroll) віддає всю висоту, тому картка
     з 3D розтягується/стискається під доступне місце БЕЗ власного скролу,
     а права колонка з налаштуваннями скролиться сама. */
  .wm-editor {
    display: grid;
    grid-template-columns: minmax(280px, 1.05fr) minmax(0, 1fr);
    gap: 18px;
    flex: 1;
    min-height: 0;
    height: 100%;
    padding: 18px;
  }
  .wm-editor-left { display: flex; min-height: 0; }
  .wm-editor-3d-card {
    position: relative;
    flex: 1;
    /* Мінімум лише щоб превʼю не перетворилось на смужку на дуже низькій
       модалці; в іншому картка розтягується/стискається під доступне місце. */
    min-height: 140px;
    border-radius: 14px;
    overflow: hidden;
    background: #101013;
    border: 1px solid var(--wr2-border);
    cursor: grab;
  }
  .wm-editor-3d-card:active { cursor: grabbing; }
  .wm-editor-3d { position: absolute; inset: 0; }
  .wm-editor-right {
    display: flex; flex-direction: column; gap: 14px;
    min-height: 0;
    /* Налаштування скроляться САМІ, незалежно від 3D-картки. */
    overflow-y: auto;
    padding-right: 4px;
    scrollbar-width: thin;
    scrollbar-color: color-mix(in srgb, var(--wr2-white) 10%, transparent) transparent;
  }
  .wm-editor-right::-webkit-scrollbar { width: 6px; }
  .wm-editor-right::-webkit-scrollbar-thumb { background: color-mix(in srgb, var(--wr2-white) 10%, transparent); border-radius: 3px; }
  .wm-label {
    font-size: 10.5px; font-weight: 700;
    color: var(--wr2-text-mute);
    letter-spacing: 1.5px; text-transform: uppercase;
    display: block; margin-bottom: 6px;
  }
  .wm-name-warn {
    display: flex; align-items: center; gap: 6px;
    margin-top: 6px;
    font-size: 11px; color: #ffb454; font-weight: 600;
  }
  .wm-name-warn i { font-size: 13px; }

  /* Segmented control з sliding thumb */
  .wm-seg {
    position: relative;
    display: grid; grid-template-columns: 1fr 1fr; gap: 4px;
    padding: 4px;
    background: var(--bg-input);
    border: 1px solid var(--wr2-border);
    border-radius: 12px;
  }
  .wm-seg-thumb {
    position: absolute; top: 4px; bottom: 4px; left: 4px;
    width: calc(50% - 4px);
    border-radius: 9px;
    background: linear-gradient(135deg, var(--orange), var(--orange-2));
    box-shadow: 0 6px 18px rgba(var(--orange-rgb), 0.35);
    transition: transform 320ms cubic-bezier(.22,1.2,.36,1);
    pointer-events: none;
  }
  .wm-seg-btn {
    position: relative; z-index: 1;
    padding: 9px 12px;
    background: transparent; border: none;
    color: var(--wr2-text-mute);
    font-family: inherit; font-size: 11.5px; font-weight: 700; letter-spacing: 0.3px;
    border-radius: 9px; cursor: pointer;
    transition: color 0.2s;
  }
  .wm-seg-btn.active { color: #fff; }
  .wm-seg-btn:hover:not(.active) { color: color-mix(in srgb, var(--wr2-white) 85%, transparent); }

  /* Dropzone */
  .wm-drop {
    display: flex; align-items: center; gap: 12px;
    width: 100%; padding: 14px;
    background: color-mix(in srgb, var(--wr2-white) 2%, transparent);
    border: 1.5px dashed var(--wr2-border-hi);
    border-radius: 12px;
    color: inherit; font-family: inherit;
    cursor: pointer;
    transition: background 0.2s, border-color 0.2s;
    text-align: left;
  }
  .wm-drop:hover { background: rgba(var(--orange-rgb), 0.06); border-color: rgba(var(--orange-rgb), 0.6); }
  .wm-drop-icon {
    width: 40px; height: 40px; flex-shrink: 0;
    display: flex; align-items: center; justify-content: center;
    border-radius: 10px;
    background: rgba(var(--orange-rgb), 0.12);
    color: var(--orange);
    font-size: 18px;
  }
  .wm-drop-title { font-size: 12px; font-weight: 700; color: var(--text); }
  .wm-drop-sub { font-size: 10.5px; color: var(--wr2-text-dim); margin-top: 2px; }

  /* Editor cape grid */
  .wm-editor-capes {
    flex: 1; min-height: 0;
    display: flex; flex-direction: column;
    overflow: hidden;
  }
  .wm-cape-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(64px, 1fr));
    gap: 8px;
    flex: 1; min-height: 0;
    overflow-y: auto;
    padding-right: 4px; padding-top: 4px;
    align-content: start;
  }
  .wm-cape-grid::-webkit-scrollbar { width: 6px; }
  .wm-cape-grid::-webkit-scrollbar-thumb { background: color-mix(in srgb, var(--wr2-white) 10%, transparent); border-radius: 3px; }
  .wm-cape {
    position: relative;
    padding: 0; padding-bottom: 160%; /* 10:16 */
    border-radius: 10px;
    border: 1.5px solid var(--wr2-border);
    background: linear-gradient(160deg, var(--wr2-stage-cape-a), var(--wr2-stage-cape-b));
    cursor: pointer;
    overflow: hidden;
    transition: transform 0.2s, border-color 0.2s, box-shadow 0.2s;
  }
  .wm-cape:hover { transform: translateY(-1px); border-color: var(--wr2-border-hi); }
  .wm-cape.sel { border-color: var(--wr2-violet); box-shadow: 0 0 0 2px rgba(123, 77, 255, 0.28); }
  .wm-cape-swatch {
    position: absolute; inset: 0;
    background-repeat: no-repeat;
    background-size: 100% 100%;
    background-position: center;
    image-rendering: pixelated;
  }
  .wm-cape-swatch-none {
    display: flex; align-items: center; justify-content: center;
    background: color-mix(in srgb, var(--wr2-white) 3%, transparent);
    border: 1.5px dashed var(--wr2-border-hi);
    box-sizing: border-box;
    color: var(--wr2-text-dim); font-size: 13px;
  }
  .wm-cape-check {
    position: absolute; top: 4px; right: 4px;
    width: 14px; height: 14px; border-radius: 50%;
    background: var(--wr2-violet); color: #fff;
    display: none; align-items: center; justify-content: center;
    font-size: 9px;
  }
  .wm-cape.sel .wm-cape-check { display: flex; }
  .wm-cape-empty {
    grid-column: 1 / -1;
    padding: 10px;
    font-size: 11px; color: var(--wr2-text-dim);
  }
  .wm-foot-left { margin-right: auto; flex: 0 0 auto; }

  /* ── Responsive ── */
  @media (max-width: 1180px) {
    .wr2-shell { grid-template-columns: minmax(300px, 1fr) minmax(0, 1.35fr); }
    /* На вузькій сцені кнопка «Скопіювати» лишає текст — круглий знак */
    .wr2-copy-btn .wr2-copy-label { display: none; }
    .wr2-copy-btn { padding: 10px; border-radius: 50%; }
  }
  @media (max-width: 900px) {
    .wr2-shell { grid-template-columns: 1fr; grid-template-rows: minmax(340px, 46%) 1fr; }
    .wr2-scene { grid-column: 1 / 2; grid-row: 1 / 2; }
    .wr2-grid-wrap { grid-column: 1 / 2; grid-row: 2 / 3; }
    /* Редактор: 3D згори (висота підлаштовується під вікно), налаштування знизу. */
    .wm-editor { grid-template-columns: 1fr; grid-template-rows: minmax(280px, 42%) 1fr; }
  }
  @media (prefers-reduced-motion: reduce) {
    .wr2-copy-shine, .wr2-scene-bg { animation: none; }
    .wr2-card, .wr2-seg-btn, .wr2-btn { transition: none; }
  }
</style>
