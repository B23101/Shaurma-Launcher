<script lang="ts">
  // ── Світи сервера + бекапи ────────────────────────────────────────────
  // Список світів (з server.properties level-name), для кожного — бекапи:
  // створити zip, відновити (бекенд сам робить pre-restore копію), видалити.
  import { App, on } from './wails'
  import { toast, confirmDialog } from './toast.svelte'
  import { slide } from 'svelte/transition'
  import SelectField from './SelectField.svelte'
  import type { WorldInfo, BackupInfo, ServerBackupConfig, ServerBackupProgress, ServerView } from './types'

  let {
    serverId = '',
    running = false,
  }: {
    serverId?: string
    running?: boolean
  } = $props()

  let worlds = $state<WorldInfo[]>([])
  let loading = $state(true)
  let backups = $state<Record<string, BackupInfo[]>>({})
  let expanded = $state<Record<string, boolean>>({})
  // busy — світ, для якого ЗАРАЗ пишеться бекап (і ручний, і авто). Джерело —
  // подія server:backup-progress, а не локальний прапорець: тоді індикатор
  // чесний навіть для авто-бекапу (таймер / зупинка / старт сервера).
  let busy = $state('')
  // backupProgress — останній крок створення бекапу (прогрес-бар).
  let backupProgress = $state<ServerBackupProgress | null>(null)
  // flash — світ, для якого бекап щойно завершився (підсвітка рядка).
  let flash = $state('')

  // ── Авто-бекапи (налаштування періодичних копій) ─────────────────────
  // Раніше BackupConfig існував лише в структурі даних: жодного таймера
  // й жодного UI для нього не було («авто-бекапи» — мертвий код). Тепер
  // тут: інтервал, ліміт копій на добу, скільки лишити і скільки днів
  // зберігати — і все це зберігається ОКРЕМОЮ карткою поруч.
  let autoCfg = $state<ServerBackupConfig | null>(null)
  let autoSaved = $state<ServerBackupConfig | null>(null)
  let cfgSaving = $state(false)

  const cfgDirty = $derived(!!autoCfg && JSON.stringify(autoCfg) !== JSON.stringify(autoSaved))
  // Картка авто-бекапу за замовчуванням згорнута до одного рядка-підсумку:
  // налаштовують її раз, а місце на екрані потрібне світам і копіям.
  let autoOpen = $state(false)
  function cancelAutoCfg() { if (autoSaved) autoCfg = { ...autoSaved } }

  const intervalOptions = [
    { value: '15', label: 'Кожні 15 хвилин' },
    { value: '30', label: 'Кожні 30 хвилин' },
    { value: '60', label: 'Кожну годину' },
    { value: '120', label: 'Кожні 2 години' },
    { value: '240', label: 'Кожні 4 години' },
    { value: '480', label: 'Кожні 8 годин' },
    { value: '720', label: 'Кожні 12 годин' },
    { value: '1440', label: 'Раз на добу' },
  ]
  const noLimit = { value: '0', label: 'Без ліміту' }
  const maxPerDayOptions = [
    noLimit,
    ...[1, 2, 3, 4, 6, 8, 12, 24, 48].map(n => ({ value: String(n), label: `${n} ${n === 1 ? 'копія' : n < 5 ? 'копії' : 'копій'} на добу` })),
  ]
  const keepCountOptions = [
    noLimit,
    ...[3, 5, 10, 20, 50, 100].map(n => ({ value: String(n), label: `Лишити ${n} останніх` })),
  ]
  const keepDaysOptions = [
    noLimit,
    ...[1, 3, 7, 14, 30, 90, 180].map(n => ({ value: String(n), label: `Видаляти старші за ${n} дн.` })),
  ]

  async function loadAutoCfg() {
    if (!serverId) return
    try {
      const srv: ServerView = await App.GetServer(serverId)
      const base = srv.autoBackup ?? {
        enabled: false, intervalMin: 60, keepCount: 10, keepDays: 7, maxPerDay: 0, worldDir: '',
        backupOnStop: false, backupOnStart: false,
      }
      autoCfg = { ...base, intervalMin: base.intervalMin || 60 }
      autoSaved = { ...autoCfg }
    } catch { autoCfg = null; autoSaved = null }
  }

  async function saveAutoCfg() {
    if (!autoCfg || cfgSaving) return
    cfgSaving = true
    try {
      await App.SaveServerConfig(serverId, { autoBackup: { ...autoCfg } })
      autoSaved = { ...autoCfg }
      toast('Налаштування авто-бекапів збережено', 'success')
    } catch (e: any) { toast(String(e), 'error') }
    finally { cfgSaving = false }
  }

  async function loadWorlds() {
    loading = true
    try { worlds = (await App.ListWorlds(serverId)) ?? [] } catch (e: any) { toast(String(e), 'error'); worlds = [] }
    finally { loading = false }
  }

  async function loadBackups(world: string) {
    try {
      backups[world] = (await App.ListWorldBackups(serverId, world)) ?? []
    } catch { backups[world] = [] }
  }

  // loadAllBackups — бекапи ВСІХ світів одразу. Потрібно, щоб «швидке
  // відновлення» було видно з першого погляду, а не лише після розкриття
  // кожного світу (саме на це скарга «не бачу бекапи прямо в інтерфейсі»).
  async function loadAllBackups() {
    const list = worlds
    for (const w of list) {
      // послідовно — не плодимо десятки паралельних викликів на великому
      // сервері з багатьма світами
      await loadBackups(w.name)
    }
  }

  // totalBackups — скільки копій усього (для заголовка вкладки).
  const totalBackups = $derived(Object.values(backups).reduce((n, list) => n + (list?.length ?? 0), 0))

  $effect(() => {
    void serverId
    loadWorlds().then(() => loadAllBackups())
    loadAutoCfg()
  })

  // ── Живий прогрес створення бекапів ─────────────────────────────────
  // Одне джерело правди для ручного й авто-бекапу: бекенд шле
  // server:backup-progress на кожному кроці (preparing → packing → done).
  // Раніше ручний бекап великого світу виглядав як «кнопка натиснута і
  // нічого не відбувається», а авто-бекап був узагалі невидимий.
  $effect(() => {
    const id = serverId
    if (!id) return
    const off = on('server:backup-progress', (p: any) => {
      if (!p || p.serverId !== id) return
      const ev = p as ServerBackupProgress
      backupProgress = ev
      if (ev.stage === 'packing' || ev.stage === 'preparing') {
        busy = ev.world
        return
      }
      busy = ''
      if (ev.stage === 'done') {
        void loadBackups(ev.world)
        flash = ev.world
        setTimeout(() => { if (flash === ev.world) flash = '' }, 3500)
        const label = ev.auto ? `Авто-бекап світу «${ev.world}» створено` : `Бекап світу «${ev.world}» створено`
        toast(label, 'success')
      } else if (ev.stage === 'error') {
        toast(`Бекап світу «${ev.world}» не вдався: ${ev.error || 'невідома помилка'}`, 'error')
      }
      // Прогрес-бар тримаємо ще трохи після завершення — щоб було видно
      // «100%» і легенду, а не миттєве зникнення смуги.
      setTimeout(() => { if (backupProgress === ev) backupProgress = null }, 5000)
    })
    return off
  })

  async function toggleWorld(world: string) {
    expanded[world] = !expanded[world]
    if (expanded[world] && !backups[world]) await loadBackups(world)
  }

  async function createBackup(world: string) {
    if (busy) return
    busy = world
    backupProgress = { serverId, world, stage: 'preparing', files: 0, done: 0, percent: -1, message: 'Підготовка…' }
    try {
      await App.CreateWorldBackup(serverId, world)
      // Тост про успіх показує сама подія server:backup-progress (stage=done),
      // щоб не дублювати повідомлення при швидкому бекапі.
      await loadBackups(world)
    } catch (e: any) {
      toast(String(e), 'error')
    } finally {
      busy = ''
    }
  }

  async function restoreBackup(world: string, b: BackupInfo) {
    if (running) {
      toast('Спочатку зупиніть сервер — відновлення світу під живим процесом неможливе', 'error')
      return
    }
    const proceed = await confirmDialog(
      `Відновити світ «${world}»?`,
      `Поточний стан світу буде замінено бекапом від ${fmtDate(b.created)}. Перед відновленням бекенд автоматично створить копію поточного стану (pre-restore).`,
      { confirmLabel: 'Відновити', cancelLabel: 'Скасувати', danger: true },
    )
    if (!proceed) return
    try {
      await App.RestoreWorldBackup(serverId, world, b.id)
      toast(`Світ «${world}» відновлено з бекапу`, 'success')
    } catch (e: any) { toast(String(e), 'error') }
  }

  async function deleteBackup(world: string, b: BackupInfo) {
    const proceed = await confirmDialog(
      'Видалити бекап?',
      `Бекап «${b.name}» буде видалено назавжди.`,
      { confirmLabel: 'Видалити', cancelLabel: 'Скасувати', danger: true },
    )
    if (!proceed) return
    try {
      await App.DeleteWorldBackup(serverId, world, b.id)
      backups[world] = (backups[world] ?? []).filter(x => x.id !== b.id)
      toast('Бекап видалено', 'success')
    } catch (e: any) { toast(String(e), 'error') }
  }

  function fmtSize(mb: number): string {
    if (mb >= 1024) return `${(mb / 1024).toFixed(1)} ГБ`
    if (mb >= 1) return `${mb.toFixed(1)} МБ`
    return `${Math.max(1, Math.round(mb * 1024))} КБ`
  }
  function fmtDate(iso: string): string {
    if (!iso) return ''
    const d = new Date(iso)
    if (isNaN(d.getTime())) return iso
    return d.toLocaleString('uk-UA', { day: '2-digit', month: '2-digit', year: 'numeric', hour: '2-digit', minute: '2-digit' })
  }

  function plural(n: number, f: [string, string, string]): string {
    const m10 = n % 10, m100 = n % 100
    if (m10 === 1 && m100 !== 11) return f[0]
    if (m10 >= 2 && m10 <= 4 && (m100 < 10 || m100 >= 20)) return f[1]
    return f[2]
  }
  // «4 дні тому» читається швидше за «14.10.2026, 16:48» — головне, що людині
  // треба знати про бекап: наскільки він свіжий.
  function fmtAgo(iso: string): string {
    const ts = new Date(iso).getTime()
    if (isNaN(ts)) return ''
    const s = Math.max(0, (Date.now() - ts) / 1000)
    if (s < 60) return 'щойно'
    const m = Math.floor(s / 60)
    if (m < 60) return `${m} ${plural(m, ['хвилину', 'хвилини', 'хвилин'])} тому`
    const h = Math.floor(m / 60)
    if (h < 24) return `${h} ${plural(h, ['годину', 'години', 'годин'])} тому`
    const d = Math.floor(h / 24)
    if (d < 30) return `${d} ${plural(d, ['день', 'дні', 'днів'])} тому`
    const mo = Math.floor(d / 30)
    if (mo < 12) return `${mo} ${plural(mo, ['місяць', 'місяці', 'місяців'])} тому`
    const y = Math.floor(d / 365)
    return `${y} ${plural(y, ['рік', 'роки', 'років'])} тому`
  }
  function fmtDay(iso: string): string {
    const d = new Date(iso)
    if (isNaN(d.getTime())) return iso
    return d.toLocaleString('uk-UA', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })
  }

  const sortedBackups = (world: string): BackupInfo[] =>
    [...(backups[world] ?? [])].sort((a, b) => new Date(b.created).getTime() - new Date(a.created).getTime())
  const maxBackupSize = (world: string): number =>
    Math.max(0.001, ...(backups[world] ?? []).map(b => b.sizeMB))
  const totalBackupsMB = $derived(
    Object.values(backups).reduce((n, list) => n + (list ?? []).reduce((s, b) => s + b.sizeMB, 0), 0),
  )

  // Підсумок розкладу — той рядок, який видно у згорнутій картці.
  const autoSummary = $derived.by(() => {
    if (!autoCfg) return ''
    if (!autoCfg.enabled) return 'Вимкнено'
    const parts = [intervalOptions.find(o => o.value === String(autoCfg!.intervalMin))?.label ?? `Кожні ${autoCfg.intervalMin} хв`]
    if (autoCfg.keepCount) parts.push(`лишати ${autoCfg.keepCount}`)
    if (autoCfg.backupOnStop) parts.push('при зупинці')
    if (autoCfg.backupOnStart) parts.push('при запуску')
    return parts.join(' · ')
  })

