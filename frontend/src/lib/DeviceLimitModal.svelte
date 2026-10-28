<script lang="ts">
  // Вхід на новому пристрої, а в акаунті вже максимум пристроїв: просимо обрати, який вилучити.
  // Особу вже підтверджено (id_token збережено в бекенді), тож другого входу в браузері не буде.
  // Нічого не видаляється: вилучений пристрій можна додати знову.
  import UiModal from './UiModal.svelte'
  import { t } from './i18n.svelte'
  import type { DeviceChoice } from './wails'

  let {
    open = false,
    devices = [] as DeviceChoice[],
    max = 10,
    onpick = null as null | ((id: string) => Promise<void>),
    oncancel = null as null | (() => void),
  } = $props()

  let sel = $state('')
  let busy = $state(false)
  let err = $state('')

  $effect(() => { if (open) { sel = ''; err = ''; busy = false } })

  async function go() {
    if (!sel || busy) return
    busy = true; err = ''
    try { await onpick?.(sel) }
    catch (e: any) { err = String(e?.message ?? e) }
    finally { busy = false }
  }
  const when = (ms: number) => (ms ? new Date(ms).toLocaleString() : '—')
</script>

<UiModal {open} dismissible={false} icon="ti-devices" title={t('dev.limit.title')}>
  <div style="display:flex;flex-direction:column;gap:12px">
    <p class="dl-text">{t('dev.limit.text', { max })}</p>
    <div class="dl-list" role="radiogroup">
      {#each devices as d (d.id)}
        <label class="dl-item" class:on={sel === d.id}>
          <input type="radio" name="dl" value={d.id} bind:group={sel} />
          <div>
            <b>{d.name || d.clientName}</b>
            <div class="dl-meta">
              {d.clientName}{d.os ? ' · ' + d.os : ''}{d.country ? ' · ' + d.country : ''}
              · {t('dev.lastSeen')}: {when(d.lastSeenAt)}
            </div>
          </div>
        </label>
      {/each}
    </div>
    {#if err}<div class="dl-err"><i class="ti ti-alert-triangle"></i> {err}</div>{/if}
  </div>
  {#snippet footer()}
    <button class="btn btn-ghost" disabled={busy} onclick={oncancel}>{t('dev.limit.cancel')}</button>
    <button class="btn btn-primary" disabled={!sel || busy} onclick={go}>
      {#if busy}<i class="ti ti-loader-2 spin"></i>{:else}<i class="ti ti-logout"></i>{/if}
      {t('dev.limit.pick')}
    </button>
  {/snippet}
</UiModal>

<style>
  .dl-text { margin:0; font-size:12.5px; line-height:1.5; color:var(--text-mute); }
  .dl-list { display:flex; flex-direction:column; gap:6px; max-height:300px; overflow:auto; }
  .dl-item { display:flex; gap:10px; align-items:flex-start; padding:10px 12px; border-radius:10px; cursor:pointer;
    background:var(--bg-input); border:1px solid var(--border); }
  .dl-item.on { border-color:var(--orange); background:rgba(var(--orange-rgb),.08); }
  .dl-item b { font:700 13px/1.3 var(--font-ui); color:var(--text); }
  .dl-meta { font-size:11.5px; color:var(--text-dim); margin-top:2px; }
  .dl-err { font-size:12px; color:#ff6b6b; display:flex; gap:6px; align-items:center; }
</style>
