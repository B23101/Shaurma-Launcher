<script lang="ts">
  import UiModal from './UiModal.svelte'
  // Передзапускове попередження про моди (дублікати/відсутні залежності),
  // як у Prism. Показується перед «Грати», коли pre-launch перевірка знайшла
  // проблеми. Рішення передаються колбеками у батька (App.svelte).
  import { t } from './i18n.svelte'
  import type { PreLaunchModIssues } from './types'

  let {
    issues = null as null | PreLaunchModIssues,
    onproceed = null as null | (() => void),   // «Все одно запустити»
    ondeletedup = null as null | (() => void), // «Видалити дублікати і запустити»
    onmods = null as null | (() => void)       // «Відкрити моди»
  }: {
    issues?: null | PreLaunchModIssues
    onproceed?: null | (() => void)
    ondeletedup?: null | (() => void)
    onmods?: null | (() => void)
  } = $props()

  const dups = $derived(issues?.duplicates ?? [])
  const deps = $derived(issues?.missingDeps ?? [])
  const blocked = $derived(issues?.blockedMods ?? [])
  const dupCount = $derived(dups.reduce((n, g) => n + g.mods.length - 1, 0))
</script>

{#if issues}
  <UiModal
    open={true}
    size="lg"
    title={t('mods.preLaunch.title')}
    icon="ti-alert-triangle"
    color="var(--yellow)"
    onclose={() => onproceed?.()}
  >
      <div class="pl-body">
        {#if dups.length > 0}
          <div class="pl-block">
            <div class="pl-warn-title">
              <i class="ti ti-copy"></i> {t('mods.preLaunch.duplicatesTitle', { n: dupCount })}
            </div>
            <div class="pl-groups">
              {#each dups as g, gi (gi)}
                {@const keep = g.mods.find(m => m.enabled) ?? g.mods[0]}
                <div class="pl-group" style="animation-delay:{Math.min(gi,6)*40}ms">
                  <div class="pl-group-head">
                    <span class="pl-group-name" title={g.mods[0].name}>{g.mods[0].name}</span>
                    <span class="pl-group-count">{g.mods.length}×</span>
                  </div>
                  <div class="pl-files">
                    {#each g.mods as m, i}
                      <span class="pl-file" class:keep={m.fileName === keep?.fileName} class:off={!m.enabled}>
                        <i class="ti {m.enabled ? 'ti-circle-check' : 'ti-player-pause'}"></i>
                        {m.fileName}
                      </span>
                    {/each}
                  </div>
                </div>
              {/each}
            </div>
          </div>
        {/if}
        {#if deps.length > 0}
          <div class="pl-block">
            <div class="pl-warn-title"><i class="ti ti-link"></i> {t('mods.preLaunch.depsTitle')}</div>
            <div class="pl-deps">
              {#each deps as d}
                <span class="pl-dep-chip"><i class="ti ti-puzzle"></i> {d}</span>
              {/each}
            </div>
          </div>
        {/if}
        {#if blocked.length > 0}
          <div class="pl-block">
            <div class="pl-warn-title"><i class="ti ti-lock"></i> {t('mods.preLaunch.blockedTitle', { n: blocked.length })}</div>
            <div class="pl-deps">
              {#each blocked as b}
                <span class="pl-dep-chip pl-blocked"><i class="ti ti-file"></i> {b}</span>
              {/each}
            </div>
            <div class="pl-blocked-hint">{t('mods.preLaunch.blockedHint')}</div>
          </div>
        {/if}
        <div class="pl-hint">{t('mods.preLaunch.hint')}</div>
      </div>
    {#snippet footer()}
      <button class="btn btn-ghost" onclick={() => onproceed?.()}><i class="ti ti-player-play"></i> {t('mods.preLaunch.anyway')}</button>
      <div class="mt-spacer"></div>
      {#if dupCount > 0}
        <button class="btn btn-danger" onclick={() => ondeletedup?.()} title={t('mods.preLaunch.deleteDupsHint')}>
          <i class="ti ti-trash"></i> {t('mods.preLaunch.deleteDups', { n: dupCount })}
        </button>
      {/if}
      <button class="btn btn-primary" onclick={() => onmods?.()}>
        <i class="ti ti-puzzle"></i> {t('mods.preLaunch.openMods')}
      </button>
    {/snippet}
  </UiModal>
{/if}

<style>
  /* Перевизначення scoped-стилю App.svelte: заголовок блоку жовтий,
     як у решти попереджень модалки. */
  .pl-warn-title {
    display: flex; align-items: center; gap: 7px;
    font-size: 12.5px; font-weight: 700; color: var(--yellow); margin-bottom: 8px;
  }
  .pl-warn-title i { font-size: 14px; }
  .pl-deps { align-items: center; margin-bottom: 8px; }
  .pl-blocked { background: color-mix(in srgb, var(--red) 10%, transparent); color: var(--red); }
  .pl-blocked-hint { font-size: 11.5px; color: var(--text-dim); margin-top: 4px; }
</style>
