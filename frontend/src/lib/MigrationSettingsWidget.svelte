<script lang="ts">
  // ── MigrationSettingsWidget: віджет міграції у Налаштуваннях ──────────
  // Показує, чи знайдено інші лаунчери (Prism / Modrinth App), і відкриває
  // модалку міграції. На відміну від пост-логін вікна, dismissOnClose=false
  // — віджет не ставить «ми показали» і може використовуватись скільки
  // завгодно разів (ТЗ: «якщо гравець мігрував і захоче ще — може це
  // зробити у налаштуваннях»).

  import { onMount } from 'svelte'
  import { t } from './i18n.svelte'
  import { App } from './wails'
  import type { MigrateStatus } from './types'
  import LauncherMigrationModal from './LauncherMigrationModal.svelte'

  let status = $state<MigrateStatus | null>(null)
  let open = $state(false)

  onMount(async () => {
    try {
      status = await App.MigrateStatus()
    } catch { status = null }
  })

  function refresh() {
    App.MigrateStatus().then((s) => (status = s)).catch(() => {})
  }

  const launcherNames = $derived((status?.launchers ?? []).map((l) => l.name).join(' / '))
</script>

<div class="card mig-widget">
  <div class="card-head">
    <div class="ci orange"><i class="ti ti-arrows-shuffle"></i></div>
    <div class="ctw">
      <div class="ct">{t('migrate.settingsWidgetTitle')}</div>
      <div class="cd">
        {#if status && status.foundLaunchers > 0}
          {t('migrate.settingsWidgetDesc', { n: status.foundLaunchers })} <span class="mw-names">{launcherNames}</span>
        {:else}
          {t('migrate.settingsWidgetNone')}
        {/if}
      </div>
    </div>
    {#if status && status.foundLaunchers > 0}
      <button class="btn btn-primary btn-sm" onclick={() => open = true}>
        <i class="ti ti-arrows-right-left"></i> {t('migrate.settingsWidgetOpen')}
      </button>
    {/if}
  </div>
</div>

{#if open}
  <LauncherMigrationModal
    open={open}
    dismissOnClose={false}
    onClose={() => { open = false; refresh() }}
  />
{/if}

<style>
  .mig-widget .cd { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
  .mw-names { font-weight: 700; color: var(--text); }
</style>
