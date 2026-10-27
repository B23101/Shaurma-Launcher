<script lang="ts">
  // ── Фоторедактор скріншоту (повноекранний) ────────────────────────────
  // Малювання (пензлик/маркер), фігури (прямокутник/овал/лінія/стрілка),
  // обрізання з фіксацією пропорцій. Малювання ведеться векторним
  // стеком дій (не піксельною історією) — кожен рендер перемальовує
  // базове зображення + всі дії по порядку, тому Undo/Redo — це просто
  // зсув по стеку, без втрати якості при багаторазовому редагуванні.
  //
  // Збереження: canvas.toDataURL('image/png') → SaveEditedScreenshot
  // (перезапис оригіналу) або SaveEditedScreenshotAsCopy (новий файл),
  // або CopyEditedImageToClipboard (без збереження на диск).
  import { t } from './i18n.svelte'
  import { App } from './wails'
  import { toast, confirmDialog } from './toast.svelte'
  import type { ScreenshotEntry } from './types'

  let { packId, entry, onClose, onSaved } = $props<{
    packId: string
    entry: ScreenshotEntry
    onClose: () => void
    onSaved: () => void
  }>()

  type Tool = 'brush' | 'marker' | 'eraser' | 'rect' | 'ellipse' | 'arrow' | 'line' | 'crop'
  type Point = { x: number; y: number }

  // 'eraser' — окремий рід штриха (не 'brush'/'marker'): малюється тим
  // самим механізмом points[], але у drawAction рендериться через
  // globalCompositeOperation='destination-out', тобто прибирає намальоване
  // (і базове зображення) під штрихом, замість того щоб малювати кольором.
  type StrokeAction = { kind: 'stroke'; tool: 'brush' | 'marker' | 'eraser'; color: string; width: number; points: Point[] }
  type ShapeAction = { kind: 'shape'; tool: 'rect' | 'ellipse' | 'line' | 'arrow'; color: string; width: number; from: Point; to: Point }
  type Action = StrokeAction | ShapeAction

  let tool = $state<Tool>('brush')
  let color = $state('#ff3b30')
  let thickness = $state(4)
  let actions = $state<Action[]>([])
  let redoStack = $state<Action[]>([])
  let cropRect = $state<{ x: number; y: number; w: number; h: number } | null>(null)

  let canvasEl: HTMLCanvasElement
  let wrapEl: HTMLDivElement
  let baseImg = $state<HTMLImageElement | null>(null)
  let loading = $state(true)
  let loadError = $state('')
  let saving = $state(false)
  let dirty = $derived(actions.length > 0 || cropRect !== null)

  // Розмір канви = природний розмір зображення (щоб збереження було без
  // втрати якості — координати дій зберігаються у природних пікселях).
  let naturalW = $state(0), naturalH = $state(0)
  // Масштаб показу canvas на екрані (CSS-розмір / природний розмір) —
  // потрібен, щоб переводити координати миші (екранні) у природні пікселі
  // зображення для малювання.
  let displayScale = $state(1)

  async function loadImage() {
    loading = true
    loadError = ''
    try {
      const url = await App.GetScreenshotAsset(packId, entry.fileName)
      const img = new Image()
      await new Promise<void>((resolve, reject) => {
        img.onload = () => resolve()
        img.onerror = () => reject(new Error('decode error'))
        img.src = url
      })
      baseImg = img
      naturalW = img.naturalWidth
      naturalH = img.naturalHeight
      loading = false
      requestAnimationFrame(fitToScreen)
      requestAnimationFrame(render)
    } catch (e: any) {
      loadError = t('gallery.assetError')
      loading = false
    }
  }

  $effect(() => { loadImage() })

  function fitToScreen() {
    if (!wrapEl || !naturalW || !naturalH) return
    const maxW = wrapEl.clientWidth - 32
    const maxH = wrapEl.clientHeight - 32
    const scale = Math.min(1, maxW / naturalW, maxH / naturalH)
    displayScale = scale > 0 ? scale : 1
  }

  // ── Кеш «завершеного» шару ──────────────────────────────────────────
  // baseCanvas — офскрін-канва з усіма ЗАВЕРШЕНИМИ діями (actions) ПОВЕРХ
  // прозорого фону (базове зображення тут НЕ малюється — воно кладеться
  // на видиму канву окремо, першим drawImage у render). Таке розділення
  // дозволяє гумці стирати САМЕ намальоване користувачем (destination-out
  // по шару штрихів), не «прорізаючи» наскрізь скріншот під ним.
  // Перемальовується лише коли сам список actions змінюється
  // (додали/скасували дію, обрізали, завантажили зображення).
  // render() під час активного малювання (draftAction) лише копіює цей
  // готовий кеш на видиму канву одним drawImage і домальовує ПОТОЧНИЙ
  // штрих — замість повного перегравання всієї історії дій на кожен
  // pointermove. Без цього кешу довгий сеанс редагування (десятки дій)
  // великого скріншоту відчутно лагав: кожен рух миші перемальовував усі
  // попередні штрихи заново, і canvas 2D — синхронний, тож ці перемальовки
  // блокували UI-thread і лаунчер «підвисав» під час малювання.
  let baseCanvas: HTMLCanvasElement | null = null
  // scratchCanvas — тимчасова офскрін-канва для активного штриха гумки:
  // щоб destination-out під час малювання не «прорізав» базове зображення.
  let scratchCanvas: HTMLCanvasElement | null = null
  // ВАЖЛИВО: baseCanvasDirty має бути $state, а не звичайною let-змінною.
  // Раніше render() викликався ефектом, що стежив лише за draftAction/
  // canvasEl/baseImg — жоден з них не змінюється під час undo/redo/
  // resetAll (там міняється тільки actions, який рахує ОКРЕМИЙ ефект,
  // що просто виставляв прапорець і сам render() не кликав). Через це
  // після Ctrl+Z / стрілки назад canvas фізично НЕ перемальовувався, і
  // стара картинка «висіла» на екрані аж доки користувач не починав нове
  // малювання — onPointerMove викликає render() явно, і тоді нарешті
  // підхоплювався вже виставлений раніше прапорець. Зробивши прапорець
  // реактивним $state, другий ефект (render()) сам перезапускається
  // щоразу, коли перший ефект його виставляє.
  let baseCanvasDirty = $state(true)

  function markBaseDirty() { baseCanvasDirty = true }

  function rebuildBaseCanvas() {
    if (!baseImg || !naturalW || !naturalH) return
    if (!baseCanvas) baseCanvas = document.createElement('canvas')
    baseCanvas.width = naturalW
    baseCanvas.height = naturalH
    const ctx = baseCanvas.getContext('2d')
    if (!ctx) return
    ctx.clearRect(0, 0, naturalW, naturalH)
    for (const a of actions) drawAction(ctx, a)
    baseCanvasDirty = false
  }

  // ── Рендер: базове зображення + кешований шар штрихів + активна дія ──
  function render() {
    if (!canvasEl || !baseImg) return
    if (baseCanvasDirty) rebuildBaseCanvas() // читання $state тут — і робить render() реактивним до цього прапорця
    if (!baseCanvas) return
    canvasEl.width = naturalW
    canvasEl.height = naturalH
    const ctx = canvasEl.getContext('2d')
    if (!ctx) return
    ctx.clearRect(0, 0, naturalW, naturalH)
    // Спершу базовий скріншот, ПОТІМ шар штрихів — гумка (destination-out)
    // у baseCanvas діє лише на намальоване, а не «вирізає» сам скріншот.
    ctx.drawImage(baseImg, 0, 0, naturalW, naturalH)
    if (draftAction && draftAction.kind === 'stroke' && draftAction.tool === 'eraser') {
      // Активний штрих гумки не можна малювати прямо на видимій канві —
      // destination-out «прорізав» би й базове зображення під штрихом.
      // Композитимо його через scratch-канву: туди копіюється лише шар
      // завершених штрихів, стирається потрібна ділянка, і результат
      // кладеться на канву — скріншот лишається недоторканим.
      if (!scratchCanvas) scratchCanvas = document.createElement('canvas')
      scratchCanvas.width = naturalW
      scratchCanvas.height = naturalH
      const sctx = scratchCanvas.getContext('2d')
      if (sctx) {
        sctx.clearRect(0, 0, naturalW, naturalH)
        sctx.drawImage(baseCanvas, 0, 0)
        drawAction(sctx, draftAction)
        ctx.drawImage(scratchCanvas, 0, 0)
      } else {
        ctx.drawImage(baseCanvas, 0, 0)
      }
    } else {
      ctx.drawImage(baseCanvas, 0, 0)
      if (draftAction) drawAction(ctx, draftAction)
    }
  }

  function drawAction(ctx: CanvasRenderingContext2D, a: Action) {
    ctx.save()
    ctx.strokeStyle = a.color
    ctx.fillStyle = a.color
    ctx.lineCap = 'round'
    ctx.lineJoin = 'round'
    if (a.kind === 'stroke') {
      ctx.lineWidth = a.width
      ctx.globalAlpha = a.tool === 'marker' ? 0.45 : 1
      // Гумка: 'destination-out' стирає пікселі під штрихом замість того,
      // щоб малювати кольором. Оскільки ця функція викликається ЛИШЕ на
      // шарі штрихів (baseCanvas / scratchCanvas), а не на канві з базовим
      // скріншотом, гумка прибирає саме намальоване користувачем і не
      // «прорізає» саме зображення під ним.
      if (a.tool === 'eraser') ctx.globalCompositeOperation = 'destination-out'
      ctx.beginPath()
      a.points.forEach((p, i) => { if (i === 0) ctx.moveTo(p.x, p.y); else ctx.lineTo(p.x, p.y) })
      ctx.stroke()
    } else if (a.kind === 'shape') {
      ctx.lineWidth = a.width
      if (a.tool === 'rect') {
        const x = Math.min(a.from.x, a.to.x), y = Math.min(a.from.y, a.to.y)
        ctx.strokeRect(x, y, Math.abs(a.to.x - a.from.x), Math.abs(a.to.y - a.from.y))
      } else if (a.tool === 'ellipse') {
        const cx = (a.from.x + a.to.x) / 2, cy = (a.from.y + a.to.y) / 2
        const rx = Math.abs(a.to.x - a.from.x) / 2, ry = Math.abs(a.to.y - a.from.y) / 2
        ctx.beginPath()
        ctx.ellipse(cx, cy, rx, ry, 0, 0, Math.PI * 2)
        ctx.stroke()
      } else if (a.tool === 'line') {
        ctx.beginPath()
        ctx.moveTo(a.from.x, a.from.y)
        ctx.lineTo(a.to.x, a.to.y)
        ctx.stroke()
      } else if (a.tool === 'arrow') {
        drawArrow(ctx, a.from, a.to, a.width)
      }
    }
    ctx.restore()
  }

  function drawArrow(ctx: CanvasRenderingContext2D, from: Point, to: Point, width: number) {
    const headLen = Math.max(14, width * 4)
    const angle = Math.atan2(to.y - from.y, to.x - from.x)
    ctx.lineWidth = width
    ctx.beginPath()
    ctx.moveTo(from.x, from.y)
    ctx.lineTo(to.x, to.y)
    ctx.stroke()
    ctx.beginPath()
    ctx.moveTo(to.x, to.y)
    ctx.lineTo(to.x - headLen * Math.cos(angle - Math.PI / 6), to.y - headLen * Math.sin(angle - Math.PI / 6))
    ctx.lineTo(to.x - headLen * Math.cos(angle + Math.PI / 6), to.y - headLen * Math.sin(angle + Math.PI / 6))
    ctx.closePath()
    ctx.fill()
  }

  // Кеш «завершеного» шару перебудовується щоразу, коли САМ список
  // завершених дій змінюється (не draftAction — той не входить у actions,
  // поки штрих не завершено onPointerUp). render() тут реагує і на
  // actions.length (нова/скасована дія), і на draftAction (поточний
  // штрих) — але перебудова важкого baseCanvas відбувається лише коли
  // саме actions.length змінився, завдяки прапорцю baseCanvasDirty:
  // рух миші під час малювання лише копіює вже готовий кеш, не перегравши
  // всю історію дій щоразу.
  $effect(() => {
    actions.length // явна залежність — Svelte стежить за довжиною масиву
    markBaseDirty()
  })

  $effect(() => { render() })

  // ── Малювання: переведення координат миші у природні пікселі ──
  function toNatural(e: PointerEvent): Point {
    const rect = canvasEl.getBoundingClientRect()
    return {
      x: (e.clientX - rect.left) / displayScale,
      y: (e.clientY - rect.top) / displayScale,
    }
  }

  let drawing = false
  let draftAction = $state<Action | null>(null)
  let dragStart: Point | null = null

  function onPointerDown(e: PointerEvent) {
    if (tool === 'crop') { onCropPointerDown(e); return }
    if (loading || !baseImg) return
    const p = toNatural(e)
    drawing = true
    redoStack = []
    if (tool === 'brush' || tool === 'marker' || tool === 'eraser') {
      draftAction = { kind: 'stroke', tool, color, width: thickness, points: [p] }
    } else if (tool === 'rect' || tool === 'ellipse' || tool === 'line' || tool === 'arrow') {
      dragStart = p
      draftAction = { kind: 'shape', tool, color, width: thickness, from: p, to: p }
    }
    ;(e.target as Element).setPointerCapture?.(e.pointerId)
  }

  function onPointerMove(e: PointerEvent) {
    if (tool === 'crop') { onCropPointerMove(e); return }
    if (!drawing || !draftAction) return
    const p = toNatural(e)
    if (draftAction.kind === 'stroke') {
      draftAction = { ...draftAction, points: [...draftAction.points, p] }
    } else if (draftAction.kind === 'shape' && dragStart) {
      draftAction = { ...draftAction, to: p }
    }
    render()
  }

  function onPointerUp(e: PointerEvent) {
    if (tool === 'crop') { onCropPointerUp(e); return }
    if (!drawing) return
    drawing = false
    if (draftAction) {
      actions = [...actions, draftAction]
      draftAction = null
    }
    dragStart = null
  }

  // ── Обрізання ──
  let cropAspect = $state<number | null>(null) // null = вільно, інакше w/h
  type CropHandle = 'move' | 'nw' | 'ne' | 'sw' | 'se' | 'n' | 's' | 'e' | 'w'
  let cropDragMode = $state<CropHandle | null>(null)
  let cropDragStart: Point | null = null
  let cropRectStart: { x: number; y: number; w: number; h: number } | null = null

  const aspectPresets: { label: string; value: number | null }[] = [
    { label: 'Free', value: null },
    { label: '1:1', value: 1 },
    { label: '4:3', value: 4 / 3 },
    { label: '16:9', value: 16 / 9 },
    { label: '3:4', value: 3 / 4 },
    { label: '9:16', value: 9 / 16 },
  ]

  function enterCrop() {
    tool = 'crop'
    // За замовчуванням рамка обрізання охоплює ВСЕ зображення — користувач
    // сам зменшує її до потрібного фрагмента (раніше було 80% по центру,
    // тобто «нарізка» стартувала вже з урізаною областю).
    cropRect = { x: 0, y: 0, w: naturalW, h: naturalH }
  }

  function applyAspect(a: number | null) {
    cropAspect = a
    if (!cropRect) return
    if (a === null) return
    const cx = cropRect.x + cropRect.w / 2, cy = cropRect.y + cropRect.h / 2
    let w = cropRect.w, h = w / a
    if (h > naturalH) { h = naturalH; w = h * a }
    if (w > naturalW) { w = naturalW; h = w / a }
    cropRect = clampCrop({ x: cx - w / 2, y: cy - h / 2, w, h })
  }

  // clampCrop втискає рамку в межі зображення. Коли задана фіксована
  // пропорція (cropAspect !== null) — зменшення ЗАВЖДИ рахує обидва
  // виміри разом (через ту саму пропорцію), а не «урізає» w і h окремо:
  // інакше рамка 16:9, що впирається у край зображення під час
  // перетягування, ламала б пропорцію рівно в момент дотику до межі —
  // це і була причина «пропорція не тримається», хоч сам код ресайзу
  // (resizeWithAspect) її рахував правильно.
  function clampCrop(r: { x: number; y: number; w: number; h: number }) {
    let { x, y, w, h } = r
    if (cropAspect) {
      if (w > naturalW) { w = naturalW; h = w / cropAspect }
      if (h > naturalH) { h = naturalH; w = h * cropAspect }
    } else {
      w = Math.min(w, naturalW)
      h = Math.min(h, naturalH)
    }
    x = Math.max(0, Math.min(x, naturalW - w))
    y = Math.max(0, Math.min(y, naturalH - h))
    return { x, y, w, h }
  }

  // cropHandleAt визначає, за яку частину рамки обрізання схопився
  // користувач: 4 кути (діагональний ресайз) + 4 середини сторін (ресайз
  // лише по одній осі — це і була відсутня можливість «рухати 1 сторону»)
  // + сама рамка (переміщення цілком). Порядок перевірки: спершу кути
  // (вужча зона, точніша), потім середини сторін, потім «усередині».
  function cropHandleAt(p: Point): CropHandle | null {
    if (!cropRect) return null
    const hs = 14 / displayScale // хват-зона хендла в природних пікселях
    const { x, y, w, h } = cropRect
    const midX = x + w / 2, midY = y + h / 2
    const corners: [('nw' | 'ne' | 'sw' | 'se'), Point][] = [
      ['nw', { x, y }], ['ne', { x: x + w, y }], ['sw', { x, y: y + h }], ['se', { x: x + w, y: y + h }],
    ]
    for (const [name, c] of corners) {
      if (Math.abs(p.x - c.x) < hs && Math.abs(p.y - c.y) < hs) return name
    }
    const edges: [('n' | 's' | 'e' | 'w'), Point][] = [
      ['n', { x: midX, y }], ['s', { x: midX, y: y + h }], ['w', { x, y: midY }], ['e', { x: x + w, y: midY }],
    ]
    for (const [name, c] of edges) {
      if (Math.abs(p.x - c.x) < hs && Math.abs(p.y - c.y) < hs) return name
    }
    if (p.x >= x && p.x <= x + w && p.y >= y && p.y <= y + h) return 'move'
    return null
  }

  let cropHoverHandle = $state<CropHandle | null>(null)

  const cropCursors: Record<CropHandle, string> = {
    move: 'move', nw: 'nwse-resize', se: 'nwse-resize', ne: 'nesw-resize', sw: 'nesw-resize',
    n: 'ns-resize', s: 'ns-resize', e: 'ew-resize', w: 'ew-resize',
  }

  function onCropPointerDown(e: PointerEvent) {
    if (!cropRect) return
    const p = toNatural(e)
    cropDragMode = cropHandleAt(p)
    cropDragStart = p
    cropRectStart = { ...cropRect }
    ;(e.target as Element).setPointerCapture?.(e.pointerId)
  }

  function onCropPointerMove(e: PointerEvent) {
    if (!cropDragMode) {
      // Не тягнемо — лише оновлюємо курсор під наведеною частиною рамки
      // (кут / сторона / усередині), щоб було видно, ЩО саме потягне клік.
      cropHoverHandle = cropHandleAt(toNatural(e))
      return
    }
    if (!cropDragStart || !cropRectStart || !cropRect) return
    const p = toNatural(e)
    const dx = p.x - cropDragStart.x, dy = p.y - cropDragStart.y
    let r = { ...cropRectStart }
    if (cropDragMode === 'move') {
      r.x = cropRectStart.x + dx
      r.y = cropRectStart.y + dy
    } else if (cropAspect) {
      // З фіксованою пропорцією будь-який хендл (навіть на середині
      // сторони) веде однаковий пропорційний ресайз — інакше рух лише
      // по одній осі зламав би задану пропорцію.
      r = resizeWithAspect(cropRectStart, cropDragMode, dx, dy, cropAspect)
    } else {
      // Вільна пропорція: кути тягнуть дві сторони одразу, середини
      // сторін — лише одну (саме це й дає змогу рухати одну сторону
      // окремо, чого не було раніше).
      if (cropDragMode === 'se') { r.w = cropRectStart.w + dx; r.h = cropRectStart.h + dy }
      else if (cropDragMode === 'sw') { r.x = cropRectStart.x + dx; r.w = cropRectStart.w - dx; r.h = cropRectStart.h + dy }
      else if (cropDragMode === 'ne') { r.y = cropRectStart.y + dy; r.w = cropRectStart.w + dx; r.h = cropRectStart.h - dy }
      else if (cropDragMode === 'nw') { r.x = cropRectStart.x + dx; r.y = cropRectStart.y + dy; r.w = cropRectStart.w - dx; r.h = cropRectStart.h - dy }
      else if (cropDragMode === 'n') { r.y = cropRectStart.y + dy; r.h = cropRectStart.h - dy }
      else if (cropDragMode === 's') { r.h = cropRectStart.h + dy }
      else if (cropDragMode === 'w') { r.x = cropRectStart.x + dx; r.w = cropRectStart.w - dx }
      else if (cropDragMode === 'e') { r.w = cropRectStart.w + dx }
      r.w = Math.max(20, r.w)
      r.h = Math.max(20, r.h)
    }
    cropRect = clampCrop(r)
  }

  // resizeWithAspect — ресайз з фіксованою пропорцією aspect=w/h. Для
  // кутів рахуємо ширину і висоту з ОБОХ dx/dy (через пропорцію) і беремо
  // ту зміну, що дає більшу за модулем різницю площі — так курсор реально
  // «веде» рамку в напрямку руху миші, а не лише по горизонталі, як тягнеш
  // по діагоналі. Для середин сторін — лише відповідна вісь. Протилежний
  // бік рамки (не той, за який тягнуть) лишається нерухомим.
  function resizeWithAspect(
    start: { x: number; y: number; w: number; h: number },
    handle: CropHandle,
    dx: number, dy: number,
    aspect: number
  ): { x: number; y: number; w: number; h: number } {
    const left = start.x, top = start.y, right = start.x + start.w, bottom = start.y + start.h

    // Для кутів: рахуємо варіант «веде dx» (ширина + dx, висота від неї)
    // і варіант «веде dy» (висота + dy, ширина від неї), обираємо той, що
    // дає більшу за модулем зміну ширини — це і є «домінантна вісь руху».
    function cornerSize(dxSign: 1 | -1, dySign: 1 | -1): { w: number; h: number } {
      const wFromDx = start.w + dxSign * dx
      const hFromDy = start.h + dySign * dy
      const wFromDy = hFromDy * aspect
      return Math.abs(wFromDx - start.w) >= Math.abs(wFromDy - start.w)
        ? { w: wFromDx, h: wFromDx / aspect }
        : { w: wFromDy, h: hFromDy }
    }

    switch (handle) {
      case 'se': {
        const { w, h } = cornerSize(1, 1)
        return clampAnchored(left, top, w, h, 'tl')
      }
      case 'nw': {
        const { w, h } = cornerSize(-1, -1)
        return clampAnchored(right, bottom, w, h, 'br')
      }
      case 'ne': {
        const { w, h } = cornerSize(1, -1)
        return clampAnchored(left, bottom, w, h, 'bl')
      }
      case 'sw': {
        const { w, h } = cornerSize(-1, 1)
        return clampAnchored(right, top, w, h, 'tr')
      }
      case 'n': {
        const newH = start.h - dy
        return clampAnchored((left + right) / 2, bottom, newH * aspect, newH, 'bc')
      }
      case 's': {
        const newH = start.h + dy
        return clampAnchored((left + right) / 2, top, newH * aspect, newH, 'tc')
      }
      case 'w': {
        const newW = start.w - dx
        return clampAnchored(right, (top + bottom) / 2, newW, newW / aspect, 'rc')
      }
      case 'e': {
        const newW = start.w + dx
        return clampAnchored(left, (top + bottom) / 2, newW, newW / aspect, 'lc')
      }
      default:
        return start
    }
  }

  // clampAnchored будує прямокутник newW×newH так, щоб задана «якірна»
  // точка anchor (кут або середина сторони, що лишається нерухомою під
  // час ресайзу) лишалась на своєму місці — anchorKind визначає, ЯКОЮ
  // частиною нового прямокутника є ця точка (tl=top-left, br=bottom-right
  // і так далі, c=center по відповідній осі).
  // clampAnchored будує прямокутник newW×newH так, щоб задана «якірна»
  // точка anchor (кут або середина сторони, що лишається нерухомою під
  // час ресайзу) лишалась на своєму місці — anchorKind визначає, ЯКОЮ
  // частиною нового прямокутника є ця точка (tl=top-left, br=bottom-right
  // і так далі, c=center по відповідній осі). aspect (якщо задано) —
  // мінімальний розмір 20px рахується РАЗОМ для w і h через пропорцію,
  // а не для кожного окремо: інакше на дуже маленькій рамці пропорція
  // могла зламатись рівно біля мінімального розміру.
  function clampAnchored(
    anchorX: number, anchorY: number, w: number, h: number,
    anchorKind: 'tl' | 'tr' | 'bl' | 'br' | 'tc' | 'bc' | 'lc' | 'rc',
    aspect?: number
  ): { x: number; y: number; w: number; h: number } {
    const minSize = 20
    if (aspect) {
      if (w < minSize) { w = minSize; h = w / aspect }
      if (h < minSize) { h = minSize; w = h * aspect }
    } else {
      w = Math.max(minSize, w)
      h = Math.max(minSize, h)
    }
    let x: number, y: number
    switch (anchorKind) {
      case 'tl': x = anchorX; y = anchorY; break
      case 'tr': x = anchorX - w; y = anchorY; break
      case 'bl': x = anchorX; y = anchorY - h; break
      case 'br': x = anchorX - w; y = anchorY - h; break
      case 'tc': x = anchorX - w / 2; y = anchorY; break
      case 'bc': x = anchorX - w / 2; y = anchorY - h; break
      case 'lc': x = anchorX; y = anchorY - h / 2; break
      case 'rc': x = anchorX - w; y = anchorY - h / 2; break
    }
    return { x, y, w, h }
  }

  function onCropPointerUp(e: PointerEvent) {
    cropDragMode = null
    cropDragStart = null
    cropRectStart = null
  }

  function applyCrop() {
    if (!cropRect) return
    // Обрізання «випікає» поточний стан (база + всі дії) у нову базову
    // картинку розміром рамки — після цього координати наступних дій
    // знову рахуються від (0,0) нового зображення.
    const tmp = document.createElement('canvas')
    tmp.width = Math.round(cropRect.w)
    tmp.height = Math.round(cropRect.h)
    const tctx = tmp.getContext('2d')
    if (!tctx || !canvasEl) return
    tctx.drawImage(canvasEl, cropRect.x, cropRect.y, cropRect.w, cropRect.h, 0, 0, tmp.width, tmp.height)
    const img = new Image()
    img.onload = () => {
      baseImg = img
      naturalW = tmp.width
      naturalH = tmp.height
      actions = []
      redoStack = []
      cropRect = null
      tool = 'brush'
      requestAnimationFrame(fitToScreen)
      requestAnimationFrame(render)
    }
    img.src = tmp.toDataURL('image/png')
  }

  function cancelCrop() {
    cropRect = null
    tool = 'brush'
  }

  // ── Undo/Redo ──
  // Додатковий явний виклик markBaseDirty()+render() після зміни стеку
  // дій гарантує миттєве перемалювання canvas (зокрема для resetAll, де
  // реактивний ланцюжок $effect інакше оновлював екран лише після
  // наступного руху миші/нового штриха).
  function undo() {
    if (actions.length === 0) return
    const a = actions[actions.length - 1]
    actions = actions.slice(0, -1)
    redoStack = [...redoStack, a]
    markBaseDirty()
    render()
  }

  function redo() {
    if (redoStack.length === 0) return
    const a = redoStack[redoStack.length - 1]
    redoStack = redoStack.slice(0, -1)
    actions = [...actions, a]
    markBaseDirty()
    render()
  }

  function resetAll() {
    actions = []
    redoStack = []
    markBaseDirty()
    render()
  }

  function onKeydown(e: KeyboardEvent) {
    if ((e.ctrlKey || e.metaKey) && e.key === 'z' && !e.shiftKey) { e.preventDefault(); undo() }
    else if ((e.ctrlKey || e.metaKey) && (e.key === 'y' || (e.key === 'z' && e.shiftKey))) { e.preventDefault(); redo() }
    else if (e.key === 'Escape') { requestClose() }
  }

  // ── Збереження / копіювання ──
  // ── Збереження / копіювання ──
  function exportDataURL(): string {
    markBaseDirty()
    render()
    return canvasEl.toDataURL('image/png')
  }

  async function saveOverwrite() {
    saving = true
    try {
      await App.SaveEditedScreenshot(packId, entry.fileName, exportDataURL())
      toast(t('editor.saved'), 'success')
      onSaved()
      onClose()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      saving = false
    }
  }

  async function saveAsCopy() {
    saving = true
    try {
      // exportDataURL() — ДО діалогу вибору файлу: фіксуємо поточний
      // стан canvas одразу (як і при звичайному «Зберегти»), а не після
      // того, як користувач кілька секунд вибирав папку в системному
      // діалозі — бо весь цей час textarea незавершеного inline-тексту
      // й overlay редактора лишаються в тому ж стані, це не критично, але
      // так поведінка «Зберегти» і «Зберегти як» лишається однаковою.
      const data = exportDataURL()
      const name = await App.SaveEditedScreenshotAsCopy(packId, entry.fileName, data)
      if (!name) {
        // Користувач закрив діалог вибору файлу без збереження — це не
        // помилка, просто нічого не робимо (без тосту, без закриття
        // редактора).
        return
      }
      toast(t('editor.savedAsCopy', { name }), 'success')
      onSaved()
      onClose()
    } catch (e: any) {
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      saving = false
    }
  }

  async function copyToClipboard() {
    try {
      await App.CopyEditedImageToClipboard(exportDataURL())
      toast(t('editor.copyDone'), 'success')
    } catch (e: any) {
      toast(t('editor.copyError'), 'error')
    }
  }

  async function requestClose() {
    if (dirty) {
      const ok = await confirmDialog(t('editor.exitConfirm'), t('editor.exitConfirmHint'), { confirmLabel: t('editor.exit'), cancelLabel: t('gallery.cancel'), danger: true })
      if (!ok) return
    }
    onClose()
  }

  const colorPresets = ['#ff3b30', '#ffcc00', '#34c759', '#0a84ff', '#ffffff', '#000000']
