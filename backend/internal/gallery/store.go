// Package gallery — метадані галереї скріншотів (розділ «Галерея» на
// сторінці збірки). Сама галерея показує реальні файли з
// <InstanceDir>/<packID>/.minecraft/screenshots — цей пакет лише тримає
// ДОДАТКОВІ дані, яких файлова система не має: власна назва (якщо
// користувач перейменував не сам файл, а «підпис» у галереї), опис і
// час останнього перегляду. Прив'язка — по packID + імені файлу.
//
// Зберігається в instance.json кожної збірки (персональний файл),
// а НЕ в спільному gallery-meta.json.
package gallery

import (
	"shaurma-launcher-backend/internal/custompack"
)

// Meta — додаткові дані одного скріншоту (title/description користувача).
type Meta struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

// Store — персистентний реєстр метаданих скріншотів усіх збірок.
// Джерело правди — instance.json кожної збірки (custompack.Store).
type Store struct {
	cps *custompack.Store
}

func NewStore(cps *custompack.Store) *Store {
	return &Store{cps: cps}
}

// Get повертає метадані одного скріншоту (порожній Meta, якщо немає).
func (s *Store) Get(packID, fileName string) Meta {
	g := s.cps.GetGallery(packID)
	if m, ok := g[fileName]; ok {
		return Meta{Title: m.Title, Description: m.Description}
	}
	return Meta{}
}

// GetAll повертає всі метадані однієї збірки (для списку галереї).
func (s *Store) GetAll(packID string) map[string]Meta {
	g := s.cps.GetGallery(packID)
	out := make(map[string]Meta, len(g))
	for k, v := range g {
		out[k] = Meta{Title: v.Title, Description: v.Description}
	}
	return out
}

// Set зберігає (або, якщо і title, і description порожні — видаляє)
// метадані одного скріншоту.
func (s *Store) Set(packID, fileName string, m Meta) error {
	return s.cps.SetGallery(packID, fileName, custompack.GalleryEntry{
		Title:       m.Title,
		Description: m.Description,
	})
}

// Rename переносить метадані зі старого імені файлу на нове.
func (s *Store) Rename(packID, oldName, newName string) error {
	return s.cps.RenameGallery(packID, oldName, newName)
}

// Delete прибирає метадані видаленого скріншоту.
func (s *Store) Delete(packID, fileName string) error {
	return s.cps.DeleteGallery(packID, fileName)
}
