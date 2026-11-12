<script lang="ts" module>
  // Попередній перегляд групи за токеном посилання. Кеш на рівні модуля: картка в чаті
  // перемальовується часто (скрол, нові повідомлення) — на сервер ходимо один раз на токен.
  export interface LinkPreview { title: string; icon?: string; color?: string; members: number; max: number; joined?: boolean }
  type PeekResult =
    | { kind: 'ok'; pv: LinkPreview }
    | { kind: 'gone' }        // посилання відкликано / прострочене / групи вже нема
    | { kind: 'denied' }      // вступ заборонено (блокування)
    | { kind: 'unknown' }     // сервер без link_peek, офлайн тощо — показуємо просту картку
  const cache = new Map<string, Promise<PeekResult>>()
</script>

<script lang="ts">
  import { tip as launcherTip } from './tooltip'

  // Картка запрошення в групу (посилання shaurma-launcher.org/group/join#…). Та сама мова оформлення,
  // що й InviteCard / ModCard: обкладинка, велика іконка, назва, статистика, кнопка. Уся логіка вступу
  // лишається в ShaurmaChat (onJoin) — тут лише попередній перегляд і розмітка.
  import { t } from '../i18n.svelte'
  import GroupAvatar from '../groups/GroupAvatar.svelte'
  import { safeGroupColor } from '../groups/groupTypes'
  import { App } from '../wails'

  let { token, own = false, joining = false, joined = false, onJoin }: {
    token: string
    own?: boolean
    joining?: boolean
    /** Щойно приєдналися через цю картку (сервер ще міг не віддати joined). */
    joined?: boolean
    onJoin: () => void
  } = $props()

  let res = $state<PeekResult | null>(null)

  function peek(tok: string): Promise<PeekResult> {
    const hit = cache.get(tok)
    if (hit) return hit
    const p: Promise<PeekResult> = App.GroupRequest('link_peek', { token: tok })
      .then((r: any): PeekResult => (r?.preview?.title ? { kind: 'ok', pv: r.preview as LinkPreview } : { kind: 'unknown' }))
      .catch((e: unknown): PeekResult => {
        const code = /social:([A-Za-z_ ]+)/.exec(String((e as any)?.message ?? e ?? ''))?.[1]?.trim().replace(/ /g, '_') ?? ''
        if (code === 'not_found' || code === 'expired') return { kind: 'gone' }
        if (code === 'not_allowed') return { kind: 'denied' }
        return { kind: 'unknown' }
      })
    cache.set(tok, p)
    // «unknown» не кешуємо: сервер могли оновити або мережа повернутись.
    p.then((r) => { if (r.kind === 'unknown') cache.delete(tok) })
    return p
  }

  $effect(() => {
    const tok = token
    res = null
    let alive = true
    peek(tok).then((r) => { if (alive) res = r })
    return () => { alive = false }
  })

  const pv = $derived(res?.kind === 'ok' ? res.pv : null)
  const isIn = $derived(joined || !!pv?.joined)
  const full = $derived(!!pv && !isIn && pv.max > 0 && pv.members >= pv.max)
  const dead = $derived(res?.kind === 'gone' || res?.kind === 'denied')
  const tint = $derived(safeGroupColor(pv?.color ?? ''))

  // «1 учасник / 2 учасники / 5 учасників» за правилами множини поточної мови.
  function membersText(n: number): string {
    let cat = 'many'
    try { cat = new Intl.PluralRules(document.documentElement.lang || 'uk').select(n) } catch { /* без Intl */ }
    return t('groups.card.members.' + (cat === 'one' || cat === 'few' ? cat : 'many'), { n })
  }
</script>

