<script lang="ts">
  // ── «Гравці» — жива смуга над консоллю сервера ─────────────────────────
  // Реєстр веде бекенд (internal/servers/players.go): читає рядки логу java
  // («Steve joined the game» / «left the game» / «lost connection») і тримає
  // список з ніками. Сюди приходить і початковий знімок (GetServerPlayers),
  // і кожна подія входу/виходу (server:player).
  //
  // «Реальний час»: список міняється миттєво по події, «час на сервері» тікає
  // сам (раніше рахувався один раз при рендері і застигав), а тиха фонова
  // синхронізація зі знімком бекенду підстраховує пропущену подію — окрема
  // кнопка «оновити» більше не потрібна.
  //
  // Тости з ніком показує App.svelte (глобально, з будь-якої сторінки) —
  // тут лише список, щоб не було подвійних повідомлень.
  import { flip } from 'svelte/animate'
  import { scale, fade, fly } from 'svelte/transition'
  import { App, on } from '../../wails'
  import FriendAvatar from '../../friends/FriendAvatar.svelte'
  import type { ServerPlayer, ServerPlayerPlaytime } from '../../types'

  let { serverId = '', running = false }: { serverId?: string; running?: boolean } = $props()

  const RESYNC_MS = 15000
  const TICK_MS = 10000

  let players = $state<ServerPlayer[]>([])
  let loaded = $state(false)
  let now = $state(Date.now())
  // ── «Час гри» — накопичений час кожного гравця (stats світу) ───────────
  // Окрема панель під смугою: показує й тих, хто зараз офлайн. Сервер
  // скидає stats на диск при виході гравця та автозбереженні, тому після
  // події leave перечитуємо з невеликою затримкою.
  const PLAYTIME_MS = 30000
  let showTime = $state(false)
  let playtimes = $state<ServerPlayerPlaytime[]>([])
  let timeLoaded = $state(false)
  let timeSeq = 0

  async function refreshTimes() {
    if (!serverId) return
    const seq = ++timeSeq
    try {
      const list = (await App.GetServerPlayerPlaytimes(serverId)) ?? []
      if (seq === timeSeq) playtimes = list
    } catch { /* лишаємо попередній список */ }
    finally { timeLoaded = true }
  }

  // Лічильник подій: відповідь знімка, що вилетіла ДО останньої події, застаріла.
  let evSeq = 0

  async function refresh() {
    if (!serverId) return
    const seq = evSeq
    try {
      const list = (await App.GetServerPlayers(serverId)) ?? []
      if (seq === evSeq) players = list
    } catch { /* тиха синхронізація: лишаємо те, що є */ }
    finally { loaded = true }
  }

  $effect(() => {
    const id = serverId
    if (!id) return
    loaded = false
    void refresh()
    const off = on('server:player', (ev: any) => {
      if (!ev || ev.serverId !== id) return
      evSeq++
      if (ev.action === 'reset') { players = []; if (showTime) void refreshTimes(); return }
      if (showTime) setTimeout(() => { void refreshTimes() }, ev.action === 'leave' ? 2500 : 300)
      // Повний список приходить у САМІЙ події — не збираємо стан локально
      // інкрементами, інакше одна пропущена подія = розбіжність із реальністю.
      const prev = new Map(players.map((p) => [p.name, p]))
      players = ((ev.list ?? []) as string[]).map((n) => {
        const old = prev.get(n)
        if (old) return old
        return ev.action === 'join' && n === ev.name ? { name: n, joinedAt: ev.at } : { name: n }
      })
    })
    return off
  })

  // Тиха синхронізація, поки сервер працює, і при кожному його запуску.
  $effect(() => {
    if (!serverId || !running) return
    void refresh()
    const h = setInterval(() => { void refresh() }, RESYNC_MS)
    return () => clearInterval(h)
  })

  // Панель «Час гри»: читаємо при відкритті й оновлюємо, поки вона відкрита.
  $effect(() => {
    if (!serverId || !showTime) return
    timeLoaded = false
    void refreshTimes()
    const h = setInterval(() => { void refreshTimes() }, PLAYTIME_MS)
    return () => clearInterval(h)
  })

  // Годинник для «N хв на сервері».
  $effect(() => {
    const h = setInterval(() => { now = Date.now() }, TICK_MS)
    return () => clearInterval(h)
  })

  function fmtSince(at: number | undefined, nowMs: number): string {
    if (!at) return ''
    const mins = Math.max(0, Math.floor((nowMs - at) / 60000))
    if (mins < 1) return 'щойно'
    if (mins < 60) return `${mins} хв`
    const h = Math.floor(mins / 60)
    return `${h} год ${mins % 60} хв`
  }

  // Загальний час: «12 год 5 хв», «3 хв», «<1 хв»; від доби — «3 д 4 год».
  function fmtTotal(sec: number): string {
    const mins = Math.floor(sec / 60)
    if (mins < 1) return '<1 хв'
    if (mins < 60) return `${mins} хв`
    const h = Math.floor(mins / 60)
    if (h < 24) return `${h} год ${mins % 60} хв`
    return `${Math.floor(h / 24)} д ${h % 24} год`
  }

  function fmtAgo(at: number | undefined, nowMs: number): string {
    if (!at) return ''
    const mins = Math.max(0, Math.floor((nowMs - at) / 60000))
    if (mins < 2) return 'щойно'
    if (mins < 60) return `${mins} хв тому`
    const h = Math.floor(mins / 60)
    if (h < 24) return `${h} год тому`
    return `${Math.floor(h / 24)} д тому`
  }

  const maxSeconds = $derived(Math.max(1, ...playtimes.map((p) => p.seconds)))

  // Панель «Час гри» — випадаюча: закривається кліком поза смугою та Esc.
  let root = $state<HTMLElement | null>(null)
  function onWindowPointer(e: PointerEvent) {
    if (showTime && root && !root.contains(e.target as Node)) showTime = false
  }
  function onWindowKey(e: KeyboardEvent) {
    if (showTime && e.key === 'Escape') showTime = false
  }
