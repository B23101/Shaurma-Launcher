<script lang="ts">
  // ── FolderViewModal — вікно відкритої папки (збірки / сервери) ───────────
  // Оболонка — спільний UiModal (шапка з іконкою папки, Esc, фокус), кнопки
  // дій — .btn.btn-ghost у шапці, решта — глобальні .fm-* зі style.css.
  // Вміст (картки збірок чи серверів) малює батько — тут лише оболонка.
  // Адаптивність: шапка переноситься (flex-wrap), на вузькому вікні кнопки
  // дій стають іконками (container query у style.css) — текст не обрізається.
  //
  // УВАГА: кореневий елемент має клас `folder-modal` — за ним App.svelte
  // (pointerDragEnd) розпізнає, що збірку відпустили ВСЕРЕДИНІ вікна, а за
  // його межами — «витягнути з папки».
  import type { Snippet } from 'svelte'
  import UiModal from './UiModal.svelte'
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
</script>

<UiModal open={true} {onclose} icon={ic} {color} title={name}
         subtitle={managed ? `${count} ${countLabel} · ${t('folder.managed')}` : `${count} ${countLabel}`}
         width="780px" maxHeight="min(84vh, 800px)" modalClass="folder-modal"
         actions={actions.length ? acts : null} footer={footHint && count > 0 ? foot : null}>
  {#if managed && managedHint}
    <div class="fm-hint fm-hint-top"><i class="ti ti-info-circle"></i> {managedHint}</div>
  {/if}
  {#if count === 0}
    <div class="fm-empty">
      <i class="ti ti-folder-off"></i>
      <div>{emptyText}</div>
    </div>
  {:else}
    {@render children?.()}
  {/if}
</UiModal>

{#snippet acts()}
  {#each actions as a (a.label)}
    <button class="btn btn-ghost fm-btn" class:danger={a.danger} onclick={a.onclick} title={a.label} aria-label={a.label}>
      <i class="ti {a.icon}"></i><span>{a.label}</span>
    </button>
  {/each}
{/snippet}

{#snippet foot()}
  <div class="fm-hint"><i class="ti ti-hand-grab"></i> {footHint}</div>
{/snippet}
