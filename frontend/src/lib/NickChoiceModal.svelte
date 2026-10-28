<script lang="ts">
  // «Придумайте нік» — крок ПІСЛЯ входу через Google / Microsoft (особу підтверджено, акаунта Шаурми
  // ще нема). Нік унікальний у Шаурмі й не може збігатися з іменем гравця Mojang: за ним у грі
  // підвантажується скін. Живу перевірку робить сервер (CheckShaurmaNick); 🎲 підбирає вільний
  // осмислений нік (ПрикметникІменник).
  import { untrack } from 'svelte'
  import UiModal from './UiModal.svelte'
  import './settings/settings.css'
  import { t } from './i18n.svelte'
  import { App } from './wails'
  import { pirateNicknameError, sanitizeNick } from './validation'
  import { generateNick } from './nickgen'

  let {
    open = false,
    provider = 'google' as string,
    noLicense = '' as string,
    onconfirm = null as null | ((nick: string) => Promise<void>),
    oncancel = null as null | (() => void),
  } = $props()

  let nick = $state('')
  let st = $state<'idle' | 'checking' | 'ok' | 'bad'>('idle')
  let msg = $state('')
  let busy = $state(false)
  let rolling = $state(false)
  let submitErr = $state('')
  let seq = 0
  let timer: ReturnType<typeof setTimeout> | null = null

  async function check(value: string): Promise<boolean> {
    const my = ++seq
    const local = pirateNicknameError(value)
    if (!value || local) { st = value ? 'bad' : 'idle'; msg = local; return false }
    st = 'checking'; msg = ''
    try {
      const r = await App.CheckShaurmaNick(value)
      if (my !== seq) return false
      st = r.available ? 'ok' : 'bad'
      msg = r.available ? '' : (r.message || t('nick.taken'))
      return r.available
    } catch (e: any) {
      if (my !== seq) return false
      st = 'idle'; msg = String(e?.message ?? e)
      return false
    }
  }

  function onInput(v: string) {
    nick = sanitizeNick(v); submitErr = ''
    if (timer) clearTimeout(timer)
    st = nick ? 'checking' : 'idle'
    timer = setTimeout(() => check(nick), 350)
  }

  // 🎲: до 6 кандидатів, перший вільний лишається в полі.
  async function roll() {
    if (rolling) return
    rolling = true; submitErr = ''
    try {
      for (let i = 0; i < 6; i++) {
        const cand = generateNick(i >= 2)
        nick = cand
        if (await check(cand)) return
      }
    } finally { rolling = false }
  }

  async function submit() {
    if (busy || !(await check(nick.trim()))) return
    busy = true; submitErr = ''
    try { await onconfirm?.(nick.trim()) }
    catch (e: any) { submitErr = String(e?.message ?? e) ; st = 'bad' }
    finally { busy = false }
  }

  $effect(() => {
    if (open) { untrack(() => { nick = ''; st = 'idle'; msg = ''; submitErr = ''; roll() }) }
    else if (timer) { clearTimeout(timer); timer = null }
  })
</script>

<UiModal {open} dismissible={false} icon="ti-user-edit" title={t('nick.title')}>
  <div style="display:flex;flex-direction:column;gap:12px">
    {#if provider === 'microsoft' && noLicense}
      <div class="nc-note warn">
        <i class="ti ti-alert-circle"></i>
        <span>{noLicense === 'no_xbox' ? t('nick.noXbox') : t('nick.noLicense')}</span>
      </div>
    {:else}
      <div class="nc-note"><i class="ti ti-info-circle"></i><span>{t('nick.intro')}</span></div>
      {#if provider === 'google'}
        <!-- Акаунт Шаурми шукається за парою «провайдер + особа»: акаунт, створений через
             Microsoft або інший Google, за цим входом НЕ знаходиться. Кажемо про це одразу,
             щоб людина не створювала дубль замість входу в наявний. -->
        <div class="nc-note warn"><i class="ti ti-alert-circle"></i><span>{t('nick.googleNew')}</span></div>
      {/if}
    {/if}
    <div class="wiz-field-row">
      <input class="st-input grow {st === 'bad' ? 'input-err' : ''}" maxlength="16"
             placeholder={t('nickname')} value={nick}
             oninput={(e: any) => onInput(e.target.value)}
             onkeydown={(e: any) => e.key === 'Enter' && submit()} />
      <button class="btn btn-ghost" title={t('nick.random')} disabled={rolling || busy} onclick={roll}>
        <i class="ti ti-dice-5 {rolling ? 'nc-spin' : ''}"></i>
      </button>
    </div>
    <div class="nc-status {st}">
      {#if st === 'checking'}<i class="ti ti-loader-2 nc-spin"></i> {t('nick.checking')}
      {:else if st === 'ok'}<i class="ti ti-circle-check"></i> {t('nick.free')}
      {:else if st === 'bad' && (submitErr || msg)}<i class="ti ti-alert-triangle"></i> {submitErr || msg}
      {:else}&nbsp;{/if}
    </div>
    <div class="nc-hint"><i class="ti ti-shirt"></i> {t('nick.skinHint')}</div>
  </div>
  {#snippet footer()}
    <button class="btn btn-ghost" disabled={busy} onclick={oncancel}>{t('cancel')}</button>
    <button class="btn btn-primary" disabled={busy || rolling || st !== 'ok'} onclick={submit}>
      {#if busy}<i class="ti ti-loader-2 nc-spin"></i>{:else}<i class="ti ti-check"></i>{/if}
      {t('nick.create')}
    </button>
  {/snippet}
</UiModal>

<style>
  .nc-note { display:flex; gap:8px; align-items:flex-start; font-size:12.5px; line-height:1.5; color:var(--text-mute);
    background:rgba(var(--orange-rgb),.07); border:1px solid rgba(var(--orange-rgb),.2); border-radius:10px; padding:10px 14px; }
  .nc-note.warn { background:rgba(255,170,0,.08); border-color:rgba(255,170,0,.3); }
  .nc-note i { color:var(--orange); font-size:16px; flex-shrink:0; margin-top:1px; }
  .nc-status { min-height:18px; font-size:12px; display:flex; gap:6px; align-items:center; color:var(--text-mute); }
  .nc-status.ok { color:#2ecc71; }
  .nc-status.bad { color:#ff6b6b; }
  .nc-hint { display:flex; gap:7px; font-size:11px; color:var(--text-mute); line-height:1.45; }
  .nc-hint i { color:var(--orange); font-size:13px; }
  .nc-spin { animation: nc-rot 1s linear infinite; }
  @keyframes nc-rot { to { transform: rotate(360deg); } }
</style>
