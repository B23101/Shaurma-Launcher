// ── Wardrobe FX: блиск, спалах, світлячки, блик на 3D-моделі гравця ──────
// Окремий модуль, який підключається до wardrobe3d.ts (див. кінець файлу).
// Ідея візуалу — «студійний» вигляд моделі: м'який відблиск від IBL-карти,
// спалах і оберт при зміні скіна, світлячки навколо тіла, хвиля-кільце на
// підлозі, блик, що час від часу пробігає по моделі, та повільні пилинки.
//
// Код написаний з нуля; запозичені лише загальні ідеї та числові параметри
// вигляду (roughness/metalness, тривалості, кількість частинок).
//
// Принципи:
//  • Працює з прозорим фоном (alpha: true) — тому БЕЗ постпроцесингу/bloom.
//    «Світіння» робимо emissive-спалахом і additive-спрайтами.
//  • Нічого не алокує щокадру: частинки в пулі, матеріали/текстури спільні.
//  • Performance Mode (<html data-performance-mode="on"> або
//    data-animations="off") вимикає частинки/блик/оберт; статичний блиск
//    (applyShine) лишається, бо він майже безкоштовний.
//  • Ніяких ефектів, поки вікно неактивне, — update() викликається з
//    tick() лише на активних кадрах, тож сам модуль нічого не крутить.

import * as THREE from 'three'
import { RoomEnvironment } from 'three/addons/environments/RoomEnvironment.js'

// ── Параметри (підбирались на око, всі в одному місці) ───────────────────
// Скін — піксель-арт, а не метал: блиск має лише підкреслювати форму,
// а не «випалювати» кольори. Тому roughness вище, metalness майже нульовий,
// а IBL-відбиток м'який — інакше обличчя й одяг зливались у білу пляму.
const SHINE = {
  roughness: 0.62,
  metalness: 0.02,
  envIntensity: 0.42,
}

const DRESS = {
  duration: 1.25, // с — загальна тривалість ефекту
  spinFraction: 0.85, // частка часу, за яку добігає оберт
  spinTurns: 1, // кількість обертів
  flashFraction: 0.35, // частка часу, на яку припадає спалах
  flashPeak: 0.6, // пік emissiveIntensity
  flashColor: 0xd6e6ff,
  fireflies: 24,
  ringDuration: 0.7,
  ringFrom: 0.5,
  ringTo: 3.6,
}

const GLINT = {
  period: 6.5, // с між проходами
  sweep: 0.95, // с — тривалість одного проходу
  color: 0xfff6e6,
  // Було 9 — лампа заливала модель білим на кожному проході, і скін
  // неможливо було роздивитись. 2.2 дає делікатний відблиск.
  peak: 2.2,
  distance: 7,
  fromX: -2.2,
  toX: 2.2,
  y: 1.7,
  z: 1.6,
}

const MOTES = {
  count: 12,
  colors: [0xffffff, 0xe8eef8, 0xfff4e0, 0xd8e4ff],
}

// Висота моделі після fitModel() = 3.1 (ноги на y = 0).
const BODY_H = 3.1

// ── Допоміжне ────────────────────────────────────────────────────────────
const easeOutCubic = (t: number) => 1 - Math.pow(1 - t, 3)
const clamp01 = (t: number) => (t < 0 ? 0 : t > 1 ? 1 : t)
const rand = (a: number, b: number) => a + Math.random() * (b - a)

/** Ефекти вимкнені? (Performance Mode або вимкнені анімації). */
function effectsSuppressed(): boolean {
  const el = document.documentElement
  return el.getAttribute('data-performance-mode') === 'on' || el.getAttribute('data-animations') === 'off'
}

// Текстури плям і кільця — СВОЇ для кожного екземпляра ефектів і звільняються в dispose().
// Раніше вони були спільні на модуль і ніколи не звільнялись: three.js вішає на текстуру
// слухач dispose від кожного рендерера, що її використав, і через нього спільна текстура
// назавжди тримала кожен закритий переглядач гардероба разом із WebGL-контекстом.

/** М'яка кругла пляма для спрайтів. */
function makeSoftGlow(): THREE.CanvasTexture {
  const c = document.createElement('canvas')
  c.width = c.height = 64
  const g = c.getContext('2d')!
  const grad = g.createRadialGradient(32, 32, 0, 32, 32, 32)
  grad.addColorStop(0, 'rgba(255,255,255,1)')
  grad.addColorStop(0.25, 'rgba(255,255,255,0.55)')
  grad.addColorStop(1, 'rgba(255,255,255,0)')
  g.fillStyle = grad
  g.fillRect(0, 0, 64, 64)
  const tex = new THREE.CanvasTexture(c)
  tex.colorSpace = THREE.SRGBColorSpace
  return tex
}

