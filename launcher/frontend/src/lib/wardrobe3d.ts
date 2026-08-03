// ── Wardrobe 3D viewer (three.js) ────────────────────────────────────────
// Обгортка над three.js для лівого переглядача Гардероба. Логіка тримається
// поза .svelte-файлом, щоб Wardrobe.svelte лишався читабельним.
//
// Моделі — стандартні binary glTF (skin-model-launcher.glb / -slim.glb),
// читаються штатним THREE.GLTFLoader. Текстура скіна (PNG 64x64/64x32) із
// data URL накладається на всі меші моделі з NearestFilter, щоб піксель-арт
// не розмивався при масштабуванні (як у старому JavaFX-лаунчері).
//
// ПЛАЩ: у .glb є окрема гілка node "Cape" → mesh "cube" (див. glbdump.js).
// Як і в старому лаунчері (capeMaterial/capeMeshViews), цей меш отримує
// ВЛАСНИЙ матеріал, не чіпається скіном і видимий ЛИШЕ коли задана текстура
// плаща через setCapeTexture. Текстура плаща з Mojang — 64x32, її треба
// розтягнути в 64x64 (nearest-neighbor), бо UV "Cape" в glb розраховані на
// 64x64-полотно (v до ~0.531, тобто ~34/64) — як toCapeAtlas у JavaFX.
//
// Анімації: у .glb присутні лише кліпи idle/idle23/idle4/idle1/walk, тому
// кнопки UI мапляться на них через ANIM_MAP. Відсутній кліп тихо
// ігнорується — поточна анімація продовжує працювати.

import * as THREE from 'three'
import { GLTFLoader } from 'three/addons/loaders/GLTFLoader.js'
import classicModelUrl from '../assets/models/skin-model-launcher.glb'
import slimModelUrl from '../assets/models/skin-model-launcher-slim.glb'
// Фолбек-текстура: якщо реальний скін пресету не вдалось завантажити
// (видалений файл кешу, мережева помилка Mojang CDN тощо), показуємо
// ванільного Steve замість "білої" непокритої моделі (баг з фото 1).
import fallbackSkinUrl from '../assets/skins/steve.png'

/** Мапінг кнопки UI → іменований кліп у .glb. */
const ANIM_MAP: Record<'idle' | 'sit' | 'look', string> = {
  idle: 'idle',
  sit: 'idle1', // тимчасова заглушка, поки немає власного "sit"-кліпа
  look: 'walk', // найближчий доступний рух
}

export interface WardrobeViewerHandle {
  /** Застосувати PNG скіна (64x64/64x32) на матеріал моделі. */
  setTexture(dataUrl: string): void
  /** Застосувати PNG плаща (64x32/64x64); null — сховати плащ. */
  setCapeTexture(dataUrl: string | null): void
  /** Перезавантажити відповідну .glb (classic/slim). */
  setSlim(slim: boolean): void
  /** Програти кліп через ANIM_MAP (тихо ігнорує відсутні кліпи). */
  playAnimation(name: 'idle' | 'sit' | 'look'): void
  /** Повністю звільнити WebGL-контекст та GPU-памʼять. */
  dispose(): void
}

function loadImage(url: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const img = new Image()
    img.onload = () => resolve(img)
    img.onerror = () => reject(new Error(`Failed to load texture: ${url}`))
    img.src = url
  })
}

/**
 * Розтягнути PNG плаща в 64x64 nearest-neighbor (аналог toCapeAtlas у
 * JavaFX). Mojang віддає капську 64x32; UV меша "Cape" в glb розраховані
 * на 64x64-полотно, тому треба перемапити 1px джерела → 1px цілі без
 * згладжування (інакше "попливе" малюнок плаща). Повний стрейтч 64x32 →
 * 64x64, як було споконвіку — користувач просив не чіпати плащ на моделі.
 */
function capeToAtlas(img: HTMLImageElement): HTMLCanvasElement {
  const canvas = document.createElement('canvas')
  canvas.width = 64
  canvas.height = 64
  const ctx = canvas.getContext('2d')
  if (!ctx) return canvas
  ctx.imageSmoothingEnabled = false
  ctx.clearRect(0, 0, 64, 64)
  ctx.drawImage(img, 0, 0, 64, 64)
  return canvas
}

