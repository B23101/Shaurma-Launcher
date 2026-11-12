<script lang="ts">
  // ── Прев'ю «так друзі побачать ваш профіль» (Налаштування → Акаунт) ─────────
  // Це зменшена копія реального вікна FriendProfileModal: та сама шапка (іконка кольору профілю,
  // нік, статус), та сама ліва колонка (обкладинка, аватар, нік, статус, значки) і картка «Про себе».
  // Розмітка й стилі навмисно ті самі, що у FriendProfileModal (класи fpm-*), щоб прев'ю не розходилося
  // з тим, що бачить друг (бейдж «Друзі з…» не показуємо: дата в кожного друга своя). Змінюєте вигляд вікна профілю — змініть і тут.
  import { t } from '../i18n.svelte'
  import { profileAccent } from './profileStyle'

  let { nickname = '', head = '', accountId = '', color = '', bio = '', handle = '' }: {
    nickname?: string
    head?: string
    accountId?: string
    /** Чернетка кольору: '#rrggbb' або '' (стандартний за ID). */
    color?: string
    /** Уже очищене біо ('' — порожнє). */
    bio?: string
    handle?: string
  } = $props()

  const accent = $derived(profileAccent(color, accountId))
  const initial = $derived((nickname || '?').trim().charAt(0).toUpperCase() || '?')
</script>

