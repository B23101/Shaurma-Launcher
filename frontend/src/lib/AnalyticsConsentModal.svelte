<script lang="ts">
  import UiModal from './UiModal.svelte'
  // Модалка згоди на анонімну статистику — показується ОДИН раз, при першому
  // запуску (коли вже є акаунт). Це opt-in: поки користувач не натисне
  // «Допомогти», лаунчер не надсилає жодного запиту (backend/analytics.go
  // виходить на першій же перевірці AnalyticsEnabled).
  //
  // Свідомо НЕ закривається хрестиком/кліком по фону: обидві кнопки явні, бо
  // «закрив випадково» = стан згоди не зафіксовано, і питання спливе знову.
  import { t } from './i18n.svelte'

  let {
    open = false,
    busy = false,
    ondecide = null as null | ((enabled: boolean) => void)
  }: {
    open?: boolean
    busy?: boolean
    ondecide?: null | ((enabled: boolean) => void)
  } = $props()
</script>

<UiModal {open} dismissible={false} icon="ti-chart-histogram" title={t('analytics.title')} width="520px">
  <p class="ac-lead">{@html t('analytics.lead')}</p>
  <ul class="ac-list">
    <li><i class="ti ti-clock-play"></i> {@html t('analytics.what.usage')}</li>
    <li><i class="ti ti-shield-lock"></i> {@html t('analytics.what.nick')}</li>
    <li><i class="ti ti-ban"></i> {@html t('analytics.what.never')}</li>
  </ul>
  <div class="ac-note">
    <i class="ti ti-info-circle"></i>
    <span>{t('analytics.note')}</span>
  </div>
  {#snippet footer()}
    <button class="btn btn-ghost" disabled={busy} onclick={() => ondecide?.(false)}>
      {t('analytics.no')}
    </button>
    <button class="btn btn-primary" disabled={busy} onclick={() => ondecide?.(true)}>
      {#if busy}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-heart-handshake"></i>{/if}
      {t('analytics.yes')}
    </button>
  {/snippet}
</UiModal>

<style>
  .ac-lead { font-size:13px; line-height:1.6; color:var(--text); margin:0 0 12px; }
  .ac-list { list-style:none; margin:0 0 14px; padding:0; display:flex; flex-direction:column; gap:9px; }
  .ac-list li {
    display:flex; align-items:flex-start; gap:9px;
    font-size:12.5px; line-height:1.5; color:var(--text-mute);
  }
  .ac-list i { color:var(--orange); font-size:15px; margin-top:1px; flex-shrink:0; }
  .ac-note {
    display:flex; align-items:flex-start; gap:8px;
    font-size:11.5px; line-height:1.5; color:var(--text-mute);
    background:rgba(255,255,255,.03); border:1px solid var(--border);
    border-radius:10px; padding:10px 12px;
  }
  .ac-note i { color:var(--orange); font-size:14px; margin-top:1px; flex-shrink:0; }
</style>
