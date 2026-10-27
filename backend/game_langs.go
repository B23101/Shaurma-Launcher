package main

import (
	"shaurma-launcher-backend/internal/crashguard"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"shaurma-launcher-backend/internal/custompack"
	"shaurma-launcher-backend/internal/model"
)

// ── Мови гри з ОРИГІНАЛЬНИХ json-мов Mojang ─────────────────────────────
// Раніше список мов у налаштуваннях збірки був захардкоджений (оптимальні
// 15 мов). Тепер беремо його з справжнього asset index версії Minecraft:
//   assets/indexes/<id>.json  →  ключі "minecraft/lang/<code>.json"
// а назви (language.name + language.region) читаємо з САМИХ файлів мов
// (assets/objects/<hh>/<hash>, той самий кеш, що й гра; при відсутності —
// довантажуємо з CDN Mojang і зберігаємо в той самий кеш).
//
// Так список завжди відповідає конкретній версії гри (нові мови з'являються
// автоматично), а не фіксованому набору.

var (
	gameLangsMu    sync.Mutex
	gameLangsCache = map[string]gameLangsEntry{}
)

type gameLangsEntry struct {
	at   time.Time
	list []model.GameLanguage
}

const gameLangsTTL = 30 * time.Minute

// GetGameLanguagesForPack — мови гри для версії КОНКРЕТНОЇ збірки
// (сторінка збірки → «Налаштування гри (options.txt)» → Мова).
func (a *App) GetGameLanguagesForPack(packID string) ([]model.GameLanguage, error) {
	cp, ok := a.customPacks.Get(packID)
	if !ok {
		return nil, fmt.Errorf("збірку не знайдено: %s", packID)
	}
	return a.GetGameLanguages(cp.MCVersion)
}

// GetGameLanguages — мови гри за версією Minecraft. Версія без аргументу
// («Гра за умовч.» у налаштуваннях): беремо версію останньої створеної
// збірки, інакше — будь-яку встановлену версію на диску.
func (a *App) GetGameLanguages(mcVersion string) ([]model.GameLanguage, error) {
	if strings.TrimSpace(mcVersion) == "" {
		baseDir := a.cfg.GetSettings().InstanceDir
		mcVersion = findLatestKnownVersion(a.customPacks, baseDir)
	}
	if mcVersion == "" {
		// Немає локально встановлених версій — беремо останню стабільну
		// версію прямо з маніфесту Mojang (version_manifest_v2): так мови
		// вантажаться завжди, навіть на свіжому лаунчері без жодної збірки.
		latest, err := latestReleaseMCVersion()
		if err != nil {
			return nil, nil // офлайн/маніфест недоступний — фронт лишає fallback
		}
		mcVersion = latest
	}

	gameLangsMu.Lock()
	if e, ok := gameLangsCache[mcVersion]; ok && time.Since(e.at) < gameLangsTTL {
		gameLangsMu.Unlock()
		return e.list, nil
	}
	gameLangsMu.Unlock()

	list, err := loadGameLanguages(a.cfg.GetSettings().InstanceDir, mcVersion)
	if err != nil {
		return nil, err
	}

	gameLangsMu.Lock()
	gameLangsCache[mcVersion] = gameLangsEntry{at: time.Now(), list: list}
	gameLangsMu.Unlock()
	return list, nil
}

// latestReleaseMCVersion — остання стабільна версія Minecraft з маніфесту
// Mojang (version_manifest_v2). Використовується, коли локально ще немає
// жодної встановленої версії — список мов все одно має вантажитися.
func latestReleaseMCVersion() (string, error) {
	return custompack.NewVersionsProvider().LatestReleaseMCVersion()
}

