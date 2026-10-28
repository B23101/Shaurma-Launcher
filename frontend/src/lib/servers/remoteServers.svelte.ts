// ── Сервери друзів (стан + дії) ─────────────────────────────────────────
// Друг дав мені доступ до свого сервера → він з'являється в «Серверах» як звичайна картка
// (ServerCard у режимі remote). Запуск/зупинка/консоль ідуть через VPS (перевіряє дружбу й права)
// або напряму зашифрованим каналом. Тут лише спільний стан і виклики бекенда; вигляд — у ServerCard
// та ServerInstancePage (сервер друга — та сама сторінка сервера з дозволеними вкладками).
import { App, on } from '../wails'
import { toast } from '../toast.svelte'
import type { RemoteServer, RemoteResultEvent, RemoteConsoleEvent, SocialFriend, ServerView } from '../types'

export const REMOTE_CODES: Record<string, string> = {
  not_allowed: 'Немає права на цю дію', not_friend: 'Ви більше не друзі', disabled: 'Власник призупинив доступ',
  rate_limited: 'Занадто часто — зачекайте', cooldown: 'Занадто часто — зачекайте', unavailable: 'Лаунчер друга офлайн',
  players_online: 'На сервері є гравці — зупинку заблоковано', busy: 'Сервер зайнятий', no_session: 'Сесію консолі закрито',
  bad_command: 'Некоректна команда', privileged: 'Ця команда заборонена власником', start_failed: 'Не вдалося запустити',
  stop_failed: 'Не вдалося зупинити', exec_failed: 'Не вдалося виконати команду', session_limit: 'Забагато відкритих консолей',
  already_running: 'Сервер уже працює', already_stopped: 'Сервер уже зупинено',
  not_running: 'Спершу запустіть сервер', p2p_failed: 'Не вдалося змінити P2P-адресу',
  accepted: 'Команду прийнято — чекаємо на сервер',
  // Лише в журналі власника (у відповідях другові їх не буває):
  ok: 'Виконано', duplicate: 'Повторна сесія', mandate_invalid: 'Без підпису VPS — відхилено',
  bad_envelope: 'Пошкоджена команда — відхилено', envelope_mismatch: 'Підроблена команда — відхилено',
  no_vps_key: 'Ключ VPS не підтверджено — відхилено',
}
const REASONS: Record<string, string> = {
  revoked: 'Власник зняв право на консоль', owner_offline: 'Лаунчер друга вийшов з мережі', friend_gone: 'Дружбу розірвано',
  expired: 'Сесія завершилась за часом', closed: 'Сесію закрито',
}

export interface RemoteConsoleLine { i: number; t: string; l: string; x: string }

// Один спільний об'єкт: властивості реактивні, самий об'єкт не перепризначаємо.
export const remote = $state({
  servers: [] as RemoteServer[],
  nicks: {} as Record<string, string>,
  pending: {} as Record<string, string>, // `${ownerId}|${serverId}` → reqId
  consoleFor: null as RemoteServer | null,
  sid: '',
  lines: [] as RemoteConsoleLine[],
  ended: '',
  secure: null as boolean | null, // null — ще не відомо
  // Сервери, для яких P2P вмикається/вимикається (key → true), до фінальної відповіді власника.
  p2pPending: {} as Record<string, boolean>,
})

export const remoteKey = (s: Pick<RemoteServer, 'ownerId' | 'id'>) => `${s.ownerId}|${s.id}`

// ── «Команда в дорозі» ───────────────────────────────────────────────────
// Власник відповідає на start/stop одразу «accepted» (не фінальна відповідь): справжній результат —
// це зміна стану сервера, яка приходить зі списком remote:servers. Раніше pending знімався лише
// на фінальну відповідь, тож після успішного запуску/зупинки кнопка лишалась неактивною назавжди.
// Тепер pending знімається: фінальною відповіддю/помилкою, коли стан став очікуваним, коли сервер
// зник зі списку, або через PENDING_TTL (щоб кнопка ніколи не залипала).
const PENDING_TTL = 45_000
const pendingMeta = new Map<string, { op: 'start' | 'stop'; from: string; timer: ReturnType<typeof setTimeout> }>()

function clearPending(key: string) {
  const m = pendingMeta.get(key)
  if (m) { clearTimeout(m.timer); pendingMeta.delete(key) }
  delete remote.pending[key]
}

function setPending(key: string, op: 'start' | 'stop', from: string) {
  const old = pendingMeta.get(key)
  if (old) clearTimeout(old.timer)
  remote.pending[key] = 'sending' // до отримання reqId; швидка відмова встигне зняти блокування
  pendingMeta.set(key, { op, from, timer: setTimeout(() => clearPending(key), PENDING_TTL) })
}

// Стан, якого чекаємо від сервера після команди.
function pendingDone(op: 'start' | 'stop', state: string): boolean {
  if (op === 'start') return state === 'running' || state === 'crashed' || state === 'error'
  return state === 'stopped' || state === 'crashed' || state === 'error'
}

function settlePending() {
  for (const key of Object.keys(remote.pending)) {
    const m = pendingMeta.get(key)
    const srv = remote.servers.find(r => remoteKey(r) === key)
    if (!m || !srv || !srv.ownerOnline || (srv.state !== m.from && pendingDone(m.op, srv.state))) clearPending(key)
  }
}

