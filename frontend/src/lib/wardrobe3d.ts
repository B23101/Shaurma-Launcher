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
// Анімації в .glb: `idle` (із airplane_character.animation.json), `run`
// (tools/add_run.py) та сидіння — `sit_down` → `sit_idle` (цикл) → `stand_up`
// (tools/add_sit.py). Решту старих кліпів прибрано — вони лагали.
// Поверх idle накладаються ЖЕСТИ (див. GESTURES): короткі випадкові дії — озирнувся,
// глянув на руки, потягнувся, глянув через плече — як idle_sub_* у Modrinth, а при
// встановленні скіна — «оглядає себе». Жести адитивні (дельта до поточної пози idle),
// тому без стрибків на стику.
// «Сидіти» програє sit_down, далі sit_idle. З сидіння на idle/run модель
// спершу проходить stand_up і лише тоді вмикає обрану анімацію. Повторний
// виклик тієї самої анімації нічого не перезапускає.
//
// ПРОЗОРІСТЬ 2-го шару: меші "… Layer" у .glb мають окремий матеріал
// skin_layer (alphaMode BLEND, FrontSide, depthWrite=false), тому напівпрозорі
// пікселі (скельця окулярів, дірки на джинсах) змішуються з тілом, а не
// малюються суцільним чорним, як було з єдиним MASK-матеріалом.

import * as THREE from 'three'
import { GLTFLoader } from 'three/addons/loaders/GLTFLoader.js'
// Оптимізація (розділ 3): коли вікно згорнуте/приховане АБО просто
// неактивне (поза фокусом) — рендер-цикл пропускає кадри (мінімальна
// робота), але не зупиняється повністю, щоб при поверненні у фокус
// анімація миттєво продовжилась без пересоздання переглядача. Джерело
// активності — спільний модуль windowActive.svelte.ts (нативна подія
// window:active від shell, WM_ACTIVATE): у WebView2 document.hasFocus()
// не відображає деактивацію вікна, тому нативний канал обов'язковий.
import { windowVisibility } from './visible.svelte'
import { windowActive } from './windowActive.svelte'
import { createWardrobeFx } from './wardrobeFx'
import classicModelUrl from '../assets/models/skin-model-launcher.glb'
import slimModelUrl from '../assets/models/skin-model-launcher-slim.glb'
// Фолбек-текстура: якщо реальний скін пресету не вдалось завантажити
// (видалений файл кешу, мережева помилка Mojang CDN тощо), показуємо
// ванільного Steve замість "білої" непокритої моделі (баг з фото 1).
import fallbackSkinUrl from '../assets/skins/steve.png'

/** Живий 40-секундний цикл сидіння (будується в коді, див. buildSitRelaxedClip). */
const SIT_CLIP = 'sit_relaxed'

/** Мапінг кнопки UI → іменований кліп у .glb. */
export type WardrobeAnim = 'idle' | 'run' | 'sit' | 'look'
/** Кліп для «стоячих» анімацій; 'sit' обробляється окремо (станом сидіння). */
const ANIM_MAP: Record<WardrobeAnim, string> = {
  idle: 'idle',
  run: 'run',
  sit: SIT_CLIP,
  look: 'idle',
}

// ── Живе сидіння: 40 с без швів ───────────────────────────────────────────
// Кліп будується в коді з опорних поз і накладається на кістки .glb за uuid
// (імена нод у .glb повторюються: «Head» — і mesh, і pivot). Перша й остання
// пози збігаються, а всі «дихальні» коливання мають період, що ділить 40 с
// (4, 5, 8, 10, 20), тож цикл замикається без стрибка. З `sit_down` у цикл
// модель переходить кросфейдом.
//
// ПЕРЕГЛЯД СКІНА: сидячи він «живе» і нудьгує — озирається вліво-вправо, глянув
// униз, міняє руку, на яку спирається (права → ліва), на мить розвалюється вперед
// і знову відкидається. Ноги ЛИШАЮТЬСЯ на «землі» (не вище горизонталі, ≈1.57
// рад) і лише ледь погойдуються — жодного задирання ноги.
// Знаки (локальні осі моделі): для кінцівок +X = уперед; для корпусу/голови
// +X = назад (від'ємне hx — дивиться вниз); +Z у правих кінцівок = убік (у лівих навпаки).
export const SIT_LOOP_SECONDS = 40

type SitPose = Record<string, number>

// Базова поза — розслаблено відкинувся назад: обидві руки впираються позаду й
// трохи в боки, ноги витягнуті вперед і трохи розведені (обидві на одній висоті).
// Початок кліпа підхоплюється з кінця `sit_down` кросфейдом, тож стрибка немає.
const SIT_BASE: SitPose = {
  wy: 0.04,             // висота таза
  wx: 0.2, wz: 0, wyaw: 0,     // корпус відкинутий назад
  hx: 0.1, hy: 0, hz: 0.05,    // голова
  rax: -0.5, raz: 0.34, ray: 0, // права рука позаду
  lax: -0.5, laz: 0.34, lay: 0, // ліва рука позаду (знак по Z застосовується в побудові)
  rlx: 1.5, rlz: 0.26, rly: 0,  // права нога вперед і трохи вбік
  llx: 1.5, llz: 0.26, lly: 0,  // ліва нога на тій самій висоті
  cx: -0.12,            // плащ
}

// Спирається на праву руку, ліва лежить на коліні (ноги не піднімаються).
const LEAN_RIGHT = { wz: -0.12, wx: 0.1, hz: 0.1, rax: -0.55, raz: 0.42, lax: 0.9, laz: 0.04, wy: 0.05, llz: 0.14 }
// Дзеркально: спирається на ліву, права на коліні.
const LEAN_LEFT = { wz: 0.12, wx: 0.1, hz: -0.02, lax: -0.55, laz: 0.42, rax: 0.9, raz: 0.04, wy: 0.05, rlz: 0.14 }
// Відкинувся далі, руки позаду, ноги ширше.
const LEAN_BACK = { wx: 0.3, hx: 0.24, wy: 0.03, rax: -0.62, raz: 0.4, lax: -0.62, laz: 0.4, rlz: 0.34, llz: 0.34, cx: -0.16 }