</script>

<div class="swt">
  <!-- ── Шапка ── -->
  <header class="swt-head">
    <span class="swt-head-ic"><i class="ti ti-world"></i></span>
    <div class="swt-head-txt">
      <h3>Світи та бекапи</h3>
      <p>
        {worlds.length} {plural(worlds.length, ['світ', 'світи', 'світів'])}
        · {totalBackups} {plural(totalBackups, ['копія', 'копії', 'копій'])}
        {#if totalBackupsMB > 0}· {fmtSize(totalBackupsMB)}{/if}
      </p>
    </div>
    <button class="swt-ibtn" onclick={loadWorlds} title="Оновити список світів" aria-label="Оновити список світів">
      <i class="ti ti-refresh" class:spin={loading}></i>
    </button>
  </header>

  {#if running}
    <div class="swt-banner" role="status">
      <i class="ti ti-player-play-filled"></i>
      <span><b>Сервер працює.</b> Бекап створити можна, а відновити — лише після зупинки сервера.</span>
    </div>
  {/if}

  <!-- ── Авто-бекап: один рядок, налаштування розгортаються ── -->
  {#if autoCfg}
    <section class="swt-auto" class:on={autoCfg.enabled}>
      <div class="swt-auto-top">
        <button class="swt-auto-main" onclick={() => (autoOpen = !autoOpen)} aria-expanded={autoOpen}>
          <span class="swt-auto-ic"><i class="ti ti-clock-play"></i></span>
          <span class="swt-auto-txt">
            <b>Авто-бекап</b>
            <small>{autoSummary}</small>
          </span>
          <i class="ti ti-chevron-down swt-chev" class:open={autoOpen}></i>
        </button>
        <div class="toggle" class:on={autoCfg.enabled} role="switch" aria-checked={autoCfg.enabled}
             aria-label="Увімкнути авто-бекапи" tabindex="0"
             onclick={() => (autoCfg!.enabled = !autoCfg!.enabled)}
             onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); autoCfg!.enabled = !autoCfg!.enabled } }}></div>
      </div>

      {#if autoOpen}
        <div class="swt-auto-body" transition:slide={{ duration: 180 }}>
          <div class="swt-grid" class:dim={!autoCfg.enabled}>
            <label class="swt-field">
              <span>Як часто</span>
              <SelectField value={String(autoCfg.intervalMin)} options={intervalOptions}
                           onchange={(v) => (autoCfg!.intervalMin = Number(v))} minWidth="100%"
                           ariaLabel="Інтервал авто-бекапів" disabled={!autoCfg.enabled} />
            </label>
            <label class="swt-field">
              <span>Максимум на добу</span>
              <SelectField value={String(autoCfg.maxPerDay)} options={maxPerDayOptions}
                           onchange={(v) => (autoCfg!.maxPerDay = Number(v))} minWidth="100%"
                           ariaLabel="Ліміт авто-бекапів на добу" disabled={!autoCfg.enabled} />
            </label>
            <label class="swt-field">
              <span>Скільки зберігати</span>
              <SelectField value={String(autoCfg.keepCount)} options={keepCountOptions}
                           onchange={(v) => (autoCfg!.keepCount = Number(v))} minWidth="100%"
                           ariaLabel="Скільки бекапів лишити" disabled={!autoCfg.enabled} />
            </label>
            <label class="swt-field">
              <span>Видаляти старі</span>
              <SelectField value={String(autoCfg.keepDays)} options={keepDaysOptions}
                           onchange={(v) => (autoCfg!.keepDays = Number(v))} minWidth="100%"
                           ariaLabel="Видаляти бекапи старші за N днів" disabled={!autoCfg.enabled} />
            </label>
          </div>

          <!-- Ключові моменти: зупинка дає консистентний знімок (Java вже закрила
               регіони), старт — «як було до сесії». -->
          <div class="swt-opts" class:dim={!autoCfg.enabled}>
            <div class="swt-opt">
              <span class="swt-opt-ic"><i class="ti ti-player-stop"></i></span>
              <span class="swt-opt-txt"><b>При зупинці сервера</b><small>Найнадійніший знімок світу</small></span>
              <div class="toggle" class:on={!!autoCfg.backupOnStop} role="switch" aria-checked={!!autoCfg.backupOnStop}
                   aria-label="Бекап при зупинці сервера" tabindex="0"
                   onclick={() => (autoCfg!.backupOnStop = !autoCfg!.backupOnStop)}
                   onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); autoCfg!.backupOnStop = !autoCfg!.backupOnStop } }}></div>
            </div>
            <div class="swt-opt">
              <span class="swt-opt-ic"><i class="ti ti-player-play"></i></span>
              <span class="swt-opt-txt"><b>При запуску сервера</b><small>Стан «як було до сесії»</small></span>
              <div class="toggle" class:on={!!autoCfg.backupOnStart} role="switch" aria-checked={!!autoCfg.backupOnStart}
                   aria-label="Бекап при запуску сервера" tabindex="0"
                   onclick={() => (autoCfg!.backupOnStart = !autoCfg!.backupOnStart)}
                   onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); autoCfg!.backupOnStart = !autoCfg!.backupOnStart } }}></div>
            </div>
          </div>
          <p class="swt-note"><i class="ti ti-info-circle"></i> Перший бекап — через інтервал після старту. Копії лежать у <b>.shaurma/backups</b> поруч зі світом.</p>
        </div>
      {/if}

      {#if cfgDirty}
        <div class="swt-savebar" transition:slide={{ duration: 150 }}>
          <span><i class="ti ti-point-filled"></i> Є незбережені зміни</span>
          <div>
            <button class="btn btn-ghost btn-sm" onclick={cancelAutoCfg} disabled={cfgSaving}>Скасувати</button>
            <button class="btn btn-primary btn-sm" onclick={saveAutoCfg} disabled={cfgSaving}>
              {#if cfgSaving}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-device-floppy"></i>{/if} Зберегти
            </button>
          </div>
        </div>
      {/if}
    </section>
  {/if}

  <!-- ── Живий прогрес створення бекапу (ручний і авто) ── -->
  {#if backupProgress}
    <div class="swt-prog" class:err={backupProgress.stage === 'error'} class:ok={backupProgress.stage === 'done'} transition:slide={{ duration: 150 }}>
      <div class="swt-prog-head">
        {#if backupProgress.stage === 'done'}
          <i class="ti ti-circle-check-filled"></i>
        {:else if backupProgress.stage === 'error'}
          <i class="ti ti-alert-triangle-filled"></i>
        {:else}
          <i class="ti ti-loader spin"></i>
        {/if}
        <b>{backupProgress.world}</b>
        {#if backupProgress.auto}<span class="swt-chip">авто</span>{/if}
        <span class="swt-prog-msg">
          {#if backupProgress.stage === 'error'}
            {backupProgress.error || 'помилка'}
          {:else if backupProgress.stage === 'done'}
            Готово · {backupProgress.files} {plural(backupProgress.files, ['файл', 'файли', 'файлів'])}
          {:else}
            {backupProgress.message || 'Пакування…'}
          {/if}
        </span>
        {#if backupProgress.percent >= 0}<span class="swt-prog-pct">{backupProgress.percent}%</span>{/if}
      </div>
      <div class="swt-prog-bar">
        {#if backupProgress.percent >= 0}
          <div class="swt-prog-fill" style="width:{backupProgress.percent}%"></div>
        {:else}
          <div class="swt-prog-fill swt-prog-indet"></div>
        {/if}
      </div>
    </div>
  {/if}

  <!-- ── Світи ── -->
  {#if loading}
    <div class="swt-state"><i class="ti ti-loader spin"></i> Завантаження…</div>
  {:else if worlds.length === 0}
    <div class="swt-state">
      <span class="swt-state-ic"><i class="ti ti-world-off"></i></span>
      <b>Світів ще немає</b>
      <span class="swt-hint">Світ зʼявиться тут після першого запуску сервера (level-name з server.properties).</span>
    </div>
  {:else}
    <div class="swt-list">
      {#each worlds as w (w.name)}
        {@const list = sortedBackups(w.name)}
        {@const latest = list[0]}
        <article class="swt-world" class:open={expanded[w.name]} class:flash={flash === w.name}>
          <div class="swt-row">
            <button class="swt-main" onclick={() => toggleWorld(w.name)} aria-expanded={!!expanded[w.name]}>
              <span class="swt-w-ic"><i class="ti ti-map-2"></i></span>
              <span class="swt-info">
                <span class="swt-name">
                  {w.name}
                  {#if w.name.toLowerCase() === 'world'}<span class="swt-chip ok">основний</span>{/if}
                </span>
                <span class="swt-meta">{fmtSize(w.sizeMB)} · змінено {fmtAgo(w.modified)}</span>
              </span>
              {#if backups[w.name] !== undefined}
                <span class="swt-last" class:none={!latest} title={latest ? `Останній бекап: ${fmtDate(latest.created)}` : 'Бекапів цього світу ще немає'}>
                  {#if latest}
                    <i class="ti ti-shield-check"></i> {fmtAgo(latest.created)}
                  {:else}
                    <i class="ti ti-shield-off"></i> без бекапів
                  {/if}
                </span>
                {#if list.length > 0}<span class="swt-count" title="Бекапів цього світу">{list.length}</span>{/if}
              {/if}
              <i class="ti ti-chevron-down swt-chev" class:open={expanded[w.name]}></i>
            </button>
            <button class="btn btn-primary btn-sm swt-create" onclick={() => createBackup(w.name)} disabled={!!busy}
                    title="Створити бекап цього світу зараз">
              {#if busy === w.name}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-device-floppy"></i>{/if}
              <span>Бекап</span>
            </button>
          </div>

          {#if expanded[w.name]}
            <div class="swt-expand" transition:slide={{ duration: 180 }}>
              {#if backups[w.name] === undefined}
                <div class="swt-state small"><i class="ti ti-loader spin"></i></div>
              {:else if list.length === 0}
                <div class="swt-empty">
                  <span class="swt-state-ic"><i class="ti ti-archive-off"></i></span>
                  <b>Бекапів поки немає</b>
                  <span class="swt-hint">Створіть першу копію, перш ніж щось міняти у світі.</span>
                </div>
              {:else}
                <ol class="swt-tl">
                  {#each list as b, i (b.id)}
                    <li class="swt-bk">
                      <span class="swt-dot" class:auto={b.auto}></span>
                      <div class="swt-bk-body">
                        <div class="swt-bk-top">
                          <span class="swt-bk-when">{fmtDay(b.created)}</span>
                          <span class="swt-bk-ago">{fmtAgo(b.created)}</span>
                          {#if i === 0}<span class="swt-chip ok">найновіший</span>{/if}
                          <span class="swt-chip" class:hand={!b.auto}>{b.auto ? 'авто' : 'вручну'}</span>
                        </div>
                        <div class="swt-bk-meta">
                          <span>{fmtSize(b.sizeMB)}</span>
                          {#if b.files}<span>{b.files} {plural(b.files, ['файл', 'файли', 'файлів'])}</span>{/if}
                          <span class="swt-bk-file" title={b.name}>{b.name}</span>
                        </div>
                        <div class="swt-bar" aria-hidden="true"><span style="width:{Math.max(4, (b.sizeMB / maxBackupSize(w.name)) * 100)}%"></span></div>
                      </div>
                      <div class="swt-bk-actions">
                        <button class="btn btn-ghost btn-sm" onclick={() => restoreBackup(w.name, b)} disabled={running}
                                title={running ? 'Зупиніть сервер для відновлення' : 'Відновити світ з цього бекапу'}>
                          <i class="ti ti-history"></i> Відновити
                        </button>
                        <button class="swt-ibtn danger" onclick={() => deleteBackup(w.name, b)} title="Видалити бекап" aria-label="Видалити бекап">
                          <i class="ti ti-trash"></i>
                        </button>
                      </div>
                    </li>
                  {/each}
                </ol>
              {/if}
            </div>
          {/if}
        </article>
      {/each}
    </div>
  {/if}
</div>

<style>
  .swt { display: flex; flex-direction: column; gap: 12px; height: 100%; overflow-y: auto; padding: 2px 4px 8px 2px; }
  .swt i.spin { animation: swt-spin 1s linear infinite; }
  @keyframes swt-spin { to { transform: rotate(360deg); } }

  /* ── Шапка ── */
  .swt-head { display: flex; align-items: center; gap: 12px; }
  .swt-head-ic {
    width: 38px; height: 38px; border-radius: 12px; flex-shrink: 0; display: grid; place-items: center; font-size: 19px;
    color: var(--pcol, var(--orange));
    background: color-mix(in srgb, var(--pcol, var(--orange)) 14%, transparent);
  }
  .swt-head-txt { min-width: 0; flex: 1; }
  .swt-head-txt h3 { margin: 0; font: 800 16px/1.2 var(--font-ui); color: var(--text); }
  .swt-head-txt p { margin: 3px 0 0; font: 500 12px/1.3 var(--font-ui); color: var(--text-mute); }
  .swt-ibtn {
    width: 32px; height: 32px; border-radius: 10px; flex-shrink: 0; display: grid; place-items: center; cursor: pointer;
    border: 1px solid var(--border); background: var(--bg-card); color: var(--text-mute); font-size: 15px;
    transition: background .14s, color .14s, border-color .14s, transform .14s;
  }
  .swt-ibtn:hover { background: var(--hover); color: var(--text); border-color: var(--border-hi); }
  .swt-ibtn:active { transform: scale(.94); }
  .swt-ibtn.danger:hover { background: rgba(239,68,68,.12); color: var(--red, #ef4444); border-color: rgba(239,68,68,.35); }

  .swt-banner {
    display: flex; align-items: center; gap: 10px; padding: 10px 14px; border-radius: 12px;
    font: 500 12.5px/1.4 var(--font-ui); color: var(--text-mute);
    background: rgba(234,179,8,.08); border: 1px solid rgba(234,179,8,.22);
  }
  .swt-banner i { color: var(--yellow, #eab308); font-size: 15px; }
  .swt-banner b { color: var(--text); }

  .swt-chip {
    display: inline-flex; align-items: center; font: 700 10px/1 var(--font-ui); letter-spacing: .3px;
    padding: 3px 7px; border-radius: 999px; color: var(--text-mute); background: var(--hover);
  }
  .swt-chip.ok { color: var(--green, #22c55e); background: rgba(34,197,94,.12); }
  .swt-chip.hand { color: var(--pcol, var(--orange)); background: color-mix(in srgb, var(--pcol, var(--orange)) 12%, transparent); }

  .swt-chev { color: var(--text-dim); font-size: 15px; transition: transform .2s; flex-shrink: 0; }
  .swt-chev.open { transform: rotate(180deg); }

  /* ── Авто-бекап ── */
  .swt-auto { border: 1px solid var(--border); border-radius: 16px; background: var(--bg-card); overflow: hidden; transition: border-color .2s; }
  .swt-auto.on { border-color: color-mix(in srgb, var(--pcol, var(--orange)) 30%, var(--border)); }
  .swt-auto-top { display: flex; align-items: center; gap: 12px; padding: 4px 14px 4px 4px; }
  .swt-auto-main {
    flex: 1; min-width: 0; display: flex; align-items: center; gap: 12px; padding: 10px;
    border: 0; background: transparent; color: inherit; font: inherit; text-align: left; cursor: pointer; border-radius: 12px;
  }
  .swt-auto-main:hover { background: var(--hover); }
  .swt-auto-ic {
    width: 34px; height: 34px; border-radius: 11px; flex-shrink: 0; display: grid; place-items: center; font-size: 17px;
    color: var(--text-dim); background: var(--hover); transition: color .2s, background .2s;
  }
  .swt-auto.on .swt-auto-ic { color: var(--pcol, var(--orange)); background: color-mix(in srgb, var(--pcol, var(--orange)) 14%, transparent); }
  .swt-auto-txt { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 3px; }
  .swt-auto-txt b { font: 700 13.5px/1.2 var(--font-ui); color: var(--text); }
  .swt-auto-txt small { font: 500 12px/1.3 var(--font-ui); color: var(--text-mute); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .swt-auto-body { padding: 4px 16px 14px; display: flex; flex-direction: column; gap: 14px; border-top: 1px solid var(--border); padding-top: 14px; }
  .swt-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(190px, 1fr)); gap: 12px; transition: opacity .15s; }
  .swt-grid.dim, .swt-opts.dim { opacity: .45; pointer-events: none; }
  .swt-field { display: flex; flex-direction: column; gap: 6px; min-width: 0; }
  .swt-field > span { font: 600 11.5px/1 var(--font-ui); color: var(--text-mute); }

  .swt-opts { display: grid; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); gap: 8px; transition: opacity .15s; }
  .swt-opt { display: flex; align-items: center; gap: 12px; padding: 10px 12px; border-radius: 12px; background: var(--bg-input); border: 1px solid var(--border); }
  .swt-opt-ic { width: 28px; height: 28px; border-radius: 9px; display: grid; place-items: center; font-size: 14px; color: var(--text-mute); background: var(--hover); flex-shrink: 0; }
  .swt-opt-txt { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 3px; }
  .swt-opt-txt b { font: 600 12.5px/1.2 var(--font-ui); color: var(--text); }
  .swt-opt-txt small { font: 400 11.5px/1.3 var(--font-ui); color: var(--text-dim); }
  .swt-note { display: flex; align-items: center; gap: 6px; margin: 0; font: 400 11.5px/1.4 var(--font-ui); color: var(--text-dim); }
  .swt-note b { font-family: var(--font-mono); font-weight: 600; color: var(--text-mute); }

  .swt-savebar {
    display: flex; align-items: center; justify-content: space-between; gap: 10px; flex-wrap: wrap;
    padding: 10px 14px; border-top: 1px solid var(--border);
    background: color-mix(in srgb, var(--pcol, var(--orange)) 7%, var(--bg-card));
  }
  .swt-savebar > span { display: inline-flex; align-items: center; gap: 4px; font: 600 12px/1 var(--font-ui); color: var(--text); }
  .swt-savebar > span i { color: var(--pcol, var(--orange)); font-size: 14px; }
  .swt-savebar > div { display: flex; gap: 8px; }

  /* ── Прогрес ── */
  .swt-prog {
    border: 1px solid color-mix(in srgb, var(--pcol, var(--orange)) 35%, var(--border)); border-radius: 14px; padding: 12px 14px;
    background: color-mix(in srgb, var(--pcol, var(--orange)) 6%, var(--bg-card)); display: flex; flex-direction: column; gap: 9px;
  }
  .swt-prog.ok { border-color: rgba(34,197,94,.35); background: rgba(34,197,94,.06); }
  .swt-prog.err { border-color: rgba(239,68,68,.35); background: rgba(239,68,68,.06); }
  .swt-prog-head { display: flex; align-items: center; gap: 8px; font: 500 12.5px/1.3 var(--font-ui); }
  .swt-prog-head i { color: var(--pcol, var(--orange)); font-size: 16px; }
  .swt-prog.ok .swt-prog-head i { color: var(--green, #22c55e); }
  .swt-prog.err .swt-prog-head i { color: var(--red, #ef4444); }
  .swt-prog-head b { color: var(--text); font-weight: 700; }
  .swt-prog-msg { flex: 1; min-width: 0; color: var(--text-mute); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .swt-prog-pct { font: 700 12px var(--font-mono); color: var(--pcol, var(--orange)); }
  .swt-prog-bar { height: 6px; border-radius: 3px; background: var(--hover); overflow: hidden; }
  .swt-prog-fill { height: 100%; border-radius: 3px; background: linear-gradient(90deg, var(--pcol, var(--orange)), color-mix(in srgb, var(--pcol, var(--orange)) 60%, #fff)); transition: width .25s ease; }
  .swt-prog.ok .swt-prog-fill { background: var(--green, #22c55e); }
  .swt-prog.err .swt-prog-fill { background: var(--red, #ef4444); }
  .swt-prog-fill.swt-prog-indet { width: 40%; animation: swt-slide 1.6s ease-in-out infinite; }
  @keyframes swt-slide { 0% { transform: translateX(-100%); } 100% { transform: translateX(350%); } }

  /* ── Стани ── */
  .swt-state, .swt-empty { display: flex; flex-direction: column; align-items: center; gap: 6px; padding: 36px 20px; color: var(--text-mute); font: 500 13px/1.4 var(--font-ui); text-align: center; }
  .swt-state.small { padding: 18px; }
  .swt-empty { padding: 26px 20px; }
  .swt-state b, .swt-empty b { color: var(--text); font-weight: 700; }
  .swt-state-ic { width: 52px; height: 52px; border-radius: 16px; display: grid; place-items: center; font-size: 24px; color: var(--text-dim); background: var(--hover); margin-bottom: 4px; }
  .swt-hint { font: 400 12px/1.4 var(--font-ui); color: var(--text-dim); max-width: 340px; }

  /* ── Світи ── */
  .swt-list { display: flex; flex-direction: column; gap: 10px; }
  .swt-world { border: 1px solid var(--border); border-radius: 16px; background: var(--bg-card); overflow: hidden; transition: border-color .2s, box-shadow .2s; }
  .swt-world:hover { border-color: var(--border-hi); }
  .swt-world.open { border-color: color-mix(in srgb, var(--pcol, var(--orange)) 28%, var(--border)); }
  .swt-world.flash { animation: swt-flash 3s ease-out; }
  @keyframes swt-flash { 0% { box-shadow: 0 0 0 3px rgba(34,197,94,.35); } 100% { box-shadow: 0 0 0 3px rgba(34,197,94,0); } }
  .swt-row { display: flex; align-items: center; gap: 8px; padding: 4px 12px 4px 4px; }
  .swt-main {
    flex: 1; min-width: 0; display: flex; align-items: center; gap: 12px; padding: 10px; border: 0; background: transparent;
    color: inherit; font: inherit; text-align: left; cursor: pointer; border-radius: 12px;
  }
  .swt-main:hover { background: var(--hover); }
  .swt-w-ic {
    width: 42px; height: 42px; border-radius: 13px; flex-shrink: 0; display: grid; place-items: center; font-size: 20px;
    color: var(--pcol, var(--orange)); background: color-mix(in srgb, var(--pcol, var(--orange)) 13%, transparent);
  }
  .swt-info { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 4px; }
  .swt-name { display: flex; align-items: center; gap: 8px; font: 700 14px/1.2 var(--font-ui); color: var(--text); min-width: 0; }
  .swt-meta { font: 500 12px/1.3 var(--font-ui); color: var(--text-mute); }
  .swt-last { display: inline-flex; align-items: center; gap: 5px; font: 600 12px/1 var(--font-ui); color: var(--green, #22c55e); white-space: nowrap; }
  .swt-last.none { color: var(--yellow, #eab308); }
  .swt-last i { font-size: 15px; }
  .swt-count { font: 700 11px/1 var(--font-mono); color: var(--text-mute); padding: 4px 8px; border-radius: 999px; background: var(--hover); }
  .swt-create { flex-shrink: 0; }

  /* ── Розгорнутий світ: таймлайн копій ── */
  .swt-expand { border-top: 1px solid var(--border); background: var(--bg-panel); padding: 8px 14px 12px; }
  .swt-tl { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; }
  .swt-bk { position: relative; display: flex; align-items: center; gap: 14px; padding: 12px 6px 12px 0; }
  .swt-bk + .swt-bk { border-top: 1px solid color-mix(in srgb, var(--border) 60%, transparent); }
  .swt-dot {
    width: 10px; height: 10px; border-radius: 50%; flex-shrink: 0; margin-left: 4px;
    background: var(--pcol, var(--orange)); box-shadow: 0 0 0 4px color-mix(in srgb, var(--pcol, var(--orange)) 18%, transparent);
  }
  .swt-dot.auto { background: var(--text-dim); box-shadow: 0 0 0 4px color-mix(in srgb, var(--text-dim) 16%, transparent); }
  .swt-bk-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 5px; }
  .swt-bk-top { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; }
  .swt-bk-when { font: 700 13.5px/1.2 var(--font-ui); color: var(--text); }
  .swt-bk-ago { font: 500 12px/1.2 var(--font-ui); color: var(--text-mute); }
  .swt-bk-meta { display: flex; align-items: center; flex-wrap: wrap; gap: 4px 12px; font: 500 12px/1.3 var(--font-ui); color: var(--text-mute); min-width: 0; }
  .swt-bk-file { font: 400 11px/1.3 var(--font-mono); color: var(--text-dim); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; min-width: 0; max-width: 100%; }
  .swt-bar { height: 3px; border-radius: 2px; background: var(--hover); overflow: hidden; max-width: 260px; }
  .swt-bar span { display: block; height: 100%; border-radius: 2px; background: color-mix(in srgb, var(--pcol, var(--orange)) 70%, transparent); }
  .swt-bk-actions { display: flex; align-items: center; gap: 6px; flex-shrink: 0; }

  @media (max-width: 760px) {
    .swt-last { display: none; }
    .swt-create span { display: none; }
    .swt-bk { flex-wrap: wrap; }
    .swt-bk-actions { width: 100%; justify-content: flex-end; }
  }
  @media (prefers-reduced-motion: reduce) {
    .swt *, .swt *::before, .swt *::after { animation-duration: .01ms !important; transition-duration: .01ms !important; }
  }
</style>
