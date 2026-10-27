<script lang="ts">
  // ── FolderViewModal — вікно відкритої папки (збірки / сервери) ───────────
  // Спільний «каркас» на ГЛОБАЛЬНИХ стилях лаунчера (style.css): .modal,
  // .modal-head, .modal-body, .btn.btn-ghost, .md-close, .fm-* — власного
  // оформлення тут немає, тож вікно виглядає як усі інші модалки.
  // Вміст (картки збірок чи серверів) малює батько — тут лише оболонка.
  // Адаптивність: шапка переноситься (flex-wrap), на вузькому вікні кнопки
  // дій стають іконками (container query у style.css) — текст не обрізається.
  //
  // УВАГА: кореневий елемент має клас `folder-modal` — за ним App.svelte
  // (pointerDragEnd) розпізнає, що збірку відпустили ВСЕРЕДИНІ вікна, а за
  // його межами — «витягнути з папки».
  import { onMount } from 'svelte'
  import type { Snippet } from 'svelte'
  import Portal from './Portal.svelte'
  import { t } from './i18n.svelte'
  import { safeFolderIcon } from './folderMeta'

  export interface FolderAction {
    icon: string
    label: string
    onclick: () => void
    danger?: boolean
  }

  let {
    name = '',
    color = '#ff8a00',
    icon = '',
    count = 0,
    countLabel = '',
    managed = false,
    managedHint = '',
    footHint = '',
    emptyText = '',
    actions = [],
    onclose = () => {},
    children,
  }: {
    name?: string
    color?: string
    icon?: string
    count?: number
    countLabel?: string
    managed?: boolean
    managedHint?: string
    footHint?: string
    emptyText?: string
    actions?: FolderAction[]
    onclose?: () => void
    children?: Snippet
  } = $props()

  const ic = $derived(safeFolderIcon(icon))

  onMount(() => {
    const onKey = (e: KeyboardEvent) => { if (e.key === 'Escape') { e.stopPropagation(); onclose() } }
    window.addEventListener('keydown', onKey, true)
    return () => window.removeEventListener('keydown', onKey, true)
  })
</script>

<Portal>
  <div class="modal-overlay" onclick={(e) => { if (e.target === e.currentTarget) onclose() }} role="presentation">
    <div class="modal folder-modal" style="--fc:{color}" role="dialog" aria-modal="true" aria-label={name}>
      <div class="modal-head fm-head">
        <div class="fm-icon fm-icon-lg" style="color:var(--fc);background:color-mix(in srgb, var(--fc) 14%, var(--bg))"><i class="ti {ic}"></i></div>
        <div class="fm-titlebox">
          <h3 title={name}>{name}</h3>
          <div class="fm-meta">
            <span class="fm-count"><b>{count}</b> {countLabel}</span>
            {#if managed}<span class="fm-count"><i class="ti ti-lock"></i> {t('folder.managed')}</span>{/if}
          </div>
        </div>
        {#if actions.length}
          <div class="fm-actions">
            {#each actions as a (a.label)}
              <button class="btn btn-ghost fm-btn" class:danger={a.danger} onclick={a.onclick} title={a.label} aria-label={a.label}>
                <i class="ti {a.icon}"></i><span>{a.label}</span>
              </button>
            {/each}
          </div>
        {/if}
        <button class="md-close fm-close" onclick={onclose} title={t('close')} aria-label={t('close')}><i class="ti ti-x"></i></button>
      </div>

      {#if managed && managedHint}
        <div class="fm-hint fm-hint-top"><i class="ti ti-info-circle"></i> {managedHint}</div>
      {/if}

      <div class="modal-body">
        {#if count === 0}
          <div class="fm-empty">
            <i class="ti ti-folder-off"></i>
            <div>{emptyText}</div>
          </div>
        {:else}
          {@render children?.()}
        {/if}
      </div>

      {#if footHint && count > 0}
        <div class="fm-hint"><i class="ti ti-hand-grab"></i> {footHint}</div>
      {/if}
    </div>
  </div>
</Portal>
