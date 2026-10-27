<script lang="ts">
  // ── CompareChip — спільна картка СТОРОНИ порівняння ────────────────────
  //
  // Використовується ОБОМА порівнювачами лаунчера: PackCompareDialog
  // (збірки, через обгортку PackCompareChip) і ServerCompareDialog
  // (сервери). Раніше кожен малював свою шапку власними класами/версткою —
  // через це порівняння серверів виглядало «чужим» поруч із порівнянням
  // збірок, а будь-яка зміна дизайну в одному місці не доходила до іншого.
  // Тепер вигляд шапки визначається ОДНИМ компонентом.
  //
  // Джерело картинки не задається тут — його обчислює викликач (збірки:
  // packAsset(iconUrl) → пресет; сервери: serverVisual()) і передає готовим
  // через src/ti/color. Так компонент не знає нічого про типи сутностей.
  let {
    // Підпис сторони («Збірка / Порівняти з», «Цей сервер / Порівняти з»).
    label = '',
    name = '',
    meta = '',
    src = undefined as string | undefined,
    ti = '',
    color = '',
    // Клікабельність: у порівнянні збірок обидві сторони рівноправні.
    clickable = true,
    // Порожня сторона (ще не обрана) — пунктирна рамка й плейсхолдер.
    empty = false,
    onclick = () => {},
  }: {
    label?: string
    name?: string
    meta?: string
    src?: string
    ti?: string
    color?: string
    clickable?: boolean
    empty?: boolean
    onclick?: () => void
  } = $props()
</script>

{#snippet body()}
  <span class="cc-ic" class:has-icon={!!src}>
    {#if src}<img src={src} alt="" loading="lazy" />
    {:else if name}<i class="ti {ti || 'ti-puzzle'}"></i>
    {:else}<i class="ti ti-square-plus"></i>{/if}
  </span>
  <div class="cc-info">
    {#if name}
      <div class="cc-name">{name}</div>
      {#if meta}<div class="cc-meta">{meta}</div>{/if}
    {:else}
      <div class="cc-name cc-placeholder">…</div>
    {/if}
  </div>
  {#if clickable}<i class="ti ti-chevron-down cc-chev"></i>{/if}
{/snippet}

<div class="cc-chip" class:clickable class:empty>
  {#if label}<div class="cc-label">{label}</div>{/if}
  {#if clickable}
    <button type="button" class="cc-body" style={color ? `--pcol:${color}` : ''} {onclick}>
      {@render body()}
    </button>
  {:else}
    <div class="cc-body" style={color ? `--pcol:${color}` : ''}>
      {@render body()}
    </div>
  {/if}
</div>

<style>
  .cc-chip { display: flex; flex-direction: column; gap: 5px; flex: 1; min-width: 0; }
  .cc-label { font-size: 11px; font-weight: 700; color: var(--text-mute); text-transform: uppercase; letter-spacing: .4px; }

  .cc-body {
    display: flex; align-items: center; gap: 10px; width: 100%;
    padding: 8px 10px; border-radius: var(--r-sm);
    background: var(--bg-input); border: 1px solid var(--border);
    font: inherit; text-align: left; transition: .15s;
  }
  .cc-chip.clickable .cc-body { cursor: pointer; }
  .cc-chip.clickable .cc-body:hover { border-color: var(--pcol, var(--orange)); background: var(--bg-input-hi); }
  .cc-chip.empty .cc-body { border-style: dashed; }

  .cc-ic {
    width: 32px; height: 32px; min-width: 32px; border-radius: 8px; overflow: hidden;
    display: flex; align-items: center; justify-content: center;
    background: color-mix(in srgb, var(--pcol, var(--orange)) 18%, var(--bg-input-hi));
    color: var(--pcol, var(--orange)); font-size: 14px; flex-shrink: 0;
  }
  .cc-ic.has-icon { background: transparent; }
  .cc-ic img { width: 100%; height: 100%; object-fit: cover; border-radius: 8px; }

  .cc-info { flex: 1; min-width: 0; }
  .cc-name { font-size: 12.5px; font-weight: 700; color: var(--text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .cc-placeholder { color: var(--text-dim); font-weight: 500; }
  .cc-meta { font: 500 10.5px/1.2 var(--font-mono); color: var(--text-mute); margin-top: 2px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

  .cc-chev { color: var(--text-dim); font-size: 14px; flex-shrink: 0; }
</style>
