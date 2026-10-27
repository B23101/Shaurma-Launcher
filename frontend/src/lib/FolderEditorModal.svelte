<script lang="ts">
  // ── FolderEditorModal — створення / редагування папки ────────────────────
  // Спільний для збірок і серверів. Зверху — ЖИВЕ ПРЕВ'Ю картки папки (змінюєш
  // назву/колір/іконку — вона оновлюється одразу), нижче поля, а вибір
  // елементів (збірок чи серверів) батько передає як children.
  import { onMount, tick } from 'svelte'
  import type { Snippet } from 'svelte'
  import Portal from './Portal.svelte'
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
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') { e.stopPropagation(); oncancel() } }
    window.addEventListener('keydown', onKey, true)
    return () => window.removeEventListener('keydown', onKey, true)
  })
</script>

<Portal>
  <div class="modal-overlay" onclick={(e) => { if (e.target === e.currentTarget) oncancel() }} role="presentation">
    <div class="modal fe2" style="--fc:{color}" role="dialog" aria-modal="true" aria-label={mode === 'create' ? t('packs.folderCreate') : t('packs.folderRename')}>
      <div class="fe2-preview">
        <div class="fe2-glow" aria-hidden="true"></div>
        <div class="fe2-tile">
          <div class="fe2-badge"><i class="ti {ic}"></i></div>
          <div class="fe2-info">
            <div class="fe2-pname">{name.trim() || t('packs.folderNewName')}</div>
            <div class="fe2-psub">{count} {countLabel}</div>
          </div>
        </div>
        <button class="fe2-x" onclick={oncancel} title={t('cancel')} aria-label={t('cancel')}><i class="ti ti-x"></i></button>
      </div>

      <div class="fe2-body">
        <label class="fe2-field">
          <span class="fe2-label">{t('packs.folderName')}</span>
          <input class="fe2-input" bind:this={input} bind:value={name} maxlength="40"
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

      <div class="modal-foot">
        <button class="btn btn-ghost" onclick={oncancel}>{t('cancel')}</button>
        <button class="btn btn-primary" onclick={onsave} disabled={!canSave}>
          <i class="ti {mode === 'create' ? 'ti-folder-plus' : 'ti-check'}"></i>
          {mode === 'create' ? t('packs.folderCreateBtn') : t('save')}
        </button>
      </div>
    </div>
  </div>
</Portal>

<style>
  .fe2 { width: min(500px, 94vw); max-width: none; border-radius: 22px; border-color: color-mix(in srgb, var(--fc) 26%, var(--border-hi)); }
  .fe2-preview {
    position: relative; flex: 0 0 auto; padding: 26px 22px 20px; overflow: hidden;
    background: linear-gradient(135deg, color-mix(in srgb, var(--fc) 24%, transparent), transparent 70%);
    border-bottom: 1px solid color-mix(in srgb, var(--fc) 16%, var(--border));
  }
  .fe2-glow { position: absolute; right: -50px; top: -70px; width: 220px; height: 220px; border-radius: 50%; pointer-events: none;
    background: radial-gradient(circle, color-mix(in srgb, var(--fc) 34%, transparent), transparent 68%); filter: blur(8px); }
  .fe2-tile { position: relative; display: flex; align-items: center; gap: 14px; }
  .fe2-badge {
    width: 58px; height: 58px; border-radius: 18px; display: grid; place-items: center; font-size: 29px; color: #fff; flex: none;
    background: linear-gradient(145deg, var(--fc), color-mix(in srgb, var(--fc) 55%, #000));
    box-shadow: 0 8px 22px color-mix(in srgb, var(--fc) 42%, transparent), inset 0 1px 0 rgba(255, 255, 255, .35);
    transition: background .2s, box-shadow .2s;
  }
  .fe2-info { min-width: 0; }
  .fe2-pname { font-size: 19px; font-weight: 800; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .fe2-psub { margin-top: 3px; font-size: 12px; color: var(--text-mute); }
  .fe2-x {
    position: absolute; right: 14px; top: 14px; width: 32px; height: 32px; border-radius: 10px; display: grid; place-items: center;
    cursor: pointer; font-size: 16px; background: transparent; border: none; color: var(--text-mute); transition: all .2s;
  }
  .fe2-x:hover { background: var(--hover); color: var(--text); transform: rotate(90deg); }

  .fe2-body { flex: 1; min-height: 0; overflow-y: auto; padding: 18px 22px; display: flex; flex-direction: column; gap: 16px; scrollbar-width: thin; }
  .fe2-field { display: flex; flex-direction: column; gap: 8px; }
  .fe2-label { font-size: 11px; font-weight: 700; color: var(--text-mute); text-transform: uppercase; letter-spacing: .05em; }
  .fe2-count { color: var(--fc); font-family: var(--font-mono); margin-left: 4px; }
  .fe2-input {
    width: 100%; padding: 10px 13px; border-radius: 11px; font-size: 14px; background: var(--bg); color: var(--text);
    border: 1.5px solid var(--border-hi); outline: none; transition: border-color .15s, box-shadow .15s;
  }
  .fe2-input:focus { border-color: var(--fc); box-shadow: 0 0 0 3px color-mix(in srgb, var(--fc) 20%, transparent); }
  .fe2-input::placeholder { color: var(--text-dim); }

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
  @media (prefers-reduced-motion: reduce) { .fe2-ic, .fe2-dot, .fe2-x, .fe2-badge { transition: none; } }
</style>
