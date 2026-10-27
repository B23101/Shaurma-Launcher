// migration.go — ОДНОРАЗОВА міграція даних зі старих JSON-файлів у SQLite.
//
// Історія: до версії з єдиною базою даних лаунчер тримав стан у розкиданих
// JSON-файлах (builds.json, custom-packs.json, gallery-meta.json,
// shortcuts.json, browser-ai/, install-state/, skins/presets.json) у папці
// даних .shaurm та теці конфігурації. Тепер джерело правди — shaurma.db
// (таблиці builds_registry, custom_packs, gallery_meta, shortcuts,
// browser_ai_cache, install_state, wardrobe_presets).
//
// Міграція запускається рівно ОДИН раз (прапорець files_migrated у
// app_meta): імпортує наявні JSON-файли в БД і ПОТІМ ВИДАЛЯЄ їх, щоб не
// лишалось дублів і старого сміття. Повторний запуск не відновлює
// видалені файли — наступні запуски працюють лише з БД.
//
// Додатково прибирається мусор, який лаунчер більше не створює:
//   - download-state/  — легасі стан качалок V1 (не використовується);
//   - .locks/          — залишки файлових блокувань;
//   - logs/            — старі логи лаунчера (місце логів вибирає користувач);
//   - mods-state.json  — старий файл менеджера модів (легасі);
//   - avatars/ — текстури голів переносяться в cache/ (це кеш);
//   - skins/ — ЛИШАЄТЬСЯ на місці (.shaurm/skins): текстури пресетів
//     гардеробу — постійні дані, не кеш;
//   - window-state.json — ЛИШАЄТЬСЯ на місці: його читає shell ДО старту
//     бази даних (відновлення розміру/позиції вікна до відкриття webview),
//     тому він фізично не може жити в SQLite.
package main

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"shaurma-launcher-backend/internal/gallery"
	"shaurma-launcher-backend/internal/model"
	"shaurma-launcher-backend/internal/store"
)

// metaFilesMigrated — ключ прапорця «файлова міграція виконана» в app_meta.
const metaFilesMigrated = "files_migrated"

// runFilesMigration виконує одноразове перенесення даних зі старих
// JSON-файлів у SQLite (див. коментар пакета). Ідемпотентно: якщо прапорець
// вже стоїть — нічого не робить. Викликається з ServiceStartup після
// cfg.Load() (БД вже відкрита) і до першого використання реєстрів.
func (a *App) runFilesMigration() {
	// Тепер дані зберігаються в JSON (shaurma.json + instance.json).
	// Конвертація з SQLite виконується в store.MigrateFromSQLiteIfAvailable().
	// Цей метод лишається як no-op для сумісності.
}

// migrateBuildsRegistry переносить builds.json → builds_registry.
func (a *App) migrateBuildsRegistry(st *store.Store) {
	path := filepath.Join(a.cfg.Dir(), "builds.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return // файлу немає — нема чого мігрувати
	}
	var list []struct {
		ID               string `json:"id"`
		Kind             string `json:"kind"`
		InstalledVersion string `json:"installedVersion"`
		InstalledAt      string `json:"installedAt"`
	}
	if json.Unmarshal(data, &list) != nil {
		log.Printf("migration: builds.json не розпізнано, файл видаляється без імпорту")
		_ = os.Remove(path)
		return
	}
	rows := make([]store.RegistryEntry, 0, len(list))
	for _, e := range list {
		at := time.Now().Unix()
		if t, err := time.Parse(time.RFC3339, e.InstalledAt); err == nil {
			at = t.Unix()
		}
		rows = append(rows, store.RegistryEntry{
			ID:               e.ID,
			Kind:             e.Kind,
			InstalledVersion: e.InstalledVersion,
			InstalledAt:      at,
		})
	}
	if len(rows) > 0 {
		if err := st.SaveBuildsRegistry(rows); err != nil {
			log.Printf("migration: builds.json → БД: %v", err)
			return
		}
	}
	_ = os.Remove(path)
	log.Printf("migration: builds.json імпортовано (%d збірок), файл видалено", len(rows))
}

// migrateCustomPacks — legacy: custom-packs.json вже є джерелом правди.
func (a *App) migrateCustomPacks(st *store.Store) {}

