<script lang="ts">
  // ── Список серверів як цілей встановлення ─────────────────────────────
  // Використовується ВСІМА місцями браузера контенту, де є вибір цілі
  // (сайдбар ContentBrowser, перемикач цілі на сторінці проєкту, діалог
  // «Встановити»), щоб сервери скрізь виглядали й поводились однаково:
  //   • реальна іконка сервера (власна або іконка його ядра) і колір;
  //   • показуємо ЛИШЕ сервери, що підтримують поточний вид контенту
  //     (моди → мод-ядра, плагіни → плагін-ядра).
  import { t } from '../i18n.svelte'
  import type { BrowserServerCompatEntry } from '../types'
  import {
    kindForTab, serversForTab, serverVisual, coreName,
    type BrowserServer,
  } from './serverTargets'

  let {
    servers = [],
    // Вкладка браузера ('mod' | 'plugin' | …) — визначає, які сервери
    // підтримують поточний контент.
    tab = 'mod',
    // id вибраного сервера.
    selectedId = '',
    onpick = (_s: BrowserServer) => {},
    // Залишено для сумісності з викликами: вигляд однаковий скрізь
    // (рядок як у збірок), радіо-кружечки прибрано.
    variant = 'radio' as 'radio' | 'row',
    // Ім'я групи радіо (щоб не конфліктувати з іншими радіо на сторінці).
    name = 'cb-target',
    // allKinds — сервер підходить, якщо тримає БУДЬ-ЩО (моди АБО плагіни):
    // для колекцій, де змішано різні типи. Тоді `tab` ігнорується.
    allKinds = false,
    // compat — результат перевірки сумісності (GetBrowserServerCompat):
    // несумісні сервери показуються вимкненими з причиною. null — без перевірки.
    compat = null as BrowserServerCompatEntry[] | null,
    // checking — перевірка ще йде (рядки з індикатором, клік недоступний).
    checking = false,
  }: {
    servers?: BrowserServer[]
    tab?: string
    selectedId?: string
    onpick?: (s: BrowserServer) => void
    variant?: 'radio' | 'row'
    name?: string
    allKinds?: boolean
    compat?: BrowserServerCompatEntry[] | null
    checking?: boolean
  } = $props()

  const compatById = $derived(new Map((compat ?? []).map(c => [c.serverId, c])))
  // Несумісний сервер — лише коли перевірка завершилась і сказала «ні».
  const blocked = (id: string) => !!compat && compatById.get(id)?.compatible === false
  function reasonText(id: string): string {
    const c = compatById.get(id)
    if (!c) return ''
    if (c.reason === 'clientOnly') return t('browser.target.clientOnly')
    if (c.reason === 'noVersion') return t('browser.target.noVersion')
    if (c.reason === 'unsupportedCore') return t('browser.target.unsupportedCore')
    return ''
  }

  // Сервери, придатні для поточної вкладки (решту НЕ показуємо взагалі).
  const shown = $derived.by(() => {
    const list = allKinds
      ? servers.filter(s => serversForTab([s], 'mod').length > 0 || serversForTab([s], 'plugin').length > 0)
      : serversForTab(servers, tab)
    // Сумісні — першими (стабільне сортування зберігає порядок усередині груп).
    return compat ? list.slice().sort((a, b) => Number(blocked(a.id)) - Number(blocked(b.id))) : list
  })
  const kind = $derived(kindForTab(tab))

</script>

