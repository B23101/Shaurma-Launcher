<script lang="ts">
  import Portal from './Portal.svelte'
  // Перейменування Шаурма-акаунта (неліцензійного): змінює ID/UUID акаунта
  // (вони рахуються з ніка), переносить прив'язки збірок і пресетів
  // гардероба та, якщо активна Google-сесія скін-системи мала той самий
  // старий нік, пробує переназвати й скін-нік на сервері — див.
  // App.RenameOfflineAccount у backend/app.go.
  import { App } from './wails'
  import { t } from './i18n.svelte'
  import { pirateNicknameError, sanitizeNick } from './validation'

  let {
    open = false,
    currentNick = '',
    accountId = '',
    onrenamed = null as null | ((newAccountId: string, skinWarning: string) => void),
    oncancel = null as null | (() => void),
  }: {
    open?: boolean
    currentNick?: string
    accountId?: string
    onrenamed?: null | ((newAccountId: string, skinWarning: string) => void)
    oncancel?: null | (() => void)
  } = $props()

  let nick = $state('')
  let touched = $state(false)
  let busy = $state(false)
  let serverErr = $state('')
  const nickErr = $derived(pirateNicknameError(nick.trim()))

  async function submit() {
    if (busy) return
    const trimmed = nick.trim()
    if (!trimmed || nickErr) { touched = true; return }
    if (trimmed.toLowerCase() === currentNick.trim().toLowerCase()) {
      oncancel?.()
      return
    }
    busy = true
    serverErr = ''
    try {
      const result = await App.RenameOfflineAccount(accountId, trimmed)
      onrenamed?.(result.account.id, result.skinWarning || '')
    } catch (e: any) {
      serverErr = e?.message || String(e)
    } finally {
      busy = false
    }
  }

  // Відкрили вікно заново — підставляємо поточний нік як стартову точку
  // (зручніше правити одну літеру, ніж друкувати з нуля).
  $effect(() => {
    if (open) {
      nick = currentNick
      touched = false
      serverErr = ''
    } else {
      nick = ''; touched = false; busy = false; serverErr = ''
    }
  })
</script>

{#if open}
  <Portal>
    <div class="modal-overlay" onclick={() => !busy && oncancel?.()}>
      <div class="modal" style="max-width:420px" onclick={(e: any) => e.stopPropagation()}>
        <div class="modal-head">
          <h3>{t('acc.renameTitle')}</h3>
        </div>
        <div class="modal-body">
          <div class="rename-hint"><i class="ti ti-info-circle"></i>
            <span>{t('acc.renameHint')}</span>
          </div>
          <div class="wiz-field-row" style="margin-top:12px">
            <!-- svelte-ignore a11y_autofocus -->
            <input class="input mono {nickErr && touched ? 'input-err' : ''}" style="flex:1"
                   placeholder={t('nickname')} value={nick} maxlength="16" autofocus
                   oninput={(e: any) => { nick = sanitizeNick(e.target.value); touched = true; serverErr = '' }}
                   onkeydown={(e: any) => e.key === 'Enter' && submit()}
                   aria-invalid={!!(nickErr && touched)} />
          </div>
          {#if nickErr && touched}
            <div class="field-err"><i class="ti ti-alert-triangle"></i> {nickErr}</div>
          {:else if serverErr}
            <div class="field-err"><i class="ti ti-alert-triangle"></i> {serverErr}</div>
          {/if}
        </div>
        <div class="modal-foot">
          <button class="btn btn-ghost" onclick={oncancel} disabled={busy}>{t('cancel')}</button>
          <button class="btn btn-primary" disabled={busy || !nick.trim() || !!nickErr} onclick={submit}>
            <i class="ti {busy ? 'ti-loader-2 spin' : 'ti-check'}"></i> {t('save')}
          </button>
        </div>
      </div>
    </div>
  </Portal>
{/if}

<style>
  .rename-hint {
    display:flex; align-items:flex-start; gap:8px;
    font-size:12.5px; color:var(--text-mute); line-height:1.5;
    background:rgba(var(--orange-rgb),.07); border:1px solid rgba(var(--orange-rgb),.2);
    border-radius:10px; padding:10px 14px;
  }
  .rename-hint i { color:var(--orange); font-size:16px; flex-shrink:0; margin-top:1px; }
</style>
