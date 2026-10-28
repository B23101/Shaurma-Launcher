<script lang="ts">
  // ── Налаштування сервера ────────────────────────────────────────────
  //
  // МАКЕТ (як просив користувач): шапка розділу зі станом сервера → картка
  // «Базові параметри» (ядро/версія/Java/RAM/порт/аргументи в сітці) →
  // server.properties окремою карткою з пошуком і вкладками груп →
  // небезпечна зона останньою. Кольори — ЛАУНЧЕРА (--bg-card/--border/--pcol),
  // жодних Tailwind-класів із макета: у проєкті своя тема.
  //
  // АРГУМЕНТИ — БЛОКАМИ, а не рядком (JvmArgsEditor: один блок = один
  // аргумент), як у налаштуваннях збірки. У макеті під них було текстове
  // поле — тут свідомо НЕ так: «налаштування аргументів» має бути однакове
  // і для збірки, і для сервера.
  //
  // ЗБЕРЕЖЕННЯ: одна панель знизу з'являється, лише коли щось змінено, і пише
  // в конфіг ТІЛЬКИ змінені поля (незачеплені не перезаписуються).
  import { App } from '../../wails'
  import { toast, confirmDialog } from '../../toast.svelte'
  import { t } from '../../i18n.svelte'
  import type { ServerView, ServerConfigPatch } from '../../types'
  import JvmArgsEditor from '../../JvmArgsEditor.svelte'
  import StSelect from '../../StSelect.svelte'
  import SCard from '../../settings/SCard.svelte'
  import ServerCoreChangeModal from '../components/ServerCoreChangeModal.svelte'
  import PackServerLinkCard from '../components/PackServerLinkCard.svelte'
  import { CORE_META } from '../constants'

  let {
    srv,
    isRunning = false,
    isVelocity = false,
    onUpdated = (_s: ServerView) => {},
    onDeleted = () => {},
    onOpenProperties = () => {},
  }: {
    srv: ServerView
    isRunning?: boolean
    isVelocity?: boolean
    onUpdated?: (s: ServerView) => void
    onDeleted?: () => void
    onOpenProperties?: () => void
  } = $props()

  // Проксі не тримає світів — свої межі RAM (Velocity: 512 МБ–1 ГБ типово).
  const ramMin = $derived(isVelocity ? 256 : 512)
  const ramMax = $derived(isVelocity ? 4096 : 32768)
  const ramStep = $derived(isVelocity ? 128 : 256)
  // 4096 МБ → «4 ГБ», 1536 МБ → «1.5 ГБ», 512 МБ → «512 МБ».
  const fmtRam = (mb: number) => mb >= 1024 ? `${+(mb / 1024).toFixed(2)} ГБ` : `${mb} МБ`

  let editRAM = $state(srv.ramMB)
  let editJvmArgs = $state(srv.jvmArgs || '')
  let editProgramArgs = $state(srv.programArgs || '')
  let editJavaMajor = $state(srv.javaMajor)
  let editPort = $state(srv.port || 25565)
  let coreModalOpen = $state(false)

  // ── Жива перевірка порту (вільний/зайнятий) ─────────────────────────
  let portCheck = $state<{ free: boolean; holder?: string } | null>(null)
  let portChecking = $state(false)
  let portSeq = 0
  $effect(() => {
    const id = srv.id
    const p = Number(editPort)
    if (!Number.isFinite(p) || p < 1 || p > 65534) { portCheck = null; return }
    const seq = ++portSeq
    const timer = setTimeout(() => {
      portChecking = true
      App.CheckServerPortForServer(id, p)
        .then(r => { if (seq === portSeq) portCheck = r })
        .catch(() => { if (seq === portSeq) portCheck = null })
        .finally(() => { if (seq === portSeq) portChecking = false })
    }, 250)
    return () => clearTimeout(timer)
  })

  // Синхронізація з srv (коли він оновлюється зовні)
  let loadedId = ''
  $effect(() => {
    if (srv && loadedId !== srv.id) {
      loadedId = srv.id
      editRAM = srv.ramMB; editJvmArgs = srv.jvmArgs || ''
      editProgramArgs = srv.programArgs || ''
      editJavaMajor = srv.javaMajor
      editPort = srv.port || 25565
    }
  })

  // ── Брудні поля ──
  const dirtyRAM = $derived(editRAM !== srv.ramMB)
  const dirtyJvm = $derived(editJvmArgs !== (srv.jvmArgs || ''))
  const dirtyProgram = $derived(editProgramArgs !== (srv.programArgs || ''))
  const dirtyJava = $derived(editJavaMajor !== srv.javaMajor)
  const dirtyPort = $derived(Number(editPort) !== Number(srv.port || (isVelocity ? 25577 : 25565)))
  // Скільки полів змінено — для пігулки в шапці.
  const dirtyCount = $derived([dirtyRAM, dirtyJvm, dirtyProgram, dirtyJava, dirtyPort].filter(Boolean).length)

  let saving = $state(false)

  function revertAll() {
    editRAM = srv.ramMB; editJvmArgs = srv.jvmArgs || ''
    editProgramArgs = srv.programArgs || ''
    editJavaMajor = srv.javaMajor
    editPort = srv.port || (isVelocity ? 25577 : 25565)
  }

  async function saveAll() {
    const patch: Partial<ServerConfigPatch> = {}
    if (dirtyRAM) {
      if (editRAM < ramMin) { toast(t('servers.settings.ramMin'), 'error'); return }
      patch.ramMB = editRAM
    }
    if (dirtyJvm) patch.jvmArgs = editJvmArgs
    if (dirtyProgram) patch.programArgs = editProgramArgs
    if (dirtyJava) patch.javaMajor = editJavaMajor
    if (dirtyPort) {
      const p = Number(editPort)
      if (!Number.isInteger(p) || p < 1 || p > 65534) { toast(t('servers.settings.portInvalid'), 'error'); return }
      if (portCheck && !portCheck.free) { toast(t('servers.settings.portBusy', { holder: portCheck.holder || '—' }), 'error'); return }
      if (isRunning) { toast(t('servers.settings.portStopFirst'), 'error'); return }
      if (isVelocity) {
        // Порт проксі живе у velocity.toml й налаштуваннях мережі, а не в server.properties:
        // зберігаємо тим самим викликом, що й вкладка «Мережа» (він перевіряє конфлікти з учасниками).
        saving = true
        try {
          const v = await App.NetworkGetView(srv.id)
          await App.NetworkSaveSettings(srv.id, { ...$state.snapshot(v.settings), proxyPort: p })
          const updated = await App.GetServer(srv.id)
          onUpdated(updated)
          toast(t('servers.settings.savedOne'), 'success')
        } catch (e: any) { toast(String(e), 'error') }
        finally { saving = false }
        if (Object.keys(patch).length === 0) return
      } else {
        patch.port = p
      }
    }
    if (Object.keys(patch).length === 0) return

    saving = true
    try {
      const updated = await App.SaveServerConfig(srv.id, patch)
      // ПОРТ: джерело правди — server.properties. Якщо не дописати
      // server-port туди, StartServer прочитає СТАРЕ значення з файлу і
      // зміна в UI «зникне» при наступному старті.
      if (patch.port !== undefined) {
        try {
          await App.SaveServerProperties(srv.id, [{ key: 'server-port', value: String(patch.port) }])
        } catch {
          // У Velocity свого server.properties немає (порт живе у
          // velocity.toml) — тоді лише кеш у сторі.
        }
      }
      onUpdated(updated)
      toast(t('servers.settings.savedOne'), 'success')
    } catch (e: any) {
      toast(String(e), 'error')
    } finally {
      saving = false
    }
  }

  async function del() {
    const proceed = await confirmDialog(
      t('servers.settings.deleteTitle'),
      t('servers.settings.deleteDesc'),
      { confirmLabel: t('delete'), cancelLabel: t('cancel'), danger: true },
    )
    if (!proceed) return
    try { await App.DeleteServer(srv.id); toast(t('servers.settings.deleted'), 'success'); onDeleted() }
    catch (e: any) { toast(String(e), 'error') }
  }

  const javaOptions = [
    { value: '8', label: 'Java 8' },
    { value: '16', label: 'Java 16' },
    { value: '17', label: 'Java 17' },
    { value: '21', label: 'Java 21' },
    { value: '25', label: 'Java 25' },
  ]

  const coreMeta = $derived(CORE_META[srv.core])
