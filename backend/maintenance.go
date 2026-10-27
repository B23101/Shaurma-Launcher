// maintenance.go — бот-прибиральник (garbage collector) лаунчера.
//
// Запускається фоном при старті (ServiceStartup → startMaintenance) і
// періодично (раз на годину) чистить дані, які накопичуються і нікому не
// потрібні, щоб база даних і кеш не роздувались:
//
//   - ШІ-кеш браузера контенту (browser_ai_cache): переклади/відгуки, які
//     не показувались 30 днів — видаляються (переклад/відгук генеруються
//     заново при наступному відкритті);
//   - install_state: стан збірок, яких більше немає в реєстрі встановлених
//     (видалені/фантомні) — стан-сирота. Стан завершених (StageComplete)
//     збірок НЕ чіпаємо: його Files — це кеш хешів, який дозволяє наступному
//     запуску гри не хешувати всі файли збірки заново;
//   - іконки ярликів (.ico у cache/shortcuts): якщо .lnk на робочому столі
//     видалений — іконка більше не потрібна, реєстр теж чиститься;	//   - аватари в cache: файли, які не відкривались 30 днів — це кеш, вони
//     відновлюються автоматично при наступному показі. Скіни гардеробу
//     (.shaurm/skins) НЕ чіпаються — це постійні дані користувача;
//   - gallery_meta: метадані скріншотів, чиї файли вже видалені з диска
//     (скріншот стерли у .minecraft/screenshots) — запис-сирота.
//
// Все ідемпотентно і безпечно: помилки лише логуються, видалення не
// чіпає жодних «живих» даних.
package main

import (
	"shaurma-launcher-backend/internal/crashguard"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"shaurma-launcher-backend/internal/custompack"
	"shaurma-launcher-backend/internal/store"
)

// AI-кеш: переклади і відгуки живуть 3 дні (користувач: «ШІ у базу даних
// з очищенням, якщо воно більше 3 днів»). Старі відповіді генеруються
// заново при наступному відкритті проєкту — кеш лишається «свіжим», а
// застаріле не накопичується в БД.
const aiCacheTTL = 3 * 24 * time.Hour

// maintenanceInt — період між проходами прибиральника.
const maintenanceInt = 1 * time.Hour

// textureCacheTTL — «свіжість» закешованих текстур голів/скінів.
const textureCacheTTL = 30 * 24 * time.Hour

// startMaintenance запускає фоновий цикл прибиральника: перший прохід —
// одразу після старту (щоб щойно мігрована БД не почала зі сміття), далі —
// кожну годину. Goroutine завершується разом із процесом.
func (a *App) startMaintenance() {
	go func() {
		defer crashguard.Recover("maintenance.go:52")
		a.runMaintenance()
		ticker := time.NewTicker(maintenanceInt)
		defer ticker.Stop()
		for range ticker.C {
			a.runMaintenance()
		}
	}()
}

// runMaintenance виконує один прохід прибирання.
func (a *App) runMaintenance() {
	st := a.cfg.Store()
	if st == nil {
		return // БД недоступна — прибирати нічого
	}
	a.cleanupAICache(st)
	a.cleanupInstallState(st)
	a.cleanupExportSettings(st)
	a.cleanupDuplicateSettings(st)
	a.cleanupShortcutIcons()
	a.cleanupCacheTextures()
	a.cleanupGalleryOrphans(st)
	a.cleanupInstallDirGarbage()
	a.cleanupSkinOrphans()
	a.cleanupExpiredTempMods()
}

// installDirGarbageTTL — вік, після якого службові файли апдейтера в теці
// встановлення вважаються сміттям: .previous (backup перед заміною), .part
// (недокачане), .new (відкладене довстановлення), <shell>~ (відкладений
// self-update exe). Свіжі файли (активне оновлення або ще не застосоване
// довстановлення) НЕ чіпаються.
const installDirGarbageTTL = 7 * 24 * time.Hour

// cleanupInstallDirGarbage прибирає з теки встановлення (ConfigDir — та сама,
// де лежить launcher-version.json, backend.dll тощо) службові файли
// апдейтера, які накопичуються після оновлень: *.previous (backup перед
// заміною файлу — якщо оновлення давно застосовано, backup не потрібен),
// *.part (обірвані завантаження), *.new (відкладені довстановлення, які
// shell не застосував), <shell>~ (старий exe після self-update). Свіжі
// (молодші TTL) лишаються — це активний процес оновлення.
func (a *App) cleanupInstallDirGarbage() {
	dir := a.cfg.ConfigDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-installDirGarbageTTL)
	removed := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		isGarbage := strings.HasSuffix(name, ".previous") ||
			strings.HasSuffix(name, ".part") ||
			strings.HasSuffix(name, ".new") ||
			strings.HasSuffix(name, "~")
		if !isGarbage {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			if os.Remove(filepath.Join(dir, name)) == nil {
				removed++
			}
		}
	}
	if removed > 0 {
		log.Printf("maintenance: видалено %d службових файлів апдейтера з %s", removed, dir)
	}
}

