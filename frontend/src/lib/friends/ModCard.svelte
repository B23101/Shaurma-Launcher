<script lang="ts">
  // Картка мода в чаті (див. modShare.ts). Дані тягнемо з браузера лаунчера
  // (GetBrowserProjectDetail) — у повідомленні лише посилання. Поки вантажиться
  // — «скелетон» з блиском; не вдалось — компактна картка з посиланням.
  import { onMount } from 'svelte'
  import { App } from '../wails'
  import { t } from '../i18n.svelte'
  import { tip } from './tooltip'
  import { loadModDetail, type ModRef } from './modShare'
  import type { BrowserProjectDetail } from '../types'

  let { ref, onOpenInLauncher }: {
    ref: ModRef
    onOpenInLauncher?: (source: string, projectId: string) => void
  } = $props()

  let detail = $state<BrowserProjectDetail | null>(null)
  let phase = $state<'loading' | 'ok' | 'fail'>('loading')

  onMount(() => {
    let dead = false
    loadModDetail(ref)
      .then(d => { if (!dead) { detail = d; phase = 'ok' } })
      .catch(() => { if (!dead) phase = 'fail' })
    return () => { dead = true }
  })

  const typeKey = $derived(detail?.projectType ? `browser.type.${detail.projectType}` : '')
  const typeLabel = $derived.by(() => {
    if (!typeKey) return ''
    const v = t(typeKey)
    return v && v !== typeKey ? v : (detail?.projectType ?? '')
  })
  const srcName = $derived(ref.source === 'curseforge' ? 'CurseForge' : 'Modrinth')
  const loaders = $derived((detail?.loaders ?? []).slice(0, 3))

  // Обкладинка: «featured»/перше зображення галереї; інакше — розмита іконка.
  const cover = $derived(
    detail?.gallery?.find(g => g.featured)?.url || detail?.gallery?.[0]?.url || ''
  )
  let coverBad = $state(false)
  let iconBad = $state(false)
  let avaBad = $state(false)

  // Діапазон версій Minecraft: найменша–найбільша (порівняння по числових частинах).
  function vkey(v: string): number[] { return v.split(/[.\-]/).map(x => parseInt(x, 10) || 0) }
  function vcmp(a: string, b: string): number {
    const x = vkey(a), y = vkey(b)
    for (let i = 0; i < Math.max(x.length, y.length); i++) {
      const d = (x[i] ?? 0) - (y[i] ?? 0)
      if (d) return d
    }
    return 0
  }
  const verRange = $derived.by(() => {
    const rel = (detail?.mcVersions ?? []).filter(v => /^\d+(\.\d+){1,2}$/.test(v)).sort(vcmp)
    if (!rel.length) return ''
    return rel.length === 1 || rel[0] === rel[rel.length - 1] ? rel[0] : `${rel[0]}–${rel[rel.length - 1]}`
  })

  // «3 дні тому» — локаль береться з мови інтерфейсу.
  const updatedAgo = $derived.by(() => {
    const ts = detail?.updated ? Date.parse(detail.updated) : NaN
    if (!isFinite(ts)) return ''
    const sec = Math.round((ts - Date.now()) / 1000)
    const units: [Intl.RelativeTimeFormatUnit, number][] = [['year', 31536000], ['month', 2592000], ['week', 604800], ['day', 86400], ['hour', 3600], ['minute', 60]]
    try {
      const rtf = new Intl.RelativeTimeFormat(document.documentElement.lang || 'uk', { numeric: 'auto' })
      for (const [u, n] of units) if (Math.abs(sec) >= n) return rtf.format(Math.round(sec / n), u)
      return rtf.format(0, 'second')
    } catch { return '' }
  })

  const env = $derived.by(() => {
    const c = detail?.clientSide, sv = detail?.serverSide
    const on = (x?: string) => x === 'required' || x === 'optional'
    return { client: on(c), server: on(sv) }
  })

  function fmt(n: number): string {
    if (n >= 1e6) return (n / 1e6).toFixed(n >= 1e7 ? 0 : 1).replace(/\.0$/, '') + 'M'
    if (n >= 1e3) return (n / 1e3).toFixed(n >= 1e4 ? 0 : 1).replace(/\.0$/, '') + 'K'
    return String(n)
  }
  function openSite() { App.OpenExternal(detail?.projectUrl || ref.url).catch(() => {}) }
  function openLauncher() {
    onOpenInLauncher?.(detail?.source ?? ref.source, detail?.projectId ?? ref.projectId)
  }
