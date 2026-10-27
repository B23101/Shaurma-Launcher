<script lang="ts">
  // Setting.svelte — один рядок налаштування. Тип контрола береться зі схеми
  // бекенду (def.type), тексти — з i18n `st.k.<ключ>` (назва) і `st.k.<ключ>.d`
  // (коротка підказка). Корінь рядка — РЕАЛЬНИЙ блок з data-setting-key:
  // саме на нього скролить і підсвічує пошук.
  //
  // invert — для параметрів, що у схемі названі «навпаки» (disableSkins,
  // serverWarnIgnoreRam…): людині показуємо позитивне формулювання
  // («Скіни увімкнено»), а в налаштування пишемо протилежне значення.
  import { t, getCustomLanguages } from '../i18n.svelte'
  import { App } from '../wails'
  import JvmArgsEditor from '../JvmArgsEditor.svelte'
  import type { SettingDef, SettingOption } from '../types'

  let {
    def, value, onchange,
    invert = false, unit = '', disabled = false, sub = false,
    label = undefined, hint = undefined,
  }: {
    def: SettingDef
    value: any
    onchange: (v: any) => void
    invert?: boolean
    unit?: string
    disabled?: boolean
    sub?: boolean
    label?: string
    hint?: string
  } = $props()

  // Текст з i18n або запасний варіант (якщо ключа немає — t() повертає ключ).
  function tx(key: string, fallback = ''): string {
    const v = t(key)
    return v === key ? fallback : v
  }
  const title = $derived(label ?? tx(`st.k.${def.key}`, t(def.label)))
  const note = $derived(hint ?? tx(`st.k.${def.key}.d`, ''))
  const optLabel = (o: SettingOption) => tx(`st.o.${def.key}.${o.value}`, t(o.label))
  const optDesc = (o: SettingOption) => tx(`st.o.${def.key}.${o.value}.d`, o.desc ? t(o.desc) : '')

  // Довгі поля (команди, шляхи, ключі) і складні контроли — під назвою на всю ширину.
  const STACK_TYPES = ['choice', 'ram', 'jvmargs', 'language']
  const stack = $derived(STACK_TYPES.includes(def.type) || def.multiline === true || def.type === 'string')

  // ── bool ──
  const on = $derived(invert ? !value : !!value)
  const toggle = () => { if (!disabled) onchange(invert ? on : !on) }

  // ── int ──
  const iMin = $derived(def.min ?? 0)
  const iMax = $derived(def.max ?? 1_000_000)
  const iStep = $derived(def.step ?? 1)
  function commitInt(raw: string) {
    const n = Number(raw)
    onchange(Math.max(iMin, Math.min(iMax, Number.isFinite(n) ? Math.round(n) : iMin)))
  }

  // ── ram ──
  const rMin = $derived(def.min ?? 1024)
  const rMax = $derived(def.max ?? 16384)
  const rStep = $derived(def.step ?? 512)
  const ramMB = $derived(Number(value ?? def.default ?? 4096))
  const fmtGB = (mb: number) => {
    const g = mb / 1024
    return `${Number.isInteger(g) ? g : g.toFixed(1)} ${t('st.unit.gb')}`
  }
  const clampRam = (n: number) => Math.max(rMin, Math.min(rMax, Math.round(n / 64) * 64 || rMin))

  // ── color ──
  const isHex = (s: any) => /^#[0-9a-fA-F]{6}$/.test(String(s ?? ''))

  // ── string-дії ──
  async function runAction() {
    if (def.action === 'browse') { const p = await App.BrowseFolder(); if (p) onchange(p) }
    else if (def.action === 'detectJava') onchange(await App.DetectJava())
    else if (def.action === 'fontFile') { const p = await App.BrowseFontFile(); if (p) onchange(p) }
  }

  // ── select: меню виноситься в <body>, щоб не обрізалось картками ──
  let open = $state(false)
  let rootEl = $state<HTMLDivElement | undefined>()
  let menuEl = $state<HTMLDivElement | undefined>()
  const current = $derived(def.options?.find((o) => o.value === value) ?? def.options?.[0])

  $effect(() => {
    if (!open) return
    const close = () => { open = false }
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') open = false }
    document.addEventListener('click', close)
    document.addEventListener('keydown', onKey)
    return () => { document.removeEventListener('click', close); document.removeEventListener('keydown', onKey) }
  })
  $effect(() => {
    if (!open || !menuEl || !rootEl) return
    const el = menuEl, root = rootEl
    document.body.appendChild(el)
    const place = () => {
      const r = root.getBoundingClientRect()
      const w = Math.max(r.width, 190)
      const below = window.innerHeight - r.bottom
      el.style.position = 'fixed'
      el.style.minWidth = `${w}px`
      el.style.left = `${Math.max(8, Math.min(r.left, window.innerWidth - w - 8))}px`
      // якщо внизу мало місця — відкриваємо вгору
      if (below < 220 && r.top > below) { el.style.top = 'auto'; el.style.bottom = `${window.innerHeight - r.top + 4}px` }
      else { el.style.bottom = 'auto'; el.style.top = `${r.bottom + 4}px` }
    }
    place()
    window.addEventListener('resize', place)
    window.addEventListener('scroll', place, true)
    return () => { window.removeEventListener('resize', place); window.removeEventListener('scroll', place, true); el.remove() }
  })
  function pick(o: SettingOption) { open = false; onchange(o.value) }
</script>

