<script lang="ts">
  // ── Модалка розпакування файлу збірки ──────────────────────────────────
  // Відкривається, коли користувач ДВІЧІ клікнув .shaurmpack/.shaurmbundle
  // у Провіднику (Windows-асоціація файлів → подія packfile:open) або
  // обрав файл через «Відкрити за допомогою». Раніше лаунчер мовчки
  // імпортував файл одразу: не було видно, ЩО імпортується, не було
  // моменту «Стоп/Скасувати» і взагалі не було відчуття «відкрив файл →
  // лаунчер показав вікно розпакування».
  //
  // Робить рівно три речі: прев'ю (спершу шапка «Читаємо файл…»),
  // кнопку «Розпакувати», і сповіщення батькові (App.svelte) про результат,
  // щоб той зробив тост і відкрив збірку — та сама логіка, що й раніше
  // жила в autoImportPackFile.
  import UiModal from './UiModal.svelte'
  import { onMount } from 'svelte'
  import { t } from './i18n.svelte'
  import { App } from './wails'
  import type { ShaurmpackPreview, ShaurmbundlePreview, CustomPack } from './types'

  // PackFileImportResult — чим закінчилось розпакування (для батька:
  // App.svelte показує тост і відкриває збірку).
  type PackFileImportResult =
    | { kind: 'pack'; pack: CustomPack }
    | { kind: 'bundle'; count: number }

  let {
    path = '',
    onclose = () => {},
    ondone = (_res: PackFileImportResult) => {},
  }: {
    path: string
    onclose?: () => void
    ondone?: (res: PackFileImportResult) => void
  } = $props()

  const isBundle = $derived(/\.shaurmbundle$/i.test(path))
  const isPack = $derived(/\.shaurmpack$/i.test(path))

  let loading = $state(true)
  let busy = $state(false)
  let error = $state('')
  let packPrev = $state<ShaurmpackPreview | null>(null)
  let bundlePrev = $state<ShaurmbundlePreview | null>(null)

  const fileName = $derived(path.split(/[\\/]/).pop() ?? path)

  onMount(() => { void loadPreview() })

  // Завантаження прев'ю: саме воно робить «модальне вікно РОЗПАКУВАННЯ»
  // видимим — поки файл відкривається, юзер уже бачить вікно з назвою.
  async function loadPreview() {
    loading = true
    error = ''
    try {
      if (isBundle) bundlePrev = await App.PreviewShaurmbundle(path)
      else if (isPack) packPrev = await App.PreviewShaurmpack(path)
      else error = t('osPack.unknown')
    } catch (e: any) {
      error = t('osPack.modal.badFile', { message: String(e?.message ?? e ?? '') })
    } finally {
      loading = false
    }
  }

  // Той самий набір викликів, що й ранішій авто-імпорт (App.svelte), —
  // лише перенесений сюди, щоб користувач натиснув «Розпакувати».
  async function unpack() {
    if (busy || loading || error) return
    busy = true
    try {
      if (isBundle) {
        const res = await App.ImportShaurmbundleFile(path, '')
        ondone({ kind: 'bundle', count: res?.packs?.length ?? 0 })
      } else {
        const pack = await App.ImportShaurmpackFile(path, '', '', '', '')
        ondone({ kind: 'pack', pack })
      }
    } catch (e: any) {
      // Показуємо помилку всередині модалки (закривати вікно в такому
      // разі не можна — користувач має прочитати, що пішло не так).
      error = t('osPack.error', { message: String(e?.message ?? e ?? '') })
    } finally {
      busy = false
    }
  }
</script>



<UiModal
  open={true}
  size="md"
  title={t('osPack.modal.title')}
  subtitle={fileName}
  icon={isBundle ? 'ti-package' : 'ti-box'}
  color="var(--orange)"
  zIndex="2100000"
  locked={busy}
  onclose={busy ? () => {} : onclose}
