<script lang="ts">
  import UiModal from './UiModal.svelte'
  import './settings/settings.css'
  // Перейменування Шаурма-акаунта (неліцензійного): змінює ID/UUID акаунта
  // (вони рахуються з ніка), переносить прив'язки збірок і пресетів
  // гардероба та, якщо активна Google-сесія скін-системи мала той самий
  // старий нік, пробує переназвати й скін-нік на сервері — див.
  // App.RenameOfflineAccount у backend/app.go.
  import { App } from './wails'
  import { t } from './i18n.svelte'
  import { pirateNicknameError, sanitizeNick } from './validation'
  import Callout from './Callout.svelte'

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

<UiModal {open} onclose={() => oncancel?.()} locked={busy} size="sm" icon="ti-pencil" title={t('acc.renameTitle')}>
  <Callout>{t('acc.renameHint')}</Callout>
  <div class="wiz-field-row" style="margin-top:12px">
    <!-- svelte-ignore a11y_autofocus -->
    <input class="st-input mono grow {nickErr && touched ? 'input-err' : ''}"
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
  {#snippet footer()}
    <button class="btn btn-ghost" onclick={oncancel} disabled={busy}>{t('cancel')}</button>
    <button class="btn btn-primary" disabled={busy || !nick.trim() || !!nickErr} onclick={submit}>
      <i class="ti {busy ? 'ti-loader-2 spin' : 'ti-check'}"></i> {t('save')}
    </button>
  {/snippet}
</UiModal>

<style>
</style>
