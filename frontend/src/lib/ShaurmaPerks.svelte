<script lang="ts">
  // «Соціальні рекомендації» — блок-запрошення створити акаунт Шаурми.
  // Показується там, де людина вирішує, входити їй чи ні: «ворота входу» (LoginModal)
  // і крок «Акаунт» майстра першого запуску (Wizard).
  //
  // Правила тексту (marketingskills: signup / onboarding / copywriting):
  //   • заголовок — вигода, а не назва функції; одна дія (CTA — кнопки входу поруч);
  //   • жодних слів про гроші («безкоштовно», «без картки»): вони наводять на думку, що щось платне;
  //     знімаємо лише тертя входу — «без пароля, за кілька секунд»;
  //   • усі пункти — реальні можливості акаунта; жодних вигаданих лічильників чи «N тисяч гравців»;
  //   • чесне зняття заперечення: лаунчер працює і без акаунта (офлайн-нік лишається).
  import { t } from './i18n.svelte'

  let { compact = false } = $props<{ compact?: boolean }>()

  const perks = [
    { icon: 'ti-users', title: 'perks.friends.title', text: 'perks.friends.text' },
    { icon: 'ti-world-share', title: 'perks.p2p.title', text: 'perks.p2p.text' },
    { icon: 'ti-hanger', title: 'perks.skin.title', text: 'perks.skin.text' },
    { icon: 'ti-cloud-check', title: 'perks.time.title', text: 'perks.time.text' }
  ]
</script>