{#if servers.length === 0}
  <div class="stl-empty">{t('browser.target.noServers')}</div>
{:else if shown.length === 0}
  <div class="stl-empty">
    <i class="ti ti-server-off"></i>
    <div>{kind === 'plugins' ? t('browser.target.noPluginServers') : t('browser.target.noModServers')}</div>
  </div>
{:else}
  {#each shown as s (s.id)}
    {@const vis = serverVisual(s)}
    {@const off = blocked(s.id) || checking}
    {@const cs = compatById.get(s.id)}
      <div
        class="pp-row"
        class:compact={variant === 'radio'}
        class:sel={selectedId === s.id}
        class:off={blocked(s.id)}
        style="--pcol:{vis.color}"
        role="button"
        tabindex={off ? -1 : 0}
        aria-disabled={off}
        title={blocked(s.id) ? reasonText(s.id) : undefined}
        onclick={() => { if (!off) onpick(s) }}
        onkeydown={(e) => { if (!off && (e.key === 'Enter' || e.key === ' ')) { e.preventDefault(); onpick(s) } }}
      >
        <span class="pp-ic" class:has-icon={!!vis.src}>
          {#if vis.src}<img src={vis.src} alt="" />{:else}<i class="ti {vis.ti || 'ti-server-2'}"></i>{/if}
        </span>
        <span class="pp-info">
          <span class="pp-name">{s.name}</span>
          <span class="pp-sub">{coreName(s)} · {s.mcVersion}</span>
          {#if checking}
            <span class="pp-note"><i class="ti ti-loader spin"></i> {t('browser.compatChecking')}</span>
          {:else if blocked(s.id)}
            <span class="pp-note bad"><i class="ti ti-ban"></i> {reasonText(s.id)}</span>
          {:else if cs?.alreadyInstalled}
            <span class="pp-note warn"><i class="ti ti-alert-triangle"></i> {t('browser.alreadyInstalled', { file: cs.installedFileName ?? '' })}</span>
          {:else if cs?.unconfirmed}
            <span class="pp-note warn"><i class="ti ti-help-circle"></i> {t('browser.target.unconfirmed')}</span>
          {/if}
        </span>
        {#if selectedId === s.id}
          <i class="ti ti-check pp-check"></i>
        {/if}
      </div>
  {/each}

{/if}

<style>
  .stl-empty {
    padding: 10px 8px;
    font: 500 11.5px/1.45 var(--font-ui);
    color: var(--text-dim);
    text-align: center;
    display: flex; flex-direction: column; align-items: center; gap: 4px;
  }
  .stl-empty i { font-size: 18px; opacity: .7; }


  /* Рядок сервера (variant="row") — 1-в-1 стилі рядка збірки з PackPicker,
     щоб сервери в списку цілей виглядали точно як збірки, без власного
     дизайну. Svelte scope'ить CSS до файлу, тому клас однакового імені
     треба продублювати тут (спільного стилю між компонентами нема). */
  .pp-row {
    display: flex; align-items: center; gap: 9px;
    padding: 6px 8px; border-radius: var(--r-md);
    cursor: pointer; user-select: none;
    transition: background .15s, box-shadow .15s;
  }
  .pp-row.compact { padding: 6px 10px; font-size: 12px; }
  .pp-row:hover { background: var(--hover); }
  .pp-row.sel {
    background: color-mix(in srgb, var(--pcol, var(--orange)) 12%, var(--hover));
    box-shadow: inset 2px 0 0 0 var(--pcol, var(--orange));
  }
  .pp-ic {
    width: 28px; height: 28px; min-width: 28px; border-radius: 7px;
    display: flex; align-items: center; justify-content: center;
    background: var(--bg-input); overflow: hidden;
  }
  .pp-ic img {
    width: 100%; height: 100%;
    max-width: 100%; max-height: 100%;
    object-fit: cover; display: block;
  }
  .pp-ic:not(.has-icon) { color: var(--pcol, var(--orange)); }
  .pp-ic i { font-size: 14px; }
  .pp-info { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
  .pp-name {
    font-size: 12.5px; font-weight: 700; color: var(--text);
    white-space: nowrap; overflow: hidden; text-overflow: ellipsis;
  }
  .pp-sub { font-size: var(--fs-min); color: var(--text-mute); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .pp-check { color: var(--pcol, var(--orange)); font-size: 15px; flex-shrink: 0; }
  .pp-row.off { cursor: not-allowed; opacity: .55; }
  .pp-row.off:hover { background: transparent; }
  .pp-note { display: inline-flex; align-items: center; gap: 4px; font-size: var(--fs-min); color: var(--text-dim); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .pp-note.bad { color: var(--red); }
  .pp-note.warn { color: var(--orange); }
</style>