// migrateShaurmaPacksToSubdir переносить Shaurma-збірки з installations/<id>/
// в installations/shaurma_packs/<id>/ (окрема тека для Shaurma-збірок).
// Викликається ОДИН раз при старті після buildsReg.Load().
func (a *App) migrateShaurmaPacksToSubdir() {
	if a.buildsReg == nil {
		return
	}
	instDir := a.cfg.InstancesDir()
	shaurmaDir := a.cfg.ShaurmaPacksDir()
	if err := os.MkdirAll(shaurmaDir, 0755); err != nil {
		return
	}
	moved := 0
	for _, id := range a.buildsReg.AllIDs() {
		entry, ok := a.buildsReg.Get(id)
		if !ok || entry.Kind != "shaurma" {
			continue
		}
		src := filepath.Join(instDir, id)
		dst := filepath.Join(shaurmaDir, id)
		// Вже в правильній теці
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		// Немає звідки переносити
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := os.Rename(src, dst); err != nil {
			log.Printf("migration: не вдалося перенести shaurma збірку %s: %v", entry.ID, err)
			continue
		}
		moved++
	}
	if moved > 0 {
		log.Printf("migration: перенесено %d Shaurma-збірок у shaurma_packs/", moved)
	}
}

// migrateGalleryMeta переносить gallery-meta.json → instance.json кожної збірки.
func (a *App) migrateGalleryMeta(_ *store.Store) {
	path := filepath.Join(a.cfg.ConfigDir(), "gallery-meta.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var list []struct {
		Key struct {
			PackID   string `json:"packId"`
			FileName string `json:"fileName"`
		} `json:"key"`
		Meta gallery.Meta `json:"meta"`
	}
	if json.Unmarshal(data, &list) != nil {
		_ = os.Remove(path)
		return
	}
	imported := 0
	for _, e := range list {
		if e.Key.PackID == "" || e.Key.FileName == "" {
			continue
		}
		if err := a.gallery.Set(e.Key.PackID, e.Key.FileName, e.Meta); err == nil {
			imported++
		}
	}
	_ = os.Remove(path)
	log.Printf("migration: gallery-meta.json імпортовано в instance.json (%d записів), файл видалено", imported)
}

// migrateShortcuts переносить shortcuts.json → таблиця shortcuts.
func (a *App) migrateShortcuts(st *store.Store) {
	path := a.shortcutsFile()
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var reg map[string]string
	if json.Unmarshal(data, &reg) != nil || reg == nil {
		_ = os.Remove(path)
		return
	}
	if len(reg) > 0 {
		if err := st.SaveShortcuts(reg); err != nil {
			log.Printf("migration: shortcuts.json → БД: %v", err)
			return
		}
	}
	_ = os.Remove(path)
	log.Printf("migration: shortcuts.json імпортовано (%d ярликів), файл видалено", len(reg))
}

// migrateBrowserAICache переносить browser-ai/ (translations + reviews) у
// таблицю browser_ai_cache. Текa видаляється цілком після імпорту.
func (a *App) migrateBrowserAICache(st *store.Store) {
	root := a.browserAIDir()
	dirs := []struct {
		sub  string
		kind string
	}{
		{"translations", aiCacheKindTranslation},
		{"reviews", aiCacheKindReview},
	}
	total := 0
	for _, d := range dirs {
		dir := filepath.Join(root, d.sub)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			key := strings.TrimSuffix(e.Name(), ".json")
			if err := st.SaveAICache(d.kind, key, string(data)); err == nil {
				total++
			}
		}
	}
	if total > 0 {
		log.Printf("migration: browser-ai імпортовано (%d записів) у SQLite", total)
	}
	// Теку прибираємо цілком — кеш тепер у БД.
	_ = os.RemoveAll(root)
}

// migrateInstallState — legacy migration. InstallState вже зберігається
// в install-state/<packID>.json (sync.StateStore), файли вже на місці.
func (a *App) migrateInstallState(st *store.Store) {
	// InstallState вже файловий — нічого мігрувати.
}

