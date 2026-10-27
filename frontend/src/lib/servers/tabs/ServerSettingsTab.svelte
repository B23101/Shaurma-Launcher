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
  // ЗБЕРЕЖЕННЯ: кожне поле має власну галочку-підтвердження (зʼявляється
  // лише коли значення змінене), а server.properties має власну липку панель
  // знизу. Раніше тут була одна кнопка «Зберегти все» — вона змушувала
  // перезаписувати поля, які гравець навіть не чіпав.
  import { App } from '../../wails'
  import { toast, confirmDialog } from '../../toast.svelte'
  import { t } from '../../i18n.svelte'
  import type { ServerView, ServerConfigPatch } from '../../types'
  import JvmArgsEditor from '../../JvmArgsEditor.svelte'
  import SelectField from '../../SelectField.svelte'
  import ServerCoreChangeModal from '../components/ServerCoreChangeModal.svelte'
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

  const ramMin = 512, ramMax = 32768, ramStep = 256
  // 4096 МБ → «4 ГБ», 1536 МБ → «1.5 ГБ», 512 МБ → «512 МБ».
  const fmtRam = (mb: number) => mb >= 1024 ? `${+(mb / 1024).toFixed(2)} ГБ` : `${mb} МБ`

  let editRAM = $state(srv.ramMB)
  let editJvmArgs = $state(srv.jvmArgs || '')
  let editProgramArgs = $state(srv.programArgs || '')
  let editJavaMajor = $state(srv.javaMajor)
  let editPort = $state(srv.port || 25565)
  // Яке саме поле зараз зберігається (для кнопки-галочки біля нього).
  let savingKey = $state('')
  let coreModalOpen = $state(false)

  // ── Жива перевірка порту (вільний/зайнятий) ─────────────────────────
  let portCheck = $state<{ free: boolean; holder?: string } | null>(null)
  let portChecking = $state(false)
  let portSeq = 0
  $effect(() => {
    const id = srv.id
    const p = Number(editPort)
    if (!Number.isFinite(p) || p < 1 || p > 65534 || isVelocity) { portCheck = null; return }
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

  // ── Повзунок RAM ──
  const ramPct = $derived(((editRAM - ramMin) / (ramMax - ramMin)) * 100)
  function ramFromPoint(e: PointerEvent | MouseEvent, el: HTMLElement): number {
    const r = el.getBoundingClientRect()
    let pct = (e.clientX - r.left) / r.width
    pct = Math.max(0, Math.min(1, pct))
    return Math.max(ramMin, Math.min(ramMax, Math.round((ramMin + pct * (ramMax - ramMin)) / ramStep) * ramStep))
  }
  let ramDragging = $state(false)
  function ramTrackClick(e: MouseEvent) { editRAM = ramFromPoint(e, e.currentTarget as HTMLElement) }
  function ramDragStart(e: PointerEvent) {
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId)
    ramDragging = true
    editRAM = ramFromPoint(e, e.currentTarget as HTMLElement)
  }
  function ramDragMove(e: PointerEvent) { if (ramDragging) editRAM = ramFromPoint(e, e.currentTarget as HTMLElement) }
  function ramDragStop() { ramDragging = false }

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
  const dirtyPort = $derived(!isVelocity && Number(editPort) !== Number(srv.port || 25565))
  // Скільки полів змінено — для пігулки в шапці.
  const dirtyCount = $derived([dirtyRAM, dirtyJvm, dirtyProgram, dirtyJava, dirtyPort].filter(Boolean).length)

  async function saveOne(field: 'ram' | 'jvm' | 'program' | 'java' | 'port') {
    const patch: Partial<ServerConfigPatch> = {}
    switch (field) {
      case 'ram':
        if (!dirtyRAM) return
        if (editRAM < ramMin) { toast(t('servers.settings.ramMin'), 'error'); return }
        patch.ramMB = editRAM
        break
      case 'jvm':
        if (!dirtyJvm) return
        patch.jvmArgs = editJvmArgs
        break
      case 'program':
        if (!dirtyProgram) return
        patch.programArgs = editProgramArgs
        break
      case 'java':
        if (!dirtyJava) return
        patch.javaMajor = editJavaMajor
        break
      case 'port': {
        if (!dirtyPort) return
        const p = Number(editPort)
        if (!Number.isInteger(p) || p < 1 || p > 65534) { toast(t('servers.settings.portInvalid'), 'error'); return }
        if (portCheck && !portCheck.free) { toast(t('servers.settings.portBusy', { holder: portCheck.holder || '—' }), 'error'); return }
        if (isRunning) { toast(t('servers.settings.portStopFirst'), 'error'); return }
        patch.port = p
        break
      }
    }

    savingKey = field
    try {
      const updated = await App.SaveServerConfig(srv.id, patch)
      // ПОРТ: джерело правди — server.properties. Якщо не дописати
      // server-port туди, StartServer прочитає СТАРЕ значення з файлу і
      // зміна в UI «зникне» при наступному старті.
      if (field === 'port') {
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
      savingKey = ''
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
  <!-- ═══ ШАПКА РОЗДІЛУ + СТАН СЕРВЕРА ═══ -->
  <div class="sst-head">
    <div class="sst-head-ic"><i class="ti ti-adjustments-horizontal"></i></div>
    <div class="sst-head-t">
      <h1>{t('servers.settings.title')}</h1>
      <p>{t('servers.settings.subtitle')}</p>
    </div>
    <span class="sst-state" class:run={isRunning}>
      <span class="sst-state-dot"></span>
      {isRunning ? t('servers.settings.stateRunning') : t('servers.settings.stateStopped')}
    </span>
  </div>

  {#if dirtyCount > 0}
    <div class="sst-unsaved">
      <i class="ti ti-info-circle"></i>
      {t('servers.settings.unsaved', { n: dirtyCount })}
    </div>
  {/if}

  <!-- ═══ БАЗОВІ ПАРАМЕТРИ (сітка як у макеті) ═══ -->
  <section class="sst-sec">
    <div class="sst-sec-h"><i class="ti ti-cpu"></i> {t('servers.settings.basic')}</div>

    <div class="sst-grid">
      <!-- ЯДРО + ВЕРСІЯ -->
      <div class="sst-field" class:dirty={false}>
        <label for="srv-core">{t('servers.settings.core')}</label>
        <div class="sst-inline">
          <div class="sst-core-line" id="srv-core">
            {#if coreMeta?.icon}<img src={coreMeta.icon} alt="" class="sst-core-ico" />{:else}<i class="ti ti-cpu-2"></i>{/if}
            <b>{srv.coreLabel || coreMeta?.label || srv.core}</b>
            <span class="mono">{srv.mcVersion || (isVelocity ? 'proxy' : '')}</span>
          </div>
          <button class="sst-mini" onclick={() => coreModalOpen = true} disabled={isRunning} data-testid="server-core-change-btn"
                  title={isRunning ? t('servers.core.running') : t('servers.settings.coreChange')}>
            <i class="ti ti-replace"></i> {t('servers.settings.coreChange')}
          </button>
        </div>
        <div class="sst-hint">
          {#if srv.coreInstalled}
            <span class="ok"><i class="ti ti-check"></i> {t('servers.settings.coreInstalled')}</span>
          {:else}
            <span class="warn"><i class="ti ti-download"></i> {t('servers.settings.coreMissing')}</span>
          {/if}
        </div>
      </div>

      <!-- JAVA -->
      <div class="sst-field" class:dirty={dirtyJava}>
        <label for="srv-java">Java</label>
        <div class="sst-inline">
          <SelectField
            value={String(editJavaMajor)}
            options={javaOptions}
            onchange={(v) => { editJavaMajor = Number(v) }}
            ariaLabel="Java"
            minWidth="100%"
          />
          <button class="sst-save-one" class:busy={savingKey === 'java'} disabled={savingKey === 'java' || !dirtyJava}
                  onclick={() => saveOne('java')} title={t('servers.settings.save')} aria-label={t('servers.settings.save')}>
            {#if savingKey === 'java'}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-check"></i>{/if}
          </button>
        </div>
        <div class="sst-hint">{t('servers.settings.javaDesc')}</div>
        {#if srv.javaInstalled === false}
          <div class="sst-hint warn"><i class="ti ti-alert-triangle"></i> {t('servers.settings.javaNotInstalled')}</div>
        {/if}
      </div>

      <!-- ПОРТ -->
      <div class="sst-field" class:dirty={dirtyPort}>
        <label for="srv-port">{t('servers.settings.port')}</label>
        {#if isVelocity}
          <div class="sst-port-ro mono"><i class="ti ti-world"></i> :{srv.port || 25577}</div>
          <div class="sst-hint">{t('servers.settings.portVelocity')}</div>
        {:else}
          <div class="sst-inline">
            <input id="srv-port" class="input mono" type="number" min="1" max="65534" bind:value={editPort} data-testid="server-port-input" />
            <button class="sst-save-one" class:busy={savingKey === 'port'} disabled={savingKey === 'port' || !dirtyPort}
                    onclick={() => saveOne('port')} title={t('servers.settings.save')} aria-label={t('servers.settings.save')}>
              {#if savingKey === 'port'}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-check"></i>{/if}
            </button>
          </div>
          <div class="sst-hint" class:ok={portCheck?.free === true} class:bad={!!portCheck && !portCheck.free}>
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
        {/if}
      </div>

      <!-- RAM (широка, як у макеті) -->
      <div class="sst-field span-2" class:dirty={dirtyRAM}>
        <div class="sst-field-head">
          <label for="srv-ram">{t('servers.settings.ram')}</label>
          <span class="sst-ram-val mono">{fmtRam(editRAM)}</span>
          <button class="sst-save-one" class:busy={savingKey === 'ram'} disabled={savingKey === 'ram' || !dirtyRAM}
                  onclick={() => saveOne('ram')} title={t('servers.settings.save')} aria-label={t('servers.settings.save')}>
            {#if savingKey === 'ram'}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-check"></i>{/if}
          </button>
        </div>
        <div class="ram-track" id="srv-ram" role="slider" tabindex="0" aria-label={t('servers.settings.ram')}
             aria-valuemin={ramMin} aria-valuemax={ramMax} aria-valuenow={editRAM}
             onclick={ramTrackClick} onpointerdown={ramDragStart} onpointermove={ramDragMove} onpointerup={ramDragStop} onpointercancel={ramDragStop}
             onkeydown={(e) => { if (e.key === 'ArrowRight' || e.key === 'ArrowUp') { e.preventDefault(); editRAM = Math.min(ramMax, editRAM + ramStep) } else if (e.key === 'ArrowLeft' || e.key === 'ArrowDown') { e.preventDefault(); editRAM = Math.max(ramMin, editRAM - ramStep) } }}>
          <div class="ram-fill" style="width:{ramPct}%"></div>
          <div class="ram-knob" style="left:{ramPct}%"></div>
        </div>
        <div class="sst-ram-scale mono"><span>{fmtRam(ramMin)}</span><span>{fmtRam(ramMax)}</span></div>
        <div class="sst-hint">{t('servers.settings.ramDesc')}</div>
      </div>

      <!-- АРГУМЕНТИ ЗАПУСКУ (блоками, на всю ширину) -->
      <div class="sst-field span-3" class:dirty={dirtyJvm || dirtyProgram}>
        <div class="sst-field-head">
          <label>{t('servers.settings.args')}</label>
        </div>

        <div class="sst-args">
          <div class="sst-arg-block">
            <div class="sst-arg-head">
              <span>{t('servers.settings.jvmArgs')}</span>
              <button class="sst-save-one sm" class:busy={savingKey === 'jvm'} disabled={savingKey === 'jvm' || !dirtyJvm}
                      onclick={() => saveOne('jvm')} title={t('servers.settings.save')} aria-label={t('servers.settings.save')}>
                {#if savingKey === 'jvm'}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-check"></i>{/if}
              </button>
            </div>
            <JvmArgsEditor value={editJvmArgs} onchange={(v: string) => editJvmArgs = v} />
            <div class="sst-hint">{t('servers.settings.jvmArgsDesc')}</div>
          </div>

          <div class="sst-arg-block">
            <div class="sst-arg-head">
              <span>{t('servers.settings.programArgs')}</span>
              <button class="sst-save-one sm" class:busy={savingKey === 'program'} disabled={savingKey === 'program' || !dirtyProgram}
                      onclick={() => saveOne('program')} title={t('servers.settings.save')} aria-label={t('servers.settings.save')}>
                {#if savingKey === 'program'}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-check"></i>{/if}
              </button>
            </div>
            <JvmArgsEditor value={editProgramArgs} onchange={(v: string) => editProgramArgs = v} placeholder="--nogui" />
            <div class="sst-hint">{t('servers.settings.programArgsDesc')}</div>
          </div>
        </div>
        <div class="sst-hint">{t('servers.settings.argsDesc')}</div>
      </div>
    </div>
  </section>

  <!-- ═══ server.properties — окрема сторінка, відкривається кнопкою ═══ -->
  {#if !isVelocity}
    <section class="sst-sec sst-props-cta">
      <div class="sst-danger-row">
        <div class="sst-danger-t">
          <b><i class="ti ti-adjustments-horizontal"></i> {t('servers.settings.propsCta.title')}</b>
          <span>{t('servers.settings.propsCta.desc')}</span>
        </div>
        <button class="btn btn-primary btn-sm" onclick={onOpenProperties} data-testid="server-properties-open"><i class="ti ti-arrow-right"></i> {t('servers.settings.propsCta.open')}</button>
      </div>
    </section>
  {/if}

  <!-- ═══ НЕБЕЗПЕЧНА ЗОНА ═══ -->
  <section class="sst-sec danger">
    <div class="sst-sec-h"><i class="ti ti-alert-triangle"></i> {t('servers.settings.danger')}</div>
    <div class="sst-danger-row">
      <div class="sst-danger-t">
        <b>{t('servers.settings.delete')}</b>
        <span>{t('servers.settings.deleteDesc')}</span>
      </div>
      <button class="btn btn-danger btn-sm" onclick={del} data-testid="server-delete-btn"><i class="ti ti-trash"></i> {t('delete')}</button>
    </div>
  </section>
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
  .sst { display: flex; flex-direction: column; gap: 16px; padding-bottom: 24px; min-width: 0; }
  .sst > :global(*) { min-width: 0; }

  /* ── Шапка розділу ── */
  .sst-head { display: flex; align-items: center; gap: 14px; flex-wrap: wrap; }
  .sst-head-ic {
    width: 42px; height: 42px; flex-shrink: 0; border-radius: 13px; display: grid; place-items: center; font-size: 21px;
    color: var(--pcol, var(--orange));
    background: color-mix(in srgb, var(--pcol, var(--orange)) 13%, transparent);
    border: 1px solid color-mix(in srgb, var(--pcol, var(--orange)) 28%, transparent);
  }
  .sst-head-t { flex: 1; min-width: 0; }
  .sst-head-t h1 { margin: 0; font: 750 19px/1.2 var(--font-ui); letter-spacing: -.02em; color: var(--text); }
  .sst-head-t p { margin: 3px 0 0; font: 400 12px/1.4 var(--font-ui); color: var(--text-mute); }
  .sst-state {
    display: inline-flex; align-items: center; gap: 7px; flex-shrink: 0;
    padding: 7px 13px; border-radius: 999px;
    background: var(--bg-card); border: 1px solid var(--border);
    font: 650 11.5px/1 var(--font-ui); color: var(--text-mute);
  }
  .sst-state-dot { width: 7px; height: 7px; border-radius: 50%; background: var(--red); }
  .sst-state.run { color: #22c55e; border-color: color-mix(in srgb, #22c55e 35%, var(--border)); background: color-mix(in srgb, #22c55e 8%, var(--bg-card)); }
  .sst-state.run .sst-state-dot { background: #22c55e; box-shadow: 0 0 8px rgba(34, 197, 94, .8); }

  .sst-unsaved {
    display: flex; align-items: center; gap: 8px; padding: 9px 13px; border-radius: 11px;
    font: 600 11.5px/1.3 var(--font-ui); color: var(--pcol, var(--orange));
    background: color-mix(in srgb, var(--pcol, var(--orange)) 8%, transparent);
    border: 1px solid color-mix(in srgb, var(--pcol, var(--orange)) 28%, transparent);
  }

  /* ── Секція-картка ── */
  .sst-sec { padding: 18px; border-radius: var(--r-lg, 16px); background: var(--bg-card); border: 1px solid var(--border); }
  .sst-sec.danger { border-color: rgba(239, 68, 68, .25); }
  .sst-sec-h {
    display: flex; align-items: center; gap: 8px; margin-bottom: 15px;
    font: 700 11.5px/1 var(--font-ui); letter-spacing: .1em; text-transform: uppercase; color: var(--text);
  }
  .sst-sec-h i { font-size: 16px; color: var(--pcol, var(--orange)); }
  .sst-sec.danger .sst-sec-h { color: var(--red); }
  .sst-sec.danger .sst-sec-h i { color: var(--red); }

  /* ── Сітка полів: 3 колонки (як у макеті), поля можуть займати 2–3 ── */
  .sst-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 14px; }
  .sst-field {
    display: flex; flex-direction: column; gap: 7px; min-width: 0;
    padding: 13px 14px; border-radius: 13px;
    background: var(--bg-input); border: 1px solid var(--border);
    transition: border-color .15s;
  }
  .sst-field.dirty { border-color: color-mix(in srgb, var(--pcol, var(--orange)) 45%, var(--border)); background: color-mix(in srgb, var(--pcol, var(--orange)) 5%, var(--bg-input)); }
  .sst-field.span-2 { grid-column: span 2; }
  .sst-field.span-3 { grid-column: span 3; }
  .sst-field > label, .sst-field-head > label {
    font: 650 11px/1 var(--font-ui); letter-spacing: .06em; text-transform: uppercase; color: var(--text-mute);
  }
  .sst-field-head { display: flex; align-items: center; gap: 8px; }
  .sst-field-head > label { flex: 1; }
  .sst-hint { display: flex; align-items: center; gap: 5px; flex-wrap: wrap; font: 400 10.5px/1.45 var(--font-ui); color: var(--text-dim); }
  .sst-hint.ok { color: var(--green); }
  .sst-hint.bad { color: var(--red); }
  .sst-hint.warn { color: var(--yellow); }
  .sst-hint .ok { display: inline-flex; align-items: center; gap: 4px; color: var(--green); }
  .sst-hint .warn { display: inline-flex; align-items: center; gap: 4px; color: var(--yellow); }
  .sst-hint.mono { font-family: var(--font-mono); }

  .sst-inline { display: flex; gap: 8px; align-items: center; }
  .sst-inline .input, .sst-inline > :global(.sf) { flex: 1; min-width: 0; }

  .sst-core-line { flex: 1; min-width: 0; display: flex; align-items: center; gap: 8px; font: 650 13px/1.25 var(--font-ui); color: var(--text); }
  .sst-core-line .mono { font: 500 11px/1 var(--font-mono); color: var(--text-mute); white-space: nowrap; }
  .sst-core-ico { width: 20px; height: 20px; object-fit: contain; }
  .sst-core-line i { color: var(--pcol, var(--orange)); }

  /* Кнопка-збереження БІЛЯ КОЖНОГО налаштування. */
  .sst-save-one {
    width: 30px; height: 30px; flex-shrink: 0;
    display: inline-flex; align-items: center; justify-content: center;
    border-radius: 8px; cursor: pointer; font-size: 14px;
    background: color-mix(in srgb, #22c55e 14%, transparent);
    color: #22c55e; border: 1px solid color-mix(in srgb, #22c55e 34%, transparent);
    transition: background .14s, color .14s, opacity .14s, transform .12s;
  }
  .sst-save-one:hover:not(:disabled) { background: #22c55e; color: #06230f; transform: scale(1.05); }
  .sst-save-one:disabled { opacity: .3; cursor: default; }
  .sst-save-one.busy { opacity: .8; }
  .sst-save-one.sm { width: 24px; height: 24px; font-size: 12px; border-radius: 6px; }
  .sst-mini {
    display: inline-flex; align-items: center; gap: 6px; flex-shrink: 0; cursor: pointer;
    padding: 8px 12px; border-radius: 9px; white-space: nowrap;
    background: var(--hover-strong); border: 1px solid var(--border); color: var(--text-mute);
    font: 650 11.5px/1 var(--font-ui);
  }
  .sst-mini:hover:not(:disabled) { color: var(--text); border-color: var(--border-hi); }
  .sst-mini:disabled { opacity: .45; cursor: default; }

  .sst-port-ro { display: inline-flex; align-items: center; gap: 6px; font: 600 12px/1 var(--font-mono); color: var(--text-mute); padding: 7px 11px; border-radius: 9px; background: var(--bg-card); border: 1px solid var(--border); }
  .sst-port-ro i { color: var(--text-dim); }

  /* ── RAM ── */
  .sst-ram-val { font: 750 13px/1 var(--font-mono); color: var(--pcol, var(--orange)); white-space: nowrap; }
  .sst-ram-scale { display: flex; justify-content: space-between; font: 500 10px/1 var(--font-mono); color: var(--text-dim); margin-top: -1px; }
  .ram-track { position: relative; height: 6px; border-radius: 4px; background: var(--hover-strong); cursor: pointer; margin: 9px 0 4px; }
  .ram-fill { position: absolute; inset: 0 auto 0 0; border-radius: 4px; background: linear-gradient(90deg, var(--pcol, var(--orange)), color-mix(in srgb, var(--pcol, var(--orange)) 55%, #fff)); }
  .ram-knob {
    position: absolute; top: 50%; width: 16px; height: 16px; border-radius: 50%; transform: translate(-50%, -50%);
    background: #fff; border: 3px solid var(--pcol, var(--orange)); box-shadow: 0 2px 8px rgba(0, 0, 0, .4);
  }

  /* ── Аргументи (блоки) ── */
  .sst-args { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(320px, 100%), 1fr)); gap: 12px; }
  .sst-arg-block { display: flex; flex-direction: column; gap: 7px; min-width: 0; padding: 12px; border-radius: 11px; background: var(--bg-card); border: 1px solid var(--border); }
  .sst-arg-head { display: flex; align-items: center; gap: 8px; }
  .sst-arg-head > span { flex: 1; font: 650 12px/1 var(--font-ui); color: var(--text); }

  /* ── Небезпечна зона ── */
  .sst-danger-row { display: flex; align-items: center; gap: 14px; flex-wrap: wrap; }
  .sst-danger-t { flex: 1; min-width: 200px; display: flex; flex-direction: column; gap: 3px; }
  .sst-danger-t b { font: 650 13px/1.2 var(--font-ui); color: var(--text); }
  .sst-props-cta .sst-danger-t b { display: flex; align-items: center; gap: 7px; }
  .sst-props-cta .sst-danger-t b i { color: var(--pcol, var(--orange)); font-size: 16px; }
  .sst-danger-t span { font: 400 11px/1.4 var(--font-ui); color: var(--text-mute); }

  @media (max-width: 900px) {
    .sst-grid { grid-template-columns: minmax(0, 1fr); }
    .sst-field.span-2, .sst-field.span-3 { grid-column: span 1; }
  }
</style>
