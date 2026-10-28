<script lang="ts">
  // ── ModListExportDialog: експорт списку модів збірки ──────────────────
  // Порт Prism ExportToModList: користувач обирає формат (HTML/Markdown/
  // plain/JSON/CSV) та поля (автор, URL, версія, ім'я файлу) і отримує
  // готовий список — для форуму/README/спільноти. Результат копіюється
  // в буфер обміну (як у Prism — діалог з прев'ю і «Copy to Clipboard»).
  //
  // ОБСЯГ експорту вибирається ВСЕРЕДИНІ діалогу (пілюлі «Усі моди» /
  // «Лише вибрані»), а не окремою кнопкою в панелі масових дій — раніше
  // було два окремих експорти з різним обсягом, що дублювало один діалог.
  //
  // Стиль модалки — стандартний для лаунчера (modal-head + X справа),
  // плюс прев'ю результату для кожного формату, щоб користувач бачив,
  // як виглядатиме список до копіювання.
  import { t } from './i18n.svelte'
  import { App } from './wails'
  import { toast } from './toast.svelte'
  import UiModal from './UiModal.svelte'
  import { highlightCode } from './highlight'
  import type { ModListExportOptions } from './types'

  let { packId = '', open, onClose, selectedFileNames = [] as string[] } = $props<{
    // packId — ID таргету менеджера модів: збірка або сервер ("srv:<id>").
    packId?: string
    open: boolean
    onClose: () => void
    // Поточне виділення менеджера модів — джерело для режиму «лише вибрані».
    // Порожній масив = експортувати всі моди (перемикач обсягу сховано).
    selectedFileNames?: string[]
  }>()

  let scope = $state<'all' | 'selected'>('all')
  let format = $state<'html' | 'markdown' | 'plain' | 'json' | 'csv'>('markdown')
  let authors = $state(true)
  let url = $state(true)
  let version = $state(true)
  let fileName = $state(false)
  let onlyEnabled = $state(true)
  let result = $state('')
  let busy = $state(false)
  let error = $state('')

  const opts = $derived<ModListExportOptions>({
    format, authors, url, version, fileName, onlyEnabled,
    // Обсяг: усі моди, або лише поточне виділення (якщо воно є).
    fileNames: scope === 'selected' && selectedFileNames.length > 0 ? selectedFileNames : undefined,
  })

  const formatLabel = $derived.by(() => {
    switch (format) {
      case 'html': return t('mods.export.formatHtml')
      case 'markdown': return t('mods.export.formatMarkdown')
      case 'json': return t('mods.export.formatJson')
      case 'csv': return t('mods.export.formatCsv')
      default: return t('mods.export.formatPlain')
    }
  })

  // Підсвітка синтаксису прев'ю: highlightCode() визначає мову за
  // РОЗШИРЕННЯМ імені файлу, а результат експорту — не файл на диску,
  // тож підбираємо «віртуальне» ім'я 1-в-1 під формат (той самий підхід,
  // що для реальних файлів у FileBrowserPanel/PackFilesPanel). Для
  // 'plain' розширення немає навмисно — highlightCode() тоді просто
  // повертає текст без токенізації (нема синтаксису — нема що підсвічувати).
  const previewVirtualName = $derived.by(() => {
    switch (format) {
      case 'html': return 'preview.html'
      case 'markdown': return 'preview.md'
      case 'json': return 'preview.json'
      case 'csv': return 'preview.csv'
      default: return 'preview'
    }
  })
  const highlightedResult = $derived(highlightCode(previewVirtualName, result))

  // Всі формати для міні-прев'ю під селектором: клік по прев'ю перемикає
  // формат і одразу оновлює результат.
  const formatChoices: { value: typeof format; label: string }[] = [
    { value: 'markdown', label: t('mods.export.formatMarkdown') },
    { value: 'html', label: t('mods.export.formatHtml') },
    { value: 'plain', label: t('mods.export.formatPlain') },
    { value: 'json', label: t('mods.export.formatJson') },
    { value: 'csv', label: t('mods.export.formatCsv') },
  ]

  // Скидаємо стан і одразу будуємо результат при ВІДКРИТТІ (формат/галочки
  // змінюються далі і перебудовують прев'ю на льоту через doExport).
  //
  // ВАЖЛИВО (баг «перемикаєш формат — а воно скидається»): тут НЕ можна
  // викликати doExport() прямо в тілі $effect на кожному перезапуску —
  // doExport читає opts (format/authors/...), тож ефект підхопив би їх як
  // залежності і перезапускався/скидав стан при зміні будь-якої галочки чи
  // формату. lastOpen — звичайна (не реактивна) змінна-прапорець: ефект
  // рефлектує лише на open і скидає стан рівно ОДИН раз при відкритті.
  let lastOpen = false
  $effect(() => {
    if (open) {
      if (!lastOpen) {
        format = 'markdown'
        authors = true
        url = true
        version = true
        fileName = false
        onlyEnabled = true
        scope = selectedFileNames.length > 0 ? 'selected' : 'all'
        result = ''
        busy = false
        error = ''
        doExport()
      }
      lastOpen = true
    } else {
      lastOpen = false
    }
  })

  async function doExport() {
    busy = true; error = ''
    try {
      result = await App.ExportModList(packId, opts)
      if (!result.trim()) toast(t('mods.export.empty'), 'error')
    } catch (e: any) {
      error = String(e?.message ?? e)
    } finally { busy = false }
  }

  async function copyResult() {
    try {
      await navigator.clipboard.writeText(result)
      toast(t('mods.export.copied'), 'success')
    } catch { error = t('mods.export.copyFailed') }
  }