// migrateWardrobePresets переносить skins/presets.json → wardrobe_presets.
// Блоб формату storeDoc (v2: byAccount + legacy) зберігається 1-в-1 на
// кожен акаунт; файл presets.json видаляється.
func (a *App) migrateWardrobePresets(st *store.Store) {
	skinsDir := filepath.Join(a.cfg.Dir(), "skins")
	path := filepath.Join(skinsDir, "presets.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var doc struct {
		Version   int                           `json:"version"`
		ByAccount map[string][]model.SkinPreset `json:"byAccount"`
		Legacy    []model.SkinPreset            `json:"legacy,omitempty"`
	}
	if json.Unmarshal(data, &doc) != nil || doc.ByAccount == nil {
		// Старий плоский формат: весь масив → першому акаунту (як робив
		// wardrobe.Store при «усиновленні» legacy-інвентаря).
		var list []model.SkinPreset
		if json.Unmarshal(data, &list) == nil && list != nil {
			doc = struct {
				Version   int                           `json:"version"`
				ByAccount map[string][]model.SkinPreset `json:"byAccount"`
				Legacy    []model.SkinPreset            `json:"legacy,omitempty"`
			}{Version: 2, Legacy: list}
		} else {
			_ = os.Remove(path)
			return
		}
	}
	// Зберігаємо блоб за кожним акаунтом (якщо byAccount порожній, але є
	// legacy — тимчасово під ключем "_legacy", гардероб підхопить).
	accounts := doc.ByAccount
	if len(accounts) == 0 && len(doc.Legacy) > 0 {
		accounts = map[string][]model.SkinPreset{"_legacy": doc.Legacy}
	}
	imported := 0
	for accountID, presets := range accounts {
		sub := struct {
			Version   int                           `json:"version"`
			ByAccount map[string][]model.SkinPreset `json:"byAccount"`
			Legacy    []model.SkinPreset            `json:"legacy,omitempty"`
		}{Version: 2, ByAccount: map[string][]model.SkinPreset{accountID: presets}}
		blob, err := json.Marshal(sub)
		if err != nil {
			continue
		}
		if err := st.SaveWardrobePresets(accountID, string(blob)); err == nil {
			imported++
		}
	}
	_ = os.Remove(path)
	log.Printf("migration: presets.json імпортовано (%d акаунтів), файл видалено", imported)
}

// cleanupLegacyGarbage прибирає файли/теки, які лаунчер більше не створює
// або які не повинні лишатись у папці даних:
//   - download-state/  — легасі стан качалок (двигун V2 не використовує);
//   - .locks/          — старі файлові блокування;
//   - logs/            — старі логи лаунчера;
//   - mods-state.json  — старий файл менеджера модів;
//   - avatars/ → cache/avatars (текстури голів — це кеш);
//   - skins/ лишається на місці (.shaurm/skins — постійні дані гардеробу).
func (a *App) cleanupLegacyGarbage() {
	base := a.cfg.Dir()
	cacheDir := a.cfg.CacheDir()

	// Легасі-кеш binpatch-файлів Forge/NeoForge у СИСТЕМНОМУ %TEMP%:
	// старі версії лаунчера витягували їх у os.TempDir()/shaurma-forge.
	// Тепер кеш живе у cache/forge (папка даних лаунчера, чиститься
	// ClearCache) — системний temp більше не використовується взагалі
	// (він може бути на іншому диску і лишати сміття на C:). Прибираємо
	// залишки один раз.
	if tmpRoot := filepath.Join(os.TempDir(), "shaurma-forge"); pathExists(tmpRoot) {
		_ = os.RemoveAll(tmpRoot)
		log.Printf("migration: видалено легасі кеш Forge з системного temp %s", tmpRoot)
	}

	for _, name := range []string{"download-state", ".locks", "logs"} {
		dir := filepath.Join(base, name)
		if _, err := os.Stat(dir); err == nil {
			_ = os.RemoveAll(dir)
			log.Printf("migration: видалено легасі теку %s", dir)
		}
	}

	// mods-state.json — старий файл менеджера модів (легасі).
	if p := filepath.Join(base, "mods-state.json"); fileExists(p) {
		_ = os.Remove(p)
		log.Printf("migration: видалено легасі файл %s", p)
	}

	// avatars/ → cache/avatars: переміщуємо файли, щоб не губити закешовані
	// голови (кеш у .shaurm/avatars більше не читається).
	a.moveDirContents(filepath.Join(base, "avatars"), filepath.Join(cacheDir, "avatars"))

	// skins/ — ЛИШАЄТЬСЯ на місці (.shaurm/skins): PNG текстури пресетів
	// гардеробу — постійні дані користувача, а не кеш (їх не можна чистити).
	// presets.json імпортований у БД вище (migrateWardrobePresets), а самі
	// PNG-файли лишаються в .shaurm/skins і надалі пишуться туди ж.

	// window-state.json: стан вікна переїжджає з папки даних (.shaurm) у
	// теку ПРОГРАМИ (ConfigDir — біля exe). Вікно запускається до БД, тому
	// це лишається файлом (єдиний виняток), але не в даних користувача.
	// Якщо у новому місці файлу вже є — старий зайвий (свіжіший стан там).
	if _, err := os.Stat(a.windowStatePath()); err != nil {
		oldWin := filepath.Join(base, "window-state.json")
		if _, err := os.Stat(oldWin); err == nil {
			if os.MkdirAll(filepath.Dir(a.windowStatePath()), 0755) == nil {
				_ = os.Rename(oldWin, a.windowStatePath())
				log.Printf("migration: window-state.json перенесено у теку програми")
			}
		}
	} else {
		_ = os.Remove(filepath.Join(base, "window-state.json"))
	}
}

