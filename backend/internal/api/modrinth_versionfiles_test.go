package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// TestVersionFilesResponseIsObjectNotArray фіксує формат відповіді
// POST /version_files: Modrinth повертає ОБ'ЄКТ {hash: version}, а не
// масив версій. Раніше GetVersionsByHashes(512) парсили це як []mrVersion,
// json.Unmarshal мовчки провалювався на об'єкті, і виклик трактувався як
// "мережа недоступна" — усі моди йшли в overrides/bundled без резолву,
// а modrinth.index.json завжди отримував порожній "files": []. Цей тест
// ловить регрес, якщо хтось знову поміняє тип назад на масив.
// TestLatestVersionsByHashesFilteredSendsVersionTypes фіксує логіку каналу
// релізів як у Modrinth App: для release-only має йти version_types=["release"].
func TestLatestVersionsByHashesFilteredSendsVersionTypes(t *testing.T) {
	var gotTypes []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Hashes       []string `json:"hashes"`
			VersionTypes []string `json:"version_types"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotTypes = body.VersionTypes
		_ = json.NewEncoder(w).Encode(map[string]any{
			"abc": map[string]any{
				"id": "v1", "project_id": "p1", "version_number": "1.0.0",
				"files": []map[string]any{{"url": "https://cdn/x.jar", "filename": "x.jar", "primary": true}},
			},
		})
	}))
	defer srv.Close()

	c := NewModrinthClient()
	c.baseURL = srv.URL
	got, err := c.GetLatestVersionsByHashesFiltered([]string{"abc"}, []string{"fabric"}, []string{"1.21.1"}, []string{"release"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(gotTypes, []string{"release"}) {
		t.Fatalf("version_types не передано: %#v", gotTypes)
	}
	if len(got) != 1 || got["abc"] == nil || got["abc"].VersionNumber != "1.0.0" {
		t.Fatalf("response not parsed: %#v", got)
	}
}

// TestLatestVersionsByHashesFilteredFallsBackWithoutVersionTypes — старіший
// проксі/бекенд може відкинути version_types; перевірка не повинна падати,
// вона повторює запит без фільтра.
func TestLatestVersionsByHashesFilteredFallsBackWithoutVersionTypes(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, has := body["version_types"]; has {
			http.Error(w, "unknown field version_types", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer srv.Close()

	c := NewModrinthClient()
	c.baseURL = srv.URL
	if _, err := c.GetLatestVersionsByHashesFiltered([]string{"abc"}, []string{"fabric"}, []string{"1.21.1"}, []string{"release"}); err != nil {
		t.Fatalf("fallback без version_types мав спрацювати: %v", err)
	}
	if calls != 2 {
		t.Fatalf("очікували 2 запити (з фільтром + fallback), отримали %d", calls)
	}
}

func TestVersionFilesResponseIsObjectNotArray(t *testing.T) {
	// Реальна форма відповіді Modrinth /version_files.
	raw := []byte(`{
		"aabbccsha512hash": {
			"id": "v1",
			"project_id": "p1",
			"version_number": "1.0.0",
			"files": [
				{"url": "https://cdn.modrinth.com/data/create.jar", "filename": "create.jar",
				 "size": 123, "primary": true,
				 "hashes": {"sha1": "aabbccsha1hash", "sha512": "aabbccsha512hash"}}
			]
		}
	}`)

	// Старий (баг) спосіб — має провалитись на об'єкті.
	var arr []mrVersion
	if err := json.Unmarshal(raw, &arr); err == nil {
		t.Fatal("очікували помилку unmarshal у масив — Modrinth повертає об'єкт, не масив")
	}

	// Правильний спосіб — має спрацювати.
	var obj map[string]mrVersion
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("unmarshal у map мав спрацювати: %v", err)
	}
	v, ok := obj["aabbccsha512hash"]
	if !ok {
		t.Fatal("ключ хешу відсутній у розпарсеній мапі")
	}
	if v.ProjectID != "p1" || len(v.Files) != 1 {
		t.Fatalf("невірний розпарсений вміст: %+v", v)
	}
	if v.Files[0].Hashes.Sha512 != "aabbccsha512hash" {
		t.Errorf("sha512 файлу невірний: %+v", v.Files[0])
	}
}
