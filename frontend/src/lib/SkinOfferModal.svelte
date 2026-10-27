<script lang="ts">
  // Після створення НЕліцензійного акаунта: запропонувати одразу поставити скін (гардероб відкриється
  // вже з цим акаунтом). У ліцензійного скін береться з Mojang, тому йому це вікно не показуємо.
  import Portal from './Portal.svelte'
  import { t } from './i18n.svelte'

  let {
    open = false,
    nickname = '',
    onwardrobe = null as null | (() => void),
    onlater = null as null | (() => void),
  } = $props()
</script>

{#if open}
<Portal>
  <div class="modal-overlay" onclick={onlater}>
    <div class="modal" onclick={(e: any) => e.stopPropagation()}>
      <div class="modal-head"><h3>{t('skinOffer.title', { nick: nickname })}</h3></div>
      <div class="modal-body" style="font-size:13px;line-height:1.55;color:var(--text-mute)">
        {t('skinOffer.text')}
      </div>
      <div class="modal-foot">
        <button class="btn btn-ghost" onclick={onlater}>{t('skinOffer.later')}</button>
        <button class="btn btn-primary" onclick={onwardrobe}><i class="ti ti-shirt"></i> {t('skinOffer.open')}</button>
      </div>
    </div>
  </div>
</Portal>
{/if}
