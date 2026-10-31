<script lang="ts">
  import type { Snippet } from 'svelte'
  import { App } from './wails'
  import mafia from './page-designs/mafia.html?raw'
  import './page-designs/mafia.css'
  let { packId, official, background = '', onLayoutChange, children } = $props<{ packId: string; official: boolean; background?: string; onLayoutChange?: (active: boolean) => void; children: Snippet }>()
  let html = $state('')
  let palette = $state('')
  let enabled = $state(true)
  let designKey = ''
  let skin = $state('')
  let scene = $state('')
  let viewHeight = $state(720)
  const full = $derived(!!html && enabled && skin === 'mafia')
  $effect(() => { onLayoutChange?.(full) })
  function measure(node: HTMLElement) {
    const viewport = node.closest('.content') || node.parentElement!
    const update = () => { viewHeight = viewport.clientHeight }
    const observer = new ResizeObserver(update); observer.observe(viewport); update()
    function paintRanges() {
      for (const range of node.querySelectorAll<HTMLInputElement>('input[type="range"]')) {
        const min = Number(range.min || 0), max = Number(range.max || 100)
        const fill = max > min ? Math.max(0, Math.min(100, (Number(range.value) - min) / (max - min) * 100)) : 0
        range.style.setProperty('--mafia-range-fill', `${fill}%`)
      }
    }
    const contentObserver = new MutationObserver(paintRanges)
    contentObserver.observe(node, { childList: true, subtree: true })
    node.addEventListener('input', paintRanges, true)
    paintRanges()
    return { destroy() { observer.disconnect(); contentObserver.disconnect(); node.removeEventListener('input', paintRanges, true) } }
  }
  $effect(() => {
    const id = packId
    const isOfficial = official
    let cancelled = false
    html = ''; palette = ''; skin = ''; scene = ''
    enabled = true
    designKey = `shaurma.page-design.disabled.${id}`
    try { enabled = localStorage.getItem(designKey) !== 'true' } catch { /* optional preference */ }
    function apply(source: string) {
      if (cancelled || !source) return
      const doc = new DOMParser().parseFromString(source, 'text/html')
      skin = doc.querySelector('meta[name="shaurma-skin"]')?.getAttribute('content') === 'mafia' ? 'mafia' : ''
      const image = doc.querySelector('img[data-shaurma-background]')?.getAttribute('src') || doc.querySelector('meta[name="shaurma-background"]')?.getAttribute('content') || ''
      scene = /^data:image\/(png|jpeg|webp);base64,[a-zA-Z0-9+/=]+$/.test(image) ? image : ''
      const tokens = ['bg-panel', 'bg-card', 'bg-input', 'bg-input-hi', 'text', 'text-mute', 'text-dim', 'orange', 'orange-2', 'border']
      palette = tokens.map(key => {
        const value = doc.querySelector(`meta[name="shaurma-${key}"]`)?.getAttribute('content') || ''
        return /^#[0-9a-f]{6}$/i.test(value) ? `--${key}:${value}` : ''
      }).filter(Boolean).join(';')
      const accent = doc.querySelector('meta[name="shaurma-orange"]')?.getAttribute('content') || ''
      if (/^#[0-9a-f]{6}$/i.test(accent)) palette += `;--orange-rgb:${[1,3,5].map(i => parseInt(accent.slice(i,i+2),16)).join(',')}`
      doc.querySelectorAll('script,iframe,object,embed,base,link,form,meta[http-equiv]').forEach(el => el.remove())
      doc.querySelectorAll('*').forEach(el => {
        for (const attr of [...el.attributes]) if (/^on/i.test(attr.name) || ['href','action','formaction'].includes(attr.name)) el.removeAttribute(attr.name)
      })
      const csp = doc.createElement('meta')
      csp.httpEquiv = 'Content-Security-Policy'
      csp.content = "default-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src data:; base-uri 'none'; form-action 'none'"
      doc.head.prepend(csp)
      html = '<!doctype html>' + doc.documentElement.outerHTML
    }
    if (isOfficial) {
      if (id.toLowerCase() === 'mafia') apply(mafia)
      App.GetPackPageDesign(id).then(apply).catch(() => { /* standard or bundled design remains usable offline */ })
    }
    return () => { cancelled = true }
  })
  function toggle() {
    enabled = !enabled
    try { localStorage.setItem(designKey, String(!enabled)) } catch { /* preference only */ }
  }
</script>

<div class="pack-page-design" class:designed={!!html && enabled} class:mafia-layout={full} style={`${html && enabled ? palette : ''};--mafia-scene:url("${scene || (/^data:image\/(png|jpeg|webp);base64,[a-zA-Z0-9+/=]+$/.test(background) ? background : '')}");--mafia-view-height:${viewHeight}px`} use:measure>
  {#if full}
    <div class="mafia-scene" aria-hidden="true"><div class="mafia-panorama"></div><div class="mafia-shade"></div><div class="mafia-scanlines"></div><div class="mafia-embers"></div><div class="mafia-corners"></div></div>
  {/if}
  {#if html}
    <div class="design-switch"><button type="button" onclick={toggle} aria-pressed={enabled} title="Перемкнути оформлення сторінки">{enabled ? 'Стандартний дизайн' : 'Дизайн збірки'}</button></div>
    {#if enabled && !full}<iframe class="design-art" title="Оформлення збірки" srcdoc={html} sandbox="" tabindex="-1" aria-hidden="true"></iframe>{/if}
  {/if}
  <div class="design-content">{@render children()}</div>
</div>

<style>
  .pack-page-design:not(.designed), .design-content { display: contents; }
  .designed { color: var(--text); min-height: 100%; }
  .design-switch { display: flex; justify-content: flex-end; padding: 8px 0; }
  .design-switch button { font: inherit; font-size: 11px; color: var(--text-mute); background: transparent; border: 1px solid var(--border); border-radius: 4px; padding: 5px 10px; cursor: pointer; }
  .design-switch button:focus-visible { outline: 2px solid var(--orange); outline-offset: 3px; }
  .design-art { display: block; width: 100%; height: 310px; border: 0; pointer-events: none; }
</style>