</script>

{#if open}
  <UiModal
    open={true}
    size="xl"
    title={t('mods.export.title')}
    icon="ti-list-details"
    color="var(--orange)"
    locked={busy}
    onclose={onClose}
  >
        <div class="mel-body">
          <div class="mel-cols">
            <!-- ═══ Ліва колонка: налаштування ═══ -->
            <div class="mel-settings">
              <!-- Обсяг експорту — тут, всередині діалогу, а не окремою
                   кнопкою в тулбарі (раніше було два однакових експорти). -->
              {#if selectedFileNames.length > 0}
                <div class="mel-section-label">{t('mods.export.scope')}</div>
                <div class="mel-previews">
                  <button
                    class="mel-format-pill {scope === 'all' ? 'sel' : ''}"
                    onclick={() => { scope = 'all'; doExport() }}
                  ><i class="ti ti-list"></i> {t('mods.export.scopeAll')}</button>
                  <button
                    class="mel-format-pill {scope === 'selected' ? 'sel' : ''}"
                    onclick={() => { scope = 'selected'; doExport() }}
                  ><i class="ti ti-square-check"></i> {t('mods.export.selectedOnly', { n: selectedFileNames.length })}</button>
                </div>
              {/if}

              <!-- Вибір формату — ЄДИНИЙ спосіб (пілюлі), без дублюючого
                   select-дропдауна поруч, який керував тим самим `format`. -->
              <div class="mel-section-label">{t('mods.export.format')}</div>
              <div class="mel-previews">
                {#each formatChoices as fc (fc.value)}
                  <button
                    class="mel-format-pill {format === fc.value ? 'sel' : ''}"
                    onclick={() => { format = fc.value; doExport() }}
                    title={fc.label}
                  >
                    <i class="ti {fc.value === 'html' ? 'ti-code' : fc.value === 'json' ? 'ti-braces' : fc.value === 'csv' ? 'ti-table' : fc.value === 'plain' ? 'ti-file-text' : 'ti-markdown'}"></i>
                    {fc.label}
                  </button>
                {/each}
              </div>

              <div class="mel-section-label">{t('mods.export.fields')}</div>
              <div class="mel-fields">
                <label class="mel-check"><input type="checkbox" checked={authors} onchange={(e: any) => { authors = e.target.checked; doExport() }} /> {t('mods.export.fieldAuthors')}</label>
                <label class="mel-check"><input type="checkbox" checked={url} onchange={(e: any) => { url = e.target.checked; doExport() }} /> {t('mods.export.fieldUrl')}</label>
                <label class="mel-check"><input type="checkbox" checked={version} onchange={(e: any) => { version = e.target.checked; doExport() }} /> {t('mods.export.fieldVersion')}</label>
                <label class="mel-check"><input type="checkbox" checked={fileName} onchange={(e: any) => { fileName = e.target.checked; doExport() }} /> {t('mods.export.fieldFileName')}</label>
                {#if !(scope === 'selected' && selectedFileNames.length > 0)}
                  <label class="mel-check"><input type="checkbox" checked={onlyEnabled} onchange={(e: any) => { onlyEnabled = e.target.checked; doExport() }} /> {t('mods.export.onlyEnabled')}</label>
                {/if}
              </div>

              <div class="mel-actions">
                <button class="btn btn-ghost btn-sm" onclick={doExport} disabled={busy}>
                  {#if busy}<i class="ti ti-loader spin"></i>{:else}<i class="ti ti-refresh"></i>{/if} {t('mods.export.refresh')}
                </button>
                <button class="btn btn-primary btn-sm" onclick={copyResult} disabled={busy || !result}>
                  <i class="ti ti-copy"></i> {t('mods.export.copy')}
                </button>
              </div>

              {#if error}<div class="mel-error">{error}</div>{/if}
            </div>

            <!-- ═══ Права колонка: прев'ю результату — власний незалежний
                 скрол (.mel-pre), не пов'язаний зі скролом лівої колонки
                 чи модалки в цілому. ═══ -->
            <div class="mel-preview">
              <div class="mel-preview-head">
                <span>{formatLabel}</span><span class="mono">{result.split('\n').length} {t('mods.export.lines')}</span>
              </div>
              <pre class="mel-pre">{#if result}{@html highlightedResult}{:else}{busy ? t('mods.export.generating') : t('mods.export.previewHint')}{/if}</pre>
            </div>
          </div>
        </div>
  </UiModal>
{/if}

<style>
  .mel-body { display: flex; flex-direction: column; gap: 14px; overflow: hidden; height: 460px; }

  /* ── Два незалежні стовпці: зліва налаштування (свій скрол, якщо не
     влазить), справа прев'ю результату (свій незалежний скрол — не
     залежить від висоти/скролу лівої колонки). ── */
  .mel-cols { display: grid; grid-template-columns: 260px 1fr; gap: 18px; flex: 1; min-height: 0; }

  .mel-settings {
    display: flex; flex-direction: column; gap: 10px;
    overflow-y: auto; padding-right: 4px; min-height: 0;
  }
  .mel-section-label {
    font-size: 11px; font-weight: 700; color: var(--text-mute);
    text-transform: uppercase; letter-spacing: .3px; margin-top: 4px;
  }
  .mel-fields { display: flex; flex-direction: column; gap: 8px; }
  .mel-check { display: inline-flex; align-items: center; gap: 6px; font-size: 12px; color: var(--text); cursor: pointer; }
  .mel-check input { accent-color: var(--orange); }

  /* Пілюлі форматів — ЄДИНИЙ спосіб вибору формату (раніше поруч був ще
     select-дропдаун з тим самим призначенням — прибрано, щоб не було
     двох контролів для однієї дії). */
  .mel-previews { display: flex; flex-wrap: wrap; gap: 6px; }
  .mel-format-pill {
    display: inline-flex; align-items: center; gap: 6px;
    padding: 5px 11px; border-radius: 999px;
    border: 1px solid var(--border); background: var(--bg-input);
    color: var(--text-mute); font-size: 11px; font-weight: 700; font-family: inherit;
    cursor: pointer; transition: all .15s;
  }
  .mel-format-pill i { font-size: 12px; }
  .mel-format-pill:hover { color: var(--text); border-color: var(--border-hi); }
  .mel-format-pill.sel {
    background: rgba(157,124,255,.12); border-color: rgba(157,124,255,.45);
    color: var(--violet-l);
  }

  .mel-actions { display: flex; gap: 8px; margin-top: auto; padding-top: 6px; }
  .mel-error { font-size: 12px; color: var(--red); }

  /* Права колонка — прев'ю: займає всю висоту .mel-cols, .mel-pre
     скролиться незалежно (max-height знято — висоту диктує grid-рядок). */
  .mel-preview { display: flex; flex-direction: column; gap: 6px; min-height: 0; height: 100%; }
  .mel-preview-head { display: flex; justify-content: space-between; font-size: 11px; font-weight: 700; color: var(--text-mute); flex-shrink: 0; }
  .mel-pre {
    flex: 1; min-height: 0; overflow: auto;
    background: var(--bg-input); border: 1px solid var(--border); border-radius: 10px;
    padding: 12px; font-size: 11.5px; line-height: 1.55; color: var(--text);
    white-space: pre-wrap; word-break: break-word;
  }
</style>
