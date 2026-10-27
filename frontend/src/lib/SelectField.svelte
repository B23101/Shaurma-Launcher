<script lang="ts">
  // ── SelectField: випадаючий список у стилі лаунчера ─────────────────────
  // Замінює нативний <select> у сторінках збірок, щоб меню виглядало як
  // решта UI лаунчера (без ванільних стрілок ОС). Підтримує опції з
  // іконкою (ti-*) або аватаром (head — URL голови акаунта).
  //
  // editable — режим «комбобокс»: можна вводити своє значення (напр. IP
  // сервера вручну), а список опцій фільтрується за введеним текстом.

  export interface SelectOption {
    value: string
    label: string
    icon?: string
    head?: string
    // Колір збірки (hex/var) — фон квадрата-іконки в стилі sidebar
    // (color-mix), коли показуємо `icon` як fallback замість PNG.
    color?: string
    // Міні-прев'ю плаща (dataURL, напр. для пресетів Гардероба) — малюється
    // ПОРУЧ з head, щоб опція показувала «голова + плащ» одним блоком.
    cape?: string
    // Іконка-бейдж СПРАВА від тексту опції (ti-*, напр. 'ti-star'
    // для «рекомендована версія») — на відміну від `icon`, який малюється
    // ЗЛІВА і як маркер поточного вибору в тригері.
    badgeIcon?: string
    badgeTitle?: string
  }

  let {
    value,
    options,
    onchange,
    placeholder = '',
    disabled = false,
    minWidth = '160px',
    ariaLabel = '',
    editable = false,
    emptyText = '',
    // 'md' (типово, 20px іконки — компактні односрокові списки: мова,
    // масштаб GUI, версії тощо) або 'lg' (28px іконки, вищі рядки —
    // списки ЗБІРОК з іконкою/кольором, щоб віджет читався так само, як
    // картки в PackPicker, а не тулився в 20px). При 'lg' меню також
    // показує рівно ~5 рядків без прокрутки замість ~8 — довший список
    // все одно скролиться, але видно одразу менше й читабельніше.
    size = 'md' as 'md' | 'lg',
    // `class` — зарезервоване слово JS, тому в деструктуризації
    // перейменовуємо (зовнішній проп клас передається як `class`).
    class: className = ''
  } = $props<{
    value: string
    options: SelectOption[]
    onchange: (v: string) => void
    placeholder?: string
    disabled?: boolean
    minWidth?: string
    ariaLabel?: string
    class?: string
    // Режим комбобокса: текстове поле з фільтрацією опцій (ручне введення).
    editable?: boolean
    // Текст у відкритому меню, коли жодна опція не збігається (editable).
    emptyText?: string
    size?: 'md' | 'lg'
  }>()

  let open = $state(false)
  let rootEl = $state<HTMLDivElement | undefined>()
  let menuEl = $state<HTMLDivElement | undefined>()

  const current = $derived((options ?? []).find((o: SelectOption) => o.value === value))


  // Опції, видимі у меню: у режимі editable фільтруємо за введеним текстом
  // (підказки server.dat не заважають вводити свою адресу).
  const filteredOptions = $derived.by((): SelectOption[] => {
    if (!editable || !value) return options
    const q = value.trim().toLowerCase()
    if (!q) return options
    return (options ?? []).filter((o: SelectOption) =>
      o.value.toLowerCase().includes(q) || o.label.toLowerCase().includes(q)
    )
  })

  $effect(() => {
    if (!open) return
    const close = () => { open = false }
    // Esc закриває меню (стандартна поведінка дропдауна).
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') open = false }
    document.addEventListener('click', close)
    window.addEventListener('keydown', onKey, true)
    return () => {
      document.removeEventListener('click', close)
      window.removeEventListener('keydown', onKey, true)
    }
  })

  // ── Портал меню в document.body ──
  // Картки (.card) мають backdrop-filter, який створює окремий stacking
  // context: position:absolute-меню всередині картки малювалось би ПІД
  // наступною карткою («селект під елементами»). Тому відкрите меню
  // переносимо в кінець body і позиціюємо за координатами тригера —
  // воно завжди поверх усього інтерфейсу.
  $effect(() => {
    if (!open || !menuEl || !rootEl) return
    // ⚠️ ВАЖЛИВО: захоплюємо реальний DOM-вузол у ЛОКАЛЬНУ змінну, а не
    // працюємо з реактивним `menuEl` напряму. Коли `open` стає false,
    // Svelte знищує блок {#if open} і bind:this ОБНУЛЮЄ menuEl ДО запуску
    // cleanup цього ефекту — `menuEl.remove()` у cleanup падав би з
    // "Cannot read properties of null (reading 'remove')" (саме так і було:
    // краш при виборі опції будь-якого дропдауна). Локальна `el` — це
    // живий вузол, remove() на вже від'єднаному — безпечний no-op.
    const el = menuEl
    const root = rootEl
    document.body.appendChild(el)
    const pos = () => {
      const r = root.getBoundingClientRect()
      const mw = Math.min(Math.max(r.width, 200), window.innerWidth - 16)
      el.style.position = 'fixed'
      el.style.top = `${Math.min(r.bottom + 4, Math.max(8, window.innerHeight - 60))}px`
      el.style.left = `${Math.max(8, Math.min(r.left, window.innerWidth - mw - 8))}px`
      el.style.minWidth = `${mw}px`
    }
    pos()
    window.addEventListener('resize', pos)
    window.addEventListener('scroll', pos, true)
    return () => {
      window.removeEventListener('resize', pos)
      window.removeEventListener('scroll', pos, true)
      // Гарантія від «завислого» меню: меню портоване в document.body, і якщо
      // компонент розмонтовують з відкритим дропдауном (напр. перейшли з
      // розділу збірки), воно МАЄ бути прибране з body явно — інакше лишається
      // ghost-елементом поверх інтерфейсу (z-index вище за все) і «висить»
      // навіть після виходу з розділу. remove() на вже від'єднаному вузлі — no-op.
      el.remove()
    }
  })

  function pick(o: SelectOption) {
    open = false
    onchange(o.value)
  }

  function toggle(e: Event) {
    if (disabled) return
    e.stopPropagation()
    open = !open
  }

  // Битий head (URL, що не завантажився — типово токенізований pack-asset
  // URL, переданий напряму замість через packAsset()) ховаємо і падаємо
  // назад на кольорову іконку-заглушку (icon/color), той самий fallback,
  // що показує PackPicker/PackCompareChip для збірок без завантаженої
  // картинки — щоб «биту картинку» ніколи не було видно в жодному дропдауні.
  let brokenHeads = $state<Set<string>>(new Set())
  function markBroken(src: string) {
    if (brokenHeads.has(src)) return
    brokenHeads = new Set(brokenHeads).add(src)
  }