>
  {#if loading}
    <div class="ipf-state"><i class="ti ti-loader-2 spin"></i> {t('osPack.modal.loading')}</div>
  {:else if error}
    <div class="ipf-state err"><i class="ti ti-alert-triangle"></i> <span>{error}</span></div>
  {:else if bundlePrev}
    <div class="ipf-hero">
      <div class="ipf-ic"><i class="ti ti-package"></i></div>
      <div class="ipf-hmeta">
        {#if bundlePrev.description}<div class="ipf-desc">{bundlePrev.description}</div>{/if}
        <div class="ipf-stats">
          <span class="ipf-chip"><i class="ti ti-stack-2"></i> {t('osPack.modal.packs', { n: bundlePrev.packCount })}</span>
          <span class="ipf-chip"><i class="ti ti-file"></i> {t('osPack.modal.files', { n: bundlePrev.totalFiles })}</span>
          {#if bundlePrev.launcherName}<span class="ipf-chip dim"><i class="ti ti-rocket"></i> {bundlePrev.launcherName}</span>{/if}
        </div>
      </div>
    </div>
    {#if bundlePrev.packs?.length}
      <div class="ipf-list">
        {#each bundlePrev.packs as p (p.packId)}
          <div class="ipf-row">
            <span class="ipf-dot" style="background:{/^#[0-9a-fA-F]{6}$/.test(p.color || '') ? p.color : 'var(--orange)'}"></span>
            <span class="ipf-name">{p.name}</span>
            <span class="ipf-sub">{p.mcVersion}{#if p.loader}&nbsp;· {p.loader}{/if}</span>
          </div>
        {/each}
      </div>
    {/if}
    <div class="ipf-hint">{t('osPack.modal.bundleHint', { n: bundlePrev.packCount })}</div>
  {:else if packPrev}
    <div class="ipf-hero">
      <div class="ipf-ic">
        {#if packPrev.iconDataUrl}<img src={packPrev.iconDataUrl} alt="" />{:else}<i class="ti ti-box"></i>{/if}
      </div>
      <div class="ipf-hmeta">
        <div class="ipf-name big">{packPrev.name}</div>
        {#if packPrev.description}<div class="ipf-desc">{packPrev.description}</div>{/if}
        <div class="ipf-stats">
          {#if packPrev.versionId}<span class="ipf-chip"><i class="ti ti-tag"></i> {packPrev.versionId}</span>{/if}
          {#if packPrev.gameVersion}<span class="ipf-chip"><i class="ti ti-cube"></i> {packPrev.gameVersion}</span>{/if}
          {#if packPrev.loader}<span class="ipf-chip"><i class="ti ti-puzzle"></i> {packPrev.loader}{packPrev.loaderVersion ? ' ' + packPrev.loaderVersion : ''}</span>{/if}
        </div>
      </div>
    </div>
    <div class="ipf-stats">
      <span class="ipf-chip"><i class="ti ti-file"></i> {t('osPack.modal.files', { n: packPrev.fileCount })}</span>
      <span class="ipf-chip"><i class="ti ti-cloud-download"></i> {t('osPack.modal.online', { n: packPrev.resolvedCount })}</span>
      <span class="ipf-chip"><i class="ti ti-archive"></i> {t('osPack.modal.bundled', { n: packPrev.bundledCount })}</span>
    </div>
    <div class="ipf-hint">{t('osPack.modal.packHint')}</div>
  {/if}

  {#snippet footer()}
    <button class="btn btn-ghost" onclick={onclose} disabled={busy}>{t('cancel')}</button>
    <button class="btn btn-primary" onclick={unpack} disabled={busy || loading || !!error}>
      {#if busy}<i class="ti ti-loader-2 spin"></i>{:else}<i class="ti ti-folder-open"></i>{/if}
      {busy ? t('osPack.modal.unpacking') : t('osPack.modal.unpackBtn')}
    </button>
  {/snippet}
</UiModal>

<style>
  .ipf-state { display: flex; align-items: center; gap: 9px; padding: 22px 4px; font-size: 13px; color: var(--text-mute); }
  .ipf-state.err { color: var(--red); align-items: flex-start; }
  .ipf-state.err span { white-space: pre-line; line-height: 1.5; }
  .ipf-hero { display: flex; gap: 13px; align-items: center; min-width: 0; }
  .ipf-ic {
    flex: 0 0 auto; width: 62px; height: 62px; border-radius: 16px;
    background: rgba(var(--orange-rgb), .1); border: 1px solid rgba(var(--orange-rgb), .3);
    display: flex; align-items: center; justify-content: center; overflow: hidden;
  }
  .ipf-ic img { width: 100%; height: 100%; object-fit: cover; image-rendering: pixelated; }
  .ipf-ic i { font-size: 26px; color: var(--orange); }
  .ipf-hmeta { min-width: 0; display: flex; flex-direction: column; gap: 6px; }
  .ipf-name { font: 750 14px/1.25 var(--font-ui); color: var(--text); overflow: hidden; text-overflow: ellipsis; }
  .ipf-name.big { font-size: 17px; }
  .ipf-desc { font: 400 12px/1.5 var(--font-ui); color: var(--text-mute); }
  .ipf-stats { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 12px; }
  .ipf-chip {
    display: inline-flex; align-items: center; gap: 5px;
    font: 650 11px/1 var(--font-mono); color: var(--text);
    padding: 5px 9px; border-radius: 8px;
    background: var(--bg-input); border: 1px solid var(--border);
  }
  .ipf-chip i { color: var(--orange); font-size: 12px; }
  .ipf-chip.dim { color: var(--text-mute); }
  .ipf-list { display: flex; flex-direction: column; gap: 3px; margin-top: 14px; max-height: 34vh; overflow-y: auto; }
  .ipf-row {
    display: flex; align-items: center; gap: 8px; padding: 7px 9px;
    border-radius: 9px; background: var(--bg-input); border: 1px solid var(--border);
    font: 600 12.5px/1.3 var(--font-ui); color: var(--text);
  }
  .ipf-dot { flex: 0 0 auto; width: 9px; height: 9px; border-radius: 50%; }
  .ipf-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .ipf-sub { margin-left: auto; font: 500 10.5px/1 var(--font-mono); color: var(--text-dim); white-space: nowrap; }
  .ipf-hint { margin-top: 13px; font: 400 11.5px/1.55 var(--font-ui); color: var(--text-dim); }
</style>
