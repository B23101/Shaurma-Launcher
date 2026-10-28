<script lang="ts">
  import UiModal from './UiModal.svelte'
  import './settings/settings.css'
  // Окреме модальне вікно «придумайте нік» для скін-акаунта. Відкривається
  // батьком ЛИШЕ після Google-входу, якщо в акаунта ще нема ніка на сервері.
  // Нік валідується тими самими правилами, що й офлайн-нік (lib/validation).
  import { App } from './wails'
  import { t } from './i18n.svelte'
  import { pirateNicknameError, sanitizeNick } from './validation'

  let {
    open = false,
    busy = false,
    email = '',
    identity = 'google' as 'google' | 'microsoft',
    onconfirm = null as null | ((nick: string) => void),
    oncancel = null as null | (() => void)
  }: {
    open?: boolean
    busy?: boolean
    email?: string
    // Яка саме особистість ставить нік: Google-скін-акаунт чи Microsoft-
    // акаунт Шаурми. Від цього залежить лише формулювання підказки.
    identity?: 'google' | 'microsoft'
    onconfirm?: null | ((nick: string) => void)
    oncancel?: null | (() => void)
  } = $props()

  let nick = $state('')
  let touched = $state(false)
  const nickErr = $derived(pirateNicknameError(nick.trim()))

  // Жива перевірка зайнятості — лише ПІДКАЗКА (debounce 400 мс; токен
  // запиту відкидає застарілі відповіді). Остаточне рішення приймає сервер
  // при збереженні (409, якщо нік справді чужий), тож кнопка не блокується
  // відповіддю «зайнято»: вона може бути хибною, напр. на застарілому Worker.
  let check = $state<'idle' | 'checking' | 'ok' | 'taken'>('idle')
  let checkMsg = $state('')
  // Нік є в реєстрі Mojang (належить гравцю з ліцензією) — показуємо
  // ПОПЕРЕДЖЕННЯ (жовте), але НЕ блокуємо: політика «попереджувати».
  let mojangOwned = $state(false)
  let seq = 0
  let timer: ReturnType<typeof setTimeout> | null = null

  function scheduleCheck() {
    if (timer) { clearTimeout(timer); timer = null }
    seq++
    check = 'idle'; checkMsg = ''; mojangOwned = false
    const name = nick.trim()
    if (!name || pirateNicknameError(name)) return
    const mine = seq
    check = 'checking'
    timer = setTimeout(async () => {
      try {
        const r = await App.CheckSkinNickname(name)
        if (mine !== seq) return
        check = r.available ? 'ok' : 'taken'
        checkMsg = r.available ? '' : (r.message || t('skinNickTaken'))
        mojangOwned = r.available && !!r.mojangOwned
      } catch {
        if (mine === seq) { check = 'idle'; checkMsg = ''; mojangOwned = false }
      }
    }, 400)
  }

  function submit() {
    if (busy || !nick.trim() || nickErr) { touched = true; return }
    onconfirm?.(nick.trim())
  }

  // Закрили вікно — чистимо поле й таймер.
  $effect(() => {
    if (!open) {
      nick = ''; touched = false; check = 'idle'; checkMsg = ''; mojangOwned = false
      seq++
      if (timer) { clearTimeout(timer); timer = null }
    }
  })
</script>

<!-- Клік поза вікном НЕ закриває його: щоб випадково не втратити крок
     входу. Вийти можна лише кнопкою «Скасувати». -->
<UiModal {open} dismissible={false} size="sm" icon="ti-user-edit" title={t('skinNickModalTitle')}>
  <div class="nick-hint"><i class="ti ti-brand-google"></i>
    <span>{t(identity === 'microsoft' ? 'skinNickModalHintMs' : 'skinNickModalHint')}{#if email} <b>{email}</b>{/if}</span>
  </div>
  <div class="wiz-field-row" style="margin-top:12px">
    <!-- svelte-ignore a11y_autofocus -->
    <input class="st-input mono grow {(nickErr && touched) || check === 'taken' ? 'input-err' : ''}"
           placeholder={t('nickname')} value={nick} maxlength="16" autofocus
           oninput={(e: any) => { nick = sanitizeNick(e.target.value); touched = true; scheduleCheck() }}
           onkeydown={(e: any) => e.key === 'Enter' && submit()}
           aria-invalid={!!((nickErr && touched) || check === 'taken')} />
  </div>
  {#if nickErr && touched}
    <div class="field-err"><i class="ti ti-alert-triangle"></i> {nickErr}</div>
  {:else if check === 'taken'}
    <div class="field-err"><i class="ti ti-alert-triangle"></i> {checkMsg}</div>
  {:else if check === 'checking'}
    <div class="nick-note"><i class="ti ti-loader-2"></i> {t('skinNickChecking')}</div>
  {:else if check === 'ok' && mojangOwned}
    <!-- Нік вільний у нас, але належить ліцензійному акаунту:
         попереджаємо, що скіном у грі керує він і редагувати
         скін під цим ніком зі Шаурма-акаунта не вийде. -->
    <div class="nick-warn"><i class="ti ti-alert-triangle"></i> {t('skinNickMojangWarn')}</div>
  {:else if check === 'ok'}
    <div class="nick-ok"><i class="ti ti-circle-check"></i> {t('skinNickFree')}</div>
  {/if}
  {#snippet footer()}
    <button class="btn btn-ghost" onclick={oncancel} disabled={busy}>{t('cancel')}</button>
    <button class="btn btn-primary" disabled={busy || !nick.trim() || !!nickErr} onclick={submit}>
      <i class="ti ti-check"></i> {t('skinNickContinue')}
    </button>
  {/snippet}
</UiModal>

<style>
  .nick-hint {
    display:flex; align-items:flex-start; gap:8px;
    font-size:12.5px; color:var(--text-mute); line-height:1.5;
    background:rgba(var(--orange-rgb),.07); border:1px solid rgba(var(--orange-rgb),.2);
    border-radius:10px; padding:10px 14px;
  }
  .nick-hint i { color:var(--orange); font-size:16px; flex-shrink:0; margin-top:1px; }
  .nick-note { display:flex; align-items:center; gap:6px; font-size:11.5px; color:var(--text-mute); margin-top:6px; }
  .nick-ok { display:flex; align-items:center; gap:6px; font-size:11.5px; color:var(--green, #4caf50); margin-top:6px; }
  .nick-warn {
    display:flex; align-items:flex-start; gap:6px; font-size:11.5px; line-height:1.5;
    color:var(--yellow, #ffb300); margin-top:6px;
  }
  .nick-warn i { flex-shrink:0; margin-top:1px; }
</style>
