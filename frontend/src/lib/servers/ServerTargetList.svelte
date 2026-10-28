<script lang="ts">
  // ── Список серверів як цілей встановлення ─────────────────────────────
  // Використовується ВСІМА місцями браузера контенту, де є вибір цілі
  // (сайдбар ContentBrowser, перемикач цілі на сторінці проєкту, діалог
  // «Встановити»), щоб сервери скрізь виглядали й поводились однаково:
  //   • реальна іконка сервера (власна або іконка його ядра) і колір;
  //   • показуємо ЛИШЕ сервери, що підтримують поточний вид контенту
  //     (моди → мод-ядра, плагіни → плагін-ядра).
  import { onMount } from 'svelte'
  import { App } from '../wails'
  import { t } from '../i18n.svelte'
  import type { BrowserServerCompatEntry, ServerFolder } from '../types'
  import PackPicker from '../PackPicker.svelte'
  import type { PackRow, PackFolderRow, PackPickerItem } from '../PackPicker.svelte'
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
    // installButton — діалог встановлення: у кожному рядку кнопка «Встановити»
    // (як у списку збірок), клік по самому рядку нічого не робить.
    installButton = false,
    // installedDisabled — (лише з installButton) уже встановлений сервер: кнопка
    // «Встановлено» вимкнена. Інакше — зелена галочка + «Перевстановити».
    installedDisabled = false,
    // class — додатковий CSS-клас списку (ті самі, що й у PackPicker збірок).
    class: cls = '',
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
    installButton?: boolean
    installedDisabled?: boolean
    class?: string
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

  // Папки серверів — ті самі, що на сторінці «Сервери» (App.GetServerFolders).
  let folders = $state<ServerFolder[]>([])
  onMount(() => {
    App.GetServerFolders().then(f => { folders = f ?? [] }).catch(() => {})
  })

  const off = (id: string) => blocked(id) || checking

  // Дані для PackPicker: рядок на сервер (іконка/колір з serverVisual — те
  // саме джерело правди, що й раніше) + папки лише з показаних серверів.
  const picker = $derived.by(() => {
    const rowById = new Map<string, PackRow>()
    for (const s of shown) {
      const vis = serverVisual(s)
      const cs = compatById.get(s.id)
      let note: PackRow['note'] | undefined
      if (checking) note = { text: t('browser.compatChecking'), kind: 'busy' }
      else if (blocked(s.id)) note = { text: reasonText(s.id), kind: 'bad', icon: 'ti-ban' }
      else if (cs?.unconfirmed) note = { text: t('browser.target.unconfirmed'), kind: 'warn', icon: 'ti-help-circle' }
      else if (cs?.alreadyInstalled && !installButton) note = { text: t('browser.alreadyInstalled', { file: cs.installedFileName ?? '' }), kind: 'warn', icon: 'ti-alert-triangle' }
      rowById.set(s.id, {
        name: s.name,
        color: vis.color,
        iconSrc: vis.src,
        icon: vis.ti || 'ti-server-2',
        loader: coreName(s),
        mcVersion: s.mcVersion,
        note,
        disabled: off(s.id),
      })
    }
    const folderById = new Map<string, PackFolderRow>()
    const items: PackPickerItem[] = []
    const inFolder = new Set<string>()
    for (const f of folders) {
      const members = new Set(f.serverIds)
      const packIds = shown.filter(s => members.has(s.id) && !inFolder.has(s.id)).map(s => s.id)
      if (packIds.length === 0) continue
      packIds.forEach(id => inFolder.add(id))
      folderById.set(f.id, { name: f.name, color: f.color, packIds })
      items.push({ kind: 'folder', id: f.id })
    }
    for (const s of shown) if (!inFolder.has(s.id)) items.push({ kind: 'pack', id: s.id })
    return { items, rowById, folderById }
  })

  function pickById(id: string) {
    const s = shown.find(x => x.id === id)
    if (s && !off(id)) onpick(s)
  }

  // Лишаємо cb-pp для компактного вигляду в сайдбарі (variant="radio").
  const listClass = $derived([cls, variant === 'radio' ? 'cb-pp' : ''].filter(Boolean).join(' '))
</script>

{#snippet srvActions(id: string)}
  {@const cs = compatById.get(id)}
  {#if cs?.alreadyInstalled && installedDisabled}
    <button class="btn btn-ghost btn-sm" disabled><i class="ti ti-check"></i> {t('browser.installed.short')}</button>
  {:else}
    {#if cs?.alreadyInstalled}
      <span class="stl-installed-badge" title={t('browser.alreadyInstalled', { file: cs.installedFileName ?? '' })}>
        <i class="ti ti-circle-check"></i>
      </span>
    {/if}
    <button class="btn btn-primary btn-sm" onclick={() => pickById(id)} disabled={off(id)}>
      {#if cs?.alreadyInstalled}
        <i class="ti ti-refresh"></i> {t('browser.install.reinstall')}
      {:else}
        <i class="ti ti-download"></i> {t('browser.install')}
      {/if}
    </button>
  {/if}
{/snippet}

{#if servers.length === 0}
  <div class="stl-empty">{t('browser.target.noServers')}</div>
{:else if shown.length === 0}
  <div class="stl-empty">
    <i class="ti ti-server-off"></i>
    <div>{kind === 'plugins' ? t('browser.target.noPluginServers') : t('browser.target.noModServers')}</div>
  </div>
{:else}
  <PackPicker
    items={picker.items}
    rowById={picker.rowById}
    folderById={picker.folderById}
    selected={selectedId}
    onselect={pickById}
    selectable={!installButton}
    rowActions={installButton ? srvActions : null}
    class={listClass}
  />
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
  .stl-installed-badge { display: inline-flex; align-items: center; justify-content: center; color: var(--green); font-size: 15px; }
</style>