// cleanupAICache видаляє ШІ-переклади/відгуки старші за TTL.
func (a *App) cleanupAICache(st *store.Store) {
	for _, kind := range []string{aiCacheKindTranslation, aiCacheKindReview} {
		n, err := st.DeleteAICacheKindOlderThan(kind, aiCacheTTL)
		if err != nil {
			log.Printf("maintenance: очищення ШІ-кешу (%s): %v", kind, err)
			continue
		}
		if n > 0 {
			log.Printf("maintenance: видалено %d застарілих ШІ-%s з кешу", n, kind)
		}
	}
}

// cleanupInstallState прибирає стан встановлення ЗБІРОК, ЯКИХ БІЛЬШЕ НЕМАЄ
// в реєстрі встановлених (видалені/фантомні) — стан-сирота. Стан завершеної
// збірки (StageComplete) не чіпаємо: його Files — це кеш хешів, що дозволяє
// наступному запуску не хешувати всі файли збірки заново (видалення
// призвело б до повної перевірки кожної збірки при кожному запуску).
func (a *App) cleanupInstallState(st *store.Store) {
	// InstallState тепер зберігається в install-state/<packID>.json
	// (sync.StateStore), тому чистимо через нього.
	syncStore := a.syncStateStore()
	removed := syncStore.CleanupOrphaned(func() map[string]bool {
		active := make(map[string]bool)
		for _, id := range a.buildsReg.AllIDs() {
			active[id] = true
		}
		return active
	})
	if removed > 0 {
		log.Printf("maintenance: прибрано %d install_state видалених збірок", removed)
	}
}

// cleanupExportSettings прибирає export_settings для видалених збірок —
// той самий принцип «стан-сирота», що cleanupInstallState вище: якщо
// packID немає в реєстрі встановлених, збережені параметри останнього
// експорту цій збірці більше ніколи не знадобляться.
func (a *App) cleanupExportSettings(st *store.Store) {
	keys, err := st.LoadAllExportSettingsKeys()
	if err != nil {
		log.Printf("maintenance: export_settings keys: %v", err)
		return
	}
	removed := 0
	for _, packID := range keys {
		if _, ok := a.buildsReg.Get(packID); !ok {
			if err := st.DeleteExportSettings(packID); err == nil {
				removed++
			}
		}
	}
	if removed > 0 {
		log.Printf("maintenance: прибрано %d export_settings видалених збірок", removed)
	}
}

// cleanupDuplicateSettings — той самий принцип, що cleanupExportSettings
// вище, для duplicate_settings (pack_id тут — збірка-ДЖЕРЕЛО дублювання).
func (a *App) cleanupDuplicateSettings(st *store.Store) {
	keys, err := st.LoadAllDuplicateSettingsKeys()
	if err != nil {
		log.Printf("maintenance: duplicate_settings keys: %v", err)
		return
	}
	removed := 0
	for _, packID := range keys {
		if _, ok := a.buildsReg.Get(packID); !ok {
			if err := st.DeleteDuplicateSettings(packID); err == nil {
				removed++
			}
		}
	}
	if removed > 0 {
		log.Printf("maintenance: прибрано %d duplicate_settings видалених збірок", removed)
	}
}

// cleanupShortcutIcons видаляє .ico іконки ярликів, чиї .lnk на робочому
// столі вже видалені (користувач прибрав ярлик вручну або збірку видалили),
// і чистить відповідні записи реєстру.
func (a *App) cleanupShortcutIcons() {
	reg := a.loadShortcutRegistry()
	if len(reg) == 0 {
		return
	}
	desktop, err := desktopDir()
	changed := false
	for packID, lnkName := range reg {
		exists := false
		if err == nil {
			_, statErr := os.Stat(filepath.Join(desktop, lnkName))
			exists = statErr == nil
		}
		if exists {
			continue
		}
		// Ярлика немає — прибираємо запис реєстру та іконку .ico.
		delete(reg, packID)
		_ = os.Remove(filepath.Join(a.cfg.CacheDir(), "shortcuts", packID+".ico"))
		changed = true
	}
	if changed {
		_ = a.saveShortcutRegistry(reg)
		log.Printf("maintenance: прибрано записи ярликів без .lnk на робочому столі")
	}
}

