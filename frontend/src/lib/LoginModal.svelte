<script lang="ts">
  import Portal from './Portal.svelte'
  import ShaurmaPerks from './ShaurmaPerks.svelte'
  // Модалка входу в акаунт. У режимі «воріт входу» (gate=true) не закривається
  // і блокує решту лаунчера, поки не додано хоча б один акаунт. Microsoft-
  // авторизацію та офлайн-вхід виконує батько через колбеки (onlogin —
  // «Увійти через Microsoft» / «Спробувати ще раз», oncancelms — скасувати
  // очікування, onpirate — офлайн-вхід з ніком).
  import { t } from './i18n.svelte'
  import { pirateNicknameError, sanitizeNick } from './validation'
  import steveHead from '../assets/images/steve-head.png'

  let {
    open = false,
    gate = false,
    step = 'idle' as 'idle' | 'waiting',
    skinstep = 'idle' as 'idle' | 'waiting',
    skingate = false,
    onclose = null as null | (() => void),
    onlogin = null as null | (() => void),
    oncancelms = null as null | (() => void),
    onpirate = null as null | ((username: string) => void),
    onskinlogin = null as null | (() => void),
    onskincancel = null as null | (() => void)
  }: {
    open?: boolean
    gate?: boolean
    step?: 'idle' | 'waiting'
    // skinstep — стан Google-входу в скін-систему: 'idle' — видно лише
    // кнопку Google (БЕЗ поля ніка); 'waiting' — чекаємо повернення з
    // браузера. Питання про нік (якщо його ще нема) — ОКРЕМЕ модальне
    // вікно SkinNickModal, яке відкриває батько.
    skinstep?: 'idle' | 'waiting'
    // skingate — чи доступна скін-система (Shaurma-редакція і не вимкнена
    // в налаштуваннях). Вирішує батько (App.svelte): сама модалка не знає
    // ні про редакцію білда, ні про налаштування.
    skingate?: boolean
    onclose?: null | (() => void)
    onlogin?: null | (() => void)
    oncancelms?: null | (() => void)
    onpirate?: null | ((username: string) => void)
    // onskinlogin — старт Google-входу в скін-систему (без ніка).
    onskinlogin?: null | (() => void)
    // onskincancel — скасувати очікування Google.
    onskincancel?: null | (() => void)
  } = $props()

  let username = $state('')
  let nickTouched = $state(false)
  const pirateNickErr = $derived(pirateNicknameError(username.trim()))

  // «Останній вхід» — запам'ятовуємо провайдера в localStorage (лише підказка для UI).
  let lastProv = $state('')
  try { lastProv = localStorage.getItem('shaurma.lastProvider') || '' } catch { /* приватний режим */ }
  function remember(p: string) { try { localStorage.setItem('shaurma.lastProvider', p) } catch { /* ignore */ } lastProv = p }
  function pickMs() { remember('microsoft'); onlogin?.() }
  function pickGoogle() { remember('google'); onskinlogin?.() }

  function tryPirate() {
    if (pirateNickErr || !username.trim()) return
    onpirate?.(username.trim())
  }

  // При закритті модалки скидаємо нік і стан «торкався поля».
  $effect(() => {
    if (!open) { username = ''; nickTouched = false }
  })
</script>

