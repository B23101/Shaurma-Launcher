<script lang="ts">
  // ── SettingRow: універсальний рядок налаштування ─────────────────────
  // Рендерить контрол за типом def.type (див. config.SettingsSchema()):
  //   bool     → toggle
  //   select   → випадаючий список
  //   language → сітка мов (uk/en/auto)
  //   accent   → сітка акцентних кольорів
  //   ram      → слайдер + числове поле (МБ)
  //   string   → текстове поле (+ кнопка дії browse/detectJava)
  import { t } from './i18n.svelte'
  import { App } from './wails'
  import { ACCENTS } from './theme.svelte'
  import type { SettingDef, SettingOption } from './types'

  let { def, value, onchange } = $props<{ def: SettingDef; value: any; onchange: (v: any) => void }>()

  const LANG_FLAGS: Record<string, string> = { uk: '🇺🇦', en: '🇬🇧', auto: '🌐' }

  function setVal(v: any) { onchange(v) }

  async function runAction() {
    if (def.action === 'browse') {
      const p = await App.BrowseFolder()
      if (p) onchange(p)
    } else if (def.action === 'detectJava') {
      onchange(await App.DetectJava())
    } else if (def.action === 'fontFile') {
      const p = await App.BrowseFontFile()
      if (p) onchange(p)
    }
  }

  const ramMB = $derived(value ?? 4096)
  const ramMin = $derived(def.min ?? 1024)
  const ramMax = $derived(def.max ?? 16384)
  const ramPct = $derived(ramMax > ramMin ? ((ramMB - ramMin) / (ramMax - ramMin)) * 100 : 0)

  const intMin = $derived(def.min ?? 0)
  const intMax = $derived(def.max ?? 100000)
  const intStep = $derived(def.step ?? 1)

  // ── Кастомний випадаючий список (select) ──────────────────────────────
  // Замінює нативний <select>, щоб меню виглядало як інший UI лаунчера
  // (без ванільних стрілок ОС) і коректно перемикалося на світлу тему.
  let ddOpen = $state(false)

  const currentOption = $derived(def.options?.find((o: SettingOption) => o.value === value) ?? def.options?.[0])

  // Закриваємо меню кліком поза ним.
  $effect(() => {
    if (!ddOpen) return
    const close = () => { ddOpen = false }
    document.addEventListener('click', close)
    return () => document.removeEventListener('click', close)
  })

  function pick(opt: SettingOption) {
    ddOpen = false
    setVal(opt.value)
  }
</script>