/** Кільце-хвиля: прозорий центр, світлий обідок. */
function makeRingTex(): THREE.CanvasTexture {
  const c = document.createElement('canvas')
  c.width = c.height = 128
  const g = c.getContext('2d')!
  const grad = g.createRadialGradient(64, 64, 0, 64, 64, 64)
  grad.addColorStop(0, 'rgba(255,255,255,0)')
  grad.addColorStop(0.6, 'rgba(255,255,255,0.05)')
  grad.addColorStop(0.86, 'rgba(255,255,255,0.9)')
  grad.addColorStop(1, 'rgba(255,255,255,0)')
  g.fillStyle = grad
  g.fillRect(0, 0, 128, 128)
  const tex = new THREE.CanvasTexture(c)
  tex.colorSpace = THREE.SRGBColorSpace
  return tex
}

// IBL-карта: PMREM дорогий (десятки мс), тому рахуємо один раз на renderer.
const envCache = new WeakMap<THREE.WebGLRenderer, THREE.WebGLRenderTarget>()
function getEnv(renderer: THREE.WebGLRenderer): THREE.Texture {
  let rt = envCache.get(renderer)
  if (!rt) {
    const pmrem = new THREE.PMREMGenerator(renderer)
    const room = new RoomEnvironment()
    rt = pmrem.fromScene(room, 0.04)
    room.dispose()
    pmrem.dispose()
    envCache.set(renderer, rt)
  }
  return rt.texture
}

// ── Частинки ─────────────────────────────────────────────────────────────
type PKind = 'firefly' | 'mote'
interface Particle {
  sprite: THREE.Sprite
  active: boolean
  kind: PKind
  life: number
  maxLife: number
  vx: number
  vy: number
  vz: number
  size: number
  maxOpacity: number
  twinkle: number
  phase: number
}

export interface WardrobeFx {
  /** Блиск: roughness/metalness/envMap на всі стандартні матеріали моделі. */
  applyShine(root: THREE.Object3D): void
  /** Спалах + оберт + світлячки + хвиля. Викликати при зміні скіна. */
  playDress(root: THREE.Object3D): void
  /** Додатковий поворот навколо Y (радіани) від обертового ефекту. */
  readonly spin: number
  /** Блик, що періодично пробігає по моделі. */
  setGlint(on: boolean): void
  /** Повільні пилинки навколо персонажа. */
  setMotes(on: boolean): void
  /** Викликати щокадру з tick() (лише на активних кадрах). */
  update(dt: number): void
  dispose(): void
}