/**
 * Знайти меші плаща в зчитаній моделі: node "Cape" → mesh "cube".
 * У три.js GLTFLoader зберігає імена нод, тож шукаємо групу "Cape" і
 * збираємо всі її mesh-нащадки.
 */
function collectCapeMeshes(root: THREE.Object3D): THREE.Mesh[] {
  const out: THREE.Mesh[] = []
  root.traverse((o) => {
    if (o.name !== 'Cape' && o.name !== 'cube') return
    const mesh = o as THREE.Mesh
    if (!mesh.isMesh) return
    out.push(mesh)
  })
  return out
}

/** Bounding box моделі БЕЗ мешів плаща (щоб плащ не зміщував кадрування). */
function modelBoxExcludingCape(model: THREE.Object3D, capeSet: Set<THREE.Object3D>): THREE.Box3 {
  const box = new THREE.Box3()
  const v = new THREE.Vector3()
  model.traverse((o) => {
    const mesh = o as THREE.Mesh
    if (!mesh.isMesh || capeSet.has(mesh)) return
    mesh.updateWorldMatrix(true, false)
    if (mesh.geometry.boundingBox == null) mesh.geometry.computeBoundingBox()
    const bb = mesh.geometry.boundingBox
    if (!bb) return
    box.union(bb.clone().applyMatrix4(mesh.matrixWorld))
  })
  return box
}

/**
 * Навести камеру на модель так, щоб вона повністю влізла у кадр із запасом
 * (fill). Центрує по вертикалі на центрі моделі — голова/ноги не зрізаються
 * навіть на вузьких портретних картках. Це виправляє "модель завелика,
 * голова зрізана" зі старої версії з хардкод-камерою.
 */
function frameCamera(camera: THREE.PerspectiveCamera, box: THREE.Box3, aspect: number, fill = 0.82) {
  const size = box.getSize(new THREE.Vector3())
  const center = box.getCenter(new THREE.Vector3())
  const halfH = size.y / 2
  const halfW = size.x / 2
  const tanFov = Math.tan(THREE.MathUtils.degToRad(camera.fov / 2))
  const distH = halfH / (tanFov * fill)
  const distW = halfW / (tanFov * Math.max(aspect, 0.001) * fill)
  const dist = Math.max(distH, distW, 0.1)
  camera.position.set(center.x, center.y, center.z + dist)
  camera.lookAt(center.x, center.y, center.z)
}