<article class="gl" class:dead style={tint ? `--gt:${tint}` : ''} data-testid="chat-group-link-card" aria-busy={res === null}>
  <div class="gl-cover">
    <i class="ti ti-users-group gl-deco" aria-hidden="true"></i>
    <div class="gl-fade"></div>
    <span class="gl-pill"><i class="ti ti-link"></i> {t('groups.card.kind')}</span>
  </div>

  <div class="gl-main">
    <div class="gl-av">
      {#if pv}
        <GroupAvatar title={pv.title} icon={pv.icon} color={pv.color} size={54} />
      {:else if res === null}
        <span class="gl-sk gl-sk-av"></span>
      {:else}
        <i class="ti ti-users-group gl-av-ph"></i>
      {/if}
    </div>
    <div class="gl-titles">
      {#if pv}
        <div class="gl-name" use:launcherTip={pv.title}>{pv.title}</div>
      {:else if res === null}
        <span class="gl-sk gl-sk-name"></span>
      {:else}
        <div class="gl-name">{dead ? t('groups.card.goneTitle') : t('groups.card.title')}</div>
      {/if}
      {#if pv}
        <div class="gl-sub"><i class="ti ti-users"></i><span>{membersText(pv.members)}</span></div>
      {:else if res === null}
        <span class="gl-sk gl-sk-sub"></span>
      {/if}
    </div>
  </div>

  <div class="gl-pad">
    {#if dead}
      <div class="gl-note bad"><i class="ti ti-circle-x"></i>{res?.kind === 'denied' ? t('groups.card.denied') : t('groups.card.gone')}</div>
    {:else if isIn}
      <div class="gl-note ok"><i class="ti ti-circle-check"></i>{own ? t('groups.card.yours') : t('groups.card.joined')}</div>
    {:else if full}
      <div class="gl-note bad"><i class="ti ti-users-minus"></i>{t('groups.card.full')}</div>
    {:else}
      <button class="gl-btn" data-testid="chat-group-link-join" onclick={onJoin} disabled={joining || res === null}>
        {#if joining}<i class="ti ti-loader-2 gl-spin"></i>{:else}<i class="ti ti-users-plus"></i>{/if}
        {t('groups.link.join')}
      </button>
    {/if}
  </div>
</article>

<style>
  .gl { --gt: var(--orange); position: relative; width: 100%; min-width: 0; border-radius: 14px; overflow: hidden; background: var(--bg-input);
    border: 1px solid var(--border-hi); animation: gl-in .34s cubic-bezier(.2, 1.1, .3, 1) both;
    transition: border-color .18s, box-shadow .18s, transform .18s, opacity .18s; }
  .gl:not(.dead):hover { border-color: color-mix(in srgb, var(--gt) 55%, transparent); box-shadow: 0 8px 26px rgba(0, 0, 0, .35); transform: translateY(-1px); }
  .gl.dead { opacity: .66; }
  @keyframes gl-in { from { opacity: 0; transform: translateY(6px) scale(.97); } to { opacity: 1; transform: none; } }

  .gl-cover { position: relative; height: 64px; overflow: hidden;
    background: linear-gradient(135deg, color-mix(in srgb, var(--gt) 38%, transparent), color-mix(in srgb, var(--gt) 6%, transparent) 70%), var(--bg-panel); }
  .gl-deco { position: absolute; right: 16px; top: 50%; transform: translateY(-50%) rotate(-8deg); font-size: 64px; color: rgba(255, 255, 255, .09); pointer-events: none; }
  .gl-fade { position: absolute; inset: 0; background: linear-gradient(180deg, rgba(0, 0, 0, .28) 0%, rgba(0, 0, 0, 0) 40%, var(--bg-input) 100%); }
  .gl-pill { position: absolute; top: 8px; left: 8px; display: inline-flex; align-items: center; gap: 5px; padding: 3px 9px; border-radius: 999px;
    font: 700 var(--fs-min)/1.4 var(--font-ui); letter-spacing: .2px; color: #fff; background: rgba(0, 0, 0, .55); border: 1px solid rgba(255, 255, 255, .14); }

  .gl-main { display: flex; align-items: flex-end; gap: 12px; padding: 0 12px; margin-top: -26px; position: relative; }
  .gl-av { flex: 0 0 auto; width: 62px; height: 62px; border-radius: 17px; display: grid; place-items: center; overflow: hidden;
    background: var(--bg-panel); border: 2px solid var(--bg-input); box-shadow: 0 6px 18px rgba(0, 0, 0, .45), 0 0 0 1px var(--border-hi); }
  .gl-av-ph { font-size: 26px; color: var(--text-dim); }
  .gl-titles { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 4px; padding-bottom: 2px; }
  .gl-name { font: 800 15px/1.2 var(--font-ui); color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; letter-spacing: -.1px; }
  .gl-sub { display: flex; align-items: center; gap: 6px; min-width: 0; font: 650 11.5px/1.2 var(--font-ui); color: var(--text-mute); }
  .gl-sub i { font-size: 13px; color: var(--gt); }

  .gl-pad { padding: 10px 12px 12px; display: flex; flex-direction: column; gap: 8px; }
  .gl-btn { display: inline-flex; align-items: center; justify-content: center; gap: 8px; width: 100%; padding: 9px 14px; border-radius: 10px; border: 0; cursor: pointer;
    background: var(--gt); color: #111; font: 800 13px/1 var(--font-ui); transition: filter .15s, transform .12s; }
  .gl-btn:hover:not(:disabled) { filter: brightness(1.1); }
  .gl-btn:active:not(:disabled) { transform: scale(.98); }
  .gl-btn:disabled { opacity: .6; cursor: default; }
  .gl-btn:focus-visible { outline: 2px solid var(--text); outline-offset: 2px; }
  .gl-btn i { font-size: 16px; }
  .gl-note { display: flex; align-items: center; gap: 7px; font: 650 12px/1.3 var(--font-ui); color: var(--text-mute); }
  .gl-note i { font-size: 15px; flex: none; }
  .gl-note.ok { color: var(--green); }
  .gl-note.bad { color: var(--red); }
  .gl-spin { animation: gl-spin .8s linear infinite; }
  @keyframes gl-spin { to { transform: rotate(360deg); } }

  /* Скелетон, поки йде запит */
  .gl-sk { display: block; border-radius: 7px; background: var(--bg-input-hi); animation: gl-pulse 1.2s ease-in-out infinite; }
  .gl-sk-av { width: 54px; height: 54px; border-radius: 15px; }
  .gl-sk-name { width: 62%; height: 14px; }
  .gl-sk-sub { width: 38%; height: 11px; animation-delay: .15s; }
  @keyframes gl-pulse { 50% { opacity: .45; } }
  @media (prefers-reduced-motion: reduce) { .gl, .gl-sk, .gl-spin { animation: none; } .gl { transition: none; } }
</style>
