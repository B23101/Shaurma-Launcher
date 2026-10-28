<script lang="ts">
  // ── Пігулка статусу з "живою" крапкою ────────────────────────────────
  // Той самий візуал, що на сторінці збірки (BuildsPage) — .status-pill з
  // кольоровою крапкою й підписом. Використовується і в бібліотеці
  // серверів (як маленький бейдж), і на банері сторінки сервера.
  import { statusColor, statusLabel } from '../utils'
  let { status = 'stopped', size = 'md' as 'sm' | 'md' }: { status?: string; size?: 'sm' | 'md' } = $props()
  const color = $derived(statusColor(status))
  const label = $derived(statusLabel(status))
  const busy = $derived(status === 'starting' || status === 'stopping' || status === 'installing')
</script>

<span class="ssb ssb-{size}" style="color:{color};border-color:currentColor">
  <span class="ssb-dot" class:pulse={busy}></span>
  {label}
</span>

<style>
  .ssb {
    display: inline-flex; align-items: center; gap: 6px;
    padding: 4px 10px; border-radius: 999px;
    background: rgba(0, 0, 0, .35);
    border: 1px solid currentColor;
    font: 700 11px/1 var(--font-ui);
    backdrop-filter: blur(4px);
    white-space: nowrap;
  }
  .ssb-sm { padding: 2px 7px; font-size: var(--fs-min); gap: 4px; }
  .ssb-dot { width: 7px; height: 7px; border-radius: 50%; background: currentColor; flex-shrink: 0; }
  .ssb-sm .ssb-dot { width: 6px; height: 6px; }
  .ssb-dot.pulse { animation: ssb-pulse 1.3s ease-in-out infinite; }
  @keyframes ssb-pulse { 0%, 100% { opacity: 1; } 50% { opacity: .35; } }
  @media (prefers-reduced-motion: reduce) { .ssb-dot.pulse { animation: none; } }
</style>