// ServerCard приймає ServerView; для сервера друга відомі лише ці поля — решта порожні.
export function remoteAsView(r: RemoteServer): ServerView {
  return {
    id: remoteKey(r), name: r.name, dir: '', core: r.core ?? '', coreLabel: r.core ?? '', mcVersion: '', loaderVer: '',
    javaMajor: 0, javaInstalled: true, coreInstalled: true, ramMB: 0, port: 0, status: r.state || 'stopped',
    color: r.color, icon: r.icon, launcherIconData: r.img,
    hasWorlds: false, hasMods: false, hasPlugins: false, createdAt: 0,
  } as unknown as ServerView
}

export async function loadRemote() {
  try {
    remote.servers = (await App.GetRemoteServers()) ?? []
    const st = await App.GetFriendsState()
    remote.nicks = Object.fromEntries((st.friends ?? []).map((f: SocialFriend) => [f.accountId, f.nickname]))
  } catch { /* соціальна служба ще не запущена */ }
}

let users = 0
let offs: Array<() => void> = []

// startRemote підписується на події один раз (навіть якщо викликано з кількох місць); повертає відписку.
export function startRemote(): () => void {
  if (users++ === 0) {
    void loadRemote()
    offs = [
      on('remote:servers', (d: RemoteServer[]) => { remote.servers = d ?? []; settlePending() }),
      on('friends:update', () => { void loadRemote() }),
      on('remote:secure', (d: { ownerId: string; serverId: string; secure: boolean }) => {
        const c = remote.consoleFor
        if (c && d.ownerId === c.ownerId && d.serverId === c.id) remote.secure = d.secure
      }),
      on('remote:result', (r: RemoteResultEvent) => {
        const key = `${r.ownerId}|${r.serverId}`
        if (r.op === 'console_exec') { if (!r.ok) toast(REMOTE_CODES[r.code ?? ''] ?? r.code ?? 'Помилка', 'error'); return }
        if (r.op === 'console_open') {
          if (!r.ok) { toast(REMOTE_CODES[r.code ?? ''] ?? r.code ?? 'Не вдалося відкрити консоль', 'error'); remote.consoleFor = null; remote.sid = '' }
          return
        }
        if (r.op === 'p2p_on' || r.op === 'p2p_off') {
          if (r.final || !r.ok) delete remote.p2pPending[key]
          if (!r.ok) toast(REMOTE_CODES[r.code ?? ''] ?? r.code ?? 'Помилка', 'error')
          return
        }
        if (r.final || !r.ok) clearPending(key)
        if (!r.ok) toast(REMOTE_CODES[r.code ?? ''] ?? r.code ?? 'Помилка', 'error')
        else if (r.code && REMOTE_CODES[r.code]) toast(REMOTE_CODES[r.code], 'info')
      }),
      on('remote:console', (e: RemoteConsoleEvent) => {
        if (e.sid !== remote.sid) return
        if (e.lines?.length) remote.lines = [...remote.lines, ...e.lines].slice(-1500)
        if (e.end) { remote.ended = REASONS[e.reason ?? 'closed'] ?? 'Сесію закрито'; remote.sid = '' }
      }),
    ]
  }
  return () => {
    if (--users === 0) { offs.forEach(o => o()); offs = [] }
  }
}

export async function remoteAct(s: RemoteServer, what: 'start' | 'stop') {
  const key = remoteKey(s)
  if (remote.pending[key]) return
  setPending(key, what, s.state)
  try {
    const reqId = what === 'start' ? await App.RemoteStartServer(s.ownerId, s.id) : await App.RemoteStopServer(s.ownerId, s.id)
    if (remote.pending[key]) remote.pending[key] = reqId
  } catch (e: any) {
    clearPending(key)
    toast(REMOTE_CODES[String(e?.message)] ?? String(e?.message ?? e), 'error')
  }
}

// remoteP2P — увімкнути/вимкнути публічну P2P-адресу сервера друга. Новий стан приходить
// у списку серверів (власник перепублікує його, щойно тунель піднявся чи зупинився).
export async function remoteP2P(s: RemoteServer, on: boolean) {
  const key = remoteKey(s)
  if (remote.p2pPending[key]) return
  remote.p2pPending[key] = true
  try {
    await App.RemoteSetP2P(s.ownerId, s.id, on)
  } catch (e: any) {
    delete remote.p2pPending[key]
    toast(REMOTE_CODES[String(e?.message)] ?? String(e?.message ?? e), 'error')
  }
}

export async function remoteOpenConsole(s: RemoteServer) {
  await remoteCloseConsole()
  remote.lines = []; remote.ended = ''; remote.secure = null; remote.consoleFor = s
  try { remote.sid = await App.RemoteOpenConsole(s.ownerId, s.id) } catch (e: any) { toast(String(e?.message ?? e), 'error'); remote.consoleFor = null }
}

export async function remoteCloseConsole() {
  const old = remote.sid
  remote.sid = ''; remote.consoleFor = null
  if (old) await App.RemoteCloseConsole(old).catch(() => {})
}

export async function remoteSend(cmd: string) {
  const c = cmd.trim()
  if (!c || !remote.sid) return
  try { await App.RemoteConsoleExec(remote.sid, c) } catch (e: any) { toast(String(e?.message ?? e), 'error') }
}
