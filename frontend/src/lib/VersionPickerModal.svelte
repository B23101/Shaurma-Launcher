<script lang="ts">
  // ── VersionPickerModal ───────────────────────────────────────────────────
  // Спільне модульне вікно вибору ВЕРСІЇ (гри Minecraft, лоадера, ядра
  // сервера — будь-якого списку версій) замість нативного <select> і
  // замість SelectField у формах створення/редагування збірок і серверів.
  //
  // Навіщо окремий компонент, а не ще один SelectField: версій може бути
  // дуже багато (сотні релізів/снапшотів Minecraft, десятки білдів
  // Paper/Forge) — маленький inline-дропдаун із них незручний для пошуку
  // на телефоні й неохайно виглядає. Модалка дає повноекранний (у межах
  // діалогу) список із пошуком і великими рядками — саме те, що просив
  // юзер: "модульне вікно вибору версій" замість "тупого select".
  //
  // Компонент відповідає лише за UI ВІДКРИТОГО діалогу; сам тригер —
  // кнопка, що виглядає як звичайне поле форми (.vpm-trigger, той самий
  // візуальний контракт, що .select/.sf-trigger) — теж малюється тут,
  // щоб виклик був одним тегом у батьківському файлі:
  //
  //   <VersionPickerModal
  //     value={mcVersion}
  //     onselect={(v) => mcVersion = v}
  //     options={filteredMCVersions.map(v => ({ value: v.id, label: v.id }))}
  //     title={t('createPack.mcVersion')}
  //     icon="ti-versions"
  //   />
  //
  // Опції підтримують бейдж (напр. "рекомендовано" для версії лоадера) і
  // додатковий підпис (напр. тип версії — release/snapshot).
  import { t } from './i18n.svelte'
  import UiModal from './UiModal.svelte'

  export interface VersionOption {
    value: string
    label: string
    // Додатковий короткий підпис праворуч від лейбла в списку (напр. тип
    // версії: "release"/"snapshot", або дата випуску).
    sub?: string
    // Бейдж-зірка + підпис під курсором (напр. "рекомендована версія").
    badgeIcon?: string
    badgeTitle?: string
  }

  let {
    value,
    options,
    onselect,
    // Заголовок/іконка модалки (за замовчуванням — під версію гри).
    title = '',
    subtitle = '',
    icon = 'ti-versions',
    color = 'var(--violet)',
    placeholder = '',
    disabled = false,
    // Текст, коли список порожній (напр. "немає версій під цю Minecraft").
    emptyText = '',
    // Ширина тригера-кнопки (як minWidth у SelectField).
    minWidth = '170px',
    ariaLabel = '',
  } = $props<{
    value: string
    options: VersionOption[]
    onselect: (v: string) => void
    title?: string
    subtitle?: string
    icon?: string
    color?: string
    placeholder?: string
    disabled?: boolean
    emptyText?: string
    minWidth?: string
    ariaLabel?: string
  }>()

  let open = $state(false)
  let query = $state('')

  const current = $derived((options ?? []).find((o: VersionOption) => o.value === value))

  const filtered = $derived.by((): VersionOption[] => {
    const q = query.trim().toLowerCase()
    if (!q) return options ?? []
    return (options ?? []).filter((o: VersionOption) =>
      o.label.toLowerCase().includes(q) || o.value.toLowerCase().includes(q)
    )
  })

  function openModal() {
    if (disabled) return
    query = ''
    open = true
  }

  function pick(o: VersionOption) {
    open = false
    onselect(o.value)
  }

  function onKeydown(e: KeyboardEvent) {
    // Enter обирає перший видимий рядок — зручно набрати номер версії і
    // одразу підтвердити, не тягнучись мишкою до списку.
    if (e.key === 'Enter' && filtered.length > 0) {
      e.preventDefault()
      pick(filtered[0])
    }
  }
</script>

<button
  type="button"
  class="vpm-trigger"
  class:disabled
  style="min-width:{minWidth}"
  onclick={openModal}
  disabled={disabled}
  aria-label={ariaLabel || title || placeholder}
  title={current?.label || placeholder}
>
  <span class="vpm-trigger-label">{current?.label ?? placeholder}</span>
  <i class="ti ti-chevron-down vpm-trigger-arrow"></i>
</button>

