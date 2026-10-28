<script lang="ts">
  import UiModal from './UiModal.svelte'
  // RAM-попередження перед запуском (ТЗ). Показується, коли:
  //   • showRecommended — ефективна RAM нижча за рекомендовану збірки;
  //   • showLowRam — виділена RAM перевищує фізичну пам'ять системи.
  // Дії: «Скасувати» / «Запустити», слайдер RAM (per-pack override) та
  // галочка «не показувати знову для цієї збірки». Стан (ramMb/dismiss)
  // — bindable, бо зберігаються/застосовуються в батьку (App.svelte).
  import { t } from './i18n.svelte'
  import type { LaunchWarnings } from './types'

  let {
    warnings = null as null | LaunchWarnings,
    packName = '',
    ramMb = $bindable(0),
    dismiss = $bindable(false),
    onproceed = null as null | (() => void),
    oncancel = null as null | (() => void)
  }: {
    warnings?: null | LaunchWarnings
    packName?: string
    ramMb?: number
    dismiss?: boolean
    onproceed?: null | (() => void)
    oncancel?: null | (() => void)
  } = $props()

  function fmtGb(mb: number): string {
    return `${(mb / 1024).toFixed(1)} ГБ`
  }

  const rw = $derived(warnings)
  const ramMax = $derived(rw ? Math.max(16384, Math.max(rw.availableRamMb, rw.recommendedRamMb)) : 16384)
</script>

{#if rw}
  <UiModal
    open={true}
    size="md"
    title={t('ramWarning.title', { pack: packName })}
    icon="ti-alert-triangle"
    color="var(--yellow)"
    shake={true}
    onclose={() => oncancel?.()}
  >
        <div style="display:flex;flex-direction:column;gap:14px">
          {#if rw.showLowRam}
            <div class="rw-card" style="border-left:3px solid var(--red)">
              <div class="rw-title" style="color:var(--red)"><i class="ti ti-device-desktop"></i> {t('ramWarning.lowRamTitle')}</div>
              <div class="rw-text">{t('ramWarning.lowRamText', { ram: fmtGb(rw.effectiveRamMb), phys: fmtGb(rw.availableRamMb) })}</div>
            </div>
          {/if}
          {#if rw.showRecommended}
            <div class="rw-card" style="border-left:3px solid var(--yellow)">
              <div class="rw-title" style="color:var(--yellow)"><i class="ti ti-trophy"></i> {t('ramWarning.recTitle')}</div>
              <div class="rw-text">{t('ramWarning.recText', { ram: fmtGb(rw.effectiveRamMb), rec: fmtGb(rw.recommendedRamMb) })}</div>
            </div>
          {/if}

          <!-- Слайдер RAM: змінює RAM саме ЦІЄЇ збірки (SetPackRAM). Коли
               користувач піднімає значення вище рекомендованого/фізичного —
               попередження більше не з'явиться, а збірка отримає СВОЮ RAM
               (override), не зачіпаючи загальні налаштування (ТЗ). -->
          <div class="rw-card" style="border-left:3px solid var(--blue)">
            <div class="rw-title"><i class="ti ti-cpu"></i> {t('ramWarning.ramLabel')} <span style="color:var(--text);font-weight:700">{fmtGb(ramMb)}</span></div>
            <input type="range" min="1024" max="{ramMax}" step="256"
                   value={ramMb} oninput={(e: any) => ramMb = Number(e.target.value)}
                   style="width:100%;accent-color:var(--orange);margin-top:6px" />
            <div style="display:flex;justify-content:space-between;font-size:var(--fs-min);color:var(--text-mute);margin-top:2px">
              <span>1 ГБ</span><span>{Math.round(ramMax / 1024)} ГБ</span>
            </div>
          </div>

          <label class="cb-dont" style="display:flex;align-items:center;gap:8px;font-size:11.5px;color:var(--text-mute);cursor:pointer;user-select:none">
            <input type="checkbox" checked={dismiss} onchange={() => dismiss = !dismiss} style="accent-color:var(--orange)" />
            {t('ramWarning.dontShow')}
          </label>
        </div>
    {#snippet footer()}
      <button class="btn btn-ghost" onclick={oncancel}><i class="ti ti-x"></i> {t('cancel')}</button>
      <button class="btn btn-primary" onclick={onproceed}>
        <i class="ti ti-player-play"></i> {t('ramWarning.launch')}
      </button>
    {/snippet}
  </UiModal>
{/if}

<style>
  .rw-card {
    background: var(--bg-input); border: 1px solid var(--border);
    border-radius: var(--r-md); padding: 12px 14px;
  }
  .rw-title {
    display: flex; align-items: center; gap: 7px;
    font-size: 12.5px; font-weight: 700; margin-bottom: 6px;
  }
  .rw-title i { font-size: 14px; }
  .rw-text { font-size: 12px; color: var(--text-mute); line-height: 1.6; }
</style>
