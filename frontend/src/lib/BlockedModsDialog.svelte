<script lang="ts">
  import UiModal from './UiModal.svelte'
  // Діалог заблокованих модів (імпорт CurseForge zip, як у Prism):
  // автор(-и) модів заборонили завантаження зі сторонніх джерел — CurseForge
  // не відає пряме посилання. Імпорт стоїть на паузі, поки користувач не
  // обере рішення (рішення передається колбеком onresolve у батька).
  //
  // Галочки: користувач може позначити моди, які вже скачав вручну.
  // Позначені моди зберігаються в BlockedMods з downloaded=true.
  import { t } from './i18n.svelte'
  import { App } from './wails'
  import type { BlockedCFMod } from './types'

  let {
    mods = [] as BlockedCFMod[],
    allowModrinth = false,
    onresolve = null as null | ((decision: 'modrinth' | 'skip' | 'manual' | 'cancel', checked: Set<string>) => void)
  }: {
    mods: BlockedCFMod[]
    allowModrinth: boolean
    onresolve?: null | ((decision: 'modrinth' | 'skip' | 'manual' | 'cancel', checked: Set<string>) => void)
  } = $props()

  let checked = $state(new Set<string>())

  function toggle(fileName: string) {
    if (checked.has(fileName)) {
      checked.delete(fileName)
    } else {
      checked.add(fileName)
    }
    checked = new Set(checked)
  }

  // «Скачати вручну»: відкриває прямі посилання на сторінки файлів
  // CurseForge у браузері, щоб користувач скачав моди сам, і продовжує
  // імпорт БЕЗ них (бекенд трактує manual як skip на рівні конвеєра).
  function manual() {
    const seen = new Set<string>()
    for (const m of mods) {
      if (m.websiteUrl && !seen.has(m.websiteUrl)) {
        seen.add(m.websiteUrl)
        App.OpenExternal(m.websiteUrl)
      }
    }
    onresolve?.('manual', checked)
  }
</script>

{#if mods.length > 0}
  <UiModal
    open={true}
    width="520px"
    title={t('import.blocked.title', { n: mods.length })}
    icon="ti-lock"
    color="var(--red)"
    onclose={() => onresolve?.('cancel', checked)}
  >
      <div class="bm-body">
        {#if allowModrinth}
          <p class="bm-desc">{t('import.blocked.desc')}</p>
        {:else}
          <p class="bm-desc bm-desc-mr">
            <i class="ti ti-cloud-off"></i>
            {t('import.blocked.afterModrinth')}
          </p>
        {/if}
        <div class="bm-list">
          {#each mods as m (m.projectId + ':' + m.fileId)}
            <div class="bm-row" class:bm-checked={checked.has(m.fileName)}>
              <label class="bm-check">
                <input type="checkbox" checked={checked.has(m.fileName)} onchange={() => toggle(m.fileName)} />
                <i class="ti ti-check"></i>
              </label>
              <span class="bm-file" title={m.fileName}>
                <i class="ti ti-file"></i>
                <span>{m.fileName}</span>
              </span>
              {#if m.required}
                <span class="bm-badge req">{t('import.blocked.required')}</span>
              {/if}
              <button class="bm-link" onclick={() => App.OpenExternal(m.websiteUrl)} title={m.websiteUrl}>
                <i class="ti ti-external-link"></i>
              </button>
            </div>
          {/each}
    {#snippet footer()}
      <button class="btn btn-ghost" onclick={() => onresolve?.('cancel', checked)}>{t('import.blocked.cancel')}</button>
      <button class="btn bm-skip" onclick={() => onresolve?.('skip', checked)}>
        <i class="ti ti-ban"></i> {t('import.blocked.skip')}
      </button>
      <button class="btn bm-manual" onclick={manual}>
        <i class="ti ti-download"></i> {t('import.blocked.manual')}
      </button>
      {#if allowModrinth}
        <button class="btn btn-primary bm-mr" onclick={() => onresolve?.('modrinth', checked)}>
          <i class="ti ti-cloud-download"></i> {t('import.blocked.modrinth')}
        </button>
      {/if}
    {/snippet}
  </UiModal>
{/if}

<style>
  .bm-body { display: flex; flex-direction: column; gap: 10px; }
  .bm-desc { font-size: 12.5px; color: var(--text-dim); line-height: 1.55; margin: 0; }
  .bm-desc-mr { display: flex; align-items: center; gap: 7px; }
  .bm-desc-mr i { font-size: 16px; color: var(--red); flex: none; }
  .bm-list {
    display: flex; flex-direction: column; gap: 5px;
    max-height: 260px; overflow-y: auto; padding-right: 2px;
  }
  .bm-list::-webkit-scrollbar { width: 6px; }
  .bm-list::-webkit-scrollbar-thumb { background: var(--hover-strong); border-radius: 3px; }
  .bm-row {
    display: flex; align-items: center; gap: 8px;
    background: var(--bg-input); border: 1px solid var(--border);
    border-radius: 9px; padding: 7px 10px;
    animation: bm-row-in .18s ease;
    transition: border-color .14s;
  }
  .bm-row.bm-checked { border-color: var(--green); background: rgba(16,185,129,.06); }
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
    flex: none; font-size: 10px; font-weight: 800; text-transform: uppercase; letter-spacing: .3px;
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
  .bm-foot { gap: 8px; }
  .bm-skip {
    background: var(--bg-input-hi); border: 1px solid var(--border-hi); color: var(--text);
    transition: all .14s;
  }
  .bm-skip:hover { border-color: var(--text-dim); }
  .bm-manual {
    background: var(--bg-input-hi); border: 1px solid var(--border-hi); color: var(--violet);
    transition: all .14s;
  }
  .bm-manual:hover { border-color: var(--violet); background: var(--bg-input-hi); }
  .bm-mr { margin-left: auto; }
</style>
