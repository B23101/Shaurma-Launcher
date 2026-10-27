package api

import (
	"sync"
	"time"
)

// Кеш відповідей пошуку Modrinth з дедуплікацією одночасних запитів.
// TTL короткий (5 хв): достатньо, щоб повернення на вкладку / повторний
// пошук були миттєвими, і досить мало, щоб не показувати застарілі дані.
const searchCacheTTL = 5 * time.Minute
const searchCacheMax = 200

// searchCacheMaxBytes — бюджет пам'яті кешу за ВАГОЮ відповідей. Ліміт у 200
// записів сам нічого не гарантує: одна сторінка видачі — це сотні КБ JSON,
// і 200 таких лежали в Go-купі до 5 хв кожна (а прострочені — ще довше, бо
// вилучались лише при переповненні). Тепер і прострочені, і найстаріші
// записи викидаються, щоб сумарна вага не перевищувала бюджет.
const searchCacheMaxBytes = 6 << 20

type searchCacheItem struct {
	data []byte
	at   time.Time
}

type searchInflight struct {
	wg   sync.WaitGroup
	data []byte
	err  error
}

var (
	searchCacheMu sync.Mutex
	searchCache   = map[string]searchCacheItem{}
	searchFlight  = map[string]*searchInflight{}
)

// searchCacheGet повертає кешовану відповідь або викликає fetch. Якщо
// такий самий запит уже летить — чекає на нього, а не шле дублікат
// (наприклад, prefetch і реальний пошук одночасно).
func searchCacheGet(key string, fetch func() ([]byte, error)) ([]byte, error) {
	searchCacheMu.Lock()
	if it, ok := searchCache[key]; ok && time.Since(it.at) < searchCacheTTL {
		searchCacheMu.Unlock()
		return it.data, nil
	}
	if fl, ok := searchFlight[key]; ok {
		searchCacheMu.Unlock()
		fl.wg.Wait()
		return fl.data, fl.err
	}
	fl := &searchInflight{}
	fl.wg.Add(1)
	searchFlight[key] = fl
	searchCacheMu.Unlock()

	fl.data, fl.err = fetch()

	searchCacheMu.Lock()
	delete(searchFlight, key)
	if fl.err == nil {
		pruneSearchCacheLocked(len(fl.data))
		if len(searchCache) >= searchCacheMax {
			// Простий евікшн: викидаємо прострочені, а якщо все свіже — найстаріший.
			var oldestKey string
			var oldest time.Time
			for k, v := range searchCache {
				if time.Since(v.at) >= searchCacheTTL {
					delete(searchCache, k)
					continue
				}
				if oldestKey == "" || v.at.Before(oldest) {
					oldestKey, oldest = k, v.at
				}
			}
			if len(searchCache) >= searchCacheMax && oldestKey != "" {
				delete(searchCache, oldestKey)
			}
		}
		searchCache[key] = searchCacheItem{data: fl.data, at: time.Now()}
	}
	searchCacheMu.Unlock()
	fl.wg.Done()
	return fl.data, fl.err
}

// pruneSearchCacheLocked прибирає прострочені записи і, якщо разом із новим
// записом (incoming байт) кеш перевищує searchCacheMaxBytes, — найстаріші.
// Викликається під searchCacheMu.
func pruneSearchCacheLocked(incoming int) {
	total := incoming
	for k, v := range searchCache {
		if time.Since(v.at) >= searchCacheTTL {
			delete(searchCache, k)
			continue
		}
		total += len(v.data)
	}
	for total > searchCacheMaxBytes && len(searchCache) > 0 {
		var oldestKey string
		var oldest time.Time
		for k, v := range searchCache {
			if oldestKey == "" || v.at.Before(oldest) {
				oldestKey, oldest = k, v.at
			}
		}
		total -= len(searchCache[oldestKey].data)
		delete(searchCache, oldestKey)
	}
}
