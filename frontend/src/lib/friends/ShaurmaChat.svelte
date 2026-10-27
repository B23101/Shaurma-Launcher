<script lang="ts">
  // ── Чат з другом (Shaurma) ──────────────────────────────────────────
  // Один діалог. Текст шифрується/розшифровується ЛИШЕ в backend/internal/social —
  // сюди plaintext приходить уже готовим, ciphertext/nonce ми не тримаємо.
  //
  // СТАНИ СВОГО ПОВІДОМЛЕННЯ (видно і на бульбашці, і підписом під останнім):
  //   sending  — залишили лаунчер, чекаємо підтвердження сервера (chat:ack)
  //   sent     — сервер прийняв («Надіслано»)
  //   read     — друг прочитав («Прочитано», лише якщо в нього увімкнено)
  //   failed   — не надіслано: ЛИШЕ коли сервер явно відмовив (chat:failed / GetChatSendState) → «Повторити»
  //
  // МОДИ. Повідомлення з посиланням на Modrinth малюється карткою мода
  // (modShare.ts). Поділитися можна кнопкою у полі вводу або зі сторінки мода.
  //
  // «ЗМІНИТИ». СВОЄ текстове повідомлення можна відредагувати (меню → «Змінити»): текст
  // підставляється в поле вводу, Enter зберігає, Esc скасовує. Друг бачить нову версію
  // одразу (chat:edited) і позначку «змінено» біля часу.
  //
  // «ВИДАЛИТИ». СВОЄ повідомлення видаляється для всіх: з серверу й у друга (VPS → Worker).
  // ЧУЖЕ можна лише сховати в себе (у друга воно лишається) — підказка так і називається.
  //
  // ЗАКРІПЛЕНІ. До 5 на діалог, спільні для обох. Панель під шапкою: клік переходить до
  // повідомлення (завантажуючи його, якщо воно далеко) і перемикає на наступне закріплене.
  //
  // СКРОЛ (як у Telegram). Відкриваємо на першому непрочитаному (з роздільником «Нові»), а
  // якщо все прочитане — внизу. Старіше підвантажується саме при прокрутці вгору, новіше —
  // вниз після переходу до старого повідомлення. Позиція тримається, коли довантажуються
  // картки модів чи аватари (ResizeObserver), а не «з'їжджає».
  //
  // БЕЗПЕКА ВІДОБРАЖЕННЯ. Нік, назва збірки й адреса сервера санітизовані на
  // бекенді, але UI не довіряє наосліп: жодного {@html}, адреса показується
  // текстом, помилки — перекладом за кодом, а не сирим текстом сервера.
  import { onMount, tick, untrack } from 'svelte'
  import { fly, fade, scale, slide } from 'svelte/transition'
  import { cubicOut } from 'svelte/easing'
  import { App, on } from '../wails'
  import { toast } from '../toast.svelte'
