<script lang="ts">
  // ── Розумний аналізатор запуску сервера ───────────────────────────────
  // Модалка, яка зʼявляється ПЕРЕД стартом, якщо бекенд знайшов проблеми
  // (порт/RAM/Java/EULA/диск/ядро/минулий краш). Той самий патерн, що й
  // попередження перед запуском збірки (RamWarningModal/PreLaunchModsWarning).
  //
  // Правила з налаштувань лаунчера:
  //   • показується ЛИШЕ те, що людина не вимкнула (бекенд фільтрує);
  //   • порт показується ЗАВЖДИ — його неможливо ігнорувати;
  //   • біля ігнорованих пунктів (RAM/Java/диск/краш) є галочка
  //     «більше не показувати» — вона пише окремий перемикач у
  //     Налаштування → Попередження.
  import { App } from '../../wails'
  import { toast } from '../../toast.svelte'
  import { t } from '../../i18n.svelte'
  import UiModal from '../../UiModal.svelte'
  import type { ServerPreflight } from '../../types'

  let {
    result,
    serverName = '',
    onstart = () => {},
    onclose = () => {},
  }: {
    result: ServerPreflight
    serverName?: string
    onstart?: () => void
    onclose?: () => void
  } = $props()

  // Локально сховані «більше не показувати» (після кліку зникають зі
  // списку одразу, без повторного запиту до бекенду).
  let hidden = $state<Set<string>>(new Set())
  const visible = $derived((result.warnings ?? []).filter(w => !hidden.has(w.kind)))
  const canStart = $derived(result.canStart !== false)

  const KIND_TO_SETTING: Record<string, string> = {
    ram: 'serverWarnIgnoreRam',
    java: 'serverWarnIgnoreJava',
    disk: 'serverWarnIgnoreDisk',
    crash: 'serverWarnIgnoreCrash',
  }

  function iconFor(level: string): string {
    if (level === 'error') return 'ti-circle-x'
    if (level === 'warn') return 'ti-alert-triangle'
    return 'ti-info-circle'
  }

  // «Більше не показувати» → окремий перемикач у налаштуваннях лаунчера.
  async function ignoreKind(kind: string) {
    const key = KIND_TO_SETTING[kind]
    if (!key) return
    try {
      const s: Record<string, unknown> = await App.GetSettings() as unknown as Record<string, unknown>
      s[key] = true
      await App.SaveSettings(s as any)
      hidden = new Set(hidden).add(kind)
      toast(t('servers.preflight.ignored'), 'success')
    } catch (e: any) {
      toast(String(e), 'error')
    }
  }
</script>

<UiModal open={true} {onclose} icon="ti-stethoscope" width="560px" title={t('servers.preflight.title')}>
  <div class="smp-sub">{t('servers.preflight.sub', { name: serverName })}</div>
  {#each visible as w (w.kind)}
    <div class="smp-w {w.level}" class:fatal={w.fatal}>
      <i class="ti {iconFor(w.level)}"></i>
      <div class="smp-w-body">
        <div class="smp-w-msg">{w.message}</div>
        {#if w.hint}<div class="smp-w-hint">{w.hint}</div>{/if}
      </div>
      {#if w.ignorable && KIND_TO_SETTING[w.kind]}
        <button class="btn btn-ghost btn-sm" onclick={() => ignoreKind(w.kind)} title={t('servers.preflight.ignoreHint')}>
          <i class="ti ti-eye-off"></i> {t('servers.preflight.ignore')}
        </button>
      {:else if w.kind === 'port'}
        <span class="smp-lock" title={t('servers.preflight.portLock')}><i class="ti ti-lock"></i></span>
      {/if}
    </div>
  {/each}
  {#if visible.length === 0}
    <div class="smp-ok"><i class="ti ti-circle-check"></i> {t('servers.preflight.allOk')}</div>
  {/if}
  {#snippet footer()}
    {#if !canStart}
      <div class="smp-blocked"><i class="ti ti-lock"></i> {t('servers.preflight.blocked')}</div>
    {/if}
    <button class="btn btn-ghost" onclick={onclose}>{t('servers.preflight.cancel')}</button>
    <button class="btn btn-primary" disabled={!canStart} onclick={onstart}>
      <i class="ti ti-player-play"></i> {t('servers.preflight.start')}
    </button>
  {/snippet}
</UiModal>

<style>
  .smp-sub { font: 500 12px/1.5 var(--font-ui); color: var(--text-mute); margin-bottom: 12px; }

  .smp-w {
    display: flex; align-items: flex-start; gap: 10px;
    padding: 10px 12px; border-radius: 10px; margin-bottom: 8px;
    border: 1px solid var(--border); background: var(--bg-input);
  }
  .smp-w > i { font-size: 17px; flex-shrink: 0; margin-top: 1px; color: var(--text-dim); }
  .smp-w.warn { border-color: rgba(234, 179, 8, .4); background: rgba(234, 179, 8, .07); }
  .smp-w.warn > i { color: #eab308; }
  .smp-w.error { border-color: rgba(239, 68, 68, .45); background: rgba(239, 68, 68, .08); }
  .smp-w.error > i { color: var(--red); }
  .smp-w.info > i { color: var(--blue); }
  .smp-w.fatal { box-shadow: inset 3px 0 0 0 currentColor; }

  .smp-w-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 3px; }
  .smp-w-msg { font: 600 12.5px/1.4 var(--font-ui); color: var(--text); }
  .smp-w-hint { font: 400 11px/1.45 var(--font-ui); color: var(--text-mute); }

  .smp-lock { flex-shrink: 0; color: var(--text-dim); font-size: 13px; }

  .smp-ok { display: flex; align-items: center; gap: 8px; color: var(--green); font: 600 13px/1 var(--font-ui); padding: 8px 0; }
  .smp-blocked {
    flex: 1; display: flex; align-items: center; gap: 6px;
    font: 600 11px/1.3 var(--font-ui); color: var(--red);
  }
</style>