/** Опорні пози: t у секундах, p — відхилення від SIT_BASE. Остання = перша. */
const SIT_KEYS: { t: number; p: SitPose }[] = [
  { t: 0,    p: {} },
  // 0–12: роззирається — ліворуч, пауза, праворуч, пауза, глянув униз (нудно)
  { t: 2.5,  p: { hy: 0.5, hz: -0.04, hx: 0.12 } },
  { t: 5.5,  p: { hy: 0.5, hz: -0.04, hx: 0.12 } },
  { t: 8,    p: { hy: -0.5, hz: 0.08, hx: 0.1 } },
  { t: 10,   p: { hy: -0.5, hz: 0.08, hx: 0.1 } },
  { t: 11.7, p: { hy: 0, hx: -0.2, wx: 0.12 } },
  // 12–22: перенесла вагу на праву руку, дивиться навколо
  { t: 14.5, p: { ...LEAN_RIGHT, hy: 0.2 } },
  { t: 18.5, p: { ...LEAN_RIGHT, hy: -0.42, hx: 0.06 } },
  { t: 21.5, p: { hy: 0.1, wx: 0.16 } },               // повернувся до середини
  // 22–30: змінює руку — тепер на ліву
  { t: 24.5, p: { ...LEAN_LEFT, hy: -0.15 } },
  { t: 28.5, p: { ...LEAN_LEFT, hy: 0.45, hx: 0.06 } },
  // 30–33: нудьгує — підвівся вперед, дивиться то вправо, то вліво
  { t: 31,   p: { hy: 0, wx: 0.04, hx: -0.28 } },
  { t: 32.2, p: { hy: 0.3, wx: 0.04, hx: -0.28 } },
  { t: 33.4, p: { hy: -0.3, wx: 0.05, hx: -0.28 } },
  // 34–38.5: відкинувся назад, дивиться вгору й убік
  { t: 35,   p: { ...LEAN_BACK, hy: 0 } },
  { t: 37,   p: { ...LEAN_BACK, hy: -0.3, hx: 0.22 } },
  { t: 38.7, p: { hy: 0.05, hx: 0.05 } },
  { t: 40,   p: {} },
]

const smoother = (k: number) => k * k * k * (k * (k * 6 - 15) + 10)

function sitPoseAt(t: number): SitPose {
  let i = 0
  while (i < SIT_KEYS.length - 2 && t > SIT_KEYS[i + 1].t) i++
  const a = SIT_KEYS[i], b = SIT_KEYS[i + 1]
  const k = smoother(Math.min(1, Math.max(0, (t - a.t) / (b.t - a.t || 1))))
  const out: SitPose = {}
  for (const key in SIT_BASE) {
    const va = a.p[key] ?? SIT_BASE[key]
    const vb = b.p[key] ?? SIT_BASE[key]
    out[key] = va + (vb - va) * k
  }
  const TAU = Math.PI * 2
  const w = (period: number, phase = 0) => Math.sin((TAU * t) / period + phase)
  // Дихання й дрібні рухи (періоди ділять 40 с → цикл замкнений).
  out.wx += 0.012 * w(4)
  out.hy += 0.03 * w(8)
  out.hx += 0.015 * w(5, 1)
  out.hz += 0.012 * w(10, 2)
  out.rax += 0.025 * w(5)
  out.lax += 0.025 * w(5, 2.1)
  // Ноги ледь погойдуються в протифазі, але не підіймаються вище горизонталі.
  out.rlx += 0.02 * w(5, 0.5)
  out.llx += 0.02 * w(5, 2.5)
  out.cx += 0.02 * w(8, 1)
  return out
}

// ── Жести поверх idle ────────────────────────────────────────────────────
// Як у Modrinth: базовий idle + випадкові короткі під-анімації. Але не окремі
// кліпи (вони абсолютні й «рвали» стик з idle), а АДИТИВНІ дельти кісток, що
// множаться на позу з mixer щокадру. Кожна кістка має власну затримку (тулуб
// відстає від голови, плащ — від тулуба), тож рух «тече» тілом, а не двигтить
// усім разом. Канали: w* — тулуб, h* — голова, ra*/la* — руки, cx — плащ.
// Знаки: для тулуба/голови -X = нахил уперед/вниз; для рук +X = уперед,
// +Z = убік (ліва рука віддзеркалюється всередині applyGesture).
type GPose = Record<string, number>
interface Gesture { dur: number; keys: { t: number; p: GPose }[]; swapArms?: boolean }

/** Затримка кісток відносно голови, с. */
const GESTURE_LAG: Record<string, number> = { Head: 0, Waist: 0.14, 'Right Arm': 0.08, 'Left Arm': 0.1, Cape: 0.24 }