// moveDirContents переміщує ВМІСТ тек-джерела в тек-призначення (створює
// призначення, якщо треба) і видаляє порожнє джерело. Якщо джерела немає —
// no-op. Файли з тим самим іменем у призначенні не перезаписуються.
func (a *App) moveDirContents(src, dst string) {
	entries, err := os.ReadDir(src)
	if err != nil {
		return // джерела немає або воно порожнє — нічого переносити
	}
	moved := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		from := filepath.Join(src, e.Name())
		to := filepath.Join(dst, e.Name())
		if fileExists(to) {
			continue
		}
		if err := os.MkdirAll(dst, 0755); err != nil {
			continue
		}
		if os.Rename(from, to) == nil {
			moved++
		}
	}
	if moved > 0 {
		log.Printf("migration: %s → %s (%d файлів)", src, dst, moved)
	}
	// Джерело тепер порожнє (або взагалі не існувало) — прибираємо.
	_ = os.RemoveAll(src)
}

// samePath — чи два шляхи вказують на одне й те саме місце (нормалізовані
// і, на Windows, без урахування регістру). Використовується, щоб не
// «переносити теку в саму себе» при зміні папки даних на ту саму.
func samePath(a, b string) bool {
	if a == "" || b == "" {
		return a == b
	}
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// moveTree рекурсивно переносить src (файл або теку) у dst. Якщо dst вже
// існує — вміст зливається (файли з тим самим іменем не перезаписуються:
// лишається той, що вже є в dst). Після перенесення порожні теки-джерела
// видаляються. Повертає першу помилку, але не зупиняється на ній — щоб
// один файл, що не перенісся (заблокований), не загубив решту даних.
// moveFile — перенос одного файлу з надійністю: спочатку швидкий
// os.Rename (атомарний, той самий том), якщо не вдалось — копіювання з
// наступним видаленням. Фолбек-копія покриває ОБИДВА випадки, де rename
// на Windows падає:
//   - файл відкритий процесом (напр. launcher.log — Windows забороняє
//     перейменування відкритих файлів, але дозволяє читання);
//   - різні томи (стара папка даних на C:, нова на D:) — rename між
//     дисками завжди дає ERROR_NOT_SAME_DEVICE.
//
// Копія відкритого файлу читається коректно (антивірус/лог ще пишуть у
// хвіст, але вміст, що вже збережений, доступний).
func moveFile(from, to string) error {
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	if err := copyFileBytes(from, to); err != nil {
		return err
	}
	_ = os.Remove(from)
	return nil
}

// copyFileBytes копіює вміст файлу (фолбек для rename, що не вдався).
func copyFileBytes(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(to)
		return err
	}
	return out.Close()
}

func moveTree(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	var firstErr error
	for _, e := range entries {
		from := filepath.Join(src, e.Name())
		to := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := moveTree(from, to); err != nil && firstErr == nil {
				firstErr = err
			}
			continue
		}
		if fileExists(to) {
			continue // у призначенні вже є такий файл — лишаємо його
		}
		if err := moveFile(from, to); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	// Текa-джерело тепер порожня (або містить лише те, що не перенеслося) —
	// прибираємо її, якщо вона спорожніла.
	if rest, _ := os.ReadDir(src); len(rest) == 0 {
		_ = os.RemoveAll(src)
	}
	return firstErr
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ensureNoLegacyFiles — контрольна перевірка після міграції: старі файли
// не повинні існувати (видалені вище). Повертає список тих, що лишились
// (для діагностики; не критично — вони більше не читаються кодом).
func (a *App) ensureNoLegacyFiles() []string {
	var left []string
	for _, p := range []string{
		filepath.Join(a.cfg.Dir(), "builds.json"),
		filepath.Join(a.cfg.ConfigDir(), "custom-packs.json"),
		filepath.Join(a.cfg.ConfigDir(), "gallery-meta.json"),
		a.shortcutsFile(),
	} {
		if fileExists(p) {
			left = append(left, p)
		}
	}
	return left
}
