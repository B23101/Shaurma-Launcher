<script lang="ts">
  // ── AIAgentIndicator: плашка «ШІ-агент керує лаунчером» ───────────────────
  // Показується, поки зовнішній ШІ підключений (MCP, backend/ai_agent_api.go):
  // що він робить зараз, журнал останніх дій і кнопка негайного відключення.
  // Стиль — як у Toasts (токени --bg-card/--border-hi/--orange, blur, elev-3).

  import { onMount } from 'svelte'
  import { fly, slide } from 'svelte/transition'
  import { flip } from 'svelte/animate'
  import { cubicOut } from 'svelte/easing'
  import { getLanguage } from './i18n.svelte'
  import { App, on } from './wails'
  import { confirmDialog } from './toast.svelte'

  type Action = { tool: string; label: string; target?: string; icon?: string; write: boolean; started: number; millis: number; done: boolean; ok: boolean; error?: string }
  type Activity = { running: boolean; connected: boolean; client: string; lastSeen: number; current?: Action; recent: Action[]; show?: boolean }

  const L = {
    uk: {
      agent: 'ШІ-агент', idle: 'підключений, очікує', open: 'Журнал дій', stop: 'Відключити',
      stopTitle: 'Відключити ШІ-агента?', cancel: 'Скасувати',
      stopConfirm: 'Агент буде відключений: його запити відхилятимуться 10 хвилин (або доки ви не натиснете «Дозволити знову» в Налаштуваннях → ШІ-агент). Сам MCP-сервер лишиться ввімкненим.',
      empty: 'Дій ще не було', changes: 'змінює', reads: 'читає', failed: 'помилка',
    },
    en: {
      agent: 'AI agent', idle: 'connected, idle', open: 'Activity log', stop: 'Disconnect',
      stopTitle: 'Disconnect the AI agent?', cancel: 'Cancel',
      stopConfirm: 'The agent will be disconnected: its requests are refused for 10 minutes (or until you press “Allow again” in Settings → AI agent). The MCP server itself stays on.',
      empty: 'No actions yet', changes: 'changes', reads: 'reads', failed: 'failed',
    },
  }
  const tr = $derived(getLanguage() === 'en' ? L.en : L.uk)

  let act = $state<Activity | null>(null)
  let expanded = $state(false)

  const visible = $derived(!!act && act.show !== false && act.running && (act.connected || !!act.current))
  const rows = $derived((act?.recent ?? []).slice(0, 12))

  // Події приходять пачками (кілька викликів за мс) — зливаємо в один кадр,
  // щоб плашка не смикалась на кожну подію.
  let pending: Activity | null = null
  let raf = 0
  function push(a: Activity) {
    pending = a
    if (raf) return
    raf = requestAnimationFrame(() => { raf = 0; if (pending) act = pending; pending = null })
  }

  onMount(() => {
    App.GetAIAgentActivity().then((a: Activity) => (act = a)).catch(() => {})
    const off = on('aiagent:activity', push)
    const t = setInterval(() => {
      if (act && act.connected && Date.now() - act.lastSeen > 120000) act = { ...act, connected: false }
    }, 10000)
    return () => { off(); clearInterval(t); if (raf) cancelAnimationFrame(raf) }
  })

  const fmt = (ms: number) => new Date(ms).toLocaleTimeString()
  async function stop() {
    if (!(await confirmDialog(tr.stopTitle, tr.stopConfirm, { confirmLabel: tr.stop, cancelLabel: tr.cancel, danger: true }))) return
    try { await App.DisconnectAIAgent() } catch {}
    act = act ? { ...act, connected: false, current: undefined } : act
  }
</script>

