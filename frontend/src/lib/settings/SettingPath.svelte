<script lang="ts">
  // SettingPath.svelte — клікабельний шлях до налаштування:
  // «Налаштування → Java та запуск → Пам’ять ↗». Клік відкриває потрібний
  // розділ і підсвічує рядок. Текст збирається з реєстру (див. nav.ts), тож
  // завжди збігається з тим, що реально є в лаунчері.
  import { t } from '../i18n.svelte'
  import { resolveSettingRef, goToSetting, canNavigateToSettings } from './nav'

  let { ref }: { ref: string } = $props()
  const r = $derived(resolveSettingRef(ref))
  const linkable = canNavigateToSettings()
  const full = $derived(r ? [t('nav.settings'), ...r.parts].join(' → ') : '')
</script>

{#if r && linkable}
  <button type="button" class="sp-link" title={t('settings.path.open')}
          onclick={(e) => { e.stopPropagation(); goToSetting(ref) }}>
    <span class="sp-txt">{full}</span><i class="ti ti-arrow-up-right"></i>
  </button>
{:else if r}
  <span class="sp-plain">{full}</span>
{/if}

<style>
  .sp-link {
    display: inline; padding: 0 1px; margin: 0; border: 0; background: none; cursor: pointer;
    font: inherit; font-weight: 600; color: var(--orange); text-align: left;
    border-bottom: 1px dashed rgba(var(--orange-rgb), .5); border-radius: 2px;
    transition: background .15s, border-color .15s;
  }
  .sp-link:hover { background: rgba(var(--orange-rgb), .12); border-bottom-style: solid; }
  .sp-link:focus-visible { outline: 2px solid var(--orange); outline-offset: 2px; }
  .sp-link i { font-size: .95em; margin-left: 2px; vertical-align: -1px; }
  .sp-plain { font-weight: 600; }
</style>
