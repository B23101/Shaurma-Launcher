<script lang="ts">
  // ── PackWikiSection — режим вікі шаурма-збірки ─────────────────────────
  // Показує документ packs/<id>/wiki.json, який адмін створив у Pack
  // Manager. Кожен розділ: картинка зліва, HTML або Markdown-опис справа.
  // Вікі немає (null / вимкнена / без розділів) — показуємо акуратний
  // порожній стан, а не помилку.
  import { onMount } from 'svelte'
  import { marked } from 'marked'
  import { sanitizeHtml } from './sanitizeHtml'
  import { App } from './wails'
  import { t } from './i18n.svelte'
  import type { PackWikiDoc, PackWikiSection } from './types'

  let { packId, packName = '' } = $props<{ packId: string; packName?: string }>()

  let doc = $state<PackWikiDoc | null>(null)
  let loading = $state(true)
  let error = $state('')
  let activeId = $state('')

  onMount(async () => {
    if (!packId) { loading = false; return }
    try {
      doc = await App.GetPackWiki(packId)
    } catch (e: any) {
      error = String(e?.message ?? e)
      doc = null
    } finally {
      loading = false
    }
    if (doc?.sections?.length) activeId = doc.sections[0].id
  })

  const sections = $derived(doc?.sections ?? [])
  const active = $derived(sections.find((s) => s.id === activeId) ?? sections[0] ?? null)

  // Тіло розділу: Markdown → HTML через marked, інакше — HTML із редактора.
  // Вікі приходить із CDN (її пише адмін збірки), тож це СТОРОННІЙ HTML:
  // без білого списку <img onerror=…> виконався б у WebView із доступом до
  // App.* (файли, запуск процесів). Той самий санітайзер, що й для описів.
  function bodyHtml(s: PackWikiSection | null): string {
    if (!s) return ''
    const raw = s.body ?? ''
    let html: string
    if ((s.format ?? 'html') === 'md') {
      try {
        html = marked.parse(raw) as string
      } catch {
        // marked упав — показуємо текст без розмітки (не сирий HTML).
        html = raw.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
      }
    } else {
      html = raw
    }
    return sanitizeHtml(html)
  }

  const activeHtml = $derived(bodyHtml(active))
</script>

