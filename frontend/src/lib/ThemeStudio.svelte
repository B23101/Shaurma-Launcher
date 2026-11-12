<script lang="ts">
  // ── ThemeStudio — редактор власної теми ─────────────────────────────────
  // Було: три кольори (фон / картки / текст) — і все інше вираховувалось само,
  // тому підкрутити, скажімо, лише бічну панель чи рамки було неможливо.
  // Стало: основа (ті самі три кольори) + «тонке налаштування»: бічна панель,
  // панелі/меню, поля вводу, рамки, другорядний текст, скруглення, тіні. Кожна
  // тонка ручка за замовчуванням «Авто» (похідна від основи — тож стара тема
  // виглядає як раніше), а задана вручну — перекриває авто.
  //
  // Прев'ю малюється тими ж customThemeVars(), що й applyTheme, тож збігається
  // з реальним інтерфейсом. Компонент лише читає/пише settings через onchange.
  import { t } from './i18n.svelte'
  import { toast } from './toast.svelte'
  import {
    customThemeVars, parseThemeExtra, stringifyThemeExtra, mixHex, RADIUS_MIN, RADIUS_MAX,
    type ThemeExtra, type ThemeDepth,
  } from './theme.svelte'

  interface Props {
    bg: string
    card: string
    text: string
    extra: string
    onchange: (key: string, value: any) => void
  }
  let { bg, card, text, extra, onchange }: Props = $props()

  const HEX = /^#[0-9a-fA-F]{6}$/

  type Preset = { bg: string; card: string; text: string; extra?: ThemeExtra }
  // Готові теми: тепер вони можуть нести й тонкі параметри (темніша бічна панель,
  // «заглиблені» поля, м'якші рамки), а не лише три кольори.
  const PRESETS: Preset[] = [
    { bg: '#0f0f0f', card: '#161616', text: '#ffffff' },
    { bg: '#0b1020', card: '#151c36', text: '#e9edff', extra: { sidebar: '#0e1428', input: '#0f1530', border: '#232c52' } },
    { bg: '#12101f', card: '#1d1933', text: '#f1eaff', extra: { sidebar: '#171428', input: '#141126', border: '#2c2650' } },
    { bg: '#0d1510', card: '#16251b', text: '#e9f5ec', extra: { sidebar: '#101b14', input: '#0f1a13', border: '#24402d' } },
    { bg: '#16120d', card: '#241b11', text: '#f6ecd9', extra: { sidebar: '#1c150d', input: '#19130c', border: '#3d2d1a' } },
    { bg: '#101418', card: '#1a212a', text: '#e6edf3', extra: { sidebar: '#141a20', input: '#12171d', border: '#2a3541' } },
    { bg: '#f5f5f2', card: '#ffffff', text: '#1d1d1d', extra: { sidebar: '#fbfbf9', input: '#ecece7', border: '#dcdcd5' } },
    { bg: '#f1eef7', card: '#ffffff', text: '#2b2536', extra: { sidebar: '#f8f6fc', input: '#ebe7f3', border: '#dbd5e8' } },
  ]

  const x = $derived(parseThemeExtra(extra))
  const live = $derived(customThemeVars(
    HEX.test(bg) ? bg : '#0f0f0f', HEX.test(card) ? card : '#161616', HEX.test(text) ? text : '#ffffff', extra))
  // Прев'ю читає ті самі токени, що й інтерфейс; радіуси/тіні, яких тема не чіпає,
  // задаємо явно — інакше блок успадкує їх з :root (що теж ок, але так надійніше).
  const previewStyle = $derived(
    '--r-sm:8px;--r-md:10px;--r-lg:16px;--r-xl:24px;' +
    Object.entries(live.vars).map(([k, v]) => `${k}:${v}`).join(';'))

  // Що означає «Авто» для кожного параметра — показуємо як кольорову плашку.
  const autoOf = $derived({
    sidebar: card, panel: card, input: card,
    border: HEX.test(bg) && HEX.test(text) ? mixHex(bg, text, .11) : '#2a2a2a',
    muted: HEX.test(bg) && HEX.test(text) ? mixHex(bg, text, .7) : '#a0a0a0',
  } as Record<ColorKey, string>)

  type ColorKey = 'sidebar' | 'panel' | 'input' | 'border' | 'muted'
  const FINE: { key: ColorKey; icon: string }[] = [
    { key: 'sidebar', icon: 'ti-layout-sidebar' },
    { key: 'panel', icon: 'ti-layout-board' },
    { key: 'input', icon: 'ti-forms' },
    { key: 'border', icon: 'ti-border-style-2' },
    { key: 'muted', icon: 'ti-typography' },
  ]
  const BASE: { key: 'themeCustomBg' | 'themeCustomCard' | 'themeCustomText'; icon: string; get: () => string }[] = [
    { key: 'themeCustomBg', icon: 'ti-app-window', get: () => bg },
    { key: 'themeCustomCard', icon: 'ti-cards', get: () => card },
    { key: 'themeCustomText', icon: 'ti-letter-case', get: () => text },
  ]

  const hasFine = $derived(Object.keys(x).length > 0)
  const radius = $derived(x.radius ?? 100)
  const depth = $derived<ThemeDepth>(x.depth ?? 'normal')
  const DEPTHS: ThemeDepth[] = ['flat', 'normal', 'deep']

  function normHex(raw: string): string | null {
    let v = raw.trim().replace(/^#/, '')
    if (/^[0-9a-fA-F]{3}$/.test(v)) v = v.split('').map((c) => c + c).join('')
    return /^[0-9a-fA-F]{6}$/.test(v) ? '#' + v.toLowerCase() : null
  }

  function patch(p: Partial<Record<keyof ThemeExtra, any>>) {
    const next: any = { ...x }
    for (const [k, v] of Object.entries(p)) {
      if (v === undefined || v === null || v === '') delete next[k]
      else next[k] = v
    }
    onchange('themeCustomExtra', stringifyThemeExtra(next))
  }

  function onBaseHex(key: string, e: Event, cur: string) {
    const el = e.currentTarget as HTMLInputElement
    const n = normHex(el.value)
    if (n) onchange(key, n)
    else el.value = cur
  }
  function onFineHex(key: ColorKey, e: Event) {
    const el = e.currentTarget as HTMLInputElement
    if (!el.value.trim()) { patch({ [key]: undefined }); return }
    const n = normHex(el.value)
    if (n) patch({ [key]: n })
    else el.value = x[key] ?? ''
  }

  function applyPreset(p: Preset) {
    onchange('themeCustomBg', p.bg)
    onchange('themeCustomCard', p.card)
    onchange('themeCustomText', p.text)
    onchange('themeCustomExtra', stringifyThemeExtra(p.extra ?? {}))
  }
  const activePreset = $derived(PRESETS.findIndex((p) =>
    p.bg === bg && p.card === card && p.text === text &&
    stringifyThemeExtra(p.extra ?? {}) === stringifyThemeExtra(x)))

  // Обмін темою: JSON у буфері — щоб поділитися своєю темою з другом одним рядком.
  async function copyTheme() {
    const payload = JSON.stringify({ shaurmaTheme: 1, bg, card, text, extra: x })
    try { await navigator.clipboard.writeText(payload); toast(t('st.theme.copied'), 'success') }
    catch { toast(t('st.theme.clipFail'), 'error') }
  }
  async function pasteTheme() {
    try {
      const o = JSON.parse(await navigator.clipboard.readText())
      if (!o || o.shaurmaTheme !== 1 || !HEX.test(o.bg) || !HEX.test(o.card) || !HEX.test(o.text)) throw new Error('bad')
      applyPreset({ bg: o.bg, card: o.card, text: o.text, extra: parseThemeExtra(o.extra) })
      toast(t('st.theme.pasted'), 'success')
    } catch { toast(t('st.theme.pasteBad'), 'error') }
  }
</script>

<div class="ts">
  <!-- ══ Готові теми ══ -->
  <div class="ts-presets">
    {#each PRESETS as p, i}
      <button class="ts-preset" class:sel={activePreset === i} onclick={() => applyPreset(p)}>
        <span class="ts-preset-sw">
          <span style="background:{p.bg}"></span>
          <span style="background:{p.extra?.sidebar ?? p.card}"></span>
          <span style="background:{p.card}"></span>
          <span style="background:{p.text}"></span>
        </span>
        {t('st.preset')} {i + 1}
      </button>
    {/each}
  </div>

  <div class="ts-split">
    <!-- ══ Прев'ю ══ -->
    <div class="ts-prev-wrap">
      <div class="ts-cap">{t('st.theme.preview')}</div>
      <div class="ts-prev" style={previewStyle}>
        <aside class="tp-side">
          <span class="tp-logo"></span>
          <span class="tp-nav on"></span>
          <span class="tp-nav"></span>
          <span class="tp-nav"></span>
          <span class="tp-nav s"></span>
        </aside>
        <div class="tp-main">
          <div class="tp-top">
            <span class="tp-search"><i class="ti ti-search"></i>{t('st.theme.pv.search')}</span>
            <span class="tp-pill"></span>
          </div>
          <div class="tp-cards">
            <div class="tp-card">
              <b>{t('st.theme.pv.title')}</b>
              <small>{t('st.theme.pv.sub')}</small>
              <div class="tp-row">
                <span class="tp-btn">{t('st.theme.pv.play')}</span>
                <span class="tp-ghost"></span>
              </div>
            </div>
            <div class="tp-card">
              <span class="tp-line"></span>
              <span class="tp-line s"></span>
              <span class="tp-field"></span>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- ══ Кольори ══ -->
    <div class="ts-controls">
      <div class="ts-group">{t('st.theme.base')}</div>
      {#each BASE as r (r.key)}
        {@const cur = r.get()}
        <div class="ts-row">
          <span class="ts-ic"><i class="ti {r.icon}"></i></span>
          <div class="ts-txt">
            <b>{t('settings.' + r.key)}</b>
            <small>{t('settings.' + r.key + 'Desc')}</small>
          </div>
          <div class="ts-ctl">
            <label class="ts-chip" style="background:{HEX.test(cur) ? cur : '#000'}">
              <input type="color" value={HEX.test(cur) ? cur : '#000000'}
                     oninput={(e) => onchange(r.key, (e.currentTarget as HTMLInputElement).value)} />
            </label>
            <input class="ts-hex" type="text" value={cur} maxlength="7" spellcheck="false" aria-label={t('settings.' + r.key)}
                   onchange={(e) => onBaseHex(r.key, e, cur)} />
          </div>
        </div>
      {/each}

      <div class="ts-group">
        {t('st.theme.fine')}
        {#if hasFine}
          <button class="ts-link" onclick={() => onchange('themeCustomExtra', '')}>
            <i class="ti ti-restore"></i> {t('st.theme.resetFine')}
          </button>
        {/if}
      </div>
      <p class="ts-hint">{t('st.theme.fine.d')}</p>

      {#each FINE as r (r.key)}
        {@const val = x[r.key]}
        <div class="ts-row" class:auto={!val}>
          <span class="ts-ic"><i class="ti {r.icon}"></i></span>
          <div class="ts-txt">
            <b>{t('st.theme.' + r.key)}</b>
            <small>{t('st.theme.' + r.key + '.d')}</small>
          </div>
          <div class="ts-ctl">
            <label class="ts-chip" style="background:{val ?? autoOf[r.key]}">
              <input type="color" value={val ?? autoOf[r.key]}
                     oninput={(e) => patch({ [r.key]: (e.currentTarget as HTMLInputElement).value })} />
            </label>
            <input class="ts-hex" type="text" value={val ?? ''} placeholder={t('st.theme.auto')} maxlength="7" spellcheck="false"
                   aria-label={t('st.theme.' + r.key)} onchange={(e) => onFineHex(r.key, e)} />
            <button class="ts-x" class:vis={!!val} onclick={() => patch({ [r.key]: undefined })}
                    aria-label={t('st.theme.autoReset')} title={t('st.theme.autoReset')} tabindex={val ? 0 : -1}>
              <i class="ti ti-x"></i>
            </button>
          </div>
        </div>
      {/each}

      <div class="ts-row" class:auto={x.radius === undefined}>
        <span class="ts-ic"><i class="ti ti-border-corner-rounded"></i></span>
        <div class="ts-txt">
          <b>{t('st.theme.radius')}</b>
          <small>{t('st.theme.radius.d')}</small>
        </div>
        <div class="ts-ctl wide">
          <input class="ts-range" type="range" min={RADIUS_MIN} max={RADIUS_MAX} step="10" value={radius}
                 aria-label={t('st.theme.radius')}
                 oninput={(e) => patch({ radius: Number((e.currentTarget as HTMLInputElement).value) === 100 ? undefined : Number((e.currentTarget as HTMLInputElement).value) })} />
          <span class="ts-val">{radius}%</span>
        </div>
      </div>

      <div class="ts-row" class:auto={x.depth === undefined}>
        <span class="ts-ic"><i class="ti ti-stack-2"></i></span>
        <div class="ts-txt">
          <b>{t('st.theme.depth')}</b>
          <small>{t('st.theme.depth.d')}</small>
        </div>
        <div class="ts-ctl">
          <div class="ts-seg" role="radiogroup" aria-label={t('st.theme.depth')}>
            {#each DEPTHS as d}
              <button class:sel={depth === d} role="radio" aria-checked={depth === d}
                      onclick={() => patch({ depth: d === 'normal' ? undefined : d })}>{t('st.theme.depth.' + d)}</button>
            {/each}
          </div>
        </div>
      </div>

      <div class="ts-foot">
        <button class="btn btn-ghost btn-sm" onclick={copyTheme}><i class="ti ti-copy"></i> {t('st.theme.copy')}</button>
        <button class="btn btn-ghost btn-sm" onclick={pasteTheme}><i class="ti ti-clipboard-text"></i> {t('st.theme.paste')}</button>
      </div>
    </div>
  </div>
</div>

<style>
  .ts { display: flex; flex-direction: column; gap: 18px; }

  /* ── Готові теми ── */
  .ts-presets { display: grid; grid-template-columns: repeat(auto-fill, minmax(104px, 1fr)); gap: 10px; }
  .ts-preset {
    display: flex; flex-direction: column; gap: 7px; padding: 8px; cursor: pointer; text-align: left;
    border: 1px solid var(--border-hi); border-radius: var(--r-md); background: var(--bg-input); color: var(--text-mute);
    font: 600 11px var(--font-ui); transition: border-color .15s, transform .15s;
  }
  .ts-preset:hover { border-color: rgba(var(--orange-rgb), .45); transform: translateY(-1px); }
  .ts-preset.sel { border-color: var(--orange); color: var(--orange); }
  .ts-preset-sw { display: flex; height: 28px; border-radius: 6px; overflow: hidden; border: 1px solid var(--border); }
  .ts-preset-sw span { flex: 1; }

  .ts-split { display: grid; grid-template-columns: minmax(250px, 330px) minmax(0, 1fr); gap: 22px; align-items: start; }
  @media (max-width: 980px) { .ts-split { grid-template-columns: 1fr; } }

  .ts-cap { font: 700 11px/1 var(--font-ui); letter-spacing: .6px; text-transform: uppercase; color: var(--text-dim); margin-bottom: 10px; }
  .ts-prev-wrap { position: sticky; top: 8px; }

  /* ── Прев'ю: усе читає ті самі --токени, що й справжній інтерфейс ── */
  .ts-prev {
    display: flex; height: 236px; border-radius: var(--r-lg); overflow: hidden; background: var(--bg-app); color: var(--text);
    border: 1px solid var(--border); box-shadow: var(--elev-2); font-family: var(--font-ui);
  }
  .tp-side { width: 52px; flex: none; background: var(--bg-window); border-right: 1px solid var(--border); padding: 12px 9px; display: flex; flex-direction: column; gap: 9px; }
  .tp-logo { width: 100%; aspect-ratio: 1; border-radius: var(--r-sm); background: var(--accent-grad); }
  .tp-nav { height: 8px; border-radius: 4px; background: var(--hover-strong); }
  .tp-nav.on { background: rgba(var(--orange-rgb), .55); }
  .tp-nav.s { width: 60%; }
  .tp-main { flex: 1; min-width: 0; padding: 11px; display: flex; flex-direction: column; gap: 10px; }
  .tp-top { display: flex; align-items: center; gap: 8px; }
  .tp-search {
    flex: 1; display: flex; align-items: center; gap: 6px; padding: 6px 9px; border-radius: var(--r-md);
    background: var(--bg-input); border: 1px solid var(--border); color: var(--text-dim); font-size: 10.5px; font-weight: 500;
  }
  .tp-pill { width: 22px; height: 22px; border-radius: 50%; background: var(--bg-panel); border: 1px solid var(--border-hi); }
  .tp-cards { display: grid; grid-template-columns: 1.25fr 1fr; gap: 9px; flex: 1; min-height: 0; }
  .tp-card {
    background: var(--bg-card); border: 1px solid var(--border); border-radius: var(--r-lg); padding: 10px;
    display: flex; flex-direction: column; gap: 5px; box-shadow: var(--elev-1); min-width: 0;
  }
  .tp-card b { font-size: 12px; font-weight: 700; color: var(--text); }
  .tp-card small { font-size: 10px; color: var(--text-mute); }
  .tp-row { display: flex; align-items: center; gap: 6px; margin-top: auto; }
  .tp-btn { padding: 5px 11px; border-radius: var(--r-md); background: var(--accent-grad); color: var(--accent-ink); font-size: 10.5px; font-weight: 700; }
  .tp-ghost { width: 24px; height: 22px; border-radius: var(--r-md); background: var(--bg-panel); border: 1px solid var(--border-hi); }
  .tp-line { height: 7px; border-radius: 4px; background: var(--hover-strong); }
  .tp-line.s { width: 55%; background: var(--hover); }
  .tp-field { margin-top: auto; height: 20px; border-radius: var(--r-sm); background: var(--bg-input); border: 1px solid var(--border); }

  /* ── Контроли ── */
  .ts-controls { min-width: 0; display: flex; flex-direction: column; }
  .ts-group {
    display: flex; align-items: center; justify-content: space-between; gap: 10px; margin: 6px 0 4px;
    font: 700 11px/1 var(--font-ui); letter-spacing: .6px; text-transform: uppercase; color: var(--text-dim);
  }
  .ts-group:not(:first-child) { margin-top: 18px; }
  .ts-hint { margin: 2px 0 6px; font: 400 12px/1.45 var(--font-ui); color: var(--text-mute); }
  .ts-link {
    display: inline-flex; align-items: center; gap: 5px; border: 0; background: transparent; cursor: pointer;
    font: 600 11.5px var(--font-ui); letter-spacing: 0; text-transform: none; color: var(--orange);
  }
  .ts-link:hover { text-decoration: underline; }

  .ts-row { display: flex; align-items: center; gap: 12px; padding: 9px 0; }
  .ts-row + .ts-row { border-top: 1px solid var(--border); }
  .ts-ic {
    width: 32px; height: 32px; flex: none; border-radius: 10px; display: grid; place-items: center; font-size: 16px;
    color: var(--orange); background: rgba(var(--orange-rgb), .13);
  }
  .ts-row.auto .ts-ic { color: var(--text-mute); background: var(--hover); }
  .ts-txt { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 3px; }
  .ts-txt b { font: 600 13px/1.2 var(--font-ui); color: var(--text); }
  .ts-txt small { font: 400 11.5px/1.35 var(--font-ui); color: var(--text-dim); }
  .ts-ctl { display: flex; align-items: center; gap: 8px; flex: none; }
  .ts-ctl.wide { min-width: 190px; }
  .ts-chip { position: relative; width: 30px; height: 30px; border-radius: 9px; cursor: pointer; border: 1px solid var(--border-hi); overflow: hidden; flex: none; }
  .ts-row.auto .ts-chip { border-style: dashed; }
  .ts-chip input { position: absolute; inset: -6px; width: 44px; height: 44px; opacity: 0; cursor: pointer; }
  .ts-hex {
    width: 90px; padding: 7px 10px; border-radius: 8px; border: 1px solid var(--border); background: var(--bg-input);
    color: var(--text); font: 500 12.5px var(--font-mono);
  }
  .ts-hex::placeholder { color: var(--text-dim); font-family: var(--font-ui); font-weight: 600; }
  .ts-hex:focus { outline: none; border-color: var(--orange); }
  .ts-x {
    width: 26px; height: 26px; border-radius: 8px; border: 0; background: transparent; color: var(--text-mute);
    display: grid; place-items: center; font-size: 14px; cursor: pointer; opacity: 0; pointer-events: none; transition: opacity .15s, background .15s;
  }
  .ts-x.vis { opacity: 1; pointer-events: auto; }
  .ts-x:hover { background: var(--hover-strong); color: var(--text); }

  .ts-range { flex: 1; min-width: 0; accent-color: var(--orange); }
  .ts-val { width: 42px; text-align: right; font: 600 12px var(--font-mono); color: var(--text-mute); }
  .ts-seg { display: flex; gap: 2px; padding: 3px; border-radius: 10px; background: var(--bg-input); border: 1px solid var(--border); }
  .ts-seg button {
    padding: 6px 11px; border: 0; border-radius: 8px; background: transparent; color: var(--text-mute);
    font: 600 12px var(--font-ui); cursor: pointer; white-space: nowrap; transition: .15s;
  }
  .ts-seg button:hover { color: var(--text); }
  .ts-seg button.sel { background: var(--bg-card); color: var(--text); box-shadow: 0 1px 3px rgba(0,0,0,.3); }

  .ts-foot { display: flex; gap: 8px; flex-wrap: wrap; padding-top: 14px; border-top: 1px solid var(--border); margin-top: 4px; }

  @media (max-width: 620px) {
    .ts-row { flex-wrap: wrap; }
    .ts-ctl { margin-left: 44px; }
  }
</style>
