<script lang="ts">
  // ── FolderEditorModal — створення / редагування папки ────────────────────
  // Спільний для збірок і серверів. Шапка UiModal — ЖИВЕ ПРЕВ'Ю папки (змінюєш
  // назву/колір/іконку — вона оновлюється одразу), нижче поля, а вибір
  // елементів (збірок чи серверів) батько передає як children.
  import { onMount, tick } from 'svelte'
  import type { Snippet } from 'svelte'
  import UiModal from './UiModal.svelte'
  import './settings/settings.css'
  import { t } from './i18n.svelte'
  import { FOLDER_COLORS, FOLDER_ICONS, safeFolderIcon } from './folderMeta'

  let {
    mode = 'create',
    name = $bindable(''),
    color = $bindable('#ff8a00'),
    icon = $bindable('ti-folder'),
    count = 0,
    countLabel = '',
    pickerLabel = '',
    showPicker = false,
    onsave = () => {},
    oncancel = () => {},
    children,
  }: {
    mode?: 'create' | 'edit'
    name?: string
    color?: string
    icon?: string
    count?: number
    countLabel?: string
    pickerLabel?: string
    showPicker?: boolean
    onsave?: () => void
    oncancel?: () => void
    children?: Snippet
  } = $props()

  let input = $state<HTMLInputElement | null>(null)
  const ic = $derived(safeFolderIcon(icon))
  const canSave = $derived(name.trim().length > 0)

  onMount(() => {
    tick().then(() => { input?.focus(); input?.select() })
  })
</script>

<UiModal open={true} onclose={oncancel} icon={ic} {color} width="min(500px, 94vw)"
         title={name.trim() || t('packs.folderNewName')} subtitle="{count} {countLabel}">
  <div class="fe2-body" style="--fc:{color}">
    <label class="fe2-field">
      <span class="fe2-label">{t('packs.folderName')}</span>
      <input class="st-input grow" bind:this={input} bind:value={name} maxlength="40"
             placeholder={t('packs.folderNamePlaceholder')}
             onkeydown={(e) => { if (e.key === 'Enter' && canSave) onsave() }} />
    </label>

    <div class="fe2-field">
      <span class="fe2-label">{t('folder.icon')}</span>
      <div class="fe2-icons">
        {#each FOLDER_ICONS as i (i)}
          <button class="fe2-ic" class:sel={ic === i} onclick={() => (icon = i)} aria-label={i} aria-pressed={ic === i}>
            <i class="ti {i}"></i>
          </button>
        {/each}
      </div>
    </div>

    <div class="fe2-field">
      <span class="fe2-label">{t('packs.folderColor')}</span>
      <div class="fe2-colors">
        <label class="fe2-cpick" title={t('duplicate.customColor')}>
          <input type="color" value={color} oninput={(e) => (color = e.currentTarget.value)} />
          <i class="ti ti-color-picker"></i>
        </label>
        {#each FOLDER_COLORS as c (c)}
          <button class="fe2-dot" class:sel={color.toLowerCase() === c} style="background:{c}" onclick={() => (color = c)} aria-label={c}></button>
        {/each}
      </div>
    </div>

    {#if showPicker}
      <div class="fe2-field">
        <span class="fe2-label">{pickerLabel} <b class="fe2-count">{count}</b></span>
        {@render children?.()}
      </div>
    {/if}
  </div>

  {#snippet footer()}
    <button class="btn btn-ghost" onclick={oncancel}>{t('cancel')}</button>
    <button class="btn btn-primary" onclick={onsave} disabled={!canSave}>
      <i class="ti {mode === 'create' ? 'ti-folder-plus' : 'ti-check'}"></i>
      {mode === 'create' ? t('packs.folderCreateBtn') : t('save')}
    </button>
  {/snippet}
</UiModal>

<style>
  .fe2-body { display: flex; flex-direction: column; gap: 16px; }
  .fe2-field { display: flex; flex-direction: column; gap: 8px; }
  .fe2-label { font-size: 11px; font-weight: 700; color: var(--text-mute); text-transform: uppercase; letter-spacing: .05em; }
  .fe2-count { color: var(--fc); font-family: var(--font-mono); margin-left: 4px; }

  .fe2-icons { display: grid; grid-template-columns: repeat(10, 1fr); gap: 6px; }
  .fe2-ic {
    aspect-ratio: 1; border-radius: 10px; display: grid; place-items: center; font-size: 17px; cursor: pointer;
    background: var(--bg); border: 1.5px solid var(--border); color: var(--text-mute); transition: all .14s;
  }
  .fe2-ic:hover { color: var(--text); border-color: var(--text-dim); transform: translateY(-1px); }
  .fe2-ic.sel { color: #fff; background: var(--fc); border-color: var(--fc); box-shadow: 0 4px 14px color-mix(in srgb, var(--fc) 40%, transparent); }

  .fe2-colors { display: flex; align-items: center; gap: 9px; flex-wrap: wrap; }
  .fe2-cpick {
    position: relative; width: 30px; height: 30px; border-radius: 50%; overflow: hidden; cursor: pointer; display: grid; place-items: center;
    background: conic-gradient(#f87171, #fbbf24, #34d399, #60a5fa, #a78bfa, #f87171); color: #fff; font-size: 14px;
    border: 2px solid transparent; text-shadow: 0 1px 3px rgba(0, 0, 0, .6);
  }
  .fe2-cpick input { position: absolute; inset: -8px; width: 60px; height: 60px; opacity: 0; cursor: pointer; }
  .fe2-dot { width: 28px; height: 28px; border-radius: 50%; border: 2px solid transparent; cursor: pointer; transition: transform .12s, box-shadow .12s; }
  .fe2-dot:hover { transform: scale(1.14); }
  .fe2-dot.sel { border-color: var(--text); box-shadow: 0 0 0 3px color-mix(in srgb, var(--fc) 30%, transparent); }

  @media (max-width: 480px) { .fe2-icons { grid-template-columns: repeat(5, 1fr); } }
  @media (prefers-reduced-motion: reduce) { .fe2-ic, .fe2-dot { transition: none; } }
</style>