// cleanupCacheTextures видаляє з cache/avatars файли, що не відкривались
// textureCacheTTL (кеш голів відновлюється автоматично). Скіни гардеробу
// НЕ входять у цей список: вони живуть у .shaurm/skins як постійні дані
// користувача і не є кешем.
func (a *App) cleanupCacheTextures() {
	cutoff := time.Now().Add(-textureCacheTTL)
	for _, dir := range []string{
		filepath.Join(a.cfg.CacheDir(), "avatars"),
	} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		removed := 0
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			if info.ModTime().Before(cutoff) {
				if os.Remove(filepath.Join(dir, e.Name())) == nil {
					removed++
				}
			}
		}
		if removed > 0 {
			log.Printf("maintenance: видалено %d застарілих файлів з %s", removed, dir)
		}
	}
}

// cleanupSkinOrphans видаляє з .shaurm/skins PNG-файли, на які більше не
// посилається ЖОДЕН пресет гардеробу (їхній останній пресет видалили).
//
// Чому це безпечно: текстури дедуплікуються за вмістом (skin-<hash>.png),
// тому один і той самий файл можуть використовувати кілька пресетів —
// видаляємо ЛИШЕ ті, яких немає в жодному SkinPath/CapePath. Самі скіни
// лишаються постійними даними (не кеш), за віком не чіпаються.
func (a *App) cleanupSkinOrphans() {
	refs := map[string]struct{}{}
	for _, p := range a.wardrobe.AllReferencedTextures() {
		refs[filepath.Clean(p)] = struct{}{}
	}
	dir := a.wardrobe.SkinsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	removed := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		full := filepath.Join(dir, e.Name())
		if _, ok := refs[filepath.Clean(full)]; ok {
			continue // на файл ще посилається хоча б один пресет
		}
		if os.Remove(full) == nil {
			removed++
		}
	}
	if removed > 0 {
		log.Printf("maintenance: видалено %d PNG-сиріт гардеробу з %s", removed, dir)
	}
}

// cleanupGalleryOrphans прибирає метадані галереї для скріншотів, файли
// яких уже видалені з диска (<inst>/<packID>/.minecraft/screenshots).
// Галерея тепер зберігається в instance.json кожної збірки.
func (a *App) cleanupGalleryOrphans(_ *store.Store) {
	if a.customPacks == nil {
		return
	}
	removed := 0
	for _, cp := range a.customPacks.List() {
		gallery := a.gallery.GetAll(cp.ID)
		if len(gallery) == 0 {
			continue
		}
		screensDir := filepath.Join(a.gameDir(cp.ID), "screenshots")
		for fileName := range gallery {
			if _, err := os.Stat(filepath.Join(screensDir, fileName)); err != nil {
				_ = a.gallery.Delete(cp.ID, fileName)
				removed++
			}
		}
	}
	if removed > 0 {
		log.Printf("maintenance: прибрано %d записів галереї без файлів скріншотів", removed)
	}
}

// cleanupExpiredTempMods обробляє тимчасові моди («становлення на
// строк»), чий строк уже настав: видаляє файл повністю (Action="delete")
// або вимикає його (Action="disable", перейменування на .jar.disabled —
// той самий механізм, що звичайне вимкнення з менеджера модів). Прохід
// іде по ВСІХ збірках одразу (AllPacksWithTempMods — один виклик замість
// List()+GetTempMods у циклі, див. коментар на цьому методі).
//
// Порядок обробки в межах однієї збірки важливий: спершу застосовуються
// всі прострочені записи (видалення/вимкнення файлів), і лише ПОТІМ —
// одним повторним скануванням — перевіряється, чи «осиротілі» залежності
// (AddedByDependency=true, чий RequiredBy щойно прибрано) усе ще
// потрібні ЯКОМУСЬ іншому модові, що лишився увімкненим. Без цього
// порядку залежність, підтягнута ЛИШЕ заради видаленого мода, лишалась
// би в mods/ назавжди (тимчасова мітка на самій залежності знята НЕ
// буде, доки хтось явно не приберe РЕШТУ, а RequiredBy вказує вже на
// неіснуючий файл).
func (a *App) cleanupExpiredTempMods() {
	if a.customPacks == nil {
		return
	}
	byPack := a.customPacks.AllPacksWithTempMods()
	now := time.Now()
	totalProcessed := 0

	for packID, entries := range byPack {
		modsDir := a.instanceModsDir(packID)
		var expiredParents []string // FileName головних (не-залежність) модів, що спрацювали цим проходом

		for fileName, e := range entries {
			if now.Before(e.ExpiresAt) {
				continue
			}
			if a.applyExpiredTempMod(packID, modsDir, fileName, e) {
				totalProcessed++
				if !e.AddedByDependency {
					expiredParents = append(expiredParents, fileName)
				}
			}
		}

		if len(expiredParents) > 0 {
			a.cleanupOrphanedTempDeps(packID, modsDir, expiredParents)
			a.invalidateModUpdateCache(packID)
			a.InvalidateBrowserInstalledCache(packID)
			a.emit("mods:changed", packID)
		}
	}

	if totalProcessed > 0 {
		log.Printf("maintenance: оброблено %d прострочених тимчасових модів", totalProcessed)
	}
}

