<script lang="ts">
  // ── Згортувана панель помилки ─────────────────────────────────────────
  // Великі помилки (інсталер/запуск) більше не заповнюють пів екрана:
  // панель згорнута за замовчуванням, згори — заголовок і кнопки
  // «Скопіювати» / «Розгорнути». Текст НЕ обрізається — користувач може
  // розгорнути повністю і скопіювати суть.
  import { t } from './i18n.svelte'
  import { App } from './wails'

  let {
    title = t('error.title'),
    message = '',
    collapsedByDefault = true,
    accent = 'red'
  } = $props<{
    title?: string
    message: string
    collapsedByDefault?: boolean
    accent?: 'red' | 'yellow'
  }>()

  let open = $state(!collapsedByDefault)

  async function copyMessage() {
    try {
      await App.CopyToClipboard(message)
    } catch { /* буфер обміну недоступний */ }
  }
</script>

<div class="err-panel" class:open class:yellow={accent === 'yellow'} style="--err-a:{accent === 'yellow' ? 'var(--yellow)' : 'var(--red)'}">
  <div class="ep-head" role="button" tabindex="0" onclick={() => open = !open}
       onkeydown={(e: any) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); open = !open } }}>
    <span class="ep-title"><i class="ti ti-alert-triangle"></i> <span class="ep-title-text">{title}</span></span>
    <div class="ep-acts" role="group" onclick={(e: any) => e.stopPropagation()}>
      <button class="ep-btn" onclick={copyMessage} title={t('error.copy')}>
        <i class="ti ti-copy"></i> {t('error.copy')}
      </button>
      <button class="ep-btn" onclick={() => open = !open} title={open ? t('error.collapse') : t('error.expand')}>
        <i class="ti {open ? 'ti-chevron-up' : 'ti-chevron-down'}"></i>
      </button>
    </div>
  </div>
  {#if open}
    <pre class="ep-body">{message}</pre>
  {/if}
</div>

<style>
  .err-panel {
    margin: 12px 0;
    border: 1px solid color-mix(in srgb, var(--err-a) 45%, transparent);
    border-radius: var(--r-md);
    background: color-mix(in srgb, var(--err-a) 8%, var(--bg-card));
    overflow: hidden;
  }
  .ep-head {
    display: flex; align-items: center; gap: 10px;
    padding: 9px 12px; cursor: pointer; user-select: none;
  }
  .ep-head:hover { background: rgba(255,255,255,.03); }
  .ep-title {
    display: flex; align-items: center; gap: 8px; flex: 1; min-width: 0;
    font-size: 12.5px; font-weight: 700; color: var(--err-a);
  }
  .ep-title i { font-size: 15px; flex-shrink: 0; }
  .ep-title-text { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .ep-acts { display: flex; align-items: center; gap: 4px; flex-shrink: 0; }
  .ep-btn {
    display: inline-flex; align-items: center; gap: 5px;
    padding: 5px 9px; border-radius: 6px; border: 1px solid var(--border);
    background: var(--bg-input); color: var(--text-mute);
    font-size: 11px; font-weight: 600; cursor: pointer;
    transition: background .15s, color .15s, border-color .15s;
  }
  .ep-btn:hover { color: var(--text); border-color: var(--border-hi); background: var(--hover); }
  .ep-body {
    margin: 0; padding: 12px 14px;
    font-family: var(--font-mono); font-size: 11.5px; line-height: 1.55;
    color: var(--text-mute);
    background: color-mix(in srgb, var(--err-a) 6%, #0e0c09);
    border-top: 1px solid color-mix(in srgb, var(--err-a) 20%, transparent);
    white-space: pre-wrap; word-break: break-word;
    max-height: 46vh; overflow-y: auto;
  }
  .yellow { --err-a: var(--yellow, #ffd166); }
</style>
