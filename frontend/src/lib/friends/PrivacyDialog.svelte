<script lang="ts">
  // «Приватність» у розділі «Друзі». Вміст — той самий єдиний компонент, що й у «Мій профіль»
  // та в Налаштуваннях, тому тут нічого не дублюється.
  import { useFriendsStore } from './FriendsStore.svelte'
  import { t } from '../i18n.svelte'
  import UiModal from '../UiModal.svelte'
  import AccountPrivacy from '../AccountPrivacy.svelte'

  let { open = false, onclose }: { open?: boolean; onclose: () => void } = $props()
  const s = useFriendsStore()
  // Після закриття підтягуємо актуальний стан у список друзів (блокування, мут).
  function close() { onclose(); void s.load() }
</script>

<UiModal {open} onclose={close} icon="ti-lock" title={t('social.priv.title')} subtitle={t('ff.priv.sub')} width="540px" zIndex={1000001}>
  {#if open}<AccountPrivacy />{/if}
  {#snippet footer()}
    <button class="btn btn-primary btn-sm" onclick={close}>{t('ff.done')}</button>
  {/snippet}
</UiModal>