const GESTURES: Record<string, Gesture> = {
  // Озирнувся ліворуч, пауза, праворуч, назад. Голова веде, тулуб трохи слідує.
  lookAround: { dur: 5.4, keys: [
    { t: 0, p: {} },
    { t: 0.9, p: { hy: 0.62, wy: 0.2, hx: -0.03, hz: 0.05 } },
    { t: 2.1, p: { hy: 0.62, wy: 0.2, hx: -0.03, hz: 0.05 } },
    { t: 3.5, p: { hy: -0.68, wy: -0.22, hz: -0.05 } },
    { t: 4.2, p: { hy: -0.68, wy: -0.22, hz: -0.05 } },
    { t: 5.4, p: {} },
  ] },
  // Глянув униз на себе, руки трохи подались уперед.
  glanceDown: { dur: 3.8, keys: [
    { t: 0, p: {} },
    { t: 0.8, p: { hx: -0.38, wx: -0.06, rax: 0.15, lax: 0.15 } },
    { t: 2.5, p: { hx: -0.4, hy: 0.08, wx: -0.06, rax: 0.15, lax: 0.15 } },
    { t: 3.8, p: {} },
  ] },
  // Потягнувся: коротка підготовка, руки вгору, груди вперед, видих.
  stretch: { dur: 5.2, keys: [
    { t: 0, p: {} },
    { t: 0.9, p: { rax: 0.3, lax: 0.3, raz: 0.1, laz: 0.1, wx: -0.05, hx: -0.06 } },
    { t: 2.1, p: { rax: 2.5, lax: 2.5, raz: 0.32, laz: 0.32, wx: 0.2, hx: 0.34 } },
    { t: 3.4, p: { rax: 2.6, lax: 2.6, raz: 0.32, laz: 0.32, wx: 0.24, hx: 0.38, hy: 0.05 } },
    { t: 4.3, p: { rax: 0.3, lax: 0.3, wx: -0.04, hx: -0.1 } },
    { t: 5.2, p: {} },
  ] },
  // Підняв руку й розглядає її, повертаючи кисть.
  checkHand: { dur: 4.6, swapArms: true, keys: [
    { t: 0, p: {} },
    { t: 0.8, p: { rax: 1.0, hx: -0.3, hy: 0.15, wx: -0.04 } },
    { t: 1.6, p: { rax: 1.25, ray: -0.3, hx: -0.3, hy: 0.15 } },
    { t: 2.5, p: { rax: 1.25, ray: 0.35, hx: -0.3, hy: 0.15 } },
    { t: 3.3, p: { rax: 1.0, ray: 0, hx: -0.26, hy: 0.1 } },
    { t: 4.6, p: {} },
  ] },
  // Глянув через плече на плащ і легко смикнув його.
  overShoulder: { dur: 5.6, keys: [
    { t: 0, p: {} },
    { t: 1.0, p: { wy: 0.45, hy: 0.6, hx: -0.04, cx: -0.05 } },
    { t: 2.0, p: { wy: 0.5, hy: 0.7, cx: -0.22, rax: -0.25 } },
    { t: 2.9, p: { wy: 0.5, hy: 0.7, cx: -0.06, rax: -0.15 } },
    { t: 3.7, p: { wy: 0.5, hy: 0.7, cx: -0.2 } },
    { t: 4.6, p: { wy: 0.15, hy: 0.2 } },
    { t: 5.6, p: {} },
  ] },
  // Скін щойно поставили: глянув на груди, покрутив руки, потім через плече на плащ.
  inspect: { dur: 7.2, keys: [
    { t: 0, p: {} },
    { t: 0.7, p: { hx: -0.4, wx: -0.05 } },
    { t: 1.6, p: { hx: -0.42, wx: -0.05, rax: 0.55, lax: 0.55, raz: 0.1, laz: 0.1 } },
    { t: 2.6, p: { hx: -0.3, hy: 0.18, wx: -0.04, rax: 1.05, lax: 0.8, ray: 0.35, lay: -0.2, raz: 0.12, laz: 0.12 } },
    { t: 3.6, p: { hx: -0.3, hy: -0.18, wx: -0.04, rax: 0.8, lax: 1.05, ray: -0.2, lay: 0.35, raz: 0.12, laz: 0.12 } },
    { t: 4.4, p: { hx: -0.12, rax: 0.1, lax: 0.1 } },
    { t: 5.2, p: { wy: 0.45, hy: 0.65, cx: -0.2 } },
    { t: 6.1, p: { wy: 0.5, hy: 0.7, cx: -0.06, rax: -0.2 } },
    { t: 7.2, p: {} },
  ] },
}
const GESTURE_RANDOM = ['lookAround', 'glanceDown', 'stretch', 'checkHand', 'overShoulder']
const GESTURE_CHANNELS = ['wx', 'wy', 'wz', 'hx', 'hy', 'hz', 'rax', 'ray', 'raz', 'lax', 'lay', 'laz', 'cx']

/** Поза жесту в момент t (відсутній канал = 0), м'яка інтерполяція між опорними кадрами. */
function gesturePoseAt(g: Gesture, t: number, out: Record<string, number>) {
  const keys = g.keys
  if (t <= 0 || t >= g.dur) { for (const c of GESTURE_CHANNELS) out[c] = 0; return }
  let i = 0
  while (i < keys.length - 2 && t > keys[i + 1].t) i++
  const a = keys[i], b = keys[i + 1]
  const k = smoother(Math.min(1, Math.max(0, (t - a.t) / (b.t - a.t || 1))))
  for (const c of GESTURE_CHANNELS) {
    const va = a.p[c] ?? 0, vb = b.p[c] ?? 0
    out[c] = va + (vb - va) * k
  }
}

const _bq = new THREE.Quaternion()
const _be = new THREE.Euler()

/** Побудувати кліп `sit_relaxed` для кісток моделі (за іменем pivot-вузла). */
function buildSitRelaxedClip(nodes: Record<string, THREE.Object3D>): THREE.AnimationClip | null {
  const need = ['Waist', 'Head', 'Right Arm', 'Left Arm', 'Right Leg', 'Left Leg', 'Cape']
  if (need.some((n) => !nodes[n])) return null
  const FPS = 24
  const N = SIT_LOOP_SECONDS * FPS
  const times: number[] = []
  const q: Record<string, number[]> = { Waist: [], Head: [], 'Right Arm': [], 'Left Arm': [], 'Right Leg': [], 'Left Leg': [], Cape: [] }
  const waistPos: number[] = [], rLegPos: number[] = [], lLegPos: number[] = [], waistScale: number[] = []
  const push = (name: string, x: number, y: number, z: number) => {
    _bq.setFromEuler(_be.set(x, y, z, 'YXZ'))
    q[name].push(_bq.x, _bq.y, _bq.z, _bq.w)
  }
  const rl = nodes['Right Leg'].position, ll = nodes['Left Leg'].position
  for (let i = 0; i <= N; i++) {
    const t = i / FPS
    const p = sitPoseAt(t)
    times.push(t)
    push('Waist', p.wx, p.wyaw, p.wz)
    push('Head', p.hx, p.hy, p.hz)
    push('Right Arm', p.rax, p.ray, p.raz)
    push('Left Arm', p.lax, p.lay, -p.laz)
    push('Right Leg', p.rlx, p.rly, p.rlz)
    push('Left Leg', p.llx, p.lly, -p.llz)
    push('Cape', p.cx, 0, 0)
    waistPos.push(0, p.wy, 0)
    rLegPos.push(rl.x, p.wy, rl.z)
    lLegPos.push(ll.x, p.wy, ll.z)
    const br = 1 + 0.008 * Math.sin((Math.PI * 2 * t) / 4)
    waistScale.push(1 + (br - 1) * 0.4, br, 1)
  }
  // Останній кадр = перший (на випадок дрібної похибки округлення).
  const tracks: THREE.KeyframeTrack[] = []
  for (const name in q) tracks.push(new THREE.QuaternionKeyframeTrack(`${nodes[name].uuid}.quaternion`, times, q[name]))
  tracks.push(new THREE.VectorKeyframeTrack(`${nodes['Waist'].uuid}.position`, times, waistPos))
  tracks.push(new THREE.VectorKeyframeTrack(`${nodes['Right Leg'].uuid}.position`, times, rLegPos))
  tracks.push(new THREE.VectorKeyframeTrack(`${nodes['Left Leg'].uuid}.position`, times, lLegPos))
  tracks.push(new THREE.VectorKeyframeTrack(`${nodes['Waist'].uuid}.scale`, times, waistScale))
  return new THREE.AnimationClip(SIT_CLIP, SIT_LOOP_SECONDS, tracks)
}