<div class="st-row" class:stack class:off={disabled} class:sub data-setting-key={def.key}>
  <div class="st-row-text">
    <div class="st-row-name">{title}</div>
    {#if note}<div class="st-row-hint">{note}</div>{/if}
  </div>

  <div class="st-row-ctl">
    {#if def.type === 'bool'}
      <button type="button" class="st-switch" class:on role="switch" aria-checked={on}
              aria-label={title} {disabled} onclick={toggle}></button>

    {:else if def.type === 'select'}
      <div class="st-select" bind:this={rootEl}>
        <button type="button" class="st-select-btn" class:open aria-haspopup="listbox" aria-expanded={open}
                aria-label={title} {disabled} onclick={(e) => { e.stopPropagation(); open = !open }}>
          <span>{current ? optLabel(current) : ''}</span><i class="ti ti-chevron-down"></i>
        </button>
        {#if open}
          <div class="st-menu" role="listbox" bind:this={menuEl} onclick={(e) => e.stopPropagation()}>
            {#each def.options ?? [] as o (o.value)}
              <button type="button" role="option" aria-selected={value === o.value} class:sel={value === o.value} onclick={() => pick(o)}>
                {optLabel(o)}{#if value === o.value}<i class="ti ti-check"></i>{/if}
              </button>
            {/each}
          </div>
        {/if}
      </div>

    {:else if def.type === 'choice'}
      <div class="st-choices" role="radiogroup" aria-label={title}>
        {#each def.options ?? [] as o (o.value)}
          <button type="button" role="radio" aria-checked={value === o.value} class="st-choice" class:sel={value === o.value}
                  {disabled} onclick={() => onchange(o.value)}>
            {#if o.icon}<i class="ti {o.icon}"></i>{/if}
            <span class="st-choice-body">
              <span class="st-choice-name">{optLabel(o)}</span>
              {#if optDesc(o)}<span class="st-choice-desc">{optDesc(o)}</span>{/if}
            </span>
            <span class="st-choice-dot"></span>
          </button>
        {/each}
      </div>

    {:else if def.type === 'int'}
      <input class="st-input num mono" type="number" value={value ?? def.default ?? iMin}
             min={iMin} max={iMax} step={iStep} {disabled} aria-label={title}
             onchange={(e) => commitInt((e.currentTarget as HTMLInputElement).value)} />
      {#if unit}<span class="st-unit">{unit}</span>{/if}

    {:else if def.type === 'ram'}
      <div class="st-slider">
        <input type="range" min={rMin} max={rMax} step={rStep} value={ramMB} aria-label={title}
               oninput={(e) => onchange(clampRam(Number((e.currentTarget as HTMLInputElement).value)))} />
        <span class="st-slider-val">{fmtGB(ramMB)}</span>
        <input class="st-input num mono" type="number" min={rMin} max={rMax} step={rStep} value={ramMB}
               aria-label="{title} ({t('st.unit.mb')})"
               onchange={(e) => onchange(clampRam(Number((e.currentTarget as HTMLInputElement).value)))} />
        <span class="st-unit">{t('st.unit.mb')}</span>
      </div>

    {:else if def.type === 'color'}
      <div class="st-color">
        <label class="st-color-dot" style="background:{isHex(value) ? value : '#ffffff'}" title={t('st.pickColor')}>
          <input type="color" value={isHex(value) ? value : '#ffffff'} aria-label={title}
                 oninput={(e) => onchange((e.currentTarget as HTMLInputElement).value)} />
        </label>
        <input class="st-input mono" style="width:104px" value={value || ''} placeholder="#rrggbb" aria-label="{title} (hex)"
               onchange={(e) => {
                 const v = (e.currentTarget as HTMLInputElement).value.trim()
                 if (/^#?[0-9a-fA-F]{6}$/.test(v)) onchange(v.startsWith('#') ? v : '#' + v)
               }} />
      </div>

    {:else if def.type === 'jvmargs'}
      <div style="width:100%"><JvmArgsEditor value={value ?? ''} onchange={(v: string) => onchange(v)} /></div>

    {:else if def.type === 'language'}
      <div class="st-pills">
        {#each def.options ?? [] as o (o.value)}
          <button type="button" class="st-pill" class:sel={value === o.value} onclick={() => onchange(o.value)}>{optLabel(o)}</button>
        {/each}
        {#each Object.values(getCustomLanguages()) as cl (cl.code)}
          <button type="button" class="st-pill" class:sel={value === cl.code} onclick={() => onchange(cl.code)}>{cl.name}</button>
        {/each}
      </div>

    {:else if def.multiline}
      <textarea class="st-input mono grow" rows="3" value={value ?? ''} placeholder={def.placeholder} {disabled}
                aria-label={title} onchange={(e) => onchange((e.currentTarget as HTMLTextAreaElement).value)}></textarea>

    {:else}
      <input class="st-input mono grow" type={def.key === 'consoleAIKey' || def.key === 'curseForgeApiKey' ? 'password' : 'text'}
             autocomplete="off" spellcheck="false" value={value ?? ''} placeholder={def.placeholder} {disabled}
             aria-label={title} onchange={(e) => onchange((e.currentTarget as HTMLInputElement).value)} />
      {#if def.action}
        <button type="button" class="st-iconbtn" onclick={runAction} aria-label={t('st.browse')} title={t('st.browse')}>
          <i class="ti {def.action === 'detectJava' ? 'ti-search' : def.action === 'fontFile' ? 'ti-file-typography' : 'ti-folder'}"></i>
        </button>
      {/if}
    {/if}
  </div>
</div>