import { windowActive } from '../windowActive.svelte'
  import { t } from '../i18n.svelte'
  import { tip } from './tooltip'
  import { parseModMessage, loadHidden, saveHidden } from './modShare'
  import { confirmDialog } from '../toast.svelte'
  import ModCard from './ModCard.svelte'
  import ShareCard from './ShareCard.svelte'
  import { parseShareMessage, parseShareCtl, ctlText, type ShareCtl } from './shareMsg'
  import { shareIn, shareOut, openTransfer } from './shareStore.svelte'
  import InviteCard from './InviteCard.svelte'
  import TempInviteModal from './TempInviteModal.svelte'
  import P2PJoinModal from '../servers/components/P2PJoinModal.svelte'
  import type {
    SocialFriend, SocialChatMessage, SocialChatEvent, SocialChatAck, SocialConn, TempInviteState, TempInvite,
    SocialTypingEvent, SocialReadEvent, SocialServerInviteEvent, SocialDeletedEvent, SocialEditedEvent, SocialPinsEvent,
    SocialReactionsEvent, SocialReaction,
  } from '../types'

  let {
    peer,
    me,
    head = '',
    conn = 'online',
    onBack,
    onOpenMod,
    onOpenProfile,
    allFriends = [] as SocialFriend[],
    isFriend = true,
  }: {
    peer: SocialFriend
    me: string
    head?: string
    conn?: SocialConn
    // Усі друзі — для «Запросити у мій світ» (можна запросити кількох одразу).
    allFriends?: SocialFriend[]
    // false — співрозмовник не друг (колишній або писав першим): без запрошень у світ.
    isFriend?: boolean
    // Є лише у вузькому режимі (список і діалог не вміщаються разом).
    onBack?: () => void
    // «Відкрити в лаунчері» на картці мода → сторінка мода в Браузері.
    onOpenMod?: (source: string, projectId: string) => void
    // Клік по аватарці/імені в заголовку → профіль друга (модальне вікно).
    onOpenProfile?: () => void
  } = $props()

  // ── Модель повідомлення в UI ─────────────────────────────────────────────
  type MsgState = 'sending' | 'sent' | 'read' | 'failed'
  interface Invite {
    id: string
    serverAddr: string
    packId?: string
    packName?: string
    expiresAt?: number
    revoked?: boolean
  }
  interface Msg {
    key: string
    id: number            // серверний id; 0 — ще не підтверджено
    from: string
    ts: number            // мілісекунди (нормалізуємо із секунд)
    bodyType: 'text' | 'server_invite'
    text?: string
    decryptFailed?: boolean
    sending?: boolean     // оптимістична бульбашка, ще без chat:ack
    failed?: boolean
    failReason?: string   // код помилки для підпису
    clientMsgId?: string
    localId?: number      // стабільний id локально створеного (ключ не міняється після ack)
    fresh?: boolean       // з'явилось у цій сесії → грає анімацію входу
    invite?: Invite
    replyToId?: number    // відповідь на повідомлення (0 = ні)
    editedAt?: number     // коли текст востаннє змінили (мс); 0/немає — оригінал
    reactions?: SocialReaction[] // реакції учасників (спільні, як закріплення)
  }
  type Row =
    | { kind: 'day'; key: string; ts: number }
    | { kind: 'new'; key: string }
    | { kind: 'msg'; key: string; m: Msg; groupEnd: boolean; groupStart: boolean }
    // Подія передачі збірки (службове повідомлення) — окремою «пігулкою» по центру, не бульбашкою.
    | { kind: 'share'; key: string; m: Msg; c: ShareCtl }

  const ACK_POLL_MS = 5000

  // ── Антиспам (дзеркало backend/internal/social/sendlimit.go) ─────────────
  // Бекенд — остаточний суддя; тут ті самі числа, щоб користувач одразу бачив
  // «зачекайте N с» і не втрачав набраний текст, а запит навіть не йшов у мережу.
  const SPAM_BURST_MAX = 5, SPAM_BURST_MS = 8000
  const SPAM_DUP_MAX = 3, SPAM_DUP_MS = 30000
  const SPAM_BACKEND_PAUSE_MS = 4000

  let messages = $state<Msg[]>([])
  let hidden = $state<Set<number>>(new Set())
  let loading = $state(true)
  let loadingMore = $state(false)
  let hasMore = $state(false)
  let input = $state('')
  let joining = $state(false)
  let peerTyping = $state(false)
  let peerReadUpTo = $state(0)
  let pinned = $state<Msg[]>([])       // закріплені (сервер віддає за порядком закріплення)
  // ── Відповідь (як у Telegram) ───────────────────────────────────────────
  let replyTo = $state<Msg | null>(null)   // повідомлення, на яке відповідаємо
  let editing = $state<Msg | null>(null)   // своє повідомлення, яке зараз змінюємо
  let editBusy = $state(false)
  // Повний набір реакцій для пікера («+» у панелі при наведенні та пункт «Реакція» в меню).
  // Раніше тут були ті самі емодзі, що й у панелі, — кнопка «+» відкривала дублікат. Тепер
  // пікер розширює вибір (перші шість — улюблені, далі решта).
  const ALL_REACT = [
    '👍', '❤️', '😂', '😮', '😢', '🔥', '👎', '😡',
    '🎉', '👏', '🙏', '🤝', '😍', '🤔', '💀', '👀',
    '💯', '✅', '😎', '🥲', '🤯', '😴', '🍕',
  ]
  // Швидкі реакції у плаваючій панелі при наведенні на повідомлення (як у Telegram).
  const HOVER_REACT = ['❤️', '👍', '😂', '🔥']
  let reactFor = $state<string | null>(null) // key повідомлення, для якого відкрито пікер
  // Яке закріплене «світиться» в панелі: найближче до середини видимої області за розташуванням
  // у чаті (а не за порядком закріплення). 0 — ще не обчислено. Клік веде саме до нього.
  let pinNearId = $state(0)
  let hasNewer = $state(false)         // після переходу до старого: нижче є ще повідомлення
  let loadingNewer = $state(false)
  let firstUnreadKey = $state('')      // перед ним малюємо роздільник «Нові повідомлення»
  let flashKey = $state('')
  let copiedKey = $state('')
  let unseen = $state(0)
  let atBottom = $state(true)
  // ── Виділення кількох повідомлень (як у Telegram Desktop) ────────────────
  let selecting = $state(false)
  let selected = $state<Set<string>>(new Set())
  let bulkBusy = $state(false)
  // Контекстне меню (правий клік) і виділення «протягуванням» затиснутою кнопкою миші.
  let menu = $state<{ x: number; y: number; m: Msg } | null>(null)
  // Останній відкритий стан меню. Під час out-переходу ({#if menu} + transition:scale)
  // `menu` вже null, але шаблон ще перераховується — читання menu.m/menu.x кидало
  // «Cannot read properties of null (reading 'm')» (напр. при видаленні повідомлення,
  // бо act() спершу ставить menu = null). Шаблон тому читає menuView, який не обнуляється.
  let menuView = $state<{ x: number; y: number; m: Msg } | null>(null)
  let menuEl: HTMLDivElement | undefined = $state()
  let dragStart = ''
  let dragging = false
  let dragBase = new Set<string>()
  let dragY = 0
  let dragDir = 0
  let dragTimer: ReturnType<typeof setInterval> | undefined
  let justDragged = false
  let lastSelKey = ''
  let spamLeft = $state(0)             // секунд до наступної можливої відправки (0 — можна)
  let spamDup = $state(false)          // причина — однакове повідомлення

  let scroller: HTMLDivElement | undefined = $state()
  let inner: HTMLDivElement | undefined = $state()
  // settling — перші секунди після відкриття: тримаємо обрану позицію (перший непрочитаний чи
  // низ), поки довантажуються картки/зображення. Будь-яка дія користувача вимикає.
  let settling = false
  let settleTimer: ReturnType<typeof setTimeout> | undefined
  let flashTimer: ReturnType<typeof setTimeout> | undefined
  let anchorKey = ''
  let ta: HTMLTextAreaElement | undefined = $state()
  let stickBottom = true
  let typingTimer: ReturnType<typeof setTimeout> | undefined
  let copiedTimer: ReturnType<typeof setTimeout> | undefined
  let lastTypingSent = 0
  let sentLog: { at: number; text: string }[] = []
  let spamUntil = 0
  let spamTimer: ReturnType<typeof setInterval> | undefined
  let markReadSent = 0
  let tmpSeq = 0
  const ackTimers = new Map<number, ReturnType<typeof setTimeout>>()
  // Ack-и, що прийшли раніше за clientMessageId (див. dispatch()).
  const orphanAcks = new Map<string, SocialChatAck>()

  // ── Допоміжне ────────────────────────────────────────────────────────────
  // chatId мусить дорівнювати канонічному на бекенді: менший accountId першим.
  const chatId = $derived(me && peer?.accountId && me !== peer.accountId
    ? (me < peer.accountId ? `${me}_${peer.accountId}` : `${peer.accountId}_${me}`)
    : '')

  // eslint-disable-next-line no-control-regex
  const HIDDEN_CH = /[\u0000-\u001f\u007f-\u009f\u200b-\u200f\u202a-\u202e\u2066-\u2069\ufeff]/g
  const clean = (s?: string) => (s ?? '').replace(HIDDEN_CH, '').trim()
  const initial = (s?: string) => clean(s).charAt(0).toUpperCase()

  // ── Виділення ────────────────────────────────────────────────────────────
  function startSelect(m: Msg) { selecting = true; selected = new Set([m.key]); lastSelKey = m.key }
  function exitSelect() { selecting = false; selected = new Set(); lastSelKey = '' }
  // Клік — перемкнути; Shift+клік — виділити діапазон від попереднього кліку.
  function toggleSel(key: string, range = false) {
    const next = new Set(selected)
    if (range && lastSelKey && lastSelKey !== key) {
      const keys = visible.map(m => m.key)
      const a = keys.indexOf(lastSelKey), b = keys.indexOf(key)
      if (a >= 0 && b >= 0) for (let i = Math.min(a, b); i <= Math.max(a, b); i++) next.add(keys[i])
    } else if (next.has(key)) next.delete(key)
    else next.add(key)
    lastSelKey = key
    selected = next
    if (next.size === 0) exitSelect()
  }
  // ── Анімації ─────────────────────────────────────────────────────────────
  const reduceMotion = () => typeof matchMedia !== 'undefined' && matchMedia('(prefers-reduced-motion: reduce)').matches
  let noOut = false          // масова заміна стрічки (перехід до старого): без анімації зникнення
  let intro = $state(false)  // кілька сотень мс після відкриття діалогу: стрічка «виїжджає» каскадом
  let introTimer: ReturnType<typeof setTimeout> | undefined
  let sendFly = $state(false)
  let sendFlyTimer: ReturnType<typeof setTimeout> | undefined

  // Нове повідомлення плавно розсуває стрічку (замість різкого стрибка), решта — без анімації.
  function rowIn(node: Element, p: { on: boolean }) {
    if (!p.on || reduceMotion()) return { duration: 0 }
    return slide(node, { duration: 280, easing: cubicOut })
  }
  // Видалене повідомлення від'їжджає вбік, згасає й згортається по висоті — сусіди підтягуються плавно.
  function rowOut(node: HTMLElement, p: { dir: number }) {
    if (noOut || reduceMotion()) return { duration: 0 }
    const h = node.offsetHeight
    return {
      duration: 260, easing: cubicOut,
      css: (t: number, u: number) => `opacity:${t};transform:translateX(${p.dir * u * 40}px) scale(${0.96 + 0.04 * t});height:${h * t}px;overflow:hidden;`,
    }
  }
  function playIntro() {
    intro = true
    if (introTimer) clearTimeout(introTimer)
    introTimer = setTimeout(() => { intro = false }, 1000)
  }
  function flySend() {
    sendFly = true
    if (sendFlyTimer) clearTimeout(sendFlyTimer)
    sendFlyTimer = setTimeout(() => { sendFly = false }, 460)
  }

  function portal(node: HTMLElement) {
    document.body.appendChild(node)
    return { destroy() { node.remove() } }
  }
  function openMenu(e: MouseEvent, m: Msg) {
    e.preventDefault()
    if (selecting) return
    menuView = { x: Math.min(e.clientX, window.innerWidth - 220), y: Math.min(e.clientY, window.innerHeight - 40), m }
    menu = menuView
  }
  const act = (fn: () => void) => { menu = null; fn() }
  // Після відкриття міряємо меню й не даємо йому вилізти за межі вікна.
  $effect(() => {
    if (!menu || !menuEl) return
    const r = menuEl.getBoundingClientRect()
    if (r.right > window.innerWidth - 8) menuEl.style.left = Math.max(8, window.innerWidth - r.width - 8) + 'px'
    if (r.bottom > window.innerHeight - 8) menuEl.style.top = Math.max(8, window.innerHeight - r.height - 8) + 'px'
  })

  // Виділення протягуванням: затисни ліву кнопку на повідомленні й веди на інші (як у Telegram).
  // Всередині одного повідомлення працює звичайне виділення тексту.
  function onDragDown(e: MouseEvent, key: string) {
    if (e.button !== 0 || menu) return
    if ((e.target as HTMLElement).closest('button, a, input, textarea')) return
    dragStart = key
    dragging = false
    dragBase = new Set(selecting ? selected : [])
    dragY = e.clientY
  }
  function dragUpdate() {
    if (!dragStart || !scroller) return
    const r = scroller.getBoundingClientRect()
    const y = Math.min(Math.max(dragY, r.top + 1), r.bottom - 1)
    const el = document.elementFromPoint(r.left + r.width / 2, y) as HTMLElement | null
    const key = (el?.closest?.('[data-key]') as HTMLElement | null)?.dataset.key ?? ''
    const keys = visible.map(m => m.key)
    const a = keys.indexOf(dragStart), b = keys.indexOf(key)
    if (a < 0 || b < 0) return
    if (!dragging) {
      if (a === b) return
      dragging = true; selecting = true; userTookOver()
    }
    const next = new Set(dragBase)
    for (let i = Math.min(a, b); i <= Math.max(a, b); i++) next.add(keys[i])
    selected = next
    lastSelKey = key
    window.getSelection()?.removeAllRanges()
  }
  function onDragMove(e: MouseEvent) {
    if (!dragStart) return
    if (!(e.buttons & 1)) { endDrag(); return }
    dragY = e.clientY
    dragUpdate()
    const r = scroller?.getBoundingClientRect()
    dragDir = !dragging || !r ? 0 : dragY < r.top + 36 ? -1 : dragY > r.bottom - 36 ? 1 : 0
    if (dragDir && !dragTimer) {
      dragTimer = setInterval(() => {
        if (!dragDir || !scroller) return
        scroller.scrollTop += dragDir * 12
        dragUpdate()
      }, 16)
    } else if (!dragDir && dragTimer) { clearInterval(dragTimer); dragTimer = undefined }
  }
  function endDrag() {
    if (dragging) { justDragged = true; setTimeout(() => { justDragged = false }, 0) }
    dragStart = ''; dragging = false; dragDir = 0
    if (dragTimer) { clearInterval(dragTimer); dragTimer = undefined }
  }

  function onRowClickCapture(e: MouseEvent, key: string) {
    if (!selecting) return
    if (justDragged) { e.preventDefault(); e.stopPropagation(); return }
    // Перехоплюємо, щоб кнопки/посилання/картки модів усередині не спрацьовували.
    e.preventDefault(); e.stopPropagation()
    toggleSel(key, e.shiftKey)
  }

  // Копіювання пачки: одне повідомлення — чистий текст; кілька — «Автор, [дата час]» + текст (як у Telegram).
  async function copySelected() {
    const list = selectedMsgs.filter(m => !m.decryptFailed)
    if (!list.length || bulkBusy) return
    const text = list.length === 1
      ? copyPayload(list[0])
      : list.map(m => {
          const who = m.from === me ? t('social.chat.you') : clean(peer.nickname)
          return `${who}, [${new Date(m.ts).toLocaleDateString()} ${timeOf(m.ts)}]\n${copyPayload(m)}`
        }).join('\n\n')
    if (!text) return
    try { await App.CopyToClipboard(text) } catch {}
    toast(t('social.chat.copiedN', { n: list.length }), 'success')
    exitSelect()
  }

  // Видалення пачки: СВОЇ → для всіх (сервер + друг), ЧУЖІ → лише в себе, невдалі → прибрати.
  async function deleteSelected() {
    if (!selectedMsgs.length || bulkBusy || !peer?.accountId) return
    const list = selectedMsgs
    const failedL = list.filter(m => m.failed)
    const mine = list.filter(m => !m.failed && m.id > 0 && isOwn(m) && m.bodyType === 'text')
    const local = list.filter(m => !m.failed && m.id > 0 && !(isOwn(m) && m.bodyType === 'text'))
    if (!failedL.length && !mine.length && !local.length) { exitSelect(); return }
    const ok = await confirmDialog(
      t('social.chat.deleteSelTitle', { n: list.length }),
      mine.length
        ? t('social.chat.deleteSelText', { n: mine.length, name: clean(peer.nickname) })
        : t('social.chat.deleteSelLocalText'),
      { confirmLabel: t('social.chat.deleteAllConfirm'), cancelLabel: t('social.chat.deleteAllCancel'), danger: true })
    if (!ok) return
    bulkBusy = true
    try {
      failedL.forEach(discardFailed)
      if (local.length) {
        const next = new Set(hidden)
        local.forEach(m => next.add(m.id))
        hidden = next
        saveHidden(me, chatId, next)
      }
      // По одному й послідовно: кожне видалення — запит до сервера, не штурмуємо його паралеллю.
      for (const m of mine) {
        try { await App.DeleteChatMessage(peer.accountId, m.id); removeLocal(m.id) }
        catch (e) { toast(errText(e), 'error'); break }
      }
    } finally {
      bulkBusy = false
      exitSelect()
    }
  }

  // Скільки мс чекати до дозволеної відправки (0 — можна зараз).
  function spamWaitMs(text: string, now = Date.now()): { ms: number; dup: boolean } {
    sentLog = sentLog.filter(e => now - e.at < SPAM_DUP_MS)
    const norm = text.trim().toLowerCase()
    const burst = sentLog.filter(e => now - e.at < SPAM_BURST_MS)
    const dups = sentLog.filter(e => e.text === norm)
    let ms = 0, dup = false
    if (burst.length >= SPAM_BURST_MAX) ms = burst[burst.length - SPAM_BURST_MAX].at + SPAM_BURST_MS - now
    if (dups.length >= SPAM_DUP_MAX) {
      const w = dups[dups.length - SPAM_DUP_MAX].at + SPAM_DUP_MS - now
      if (w > ms) { ms = w; dup = true }
    }
    return { ms: Math.max(0, ms), dup }
  }
  function startCooldown(ms: number, dup = false) {
    spamUntil = Date.now() + ms
    spamDup = dup
    const tickLeft = () => {
      const left = Math.ceil((spamUntil - Date.now()) / 1000)
      spamLeft = left > 0 ? left : 0
      if (left <= 0 && spamTimer) { clearInterval(spamTimer); spamTimer = undefined }
    }
    tickLeft()
    if (spamLeft > 0 && !spamTimer) spamTimer = setInterval(tickLeft, 250)
  }

  function normalizeTs(ts: number): number {
    if (!ts || ts <= 0) return Date.now()
    return ts < 1e12 ? ts * 1000 : ts   // секунди → мс
  }

  function errCode(e: unknown): string {
    const msg = String((e as any)?.message ?? e ?? '')
    const m = /social:([A-Za-z_ ]+)/.exec(msg)
    return m ? m[1].trim().replace(/ /g, '_') : ''
  }
  function errText(e: unknown): string {
    const code = errCode(e)
    const key = `social.err.${code}`
    const txt = code ? t(key) : ''
    return txt && txt !== key ? txt : t('social.err.unknown')
  }

  const isOwn = (m: Msg) => m.from === me
  const stateOf = (m: Msg): MsgState =>
    m.failed ? 'failed' :
    m.sending ? 'sending' :
    (m.id > 0 && peerReadUpTo >= m.id) ? 'read' : 'sent'

  const peerStatus = $derived(peer?.presence?.status ?? 'offline')
  const peerStatusText = $derived(
    peerStatus === 'in_game' ? (clean(peer?.presence?.packName) || t('social.playing')) :
    peerStatus === 'away' ? t('social.status.away') :
    peerStatus === 'online' ? t('social.status.online') : t('social.status.offline'))

  // ── Рядки стрічки: роздільники днів + групування за відправником ─────────
  const visible = $derived(messages.filter(m => !(m.id > 0 && hidden.has(m.id))))
  const selectedMsgs = $derived(visible.filter(m => selected.has(m.key)))
  const dayKey = (ts: number) => { const d = new Date(ts); return `${d.getFullYear()}.${d.getMonth()}.${d.getDate()}` }
  const ctlOf = (m: Msg) => (m.bodyType === 'text' && !m.decryptFailed ? parseShareCtl(m.text) : null)
  const rows = $derived.by<Row[]>(() => {
    const out: Row[] = []
    const v = visible
    // Однакові події передачі (повтор після перезапуску лаунчера) показуємо один раз.
    const seenCtl = new Set<string>()
    for (let i = 0; i < v.length; i++) {
      const m = v[i], prev = v[i - 1], next = v[i + 1]
      const newDay = !prev || dayKey(prev.ts) !== dayKey(m.ts)
      if (newDay) out.push({ kind: 'day', key: 'd' + dayKey(m.ts), ts: m.ts })
      if (firstUnreadKey && m.key === firstUnreadKey) out.push({ kind: 'new', key: 'new-divider' })
      const c = ctlOf(m)
      if (c) {
        const k = `${c.sid}|${c.e}|${c.r ?? ''}|${m.from}`
        if (!seenCtl.has(k)) { seenCtl.add(k); out.push({ kind: 'share', key: m.key, m, c }) }
        continue
      }
      const near = (a: Msg, b: Msg) => a.from === b.from && Math.abs(b.ts - a.ts) < 120000 && dayKey(a.ts) === dayKey(b.ts)
      out.push({
        kind: 'msg', key: m.key, m,
        groupStart: !prev || newDay || !near(prev, m),
        groupEnd: !next || !near(m, next),
      })
    }
    return out
  })
  const pinnedIds = $derived(new Set(pinned.map(p => p.id)))
  // Закріплені в порядку розташування в чаті: давніше — зверху сегментів, новіше — знизу.
  const pinnedSorted = $derived([...pinned].sort((a, b) => (a.ts - b.ts) || (a.id - b.id)))
  // Індекс «світного» сегмента. Якщо ще не обчислено (або його відкріпили) — останнє за розташуванням.
  const pinActiveIdx = $derived.by(() => {
    if (pinnedSorted.length === 0) return -1
    const i = pinnedSorted.findIndex(p => p.id === pinNearId)
    return i >= 0 ? i : pinnedSorted.length - 1
  })
  // Останнє МОЄ повідомлення — під ним підпис стану.
  const lastOwnKey = $derived.by(() => {
    for (let i = visible.length - 1; i >= 0; i--) if (isOwn(visible[i])) return visible[i].key
    return ''
  })

  // ── Скрол ────────────────────────────────────────────────────────────────
  function userTookOver() { settling = false; anchorKey = '' }

  // Найближче до середини видимої області закріплене. Міряємо у пікселях по тих, що вже в DOM;
  // якщо жодне не завантажене (історія далеко) — вирішує порядок: останнє, що не пізніше за
  // повідомлення посередині екрана (а якщо всі пізніше — найстаріше).
  let pinRaf = 0
  function schedulePinTrack() {
    if (pinRaf) return
    pinRaf = requestAnimationFrame(() => { pinRaf = 0; trackNearestPin() })
  }
  function trackNearestPin() {
    const el = scroller
    if (!el || pinnedSorted.length === 0) { pinNearId = 0; return }
    const box = el.getBoundingClientRect()
    const mid = box.top + box.height / 2
    let best = 0, bestDist = Infinity
    for (const p of pinnedSorted) {
      const pe = elByKey(p.key)
      if (!pe) continue
      const r = pe.getBoundingClientRect()
      const d = mid < r.top ? r.top - mid : mid > r.bottom ? mid - r.bottom : 0
      if (d < bestDist) { bestDist = d; best = p.id }
    }
    if (!best) {
      // Жодне закріплене не в DOM — порівнюємо за позицією з повідомленням посередині.
      let ref: Msg | undefined
      for (const m of visible) {
        const me2 = m.id > 0 ? elByKey(m.key) : null
        if (!me2) continue
        const r = me2.getBoundingClientRect()
        if (r.top <= mid) ref = m
        if (r.top > mid) break
      }
      if (ref) {
        const r0 = ref
        const before = pinnedSorted.filter(p => p.ts < r0.ts || (p.ts === r0.ts && p.id <= r0.id))
        best = (before.length ? before[before.length - 1] : pinnedSorted[0]).id
      } else {
        best = pinnedSorted[pinnedSorted.length - 1].id
      }
    }
    if (best !== pinNearId) pinNearId = best
  }
  // Закріплені чи повідомлення змінились — перерахувати «світний» сегмент.
  $effect(() => { void pinned; void messages; void loading; if (scroller) tick().then(schedulePinTrack) })

  function onScroll() {
    const el = scroller
    if (!el) return
    schedulePinTrack()
    const near = el.scrollHeight - el.scrollTop - el.clientHeight < 60
    stickBottom = near && !hasNewer
    atBottom = near && !hasNewer
    if (near && !hasNewer) unseen = 0
    if (settling || loading) return
    // Нескінченна прокрутка: ближче до верху — підвантажуємо старіше, до низу — новіше.
    if (el.scrollTop < 240 && hasMore && !loadingMore) loadHistory(messages[0]?.ts ?? 0, true)
    if (near && hasNewer && !loadingNewer) loadNewer()
  }
  async function scrollToBottom(force = false, smooth = false) {
    if (!force && !stickBottom) return
    await tick()
    if (scroller) scroller.scrollTo({ top: scroller.scrollHeight, behavior: smooth ? 'smooth' : 'auto' })
  }
  function elByKey(key: string): HTMLElement | null {
    return scroller?.querySelector<HTMLElement>(`[data-key="${CSS.escape(key)}"]`) ?? null
  }
  function scrollToKey(key: string, block: 'start' | 'center' = 'center', smooth = false) {
    const el = elByKey(key)
    if (!el || !scroller) return false
    if (block === 'start') {
      scroller.scrollTop += el.getBoundingClientRect().top - scroller.getBoundingClientRect().top - 40
    } else {
      el.scrollIntoView({ block: 'center', behavior: smooth ? 'smooth' : 'auto' })
    }
    return true
  }
  function flash(key: string) {
    flashKey = key
    if (flashTimer) clearTimeout(flashTimer)
    flashTimer = setTimeout(() => { flashKey = '' }, 1700)
  }
  // Повертає позицію, яку ми обрали при відкритті, після зміни висоти вмісту.
  function reapplyPosition() {
    if (!scroller) return
    if (anchorKey) { scrollToKey(anchorKey, 'start'); return }
    if (settling || stickBottom) scroller.scrollTop = scroller.scrollHeight
  }

  // Живе запрошення приходить без messageId. Дістаємо збережену версію з
  // останньої сторінки історії та підміняємо нею локальну картку (id=0) —
  // інакше реакції на P2P-картку недоступні до перезаходу в чат.
  async function reconcileInvite(inviteId: string, attempt = 0) {
    if (!inviteId || !peer?.accountId) return
    try {
      const page = await App.GetChatHistory(peer.accountId, 0, 30)
      const stored = (page?.messages ?? []).find(m => m.invite?.id === inviteId)
      if (!stored) {
        if (attempt < 2) setTimeout(() => { void reconcileInvite(inviteId, attempt + 1) }, 700)
        return
      }
      const fresh = toMsg(stored)
      messages = messages.map(m =>
        m.bodyType === 'server_invite' && m.invite?.id === inviteId && m.id <= 0 ? fresh : m)
    } catch { /* мережевий збій — картка лишиться локальною до перезаходу */ }
  }

  // ── Перетворення історії ─────────────────────────────────────────────────
  function toMsg(m: SocialChatMessage): Msg {
    return {
      key: 's' + m.id,
      id: m.id,
      from: m.from,
      ts: normalizeTs(m.createdAt),
      bodyType: m.bodyType,
      text: m.plaintext,
      decryptFailed: m.decryptFailed,
      replyToId: m.replyToId || 0,
      editedAt: m.editedAt ? normalizeTs(m.editedAt) : 0,
      reactions: (m.reactions ?? []).filter(r => r && r.by && r.emoji),
      invite: m.invite ? {
        id: m.invite.id, serverAddr: m.invite.serverAddr, packId: m.invite.packId,
        packName: m.invite.packName, expiresAt: m.invite.expiresAt, revoked: m.invite.revoked,
      } : undefined,
    }
  }

  function mergeSorted(a: Msg[], b: Msg[]): Msg[] {
    const seen = new Set(a.map(m => m.key))
    const all = [...a, ...b.filter(m => !seen.has(m.key))]
    return all.sort((x, y) => (x.ts - y.ts) || (x.id - y.id))
  }
  // Перше непрочитане: рахуємо з кінця повідомлення співрозмовника (лічильник — лише text).
  function findFirstUnread(list: Msg[], n: number): string {
    if (n <= 0) return ''
    let left = n, key = ''
    for (let i = list.length - 1; i >= 0 && left > 0; i--) {
      const m = list[i]
      if (m.from !== me && m.bodyType === 'text') { key = m.key; left-- }
    }
    return key
  }

  // Найновіша сторінка (відкриття чату або повернення «до нових»).
  async function loadLatest(openAtUnread = true) {
    if (!peer?.accountId) { loading = false; return }
    loading = true
    try {
      let unread = 0
      if (openAtUnread) {
        try { unread = (await App.GetFriendsState())?.unread?.[peer.accountId] ?? 0 } catch {}
      }
      const page = await App.GetChatHistory(peer.accountId, 0, 50)
      // Курсори «прочитано» з найновішої сторінки: інакше після перезаходу в чат
      // галочки «переглянуто» не з'являлись би до живої події chat:read від друга.
      for (const r of page?.reads ?? []) {
        if (r?.by === peer.accountId && r.upTo > 0) peerReadUpTo = Math.max(peerReadUpTo, r.upTo)
      }
      messages = (page?.messages ?? []).map(toMsg)
      pinned = (page?.pinned ?? []).map(toMsg)
      pinNearId = 0
      hasMore = !!page?.hasMore
      hasNewer = false
      firstUnreadKey = findFirstUnread(messages, unread)
      anchorKey = firstUnreadKey
      settling = true
      stickBottom = !firstUnreadKey
      if (settleTimer) clearTimeout(settleTimer)
      settleTimer = setTimeout(() => { settling = false; anchorKey = '' }, 3000)
      loading = false
      playIntro()
      await tick()
      reapplyPosition()
      onScroll()
      markReadUpToLatest()
    } catch (e) {
      toast(errText(e), 'error')
    } finally {
      loading = false
    }
  }

  // Старіше (prepend=true) — позиція лишається на тому ж місці.
  async function loadHistory(before = 0, prepend = false) {
    if (!prepend) { await loadLatest(); return }
    if (!peer?.accountId || loadingMore || !before) return
    loadingMore = true
    try {
      // before — ЧАС (мс) найстарішого показаного повідомлення, а не id.
      const page = await App.GetChatHistory(peer.accountId, before, 50)
      const mapped = (page?.messages ?? []).map(toMsg)
      const seen = new Set(messages.map(m => m.key))
      const prevH = scroller?.scrollHeight ?? 0
      const prevTop = scroller?.scrollTop ?? 0
      messages = [...mapped.filter(m => !seen.has(m.key)), ...messages]
      hasMore = !!page?.hasMore
      await tick()
      if (scroller) scroller.scrollTop = prevTop + (scroller.scrollHeight - prevH)
    } catch (e) {
      toast(errText(e), 'error')
    } finally {
      loadingMore = false
    }
  }

  // Новіше після переходу до старого повідомлення.
  async function loadNewer() {
    if (!peer?.accountId || loadingNewer || !hasNewer) return
    let last = 0
    for (const m of messages) if (m.id > 0 && m.ts > last) last = m.ts
    if (!last) { hasNewer = false; return }
    loadingNewer = true
    try {
      // -1 мс: повідомлення з тією самою мілісекундою не губляться (дублі відсікає ключ).
      const page = await App.GetChatHistoryAfter(peer.accountId, last - 1, 50)
      messages = mergeSorted(messages, (page?.messages ?? []).map(toMsg))
      hasNewer = !!page?.hasMore
      if (!hasNewer) { await tick(); onScroll(); markReadUpToLatest() }
    } catch (e) {
      toast(errText(e), 'error')
    } finally {
      loadingNewer = false
    }
  }

  async function returnToLatest() {
    unseen = 0
    await loadLatest(false)
    await scrollToBottom(true)
  }

  // Перехід до повідомлення (закріплене тощо). Якщо його ще нема в стрічці — вантажимо
  // вікно навколо нього, а новіше дотягується при прокрутці вниз.
  async function jumpTo(id: number, ts: number) {
    userTookOver()
    const key = 's' + id
    if (elByKey(key)) { scrollToKey(key, 'center', true); flash(key); return }
    try {
      const [older, newer] = await Promise.all([
        App.GetChatHistory(peer.accountId, ts + 1, 30),
        App.GetChatHistoryAfter(peer.accountId, Math.max(ts - 1, 1), 30),
      ])
      const win = mergeSorted((older?.messages ?? []).map(toMsg), (newer?.messages ?? []).map(toMsg))
      if (!win.some(m => m.key === key)) { toast(t('social.err.not_found'), 'error'); return }
      noOut = true
      messages = win
      hasMore = !!older?.hasMore
      hasNewer = !!newer?.hasMore
      firstUnreadKey = ''
      stickBottom = false
      atBottom = false
      await tick()
      noOut = false
      scrollToKey(key, 'center')
      flash(key)
    } catch (e) {
      toast(errText(e), 'error')
    }
  }

  function lastServerId(): number {
    // МАКСИМУМ, а не «останній у списку»: рядок запрошення на сервер має власний
    // (малий) id і міг би опинитись останнім, тоді непрочитане не скинулось би.
    let max = 0
    for (const m of messages) if (m.id > max) max = m.id
    return max
  }
  // «Прочитано» лише коли користувач справді може бачити діалог: вікно лаунчера
  // активне. Неактивне вікно (згорнули, переключились у гру/іншу програму) — це НЕ
  // «переглянув»: повідомлення лишається непрочитаним, а тост показує бекенд. Коли
  // вікно знову стає активним, effect нижче дочитує до останнього повідомлення.
  function markReadUpToLatest() {
    if (!windowActive.active) return
    const id = lastServerId()
    if (id > markReadSent && peer?.accountId) {
      markReadSent = id
      App.MarkChatRead(peer.accountId, id).catch(() => {})
    }
  }

  // Вікно знову активне — дочитуємо те, що прийшло, поки воно було неактивним.
  // Залежимо ЛИШЕ від windowActive.active: решта читається в untrack, щоб не
  // позначати прочитаним при кожній зміні списку повідомлень.
  $effect(() => {
    if (!windowActive.active) return
    untrack(() => { if (!loading && messages.length) markReadUpToLatest() })
  })

  // ── Підписки на події ────────────────────────────────────────────────────
  onMount(() => {
    hidden = loadHidden(me, chatId)
    loadLatest()
    const ro = new ResizeObserver(() => reapplyPosition())
    if (inner) ro.observe(inner)
    const onKey = (e: KeyboardEvent) => {
      if (menu) { if (e.key === 'Escape') { e.preventDefault(); menu = null } return }
      if (reactFor) { if (e.key === 'Escape') { e.preventDefault(); reactFor = null } return }
      if (replyTo && e.key === 'Escape') { e.preventDefault(); replyTo = null; return }
      if (!selecting) return
      const mod = e.ctrlKey || e.metaKey
      if (e.key === 'Escape') { e.preventDefault(); exitSelect() }
      else if (mod && e.key.toLowerCase() === 'a') { e.preventDefault(); selected = new Set(visible.map(m => m.key)) }
      else if (mod && e.key.toLowerCase() === 'c') { e.preventDefault(); copySelected() }
      else if (e.key === 'Delete') { e.preventDefault(); deleteSelected() }
    }
    const onDown = (e: PointerEvent) => {
      if (menu && !(e.target as HTMLElement)?.closest?.('.sc-menu')) menu = null
      if (reactFor && !(e.target as HTMLElement)?.closest?.('.sc-reactpick')) reactFor = null
    }
    const closeMenu = () => { menu = null; reactFor = null }
    window.addEventListener('keydown', onKey)
    window.addEventListener('pointerdown', onDown, true)
    window.addEventListener('wheel', closeMenu, { passive: true })
    window.addEventListener('blur', closeMenu)
    window.addEventListener('mousemove', onDragMove)
    window.addEventListener('mouseup', endDrag)
    const offs = [
      on('chat:message', (e: SocialChatEvent) => {
        if (!e || e.chatId !== chatId || !chatId) return
        if (e.id && messages.some(m => m.id === e.id)) return  // дедуп з історією
        if (hasNewer) { if (e.from !== me) unseen++; return }   // між нами й кінцем — прогалина; дотягнеться вниз
        messages = [...messages, {
          key: e.id ? 's' + e.id : 'ev' + (++tmpSeq),
          id: e.id || 0,
          from: e.from,
          ts: normalizeTs(e.ts),
          bodyType: e.bodyType === 'server_invite' ? 'server_invite' : 'text',
          text: e.plaintext,
          decryptFailed: e.decryptFailed,
          replyToId: e.replyToId || 0,
          fresh: true,
        }]
        if (e.from !== me) {
          peerTyping = false
          if (!stickBottom) unseen++
          // Вікно неактивне — не позначаємо прочитаним (див. markReadUpToLatest).
          if (windowActive.active && e.id && e.id > markReadSent && peer?.accountId) {
            markReadSent = e.id
            App.MarkChatRead(peer.accountId, e.id).catch(() => {})
          }
        }
        scrollToBottom(false, true)
      }),
      on('chat:ack', (a: SocialChatAck) => {
        if (!a || a.chatId !== chatId) return
        let hit = false
        messages = messages.map(m => {
          if (m.clientMsgId && m.clientMsgId === a.clientMessageId) {
            hit = true
            if (m.localId) clearAckTimer(m.localId)
            // key НЕ міняємо: інакше Svelte перемонтує бульбашку і вона блимне.
            return { ...m, id: a.id, ts: normalizeTs(a.ts), sending: false, failed: false, failReason: undefined }
          }
          return m
        })
        // Ack міг прийти раніше за відповідь SendChatMessage — збережемо.
        if (!hit) orphanAcks.set(a.clientMessageId, a)
      }),
      on('chat:failed', (f: { clientMessageId: string; code: string }) => {
        if (!f) return
        messages = messages.map(m => {
          if (m.clientMsgId && m.clientMsgId === f.clientMessageId && m.sending) {
            if (m.localId) clearAckTimer(m.localId)
            return { ...m, sending: false, failed: true, failReason: f.code || 'unknown' }
          }
          return m
        })
      }),
      on('chat:typing', (e: SocialTypingEvent) => {
        if (!e || e.chatId !== chatId || e.accountId !== peer?.accountId) return
        peerTyping = true
        if (stickBottom) scrollToBottom(false, true)
        if (typingTimer) clearTimeout(typingTimer)
        typingTimer = setTimeout(() => { peerTyping = false }, 4000)
      }),
      on('chat:read', (e: SocialReadEvent) => {
        if (!e || e.chatId !== chatId) return
        peerReadUpTo = Math.max(peerReadUpTo, e.upToMessageId || 0)
      }),
      on('chat:server_invite', (e: SocialServerInviteEvent) => {
        if (!e || e.chatId !== chatId) return
        // Те саме запрошення могло вже прийти з історії або ехом — другу картку не додаємо.
        if (e.inviteId && messages.some(m => m.invite?.id === e.inviteId)) return
        messages = [...messages, {
          key: 'inv' + (e.inviteId || (++tmpSeq)),
          id: 0,
          from: e.from,
          ts: Date.now(),
          bodyType: 'server_invite',
          fresh: true,
          invite: {
            id: e.inviteId, serverAddr: e.serverAddr,
            packId: e.packId, packName: e.packName,
          },
        }]
        if (!stickBottom) unseen++
        scrollToBottom(false, true)
        // Жива подія не несе серверного id повідомлення (Worker шле лише
        // inviteId), тож щойно додана картка має id=0 — на неї не можна
        // поставити реакцію (ReactChatMessage вимагає messageID > 0).
        // Підтягуємо збережене повідомлення з історії й замінюємо локальну
        // картку на «справжню» з id/реакціями.
        if (e.inviteId) void reconcileInvite(e.inviteId)
      }),
      on('chat:deleted', (e: SocialDeletedEvent) => {
        if (!e || e.chatId !== chatId) return
        removeLocal(e.messageId)
      }),
      on('chat:edited', (e: SocialEditedEvent) => {
        if (!e || e.chatId !== chatId || !e.messageId) return
        const patch = (m: Msg): Msg => (m.id === e.messageId
          ? { ...m, text: e.plaintext, decryptFailed: !!e.decryptFailed, editedAt: e.editedAt ? normalizeTs(e.editedAt) : Date.now() }
          : m)
        messages = messages.map(patch)
        pinned = pinned.map(patch)
        // Якщо саме це повідомлення зараз редагується, а його змінили деінде — не тримаємо застарілу чернетку.
        if (editing && editing.id === e.messageId && e.by !== me) cancelEdit()
      }),
      on('chat:pins', (e: SocialPinsEvent) => {
        if (!e || e.chatId !== chatId) return
        pinned = (e.pinned ?? []).map(toMsg)
        schedulePinTrack()
      }),
      on('chat:reactions', (e: SocialReactionsEvent) => {
        if (!e || e.chatId !== chatId || !e.messageId) return
        messages = messages.map(m => (m.id === e.messageId
          ? { ...m, reactions: (e.reactions ?? []).filter(r => r && r.by && r.emoji) }
          : m))
      }),
      on('chat:server_invite_revoked', (inviteId: string) => {
        if (!inviteId) return
        messages = messages.map(m =>
          m.invite && m.invite.id === inviteId ? { ...m, invite: { ...m.invite, revoked: true } } : m)
      }),
    ]
    return () => {
      ro.disconnect()
      window.removeEventListener('keydown', onKey)
      window.removeEventListener('pointerdown', onDown, true)
      window.removeEventListener('wheel', closeMenu)
      window.removeEventListener('blur', closeMenu)
      window.removeEventListener('mousemove', onDragMove)
      window.removeEventListener('mouseup', endDrag)
      if (dragTimer) clearInterval(dragTimer)
      if (settleTimer) clearTimeout(settleTimer)
      if (flashTimer) clearTimeout(flashTimer)
      offs.forEach(off => off())
      if (pinRaf) cancelAnimationFrame(pinRaf)
      if (typingTimer) clearTimeout(typingTimer)
      if (copiedTimer) clearTimeout(copiedTimer)
      if (spamTimer) clearInterval(spamTimer)
      if (introTimer) clearTimeout(introTimer)
      if (sendFlyTimer) clearTimeout(sendFlyTimer)
      ackTimers.forEach(clearTimeout)
      ackTimers.clear()
    }
  })

  // ── Надсилання ───────────────────────────────────────────────────────────
  function patchLocal(localId: number, patch: Partial<Msg>) {
    messages = messages.map(m => (m.localId === localId ? { ...m, ...patch } : m))
  }
  function armAckPoll(localId: number, cid: string) {
    clearAckTimer(localId)
    ackTimers.set(localId, setTimeout(async () => {
      ackTimers.delete(localId)
      try {
        const st = await App.GetChatSendState(cid)
        if (st.status === 'acked') {
          patchLocal(localId, { id: st.id || 0, ts: st.ts ? normalizeTs(st.ts) : Date.now(), sending: false, failed: false, failReason: undefined })
        } else if (st.status === 'failed') {
          patchLocal(localId, { sending: false, failed: true, failReason: st.code || 'unknown' })
        } else {
          armAckPoll(localId, cid) // pending/unknown: чекаємо далі, не оголошуємо помилку
        }
      } catch {
        armAckPoll(localId, cid)
      }
    }, ACK_POLL_MS))
  }

  function clearAckTimer(localId: number) {
    const tm = ackTimers.get(localId)
    if (tm) { clearTimeout(tm); ackTimers.delete(localId) }
  }

  // Відправка одного повідомлення (нового або повтор невдалого).
  async function dispatch(localId: number, text: string, resendId = '', replyToID = 0) {
    clearAckTimer(localId)
    patchLocal(localId, { sending: true, failed: false, failReason: undefined })
    try {
      const cid = resendId
        ? await App.RetryChatMessage(peer.accountId, text, resendId) // той самий ID → без дубля
        : replyToID > 0
          ? await App.SendChatMessageReply(peer.accountId, text, replyToID)
          : await App.SendChatMessage(peer.accountId, text)
      const early = orphanAcks.get(cid)
      if (early) {
        orphanAcks.delete(cid)
        patchLocal(localId, { clientMsgId: cid, id: early.id, ts: normalizeTs(early.ts), sending: false })
        return
      }
      patchLocal(localId, { clientMsgId: cid })
      // «Не надіслано» — лише коли бекенд каже failed (сервер явно відмовив). Повільна мережа,
      // перепідключення чи запізнілий ack — це «надсилається», а не помилка: інакше UI писав
      // «не надіслано», хоча повідомлення доходило. Питаємо бекенд про справжній стан.
      armAckPoll(localId, cid)
    } catch (e) {
      const code = errCode(e) || 'unknown'
      if (code === 'chat_spam') {
        // Не помилка доставки: повідомлення не пішло. Прибираємо бульбашку, повертаємо текст у поле.
        discardFailed({ localId } as Msg)
        if (!input.trim()) input = text
        sentLog = sentLog.slice(0, -1)
        startCooldown(SPAM_BACKEND_PAUSE_MS)
        toast(errText(e), 'info')
        return
      }
      patchLocal(localId, { sending: false, failed: true, failReason: code })
      // not_connected вже видно на бульбашці й у стрічці стану — тост зайвий.
      if (code !== 'not_connected') toast(errText(e), 'error')
    }
  }

  function sendText(text: string) {
    const body = text.trim()
    if (!body || !me || !peer?.accountId) return
    const localId = ++tmpSeq
    const rt = replyTo?.id && replyTo.id > 0 ? replyTo.id : 0
    messages = [...messages, {
      key: 'l' + localId, localId, id: 0, from: me, ts: Date.now(),
      bodyType: 'text', text: body, sending: true, fresh: true, replyToId: rt,
    }]
    replyTo = null // цитата використана
    scrollToBottom(true, true)
    dispatch(localId, body, '', rt)
  }

  async function send() {
    if (editing) { await submitEdit(); return }
    const text = input.trim()
    if (!text) return
    if (spamLeft > 0) return   // триває пауза: текст у полі лишається
    const w = spamWaitMs(text)
    if (w.ms > 0) { startCooldown(w.ms, w.dup); return }
    sentLog.push({ at: Date.now(), text: text.toLowerCase() })
    flySend()
    input = ''
    if (ta) ta.style.height = 'auto'
    if (hasNewer) await returnToLatest()   // писати можна лише в кінець стрічки
    sendText(text)
    ta?.focus()
  }

  function retry(m: Msg) {
    if (!m.localId || !m.text) return
    messages = messages.map(x => (x.localId === m.localId ? { ...x, ts: Date.now() } : x))
    dispatch(m.localId, m.text, m.clientMsgId || '')
  }

  // Невдале повідомлення на сервері не існує — прибираємо зовсім.
  function discardFailed(m: Msg) {
    if (m.localId) clearAckTimer(m.localId)
    messages = messages.filter(x => x !== m && x.key !== m.key && !(m.localId && x.localId === m.localId))
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape' && editing) { e.preventDefault(); cancelEdit(); return }
    if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
      e.preventDefault()
      send()
    }
  }

  function autoGrow() {
    if (!ta) return
    ta.style.height = 'auto'
    ta.style.height = Math.min(ta.scrollHeight, 120) + 'px'
  }

  function onInput() {
    autoGrow()
    const now = Date.now()
    if (now - lastTypingSent > 3000 && input.trim()) {
      lastTypingSent = now
      App.SendChatTyping(peer.accountId).catch(() => {})
    }
  }

  // ── Копіювання / приховування ────────────────────────────────────────────
  // Що саме копіювати: текст; для мода — посилання без службового фрагмента;
  // для запрошення — адресу сервера.
  function copyPayload(m: Msg): string {
    if (m.invite) return clean(m.invite.serverAddr)
    const mod = parseModMessage(m.text)
    if (mod) return mod.ref.url
    return m.text ?? ''
  }
  async function copyMsg(m: Msg) {
    const text = copyPayload(m)
    if (!text) return
    try { await App.CopyToClipboard(text) } catch {}
    toast(t('social.chat.copied'), 'success')
    copiedKey = m.key
    if (copiedTimer) clearTimeout(copiedTimer)
    copiedTimer = setTimeout(() => { copiedKey = '' }, 1400)
  }

  function removeLocal(id: number) {
    if (editing && editing.id === id) cancelEdit()
    messages = messages.filter(m => m.id !== id)
    pinned = pinned.filter(p => p.id !== id)
  }

  // Своє повідомлення — для всіх (сервер + друг). Підтвердження обов'язкове: це незворотно.
  async function deleteForAll(m: Msg) {
    if (m.id <= 0 || !peer?.accountId) return
    const ok = await confirmDialog(
      t('social.chat.deleteAllTitle'),
      t('social.chat.deleteAllText', { name: clean(peer.nickname) }),
      { confirmLabel: t('social.chat.deleteAllConfirm'), cancelLabel: t('social.chat.deleteAllCancel'), danger: true })
    if (!ok) return
    try {
      await App.DeleteChatMessage(peer.accountId, m.id)
      removeLocal(m.id)
    } catch (e) {
      toast(errText(e), 'error')
    }
  }

  async function togglePin(m: Msg) {
    if (m.id <= 0 || !peer?.accountId) return
    try {
      if (pinnedIds.has(m.id)) await App.UnpinChatMessage(peer.accountId, m.id)
      else await App.PinChatMessage(peer.accountId, m.id)
    } catch (e) {
      toast(errText(e), 'error')
    }
  }

  // ── Зміна повідомлення ────────────────────────────────────────────────────
  const canEdit = (m: Msg) =>
    isOwn(m) && m.id > 0 && m.bodyType === 'text' && !m.decryptFailed && !m.failed && !m.sending && !parseModMessage(m.text)

  function startEdit(m: Msg) {
    if (!canEdit(m)) return
    editing = m
    replyTo = null
    reactFor = null
    input = m.text ?? ''
    tick().then(() => { autoGrow(); ta?.focus(); ta?.setSelectionRange(input.length, input.length) })
  }
  function cancelEdit() {
    if (!editing) return
    editing = null
    input = ''
    if (ta) ta.style.height = 'auto'
  }
  async function submitEdit() {
    const m = editing
    const text = input.trim()
    if (!m || !text || !peer?.accountId || editBusy) return
    if (text === (m.text ?? '').trim()) { cancelEdit(); return } // нічого не змінилось
    editBusy = true
    try {
      await App.EditChatMessage(peer.accountId, m.id, text)
      // Сервер підтвердив; chat:edited теж оновить стрічку, але не чекаємо на нього.
      const at = Date.now()
      const patch = (x: Msg): Msg => (x.id === m.id ? { ...x, text, editedAt: Math.max(at, x.editedAt ?? 0) } : x)
      messages = messages.map(patch)
      pinned = pinned.map(patch)
      editing = null
      input = ''
      if (ta) ta.style.height = 'auto'
    } catch (e) {
      toast(errText(e), 'error')
    } finally {
      editBusy = false
    }
  }

  // ── Відповідь (як у Telegram) ─────────────────────────────────────────────
  function startReply(m: Msg) {
    if (m.id <= 0) return
    replyTo = m
    reactFor = null
    ta?.focus()
  }
  function cancelReply() { replyTo = null }

  // Прев'ю відповіді: текст цілі або позначка, якщо ціль зникла/не в цьому вікні.
  function replyPreview(m: Msg): { text: string; missing: boolean; who: string } {
    const target = messages.find(x => x.id === m.replyToId)
    if (!target) return { text: t('social.chat.replyGone'), missing: true, who: '' }
    const who = target.from === me ? t('social.chat.you') : clean(peer.nickname)
    if (target.decryptFailed) return { text: t('social.chat.decryptFailed'), missing: false, who }
    const mod = parseModMessage(target.text)
    const txt = mod ? (clean(mod.comment) || t('social.chat.pinnedMod')) : clean(target.text)
    return { text: txt || (target.bodyType === 'server_invite' ? t('social.chat.invite.title') : ''), missing: false, who }
  }
  function jumpReply(m: Msg) {
    if (!m.replyToId) return
    const target = messages.find(x => x.id === m.replyToId)
    if (target) { scrollToKey(target.key, 'center', true); flash(target.key) }
    else {
      // Цілі немає у вікні — довантажуємо вікно навколо її ймовірного часу (id кодує час).
      const ts = Math.floor(m.replyToId / 1024)
      if (ts > 0) jumpTo(m.replyToId, ts)
    }
  }

  // ── Реакції (один емодзі на акаунт, як у Telegram) ───────────────────────
  async function react(m: Msg, emoji: string) {
    reactFor = null
    if (m.id <= 0 || !peer?.accountId) return
    // Оптимістично: своє змінюємо локально, сервер пришле ПОВНИЙ список (chat:reactions).
    try { await App.ReactChatMessage(peer.accountId, m.id, emoji) }
    catch (e) { toast(errText(e), 'error') }
  }
  function myReaction(m: Msg): string {
    return m.reactions?.find(r => r.by === me)?.emoji ?? ''
  }
  // Групуємо реакції: емодзі → кількість (одна на акаунт, тож кількість = унікальні люди).
  function reactionGroups(m: Msg): { emoji: string; n: number; mine: boolean }[] {
    const map = new Map<string, number>()
    for (const r of m.reactions ?? []) map.set(r.emoji, (map.get(r.emoji) ?? 0) + 1)
    return [...map.entries()].map(([emoji, n]) => ({ emoji, n, mine: !!myReaction(m) && myReaction(m) === emoji }))
  }

  // Клік по панелі веде до того закріпленого, що «світиться» (найближче за розташуванням).
  // Якщо ми вже на ньому (воно посередині екрана) — перейти до попереднього за розташуванням,
  // по колу, щоб клік не був порожнім і до решти закріплених можна було дістатись без прокрутки.
  function openPinned() {
    const n = pinnedSorted.length
    if (n === 0 || pinActiveIdx < 0) return
    let target = pinnedSorted[pinActiveIdx]
    const el = scroller && elByKey(target.key)
    if (n > 1 && el && scroller) {
      const box = scroller.getBoundingClientRect()
      const r = el.getBoundingClientRect()
      const mid = box.top + box.height / 2
      if (r.top < mid + 24 && r.bottom > mid - 24 && r.bottom - r.top < box.height) {
        target = pinnedSorted[(pinActiveIdx - 1 + n) % n]
      }
    }
    pinNearId = target.id // світиться те, куди їдемо — без «стрибка» підсвітки після кліку
    jumpTo(target.id, target.ts)
  }
  function pinPreview(m: Msg): string {
    if (m.decryptFailed) return '🔒'
    const mod = parseModMessage(m.text)
    if (mod) return '📦 ' + (clean(mod.comment) || t('social.chat.pinnedMod'))
    return clean((m.text ?? '').split('\n')[0])
  }

  function hideMsg(m: Msg) {
    if (m.failed) { discardFailed(m); return }
    if (m.id <= 0) return
    const next = new Set(hidden)
    next.add(m.id)
    hidden = next
    saveHidden(me, chatId, next)
  }

  // ── Запрошення на сервер ─────────────────────────────────────────────────
  const inviteExpired = (inv: Invite) => !!(inv.expiresAt && inv.expiresAt > 0 && Date.now() > inv.expiresAt)

  // ── Тимчасові запрошення: живий стан карток (опитування видимих P2P-адрес кожні 10 с) ──
  const P2P_SUFFIX = '.play.shaurma-launcher.org'
  let showTempInvite = $state(false)

  // ── Мут діалогу ──
  // Кнопка «Дзвоник» у шапці: мут прибирає тости про повідомлення в ЦЬОМУ
  // діалозі (самі повідомлення досі приходять і накопичуються — бейдж
  // непрочитаного в списку лишається). Стан зберігається в settings.json
  // бекенда і ПЕРЕЖИВАЄ видалення з друзів — чат лишається мутнутим.
  let chatMuted = $state(false)
  let muteBusy = $state(false)
  onMount(() => {
    App.GetChatMuteState().then((s) => {
      chatMuted = (s?.muted ?? []).map((x) => x.toLowerCase()).includes(peer.accountId.toLowerCase())
    }).catch(() => {})
  })
  async function toggleMute() {
    if (muteBusy) return
    muteBusy = true
    try {
      const s = await App.SetChatMuted(peer.accountId, !chatMuted)
      chatMuted = (s?.muted ?? []).map((x) => x.toLowerCase()).includes(peer.accountId.toLowerCase())
      toast(chatMuted ? t('social.chat.mutedOn', { name: clean(peer.nickname) }) : t('social.chat.mutedOff', { name: clean(peer.nickname) }), 'success')
    } catch (e: any) {
      toast(String(e?.message ?? e), 'error')
    } finally {
      muteBusy = false
    }
  }
  let tiStates = $state<Record<string, TempInviteState>>({})
  async function refreshTempStates() {
    const addrs = [...new Set(messages
      .filter((m) => m.bodyType === 'server_invite' && m.invite && !m.invite.revoked)
      .map((m) => clean(m.invite!.serverAddr).toLowerCase())
      .filter((a) => a.endsWith(P2P_SUFFIX)))].slice(-20)
    if (!addrs.length) return
    try {
      const list = (await App.TempInviteStates(addrs)) ?? []
      const next: Record<string, TempInviteState> = {}
      for (const s of list) next[s.address] = s
      tiStates = next
    } catch { /* старий вузол або немає мережі — картки лишаються як були */ }
  }
  // Швидке запрошення цього друга у вже відкритий доступ (без нового IP).
  let openInvites = $state<TempInvite[]>([])
  let quickMenu = $state(false)
  let quickBusy = $state(false)
  async function loadOpenInvites() {
    try { openInvites = (await App.ListTempInvites()) ?? [] } catch { openInvites = [] }
    if (!openInvites.length) quickMenu = false
  }
  const peerInvitedIn = (ti: TempInvite) => !!peer && ti.guests.includes(peer.accountId)
  const quickTargets = $derived(openInvites.filter((ti) => !peerInvitedIn(ti)))
  async function quickInvite(ti: TempInvite) {
    if (quickBusy || !peer) return
    quickBusy = true
    quickMenu = false
    try {
      await App.InviteToTempInvite(ti.id, [peer.accountId])
      toast(`Друга запрошено у «${ti.meta.motd || tiKind(ti.meta.kind)}»`, 'success')
      setTimeout(refreshTempStates, 1500)
    } catch (e) {
      toast(errText(e), 'error')
    } finally {
      quickBusy = false
      loadOpenInvites()
    }
  }
  function openInviteMenu() {
    if (quickBusy) return
    quickMenu = !quickMenu
  }
  // Підтягує з сервера нові картки запрошень у ВІДКРИТИЙ чат, не перезавантажуючи його.
  // Потрібно відправнику: запрошення створює сервер, і поки він не поверне ехо,
  // картка інакше з'являлась би лише після перевідкриття чату.
  async function pullNewInvites() {
    if (!peer?.accountId || hasNewer) return
    try {
      const page = await App.GetChatHistory(peer.accountId, 0, 20)
      const fresh = (page?.messages ?? []).map(toMsg).filter(m =>
        m.bodyType === 'server_invite' && m.invite &&
        !messages.some(x => x.key === m.key || x.invite?.id === m.invite!.id))
      if (!fresh.length) return
      messages = mergeSorted(messages, fresh.map(m => ({ ...m, fresh: true })))
      refreshTempStates()
      scrollToBottom(false, true)
    } catch { /* офлайн — картка з'явиться при наступному відкритті чату */ }
  }
  onMount(() => {
    const t0 = setTimeout(refreshTempStates, 600)
    const iv = setInterval(refreshTempStates, 10000)
    const timers: ReturnType<typeof setTimeout>[] = []
    loadOpenInvites()
    const off = on('tinvite:closed', () => { refreshTempStates(); loadOpenInvites() })
    const offInv = on('tinvite:invited', (friendId: string) => {
      if (friendId && friendId !== peer?.accountId) return
      loadOpenInvites()
      // Ехо від VPS зазвичай приходить першим (тоді дубля не буде — дедуп за id);
      // ці два підтягування покривають старий VPS без ехо.
      timers.push(setTimeout(pullNewInvites, 700), setTimeout(pullNewInvites, 2200))
    })
    return () => { clearTimeout(t0); clearInterval(iv); timers.forEach(clearTimeout); off(); offInv() }
  })
  const tempOf = (inv: Invite) => tiStates[clean(inv.serverAddr).toLowerCase()]
  const tiKind = (k: string) => (k === 'lan' ? 'LAN-світ' : k === 'modded' ? 'Сервер з модами' : 'Сервер')

  // Збірка вже запущена на цьому акаунті — бекенд віддає "…already running…". Це не збій,
  // а підказка: замість сирої англійської помилки копіюємо адресу, щоб гравець просто
  // вставив її в «Пряме підключення» у вже відкритій грі.
  const isAlreadyRunning = (e: unknown) => /already running/i.test(String((e as any)?.message ?? e))
  async function copyForRunningGame(addr: string) {
    try { await App.CopyToClipboard(addr) } catch { /* нічого */ }
    toast(`Збірка вже запущена — IP ${addr} скопійовано. Вставте його в «Пряме підключення» в грі`, 'info')
  }

  async function copyAddr(inv: Invite) {
    const addr = clean(inv.serverAddr)
    try { await App.CopyToClipboard(addr) } catch { /* нічого */ }
    toast(t('social.chat.invite.copied', { addr }), 'success')
  }

  // ── «Грати» прямо з картки запрошення ────────────────────────────────────
  // Раніше картка пропонувала лише «Приєднатися» зі збіркою хоста — а якщо
  // запрошення без збірки (спільна P2P-кімната), гравець мав сам копіювати
  // адресу і лізти в «Пряме підключення» в грі. Тепер тут вибір збірки:
  // гра запускається з ПРЯМИМ підключенням до цієї адреси, або її можна
  // тимчасово доклеїти у список серверів обраної збірки.
  let playAddr = $state('')
  async function openPlayPicker(inv: Invite, ti?: TempInviteState) {
    const addr = clean(inv.serverAddr)
    if (!addr) return
    // Тимчасовий доступ без квитка не пустить: беремо квиток ПЕРШ ніж гра
    // спробує підключитись.
    if (ti) {
      try { await App.JoinTempInvite(addr) }
      catch (e) { toast(errText(e), 'error'); return }
    }
    playAddr = addr
  }

  // Гість тимчасового доступу: спершу квиток (без нього хост не пустить), потім звичайний вхід.
  async function joinTemp(inv: Invite) {
    if (joining) return
    try {
      await App.JoinTempInvite(clean(inv.serverAddr))
    } catch (e) {
      toast(errText(e), 'error')
      refreshTempStates()
      return
    }
    await joinInvite(inv)
  }

  async function joinInvite(inv: Invite) {
    if (inv.revoked || inviteExpired(inv) || joining) return
    const addr = clean(inv.serverAddr)
    if (!addr) return
    if (inv.packId) {
      joining = true
      try {
        await App.LaunchInstanceOnServer(inv.packId, addr)
        toast(t('social.chat.invite.launching'), 'info')
      } catch (e) {
        if (isAlreadyRunning(e)) await copyForRunningGame(addr)
        // Збірки може не бути встановлено — тоді хоч адресу дамо скопіювати.
        else toast(errText(e), 'error')
      } finally {
        joining = false
      }
    } else {
      try {
        await App.CopyToClipboard(addr)
        toast(t('social.chat.invite.copied', { addr }), 'success')
      } catch {
        toast(t('social.chat.invite.copied', { addr }), 'info')
      }
    }
  }

  // ── Час ──────────────────────────────────────────────────────────────────
  function timeOf(ts: number): string {
    const d = new Date(ts)
    return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
  }
  function fullTime(ts: number): string {
    return new Date(ts).toLocaleString()
  }
  function dayLabel(ts: number): string {
    const now = new Date()
    if (dayKey(ts) === dayKey(now.getTime())) return t('social.chat.today')
    const y = new Date(now); y.setDate(now.getDate() - 1)
    if (dayKey(ts) === dayKey(y.getTime())) return t('social.chat.yesterday')
    return new Date(ts).toLocaleDateString()
  }

  const stateLabel = (s: MsgState) => t(`social.chat.state.${s}`)
  const connBanner = $derived(conn !== 'online')
