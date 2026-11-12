<script lang="ts">
  import { tip as launcherTip } from './friends/tooltip'

  // ── ExportForge — анімація експорту збірки ──────────────────────────────
  // Canvas-сцена «конвеєр»: з лійки ліворуч моди (реальні іконки з jar /
  // pack.png / Modrinth) падають на стрічку й їдуть по лінії до КОНВЕРТОРА.
  // Усередині, за скляним віконцем, іконка «розкладається» в хеш-ідентикон під
  // променем сканера, а на виході вилазить уже ЗАХЕШОВАНОЮ — з міткою #a3f91c —
  // і їде далі, де падає у відкритий пакунок (він підстрибує, «+1»). Конфіги та
  // інші файли не хешуються: вони їдуть тонкою обхідною доріжкою під конвертором
  // просто в пакунок. Сама стрічка підсвічується зліва направо — це і є прогрес.
  // На завершення пакунок виїжджає в центр, кришка зачиняється, іконки
  // розлітаються конфеті, ударні хвилі й галочка.
  //
  // Подій прогресу можуть бути тисячі за секунду, тому сцена не прив'язана до
  // Svelte-реактивності: push() кладе іконки в чергу, requestAnimationFrame
  // видає їх на стрічку з темпом, що росте разом із чергою. HUD (відсотки,
  // лічильники) оновлюється не частіше ~8 разів на секунду.
  //
  // Родитель: bind:this → push(p) на кожну подію export:progress, finish()
  // наприкінці (повертає Promise, що завершується після фіналу анімації).
  import { onMount, onDestroy } from 'svelte'
  import Portal from './Portal.svelte'
  import { t } from './i18n.svelte'
  import { App } from './wails'
  import type { ExportProgress } from './types'

  let {
    packId = '',
    packName = '',
    // Скільки окремих проходів експорту буде (ExportPack + ExportShaurmpack).
    rounds = 1,
    onready = () => {}
  }: { packId?: string; packName?: string; rounds?: number; onready?: () => void } = $props()

  type Kind = 'mod' | 'pack' | 'shader' | 'data' | 'dust'

  // ── HUD (реактивний стан, оновлюється рідко) ─────────────────────────────
  let hudStage = $state<string>('')
  let hudPct = $state(0)
  let hudLabel = $state('')
  let hudCounts = $state({ mod: 0, pack: 0, shader: 0, data: 0, files: 0 })
  let hudSeconds = $state(0)
  let hudDone = $state(false)
  let stagesSeen = $state<Record<string, boolean>>({})

  const STEPS: { id: string; key: string }[] = [
    { id: 'scan', key: 'export.stage.scan' },
    { id: 'hash', key: 'export.stage.hash' },
    { id: 'resolve', key: 'export.stage.resolve' },
    { id: 'pack', key: 'export.stage.pack' }
  ]
  const STAGE_ORDER = ['mod_sources', 'scan', 'hash', 'resolve', 'pack', 'done']
  // Вага етапу в загальному відсотку одного проходу.
  const WEIGHTS: Record<string, [number, number]> = {
    mod_sources: [0, 0.05],
    scan: [0.05, 0.05],
    hash: [0.1, 0.3],
    resolve: [0.4, 0.1],
    pack: [0.5, 0.5]
  }

  // ── Стан сцени (не реактивний) ───────────────────────────────────────────
  type Item = { key: string; kind: Kind; name: string; url: string; seed: number }
  // stage: 0 — до конвертора, 1 — всередині, 2 — захешований. dive>0 — падає в пакунок.
  type Belt = { item: Item; x: number; drop: number; stage: 0 | 1 | 2; ex: number; dive: number; hash: string }
  type Chip = { x: number; hue: number }
  type Part = { x: number; y: number; vx: number; vy: number; life: number; max: number; r: number; hue: number; add: boolean; icon?: Item; rot?: number; vr?: number; g?: number }
  type Pop = { x: number; y: number; t: number; text: string }
  type Ring = { t: number; max: number }

  let canvas = $state<HTMLCanvasElement>()
  let ctx: CanvasRenderingContext2D | null = null
  let W = 0
  let H = 0
  let dpr = 1
  let raf = 0
  let lastTs = 0
  let startedAt = 0
  let hudTick = 0

  const queue: Item[] = []
  const belt: Belt[] = []
  const lane: Chip[] = []
  const recent: Item[] = []
  const parts: Part[] = []
  const pops: Pop[] = []
  const rings: Ring[] = []
  const seen = new Set<string>()
  const counts = { mod: 0, pack: 0, shader: 0, data: 0, files: 0 }
  let dustBudget = 0
  let laneAcc = 0
  let spawnAcc = 0
  let bump = 0           // 0..1 — пружинний відбій пакунка
  let glow = 0           // 0..1 — світло зсередини пакунка
  let busy = 0           // 0..1 — наскільки конвертор зараз «працює»
  let beltPhase = 0      // зсув рисок стрічки
  let beltSpeed = 160
  let ringPct = 0        // плавний відсоток (підсвітка стрічки)
  let targetPct = 0
  let round = 0
  let lastStageIdx = -1
  let afterDone = false
  let finale = false
  let finaleT = 0
  let burst = false
  let finaleResolve: (() => void) | null = null
  let orangeRgb = '255, 144, 30'
  let reduced = false
  let iconBase = ''
  let theme = { card: '#1e1e1e', panel: '#161616', edge: 'rgba(255,255,255,.14)', text: '#ffffff', mute: '#a0a0a0' }

  const MAX_BELT = 16
  const MAX_LANE = 40
  const MAX_PARTS = 360
  const RECENT = 18

  // ── Іконки ───────────────────────────────────────────────────────────────
  const imgs = new Map<string, HTMLImageElement | 'fail'>()
  function iconOf(url: string): HTMLImageElement | null {
    if (!url) return null
    const hit = imgs.get(url)
    if (hit === 'fail') return null
    if (hit) return hit.complete && hit.naturalWidth > 0 ? hit : null
    const img = new Image()
    img.decoding = 'async'
    img.onerror = () => imgs.set(url, 'fail')
    img.src = url
    imgs.set(url, img)
    if (imgs.size > 500) {
      const first = imgs.keys().next().value
      if (first !== undefined) imgs.delete(first)
    }
    return img.complete && img.naturalWidth > 0 ? img : null
  }

  function hash32(s: string): number {
    let h = 2166136261
    for (let i = 0; i < s.length; i++) { h ^= s.charCodeAt(i); h = Math.imul(h, 16777619) }
    return h >>> 0
  }
  // Наочний «хеш» для мітки на виході конвертора (відтворюваний для того самого файлу).
  const hashLabel = (key: string) => '#' + hash32('sha1:' + key).toString(16).padStart(8, '0').slice(0, 6)

  const KIND_HUE: Record<Kind, number> = { mod: 28, pack: 140, shader: 270, data: 200, dust: 40 }

  function classify(p: ExportProgress): Item | null {
    const f = (p.fileName || '').replace(/\\/g, '/')
    if (!f) return null
    const segs = f.split('/')
    const top = segs[0]
    const mk = (kind: Kind, key: string, name: string, local: string): Item => ({
      key, kind, name: p.modName || name,
      url: p.iconUrl || (iconBase && local ? `${iconBase}/${encodeURIComponent(packId)}/${local.split('/').map(encodeURIComponent).join('/')}` : ''),
      seed: hash32(key)
    })
    if (top === 'mods' && segs.length === 2 && /\.(jar|disabled)$/i.test(segs[1])) {
      return mk('mod', f, segs[1].replace(/\.(jar)?(\.disabled)?$/i, ''), f)
    }
    if (top === 'resourcepacks' && segs.length >= 2) {
      const key = `${top}/${segs[1]}`
      return mk('pack', key, segs[1].replace(/\.zip$/i, ''), key)
    }
    if (top === 'shaderpacks' && segs.length >= 2) {
      const key = `${top}/${segs[1]}`
      return mk('shader', key, segs[1].replace(/\.zip$/i, ''), '')
    }
    if (top === 'datapacks' || (top === 'saves' && segs.includes('datapacks'))) {
      const key = segs.slice(0, 2).join('/')
      return mk('data', key, segs[1] ?? top, '')
    }
    return { key: f, kind: 'dust', name: segs[segs.length - 1], url: '', seed: hash32(f) }
  }

  // ── Публічний API ────────────────────────────────────────────────────────
  export function push(p: ExportProgress) {
    if (finale) return
    const si = STAGE_ORDER.indexOf(p.stage)
    if (si >= 0) {
      // Новий прохід (мрпак → shaurmpack): етап повернувся назад після 'done'.
      if (afterDone && p.stage !== 'done') { round = Math.min(round + 1, Math.max(rounds - 1, 0)); afterDone = false; lastStageIdx = -1 }
      if (si > lastStageIdx) lastStageIdx = si
      if (p.stage === 'done') afterDone = true
      const [base, span] = WEIGHTS[p.stage] ?? [1, 0]
      let frac = 0
      if (p.stage === 'done') frac = 1
      else if (p.total > 0) frac = Math.min(1, p.current / p.total)
      const inRound = p.stage === 'done' ? 1 : Math.min(1, base + span * frac)
      const overall = Math.min(1, (round + inRound) / Math.max(rounds, 1))
      targetPct = Math.max(targetPct, overall)
      if (!stagesSeen[p.stage]) stagesSeen = { ...stagesSeen, [p.stage]: true }
      hudStage = p.stage
    }
    if (!p.fileName) return
    const it = classify(p)
    if (!it) return
    if (it.kind === 'dust') {
      counts.files++
      if (queue.length < 200) dustBudget++
      hudLabel = it.name
      return
    }
    if (seen.has(it.key)) {
      // Той самий файл вдруге (hash → resolve): лише оновлюємо підпис.
      if (p.modName) hudLabel = p.modName
      return
    }
    seen.add(it.key)
    counts[it.kind]++
    counts.files++
    hudLabel = it.name
    if (queue.length < 800) queue.push(it)
  }

  export function finish(): Promise<void> {
    if (finale) return Promise.resolve()
    targetPct = 1
    finale = true
    finaleT = 0
    hudDone = true
    hudPct = 100
    return new Promise<void>((res) => {
      finaleResolve = res
      window.setTimeout(() => { finaleResolve?.(); finaleResolve = null }, reduced ? 400 : 2500)
    })
  }

  // ── Геометрія (в пікселях полотна) ───────────────────────────────────────
  function geo() {
    const tile = Math.max(30, Math.min(40, H * 0.105))
    const by = H * 0.5                              // лінія стрічки (центри іконок)
    const s = Math.min(W * 0.085, H * 0.17)         // півширина пакунка
    const cx = W * 0.865
    const cy = by + s * 0.55                        // верх пакунка — на рівні стрічки
    const mw = Math.max(110, Math.min(176, W * 0.21))
    const mh = Math.min(H * 0.66, 196)
    const mx = W * 0.46
    const hx = W * 0.105
    return { tile, by, s, cx, cy, mw, mh, mx, hx, mx0: mx - mw / 2, mx1: mx + mw / 2, ch: tile + 18, xd: cx - s * 0.95, lane: Math.min(H - 16, by + mh / 2 + 16) }
  }
  type Geo = ReturnType<typeof geo>

  function addPart(p: Part) { if (parts.length < MAX_PARTS) parts.push(p) }

  function sparks(x: number, y: number, n: number, hue: number, spread = 140) {
    if (reduced) n = Math.ceil(n / 3)
    for (let i = 0; i < n; i++) {
      const a = Math.random() * Math.PI * 2
      const v = 40 + Math.random() * spread
      addPart({ x, y, vx: Math.cos(a) * v, vy: Math.sin(a) * v - 40, life: 0, max: 0.4 + Math.random() * 0.5, r: 1.2 + Math.random() * 2, hue, add: true, g: 220 })
    }
  }

  // Іконка дісталась пакунка: відбій, «+1», і в чергу на конфеті фіналу.
  function arrive(it: Item, g: Geo, quiet = false) {
    bump = Math.min(1.2, bump + (quiet ? 0.18 : 0.5))
    glow = Math.min(1, glow + (quiet ? 0.08 : 0.25))
    if (!quiet) {
      sparks(g.cx, g.cy - g.s * 0.3, 6, KIND_HUE[it.kind])
      if (pops.length < 8) pops.push({ x: g.cx + (Math.random() - 0.5) * g.s * 0.8, y: g.cy - g.s * 0.7, t: 0, text: '+1' })
      recent.push(it)
      if (recent.length > RECENT) recent.shift()
    }
  }

  function spawnBelt(it: Item, g: Geo) {
    belt.push({ item: it, x: g.hx, drop: 0, stage: 0, ex: 0, dive: 0, hash: hashLabel(it.key) })
  }

  function startBurst(g: Geo) {
    burst = true
    const c = { x: W / 2, y: H * 0.5 }
    rings.push({ t: 0, max: 1.1 }, { t: -0.18, max: 1.3 }, { t: -0.36, max: 1.5 })
    glow = 1
    bump = 1.2
    const src: Item[] = [...recent, ...belt.map((b) => b.item)]
    for (const it of src) {
      const a = Math.random() * Math.PI * 2
      const v = 160 + Math.random() * 320
      addPart({
        x: c.x, y: c.y - 10,
        vx: Math.cos(a) * v, vy: Math.sin(a) * v - 220,
        life: 0, max: 1.6 + Math.random() * 0.8, r: 14 + Math.random() * 6, hue: 40, add: false,
        icon: it, rot: Math.random() * 6, vr: (Math.random() - 0.5) * 9, g: 520
      })
    }
    belt.length = 0
    lane.length = 0
    sparks(c.x, c.y, reduced ? 20 : 90, 45)
    void g
  }

  // ── Малювання ────────────────────────────────────────────────────────────
  function rr(c: CanvasRenderingContext2D, x: number, y: number, w: number, h: number, r: number) {
    c.beginPath()
    c.moveTo(x + r, y)
    c.arcTo(x + w, y, x + w, y + h, r)
    c.arcTo(x + w, y + h, x, y + h, r)
    c.arcTo(x, y + h, x, y, r)
    c.arcTo(x, y, x + w, y, r)
    c.closePath()
  }

  function tile(c: CanvasRenderingContext2D, it: Item, x: number, y: number, size: number, rot: number, alpha = 1, hashed = 0) {
    c.save()
    c.translate(x, y)
    c.rotate(rot)
    c.globalAlpha = alpha
    const h = size / 2
    const hue = (KIND_HUE[it.kind] + (it.seed % 40) - 20 + 360) % 360
    rr(c, -h, -h, size, size, size * 0.24)
    const g = c.createLinearGradient(-h, -h, h, h)
    g.addColorStop(0, `hsl(${hue} 70% 46%)`)
    g.addColorStop(1, `hsl(${(hue + 30) % 360} 65% 26%)`)
    c.fillStyle = g
    c.fill()
    const img = iconOf(it.url)
    c.save()
    c.clip()
    if (img) {
      c.drawImage(img, -h, -h, size, size)
    } else {
      c.fillStyle = 'rgba(255,255,255,.92)'
      c.font = `800 ${Math.round(size * 0.5)}px ui-sans-serif, system-ui, sans-serif`
      c.textAlign = 'center'
      c.textBaseline = 'middle'
      c.fillText((it.name.trim()[0] ?? '?').toUpperCase(), 0, size * 0.03)
    }
    // Хеш-ідентикон 5×5 (дзеркальний, з бітів seed) поверх іконки: «розклали на хеш».
    if (hashed > 0.01) {
      const cell = size / 5
      c.fillStyle = `rgba(${orangeRgb},${0.62 * hashed})`
      for (let r = 0; r < 5; r++) {
        for (let col = 0; col < 3; col++) {
          if (((it.seed >>> (r * 3 + col)) & 1) === 0) continue
          c.fillRect(-h + col * cell, -h + r * cell, cell - 0.5, cell - 0.5)
          if (col < 2) c.fillRect(-h + (4 - col) * cell, -h + r * cell, cell - 0.5, cell - 0.5)
        }
      }
      c.fillStyle = `rgba(0,0,0,${0.22 * hashed})`
      c.fillRect(-h, -h, size, size)
    }
    c.restore()
    rr(c, -h, -h, size, size, size * 0.24)
    c.lineWidth = Math.max(1, size * 0.045)
    c.strokeStyle = hashed > 0.5 ? `rgba(${orangeRgb},.95)` : 'rgba(255,255,255,.35)'
    c.stroke()
    c.restore()
  }

  function chip(c: CanvasRenderingContext2D, text: string, x: number, y: number, alpha: number) {
    c.save()
    c.globalAlpha = alpha
    c.font = '700 9px "JetBrains Mono", Consolas, monospace'
    c.textAlign = 'center'
    c.textBaseline = 'middle'
    const w = c.measureText(text).width + 10
    rr(c, x - w / 2, y - 7, w, 14, 7)
    c.fillStyle = 'rgba(8,6,2,.78)'
    c.fill()
    c.lineWidth = 1
    c.strokeStyle = `rgba(${orangeRgb},.8)`
    c.stroke()
    c.fillStyle = '#ffe0a0'
    c.fillText(text, x, y + 0.5)
    c.restore()
  }

  function drawCrate(c: CanvasRenderingContext2D, cx: number, cy: number, s: number, lid: number) {
    // Ізометрична скриня: s — півширина. lid 0 = відкрита, 1 = закрита.
    const sc = 1 + bump * 0.07
    c.save()
    c.translate(cx, cy)
    c.scale(sc, sc)
    const top = -s * 0.15
    const h = s * 0.95
    const hw = s
    const hh = s * 0.5
    // ліва і права грані
    const left = c.createLinearGradient(-hw, top, 0, top + h)
    left.addColorStop(0, `rgb(${orangeRgb})`)
    left.addColorStop(1, 'rgba(120,52,0,1)')
    c.fillStyle = left
    c.beginPath(); c.moveTo(-hw, top); c.lineTo(0, top + hh); c.lineTo(0, top + hh + h); c.lineTo(-hw, top + h); c.closePath(); c.fill()
    const right = c.createLinearGradient(hw, top, 0, top + h)
    right.addColorStop(0, 'rgba(255,190,110,1)')
    right.addColorStop(1, 'rgba(170,80,0,1)')
    c.fillStyle = right
    c.beginPath(); c.moveTo(hw, top); c.lineTo(0, top + hh); c.lineTo(0, top + hh + h); c.lineTo(hw, top + h); c.closePath(); c.fill()
    // планки
    c.strokeStyle = 'rgba(60,24,0,.55)'
    c.lineWidth = Math.max(1.5, s * 0.035)
    for (const k of [0.33, 0.66]) {
      c.beginPath(); c.moveTo(-hw, top + h * k); c.lineTo(0, top + hh + h * k); c.lineTo(hw, top + h * k); c.stroke()
    }
    c.strokeStyle = 'rgba(255,255,255,.22)'
    c.beginPath(); c.moveTo(0, top + hh); c.lineTo(0, top + hh + h); c.stroke()
    // внутрішність
    const lift = (1 - lid) * s * 0.55
    c.fillStyle = `rgba(20,8,0,1)`
    c.beginPath(); c.moveTo(-hw, top); c.lineTo(0, top - hh); c.lineTo(hw, top); c.lineTo(0, top + hh); c.closePath(); c.fill()
    if (glow > 0.01) {
      const gg = c.createRadialGradient(0, top, 0, 0, top, s * 1.1)
      gg.addColorStop(0, `rgba(255,230,160,${0.9 * glow})`)
      gg.addColorStop(1, `rgba(${orangeRgb},0)`)
      c.fillStyle = gg
      c.beginPath(); c.moveTo(-hw, top); c.lineTo(0, top - hh); c.lineTo(hw, top); c.lineTo(0, top + hh); c.closePath(); c.fill()
    }
    // кришка (піднята/відкинута, поки йде експорт)
    c.save()
    c.translate(0, -lift - s * 0.02)
    c.globalAlpha = lid > 0.98 ? 1 : 0.0 + lid
    const lg = c.createLinearGradient(-hw, top - hh, hw, top + hh)
    lg.addColorStop(0, 'rgba(255,215,150,1)')
    lg.addColorStop(1, `rgb(${orangeRgb})`)
    c.fillStyle = lg
    c.beginPath(); c.moveTo(-hw, top); c.lineTo(0, top - hh); c.lineTo(hw, top); c.lineTo(0, top + hh); c.closePath(); c.fill()
    c.strokeStyle = 'rgba(255,255,255,.4)'
    c.lineWidth = 1.5
    c.stroke()
    c.restore()
    c.restore()
  }

  function drawBelt(c: CanvasRenderingContext2D, g: Geo) {
    const sy = g.by + g.tile / 2 + 6
    const sx = g.hx - 46
    const ex = g.cx - g.s * 0.2
    // основа стрічки
    rr(c, sx, sy, ex - sx, 7, 3.5)
    c.fillStyle = 'rgba(255,255,255,.07)'
    c.fill()
    // підсвічена (пройдена) частина — це і є прогрес
    const lit = (ex - sx) * Math.min(1, ringPct)
    if (lit > 3) {
      const lg = c.createLinearGradient(sx, 0, ex, 0)
      lg.addColorStop(0, `rgba(${orangeRgb},.95)`)
      lg.addColorStop(1, 'rgba(255,224,160,.95)')
      c.save()
      c.shadowColor = `rgba(${orangeRgb},.7)`
      c.shadowBlur = 10
      rr(c, sx, sy, lit, 7, 3.5)
      c.fillStyle = lg
      c.fill()
      c.restore()
    }
    // рухомі риски роликів
    c.fillStyle = 'rgba(0,0,0,.32)'
    const off = beltPhase % 12
    for (let xx = sx - off + 12; xx < ex; xx += 12) c.fillRect(xx, sy + 1, 1.6, 5)

    // обхідна доріжка для конфігів/інших файлів: під конвертором, у пакунок
    const ly = g.lane
    c.save()
    c.setLineDash([3, 7])
    c.lineDashOffset = -beltPhase * 0.6
    c.strokeStyle = 'rgba(255,255,255,.16)'
    c.lineWidth = 2
    c.lineCap = 'round'
    c.beginPath(); c.moveTo(sx, ly); c.lineTo(g.cx - g.s * 0.98, ly); c.stroke()
    c.restore()
  }

  function drawHopper(c: CanvasRenderingContext2D, g: Geo) {
    const topY = g.by - g.tile - 34
    const botY = g.by - g.tile / 2 - 5
    const tw = 74, bw = Math.max(g.tile + 6, 40)
    c.beginPath()
    c.moveTo(g.hx - tw / 2, topY); c.lineTo(g.hx + tw / 2, topY)
    c.lineTo(g.hx + bw / 2, botY); c.lineTo(g.hx - bw / 2, botY); c.closePath()
    const hg = c.createLinearGradient(0, topY, 0, botY)
    hg.addColorStop(0, theme.card); hg.addColorStop(1, theme.panel)
    c.fillStyle = hg
    c.fill()
    c.lineWidth = 1.5
    c.strokeStyle = theme.edge
    c.stroke()
    c.fillStyle = `rgba(${orangeRgb},.5)`
    c.fillRect(g.hx - tw / 2 - 2, topY - 3, tw + 4, 4)
    c.font = '700 10px ui-sans-serif, system-ui, sans-serif'
    c.textAlign = 'center'
    c.textBaseline = 'alphabetic'
    c.fillStyle = theme.mute
    c.fillText(t('export.forge.src').toUpperCase(), g.hx, topY - 11)
  }

  function drawMachine(c: CanvasRenderingContext2D, g: Geo, ts: number) {
    const x0 = g.mx0, w = g.mw, h = g.mh, y0 = g.by - h / 2 - 4
    c.save()
    // корпус
    c.shadowColor = `rgba(${orangeRgb},${0.15 + busy * 0.55})`
    c.shadowBlur = 16 + busy * 26
    rr(c, x0, y0, w, h, 16)
    const bg = c.createLinearGradient(0, y0, 0, y0 + h)
    bg.addColorStop(0, theme.card); bg.addColorStop(1, theme.panel)
    c.fillStyle = bg
    c.fill()
    c.shadowBlur = 0
    c.lineWidth = 2
    c.strokeStyle = `rgba(${orangeRgb},${0.35 + busy * 0.5})`
    c.stroke()
    // символ хеша зверху
    c.fillStyle = `rgba(${orangeRgb},${0.75 + busy * 0.25})`
    c.shadowColor = `rgba(${orangeRgb},.8)`
    c.shadowBlur = 6 + busy * 16
    c.font = `800 ${Math.round(h * 0.2)}px "JetBrains Mono", Consolas, monospace`
    c.textAlign = 'center'
    c.textBaseline = 'middle'
    c.fillText('#', g.mx, y0 + h * 0.19)
    c.shadowBlur = 0
    // індикаторна лампа
    c.beginPath(); c.arc(x0 + w - 14, y0 + 14, 4, 0, Math.PI * 2)
    c.fillStyle = busy > 0.15 ? `rgba(${orangeRgb},${0.55 + 0.45 * Math.sin(ts / 110)})` : 'rgba(255,255,255,.15)'
    c.fill()
    // вентиляційні прорізі й підпис знизу
    c.fillStyle = 'rgba(0,0,0,.35)'
    for (let i = 0; i < 4; i++) c.fillRect(x0 + 16 + i * 9, y0 + h - 46, 5, 12)
    c.font = '700 10px ui-sans-serif, system-ui, sans-serif'
    c.fillStyle = theme.mute
    c.textBaseline = 'alphabetic'
    c.fillText(t('export.forge.conv').toUpperCase(), g.mx, y0 + h - 12)
    // тунель-віконце крізь корпус
    rr(c, x0 - 1, g.by - g.ch / 2, w + 2, g.ch, 8)
    c.fillStyle = '#07050a'
    c.fill()
    c.lineWidth = 1.5
    c.strokeStyle = `rgba(${orangeRgb},.55)`
    c.stroke()
    // риски сканування по дну тунелю
    c.fillStyle = `rgba(${orangeRgb},.08)`
    for (let yy = g.by - g.ch / 2 + 3; yy < g.by + g.ch / 2; yy += 4) c.fillRect(x0 + 2, yy, w - 4, 1)
    c.restore()
  }

  // Стадія (0/1/2) і ступінь «захешованості» p 0..1 за позицією на стрічці.
  const hashP = (x: number, g: Geo) => Math.max(0, Math.min(1, (x - (g.mx0 + g.mw * 0.12)) / (g.mw * 0.76)))

  function beltPos(b: Belt, g: Geo) {
    if (b.dive > 0) {
      const u = Math.min(1, b.dive)
      const e = u * u * (3 - 2 * u)
      const ty = g.cy - g.s * 0.15 + 4
      const x = g.xd + (g.cx - g.xd) * u
      const y = g.by + (ty - g.by) * e - Math.sin(u * Math.PI) * 14
      return { x, y, size: g.tile * (1 - 0.5 * e), alpha: u > 0.7 ? 1 - (u - 0.7) / 0.3 : 1 }
    }
    const d = Math.min(1, b.drop)
    const ed = 1 - (1 - d) * (1 - d)
    const fall = (1 - ed) * (g.tile + 22)
    return { x: b.x, y: g.by - fall, size: g.tile * (0.55 + 0.45 * ed), alpha: Math.min(1, d * 3) }
  }

  function drawBeltItem(c: CanvasRenderingContext2D, b: Belt, g: Geo) {
    const { x, y, size, alpha } = beltPos(b, g)
    const p = b.stage === 2 ? 1 : b.stage === 1 ? hashP(x, g) : 0
    // «вискакує» з конвертора: коротке перерегулювання розміру
    const pop = b.stage === 2 && b.ex < 0.4 ? 1 + 0.22 * Math.sin((b.ex / 0.4) * Math.PI) : 1
    const draw = () => {
      tile(c, b.item, x, y, size * pop, 0, alpha, p)
      if (p > 0.85 && b.dive === 0) chip(c, b.hash, x, y + size / 2 + 12, Math.min(1, (p - 0.85) / 0.15) * alpha)
    }
    const overlaps = b.dive === 0 && x + size / 2 > g.mx0 && x - size / 2 < g.mx1
    if (!overlaps) { draw(); return }
    // Перетин корпусу: частина ззовні — як є, частина в межах корпусу — лише в тунелі.
    c.save()
    c.beginPath()
    c.rect(-1e4, -1e4, 2e4, 2e4)
    c.rect(g.mx0, g.by - g.mh / 2 - 4, g.mw, g.mh)
    c.clip('evenodd')
    draw()
    c.restore()
    c.save()
    c.beginPath()
    c.rect(g.mx0, g.by - g.ch / 2 + 1, g.mw, g.ch - 2)
    c.clip()
    draw()
    c.restore()
  }

  function frame(ts: number) {
    raf = requestAnimationFrame(frame)
    if (!ctx || !canvas || W === 0) return
    const dt = Math.min(0.05, lastTs ? (ts - lastTs) / 1000 : 0.016)
    lastTs = ts
    const c = ctx
    const g = geo()

    // ── логіка ──
    beltSpeed = 160 + Math.min(queue.length * 6, 520)
    if (!finale) {
      if (reduced) {
        // Без руху: просто рахуємо надходження й легенько «пульсуємо» пакунком.
        spawnAcc += dt * 12
        while (spawnAcc >= 1 && queue.length > 0) { spawnAcc -= 1; arrive(queue.shift()!, g, true) }
      } else {
        // На стрічку — лише коли попередня іконка вже від'їхала від лійки.
        const tail = belt[belt.length - 1]
        if (queue.length > 0 && belt.length < MAX_BELT && (!tail || tail.x > g.hx + g.tile + 16)) spawnBelt(queue.shift()!, g)
        // Обхідна доріжка: конфіги та інші файли, без хешування.
        laneAcc += dt * 7
        while (laneAcc >= 1 && dustBudget > 0 && lane.length < MAX_LANE) { laneAcc -= 1; dustBudget--; lane.push({ x: g.hx - 46, hue: 36 + Math.random() * 16 }) }
        if (dustBudget <= 0) laneAcc = Math.min(laneAcc, 1)
      }
    }

    let inside = 0
    if (!finale) {
      beltPhase += beltSpeed * dt
      for (let i = belt.length - 1; i >= 0; i--) {
        const b = belt[i]
        if (b.drop < 1) b.drop = Math.min(1, b.drop + dt / 0.28)
        if (b.dive > 0) {
          b.dive += dt / 0.36
          if (b.dive >= 1) { arrive(b.item, g); belt.splice(i, 1) }
          continue
        }
        b.x += beltSpeed * dt
        const st = b.x < g.mx0 ? 0 : b.x < g.mx1 ? 1 : 2
        if (st === 1) inside++
        if (st === 2 && b.stage !== 2) {
          // Вийшов із конвертора вже захешованим: спалах і іскри на виході.
          sparks(g.mx1, g.by, 7, 42, 110)
          glow = Math.max(glow, 0.15)
          b.ex = 0
        }
        b.stage = st as 0 | 1 | 2
        if (b.stage === 2) b.ex += dt
        if (b.x >= g.xd) b.dive = 0.0001
      }
      for (let i = lane.length - 1; i >= 0; i--) {
        const ch = lane[i]
        ch.x += 250 * dt
        if (ch.x >= g.cx - g.s * 0.98) { arrive({ key: '', kind: 'dust', name: '', url: '', seed: 0 }, g, true); lane.splice(i, 1) }
      }
    }
    busy += ((inside > 0 ? 1 : 0) - busy) * Math.min(1, dt * 8)
    for (let i = pops.length - 1; i >= 0; i--) { pops[i].t += dt; if (pops[i].t > 0.9) pops.splice(i, 1) }
    for (let i = parts.length - 1; i >= 0; i--) {
      const p = parts[i]
      p.life += dt
      if (p.life >= p.max) { parts.splice(i, 1); continue }
      if (p.g) p.vy += p.g * dt
      p.x += p.vx * dt
      p.y += p.vy * dt
      if (p.rot !== undefined && p.vr) p.rot += p.vr * dt
    }
    for (let i = rings.length - 1; i >= 0; i--) {
      rings[i].t += dt
      if (rings[i].t >= rings[i].max) rings.splice(i, 1)
    }
    bump = bump > 0.001 ? bump * Math.pow(0.0009, dt) : 0
    glow = glow > 0.001 ? glow * Math.pow(0.12, dt) : 0
    if (!finale && belt.some((b) => b.dive > 0)) glow = Math.max(glow, 0.22)
    ringPct += (targetPct - ringPct) * Math.min(1, dt * 5)
    if (finale) {
      finaleT += dt
      if (!burst && finaleT > 0.55) startBurst(g)
    }

    // ── малювання ──
    c.setTransform(dpr, 0, 0, dpr, 0, 0)
    c.clearRect(0, 0, W, H)

    // фінал: пакунок виїжджає в центр і росте, решта сцени гасне
    const fin = finale ? Math.min(1, finaleT / 0.55) : 0
    const fe = 1 - Math.pow(1 - fin, 3)
    const crateX = g.cx + (W / 2 - g.cx) * fe
    const crateY = g.cy + (H * 0.5 + 6 - g.cy) * fe
    const crateS = g.s * (1 + 0.55 * fe)

    // ореол
    const halo = c.createRadialGradient(crateX, crateY, 0, crateX, crateY, Math.max(W, H) * 0.55)
    halo.addColorStop(0, `rgba(${orangeRgb},${0.1 + glow * 0.2})`)
    halo.addColorStop(1, `rgba(${orangeRgb},0)`)
    c.fillStyle = halo
    c.fillRect(0, 0, W, H)

    // машина і стрічка (гаснуть у фіналі)
    c.save()
    c.globalAlpha = 1 - 0.92 * fe
    drawBelt(c, g)
    drawHopper(c, g)
    drawMachine(c, g, ts)
    c.restore()

    // обхідні «чіпи» конфігів
    if (!finale) {
      for (const ch of lane) {
        const a = Math.min(1, (ch.x - (g.hx - 46)) / 24)
        c.fillStyle = `hsla(${ch.hue} 90% 62% / ${a})`
        rr(c, ch.x - 3, g.lane - 3, 6, 6, 1.6)
        c.fill()
      }
    }

    // підписи під пакунком
    c.font = '700 10px ui-sans-serif, system-ui, sans-serif'
    c.textAlign = 'center'
    c.textBaseline = 'alphabetic'
    c.fillStyle = theme.mute
    c.globalAlpha = 1 - fe
    c.fillText(t('export.forge.out').toUpperCase(), g.cx, g.cy + g.s * 1.3 + 16)
    c.fillText(t('export.forge.other').toUpperCase(), g.hx + 20, g.lane + 14)
    c.globalAlpha = 1

    // пакунок
    const lid = finale ? Math.min(1, Math.max(0, (finaleT - 0.35) * 4)) : 0
    drawCrate(c, crateX, crateY, crateS, lid)

    // іконки на стрічці, у тунелі та у падінні
    if (!finale) {
      // тунель: промінь сканера на кожній іконці всередині
      for (const b of belt) drawBeltItem(c, b, g)
      c.save()
      c.beginPath(); c.rect(g.mx0, g.by - g.ch / 2 + 1, g.mw, g.ch - 2); c.clip()
      for (const b of belt) {
        if (b.stage !== 1 || b.dive > 0) continue
        const sg = c.createLinearGradient(b.x - 10, 0, b.x + 10, 0)
        sg.addColorStop(0, `rgba(${orangeRgb},0)`); sg.addColorStop(0.5, `rgba(255,236,190,.85)`); sg.addColorStop(1, `rgba(${orangeRgb},0)`)
        c.fillStyle = sg
        c.fillRect(b.x - 10, g.by - g.ch / 2, 20, g.ch)
        // «потік» шістнадцяткових символів над іконкою
        c.font = '700 8px "JetBrains Mono", Consolas, monospace'
        c.textAlign = 'center'
        c.fillStyle = `rgba(${orangeRgb},.7)`
        const hx = ((b.item.seed >>> 3) + Math.floor(ts / 70)).toString(16).slice(-3)
        c.fillText(hx, b.x, g.by - g.ch / 2 + 9)
      }
      c.restore()
    }

    // шлейфи і частинки (адитивні)
    c.globalCompositeOperation = 'lighter'
    for (const p of parts) {
      if (!p.add) continue
      const k = 1 - p.life / p.max
      c.fillStyle = `hsla(${p.hue} 100% 62% / ${k * 0.9})`
      c.beginPath(); c.arc(p.x, p.y, p.r * (0.4 + k), 0, Math.PI * 2); c.fill()
    }
    c.globalCompositeOperation = 'source-over'

    // «+1» над пакунком
    c.font = '800 12px ui-sans-serif, system-ui, sans-serif'
    c.textAlign = 'center'
    for (const pp of pops) {
      const k = pp.t / 0.9
      c.fillStyle = `rgba(255,224,160,${1 - k})`
      c.fillText(pp.text, pp.x, pp.y - k * 26)
    }

    // конфеті-іконки
    for (const p of parts) {
      if (p.add || !p.icon) continue
      const k = 1 - p.life / p.max
      tile(c, p.icon, p.x, p.y, p.r * 2, p.rot ?? 0, Math.min(1, k * 2), 1)
    }

    // ударні хвилі
    const R = crateS * 1.75
    for (const r of rings) {
      if (r.t < 0) continue
      const k = r.t / r.max
      c.strokeStyle = `rgba(255,220,150,${(1 - k) * 0.7})`
      c.lineWidth = 3 + (1 - k) * 6
      c.beginPath(); c.arc(crateX, crateY - crateS * 0.1, R * (0.4 + k * 2.3), 0, Math.PI * 2); c.stroke()
    }

    // галочка
    if (finale) {
      const k = Math.min(1, Math.max(0, (finaleT - 0.62) * 2.6))
      if (k > 0) {
        const sz = crateS * 0.9
        const p0 = [-0.45, 0.02], p1 = [-0.1, 0.36], p2 = [0.5, -0.32]
        c.save()
        c.translate(crateX, crateY + crateS * 0.08)
        c.lineWidth = Math.max(7, crateS * 0.17)
        c.lineCap = 'round'; c.lineJoin = 'round'
        c.strokeStyle = '#fff'
        c.shadowColor = 'rgba(255,200,100,.9)'; c.shadowBlur = 18
        c.beginPath()
        c.moveTo(p0[0] * sz, p0[1] * sz)
        const seg1 = Math.min(1, k * 2)
        c.lineTo((p0[0] + (p1[0] - p0[0]) * seg1) * sz, (p0[1] + (p1[1] - p0[1]) * seg1) * sz)
        if (k > 0.5) {
          const seg2 = (k - 0.5) * 2
          c.lineTo((p1[0] + (p2[0] - p1[0]) * seg2) * sz, (p1[1] + (p2[1] - p1[1]) * seg2) * sz)
        }
        c.stroke()
        c.restore()
      }
    }

    // HUD — рідко
    if (ts - hudTick > 120) {
      hudTick = ts
      if (!finale) hudPct = Math.min(99, Math.round(ringPct * 100))
      hudCounts = { ...counts }
      hudSeconds = Math.floor((performance.now() - startedAt) / 1000)
    }
  }

  function resize() {
    if (!canvas) return
    const r = canvas.getBoundingClientRect()
    W = r.width
    H = r.height
    dpr = Math.min(2, window.devicePixelRatio || 1)
    canvas.width = Math.round(W * dpr)
    canvas.height = Math.round(H * dpr)
    ctx = canvas.getContext('2d')
  }

  // Кольори поточної теми для полотна (CSS-змінні в canvas напряму не працюють).
  function readTheme() {
    const cs = getComputedStyle(document.documentElement)
    const v = (n: string, d: string) => cs.getPropertyValue(n).trim() || d
    theme = { card: v('--bg-card', theme.card), panel: v('--bg-panel', theme.panel), edge: v('--border-hi', theme.edge), text: v('--text', theme.text), mute: v('--text-mute', theme.mute) }
  }

  onMount(() => {
    reduced = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false
    const v = getComputedStyle(document.documentElement).getPropertyValue('--orange-rgb').trim()
    if (v) orangeRgb = v
    readTheme()
    startedAt = performance.now()
    resize()
    const ro = new ResizeObserver(resize)
    if (canvas) ro.observe(canvas)
    raf = requestAnimationFrame(frame)
    // Базовий URL локального сервера іконок: той самий loopback, інший маршрут.
    App.GetModIconBase().then((b: string) => {
      if (typeof b === 'string' && b) iconBase = b.replace('/modicon/', '/packicon/')
    }).catch(() => {}).finally(() => onready())
    return () => ro.disconnect()
  })

  onDestroy(() => {
    cancelAnimationFrame(raf)
    finaleResolve?.()
    imgs.clear()
  })

  function stepState(id: string): 'done' | 'active' | 'todo' {
    if (hudDone) return 'done'
    const idx = STAGE_ORDER.indexOf(id)
    const cur = STAGE_ORDER.indexOf(hudStage)
    if (cur < 0) return 'todo'
    if (idx < cur) return 'done'
    return idx === cur ? 'active' : 'todo'
  }

  function fmtTime(s: number) {
    return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`
  }

  const stageText = $derived.by(() => {
    if (hudDone) return t('export.forge.done')
    switch (hudStage) {
      case 'mod_sources': return t('export.stage.modSources')
      case 'scan': return t('export.stage.scan')
      case 'hash': return t('export.stage.hash')
      case 'resolve': return t('export.stage.resolve')
      case 'pack': return t('export.stage.pack')
      default: return t('export.forge.starting')
    }
  })
</script>

<Portal>
  <!-- Оболонка й шапка — ті самі, що в UiModal (форма експорту, міграція тощо),
       щоб вікно прогресу не вибивалось зі стилю лаунчера. -->
  <div class="xf-overlay" role="dialog" aria-modal="true" aria-label={t('export.title')}>
    <div class="xf-card" class:xf-done={hudDone}>
      <div class="xf-head">
        <div class="xf-ico"><i class="ti {hudDone ? 'ti-circle-check' : 'ti-package-export'}"></i></div>
        <div class="xf-title">
          <b>{t('export.title')}</b>
          <span>{packName}</span>
        </div>
        <div class="xf-time"><i class="ti ti-clock"></i>{fmtTime(hudSeconds)}</div>
      </div>

      <div class="xf-body">
        <div class="xf-stagebox">
          <canvas bind:this={canvas} class="xf-canvas"></canvas>
        </div>

        <div class="xf-status">
          <div class="xf-stage">{stageText}</div>
          {#if !hudDone}<span class="xf-pctnum">{hudPct}%</span>{/if}
        </div>
        <div class="xf-bar" class:done={hudDone}><span style="width:{hudDone ? 100 : hudPct}%"></span></div>

        <div class="xf-steps">
          {#each STEPS as s (s.id)}
            {#if s.id === 'pack' || s.id === 'scan' || stagesSeen[s.id]}
              <span class="xf-step {stepState(s.id)}"><i class="ti {stepState(s.id) === 'done' ? 'ti-check' : 'ti-point-filled'}"></i>{t(s.key)}</span>
            {/if}
          {/each}
        </div>

        <div class="xf-stats">
          <div class="xf-stat"><i class="ti ti-puzzle"></i><div><b>{hudCounts.mod}</b><small>{t('export.forge.mods')}</small></div></div>
          <div class="xf-stat"><i class="ti ti-photo"></i><div><b>{hudCounts.pack}</b><small>{t('export.forge.packs')}</small></div></div>
          {#if hudCounts.shader > 0}<div class="xf-stat"><i class="ti ti-sun"></i><div><b>{hudCounts.shader}</b><small>{t('export.forge.shaders')}</small></div></div>{/if}
          {#if hudCounts.data > 0}<div class="xf-stat"><i class="ti ti-database"></i><div><b>{hudCounts.data}</b><small>{t('export.forge.data')}</small></div></div>{/if}
          <div class="xf-stat"><i class="ti ti-files"></i><div><b>{hudCounts.files}</b><small>{t('export.forge.files')}</small></div></div>
        </div>
      </div>

      <div class="xf-foot">
        <i class="ti {hudDone ? 'ti-check' : 'ti-file'}"></i>
        <span class="xf-file" use:launcherTip={hudLabel}>{hudLabel || '…'}</span>
      </div>
    </div>
  </div>
</Portal>

<style>
  /* Оболонка = UiModal: темний оверлей з блюром, панель --bg-panel, рамка --border-hi. */
  .xf-overlay {
    position: fixed; inset: 0; z-index: 2100000; padding: var(--modal-gap, 30px);
    display: flex; align-items: center; justify-content: center;
    background: rgba(0, 0, 0, .65); backdrop-filter: blur(6px);
    animation: xf-in .22s ease-out both;
  }
  @keyframes xf-in { from { opacity: 0; } to { opacity: 1; } }
  .xf-card {
    width: min(860px, 94vw); max-height: 100%;
    display: flex; flex-direction: column; overflow: hidden;
    background: var(--bg-panel); border: 1px solid var(--border-hi); border-radius: var(--r-lg);
    box-shadow: 0 30px 60px rgba(0, 0, 0, .7);
    animation: xf-pop .26s cubic-bezier(.22, 1.2, .36, 1) both;
    transition: border-color .4s;
  }
  .xf-card.xf-done { border-color: rgba(var(--orange-rgb), .5); }
  @keyframes xf-pop { from { opacity: 0; transform: translateY(14px) scale(.965); filter: blur(3px); } to { opacity: 1; transform: none; filter: none; } }

  .xf-head { display: flex; align-items: center; gap: 11px; padding: 18px 20px 12px; border-bottom: 1px solid var(--border); flex: none; }
  .xf-ico {
    width: 38px; height: 38px; border-radius: 10px; flex: none; display: flex; align-items: center; justify-content: center;
    background: color-mix(in srgb, var(--orange) 16%, transparent); color: var(--orange); font-size: 19px; transition: .3s;
  }
  .xf-ico :global(.ti) { display: block; line-height: 1; transform: translateY(1px); }
  .xf-card.xf-done .xf-ico { background: rgba(16, 185, 129, .16); color: var(--green); }
  .xf-title { display: flex; flex-direction: column; gap: 2px; min-width: 0; flex: 1; }
  .xf-title b { font-size: 15px; font-weight: 800; color: var(--text); }
  .xf-title span { font-size: 11.5px; color: var(--text-mute); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .xf-time {
    display: inline-flex; align-items: center; gap: 5px; padding: 5px 10px; border-radius: 999px;
    font: 700 11.5px var(--font-mono, monospace); color: var(--text-mute); background: var(--bg-input); border: 1px solid var(--border);
  }
  .xf-time i { font-size: 13px; }

  .xf-body { flex: 1; min-height: 0; overflow-y: auto; padding: 16px 20px; display: flex; flex-direction: column; gap: 12px; }
  .xf-body > * { flex-shrink: 0; }

  /* Сцена — у рамці-«вітрині», як інші блоки лаунчера; фон із ледь помітною сіткою. */
  .xf-stagebox {
    position: relative; border-radius: var(--r-md); overflow: hidden;
    background: var(--bg-input); border: 1px solid var(--border);
  }
  .xf-stagebox::before {
    content: ''; position: absolute; inset: 0; pointer-events: none; opacity: .55;
    background-image: linear-gradient(var(--border) 1px, transparent 1px), linear-gradient(90deg, var(--border) 1px, transparent 1px);
    background-size: 26px 26px;
    -webkit-mask-image: radial-gradient(ellipse at center, #000 30%, transparent 85%); mask-image: radial-gradient(ellipse at center, #000 30%, transparent 85%);
  }
  .xf-canvas { position: relative; display: block; width: 100%; height: min(330px, 42vh); }

  .xf-status { display: flex; align-items: baseline; justify-content: space-between; gap: 12px; }
  .xf-stage { font-size: 14px; font-weight: 800; color: var(--text); }
  .xf-pctnum { color: var(--orange); font: 800 14px var(--font-mono, monospace); font-variant-numeric: tabular-nums; }
  .xf-bar { height: 6px; border-radius: 3px; background: var(--bg-input); border: 1px solid var(--border); overflow: hidden; margin-top: -4px; }
  .xf-bar span { display: block; height: 100%; border-radius: 3px; background: var(--accent-grad, var(--orange)); transition: width .25s ease-out; }
  .xf-bar.done span { background: var(--green); }

  .xf-steps { display: flex; flex-wrap: wrap; gap: 6px; }
  .xf-step {
    display: inline-flex; align-items: center; gap: 5px; height: 26px; padding: 0 11px; border-radius: 999px;
    font: 600 11.5px var(--font-ui, sans-serif); color: var(--text-mute);
    border: 1px solid var(--border); background: var(--bg-input); transition: all .25s ease;
  }
  .xf-step i { font-size: 11px; }
  .xf-step.active { color: var(--orange); border-color: rgba(var(--orange-rgb), .5); background: rgba(var(--orange-rgb), .12); }
  .xf-step.active i { animation: xf-blink .9s ease-in-out infinite alternate; }
  .xf-step.done { color: var(--text); border-color: var(--border-hi); }
  .xf-step.done i { color: var(--green); }
  @keyframes xf-blink { from { opacity: .35; } to { opacity: 1; } }

  /* Лічильники — плитки, як «Система / Архітектура» в «Про лаунчер». */
  .xf-stats { display: grid; grid-template-columns: repeat(auto-fit, minmax(120px, 1fr)); gap: 8px; }
  .xf-stat { display: flex; align-items: center; gap: 10px; padding: 9px 12px; border-radius: var(--r-md); background: var(--bg-card); border: 1px solid var(--border); }
  .xf-stat > i { width: 30px; height: 30px; border-radius: 9px; display: grid; place-items: center; font-size: 16px; color: var(--orange); background: rgba(var(--orange-rgb), .13); flex: none; }
  .xf-stat div { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .xf-stat b { font: 800 15px/1 var(--font-ui, sans-serif); color: var(--text); font-variant-numeric: tabular-nums; }
  .xf-stat small { font: 600 10.5px/1 var(--font-ui, sans-serif); color: var(--text-dim); white-space: nowrap; }

  .xf-foot { display: flex; align-items: center; gap: 8px; padding: 12px 20px; border-top: 1px solid var(--border); color: var(--text-dim); flex: none; }
  .xf-foot i { font-size: 15px; }
  .xf-card.xf-done .xf-foot { color: var(--green); }
  .xf-file { min-width: 0; font: 600 12px var(--font-mono, monospace); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

  @media (prefers-reduced-motion: reduce) {
    .xf-overlay, .xf-card { animation-duration: .01s; }
    .xf-step.active i { animation: none; }
  }
</style>
