<script lang="ts">
  // ── Віджет сповіщень у трей (стиль Steam) ────────────────────────────────
  //
  // Живе в ОКРЕМОМУ вікні (frontend/notify.html), яке створює
  // backend/notify_window.go. Завдання сторінки — показати стос карток і
  // повідомити бекенд про фактичну висоту контенту (App.SetNotifyPopupSize),
  // бо саме він розміщує вікно в правому нижньому куту та ховає його, коли
  // стос порожній (інакше прозоре вікно «їла» б кліки по куту екрана).
  //
  // БЕЗПЕКА ВІДОБРАЖЕННЯ: заголовок/текст уже санітизовані на бекенді, але
  // ми не робимо {@html} — лише текстове інтерполяція. Сирі помилки тут
  // взагалі не зʼявляються: контент приходить лише з бекенда.
  import { onMount, tick } from 'svelte'
  import { App, on } from './wails'
  import { t, setLanguage } from './i18n.svelte'
  import { syncAppearance } from './theme.svelte'
  import type { SocialNotifyPayload } from './types'
  import notifySoundUrl from '../assets/sounds/chat-notify.mp3'

  // Тривалість показу картки. ОСНОВНЕ джерело — поле durationMs у самій
  // події (бекенд підставляє його з налаштувань «Час показу» при
  // постановці в чергу), тож зміна налаштування діє вже з НАСТУПНОГО
  // сповіщення без перестворення вікна. ttl тут — запасний варіант для
  // карток без durationMs + початкове значення до першого завантаження
  // налаштувань.
  let ttl = 7000
  const TTL_MIN = 3000
  const TTL_MAX = 60000

  // Час показу конкретної картки з урахуванням індивідуального durationMs.
  const ttlOf = (p: SocialNotifyPayload) => {
    const ms = typeof p.durationMs === 'number' && isFinite(p.durationMs) && p.durationMs > 0
      ? p.durationMs : ttl
    return Math.min(TTL_MAX, Math.max(TTL_MIN, ms))
  }
  // Максимум одночасно видимих карток (має збігатися з notifyMaxStack у бекенді).
  // Стос росте ВГОРУ (нижній кут вікна нерухомий, нові картки зверху). Коли
  // карток уже MAX, нова просто ВИТІСНЯЄ найстарішу (нижню): вона плавно
  // зникає, а на її місце «перемикається» свіжа.
  const MAX = 3
  // Тривалість анімації зникнення (має збігатися з transition у стилях).
  const GONE = 260
  // Ширина вікна. Фіксована: вікно не змінює ширину, лише висоту.
  const POPUP_W = 360

  interface Card {
    key: string
    kind: string
    title: string
    body: string
    accountId: string
    ts: number
    leaving: boolean
    timer?: number
    left: number
    started: number
  }

  let cards = $state<Card[]>([])
  let stack: HTMLDivElement | undefined = $state()
  let lastW = -1
  let lastH = -1

  // Голови гравців. Як і в шухляді «Друзі» — одна спроба на нік (seen),
  // щоб невдалий запит не повторювався на кожен рендер.
  const heads = $state<Record<string, string>>({})
  const headSeen = new Set<string>()

  const HIDDEN = /[\u0000-\u001f\u007f-\u009f\u200b-\u200f\u202a-\u202e\u2066-\u2069\ufeff]/g
  const clean = (s?: string) => (s ?? '').replace(HIDDEN, '').trim()

  function wantHead(name: string) {
    if (!name || headSeen.has(name)) return
    headSeen.add(name)
    App.GetAccountHead('', name, false)
      .then((h: string) => { if (typeof h === 'string' && h.startsWith('data:image/')) heads[name] = h })
      .catch(() => {})
  }

  // Звук нового повідомлення. Мут звуку (ChatMuteSound) перевіряємо щоразу:
  // користувач міг змінити його вже після відкриття цього вікна.
  let notifyAudio: HTMLAudioElement | null = null
  async function playSound() {
    try {
      const m = await App.GetChatMuteState()
      if (m?.sound) return
      notifyAudio ??= new Audio(notifySoundUrl)
      notifyAudio.volume = 0.7
      notifyAudio.currentTime = 0
      await notifyAudio.play()
    } catch { /* звук не критичний: автоплей заблоковано або файл недоступний */ }
  }

  // ── Додавання/видалення карток ──────────────────────────────────────────
  function add(p: SocialNotifyPayload | null | undefined) {
    if (!p) return
    const title = clean(p.title)
    const body = clean(p.body)
    if (!title && !body) return
    const key = p.id || `${p.kind}-${p.accountId}-${p.ts}`
    if (cards.some(c => c.key === key)) return
    void playSound()

    // Для чату: одна картка на співрозмовника — нове повідомлення ОНОВЛЮЄ
    // текст і скидає таймер, а не додає черговий рядок. Це і не спамить, і
    // тримає стос у межах MAX при активній переписці.
    const dup = p.kind === 'chat' && p.accountId
      ? cards.findIndex(c => c.kind === 'chat' && c.accountId === p.accountId)
      : -1

    const fresh: Card = {
      key, kind: p.kind || 'chat', title, body,
      accountId: p.accountId || '', ts: p.ts || Date.now(),
      leaving: false, left: ttlOf(p), started: 0,
    }
    if (dup >= 0) {
      const kept = cards[dup]
      if (kept.timer) window.clearTimeout(kept.timer)
      cards = [fresh, ...cards.filter((_, i) => i !== dup)]
    } else {
      cards = [fresh, ...cards]
    }
    // Переповнення: витісняємо найстарші активні картки (з кінця стосу).
    const active = cards.filter(c => !c.leaving)
    for (let i = active.length - 1; i >= MAX; i--) dismiss(active[i].key)
    wantHead(title)
    armAll()
    void sync()
  }

  function arm(c: Card) {
    if (c.timer || c.leaving) return
    c.started = Date.now()
    c.timer = window.setTimeout(() => dismiss(c.key), c.left)
  }
  function armAll() { for (const c of cards) arm(c) }
  function pauseAll() {
    for (const c of cards) {
      if (!c.timer) continue
      window.clearTimeout(c.timer)
      c.timer = undefined
      c.left = Math.max(600, c.left - (Date.now() - c.started))
    }
  }

  function dismiss(key: string) {
    const c = cards.find(x => x.key === key)
    if (!c || c.leaving) return
    if (c.timer) { window.clearTimeout(c.timer); c.timer = undefined }
    c.leaving = true
    window.setTimeout(() => {
      cards = cards.filter(x => x.key !== key)
      void sync()
    }, GONE)
  }

  // ── Розмір вікна ────────────────────────────────────────────────────────
  // Вікно має бути рівно тим, що малюється: більше — і воно перекриває
  // кути екрана непомітними для мишенятка (webview приймає кліки), менше —
  // і картки обрізаються. Тому міряємо КОНТЕЙНЕР і шлемо бекенду.
  async function sync() {
    await tick()
    const h = cards.length === 0 ? 0 : Math.ceil(stack?.getBoundingClientRect().height || 0)
    const w = POPUP_W
    if (w === lastW && h === lastH) return
    lastW = w
    lastH = h
    App.SetNotifyPopupSize(w, h).catch(() => {})
  }

  function open(c: Card) {
    // Службове сповіщення (NotifyOS: завантаження завершено, помилка
    // встановлення...) не веде в чат — клік лише закриває картку.
    if (c.kind === 'info') { dismiss(c.key); return }
    // Бекенд сам покаже/активізує головне вікно, шле social:open-chat і
    // ховає тост. Для запрошення (без accountId) відкриється просто
    // шухляда «Друзі» — це теж нормальний результат.
    App.OpenChatFromNotification(c.accountId).catch(() => {})
  }

  const bodyOf = (c: Card) => (c.kind === 'invite' && !c.body ? t('social.notify.friendRequest') : c.body)

  onMount(() => {
    // Вигляд вікна = вигляд лаунчера. Це ОКРЕМИЙ документ (свій <html>),
    // тож тема/акцент/шрифт із головного вікна сюди самі не переходять:
    // без syncAppearance тост завжди малювався дефолтно-темним з
    // помаранчевим акцентом, навіть якщо користувач вибрав світлу тему чи
    // інший акцент. syncAppearance ще й слухає "settings:saved" — зміна
    // теми підхоплюється наживо. Разом із налаштуваннями дістаємо мову й
    // час показу (запасне значення для карток без durationMs).
    let offAppearance: (() => void) | undefined
    syncAppearance().then(({ settings: s, off }) => {
      offAppearance = off
      if (s?.language) setLanguage(s.language)
      const sec = typeof (s as any)?.notifyDurationSec === 'number' && isFinite((s as any).notifyDurationSec)
        ? Math.round((s as any).notifyDurationSec) : 7
      ttl = Math.min(TTL_MAX, Math.max(TTL_MIN, sec * 1000))
    }).catch(() => {
      // Налаштування не прийшли — лишаємо дефолти з style.css і мову як є.
    })
    const offs = [
      on('notify:push', (p: SocialNotifyPayload) => add(p)),
    ]
    // Подія могла прийти ДО того, як це вікно встигло підписатись
    // (створення webview асинхронне) — бекенд поклав їх у чергу.
    App.TakeNotifyPopup()
      .then((list: SocialNotifyPayload[]) => { (list ?? []).forEach(add) })
      .catch(() => {})
    return () => { offs.forEach(off => off()); offAppearance?.() }
  })