// applyExpiredTempMod застосовує дію (видалити/вимкнути) до ОДНОГО
// простроченого запису і знімає його мітку з Pack.TempMods незалежно від
// результату видалення файлу — файл, якого вже немає на диску (гравець
// прибрав його вручну раніше), не повинен лишати мертву мітку назавжди.
// Повертає true, якщо запис оброблено (для підрахунку в лозі й для
// вирішення, чи запускати cleanupOrphanedTempDeps).
func (a *App) applyExpiredTempMod(packID, modsDir, fileName string, e custompack.TempModEntry) bool {
	var err error
	switch e.Action {
	case "delete":
		err = a.modMgr.DeleteMod(modsDir, fileName)
	default: // "disable" і будь-яке невідоме значення — безпечніший шлях
		err = a.modMgr.DisableMod(modsDir, fileName)
	}
	if err != nil && !os.IsNotExist(err) {
		log.Printf("maintenance: тимчасовий мод %s (%s, збірка %s): %v", fileName, e.Action, packID, err)
		// Не повертаємось — мітку все одно знімаємо нижче: помилка
		// перейменування/видалення файлу не повинна означати, що
		// прибиральник намагатиметься те саме щогодини нескінченно без
		// жодного логу про причину (лог уже написано рядком вище).
	}
	if rmErr := a.customPacks.RemoveTempMod(packID, fileName); rmErr != nil {
		log.Printf("maintenance: зняття мітки тимчасового мода %s (збірка %s): %v", fileName, packID, rmErr)
	}
	return true
}

// cleanupOrphanedTempDeps перевіряє залежності, підтягнуті ЛИШЕ заради
// щойно прибраних головних модів (expiredParents), і прибирає ті з них,
// яких не потребує БІЛЬШЕ ЖОДЕН із модів, що лишились у збірці. Один
// повторний скан модів (ПІСЛЯ того, як головні прострочені моди вже
// прибрані з диска) — щоб бачити актуальний стан mods/, а не той, що був
// до видалення.
func (a *App) cleanupOrphanedTempDeps(packID, modsDir string, expiredParents []string) {
	remaining := a.customPacks.GetTempMods(packID)
	if len(remaining) == 0 {
		return
	}
	expiredSet := map[string]bool{}
	for _, p := range expiredParents {
		expiredSet[p] = true
	}

	all, err := a.modMgr.ScanModsFull(modsDir)
	if err != nil {
		return
	}
	// modID → ще потрібен ЯКИМОСЬ модом, що лишився на диску.
	stillNeeded := map[string]bool{}
	for _, m := range all {
		for _, dep := range m.DependsOn {
			stillNeeded[dep] = true
		}
	}

	for fileName, e := range remaining {
		if !e.AddedByDependency || !expiredSet[e.RequiredBy] {
			continue // не осиротіла залежність саме цього проходу
		}
		var modID string
		var provides []string
		for _, m := range all {
			if m.FileName == fileName {
				modID = m.ID
				provides = m.Provides
				break
			}
		}
		needed := stillNeeded[modID]
		for _, p := range provides {
			if stillNeeded[p] {
				needed = true
			}
		}
		if needed {
			continue // хтось інший, хто лишився, досі має цей modId у DependsOn
		}
		// Нікому більше не потрібна — застосовуємо ту саму дію, що була
		// би застосована для неї самої (успадковану дію запису).
		if a.applyExpiredTempMod(packID, modsDir, fileName, e) {
			log.Printf("maintenance: прибрано осиротілу залежність %s (збірка %s)", fileName, packID)
		}
	}
}