/** Повний вигляд персонажа: тип моделі, скін і плащ (null скіна — стандартний Steve). */
export interface WardrobeLook {
  skin: string | null
  cape: string | null
  slim: boolean
}

export interface WardrobeViewerHandle {
  /**
   * Показати вигляд ЦІЛКОМ і атомарно: тип моделі, скін і плащ разом. Це єдиний правильний спосіб
   * перемкнути персонажа (інший акаунт, інший пресет): окремі setSlim + setTexture перезавантажували
   * модель і на мить накладали на неї скін попереднього персонажа. Ідемпотентний: однаковий вигляд
   * нічого не змінює (без анімації й перезавантаження). Повертає true, якщо вигляд справді змінився.
   */
  setLook(look: WardrobeLook): boolean
  /** Застосувати PNG скіна (64x64/64x32) на матеріал моделі. */
  setTexture(dataUrl: string): void
  /** Застосувати PNG плаща (64x32/64x64); null — сховати плащ. */
  setCapeTexture(dataUrl: string | null): void
  /** Перезавантажити відповідну .glb (classic/slim). */
  setSlim(slim: boolean): void
  /** Програти кліп через ANIM_MAP (тихо ігнорує відсутні кліпи). */
  playAnimation(name: WardrobeAnim): void
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
interface CamFrame { center: THREE.Vector3; dist: number }

function computeFrame(camera: THREE.PerspectiveCamera, box: THREE.Box3, aspect: number, fill = 0.82, nearPlane = false): CamFrame {
  const size = box.getSize(new THREE.Vector3())
  const center = box.getCenter(new THREE.Vector3())
  const halfH = size.y / 2
  const halfW = size.x / 2
  const tanFov = Math.tan(THREE.MathUtils.degToRad(camera.fov / 2))
  const distH = halfH / (tanFov * fill)
  const distW = halfW / (tanFov * Math.max(aspect, 0.001) * fill)
  let dist = Math.max(distH, distW, 0.1)
  // Сидячи, ноги витягнуті до камери і через перспективу виглядають більшими:
  // відраховуємо відстань від найближчої до камери площини моделі.
  if (nearPlane) dist += Math.max(0, box.max.z - center.z)
  return { center, dist }
}


// ── Пул WebGL-рендерерів переглядачів ─────────────────────────────────────
// three.js тримає у внутрішній спільній «порожній» текстурі (WebGLUniforms:
// emptyTexture) слухач dispose від КОЖНОГО рендерера, що її використав, і
// ніколи її не звільняє. Тож renderer.dispose() не робить рендерер досяжним
// для GC: кожне відкриття гардероба/профілю/редактора назавжди лишало
// WebGL-контекст у GPU-процесі. Тому рендерери не знищуємо, а повертаємо в
// пул і даємо наступному переглядачу: живих контекстів стільки, скільки
// переглядачів відкрито одночасно, а не скільки разів їх відкривали.
const RENDERER_POOL_MAX = 3
const rendererPool: THREE.WebGLRenderer[] = []
const RESOURCE_IDLE_MS = 60_000
const rendererIdleTimers = new Map<THREE.WebGLRenderer, ReturnType<typeof setTimeout>>()

function acquireRenderer(): THREE.WebGLRenderer {
  const pooled = rendererPool.pop()
  if (pooled) {
    clearTimeout(rendererIdleTimers.get(pooled))
    rendererIdleTimers.delete(pooled)
    return pooled
  }
  return new THREE.WebGLRenderer({ antialias: true, alpha: true, powerPreference: 'high-performance' })
}

function releaseRenderer(renderer: THREE.WebGLRenderer) {
  renderer.domElement.remove()
  renderer.renderLists.dispose()
  if (rendererPool.length < RENDERER_POOL_MAX) {
    rendererPool.push(renderer)
    rendererIdleTimers.set(renderer, setTimeout(() => {
      rendererIdleTimers.delete(renderer)
      // Keep the reusable context, but release the full-size drawing buffer.
      renderer.setSize(1, 1, false)
      renderer.renderLists.dispose()
    }, RESOURCE_IDLE_MS))
    return
  }
  renderer.dispose()
  renderer.forceContextLoss()
}

export function createWardrobeViewer(container: HTMLElement): Promise<WardrobeViewerHandle> {
  const scene = new THREE.Scene()

  const camera = new THREE.PerspectiveCamera(35, 1, 0.1, 100)
  camera.position.set(0, 1.55, 5.5)

  const renderer = acquireRenderer()
  // 1.5 замість 2: на 4K/Retina різниця непомітна, а кількість пікселів
  // падає майже вдвічі — саме це прибирає секундний фриз при відкритті.
  renderer.setPixelRatio(Math.min(window.devicePixelRatio || 1, 1.5))
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

  // М'яке освітлення в стилі SubScene старого JavaFX-лаунчера. Світла
  // два джерела: IBL-відблиск (wardrobeFx) + прямі лампи.
  scene.add(new THREE.AmbientLight(0xffffff, 0.5))
  const dirLight = new THREE.DirectionalLight(0xffffff, 1.1)
  dirLight.position.set(2.5, 3.5, 4)
  scene.add(dirLight)

  // Ефекти Millida-стилю: блиск, спалах при зміні скіна, світлячки, блик.
  const fx = createWardrobeFx(renderer, scene)
  fx.setGlint(true)
  fx.setMotes(true)

  // Контейнер для обертання моделі навколо вертикальної осі.
  const group = new THREE.Group()
  scene.add(group)

  let model: THREE.Object3D | null = null
  let mixer: THREE.AnimationMixer | null = null
  let animations: THREE.AnimationClip[] = []
  let currentAction: THREE.AnimationAction | null = null
  let currentAnim: WardrobeAnim = 'idle'
  let currentTexture: THREE.Texture | null = null
  let lastTextureDataUrl: string | null = null
  // Для ефекту «переодягнувся»: не грати на першому завантаженні і на тому
  // самому скіні (Wardrobe.svelte викликає setTexture при кожному $effect).
  let appliedTextureUrl: string | null = null
  let currentCapeTexture: THREE.Texture | null = null
  let lastCapeDataUrl: string | null = null
  let capeMeshes: THREE.Mesh[] = []
  let capeMaterial: THREE.MeshStandardMaterial | null = null
  let currentSlim = false
  let disposed = false
  // Кадрування: стоячи й сидячи тіло займає різну висоту, тому камера плавно
  // переходить між двома кадрами (0 = стоячи, 1 = сидячи).
  let standBox: THREE.Box3 | null = null
  let sitBox: THREE.Box3 | null = null
  let frameK = 0
  let frameTarget = 0
  // Модель показуємо лише коли на ній уже є текстура (інакше блимає біла
  // модель), а перший рендер чекає на асинхронну компіляцію шейдерів
  // (compileAsync) — без неї компіляція блокує головний потік на ~секунду.
  let renderGate = true
  let firstShown = false
  let revealTimer: ReturnType<typeof setTimeout> | undefined
  let loadingEl: HTMLDivElement | null = null
  let loadingAnim: Animation | null = null
  let containerPosPatched = false

  function showLoading() {
    if (loadingEl || disposed) return
    if (getComputedStyle(container).position === 'static') {
      container.style.position = 'relative'
      containerPosPatched = true
    }
    const wrap = document.createElement('div')
    wrap.style.cssText = 'position:absolute;inset:0;display:flex;align-items:center;justify-content:center;pointer-events:none'
    const ring = document.createElement('div')
    ring.style.cssText = 'width:30px;height:30px;box-sizing:border-box;border-radius:50%;border:3px solid rgba(255,255,255,.14);border-top-color:var(--orange,#f59e0b)'
    wrap.appendChild(ring)
    container.appendChild(wrap)
    loadingEl = wrap
    loadingAnim = ring.animate([{ transform: 'rotate(0deg)' }, { transform: 'rotate(360deg)' }], { duration: 800, iterations: Infinity })
  }
  function hideLoading() {
    loadingAnim?.cancel()
    loadingAnim = null
    loadingEl?.remove()
    loadingEl = null
    if (containerPosPatched) {
      container.style.position = ''
      containerPosPatched = false
    }
  }
  showLoading()

  // Текстура вже на моделі — показуємо її. Перший показ: шейдери компілюються
  // паралельно (compileAsync), і лише після цього знімаємо індикатор і
  // дозволяємо рендер.
  function revealModel() {
    if (!model || disposed) return
    model.visible = true
    clearTimeout(revealTimer)
    if (firstShown) return
    firstShown = true
    const done = () => {
      if (disposed) return
      renderGate = false
      hideLoading()
    }
    renderer.compileAsync(scene, camera).then(done, done)
  }

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

        if (!isFallbackRetry) {
          if (appliedTextureUrl !== null && appliedTextureUrl !== dataUrl && model) {
            fx.playDress(model)
            startInspect()
          }
          appliedTextureUrl = dataUrl
        }
        revealModel()
      })
      .catch(() => {
        if (disposed || seq !== textureSeq) return
        // Текстура пресету не завантажилась (видалений файл кешу, мережева
        // помилка тощо). Тепер підставляємо ванільного Steve, щоб модель
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
  // Розмір плаща на моделі НЕ чіпаємо (картки плащів зменшуються на рівні
  // CSS у Wardrobe.svelte, а сам 3D-меш лишається як у glb).
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
    standBox = modelBoxExcludingCape(model, capeSet)
    sitBox = measureSeatedBox()
    applyFrame()
  }

  /** Рамка тіла в позі сидіння (початок циклу) — для кадрування камери. Позу кісток потім відновлюємо. */
  function measureSeatedBox(): THREE.Box3 | null {
    const clip = animations.find((a) => a.name === SIT_CLIP)
    if (!model || !clip) return null
    const saved: { n: THREE.Object3D; p: THREE.Vector3; q: THREE.Quaternion; sc: THREE.Vector3 }[] = []
    model.traverse((o) => { if (!(o as THREE.Mesh).isMesh) saved.push({ n: o, p: o.position.clone(), q: o.quaternion.clone(), sc: o.scale.clone() }) })
    const tmp = new THREE.AnimationMixer(model)
    const act = tmp.clipAction(clip)
    act.play()
    tmp.update(0)
    model.updateMatrixWorld(true)
    const box = modelBoxExcludingCape(model, new Set<THREE.Object3D>(capeMeshes))
    act.stop()
    tmp.uncacheRoot(model)
    for (const r of saved) { r.n.position.copy(r.p); r.n.quaternion.copy(r.q); r.n.scale.copy(r.sc) }
    model.updateMatrixWorld(true)
    return box
  }

  /** Поставити камеру за поточним переходом стоячи↔сидячи. */
  function applyFrame() {
    if (!standBox) return
    const a = computeFrame(camera, standBox, camera.aspect)
    let center = a.center, dist = a.dist
    if (sitBox && frameK > 0) {
      const b = computeFrame(camera, sitBox, camera.aspect, 0.76, true)
      center = a.center.clone().lerp(b.center, frameK)
      dist = a.dist + (b.dist - a.dist) * frameK
    }
    camera.position.set(center.x, center.y, center.z + dist)
    camera.lookAt(center.x, center.y, center.z)
  }

  // ── Сидіння: станова машина stand → sitting → standing → stand ──
  type SitState = 'stand' | 'sitting' | 'standing'
  let sitState: SitState = 'stand'
  let standTarget: 'idle' | 'run' = 'idle' // що грати після stand_up

  function hasClip(name: string): boolean {
    return animations.some((a) => a.name === name)
  }

  /** Плавно перейти на кліп. once=true — один раз і завмерти на останньому кадрі. */
  function fadeTo(clipName: string, opts: { once?: boolean; fade?: number } = {}): boolean {
    const clip = animations.find((a) => a.name === clipName)
    if (!clip || !mixer) return false
    const next = mixer.clipAction(clip)
    // Той самий кліп уже грає (напр. таймер профілю кличе 'look' кожні 7 с,
    // а це теж idle) — не скидаємо його на початок.
    if (next === currentAction && next.isRunning()) return true
    next.reset()
    next.setLoop(opts.once ? THREE.LoopOnce : THREE.LoopRepeat, opts.once ? 1 : Infinity)
    next.clampWhenFinished = !!opts.once
    if (currentAction && currentAction !== next) currentAction.crossFadeTo(next, opts.fade ?? 0.3, true)
    next.play()
    currentAction = next
    return true
  }

  function onClipFinished(clipName: string) {
    if (clipName === 'sit_down' && sitState === 'sitting') {
      fadeTo(hasClip(SIT_CLIP) ? SIT_CLIP : 'sit_idle', { fade: hasClip(SIT_CLIP) ? 0.5 : 0.2 })
    } else if (clipName === 'stand_up' && sitState === 'standing') {
      sitState = 'stand'
      fadeTo(ANIM_MAP[standTarget], { fade: 0.25 })
    }
  }

  // ── Шар жестів (адитивний, поверх idle) ──
  let gesture: Gesture | null = null
  let gestureT = 0
  let gestureMirror = 1 // ±1: у який бік дивиться/повертається (міняється щоразу)
  let gestureW = 0 // вага: плавно гасне, якщо стоячу анімацію перебили (біг, сидіння)
  let gestureCountdown = 4 + Math.random() * 3
  let lastGesture = ''
  const _gpose: Record<string, number> = {}
  const _gq = new THREE.Quaternion()
  const _ge = new THREE.Euler()

  function standingIdle(): boolean {
    return sitState === 'stand' && (currentAnim === 'idle' || currentAnim === 'look')
  }

  function startGesture(name: string) {
    const g = GESTURES[name]
    if (!g || !model || !mixer || !standingIdle()) return
    gesture = g
    gestureT = 0
    gestureW = 1
    gestureMirror = Math.random() < 0.5 ? -1 : 1
    lastGesture = name
  }

  /** Скін щойно поставили: стоячи персонаж оглядає себе (одноразово, потім назад в idle). */
  function startInspect() {
    startGesture('inspect')
  }

  function nextRandomGesture() {
    const pool = GESTURE_RANDOM.filter((n) => n !== lastGesture)
    startGesture(pool[Math.floor(Math.random() * pool.length)])
    gestureCountdown = 6 + Math.random() * 5
  }

  /** Накласти поточний жест на кістки (викликається щокадру ПІСЛЯ mixer.update). */
  function updateGesture(dt: number) {
    if (!gesture) {
      // Нова анімація-спокій: лічильник до наступного випадкового жесту.
      if (model?.visible && standingIdle()) {
        gestureCountdown -= dt
        if (gestureCountdown <= 0) nextRandomGesture()
      } else gestureCountdown = Math.max(gestureCountdown, 3)
      return
    }
    const allowed = standingIdle()
    gestureW += ((allowed ? 1 : 0) - gestureW) * (1 - Math.exp(-dt * (allowed ? 8 : 12)))
    gestureT += dt
    if (gestureT >= gesture.dur + 0.3 || (!allowed && gestureW < 0.02)) { gesture = null; gestureCountdown = 6 + Math.random() * 5; return }
    const m = gestureMirror
    for (const b of bones) {
      const lag = GESTURE_LAG[b.name]
      if (lag === undefined) continue
      gesturePoseAt(gesture, gestureT - lag, _gpose)
      const p = _gpose
      let x = 0, y = 0, z = 0
      switch (b.name) {
        case 'Waist': x = p.wx; y = p.wy * m; z = p.wz * m; break
        case 'Head': x = p.hx; y = p.hy * m; z = p.hz * m; break
        case 'Right Arm': { const sw = gesture.swapArms && m < 0; x = sw ? p.lax : p.rax; y = sw ? p.lay : p.ray; z = sw ? p.laz : p.raz; break }
        case 'Left Arm': { const sw = gesture.swapArms && m < 0; x = sw ? p.rax : p.lax; y = -(sw ? p.ray : p.lay); z = -(sw ? p.raz : p.laz); break }
        case 'Cape': x = p.cx; break
      }
      const w = gestureW
      b.node.quaternion.multiply(_gq.setFromEuler(_ge.set(x * w, y * w, z * w, 'YXZ')))
    }
  }

  function playAnimation(name: WardrobeAnim) {
    currentAnim = name
    frameTarget = name === 'sit' ? 1 : 0
    if (!model || !mixer) return
    if (name === 'sit') {
      if (sitState === 'sitting') return // уже сидить / сідає
      if (!hasClip('sit_down')) return
      sitState = 'sitting'
      fadeTo('sit_down', { once: true, fade: 0.25 })
      return
    }
    const target: 'idle' | 'run' = name === 'run' ? 'run' : 'idle'
    if (sitState === 'sitting') {
      // Піднімаємося: stand_up → далі обрана анімація. Немає кліпа — просто стоїмо.
      standTarget = target
      sitState = 'standing'
      if (!fadeTo('stand_up', { once: true, fade: 0.2 })) {
        sitState = 'stand'
        fadeTo(ANIM_MAP[target])
      }
      return
    }
    if (sitState === 'standing') {
      standTarget = target // передумав, поки встає
      return
    }
    fadeTo(ANIM_MAP[target])
  }

  function loadModel(slim: boolean) {
    const seq = ++modelSeq
    const url = slim ? slimModelUrl : classicModelUrl

    mixer?.stopAllAction()
    mixer = null
    currentAction = null
    gesture = null
    sitState = 'stand'
    animations = []
    bones = []
    standBox = null
    sitBox = null
    hitT = -1
    if (model) {
      group.remove(model)
      disposeObject(model)
      model = null
    }

    const loader = new GLTFLoader()
    loader.loadAsync(url).then((gltf) => {
      if (disposed || seq !== modelSeq) { disposeObject(gltf.scene); return }
      model = gltf.scene
      animations = gltf.animations ?? []
      group.add(model)
      // Без текстури модель біла — ховаємо, доки applyTexture не покаже її.
      model.visible = false
      mixer = new THREE.AnimationMixer(model)
      mixer.addEventListener('finished', (e) => {
        if ((e as unknown as { action: THREE.AnimationAction }).action === currentAction) {
          onClipFinished((e as unknown as { action: THREE.AnimationAction }).action.getClip().name)
        }
      })
      bones = collectBones(model)
      {
        // Живий 40-секундний цикл сидіння — будуємо по кістках цієї моделі.
        // GLTFLoader замінює пробіли в іменах на «_» («Right Arm» → «Right_Arm»).
        const nodes: Record<string, THREE.Object3D> = {}
        model.traverse((o) => {
          if ((o as THREE.Mesh).isMesh || o.children.length === 0) return
          nodes[o.name.replace(/_/g, ' ')] ??= o
        })
        const sit = buildSitRelaxedClip(nodes)
        if (sit) animations.push(sit)
      }
      wireCape(model)
      fx.applyShine(model) // ПІСЛЯ wireCape: матеріал плаща вже клонований
      fitModel()
      if (currentAnim === 'sit') {
        // Перезавантаження моделі (classic↔slim) під час сидіння — одразу в сидіння.
        sitState = 'sitting'
        frameTarget = frameK = 1
        applyFrame()
        fadeTo(hasClip(SIT_CLIP) ? SIT_CLIP : 'sit_idle', { fade: 0 })
      } else {
        playAnimation(currentAnim)
      }
      if (lastTextureDataUrl) applyTexture(lastTextureDataUrl)
      // Страховка: текстура не прийшла (бекенд мовчить/не викликали setTexture) —
      // за 4 с показуємо Steve, щоб модель не лишалась невидимою назавжди.
      clearTimeout(revealTimer)
      revealTimer = setTimeout(() => {
        if (disposed || seq !== modelSeq || !model || model.visible) return
        applyTexture(fallbackSkinUrl, true)
      }, 4000)
    })
  }

  // ── Drag-to-rotate ──
  let dragging = false
  let lastX = 0
  // Модель у .glb повернута на 180° (спиною до камери за замовчуванням),
  // тому базовий yaw = 180° + невеликий доворот (~-18°, як у CSS-версії).
  let yaw = Math.PI - 0.31
  group.rotation.y = yaw // застосувати одразу, а не лише після першого drag

  let downX = 0
  let downY = 0

  function onPointerDown(e: MouseEvent) {
    dragging = true
    lastX = e.clientX
    downX = e.clientX
    downY = e.clientY
  }

  function onPointerMove(e: MouseEvent) {
    if (!dragging) return
    const dx = e.clientX - lastX
    lastX = e.clientX
    yaw += dx * 0.01
    group.rotation.y = yaw
  }

  function onPointerUp(e: MouseEvent) {
    // Клік без перетягування — «удар» по моделі.
    if (dragging && Math.hypot(e.clientX - downX, e.clientY - downY) < 5) triggerHit()
    dragging = false
  }

  // ── «Удар» як у Minecraft: відкидання назад + червоний відтінок ──
  // Окрім відкидання всієї моделі, кістки реагують поверх idle: голову
  // закидає, руки розлітаються, ноги підгинаються, плащ відлітає. Кожна
  // частина має власну затримку й силу, тож рух «хвилею» йде по тілу.
  const HIT_DURATION = 0.7
  const HIT_RED = new THREE.Color(1, 0.3, 0.3)
  let hitT = -1
  let hitDir = 1 // ±1: у який бік скручує тіло (міняється щоразу)

  // Кістки-pivot (групи без mesh) за іменем у .glb. animated=true — кістку
  // веде mixer (його значення скидаються щокадру), інакше треба самим
  // повертати у спокій, щоб дельта не накопичувалась.
  interface Bone { name: string; node: THREE.Object3D; rest: THREE.Quaternion; animated: boolean }
  let bones: Bone[] = []
  const ANIMATED = new Set(['Waist', 'Head', 'Right Arm', 'Left Arm', 'Right Leg', 'Cape'])
  const BONE_NAMES = ['Waist', 'Head', 'Body', 'Right Arm', 'Left Arm', 'Right Leg', 'Left Leg', 'Cape']
  const _dq = new THREE.Quaternion()
  const _de = new THREE.Euler()

  function collectBones(root: THREE.Object3D): Bone[] {
    const out: Bone[] = []
    root.traverse((o) => {
      if ((o as THREE.Mesh).isMesh || !BONE_NAMES.includes(o.name)) return
      if (o.children.length === 0) return
      out.push({ name: o.name, node: o, rest: o.quaternion.clone(), animated: ANIMATED.has(o.name) })
    })
    return out
  }

  /** Пружинна крива удару: швидкий поштовх, далі згасаюче коливання. */
  function spring(t: number): number {
    const IN = 0.08
    if (t <= 0) return 0
    if (t < IN) return Math.sin((t / IN) * Math.PI / 2)
    const s = t - IN
    return Math.exp(-5 * s) * Math.cos(8 * s)
  }

  // Додатний X у локальних осях моделі = хил назад (модель повернута на 180°).
  function boneDelta(name: string, t: number): [number, number, number] {
    switch (name) {
      case 'Waist': { const f = spring(t); return [0.12 * f, 0.22 * hitDir * f, 0.1 * hitDir * f] }
      case 'Head': { const f = spring(t - 0.04); return [0.55 * f, -0.3 * hitDir * f, 0] }
      case 'Right Arm': { const f = spring(t - 0.02); return [1.0 * f, 0, 0.55 * f] }
      case 'Left Arm': { const f = spring(t - 0.03); return [0.8 * f, 0, -0.65 * f] }
      case 'Right Leg': { const f = spring(t - 0.01); return [0.35 * f, 0, 0.06 * f] }
      case 'Left Leg': { const f = spring(t - 0.02); return [-0.3 * f, 0, -0.06 * f] }
      case 'Cape': { const f = spring(t - 0.06); return [-0.7 * f, 0, 0] }
      default: return [0, 0, 0]
    }
  }

  function applyBones(t: number, active: boolean) {
    for (const b of bones) {
      if (!active) {
        if (!b.animated) b.node.quaternion.copy(b.rest)
        continue
      }
      const [x, y, z] = boneDelta(b.name, t)
      _dq.setFromEuler(_de.set(x, y, z, 'ZYX'))
      if (!b.animated) b.node.quaternion.copy(b.rest)
      b.node.quaternion.multiply(_dq)
    }
  }

  function triggerHit() {
    if (!model || !model.visible || renderGate || disposed) return
    hitT = 0
    hitDir = Math.random() < 0.5 ? -1 : 1
  }

  function setHitTint(k: number) {
    group.traverse((o) => {
      const mesh = o as THREE.Mesh
      if (!mesh.isMesh) return
      const mats = Array.isArray(mesh.material) ? mesh.material : [mesh.material]
      for (const m of mats) {
        const c = (m as THREE.MeshStandardMaterial)?.color
        if (c) c.set(0xffffff).lerp(HIT_RED, k)
      }
    })
  }

  function updateHit(dt: number) {
    if (hitT < 0) return
    hitT += dt
    if (hitT >= HIT_DURATION) {
      hitT = -1
      group.rotation.x = 0
      group.position.set(0, 0, 0)
      setHitTint(0)
      applyBones(0, false)
      return
    }
    const f = spring(hitT)
    // Камера дивиться з +z: від'ємний кут по X хилить модель від камери.
    group.rotation.x = -0.38 * f
    group.position.z = -0.22 * f
    group.position.y = 0.07 * Math.sin(Math.PI * Math.min(1, hitT / 0.3))
    setHitTint(Math.max(0, 1 - hitT / 0.4))
    applyBones(hitT, true)
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
    applyFrame()
  })
  resizeObserver.observe(container)

  // ── Рендер-цикл ──
  const clock = new THREE.Clock()
  let rafId = 0

  function tick() {
    if (disposed) return
    rafId = requestAnimationFrame(tick)
    // Вікно згорнуте/приховане АБО неактивне — рендер не потрібен (ніхто
    // його не бачить / анімації паузуються за правилом «неактивно → не
    // граємо»). Пропускаємо кадр без dispose: mixer залишається в робочому
    // стані, а коли вікно знову видиме/активне — анімація продовжується
    // з наступного кадру.
    if (!windowVisibility.visible || !windowActive.active) {
      // Скидаємо накопичений час ПЕРЕД пропуском кадру: якщо просто
      // return, наступний getDelta() після хвилинної паузи поверне
      // ВЕСЬ інтервал неактивності, і анімація "стрибне" на кілька
      // циклів уперед при поверненні у фокус.
      clock.getDelta()
      return
    }
    // Шейдери ще компілюються (compileAsync) — не малюємо, щоб не блокувати потік.
    if (renderGate) {
      clock.getDelta()
      return
    }
    const dt = clock.getDelta()
    mixer?.update(dt)
    updateGesture(dt)
    if (frameK !== frameTarget) {
      frameK += (frameTarget - frameK) * (1 - Math.exp(-dt * 3.2))
      if (Math.abs(frameTarget - frameK) < 0.002) frameK = frameTarget
      applyFrame()
    }
    updateHit(dt)
    fx.update(dt)
    // Кут від перетягування мишею + оберт від ефекту (не перетирають одне одного).
    group.rotation.y = yaw + fx.spin
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
    // НЕ викликаємо fitModel(): він міряє ПОТОЧНУ позу (біг/сидіння) і повторно
    // масштабує модель. Для нового розміру контейнера досить перерахувати камеру.
    applyFrame()
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
    setLook(look: WardrobeLook): boolean {
      if (disposed) return false
      const skin = look.skin || fallbackSkinUrl
      const cape = look.cape || null
      const slim = !!look.slim
      const sameSkin = skin === lastTextureDataUrl
      const sameCape = cape === lastCapeDataUrl
      const sameSlim = slim === currentSlim
      if (sameSkin && sameCape && sameSlim) return false
      if (!sameSlim) {
        // Модель перезавантажується (classic↔slim): новий скін і плащ записуємо ДО цього, щоб loadModel
        // підхопив саме їх, а не залишки попереднього персонажа. Відповіді, що ще в польоті для старого
        // вигляду, скасовуємо лічильниками.
        textureSeq++
        capeSeq++
        lastTextureDataUrl = skin
        lastCapeDataUrl = cape
        currentSlim = slim
        loadModel(slim)
      } else {
        if (!sameSkin) applyTexture(skin)
        if (!sameCape) applyCapeTexture(cape)
      }
      return true
    },
    playAnimation,
    dispose() {
      if (disposed) return
      disposed = true
      clearTimeout(revealTimer)
      hideLoading()
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
      fx.dispose()
      releaseRenderer(renderer)
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
const _thumbSize = new THREE.Vector2()
const thumbModelCache = new Map<boolean, Promise<THREE.Object3D>>()
// Серійна черга: один спільний renderer не перетинається між запитами.
let thumbChain: Promise<unknown> = Promise.resolve()
let thumbJobs = 0
let thumbIdleTimer: ReturnType<typeof setTimeout> | undefined

function scheduleThumbCleanup() {
  if (thumbJobs !== 0) return
  clearTimeout(thumbIdleTimer)
  thumbIdleTimer = setTimeout(() => {
    thumbIdleTimer = undefined
    if (thumbJobs !== 0) return
    thumbEnv?.renderer.setSize(1, 1, false)
    thumbEnv?.renderer.renderLists.dispose()
    // Jobs are counted before entering the serial queue, so no model is in use.
    for (const model of thumbModelCache.values()) {
      void model.then(root => root.traverse(obj => {
        const mesh = obj as THREE.Mesh
        if (!mesh.isMesh) return
        mesh.geometry.dispose()
        for (const mat of Array.isArray(mesh.material) ? mesh.material : [mesh.material]) {
          if ('map' in mat) (mat.map as THREE.Texture | null)?.dispose()
          mat.dispose()
        }
      })).catch(() => {})
    }
    thumbModelCache.clear()
  }, RESOURCE_IDLE_MS)
}

/** Зробити статичний 3D-кадр моделі зі скіном і повернути Blob (webp з альфою, фолбек — png). */
export function renderSkinThumbnail(opts: SkinThumbOptions): Promise<Blob> {
  clearTimeout(thumbIdleTimer)
  thumbJobs++
  const task = thumbChain.then(() => renderThumbOnce(opts)).finally(() => {
    thumbJobs--
    scheduleThumbCleanup()
  })
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

async function renderThumbOnce(opts: SkinThumbOptions): Promise<Blob> {
  const width = opts.width ?? 150
  const height = opts.height ?? 210
  const { renderer, scene, camera, group } = ensureThumbEnv()
  camera.aspect = width / height
  camera.updateProjectionMatrix()
  // setSize щоразу перевиділяє буфер і скидає стан canvas — міняємо лише коли розмір справді інший.
  renderer.getSize(_thumbSize)
  if (_thumbSize.x !== width || _thumbSize.y !== height) renderer.setSize(width, height, false)

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
  // Асинхронне кодування (як у Modrinth): toDataURL('image/png') кодував синхронно й
  // блокував потік на кожну картку. toBlob знімає кадр одразу, а кодує поза основним потоком.
  const blob = await canvasToBlob(renderer.domElement)
  group.remove(model)
  for (const m of matCopies) m.dispose()
  tex?.dispose()
  capeTex?.dispose()
  return blob
}

function canvasToBlob(canvas: HTMLCanvasElement): Promise<Blob> {
  const encode = (type: string, quality?: number) =>
    new Promise<Blob | null>((resolve) => canvas.toBlob((b) => resolve(b), type, quality))
  return encode('image/webp', 0.92).then(async (b) => {
    // Якщо webp не підтримується, браузер мовчки віддає png — обидва варіанти нам підходять.
    if (b) return b
    const png = await encode('image/png')
    if (!png) throw new Error('thumbnail encode failed')
    return png
  })
}
