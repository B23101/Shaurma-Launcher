<script lang="ts">
  // Вибір Java для збірки: провайдер (як у лаунчері / Mojang / Temurin / Zulu)
  // або своя Java (java/javaw.exe). Своя Java одразу перевіряється бекендом:
  // чи це справжня Java і чи підходить її версія для версії гри.
  import { t } from './i18n.svelte'
  import { App } from './wails'
  import type { JavaCheck } from './types'

  let {
    provider = '',
    path = '',
    mcVersion = '',
    inherit = true,
    onchange,
  }: {
    provider?: string
    path?: string
    mcVersion?: string
    inherit?: boolean
    onchange: (provider: string, path: string) => void
  } = $props()

  const OPTIONS = $derived([
    ...(inherit ? [{ id: '', label: 'java.provider.inherit', icon: 'ti-settings' }] : []),
    { id: 'mojang', label: 'java.provider.mojang', icon: 'ti-cube' },
    { id: 'adoptium', label: 'java.provider.adoptium', icon: 'ti-coffee' },
    { id: 'zulu', label: 'java.provider.zulu', icon: 'ti-coffee' },
    { id: 'custom', label: 'java.provider.custom', icon: 'ti-file-code' },
  ])

  let check = $state<JavaCheck | null>(null)
  let checking = $state(false)
  let seq = 0

  $effect(() => {
    const p = path.trim()
    const v = mcVersion
    if (provider !== 'custom' || !p) { check = null; return }
    const my = ++seq
    checking = true
    const timer = setTimeout(async () => {
      try {
        const r = await App.InspectJava(p, v)
        if (my === seq) check = r
      } catch (e: any) {
        if (my === seq) check = { ok: false, status: 'error', error: String(e?.message ?? e) } as JavaCheck
      } finally {
        if (my === seq) checking = false
      }
    }, 250)
    return () => clearTimeout(timer)
  })

  async function browse() {
    try {
      const p = await App.BrowseJavaExe()
      if (p) onchange('custom', p)
    } catch { /* діалог скасовано */ }
  }

  const tone = $derived(
    !check ? '' : !check.ok ? 'bad' : check.status === 'ok' ? 'good' : check.status === 'too_old' ? 'bad' : 'warn'
  )
  const verdict = $derived.by(() => {
    if (!check) return ''
    if (!check.ok) return check.status === 'not_java' ? t('java.check.notJava') : (check.error || t('java.check.notJava'))
    const need = { major: check.required, mc: mcVersion }
    switch (check.status) {
      case 'too_old': return t('java.check.tooOld', need)
      case 'newer': return t('java.check.newer', need)
      case 'legacy': return t('java.check.legacy', need)
      case '32bit': return t('java.check.bits32')
      default: return mcVersion ? t('java.check.ok', need) : t('java.check.found')
    }
  })
</script>

<div class="jp">
  <div class="st-seg labeled jp-seg" role="radiogroup" aria-label={t('java.provider')}>
    {#each OPTIONS as o (o.id)}
      <button type="button" role="radio" aria-checked={provider === o.id} class:on={provider === o.id}
              onclick={() => onchange(o.id, o.id === 'custom' ? path : '')}>
        <i class="ti {o.icon}" aria-hidden="true"></i>{t(o.label)}
      </button>
    {/each}
  </div>

  {#if provider === 'custom'}
    <div class="folder-row jp-path">
      <input class="input mono" style="flex:1" value={path} placeholder={t('java.custom.placeholder')}
             aria-label={t('java.provider.custom')}
             onchange={(e) => onchange('custom', (e.currentTarget as HTMLInputElement).value)} />
      <button class="btn btn-ghost" onclick={browse} aria-label={t('java.custom.browse')}><i class="ti ti-file-search"></i></button>
    </div>
    {#if checking}
      <div class="jp-check"><i class="ti ti-loader spin"></i>{t('java.check.running')}</div>
    {:else if check}
      <div class="jp-check {tone}">
        <i class="ti {tone === 'good' ? 'ti-circle-check' : tone === 'bad' ? 'ti-circle-x' : 'ti-alert-triangle'}"></i>
        <div class="jp-text">
          {#if check.ok}
            <b>Java {check.version || check.major}</b>
            <span class="jp-meta">{[check.vendor, check.bits ? `${check.bits}-bit` : ''].filter(Boolean).join(' · ')}</span>
          {/if}
          <span>{verdict}</span>
        </div>
      </div>
    {/if}
  {/if}
</div>

<style>
  .jp { display: flex; flex-direction: column; gap: 10px; min-width: 0; }
  .jp-seg { flex-wrap: wrap; max-width: 100%; }
  .jp-check {
    display: flex; align-items: flex-start; gap: 9px;
    padding: 9px 12px; border-radius: var(--r-md);
    font-size: 12px; color: var(--text-mute);
    background: var(--bg-input); border: 1px solid var(--border);
  }
  .jp-check i { font-size: 16px; flex: none; margin-top: 1px; }
  .jp-text { display: flex; flex-wrap: wrap; gap: 4px 8px; min-width: 0; }
  .jp-text b { color: var(--text); }
  .jp-meta { color: var(--text-dim); }
  .jp-check.good { border-color: color-mix(in srgb, var(--green) 40%, transparent); }
  .jp-check.good i { color: var(--green); }
  .jp-check.warn { border-color: color-mix(in srgb, var(--yellow) 40%, transparent); }
  .jp-check.warn i { color: var(--yellow); }
  .jp-check.bad { border-color: color-mix(in srgb, var(--red) 45%, transparent); }
  .jp-check.bad i { color: var(--red); }
</style>