// findLatestKnownVersion — версія останньої створеної кастомної збірки;
// якщо збірок немає — перша встановлена версія (перебір теки versions/).
func findLatestKnownVersion(store *custompack.Store, baseDir string) string {
	if store != nil {
		if packs := store.List(); len(packs) > 0 {
			best := packs[0]
			for _, p := range packs[1:] {
				if p.CreatedAt.After(best.CreatedAt) {
					best = p
				}
			}
			if best.MCVersion != "" {
				return best.MCVersion
			}
		}
	}
	// Фолбек: будь-яка тека versions/<id>/ з <id>.json, що має assetIndex.
	entries, err := os.ReadDir(filepath.Join(baseDir, "versions"))
	if err != nil {
		return ""
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for i := len(names) - 1; i >= 0; i-- {
		if _, err := os.Stat(filepath.Join(baseDir, "versions", names[i], names[i]+".json")); err == nil {
			return names[i]
		}
	}
	return ""
}

func loadGameLanguages(baseDir, mcVersion string) ([]model.GameLanguage, error) {
	vjsonPath := filepath.Join(baseDir, "versions", mcVersion, mcVersion+".json")
	data, err := os.ReadFile(vjsonPath)
	if err != nil {
		// Версія ще НЕ встановлена (збірка не завантажена) — це не помилка,
		// а просто «мов поки немає». Раніше кидали помилку, яка через
		// автолог call() потрапляла в журнал помилок лаунчера (шум: сторінка
		// кожної невстановленої збірки писала «не знайдено version.json»).
		// Фронтенд і так лишає fallback-список мов при порожньому результаті.
		if os.IsNotExist(err) {
			return []model.GameLanguage{}, nil
		}
		return nil, fmt.Errorf("не знайдено version.json для %s: %w", mcVersion, err)
	}
	var vjson struct {
		AssetIndex struct {
			ID   string `json:"id"`
			URL  string `json:"url"`
			Sha1 string `json:"sha1"`
		} `json:"assetIndex"`
	}
	if err := json.Unmarshal(data, &vjson); err != nil {
		return nil, err
	}
	if vjson.AssetIndex.ID == "" {
		return nil, fmt.Errorf("version.json %s не містить assetIndex", mcVersion)
	}

	indexPath := filepath.Join(baseDir, "assets", "indexes", vjson.AssetIndex.ID+".json")
	idxData, err := readOrFetch(indexPath, vjson.AssetIndex.URL, vjson.AssetIndex.Sha1)
	if err != nil {
		return nil, fmt.Errorf("asset index %s: %w", vjson.AssetIndex.ID, err)
	}
	var idx struct {
		Objects map[string]struct {
			Hash string `json:"hash"`
			Size int64  `json:"size"`
		} `json:"objects"`
	}
	if err := json.Unmarshal(idxData, &idx); err != nil {
		return nil, err
	}

	// Коди мов: ключі "minecraft/lang/<code>.json".
	codes := make([]string, 0)
	for k := range idx.Objects {
		if strings.HasPrefix(k, "minecraft/lang/") && strings.HasSuffix(k, ".json") {
			codes = append(codes, strings.TrimSuffix(strings.TrimPrefix(k, "minecraft/lang/"), ".json"))
		}
	}
	if len(codes) == 0 {
		return []model.GameLanguage{}, nil
	}

	// Назви — з самих файлів мов. Паралельно (ліміт 8), локальний кеш
	// assets/objects використовується, якщо гра вже качалася.
	out := make([]model.GameLanguage, 0, len(codes))
	var (
		wg  sync.WaitGroup
		sem = make(chan struct{}, 8)
		mu  sync.Mutex
	)
	for _, code := range codes {
		wg.Add(1)
		go func(code string) {
			defer crashguard.Recover("game_langs.go:198")
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			obj, ok := idx.Objects["minecraft/lang/"+code+".json"]
			if !ok {
				return
			}
			name := langNameFromObject(baseDir, obj.Hash)
			mu.Lock()
			out = append(out, model.GameLanguage{Code: code, Name: name})
			mu.Unlock()
		}(code)
	}
	wg.Wait()

	sort.Slice(out, func(i, j int) bool {
		ni, nj := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name)
		if ni == nj {
			return out[i].Code < out[j].Code
		}
		return ni < nj
	})
	return out, nil
}

// langNameFromObject читає language.name/language.region з файлу мови
// (assets/objects/<hh>/<hash>); при відсутності локально — качає з CDN
// Mojang і кладе в той самий кеш (щоб наступного разу було локально).
func langNameFromObject(baseDir, hash string) string {
	if hash == "" {
		return ""
	}
	objPath := filepath.Join(baseDir, "assets", "objects", hash[:2], hash)
	data, err := os.ReadFile(objPath)
	if err != nil {
		data, err = fetchBytes("https://resources.download.minecraft.net/" + hash[:2] + "/" + hash)
		if err != nil || len(data) == 0 {
			return ""
		}
		if mdir := os.MkdirAll(filepath.Dir(objPath), 0755); mdir == nil {
			_ = os.WriteFile(objPath, data, 0644)
		}
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return ""
	}
	name, _ := m["language.name"].(string)
	region, _ := m["language.region"].(string)
	name = strings.TrimSpace(name)
	region = strings.TrimSpace(region)
	switch {
	case name == "":
		return ""
	case region == "":
		return name
	default:
		return name + " (" + region + ")"
	}
}

// readOrFetch читає файл з диску, при відсутності — качає і зберігає
// (з перевіркою sha1, якщо задано).
func readOrFetch(path, url, wantSha1 string) ([]byte, error) {
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		return data, nil
	}
	data, err := fetchBytes(url)
	if err != nil {
		return nil, err
	}
	if wantSha1 != "" {
		sum := sha1.Sum(data)
		if hex.EncodeToString(sum[:]) != strings.ToLower(wantSha1) {
			return nil, fmt.Errorf("sha1 не збігається (%s)", url)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err == nil {
		_ = os.WriteFile(path, data, 0644)
	}
	return data, nil
}

var langsHTTPClient = &http.Client{Timeout: 25 * time.Second}

func fetchBytes(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ShaurmaLauncher/2.0 (+assets)")
	resp, err := langsHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