<section class="sp" class:compact aria-label={t('perks.title')}>
  <!-- Оригінальна ілюстрація: троє гравців-«блоків» і чат між ними. Кольори — з теми (--orange/--green). -->
  <div class="sp-art" aria-hidden="true">
    <svg viewBox="0 0 360 150" xmlns="http://www.w3.org/2000/svg" preserveAspectRatio="xMidYMid meet">
      <defs>
        <radialGradient id="sp-glow" cx=".5" cy=".5" r=".5">
          <stop offset="0" style="stop-color:rgba(var(--orange-rgb),.55)" />
          <stop offset="1" style="stop-color:rgba(var(--orange-rgb),0)" />
        </radialGradient>
      </defs>
      <!-- пікселі-«зірки» -->
      <g fill="rgba(255,255,255,.35)">
        <rect x="24" y="18" width="3" height="3" /><rect x="88" y="34" width="2" height="2" />
        <rect x="150" y="14" width="2" height="2" /><rect x="232" y="26" width="3" height="3" />
        <rect x="300" y="16" width="2" height="2" /><rect x="334" y="48" width="3" height="3" />
      </g>
      <!-- нитки зв'язку між гравцями -->
      <g class="sp-links" stroke-width="2" stroke-dasharray="3 5" fill="none" stroke-linecap="round" style="stroke:rgba(var(--orange-rgb),.7)">
        <path d="M82 92 Q130 40 180 58" /><path d="M278 92 Q230 40 180 58" /><path d="M96 112 Q180 140 264 112" />
      </g>
      <!-- центр: світіння + «Безкоштовно» -->
      <circle cx="180" cy="62" r="46" fill="url(#sp-glow)" />
      <g transform="translate(180 62)">
        <rect x="-26" y="-26" width="52" height="52" rx="12" stroke-width="2" style="fill:var(--bg-panel);stroke:rgba(var(--orange-rgb),.9)" />
        <path d="M-11 0 l8 8 l15 -16" fill="none" stroke-width="5" stroke-linecap="round" stroke-linejoin="round" style="stroke:var(--green)" />
      </g>
      <!-- гравець 1 -->
      <g class="sp-p sp-p1" transform="translate(54 80)">
        <rect width="48" height="48" rx="8" fill="#e8892b" />
        <rect x="9" y="17" width="8" height="8" fill="#1a1a1a" /><rect x="31" y="17" width="8" height="8" fill="#1a1a1a" />
        <rect x="14" y="33" width="20" height="4" fill="#1a1a1a" />
        <circle cx="46" cy="4" r="6" stroke-width="2" style="fill:var(--green);stroke:var(--bg-panel)" />
      </g>
      <!-- гравець 2 -->
      <g class="sp-p sp-p2" transform="translate(256 80)">
        <rect width="48" height="48" rx="8" fill="#5b8def" />
        <rect x="9" y="17" width="8" height="8" fill="#1a1a1a" /><rect x="31" y="17" width="8" height="8" fill="#1a1a1a" />
        <rect x="12" y="31" width="24" height="4" fill="#1a1a1a" /><rect x="12" y="31" width="4" height="8" fill="#1a1a1a" /><rect x="32" y="31" width="4" height="8" fill="#1a1a1a" />
        <circle cx="46" cy="4" r="6" stroke-width="2" style="fill:var(--green);stroke:var(--bg-panel)" />
      </g>
      <!-- гравець 3 -->
      <g class="sp-p sp-p3" transform="translate(156 96)">
        <rect width="48" height="48" rx="8" fill="#a368e8" />
        <rect x="9" y="17" width="8" height="8" fill="#1a1a1a" /><rect x="31" y="17" width="8" height="8" fill="#1a1a1a" />
        <rect x="14" y="33" width="20" height="4" fill="#1a1a1a" />
        <circle cx="46" cy="4" r="6" stroke-width="2" style="fill:var(--green);stroke:var(--bg-panel)" />
      </g>
      <!-- бульбашки чату -->
      <g class="sp-bubble sp-b1">
        <rect x="108" y="38" width="46" height="24" rx="8" stroke="rgba(255,255,255,.18)" style="fill:var(--bg-panel)" />
        <circle cx="123" cy="50" r="3" style="fill:var(--text-mute)" /><circle cx="131" cy="50" r="3" style="fill:var(--text-mute)" /><circle cx="139" cy="50" r="3" style="fill:var(--text-mute)" />
      </g>
      <g class="sp-bubble sp-b2">
        <rect x="208" y="30" width="44" height="24" rx="8" style="fill:rgba(var(--orange-rgb),.95)" />
        <path d="M222 41 q4 -6 8 0 q4 -6 8 0 q0 6 -8 10 q-8 -4 -8 -10z" fill="#1a1a1a" />
      </g>
    </svg>
  </div>

  <div class="sp-body">
    <h2 class="sp-title">{t('perks.title')}</h2>
    <p class="sp-sub">{t('perks.sub')}</p>

    <ul class="sp-list">
      {#each perks as p}
        <li>
          <span class="sp-ico"><i class="ti {p.icon}"></i></span>
          <span class="sp-txt"><b>{t(p.title)}</b><small>{t(p.text)}</small></span>
        </li>
      {/each}
    </ul>

    <p class="sp-note"><i class="ti ti-shield-check"></i> {t('perks.note')}</p>
  </div>
</section>

<style>
  .sp {
    border: 1px solid rgba(var(--orange-rgb), .25); border-radius: 14px; overflow: hidden;
    background: linear-gradient(180deg, rgba(var(--orange-rgb), .06), transparent 60%), var(--bg-panel);
  }
  .sp-art {
    position: relative; height: 150px; border-bottom: 1px solid rgba(var(--orange-rgb), .18);
    background: linear-gradient(180deg, rgba(var(--orange-rgb), .28), rgba(var(--orange-rgb), .04));
  }
  .sp-art svg { width: 100%; height: 100%; display: block; }
  .sp-body { padding: 14px 16px 14px; }
  .sp-title { margin: 0 0 4px; font-size: 16px; font-weight: 800; line-height: 1.25; color: var(--text); }
  .sp-sub { margin: 0 0 12px; font-size: 12.5px; line-height: 1.5; color: var(--text-mute); }
  .sp-list { list-style: none; margin: 0 0 10px; padding: 0; display: grid; grid-template-columns: 1fr 1fr; gap: 8px; }
  .sp-list li {
    display: flex; gap: 9px; align-items: flex-start; padding: 9px 10px; border-radius: 10px;
    background: rgba(255, 255, 255, .03); border: 1px solid var(--border);
  }
  .sp-ico {
    flex: 0 0 28px; height: 28px; border-radius: 8px; display: grid; place-items: center; font-size: 16px;
    color: rgb(var(--orange-rgb)); background: rgba(var(--orange-rgb), .12);
  }
  .sp-txt { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
  .sp-txt b { font-size: 12.5px; font-weight: 700; color: var(--text); }
  .sp-txt small { font-size: 11px; line-height: 1.4; color: var(--text-mute); }
  .sp-note { display: flex; align-items: flex-start; gap: 6px; margin: 0; font-size: 11px; line-height: 1.45; color: var(--text-mute); }
  .sp-note i { color: var(--green); font-size: 14px; flex-shrink: 0; margin-top: 1px; }

  .compact .sp-art { height: 104px; }
  .compact .sp-body { padding: 10px 12px 12px; }
  .compact .sp-sub, .compact .sp-txt small { display: none; }
  .compact .sp-list { gap: 6px; margin-bottom: 8px; }

  @media (max-width: 560px) { .sp-list { grid-template-columns: 1fr; } }

  /* Легка «жива» анімація — вимикається, якщо користувач просить менше руху. */
  .sp-p, .sp-bubble { animation: sp-float 4.5s ease-in-out infinite; }
  .sp-p2 { animation-delay: -1.4s; } .sp-p3 { animation-delay: -2.8s; }
  .sp-b2 { animation-delay: -2s; }
  .sp-links { animation: sp-dash 6s linear infinite; }
  @keyframes sp-float { 0%, 100% { translate: 0 0; } 50% { translate: 0 -4px; } }
  @keyframes sp-dash { to { stroke-dashoffset: -64; } }
  @media (prefers-reduced-motion: reduce) { .sp-p, .sp-bubble, .sp-links { animation: none; } }
</style>
