<script lang="ts">
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
    { value: 'desktop', label: t('shortcut.locationDesktop') },
    { value: 'startmenu', label: t('shortcut.locationStartMenu') },
    { value: 'custom', label: t('shortcut.locationCustom') },
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
      <label class="sc-field">
        <span class="sc-label">{t('shortcut.name')}</span>
        <input class="sc-input" bind:value={name} placeholder={packName} maxlength="80" disabled={busy} />
      </label>

      <span class="sc-label">{t('shortcut.location')}</span>
      <div class="sc-locations">
        {#each locations as loc (loc.value)}
          <button
            class="sc-loc"
            class:sel={location === loc.value}
            onclick={() => location = loc.value}
            disabled={busy}
          >
            <i class="ti {loc.value === 'desktop' ? 'ti-monitor' : loc.value === 'startmenu' ? 'ti-layout-grid' : 'ti-folder'}"></i>
            {loc.label}
          </button>
        {/each}
      </div>

      {#if location === 'custom'}
        <div class="sc-custom">
          <input class="sc-input mono" bind:value={customDir} placeholder="C:\..." disabled={busy} />
          <button class="btn btn-ghost" onclick={browse} disabled={busy} title={t('shortcut.browse')}>
            <i class="ti ti-folder-open"></i>
          </button>
        </div>
      {/if}

      <span class="sc-label">{t('shortcut.optionsHint')}</span>
      <label class="sc-field">
        <span class="sc-label">{t('shortcut.account')}</span>
        <select class="sc-input" bind:value={accountId} disabled={busy || accounts.length === 0}>
          <option value="">{t('shortcut.accountDefault')}</option>
          {#each accounts as acc (acc.id)}
            <option value={acc.id}>{acc.username}{acc.isLicensed ? '' : ` (${t('shortcut.accountOffline')})`}</option>
          {/each}
        </select>
      </label>
      <label class="sc-field">
        <span class="sc-label">{t('shortcut.autoJoin')}</span>
        <input class="sc-input mono" bind:value={autoJoinServer} placeholder="play.example.com:25565" disabled={busy} />
      </label>

      <div class="sc-note"><i class="ti ti-info-circle"></i> {t('shortcut.optionsNote')}</div>

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
  .sc-body { display: flex; flex-direction: column; gap: 8px; }

  .sc-field { display: flex; flex-direction: column; gap: 5px; }
  .sc-label { font-size: 11.5px; font-weight: 700; color: var(--text-mute); }
  .sc-input {
    width: 100%; padding: 8px 11px; font-size: 12.5px; font-family: inherit;
    background: var(--bg-input); color: var(--text); border: 1px solid var(--border);
    border-radius: var(--r-sm); outline: none; box-sizing: border-box;
  }
  .sc-input:focus { border-color: var(--violet); }

  .sc-locations { display: flex; gap: 6px; flex-wrap: wrap; }
  .sc-loc {
    display: inline-flex; align-items: center; gap: 6px;
    padding: 7px 12px; border-radius: 999px; border: 1px solid var(--border);
    background: var(--bg-input); color: var(--text-mute);
    font-size: 11.5px; font-weight: 700; font-family: inherit; cursor: pointer;
    transition: all .15s;
  }
  .sc-loc i { font-size: 13px; }
  .sc-loc.sel { background: rgba(157,124,255,.12); border-color: rgba(157,124,255,.45); color: var(--violet-l); }
  .sc-loc:disabled { opacity: .55; cursor: default; }

  .sc-custom { display: flex; gap: 6px; }
  .sc-custom .sc-input { flex: 1; }

  .sc-error {
    display: flex; align-items: center; gap: 7px;
    padding: 8px 11px; font-size: 11.5px; color: #ef4444;
    background: rgba(239,68,68,.08); border: 1px solid rgba(239,68,68,.25);
    border-radius: var(--r-sm); word-break: break-word;
  }

  .sc-note {
    display: flex; align-items: flex-start; gap: 7px;
    padding: 8px 11px; font-size: 11px; line-height: 1.45;
    color: var(--text-mute);
    background: color-mix(in srgb, var(--violet) 8%, transparent);
    border: 1px solid color-mix(in srgb, var(--violet) 22%, transparent);
    border-radius: var(--r-sm);
  }
  .sc-note i { flex-shrink: 0; margin-top: 1px; color: var(--violet-l); }

  .sc-foot { display: flex; justify-content: flex-end; gap: 8px; padding: 10px 18px 16px; border-top: 1px solid var(--border); }
</style>