</script>

<div class="sf {className}" class:open class:disabled={disabled} class:sf-lg={size === 'lg'} style="min-width:{minWidth};width:{minWidth === '100%' ? '100%' : 'auto'}" bind:this={rootEl}>
  {#if editable}
    <input
      class="sf-input"
      type="text"
      value={value}
      placeholder={placeholder}
      disabled={disabled}
      aria-label={ariaLabel || placeholder}
      spellcheck="false"
      oninput={(e: any) => { open = true; onchange(e.currentTarget.value) }}
      onfocus={() => { if (!disabled) open = true }}
      onclick={(e) => { if (disabled) return; e.stopPropagation() }}
      onkeydown={(e) => { if (e.key === 'Enter' || e.key === 'Escape') open = false }}
    />
    <i class="ti ti-chevron-down tr"></i>
  {:else}
    <div class="sf-trigger" role="button" tabindex={disabled ? -1 : 0}
         aria-label={ariaLabel || current?.label || placeholder}
         title={current?.label || placeholder}
         onclick={toggle}>
      {#if current?.head && !brokenHeads.has(current.head)}
        <span class="sf-avatars">
          <img class="sf-head" src={current.head} alt="" onerror={() => markBroken(current!.head!)} />
          {#if current?.cape}
            <img class="sf-cape" src={current.cape} alt="" />
          {/if}
        </span>
      {:else if current?.icon || current?.head}
        <span class="sf-ic" style={current.color ? `--pcol:${current.color}` : undefined}>
          <i class="ti {current.icon || 'ti-package'}"></i>
        </span>
      {/if}
      <span class="sf-label">{current?.label ?? placeholder}</span>
      <i class="ti ti-chevron-down tr"></i>
    </div>
  {/if}
  {#if open && !disabled}
    <div class="sf-menu" class:sf-lg={size === 'lg'} bind:this={menuEl} onclick={(e) => e.stopPropagation()}>
      {#each filteredOptions as opt}
        <button class="sf-item" class:sel={value === opt.value} onclick={() => pick(opt)} title={opt.label}>
          {#if opt.head && !brokenHeads.has(opt.head)}
            <span class="sf-avatars">
              <img class="sf-head" src={opt.head} alt="" onerror={() => markBroken(opt.head!)} />
              {#if opt.cape}
                <img class="sf-cape" src={opt.cape} alt="" />
              {/if}
            </span>
          {:else if opt.icon || opt.head}
            <span class="sf-ic" style={opt.color ? `--pcol:${opt.color}` : undefined}>
              <i class="ti {opt.icon || 'ti-package'}"></i>
            </span>
          {/if}
          <span class="sf-label">{opt.label}</span>
          {#if opt.badgeIcon}
            <i class="ti {opt.badgeIcon} sf-badge" title={opt.badgeTitle || ''}></i>
          {/if}
        </button>
      {/each}
      {#if editable && filteredOptions.length === 0 && emptyText}
        <div class="sf-empty">{emptyText}</div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .sf { position: relative; display: inline-flex; }
  .sf-trigger {
    display: flex; align-items: center; gap: 8px; width: 100%;
    background: var(--bg-input); border: 1px solid var(--border);
    border-radius: var(--r-md); padding: 9px 32px 9px 12px;
    font-size: 12px; font-weight: 600; color: var(--text);
    cursor: pointer; font-family: inherit;
    transition: border-color .15s;
  }
  .sf-trigger:hover { border-color: var(--border-hi); background: var(--bg-input-hi); }
  .sf.open .sf-trigger, .sf.open .sf-input { border-color: var(--violet); box-shadow: 0 0 0 2px rgba(123,77,255,.2); }
  .sf.disabled .sf-trigger, .sf.disabled .sf-input { opacity: .5; cursor: not-allowed; }
  .sf-trigger .lf { color: var(--orange); font-size: 14px; }
  .sf-ic {
    width: 20px; height: 20px; min-width: 20px; border-radius: 6px; flex-shrink: 0;
    display: inline-flex; align-items: center; justify-content: center;
    background: color-mix(in srgb, var(--pcol, var(--orange)) 16%, var(--bg-input-hi));
    color: var(--pcol, var(--orange)); font-size: 12px;
  }
  .sf-item .sf-ic { font-size: 11px; }
  .sf .tr {
    position: absolute; right: 10px; top: 50%; transform: translateY(-50%);
    color: var(--text-mute); font-size: 14px; pointer-events: none;
  }
  .sf-label { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  /* Голова + плащ одним блоком (пресети Гардероба, акаунти з плащем). */
  .sf-avatars { display: inline-flex; align-items: center; gap: 3px; flex-shrink: 0; }
  .sf-head { width: 20px; height: 20px; border-radius: 6px; object-fit: cover; flex-shrink: 0; }
  .sf-cape {
    width: 11px; height: 13px; border-radius: 2px;
    object-fit: cover; object-position: top center; flex-shrink: 0;
    image-rendering: pixelated;
  }

  /* Режим комбобокса (ручне введення, напр. IP сервера) */
  .sf-input {
    width: 100%; min-width: 0;
    background: var(--bg-input); border: 1px solid var(--border);
    border-radius: var(--r-md); padding: 9px 32px 9px 12px;
    font-size: 12px; font-weight: 600; color: var(--text);
    font-family: var(--font-mono); outline: none;
    transition: border-color .15s, box-shadow .15s;
  }
  .sf-input::placeholder { color: var(--text-dim); font-family: var(--font-sans); font-weight: 500; }
  .sf-input:focus { border-color: var(--violet); box-shadow: 0 0 0 2px rgba(123,77,255,.2); }

  .sf-menu {
    position: absolute; top: calc(100% + 4px); left: 0; min-width: 100%;
    max-width: min(520px, calc(100vw - 16px));
    z-index: 2147483000;
    background: var(--bg-card); border: 1px solid var(--border);
    border-radius: var(--r-md); box-shadow: 0 8px 24px rgba(0,0,0,.4);
    padding: 4px; display: flex; flex-direction: column; gap: 2px;
    max-height: 260px; overflow-y: auto;
  }
  .sf-item {
    display: flex; align-items: center; gap: 8px; width: 100%; max-width: 100%;
    padding: 8px 10px; border: none; border-radius: var(--r-md);
    background: transparent; color: var(--text); font-family: inherit;
    font-size: 12px; font-weight: 600; text-align: left; cursor: pointer;
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
    transition: background .15s, box-shadow .15s;
  }
  /* Довгі лейбли (напр. версії модів у модульних вікнах) НЕ розтягують
     меню за межі модалки/вікна: пункт обрізається ellipsis, а повний
     текст видно через title-тултіп. ВАЖЛИВО: flex: 1 лише для ТЕКСТОВОГО
     лейбла — раніше селектор .sf-item span застосовувався і до іконок
     (.sf-ic/.sf-avatars), тож вони розтягувались на всю ширину пункту,
     а лейбл стискався («кривий макет вибору збірки в міграції/створенні»). */
  .sf-item .sf-label { overflow: hidden; text-overflow: ellipsis; flex: 1; min-width: 0; }
  .sf-item .sf-ic, .sf-item .sf-avatars { flex-shrink: 0; }
  .sf-item:hover { background: var(--hover, rgba(255,255,255,.07)); }
  /* Вибраний пункт — той самий «модульний» патерн, що й PackPicker.pp-row.sel:
     тонована заливка кольором опції + ліва смужка-індикатор замість
     самого лише кольору тексту (менш помітно в темній темі). */
  .sf-item.sel {
    color: var(--pcol, var(--violet));
    background: color-mix(in srgb, var(--pcol, var(--violet)) 12%, var(--hover));
    box-shadow: inset 2px 0 0 0 var(--pcol, var(--violet));
  }
  .sf-item > i { color: var(--orange); }
  .sf-item .sf-head { width: 20px; height: 20px; }
  /* Бейдж «рекомендована версія» — зірка справа замість тексту "(recommended)",
     компактніше і читабельніше в вузькому списку. */
  .sf-item .sf-badge { flex-shrink: 0; color: var(--yellow); font-size: 13px; }
  .sf-empty { padding: 10px 12px; font-size: 11.5px; color: var(--text-dim); text-align: center; }
  .sf-menu::-webkit-scrollbar { width: 6px; }
  .sf-menu::-webkit-scrollbar-thumb { background: var(--hover-strong); border-radius: 3px; }
  .sf-menu::-webkit-scrollbar-track { background: transparent; }

  /* ── Розмір 'lg': списки ЗБІРОК (іконка+назва+loader·версія) — рядок
       читається як картка PackPicker, не як компактний односроковий
       список мов/масштабу. Висота рядка ~44px (28px іконка + паддінги +
       gap 2px) × 5 видимих рядків + паддінг контейнера 8px ≈ 228px —
       рівно 5 пунктів влазить без скролу, шостий вже під прокруткою.
       ВАЖЛИВО: .sf-menu портується в document.body (щоб не губитись під
       backdrop-filter карток — див. $effect вище), тому фізично НЕ є
       нащадком .sf-lg у DOM — предковий селектор `.sf-lg .sf-menu` там
       ніколи б не спрацював. Клас sf-lg тому продубльовано прямо на
       .sf-menu (див. розмітку), і кожне правило нижче написане так, щоб
       працювати однаково в обох контекстах — усередині .sf (тригер) і
       окремо на порталованому .sf-menu. */
  .sf-lg.sf .sf-trigger, .sf-lg.sf .sf-input { padding: 11px 34px 11px 12px; }
  .sf-lg .sf-ic { width: 28px; height: 28px; min-width: 28px; border-radius: 8px; font-size: 15px; }
  .sf-lg .sf-item .sf-ic { font-size: 13px; }
  .sf-lg .sf-head { width: 28px; height: 28px; border-radius: 8px; }
  .sf-lg .sf-item .sf-head { width: 28px; height: 28px; }
  .sf-lg .sf-cape { width: 15px; height: 18px; }
  .sf-lg .sf-label { font-size: 13px; }
  .sf-lg .sf-item { padding: 9px 11px; gap: 10px; }
  .sf-menu.sf-lg { max-height: 228px; padding: 6px; gap: 3px; }
</style>
