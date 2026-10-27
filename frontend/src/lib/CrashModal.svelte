<script lang="ts">
  import UiModal from './UiModal.svelte'
  // Модалка краху (замість старого банера на «Моїх збірках»): показується
  // поверх будь-якої сторінки, коли гра завершилась аварійно. Кнопка
  // «Відкрити консоль» переходить на сторінку збірки, яка впала, і
  // відкриває її вкладку «Консоль» (робить батько через onconsole).
  import { t } from './i18n.svelte'

  let {
    crashed = false,
    dontShow = false,
    onclose = null as null | (() => void),       // сховати для поточного краху
    onconsole = null as null | (() => void),     // відкрити консоль збірки
    ondontshow = null as null | (() => void)     // «не показувати знову» (localStorage)
  }: {
    crashed?: boolean
    dontShow?: boolean
    onclose?: null | (() => void)
    onconsole?: null | (() => void)
    ondontshow?: null | (() => void)
  } = $props()
</script>

{#if crashed && !dontShow}
  <UiModal
    open={true}
    size="sm"
    title={t('crashBanner.title')}
    icon="ti-alert-triangle"
    color="var(--red)"
    shake={true}
    onclose={() => onclose?.()}
  >
    <div style="display:flex;flex-direction:column;gap:14px">
      <div style="font-size:13px;color:var(--text-mute);line-height:1.6">{t('crashBanner.text')}</div>
      <label class="cb-dont" style="align-self:flex-start;display:flex;align-items:center;gap:8px;font-size:11.5px;color:var(--text-mute);cursor:pointer;user-select:none">
        <input type="checkbox" checked={dontShow} onchange={ondontshow} style="accent-color:var(--red)" />
        {t('crashBanner.dontShowAgain')}
      </label>
    </div>
    {#snippet footer()}
      <button class="btn btn-ghost" onclick={() => onclose?.()}><i class="ti ti-x"></i> {t('cancel')}</button>
      <button class="btn btn-primary" onclick={() => onconsole?.()}>
        <i class="ti ti-terminal-2"></i> {t('crashBanner.openConsole')}
      </button>
    {/snippet}
  </UiModal>
{/if}
