// ── Асети збірок (іконки/фони) через бекенд ─────────────────────────────
// Worker віддає картинки збірок лише з токеном у заголовках, які браузер
// не може додати до <img>/background-image, тому асети качає бекенд
// (App.GetPackAsset) і повертає data URL. Цей модуль кешує результат на
// рівні модуля, щоб усі компоненти (sidebar, тайли, сторінка збірки)
// ділили один кеш і не качали той самий файл повторно.
//
// Значення '' у кеші = асет не завантажився (помилка/404) — більше не
// пробуємо; компонент показує fallback-іконку замість порожнього квадрата.
import { App } from './wails'

export const packAssets = $state<Record<string, string>>({})

const inflight = new Set<string>()

/**
 * Повертає data URL асета, якщо він уже завантажений; інакше ініціює
 * завантаження через бекенд і повертає undefined (компонент показує
 * fallback). Коли кеш оновиться, Svelte 5 сам перерендерить компоненти,
 * що читали packAssets[url] під час рендеру.
 */
export function packAsset(url: string | undefined | null): string | undefined {
  if (!url) return undefined
  const hit = packAssets[url]
  if (hit !== undefined) return hit || undefined
  if (!inflight.has(url)) {
    inflight.add(url)
    App.GetPackAsset(url)
      .then((d) => { packAssets[url] = d ?? '' })
      .catch(() => { packAssets[url] = '' })
  }
  return undefined
}
