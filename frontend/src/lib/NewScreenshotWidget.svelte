<script lang="ts">
  // ── Плаваючий віджет «новий скріншот» ─────────────────────────────────
  // Живе поверх усього лаунчера (той самий шар, що Toasts.svelte —
  // фіксоване позиціонування знизу справа), самостійно підписується на
  // подію бекенду 'gallery:new-screenshot' (screenshots.go — фоновий
  // watcher, поки збірка запущена). Показує міні-прев'ю щойно зробленого
  // F2-скріншоту + дату + швидкі кнопки: редагувати (відкриває
  // фоторедактор одразу) і відкрити галерею збірки.
  //
  // Кілька карток можуть накопичуватись (гравець клацнув кілька разів
  // підряд) — стек знизу вгору, як toast'и; кожна сама зникає за
  // widgetAutoHideMs, якщо гравець її не торкнувся.
  import { onDestroy } from 'svelte'
  import { t } from './i18n.svelte'
  import { App, on } from './wails'
  import type { NewScreenshotEvent } from './types'
  import ScreenshotEditor from './ScreenshotEditor.svelte'

  let { onOpenGallery } = $props<{ onOpenGallery: (packId: string) => void }>()

  type Card = NewScreenshotEvent & { id: number; thumb: string; timer: ReturnType<typeof setTimeout> }

  let cards = $state<Card[]>([])
  let nextId = 1
  const widgetAutoHideMs = 8000

  function dismiss(id: number) {
    const c = cards.find(c => c.id === id)
    if (c) clearTimeout(c.timer)
    cards = cards.filter(c => c.id !== id)
  }

  async function addCard(ev: NewScreenshotEvent) {
    let thumb = ''
    try {
      thumb = await App.GetScreenshotThumbnail(ev.packId, ev.entry.fileName)
    } catch {
      // не критично — картка покаже плейсхолдер-іконку
    }
    const id = nextId++
    const timer = setTimeout(() => dismiss(id), widgetAutoHideMs)
    cards = [...cards, { ...ev, id, thumb, timer }]
  }

  const unsub = on('gallery:new-screenshot', (data: NewScreenshotEvent) => {
    addCard(data)
  })

  onDestroy(() => {
    unsub()
    for (const c of cards) clearTimeout(c.timer)
  })

  function fmtTime(rfc: string): string {
    const d = new Date(rfc)
    if (isNaN(d.getTime())) return ''
    return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
  }

  // ── Швидке редагування прямо з віджету ──
  let editingCard = $state<Card | null>(null)

  function openEdit(c: Card) {
    editingCard = c
    dismiss(c.id)
  }

  function closeEdit() {
    editingCard = null
  }

  function openGallery(c: Card) {
    onOpenGallery(c.packId)
    dismiss(c.id)
  }
</script>

{#if cards.length > 0}
  <div class="scr-widget-stack">
    {#each cards as c (c.id)}
      <div class="scr-widget">
        <button class="scr-widget-close" onclick={() => dismiss(c.id)} title={t('widget.dismiss')}><i class="ti ti-x"></i></button>
        <div class="scr-widget-thumb" onclick={() => openEdit(c)}>
          {#if c.thumb}
            <img src={c.thumb} alt={c.entry.fileName} />
          {:else}
            <i class="ti ti-photo"></i>
          {/if}
        </div>
        <div class="scr-widget-info">
          <div class="scr-widget-title"><i class="ti ti-camera"></i> {t('widget.newScreenshot')}</div>
          <div class="scr-widget-pack" title={c.packName}>{c.packName}</div>
          <div class="scr-widget-time">{fmtTime(c.entry.modified)}</div>
          <div class="scr-widget-actions">
            <button class="scr-widget-btn" onclick={() => openEdit(c)}><i class="ti ti-edit"></i> {t('widget.edit')}</button>
            <button class="scr-widget-btn" onclick={() => openGallery(c)}><i class="ti ti-photo"></i> {t('widget.openGallery')}</button>
          </div>
        </div>
      </div>
    {/each}
  </div>
{/if}

{#if editingCard}
  <ScreenshotEditor
    packId={editingCard.packId}
    entry={editingCard.entry}
    onClose={closeEdit}
    onSaved={closeEdit}
  />
{/if}

<style>
  .scr-widget-stack {
    position: fixed;
    bottom: 18px;
    right: 18px;
    z-index: 999998; /* трохи нижче Toasts (999999), щоб тост не ховався за віджетом */
    display: flex;
    flex-direction: column-reverse;
    gap: 10px;
    max-width: 340px;
  }
  .scr-widget {
    display: flex;
    gap: 10px;
    padding: 10px;
    border-radius: 14px;
    background: var(--bg-panel);
    border: 1px solid var(--border-hi);
    box-shadow: 0 18px 40px rgba(0, 0, 0, .6);
    backdrop-filter: blur(10px);
    position: relative;
    animation: scrWidgetIn .25s ease;
  }
  .scr-widget-close {
    position: absolute; top: 4px; right: 4px; width: 20px; height: 20px; border-radius: 6px;
    border: none; background: rgba(0,0,0,.35); color: #fff; opacity: .7; cursor: pointer;
    display: flex; align-items: center; justify-content: center; font-size: 11px; z-index: 1;
  }
  .scr-widget-close:hover { opacity: 1; background: rgba(0,0,0,.6); }
  .scr-widget-thumb {
    width: 76px; height: 76px; border-radius: 10px; overflow: hidden; flex-shrink: 0;
    background: #000; display: flex; align-items: center; justify-content: center;
    color: var(--text-dim); font-size: 20px; cursor: pointer;
  }
  .scr-widget-thumb img { width: 100%; height: 100%; object-fit: cover; display: block; }
  .scr-widget-info { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
  .scr-widget-title { display: flex; align-items: center; gap: 6px; font-size: 12px; font-weight: 700; color: var(--text); }
  .scr-widget-title i { color: var(--orange); font-size: 13px; }
  .scr-widget-pack { font-size: 11px; color: var(--text-mute); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .scr-widget-time { font-size: var(--fs-min); color: var(--text-dim); font-family: var(--font-mono); margin-bottom: 4px; }
  .scr-widget-actions { display: flex; gap: 6px; margin-top: auto; }
  .scr-widget-btn {
    flex: 1; border: none; border-radius: 8px; background: var(--hover); color: var(--text);
    font-size: 11px; font-weight: 600; padding: 6px 8px; cursor: pointer; display: flex; align-items: center;
    justify-content: center; gap: 5px; transition: background .15s;
  }
  .scr-widget-btn:hover { background: var(--hover-strong); }
  .scr-widget-btn i { font-size: 12px; }

  @keyframes scrWidgetIn {
    from { opacity: 0; transform: translateY(12px) scale(.96); }
    to { opacity: 1; transform: translateY(0) scale(1); }
  }
</style>
