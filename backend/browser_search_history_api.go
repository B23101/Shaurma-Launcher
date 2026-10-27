package main

import "time"

// ── Історія пошукових запитів браузера контенту ─────────────────────────
// Останні запити, які ДАЛИ РЕЗУЛЬТАТ (див. store.SearchHistoryEntry) —
// гравець бачить, що він шукав раніше і клацає, щоб повторити пошук
// замість передруковування. Дані живуть у .search-history.json
// (internal/store), той самий принцип, що й закладки: окремо від
// shaurma.json, не прив'язано до конкретної збірки.

// SearchHistoryItem — один запис історії для фронтенду.
type SearchHistoryItem struct {
	Query       string `json:"query"`
	ProjectType string `json:"projectType"`
	ResultCount int    `json:"resultCount"`
	SearchedAt  string `json:"searchedAt"` // RFC3339
}

// GetSearchHistory повертає історію пошуку, найновіші записи першими.
func (a *App) GetSearchHistory() []SearchHistoryItem {
	st := a.bookmarksStore()
	if st == nil {
		return []SearchHistoryItem{}
	}
	entries := st.GetSearchHistory()
	out := make([]SearchHistoryItem, 0, len(entries))
	for _, e := range entries {
		out = append(out, SearchHistoryItem{
			Query:       e.Query,
			ProjectType: e.ProjectType,
			ResultCount: e.ResultCount,
			SearchedAt:  e.SearchedAt.Format(time.RFC3339),
		})
	}
	return out
}

// AddSearchHistory записує пошуковий запит, що дав результат. Викликається
// з фронтенду (ContentBrowser.svelte) ЛИШЕ коли пошук реально завершився
// з непорожньою видачею — не на кожен символ під час набору і не для
// запитів без результату (див. store.AddSearchHistory: порожній query чи
// resultCount<=0 ігноруються і на боці Store теж, це друга лінія захисту,
// не єдина — про всяк випадок, якщо фронтенд колись викличе інакше).
func (a *App) AddSearchHistory(query, projectType string, resultCount int) error {
	st := a.bookmarksStore()
	if st == nil {
		return nil
	}
	return st.AddSearchHistory(query, projectType, resultCount)
}

// RemoveSearchHistoryEntry прибирає один запис («х» на рядку історії).
func (a *App) RemoveSearchHistoryEntry(query, projectType string) error {
	st := a.bookmarksStore()
	if st == nil {
		return nil
	}
	return st.RemoveSearchHistoryEntry(query, projectType)
}

// ClearSearchHistory очищає всю історію пошуку.
func (a *App) ClearSearchHistory() error {
	st := a.bookmarksStore()
	if st == nil {
		return nil
	}
	return st.ClearSearchHistory()
}
