<script lang="ts">
  // BlockedModsWarning — попередження на сторінці збірки про моди, які
  // не вдалося завантажити при імпорті (автор заблокував, Modrinth не
  // знайдено). Показується під банером збірки; клік відкриває модалку
  // зі списком модів, посиланнями для ручного скачування та галочками
  // «скачано» (Downloaded=true → прибирається зі списку).
  import UiModal from './UiModal.svelte'
  import { App } from './wails'
  import { t } from './i18n.svelte'
  import type { BlockedModInfo } from './types'

  let {
    mods = [] as BlockedModInfo[],
    packId = '',
    onchange = null as null | (() => void)
  }: {
    mods: BlockedModInfo[]
    packId: string
    onchange?: null | (() => void)
  } = $props()

  let open = $state(false)

  // Тільки моди, які ще НЕ скачані
  let undownloaded = $derived(mods.filter(m => !m.downloaded))

  async function toggleDownloaded(fileName: string, downloaded: boolean) {
    await App.ToggleBlockedModDownloaded(packId, fileName, downloaded)
    onchange?.()
  }
</script>

{#if undownloaded.length > 0}
  <button class="blocked-warning" onclick={() => open = true}>
    <i class="ti ti-alert-triangle"></i>
    <span>{t('import.blocked.warning', { n: undownloaded.length })}</span>
    <i class="ti ti-chevron-right"></i>
  </button>
{/if}

{#if open}
  <UiModal
    open={true}
    width="520px"
    title={t('import.blocked.title', { n: undownloaded.length })}
    icon="ti-lock"
    color="var(--yellow)"
    onclose={() => open = false}
  >
    <div class="bm-body">
      <p class="bm-desc">{t('import.blocked.pageDesc')}</p>
      <div class="bm-list">
        {#each undownloaded as m (m.fileName)}
          <div class="bm-row">
            <label class="bm-check">
              <input type="checkbox" checked={m.downloaded} onchange={() => toggleDownloaded(m.fileName, !m.downloaded)} />
              <i class="ti ti-check"></i>
            </label>
            <span class="bm-file" title={m.fileName}>
              <i class="ti ti-file"></i>
              <span>{m.fileName}</span>
            </span>
            {#if m.required}
              <span class="bm-badge req">{t('import.blocked.required')}</span>
            {/if}
            {#if m.websiteUrl}
              <button class="bm-link" onclick={() => App.OpenExternal(m.websiteUrl!)} title={m.websiteUrl}>
                <i class="ti ti-external-link"></i>
              </button>
            {/if}
          </div>
        {/each}
      </div>
    </div>
    {#snippet footer()}
      <button class="btn btn-ghost" onclick={() => open = false}>{t('close')}</button>
    {/snippet}
  </UiModal>
{/if}

<style>
  .blocked-warning {
    display: flex; align-items: center; gap: 8px;
    width: 100%; padding: 10px 14px; margin: 0 0 10px;
    background: color-mix(in srgb, var(--yellow, #eab308) 10%, transparent);
    border: 1px solid color-mix(in srgb, var(--yellow, #eab308) 30%, transparent);
    border-radius: 10px; cursor: pointer;
    color: var(--yellow, #eab308); font-size: 13px; font-weight: 500;
    transition: all .15s;
  }
  .blocked-warning:hover {
    background: color-mix(in srgb, var(--yellow, #eab308) 18%, transparent);
    border-color: color-mix(in srgb, var(--yellow, #eab308) 50%, transparent);
  }
  .blocked-warning i:first-child { font-size: 16px; flex: none; }
  .blocked-warning span { flex: 1; text-align: left; }
  .blocked-warning i:last-child { font-size: 14px; opacity: .5; }
  .bm-body { display: flex; flex-direction: column; gap: 10px; }
  .bm-desc { font-size: 12.5px; color: var(--text-dim); line-height: 1.55; margin: 0; }
  .bm-list {
    display: flex; flex-direction: column; gap: 5px;
    max-height: 300px; overflow-y: auto; padding-right: 2px;
  }
  .bm-list::-webkit-scrollbar { width: 6px; }
  .bm-list::-webkit-scrollbar-thumb { background: var(--hover-strong); border-radius: 3px; }
  .bm-row {
    display: flex; align-items: center; gap: 8px;
    background: var(--bg-input); border: 1px solid var(--border);
    border-radius: 9px; padding: 7px 10px;
    animation: bm-row-in .18s ease;
  }
  @keyframes bm-row-in { from { opacity: 0; transform: translateY(-3px); } to { opacity: 1; transform: none; } }
  .bm-check {
    flex: none; width: 22px; height: 22px; border: 2px solid var(--border-hi);
    border-radius: 6px; display: flex; align-items: center; justify-content: center;
    cursor: pointer; position: relative; transition: all .14s;
  }
  .bm-check input { position: absolute; opacity: 0; width: 0; height: 0; }
  .bm-check .ti-check { font-size: 13px; color: var(--green); opacity: 0; transition: opacity .14s; }
  .bm-check:has(input:checked) { background: var(--green); border-color: var(--green); }
  .bm-check:has(input:checked) .ti-check { opacity: 1; color: #fff; }
  .bm-file {
    flex: 1; min-width: 0; display: flex; align-items: center; gap: 8px;
    font-size: 12.5px; color: var(--text);
  }
  .bm-file .ti-file { color: var(--text-mute); font-size: 14px; flex: none; }
  .bm-file span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .bm-badge {
    flex: none; font-size: var(--fs-min); font-weight: 800; text-transform: uppercase; letter-spacing: .3px;
    border-radius: 6px; padding: 2px 7px;
    background: color-mix(in srgb, var(--red, #ef4444) 15%, transparent);
    color: var(--red, #ef4444);
  }
  .bm-link {
    flex: none; width: 26px; height: 26px; border: none; border-radius: 7px;
    background: transparent; color: var(--text-dim); cursor: pointer;
    display: flex; align-items: center; justify-content: center; font-size: 13px;
    transition: all .14s;
  }
  .bm-link:hover { background: var(--bg-input-hi); color: var(--violet); }
</style>
