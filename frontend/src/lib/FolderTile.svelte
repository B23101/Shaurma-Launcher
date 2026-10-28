<script lang="ts">
  // ── FolderTile: плитка папки в сітці «Мої збірки» і в бібліотеці серверів ──
  // Раніше ця розмітка була скопійована в App.svelte і ServerLibraryPage.svelte.
  // Тут лише вигляд (стек до трьох іконок, «+N», назва, підпис); поведінку —
  // кліки, drag&drop, контекстне меню, класи стану — передає батько через
  // звичайні атрибути (...rest) і проп class. Стилі — .folder-tile/.ft-* у style.css.
  import type { Snippet } from 'svelte'
  import AnimatedIcon from './AnimatedIcon.svelte'
  import { safeFolderIcon } from './folderMeta'

  export interface FolderTileIcon {
    src?: string
    paused?: boolean
    pcol?: string
    preset?: string
  }

  let {
    name, color = '', icon = '', icons = [], total = 0,
    animated = false, class: cls = '', style = '', sub, ...rest
  }: {
    name: string
    color?: string
    icon?: string
    icons?: FolderTileIcon[]
    total?: number
    // true — іконки збірок можуть бути анімованими (AnimatedIcon), інакше звичайний <img>.
    animated?: boolean
    class?: string
    style?: string
    sub?: Snippet
    [key: string]: unknown
  } = $props()

  const col = $derived(color || 'var(--orange)')
</script>

<div class="inst-tile folder-tile {cls}" style="--pcol:{col};{style}" title={name} {...rest}>
  <div class="ft-stack" aria-hidden="true">
    {#if total === 0}
      <span class="ft-stack-fallback ft-stack-empty" style="--pcol:{col}"><i class="ti {safeFolderIcon(icon)}"></i></span>
    {:else}
      {#each icons.slice(0, 3) as ic, i (i)}
        {#if ic.src}
          {#if animated}<AnimatedIcon src={ic.src} paused={ic.paused} alt="" />{:else}<img src={ic.src} alt="" />{/if}
        {:else}
          <span class="ft-stack-fallback" style="--pcol:{ic.pcol || col}"><i class="ti {ic.preset || 'ti-puzzle'}"></i></span>
        {/if}
      {/each}
      {#if total > 3}<span class="ft-more">+{total - 3}</span>{/if}
    {/if}
  </div>
  <div class="ft-info">
    <div class="ft-name">{name}</div>
    {#if sub}<div class="ft-sub">{@render sub()}</div>{/if}
  </div>
</div>