{#if open}
  <Portal>
<div class="modal-overlay" class:gate={gate} onclick={() => { if (!gate) onclose?.() }}>
    <div class="modal" onclick={(e: any) => e.stopPropagation()}>
      <div class="modal-head">
        {#if gate}
          <div class="gate-av"><img src={steveHead} alt=""/></div>
        {/if}
        <h3>{t('modal.title')}</h3>
      </div>
      <div class="modal-body">
        {#if gate}
          <div class="gate-hint">
            <i class="ti ti-user-plus"></i> {@html t('modal.gate.hint')}
          </div>
        {/if}
        <!-- Соціальні рекомендації: чому варто мати БЕЗКОШТОВНИЙ акаунт Шаурми (лише коли він доступний). -->
        {#if skingate && step !== 'waiting' && skinstep !== 'waiting'}
          <div style="margin-bottom:12px"><ShaurmaPerks compact /></div>
        {/if}
        <div style="display:flex;flex-direction:column;gap:12px">
          {#if step === 'waiting' || skinstep === 'waiting'}
            <div class="ms-waiting">
              <div class="spinner"></div>
              <span>{step === 'waiting' ? t('modal.waiting') : t('loginGoogleSkinWaiting')}</span>
              <div style="display:flex;gap:8px;margin-top:4px">
                {#if step === 'waiting'}
                  <button class="btn btn-primary btn-sm" onclick={onlogin}><i class="ti ti-refresh"></i> {t('modal.retry')}</button>
                {/if}
                {#if !gate || skinstep === 'waiting'}
                  <button class="btn btn-ghost btn-sm" onclick={step === 'waiting' ? oncancelms : onskincancel}>{t('cancel')}</button>
                {/if}
              </div>
            </div>
          {:else if skingate}
            <!-- Шаурма-вхід: лише ДВІ кнопки. Нік (якщо акаунта ще нема) питається ПІСЛЯ входу. -->
            <div class="prov-row">
              <button class="prov-btn" onclick={pickMs}>
                {#if lastProv === 'microsoft'}<span class="prov-last">{t('login.lastUsed')}</span>{/if}
                <svg width="20" height="20" viewBox="0 0 21 21" aria-hidden="true"><rect x="1" y="1" width="9" height="9" fill="#f25022"/><rect x="11" y="1" width="9" height="9" fill="#7fba00"/><rect x="1" y="11" width="9" height="9" fill="#00a4ef"/><rect x="11" y="11" width="9" height="9" fill="#ffb900"/></svg>
                <span>Microsoft</span>
              </button>
              <button class="prov-btn" onclick={pickGoogle}>
                {#if lastProv === 'google'}<span class="prov-last">{t('login.lastUsed')}</span>{/if}
                <svg width="20" height="20" viewBox="0 0 48 48" aria-hidden="true"><path fill="#EA4335" d="M24 9.5c3.5 0 6.6 1.2 9.1 3.6l6.8-6.8C35.8 2.4 30.3 0 24 0 14.6 0 6.5 5.4 2.6 13.2l7.9 6.1C12.4 13.6 17.7 9.5 24 9.5z"/><path fill="#4285F4" d="M46.5 24.5c0-1.6-.1-3.1-.4-4.5H24v9h12.7c-.6 3-2.3 5.5-4.8 7.2l7.6 5.9c4.4-4.1 7-10.1 7-17.6z"/><path fill="#FBBC05" d="M10.5 28.7c-.5-1.5-.8-3.1-.8-4.7s.3-3.2.8-4.7l-7.9-6.1C.9 16.4 0 20.1 0 24s.9 7.6 2.6 10.8l7.9-6.1z"/><path fill="#34A853" d="M24 48c6.5 0 11.9-2.1 15.9-5.8l-7.6-5.9c-2.1 1.4-4.9 2.3-8.3 2.3-6.3 0-11.6-4.1-13.5-9.8l-7.9 6.1C6.5 42.6 14.6 48 24 48z"/></svg>
                <span>Google</span>
              </button>
            </div>
            <div class="skin-hint"><i class="ti ti-info-circle"></i> {t('login.hint')}</div>
            <!-- Офлайн-акаунт «просто нік» — БЕЗ реєстрації Шаурми (для
                 розробки/тестів і тих, кому не потрібен Google/Microsoft).
                 Раніше в Shaurma-редакції цієї опції не було взагалі: модалка
                 показувала лише дві кнопки провайдерів. -->
            <div class="wiz-divider">{t('orOffline')}</div>
            <div class="wiz-field-row">
              <input class="input {pirateNickErr && nickTouched ? 'input-err' : ''}" style="flex:1" placeholder={t('nickname')}
                     value={username} maxlength="16"
                     oninput={(e: any) => { username = sanitizeNick(e.target.value); nickTouched = true }}
                     onkeydown={(e: any) => e.key === 'Enter' && !pirateNickErr && tryPirate()}
                     aria-invalid={!!(pirateNickErr && nickTouched)} />
              <button class="btn btn-ghost" disabled={!!pirateNickErr || !username.trim()} onclick={tryPirate}><i class="ti ti-user"></i> {t('login')}</button>
            </div>
            {#if pirateNickErr && nickTouched}
              <div class="field-err"><i class="ti ti-alert-triangle"></i> {pirateNickErr}</div>
            {/if}
            <div class="skin-hint"><i class="ti ti-user-question"></i> {t('login.offlineHint')}</div>
          {:else}
            <button class="btn btn-primary btn-lg" style="justify-content:center" onclick={pickMs}>
              <i class="ti ti-brand-windows"></i> {t('loginMicrosoft')}
            </button>
            <div class="wiz-divider">{t('orPirate')}</div>
            <div class="wiz-field-row">
              <!-- Нік офлайн-акаунта (base-збірка): a-zA-Z0-9_, 3–16 символів. -->
              <input class="input {pirateNickErr && nickTouched ? 'input-err' : ''}" style="flex:1" placeholder={t('nickname')}
                     value={username} maxlength="16"
                     oninput={(e: any) => { username = sanitizeNick(e.target.value); nickTouched = true }}
                     onkeydown={(e: any) => e.key === 'Enter' && !pirateNickErr && tryPirate()}
                     aria-invalid={!!(pirateNickErr && nickTouched)} />
              <button class="btn btn-ghost" disabled={!!pirateNickErr || !username.trim()} onclick={tryPirate}><i class="ti ti-user"></i> {t('login')}</button>
            </div>
            {#if pirateNickErr && nickTouched}
              <div class="field-err"><i class="ti ti-alert-triangle"></i> {pirateNickErr}</div>
            {/if}
          {/if}
        </div>
      </div>
      {#if !gate}
        <div class="modal-foot">
          <button class="btn btn-ghost" onclick={onclose}>{t('cancel')}</button>
        </div>
      {/if}
    </div>
  </div>
</Portal>
{/if}

<style>
  .gate-av {
    width:40px; height:40px; border-radius:10px; overflow:hidden;
    border:1px solid var(--border); flex-shrink:0;
  }
  .gate-av img { width:100%; height:100%; object-fit:cover; display:block; }
  .gate-hint {
    display:flex; align-items:center; gap:8px;
    font-size:12.5px; color:var(--text-mute); line-height:1.5;
    background:rgba(var(--orange-rgb),.07); border:1px solid rgba(var(--orange-rgb),.2);
    border-radius:10px; padding:10px 14px; margin-bottom:12px;
  }
  .gate-hint i { color:var(--orange); font-size:16px; flex-shrink:0; }
  .prov-row { display:grid; grid-template-columns:1fr 1fr; gap:10px; }
  .prov-btn {
    position:relative; height:46px; display:flex; align-items:center; justify-content:center; gap:10px;
    font:inherit; font-size:14px; font-weight:600; color:var(--text); cursor:pointer;
    background:var(--bg-panel); border:1px solid var(--border); border-radius:12px;
    transition:border-color .15s, background .15s, transform .1s;
  }
  .prov-btn:hover { border-color:rgba(var(--orange-rgb),.55); background:rgba(var(--orange-rgb),.06); }
  .prov-btn:active { transform:scale(.98); }
  .prov-last {
    position:absolute; top:-9px; right:12px; padding:1px 7px; border-radius:8px;
    font-size:10px; font-weight:600; line-height:14px; color:#2ecc71;
    background:var(--bg-panel); border:1px solid rgba(46,204,113,.7);
  }
  .skin-hint {
    display:flex; align-items:flex-start; gap:7px;
    font-size:11px; color:var(--text-mute); line-height:1.45;
  }
  .skin-hint i { color:var(--orange); font-size:13px; margin-top:1px; flex-shrink:0; }
</style>