</script>

<div class="sst" style="--pcol:{srv.color || '#ff8a00'}">
  <!-- Заголовок розділу й стан сервера показує сама сторінка сервера (шапка розділу + банер).
       Розмітка — як у налаштуваннях лаунчера: SCard + .st-row (settings.css). -->
  <SCard icon="ti-cpu" title={t('servers.settings.basic')}>
    <!-- ЯДРО -->
    <div class="st-row" data-skey="core">
      <div class="st-row-text">
        <div class="st-row-name">{t('servers.settings.core')}</div>
        <div class="st-row-hint sst-core">
          {#if coreMeta?.icon}<img src={coreMeta.icon} alt="" />{/if}
          <b>{srv.coreLabel || coreMeta?.label || srv.core}</b>
          <span class="mono">{srv.mcVersion || (isVelocity ? 'proxy' : '')}</span>
          {#if srv.coreInstalled}
            <span class="st-tag ok"><i class="ti ti-check"></i> {t('servers.settings.coreInstalled')}</span>
          {:else}
            <span class="st-tag warn"><i class="ti ti-download"></i> {t('servers.settings.coreMissing')}</span>
          {/if}
        </div>
      </div>
      <div class="st-row-ctl">
        <button class="btn btn-ghost btn-sm" onclick={() => coreModalOpen = true} disabled={isRunning} data-testid="server-core-change-btn"
                title={isRunning ? t('servers.core.running') : t('servers.settings.coreChange')}>
          <i class="ti ti-replace"></i> {t('servers.settings.coreChange')}
        </button>
      </div>
    </div>

    <!-- JAVA -->
    <div class="st-row" class:sst-dirty={dirtyJava} data-skey="java">
      <div class="st-row-text">
        <div class="st-row-name">Java</div>
        <div class="st-row-hint">{t('servers.settings.javaDesc')}</div>
        {#if srv.javaInstalled === false}
          <div class="sst-note warn"><i class="ti ti-alert-triangle"></i> {t('servers.settings.javaNotInstalled')}</div>
        {/if}
      </div>
      <div class="st-row-ctl">
        <StSelect value={String(editJavaMajor)} options={javaOptions} onchange={(v) => { editJavaMajor = Number(v) }}
                  ariaLabel="Java" minWidth="170px" />
      </div>
    </div>

    <!-- ПОРТ -->
    <div class="st-row" class:sst-dirty={dirtyPort} data-skey="port">
      <div class="st-row-text">
        <div class="st-row-name">{t('servers.settings.port')}</div>
        <div class="sst-note" class:ok={portCheck?.free === true} class:bad={!!portCheck && !portCheck.free}>
          {#if portChecking}
            <i class="ti ti-loader spin"></i> {t('servers.settings.portChecking')}
          {:else if portCheck === null}
            <i class="ti ti-help-circle"></i> {t('servers.settings.portUnknown')}
          {:else if portCheck.free}
            <i class="ti ti-circle-check"></i> {t('servers.settings.portFree')}
          {:else}
            <i class="ti ti-circle-x"></i> {t('servers.settings.portBusy', { holder: portCheck.holder || '—' })}
          {/if}
        </div>
      </div>
      <div class="st-row-ctl">
        <input id="srv-port" class="st-input num mono" type="number" min="1" max="65534" bind:value={editPort} data-testid="server-port-input" />
      </div>
    </div>

    <!-- RAM -->
    <div class="st-row stack" class:sst-dirty={dirtyRAM} data-skey="ram">
      <div class="st-row-text">
        <div class="st-row-name">{t('servers.settings.ram')}</div>
        <div class="st-row-hint">{t('servers.settings.ramDesc')}</div>
      </div>
      <div class="st-row-ctl">
        <div class="st-slider">
          <input id="srv-ram" type="range" min={ramMin} max={ramMax} step={ramStep} bind:value={editRAM} aria-label={t('servers.settings.ram')} />
          <span class="st-slider-val">{fmtRam(editRAM)}</span>
        </div>
      </div>
    </div>
  </SCard>

  <!-- АРГУМЕНТИ ЗАПУСКУ (блоками, як у налаштуваннях збірки) -->
  <SCard icon="ti-terminal-2" title={t('servers.settings.args')} desc={t('servers.settings.argsDesc')}>
    <div class="st-row stack" class:sst-dirty={dirtyJvm} data-skey="args">
      <div class="st-row-text">
        <div class="st-row-name">{t('servers.settings.jvmArgs')}</div>
        <div class="st-row-hint">{t('servers.settings.jvmArgsDesc')}</div>
      </div>
      <div class="st-row-ctl"><JvmArgsEditor value={editJvmArgs} onchange={(v: string) => editJvmArgs = v} /></div>
    </div>
    <div class="st-row stack" class:sst-dirty={dirtyProgram}>
      <div class="st-row-text">
        <div class="st-row-name">{t('servers.settings.programArgs')}</div>
        <div class="st-row-hint">{t('servers.settings.programArgsDesc')}</div>
      </div>
      <div class="st-row-ctl"><JvmArgsEditor value={editProgramArgs} onchange={(v: string) => editProgramArgs = v} placeholder="--nogui" /></div>
    </div>
  </SCard>

  <!-- server.properties — окрема сторінка -->
  {#if !isVelocity}
    <SCard icon="ti-adjustments-horizontal" title={t('servers.settings.propsCta.title')} desc={t('servers.settings.propsCta.desc')}>
      {#snippet actions()}
        <button class="btn btn-ghost btn-sm" onclick={onOpenProperties} data-testid="server-properties-open">{t('servers.settings.propsCta.open')} <i class="ti ti-arrow-right"></i></button>
      {/snippet}
    </SCard>
  {/if}

  <!-- Запуск разом зі збіркою (та сама картка, що й на сторінці збірки) -->
  {#if !isVelocity}<PackServerLinkCard serverId={srv.id} />{/if}

  <!-- Небезпечна зона -->
  <SCard icon="ti-trash" tone="red" title={t('servers.settings.delete')} desc={t('servers.settings.deleteDesc')} id="delete">
    {#snippet actions()}
      <button class="btn btn-danger btn-sm" onclick={del} data-testid="server-delete-btn"><i class="ti ti-trash"></i> {t('delete')}</button>
    {/snippet}
  </SCard>

  <!-- Одна панель збереження: зберігає ЛИШЕ змінені поля. -->
  {#if dirtyCount > 0}
    <div class="sst-save">
      <span class="sst-save-t"><i class="ti ti-pencil"></i> {t('servers.settings.unsaved', { n: dirtyCount })}</span>
      <button class="btn btn-ghost btn-sm" onclick={revertAll} disabled={saving}><i class="ti ti-arrow-back-up"></i> {t('servers.props.revertAll')}</button>
      <button class="btn btn-primary btn-sm" onclick={saveAll} disabled={saving}>
        {#if saving}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-check"></i>{/if} {t('servers.settings.save')}
      </button>
    </div>
  {/if}
</div>

{#if coreModalOpen}
  <ServerCoreChangeModal
    {srv}
    open={coreModalOpen}
    running={isRunning}
    onclose={() => coreModalOpen = false}
    onchanged={(updated) => { onUpdated(updated) }}
  />
{/if}

<style>
  .sst { display: flex; flex-direction: column; padding-bottom: 24px; min-width: 0; }
  .sst > :global(*) { min-width: 0; }

  /* Змінене, ще не збережене поле — тонка смужка зліва (як у властивостях сервера). */
  .sst :global(.st-row) { position: relative; }
  .sst-dirty::before { content: ''; position: absolute; left: 0; top: 12px; bottom: 12px; width: 3px; border-radius: 0 3px 3px 0; background: var(--orange); }

  .sst-core { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; margin-top: 4px; }
  .sst-core img { width: 18px; height: 18px; object-fit: contain; }
  .sst-core b { color: var(--text); font-weight: 600; }
  .sst-core .mono { font-family: var(--font-mono); font-size: 11.5px; }

  .sst-note { display: flex; align-items: center; gap: 5px; margin-top: 3px; font-size: 12px; color: var(--text-mute); }
  .sst-note.ok { color: var(--green); }
  .sst-note.bad { color: var(--red); }
  .sst-note.warn { color: var(--yellow); }

  .sst :global(.st-row.stack .st-row-ctl > .jvm-editor) { width: 100%; }

  .sst-save {
    position: sticky; bottom: 0; z-index: 5; display: flex; align-items: center; gap: 10px; flex-wrap: wrap;
    padding: 10px 14px 10px 18px; border-radius: var(--r-lg);
    background: var(--bg-card); border: 1px solid rgba(var(--orange-rgb), .45);
    box-shadow: 0 10px 30px rgba(0, 0, 0, .3);
  }
  .sst-save-t { flex: 1; display: inline-flex; align-items: center; gap: 6px; font-size: 13px; font-weight: 600; color: var(--orange); }
</style>
