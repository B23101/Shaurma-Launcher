<script lang="ts">
  // AIAgentSection.svelte — розділ «ШІ-агент» у налаштуваннях: підключення,
  // дрібні права за категоріями, поведінка (порт, час блокування, плашка).
  // Розмітка — ті самі картки/рядки (.st-*), що й в інших розділах, тож пошук
  // налаштувань знаходить кожен параметр за якорем data-setting-key.

  import { onMount } from 'svelte'
  import { t } from '../i18n.svelte'
  import { App } from '../wails'
  import { toast, confirmDialog } from '../toast.svelte'
  import SCard from './SCard.svelte'

  type Status = {
    enabled: boolean
    allowWrite: boolean
    running: boolean
    disconnectedUntil?: number
    perms: Record<string, boolean>
    permOrder: string[]
    port: number
    wantedPort: number
    lockSeconds: number
    showIndicator: boolean
    protectUserData: boolean
    editPolicy: string
    ask: Record<string, boolean>
    askOrder: string[]
    agentNick: string
    maxClients: number
    agentAutoUpdate: boolean
    url: string
    token: string
    claudeCodeCmd: string
    configJson: string
    error?: string
  }

  let status = $state<Status | null>(null)
  let busy = $state(false)
  let copied = $state('')
  let portDraft = $state('')
  let lockDraft = $state('')

  // Категорії прав: id → іконка. Тексти — st.x.aiPerm<Id> (+ `.d`).
  const permIcon: Record<string, string> = {
    mods: 'ti-package', buildSettings: 'ti-adjustments', create: 'ti-sparkles', deleteOwn: 'ti-trash',
    style: 'ti-palette', launch: 'ti-player-play', snapshots: 'ti-shield-check', servers: 'ti-server', launcherSettings: 'ti-settings',
    gameControl: 'ti-device-gamepad-2',
  }
  const permKey = (id: string) => `aiPerm${id[0].toUpperCase()}${id.slice(1)}`

  const SAFE = ['mods', 'buildSettings', 'create', 'deleteOwn', 'style', 'launch', 'snapshots', 'gameControl']
  const preset = $derived.by(() => {
    if (!status) return ''
    const on = status.permOrder.filter((c) => status!.perms[c])
    if (on.length === 0) return 'readonly'
    if (on.length === status.permOrder.length) return 'full'
    if (on.length === SAFE.length && SAFE.every((c) => status!.perms[c])) return 'safe'
    return 'custom'
  })

  function sync(s: Status) {
    status = s
    portDraft = String(s.wantedPort || 0)
    lockDraft = String(s.lockSeconds)
  }

  async function run(p: Promise<Status>) {
    busy = true
    try {
      sync(await p)
      if (status?.error) toast(status.error, 'error')
    } catch (e: any) {
      toast(String(e?.message ?? e), 'error')
      try { sync(await App.GetAIAgentStatus()) } catch {}
    } finally {
      busy = false
    }
  }

  async function copy(key: string, text: string) {
    try {
      await navigator.clipboard.writeText(text)
      copied = key
      setTimeout(() => { if (copied === key) copied = '' }, 1600)
    } catch {}
  }

  async function regen() {
    const ok = await confirmDialog(t('st.x.aiToken'), t('st.aiToken.confirm'), { confirmLabel: t('st.aiToken.regen'), cancelLabel: t('st.aiAgent.cancel') })
    if (ok) run(App.RegenerateAIAgentToken())
  }

  function applyPort() {
    const n = Math.round(Number(portDraft))
    if (!Number.isFinite(n) || (n !== 0 && (n < 1024 || n > 65535))) { portDraft = String(status?.wantedPort ?? 0); toast(t('st.aiPort.bad'), 'warn'); return }
    if (n !== (status?.wantedPort ?? 0)) run(App.SetAIAgentPort(n))
  }
  function applyLock() {
    const n = Math.max(30, Math.min(900, Math.round(Number(lockDraft)) || 90))
    lockDraft = String(n)
    if (n !== status?.lockSeconds) run(App.SetAIAgentLockSeconds(n))
  }

  onMount(async () => {
    try { sync(await App.GetAIAgentStatus()) } catch {}
  })
