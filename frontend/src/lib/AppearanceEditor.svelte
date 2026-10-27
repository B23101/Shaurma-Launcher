<script lang="ts">
  // ── AppearanceEditor — ЄДИНИЙ віджет «Вигляд» ──────────────────────────
  //
  // Іконка (PNG + пресет) + колір (+ опційно банер). Раніше ця сама
  // розмітка була продубльована у PackBasicSection.svelte та ДВІЧІ в
  // CreatePackPage.svelte — і саме тому сервери (де свого блоку вигляду
  // не було взагалі) виглядали й поводились інакше за збірки. Тепер і
  // збірки, і сервери викликають ЦЕЙ компонент, тож розійтись вони більше
  // не можуть: один PresetIconPicker, одна палітра, одна розмітка.
  //
  // Компонент лише малює + володіє локальною логікою палітри (пресети й
  // «свій колір»); стан форми (що саме зберігати й коли) лишається у
  // виклику — через onColorChange/onSelectPreset/onBrowseIcon.
  //
  // Глобальні класи .card/.row/.rl-name/.icon-pick/.color-swatches/.sw/
  // .sw-custom/.color-hex живуть у style.css — компонент їх перевикористовує,
  // тому вигляд ідентичний незалежно від того, хто його відрендерив.
  import { t } from './i18n.svelte'
  import PresetIconPicker from './PresetIconPicker.svelte'

  let {
    // Заголовок картки вже перекладений викликом (у кожного свій ключ:
    // «Вигляд» збірки / крок 5 створення / «Вигляд» сервера).
    title,
    desc = '',
    // Номер кроку праворуч у шапці (лише форма створення збірки).
    step = 0,
    // Колір іконки-заглушки в шапці картки.
    ciClass = '',
    iconPreviewUrl = '',
    iconName = '',
    color,
    onBrowseIcon,
    onSelectPreset,
    onColorChange,
    // Необов'язковий банер (збірки). Якщо onBrowseBackground не передано —
    // блок банера не рендериться взагалі (сервери/створення без фону).
    backgroundPreviewUrl = '',
    backgroundRemovePending = false,
    onBrowseBackground = null as null | (() => void),
    onRemoveBackground = null as null | (() => void),
    // Індикатор авто-збереження вигляду ('' = не показувати).
    autoSaveState = 'idle' as 'idle' | 'saving' | 'saved',
  }: {
    title: string
    desc?: string
    step?: number
    ciClass?: string
    iconPreviewUrl?: string
    iconName?: string
    color: string
    onBrowseIcon: () => void
    onSelectPreset: (n: string) => void
    onColorChange: (c: string) => void
    backgroundPreviewUrl?: string
    backgroundRemovePending?: boolean
    onBrowseBackground?: null | (() => void)
    onRemoveBackground?: null | (() => void)
    autoSaveState?: 'idle' | 'saving' | 'saved'
  } = $props()

  // Ті самі 5 пресет-кольорів, що й у збірках (жодного власного набору).
  const colorPresets = ['#ff8a00', '#7b4dff', '#10b981', '#3b82f6', '#ef4444']

  // customColor — запам'ятовує останній «свій» колір, щоб свотч не
  // скидався на дефолт, коли користувач тимчасово обрав пресет.
  let customColor = $state('#b24592')
  const usingCustomColor = $derived(!colorPresets.includes(color))
  // Якщо колір задали ззовні (відкриття форми/імпорт) — підхоплюємо його
  // у «свій» свотч, інакше палітра показувала б чужий колір.
  $effect(() => {
    if (color && !colorPresets.includes(color)) customColor = color
  })

  function applyCustomColor(c: string) {
    customColor = c
    onColorChange(c)
  }

  function applyHex(v: string) {
    const s = v.trim()
    if (/^[0-9a-fA-F]{6}$/.test(s)) {
      const c = '#' + s.toLowerCase()
      customColor = c
      onColorChange(c)
    }
  }
</script>