<UiModal
  open={open}
  title={title}
  subtitle={subtitle}
  icon={icon}
  color={color}
  size="sm"
  bodyClass="no-pad"
  onclose={() => (open = false)}
>
  <div class="vpm-search-row">
    <i class="ti ti-search"></i>
    <input
      class="vpm-search-input"
      type="text"
      placeholder={t('versionPicker.search')}
      bind:value={query}
      onkeydown={onKeydown}
      autofocus
      spellcheck="false"
    />
  </div>
  <div class="vpm-list">
    {#if filtered.length === 0}
      <div class="vpm-empty"><i class="ti ti-search-off"></i> {emptyText || t('versionPicker.noResults')}</div>
    {:else}
      {#each filtered as opt (opt.value)}
        <button type="button" class="vpm-item" class:sel={opt.value === value} onclick={() => pick(opt)}>
          <span class="vpm-item-label">{opt.label}</span>
          {#if opt.sub}<span class="vpm-item-sub">{opt.sub}</span>{/if}
          {#if opt.badgeIcon}
            <i class="ti {opt.badgeIcon} vpm-item-badge" title={opt.badgeTitle || ''}></i>
          {/if}
          {#if opt.value === value}<i class="ti ti-check vpm-item-check"></i>{/if}
        </button>
      {/each}
    {/if}
  </div>
</UiModal>

<style>
  /* Тригер: той самий контракт, що .select/.sf-trigger — виглядає як
     звичайне поле форми, відкриває модалку по кліку. */
  .vpm-trigger {
    display: flex; align-items: center; gap: 8px; width: auto;
    background: var(--bg-input); border: 1px solid var(--border);
    border-radius: var(--r-md); padding: 9px 12px;
    font-size: 12px; font-weight: 600; color: var(--text);
    cursor: pointer; font-family: inherit;
    transition: border-color .15s, background .15s;
  }
  .vpm-trigger:hover:not(.disabled) { border-color: var(--border-hi); background: var(--bg-input-hi); }
  .vpm-trigger.disabled { opacity: .5; cursor: not-allowed; }
  .vpm-trigger-label {
    flex: 1; min-width: 0; text-align: left;
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .vpm-trigger-arrow { color: var(--text-mute); font-size: 14px; flex-shrink: 0; }

  /* Рядок пошуку вгорі модалки — прилипає, список під ним скролиться. */
  .vpm-search-row {
    display: flex; align-items: center; gap: 8px;
    padding: 12px 16px; border-bottom: 1px solid var(--border);
    color: var(--text-mute); font-size: 14px;
  }
  .vpm-search-input {
    flex: 1; min-width: 0; background: transparent; border: none; outline: none;
    font-size: 13px; font-weight: 600; color: var(--text); font-family: inherit;
  }
  .vpm-search-input::placeholder { color: var(--text-dim); font-weight: 500; }

  .vpm-list {
    display: flex; flex-direction: column; gap: 2px;
    padding: 8px; max-height: 360px; overflow-y: auto;
  }
  .vpm-list::-webkit-scrollbar { width: 6px; }
  .vpm-list::-webkit-scrollbar-thumb { background: var(--hover-strong); border-radius: 3px; }
  .vpm-list::-webkit-scrollbar-track { background: transparent; }

  .vpm-item {
    display: flex; align-items: center; gap: 8px; width: 100%;
    padding: 10px 12px; border: none; border-radius: var(--r-md);
    background: transparent; color: var(--text); font-family: inherit;
    font-size: 13px; font-weight: 600; text-align: left; cursor: pointer;
    transition: background .15s, box-shadow .15s;
  }
  .vpm-item:hover { background: var(--hover, rgba(255,255,255,.07)); }
  .vpm-item.sel {
    color: var(--violet);
    background: color-mix(in srgb, var(--violet) 12%, var(--hover));
    box-shadow: inset 2px 0 0 0 var(--violet);
  }
  .vpm-item-label { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .vpm-item-sub { font-size: var(--fs-min); color: var(--text-mute); flex-shrink: 0; }
  .vpm-item-badge { color: var(--yellow); font-size: 14px; flex-shrink: 0; }
  .vpm-item-check { color: var(--violet); font-size: 15px; flex-shrink: 0; }

  .vpm-empty {
    display: flex; align-items: center; justify-content: center; gap: 8px;
    padding: 26px 0; color: var(--text-mute); font-size: 12.5px;
  }
</style>