</script>

<!-- ── Підключення ──────────────────────────────────────────────────── -->
<SCard icon="ti-robot" tone="orange" title={t('st.c.aiConnection')} desc={t('st.c.aiConnection.d')}>
  <div class="st-row" data-setting-key="x:aiEnable">
    <div class="st-row-text">
      <div class="st-row-name">{t('st.x.aiEnable')}</div>
      <div class="st-row-hint">
        {#if status?.running}{t('st.aiAgent.running')} · <code>{status.url}</code>{:else}{t('st.x.aiEnable.d')}{/if}
      </div>
    </div>
    <div class="st-row-ctl">
      <button type="button" class="st-switch" class:on={!!status?.enabled} role="switch" aria-checked={!!status?.enabled}
              aria-label={t('st.x.aiEnable')} disabled={busy || !status} onclick={() => run(App.SetAIAgentEnabled(!status!.enabled))}></button>
    </div>
  </div>

  {#if status?.running && status.disconnectedUntil && status.disconnectedUntil > Date.now()}
    <div class="st-row" data-setting-key="x:aiDisconnected">
      <div class="st-row-text">
        <div class="st-row-name">{t('st.aiDisc.title')}</div>
        <div class="st-row-hint">{t('st.aiDisc.hint')}</div>
      </div>
      <div class="st-row-ctl">
        <button class="btn btn-primary btn-sm" disabled={busy} onclick={() => run(App.AllowAIAgentAgain())}><i class="ti ti-plug-connected"></i> {t('st.aiDisc.allow')}</button>
      </div>
    </div>
  {/if}

  {#if status?.running}
    <div class="st-row stack" data-setting-key="x:aiCommand">
      <div class="st-row-text">
        <div class="st-row-name">{t('st.x.aiCommand')}</div>
        <div class="st-row-hint">{t('st.x.aiCommand.d')}</div>
      </div>
      <div class="st-row-ctl ai-code-row">
        <pre class="ai-code">{status.claudeCodeCmd}</pre>
        <button class="btn btn-ghost btn-sm" onclick={() => copy('cmd', status!.claudeCodeCmd)}>
          <i class="ti {copied === 'cmd' ? 'ti-check' : 'ti-copy'}"></i> {copied === 'cmd' ? t('st.aiAgent.copied') : t('st.aiAgent.copy')}
        </button>
      </div>
    </div>

    <div class="st-row stack" data-setting-key="x:aiConfig">
      <div class="st-row-text">
        <div class="st-row-name">{t('st.x.aiConfig')}</div>
        <div class="st-row-hint">{t('st.x.aiConfig.d')}</div>
      </div>
      <div class="st-row-ctl ai-code-row">
        <pre class="ai-code">{status.configJson}</pre>
        <button class="btn btn-ghost btn-sm" onclick={() => copy('cfg', status!.configJson)}>
          <i class="ti {copied === 'cfg' ? 'ti-check' : 'ti-copy'}"></i> {copied === 'cfg' ? t('st.aiAgent.copied') : t('st.aiAgent.copy')}
        </button>
      </div>
    </div>

    <div class="st-row stack" data-setting-key="x:aiParts">
      <div class="st-row-text">
        <div class="st-row-name">{t('st.x.aiParts')}</div>
        <div class="st-row-hint">{t('st.x.aiParts.d')}</div>
      </div>
      <div class="ai-parts">
        {#each [
          { k: 'url', label: t('st.aiPart.url'), val: status.url, shown: status.url },
          { k: 'hdr', label: t('st.aiPart.header'), val: 'Authorization', shown: 'Authorization' },
          { k: 'hval', label: t('st.aiPart.headerValue'), val: 'Bearer ' + status.token, shown: 'Bearer ••••••••' },
          { k: 'tok', label: t('st.aiPart.token'), val: status.token, shown: '••••••••••••••••' },
        ] as part (part.k)}
          <div class="ai-part">
            <span class="ai-part-l">{part.label}</span>
            <code class="ai-part-v">{part.shown}</code>
            <button class="btn btn-ghost btn-sm" onclick={() => copy(part.k, part.val)}>
              <i class="ti {copied === part.k ? 'ti-check' : 'ti-copy'}"></i> {copied === part.k ? t('st.aiAgent.copied') : t('st.aiAgent.copy')}
            </button>
          </div>
        {/each}
      </div>
    </div>

    <div class="st-row" data-setting-key="x:aiToken">
      <div class="st-row-text">
        <div class="st-row-name">{t('st.x.aiToken')}</div>
        <div class="st-row-hint">{t('st.x.aiToken.d')}</div>
      </div>
      <div class="st-row-ctl">
        <button class="btn btn-ghost btn-sm" disabled={busy} onclick={regen}><i class="ti ti-key"></i> {t('st.aiToken.regen')}</button>
      </div>
    </div>
  {/if}
</SCard>

<!-- ── Права ────────────────────────────────────────────────────────── -->
<SCard icon="ti-shield-lock" tone="green" title={t('st.c.aiPerms')} desc={t('st.c.aiPerms.d')}>
  <div class="st-row stack" data-setting-key="x:aiPreset">
    <div class="st-row-text">
      <div class="st-row-name">{t('st.x.aiPreset')}</div>

    </div>
    <div class="st-row-ctl">
      <div class="st-pills">
        {#each ['readonly', 'safe', 'full'] as p (p)}
          <button type="button" class="st-pill" class:sel={preset === p} disabled={busy || !status}
                  onclick={() => run(App.SetAIAgentPreset(p))}>{t(`st.aiPreset.${p}`)}</button>
        {/each}
        {#if preset === 'custom'}<span class="st-pill sel">{t('st.aiPreset.custom')}</span>{/if}
      </div>
    </div>
  </div>

  <div class="st-row stack" data-setting-key="x:aiEditPolicy">
    <div class="st-row-text">
      <div class="st-row-name">{t('st.x.aiEditPolicy')}</div>
      
    </div>
    <div class="st-row-ctl">
      <div class="st-pills">
        {#each ['ask', 'copy', 'original'] as p (p)}
          <button type="button" class="st-pill" class:sel={status?.editPolicy === p} disabled={busy || !status}
                  onclick={() => run(App.SetAIAgentEditPolicy(p))}>{t(`st.aiEdit.${p}`)}</button>
        {/each}
      </div>
    </div>
  </div>

  {#each status?.permOrder ?? [] as id (id)}
    <div class="st-row" data-setting-key={`x:${permKey(id)}`}>
      <div class="ai-perm-ic"><i class="ti {permIcon[id] ?? 'ti-point'}"></i></div>
      <div class="st-row-text">
        <div class="st-row-name">{t(`st.x.${permKey(id)}`)}</div>
        {#if t(`st.x.${permKey(id)}.d`)}<div class="st-row-hint">{t(`st.x.${permKey(id)}.d`)}</div>{/if}
      </div>
      <div class="st-row-ctl">
        <button type="button" class="st-switch" class:on={!!status?.perms[id]} role="switch" aria-checked={!!status?.perms[id]}
                aria-label={t(`st.x.${permKey(id)}`)} disabled={busy}
                onclick={() => run(App.SetAIAgentPermission(id, !status!.perms[id]))}></button>
      </div>
    </div>
  {/each}
</SCard>

<!-- ── Коли питати ──────────────────────────────────────────────────── -->
<SCard icon="ti-message-question" tone="yellow" title={t('st.c.aiAsk')} desc={t('st.c.aiAsk.d')}>
  {#each status?.askOrder ?? [] as key (key)}
    <div class="st-row" data-setting-key={`x:aiAsk${key[0].toUpperCase()}${key.slice(1)}`}>
      <div class="st-row-text">
        <div class="st-row-name">{t(`st.x.aiAsk${key[0].toUpperCase()}${key.slice(1)}`)}</div>
        {#if t(`st.x.aiAsk${key[0].toUpperCase()}${key.slice(1)}.d`)}<div class="st-row-hint">{t(`st.x.aiAsk${key[0].toUpperCase()}${key.slice(1)}.d`)}</div>{/if}
      </div>
      <div class="st-row-ctl">
        <button type="button" class="st-switch" class:on={!!status?.ask[key]} role="switch" aria-checked={!!status?.ask[key]}
                aria-label={t(`st.x.aiAsk${key[0].toUpperCase()}${key.slice(1)}`)} disabled={busy}
                onclick={() => run(App.SetAIAgentAsk(key, !status!.ask[key]))}></button>
      </div>
    </div>
  {/each}
</SCard>

<!-- ── Поведінка ────────────────────────────────────────────────────── -->
<SCard icon="ti-adjustments" tone="blue" title={t('st.c.aiBehavior')}>
  <div class="st-row" data-setting-key="x:aiIndicator">
    <div class="st-row-text">
      <div class="st-row-name">{t('st.x.aiIndicator')}</div>
      
    </div>
    <div class="st-row-ctl">
      <button type="button" class="st-switch" class:on={!!status?.showIndicator} role="switch" aria-checked={!!status?.showIndicator}
              aria-label={t('st.x.aiIndicator')} disabled={busy || !status} onclick={() => run(App.SetAIAgentShowIndicator(!status!.showIndicator))}></button>
    </div>
  </div>

  <div class="st-row" data-setting-key="x:aiLock">
    <div class="st-row-text">
      <div class="st-row-name">{t('st.x.aiLock')}</div>
      
    </div>
    <div class="st-row-ctl">
      <input class="st-input num" type="number" min="30" max="900" step="10" bind:value={lockDraft}
             onchange={applyLock} onblur={applyLock} aria-label={t('st.x.aiLock')} />
      <span class="ai-unit">{t('st.aiLock.unit')}</span>
    </div>
  </div>

  <div class="st-row" data-setting-key="x:aiPort">
    <div class="st-row-text">
      <div class="st-row-name">{t('st.x.aiPort')}</div>
      <div class="st-row-hint">{t('st.x.aiPort.d')}{#if status?.running} · {t('st.aiPort.now')} {status.port}{/if}</div>
    </div>
    <div class="st-row-ctl">
      <input class="st-input num" type="number" min="0" max="65535" bind:value={portDraft}
             onchange={applyPort} onblur={applyPort} aria-label={t('st.x.aiPort')} />
    </div>
  </div>
</SCard>

<style>
  .ai-code-row { align-items: flex-start; flex-wrap: nowrap; }
  .ai-code {
    flex: 1; min-width: 0; margin: 0; padding: 10px 12px; box-sizing: border-box;
    background: var(--bg-input); border: 1px solid var(--border); border-radius: 10px;
    font: 12px/1.5 var(--font-mono); color: var(--text-mute); white-space: pre-wrap; word-break: break-all; user-select: all;
  }
  .ai-parts { display: flex; flex-direction: column; gap: 6px; width: 100%; }
  .ai-part { display: flex; align-items: center; gap: 10px; padding: 6px 8px 6px 12px; background: var(--bg-input); border: 1px solid var(--border); border-radius: 10px; }
  .ai-part-l { flex: none; width: 130px; font-size: 12px; color: var(--text-mute); }
  .ai-part-v { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text); }
  .ai-perm-ic {
    flex: none; width: 32px; height: 32px; border-radius: 9px; display: grid; place-items: center; font-size: 17px;
    background: var(--bg-input); color: var(--text-mute);
  }
  .ai-unit { font-size: 12px; color: var(--text-mute); }
  code { font: 12px var(--font-mono); }
</style>
