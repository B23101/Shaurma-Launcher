<script lang="ts">
  // ── Сторінка автора ──────────────────────────────────────────────────
  // Модель — 1-в-1 з Modrinth app UserPage: профіль автора/організації
  // (аватар, ім'я, роль, біо, дата реєстрації) + усі його проєкти сіткою
  // карток (та сама картка, що в браузері контенту). Клік по проєкту →
  // сторінка проєкту.
  //
  // CurseForge НЕ має API профілю користувача (немає /users endpoints) —
  // бекенд повертає HasProfile=false: показуємо лише ім'я з проєкту та
  // сітку його проєктів (через /mods/search?authorId=...), без аватара/біо.
  import { t } from './i18n.svelte'
  import { App } from './wails'
  import { toast } from './toast.svelte'
  import type { BrowserAuthor, BrowserEntry } from './types'

  let {
    source,
    authorId,
    authorName,
    onBack,
    openProject,
  } = $props<{
    source: string
    authorId: string
    authorName: string
    onBack: () => void
    // Відкрити сторінку проєкту (клік по картці проєкту автора).
    openProject: (source: string, projectId: string) => void
  }>()

  let author = $state<BrowserAuthor | null>(null)
  let projects = $state<BrowserEntry[]>([])
  let loading = $state(true)
  let projectsLoading = $state(false)
  let error = $state('')

  // Палітра для запасного аватара (коли джерело не дає картинку).
  const palette = ['#ff8a00', '#7b4dff', '#22c55e', '#3b82f6', '#ef4444', '#06b6d4', '#eab308', '#ec4899']
  function accentColor(): string {
    let h = 0
    for (const ch of authorId + authorName) h = (h * 31 + ch.charCodeAt(0)) >>> 0
    return palette[h % palette.length]
  }

  function formatDate(s: string): string {
    if (!s) return ''
    const d = new Date(s)
    return isNaN(d.getTime()) ? '' : d.toLocaleDateString()
  }

  function fmtCount(n: number): string {
    if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M'
    if (n >= 1000) return (n / 1000).toFixed(1) + 'K'
    return String(n || 0)
  }

  function typeIcon(type: string): string {
    switch (type) {
      case 'modpack': return 'ti-package'
      case 'resourcepack': return 'ti-photo'
      case 'shader': return 'ti-brush'
      case 'datapack': return 'ti-database'
      default: return 'ti-puzzle'
    }
  }

  async function load() {
    loading = true
    error = ''
    try {
      const [a, projs] = await Promise.all([
        App.GetBrowserAuthor(source, authorId, authorName),
        App.GetBrowserAuthorProjects(source, authorId),
      ])
      author = a || null
      projects = projs ?? []
    } catch (e: any) {
      error = String(e?.message ?? e)
      toast(t('error.withMessage', { message: e }), 'error')
    } finally {
      loading = false
    }
  }

  let loadedKey = $state('')
  $effect(() => {
    const key = `${source}::${authorId}`
    if (loadedKey === key) return
    loadedKey = key
    load()
  })
</script>

