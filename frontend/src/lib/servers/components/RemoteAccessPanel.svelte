<script lang="ts">
  // ── Доступ друзів до цього сервера (віддалене керування) ───────────────
  // Власник тут вирішує, що саме може кожен друг: запуск, зупинка, перегляд консолі, команди.
  // Перевіряє ВСЕ це VPS (дружба + права) і ще раз сам лаунчер: без підпису VPS команда не виконується.
  import { App, on } from '../../wails'
  import { toast } from '../../toast.svelte'
  const toastErr = (m: string) => toast(m, 'error')
  const toastInfo = (m: string) => toast(m, 'info')
  import type { RemoteAccessView, RemoteServerAccess, RemoteAuditEntry, SocialFriend } from '../../types'
  import FriendAvatar from '../../friends/FriendAvatar.svelte'
  import { REMOTE_CODES } from '../remoteServers.svelte'

  // isProxy — проксі Velocity: можна дати другу запуск/зупинку й серверів його мережі.
  let { serverId = '', isProxy = false }: { serverId?: string; isProxy?: boolean } = $props()

  let view = $state<RemoteAccessView>({ paused: false, servers: [] })
  let friends = $state<SocialFriend[]>([])
  let audit = $state<RemoteAuditEntry[]>([])
  let busy = $state(false)
  let showAudit = $state(false)
  let keyWarn = $state('')

  const acc = $derived<RemoteServerAccess>(
    view.servers.find(s => s.serverId === serverId) ?? { serverId, allowStopWithPlayers: false, allowPrivileged: false, grants: [] },
  )
  const grantOf = (id: string) => acc.grants.find(g => g.accountId === id)

  async function load() {
    try {
      const [v, st] = await Promise.all([App.GetRemoteAccess(), App.GetFriendsState()])
      view = v
      friends = st.friends ?? []
      if (showAudit) audit = await App.GetRemoteAudit(serverId, 30)
    } catch (e: any) { /* соціальна служба ще не запущена */ }
  }

  $effect(() => { void serverId; void load() })
  $effect(() => {
    const off1 = on('remote:key_changed', (d: any) => { keyWarn = d?.message ?? 'Ключ сервера Шаурми змінився — перевірте з’єднання' })
    const off2 = on('friends:update', () => { void load() })
    return () => { off1(); off2() }
  })

  async function setGrant(friendId: string, patch: Partial<{ start: boolean; stop: boolean; console: boolean; exec: boolean; network: boolean }>) {
    const g = grantOf(friendId)
    const next = { start: !!g?.start, stop: !!g?.stop, console: !!g?.console, exec: !!g?.exec, network: !!g?.network, ...patch }
    if (!next.console) next.exec = false // команди «наосліп» заборонені
    if (!next.start && !next.stop) next.network = false // мережа — це запуск/зупинка її серверів
    busy = true
    try {
      await App.SetRemoteGrant(serverId, friendId, next.start, next.stop, next.console, next.exec, next.network)
      await load()
    } catch (e: any) { toastErr(String(e?.message ?? e)) } finally { busy = false }
  }

  async function flag(kind: 'players' | 'priv', v: boolean) {
    try {
      if (kind === 'players') await App.SetRemoteAllowStopWithPlayers(serverId, v)
      else await App.SetRemoteAllowPrivileged(serverId, v)
      await load()
    } catch (e: any) { toastErr(String(e?.message ?? e)) }
  }

  async function togglePause() {
    try { await App.SetRemotePaused(!view.paused); await load() } catch (e: any) { toastErr(String(e?.message ?? e)) }
  }

  async function toggleAudit() {
    showAudit = !showAudit
    if (showAudit) audit = await App.GetRemoteAudit(serverId, 30).catch(() => [])
  }

  const fmt = (ts: number) => new Date(ts).toLocaleString()

  const BASE_PERMS = [
    { k: 'start', icon: 'ti-player-play', label: 'Запуск' },
    { k: 'stop', icon: 'ti-player-stop', label: 'Зупинка' },
    { k: 'console', icon: 'ti-eye', label: 'Консоль' },
    { k: 'exec', icon: 'ti-terminal-2', label: 'Команди' },
  ] as const
  const NETWORK_PERM = { k: 'network', icon: 'ti-topology-star-3', label: 'Сервери мережі' } as const
  const PERMS = $derived(isProxy ? [...BASE_PERMS, NETWORK_PERM] : [...BASE_PERMS])
  const withAccess = $derived(acc.grants.filter(g => g.start || g.stop || g.console || g.exec).length)
  const OPS: Record<string, string> = { start: 'Запуск', stop: 'Зупинка', console_open: 'Відкрив консоль', console_exec: 'Команда', console_close: 'Закрив консоль' }
