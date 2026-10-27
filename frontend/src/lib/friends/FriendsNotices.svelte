<script lang="ts">
  // Службові повідомлення над списком: ключі чату, офлайн, помилка синхронізації.
  import { useFriendsStore } from './FriendsStore.svelte'
  import { t } from '../i18n.svelte'

  const s = useFriendsStore()
  const errMsg = $derived(s.st.error ? s.codeText(s.st.error) : '')
</script>

{#if s.keys.state === 'need_recovery' || s.keys.state === 'pending' || s.keys.offline || errMsg}
  <div class="fn-wrap">
    {#if s.keys.state === 'need_recovery'}
      <div class="fn warn">
        <i class="ti ti-key-off"></i>
        <div class="fn-b">
          <b>{t('social.keys.needRecovery.title')}</b>
          <span>{t('social.keys.needRecovery.text')}</span>
          <button class="btn btn-danger btn-sm" disabled={s.keysBusy} onclick={() => s.resetKeys()}>{t('social.keys.reset')}</button>
        </div>
      </div>
    {:else if s.keys.state === 'pending'}
      <div class="fn"><i class="ti ti-loader-2 ff-spin"></i><div class="fn-b"><span>{t('social.keys.pending')}</span></div></div>
    {:else if s.keys.offline}
      <div class="fn"><i class="ti ti-cloud-off"></i><div class="fn-b"><span>{t('social.keys.offline')}</span></div></div>
    {/if}
    {#if errMsg}
      <div class="fn"><i class="ti ti-wifi-off"></i><div class="fn-b"><span>{errMsg}</span></div></div>
    {/if}
  </div>
{/if}

<style>
  .fn-wrap { display: flex; flex-direction: column; gap: 6px; padding: 0 12px 8px; flex: 0 0 auto; }
  .fn { display: flex; gap: 9px; align-items: flex-start; padding: 9px 11px; border-radius: 11px; animation: fn-in .24s var(--ff-ease) both;
    border: 1px solid color-mix(in srgb, var(--ff-warn) 40%, transparent); background: color-mix(in srgb, var(--ff-warn) 9%, transparent); }
  .fn > i { flex: 0 0 auto; margin-top: 1px; font-size: 15px; color: var(--ff-warn); }
  .fn.warn > i { color: var(--ff-bad); }
  .fn-b { min-width: 0; display: flex; flex-direction: column; align-items: flex-start; gap: 4px; font: 400 11.5px/1.45 var(--font-ui); color: var(--text); overflow-wrap: anywhere; }
  .fn-b b { font-weight: 750; }
  @keyframes fn-in { from { opacity: 0; transform: translateY(-4px); } to { opacity: 1; transform: none; } }
  @media (prefers-reduced-motion: reduce) { .fn { animation: none; } }
</style>
