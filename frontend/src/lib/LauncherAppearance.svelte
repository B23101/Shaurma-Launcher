<script lang="ts">
  // ── LauncherAppearance — єдиний редактор вигляду лаунчера ───────────────
  // Три рядки: Тема · Акцент · Власні кольори (Gradient/Flat + 2 hex).
  //
  // ЖИВОГО ПРЕВ'Ю ТУТ НЕМАЄ навмисно: користувач і так дивиться на сам
  // лаунчер, тож мініатюра лише займала місце зверху картки. Результат
  // вибору видно одразу в усьому інтерфейсі — CSS-змінні застосовуються
  // миттєво.
  //
  // Компонент лише читає/пише settings через onchange(key, value) — уся
  // логіка застосування (CSS-змінні, data-theme) лишається в theme.svelte.ts.
  import { t } from './i18n.svelte'
  import { ACCENTS } from './theme.svelte'

  interface Props {
    theme: string
    accent: string
    accentCustom: string
    accentCustom2: string
    accentCustomMode: string
    // Кольори «своєї» теми — потрібні для міні-прев'ю теми (прямокутники
    // в рядку «Тема»), а не лише для видаленого повнорозмірного прев'ю.
    themeCustomBg: string
    themeCustomCard: string
    themeCustomText: string
    onchange: (key: string, value: any) => void
  }
  let {
    theme, accent, accentCustom, accentCustom2, accentCustomMode,
    themeCustomBg, themeCustomCard, themeCustomText, onchange,
  }: Props = $props()

  const HEX = /^#[0-9a-fA-F]{6}$/
  const THEMES = ['system', 'light', 'dark', 'oled'] as const
  type Pal = { bg: string; card: string; text: string }
  const PAL: Record<string, Pal> = {
    dark:  { bg: '#0f0f0f', card: '#161616', text: '#ffffff' },
    light: { bg: '#f2f0ec', card: '#ffffff', text: '#1d1d1d' },
    oled:  { bg: '#000000', card: '#0a0a0a', text: '#ffffff' },
  }

  // Порядок кружечків як на референсі: пресети + градієнти, «свій» — останній.
  const SWATCHES = ['violet', 'red', 'orange', 'green', 'blue', 'sunset', 'ocean', 'aurora']

  function systemIsLight(): boolean {
    return typeof window !== 'undefined' && !!window.matchMedia?.('(prefers-color-scheme: light)').matches
  }
  // Палітра теми для міні-прев'ю в рядку «Тема».
  function palette(v: string): Pal {
    if (v === 'custom') return { bg: themeCustomBg, card: themeCustomCard, text: themeCustomText }
    if (v === 'system') return PAL[systemIsLight() ? 'light' : 'dark']
    return PAL[v] ?? PAL.dark
  }

  const c1 = $derived(HEX.test(accentCustom || '') ? accentCustom : '#8b5cf6')
  const c2 = $derived(HEX.test(accentCustom2 || '') ? accentCustom2 : '#3b82f6')
  const mode = $derived(accentCustomMode === 'gradient' ? 'gradient' : 'flat')

  // Поточні кольори акценту (main → deep) — для кружечка «Свій колір».
  const accentPair = $derived.by((): [string, string] => {
    if (accent === 'custom') return [c1, mode === 'gradient' ? c2 : c1]
    const a = ACCENTS[accent] ?? ACCENTS.orange
    return [a.main, a.deep]
  })
  const accentGrad = $derived(`linear-gradient(135deg, ${accentPair[0]}, ${accentPair[1]})`)

  function swatchBg(k: string): string {
    const a = ACCENTS[k]
    return `linear-gradient(135deg, ${a.main}, ${a.deep})`
  }

  // Ручне введення hex: приймаємо і без '#', і скорочений #abc.
  function normHex(raw: string): string | null {
    let v = raw.trim().replace(/^#/, '')
    if (/^[0-9a-fA-F]{3}$/.test(v)) v = v.split('').map((c) => c + c).join('')
    return /^[0-9a-fA-F]{6}$/.test(v) ? '#' + v.toLowerCase() : null
  }
  function onHex(key: 'accentCustom' | 'accentCustom2', e: Event) {
    const el = e.currentTarget as HTMLInputElement
    const n = normHex(el.value)
    if (n) onchange(key, n)
    else el.value = key === 'accentCustom' ? c1 : c2 // невалідне — відкат
  }
  function pickCustom() {
    // Перший вибір «свого» кольору одразу дає гарний градієнт, як на референсі.
    if (accent !== 'custom') {
      onchange('accentCustom', c1)
      onchange('accentCustom2', c2)
      onchange('accentCustomMode', 'gradient')
    }
    onchange('accent', 'custom')
  }
</script>

<div class="la">
  <div class="la-panel">
    <!-- ══ Тема ══ -->
    <div class="la-row">
      <div class="la-label">
        <div class="la-name">{t('settings.appearance.theme')}</div>
        <div class="la-desc">{t('settings.themeSystemDesc')}</div>
      </div>
      <div class="la-themes" role="radiogroup" aria-label={t('settings.appearance.theme')}>
        {#each THEMES as v}
          {@const p = palette(v)}
          <button class="la-theme" class:sel={theme === v} role="radio" aria-checked={theme === v}
                  onclick={() => onchange('theme', v)}>
            <span class="lt-prev" style={v === 'system'
              ? `background:linear-gradient(135deg, ${PAL.light.bg} 50%, ${PAL.dark.bg} 50%)`
              : `background:${p.bg}`}>
              <span class="lt-side" style="background:{v === 'system' ? PAL.dark.card : p.card}"></span>
              <span class="lt-win" style="background:{v === 'system' ? PAL.light.card : p.card}">
                <i style="background:{v === 'system' ? PAL.light.text : p.text}"></i>
                <i class="s" style="background:{v === 'system' ? PAL.light.text : p.text}"></i>
              </span>
            </span>
            <span class="lt-name">{t('settings.theme.' + v)}</span>
          </button>
        {/each}
      </div>
    </div>

    <!-- ══ Акцент ══ -->
    <div class="la-row">
      <div class="la-label">
        <div class="la-name">{t('settings.appearance.accent')}</div>
        <div class="la-desc">{accent === 'custom' ? t('accent.custom') : t('accent.' + accent)}</div>
      </div>
      <div class="la-swatches" role="radiogroup" aria-label={t('settings.appearance.accent')}>
        {#each SWATCHES as k}
          <button class="la-sw" class:sel={accent === k} role="radio" aria-checked={accent === k}
                  style="background:{swatchBg(k)}" title={t('accent.' + k)} aria-label={t('accent.' + k)}
                  onclick={() => onchange('accent', k)}></button>
        {/each}
        <button class="la-sw la-sw-custom" class:sel={accent === 'custom'} role="radio" aria-checked={accent === 'custom'}
                style="background:{accent === 'custom' ? accentGrad : 'var(--bg-input)'}"
                title={t('accent.custom')} aria-label={t('accent.custom')} onclick={pickCustom}>
          <i class="ti ti-color-picker"></i>
        </button>
      </div>
    </div>

    <!-- ══ Власні кольори (лише для «Свій колір») ══ -->
    {#if accent === 'custom'}
      <div class="la-row">
        <div class="la-label">
          <div class="la-name">{t('settings.appearance.custom')}</div>
          <div class="la-desc">{t('settings.appearance.customDesc')}</div>
        </div>
        <div class="la-custom">
          <div class="la-seg" role="group">
            <button class:sel={mode === 'gradient'} onclick={() => onchange('accentCustomMode', 'gradient')}>
              <i class="ti ti-circles-relation"></i> {t('settings.accentCustomMode.gradient')}
            </button>
            <button class:sel={mode === 'flat'} onclick={() => onchange('accentCustomMode', 'flat')}>
              <i class="ti ti-bucket-droplet"></i> {t('settings.accentCustomMode.flat')}
            </button>
          </div>

          <div class="la-color">
            <label class="lc-chip" style="background:{c1}" title={t('settings.appearance.pickColor')}>
              <input type="color" value={c1} oninput={(e) => onchange('accentCustom', (e.currentTarget as HTMLInputElement).value)} />
            </label>
            <input class="lc-hex" type="text" value={c1} maxlength="7" spellcheck="false"
                   aria-label={t('settings.accentCustomColor1')}
                   onchange={(e) => onHex('accentCustom', e)} />
          </div>

          {#if mode === 'gradient'}
            <div class="la-color">
              <label class="lc-chip" style="background:{c2}" title={t('settings.appearance.pickColor')}>
                <input type="color" value={c2} oninput={(e) => onchange('accentCustom2', (e.currentTarget as HTMLInputElement).value)} />
              </label>
              <input class="lc-hex" type="text" value={c2} maxlength="7" spellcheck="false"
                     aria-label={t('settings.accentCustomColor2')}
                     onchange={(e) => onHex('accentCustom2', e)} />
            </div>
          {/if}
        </div>
      </div>
    {/if}
  </div>
</div>

<style>
  .la { display: flex; flex-direction: column; gap: 14px; padding: 4px 0; }

  /* ── Панель рядків ── */
  .la-panel { border: 1px solid var(--border); border-radius: 14px; background: var(--bg-card); overflow: hidden; }
  .la-row { display: flex; align-items: center; justify-content: space-between; gap: 20px; padding: 16px 20px; flex-wrap: wrap; }
  .la-row + .la-row { border-top: 1px solid var(--border); }
  .la-label { min-width: 150px; }
  .la-name { font-weight: 700; font-size: 14px; color: var(--text); }
  .la-desc { font-size: 12px; color: var(--text-mute); margin-top: 2px; }

  /* Тема */
  .la-themes { display: flex; gap: 10px; flex-wrap: wrap; }
  .la-theme { background: none; border: none; padding: 0; cursor: pointer; display: flex; flex-direction: column; align-items: center; gap: 6px; }
  .lt-prev {
    position: relative; display: block; width: 88px; height: 56px; border-radius: 10px; overflow: hidden;
    border: 2px solid var(--border); transition: border-color .15s, box-shadow .15s, transform .15s;
  }
  .la-theme:hover .lt-prev { transform: translateY(-2px); }
  .la-theme.sel .lt-prev { border-color: var(--orange); box-shadow: 0 0 0 3px rgba(var(--orange-rgb), .22); }
  .lt-side { position: absolute; left: 5px; top: 6px; bottom: 6px; width: 9px; border-radius: 3px; opacity: .9; }
  .lt-win { position: absolute; left: 19px; right: 5px; top: 6px; bottom: 6px; border-radius: 4px; padding: 5px; display: flex; flex-direction: column; gap: 4px; }
  .lt-win i { display: block; height: 3px; border-radius: 2px; opacity: .55; }
  .lt-win i.s { width: 60%; opacity: .3; }
  .lt-name { font-size: 12px; font-weight: 600; color: var(--text-mute); }
  .la-theme.sel .lt-name { color: var(--text); }

  /* Акцент */
  .la-swatches { display: flex; gap: 10px; flex-wrap: wrap; align-items: center; }
  .la-sw {
    width: 28px; height: 28px; border-radius: 50%; cursor: pointer; padding: 0;
    border: 2px solid transparent; display: flex; align-items: center; justify-content: center;
    color: #fff; transition: transform .15s, box-shadow .15s;
  }
  .la-sw:hover { transform: scale(1.12); }
  .la-sw.sel { box-shadow: 0 0 0 2px var(--bg-card), 0 0 0 4px var(--text); }
  .la-sw-custom i { font-size: 14px; }
  .la-sw-custom:not(.sel) { border: 1px dashed var(--border-hi); color: var(--text-mute); }

  /* Власні кольори */
  .la-custom { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
  .la-seg { display: flex; background: var(--bg-input); border: 1px solid var(--border); border-radius: 10px; padding: 3px; gap: 2px; }
  .la-seg button {
    display: flex; align-items: center; gap: 6px; padding: 7px 14px; border-radius: 8px; border: none;
    background: transparent; color: var(--text-mute); font-weight: 600; font-size: 13px; cursor: pointer; transition: .15s;
  }
  .la-seg button:hover { color: var(--text); }
  .la-seg button.sel { background: var(--bg-card); color: var(--text); box-shadow: 0 1px 3px rgba(0,0,0,.3); }
  .la-color { display: flex; align-items: center; gap: 8px; }
  .lc-chip { position: relative; width: 28px; height: 28px; border-radius: 8px; cursor: pointer; border: 1px solid var(--border-hi); overflow: hidden; }
  .lc-chip input { position: absolute; inset: -6px; width: 44px; height: 44px; opacity: 0; cursor: pointer; }
  .lc-hex {
    width: 92px; padding: 7px 10px; border-radius: 8px; border: 1px solid var(--border);
    background: var(--bg-input); color: var(--text); font-family: var(--font-mono); font-size: 13px;
  }
  .lc-hex:focus { outline: none; border-color: var(--orange); }
</style>
