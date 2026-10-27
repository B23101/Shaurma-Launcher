package custompack

import (
	"sync"
	"time"
)

const cfSearchCacheTTL = 5 * time.Minute
const cfSearchCacheMax = 200

type cfSearchCacheItem struct {
	data []byte
	at   time.Time
}

type cfSearchInflight struct {
	wg   sync.WaitGroup
	data []byte
	err  error
}

var (
	cfSearchCacheMu sync.Mutex
	cfSearchCache   = map[string]cfSearchCacheItem{}
	cfSearchFlight  = map[string]*cfSearchInflight{}
)

func cfSearchCacheGet(key string, fetch func() ([]byte, error)) ([]byte, error) {
	cfSearchCacheMu.Lock()
	if it, ok := cfSearchCache[key]; ok && time.Since(it.at) < cfSearchCacheTTL {
		cfSearchCacheMu.Unlock()
		return it.data, nil
	}
	if fl, ok := cfSearchFlight[key]; ok {
		cfSearchCacheMu.Unlock()
		fl.wg.Wait()
		return fl.data, fl.err
	}
	fl := &cfSearchInflight{}
	fl.wg.Add(1)
	cfSearchFlight[key] = fl
	cfSearchCacheMu.Unlock()

	fl.data, fl.err = fetch()

	cfSearchCacheMu.Lock()
	delete(cfSearchFlight, key)
	if fl.err == nil {
		if len(cfSearchCache) >= cfSearchCacheMax {
			var oldestKey string
			var oldest time.Time
			for k, v := range cfSearchCache {
				if time.Since(v.at) >= cfSearchCacheTTL {
					delete(cfSearchCache, k)
					continue
				}
				if oldestKey == "" || v.at.Before(oldest) {
					oldestKey, oldest = k, v.at
				}
			}
			if len(cfSearchCache) >= cfSearchCacheMax && oldestKey != "" {
				delete(cfSearchCache, oldestKey)
			}
		}
		cfSearchCache[key] = cfSearchCacheItem{data: fl.data, at: time.Now()}
	}
	cfSearchCacheMu.Unlock()
	fl.wg.Done()
	return fl.data, fl.err
}
