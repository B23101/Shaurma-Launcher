<script lang="ts">
  // Плаваючий віджет активних передач (знизу по центру). Показується з будь-якої сторінки:
  // вхідні — прогрес, пауза/продовжити/зупинити; вихідні — підготовка, стан кожного друга, закрити.
  import { onMount } from 'svelte'
  import { App } from './wails'
  import { toast } from './toast.svelte'
  import { shareIn, shareOut } from './friends/shareStore.svelte'
  import { fmtBytes, fmtEta } from './friends/shareMsg'

  let names = $state<Record<string, string>>({})
  onMount(async () => {
    try {
      const st = await App.GetFriendsState()
      for (const f of st?.friends ?? []) names[f.accountId] = f.nickname
    } catch {}
  })
  const nameOf = (id: string) => names[id] ?? 'друг'

  const ins = $derived(Object.values(shareIn).filter(v => ['connecting', 'downloading', 'paused', 'installing'].includes(v.phase)))
  const outs = $derived(Object.values(shareOut).filter(v => v.phase === 'preparing' || v.phase === 'ready'))
  const visible = $derived(ins.length + outs.length > 0)

  const pctIn = (p: { bytesDone: number; bytesTotal: number }) => p.bytesTotal > 0 ? Math.min(100, Math.floor(p.bytesDone * 100 / p.bytesTotal)) : 0
  const err = (e: any) => toast(String(e?.message ?? e), 'error')
</script>

{#if visible}
  <div class="tw" role="region" aria-label="Передачі">
    {#each ins as v (v.id)}
      <div class="tw-item">
        <div class="tw-top"><i class="ti ti-download"></i><b title={v.name}>{v.name}</b><span class="tw-from">від {v.from}</span></div>
        {#if v.phase === 'installing'}
          <div class="tw-sub">Встановлення…</div>
        {:else if v.phase === 'connecting'}
          <div class="tw-sub">Підключення…</div>
        {:else}
          <div class="tw-bar"><div class="tw-fill" class:paused={v.phase === 'paused'} style="width:{pctIn(v.progress)}%"></div></div>
          <div class="tw-sub">
            {pctIn(v.progress)}% · {fmtBytes(v.progress.bytesDone)} / {fmtBytes(v.progress.bytesTotal)}
            {#if v.phase === 'paused'} · пауза{v.progress.pausedBy === 'sender' ? ' (відправник)' : ''}
            {:else} · {fmtBytes(v.progress.speedBps)}/с {fmtEta(v.progress.bytesTotal - v.progress.bytesDone, v.progress.speedBps)}{/if}
          </div>
        {/if}
        <div class="tw-acts">
          {#if v.phase === 'paused' && v.progress.pausedBy !== 'sender'}
            <button aria-label="Продовжити" onclick={() => App.ShareResume(v.id).catch(err)}><i class="ti ti-player-play"></i></button>
          {:else if v.phase === 'downloading'}
            <button aria-label="Пауза" onclick={() => App.SharePause(v.id).catch(err)}><i class="ti ti-player-pause"></i></button>
          {/if}
          {#if v.phase !== 'installing'}<button aria-label="Зупинити" onclick={() => App.ShareCancelIn(v.id).catch(err)}><i class="ti ti-x"></i></button>{/if}
        </div>
      </div>
    {/each}

    {#each outs as o (o.id)}
      <div class="tw-item">
        <div class="tw-top"><i class="ti ti-upload"></i><b title={o.name}>{o.name}</b></div>
        {#if o.phase === 'preparing'}
          <div class="tw-bar"><div class="tw-fill" style="width:{o.hashTotal ? Math.floor(o.hashed * 100 / o.hashTotal) : 0}%"></div></div>
          <div class="tw-sub">Готую файли: {o.hashed}/{o.hashTotal}</div>
        {:else}
          {#each (o.recipients ?? []) as r (r.id)}
            <div class="tw-sub tw-rec">
              <span>{nameOf(r.id)}</span>
              <span>
                {#if r.state === 'waiting'}чекає на прийняття
                {:else if r.state === 'transferring'}{o.totalBytes ? Math.floor(r.bytesDone * 100 / o.totalBytes) : 0}% · {fmtBytes(r.bytesDone)}
                {:else if r.state === 'paused'}пауза ({r.pausedBy === 'sender' ? 'ви' : 'друг'})
                {:else if r.state === 'done'}готово
                {:else if r.state === 'declined'}відмовився
                {:else if r.state === 'revoked'}скасовано
                {:else}помилка{/if}
              </span>
              {#if r.state === 'transferring' || (r.state === 'paused' && r.pausedBy === 'sender')}
                <button aria-label="Пауза / продовжити" onclick={() => App.ShareSenderPause(o.id, r.id, r.state === 'transferring').catch(err)}>
                  <i class="ti {r.state === 'transferring' ? 'ti-player-pause' : 'ti-player-play'}"></i>
                </button>
              {/if}
            </div>
          {/each}
          {#if !o.recipients?.length}<div class="tw-sub">Доступ відкрито — чекаємо друзів</div>{/if}
          {#if o.error}<div class="tw-sub" style="color:var(--red)">{o.error}</div>{/if}
          {#if o.skipped?.length}<div class="tw-sub">Пропущено небезпечних файлів: {o.skipped.length}</div>{/if}
        {/if}
        <div class="tw-acts"><button onclick={() => App.ShareStop(o.id).catch(err)} title="Закрити передачу"><i class="ti ti-x"></i></button></div>
      </div>
    {/each}
  </div>
{/if}

<style>
  .tw { position: fixed; left: 50%; bottom: 18px; transform: translateX(-50%); z-index: 2000000; display: flex; flex-direction: column;
    gap: 8px; width: min(440px, calc(100vw - 32px)); font-family: var(--font-ui); }
  .tw-item { position: relative; background: var(--bg-panel); border: 1px solid var(--border-hi); border-radius: 12px; padding: 10px 40px 10px 12px;
    box-shadow: 0 10px 30px rgba(0,0,0,.45); display: flex; flex-direction: column; gap: 6px; }
  .tw-top { display: flex; gap: 8px; align-items: center; color: var(--text); min-width: 0; }
  .tw-top b { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .tw-from { color: var(--text-mute); font-size: 11px; margin-left: auto; white-space: nowrap; }
  .tw-bar { height: 6px; border-radius: 99px; background: var(--bg-input); overflow: hidden; }
  .tw-fill { height: 100%; background: var(--orange); transition: width .3s; }
  .tw-fill.paused { background: var(--text-mute); }
  .tw-sub { font-size: 11px; color: var(--text-dim); }
  .tw-rec { display: flex; gap: 8px; align-items: center; justify-content: space-between; }
  .tw-acts { position: absolute; right: 8px; top: 8px; display: flex; flex-direction: column; gap: 4px; }
  .tw button { border: 0; background: var(--bg-input); color: var(--text); width: 24px; height: 24px; border-radius: 7px; cursor: pointer; }
  .tw button:hover { background: var(--hover-strong); }
</style>
