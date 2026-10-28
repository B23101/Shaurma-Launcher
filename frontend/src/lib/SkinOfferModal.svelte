<script lang="ts">
  // Після створення НЕліцензійного акаунта: запропонувати одразу поставити скін (гардероб відкриється
  // вже з цим акаунтом). У ліцензійного скін береться з Mojang, тому йому це вікно не показуємо.
  import UiModal from './UiModal.svelte'
  import { t } from './i18n.svelte'

  let {
    open = false,
    nickname = '',
    onwardrobe = null as null | (() => void),
    onlater = null as null | (() => void),
  } = $props()
</script>

<UiModal {open} onclose={() => onlater?.()} icon="ti-shirt" title={t('skinOffer.title', { nick: nickname })}>
  <div style="font-size:13px;line-height:1.55;color:var(--text-mute)">{t('skinOffer.text')}</div>
  {#snippet footer()}
    <button class="btn btn-ghost" onclick={onlater}>{t('skinOffer.later')}</button>
    <button class="btn btn-primary" onclick={onwardrobe}><i class="ti ti-shirt"></i> {t('skinOffer.open')}</button>
  {/snippet}
</UiModal>
