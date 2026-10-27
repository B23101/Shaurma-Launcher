<script lang="ts">
  // ── ErrorReportsConsentModal: згода на надсилання звітів про помилки ───
  // Показується після входу (один раз — прапорець ErrorReportsPromptShown
  // у БД, той самий принцип, що LauncherMigrationModal/MigrateDismiss).
  // «Так» → App.ErrorReportsSetConsent(true), «Не зараз» →
  // App.ErrorReportsSetConsent(false). Обидві дії ставлять
  // ErrorReportsPromptShown = true, тож вікно більше не спливає — перемикач
  // лишається доступним у Налаштуваннях (settings.errorReportsEnabled).
  //
  // Публічний контракт: open + onClose(enabled). App.svelte викликає
  // onClose після запису вибору в БД і показує наступну модалку черги
  // (міграція збірок), якщо вона є.
  import UiModal from './UiModal.svelte'
  import { t } from './i18n.svelte'
  import { App } from './wails'

  let { open, onClose } = $props<{
    open: boolean
    onClose: (enabled: boolean) => void
  }>()

  let busy = $state(false)

  async function choose(enabled: boolean) {
    if (busy) return
    busy = true
    try {
      await App.ErrorReportsSetConsent(enabled)
    } catch { /* якщо запис не вдався — все одно не блокуємо чергу вікон */ }
    busy = false
    onClose(enabled)
  }
</script>

{#if open}
  <UiModal
    open={true}
    size="sm"
    title={t('errorReports.title')}
    subtitle={t('errorReports.subtitle')}
    icon="ti-bug"
    color="var(--orange)"
    locked={busy}
    onclose={() => choose(false)}
  >
    <div style="font-size:13px;color:var(--text-mute);line-height:1.6">{t('errorReports.body')}</div>
    {#snippet footer()}
      <button class="btn btn-ghost" disabled={busy} onclick={() => choose(false)}>{t('errorReports.decline')}</button>
      <button class="btn btn-primary" disabled={busy} onclick={() => choose(true)}>
        <i class="ti ti-check"></i> {t('errorReports.accept')}
      </button>
    {/snippet}
  </UiModal>
{/if}
