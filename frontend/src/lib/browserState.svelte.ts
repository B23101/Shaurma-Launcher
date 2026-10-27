// ── Сесійний стан Браузера контенту ─────────────────────────────────────
// ContentBrowser розмонтовується при виході зі сторінки пошуку (перехід на
// іншу сторінку, відкритий проєкт/автор) — замість старого hidden-холдера,
// який тримав компонент (і його DOM/підписки) змонтованим назавжди. Щоб
// «попередній видимий екран» виглядав ІДЕНТИЧНО при поверненні, пошуковий
// стан (таб, запит, фільтри, сорт, результати, сторінка пагінації,
// прокрутка) знімається сюди в onDestroy і відновлюється при наступному
// монтуванні.
//
// Сторінг сесійний (тільки в пам'яті, не localStorage): перезапуск лаунчера
// = свіжий пошук, як і раніше. Персистентність «останнього вікна» (сторінка,
// вкладка, відкрита збірка) живе окремо в uiState.ts.
import type { BrowserSearchResult, BrowserCategory, BrowserInstalledItem } from './types'

export const browserUI = $state({
  activeType: null as string | null,
  query: '',
  sort: 'featured',
  sourceFilter: 'all', // all | modrinth | curseforge
  selectedGameVersion: '',
  selectedLoader: '',
  packFilterId: '',
  selectedCategories: [] as string[],
  categories: [] as BrowserCategory[],
  mcVersions: [] as string[],
  versionsLoaded: false,
  results: null as BrowserSearchResult | null,
  offset: 0,
  currentPage: 1,
  searched: false,
  installedMap: {} as Record<string, BrowserInstalledItem>,
  // Прокрутка списку результатів (.cb-main) — теж переживає розмонтування.
  scroll: 0,
})