</script>

{#snippet avatar(cls: string)}
  <div class="sc-av {cls}">
    {#if head}<img src={head} alt="" />{:else}<span>{initial(peer?.nickname)}</span>{/if}
  </div>
{/snippet}

{#snippet tickIcon(s: MsgState)}
  {#if s === 'sending'}<span class="tk sending" aria-hidden="true"><i class="ring"></i></span>
  {:else if s === 'failed'}<span class="tk failed"><i class="ti ti-alert-circle"></i></span>
  {:else if s === 'read'}<span class="tk read"><i class="ti ti-checks"></i></span>
  {:else}<span class="tk sent"><i class="ti ti-check"></i></span>{/if}
{/snippet}


<div class="sc">
  <!-- Заголовок діалогу -->
  <div class="sc-head">
    {#if onBack}
      <button class="sc-back" onclick={onBack} use:tip={t('social.chat.back')} aria-label={t('social.chat.back')}>
        <i class="ti ti-arrow-left"></i>
      </button>
    {/if}
    <div class="sc-av lg" class:clk={!!onOpenProfile} onclick={() => onOpenProfile?.()} role="presentation">
      {#if head}<img src={head} alt="" />{:else}<span>{initial(peer?.nickname)}</span>{/if}
      <i class="sc-st {peerStatus}"></i>
    </div>
    <div class="sc-who" class:clk={!!onOpenProfile} onclick={() => onOpenProfile?.()} role="presentation">
      <div class="sc-name">{clean(peer?.nickname)}{#if peer?.handle}<span class="sc-handle">@{peer.handle}</span>{/if}</div>
      {#key peerTyping ? 'typ' : peerStatusText}
        <div class="sc-sub" class:typ={peerTyping} in:fly={{ y: 6, duration: 180, easing: cubicOut }}>
          {#if peerTyping}<span class="dots"><i></i><i></i><i></i></span> {t('social.chat.typing', { name: clean(peer?.nickname) })}
          {:else}{peerStatusText}{/if}
        </div>
      {/key}
    </div>
    <!-- ОДНА кнопка «Запросити»: сама лише іконка з підказкою + меню дій.
         Раніше в шапці жили ДВІ різні кнопки запрошення (швидка в уже
         відкритий світ і текстова «Запросити») — обидві легко було натиснути
         випадково, бо вони виглядали як плейсхолдер, а не як кнопка. -->
    {#if isFriend}
      <div class="sc-qi">
        <button class="sc-hbtn sc-hbtn-txt" data-testid="chat-temp-invite-btn" disabled={quickBusy}
                onclick={openInviteMenu} use:tip={t('social.chat.inviteTip')}
                aria-label={t('social.chat.inviteTip')} aria-haspopup="menu" aria-expanded={quickMenu}>
          <i class="ti {quickBusy ? 'ti-loader-2 spin' : 'ti-plus'}"></i>
          <span>{t('social.chat.inviteBtn')}</span>
        </button>
        {#if quickMenu}
          <div class="sc-qi-menu" data-testid="chat-quick-invite-menu" transition:fly={{ y: -6, duration: 150 }}>
            <button data-testid="chat-quick-invite-open" onclick={() => { quickMenu = false; showTempInvite = true }}>
              <i class="ti ti-plus"></i>
              <span>{t('social.chat.inviteTip')}</span>
            </button>
            <div class="sc-qi-sep"></div>
            {#if quickTargets.length}
              <div class="sc-qi-sec">{t('social.chat.inviteQuickTip')}</div>
              {#each quickTargets as ti (ti.id)}
                <button data-testid="chat-quick-invite-item-{ti.id}" onclick={() => quickInvite(ti)}>
                  <i class="ti {ti.meta.kind === 'lan' ? 'ti-world' : 'ti-server-2'}"></i>
                  <span>{ti.meta.motd || tiKind(ti.meta.kind)} <small>порт {ti.meta.port}</small></span>
                </button>
              {/each}
            {:else}
              <div class="sc-qi-sec">{t('social.chat.inviteAlready')}</div>
            {/if}
          </div>
        {/if}
      </div>
    {/if}
    <!-- Мут діалогу: тости про повідомлення не показуються (самі
         повідомлення лишаються в чаті). Стан видно кольором і іконкою
         вимкненого дзвінка; блокування гравця — у його ПРОФІЛІ (клік по
         аватарці/імені в шапці), а не в чаті. -->
    <button class="sc-hbtn" class:on={chatMuted} data-testid="chat-mute-btn" disabled={muteBusy}
            onclick={toggleMute} use:tip={chatMuted ? t('social.chat.unmuteTip') : t('social.chat.muteTip')}
            aria-label={chatMuted ? t('social.chat.unmuteTip') : t('social.chat.muteTip')} aria-pressed={chatMuted}>
      <i class="ti {chatMuted ? 'ti-bell-off' : 'ti-bell'}"></i>
    </button>
  </div>
  {#if showTempInvite}
    <TempInviteModal friends={allFriends.length ? allFriends : [peer]} preselect={peer.accountId} onclose={() => { showTempInvite = false; loadOpenInvites(); setTimeout(refreshTempStates, 1500) }} />
  {/if}
  {#if playAddr}
    <P2PJoinModal address={playAddr} onclose={() => (playAddr = '')} />
  {/if}

  {#if connBanner}
    <div class="sc-conn" class:wait={conn === 'connecting'} transition:fly={{ y: -10, duration: 220, easing: cubicOut }}>
      <i class="ti {conn === 'connecting' ? 'ti-loader-2 spin' : 'ti-plug-connected-x'}"></i>
      {conn === 'connecting' ? t('social.chat.connecting') : t('social.chat.noConn')}
    </div>
  {/if}

  <!-- Закріплені -->
  {#if pinned.length > 0}
    {@const cur = pinnedSorted[Math.max(0, pinActiveIdx)]}
    <div class="sc-pinbar" transition:fly={{ y: -8, duration: 180, easing: cubicOut }}>
      <button class="sc-pinmain" onclick={openPinned}>
        <span class="sc-pinseg" aria-hidden="true">
          {#each pinnedSorted as p, i (p.key)}<i class:act={i === pinActiveIdx}></i>{/each}
        </span>
        <span class="sc-pintxt">
          <b>{pinned.length > 1 ? t('social.chat.pinnedBarN', { n: Math.max(0, pinActiveIdx) + 1, total: pinned.length }) : t('social.chat.pinnedBar')}</b>
          {#key cur.key}<span class="sc-pinprev" in:fly={{ y: 6, duration: 160 }}>{pinPreview(cur)}</span>{/key}
        </span>
      </button>
      <button class="sc-pinx" onclick={() => togglePin(cur)} use:tip={t('social.chat.unpin')} aria-label={t('social.chat.unpin')}>
        <i class="ti ti-pin-off"></i>
      </button>
    </div>
  {/if}

  <!-- Стрічка повідомлень -->
  <div class="sc-msgs" class:selecting bind:this={scroller} onscroll={onScroll}
       onwheel={userTookOver} ontouchstart={userTookOver} onpointerdown={userTookOver} onkeydown={userTookOver}>
   <div class="sc-inner" bind:this={inner}>
    {#if loading}
      <div class="sc-skel" aria-busy="true" in:fade={{ duration: 160 }}>
        <span class="sr-only">{t('social.chat.loadingHistory')}</span>
        {#each [46, 64, 38, 72, 52, 34] as w, i}
          <div class="sk-row" class:own={i % 3 === 1} style="--sd:{i * 90}ms">
            {#if i % 3 !== 1}<i class="sk-av"></i>{/if}
            <div class="sk-bub" style="width:{w}%"></div>
          </div>
        {/each}
      </div>
    {:else}
      {#if hasMore}
        <button class="sc-more" onclick={() => loadHistory(messages[0]?.ts ?? 0, true)} disabled={loadingMore}>
          {#if loadingMore}<i class="ti ti-loader-2 spin"></i>{:else}<i class="ti ti-chevrons-up"></i>{/if}
          {t('social.chat.loadOlder')}
        </button>
      {/if}

      {#if rows.length === 0}
        <div class="sc-center sc-empty" in:fade={{ duration: 260 }}>
          <div class="sc-empty-ic"><i class="ti ti-messages"></i><i class="ti ti-lock sc-empty-lock"></i></div>
          <div>{t('social.chat.noMessages')}</div>
          <div class="sc-hint">{t('social.chat.noMessagesHint')}</div>
        </div>
      {/if}

      {#each rows as row, idx (row.key)}
       <div class="sc-it" class:intro data-key={row.key}
            style={intro ? `--d:${Math.max(0, 10 - (rows.length - idx)) * 32}ms` : undefined}
            in:rowIn={{ on: row.kind === 'msg' && !!row.m.fresh }}
            out:rowOut={{ dir: row.kind === 'msg' && row.m.from === me ? 1 : -1 }}>
        {#if row.kind === 'day'}
          <div class="sc-day"><span>{dayLabel(row.ts)}</span></div>
        {:else if row.kind === 'new'}
          <div class="sc-newdiv"><span>{t('social.chat.newMessages')}</span></div>
        {:else if row.kind === 'share'}
          {@const c = row.c}
          {@const own = isOwn(row.m)}
          {@const tone = c.e === 'failed' ? 'bad' : c.e === 'declined' || c.r === 'cancelled_receiver' ? 'warn' : 'mute'}
          {@const dir = shareOut[c.sid] ? 'out' : shareIn[c.sid] ? 'in' : null}
          <button class="sc-evt {tone}" disabled={!dir} onclick={() => dir && openTransfer(dir, c.sid)}
                  title={fullTime(row.m.ts)} oncontextmenu={(e) => openMenu(e, row.m)}>
            <span class="sc-evt-ic"><i class="ti {c.e === 'failed' ? 'ti-alert-triangle' : c.e === 'declined' ? 'ti-circle-x' : c.r === 'timeout' ? 'ti-clock-x' : c.r === 'sender_offline' ? 'ti-plug-connected-x' : 'ti-player-stop'}"></i></span>
            <span class="sc-evt-t">{ctlText(c, own, peer?.nickname ?? '')}</span>
            <span class="sc-evt-tm">{timeOf(row.m.ts)}</span>
          </button>
        {:else}
          {@const m = row.m}
          {@const own = isOwn(m)}
          {@const mod = m.bodyType === 'text' && !m.decryptFailed ? parseModMessage(m.text) : null}
          {@const shr = m.bodyType === 'text' && !m.decryptFailed && !mod ? parseShareMessage(m.text) : null}
          {@const st = own ? stateOf(m) : 'sent'}
          <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
          <div class="sc-row" class:selecting class:picked={selected.has(m.key)} onclickcapture={(e) => onRowClickCapture(e, m.key)} oncontextmenu={(e) => openMenu(e, m)} onmousedown={(e) => onDragDown(e, m.key)} class:own class:fresh={m.fresh} class:flash={flashKey === m.key} class:gstart={row.groupStart} class:gend={row.groupEnd}>
            {#if selecting}<span class="sc-chk" class:on={selected.has(m.key)} aria-hidden="true"><i class="ti ti-check"></i></span>{/if}
            {#if !own}
              <div class="sc-avcol">{#if row.groupEnd}{@render avatar('sm')}{/if}</div>
            {/if}
            <div class="sc-colmsg" class:invite-col={m.bodyType === 'server_invite'}>
              <div class="sc-bwrap" class:invite-bwrap={m.bodyType === 'server_invite'}>
                {#if m.bodyType === 'server_invite' && m.invite}
                  {@const inv = m.invite}
                  {@const ti = tempOf(inv)}
                  {@const dead = !!(inv.revoked || inviteExpired(inv) || (ti && !ti.active))}
                  <div class="sc-bub sc-invite" data-testid="chat-invite-card">
                    <InviteCard
                      title={ti ? (clean(ti.motd) || clean(inv.packName) || tiKind(ti.kind)) : (clean(inv.packName) || t('social.chat.invite.noPack'))}
                      kind={ti ? tiKind(ti.kind) : ''}
                      temp={!!ti}
                      {dead}
                      deadText={ti && !ti.active ? 'Світ закрито — доступ більше недоступний' : inv.revoked ? t('social.chat.invite.revoked') : t('social.chat.invite.expired')}
                      addr={clean(inv.serverAddr)}
                      version={ti?.version ?? ''}
                      players={ti?.players ?? 0}
                      max={ti?.max ?? 0}
                      host={!!(ti && ti.host)}
                      {own}
                      canJoin={!!inv.packId && !own}
                      {joining}
                      onJoin={() => (ti ? joinTemp(inv) : joinInvite(inv))}
                      onPlay={() => openPlayPicker(inv, ti)}
                      onCopy={() => copyAddr(inv)} />
                    <div class="sc-bfoot">
                      {#if m.reactions && m.reactions.length > 0}
                        <div class="sc-reacts">
                          {#each reactionGroups(m) as g (g.emoji)}
                            <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
                            <button class="sc-react" class:mine={g.mine} onclick={() => react(m, g.mine ? '' : g.emoji)} use:tip={g.mine ? t('social.chat.reactionRemove') : t('social.chat.reactionAdd')}>
                              <span class="sc-react-e">{g.emoji}</span>{#if g.n > 1}<b>{g.n}</b>{/if}
                            </button>
                          {/each}
                        </div>
                      {/if}
                      {#if m.id > 0 && !m.decryptFailed && !m.failed}
                        <button class="sc-react sc-react-add" onclick={() => (reactFor = m.key)} use:tip={t('social.chat.reactionAdd')} aria-label={t('social.chat.reactionAdd')}>
                          <i class="ti ti-mood-plus"></i>
                        </button>
                      {/if}
                      <div class="sc-meta"><time use:tip={fullTime(m.ts)}>{timeOf(m.ts)}</time></div>
                    </div>
                  </div>
                {:else}
                  <div class="sc-bub" class:sending={st === 'sending'} class:failed={st === 'failed'} class:modmsg={!!mod}>
                    {#if m.replyToId}
                      {@const rp = replyPreview(m)}
                      <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
                      <div class="sc-reply" class:missing={rp.missing} onclick={() => jumpReply(m)} role="button" tabindex="-1">
                        <span class="sc-reply-ic"><i class="ti ti-arrow-back-up"></i></span>
                        <span class="sc-reply-body">
                          {#if rp.who}<b>{rp.who}</b>{/if}
                          <span class="sc-reply-t">{rp.text || t('social.chat.replyGone')}</span>
                        </span>
                      </div>
                    {/if}
                    {#if m.decryptFailed}
                      <div class="sc-txt sc-bad"><i class="ti ti-lock-off"></i> {t('social.chat.decryptFailed')}</div>
                    {:else if shr}
                      <ShareCard card={shr.card} encoded={shr.encoded} {own} peerAccountId={peer?.accountId ?? ''} peerName={peer?.nickname ?? ''} />
                    {:else if mod}
                      {#if mod.comment}<div class="sc-txt sc-modc">{mod.comment}</div>{/if}
                      <ModCard ref={mod.ref} onOpenInLauncher={onOpenMod} />
                    {:else}
                      <div class="sc-txt">{m.text}</div>
                    {/if}
                    <div class="sc-bfoot">
                      {#if m.reactions && m.reactions.length > 0}
                        <div class="sc-reacts">
                          {#each reactionGroups(m) as g (g.emoji)}
                            <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
                            <button class="sc-react" class:mine={g.mine} onclick={() => react(m, g.mine ? '' : g.emoji)} use:tip={g.mine ? t('social.chat.reactionRemove') : t('social.chat.reactionAdd')}>
                              <span class="sc-react-e">{g.emoji}</span>{#if g.n > 1}<b>{g.n}</b>{/if}
                            </button>
                          {/each}
                        </div>
                      {/if}
                      {#if m.id > 0 && !m.decryptFailed && !m.failed}
                        <button class="sc-react sc-react-add" onclick={() => (reactFor = m.key)} use:tip={t('social.chat.reactionAdd')} aria-label={t('social.chat.reactionAdd')}>
                          <i class="ti ti-mood-plus"></i>
                        </button>
                      {/if}
                      <div class="sc-meta">
                        {#if pinnedIds.has(m.id)}<span class="sc-pinmark"><i class="ti ti-pin"></i> {t('social.chat.pinned')}</span>{/if}
                        {#if m.editedAt}<span class="sc-edited" use:tip={fullTime(m.editedAt)}>{t('social.chat.edited')}</span>{/if}
                        <time use:tip={fullTime(m.ts)}>{timeOf(m.ts)}</time>
                        {#if own}{#key st}{@render tickIcon(st)}{/key}{/if}
                      </div>
                    </div>
                  </div>
                {/if}

                <!-- Плаваюча панель при наведенні (як у Telegram): «Відповісти» + швидкі реакції -->
                {#if m.id > 0 && !m.decryptFailed && !selecting && !m.failed}
                  <div class="sc-hv">
                    <button class="sc-hv-reply" onclick={(e) => { e.stopPropagation(); startReply(m) }}>
                      <i class="ti ti-arrow-back-up"></i><span>{t('social.chat.reply')}</span>
                    </button>
                    <div class="sc-hv-react"><div class="sc-hv-col" role="group" aria-label={t('social.chat.react')}>
                      {#each HOVER_REACT as e}
                        {@const mine = myReaction(m) === e}
                        <button class="sc-hv-e" class:mine onclick={(ev) => { ev.stopPropagation(); react(m, mine ? '' : e) }}>{e}</button>
                      {/each}
                      <button class="sc-hv-e more" onclick={(ev) => { ev.stopPropagation(); reactFor = m.key }} aria-label={t('social.chat.react')}><i class="ti ti-mood-plus"></i></button>
                    </div></div>
                  </div>
                {/if}
              </div>

              <!-- Підпис стану під останнім своїм повідомленням -->
              {#if own && row.m.key === lastOwnKey && m.bodyType === 'text'}
                {#key st}
                  <div class="sc-state {st}" in:fly={{ y: -5, duration: 200, easing: cubicOut }}>
                    {#if st === 'sending'}{stateLabel('sending')}<span class="dots"><i></i><i></i><i></i></span>
                    {:else if st === 'failed'}
                      <i class="ti ti-alert-circle"></i>
                      {m.failReason === 'not_connected' ? t('social.chat.state.failedConn') : m.failReason === 'timeout' ? t('social.chat.state.failedTimeout') : t('social.chat.state.failed')}
                      {#if m.text}<button class="sc-link" onclick={() => retry(m)}>{t('social.chat.retry')}</button>{/if}
                      <button class="sc-link muted" onclick={() => discardFailed(m)}>{t('social.chat.discard')}</button>
                    {:else}{stateLabel(st)}{/if}
                  </div>
                {/key}
              {:else if own && m.failed && m.bodyType === 'text'}
                <div class="sc-state failed">
                  <i class="ti ti-alert-circle"></i> {t('social.chat.state.failed')}
                  {#if m.text}<button class="sc-link" onclick={() => retry(m)}>{t('social.chat.retry')}</button>{/if}
                  <button class="sc-link muted" onclick={() => discardFailed(m)}>{t('social.chat.discard')}</button>
                </div>
              {/if}
            </div>
          </div>
        {/if}
       </div>
      {/each}

      {#if peerTyping}
        <div class="sc-row typ-row" in:fly={{ y: 8, duration: 200, easing: cubicOut }} out:fade={{ duration: 120 }}>
          <div class="sc-avcol">{@render avatar('sm')}</div>
          <div class="sc-bub sc-typ"><span class="dots big"><i></i><i></i><i></i></span></div>
        </div>
      {/if}
      {#if loadingNewer}
        <div class="sc-more-new"><i class="ti ti-loader-2 spin"></i> {t('social.chat.loadingNewer')}</div>
      {/if}
    {/if}
   </div>
  </div>

  {#if (!atBottom || hasNewer) && !loading}
    <button class="sc-down" onclick={() => { if (hasNewer) returnToLatest(); else { scrollToBottom(true, true); unseen = 0 } }}
            transition:fly={{ y: 10, duration: 180, easing: cubicOut }} use:tip={t('social.chat.toBottom')} aria-label={t('social.chat.toBottom')}>
      <i class="ti ti-arrow-down"></i>
      {#if unseen > 0}<span class="sc-down-n">{unseen > 9 ? '9+' : unseen}</span>{/if}
    </button>
  {/if}

  {#if menu && menuView}
    {@const m = menuView.m}
    {@const own = isOwn(m)}
    <div class="sc-menu" use:portal bind:this={menuEl} role="menu" style="left:{menuView.x}px;top:{menuView.y}px" transition:scale={{ duration: 140, start: 0.9, easing: cubicOut }}>
      {#if m.failed && m.text}
        <button role="menuitem" onclick={() => act(() => retry(m))}><i class="ti ti-refresh"></i> {t('social.chat.retry')}</button>
      {/if}
      {#if !m.failed}
        <button role="menuitem" onclick={() => act(() => copyMsg(m))}>
          <i class="ti ti-copy"></i> {m.invite ? t('social.chat.invite.copy') : parseModMessage(m.text) ? t('social.chat.copyLink') : t('social.chat.copy')}
        </button>
      {/if}
      {#if m.id > 0 && m.bodyType === 'text' && !m.decryptFailed}
        <button role="menuitem" onclick={() => act(() => togglePin(m))}>
          <i class="ti {pinnedIds.has(m.id) ? 'ti-pin-off' : 'ti-pin'}"></i> {pinnedIds.has(m.id) ? t('social.chat.unpin') : t('social.chat.pin')}
        </button>
      {/if}
      {#if canEdit(m)}
        <button role="menuitem" data-testid="chat-edit-btn" onclick={() => act(() => startEdit(m))}>
          <i class="ti ti-pencil"></i> {t('social.chat.edit')}
        </button>
      {/if}
      {#if m.id > 0 && !m.decryptFailed}
        <button role="menuitem" onclick={() => act(() => startReply(m))}>
          <i class="ti ti-arrow-back-up"></i> {t('social.chat.reply')}
        </button>
        <button role="menuitem" onclick={() => act(() => { reactFor = reactFor === m.key ? null : m.key })}>
          <i class="ti ti-mood-smile"></i> {t('social.chat.react')}
        </button>
      {/if}
      <button role="menuitem" onclick={() => act(() => startSelect(m))}><i class="ti ti-checkbox"></i> {t('social.chat.select')}</button>
      {#if m.failed || m.id > 0}
        <hr />
        {#if m.failed}
          <button role="menuitem" class="danger" onclick={() => act(() => hideMsg(m))}><i class="ti ti-trash"></i> {t('social.chat.discard')}</button>
        {:else if own && m.bodyType === 'text'}
          <button role="menuitem" class="danger" onclick={() => act(() => deleteForAll(m))}><i class="ti ti-trash"></i> {t('social.chat.deleteAll')}</button>
        {:else}
          <button role="menuitem" class="danger" onclick={() => act(() => hideMsg(m))}><i class="ti ti-trash"></i> {t('social.chat.deleteLocal')}</button>
        {/if}
      {/if}
    </div>
  {/if}

  {#if selecting}
    <div class="sc-selbar" in:fly={{ y: 8, duration: 160, easing: cubicOut }}>
      <button class="sc-selx" onclick={exitSelect} use:tip={t('social.chat.selCancel')} aria-label={t('social.chat.selCancel')}><i class="ti ti-x"></i></button>
      <span class="sc-seln">{t('social.chat.selected', { n: selected.size })}</span>
      <button class="sc-selbtn" onclick={copySelected} disabled={bulkBusy}><i class="ti ti-copy"></i> {t('social.chat.copy')}</button>
      <button class="sc-selbtn danger" onclick={deleteSelected} disabled={bulkBusy}>
        {#if bulkBusy}<i class="ti ti-loader-2 spin"></i>{:else}<i class="ti ti-trash"></i>{/if} {t('social.chat.deleteSel')}
      </button>
    </div>
  {:else}
  {#if spamLeft > 0}
    <div class="sc-spam" role="status" transition:fly={{ y: 6, duration: 160, easing: cubicOut }}>
      <i class="ti ti-hourglass"></i>
      {spamDup ? t('social.chat.spamDup', { n: spamLeft }) : t('social.chat.spamWait', { n: spamLeft })}
    </div>
  {/if}

  <!-- Швидкі реакції на повідомлення -->
  {#if reactFor}
    {@const rm = messages.find(x => x.key === reactFor)}
    <div class="sc-reactpick" transition:fly={{ y: 8, duration: 160, easing: cubicOut }}>
      {#each ALL_REACT as emoji}
        {@const isMine = !!rm && myReaction(rm) === emoji}
        <button class="sc-reactbig" class:mine={isMine}
                onclick={() => { if (rm) react(rm, isMine ? '' : emoji) }}>{emoji}</button>
      {/each}
      <button class="sc-reactbig rm" onclick={() => { if (rm && myReaction(rm)) react(rm, ''); else reactFor = null }}
              aria-label={rm && myReaction(rm) ? t('social.chat.reactionRemove') : t('friends.close')}><i class="ti ti-x"></i></button>
    </div>
  {/if}

  <!-- Режим зміни: показуємо, що саме редагуємо -->
  {#if editing}
    <div class="sc-replybar sc-editbar" data-testid="chat-edit-bar" transition:fly={{ y: 8, duration: 160, easing: cubicOut }}>
      <i class="ti ti-pencil"></i>
      <div class="sc-replybar-txt">
        <b>{t('social.chat.editing')}</b>
        <span>{clean(editing.text)}</span>
      </div>
      <button class="sc-replyx" onclick={cancelEdit} aria-label={t('social.chat.editCancel')}><i class="ti ti-x"></i></button>
    </div>
  {/if}

  <!-- Цитата відповіді над полем вводу -->
  {#if replyTo}
    <div class="sc-replybar" transition:fly={{ y: 8, duration: 160, easing: cubicOut }}>
      <i class="ti ti-arrow-back-up"></i>
      <div class="sc-replybar-txt">
        <b>{replyTo.from === me ? t('social.chat.replyMe') : clean(peer.nickname)}</b>
        <span>{clean(replyTo.text) || (replyTo.bodyType === 'server_invite' ? t('social.chat.invite.title') : '🔒')}</span>
      </div>
      <button class="sc-replyx" onclick={cancelReply} aria-label={t('social.chat.replyCancel')}><i class="ti ti-x"></i></button>
    </div>
  {/if}

  <!-- Поле вводу -->
  <div class="sc-foot" class:off={peerStatus === 'offline'}>
    <textarea
      bind:this={ta}
      class="sc-input"
      bind:value={input}
      rows="1"
      maxlength="4000"
      placeholder={t('social.chat.placeholder')}
      aria-label={t('social.chat.placeholder')}
      oninput={onInput}
      onkeydown={onKeydown}
    ></textarea>
    <button class="sc-send" class:fly={sendFly} class:ready={!!input.trim() && spamLeft === 0} onclick={send} disabled={!input.trim() || spamLeft > 0}
            use:tip={t('social.chat.send')} aria-label={t('social.chat.send')}>
      <i class="ti ti-send"></i>
    </button>
  </div>
  {#if peerStatus === 'offline'}
    <div class="sc-offline" transition:fade={{ duration: 160 }}><i class="ti ti-moon"></i> {t('social.chat.offline')}</div>
  {/if}
  {/if}
</div>

<style>
  .sc { position: relative; display: flex; flex-direction: column; height: 100%; min-height: 0; min-width: 0; }

  /* ── Шапка ── */
  .sc-head { display: flex; align-items: center; gap: 10px; flex-shrink: 0; padding: 11px 16px; border-bottom: 1px solid var(--border);
    background: linear-gradient(135deg, color-mix(in srgb, var(--bg-panel) 94%, var(--orange) 6%), var(--bg-panel)); }
  .sc-back { width: 30px; height: 30px; display: flex; align-items: center; justify-content: center; background: var(--hover); border: 0; border-radius: 9px; color: var(--text-mute); cursor: pointer; transition: background .14s, color .14s, transform .12s; }
  .sc-back:hover { background: var(--hover-strong); color: var(--text); } .sc-back:active { transform: scale(.92); }
  .sc-av { position: relative; flex: 0 0 auto; width: 30px; height: 30px; border-radius: 9px; background: var(--bg-input); display: flex; align-items: center; justify-content: center; font: 800 13px/1 var(--font-ui); color: var(--text-mute); }
  .sc-av img { width: 100%; height: 100%; border-radius: 9px; image-rendering: pixelated; }
  .sc-av.lg { width: 38px; height: 38px; border-radius: 11px; } .sc-av.lg img { border-radius: 11px; }
  .sc-av.sm { width: 26px; height: 26px; border-radius: 8px; } .sc-av.sm img { border-radius: 8px; }
  .sc-st { position: absolute; right: -3px; bottom: -3px; width: 11px; height: 11px; border-radius: 50%; border: 2px solid var(--bg-panel); background: var(--text-dim); transition: background .25s; }
  .sc-st.online { background: #22c55e; } .sc-st.in_game { background: var(--orange); animation: sc-pulse 2s ease-in-out infinite; } .sc-st.away { background: #eab308; }
  @keyframes sc-pulse { 0%,100% { box-shadow: 0 0 0 0 rgba(var(--orange-rgb), .55); } 50% { box-shadow: 0 0 0 5px rgba(var(--orange-rgb), 0); } }
  .sc-who { flex: 1; min-width: 0; }
  .sc-name { font: 750 13.5px/1.25 var(--font-ui); color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .sc-sub { font: 500 11px/1.3 var(--font-ui); color: var(--text-dim); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; display: flex; align-items: center; gap: 6px; }
  .sc-sub.typ { color: var(--orange); }
  /* Кнопки-іконки в шапці діалогу (запрошення, мут) — такі самі «кнопки», як
     .sc-back: рамка, фон, hover і фокус-кільце. Раніше це були голі іконки
     без жодного фону — у темній шапці виглядали як залишки тексту й не
     читались як клікабельні. */
  .sc-hbtn {
    width: 30px; height: 30px; flex: 0 0 auto; display: flex; align-items: center; justify-content: center;
    border-radius: 9px; padding: 0; font-size: 15px;
    background: var(--hover); border: 1px solid var(--border); color: var(--text-mute);
    cursor: pointer; transition: background .14s, border-color .14s, color .14s, transform .12s;
  }
  .sc-hbtn:hover:not(:disabled) { background: var(--hover-strong); border-color: var(--border-hi); color: var(--text); }
  .sc-hbtn-txt { width: auto; padding: 0 12px; gap: 6px; font: 700 12.5px/1 var(--font-ui); white-space: nowrap; }
  .sc-hbtn-txt i { font-size: 15px; }
  .sc-hbtn:active:not(:disabled) { transform: scale(.92); }
  .sc-hbtn:focus-visible { outline: 2px solid var(--orange); outline-offset: 2px; }
  .sc-hbtn:disabled { opacity: .5; cursor: default; }
  /* Мут діалогу (кнопка дзвінка): увімкнено → червоний, іконка виключеного дзвінка. */
  .sc-hbtn.on { color: var(--red); border-color: color-mix(in srgb, var(--red) 40%, transparent); background: color-mix(in srgb, var(--red) 10%, transparent); }
  .sc-hbtn.on:hover:not(:disabled) { color: color-mix(in srgb, var(--red) 80%, #fff); background: color-mix(in srgb, var(--red) 16%, transparent); }
  .sc-qi { position: relative; flex: 0 0 auto; }
  .sc-qi-menu { position: absolute; right: 0; top: calc(100% + 6px); z-index: 50; min-width: 220px; display: grid; gap: 2px; padding: 6px; border-radius: 12px; background: var(--bg-elev, #17171c); border: 1px solid var(--border, #2a2a33); box-shadow: 0 10px 30px rgba(0,0,0,.35); }
  .sc-qi-menu button { display: flex; align-items: center; gap: 8px; padding: 8px 10px; border: 0; border-radius: 8px; background: transparent; color: inherit; cursor: pointer; text-align: left; transition: background-color .15s; }
  .sc-qi-menu button:hover { background: color-mix(in srgb, var(--accent, #f5a524) 14%, transparent); }
  .sc-qi-menu button small { color: var(--text-mute); }
  .sc-qi-sep { height: 1px; margin: 4px 6px; background: var(--border); }
  .sc-qi-sec { padding: 4px 10px 2px; font: 700 10px/1.3 var(--font-ui); text-transform: uppercase; letter-spacing: .05em; color: var(--text-dim); }
  .sc-qi-menu small { opacity: .6; font-size: 11px; margin-left: 4px; }

  .sc-conn { display: flex; align-items: center; gap: 8px; flex-shrink: 0; padding: 7px 16px; font: 600 11px/1.3 var(--font-ui); color: var(--text); background: rgba(239,68,68,.1); border-bottom: 1px solid rgba(239,68,68,.25); }
  .sc-conn i { color: var(--red); font-size: 14px; }
  .sc-conn.wait { background: rgba(234,179,8,.09); border-bottom-color: rgba(234,179,8,.3); } .sc-conn.wait i { color: #eab308; }

  /* ── Стрічка ── */
  .sc-msgs { container: scm / inline-size; flex: 1; min-height: 0; overflow-y: auto; padding: 26px 16px 8px; overflow-anchor: none; }
  .sc-inner { display: flex; flex-direction: column; min-height: 100%; }
  .sc-more-new { align-self: center; display: flex; align-items: center; gap: 6px; padding: 8px; color: var(--text-dim); font: 600 11px/1 var(--font-ui); }
  .sc-newdiv { display: flex; align-items: center; gap: 10px; margin: 12px 0 4px; color: var(--orange); font: 750 10.5px/1 var(--font-ui); }
  .sc-newdiv::before, .sc-newdiv::after { content: ''; flex: 1; height: 1px; background: rgba(var(--orange-rgb), .35); }

  /* ── Закріплені ── */
  .sc-pinbar { display: flex; align-items: stretch; flex-shrink: 0; border-bottom: 1px solid var(--border); background: color-mix(in srgb, var(--bg-panel) 92%, var(--orange) 8%); }
  .sc-pinmain { flex: 1; min-width: 0; display: flex; align-items: center; gap: 10px; padding: 7px 8px 7px 16px; border: 0; background: transparent; color: inherit; text-align: left; cursor: pointer; transition: background .14s; }
  .sc-pinmain:hover { background: var(--hover); }
  .sc-pinseg { display: flex; flex-direction: column; gap: 2px; flex: 0 0 3px; align-self: stretch; }
  .sc-pinseg i { flex: 1; min-height: 4px; border-radius: 2px; background: rgba(var(--orange-rgb), .28); transition: background .2s; }
  .sc-pinseg i.act { background: var(--orange); }
  .sc-pintxt { min-width: 0; display: flex; flex-direction: column; gap: 2px; }
  .sc-pintxt b { font: 750 11px/1.2 var(--font-ui); color: var(--orange); }
  .sc-pinprev { font: 500 12px/1.3 var(--font-ui); color: var(--text-mute); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .sc-pinx { flex: 0 0 auto; width: 40px; border: 0; background: transparent; color: var(--text-dim); font-size: 16px; cursor: pointer; transition: color .14s, background .14s; }
  .sc-pinx:hover { color: var(--text); background: var(--hover); }
  .sc-edited { font-style: italic; opacity: .85; }
  .sc-pinmark { display: inline-flex; align-items: center; gap: 3px; color: var(--orange); font-weight: 650; }
  .sc-pinmark i { font-size: 11px; }

  /* ── Відповідь (цитата в бульбашці) ── */
  .sc-reply { position: relative; display: flex; align-items: stretch; gap: 9px; margin: 0 0 7px; padding: 6px 10px 6px 0; border-radius: 10px; overflow: hidden;
    background: color-mix(in srgb, var(--text) 7%, transparent); cursor: pointer; max-width: 100%; transition: background-color .14s; }
  .sc-reply::before { content: ''; flex: 0 0 3px; border-radius: 3px; background: var(--orange); }
  .sc-reply:hover { background: color-mix(in srgb, var(--text) 12%, transparent); }
  .sc-reply-ic { flex: 0 0 auto; align-self: center; width: 22px; height: 22px; display: grid; place-items: center; margin-left: -2px; border-radius: 7px; font-size: 14px; color: var(--orange); background: rgba(var(--orange-rgb), .16); }
  .sc-reply-body { min-width: 0; display: flex; flex-direction: column; gap: 2px; }
  .sc-reply-body b { font: 750 12px/1.25 var(--font-ui); color: color-mix(in srgb, var(--orange) 55%, var(--text)); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .sc-reply-t { font: 500 12.5px/1.35 var(--font-ui); color: var(--text); opacity: .88; overflow: hidden; text-overflow: ellipsis; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; word-break: break-word; }
  .sc-reply.missing { opacity: .75; }
  .sc-reply.missing::before { background: var(--text-dim); }
  .sc-reply.missing .sc-reply-ic { color: var(--text-dim); background: var(--hover); }
  .sc-reply.missing .sc-reply-t { font-style: italic; }

  /* ── Реакції: чіпи в нижньому рядку бульбашки, час — справа ── */
  .sc-bfoot { display: flex; flex-wrap: wrap; align-items: flex-end; gap: 4px 10px; margin-top: 4px; }
  .sc-bfoot .sc-meta { margin: 0 0 0 auto; padding: 0; }
  .sc-reacts { display: flex; flex-wrap: wrap; gap: 4px; min-width: 0; margin: 0; }
  .sc-react { display: inline-flex; align-items: center; gap: 5px; height: 24px; padding: 0 9px 0 7px; border: 0; border-radius: 12px; cursor: pointer;
    background: color-mix(in srgb, var(--text) 10%, transparent); color: var(--text); font: 700 11.5px/1 var(--font-ui);
    transition: background-color .14s, transform .14s var(--ff-spring, ease); animation: sc-rpop .26s cubic-bezier(.34,1.5,.64,1) both; }
  .sc-react-e { font-size: 14px; line-height: 1; }
  .sc-react:hover { background: color-mix(in srgb, var(--text) 18%, transparent); transform: translateY(-1px); }
  .sc-react:active { transform: scale(.92); }
  .sc-react.mine { background: color-mix(in srgb, var(--orange) 32%, transparent); box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--orange) 70%, transparent); }
  .sc-react b { font-weight: 800; }
  /* «Додати реакцію» — постійно видима кнопка в нижньому рядку бульбашки,
     щоб реакції працювали не лише з hover-панелі (вона на вузькому чаті й
     на широких картках P2P виходила за межі стрічки й була недосяжна). */
  .sc-react-add { width: 24px; padding: 0; justify-content: center; color: var(--text-dim); background: transparent; }
  .sc-react-add:hover { color: var(--orange); background: color-mix(in srgb, var(--orange) 14%, transparent); }
  .sc-react-add i { font-size: 14px; }
  @keyframes sc-rpop { from { opacity: 0; transform: scale(.5); } to { opacity: 1; transform: none; } }

  /* ── Панель при наведенні на повідомлення ── */
  .sc-hv { position: absolute; inset: 0; pointer-events: none; z-index: 6; }
  .sc-hv-reply { position: absolute; left: 50%; top: -15px; transform: translate(-50%, 4px); display: inline-flex; align-items: center; gap: 6px; height: 28px; padding: 0 12px 0 10px; border: 0; border-radius: 14px; cursor: pointer; white-space: nowrap;
    font: 700 12px/1 var(--font-ui); color: var(--text); background: var(--bg-panel); box-shadow: 0 0 0 1px var(--border-hi), 0 8px 20px rgba(0,0,0,.28);
    opacity: 0; pointer-events: none; transition: opacity .14s, transform .16s var(--ff-ease, ease), color .14s; }
  .sc-hv-reply i { font-size: 15px; color: var(--orange); }
  .sc-hv-reply:hover { color: var(--orange); }
  .sc-hv-react { position: absolute; top: 50%; left: 100%; padding-left: 8px; transform: translate(-4px, -50%); opacity: 0; pointer-events: none; transition: opacity .14s, transform .16s var(--ff-ease, ease); }
  .sc-row.own .sc-hv-react { left: auto; right: 100%; padding-left: 0; padding-right: 8px; transform: translate(4px, -50%); }
  .sc-hv-col { display: flex; align-items: center; gap: 1px; padding: 3px; border-radius: 18px; background: var(--bg-panel); box-shadow: 0 0 0 1px var(--border-hi), 0 8px 20px rgba(0,0,0,.28); }
  .sc-hv-e { width: 30px; height: 30px; display: grid; place-items: center; border: 0; border-radius: 50%; background: transparent; cursor: pointer; font-size: 17px; line-height: 1; transition: transform .14s var(--ff-spring, ease), background-color .14s; }
  .sc-hv-e:hover { background: var(--hover-strong); transform: scale(1.22); }
  .sc-hv-e.mine { background: rgba(var(--orange-rgb), .22); }
  .sc-hv-e.more { color: var(--text-mute); font-size: 17px; }
  .sc-hv-e.more:hover { color: var(--orange); transform: none; }
  .sc-bwrap:hover .sc-hv-reply, .sc-bwrap:focus-within .sc-hv-reply { opacity: 1; transform: translate(-50%, 0); pointer-events: auto; }
  .sc-bwrap:hover .sc-hv-react, .sc-bwrap:focus-within .sc-hv-react { opacity: 1; transform: translate(0, -50%); pointer-events: auto; }
  .sc-row:hover, .sc-row:focus-within { z-index: 8; }
  /* Вузький чат: реакції під бульбашкою, а не збоку. */
  @container scm (max-width: 560px) {
    .sc-hv-react, .sc-row.own .sc-hv-react { left: auto; right: 8px; top: 100%; padding: 0; transform: translate(0, -4px); }
    .sc-bwrap:hover .sc-hv-react, .sc-bwrap:focus-within .sc-hv-react { transform: translate(0, -6px); }
  }
  @media (hover: none) { .sc-hv { display: none; } }

  /* ── Пікер швидких реакцій ── */
  .sc-reactpick { display: flex; flex-wrap: wrap; align-items: center; justify-content: center; gap: 4px; flex-shrink: 0; margin: 0 auto 8px; padding: 6px 8px; border-radius: 22px;
    width: min(364px, calc(100% - 28px)); max-height: 40vh; overflow-y: auto;
    background: var(--bg-panel); box-shadow: 0 0 0 1px var(--border-hi), 0 12px 28px rgba(0,0,0,.3); }
  .sc-reactbig { width: 40px; height: 40px; flex: 0 0 auto; display: flex; align-items: center; justify-content: center;
    border: 0; border-radius: 50%; background: transparent; font-size: 22px; cursor: pointer;
    transition: background .14s, transform .14s cubic-bezier(.34,1.5,.64,1); }
  .sc-reactbig:hover { background: var(--hover-strong); transform: scale(1.2); }
  .sc-reactbig.mine { background: rgba(var(--orange-rgb), .22); }
  .sc-reactbig.rm { color: var(--text-dim); font-size: 16px; }
  .sc-reactbig.rm i { font-size: 16px; }

  /* ── Цитата над полем вводу ── */
  .sc-replybar { display: flex; align-items: center; gap: 9px; flex-shrink: 0; margin: 0 14px; padding: 7px 8px 7px 11px;
    border-left: 3px solid var(--orange); border-radius: 6px; background: rgba(var(--orange-rgb), .09); }
  .sc-replybar > i { color: var(--orange); font-size: 15px; flex: 0 0 auto; }
  .sc-replybar-txt { min-width: 0; flex: 1; display: flex; flex-direction: column; gap: 1px; }
  .sc-replybar-txt b { font: 700 11px/1.3 var(--font-ui); color: var(--orange); }
  .sc-replybar-txt span { font: 500 11.5px/1.35 var(--font-ui); color: var(--text-dim); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .sc-replyx { width: 26px; height: 26px; flex: 0 0 auto; display: flex; align-items: center; justify-content: center;
    border: 0; border-radius: 8px; background: transparent; color: var(--text-dim); cursor: pointer; transition: background .14s, color .14s; }
  .sc-replyx:hover { background: var(--hover-strong); color: var(--text); }
  .sc-row.flash .sc-bub { animation: sc-flash 1.6s ease-out; }
  @keyframes sc-flash { 0%, 35% { box-shadow: 0 0 0 3px rgba(var(--orange-rgb), .55); } 100% { box-shadow: 0 0 0 0 rgba(var(--orange-rgb), 0); } }
  .sc-center { display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 8px; flex: 1; padding: 30px 14px; text-align: center; color: var(--text-dim); font: 500 12.5px/1.4 var(--font-ui); }
  .sc-center i { font-size: 26px; opacity: .7; }
  .sc-empty-ic { position: relative; width: 64px; height: 64px; border-radius: 20px; display: flex; align-items: center; justify-content: center; background: rgba(var(--orange-rgb), .08); border: 1px solid rgba(var(--orange-rgb), .22); animation: sc-float 3.4s ease-in-out infinite; }
  .sc-empty-ic > .ti-messages { font-size: 30px; color: var(--orange); opacity: 1; }
  .sc-empty-lock { position: absolute; right: -6px; bottom: -6px; font-size: 14px !important; width: 24px; height: 24px; border-radius: 50%; display: flex; align-items: center; justify-content: center; background: var(--bg-panel); border: 1px solid var(--border-hi); color: #22c55e; opacity: 1 !important; }
  @keyframes sc-float { 50% { transform: translateY(-5px); } }
  .sc-hint { font-size: 11px; color: var(--text-dim); max-width: 250px; line-height: 1.5; }

  .sc-more { align-self: center; display: inline-flex; align-items: center; gap: 6px; margin: 2px 0 10px; padding: 6px 13px; border-radius: 20px; border: 1px solid var(--border); background: var(--bg-input); color: var(--text-mute); font: 650 11px/1 var(--font-ui); cursor: pointer; transition: color .14s, border-color .14s, transform .12s; }
  .sc-more:hover { color: var(--text); border-color: var(--border-hi); } .sc-more:active { transform: scale(.96); }

  .sc-it { display: flex; flex-direction: column; min-width: 0; }
  .sc-day { align-self: center; margin: 10px 0 6px; padding: 3px 13px; border-radius: 20px; background: color-mix(in srgb, var(--bg-input) 75%, transparent); border: 1px solid var(--border); font: 650 10.5px/1.6 var(--font-ui); color: var(--text-dim); }

  .sc-row { display: flex; align-items: flex-end; gap: 8px; max-width: 100%; margin-top: 8px; }
  .sc-row:not(.gstart) { margin-top: 2px; }
  .sc-row.own { flex-direction: row-reverse; }
  .sc-row.fresh .sc-bub { animation: sc-in-l .38s cubic-bezier(.2,1.2,.3,1) both; transform-origin: bottom left; }
  .sc-row.own.fresh .sc-bub { animation-name: sc-in-r; transform-origin: bottom right; }
  @keyframes sc-in-l { from { opacity: 0; transform: translate(-12px, 10px) scale(.88); } to { opacity: 1; transform: none; } }
  @keyframes sc-in-r { from { opacity: 0; transform: translate(12px, 10px) scale(.88); } to { opacity: 1; transform: none; } }
  .sc-avcol { flex: 0 0 26px; width: 26px; }
  .sc-colmsg { display: flex; flex-direction: column; min-width: 0; max-width: min(74%, 520px); }
  .sc-row.own .sc-colmsg { align-items: flex-end; }
  .sc-bwrap { position: relative; display: flex; min-width: 0; max-width: 100%; }

  .sc-bub { position: relative; min-width: 0; max-width: 100%; padding: 8px 11px 6px; border-radius: 15px; background: var(--bg-input); border: 1px solid var(--border);
    border-bottom-left-radius: 15px; transition: opacity .25s, background .25s, border-color .25s, filter .25s; }
  .sc-row:not(.own).gend .sc-bub { border-bottom-left-radius: 5px; }
  .sc-row.own .sc-bub { background: linear-gradient(135deg, rgba(var(--orange-rgb), .24), rgba(var(--orange-rgb), .12)); border-color: rgba(var(--orange-rgb), .42); }
  .sc-row.own.gend .sc-bub { border-bottom-right-radius: 5px; }
  .sc-bub.sending { opacity: .62; filter: saturate(.8); }
  .sc-bub.failed { border-color: color-mix(in srgb, var(--red) 60%, transparent); background: rgba(239,68,68,.1); animation: sc-shake .36s ease-in-out; }
  @keyframes sc-shake { 20%,60% { transform: translateX(-4px); } 40%,80% { transform: translateX(4px); } }
  .sc-av.clk, .sc-who.clk { cursor: pointer; }
  .sc-bub.modmsg { padding: 5px 5px 6px; min-width: min(320px, 100%); }
  .sc-bub.modmsg .sc-meta { padding: 0 6px; }
  .sc-txt { font: 500 13px/1.45 var(--font-ui); color: var(--text); white-space: pre-wrap; word-break: break-word; overflow-wrap: anywhere; }
  .sc-modc { margin: 3px 7px 8px; }
  /* Подія передачі збірки — пігулка по центру, як роздільник дня, але з кольоровою іконкою. */
  .sc-evt { align-self: center; display: inline-flex; align-items: center; gap: 8px; max-width: min(460px, 92%); margin: 6px auto;
    padding: 4px 12px 4px 4px; border-radius: 99px; border: 1px solid var(--border); cursor: pointer;
    background: color-mix(in srgb, var(--bg-input) 80%, transparent); backdrop-filter: blur(6px);
    font: 600 11.5px/1.35 var(--font-ui); color: var(--text); text-align: left;
    transition: border-color .15s, background .15s, transform .15s; animation: sc-fade-up .38s ease both; }
  .sc-evt:not(:disabled):hover { border-color: var(--border-hi); background: var(--bg-input); transform: translateY(-1px); }
  .sc-evt:disabled { cursor: default; }
  .sc-evt-ic { width: 22px; height: 22px; border-radius: 50%; display: grid; place-items: center; flex: none; font-size: 13px;
    color: var(--text-mute); background: var(--hover); }
  .sc-evt.warn .sc-evt-ic { color: var(--yellow); background: color-mix(in srgb, var(--yellow) 16%, transparent); }
  .sc-evt.bad .sc-evt-ic { color: var(--red); background: color-mix(in srgb, var(--red) 15%, transparent); }
  .sc-evt-t { min-width: 0; overflow: hidden; text-overflow: ellipsis; display: -webkit-box; -webkit-line-clamp: 2; line-clamp: 2; -webkit-box-orient: vertical; }
  .sc-evt-tm { flex: none; font-size: 10px; font-weight: 600; color: var(--text-mute); }
  .sc-bad { color: var(--text-dim); font-style: italic; display: inline-flex; align-items: center; gap: 5px; }
  .sc-meta { display: flex; align-items: center; justify-content: flex-end; gap: 5px; margin-top: 3px; font: 500 10px/1 var(--font-ui); color: var(--text-dim); }

  /* ── Іконки стану ── */
  .tk { display: inline-flex; align-items: center; justify-content: center; width: 14px; height: 14px; font-size: 14px; }
  .tk.sent { color: var(--text-mute); animation: tk-pop .32s cubic-bezier(.2,1.6,.4,1); }
  .tk.read { color: var(--orange); animation: tk-pop .36s cubic-bezier(.2,1.6,.4,1); }
  .tk.failed { color: var(--red); }
  @keyframes tk-pop { from { opacity: 0; transform: scale(.3) rotate(-25deg); } to { opacity: 1; transform: none; } }
  .tk.sending .ring { width: 11px; height: 11px; border-radius: 50%; border: 2px solid color-mix(in srgb, var(--orange) 28%, transparent); border-top-color: var(--orange); animation: tk-spin .8s linear infinite; }
  @keyframes tk-spin { to { transform: rotate(360deg); } }

  .sc-state { display: flex; align-items: center; gap: 5px; margin: 3px 4px 0; font: 600 10.5px/1.3 var(--font-ui); color: var(--text-dim); }
  .sc-state.sending { color: var(--orange); }
  .sc-state.read { color: var(--orange); }
  .sc-state.failed { color: var(--red); flex-wrap: wrap; justify-content: flex-end; }
  .sc-link { background: none; border: 0; padding: 0 2px; font: 750 10.5px/1.3 var(--font-ui); color: var(--orange); cursor: pointer; text-decoration: underline; text-underline-offset: 2px; }
  .sc-link.muted { color: var(--text-mute); } .sc-link:hover { filter: brightness(1.2); }

  /* крапки «друкує / надсилається» */
  .dots { display: inline-flex; gap: 3px; align-items: center; }
  .dots i { width: 4px; height: 4px; border-radius: 50%; background: currentColor; animation: dot-b 1.1s ease-in-out infinite; }
  .dots i:nth-child(2) { animation-delay: .15s; } .dots i:nth-child(3) { animation-delay: .3s; }
  .dots.big i { width: 6px; height: 6px; background: var(--text-dim); }
  @keyframes dot-b { 0%,60%,100% { transform: translateY(0); opacity: .45; } 30% { transform: translateY(-3px); opacity: 1; } }
  .sc-typ { padding: 11px 13px; border-bottom-left-radius: 5px; }

  /* ── Панель дій над повідомленням (копіювати / видалити) ── */

  /* ── Запрошення на сервер: сама картка — InviteCard.svelte, тут лише обгортка-бульбашка ── */
  .sc-bub.sc-invite { padding: 5px 5px 6px; width: 100%; min-width: 0; }
  .sc-bub.sc-invite .sc-bfoot { padding: 0 6px; }
  .sc-bub.sc-invite .sc-meta { padding: 0; }
  /* Картка запрошення ширша за звичайну бульбашку: даємо їй власну колонку,
     щоб вона не вилазила за межі стрічки на вузькому чаті. */
  .sc-colmsg.invite-col { max-width: min(86%, 420px); }
  .sc-bwrap.invite-bwrap { width: 100%; }
  /* Реакції для широких карток — під карткою, а не збоку (збоку вони
     обрізались межами .sc-msgs). */
  .sc-bwrap.invite-bwrap .sc-hv-react,
  .sc-row.own .sc-bwrap.invite-bwrap .sc-hv-react { left: auto; right: 6px; top: 100%; padding: 0; transform: translate(0, -4px); }
  .sc-bwrap.invite-bwrap:hover .sc-hv-react,
  .sc-bwrap.invite-bwrap:focus-within .sc-hv-react { transform: translate(0, -6px); }

  /* ── «вниз» ── */
  .sc-down { position: absolute; right: 18px; bottom: 76px; z-index: 12; width: 34px; height: 34px; display: flex; align-items: center; justify-content: center; border-radius: 50%; border: 1px solid var(--border-hi); background: var(--bg-panel); color: var(--text); font-size: 16px; cursor: pointer; box-shadow: 0 6px 18px rgba(0,0,0,.45); transition: transform .12s, border-color .14s, bottom .16s var(--ff-spring); }
  /* Смуга цитати/редагування займає місце над полем вводу — кнопка «вниз»
     піднімається вище, щоб не перекривати хрестик скасування відповіді. */
  .sc:has(.sc-replybar) .sc-down { bottom: 128px; }
  .sc-down:hover { border-color: rgba(var(--orange-rgb), .6); } .sc-down:active { transform: scale(.9); }
  .sc-down-n { position: absolute; top: -6px; right: -4px; min-width: 17px; height: 17px; padding: 0 5px; border-radius: 10px; background: var(--orange); color: #111; font: 800 10px/17px var(--font-ui); text-align: center; animation: tk-pop .3s cubic-bezier(.2,1.6,.4,1); }

  /* ── Поле вводу ── */
  .sc-foot { display: flex; align-items: flex-end; gap: 8px; flex-shrink: 0; padding: 10px 14px; border-top: 1px solid var(--border); }
  .sc-foot.off { opacity: .9; }
  .sc-input { flex: 1; min-width: 0; resize: none; max-height: 120px; padding: 9px 12px; border-radius: 11px; border: 1px solid var(--border-hi); background: var(--bg-input); color: var(--text); font: 500 13px/1.4 var(--font-ui); outline: none; transition: border-color .15s, box-shadow .15s; }
  .sc-input:focus { border-color: color-mix(in srgb, var(--orange) 60%, transparent); box-shadow: 0 0 0 3px rgba(var(--orange-rgb), .12); }
  .sc-send { flex: 0 0 auto; width: 36px; height: 36px; display: flex; align-items: center; justify-content: center; border: 0; border-radius: 11px; background: var(--bg-input-hi); color: var(--text-dim); font-size: 16px; cursor: default; transition: background .2s, color .2s, transform .16s cubic-bezier(.2,1.4,.4,1); }
  .sc-send.ready { background: var(--orange); color: #111; cursor: pointer; transform: scale(1.04); }
  .sc-send.ready:hover { filter: brightness(1.08); } .sc-send.ready:active { transform: scale(.9); }
  .sc-send { overflow: hidden; }
  .sc-send.fly i { animation: sc-plane .46s ease-in-out; }
  @keyframes sc-plane { 0% { transform: none; opacity: 1; } 45% { transform: translate(18px, -18px) rotate(-8deg); opacity: 0; } 46% { transform: translate(-18px, 18px) rotate(-8deg); opacity: 0; } 100% { transform: none; opacity: 1; } }
  .sc-send.ready i { animation: sc-fly .3s ease-out; }
  @keyframes sc-fly { from { transform: translate(-3px, 3px) rotate(-12deg); opacity: .4; } to { transform: none; opacity: 1; } }

  .sc-row { position: relative; }
  .sc-row { transition: padding-left .22s cubic-bezier(.2,.9,.3,1), background .14s; }
  .sc-row.selecting { padding-left: 32px; cursor: pointer; user-select: none; border-radius: 12px; }
  .sc-row.selecting:hover { background: var(--hover); }
  .sc-row.picked { background: rgba(var(--orange-rgb), .1) !important; }
  .sc-chk { position: absolute; left: 6px; top: calc(50% - 9px); width: 18px; height: 18px; border-radius: 50%; border: 2px solid var(--border-hi); display: flex; align-items: center; justify-content: center; color: transparent; font-size: 12px; transition: background .14s, border-color .14s, color .14s; }
  .sc-chk { animation: sc-chk-in .24s cubic-bezier(.2,1.4,.4,1) both; }
  @keyframes sc-chk-in { from { opacity: 0; transform: scale(.3); } to { opacity: 1; transform: none; } }
  .sc-chk.on { background: var(--orange); border-color: var(--orange); color: #111; animation: tk-pop .24s cubic-bezier(.2,1.6,.4,1); }
  .sc-selbar { display: flex; align-items: center; gap: 8px; flex-shrink: 0; padding: 10px 14px; border-top: 1px solid var(--border); background: var(--bg-panel); }
  .sc-selx { width: 32px; height: 32px; display: flex; align-items: center; justify-content: center; border: 0; border-radius: 9px; background: var(--hover); color: var(--text-mute); font-size: 16px; cursor: pointer; }
  .sc-selx:hover { background: var(--hover-strong); color: var(--text); }
  .sc-seln { flex: 1; min-width: 0; font: 700 12.5px/1.2 var(--font-ui); color: var(--text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .sc-selbtn { display: inline-flex; align-items: center; gap: 6px; padding: 8px 13px; border-radius: 10px; border: 1px solid var(--border-hi); background: var(--bg-input); color: var(--text); font: 650 12px/1 var(--font-ui); cursor: pointer; transition: background .14s, border-color .14s, color .14s, transform .12s; }
  .sc-selbtn:hover { border-color: rgba(var(--orange-rgb), .6); } .sc-selbtn:active { transform: scale(.95); } .sc-selbtn:disabled { opacity: .55; cursor: default; }
  .sc-selbtn.danger:hover { border-color: color-mix(in srgb, var(--red) 60%, transparent); color: var(--red); background: rgba(239,68,68,.1); }
  .sc-menu { position: fixed; z-index: 100001; min-width: 190px; padding: 5px; border-radius: 12px; background: var(--bg-panel); border: 1px solid var(--border-hi); box-shadow: 0 12px 32px rgba(0,0,0,.5); }
  .sc-menu button { display: flex; align-items: center; gap: 10px; width: 100%; padding: 8px 11px; border: 0; border-radius: 8px; background: transparent; color: var(--text); font: 600 12.5px/1 var(--font-ui); text-align: left; cursor: pointer; transition: background .12s; }
  .sc-menu button:hover { background: var(--hover-strong); }
  .sc-menu button i { font-size: 16px; color: var(--text-mute); }
  .sc-menu button.danger, .sc-menu button.danger i { color: var(--red); }
  .sc-menu button.danger:hover { background: rgba(239,68,68,.12); }
  .sc-menu hr { border: 0; height: 1px; margin: 4px 2px; background: var(--border); }
  .sc-msgs.selecting { user-select: none; }
  .sc-menu { transform-origin: top left; }
  .sc-it.intro { animation: sc-rise .45s cubic-bezier(.2,.9,.3,1) both; animation-delay: var(--d, 0ms); }
  @keyframes sc-rise { from { opacity: 0; transform: translateY(14px); } to { opacity: 1; transform: none; } }
  .sc-day, .sc-newdiv { animation: sc-fade-up .38s ease both; }
  @keyframes sc-fade-up { from { opacity: 0; transform: translateY(6px); } to { opacity: 1; transform: none; } }
  .sc-row:not(.own):not(.selecting) .sc-bub:hover { border-color: var(--border-hi); }
  .sc-row.own:not(.selecting) .sc-bub:hover { filter: brightness(1.08); }
  /* скелетон завантаження історії */
  .sc-skel { display: flex; flex-direction: column; gap: 12px; padding: 18px 4px; }
  .sk-row { display: flex; align-items: flex-end; gap: 8px; animation: sc-fade-up .4s ease both; animation-delay: var(--sd, 0ms); }
  .sk-row.own { justify-content: flex-end; }
  .sk-av { flex: 0 0 26px; width: 26px; height: 26px; border-radius: 8px; }
  .sk-bub { height: 38px; max-width: 74%; border-radius: 15px; }
  .sk-av, .sk-bub { background: linear-gradient(100deg, var(--bg-input) 30%, color-mix(in srgb, var(--bg-input-hi, var(--bg-input)) 80%, var(--orange) 8%) 50%, var(--bg-input) 70%); background-size: 220% 100%; animation: sk-shimmer 1.5s ease-in-out infinite; }
  @keyframes sk-shimmer { from { background-position: 120% 0; } to { background-position: -120% 0; } }
  .sr-only { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); white-space: nowrap; }
  .sc-spam { display: flex; align-items: center; gap: 7px; flex-shrink: 0; margin: 0 14px 6px; padding: 6px 11px; border-radius: 9px; border: 1px solid rgba(234,179,8,.35); background: rgba(234,179,8,.09); font: 600 11.5px/1.3 var(--font-ui); color: var(--text); }
  .sc-spam i { color: #eab308; font-size: 14px; }
  .sc-offline { display: flex; align-items: center; gap: 6px; flex-shrink: 0; padding: 0 16px 10px; font: 500 11px/1.4 var(--font-ui); color: var(--text-dim); }
  .sc-offline i { color: #eab308; }

  @media (prefers-reduced-motion: reduce) {
    .sc-row.fresh .sc-bub, .sc-row.flash .sc-bub, .sc-bub.failed, .sc-st.in_game, .sc-empty-ic, .tk, .sc-it.intro, .sc-day, .sc-newdiv, .sk-row, .sk-av, .sk-bub, .sc-chk, .sc-send.fly i { animation: none !important; }
  }
  .sc-handle { margin-left: 6px; font: 500 11px/1 var(--font-mono); color: var(--text-mute); }
</style>
