<script lang="ts">
  import { tip as launcherTip } from './friends/tooltip'

  // ── ShortcutDialog: гнучке створення ярлика збірки ─────────────────────
  // Порт діалогу CreateShortcutDialog з Prism: назва ярлика + місце
  // збереження (робочий стіл / меню «Пуск» / довільна тека — "Other...") +
  // персональні опції запуску ярлика (акаунт, автоприєднання до сервера).
  // Ці опції перекривають акаунт/автоприєднання ЗБІРКИ рівно для запуску з
  // цього ярлика (як у Prism); решта параметрів — як при звичайному
  // запуску. Іконка завжди береться зі збірки; запуск — shell.exe
  // --launch <packID> [--account <id>] [--server <host>]. Після успішного
  // створення — onCreate(шлях до .lnk).

  import { t } from './i18n.svelte'
  import { App } from './wails'
  import UiModal from './UiModal.svelte'
  import SelectField from './SelectField.svelte'
  import type { DesktopShortcut, Account } from './types'

  let { packId, packName, open, accounts, onClose, onCreate } = $props<{
    packId: string
    packName: string
    open: boolean
    accounts: Account[]
    onClose: () => void
    onCreate?: (sc: DesktopShortcut) => void
  }>()

  let name = $state('')
  let location = $state('desktop') // desktop | startmenu | custom
  let customDir = $state('')
  let accountId = $state('') // порожній = активний акаунт лаунчера
  let autoJoinServer = $state('')
  let busy = $state(false)
  let error = $state('')

  // Передзаповнення при відкритті: якщо ярлик для збірки вже є — показуємо
  // його назву/місце/персональні опції (акаунт/сервер), щоб діалог працював
  // і як «редагування» ярлика (як у Prism), а не лише створення.
  $effect(() => {
    if (open) {
      name = packName
      location = 'desktop'
      customDir = ''
      accountId = ''
      autoJoinServer = ''
      busy = false
      error = ''
      App.GetDesktopShortcut(packId)
        .then(sc => {
          if (!sc?.exists || !sc.path) return
          // Назва ярлика = ім'я .lnk без розширення (щоб редагування не
          // скидало назву, яку юзер задав при створенні).
          const base = sc.path.split(/[\\/]/).pop() ?? ''
          if (base.toLowerCase().endsWith('.lnk')) {
            name = base.slice(0, -4)
          }
          // Місце: визначаємо за шляхом (desktop / startmenu / довільна
          // тека) — щоб збереження не «переїхало» ярлик в інше місце.
          const lower = sc.path.toLowerCase()
          if (lower.includes('\\desktop\\') || lower.includes('/desktop/')) location = 'desktop'
          else if (lower.includes('start menu\\programs') || lower.includes('start menu/programs')) location = 'startmenu'
          else { location = 'custom'; customDir = sc.path.replace(/[\\/][^\\/]*$/, '') }
          accountId = sc.accountId || ''
          autoJoinServer = sc.autoJoinServer || ''
        })
        .catch(() => {})
    }
  })

  // Голови акаунтів для списку (той самий кеш, що в сайдбарі й консолі).
  let heads = $state<Record<string, string>>({})
  $effect(() => {
    if (!open) return
    const accs: Account[] = accounts ?? []
    let cancelled = false
    ;(async () => {
      const next: Record<string, string> = {}
      await Promise.all(accs.map(async (acc) => {
        try {
          const url = await App.GetAccountHead(acc.uuid || '', acc.username, !!acc.isLicensed)
          if (url) next[acc.id] = url
        } catch { /* без голови — іконка за замовчуванням */ }
      }))
      if (!cancelled) heads = next
    })()
    return () => { cancelled = true }
  })

  const accountOptions = $derived([
    { value: '', label: t('shortcut.accountDefault'), icon: 'ti-user-check' },
    ...(accounts ?? []).map((acc: Account) => ({
      value: acc.id,
      label: acc.username + (acc.isLicensed ? '' : ` (${t('shortcut.accountOffline')})`),
      head: heads[acc.id],
      icon: 'ti-user',
    })),
  ])

  async function create() {
    busy = true
    error = ''
    try {
      const target = location === 'custom' ? customDir : location
      const sc = await App.CreateShortcut(packId, {
        name: name.trim(),
        location: target,
        accountId: accountId || undefined,
        autoJoinServer: autoJoinServer.trim() || undefined,
      })
      onCreate?.(sc)
      onClose()
    } catch (e: any) {
      error = String(e?.message ?? e)
    } finally {
      busy = false
    }
  }

  async function browse() {
    const dir = await App.BrowseFolder()
    if (dir) customDir = dir
  }

  const locations = [
    { value: 'desktop', label: t('shortcut.locationDesktop'), icon: 'ti-device-desktop' },
    { value: 'startmenu', label: t('shortcut.locationStartMenu'), icon: 'ti-layout-grid' },
    { value: 'custom', label: t('shortcut.locationCustom'), icon: 'ti-folder' },
  ]