{#if visible && act}
  <div class="aia" class:busy={!!act.current} role="status" aria-live="polite"
    in:fly={{ y: 16, duration: 320, easing: cubicOut }} out:fly={{ y: 16, duration: 220, easing: cubicOut }}>
    <div class="aia-top">
    <button class="aia-head" onclick={() => (expanded = !expanded)} aria-expanded={expanded} title={tr.open}>
      <span class="aia-ico"><i class="ti ti-robot"></i><span class="aia-dot"></span></span>
      <span class="aia-txt">
        <span class="aia-name">{tr.agent}{act.client ? ` · ${act.client}` : ''}</span>
        <span class="aia-sub">
          {#if act.current}
            <i class="ti {act.current.icon || 'ti-tool'} aia-ai"></i>
            <b>{act.current.label}</b>{#if act.current.target}<span class="aia-tg"> · {act.current.target}</span>{/if}
          {:else}
            {tr.idle}
          {/if}
        </span>
      </span>
      <i class="ti ti-chevron-up aia-chev" class:open={expanded}></i>
    </button>
    <button class="aia-off" onclick={stop} title={tr.stop} aria-label={tr.stop}><i class="ti ti-power"></i></button>
    </div>

    {#if expanded}
      <div class="aia-body" transition:slide={{ duration: 260, easing: cubicOut }}>
        {#if rows.length === 0}
          <div class="aia-empty">{tr.empty}</div>
        {:else}
          <ul class="aia-list">
            {#each rows as r (r.started + r.tool + r.label)}
              <li class:err={!r.ok} class:w={r.write} animate:flip={{ duration: 220 }} in:fly={{ y: -6, duration: 220 }}>
                <span class="t">{fmt(r.started)}</span>
                <i class="ti {r.icon || 'ti-tool'} ri"></i>
                <span class="n" title={r.error || (r.label + (r.target ? ' · ' + r.target : ''))}><b>{r.label}</b>{#if r.target}<span class="tg"> · {r.target}</span>{/if}</span>
                {#if !r.ok}<span class="e">{tr.failed}</span>{/if}
                <span class="d">{r.millis} мс</span>
              </li>
            {/each}
          </ul>
        {/if}
        <button class="aia-stop" onclick={stop}><i class="ti ti-plug-off"></i> {tr.stop}</button>
      </div>
    {/if}
  </div>
{/if}

<style>
  .aia {
    position: fixed; right: 16px; bottom: 16px; z-index: 9000;
    width: min(380px, calc(100vw - 32px)); /* фіксована ширина — текст не розтягує плашку */
    border-radius: 12px; overflow: hidden;
    background: var(--bg-card); border: 1px solid var(--border-hi);
    box-shadow: var(--elev-3, 0 18px 40px rgba(0, 0, 0, .6));
    backdrop-filter: blur(10px);
    color: var(--text); font-size: 12.5px;
    transition: border-color .3s var(--ease-out, ease), box-shadow .3s var(--ease-out, ease);
  }
  .aia.busy { border-color: rgba(var(--orange-rgb), .55); }
  .aia-top { display: flex; align-items: stretch; }
  .aia-off {
    all: unset; box-sizing: border-box; flex: none; width: 44px; cursor: pointer; display: grid; place-items: center;
    color: var(--text-dim); font-size: 17px; border-left: 1px solid var(--border); transition: color .15s, background .15s;
  }
  .aia-off:hover { color: var(--red); background: var(--hover); }
  .aia-off:focus-visible { outline: 2px solid rgba(var(--orange-rgb), .6); outline-offset: -2px; }
  .aia-head {
    all: unset; box-sizing: border-box; flex: 1; min-width: 0; cursor: pointer;
    display: flex; align-items: center; gap: 10px; padding: 10px 12px;
    transition: background .15s;
  }
  .aia-head:hover { background: var(--hover); }
  .aia-head:focus-visible { outline: 2px solid rgba(var(--orange-rgb), .6); outline-offset: -2px; }
  .aia-ico {
    position: relative; flex: none; width: 30px; height: 30px; border-radius: 8px;
    display: grid; place-items: center; font-size: 16px;
    color: var(--orange); background: rgba(var(--orange-rgb), .14);
  }
  .aia-dot {
    position: absolute; right: -2px; top: -2px; width: 9px; height: 9px; border-radius: 50%;
    background: var(--green); border: 2px solid var(--bg-card);
    transition: background .3s;
  }
  .aia.busy .aia-dot { background: var(--orange); animation: aia-pulse 1.1s ease-in-out infinite; }
  @keyframes aia-pulse { 50% { transform: scale(.7); opacity: .55; } }
  .aia-txt { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 1px; }
  .aia-name { font-weight: 600; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .aia-sub {
    color: var(--text-mute); white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
    font-family: var(--font-mono); font-size: 11.5px;
  }
  .aia-mode {
    display: inline-block; margin-right: 4px; padding: 0 6px; border-radius: 6px; font-family: var(--font-ui); font-size: 10.5px;
    color: var(--text-mute); background: var(--bg-input);
  }
  .aia-mode.w { color: var(--yellow); background: rgba(234, 179, 8, .15); }
  .aia-chev { flex: none; color: var(--text-dim); transition: transform .28s var(--ease-out, ease); }
  .aia-chev.open { transform: rotate(180deg); }
  .aia-body { padding: 0 12px 12px; display: flex; flex-direction: column; gap: 8px; border-top: 1px solid var(--border); }
  .aia-list {
    list-style: none; margin: 8px 0 0; padding: 0; max-height: 220px; overflow-y: auto;
    display: flex; flex-direction: column; gap: 3px; scrollbar-width: thin; scrollbar-color: var(--border-hi) transparent;
  }
  .aia-list::-webkit-scrollbar { width: 6px; }
  .aia-list::-webkit-scrollbar-track { background: transparent; }
  .aia-list::-webkit-scrollbar-thumb { background: var(--border-hi); border-radius: 3px; }
  .aia-list li { display: flex; gap: 8px; align-items: baseline; font-family: var(--font-mono); font-size: 11.5px; }
  .aia-list li.w .n { color: var(--yellow); }
  .aia-list li.err .n { color: var(--red); }
  .aia-list .ri { flex: none; align-self: center; font-size: 14px; color: var(--text-mute); }
  .aia-list li.w .ri { color: var(--yellow); }
  .aia-list li.err .ri { color: var(--red); }
  .aia-list .n b { font-weight: 600; font-family: var(--font-ui); }
  .aia-list .tg, .aia-tg { color: var(--text-mute); font-family: var(--font-ui); }
  .aia-ai { margin-right: 4px; vertical-align: -1px; color: var(--orange); }
  .aia-sub b { font-weight: 600; color: var(--text); font-family: var(--font-ui); }
  .aia-list .t, .aia-list .d { flex: none; color: var(--text-dim); }
  .aia-list .n { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .aia-list .e { color: var(--red); }
  .aia-empty { margin-top: 8px; color: var(--text-dim); }
  .aia-stop {
    all: unset; box-sizing: border-box; cursor: pointer; align-self: flex-start;
    display: inline-flex; align-items: center; gap: 6px; padding: 6px 10px; border-radius: 8px;
    background: var(--bg-input); border: 1px solid var(--border); color: var(--text-mute);
    transition: background .15s, color .15s, border-color .15s;
  }
  .aia-stop:hover { color: var(--red); border-color: var(--red); background: var(--hover); }
  .aia-stop:focus-visible { outline: 2px solid rgba(var(--orange-rgb), .6); outline-offset: 2px; }
  @media (prefers-reduced-motion: reduce) {
    .aia, .aia-chev, .aia-dot { transition: none; animation: none; }
    .aia.busy .aia-dot { animation: none; }
  }
</style>
