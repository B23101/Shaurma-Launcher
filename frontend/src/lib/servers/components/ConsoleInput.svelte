<script lang="ts">
  // ── Поле вводу команд з історією (↑ / ↓) ─────────────────────────────
  import { t } from '../../i18n.svelte'

  let {
    value = $bindable(''),
    disabled = false,
    placeholder = t('servers.console.commandPlaceholder'),
    onsubmit = (_v: string) => {},
  }: {
    value?: string
    disabled?: boolean
    placeholder?: string
    onsubmit?: (v: string) => void
  } = $props()

  let history = $state<string[]>([])
  let idx = $state(-1)

  function keydown(e: KeyboardEvent) {
    if (e.key === 'Enter') {
      e.preventDefault()
      const v = value.trim()
      if (!v || disabled) return
      history.push(v)
      idx = history.length
      onsubmit(v)
      value = ''
      return
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      if (idx > 0) { idx--; value = history[idx] || '' }
      return
    }
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      if (idx < history.length - 1) { idx++; value = history[idx] || '' }
      else { idx = history.length; value = '' }
    }
  }
</script>

<!-- Сучасний рядок команди: інпут займає всю ширину, prompt і кнопка
     відправки (стрілка) сидять ВСЕРЕДИНІ поля — той самий патерн, що в
     чатах/пошуку лаунчера, а не окрема кнопка збоку "як у формі". -->
<div class="console-input">
  <span class="ci-prompt">&gt;</span>
  <input
    bind:value
    onkeydown={keydown}
    {placeholder}
    {disabled}
    class="ci-cmd"
  />
  <button
    class="ci-send"
    class:ready={!disabled && !!value.trim()}
    onclick={() => { if (!disabled && value.trim()) { history.push(value.trim()); idx = history.length; onsubmit(value.trim()); value = '' } }}
    disabled={disabled || !value.trim()}
    title={t('servers.console.send')}
    aria-label={t('servers.console.send')}
  >
    <i class="ti ti-arrow-up"></i>
  </button>
</div>

<style>
  /* .console-input, не .ci — щоб не плутати з глобальним .card-head .ci
     (квадратна іконка картки), навіть попри те, що Svelte-скоуп сам би
     не дав їм зіткнутися. */
  .console-input {
    display: flex; align-items: center; gap: 8px;
    margin: 10px 12px 12px; padding: 5px 5px 5px 14px;
    background: var(--bg-input); border: 1px solid var(--border); border-radius: 14px;
    transition: border-color .15s, box-shadow .15s;
  }
  .console-input:focus-within {
    border-color: color-mix(in srgb, var(--pcol, var(--orange)) 65%, transparent);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--pcol, var(--orange)) 14%, transparent);
  }
  .ci-prompt { color: var(--pcol, var(--orange)); font: 700 14px var(--font-mono); flex-shrink: 0; }
  .ci-cmd {
    flex: 1; min-width: 0; background: transparent; border: none;
    padding: 7px 2px; color: var(--text); font: 400 12.5px var(--font-mono); outline: none;
  }
  .ci-cmd::placeholder { color: var(--text-dim); }
  .ci-cmd:disabled { opacity: .5; cursor: not-allowed; }
  .ci-send {
    width: 32px; height: 32px; flex-shrink: 0; border-radius: 10px; cursor: pointer;
    display: flex; align-items: center; justify-content: center; font-size: 15px;
    background: var(--hover); border: 1px solid transparent; color: var(--text-dim);
    transition: background .15s, color .15s, transform .12s, border-color .15s;
  }
  .ci-send.ready { background: var(--pcol, var(--orange)); color: #1a0f00; border-color: var(--pcol, var(--orange)); }
  .ci-send.ready:hover { transform: translateY(-1px); }
  .ci-send:disabled { cursor: not-allowed; }
</style>
