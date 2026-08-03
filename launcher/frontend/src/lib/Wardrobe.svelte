<script lang="ts">
  // ── Гардероб ──────────────────────────────────────────────────────────
  // Перенесено з WardrobeScreen.java (JavaFX): ліва 3D-панель + права
  // галерея пресетів з двома акордеон-секціями ("Збережені скіни" /
  // "Усталені скіни"), кожна секція скролиться незалежно від решти
  // сторінки. Плюс нова фіча: "Скопіювати скін гравця" за ніком.
  //
  // 3D-переглядач — реальний рендер через three.js + GLTFLoader
  // (skin-model-launcher.glb / -slim.glb з анімаціями idle/idle1/idle4/
  // idle23/walk). Вся логіка винесена в окремий модуль wardrobe3d.ts
  // (createWardrobeViewer) — дивись коментар біля #wrViewer нижче.

  import { onMount, onDestroy } from 'svelte'
  import { t } from './i18n.svelte'
  import { App } from './wails'
  import { toast, confirmDialog } from './toast.svelte'
  import { createWardrobeViewer, type WardrobeViewerHandle } from './wardrobe3d'
  import MiniSkin3D from './MiniSkin3D.svelte'
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

  let { activeAccount }: { activeAccount: Account | undefined } = $props()

  let presets = $state<SkinPreset[]>([])
  let selectedPresetId = $state<string | null>(null)
  // activePresetId — пресет, який ЗАРАЗ стоїть на акаунті (Mojang).
  // Кнопка «Застосувати» показується ЛИШЕ коли обраний пресет відрізняється
  // від нього (тобто є незастосовані зміни скіна/плаща/моделі) — якщо
  // змін немає, кнопки немає (ТЗ гардероба).
  let activePresetId = $state<string | null>(null)
  let loading = $state(true)
  let applying = $state(false)

  let savedCollapsed = $state(false)
  let defaultsCollapsed = $state(true)

  // id пресету, створеного з кожного дефолтного скіна (щоб підсвічувати
  // активну картку у секції «Усталені скіни»).
  let defaultPresetIds = $state<Record<string, string>>({})
  // назва дефолтного скіна, який зараз застосовується (спінер/блокування картки).
  let defaultBusy = $state('')

  let anim = $state<'idle' | 'sit' | 'look'>('idle')

  // ── 3D-переглядач (three.js, див. wardrobe3d.ts) ──
  let viewerContainer: HTMLDivElement | undefined = $state()
  let viewer: WardrobeViewerHandle | null = $state(null)

  onMount(() => {
    if (viewerContainer) {
      createWardrobeViewer(viewerContainer).then((h) => {
        viewer = h
      })
    }
  })

  onDestroy(() => {
    viewer?.dispose()
    viewer = null
    editorViewer?.dispose()
    editorViewer = null
  })

  // Коли вибрано пресет — оновити модель (classic/slim), текстуру скіна і плаща.
  // Якщо вибраного пресету нема (поточний вигляд акаунта не збігається з жодним
  // збереженим пресетом) — показуємо реальний скін+плащ, що стоять на Mojang,
  // як це робив старий лаунчер.
  $effect(() => {
    const p = selectedPreset()
    const v = viewer
    if (!v) return

    if (p) {
      v.setSlim(p.slimArms)
      let alive = true
      App.GetWardrobeTextureDataURL(p.skinPath)
        .then((dataUrl) => {
          if (alive) v.setTexture(dataUrl)
        })
        .catch(() => {
          // Текстура недоступна — лишаємо стандартний матеріал моделі.
        })
      if (p.capePath) {
        App.GetWardrobeTextureDataURL(p.capePath)
          .then((d) => {
            if (alive) v.setCapeTexture(d)
          })
          .catch(() => {
            if (alive) v.setCapeTexture(null)
          })
      } else {
        v.setCapeTexture(null)
      }
      return () => {
        alive = false
      }
    }

    // Жоден пресет не збігається з акаунтом — підтягуємо активний вигляд
    // акаунта (скін + плащ) з Mojang, щоб гардероб його показав.
    const acc = activeAccount
    if (!acc?.id) return
    let alive = true
    App.GetWardrobeActiveLook(acc.id)
      .then((look) => {
        if (!alive || !look) return
        v.setSlim(look.slimArms)
        v.setTexture(look.skinDataUrl)
        v.setCapeTexture(look.capeDataUrl ?? null)
      })
      .catch(() => {})
    return () => {
      alive = false
    }
  })

  // Кнопки Idle/Сидіти/Огляд — програємо кліп у поточній моделі.
  $effect(() => {
    viewer?.playAnimation(anim)
  })

  // ── Міні-3D превʼю карток ──
  // Текстури збережених пресетів підтягуємо з бекенду; дефолтні ванільні
  // скіни — вшиті ассети (steveSkin/alexSkin/...).
  //
  // БАГ (виправлено): раніше кеш індексувався лише за p.id, тому коли
  // пресет оновлювався (новий скін/плащ, той самий id) — картка
  // продовжувала показувати стару текстуру. Тепер разом із самим
  // значенням тексту зберігаємо "підпис" (skinPath+capePath), який
  // порівнюється при кожному рендері presets — зміна шляху інвалідує
  // кеш і перезавантажує текстуру наново.
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
      // Скидаємо старе значення, щоб MiniSkin3D одразу перерендерився
      // (undefined = "запит у польоті", як і при першому завантаженні).
      delete presetTextures[p.id]
      delete presetCapeTextures[p.id]
      App.GetWardrobeTextureDataURL(p.skinPath)
        .then((d) => {
          presetTextures[p.id] = d
        })
        .catch(() => {
          presetTextures[p.id] = ''
        })
      if (p.capePath) {
        App.GetWardrobeTextureDataURL(p.capePath)
          .then((d) => {
            presetCapeTextures[p.id] = d
          })
          .catch(() => {
            presetCapeTextures[p.id] = ''
          })
      }
    }
  })

  // ── 3D-превʼю в редакторі (жива модель, drag + idle) ──
  let editorViewerContainer: HTMLDivElement | undefined = $state()
  let editorViewer: WardrobeViewerHandle | null = $state(null)

  // Створюємо/знищуємо переглядач разом із відкриттям модалки.
  $effect(() => {
    if (!editorOpen) {
      editorViewer?.dispose()
      editorViewer = null
      return
    }
    const el = editorViewerContainer
    if (el && !editorViewer) {
      createWardrobeViewer(el).then((h) => {
        if (editorOpen) editorViewer = h
        else h.dispose()
      })
    }
  })

  // Текстура редактора (скіна): поточний пресет або ванільний Steve для нового.
  // Плащ на 3D окремо оновлює ефект нижче — завдяки editorPreviewCapeUrl.
  $effect(() => {
    const v = editorViewer
    if (!v) return
    const p = editorPreset
    if (p) {
      let alive = true
      App.GetWardrobeTextureDataURL(p.skinPath)
        .then((d) => {
          if (alive) v.setTexture(d)
        })
        .catch(() => {})
      return () => {
        alive = false
      }
    } else {
      v.setTexture(steveSkin)
    }
  })

  // Плащ у редакторі: єдине джерело істини для того, що видно на 3D, — це
  // editorPreviewCapeUrl. Він оновлюється у openEditor (поточний плащ пресету
  // чи null) і кліками по картках плащів.
  // БАГ (виправлено): раніше при editorSelectedCapeId === '' ("без плаща")
  // ефект просто виходив нічого не роблячи — плащ, накладений попереднім
  // ефектом (з p.capePath пресету), так і лишався видимим на моделі. Тепер
  // порожній вибір явно знімає текстуру плаща через editorPreviewCapeUrl = null.
  $effect(() => {
    const v = editorViewer
    if (!v || !editorOpen) return
    const url = editorPreviewCapeUrl
    if (!url) {
      v.setCapeTexture(null)
      return
    }
    let alive = true
    App.GetWardrobeCapeDataURL(url)
      .then((d) => {
        if (alive) v.setCapeTexture(d)
      })
      .catch(() => {
        if (alive) v.setCapeTexture(null)
      })
    return () => {
      alive = false
    }
  })

  // Перемикач classic/slim у редакторі оновлює модель одразу.
  $effect(() => {
    editorViewer?.setSlim(editorSlim)
  })

  // ── Модалка "Скопіювати скін гравця" ──
  let copyModalOpen = $state(false)
  let copyNick = $state('')
  let copyStatus = $state<'idle' | 'searching' | 'found' | 'notfound' | 'nosk' | 'error'>('idle')
  let copyResult = $state<PlayerProfile | null>(null)
  let copyIncludeCape = $state(true)
  let copyBusy = $state(false)
  let copyDebounceTimer: ReturnType<typeof setTimeout> | undefined

  // 3D-переглядач скіна знайденого гравця — зʼявляється зліва під полем
  // пошуку одразу як тільки гравця знайдено (copyStatus === 'found').
  let copyViewerContainer: HTMLDivElement | undefined = $state()
  let copyViewer: WardrobeViewerHandle | null = $state(null)

  // 2D-голова знайденого гравця (для блоку інформації в модалці копіювання):
  // вирізаємо ПЕРЕДНЮ панель голови з його скіна (квадрат 8x8 на позиції
  // 8,8 у текстурі 64x64) і збільшуємо до 52x52 без згладжування — та сама
  // голова, що показується в сайдбарі для акаунтів лаунчера. Запасна
  // іконка — якщо скін не вдалося завантажити.
  let copyHeadUrl = $state('')
  async function loadCopyHead(p: PlayerProfile) {
    copyHeadUrl = ''
    try {
      const skin = await App.GetWardrobeRemoteSkinDataURL(p.skinUrl)
      const img = new Image()
      img.onload = () => {
        try {
          const c = document.createElement('canvas')
          c.width = 52
          c.height = 52
          const ctx = c.getContext('2d')
          if (!ctx) return
          ctx.imageSmoothingEnabled = false
          ctx.drawImage(img, 8, 8, 8, 8, 0, 0, 52, 52)
          copyHeadUrl = c.toDataURL('image/png')
        } catch { /* запасна іконка */ }
      }
      img.onerror = () => { /* запасна іконка */ }
      img.src = skin
    } catch { /* запасна іконка */ }
  }

  $effect(() => {
    if (!copyModalOpen || !copyResult) {
      copyViewer?.dispose()
      copyViewer = null
      return
    }
    const el = copyViewerContainer
    if (el && !copyViewer) {
      createWardrobeViewer(el).then((h) => {
        if (copyModalOpen && copyResult) copyViewer = h
        else h.dispose()
      })
    }
  })

  onDestroy(() => {
    copyViewer?.dispose()
    copyViewer = null
  })

  // Текстура/поза гравця в переглядачі копіювання: скін завжди показуємо,
  // плащ — лише якщо тумблер "Разом з плащем" увімкнено (кнопка "Копіювати
  // плащ" відображає стан того самого тумблера, як і вимагає ТЗ).
  $effect(() => {
    const v = copyViewer
    const p = copyResult
    if (!v || !p) return
    v.setSlim(p.slimArms)
    let alive = true
    App.GetWardrobeRemoteSkinDataURL(p.skinUrl)
      .then((d) => {
        if (alive) v.setTexture(d)
      })
      .catch(() => {})
    if (copyIncludeCape && p.capeUrl) {
      App.GetWardrobeCapeDataURL(p.capeUrl)
        .then((d) => {
          if (alive) v.setCapeTexture(d)
        })
        .catch(() => {
          if (alive) v.setCapeTexture(null)
        })
    } else {
      v.setCapeTexture(null)
    }
    return () => {
      alive = false
    }
  })

  // ── Модалка редактора пресету ──
  let editorOpen = $state(false)
  let editorPreset = $state<SkinPreset | null>(null)
  let editorName = $state('')
  let editorSlim = $state(false)
  let editorCapeGrid = $state<CapeInfo[]>([])
  let editorSelectedCapeId = $state<string>('') // '' = без плаща (обрано користувачем)
  // URL плаща, який зараз показується на 3D в редакторі; null = без плаща.
  // Не залежить напряму від editorSelectedCapeId, щоб вибір "без плаща"
  // гарантовано знімав плащ навіть якщо список плащів ще не довантажився.
  let editorPreviewCapeUrl = $state<string | null>(null)
  // Чи користувач вже свідомо обрав плащ/його відсутність. Доки false —
  // кнопка збереження не чіпає плащ на акаунті (напр. коли плащ пресету ще
  // не знайдений у списку власних плащів, бо сітка тільки завантажується).
  let editorCapeTouched = $state(false)
  let editorFileInput: HTMLInputElement | undefined = $state()

  // Обрізані превʼю передніх панелей плащів (cape.id -> data URL).
  // БАГ (виправлено): раніше картка показувала ВЕСЬ атлас плаща через
  // CSS-відсотки background-position/size — це давало квадратну картку з
  // „кровотечею“ сусідніх пікселів. Тепер передню панель (зсув 1,2,
  // масштаб 11,32 у пікселях атласу 64x64) вирізаємо на canvas ПО
  // ПІКСЕЛЯХ і розтягуємо у пропорцію 9:16 — картка гарантовано 9:16.
  let capeThumbs = $state<Record<string, string>>({})

  // Вирізає передню панель плаща та повертає data URL розміру 9:16 (99x176),
  // розтягнутий на весь блок картки.
  // Параметри виділення (у пікселях вихідної текстури):
  //   зсув горизонтально: 1, зсув вертикально: 2
  //   масштаб горизонтально: 11, масштаб вертикально: 32
  // Зона плаща в атласі — ЗАВЖДИ одна (це UV-зона грані плаща з моделі):
  // відступ 1px зліва, 2px зверху, розмір 10x32. НІЧОГО не обрізаємо по
  // прозорості і не «розуміємо» малюнок — ця зона і є плащ у картинці
  // плаща. Текстура плаща розтягнута по вертикалі вдвічі (реальний плащ
  // на гравці 10x16), тому стискаємо зону 10x32 у полотно 10:16 (100x160)
  // — малюнок набуває правильних пропорцій і заповнює картку цілком.
  // Висота джерела клемпиться до реальних меж атласу (64x32), щоб рядки
  // за межами (вони прозорі) не додавали нічого зайвого.
  function cropCapeFront(dataUrl: string): Promise<string> {
    return new Promise((resolve, reject) => {
      const img = new Image()
      img.onload = () => {
        try {
          const srcX = 1
          const srcY = 2
          const srcW = 10
          const srcH = 15
          const ch = Math.max(1, Math.min(srcH, img.height - srcY))
          const c = document.createElement('canvas')
          c.width = 100   // 10 * 10
          c.height = 120  // 12 * 10
          const ctx = c.getContext('2d')
          if (!ctx) {
            reject(new Error('canvas unavailable'))
            return
          }
          ctx.imageSmoothingEnabled = false
          ctx.drawImage(img, srcX, srcY, srcW, ch, 0, 0, c.width, c.height)
          resolve(c.toDataURL('image/png'))
        } catch (e) {
          reject(e)
        }
      }
      img.onerror = () => reject(new Error('cape load failed'))
      img.src = dataUrl
    })
  }

  async function loadCapeThumbs(grid: CapeInfo[]) {
    // Скидаємо попередні превʼю одразу (БАГ: при швидкому перевідкритті
    // редактора старий асинхронний виклик міг перезаписати новіший).
    capeThumbs = {}
    const next: Record<string, string> = {}
    await Promise.all(grid.map(async (cape) => {
      try {
        const full = await App.GetWardrobeCapeDataURL(cape.url)
        next[cape.id] = await cropCapeFront(full)
      } catch {
        // Плащ не завантажився — картка лишиться порожньою (background-color).
      }
    }))
    capeThumbs = next
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

  // Завантажує пресети конкретного акаунта (персональні для кожного
  // ліцензійного акаунта). Виділяємо пресет, чий скін+плащ побайтово
  // збігається з тим, що зараз реально стоїть на акаунті на Mojang; якщо
  // збігу нема — вибір лишаємо порожнім (лівий переглядач покаже активний
  // вигляд акаунта, кнопка «Застосувати» буде вимкнена).
  async function loadPresets(accountID: string) {
    loading = true
    try {
      presets = await App.GetWardrobePresets(accountID)
      // Відновлюємо зв'язок «дефолтний скін → пресет» після перезавантаження,
      // щоб активна картка у секції «Усталені скіни» підсвічувалась (пресет
      // створюється з дефолтного скіна при кліку, див. pickDefaultSkin).
      const dmap: Record<string, string> = {}
      for (const d of DEFAULT_SKINS) {
        const m = presets.find((p) => p.name === d.name && p.slimArms === d.slim)
        if (m) dmap[d.name] = m.id
      }
      defaultPresetIds = dmap
      let matchedId = ''
      if (accountID) {
        try {
          matchedId = await App.GetWardrobeActivePresetID(accountID)
        } catch {
          // Немає мережі/акаунта — тихо падаємо назад на порожній вибір.
        }
      }
      if (matchedId && presets.some((p) => p.id === matchedId)) {
        selectedPresetId = matchedId
      } else {
        selectedPresetId = null
      }
      activePresetId = matchedId || null
    } catch (e) {
      toast(String(e), 'error')
    } finally {
      loading = false
    }
  }

  // Перезавантажуємо гардероб при зміні акаунта (у тому числі при першому
  // завантаженні, коли activeAccount ще пустий). Пресети іншого акаунта на
  // цей час зникають зі списку.
  $effect(() => {
    selectedPresetId = null
    loadPresets(activeAccount?.id ?? '')
  })

  function selectedPreset(): SkinPreset | undefined {
    return presets.find((p) => p.id === selectedPresetId)
  }

  function selectPreset(id: string) {
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
      // Видалення пресету — це не «застосувати до акаунта»: видалений скін
      // досі стоїть на Mojang, тож не стрибаємо на інший пресет, а просто
      // знімаємо виділення (переглядач покаже активний вигляд акаунта).
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
      // БАГ (виправлено): біндинг чекає (accountID, presetID), а раніше
      // передавався лише p.id → presetID приходив порожнім і застосування
      // завжди падало з "preset not found".
      await App.ApplyWardrobePreset(activeAccount?.id ?? '', p.id)
      // Пресет тепер стоїть на акаунті — кнопка «Застосувати» зникає
      // (незастосованих змін більше немає).
      activePresetId = p.id
      toast(t('wardrobe.applied'), 'success')
    } catch (e) {
      toast(t('wardrobe.applyError') + ': ' + String(e), 'error')
    } finally {
      applying = false
    }
  }

  // ── Дефолтні скіни («Усталені скіни») ──

  // Конвертує вшитий PNG-ассет у base64 без префікса data:. Vite інлайнить
  // маленькі PNG (ці скіни 64x64) як data: URL — тоді просто зрізаємо префікс
  // без fetch-обходу (надійніше у продакшн-режимі Wails).
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

  // Клік по дефолтному скіну (Steve/Alex/...): створює пресет з вшитого
  // ассета (store.Save дедуплікує за вмістом — повторні кліки перевикорис-
  // товують той самий пресет), вибирає його і одразу застосовує на акаунт,
  // як це робив старий лаунчер.
  async function pickDefaultSkin(d: (typeof DEFAULT_SKINS)[number]) {
    if (defaultBusy || !activeAccount?.id) return
    defaultBusy = d.name
    try {
      const base64 = await assetToBase64(d.tex)
      const preset = await App.SaveWardrobeLocalSkin(activeAccount.id, d.name, base64, d.slim)
      presets = [preset, ...presets.filter((p) => p.id !== preset.id)]
      defaultPresetIds[d.name] = preset.id
      selectedPresetId = preset.id
      await App.ApplyWardrobePreset(activeAccount.id, preset.id)
      activePresetId = preset.id
      toast(t('wardrobe.applied'), 'success')
    } catch (e) {
      toast(t('wardrobe.applyError') + ': ' + String(e), 'error')
    } finally {
      defaultBusy = ''
    }
  }

  // ── "Скопіювати скін гравця" ──

  function openCopyModal() {
    copyModalOpen = true
    copyNick = ''
    copyStatus = 'idle'
    copyResult = null
    copyHeadUrl = ''
    copyIncludeCape = true
  }

  function onCopyNickInput() {
    clearTimeout(copyDebounceTimer)
    copyResult = null
    copyHeadUrl = ''
    const nick = copyNick.trim()
    if (nick.length < 3) {
      copyStatus = 'idle'
      return
    }
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

  // ── Редактор пресету ──

  async function openEditor(p: SkinPreset | null) {
    editorPreset = p
    editorName = p?.name ?? ''
    editorSlim = p?.slimArms ?? false
    editorSelectedCapeId = ''
    // Показуємо поточний плащ пресету на 3D одразу; null = без плаща.
    editorPreviewCapeUrl = p?.capeUrl ?? null
    editorCapeTouched = false
    editorOpen = true
    try {
      editorCapeGrid = await App.GetWardrobeOwnedCapes(activeAccount?.id ?? '')
    } catch {
      editorCapeGrid = []
    }
    loadCapeThumbs(editorCapeGrid)
    if (p?.capeUrl) {
      const match = editorCapeGrid.find((c) => c.url === p.capeUrl)
      if (match) editorSelectedCapeId = match.id
    }
  }

  function pickEditorFile() {
    editorFileInput?.click()
  }

  function onEditorFileChange(ev: Event) {
    const input = ev.target as HTMLInputElement
    const file = input.files?.[0]
    if (!file) return
    const reader = new FileReader()
    reader.onload = async () => {
      const dataUrl = reader.result as string
      const base64 = dataUrl.split(',')[1] ?? ''
      try {
        const preset = await App.SaveWardrobeLocalSkin(activeAccount?.id ?? '', editorName || file.name.replace(/\.png$/i, ''), base64, editorSlim)
        presets = [preset, ...presets.filter((p) => p.id !== preset.id)]
        selectedPresetId = preset.id
        editorOpen = false
        toast(t('wardrobe.editor.saved'), 'success')
      } catch (e) {
        toast(String(e), 'error')
      }
    }
    reader.readAsDataURL(file)
  }

  // Кнопка "Редагувати скін": якщо поточний вигляд акаунта (скін+плащ на
  // Mojang) не має збереженого пресету — пропонуємо спершу створити пресет
  // з нього, бо редагувати "неіснуючий" пресет неможливо (так робив старий
  // лаунчер, завантажуючи скін з акаунта в редактор).
  async function onEditSkinClick() {
    const p = selectedPreset()
    if (p) {
      openEditor(p)
      return
    }
    if (!activeAccount?.isLicensed) {
      openEditor(null)
      return
    }
    const ok = await confirmDialog(
      t('wardrobe.createFromActiveTitle'),
      t('wardrobe.createFromActiveMsg'),
      {
        confirmLabel: t('wardrobe.createFromActiveBtn'),
        cancelLabel: t('wardrobe.editor.cancel'),
      },
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
    // Доки користувач не обрав плащ/його відсутність — не чіпаємо плащ на
    // акаунті (щоб не зняти плащ пресету лише тому, що список плащів ще
    // не встиг довантажитися).
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
    // Якщо редактор відкрито без пресету (кнопка "+" / нічого не обрано) —
    // зберігати нічого: ні плащ на акаунті не чіпаємо, ні "збережено" не
    // показуємо (інакше це б мовчки змінило плащ акаунта без створення
    // пресету). Така модалка закривається лише через вибір файлу.
    const p = editorPreset
    if (!p?.id) {
      editorOpen = false
      return
    }
    // Застосувати плащ на акаунт (якщо користувач свідомо обрав його).
    await saveEditorCape()
    // Зберегти зміни пресету (назва, тип рук, плащ) — без цього картка
    // пресету не оновлювалась би після редагування (БАГ: плащ у картці
    // лишався старим). Плащ чіпаємо лише якщо користувач його обрав;
    // інакше зберігаємо поточний плащ пресету (він міг не збігатися зі
    // списком власних плащів, поки сітка ще вантажилась).
    const accId = activeAccount?.id ?? ''
    const capeUrl = editorCapeTouched
      ? (editorSelectedCapeId === '' ? '' : (editorCapeGrid.find((c) => c.id === editorSelectedCapeId)?.url ?? ''))
      : (p.capeUrl ?? '')
    try {
        const updated = await App.UpdateWardrobePreset(accId, p.id, editorName || p.name, editorSlim, capeUrl)
        // Підміняємо пресет у списку: сигнатура (skinPath|capePath)
        // змінилась → кеш текстури картки інвалідується і картка
        // перерендерюється з новим плащем/назвою одразу.
        //
        // БАГ (крайовий випадок): якщо новий вигляд пресету (скін+плащ)
        // побайтово збігся з ІНШИМ пресетом, store.Save повертає той
        // існуючий пресет з чужим id. Тоді відредаговану картку треба
        // прибрати зі списку, а картку-двійник оновити — інакше у сітці
        // лишились би дві картки з однаковим id (ключ {#each} колізія).
        presets = [updated, ...presets.filter((x) => x.id !== p.id && x.id !== updated.id)]
      // Якщо користувач редагував саме вибраний пресет — слідуємо за ним
      // (при дедуплікації id міг змінитись на двійник). Інакше виділення
      // не чіпаємо, щоб редагування іншого пресету не перестрибувало вибір.
      if (selectedPresetId === p.id) selectedPresetId = updated.id
      // Редагували пресет, який зараз вибраний (тобто фактично застосований
      // на акаунт), і змінився сам вигляд (тип рук / плащ) — оновлюємо скін
      // і на Mojang, щоб акаунт одразу відобразив зміни (БАГ: раніше
      // зберігалося лише локально, на акаунті лишався старий скін).
      const lookChanged = editorSlim !== p.slimArms || capeUrl !== (p.capeUrl ?? '')
      if (selectedPresetId === updated.id && lookChanged) {
        // Якщо застосування на Mojang впало (токен тощо) — показуємо помилку
        // і НЕ закриваємо редактор з "збережено": локальний пресет змінився,
        // але на акаунті — ні, тож користувач має бачити, що щось не так.
        try {
          await App.ApplyWardrobePreset(accId, updated.id)
        } catch (e) {
          toast(String(e), 'error')
          editorOpen = false
          return
        }
      }
    } catch (e) {
        toast(String(e), 'error')
        editorOpen = false
        return
      }
    editorOpen = false
    toast(t('wardrobe.editor.saved'), 'success')
  }

  async function deleteFromEditor() {
    if (!editorPreset) return
    const p = editorPreset
    editorOpen = false
    await deletePreset(p, new Event('click'))
  }
</script>

<div class="wr-actions">
  <button class="btn btn-ghost" onclick={openCopyModal}>
    <i class="ti ti-user-search"></i> {t('wardrobe.copySkin')}
  </button>
</div>

<div class="wr-layout">
  <!-- ══ ЛІВА ПАНЕЛЬ: 3D-переглядач ══ -->
  <div class="wr-left">
    <!-- #wrViewer: real 3D via three.js (wardrobe3d.ts → createWardrobeViewer).
         Canvas монтується в .wr-3d-canvas програмно і накладається прозорим
         поверх CSS-градієнта .wr-viewer. Модель skin-model-launcher(-slim).glb
         перезавантажується через handle.setSlim(preset.slimArms), текстура —
         PNG з preset.skinPath через App.GetWardrobeTextureDataURL →
         handle.setTexture (NearestFilter, без розмиття піксель-арту). Кліпи
         мапляться через ANIM_MAP у wardrobe3d.ts (idle/sit→idle1/look→walk).
         Рендер-цикл зупиняється в onDestroy (dispose → без WebGL-витоків). -->
    <div class="wr-viewer" id="wrViewer">
      <div class="wr-3d-canvas" bind:this={viewerContainer}></div>
      <div class="wr-viewer-hint"><i class="ti ti-hand-move"></i> {t('wardrobe.dragHint')}</div>
    </div>

    <div class="wr-info">
      <!-- Назва вибраного пресету та UUID гравця НЕ показуються (ТЗ): внизу
           переглядача є лише ім'я акаунта + кнопка дії, якщо є зміни. -->
      <div class="nick">{activeAccount?.username || 'Steve'}</div>
    </div>
    {#if selectedPreset() && selectedPresetId !== activePresetId}
      <div class="wr-left-foot">
        <button class="btn btn-primary" style="width:100%; justify-content:center;" disabled={applying} onclick={applyToAccount}>
          <i class="ti {applying ? 'ti-loader-2 spin' : 'ti-device-floppy'}"></i>
          {applying ? t('wardrobe.applying') : t('wardrobe.applyBtn')}
        </button>
      </div>
    {/if}

    <div class="wr-left-foot">
      <button class="btn btn-primary" style="width:100%; justify-content:center;" onclick={onEditSkinClick}>
        <i class="ti ti-edit"></i> {t('wardrobe.editSkin')}
      </button>
    </div>

    <div class="wr-anim-toggle">
      <div class="wr-anim-btn {anim === 'idle' ? 'active' : ''}" onclick={() => (anim = 'idle')}>{t('wardrobe.animIdle')}</div>
      <div class="wr-anim-btn {anim === 'sit' ? 'active' : ''}" onclick={() => (anim = 'sit')}>{t('wardrobe.animSit')}</div>
      <div class="wr-anim-btn {anim === 'look' ? 'active' : ''}" onclick={() => (anim = 'look')}>{t('wardrobe.animLook')}</div>
    </div>
  </div>

  <!-- ══ ПРАВА ПАНЕЛЬ: галерея (кожна секція має власний внутрішній скрол) ══ -->
  <div class="wr-right">
    <div class="wr-gallery">
      <div class="wr-section {savedCollapsed ? 'collapsed' : ''}">
        <div class="wr-section-head" onclick={() => (savedCollapsed = !savedCollapsed)}>
          <i class="ti ti-chevron-up chev"></i>
          <span class="wr-section-title">{t('wardrobe.savedSkins')}</span>
          <span class="wr-section-count">{presets.length}</span>
        </div>
        <div class="wr-section-body">
          <div class="wr-tile-grid">
            <div class="wr-add-card" onclick={() => openEditor(null)}>
              <i class="ti ti-plus plus"></i>
              <div class="t">{t('wardrobe.addSkin')}</div>
              <div class="h">{t('wardrobe.addSkinHint')}</div>
            </div>

            {#each presets as p (p.id)}
              <div class="wr-card {selectedPresetId === p.id ? 'active' : ''} {p.capePath ? 'has-cape' : ''}" onclick={() => selectPreset(p.id)}>
                <div class="wr-card-active-badge"><i class="ti ti-check"></i></div>

                <!-- Назва пресету зверху по центру + тип під нею — з'являються лише при наведенні. -->
                <div class="wr-card-hover-name">
                  <div class="hn-title">{p.name}</div>
                  <div class="hn-meta">
                    {p.slimArms ? t('wardrobe.slim') : t('wardrobe.classic')}
                    {#if p.capePath}· <i class="ti ti-shirt" style="font-size:10px"></i> {t('wardrobe.withCape')}{/if}
                  </div>
                </div>

                <div class="wr-card-preview">
                  <MiniSkin3D slim={p.slimArms} textureUrl={presetTextures[p.id]} capeTextureUrl={presetCapeTextures[p.id]} />
                </div>

                <!-- Знизу при наведенні: "Редагувати" (іконка+текст) + кнопка видалення (лише іконка, червона). -->
                <div class="wr-card-hover-actions">
                  <button class="wr-card-edit-btn" onclick={(e) => { e.stopPropagation(); openEditor(p) }}>
                    <i class="ti ti-edit"></i> {t('wardrobe.editor.editShort')}
                  </button>
                  <button class="wr-card-del-btn" onclick={(e) => deletePreset(p, e)} title={t('wardrobe.editor.delete')}>
                    <i class="ti ti-trash"></i>
                  </button>
                </div>
              </div>
            {/each}

            {#if !loading && presets.length === 0}
              <div class="wr-empty-hint">{t('wardrobe.emptyState')}</div>
            {/if}
          </div>
        </div>
      </div>

      <div class="wr-section {defaultsCollapsed ? 'collapsed' : ''}">
        <div class="wr-section-head" onclick={() => (defaultsCollapsed = !defaultsCollapsed)}>
          <i class="ti ti-chevron-up chev"></i>
          <span class="wr-section-title">{t('wardrobe.defaultSkins')}</span>
          <span class="wr-section-count">{DEFAULT_SKINS.length}</span>
        </div>
        <div class="wr-section-body">
          <div class="wr-tile-grid">
            {#each DEFAULT_SKINS as d}
              <div
                class="wr-card {selectedPresetId === defaultPresetIds[d.name] ? 'active' : ''} {defaultBusy === d.name ? 'busy' : ''}"
                onclick={() => pickDefaultSkin(d)}
              >
                <div class="wr-card-active-badge"><i class="ti ti-check"></i></div>
                <div class="wr-card-preview">
                  <MiniSkin3D slim={d.slim} textureUrl={d.tex} />
                </div>
                <div class="wr-card-body">
                  <div class="wr-card-name">{d.name}</div>
                  <div class="wr-card-meta">{d.slim ? t('wardrobe.slim') : t('wardrobe.classic')}</div>
                </div>
              </div>
            {/each}
          </div>
        </div>
      </div>
    </div>
  </div>
</div>

<!-- ══ МОДАЛКА: Скопіювати скін гравця ══ -->
{#if copyModalOpen}
  <div class="modal-overlay" onclick={(e) => { if (e.target === e.currentTarget) copyModalOpen = false }}>
    <div class="modal">
      <div class="modal-head">
        <i class="ti ti-user-search" style="font-size:20px; color:var(--violet-l)"></i>
        <h3>{t('wardrobe.copy.title')}</h3>
        <button class="icon-btn" onclick={() => (copyModalOpen = false)}><i class="ti ti-x"></i></button>
      </div>
      <div class="modal-body">
        <p style="font-size:12px; color:var(--text-mute); margin-bottom:14px; line-height:1.5;">{t('wardrobe.copy.desc')}</p>

        <div class="wr-copy-input-row">
          <i class="ti ti-user li"></i>
          <input
            type="text"
            bind:value={copyNick}
            placeholder={t('wardrobe.copy.placeholder')}
            maxlength="16"
            oninput={onCopyNickInput}
            onkeydown={(e) => { if (e.key === 'Enter') runCopyLookup() }}
          />
        </div>

        <div class="wr-copy-status {copyStatus === 'notfound' || copyStatus === 'nosk' || copyStatus === 'error' ? 'err' : ''} {copyStatus === 'found' ? 'ok' : ''}">
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

        {#if copyResult}
          <div class="wr-copy-result">
            <!-- Зліва: 3D-переглядач скіна знайденого гравця. Кнопка/тумблер
                 "Копіювати плащ" (справа) вмикає/вимикає показ плаща тут же. -->
            <div class="wr-copy-3d" bind:this={copyViewerContainer}></div>

            <div class="wr-copy-info">
              <div class="wr-copy-preview">
                <div class="avatar" style={copyHeadUrl ? `background-image:url('${copyHeadUrl}')` : ''}>
                  {#if !copyHeadUrl}<i class="ti ti-user"></i>{/if}
                </div>
                <div class="info">
                  <div class="nick">{copyResult.nickname}</div>
                  <div class="sub">{copyResult.slimArms ? t('wardrobe.slim') : t('wardrobe.classic')}</div>
                </div>
              </div>

              <div class="wr-copy-cape-toggle">
                <div>
                  <div class="l">{t('wardrobe.copy.withCapeLabel')}</div>
                  <div class="s">{t('wardrobe.copy.withCapeHint')}</div>
                </div>
                <div class="toggle {copyIncludeCape ? 'on' : ''} {!copyResult.capeUrl ? 'disabled' : ''}"
                     onclick={() => { if (copyResult?.capeUrl) copyIncludeCape = !copyIncludeCape }}></div>
              </div>
            </div>
          </div>
        {/if}
      </div>
      <div class="modal-foot">
        <button class="btn btn-ghost" onclick={() => (copyModalOpen = false)}>{t('wardrobe.editor.cancel')}</button>
        <button class="btn btn-primary" disabled={!copyResult || copyBusy} onclick={createPresetFromCopy}>
          <i class="ti {copyBusy ? 'ti-loader-2 spin' : 'ti-plus'}"></i> {t('wardrobe.copy.createBtn')}
        </button>
      </div>
    </div>
  </div>
{/if}

<!-- ══ МОДАЛКА: Редактор пресету ══ -->
{#if editorOpen}
  <div class="modal-overlay" onclick={(e) => { if (e.target === e.currentTarget) editorOpen = false }}>
    <div class="modal lg">
      <div class="modal-head">
        <i class="ti ti-edit text-orange" style="font-size:20px"></i>
        <h3>{t('wardrobe.editor.title')}</h3>
        <button class="icon-btn" onclick={() => (editorOpen = false)}><i class="ti ti-x"></i></button>
      </div>
      <div class="modal-body">
        <div class="wr-editor-body">
          <div class="wr-editor-preview">
            <!-- Живий 3D-переглядач редактора: drag-to-rotate + idle.
                 Монтується через createWardrobeViewer (wardrobe3d.ts),
                 текстура — поточний пресет або Steve для нового скіна. -->
            <div class="wr-editor-3d" bind:this={editorViewerContainer}></div>
          </div>

          <div class="wr-editor-fields">
            <div>
              <label class="wr-field-label">{t('wardrobe.editor.nameLabel')}</label>
              <div class="wr-copy-input-row" style="position:static;">
                <input type="text" bind:value={editorName} style="padding:11px 14px;" />
              </div>
            </div>

            <div>
              <label class="wr-field-label">{t('wardrobe.editor.fileLabel')}</label>
              <div class="wr-dropzone" onclick={pickEditorFile}>
                <i class="ti ti-upload"></i>
                <div class="t">{t('wardrobe.editor.dropHint')}</div>
                <div class="h">{t('wardrobe.editor.dropSpec')}</div>
                <input type="file" bind:this={editorFileInput} accept=".png" style="display:none;" onchange={onEditorFileChange} />
              </div>
            </div>

            <!-- ══ Секція «Текстура»: перемикачі типу скіна + список плащів ══ -->
            <div class="wr-texture-section">
              <label class="wr-field-label">{t('wardrobe.editor.textureLabel')}</label>

              <div class="wr-seg-row seg">
                <div class="sb {!editorSlim ? 'active' : ''}" onclick={() => (editorSlim = false)}>{t('wardrobe.editor.armClassic')}</div>
                <div class="sb {editorSlim ? 'active' : ''}" onclick={() => (editorSlim = true)}>{t('wardrobe.editor.armSlim')}</div>
              </div>

              <div class="wr-cape-grid">
                <!-- Вибір "без плаща": окрім id також скидаємо превʼю на 3D
                     (editorPreviewCapeUrl = null), бо ефект плаща більше не
                     залежить від пресету — інакше плащ би лишався на моделі. -->
                <div class="wr-cape-card none {editorSelectedCapeId === '' ? 'sel' : ''}" onclick={() => { editorSelectedCapeId = ''; editorPreviewCapeUrl = null; editorCapeTouched = true }}>
                  <div class="cc-swatch"><i class="ti ti-ban"></i></div>
                  <div class="cc-check"><i class="ti ti-check"></i></div>
                </div>
                {#each editorCapeGrid as cape (cape.id)}
                  <div class="wr-cape-card {editorSelectedCapeId === cape.id ? 'sel' : ''}" onclick={() => { editorSelectedCapeId = cape.id; editorPreviewCapeUrl = cape.url; editorCapeTouched = true }}>
                    <!-- Поки canvas-обрізка вантажиться — показуємо лише фон
                         (БЕЗ сирого атласу: він би миготів розтягнутим цілим
                         атласом замість передньої панелі 9:16). -->
                    <div class="cc-swatch" style="background-image:url('{capeThumbs[cape.id]}');"></div>
                    <div class="cc-check"><i class="ti ti-check"></i></div>
                  </div>
                {/each}
                {#if editorCapeGrid.length === 0}
                  <div class="wr-cape-empty">{t('wardrobe.editor.capeLocked')}</div>
                {/if}
              </div>
            </div>
          </div>
        </div>
      </div>
      <div class="modal-foot">
        {#if editorPreset}
          <button class="btn btn-ghost" style="margin-right:auto; color:var(--red);" onclick={deleteFromEditor}>
            <i class="ti ti-trash"></i> {t('wardrobe.editor.delete')}
          </button>
        {/if}
        <button class="btn btn-ghost" onclick={() => (editorOpen = false)}>{t('wardrobe.editor.cancel')}</button>
        <button class="btn btn-primary" onclick={saveEditor}>
          <i class="ti ti-device-floppy"></i> {t('wardrobe.editor.save')}
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  /* ══════════════════ WARDROBE (ГАРДЕРОБ) ══════════════════
     Макет 1:1 зі старого JavaFX WardrobeScreen.java: ліва 3D-панель +
     права галерея з двома акордеон-секціями. Кожна .wr-section-body
     скролиться незалежно — головний скрол сторінки лаунчера тут не
     задіяний (page batch scroll лишається на .content-контейнері
     батьківського App.svelte і НЕ рухається разом з галереєю).
     ════════════════════════════════════════════════════════ */
  .wr-layout { display:flex; gap:0; flex:1; min-height:0; border:1px solid var(--border); border-radius:var(--r-lg); overflow:hidden; background:rgba(20,20,20,.5); }

  .wr-left { width:300px; min-width:260px; max-width:340px; border-right:1px solid var(--border); display:flex; flex-direction:column; background:rgba(15,15,15,.4); }
  .wr-viewer { flex:1; min-height:280px; position:relative; display:flex; align-items:center; justify-content:center; cursor:grab; overflow:hidden;
    background:radial-gradient(ellipse at 50% 30%, rgba(255,138,0,.05), transparent 65%); }
  .wr-viewer:active { cursor:grabbing; }
  .wr-3d-canvas { position:absolute; inset:0; cursor:grab; }
  .wr-3d-canvas:active { cursor:grabbing; }
  .wr-viewer-hint { position:absolute; bottom:10px; left:0; right:0; display:flex; align-items:center; justify-content:center; gap:6px; font-size:10.5px; color:var(--text-dim); pointer-events:none; }

  .wr-info { padding:14px 18px 6px; text-align:center; border-top:1px solid var(--border); }
  .wr-info .nick { font-family:var(--font-mono); font-size:13.5px; font-weight:700; }
  .wr-info .uuid { font-family:var(--font-mono); font-size:9.5px; color:var(--text-dim); margin-top:3px; word-break:break-all; }

  .wr-anim-toggle { display:flex; gap:6px; padding:12px 16px; justify-content:center; }
  .wr-anim-btn { flex:1; padding:7px 0; text-align:center; font-size:10.5px; font-weight:700; border-radius:var(--r-sm); background:rgba(255,255,255,.04); border:1px solid var(--border); color:var(--text-mute); cursor:pointer; transition:.15s; }
  .wr-anim-btn:hover { color:#fff; border-color:var(--border-hi); }
  .wr-anim-btn.active { background:rgba(255,138,0,.14); border-color:rgba(255,138,0,.4); color:var(--orange); }

  .wr-left-foot { padding:6px 16px 16px; display:flex; flex-direction:column; gap:8px; }

  .wr-right { flex:1; min-width:0; display:flex; flex-direction:column; min-height:0; }
  .wr-gallery { flex:1; min-height:0; display:flex; flex-direction:column; overflow:hidden; }

  .wr-section { border-bottom:1px solid var(--border); display:flex; flex-direction:column; min-height:0; }
  .wr-section:last-child { border-bottom:none; flex:1; }
  .wr-section:not(.collapsed) { flex:1; min-height:0; }
  .wr-section.collapsed { flex:0 0 auto; }
  .wr-section-head { flex:0 0 auto; display:flex; align-items:center; gap:8px; padding:13px 20px; cursor:pointer; user-select:none; }
  .wr-section-head:hover { background:rgba(255,255,255,.02); }
  .wr-section-head i.chev { font-size:15px; color:var(--text-mute); transition:transform .18s; }
  .wr-section.collapsed .chev { transform:rotate(-90deg); }
  .wr-actions { display:flex; gap:8px; justify-content:flex-end; margin-bottom:16px; }
  .wr-section-title { font-size:12.5px; font-weight:700; }
  .wr-section-count { margin-left:4px; font-size:10.5px; color:var(--text-mute); font-weight:600; }
  .wr-section-body { padding:6px 20px 18px; overflow-y:auto; min-height:0; }
  .wr-section:not(.collapsed) .wr-section-body { flex:1; }
  .wr-section.collapsed .wr-section-body { display:none; }
  /* Кастомний скрол замість ванільного браузерного (як .content у style.css). */
  .wr-section-body::-webkit-scrollbar { width:6px; }
  .wr-section-body::-webkit-scrollbar-thumb { background:var(--hover-strong); border-radius:3px; }
  .wr-section-body::-webkit-scrollbar-track { background:transparent; }

  .wr-tile-grid { display:grid; grid-template-columns:repeat(auto-fill, minmax(150px,1fr)); gap:12px; }
  .wr-empty-hint { grid-column:1 / -1; text-align:center; padding:30px 10px; font-size:12px; color:var(--text-mute); }

  /* Картка пресету: тільки модель на картці + галочка вибору. Назва/тип
     і кнопки з'являються поверх зображення лише при наведенні. */
  .wr-card-preview { height:214px; display:flex; align-items:center; justify-content:center; background:linear-gradient(160deg,#202020,#131313); position:relative; overflow:hidden; }

  .wr-card-active-badge { position:absolute; top:8px; left:8px; width:20px; height:20px; border-radius:50%; background:var(--orange); color:#fff; display:none; align-items:center; justify-content:center; font-size:11px; z-index:3; }
  .wr-card.active .wr-card-active-badge { display:flex; }

  .wr-card { position:relative; border-radius:var(--r-md); border:1px solid var(--border); background:var(--bg-card); overflow:hidden; cursor:pointer; transition:border-color .15s, transform .15s; min-height:214px; }
  .wr-card:hover { border-color:var(--border-hi); transform:translateY(-2px); }
  .wr-card.active { border-color:var(--orange); box-shadow:0 0 0 2px rgba(255,138,0,.22); }
  .wr-card.busy { opacity:.6; pointer-events:none; }

  /* Назва пресету + тип моделі — накладка зверху по центру, лише при hover. */
  .wr-card-hover-name { position:absolute; top:0; left:0; right:0; padding:10px 30px 16px; text-align:center;
    background:linear-gradient(to bottom, rgba(0,0,0,.65), transparent); opacity:0; transition:opacity .15s; z-index:2; pointer-events:none; }
  .wr-card:hover .wr-card-hover-name { opacity:1; }
  .wr-card-hover-name .hn-title { font-size:11.5px; font-weight:700; color:#fff; white-space:nowrap; overflow:hidden; text-overflow:ellipsis; }
  .wr-card-hover-name .hn-meta { font-size:9.5px; color:rgba(255,255,255,.7); margin-top:2px; display:flex; align-items:center; justify-content:center; gap:5px; }

  /* Кнопки знизу при hover: "Редагувати" (іконка+текст) + іконка видалення (червона). */
  .wr-card-hover-actions { position:absolute; bottom:0; left:0; right:0; padding:8px; display:flex; gap:6px; align-items:center;
    background:linear-gradient(to top, rgba(0,0,0,.7), transparent); opacity:0; transition:opacity .15s; z-index:3; }
  .wr-card:hover .wr-card-hover-actions { opacity:1; }
  .wr-card-edit-btn { flex:1; display:flex; align-items:center; justify-content:center; gap:5px; padding:7px 0; border-radius:var(--r-sm);
    background:rgba(255,255,255,.12); backdrop-filter:blur(4px); border:1px solid rgba(255,255,255,.18); color:#fff; font-size:10.5px; font-weight:700; cursor:pointer; transition:.15s; }
  .wr-card-edit-btn:hover { background:rgba(255,255,255,.2); }
  .wr-card-del-btn { width:30px; height:30px; flex-shrink:0; display:flex; align-items:center; justify-content:center; border-radius:var(--r-sm);
    background:rgba(239,68,68,.16); backdrop-filter:blur(4px); border:1px solid rgba(239,68,68,.3); color:var(--red); font-size:13px; cursor:pointer; transition:.15s; }
  .wr-card-del-btn:hover { background:rgba(239,68,68,.28); }

  .wr-add-card { border:1.5px dashed var(--border-hi); border-radius:var(--r-md); display:flex; flex-direction:column; align-items:center; justify-content:center; gap:8px; min-height:214px; cursor:pointer; color:var(--text-mute); transition:.15s; text-align:center; padding:10px; }
  .wr-add-card:hover { border-color:var(--orange); color:var(--orange); background:rgba(255,138,0,.04); }
  .wr-add-card .plus { font-size:26px; }
  .wr-add-card .t { font-size:11.5px; font-weight:700; }
  .wr-add-card .h { font-size:9.5px; color:var(--text-dim); }

  .wr-field-label { font-size:11px; color:var(--text-mute); font-weight:600; display:block; margin-bottom:6px; }

  .wr-copy-input-row { position:relative; }
  .wr-copy-input-row input { width:100%; background:var(--bg-input); border:1px solid var(--border-hi); border-radius:var(--r-md); padding:11px 14px 11px 38px; font-size:13px; color:#fff; }
  .wr-copy-input-row input:focus { outline:none; border-color:var(--violet); }
  .wr-copy-input-row i.li { position:absolute; left:13px; top:50%; transform:translateY(-50%); color:var(--text-mute); font-size:15px; }

  /* ── Результат пошуку гравця: 3D-переглядач зліва + інформація справа ── */
  .wr-copy-result { display:flex; gap:14px; margin-top:14px; }
  .wr-copy-3d { width:130px; height:170px; flex-shrink:0; position:relative; border-radius:var(--r-md); overflow:hidden; cursor:grab;
    background:radial-gradient(ellipse at 50% 30%, rgba(123,77,255,.06), transparent 65%); border:1px solid var(--border); }
  .wr-copy-3d:active { cursor:grabbing; }
  .wr-copy-info { flex:1; min-width:0; display:flex; flex-direction:column; justify-content:center; gap:10px; }

  .wr-copy-preview { display:flex; gap:14px; align-items:center; padding:14px; border-radius:var(--r-md); background:rgba(255,255,255,.03); border:1px solid var(--border); }
  .wr-copy-preview .avatar { width:52px; height:52px; border-radius:var(--r-sm); background:linear-gradient(160deg,#2c8fc7,#1a3a5a); display:flex; align-items:center; justify-content:center; font-size:22px; color:#fff; flex-shrink:0; background-size:cover; background-position:center; }
  .wr-copy-preview .info { flex:1; min-width:0; }
  .wr-copy-preview .nick { font-size:13px; font-weight:700; }
  .wr-copy-preview .sub { font-size:10.5px; color:var(--text-mute); margin-top:2px; }
  .wr-copy-status { font-size:11px; color:var(--text-mute); margin-top:10px; display:flex; align-items:center; gap:6px; min-height:16px; }
  .wr-copy-status.err { color:var(--red); }
  .wr-copy-status.ok { color:var(--green); }
  .wr-copy-status i { font-size:13px; }
  .wr-copy-cape-toggle { display:flex; align-items:center; justify-content:space-between; padding:12px 0 0; }
  .wr-copy-cape-toggle .l { font-size:12px; font-weight:600; }
  .wr-copy-cape-toggle .s { font-size:10.5px; color:var(--text-mute); margin-top:1px; }
  /* Немає плаща на акаунті гравця — тумблер неактивний (нічого копіювати). */
  .toggle.disabled { opacity:.4; cursor:not-allowed; pointer-events:none; }

  /* Редактор пресету: модалка ФІКСОВАНОЇ висоти — скролить ТІЛЬКИ сітка
     плащів, а не все модальне вікно. БАГ (виправлено): раніше .modal-body
     (глобальний overflow-y:auto) крутив весь редактор, бо 3D-превʼю мало
     жорстку висоту 320px і контент не влазив. Тепер модалка не скролиться,
     .wr-editor-body заповнює її висоту (flex:1), а сітка плащів — єдиний
     скролящий регіон (flex:1 + min-height:0 + overflow-y:auto). */
  .modal.lg > .modal-body { overflow:hidden; display:flex; flex-direction:column; }
  .wr-editor-body { display:flex; gap:20px; flex:1; min-height:0; }
  .wr-editor-preview { width:210px; min-width:210px; flex-shrink:0; display:flex; flex-direction:column; align-items:center; justify-content:center; gap:10px; min-height:0; }
  /* 3D-превʼю редактора: займає всю доступну висоту лівої колонки, модель
     центрується по вертикалі (fitModel/frameCamera у wardrobe3d.ts).
     БАГ (виправлено): раніше жорстка висота 320px зсувала модель уверх у
     високій модалці й змушувала скролитись усе вікно. */
  .wr-editor-3d { position:relative; width:210px; flex:1; min-height:0; align-self:stretch; border-radius:var(--r-md); overflow:hidden; cursor:grab;
    display:flex; align-items:center; justify-content:center;
    background:radial-gradient(ellipse at 50% 30%, rgba(255,138,0,.05), transparent 65%); }
  .wr-editor-3d:active { cursor:grabbing; }
  .wr-editor-fields { flex:1; min-width:0; min-height:0; display:flex; flex-direction:column; gap:14px; overflow:hidden; }
  /* Сегментовані кнопки типу рук (classic/slim) — той самий стиль, що й
     .wd-side .seg у style.css (глобальний варіант скоуплений до .wd-side,
     тому тут свій). Раніше .sb був голим текстом без фону/паддінгу/курсора. */
  .wr-seg-row { display:flex; gap:4px; padding:4px; background:var(--bg-input); border:1px solid var(--border); border-radius:var(--r-md); }
  .wr-seg-row .sb { flex:1; text-align:center; padding:7px 14px; font-size:11.5px; font-weight:700; border-radius:var(--r-sm); cursor:pointer; color:var(--text-mute); transition:background .15s, color .15s; user-select:none; }
  .wr-seg-row .sb:hover { color:var(--text); background:var(--hover); }
  .wr-seg-row .sb.active { background:var(--orange); color:#fff; }
  .wr-dropzone { border:1.5px dashed var(--border-hi); border-radius:var(--r-md); padding:20px; text-align:center; cursor:pointer; transition:.15s; }
  .wr-dropzone:hover { border-color:var(--orange); background:rgba(255,138,0,.04); }
  .wr-dropzone i { font-size:22px; color:var(--text-mute); margin-bottom:6px; display:block; }
  .wr-dropzone .t { font-size:11.5px; font-weight:700; }
  .wr-dropzone .h { font-size:10px; color:var(--text-dim); margin-top:2px; }

  .wr-texture-section { display:flex; flex-direction:column; gap:10px; flex:1; min-height:0; overflow:hidden; }

  .wr-cape-grid { display:grid; grid-template-columns:repeat(auto-fill, minmax(96px,1fr)); gap:12px; margin-top:8px;
    /* Сітка плащів — ЄДИНИЙ скролящий регіон модалки редактора.
       БАГ (виправлено): (1) раніше був max-height:320px, а сама модалка
       скролилась цілком — тепер flex:1 + min-height:0 + overflow-y:auto,
       тому крутиться тільки сітка. (2) align-content:start — рядки зберігають
       природну висоту 9:16, зайве йде у скрол. (3) картки збільшено 64->96px,
       щоб було видніше. */
    flex:1; min-height:0; overflow-y:auto; padding-right:4px; align-content:start; }
  /* Кастомний скрол замість ванільного браузерного, підтримує світлу тему
     (через var(--hover-strong), яка вже перевизначена в html[data-theme="light"]). */
  .wr-cape-grid::-webkit-scrollbar { width:6px; }
  .wr-cape-grid::-webkit-scrollbar-thumb { background:var(--hover-strong); border-radius:3px; }
  .wr-cape-grid::-webkit-scrollbar-track { background:transparent; }
  /* Картка плаща: ЗАВЖДИ вертикальна 10:16 (висота більша за ширину, як
     «кістка» плаща на моделі — реальний плащ 10x16, тож картка саме такої
     пропорції, не вища). Висоту задаємо padding-bottom у відсотках від
     ширини (160% = 16/10) — це працює у БУДЬ-ЯКОМУ браузері та WebView2, на
     відміну від aspect-ratio: у старіших рантаймах WebView2 він
     ігнорувався, і картка лишалась горизонтальною (ширина 16, висота 9)
     або взагалі без висоти (рядки сітки схлопувались — той самий баг).
     Свотч заповнює картку абсолютно (inset:0), тож малюнок плаща
     розтягується на всі грані картки. Передню панель вирізаємо на canvas
     ПО ПІКСЕЛЯХ (зона 10x32 стискається у 10:16, див. cropCapeFront), тому
     background просто заповнює картку цілком. */
  .wr-cape-card {
    position:relative; border-radius:var(--r-sm); border:1.5px solid var(--border);
    background:linear-gradient(160deg,#242424,#161616); cursor:pointer;
    padding:0 0 160% 0; overflow:hidden; transition:border-color .15s, transform .15s;
  }
  .wr-cape-card:hover { border-color:var(--border-hi); transform:translateY(-1px); }
  .wr-cape-card.sel { border-color:var(--violet); box-shadow:0 0 0 2px rgba(123,77,255,.25); }
  .wr-cape-card .cc-swatch {
    position:absolute; inset:0; width:100%; height:100%;
    background-color:#333;
    background-repeat:no-repeat;
    background-size: 100% 100%;
    background-position: center;
    image-rendering: pixelated;
  }
  .wr-cape-card .cc-name { display:none; } /* назви плащів навмисно не показуємо */
  .wr-cape-card .cc-check { position:absolute; top:5px; right:5px; width:16px; height:16px; border-radius:50%; background:var(--violet); color:#fff; display:none; align-items:center; justify-content:center; font-size:9px; z-index:2; }
  .wr-cape-card.sel .cc-check { display:flex; }
  .wr-cape-card.none .cc-swatch { display:flex; align-items:center; justify-content:center; background:rgba(255,255,255,.03); border:1.5px dashed var(--border-hi); box-shadow:none; background-image:none !important; }
  .wr-cape-card.none .cc-swatch i { font-size:18px; color:var(--text-dim); }
  .wr-cape-empty { grid-column:1 / -1; font-size:10.5px; color:var(--text-dim); padding:6px 2px; }

  /* Кнопка-іконка закриття модалки (БАГ: була ванільною — .icon-btn
     визначений лише у GameConsole, тут свій варіант у стилі лаунчера). */
  .icon-btn { width:32px; height:32px; border-radius:9px; border:none; background:var(--hover); color:var(--text-mute); cursor:pointer; display:flex; align-items:center; justify-content:center; transition:background .15s,color .15s; flex-shrink:0; font-size:15px; }
  .icon-btn:hover { background:var(--hover-strong); color:var(--text); }

  .spin { animation:wr-spin 1s linear infinite; }
  @keyframes wr-spin { from { transform:rotate(0deg); } to { transform:rotate(360deg); } }
</style>
