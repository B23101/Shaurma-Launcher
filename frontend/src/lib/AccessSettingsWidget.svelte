<script lang="ts">
  // ── AccessSettingsWidget: Налаштування → «Доступ» ─────────────────────────
  // Активація приватних збірок сертифікатом (план «Єдиний лаунчер», §3.7):
  // вибір файлу, перетягування у вікно, вставлений текст; відв'язування
  // пристрою. Після активації розділ збірок з'являється без перезапуску
  // (бекенд шле access:changed).

  import { onMount } from 'svelte'
  import { getLanguage } from './i18n.svelte'
  import { App, on } from './wails'

  type AccessStatus = {
    state: 'none' | 'active' | 'pending' | 'revoked'
    deviceId?: string
    activatedAt?: string
    epoch?: number
    certFound: boolean
    weakStorage?: boolean
    message?: string
    enabled: boolean
  }

  const L = {
    uk: {
      title: 'Доступ до приватних збірок',
      none: 'Не активовано',
      active: 'Активовано',
      pending: 'Очікує схвалення',
      revoked: 'Доступ відкликано',
      desc: 'Додайте файл сертифіката (.cert) — лаунчер сам отримає особистий ключ цього пристрою.',
      pick: 'Вибрати файл сертифіката',
      paste: 'Вставити текстом',
      apply: 'Активувати',
      unlink: 'Відв\'язати цей пристрій',
      unlinkConfirm: 'Відв\'язати пристрій? Розділ приватних збірок зникне.',
      device: 'Пристрій',
      since: 'з',
      weak: 'Ключ збережено зі слабшим захистом (немає сховища ключів ОС).',
      drop: 'Відпустіть файл сертифіката тут',
      disabled: 'Ця збірка лаунчера не підтримує активацію.',
      certFound: 'Сертифікат знайдено біля лаунчера — активація відбудеться автоматично.',
    },
    en: {
      title: 'Access to private packs',
      none: 'Not activated',
      active: 'Activated',
      pending: 'Waiting for approval',
      revoked: 'Access revoked',
      desc: 'Add the certificate file (.cert) — the launcher will get a personal key for this device.',
      pick: 'Choose certificate file',
      paste: 'Paste as text',
      apply: 'Activate',
      unlink: 'Unlink this device',
      unlinkConfirm: 'Unlink this device? The private packs section will disappear.',
      device: 'Device',
      since: 'since',
      weak: 'The key is stored with weaker protection (no OS key store).',
      drop: 'Drop the certificate file here',
      disabled: 'This launcher build does not support activation.',
      certFound: 'A certificate was found next to the launcher — activation will happen automatically.',
    },
  }
  const tr = $derived(getLanguage() === 'en' ? L.en : L.uk)

  let status = $state<AccessStatus | null>(null)
  let busy = $state(false)
  let error = $state('')
  let pasteOpen = $state(false)
  let pasted = $state('')
  let dragging = $state(false)

  async function refresh() {
    try { status = await App.GetAccessStatus() } catch { status = null }
  }

  async function run(p: Promise<AccessStatus>) {
    busy = true
    error = ''
    try {
      status = await p
    } catch (e: any) {
      error = String(e?.message ?? e)
      await refresh()
    } finally {
      busy = false
    }
  }

  function pick() { run(App.PickAndActivateAccess()) }
  function submitPaste() {
    if (!pasted.trim()) return
    run(App.ActivateAccessFromText(pasted)).then(() => { if (!error) { pasteOpen = false; pasted = '' } })
  }
  function unlink() {
    if (confirm(tr.unlinkConfirm)) run(App.UnlinkAccessDevice())
  }

  // Перетягування файлу: у вбудованому webview шлях недоступний, тож
  // читаємо вміст (сертифікат крихітний) і активуємо текстом.
  async function onDrop(ev: DragEvent) {
    ev.preventDefault()
    dragging = false
    const f = ev.dataTransfer?.files?.[0]
    if (!f || f.size > 16384) return
    run(App.ActivateAccessFromText(await f.text()))
  }

  onMount(() => {
    refresh()
    return on('access:changed', (s: AccessStatus) => { status = s })
  })

  const stateText = $derived(status ? tr[status.state] ?? status.state : '')
  const since = $derived(status?.activatedAt ? new Date(status.activatedAt).toLocaleDateString() : '')
</script>

<div
  class="card access-widget"
  class:dragging
  role="region"
  aria-label={tr.title}
  ondragover={(e) => { e.preventDefault(); dragging = true }}
  ondragleave={() => (dragging = false)}
  ondrop={onDrop}
>
  <div class="card-head">
    <div class="ci orange"><i class="ti ti-key"></i></div>
    <div class="ctw">
      <div class="ct">{tr.title}</div>
      <div class="cd">
        {#if !status}
          …
        {:else if !status.enabled}
          {tr.disabled}
        {:else}
          <span class="aw-state aw-{status.state}">{stateText}</span>
          {#if status.deviceId}
            · {tr.device} <code>{status.deviceId}</code>{#if since} {tr.since} {since}{/if}
          {/if}
        {/if}
      </div>
    </div>
    {#if status?.enabled}
      {#if status.state === 'active'}
        <button class="btn btn-sm" onclick={unlink} disabled={busy}><i class="ti ti-link-off"></i> {tr.unlink}</button>
      {:else}
        <button class="btn btn-primary btn-sm" onclick={pick} disabled={busy}><i class="ti ti-file-certificate"></i> {tr.pick}</button>
      {/if}
    {/if}
  </div>

  {#if status?.enabled && status.state !== 'active'}
    <div class="aw-body">
      <p class="aw-hint">{dragging ? tr.drop : (status.certFound ? tr.certFound : tr.desc)}</p>
      {#if pasteOpen}
        <textarea class="aw-paste" rows="4" bind:value={pasted} spellcheck="false"></textarea>
        <button class="btn btn-primary btn-sm" onclick={submitPaste} disabled={busy || !pasted.trim()}>{tr.apply}</button>
      {:else}
        <button class="btn btn-ghost btn-sm" onclick={() => (pasteOpen = true)}>{tr.paste}</button>
      {/if}
    </div>
  {/if}

  {#if status?.weakStorage}<p class="aw-hint aw-warn">{tr.weak}</p>{/if}
  {#if error || status?.message}<p class="aw-hint aw-err" role="alert">{error || status?.message}</p>{/if}
</div>

<style>
  .access-widget.dragging { outline: 2px dashed var(--accent, #ff8a00); outline-offset: -4px; }
  .aw-body { display: flex; flex-direction: column; gap: 8px; padding: 0 16px 12px; align-items: flex-start; }
  .aw-hint { margin: 0; padding: 0 16px 8px; font-size: 12px; opacity: .8; }
  .aw-body .aw-hint { padding: 0; }
  .aw-warn { color: var(--warning, #e0a100); }
  .aw-err { color: var(--danger, #e5484d); }
  .aw-paste { width: 100%; box-sizing: border-box; font-family: monospace; font-size: 12px; }
  .aw-state { font-weight: 600; }
  .aw-active { color: var(--success, #3fb950); }
  .aw-revoked { color: var(--danger, #e5484d); }
  .aw-pending { color: var(--warning, #e0a100); }
</style>