<div class="pp-wrap" aria-hidden="true">
  <div class="pp-modal" style="--ui-c:{accent}">
    <!-- Шапка — як у UiModal -->
    <div class="pp-head">
      <span class="pp-ico"><i class="ti ti-user-circle"></i></span>
      <div class="pp-ttw">
        <div class="pp-tt">{nickname || '—'}</div>
        <div class="pp-td">{t('social.status.online')}</div>
      </div>
    </div>

    <div class="pp-body">
      <div class="fpm" style="--cv:{accent}">
        <aside class="fpm-hero">
          <div class="fpm-cover"></div>
          <div class="fpm-ava">
            {#if head}<img src={head} alt="" />{:else}<span>{initial}</span>{/if}
            <i class="fpm-dot online"></i>
          </div>
          <div class="fpm-name">{nickname || '—'}</div>
          <div class="fpm-status online">{t('social.status.online')}</div>
          <div class="fpm-badges">
            {#if handle}<span class="fpm-badge plain"><i class="ti ti-at"></i> <b>@{handle}</b></span>{/if}
          </div>
        </aside>

        <section class="fpm-main">
          <div class="fpm-card">
            <div class="fpm-card-t"><i class="ti ti-quote"></i> {t('acc.modal.bio')}</div>
            {#if bio}
              <p class="fpm-bio">{bio}</p>
            {:else}
              <p class="fpm-bio empty">Так друзі побачать ваше біо</p>
            {/if}
          </div>
        </section>
      </div>
    </div>
  </div>
</div>

<style>
  /* Контейнерний запит: вікно профілю перемикається на одну колонку по ширині ВІКНА (@media), а прев'ю
     живе в картці налаштувань — тому дивимось на ширину самої картки. */
  .pp-wrap { container-type: inline-size; }
  .pp-modal { background: var(--bg-panel); border: 1px solid var(--border-hi); border-radius: var(--r-lg); overflow: hidden; }

  /* ── Шапка (копія .ui-head / .ui-ico / .ui-tt з UiModal) ── */
  .pp-head { display: flex; align-items: center; gap: 11px; padding: 14px 20px 12px; border-bottom: 1px solid var(--border); }
  .pp-ico {
    width: 38px; height: 38px; border-radius: 10px; flex-shrink: 0; display: flex; align-items: center; justify-content: center;
    background: color-mix(in srgb, var(--ui-c) 16%, transparent); color: var(--ui-c); font-size: 19px;
  }
  .pp-ico :global(.ti) { display: block; line-height: 1; transform: translateY(1px); }
  .pp-ttw { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .pp-tt { font-size: 15px; font-weight: 800; color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .pp-td { font-size: 11.5px; color: var(--text-mute); }
  .pp-body { padding: 16px 20px; }

  /* ── Тіло: копія стилів FriendProfileModal (лише те, що є в прев'ю) ── */
  .fpm { --cv: hsl(var(--h, 24) 72% 52%); display: grid; grid-template-columns: 252px minmax(0, 1fr); gap: 16px; align-items: start; }
  .fpm-hero {
    position: relative; display: flex; flex-direction: column; align-items: center; gap: 8px;
    padding: 0 14px 16px; overflow: hidden; border-radius: 18px;
    background: linear-gradient(180deg, color-mix(in srgb, var(--cv) 16%, var(--bg-panel)), var(--bg-panel) 68%);
    border: 1px solid color-mix(in srgb, var(--cv) 32%, var(--border));
  }
  .fpm-cover {
    align-self: stretch; height: 74px; margin: 0 -14px 0;
    background:
      radial-gradient(120% 140% at 15% 0%, color-mix(in srgb, var(--cv) 90%, #fff 10%), transparent 62%),
      radial-gradient(110% 130% at 100% 100%, color-mix(in srgb, var(--cv) 55%, #000), transparent 62%),
      color-mix(in srgb, var(--cv) 38%, var(--bg-input));
  }
  .fpm-ava {
    position: relative; width: 82px; height: 82px; margin-top: -42px; border-radius: 22px;
    display: grid; place-items: center; overflow: visible; color: var(--text-mute);
    background: var(--bg-input); border: 3px solid var(--bg-panel);
    box-shadow: 0 10px 26px rgba(0,0,0,.35);
  }
  .fpm-ava img { width: 100%; height: 100%; object-fit: cover; image-rendering: pixelated; border-radius: 19px; display: block; }
  .fpm-ava > span { font: 800 30px/1 var(--font-ui); }
  .fpm-dot { position: absolute; right: -4px; bottom: -4px; width: 18px; height: 18px; border-radius: 50%; border: 3px solid var(--bg-panel); background: var(--text-dim); }
  .fpm-dot.online { background: var(--ff-ok, #22c55e); }
  .fpm-name { margin-top: 2px; max-width: 100%; font: 800 18px/1.15 var(--font-ui); color: var(--text); overflow-wrap: anywhere; text-align: center; }
  .fpm-status { max-width: 100%; font: 600 11.5px/1.3 var(--font-ui); color: var(--text-dim); text-align: center; overflow-wrap: anywhere; }
  .fpm-status.online { color: var(--ff-ok, #22c55e); }
  .fpm-badges { display: flex; flex-wrap: wrap; justify-content: center; gap: 6px; margin-top: 2px; }
  .fpm-badge {
    display: inline-flex; align-items: center; gap: 5px; max-width: 100%; padding: 4px 9px; border-radius: 999px;
    border: 1px solid var(--border); background: var(--hover); color: var(--text-mute); font: 600 var(--fs-min)/1.2 var(--font-ui);
  }
  .fpm-badge b { font-weight: 700; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .fpm-badge .ti { font-size: 12px; }

  .fpm-main { display: grid; gap: 10px; min-width: 0; }
  .fpm-card { display: grid; gap: 8px; padding: 12px; border-radius: 14px; background: var(--bg-input); border: 1px solid var(--border); }
  .fpm-card-t { display: flex; align-items: center; gap: 6px; font: 700 11.5px/1 var(--font-ui); color: var(--text-dim); }
  .fpm-card-t i { color: var(--orange); font-size: 14px; }
  .fpm-bio { margin: 0; font: 450 12.5px/1.5 var(--font-ui); color: var(--text); overflow-wrap: anywhere; white-space: pre-wrap; }
  .fpm-bio.empty { color: var(--text-dim); }

  @container (max-width: 560px) {
    .fpm { grid-template-columns: minmax(0, 1fr); }
  }
</style>
