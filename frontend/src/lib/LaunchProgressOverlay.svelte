<script lang="ts">
  // ── LaunchProgressOverlay: мінімальне вікно прогресу запуску ───────────
  // Коли лаунчер запущено з ярлика збірки (--launch <packID>), замість
  // повного інтерфейсу показуємо ТІЛЬКИ прогрес запуску: іконка + назва
  // збірки + прогрес-бар + стадія. Користувач бачить «вікно прогресу
  // запуску» без усього лаунчера навколо (як просили: «замість вікна
  // лаунчера показати вікно прогресу запуску без повного запуску вікна
  // лаунчера»). Коли гра стартує (game:started) або запуск впав — оверлей
  // зникає і відкривається звичайний інтерфейс.

  import { t } from './i18n.svelte'
  import { launchStageLabel, launchStagePct } from './launchStages'
  import type { LaunchProgress } from './types'

  let { packName, packIcon, presetIcon, packColor, progress, onCancel } = $props<{
    packName: string
    packIcon: string // data URL/шлях PNG-іконки збірки (порожній = пресет)
    presetIcon: string // Tabler-клас пресет-іконки (напр. "ti-puzzle")
    packColor: string
    progress: LaunchProgress | null
    onCancel: () => void
  }>()

  const stageLabel = $derived(progress ? launchStageLabel(progress.stage) : t('launch.stage.checking'))
  const pct = $derived(progress ? launchStagePct(progress) : 0)
</script>

<div class="lp-overlay" style="--pcol:{packColor || 'var(--orange)'}">
  <div class="lp-card">
    <div class="lp-icon">
      {#if packIcon}
        <img src={packIcon} alt="" />
      {:else if presetIcon}
        <i class="ti {presetIcon}"></i>
      {:else}
        <i class="ti ti-rocket"></i>
      {/if}
    </div>
    <div class="lp-name">{packName}</div>
    <div class="lp-stage">{stageLabel}</div>
    <div class="lp-bar"><div class="lp-bar-fill" style="width:{pct}%"></div></div>
    <div class="lp-pct mono">{pct}%</div>
    <button class="lp-cancel" onclick={onCancel}><i class="ti ti-square"></i> {t('cancel')}</button>
  </div>
</div>

<style>
  .lp-overlay {
    position: fixed; inset: 0; z-index: 100000;
    background: radial-gradient(1200px 600px at 50% 35%, rgba(255,138,0,.07), transparent 60%), #0a0a0f;
    display: flex; align-items: center; justify-content: center;
    --wails-draggable: no-drag;
  }
  .lp-card {
    display: flex; flex-direction: column; align-items: center; gap: 12px;
    width: 320px; padding: 34px 30px;
    background: rgba(22,22,28,.85); border: 1px solid var(--border); border-radius: 18px;
    backdrop-filter: blur(8px);
    box-shadow: 0 24px 60px rgba(0,0,0,.5);
  }
  .lp-icon {
    width: 72px; height: 72px; border-radius: 18px; overflow: hidden;
    display: flex; align-items: center; justify-content: center;
    background: linear-gradient(135deg, color-mix(in srgb, var(--pcol) 22%, transparent), transparent);
    font-size: 30px; color: var(--pcol);
  }
  .lp-icon img { width: 100%; height: 100%; object-fit: cover; }

  .lp-name {
    font-size: 15px; font-weight: 800; color: var(--text); text-align: center;
    max-width: 260px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .lp-stage { font-size: 12px; color: var(--text-mute); min-height: 16px; }

  .lp-bar {
    width: 100%; height: 6px; border-radius: 3px; background: var(--bg-input-hi);
    overflow: hidden;
  }
  .lp-bar-fill {
    height: 100%; border-radius: 3px;
    background: linear-gradient(90deg, var(--pcol), var(--violet));
    transition: width .25s ease;
  }
  .lp-pct { font-size: 11px; font-weight: 800; color: var(--pcol); }

  .lp-cancel {
    margin-top: 6px; display: inline-flex; align-items: center; gap: 6px;
    padding: 6px 14px; border: 1px solid var(--border); border-radius: 999px;
    background: transparent; color: var(--text-mute);
    font-size: 11px; font-weight: 700; font-family: inherit; cursor: pointer;
    transition: all .15s;
  }
  .lp-cancel:hover { background: rgba(239,68,68,.1); color: #ef4444; border-color: rgba(239,68,68,.3); }
</style>