export function createWardrobeViewer(container: HTMLElement): Promise<WardrobeViewerHandle> {
  const scene = new THREE.Scene()

  const camera = new THREE.PerspectiveCamera(35, 1, 0.1, 100)
  camera.position.set(0, 1.55, 5.5)

  const renderer = new THREE.WebGLRenderer({ antialias: true, alpha: true })
  renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 2))
  // ПРИМІТКА: якщо контейнер щойно змонтовано (наприклад .wr-editor-3d
  // усередині модалки, яка щойно відкрилась), браузер міг ще не встигнути
  // зробити layout pass — clientWidth/clientHeight повернуть 0, viewer
  // стартує з aspect=1 у крихітному 1x1 canvas, і навіть після коректного
  // resize модель лишається маленькою й зміщеною (саме той баг "модель
  // мала і не по центру" в редакторі). getBoundingClientRect() форсує
  // синхронний layout, тому читаємо актуальний розмір ще до першого кадру.
  const rect = container.getBoundingClientRect()
  const width = rect.width || container.clientWidth || 1
  const height = rect.height || container.clientHeight || 1
  camera.aspect = width / height
  renderer.setSize(width, height)
  container.appendChild(renderer.domElement)

  // М'яке освітлення в стилі SubScene старого JavaFX-лаунчера.
  scene.add(new THREE.AmbientLight(0xffffff, 1.15))
  const dirLight = new THREE.DirectionalLight(0xffffff, 1.6)
  dirLight.position.set(2.5, 3.5, 4)
  scene.add(dirLight)

  // Контейнер для обертання моделі навколо вертикальної осі.
  const group = new THREE.Group()
  scene.add(group)

  let model: THREE.Object3D | null = null
  let mixer: THREE.AnimationMixer | null = null
  let animations: THREE.AnimationClip[] = []
  let currentAction: THREE.AnimationAction | null = null
  let currentAnim: 'idle' | 'sit' | 'look' = 'idle'
  let currentTexture: THREE.Texture | null = null
  let lastTextureDataUrl: string | null = null
  let currentCapeTexture: THREE.Texture | null = null
  let lastCapeDataUrl: string | null = null
  let capeMeshes: THREE.Mesh[] = []
  let capeMaterial: THREE.MeshStandardMaterial | null = null
  let currentSlim = false
  let disposed = false

  // Окремий лічильник, щоб асинхронні завантаження моделі не обганяли одна одну.
  let modelSeq = 0
  // Окремий лічильник, щоб застосувати лише останню накладену текстуру.
  let textureSeq = 0
  let capeSeq = 0

  function disposeObject(root: THREE.Object3D) {
    root.traverse((o) => {
      const mesh = o as THREE.Mesh
      if (!mesh.isMesh) return
      mesh.geometry?.dispose()
      const mats = Array.isArray(mesh.material) ? mesh.material : [mesh.material]
      for (const mat of mats) {
        if (!mat) continue
        if ('map' in mat) (mat.map as THREE.Texture | null)?.dispose()
        mat.dispose()
      }
    })
  }

  function setMeshMaterial(mesh: THREE.Mesh, mat: THREE.Material) {
    mesh.material = mat
    mesh.visible = true
  }

  function applyTexture(dataUrl: string, isFallbackRetry = false) {
    // Не перетираємо lastTextureDataUrl фолбеком: при setSlim() модель
    // перезавантажується і має знову спробувати реальну текстуру пресету,
    // а не застрягнути на Steve назавжди після одного мережевого збою.
    if (!isFallbackRetry) lastTextureDataUrl = dataUrl
    const seq = ++textureSeq
    loadImage(dataUrl)
      .then((img) => {
        if (disposed || seq !== textureSeq) return
        const tex = new THREE.Texture(img)
        // КРИТИЧНО: без NearestFilter піксель-арт скіна розмиється.
        tex.magFilter = THREE.NearestFilter
        tex.minFilter = THREE.NearestFilter
        tex.colorSpace = THREE.SRGBColorSpace
        // КРИТИЧНО: glTF UV розраховані під "не перевернуту" текстуру
        // (V=0 зверху), а THREE.Texture(img) за замовчуванням має
        // flipY=true — саме тому текстура скіна "з'їжджала" не на ті
        // грані кубів (фото 2). Вимикаємо flip, щоб UV з .glb збігались
        // з пікселями PNG 1:1.
        tex.flipY = false
        tex.needsUpdate = true

        const capeSet = new Set<THREE.Object3D>(capeMeshes)

        group.traverse((o) => {
          const mesh = o as THREE.Mesh
          if (!mesh.isMesh || capeSet.has(mesh)) return
          const mats = Array.isArray(mesh.material) ? mesh.material : [mesh.material]
          for (const mat of mats) {
            if (mat && 'map' in mat) {
              mat.map = tex
              mat.needsUpdate = true
            }
          }
        })

        currentTexture?.dispose()
        currentTexture = tex
      })
      .catch(() => {
        if (disposed || seq !== textureSeq) return
        // Текстура пресету не завантажилась (видалений файл кешу,
        // мережева помилка тощо) — раніше модель лишалась білою/непокритою
        // (баг з фото 1). Тепер підставляємо ванільного Steve, щоб модель
        // завжди мала якусь текстуру, і не намагаємось знову підвантажити
        // той самий фолбек у нескінченному циклі.
        if (!isFallbackRetry) applyTexture(fallbackSkinUrl, true)
      })
  }

  function applyCapeTexture(dataUrl: string | null) {
    lastCapeDataUrl = dataUrl
    const seq = ++capeSeq
    const mat = capeMaterial
    if (!mat || capeMeshes.length === 0) return
    if (!dataUrl) {
      mat.map = null
      mat.needsUpdate = true
      for (const m of capeMeshes) m.visible = false
      currentCapeTexture?.dispose()
      currentCapeTexture = null
      return
    }
    loadImage(dataUrl)
      .then((img) => {
        if (disposed || seq !== capeSeq) return
        const atlas = capeToAtlas(img)
        const tex = new THREE.Texture(atlas)
        tex.magFilter = THREE.NearestFilter
        tex.minFilter = THREE.NearestFilter
        tex.colorSpace = THREE.SRGBColorSpace
        // Той самий flipY-фікс, що й для скіна — UV меша "Cape" теж
        // розраховані на неперевернуте полотно.
        tex.flipY = false
        tex.needsUpdate = true
        mat.map = tex
        mat.needsUpdate = true
        for (const m of capeMeshes) m.visible = true
        currentCapeTexture?.dispose()
        currentCapeTexture = tex
      })
      .catch(() => {})
  }

  // Плащ: виділяємо окремий матеріал (як capeMaterial у старому лаунчері),
  // скін на нього не накладається, видимий лише при заданій текстурі.
  function wireCape(root: THREE.Object3D) {
    const meshes = collectCapeMeshes(root)
    capeMeshes = meshes
    capeMaterial = null
    if (meshes.length === 0) return
    const base = meshes[0].material
    capeMaterial = (Array.isArray(base) ? base[0] : base).clone() as THREE.MeshStandardMaterial
    capeMaterial.map = null
    for (const m of meshes) setMeshMaterial(m, capeMaterial)
    applyCapeTexture(lastCapeDataUrl)
  }

  function fitModel() {
    if (!model) return
    const capeSet = new Set<THREE.Object3D>(capeMeshes)
    const box = modelBoxExcludingCape(model, capeSet)
    const size = box.getSize(new THREE.Vector3())
    const center = box.getCenter(new THREE.Vector3())
    model.position.sub(center)
    model.position.y += size.y / 2 // ноги моделі на y = 0
    const targetHeight = 3.1
    const scale = size.y > 0 ? targetHeight / size.y : 1
    model.scale.multiplyScalar(scale)
    // Після фіту центр моделі на (0, size.y/2, 0) — кадруємо камеру на нього.
    const box2 = modelBoxExcludingCape(model, capeSet)
    frameCamera(camera, box2, camera.aspect)
  }

  function playAnimation(name: 'idle' | 'sit' | 'look') {
    currentAnim = name
    if (!model || !mixer) return
    const clipName = ANIM_MAP[name]
    const clip = animations.find((a) => a.name === clipName)
    // Немає потрібного кліпа — тихо ігноруємо, лишаємо поточну анімацію.
    if (!clip) return
    const nextAction = mixer.clipAction(clip)
    nextAction.reset()
    if (currentAction) {
      currentAction.crossFadeTo(nextAction, 0.3, true)
    }
    nextAction.play()
    currentAction = nextAction
  }

  function loadModel(slim: boolean) {
    const seq = ++modelSeq
    const url = slim ? slimModelUrl : classicModelUrl

    mixer?.stopAllAction()
    mixer = null
    currentAction = null
    animations = []
    if (model) {
      group.remove(model)
      disposeObject(model)
      model = null
    }

    const loader = new GLTFLoader()
    loader.loadAsync(url).then((gltf) => {
      if (disposed || seq !== modelSeq) return
      model = gltf.scene
      animations = gltf.animations ?? []
      group.add(model)
      mixer = new THREE.AnimationMixer(model)
      wireCape(model)
      fitModel()
      playAnimation(currentAnim)
      if (lastTextureDataUrl) applyTexture(lastTextureDataUrl)
    })
  }

  // ── Drag-to-rotate ──
  let dragging = false
  let lastX = 0
  // Модель у .glb повернута на 180° (спиною до камери за замовчуванням),
  // тому базовий yaw = 180° + невеликий доворот (~-18°, як у CSS-версії).
  let yaw = Math.PI - 0.31
  group.rotation.y = yaw // застосувати одразу, а не лише після першого drag

  function onPointerDown(e: MouseEvent) {
    dragging = true
    lastX = e.clientX
  }

  function onPointerMove(e: MouseEvent) {
    if (!dragging) return
    const dx = e.clientX - lastX
    lastX = e.clientX
    yaw += dx * 0.01
    group.rotation.y = yaw
  }

  function onPointerUp() {
    dragging = false
  }

  container.addEventListener('mousedown', onPointerDown)
  window.addEventListener('mousemove', onPointerMove)
  window.addEventListener('mouseup', onPointerUp)

  // ── Resize ──
  const resizeObserver = new ResizeObserver(() => {
    const w = container.clientWidth || 1
    const h = container.clientHeight || 1
    camera.aspect = w / h
    camera.updateProjectionMatrix()
    renderer.setSize(w, h)
    if (model) {
      const capeSet = new Set<THREE.Object3D>(capeMeshes)
      frameCamera(camera, modelBoxExcludingCape(model, capeSet), camera.aspect)
    }
  })
  resizeObserver.observe(container)

  // ── Рендер-цикл ──
  const clock = new THREE.Clock()
  let rafId = 0

  function tick() {
    if (disposed) return
    rafId = requestAnimationFrame(tick)
    const dt = clock.getDelta()
    mixer?.update(dt)
    renderer.render(scene, camera)
  }
  tick()

  // Страховка на випадок, коли контейнер щойно з'явився в DOM з
  // transition/анімацією (модалка редактора) — навіть getBoundingClientRect
  // у момент виклику createWardrobeViewer міг зловити проміжний 0-розмір.
  // Через два кадри розмір точно вже стабілізувався — форсуємо ще один
  // resize+перекадрування, щоб модель не лишалась маленькою й зміщеною.
  function forceResync() {
    const w = container.clientWidth || 1
    const h = container.clientHeight || 1
    camera.aspect = w / h
    camera.updateProjectionMatrix()
    renderer.setSize(w, h)
    if (model) fitModel()
  }

  // Стартове завантаження класичної моделі, потім віддаємо handle.
  const ready = new Promise<void>((resolve) => {
    loadModel(false)
    // Розв'язуємо після першого кадру рендера — модель підхопиться
    // через setSlim/setTexture одразу зі стану пресету.
    requestAnimationFrame(() => {
      requestAnimationFrame(() => {
        forceResync()
        resolve()
      })
    })
  })

  return ready.then(() => ({
    setTexture(dataUrl: string) {
      applyTexture(dataUrl)
    },
    setCapeTexture(dataUrl: string | null) {
      applyCapeTexture(dataUrl)
    },
    setSlim(slim: boolean) {
      if (slim === currentSlim) return
      currentSlim = slim
      loadModel(slim)
    },
    playAnimation,
    dispose() {
      disposed = true
      cancelAnimationFrame(rafId)
      resizeObserver.disconnect()
      container.removeEventListener('mousedown', onPointerDown)
      window.removeEventListener('mousemove', onPointerMove)
      window.removeEventListener('mouseup', onPointerUp)
      mixer?.stopAllAction()
      if (model) {
        group.remove(model)
        disposeObject(model)
      }
      currentTexture?.dispose()
      currentCapeTexture?.dispose()
      renderer.dispose()
      renderer.domElement.remove()
    },
  }))
}

