<script lang="ts">
  // NumberInput.svelte — числове поле у стилі лаунчера. Замінює нативний
  // <input type="number">, у якого браузерні стрілки (spin-button) не
  // підлаштовуються під тему. Власні кнопки ▲/▼ усередині поля:
  // тиск-і-тримай повторює крок; min/max/step беруться з самого поля
  // (input.stepUp/stepDown), тож поведінка така ж, як у нативного.
  //
  // Події input/change диспатчаться на сам <input>, тому всі існуючі
  // обробники (bind:value, oninput, onchange) працюють без змін.
  // class — клас поля (st-input num mono / input mono …), style — на обгортку
  // (ширина). Решта атрибутів (min, max, step, aria-label, disabled…) — на поле.
  import type { HTMLInputAttributes } from 'svelte/elements'

  let {
    value = $bindable(),
    class: cls = '',
    style = '',
    ...rest
  }: Omit<HTMLInputAttributes, 'type' | 'value' | 'class' | 'style'> & {
    value?: number | string | null
    class?: string
    style?: string
  } = $props()

  let el = $state<HTMLInputElement | undefined>()
  let holdTimer: ReturnType<typeof setTimeout> | undefined
  let repeatTimer: ReturnType<typeof setInterval> | undefined

  function step(dir: 1 | -1) {
    if (!el || el.disabled || el.readOnly) return
    try {
      if (dir > 0) el.stepUp(); else el.stepDown()
    } catch {
      // step="any" або порожнє поле без min — крокуємо вручну
      const cur = Number(el.value) || 0
      el.value = String(cur + dir)
    }
    el.dispatchEvent(new Event('input', { bubbles: true }))
  }

  function stop(commit: boolean) {
    const was = holdTimer !== undefined || repeatTimer !== undefined
    clearTimeout(holdTimer); clearInterval(repeatTimer)
    holdTimer = repeatTimer = undefined
    // change — один раз після відпускання, а не на кожному кроці повтору
    if (commit && was && el) el.dispatchEvent(new Event('change', { bubbles: true }))
  }

  function down(e: PointerEvent, dir: 1 | -1) {
    if (e.button !== 0) return
    e.preventDefault() // не забирати фокус із поля
    ;(e.currentTarget as HTMLElement).setPointerCapture?.(e.pointerId)
    step(dir)
    holdTimer = setTimeout(() => { repeatTimer = setInterval(() => step(dir), 55) }, 380)
  }

  $effect(() => () => stop(false))
</script>

<span class="nf" class:disabled={rest.disabled} {style}>
  <input bind:this={el} bind:value type="number" class="nf-in {cls}" {...rest} />
  {#if !rest.disabled && !rest.readonly}
    <span class="nf-steps" aria-hidden="true">
      <button type="button" tabindex="-1" class="nf-up"
              onpointerdown={(e) => down(e, 1)} onpointerup={() => stop(true)} onpointercancel={() => stop(true)}>
        <i class="ti ti-chevron-up"></i>
      </button>
      <button type="button" tabindex="-1" class="nf-down"
              onpointerdown={(e) => down(e, -1)} onpointerup={() => stop(true)} onpointercancel={() => stop(true)}>
        <i class="ti ti-chevron-down"></i>
      </button>
    </span>
  {/if}
</span>

<style>
  .nf { position: relative; display: inline-flex; align-items: stretch; width: 100%; min-width: 0; flex: none; }
  /* специфічність вища за .st-input.num / .input — ширину задає обгортка */
  .nf > input.nf-in { width: 100%; padding-right: 28px; min-width: 0; -moz-appearance: textfield; appearance: textfield; }
  .nf > input.nf-in::-webkit-inner-spin-button,
  .nf > input.nf-in::-webkit-outer-spin-button { -webkit-appearance: none; appearance: none; margin: 0; display: none; }

  .nf-steps {
    position: absolute; top: 3px; bottom: 3px; right: 3px; width: 20px;
    display: flex; flex-direction: column; gap: 1px;
    opacity: .55; transition: opacity .15s;
  }
  .nf:hover .nf-steps, .nf:focus-within .nf-steps { opacity: 1; }
  .nf-steps button {
    flex: 1; min-height: 0; padding: 0; border: 0; background: transparent; cursor: pointer;
    color: var(--text-mute); font-size: 11px; line-height: 1;
    display: flex; align-items: center; justify-content: center;
    transition: background .12s, color .12s, transform .08s;
  }
  .nf-up { border-radius: 6px 6px 3px 3px; }
  .nf-down { border-radius: 3px 3px 6px 6px; }
  .nf-steps button:hover { background: var(--hover-strong); color: var(--orange); }
  .nf-steps button:active { background: rgba(var(--orange-rgb), .2); color: var(--orange); transform: scale(.92); }
</style>