<div class="ap-wrap">
  {#if loading}
    <div class="ap-loading"><i class="ti ti-loader spin"></i> {t('browser.author.loading')}</div>
  {:else if !author}
    <div class="ap-empty">
      <i class="ti ti-alert-triangle"></i>
      <div>{error || t('browser.author.notFound')}</div>
      <button class="btn btn-ghost btn-sm" onclick={onBack}><i class="ti ti-arrow-left"></i> {t('browser.project.back')}</button>
    </div>
  {:else}
    <!-- Кнопка назад -->
    <div class="ap-topbar">
      <button class="btn btn-ghost btn-sm" onclick={onBack}><i class="ti ti-arrow-left"></i> {t('browser.project.back')}</button>
      {#if author.profileUrl}
        <a class="ap-external" href={author.profileUrl} target="_blank" rel="noreferrer" title={t('browser.author.openProfile')}>
          <i class="ti ti-external-link"></i> {author.source === 'curseforge' ? 'CurseForge' : 'Modrinth'}
        </a>
      {/if}
    </div>

    <!-- Банер профілю: аватар + ім'я + роль/біо/дата (як UserPageHeader Modrinth) -->
    <div class="ap-banner" style="--pcol:{accentColor()}">
      {#if author.avatar}
        <img class="ap-avatar" src={author.avatar} alt="" onerror={(e) => ((e.currentTarget as HTMLElement).style.display = 'none')} />
      {:else}
        <div class="ap-avatar ap-avatar-fb" style={`background:linear-gradient(135deg,${accentColor()},#7b4dff)`}>
          {(author.name || authorName || '?').trim().charAt(0).toUpperCase()}
        </div>
      {/if}
      <div class="ap-meta">
        <div class="ap-name">
          {author.name || authorName}
          {#if author.isOrg}<span class="ap-badge"><i class="ti ti-building"></i> {t('browser.author.organization')}</span>{/if}
        </div>
        <div class="ap-sub">
          {#if author.username && author.username !== author.name}<span class="ap-username">@{author.username}</span>{/if}
          {#if author.role}<span class="ap-role"><i class="ti ti-badge"></i> {author.role}</span>{/if}
          {#if author.created}<span class="ap-created"><i class="ti ti-calendar"></i> {t('browser.author.memberSince', { date: formatDate(author.created) })}</span>{/if}
        </div>
        {#if author.bio}
          <div class="ap-bio">{author.bio}</div>
        {/if}
        {#if !author.hasProfile}
          <div class="ap-noprofile"><i class="ti ti-alert-triangle"></i> {t('browser.author.noProfile')}</div>
        {/if}
      </div>
    </div>

    <!-- Сітка проєктів автора (та сама картка, що в браузері) -->
    <div class="ap-section">
      <div class="ap-section-title"><i class="ti ti-package"></i> {t('browser.author.projects', { n: projects.length })}</div>
      {#if projects.length === 0}
        <div class="ap-empty sm"><i class="ti ti-mood-sad"></i><div>{t('browser.author.noProjects')}</div></div>
      {:else}
        <div class="ap-grid">
          {#each projects as p (p.source + '|' + (p.projectId || p.id))}
            <div class="ap-card" role="button" tabindex="0" onclick={() => openProject(p.source, p.projectId || p.id)}>
              {#if p.iconUrl}
                <img class="ap-card-ic" src={p.iconUrl} alt="" loading="lazy" />
              {:else}
                <div class="ap-card-ic ap-card-ic-fallback"><i class="ti {typeIcon(p.type)}"></i></div>
              {/if}
              <div class="ap-card-body">
                <div class="ap-card-name">
                  <span class="ap-src-dot {p.source === 'curseforge' ? 'cf' : 'mr'}" title={p.source === 'curseforge' ? 'CurseForge' : 'Modrinth'}></span>
                  <span class="ap-card-title">{p.name}</span>
                </div>
                <div class="ap-card-tags">
                  <span class="ap-card-type">{p.type}</span>
                  <span class="ap-card-src {p.source === 'curseforge' ? 'cf' : 'mr'}">{p.source === 'curseforge' ? 'CurseForge' : 'Modrinth'}</span>
                </div>
                <div class="ap-card-desc">{p.description}</div>
                <div class="ap-card-meta">
                  {#if p.loader}<span class="chip chip-l"><i class="ti ti-brand-git"></i> {p.loader}</span>{/if}
                  {#if p.mcVersion}<span class="chip"><i class="ti ti-cube"></i> {p.mcVersion}</span>{/if}
                  <span class="ap-downloads"><i class="ti ti-download"></i> {fmtCount(p.downloads)}</span>
                </div>
              </div>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .ap-wrap { display: flex; flex-direction: column; gap: 14px; }

  .ap-topbar { display: flex; align-items: center; justify-content: space-between; }
  .ap-external { display: inline-flex; align-items: center; gap: 6px; color: var(--text-mute); font-size: 12px; text-decoration: none; }
  .ap-external:hover { color: var(--orange); }

  .ap-loading, .ap-empty { display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 10px; padding: 70px 0; color: var(--text-mute); font-size: 13px; }
  .ap-loading i { font-size: 30px; }
  .ap-empty i { font-size: 34px; color: var(--border-hi); }
  .ap-empty.sm { padding: 30px 0; font-size: 12px; }
  .ap-empty.sm i { font-size: 24px; }

  .ap-banner {
    position: relative; overflow: hidden; border-radius: var(--r-lg);
    border: 1px solid var(--border); padding: 26px 24px;
    background: linear-gradient(135deg, rgba(var(--orange-rgb), .14), rgba(123,77,255,.12));
    display: flex; gap: 18px; align-items: flex-start;
  }
  .ap-avatar {
    width: 84px; height: 84px; min-width: 84px; border-radius: 50%;
    object-fit: cover; box-shadow: 0 6px 20px rgba(0,0,0,.4);
  }
  .ap-avatar-fb {
    display: flex; align-items: center; justify-content: center;
    color: #fff; font-size: 34px; font-weight: 900;
  }
  .ap-meta { min-width: 0; }
  .ap-name { font-size: 22px; font-weight: 900; color: var(--text); line-height: 1.2; display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  .ap-badge {
    display: inline-flex; align-items: center; gap: 4px; padding: 2px 9px; font-size: 11px; font-weight: 700;
    border-radius: 999px; background: rgba(123,77,255,.18); border: 1px solid rgba(123,77,255,.45); color: #b79cff;
  }
  .ap-sub { display: flex; align-items: center; gap: 12px; margin-top: 6px; flex-wrap: wrap; font-size: 12px; color: var(--text-mute); }
  .ap-sub span { display: inline-flex; align-items: center; gap: 5px; }
  .ap-sub i { color: var(--orange); }
  .ap-username { font-family: var(--font-mono); color: var(--text); }
  .ap-bio { margin-top: 10px; font-size: 13px; line-height: 1.55; color: var(--text); max-width: 640px; }
  .ap-noprofile {
    margin-top: 10px; display: inline-flex; align-items: center; gap: 6px;
    font-size: 11.5px; color: var(--text-mute); padding: 4px 10px;
    background: rgba(255,255,255,.05); border: 1px solid var(--border); border-radius: var(--r-md);
  }
  .ap-noprofile i { color: var(--orange); }

  .ap-section { display: flex; flex-direction: column; gap: 10px; }
  .ap-section-title { font-size: 14px; font-weight: 800; color: var(--text); display: flex; align-items: center; gap: 7px; }
  .ap-section-title i { color: var(--orange); }

  .ap-grid {
    display: grid; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); gap: 12px;
  }
  .ap-card {
    display: flex; gap: 12px; padding: 12px; background: var(--bg-card);
    border: 1px solid var(--border); border-radius: var(--r-lg); cursor: pointer;
    transition: transform .15s, border-color .15s, box-shadow .15s;
  }
  .ap-card:hover { transform: translateY(-2px); border-color: var(--orange); box-shadow: 0 8px 24px rgba(0,0,0,.25); }
  .ap-card-ic { width: 52px; height: 52px; min-width: 52px; border-radius: var(--r-md); object-fit: cover; }
  .ap-card-ic-fallback {
    display: flex; align-items: center; justify-content: center; font-size: 20px; color: #fff;
    background: linear-gradient(135deg, var(--violet), var(--orange));
  }
  .ap-card-body { min-width: 0; display: flex; flex-direction: column; gap: 4px; }
  .ap-card-name { display: flex; align-items: center; gap: 5px; min-width: 0; }
  .ap-src-dot { width: 7px; height: 7px; min-width: 7px; border-radius: 50%; }
  .ap-src-dot.mr { background: #1bd96a; }
  .ap-src-dot.cf { background: #f16436; }
  .ap-card-title { font-size: 13px; font-weight: 700; color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .ap-card-tags { display: flex; align-items: center; gap: 5px; }
  .ap-card-type, .ap-card-src { font-size: 10px; font-weight: 700; text-transform: uppercase; letter-spacing: .4px; border-radius: 4px; padding: 1px 5px; }
  .ap-card-type { background: rgba(255,255,255,.07); color: var(--text-mute); }
  .ap-card-src.mr { background: rgba(27,217,106,.15); color: #1bd96a; }
  .ap-card-src.cf { background: rgba(241,100,54,.15); color: #f16436; }
  .ap-card-desc {
    font-size: 11.5px; color: var(--text-mute); line-height: 1.4;
    display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden;
  }
  .ap-card-meta { display: flex; align-items: center; gap: 6px; margin-top: auto; flex-wrap: wrap; }
  .ap-card-meta .chip { font-size: 10.5px; padding: 2px 7px; }
  .ap-downloads { font-size: 11px; color: var(--text-mute); display: inline-flex; align-items: center; gap: 4px; margin-left: auto; }
  .ap-downloads i { color: var(--orange); }
</style>