{#if loading}
  <div class="pwk-empty"><i class="ti ti-loader spin"></i> {t('pack.wiki.loading')}</div>
{:else if error}
  <div class="pwk-empty err"><i class="ti ti-alert-triangle"></i> {error}</div>
{:else if !doc || !doc.enabled || sections.length === 0}
  <div class="pwk-empty">
    <i class="ti ti-book-2"></i>
    <div class="pwk-empty-title">{t('pack.wiki.emptyTitle', { name: packName })}</div>
    <div class="pwk-empty-sub">{t('pack.wiki.emptySub')}</div>
  </div>
{:else}
  <div class="pwk">
    <div class="pwk-head">
      <div class="pwk-ic"><i class="ti ti-book-2"></i></div>
      <div class="pwk-titles">
        <div class="pwk-title">{doc.title || packName || t('pack.wiki.defaultTitle')}</div>
        {#if doc.description}<div class="pwk-desc">{doc.description}</div>{/if}
      </div>
    </div>

    <div class="pwk-tabs">
      {#each sections as s (s.id)}
        <button class="pwk-tab {active?.id === s.id ? 'active' : ''}" onclick={() => activeId = s.id}>
          {#if s.icon}<i class="ti {s.icon}"></i>{/if}
          {s.title}
        </button>
      {/each}
    </div>

    {#if active}
      <div class="pwk-sec">
        <div class="pwk-sec-img">
          {#if active.image}
            <img src={active.image} alt={active.title} />
          {:else}
            <div class="pwk-sec-img-ph"><i class="ti {active.icon || 'ti-book-2'}"></i></div>
          {/if}
        </div>
        <div class="pwk-sec-body">
          <div class="pwk-sec-title">{active.title}</div>
          <div class="pwk-md">{@html activeHtml}</div>
        </div>
      </div>
    {/if}
  </div>
{/if}

<style>
  .pwk { display: flex; flex-direction: column; gap: 16px; }
  .pwk-head { display: flex; align-items: center; gap: 14px; padding: 16px 18px; border-radius: var(--r-lg, 14px);
    background: linear-gradient(135deg, color-mix(in srgb, var(--orange) 14%, var(--bg-card)), var(--bg-card));
    border: 1px solid var(--border); }
  .pwk-ic { width: 44px; height: 44px; border-radius: 12px; display: flex; align-items: center; justify-content: center;
    background: var(--orange); color: #fff; font-size: 22px; flex-shrink: 0; box-shadow: 0 8px 22px rgba(var(--orange-rgb), .35); }
  .pwk-titles { min-width: 0; }
  .pwk-title { font-size: 17px; font-weight: 800; color: var(--text); }
  .pwk-desc { font-size: 12.5px; color: var(--text-mute); margin-top: 2px; }

  .pwk-tabs { display: flex; gap: 6px; flex-wrap: wrap; }
  .pwk-tab { display: inline-flex; align-items: center; gap: 7px; padding: 7px 14px; border-radius: 999px;
    background: var(--bg-input); border: 1px solid var(--border); color: var(--text-mute);
    font: 700 12px/1 var(--font-ui); cursor: pointer; transition: color .15s, border-color .15s, background .15s; }
  .pwk-tab:hover { color: var(--text); border-color: var(--border-hi); }
  .pwk-tab.active { color: var(--orange); border-color: rgba(var(--orange-rgb), .5); background: rgba(var(--orange-rgb), .12); }

  .pwk-sec { display: grid; grid-template-columns: minmax(200px, 340px) 1fr; gap: 20px; align-items: start;
    background: var(--bg-card); border: 1px solid var(--border); border-radius: var(--r-lg, 14px); padding: 18px; }
  .pwk-sec-img { position: sticky; top: 70px; }
  .pwk-sec-img img { width: 100%; border-radius: 12px; display: block; border: 1px solid var(--border); }
  .pwk-sec-img-ph { width: 100%; aspect-ratio: 4 / 3; border-radius: 12px; display: flex; align-items: center; justify-content: center;
    background: var(--bg-input); border: 1px solid var(--border); color: var(--text-dim); font-size: 42px; }
  .pwk-sec-body { min-width: 0; }
  .pwk-sec-title { font-size: 15px; font-weight: 800; color: var(--text); margin-bottom: 10px; }
  .pwk-md { font-size: 13px; line-height: 1.7; color: var(--text); overflow-wrap: anywhere; }
  .pwk-md :global(h1), .pwk-md :global(h2), .pwk-md :global(h3) { color: var(--text); margin: 16px 0 8px; line-height: 1.3; }
  .pwk-md :global(h1) { font-size: 19px; }
  .pwk-md :global(h2) { font-size: 16px; }
  .pwk-md :global(h3) { font-size: 14px; }
  .pwk-md :global(p) { margin: 8px 0; }
  .pwk-md :global(a) { color: var(--orange); }
  .pwk-md :global(img) { max-width: 100%; border-radius: 10px; margin: 8px 0; }
  .pwk-md :global(ul), .pwk-md :global(ol) { padding-left: 22px; margin: 8px 0; }
  .pwk-md :global(li) { margin: 3px 0; }
  .pwk-md :global(code) { font-family: var(--font-mono); font-size: 12px; background: var(--bg-input); padding: 1px 5px; border-radius: 5px; }
  .pwk-md :global(pre) { background: var(--bg-input); border: 1px solid var(--border); border-radius: 10px; padding: 12px; overflow: auto; }
  .pwk-md :global(pre code) { background: none; padding: 0; }
  .pwk-md :global(blockquote) { border-left: 3px solid var(--orange); margin: 10px 0; padding: 4px 14px; color: var(--text-mute); }
  .pwk-md :global(table) { border-collapse: collapse; width: 100%; margin: 10px 0; }
  .pwk-md :global(th), .pwk-md :global(td) { border: 1px solid var(--border); padding: 6px 10px; text-align: left; }

  .pwk-empty { display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 10px;
    padding: 54px 20px; text-align: center; color: var(--text-mute); }
  .pwk-empty > i { font-size: 46px; color: var(--text-dim); }
  .pwk-empty.err { color: var(--red); }
  .pwk-empty-title { font-size: 14px; font-weight: 700; color: var(--text); }
  .pwk-empty-sub { font-size: 12px; max-width: 420px; line-height: 1.6; }

  @media (max-width: 720px) {
    .pwk-sec { grid-template-columns: 1fr; }
    .pwk-sec-img { position: static; }
  }
</style>
