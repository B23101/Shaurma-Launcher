<script lang="ts">
  // GlobalOverride.svelte — «Глобально / своє значення» для налаштувань збірки.
  // Перемикач «Використовувати глобальне»: поки він увімкнений, збірка бере значення
  // з налаштувань лаунчера (показуємо яке саме + клікабельний шлях до нього).
  // Вимикаєш — плавно висувається блок зі своїми значеннями збірки.
  import type { Snippet } from 'svelte'
  import { slide } from 'svelte/transition'
  import { t } from './i18n.svelte'
  import './settings/settings.css'
  import SettingPath from './settings/SettingPath.svelte'

  let {
    global, onToggle, ref, current = '', hint = '', children,
  }: {
    /** true = збірка бере глобальне значення. */
    global: boolean
    onToggle: () => void
    /** ref глобального параметра (див. settings/nav.ts) — для шляху-посилання. */
    ref: string
    /** Яке значення діє глобально зараз (текст). */
    current?: string
    hint?: string
    children?: Snippet
  } = $props()

  // «Анімації вимкнено» / режим слабкого ПК — без руху.
  const dur = () => {
    const d = document.documentElement.dataset
    return d.animations === 'off' || d.performanceMode === 'on' ? 0 : 200
  }
</script>

<div class="go">
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
  <div class="go-head" onclick={onToggle}>
    <span class="go-text">
      <span class="go-name">{t('pack.global.use')}</span>
      <span class="go-sub">{hint || t('pack.perf.useGlobalDesc')}</span>
    </span>
    <span class="go-pill" class:own={!global}>{global ? t('pack.global.badge') : t('pack.global.badgeOwn')}</span>
    <button type="button" class="st-switch" class:on={global} role="switch"
            aria-checked={global} aria-label={t('pack.global.use')}></button>
  </div>

  {#if global}
    <div class="go-now" transition:slide={{ duration: dur() }}>
      {#if current}<span class="go-cur">{t('pack.global.now', { value: current })}</span>{/if}
      <span class="go-where">{t('pack.global.changeAt')} <SettingPath {ref} /></span>
    </div>
  {:else}
    <div class="go-body" transition:slide={{ duration: dur() }}>
      {@render children?.()}
      <div class="go-foot">{t('pack.global.ownFoot')} <SettingPath {ref} /></div>
    </div>
  {/if}
</div>

<style>
  .go-head {
    display: flex; align-items: center; gap: 14px; padding: 13px 20px; cursor: pointer; user-select: none;
  }
  .go-text { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
  .go-name { font-size: 13.5px; font-weight: 600; line-height: 1.35; }
  .go-sub { font-size: 12px; line-height: 1.45; color: var(--text-mute); }
  .go-pill {
    flex: none; font-size: 11px; font-weight: 700; padding: 3px 9px; border-radius: 999px;
    background: var(--hover-strong); color: var(--text-mute);
  }
  .go-pill.own { background: rgba(var(--orange-rgb), .16); color: var(--orange); }
  .go-now {
    display: flex; flex-direction: column; gap: 4px; padding: 0 20px 14px;
    font-size: 12px; line-height: 1.5; color: var(--text-mute);
  }
  .go-cur { font-family: var(--font-mono); font-size: 13px; font-weight: 600; color: var(--orange); }
  .go-body { border-top: 1px solid var(--border); background: rgba(127, 127, 127, .04); }
  .go-foot { padding: 10px 20px 12px; font-size: 12px; line-height: 1.5; color: var(--text-mute); border-top: 1px solid var(--border); }
</style>
