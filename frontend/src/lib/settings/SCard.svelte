<script lang="ts">
  // SCard.svelte — картка налаштувань: іконка + назва (+ короткий опис) і тіло.
  // id — якір для пошуку (data-setting-key), коли картка сама є «параметром»
  // (тема, акаунти, теки…).
  import type { Snippet } from 'svelte'
  let {
    title, icon = 'ti-settings', tone = '', desc = '', id = undefined, actions = undefined, flush = false, children,
  }: {
    title: string
    icon?: string
    tone?: '' | 'orange' | 'violet' | 'green' | 'blue' | 'yellow' | 'red' | 'gray'
    desc?: string
    id?: string
    actions?: Snippet
    flush?: boolean
    children?: Snippet
  } = $props()
</script>

<section class="st-card" data-setting-key={id}>
  <header class="st-card-head">
    <span class="st-card-ic {tone}"><i class="ti {icon}"></i></span>
    <div class="st-card-tx">
      <h3>{title}</h3>
      {#if desc}<p>{desc}</p>{/if}
    </div>
    {#if actions}<div class="st-card-act">{@render actions()}</div>{/if}
  </header>
  {#if children}<div class="st-card-body" class:flush>{@render children()}</div>{/if}
</section>