</script>

<section class="rap">
  <header class="rap-hero" data-skey="pause">
    <div class="rap-ic"><i class="ti ti-users-group"></i></div>
    <div class="rap-ht">
      <div class="rap-t">Доступ друзів</div>
      <div class="rap-s">{#if friends.length === 0}Додайте друзів, щоб дати їм доступ{:else}Доступ мають {withAccess} із {friends.length}{/if}</div>
    </div>
    <button class="btn {view.paused ? 'btn-primary' : 'btn-ghost'}" onclick={togglePause}>
      <i class="ti" class:ti-player-pause={!view.paused} class:ti-player-play={view.paused}></i>
      {view.paused ? 'Відновити доступ' : 'Призупинити весь доступ'}
    </button>
  </header>

  {#if keyWarn}
    <div class="rap-banner warn"><i class="ti ti-alert-triangle"></i><span>{keyWarn}</span></div>
  {/if}
  {#if view.paused}
    <div class="rap-banner warn"><i class="ti ti-player-pause"></i><span>Доступ призупинено: друзі нічого не можуть робити з вашими серверами.</span></div>
  {/if}

  <div class="rap-banner info">
    <i class="ti ti-shield-lock"></i>
    <span>Друг керує сервером, лише поки ваш лаунчер у мережі. Кожну дію перевіряє сервер Шаурми і ваш лаунчер.</span>
  </div>

  {#if friends.length === 0}
    <div class="rap-empty"><i class="ti ti-user-plus"></i> У вас ще немає друзів у Shaurma.</div>
  {:else}
    <div class="rap-h"><i class="ti ti-users"></i> Друзі</div>
    <div class="rap-list" data-skey="friends">
      {#each friends as f (f.accountId)}
        {@const g = grantOf(f.accountId)}
        {@const has = !!(g?.start || g?.stop || g?.console || g?.exec)}
        <div class="rap-f" class:has>
          <div class="rap-who">
            <FriendAvatar nickname={f.nickname} color={f.color ?? ''} status={f.presence?.status} size={38} />
            <div class="rap-id">
              <div class="rap-nick" title={f.nickname}>{f.nickname}</div>
              <div class="rap-sub">{has ? 'Має доступ' : 'Без доступу'}</div>
            </div>
          </div>
          <div class="rap-perms">
            {#each PERMS as p (p.k)}
              {@const on = !!g?.[p.k]}
              {@const dis = busy || (p.k === 'exec' && !g?.console) || (p.k === 'network' && !g?.start && !g?.stop)}
              <button class="rap-perm" class:on aria-pressed={on} disabled={dis}
                      title={p.k === 'exec' && !g?.console ? 'Спершу дозвольте перегляд консолі'
                        : p.k === 'network' ? 'Запуск і зупинка (як позначено ліворуч) діють і на кожен сервер мережі проксі. Консоль серверів мережі — окремо на кожному сервері.' : ''}
                      onclick={() => setGrant(f.accountId, { [p.k]: !on })}>
                <i class="ti {p.icon}"></i> {p.label}
              </button>
            {/each}
          </div>
        </div>
      {/each}
    </div>

    <div class="rap-h"><i class="ti ti-adjustments"></i> Додаткові дозволи</div>
    <div class="rap-opts">
      <div class="rap-opt" data-skey="stopplayers">
        <div class="rap-ot"><b>Зупинка, коли на сервері є гравці</b><span>Інакше зупинку заблоковано, поки хтось онлайн.</span></div>
        <button class="toggle" class:on={acc.allowStopWithPlayers} role="switch" aria-checked={acc.allowStopWithPlayers} aria-label="Дозволити зупинку з гравцями" onclick={() => flag('players', !acc.allowStopWithPlayers)}></button>
      </div>
      <div class="rap-opt" data-skey="privileged">
        <div class="rap-ot"><b>Привілейовані команди</b><span><code>op</code>, <code>deop</code>, <code>whitelist</code>, <code>save-off</code>, плагіни прав (<code>lp</code>, <code>pex</code>) і <code>sudo</code> — також усередині <code>execute … run</code>. Небезпечні, вмикайте лише для довірених.</span></div>
        <button class="toggle" class:on={acc.allowPrivileged} role="switch" aria-checked={acc.allowPrivileged} aria-label="Дозволити привілейовані команди" onclick={() => flag('priv', !acc.allowPrivileged)}></button>
      </div>
    </div>
  {/if}

  <button class="rap-audit-h" class:open={showAudit} onclick={toggleAudit} data-skey="audit">
    <span><i class="ti ti-history"></i> Журнал дій друзів</span>
    <i class="ti ti-chevron-down rap-chev"></i>
  </button>
  {#if showAudit}
    <div class="rap-audit">
      <!-- Ключ — індекс: кілька спроб в одну мілісекунду (напр. відмови rate_limited) давали
           однаковий ключ ts+op+friend, і Svelte падав на дублі ключа. -->
      {#each audit as a, i (i)}
        <div class="rap-a">
          <span class="rap-a-t">{fmt(a.ts)}</span>
          <b>{a.nick || a.friend.slice(0, 8)}</b>
          <span>{OPS[a.op] ?? a.op}</span>
          <span class="rap-a-r" class:bad={a.result !== 'ok' && a.result !== 'accepted'} title={a.result}>{REMOTE_CODES[a.result] ?? a.result}</span>
          {#if a.cmd}<code>{a.cmd}</code>{/if}
        </div>
      {:else}
        <div class="rap-empty sm">Журнал порожній.</div>
      {/each}
    </div>
  {/if}
</section>

<style>
  .rap { display: flex; flex-direction: column; gap: 14px; --pcol: var(--orange); }

  .rap-hero { display: flex; align-items: center; gap: 14px; flex-wrap: wrap; }
  .rap-ic {
    width: 42px; height: 42px; border-radius: 13px; display: grid; place-items: center; font-size: 21px; color: #fff; flex-shrink: 0;
    background: linear-gradient(135deg, var(--orange), color-mix(in srgb, var(--orange) 60%, #ff3d00));
    box-shadow: 0 8px 18px -8px rgba(var(--orange-rgb), .7);
  }
  .rap-ht { flex: 1; min-width: 180px; }
  .rap-t { font: 800 15px/1.2 var(--font-ui); color: var(--text); }
  .rap-s { margin-top: 3px; font: 500 12px/1.3 var(--font-ui); color: var(--text-mute); }

  .rap-banner {
    display: flex; align-items: flex-start; gap: 10px; padding: 11px 13px; border-radius: 13px;
    font: 500 12px/1.5 var(--font-ui); color: var(--text-mute); background: var(--bg-input); border: 1px solid var(--border);
  }
  .rap-banner i { font-size: 17px; margin-top: 1px; flex-shrink: 0; color: var(--violet-l, var(--orange)); }
  .rap-banner.warn { color: var(--text); background: rgba(245, 158, 11, .08); border-color: rgba(245, 158, 11, .35); }
  .rap-banner.warn i { color: var(--yellow, #f59e0b); }

  .rap-h {
    display: flex; align-items: center; gap: 8px; margin-top: 4px;
    font: 700 11px/1 var(--font-ui); letter-spacing: .1em; text-transform: uppercase; color: var(--text-mute);
  }
  .rap-h i { font-size: 15px; color: var(--orange); }

  .rap-list { display: flex; flex-direction: column; gap: 10px; }
  .rap-f {
    display: flex; align-items: center; justify-content: space-between; gap: 14px; flex-wrap: wrap;
    padding: 12px 14px; border-radius: 14px; background: var(--bg-input); border: 1px solid var(--border);
    transition: border-color .15s, background .15s;
  }
  .rap-f.has { border-color: color-mix(in srgb, var(--orange) 40%, var(--border)); background: color-mix(in srgb, var(--orange) 5%, var(--bg-input)); }
  .rap-who { display: flex; align-items: center; gap: 11px; min-width: 150px; flex: 1; }
  .rap-id { min-width: 0; }
  .rap-nick { font: 700 13.5px/1.2 var(--font-ui); color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .rap-sub { margin-top: 3px; font: 500 11px/1 var(--font-ui); color: var(--text-dim); }
  .rap-f.has .rap-sub { color: var(--orange); }

  .rap-perms { display: flex; gap: 7px; flex-wrap: wrap; }
  .rap-perm {
    display: inline-flex; align-items: center; gap: 6px; padding: 7px 11px; border-radius: 999px; cursor: pointer;
    font: 650 11.5px/1 var(--font-ui); color: var(--text-mute); background: transparent; border: 1px solid var(--border);
    transition: color .15s, background .15s, border-color .15s, transform .15s;
  }
  .rap-perm i { font-size: 14px; }
  .rap-perm:hover:not(:disabled) { color: var(--text); border-color: var(--border-hi); background: var(--hover); }
  .rap-perm.on { color: var(--orange); background: rgba(var(--orange-rgb), .12); border-color: rgba(var(--orange-rgb), .45); }
  .rap-perm:active:not(:disabled) { transform: scale(.96); }
  .rap-perm:disabled { opacity: .4; cursor: default; }

  .rap-opts { display: flex; flex-direction: column; gap: 10px; }
  .rap-opt {
    display: flex; align-items: center; justify-content: space-between; gap: 16px;
    padding: 13px 14px; border-radius: 14px; background: var(--bg-input); border: 1px solid var(--border);
  }
  .rap-ot { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
  .rap-ot b { font: 700 13px/1.2 var(--font-ui); color: var(--text); }
  .rap-ot span { font: 500 11.5px/1.45 var(--font-ui); color: var(--text-mute); }
  .rap-ot code, .rap-a code { font-family: var(--font-mono); font-size: 11px; padding: 1px 5px; border-radius: 5px; background: var(--hover); color: var(--text); }
  .rap-opt .toggle { appearance: none; padding: 0; flex-shrink: 0; }

  .rap-empty { display: flex; align-items: center; gap: 8px; padding: 18px; border-radius: 14px; border: 1px dashed var(--border-hi, var(--border)); color: var(--text-mute); font: 500 12.5px/1.4 var(--font-ui); justify-content: center; }
  .rap-empty i { font-size: 18px; color: var(--orange); }
  .rap-empty.sm { padding: 12px; border: 0; }

  .rap-audit-h {
    display: flex; align-items: center; justify-content: space-between; gap: 8px; width: 100%; cursor: pointer;
    padding: 11px 14px; border-radius: 13px; background: var(--bg-input); border: 1px solid var(--border);
    font: 700 12px/1 var(--font-ui); color: var(--text); transition: border-color .15s, background .15s;
  }
  .rap-audit-h span { display: inline-flex; align-items: center; gap: 8px; }
  .rap-audit-h span i { font-size: 16px; color: var(--orange); }
  .rap-audit-h:hover { border-color: var(--border-hi); background: var(--hover); }
  .rap-chev { color: var(--text-mute); transition: transform .2s; }
  .rap-audit-h.open .rap-chev { transform: rotate(180deg); }

  .rap-audit { display: flex; flex-direction: column; border-radius: 13px; border: 1px solid var(--border); background: var(--bg-input); overflow: hidden; max-height: 320px; overflow-y: auto; }
  .rap-a { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; padding: 9px 13px; font: 500 12px/1.3 var(--font-ui); color: var(--text-mute); }
  .rap-a + .rap-a { border-top: 1px solid var(--border); }
  .rap-a b { color: var(--text); font-weight: 700; }
  .rap-a-t { font-family: var(--font-mono); font-size: 11px; color: var(--text-dim); }
  .rap-a-r { margin-left: auto; padding: 2px 8px; border-radius: 999px; font: 700 var(--fs-min)/1.2 var(--font-ui); color: var(--green); background: rgba(34, 197, 94, .12); }
  .rap-a-r.bad { color: var(--red); background: rgba(239, 68, 68, .12); }
</style>