<div class="card" style="margin:0">
  <div class="card-head">
    <div class="ci {ciClass}"><i class="ti ti-palette"></i></div>
    <div class="ctw">
      <div class="ct">{title}</div>
      {#if desc}<div class="cd">{desc}</div>{/if}
    </div>
    {#if step}<span class="step-num">{step}</span>{/if}
  </div>

  <!-- Іконка: клік по квадрату → вибір файлу (PNG/JPG/WEBP/GIF), як у збірок. -->
  <div class="row">
    <div
      class="icon-pick"
      class:has-icon={!!iconPreviewUrl || !!iconName}
      onclick={onBrowseIcon}
      role="button"
      tabindex="0"
      onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); onBrowseIcon() } }}
    >
      {#if iconPreviewUrl}
        <img src={iconPreviewUrl} alt="" />
      {:else if iconName}
        <i class="ti {iconName}" style="color:{color}"></i>
      {:else}
        <i class="ti ti-photo-plus"></i><span>{t('createPack.icon').toUpperCase()}</span>
      {/if}
    </div>
    <div class="row-label">
      <div class="rl-name">{t('createPack.icon')}</div>
      <div class="rl-sub">{t('createPack.icon.hint')}</div>
    </div>
  </div>

  <!-- Пресет-іконка (Tabler) — той самий пікер, що й у збірках. -->
  <div class="row col">
    <div class="rl-name">{t('createPack.icon.preset')}</div>
    <PresetIconPicker value={iconName} {color} onSelect={onSelectPreset} />
  </div>

  <!-- Колір: 5 пресетів + свій колір + hex-поле (усе в одному місці). -->
  <div class="row col" class:last={!onBrowseBackground}>
    <div class="rl-name">{t('createPack.color')}</div>
    <div class="color-swatches">
      {#each colorPresets as c (c)}
        <button class="sw" class:sel={color === c} style="background:{c}" onclick={() => onColorChange(c)} aria-label={c}></button>
      {/each}
      <label class="sw sw-custom" class:sel={usingCustomColor} style="--cc:{usingCustomColor ? color : customColor}" title={t('createPack.color.custom')}>
        <input type="color" value={usingCustomColor ? color : customColor} oninput={(e) => applyCustomColor(e.currentTarget.value)} />
      </label>
    </div>
    <div class="color-hex">
      <span class="rl-sub mono">#</span>
      <input
        class="input mono color-hex-input"
        value={color.replace('#', '')}
        placeholder="ff8a00"
        spellcheck="false"
        oninput={(e) => applyHex(e.currentTarget.value)}
      />
    </div>
  </div>

  {#if onBrowseBackground}
    <div class="row col" style="border-bottom:none">
      <div class="rl-name">{t('createPack.background')}</div>
      <div class="flex gap8">
        <button class="btn btn-ghost" style="justify-content:center" onclick={() => onBrowseBackground?.()}>
          <i class="ti ti-photo"></i> {t('createPack.background.choose')}
        </button>
        {#if (backgroundPreviewUrl || backgroundRemovePending) && onRemoveBackground}
          <button class="btn btn-ghost btn-danger-ghost" style="justify-content:center" onclick={() => onRemoveBackground?.()} title={t('createPack.background.remove')}>
            <i class="ti ti-x"></i> {t('createPack.background.remove')}
          </button>
        {/if}
        {#if backgroundPreviewUrl}
          <div class="ae-bg-preview" style="background-image:url('{backgroundPreviewUrl}')"></div>
        {/if}
      </div>
      <div class="ae-autosave">
        {#if autoSaveState === 'saving'}
          <span class="ae-chip"><i class="ti ti-loader spin"></i> {t('pack.basic.autoSaving')}</span>
        {:else if autoSaveState === 'saved'}
          <span class="ae-chip ok"><i class="ti ti-check"></i> {t('pack.basic.autoSaved')}</span>
        {/if}
      </div>
    </div>
  {/if}
</div>

<style>
  /* .card/.row/.rl-name/.icon-pick/.color-swatches/.sw/.color-hex — глобальні
     (style.css), тому вигляд однаковий у збірок і серверів. Тут лише те,
     що специфічне саме для цього віджета. */
  .ae-bg-preview {
    width: 72px; height: 40px; border-radius: 8px; flex-shrink: 0;
    background-size: cover; background-position: center;
    border: 1px solid var(--border);
  }
  .ae-autosave { display: flex; align-items: center; gap: 8px; margin-top: 10px; }
  .ae-chip { display: inline-flex; align-items: center; gap: 6px; font-size: 11px; font-weight: 600; color: var(--text-dim); }
  .ae-chip.ok { color: var(--green); }
  .row.last { border-bottom: none; }
</style>
