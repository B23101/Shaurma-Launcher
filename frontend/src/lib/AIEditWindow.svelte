<script lang="ts">
  // ── AIEditWindow: накладка «ШІ-агент керує збіркою» ───────────────────────
  // Перекриває ЛИШЕ область сторінки збірки (бокова панель і решта лаунчера лишаються доступними),
  // а не весь застосунок. Поки агент працює зі збіркою, гравець не може її правити чи запускати
  // (guard у бекенді). Показує, що робиться зараз, лічильники й стрічку змін (моди з іконками).
  // Стиль — токени лаунчера.

  import { onMount, onDestroy } from 'svelte'
  import { fade, fly } from 'svelte/transition'
  import { flip } from 'svelte/animate'
  import { cubicOut } from 'svelte/easing'
  import { getLanguage } from './i18n.svelte'
  import { App } from './wails'
  import { confirmDialog } from './toast.svelte'
  import { aiEdit, initAIEdit } from './aiAgentEdit.svelte'

  let { packId }: { packId: string } = $props()

  const L = {
    uk: {
      active: 'ШІ-агент керує збіркою', done: 'ШІ завершив роботу зі збіркою', now: 'Зараз', idle: 'чекає наступної дії',
      locked: 'Збірка заблокована: запуск і зміни доступні після завершення.', empty: 'Змін поки немає',
      stopTitle: 'Відключити ШІ-агента?', cancel: 'Скасувати',
      stop: 'Відключити агента', stopConfirm: 'Агент буде відключений: його запити відхилятимуться 10 хвилин (або доки ви не натиснете «Дозволити знову» в Налаштуваннях → ШІ-агент). Сам MCP-сервер лишиться ввімкненим.',
      minimize: 'Згорнути', expand: 'Розгорнути', close: 'Закрити', mods: 'моди', settings: 'налаштування', other: 'інше',
      last: 'Остання дія', since: 'працює',
    },
    en: {
      active: 'AI agent is managing this build', done: 'AI agent finished with this build', now: 'Now', idle: 'waiting for the next action',
      locked: 'The build is locked: launching and changes are available when it finishes.', empty: 'No changes yet',
      stopTitle: 'Disconnect the AI agent?', cancel: 'Cancel',
      stop: 'Disconnect agent', stopConfirm: 'The agent will be disconnected: its requests are refused for 10 minutes (or until you press “Allow again” in Settings → AI agent). The MCP server itself stays on.',
      minimize: 'Minimize', expand: 'Expand', close: 'Close', mods: 'mods', settings: 'settings', other: 'other',
      last: 'Last action', since: 'running',
    },
  }
  const tr = $derived(getLanguage() === 'en' ? L.en : L.uk)

  const session = $derived(aiEdit.get(packId))
  const show = $derived(!!session && (session.active || session.changes.length > 0))
  let minimized = $state(false)

  const counts = $derived.by(() => {
    const c = { mod: 0, setting: 0, other: 0, err: 0 }
    for (const x of session?.changes ?? []) {
      if (!x.ok) c.err++
      else if (x.kind === 'mod') c.mod++
      else if (x.kind === 'setting' || x.kind === 'style') c.setting++
      else c.other++
    }
    return c
  })
  const clean = (x: string) => x.replace(/\.jar/gi, '')
  const lastAction = $derived(clean(session?.changes?.[0]?.summary ?? ''))

  // Накладка кріпиться до області вмісту (.content): координати фіксуються за її розміром.
  let anchor = $state<HTMLElement | null>(null)
  let box = $state({ left: 0, top: 0, width: 0, height: 0 })
  let ro: ResizeObserver | null = null
  function place() {
    const host = anchor?.closest('.content') as HTMLElement | null ?? document.querySelector('.content') as HTMLElement | null
    if (!host) return
    const r = host.getBoundingClientRect()
    box = { left: r.left, top: r.top, width: r.width, height: r.height }
  }

  // Іконки модів тягнемо з jar лише для нових файлів і кешуємо.
  let icons = $state<Record<string, string>>({})
  $effect(() => {
    const files = (session?.changes ?? []).map((c) => c.file).filter((f): f is string => !!f && !(f in icons))
    if (!files.length) return
    const unique = [...new Set(files)]
    App.GetModIcons(packId, unique)
      .then((m: Record<string, string>) => {
        const next = { ...icons }
        for (const f of unique) next[f] = m?.[f] ?? ''
        icons = next
      })
      .catch(() => {
        const next = { ...icons }
        for (const f of unique) next[f] = ''
        icons = next
      })
  })

  const kindIcon: Record<string, string> = {
    mod: 'ti-package', setting: 'ti-adjustments', style: 'ti-palette', create: 'ti-sparkles', test: 'ti-player-play', other: 'ti-point',
  }
  const fmt = (ms: number) => new Date(ms).toLocaleTimeString()

  // Нова сесія → знову розгортаємо.
  let lastStart = 0
  $effect(() => {
    if (session && session.started !== lastStart) {
      lastStart = session.started
      minimized = false
    }
  })

  // Тривалість роботи агента (оновлюється раз на секунду, лише поки сесія активна).
  let nowMs = $state(Date.now())
  let timer: ReturnType<typeof setInterval> | null = null
  const elapsed = $derived.by(() => {
    if (!session) return ''
    const s = Math.max(0, Math.floor((nowMs - session.started) / 1000))
    return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`
  })

  onMount(() => {
    initAIEdit()
    place()
    ro = new ResizeObserver(place)
    const host = document.querySelector('.content')
    if (host) ro.observe(host)
    window.addEventListener('resize', place)
    timer = setInterval(() => { nowMs = Date.now(); place() }, 1000)
  })
  onDestroy(() => {
    ro?.disconnect()
    window.removeEventListener('resize', place)
    if (timer) clearInterval(timer)
  })

  async function stopAgent() {
    if (!(await confirmDialog(tr.stopTitle, tr.stopConfirm, { confirmLabel: tr.stop, cancelLabel: tr.cancel, danger: true }))) return
    try { await App.DisconnectAIAgent() } catch {}
  }
</script>

<span class="aiw-anchor" bind:this={anchor} aria-hidden="true"></span>

{#if show && session}
  {#if !minimized}
    <div class="aiw-layer" style="left:{box.left}px;top:{box.top}px;width:{box.width}px;height:{box.height}px"
         role="status" aria-live="polite" transition:fade={{ duration: 200 }}>
      <div class="aiw" in:fly={{ y: 14, duration: 280, easing: cubicOut }} out:fly={{ y: 10, duration: 180, easing: cubicOut }}>
        <div class="aiw-head">
          <span class="aiw-ico" class:busy={session.active}><i class="ti ti-robot"></i></span>
          <div class="aiw-title">
            <div class="aiw-h">{session.active ? tr.active : tr.done}</div>
            <div class="aiw-s">{session.client || 'ШІ'} · {tr.since} {elapsed}</div>
          </div>
          <button class="aiw-x" onclick={() => (minimized = true)} title={tr.minimize} aria-label={tr.minimize}><i class="ti ti-minus"></i></button>
        </div>

        {#if session.active}
          <div class="aiw-bar" class:run={!!session.current}></div>
          <div class="aiw-now">
            <span class="aiw-now-l">{tr.now}</span>
            <b>{session.current || tr.idle}</b>
          </div>
        {/if}

        <div class="aiw-stats">
          <span class="aiw-chip"><i class="ti ti-package"></i> {counts.mod} {tr.mods}</span>
          <span class="aiw-chip"><i class="ti ti-adjustments"></i> {counts.setting} {tr.settings}</span>
          {#if counts.other}<span class="aiw-chip"><i class="ti ti-point"></i> {counts.other} {tr.other}</span>{/if}
          {#if counts.err}<span class="aiw-chip err"><i class="ti ti-alert-triangle"></i> {counts.err}</span>{/if}
        </div>
        {#if lastAction}<div class="aiw-last"><span>{tr.last}</span> {lastAction}</div>{/if}

        <ul class="aiw-list">
          {#each session.changes.slice(0, 30) as c (c.time + c.summary)}
            <li class:err={!c.ok} animate:flip={{ duration: 220 }} in:fly={{ y: -8, duration: 240, easing: cubicOut }}>
              <span class="aiw-li-ic">
                {#if c.file && icons[c.file]}
                  <img src={icons[c.file]} alt="" />
                {:else}
                  <i class="ti {c.ok ? (kindIcon[c.kind] ?? 'ti-point') : 'ti-alert-triangle'}"></i>
                {/if}
              </span>
              <span class="aiw-li-t" title={clean(c.summary)}>{clean(c.summary)}</span>
              <span class="aiw-li-time">{fmt(c.time)}</span>
            </li>
          {:else}
            <li class="empty">{tr.empty}</li>
          {/each}
        </ul>

        <div class="aiw-foot">
          {#if session.active}
            <span class="aiw-lock"><i class="ti ti-lock"></i> {tr.locked}</span>
            <button class="aiw-btn" onclick={stopAgent}><i class="ti ti-plug-off"></i> {tr.stop}</button>
          {:else}
            <span></span>
            <button class="aiw-btn primary" onclick={() => aiEdit.dismiss(packId)}>{tr.close}</button>
          {/if}
        </div>
      </div>
    </div>
  {:else}
    <button class="aiw-mini" class:busy={session.active} style="left:{box.left + 20}px;top:{box.top + box.height - 56}px"
            onclick={() => (minimized = false)} title={tr.expand} transition:fly={{ y: 12, duration: 220, easing: cubicOut }}>
      <i class="ti ti-robot"></i>
      <span>{session.active ? (session.current || tr.active) : tr.done}</span>
      {#if session.active}<i class="ti ti-lock"></i>{/if}
    </button>
  {/if}
{/if}

<style>
  .aiw-anchor { display: none; }
  /* Накладка лише поверх області збірки; сайдбар і шапка не перекриваються. */
  .aiw-layer {
    position: fixed; z-index: 40; display: grid; place-items: center; padding: 24px; box-sizing: border-box;
    background: rgba(8, 8, 10, .5); backdrop-filter: blur(5px);
  }
  .aiw {
    width: min(540px, 100%); max-height: 100%; display: flex; flex-direction: column;
    background: var(--bg-card); border: 1px solid var(--border-hi); border-radius: var(--r-lg, 16px);
    box-shadow: var(--elev-4, 0 40px 90px -30px rgba(0, 0, 0, .7)); color: var(--text); overflow: hidden;
  }
  .aiw-head { display: flex; align-items: center; gap: 12px; padding: 16px 18px 12px; }
  .aiw-ico {
    flex: none; width: 38px; height: 38px; border-radius: 10px; display: grid; place-items: center; font-size: 20px;
    color: var(--orange); background: rgba(var(--orange-rgb), .14);
  }
  .aiw-ico.busy { animation: aiw-glow 1.6s ease-in-out infinite; }
  @keyframes aiw-glow { 50% { box-shadow: 0 0 0 6px rgba(var(--orange-rgb), .14); } }
  .aiw-title { flex: 1; min-width: 0; }
  .aiw-h { font-weight: 600; font-size: 15px; }
  .aiw-s { font-size: 12px; color: var(--text-mute); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; font-variant-numeric: tabular-nums; }
  .aiw-x {
    all: unset; cursor: pointer; width: 30px; height: 30px; border-radius: 8px; display: grid; place-items: center; color: var(--text-dim);
    transition: background .15s, color .15s;
  }
  .aiw-x:hover { background: var(--hover); color: var(--text); }
  .aiw-bar { height: 2px; background: var(--border); position: relative; overflow: hidden; }
  .aiw-bar.run::after {
    content: ''; position: absolute; inset: 0 auto 0 0; width: 35%; background: var(--accent-grad, var(--orange));
    animation: aiw-slide 1.3s ease-in-out infinite;
  }
  @keyframes aiw-slide { from { transform: translateX(-100%); } to { transform: translateX(300%); } }
  .aiw-now { display: flex; gap: 10px; align-items: baseline; padding: 12px 18px 0; font-size: 13px; }
  .aiw-now-l { color: var(--text-dim); font-size: 12px; }
  .aiw-now b { font-weight: 600; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .aiw-stats { display: flex; flex-wrap: wrap; gap: 6px; padding: 12px 18px 0; }
  .aiw-chip {
    display: inline-flex; align-items: center; gap: 5px; padding: 3px 9px; border-radius: 999px; font-size: 11.5px;
    background: var(--bg-input); border: 1px solid var(--border); color: var(--text-mute);
  }
  .aiw-chip.err { color: var(--red); border-color: rgba(239, 68, 68, .4); background: rgba(239, 68, 68, .1); }
  .aiw-last { padding: 8px 18px 0; font-size: 12px; color: var(--text-mute); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .aiw-last span { color: var(--text-dim); margin-right: 6px; }
  .aiw-list {
    list-style: none; margin: 10px 0 0; padding: 0 12px 4px; overflow-y: auto; min-height: 80px; max-height: 260px; flex: 1;
    display: flex; flex-direction: column; gap: 3px; scrollbar-width: thin; scrollbar-color: var(--border-hi) transparent;
  }
  .aiw-list li { display: flex; align-items: center; gap: 10px; padding: 6px 8px; border-radius: 10px; font-size: 12.5px; transition: background .15s; }
  .aiw-list li:hover { background: var(--hover); }
  .aiw-list li.empty { color: var(--text-dim); justify-content: center; }
  .aiw-list li.err .aiw-li-t { color: var(--red); }
  .aiw-li-ic {
    flex: none; width: 28px; height: 28px; border-radius: 8px; display: grid; place-items: center; overflow: hidden;
    background: var(--bg-input); color: var(--text-mute); font-size: 15px;
  }
  .aiw-li-ic img { width: 100%; height: 100%; object-fit: cover; }
  .aiw-list li.err .aiw-li-ic { color: var(--red); background: rgba(239, 68, 68, .15); }
  .aiw-li-t { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .aiw-li-time { flex: none; color: var(--text-dim); font-size: 11px; font-family: var(--font-mono); }
  .aiw-foot { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 12px 18px 16px; border-top: 1px solid var(--border); }
  .aiw-lock { font-size: 12px; color: var(--text-mute); display: flex; gap: 6px; align-items: center; min-width: 0; }
  .aiw-btn {
    all: unset; flex: none; cursor: pointer; display: inline-flex; align-items: center; gap: 6px; padding: 7px 12px; border-radius: 8px;
    background: var(--bg-input); border: 1px solid var(--border); color: var(--text-mute); font-size: 12.5px;
    transition: background .15s, color .15s, border-color .15s;
  }
  .aiw-btn:hover { color: var(--red); border-color: var(--red); background: var(--hover); }
  .aiw-btn.primary { color: var(--accent-ink, #1c0e00); background: var(--accent-grad, var(--orange)); border-color: transparent; font-weight: 600; }
  .aiw-btn.primary:hover { color: var(--accent-ink, #1c0e00); filter: brightness(1.08); }
  .aiw-btn:focus-visible, .aiw-x:focus-visible, .aiw-mini:focus-visible { outline: 2px solid rgba(var(--orange-rgb), .6); outline-offset: 2px; }
  .aiw-mini {
    all: unset; position: fixed; z-index: 40; cursor: pointer;
    display: inline-flex; align-items: center; gap: 8px; max-width: min(420px, 60vw); padding: 9px 14px; border-radius: 12px;
    background: var(--bg-card); border: 1px solid var(--border-hi); box-shadow: var(--elev-3); font-size: 12.5px; color: var(--text);
  }
  .aiw-mini.busy { border-color: rgba(var(--orange-rgb), .55); }
  .aiw-mini span { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .aiw-mini .ti-robot { color: var(--orange); }
  @media (prefers-reduced-motion: reduce) {
    .aiw-ico.busy, .aiw-bar.run::after { animation: none; }
  }
  .aiw-list::-webkit-scrollbar { width: 6px; }
  .aiw-list::-webkit-scrollbar-track { background: transparent; }
  .aiw-list::-webkit-scrollbar-thumb { background: var(--border-hi); border-radius: 3px; }
</style>
