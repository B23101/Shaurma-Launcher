package store

import (
	"time"
)

// ── builds_registry ────────────────────────────────────────────────

func (s *Store) LoadBuildsRegistry() ([]RegistryEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]RegistryEntry, len(s.data.BuildsRegistry))
	copy(out, s.data.BuildsRegistry)
	return out, nil
}

func (s *Store) SaveBuildsRegistry(entries []RegistryEntry) error {
	s.mu.Lock()
	s.data.BuildsRegistry = entries
	err := s.saveGlobal()
	s.mu.Unlock()
	return err
}

func (s *Store) RemoveBuildRegistryEntry(id string) error {
	s.mu.Lock()
	for i, e := range s.data.BuildsRegistry {
		if e.ID == id {
			s.data.BuildsRegistry = append(s.data.BuildsRegistry[:i], s.data.BuildsRegistry[i+1:]...)
			break
		}
	}
	err := s.saveGlobal()
	s.mu.Unlock()
	return err
}

// customPacks тепер зберігаються ТІЛЬКИ в custom-packs.json
// (custompack.Store) та instance.json. Ніякого центрального хабу.

// ── gallery_meta ───────────────────────────────────────────────────

func (s *Store) LoadGalleryMeta() ([]GalleryMetaEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]GalleryMetaEntry, len(s.data.GalleryMeta))
	copy(out, s.data.GalleryMeta)
	return out, nil
}

func (s *Store) SaveGalleryMeta(entries []GalleryMetaEntry) error {
	s.mu.Lock()
	s.data.GalleryMeta = entries
	err := s.saveGlobal()
	s.mu.Unlock()
	return err
}

// ── shortcuts ──────────────────────────────────────────────────────

func (s *Store) LoadShortcuts() (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.data.Shortcuts))
	for k, v := range s.data.Shortcuts {
		out[k] = v
	}
	return out, nil
}

func (s *Store) SaveShortcuts(reg map[string]string) error {
	s.mu.Lock()
	s.data.Shortcuts = reg
	err := s.saveGlobal()
	s.mu.Unlock()
	return err
}

// ── browser_ai_cache ───────────────────────────────────────────────

func (s *Store) GetAICache(cacheKey string) (AICacheEntry, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.AICache == nil {
		return AICacheEntry{}, false, nil
	}
	e, ok := s.data.AICache[cacheKey]
	if !ok {
		return AICacheEntry{}, false, nil
	}
	return AICacheEntry{
		CacheKey:  e.CacheKey,
		Kind:      e.Kind,
		Data:      e.Data,
		CreatedAt: e.CreatedAt,
	}, true, nil
}

func (s *Store) SaveAICache(kind, cacheKey, data string) error {
	s.mu.Lock()
	if s.data.AICache == nil {
		s.data.AICache = map[string]aiCacheEntry{}
	}
	s.data.AICache[cacheKey] = aiCacheEntry{
		CacheKey:  cacheKey,
		Kind:      kind,
		Data:      data,
		CreatedAt: time.Now(),
	}
	err := s.saveGlobal()
	s.mu.Unlock()
	return err
}

func (s *Store) DeleteAICacheOlderThan(ttl time.Duration) (int64, error) {
	s.mu.Lock()
	cutoff := time.Now().Add(-ttl)
	var removed int64
	for k, e := range s.data.AICache {
		if e.CreatedAt.Before(cutoff) {
			delete(s.data.AICache, k)
			removed++
		}
	}
	err := s.saveGlobal()
	s.mu.Unlock()
	return removed, err
}

func (s *Store) DeleteAICacheKindOlderThan(kind string, ttl time.Duration) (int64, error) {
	s.mu.Lock()
	cutoff := time.Now().Add(-ttl)
	var removed int64
	for k, e := range s.data.AICache {
		if e.Kind == kind && e.CreatedAt.Before(cutoff) {
			delete(s.data.AICache, k)
			removed++
		}
	}
	err := s.saveGlobal()
	s.mu.Unlock()
	return removed, err
}

// ── install_state ──────────────────────────────────────────────────

// InstallState тепер зберігається ТІЛЬКИ в install-state/<packID>.json
// (sync.StateStore). Ніякого центрального хабу.

// ── wardrobe_presets ───────────────────────────────────────────────

func (s *Store) LoadWardrobePresets(username string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.WardrobePresets == nil {
		return "", false, nil
	}
	v, ok := s.data.WardrobePresets[username]
	if !ok {
		return "", false, nil
	}
	return v, true, nil
}

func (s *Store) SaveWardrobePresets(username, data string) error {
	s.mu.Lock()
	if s.data.WardrobePresets == nil {
		s.data.WardrobePresets = map[string]string{}
	}
	s.data.WardrobePresets[username] = data
	err := s.saveGlobal()
	s.mu.Unlock()
	return err
}

func (s *Store) DeleteWardrobePresets(username string) error {
	s.mu.Lock()
	delete(s.data.WardrobePresets, username)
	err := s.saveGlobal()
	s.mu.Unlock()
	return err
}

func (s *Store) LoadAllWardrobePresets() (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.data.WardrobePresets))
	for k, v := range s.data.WardrobePresets {
		out[k] = v
	}
	return out, nil
}

// ── export_settings ────────────────────────────────────────────────

func (s *Store) LoadExportSettings(packID string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.ExportSettings == nil {
		return "", false, nil
	}
	v, ok := s.data.ExportSettings[packID]
	if !ok {
		return "", false, nil
	}
	return v, true, nil
}

func (s *Store) SaveExportSettings(packID, data string) error {
	s.mu.Lock()
	if s.data.ExportSettings == nil {
		s.data.ExportSettings = map[string]string{}
	}
	s.data.ExportSettings[packID] = data
	err := s.saveGlobal()
	s.mu.Unlock()
	return err
}

func (s *Store) DeleteExportSettings(packID string) error {
	s.mu.Lock()
	delete(s.data.ExportSettings, packID)
	err := s.saveGlobal()
	s.mu.Unlock()
	return err
}

func (s *Store) LoadAllExportSettingsKeys() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.data.ExportSettings))
	for k := range s.data.ExportSettings {
		out = append(out, k)
	}
	return out, nil
}

// ── duplicate_settings ─────────────────────────────────────────────

func (s *Store) LoadDuplicateSettings(packID string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.data.DuplicateSettings == nil {
		return "", false, nil
	}
	v, ok := s.data.DuplicateSettings[packID]
	if !ok {
		return "", false, nil
	}
	return v, true, nil
}

func (s *Store) SaveDuplicateSettings(packID, data string) error {
	s.mu.Lock()
	if s.data.DuplicateSettings == nil {
		s.data.DuplicateSettings = map[string]string{}
	}
	s.data.DuplicateSettings[packID] = data
	err := s.saveGlobal()
	s.mu.Unlock()
	return err
}

func (s *Store) DeleteDuplicateSettings(packID string) error {
	s.mu.Lock()
	delete(s.data.DuplicateSettings, packID)
	err := s.saveGlobal()
	s.mu.Unlock()
	return err
}

func (s *Store) LoadAllDuplicateSettingsKeys() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.data.DuplicateSettings))
	for k := range s.data.DuplicateSettings {
		out = append(out, k)
	}
	return out, nil
}