// ── Міні-рендер карток (статична 3D-превʼюшка) ───────────────────────────
// Рендеримо СПРАВЖНЮ модель (classic/slim) з текстурою скіна в маленький
// canvas ОДИН раз і віддаємо data URL. Так навіть десятки карток у галереї
// не тримають живих WebGL-контекстів (на відміну від JavaFX mini-SubScenes,
// де кожна картка тримала свій 3D-рівень — для web це впиралося б у ліміт
// контекстів браузера). Візуально превʼю — той самий 3D-персонаж з тією ж
// текстурою, лише як легкий статичний кадр.

export interface SkinThumbOptions {
  slim: boolean
  /** URL/DataURL PNG-скіна; якщо не задано — рендеримо стандартний матеріал. */
  textureUrl?: string
  /** URL/DataURL PNG-плаща; якщо не задано — плащ приховано. */
  capeTextureUrl?: string
  width?: number
  height?: number
}

interface ThumbEnv {
  renderer: THREE.WebGLRenderer
  scene: THREE.Scene
  camera: THREE.PerspectiveCamera
  group: THREE.Group
}

let thumbEnv: ThumbEnv | null = null
const thumbModelCache = new Map<boolean, Promise<THREE.Object3D>>()
// Серійна черга: один спільний renderer не перетинається між запитами.
let thumbChain: Promise<unknown> = Promise.resolve()