</script>

<svelte:window onpointerdown={onWindowPointer} onkeydown={onWindowKey} />

<div class="sps" bind:this={root}>
  <div class="sps-head">
    <i class="ti ti-users"></i>
    <span class="sps-title">Гравці</span>
    <span class="sps-n">{players.length}</span>
  </div>
  <button class="sps-toggle" class:on={showTime} onclick={() => (showTime = !showTime)}
          title="Скільки кожен гравець награв на сервері за весь час" aria-expanded={showTime}>
    <i class="ti ti-clock-hour-4"></i> Час гри
  </button>
  {#if !loaded}
    <span class="sps-hint"><i class="ti ti-loader spin"></i> завантаження…</span>
  {:else if players.length === 0}
    <span class="sps-hint">
      {running
        ? 'Нікого немає — тут зʼявляться ті, хто зайде'
        : 'Сервер зупинено'}
    </span>
  {:else}
    <div class="sps-list">
      {#each players as p (p.name)}
        <span class="sps-chip"
              title={p.joinedAt ? `На сервері: ${fmtSince(p.joinedAt, now)}` : 'Час входу невідомий'}
              in:scale={{ duration: 180, start: 0.85 }} out:fade={{ duration: 120 }}
              animate:flip={{ duration: 200 }}>
          <FriendAvatar nickname={p.name} status="online" size={22} />
          <span class="sps-name">{p.name}</span>
          {#if p.joinedAt}<span class="sps-time">{fmtSince(p.joinedAt, now)}</span>{/if}
        </span>
      {/each}
    </div>
  {/if}

  {#if showTime}
    <div class="sps-pop" role="dialog" aria-label="Час гри" transition:fly={{ y: -6, duration: 140 }}>
      <div class="sps-pop-head">
        <i class="ti ti-clock-hour-4"></i>
        <b>Час гри</b>
        <span class="sps-pop-sub">за весь час на цьому сервері</span>
        <button class="sps-pop-x" onclick={() => (showTime = false)} aria-label="Закрити"><i class="ti ti-x"></i></button>
      </div>
      <div class="sps-pop-body">
        {#if !timeLoaded}
          <span class="sps-hint"><i class="ti ti-loader spin"></i> читаю статистику світу…</span>
        {:else if playtimes.length === 0}
          <span class="sps-hint">Статистики ще немає — зʼявиться після першого збереження світу гравцем</span>
        {:else}
          <div class="sps-grid">
            {#each playtimes as p (p.uuid || p.name)}
              <div class="sps-card" class:online={p.online}>
                <FriendAvatar nickname={p.name} status={p.online ? 'online' : 'offline'} size={34} />
                <div class="sps-card-main">
                  <div class="sps-card-top">
                    <span class="sps-tname" title={p.name}>{p.name}</span>
                    <span class="sps-ttotal">{p.seconds > 0 ? fmtTotal(p.seconds) : '—'}</span>
                  </div>
                  <span class="sps-bar"><span style:width="{Math.max(3, Math.round((p.seconds / maxSeconds) * 100))}%"></span></span>
                  <span class="sps-tnote">
                    {#if p.online}
                      {p.sessionStart ? `онлайн · ${fmtSince(p.sessionStart, now)}` : 'онлайн'}
                    {:else if p.lastSeen}
                      був {fmtAgo(p.lastSeen, now)}
                    {:else}
                      —
                    {/if}
                  </span>
                </div>
              </div>
            {/each}
          </div>
        {/if}
      </div>
    </div>
  {/if}
</div>

<style>
  .sps {
    position: relative;
    display: flex; align-items: center; gap: 10px; flex-wrap: wrap;
    padding: 8px 14px; flex-shrink: 0;
    border-bottom: 1px solid var(--border);
    background: color-mix(in srgb, var(--bg-window) 92%, transparent);
  }
  .sps-head { display: flex; align-items: center; gap: 6px; flex-shrink: 0; }
  .sps-head i { color: var(--text-mute); font-size: 14px; }
  .sps-title { font: 700 11px/1 var(--font-ui); color: var(--text-mute); text-transform: uppercase; letter-spacing: .5px; }
  .sps-n {
    font: 700 var(--fs-min)/1 var(--font-mono); color: var(--text);
    background: var(--hover); padding: 3px 7px; border-radius: 999px;
  }
  .sps-hint { font: 400 11px/1.4 var(--font-ui); color: var(--text-dim); display: inline-flex; align-items: center; gap: 5px; }
  .sps-list { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; min-width: 0; }
  .sps-chip {
    display: inline-flex; align-items: center; gap: 7px;
    padding: 3px 11px 3px 4px; border-radius: 999px;
    background: var(--bg-input); border: 1px solid var(--border);
    transition: border-color .15s, background .15s;
  }
  .sps-chip:hover { border-color: var(--border-hi); background: var(--hover); }
  .sps-toggle {
    display: inline-flex; align-items: center; gap: 5px; margin-left: auto;
    padding: 4px 10px; border-radius: 999px; cursor: pointer;
    font: 600 11px/1 var(--font-ui); color: var(--text-mute);
    background: transparent; border: 1px solid var(--border);
    transition: border-color .15s, background .15s, color .15s;
  }
  .sps-toggle:hover { border-color: var(--border-hi); background: var(--hover); color: var(--text); }
  .sps-toggle.on { color: var(--text); background: var(--hover); border-color: var(--border-hi); }
  .sps-pop {
    position: absolute; z-index: 20; top: calc(100% + 6px); left: 10px; right: 10px;
    display: flex; flex-direction: column; max-height: min(420px, 62vh);
    background: var(--bg-panel); border: 1px solid var(--border-hi); border-radius: 14px;
    box-shadow: 0 18px 48px rgba(0, 0, 0, .55);
  }
  .sps-pop-head {
    display: flex; align-items: center; gap: 8px; padding: 11px 14px; flex-shrink: 0;
    border-bottom: 1px solid var(--border);
  }
  .sps-pop-head > i { color: var(--text-mute); font-size: 15px; }
  .sps-pop-head b { font: 700 13px/1 var(--font-ui); color: var(--text); }
  .sps-pop-sub { font: 400 11px/1 var(--font-ui); color: var(--text-dim); flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .sps-pop-x {
    width: 26px; height: 26px; border-radius: 8px; display: grid; place-items: center; cursor: pointer;
    background: transparent; border: 1px solid var(--border); color: var(--text-mute); font-size: 13px;
  }
  .sps-pop-x:hover { background: var(--hover); color: var(--text); border-color: var(--border-hi); }
  .sps-pop-body { padding: 12px; overflow-y: auto; min-height: 0; }
  .sps-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(230px, 1fr)); gap: 8px; }
  .sps-card {
    display: flex; align-items: center; gap: 10px; min-width: 0; padding: 10px 12px;
    border-radius: 12px; background: var(--bg-input); border: 1px solid var(--border);
  }
  .sps-card.online { border-color: color-mix(in srgb, var(--accent, var(--text)) 40%, var(--border)); }
  .sps-card-main { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 6px; }
  .sps-card-top { display: flex; align-items: baseline; justify-content: space-between; gap: 8px; min-width: 0; }
  .sps-tname { font: 650 13px/1.1 var(--font-ui); color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; }
  .sps-ttotal { font: 700 12px/1 var(--font-mono); color: var(--text); white-space: nowrap; flex-shrink: 0; }
  .sps-bar { height: 4px; border-radius: 999px; background: var(--hover); overflow: hidden; }
  .sps-bar > span { display: block; height: 100%; border-radius: inherit; background: var(--text-dim); }
  .sps-card.online .sps-bar > span { background: var(--accent, var(--text)); }
  .sps-tnote { font: 400 var(--fs-min)/1 var(--font-ui); color: var(--text-dim); }
  .sps-name { font: 650 12px/1 var(--font-ui); color: var(--text); }
  .sps-time { font: 400 var(--fs-min)/1 var(--font-mono); color: var(--text-dim); }
</style>
