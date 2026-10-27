<script lang="ts">
  // ── Portal ───────────────────────────────────────────────────────────
  // Телепортує свій вміст напряму в document.body (в кінець <body>).
  //
  // Навіщо: .modal-overlay має position:fixed; inset:0, розрахований на
  // весь viewport вікна. Але якщо будь-який предок по дереву DOM має
  // transform / filter / backdrop-filter / perspective / contain:layout|paint
  // (наприклад .mp-topbar з backdrop-filter в ModpackPage, .mod-ic-wrap з
  // transform у PackModsSection тощо) — цей предок за специфікацією CSS
  // стає НОВИМ containing block для нащадків із position:fixed. Тоді
  // inset:0 рахується вже не від вікна застосунку, а від рамки того
  // предка — і модалка «стискається» в його контейнер замість того, щоб
  // покрити весь екран, як усі інші модалки, змонтовані прямо з App.svelte
  // (де по дереву до кореня нема жодного transform/filter).
  //
  // Portal вирішує це раз і назавжди: вміст рендериться в реальному DOM
  // усередині <body>, поза будь-якими такими предками — і .modal-overlay
  // завжди позиціонується відносно вікна, незалежно від того, звідки він
  // був викликаний (App.svelte, PackServersSection, PackModsSection,
  // ModpackPage, Wardrobe і т.д.).
  //
  // Використання:
  //   <Portal>
  //     <div class="modal-overlay" ...>...</div>
  //   </Portal>

  let { children, target = 'body' } = $props<{
    children: import('svelte').Snippet
    target?: string | HTMLElement
  }>()

  let host: HTMLElement | null = null

  function resolveTarget(): HTMLElement {
    if (typeof target !== 'string') return target
    return (document.querySelector(target) as HTMLElement) ?? document.body
  }

  function mount(node: HTMLElement) {
    host = node
    resolveTarget().appendChild(node)
    return {
      destroy() {
        node.remove()
      }
    }
  }
</script>

<div use:mount style="display:contents">
  {@render children()}
</div>
