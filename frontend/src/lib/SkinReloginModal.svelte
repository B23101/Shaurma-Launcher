<script lang="ts">
  import Portal from './Portal.svelte'
  // Модальне вікно автоматичного ПЕРЕЗАХОДУ в Google-акаунт Shaurma.
  // Показується само (App.svelte), коли сервер відхилив refresh-токен
  // скін-сесії: людині не треба шукати кнопку входу в «Скінах» чи
  // налаштуваннях. Саму OAuth-логіку виконує батько (onlogin/oncancel) —
  // тут лише картка з однією головною дією.
  import { t } from './i18n.svelte'
  import steveHead from '../assets/images/steve-head.png'

  let {
    open = false,
    waiting = false,
    email = '',
    link = false,
    onlogin = null as null | (() => void),
    oncancel = null as null | (() => void),
    onlater = null as null | (() => void)
  }: {
    open?: boolean
    // true — браузер відкрито, чекаємо повернення з Google.
    waiting?: boolean
    // Пошта, з якою входили раніше (щоб було зрозуміло, який акаунт).
    email?: string
    // link — режим «прив'язки»: показується одразу після входу в Microsoft,
    // коли Google-сесії ще немає (без неї друзі/P2P/соціальне закриті).
    link?: boolean
    onlogin?: null | (() => void)
    oncancel?: null | (() => void)
    onlater?: null | (() => void)
  } = $props()
</script>

{#if open}
  <Portal>
    <!-- Клік поза вікном не закриває його: це важливий крок, закрити можна
         кнопкою «Пізніше». -->
    <div class="modal-overlay">
      <div class="modal" style="max-width:430px" role="dialog" aria-modal="true" aria-label={link ? t('skinLink.title') : t('skinRelogin.title')}>
        <div class="modal-head">
          <div class="rl-av"><img src={steveHead} alt="" /></div>
          <h3>{link ? t('skinLink.title') : t('skinRelogin.title')}</h3>
        </div>
        <div class="modal-body">
          <div class="rl-hint">
            <i class="ti ti-brand-google"></i>
            <span>{link ? t('skinLink.hint') : t('skinRelogin.hint')}{#if email} <b>{email}</b>{/if}</span>
          </div>
          {#if waiting}
            <div class="rl-wait"><i class="ti ti-loader-2 spin"></i> {t('skinRelogin.waiting')}</div>
          {/if}
        </div>
        <div class="modal-foot">
          {#if waiting}
            <button class="btn btn-ghost" onclick={oncancel}>{t('cancel')}</button>
            <button class="btn btn-primary" onclick={onlogin}>
              <i class="ti ti-refresh"></i> {t('skinRelogin.retry')}
            </button>
          {:else}
            <button class="btn btn-ghost" onclick={onlater}>{t('skinRelogin.later')}</button>
            <button class="btn btn-primary" onclick={onlogin}>
              <i class="ti ti-brand-google"></i> {t('skinRelogin.btn')}
            </button>
          {/if}
        </div>
      </div>
    </div>
  </Portal>
{/if}

<style>
  .rl-av {
    width:40px; height:40px; border-radius:10px; overflow:hidden;
    border:1px solid var(--border); flex-shrink:0;
  }
  .rl-av img { width:100%; height:100%; object-fit:cover; display:block; }
  .rl-hint {
    display:flex; align-items:flex-start; gap:8px;
    font-size:12.5px; color:var(--text-mute); line-height:1.5;
    background:rgba(var(--orange-rgb),.07); border:1px solid rgba(var(--orange-rgb),.2);
    border-radius:10px; padding:10px 14px;
  }
  .rl-hint i { color:var(--orange); font-size:16px; flex-shrink:0; margin-top:1px; }
  .rl-wait { display:flex; align-items:center; gap:8px; margin-top:12px; font-size:12.5px; color:var(--text-mute); }
</style>