</script>

<svelte:window onkeydown={onKeydown} onresize={fitToScreen} />

<div class="editor-overlay">
  <div class="editor-topbar">
    <div class="editor-file" title={entry.fileName}><i class="ti ti-photo"></i> {entry.title || entry.fileName}</div>
    <div style="flex:1"></div>
    <button class="btn btn-ghost btn-sm" onclick={undo} disabled={actions.length === 0} title={t('editor.undo')}><i class="ti ti-arrow-back-up"></i></button>
    <button class="btn btn-ghost btn-sm" onclick={redo} disabled={redoStack.length === 0} title={t('editor.redo')}><i class="ti ti-arrow-forward-up"></i></button>
    <button class="btn btn-ghost btn-sm" onclick={resetAll} disabled={actions.length === 0} title={t('editor.reset')}><i class="ti ti-trash-x"></i></button>
    <button class="btn btn-ghost btn-sm" onclick={copyToClipboard} title={t('editor.copyToClipboard')}><i class="ti ti-copy"></i></button>
    <button class="btn btn-ghost btn-sm" onclick={saveAsCopy} disabled={saving}><i class="ti ti-copy-plus"></i> {t('editor.saveAsCopy')}</button>
    <button class="btn btn-primary btn-sm" onclick={saveOverwrite} disabled={saving}>
      {#if saving}<i class="ti ti-loader spin"></i> {t('editor.saving')}{:else}<i class="ti ti-device-floppy"></i> {t('editor.save')}{/if}
    </button>
    <button class="btn btn-ghost btn-sm" onclick={requestClose} title={t('gallery.close')}><i class="ti ti-x"></i></button>
  </div>

  <div class="editor-body">
    {#if tool !== 'crop'}
      <div class="editor-toolside">
        <button class="etool" class:active={tool === 'brush'} onclick={() => tool = 'brush'} title={t('editor.tool.brush')}><i class="ti ti-brush"></i></button>
        <button class="etool" class:active={tool === 'marker'} onclick={() => tool = 'marker'} title={t('editor.tool.marker')}><i class="ti ti-highlight"></i></button>
        <button class="etool" class:active={tool === 'eraser'} onclick={() => tool = 'eraser'} title={t('editor.tool.eraser')}><i class="ti ti-eraser"></i></button>
        <button class="etool" class:active={tool === 'line'} onclick={() => tool = 'line'} title={t('editor.tool.line')}><i class="ti ti-slash"></i></button>
        <button class="etool" class:active={tool === 'arrow'} onclick={() => tool = 'arrow'} title={t('editor.tool.arrow')}><i class="ti ti-arrow-up-right"></i></button>
        <button class="etool" class:active={tool === 'rect'} onclick={() => tool = 'rect'} title={t('editor.tool.rect')}><i class="ti ti-square"></i></button>
        <button class="etool" class:active={tool === 'ellipse'} onclick={() => tool = 'ellipse'} title={t('editor.tool.ellipse')}><i class="ti ti-circle"></i></button>
        <div class="etool-sep"></div>
        <button class="etool" onclick={enterCrop} title={t('editor.tool.crop')}><i class="ti ti-crop"></i></button>
        <div class="etool-sep"></div>
        <div class="etool-label">{t('editor.color')}</div>
        <div class="etool-colors">
          {#each colorPresets as c}
            <button class="ecolor" class:active={color === c} style="background:{c}" onclick={() => color = c}></button>
          {/each}
        </div>
        <input class="ecolor-custom" type="color" bind:value={color} title={t('editor.color')} />
        {#if tool !== 'eraser'}
          <div class="etool-label">{t('editor.thickness')}</div>
          <input class="ethickness" type="range" min="1" max="24" bind:value={thickness} />
        {:else}
          <div class="etool-label">{t('editor.eraserSize')}</div>
          <input class="ethickness" type="range" min="4" max="80" bind:value={thickness} />
        {/if}
      </div>
    {/if}

    <div class="editor-canvas-wrap" bind:this={wrapEl}>
      {#if loading}
        <div class="editor-loading"><i class="ti ti-loader spin"></i> {t('gallery.loading')}</div>
      {:else if loadError}
        <div class="editor-loading">{loadError}</div>
      {:else}
        <div class="editor-canvas-inner" style="width:{naturalW * displayScale}px;height:{naturalH * displayScale}px">
          <canvas
            bind:this={canvasEl}
            style="width:100%;height:100%;touch-action:none;cursor:{tool === 'crop' ? 'default' : tool === 'eraser' ? 'cell' : 'crosshair'}"
            onpointerdown={onPointerDown}
            onpointermove={onPointerMove}
            onpointerup={onPointerUp}
          ></canvas>
          {#if tool === 'crop' && cropRect}
            <div class="crop-overlay">
              <div class="crop-mask" style="clip-path: polygon(0% 0%, 0% 100%, {cropRect.x*displayScale}px 100%, {cropRect.x*displayScale}px {cropRect.y*displayScale}px, {(cropRect.x+cropRect.w)*displayScale}px {cropRect.y*displayScale}px, {(cropRect.x+cropRect.w)*displayScale}px {(cropRect.y+cropRect.h)*displayScale}px, {cropRect.x*displayScale}px {(cropRect.y+cropRect.h)*displayScale}px, {cropRect.x*displayScale}px 100%, 100% 100%, 100% 0%)"></div>
              <div class="crop-box"
                   style="left:{cropRect.x*displayScale}px;top:{cropRect.y*displayScale}px;width:{cropRect.w*displayScale}px;height:{cropRect.h*displayScale}px;cursor:{cropCursors[cropDragMode ?? cropHoverHandle ?? 'move']}"
                   onpointerdown={onCropPointerDown} onpointermove={onCropPointerMove} onpointerup={onCropPointerUp}>
                <div class="crop-handle nw"></div>
                <div class="crop-handle ne"></div>
                <div class="crop-handle sw"></div>
                <div class="crop-handle se"></div>
                <div class="crop-handle-edge n"></div>
                <div class="crop-handle-edge s"></div>
                <div class="crop-handle-edge w"></div>
                <div class="crop-handle-edge e"></div>
              </div>
            </div>
          {/if}
        </div>
      {/if}
    </div>
  </div>

  {#if tool === 'crop'}
    <div class="crop-bottombar">
      <span class="crop-label">{t('editor.tool.crop')}:</span>
      {#each aspectPresets as p}
        <button class="crop-aspect" class:active={cropAspect === p.value} onclick={() => applyAspect(p.value)}>{p.value === null ? t('editor.cropFree') : p.label}</button>
      {/each}
      <div style="flex:1"></div>
      <button class="btn btn-ghost btn-sm" onclick={cancelCrop}>{t('editor.cropCancel')}</button>
      <button class="btn btn-primary btn-sm" onclick={applyCrop}><i class="ti ti-check"></i> {t('editor.cropApply')}</button>
    </div>
  {/if}
</div>

<style>
  .editor-overlay {
    position:fixed; inset:0; z-index:2000000; background:#0a0a0a;
    display:flex; flex-direction:column;
    animation:editor-overlay-in .16s ease-out both;
  }
  @keyframes editor-overlay-in {
    from { opacity:0; }
    to   { opacity:1; }
  }
  .editor-topbar {
    display:flex; align-items:center; gap:8px; padding:10px 16px;
    background:var(--bg-panel); border-bottom:1px solid var(--border); flex-shrink:0;
    animation:editor-topbar-in .22s cubic-bezier(.16,1,.3,1) .04s both;
  }
  @keyframes editor-topbar-in {
    from { opacity:0; transform:translateY(-6px); }
    to   { opacity:1; transform:none; }
  }
  .editor-file { display:flex; align-items:center; gap:8px; font-size:13px; font-weight:700; color:var(--text); max-width:320px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
  .editor-file i { color:var(--orange); }
  .btn-sm { padding:7px 11px; font-size:11.5px; }

  .editor-body { flex:1; display:flex; overflow:hidden; }

  .editor-toolside {
    width:76px; flex-shrink:0; background:var(--bg-panel); border-right:1px solid var(--border);
    display:flex; flex-direction:column; align-items:center; gap:8px; padding:14px 8px; overflow-y:auto;
  }
  .etool {
    width:48px; height:48px; border-radius:12px; border:1px solid var(--border); background:var(--bg-input);
    color:var(--text-mute); font-size:19px; cursor:pointer; display:flex; align-items:center; justify-content:center;
    transition:background .15s, color .15s, border-color .15s;
  }
  .etool:hover { background:var(--hover-strong); color:var(--text); }
  .etool.active { background:var(--orange); color:#fff; border-color:var(--orange); }
  .etool-sep { width:100%; height:1px; background:var(--border); margin:2px 0; }
  .etool-label { font-size:9px; color:var(--text-mute); text-transform:uppercase; letter-spacing:.04em; margin-top:2px; }
  .etool-colors { display:grid; grid-template-columns:repeat(2, 1fr); gap:6px; }
  .ecolor { width:20px; height:20px; border-radius:50%; border:2px solid var(--border); cursor:pointer; }
  .ecolor.active { border-color:var(--text); box-shadow:0 0 0 2px var(--orange); }
  .ecolor-custom { width:48px; height:26px; border:none; border-radius:8px; background:transparent; cursor:pointer; padding:0; }
  .ethickness { width:56px; }

  .editor-canvas-wrap { flex:1; display:flex; align-items:center; justify-content:center; overflow:auto; padding:16px; position:relative; }
  .editor-loading { color:var(--text-mute); font-size:13px; display:flex; align-items:center; gap:8px; }
  .editor-canvas-inner { position:relative; box-shadow:0 20px 60px rgba(0,0,0,.6); }
  .editor-canvas-inner canvas { display:block; border-radius:4px; }

  .crop-overlay { position:absolute; inset:0; pointer-events:none; }
  .crop-mask { position:absolute; inset:0; background:rgba(0,0,0,.6); }
  .crop-box {
    position:absolute; border:2px solid #fff; box-shadow:0 0 0 1px rgba(0,0,0,.4);
    pointer-events:auto; cursor:move;
  }
  .crop-handle {
    position:absolute; width:14px; height:14px; background:#fff; border:2px solid var(--orange);
    border-radius:50%; pointer-events:none;
  }
  .crop-handle.nw { left:-8px; top:-8px; }
  .crop-handle.ne { right:-8px; top:-8px; }
  .crop-handle.sw { left:-8px; bottom:-8px; }
  .crop-handle.se { right:-8px; bottom:-8px; }

  /* Хендли на серединах сторін — так само лише візуальні (pointer-events:
     none, як і кутові вище) — реальне визначення «за що схопились»
     рахується в JS (cropHandleAt) з координат кліку всередині .crop-box,
     а не окремими DOM-обробниками на кожному хендлі. */
  .crop-handle-edge {
    position:absolute; background:#fff; border:2px solid var(--orange);
    pointer-events:none; border-radius:4px;
  }
  .crop-handle-edge.n, .crop-handle-edge.s { left:50%; width:22px; height:10px; margin-left:-11px; }
  .crop-handle-edge.n { top:-6px; }
  .crop-handle-edge.s { bottom:-6px; }
  .crop-handle-edge.w, .crop-handle-edge.e { top:50%; width:10px; height:22px; margin-top:-11px; }
  .crop-handle-edge.w { left:-6px; }
  .crop-handle-edge.e { right:-6px; }

  .crop-bottombar {
    display:flex; align-items:center; gap:8px; padding:12px 16px; background:var(--bg-panel);
    border-top:1px solid var(--border); flex-shrink:0;
  }
  .crop-label { font-size:12px; color:var(--text-mute); font-weight:600; margin-right:4px; }
  .crop-aspect {
    padding:7px 13px; border-radius:8px; border:1px solid var(--border); background:var(--bg-input);
    color:var(--text-mute); font-size:12px; font-weight:600; cursor:pointer; transition:background .15s, color .15s;
  }
  .crop-aspect:hover { background:var(--hover-strong); color:var(--text); }
  .crop-aspect.active { background:var(--orange); color:#fff; border-color:var(--orange); }
</style>