export function createWardrobeFx(renderer: THREE.WebGLRenderer, scene: THREE.Scene): WardrobeFx {
  // Tone mapping: Neutral не «випалює» кольори скіна (на відміну від ACES).
  renderer.toneMapping = THREE.NeutralToneMapping
  renderer.toneMappingExposure = 0.95

  const env = getEnv(renderer)
  const glow = makeSoftGlow()
  const ringTex = makeRingTex()

  const fxGroup = new THREE.Group()
  fxGroup.name = 'wardrobeFx'
  scene.add(fxGroup)

  // ── Блик: PointLight, що їде по дузі перед моделлю ──
  const glintLight = new THREE.PointLight(GLINT.color, 0, GLINT.distance, 2)
  glintLight.position.set(GLINT.fromX, GLINT.y, GLINT.z)
  // Світло лишається в сцені завжди (інтенсивність 0): зміна кількості
  // ламп перекомпілювала б шейдери і дала б підвис при першому проході.
  scene.add(glintLight)
  let glintOn = false
  let glintTimer = 1.2 // перший прохід швидко після відкриття
  let glintT = -1 // <0 — не грає

  // ── Хвиля-кільце ──
  const ringMat = new THREE.MeshBasicMaterial({
    map: ringTex,
    color: 0xd8e8ff,
    transparent: true,
    opacity: 0,
    depthWrite: false,
    blending: THREE.AdditiveBlending,
    side: THREE.DoubleSide,
    toneMapped: false,
  })
  const ring = new THREE.Mesh(new THREE.PlaneGeometry(1, 1), ringMat)
  ring.rotation.x = -Math.PI / 2
  ring.position.y = 0.02
  ring.visible = false
  fxGroup.add(ring)
  let ringT = -1

  // ── Пул спрайтів ──
  const POOL = DRESS.fireflies * 2 + MOTES.count
  const pool: Particle[] = []
  for (let i = 0; i < POOL; i++) {
    const mat = new THREE.SpriteMaterial({
      map: glow,
      transparent: true,
      opacity: 0,
      depthWrite: false,
      blending: THREE.AdditiveBlending,
      toneMapped: false,
    })
    const sprite = new THREE.Sprite(mat)
    sprite.visible = false
    fxGroup.add(sprite)
    pool.push({
      sprite, active: false, kind: 'firefly', life: 0, maxLife: 1,
      vx: 0, vy: 0, vz: 0, size: 0.2, maxOpacity: 1, twinkle: 0, phase: 0,
    })
  }

  function acquire(kind: PKind): Particle | null {
    // Світлячки мають пріоритет: якщо пул повний — витісняємо пилинку.
    for (const p of pool) if (!p.active) return p
    if (kind === 'firefly') for (const p of pool) if (p.kind === 'mote') return p
    return null
  }

  function spawnFirefly() {
    const p = acquire('firefly')
    if (!p) return
    const a = Math.random() * Math.PI * 2
    const r = rand(0.45, 1.0)
    p.kind = 'firefly'
    p.active = true
    p.life = 0
    p.maxLife = rand(1.1, 2.0)
    p.size = rand(0.12, 0.26)
    p.maxOpacity = rand(0.7, 1)
    p.twinkle = rand(6, 12)
    p.phase = Math.random() * 6.28
    p.vx = Math.cos(a + 1.57) * rand(0.05, 0.25) // легке закручування
    p.vz = Math.sin(a + 1.57) * rand(0.05, 0.25)
    p.vy = rand(0.5, 1.3)
    p.sprite.position.set(Math.cos(a) * r, rand(0.05, BODY_H * 0.7), Math.sin(a) * r)
    ;(p.sprite.material as THREE.SpriteMaterial).color.setHex(
      [0xffffff, 0xe8f0ff, 0xfff0d0, 0xd0ecff][(Math.random() * 4) | 0],
    )
    p.sprite.scale.setScalar(p.size)
    p.sprite.visible = true
  }

  function spawnMote() {
    const p = acquire('mote')
    if (!p) return
    const a = Math.random() * Math.PI * 2
    const r = rand(0.9, 2.4)
    p.kind = 'mote'
    p.active = true
    p.life = 0
    p.maxLife = rand(5, 10)
    p.size = rand(0.1, 0.22)
    p.maxOpacity = rand(0.2, 0.34)
    p.twinkle = rand(2, 5.5)
    p.phase = Math.random() * 6.28
    p.vx = rand(-0.08, 0.08)
    p.vz = rand(-0.08, 0.08)
    p.vy = rand(0.06, 0.16)
    p.sprite.position.set(Math.cos(a) * r, rand(0.2, BODY_H + 0.6), Math.sin(a) * r)
    ;(p.sprite.material as THREE.SpriteMaterial).color.setHex(
      MOTES.colors[(Math.random() * MOTES.colors.length) | 0],
    )
    p.sprite.scale.setScalar(p.size)
    p.sprite.visible = true
  }

  let motesOn = false
  function ensureMotes() {
    let n = 0
    for (const p of pool) if (p.active && p.kind === 'mote') n++
    for (; n < MOTES.count; n++) spawnMote()
  }
  function clearKind(kind: PKind) {
    for (const p of pool) {
      if (p.active && p.kind === kind) {
        p.active = false
        p.sprite.visible = false
      }
    }
  }

  // ── Спалах на матеріалах ──
  // Оригінальний emissive зберігаємо, щоб повернути рівно його.
  const emissiveBackup = new WeakMap<THREE.Material, { color: THREE.Color; intensity: number }>()
  const flashMats: THREE.MeshStandardMaterial[] = []
  let dressT = -1
  let spin = 0

  function standardMaterials(root: THREE.Object3D): THREE.MeshStandardMaterial[] {
    const out: THREE.MeshStandardMaterial[] = []
    root.traverse((o) => {
      const mesh = o as THREE.Mesh
      if (!mesh.isMesh) return
      const mats = Array.isArray(mesh.material) ? mesh.material : [mesh.material]
      for (const m of mats) {
        if (m && (m as THREE.MeshStandardMaterial).isMeshStandardMaterial) {
          out.push(m as THREE.MeshStandardMaterial)
        }
      }
    })
    return out
  }

  function restoreFlash() {
    for (const m of flashMats) {
      const b = emissiveBackup.get(m)
      if (b) {
        m.emissive.copy(b.color)
        m.emissiveIntensity = b.intensity
      }
    }
    flashMats.length = 0
  }

  // ── Публічний API ──
  const api: WardrobeFx = {
    applyShine(root) {
      for (const m of standardMaterials(root)) {
        m.roughness = SHINE.roughness
        m.metalness = SHINE.metalness
        m.envMap = env
        m.envMapIntensity = SHINE.envIntensity
        m.needsUpdate = true
      }
    },

    playDress(root) {
      if (effectsSuppressed()) return
      restoreFlash() // якщо попередній спалах ще не догорів
      for (const m of standardMaterials(root)) {
        if (!emissiveBackup.has(m)) {
          emissiveBackup.set(m, { color: m.emissive.clone(), intensity: m.emissiveIntensity })
        }
        m.emissive.setHex(DRESS.flashColor)
        m.emissiveIntensity = 0
        flashMats.push(m)
      }
      dressT = 0
      ringT = 0
      ring.visible = true
      for (let i = 0; i < DRESS.fireflies; i++) spawnFirefly()
    },

    get spin() {
      return spin
    },

    setGlint(on) {
      glintOn = on
      if (!on) {
        glintT = -1
        glintLight.intensity = 0
      }
    },

    setMotes(on) {
      motesOn = on
      if (!on) clearKind('mote')
    },

    update(dt) {
      // dt обмежуємо: після підвису/паузи не робимо «стрибок» анімації.
      dt = Math.min(dt, 0.05)

      const suppressed = effectsSuppressed()
      if (suppressed) {
        // Режим продуктивності ввімкнули посеред ефекту — гасимо все.
        if (dressT >= 0 || glintT >= 0 || ringT >= 0) {
          dressT = -1
          ringT = -1
          glintT = -1
          spin = 0
          restoreFlash()
          ring.visible = false
          glintLight.intensity = 0
          for (const p of pool) { p.active = false; p.sprite.visible = false }
        }
        return
      }

      // Оберт + емісивний спалах
      if (dressT >= 0) {
        dressT += dt
        const k = clamp01(dressT / DRESS.duration)
        spin = Math.PI * 2 * DRESS.spinTurns * easeOutCubic(clamp01(k / DRESS.spinFraction))
        const f = clamp01(k / DRESS.flashFraction)
        // Швидкий наліт і плавне згасання; після flashFraction — нуль.
        const flash = k < DRESS.flashFraction ? Math.sin(f * Math.PI) * DRESS.flashPeak : 0
        for (const m of flashMats) m.emissiveIntensity = flash
        if (k >= 1) {
          dressT = -1
          spin = 0 // 2π ≡ 0: не накопичуємо кут
          restoreFlash()
        }
      }

      // Хвиля-кільце
      if (ringT >= 0) {
        ringT += dt
        const k = clamp01(ringT / DRESS.ringDuration)
        const s = DRESS.ringFrom + (DRESS.ringTo - DRESS.ringFrom) * easeOutCubic(k)
        ring.scale.set(s, s, 1)
        ringMat.opacity = 0.55 * (1 - k)
        if (k >= 1) {
          ringT = -1
          ring.visible = false
        }
      }

      // Блик
      if (glintOn) {
        if (glintT < 0) {
          glintTimer -= dt
          if (glintTimer <= 0) {
            glintT = 0
            glintTimer = GLINT.period
          }
        } else {
          glintT += dt
          const k = clamp01(glintT / GLINT.sweep)
          glintLight.position.x = GLINT.fromX + (GLINT.toX - GLINT.fromX) * k
          // Дзвоноподібна огинаюча: світло з'являється і зникає плавно.
          glintLight.intensity = GLINT.peak * Math.sin(k * Math.PI)
          if (k >= 1) {
            glintT = -1
            glintLight.intensity = 0
          }
        }
      }

      // Пилинки
      if (motesOn) ensureMotes()

      // Частинки
      for (const p of pool) {
        if (!p.active) continue
        p.life += dt
        if (p.life >= p.maxLife) {
          p.active = false
          p.sprite.visible = false
          continue
        }
        const k = p.life / p.maxLife
        const pos = p.sprite.position
        pos.x += p.vx * dt
        pos.y += p.vy * dt
        pos.z += p.vz * dt
        // Огинаюча: швидкий вхід (10%), довге затухання.
        const fade = k < 0.1 ? k / 0.1 : 1 - (k - 0.1) / 0.9
        const tw = 0.75 + 0.25 * Math.sin(p.life * p.twinkle + p.phase)
        const mat = p.sprite.material as THREE.SpriteMaterial
        mat.opacity = p.maxOpacity * fade * tw
        p.sprite.scale.setScalar(p.size * (p.kind === 'firefly' ? 1 - k * 0.5 : 1))
      }
    },

    dispose() {
      restoreFlash()
      scene.remove(fxGroup)
      scene.remove(glintLight)
      glintLight.dispose()
      for (const p of pool) (p.sprite.material as THREE.SpriteMaterial).dispose()
      ring.geometry.dispose()
      ringMat.dispose()
      glow.dispose()
      ringTex.dispose()
      // env-карту НЕ звільняємо: рендерер повертається в пул (wardrobe3d.ts) і
      // наступний переглядач візьме її з envCache без повторного PMREM.
    },
  }

  return api
}
