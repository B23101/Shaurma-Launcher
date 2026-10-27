package share

import (
	"encoding/json"
	"os"
	"sort"
	"sync"
	"time"
)

// History — невеликий журнал підсумків передач на диску (вхідних і вихідних), щоб після
// перезапуску лаунчера картка в чаті показувала, чим усе скінчилось («друг відмовився»,
// «скасовано відправником»…), а незавершене можна було позначити як перерване.
// Значення — довільний JSON (вигляд для UI визначає лаунчер); ключ — kind + id.
type History struct {
	path string
	max  int

	mu      sync.Mutex
	loaded  bool
	entries map[string]historyEntry
}

type historyEntry struct {
	Kind string          `json:"k"`
	ID   string          `json:"id"`
	At   int64           `json:"at"`
	Data json.RawMessage `json:"d"`
}

// NewHistory — журнал у файлі path; зберігається не більше max записів кожного виду.
func NewHistory(path string, max int) *History {
	if max <= 0 {
		max = 100
	}
	return &History{path: path, max: max}
}

func (h *History) loadLocked() {
	if h.loaded {
		return
	}
	h.loaded = true
	h.entries = map[string]historyEntry{}
	raw, err := os.ReadFile(h.path)
	if err != nil {
		return
	}
	var list []historyEntry
	if json.Unmarshal(raw, &list) != nil {
		return
	}
	for _, e := range list {
		if e.Kind != "" && e.ID != "" && len(e.Data) > 0 {
			h.entries[e.Kind+":"+e.ID] = e
		}
	}
}

// Put зберігає (замінює) запис і одразу пише файл.
func (h *History) Put(kind, id string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.loadLocked()
	h.entries[kind+":"+id] = historyEntry{Kind: kind, ID: id, At: time.Now().UnixMilli(), Data: raw}
	h.pruneLocked(kind)
	return h.saveLocked()
}

// Get читає запис у v; false — немає.
func (h *History) Get(kind, id string, v any) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.loadLocked()
	e, ok := h.entries[kind+":"+id]
	return ok && json.Unmarshal(e.Data, v) == nil
}

// List — усі записи виду kind, від найновішого.
func (h *History) List(kind string) []json.RawMessage {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.loadLocked()
	var list []historyEntry
	for _, e := range h.entries {
		if e.Kind == kind {
			list = append(list, e)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].At > list[j].At })
	out := make([]json.RawMessage, len(list))
	for i, e := range list {
		out[i] = e.Data
	}
	return out
}

func (h *History) pruneLocked(kind string) {
	var list []historyEntry
	for _, e := range h.entries {
		if e.Kind == kind {
			list = append(list, e)
		}
	}
	if len(list) <= h.max {
		return
	}
	sort.Slice(list, func(i, j int) bool { return list[i].At > list[j].At })
	for _, e := range list[h.max:] {
		delete(h.entries, e.Kind+":"+e.ID)
	}
}

func (h *History) saveLocked() error {
	list := make([]historyEntry, 0, len(h.entries))
	for _, e := range h.entries {
		list = append(list, e)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].At > list[j].At })
	return writeJSONAtomic(h.path, list)
}