/** Зробити статичний 3D-кадр моделі зі скіном і повернути PNG data URL. */
export function renderSkinThumbnail(opts: SkinThumbOptions): Promise<string> {
  const task = thumbChain.then(() => renderThumbOnce(opts))
  // Помилка одного рендера не ламає чергу для решти карток.
  thumbChain = task.catch(() => {})
  return task
}

function getThumbBaseModel(slim: boolean): Promise<THREE.Object3D> {
  let p = thumbModelCache.get(slim)
  if (!p) {
    p = new GLTFLoader()
      .loadAsync(slim ? slimModelUrl : classicModelUrl)
      .then((gltf) => gltf.scene)
    thumbModelCache.set(slim, p)
  }
  return p
}

function ensureThumbEnv(): ThumbEnv {
  if (thumbEnv) return thumbEnv
  const renderer = new THREE.WebGLRenderer({ antialias: false, alpha: true, preserveDrawingBuffer: true })
  renderer.setPixelRatio(1)
  const camera = new THREE.PerspectiveCamera(35, 1, 0.1, 100)
  camera.position.set(0, 1.55, 5.5)
  const scene = new THREE.Scene()
  scene.add(new THREE.AmbientLight(0xffffff, 1.15))
  const dir = new THREE.DirectionalLight(0xffffff, 1.6)
  dir.position.set(2.5, 3.5, 4)
  scene.add(dir)
  const group = new THREE.Group()
  scene.add(group)
  thumbEnv = { renderer, scene, camera, group }
  return thumbEnv
}