</script>

<article class="mc" class:loading={phase === 'loading'}>
  {#if phase === 'loading'}
    <div class="mc-cover sk"></div>
    <div class="mc-main">
      <div class="mc-ic sk"></div>
      <div class="mc-titles">
        <div class="sk sk-l1"></div>
        <div class="sk sk-l2"></div>
      </div>
    </div>
    <div class="mc-pad"><div class="sk sk-l3"></div><div class="sk sk-l3 short"></div></div>
  {:else if phase === 'fail' || !detail}
    <div class="mc-pad fail">
      <div class="mc-ic fb"><i class="ti ti-puzzle-off"></i></div>
      <div class="mc-titles">
        <div class="mc-name">{t('social.mod.unavailable')}</div>
        <div class="mc-by">{srcName}</div>
      </div>
      <button class="mc-btn ghost" onclick={openSite} use:tip={t('social.mod.openSite')}><i class="ti ti-external-link"></i> {srcName}</button>
    </div>
  {:else}
    <div class="mc-cover" class:src-cf={ref.source === 'curseforge'}>
      {#if cover && !coverBad}
        <img class="mc-cover-img" src={cover} alt="" loading="lazy" onerror={() => (coverBad = true)} />
      {:else if detail.iconUrl && !iconBad}
        <img class="mc-cover-blur" src={detail.iconUrl} alt="" aria-hidden="true" />
      {/if}
      <div class="mc-cover-fade"></div>
      <span class="mc-pill type"><i class="ti ti-package"></i> {typeLabel || t('social.mod.shared')}</span>
      <span class="mc-pill src" class:cf={ref.source === 'curseforge'}>{srcName}</span>
    </div>

    <div class="mc-main">
      <div class="mc-ic">
        {#if detail.iconUrl && !iconBad}
          <img src={detail.iconUrl} alt="" loading="lazy" onerror={() => (iconBad = true)} />
        {:else}<i class="ti ti-puzzle"></i>{/if}
      </div>
      <div class="mc-titles">
        <div class="mc-name" title={detail.name}>{detail.name}</div>
        {#if detail.author}
          <div class="mc-by">
            {#if detail.authorAvatar && !avaBad}
              <img class="mc-ava" src={detail.authorAvatar} alt="" loading="lazy" onerror={() => (avaBad = true)} />
            {:else}
              <span class="mc-ava ph">{detail.author.charAt(0)}</span>
            {/if}
            <span class="mc-auth">{detail.author}</span>
          </div>
        {/if}
      </div>
    </div>

    <div class="mc-pad">
      {#if detail.description}<p class="mc-desc">{detail.description}</p>{/if}

      <div class="mc-stats">
        <span class="mc-stat" use:tip={t('social.mod.downloads')}><i class="ti ti-download"></i> {fmt(detail.downloads)}</span>
        {#if detail.followers > 0}
          <span class="mc-stat" use:tip={t('social.mod.followers')}><i class="ti ti-heart"></i> {fmt(detail.followers)}</span>
        {/if}
        {#if updatedAgo}
          <span class="mc-stat" use:tip={t('social.mod.updated')}><i class="ti ti-refresh"></i> {updatedAgo}</span>
        {/if}
      </div>

      {#if loaders.length || verRange || env.client || env.server}
        <div class="mc-tags">
          {#each loaders as l (l)}<span class="ld">{l}</span>{/each}
          {#if verRange}<span class="ver"><i class="ti ti-cube"></i> {verRange}</span>{/if}
          {#if env.client}<span class="env" use:tip={t('social.mod.client')}><i class="ti ti-device-desktop"></i></span>{/if}
          {#if env.server}<span class="env" use:tip={t('social.mod.server')}><i class="ti ti-server"></i></span>{/if}
        </div>
      {/if}

      <div class="mc-acts">
        {#if onOpenInLauncher}
          <button class="mc-btn pri" onclick={openLauncher} use:tip={t('social.mod.openLauncherHint')}>
            <i class="ti ti-layout-grid"></i> {t('social.mod.openLauncher')}
          </button>
        {/if}
        <button class="mc-btn ghost icon" onclick={openSite} aria-label={t('social.mod.openSite')} use:tip={t('social.mod.openSite') + ' · ' + srcName}>
          <i class="ti ti-external-link"></i>
        </button>
      </div>
    </div>
  {/if}
</article>

<style>
  .mc { position: relative; width: 100%; min-width: 260px; border-radius: 16px; overflow: hidden; background: var(--bg-input);
    border: 1px solid var(--border-hi); animation: mc-in .34s cubic-bezier(.2,1.1,.3,1) both;
    transition: border-color .18s, box-shadow .18s, transform .18s; }
  .mc:not(.loading):hover { border-color: rgba(var(--orange-rgb), .45); box-shadow: 0 8px 26px rgba(0,0,0,.35); transform: translateY(-1px); }
  @keyframes mc-in { from { opacity: 0; transform: translateY(6px) scale(.97); } to { opacity: 1; transform: none; } }

  .mc-cover { position: relative; height: 64px; overflow: hidden;
    background: linear-gradient(135deg, rgba(var(--orange-rgb), .35), rgba(var(--orange-rgb), .06) 70%), var(--bg-panel); }
  .mc-cover.src-cf { background: linear-gradient(135deg, rgba(241, 100, 54, .38), rgba(241, 100, 54, .06) 70%), var(--bg-panel); }
  .mc-cover-img { position: absolute; inset: 0; width: 100%; height: 100%; object-fit: cover; display: block; animation: mc-fade .4s ease both; }
  .mc-cover-blur { position: absolute; inset: -30px; width: calc(100% + 60px); height: calc(100% + 60px); object-fit: cover; filter: none; opacity: .5; transform: scale(1.4); animation: mc-fade .4s ease both; }
  .mc-cover-fade { position: absolute; inset: 0; background: linear-gradient(180deg, rgba(0,0,0,.28) 0%, rgba(0,0,0,0) 40%, var(--bg-input) 100%); }
  @keyframes mc-fade { from { opacity: 0; } }
  .mc-pill { position: absolute; top: 8px; display: inline-flex; align-items: center; gap: 5px; padding: 3px 9px; border-radius: 999px;
    font: 700 10px/1.4 var(--font-ui); letter-spacing: .2px; color: #fff; background: rgba(0,0,0,.55);
    border: 1px solid rgba(255,255,255,.14); }
  .mc-pill.type { left: 8px; }
  .mc-pill.src { right: 8px; background: rgba(27, 217, 106, .22); border-color: rgba(27, 217, 106, .5); }
  .mc-pill.src.cf { background: rgba(241, 100, 54, .26); border-color: rgba(241, 100, 54, .55); }

  .mc-main { display: flex; align-items: flex-end; gap: 12px; padding: 0 12px; margin-top: -26px; position: relative; }
  .mc-ic { flex: 0 0 auto; width: 58px; height: 58px; border-radius: 16px; overflow: hidden; background: var(--bg-panel);
    border: 2px solid var(--bg-input); box-shadow: 0 6px 18px rgba(0,0,0,.45), 0 0 0 1px var(--border-hi);
    display: flex; align-items: center; justify-content: center; color: var(--text-dim); font-size: 24px; }
  .mc-ic img { width: 100%; height: 100%; object-fit: cover; display: block; image-rendering: auto; }
  .mc-ic.fb { width: 40px; height: 40px; border-radius: 11px; font-size: 18px; box-shadow: none; border-color: var(--border-hi); }
  .mc-titles { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 3px; padding-bottom: 2px; }
  .mc-name { font: 800 15px/1.2 var(--font-ui); color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; letter-spacing: -.1px; }
  .mc-by { display: flex; align-items: center; gap: 6px; min-width: 0; font: 600 11.5px/1.2 var(--font-ui); color: var(--text-mute); }
  .mc-auth { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .mc-ava { width: 16px; height: 16px; border-radius: 50%; object-fit: cover; flex: 0 0 auto; }
  .mc-ava.ph { display: inline-flex; align-items: center; justify-content: center; background: rgba(var(--orange-rgb), .2); color: var(--orange); font: 800 9px/1 var(--font-ui); text-transform: uppercase; }

  .mc-pad { padding: 9px 12px 12px; display: flex; flex-direction: column; gap: 8px; }
  .mc-pad.fail { flex-direction: row; align-items: center; gap: 10px; padding: 12px; }
  .mc-pad.fail .mc-titles { padding: 0; }
  .mc-desc { margin: 0; font: 400 12px/1.45 var(--font-ui); color: var(--text-mute); display: -webkit-box; -webkit-line-clamp: 2; line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }

  .mc-stats { display: flex; align-items: center; flex-wrap: wrap; gap: 6px 14px; font: 650 11.5px/1 var(--font-ui); color: var(--text-mute); }
  .mc-stat { display: inline-flex; align-items: center; gap: 5px; cursor: default; }
  .mc-stat i { font-size: 13px; color: var(--orange); }

  .mc-tags { display: flex; gap: 5px; flex-wrap: wrap; align-items: center; }
  .mc-tags span { display: inline-flex; align-items: center; gap: 4px; padding: 3px 9px; border-radius: 999px; font: 650 10.5px/1.3 var(--font-ui); }
  .mc-tags .ld { text-transform: capitalize; background: rgba(var(--orange-rgb), .12); color: var(--text); border: 1px solid rgba(var(--orange-rgb), .3); }
  .mc-tags .ver { background: var(--bg-panel); color: var(--text-mute); border: 1px solid var(--border-hi); font-family: var(--font-mono); }
  .mc-tags .env { padding: 3px 7px; background: var(--bg-panel); color: var(--text-dim); border: 1px solid var(--border-hi); cursor: default; }

  .mc-acts { display: flex; gap: 6px; margin-top: 2px; }
  .mc-btn { display: inline-flex; align-items: center; justify-content: center; gap: 7px; padding: 8px 12px; border-radius: 10px; border: 1px solid transparent; cursor: pointer;
    font: 750 12px/1 var(--font-ui); transition: filter .14s, transform .12s, background .14s, border-color .14s, color .14s; }
  .mc-btn:active { transform: scale(.96); }
  .mc-btn.pri { flex: 1; background: linear-gradient(135deg, var(--orange), var(--orange-2, var(--orange))); color: #fff; box-shadow: 0 6px 14px rgba(var(--orange-rgb), .28); }
  .mc-btn.pri:hover { filter: brightness(1.1); }
  .mc-btn.ghost { background: var(--bg-panel); color: var(--text-mute); border-color: var(--border-hi); }
  .mc-btn.ghost:hover { color: var(--text); border-color: rgba(var(--orange-rgb), .5); }
  .mc-btn.icon { padding: 8px 11px; }
  .mc-btn i { font-size: 15px; }

  .sk { border-radius: 8px; background: linear-gradient(100deg, var(--bg-panel) 30%, var(--hover-strong) 50%, var(--bg-panel) 70%); background-size: 220% 100%; animation: mc-sh 1.2s linear infinite; }
  .mc-cover.sk { border-radius: 0; height: 64px; }
  .mc-ic.sk { border: 2px solid var(--bg-input); }
  .sk-l1 { height: 15px; width: 62%; }
  .sk-l2 { height: 11px; width: 38%; }
  .sk-l3 { height: 11px; width: 92%; }
  .sk-l3.short { width: 60%; }
  @keyframes mc-sh { to { background-position: -220% 0; } }
  @media (prefers-reduced-motion: reduce) { .mc, .sk, .mc-cover-img, .mc-cover-blur { animation: none; } .mc:hover { transform: none; } }
</style>