<div class="row">
  <div class="row-label">
    <div class="rl-name">{t(def.label)}</div>
    {#if def.desc}<div class="rl-sub">{t(def.desc)}</div>{/if}
  </div>

  {#if def.type === 'bool'}
    <div class="toggle" class:on={!!value} onclick={() => setVal(!value)}></div>

  {:else if def.type === 'select'}
    <div class="select" class:open={ddOpen}>
      <i class="ti ti-adjustments-horizontal lf"></i>
      <span class="sel-label">{t(currentOption?.label ?? '')}</span>
      <i class="ti ti-chevron-down tr"></i>
      <button class="sel-trigger" aria-label={t(def.label)} onclick={(e) => { e.stopPropagation(); ddOpen = !ddOpen }}></button>
      {#if ddOpen}
        <div class="dd-menu" onclick={(e) => e.stopPropagation()}>
          {#each def.options ?? [] as opt}
            <button class="dd-item" class:sel={value === opt.value} onclick={() => pick(opt)}>
              {t(opt.label)}
            </button>
          {/each}
        </div>
      {/if}
    </div>

  {:else if def.type === 'language'}
    <div class="lang-grid" style="min-width:260px">
      {#each def.options ?? [] as opt}
        <div class="lang-opt" class:sel={value === opt.value} onclick={() => setVal(opt.value)}>
          <div class="flag">{LANG_FLAGS[opt.value] ?? '🌐'}</div>
          <div class="ln">{t(opt.label)}</div>
        </div>
      {/each}
    </div>

  {:else if def.type === 'accent'}
    <div class="accent-grid">
      {#each def.options ?? [] as opt}
        <div class="accent-opt" class:sel={value === opt.value} style="background:{opt.value === 'custom' ? 'conic-gradient(from 180deg, #ff8a00, #ff3d81, #8a2be2, #00b3ff, #10b981, #ff8a00)' : (ACCENTS[opt.value]?.main ?? '#888')}"
             title={t(opt.label)} onclick={() => setVal(opt.value)}>
          {#if value === opt.value}<i class="ti ti-check"></i>{/if}
        </div>
      {/each}
    </div>

  {:else if def.type === 'color'}
    <div class="color-ctl">
      <!-- Клік по плашці відкриває НАТИВНИЙ пікер кольору (input type=color).
           БАГ (виправлено): раніше тут був лише hex-інпут + пресети — щоб
           поставити свій колір, користувач мав ВРУЧНУ вписувати #rrggbb.
           Тепер плашка = label поверх прихованого нативного пікера, hex-поле
           і пресети лишились як додаткові способи. -->
      <label class="color-dot" style="background:{/^#[0-9a-fA-F]{6}$/.test(value || '') ? value : '#ffffff'}" title={t('settings.colorPicker')} aria-label={t('settings.colorPicker')}>
        <input type="color" value={/^#[0-9a-fA-F]{6}$/.test(value || '') ? value : '#ffffff'} oninput={(e: any) => onchange(e.target.value)} />
      </label>
      <input class="input mono" style="max-width:104px" value={value || ''} placeholder="#rrggbb"
             onchange={(e: any) => { const v = (e.target.value || '').trim(); if (/^#?[0-9a-fA-F]{6}$/.test(v)) onchange(v.startsWith('#') ? v : '#' + v) }} />
      <div class="color-presets">
        {#each ['#ff8a00','#7b4dff','#10b981','#3b82f6','#ef4444','#f59e0b','#8fd3ff','#ffd166','#ff6b6b'] as c}
          <button class="sw" class:sel={value === c} style="background:{c}" onclick={() => onchange(c)} aria-label={c}></button>
        {/each}
      </div>
    </div>

  {:else if def.type === 'int'}
    <div class="int-ctl">
      <input class="input mono" style="max-width:120px" type="number" value={value ?? def.default ?? 0} step={intStep} min={intMin} max={intMax}
             onchange={(e: any) => onchange(Math.max(intMin, Math.min(intMax, Number(e.target.value) || intMin)))} />
    </div>

  {:else if def.type === 'ram'}
    {@const min = def.min ?? 1024}
    {@const max = def.max ?? 16384}
    {@const step = def.step ?? 512}
    <div class="ram-ctl">
      <div class="ram-track" style="width:150px"
           onclick={(e: any) => {
             const r = (e.currentTarget as HTMLElement).getBoundingClientRect()
             let pct = (e.clientX - r.left) / r.width
             pct = Math.max(0, Math.min(1, pct))
             const v = Math.round((min + pct * (max - min)) / step) * step
             onchange(Math.max(min, Math.min(max, v)))
           }}>
        <div class="ram-fill" style="width:{ramPct}%"></div>
        <div class="ram-knob" style="left:{ramPct}%"></div>
      </div>
      <input class="input mono" style="max-width:110px" type="number" value={ramMB} step={step} min={min} max={max}
             onchange={(e: any) => onchange(Math.max(min, Math.min(max, Number(e.target.value))))} />
    </div>

  {:else if def.multiline}
    <div class="folder-row" style="flex:1; align-items:flex-start">
      <textarea class="input mono" rows="3" style="flex:1; resize:vertical; font-size:11px" value={value ?? ''} placeholder={def.placeholder}
                onchange={(e: any) => setVal(e.target.value)}></textarea>
    </div>

  {:else}
    <div class="folder-row" style="flex:1">
      <input class="input mono" style="flex:1" value={value ?? ''} placeholder={def.placeholder}
             onchange={(e: any) => setVal(e.target.value)} />
      {#if def.action === 'browse'}
        <button class="btn btn-ghost" onclick={runAction} title={t('wiz.folders.browse')} aria-label={t('wiz.folders.browse')}><i class="ti ti-folder"></i></button>
      {:else if def.action === 'detectJava'}
        <button class="btn btn-ghost" onclick={runAction} title={t('wiz.folders.findJava')} aria-label={t('wiz.folders.findJava')}><i class="ti ti-search"></i></button>
      {:else if def.action === 'fontFile'}
        <button class="btn btn-ghost" onclick={runAction} title={t('settings.font.custom')} aria-label={t('settings.font.custom')}><i class="ti ti-file-typography"></i></button>
      {/if}
    </div>
  {/if}
</div>

<style>
  .sel-label { font-size: 12px; font-weight: 600; }
  /* Кастомний dropdown: клікабельна прозора кнопка поверх тригера. */
  .sel-trigger {
    position:absolute; inset:0; width:100%; height:100%;
    border:none; background:transparent; cursor:pointer; border-radius:inherit;
  }
  .select.open { border-color:var(--violet); box-shadow:0 0 0 2px rgba(123,77,255,.2); }
  .dd-menu {
    position:absolute; top:calc(100% + 4px); left:0; min-width:100%; z-index:60;
    background:var(--surface-2, #1d1d1d); border:1px solid var(--border);
    border-radius:var(--r-md); box-shadow:0 8px 24px rgba(0,0,0,.4);
    padding:4px; display:flex; flex-direction:column; gap:2px;
    max-height:260px; overflow-y:auto;
  }
  .dd-item {
    display:flex; align-items:center; gap:8px; width:100%;
    padding:8px 10px; border:none; border-radius:var(--r-sm);
    background:transparent; color:var(--text); font-family:inherit;
    font-size:12px; font-weight:600; text-align:left; cursor:pointer;
  }
  .dd-item:hover { background:var(--hover, rgba(255,255,255,.07)); }
  .dd-item.sel { color:var(--violet); }
  .dd-menu::-webkit-scrollbar { width:6px; }
  .dd-menu::-webkit-scrollbar-thumb { background:var(--hover-strong); border-radius:3px; }
  .dd-menu::-webkit-scrollbar-track { background:transparent; }
  .ram-ctl { display: flex; align-items: center; gap: 10px; }
  .ram-ctl .ram-track { height: 6px; margin: 0; }
  .color-ctl { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
  .color-dot {
    position: relative; width: 34px; height: 34px; border-radius: 10px; flex-shrink: 0;
    border: 1px solid var(--border-hi); box-shadow: 0 2px 8px rgba(0,0,0,.35);
    cursor: pointer; overflow: hidden; display: block;
    transition: transform .12s, box-shadow .12s;
  }
  .color-dot:hover { transform: scale(1.08); box-shadow: 0 0 0 3px rgba(255,255,255,.06); }
  .color-dot input {
    position: absolute; inset: -12px; width: 60px; height: 60px;
    opacity: 0; cursor: pointer; border: none; padding: 0;
  }
  .color-presets { display: flex; gap: 6px; flex-wrap: wrap; }
  .color-presets .sw {
    width: 22px; height: 22px; border-radius: 7px;
    border: 1px solid var(--border); cursor: pointer;
    transition: transform .12s, border-color .12s, box-shadow .12s;
  }
  .color-presets .sw:hover { transform: scale(1.15); border-color: var(--border-hi); }
  .color-presets .sw.sel { border-color: var(--orange); box-shadow: 0 0 0 2px rgba(255,138,0,.35); }
  .int-ctl { display: flex; align-items: center; }
</style>