function fitThumbModel(model: THREE.Object3D, capeSet: Set<THREE.Object3D>, targetHeight: number) {
  const box = modelBoxExcludingCape(model, capeSet)
  const size = box.getSize(new THREE.Vector3())
  const center = box.getCenter(new THREE.Vector3())
  model.position.sub(center)
  model.position.y += size.y / 2 // ноги моделі на y = 0
  const scale = size.y > 0 ? targetHeight / size.y : 1
  model.scale.multiplyScalar(scale)
}

/** Фіксований ракурс картки пресету: модель повернута на 30° праворуч
 *  (як у макеті — видно тіло, голову, руки, ноги йдуть за межі картки). */
const THUMB_YAW = Math.PI + THREE.MathUtils.degToRad(30)

/**
 * Кадрує камеру на верхню частину моделі (голова+тіло+руки), так щоб ноги
 * виходили за нижній край картки — картка пресету навмисно "обрізає" ноги.
 */
function frameThumbCameraCropLegs(camera: THREE.PerspectiveCamera, box: THREE.Box3, aspect: number) {
  const size = box.getSize(new THREE.Vector3())
  const full = box.getCenter(new THREE.Vector3())
  // Показуємо верхні ~64% висоти моделі (голова/тіло/руки), центр кадру
  // зсунутий вище геометричного центру, щоб низ (ноги) пішов за межі.
  const visibleFraction = 0.64
  const visibleHeight = size.y * visibleFraction
  const center = new THREE.Vector3(full.x, box.max.y - visibleHeight / 2, full.z)
  const halfH = visibleHeight / 2
  const halfW = size.x / 2
  const tanFov = Math.tan(THREE.MathUtils.degToRad(camera.fov / 2))
  const fill = 0.86
  const distH = halfH / (tanFov * fill)
  const distW = halfW / (tanFov * Math.max(aspect, 0.001) * fill)
  const dist = Math.max(distH, distW, 0.1)
  camera.position.set(center.x, center.y, center.z + dist)
  camera.lookAt(center.x, center.y, center.z)
}