</script>

{#if open}
  <UiModal
    open={true}
    size="sm"
    title={t('shortcut.title')}
    icon="ti-external-link"
    color="var(--orange)"
    locked={busy}
    onclose={onClose}
  >
    <div class="sc-body">
      <section class="sc-sec">
        <div class="sc-label"><i class="ti ti-pencil"></i> {t('shortcut.name')}</div>
        <div class="sc-name">
          <input class="input" bind:value={name} placeholder={packName} maxlength="80" disabled={busy} />
          <span class="sc-ext mono">.lnk</span>
        </div>
      </section>

      <section class="sc-sec">
        <div class="sc-label"><i class="ti ti-map-pin"></i> {t('shortcut.location')}</div>
        <div class="sc-locations" role="radiogroup" aria-label={t('shortcut.location')}>
          {#each locations as loc (loc.value)}
            <button
              class="sc-loc"
              class:sel={location === loc.value}
              role="radio"
              aria-checked={location === loc.value}
              onclick={() => location = loc.value}
              disabled={busy}
            >
              <i class="ti {loc.icon}"></i>
              <span>{loc.label}</span>
            </button>
          {/each}
        </div>
        {#if location === 'custom'}
          <div class="sc-custom">
            <input class="input mono" bind:value={customDir} placeholder="C:\..." disabled={busy} />
            <button class="btn btn-ghost" onclick={browse} disabled={busy} use:launcherTip={t('shortcut.browse')}>
              <i class="ti ti-folder-open"></i>
            </button>
          </div>
        {/if}
      </section>

      <section class="sc-card">
        <div class="sc-card-head"><i class="ti ti-player-play"></i> {t('shortcut.optionsHint')}</div>
        <div class="sc-sec">
          <div class="sc-sub">{t('shortcut.account')}</div>
          <SelectField
            value={accountId}
            options={accountOptions}
            onchange={(v) => accountId = v}
            disabled={busy || accounts.length === 0}
            minWidth="100%"
            ariaLabel={t('shortcut.account')}
          />
        </div>
        <div class="sc-sec">
          <div class="sc-sub">{t('shortcut.autoJoin')}</div>
          <div class="sc-iconfield">
            <i class="ti ti-server-2"></i>
            <input class="input mono" bind:value={autoJoinServer} placeholder="play.example.com:25565" disabled={busy} />
          </div>
        </div>
        <div class="sc-note"><i class="ti ti-info-circle"></i> {t('shortcut.optionsNote')}</div>
      </section>

      {#if error}
        <div class="sc-error"><i class="ti ti-alert-triangle"></i> {error}</div>
      {/if}
    </div>
    {#snippet footer()}
      <button class="btn btn-ghost" onclick={onClose} disabled={busy}>{t('cancel')}</button>
      <button class="btn btn-primary" onclick={create} disabled={busy || !name.trim() || (location === 'custom' && !customDir.trim())}>
        {#if busy}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-external-link"></i>{/if}
        {busy ? t('shortcut.creating') : t('shortcut.create')}
      </button>
    {/snippet}
  </UiModal>
{/if}

<style>
  .sc-body { display: flex; flex-direction: column; gap: 16px; }
  .sc-sec { display: flex; flex-direction: column; gap: 8px; }

  .sc-label {
    display: flex; align-items: center; gap: 7px;
    font-size: 11px; font-weight: 800; letter-spacing: .04em; text-transform: uppercase;
    color: var(--text-mute);
  }
  .sc-label i { font-size: 14px; color: var(--orange); }
  .sc-sub { font-size: 11.5px; font-weight: 700; color: var(--text-mute); }

  .sc-name { position: relative; }
  .sc-name .input { padding-right: 46px; font-weight: 700; }
  .sc-ext {
    position: absolute; right: 12px; top: 50%; transform: translateY(-50%);
    font-size: 11.5px; color: var(--text-dim); pointer-events: none;
  }

  .sc-locations { display: grid; grid-template-columns: repeat(3, 1fr); gap: 8px; }
  .sc-loc {
    display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 7px;
    padding: 13px 6px 11px; border-radius: var(--r-md);
    border: 1px solid var(--border); background: var(--bg-input);
    color: var(--text-mute); font-family: inherit; font-size: 11.5px; font-weight: 700;
    line-height: 1.2; text-align: center; cursor: pointer;
    transition: border-color .15s, background .15s, color .15s, transform .15s;
  }
  .sc-loc i { font-size: 20px; transition: transform .2s cubic-bezier(.34,1.56,.64,1), color .15s; }
  .sc-loc:hover:not(:disabled) { border-color: var(--border-hi); color: var(--text); }
  .sc-loc:hover:not(:disabled) i { transform: translateY(-1px); }
  .sc-loc.sel {
    color: var(--text);
    border-color: rgba(var(--orange-rgb), .5);
    background: linear-gradient(180deg, rgba(var(--orange-rgb), .16), rgba(var(--orange-rgb), .06));
    box-shadow: 0 6px 18px -10px rgba(var(--orange-rgb), .7);
  }
  .sc-loc.sel i { color: var(--orange); }
  .sc-loc:focus-visible { outline: 2px solid rgba(var(--orange-rgb), .55); outline-offset: 2px; }
  .sc-loc:disabled { opacity: .55; cursor: default; }

  .sc-custom { display: flex; gap: 6px; }
  .sc-custom .input { flex: 1; }

  .sc-card {
    display: flex; flex-direction: column; gap: 12px;
    padding: 14px; border-radius: var(--r-md);
    background: color-mix(in srgb, var(--bg-input) 70%, transparent);
    border: 1px solid var(--border);
  }
  .sc-card-head {
    display: flex; align-items: center; gap: 8px;
    font-size: 12.5px; font-weight: 800; color: var(--text);
  }
  .sc-card-head i { color: var(--orange); font-size: 15px; }

  .sc-iconfield { position: relative; }
  .sc-iconfield > i {
    position: absolute; left: 12px; top: 50%; transform: translateY(-50%);
    font-size: 15px; color: var(--text-mute); pointer-events: none;
  }
  .sc-iconfield .input { padding-left: 36px; }

  .sc-note {
    display: flex; align-items: flex-start; gap: 7px;
    font-size: 11px; line-height: 1.45; color: var(--text-dim);
  }
  .sc-note i { flex-shrink: 0; margin-top: 1px; font-size: 13px; }

  .sc-error {
    display: flex; align-items: center; gap: 7px;
    padding: 8px 11px; font-size: 11.5px; color: #ef4444;
    background: rgba(239,68,68,.08); border: 1px solid rgba(239,68,68,.25);
    border-radius: var(--r-sm); word-break: break-word;
  }
</style>
