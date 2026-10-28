<script lang="ts">
  // ── WhitelistPanel: «дозволені понад маніфест» моди шаурма-збірки ─────
  // Раніше ніде в лаунчері не було видно, ЯКІ саме моди автор збірки
  // дозволив через Pack Manager (whitelist) — лаунчер їх просто мовчки не
  // видаляв під час перевірки цілісності файлів, без жодного сліду в UI.
  // Ця панель показує список: іконка + назва + перехід на сторінку
  // Modrinth/CurseForge, коли запис додано за посиланням на проєкт
  // (platform/projectUrl заповнені); для jar/manual-записів — звичайна
  // картка без іконки, лише назва/modId.
  //
  // Компонент сам малює свою картку-обгортку (card-head + заголовок) і
  // САМ вирішує, чи показуватись: більшість шаурма-збірок білий список
  // ніколи не використовують (це опційна фіча Pack Manager) — для них
  // порожня картка «Дозволені моди понад маніфест» на кожній сторінці
  // збірки була б зайвим шумом. Тому при порожньому списку БЕЗ помилки
  // компонент рендерить null; помилку завантаження показує завжди —
  // мовчати про мережевий збій гірше, ніж показати зайву картку.
  import { App } from './wails'
  import type { WhitelistEntry } from './types'
  import { t } from './i18n.svelte'
  import modrinthIcon from '../assets/images/Modrinth_icon.png'
  import curseforgeIcon from '../assets/images/curseforge_icon.png'

  let { packId }: { packId: string } = $props()

  let entries = $state<WhitelistEntry[]>([])
  let loading = $state(true)
  let error = $state('')
  let loadedFor = ''

  async function load(id: string) {
    loading = true
    error = ''
    try {
      entries = (await App.GetPackWhitelist(id)) ?? []
    } catch (e: any) {
      error = String(e?.message ?? e)
    } finally {
      loading = false
      loadedFor = id
    }
  }

  $effect(() => {
    if (packId && packId !== loadedFor) load(packId)
  })

  function platformIcon(e: WhitelistEntry): string | null {
    if (e.iconUrl) return e.iconUrl
    if (e.platform === 'curseforge') return curseforgeIcon
    if (e.platform === 'modrinth') return modrinthIcon
    return null
  }
</script>

{#if loading || error || entries.length > 0}
  <div class="card">
    <div class="card-head">
      <div class="ci"><i class="ti ti-shield-check"></i></div>
      <div class="ctw"><div class="ct">{t('pack.whitelist.title')}</div></div>
    </div>
    {#if loading}
      <div class="wlp-state"><i class="ti ti-loader-2 spin"></i> {t('whitelist.loading')}</div>
    {:else if error}
      <div class="wlp-state wlp-error"><i class="ti ti-alert-triangle"></i> {error}</div>
    {:else}
      <div class="wlp-list">
        {#each entries as e (e.modId)}
          <div class="wlp-card">
            <div class="wlp-icon" class:wlp-icon-fallback={!e.iconUrl && !!platformIcon(e)}>
              {#if platformIcon(e)}
                <img src={platformIcon(e)} alt="" />
              {:else}
                <i class="ti ti-puzzle"></i>
              {/if}
            </div>
            <div class="wlp-body">
              <div class="wlp-name">{e.name || e.modId}</div>
              <div class="wlp-modid">{e.modId}</div>
            </div>
            {#if e.projectUrl}
              <a href={e.projectUrl} target="_blank" rel="noopener noreferrer" class="wlp-link" title={t('whitelist.openPage')}>
                <i class="ti ti-external-link"></i>
              </a>
            {/if}
          </div>
        {/each}
      </div>
    {/if}
  </div>
{/if}

<style>
  .wlp-state {
    display: flex; align-items: center; gap: 8px;
    padding: 14px; font-size: 12.5px; color: var(--text-dim);
  }
  .wlp-error { color: var(--red); }

  .wlp-list { display: flex; flex-direction: column; gap: 6px; }
  .wlp-card {
    display: flex; align-items: center; gap: 10px;
    padding: 8px 10px; border-radius: 10px;
    background: color-mix(in srgb, var(--bg-card) 85%, transparent);
    border: 1px solid var(--border);
  }
  .wlp-icon {
    width: 28px; height: 28px; border-radius: 7px; overflow: hidden; flex-shrink: 0;
    background: var(--bg); border: 1px solid var(--border);
    display: flex; align-items: center; justify-content: center;
    color: var(--text-dim); font-size: 13px;
  }
  .wlp-icon img { width: 100%; height: 100%; object-fit: cover; }
  .wlp-icon-fallback img { object-fit: contain; padding: 5px; }
  .wlp-body { flex: 1; min-width: 0; }
  .wlp-name {
    font-size: 12.5px; font-weight: 700; color: var(--text);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .wlp-modid {
    font-family: ui-monospace, monospace; font-size: var(--fs-min); color: var(--text-dim);
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .wlp-link {
    display: flex; align-items: center; justify-content: center;
    width: 26px; height: 26px; flex-shrink: 0; border-radius: 999px;
    color: var(--text-dim); border: 1px solid var(--border);
  }
  .wlp-link:hover { color: var(--orange); border-color: color-mix(in srgb, var(--orange) 50%, var(--border)); }
</style>