async function renderThumbOnce(opts: SkinThumbOptions): Promise<string> {
  const width = opts.width ?? 150
  const height = opts.height ?? 210
  const { renderer, scene, camera, group } = ensureThumbEnv()
  camera.aspect = width / height
  camera.updateProjectionMatrix()
  renderer.setSize(width, height)

  const base = await getThumbBaseModel(opts.slim)
  // Клон: геометрія спільна з кешем, а матеріали — свіжі копії, щоб не
  // мутувати закешовану базову модель.
  const model = base.clone(true)
  model.position.set(0, 0, 0)
  model.rotation.set(0, 0, 0)
  model.scale.set(1, 1, 1)

  const capeMeshes = collectCapeMeshes(model)
  const capeSet = new Set<THREE.Object3D>(capeMeshes)

  let tex: THREE.Texture | null = null
  {
    // opts.textureUrl порожній/undefined (запит бекенду впав) або сам
    // loadImage впав — в обох випадках підставляємо ванільного Steve,
    // щоб картка НІКОЛИ не лишалась білою непокритою моделлю (фото 1).
    const primaryUrl = opts.textureUrl || fallbackSkinUrl
    try {
      const img = await loadImage(primaryUrl)
      tex = new THREE.Texture(img)
      tex.magFilter = THREE.NearestFilter
      tex.minFilter = THREE.NearestFilter
      tex.colorSpace = THREE.SRGBColorSpace
      tex.flipY = false
      tex.needsUpdate = true
    } catch {
      try {
        const img = await loadImage(fallbackSkinUrl)
        tex = new THREE.Texture(img)
        tex.magFilter = THREE.NearestFilter
        tex.minFilter = THREE.NearestFilter
        tex.colorSpace = THREE.SRGBColorSpace
        tex.flipY = false
        tex.needsUpdate = true
      } catch {
        tex = null
      }
    }
  }

  let capeTex: THREE.Texture | null = null
  if (opts.capeTextureUrl) {
    try {
      const img = await loadImage(opts.capeTextureUrl)
      capeTex = new THREE.Texture(capeToAtlas(img))
      capeTex.magFilter = THREE.NearestFilter
      capeTex.minFilter = THREE.NearestFilter
      capeTex.colorSpace = THREE.SRGBColorSpace
      capeTex.flipY = false
      capeTex.needsUpdate = true
    } catch {
      capeTex = null
    }
  }

  const matCopies: THREE.Material[] = []
  model.traverse((o) => {
    const mesh = o as THREE.Mesh
    if (!mesh.isMesh) return
    const isCape = capeSet.has(mesh)
    const apply = (mat: THREE.Material): THREE.Material => {
      const copy = mat.clone()
      matCopies.push(copy)
      if (isCape) {
        // Плащ не отримує скін; його матеріал окремо (capeTex або прозорий).
        const sm = copy as THREE.MeshStandardMaterial
        sm.map = capeTex
      } else if (tex && 'map' in copy) {
        copy.map = tex
      }
      copy.needsUpdate = true
      return copy
    }
    if (Array.isArray(mesh.material)) {
      mesh.material = mesh.material.map(apply)
    } else {
      mesh.material = apply(mesh.material)
    }
    if (isCape) mesh.visible = capeTex != null
  })

  group.add(model)
  model.rotation.y = THUMB_YAW
  fitThumbModel(model, capeSet, 3.1)
  // Кадрування картки: фіксований ракурс, верх моделі в кадрі, ноги
  // навмисно виходять за нижній край (обрізка як у макеті пресету).
  frameThumbCameraCropLegs(camera, modelBoxExcludingCape(model, capeSet), camera.aspect)
  renderer.render(scene, camera)
  const dataUrl = renderer.domElement.toDataURL('image/png')
  group.remove(model)
  for (const m of matCopies) m.dispose()
  tex?.dispose()
  capeTex?.dispose()
  return dataUrl
}
