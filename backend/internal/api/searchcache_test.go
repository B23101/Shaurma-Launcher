package api

import (
	"testing"
	"time"
)

func TestSearchCacheByteBudget(t *testing.T) {
	searchCacheMu.Lock()
	searchCache = map[string]searchCacheItem{}
	searchCacheMu.Unlock()

	chunk := 1 << 20 // 1 МБ
	for i := 0; i < 20; i++ {
		key := string(rune('a' + i))
		if _, err := searchCacheGet(key, func() ([]byte, error) { return make([]byte, chunk), nil }); err != nil {
			t.Fatal(err)
		}
	}
	searchCacheMu.Lock()
	defer searchCacheMu.Unlock()
	total := 0
	for _, v := range searchCache {
		total += len(v.data)
	}
	if total > searchCacheMaxBytes {
		t.Fatalf("кеш перевищив бюджет: %d > %d", total, searchCacheMaxBytes)
	}
}

func TestSearchCacheDropsExpired(t *testing.T) {
	searchCacheMu.Lock()
	searchCache = map[string]searchCacheItem{"old": {data: []byte("x"), at: time.Now().Add(-2 * searchCacheTTL)}}
	searchCacheMu.Unlock()
	if _, err := searchCacheGet("new", func() ([]byte, error) { return []byte("y"), nil }); err != nil {
		t.Fatal(err)
	}
	searchCacheMu.Lock()
	defer searchCacheMu.Unlock()
	if _, ok := searchCache["old"]; ok {
		t.Fatal("прострочений запис мав бути вилучений")
	}
}