</script>

<div
  class="np"
  bind:this={stack}
  onpointerenter={pauseAll}
  onpointerleave={armAll}
>
  {#each cards as c (c.key)}
    <div
      class="np-card"
      class:leaving={c.leaving}
      class:invite={c.kind === 'invite'}
      role="button"
      tabindex="0"
      aria-label={t('social.notify.openChat')}
      onclick={() => open(c)}
      onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); open(c) } }}
    >
      <span class="np-av">
        {#if heads[c.title]}<img src={heads[c.title]} alt="" />{:else}<i class="ti ti-flame"></i>{/if}
      </span>
      <span class="np-text">
        <span class="np-title">{c.title}</span>
        <span class="np-sub">{bodyOf(c)}</span>
      </span>
      <button
        class="np-x"
        aria-label={t('social.notify.dismiss')}
        title={t('social.notify.dismiss')}
        onclick={(e) => { e.stopPropagation(); dismiss(c.key) }}
      >
        <i class="ti ti-x"></i>
      </button>
    </div>
  {/each}
</div>

<style>
  /* Вікно тостів ВСУЦІЛЬНУ заповнене стосом карток: без прозорих відступів,
     тіней і заокруглень. Раніше навколо карток був прозорий відступ 12px —
     якщо WebView2/DWM не давав справжньої прозорості (віддалений робочий
     стіл, ВМ, без GPU-прискорення), він малювався сірим прямокутником.
     Тепер вікно = стос, тож прозорість не потрібна взагалі і однаково
     виглядає на Windows та Linux. Фон html/body — суцільний, !important,
     щоб його не перекрив інлайн-стиль чи style.css. */
  :global(html), :global(body) { background: var(--bg-panel) !important; margin: 0; }
  :global(#app) { background: transparent !important; height: auto !important; display: block !important; }

  /* Контейнер — ЄДИНЕ джерело висоти вікна. overflow:hidden обрізає
     анімацію появи/зникнення карток по межі вікна. */
  .np {
    box-sizing: border-box;
    width: 360px;
    display: flex;
    flex-direction: column;
    background: var(--bg-panel);
    border: 1px solid var(--border-hi);
    overflow: hidden;
  }

  .np-card {
    position: relative;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 9px 30px 9px 10px;
    background: var(--bg-panel);
    border-bottom: 1px solid var(--border);
    cursor: pointer;
    animation: np-in .26s cubic-bezier(.22, 1.1, .36, 1) both;
    transition: opacity .24s ease, transform .24s ease;
  }
  .np-card:last-child { border-bottom: 0; }
  .np-card.leaving {
    opacity: 0;
    transform: translateY(8px);
    pointer-events: none;
  }
  .np-card:hover { background: var(--hover); }
  .np-card:focus-visible { outline: 2px solid var(--orange); outline-offset: -2px; }

  @keyframes np-in {
    from { opacity: 0; transform: translateY(-14px); }
    to   { opacity: 1; transform: translateY(0); }
  }

  .np-av {
    flex: 0 0 auto;
    width: 34px;
    height: 34px;
    border-radius: 9px;
    background: var(--bg-input);
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--orange);
    font-size: 17px;
    overflow: hidden;
  }
  .np-av img { width: 100%; height: 100%; image-rendering: pixelated; }

  .np-text { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
  .np-title {
    font: 650 12.5px/1.25 var(--font-ui);
    color: var(--text);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .np-sub {
    font: 500 11.5px/1.35 var(--font-ui);
    color: var(--text-mute);
    display: -webkit-box;
    line-clamp: 2;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
    word-break: break-word;
  }

  .np-x {
    position: absolute;
    top: 50%;
    right: 8px;
    transform: translateY(-50%);
    width: 24px;
    height: 24px;
    border: 1px solid var(--border);
    border-radius: 8px;
    background: var(--hover);
    color: var(--text-mute);
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 13px;
    cursor: pointer;
    opacity: .55;
    transition: opacity .14s, background .14s, color .14s, border-color .14s, transform .14s;
  }
  .np-card:hover .np-x,
  .np-x:focus-visible { opacity: 1; }
  .np-x:hover { background: rgba(239,68,68,.12); border-color: rgba(239,68,68,.45); color: var(--red); }
  .np-x:active { transform: translateY(-50%) scale(.92); }
  .np-x:focus-visible { outline: 2px solid var(--orange); outline-offset: 1px; }

  /* Запрошення в друзі — акцентна смужка зліва. */
  .np-card.invite { box-shadow: inset 3px 0 0 var(--orange); }
</style>
