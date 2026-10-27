<script lang="ts">
  // Верхня панель лаунчера: іконка/назва розділу, крихти вкладеності
  // (відкрита збірка / «Нова збірка» / аналітика), глобальний пошук і
  // віконні кнопки. Усі значення приходять пропсами від App.svelte.
  import { t } from './i18n.svelte'

  // Крихта: текст + режим «назад» (для аналітики — кнопка-повернення
  // в «Мої збірки»); null = крихт немає.
  type Crumb = null | { text: string; back: boolean; onback: (() => void) | null }

  let {
    icon = '',
    title = '',
    minimized = false,
    crumb = null as Crumb,
    isMaximized = false,
    onsearch = null as null | (() => void),
    onmin = null as null | (() => void),
    onmax = null as null | (() => void),
    onclose = null as null | (() => void)
  }: {
    icon?: string
    title?: string
    minimized?: boolean
    crumb?: Crumb
    isMaximized?: boolean
    onsearch?: null | (() => void)
    onmin?: null | (() => void)
    onmax?: null | (() => void)
    onclose?: null | (() => void)
  } = $props()
</script>

<div class="topbar" class:minimized={minimized}>
  {#key icon}
    <div class="tb-icon"><i class="ti {icon}"></i></div>
  {/key}
  {#key title}
    <div class="tb-titles">
      <div class="topbar-title">{title}</div>
    </div>
    {#if crumb}
      <div class="tb-sep"></div>
      <div class="topbar-crumbs">
        <i class="ti ti-chevron-right"></i>
        {#if crumb.back}
          <button class="crumb-back" onclick={() => crumb.onback?.()}>
            <i class="ti ti-arrow-left"></i> {crumb.text}
          </button>
        {:else}
          <span style="color:var(--text);font-weight:600;">{crumb.text}</span>
        {/if}
      </div>
    {/if}
  {/key}
  <div class="topbar-spacer"></div>
  <div class="search-top no-drag" onclick={onsearch} role="button" tabindex="0"
       onkeydown={(e: any) => (e.key === 'Enter' || e.key === ' ') && onsearch?.()}>
    <i class="ti ti-search"></i>
    <span>{t('topbar.search')}</span>
    <kbd>Ctrl+K</kbd>
  </div>
  <div class="ctrl-box">
    <button class="ctrl-btn" title={t('win.minimize')} onclick={onmin}><i class="ti ti-minus"></i></button>
    <button class="ctrl-btn" title={t('win.maximize')} onclick={onmax}><i class="ti {isMaximized ? 'ti-copy' : 'ti-square'}"></i></button>
    <button class="ctrl-btn close" title={t('win.close')} onclick={onclose}><i class="ti ti-x"></i></button>
  </div>
</div>

<style>
  .topbar {
    /* Вища панель (72px): кнопки вікна «дихають» — відступи між верхом і
       низом, а іконка РОЗДІЛУ + заголовок + опис сторінки живуть тут
       (заголовки/описи з контенту сторінок прибрані). */
    height:72px; min-height:72px;
    display:flex; align-items:center;
    padding:0 18px 0 20px; gap:12px;
    background:var(--bg-card);
    border-bottom:1px solid var(--border);
    user-select:none;
    -webkit-user-select:none;
    --wails-draggable: drag;
  }
  .tb-icon {
    width:40px; height:40px;
    display:flex; align-items:center; justify-content:center;
    font-size:28px; color:var(--orange);
    flex-shrink:0;
    filter:drop-shadow(0 2px 10px rgba(var(--orange-rgb),.5));
    animation:tb-pop-in .25s cubic-bezier(.34,1.56,.64,1) both;
  }
  @keyframes tb-pop-in {
    from { opacity:0; transform:scale(.7) rotate(-8deg); }
    to   { opacity:1; transform:scale(1) rotate(0deg); }
  }
  @keyframes tb-slide-in {
    from { opacity:0; transform:translateX(-10px); }
    to   { opacity:1; transform:translateX(0); }
  }
  .tb-titles { display:flex; flex-direction:column; gap:2px; min-width:0; animation:tb-slide-in .22s ease both .05s; justify-content:center; }
  /* Сторінка контенту: у верху сторінки topbar не дублює іконку/назву
     розділу (вони й так видно в табах браузера) — приховані, а при
     прокрутці вниз з'являються плавною анімацією. */
  .tb-icon, .tb-titles { transition: opacity .2s ease, transform .26s cubic-bezier(.34, 1.56, .64, 1); }
  .topbar.minimized .tb-icon,
  .topbar.minimized .tb-titles { opacity: 0; transform: translateY(-8px); pointer-events: none; }
  .topbar-title { font-size:16px; font-weight:700; white-space:nowrap; line-height:1.2; }
  .topbar-crumbs {
    font-size:11px; color:var(--text-mute); display:flex; align-items:center; gap:6px; white-space:nowrap;
    animation:tb-slide-in .22s ease both .1s;
  }
  .topbar-crumbs i { font-size:12px; }
  .tb-sep { width:1px; height:26px; background:var(--border); margin:0 4px; }

  .ctrl-box {
    display:flex; align-items:center;
    background:var(--hover);
    border:1px solid var(--border);
    border-radius:12px;
    padding:5px;
    margin-left:auto; margin-right:0;
    --wails-draggable: no-drag;
  }
  .ctrl-btn {
    width:34px; height:34px; border-radius:8px;
    border:none; background:transparent; cursor:pointer;
    color:var(--text-mute); font-size:15px;
    display:flex; align-items:center; justify-content:center;
    transition:background .15s, color .15s;
    --wails-draggable: no-drag;
  }
  .ctrl-btn:hover { background:var(--hover-strong); color:var(--text); }
  .ctrl-btn.close:hover { background:var(--red); color:#fff; }
  .no-drag { --wails-draggable: no-drag; }
</style>
